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

	// defaultSolarTilt defines the standard fixed roof tilt used for automatic solar layout
	// detection and solar forecast modeling. Empirical analysis across active production sites demonstrates
	// that varying tilt provides negligible accuracy benefit (<1.0% MAE) as hourly scaling factors absorb
	// tilt amplitude differences, while fixing tilt prevents model churn and degeneracy.
	defaultSolarTilt = 25.0

	// unconstrainedBaselineMinGTI is the minimum irradiance (W/m²) required to include an
	// unconstrained hour (!matchesHomeLoad) in the median reference efficiency baseline
	// (unconstrainedEffsByHour / unconstrainedHorizEffsByHour). Filtering out low-light (< 150 W/m²)
	// dawn/dusk hours prevents noisy low-angle conversion ratios from skewing the reference median.
	unconstrainedBaselineMinGTI = 150.0

	// dcCurtailmentHighGTI is the irradiance threshold (W/m²) indicating strong sunlight when
	// evaluating DC-coupled home-load-matching curtailment:
	// 1. In buildHistoricalCache, when a specific hour of the day has < 3 unconstrained historical
	//    samples (refEffByHour[h] == 0), we only fall back to the day-wide globalRefEff if
	//    gti >= dcCurtailmentHighGTI (300 W/m²), preventing low-angle or tree-shaded morning/evening
	//    hours from being falsely flagged against a midday-dominated global median.
	// 2. When gti >= dcCurtailmentHighGTI, there is enough sunlight that an hour matching home load
	//    at a fraction of normal efficiency can be flagged as DC-curtailed even if forecasted cloud cover is high.
	dcCurtailmentHighGTI = 300.0

	// dcCurtailmentOvercastCloudPercent is the cloud cover threshold (%) above which an hour
	// (when combined with gti < dcCurtailmentHighGTI) is treated as heavy overcast and protected
	// from DC-curtailment invalidation. On dark overcast/rainy days, Open-Meteo can over-predict
	// irradiance while actual solar drops low enough to coincidentally match home load; preserving
	// these hours ensures similarity weighting retains genuine cloudy-day telemetry.
	dcCurtailmentOvercastCloudPercent = 80.0

	// dcCurtailmentMaxEffRatio is the maximum efficiency ratio (relative to the median unconstrained
	// efficiency refEff) below which a home-load-matching hour in buildHistoricalCache is flagged
	// as DC-coupled curtailed (eff < 0.65 * refEff).
	dcCurtailmentMaxEffRatio = 0.65

	// dcCurtailmentEvalMaxHorizEffRatio is the stricter efficiency ratio threshold (horizEff < 0.45 * refEff)
	// used when filtering candidate-independent evalHours before the orientation search in CalculateWeatherSolar.
	// Because evalHours uses flat horizontal irradiance (tilt=0) before roof orientation is known, a stricter
	// 45% threshold ensures only severely throttled hours are dropped from orientation MAE scoring.
	dcCurtailmentEvalMaxHorizEffRatio = 0.45
)

// solarPredictionRecencyDecay represents the exponential recency decay factor applied as Pow(solarPredictionRecencyDecay, ageDays)
// when weighting historical solar telemetry points in calibrateSolarScaleFactor.
// Strict out-of-sample parameter sweeps across active production sites (with target evaluation days strictly excluded from history)
// evaluated decay factors from 1.00 down to 0.80. Out-of-sample forecast MAE forms a clear U-shaped curve, with 0.95 achieving
// optimal out-of-sample accuracy (All-Hours MAE: 0.4214 kWh, 9 AM Peak MAE: 0.8665 kWh). Decay values below 0.85 overfit to single-day weather noise.
// A decay factor of 0.95 weights telemetry from 7 days ago at ~70% and 14 days ago at ~50%, aligning with homeLoadPredictionRecencyDecay in energy.go.
var solarPredictionRecencyDecay = 0.95

// solarIrradianceSimilarityScale acts as the denominator in the exponential irradiance similarity weighting function:
// exp(-abs(histIrradiance - forecastIrradiance) / solarIrradianceSimilarityScale) applied when predicting future forecast hours.
// Strict out-of-sample backtesting across active production sites (where target evaluation days were strictly excluded from calibration history)
// evaluated scale values from 500 W/m² down to 2.0 W/m². Scales below 30 W/m² suffer from sample starvation out-of-sample (increasing error to >0.53 kWh),
// whereas a scale of 150.0 W/m² achieves optimal out-of-sample performance (lowest all-hours MAE of 0.4293 kWh and 9 AM peak MAE of 0.9003 kWh).
// It smoothly discounts vastly different weather days (e.g. 150 W/m² vs 650 W/m²) while retaining a robust historical sample size.
var solarIrradianceSimilarityScale = 150.0

// solarCloudSimilarityScale acts as the denominator in the exponential cloud cover similarity weighting function:
// exp(-abs(histCloudCover - forecastCloudCover) / solarCloudSimilarityScale) applied when predicting future forecast hours.
// Evaluated across 58 production sites and 24,700+ daylight hours, a scale of 25.0% achieves optimal forecast accuracy,
// reducing all-hours MAE by 6.9%, clear-day MAE by 9.5%, and overcast MAE by 5.5%.
var solarCloudSimilarityScale = 25.0

// WeatherSolar contains the solar generation data for a given hour.
type WeatherSolar struct {
	TSHourStart int64
	SolarKWH    float64
	SnowDepth   float64
	TempFactor  float64
	SnowFactor  float64
	Irradiance  float64
}

// CalculateSunPosition is an exported wrapper for calculateSunPosition.
func CalculateSunPosition(now time.Time, lat, lon float64) (float64, float64) {
	return calculateSunPosition(now, lat, lon)
}

// CalculateGTI is an exported wrapper for calculateGTI.
func CalculateGTI(dni, dhi, elevation, azimuth, tilt, panelAzimuth float64) float64 {
	return calculateGTI(dni, dhi, elevation, azimuth, tilt, panelAzimuth)
}

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

	if arrayAzimuth < 0 {
		// East-West Split.
		// The absolute value of arrayAzimuth is the fraction of panels facing East.
		// (e.g. -0.5 is 50% East / 50% West, -0.4 is 40% East / 60% West)
		eastFraction := -arrayAzimuth
		westFraction := 1.0 - eastFraction
		gtiEast := calculateGTI(dni, dhi, elevation, sunAzimuth, arrayTilt, 90.0)
		gtiWest := calculateGTI(dni, dhi, elevation, sunAzimuth, arrayTilt, 270.0)
		return eastFraction*gtiEast + westFraction*gtiWest
	}

	// Diffuse Component (Isotropic Sky View Model)
	const rad = math.Pi / 180.0
	tiltRad := arrayTilt * rad
	diffuse := dhi * (1.0 + math.Cos(tiltRad)) / 2.0

	aoi := calculateAngleOfIncidence(elevation, sunAzimuth, arrayTilt, arrayAzimuth)
	cosAOI := math.Cos(aoi)
	if cosAOI < 0 {
		cosAOI = 0.0
	}

	// Direct Beam Component
	direct := dni * cosAOI

	// We completely omit the Ground Reflected Component (albedo) because the training
	// and calibration loop divides the actual historical solar production by this
	// raw theoretical GTI. This learned hourly multiplier (efficiency) organically
	// absorbs the user's specific albedo (e.g. concrete, grass, gravel). Hardcoding
	// a generic 20% albedo here would force a standard albedo onto all installations
	// and distort the site-specific calibration scale.
	// TODO: Handle snow albedo dynamically in the future.

	return direct + diffuse
}

type sunPosition struct {
	Elevation float64
	Azimuth   float64
}

// calculateHourlyPositions computes 4 intra-hour sun positions (+7m30s, +22m30s, +37m30s, +52m30s)
// across the 1-hour window starting at hourStart.
func calculateHourlyPositions(hourStart time.Time, lat, lon float64) [4]sunPosition {
	var positions [4]sunPosition
	for i := 0; i < 4; i++ {
		offset := time.Duration(450+i*900) * time.Second
		el, az := calculateSunPosition(hourStart.Add(offset), lat, lon)
		positions[i] = sunPosition{Elevation: el, Azimuth: az}
	}
	return positions
}

