package controller

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	nominalOperatingCellTemperature = 45.0   // Nominal Operating Cell Temperature in °C
	powerTemperatureCoefficient     = 0.0035 // Typical power temperature coefficient
)

type WeatherSolar struct {
	TSHourStart    int64
	ImprovedSolar  float64
	UnclippedSolar float64
	SnowDepth      float64
	TempFactor     float64
	SnowFactor     float64
	TCell          float64
	Irradiance     float64
}

// CalculateWeatherSolar estimates solar generation for each weather hour by:
//  1. Calibrating robust hourly efficiency factors from filtered historical actual solar vs. irradiance data.
//  2. Tracking snow accumulation and melt sequentially to derive a snow attenuation factor.
//  3. Applying NOCT-based cell temperature estimation to correct for temperature-dependent efficiency.
//  4. Projecting forward (and backward for history comparison) using the calibrated efficiency.
//
// Returns a map keyed by Unix timestamp (seconds) of each weather hour's computed improvedSolar.
func CalculateWeatherSolar(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	locInfo types.SiteLocation,
) map[int64]WeatherSolar {

	// Index weather by timestamp; later hours overwrite earlier for the same slot (dedup).
	weatherByHour := make(map[int64]types.HourlyWeather)
	for _, w := range weather {
		for _, hw := range w.ForecastHours {
			weatherByHour[hw.TSHourStart.Unix()] = hw
		}
	}

	// 2. Process hours chronologically so snow state carries forward correctly.
	timestamps := make([]int64, 0, len(weatherByHour))
	for ts := range weatherByHour {
		timestamps = append(timestamps, ts)
	}
	slices.Sort(timestamps)

	// Identify the best irradiance source (GTI if available, fallback to GHI)
	var anyGTI bool
	var anyGHI bool
	for _, hw := range weatherByHour {
		if hw.GTI > 0 {
			anyGTI = true
			break
		}
		if hw.GHI > 0 {
			anyGHI = true
		}
	}
	useGTI := anyGTI || !anyGHI // if no GHI either, doesn't matter, but if we have GTI use it.

	getIrr := func(hw types.HourlyWeather) float64 {
		if useGTI {
			return hw.GTI
		}
		return hw.GHI
	}

	timeLoc, err := time.LoadLocation(locInfo.TimeZone)
	if err != nil {
		timeLoc = time.UTC
	}

	hourlyEffs, clippingCap := CalibrateSolarScaleFactor(ctx, now, history, weather, locInfo, getIrr)

	results := make(map[int64]WeatherSolar, len(timestamps))

	// Project solar generation for every weather hour using the calibrated
	// efficiency unless we don't have any efficiency data.
	for _, ts := range timestamps {
		hw := weatherByHour[ts]
		h := WeatherSolar{TSHourStart: ts}

		h.Irradiance = getIrr(hw)

		// Estimated cell temperature via NOCT model:
		//   Tcell = Tamb + (Irradiance / 800) * (NOCT - 20)
		h.TCell = hw.TemperatureC + (h.Irradiance/800.0)*(nominalOperatingCellTemperature-20.0)

		// Use direct snow depth from API which is an average from this hour to the next
		h.SnowDepth = hw.SnowDepthCM

		// Calculate factor based on cell difference compared to STC (25C cell temp).
		h.TCell = min(max(h.TCell, -40), 80)
		h.TempFactor = 1.0 - (h.TCell-25)*powerTemperatureCoefficient

		h.SnowFactor = calculateSnowFactor(h.SnowDepth)

		localHour := time.Unix(ts, 0).In(timeLoc).Hour()
		eff := hourlyEffs[localHour]

		if eff > 0 && h.Irradiance > 0 {
			h.UnclippedSolar = h.Irradiance * eff * h.TempFactor * h.SnowFactor
			h.ImprovedSolar = h.UnclippedSolar
			if clippingCap > 0 && h.ImprovedSolar > clippingCap {
				h.ImprovedSolar = clippingCap
			}
		}
		results[ts] = h
	}

	return results
}

