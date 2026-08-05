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

// TODO: Put vppPrepChargingBuffer in settings after clarification from Franklin.
const vppPrepChargingBuffer = 2 * time.Hour

// TODO: Decide if vppStandbyDuration should be combined with vppPrepChargingBuffer.
const vppStandbyDuration = 2 * time.Hour

// batteryCapacityBuffer prevents us from trying to charge the battery to exactly
// 100% which isn't possible
const batteryCapacityBuffer = 0.98

// SimHour represents one hour of simulated energy state.
type SimHour struct {
	TS                              time.Time   `json:"ts"`
	Hour                            int         `json:"hour"`
	NetLoadSolarKWH                 float64     `json:"netLoadSolarKWH"`
	ClampedNetLoadSolarKWH          float64     `json:"clampedNetLoadSolarKWH"`
	BufferedNetLoadSolarKWH         float64     `json:"bufferedNetLoadSolarKWH"`
	ThresholdNetLoadSolarKWH        float64     `json:"thresholdNetLoadSolarKWH"`
	BufferedClampedNetLoadSolarKWH  float64     `json:"bufferedClampedNetLoadSolarKWH"`
	ThresholdClampedNetLoadSolarKWH float64     `json:"thresholdClampedNetLoadSolarKWH"`
	GridChargeDollarsPerKWH         float64     `json:"gridChargeDollarsPerKWH"`
	SolarOppDollarsPerKWH           float64     `json:"solarOppDollarsPerKWH"`
	AvgHomeLoadKWH                  float64     `json:"avgHomeLoadKWH"`
	PredictedSolarKWH               float64     `json:"predictedSolarKWH"`
	BufferedPredictedSolarKWH       float64     `json:"bufferedPredictedSolarKWH"`
	ThresholdPredictedSolarKWH      float64     `json:"thresholdPredictedSolarKWH"`
	BatteryKWH                      float64     `json:"batteryKWH"`
	StartBatteryKWH                 float64     `json:"startBatteryKWH"`
	BatteryCapacityKWH              float64     `json:"batteryCapacityKWH"`
	CapacityThresholdKWH            float64     `json:"capacityThresholdKWH"`
	BatteryReserveKWH               float64     `json:"batteryReserveKWH"`
	StandbyBatteryKWH               float64     `json:"standbyBatteryKWH"`
	TotalBatteryDeficitKWH          float64     `json:"totalBatteryDeficitKWH"`
	TotalBufferedDeficitKWH         float64     `json:"totalBufferedDeficitKWH"`
	TotalThresholdDeficitKWH        float64     `json:"totalThresholdDeficitKWH"`
	BufferedBatteryKWH              float64     `json:"bufferedBatteryKWH"`
	ThresholdBatteryKWH             float64     `json:"thresholdBatteryKWH"`
	TodaySolarTrend                 float64     `json:"todaySolarTrend"`
	EnergyApplyRatio                float64     `json:"energyApplyRatio"`
	HitCapacityAt                   time.Time   `json:"hitCapacityAt"`
	HitBufferedCapacityAt           time.Time   `json:"hitBufferedCapacityAt"`
	HitThresholdCapacityAt          time.Time   `json:"hitThresholdCapacityAt"`
	HitStandbyCapacityAt            time.Time   `json:"hitStandbyCapacityAt"`
	HitSolarCapacityAt              time.Time   `json:"hitSolarCapacityAt"`
	HitVPPCapacityAt                time.Time   `json:"hitVPPCapacityAt"`
	HitDeficitAt                    time.Time   `json:"hitDeficitAt"`
	HitBufferedDeficitAt            time.Time   `json:"hitBufferedDeficitAt"`
	HitThresholdDeficitAt           time.Time   `json:"hitThresholdDeficitAt"`
	Price                           types.Price `json:"price"`
	StartedVPPChargingAt            time.Time   `json:"startedVPPChargingAt"`
	VPPStandbyAt                    time.Time   `json:"vppStandbyAt"`
	VPPEndAt                        time.Time   `json:"vppEndAt"`
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
	// convert now if it has a default location
	if nowl := now.Location(); (nowl == nil || nowl == time.UTC || nowl.String() == "") && currentStatus.TimeLocation != "" {
		if statusLoc, err := time.LoadLocation(currentStatus.TimeLocation); err == nil {
			now = now.In(statusLoc)
		}
	}

	capacityKWH := currentStatus.BatteryCapacityKWH
	capacityThresholdKWH := capacityKWH * batteryCapacityBuffer
	currentSOC := currentStatus.BatterySOC
	// simulate battery energy over the 24 hours
	simEnergyKWH := capacityKWH * (currentSOC / 100.0)
	bufferedEnergyKWH := simEnergyKWH
	thresholdEnergyKWH := simEnergyKWH
	// bufferedShiftedEnergyKWH tracks a parallel battery SOC simulated using the safety-shifted
	// solar (bufferedPredictedSolarKWH) specifically to determine safety-buffered capacity hit times
	// without impacting the primary optimal cost simulation (which uses unshifted solar).
	bufferedShiftedEnergyKWH := simEnergyKWH
	thresholdShiftedEnergyKWH := simEnergyKWH
	standbyEnergyKWH := simEnergyKWH
	var simStandbyCapacityAt time.Time
	// simBufferedCapacityAt records when the safety-buffered battery hits capacity.
	var simBufferedCapacityAt time.Time
	var simThresholdHitCapacityAt time.Time
	var simBufferedHitDeficitAt time.Time
	var simThresholdHitDeficitAt time.Time
	var deficitKWH float64
	var bufferedDeficitKWH float64
	var thresholdDeficitKWH float64

	// Build Energy Models
	model, simParams := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

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

	var simHitDeficitAt time.Time
	var simCapacityAt time.Time
	var simSolarCapacityAt time.Time
	var simVPPCapacityAt time.Time
	var startedVPPChargingAt time.Time
	var currentVPPEventEnd time.Time
	var wasInVPPEvent bool
	simTime := now

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

	simEnd := now.Truncate(time.Hour).Add(time.Duration(simHours) * time.Hour)
	simTime = now

	for simTime.Before(simEnd) {
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
				// note: this won't exactly work if there are sub-hour prices
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

		bufferedAvgSolarKWH := profile.AvgSolarKWH
		if settings.SolarCapacityBufferMinutes > 0 {
			shiftHours := settings.SolarCapacityBufferMinutes / 60
			shiftMinutes := settings.SolarCapacityBufferMinutes % 60

			baseHour := (h - shiftHours + 24) % 24
			prevHour := (baseHour - 1 + 24) % 24

			fraction := float64(shiftMinutes) / 60.0
			shiftedSolar := (1.0-fraction)*model[baseHour].AvgSolarKWH + fraction*model[prevHour].AvgSolarKWH

			// Clamp by unshifted solar to prevent "ghost solar" after sunset
			if profile.AvgSolarKWH < shiftedSolar {
				bufferedAvgSolarKWH = profile.AvgSolarKWH
			} else {
				bufferedAvgSolarKWH = shiftedSolar
			}
		}
		bufferedPredictedSolarKWH := bufferedAvgSolarKWH * currentSolarTrend

		thresholdAvgSolarKWH := profile.AvgSolarKWH
		halfSolarBuffer := settings.SolarCapacityBufferMinutes / 2
		if halfSolarBuffer > 0 {
			shiftHours := halfSolarBuffer / 60
			shiftMinutes := halfSolarBuffer % 60

			baseHour := (h - shiftHours + 24) % 24
			prevHour := (baseHour - 1 + 24) % 24

			fraction := float64(shiftMinutes) / 60.0
			shiftedSolar := (1.0-fraction)*model[baseHour].AvgSolarKWH + fraction*model[prevHour].AvgSolarKWH

			// Clamp by unshifted solar to prevent "ghost solar" after sunset
			if profile.AvgSolarKWH < shiftedSolar {
				thresholdAvgSolarKWH = profile.AvgSolarKWH
			} else {
				thresholdAvgSolarKWH = shiftedSolar
			}
		}
		thresholdPredictedSolarKWH := thresholdAvgSolarKWH * currentSolarTrend

		netLoadSolarKWH := profile.AvgHomeLoadKWH - predictedAvgSolarKWH
		bufferedNetLoadSolarKWH := profile.AvgHomeLoadKWH - bufferedPredictedSolarKWH
		thresholdNetLoadSolarKWH := profile.AvgHomeLoadKWH - thresholdPredictedSolarKWH

		// Determine the next step's end boundary
		nextHour := simTime.Add(1 * time.Hour).Truncate(time.Hour)
		stepEnd := nextHour
		if !price.TSEnd.IsZero() && price.TSEnd.Before(nextHour) && price.TSEnd.After(simTime) {
			stepEnd = price.TSEnd
		}
		if stepEnd.After(simEnd) {
			stepEnd = simEnd
		}

		stepDuration := stepEnd.Sub(simTime)
		simEnergyApplyRatio := stepDuration.Hours()

		// Find the next active or upcoming VPP event that ends after the current simulation hour starts.
		// VPPEvents are assumed to be sorted by start time.
		var nextVPP *types.VPPEvent
		for _, ev := range currentStatus.VPPEvents {
			if ev.OptOut {
				continue
			}
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
				if !stepEnd.Before(chargeStart) && simTime.Before(bufferStart) {
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
				if !ts.IsZero() && ts.After(simTime) && ts.Before(stepEnd) {
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
		transitions = append(transitions, stepEnd)
		slices.SortFunc(transitions, func(a, b time.Time) int {
			return a.Compare(b)
		})

		stepMinSOC := settings.GetMinBatterySOC(ctx, simTime, price)

		var hourlyClampedNetKWH float64
		var hourlyBufferedShiftedClampedNetKWH float64
		var hourlyThresholdShiftedClampedNetKWH float64
		var simBlackoutAt time.Time
		var simVPPEndAt time.Time
		if nextVPP != nil {
			vppStandbyStart := nextVPP.TSStart.Add(-vppStandbyDuration)
			if vppStandbyStart.Before(stepEnd) {
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
				if simEnergyKWH < capacityThresholdKWH {
					subD := vppBufferStart.Sub(subStart).Hours()
					if subD > 0 {
						num := simEnergyKWH + currentStatus.MaxBatteryChargeKW*subD - capacityThresholdKWH
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

			// The minimum SOC threshold is normally the configured minimum reserve SOC for this period.
			// During a VPP event, the battery is permitted to discharge down to the VPP target SOC.
			stepMinKWH := capacityKWH * (min(stepMinSOC+1.0, 100.0) / 100.0)

			var subMinKWH float64
			if inVPPEvent && subVPP != nil {
				subMinKWH = capacityKWH * (subVPP.VPPSoc / 100.0)
			} else {
				subMinKWH = stepMinKWH
			}

			unbufferedMinKWH := subMinKWH
			bufferedMinKWH := subMinKWH + capacityKWH*(settings.SOCBufferPercent/100.0)
			thresholdMinKWH := subMinKWH + capacityKWH*((settings.SOCBufferPercent/2.0)/100.0)

			// Once a VPP discharge event ends, or when a new higher reserve SOC period begins,
			// the ESS system prioritizes restoring the battery up to the required reserve SOC.
			// If the current simulated energy is below the required reserve, we immediately add
			// the difference to deficitKWH and raise simEnergyKWH to unbufferedMinKWH so the simulation
			// accounts for the deficit cost and continues with the battery at or above the reserve.
			//
			// Note: We deliberately do NOT set simHitDeficitAt/simBufferedHitDeficitAt/simThresholdHitDeficitAt
			// when restoring battery energy immediately after a VPP discharge event (isPostVPP).
			// The battery was intentionally discharged down to the VPP target SOC during the event,
			// so flagging a deficit hit timestamp right post-VPP would complicate pre-VPP decision logic,
			// as no amount of standby or charging prior to the VPP event could prevent the VPP event discharge.
			isPostVPP := wasInVPPEvent && !inVPPEvent
			if isPostVPP || simEnergyKWH < unbufferedMinKWH {
				if simEnergyKWH < unbufferedMinKWH {
					if !isPostVPP && simHitDeficitAt.IsZero() {
						simHitDeficitAt = subStart
					}
					deficitKWH += unbufferedMinKWH - simEnergyKWH
					simEnergyKWH = unbufferedMinKWH
				}
				if bufferedEnergyKWH < bufferedMinKWH {
					if !isPostVPP && simBufferedHitDeficitAt.IsZero() {
						simBufferedHitDeficitAt = subStart
					}
					bufferedDeficitKWH += bufferedMinKWH - bufferedEnergyKWH
					bufferedEnergyKWH = bufferedMinKWH
				}
				if thresholdEnergyKWH < thresholdMinKWH {
					if !isPostVPP && simThresholdHitDeficitAt.IsZero() {
						simThresholdHitDeficitAt = subStart
					}
					thresholdDeficitKWH += thresholdMinKWH - thresholdEnergyKWH
					thresholdEnergyKWH = thresholdMinKWH
				}
				if bufferedShiftedEnergyKWH < bufferedMinKWH {
					bufferedShiftedEnergyKWH = bufferedMinKWH
				}
				if thresholdShiftedEnergyKWH < thresholdMinKWH {
					thresholdShiftedEnergyKWH = thresholdMinKWH
				}
			}
			wasInVPPEvent = inVPPEvent

			// startEnergy is the starting battery energy for the primary cost-optimization run (unshifted solar, unbuffered raw reserve).
			startEnergy := simEnergyKWH
			// bufferedStartEnergy tracks battery energy on unshifted solar, clamping at the buffered safety reserve (MinBatterySOC + SOCBufferPercent). Used for deficit refilling lookahead.
			bufferedStartEnergy := bufferedEnergyKWH
			// thresholdStartEnergy tracks battery energy on unshifted solar, clamping at the threshold safety reserve (MinBatterySOC + SOCBufferPercent/2).
			thresholdStartEnergy := thresholdEnergyKWH
			// bufferedShiftedStartEnergy tracks battery energy on safety-shifted (worst-case) solar, clamping at the buffered safety reserve. Used for capacity/curtailment lookahead.
			bufferedShiftedStartEnergy := bufferedShiftedEnergyKWH
			// thresholdShiftedStartEnergy tracks battery energy on safety-shifted (worst-case) solar, clamping at the threshold safety reserve.
			thresholdShiftedStartEnergy := thresholdShiftedEnergyKWH

			var subClampedNetKWH float64
			var bufferedShiftedSubClampedNetKWH float64
			var thresholdShiftedSubClampedNetKWH float64

			switch {
			case inVPPEvent:
				// VPP Event Phase:
				// The battery discharges down to the VPP target SOC (vppSocEnergy) at MaxBatteryDischargeKW.
				// Home load is covered by solar first. Remaining home load is covered by the battery (as part of
				// its discharge). It does not make sense to pull from the grid when exporting, so we only
				// import once the battery has reached its VPP minimum SOC limit.
				// During VPP events, normal safety buffers (threshold and buffered) are completely bypassed
				// to maximize utility export credits. Therefore, all trajectories (raw, threshold, and buffered)
				// are simulated to discharge down to the VPP target limit (subMinKWH).
				vppSocEnergy := subMinKWH
				maxDischargePower := currentStatus.MaxBatteryDischargeKW
				dischargePower := 0.0
				if startEnergy > vppSocEnergy && maxDischargePower > 0 {
					dischargePower = min(maxDischargePower, (startEnergy-vppSocEnergy)/subDt)
				}
				subClampedNetKWH = dischargePower

				// Discharge the standby battery capacity similarly.
				standbyDischargePower := 0.0
				if standbyEnergyKWH > vppSocEnergy && maxDischargePower > 0 {
					standbyDischargePower = min(maxDischargePower, (standbyEnergyKWH-vppSocEnergy)/subDt)
				}
				standbyEnergyKWH -= standbyDischargePower * subDt

				// Buffered simulation
				bufferedDischargePower := 0.0
				if bufferedShiftedStartEnergy > vppSocEnergy && maxDischargePower > 0 {
					bufferedDischargePower = min(maxDischargePower, (bufferedShiftedStartEnergy-vppSocEnergy)/subDt)
				}
				bufferedShiftedSubClampedNetKWH = bufferedDischargePower

				// Threshold simulation
				thresholdDischargePower := 0.0
				if thresholdShiftedStartEnergy > vppSocEnergy && maxDischargePower > 0 {
					thresholdDischargePower = min(maxDischargePower, (thresholdShiftedStartEnergy-vppSocEnergy)/subDt)
				}
				thresholdShiftedSubClampedNetKWH = thresholdDischargePower

			case inPreVPPCharging:
				// Pre-charging Phase:
				// Charge the battery at maximum power (MaxBatteryChargeKW) up to the 98% threshold.
				maxChargePower := currentStatus.MaxBatteryChargeKW
				chargePower := 0.0
				if startEnergy < capacityThresholdKWH && maxChargePower > 0 {
					chargePower = min(maxChargePower, (capacityThresholdKWH-startEnergy)/subDt)
				}
				subClampedNetKWH = -chargePower

				// Charge the standby battery capacity similarly.
				standbyChargePower := 0.0
				if standbyEnergyKWH < capacityThresholdKWH && maxChargePower > 0 {
					standbyChargePower = min(maxChargePower, (capacityThresholdKWH-standbyEnergyKWH)/subDt)
				}
				standbyEnergyKWH += standbyChargePower * subDt

				// Buffered simulation
				bufferedChargePower := 0.0
				if bufferedShiftedStartEnergy < capacityThresholdKWH && maxChargePower > 0 {
					bufferedChargePower = min(maxChargePower, (capacityThresholdKWH-bufferedShiftedStartEnergy)/subDt)
				}
				bufferedShiftedSubClampedNetKWH = -bufferedChargePower

				// Threshold simulation
				thresholdChargePower := 0.0
				if thresholdShiftedStartEnergy < capacityThresholdKWH && maxChargePower > 0 {
					thresholdChargePower = min(maxChargePower, (capacityThresholdKWH-thresholdShiftedStartEnergy)/subDt)
				}
				thresholdShiftedSubClampedNetKWH = -thresholdChargePower

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

				// Buffered simulation
				if bufferedNetLoadSolarKWH <= 0 {
					clampedNetKWH := bufferedNetLoadSolarKWH
					if currentStatus.MaxBatteryChargeKW > 0 && clampedNetKWH < -currentStatus.MaxBatteryChargeKW {
						clampedNetKWH = -currentStatus.MaxBatteryChargeKW
					}
					bufferedShiftedSubClampedNetKWH = clampedNetKWH
				} else {
					bufferedShiftedSubClampedNetKWH = 0.0
				}

				// Threshold simulation
				if thresholdNetLoadSolarKWH <= 0 {
					clampedNetKWH := thresholdNetLoadSolarKWH
					if currentStatus.MaxBatteryChargeKW > 0 && clampedNetKWH < -currentStatus.MaxBatteryChargeKW {
						clampedNetKWH = -currentStatus.MaxBatteryChargeKW
					}
					thresholdShiftedSubClampedNetKWH = clampedNetKWH
				} else {
					thresholdShiftedSubClampedNetKWH = 0.0
				}

			default:
				// Normal Simulation Phase:
				// Standard operation where the battery discharges to cover home load or charges from surplus solar/grid.
				maxDischargePower := currentStatus.MaxBatteryDischargeKW
				maxChargePower := currentStatus.MaxBatteryChargeKW

				// Primary run
				clampedNetKWH := netLoadSolarKWH
				if netLoadSolarKWH > 0 {
					// Discharging to cover load.
					if maxDischargePower > 0 && clampedNetKWH > maxDischargePower {
						clampedNetKWH = maxDischargePower
					}
					subClampedNetKWH = clampedNetKWH
				} else {
					// Charging from surplus solar/grid.
					if maxChargePower > 0 && clampedNetKWH < -maxChargePower {
						clampedNetKWH = -maxChargePower
					}
					subClampedNetKWH = clampedNetKWH
				}

				// Buffered simulation
				bufferedClampedNetKWH := bufferedNetLoadSolarKWH
				if bufferedNetLoadSolarKWH > 0 {
					if maxDischargePower > 0 && bufferedClampedNetKWH > maxDischargePower {
						bufferedClampedNetKWH = maxDischargePower
					}
				} else {
					if maxChargePower > 0 && bufferedClampedNetKWH < -maxChargePower {
						bufferedClampedNetKWH = -maxChargePower
					}
				}
				bufferedShiftedSubClampedNetKWH = bufferedClampedNetKWH

				// Threshold simulation
				thresholdClampedNetKWH := thresholdNetLoadSolarKWH
				if thresholdNetLoadSolarKWH > 0 {
					if maxDischargePower > 0 && thresholdClampedNetKWH > maxDischargePower {
						thresholdClampedNetKWH = maxDischargePower
					}
				} else {
					if maxChargePower > 0 && thresholdClampedNetKWH < -maxChargePower {
						thresholdClampedNetKWH = -maxChargePower
					}
				}
				thresholdShiftedSubClampedNetKWH = thresholdClampedNetKWH

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

			// Apply new unbuffered, buffered, and threshold optimal and shifted simulations
			newSimEnergy := startEnergy - subClampedNetKWH*subDt
			newBufferedEnergy := bufferedStartEnergy - subClampedNetKWH*subDt
			newThresholdEnergy := thresholdStartEnergy - subClampedNetKWH*subDt

			newBufferedShiftedEnergy := bufferedShiftedStartEnergy - bufferedShiftedSubClampedNetKWH*subDt
			newThresholdShiftedEnergy := thresholdShiftedStartEnergy - thresholdShiftedSubClampedNetKWH*subDt

			// Determine minimum limits. Under VPP events, safety buffers are bypassed.
			unbufferedMinLimit := unbufferedMinKWH
			bufferedMinLimit := bufferedMinKWH
			thresholdMinLimit := thresholdMinKWH
			if inVPPEvent {
				bufferedMinLimit = subMinKWH
				thresholdMinLimit = subMinKWH
			}

			// Deficit and hit times check based on unclamped unshifted trajectory
			// We use the unshifted trajectories (newSimEnergy, newThresholdEnergy, newBufferedEnergy)
			// because they represent the realistic/optimal forecast of energy used for assessing if we will run out.
			if subClampedNetKWH > 0 {
				if newSimEnergy < unbufferedMinLimit {
					if simHitDeficitAt.IsZero() {
						remainingBeforeMin := startEnergy - unbufferedMinLimit
						if remainingBeforeMin > 0 {
							fraction := max(remainingBeforeMin/subClampedNetKWH, 0)
							simHitDeficitAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
						} else {
							simHitDeficitAt = subStart
						}
					}
				}
				if newThresholdEnergy < thresholdMinLimit {
					if simThresholdHitDeficitAt.IsZero() {
						remainingBeforeMin := thresholdStartEnergy - thresholdMinLimit
						if remainingBeforeMin > 0 {
							fraction := max(remainingBeforeMin/subClampedNetKWH, 0)
							simThresholdHitDeficitAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
						} else {
							simThresholdHitDeficitAt = subStart
						}
					}
				}
				if newBufferedEnergy < bufferedMinLimit {
					if simBufferedHitDeficitAt.IsZero() {
						remainingBeforeMin := bufferedStartEnergy - bufferedMinLimit
						if remainingBeforeMin > 0 {
							fraction := max(remainingBeforeMin/subClampedNetKWH, 0)
							simBufferedHitDeficitAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
						} else {
							simBufferedHitDeficitAt = subStart
						}
					}
				}

				// Clamping and deficit accumulation
				if newSimEnergy < unbufferedMinLimit {
					deficitKWH += unbufferedMinLimit - newSimEnergy
					simEnergyKWH = unbufferedMinLimit
				} else {
					simEnergyKWH = newSimEnergy
				}

				if newBufferedEnergy < bufferedMinLimit {
					bufferedDeficitKWH += bufferedMinLimit - newBufferedEnergy
					bufferedEnergyKWH = bufferedMinLimit
				} else {
					bufferedEnergyKWH = newBufferedEnergy
				}

				if newThresholdEnergy < thresholdMinLimit {
					thresholdDeficitKWH += thresholdMinLimit - newThresholdEnergy
					thresholdEnergyKWH = thresholdMinLimit
				} else {
					thresholdEnergyKWH = newThresholdEnergy
				}
			} else {
				// Charging
				if newSimEnergy > capacityKWH {
					simEnergyKWH = capacityKWH
				} else {
					simEnergyKWH = newSimEnergy
				}

				if newBufferedEnergy > capacityKWH {
					bufferedEnergyKWH = capacityKWH
				} else {
					bufferedEnergyKWH = newBufferedEnergy
				}

				if newThresholdEnergy > capacityKWH {
					thresholdEnergyKWH = capacityKWH
				} else {
					thresholdEnergyKWH = newThresholdEnergy
				}
			}

			// Shifted runs (used for capacity hits)
			if bufferedShiftedSubClampedNetKWH > 0 {
				if newBufferedShiftedEnergy < bufferedMinLimit {
					bufferedShiftedEnergyKWH = bufferedMinLimit
				} else {
					bufferedShiftedEnergyKWH = newBufferedShiftedEnergy
				}
			} else {
				if newBufferedShiftedEnergy > capacityKWH {
					bufferedShiftedEnergyKWH = capacityKWH
				} else {
					bufferedShiftedEnergyKWH = newBufferedShiftedEnergy
				}
			}

			if thresholdShiftedSubClampedNetKWH > 0 {
				if newThresholdShiftedEnergy < thresholdMinLimit {
					thresholdShiftedEnergyKWH = thresholdMinLimit
				} else {
					thresholdShiftedEnergyKWH = newThresholdShiftedEnergy
				}
			} else {
				if newThresholdShiftedEnergy > capacityKWH {
					thresholdShiftedEnergyKWH = capacityKWH
				} else {
					thresholdShiftedEnergyKWH = newThresholdShiftedEnergy
				}
			}

			// Check for Solar charge limits / headroom limits if configured.
			// HitSolarCapacityAt here represents the solar curtailment/headroom limit hit time.
			// It is only set if GridExportSolar is false because if solar export is enabled,
			// excess solar is exported to the grid rather than being curtailed (so curtailment prevention is not needed).
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
					if remainingBeforeCapacity > 0 && subClampedNetKWH < 0 {
						fraction = max(remainingBeforeCapacity/-subClampedNetKWH, 0)
					}
					hitTime := subStart.Add(time.Duration(fraction * float64(time.Hour)))
					simCapacityAt = hitTime
					if inPreVPPCharging {
						simVPPCapacityAt = hitTime
					}
				}
				deficitKWH = 0.0
				bufferedDeficitKWH = 0.0
				thresholdDeficitKWH = 0.0
				simHitDeficitAt = time.Time{}
				simBufferedHitDeficitAt = time.Time{}
				simThresholdHitDeficitAt = time.Time{}
			}

			// Capacity hit check for buffered safety battery
			// We use the shifted trajectories for capacity hits to protect against premature curtailment/standby
			// decisions based on overly optimistic solar forecasts.
			if (bufferedShiftedSubClampedNetKWH < 0 && newBufferedShiftedEnergy >= capacityThresholdKWH) || newBufferedShiftedEnergy > capacityKWH {
				if simBufferedCapacityAt.IsZero() {
					remainingBeforeCapacity := capacityThresholdKWH - bufferedShiftedStartEnergy
					var fraction float64
					if remainingBeforeCapacity > 0 && bufferedShiftedSubClampedNetKWH < 0 {
						fraction = max(remainingBeforeCapacity/-bufferedShiftedSubClampedNetKWH, 0)
					}
					simBufferedCapacityAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
				}
				bufferedShiftedEnergyKWH = capacityKWH
			}

			// Capacity hit check for threshold safety battery
			if (thresholdShiftedSubClampedNetKWH < 0 && newThresholdShiftedEnergy >= capacityThresholdKWH) || newThresholdShiftedEnergy > capacityKWH {
				if simThresholdHitCapacityAt.IsZero() {
					remainingBeforeCapacity := capacityThresholdKWH - thresholdShiftedStartEnergy
					var fraction float64
					if remainingBeforeCapacity > 0 && thresholdShiftedSubClampedNetKWH < 0 {
						fraction = max(remainingBeforeCapacity/-thresholdShiftedSubClampedNetKWH, 0)
					}
					simThresholdHitCapacityAt = subStart.Add(time.Duration(fraction * float64(time.Hour)))
				}
				thresholdShiftedEnergyKWH = capacityKWH
			}

			// Still cap physical energy at 100% capacity
			if simEnergyKWH > capacityKWH {
				simEnergyKWH = capacityKWH
			}
			if bufferedEnergyKWH > capacityKWH {
				bufferedEnergyKWH = capacityKWH
			}
			if thresholdEnergyKWH > capacityKWH {
				thresholdEnergyKWH = capacityKWH
			}
			if bufferedShiftedEnergyKWH > capacityKWH {
				bufferedShiftedEnergyKWH = capacityKWH
			}
			if thresholdShiftedEnergyKWH > capacityKWH {
				thresholdShiftedEnergyKWH = capacityKWH
			}
			if standbyEnergyKWH > capacityKWH {
				standbyEnergyKWH = capacityKWH
			}

			hourlyClampedNetKWH += subClampedNetKWH * subDt
			hourlyBufferedShiftedClampedNetKWH += bufferedShiftedSubClampedNetKWH * subDt
			hourlyThresholdShiftedClampedNetKWH += thresholdShiftedSubClampedNetKWH * subDt
		}

		simData = append(simData, SimHour{
			TS:                              simTime,
			Hour:                            h,
			NetLoadSolarKWH:                 netLoadSolarKWH,
			ClampedNetLoadSolarKWH:          hourlyClampedNetKWH / simEnergyApplyRatio,
			BufferedNetLoadSolarKWH:         bufferedNetLoadSolarKWH,
			ThresholdNetLoadSolarKWH:        thresholdNetLoadSolarKWH,
			BufferedClampedNetLoadSolarKWH:  hourlyBufferedShiftedClampedNetKWH / simEnergyApplyRatio,
			ThresholdClampedNetLoadSolarKWH: hourlyThresholdShiftedClampedNetKWH / simEnergyApplyRatio,
			GridChargeDollarsPerKWH:         gridChargeCost,
			SolarOppDollarsPerKWH:           solarOppCost,
			AvgHomeLoadKWH:                  profile.AvgHomeLoadKWH,
			PredictedSolarKWH:               predictedAvgSolarKWH,
			BufferedPredictedSolarKWH:       bufferedPredictedSolarKWH,
			ThresholdPredictedSolarKWH:      thresholdPredictedSolarKWH,
			BatteryKWH:                      simEnergyKWH,
			BufferedBatteryKWH:              bufferedEnergyKWH,
			ThresholdBatteryKWH:             thresholdEnergyKWH,
			StartBatteryKWH:                 startBatteryKWH,
			BatteryCapacityKWH:              capacityKWH,
			CapacityThresholdKWH:            capacityThresholdKWH,
			BatteryReserveKWH:               capacityKWH * (stepMinSOC / 100.0),
			StandbyBatteryKWH:               standbyEnergyKWH,
			TotalBatteryDeficitKWH:          deficitKWH,
			TotalBufferedDeficitKWH:         bufferedDeficitKWH,
			TotalThresholdDeficitKWH:        thresholdDeficitKWH,
			TodaySolarTrend:                 currentSolarTrend,
			EnergyApplyRatio:                simEnergyApplyRatio,
			HitCapacityAt:                   simCapacityAt,
			HitBufferedCapacityAt:           simBufferedCapacityAt,
			HitThresholdCapacityAt:          simThresholdHitCapacityAt,
			HitStandbyCapacityAt:            simStandbyCapacityAt,
			HitSolarCapacityAt:              simSolarCapacityAt,
			HitVPPCapacityAt:                simVPPCapacityAt,
			HitDeficitAt:                    simHitDeficitAt,
			HitBufferedDeficitAt:            simBufferedHitDeficitAt,
			HitThresholdDeficitAt:           simThresholdHitDeficitAt,
			Price:                           price,
			StartedVPPChargingAt:            startedVPPChargingAt,
			VPPStandbyAt:                    simBlackoutAt,
			VPPEndAt:                        simVPPEndAt,
		})
		simTime = stepEnd
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
