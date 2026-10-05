package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// minPlanningHorizonHours is the minimum lookahead required for accurate planning before erroring out.
	minPlanningHorizonHours = 4

	// maxPlanningHorizonHours is the hard ceiling for forward lookahead.
	maxPlanningHorizonHours = 24

	// defaultInertiaThresholdDollars is the minimum monetary improvement ($) across the horizon
	// required to switch from the current active battery mode in lastAction to a new mode.
	// This prevents inverter relays from chattering when two plans differ by fractions of a cent.
	defaultInertiaThresholdDollars = 0.05

	// continueChargeHeadroomKWH is the lower headroom threshold applied when the battery is
	// already actively grid-charging. This allows an existing charge session to run smoothly
	// until the battery is essentially full without premature termination.
	continueChargeHeadroomKWH = 0.1

	// periodIdealDuration is the targeted standard duration (20 minutes) for planning intervals.
	periodIdealDuration = 20 * time.Minute

	// periodSplitThresholdDuration (40 minutes, or 2x periodIdealDuration) is the threshold above which
	// longer intervals (such as 1-hour tariff blocks) are subdivided into periodIdealDuration chunks.
	periodSplitThresholdDuration = 2 * periodIdealDuration

	// fastTrackDelayThreshold is the maximum delay duration below which a planned charge
	// at an equal price will be triggered immediately instead of waiting for a subsequent cycle.
	// Set to 15 minutes so that standard 20-minute planning intervals are evaluated by the next
	// polling cycle without eager pre-charging, while short intervals (< 15 min) are fast-tracked.
	fastTrackDelayThreshold = 15 * time.Minute

	// minSolarGenerationKW is the threshold below which solar generation is treated as negligible.
	minSolarGenerationKW = 0.05

	// minDirectSolarExportKW is the minimum solar generation threshold (0.5 kW) required
	// to enable direct solar export. Below 500 W, generation is negligible dawn/dusk sensor noise
	// or overcast trickle that does not justify cycling the battery to export solar.
	minDirectSolarExportKW = 0.5

	// vppStandbyLeadTime is the duration prior to a VPP event during which the battery
	// must be held in standby at 100% capacity to prevent the hardware ESS from taking over.
	vppStandbyLeadTime = 2 * time.Hour

	// vppMinFullCapacitySOC is the minimum state of charge (99.0%) considered approximately full
	// when entering the VPP lead-time preparation window. If the battery enters this window below
	// 99.0% SOC, ESS firmware takes autonomous control to execute an emergency grid top-up to 100%
	// regardless of tariff rates. Standby is withheld below this threshold so the planner reflects
	// actual hardware dispatch rather than proposing unrealistic idle trajectories during expensive rates.
	vppMinFullCapacitySOC = 99.0

	// batteryHoldingCostPerHourPerKWH is a minute physical holding penalty ($/kWh-hour) applied
	// during dynamic programming forward search. It breaks mathematical ties during contiguous
	// flat/equal rate windows, steering the planner to schedule charging just-in-time closer to peak
	// or VPP windows rather than prematurely charging hours in advance. This improves forecast accuracy,
	// reduces high-SOC calendar degradation, and minimizes standby losses without overriding true price differences.
	batteryHoldingCostPerHourPerKWH = 0.0001

	// modeSwitchPenalty is a nominal physical transition penalty ($0.01 = 1 cent)
	// applied during dynamic programming forward search whenever transitioning between battery modes.
	// This eliminates mode chattering, relay/contactor flutter, and fragmentation across all modes (charging,
	// discharging to load, exporting, or standby), strictly prioritizing contiguous dispatch blocks
	// while still permitting mode switches whenever economically justified.
	modeSwitchPenalty = 0.01

	// exportReserveMarginPct is the safety margin (in % SOC) maintained strictly above the effective
	// reserve floor during battery grid export. Because grid exports discharge at maximum inverter power,
	// hardware reporting latencies, inverter shutdown ramps, and cell-balancing variances can cause over-discharge.
	// Adding a 5% buffer guarantees that deliberate grid export operations terminate well before cutting into
	// the homeowner's essential emergency backup reserve.
	exportReserveMarginPct = 5.0

	// minSignificantSolarKW is the threshold (in kW) above which solar production is considered
	// materially significant for battery charging or daytime export arbitrage. This filters out dawn/dusk sensor noise,
	// pyranometer trickle, or heavily overcast conditions (< 500 W) that cannot meaningfully overcome inverter tare load
	// or deliver usable battery charging.
	minSignificantSolarKW = 0.5

	// minSurplusSolarForReserveKW (1.5 kW) is the minimum excess solar generation rate above home load
	// (solarKWH - loadKWH >= 1.5 kW * durationHours) required to rely on solar for reserve deficit restoration
	// rather than grid charging. At 1.5 kW net surplus, restoring a typical 15% deficit (e.g. 5% to 20% on a 13.5 kWh pack,
	// ~2 kWh) takes approximately 1.3 hours. If the threshold is set too low (e.g. sensor noise or trickle solar), the battery
	// will not meaningfully recharge, leaving the home exposed. If set too high, the system will unnecessarily import from
	// the grid despite solar already providing substantial recharging capability.
	minSurplusSolarForReserveKW = 1.5

	// socTargetTolerancePct is the tolerance (in % SOC) allowed when verifying whether a forward path reached its target SOC.
	// It matches socBucketResolutionPct (0.5%) so that trajectories ending at e.g. 99.6% due to bin quantization are not discarded.
	socTargetTolerancePct = 0.5

	// priceMaterialityThresholdDollars ($0.005/kWh, or 0.5¢/kWh) is the minimum price differential required for an economic
	// distinction to be meaningful to homeowners when generating decision explanations (e.g. waiting to charge vs charging now).
	// Differences below half a cent are typically floating-point noise or insignificant tariff riders.
	priceMaterialityThresholdDollars = 0.005

	// defaultBatteryCyclingHoldHurdleDollars ($0.01/kWh, or 1.0¢/kWh) is the marginal battery cycling
	// and degradation hurdle applied during forward search when daytime solar refill is projected.
	// Discharging the battery to save cheap off-peak grid imports is penalized by this amount to prevent
	// premature cell wear for negligible fractional-cent arbitrage.
	defaultBatteryCyclingHoldHurdleDollars = 0.01

	// minPeakRateSpreadDollars ($0.10/kWh, or 10¢/kWh) is the minimum required price spread across the planning horizon
	// before declaring an interval as "peak" in user explanations. This prevents minor off-peak variations or riders
	// (like 15¢ vs 8¢) from erroneously telling users they are in an expensive "peak rate" period when true TOU peak
	// pricing is typically 25¢–65¢/kWh.
	minPeakRateSpreadDollars = 0.10

	// priceEqualityToleranceDollars is the price tolerance ($/kWh) used when comparing tariff rates
	// to determine mathematical equality (e.g. fast-tracking step 0 charges), preventing 64-bit float
	// precision errors (such as $0.0800000001 vs $0.08) from failing equality checks.
	priceEqualityToleranceDollars = priceEpsilonForEquality

	// defaultBatteryPowerSmallKW is the nominal fallback charge/discharge inverter power (5.0 kW)
	// used when inverter telemetry does not report max charge rate for typical residential systems (< 20 kWh).
	defaultBatteryPowerSmallKW = 5.0

	// defaultBatteryPowerLargeKW is the nominal fallback charge/discharge inverter power (10.0 kW)
	// for larger dual-inverter or high-capacity residential systems (>= 20 kWh).
	defaultBatteryPowerLargeKW = 10.0

	// largeBatteryCapacityThresholdKWH is the capacity threshold (20 kWh) above which a system
	// is presumed to have multi-inverter capability supporting 10 kW charge/discharge.
	largeBatteryCapacityThresholdKWH = 20.0

	// minStartChargeCapacityHeadroomPct is the minimum empty capacity percentage (4%) required
	// to initiate a new grid charge session, preventing inverter short-cycling near full capacity.
	minStartChargeCapacityHeadroomPct = 0.04

	// minStartChargeFloorKWH is the absolute minimum empty energy buffer (0.6 kWh) required
	// to initiate a new grid charge session regardless of battery size or charging rate.
	minStartChargeFloorKWH = 0.6

	// reserveFloorTolerancePct is the numerical tolerance (0.75% SOC) used when evaluating if
	// the battery is resting at its emergency reserve floor.
	reserveFloorTolerancePct = 0.75

	// minReserveDeficitPenaltyDollarsPerKWH ($1.00/kWh) is the minimum penalty rate applied to energy deficits below the user's reserve.
	// It guarantees that the penalty strictly exceeds typical retail electricity rates and peak-shaving benefits, preventing the optimizer
	// from treating the homeowner's backup reserve as a cheap source of energy to borrow or delay recharging.
	minReserveDeficitPenaltyDollarsPerKWH = 1.00

	// reserveDeficitPenaltyMultiplier (3.0x) scales the deficit penalty with current electricity rates.
	// When import rates are high (e.g. $0.50/kWh during extreme events), a flat $1.00/kWh penalty might no longer sufficiently
	// discourage borrowing from reserve if peak arbitrage spreads are exceptionally large. Multiplying by 3x ensures the penalty
	// remains prohibitive relative to any single-cycle arbitrage gain.
	reserveDeficitPenaltyMultiplier = 3.0

	// peakSurvivalBufferShortfallPenaltyDollarsPerKWH ($0.02/kWh) is the soft tie-breaker penalty rate applied
	// to any shortfall below the peak survival safety buffer (reserve + bufferEnergyKWH) at peak window exit.
	peakSurvivalBufferShortfallPenaltyDollarsPerKWH = 0.02

	// minSignificantBatteryPowerKW (0.3 kW = 300 W) is the minimum battery charge or discharge power rate
	// required to be considered meaningful activity rather than inverter tare or idle losses.
	// When evaluated over an interval, this is scaled by durationHours (e.g. 100 Wh for 20 min).
	minSignificantBatteryPowerKW = 0.3

	// postHorizonPriceRollupTime is the duration used to blend prices beyond the planning horizon.
	postHorizonPriceRollupTime = 4 * time.Hour

	// abnormalRecentUsageThresholdKWH (1.0 kWh) is the minimum energy consumption excess above the 75th
	// percentile (Q3) baseline over the recent 1–2 hour evaluation window required to classify usage as abnormal.
	// A typical 13.5 kWh home battery reserve buffer is 10%–20% (1.35–2.7 kWh). Minor variations (< 1.0 kWh)
	// reflect normal appliance cycles (refrigerators, televisions, lighting), whereas an excess of 1.0 kWh or more
	// represents major, sustained discretionary energy draw (e.g. electric oven, stove, dryer, or car charging)
	// that materially depleted the homeowner's backup reserve.
	abnormalRecentUsageThresholdKWH = 1.0

	// almostFullSOC is the threshold below which a battery is considered full enough to not meaninginfully need
	// solar refill and also to avoid hold penalty
	almostFullSOC = 95.0
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

// calculateStartChargeHeadroomKWH calculates the minimum physical empty capacity (kWh)
// required to initiate a new grid-charging session from an idle or discharging state.
// This prevents short-cycling inverters for 2-3 minutes when the battery is already at 98-99% SOC,
// honors settings.MinStartChargeMinutes, and preserves headroom for rooftop solar.
func calculateStartChargeHeadroomKWH(capacityKWH, maxChargeKW float64, settings types.Settings) float64 {
	maxChargeKW = resolveBatteryPowerKW(maxChargeKW, capacityKWH)
	durationHours := float64(settings.MinStartChargeMinutes) / 60.0
	timeHeadroomKWH := maxChargeKW * durationHours
	pctHeadroomKWH := capacityKWH * minStartChargeCapacityHeadroomPct
	return max(minStartChargeFloorKWH, max(pctHeadroomKWH, timeHeadroomKWH))
}

// planInterval represents a discrete period in the planning horizon.
type planInterval struct {
	index         int
	startTime     time.Time
	endTime       time.Time
	durationHours float64
	price         types.Price
	importRate    float64 // Total delivered retail rate: supply + grid use/delivery fees ($/kWh)
	exportRate    float64 // Total compensation for exported energy to grid ($/kWh)
	loadKWH       float64 // Projected total home load energy for this interval (kWh)
	q3LoadKWH     float64 // 75th percentile home load energy for this interval (kWh)
	solarKWH      float64 // Projected total rooftop solar generation energy for this interval (kWh)
	minSOC        float64 // Active reserve limit for this interval (%)
}

func (pi planInterval) avgLoadKW() float64 {
	if pi.durationHours <= 0 {
		return 0.0
	}
	return pi.loadKWH / pi.durationHours
}

