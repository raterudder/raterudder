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
//  1. Calibrating a robust efficiency factor from filtered historical actual solar vs. irradiance data.
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
	const (
		noct        = 45.0   // Nominal Operating Cell Temperature
		tempCoeff   = 0.0035 // typical power temperature coefficient
		clippingEps = 0.05   // kWh epsilon for detecting a plateau
	)

	// 1. Index historical actual solar by hour timestamp for O(1) lookup.
	statsByHour := make(map[int64]types.EnergyStats, len(history))
	maxSolarKWH := 0.0
	for _, h := range history {
		statsByHour[h.TSHourStart.Unix()] = h
		if h.SolarKWH > maxSolarKWH {
			maxSolarKWH = h.SolarKWH
		}
	}

	// Learning the Clipping Cap:
	// Identify days where production plateaus at the peak.
	var clippingCap float64
	if maxSolarKWH > 1.0 { // only consider clipping if production is significant
		usageCounts := make(map[int]int)
		for _, s := range history {
			if s.SolarKWH > maxSolarKWH*0.9 {
				// Round to 1 decimal place to group similar peak values
				val := int(math.Round(s.SolarKWH * 10))
				usageCounts[val]++
			}
		}

		// If any value in the top 30% of max occurs frequently, it might be a cap.
		mostFreqVal := -1
		mostFreqCount := 0
		for val, count := range usageCounts {
			if count > mostFreqCount {
				mostFreqVal = val
				mostFreqCount = count
			}
		}

		// only if we've seen this for at least 6 hours
		if mostFreqVal > 0 && mostFreqCount >= 6 {
			clippingCap = float64(mostFreqVal) / 10.0
			log.Ctx(ctx).DebugContext(
				ctx,
				"learned inverter clipping cap",
				slog.Float64("capKWH", clippingCap),
				slog.Int("occurrences", mostFreqCount),
			)
		} else {
			log.Ctx(ctx).DebugContext(
				ctx,
				"found no inverter clipping cap",
				slog.Float64("maxSolarKWH", maxSolarKWH),
				slog.Float64("frequentKWH", float64(mostFreqVal)/10.0),
				slog.Int("occurrences", mostFreqCount),
			)
		}
	}

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

	var minClippedIrradiance float64
	if clippingCap > 0 {
		for ts, stats := range statsByHour {
			if stats.SolarKWH >= clippingCap-clippingEps {
				if hw, ok := weatherByHour[ts]; ok {
					var irr float64
					if useGTI {
						irr = hw.GTI
					} else {
						irr = hw.GHI
					}
					if irr > 0 && (irr < minClippedIrradiance || minClippedIrradiance == 0) {
						minClippedIrradiance = irr
					}
				}
			}
		}
		if minClippedIrradiance > 0 {
			log.Ctx(ctx).DebugContext(
				ctx,
				"learned min clipped irradiance",
				slog.Float64("minClippedIrradiance", minClippedIrradiance),
			)
		}
	}

	type dailyAcc struct {
		solarKWH         float64
		theoreticalIrrad float64
		count            int
	}
	dailyData := make(map[string]*dailyAcc)

	timeLoc, err := time.LoadLocation(locInfo.TimeZone)
	if err != nil {
		timeLoc = time.UTC
	}

	results := make(map[int64]WeatherSolar, len(timestamps))

	for _, ts := range timestamps {
		hw := weatherByHour[ts]
		h := WeatherSolar{TSHourStart: ts}

		// Use GTI from Open-Meteo when available; fallback to GHI.
		if useGTI {
			h.Irradiance = hw.GTI
		} else {
			h.Irradiance = hw.GHI
		}

		// Estimated cell temperature via NOCT model:
		//   Tcell = Tamb + (Irradiance / 800) * (NOCT - 20)
		h.TCell = hw.TemperatureC + (h.Irradiance/800.0)*(noct-20.0)

		// Use direct snow depth from API which is an average from this hour to the next
		h.SnowDepth = hw.SnowDepthCM

		// Calculate factor based on cell difference compared to STC (25C cell temp).
		h.TCell = min(max(h.TCell, -40), 80)
		h.TempFactor = 1.0 - (h.TCell-25)*tempCoeff

		// > 5 cm: opaque layer, essentially zero generation.
		// > 0.2 cm: partial blockage, ~90 % reduction.
		// > 0 cm: light dusting, ~30 % reduction.
		h.SnowFactor = 1.0
		switch {
		case h.SnowDepth > 5:
			h.SnowFactor = 0.0
		case h.SnowDepth > 0.2:
			h.SnowFactor = 0.1
		case h.SnowDepth > 0:
			h.SnowFactor = 0.70
		}

		// Collect historical efficiency ratios that pass several quality filters:
		// 1. Minimum Irradiance: Ignore low light / noise levels.
		// 2. Curtailment: Ignore hours if the battery was nearly full and no export occurred.
		// 3. Snow: Ignore hours with significant snow accumulation.
		// 4. Clipping: If clipped, clamp irradiance to minClippedIrradiance.
		// 5. Skip the current hour to avoid partial actuals distorting learned daily efficiencies
		if stats, ok := statsByHour[ts]; ok {
			isCurtailed := stats.GridExportKWH <= 0.1 && stats.MaxBatterySOC >= 98.0
			isSnowy := h.SnowDepth > 0.2
			hasSolar := stats.SolarKWH > 0.5
			isClipped := clippingCap > 0 && stats.SolarKWH >= clippingCap-clippingEps
			currentHour := ts == now.Truncate(time.Hour).Unix()

			// Filters: Irradiance < 25. Ignore low light.
			if h.Irradiance >= 25 && h.TempFactor > 0 && hasSolar && !isCurtailed && !isSnowy && !currentHour {
				effectiveIrradiance := h.Irradiance
				if isClipped && minClippedIrradiance > 0 {
					effectiveIrradiance = math.Min(h.Irradiance, minClippedIrradiance)
				}

				dayStr := time.Unix(ts, 0).In(timeLoc).Format("2006-01-02")
				if dailyData[dayStr] == nil {
					dailyData[dayStr] = &dailyAcc{}
				}
				dailyData[dayStr].solarKWH += stats.SolarKWH
				dailyData[dayStr].theoreticalIrrad += effectiveIrradiance * h.TempFactor * h.SnowFactor
				dailyData[dayStr].count++
			}
		}

		results[ts] = h
	}

	// 3. Determine robust efficiency by analyzing daily points
	var dailyEfficiencies []float64
	for _, acc := range dailyData {
		if acc.theoreticalIrrad > 0 && acc.count > 0 {
			dailyEfficiencies = append(dailyEfficiencies, acc.solarKWH/acc.theoreticalIrrad)
		}
	}

	var finalEff float64

	if len(dailyEfficiencies) > 0 {
		sort.Float64s(dailyEfficiencies)

		// simple averaging
		var sum float64
		for _, e := range dailyEfficiencies {
			sum += e
		}
		finalEff = sum / float64(len(dailyEfficiencies))
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"calculated gti scale factor",
		slog.Float64("finalEfficiency", finalEff),
		slog.Any("dailyEfficiencies", dailyEfficiencies),
	)

	// 4. Project solar generation for every weather hour using the calibrated
	// efficiency unless we don't have any efficiency data.
	if finalEff > 0 {
		for _, ts := range timestamps {
			h := results[ts]
			if h.Irradiance > 0 {
				h.UnclippedSolar = h.Irradiance * finalEff * h.TempFactor * h.SnowFactor
				h.ImprovedSolar = h.UnclippedSolar
				if clippingCap > 0 && h.ImprovedSolar > clippingCap {
					h.ImprovedSolar = clippingCap
				}
			}
			results[ts] = h
		}
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