// CalculateSmoothedSolar averages usage and solar by hour of day from history and fits a bell curve.
func CalculateSmoothedSolar(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	settings types.Settings,
) map[int]float64 {
	hourlyData := make(map[int][]float64)

	// Regroup history by hour
	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		hour := h.TSHourStart.In(now.Location()).Hour()
		if h.SolarKWH > 0.1 {
			hourlyData[hour] = append(hourlyData[hour], h.SolarKWH)
		}
	}

	result := make(map[int]float64)
	for h, points := range hourlyData {
		var totalSolar float64
		for _, p := range points {
			totalSolar += p
		}
		result[h] = totalSolar / float64(len(points))
	}

	// if they disabled solar bell curve fitting return early
	if settings.SolarBellCurveMultiplier == 0 {
		return result
	}

	// determine "Daylight Hours" range
	startSolarHour := -1
	endSolarHour := -1
	for h, val := range result {
		if val > 0.1 {
			if startSolarHour == -1 || h < startSolarHour {
				startSolarHour = h
			}
			if h > endSolarHour {
				endSolarHour = h
			}
		}
	}

	if startSolarHour == -1 || endSolarHour == -1 {
		return result
	}

	daylightDuration := endSolarHour - startSolarHour + 1
	sigma := float64(daylightDuration) / 3.0
	mu := float64(startSolarHour) + float64(daylightDuration)/2.0

	bellCurveFactor := func(x float64) float64 {
		return math.Exp(-math.Pow(x-mu, 2) / (2 * math.Pow(sigma, 2)))
	}

	maxEstimatedPeak := 0.0
	maxOriginalPeak := 0.0
	validSolarByHour := make(map[int][]float64)

	for _, h := range history {
		hourStart := h.TSHourStart.In(now.Location())
		hour := hourStart.Hour()
		hourFactor := bellCurveFactor(float64(hour))
		if h.SolarKWH <= 0.1 || hourFactor <= 0.2 {
			continue
		}

		if h.SolarKWH > maxOriginalPeak {
			maxOriginalPeak = h.SolarKWH
		}

		if h.GridExportKWH > 0.1 || h.MaxBatterySOC < 98.0 {
			validSolarByHour[hour] = append(validSolarByHour[hour], h.SolarKWH)
		}
	}

	bestHour := -1
	maxCount := 0
	maxAvg := 0.0

	for h, readings := range validSolarByHour {
		count := len(readings)
		sum := 0.0
		for _, v := range readings {
			sum += v
		}
		avg := sum / float64(count)

		if count > maxCount {
			bestHour = h
			maxCount = count
			maxAvg = avg
		} else if count == maxCount {
			if avg > maxAvg {
				bestHour = h
				maxAvg = avg
			} else if avg == maxAvg {
				// Deterministic tie-breaker: prefer hours closer to solar noon (12:00)
				// to align with the peak of the bell curve.
				currDist := math.Abs(float64(h) - 12.0)
				bestDist := math.Abs(float64(bestHour) - 12.0)
				if currDist < bestDist || (currDist == bestDist && h < bestHour) {
					bestHour = h
					maxAvg = avg
				}
			}
		}
	}

	if bestHour != -1 {
		factor := bellCurveFactor(float64(bestHour))
		maxEstimatedPeak = maxAvg / factor
	}

	if maxEstimatedPeak == 0 {
		for _, h := range history {
			hourStart := h.TSHourStart.In(now.Location())
			hourFactor := bellCurveFactor(float64(hourStart.Hour()))
			if h.SolarKWH > 0.1 && hourFactor > 0.2 {
				if h.SolarKWH > maxOriginalPeak {
					maxOriginalPeak = h.SolarKWH
					maxEstimatedPeak = h.SolarKWH / hourFactor
				}
			}
		}
	}

	if maxEstimatedPeak == 0 {
		return result
	}

	for h := startSolarHour; h <= endSolarHour; h++ {
		curr := result[h]
		predicted := maxEstimatedPeak * bellCurveFactor(float64(h))
		if curr < predicted {
			newSolar := curr + (predicted-curr)*settings.SolarBellCurveMultiplier
			result[h] = newSolar
		}
	}

	return result
}

