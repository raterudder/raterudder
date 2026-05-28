package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// Decision represents the result of the decision logic.
type Decision struct {
	Action      types.Action
	Explanation string
}

// Controller handles the decision-making logic for the ESS.
type Controller struct{}

type simPriceSlot struct {
	cost  float64
	ts    time.Time
	price types.Price
}

// NewController creates a new Controller.
func NewController() *Controller {
	return &Controller{}
}

// Decide determines the best action to take based on current state and history.
func (c *Controller) Decide(
	ctx context.Context,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
) (Decision, error) {
	log.Ctx(ctx).DebugContext(ctx, "controller decide started",
		slog.Float64("soc", currentStatus.BatterySOC),
		slog.Float64("batteryKW", currentStatus.BatteryKW),
		slog.Float64("solarKW", currentStatus.SolarKW),
		slog.Float64("homeKW", currentStatus.HomeKW),
		slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
	)

	now := currentStatus.Timestamp
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(currentStatus.Timestamp.Location())

	solarMode := types.SolarModeAny
	if !settings.GridExportSolar {
		solarMode = types.SolarModeNoExport
	}

	simData := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, weather, settings)

	if len(simData) > 0 && simData[0].TS.After(now) {
		log.Ctx(ctx).WarnContext(ctx, "simulation started in the future", slog.Time("simTime", simData[0].TS))
	}

	// Rule 1: If the solar export value is negative, then don't export solar to the grid.
	// This ensures that net-metering users (with positive solar opportunity values)
	// continue to export solar even if the spot price is negative.
	if len(simData) > 0 && !simData[0].TS.After(now) && simData[0].SolarOppDollarsPerKWH < 0 {
		solarMode = types.SolarModeNoExport
		log.Ctx(ctx).DebugContext(ctx, "solar export value is negative, disabling solar export",
			slog.Float64("price", currentPrice.DollarsPerKWH),
			slog.Float64("solarOppValue", simData[0].SolarOppDollarsPerKWH))
		// We do NOT return here. We fall through to allow charging logic to trigger.
	}

	var hitDeficitAt time.Time
	var hitCapacityAt time.Time
	var hitStandbyCapacityAt time.Time
	var hitSolarCapacityAt time.Time
	// Helper to build final action
	decision := func(batteryMode types.BatteryMode, reason types.ActionReason, modeReason string, futurePrice *types.Price) Decision {
		return Decision{
			Action: types.Action{
				Timestamp:         now.UTC(),
				BatteryMode:       batteryMode,
				SolarMode:         solarMode,
				TargetBatteryMode: batteryMode,
				TargetSolarMode:   solarMode,
				Reason:            reason,
				Description:       modeReason,
				CurrentPrice:      &currentPrice,
				FuturePrice:       futurePrice,
				SystemStatus:      currentStatus,
				HitDeficitAt:      hitDeficitAt,
				HitCapacityAt:     hitCapacityAt,
			},
		}
	}

	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		return decision(types.BatteryModeStandby, types.ActionReasonMissingBattery, "Battery Config Missing or Capacity 0. Standby.", nil), nil
	}

	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	// Rule 2: If the price is below the Always Charge Threshold, then charge the
	// battery.
	if !currentStatus.BatteryChargingDisabled && gridChargeNowCost <= settings.AlwaysChargeUnderDollarsPerKWH {
		desc := fmt.Sprintf(
			"Price Low (%.3f < %.3f). Charging.",
			gridChargeNowCost,
			settings.AlwaysChargeUnderDollarsPerKWH,
		)
		if solarMode == types.SolarModeNoExport {
			desc += " (Export Disabled due to Negative Price)"
		}
		// If negative, we charge.
		log.Ctx(ctx).DebugContext(ctx, "price below always charge threshold", slog.Float64("price", gridChargeNowCost), slog.Float64("threshold", settings.AlwaysChargeUnderDollarsPerKWH))
		return decision(types.BatteryModeChargeAny, types.ActionReasonAlwaysChargeBelowThreshold, desc, nil), nil
	}

	// Rule 3: Charge now if its cheaper than later, if we will run out of energy
	// or if we can make more money buying now and selling later (arbitrage)

	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		// conservatively assume it takes 3 hours to charge the battery from 0->100
		chargeKW = capacityKWH / 3.0
	}
	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0

	// To prevent rapid start/stop cycling near full capacity, we implement a hysteresis buffer:
	// - If the battery is already charging from the grid, we can continue charging up to 100% capacity (minus a 0.1 kWh buffer).
	// - If the battery is not currently charging from the grid, we only start charging if the battery has at least 5 minutes of charging headroom.
	isAlreadyChargingGrid := currentStatus.BatteryKW < 0 && currentStatus.GridKW > 0
	minStartChargeDurationHours := 5.0 / 60.0
	startChargeHeadroom := chargeKW * minStartChargeDurationHours
	if startChargeHeadroom < 0.3 {
		startChargeHeadroom = 0.3 // minimum 0.3 kWh headroom to initiate a charge
	}

	var allowedHeadroom float64
	if isAlreadyChargingGrid {
		allowedHeadroom = 0.1 // 0.1 kWh buffer to complete charge
	} else {
		allowedHeadroom = startChargeHeadroom
	}

	canChargeNow := currentEnergyKWH+allowedHeadroom < capacityKWH && settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled
	canChargeFuture := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled

	// assume we need to charge for at least 10 minutes for it to be worth it for arbitrage
	minChargeDurationHours := 10.0 / 60.0
	simEnergyAfterCharge := currentEnergyKWH + chargeKW*minChargeDurationHours
	canChargeArbitrage := simEnergyAfterCharge < capacityKWH && canChargeNow

	shouldCharge := false
	var chargeDescription string
	var chargeActionReason types.ActionReason
	var futurePrice *types.Price

	for _, slot := range simData {
		if !slot.HitCapacityAt.IsZero() && hitCapacityAt.IsZero() {
			hitCapacityAt = slot.HitCapacityAt
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy hit capacity",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("capacityKWH", capacityKWH),
				slog.Int("simHour", slot.Hour),
				slog.Time("hitCapacityAt", hitCapacityAt),
			)
		}
		if !slot.HitStandbyCapacityAt.IsZero() && hitStandbyCapacityAt.IsZero() {
			hitStandbyCapacityAt = slot.HitStandbyCapacityAt
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated standby energy hit capacity",
				slog.Float64("standbyBatteryKWH", slot.StandbyBatteryKWH),
				slog.Float64("capacityKWH", capacityKWH),
				slog.Int("simHour", slot.Hour),
				slog.Time("hitStandbyCapacityAt", hitStandbyCapacityAt),
			)
		}
		if !slot.HitSolarCapacityAt.IsZero() && hitSolarCapacityAt.IsZero() {
			hitSolarCapacityAt = slot.HitSolarCapacityAt
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy hit solar headroom capacity",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("capacityKWH", capacityKWH),
				slog.Int("simHour", slot.Hour),
				slog.Time("hitSolarCapacityAt", hitSolarCapacityAt),
			)
		}
	}

	// Record the earliest of solar or battery capacity as hitCapacityAt
	if !hitSolarCapacityAt.IsZero() && (hitCapacityAt.IsZero() || hitSolarCapacityAt.Before(hitCapacityAt)) {
		hitCapacityAt = hitSolarCapacityAt
	}

	maxFutureGridChargeCost := gridChargeNowCost
	maxFutureGridChargePrice := currentPrice
	maxFutureGridChargeTime := now
	minFutureGridChargeCost := gridChargeNowCost
	for _, slot := range simData {
		// don't bother recording any grid charge costs after we hit capacity (earliest of solar or battery capacity)
		if !hitCapacityAt.IsZero() && slot.TS.After(hitCapacityAt) {
			break
		}
		if slot.GridChargeDollarsPerKWH > maxFutureGridChargeCost {
			maxFutureGridChargeCost = slot.GridChargeDollarsPerKWH
			maxFutureGridChargePrice = slot.Price
			maxFutureGridChargeTime = slot.TS
		}
		if slot.GridChargeDollarsPerKWH < minFutureGridChargeCost {
			minFutureGridChargeCost = slot.GridChargeDollarsPerKWH
		}
	}

	// track simulated energy
	var plannedChargeTime time.Time
	var plannedChargePrice types.Price
	var plannedChargeCost float64
	var soonestExportValue float64
	var soonestExportPrice types.Price
	var soonestExportAt time.Time
	minEnergy := -1.0
	maxEnergy := -1.0
	var deficitAmount float64
	var chargeDurationHours int
	var neededEnergy float64
	var hitSolarSurplusAt time.Time
	var predictedSolarAtDeficitKWH float64

	for i, slot := range simData {
		if minEnergy == -1 || slot.BatteryKWH < minEnergy {
			minEnergy = slot.BatteryKWH
		}
		if maxEnergy == -1 || slot.BatteryKWH > maxEnergy {
			maxEnergy = slot.BatteryKWH
		}

		// if we have less than 7 minutes charging's worth of deficit, ignore it
		hasDeficit := slot.TotalBatteryDeficitKWH > 0
		if hasDeficit {
			deficitDurationMinutes := (slot.TotalBatteryDeficitKWH / chargeKW) * 60.0
			if deficitDurationMinutes < 7.0 {
				log.Ctx(ctx).DebugContext(
					ctx,
					"deficit ignored: charge duration too small",
					slog.Float64("deficit", slot.TotalBatteryDeficitKWH),
					slog.Float64("chargeKW", chargeKW),
					slog.Float64("deficitDurationMinutes", deficitDurationMinutes),
					slog.Int("simHour", slot.Hour),
					slog.Float64("predictedSolarKWH", slot.PredictedSolarKWH),
				)
				hasDeficit = false
			}
		}

		// check if we are below the minimum SOC and when we need to charge
		isPostCapacityDeficit := !hitCapacityAt.IsZero() && !slot.HitDeficitAt.IsZero() && !slot.HitDeficitAt.Before(hitCapacityAt)
		if (hasDeficit || isPostCapacityDeficit) && !slot.HitDeficitAt.IsZero() && hitDeficitAt.IsZero() {
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy below minimum SOC causing a deficit",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("reserveKWH", slot.BatteryReserveKWH),
				slog.Int("simHour", slot.Hour),
				slog.Time("hitAt", slot.HitDeficitAt),
				slog.Float64("predictedSolarKWH", slot.PredictedSolarKWH),
			)
			hitDeficitAt = slot.HitDeficitAt
			if hitDeficitAt.Before(slot.TS) {
				hitDeficitAt = slot.TS
			}
			predictedSolarAtDeficitKWH = slot.PredictedSolarKWH
		}

		// check when solar generates more than home load
		if slot.PredictedSolarKWH > slot.AvgHomeLoadKWH && hitSolarSurplusAt.IsZero() {
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated solar generation exceeds home usage",
				slog.Float64("solarKWH", slot.PredictedSolarKWH),
				slog.Float64("homeKWH", slot.AvgHomeLoadKWH),
				slog.Int("simHour", slot.Hour),
				slog.Time("hitAt", slot.TS),
			)
			hitSolarSurplusAt = slot.TS
		}

		// we only track the export value for arbitrage since we cannot export battery
		// to the grid. If we have a deficit, it is handled by the deficit logic.
		// "Save" arbitrage (avoiding grid import) is redundant and causes unnecessary
		// charging when there is no deficit.
		if slot.NetLoadSolarKWH <= -0.05 {
			if soonestExportValue == 0 && slot.SolarOppDollarsPerKWH-gridChargeNowCost > settings.MinArbitrageDifferenceDollarsPerKWH {
				soonestExportValue = slot.SolarOppDollarsPerKWH
				soonestExportAt = slot.TS
				soonestExportPrice = slot.Price
			}
		}

		if hasDeficit {
			deficitAmount = slot.TotalBatteryDeficitKWH
			maxHeadroom := capacityKWH - slot.BatteryReserveKWH
			if maxHeadroom < 0 {
				maxHeadroom = 0
			}
			neededEnergy = deficitAmount
			if neededEnergy > maxHeadroom {
				neededEnergy = maxHeadroom
			}

			// factor in the cost of charging for the duration of the charge which
			// means we need to look at the nth cheapest charge cost
			// round up the hours we need to charge except for a little buffer
			chargeDurationHours = max(1, int((neededEnergy/chargeKW + 0.84)))
		}

		// If we are simulating an hour after a future deficit, we skip it for planning
		// because we cannot delay charging past the deficit hour without running out of battery.
		// However, if the deficit is happening right now, we do not skip future hours so we
		// can compare current price against all future prices to decide whether to charge now.
		isAfterFutureDeficit := !hitDeficitAt.IsZero() && hitDeficitAt.After(now.Add(time.Hour)) && slot.TS.After(hitDeficitAt)
		if hasDeficit && canChargeFuture && !isAfterFutureDeficit {
			simInFuture := i > 0

			// these costs ignore the "now" hour so it can be compared against gridChargeNowCost.
			// They are called simPrevChargeCosts because they are the candidate price slots *previous*
			// to the simulated future hour `i` where we have a deficit, excluding the "now" hour at index 0.
			var simPrevChargeCosts []simPriceSlot
			if simInFuture {
				simPrevChargeCosts = make([]simPriceSlot, i)
				for j := 1; j <= i; j++ {
					simPrevChargeCosts[j-1] = simPriceSlot{
						cost:  simData[j].GridChargeDollarsPerKWH,
						ts:    simData[j].TS,
						price: simData[j].Price,
					}
				}
			}

			cheapestTime, cheapestPrice, cheapestCost, cheapestFutureChargeSlot := c.findCheapestPlan(simPrevChargeCosts, chargeDurationHours)

			// We determine if charging right now is significantly cheaper than the cheapest future planned slot.
			isSignificantlyCheaperNow := simInFuture && cheapestFutureChargeSlot.cost-gridChargeNowCost >= settings.MinDeficitPriceDifferenceDollarsPerKWH

			// We determine if the upcoming peak deficit price is high enough relative to the cheap window
			// cost to justify charging the battery at the cheap rate.
			isSignificantlyCheaperThanDeficit := simInFuture && slot.GridChargeDollarsPerKWH-cheapestFutureChargeSlot.cost >= settings.MinDeficitPriceDifferenceDollarsPerKWH

			// Stutter prevention: If the battery is already charging from the grid during the cheapest
			// window of the day, and charging is economically justified to cover the upcoming peak deficit,
			// continue charging now to avoid inverter start/stop wear.
			isCheapestWindow := cheapestFutureChargeSlot.cost == gridChargeNowCost && gridChargeNowCost <= minFutureGridChargeCost+0.001
			isAlreadyChargingSamePrice := canChargeNow && isAlreadyChargingGrid && isCheapestWindow && isSignificantlyCheaperThanDeficit

			if simInFuture && canChargeNow && (isSignificantlyCheaperNow || isAlreadyChargingSamePrice) {
				// count how many future cheap hours we have before the current hour i
				futureCheapHours := 0
				for j := 1; j < i; j++ {
					if simData[j].GridChargeDollarsPerKWH <= gridChargeNowCost+settings.MinDeficitPriceDifferenceDollarsPerKWH {
						futureCheapHours++
					}
				}

				// if there is a peak rate in the future (current price < max future price) and
				// we have enough cheap hours in the future before the deficit hour to satisfy
				// the remaining battery capacity, we can delay charging.
				if !isAlreadyChargingSamePrice && gridChargeNowCost < maxFutureGridChargeCost && futureCheapHours > 0 && float64(futureCheapHours)*chargeKW >= neededEnergy {
					// update planned charge details if this is the first one or is cheaper than
					// previously recorded planned charges
					if plannedChargeTime.IsZero() || cheapestCost < plannedChargeCost {
						plannedChargeTime = cheapestTime
						plannedChargePrice = cheapestPrice
						plannedChargeCost = cheapestCost
						log.Ctx(ctx).DebugContext(
							ctx,
							"deficit charge delayed; planned charge time updated",
							slog.Time("plannedChargeTime", plannedChargeTime),
							slog.Float64("plannedChargeCost", plannedChargeCost),
							slog.Float64("deficit", deficitAmount),
							slog.Int("futureCheapHours", futureCheapHours),
							slog.Bool("isAlreadyChargingGrid", isAlreadyChargingGrid),
							slog.Bool("isCheapestWindow", isCheapestWindow),
							slog.Bool("isAlreadyChargingSamePrice", isAlreadyChargingSamePrice),
							slog.Float64("predictedSolarKWH", slot.PredictedSolarKWH),
						)
					}
				} else {
					// we cannot delay charging, charge now.
					shouldCharge = true
					chargeDescription = fmt.Sprintf(
						"Projected Deficit at %s. Charge Now ($%.3f) <= Later ($%.3f).",
						hitDeficitAt.Format(time.Kitchen),
						gridChargeNowCost,
						cheapestFutureChargeSlot.cost,
					)
					futurePrice = &cheapestFutureChargeSlot.price
					chargeActionReason = types.ActionReasonDeficitChargeNow
					log.Ctx(ctx).DebugContext(
						ctx,
						"deficit predicted, charging now",
						slog.Float64("deficit", deficitAmount),
						slog.Time("deficitAt", hitDeficitAt),
						slog.Float64("chargeCost", gridChargeNowCost),
						slog.Float64("cheapestFutureCost", cheapestFutureChargeSlot.cost),
						slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
						slog.Time("cheapestFutureChargeTime", cheapestFutureChargeSlot.ts),
						slog.Int("chargeDurationHours", chargeDurationHours),
						slog.Bool("isAlreadyChargingGrid", isAlreadyChargingGrid),
						slog.Bool("isCheapestWindow", isCheapestWindow),
						slog.Bool("isAlreadyChargingSamePrice", isAlreadyChargingSamePrice),
						slog.Bool("isSignificantlyCheaperNow", isSignificantlyCheaperNow),
						slog.Float64("predictedSolarKWH", slot.PredictedSolarKWH),
					)
					break
				}
			}

			if simInFuture {
				// We determine if it is significantly cheaper to charge at a future planned slot before the deficit.
				// This condition is split into two logical paths:
				//
				// 1. If charging in the future is strictly and significantly cheaper than charging right now,
				//    we should wait and charge later.
				//
				// 2. We are already in a cheap window compared to now, but want to plan a charge time before an upcoming peak-rate deficit.
				//    Ensures the simulated deficit hour's cost is significantly more expensive than the cheapest future slot
				//    before the deficit, making it optimal to plan a charge.
				isSignificantlyCheaper := (gridChargeNowCost-cheapestFutureChargeSlot.cost > settings.MinDeficitPriceDifferenceDollarsPerKWH) ||
					(cheapestFutureChargeSlot.cost <= gridChargeNowCost &&
						slot.GridChargeDollarsPerKWH-cheapestFutureChargeSlot.cost >= settings.MinDeficitPriceDifferenceDollarsPerKWH)
				if isSignificantlyCheaper && (plannedChargeTime.IsZero() || cheapestFutureChargeSlot.cost < plannedChargeCost) {
					plannedChargeTime = cheapestFutureChargeSlot.ts
					plannedChargePrice = cheapestFutureChargeSlot.price
					plannedChargeCost = cheapestFutureChargeSlot.cost
					log.Ctx(ctx).DebugContext(
						ctx,
						"deficit predicted, planning to charge later",
						slog.Float64("deficit", deficitAmount),
						slog.Time("deficitAt", hitDeficitAt),
						slog.Float64("chargeCost", gridChargeNowCost),
						slog.Float64("cheapestFutureCost", cheapestFutureChargeSlot.cost),
						slog.Int("chargeDurationHours", chargeDurationHours),
						slog.Time("plannedChargeTime", plannedChargeTime),
						slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
						slog.Bool("isAlreadyChargingGrid", isAlreadyChargingGrid),
						slog.Bool("isCheapestWindow", isCheapestWindow),
						slog.Bool("isAlreadyChargingSamePrice", isAlreadyChargingSamePrice),
						slog.Float64("predictedSolarKWH", slot.PredictedSolarKWH),
					)
				}
			}
		}
	}

	// at this point it's opportunity cost because we either have enough energy
	// or it'll be cheaper later to charge.
	// We check for arbitrage export opportunities regardless of when we hit battery capacity,
	// because even if solar refills the battery later, we still want to hold the existing
	// energy in standby during cheap rates instead of discharging it prematurely.
	if !shouldCharge && plannedChargeTime.IsZero() && settings.GridExportSolar && soonestExportValue > 0 {
		minKWH := capacityKWH * (min(settings.MinBatterySOC+1.0, 100.0) / 100.0)
		standbyHitCapacityAt, standbyHitDeficitAt := c.simulateStandby(
			simData,
			soonestExportValue,
			settings.MinArbitrageDifferenceDollarsPerKWH,
			currentEnergyKWH,
			capacityKWH,
			minKWH,
		)

		effectiveExportValue := soonestExportValue
		if !standbyHitCapacityAt.IsZero() && !standbyHitCapacityAt.After(soonestExportAt) {
			// If we will hit capacity before the peak export time, holding energy in the battery
			// forces us to export solar at the capacity hour instead of storing it.
			// Therefore, the marginal value of holding energy is the solar export price at the capacity hour.
			var exportValueAtCapacity float64
			for _, slot := range simData {
				if slot.TS.Equal(standbyHitCapacityAt) || (slot.TS.Before(standbyHitCapacityAt) && slot.TS.Add(time.Hour).After(standbyHitCapacityAt)) {
					exportValueAtCapacity = slot.SolarOppDollarsPerKWH
					break
				}
			}
			effectiveExportValue = exportValueAtCapacity
		}

		// we hold the battery in standby if the effective value of the export is greater tha
		// the cost to cover home load today by at least the minimum arbitrage difference.
		// shifting load to the daytime peak via standby has no round-trip efficiency loss,
		// so it's profitable even for tiny price differences,
		// but we still require the min arbitrage difference threshold to avoid
		// low-value/wear standby holding.
		if effectiveExportValue-gridChargeNowCost > settings.MinArbitrageDifferenceDollarsPerKWH {
			chargeDescTemplate := "Arbitrage Opportunity (Export) at %s. Buy@%.3f -> Sell/Save@%.3f."
			chargeActionReason = types.ActionReasonArbitrageChargeExport
			holdReason := types.ActionReasonArbitrageHoldExport

			headroom := capacityKWH - currentEnergyKWH
			neededDurationHours := max(1, int((headroom/chargeKW + 0.84)))

			// Check if charging now would cause us to hit capacity before the peak export hour
			stepEnergyKWH := chargeKW * minChargeDurationHours
			standbyHitCapacityWithChargeAt, _ := c.simulateStandby(
				simData,
				soonestExportValue,
				settings.MinArbitrageDifferenceDollarsPerKWH,
				currentEnergyKWH+stepEnergyKWH,
				capacityKWH,
				minKWH,
			)
			willHitCapacityBeforePeak := !standbyHitCapacityWithChargeAt.IsZero() && !standbyHitCapacityWithChargeAt.After(soonestExportAt)

			// we only charge from the grid if we won't hit capacity before the peak.
			if canChargeArbitrage && !willHitCapacityBeforePeak {
				// Check if we can delay charging to a future cheap window before soonestExportAt
				var simPrevChargeCosts []simPriceSlot
				futureCheapHours := 0
				for _, slot := range simData {
					if slot.TS.Equal(now) {
						continue
					}
					if !slot.TS.Before(soonestExportAt) {
						break
					}
					simPrevChargeCosts = append(simPrevChargeCosts, simPriceSlot{
						cost:  slot.GridChargeDollarsPerKWH,
						ts:    slot.TS,
						price: slot.Price,
					})
					// We use the smaller MinDeficitPriceDifference here to compare charge-timing costs,
					// rather than MinArbitrageDifference, because we have already decided cycling the battery
					// is profitable, and now we are just optimizing charge timing without adding extra cycles.
					if slot.GridChargeDollarsPerKWH <= gridChargeNowCost+settings.MinDeficitPriceDifferenceDollarsPerKWH {
						futureCheapHours++
					}
				}

				cheapestTime, cheapestPrice, cheapestCost, cheapestFutureChargeSlot := c.findCheapestPlan(simPrevChargeCosts, neededDurationHours)

				// Compare charging now vs charging later using MinDeficitPriceDifference to choose
				// the cheapest charge window and avoid over-penalizing delay-charging decisions.
				isSignificantlyCheaperNow := len(simPrevChargeCosts) > 0 && cheapestFutureChargeSlot.cost-gridChargeNowCost >= settings.MinDeficitPriceDifferenceDollarsPerKWH

				if len(simPrevChargeCosts) > 0 && !isSignificantlyCheaperNow && futureCheapHours > 0 && float64(futureCheapHours)*chargeKW >= headroom {
					plannedChargeTime = cheapestTime
					plannedChargePrice = cheapestPrice
					plannedChargeCost = cheapestCost
					log.Ctx(ctx).DebugContext(
						ctx,
						"arbitrage charge delayed; planned charge time updated",
						slog.Time("plannedChargeTime", plannedChargeTime),
						slog.Float64("plannedChargeCost", plannedChargeCost),
						slog.Float64("headroom", headroom),
						slog.Int("futureCheapHours", futureCheapHours),
						slog.Time("soonestExportAt", soonestExportAt),
						slog.Float64("soonestExportValue", soonestExportValue),
						slog.Time("standbyHitCapacityWithChargeAt", standbyHitCapacityWithChargeAt),
						slog.Float64("stepEnergyKWH", stepEnergyKWH),
						slog.Int("neededDurationHours", neededDurationHours),
					)
				} else {
					shouldCharge = true
					chargeDescription = fmt.Sprintf(
						chargeDescTemplate,
						soonestExportAt.Format(time.Kitchen),
						gridChargeNowCost,
						soonestExportValue,
					)
					futurePrice = &soonestExportPrice
					log.Ctx(ctx).DebugContext(
						ctx,
						"arbitrage opportunity found, charging now",
						slog.Float64("buyAt", gridChargeNowCost),
						slog.Float64("sellAt", soonestExportValue),
						slog.Float64("diff", soonestExportValue-gridChargeNowCost),
						slog.Time("soonestExportAt", soonestExportAt),
						slog.Time("standbyHitCapacityWithChargeAt", standbyHitCapacityWithChargeAt),
						slog.Float64("stepEnergyKWH", stepEnergyKWH),
						slog.Float64("headroom", headroom),
						slog.Int("neededDurationHours", neededDurationHours),
					)
				}
			} else if (currentStatus.BatteryAboveMinSOC || currentStatus.ElevatedMinBatterySOC) && (standbyHitDeficitAt.IsZero() || standbyHitDeficitAt.After(soonestExportAt)) {
				// We hold the battery in standby if we cannot charge now.
				// Note that we check standbyHitDeficitAt to prioritize deficit handling if we would run out
				// of battery before the peak even under the dynamic standby model.
				var holdState string
				if currentStatus.BatterySOC >= 98.0 {
					holdState = "Battery full"
				} else if !settings.GridChargeBatteries {
					holdState = "Grid charging disabled"
				} else if currentStatus.BatteryChargingDisabled {
					holdState = "Charging disabled"
				} else {
					holdState = "Unable to charge"
				}

				holdType := "Export"

				holdDescription := fmt.Sprintf(
					"Arbitrage Opportunity (%s) at %s. %s. Hold energy.",
					holdType,
					soonestExportAt.Format(time.Kitchen),
					holdState,
				)
				log.Ctx(ctx).DebugContext(
					ctx,
					"arbitrage opportunity found but cannot charge, holding",
					slog.Float64("buyAt", gridChargeNowCost),
					slog.Float64("sellAt", soonestExportValue),
					slog.Float64("diff", soonestExportValue-gridChargeNowCost),
					slog.Time("soonestExportAt", soonestExportAt),
					slog.Time("standbyHitCapacityWithChargeAt", standbyHitCapacityWithChargeAt),
					slog.Float64("stepEnergyKWH", stepEnergyKWH),
					slog.Float64("headroom", headroom),
					slog.Int("neededDurationHours", neededDurationHours),
				)
				return decision(
					types.BatteryModeStandby,
					holdReason,
					holdDescription,
					&soonestExportPrice,
				), nil
			}
		}
	}

	// make sure we're not about to charge with only a few minutes left until peak
	// price which would mean we charge mostly in peak pricing because we only run
	// this decision periodically
	if shouldCharge {
		timeLeft := maxFutureGridChargeTime.Sub(now)
		if gridChargeNowCost < maxFutureGridChargeCost && timeLeft < 10*time.Minute {
			log.Ctx(ctx).DebugContext(
				ctx,
				"grid charging blocked: too close to peak price",
				slog.Duration("timeLeft", timeLeft),
				slog.Float64("priceNow", gridChargeNowCost),
				slog.Float64("maxPrice", maxFutureGridChargeCost),
				slog.Time("maxPriceStart", maxFutureGridChargeTime),
			)
			shouldCharge = false
		}
	}

	// if we should charge, return now.
	if shouldCharge {
		desc := fmt.Sprintf("Charging Optimized: %s", chargeDescription)
		return decision(types.BatteryModeChargeAny, chargeActionReason, desc, futurePrice), nil
	}

	// check if the battery is below the minimum SOC
	isBatteryAtReserve := !currentStatus.BatteryAboveMinSOC && !currentStatus.ElevatedMinBatterySOC
	if !isBatteryAtReserve && !hitDeficitAt.IsZero() {
		// or if we're about to hit the reserve within 5 minutes
		if hitDeficitAt.Before(now.Add(5 * time.Minute)) {
			isBatteryAtReserve = true
		}
	}

	// Optimization: If there is a future planned charge time, and the battery has enough energy
	// to last until that planned charge time without hitting a deficit, we should continue
	// using the battery (Load) now instead of standing by.
	//
	// This is valid if the planned future charge cost is cheaper than or equal to charging right now.
	if !plannedChargeTime.IsZero() && (hitDeficitAt.IsZero() || !hitDeficitAt.Before(plannedChargeTime)) && plannedChargeCost < gridChargeNowCost {
		loadReason := fmt.Sprintf("Sufficient battery to reach planned charge time at %s.", plannedChargeTime.Format(time.Kitchen))
		log.Ctx(ctx).DebugContext(
			ctx,
			"sufficient battery to reach charging window, using battery",
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Time("plannedChargeTime", plannedChargeTime),
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
		)
		return decision(types.BatteryModeLoad, types.ActionReasonSufficientBatteryTillCharge, loadReason, &plannedChargePrice), nil
	}

	// If we have plenty of battery (no deficit), Use it (Load).
	// If we have a deficit, but we are at the Highest Price, Use it (Load).
	// If we have a deficit, and cheaper now than later, Standby (Save for later).
	if !hitDeficitAt.IsZero() {
		// Optimization: If we hit full capacity BEFORE we hit the deficit, then
		// the current energy we have in the battery is "use it or lose it" effectively,
		// because we will refill to 100% anyway. So we should NOT Standby to save THIS energy.
		// don't bother using this reason if we're already above 90%
		if !hitCapacityAt.IsZero() && hitCapacityAt.Before(hitDeficitAt) {
			reason := types.ActionReasonDischargeBeforeCapacityNow
			loadReason := fmt.Sprintf("Capacity hit at %s before deficit at %s.", hitCapacityAt.Format(time.Kitchen), hitDeficitAt.Format(time.Kitchen))

			if !hitSolarCapacityAt.IsZero() && hitSolarCapacityAt.Before(hitDeficitAt) && hitCapacityAt.Equal(hitSolarCapacityAt) {
				reason = types.ActionReasonPreventSolarCurtailment
				loadReason = fmt.Sprintf("Solar curtailment likely at %s before deficit at %s.", hitSolarCapacityAt.Format(time.Kitchen), hitDeficitAt.Format(time.Kitchen))
			}

			log.Ctx(ctx).DebugContext(
				ctx,
				"deficit predicted but will refill to capacity before then",
				slog.Time("hitCapacityAt", hitCapacityAt),
				slog.Time("hitSolarCapacityAt", hitSolarCapacityAt),
				slog.Time("hitDeficitAt", hitDeficitAt),
				slog.String("reason", string(reason)),
			)
			return decision(types.BatteryModeLoad, reason, loadReason, nil), nil
		}

		// If there is a future planned charge time that occurs after 'now' but before the peak price
		// time (maxFutureGridChargeTime), we stand by to wait for that cheap charge window.
		//
		// However, we only stand by (instead of discharging the battery to cover load) if:
		// 1. The battery is already at its reserve limit (so it cannot discharge anyway).
		// 2. The planned charge cost is strictly cheaper than the current cost (making it financially
		//    optimal to preserve battery energy and wait to charge at the lower rate, rather than
		//    discharging now and having to recharge at a higher rate later).
		if !plannedChargeTime.IsZero() && plannedChargeTime.After(now) && plannedChargeTime.Before(maxFutureGridChargeTime) && (isBatteryAtReserve || plannedChargeCost <= gridChargeNowCost) {
			var standbyReason string
			if plannedChargeCost < gridChargeNowCost {
				standbyReason = fmt.Sprintf("Waiting to charge at %s ($%.3f < $%.3f).", plannedChargeTime.Format(time.Kitchen), plannedChargeCost, gridChargeNowCost)
			} else {
				standbyReason = fmt.Sprintf("Waiting to charge at %s ($%.3f).", plannedChargeTime.Format(time.Kitchen), plannedChargeCost)
			}
			log.Ctx(ctx).DebugContext(
				ctx,
				"waiting to charge",
				slog.Time("hitDeficitAt", hitDeficitAt),
				slog.Time("plannedChargeTime", plannedChargeTime),
				slog.Float64("plannedChargeCost", plannedChargeCost),
				slog.Float64("maxFutureGridChargeCost", maxFutureGridChargeCost),
				slog.Float64("gridChargeNowCost", gridChargeNowCost),
				slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
			)
			return decision(types.BatteryModeStandby, types.ActionReasonWaitingToCharge, standbyReason, &plannedChargePrice), nil
		}

		if isBatteryAtReserve {
			return decision(types.BatteryModeLoad, types.ActionReasonBatteryAtReserve, "Battery is at reserve. Using remaining energy because standby is not meaningful (battery is already held at reserve).", nil), nil
		}

		// We are going to run out. Should we save it?
		// Check if there is a significantly more expensive time later.
		// If current price is lower than maxFuturePrice, we should probably save it.
		if gridChargeNowCost < maxFutureGridChargeCost {
			standbyReason := fmt.Sprintf(
				"If discharged, battery would deplete at %s. "+
					"Since current price ($%.3f) is cheap and will remain cheap, "+
					"holding off charging and preserving battery energy for higher prices at %s ($%.3f < $%.3f).",
				hitDeficitAt.Format(time.Kitchen),
				gridChargeNowCost,
				maxFutureGridChargeTime.Format(time.Kitchen),
				gridChargeNowCost,
				maxFutureGridChargeCost,
			)
			log.Ctx(ctx).DebugContext(
				ctx,
				"deficit predicted, saving for peak",
				slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
				slog.Float64("maxFutureGridChargeCost", maxFutureGridChargeCost),
				slog.Time("maxFutureGridChargeTime", maxFutureGridChargeTime),
				slog.Time("hitDeficitAt", hitDeficitAt),
				slog.Time("plannedChargeTime", plannedChargeTime),
				slog.Float64("plannedChargeCost", plannedChargeCost),
				slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
				slog.Float64("gridChargeNowCost", gridChargeNowCost),
				slog.Float64("deficit", deficitAmount),
				slog.Int("chargeDurationHours", chargeDurationHours),
				slog.Float64("predictedSolarKWH", predictedSolarAtDeficitKWH),
			)
			return decision(types.BatteryModeStandby, types.ActionReasonDeficitSaveForPeak, standbyReason, &maxFutureGridChargePrice), nil
		}

		// If we are at the peak (or flat), use it until empty.
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit predicted but at peak price",
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("deficit", deficitAmount),
			slog.Int("chargeDurationHours", chargeDurationHours),
			slog.Float64("predictedSolarKWH", predictedSolarAtDeficitKWH),
		)
		return decision(types.BatteryModeLoad, types.ActionReasonArbitrageSave, "Deficit predicted but Current Price is Peak.", nil), nil
	}

	// No deficit predicted, use battery.
	log.Ctx(ctx).DebugContext(
		ctx,
		"no deficit predicted, using battery",
		slog.Float64("minEnergy", minEnergy),
		slog.Float64("maxEnergy", maxEnergy),
	)
	return decision(types.BatteryModeLoad, types.ActionReasonSufficientBattery, "Sufficient battery.", nil), nil
}