// calculateHourlyGTI blends the Global Tilted Irradiance across the 4 intra-hour sun positions.
func calculateHourlyGTI(dni, dhi float64, positions [4]sunPosition, arrayTilt, arrayAzimuth float64) float64 {
	var sum float64
	for i := 0; i < 4; i++ {
		sum += calculateGTI(dni, dhi, positions[i].Elevation, positions[i].Azimuth, arrayTilt, arrayAzimuth)
	}
	return sum / 4.0
}

// calculateSolarClippingCap estimates the inverter's clipping limit in kWh based on historical production.
func calculateSolarClippingCap(ctx context.Context, history []types.EnergyStats) float64 {
	maxSolarKWH := 0.0
	for _, h := range history {
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
				if count > mostFreqCount || (count == mostFreqCount && val > mostFreqVal) {
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
			hourlyClippingCap = 0
			log.Ctx(ctx).DebugContext(
				ctx,
				"resetting hourly inverter clipping cap due to high max solar",
				slog.Float64("maxSolarKWH", maxSolarKWH),
				slog.Float64("hourlyClippingCap", hourlyClippingCap),
			)
		}
	}

	return hourlyClippingCap
}

type hourScaleFactorLog struct {
	HourOfDay  int     `json:"hourOfDay"`
	Efficiency float64 `json:"efficiency"`
	NumPoints  int     `json:"numPoints"`
}

// SolarCalibration holds the calibrated solar scale factors for each hour of the day.
type SolarCalibration struct {
	HourlyEffs           [24]float64
	StaticEff            float64
	StdDevRatio          float64
	RegularizationWeight float64
	hourScaleFactors     []hourScaleFactorLog
	validHoursCount      int
	daylightHoursCount   int
	cacheByHour          map[int][]historicalHourCache
}

// calibrateFromHistoricalCache computes static and per-hour solar scale factors from a sorted
// slice of historicalHourCache entries and a 24-hour daylight mask without map or per-hour slice allocations.
func calibrateFromHistoricalCache(
	allCache []historicalHourCache,
	daylightHours [24]bool,
	clippingCap float64,
	includeLog bool,
) SolarCalibration {
	var hourlyEffs [24]float64

	var staticEff float64
	var minClippedIrradiance float64
	if clippingCap > 0 {
		for i := range allCache {
			h := &allCache[i]
			if h.isValid && h.isClipped {
				if h.gti < minClippedIrradiance || minClippedIrradiance == 0 {
					minClippedIrradiance = h.gti
				}
			}
		}
	}

	var totalSolarKWH float64
	var totalTheoreticalIrrad float64

	for i := range allCache {
		h := &allCache[i]
		if h.isValid {
			effectiveIrradiance := h.gti
			if h.isClipped && minClippedIrradiance > 0 {
				effectiveIrradiance = min(h.gti, minClippedIrradiance)
			}

			totalSolarKWH += h.solarKWH * h.recencyWeight
			totalTheoreticalIrrad += effectiveIrradiance * h.tempFactor * h.snowFactor * h.recencyWeight
		}
	}

	if totalTheoreticalIrrad > 0 {
		staticEff = totalSolarKWH / totalTheoreticalIrrad
	}

	// Per-hour scale factor calibration accumulated directly from allCache in ascending timestamp order
	// so per-hour IEEE-754 sums match hour-bucket iteration without building intermediate bucket slices.
	type hourlyAcc struct {
		solarKWH float64
		denom    float64
		count    int
	}
	var efficienciesByHourOfDay [24]hourlyAcc

	for i := range allCache {
		h := &allCache[i]
		isClipped := h.isClipped && staticEff > 0 && h.denom*staticEff > clippingCap

		// Skip curtailed, snowy, and clipped hours so that the hourly shading factors
		// are learned from unconstrained and unblocked solar generation.
		if h.isValid && !isClipped && h.denom > 0 {
			if staticEff > 0 && (h.eff < h.minEffRatio*staticEff || h.eff > 1.5*staticEff) {
				continue
			}

			hOfDay := h.hourOfDay
			efficienciesByHourOfDay[hOfDay].solarKWH += h.solarKWH * h.recencyWeight
			efficienciesByHourOfDay[hOfDay].denom += h.denom * h.recencyWeight
			efficienciesByHourOfDay[hOfDay].count++
		}
	}

	var validEffs [24]float64
	var validMask [24]bool
	var validHoursCount int
	for h := 0; h < 24; h++ {
		acc := efficienciesByHourOfDay[h]
		if acc.count < 3 {
			continue
		}

		if acc.denom > 0 {
			mean := acc.solarKWH / acc.denom
			validEffs[h] = mean
			validMask[h] = true
			validHoursCount++
		}
	}

	var hourScaleFactors []hourScaleFactorLog
	if includeLog && validHoursCount > 0 {
		hourScaleFactors = make([]hourScaleFactorLog, 0, validHoursCount)
		for h := 0; h < 24; h++ {
			if validMask[h] {
				hourScaleFactors = append(hourScaleFactors, hourScaleFactorLog{
					HourOfDay:  h,
					Efficiency: validEffs[h],
					NumPoints:  efficienciesByHourOfDay[h].count,
				})
			}
		}
	}

	// If we have at least 4 valid hours, interpolate the rest.
	// Otherwise, fall back to using staticEff for all hours.
	var rawHourlyEffs [24]float64
	if validHoursCount >= 4 {
		rawHourlyEffs = interpolateHourlyEfficienciesArray(validEffs, validMask)
	} else {
		for h := 0; h < 24; h++ {
			rawHourlyEffs[h] = staticEff
		}
	}

	// 4. Adaptive Shading Regularization (Shrinkage)
	var daylightRatiosBuf [24]float64
	daylightRatios := daylightRatiosBuf[:0]
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
	// We use a lower threshold of 0.03 (down from 0.08) and an upper threshold of 0.11.
	// This prevents the model from completely regularizing out systematic, time-dependent
	// geometric variations (such as panel tilt/azimuth configuration errors, angle-of-incidence
	// Fresnel reflection losses, and local albedo) on roofs with little to no physical shading.
	// An efficiency standard deviation of >3% represents a real geometric signature, not noise.
	// If stdDevRatio <= 0.03, w = 0 (100% static, no shading/geometric variation)
	// If stdDevRatio >= 0.11, w = 1.0 (100% hourly, full shading/geometric variation)
	w := 0.0
	if stdDevRatio > 0.03 {
		w = (stdDevRatio - 0.03) / (0.11 - 0.03)
		if w > 1.0 {
			w = 1.0
		}
	}

	for h := 0; h < 24; h++ {
		val := rawHourlyEffs[h]
		if val == 0 {
			val = staticEff
		}

		// Note: We previously clamped values above 1.15*staticEff here to prevent unrealistic efficiencies,
		// but this was removed as it caused systematic underprediction during peak Sun hours on unshaded roofs.
		// The 0.5*staticEff to 1.5*staticEff hourly outlier filter in stage 3 is sufficient to handle weather anomalies.

		hourlyEffs[h] = w*val + (1.0-w)*staticEff
	}

	return SolarCalibration{
		HourlyEffs:           hourlyEffs,
		StaticEff:            staticEff,
		StdDevRatio:          stdDevRatio,
		RegularizationWeight: w,
		hourScaleFactors:     hourScaleFactors,
		validHoursCount:      validHoursCount,
		daylightHoursCount:   len(daylightRatios),
	}
}

// calibrateSolarScaleFactor calculates the calibrated solar scale factor (efficiency) by comparing
// historical actual solar production against theoretical irradiance.
func calibrateSolarScaleFactor(
	now time.Time,
	timeLoc *time.Location,
	weatherByHour map[int64]types.HourlyWeather,
	statsByHour map[int64]types.EnergyStats,
	clippingCap float64,
	getIrradiance func(hw types.HourlyWeather) float64,
) SolarCalibration {
	// We calculate a preliminary static scale factor (staticEff) first.
	// We'll use this static efficiency to perform the clipping detection,
	// and as a fallback if hourly calibration doesn't have enough data points.
	cacheByHour, allCache := buildHistoricalCache(now, timeLoc, weatherByHour, statsByHour, clippingCap, getIrradiance)

	// Find daylight hours to assess shading variation.
	// We define daylight hours as any hour of day where irradiance is significant (>= 50 W/m2)
	// in the weather forecast.
	var daylightHours [24]bool
	for _, hw := range weatherByHour {
		if getIrradiance(hw) >= 50 {
			daylightHours[hw.TSHourStart.In(timeLoc).Hour()] = true
		}
	}

	calib := calibrateFromHistoricalCache(allCache, daylightHours, clippingCap, true)
	calib.cacheByHour = cacheByHour
	return calib
}

