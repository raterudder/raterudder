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

const (
	// minPlanningHorizonHours is the minimum lookahead required for accurate planning before erroring out.
	minPlanningHorizonHours = 4

	// maxPlanningHorizonHours is the default target duration (24 hours) for the planning horizon.
	maxPlanningHorizonHours = 24

	// maxFlexibleHorizonHours (34 hours) is the maximum duration the planning horizon may be extended
	// so that the horizon concludes in an off-peak valley (~1 hour after an ongoing or imminent peak window)
	// rather than terminating mid-peak or immediately before a peak window.
	maxFlexibleHorizonHours = 34

	// periodIdealDuration is the targeted standard duration (20 minutes) for planning intervals.
	periodIdealDuration = 20 * time.Minute

	// periodSplitThresholdDuration (40 minutes, or 2x periodIdealDuration) is the threshold above which
	// longer intervals (such as 1-hour tariff blocks) are subdivided into periodIdealDuration chunks.
	periodSplitThresholdDuration = 2 * periodIdealDuration

	// minSolarGenerationKW is the threshold below which solar generation is treated as negligible.
	minSolarGenerationKW = 0.05

	// vppStandbyLeadTime is the duration prior to a VPP event during which the battery
	// must be held in standby at 100% capacity to prevent the hardware ESS from taking over.
	vppStandbyLeadTime = 2 * time.Hour

	// batteryHoldingCostPerHourPerKWH ($0.0001/kWh-hour, or 0.01¢/kWh-hour) is a minute time-preference
	// tie-breaker applied by stepPenaltyCost during trajectory scoring. It is intentionally orders of
	// magnitude smaller than real tariff spreads or wear gates so it never overrides profitable arbitrage,
	// while breaking exact $0.00 physical-cost ties across three scenarios:
	//  1. Load vs. Standby across equal-rate blocks: Discharging 1 kWh now (Load) vs. idling in Standby
	//     (importing at rate R) and discharging that 1 kWh hours later at the same rate R has identical
	//     grid cost, wear, and terminal valuation; the holding penalty makes immediate self-consumption
	//     strictly cheaper than unnecessary Standby holds.
	//  2. Leaving headroom for free solar refills: When upcoming daytime solar will refill the battery to
	//     100% anyway, holding excess grid-charged energy through the morning incurs holding cost without
	//     increasing the post-solar SOC, steering Pass 2 target-SOC search to charge only what is needed
	//     to survive pre-solar load.
	//  3. Just-in-time charging across equal-rate off-peak blocks: When multiple off-peak blocks share the
	//     same import rate, charging in the later block keeps stored energy lower during earlier hours.
	batteryHoldingCostPerHourPerKWH = 0.0001

	// minSignificantSolarKW is the threshold (in kW) above which solar production is considered
	// materially significant for battery charging or daytime export arbitrage. This filters out dawn/dusk sensor noise,
	// pyranometer trickle, or heavily overcast conditions (< 500 W) that cannot meaningfully overcome inverter tare load
	// or deliver usable battery charging.
	minSignificantSolarKW = 0.5

	// socTargetTolerancePct (0.5% SOC) is the numerical tolerance allowed when verifying whether a simulated
	// charge or export window reached its target or floor SOC.
	socTargetTolerancePct = 0.5

	// priceMaterialityThresholdDollars ($0.03/kWh, or 3.0¢/kWh) is the minimum price differential required for an economic
	// distinction to be meaningful when evaluating rate jumps, standby holds, and decision explanations (e.g. waiting to charge vs charging now).
	// Differences below 3 cents are typically minor hourly wholesale fluctuations or insignificant tariff riders.
	priceMaterialityThresholdDollars = 0.03

	// minPeakRateSpreadDollars ($0.10/kWh, or 10¢/kWh) is the minimum required price spread across the planning horizon
	// before declaring an interval as "peak" in user explanations. This prevents minor off-peak variations or riders
	// (like 15¢ vs 8¢) from erroneously telling users they are in an expensive "peak rate" period when true TOU peak
	// pricing is typically 25¢–65¢/kWh.
	minPeakRateSpreadDollars = 0.10

	// defaultBatteryPowerSmallKW is the nominal fallback charge/discharge inverter power (5.0 kW)
	// used when inverter telemetry does not report max charge rate for typical residential systems (< 20 kWh).
	defaultBatteryPowerSmallKW = 5.0

	// defaultBatteryPowerLargeKW is the nominal fallback charge/discharge inverter power (10.0 kW)
	// for larger dual-inverter or high-capacity residential systems (>= 20 kWh).
	defaultBatteryPowerLargeKW = 10.0

	// largeBatteryCapacityThresholdKWH is the capacity threshold (20 kWh) above which a system
	// is presumed to have multi-inverter capability supporting 10 kW charge/discharge.
	largeBatteryCapacityThresholdKWH = 20.0

	// reserveFloorToleranceSOC is the numerical tolerance (0.75% SOC) used when evaluating if
	// the battery is resting at its emergency reserve floor.
	reserveFloorToleranceSOC = 0.75

	// minReserveDeficitPenaltyDollarsPerKWH ($1.00/kWh) is the minimum penalty rate applied to energy deficits below the user's reserve.
	// It guarantees that the penalty strictly exceeds typical retail electricity rates and peak-shaving benefits, preventing the optimizer
	// from treating the homeowner's backup reserve as a cheap source of energy to borrow or delay recharging.
	minReserveDeficitPenaltyDollarsPerKWH = 1.00

	// reserveDeficitPenaltyMultiplier (3.0x) scales the deficit penalty with current electricity rates.
	// When import rates are high (e.g. $0.50/kWh during extreme events), a flat $1.00/kWh penalty might no longer sufficiently
	// discourage borrowing from reserve if peak arbitrage spreads are exceptionally large. Multiplying by 3x ensures the penalty
	// remains prohibitive relative to any single-cycle arbitrage gain.
	reserveDeficitPenaltyMultiplier = 3.0

	// minSignificantBatteryPowerKW (0.3 kW = 300 W) is the minimum battery charge or discharge power rate
	// required to be considered meaningful activity rather than inverter tare or idle losses.
	// When evaluated over an interval, this is scaled by durationHours (e.g. 100 Wh for 20 min).
	minSignificantBatteryPowerKW = 0.3

	// postHorizonPriceRollupTime is the duration used to blend prices beyond the planning horizon.
	postHorizonPriceRollupTime = 4 * time.Hour

	// postHorizonLoadForecastHours (4 hours) is the lookahead window used to calculate typical
	// household consumption immediately following the planning horizon for terminal battery valuation.
	postHorizonLoadForecastHours = 4

	// defaultPostHorizonLoadKWH (6.0 kWh) is the fallback post-horizon household load requirement
	// used when hourly energy forecast models are unavailable (e.g. unit tests or sparse history),
	// corresponding to 4 hours of baseline residential consumption at 1.5 kW.
	defaultPostHorizonLoadKWH = 6.0

	// terminalTier1DiscountFactor (0.95) applies a 5% time-preference discount to banked battery
	// energy up to the forecasted post-horizon home load. This ensures the solver strictly prefers
	// covering certain in-horizon home load at rate R today over hoarding energy in Standby to T+24h
	// for speculative post-horizon consumption at the same rate R tomorrow.
	terminalTier1DiscountFactor = 0.95

	// terminalSurplusDiscountFactor (0.70) applies a 30% discount to banked battery energy exceeding
	// the immediate post-horizon household load requirements, reflecting diminishing marginal utility
	// and future solar replenishment opportunity.
	terminalSurplusDiscountFactor = 0.70

	// abnormalRecentUsageThresholdKWH (1.0 kWh) is the minimum energy consumption excess above the 75th
	// percentile (Q3) baseline over the recent 1–2 hour evaluation window required to classify usage as abnormal.
	// A typical 13.5 kWh home battery reserve buffer is 10%–20% (1.35–2.7 kWh). Minor variations (< 1.0 kWh)
	// reflect normal appliance cycles (refrigerators, televisions, lighting), whereas an excess of 1.0 kWh or more
	// represents major, sustained discretionary energy draw (e.g. electric oven, stove, dryer, or car charging)
	// that materially depleted the homeowner's backup reserve.
	abnormalRecentUsageThresholdKWH = 1.0

	// defaultRoundTripEfficiency (0.90, or 90%) is the fallback AC-to-DC-to-AC round-trip conversion
	// efficiency used when OptimizationParams.RoundTripEfficiency is unpopulated or out of range.
	defaultRoundTripEfficiency = 0.90

	// defaultDCCoupledRoundTripEfficiency (0.96, or 96%) is the round-trip efficiency floor used for
	// DC-coupled solar+battery systems (such as Tesla Powerwall 3 with solar_powerwall), where PV strings
	// feed the battery DC bus via MPPT and discharge through a high-efficiency SiC inverter.
	defaultDCCoupledRoundTripEfficiency = 0.96
)

// resolveBatteryPowerKW returns the given battery charge/discharge rate in kW, falling back
// to a nominal default if unpopulated or invalid.
func resolveBatteryPowerKW(rateKW, capacityKWH float64) float64 {
	if math.IsNaN(rateKW) || rateKW <= 0 {
		if capacityKWH >= largeBatteryCapacityThresholdKWH {
			return defaultBatteryPowerLargeKW
		}
		return defaultBatteryPowerSmallKW
	}
	return rateKW
}

// sanitizeRoundTripEfficiency returns the given round-trip efficiency, falling back to
// defaultRoundTripEfficiency (0.90) if unpopulated or outside (0, 1].
func sanitizeRoundTripEfficiency(eff float64) float64 {
	if math.IsNaN(eff) || eff <= 0 || eff > 1.0 {
		return defaultRoundTripEfficiency
	}
	return eff
}

// resolveRoundTripEfficiency returns the effective round-trip efficiency for the system,
// falling back to defaultRoundTripEfficiency (0.90) if unpopulated/invalid, and elevating to at least
// defaultDCCoupledRoundTripEfficiency (0.96) when the system is DC-coupled.
func resolveRoundTripEfficiency(eff float64, currentStatus types.SystemStatus) float64 {
	eff = sanitizeRoundTripEfficiency(eff)
	if currentStatus.DCCoupled {
		return max(eff, defaultDCCoupledRoundTripEfficiency)
	}
	return eff
}

// planInterval represents a discrete period in the planning horizon.
type planInterval struct {
	startTime     time.Time
	endTime       time.Time
	durationHours float64
	price         types.Price
	importRate    float64 // Total delivered retail rate: supply + grid use/delivery fees ($/kWh)
	exportRate    float64 // Total compensation for exported energy to grid ($/kWh)
	loadKWH       float64 // Projected total home load energy for this interval (kWh)
	solarKWH      float64 // Projected total rooftop solar generation energy for this interval (kWh)
	minSOC        float64 // Active reserve limit for this interval (%)
}

// planState tracks the physical battery state at a point in time during forward simulation.
type planState struct {
	time             time.Time
	energyKWH        float64
	soc              float64
	capacityKWH      float64
	maxChargeKW      float64
	maxDischargeKW   float64
	chargingDisabled bool
}

// actionCandidate represents a permissible control action pair for an interval.
type actionCandidate struct {
	batteryMode        types.BatteryMode
	solarMode          types.SolarMode
	reason             types.ActionReason
	description        string
	targetSOC          float64
	overrideReserveSOC float64
	futurePrice        *types.Price
}

