package controller

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// SimHour represents one hour of simulated energy state.
type SimHour struct {
	TS                      time.Time   `json:"ts"`
	Hour                    int         `json:"hour"`
	NetLoadSolarKWH         float64     `json:"netLoadSolarKWH"`
	ClampedNetLoadSolarKWH  float64     `json:"clampedNetLoadSolarKWH"`
	GridChargeDollarsPerKWH float64     `json:"gridChargeDollarsPerKWH"`
	SolarOppDollarsPerKWH   float64     `json:"solarOppDollarsPerKWH"`
	AvgHomeLoadKWH          float64     `json:"avgHomeLoadKWH"`
	PredictedSolarKWH       float64     `json:"predictedSolarKWH"`
	BatteryKWH              float64     `json:"batteryKWH"`
	BatteryKWHIfStandby     float64     `json:"batteryKWHIfStandby"`
	BatteryCapacityKWH      float64     `json:"batteryCapacityKWH"`
	BatteryReserveKWH       float64     `json:"batteryReserveKWH"`
	TotalBatteryDeficitKWH  float64     `json:"totalBatteryDeficitKWH"`
	TodaySolarTrend         float64     `json:"todaySolarTrend"`
	HitCapacity             bool        `json:"hitCapacity"`
	HitSolarCapacity        bool        `json:"hitSolarCapacity"`
	HitDeficit              bool        `json:"hitDeficit"`
	Price                   types.Price `json:"price"`
}