// calculateSunPosition calculates the sun's elevation and azimuth in degrees for a given time, latitude, and longitude.
// The returned azimuth uses the compass convention: 0 = North, 90 = East, 180 = South, 270 = West.
// The returned elevation is the angle above the horizon (in degrees).
func calculateSunPosition(t time.Time, lat, lng float64) (elevation, azimuth float64) {
	const (
		rad   = math.Pi / 180.0
		dayMs = 24.0 * 60.0 * 60.0 * 1000.0
		j1970 = 2440588.0
		j2000 = 2451545.0
		e     = 23.4397 * rad // obliquity of the Earth
	)

	// Convert to UTC for astronomical calculations
	tUTC := t.UTC()
	ms := float64(tUTC.Unix())*1000.0 + float64(tUTC.Nanosecond())/1e6
	toJulian := ms/dayMs - 0.5 + j1970
	d := toJulian - j2000

	// Solar Mean Anomaly
	M := rad * (357.5291 + 0.98560028*d)

	// Ecliptic Longitude
	C := rad * (1.9148*math.Sin(M) + 0.02*math.Sin(2*M) + 0.0003*math.Sin(3*M))
	P := 102.9372 * rad // perihelion of the Earth
	L := M + C + P + math.Pi

	// Declination (b = 0)
	dec := math.Asin(math.Sin(e) * math.Sin(L))

	// Right ascension (b = 0)
	ra := math.Atan2(math.Sin(L)*math.Cos(e), math.Cos(L))

	// Sidereal Time
	lw := rad * -lng
	phi := rad * lat
	H := (rad*(280.16+360.9856235*d) - lw) - ra

	// Altitude (elevation)
	altRad := math.Asin(math.Sin(phi)*math.Sin(dec) + math.Cos(phi)*math.Cos(dec)*math.Cos(H))
	elevation = altRad / rad

	// Azimuth
	azRad := math.Atan2(math.Sin(H), math.Cos(H)*math.Sin(phi)-math.Tan(dec)*math.Cos(phi))
	azDeg := azRad / rad

	// Convert azimuth from 0 is South, positive West to compass degrees (0 is North)
	compAz := azDeg + 180.0
	compAz = math.Mod(compAz, 360.0)
	if compAz < 0 {
		compAz += 360.0
	}
	azimuth = compAz

	return elevation, azimuth
}

// calculateAngleOfIncidence calculates the angle of incidence (in radians) of the sun on a tilted solar array.
// All input angles should be in degrees.
// elevation: sun elevation angle above the horizon (0 to 90)
// sunAzimuth: compass direction of the sun (0 to 360)
// arrayTilt: tilt angle of the array from horizontal (0 to 90)
// arrayAzimuth: compass direction the array is facing (0 to 360)
func calculateAngleOfIncidence(elevation, sunAzimuth, arrayTilt, arrayAzimuth float64) float64 {
	const rad = math.Pi / 180.0
	elRad := elevation * rad
	sunAzRad := sunAzimuth * rad
	tiltRad := arrayTilt * rad
	arrAzRad := arrayAzimuth * rad

	cosAOI := math.Sin(elRad)*math.Cos(tiltRad) + math.Cos(elRad)*math.Sin(tiltRad)*math.Cos(sunAzRad-arrAzRad)

	if cosAOI > 1.0 {
		cosAOI = 1.0
	} else if cosAOI < -1.0 {
		cosAOI = -1.0
	}

	return math.Acos(cosAOI)
}

// calculateGTI calculates the Global Tilted Irradiance (W/m²) for a given period.
// If sun elevation is <= 0, the sun is below the horizon and GTI is 0.
func calculateGTI(dni, dhi, elevation, sunAzimuth, arrayTilt, arrayAzimuth float64) float64 {
	if elevation <= 0 {
		return 0.0
	}

	aoi := calculateAngleOfIncidence(elevation, sunAzimuth, arrayTilt, arrayAzimuth)
	cosAOI := math.Cos(aoi)
	if cosAOI < 0 {
		cosAOI = 0.0
	}

	// 1. Direct Beam Component
	direct := dni * cosAOI

	// 2. Diffuse Component (Isotropic Sky View Model)
	const rad = math.Pi / 180.0
	tiltRad := arrayTilt * rad
	diffuse := dhi * (1.0 + math.Cos(tiltRad)) / 2.0

	// We completely omit the Ground Reflected Component (albedo) because the training
	// and calibration loop divides the actual historical solar production by this
	// raw theoretical GTI. This learned hourly multiplier (efficiency) organically
	// absorbs the user's specific albedo (e.g. concrete, grass, gravel). Hardcoding
	// a generic 20% albedo here would force a standard albedo onto all installations
	// and distort the site-specific calibration scale.
	// TODO: Handle snow albedo dynamically in the future.

	return direct + diffuse
}