func isSamePrice(importA, exportA, importB, exportB float64) bool {
	return math.Abs(importA-importB) <= priceEpsilonForEquality &&
		math.Abs(exportA-exportB) <= priceEpsilonForEquality
}

// wearRates separates physical battery degradation costs ($/kWh, applied in stepWearCostDollars during
// trajectory scoring) from minimum price-spread hurdles ($/kWh, used exclusively for eligibility/gating):
//   - gridChargePerKWH, solarExportPerKWH, and batteryExportPerKWH are the physical wear rates applied in
//     stepWearCostDollars (where batteryExportPerKWH is the incremental export-half wear after accounting
//     for gridChargePerKWH on the charging step).
//   - arbitrageGatePerKWH (max(GridChargeDegradation, MinArbitrageDifferenceDollarsPerKWH)) and
//     exportGatePerKWH (max(BatteryExportDegradation, MinBatteryExportDifferenceDollarsPerKWH)) are compared
//     directly against raw (non-RTE-adjusted) import and export rate differences in block and trajectory
//     eligibility checks (isBlockActionAllowed, validateCandidateTrajectory, holdInterveningOffPeakBlocks,
//     and resolvePlanActionReason), never as actual wear costs or penalties (physical RTE losses and wear
//     are evaluated in stepPhysics and stepWearCostDollars during trajectory scoring).
type wearRates struct {
	gridChargePerKWH    float64
	arbitrageGatePerKWH float64
	solarExportPerKWH   float64
	batteryExportPerKWH float64
	exportGatePerKWH    float64
}

func getWearRates(settings types.Settings, optParams types.OptimizationParams) wearRates {
	solarExport := max(priceEpsilonForEquality, optParams.SolarExportDegradationDollarsPerKWH)
	effectiveExportHurdle := max(optParams.BatteryExportDegradationDollarsPerKWH, settings.MinBatteryExportDifferenceDollarsPerKWH)
	effectiveArbitrageHurdle := max(optParams.GridChargeDegradationDollarsPerKWH, settings.MinArbitrageDifferenceDollarsPerKWH)
	return wearRates{
		gridChargePerKWH:    max(priceEpsilonForEquality, optParams.GridChargeDegradationDollarsPerKWH),
		arbitrageGatePerKWH: max(priceEpsilonForEquality, effectiveArbitrageHurdle),
		solarExportPerKWH:   solarExport,
		batteryExportPerKWH: max(solarExport, optParams.BatteryExportDegradationDollarsPerKWH-optParams.GridChargeDegradationDollarsPerKWH),
		exportGatePerKWH:    max(priceEpsilonForEquality, effectiveExportHurdle),
	}
}

// intervalMetrics tracks the energy flows and monetary accounting of a single step.
type intervalMetrics struct {
	gridImportKWH            float64
	gridExportKWH            float64
	batExportKWH             float64
	solarExportKWH           float64
	batSuppliedHomeKWH       float64
	gridChargeKWH            float64
	costDollars              float64
	grossImportCostDollars   float64
	grossExportCreditDollars float64
	solarCurtailedKW         float64
}

// vppAnchor represents a single scheduled VPP event in the horizon.
type vppAnchor struct {
	eventStart time.Time
	eventEnd   time.Time
	deadline   time.Time   // Target charging completion deadline (accounting for buffer or mandatory lead time)
	boundaries []time.Time // All operational boundaries (prep deadline, mandatory takeover, event start/end)
	vppSoc     float64
	mandatory  bool
	price      float64
}

// postHorizonLoadSlice pairs a forecasted post-horizon net load requirement (kWh)
// with the import rate ($/kWh) of the hour in which it is consumed.
type postHorizonLoadSlice struct {
	loadKWH    float64
	importRate float64
}

// postHorizonValuation holds the forecasted electricity rates, net load slices, and solar surplus
// immediately following the planning horizon for terminal battery valuation.
type postHorizonValuation struct {
	knownPostHorizonRate       float64 // Blended replacement rate across the post-horizon window ($/kWh)
	postHorizonSolarSurplusKWH float64 // Forecasted net DC solar surplus across the post-horizon window (kWh)
	postHorizonSlices          []postHorizonLoadSlice
}

// minDeficitPriceSpread returns the minimum price spread ($/kWh) required for an economic peak.
func minDeficitPriceSpread(settings types.Settings) float64 {
	return max(minPeakRateSpreadDollars, settings.GetOptimizationParams().GridChargeDegradationDollarsPerKWH)
}

// calculateTimelinePriceRange returns the minimum and maximum import rates across the timeline.
func calculateTimelinePriceRange(timeline []planInterval) (minImport, maxImport float64) {
	if len(timeline) == 0 {
		return 0, 0
	}
	minImport = timeline[0].importRate
	maxImport = timeline[0].importRate
	for _, it := range timeline {
		if it.importRate > maxImport {
			maxImport = it.importRate
		}
		if it.importRate < minImport {
			minImport = it.importRate
		}
	}
	return minImport, maxImport
}

// isTruePeakRate returns true if the import rate is at or near the horizon peak rate
// and the horizon price spread exceeds minDeficitPriceSpread(settings).
func isTruePeakRate(importRate, minImport, maxImport float64, settings types.Settings) bool {
	return maxImport > 0 &&
		importRate >= maxImport-priceMaterialityThresholdDollars &&
		maxImport >= minImport+minDeficitPriceSpread(settings)
}

// findUpcomingPeak finds the highest price interval in timeline strictly after stepIdx.
func findUpcomingPeak(timeline []planInterval, stepIdx int) (peakIdx int, futPrice *types.Price) {
	peakIdx = -1
	var maxPeak float64
	for j := stepIdx + 1; j < len(timeline); j++ {
		if timeline[j].importRate > maxPeak {
			maxPeak = timeline[j].importRate
			peakIdx = j
		}
	}
	if peakIdx != -1 {
		futPrice = &timeline[peakIdx].price
	}
	return peakIdx, futPrice
}

// planPath represents a complete simulated trajectory over the entire horizon.
type planPath struct {
	actions   []actionCandidate
	states    []planState
	metrics   []intervalMetrics
	timeline  []planInterval
	totalCost float64
	bestScore float64
}

// sanitizeLastAction filters out faults, failed hardware dispatches, paused actions, uninitialized modes,
// and stale actions (>90 minutes) so that invalid historical records do not influence plan candidate generation or inertia.
func sanitizeLastAction(lastAction *types.Action, refTime time.Time) *types.Action {
	if lastAction == nil {
		return nil
	}
	if lastAction.Fault || lastAction.Failed || lastAction.Paused || lastAction.BatteryMode == types.BatteryModeNoChange {
		return nil
	}
	if !lastAction.Timestamp.IsZero() && !refTime.IsZero() && refTime.Sub(lastAction.Timestamp) > 90*time.Minute {
		return nil
	}
	return lastAction
}

// Plan computes the optimal battery and solar schedule across the planning horizon (4-24h)
// using a two-pass regime-block search and target-SOC line search with physical round-trip conversion efficiency.
// It returns both the immediate Decision for physical inverter execution and the full Plan for visualization.
func (c *Controller) Plan(
	ctx context.Context,
	currentStatus types.SystemStatus,
	currentPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
	lastAction *types.Action,
) (Decision, types.Plan, error) {
	now := currentStatus.Timestamp
	if now.IsZero() {
		now = time.Now()
	}
	tz := currentStatus.TimeLocation
	if tz == "" && settings.Location != nil {
		tz = settings.Location.TimeZone
	}
	if tz != "" {
		if now.Location() != nil && now.Location().String() == tz {
			// Already in target timezone
		} else if currentStatus.Timestamp.Location() != nil && currentStatus.Timestamp.Location().String() == tz {
			now = now.In(currentStatus.Timestamp.Location())
		} else if loc, err := time.LoadLocation(tz); err == nil {
			now = now.In(loc)
		}
	}

	lastAction = sanitizeLastAction(lastAction, now)

	log.Ctx(ctx).DebugContext(ctx, "plan execution started",
		slog.Time("now", now),
		slog.Float64("currentSOC", currentStatus.BatterySOC),
		slog.Float64("capacityKWH", currentStatus.BatteryCapacityKWH),
		slog.Float64("currentPriceDollarsPerKWH", currentPrice.DollarsPerKWH),
		slog.Float64("gridUseDollarsPerKWH", currentPrice.GridUseDollarsPerKWH),
		slog.Float64("homeKW", currentStatus.HomeKW),
		slog.Float64("solarKW", currentStatus.SolarKW),
		slog.Int("futurePricesCount", len(futurePrices)),
	)

	solarMode := types.SolarModeAny
	if !settings.GridExportSolar {
		solarMode = types.SolarModeNoExport
	}

	// 1. Guardrail: Missing Battery or Zero Capacity
	if currentStatus.BatteryCapacityKWH <= 0 {
		log.Ctx(ctx).WarnContext(ctx, "plan aborted: battery capacity is zero or missing",
			slog.Float64("capacityKWH", currentStatus.BatteryCapacityKWH),
		)
		fallbackAction := types.Action{
			Timestamp:       now.UTC(),
			SystemTimestamp: now,
			BatteryMode:     types.BatteryModeStandby,
			SolarMode:       solarMode,
			Reason:          types.ActionReasonMissingBattery,
			Description:     "Battery Config Missing or Capacity 0. Standby.",
			CurrentPrice:    &currentPrice,
			SystemStatus:    currentStatus,
		}
		fallbackPlan := types.Plan{
			TSCreated:          now.UTC(),
			HorizonHours:       0,
			TotalProjectedCost: 0,
			Periods:            []types.PlanPeriod{},
		}
		return Decision{Action: fallbackAction}, fallbackPlan, nil
	}

	// 2. Extract active VPP events and prep deadlines
	vppEvents := extractVPPAnchors(now, currentStatus, settings)

	// 3. Build the discrete timeline across the horizon (including price synthesis for midnight cutoffs) and post-horizon valuation
	timeline, simParams, model, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, history, weather, settings, currentStatus, vppEvents)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to build planning timeline", slog.Any("error", err))
		return Decision{}, types.Plan{}, fmt.Errorf("buildPlanningTimeline: %w", err)
	}
	postHorizon := c.buildPostHorizonValuation(timeline, futurePrices, settings, currentStatus, model)

	// 4. Initial battery state
	capKWH := currentStatus.BatteryCapacityKWH
	maxChargeKW := resolveBatteryPowerKW(currentStatus.MaxBatteryChargeKW, capKWH)
	maxDischargeKW := resolveBatteryPowerKW(currentStatus.MaxBatteryDischargeKW, capKWH)
	initialEnergy := capKWH * (currentStatus.BatterySOC / 100.0)
	initialState := planState{
		time:             now,
		energyKWH:        initialEnergy,
		soc:              currentStatus.BatterySOC,
		capacityKWH:      capKWH,
		maxChargeKW:      maxChargeKW,
		maxDischargeKW:   maxDischargeKW,
		chargingDisabled: currentStatus.BatteryChargingDisabled,
	}

	// 5. Perform the two-pass regime block plan search across the timeline
	winningPath := c.searchOptimalPlanBlocks(ctx, timeline, initialState, vppEvents, postHorizon, settings, currentStatus, lastAction)

	// 6. Finalize output: extract immediate Decision for hardware dispatch and full Plan for UI
	decision, plan := finalizeDecisionAndPlan(ctx, winningPath, timeline, currentStatus, currentPrice, now, settings, history, model)
	decision.SimulationParams = simParams
	decision.Action.SimulationParams = simParams

	log.Ctx(ctx).InfoContext(ctx, "plan execution completed successfully",
		slog.String("immediateBatteryMode", drModeString(decision.Action.BatteryMode)),
		slog.String("immediateSolarMode", solarModeString(decision.Action.SolarMode)),
		slog.String("immediateReason", string(decision.Action.Reason)),
		slog.Int("planHorizonHours", plan.HorizonHours),
		slog.Float64("totalProjectedCostDollars", plan.TotalProjectedCost),
		slog.Float64("totalExportCreditsDollars", plan.TotalExportCredits),
		slog.Float64("netEconomicBenefitDollars", plan.NetEconomicBenefit),
	)

	return decision, plan, nil
}