// findCheapestPlan finds the planned charge details and the marginal cheapest slot from candidate slots.
func (c *Controller) findCheapestPlan(
	simPrevChargeCosts []simPriceSlot,
	neededDurationHours int,
) (cheapestTime time.Time, cheapestPrice types.Price, cheapestCost float64, marginalSlot simPriceSlot) {
	if len(simPrevChargeCosts) == 0 {
		return
	}

	sort.Slice(simPrevChargeCosts, func(a, b int) bool {
		if simPrevChargeCosts[a].cost != simPrevChargeCosts[b].cost {
			return simPrevChargeCosts[a].cost < simPrevChargeCosts[b].cost
		}
		return simPrevChargeCosts[a].ts.After(simPrevChargeCosts[b].ts)
	})

	idx := neededDurationHours
	if idx > len(simPrevChargeCosts) {
		idx = len(simPrevChargeCosts)
	}

	marginalSlot = simPrevChargeCosts[idx-1]

	cheapestTime = simPrevChargeCosts[0].ts
	cheapestPrice = simPrevChargeCosts[0].price
	cheapestCost = simPrevChargeCosts[0].cost

	for j := 1; j < idx; j++ {
		if simPrevChargeCosts[j].ts.Before(cheapestTime) {
			cheapestTime = simPrevChargeCosts[j].ts
			cheapestPrice = simPrevChargeCosts[j].price
			cheapestCost = simPrevChargeCosts[j].cost
		}
	}
	return
}