func (pi planInterval) avgSolarKW() float64 {
	if pi.durationHours <= 0 {
		return 0.0
	}
	return pi.solarKWH / pi.durationHours
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

// candidateLogData holds diagnostic attributes for an action candidate, avoiding heap
// allocation from closures in generateActionCandidates.
type candidateLogData struct {
	vppStart                time.Time
	vppEnd                  time.Time
	vppDeadline             time.Time
	vppMandatory            bool
	homeKW                  float64
	stepKW                  float64
	periodStart             time.Time
	periodEnd               time.Time
	headroomKWH             float64
	isAlreadyCharging       bool
	refillExportRate        float64
	effectiveReserveSOC     float64
	earliestSolarRefillTime time.Time
	higherFutureRate        float64
	vppEventDeadline        time.Time
	hasUpcomingSolarRefill  bool
	hasVPPAhead             bool
	vppBeforeSolarRefill    bool
	isAlreadyStandby        bool
	roundTripEff            float64
	rechargeCost            float64
	futureExportRate        float64
	futurePeakRate          float64
	minArbitrageDiff        float64
	minDeficitDiff          float64
	earliestArbitrageTime   time.Time
	vppRechargeDeadline     time.Time
	canDischarge            bool
	minAlternativeValue     float64
	cycleHurdle             float64
	exportReplacementCost   float64
}

// actionCandidate represents a permissible control action pair for an interval.
type actionCandidate struct {
	batteryMode        types.BatteryMode
	solarMode          types.SolarMode
	reason             types.ActionReason
	description        string
	actionName         PlanActionName
	targetSOC          int
	overrideReserveSOC float64
	isTruePeak         bool
	futurePrice        *types.Price
	logData            *candidateLogData
}

// precedingAction contains only the action state guaranteed to be known from
// the previous interval (both from lastAction at step 0 and from parent nodes during DP rollout).
type precedingAction struct {
	BatteryMode types.BatteryMode
	SolarMode   types.SolarMode
	ImportRate  float64
	ExportRate  float64
}

func (c *Controller) toPrecedingAction(action *types.Action, settings types.Settings) precedingAction {
	if action == nil {
		return precedingAction{}
	}
	var importRate, exportRate float64
	if action.CurrentPrice != nil {
		importRate = action.CurrentPrice.ImportRateDollars()
		exportRate = c.calculateExportCredit(*action.CurrentPrice, settings, nil)
	}
	return precedingAction{
		BatteryMode: action.BatteryMode,
		SolarMode:   action.SolarMode,
		ImportRate:  importRate,
		ExportRate:  exportRate,
	}
}

func toPrecedingAction(a actionCandidate, prevInterval planInterval) precedingAction {
	return precedingAction{
		BatteryMode: a.batteryMode,
		SolarMode:   a.solarMode,
		ImportRate:  prevInterval.importRate,
		ExportRate:  prevInterval.exportRate,
	}
}

// planLog formats and emits structured slog messages for action candidates, distinguishing whether
// the candidate was ultimately chosen for execution or evaluated but not chosen.
func planLog(ctx context.Context, selected bool, actionName PlanActionName, attrs ...slog.Attr) {
	msg := "optimal plan action: " + actionName
	if !selected {
		msg = "optimal plan candidate not chosen: " + actionName
	}
	allAttrs := make([]any, 0, len(attrs)+1)
	allAttrs = append(allAttrs, slog.Bool("selected", selected))
	for _, attr := range attrs {
		allAttrs = append(allAttrs, attr)
	}
	log.Ctx(ctx).DebugContext(ctx, msg, allAttrs...)
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
	solarToHomeKW            float64
	solarExportKW            float64
	solarToBatKW             float64
	solarCurtailedKW         float64
	batteryChargeKW          float64
	batteryDischargeKW       float64
	endingSOC                float64
}

// vppAnchor represents a single scheduled VPP event in the horizon.
type vppAnchor struct {
	eventStart time.Time
	eventEnd   time.Time
	deadline   time.Time // Target charging completion deadline (accounting for buffer)
	vppSoc     float64
	mandatory  bool
	price      float64
}

// peakWindowAnchor represents a contiguous peak pricing window within the planning horizon.
type peakWindowAnchor struct {
	startIndex      int
	endIndex        int
	startTime       time.Time
	endTime         time.Time
	minPeakRate     float64
	maxPeakRate     float64
	bufferEnergyKWH float64
}

// planningAnchors holds fixed operational markers detected across the horizon.
type planningAnchors struct {
	vppEvents            []vppAnchor
	knownPostHorizonRate float64 // Known rate at H+1 if tariff has a fixed weekly schedule
	minHorizonImportRate float64
	maxHorizonImportRate float64
	peakWindows          []peakWindowAnchor
}

// minDeficitPriceSpread returns the minimum price spread ($/kWh) required for an economic peak.
func minDeficitPriceSpread(settings types.Settings) float64 {
	return max(minPeakRateSpreadDollars, settings.MinDeficitPriceDifferenceDollarsPerKWH)
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

// isTruePeak returns true if the import rate is at or near the horizon peak rate
// and the horizon price spread exceeds minDeficitPriceSpread(settings).
func (a planningAnchors) isTruePeak(importRate float64, settings types.Settings) bool {
	return isTruePeakRate(importRate, a.minHorizonImportRate, a.maxHorizonImportRate, settings)
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
	actions           []actionCandidate
	states            []planState
	metrics           []intervalMetrics
	timeline          []planInterval
	totalCost         float64
	strikes           int
	initialCandidates []actionCandidate
	modeScores        map[types.BatteryMode]float64
	modeStrikes       map[types.BatteryMode]int
	bestScore         float64
}

// logCandidate emits structured slog messages for an action candidate, distinguishing whether
// the candidate was ultimately chosen for execution or evaluated but not chosen.
func logCandidate(ctx context.Context, cand actionCandidate, interval planInterval, state planState, selected bool) {
	if cand.actionName == "" {
		return
	}
	var ld candidateLogData
	if cand.logData != nil {
		ld = *cand.logData
	}
	switch cand.actionName {
	case PlanActionVPPTargetSOCReached, PlanActionVPPActiveEventDischarging:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Time("vppStart", ld.vppStart),
			slog.Time("vppEnd", ld.vppEnd),
			slog.Float64("currentSOC", state.soc),
			slog.Float64("vppTargetSOC", cand.overrideReserveSOC),
			slog.Bool("mandatory", ld.vppMandatory),
		)
	case PlanActionVPP2HourPrepEmergencyTopUp, PlanActionVPP2HourPrepStandbyLock, PlanActionVPP2HourPrepDirectSolarExport:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Time("vppDeadline", ld.vppDeadline),
			slog.Time("vppStart", ld.vppStart),
			slog.Float64("currentSOC", state.soc),
		)
	case PlanActionActiveEVChargingStandby:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("homeKW", ld.homeKW),
			slog.Float64("stepKW", ld.stepKW),
			slog.Time("periodStart", ld.periodStart),
			slog.Time("periodEnd", ld.periodEnd),
		)
	case PlanActionNegativeOrForceChargeThresholdCharging, PlanActionNegativeOrForceChargeThresholdStandby:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("importRate", interval.importRate),
			slog.Float64("currentSOC", state.soc),
			slog.Float64("headroomKWH", ld.headroomKWH),
			slog.Bool("isAlreadyCharging", ld.isAlreadyCharging),
		)
	case PlanActionDischargingBattery, PlanActionBatteryAtReserveStandby:
		if !selected {
			return
		}
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("refillExportRate", ld.refillExportRate),
			slog.Float64("currentSOC", state.soc),
			slog.Float64("effectiveReserveSOC", ld.effectiveReserveSOC),
		)
	case PlanActionBatteryStandby:
		if !selected {
			return
		}
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("refillExportRate", ld.refillExportRate),
			slog.Time("earliestSolarRefillTime", ld.earliestSolarRefillTime),
			slog.Float64("currentSOC", state.soc),
			slog.Float64("effectiveReserveSOC", ld.effectiveReserveSOC),
			slog.Float64("higherFutureRate", ld.higherFutureRate),
			slog.Time("vppEventDeadline", ld.vppEventDeadline),
			slog.Bool("hasUpcomingSolarRefill", ld.hasUpcomingSolarRefill),
			slog.Bool("hasVPPAhead", ld.hasVPPAhead),
			slog.Bool("vppBeforeSolarRefill", ld.vppBeforeSolarRefill),
			slog.Bool("isAlreadyStandby", ld.isAlreadyStandby),
			slog.String("reason", string(cand.reason)),
		)
	case PlanActionReserveTargetCharge:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("importRate", interval.importRate),
			slog.Float64("currentSOC", state.soc),
			slog.Float64("targetReserve", interval.minSOC),
		)
	case PlanActionGridArbitragePreCharge:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("importRate", interval.importRate),
			slog.String("reason", string(cand.reason)),
			slog.Float64("roundTripEff", ld.roundTripEff),
			slog.Float64("rechargeCost", ld.rechargeCost),
			slog.Float64("futureExportRate", ld.futureExportRate),
			slog.Float64("futurePeakRate", ld.futurePeakRate),
			slog.Float64("minArbitrageDiff", ld.minArbitrageDiff),
			slog.Float64("minDeficitDiff", ld.minDeficitDiff),
			slog.Time("earliestArbitrageTime", ld.earliestArbitrageTime),
			slog.Time("vppRechargeDeadline", ld.vppRechargeDeadline),
		)
	case PlanActionVPPPreChargeBeforeDeadline:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Time("vppDeadline", ld.vppDeadline),
			slog.Float64("importRate", interval.importRate),
			slog.Float64("roundTripEff", ld.roundTripEff),
			slog.Float64("rechargeCost", ld.rechargeCost),
			slog.Float64("futureExportRate", ld.futureExportRate),
			slog.Float64("futurePeakRate", ld.futurePeakRate),
			slog.Float64("minArbitrageDiff", ld.minArbitrageDiff),
			slog.Float64("minDeficitDiff", ld.minDeficitDiff),
			slog.Time("earliestArbitrageTime", ld.earliestArbitrageTime),
			slog.Time("vppRechargeDeadline", ld.vppRechargeDeadline),
		)
	case PlanActionDirectSolarExport:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.String("reason", string(types.ActionReasonDirectExport)),
			slog.Float64("exportRate", interval.exportRate),
			slog.Float64("importRate", interval.importRate),
			slog.Float64("solarKWH", interval.solarKWH),
			slog.Float64("currentSOC", state.soc),
			slog.Float64("effectiveReserveSOC", ld.effectiveReserveSOC),
			slog.Bool("canDischarge", ld.canDischarge),
			slog.Time("vppRechargeDeadline", ld.vppRechargeDeadline),
		)
	case PlanActionBatteryGridExportDump:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("exportRate", interval.exportRate),
			slog.Int("targetSOC", cand.targetSOC),
			slog.Float64("rechargeCost", ld.exportReplacementCost),
			slog.Float64("minAlternativeValue", ld.minAlternativeValue),
			slog.Float64("roundTripEff", ld.roundTripEff),
			slog.Float64("cycleHurdle", ld.cycleHurdle),
		)
	default:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.String("batteryMode", drModeString(cand.batteryMode)),
			slog.String("solarMode", solarModeString(cand.solarMode)),
			slog.String("reason", string(cand.reason)),
			slog.Float64("currentSOC", state.soc),
		)
	}
}

// executeLogs invokes diagnostic logging on candidate actions evaluated at step 0
// that were not chosen, as well as actions in the winning trajectory.
func (p *planPath) executeLogs(ctx context.Context) {
	if p == nil {
		return
	}
	timeline := p.timeline

	chosenAction := actionCandidate{}
	if len(p.actions) > 0 {
		chosenAction = p.actions[0]
	}

	// 1. Log candidates evaluated at step 0 that were not chosen
	if len(timeline) > 0 && len(p.states) > 0 {
		step0Interval := timeline[0]
		step0State := p.states[0]
		for _, cand := range p.initialCandidates {
			isChosen := cand.batteryMode == chosenAction.batteryMode &&
				cand.solarMode == chosenAction.solarMode &&
				cand.reason == chosenAction.reason &&
				(cand.targetSOC == chosenAction.targetSOC || (cand.batteryMode == types.BatteryModeChargeAny && cand.targetSOC <= 0))
			if isChosen {
				continue
			}
			logCandidate(ctx, cand, step0Interval, step0State, false)
			score, evaluated := p.modeScores[cand.batteryMode]
			candStrikes := 0
			if p.modeStrikes != nil {
				candStrikes = p.modeStrikes[cand.batteryMode]
			}
			var delta float64
			if evaluated {
				delta = score - p.bestScore
				if delta < 0 {
					delta = 0
				}
			}
			log.Ctx(ctx).DebugContext(ctx, "optimal plan candidate not chosen",
				slog.String("batteryMode", drModeString(cand.batteryMode)),
				slog.String("solarMode", solarModeString(cand.solarMode)),
				slog.String("reason", string(cand.reason)),
				slog.String("description", cand.description),
				slog.Int("targetSOC", cand.targetSOC),
				slog.Bool("evaluated", evaluated),
				slog.Int("candidateStrikes", candStrikes),
				slog.Int("winningStrikes", p.strikes),
				slog.Float64("candidateScoreDollars", score),
				slog.Float64("winningScoreDollars", p.bestScore),
				slog.Float64("costDeltaDollars", delta),
			)
		}
	}

	// 2. Log chosen actions in the winning trajectory, skipping subsequent consecutive duplicate actions
	for i, act := range p.actions {
		if i >= len(timeline) || i >= len(p.states) {
			break
		}
		if i > 0 {
			prev := p.actions[i-1]
			if act.batteryMode == prev.batteryMode &&
				act.solarMode == prev.solarMode &&
				act.reason == prev.reason &&
				act.actionName == prev.actionName &&
				act.targetSOC == prev.targetSOC {
				continue
			}
		}
		logCandidate(ctx, act, timeline[i], p.states[i], true)
	}
}

const (
	// priceHourlyVariationToleranceDollars ($0.01/kWh, or 1.0¢/kWh) is the price variance tolerance used when grouping
	// contiguous charging episodes and evaluating same-rate discharge windows under dynamic hourly tariffs (e.g., ComEd, Ameren).
	// Price movements within 1.0¢ represent typical overnight wholesale clearing noise and are economically unprofitable
	// to discharge against due to round-trip battery efficiency losses.
	priceHourlyVariationToleranceDollars = 0.01
)

func determineRefinedStandbyReason(chargeReason types.ActionReason, interval planInterval, timeline []planInterval, stepIdx int) (types.ActionReason, string) {
	if chargeReason == types.ActionReasonVPPPrep {
		return types.ActionReasonVPPPrep, "Preserving battery in standby ahead of VPP event."
	}
	for k := stepIdx + 1; k < len(timeline); k++ {
		if timeline[k].importRate > interval.importRate+priceHourlyVariationToleranceDollars {
			return types.ActionReasonDeficitSaveForPeak, "Preserving battery in standby for upcoming peak rates."
		}
	}
	return types.ActionReasonHoldSimilarPrice, "Preserving battery in standby."
}

// refineOverchargedEpisodes detects situations where the forward DP search selected a grid charge episode
// that overshot the energy needed for future higher-rate periods, causing the battery to bleed off the excess
// charge (via BatteryModeLoad) during the same flat-price window before rates increased.
//
// If refinement occurs and saves money without increasing cost, a new refined planPath is returned with true.
// If no adjustments are needed or if refinement is rejected, the original planPath is returned with false.
func (p *planPath) refineOverchargedEpisodes(ctx context.Context, settings types.Settings, roundTripEff float64) (*planPath, bool) {
	if p == nil || len(p.actions) == 0 || len(p.timeline) == 0 {
		return p, false
	}

	timeline := p.timeline
	actions := make([]actionCandidate, len(p.actions))
	copy(actions, p.actions)
	states := make([]planState, len(p.states))
	copy(states, p.states)
	metrics := make([]intervalMetrics, len(p.metrics))
	copy(metrics, p.metrics)

	adjusted := false
	originalTotalCost := p.totalCost

	i := 0
	for i < len(actions) {
		act := actions[i]
		// Look for a discretionary grid charge episode with actual grid charging.
		// Skip mandatory emergency top-up, negative/force-charge threshold charging, and negative import rates.
		if act.batteryMode != types.BatteryModeChargeAny ||
			act.actionName == PlanActionVPP2HourPrepEmergencyTopUp ||
			act.actionName == PlanActionNegativeOrForceChargeThresholdCharging ||
			timeline[i].importRate < 0 ||
			i >= len(metrics) || metrics[i].gridChargeKWH <= minSignificantBatteryPowerKW*timeline[i].durationHours {
			i++
			continue
		}

		chargeStartIdx := i
		chargeEndIdx := i
		chargeImportRate := timeline[i].importRate
		maxChargeImportRate := timeline[i].importRate
		chargeReason := act.reason

		// Find contiguous charge episode at the same rate or within hourly variation tolerance
		for chargeEndIdx+1 < len(actions) &&
			actions[chargeEndIdx+1].batteryMode == types.BatteryModeChargeAny &&
			actions[chargeEndIdx+1].actionName != PlanActionVPP2HourPrepEmergencyTopUp &&
			actions[chargeEndIdx+1].actionName != PlanActionNegativeOrForceChargeThresholdCharging &&
			timeline[chargeEndIdx+1].importRate >= 0 &&
			math.Abs(timeline[chargeEndIdx+1].importRate-chargeImportRate) <= priceHourlyVariationToleranceDollars {
			chargeEndIdx++
			if timeline[chargeEndIdx].importRate > maxChargeImportRate {
				maxChargeImportRate = timeline[chargeEndIdx].importRate
			}
		}

		// states[j+1] holds the battery state after interval j completes; guard against out-of-bounds
		// when reading the post-charge peak SOC if states is truncated or at the horizon boundary.
		if chargeEndIdx+1 >= len(states) {
			i = chargeEndIdx + 1
			continue
		}
		peakSOC := states[chargeEndIdx+1].soc

		// Look ahead within the same price window before rates increase or export/VPP opportunities occur
		windowEndIdx := chargeEndIdx
		hasSameRateDischarge := false
		for k := chargeEndIdx + 1; k < len(actions); k++ {
			// Stop if rate increases, export opportunity occurs, a VPP event begins, or a new grid charge episode begins
			if timeline[k].importRate > maxChargeImportRate+priceHourlyVariationToleranceDollars ||
				timeline[k].exportRate > maxChargeImportRate+priceHourlyVariationToleranceDollars ||
				actions[k].batteryMode == types.BatteryModeChargeAny ||
				actions[k].batteryMode == types.BatteryModeExport ||
				actions[k].reason == types.ActionReasonVPPActive {
				break
			}
			windowEndIdx = k
			// Check if there was discharge to home load without significant solar
			isDischarging := actions[k].batteryMode == types.BatteryModeLoad &&
				actions[k].reason != types.ActionReasonVPPActive &&
				k < len(metrics) && metrics[k].batSuppliedHomeKWH > minSignificantBatteryPowerKW*timeline[k].durationHours
			if isDischarging {
				hasSameRateDischarge = true
			}
		}

		// Guard against out-of-bounds when reading the exit SOC entering downstream periods at states[windowEndIdx+1].
		if windowEndIdx+1 >= len(states) {
			i = windowEndIdx + 1
			continue
		}

		exitSOC := states[windowEndIdx+1].soc
		// Churn is present if the battery discharged below peakSOC during the same price block
		if hasSameRateDischarge && exitSOC < peakSOC-socTargetTolerancePct {
			// Target SOC needed at the end of the charge episode is exitSOC.
			// Incorporate user profile reserve safety buffers and round up (Ceil)
			// to guarantee the battery never enters downstream periods in deficit.
			reserveBufferPct := settings.GetOptimizationParams().ReserveBufferPercent
			minReserve := timeline[chargeEndIdx].minSOC + reserveBufferPct
			targetSOCFloat := max(minReserve, exitSOC)
			targetSOCInt := int(math.Ceil(targetSOCFloat))
			if float64(targetSOCInt) < minReserve {
				targetSOCInt = int(math.Ceil(minReserve))
			}
			if targetSOCInt > 100 {
				targetSOCInt = 100
			}

			// 1. Cap charge intervals to targetSOCInt
			for j := chargeStartIdx; j <= chargeEndIdx; j++ {
				if actions[j].targetSOC == 0 {
					actions[j].targetSOC = targetSOCInt
				} else if actions[j].targetSOC != targetSOCInt {
					log.Ctx(ctx).DebugContext(ctx, "encountered non-zero targetSOC during overcharge refinement",
						slog.Time("intervalStart", timeline[j].startTime),
						slog.String("actionName", string(actions[j].actionName)),
						slog.Int("existingTargetSOC", actions[j].targetSOC),
						slog.Int("refinedTargetSOC", targetSOCInt),
					)
					// If existing target was higher than refined target, clamp it down; never raise an existing lower cap
					if actions[j].targetSOC > targetSOCInt {
						actions[j].targetSOC = targetSOCInt
					}
				}
			}

			// 2. Convert subsequent same-price load intervals (without solar) to Standby
			for j := chargeEndIdx + 1; j <= windowEndIdx; j++ {
				if actions[j].batteryMode == types.BatteryModeLoad &&
					actions[j].reason != types.ActionReasonVPPActive &&
					actions[j].solarMode != types.SolarModeExport &&
					timeline[j].solarKWH <= minSignificantSolarKW*timeline[j].durationHours {

					sbReason, sbDesc := determineRefinedStandbyReason(chargeReason, timeline[j], timeline, j)
					actions[j].batteryMode = types.BatteryModeStandby
					actions[j].reason = sbReason
					actions[j].description = sbDesc
					actions[j].actionName = PlanActionBatteryStandby
					actions[j].targetSOC = 0
				}
			}

			// 3. Re-simulate physics forward from chargeStartIdx to end of horizon to update all states and metrics
			for j := chargeStartIdx; j < len(actions) && j < len(timeline); j++ {
				// Note: states[j] is the STARTING state entering interval j (states[j+1] is the ending state).
				// If previous intervals already brought the battery to targetSOCInt before interval j begins,
				// switch interval j from ChargeAny to Standby. If interval j starts below targetSOCInt,
				// it remains ChargeAny and stepPhysics clamps the charge to targetSOCInt.
				if j > 0 && j <= chargeEndIdx && actions[j].batteryMode == types.BatteryModeChargeAny &&
					states[j].soc >= float64(targetSOCInt)-reserveFloorTolerancePct {
					sbReason, sbDesc := determineRefinedStandbyReason(chargeReason, timeline[j], timeline, j)
					actions[j].batteryMode = types.BatteryModeStandby
					actions[j].reason = sbReason
					actions[j].description = sbDesc
					actions[j].actionName = PlanActionBatteryStandby
					actions[j].targetSOC = 0
				}

				nextState, stepMetrics := stepPhysics(states[j], actions[j], timeline[j], settings, roundTripEff)
				metrics[j] = stepMetrics
				if j+1 < len(states) {
					states[j+1] = nextState
				}
			}

			adjusted = true
			log.Ctx(ctx).DebugContext(ctx, "refined overcharged grid charge episode",
				slog.Time("chargeStartTime", timeline[chargeStartIdx].startTime),
				slog.Time("windowEndTime", timeline[windowEndIdx].endTime),
				slog.Float64("chargeImportRate", chargeImportRate),
				slog.Float64("peakSOC", peakSOC),
				slog.Float64("exitSOC", exitSOC),
				slog.Float64("overchargeDeltaSOC", peakSOC-exitSOC),
				slog.Int("refinedTargetSOC", targetSOCInt),
			)
		}

		i = windowEndIdx + 1
	}

	if !adjusted {
		return p, false
	}

	var newTotalCost float64
	for _, m := range metrics {
		newTotalCost += m.costDollars
	}
	costSaved := originalTotalCost - newTotalCost

	// Reject refinement if the updated trajectory costs more than the original plan
	if costSaved < -priceEpsilonForEquality {
		log.Ctx(ctx).WarnContext(ctx, "rejecting overcharge refinement because refined cost exceeds original cost",
			slog.Float64("originalTotalCostDollars", originalTotalCost),
			slog.Float64("refinedTotalCostDollars", newTotalCost),
			slog.Float64("costIncreaseDollars", -costSaved),
		)
		return p, false
	}

	log.Ctx(ctx).DebugContext(ctx, "optimal plan overcharge refinement completed",
		slog.Float64("originalTotalCostDollars", originalTotalCost),
		slog.Float64("refinedTotalCostDollars", newTotalCost),
		slog.Float64("costSavedDollars", costSaved),
	)

	refinedPath := new(planPath)
	// copy p in case new fields are added automatically and then only overwrite fields we changed
	*refinedPath = *p
	refinedPath.actions = actions
	refinedPath.states = states
	refinedPath.metrics = metrics
	refinedPath.timeline = timeline
	refinedPath.totalCost = newTotalCost
	refinedPath.bestScore = p.bestScore - costSaved

	return refinedPath, true
}

