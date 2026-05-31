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
	Action types.Action
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

// DecisionResult represents the components of a decision.
type DecisionResult struct {
	BatteryMode types.BatteryMode
	Reason      types.ActionReason
	Description string
	FuturePrice *types.Price
}

// PlannedCharge represents the details of a planned future charge.
type PlannedCharge struct {
	Time  time.Time
	Price types.Price
	Cost  float64
}

// futurePlan represents a planned future charge window.
type futurePlan struct {
	ChargeTime  time.Time
	ChargePrice types.Price
	ChargeCost  float64
}

// StrategyEvaluation represents the economic evaluation of a potential strategy.
type StrategyEvaluation struct {
	Decision       *DecisionResult
	Plan           *futurePlan
	BenefitDollars float64
}

// simulationSummary holds structural markers extracted from simulation data.
type simulationSummary struct {
	HitCapacityAt              time.Time
	HitStandbyCapacityAt       time.Time
	HitSolarCapacityAt         time.Time
	HitDeficitAt               time.Time
	HitBelowDeficitAt          time.Time
	HitAboveDeficitAt          time.Time
	PredictedSolarAtDeficitKWH float64
	SoonestExportAt            time.Time
	SoonestExportValue         float64
	SoonestExportPrice         types.Price
	SoonestSaveAt              time.Time
	SoonestSaveValue           float64
	SoonestSavePrice           types.Price
	MinFutureGridChargeCost    float64
	MinEnergy                  float64
	MaxEnergy                  float64
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

	// if the solar export value is negative, then don't export solar to the grid.
	if solarMode != types.SolarModeNoExport && len(simData) > 0 && !simData[0].TS.After(now) && simData[0].SolarOppDollarsPerKWH < 0 {
		solarMode = types.SolarModeNoExport
		log.Ctx(ctx).DebugContext(ctx, "solar export value is negative, disabling solar export",
			slog.Float64("price", currentPrice.DollarsPerKWH),
			slog.Float64("solarOppValue", simData[0].SolarOppDollarsPerKWH))
	}

	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		return Decision{
			Action: types.Action{
				Timestamp:    now.UTC(),
				BatteryMode:  types.BatteryModeStandby,
				SolarMode:    solarMode,
				Reason:       types.ActionReasonMissingBattery,
				Description:  "Battery Config Missing or Capacity 0. Standby.",
				CurrentPrice: &currentPrice,
				SystemStatus: currentStatus,
			},
		}, nil
	}

	// gridChargeNowCost represents the total marginal cost of importing power from the grid right now,
	// combining the supply rate (DollarsPerKWH) and delivery/distribution fees (GridUseDollarsPerKWH).
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	// Price Threshold Force Charge:
	// If the marginal cost of importing grid energy is extremely cheap (at or below AlwaysChargeUnderDollarsPerKWH),
	// we immediately decide to charge the battery. This overrides all other optimization evaluations because
	// the energy is essentially free or so cheap that it is guaranteed to yield maximum economic savings, regardless
	// of load shapes, solar forecasts, or future peaks.
	if !currentStatus.BatteryChargingDisabled && gridChargeNowCost <= settings.AlwaysChargeUnderDollarsPerKWH {
		desc := fmt.Sprintf(
			"Price Low (%.3f < %.3f). Charging.",
			gridChargeNowCost,
			settings.AlwaysChargeUnderDollarsPerKWH,
		)
		if solarMode == types.SolarModeNoExport {
			desc += " (Export Disabled)"
		}
		log.Ctx(ctx).DebugContext(ctx, "price below always charge threshold", slog.Float64("price", gridChargeNowCost), slog.Float64("threshold", settings.AlwaysChargeUnderDollarsPerKWH))
		return Decision{
			Action: types.Action{
				Timestamp:    now.UTC(),
				BatteryMode:  types.BatteryModeChargeAny,
				SolarMode:    solarMode,
				Reason:       types.ActionReasonAlwaysChargeBelowThreshold,
				Description:  desc,
				CurrentPrice: &currentPrice,
				SystemStatus: currentStatus,
			},
		}, nil
	}

	// Run simulation analysis to locate key markers
	summary := c.analyzeSimulation(ctx, now, currentPrice, settings, simData)

	log.Ctx(ctx).DebugContext(ctx, "simulation analysis summary", slog.Any("summary", summary))

	// Helper to build the final Decision object using the summary's computed times.
	buildFinalDecision := func(dr *DecisionResult) Decision {
		return Decision{
			Action: types.Action{
				Timestamp:         now.UTC(),
				BatteryMode:       dr.BatteryMode,
				SolarMode:         solarMode,
				Reason:            dr.Reason,
				Description:       dr.Description,
				CurrentPrice:      &currentPrice,
				FuturePrice:       dr.FuturePrice,
				SystemStatus:      currentStatus,
				HitDeficitAt:      summary.HitDeficitAt,
				HitBelowDeficitAt: summary.HitBelowDeficitAt,
				HitAboveDeficitAt: summary.HitAboveDeficitAt,
				HitCapacityAt:     summary.HitCapacityAt,
			},
		}
	}

	evalDeficit := c.evaluateDeficit(ctx, now, currentStatus, currentPrice, settings, simData, summary)
	evalExport := c.evaluateExportArbitrage(ctx, now, currentStatus, currentPrice, settings, simData, summary)

	var bestImmediate *StrategyEvaluation
	var bestPlan *StrategyEvaluation

	evals := []*StrategyEvaluation{evalDeficit, evalExport}

	for _, e := range evals {
		if e != nil {
			if e.Decision != nil {
				if bestImmediate == nil || e.BenefitDollars > bestImmediate.BenefitDollars {
					bestImmediate = e
				}
			}
			if e.Plan != nil {
				if bestPlan == nil || e.Plan.ChargeTime.Before(bestPlan.Plan.ChargeTime) || (e.Plan.ChargeTime.Equal(bestPlan.Plan.ChargeTime) && e.BenefitDollars > bestPlan.BenefitDollars) {
					bestPlan = e
				}
			}
		}
	}

	if bestImmediate != nil {
		log.Ctx(ctx).DebugContext(ctx, "immediate decision chosen",
			slog.String("mode", drModeString(bestImmediate.Decision.BatteryMode)),
			slog.String("reason", string(bestImmediate.Decision.Reason)),
			slog.Float64("benefitDollars", bestImmediate.BenefitDollars),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("batterySOC", currentStatus.BatterySOC),
			slog.Bool("batteryChargingDisabled", currentStatus.BatteryChargingDisabled),
			slog.Float64("minDeficitPriceDiff", settings.MinDeficitPriceDifferenceDollarsPerKWH),
			slog.Float64("minArbitragePriceDiff", settings.MinArbitrageDifferenceDollarsPerKWH),
			slog.Time("hitDeficitAt", summary.HitDeficitAt),
			slog.Time("hitCapacityAt", summary.HitCapacityAt),
		)
		dec := buildFinalDecision(bestImmediate.Decision)
		dec.Action.StrategyBenefitDollars = bestImmediate.BenefitDollars
		return dec, nil
	}

	var activePlan *PlannedCharge
	if bestPlan != nil {
		activePlan = &PlannedCharge{
			Time:  bestPlan.Plan.ChargeTime,
			Price: bestPlan.Plan.ChargePrice,
			Cost:  bestPlan.Plan.ChargeCost,
		}
		log.Ctx(ctx).DebugContext(ctx, "evaluated planned charges",
			slog.Bool("hasActivePlan", true),
			slog.Time("activePlanTime", activePlan.Time),
			slog.Float64("activePlanCost", activePlan.Cost),
			slog.Float64("benefitDollars", bestPlan.BenefitDollars),
		)
	}

	if activePlan != nil {
		planDecision := c.evaluatePlannedCharge(ctx, now, currentStatus, currentPrice, settings, simData, summary, *activePlan)
		log.Ctx(ctx).DebugContext(ctx, "executing active planned charge",
			slog.Time("planTime", activePlan.Time),
			slog.Float64("planCost", activePlan.Cost),
			slog.String("mode", drModeString(planDecision.BatteryMode)),
			slog.String("reason", string(planDecision.Reason)),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("batterySOC", currentStatus.BatterySOC),
		)
		dec := buildFinalDecision(planDecision)
		if dec.Action.Reason == types.ActionReasonWaitingToCharge || dec.Action.Reason == types.ActionReasonDeficitSaveForPeak {
			dec.Action.StrategyBenefitDollars = bestPlan.BenefitDollars
		}
		return dec, nil
	}

	fallbackDecision := c.evaluateFallback(ctx, now, currentStatus, currentPrice, settings, simData, summary)
	log.Ctx(ctx).DebugContext(ctx, "falling back to economical decision",
		slog.String("mode", drModeString(fallbackDecision.BatteryMode)),
		slog.String("reason", string(fallbackDecision.Reason)),
		slog.Float64("gridChargeNowCost", gridChargeNowCost),
		slog.Float64("batterySOC", currentStatus.BatterySOC),
		slog.Bool("batteryChargingDisabled", currentStatus.BatteryChargingDisabled),
		slog.Bool("hasActivePlan", activePlan != nil),
	)
	return buildFinalDecision(fallbackDecision), nil
}