// selectPlanningHorizonEnd chooses the planning horizon cutoff time in [now+24h, now+34h].
// By default the horizon is 24 hours (now + maxPlanningHorizonHours). However, when now + 24h lands
// inside an elevated/peak rate window, within 1 hour after one, or within postHorizonPriceRollupTime (4h)
// before one—and allPrices contains the end of that peak window before scanLimit—extending the horizon
// to conclude 1 hour after that peak window ensures:
//  1. The entire Day 2 peak window and its immediate post-peak reserve step-up are simulated directly
//     inside the timeline rather than approximated by terminal valuation.
//  2. The post-horizon terminal valuation window ([horizonEnd, horizonEnd+4h]) sits cleanly in the
//     post-peak off-peak valley.
//  3. The timeline always spans >= 24h whenever >= 24h of prices exist, preserving full 24-hour clock
//     coverage for BuildTOUSchedule.
func selectPlanningHorizonEnd(
	now time.Time,
	latestPriceTime time.Time,
	allPrices []types.Price,
	settings types.Settings,
) time.Time {
	defaultEnd := now.Add(time.Duration(maxPlanningHorizonHours) * time.Hour)
	if !latestPriceTime.After(defaultEnd) || len(allPrices) == 0 {
		return defaultEnd
	}
	scanLimit := now.Add(time.Duration(maxFlexibleHorizonHours) * time.Hour)
	if latestPriceTime.Before(scanLimit) {
		scanLimit = latestPriceTime
	}

	minImp := math.MaxFloat64
	maxImp := -math.MaxFloat64
	minExp := math.MaxFloat64
	maxExp := -math.MaxFloat64
	for _, p := range allPrices {
		if !p.TSEnd.After(now) || !p.TSStart.Before(scanLimit) {
			continue
		}
		imp := p.ImportRateDollars()
		exp := p.ExportRateDollars(settings, allPrices)
		if imp < minImp {
			minImp = imp
		}
		if imp > maxImp {
			maxImp = imp
		}
		if exp < minExp {
			minExp = exp
		}
		if exp > maxExp {
			maxExp = exp
		}
	}

	minSpread := minDeficitPriceSpread(settings)
	hasImportPeak := maxImp-max(0.0, minImp) >= minSpread
	hasExportPeak := (settings.GridExportBatteries || settings.GridExportSolar) &&
		!settings.UtilityRateOptions.IsFlatNetMetering() &&
		maxExp-max(0.0, minExp) >= priceMaterialityThresholdDollars
	if !hasImportPeak && !hasExportPeak {
		return defaultEnd
	}

	importPeakThreshold := maxImp - priceMaterialityThresholdDollars
	exportPeakThreshold := maxExp - priceMaterialityThresholdDollars
	isElevatedPrice := func(p types.Price) bool {
		if hasImportPeak && p.ImportRateDollars() >= importPeakThreshold {
			return true
		}
		if hasExportPeak && p.ExportRateDollars(settings, allPrices) >= exportPeakThreshold {
			return true
		}
		return false
	}

	// Find the earliest elevated/peak window [peakStart, peakEnd] that is ongoing at defaultEnd,
	// ended less than 1 hour before defaultEnd, or starts within postHorizonPriceRollupTime (4h) after defaultEnd.
	lookaheadLimit := defaultEnd.Add(postHorizonPriceRollupTime)
	var peakEnd time.Time
	inPeak := false
	for _, p := range allPrices {
		if !p.TSEnd.After(now) || !p.TSStart.Before(scanLimit) {
			continue
		}
		pStart := p.TSStart
		pEnd := p.TSEnd
		if pEnd.After(scanLimit) {
			pEnd = scanLimit
		}
		if isElevatedPrice(p) {
			if !inPeak {
				if pStart.After(lookaheadLimit) {
					break
				}
				inPeak = true
			}
			peakEnd = pEnd
		} else if inPeak {
			if peakEnd.After(defaultEnd) {
				break
			}
			inPeak = false
			peakEnd = time.Time{}
		}
	}

	// Only extend when allPrices actually contains the end of that peak window before scanLimit
	// so we never extend into a truncated mid-peak boundary.
	if !peakEnd.IsZero() && peakEnd.Before(scanLimit) {
		targetEnd := peakEnd.Add(time.Hour)
		if targetEnd.After(scanLimit) {
			targetEnd = scanLimit
		}
		if targetEnd.After(defaultEnd) {
			return targetEnd
		}
	}
	return defaultEnd
}