// sanitizeLastAction filters out faults, paused actions, uninitialized modes, and stale actions (>90 minutes)
// so that invalid historical records do not influence plan candidate generation or inertia.
func sanitizeLastAction(lastAction *types.Action, refTime time.Time) *types.Action {
	if lastAction == nil {
		return nil
	}
	if lastAction.Fault || lastAction.Paused || lastAction.BatteryMode == types.BatteryModeNoChange {
		return nil
	}
	if !lastAction.Timestamp.IsZero() && !refTime.IsZero() && refTime.Sub(lastAction.Timestamp) > 90*time.Minute {
		return nil
	}
	return lastAction
}

// Plan computes the optimal battery and solar schedule across the planning horizon (16-24h)
// using a pruned decision tree search and physical round-trip conversion efficiency.
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

	// 2. Build the discrete timeline across the horizon (including price synthesis for midnight cutoffs)
	timeline, simParams, model, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, history, weather, settings, currentStatus)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to build planning timeline", slog.Any("error", err))
		return Decision{}, types.Plan{}, fmt.Errorf("buildPlanningTimeline: %w", err)
	}

	// 3. Scan timeline to detect fixed operational anchors (VPP deadlines and post-horizon replacement rate)
	anchors := c.detectPlanningAnchors(timeline, futurePrices, currentStatus, settings)

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

	// 5. Perform the pruned decision tree search across the timeline
	winningPath, err := c.searchOptimalPlan(ctx, timeline, initialState, anchors, settings, currentStatus, history, lastAction)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "optimal plan search failed", slog.Any("error", err))
		return Decision{}, types.Plan{}, fmt.Errorf("searchOptimalPlan: %w", err)
	}

	// 6. Finalize output: extract immediate Decision for hardware dispatch and full Plan for UI
	decision, plan := finalizeDecisionAndPlan(ctx, winningPath, timeline, currentStatus, currentPrice, now, settings, history, model)
	decision.SimulationParams = simParams
	decision.Action.SimulationParams = simParams

	winningPath.executeLogs(ctx)

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