// CalibrateSolarScaleFactor calculates the calibrated solar scale factor (efficiency) by comparing
// historical actual solar production against theoretical irradiance.
func CalibrateSolarScaleFactor(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	locInfo types.SiteLocation,
	getIrradiance func(hw types.HourlyWeather) float64,
) (hourlyEffs [24]float64, clippingCap float64) {
	const (
		clippingEps = 0.05 // kWh epsilon for detecting a plateau
	)

	// Gather all forecast hours across all days in weather
	var forecastHours []types.HourlyWeather
	for _, w := range weather {
		forecastHours = append(forecastHours, w.ForecastHours...)
	}

	// 1. Index historical actual solar by hour timestamp for O(1) lookup.
	statsByHour := make(map[int64]types.EnergyStats, len(history))
	maxSolarKWH := 0.0
	for _, h := range history {
		statsByHour[h.TSHourStart.Unix()] = h
		if h.SolarKWH > maxSolarKWH {
			maxSolarKWH = h.SolarKWH
		}
	}

	// Learning the Clipping Cap (Hourly):
	// Identify days where production plateaus at the peak (Hybrid Plateau & Frequency approach).
	var hourlyClippingCap float64
	if maxSolarKWH > 2.0 { // only consider clipping if production is significant
		// 1. Group hourly values by day
		byDay := make(map[string][]float64)
		for _, s := range history {
			if s.TSHourStart.IsZero() {
				continue
			}
			dayStr := s.TSHourStart.Format("2006-01-02")
			if s.SolarKWH > 0.5 {
				byDay[dayStr] = append(byDay[dayStr], s.SolarKWH)
			}
		}

		var dayPlateaus []float64
		for _, dayVals := range byDay {
			if len(dayVals) < 3 {
				continue
			}
			sort.Float64s(dayVals)
			// Sort descending
			for i, j := 0, len(dayVals)-1; i < j; i, j = i+1, j-1 {
				dayVals[i], dayVals[j] = dayVals[j], dayVals[i]
			}

			// If the day's peak is near the historical window max
			if dayVals[0] > maxSolarKWH*0.85 {
				// Check for plateau: 3rd highest is within 4% of peak, OR 2nd highest is within 1.5%
				if dayVals[2]/dayVals[0] >= 0.96 {
					dayPlateaus = append(dayPlateaus, dayVals[0])
				} else if dayVals[1]/dayVals[0] >= 0.985 {
					dayPlateaus = append(dayPlateaus, (dayVals[0]+dayVals[1])/2.0)
				}
			}
		}

		if len(dayPlateaus) > 0 {
			sort.Float64s(dayPlateaus)
			hourlyClippingCap = dayPlateaus[len(dayPlateaus)-1] // return the max of the detected plateaus
			log.Ctx(ctx).DebugContext(
				ctx,
				"learned hourly inverter clipping cap via daily plateaus",
				slog.Float64("capKWH", hourlyClippingCap),
				slog.Int("plateauDays", len(dayPlateaus)),
			)
		} else {
			// 2. Fall back to frequency count with a threshold of 3 occurrences
			usageCounts := make(map[int]int)
			for _, s := range history {
				if s.SolarKWH > maxSolarKWH*0.9 {
					// Round to 1 decimal place to group similar peak values
					val := int(math.Round(s.SolarKWH * 10))
					usageCounts[val]++
				}
			}
			mostFreqVal := -1
			mostFreqCount := 0
			for val, count := range usageCounts {
				if count > mostFreqCount {
					mostFreqVal = val
					mostFreqCount = count
				}
			}
			if mostFreqVal > 0 && mostFreqCount >= 3 {
				hourlyClippingCap = float64(mostFreqVal) / 10.0
				log.Ctx(ctx).DebugContext(
					ctx,
					"learned hourly inverter clipping cap via frequency fallback",
					slog.Float64("capKWH", hourlyClippingCap),
					slog.Int("occurrences", mostFreqCount),
				)
			} else {
				log.Ctx(ctx).DebugContext(
					ctx,
					"found no hourly inverter clipping cap",
					slog.Float64("maxSolarKWH", maxSolarKWH),
					slog.Float64("frequentKWH", float64(mostFreqVal)/10.0),
					slog.Int("occurrences", mostFreqCount),
				)
			}
		}
	}

	// Reject learned clipping cap if it is significantly below the maximum observed production
	// in the history window, as the system has proven it can generate more.
	// We allow a small tolerance (e.g. 5% and 0.3 kWh) for minor hourly fluctuations or sensor noise.
	if hourlyClippingCap > 0 {
		if maxSolarKWH > hourlyClippingCap*1.05 && maxSolarKWH > hourlyClippingCap+0.3 {
			log.Ctx(ctx).DebugContext(
				ctx,
				"rejected learned clipping cap (proven higher peak exists)",
				slog.Float64("learnedCap", hourlyClippingCap),
				slog.Float64("maxSolarKWH", maxSolarKWH),
			)
			hourlyClippingCap = 0
		}
	}

	clippingCap = hourlyClippingCap

	// Index weather by timestamp; later hours overwrite earlier for the same slot (dedup).
	weatherByHour := make(map[int64]types.HourlyWeather)
	for _, hw := range forecastHours {
		weatherByHour[hw.TSHourStart.Unix()] = hw
	}

	timeLoc, err := time.LoadLocation(locInfo.TimeZone)
	if err != nil {
		timeLoc = time.UTC
	}

	// We calculate a preliminary static scale factor (staticEff) first.
	// We'll use this static efficiency to perform the 15-minute clipping detection,
	// and as a fallback if hourly calibration doesn't have enough data points.
	var staticEff float64
	var minClippedIrradiance float64
	if clippingCap > 0 {
		for ts, stats := range statsByHour {
			if stats.SolarKWH >= clippingCap-clippingEps {
				if hw, ok := weatherByHour[ts]; ok {
					irr := getIrradiance(hw)
					if irr > 0 && (irr < minClippedIrradiance || minClippedIrradiance == 0) {
						minClippedIrradiance = irr
					}
				}
			}
		}
	}

	type dailyAcc struct {
		solarKWH         float64
		theoreticalIrrad float64
		count            int
	}
	dailyData := make(map[string]*dailyAcc)

	for ts, hw := range weatherByHour {
		irradiance := getIrradiance(hw)
		tCell := hw.TemperatureC + (irradiance/800.0)*(nominalOperatingCellTemperature-20.0)
		tCell = min(max(tCell, -40), 80)
		tempFactor := 1.0 - (tCell-25)*powerTemperatureCoefficient

		snowDepth := hw.SnowDepthCM
		snowFactor := calculateSnowFactor(snowDepth)

		if stats, ok := statsByHour[ts]; ok {
			isCurtailed := isSolarCurtailed(stats)
			isSnowy := snowDepth > 0.2
			hasSolar := stats.SolarKWH > 0.5
			isClipped := clippingCap > 0 && stats.SolarKWH >= clippingCap-clippingEps
			currentHour := ts == now.Truncate(time.Hour).Unix()

			// Skip curtailed hours (when battery is full and we aren't exporting, solar is throttled)
			// and snowy hours (snow coverage blocks solar panels, obscuring true efficiency)
			// to avoid skewing our calibration of the panel's physical scale factor.
			if irradiance >= 25 && tempFactor > 0 && hasSolar && !isCurtailed && !isSnowy && !currentHour {
				effectiveIrradiance := irradiance
				if isClipped && minClippedIrradiance > 0 {
					effectiveIrradiance = math.Min(irradiance, minClippedIrradiance)
				}

				dayStr := time.Unix(ts, 0).In(timeLoc).Format("2006-01-02")
				if dailyData[dayStr] == nil {
					dailyData[dayStr] = &dailyAcc{}
				}
				dailyData[dayStr].solarKWH += stats.SolarKWH
				dailyData[dayStr].theoreticalIrrad += effectiveIrradiance * tempFactor * snowFactor
				dailyData[dayStr].count++
			}
		}
	}

	var totalSolarKWH float64
	var totalTheoreticalIrrad float64
	for _, acc := range dailyData {
		totalSolarKWH += acc.solarKWH
		totalTheoreticalIrrad += acc.theoreticalIrrad
	}

	if totalTheoreticalIrrad > 0 {
		staticEff = totalSolarKWH / totalTheoreticalIrrad
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"calculated daily static scale factor using ratio estimator",
		slog.Float64("staticEfficiency", staticEff),
		slog.Float64("totalSolarKWH", totalSolarKWH),
		slog.Float64("totalTheoreticalIrrad", totalTheoreticalIrrad),
	)

	// Per-hour scale factor calibration
	// Re-calculate minClippedIrradiance using the updated clippingCap if not already set
	if minClippedIrradiance == 0.0 && clippingCap > 0 {
		for ts, stats := range statsByHour {
			if stats.SolarKWH >= clippingCap-clippingEps {
				if hw, ok := weatherByHour[ts]; ok {
					irr := getIrradiance(hw)
					if irr > 0 && (irr < minClippedIrradiance || minClippedIrradiance == 0) {
						minClippedIrradiance = irr
					}
				}
			}
		}
	}

	type hourlyAcc struct {
		solarKWH float64
		denom    float64
		count    int
	}
	efficienciesByHourOfDay := make(map[int]*hourlyAcc)
	for h := 0; h < 24; h++ {
		efficienciesByHourOfDay[h] = &hourlyAcc{}
	}

	for ts, hw := range weatherByHour {
		irradiance := getIrradiance(hw)
		tCell := hw.TemperatureC + (irradiance/800.0)*(nominalOperatingCellTemperature-20.0)
		tCell = min(max(tCell, -40), 80)
		tempFactor := 1.0 - (tCell-25)*powerTemperatureCoefficient

		snowDepth := hw.SnowDepthCM
		snowFactor := calculateSnowFactor(snowDepth)

		if stats, ok := statsByHour[ts]; ok {
			isCurtailed := isSolarCurtailed(stats)
			isSnowy := snowDepth > 0.2
			hasSolar := stats.SolarKWH > 0.5
			// An hour is clipped if actual solar is near the cap OR if theoretical unclipped solar exceeds the cap
			isClipped := clippingCap > 0 && (stats.SolarKWH >= clippingCap-clippingEps || (staticEff > 0 && irradiance*staticEff*tempFactor*snowFactor > clippingCap-clippingEps))
			currentHour := ts == now.Truncate(time.Hour).Unix()

			// Skip curtailed, snowy, and clipped hours so that the hourly shading factors
			// are learned from unconstrained and unblocked solar generation.
			if irradiance >= 25 && tempFactor > 0 && hasSolar && !isCurtailed && !isSnowy && !currentHour && !isClipped {
				effectiveIrradiance := irradiance
				denom := effectiveIrradiance * tempFactor * snowFactor
				if denom > 0 {
					eff := stats.SolarKWH / denom
					// Skip hourly outliers where weather forecast severely mismatched actual production
					if staticEff > 0 && (eff < 0.5*staticEff || eff > 1.5*staticEff) {
						continue
					}
					hourOfDay := time.Unix(ts, 0).In(timeLoc).Hour()
					efficienciesByHourOfDay[hourOfDay].solarKWH += stats.SolarKWH
					efficienciesByHourOfDay[hourOfDay].denom += denom
					efficienciesByHourOfDay[hourOfDay].count++
				}
			}
		}
	}

	validHours := make(map[int]float64)
	type hourScaleFactorLog struct {
		HourOfDay  int     `json:"hourOfDay"`
		Efficiency float64 `json:"efficiency"`
		NumPoints  int     `json:"numPoints"`
	}
	var hourScaleFactors []hourScaleFactorLog

	for h := 0; h < 24; h++ {
		acc := efficienciesByHourOfDay[h]
		if acc.count < 3 {
			if acc.count > 0 {
				// Only log if this hour ever has daylight during our forecast period
				hasDaylight := false
				for _, hw := range weatherByHour {
					if hw.TSHourStart.In(timeLoc).Hour() == h && getIrradiance(hw) >= 25 {
						hasDaylight = true
						break
					}
				}
				if hasDaylight {
					log.Ctx(ctx).DebugContext(
						ctx,
						"solar scale factor hour invalid (not enough points)",
						slog.Int("hourOfDay", h),
						slog.Int("numPoints", acc.count),
					)
				}
			}
			continue
		}

		if acc.denom > 0 {
			mean := acc.solarKWH / acc.denom
			validHours[h] = mean
			hourScaleFactors = append(hourScaleFactors, hourScaleFactorLog{
				HourOfDay:  h,
				Efficiency: mean,
				NumPoints:  acc.count,
			})
		}
	}

	if len(hourScaleFactors) > 0 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"stage 3: hour valid scale factors",
			slog.Any("factors", hourScaleFactors),
		)
	}

	// If we have at least 4 valid hours, interpolate the rest.
	// Otherwise, fall back to using staticEff for all hours.
	var rawHourlyEffs [24]float64
	if len(validHours) >= 4 {
		rawHourlyEffs = interpolateHourlyEfficiencies(validHours)
	} else {
		for h := 0; h < 24; h++ {
			rawHourlyEffs[h] = staticEff
		}
	}

	// 4. Adaptive Shading Regularization (Shrinkage)
	// Find daylight hours to assess shading variation.
	// We define daylight hours as any hour of day where irradiance is significant (>= 50 W/m2)
	// in the weather forecast.
	daylightHours := make(map[int]bool)
	for _, hw := range weatherByHour {
		if getIrradiance(hw) >= 50 {
			daylightHours[hw.TSHourStart.In(timeLoc).Hour()] = true
		}
	}

	var daylightRatios []float64
	for h := 0; h < 24; h++ {
		if daylightHours[h] {
			val := rawHourlyEffs[h]
			if val > 0 && staticEff > 0 {
				daylightRatios = append(daylightRatios, val/staticEff)
			}
		}
	}

	// Calculate standard deviation of ratios
	stdDevRatio := 0.0
	if len(daylightRatios) > 1 {
		var sumRatio float64
		for _, r := range daylightRatios {
			sumRatio += r
		}
		meanRatio := sumRatio / float64(len(daylightRatios))

		var sumSqDiff float64
		for _, r := range daylightRatios {
			sumSqDiff += math.Pow(r-meanRatio, 2)
		}
		stdDevRatio = math.Sqrt(sumSqDiff / float64(len(daylightRatios)))
	}

	// Compute weight w
	// If stdDevRatio <= 0.08, w = 0 (100% static, no shading)
	// If stdDevRatio >= 0.16, w = 1.0 (100% hourly, full shading)
	w := 0.0
	if stdDevRatio > 0.08 {
		w = (stdDevRatio - 0.08) / (0.16 - 0.08)
		if w > 1.0 {
			w = 1.0
		}
	}

	for h := 0; h < 24; h++ {
		val := rawHourlyEffs[h]
		if val == 0 {
			val = staticEff
		}

		// Clamp values that are physically unrealistic (i.e. efficiency way above staticEff)
		if val > 1.15*staticEff && staticEff > 0 {
			val = 1.15 * staticEff
		}

		hourlyEffs[h] = w*val + (1.0-w)*staticEff
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"calibrated per-hour scale factors with adaptive regularization",
		slog.Any("hourlyEfficiencies", hourlyEffs),
		slog.Float64("stdDevRatio", stdDevRatio),
		slog.Float64("regularizationWeight", w),
		slog.Float64("staticEfficiency", staticEff),
	)

	return hourlyEffs, clippingCap
}