// buildPlanningTimeline collates pricing periods and constructs discrete intervals
// across the available pricing horizon (24h by default, extended up to 34h to conclude after a peak window).
// Errors out if the available horizon is below minPlanningHorizonHours.
func (c *Controller) buildPlanningTimeline(
	ctx context.Context,
	now time.Time,
	nowPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
	currentStatus types.SystemStatus,
	vppEvents []vppAnchor,
) ([]planInterval, types.SimulationParams, []TimeProfile, error) {
	// 1. Collate pricing records
	var allPrices []types.Price
	if nowPrice.TSEnd.After(now) {
		for _, fp := range futurePrices {
			if fp.TSStart.After(now) && fp.TSStart.Before(nowPrice.TSEnd) {
				nowPrice.TSEnd = fp.TSStart
			}
		}
		allPrices = append(allPrices, nowPrice)
	}

	for _, fp := range futurePrices {
		if !fp.TSEnd.After(now) {
			continue
		}
		// If nowPrice is active, skip future prices that duplicate or overlap it at now
		if nowPrice.TSEnd.After(now) && (fp.Contains(now) || (!nowPrice.TSStart.IsZero() && fp.TSStart.Equal(nowPrice.TSStart))) {
			continue
		}
		allPrices = append(allPrices, fp)
	}

	sort.SliceStable(allPrices, func(i, j int) bool {
		if allPrices[i].TSStart.Equal(allPrices[j].TSStart) {
			return allPrices[i].TSEnd.Before(allPrices[j].TSEnd)
		}
		return allPrices[i].TSStart.Before(allPrices[j].TSStart)
	})

	deduped := make([]types.Price, 0, len(allPrices))
	for _, p := range allPrices {
		if len(deduped) > 0 && deduped[len(deduped)-1].TSStart.Equal(p.TSStart) && deduped[len(deduped)-1].TSEnd.Equal(p.TSEnd) {
			continue
		}
		deduped = append(deduped, p)
	}
	allPrices = deduped

	// Determine available horizon duration
	var latestPriceTime time.Time
	for _, p := range allPrices {
		if p.TSEnd.After(latestPriceTime) {
			latestPriceTime = p.TSEnd
		}
	}

	availableHours := 0.0
	if !latestPriceTime.IsZero() && latestPriceTime.After(now) {
		availableHours = latestPriceTime.Sub(now).Hours()
	}

	if availableHours < float64(minPlanningHorizonHours) {
		log.Ctx(ctx).WarnContext(ctx, "pricing horizon is too short for accurate planning",
			slog.Float64("availableHours", availableHours),
			slog.Int("minPlanningHorizonHours", minPlanningHorizonHours),
			slog.Time("latestPriceTime", latestPriceTime),
		)
		return nil, types.SimulationParams{}, nil, fmt.Errorf("insufficient pricing horizon: %.2f hours available, minimum required is %d hours", availableHours, minPlanningHorizonHours)
	}

	// Build energy model for hourly load and solar forecasts
	model, simParams := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

	// Build solar trend adjustment
	todaySolarTrend := 1.0
	if len(weather) == 0 {
		todaySolarTrend = c.calculateSolarTrend(ctx, now, history, model, settings)
	}

	optParams := settings.GetOptimizationParams()

	peakSolarHour := 12
	var maxSolar float64
	for _, prof := range model {
		if prof.AvgSolarKWH > maxSolar {
			maxSolar = prof.AvgSolarKWH
			peakSolarHour = prof.Hour
		}
	}

	// 2. Convert into discrete intervals across the horizon.
	// By default the horizon covers maxPlanningHorizonHours (24h), extending up to maxFlexibleHorizonHours (34h)
	// when 24h lands mid-peak or shortly before a peak so the horizon ends in the post-peak off-peak valley.
	maxEnd := selectPlanningHorizonEnd(now, latestPriceTime, allPrices, settings)

	var timeline []planInterval
	currentTime := now
	currentPrice := nowPrice
	idx := 0
	var step0SolarDepressed bool

	for currentTime.Before(maxEnd) && currentTime.Before(latestPriceTime) {
		// Default interval boundary is top of the next hour
		stepEnd := currentTime.Truncate(time.Hour).Add(time.Hour)

		// Match active price record and clamp to price boundary if price ends earlier
		foundPrice := false
		for _, p := range allPrices {
			if p.Contains(currentTime) {
				currentPrice = p
				foundPrice = true
				if p.TSEnd.Before(stepEnd) && p.TSEnd.After(currentTime) {
					stepEnd = p.TSEnd
				}
				break
			}
		}

		// Clamp stepEnd to not overshoot the start of any upcoming price record
		for _, p := range allPrices {
			if p.TSStart.After(currentTime) && p.TSStart.Before(stepEnd) {
				stepEnd = p.TSStart
			}
		}

		if !foundPrice {
			// just continue using the last found price if we don't have one
			log.Ctx(ctx).WarnContext(ctx, "no price found at current time when planning",
				slog.Time("currentTime", currentTime),
				slog.Int("totalPricesInHorizon", len(allPrices)),
				slog.Float64("availableHours", availableHours),
				slog.Time("latestPriceTime", latestPriceTime),
				slog.Any("lastPrice", currentPrice),
			)
		}

		// Check operational VPP boundaries (prep deadline, event start, event end) to split intervals cleanly
		for _, vpp := range vppEvents {
			for _, bound := range vpp.boundaries {
				if bound.After(currentTime) && bound.Before(stepEnd) {
					stepEnd = bound
				}
			}
		}

		stepEnd = stepEnd.In(currentTime.Location())

		// If duration is greater than or equal to periodSplitThresholdDuration, split it to provide finer sub-hourly resolution.
		// Chop off periodIdealDuration from the start so that an hour breaks into standard segments.
		// (Do not split intervals under periodSplitThresholdDuration, e.g. a 30-minute period should not be split into 20 and 10).
		duration := stepEnd.Sub(currentTime)
		if duration >= periodSplitThresholdDuration {
			stepEnd = currentTime.Add(periodIdealDuration)
		}

		if stepEnd.After(latestPriceTime) {
			stepEnd = latestPriceTime
		}

		// Short interval handling:
		// If duration is under 10 minutes, check if we can merge with the next boundary without crossing
		// an actual tariff/reserve change or any VPP event boundary. Note that utility APIs emit
		// separate hourly types.Price structs even when consecutive hours share the exact same TOU rate;
		// we allow merging across hourly struct boundaries as long as the import rate, export credit,
		// and minimum reserve SOC remain unchanged across [currentTime, nextBoundary).
		duration = stepEnd.Sub(currentTime)
		if duration < 10*time.Minute && !stepEnd.Equal(latestPriceTime) {
			nextBoundary := stepEnd.Add(periodIdealDuration)
			if nextBoundary.After(latestPriceTime) {
				nextBoundary = latestPriceTime
			}

			crossesBoundary := false
			currImport := currentPrice.ImportRateDollars()
			currExport := currentPrice.ExportRateDollars(settings, allPrices)
			currMinSOC := settings.GetMinBatterySOC(ctx, currentTime, currentTime.Location(), currentPrice)
			coveredUntil := currentTime
			if !currentPrice.TSEnd.IsZero() && currentPrice.TSEnd.After(coveredUntil) {
				coveredUntil = currentPrice.TSEnd
			}

			for _, p := range allPrices {
				if !p.TSEnd.After(currentTime) || !p.TSStart.Before(nextBoundary) {
					continue
				}
				if p.TSStart.After(coveredUntil) {
					crossesBoundary = true
					break
				}
				pImport := p.ImportRateDollars()
				pExport := p.ExportRateDollars(settings, allPrices)
				evalTime := p.TSStart
				if evalTime.Before(currentTime) {
					evalTime = currentTime
				}
				pMinSOC := settings.GetMinBatterySOC(ctx, evalTime, currentTime.Location(), p)
				if math.Abs(pImport-currImport) > priceEpsilonForEquality ||
					math.Abs(pExport-currExport) > priceEpsilonForEquality ||
					math.Abs(pMinSOC-currMinSOC) > priceEpsilonForEquality {
					crossesBoundary = true
					break
				}
				if p.TSEnd.After(coveredUntil) {
					coveredUntil = p.TSEnd
				}
			}
			if coveredUntil.Before(nextBoundary) {
				crossesBoundary = true
			}

			if !crossesBoundary {
				for _, vpp := range vppEvents {
					for _, b := range vpp.boundaries {
						if b.After(currentTime) && b.Before(nextBoundary) {
							crossesBoundary = true
							break
						}
					}
					if crossesBoundary {
						break
					}
				}
			}

			if nextBoundary.Sub(currentTime) <= periodSplitThresholdDuration {
				if crossesBoundary {
					log.Ctx(ctx).DebugContext(ctx, "couldn't merge short planning interval",
						slog.Time("currentTime", currentTime),
						slog.Time("originalStepEnd", stepEnd),
						slog.Time("mergedStepEnd", nextBoundary),
						slog.Duration("shortDuration", duration),
						slog.Duration("mergedDuration", nextBoundary.Sub(currentTime)),
					)
				} else {
					log.Ctx(ctx).DebugContext(ctx, "merged short planning interval",
						slog.Time("currentTime", currentTime),
						slog.Time("originalStepEnd", stepEnd),
						slog.Time("mergedStepEnd", nextBoundary),
						slog.Duration("shortDuration", duration),
						slog.Duration("mergedDuration", nextBoundary.Sub(currentTime)),
					)
					stepEnd = nextBoundary
				}
			}
		}

		if stepEnd.Sub(currentTime) < 30*time.Second {
			currentTime = stepEnd
			continue
		}

		durationHrs := stepEnd.Sub(currentTime).Hours()
		h := currentTime.Hour()
		profile := findTimeProfile(model, currentTime)

		avgSolarKWH := profile.AvgSolarKWH
		if optParams.SolarCapacityBufferMinutes > 0 && len(model) > 0 && h <= peakSolarHour {
			shiftHours := optParams.SolarCapacityBufferMinutes / 60
			shiftMinutes := optParams.SolarCapacityBufferMinutes % 60

			baseHour := (h - shiftHours + 24) % 24
			prevHour := (baseHour - 1 + 24) % 24

			fraction := float64(shiftMinutes) / 60.0
			shiftedSolar := (1.0-fraction)*findTimeProfileByHour(model, baseHour).AvgSolarKWH + fraction*findTimeProfileByHour(model, prevHour).AvgSolarKWH

			// In morning ramp up to peak solar, clamp to shifted solar to delay reaching full capacity
			if shiftedSolar < avgSolarKWH {
				avgSolarKWH = shiftedSolar
			}
		}

		// Projected solar generation energy for this interval (kWh)
		solarKWH := avgSolarKWH * durationHrs
		if currentTime.YearDay() == now.YearDay() {
			solarKWH *= todaySolarTrend
		}
		if len(history) == 0 && currentStatus.SolarKW > 0 {
			// Real-time solar active during cold start: scale bell curve so generation at now matches currentStatus.SolarKW
			hNow := now.Hour()
			nowBell := 0.1
			if hNow >= 6 && hNow <= 18 {
				dist := math.Abs(float64(hNow) - 12.0)
				nowBell = max(0.1, 1.0-(dist/6.0))
			}
			estimatedPeakKW := currentStatus.SolarKW / nowBell
			if h >= 5 && h <= 20 {
				distFromNoon := math.Abs(float64(h) - 12.0)
				bellFactor := max(0.05, 1.0-(distFromNoon/7.0))
				solarKWH = (estimatedPeakKW * bellFactor) * durationHrs
			}
		}
		// For the immediate planning interval (idx == 0), if real-time telemetry is reporting active
		// solar generation, use live generation power * durationHrs.
		// If live solar at idx == 0 is massively below idx == 0's predicted rate (< 25%), assume the
		// next 2 planning periods (40 minutes) are also depressed (clamped to never exceed their
		// forecasted solarKWH) to avoid premature battery exports before the next 20-minute re-evaluation.
		if currentStatus.SolarKW > 0 && durationHrs > 0 {
			expectedSolarKW := solarKWH / durationHrs
			if idx == 0 {
				step0SolarDepressed = expectedSolarKW > 0.1 && currentStatus.SolarKW < expectedSolarKW*0.25
				solarKWH = currentStatus.SolarKW * durationHrs
			} else if idx <= 2 && step0SolarDepressed {
				solarKWH = min(solarKWH, currentStatus.SolarKW*durationHrs)
			}
		}

		// Projected home load energy for this interval (kWh)
		loadKWH := profile.AvgHomeLoadKWH * durationHrs
		if (len(history) == 0 || loadKWH <= 0) && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
		}

		importRate := currentPrice.ImportRateDollars()
		exportRate := currentPrice.ExportRateDollars(settings, allPrices)
		for _, vpp := range vppEvents {
			if vpp.price > 0 && !currentTime.Before(vpp.eventStart) && currentTime.Before(vpp.eventEnd) {
				exportRate = vpp.price
				break
			}
		}
		minSOC := settings.GetMinBatterySOC(ctx, currentTime, currentTime.Location(), currentPrice)

		interval := planInterval{
			startTime:     currentTime,
			endTime:       stepEnd,
			durationHours: durationHrs,
			price:         currentPrice,
			importRate:    importRate,
			exportRate:    exportRate,
			loadKWH:       loadKWH,
			solarKWH:      solarKWH,
			minSOC:        minSOC,
		}

		timeline = append(timeline, interval)
		currentTime = stepEnd
		idx++
	}

	totalHorizonDuration := time.Duration(0)
	if len(timeline) > 0 {
		totalHorizonDuration = timeline[len(timeline)-1].endTime.Sub(now)
	}
	log.Ctx(ctx).DebugContext(ctx, "plan timeline built",
		slog.Int("horizonIntervals", len(timeline)),
		slog.Duration("totalHorizonDuration", totalHorizonDuration),
		slog.Float64("initialSOC", currentStatus.BatterySOC),
		slog.Float64("capacityKWH", currentStatus.BatteryCapacityKWH),
	)

	return timeline, simParams, model, nil
}

// extractVPPAnchors scans currentStatus.VPPEvents for active (non-OptOut) VPP events that overlap the
// planning horizon or its mandatory 2-hour firmware preparation lead window, computing each event's
// charging completion deadline, operational interval boundaries, and export compensation rate.
func extractVPPAnchors(
	now time.Time,
	currentStatus types.SystemStatus,
	settings types.Settings,
) []vppAnchor {
	if len(currentStatus.VPPEvents) == 0 {
		return nil
	}
	if now.IsZero() {
		now = currentStatus.Timestamp
	}

	optParams := settings.GetOptimizationParams()
	vppScanEnd := now.Add(time.Duration(maxPlanningHorizonHours)*time.Hour + vppStandbyLeadTime)
	buffer := time.Duration(optParams.VPPChargingBufferMinutes) * time.Minute

	var vppEvents []vppAnchor
	for _, ev := range currentStatus.VPPEvents {
		if ev.OptOut {
			continue
		}
		if !now.IsZero() && (!ev.TSEnd.After(now) || !ev.TSStart.Before(vppScanEnd)) {
			continue
		}
		price := ev.DollarsPerKWH
		// TODO: Find a better way to determine the export compensation price per VPP program (e.g. via utility API query or tariff schedule lookup)
		if price == 0 && !ev.Mandatory {
			price = 2.0
		}
		deadline := ev.TSStart.Add(-buffer)
		if ev.Mandatory {
			deadline = ev.TSStart.Add(-vppStandbyLeadTime)
		}
		boundaries := []time.Time{deadline, ev.TSStart, ev.TSEnd}
		vppEvents = append(vppEvents, vppAnchor{
			eventStart: ev.TSStart,
			eventEnd:   ev.TSEnd,
			deadline:   deadline,
			boundaries: boundaries,
			vppSoc:     ev.VPPSoc,
			mandatory:  ev.Mandatory,
			price:      price,
		})
	}
	sort.SliceStable(vppEvents, func(i, j int) bool {
		return vppEvents[i].eventStart.Before(vppEvents[j].eventStart)
	})
	return vppEvents
}