// simulateStandby simulates the battery progression under a dynamic standby model.
// For hours where the price is cheap, we hold the battery in standby (no load discharge, only charge from solar).
// For hours where the price is expensive, we discharge the battery to cover load.
// It returns the hitCapacityAt and hitDeficitAt times under this model.
func (c *Controller) simulateStandby(
	simData []SimHour,
	exportPrice float64,
	minArbitrageDiff float64,
	currentEnergyKWH float64,
	capacityKWH float64,
	minKWH float64,
) (time.Time, time.Time) {
	batteryEnergy := currentEnergyKWH
	var hitCapacityAt time.Time
	var hitDeficitAt time.Time

	// We default to a 98% capacity threshold to avoid rapid start/stop controller
	// oscillations when the battery is nearly full, or use the pre-calculated threshold from slot.
	capacityThresholdKWH := capacityKWH * 0.98
	if len(simData) > 0 && simData[0].CapacityThresholdKWH > 0 {
		capacityThresholdKWH = simData[0].CapacityThresholdKWH
	}

	for _, slot := range simData {
		// Use the pre-calculated EnergyApplyRatio from simulation (e.g. for fractional first hours).
		// If it is 0.0 (like in manually constructed test fixtures), default to 1.0 (full hour).
		simEnergyApplyRatio := slot.EnergyApplyRatio
		if simEnergyApplyRatio == 0.0 {
			simEnergyApplyRatio = 1.0
		}

		clampedNetKWH := slot.ClampedNetLoadSolarKWH

		// We only standby if the simulated hour's price is cheap compared to the target export price.
		// If it's expensive (grid cost >= exportPrice - minArbitrageDiff), we discharge the battery to cover the load.
		// We use a tiny epsilon of 1e-9 to prevent floating point precision inaccuracies from affecting comparisons.
		shouldDischarge := slot.GridChargeDollarsPerKWH >= exportPrice-minArbitrageDiff-1e-9

		var appliedNetKWH float64
		if shouldDischarge {
			appliedNetKWH = clampedNetKWH // cover home load (discharging if positive, solar charging if negative)
		} else {
			if clampedNetKWH < 0 {
				appliedNetKWH = clampedNetKWH // standby holds energy but still charges from solar surplus
			} else {
				appliedNetKWH = 0.0 // standby holds energy; do not discharge to cover load
			}
		}

		// Calculate simulated energy at the end of the slot based on the apply ratio
		newEnergy := batteryEnergy - (appliedNetKWH * simEnergyApplyRatio)

		// Check if solar charging pushes us above the capacity threshold.
		// If so, interpolate the exact fraction of the hour when the threshold was crossed.
		if (appliedNetKWH < 0 && newEnergy >= capacityThresholdKWH) || newEnergy > capacityKWH {
			if hitCapacityAt.IsZero() {
				remainingBeforeCapacity := capacityThresholdKWH - batteryEnergy
				if remainingBeforeCapacity > 0 && appliedNetKWH < 0 {
					fraction := remainingBeforeCapacity / -appliedNetKWH
					// Round to nearest nanosecond to avoid off-by-one float-casting issues.
					hitCapacityAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
				} else {
					hitCapacityAt = slot.TS
				}
			}
		}

		if appliedNetKWH > 0 {
			// Draining/Discharging
			// Check if we will drop below the minimum reserve limit.
			// If so, interpolate the exact fraction of the hour when we hit the reserve.
			if newEnergy < minKWH {
				if hitDeficitAt.IsZero() {
					remainingBeforeMin := batteryEnergy - minKWH
					if remainingBeforeMin > 0 {
						fraction := remainingBeforeMin / appliedNetKWH
						// Round to nearest nanosecond to avoid off-by-one float-casting issues.
						hitDeficitAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
					} else {
						hitDeficitAt = slot.TS
					}
				}
				// Clamped at minimum reserve limit; battery cannot physically drain below minKWH.
				batteryEnergy = minKWH
			} else {
				batteryEnergy = newEnergy
			}
		} else {
			// Charging or holding
			// Clamped at maximum battery capacity.
			if newEnergy > capacityKWH {
				batteryEnergy = capacityKWH
			} else {
				batteryEnergy = newEnergy
			}
		}

		// If we've already determined both capacity and deficit hit times, we can stop simulating.
		if !hitCapacityAt.IsZero() && !hitDeficitAt.IsZero() {
			break
		}
	}

	return hitCapacityAt, hitDeficitAt
}
