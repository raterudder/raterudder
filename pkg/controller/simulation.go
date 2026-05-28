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
	BatteryCapacityKWH      float64     `json:"batteryCapacityKWH"`
	BatteryReserveKWH       float64     `json:"batteryReserveKWH"`
	StandbyBatteryKWH       float64     `json:"standbyBatteryKWH"`
	TotalBatteryDeficitKWH  float64     `json:"totalBatteryDeficitKWH"`
	TodaySolarTrend         float64     `json:"todaySolarTrend"`
	HitCapacityAt           time.Time   `json:"hitCapacityAt"`
	HitStandbyCapacityAt    time.Time   `json:"hitStandbyCapacityAt"`
	HitSolarCapacityAt      time.Time   `json:"hitSolarCapacityAt"`
	HitDeficitAt            time.Time   `json:"hitDeficitAt"`
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
	weather []types.Weather,
	settings types.Settings,
) []SimHour {
	capacityKWH := currentStatus.BatteryCapacityKWH
	currentSOC := currentStatus.BatterySOC
	// simulate battery energy over the 24 hours
	simEnergyKWH := capacityKWH * (currentSOC / 100.0)
	standbyEnergyKWH := simEnergyKWH
	var simStandbyCapacityAt time.Time
	var deficitKWH float64

	// Build Energy Model
	model := c.buildHourlyEnergyModel(ctx, now, history, weather, settings)
	minKWH := capacityKWH * (min(settings.MinBatterySOC+1.0, 100.0) / 100.0)

	// simulate our energy state and prices for the next 24 hours
	simData := make([]SimHour, 0, 24)

	// build our simulation timeline
	todaySolarTrend := 1.0
	// If we don't have weather data, calculate the solar trend based on recent history
	if len(weather) == 0 {
		todaySolarTrend = c.calculateSolarTrend(ctx, now, history, model, settings)
		log.Ctx(ctx).DebugContext(ctx, "solar trend calculated", slog.Float64("trend", todaySolarTrend))
	}

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

	var simDeficitAt time.Time
	var simCapacityAt time.Time
	var simSolarCapacityAt time.Time
	simTime := now
	nowRatioIntoHour := float64(now.Minute()) / 60.0

	simHours := 24
	if len(futurePrices) > 0 {
		var lastFuturePriceTime time.Time
		for _, fp := range futurePrices {
			if !fp.TSEnd.IsZero() && fp.TSEnd.After(lastFuturePriceTime) {
				lastFuturePriceTime = fp.TSEnd
			}
		}
		if !lastFuturePriceTime.IsZero() && lastFuturePriceTime.After(now) {
			hoursUntilEnd := int(math.Ceil(lastFuturePriceTime.Sub(now).Hours()))
			if hoursUntilEnd > 0 && hoursUntilEnd < simHours {
				simHours = hoursUntilEnd
				if hoursUntilEnd < 12 {
					log.Ctx(ctx).WarnContext(
						ctx,
						"simulation running with less than 12 hours",
						slog.Int("simHours", simHours),
						slog.Time("lastFuturePriceTime", lastFuturePriceTime),
					)
				}
			}
		}
	} else {
		log.Ctx(ctx).WarnContext(ctx, "no future prices provided, simulated prices will be 0")
	}

	for i := 0; i < simHours; i++ {
		h := simTime.Hour()

		var price types.Price
		if currentPrice.Contains(simTime) {
			price = currentPrice
		} else {
			var found bool
			for _, fp := range futurePrices {
				if fp.Contains(simTime) {
					price = fp
					found = true
					break
				}
			}
			// don't log for every hour if we didn't get any prices at all
			if !found && len(futurePrices) > 0 {
				log.Ctx(ctx).WarnContext(ctx, "missing future price for simulation hour", slog.Time("simTime", simTime))
				// just use the last price for the last hour instead if we have it
				lastHour := simTime.Add(-time.Hour)
				for _, fp := range futurePrices {
					if fp.Contains(lastHour) {
						price = fp
						log.Ctx(ctx).DebugContext(ctx, "using last hour price for simulation hour", slog.Time("simTime", simTime))
						break
					}
				}
			}
		}

		gridChargeCost := price.DollarsPerKWH + price.GridUseDollarsPerKWH
		var solarOppCost float64

		if !settings.GridExportSolar {
			solarOppCost = 0
		} else if settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net" {
			switch settings.SolarNetMeteringCreditsValue {
			case "highest":
				solarOppCost = maxFutureGridChargeCost
			case "none":
				solarOppCost = 0
			default:
				// Default to conservative value ("lowest")
				solarOppCost = minFutureGridChargeCost
			}
		} else if price.SeparateGenerationCredit {
			// Post-2025 style: utility pays a distinct generation credit rate for
			// solar exported to the grid, separate from the supply rate.
			solarOppCost = price.GenerationCreditDollarsPerKWH
		} else {
			solarOppCost = price.DollarsPerKWH
		}

		profile := model[h]

		// Determine solar trend for this hour
		currentSolarTrend := todaySolarTrend
		// If we've rolled over to the next day, reset the trend to 1.0 (average)
		// We compare Year/YearDay to see if it's strictly a different calendar day.
		if simTime.Year() != now.Year() || simTime.YearDay() != now.YearDay() {
			currentSolarTrend = 1.0
		}

		predictedAvgSolarKWH := profile.avgSolarKWH * currentSolarTrend
		netLoadSolarKWH := profile.avgHomeLoadKWH - predictedAvgSolarKWH
		clampedNetKWH := netLoadSolarKWH
		// if we're in the first hour only apply the remaining fraction of the hour
		simEnergyApplyRatio := 1.0
		if i == 0 {
			simEnergyApplyRatio = 1.0 - nowRatioIntoHour
		}
		// update simulated energy state
		if netLoadSolarKWH > 0 {
			// Load > Solar: We consume battery
			// make sure we don't simulate discharging more than we can
			if currentStatus.MaxBatteryDischargeKW > 0 && clampedNetKWH > currentStatus.MaxBatteryDischargeKW {
				clampedNetKWH = currentStatus.MaxBatteryDischargeKW
			}

			newSimEnergy := simEnergyKWH - (clampedNetKWH * simEnergyApplyRatio)
			// if we will go below the minimum battery SOC update the deficit and calculate
			// when we hit the deficit
			if newSimEnergy < minKWH {
				if simDeficitAt.IsZero() {
					// estimate when into the hour we hit the deficit
					remainingBeforeMin := simEnergyKWH - minKWH
					if clampedNetKWH > 0 && remainingBeforeMin > 0 {
						fraction := max(remainingBeforeMin/clampedNetKWH, 0)
						simDeficitAt = simTime.Add(time.Duration(fraction * float64(time.Hour)))
					} else {
						simDeficitAt = simTime
					}
				}
				// once we hit capacity the deficit doesn't matter anymore so stop
				// tracking it
				if simCapacityAt.IsZero() {
					deficitKWH += minKWH - newSimEnergy
				}
				simEnergyKWH = minKWH
			} else {
				simEnergyKWH = newSimEnergy
			}
		} else {
			// Solar > Load: We charge battery
			// make sure we don't simulate charging more than we can
			if currentStatus.MaxBatteryChargeKW > 0 && clampedNetKWH < -currentStatus.MaxBatteryChargeKW {
				clampedNetKWH = -currentStatus.MaxBatteryChargeKW
			}

			newSimEnergy := simEnergyKWH - (clampedNetKWH * simEnergyApplyRatio)
			// If solar export is disabled, we might be curtailed if we hit capacity.
			if !settings.GridExportSolar && predictedAvgSolarKWH > 0.1 {
				if settings.SolarFullyChargeHeadroomBatterySOC > -99.0 {
					solarCapacityKWH := capacityKWH * (1.0 - settings.SolarFullyChargeHeadroomBatterySOC/100.0)
					if newSimEnergy > solarCapacityKWH && simSolarCapacityAt.IsZero() {
						// estimate when into the hour we hit the deficit
						remainingBeforeCapacity := solarCapacityKWH - simEnergyKWH
						if clampedNetKWH < 0 && remainingBeforeCapacity > 0 {
							fraction := max(remainingBeforeCapacity/-clampedNetKWH, 0)
							simSolarCapacityAt = simTime.Add(time.Duration(fraction * float64(time.Hour)))
						} else {
							simSolarCapacityAt = simTime
						}
					}
				}
			}

			// Treat the battery as having "hit capacity" slightly earlier (at 98% SOC)
			// rather than strictly 100%. This acts as a conservative buffer that prevents
			// feedback loops (rapidly oscillating between charge, standby, and load) when
			// the battery is nearly full. We only trigger this capacity hit if we are
			// actively charging (clampedNetKWH < 0) or if the energy exceeds capacity.
			capacityThresholdKWH := capacityKWH * 0.98
			if (clampedNetKWH < 0 && newSimEnergy >= capacityThresholdKWH) || newSimEnergy > capacityKWH {
				if simCapacityAt.IsZero() {
					// estimate when into the hour we hit the capacity threshold
					remainingBeforeCapacity := capacityThresholdKWH - simEnergyKWH
					if remainingBeforeCapacity > 0 {
						fraction := max(remainingBeforeCapacity/-clampedNetKWH, 0)
						simCapacityAt = simTime.Add(time.Duration(fraction * float64(time.Hour)))
					} else {
						simCapacityAt = simTime
					}
					// once we hit capacity the battery is full and any deficit must be handled before this
					deficitKWH = 0.0
				}
			}

			// Still cap physical energy at 100% capacity
			if newSimEnergy > capacityKWH {
				simEnergyKWH = capacityKWH
			} else {
				simEnergyKWH = newSimEnergy
			}
		}

		// Simulate standby capacity progression: standby holds battery energy but still charges from surplus solar.
		clampedStandbyNetKWH := clampedNetKWH
		if clampedStandbyNetKWH > 0 {
			clampedStandbyNetKWH = 0.0 // standby doesn't discharge to cover net load
		}
		newStandbyEnergy := standbyEnergyKWH - (clampedStandbyNetKWH * simEnergyApplyRatio)
		capacityThresholdKWH := capacityKWH * 0.98
		if (clampedStandbyNetKWH < 0 && newStandbyEnergy >= capacityThresholdKWH) || newStandbyEnergy > capacityKWH {
			if simStandbyCapacityAt.IsZero() {
				remainingBeforeCapacity := capacityThresholdKWH - standbyEnergyKWH
				if remainingBeforeCapacity > 0 {
					fraction := max(remainingBeforeCapacity/-clampedStandbyNetKWH, 0)
					simStandbyCapacityAt = simTime.Add(time.Duration(fraction * float64(time.Hour)))
				} else {
					simStandbyCapacityAt = simTime
				}
			}
			standbyEnergyKWH = capacityKWH
		} else {
			standbyEnergyKWH = newStandbyEnergy
		}

		simData = append(simData, SimHour{
			TS:                      simTime,
			Hour:                    h,
			NetLoadSolarKWH:         netLoadSolarKWH,
			ClampedNetLoadSolarKWH:  clampedNetKWH,
			GridChargeDollarsPerKWH: gridChargeCost,
			SolarOppDollarsPerKWH:   solarOppCost,
			AvgHomeLoadKWH:          profile.avgHomeLoadKWH,
			PredictedSolarKWH:       predictedAvgSolarKWH,
			BatteryKWH:              simEnergyKWH,
			BatteryCapacityKWH:      capacityKWH,
			BatteryReserveKWH:       minKWH,
			StandbyBatteryKWH:       standbyEnergyKWH,
			TotalBatteryDeficitKWH:  deficitKWH,
			TodaySolarTrend:         currentSolarTrend,
			HitCapacityAt:           simCapacityAt,
			HitStandbyCapacityAt:    simStandbyCapacityAt,
			HitSolarCapacityAt:      simSolarCapacityAt,
			HitDeficitAt:            simDeficitAt,
			Price:                   price,
		})
		simTime = simTime.Add(1 * time.Hour).Truncate(time.Hour)
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
func (c *Controller) buildHourlyEnergyModel(ctx context.Context, now time.Time, history []types.EnergyStats, weather []types.Weather, settings types.Settings) map[int]timeProfile {
	type dataPoint struct {
		load float64
	}
	hourlyData := make(map[int][]dataPoint)

	// Regroup history by hour
	for _, h := range history {
		if h.TSHourStart.IsZero() {
			continue
		}
		hour := h.TSHourStart.In(now.Location()).Hour()
		hourlyData[hour] = append(hourlyData[hour], dataPoint{
			load: h.HomeKWH,
		})
	}

	// Calculate solar predictions
	var weatherSolar map[int64]WeatherSolar
	var smoothedSolar map[int]float64

	if len(weather) > 0 {
		// Construct location from weather data
		loc := types.SiteLocation{
			Latitude:  weather[0].Latitude,
			Longitude: weather[0].Longitude,
			TimeZone:  weather[0].TimeLocation,
		}
		weatherSolar = CalculateWeatherSolar(ctx, now, history, weather, loc)
	} else {
		smoothedSolar = CalculateSmoothedSolar(ctx, now, history, settings)
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
				)
				// Rebuild valid points excluding this one
				validPoints = make([]dataPoint, 0, len(points)-1)
				for i, p := range points {
					if i != outlierIdx[0] {
						validPoints = append(validPoints, p)
					}
				}
			}
		}

		// Now calculate averages from valid points
		var totalLoad float64
		var countLoad float64
		for _, p := range validPoints {
			if p.load > 0.1 {
				totalLoad += p.load
				countLoad++
			}
		}

		avgHomeLoad := 0.0
		if countLoad > 0 {
			avgHomeLoad = totalLoad / countLoad
		}

		avgSolar := 0.0
		if len(weather) > 0 {
			// Find solar for this hour of the upcoming 24h simulation
			simTime := now.In(now.Location())
			for i := 0; i < 24; i++ {
				if simTime.Hour() == h {
					if ws, ok := weatherSolar[simTime.Truncate(time.Hour).Unix()]; ok {
						avgSolar = ws.ImprovedSolar
					}
					break
				}
				simTime = simTime.Add(time.Hour)
			}
		} else {
			avgSolar = smoothedSolar[h]
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

	return result
}

func (c *Controller) calculateSolarTrend(ctx context.Context, now time.Time, history []types.EnergyStats, model map[int]timeProfile, settings types.Settings) float64 {
	if len(history) < 2 {
		return 1.0
	}

	// Index history by time for easy lookups
	statsByTime := make(map[time.Time]types.EnergyStats)
	var latestTime time.Time
	currentHour := now.Truncate(time.Hour)
	for _, h := range history {
		t := h.TSHourStart.In(now.Location())
		if t.Equal(currentHour) {
			continue
		}
		statsByTime[t] = h
		// get the latest time today
		if t.Year() == now.Year() && t.YearDay() == now.YearDay() && t.After(latestTime) {
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

	// If model expects no solar (e.g. night), we can't calculate a meaningful
	// trend ratio.
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