// buildPlanningTimeline collates pricing periods and constructs discrete intervals
// across the available pricing horizon (up to 24h). Errors out if the available horizon is below minPlanningHorizonHours.
func (c *Controller) buildPlanningTimeline(
	ctx context.Context,
	now time.Time,
	nowPrice types.Price,
	futurePrices []types.Price,
	history []types.EnergyStats,
	weather []types.Weather,
	settings types.Settings,
	currentStatus types.SystemStatus,
) ([]planInterval, types.SimulationParams, map[int]TimeProfile, error) {
	// 1. Collate pricing records
	var allPrices []types.Price
	if !nowPrice.TSStart.IsZero() && nowPrice.TSEnd.After(now) {
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
		// If nowPrice is present, skip future prices that duplicate or overlap it at now
		if !nowPrice.TSStart.IsZero() && (fp.Contains(now) || fp.TSStart.Equal(nowPrice.TSStart)) {
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
	for hr, prof := range model {
		if prof.AvgSolarKWH > maxSolar {
			maxSolar = prof.AvgSolarKWH
			peakSolarHour = hr
		}
	}

	// 2. Convert into discrete intervals across the horizon
	// The horizon covers up to maxPlanningHorizonHours (24h).
	// Intervals that begin before the 24-hour mark are allowed to complete their full,
	// untruncated period without being artificially cut off at 24 hours.
	maxEnd := now.Add(time.Duration(maxPlanningHorizonHours) * time.Hour)

	var timeline []planInterval
	currentTime := now
	currentPrice := nowPrice
	idx := 0

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
		buffer := time.Duration(optParams.VPPChargingBufferMinutes) * time.Minute
		for _, ev := range currentStatus.VPPEvents {
			if ev.OptOut {
				continue
			}
			deadline := ev.TSStart.Add(-buffer)
			boundaries := []time.Time{deadline, ev.TSStart, ev.TSEnd}
			if ev.Mandatory {
				firmwareTakeover := ev.TSStart.Add(-vppStandbyLeadTime)
				mandatoryDeadline := firmwareTakeover.Add(-buffer)
				boundaries = append(boundaries, firmwareTakeover, mandatoryDeadline)
			}
			for _, bound := range boundaries {
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
		// a price boundary (currentPrice.TSEnd or any upcoming price start in allPrices) or any VPP event boundary.
		duration = stepEnd.Sub(currentTime)
		if duration < 10*time.Minute && !stepEnd.Equal(latestPriceTime) {
			nextBoundary := stepEnd.Add(periodIdealDuration)
			if nextBoundary.After(latestPriceTime) {
				nextBoundary = latestPriceTime
			}

			crossesBoundary := false
			if !currentPrice.TSEnd.IsZero() && nextBoundary.After(currentPrice.TSEnd) {
				crossesBoundary = true
			}

			if !crossesBoundary {
				for _, p := range allPrices {
					if p.TSStart.After(currentTime) && p.TSStart.Before(nextBoundary) {
						crossesBoundary = true
						break
					}
				}
			}

			if !crossesBoundary {
				buffer := time.Duration(optParams.VPPChargingBufferMinutes) * time.Minute
				for _, ev := range currentStatus.VPPEvents {
					if ev.OptOut {
						continue
					}
					deadline := ev.TSStart.Add(-buffer)
					vppBounds := []time.Time{deadline, ev.TSStart, ev.TSEnd}
					if ev.Mandatory {
						firmwareTakeover := ev.TSStart.Add(-vppStandbyLeadTime)
						mandatoryDeadline := firmwareTakeover.Add(-buffer)
						vppBounds = append(vppBounds, firmwareTakeover, mandatoryDeadline)
					}
					for _, b := range vppBounds {
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

			if !crossesBoundary && nextBoundary.Sub(currentTime) <= periodSplitThresholdDuration {
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

		if stepEnd.Sub(currentTime) < 30*time.Second {
			currentTime = stepEnd
			continue
		}

		durationHrs := stepEnd.Sub(currentTime).Hours()
		h := currentTime.Hour()
		profile := model[h]

		avgSolarKWH := profile.AvgSolarKWH
		if optParams.SolarCapacityBufferMinutes > 0 && len(model) > 0 && h <= peakSolarHour {
			shiftHours := optParams.SolarCapacityBufferMinutes / 60
			shiftMinutes := optParams.SolarCapacityBufferMinutes % 60

			baseHour := (h - shiftHours + 24) % 24
			prevHour := (baseHour - 1 + 24) % 24

			fraction := float64(shiftMinutes) / 60.0
			shiftedSolar := (1.0-fraction)*model[baseHour].AvgSolarKWH + fraction*model[prevHour].AvgSolarKWH

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
		// solar generation, use live generation power * durationHrs
		if idx == 0 && currentStatus.SolarKW > 0 {
			solarKWH = currentStatus.SolarKW * durationHrs
		}

		// Projected home load energy for this interval (kWh)
		loadKWH := profile.AvgHomeLoadKWH * durationHrs
		q3LoadKWH := profile.P75HomeLoadKWH * durationHrs
		if len(history) == 0 && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
			q3LoadKWH = currentStatus.HomeKW * durationHrs
		} else if loadKWH <= 0 && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
			q3LoadKWH = currentStatus.HomeKW * durationHrs
		}

		importRate := currentPrice.ImportRateDollars()
		exportRate := c.calculateExportCredit(currentPrice, settings, allPrices)
		for _, ev := range currentStatus.VPPEvents {
			if ev.OptOut {
				continue
			}
			price := ev.DollarsPerKWH
			// TODO: Find a better way to determine the export compensation price per VPP program (e.g. via utility API query or tariff schedule lookup)
			if price == 0 && !ev.Mandatory {
				price = 2.0
			}
			if price > 0 && !currentTime.Before(ev.TSStart) && currentTime.Before(ev.TSEnd) {
				exportRate = price
				break
			}
		}
		minSOC := settings.GetMinBatterySOC(ctx, currentTime, currentTime.Location(), currentPrice)

		interval := planInterval{
			index:         idx,
			startTime:     currentTime,
			endTime:       stepEnd,
			durationHours: durationHrs,
			price:         currentPrice,
			importRate:    importRate,
			exportRate:    exportRate,
			loadKWH:       loadKWH,
			q3LoadKWH:     q3LoadKWH,
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

// isFlatNetMetering checks if the utility rate structure is flat 1:1 net metering
// where surplus solar is banked against retail consumption rather than receiving TOU generation credits.
func isFlatNetMetering(opts types.UtilityRateOptions) bool {
	scheme := strings.ToLower(strings.TrimSpace(opts.NetMeteringScheme))
	return opts.NetMeteringCredits || scheme == "net" || scheme == "nem" || scheme == "nem1"
}

// calculateExportCredit determines the exact credit rate ($/kWh) for energy exported to the grid.
// Solar and battery exports are valued the same based on the utility tariff.
func (c *Controller) calculateExportCredit(price types.Price, settings types.Settings, allPrices []types.Price) float64 {
	// Flat net metering schemes (1:1 NEM, net, or nem1) bank at flat lowest/highest values
	if isFlatNetMetering(settings.UtilityRateOptions) {
		switch settings.SolarNetMeteringCreditsValue {
		case "highest":
			maxCost := price.DollarsPerKWH + price.GridUseDollarsPerKWH + price.GenerationAdjustmentDollarsPerKWH
			for _, p := range allPrices {
				cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH + p.GenerationAdjustmentDollarsPerKWH
				if cost > maxCost {
					maxCost = cost
				}
			}
			return maxCost
		case "none":
			return 0.0
		default:
			// "lowest" conservative default
			minCost := price.DollarsPerKWH + price.GridUseDollarsPerKWH + price.GenerationAdjustmentDollarsPerKWH
			for _, p := range allPrices {
				cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH + p.GenerationAdjustmentDollarsPerKWH
				if cost < minCost {
					minCost = cost
				}
			}
			return minCost
		}
	}
	return price.ExportRateDollars()
}

// detectPlanningAnchors scans the timeline and future prices to locate fixed operational markers
// (VPP deadlines and the post-horizon replacement rate).
func (c *Controller) detectPlanningAnchors(
	timeline []planInterval,
	futurePrices []types.Price,
	currentStatus types.SystemStatus,
	settings types.Settings,
) planningAnchors {
	var anchors planningAnchors

	if len(timeline) == 0 {
		return anchors
	}

	minImport, maxImport := calculateTimelinePriceRange(timeline)
	anchors.minHorizonImportRate = minImport
	anchors.maxHorizonImportRate = maxImport

	optParams := settings.GetOptimizationParams()

	horizonStart := timeline[0].startTime
	horizonEnd := timeline[len(timeline)-1].endTime

	// 1. Detect VPP Events in Horizon (extended by vppStandbyLeadTime so events starting right after horizon end are caught)
	vppScanEnd := horizonEnd.Add(vppStandbyLeadTime)
	buffer := time.Duration(optParams.VPPChargingBufferMinutes) * time.Minute
	for _, ev := range currentStatus.VPPEvents {
		if ev.OptOut {
			continue
		}
		price := ev.DollarsPerKWH
		// TODO: Find a better way to determine the export compensation price per VPP program (e.g. via utility API query or tariff schedule lookup)
		if price == 0 && !ev.Mandatory {
			price = 2.0
		}
		deadline := ev.TSStart.Add(-buffer)
		if ev.Mandatory {
			deadline = ev.TSStart.Add(-vppStandbyLeadTime).Add(-buffer)
		}
		// Event overlaps with horizon or its prep window overlaps with horizon
		if ev.TSEnd.After(horizonStart) && ev.TSStart.Before(vppScanEnd) {
			anchors.vppEvents = append(anchors.vppEvents, vppAnchor{
				eventStart: ev.TSStart,
				eventEnd:   ev.TSEnd,
				deadline:   deadline,
				vppSoc:     ev.VPPSoc,
				mandatory:  ev.Mandatory,
				price:      price,
			})
		}
	}

	// 2. Known post-horizon replacement rate:
	// Look at futurePrices for prices after horizonEnd.
	// To prevent plan flip-flopping across 20-minute re-runs, blend the next few hours (up to 4 hours)
	// post-horizon rather than anchoring to a single volatile interval.
	var postSum float64
	var postCount int
	for _, p := range futurePrices {
		if p.TSEnd.After(horizonEnd) && p.TSStart.Before(horizonEnd.Add(postHorizonPriceRollupTime)) {
			postSum += p.DollarsPerKWH + p.GridUseDollarsPerKWH
			postCount++
		}
	}
	if postCount > 0 {
		anchors.knownPostHorizonRate = postSum / float64(postCount)
	} else if len(timeline) > 0 {
		// Fallback when no post-horizon future prices exist (e.g. ComEd/Ameren before next day-ahead release).
		// TODO: Approximate post-horizon pricing using site pricing historical data or time-of-day profile
		// once site pricing data storage is optimized.
		anchors.knownPostHorizonRate = timeline[len(timeline)-1].importRate
	}

	// 3. Detect Peak Pricing Windows in Horizon:
	// A peak pricing window is a contiguous block of intervals where import rates are significantly
	// elevated (importRate >= minImport + minDeficitDiff) that contains at least one interval
	// reaching the horizon peak rate (importRate >= maxImport - priceMaterialityThresholdDollars).
	minDeficitDiff := minDeficitPriceSpread(settings)
	if maxImport >= minImport+minDeficitDiff && len(timeline) > 0 {
		bufferMinutes := optParams.PeakSurvivalBufferMinutes

		inPeak := false
		startIdx := 0
		var currentWindowMaxRate float64
		var currentWindowMinRate float64
		hasTruePeak := false

		for i, it := range timeline {
			isElevated := it.importRate >= minImport+minDeficitDiff

			if isElevated {
				if !inPeak {
					inPeak = true
					startIdx = i
					currentWindowMaxRate = it.importRate
					currentWindowMinRate = it.importRate
					hasTruePeak = isTruePeakRate(it.importRate, minImport, maxImport, settings)
				} else {
					if it.importRate > currentWindowMaxRate {
						currentWindowMaxRate = it.importRate
					}
					if it.importRate < currentWindowMinRate {
						currentWindowMinRate = it.importRate
					}
					if isTruePeakRate(it.importRate, minImport, maxImport, settings) {
						hasTruePeak = true
					}
				}
			}

			// End of contiguous block or end of timeline
			if inPeak && (!isElevated || i == len(timeline)-1) {
				endIdx := i
				if !isElevated {
					endIdx = i - 1
				}

				// If this block reaches the end of the timeline, check if the peak actually continues beyond horizon.
				// If future prices after horizonEnd remain elevated, the peak has not concluded within the horizon.
				isOngoingBeyondHorizon := false
				if endIdx == len(timeline)-1 && len(futurePrices) > 0 {
					for _, p := range futurePrices {
						if p.TSStart.Equal(horizonEnd) || (p.TSStart.Before(horizonEnd) && p.TSEnd.After(horizonEnd)) {
							rate := p.DollarsPerKWH + p.GridUseDollarsPerKWH
							if rate >= minImport+minDeficitDiff {
								isOngoingBeyondHorizon = true
							}
							break
						}
					}
				}

				if hasTruePeak && endIdx >= startIdx && !isOngoingBeyondHorizon {
					// Calculate required buffer energy (kWh) over bufferMinutes using greedy look-back
					// from endIdx across the peak window, matching controller.checkPeakSurvival.
					var bufferEnergyKWH float64
					if bufferMinutes > 0 {
						remainingMinutes := float64(bufferMinutes)
						for k := endIdx; k >= startIdx && remainingMinutes > 0; k-- {
							intervalMins := timeline[k].durationHours * 60.0
							takeMins := min(remainingMinutes, intervalMins)
							bufferEnergyKWH += (takeMins / intervalMins) * timeline[k].loadKWH
							remainingMinutes -= takeMins
						}
						if remainingMinutes > 0 {
							avgLoadKW := timeline[endIdx].loadKWH / timeline[endIdx].durationHours
							bufferEnergyKWH += avgLoadKW * (remainingMinutes / 60.0)
						}
					}

					anchors.peakWindows = append(anchors.peakWindows, peakWindowAnchor{
						startIndex:      startIdx,
						endIndex:        endIdx,
						startTime:       timeline[startIdx].startTime,
						endTime:         timeline[endIdx].endTime,
						minPeakRate:     currentWindowMinRate,
						maxPeakRate:     currentWindowMaxRate,
						bufferEnergyKWH: bufferEnergyKWH,
					})
				}

				inPeak = false
				hasTruePeak = false
				currentWindowMaxRate = 0
			}
		}
	}

	return anchors
}

// generateActionCandidates evaluates pruning rules at stepIdx and returns surviving candidate actions.
// prevAction represents the immediately preceding action: at stepIdx == 0, this is derived from the hardware's
// lastAction from the previous polling cycle; at stepIdx > 0, this is derived from the parent trajectory's action.
func (c *Controller) generateActionCandidates(
	ctx context.Context,
	stepIdx int,
	interval planInterval,
	timeline []planInterval,
	state planState,
	anchors planningAnchors,
	settings types.Settings,
	currentStatus types.SystemStatus,
	history []types.EnergyStats,
	prevAction precedingAction,
) []actionCandidate {
	currentSOC := state.soc

	capacityKWH := state.capacityKWH
	if capacityKWH <= 0 {
		capacityKWH = currentStatus.BatteryCapacityKWH
	}
	optParams := settings.GetOptimizationParams()
	roundTripEff := optParams.RoundTripEfficiency
	reserveBufferPct := optParams.ReserveBufferPercent
	effectiveReserveSOC := interval.minSOC + reserveBufferPct
	isAboveReserve := currentSOC > effectiveReserveSOC

	// Single struct allocated per generateActionCandidates call; shared across all candidate actions.
	ld := &candidateLogData{
		effectiveReserveSOC: effectiveReserveSOC,
		roundTripEff:        roundTripEff,
	}

	if anchors.maxHorizonImportRate <= 0 && len(timeline) > 0 {
		anchors.minHorizonImportRate, anchors.maxHorizonImportRate = calculateTimelinePriceRange(timeline)
	}
	isTruePeak := anchors.isTruePeak(interval.importRate, settings)

	// Determine default solar mode respecting user preference and export rates
	defaultSolarMode := types.SolarModeAny
	if !settings.GridExportSolar || interval.exportRate < 0 {
		defaultSolarMode = types.SolarModeNoExport
	}

	isFlatNEM := isFlatNetMetering(settings.UtilityRateOptions)

	// Determine if direct solar export is enabled. Flat net metering customers bank surplus 1:1 against consumption
	// and have no incentive to export solar while discharging battery for home load.
	// If the forecast predicts solar has ended or dropped below threshold, allow solar export to continue
	// as long as the preceding action was solar export and the export and import rates remain greater than or equal to preceding rates.
	hasSolar := interval.avgSolarKW() > minDirectSolarExportKW || (stepIdx == 0 && currentStatus.SolarKW > minDirectSolarExportKW)
	isSolarExportContinuation := prevAction.SolarMode == types.SolarModeExport &&
		prevAction.ExportRate > 0 &&
		interval.exportRate >= prevAction.ExportRate-priceEpsilonForEquality &&
		interval.importRate >= prevAction.ImportRate-priceEpsilonForEquality
	canDirectSolarExport := settings.ManageTOUSchedules && settings.GridExportSolar && (hasSolar || isSolarExportContinuation) && !isFlatNEM && interval.exportRate > 0

	effRT := roundTripEff
	if effRT <= 0 || effRT > 1.0 {
		effRT = 0.90
	}
	oneWayEff := math.Sqrt(effRT)

	// Compute grid charging headroom and allow a lower headroom if we're already charging so we don't
	// prematurely stop before the battery is full
	maxChargeKW := state.maxChargeKW
	if maxChargeKW <= 0 {
		maxChargeKW = currentStatus.MaxBatteryChargeKW
	}
	maxChargeKW = resolveBatteryPowerKW(maxChargeKW, capacityKWH)
	headroomKWH := capacityKWH - state.energyKWH
	isAlreadyCharging := prevAction.BatteryMode == types.BatteryModeChargeAny
	isAlreadyStandby := prevAction.BatteryMode == types.BatteryModeStandby
	ld.headroomKWH = headroomKWH
	ld.isAlreadyCharging = isAlreadyCharging

	requiredHeadroom := calculateStartChargeHeadroomKWH(capacityKWH, maxChargeKW, settings)
	if isAlreadyCharging {
		requiredHeadroom = continueChargeHeadroomKWH
	}
	gridChargeAllowed := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled && !state.chargingDisabled && !currentStatus.GridUnavailable
	canGridCharge := gridChargeAllowed && headroomKWH >= requiredHeadroom

	// Identify the nearest upcoming VPP event whose deadline has not yet passed.
	var nearestVPP *vppAnchor
	var nearestMandatoryVPP *vppAnchor
	for i := range anchors.vppEvents {
		vpp := &anchors.vppEvents[i]
		if interval.startTime.Before(vpp.deadline) {
			if nearestVPP == nil || vpp.deadline.Before(nearestVPP.deadline) {
				nearestVPP = vpp
			}
			if vpp.mandatory && (nearestMandatoryVPP == nil || vpp.deadline.Before(nearestMandatoryVPP.deadline)) {
				nearestMandatoryVPP = vpp
			}
		}
	}

	// Compute required pre-VPP recharge buffer time.
	// If an export or discharge ends within this buffer before a VPP deadline,
	// the battery cannot physically recharge to 100% in time for the event.
	// We account for current headroom plus the maximum discharge possible in this step,
	// rather than assuming the battery must recharge all the way from reserve.
	maxDischargeKW := resolveBatteryPowerKW(state.maxDischargeKW, capacityKWH)
	dHours := interval.durationHours
	if dHours <= 0 {
		dHours = 1.0
	}
	rechargeKWHNeeded := headroomKWH + maxDischargeKW*dHours
	maxReserveKWH := capacityKWH * (100.0 - effectiveReserveSOC) / 100.0
	if rechargeKWHNeeded > maxReserveKWH {
		rechargeKWHNeeded = maxReserveKWH
	}
	if rechargeKWHNeeded <= 0 {
		rechargeKWHNeeded = capacityKWH * 0.10
	}
	eff := oneWayEff
	if eff <= 0 {
		eff = 0.95
	}
	rechargeHoursNeeded := rechargeKWHNeeded / (maxChargeKW * eff)
	// Physical recharge time required to replenish the battery back to 100% capacity.
	// The safety buffer ahead of the VPP event/standby is already accounted for in vpp.deadline
	// via settings.VPPChargingBufferMinutes (and vppStandbyLeadTime for mandatory events).
	rechargeDuration := time.Duration(math.Ceil(rechargeHoursNeeded*60)) * time.Minute

	var vppRechargeDeadline time.Time
	if nearestMandatoryVPP != nil {
		vppRechargeDeadline = nearestMandatoryVPP.deadline.Add(-rechargeDuration)
	}
	ld.vppRechargeDeadline = vppRechargeDeadline
	beforeVPPRechargeDeadline := nearestMandatoryVPP == nil || !interval.endTime.After(vppRechargeDeadline)

	var nearestVPPRechargeDeadline time.Time
	if nearestVPP != nil {
		nearestVPPRechargeDeadline = nearestVPP.deadline.Add(-rechargeDuration)
	}

	var candidates []actionCandidate

	// Grid Outage / Unavailable Safety Guard:
	// When utility grid power is out, the battery must operate in off-grid backup mode (BatteryModeLoad)
	// to keep home circuits energized. All grid-dependent modes (charging, standby pass-through, grid export) are prohibited.
	if currentStatus.GridUnavailable {
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonGridUnavailable,
			description: "Grid unavailable. Discharging battery to power home backup circuits.",
			isTruePeak:  isTruePeak,
			logData:     ld,
		})
		return candidates
	}

	isNegativeOrFreePrice := interval.importRate <= 0.0
	isForceChargeThreshold := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled &&
		settings.AlwaysChargeUnderDollarsPerKWH != 0 && interval.importRate <= settings.AlwaysChargeUnderDollarsPerKWH

	// --- PRUNING RULE 1: VPP Event Active Window ---
	// Contracted utility VPP discharge takes strict precedence over retail pricing (even during negative/free import rates).
	for _, vpp := range anchors.vppEvents {
		if !interval.startTime.Before(vpp.eventStart) && interval.startTime.Before(vpp.eventEnd) {
			ld.vppStart = vpp.eventStart
			ld.vppEnd = vpp.eventEnd
			ld.vppMandatory = vpp.mandatory

			targetSOC := vpp.vppSoc
			if !vpp.mandatory {
				// Voluntary VPP: homeowner earns export incentives down to their configured reserve floor,
				// but never discharges below their effective reserve.
				targetSOC = effectiveReserveSOC
			}
			// For mandatory VPP, we cannot control vppSoc. The utility controls dispatch and the battery
			// will drain to vppSoc (e.g. 5%) regardless of what we command or configure, even if the user's
			// reserve is set higher (e.g. 20%).
			if currentSOC <= targetSOC {
				// Target reached: lock battery in standby to protect contracted reserve
				desc := "VPP Target SOC reached. Preserving reserve in standby."
				if vpp.mandatory {
					desc = "Mandatory VPP Target SOC reached. Preserving reserve in standby."
				}
				candidates = append(candidates, actionCandidate{
					batteryMode:        types.BatteryModeStandby,
					solarMode:          defaultSolarMode,
					reason:             types.ActionReasonVPPActive,
					description:        desc,
					actionName:         PlanActionVPPTargetSOCReached,
					overrideReserveSOC: targetSOC,
					isTruePeak:         isTruePeak,
					logData:            ld,
				})
				return candidates
			}

			batMode := types.BatteryModeLoad
			if (settings.ManageTOUSchedules && settings.GridExportBatteries) || vpp.price > 0 {
				batMode = types.BatteryModeExport
			}
			desc := "VPP Event Active."
			if vpp.mandatory {
				desc = "Mandatory VPP Event Active."
			}
			candidates = append(candidates, actionCandidate{
				batteryMode:        batMode,
				solarMode:          defaultSolarMode,
				reason:             types.ActionReasonVPPActive,
				description:        desc,
				actionName:         PlanActionVPPActiveEventDischarging,
				targetSOC:          int(math.Round(targetSOC)),
				overrideReserveSOC: targetSOC,
				isTruePeak:         isTruePeak,
				logData:            ld,
			})
			return candidates
		}
	}

	// --- PRUNING RULE 2: VPP Standby Lead-Time Window (T-2h to T for Mandatory VPP) ---
	// Conserves battery for the scheduled mandatory VPP discharge event.
	// Once the deadline period begins (T-2h), ESS firmware and utility dispatchers take autonomous control.
	// If the battery is undercharged (SOC < vppMinFullCapacitySOC), the hardware will force-charge
	// immediately to 100% regardless of electricity tariff rates. Standby is withheld when undercharged
	// so the planner accurately models true physical hardware behavior rather than proposing unrealistic
	// idle paths during expensive rates. Once approximately full, the battery locks into standby until the event starts.
	for _, vpp := range anchors.vppEvents {
		if !vpp.mandatory {
			continue
		}
		firmwareTakeover := vpp.eventStart.Add(-vppStandbyLeadTime)
		if !interval.startTime.Before(firmwareTakeover) && interval.startTime.Before(vpp.eventStart) {
			ld.vppDeadline = vpp.deadline
			ld.vppStart = vpp.eventStart

			isApproximatelyFull := currentSOC >= vppMinFullCapacitySOC
			if canGridCharge && !isApproximatelyFull {
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   defaultSolarMode,
					reason:      types.ActionReasonVPPPrep,
					description: "VPP Prep emergency top-up.",
					actionName:  PlanActionVPP2HourPrepEmergencyTopUp,
					targetSOC:   100,
					isTruePeak:  isTruePeak,
					logData:     ld,
				})
				return candidates
			}

			// When approximately full (or if grid charging is physically unavailable), enforce standby lock
			// until the VPP event starts.
			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeStandby,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonVPPPrep,
				description: "VPP Standby Lock.",
				actionName:  PlanActionVPP2HourPrepStandbyLock,
				isTruePeak:  isTruePeak,
				logData:     ld,
			})
			if canDirectSolarExport && isAboveReserve {
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeStandby,
					solarMode:   types.SolarModeExport,
					reason:      types.ActionReasonDirectExport,
					description: "Direct Solar Export during VPP prep.",
					actionName:  PlanActionVPP2HourPrepDirectSolarExport,
					isTruePeak:  isTruePeak,
					logData:     ld,
				})
			}
			return candidates
		}
	}

	// --- PRUNING RULE 3: Active EV Charging Detection ---
	// Prevent home battery from discharging into the vehicle during active EV charging sessions.
	// NOTE ON VPP PRECEDENCE:
	// Contractual utility VPP dispatch and pre-VPP charging (Rules 1 & 2) take strict precedence
	// over EV charging. If an upcoming VPP deadline requires charging the battery to 100%, that takes
	// priority; homeowner EV charging during a VPP window is the user's responsibility.
	//
	// IMPORTANT ARCHITECTURAL DECISION:
	// We ONLY force Standby in real-time (stepIdx == 0) when an active charging session is observed
	// (via detectEVCharging) within a configured EV charging window.
	// We do NOT force Standby across the entire window or for future timeline steps (stepIdx > 0),
	// because users do NOT charge their electric vehicles every single night.
	// Unconditionally forcing Standby on nights when the vehicle is not plugged in would needlessly
	// starve normal household baseline self-consumption and waste battery capacity.
	// Until reliable predictive models can forecast exactly WHICH nights an EV will charge, forward
	// planning models baseline consumption, and real-time telemetry overrides to Standby when charging begins.
	if stepIdx == 0 && len(settings.EVChargingPeriods) > 0 {
		for _, period := range settings.EVChargingPeriods {
			if inPeriod, _, _ := period.Contains(interval.startTime); inPeriod {
				isEV, stepKW := detectEVCharging(ctx, currentStatus.HomeKW, history)
				if isEV {
					ld.homeKW = currentStatus.HomeKW
					ld.stepKW = stepKW
					ld.periodStart = period.Start
					ld.periodEnd = period.End

					candidates = append(candidates, actionCandidate{
						batteryMode: types.BatteryModeStandby,
						solarMode:   defaultSolarMode,
						reason:      types.ActionReasonEVChargingStandby,
						description: "EV Charging Active. Standby to protect battery.",
						actionName:  PlanActionActiveEVChargingStandby,
						isTruePeak:  isTruePeak,
						logData:     ld,
					})
					return candidates
				}
			}
		}
	}

	// --- PRUNING RULE 4: Negative / Free Delivered Price or AlwaysChargeThreshold ---
	// If retail electricity is free or negative-cost, never discharge battery.
	// Power the home from the grid in Standby; charge battery if hardware permits.
	if isNegativeOrFreePrice || isForceChargeThreshold {
		canChargeNegative := gridChargeAllowed && headroomKWH >= continueChargeHeadroomKWH
		if canChargeNegative {
			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeChargeAny,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonAlwaysChargeBelowThreshold,
				description: "Price below threshold. Charging battery from grid.",
				actionName:  PlanActionNegativeOrForceChargeThresholdCharging,
				targetSOC:   100,
				isTruePeak:  isTruePeak,
				logData:     ld,
			})
			// If user configured an explicit AlwaysChargeUnder threshold and battery has headroom,
			// enforce charging as the designated action.
			if isForceChargeThreshold {
				return candidates
			}
		}

		// Standby preserves battery and allows home to consume free/cheap grid power
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonAlwaysChargeBelowThreshold,
			description: "Price below threshold. Preserving battery in standby; home powered from grid.",
			actionName:  PlanActionNegativeOrForceChargeThresholdStandby,
			isTruePeak:  isTruePeak,
			logData:     ld,
		})
		return candidates
	}

	// --- PRUNING RULE 5: Reserve Floor Protection ---
	// Battery grid export requires an export safety margin above the effective reserve floor.
	canExport := currentSOC > effectiveReserveSOC+exportReserveMarginPct
	roomLeftToFill := currentSOC < almostFullSOC

	// Evaluate daytime solar refill:
	// If daytime solar is expected and tonight's import rate is within minHoldDiff of export credit:
	// Discharging tonight to save cheap grid power when solar will refill tomorrow causes unnecessary cycling.
	var hasUpcomingSolarRefill bool
	var refillExportRate float64
	var earliestSolarRefillTime time.Time
	if isAboveReserve && roomLeftToFill && settings.GridExportSolar && !isFlatNEM && defaultBatteryCyclingHoldHurdleDollars > 0 {
		for i := stepIdx + 1; i < len(timeline); i++ {
			if timeline[i].solarKWH > minSignificantSolarKW*timeline[i].durationHours &&
				interval.importRate+defaultBatteryCyclingHoldHurdleDollars <= timeline[i].exportRate+priceEpsilonForEquality {
				hasUpcomingSolarRefill = true
				refillExportRate = timeline[i].exportRate
				earliestSolarRefillTime = timeline[i].startTime
				break
			}
		}
	}

	hasVPPAhead := nearestVPP != nil
	var vppEventDeadline time.Time
	if nearestVPP != nil {
		vppEventDeadline = nearestVPP.deadline
	}
	vppBeforeSolarRefill := hasVPPAhead && (!hasUpcomingSolarRefill || !earliestSolarRefillTime.Before(nearestVPP.deadline))

	ld.hasUpcomingSolarRefill = hasUpcomingSolarRefill
	ld.refillExportRate = refillExportRate
	ld.earliestSolarRefillTime = earliestSolarRefillTime
	ld.hasVPPAhead = hasVPPAhead
	ld.vppEventDeadline = vppEventDeadline
	ld.vppBeforeSolarRefill = vppBeforeSolarRefill

	// Self-consumption respects effective reserve floor
	canDischarge := isAboveReserve
	ld.canDischarge = canDischarge

	// Build permissible candidate branches:
	// Branch A: Standard Self-Consumption (BatteryModeLoad)
	if canDischarge {
		reason := types.ActionReasonSufficientBattery
		desc := "Discharging battery to cover household load."
		if isTruePeak {
			reason = types.ActionReasonDischargeAtPeak
			desc = "Discharging battery to power home during peak rate."
		}
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   defaultSolarMode,
			reason:      reason,
			description: desc,
			actionName:  PlanActionDischargingBattery,
			isTruePeak:  isTruePeak,
			logData:     ld,
		})
	} else if !isAboveReserve {
		// At or below reserve: battery hardware protects reserve; passthrough mode
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonBatteryAtReserve,
			description: "Battery at reserve. Home powered from grid/solar.",
			actionName:  PlanActionBatteryAtReserveStandby,
			isTruePeak:  isTruePeak,
			logData:     ld,
		})
	}

	// Branch B: Standby (Preserve battery charge for upcoming peak rates, solar export hold, or export arbitrage)
	// Pruned immediately if current importRate >= maxFutureRate with no upcoming VPP or export opportunity,
	// because there is never an economic incentive to hold battery energy during the highest rate in the horizon.
	var maxFutureRate float64
	var maxFutureExportRate float64
	cumNetSolarKWH := 0.0
	headroomKWH = max(0.0, capacityKWH-state.energyKWH)
	hasSolarRefillBeforePeak := false
	for i := stepIdx + 1; i < len(timeline); i++ {
		// If surplus solar before interval i is sufficient to refill the battery to capacity,
		// the battery will enter interval i full from rooftop solar. Standing by now cannot increase
		// energy available at interval i and would needlessly force grid imports now.
		if headroomKWH > 0 && cumNetSolarKWH >= headroomKWH {
			hasSolarRefillBeforePeak = true
			break
		}
		if timeline[i].importRate > maxFutureRate {
			maxFutureRate = timeline[i].importRate
		}
		if timeline[i].exportRate > maxFutureExportRate {
			maxFutureExportRate = timeline[i].exportRate
		}
		netSolar := max(0.0, timeline[i].solarKWH-timeline[i].loadKWH)
		cumNetSolarKWH += netSolar
	}
	effectiveMaxFutureRate := maxFutureRate
	if !hasSolarRefillBeforePeak {
		effectiveMaxFutureRate = max(maxFutureRate, anchors.knownPostHorizonRate)
	}
	// hasHigherFutureRate requires canDischarge because Standby only holds existing charge.
	// If the battery is already at or below reserve floor, holding it in Standby cannot save energy
	// for future peak rates (hardware won't allow discharging below reserve anyway) and would
	// needlessly lock the site into buying grid power while resting at reserve.
	hasHigherFutureRate := canDischarge && effectiveMaxFutureRate > interval.importRate+priceMaterialityThresholdDollars
	// hasHigherFutureExport requires canExport because Standby only holds existing charge (without adding energy).
	// If the battery does not currently have exportable energy above the reserve margin, holding it in Standby
	// would needlessly prevent home self-consumption for an export opportunity it cannot participate in.
	hasHigherFutureExport := canExport && settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM &&
		maxFutureExportRate > interval.importRate+priceMaterialityThresholdDollars
	ld.higherFutureRate = effectiveMaxFutureRate
	ld.isAlreadyStandby = isAlreadyStandby

	// Standby Pruning & Mode Maintenance:

	// We hypothesized that whenever `interval.importRate >= maxFutureRate` and there is no upcoming VPP event,
	// export opportunity, or daytime solar refill, Standby could always be pruned immediately, reasoning that
	// holding energy during the highest rate in the horizon is never economically justified.
	//
	// However:
	// 1. Flat Pricing Schedules: In flat-rate environments, all intervals have identical rates (`importRate == maxFutureRate`).
	//    Unconditionally pruning Standby removes the ability to stay in standby mode when already in standby
	//    (e.g., resting at reserve floor or preserving state), causing mode flapping and failing PrecedingStandby_MaintainsStandbyCandidate.
	// 2. Horizon Boundary Effects: At the final interval (`stepIdx == len(timeline)-1`), `maxFutureRate` is 0.0 because no
	//    future intervals remain. Unconditionally pruning Standby here forces any path that held charge across earlier intervals
	//    (such as night hold before daytime solar refill in SolarRefillHold_MinExportHoldDiff_PrunesSelfConsumptionTonight)
	//    to transition into BatteryModeLoad at the very last interval. That forced boundary transition incurs a modeSwitchPenalty ($0.01),
	//    which backpropagates through the DP search and prematurely discards the optimal holding trajectory at step 0.
	//
	// Standby MUST be pruned during genuine peak windows (`isTruePeak`, where import rate is at the horizon maximum
	// and price spread exceeds minPeakRateSpreadDollars) even if `isAlreadyStandby` is true, because idling during expensive
	// peak rates forces peak grid purchases. However, when `!isTruePeak` (flat rates, off-peak, horizon end), maintaining
	// standby (`canMaintainStandby := isAlreadyStandby && !isTruePeak`) prevents mode churn and horizon truncation artifacts.
	canMaintainStandby := isAlreadyStandby && !isTruePeak
	shouldOfferStandby := len(timeline) == 0 || hasHigherFutureRate || hasHigherFutureExport || hasUpcomingSolarRefill || hasVPPAhead || canMaintainStandby
	if shouldOfferStandby {
		reason := types.ActionReasonDeficitSaveForPeak
		desc := "Preserving battery in standby for upcoming peak rates."
		if vppBeforeSolarRefill {
			reason = types.ActionReasonVPPPrep
			desc = "Preserving battery in standby ahead of VPP event."
		} else if hasHigherFutureRate {
			reason = types.ActionReasonDeficitSaveForPeak
			desc = "Preserving battery in standby for upcoming peak rates."
		} else if hasUpcomingSolarRefill {
			reason = types.ActionReasonHoldSimilarPrice
			desc = "Preserving battery in standby for daytime solar export."
		} else if hasHigherFutureExport {
			reason = types.ActionReasonDeficitSaveForPeak
			desc = "Preserving battery in standby for upcoming export arbitrage."
		} else if hasVPPAhead {
			reason = types.ActionReasonVPPPrep
			desc = "Preserving battery in standby ahead of VPP event."
		} else if canMaintainStandby {
			reason = types.ActionReasonHoldSimilarPrice
			desc = "Maintaining standby mode."
		}
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   defaultSolarMode,
			reason:      reason,
			description: desc,
			actionName:  PlanActionBatteryStandby,
			isTruePeak:  isTruePeak,
			logData:     ld,
		})
	}

	// Branch C: Grid Charging (Off-peak pre-charge)
	if canGridCharge {
		if len(timeline) == 0 {
			// Minimal / single-step test fallback without timeline
			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeChargeAny,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonDeficitChargeNow,
				description: "Pre-charging battery from grid.",
				targetSOC:   100,
				isTruePeak:  isTruePeak,
				logData:     ld,
			})
		} else {
			// Grid Charge Arbitrage (Option B):
			// If there is any upcoming interval where the rate (import or export) clears the recharge hurdle
			// (accounting for round-trip efficiency and minimum price difference), offer ChargeAny.
			// The forward DP solver explores all charge trajectories, and finalizeDecisionAndPlan
			// dynamically resolves targetSOC to the peak SOC reached by the winning path.
			var hasArbitrageAhead bool
			chargeReason := types.ActionReasonDeficitChargeNow
			chargeDesc := ""
			var earliestArbitrageTime time.Time
			var futureExportRate float64
			var futurePeakRate float64

			minDeficitDiff := max(priceEpsilonForEquality, settings.MinDeficitPriceDifferenceDollarsPerKWH)
			minArbitrageDiff := max(priceEpsilonForEquality, settings.MinArbitrageDifferenceDollarsPerKWH)
			rechargeCost := interval.importRate
			if roundTripEff > 0 && roundTripEff <= 1.0 {
				rechargeCost = interval.importRate / roundTripEff
			}

			for i := stepIdx + 1; i < len(timeline); i++ {
				// For mandatory VPP events, firmware takes autonomous control at T-2h and forces charging
				// regardless of cost if not full. Any retail arbitrage must complete before vppRechargeDeadline.
				// For optional (voluntary) VPP events, there is no firmware takeover. Furthermore, voluntary
				// VPP export compensation ($2.00/kWh by default) is far higher than ordinary retail export rates
				// (typically $0.05–$0.25/kWh), so the forward DP search will naturally preserve and charge battery
				// energy for the VPP event rather than dumping it for lower retail export credits.
				canCompleteBeforeVPPRecharge := nearestMandatoryVPP == nil || !timeline[i].endTime.After(vppRechargeDeadline)
				// 1. Direct battery export arbitrage: charging now to export stored battery energy to the grid at high export rates.
				// Evaluated post-losses (rechargeCost = importRate / roundTripEff) because energy passes through the battery
				// electrochemically; round-trip conversion losses must be overcome to guarantee the homeowner achieves the
				// configured minimum arbitrage profit margin above inverter losses and battery cell degradation.
				// Note: canExport is intentionally not checked here because current SOC may be low/empty right now;
				// the purpose of pre-charging is to add energy so that canExport becomes true when reaching step i.
				canBatteryExportAhead := settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM && canCompleteBeforeVPPRecharge
				if canBatteryExportAhead && timeline[i].exportRate-rechargeCost >= minArbitrageDiff {
					futureExportRate = timeline[i].exportRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonArbitrageChargeExport
					chargeDesc = "Pre-charging for upcoming battery export arbitrage."
					earliestArbitrageTime = timeline[i].startTime
					break
				}

				// 2. Solar export credit arbitrage: charging now enables daytime rooftop solar export at high export rates.
				// Evaluated on nominal price spread (pre-losses) because charging from the grid displaces solar energy that would
				// have recharged the battery anyway. In both cases, the battery is cycled once, and the surplus solar exports
				// directly through the inverter without electrochemical storage losses. Applying a round-trip efficiency
				// penalty here would double-count battery losses and be overly conservative.
				canSolarExportAhead := canCompleteBeforeVPPRecharge && settings.GridExportSolar && !isFlatNEM &&
					timeline[i].solarKWH > minSignificantSolarKW*timeline[i].durationHours &&
					timeline[i].exportRate-interval.importRate >= minArbitrageDiff
				if canSolarExportAhead {
					futureExportRate = timeline[i].exportRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonArbitrageChargeExport
					chargeDesc = "Pre-charging for upcoming solar export arbitrage."
					earliestArbitrageTime = timeline[i].startTime
					break
				}

				// 3. Peak import rate arbitrage: charging now avoids buying expensive peak grid power later
				if timeline[i].importRate-rechargeCost >= minDeficitDiff {
					futurePeakRate = timeline[i].importRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonDeficitChargeNow
					chargeDesc = "Pre-charging for upcoming peak rates."
					earliestArbitrageTime = timeline[i].startTime
					break
				}
			}

			if !hasArbitrageAhead && anchors.knownPostHorizonRate > 0 && anchors.knownPostHorizonRate-rechargeCost >= minDeficitDiff {
				futurePeakRate = anchors.knownPostHorizonRate
				hasArbitrageAhead = true
				chargeReason = types.ActionReasonDeficitChargeNow
				chargeDesc = "Pre-charging for upcoming post-horizon peak rates."
				if len(timeline) > 0 {
					earliestArbitrageTime = timeline[len(timeline)-1].endTime
				}
			}

			// When both an upcoming VPP event and an upcoming arbitrage/peak rate exist, consolidate
			// into a single BatteryModeChargeAny candidate to prevent redundant state-space expansion.
			// Target reserve recharge: if currentSOC is below the interval's target reserve (e.g. during
			// a scheduled off-peak charging window), offer ChargeAny to bring battery back up to the
			// configured reserve.
			hasReserveCharge := currentSOC < interval.minSOC-reserveFloorTolerancePct
			if hasArbitrageAhead || hasVPPAhead || hasReserveCharge {
				selectedReason := types.ActionReasonVPPPrep
				selectedDesc := "VPP Pre-charging before deadline."
				selectedTargetSOC := 0

				if hasReserveCharge {
					selectedReason = types.ActionReasonDeficitChargeNow
					selectedDesc = "Charging battery to target reserve."
					if !hasArbitrageAhead && !hasVPPAhead {
						selectedTargetSOC = int(math.Round(interval.minSOC))
					}
				} else if hasArbitrageAhead && (!hasVPPAhead || earliestArbitrageTime.Before(nearestVPPRechargeDeadline)) {
					selectedReason = chargeReason
					selectedDesc = chargeDesc
				}

				var actionName PlanActionName
				if hasReserveCharge {
					actionName = PlanActionReserveTargetCharge
				} else if selectedReason == chargeReason {
					actionName = PlanActionGridArbitragePreCharge
					ld.rechargeCost = rechargeCost
					ld.futureExportRate = futureExportRate
					ld.futurePeakRate = futurePeakRate
					ld.minArbitrageDiff = minArbitrageDiff
					ld.minDeficitDiff = minDeficitDiff
					ld.earliestArbitrageTime = earliestArbitrageTime
				} else {
					actionName = PlanActionVPPPreChargeBeforeDeadline
					if nearestVPP != nil {
						ld.vppDeadline = nearestVPP.deadline
					}
					ld.rechargeCost = rechargeCost
					ld.futureExportRate = futureExportRate
					ld.futurePeakRate = futurePeakRate
					ld.minArbitrageDiff = minArbitrageDiff
					ld.minDeficitDiff = minDeficitDiff
					ld.earliestArbitrageTime = earliestArbitrageTime
				}

				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   defaultSolarMode,
					reason:      selectedReason,
					description: selectedDesc,
					actionName:  actionName,
					targetSOC:   selectedTargetSOC,
					isTruePeak:  isTruePeak,
					logData:     ld,
				})
			}
		}
	}

	// Branch D: Direct Solar Export (with BatteryModeLoad)
	// Never offered when at or below reserve floor so surplus solar restores backup protection before exporting.
	//
	// Note on BatteryModeStandby + SolarModeExport:
	// We deliberately do not support a candidate for BatteryModeStandby with SolarModeExport.
	// Behind a single net meter, retail export compensation is almost never higher than the retail
	// import rate (outside of dedicated VPP dispatch events, which have their own rules).
	// If the battery were held in Standby while exporting solar, household load would have to be
	// served by grid imports at the full retail rate while exporting solar at a rate <= import rate.
	// Displacing home load first (via BatteryModeLoad + SolarModeExport or storing solar) is always
	// economically superior or equal to importing grid power to enable solar export.
	if canDirectSolarExport && isAboveReserve && beforeVPPRechargeDeadline {
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeExport,
			reason:      types.ActionReasonDirectExport,
			description: "Direct Solar Export: battery covers load; solar exports.",
			actionName:  PlanActionDirectSolarExport,
			isTruePeak:  isTruePeak,
			logData:     ld,
		})
	}

	// Branch E: Battery Grid Export Dump (Opportunity cost of home offset + degradation hurdle)
	if settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM && canExport && beforeVPPRechargeDeadline && interval.exportRate > 0 {
		cycleHurdle := max(priceEpsilonForEquality, settings.MinBatteryExportDifferenceDollarsPerKWH)

		// Lowest possible alternative value of stored battery energy (avoided load or replacement)
		minAlternativeValue := anchors.knownPostHorizonRate
		for i := stepIdx; i < len(timeline); i++ {
			if timeline[i].importRate < minAlternativeValue {
				minAlternativeValue = timeline[i].importRate
			}
		}

		// Account for round-trip efficiency when calculating replacement energy cost.
		// Direct battery export dump is evaluated post-losses (minAlternativeValue / roundTripEff)
		// because energy stored in the battery incurs round-trip efficiency loss when replaced;
		// the export rate must clear the replacement cost divided by round-trip efficiency plus
		// the degradation cycle hurdle to guarantee net economic benefit.
		rechargeCost := minAlternativeValue / effRT

		// Export is a viable candidate if export rate beats the replacement cost by at least the degradation hurdle
		if interval.exportRate >= (rechargeCost + cycleHurdle) {
			targetSOC := int(math.Round(effectiveReserveSOC + exportReserveMarginPct))
			ld.exportReplacementCost = rechargeCost
			ld.minAlternativeValue = minAlternativeValue
			ld.cycleHurdle = cycleHurdle

			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeExport,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonDirectExport,
				description: "Battery Grid Export Dump.",
				actionName:  PlanActionBatteryGridExportDump,
				targetSOC:   targetSOC,
				isTruePeak:  isTruePeak,
				logData:     ld,
			})
		}
	}

	return candidates
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
		return cleanState, intervalMetrics{
			endingSOC: cleanState.soc,
		}
	}

	reserveBufferPct := settings.GetOptimizationParams().ReserveBufferPercent
	effectiveReserveSOC := interval.minSOC + reserveBufferPct
	if math.IsNaN(effectiveReserveSOC) || effectiveReserveSOC < 0 {
		effectiveReserveSOC = 20.0
	} else if effectiveReserveSOC > 100.0 {
		effectiveReserveSOC = 100.0
	}
	reserveKWH := capacityKWH * (effectiveReserveSOC / 100.0)
	exportReserveKWH := capacityKWH * ((effectiveReserveSOC + exportReserveMarginPct) / 100.0)

	// Half efficiency for one-way conversions: sqrt(roundTripEfficiency)
	rtEff := roundTripEfficiency
	if math.IsNaN(rtEff) || rtEff <= 0 || rtEff > 1.0 {
		rtEff = 0.85
	}
	oneWayEff := math.Sqrt(rtEff)

	// Override reserve floor if non-zero override specified on candidate action (e.g. VPP active event)
	effectiveLoadReserveKWH := reserveKWH
	effectiveExportReserveKWH := exportReserveKWH
	if action.overrideReserveSOC > 0 {
		soc := min(100.0, action.overrideReserveSOC)
		overrideKWH := capacityKWH * (soc / 100.0)
		effectiveLoadReserveKWH = overrideKWH
		effectiveExportReserveKWH = overrideKWH
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
		if action.batteryMode == types.BatteryModeExport {
			usableBatEnergy = max(0.0, newEnergy-effectiveExportReserveKWH)
		}
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
				targetLimitKWH = capacityKWH * float64(action.targetSOC) / 100.0
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
		// Battery supplies unmet home load + exports to grid at max inverter power down to reserve buffer
		usableBatEnergy := max(0.0, newEnergy-effectiveExportReserveKWH)
		maxBatDischargeKWH := min(maxDischargeKW*dHours, usableBatEnergy*oneWayEff)
		batSuppliedHomeKWH = min(unmetHomeLoadKWH, maxBatDischargeKWH)
		newEnergy -= batSuppliedHomeKWH / oneWayEff

		remainingDischargeKWH := max(0.0, maxBatDischargeKWH-batSuppliedHomeKWH)
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

	// AC terminal power for battery charging and discharging
	batChargeKW := (solarToBatKWH + gridChargeKWH) / dHours
	batDischargeKW := (batSuppliedHomeKWH + batExportKWH) / dHours

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
		solarToHomeKW:            solarToHomeKWH / dHours,
		solarExportKW:            solarExportKWH / dHours,
		solarToBatKW:             solarToBatKWH / dHours,
		solarCurtailedKW:         solarCurtailedKWH / dHours,
		batteryChargeKW:          batChargeKW,
		batteryDischargeKW:       batDischargeKW,
		endingSOC:                endingSOC,
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

// dpNode represents a node in the dynamic programming forward search tree.
type dpNode struct {
	state     planState
	totalCost float64
	strikes   int
	action    actionCandidate
	metric    intervalMetrics
	parent    *dpNode
}

// calculateVPPShortfallStrikes calculates constraint violation strikes for failing to meet a VPP deadline.
// It determines if 100% SOC is physically reachable given initial SOC, lead time, and max charge power.
// If reachable target is not met, integer strikes (1 strike per % deficit) are returned.
func calculateVPPShortfallStrikes(
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

// modeIndex maps a types.BatteryMode to an integer slot (0..3) for mode-isolated DP buckets.
func modeIndex(mode types.BatteryMode) int {
	switch mode {
	case types.BatteryModeLoad:
		return 0
	case types.BatteryModeStandby:
		return 1
	case types.BatteryModeChargeAny:
		return 2
	case types.BatteryModeExport:
		return 3
	default:
		return 0
	}
}

// searchOptimalPlan performs a discrete-state dynamic programming forward search over the action graph.
// State discretization uses 0.5% SOC buckets (201 discrete buckets from 0% to 100%), guaranteeing
// mathematical optimality within telemetry resolution while executing in sub-millisecond time.
func (c *Controller) searchOptimalPlan(
	ctx context.Context,
	timeline []planInterval,
	initial planState,
	anchors planningAnchors,
	settings types.Settings,
	currentStatus types.SystemStatus,
	history []types.EnergyStats,
	lastAction *types.Action,
) (*planPath, error) {
	if len(timeline) == 0 {
		return &planPath{}, nil
	}

	refTime := initial.time
	if !timeline[0].startTime.IsZero() {
		refTime = timeline[0].startTime
	}
	lastAction = sanitizeLastAction(lastAction, refTime)

	// Step 0 candidate generation
	candidates0 := c.generateActionCandidates(ctx, 0, timeline[0], timeline, initial, anchors, settings, currentStatus, history, c.toPrecedingAction(lastAction, settings))
	if len(candidates0) == 0 {
		return nil, fmt.Errorf("no viable plan found: all candidate actions pruned at step 0")
	}

	modeBestScores := make(map[types.BatteryMode]float64)
	modeBestStrikes := make(map[types.BatteryMode]int)
	modeBestNodes := make(map[types.BatteryMode]*dpNode)
	totalPathsEvaluated := 0

	roundTripEff := settings.GetOptimizationParams().RoundTripEfficiency
	oneWayEff := math.Sqrt(roundTripEff)
	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		capacityKWH = initial.capacityKWH
	}
	batteryExportHurdle := max(priceEpsilonForEquality, settings.MinBatteryExportDifferenceDollarsPerKWH)
	directSolarExportHurdle := max(priceEpsilonForEquality, settings.MinArbitrageDifferenceDollarsPerKWH)
	gridChargeArbitrageHurdle := directSolarExportHurdle
	isFlatNEM := isFlatNetMetering(settings.UtilityRateOptions)

	// Pre-calculate whether daytime solar refill is projected ahead for each timeline interval
	// Discharging is only penalized if tonight's import rate is within
	// defaultBatteryCyclingHoldHurdleDollars of daytime solar export credit.
	solarRefillAhead := make([]bool, len(timeline))
	if settings.GridExportSolar && !isFlatNEM {
		for i := 0; i < len(timeline); i++ {
			for j := i + 1; j < len(timeline); j++ {
				if timeline[j].solarKWH > minSignificantSolarKW*timeline[j].durationHours &&
					timeline[i].importRate+defaultBatteryCyclingHoldHurdleDollars <= timeline[j].exportRate+priceEpsilonForEquality {
					solarRefillAhead[i] = true
					break
				}
			}
		}
	}

	// Identify the nearest upcoming VPP event whose deadline has not passed relative to step 0.
	// Only the nearest upcoming event deadline is evaluated for feasibility; future events will be evaluated
	// in subsequent rolling plans once the earlier event concludes, preventing false penalties on trajectories
	// that intentionally discharge for the first event.
	var nearestVPPDeadline time.Time
	hasNearestVPP := false
	for i := range anchors.vppEvents {
		if anchors.vppEvents[i].mandatory && anchors.vppEvents[i].deadline.After(timeline[0].startTime) {
			if !hasNearestVPP || anchors.vppEvents[i].deadline.Before(nearestVPPDeadline) {
				hasNearestVPP = true
				nearestVPPDeadline = anchors.vppEvents[i].deadline
			}
		}
	}
	// Pre-calculate the maximum future valuation rate for each step across the remaining horizon.
	// Used in DP intra-bucket dominance comparisons to accurately value retained battery energy
	// against upcoming peak periods or export/VPP compensation rather than undervaluing it at the current off-peak rate.
	maxFutureValuationRates := make([]float64, len(timeline))
	canBatteryExport := settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM
	for i := len(timeline) - 1; i >= 0; i-- {
		rate := timeline[i].importRate
		if canBatteryExport && timeline[i].exportRate > rate {
			rate = timeline[i].exportRate
		}
		for _, vpp := range anchors.vppEvents {
			if vpp.price > rate && !timeline[i].startTime.Before(vpp.eventStart) && timeline[i].startTime.Before(vpp.eventEnd) {
				rate = vpp.price
			}
		}

		if i < len(timeline)-1 && maxFutureValuationRates[i+1] > rate {
			rate = maxFutureValuationRates[i+1]
		}
		if anchors.knownPostHorizonRate > rate {
			rate = anchors.knownPostHorizonRate
		}
		maxFutureValuationRates[i] = rate
	}

	// Evaluate forward rollout independently for each initial candidate action at step 0.
	// This ensures every distinct immediate dispatch action explores its optimal future trajectory.
	for _, cand0 := range candidates0 {
		cand0Slice := []actionCandidate{cand0}

		// root is the genesis/sentinel node representing the battery's real-time state (initial)
		// before any decisions in the planning horizon are taken. It has no parent and zero cost.
		root := &dpNode{
			state: initial,
		}

		// activeNodes represents the wavefront (search frontier) of surviving Pareto-optimal
		// trajectories at the current timeline step.
		//
		// How path tracking and backtracking work:
		// 1. Memory Efficiency: We do not store complete path histories in memory at every step.
		//    Instead, activeNodes only holds the latest frontier (at most 201 nodes per step).
		// 2. Tree Structure via Linked List: Each child dpNode stores a parent pointer (*dpNode)
		//    referencing the node in the previous interval that produced it.
		// 3. Step Progression: At each interval, all activeNodes branch across permissible actions
		//    into nextBuckets. After intra-bucket dominance pruning, activeNodes is replaced by
		//    the surviving nodes from nextBuckets to form the frontier for the next interval.
		// 4. Chronological Backtracking: Once the horizon concludes, the winning terminal node
		//    (lowest total cost + terminal battery valuation) is selected. We simply traverse its
		//    parent pointers backward all the way to root (where parent == nil) to reconstruct
		//    the complete chronological sequence of states, actions, and metrics.
		activeNodes := []*dpNode{root}

		// Roll forward through the timeline steps starting at step 0 using mode-aware fine bucketing.
		for stepIdx := 0; stepIdx < len(timeline); stepIdx++ {
			interval := timeline[stepIdx]

			// nextBuckets discretizes continuous battery State of Charge into 0.5% increments
			// (201 buckets from 0.0% to 100.0%) partitioned independently by candidate battery mode (4 slots).
			// This eliminates mode collisions (e.g. Standby vs Load) while maintaining sub-millisecond execution.
			//
			// Do not reduce to 101 buckets:
			// We experimentally tested reducing this to 101 buckets (1.0% SOC bins). While it halved the state space,
			// it caused massive economic regressions across real-world site recordings in TestPlanHistory (+17.4% total
			// cost increase, failing 9 of 18 customer site benchmarks, with individual sites degrading by up to $7.17/day).
			//
			// Root cause: The planning horizon evaluates 20-minute intervals. A typical household baseload of 300 W
			// consumes 0.10 kWh in 20 minutes, which represents ~0.74% SOC on a standard 13.5 kWh battery pack. At 1.0%
			// bucket width, multiple distinct energy deltas and trickle solar refill collapse into the same bin at every
			// step. Over a 72-step (24-hour) horizon, intra-bucket dominance comparison prematurely discards trajectories
			// with fractional-percent energy advantages that would have preserved just enough charge to shave expensive
			// peak rates hours later. 0.5% resolution (201 buckets) is mathematically necessary to capture residential
			// sub-hourly dynamics.
			//
			// Note: if you raise this then consider changing socTargetTolerancePct
			var nextBuckets [4][201]*dpNode

			for _, parent := range activeNodes {
				var candidates []actionCandidate
				if stepIdx == 0 {
					candidates = cand0Slice
				} else {
					candidates = c.generateActionCandidates(
						ctx,
						stepIdx,
						interval,
						timeline,
						parent.state,
						anchors,
						settings,
						currentStatus,
						history,
						toPrecedingAction(parent.action, timeline[stepIdx-1]),
					)
				}
				for _, cand := range candidates {
					// Anti-churn re-entry guard: within a continuous price window (same import and export rates),
					// once a trajectory exits a discretionary grid mode (ChargeAny or Export), it cannot
					// re-enter that same mode at the same rates. This prevents fragmented Charge/Export -> Load -> Charge/Export
					// fluttering during identical price periods, while preserving the solver's freedom to stop charging
					// at the optimal partial SOC or stop exporting when the profitable price window concludes.
					isDiscretionaryGridMode := cand.batteryMode == types.BatteryModeChargeAny || cand.batteryMode == types.BatteryModeExport
					if stepIdx > 0 && isDiscretionaryGridMode && parent.action.batteryMode != cand.batteryMode {
						currImport := interval.importRate
						currExport := interval.exportRate
						currStep := stepIdx - 1
						isReentry := false
						for p := parent; p != nil && currStep >= 0; p = p.parent {
							sameImport := math.Abs(timeline[currStep].importRate-currImport) <= priceEpsilonForEquality
							sameExport := math.Abs(timeline[currStep].exportRate-currExport) <= priceEpsilonForEquality
							if !sameImport || !sameExport {
								break // Tariff changed (import rate or export rate decoupled)
							}
							if p.action.batteryMode == cand.batteryMode {
								isReentry = true
								break
							}
							currStep--
						}
						// If ancestor trace reaches step 0, check lastAction from previous polling cycle
						if !isReentry && currStep < 0 && lastAction != nil && lastAction.CurrentPrice != nil {
							lastImport := lastAction.CurrentPrice.DollarsPerKWH + lastAction.CurrentPrice.GridUseDollarsPerKWH
							lastExport := c.calculateExportCredit(*lastAction.CurrentPrice, settings, nil)
							sameImport := math.Abs(lastImport-currImport) <= priceEpsilonForEquality
							sameExport := math.Abs(lastExport-currExport) <= priceEpsilonForEquality
							if sameImport && sameExport && lastAction.BatteryMode == cand.batteryMode {
								isReentry = true
							}
						}
						if isReentry {
							// Emergency Reserve Deficit Exemption:
							// Discretionary arbitrage charging (cycling above reserve to sell or offset peak) must not
							// flutter on/off during the same price block. However, emergency deficit recovery
							// (any BatteryModeChargeAny when parent.state.soc < interval.minSOC) must NEVER be discarded,
							// regardless of which specific candidate reason initiated the charge.
							//
							// While the optimizer naturally prioritizes continuous charging to restore reserve as quickly
							// as possible (enforced by the running deficitPenalty), valid multi-step scenarios can occur
							// where grid charging is legitimately interrupted and resumed at the same rate:
							// 1. Solar Interruption: Grid charging runs at dawn, pauses when surplus rooftop solar takes over
							//    charging for free, and must be allowed to resume from the grid if clouds drop solar below load.
							// 2. Intra-horizon Depletion: A trajectory that pre-charged earlier in the flat block and then
							//    discharged to cover large home loads may subsequently deplete into deficit; it must not be
							//    prohibited from charging back up to the reserve floor simply because it charged hours earlier.
							// 3. Polling Continuity: If lastAction was ChargeAny but step 0 evaluated Load/Standby before dipping
							//    into deficit, the system must immediately restore emergency backup reserve.
							isReserveDeficit := cand.batteryMode == types.BatteryModeChargeAny &&
								parent.state.soc < interval.minSOC-reserveFloorTolerancePct
							if !isReserveDeficit {
								continue
							}
						}
					}

					nextState, metrics := stepPhysics(parent.state, cand, interval, settings, roundTripEff)

					var holdCost float64
					if settings.GridExportSolar && !isFlatNEM && solarRefillAhead[stepIdx] && parent.state.soc < almostFullSOC {
						holdCost = metrics.batSuppliedHomeKWH * defaultBatteryCyclingHoldHurdleDollars
					}

					var transitionCost float64
					if stepIdx == 0 {
						// Inertia penalty: if switching away from lastAction at step 0, add default penalty
						// to prevent rapid mode oscillation.
						if lastAction != nil {
							if cand.batteryMode != lastAction.BatteryMode {
								transitionCost = max(0.01, defaultInertiaThresholdDollars*min(1.0, interval.durationHours))
							} else if lastAction.SolarMode > 0 && cand.solarMode > 0 && cand.solarMode != lastAction.SolarMode {
								transitionCost = modeSwitchPenalty
							}
						}
					} else if parent.action.batteryMode != cand.batteryMode || (parent.action.solarMode > 0 && cand.solarMode > 0 && parent.action.solarMode != cand.solarMode) {
						transitionCost = modeSwitchPenalty
					}

					// Direct battery export (BatteryModeExport) discharges stored battery energy directly into the grid
					// at maximum inverter power (0.5C–1.0C). This high-power discharge incurs substantial thermal and cell wear,
					// and is governed by the homeowner's configured battery export profit hurdle (batteryExportHurdle).
					//
					// Direct solar export (BatteryModeLoad + SolarModeExport) exports rooftop solar to the grid while
					// simultaneously discharging the battery to cover home load (metrics.batSuppliedHomeKWH) at gentle baseline
					// C-rates (0.03C–0.1C). While the solar export itself incurs zero cell wear, the home discharge is supplied
					// by the battery. We apply the gentle cycling degradation hurdle (directSolarExportHurdle, derived from
					// MinArbitrageDifferenceDollarsPerKWH) to prevent unprofitable cycling without blocking solar export with the
					// 10 kW grid-dump barrier.
					var exportDegradationCost float64
					if cand.batteryMode == types.BatteryModeExport {
						exportDegradationCost = metrics.batExportKWH * batteryExportHurdle
					} else if cand.batteryMode == types.BatteryModeLoad && cand.solarMode == types.SolarModeExport {
						exportDegradationCost = min(metrics.batSuppliedHomeKWH, metrics.solarExportKWH) * directSolarExportHurdle
					}

					// Grid charge arbitrage (BatteryModeChargeAny): When charging from the grid above the configured reserve
					// floor for economic arbitrage (avoiding peak import or enabling export), the battery is cycled discretionarily.
					// Charging up to the reserve floor is essential emergency backup protection (exempt from degradation hurdle).
					// Any grid charging above the reserve floor incurs the cycling degradation hurdle (gridChargeArbitrageHurdle)
					// to ensure the price spread justifies the battery cell wear.
					var chargeDegradationCost float64
					if metrics.gridChargeKWH > 0 && cand.reason == types.ActionReasonArbitrageChargeExport {
						reserveKWH := (interval.minSOC / 100.0) * capacityKWH
						reserveDeficitEnergyKWH := max(0.0, reserveKWH-parent.state.energyKWH)
						gridChargeForReserveKWH := min(metrics.gridChargeKWH, reserveDeficitEnergyKWH/oneWayEff)
						gridChargeAboveReserveKWH := max(0.0, metrics.gridChargeKWH-gridChargeForReserveKWH)
						chargeDegradationCost = gridChargeAboveReserveKWH * gridChargeArbitrageHurdle
					}

					// Penalizes idling or running below the configured reserve floor (interval.minSOC) without recharging.
					// If the battery is below reserve and charging is physically possible (either from the grid or from surplus
					// solar), the system must actively restore the homeowner's backup reserve immediately.
					//
					// 1. Surplus solar recharge: If surplus solar is available and the battery is absorbing it (cand.solarMode != SolarModeExport),
					//    the deficit is actively being restored by solar without requiring grid imports.
					// 2. Direct solar export bypass: If direct solar export is chosen (cand.solarMode == SolarModeExport), surplus solar
					//    bypasses charging the battery and exports to the grid, so the deficit penalty MUST apply to prevent abandoning reserve.
					// 3. Grid charging disabled: If GridChargeBatteries is disabled and there is no surplus solar, it is physically impossible
					//    to charge otherwise; the site is not penalized for an unavoidable deficit.
					inDeficit := nextState.soc < interval.minSOC-reserveFloorTolerancePct
					isActivelyGridCharging := cand.batteryMode == types.BatteryModeChargeAny
					canGridCharge := settings.GridChargeBatteries && !parent.state.chargingDisabled
					hasSurplusSolar := interval.durationHours > 0 && (interval.solarKWH-interval.loadKWH) >= minSurplusSolarForReserveKW*interval.durationHours
					isRechargingFromSolar := hasSurplusSolar && cand.solarMode != types.SolarModeExport

					if inDeficit && !isActivelyGridCharging && !isRechargingFromSolar && (canGridCharge || hasSurplusSolar) {
						// Hard constraint: when below reserve floor and recharging is physically possible,
						// discard any trajectory that fails to restore the homeowner's backup reserve.
						continue
					}

					newCost := parent.totalCost + metrics.costDollars + exportDegradationCost + chargeDegradationCost + holdCost + transitionCost + (nextState.energyKWH * interval.durationHours * batteryHoldingCostPerHourPerKWH)

					var strikeInc int
					// Check VPP deadline feasibility: apply strikes for nearest upcoming event deadline
					if hasNearestVPP && settings.GridChargeBatteries && !initial.chargingDisabled {
						if interval.endTime.Equal(nearestVPPDeadline) || (interval.startTime.Before(nearestVPPDeadline) && interval.endTime.After(nearestVPPDeadline)) {
							strikeInc += calculateVPPShortfallStrikes(initial, nextState, nearestVPPDeadline, oneWayEff, settings.GridChargeBatteries)
						}
					}

					// Peak Survival Buffer Cost:
					// At the conclusion of a peak pricing window, evaluate whether the simulated trajectory
					// survives the peak pricing period with the configured PeakSurvivalBufferMinutes.
					//
					// The peak survival buffer represents a risk-hedging safety cushion (e.g. 15–30 mins of home load),
					// NOT a critical hardware violation like breaching interval.minSOC. Once a peak window concludes,
					// rates have already dropped back to off-peak/mid-peak, so exiting without the buffer merely incurs
					// the minor risk that a peak window could extend or load could fluctuate right at the boundary.
					// Applying a soft penalty (peakSurvivalBufferShortfallPenaltyDollarsPerKWH = $0.02/kWh) creates
					// a steady, gentle preference in the DP search to preserve the cushion without overriding true energy economics.
					//
					// Previously, penaltyRate was calculated dynamically as min(maxAllowedPenalty, (minHorizonImportRate / roundTripEff) + spread).
					// On high-load sites or conservative profiles (η = 0.85), an off-peak rate of $0.32/kWh produced a penalty
					// of $0.428/kWh. Because the penalty rate ($0.428) exceeded retail electricity prices ($0.321), discharging
					// the battery to power the home during the day appeared to the DP as a net financial loss! The solver
					// locked the battery in Standby and forced the home to buy grid power for 8+ hours just to avoid a penalty at 9 PM.
					var peakBufferCost float64
					for _, pw := range anchors.peakWindows {
						if stepIdx == pw.endIndex && pw.bufferEnergyKWH > 0 {
							reserveBufferPct := settings.GetOptimizationParams().ReserveBufferPercent
							effectiveReserveSOC := interval.minSOC + reserveBufferPct
							reserveKWH := capacityKWH * (effectiveReserveSOC / 100.0)
							targetEnergyKWH := min(capacityKWH*0.50, reserveKWH+pw.bufferEnergyKWH)

							if nextState.energyKWH < targetEnergyKWH {
								shortfallKWH := targetEnergyKWH - nextState.energyKWH
								peakBufferCost += shortfallKWH * peakSurvivalBufferShortfallPenaltyDollarsPerKWH
							}
						}
					}

					newCost += peakBufferCost
					newStrikes := parent.strikes + strikeInc

					mIdx := modeIndex(cand.batteryMode)
					b := int(math.Round(nextState.soc * 2))
					if b < 0 {
						b = 0
					} else if b > 200 {
						b = 200
					}

					isBetter := false
					if nextBuckets[mIdx][b] == nil {
						isBetter = true
					} else {
						curr := nextBuckets[mIdx][b]
						if newStrikes < curr.strikes {
							isBetter = true
						} else if newStrikes == curr.strikes {
							// Dominance comparison within the same mode and strike count: adjust by marginal residual value of SOC difference.
							socDelta := nextState.soc - curr.state.soc
							energyDeltaKWH := (socDelta / 100.0) * capacityKWH
							valuationRate := anchors.knownPostHorizonRate
							if stepIdx+1 < len(maxFutureValuationRates) {
								valuationRate = maxFutureValuationRates[stepIdx+1]
							}
							marginalValue := energyDeltaKWH * oneWayEff * valuationRate
							effectiveNewCost := newCost - marginalValue
							if effectiveNewCost < curr.totalCost {
								isBetter = true
							}
						}
					}

					if isBetter {
						nextBuckets[mIdx][b] = &dpNode{
							state:     nextState,
							totalCost: newCost,
							strikes:   newStrikes,
							action:    cand,
							metric:    metrics,
							parent:    parent,
						}
					}
				}
			}

			// Collect surviving bucket nodes for the next time step with frontier dominance pruning.
			//
			// What is Frontier Dominance Pruning?
			// At each step, multiple path trajectories land in discrete SOC buckets across 4 mode slots.
			// To compare paths with different SOCs on an equal footing, we compute each node's "effective cost" (effCost):
			//   effCost = totalCost - (storedEnergyKWH * oneWayEff * valuationRate)
			// where valuationRate is the maximum tariff rate appearing anywhere in the remaining horizon.
			// This represents the most optimistic theoretical future net cost if the stored energy is discharged
			// at the highest possible future price.
			//
			// Pruning Rule 1 (Intra-Bucket Mode Consolidation):
			// Between BatteryModeLoad (slot 0) and BatteryModeExport (slot 3), all subsequent physical constraints,
			// valid actions, and transition penalties are identical. In the same 0.5% SOC bucket, the node with
			// the higher effective cost can never overtake the lower one. We discard the higher one.
			//
			// Pruning Rule 2 (Frontier Bounding):
			// Across the entire search wavefront, let minEffCost be the best effective cost found so far.
			// The maximum possible economic benefit ANY battery could ever deliver in the remaining horizon
			// over an empty battery is (capacityKWH * valuationRate). Therefore, any trajectory whose effective
			// cost exceeds minEffCost by more than (capacityKWH * valuationRate) + $1.00 (safety slack margin)
			// is mathematically dominated: even if it had a 100% full battery and the best node had 0%,
			// it could never overcome the cost deficit.
			// Pruning these dominated nodes cuts the active wavefront by ~50% per step without false pruning.
			activeNodes = activeNodes[:0]
			// Value frontier energy entering stepIdx+1 against remaining future rates (stepIdx+1 onwards)
			// or knownPostHorizonRate if at horizon end, avoiding including stepIdx's own completed tariff.
			valuationRate := anchors.knownPostHorizonRate
			if stepIdx+1 < len(maxFutureValuationRates) {
				valuationRate = maxFutureValuationRates[stepIdx+1]
			}

			// Between BatteryModeLoad (0) and BatteryModeExport (3), future physical constraints and
			// transition rules are identical. In the same SOC bucket, keep only the node with lower strikes or effective cost.
			for b := 0; b <= 200; b++ {
				n0 := nextBuckets[0][b]
				n3 := nextBuckets[3][b]
				if n0 != nil && n3 != nil {
					if n0.strikes < n3.strikes {
						nextBuckets[3][b] = nil
					} else if n3.strikes < n0.strikes {
						nextBuckets[0][b] = nil
					} else {
						eff0 := n0.totalCost - (n0.state.soc/100.0)*capacityKWH*oneWayEff*valuationRate
						eff3 := n3.totalCost - (n3.state.soc/100.0)*capacityKWH*oneWayEff*valuationRate
						if eff0 <= eff3 {
							nextBuckets[3][b] = nil
						} else {
							nextBuckets[0][b] = nil
						}
					}
				}
			}

			minStrikes := math.MaxInt
			for m := 0; m < 4; m++ {
				for _, n := range nextBuckets[m] {
					if n != nil && n.strikes < minStrikes {
						minStrikes = n.strikes
					}
				}
			}

			var minEffCost float64 = math.MaxFloat64
			for m := 0; m < 4; m++ {
				for _, n := range nextBuckets[m] {
					if n != nil && n.strikes == minStrikes {
						effCost := n.totalCost - (n.state.soc/100.0)*capacityKWH*oneWayEff*valuationRate
						if effCost < minEffCost {
							minEffCost = effCost
						}
					}
				}
			}

			maxAllowedEffCost := minEffCost + (capacityKWH * valuationRate) + 1.00

			for m := 0; m < 4; m++ {
				for _, n := range nextBuckets[m] {
					if n != nil && n.strikes == minStrikes {
						effCost := n.totalCost - (n.state.soc/100.0)*capacityKWH*oneWayEff*valuationRate
						if effCost <= maxAllowedEffCost {
							activeNodes = append(activeNodes, n)
						}
					}
				}
			}
			if len(activeNodes) == 0 {
				break
			}
		}

		if len(activeNodes) == 0 {
			// No branch from this initial candidate could satisfy all physical constraints
			continue
		}

		// Evaluate terminal battery valuation across all surviving trajectories for this cand0
		var bestNodeForCand *dpNode
		var bestScoreForCand float64 = math.MaxFloat64
		var bestStrikesForCand int = math.MaxInt

		for _, node := range activeNodes {
			totalPathsEvaluated++
			termVal := calculateTerminalValuation(node.state, anchors, settings, timeline, capacityKWH, roundTripEff)
			score := node.totalCost + termVal
			if node.strikes < bestStrikesForCand || (node.strikes == bestStrikesForCand && score < bestScoreForCand) {
				bestStrikesForCand = node.strikes
				bestScoreForCand = score
				bestNodeForCand = node
			}
		}

		if bestNodeForCand != nil {
			prevStrikes, exists := modeBestStrikes[cand0.batteryMode]
			prevScore := modeBestScores[cand0.batteryMode]
			isBetter := !exists || bestStrikesForCand < prevStrikes || (bestStrikesForCand == prevStrikes && bestScoreForCand < prevScore-priceEpsilonForEquality)
			if !isBetter && exists && bestStrikesForCand == prevStrikes && math.Abs(bestScoreForCand-prevScore) <= priceEpsilonForEquality {
				// Tie-breaker: prefer candidate matching lastAction to avoid unnecessary step-0 mode switches
				if lastAction != nil && cand0.batteryMode == lastAction.BatteryMode && cand0.solarMode == lastAction.SolarMode {
					isBetter = true
				}
			}
			if isBetter {
				modeBestStrikes[cand0.batteryMode] = bestStrikesForCand
				modeBestScores[cand0.batteryMode] = bestScoreForCand
				modeBestNodes[cand0.batteryMode] = bestNodeForCand
			}
		}
	}

	if len(modeBestScores) == 0 {
		return nil, fmt.Errorf("no viable plan found: all candidate paths were pruned")
	}

	// Identify winning mode and runner-up mode in deterministic preference order.
	//
	// modePriority serves strictly as a TIE-BREAKER when candidate modes produce identical
	// overall plan scores (within $1e-6). When an alternative mode is economically superior
	// (e.g. Standby to save energy for an upcoming peak/VPP, or ChargeAny at cheap/negative rates),
	// it wins outright on raw score.
	//
	// Why BatteryModeLoad (Self-Consumption) takes precedence over BatteryModeStandby on ties:
	// 1. Core Purpose & User Expectation: Residential batteries exist primarily to power the home
	//    and avoid grid imports. Cycle degradation (cycleHurdle) is intentionally applied only to
	//    grid exports (batExportKWH), not to household self-consumption (batSuppliedHomeKWH).
	// 2. Avoids "Frozen Battery" on Flat Rates: If Standby took precedence on ties, the battery
	//    would sit idle on flat or off-peak rates while the home needlessly imports grid power,
	//    and the battery would remain full at dawn, unable to store tomorrow's solar generation.
	// 3. Real-Time Responsiveness: BatteryModeLoad keeps the inverter's local control loop active,
	//    instantly offsetting unexpected appliance loads between 5-minute polling cycles. Standby
	//    locks the battery and forces unexpected loads onto the utility grid.
	// 4. Physical Equivalence on Ties: Near-identical scores typically occur at the reserve floor
	//    or during zero net load, where both modes discharge 0 kW anyway; BatteryModeLoad is the
	//    safer, standard operational baseline.
	modePriority := []types.BatteryMode{
		types.BatteryModeLoad,
		types.BatteryModeStandby,
		types.BatteryModeChargeAny,
		types.BatteryModeExport,
	}

	var bestOverallNode *dpNode
	var bestOverallScore float64 = math.MaxFloat64
	var bestOverallStrikes int = math.MaxInt
	var runnerUpScore float64 = math.MaxFloat64
	var runnerUpStrikes int = math.MaxInt

	for _, mode := range modePriority {
		score, exists := modeBestScores[mode]
		if !exists {
			continue
		}
		strikes := modeBestStrikes[mode]
		node := modeBestNodes[mode]
		if strikes < bestOverallStrikes || (strikes == bestOverallStrikes && score < bestOverallScore-priceEpsilonForEquality) {
			if bestOverallNode != nil {
				runnerUpScore = bestOverallScore
				runnerUpStrikes = bestOverallStrikes
			}
			bestOverallStrikes = strikes
			bestOverallScore = score
			bestOverallNode = node
		} else if strikes < runnerUpStrikes || (strikes == runnerUpStrikes && score < runnerUpScore-priceEpsilonForEquality) {
			runnerUpScore = score
			runnerUpStrikes = strikes
		}
	}

	// Reconstruct chronological winning path from the terminal node
	n := len(timeline)
	actions := make([]actionCandidate, n)
	states := make([]planState, n+1)
	metrics := make([]intervalMetrics, n)

	curr := bestOverallNode
	states[n] = curr.state
	for i := n - 1; i >= 0; i-- {
		actions[i] = curr.action
		metrics[i] = curr.metric
		curr = curr.parent
		states[i] = curr.state
	}

	// Fast-Track Delay Check:
	// RateRudder only executes hardware mode changes when its polling loop runs (every 20-25 minutes).
	// If the optimal trajectory did not charge immediately at step 0 (e.g. due to step-0 inertia penalty
	// or holding cost), but scheduled a grid charge in a subsequent step starting within fastTrackDelayThreshold
	// (<= 15 minutes) at an equal or cheaper rate, promote the pre-computed ChargeAny optimal trajectory.
	// This avoids missing the charge window while sleeping, and uses the DP's already-calculated
	// physically exact ChargeAny plan without requiring any post-hoc re-simulation.
	if actions[0].batteryMode != types.BatteryModeChargeAny &&
		actions[0].reason != types.ActionReasonEVChargingStandby &&
		settings.GridChargeBatteries &&
		!currentStatus.BatteryChargingDisabled &&
		!currentStatus.GridUnavailable &&
		currentStatus.BatterySOC < 100.0 {

		now := currentStatus.Timestamp
		if now.IsZero() {
			now = timeline[0].startTime
		}

		capKWH := currentStatus.BatteryCapacityKWH
		if capKWH <= 0 {
			capKWH = initial.capacityKWH
		}
		maxChargeKW := resolveBatteryPowerKW(currentStatus.MaxBatteryChargeKW, capKWH)
		headroomKWH := (100.0 - currentStatus.BatterySOC) / 100.0 * capKWH
		startHeadroom := calculateStartChargeHeadroomKWH(capKWH, maxChargeKW, settings)

		if headroomKWH >= startHeadroom {
			shouldFastTrack := false
			var fastTrackStartTime time.Time
			var fastTrackDelay time.Duration
			for i := 1; i < len(actions) && i < len(timeline); i++ {
				delay := timeline[i].startTime.Sub(now)
				if delay > fastTrackDelayThreshold {
					break
				}
				if actions[i].batteryMode == types.BatteryModeChargeAny {
					if timeline[0].importRate <= timeline[i].importRate+priceEqualityToleranceDollars {
						shouldFastTrack = true
						fastTrackStartTime = timeline[i].startTime
						fastTrackDelay = delay
						break
					}
				}
			}

			if shouldFastTrack {
				chargeNode, exists := modeBestNodes[types.BatteryModeChargeAny]
				chargeScore := modeBestScores[types.BatteryModeChargeAny]
				chargeStrikes := modeBestStrikes[types.BatteryModeChargeAny]
				if !exists || chargeNode == nil {
					log.Ctx(ctx).DebugContext(ctx, "fast-track grid charge skipped: no valid ChargeAny node exists in plan search",
						slog.Time("scheduledChargeTime", fastTrackStartTime),
						slog.Duration("delay", fastTrackDelay),
					)
				} else if chargeStrikes <= bestOverallStrikes && chargeScore-bestOverallScore <= defaultInertiaThresholdDollars {
					originalMode := actions[0].batteryMode
					originalScore := bestOverallScore
					originalStrikes := bestOverallStrikes
					var originalCost float64
					for _, m := range metrics {
						originalCost += m.costDollars
					}

					bestOverallNode = chargeNode
					bestOverallScore = modeBestScores[types.BatteryModeChargeAny]
					bestOverallStrikes = chargeStrikes

					curr = bestOverallNode
					states[n] = curr.state
					for i := n - 1; i >= 0; i-- {
						actions[i] = curr.action
						metrics[i] = curr.metric
						curr = curr.parent
						states[i] = curr.state
					}

					var promotedCost float64
					for _, m := range metrics {
						promotedCost += m.costDollars
					}

					log.Ctx(ctx).DebugContext(ctx, "fast-tracking scheduled grid charge to step 0 due to polling cycle",
						slog.Time("scheduledChargeTime", fastTrackStartTime),
						slog.Duration("delay", fastTrackDelay),
						slog.Float64("step0ImportRate", timeline[0].importRate),
						slog.Float64("batterySOC", currentStatus.BatterySOC),
						slog.Float64("headroomKWH", headroomKWH),
						slog.String("originalMode", drModeString(originalMode)),
						slog.Float64("originalScore", originalScore),
						slog.Int("originalStrikes", originalStrikes),
						slog.Float64("originalCostDollars", originalCost),
						slog.String("promotedMode", drModeString(actions[0].batteryMode)),
						slog.String("promotedReason", string(actions[0].reason)),
						slog.Float64("promotedScore", bestOverallScore),
						slog.Int("promotedStrikes", bestOverallStrikes),
						slog.Float64("promotedCostDollars", promotedCost))
				}
			}
		}
	}

	var totalPhysicalCost float64
	for _, m := range metrics {
		totalPhysicalCost += m.costDollars
	}

	winningStrikes := bestOverallStrikes
	if winningStrikes == math.MaxInt {
		winningStrikes = 0
	}
	ruStrikes := runnerUpStrikes
	if ruStrikes == math.MaxInt {
		ruStrikes = 0
	}

	bestPath := &planPath{
		actions:           actions,
		states:            states,
		metrics:           metrics,
		timeline:          timeline,
		totalCost:         totalPhysicalCost,
		strikes:           winningStrikes,
		initialCandidates: candidates0,
		modeScores:        modeBestScores,
		modeStrikes:       modeBestStrikes,
		bestScore:         bestOverallScore,
	}

	bestPath, _ = bestPath.refineOverchargedEpisodes(ctx, settings, roundTripEff)
	bestOverallScore = bestPath.bestScore

	modeSwitched := false
	lastModeStr := "none"
	if lastAction != nil && len(bestPath.actions) > 0 {
		lastModeStr = drModeString(lastAction.BatteryMode)
		if bestPath.actions[0].batteryMode != lastAction.BatteryMode {
			modeSwitched = true
		}
	}

	costDelta := runnerUpScore - bestOverallScore
	if runnerUpScore == math.MaxFloat64 || costDelta < 0 {
		costDelta = 0
	}

	log.Ctx(ctx).DebugContext(ctx, "optimal plan selected",
		slog.String("chosenBatteryMode", drModeString(bestPath.actions[0].batteryMode)),
		slog.String("chosenSolarMode", solarModeString(bestPath.actions[0].solarMode)),
		slog.String("chosenReason", string(bestPath.actions[0].reason)),
		slog.Float64("winningTotalNetCostDollars", bestPath.totalCost),
		slog.Float64("winningTotalScoreWithValuationDollars", bestOverallScore),
		slog.Int("strikes", bestPath.strikes),
		slog.Int("runnerUpStrikes", ruStrikes),
		slog.Float64("runnerUpScoreDollars", runnerUpScore),
		slog.Float64("costDeltaDollars", costDelta),
		slog.Bool("modeSwitchedFromLastAction", modeSwitched),
		slog.String("lastActionMode", lastModeStr),
		slog.Int("totalSurvivingPathsEvaluated", totalPathsEvaluated),
		slog.Float64("terminalSOC", bestPath.states[len(bestPath.states)-1].soc),
	)

	return bestPath, nil
}

// calculateTerminalValuation credits or penalizes ending battery energy based on replacement cost.
func calculateTerminalValuation(
	finalState planState,
	anchors planningAnchors,
	settings types.Settings,
	timeline []planInterval,
	capacityKWH float64,
	roundTripEfficiency float64,
) float64 {
	baseReserveSOC := settings.MinBatterySOC
	targetReserveSOC := baseReserveSOC
	if len(timeline) > 0 && timeline[len(timeline)-1].minSOC > 0 {
		targetReserveSOC = timeline[len(timeline)-1].minSOC
	}
	baseReserveKWH := capacityKWH * (baseReserveSOC / 100.0)
	targetEnergyKWH := capacityKWH * (targetReserveSOC / 100.0)

	// Determine replacement rate at horizon end
	replacementRate := anchors.knownPostHorizonRate
	oneWayEff := math.Sqrt(roundTripEfficiency)
	minAcceptableBaseKWH := capacityKWH * ((baseReserveSOC - reserveFloorTolerancePct) / 100.0)

	// 1. Ending below the base emergency reserve (settings.MinBatterySOC):
	// Penalize heavily to eliminate cheating/arbitraging the homeowner's backup reserve.
	if finalState.energyKWH < minAcceptableBaseKWH {
		deficitKWH := baseReserveKWH - finalState.energyKWH
		deficitPenaltyRate := max(minReserveDeficitPenaltyDollarsPerKWH, replacementRate*reserveDeficitPenaltyMultiplier)
		penalty := (deficitKWH / oneWayEff) * deficitPenaltyRate
		if targetEnergyKWH > baseReserveKWH {
			touDeficitKWH := targetEnergyKWH - baseReserveKWH
			rtEff := roundTripEfficiency
			if rtEff <= 0 || rtEff > 1.0 {
				rtEff = 0.90
			}
			penalty += (touDeficitKWH / rtEff) * replacementRate
		}
		return penalty
	}

	// 2. Ending below target reserve (e.g. an elevated off-peak target like 90%), but at or above base reserve:
	// If GridChargeBatteries is allowed and replacementRate > 0, the cost to replenish the battery during
	// the upcoming off-peak period is simply the replacement energy cost (accounting for round-trip efficiency).
	minAcceptableTargetKWH := capacityKWH * ((targetReserveSOC - reserveFloorTolerancePct) / 100.0)
	if finalState.energyKWH < minAcceptableTargetKWH {
		deficitKWH := targetEnergyKWH - finalState.energyKWH
		if settings.GridChargeBatteries && replacementRate > 0 {
			rtEff := roundTripEfficiency
			if rtEff <= 0 || rtEff > 1.0 {
				rtEff = 0.90
			}
			return (deficitKWH / rtEff) * replacementRate
		}
		deficitPenaltyRate := max(minReserveDeficitPenaltyDollarsPerKWH, replacementRate*reserveDeficitPenaltyMultiplier)
		return (deficitKWH / oneWayEff) * deficitPenaltyRate
	}

	energyDeltaKWH := finalState.energyKWH - targetEnergyKWH
	if energyDeltaKWH <= 0 {
		// Within the +/- 0.1% ESS hardware tolerance of the reserve floor: no penalty, no credit
		return 0.0
	}

	// 3. Ending above target reserve: credit for banked energy displacing future imports at replacement rate.
	totalCredit := energyDeltaKWH * oneWayEff * replacementRate
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

	// Preserve explicit safety, alarm, or active overrides
	if action.reason == types.ActionReasonEVChargingStandby ||
		action.reason == types.ActionReasonVPPActive ||
		action.reason == types.ActionReasonEmergencyMode ||
		action.reason == types.ActionReasonGridUnavailable ||
		action.reason == types.ActionReasonHasAlarms ||
		action.reason == types.ActionReasonMissingBattery {
		return action.reason, action.description, nil
	}

	importRate := interval.importRate

	reserveBufferPct := settings.GetOptimizationParams().ReserveBufferPercent
	effectiveReserveSOC := interval.minSOC + reserveBufferPct

	isTruePeak := action.isTruePeak || isTruePeakRate(importRate, minHorizonImportRate, maxHorizonImportRate, settings)

	switch action.batteryMode {
	case types.BatteryModeExport:
		return types.ActionReasonDirectExport, "Battery Grid Export Dump.", nil

	case types.BatteryModeChargeAny:
		if action.reason == types.ActionReasonAlwaysChargeBelowThreshold ||
			importRate < 0 ||
			(settings.AlwaysChargeUnderDollarsPerKWH > 0 && importRate <= settings.AlwaysChargeUnderDollarsPerKWH) {
			return types.ActionReasonAlwaysChargeBelowThreshold,
				"Price below threshold; pre-charging battery.", nil
		}
		if action.reason == types.ActionReasonVPPPrep {
			return types.ActionReasonVPPPrep, action.description, nil
		}

		// Battery export or solar export arbitrage ahead in the plan
		for j := stepIdx + 1; j < len(timeline); j++ {
			if settings.GridExportBatteries && j < len(winningPath.metrics) && winningPath.metrics[j].batExportKWH > 0.1 {
				return types.ActionReasonArbitrageChargeExport,
					"Pre-charging for upcoming battery export arbitrage.", &timeline[j].price
			}
			if settings.GridExportSolar && j < len(winningPath.metrics) && (winningPath.metrics[j].gridExportKWH-winningPath.metrics[j].batExportKWH > 0.1 || winningPath.metrics[j].gridExportKWH > 0.1) {
				return types.ActionReasonArbitrageChargeExport,
					"Pre-charging for upcoming solar export arbitrage.", &timeline[j].price
			}
		}

		// Upcoming peak import rate
		_, futPrice := findUpcomingPeak(timeline, stepIdx)
		if action.targetSOC > 0 && action.targetSOC == int(math.Round(interval.minSOC)) {
			return types.ActionReasonDeficitChargeNow,
				"Charging battery to target reserve.", futPrice
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
		if action.reason == types.ActionReasonHoldSimilarPrice {
			return action.reason, "Preserving battery in standby for daytime solar export.", nil
		}

		// 1. WaitingToCharge: Is an upcoming ChargeAny scheduled in the horizon?
		// Applies during off-peak/shoulder if no higher peak intervenes between now and that charge.
		if !isTruePeak {
			chargeIdx := -1
			for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
				if winningPath.actions[j].batteryMode == types.BatteryModeChargeAny {
					chargeIdx = j
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
			if winningPath.actions[j].batteryMode == types.BatteryModeExport {
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

		// 4. Fallback to candidate reason and description
		desc := action.description
		if desc == "" {
			desc = "Preserving battery in standby for upcoming peak rates."
		}
		return action.reason, desc, futPrice

	case types.BatteryModeLoad:
		// 1. Direct Solar Export
		if action.solarMode == types.SolarModeExport && interval.solarKWH > 0 {
			return types.ActionReasonDirectExport, "Direct Solar Export: battery covers load; solar exports.", nil
		}

		// 2. Battery At Reserve
		if state.soc <= effectiveReserveSOC+reserveFloorTolerancePct && metrics.batSuppliedHomeKWH <= minSignificantBatteryPowerKW*interval.durationHours {
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
						if k+1 < len(winningPath.states) && winningPath.states[k+1].soc <= effectiveReserveSOC+reserveFloorTolerancePct {
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
	model map[int]TimeProfile,
) (Decision, types.Plan) {
	if len(winningPath.actions) == 0 || len(timeline) == 0 {
		return Decision{}, types.Plan{TSCreated: now.UTC()}
	}

	immediateInterval := timeline[0]

	// Contextual post-plan reason resolution:
	// Dynamic programming selects optimal modes across time, but homeowner-facing explanations
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

	targetSOC := immediateAction.targetSOC
	if immediateAction.batteryMode == types.BatteryModeStandby {
		targetSOC = 0
	} else if immediateAction.batteryMode == types.BatteryModeChargeAny && targetSOC <= 0 {
		// Option B: Derive target SOC from peak SOC achieved during the winning path's contiguous charge episode
		peakSOC := initialStatus.BatterySOC
		if len(winningPath.states) > 1 && winningPath.states[1].soc > peakSOC {
			peakSOC = winningPath.states[1].soc
		}
		for i := 1; i < len(winningPath.actions) && i < len(timeline); i++ {
			if winningPath.actions[i].batteryMode == types.BatteryModeChargeAny {
				if i+1 < len(winningPath.states) && winningPath.states[i+1].soc > peakSOC {
					peakSOC = winningPath.states[i+1].soc
				}
			} else {
				break
			}
		}
		targetSOC = int(math.Ceil(peakSOC))
		if targetSOC > 100 {
			targetSOC = 100
		}
	} else if immediateAction.overrideReserveSOC > 0 {
		targetSOC = int(math.Round(immediateAction.overrideReserveSOC))
	} else if targetSOC <= 0 {
		targetSOC = int(math.Round(immediateInterval.minSOC))
	}

	// Deficit and capacity hit times detection from winning path trajectory
	var hitDeficitAt time.Time
	var hitCapacityAt time.Time

	reserveBufferPct := settings.GetOptimizationParams().ReserveBufferPercent
	effectiveReserveSOC := immediateInterval.minSOC + reserveBufferPct
	if initialStatus.BatterySOC <= effectiveReserveSOC+reserveFloorTolerancePct {
		hitDeficitAt = timeline[0].startTime
	}
	if initialStatus.BatterySOC >= 100.0-socTargetTolerancePct {
		hitCapacityAt = timeline[0].startTime
	}

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		stepReserveSOC := timeline[i].minSOC + reserveBufferPct
		if i < len(winningPath.states) && i+1 < len(winningPath.states) {
			startSOC := winningPath.states[i].soc
			endSOC := winningPath.states[i+1].soc

			if hitDeficitAt.IsZero() && endSOC <= stepReserveSOC+reserveFloorTolerancePct {
				if startSOC > stepReserveSOC+reserveFloorTolerancePct && startSOC > endSOC {
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

	// Build full schedule for Plan UI
	var periods []types.PlanPeriod
	var totalImportKWH, totalExportKWH, totalCostDollars, totalExportCredits float64

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		action := winningPath.actions[i]
		interval := timeline[i]
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
			if metrics.endingSOC > 0 {
				endState.soc = metrics.endingSOC
			}
		}

		totalImportKWH += metrics.gridImportKWH
		totalExportKWH += metrics.gridExportKWH
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

	horizonHours := int(math.Ceil(timeline[len(timeline)-1].endTime.Sub(timeline[0].startTime).Hours()))

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
	model map[int]TimeProfile,
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
		q3KWH += model[t1.Hour()].P75HomeLoadKWH
		hoursCounted++
	}
	if s2.HomeKWH > 0 {
		recentKWH += s2.HomeKWH
		q3KWH += model[t2.Hour()].P75HomeLoadKWH
		hoursCounted++
	}

	// Also check in-progress current hour if it has already exceeded Q3
	if sCurr.HomeKWH > 0 {
		currQ3 := model[currentHour.Hour()].P75HomeLoadKWH
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