// SimulateState builds a 24-hour simulation of energy state and prices.
// It returns the simulated hours and the current available battery energy (kWh).
func (c *Controller) SimulateState(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	settings types.Settings,
) []SimHour {
	capacityKWH := currentStatus.BatteryCapacityKWH
	currentSOC := currentStatus.BatterySOC
	// simulate battery energy over the 24 hours
	simEnergy := capacityKWH * (currentSOC / 100.0)
	simStandbyEnergy := simEnergy
	var deficitKWH float64

	// Build Energy Model
	model := c.buildHourlyEnergyModel(ctx, now, history, settings)
	minKWH := capacityKWH * (settings.MinBatterySOC / 100.0)

	// simulate our energy state and prices for the next 24 hours
	simData := make([]SimHour, 0, 24)

	// build our simulation timeline
	todaySolarTrend := c.calculateSolarTrend(ctx, now, history, model, settings)
	log.Ctx(ctx).DebugContext(ctx, "solar trend calculated", slog.Float64("trend", todaySolarTrend))

	// Find the maximum and minimum future grid charge cost within the 24h window for net metering valuation
	maxFutureGridChargeCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	minFutureGridChargeCost := maxFutureGridChargeCost
	for _, fp := range futurePrices {
		cost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
		if cost > maxFutureGridChargeCost {
			maxFutureGridChargeCost = cost
		}
		if cost < minFutureGridChargeCost {
			minFutureGridChargeCost = cost
		}
	}

	var hitDeficit bool
	var hitCapacity bool
	var hitSolarCapacity bool
	simTime := now

	// Optimization: Pre-calculate truncated prices to avoid redundant O(N) loops and Truncate calls
	currentPriceTrunc := currentPrice.TSStart.Truncate(time.Hour)
	futurePricesMap := make(map[time.Time]types.Price, len(futurePrices))
	for _, fp := range futurePrices {
		futurePricesMap[fp.TSStart.Truncate(time.Hour)] = fp
	}

	simHours := 24
	if len(futurePrices) > 0 {
		var lastFuturePriceTime time.Time
		for _, fp := range futurePrices {
			end := fp.TSEnd
			if end.IsZero() {
				end = fp.TSStart.Add(time.Hour)
			}
			if end.After(lastFuturePriceTime) {
				lastFuturePriceTime = end
			}
		}
		if !lastFuturePriceTime.IsZero() && lastFuturePriceTime.After(now) {
			hoursUntilEnd := int(math.Ceil(lastFuturePriceTime.Sub(now).Hours()))
			if hoursUntilEnd > 0 && hoursUntilEnd < 24 {
				simHours = hoursUntilEnd
				log.Ctx(ctx).DebugContext(
					ctx,
					"simulation set to stop when running out of prices",
					slog.Int("simHours", simHours),
					slog.Time("lastFuturePriceTime", lastFuturePriceTime),
				)
			}
		}
	}

	for i := 0; i < simHours; i++ {
		h := simTime.Hour()

		var price types.Price
		simTimeTrunc := simTime.Truncate(time.Hour)
		if currentPriceTrunc.Equal(simTimeTrunc) {
			price = currentPrice
		} else if fp, ok := futurePricesMap[simTimeTrunc]; ok {
			price = fp
		} else {
			// Find the closest future price that has not ended yet
			closestPrice := currentPrice
			for _, fp := range futurePrices {
				if fp.TSStart.After(simTimeTrunc) || fp.TSStart.Equal(simTimeTrunc) {
					closestPrice = fp
					break
				}
			}
			price = closestPrice
		}

		gridChargeCost := price.DollarsPerKWH + price.GridUseDollarsPerKWH
		solarOppCost := price.DollarsPerKWH

		if !settings.GridExportSolar {
			solarOppCost = 0
		} else if settings.UtilityRateOptions.NetMeteringCredits {
			switch settings.SolarNetMeteringCreditsValue {
			case "highest":
				solarOppCost = maxFutureGridChargeCost
			case "none":
				solarOppCost = 0
			default:
				// Default to conservative value ("lowest")
				solarOppCost = minFutureGridChargeCost
			}
		}

		profile := model[h]

		// Determine solar trend for this hour
		currentSolarTrend := todaySolarTrend
		// If we've rolled over to the next day, reset the trend to 1.0 (average)
		// We compare Year/Day to see if it's strictly a different calendar day.
		if simTime.Year() != now.Year() || simTime.YearDay() != now.YearDay() {
			currentSolarTrend = 1.0
		}

		predictedAvgSolar := profile.avgSolarKWH * currentSolarTrend

		netLoadSolar := profile.avgHomeLoadKWH - predictedAvgSolar

		clampedNet := netLoadSolar
		// update simulated energy state
		if netLoadSolar > 0 {
			// make sure we don't simulate discharging more than we can
			if currentStatus.MaxBatteryDischargeKW > 0 && clampedNet > currentStatus.MaxBatteryDischargeKW {
				clampedNet = currentStatus.MaxBatteryDischargeKW
			}
			// Load > Solar: We consume battery
			simEnergy -= clampedNet
			if simEnergy < minKWH {
				deficitKWH += minKWH - simEnergy
				simEnergy = minKWH
				hitDeficit = true
			}
		} else {
			// make sure we don't simulate charging more than we can
			if currentStatus.MaxBatteryChargeKW > 0 && clampedNet < -currentStatus.MaxBatteryChargeKW {
				clampedNet = -currentStatus.MaxBatteryChargeKW
			}
			// Solar > Load: We charge battery
			simEnergy -= clampedNet

			// If solar export is disabled, we might be curtailed if we hit capacity.
			if !settings.GridExportSolar && predictedAvgSolar > 0.1 {
				if settings.SolarFullyChargeHeadroomBatterySOC > -99.0 {
					solarCapacityKWH := capacityKWH * (1.0 - settings.SolarFullyChargeHeadroomBatterySOC/100.0)
					if simEnergy > solarCapacityKWH {
						hitSolarCapacity = true
					}
				}
			}

			// make sure we don't simulate charging more than it can hold
			if simEnergy > capacityKWH {
				simEnergy = capacityKWH
				hitCapacity = true
			}

			simStandbyEnergy -= clampedNet
			if simStandbyEnergy > capacityKWH {
				simStandbyEnergy = capacityKWH
			}
		}

		simData = append(simData, SimHour{
			TS:                      simTime,
			Hour:                    h,
			NetLoadSolarKWH:         netLoadSolar,
			ClampedNetLoadSolarKWH:  clampedNet,
			GridChargeDollarsPerKWH: gridChargeCost,
			SolarOppDollarsPerKWH:   solarOppCost,
			AvgHomeLoadKWH:          profile.avgHomeLoadKWH,
			PredictedSolarKWH:       predictedAvgSolar,
			BatteryKWH:              simEnergy,
			BatteryKWHIfStandby:     simStandbyEnergy,
			BatteryCapacityKWH:      capacityKWH,
			BatteryReserveKWH:       minKWH,
			TotalBatteryDeficitKWH:  deficitKWH,
			TodaySolarTrend:         currentSolarTrend,
			HitCapacity:             hitCapacity,
			HitSolarCapacity:        hitSolarCapacity,
			HitDeficit:              hitDeficit,
			Price:                   price,
		})
		simTime = simTime.Add(1 * time.Hour)
	}

	return simData
}