// CalculateWeatherSolar1h calculates the solar generation using hourly mean DNI/DHI values.
func CalculateWeatherSolar1h(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	locInfo types.SiteLocation,
) map[int64]WeatherSolar {

	// Collect all forecast hours across all days in weather
	var forecastHours []types.HourlyWeather
	for _, w := range weather {
		forecastHours = append(forecastHours, w.ForecastHours...)
	}

	timeLoc, err := time.LoadLocation(locInfo.TimeZone)
	if err != nil {
		timeLoc = time.UTC
	}

	// Calculate our custom GTI for each forecast hour
	gtiByHour := make(map[int64]float64)
	for _, hw := range forecastHours {
		ts := hw.TSHourStart.Unix()
		// Compute sun position at the middle of the hour
		tMid := hw.TSHourStart.Add(30 * time.Minute)
		el, az := calculateSunPosition(tMid, locInfo.Latitude, locInfo.Longitude)
		gtiByHour[ts] = calculateGTI(hw.DNI, hw.DHI, el, az, locInfo.SolarTilt, locInfo.SolarAzimuth)
	}

	// Calibrate scale factor using our self-calculated GTI
	getIrr := func(hw types.HourlyWeather) float64 {
		return gtiByHour[hw.TSHourStart.Unix()]
	}
	hourlyEffs, clippingCap := CalibrateSolarScaleFactor(ctx, now, history, weather, locInfo, getIrr)

	results := make(map[int64]WeatherSolar)
	var anyEff bool
	for _, eff := range hourlyEffs {
		if eff > 0 {
			anyEff = true
			break
		}
	}

	if anyEff {
		for _, hw := range forecastHours {
			ts := hw.TSHourStart.Unix()
			gti := gtiByHour[ts]
			tCell := hw.TemperatureC + (gti/800.0)*(nominalOperatingCellTemperature-20.0)
			tCell = min(max(tCell, -40), 80)
			tempFactor := 1.0 - (tCell-25.0)*powerTemperatureCoefficient

			snowDepth := hw.SnowDepthCM
			snowFactor := calculateSnowFactor(snowDepth)

			localHour := hw.TSHourStart.In(timeLoc).Hour()
			eff := hourlyEffs[localHour]

			unclipped := gti * eff * tempFactor * snowFactor
			improved := unclipped
			if clippingCap > 0 && improved > clippingCap {
				improved = clippingCap
			}

			results[ts] = WeatherSolar{
				TSHourStart:    ts,
				ImprovedSolar:  improved,
				UnclippedSolar: unclipped,
				SnowDepth:      snowDepth,
				TempFactor:     tempFactor,
				SnowFactor:     snowFactor,
				TCell:          tCell,
				Irradiance:     gti,
			}
		}
	}

	return results
}

