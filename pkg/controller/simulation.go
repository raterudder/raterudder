package controller

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// deficitThresholdOffsetCapacityRatio represents the capacity threshold ratio (3 percentage points)
// below the reserve percent before we count/accumulate it as a deficit.
const deficitThresholdOffsetCapacityRatio = 0.03

// TODO: Put vppPrepChargingBuffer in settings after clarification from Franklin.
const vppPrepChargingBuffer = 2 * time.Hour

// TODO: Decide if vppStandbyDuration should be combined with vppPrepChargingBuffer.
const vppStandbyDuration = 2 * time.Hour

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
	StartBatteryKWH         float64     `json:"startBatteryKWH"`
	BatteryCapacityKWH      float64     `json:"batteryCapacityKWH"`
	CapacityThresholdKWH    float64     `json:"capacityThresholdKWH"`
	BatteryReserveKWH       float64     `json:"batteryReserveKWH"`
	StandbyBatteryKWH       float64     `json:"standbyBatteryKWH"`
	TotalBatteryDeficitKWH  float64     `json:"totalBatteryDeficitKWH"`
	TodaySolarTrend         float64     `json:"todaySolarTrend"`
	EnergyApplyRatio        float64     `json:"energyApplyRatio"`
	HitCapacityAt           time.Time   `json:"hitCapacityAt"`
	HitStandbyCapacityAt    time.Time   `json:"hitStandbyCapacityAt"`
	HitSolarCapacityAt      time.Time   `json:"hitSolarCapacityAt"`
	HitVPPCapacityAt        time.Time   `json:"hitVPPCapacityAt"`
	HitDeficitAt            time.Time   `json:"hitDeficitAt"`
	HitBelowDeficitAt       time.Time   `json:"hitBelowDeficitAt"`
	HitAboveDeficitAt       time.Time   `json:"hitAboveDeficitAt"`
	Price                   types.Price `json:"price"`
	StartedVPPChargingAt    time.Time   `json:"startedVPPChargingAt"`
	VPPStandbyAt            time.Time   `json:"vppStandbyAt"`
	VPPEndAt                time.Time   `json:"vppEndAt"`
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
) ([]SimHour, types.SimulationParams) {
	capacityKWH := currentStatus.BatteryCapacityKWH
	capacityThresholdKWH := capacityKWH * 0.98
	currentSOC := currentStatus.BatterySOC
	// simulate battery energy over the 24 hours
	simEnergyKWH := capacityKWH * (currentSOC / 100.0)
	standbyEnergyKWH := simEnergyKWH
	var simStandbyCapacityAt time.Time
	var deficitKWH float64

	// Build Energy Models
	model, simParams := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)
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
	var simBelowDeficitAt time.Time
	var simAboveDeficitAt time.Time
	var simCapacityAt time.Time
	var simSolarCapacityAt time.Time
	var simVPPCapacityAt time.Time
	var startedVPPChargingAt time.Time
	var currentVPPEventEnd time.Time
	var wasInVPPEvent bool
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
				if hoursUntilEnd < 10 {
					log.Ctx(ctx).WarnContext(
						ctx,
						"simulation running with less than 10 hours",
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
		startBatteryKWH := simEnergyKWH

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
			if solarOppCost != 0 {
				// Apply generation adjustment if net metering is active and not valued at 0.
				solarOppCost += price.GenerationAdjustmentDollarsPerKWH
			}
		} else if price.SeparateGenerationCredit {
			// Post-2025 style: utility pays a distinct generation credit rate for
			// solar exported to the grid, separate from the supply rate.
			solarOppCost = price.GenerationCreditDollarsPerKWH
		} else {
			// If SeparateGenerationCredit is false, the export credit is equal to the supply rate (DollarsPerKWH)
			// adjusted by the GenerationAdjustmentDollarsPerKWH (if any).
			solarOppCost = price.DollarsPerKWH + price.GenerationAdjustmentDollarsPerKWH
		}

		profile := model[h]

		// Determine solar trend for this hour
		currentSolarTrend := todaySolarTrend
		// If we've rolled over to the next day, reset the trend to 1.0 (average)
		// We compare Year/YearDay to see if it's strictly a different calendar day.
		if simTime.Year() != now.Year() || simTime.YearDay() != now.YearDay() {
			currentSolarTrend = 1.0
		}

		predictedAvgSolarKWH := profile.AvgSolarKWH * currentSolarTrend
		netLoadSolarKWH := profile.AvgHomeLoadKWH - predictedAvgSolarKWH
		// if we're in the first hour only apply the remaining fraction of the hour
		simEnergyApplyRatio := 1.0
		if i == 0 {
			simEnergyApplyRatio = 1.0 - nowRatioIntoHour
		}

		// Find the next active or upcoming VPP event that ends after the current simulation hour starts.
		// VPPEvents are assumed to be sorted by start time.
		var nextVPP *types.VPPEvent
		for _, ev := range currentStatus.VPPEvents {
			if simTime.Before(ev.TSEnd) {
				nextVPP = &ev
				break
			}
		}

		// Track and reset the startedVPPChargingAt timestamp if we have moved past the end
		// of the previously monitored VPP event, so we don't carry over the charging start time.
		if nextVPP != nil {
			if !currentVPPEventEnd.IsZero() && !simTime.Before(currentVPPEventEnd) {
				startedVPPChargingAt = time.Time{}
			}
			currentVPPEventEnd = nextVPP.TSEnd
		} else {
			if !currentVPPEventEnd.IsZero() && !simTime.Before(currentVPPEventEnd) {
				startedVPPChargingAt = time.Time{}
			}
			currentVPPEventEnd = time.Time{}
		}

		// Note that for i = 0, simTime is exactly "now" (which has a non-zero minute component),
		// while for i > 0 it is truncated to the hour.
		hourEnd := simTime.Add(1 * time.Hour).Truncate(time.Hour)

		// We want to reach capacity exactly at the bufferStart (vppPrepChargingBuffer before VPP).
		// Over the remaining duration to the buffer start, we will have load pulling
		// from the battery, and when charging starts we will charge at
		// MaxBatteryChargeKW.
		var chargeStart time.Time
		if nextVPP != nil {
			eventStart := nextVPP.TSStart
			bufferStart := eventStart.Add(-vppPrepChargingBuffer)

			if simEnergyKWH < capacityThresholdKWH {
				chargeDuration := bufferStart.Sub(simTime).Hours()
				if chargeDuration > 0 {
					numerator := simEnergyKWH + currentStatus.MaxBatteryChargeKW*chargeDuration - capacityThresholdKWH
					denominator := netLoadSolarKWH + currentStatus.MaxBatteryChargeKW
					if denominator > 0 {
						x := numerator / denominator
						chargeStart = simTime.Add(time.Duration(x * float64(time.Hour)))
					} else {
						// Fallback if charge rate is zero/negative
						chargeStart = bufferStart
					}
				} else {
					// We are already past the buffer start time, start charging immediately
					chargeStart = bufferStart
				}
			} else {
				// Already fully charged, no pre-charging needed prior to the buffer start
				chargeStart = bufferStart
			}

			// If we haven't recorded the pre-charging start time yet, check if the calculated
			// chargeStart falls within this hourly simulation window.
			if startedVPPChargingAt.IsZero() && simEnergyKWH < capacityThresholdKWH {
				if !hourEnd.Before(chargeStart) && simTime.Before(bufferStart) {
					startedVPPChargingAt = chargeStart
				}
			}
		}

		// To accurately model state transitions (charging start, blackout start, VPP event start/end)
		// that happen in the middle of a simulation hour, we split the hour into sub-intervals.
		transitions := []time.Time{simTime}
		if nextVPP != nil {
			eventStart := nextVPP.TSStart
			eventEnd := nextVPP.TSEnd
			vppStandbyStart := eventStart.Add(-vppStandbyDuration)
			vppBufferStart := eventStart.Add(-vppPrepChargingBuffer)

			candidates := []time.Time{chargeStart, vppStandbyStart, vppBufferStart, eventStart, eventEnd}
			for _, ts := range candidates {
				if !ts.IsZero() && ts.After(simTime) && ts.Before(hourEnd) {
					found := false
					for _, existing := range transitions {
						if existing.Equal(ts) {
							found = true
							break
						}
					}
					if !found {
						transitions = append(transitions, ts)
					}
				}
			}
		}
		transitions = append(transitions, hourEnd)
		slices.SortFunc(transitions, func(a, b time.Time) int {
			return a.Compare(b)
		})

		var hourlyClampedNetKWH float64
		var simBlackoutAt time.Time
		var simVPPEndAt time.Time
		if nextVPP != nil {
			vppStandbyStart := nextVPP.TSStart.Add(-vppStandbyDuration)
			if vppStandbyStart.Before(hourEnd) {
				simBlackoutAt = vppStandbyStart
				simVPPEndAt = nextVPP.TSEnd
			}
		}

		// Iterate through each sub-interval and simulate battery dynamics.
		for j := 0; j < len(transitions)-1; j++ {
			subStart := transitions[j]
			subEnd := transitions[j+1]
			subDt := subEnd.Sub(subStart).Hours()
			if subDt <= 0 {
				continue
			}
			// Use the midpoint of the sub-interval to determine the active simulation state.
			subMid := subStart.Add(subEnd.Sub(subStart) / 2)
			startEnergy := simEnergyKWH

			// Determine which VPP phase applies to this sub-interval.
			var inVPPEvent, inPreVPPStandby, inPreVPPCharging bool
			var subVPP *types.VPPEvent
			if nextVPP != nil {
				eventStart := nextVPP.TSStart
				eventEnd := nextVPP.TSEnd
				vppStandbyStart := eventStart.Add(-vppStandbyDuration)
				vppBufferStart := eventStart.Add(-vppPrepChargingBuffer)

				// Recalculate chargeStart based on the current energy at the start of the sub-interval
				var subChargeStart time.Time
				if startEnergy < capacityThresholdKWH {
					subD := vppBufferStart.Sub(subStart).Hours()
					if subD > 0 {
						num := startEnergy + currentStatus.MaxBatteryChargeKW*subD - capacityThresholdKWH
						den := netLoadSolarKWH + currentStatus.MaxBatteryChargeKW
						if den > 0 {
							subChargeStart = subStart.Add(time.Duration((num / den) * float64(time.Hour)))
						} else {
							subChargeStart = vppBufferStart
						}
					} else {
						subChargeStart = vppBufferStart
					}
				} else {
					subChargeStart = vppBufferStart
				}

				if !subMid.Before(eventStart) && subMid.Before(eventEnd) {
					inVPPEvent = true
					subVPP = nextVPP
				}
				if !subMid.Before(vppStandbyStart) && subMid.Before(eventStart) {
					inPreVPPStandby = true
					subVPP = nextVPP
				}
				if !subChargeStart.IsZero() && !subMid.Before(subChargeStart) && subMid.Before(vppBufferStart) {
					inPreVPPCharging = true
					subVPP = nextVPP
				}
			}

			// The minimum SOC threshold is normally the configured minimum reserve SOC.
			// During a VPP event, the battery is permitted to discharge down to the VPP target SOC.
			var subMinKWH float64
			if inVPPEvent && subVPP != nil {
				subMinKWH = capacityKWH * (subVPP.VPPSoc / 100.0)
			} else {
				subMinKWH = minKWH
			}

			// Once a VPP discharge event ends, the ESS system immediately prioritizes
			// charging the battery back up to the user-configured minimum backup reserve SOC (subMinKWH).
			// Since typical charging rates are high (e.g. 5-10 kW), this refill generally takes less
			// than an hour, justifying the assumption of immediate restoration in the simulation.
			// We add this energy difference to deficitKWH to correctly model the grid import cost of
			// refilling the battery reserve. If we did not add it, the battery would "magically"
			// obtain free energy in the simulation, skewing the overall cost and savings calculations.
			// While this charging is inevitable regardless of controller actions, representing it as
			// a deficit ensures the simulator accurately captures the economic cost of VPP participation.
			if wasInVPPEvent && !inVPPEvent {
				if simEnergyKWH < subMinKWH {
					deficitKWH += subMinKWH - simEnergyKWH
					simEnergyKWH = subMinKWH
					startEnergy = subMinKWH
				}
			}
			wasInVPPEvent = inVPPEvent

			var subClampedNetKWH float64

			switch {
			case inVPPEvent:
				// VPP Event Phase:
				// The battery discharges down to the VPP target SOC (vppSocEnergy) at MaxBatteryDischargeKW.
				// Home load is covered by solar first. Remaining home load is covered by the battery (as part of
				// its discharge). It does not make sense to pull from the grid when exporting, so we only
				// import once the battery has reached its VPP minimum SOC limit.
				vppSocEnergy := subMinKWH
				maxDischargePower := currentStatus.MaxBatteryDischargeKW
				dischargePower := 0.0
				if startEnergy > vppSocEnergy && maxDischargePower > 0 {
					dischargePower = min(maxDischargePower, (startEnergy-vppSocEnergy)/subDt)
				}
				subClampedNetKWH = dischargePower
				simEnergyKWH = startEnergy - subClampedNetKWH*subDt

				// Discharge the standby battery capacity similarly.
				standbyDischargePower := 0.0
				if standbyEnergyKWH > vppSocEnergy && maxDischargePower > 0 {
					standbyDischargePower = min(maxDischargePower, (standbyEnergyKWH-vppSocEnergy)/subDt)
				}
				standbyEnergyKWH -= standbyDischargePower * subDt

			case inPreVPPCharging:
				// Pre-charging Phase:
				// Charge the battery at maximum power (MaxBatteryChargeKW) up to the 98% threshold.
				maxChargePower := currentStatus.MaxBatteryChargeKW
				chargePower := 0.0
				if startEnergy < capacityThresholdKWH && maxChargePower > 0 {
					chargePower = min(maxChargePower, (capacityThresholdKWH-startEnergy)/subDt)
				}
				subClampedNetKWH = -chargePower
				simEnergyKWH = startEnergy - subClampedNetKWH*subDt

				// Charge the standby battery capacity similarly.
				standbyChargePower := 0.0
				if standbyEnergyKWH < capacityThresholdKWH && maxChargePower > 0 {
					standbyChargePower = min(maxChargePower, (capacityThresholdKWH-standbyEnergyKWH)/subDt)
				}
				standbyEnergyKWH += standbyChargePower * subDt

			case inPreVPPStandby:
				// Pre-VPP Standby Phase (1 hour before VPP event):
				// The battery is prevented from discharging to ensure we enter the VPP event with maximum capacity.
				// However, if there is surplus solar (load < solar), we still allow the battery to charge from it.
				if netLoadSolarKWH <= 0 {
					clampedNetKWH := netLoadSolarKWH
					if currentStatus.MaxBatteryChargeKW > 0 && clampedNetKWH < -currentStatus.MaxBatteryChargeKW {
						clampedNetKWH = -currentStatus.MaxBatteryChargeKW
					}
					subClampedNetKWH = clampedNetKWH
					simEnergyKWH = startEnergy - subClampedNetKWH*subDt

					// Standby capacity charges from surplus solar too.
					newStandbyEnergy := standbyEnergyKWH - subClampedNetKWH*subDt
					if newStandbyEnergy > capacityKWH {
						standbyEnergyKWH = capacityKWH
					} else {
						standbyEnergyKWH = newStandbyEnergy
					}
				} else {
					subClampedNetKWH = 0.0
				}

			default:
				// Normal Simulation Phase:
				// Standard operation where the battery discharges to cover home load or charges from surplus solar/grid.
				clampedNetKWH := netLoadSolarKWH
				if netLoadSolarKWH > 0 {
					// Discharging to cover load.
					if currentStatus.MaxBatteryDischargeKW > 0 && clampedNetKWH > currentStatus.MaxBatteryDischargeKW {
						clampedNetKWH = currentStatus.MaxBatteryDischargeKW
					}
					subClampedNetKWH = clampedNetKWH
					newSimEnergy := startEnergy - subClampedNetKWH*subDt

					// Handle discharging below minimum SOC or deficit threshold buffers.
					if newSimEnergy < subMinKWH {
						deficitThresholdKWH := max(subMinKWH-(capacityKWH*deficitThresholdOffsetCapacityRatio), 0.0)
						if newSimEnergy < deficitThresholdKWH || (!simBelowDeficitAt.IsZero() && newSimEnergy < subMinKWH) {
							if simBelowDeficitAt.IsZero() {
								remainingBeforeDeficit := startEnergy - deficitThresholdKWH
								if clampedNetKWH > 0 && remainingBeforeDeficit > 0 {
									fraction := max(remainingBeforeDeficit/clampedNetKWH, 0)
									simBelowDeficitAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
								} else {
									simBelowDeficitAt = subStart
								}
							}
							deficitKWH += subMinKWH - newSimEnergy
							simEnergyKWH = subMinKWH
						} else {
							simEnergyKWH = newSimEnergy
						}
					} else {
						simEnergyKWH = newSimEnergy
					}

					// Record when the battery crosses the minimum SOC and when it rises above deficit buffers.
					if newSimEnergy < subMinKWH {
						if simDeficitAt.IsZero() {
							remainingBeforeMin := startEnergy - subMinKWH
							if clampedNetKWH > 0 && remainingBeforeMin > 0 {
								fraction := max(remainingBeforeMin/clampedNetKWH, 0)
								simDeficitAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
							} else {
								simDeficitAt = subStart
							}
						}
					}
					aboveDeficitThresholdKWH := subMinKWH + (capacityKWH * 0.01)
					if newSimEnergy < aboveDeficitThresholdKWH {
						if simAboveDeficitAt.IsZero() {
							remainingBeforeAbove := startEnergy - aboveDeficitThresholdKWH
							if clampedNetKWH > 0 && remainingBeforeAbove > 0 {
								fraction := max(remainingBeforeAbove/clampedNetKWH, 0)
								simAboveDeficitAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
							} else {
								simAboveDeficitAt = subStart
							}
						}
					}

				} else {
					// Charging from surplus solar/grid.
					if currentStatus.MaxBatteryChargeKW > 0 && clampedNetKWH < -currentStatus.MaxBatteryChargeKW {
						clampedNetKWH = -currentStatus.MaxBatteryChargeKW
					}
					subClampedNetKWH = clampedNetKWH
					simEnergyKWH = startEnergy - subClampedNetKWH*subDt
				}

				// Simulate standby capacity charging from surplus solar as well.
				clampedStandbyNetKWH := clampedNetKWH
				if clampedStandbyNetKWH > 0 {
					clampedStandbyNetKWH = 0.0 // standby doesn't discharge to cover net load
				}
				newStandbyEnergy := standbyEnergyKWH - (clampedStandbyNetKWH * subDt)
				if (clampedStandbyNetKWH < 0 && newStandbyEnergy >= capacityThresholdKWH) || newStandbyEnergy > capacityKWH {
					if simStandbyCapacityAt.IsZero() {
						remainingBeforeCapacity := capacityThresholdKWH - standbyEnergyKWH
						if remainingBeforeCapacity > 0 {
							fraction := max(remainingBeforeCapacity/-clampedStandbyNetKWH, 0)
							simStandbyCapacityAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
						} else {
							simStandbyCapacityAt = subStart
						}
					}
					standbyEnergyKWH = capacityKWH
				} else {
					standbyEnergyKWH = newStandbyEnergy
				}

			}

			newSimEnergy := startEnergy - subClampedNetKWH*subDt

			// Check for Solar charge limits / headroom limits if configured.
			if !settings.GridExportSolar && predictedAvgSolarKWH > 0.1 {
				if settings.SolarFullyChargeHeadroomBatterySOC > -99.0 {
					solarCapacityKWH := capacityKWH * (1.0 - settings.SolarFullyChargeHeadroomBatterySOC/100.0)
					if newSimEnergy > solarCapacityKWH && simSolarCapacityAt.IsZero() {
						remainingBeforeCapacity := solarCapacityKWH - startEnergy
						if subClampedNetKWH < 0 && remainingBeforeCapacity > 0 {
							fraction := max(remainingBeforeCapacity/-subClampedNetKWH, 0)
							simSolarCapacityAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
						} else {
							simSolarCapacityAt = subStart
						}
					}
				}
			}

			// Treat the battery as having "hit capacity" slightly earlier (at 98% SOC)
			// rather than strictly 100%. This acts as a conservative buffer that prevents
			// feedback loops (rapidly oscillating between charge, standby, and load) when
			// the battery is nearly full. We only trigger this capacity hit if we are
			// actively charging (subClampedNetKWH < 0) or if the energy exceeds capacity.
			if (subClampedNetKWH < 0 && newSimEnergy >= capacityThresholdKWH) || newSimEnergy > capacityKWH {
				if simCapacityAt.IsZero() {
					remainingBeforeCapacity := capacityThresholdKWH - startEnergy
					var fraction float64
					if remainingBeforeCapacity > 0 {
						fraction = max(remainingBeforeCapacity/-subClampedNetKWH, 0)
					}
					hitTime := subStart.Add(time.Duration(fraction * float64(time.Hour)))
					simCapacityAt = hitTime
					if inPreVPPCharging {
						simVPPCapacityAt = hitTime
					}
				}
				deficitKWH = 0.0
				simDeficitAt = time.Time{}
				simBelowDeficitAt = time.Time{}
				simAboveDeficitAt = time.Time{}
			}

			// Still cap physical energy at 100% capacity
			if simEnergyKWH > capacityKWH {
				simEnergyKWH = capacityKWH
			}

			hourlyClampedNetKWH += subClampedNetKWH * subDt
		}

		simData = append(simData, SimHour{
			TS:                      simTime,
			Hour:                    h,
			NetLoadSolarKWH:         netLoadSolarKWH,
			ClampedNetLoadSolarKWH:  hourlyClampedNetKWH / simEnergyApplyRatio,
			GridChargeDollarsPerKWH: gridChargeCost,
			SolarOppDollarsPerKWH:   solarOppCost,
			AvgHomeLoadKWH:          profile.AvgHomeLoadKWH,
			PredictedSolarKWH:       predictedAvgSolarKWH,
			BatteryKWH:              simEnergyKWH,
			StartBatteryKWH:         startBatteryKWH,
			BatteryCapacityKWH:      capacityKWH,
			CapacityThresholdKWH:    capacityThresholdKWH,
			BatteryReserveKWH:       minKWH,
			StandbyBatteryKWH:       standbyEnergyKWH,
			TotalBatteryDeficitKWH:  deficitKWH,
			TodaySolarTrend:         currentSolarTrend,
			EnergyApplyRatio:        simEnergyApplyRatio,
			HitCapacityAt:           simCapacityAt,
			HitStandbyCapacityAt:    simStandbyCapacityAt,
			HitSolarCapacityAt:      simSolarCapacityAt,
			HitVPPCapacityAt:        simVPPCapacityAt,
			HitDeficitAt:            simDeficitAt,
			HitBelowDeficitAt:       simBelowDeficitAt,
			HitAboveDeficitAt:       simAboveDeficitAt,
			Price:                   price,
			StartedVPPChargingAt:    startedVPPChargingAt,
			VPPStandbyAt:            simBlackoutAt,
			VPPEndAt:                simVPPEndAt,
		})
		simTime = simTime.Add(1 * time.Hour).Truncate(time.Hour)
	}

	return simData, simParams
}

type TimeProfile struct {
	Hour           int
	AvgSolarKWH    float64
	AvgHomeLoadKWH float64
}

func (c *Controller) calculateSolarTrend(ctx context.Context, now time.Time, history []types.EnergyStats, model map[int]TimeProfile, settings types.Settings) float64 {
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

	modelSolar := m1.AvgSolarKWH + m2.AvgSolarKWH

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