type timeProfile struct {
	hour           int
	avgSolarKWH    float64
	avgHomeLoadKWH float64
}

// buildHourlyEnergyModel averages usage and solar by hour of day from history.
// It filters out outliers if ignoreHourUsageOverMultiple is set and > 0.
func (c *Controller) buildHourlyEnergyModel(ctx context.Context, now time.Time, history []types.EnergyStats, settings types.Settings) map[int]timeProfile {
	type dataPoint struct {
		solar float64
		load  float64
	}
	hourlyData := make(map[int][]dataPoint)

	// Regroup history by hour
	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		// TODO: use the user timezone
		hour := h.TSHourStart.In(now.Location()).Hour()
		hourlyData[hour] = append(hourlyData[hour], dataPoint{
			solar: h.SolarKWH,
			load:  h.HomeKWH,
		})
	}

	result := make(map[int]timeProfile)
	for h, points := range hourlyData {
		if len(points) == 0 {
			continue
		}

		validPoints := points
		if len(points) >= 3 && settings.IgnoreHourUsageOverMultiple > 1 {
			// find outlierIdx by comparing each point to every other point.
			var outlierIdx []int
			for i, p := range points {
				isOutlier := true
				for j, other := range points {
					if i == j {
						continue
					}
					// if the point is NOT greater than another point * multiple, it's not an outlier
					if p.load <= other.load*settings.IgnoreHourUsageOverMultiple {
						isOutlier = false
						break
					}
				}

				if isOutlier {
					outlierIdx = append(outlierIdx, i)
				}
			}

			if len(outlierIdx) == 1 {
				// We found exactly one outlier, ignore it
				log.Ctx(ctx).DebugContext(
					ctx,
					"ignoring outlier data point",
					slog.Int("hour", h),
					slog.Float64("outlierLoad", points[outlierIdx[0]].load),
					slog.Float64("solar", points[outlierIdx[0]].solar),
				)
				// Rebuild valid points excluding this one
				validPoints = make([]dataPoint, 0, len(points)-1)
				for i, p := range points {
					if i != outlierIdx[0] {
						validPoints = append(validPoints, p)
					}
				}
			} else if len(outlierIdx) > 1 {
				log.Ctx(ctx).DebugContext(
					ctx,
					"ignoring multiple outlier data points",
					slog.Int("hour", h),
					slog.Int("outliers", len(outlierIdx)),
					slog.Int("points", len(points)),
				)
			}
		}

		// Now calculate averages from valid points
		var totalSolar, totalLoad float64
		var countSolar, countLoad float64
		for _, p := range validPoints {
			// Ignore values <= 0.1 as noise
			if p.solar > 0.1 {
				totalSolar += p.solar
				countSolar++
			}
			if p.load > 0.1 {
				totalLoad += p.load
				countLoad++
			}
		}

		avgSolar := 0.0
		if countSolar > 0 {
			avgSolar = totalSolar / countSolar
		}
		avgHomeLoad := 0.0
		if countLoad > 0 {
			avgHomeLoad = totalLoad / countLoad
		}

		result[h] = timeProfile{
			hour:           h,
			avgSolarKWH:    avgSolar,
			avgHomeLoadKWH: avgHomeLoad,
		}
	}

	// if they disabled solar bell curve fitting return early
	if settings.SolarBellCurveMultiplier == 0 {
		return result
	}

	// determine "Daylight Hours" range
	startSolarHour := -1
	endSolarHour := -1
	for h, profile := range result {
		if profile.avgSolarKWH > 0.1 {
			if startSolarHour == -1 || h < startSolarHour {
				startSolarHour = h
			}
			if h > endSolarHour {
				endSolarHour = h
			}
		}
	}

	var daylightDuration int
	if startSolarHour != -1 && endSolarHour != -1 {
		daylightDuration = endSolarHour - startSolarHour + 1
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"determined daytime hours",
		slog.Int("startSolarHour", startSolarHour),
		slog.Int("endSolarHour", endSolarHour),
		slog.Int("daylightDuration", daylightDuration),
	)

	// if no daylight detected, return early
	if daylightDuration == 0 {
		return result
	}

	// now fit a bell curve to the daylight hours
	// standard deviation should be roughly 1/6th of the duration to fit 99.7% of data
	// widening it to 1/3rd to be more conservative and less sensitive to edge readings
	sigma := float64(daylightDuration) / 3.0
	mu := float64(startSolarHour) + float64(daylightDuration)/2.0

	// function to calculate the bell curve value at hour x
	// returns a factor between 0 and 1 (height of bell curve relative to peak)
	bellCurveFactor := func(x float64) float64 {
		return math.Exp(-math.Pow(x-mu, 2) / (2 * math.Pow(sigma, 2)))
	}

	// find the max "estimated peak" solar generation in the history
	// "valid" means we weren't curtailed (battery full and no export)
	maxEstimatedPeak := 0.0
	maxOriginalPeak := 0.0
	maxPeakTS := time.Time{}

	// Bucket valid solar readings by hour of day
	validSolarByHour := make(map[int][]float64)

	for _, h := range history {
		hourStart := h.TSHourStart.In(now.Location())
		hour := hourStart.Hour()

		// ignore low solar values to avoid estimating huge peaks from noise at edges
		// only consider values that are at least 0.1 AND at least 20% of the bell curve height
		hourFactor := bellCurveFactor(float64(hour))
		if h.SolarKWH <= 0.1 || hourFactor <= 0.2 {
			continue
		}

		// Track max original peak for logging/debugging
		if h.SolarKWH > maxOriginalPeak {
			maxOriginalPeak = h.SolarKWH
			maxPeakTS = hourStart
		}

		// if we are exporting to the grid, or if the battery isn't full, then
		// we can trust the solar generation value
		// 98% is "full enough" to trigger curtailment
		if h.GridExportKWH > 0.1 || h.MaxBatterySOC < 98.0 {
			validSolarByHour[hour] = append(validSolarByHour[hour], h.SolarKWH)
		}
	}

	// Find the "best" hour:
	// 1. Most valid data points.
	// 2. Tie-breaker: Largest average solar (prefer hours closer to noon/peak).
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
			}
		}
	}

	if bestHour != -1 {
		// Calculate estimated peak using the average of the best hour
		factor := bellCurveFactor(float64(bestHour))
		maxEstimatedPeak = maxAvg / factor
		log.Ctx(ctx).DebugContext(
			ctx,
			"found best solar hour",
			slog.Int("hour", bestHour),
			slog.Int("count", maxCount),
			slog.Float64("avg", maxAvg),
			slog.Float64("estimatedPeak", maxEstimatedPeak),
		)
	}

	// if we didn't find any valid solar data, try to find *any* max solar data
	// (Fallback to original behavior if no valid data exists)
	if maxEstimatedPeak == 0 {
		log.Ctx(ctx).DebugContext(ctx, "no non-battery-curtailed solar data found for smoothing, using raw max solar")
		// Reset tracking to find the max raw solar that fits the curve constraints
		maxOriginalPeak = 0.0
		for _, h := range history {
			hourStart := h.TSHourStart.In(now.Location())
			hourFactor := bellCurveFactor(float64(hourStart.Hour()))
			if h.SolarKWH > 0.1 && hourFactor > 0.2 {
				if h.SolarKWH > maxOriginalPeak {
					// We prioritize the highest *observed* solar value to be conservative/realistic
					// rather than the highest *estimated* peak which can blow up with edge noise.
					maxOriginalPeak = h.SolarKWH
					maxEstimatedPeak = h.SolarKWH / hourFactor
				}
			}
		}
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"max solar estimated peak for smoothing",
		slog.Float64("maxEstimatedPeak", maxEstimatedPeak),
		slog.Float64("maxOriginalPeak", maxOriginalPeak),
		slog.Time("maxPeakTS", maxPeakTS),
	)

	// this shouldn't really happen because we checked for daylight hours earlier
	if maxEstimatedPeak == 0 {
		return result
	}

	// apply the bell curve to the model
	// we only apply it if the bell curve is *higher* than the average
	// this helps fill in gaps from clouds or curtailment
	// but we respect the average if it's higher (e.g. unusually sunny day or different season)
	for h := startSolarHour; h <= endSolarHour; h++ {
		curr, ok := result[h]
		if !ok {
			continue
		}
		predicted := maxEstimatedPeak * bellCurveFactor(float64(h))
		if curr.avgSolarKWH < predicted {
			newSolar := curr.avgSolarKWH + (predicted-curr.avgSolarKWH)*settings.SolarBellCurveMultiplier
			log.Ctx(ctx).DebugContext(
				ctx,
				"smoothing solar with bell curve",
				slog.Int("hour", h),
				slog.Float64("original", curr.avgSolarKWH),
				slog.Float64("predicted", predicted),
				slog.Float64("new", newSolar),
				slog.Float64("estimatedPeak", maxEstimatedPeak),
			)
			curr.avgSolarKWH = newSolar
			result[h] = curr
		}
	}

	return result
}