// interpolateHourlyEfficiencies performs circular linear interpolation over the 24 hours of the day.
// validHours maps the hour of the day (0..23) to its calibrated scale factor.
// Note: We interpolate circularly (wrapping across midnight) to ensure that dawn/dusk hours
// (which often lack sufficient historical data points to calibrate directly) receive smooth,
// realistic efficiency estimates. Any non-zero efficiency values assigned to night hours are
// harmless because the solar irradiance (DNI/DHI) at night is exactly 0.0, resulting in
// 0.0 projected generation regardless.
func interpolateHourlyEfficiencies(validHours map[int]float64) [24]float64 {
	var result [24]float64
	if len(validHours) == 0 {
		return result
	}

	for h := 0; h < 24; h++ {
		if val, ok := validHours[h]; ok {
			result[h] = val
			continue
		}

		// Find closest valid hour going backward (circularly)
		var prevHour int
		var prevDist int
		for d := 1; d <= 24; d++ {
			p := (h - d + 24) % 24
			if _, ok := validHours[p]; ok {
				prevHour = p
				prevDist = d
				break
			}
		}

		// Find closest valid hour going forward (circularly)
		var nextHour int
		var nextDist int
		for d := 1; d <= 24; d++ {
			n := (h + d) % 24
			if _, ok := validHours[n]; ok {
				nextHour = n
				nextDist = d
				break
			}
		}

		if prevDist > 0 && nextDist > 0 {
			totalDist := float64(prevDist + nextDist)
			valPrev := validHours[prevHour]
			valNext := validHours[nextHour]
			// Linear interpolation
			result[h] = (float64(nextDist)*valPrev + float64(prevDist)*valNext) / totalDist
		}
	}
	return result
}

// calculateSnowFactor returns the solar generation attenuation factor (0.0 to 1.0)
// based on snow depth in centimeters.
func calculateSnowFactor(snowDepthCM float64) float64 {
	switch {
	case snowDepthCM > 5.0:
		return 0.0
	case snowDepthCM > 0.2:
		return 0.1
	case snowDepthCM > 0.0:
		return 0.70
	default:
		return 1.0
	}
}

// isSolarCurtailed returns true if actual solar generation was likely throttled
// because the battery was full and grid export was disabled/blocked.
func isSolarCurtailed(stats types.EnergyStats) bool {
	return stats.GridExportKWH <= 0.1 && stats.MaxBatterySOC >= 98.0
}