// analyzeSimulation scans the simulation data in a single pass to collect all key markers.
func (c *Controller) analyzeSimulation(
	ctx context.Context,
	now time.Time,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
) simulationSummary {
	var summary simulationSummary
	summary.MinEnergy = -1.0
	summary.MaxEnergy = -1.0

	// 1. Scan for capacity hits
	for _, slot := range simData {
		if !slot.HitCapacityAt.IsZero() && summary.HitCapacityAt.IsZero() {
			summary.HitCapacityAt = slot.HitCapacityAt
		}
		if !slot.HitStandbyCapacityAt.IsZero() && summary.HitStandbyCapacityAt.IsZero() {
			summary.HitStandbyCapacityAt = slot.HitStandbyCapacityAt
		}
		if !slot.HitSolarCapacityAt.IsZero() && summary.HitSolarCapacityAt.IsZero() {
			summary.HitSolarCapacityAt = slot.HitSolarCapacityAt
		}
	}
	if !summary.HitSolarCapacityAt.IsZero() && (summary.HitCapacityAt.IsZero() || summary.HitSolarCapacityAt.Before(summary.HitCapacityAt)) {
		summary.HitCapacityAt = summary.HitSolarCapacityAt
	}

	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	summary.MinFutureGridChargeCost = gridChargeNowCost
	maxFutureGridChargeCost := gridChargeNowCost

	minArbitrageDiff := max(0.001, settings.MinArbitrageDifferenceDollarsPerKWH)

	for _, slot := range simData {
		// Calculate the absolute max future grid charge cost across the entire simulation window
		// to identify the true peak hours of the day.
		if slot.GridChargeDollarsPerKWH > maxFutureGridChargeCost {
			maxFutureGridChargeCost = slot.GridChargeDollarsPerKWH
		}

		// For the min future grid charge cost (the cheapest charging window), we can ignore slots
		// after we hit capacity because we cannot charge any further once full.
		if !summary.HitCapacityAt.IsZero() && summary.HitCapacityAt.After(now) && slot.TS.After(summary.HitCapacityAt) {
			continue
		}
		if slot.GridChargeDollarsPerKWH < summary.MinFutureGridChargeCost {
			summary.MinFutureGridChargeCost = slot.GridChargeDollarsPerKWH
		}
	}

	// 3. Scan for energy trends, deficits, and export arbitrage opportunities
	for _, slot := range simData {
		if summary.MinEnergy == -1 || slot.BatteryKWH < summary.MinEnergy {
			summary.MinEnergy = slot.BatteryKWH
		}
		if summary.MaxEnergy == -1 || slot.BatteryKWH > summary.MaxEnergy {
			summary.MaxEnergy = slot.BatteryKWH
		}

		hasDeficit := slot.TotalBatteryDeficitKWH > 0

		// Populate HitBelowDeficitAt if a deficit is predicted (with the 3% buffer)
		if hasDeficit && !slot.HitBelowDeficitAt.IsZero() && summary.HitBelowDeficitAt.IsZero() {
			summary.HitBelowDeficitAt = slot.HitBelowDeficitAt
			summary.PredictedSolarAtDeficitKWH = slot.PredictedSolarKWH
		}
		// Populate HitAboveDeficitAt if we drop below reserve (with the 1% safety buffer)
		if !slot.HitAboveDeficitAt.IsZero() && summary.HitAboveDeficitAt.IsZero() {
			summary.HitAboveDeficitAt = slot.HitAboveDeficitAt
		}
		// Populate HitDeficitAt if we drop below reserve exactly (no buffer)
		if !slot.HitDeficitAt.IsZero() && summary.HitDeficitAt.IsZero() {
			summary.HitDeficitAt = slot.HitDeficitAt
		}

		if slot.NetLoadSolarKWH <= -0.05 && slot.TS.After(now) {
			if summary.SoonestExportValue == 0 && slot.SolarOppDollarsPerKWH-summary.MinFutureGridChargeCost > minArbitrageDiff {
				summary.SoonestExportValue = slot.SolarOppDollarsPerKWH
				summary.SoonestExportAt = slot.TS
				summary.SoonestExportPrice = slot.Price
			}
		}
		// Save arbitrage targets hours with net home load (NetLoadSolarKWH > 0) that have peak prices.
		// We only select slots that are at or near the maximum future price of the day (maxFutureGridChargeCost)
		// to ensure we only arbitrage for true peak price periods.
		if slot.NetLoadSolarKWH > 0 && slot.GridChargeDollarsPerKWH >= maxFutureGridChargeCost-0.001 && slot.TS.After(now) {
			if summary.SoonestSaveValue == 0 && slot.GridChargeDollarsPerKWH-summary.MinFutureGridChargeCost > minArbitrageDiff {
				summary.SoonestSaveValue = slot.GridChargeDollarsPerKWH
				summary.SoonestSaveAt = slot.TS
				summary.SoonestSavePrice = slot.Price
			}
		}
	}

	return summary
}