// minSignificantCloudCoverPercent is the minimum cloud cover threshold required to trigger
// the optimization profile's cloud derate penalty. Minor cloud cover (< 10%) represents sparse
// or fair-weather clouds that typically do not cause substantial forecasting errors, whereas
// cloud cover >= 10% introduces material downside risk to the forecast.
const minSignificantCloudCoverPercent = 10.0

type indexedWeatherHour struct {
	ts         int64
	hw         types.HourlyWeather
	sunPos     [4]sunPosition
	localHour  int
	snowFactor float64
	horizGTI   float64
}

type indexedHistoryHour struct {
	ts              int64
	weatherIdx      int
	solarKWH        float64
	recencyWeight   float64
	isClipped       bool
	matchesHomeLoad bool
	baseValid       bool
}

type evalCandidatePoint struct {
	gti        float64
	tempFactor float64
}

type unconstrainedEffScratch struct {
	buf            []float64
	countsByHour   [24]int
	totalCap       int
	byHour         [24][]float64
	allUnconstrain []float64
}

func newUnconstrainedEffScratch(totalCap int, countsByHour [24]int) unconstrainedEffScratch {
	var s unconstrainedEffScratch
	s.totalCap = totalCap
	s.countsByHour = countsByHour
	if totalCap > 0 {
		s.buf = make([]float64, 2*totalCap)
	}
	s.reset()
	return s
}

func (s *unconstrainedEffScratch) reset() {
	if s.totalCap == 0 {
		s.allUnconstrain = nil
		s.byHour = [24][]float64{}
		return
	}
	s.allUnconstrain = s.buf[:0:s.totalCap]
	offset := s.totalCap
	for h := 0; h < 24; h++ {
		c := s.countsByHour[h]
		if c > 0 {
			s.byHour[h] = s.buf[offset : offset : offset+c]
			offset += c
		} else {
			s.byHour[h] = nil
		}
	}
}

func populateHistoricalCache(
	dst []historicalHourCache,
	hist []indexedHistoryHour,
	indexedWeather []indexedWeatherHour,
	candidateGTI []float64,
	scratch *unconstrainedEffScratch,
) {
	scratch.reset()
	for i := range hist {
		ih := &hist[i]
		w := &indexedWeather[ih.weatherIdx]
		gti := candidateGTI[ih.weatherIdx]
		tempFactor := calculateTempFactor(w.hw.TemperatureC, gti)
		snowFactor := w.snowFactor

		denom := gti * tempFactor * snowFactor
		var eff float64
		if denom > 0 {
			eff = ih.solarKWH / denom
		}

		minEffRatio := 0.5
		if gti < 200.0 {
			minEffRatio = 0.1
		}

		hOfDay := w.localHour
		isValid := gti >= 25 && tempFactor > 0 && ih.baseValid
		if isValid && !ih.matchesHomeLoad && gti >= unconstrainedBaselineMinGTI && ih.solarKWH > 0.1 {
			scratch.byHour[hOfDay] = append(scratch.byHour[hOfDay], eff)
			scratch.allUnconstrain = append(scratch.allUnconstrain, eff)
		}

		dst[i] = historicalHourCache{
			ts:              ih.ts,
			hourOfDay:       hOfDay,
			gti:             gti,
			cloudCover:      w.hw.CloudCoverPercent,
			solarKWH:        ih.solarKWH,
			tempFactor:      tempFactor,
			snowFactor:      snowFactor,
			recencyWeight:   ih.recencyWeight,
			denom:           denom,
			eff:             eff,
			minEffRatio:     minEffRatio,
			isClipped:       ih.isClipped,
			matchesHomeLoad: ih.matchesHomeLoad,
			isValid:         isValid,
		}
	}

	var refEffByHour [24]float64
	for h := 0; h < 24; h++ {
		if len(scratch.byHour[h]) >= 3 {
			slices.Sort(scratch.byHour[h])
			refEffByHour[h] = scratch.byHour[h][len(scratch.byHour[h])/2]
		}
	}
	var globalRefEff float64
	if len(scratch.allUnconstrain) >= 5 {
		slices.Sort(scratch.allUnconstrain)
		globalRefEff = scratch.allUnconstrain[len(scratch.allUnconstrain)/2]
	}

	for i := range dst {
		c := &dst[i]
		refEff := refEffByHour[c.hourOfDay]
		if refEff <= 0 && c.gti >= dcCurtailmentHighGTI {
			refEff = globalRefEff
		}
		isHeavyOvercast := c.cloudCover >= dcCurtailmentOvercastCloudPercent && c.gti < dcCurtailmentHighGTI
		if refEff > 0 && c.isValid && c.matchesHomeLoad &&
			!isHeavyOvercast && c.eff < dcCurtailmentMaxEffRatio*refEff {
			c.isValid = false
		}
	}
}