// buildPostHorizonValuation computes the post-horizon replacement rate, net solar surplus, and
// rate-sorted hourly net load slices across the 4 hours immediately following the planning horizon.
func (c *Controller) buildPostHorizonValuation(
	timeline []planInterval,
	futurePrices []types.Price,
	settings types.Settings,
	currentStatus types.SystemStatus,
	model []TimeProfile,
) postHorizonValuation {
	var postHorizon postHorizonValuation
	if len(timeline) == 0 {
		return postHorizon
	}

	horizonEnd := timeline[len(timeline)-1].endTime

	// 1. Known post-horizon replacement rate:
	// Blend the next 4 hours (postHorizonPriceRollupTime) of futurePrices after horizonEnd so terminal
	// battery valuation does not flip-flop on a single volatile interval.
	var postSum float64
	var postCount int
	for _, p := range futurePrices {
		if p.TSEnd.After(horizonEnd) && p.TSStart.Before(horizonEnd.Add(postHorizonPriceRollupTime)) {
			postSum += p.ImportRateDollars()
			postCount++
		}
	}
	if postCount > 0 {
		postHorizon.knownPostHorizonRate = postSum / float64(postCount)
	} else {
		// Fallback when no post-horizon future prices exist: use the final timeline interval's import rate
		// rather than averaging across the 24h timeline so ending during a multi-tier TOU peak does not
		// undervalue stored energy right before the peak continues.
		postHorizon.knownPostHorizonRate = timeline[len(timeline)-1].importRate
	}
	// Stored battery energy has a non-negative option value (>= $0.00/kWh) even when post-horizon grid prices
	// are negative, because discharging is optional.
	postHorizon.knownPostHorizonRate = max(0.0, postHorizon.knownPostHorizonRate)

	// 2. Forecasted post-horizon net home load slices paired with each hour's import rate.
	// When no historical energy model data is available (nil/empty model or all hours sitting at
	// BuildHourlyEnergyModel's 0.09 kWh standby floor), fall back to defaultPostHorizonLoadKWH (6.0 kWh)
	// distributed evenly across the post-horizon hours.
	effRT := resolveRoundTripEfficiency(settings.GetOptimizationParams().RoundTripEfficiency, currentStatus)
	oneWayEff := math.Sqrt(effRT)

	hasLoadModel := false
	for _, p := range model {
		if p.AvgHomeLoadKWH > 0.1 {
			hasLoadModel = true
			break
		}
	}
	var postHorizonSolarSurplusKWH float64
	lastHourRate := postHorizon.knownPostHorizonRate
	defaultHourlyLoadKWH := defaultPostHorizonLoadKWH / float64(postHorizonLoadForecastHours)
	for h := 0; h < postHorizonLoadForecastHours; h++ {
		tStart := horizonEnd.Add(time.Duration(h) * time.Hour)
		tEnd := tStart.Add(time.Hour)

		var weightedSum, overlapHrs float64
		for _, p := range futurePrices {
			if p.TSEnd.After(tStart) && p.TSStart.Before(tEnd) {
				oStart := p.TSStart
				if tStart.After(oStart) {
					oStart = tStart
				}
				oEnd := p.TSEnd
				if tEnd.Before(oEnd) {
					oEnd = tEnd
				}
				dur := oEnd.Sub(oStart).Hours()
				if dur > 0 {
					weightedSum += dur * p.ImportRateDollars()
					overlapHrs += dur
				}
			}
		}
		hourRate := lastHourRate
		if overlapHrs > 0 {
			hourRate = max(0.0, weightedSum/overlapHrs)
			lastHourRate = hourRate
		}

		prof := findTimeProfile(model, tStart)
		homeLoadKWH := defaultHourlyLoadKWH
		if hasLoadModel {
			homeLoadKWH = prof.AvgHomeLoadKWH
		}
		netLoadKWH := homeLoadKWH - prof.AvgSolarKWH
		if netLoadKWH <= 0 {
			// Convert AC surplus solar into stored DC battery energy via one-way charging efficiency
			postHorizonSolarSurplusKWH += (-netLoadKWH) * oneWayEff
			continue
		}
		if postHorizonSolarSurplusKWH > 0 {
			// Discharging stored DC solar energy through the inverter yields postHorizonSolarSurplusKWH * oneWayEff of AC load coverage
			offsetAC := min(netLoadKWH, postHorizonSolarSurplusKWH*oneWayEff)
			netLoadKWH -= offsetAC
			postHorizonSolarSurplusKWH -= offsetAC / oneWayEff
		}
		if netLoadKWH > 0 {
			postHorizon.postHorizonSlices = append(postHorizon.postHorizonSlices, postHorizonLoadSlice{
				loadKWH:    netLoadKWH,
				importRate: hourRate,
			})
		}
	}
	postHorizon.postHorizonSolarSurplusKWH = postHorizonSolarSurplusKWH
	// Sort slices from highest to lowest rate so banked battery energy is valued against the most
	// expensive post-horizon hours first (preserving strict concavity of terminal valuation).
	sort.SliceStable(postHorizon.postHorizonSlices, func(i, j int) bool {
		return postHorizon.postHorizonSlices[i].importRate > postHorizon.postHorizonSlices[j].importRate
	})

	return postHorizon
}

// stepPhysics computes the deterministic physical state transition and interval economics.
func stepPhysics(
	state planState,
	action actionCandidate,
	interval planInterval,
	settings types.Settings,
	roundTripEfficiency float64,
) (planState, intervalMetrics) {
	capacityKWH := state.capacityKWH
	dHours := interval.durationHours
	if capacityKWH <= 0 || math.IsNaN(capacityKWH) || dHours <= 0 || math.IsNaN(dHours) {
		cleanState := state
		if math.IsNaN(cleanState.capacityKWH) || cleanState.capacityKWH < 0 {
			cleanState.capacityKWH = 0
		}
		if math.IsNaN(cleanState.energyKWH) || cleanState.energyKWH < 0 {
			cleanState.energyKWH = 0
		}
		if math.IsNaN(cleanState.soc) || cleanState.soc < 0 {
			cleanState.soc = 0
		}
		return cleanState, intervalMetrics{}
	}

	minSOC := interval.minSOC
	if math.IsNaN(minSOC) || minSOC < 0 {
		minSOC = 20.0
	} else if minSOC > 100.0 {
		minSOC = 100.0
	}
	reserveKWH := capacityKWH * (minSOC / 100.0)

	// Half efficiency for one-way conversions: sqrt(roundTripEfficiency)
	rtEff := sanitizeRoundTripEfficiency(roundTripEfficiency)
	oneWayEff := math.Sqrt(rtEff)

	// Override reserve floor if non-zero override specified on candidate action (e.g. VPP active event)
	// or if a targetSOC floor is specified on BatteryModeExport (matching ESS MinimumSOC hardware dispatch).
	effectiveLoadReserveKWH := reserveKWH
	if action.overrideReserveSOC > 0 {
		soc := min(100.0, action.overrideReserveSOC)
		overrideKWH := capacityKWH * (soc / 100.0)
		effectiveLoadReserveKWH = overrideKWH
	} else if action.batteryMode == types.BatteryModeExport && action.targetSOC > 0 {
		soc := min(100.0, action.targetSOC)
		targetKWH := capacityKWH * (soc / 100.0)
		if targetKWH > effectiveLoadReserveKWH {
			effectiveLoadReserveKWH = targetKWH
		}
	}

	maxChargeKW := resolveBatteryPowerKW(state.maxChargeKW, capacityKWH)
	maxDischargeKW := resolveBatteryPowerKW(state.maxDischargeKW, capacityKWH)

	var metrics intervalMetrics
	newEnergy := max(0.0, min(capacityKWH, state.energyKWH))
	if math.IsNaN(newEnergy) {
		newEnergy = 0.0
	}

	totalHomeLoadKWH := max(0.0, interval.loadKWH)
	if math.IsNaN(totalHomeLoadKWH) {
		totalHomeLoadKWH = 0.0
	}
	totalSolarKWH := max(0.0, interval.solarKWH)
	if math.IsNaN(totalSolarKWH) {
		totalSolarKWH = 0.0
	}

	// 1. Solar Dispatch
	var solarToHomeKWH, solarExportKWH, solarToBatKWH, solarCurtailedKWH float64
	if action.batteryMode == types.BatteryModeExport ||
		(action.batteryMode == types.BatteryModeLoad && action.solarMode == types.SolarModeExport) {
		// Battery covers home load first; solar exports up to grid export limits.
		// However, if home load exceeds battery discharge capability,
		// solar covers remaining home load first (obeying Kirchhoff's law on residential meter).
		usableBatEnergy := max(0.0, newEnergy-effectiveLoadReserveKWH)
		maxBatDischargeKWH := min(maxDischargeKW*dHours, usableBatEnergy*oneWayEff)
		uncoveredHomeLoadKWH := max(0.0, totalHomeLoadKWH-maxBatDischargeKWH)

		solarToHomeKWH = min(totalSolarKWH, uncoveredHomeLoadKWH)
		solarSurplus := totalSolarKWH - solarToHomeKWH
		if action.solarMode != types.SolarModeNoExport {
			solarExportKWH = solarSurplus
		} else {
			solarCurtailedKWH = solarSurplus
		}
	} else {
		// Solar serves home load first
		solarToHomeKWH = min(totalSolarKWH, totalHomeLoadKWH)
		solarSurplus := max(0.0, totalSolarKWH-solarToHomeKWH)

		if action.solarMode == types.SolarModeExport && action.batteryMode != types.BatteryModeChargeAny {
			// Direct solar export mode on Standby: surplus solar exports to grid rather than charging battery
			solarExportKWH = solarSurplus
		} else {
			// BatteryModeLoad, BatteryModeStandby, and BatteryModeChargeAny with SolarModeAny:
			// Behind a single meter, solar charges battery up to available room before exporting or pulling from grid
			if !state.chargingDisabled {
				roomInBatKWH := max(0.0, capacityKWH-newEnergy)
				maxSolarChargePossible := min(solarSurplus, min(maxChargeKW*dHours, roomInBatKWH/oneWayEff))
				solarToBatKWH = maxSolarChargePossible
			}

			if action.solarMode != types.SolarModeNoExport {
				solarExportKWH = solarSurplus - solarToBatKWH
			} else {
				solarCurtailedKWH = solarSurplus - solarToBatKWH
			}
		}
	}

	unmetHomeLoadKWH := max(0.0, totalHomeLoadKWH-solarToHomeKWH)

	// 2. Battery & Grid Dispatch
	if solarToBatKWH > 0 {
		newEnergy += solarToBatKWH * oneWayEff
	}

	var gridImportKWH, batExportKWH, gridChargeKWH, batSuppliedHomeKWH float64

	switch action.batteryMode {
	case types.BatteryModeChargeAny:
		// Pull remaining charge power from grid up to max inverter charge capacity, clamped to targetSOC
		if !state.chargingDisabled {
			targetLimitKWH := capacityKWH
			if action.targetSOC > 0 && action.targetSOC < 100 {
				targetLimitKWH = capacityKWH * action.targetSOC / 100.0
			}
			roomInBatKWH := max(0.0, targetLimitKWH-newEnergy)
			remainingChargeKW := max(0.0, maxChargeKW-(solarToBatKWH/dHours))
			maxGridChargePossible := min(remainingChargeKW*dHours, roomInBatKWH/oneWayEff)
			gridChargeKWH = maxGridChargePossible
			newEnergy += gridChargeKWH * oneWayEff
		}

		// Unmet home load pulls from grid
		gridImportKWH = unmetHomeLoadKWH + gridChargeKWH

	case types.BatteryModeLoad:
		// Battery supplies unmet home load down to reserve floor
		usableBatEnergy := max(0.0, newEnergy-effectiveLoadReserveKWH)
		maxBatDischargeKWH := min(maxDischargeKW*dHours, usableBatEnergy*oneWayEff)
		batSuppliedHomeKWH = min(unmetHomeLoadKWH, maxBatDischargeKWH)

		newEnergy -= batSuppliedHomeKWH / oneWayEff
		gridImportKWH = unmetHomeLoadKWH - batSuppliedHomeKWH

	case types.BatteryModeExport:
		// Battery supplies unmet home load down to reserve floor, and exports surplus to grid down to targetSOC
		usableBatEnergy := max(0.0, newEnergy-effectiveLoadReserveKWH)
		maxBatDischargeKWH := min(maxDischargeKW*dHours, usableBatEnergy*oneWayEff)
		batSuppliedHomeKWH = min(unmetHomeLoadKWH, maxBatDischargeKWH)
		newEnergy -= batSuppliedHomeKWH / oneWayEff

		usableExportEnergy := max(0.0, newEnergy-effectiveLoadReserveKWH)
		remainingDischargeKWH := min(max(0.0, maxBatDischargeKWH-batSuppliedHomeKWH), usableExportEnergy*oneWayEff)
		batExportKWH = remainingDischargeKWH
		newEnergy -= batExportKWH / oneWayEff

		gridImportKWH = unmetHomeLoadKWH - batSuppliedHomeKWH

	case types.BatteryModeStandby:
		// Battery does not discharge for home load. Unmet home load pulls from grid.
		gridImportKWH = unmetHomeLoadKWH
	}

	// Clamp battery energy to [0, capacityKWH]
	newEnergy = max(0.0, min(capacityKWH, newEnergy))

	totalGridExportKWH := solarExportKWH + batExportKWH
	grossImportCost := gridImportKWH * interval.importRate
	grossExportCredits := totalGridExportKWH * interval.exportRate
	intervalCostDollars := grossImportCost - grossExportCredits

	endingSOC := (newEnergy / capacityKWH) * 100.0

	metrics = intervalMetrics{
		gridImportKWH:            gridImportKWH,
		gridExportKWH:            totalGridExportKWH,
		batExportKWH:             batExportKWH,
		solarExportKWH:           solarExportKWH,
		batSuppliedHomeKWH:       batSuppliedHomeKWH,
		gridChargeKWH:            gridChargeKWH,
		costDollars:              intervalCostDollars,
		grossImportCostDollars:   grossImportCost,
		grossExportCreditDollars: grossExportCredits,
		solarCurtailedKW:         solarCurtailedKWH / dHours,
	}

	newState := planState{
		time:             interval.endTime,
		energyKWH:        newEnergy,
		soc:              endingSOC,
		capacityKWH:      capacityKWH,
		maxChargeKW:      maxChargeKW,
		maxDischargeKW:   maxDischargeKW,
		chargingDisabled: state.chargingDisabled,
	}

	return newState, metrics
}