// evaluateDeficit evaluates if we need to charge immediately to cover a future deficit, or plans a future charge,
// or if we need to standby now to save battery for a future peak.
func (c *Controller) evaluateDeficit(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
) *StrategyEvaluation {
	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		chargeKW = currentStatus.BatteryCapacityKWH / 3.0
	}
	capacityKWH := currentStatus.BatteryCapacityKWH
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	// isAlreadyChargingGrid indicates if the system is currently actively drawing power from the grid
	// to charge the battery. (GridKW > 0 and BatteryKW < 0).
	isAlreadyChargingGrid := currentStatus.BatteryKW < 0 && currentStatus.GridKW > 0

	// Charge Hysteresis & Anti-Oscillation:
	// We calculate the minimum required physical battery capacity headroom (in kWh) before initiating a charge.
	// 1. To start charging: We require at least 5 minutes of charging capacity at max charge rate (startChargeHeadroom),
	//    clamped at a minimum of 0.3 kWh. This ensures we do not trigger tiny grid-charging sessions
	//    when the battery is already extremely close to 100% (which causes rapid relays toggling and reduces battery life).
	// 2. Hysteresis (to continue charging): If we are already grid-charging, we lower the required headroom to 0.1 kWh.
	//    This permits the battery to continue charging until it is almost completely full, preventing short-cycling
	//    right at the end of the charge.
	minStartChargeDurationHours := 5.0 / 60.0
	startChargeHeadroom := chargeKW * minStartChargeDurationHours
	if startChargeHeadroom < 0.3 {
		startChargeHeadroom = 0.3
	}
	var allowedHeadroom float64
	if isAlreadyChargingGrid {
		allowedHeadroom = 0.1
	} else {
		allowedHeadroom = startChargeHeadroom
	}

	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0
	canChargeNow := currentEnergyKWH+allowedHeadroom < capacityKWH && settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled
	canChargeFuture := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled

	minDeficitDiff := max(0.001, settings.MinDeficitPriceDifferenceDollarsPerKWH)

	// We look for a deficit hit in the future. We prioritize HitBelowDeficitAt (which
	// requires the battery to drop at least 3% below the reserve threshold) to
	// filter out minor fluctuations and avoid micro-charging the battery.
	// We fall back to HitDeficitAt (the exact reserve threshold crossing) if
	// HitBelowDeficitAt is zero.
	hitDeficitAt := summary.HitBelowDeficitAt
	if hitDeficitAt.IsZero() {
		hitDeficitAt = summary.HitDeficitAt
	}
	if hitDeficitAt.IsZero() {
		return nil
	}

	// Marginal Deficit Cost Estimation:
	// We calculate the average cost per kWh of the future predicted deficit.
	// Crucially, we inspect the simulation hour-by-hour and accumulate the marginal deficit
	// (the increase in deficit in each specific hour) multiplied by that hour's retail grid price.
	// This gives us the exact weighted average rate we will pay if we do NOT charge the battery now.
	// Comparing our current grid charge cost against this average deficit cost lets us calculate
	// the actual monetary benefit (savings in dollars) of charging now to avoid the deficit.
	totalDeficitCost := 0.0
	totalDeficitKWH := 0.0
	lastDeficitKWH := 0.0
	for _, slot := range simData {
		// If the battery hits capacity, any subsequent deficits cannot be prevented by charging now.
		if !summary.HitCapacityAt.IsZero() && !slot.TS.Before(summary.HitCapacityAt) {
			break
		}
		marginalDeficit := slot.TotalBatteryDeficitKWH - lastDeficitKWH
		if marginalDeficit > 0 {
			totalDeficitCost += marginalDeficit * slot.GridChargeDollarsPerKWH
			totalDeficitKWH += marginalDeficit
			lastDeficitKWH = slot.TotalBatteryDeficitKWH
		}
	}
	averageDeficitRateDollarsPerKWH := gridChargeNowCost
	if totalDeficitKWH > 0 {
		averageDeficitRateDollarsPerKWH = totalDeficitCost / totalDeficitKWH
		log.Ctx(ctx).DebugContext(
			ctx,
			"calculated average deficit rate",
			slog.Float64("averageRateDollarsPerKWH", averageDeficitRateDollarsPerKWH),
			slog.Float64("totalDeficitKWH", totalDeficitKWH),
			slog.Time("hitBelowDeficitAt", summary.HitBelowDeficitAt),
			slog.Time("hitDeficitAt", summary.HitDeficitAt),
			slog.Time("hitCapacityAt", summary.HitCapacityAt),
		)
	}

	var plannedChargeTime time.Time
	var plannedChargePrice types.Price
	var plannedChargeCost float64
	var shouldCharge bool
	var chargeDescription string
	var chargeActionReason types.ActionReason
	var futurePrice *types.Price
	var chargeBenefitDollars float64
	var planBenefitDollars float64

	for i, slot := range simData {
		deficitAmount := slot.TotalBatteryDeficitKWH
		hasDeficit := deficitAmount > 0

		isAfterFutureDeficit := !hitDeficitAt.IsZero() && hitDeficitAt.After(now.Add(time.Hour)) && slot.TS.After(hitDeficitAt)
		if hasDeficit && canChargeFuture && !isAfterFutureDeficit {
			simInFuture := i > 0
			var simPrevChargeCosts []simPriceSlot
			if simInFuture {
				for j := 1; j <= i; j++ {
					candidateTS := simData[j].TS
					// Ensure we're on the same side of capacity
					if !summary.HitCapacityAt.IsZero() && slot.TS.After(summary.HitCapacityAt) && !candidateTS.After(summary.HitCapacityAt) {
						continue
					}
					simPrevChargeCosts = append(simPrevChargeCosts, simPriceSlot{
						cost:  simData[j].GridChargeDollarsPerKWH,
						ts:    simData[j].TS,
						price: simData[j].Price,
					})
				}
			}

			maxHeadroom := capacityKWH - slot.BatteryReserveKWH
			if maxHeadroom < 0 {
				maxHeadroom = 0
			}
			neededEnergy := deficitAmount
			if neededEnergy > maxHeadroom {
				neededEnergy = maxHeadroom
			}
			chargeDurationHours := neededEnergy / chargeKW
			effectiveGridChargeNowCost := gridChargeNowCost

			// 10-Minute Boundary Penalty:
			// If the current price slot is ending within 10 minutes and the next hour is more expensive,
			// we assume the effective cost of starting a charge now is the next hour's higher cost.
			// This prevents starting a grid charge at 11:58 PM under a cheap rate when it will immediately
			// rollover into an expensive 12:00 AM rate. We only apply this lookahead penalty when not
			// already charging, so we don't prematurely stop an active charge.
			if len(simData) > 1 && !currentPrice.TSEnd.IsZero() && currentPrice.TSEnd.Sub(now) < 10*time.Minute {
				nextPrice := simData[1].Price
				nextPriceCost := nextPrice.DollarsPerKWH + nextPrice.GridUseDollarsPerKWH
				if nextPriceCost > gridChargeNowCost && !isAlreadyChargingGrid {
					effectiveGridChargeNowCost = nextPriceCost
					log.Ctx(ctx).DebugContext(ctx, "Applied 10-minute price lookahead penalty", slog.Float64("originalCost", gridChargeNowCost), slog.Float64("effectiveCost", effectiveGridChargeNowCost))
				}
			}

			canChargeNowForSlot := canChargeNow
			if !summary.HitCapacityAt.IsZero() && summary.HitCapacityAt.After(now) && slot.TS.After(summary.HitCapacityAt) {
				canChargeNowForSlot = false
			}

			if simInFuture && len(simPrevChargeCosts) > 0 {
				cheapestTime, cheapestPrice, cheapestCost, cheapestFutureChargeSlot := c.findCheapestPlan(simPrevChargeCosts, chargeDurationHours)

				isSignificantlyCheaperNow := false
				if simInFuture {
					if effectiveGridChargeNowCost > gridChargeNowCost {
						// If the 10-minute lookahead penalty is active, require a strictly greater price difference
						// to justify starting a charge otherwise let's just wait until the next hour to start charging
						isSignificantlyCheaperNow = cheapestFutureChargeSlot.cost-effectiveGridChargeNowCost > minDeficitDiff
					} else {
						isSignificantlyCheaperNow = cheapestFutureChargeSlot.cost-effectiveGridChargeNowCost >= minDeficitDiff
					}
				}

				// Price comparisons for charging decisions:
				// - isSignificantlyCheaperFuture: True if waiting until a future hour to charge saves us at least minDeficitDiff.
				// - isSignificantlyCheaperThanDeficit: True if the future cheapest slot is significantly cheaper than the deficit peak price itself.
				// - isSignificantlyCheaperThanDeficitNow: True if right now is significantly cheaper than the deficit peak price itself. We use this
				//   for hysteresis checking during active charge sessions so lookahead penalty doesn't prematurely kill them.
				// - isCheapestWindow: True if right now is tied for the absolute cheapest window before the deficit.
				isSignificantlyCheaperFuture := simInFuture && effectiveGridChargeNowCost-cheapestFutureChargeSlot.cost >= minDeficitDiff
				isSignificantlyCheaperThanDeficit := simInFuture && slot.GridChargeDollarsPerKWH-cheapestFutureChargeSlot.cost >= minDeficitDiff
				isSignificantlyCheaperThanDeficitNow := simInFuture && slot.GridChargeDollarsPerKWH-gridChargeNowCost >= minDeficitDiff
				// We use gridChargeNowCost (ignoring the 10-minute lookahead penalty) because the lookahead penalty
				// is only meant to prevent starting a new charge close to a price increase, not to prematurely stop
				// an active charge during its cheapest window.
				isCheapestWindow := cheapestFutureChargeSlot.cost == gridChargeNowCost && gridChargeNowCost <= summary.MinFutureGridChargeCost

				// Hysteresis: If we are already charging from grid and right now is tied for the cheapest window,
				// we keep charging. This prevents starting and stopping charging when multiple hours are equally cheap.
				isAlreadyChargingSamePrice := canChargeNowForSlot && isAlreadyChargingGrid && isCheapestWindow && isSignificantlyCheaperThanDeficitNow

				if simInFuture && canChargeNowForSlot && (isSignificantlyCheaperNow || isAlreadyChargingSamePrice) {
					// Count how many future cheap hours are available to decide if we can safely delay.
					futureCheapHours := 0
					for j := 1; j < i; j++ {
						candidateTS := simData[j].TS
						if !summary.HitCapacityAt.IsZero() && slot.TS.After(summary.HitCapacityAt) && !candidateTS.After(summary.HitCapacityAt) {
							continue
						}
						if simData[j].GridChargeDollarsPerKWH <= effectiveGridChargeNowCost+minDeficitDiff {
							futureCheapHours++
						}
					}

					// We only delay charging if there is a future slot that is significantly cheaper,
					// and there is enough cumulative charging capacity (futureCheapHours * chargeKW) to
					// cover the deficit otherwise we might as well continue charging now and re-evaluate
					// delaying later.
					canDelay := !isAlreadyChargingSamePrice && isSignificantlyCheaperFuture && futureCheapHours > 0 && float64(futureCheapHours)*chargeKW >= neededEnergy

					if canDelay {
						log.Ctx(ctx).DebugContext(ctx, "delaying deficit charge",
							slog.Time("until", cheapestTime),
							slog.Float64("cheapestCost", cheapestCost),
							slog.Float64("effectiveGridChargeNowCost", effectiveGridChargeNowCost),
							slog.Float64("neededEnergy", neededEnergy),
							slog.Int("futureCheapHours", futureCheapHours),
							slog.Bool("isAlreadyChargingSamePrice", isAlreadyChargingSamePrice),
							slog.Bool("isSignificantlyCheaperFuture", isSignificantlyCheaperFuture),
							slog.Bool("isAlreadyChargingGrid", isAlreadyChargingGrid),
							slog.Any("cheapestFutureChargeSlot", cheapestFutureChargeSlot),
							slog.Time("hitDeficitAt", hitDeficitAt),
						)
						if plannedChargeTime.IsZero() || cheapestCost < plannedChargeCost {
							plannedChargeTime = cheapestTime
							plannedChargePrice = cheapestPrice
							plannedChargeCost = cheapestCost
							planBenefitDollars = neededEnergy * (averageDeficitRateDollarsPerKWH - plannedChargeCost)
						}
					} else {
						log.Ctx(ctx).DebugContext(ctx, "charging now to avoid deficit",
							slog.Float64("effectiveGridChargeNowCost", effectiveGridChargeNowCost),
							slog.Time("cheapestTime", cheapestFutureChargeSlot.ts),
							slog.Float64("cheapestCost", cheapestFutureChargeSlot.cost),
							slog.Float64("neededEnergy", neededEnergy),
							slog.Bool("isSignificantlyCheaperNow", isSignificantlyCheaperNow),
							slog.Bool("isAlreadyChargingSamePrice", isAlreadyChargingSamePrice),
							slog.Bool("isSignificantlyCheaperFuture", isSignificantlyCheaperFuture),
							slog.Bool("isAlreadyChargingGrid", isAlreadyChargingGrid),
							slog.Any("cheapestFutureChargeSlot", cheapestFutureChargeSlot),
							slog.Time("hitDeficitAt", hitDeficitAt),
						)
						shouldCharge = true
						chargeDescription = fmt.Sprintf(
							"Projected Deficit at %s. Charge Now ($%.3f) <= Later ($%.3f).",
							hitDeficitAt.Format(time.Kitchen),
							effectiveGridChargeNowCost,
							cheapestFutureChargeSlot.cost,
						)
						futurePrice = &cheapestFutureChargeSlot.price
						chargeActionReason = types.ActionReasonDeficitChargeNow
						chargeBenefitDollars = neededEnergy * (averageDeficitRateDollarsPerKWH - effectiveGridChargeNowCost)
						break
					}
				}

				if simInFuture {
					isSignificantlyCheaper := isSignificantlyCheaperFuture ||
						(cheapestFutureChargeSlot.cost <= gridChargeNowCost && isSignificantlyCheaperThanDeficit)
					if isSignificantlyCheaper && (plannedChargeTime.IsZero() || cheapestFutureChargeSlot.cost < plannedChargeCost) {
						plannedChargeTime = cheapestFutureChargeSlot.ts
						plannedChargePrice = cheapestFutureChargeSlot.price
						plannedChargeCost = cheapestFutureChargeSlot.cost
						planBenefitDollars = neededEnergy * (averageDeficitRateDollarsPerKWH - plannedChargeCost)
					}
				}
			}
		}
	}

	// 1. Deficit Standby Logic (Save for Peak):
	// If we must cover a future deficit, we evaluate if we should standby now to preserve battery energy.
	// The rate of using 1 kWh of battery energy now is the rate of refilling it later:
	// - If we have a planned cheap charge before the deficit, the refilling rate is plannedChargeCost.
	// - If no cheap charge is planned, we would pay the averageDeficitRateDollarsPerKWH (since we'd buy that energy at peak rates).
	var standbyBenefit float64
	var standbyReason string
	var standbyFuturePrice *types.Price
	var refillRateDollarsPerKWH float64

	if plannedChargeTime.IsZero() {
		refillRateDollarsPerKWH = averageDeficitRateDollarsPerKWH
	} else {
		refillRateDollarsPerKWH = plannedChargeCost
	}

	// Since we are not charging, we standby to preserve energy for the deficit.
	// As long as right now is not cheaper than our rate of refilling (with a 0.001 floating point buffer), we standby.
	standbyThreshold := refillRateDollarsPerKWH - 0.001

	if gridChargeNowCost <= standbyThreshold {
		standbyBenefit = currentEnergyKWH * (averageDeficitRateDollarsPerKWH - gridChargeNowCost)

		if !plannedChargeTime.IsZero() {
			standbyReason = fmt.Sprintf("Projected Deficit at %s. Waiting to charge.", hitDeficitAt.Format(time.Kitchen))
		} else {
			standbyReason = fmt.Sprintf("Projected Deficit at %s. Will deplete battery otherwise.", hitDeficitAt.Format(time.Kitchen))
		}

		// Find the peak price to set as future price
		var peakPrice types.Price
		maxCost := 0.0
		for _, slot := range simData {
			if !slot.TS.Before(hitDeficitAt) && slot.GridChargeDollarsPerKWH > maxCost {
				maxCost = slot.GridChargeDollarsPerKWH
				peakPrice = slot.Price
			}
		}
		standbyFuturePrice = &peakPrice
	}

	var decision *DecisionResult
	var benefit float64

	if shouldCharge {
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit charge evaluated",
			slog.Float64("chargeBenefit", chargeBenefitDollars),
			slog.String("reason", string(chargeActionReason)),
			slog.Float64("averageDeficitRateDollarsPerKWH", averageDeficitRateDollarsPerKWH),
			slog.Float64("plannedChargeCost", plannedChargeCost),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Any("futurePrice", futurePrice),
		)
		decision = &DecisionResult{
			BatteryMode: types.BatteryModeChargeAny,
			Reason:      chargeActionReason,
			Description: fmt.Sprintf("Charging Optimized: %s", chargeDescription),
			FuturePrice: futurePrice,
		}
		benefit = chargeBenefitDollars
	} else if standbyBenefit > 0 {
		log.Ctx(ctx).DebugContext(ctx, "deficit standby evaluated",
			slog.Float64("standbyBenefit", standbyBenefit),
			slog.String("reason", string(standbyReason)),
			slog.Float64("averageDeficitRateDollarsPerKWH", averageDeficitRateDollarsPerKWH),
			slog.Float64("plannedChargeCost", plannedChargeCost),
			slog.Time("hitDeficitAt", hitDeficitAt),
			slog.Any("standbyFuturePrice", standbyFuturePrice),
		)
		decision = &DecisionResult{
			BatteryMode: types.BatteryModeStandby,
			Reason:      types.ActionReasonDeficitSaveForPeak,
			Description: standbyReason,
			FuturePrice: standbyFuturePrice,
		}
		benefit = standbyBenefit
	}

	var plan *futurePlan
	if !plannedChargeTime.IsZero() {
		plan = &futurePlan{
			ChargeTime:  plannedChargeTime,
			ChargePrice: plannedChargePrice,
			ChargeCost:  plannedChargeCost,
		}
		// If we don't have an immediate decision but we have a plan, set benefit
		if decision == nil {
			benefit = planBenefitDollars
		} else if planBenefitDollars > benefit {
			// If plan has higher benefit than immediate, update the combined benefit
			benefit = planBenefitDollars
		}
	}

	if decision != nil || plan != nil {
		log.Ctx(ctx).DebugContext(ctx, "evaluateDeficit returning strategy",
			slog.Bool("hasDecision", decision != nil),
			slog.Bool("hasPlan", plan != nil),
			slog.Float64("benefitDollars", benefit),
		)
		return &StrategyEvaluation{
			Decision:       decision,
			Plan:           plan,
			BenefitDollars: benefit,
		}
	}

	return nil
}

