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
type Controller struct {
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

	// Rule 1: If the price is negative, then don't export anything to the grid.
	if currentPrice.DollarsPerKWH < 0 {
		solarMode = types.SolarModeNoExport
		log.Ctx(ctx).DebugContext(ctx, "price is negative, disabling solar export", slog.Float64("price", currentPrice.DollarsPerKWH))
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

	// assume we need to charge for at least 10 minutes for it to be worth it
	minChargeDurationHours := 10.0 / 60.0
	simEnergyAfterCharge := currentEnergyKWH + chargeKW*minChargeDurationHours
	canCharge := simEnergyAfterCharge < capacityKWH && settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled

	simData := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, settings)

	shouldCharge := false
	var chargeDescription string
	var chargeActionReason types.ActionReason
	var futurePrice *types.Price

	maxFutureGridChargeCost := gridChargeNowCost
	maxFutureGridChargePrice := currentPrice
	maxFutureGridChargeTime := now
	for _, slot := range simData {
		if slot.GridChargeDollarsPerKWH > maxFutureGridChargeCost {
			maxFutureGridChargeCost = slot.GridChargeDollarsPerKWH
			maxFutureGridChargePrice = slot.Price
			maxFutureGridChargeTime = slot.TS
		}
	}

	// track simulated energy
	continuousPeakLoadKWH := 0.0
	var hitDeficitAt time.Time
	var hitCapacityAt time.Time
	var hitSolarCapacityAt time.Time
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
		var simPrevCheapestCost float64
		if i > 0 {
			simInFuture = true
			simPrevChargeCosts = make([]float64, i)
			for j := 1; j <= i; j++ {
				simPrevChargeCosts[j-1] = simData[j].GridChargeDollarsPerKWH
			}
			sort.Float64s(simPrevChargeCosts)
			simPrevCheapestCost = simPrevChargeCosts[0]
		}

		isAboveMinDeficitPriceDifference := simInFuture && slot.GridChargeDollarsPerKWH > simPrevCheapestCost+settings.MinDeficitPriceDifferenceDollarsPerKWH
		// update continuous peak load variables for each slot where the price
		// is elevated above the min deficit price difference
		if isAboveMinDeficitPriceDifference {
			// only record the clamped net load if it's positive otherwise we charged
			// the battery and that's accounted for already in the BatteryCapacityKWHIfStandby
			// slot tracking
			if slot.ClampedNetLoadSolarKWH > 0 {
				continuousPeakLoadKWH += slot.ClampedNetLoadSolarKWH
			}
		} else {
			continuousPeakLoadKWH = 0.0
		}

		// update simulated energy state
		// if we ever hit the capacity of the battery, we can't store any more power
		// so we set hitCapacity to true so we never try to charge since that power
		// would be meaningless to pull from the grid since we end up filling up
		// the batteries without the grid in the simulation anyways
		if !slot.HitCapacityAt.IsZero() && hitCapacityAt.IsZero() {
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy hit capacity",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("capacityKWH", capacityKWH),
				slog.Int("simHour", slot.Hour),
			)
			hitCapacityAt = slot.TS
		}

		if !slot.HitSolarCapacityAt.IsZero() && hitSolarCapacityAt.IsZero() {
			log.Ctx(ctx).DebugContext(
				ctx,
				"simulated energy hit solar headroom capacity",
				slog.Float64("batteryKWH", slot.BatteryKWH),
				slog.Float64("capacityKWH", capacityKWH),
				slog.Int("simHour", slot.Hour),
			)
			hitSolarCapacityAt = slot.TS
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

		// if we are importing, we avoid the import cost
		// if we are exporting, we get the export value
		if slot.NetLoadSolarKWH > 0 {
			if slot.GridChargeDollarsPerKWH > highestExportValue {
				highestExportValue = slot.GridChargeDollarsPerKWH
				highestExportAt = slot.TS
				highestExportPrice = slot.Price
			}
		} else {
			if slot.SolarOppDollarsPerKWH > highestExportValue {
				highestExportValue = slot.SolarOppDollarsPerKWH
				highestExportAt = slot.TS
				highestExportPrice = slot.Price
			}
		}

		if slot.TotalBatteryDeficitKWH > 0 && canCharge {
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
					slot.TS.Format(time.Kitchen),
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
				)
				break
			} else {
				// don't plan to charge after the deficit time and only plan to charge if
				// the difference is sigificant
				isBeforeDeficit := !cheapestFutureChargeTime.After(hitDeficitAt)
				isSignificantlyCheaper := gridChargeNowCost-plannedChargeCost > settings.MinDeficitPriceDifferenceDollarsPerKWH
				if isBeforeDeficit && isSignificantlyCheaper && (plannedChargeTime.IsZero() || cheapestFutureChargeCost < plannedChargeCost) {
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

		// check for peak survival after deficit handling but before arbitrage
		// this is checking if we hold the battery standby can we survive the peak
		// normally we are checking if we have a deficit when using the battery
		// but here we are checking if we have enough energy to survive the peak
		// without using the battery
		if isAboveMinDeficitPriceDifference && canCharge {
			remaining := slot.BatteryKWHIfStandby - continuousPeakLoadKWH
			// if we already hit capacity before the peak there's no need to do anything
			// because the battery can't hold more
			if remaining < slot.BatteryReserveKWH && hitCapacityAt.IsZero() {
				// We only charge NOW to survive the peak if NOW is significantly cheaper
				// than the cheapest opportunity to charge between now and the peak.
				// If we already missed our chance and NOW is almost as expensive as the peak,
				// we just ride it out and let the regular logic handle actual shortages.
				if gridChargeNowCost+settings.MinDeficitPriceDifferenceDollarsPerKWH <= simPrevCheapestCost {
					shouldCharge = true
					chargeActionReason = types.ActionReasonChargeSurvivePeak
					chargeDescription = fmt.Sprintf(
						"Cannot survive peak pricing at %s ($%.3f).",
						slot.TS.Format(time.Kitchen),
						slot.GridChargeDollarsPerKWH,
					)
					cannotSurvivePrice := slot.Price
					futurePrice = &cannotSurvivePrice
					log.Ctx(ctx).DebugContext(
						ctx,
						"charging to survive peak",
						slog.Float64("remaining", remaining),
						slog.Float64("reserve", slot.BatteryReserveKWH),
						slog.Float64("peakLoadKWH", continuousPeakLoadKWH),
						slog.Float64("standbyCap", slot.BatteryKWHIfStandby),
						slog.Float64("simPrevCheapestCost", simPrevCheapestCost),
						slog.Float64("currentCost", slot.GridChargeDollarsPerKWH),
					)
					break
				}
			}
		}
	}

	// at this point it's opportunity cost because we either have enough energy
	// or it'll be cheaper later to charge
	// make sure we can actually export something otherwise there's no reason to
	// arbitrage but we can't check export battery since we don't support that yet
	// if we're going to hit capacity there's no reason to try and charge now
	if !shouldCharge && canCharge && plannedChargeTime.IsZero() && settings.GridExportSolar && highestExportValue > 0 && (hitCapacityAt.IsZero() || hitCapacityAt.After(highestExportAt)) {
		// if the value we get later minus our cost to charge now is greater than
		// the minimum arbitrage difference, we should charge now
		if highestExportValue-gridChargeNowCost > settings.MinArbitrageDifferenceDollarsPerKWH {
			shouldCharge = true
			chargeDescription = fmt.Sprintf(
				"Arbitrage Opportunity at %s. Buy@%.3f -> Sell/Save@%.3f.",
				highestExportAt.Format(time.Kitchen),
				gridChargeNowCost,
				highestExportValue,
			)
			chargeActionReason = types.ActionReasonArbitrageChargeNow
			futurePrice = &highestExportPrice
			log.Ctx(ctx).DebugContext(
				ctx,
				"arbitrage opportunity found",
				slog.Float64("buyAt", gridChargeNowCost),
				slog.Float64("sellAt", highestExportValue),
				slog.Float64("diff", highestExportValue-gridChargeNowCost),
			)
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

		// We are going to run out. Should we save it?
		// Check if there is a significantly more expensive time later.
		// If current price is lower than maxFuturePrice, we should probably save it.
		if gridChargeNowCost < maxFutureGridChargeCost {
			// if we have a planned charge time, we should record as waiting to charge
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
				)
				return decision(types.BatteryModeStandby, types.ActionReasonWaitingToCharge, standbyReason, &plannedChargePrice, hitDeficitAt, hitCapacityAt), nil
			}

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
			)
			return decision(types.BatteryModeStandby, types.ActionReasonDeficitSaveForPeak, standbyReason, &maxFutureGridChargePrice, hitDeficitAt, hitCapacityAt), nil
		}
		// If we are at the peak (or flat), use it until empty.
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit predicted but at peak price",
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
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