// calculateVPPDeficitSOC calculates the SOC deficit score (in integer percentage points of shortfall)
// for failing to reach the physically achievable target SOC by a mandatory VPP deadline.
func calculateVPPDeficitSOC(
	initialState planState,
	candidateState planState,
	deadline time.Time,
	oneWayEff float64,
	gridChargeBatteries bool,
) int {
	if initialState.chargingDisabled || !gridChargeBatteries {
		return 0
	}
	leadTimeHours := deadline.Sub(initialState.time).Hours()
	if leadTimeHours <= 0 {
		targetSOC := 100.0
		if candidateState.soc < targetSOC-socTargetTolerancePct {
			return int(math.Ceil(targetSOC - candidateState.soc))
		}
		return 0
	}
	capacityKWH := initialState.capacityKWH
	maxChargeKW := resolveBatteryPowerKW(initialState.maxChargeKW, capacityKWH)
	maxChargeEnergy := maxChargeKW * leadTimeHours * oneWayEff
	targetSOC := min(100.0, initialState.soc+(maxChargeEnergy/capacityKWH)*100.0)

	if candidateState.soc < targetSOC-socTargetTolerancePct {
		deficitSOC := targetSOC - candidateState.soc
		return int(math.Ceil(deficitSOC))
	}
	return 0
}

// stepWearCostDollars calculates the physical battery wear/degradation cost ($) for a single interval transition.
// Difference thresholds (arbitrageGatePerKWH and exportGatePerKWH) are only used for eligibility gating, whereas
// actual trajectory scoring always uses the physical degradation rates (gridChargePerKWH, solarExportPerKWH,
// and batteryExportPerKWH).
func stepWearCostDollars(
	cand actionCandidate,
	metrics intervalMetrics,
	roundTripEff float64,
	wear wearRates,
) float64 {
	var exportDegradationCost float64
	if cand.batteryMode == types.BatteryModeExport {
		// In BatteryModeExport, both direct battery export (batExportKWH) and battery discharge covering home
		// load that frees up solar to export (min(batSuppliedHomeKWH, solarExportKWH)) pay batteryExportPerKWH
		// (the physical export-half wear after accounting for gridChargePerKWH on the charging step).
		exportDegradationCost = (metrics.batExportKWH + min(metrics.batSuppliedHomeKWH, metrics.solarExportKWH)) * wear.batteryExportPerKWH
	} else if cand.batteryMode == types.BatteryModeLoad && cand.solarMode == types.SolarModeExport {
		exportDegradationCost = min(metrics.batSuppliedHomeKWH, metrics.solarExportKWH) * wear.solarExportPerKWH
	}

	var chargeDegradationCost float64
	if metrics.gridChargeKWH > 0 {
		chargeDegradationCost = metrics.gridChargeKWH * roundTripEff * wear.gridChargePerKWH
	}

	return exportDegradationCost + chargeDegradationCost
}

// stepPenaltyCost calculates the minute time-preference holding tie-breaker ($) for a single interval
// transition (nextState.energyKWH * durationHours * batteryHoldingCostPerHourPerKWH).
//
// Why this single tie-breaking penalty is still necessary alongside physical grid cost, wear, and terminal
// valuation: without a small per-kWh-hour holding cost ($0.0001/kWh-hour), two trajectories that displace
// or charge the same kWh across different blocks of the exact same tariff rate (or that over-charge overnight
// before free midday solar caps the battery at 100% SOC anyway) have identical physical cost, wear, and ending
// SOC. Adding this tiny holding cost breaks those $0.00 ties in favor of immediate Load self-consumption over
// unnecessary Standby holds, minimal pre-solar grid charging so daytime solar has headroom, and later
// ("just-in-time") charging across equal-rate off-peak blocks.
func stepPenaltyCost(
	nextState planState,
	interval planInterval,
) float64 {
	return nextState.energyKWH * interval.durationHours * batteryHoldingCostPerHourPerKWH
}

// calculateTerminalValuation credits or penalizes ending battery energy based on replacement cost.
func calculateTerminalValuation(
	finalState planState,
	postHorizon postHorizonValuation,
	settings types.Settings,
	timeline []planInterval,
	capacityKWH float64,
	roundTripEfficiency float64,
) float64 {
	// Derive baseReserveSOC from the lowest reserve floor on the timeline (and targetReserveSOC from the
	// final timeline interval's minSOC) rather than reading settings.MinBatterySOC directly, because
	// settings.MinBatterySOC is not updated when a user switches into variable reserve (MinBatterySOCPeriods).
	baseReserveSOC := settings.MinBatterySOC
	targetReserveSOC := baseReserveSOC
	if len(timeline) > 0 {
		baseReserveSOC = timeline[0].minSOC
		for _, it := range timeline[1:] {
			if it.minSOC < baseReserveSOC {
				baseReserveSOC = it.minSOC
			}
		}
		targetReserveSOC = timeline[len(timeline)-1].minSOC
	} else if len(settings.MinBatterySOCPeriods) > 0 {
		baseReserveSOC = settings.MinBatterySOCPeriods[0].MinBatterySOC
		for _, p := range settings.MinBatterySOCPeriods[1:] {
			if p.MinBatterySOC < baseReserveSOC {
				baseReserveSOC = p.MinBatterySOC
			}
		}
		targetReserveSOC = baseReserveSOC
	}
	baseReserveKWH := capacityKWH * (baseReserveSOC / 100.0)
	targetEnergyKWH := capacityKWH * (targetReserveSOC / 100.0)

	// Determine replacement rate at horizon end.
	// Floor at 0.0 because negative post-horizon rates would invert terminal credits into
	// penalties for ending with stored battery energy.
	replacementRate := max(0.0, postHorizon.knownPostHorizonRate)
	rtEff := sanitizeRoundTripEfficiency(roundTripEfficiency)
	oneWayEff := math.Sqrt(rtEff)
	minAcceptableBaseKWH := capacityKWH * ((baseReserveSOC - reserveFloorToleranceSOC) / 100.0)

	// 1. Ending below the base emergency reserve (lowest minSOC on the timeline):
	// Penalize heavily to eliminate cheating/arbitraging the homeowner's backup reserve.
	if finalState.energyKWH < minAcceptableBaseKWH {
		deficitKWH := baseReserveKWH - finalState.energyKWH
		deficitPenaltyRate := max(minReserveDeficitPenaltyDollarsPerKWH, replacementRate*reserveDeficitPenaltyMultiplier)
		penalty := (deficitKWH / oneWayEff) * deficitPenaltyRate
		if targetEnergyKWH > baseReserveKWH {
			touDeficitKWH := targetEnergyKWH - baseReserveKWH
			penalty += (touDeficitKWH / oneWayEff) * replacementRate
		}
		return penalty
	}

	// 2. Ending below target reserve (e.g. an elevated off-peak target like 90%), but at or above base reserve:
	// If GridChargeBatteries is allowed and replacementRate > 0, the cost to replenish the battery during
	// the upcoming off-peak period is simply the replacement energy cost (accounting for one-way charging efficiency).
	minAcceptableTargetKWH := capacityKWH * ((targetReserveSOC - reserveFloorToleranceSOC) / 100.0)
	if finalState.energyKWH < minAcceptableTargetKWH {
		deficitKWH := targetEnergyKWH - finalState.energyKWH
		if settings.GridChargeBatteries && replacementRate > 0 {
			return (deficitKWH / oneWayEff) * replacementRate
		}
		deficitPenaltyRate := max(minReserveDeficitPenaltyDollarsPerKWH, replacementRate*reserveDeficitPenaltyMultiplier)
		return (deficitKWH / oneWayEff) * deficitPenaltyRate
	}

	energyDeltaKWH := finalState.energyKWH - targetEnergyKWH
	if energyDeltaKWH <= 0 {
		// Within the +/- 0.1% ESS hardware tolerance of the reserve floor: no penalty, no credit
		return 0.0
	}

	// 3. Ending above target reserve: credit for banked energy displacing future imports.
	// Uses a multi-slice concave valuation:
	// Tier 1: Stored energy up to the forecasted post-horizon net home consumption (4 hours)
	//         is credited against each post-horizon hour's actual import rate (from highest to lowest rate)
	//         with a 5% time-preference discount (0.95x) so certain in-horizon self-consumption at rate R
	//         strictly beats hoarding energy to T+24h for the same rate R tomorrow.
	// Tier 2: Surplus energy beyond all post-horizon net home load is credited at no more than the minimum
	//         replacement rate with an additional 30% discount reflecting diminishing returns and future
	//         solar or off-peak replenishment opportunity.
	remainingKWH := energyDeltaKWH
	var totalCredit float64
	for _, slice := range postHorizon.postHorizonSlices {
		if remainingKWH <= 0 {
			break
		}
		usedKWH := min(remainingKWH, slice.loadKWH/oneWayEff)
		creditRate := max(0.0, slice.importRate) * oneWayEff * terminalTier1DiscountFactor
		totalCredit += usedKWH * creditRate
		remainingKWH -= usedKWH
	}
	if remainingKWH > 0 {
		usableCapacityKWH := max(0.0, capacityKWH-targetEnergyKWH)
		unfilledBySolarKWH := max(0.0, usableCapacityKWH-postHorizon.postHorizonSolarSurplusKWH)
		tier2KWH := min(remainingKWH, unfilledBySolarKWH)
		tier2CreditRate := replacementRate * oneWayEff * terminalTier1DiscountFactor * terminalSurplusDiscountFactor
		totalCredit += tier2KWH * tier2CreditRate
	}
	return -totalCredit
}