func (c *Controller) calculateSolarTrend(ctx context.Context, now time.Time, history []types.EnergyStats, model map[int]timeProfile, settings types.Settings) float64 {
	if len(history) < 2 {
		return 1.0
	}

	// Index history by time for easy lookups
	statsByTime := make(map[time.Time]types.EnergyStats)
	var latestTime time.Time
	for _, h := range history {
		t := h.TSHourStart.In(now.Location())
		statsByTime[t] = h
		// get the latest time today
		if t.Day() == now.Day() && t.After(latestTime) {
			latestTime = t
		}
	}

	if latestTime.IsZero() {
		log.Ctx(ctx).DebugContext(
			ctx,
			"no recent data",
			slog.Time("now", now),
			slog.Int("len(history)", len(history)),
		)
		return 1.0
	}

	// We need the last 2 hours of data
	t1 := latestTime
	t2 := t1.Add(-1 * time.Hour)

	s1, ok1 := statsByTime[t1]
	s2, ok2 := statsByTime[t2]

	if !ok1 || !ok2 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"not enough recent data",
			slog.Time("now", now),
			slog.Time("t1", t1),
			slog.Time("t2", t2),
		)
		return 1.0
	}

	// Calculate recent actual solar
	recentSolar := s1.SolarKWH + s2.SolarKWH

	// Calculate model expected solar for these hours
	m1 := model[t1.Hour()]
	m2 := model[t2.Hour()]

	modelSolar := m1.avgSolarKWH + m2.avgSolarKWH

	// If model expects no solar (e.g. night), we can't calculate a meaningful trend ratio.
	// TODO: figure out a better way to handle this because it could be that
	// yesterday was cloudy and today is sunny
	if modelSolar < 0.001 {
		log.Ctx(ctx).DebugContext(
			ctx,
			"model expects no solar",
			slog.Time("now", now),
			slog.Time("t1", t1),
			slog.Time("t2", t2),
			slog.Float64("modelSolar", modelSolar),
		)
		return 1.0
	}

	// check for > 10% variation
	diff := recentSolar - modelSolar
	if diff < 0 {
		diff = -diff
	}

	if diff/modelSolar > 0.10 {
		// cap the ratio at the configured maximum
		return math.Min(settings.SolarTrendRatioMax, recentSolar/modelSolar)
	}

	return 1.0
}