// evaluateExportArbitrage evaluates export arbitrage opportunities (charging from grid/solar
// to export later at peak times).
//
// We only perform grid-charging arbitrage for Export Arbitrage (exporting solar/stored energy
// at peak export rates). We do not arbitrage grid energy to cover home load (Save Arbitrage).
// In practice, save arbitrage is rarely useful because any site with home load and no solar (or
// solar insufficient to cover the load) will inevitably experience a battery deficit. That deficit
// is handled by evaluateDeficit, which automatically schedules grid-charging at the cheapest off-peak
// hours to cover the deficit. This achieves identical economic results.
func (c *Controller) evaluateExportArbitrage(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
) *StrategyEvaluation {
	if !settings.GridExportSolar || summary.SoonestExportValue <= 0 {
		return nil
	}

	targetValue := summary.SoonestExportValue
	targetAt := summary.SoonestExportAt
	targetPrice := summary.SoonestExportPrice

	capacityKWH := currentStatus.BatteryCapacityKWH
	currentEnergyKWH := currentStatus.BatterySOC * capacityKWH / 100.0
	chargeKW := currentStatus.MaxBatteryChargeKW
	if chargeKW <= 0 {
		chargeKW = capacityKWH / 3.0
	}
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	minKWH := capacityKWH * (min(settings.MinBatterySOC+1.0, 100.0) / 100.0)
	minArbitrageDiff := max(0.001, settings.MinArbitrageDifferenceDollarsPerKWH)
	minDeficitDiff := max(0.001, settings.MinDeficitPriceDifferenceDollarsPerKWH)

	// Standby Simulation for Arbitrage:
	// We simulate the battery behavior assuming we standby during cheap hours (preserving energy)
	// and discharge only during the target peak hours.
	standbyHitCapacityAt, standbyHitAboveDeficitAt, _ := c.simulateStandby(
		simData,
		targetValue-minArbitrageDiff,
		currentEnergyKWH,
		capacityKWH,
		minKWH,
	)

	// Solar Capacity Offset Value Adjustment:
	// If the standby simulation indicates the battery will hit capacity (e.g. from solar)
	// BEFORE the target peak hour, then we cannot store any more grid energy without forcing solar
	// to either be exported early (if export is enabled) or curtailed (if export is disabled).
	// In either case, the opportunity cost of filling the battery from the grid is the value of the
	// solar energy at the time of the capacity hit. We adjust effectiveExportValue to this rate.
	effectiveExportValue := targetValue
	if !standbyHitCapacityAt.IsZero() && !standbyHitCapacityAt.After(targetAt) {
		var exportValueAtCapacity float64
		var exportValueAtCapacityTime time.Time
		buffer := 1 * time.Hour
		windowStart := standbyHitCapacityAt
		windowEnd := standbyHitCapacityAt.Add(buffer)

		for _, slot := range simData {
			slotStart := slot.TS
			slotEnd := slot.TS.Add(time.Hour)

			overlapStart := slotStart
			if windowStart.After(overlapStart) {
				overlapStart = windowStart
			}
			overlapEnd := slotEnd
			if windowEnd.Before(overlapEnd) {
				overlapEnd = windowEnd
			}

			if overlapStart.Before(overlapEnd) {
				val := slot.SolarOppDollarsPerKWH
				if val > exportValueAtCapacity {
					exportValueAtCapacity = val
					exportValueAtCapacityTime = slot.TS
				}
			}
		}
		effectiveExportValue = exportValueAtCapacity
		log.Ctx(ctx).DebugContext(
			ctx,
			"arbitrage capacity hit delayed export value",
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Time("exportValueAtCapacityTime", exportValueAtCapacityTime),
			slog.Time("windowStart", windowStart),
			slog.Time("windowEnd", windowEnd),
			slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
		)
	}

	isAlreadyChargingGrid := currentStatus.BatteryKW < 0 && currentStatus.GridKW > 0
	effectiveGridChargeNowCost := gridChargeNowCost

	// 10-Minute Boundary Penalty:
	// If the current price slot is ending within 10 minutes and the next hour is more expensive,
	// we assume the effective cost of starting a charge now is the next hour's higher cost.
	// This prevents starting a grid charge at 11:58 PM under a cheap rate when it will immediately
	// rollover into an expensive 12:00 AM rate. We only apply this lookahead penalty when not
	// already charging, so we don't prematurely stop an active charge.
	if len(simData) > 1 && !currentPrice.TSEnd.IsZero() && currentPrice.TSEnd.Sub(now) < 10*time.Minute {
		nextPrice := simData[1].Price
		nextPriceCost := nextPrice.DollarsPerKWH + nextPrice.GridUseDollarsPerKWH
		if nextPriceCost > gridChargeNowCost && !isAlreadyChargingGrid {
			effectiveGridChargeNowCost = nextPriceCost
		}
	}

	// chargeCostNow represents the grid charge cost we evaluate against.
	// If we are already grid-charging, we bypass the 10-minute price lookahead boundary penalty
	// (using the actual current price instead of the next hour's price) so we do not prematurely
	// stop an ongoing charge session right before a price change.
	chargeCostNow := effectiveGridChargeNowCost
	if isAlreadyChargingGrid {
		chargeCostNow = gridChargeNowCost
	}

	headroom := capacityKWH - currentEnergyKWH
	neededDurationHours := headroom / chargeKW

	var simPrevChargeCosts []simPriceSlot
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(targetAt) {
			break
		}
		simPrevChargeCosts = append(simPrevChargeCosts, simPriceSlot{
			cost:  slot.GridChargeDollarsPerKWH,
			ts:    slot.TS,
			price: slot.Price,
		})
	}

	var cheapestTime time.Time
	var cheapestPrice types.Price
	var cheapestCost float64
	var cheapestFutureChargeSlot simPriceSlot
	if len(simPrevChargeCosts) > 0 {
		cheapestTime, cheapestPrice, cheapestCost, cheapestFutureChargeSlot = c.findCheapestPlan(simPrevChargeCosts, neededDurationHours)
	}

	// minChargeDurationHours defines the minimum duration of a charging window.
	minChargeDurationHours := 10.0 / 60.0

	// minStartChargeDurationHours defines the required starting headroom in hours of charging time.
	minStartChargeDurationHours := 5.0 / 60.0

	// startChargeHeadroom represents the minimum physical empty capacity (in kWh) needed to start charging.
	startChargeHeadroom := max(0.5, chargeKW*minStartChargeDurationHours)

	// If we are already charging, lower the headroom so we don't stop charging early
	if isAlreadyChargingGrid {
		startChargeHeadroom = 0.1
		minChargeDurationHours = 0.1
	}

	// canChargeNowReal checks if we physically have enough headroom (considering the startChargeHeadroom buffer
	// to prevent short-cycling) and if grid charging is enabled in user settings and on the ESS.
	canChargeNowReal := currentEnergyKWH+startChargeHeadroom < capacityKWH && settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled

	// Calculate the energy that would be added to the battery in a single step duration.
	stepEnergyKWH := chargeKW * minChargeDurationHours
	simEnergyAfterCharge := currentEnergyKWH + stepEnergyKWH

	// simEnergyLessThanCapacity ensures a charging step won't overfill the battery.
	simEnergyLessThanCapacity := simEnergyAfterCharge < capacityKWH

	// canChargeArbitrage combines the capacity buffer limit check with general battery charging availability.
	canChargeArbitrage := simEnergyLessThanCapacity && canChargeNowReal

	standbySolarFillsBatteryBeforePeak := !standbyHitCapacityAt.IsZero() && !standbyHitCapacityAt.After(targetAt)

	// futureArbitrageProfitable determines if charging during the cheapest future slot will yield an export
	// rate-arbitrage profit that exceeds the minimum required arbitrage price difference.
	futureArbitrageProfitable := len(simPrevChargeCosts) > 0 && effectiveExportValue-cheapestFutureChargeSlot.cost >= minArbitrageDiff

	// requiredChargeEnergy determines the minimum energy we must plan to charge from the grid before the peak hour.
	// We start with the total headroom needed to reach capacity, and subtract any predicted solar surplus
	// (negative clamped net load) that will charge the battery under standby before the peak hour.
	// We leave deficit coverage entirely to evaluateDeficit.
	totalSolarSurplusKWH := 0.0
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(targetAt) {
			break
		}
		if slot.ClampedNetLoadSolarKWH < 0 {
			applyRatio := slot.EnergyApplyRatio
			if applyRatio == 0.0 {
				applyRatio = 1.0
			}
			totalSolarSurplusKWH += -slot.ClampedNetLoadSolarKWH * applyRatio
		}
	}
	requiredChargeEnergy := max(0.0, headroom-totalSolarSurplusKWH)

	futureCheapHours := 0
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(targetAt) {
			break
		}
		if slot.GridChargeDollarsPerKWH <= cheapestFutureChargeSlot.cost+minDeficitDiff {
			futureCheapHours++
		}
	}

	// isSignificantlyCheaperFuture determines if there is a future charging window before the target time
	// that offers a price reduction at least as large as minDeficitDiff compared to charging right now.
	isSignificantlyCheaperFuture := len(simPrevChargeCosts) > 0 && cheapestFutureChargeSlot.cost < chargeCostNow && chargeCostNow-cheapestFutureChargeSlot.cost >= minDeficitDiff

	// canDelay determines if we can safely postpone grid charging to a future cheap window.
	// Delaying is allowed if:
	// 1. Arbitrage is profitable in the future (futureArbitrageProfitable).
	// 2. Solar is not already projected to fill the battery before the peak (solarFillsBattery).
	// 3. A future window is significantly cheaper than the current cost (isSignificantlyCheaperFuture).
	// 4. The number of future cheap hours is sufficient to reach the required SOC/energy before the peak.
	canDelay := futureArbitrageProfitable && !standbySolarFillsBatteryBeforePeak && isSignificantlyCheaperFuture && futureCheapHours > 0 && float64(futureCheapHours)*chargeKW >= requiredChargeEnergy

	log.Ctx(ctx).DebugContext(ctx, "arbitrage evaluation variables",
		slog.Float64("effectiveExportValue", effectiveExportValue),
		slog.Float64("effectiveGridChargeNowCost", effectiveGridChargeNowCost),
		slog.Float64("chargeCostNow", chargeCostNow),
		slog.Bool("canChargeArbitrage", canChargeArbitrage),
		slog.Bool("standbySolarFillsBatteryBeforePeak", standbySolarFillsBatteryBeforePeak),
		slog.Bool("canDelay", canDelay),
		slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
		slog.Bool("futureArbitrageProfitable", futureArbitrageProfitable),
		slog.Bool("isSignificantlyCheaperFuture", isSignificantlyCheaperFuture),
		slog.Int("futureCheapHours", futureCheapHours),
		slog.Any("cheapestFutureChargeSlot", cheapestFutureChargeSlot),
	)

	if canDelay {
		log.Ctx(ctx).DebugContext(ctx, "arbitrage: delaying charge", slog.Time("until", cheapestTime), slog.Float64("cost", cheapestCost))
		return &StrategyEvaluation{
			Plan: &futurePlan{
				ChargeTime:  cheapestTime,
				ChargePrice: cheapestPrice,
				ChargeCost:  cheapestCost,
			},
			BenefitDollars: requiredChargeEnergy * (effectiveExportValue - cheapestCost),
		}
	}

	// Find the end of the peak window starting at targetAt.
	// The peak window is the contiguous block of hours starting from targetAt where the export rate
	// is high enough to be profitable (i.e. grid charge cost/price is >= targetValue - minArbitrageDiff).
	peakEnd := targetAt.Add(time.Hour)
	inPeak := false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeak = true
		}
		if inPeak {
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff {
				peakEnd = slot.TS.Add(time.Hour)
			} else {
				break
			}
		}
	}

	// solarFillsDuringPeak checks if solar is predicted to fill the battery during the peak export window itself
	// (before peakEnd). Even if the battery is not full at the start of the peak, if solar fills it during
	// the peak, we can still capture and export that energy.
	solarFillsDuringPeak := !standbyHitCapacityAt.IsZero() && standbyHitCapacityAt.After(targetAt) && !standbyHitCapacityAt.After(peakEnd)

	// We check if the battery has any usable energy above the reserve limit.
	// If the battery is empty (<= minKWH), we cannot grid-charge, and solar won't fill it
	// before or during the peak, then we have no energy to export, so we bail out.
	if !standbySolarFillsBatteryBeforePeak && !solarFillsDuringPeak && !canChargeArbitrage && currentEnergyKWH <= minKWH {
		// We can't export because the battery won't be full and we can't charge it!
		return nil
	}

	// canChargeNow determines if we should grid-charge the battery right now.
	// We charge now if:
	// 1. Exporting at the target peak is profitable enough after subtracting the current charge cost (including arbitrage difference).
	// 2. We have physical headroom in the battery and grid-charging is allowed (canChargeArbitrage).
	// 3. Solar is not already projected to fill the battery before the peak (otherwise we would waste solar energy).
	canChargeNow := effectiveExportValue-chargeCostNow >= minArbitrageDiff && canChargeArbitrage && !standbySolarFillsBatteryBeforePeak

	// canStandbyNow determines if we should hold the battery in standby (preventing it from discharging to cover home load).
	// We standby now if:
	// 1. The future export value is higher than or equal to the current grid charge cost (meaning holding is economically viable).
	// 2. Under the standby simulation model (where we hold during cheap hours), we do not drop below the deficit threshold
	//    before the target export hour (standbyHitAboveDeficitAt.IsZero() || standbyHitAboveDeficitAt.After(targetAt)).
	//    We do NOT check summary.HitBelowDeficitAt here because that deficit prediction assumes the battery is actively
	//    discharging to cover home load, which is false if we decide to stay in standby.
	canStandbyNow := effectiveExportValue >= chargeCostNow && (standbyHitAboveDeficitAt.IsZero() || standbyHitAboveDeficitAt.After(targetAt))

	// If the target is NOW, we don't hold! We want to discharge if it's profitable!
	if !targetAt.After(now) {
		canStandbyNow = false
	}

	if canChargeNow {
		chargeDescription := fmt.Sprintf(
			"Arbitrage Opportunity (Export) at %s. Buy@%.3f -> Sell/Save@%.3f.",
			targetAt.Format(time.Kitchen),
			chargeCostNow,
			targetValue,
		)
		reason := types.ActionReasonArbitrageChargeExport
		chargeBenefit := requiredChargeEnergy * (effectiveExportValue - chargeCostNow)
		log.Ctx(ctx).DebugContext(ctx, "evaluateExportArbitrage returning charge strategy",
			slog.Float64("chargeBenefit", chargeBenefit),
			slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
			slog.String("reason", string(reason)),
			slog.Float64("effectiveExportValue", effectiveExportValue),
			slog.Float64("chargeCostNow", chargeCostNow),
			slog.Bool("solarFillsDuringPeak", solarFillsDuringPeak),
			slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
			slog.Time("standbyHitAboveDeficitAt", standbyHitAboveDeficitAt),
		)
		return &StrategyEvaluation{
			Decision: &DecisionResult{
				BatteryMode: types.BatteryModeChargeAny,
				Reason:      reason,
				Description: fmt.Sprintf("Charging Optimized: %s", chargeDescription),
				FuturePrice: &targetPrice,
			},
			BenefitDollars: chargeBenefit,
		}
	}

	if canStandbyNow {
		var holdState string
		if !settings.GridChargeBatteries {
			holdState = "Grid charging disabled"
		} else if currentStatus.BatteryChargingDisabled {
			holdState = "Charging disabled"
		} else if !canChargeNowReal {
			holdState = "Battery full"
		} else if standbySolarFillsBatteryBeforePeak {
			holdState = "Battery will fill from solar"
		} else {
			holdState = "Unable to charge"
		}

		holdDescription := fmt.Sprintf(
			"Arbitrage Opportunity (Export) at %s. %s. Hold energy.",
			targetAt.Format(time.Kitchen),
			holdState,
		)
		reason := types.ActionReasonArbitrageHoldExport

		heldEnergy := currentEnergyKWH - minKWH
		if heldEnergy < 0 {
			heldEnergy = 0
		}
		benefit := heldEnergy * (effectiveExportValue - chargeCostNow)

		log.Ctx(ctx).DebugContext(ctx, "Arbitrage standby evaluated", slog.Float64("standbyBenefit", benefit), slog.String("reason", string(reason)))

		if benefit > 0 {
			log.Ctx(ctx).DebugContext(ctx, "evaluateExportArbitrage returning standby strategy",
				slog.Float64("standbyBenefit", benefit),
				slog.Float64("heldEnergy", heldEnergy),
				slog.String("reason", string(reason)),
				slog.Float64("effectiveExportValue", effectiveExportValue),
				slog.Float64("chargeCostNow", chargeCostNow),
				slog.Bool("solarFillsDuringPeak", solarFillsDuringPeak),
				slog.Time("standbyHitCapacityAt", standbyHitCapacityAt),
				slog.Time("standbyHitAboveDeficitAt", standbyHitAboveDeficitAt),
			)
			return &StrategyEvaluation{
				Decision: &DecisionResult{
					BatteryMode: types.BatteryModeStandby,
					Reason:      reason,
					Description: holdDescription,
					FuturePrice: &targetPrice,
				},
				BenefitDollars: benefit,
			}
		}
	}

	return nil
}