// CalculateWeatherSolar projects future solar generation based on forecast and historical calibration.
// It performs on-the-fly compass search to detect the optimal panel azimuth and tilt, then:
//  1. Calibrates robust hourly efficiency factors from filtered historical actual solar vs. irradiance data.
//  2. Tracks snow depth and melt attenuation.
//  3. Applies NOCT-based cell temperature estimation to correct for temperature-dependent efficiency.
//  4. Projects forward using the calibrated efficiency and optimal layout configurations.
//
// Returns a map keyed by Unix timestamp (seconds) of each weather hour's computed improvedSolar.
func CalculateWeatherSolar(
	ctx context.Context,
	now time.Time,
	history []types.EnergyStats,
	weather []types.Weather,
	locInfo types.SiteLocation,
	cloudDeratePercent float64,
) (map[int64]WeatherSolar, types.SimulationParams) {
	timeLoc := time.UTC
	if locInfo.TimeZone != "" && locInfo.TimeZone != "UTC" {
		if now.Location() != nil && now.Location().String() == locInfo.TimeZone {
			timeLoc = now.Location()
		} else if l, err := time.LoadLocation(locInfo.TimeZone); err == nil {
			timeLoc = l
		}
	}

	logger := log.Ctx(ctx)
	debugEnabled := logger.Enabled(ctx, slog.LevelDebug)

	clippingCap := calculateSolarClippingCap(ctx, history)

	// Pre-index unique weather hours once (keeping the latest HourlyWeather entry per Unix hour)
	// while simultaneously identifying the best fallback irradiance source (GTI if available, else GHI).
	totalForecastHours := 0
	for _, w := range weather {
		totalForecastHours += len(w.ForecastHours)
	}
	indexedWeather := make([]indexedWeatherHour, 0, totalForecastHours)
	weatherIdxByTS := make(map[int64]int, totalForecastHours)
	var anyGTI, anyGHI bool
	for _, w := range weather {
		for _, hw := range w.ForecastHours {
			if !anyGTI {
				if hw.GTI > 0 {
					anyGTI = true
				} else if hw.GHI > 0 {
					anyGHI = true
				}
			}
			ts := hw.TSHourStart.Unix()
			if idx, exists := weatherIdxByTS[ts]; exists {
				indexedWeather[idx].hw = hw
			} else {
				weatherIdxByTS[ts] = len(indexedWeather)
				indexedWeather = append(indexedWeather, indexedWeatherHour{
					ts: ts,
					hw: hw,
				})
			}
		}
	}
	useGTI := anyGTI || !anyGHI

	// Pre-compute localHour, snowFactor, 4 intra-hour sun positions (+7m30s, +22m30s, +37m30s, +52m30s),
	// and flat horizontal GTI once per unique weather hour.
	for i := range indexedWeather {
		w := &indexedWeather[i]
		w.localHour = w.hw.TSHourStart.In(timeLoc).Hour()
		w.snowFactor = calculateSnowFactor(w.hw.SnowDepthCM)
		if w.hw.DNI != 0 || w.hw.DHI != 0 {
			w.sunPos = calculateHourlyPositions(w.hw.TSHourStart, locInfo.Latitude, locInfo.Longitude)
			w.horizGTI = calculateHourlyGTI(w.hw.DNI, w.hw.DHI, w.sunPos, 0.0, 0.0)
		}
	}

	// Use completed prior days (prior to midnight today) for the orientation search so that
	// the determined panel azimuth and layout do not jitter or flip-flop intraday as new partial
	// hours arrive. If not enough prior history exists (e.g. brand new site on day 1, or unit tests),
	// fall back to all available history.
	var todayMidnight time.Time
	var hasTodayDaylight bool
	var usePriorOnly bool
	if !now.IsZero() {
		nowInLoc := now.In(timeLoc)
		todayMidnight = time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), 0, 0, 0, 0, timeLoc)
		var priorDaylightCount int
		for _, he := range history {
			if he.TSHourStart.Before(todayMidnight) {
				if he.SolarKWH > 0.5 {
					priorDaylightCount++
				}
			} else if he.SolarKWH > 0.02 {
				hasTodayDaylight = true
			}
		}
		usePriorOnly = priorDaylightCount >= 10
	}

	// Pre-index matched historical timestamps once in ascending order and precompute candidate-invariant
	// recencyWeight, clipping, curtailment, and load-matching flags.
	nowTruncUnix := now.Truncate(time.Hour).Unix()
	matchedHistory := make([]indexedHistoryHour, 0, min(len(history), len(indexedWeather)))
	var needsSortOrDedup bool
	var prevHistTS int64
	for _, st := range history {
		ts := st.TSHourStart.Unix()
		if ts == nowTruncUnix {
			continue
		}
		wIdx, ok := weatherIdxByTS[ts]
		if !ok {
			continue
		}
		if len(matchedHistory) > 0 && ts <= prevHistTS {
			needsSortOrDedup = true
		}
		prevHistTS = ts
		w := &indexedWeather[wIdx]
		matchedHistory = append(matchedHistory, indexedHistoryHour{
			ts:              ts,
			weatherIdx:      wIdx,
			solarKWH:        st.SolarKWH,
			recencyWeight:   calculateRecencyWeight(ts, now),
			isClipped:       clippingCap > 0 && st.SolarKWH >= clippingCap-clippingEps,
			matchesHomeLoad: isMatchingHomeLoad(st),
			baseValid:       st.SolarKWH > 0.02 && !isSolarCurtailed(st) && w.hw.SnowDepthCM <= 0.2,
		})
	}
	if needsSortOrDedup {
		sort.SliceStable(matchedHistory, func(i, j int) bool {
			return matchedHistory[i].ts < matchedHistory[j].ts
		})
		dedup := 0
		for i := range matchedHistory {
			if i+1 < len(matchedHistory) && matchedHistory[i+1].ts == matchedHistory[i].ts {
				continue // keep the latest EnergyStats entry per Unix hour
			}
			matchedHistory[dedup] = matchedHistory[i]
			dedup++
		}
		matchedHistory = matchedHistory[:dedup]
	}

	var histCountsByHour [24]int
	for i := range matchedHistory {
		histCountsByHour[indexedWeather[matchedHistory[i].weatherIdx].localHour]++
	}

	searchHist := matchedHistory
	if usePriorOnly {
		todayMidnightUnix := todayMidnight.Unix()
		priorLen := sort.Search(len(matchedHistory), func(i int) bool {
			return matchedHistory[i].ts >= todayMidnightUnix
		})
		searchHist = matchedHistory[:priorLen]
	}

	unconstrainedScratch := newUnconstrainedEffScratch(len(matchedHistory), histCountsByHour)

	// Pre-filter candidate-independent daylight evaluation points once before the orientation search:
	// 1. Efficiency: scoreCandidate is called ~18 times across candidate orientations; pre-computing
	//    horizontal GTI, snow factor, and localHour avoids repeating those calculations on every candidate.
	// 2. Fair comparison: every candidate azimuth/tilt must be scored against the exact same set of
	//    historical hours so an orientation cannot lower its MAE by dropping hours where it points away from the sun.
	// 3. Two-pass DC-coupled curtailment filtering (in place on evalHours):
	//    While buildHistoricalCache excludes DC-throttled hours (where MaxBatterySOC < 98% and solar was
	//    throttled down to match home load) from scale-factor calibration, we must also exclude those
	//    throttled hours from the MAE scoring set (evalHours). Otherwise, a true South/West array calibrated
	//    on unthrottled days (~6 kWh) would be heavily penalized in scoreCandidate when compared against
	//    throttled afternoon actuals (~1 kWh matching home load), skewing the orientation search toward East.
	type evalHourPoint struct {
		weatherIdx      int
		solarKWH        float64
		snowFactor      float64
		localHour       int
		horizEff        float64
		matchesHomeLoad bool
	}
	evalHours := make([]evalHourPoint, 0, len(searchHist))
	unconstrainedHorizEffsByHour := &unconstrainedScratch.byHour

	// Pass 1: Collect candidate-independent daylight hours into evalHours and record each hour's
	// horizontal efficiency (horizEff = SolarKWH / horizDenom, using flat tilt=0 irradiance).
	for _, he := range history {
		if usePriorOnly && !he.TSHourStart.Before(todayMidnight) {
			continue
		}
		if he.SolarKWH <= 0.5 {
			continue
		}
		ts := he.TSHourStart.Unix()
		if ts == nowTruncUnix {
			continue
		}
		wIdx, ok := weatherIdxByTS[ts]
		if !ok {
			continue
		}
		w := &indexedWeather[wIdx]
		if w.hw.SnowDepthCM > 0.2 {
			continue
		}
		horizGTI := w.horizGTI
		if horizGTI < 25 {
			continue
		}
		if isSolarCurtailed(he) {
			continue
		}
		snowFactor := w.snowFactor
		horizDenom := horizGTI * calculateTempFactor(w.hw.TemperatureC, horizGTI) * snowFactor
		if horizDenom <= 0 {
			continue
		}
		horizEff := he.SolarKWH / horizDenom
		localHour := w.localHour
		matchesHome := (he.MaxBatterySOC == 0 || he.MaxBatterySOC >= 15.0) && isMatchingHomeLoad(he)
		if !matchesHome && horizGTI >= unconstrainedBaselineMinGTI {
			unconstrainedHorizEffsByHour[localHour] = append(unconstrainedHorizEffsByHour[localHour], horizEff)
		}
		evalHours = append(evalHours, evalHourPoint{
			weatherIdx:      wIdx,
			solarKWH:        he.SolarKWH,
			snowFactor:      snowFactor,
			localHour:       localHour,
			horizEff:        horizEff,
			matchesHomeLoad: matchesHome,
		})
	}

	// Compute the median unconstrained horizontal efficiency for each hour of the day (when at least
	// 2 unconstrained historical days exist at that hour). We intentionally only compare against the
	// same localHour (no day-wide fallback) because horizontal efficiency on a tilted East/West roof
	// varies naturally by time of day.
	var refHorizEffByHour [24]float64
	for h := 0; h < 24; h++ {
		if len(unconstrainedHorizEffsByHour[h]) >= 2 {
			slices.Sort(unconstrainedHorizEffsByHour[h])
			refHorizEffByHour[h] = unconstrainedHorizEffsByHour[h][len(unconstrainedHorizEffsByHour[h])/2]
		}
	}

	// Pass 2: Filter evalHours in place by dropping points that were matching home load while producing at
	// < dcCurtailmentEvalMaxHorizEffRatio (45%) of that hour's normal unconstrained efficiency
	// (excluding heavy overcast hours with cloudCover >= 80% and GTI < 300).
	nEval := 0
	for _, pt := range evalHours {
		refEff := refHorizEffByHour[pt.localHour]
		hw := &indexedWeather[pt.weatherIdx].hw
		isHeavyOvercast := hw.CloudCoverPercent >= dcCurtailmentOvercastCloudPercent && hw.GTI < dcCurtailmentHighGTI
		if refEff > 0 && pt.matchesHomeLoad && !isHeavyOvercast && pt.horizEff < dcCurtailmentEvalMaxHorizEffRatio*refEff {
			continue
		}
		evalHours[nEval] = pt
		nEval++
	}
	evalHours = evalHours[:nEval]

	type evalResult struct {
		mae   float64
		calib SolarCalibration
		ok    bool
	}

	// minRegWeight tracks the lowest RegularizationWeight seen across all valid candidate orientations.
	minRegWeight := 1.0

	scoreCandidate := func(evalPts []evalCandidatePoint, calib *SolarCalibration, maxWeight float64) float64 {
		effs := calib.HourlyEffs
		// If this candidate calibrated with more hourly freedom (RegularizationWeight) than the
		// tightest candidate seen so far (maxWeight = minRegWeight), shrink its hourly efficiency
		// back toward StaticEff so all candidates are evaluated with the same maximum flexibility.
		if calib.RegularizationWeight > maxWeight && calib.RegularizationWeight > 0 {
			ratio := maxWeight / calib.RegularizationWeight
			for h := 0; h < 24; h++ {
				effs[h] = ratio*effs[h] + (1.0-ratio)*calib.StaticEff
			}
		}

		var sumAbsErr float64
		for i := range evalHours {
			pt := &evalHours[i]
			ep := evalPts[i]
			eff := effs[pt.localHour]

			pred := ep.gti * eff * ep.tempFactor * pt.snowFactor
			if clippingCap > 0 && pred > clippingCap {
				pred = clippingCap
			}
			sumAbsErr += math.Abs(pred - pt.solarKWH)
		}

		return sumAbsErr / float64(len(evalHours))
	}

	bestAzimuth := 180.0
	bestTilt := defaultSolarTilt
	bestMae := 99999.0
	southMae := 99999.0
	var gotCalib bool
	var bestCalib SolarCalibration
	var southCalib SolarCalibration
	var maeSplit float64
	var bestSplitAzimuth float64
	var bestSplitCalib SolarCalibration
	var hasEnoughSplit bool
	resFlat := evalResult{mae: 99999.0, ok: false}

	nWeather := len(indexedWeather)
	nAllHist := len(matchedHistory)
	nSearchHist := len(searchHist)

	var currGTI, bestGTI, bestSplitGTI []float64
	var currCacheAll, bestCacheAll, bestSplitCacheAll []historicalHourCache

	if len(evalHours) >= 5 {
		gtiBuf := make([]float64, 3*nWeather)
		currGTI = gtiBuf[0*nWeather : 1*nWeather]
		bestGTI = gtiBuf[1*nWeather : 2*nWeather]
		bestSplitGTI = gtiBuf[2*nWeather : 3*nWeather]

		cacheBuf := make([]historicalHourCache, 3*nAllHist)
		currCacheAll = cacheBuf[0*nAllHist : 1*nAllHist]
		bestCacheAll = cacheBuf[1*nAllHist : 2*nAllHist]
		bestSplitCacheAll = cacheBuf[2*nAllHist : 3*nAllHist]

		evalPointBuf := make([]evalCandidatePoint, 4*nEval)
		currEval := evalPointBuf[0*nEval : 1*nEval]
		bestEval := evalPointBuf[1*nEval : 2*nEval]
		southEval := evalPointBuf[2*nEval : 3*nEval]
		bestSplitEval := evalPointBuf[3*nEval : 4*nEval]

		// Helper to evaluate daylight MAE for a candidate azimuth and tilt using reusable GTI and cache buffers.
		evaluateAzimuthWithTilt := func(testAz, testTilt float64) evalResult {
			var daylightHours [24]bool
			if testTilt == 0.0 && testAz == 0.0 {
				for i := range indexedWeather {
					gti := indexedWeather[i].horizGTI
					currGTI[i] = gti
					if gti >= 50 {
						daylightHours[indexedWeather[i].localHour] = true
					}
				}
			} else {
				for i := range indexedWeather {
					w := &indexedWeather[i]
					var gti float64
					if w.hw.DNI != 0 || w.hw.DHI != 0 {
						gti = calculateHourlyGTI(w.hw.DNI, w.hw.DHI, w.sunPos, testTilt, testAz)
					}
					currGTI[i] = gti
					if gti >= 50 {
						daylightHours[w.localHour] = true
					}
				}
			}

			currSearchCache := currCacheAll[:nSearchHist]
			populateHistoricalCache(currSearchCache, searchHist, indexedWeather, currGTI, &unconstrainedScratch)
			calib := calibrateFromHistoricalCache(currSearchCache, daylightHours, clippingCap, debugEnabled)
			if calib.StaticEff <= 0 {
				return evalResult{mae: 99999.0, calib: calib, ok: false}
			}

			for i := range evalHours {
				wIdx := evalHours[i].weatherIdx
				gti := currGTI[wIdx]
				currEval[i] = evalCandidatePoint{
					gti:        gti,
					tempFactor: calculateTempFactor(indexedWeather[wIdx].hw.TemperatureC, gti),
				}
			}

			// If this candidate has a flatter hourly efficiency profile (lower RegularizationWeight)
			// than previous candidates, tighten minRegWeight and re-score the previously stored
			// bestMae and southMae under the stricter regularization cap so the comparison stays fair.
			// Only allow a candidate to tighten minRegWeight if it had at least 4 valid daylight hours
			// to actually measure hourly efficiency variation (rather than falling back to constant staticEff).
			if calib.validHoursCount >= 4 && calib.daylightHoursCount >= 4 && calib.RegularizationWeight < minRegWeight {
				minRegWeight = calib.RegularizationWeight
				if gotCalib {
					bestMae = scoreCandidate(bestEval, &bestCalib, minRegWeight)
					if southCalib.StaticEff > 0 {
						southMae = scoreCandidate(southEval, &southCalib, minRegWeight)
					}
				}
			}

			mae := scoreCandidate(currEval, &calib, minRegWeight)
			return evalResult{mae: mae, calib: calib, ok: true}
		}

		evaluateAzimuth := func(testAz float64) evalResult {
			return evaluateAzimuthWithTilt(testAz, defaultSolarTilt)
		}

		acceptBest := func(az, tilt float64, res evalResult) {
			bestAzimuth = az
			bestTilt = tilt
			bestMae = res.mae
			bestCalib = res.calib
			gotCalib = true
			bestGTI, currGTI = currGTI, bestGTI
			bestCacheAll, currCacheAll = currCacheAll, bestCacheAll
			bestEval, currEval = currEval, bestEval
		}

		if resSouth := evaluateAzimuth(180.0); resSouth.ok {
			copy(southEval, currEval)
			southMae = resSouth.mae
			southCalib = resSouth.calib
			acceptBest(180.0, defaultSolarTilt, resSouth)

			if resEast := evaluateAzimuth(90.0); resEast.ok {
				if resEast.mae < southMae {
					// East-facing branch: search Southeast (135°), Southwest (225°), Northeast (45°), and North (0°)
					acceptBest(90.0, defaultSolarTilt, resEast)

					for _, az := range []float64{135.0, 225.0, 45.0, 0.0} {
						res := evaluateAzimuth(az)
						if res.ok && res.mae < bestMae {
							acceptBest(az, defaultSolarTilt, res)
						}
					}
				} else {
					// West-facing or South-facing check: check West (270°)
					if resWest := evaluateAzimuth(270.0); resWest.ok {
						if resWest.mae < southMae {
							// West-facing branch: search Southwest (225°), Southeast (135°), Northwest (315°), and North (0°)
							acceptBest(270.0, defaultSolarTilt, resWest)

							for _, az := range []float64{225.0, 135.0, 315.0, 0.0} {
								res := evaluateAzimuth(az)
								if res.ok && res.mae < bestMae {
									acceptBest(az, defaultSolarTilt, res)
								}
							}
						} else {
							// South beat both pure East (90°) and pure West (270°).
							// Always evaluate Southeast (135°) and Southwest (225°).
							for _, az := range []float64{135.0, 225.0} {
								res := evaluateAzimuth(az)
								if res.ok && res.mae < bestMae {
									acceptBest(az, defaultSolarTilt, res)
								}
							}
						}
					}
				}
			}
		}

		// Evaluate if the site has an East-West split configuration.
		acceptBestSplit := func(az float64, res evalResult) {
			maeSplit = res.mae
			bestSplitAzimuth = az
			bestSplitCalib = res.calib
			hasEnoughSplit = true
			bestSplitGTI, currGTI = currGTI, bestSplitGTI
			bestSplitCacheAll, currCacheAll = currCacheAll, bestSplitCacheAll
			bestSplitEval, currEval = currEval, bestSplitEval
		}

		if res5050 := evaluateAzimuth(-0.5); res5050.ok {
			acceptBestSplit(-0.5, res5050)

			// Only search asymmetric splits if the symmetric split is a reasonably good fit.
			// We proceed if the 50/50 split is within 5% of bestMae or southMae, or within 0.15 kWh absolute MAE tolerance
			// to handle near-zero bestMae values stable in simulated test data.
			if res5050.mae <= bestMae*1.05 || res5050.mae <= southMae*1.05 || res5050.mae <= bestMae+0.15 {
				for _, frac := range []float64{0.3, 0.7} {
					prevMinWeight := minRegWeight
					res := evaluateAzimuth(-frac)
					if minRegWeight < prevMinWeight {
						maeSplit = scoreCandidate(bestSplitEval, &bestSplitCalib, minRegWeight)
					}
					if res.ok && res.mae < maeSplit {
						acceptBestSplit(-frac, res)
					}
				}
			}
		}

		// Evaluate if the site has a Flat (0° tilt) panel configuration, reusing precomputed horizontal GTI.
		prevMinWeightFlat := minRegWeight
		resFlat = evaluateAzimuthWithTilt(0.0, 0.0)
		if hasEnoughSplit && minRegWeight < prevMinWeightFlat {
			maeSplit = scoreCandidate(bestSplitEval, &bestSplitCalib, minRegWeight)
		}

		// Choose the configuration with the absolute lowest MAE.
		if hasEnoughSplit && maeSplit < bestMae {
			bestMae = maeSplit
			bestAzimuth = bestSplitAzimuth
			bestTilt = defaultSolarTilt
			bestCalib = bestSplitCalib
			gotCalib = true
			bestGTI, bestSplitGTI = bestSplitGTI, bestGTI
			bestCacheAll, bestSplitCacheAll = bestSplitCacheAll, bestCacheAll
		}

		if resFlat.ok && resFlat.mae < bestMae {
			bestMae = resFlat.mae
			bestTilt = 0.0
			bestAzimuth = 180.0
			bestCalib = resFlat.calib
			gotCalib = true
			bestGTI, currGTI = currGTI, bestGTI
			bestCacheAll, currCacheAll = currCacheAll, bestCacheAll
		}
	}

	if debugEnabled {
		logger.DebugContext(
			ctx,
			"determined best solar azimuth and tilt on-the-fly",
			slog.Float64("bestAzimuth", bestAzimuth),
			slog.Float64("bestTilt", bestTilt),
			slog.Float64("bestMAE", bestMae),
			slog.Float64("minRegWeight", minRegWeight),
			slog.Float64("southMAE", southMae),
			slog.Float64("splitMAE", maeSplit),
			slog.Float64("bestSplitAzimuth", bestSplitAzimuth),
			slog.Float64("flatMAE", resFlat.mae),
			slog.Int("evalHoursCount", len(evalHours)),
			slog.Float64("clippingCapKWH", clippingCap),
		)
	}

	getForecastIrr := func(hw types.HourlyWeather) float64 {
		if useGTI {
			return hw.GTI
		}
		return hw.GHI
	}

	var finalGTI []float64
	var finalCache []historicalHourCache
	var bucketBacking []historicalHourCache
	var hourlyEffs [24]float64
	var finalCalib SolarCalibration

	if !gotCalib {
		finalGTI = bestGTI
		if finalGTI == nil && nWeather > 0 {
			finalGTI = make([]float64, nWeather)
		}
		var finalDaylightHours [24]bool
		for i := range indexedWeather {
			w := &indexedWeather[i]
			var gti float64
			if w.hw.DNI > 0 || w.hw.DHI > 0 {
				gti = calculateHourlyGTI(w.hw.DNI, w.hw.DHI, w.sunPos, bestTilt, bestAzimuth)
			} else {
				gti = getForecastIrr(w.hw)
			}
			finalGTI[i] = gti
			if gti >= 50 {
				finalDaylightHours[w.localHour] = true
			}
		}

		if currCacheAll != nil {
			finalCache = currCacheAll[:nAllHist]
			bucketBacking = bestCacheAll[:nAllHist]
		} else if nAllHist > 0 {
			pairBuf := make([]historicalHourCache, 2*nAllHist)
			finalCache = pairBuf[:nAllHist]
			bucketBacking = pairBuf[nAllHist:]
		}
		populateHistoricalCache(finalCache, matchedHistory, indexedWeather, finalGTI, &unconstrainedScratch)
		finalCalib = calibrateFromHistoricalCache(finalCache, finalDaylightHours, clippingCap, debugEnabled)
		hourlyEffs = finalCalib.HourlyEffs
	} else if usePriorOnly && hasTodayDaylight {
		// Calibrate across all history (including today's completed daylight hours) using the chosen orientation's GTI.
		finalGTI = bestGTI
		var finalDaylightHours [24]bool
		for i := range indexedWeather {
			w := &indexedWeather[i]
			if w.hw.DNI <= 0 && w.hw.DHI <= 0 {
				finalGTI[i] = getForecastIrr(w.hw)
			}
			if finalGTI[i] >= 50 {
				finalDaylightHours[w.localHour] = true
			}
		}
		finalCache = currCacheAll[:nAllHist]
		bucketBacking = bestCacheAll[:nAllHist]
		populateHistoricalCache(finalCache, matchedHistory, indexedWeather, finalGTI, &unconstrainedScratch)
		finalCalib = calibrateFromHistoricalCache(finalCache, finalDaylightHours, clippingCap, debugEnabled)
		hourlyEffs = finalCalib.HourlyEffs
	} else {
		finalGTI = bestGTI
		for i := range indexedWeather {
			w := &indexedWeather[i]
			if w.hw.DNI <= 0 && w.hw.DHI <= 0 {
				finalGTI[i] = getForecastIrr(w.hw)
			}
		}
		finalCalib = bestCalib
		hourlyEffs = bestCalib.HourlyEffs
		finalCache = bestCacheAll[:nSearchHist]
		bucketBacking = currCacheAll[:nSearchHist]
	}

	if debugEnabled {
		logger.DebugContext(
			ctx,
			"calibrated per-hour scale factors",
			slog.Any("hourlyEfficiencies", hourlyEffs),
			slog.Float64("stdDevRatio", finalCalib.StdDevRatio),
			slog.Float64("regularizationWeight", finalCalib.RegularizationWeight),
			slog.Float64("staticEfficiency", finalCalib.StaticEff),
			slog.Any("hourScaleFactors", finalCalib.hourScaleFactors),
		)
	}

	// Build hour-of-day buckets once from a single contiguous backing slice for finalCalib.
	var buckets [24][]historicalHourCache
	hasCachedHours := len(finalCache) > 0
	if hasCachedHours {
		var bucketCounts [24]int
		for i := range finalCache {
			bucketCounts[finalCache[i].hourOfDay]++
		}
		offset := 0
		for h := 0; h < 24; h++ {
			if bucketCounts[h] > 0 {
				buckets[h] = bucketBacking[offset : offset : offset+bucketCounts[h]]
				offset += bucketCounts[h]
			}
		}
		for i := range finalCache {
			h := finalCache[i].hourOfDay
			buckets[h] = append(buckets[h], finalCache[i])
		}
	}

	results := make(map[int64]WeatherSolar, nWeather)
	for i := range indexedWeather {
		w := &indexedWeather[i]
		gti := finalGTI[i]
		tempFactor := calculateTempFactor(w.hw.TemperatureC, gti)
		snowDepth := w.hw.SnowDepthCM
		snowFactor := w.snowFactor

		var improved float64
		if gti != 0 {
			localHour := w.localHour
			eff := calculateSimilarityEfficiencyByHour(gti, w.hw.CloudCoverPercent, &buckets, hasCachedHours, localHour, finalCalib.StaticEff, hourlyEffs[localHour])

			unclipped := gti * eff * tempFactor * snowFactor
			if cloudDeratePercent > 0 && w.hw.CloudCoverPercent >= minSignificantCloudCoverPercent {
				cloudFactor := w.hw.CloudCoverPercent / 100.0
				derateFactor := max(0.0, 1.0-(cloudDeratePercent/100.0)*cloudFactor)
				unclipped *= derateFactor
			}

			improved = unclipped
			if clippingCap > 0 && improved > clippingCap {
				improved = clippingCap
			}
		}

		results[w.ts] = WeatherSolar{
			TSHourStart: w.ts,
			SolarKWH:    improved,
			SnowDepth:   snowDepth,
			TempFactor:  tempFactor,
			SnowFactor:  snowFactor,
			Irradiance:  gti,
		}
	}

	var sumEff float64
	var countEff int
	for _, eff := range hourlyEffs {
		if eff > 0 {
			sumEff += eff
			countEff++
		}
	}
	var avgEff float64
	if countEff > 0 {
		avgEff = sumEff / float64(countEff)
	}

	params := types.SimulationParams{
		ClippingCapKWH:         clippingCap,
		PanelAzimuth:           bestAzimuth,
		PanelTilt:              bestTilt,
		AverageSolarEfficiency: avgEff,
	}
	return results, params
}