// resolvePlanActionReason inspects a winning step in the context of the full plan trajectory
// and assigns the precise human-facing reason and description.
func resolvePlanActionReason(
	winningPath *planPath,
	stepIdx int,
	timeline []planInterval,
	settings types.Settings,
	maxHorizonImportRate, minHorizonImportRate float64,
) (types.ActionReason, string, *types.Price) {
	if winningPath == nil || stepIdx >= len(winningPath.actions) || stepIdx >= len(timeline) {
		return types.ActionReasonSufficientBattery, "Discharging battery to cover household load.", nil
	}

	action := winningPath.actions[stepIdx]
	interval := timeline[stepIdx]
	state := winningPath.states[stepIdx]
	var metrics intervalMetrics
	if stepIdx < len(winningPath.metrics) {
		metrics = winningPath.metrics[stepIdx]
	}

	// Preserve explicit safety or active overrides set by getHardOverride
	if action.reason == types.ActionReasonEVChargingStandby ||
		action.reason == types.ActionReasonVPPActive ||
		action.reason == types.ActionReasonGridUnavailable {
		return action.reason, action.description, nil
	}

	importRate := interval.importRate
	minSOC := interval.minSOC
	isTruePeak := isTruePeakRate(importRate, minHorizonImportRate, maxHorizonImportRate, settings)

	switch action.batteryMode {
	case types.BatteryModeExport:
		return types.ActionReasonDirectExport, "Battery Grid Export Dump.", nil

	case types.BatteryModeChargeAny:
		if action.reason == types.ActionReasonAlwaysChargeBelowThreshold || importRate < 0 {
			return types.ActionReasonAlwaysChargeBelowThreshold,
				"Price below threshold; pre-charging battery.", nil
		}
		if action.reason == types.ActionReasonVPPPrep {
			return types.ActionReasonVPPPrep, action.description, nil
		}
		_, futPrice := findUpcomingPeak(timeline, stepIdx)

		// review all contiguous charges to see if we are below SOC (which might change with variable reserve)
		for j := stepIdx; j < len(winningPath.actions) && j < len(timeline); j++ {
			act := winningPath.actions[j]
			if act.batteryMode != types.BatteryModeChargeAny {
				break
			}
			if act.targetSOC <= 0 || math.Abs(act.targetSOC-timeline[j].minSOC) > socTargetTolerancePct {
				continue
			}
			if j < len(winningPath.states) && winningPath.states[j].soc < timeline[j].minSOC-reserveFloorToleranceSOC {
				return types.ActionReasonDeficitChargeNow,
					"Charging battery to target reserve.", futPrice
			}
		}

		// Scan chronologically for the next upcoming event (battery export, peak import discharge,
		// or profitable solar export arbitrage) before the next distinct grid-charge episode.
		// Checking chronologically prevents a routine afternoon solar export from overshadowing
		// an earlier morning peak discharge.
		optParams := settings.GetOptimizationParams()
		wear := getWearRates(settings, optParams)

		inCurrentChargeEpisode := true
		for j := stepIdx + 1; j < len(timeline); j++ {
			if j < len(winningPath.actions) {
				if winningPath.actions[j].batteryMode != types.BatteryModeChargeAny {
					inCurrentChargeEpisode = false
				} else if !inCurrentChargeEpisode {
					break
				}
			}

			if settings.GridExportBatteries && j < len(winningPath.metrics) && winningPath.metrics[j].batExportKWH > 0.1 {
				return types.ActionReasonArbitrageChargeExport,
					"Pre-charging for upcoming battery export arbitrage.", &timeline[j].price
			}
			if j < len(winningPath.metrics) && j < len(winningPath.actions) &&
				timeline[j].importRate-importRate >= wear.gridChargePerKWH-priceEpsilonForEquality &&
				winningPath.metrics[j].batSuppliedHomeKWH > minSignificantBatteryPowerKW*timeline[j].durationHours &&
				winningPath.actions[j].solarMode != types.SolarModeExport {
				return types.ActionReasonDeficitChargeNow,
					"Pre-charging for upcoming peak rates.", futPrice
			}
			if settings.GridExportSolar && j < len(winningPath.metrics) &&
				winningPath.metrics[j].solarExportKWH > 0.1 &&
				timeline[j].exportRate-importRate >= wear.solarExportPerKWH {
				// Before attributing the pre-charge to daytime solar export, check whether a later interval
				// in the same contiguous export/peak window exports battery energy to the grid (since
				// DirectSolarExport runs during sunny midday peak hours and BatteryModeExport runs at dusk).
				if settings.GridExportBatteries {
					for k := j; k < len(timeline) && k < len(winningPath.metrics); k++ {
						if k < len(winningPath.actions) && winningPath.actions[k].batteryMode == types.BatteryModeChargeAny {
							break
						}
						if timeline[k].exportRate-importRate < wear.solarExportPerKWH {
							break
						}
						if winningPath.metrics[k].batExportKWH > 0.1 {
							return types.ActionReasonArbitrageChargeExport,
								"Pre-charging for upcoming battery export arbitrage.", &timeline[k].price
						}
					}
				}
				return types.ActionReasonArbitrageChargeExport,
					"Pre-charging for upcoming solar export arbitrage.", &timeline[j].price
			}
		}

		return types.ActionReasonDeficitChargeNow,
			"Pre-charging for upcoming peak rates.", futPrice

	case types.BatteryModeStandby:
		if action.reason == types.ActionReasonVPPPrep {
			desc := action.description
			if desc == "" {
				desc = "Preserving battery in standby ahead of VPP event."
			}
			return types.ActionReasonVPPPrep, desc, nil
		}

		// 1. WaitingToCharge: Contiguous Standby leading directly into a scheduled ChargeAny.
		// Applies during off-peak/shoulder if no higher peak intervenes between now and that charge.
		if !isTruePeak {
			chargeIdx := -1
			for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
				if winningPath.actions[j].batteryMode == types.BatteryModeChargeAny {
					chargeIdx = j
					break
				}
				if winningPath.actions[j].batteryMode != types.BatteryModeStandby {
					break
				}
			}
			if chargeIdx != -1 {
				hasInterveningPeak := false
				for p := stepIdx + 1; p < chargeIdx; p++ {
					if timeline[p].importRate > importRate+priceMaterialityThresholdDollars {
						hasInterveningPeak = true
						break
					}
				}
				if !hasInterveningPeak {
					return types.ActionReasonWaitingToCharge,
						"Waiting to charge at scheduled window.", &timeline[chargeIdx].price
				}
			}
		}

		// 2. Battery Export Arbitrage: Upcoming battery export window scheduled in plan
		for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
			if winningPath.actions[j].batteryMode == types.BatteryModeExport && timeline[j].exportRate > importRate {
				return types.ActionReasonArbitrageHoldExport,
					"Preserving battery in standby for upcoming export window.", &timeline[j].price
			}
		}

		// 3. DeficitSaveForPeak: If an upcoming peak rate higher than current import exists, that is the primary reason to hold
		peakIdx, futPrice := findUpcomingPeak(timeline, stepIdx)
		if peakIdx != -1 && timeline[peakIdx].importRate > importRate+priceMaterialityThresholdDollars {
			return types.ActionReasonDeficitSaveForPeak, "Preserving battery in standby for upcoming peak rates.", futPrice
		}

		// 4. Solar Export Arbitrage: Upcoming solar export window scheduled in plan (when no higher peak import rate exists)
		for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
			if winningPath.actions[j].solarMode == types.SolarModeExport && timeline[j].solarKWH > minSignificantSolarKW*timeline[j].durationHours {
				return types.ActionReasonArbitrageHoldExport,
					"Preserving battery in standby for upcoming export window.", &timeline[j].price
			}
		}

		// 5. Fallback
		return types.ActionReasonDeficitSaveForPeak, "Preserving battery in standby for upcoming peak rates.", futPrice

	case types.BatteryModeLoad:
		// 1. Direct Solar Export
		if action.solarMode == types.SolarModeExport && interval.solarKWH > 0 {
			return types.ActionReasonDirectExport, "Direct Solar Export: battery covers load; solar exports.", nil
		}

		// 2. Battery At Reserve
		if state.soc <= minSOC+reserveFloorToleranceSOC && metrics.batSuppliedHomeKWH <= minSignificantBatteryPowerKW*interval.durationHours {
			return types.ActionReasonBatteryAtReserve, "Battery is at reserve. Home powered from solar/grid.", nil
		}

		// 3. DischargeAtPeak: Currently at or near peak import rate
		if isTruePeak {
			return types.ActionReasonDischargeAtPeak,
				"Discharging battery to power home during peak rate.", nil
		}

		// 4. SufficientBatteryTillCharge: A future ChargeAny is planned, and battery will reach it without hitting reserve
		for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
			if winningPath.actions[j].batteryMode == types.BatteryModeChargeAny {
				// Check if any intervening peak occurs before the charge
				hasInterveningPeak := false
				for p := stepIdx + 1; p < j; p++ {
					if timeline[p].importRate > importRate+priceMaterialityThresholdDollars {
						hasInterveningPeak = true
						break
					}
				}
				if !hasInterveningPeak {
					canReachCharge := true
					for k := stepIdx; k < j; k++ {
						if k+1 < len(winningPath.states) && winningPath.states[k+1].soc <= timeline[k].minSOC+reserveFloorToleranceSOC {
							canReachCharge = false
							break
						}
					}
					if canReachCharge {
						return types.ActionReasonSufficientBatteryTillCharge,
							"Sufficient battery to reach scheduled charge.", &timeline[j].price
					}
				}
				break
			}
		}

		// 5. PreventSolarCurtailment: Upcoming solar will exceed battery headroom and would be curtailed
		for j := stepIdx + 1; j < len(winningPath.metrics) && j < len(timeline); j++ {
			if winningPath.metrics[j].solarCurtailedKW > minSolarGenerationKW {
				return types.ActionReasonPreventSolarCurtailment,
					"Solar generation forecast to exceed capacity; discharging now to create headroom.", nil
			}
		}

		// 6. Default Load
		return types.ActionReasonSufficientBattery, "Discharging battery to cover household load.", nil
	}

	return action.reason, action.description, nil
}

