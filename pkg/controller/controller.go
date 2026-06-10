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
	Time        time.Time
	Price       types.Price
	Cost        float64
	Description string
}

// futurePlan represents a planned future charge window.
type futurePlan struct {
	ChargeTime  time.Time
	ChargePrice types.Price
	ChargeCost  float64
	Description string
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
	HitFutureCapacityAt        time.Time
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
	evals := []*StrategyEvaluation{evalDeficit, evalExport}
	for _, e := range evals {
		if e != nil && e.Decision != nil {
			if bestImmediate == nil || e.BenefitDollars > bestImmediate.BenefitDollars {
				bestImmediate = e
			}
		}
	}

	var bestPlan *StrategyEvaluation
	var planChoiceReason string
	var chosenPlanType string

	var deficitPlanDesc string
	if evalDeficit != nil && evalDeficit.Plan != nil {
		deficitPlanDesc = evalDeficit.Plan.Description
	}
	var exportPlanDesc string
	if evalExport != nil && evalExport.Plan != nil {
		exportPlanDesc = evalExport.Plan.Description
	}

	if evalDeficit != nil && evalDeficit.Plan != nil && (evalExport == nil || evalExport.Plan == nil) {
		bestPlan = evalDeficit
		chosenPlanType = "deficit"
		planChoiceReason = "only deficit plan is available"
	} else if evalExport != nil && evalExport.Plan != nil && (evalDeficit == nil || evalDeficit.Plan == nil) {
		bestPlan = evalExport
		chosenPlanType = "export_arbitrage"
		planChoiceReason = "only export arbitrage plan is available"
	} else if evalDeficit != nil && evalDeficit.Plan != nil && evalExport != nil && evalExport.Plan != nil {
		// Both plans exist, compare them
		deficitTime := evalDeficit.Plan.ChargeTime
		exportTime := evalExport.Plan.ChargeTime
		if exportTime.Before(deficitTime) {
			bestPlan = evalExport
			chosenPlanType = "export_arbitrage"
			planChoiceReason = fmt.Sprintf("export arbitrage plan is earlier than deficit plan (%s vs %s)",
				exportTime.Format("15:04"), deficitTime.Format("15:04"))
		} else if deficitTime.Before(exportTime) {
			bestPlan = evalDeficit
			chosenPlanType = "deficit"
			planChoiceReason = fmt.Sprintf("deficit plan is earlier than export arbitrage plan (%s vs %s)",
				deficitTime.Format("15:04"), exportTime.Format("15:04"))
		} else {
			// Same charge time, compare benefit
			if evalExport.BenefitDollars > evalDeficit.BenefitDollars {
				bestPlan = evalExport
				chosenPlanType = "export_arbitrage"
				planChoiceReason = fmt.Sprintf("same charge time (%s), but export arbitrage has higher benefit ($%.4f vs $%.4f)",
					exportTime.Format("15:04"), evalExport.BenefitDollars, evalDeficit.BenefitDollars)
			} else {
				bestPlan = evalDeficit
				chosenPlanType = "deficit"
				planChoiceReason = fmt.Sprintf("same charge time (%s), but deficit has higher or equal benefit ($%.4f vs $%.4f)",
					deficitTime.Format("15:04"), evalDeficit.BenefitDollars, evalExport.BenefitDollars)
			}
		}
	}

	var activePlan *PlannedCharge
	if bestPlan != nil {
		activePlan = &PlannedCharge{
			Time:        bestPlan.Plan.ChargeTime,
			Price:       bestPlan.Plan.ChargePrice,
			Cost:        bestPlan.Plan.ChargeCost,
			Description: bestPlan.Plan.Description,
		}
		log.Ctx(ctx).DebugContext(ctx, "evaluated planned charges",
			slog.Bool("hasActivePlan", true),
			slog.String("chosenPlanType", chosenPlanType),
			slog.String("choiceReason", planChoiceReason),
			slog.Time("activePlanTime", activePlan.Time),
			slog.Float64("activePlanCost", activePlan.Cost),
			slog.Float64("benefitDollars", bestPlan.BenefitDollars),
			slog.String("description", activePlan.Description),
			slog.String("deficitPlan", deficitPlanDesc),
			slog.String("exportPlan", exportPlanDesc),
		)
	} else {
		log.Ctx(ctx).DebugContext(ctx, "evaluated planned charges",
			slog.Bool("hasActivePlan", false),
			slog.String("deficitPlan", deficitPlanDesc),
			slog.String("exportPlan", exportPlanDesc),
		)
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

	if activePlan != nil {
		planDecision := c.evaluatePlannedCharge(ctx, now, currentStatus, currentPrice, settings, simData, summary, *activePlan)
		log.Ctx(ctx).DebugContext(ctx, "executing active planned charge",
			slog.Time("planTime", activePlan.Time),
			slog.Float64("planCost", activePlan.Cost),
			slog.String("mode", drModeString(planDecision.BatteryMode)),
			slog.String("reason", string(planDecision.Reason)),
			slog.Float64("gridChargeNowCost", gridChargeNowCost),
			slog.Float64("batterySOC", currentStatus.BatterySOC),
			slog.String("description", activePlan.Description),
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

	// 1. Scan for capacity hits (both overall and strictly in the future).
	// We split capacity hits into HitCapacityAt (the absolute first capacity hit, including at 'now')
	// and HitFutureCapacityAt / HitSolarCapacityAt (strictly in the future, After(now)):
	//
	// - HitCapacityAt: Used when we need to bound our lookahead or planned charges by the first time the
	//   battery is full (whether that is right now or in the future).
	// - HitFutureCapacityAt / HitSolarCapacityAt: Used when deciding whether to standby 'now' to conserve
	//   energy for a future peak vs discharging 'now' to cover home load. If the battery starts full at 'now',
	//   an unsplit check would immediately flag a capacity hit at 'now' and assume the battery will refill.
	//   However, if there are no *future* capacity hits, the battery will never refill again after we discharge
	//   it, meaning we would deplete it prematurely and fail to save energy for the peak. Filtering to
	//   After(now) ensures we only assume the battery will refill if there is a *future* capacity hit.
	//
	// Why we do not track future solar curtailment separately:
	// Solar curtailment is an immediate operational action: if we are currently curtailing (e.g. at 99% SOC),
	// we must act immediately to prevent it. A future solar curtailment event is simply a future capacity
	// hit where the battery gets refilled. When the simulation reaches that future hour, it will naturally
	// handle the curtailment then if the battery is still full. Trying to act now (e.g., discharging the
	// battery early) to prevent future solar curtailment risks prematurely emptying the battery during cheap
	// or peak hours when we need the charge, especially if the forecast or weather changes in the meantime.
	// Thus, future solar curtailment is treated simply as a capacity refill event, not as a reason to discharge now.
	for _, slot := range simData {
		if !slot.HitCapacityAt.IsZero() && summary.HitCapacityAt.IsZero() {
			summary.HitCapacityAt = slot.HitCapacityAt
		}
		if !slot.HitCapacityAt.IsZero() && slot.HitCapacityAt.After(now) && summary.HitFutureCapacityAt.IsZero() {
			summary.HitFutureCapacityAt = slot.HitCapacityAt
		}
		if !slot.HitStandbyCapacityAt.IsZero() && slot.HitStandbyCapacityAt.After(now) && summary.HitStandbyCapacityAt.IsZero() {
			summary.HitStandbyCapacityAt = slot.HitStandbyCapacityAt
		}
		if !slot.HitSolarCapacityAt.IsZero() && slot.HitSolarCapacityAt.After(now) && summary.HitSolarCapacityAt.IsZero() {
			summary.HitSolarCapacityAt = slot.HitSolarCapacityAt
		}
	}
	if !summary.HitSolarCapacityAt.IsZero() && (summary.HitCapacityAt.IsZero() || summary.HitSolarCapacityAt.Before(summary.HitCapacityAt)) {
		summary.HitCapacityAt = summary.HitSolarCapacityAt
	}
	if !summary.HitSolarCapacityAt.IsZero() && (summary.HitFutureCapacityAt.IsZero() || summary.HitSolarCapacityAt.Before(summary.HitFutureCapacityAt)) {
		summary.HitFutureCapacityAt = summary.HitSolarCapacityAt
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
	// to charge the battery. To prevent phantom draw from trickling us into thinking we're charging,
	// and to ensure solar-only charging doesn't confuse us, we require the battery charging rate
	// to be more than 1 kW (BatteryKW < -1.0) and grid import to be positive (GridKW > 0).
	isAlreadyChargingGrid := currentStatus.BatteryKW < -1.0 && currentStatus.GridKW > 0

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

	// Charge Hysteresis & Anti-Oscillation:
	// We calculate the minimum required physical battery capacity headroom (in kWh) before initiating a charge.
	// 1. To start charging: We require at least 5 minutes of charging capacity at max charge rate (startChargeHeadroom),
	//    clamped at a minimum of 0.3 kWh. This ensures we do not trigger tiny grid-charging sessions
	//    when the battery is already extremely close to 100% (which causes rapid relays toggling and reduces battery life).
	// 2. Hysteresis (to continue charging): If we are already grid-charging, we lower the required headroom to 0.1 kWh.
	//    This permits the battery to continue charging until it is almost completely full, preventing short-cycling
	//    right at the end of the charge.
	minStartChargeDurationHours := float64(settings.MinStartChargeMinutes) / 60.0
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

	headroomNow := capacityKWH - currentEnergyKWH
	if headroomNow < 0 {
		headroomNow = 0
	}
	deficitChargeDurationHours := 0.0
	neededDeficitEnergy := totalDeficitKWH
	if chargeKW > 0 {
		if neededDeficitEnergy > headroomNow {
			neededDeficitEnergy = headroomNow
		}
		deficitChargeDurationHours = neededDeficitEnergy / chargeKW
	}
	// isTinyDeficitCharge prevents starting a new grid charge session if the duration
	// is less than 10 minutes. If we are already charging from the grid, we bypass
	// this check to allow the session to finish using the headroom hysteresis.
	isTinyDeficitCharge := !isAlreadyChargingGrid && (deficitChargeDurationHours < 10.0/60.0)

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

		if hasDeficit && canChargeFuture {
			simInFuture := i > 0
			var simPrevChargeCostsFuture []simPriceSlot
			var simPrevChargeCostsAll []simPriceSlot
			if simInFuture {
				for j := 0; j <= i; j++ {
					candidateTS := simData[j].TS
					// Ensure we don't plan to charge after we've already hit the first deficit,
					// because we cannot delay charging past the deficit hour.
					// However, if the first deficit is happening right now, we do not skip future hours.
					isFutureDeficit := !hitDeficitAt.IsZero() && hitDeficitAt.After(now.Add(time.Hour))
					if isFutureDeficit && candidateTS.After(hitDeficitAt) {
						continue
					}
					// Ensure we're on the same side of capacity
					if !summary.HitCapacityAt.IsZero() && slot.TS.After(summary.HitCapacityAt) && !candidateTS.After(summary.HitCapacityAt) {
						continue
					}
					var cost float64
					if j == 0 {
						cost = effectiveGridChargeNowCost
					} else {
						cost = simData[j].GridChargeDollarsPerKWH
					}
					slotEntry := simPriceSlot{
						cost:  cost,
						ts:    simData[j].TS,
						price: simData[j].Price,
					}
					simPrevChargeCostsAll = append(simPrevChargeCostsAll, slotEntry)
					if j > 0 {
						simPrevChargeCostsFuture = append(simPrevChargeCostsFuture, slotEntry)
					}
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

			// We disable immediate charging to cover a future slot's deficit if the battery is projected
			// to hit capacity before that slot (since it will refill anyway).
			// We use HitFutureCapacityAt (strictly after 'now') instead of HitCapacityAt because if the battery
			// starts full at 'now', HitCapacityAt would be 'now', which would incorrectly disable immediate charging
			// for all future slots. Even if full now, the battery will discharge, and we must be allowed to charge
			// it later if a deficit arises after it discharges.
			canChargeNowForSlot := canChargeNow
			if !summary.HitFutureCapacityAt.IsZero() && slot.TS.After(summary.HitFutureCapacityAt) {
				canChargeNowForSlot = false
			}

			if simInFuture && len(simPrevChargeCostsAll) > 0 {
				cheapestTime, cheapestPrice, cheapestCost, cheapestCandidateSlot := c.findCheapestPlan(simPrevChargeCostsAll, chargeDurationHours)

				var cheapestFutureChargeSlot simPriceSlot
				hasFutureSlot := len(simPrevChargeCostsFuture) > 0
				if hasFutureSlot {
					_, _, _, cheapestFutureChargeSlot = c.findCheapestPlan(simPrevChargeCostsFuture, chargeDurationHours)
				}

				isSignificantlyCheaperNow := false
				if simInFuture {
					if !hasFutureSlot {
						isSignificantlyCheaperNow = true
					} else {
						if effectiveGridChargeNowCost > gridChargeNowCost {
							// If the 10-minute lookahead penalty is active, require a strictly greater price difference
							// to justify starting a charge otherwise let's just wait until the next hour to start charging
							isSignificantlyCheaperNow = cheapestFutureChargeSlot.cost-effectiveGridChargeNowCost > minDeficitDiff
						} else {
							isSignificantlyCheaperNow = cheapestFutureChargeSlot.cost-effectiveGridChargeNowCost >= minDeficitDiff
						}
					}
				}

				// Price comparisons for charging decisions:
				// - isSignificantlyCheaperFuture: True if waiting until a future hour to charge saves us at least minDeficitDiff.
				// - isSignificantlyCheaperThanDeficit: True if the future cheapest slot is significantly cheaper than the deficit peak price itself.
				// - isSignificantlyCheaperThanDeficitNow: True if right now is significantly cheaper than the deficit peak price itself. We use this
				//   for hysteresis checking during active charge sessions so lookahead penalty doesn't prematurely kill them.
				// - isCheapestWindow: True if right now is tied for the absolute cheapest window before the deficit.
				isSignificantlyCheaperFuture := simInFuture && hasFutureSlot && effectiveGridChargeNowCost-cheapestFutureChargeSlot.cost >= minDeficitDiff
				isSignificantlyCheaperThanDeficit := simInFuture && hasFutureSlot && slot.GridChargeDollarsPerKWH-cheapestFutureChargeSlot.cost >= minDeficitDiff
				isSignificantlyCheaperThanDeficitNow := simInFuture && slot.GridChargeDollarsPerKWH-gridChargeNowCost >= minDeficitDiff
				// an active charge during its cheapest window.
				isCheapestWindow := equalCosts(cheapestCandidateSlot.cost, gridChargeNowCost) && gridChargeNowCost <= summary.MinFutureGridChargeCost

				// Hysteresis: If we are already charging from grid and right now is tied for the cheapest window,
				// we keep charging. This prevents starting and stopping charging when multiple hours are equally cheap.
				isAlreadyChargingSamePrice := canChargeNowForSlot && isAlreadyChargingGrid && isCheapestWindow && isSignificantlyCheaperThanDeficitNow

				isTiedCheapest := equalCosts(cheapestCandidateSlot.cost, effectiveGridChargeNowCost) && equalCosts(effectiveGridChargeNowCost, gridChargeNowCost)
				isCheapNow := (isTiedCheapest && isSignificantlyCheaperThanDeficitNow) || isSignificantlyCheaperNow
				if simInFuture && canChargeNowForSlot && (isCheapNow || isAlreadyChargingSamePrice) {
					// Count how many future cheap hours are available to decide if we can safely delay.
					futureCheapHours := 0
					for j := 1; j < i; j++ {
						candidateTS := simData[j].TS
						// Only count future cheap hours starting from the planned charging time.
						if !cheapestTime.IsZero() && candidateTS.Before(cheapestTime) {
							continue
						}
						// Ensure we don't plan to charge after we've already hit the first deficit,
						// because we cannot delay charging past the deficit hour.
						// However, if the first deficit is happening right now, we do not skip future hours.
						isFutureDeficit := !hitDeficitAt.IsZero() && hitDeficitAt.After(now.Add(time.Hour))
						if isFutureDeficit && candidateTS.After(hitDeficitAt) {
							continue
						}
						if !summary.HitCapacityAt.IsZero() && slot.TS.After(summary.HitCapacityAt) && !candidateTS.After(summary.HitCapacityAt) {
							continue
						}
						if simData[j].GridChargeDollarsPerKWH <= cheapestFutureChargeSlot.cost+minDeficitDiff {
							futureCheapHours++
						}
					}

					// We only delay charging if there is a future slot that is cheaper than or equal to now,
					// and there is enough cumulative charging capacity (futureCheapHours * chargeKW) to
					// cover the deficit otherwise we might as well continue charging now and re-evaluate
					// delaying later.
					futureIsCheaperOrEqual := cheapestFutureChargeSlot.cost <= effectiveGridChargeNowCost+0.001
					canDelay := !isAlreadyChargingSamePrice && futureIsCheaperOrEqual && futureCheapHours > 0 && float64(futureCheapHours)*chargeKW >= neededEnergy

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
						// If the required charging duration to cover the deficit is less than 10 minutes,
						// we ignore immediate charging to prevent tiny grid-charging sessions. However, we
						// still plan a future charge so that if the deficit grows later, we will be ready.
						if isTinyDeficitCharge {
							log.Ctx(ctx).DebugContext(ctx, "skipping immediate deficit charge because deficit charge duration is less than 10 minutes",
								slog.Float64("deficitChargeDurationHours", deficitChargeDurationHours),
								slog.Float64("neededDeficitEnergy", neededDeficitEnergy),
								slog.Float64("chargeKW", chargeKW),
								slog.Time("until", cheapestCandidateSlot.ts),
								slog.Float64("cheapestCost", cheapestCandidateSlot.cost),
							)
							if plannedChargeTime.IsZero() || cheapestCandidateSlot.cost < plannedChargeCost {
								plannedChargeTime = cheapestCandidateSlot.ts
								plannedChargePrice = cheapestCandidateSlot.price
								plannedChargeCost = cheapestCandidateSlot.cost
								planBenefitDollars = neededEnergy * (averageDeficitRateDollarsPerKWH - plannedChargeCost)
							}
						} else {
							log.Ctx(ctx).DebugContext(ctx, "charging now to avoid deficit",
								slog.Float64("effectiveGridChargeNowCost", effectiveGridChargeNowCost),
								slog.Float64("neededEnergy", neededEnergy),
								slog.Bool("isSignificantlyCheaperNow", isSignificantlyCheaperNow),
								slog.Bool("isAlreadyChargingSamePrice", isAlreadyChargingSamePrice),
								slog.Bool("isSignificantlyCheaperFuture", isSignificantlyCheaperFuture),
								slog.Bool("isAlreadyChargingGrid", isAlreadyChargingGrid),
								slog.Any("cheapestCandidateSlot", cheapestCandidateSlot),
								slog.Time("hitDeficitAt", hitDeficitAt),
							)
							shouldCharge = true
							chargeDescription = fmt.Sprintf(
								"Projected Deficit at %s. Charge Now ($%.3f) <= Later ($%.3f).",
								hitDeficitAt.Format(time.Kitchen),
								effectiveGridChargeNowCost,
								cheapestCandidateSlot.cost,
							)
							futurePrice = &cheapestCandidateSlot.price
							chargeActionReason = types.ActionReasonDeficitChargeNow
							chargeBenefitDollars = neededEnergy * (averageDeficitRateDollarsPerKWH - effectiveGridChargeNowCost)
							break
						}
					}
				}

				if simInFuture {
					isSignificantlyCheaper := isSignificantlyCheaperFuture ||
						(cheapestCandidateSlot.cost <= gridChargeNowCost && isSignificantlyCheaperThanDeficit)
					if isSignificantlyCheaper && (plannedChargeTime.IsZero() || cheapestCandidateSlot.cost < plannedChargeCost) {
						plannedChargeTime = cheapestCandidateSlot.ts
						plannedChargePrice = cheapestCandidateSlot.price
						plannedChargeCost = cheapestCandidateSlot.cost
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
			Description: fmt.Sprintf("deficit expected at %s, planned charge at %s ($%.3f)",
				hitDeficitAt.Format("15:04"), plannedChargeTime.Format("15:04"), plannedChargeCost),
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

	// isAlreadyChargingGrid indicates if the system is currently actively drawing power from the grid
	// to charge the battery. To prevent phantom draw from tricking us into thinking we're charging,
	// and to ensure solar-only charging doesn't confuse us, we require the battery charging rate
	// to be more than 1 kW (BatteryKW < -1.0) and grid import to be positive (GridKW > 0).
	isAlreadyChargingGrid := currentStatus.BatteryKW < -1.0 && currentStatus.GridKW > 0
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
	minStartChargeDurationHours := float64(settings.MinStartChargeMinutes) / 60.0

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
	// We calculate this as the current headroom adjusted by the cumulative net load (home load minus solar surplus)
	// up to the end of the last cheap charging hour before the peak. We limit the summation to the end of the cheap
	// window because any home load occurring after the cheap window has ended cannot be pre-charged (since the battery
	// will already be full at the end of the cheap window, and any further capacity is physically capped).
	lastCheapTS := now
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(targetAt) {
			break
		}
		if len(simPrevChargeCosts) > 0 && slot.GridChargeDollarsPerKWH <= cheapestFutureChargeSlot.cost+minDeficitDiff {
			lastCheapTS = slot.TS
		}
	}
	netLoadLimit := targetAt
	if !lastCheapTS.Equal(now) {
		netLoadLimit = lastCheapTS.Add(time.Hour)
		if netLoadLimit.After(targetAt) {
			netLoadLimit = targetAt
		}
	}

	totalNetLoadKWH := 0.0
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		if !slot.TS.Before(netLoadLimit) {
			break
		}
		applyRatio := slot.EnergyApplyRatio
		if applyRatio == 0.0 {
			applyRatio = 1.0
		}
		totalNetLoadKWH += slot.ClampedNetLoadSolarKWH * applyRatio
	}
	maxDischargeKWH := max(0.0, currentEnergyKWH-minKWH)
	if totalNetLoadKWH > maxDischargeKWH {
		totalNetLoadKWH = maxDischargeKWH
	}
	requiredChargeEnergy := max(0.0, headroom+totalNetLoadKWH)

	// Limit requiredChargeEnergy by the estimated useful solar export opportunity during the peak.
	// Since export arbitrage only grid-charges to enable solar export, we only care about solar surplus
	// after home load is met (net load < 0) during the peak window. Home load reduces solar surplus first,
	// so tracking only solar surplus after home load is met is economically correct.
	// Rather than assuming a contiguous block of slots using a pre-calculated peakEnd, we look at
	// every opportunity where there is solar surplus (NetLoadSolarKWH <= 0) and the price is high enough
	// (GridChargeDollarsPerKWH >= targetValue - minArbitrageDiff). As soon as we hit a slot where either
	// condition is not met (low price or home load > 0), we stop. This is because we will re-evaluate
	// at that future hour whether to charge up and export again.
	peakOpportunityKWH := 0.0
	inPeakWindowOpt := false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeakWindowOpt = true
		}
		if inPeakWindowOpt {
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff && slot.NetLoadSolarKWH <= 0 {
				applyRatio := slot.EnergyApplyRatio
				if applyRatio == 0.0 {
					applyRatio = 1.0
				}
				solarSurplus := -slot.NetLoadSolarKWH
				peakOpportunityKWH += solarSurplus * applyRatio
			} else {
				break
			}
		}
	}
	requiredChargeEnergy = min(requiredChargeEnergy, peakOpportunityKWH)

	// Simulate Case A (standby/no grid charging before peak) and Case B (with grid charging requiredChargeEnergy before peak)
	// to check if we will actually export the grid-charged energy.
	standbyEnergyAtPeakStart := currentEnergyKWH
	for _, slot := range simData {
		if slot.TS.Add(time.Hour).Equal(targetAt) {
			standbyEnergyAtPeakStart = slot.BatteryKWH
			break
		}
	}

	// Cap requiredChargeEnergy by the physical battery headroom at the start of the peak.
	// Since we cannot grid-charge the battery beyond its physical capacity, the maximum
	// grid energy we can store before the peak is limited by this headroom.
	standbyHeadroom := max(0.0, capacityKWH-standbyEnergyAtPeakStart)
	if requiredChargeEnergy > standbyHeadroom {
		requiredChargeEnergy = standbyHeadroom
	}

	// Case A: Standby (no grid charging)
	energyA := standbyEnergyAtPeakStart
	exportA := 0.0
	inPeakWindowSim := false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeakWindowSim = true
		}
		if inPeakWindowSim {
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff && slot.NetLoadSolarKWH <= 0 {
				applyRatio := slot.EnergyApplyRatio
				if applyRatio == 0.0 {
					applyRatio = 1.0
				}
				netLoad := slot.NetLoadSolarKWH * applyRatio
				// Since NetLoadSolarKWH <= 0, netLoad is negative or zero, representing solar surplus.
				// Solar surplus charges battery first, any excess is exported.
				chargeAmount := min(-netLoad, capacityKWH-energyA)
				energyA += chargeAmount
				// We only export what didn't go in the battery
				exportA += -netLoad - chargeAmount
			} else {
				break
			}
		}
	}

	// Case B: With grid charging
	energyB := standbyEnergyAtPeakStart + requiredChargeEnergy
	if energyB > capacityKWH {
		energyB = capacityKWH
	}
	exportB := 0.0
	inPeakWindowSim = false
	for _, slot := range simData {
		if slot.TS.Before(targetAt) {
			continue
		}
		if slot.TS.Equal(targetAt) {
			inPeakWindowSim = true
		}
		if inPeakWindowSim {
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff && slot.NetLoadSolarKWH <= 0 {
				applyRatio := slot.EnergyApplyRatio
				if applyRatio == 0.0 {
					applyRatio = 1.0
				}
				netLoad := slot.NetLoadSolarKWH * applyRatio
				// Since NetLoadSolarKWH <= 0, netLoad is negative or zero, representing solar surplus.
				// Solar surplus charges battery first, any excess is exported.
				chargeAmount := min(-netLoad, capacityKWH-energyB)
				energyB += chargeAmount
				// We only export what didn't go in the battery
				exportB += -netLoad - chargeAmount
			} else {
				break
			}
		}
	}

	// Calculate the increase we get by grid charging versus staying standby
	exportIncrease := exportB - exportA

	// We only grid-charge if the estimated export increase at the peak is at least the required charge energy.
	if requiredChargeEnergy > 0 && exportIncrease < requiredChargeEnergy-0.01 {
		requiredChargeEnergy = 0.0
	}

	futureCheapHours := 0
	for _, slot := range simData {
		if slot.TS.Equal(now) {
			continue
		}
		// Only count future cheap hours starting from the planned charging time
		if !cheapestTime.IsZero() && slot.TS.Before(cheapestTime) {
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

	// Delaying is allowed if the future is cheaper or equal (so we don't pay more by waiting).
	// However, if we are already charging, we require a significantly cheaper future window (hysteresis)
	// so we don't stop/start charging when prices are equal.
	futureIsCheaperOrEqual := len(simPrevChargeCosts) > 0 && cheapestFutureChargeSlot.cost <= chargeCostNow+0.001
	delayAllowed := false
	if isAlreadyChargingGrid || !canChargeNowReal {
		delayAllowed = isSignificantlyCheaperFuture
	} else {
		delayAllowed = futureIsCheaperOrEqual
	}

	// canDelay determines if we can safely postpone grid charging to a future cheap window.
	// Delaying is allowed if:
	// 1. Grid charging is enabled in user settings and is not disabled on the ESS (gridChargingAllowed).
	// 2. Arbitrage is profitable in the future (futureArbitrageProfitable).
	// 3. Solar is not already projected to fill the battery before the peak (solarFillsBattery).
	// 4. Postponing charging is allowed based on price comparison and charging state (delayAllowed).
	// 5. The economic benefit of delaying is greater than or equal to charging now (netGain <= 0).
	gridChargingAllowed := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled
	canDelay := gridChargingAllowed &&
		futureArbitrageProfitable &&
		!standbySolarFillsBatteryBeforePeak &&
		delayAllowed &&
		futureCheapHours > 0

	shouldDelayOverCharge := canDelay && float64(futureCheapHours)*chargeKW >= requiredChargeEnergy

	// If no charge is needed, we should neither delay nor charge.
	if requiredChargeEnergy <= 0 {
		canDelay = false
		shouldDelayOverCharge = false
	}

	log.Ctx(ctx).DebugContext(ctx, "arbitrage evaluation variables",
		slog.Float64("effectiveExportValue", effectiveExportValue),
		slog.Float64("effectiveGridChargeNowCost", effectiveGridChargeNowCost),
		slog.Float64("chargeCostNow", chargeCostNow),
		slog.Bool("canChargeArbitrage", canChargeArbitrage),
		slog.Bool("standbySolarFillsBatteryBeforePeak", standbySolarFillsBatteryBeforePeak),
		slog.Bool("canDelay", canDelay),
		slog.Bool("shouldDelayOverCharge", shouldDelayOverCharge),
		slog.Float64("requiredChargeEnergy", requiredChargeEnergy),
		slog.Bool("futureArbitrageProfitable", futureArbitrageProfitable),
		slog.Bool("isSignificantlyCheaperFuture", isSignificantlyCheaperFuture),
		slog.Int("futureCheapHours", futureCheapHours),
		slog.Any("cheapestFutureChargeSlot", cheapestFutureChargeSlot),
		slog.Float64("exportB", exportB),
		slog.Float64("exportA", exportA),
		slog.Float64("standbyEnergyAtPeakStart", standbyEnergyAtPeakStart),
	)

	if shouldDelayOverCharge {
		log.Ctx(ctx).DebugContext(ctx,
			"arbitrage: delaying charge (sufficient future capacity)",
			slog.Time("until", cheapestTime),
			slog.Float64("cost", cheapestCost),
		)
		return &StrategyEvaluation{
			Plan: &futurePlan{
				ChargeTime:  cheapestTime,
				ChargePrice: cheapestPrice,
				ChargeCost:  cheapestCost,
				Description: fmt.Sprintf("export arbitrage peak at %s, planned charge at %s ($%.3f)",
					targetAt.Format("15:04"), cheapestTime.Format("15:04"), cheapestCost),
			},
			BenefitDollars: requiredChargeEnergy * (effectiveExportValue - cheapestCost),
		}
	}

	// Find the end of the peak window starting at targetAt.
	// The peak window is the contiguous block of hours starting from targetAt where the export rate
	// is high enough to be profitable (i.e. grid charge cost/price is >= targetValue - minArbitrageDiff).
	// We stop as soon as the price falls below the threshold, or if there is positive home load (NetLoadSolarKWH > 0).
	// If there is home load > 0, it means home load will consume the battery energy first and we will re-evaluate
	// at that hour whether to charge up again for any future export opportunity.
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
			if slot.GridChargeDollarsPerKWH >= targetValue-minArbitrageDiff && slot.NetLoadSolarKWH <= 0 {
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
	// 4. We actually need to charge some energy.
	canChargeNow := requiredChargeEnergy > 0 && effectiveExportValue-chargeCostNow >= minArbitrageDiff && canChargeArbitrage && !standbySolarFillsBatteryBeforePeak

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

	if canDelay {
		log.Ctx(ctx).DebugContext(ctx,
			"arbitrage: delaying charge (charging now unprofitable, but cheap future capacity exists)",
			slog.Time("until", cheapestTime),
			slog.Float64("cost", cheapestCost),
		)
		return &StrategyEvaluation{
			Plan: &futurePlan{
				ChargeTime:  cheapestTime,
				ChargePrice: cheapestPrice,
				ChargeCost:  cheapestCost,
				Description: fmt.Sprintf("export arbitrage peak at %s, planned charge at %s ($%.3f)",
					targetAt.Format("15:04"), cheapestTime.Format("15:04"), cheapestCost),
			},
			BenefitDollars: requiredChargeEnergy * (effectiveExportValue - cheapestCost),
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
	hitDeficitAt := summary.HitBelowDeficitAt
	if hitDeficitAt.IsZero() {
		hitDeficitAt = summary.HitDeficitAt
	}

	minDiff := max(0.001, settings.MinDeficitPriceDifferenceDollarsPerKWH)
	gridChargeNowCost := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH

	// Cheap Price Retention check:
	// If the current price is cheap compared to the planned future price (the difference is smaller than minDiff),
	// we shouldn't discharge. We standby to wait for the planned charge window.
	// Only apply this check if the plan is in the future.
	// We also skip this check if the battery will hit capacity before the planned charge,
	// since any energy preserved now will be overwritten when the battery fills up.
	isCheapNow := plan.Time.After(now) && plan.Cost+minDiff > gridChargeNowCost && (summary.HitFutureCapacityAt.IsZero() || !summary.HitFutureCapacityAt.Before(plan.Time))

	// Future Peak Preservation scanning:
	// We scan the simulated hours between now and the planned charge time to identify if there are any peak hours
	// that are more expensive than our current price.
	// If we would hit a deficit (HitAboveDeficitAt) before or during that future peak, discharging the battery now
	// to cover home load would deplete it prematurely, forcing us to buy from the grid at those higher peak rates.
	// Thus, we force Standby now to preserve the battery's energy specifically to offset the highest upcoming peak.
	// If the planned charge time is now or in the past (e.g. we decided to skip it), we scan the entire simulation window.
	var scanUntil time.Time
	if plan.Time.After(now) {
		scanUntil = plan.Time
	}
	if !summary.HitFutureCapacityAt.IsZero() && (scanUntil.IsZero() || summary.HitFutureCapacityAt.Before(scanUntil)) {
		scanUntil = summary.HitFutureCapacityAt
	}
	bufferMinutes := 0
	if currentStatus.ElevatedMinBatterySOC {
		bufferMinutes = settings.PeakSurvivalBufferMinutes
	}
	mustStandbyForPeak, peakTime, peakCost, peakPrice := c.checkPeakSurvival(simData, scanUntil, gridChargeNowCost, hitAboveDeficitAt, bufferMinutes)

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
			futurePrice = peakPrice
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
	if hitDeficitAt.IsZero() || !hitDeficitAt.Before(plan.Time) {
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
	hitDeficitAt := summary.HitBelowDeficitAt
	if hitDeficitAt.IsZero() {
		hitDeficitAt = summary.HitDeficitAt
	}

	// Add Hysteresis:
	// If the current actual status of the battery is already in standby (ElevatedMinBatterySOC),
	// it means we are actively mitigating a deficit. We should only clear the deficit
	// if the battery's projected energy is comfortably above the minimum (HitAboveDeficitAt).
	// This prevents toggling the mode on and off due to 0.1% SOC prediction fluctuations.
	isMitigatingDeficit := currentStatus.ElevatedMinBatterySOC
	if isMitigatingDeficit {
		if !summary.HitAboveDeficitAt.IsZero() {
			hitDeficitAt = summary.HitAboveDeficitAt
		}
	}

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

	// 2. Peak Survival Standby:
	// If we drop below our above-deficit threshold (minKWH + 1% capacity) before or during a future peak price period,
	// we standby now to conserve the battery's energy specifically for that peak.
	// We check this even if no hard deficit (HitDeficitAt) is predicted, to ensure we respect PeakSurvivalBufferMinutes.
	var peakCost float64
	if !hitAboveDeficitAt.IsZero() {
		var scanUntil time.Time
		if !summary.HitFutureCapacityAt.IsZero() {
			scanUntil = summary.HitFutureCapacityAt
		}
		bufferMinutes := 0
		if currentStatus.ElevatedMinBatterySOC {
			bufferMinutes = settings.PeakSurvivalBufferMinutes
		}
		var mustStandbyForPeak bool
		var peakTime time.Time
		var peakPrice *types.Price
		mustStandbyForPeak, peakTime, peakCost, peakPrice = c.checkPeakSurvival(simData, scanUntil, gridChargeNowCost, hitAboveDeficitAt, bufferMinutes)

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
				FuturePrice: peakPrice,
			}
		}
	}

	// 3. Projected Deficit Fallback Handling:
	// If a future deficit is predicted and we have no active planned charge (e.g. grid charging is disabled):
	if !hitDeficitAt.IsZero() {
		// If the simulation indicates the battery will hit capacity from solar strictly in the future (After(now))
		// before hitting a deficit, it is safe to discharge now (Load) to cover home load and prevent solar curtailment.
		// We use HitSolarCapacityAt (which only tracks future capacity hits) because a capacity hit at 'now' only
		// tells us the battery is currently full, not whether it will refill in the future if we choose to discharge
		// it now.
		// If solar exporting is enabled, hitting capacity does not result in curtailment (the excess solar is exported),
		// so we do not discharge early, preserving the battery energy for high-value peak periods.
		if !summary.HitSolarCapacityAt.IsZero() && summary.HitSolarCapacityAt.Before(hitAboveDeficitAt) {
			reason := types.ActionReasonPreventSolarCurtailment
			loadReason := fmt.Sprintf("Solar curtailment likely at %s before deficit at %s.", summary.HitSolarCapacityAt.Format(time.Kitchen), hitAboveDeficitAt.Format(time.Kitchen))

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

		// Since we didn't standby for peak, we must be in a peak hour or peak survival is not required.
		// We fall back to discharging to cover load.
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

	// 4. Fallback when there is no deficit: discharge to cover load.
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

func equalCosts(a, b float64) bool {
	return math.Abs(a-b) <= 0.001
}

// checkPeakSurvival scans the simulated hours to determine if discharging the battery now
// would cause it to prematurely deplete before or during an upcoming peak price period.
//
// We scan the simulated hours between now and the `scanUntil` bounding time (which can be a planned charge
// time, or the time the battery naturally hits capacity) to identify if there are any peak hours
// that are more expensive than our current price.
//
// If we would hit a deficit (HitAboveDeficitAt) before or during that future peak, discharging the battery now
// to cover home load would deplete it prematurely, forcing us to buy from the grid at those higher peak rates.
// Thus, we return mustStandby=true to preserve the battery's energy specifically to offset the highest upcoming peak.
func (c *Controller) checkPeakSurvival(
	simData []SimHour,
	scanUntil time.Time,
	gridChargeNowCost float64,
	hitAboveDeficitAt time.Time,
	bufferMinutes int,
) (mustStandby bool, peakTime time.Time, peakCost float64, peakPrice *types.Price) {
	var peakEnd time.Time

	for _, slot := range simData {
		// Stop looking once we reach the bounding time (e.g., planned charge time, or refilling to capacity).
		// If we hit capacity before the peak price, we will have a full battery at the peak anyway,
		// so there is no reason to standby now (we should discharge now to utilize the battery and headroom).
		// Note that for capacity bounding, we pass HitFutureCapacityAt (strictly in the future, After(now))
		// instead of HitCapacityAt because if the battery starts full at 'now', HitCapacityAt would be 'now'.
		// That would immediately break the loop on the first slot, preventing us from scanning for peak prices.
		// Since the battery will discharge as time passes, we only care about future capacity hits that refill it later.
		if !scanUntil.IsZero() && !slot.TS.Before(scanUntil) {
			break
		}

		if slot.GridChargeDollarsPerKWH > gridChargeNowCost {
			slotEnd := slot.TS.Add(time.Hour)
			if peakEnd.IsZero() || slotEnd.After(peakEnd) {
				peakEnd = slotEnd
			}
			if slot.GridChargeDollarsPerKWH > peakCost {
				peakCost = slot.GridChargeDollarsPerKWH
				peakTime = slot.TS
				p := slot.Price
				peakPrice = &p
			}
		}
	}

	if !peakEnd.IsZero() && !hitAboveDeficitAt.IsZero() {
		if !hitAboveDeficitAt.After(peakEnd.Add(time.Duration(bufferMinutes) * time.Minute)) {
			mustStandby = true
		}
	}
	return
}