func interpolateHourlyEfficienciesArray(validEffs [24]float64, validMask [24]bool) [24]float64 {
	var result [24]float64
	hasAny := false
	for h := 0; h < 24; h++ {
		if validMask[h] {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return result
	}

	for h := 0; h < 24; h++ {
		if validMask[h] {
			result[h] = validEffs[h]
			continue
		}

		// Find closest valid hour going backward (circularly)
		var prevHour int
		var prevDist int
		for d := 1; d <= 24; d++ {
			p := (h - d + 24) % 24
			if validMask[p] {
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
			if validMask[n] {
				nextHour = n
				nextDist = d
				break
			}
		}

		if prevDist > 0 && nextDist > 0 {
			totalDist := float64(prevDist + nextDist)
			valPrev := validEffs[prevHour]
			valNext := validEffs[nextHour]
			// Linear interpolation
			result[h] = (float64(nextDist)*valPrev + float64(prevDist)*valNext) / totalDist
		}
	}
	return result
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

// isMatchingHomeLoad returns true if solar generation appears to be load-following
// (throttled by a DC-coupled MPPT to match home consumption with no grid export, e.g., when
// the battery is held in Standby or solar export is disabled during negative electricity prices).
// By conservation of energy (Solar - Home = (Export - Import) + (BattCharged - BattUsed)),
// checking SolarKWH in [HomeKWH - 0.15, HomeKWH + 0.30] with near-zero import and export already
// ensures net battery charging is near zero. However, we also explicitly require BatteryChargedKWH <= 0.5
// and BatteryUsedKWH <= 0.15 to exclude hours where the battery was discharging to cover a solar
// deficit (meaning solar was below home load, not throttled) or where the battery charged in one
// part of the hour and discharged an equal amount in another part of the hour.
func isMatchingHomeLoad(stats types.EnergyStats) bool {
	return stats.HomeKWH > 0.1 &&
		stats.GridExportKWH <= 0.1 &&
		stats.GridImportKWH <= 0.15 &&
		stats.BatteryChargedKWH <= 0.5 &&
		stats.BatteryUsedKWH <= 0.15 &&
		stats.SolarKWH >= stats.HomeKWH-0.15 &&
		stats.SolarKWH <= stats.HomeKWH+0.30
}

// historicalHourCache holds pre-computed weather factors and efficiencies for a single historical hour
// to eliminate redundant tempFactor, snowFactor, and recencyWeight recalculations.
type historicalHourCache struct {
	ts              int64
	hourOfDay       int
	gti             float64
	cloudCover      float64
	solarKWH        float64
	tempFactor      float64
	snowFactor      float64
	recencyWeight   float64
	denom           float64
	eff             float64
	minEffRatio     float64
	isClipped       bool
	matchesHomeLoad bool
	// isValid is true if the historical point has unconstrained, non-snowy, non-curtailed generation (gti >= 25, tempFactor > 0, hasSolar, !isCurtailed, !isSnowy).
	isValid bool
}

// calculateTempFactor calculates PV cell temperature and returns the temperature loss scaling factor.
func calculateTempFactor(tempC, irradiance float64) float64 {
	tCell := tempC + (irradiance/800.0)*(nominalOperatingCellTemperature-20.0)
	tCell = min(max(tCell, -40), 80)
	return 1.0 - (tCell-25.0)*powerTemperatureCoefficient
}

// calculateRecencyWeight computes the exponential recency weight Pow(solarPredictionRecencyDecay, ageDays) for a historical timestamp.
func calculateRecencyWeight(ts int64, now time.Time) float64 {
	if solarPredictionRecencyDecay >= 1.0 {
		return 1.0
	}
	ageDays := max(0.0, now.Sub(time.Unix(ts, 0)).Hours()/24.0)
	if ageDays <= 0 {
		return 1.0
	}
	return math.Pow(solarPredictionRecencyDecay, ageDays)
}

const clippingEps = 0.05 // kWh epsilon for detecting a plateau

// buildHistoricalCache pre-computes weather factors and efficiencies for all historical hours in a single pass.
func buildHistoricalCache(
	now time.Time,
	timeLoc *time.Location,
	weatherByHour map[int64]types.HourlyWeather,
	statsByHour map[int64]types.EnergyStats,
	clippingCap float64,
	getIrradiance func(types.HourlyWeather) float64,
) (map[int][]historicalHourCache, []historicalHourCache) {
	cacheByHour := make(map[int][]historicalHourCache)
	currentHourTs := now.Truncate(time.Hour).Unix()

	// Sort matched historical timestamps in ascending order so floating-point accumulations
	// in calibrateSolarScaleFactor and computeSimilarityEfficiency are 100% deterministic.
	sortedTS := make([]int64, 0, len(statsByHour))
	for ts := range statsByHour {
		if ts == currentHourTs {
			continue
		}
		if _, ok := weatherByHour[ts]; ok {
			sortedTS = append(sortedTS, ts)
		}
	}
	sort.Slice(sortedTS, func(i, j int) bool { return sortedTS[i] < sortedTS[j] })

	allCache := make([]historicalHourCache, 0, len(sortedTS))
	var unconstrainedEffsByHour [24][]float64
	var allUnconstrainedEffs []float64

	for _, ts := range sortedTS {
		hw := weatherByHour[ts]
		stats := statsByHour[ts]

		gti := getIrradiance(hw)
		tempFactor := calculateTempFactor(hw.TemperatureC, gti)
		snowDepth := hw.SnowDepthCM
		snowFactor := calculateSnowFactor(snowDepth)
		recencyWeight := calculateRecencyWeight(ts, now)

		denom := gti * tempFactor * snowFactor
		var eff float64
		if denom > 0 {
			eff = stats.SolarKWH / denom
		}

		// Skip hourly outliers where weather forecast severely mismatched actual production.
		// For low-irradiance hours (< 200 W/m²), allow lower physical conversion ratios down to 0.1 * staticEff
		// (reflecting morning haze, low incidence angle, and MPPT startup losses) so true low morning efficiencies are learned.
		minEffRatio := 0.5
		if gti < 200.0 {
			minEffRatio = 0.1
		}

		hOfDay := time.Unix(ts, 0).In(timeLoc).Hour()
		isValid := gti >= 25 && tempFactor > 0 && stats.SolarKWH > 0.02 && !isSolarCurtailed(stats) && snowDepth <= 0.2
		isClipped := clippingCap > 0 && stats.SolarKWH >= clippingCap-clippingEps
		matchesHome := isMatchingHomeLoad(stats)
		// Do not exclude isClipped hours from unconstrainedEffsByHour: on systems that hit inverter AC
		// clipping on every clear midday, excluding isClipped would leave only overcast days in the
		// midday baseline, whereas AC-clipped full output is still far above DC home-load throttling.
		if isValid && !matchesHome && gti >= unconstrainedBaselineMinGTI && stats.SolarKWH > 0.1 {
			unconstrainedEffsByHour[hOfDay] = append(unconstrainedEffsByHour[hOfDay], eff)
			allUnconstrainedEffs = append(allUnconstrainedEffs, eff)
		}

		entry := historicalHourCache{
			ts:              ts,
			hourOfDay:       hOfDay,
			gti:             gti,
			cloudCover:      hw.CloudCoverPercent,
			solarKWH:        stats.SolarKWH,
			tempFactor:      tempFactor,
			snowFactor:      snowFactor,
			recencyWeight:   recencyWeight,
			denom:           denom,
			eff:             eff,
			minEffRatio:     minEffRatio,
			isClipped:       isClipped,
			matchesHomeLoad: matchesHome,

			// isValid is true if all physical preconditions for unconstrained solar calibration are satisfied.
			// Skip curtailed hours (when battery is full and we aren't exporting, solar is throttled) to avoid skewing physical calibration.
			// Skip snowy hours (snow coverage blocks solar panels, obscuring true efficiency).
			// Include low-light early morning / late evening solar generation (e.g. 6:00-7:30 AM generation of 0.05-0.45 kWh).
			// The previous static 0.5 kWh threshold discarded 100% of valid early morning telemetry,
			// forcing fallback interpolation to leak high midday efficiency defaults into morning hours.
			isValid: isValid,
		}
		allCache = append(allCache, entry)
	}

	// If there is a significant number of unconstrained hours, detect DC-coupled solar
	// curtailment where MaxBatterySOC < 98% (e.g., Standby mode or <98% charge limit with export disabled)
	// by checking if an hour's solar was matching home load while its efficiency was significantly
	// lower (< dcCurtailmentMaxEffRatio, 65%) than the median unconstrained efficiency for that hour of day
	// (or overall if < 3 samples at that hour).
	// Heavy overcast hours (cloudCover >= dcCurtailmentOvercastCloudPercent with gti < dcCurtailmentHighGTI)
	// are not flagged so genuine overcast telemetry is preserved.
	var refEffByHour [24]float64
	for h := 0; h < 24; h++ {
		if len(unconstrainedEffsByHour[h]) >= 3 {
			slices.Sort(unconstrainedEffsByHour[h])
			refEffByHour[h] = unconstrainedEffsByHour[h][len(unconstrainedEffsByHour[h])/2]
		}
	}
	var globalRefEff float64
	if len(allUnconstrainedEffs) >= 5 {
		slices.Sort(allUnconstrainedEffs)
		globalRefEff = allUnconstrainedEffs[len(allUnconstrainedEffs)/2]
	}

	for i := range allCache {
		hOfDay := allCache[i].hourOfDay
		refEff := refEffByHour[hOfDay]
		// Only fall back to the day-wide globalRefEff (which is dominated by midday hours)
		// when irradiance is high (gti >= dcCurtailmentHighGTI), avoiding false positives on shaded
		// or low-angle morning/evening hours that have fewer than 3 historical samples.
		if refEff <= 0 && allCache[i].gti >= dcCurtailmentHighGTI {
			refEff = globalRefEff
		}
		isHeavyOvercast := allCache[i].cloudCover >= dcCurtailmentOvercastCloudPercent && allCache[i].gti < dcCurtailmentHighGTI
		if refEff > 0 && allCache[i].isValid && allCache[i].matchesHomeLoad &&
			!isHeavyOvercast && allCache[i].eff < dcCurtailmentMaxEffRatio*refEff {
			allCache[i].isValid = false
		}
		cacheByHour[hOfDay] = append(cacheByHour[hOfDay], allCache[i])
	}
	return cacheByHour, allCache
}

// computeSimilarityEfficiency calculates the similarity-weighted efficiency from one or more slices of historical hour entries.
// Returns the weighted efficiency and the number of matching historical sample hours with similar cloud cover.
func computeSimilarityEfficiency(
	forecastIrr float64,
	forecastCloud float64,
	cachedHours []historicalHourCache,
	staticEff float64,
	extraSlices ...[]historicalHourCache,
) (float64, int) {
	if solarIrradianceSimilarityScale <= 0 {
		return 0, 0
	}

	var sumSolar, sumDenom float64
	var count, validCount int

	accumulateSlice := func(slice []historicalHourCache) {
		for _, h := range slice {
			isClipped := h.isClipped && staticEff > 0 && h.eff < staticEff
			if !h.isValid || isClipped || h.denom <= 0 {
				continue
			}
			if staticEff > 0 && (h.eff < h.minEffRatio*staticEff || h.eff > 1.5*staticEff) {
				continue
			}

			// Weight past telemetry points exponentially based on irradiance similarity (|histIrr - forecastIrr|).
			// Tight irradiance matching isolates foggy/cloudy historical days from clear sunny days,
			// preventing clear-sky efficiency leakage into overcast forecasts.
			irrDiff := math.Abs(h.gti - forecastIrr)
			simWeight := math.Exp(-irrDiff / solarIrradianceSimilarityScale)

			// Weight past telemetry points exponentially based on cloud cover similarity (|histCloud - forecastCloud|).
			// When forecasting clear days, it discounts historical diffuse/overcast days; when forecasting overcast days,
			// it isolates cloudy historical days to prevent clear-sky high efficiencies from overpredicting cloudy generation.
			similarCloud := true
			if solarCloudSimilarityScale > 0 {
				cloudDiff := math.Abs(h.cloudCover - forecastCloud)
				cloudWeight := math.Exp(-cloudDiff / solarCloudSimilarityScale)
				simWeight *= cloudWeight
				similarCloud = cloudDiff <= solarCloudSimilarityScale
			}

			// Weight past telemetry points exponentially based on recency (age in days).
			// Gives higher weight to recent atmospheric and seasonal solar trend changes (e.g. multi-day coastal fog).
			totalWeight := simWeight * h.recencyWeight

			sumSolar += h.solarKWH * totalWeight
			sumDenom += h.denom * totalWeight
			validCount++
			if similarCloud {
				count++
			}
		}
	}

	accumulateSlice(cachedHours)
	for _, s := range extraSlices {
		accumulateSlice(s)
	}

	if validCount >= 3 && sumDenom > 0 {
		return sumSolar / sumDenom, count
	}
	return 0, count
}

// calculateSimilarityEfficiency calculates an irradiance-, cloud-cover-, and recency-similarity weighted efficiency ratio
// for a target forecast hour by querying pre-computed historical telemetry points. If the target hour does not have
// at least 3 matching historical samples with similar cloud cover, it falls back to pooling historical samples from
// adjacent daylight hours (±1 hour, then ±2 hours) before falling back to localHour's overall efficiency or hourlyEffs.
func calculateSimilarityEfficiency(
	forecastIrr float64,
	forecastCloud float64,
	cacheByHour map[int][]historicalHourCache,
	localHour int,
	staticEff float64,
	fallbackEff float64,
) float64 {
	if solarIrradianceSimilarityScale <= 0 || len(cacheByHour) == 0 {
		return fallbackEff
	}

	// 1. Try target hour directly. If localHour already has >= 3 valid historical samples (eff > 0),
	// return its irradiance-, cloud-, and recency-weighted efficiency directly without mixing adjacent
	// hours that have different solar geometry or physical shading profiles.
	eff, _ := computeSimilarityEfficiency(forecastIrr, forecastCloud, cacheByHour[localHour], staticEff)
	if eff > 0 {
		return eff
	}

	// 2. Fallback: pool adjacent hours (±1 hour, then ±2 hours) without slice allocations
	// when localHour lacks 3 valid historical points. Prefer the narrow ±1 hour window if it has
	// at least 3 similar-cloud-cover samples (count1 >= 3) before expanding to ±2 hours.
	prevHour := (localHour - 1 + 24) % 24
	nextHour := (localHour + 1) % 24
	eff1, count1 := computeSimilarityEfficiency(forecastIrr, forecastCloud, cacheByHour[localHour], staticEff, cacheByHour[prevHour], cacheByHour[nextHour])
	if count1 >= 3 {
		return eff1
	}

	// 3. Fallback: pool adjacent hours (±2 hours)
	prev2 := (localHour - 2 + 24) % 24
	next2 := (localHour + 2) % 24
	eff2, count2 := computeSimilarityEfficiency(forecastIrr, forecastCloud, cacheByHour[localHour], staticEff, cacheByHour[prevHour], cacheByHour[nextHour], cacheByHour[prev2], cacheByHour[next2])
	if count2 >= 3 {
		return eff2
	}

	if eff1 > 0 {
		return eff1
	}
	if eff2 > 0 {
		return eff2
	}

	// 4. Ultimate fallback to calibrated hourly efficiency
	return fallbackEff
}

func calculateSimilarityEfficiencyByHour(
	forecastIrr float64,
	forecastCloud float64,
	buckets *[24][]historicalHourCache,
	hasCachedHours bool,
	localHour int,
	staticEff float64,
	fallbackEff float64,
) float64 {
	if solarIrradianceSimilarityScale <= 0 || !hasCachedHours {
		return fallbackEff
	}

	eff, _ := computeSimilarityEfficiency(forecastIrr, forecastCloud, buckets[localHour], staticEff)
	if eff > 0 {
		return eff
	}

	prevHour := (localHour - 1 + 24) % 24
	nextHour := (localHour + 1) % 24
	eff1, count1 := computeSimilarityEfficiency(forecastIrr, forecastCloud, buckets[localHour], staticEff, buckets[prevHour], buckets[nextHour])
	if count1 >= 3 {
		return eff1
	}

	prev2 := (localHour - 2 + 24) % 24
	next2 := (localHour + 2) % 24
	eff2, count2 := computeSimilarityEfficiency(forecastIrr, forecastCloud, buckets[localHour], staticEff, buckets[prevHour], buckets[nextHour], buckets[prev2], buckets[next2])
	if count2 >= 3 {
		return eff2
	}

	if eff1 > 0 {
		return eff1
	}
	if eff2 > 0 {
		return eff2
	}

	return fallbackEff
}