// finalizeDecisionAndPlan packages the winning path into Decision and types.Plan.
func finalizeDecisionAndPlan(
	ctx context.Context,
	winningPath *planPath,
	timeline []planInterval,
	initialStatus types.SystemStatus,
	currentPrice types.Price,
	now time.Time,
	settings types.Settings,
	history []types.EnergyStats,
	model []TimeProfile,
) (Decision, types.Plan) {
	if len(winningPath.actions) == 0 || len(timeline) == 0 {
		return Decision{}, types.Plan{TSCreated: now.UTC()}
	}

	immediateInterval := timeline[0]

	// Contextual post-plan reason resolution:
	// The timeline-block solver selects optimal modes across time, while homeowner-facing explanations
	// are contextual to the full trajectory (e.g., peak discharge, waiting for scheduled charge,
	// sufficient battery until charge, holding for daytime solar refill, or preventing solar curtailment).
	minHorizonImportRate, maxHorizonImportRate := calculateTimelinePriceRange(timeline)

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		reason, desc, futPrice := resolvePlanActionReason(winningPath, i, timeline, settings, maxHorizonImportRate, minHorizonImportRate)
		winningPath.actions[i].reason = reason
		winningPath.actions[i].description = desc
		winningPath.actions[i].futurePrice = futPrice
	}
	immediateAction := winningPath.actions[0]

	// Evaluate recent home usage and Q3 baseline for action telemetry and reserve description
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	recentKWH, q3KWH, isAbnormal := calculateRecentUsageVsQ3(now, history, model, loc)
	if isAbnormal {
		log.Ctx(ctx).DebugContext(ctx, "abnormal recent home usage detected",
			slog.Float64("recentHomeUsageKWH", recentKWH),
			slog.Float64("q3HomeUsageKWH", q3KWH),
			slog.Float64("usageDeltaKWH", recentKWH-q3KWH),
			slog.String("actionReason", string(immediateAction.reason)),
			slog.Float64("batterySOC", initialStatus.BatterySOC),
		)
	}

	if immediateAction.reason == types.ActionReasonBatteryAtReserve && isAbnormal {
		immediateAction.description = "Battery is at reserve. Recent usage was well above normal. Home powered from solar/grid."
		winningPath.actions[0].description = immediateAction.description
	}

	// Resolve the integer ChargeToSOC target dispatched to the ESS hardware:
	// 1. Standby mode always reports 0 (target SOC is N/A while holding in standby).
	// 2. overrideReserveSOC takes precedence over targetSOC during active VPP events, where the
	//    contracted VPP reserve floor (e.g., 5% or 20%) overrides normal reserve/target limits.
	// 3. targetSOC is used for charging (ceiling SOC) and exporting (export floor SOC), rounded up
	//    via math.Ceil so fractional targets (e.g., 58.2%) are fully reached.
	// 4. Normal self-consumption (BatteryModeLoad with no override or targetSOC) defaults to the
	//    interval's configured minimum reserve floor (immediateInterval.minSOC).
	var targetSOC int
	if immediateAction.batteryMode == types.BatteryModeStandby {
		targetSOC = 0
	} else if immediateAction.overrideReserveSOC > 0 {
		targetSOC = int(math.Round(immediateAction.overrideReserveSOC))
	} else if immediateAction.targetSOC > 0 {
		targetSOC = min(100, int(math.Ceil(immediateAction.targetSOC-1e-9)))
	} else {
		targetSOC = int(math.Round(immediateInterval.minSOC))
	}

	// Deficit and capacity hit times detection from winning path trajectory
	var hitDeficitAt time.Time
	var hitCapacityAt time.Time

	minSOC := immediateInterval.minSOC
	if initialStatus.BatterySOC <= minSOC+reserveFloorToleranceSOC {
		hitDeficitAt = timeline[0].startTime
	}
	if initialStatus.BatterySOC >= 100.0-socTargetTolerancePct {
		hitCapacityAt = timeline[0].startTime
	}

	planDisplayEnd := now.Add(time.Duration(maxPlanningHorizonHours) * time.Hour)

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		if !timeline[i].startTime.Before(planDisplayEnd) {
			break
		}
		stepReserveSOC := timeline[i].minSOC
		if i < len(winningPath.states) && i+1 < len(winningPath.states) {
			startSOC := winningPath.states[i].soc
			endSOC := winningPath.states[i+1].soc

			if hitDeficitAt.IsZero() && endSOC <= stepReserveSOC+reserveFloorToleranceSOC {
				if startSOC > stepReserveSOC+reserveFloorToleranceSOC && startSOC > endSOC {
					frac := (startSOC - stepReserveSOC) / (startSOC - endSOC)
					if frac < 0 {
						frac = 0
					} else if frac > 1 {
						frac = 1
					}
					hitDeficitAt = timeline[i].startTime.Add(time.Duration(frac * timeline[i].durationHours * float64(time.Hour)))
				} else {
					hitDeficitAt = timeline[i].startTime
				}
			}

			if hitCapacityAt.IsZero() && endSOC >= 100.0-socTargetTolerancePct {
				if startSOC < 100.0-socTargetTolerancePct && endSOC > startSOC {
					frac := (100.0 - startSOC) / (endSOC - startSOC)
					if frac < 0 {
						frac = 0
					} else if frac > 1 {
						frac = 1
					}
					hitCapacityAt = timeline[i].startTime.Add(time.Duration(frac * timeline[i].durationHours * float64(time.Hour)))
				} else {
					hitCapacityAt = timeline[i].startTime
				}
			}
		}
	}

	act := types.Action{
		Timestamp:               now.UTC(),
		SystemTimestamp:         now,
		BatteryMode:             immediateAction.batteryMode,
		SolarMode:               immediateAction.solarMode,
		ChargeToSOC:             targetSOC,
		Reason:                  immediateAction.reason,
		Description:             immediateAction.description,
		CurrentPrice:            &currentPrice,
		FuturePrice:             immediateAction.futurePrice,
		SystemStatus:            initialStatus,
		HitDeficitAt:            hitDeficitAt,
		HitCapacityAt:           hitCapacityAt,
		RecentHomeUsageKWH:      recentKWH,
		Q3HomeUsageKWH:          q3KWH,
		RecentHomeUsageAbnormal: isAbnormal,
	}

	// Build full schedule for Plan UI, truncated to 24 hours (maxPlanningHorizonHours)
	var periods []types.PlanPeriod
	var totalCostDollars, totalExportCredits float64

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		action := winningPath.actions[i]
		interval := timeline[i]
		if !interval.startTime.Before(planDisplayEnd) {
			break
		}
		var metrics intervalMetrics
		if i < len(winningPath.metrics) {
			metrics = winningPath.metrics[i]
		}
		var startState, endState planState
		if i < len(winningPath.states) {
			startState = winningPath.states[i]
		}
		if i+1 < len(winningPath.states) {
			endState = winningPath.states[i+1]
		} else {
			endState = startState
		}

		totalCostDollars += metrics.grossImportCostDollars
		totalExportCredits += metrics.grossExportCreditDollars

		periods = append(periods, types.PlanPeriod{
			TSStart:       interval.startTime,
			TSEnd:         interval.endTime,
			DurationHours: interval.durationHours,
			ImportDollars: interval.importRate,
			ExportDollars: interval.exportRate,
			BatteryMode:   action.batteryMode,
			SolarMode:     action.solarMode,
			Reason:        action.reason,
			StartSOC:      startState.soc,
			EndSOC:        endState.soc,
			ReserveSOC:    interval.minSOC,
			LoadKWH:       interval.loadKWH,
			SolarKWH:      interval.solarKWH,
			GridImportKWH: metrics.gridImportKWH,
			GridExportKWH: metrics.gridExportKWH,
			CostDollars:   metrics.costDollars,
		})
	}

	var horizonHours int
	if len(periods) > 0 {
		horizonHours = int(math.Ceil(periods[len(periods)-1].TSEnd.Sub(periods[0].TSStart).Hours()))
	}

	// Note: NetEconomicBenefit (and StrategyBenefitDollars) currently represents net cashflow
	// (export credits minus gross import cost) over the horizon rather than savings compared
	// against a no-battery/self-consumption baseline.
	plan := types.Plan{
		TSCreated:          now.UTC(),
		HorizonHours:       horizonHours,
		TotalProjectedCost: totalCostDollars,
		TotalExportCredits: totalExportCredits,
		NetEconomicBenefit: totalExportCredits - totalCostDollars,
		Periods:            periods,
	}

	act.Plan = &plan
	act.StrategyBenefitDollars = plan.NetEconomicBenefit

	return Decision{Action: act}, plan
}

// Helper string formatters for logging
func solarModeString(m types.SolarMode) string {
	switch m {
	case types.SolarModeNoChange:
		return "NoChange"
	case types.SolarModeNoExport:
		return "NoExport"
	case types.SolarModeAny:
		return "Any"
	case types.SolarModeExport:
		return "Export"
	default:
		return fmt.Sprintf("SolarMode(%d)", m)
	}
}

// calculateRecentUsageVsQ3 evaluates recent home usage (last 1–2 hours) against the 75th percentile (Q3)
// baseline from the energy model. If actual recent usage exceeded Q3 by at least abnormalRecentUsageThresholdKWH (1.0 kWh), isAbnormal is true.
func calculateRecentUsageVsQ3(
	now time.Time,
	history []types.EnergyStats,
	model []TimeProfile,
	loc *time.Location,
) (recentKWH float64, q3KWH float64, isAbnormal bool) {
	if len(history) == 0 || loc == nil || len(model) == 0 {
		return 0, 0, false
	}

	localHour := func(t time.Time) time.Time {
		tLoc := t.In(loc)
		if tLoc.Minute() == 0 && tLoc.Second() == 0 && tLoc.Nanosecond() == 0 {
			return tLoc
		}
		return time.Date(tLoc.Year(), tLoc.Month(), tLoc.Day(), tLoc.Hour(), 0, 0, 0, loc)
	}

	currentHour := localHour(now)
	t1 := currentHour.Add(-1 * time.Hour)
	t2 := currentHour.Add(-2 * time.Hour)

	var s1, s2, sCurr types.EnergyStats
	// History is chronologically ordered, so search backwards from the end
	for i := len(history) - 1; i >= 0; i-- {
		th := localHour(history[i].TSHourStart)
		if th.Equal(t1) && s1.HomeKWH == 0 {
			s1 = history[i]
		} else if th.Equal(t2) && s2.HomeKWH == 0 {
			s2 = history[i]
		} else if th.Equal(currentHour) && sCurr.HomeKWH == 0 {
			sCurr = history[i]
		}
		if s1.HomeKWH != 0 && s2.HomeKWH != 0 && sCurr.HomeKWH != 0 {
			break
		}
		// If we've traversed into older days, stop searching
		if th.Before(t2.Add(-24 * time.Hour)) {
			break
		}
	}

	hoursCounted := 0
	if s1.HomeKWH > 0 {
		recentKWH += s1.HomeKWH
		q3KWH += findTimeProfile(model, t1).P75HomeLoadKWH
		hoursCounted++
	}
	if s2.HomeKWH > 0 {
		recentKWH += s2.HomeKWH
		q3KWH += findTimeProfile(model, t2).P75HomeLoadKWH
		hoursCounted++
	}

	// Also check in-progress current hour if it has already exceeded Q3
	if sCurr.HomeKWH > 0 {
		currQ3 := findTimeProfile(model, currentHour).P75HomeLoadKWH
		if sCurr.HomeKWH >= currQ3 && currQ3 > 0 {
			recentKWH += sCurr.HomeKWH
			q3KWH += currQ3
			hoursCounted++
		}
	}

	if hoursCounted > 0 && q3KWH > 0 && (recentKWH-q3KWH) >= abnormalRecentUsageThresholdKWH {
		isAbnormal = true
	}

	return recentKWH, q3KWH, isAbnormal
}