func (c *Controller) evaluatePlannedCharge(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
	plan PlannedCharge,
) *DecisionResult {
	hitAboveDeficitAt := summary.HitAboveDeficitAt
	hitBelowDeficitAt := summary.HitBelowDeficitAt

	minDiff := max(0.001, settings.MinDeficitPriceDifferenceDollarsPerKWH)
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	// Cheap Price Retention check:
	// If the current price is cheap compared to the planned future price (the difference is smaller than minDiff),
	// we shouldn't discharge. We standby to wait for the planned charge window.
	isCheapNow := plan.Cost+minDiff > gridChargeNowCost

	// Future Peak Preservation scanning:
	// We scan the simulated hours between now and the planned charge time to identify if there are any peak hours
	// that are more expensive than our current price.
	// If we would hit a deficit (HitAboveDeficitAt) before or during that future peak, discharging the battery now
	// to cover home load would deplete it prematurely, forcing us to buy from the grid at those higher peak rates.
	// Thus, we force Standby now to preserve the battery's energy specifically to offset the highest upcoming peak.
	var mustStandbyForPeak bool
	var peakTime time.Time
	var peakCost float64
	var peakPrice types.Price

	for _, slot := range simData {
		if !slot.TS.Before(plan.Time) {
			break
		}

		isMoreExpensiveThanNow := slot.GridChargeDollarsPerKWH > gridChargeNowCost
		deficitBeforeOrDuring := !hitAboveDeficitAt.IsZero() && !hitAboveDeficitAt.After(slot.TS.Add(time.Hour))

		if isMoreExpensiveThanNow && deficitBeforeOrDuring {
			mustStandbyForPeak = true
			if slot.GridChargeDollarsPerKWH > peakCost {
				peakCost = slot.GridChargeDollarsPerKWH
				peakTime = slot.TS
				peakPrice = slot.Price
			}
		}
	}

	if isCheapNow || mustStandbyForPeak {
		var reason types.ActionReason
		var standbyDescription string
		var futurePrice *types.Price

		if mustStandbyForPeak {
			reason = types.ActionReasonDeficitSaveForPeak
			standbyDescription = fmt.Sprintf(
				"If discharged, battery would deplete at %s. Preserving battery energy for higher peak prices at %s ($%.3f < $%.3f).",
				hitAboveDeficitAt.Format(time.Kitchen),
				peakTime.Format(time.Kitchen),
				gridChargeNowCost,
				peakCost,
			)
			futurePrice = &peakPrice
		} else {
			reason = types.ActionReasonWaitingToCharge
			if plan.Cost < gridChargeNowCost {
				standbyDescription = fmt.Sprintf("Waiting to charge at %s ($%.3f < $%.3f).", plan.Time.Format(time.Kitchen), plan.Cost, gridChargeNowCost)
			} else {
				standbyDescription = fmt.Sprintf("Waiting to charge at %s ($%.3f).", plan.Time.Format(time.Kitchen), plan.Cost)
			}
			futurePrice = &plan.Price
		}

		return &DecisionResult{
			BatteryMode: types.BatteryModeStandby,
			Reason:      reason,
			Description: standbyDescription,
			FuturePrice: futurePrice,
		}
	}

	// Otherwise, we can safely discharge the battery now to cover the home load.
	// We have already verified that our current price is either:
	// a. Cheap compared to the planned price (isCheapNow)
	// b. There's no future price that we need to save energy for (mustStandbyForPeak == false)
	if hitBelowDeficitAt.IsZero() || !hitBelowDeficitAt.Before(plan.Time) {
		// c. We have enough battery to last until the planned charge time
		loadDescription := fmt.Sprintf("Sufficient battery to reach planned charge time at %s.", plan.Time.Format(time.Kitchen))
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonSufficientBatteryTillCharge,
			Description: loadDescription,
			FuturePrice: &plan.Price,
		}
	}

	// Minimum Reserve Enforcement:
	// If the battery is already at or near its minimum reserve limit (either because BatteryAboveMinSOC is false
	// or the deficit is predicted within 5 minutes), the battery system cannot discharge any further to cover load.
	// We return BatteryModeLoad with ActionReasonBatteryAtReserve because the physical battery hardware itself
	// will protect the reserve. Declaring standby is not meaningful here, and setting Load allows any solar
	// generation to be consumed by the home instead of being curtailed.
	isBatteryAtReserve := !currentStatus.BatteryAboveMinSOC && !currentStatus.ElevatedMinBatterySOC
	if !isBatteryAtReserve && !summary.HitDeficitAt.IsZero() {
		if summary.HitDeficitAt.Before(now.Add(5 * time.Minute)) {
			isBatteryAtReserve = true
		}
	}
	if isBatteryAtReserve {
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonBatteryAtReserve,
			Description: "Battery is at reserve. Using remaining energy because standby is not meaningful (battery is already held at reserve).",
		}
	}

	loadDescription := fmt.Sprintf(
		"If discharged, battery would deplete at %s. Prices are higher now than at planned charge time ($%.3f > $%.3f).",
		hitAboveDeficitAt.Format(time.Kitchen),
		gridChargeNowCost,
		plan.Cost,
	)
	return &DecisionResult{
		BatteryMode: types.BatteryModeLoad,
		Reason:      types.ActionReasonDischargeAtPeak,
		Description: loadDescription,
	}
}

