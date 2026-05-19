package controller

import (
	"context"
	"fmt"
	"log/slog"
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

	// Helper to build final action
	decision := func(batteryMode types.BatteryMode, reason types.ActionReason, modeReason string, futurePrice *types.Price, hitDeficitAt time.Time, hitCapacityAt time.Time) Decision {
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
		return decision(types.BatteryModeStandby, types.ActionReasonMissingBattery, "Battery Config Missing or Capacity 0. Standby.", nil, time.Time{}, time.Time{}), nil
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
		return decision(types.BatteryModeChargeAny, types.ActionReasonAlwaysChargeBelowThreshold, desc, nil, time.Time{}, time.Time{}), nil
	}

	// Rule 3: Charge now if its cheaper than later, if we will run out of energy
	// or if we can make more money buying now and selling later (arbitrage)

	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		// conservatively assume it takes 3 hours to charge the battery from 0->100
		chargeKW = capacityKWH / 3.0
	}
	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0

	// add a 0.1 kWh buffer to prevent trying to charge when we are almost full (e.g. 99.9%)
	canChargeAny := currentEnergyKWH+0.1 < capacityKWH && settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled

	// assume we need to charge for at least 10 minutes for it to be worth it for arbitrage
	minChargeDurationHours := 10.0 / 60.0
	simEnergyAfterCharge := currentEnergyKWH + chargeKW*minChargeDurationHours
	canChargeArbitrage := simEnergyAfterCharge < capacityKWH && canChargeAny

	shouldCharge := false
	var chargeDescription string
	var chargeActionReason types.ActionReason
	var futurePrice *types.Price

	var hitDeficitAt time.Time
	var hitCapacityAt time.Time
	var hitSolarCapacityAt time.Time
	for _, slot := range simData {
		if !slot.HitCapacityAt.IsZero() && hitCapacityAt.IsZero() {
			hitCapacityAt = slot.HitCapacityAt
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy hit capacity",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("capacityKWH", capacityKWH),
				slog.Int("simHour", slot.Hour),
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
			)
		}
	}

	maxFutureGridChargeCost := gridChargeNowCost
	maxFutureGridChargePrice := currentPrice
	maxFutureGridChargeTime := now
	for _, slot := range simData {
		// don't bother recording any grid charge costs after we hit capacity
		if !hitCapacityAt.IsZero() && slot.TS.After(hitCapacityAt) {
			break
		}
		if slot.GridChargeDollarsPerKWH > maxFutureGridChargeCost {
			maxFutureGridChargeCost = slot.GridChargeDollarsPerKWH
			maxFutureGridChargePrice = slot.Price
			maxFutureGridChargeTime = slot.TS
		}
	}

	// track simulated energy
	var plannedChargeTime time.Time
	var plannedChargePrice types.Price
	var plannedChargeCost float64
	var highestExportValue float64
	var highestExportPrice types.Price
	var highestExportAt time.Time
	minEnergy := -1.0
	maxEnergy := -1.0

	for i, slot := range simData {
		simInFuture := false

		// these costs ignore the "now" hour so it can be compared against gridChargeNowCost
		var simPrevChargeCosts []float64
		if i > 0 {
			simInFuture = true
			simPrevChargeCosts = make([]float64, i)
			for j := 1; j <= i; j++ {
				simPrevChargeCosts[j-1] = simData[j].GridChargeDollarsPerKWH
			}
			sort.Float64s(simPrevChargeCosts)
		}

		if minEnergy == -1 || slot.BatteryKWH < minEnergy {
			minEnergy = slot.BatteryKWH
		}
		if maxEnergy == -1 || slot.BatteryKWH > maxEnergy {
			maxEnergy = slot.BatteryKWH
		}

		// check if we are below the minimum SOC and when we need to charge
		if !slot.HitDeficitAt.IsZero() && hitDeficitAt.IsZero() {
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy below minimum SOC causing a deficit",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("reserveKWH", slot.BatteryReserveKWH),
				slog.Int("simHour", slot.Hour),
				slog.Time("hitAt", slot.HitDeficitAt),
			)
			hitDeficitAt = slot.HitDeficitAt
		}

		// we only track the export value for arbitrage since we cannot export battery
		// to the grid. If we have a deficit, it is handled by the deficit logic.
		// "Save" arbitrage (avoiding grid import) is redundant and causes unnecessary
		// charging when there is no deficit.
		if slot.NetLoadSolarKWH <= 0 {
			if slot.SolarOppDollarsPerKWH > highestExportValue {
				highestExportValue = slot.SolarOppDollarsPerKWH
				highestExportAt = slot.TS
				highestExportPrice = slot.Price
			}
		}

		// the simulation resets the total deficit when we hit capacity
		if slot.TotalBatteryDeficitKWH > 0 && canChargeAny {
			deficitAmount := slot.TotalBatteryDeficitKWH

			// future in this section is actually in the PAST from the current
			// simulation hour but in the future compared to the real time
			var cheapestFutureChargeCost float64
			var cheapestFutureChargePrice types.Price
			var cheapestFutureChargeTime time.Time

			// factor in the cost of charging for the duration of the charge which
			// means we need to look at the nth cheapest charge cost
			// round up the hours we need to charge except for a little buffer
			chargeDurationHours := max(1, int((float64(deficitAmount)/chargeKW + 0.84)))

			if simInFuture {
				if chargeDurationHours > len(simPrevChargeCosts) {
					cheapestFutureChargeCost = simPrevChargeCosts[len(simPrevChargeCosts)-1]
				} else {
					cheapestFutureChargeCost = simPrevChargeCosts[chargeDurationHours-1]
				}

				// Find the price that matches the cheapest future cost
				for j := 1; j <= i; j++ {
					if simData[j].GridChargeDollarsPerKWH == cheapestFutureChargeCost {
						cheapestFutureChargePrice = simData[j].Price
						cheapestFutureChargeTime = simData[j].TS
						break
					}
				}
			}

			// if we have determined we'll run out of energy and it's cheaper to
			// charge now than later, and we have room in the battery, charge now
			if simInFuture && gridChargeNowCost+settings.MinDeficitPriceDifferenceDollarsPerKWH <= cheapestFutureChargeCost {
				shouldCharge = true
				chargeDescription = fmt.Sprintf(
					"Projected Deficit at %s. Charge Now ($%.3f) <= Later ($%.3f) - Delta ($%.3f).",
					hitDeficitAt.Format(time.Kitchen),
					gridChargeNowCost,
					cheapestFutureChargeCost,
					settings.MinDeficitPriceDifferenceDollarsPerKWH,
				)
				futurePrice = &cheapestFutureChargePrice
				chargeActionReason = types.ActionReasonDeficitChargeNow
				log.Ctx(ctx).DebugContext(
					ctx,
					"deficit predicted, charging now",
					slog.Float64("deficit", deficitAmount),
					slog.Time("deficitAt", hitDeficitAt),
					slog.Float64("chargeCost", gridChargeNowCost),
					slog.Float64("cheapestFutureCost", cheapestFutureChargeCost),
					slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
					slog.Time("cheapestFutureChargeTime", cheapestFutureChargeTime),
					slog.Int("chargeDurationHours", chargeDurationHours),
				)
				break
			}

			if len(simPrevChargeCosts) > 0 {
				isSignificantlyCheaper := gridChargeNowCost-cheapestFutureChargeCost > settings.MinDeficitPriceDifferenceDollarsPerKWH
				if isSignificantlyCheaper && (plannedChargeTime.IsZero() || cheapestFutureChargeCost < plannedChargeCost) {
					plannedChargeTime = cheapestFutureChargeTime
					plannedChargePrice = cheapestFutureChargePrice
					plannedChargeCost = cheapestFutureChargeCost
					log.Ctx(ctx).DebugContext(
						ctx,
						"deficit predicted, planning to charge later",
						slog.Float64("deficit", deficitAmount),
						slog.Time("deficitAt", hitDeficitAt),
						slog.Float64("chargeCost", gridChargeNowCost),
						slog.Float64("cheapestFutureCost", cheapestFutureChargeCost),
						slog.Int("chargeDurationHours", chargeDurationHours),
						slog.Time("plannedChargeTime", plannedChargeTime),
						slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
					)
				}
			}
		}
	}

	// at this point it's opportunity cost because we either have enough energy
	// or it'll be cheaper later to charge
	// make sure we can actually export something otherwise there's no reason to
	// arbitrage but we can't check export battery since we don't support that yet
	// if we're going to hit capacity there's no reason to try and charge now
	if !shouldCharge && plannedChargeTime.IsZero() && settings.GridExportSolar && highestExportValue > 0 && (hitCapacityAt.IsZero() || hitCapacityAt.After(highestExportAt)) {
		// if the value we get later minus our cost to charge now is greater than
		// the minimum arbitrage difference, we should charge now
		if highestExportValue-gridChargeNowCost > settings.MinArbitrageDifferenceDollarsPerKWH {
			chargeDescTemplate := "Arbitrage Opportunity (Export) at %s. Buy@%.3f -> Sell/Save@%.3f."
			chargeActionReason = types.ActionReasonArbitrageChargeExport
			holdReason := types.ActionReasonArbitrageHoldExport

			if canChargeArbitrage {
				shouldCharge = true
				chargeDescription = fmt.Sprintf(
					chargeDescTemplate,
					highestExportAt.Format(time.Kitchen),
					gridChargeNowCost,
					highestExportValue,
				)
				futurePrice = &highestExportPrice
				log.Ctx(ctx).DebugContext(
					ctx,
					"arbitrage opportunity found",
					slog.Float64("buyAt", gridChargeNowCost),
					slog.Float64("sellAt", highestExportValue),
					slog.Float64("diff", highestExportValue-gridChargeNowCost),
				)
			} else if currentStatus.BatteryAboveMinSOC || currentStatus.ElevatedMinBatterySOC {
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
					highestExportAt.Format(time.Kitchen),
					holdState,
				)
				log.Ctx(ctx).DebugContext(
					ctx,
					"arbitrage opportunity found but cannot charge, holding",
					slog.Float64("buyAt", gridChargeNowCost),
					slog.Float64("sellAt", highestExportValue),
					slog.Float64("diff", highestExportValue-gridChargeNowCost),
				)
				return decision(
					types.BatteryModeStandby,
					holdReason,
					holdDescription,
					&highestExportPrice,
					hitDeficitAt,
					hitCapacityAt,
				), nil
			}
		}
	}

	// if we should charge, return now.
	if shouldCharge {
		desc := fmt.Sprintf("Charging Optimized: %s", chargeDescription)
		return decision(types.BatteryModeChargeAny, chargeActionReason, desc, futurePrice, hitDeficitAt, hitCapacityAt), nil
	}

	// check if the battery is below the minimum SOC
	isBatteryAtReserve := !currentStatus.BatteryAboveMinSOC && !currentStatus.ElevatedMinBatterySOC
	if !isBatteryAtReserve && !hitDeficitAt.IsZero() {
		// or if we're about to hit the reserve within 5 minutes
		if hitDeficitAt.Before(now.Add(5 * time.Minute)) {
			isBatteryAtReserve = true
		}
	}
	if isBatteryAtReserve {
		return decision(types.BatteryModeLoad, types.ActionReasonBatteryAtReserve, "Battery is at reserve. Using remaining energy.", nil, hitDeficitAt, hitCapacityAt), nil
	}

	// If we have plenty of battery (no deficit), Use it (Load).
	// If we have a deficit, but we are at the Highest Price, Use it (Load).
	// If we have a deficit, and cheaper now than later, Standby (Save for later).
	if !hitDeficitAt.IsZero() {
		// Optimization: If we hit full capacity BEFORE we hit the deficit, then
		// the current energy we have in the battery is "use it or lose it" effectively,
		// because we will refill to 100% anyway. So we should NOT Standby to save THIS energy.
		// don't bother using this reason if we're already above 90%
		if (!hitCapacityAt.IsZero() && hitCapacityAt.Before(hitDeficitAt)) || (!hitSolarCapacityAt.IsZero() && hitSolarCapacityAt.Before(hitDeficitAt)) {
			reason := types.ActionReasonDischargeBeforeCapacityNow
			loadReason := fmt.Sprintf("Capacity hit at %s before deficit at %s.", hitCapacityAt.Format(time.Kitchen), hitDeficitAt.Format(time.Kitchen))

			if !hitSolarCapacityAt.IsZero() && hitSolarCapacityAt.Before(hitDeficitAt) && (hitCapacityAt.IsZero() || !hitSolarCapacityAt.After(hitCapacityAt)) {
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
			return decision(types.BatteryModeLoad, reason, loadReason, nil, hitDeficitAt, hitCapacityAt), nil
		}

		// Optimization: If we have a planned charge time, and we have enough battery to last until
		// that planned charge time without hitting a deficit, we should continue to use the battery
		// (Load) now, and then charge it up later when it is cheap.
		if !plannedChargeTime.IsZero() && !hitDeficitAt.Before(plannedChargeTime) {
			loadReason := fmt.Sprintf("Sufficient battery to reach planned charge time at %s.", plannedChargeTime.Format(time.Kitchen))
			log.Ctx(ctx).DebugContext(
				ctx,
				"sufficient battery to reach charging window, using battery",
				slog.Time("hitDeficitAt", hitDeficitAt),
				slog.Time("plannedChargeTime", plannedChargeTime),
				slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
			)
			return decision(types.BatteryModeLoad, types.ActionReasonSufficientBattery, loadReason, &plannedChargePrice, hitDeficitAt, hitCapacityAt), nil
		}

		// if we have a planned charge time, we should record as waiting to charge
		// but only if it's before the maxFutureGridChargeTime otherwise we should
		// report that we're saving the battery for the upcoming peak and then use
		// the battery during that peak
		if !plannedChargeTime.IsZero() && plannedChargeTime.After(now) && plannedChargeTime.Before(maxFutureGridChargeTime) {
			standbyReason := fmt.Sprintf("Waiting to charge at %s ($%.3f < $%.3f).", plannedChargeTime.Format(time.Kitchen), plannedChargeCost, gridChargeNowCost)
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
			return decision(types.BatteryModeStandby, types.ActionReasonWaitingToCharge, standbyReason, &plannedChargePrice, hitDeficitAt, hitCapacityAt), nil
		}

		// We are going to run out. Should we save it?
		// Check if there is a significantly more expensive time later.
		// If current price is lower than maxFuturePrice, we should probably save it.
		if gridChargeNowCost < maxFutureGridChargeCost {
			standbyReason := fmt.Sprintf("Deficit predicted at %s and higher prices at %s ($%.3f < $%.3f).", hitDeficitAt.Format(time.Kitchen), maxFutureGridChargeTime.Format(time.Kitchen), gridChargeNowCost, maxFutureGridChargeCost)
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
			)
			return decision(types.BatteryModeStandby, types.ActionReasonDeficitSaveForPeak, standbyReason, &maxFutureGridChargePrice, hitDeficitAt, hitCapacityAt), nil
		}

		// If we are at the peak (or flat), use it until empty.
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit predicted but at peak price",
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Float64("minDeficitPriceDifference", settings.MinDeficitPriceDifferenceDollarsPerKWH),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
		)
		return decision(types.BatteryModeLoad, types.ActionReasonArbitrageSave, "Deficit predicted but Current Price is Peak.", nil, hitDeficitAt, hitCapacityAt), nil
	}

	// No deficit predicted, use battery.
	log.Ctx(ctx).DebugContext(
		ctx,
		"no deficit predicted, using battery",
		slog.Float64("minEnergy", minEnergy),
		slog.Float64("maxEnergy", maxEnergy),
	)
	return decision(types.BatteryModeLoad, types.ActionReasonSufficientBattery, "Sufficient battery.", nil, hitDeficitAt, hitCapacityAt), nil
}
