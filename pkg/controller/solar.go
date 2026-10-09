package controller

import (
	"context"
	"log/slog"
	"math"
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
	daylightHoursCount   int
	cacheByHour          map[int][]historicalHourCache
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
	var hourlyEffs [24]float64

	// We calculate a preliminary static scale factor (staticEff) first.
	// We'll use this static efficiency to perform the clipping detection,
	// and as a fallback if hourly calibration doesn't have enough data points.
	cacheByHour, allCache := buildHistoricalCache(now, timeLoc, weatherByHour, statsByHour, clippingCap, getIrradiance)

	var staticEff float64
	var minClippedIrradiance float64
	if clippingCap > 0 {
		for _, h := range allCache {
			if h.isValid && h.isClipped {
				if h.gti < minClippedIrradiance || minClippedIrradiance == 0 {
					minClippedIrradiance = h.gti
				}
			}
		}
	}

	var totalSolarKWH float64
	var totalTheoreticalIrrad float64

	for _, h := range allCache {
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

	// Per-hour scale factor calibration
	type hourlyAcc struct {
		solarKWH float64
		denom    float64
		count    int
	}
	var efficienciesByHourOfDay [24]hourlyAcc

	for hOfDay := 0; hOfDay < 24; hOfDay++ {
		for _, h := range cacheByHour[hOfDay] {
			isClipped := h.isClipped && staticEff > 0 && h.denom*staticEff > clippingCap

			// Skip curtailed, snowy, and clipped hours so that the hourly shading factors
			// are learned from unconstrained and unblocked solar generation.
			if h.isValid && !isClipped && h.denom > 0 {
				if staticEff > 0 && (h.eff < h.minEffRatio*staticEff || h.eff > 1.5*staticEff) {
					continue
				}

				efficienciesByHourOfDay[hOfDay].solarKWH += h.solarKWH * h.recencyWeight
				efficienciesByHourOfDay[hOfDay].denom += h.denom * h.recencyWeight
				efficienciesByHourOfDay[hOfDay].count++
			}
		}
	}

	validHours := make(map[int]float64)
	var hourScaleFactors []hourScaleFactorLog
	for h := 0; h < 24; h++ {
		acc := efficienciesByHourOfDay[h]
		if acc.count < 3 {
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
		daylightHoursCount:   len(daylightRatios),
		cacheByHour:          cacheByHour,
	}
}

// minSignificantCloudCoverPercent is the minimum cloud cover threshold required to trigger
// the optimization profile's cloud derate penalty. Minor cloud cover (< 10%) represents sparse
// or fair-weather clouds that typically do not cause substantial forecasting errors, whereas
// cloud cover >= 10% introduces material downside risk to the forecast.
const minSignificantCloudCoverPercent = 10.0

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
	// Collect all forecast hours across all days in weather
	var forecastHours []types.HourlyWeather
	for _, w := range weather {
		forecastHours = append(forecastHours, w.ForecastHours...)
	}

	timeLoc := time.UTC
	if locInfo.TimeZone != "" && locInfo.TimeZone != "UTC" {
		if now.Location() != nil && now.Location().String() == locInfo.TimeZone {
			timeLoc = now.Location()
		} else if l, err := time.LoadLocation(locInfo.TimeZone); err == nil {
			timeLoc = l
		}
	}

	clippingCap := calculateSolarClippingCap(ctx, history)

	weatherByHour := make(map[int64]types.HourlyWeather, len(forecastHours))
	for _, hw := range forecastHours {
		weatherByHour[hw.TSHourStart.Unix()] = hw
	}

	statsByHour := make(map[int64]types.EnergyStats, len(history))
	for _, st := range history {
		statsByHour[st.TSHourStart.Unix()] = st
	}

	// Pre-compute 4 intra-hour sun positions (+7m30s, +22m30s, +37m30s, +52m30s) for all unique forecast hours
	// so sunrise and sunset hours blend across the hour rather than relying on a single midpoint.
	sunPosByHour := make(map[int64][4]sunPosition, len(weatherByHour))
	for ts, hw := range weatherByHour {
		sunPosByHour[ts] = calculateHourlyPositions(hw.TSHourStart, locInfo.Latitude, locInfo.Longitude)
	}

	calcHourlyGTI := func(hw types.HourlyWeather, positions [4]sunPosition, tilt, az float64) float64 {
		return calculateHourlyGTI(hw.DNI, hw.DHI, positions, tilt, az)
	}

	// Use completed prior days (prior to midnight today) for the orientation search so that
	// the determined panel azimuth and layout do not jitter or flip-flop intraday as new partial
	// hours arrive. If not enough prior history exists (e.g. brand new site on day 1, or unit tests),
	// fall back to all available history.
	searchHistory := history
	searchStatsByHour := statsByHour
	var hasTodayDaylight bool
	if !now.IsZero() {
		nowInLoc := now.In(timeLoc)
		todayMidnight := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), 0, 0, 0, 0, timeLoc)
		var priorHistory []types.EnergyStats
		var priorDaylightCount int
		for _, he := range history {
			if he.TSHourStart.Before(todayMidnight) {
				priorHistory = append(priorHistory, he)
				if he.SolarKWH > 0.5 {
					priorDaylightCount++
				}
			} else if he.SolarKWH > 0.02 {
				hasTodayDaylight = true
			}
		}
		if priorDaylightCount >= 10 {
			searchHistory = priorHistory
			searchStatsByHour = make(map[int64]types.EnergyStats, len(searchHistory))
			for _, st := range searchHistory {
				searchStatsByHour[st.TSHourStart.Unix()] = st
			}
		}
	}

	// Pre-filter candidate-independent daylight evaluation points once before the orientation search:
	// 1. Efficiency: scoreCandidate is called ~18 times across candidate orientations; pre-computing
	//    horizontal GTI, snow factor, and localHour avoids repeating those calculations on every candidate.
	// 2. Fair comparison: every candidate azimuth/tilt must be scored against the exact same set of
	//    historical hours so an orientation cannot lower its MAE by dropping hours where it points away from the sun.
	// 3. Two-pass DC-coupled curtailment filtering (rawEvalHours -> evalHours):
	//    While buildHistoricalCache excludes DC-throttled hours (where MaxBatterySOC < 98% and solar was
	//    throttled down to match home load) from scale-factor calibration, we must also exclude those
	//    throttled hours from the MAE scoring set (evalHours). Otherwise, a true South/West array calibrated
	//    on unthrottled days (~6 kWh) would be heavily penalized in scoreCandidate when compared against
	//    throttled afternoon actuals (~1 kWh matching home load), skewing the orientation search toward East.
	type evalHourPoint struct {
		ts              int64
		hw              types.HourlyWeather
		solarKWH        float64
		snowFactor      float64
		localHour       int
		horizEff        float64
		matchesHomeLoad bool
	}
	nowTruncUnix := now.Truncate(time.Hour).Unix()
	var rawEvalHours []evalHourPoint
	var unconstrainedHorizEffsByHour [24][]float64

	// Pass 1: Collect candidate-independent daylight hours into rawEvalHours and record each hour's
	// horizontal efficiency (horizEff = SolarKWH / horizDenom, using flat tilt=0 irradiance).
	// Even though we don't know the roof's true tilt/azimuth yet, at any fixed localHour of the day
	// the sun's position is consistent across days, so comparing horizEff against the median horizEff
	// at that same localHour reliably identifies hours where production collapsed.
	for _, he := range searchHistory {
		if he.SolarKWH <= 0.5 {
			continue
		}
		ts := he.TSHourStart.Unix()
		if ts == nowTruncUnix {
			continue
		}
		hw, ok := weatherByHour[ts]
		if !ok || hw.SnowDepthCM > 0.2 {
			continue
		}
		// Filter using candidate-independent horizontal irradiance (tilt=0) so every
		// candidate azimuth/tilt is evaluated across the exact same set of daylight hours,
		// and orientations that point away from the sun during active production hours are penalized.
		horizGTI := calcHourlyGTI(hw, sunPosByHour[ts], 0.0, 0.0)
		if horizGTI < 25 {
			continue
		}
		if isSolarCurtailed(he) {
			continue
		}
		snowFactor := calculateSnowFactor(hw.SnowDepthCM)
		// horizDenom is the temperature- and snow-adjusted horizontal irradiance (at tilt=0).
		horizDenom := horizGTI * calculateTempFactor(hw.TemperatureC, horizGTI) * snowFactor
		if horizDenom <= 0 {
			continue
		}
		// horizEff is the effective system conversion ratio relative to horizontal irradiance.
		horizEff := he.SolarKWH / horizDenom
		localHour := he.TSHourStart.In(timeLoc).Hour()
		// Check whether solar appeared to be tracking home load with near-zero grid/battery flow
		// (requiring MaxBatterySOC >= 15% when SOC telemetry is present so empty-battery morning hours aren't flagged).
		matchesHome := (he.MaxBatterySOC == 0 || he.MaxBatterySOC >= 15.0) && isMatchingHomeLoad(he)
		// If this hour was NOT matching home load and had meaningful sunlight, include it in the
		// unconstrained baseline for this hour of the day.
		if !matchesHome && horizGTI >= unconstrainedBaselineMinGTI {
			unconstrainedHorizEffsByHour[localHour] = append(unconstrainedHorizEffsByHour[localHour], horizEff)
		}
		rawEvalHours = append(rawEvalHours, evalHourPoint{
			ts:              ts,
			hw:              hw,
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
			sort.Float64s(unconstrainedHorizEffsByHour[h])
			refHorizEffByHour[h] = unconstrainedHorizEffsByHour[h][len(unconstrainedHorizEffsByHour[h])/2]
		}
	}

	// Pass 2: Build the final evalHours slice by filtering out rawEvalHours points that were
	// matching home load while producing at < dcCurtailmentEvalMaxHorizEffRatio (45%) of that hour's
	// normal unconstrained efficiency (excluding heavy overcast hours with cloudCover >= 80% and GTI < 300).
	evalHours := make([]evalHourPoint, 0, len(rawEvalHours))
	for _, pt := range rawEvalHours {
		refEff := refHorizEffByHour[pt.localHour]
		isHeavyOvercast := pt.hw.CloudCoverPercent >= dcCurtailmentOvercastCloudPercent && pt.hw.GTI < dcCurtailmentHighGTI
		if refEff > 0 && pt.matchesHomeLoad && !isHeavyOvercast && pt.horizEff < dcCurtailmentEvalMaxHorizEffRatio*refEff {
			continue
		}
		evalHours = append(evalHours, pt)
	}

	type evalResult struct {
		mae   float64
		calib SolarCalibration
		ok    bool
	}

	// minRegWeight tracks the lowest RegularizationWeight seen across all valid candidate orientations.
	//
	// Why this is needed:
	// In calibrateSolarScaleFactor, RegularizationWeight (0.0 to 1.0) controls how much the model is
	// allowed to vary efficiency hour-by-hour (HourlyEffs[h]) vs. using a single constant efficiency
	// (StaticEff) for the entire day. It is computed from the coefficient of variation (CV) of the
	// raw hourly efficiencies (SolarKWH / GTI).
	//
	// When we test the TRUE physical orientation of an unshaded array, the modeled GTI curve matches
	// the panels' geometry all day, so SolarKWH / GTI is nearly constant across hours -> low CV ->
	// low RegularizationWeight (close to 0.0, i.e., 1 degree of freedom).
	// When we test a WRONG orientation (e.g., testing West or East-West split on a South roof), the
	// geometric mismatch makes SolarKWH / GTI appear artificially high in the morning and low in the
	// afternoon -> high CV -> high RegularizationWeight (up to 1.0, i.e., 24 per-hour degrees of freedom).
	//
	// Without capping, a wrong orientation with 24 per-hour multipliers can absorb its own geometric
	// error and overfit the historical data to beat the true orientation (which only used 1 static
	// multiplier). Capping every candidate at minRegWeight forces all orientations to compete using
	// at most the hourly flexibility required by the cleanest-fitting orientation.
	minRegWeight := 1.0

	scoreCandidate := func(testAz, testTilt float64, calib SolarCalibration, maxWeight float64) float64 {
		var sumAbsErr float64
		for _, pt := range evalHours {
			gti := calcHourlyGTI(pt.hw, sunPosByHour[pt.ts], testTilt, testAz)
			tempFactor := calculateTempFactor(pt.hw.TemperatureC, gti)

			eff := calib.HourlyEffs[pt.localHour]
			// If this candidate calibrated with more hourly freedom (RegularizationWeight) than the
			// tightest candidate seen so far (maxWeight = minRegWeight), shrink its hourly efficiency
			// back toward StaticEff so all candidates are evaluated with the same maximum flexibility.
			if calib.RegularizationWeight > maxWeight && calib.RegularizationWeight > 0 {
				ratio := maxWeight / calib.RegularizationWeight
				eff = ratio*eff + (1.0-ratio)*calib.StaticEff
			}

			pred := gti * eff * tempFactor * pt.snowFactor
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

	// Helper to evaluate daylight MAE for a candidate azimuth and tilt using hourly calibrated efficiencies
	evaluateAzimuthWithTilt := func(testAz, testTilt float64) evalResult {
		if len(evalHours) < 5 {
			return evalResult{mae: 99999.0, ok: false}
		}
		getIrr := func(hw types.HourlyWeather) float64 {
			ts := hw.TSHourStart.Unix()
			return calcHourlyGTI(hw, sunPosByHour[ts], testTilt, testAz)
		}
		calib := calibrateSolarScaleFactor(now, timeLoc, weatherByHour, searchStatsByHour, clippingCap, getIrr)
		if calib.StaticEff <= 0 {
			return evalResult{mae: 99999.0, calib: calib, ok: false}
		}

		// If this candidate has a flatter hourly efficiency profile (lower RegularizationWeight)
		// than previous candidates, tighten minRegWeight and re-score the previously stored
		// bestMae and southMae under the stricter regularization cap so the comparison stays fair.
		// Only allow a candidate to tighten minRegWeight if it had at least 4 valid daylight hours
		// to actually measure hourly efficiency variation (rather than falling back to constant staticEff).
		if len(calib.hourScaleFactors) >= 4 && calib.daylightHoursCount >= 4 && calib.RegularizationWeight < minRegWeight {
			minRegWeight = calib.RegularizationWeight
			if gotCalib {
				bestMae = scoreCandidate(bestAzimuth, bestTilt, bestCalib, minRegWeight)
				if southCalib.StaticEff > 0 {
					southMae = scoreCandidate(180.0, defaultSolarTilt, southCalib, minRegWeight)
				}
			}
		}

		mae := scoreCandidate(testAz, testTilt, calib, minRegWeight)
		return evalResult{mae: mae, calib: calib, ok: true}
	}

	evaluateAzimuth := func(testAz float64) evalResult {
		return evaluateAzimuthWithTilt(testAz, defaultSolarTilt)
	}

	if resSouth := evaluateAzimuth(180.0); resSouth.ok {
		bestAzimuth = 180.0
		bestMae = resSouth.mae
		southMae = resSouth.mae
		bestCalib = resSouth.calib
		southCalib = resSouth.calib
		gotCalib = true

		if resEast := evaluateAzimuth(90.0); resEast.ok {
			if resEast.mae < southMae {
				// East-facing branch: search Southeast (135°), Southwest (225°), Northeast (45°), and North (0°)
				bestAzimuth = 90.0
				bestMae = resEast.mae
				bestCalib = resEast.calib

				for _, az := range []float64{135.0, 225.0, 45.0, 0.0} {
					res := evaluateAzimuth(az)
					if res.ok && res.mae < bestMae {
						bestMae = res.mae
						bestAzimuth = az
						bestCalib = res.calib
					}
				}
			} else {
				// West-facing or South-facing check: check West (270°)
				if resWest := evaluateAzimuth(270.0); resWest.ok {
					if resWest.mae < southMae {
						// West-facing branch: search Southwest (225°), Southeast (135°), Northwest (315°), and North (0°)
						bestAzimuth = 270.0
						bestMae = resWest.mae
						bestCalib = resWest.calib

						for _, az := range []float64{225.0, 135.0, 315.0, 0.0} {
							res := evaluateAzimuth(az)
							if res.ok && res.mae < bestMae {
								bestMae = res.mae
								bestAzimuth = az
								bestCalib = res.calib
							}
						}
					} else {
						// South beat both pure East (90°) and pure West (270°).
						// Always evaluate Southeast (135°) and Southwest (225°).
						for _, az := range []float64{135.0, 225.0} {
							res := evaluateAzimuth(az)
							if res.ok && res.mae < bestMae {
								bestMae = res.mae
								bestAzimuth = az
								bestCalib = res.calib
							}
						}
					}
				}
			}
		}
	}

	// Evaluate if the site has an East-West split configuration.
	// We represent East-West split using a negative sentinel azimuth where the absolute value
	// is the fraction of the array facing East (e.g. -0.5 represents 50% East / 50% West).
	// To minimize CPU overhead, we first evaluate the symmetric 50/50 split (-0.5).
	// If the 50/50 split is promising (within 5% of the best single direction or South), we evaluate
	// asymmetric splits: -0.3 (30% East / 70% West) and -0.7 (70% East / 30% West).
	var maeSplit float64
	var bestSplitAzimuth float64
	var bestSplitCalib SolarCalibration
	var hasEnoughSplit bool
	if res5050 := evaluateAzimuth(-0.5); res5050.ok {
		maeSplit = res5050.mae
		bestSplitAzimuth = -0.5
		bestSplitCalib = res5050.calib
		hasEnoughSplit = true

		// Only search asymmetric splits if the symmetric split is a reasonably good fit.
		// We proceed if the 50/50 split is within 5% of bestMae or southMae, or within 0.15 kWh absolute MAE tolerance
		// to handle near-zero bestMae values stable in simulated test data.
		if res5050.mae <= bestMae*1.05 || res5050.mae <= southMae*1.05 || res5050.mae <= bestMae+0.15 {
			for _, frac := range []float64{0.3, 0.7} {
				prevMinWeight := minRegWeight
				res := evaluateAzimuth(-frac)
				if minRegWeight < prevMinWeight {
					maeSplit = scoreCandidate(bestSplitAzimuth, defaultSolarTilt, bestSplitCalib, minRegWeight)
				}
				if res.ok && res.mae < maeSplit {
					maeSplit = res.mae
					bestSplitAzimuth = -frac
					bestSplitCalib = res.calib
				}
			}
		}
	}

	// Evaluate if the site has a Flat (0° tilt) panel configuration.
	prevMinWeightFlat := minRegWeight
	resFlat := evaluateAzimuthWithTilt(0.0, 0.0)
	if hasEnoughSplit && minRegWeight < prevMinWeightFlat {
		maeSplit = scoreCandidate(bestSplitAzimuth, defaultSolarTilt, bestSplitCalib, minRegWeight)
	}

	// Choose the configuration with the absolute lowest MAE.
	if hasEnoughSplit && maeSplit < bestMae {
		bestMae = maeSplit
		bestAzimuth = bestSplitAzimuth
		bestTilt = defaultSolarTilt
		bestCalib = bestSplitCalib
		gotCalib = true
	}

	if resFlat.ok && resFlat.mae < bestMae {
		bestMae = resFlat.mae
		bestTilt = 0.0
		bestAzimuth = 180.0
		bestCalib = resFlat.calib
		gotCalib = true
	}

	log.Ctx(ctx).DebugContext(
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

	// Identify the best irradiance source (GTI if available, fallback to GHI)
	var anyGTI bool
	var anyGHI bool
	for _, hw := range forecastHours {
		if hw.GTI > 0 {
			anyGTI = true
			break
		}
		if hw.GHI > 0 {
			anyGHI = true
		}
	}
	useGTI := anyGTI || !anyGHI

	getForecastIrr := func(hw types.HourlyWeather) float64 {
		if useGTI {
			return hw.GTI
		}
		return hw.GHI
	}

	gtiByHour := make(map[int64]float64, len(weatherByHour))
	for ts, hw := range weatherByHour {
		if hw.DNI > 0 || hw.DHI > 0 {
			gtiByHour[ts] = calcHourlyGTI(hw, sunPosByHour[ts], bestTilt, bestAzimuth)
		} else {
			gtiByHour[ts] = getForecastIrr(hw)
		}
	}

	var hourlyEffs [24]float64
	var finalCalib SolarCalibration
	if !gotCalib || (len(searchStatsByHour) != len(statsByHour) && hasTodayDaylight) {
		// Calibrate across all history (including today's completed daylight hours) using the chosen orientation's GTI
		finalCalib = calibrateSolarScaleFactor(now, timeLoc, weatherByHour, statsByHour, clippingCap, func(hw types.HourlyWeather) float64 {
			return gtiByHour[hw.TSHourStart.Unix()]
		})
		hourlyEffs = finalCalib.HourlyEffs
	} else {
		finalCalib = bestCalib
		hourlyEffs = bestCalib.HourlyEffs
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"calibrated per-hour scale factors",
		slog.Any("hourlyEfficiencies", hourlyEffs),
		slog.Float64("stdDevRatio", finalCalib.StdDevRatio),
		slog.Float64("regularizationWeight", finalCalib.RegularizationWeight),
		slog.Float64("staticEfficiency", finalCalib.StaticEff),
		slog.Any("hourScaleFactors", finalCalib.hourScaleFactors),
	)

	cacheByHour := finalCalib.cacheByHour

	results := make(map[int64]WeatherSolar)
	for _, hw := range forecastHours {
		ts := hw.TSHourStart.Unix()
		gti := gtiByHour[ts]
		tempFactor := calculateTempFactor(hw.TemperatureC, gti)

		snowDepth := hw.SnowDepthCM
		snowFactor := calculateSnowFactor(snowDepth)

		localHour := hw.TSHourStart.In(timeLoc).Hour()
		eff := calculateSimilarityEfficiency(gti, hw.CloudCoverPercent, cacheByHour, localHour, finalCalib.StaticEff, hourlyEffs[localHour])

		unclipped := gti * eff * tempFactor * snowFactor
		if cloudDeratePercent > 0 && hw.CloudCoverPercent >= minSignificantCloudCoverPercent {
			cloudFactor := hw.CloudCoverPercent / 100.0
			derateFactor := max(0.0, 1.0-(cloudDeratePercent/100.0)*cloudFactor)
			unclipped *= derateFactor
		}

		improved := unclipped
		if clippingCap > 0 && improved > clippingCap {
			improved = clippingCap
		}

		results[ts] = WeatherSolar{
			TSHourStart: ts,
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
			sort.Float64s(unconstrainedEffsByHour[h])
			refEffByHour[h] = unconstrainedEffsByHour[h][len(unconstrainedEffsByHour[h])/2]
		}
	}
	var globalRefEff float64
	if len(allUnconstrainedEffs) >= 5 {
		sort.Float64s(allUnconstrainedEffs)
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