// evaluateFallback implements the economical fallback battery actions.
//
// Logic and Reasoning:
//
//  1. Minimum Reserve Check:
//     If the battery is at or near its minimum reserve limit (either because the SOC is below minimum,
//     or a deficit is predicted within 5 minutes), the battery cannot physically discharge further.
//     We return Load mode with ActionReasonBatteryAtReserve because the battery system will hold the reserve
//     anyway, and declaring standby is not meaningful (we want to allow solar consumption if possible).
//
//  2. Deficit & Standby / Saving for Peak:
//     Normally, if there is a deficit, evaluateDeficit would have planned a grid charge (which is handled
//     earlier by evaluatePlannedCharge, preventing us from reaching here).
//     Therefore, reaching here with a deficit typically indicates that grid charging is disabled
//     (e.g., settings.GridChargeBatteries is false or charging is disabled), or we have flat prices
//     where no cheaper future window exists.
//
//     - If grid charging is disabled, we cannot grid charge. However, if the current price is cheap
//     compared to a future peak price (gridChargeNowCost < summary.MaxFutureGridChargeCost), we still want to
//     standby now during the cheap hours to conserve battery energy (from solar/existing charge) for the peak.
//     Otherwise, if we discharged now, we would exhaust the battery during cheap hours and pay peak rates later.
//     - If we will hit capacity (e.g. from solar) before that future peak, we do NOT standby; we discharge (Load)
//     to make room and prevent solar curtailment.
//     - If we hit capacity before the deficit, we similarly discharge (Load) to prevent curtailment.
//     - If the current price is already peak, or there is no peak (flat prices), we discharge (Load).
//     Standby forever on flat prices would exhaust/not utilize the battery.
//
//  3. Fallback:
//     In all other cases (e.g. no deficit, flat prices, or we have sufficient battery), we fall back
//     to discharging the battery to cover home load (Load mode) using ActionReasonSufficientBattery.
func (c *Controller) evaluateFallback(
	ctx context.Context,
	now time.Time,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	settings types.Settings,
	simData []SimHour,
	summary simulationSummary,
) *DecisionResult {
	// For peak survival checks, we use HitAboveDeficitAt (no 3% buffer).
	// For evaluateDeficit/retaining general deficit evaluation, we use HitBelowDeficitAt (with 3% buffer).
	hitAboveDeficitAt := summary.HitAboveDeficitAt
	hitBelowDeficitAt := summary.HitBelowDeficitAt

	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	// 1. Minimum Reserve Enforcement:
	// If the battery is already at or near its minimum reserve limit (either because BatteryAboveMinSOC is false
	// or the deficit is predicted within 5 minutes), the battery system cannot discharge any further to cover load.
	// We return BatteryModeLoad with ActionReasonBatteryAtReserve because the physical battery hardware itself
	// will protect the reserve. Declaring standby is not meaningful here, and setting Load allows any solar
	// generation to be consumed by the home instead of being curtailed.
	isBatteryAtReserve := !currentStatus.BatteryAboveMinSOC && !currentStatus.ElevatedMinBatterySOC
	if !isBatteryAtReserve && !summary.HitDeficitAt.IsZero() {
		if summary.HitDeficitAt.Before(now.Add(5 * time.Minute)) {
			isBatteryAtReserve = true
		}
	}
	if isBatteryAtReserve {
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonBatteryAtReserve,
			Description: "Battery is at reserve. Using remaining energy because standby is not meaningful (battery is already held at reserve).",
		}
	}

	// 2. Projected Deficit Fallback Handling:
	// If a future deficit is predicted and we have no active planned charge (e.g. grid charging is disabled):
	if !hitBelowDeficitAt.IsZero() {
		// Discharge before capacity (Curtailment Prevention):
		// If the simulation indicates the battery will hit capacity (e.g., from solar) before the deficit hour,
		// we should discharge now to create headroom. This prevents solar generation from being curtailed
		// when the battery gets full.
		if !summary.HitCapacityAt.IsZero() && summary.HitCapacityAt.After(now) && summary.HitCapacityAt.Before(hitAboveDeficitAt) {
			reason := types.ActionReasonDischargeBeforeCapacityNow
			loadReason := fmt.Sprintf("Capacity hit at %s before deficit at %s.", summary.HitCapacityAt.Format(time.Kitchen), hitAboveDeficitAt.Format(time.Kitchen))

			// HitSolarCapacityAt is only set if we have solar exporting disabled
			if !summary.HitSolarCapacityAt.IsZero() && summary.HitSolarCapacityAt.Before(hitAboveDeficitAt) && summary.HitCapacityAt.Equal(summary.HitSolarCapacityAt) {
				reason = types.ActionReasonPreventSolarCurtailment
				loadReason = fmt.Sprintf("Solar curtailment likely at %s before deficit at %s.", summary.HitSolarCapacityAt.Format(time.Kitchen), hitAboveDeficitAt.Format(time.Kitchen))
			}

			log.Ctx(ctx).DebugContext(
				ctx,
				"deficit predicted but will refill to capacity before then",
				slog.Time("hitCapacityAt", summary.HitCapacityAt),
				slog.Time("hitSolarCapacityAt", summary.HitSolarCapacityAt),
				slog.Time("hitDeficitAt", hitAboveDeficitAt),
				slog.String("reason", string(reason)),
			)
			return &DecisionResult{
				BatteryMode: types.BatteryModeLoad,
				Reason:      reason,
				Description: loadReason,
			}
		}

		// Standby to Save for Peak:
		// We reach this block when a future deficit is predicted but evaluateDeficit did not plan a charge
		// (which happens when grid charging is disabled, e.g. settings.GridChargeBatteries is false or
		// BatteryChargingDisabled is true, meaning we cannot refill the battery from the grid).
		// In this case, we must conserve whatever energy we currently have. If the current price is cheap,
		// but there is an upcoming peak hour that is more expensive before we hit a deficit, we standby now.
		// Preserving the battery's energy for that future peak hour is far more valuable than discharging it now.
		var mustStandbyForPeak bool
		var peakTime time.Time
		var peakCost float64
		var peakPrice types.Price

		for _, slot := range simData {
			// Stop looking once we hit capacity, because refilling to capacity resets/wipes out
			// any need to save energy from before that point.
			if !summary.HitCapacityAt.IsZero() && summary.HitCapacityAt.After(now) && !slot.TS.Before(summary.HitCapacityAt) {
				break
			}
			isMoreExpensiveThanNow := slot.GridChargeDollarsPerKWH > gridChargeNowCost
			deficitBeforeOrDuring := !hitAboveDeficitAt.IsZero() && !hitAboveDeficitAt.After(slot.TS.Add(time.Hour))
			if isMoreExpensiveThanNow && deficitBeforeOrDuring {
				mustStandbyForPeak = true
				if slot.GridChargeDollarsPerKWH > peakCost {
					peakCost = slot.GridChargeDollarsPerKWH
					peakTime = slot.TS
					peakPrice = slot.Price
				}
			}
		}

		if mustStandbyForPeak {
			standbyReason := fmt.Sprintf(
				"If discharged, battery would deplete at %s. "+
					"Since current price ($%.3f) is cheap and will remain cheap, "+
					"preserving battery energy for higher prices at %s ($%.3f < $%.3f).",
				hitAboveDeficitAt.Format(time.Kitchen),
				gridChargeNowCost,
				peakTime.Format(time.Kitchen),
				gridChargeNowCost,
				peakCost,
			)
			log.Ctx(ctx).DebugContext(
				ctx,
				"deficit predicted, saving for peak",
				slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
				slog.Float64("peakCost", peakCost),
				slog.Time("peakTime", peakTime),
				slog.Time("hitDeficitAt", hitAboveDeficitAt),
				slog.Float64("gridChargeNowCost", gridChargeNowCost),
			)
			return &DecisionResult{
				BatteryMode: types.BatteryModeStandby,
				Reason:      types.ActionReasonDeficitSaveForPeak,
				Description: standbyReason,
				FuturePrice: &peakPrice,
			}
		}

		// If current price is already peak, or there are flat prices (no more expensive peak hour coming),
		// we discharge now because holding energy indefinitely under flat/peak rates yields no economic benefit.
		log.Ctx(ctx).DebugContext(
			ctx,
			"deficit predicted but at peak price or flat prices",
			slog.Float64("currentPrice", currentPrice.DollarsPerKWH),
			slog.Time("hitDeficitAt", hitAboveDeficitAt),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
		)
		return &DecisionResult{
			BatteryMode: types.BatteryModeLoad,
			Reason:      types.ActionReasonDischargeAtPeak,
			Description: "Deficit predicted but current price is Peak.",
		}
	}

	// 3. Fallback when there is no deficit: discharge to cover load.
	log.Ctx(ctx).DebugContext(
		ctx,
		"no deficit predicted, using battery",
		slog.Float64("minEnergy", summary.MinEnergy),
		slog.Float64("maxEnergy", summary.MaxEnergy),
	)
	return &DecisionResult{
		BatteryMode: types.BatteryModeLoad,
		Reason:      types.ActionReasonSufficientBattery,
		Description: "Sufficient battery.",
	}
}

// findCheapestPlan finds the planned charge details and the marginal cheapest slot from candidate slots.
func (c *Controller) findCheapestPlan(
	simPrevChargeCosts []simPriceSlot,
	neededDurationHours float64,
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

	// Add a 50-minute buffer to duration rounding and floor it.
	// Note: Because we use 0-based indexing for the marginal slot (marginalSlot = simPrevChargeCosts[idx-1]),
	// idx = 1 corresponds to index 0 (the cheapest slot), idx = 2 corresponds to index 1 (2nd cheapest), etc.
	//
	// Examples:
	// - If neededDurationHours is 0.25 (15 mins): 0.25 + 50/60 = 1.08, flooring to idx = 1 (index 0). This is correct
	//   as we need at least 1 slot to charge.
	// - If neededDurationHours is 1.10 (1h 6m): 1.10 + 50/60 = 1.93, flooring to idx = 1 (index 0). We round down
	//   to 1 slot because the 6-minute deficit in the second hour is too small to justify planning a full extra hour of charge.
	// - If neededDurationHours is 1.20 (1h 12m): 1.20 + 50/60 = 2.03, flooring to idx = 2 (index 1). Since the second hour's
	//   deficit is >= 10 minutes (meaning we run out of battery before 50 minutes into the hour), we round up to 2 slots and
	//   evaluate against the 2nd cheapest slot (index 1) as the marginal slot.
	idx := int(math.Floor(neededDurationHours + (50.0 / 60.0)))
	if idx < 1 {
		idx = 1
	}
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
// It returns the hitCapacityAt, hitAboveDeficitAt, and hitBelowDeficitAt times under this model.
func (c *Controller) simulateStandby(
	simData []SimHour,
	dischargeOverCost float64,
	currentEnergyKWH float64,
	capacityKWH float64,
	minKWH float64,
) (hitCapacityAt, hitAboveDeficitAt, hitBelowDeficitAt time.Time) {
	batteryEnergy := currentEnergyKWH
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
		shouldDischarge := slot.GridChargeDollarsPerKWH >= dischargeOverCost

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
			hitAboveDeficitAt = time.Time{}
			hitBelowDeficitAt = time.Time{}
		}

		if appliedNetKWH > 0 {
			// Draining/Discharging
			// 1. Calculate hitAboveDeficitAt (with 1% safety buffer, i.e., drops below aboveDeficitThresholdKWH).
			aboveDeficitThresholdKWH := minKWH + (capacityKWH * 0.01)
			if newEnergy < aboveDeficitThresholdKWH {
				if hitAboveDeficitAt.IsZero() {
					remainingBeforeAbove := batteryEnergy - aboveDeficitThresholdKWH
					if remainingBeforeAbove > 0 {
						fraction := remainingBeforeAbove / appliedNetKWH
						hitAboveDeficitAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
					} else {
						hitAboveDeficitAt = slot.TS
					}
				}
			}

			// 2. Check if we will drop below the minimum reserve limit (with 3% hysteresis buffer).
			// If so, interpolate the exact fraction of the hour when we hit the reserve.
			deficitThresholdKWH := max(minKWH-(capacityKWH*deficitThresholdOffsetCapacityRatio), 0.0)
			if newEnergy < deficitThresholdKWH {
				if hitBelowDeficitAt.IsZero() {
					remainingBeforeDeficit := batteryEnergy - deficitThresholdKWH
					if remainingBeforeDeficit > 0 {
						fraction := remainingBeforeDeficit / appliedNetKWH
						// Round to nearest nanosecond to avoid off-by-one float-casting issues.
						hitBelowDeficitAt = slot.TS.Add(time.Duration(math.Round(fraction * float64(time.Hour))))
					} else {
						hitBelowDeficitAt = slot.TS
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
		if !hitCapacityAt.IsZero() && !hitBelowDeficitAt.IsZero() {
			break
		}
	}

	return hitCapacityAt, hitAboveDeficitAt, hitBelowDeficitAt
}

func drModeString(mode types.BatteryMode) string {
	switch mode {
	case types.BatteryModeNoChange:
		return "NoChange"
	case types.BatteryModeStandby:
		return "Standby"
	case types.BatteryModeChargeAny:
		return "ChargeAny"
	case types.BatteryModeChargeSolar:
		return "ChargeSolar"
	case types.BatteryModeLoad:
		return "Load"
	default:
		return fmt.Sprintf("Unknown(%d)", mode)
	}
}
