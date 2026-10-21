package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
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
	defaultInertiaThresholdDollars = 0.025

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

	// chargeCompletionBufferDuration (40 minutes, or 2 standard 20-minute intervals) is the safety buffer
	// window immediately preceding a material import rate increase. Grid charging within this final window
	// incurs a minute penalty (lateChargeBufferPenaltyPerKWH = batteryHoldingCostPerHourPerKWH - 1e-6 per kWh)
	// so the planner targets completing cheap grid charges 20-40 minutes before rates jump rather than waiting
	// until the final interval with zero margin for hardware command delays or constant-voltage top-off tapering.
	chargeCompletionBufferDuration = 40 * time.Minute
	lateChargeBufferPenaltyPerKWH  = batteryHoldingCostPerHourPerKWH - 1e-6

	// planScoreTieEpsilon ($1e-6) is the floating-point tie-breaker tolerance when comparing
	// total horizon plan scores across candidate modes, allowing sub-mill-dollar holding and
	// pre-peak buffer penalties (~$1e-4) to steer timing while breaking true floating-point ties deterministically.
	planScoreTieEpsilon = 1e-6

	// modeSwitchPenalty is a nominal physical transition penalty ($0.005 = 0.5 cents)
	// applied during dynamic programming forward search whenever transitioning between battery modes.
	// This eliminates mode chattering, relay/contactor flutter, and fragmentation across all modes (charging,
	// discharging to load, exporting, or standby), strictly prioritizing contiguous dispatch blocks
	// while still permitting mode switches whenever economically justified.
	modeSwitchPenalty = 0.005

	// solarModeSwitchPenalty ($0.01 = 1.0 cent) is the transition penalty applied when toggling SolarMode
	// (e.g. SolarModeAny <-> SolarModeExport). Because DirectSolarExport shares BatteryModeLoad with normal
	// self-consumption, a $0.01 per-switch penalty ($0.02 round-trip when entering and exiting on minor
	// < $0.03/kWh fluctuations where isSolarPriceDrop is false) prevents short end-of-hour real-time blips
	// from toggling hardware TOU modes for a single poll just to earn a cent, while preserving genuine
	// price spikes and multi-hour solar export windows.
	solarModeSwitchPenalty = 0.01

	// batteryExportReserveMarginPct is the home-load safety buffer (in % SOC) added to forecasted
	// peak home consumption when evaluating whether battery grid export is economical relative to
	// grid imports. The battery can still export down to minSOC, but any energy within this 5% buffer
	// above forecasted peak load is only exported if the export rate clears the grid import replacement cost.
	batteryExportReserveMarginPct = 5.0

	// solarExportReserveMarginPct is the minimum SOC margin (in % SOC) required above the reserve floor
	// to initiate or sustain direct solar export (BatteryModeLoad + SolarModeExport). Because direct solar
	// export routes all solar to the grid while the battery covers household load, requiring a 5% buffer
	// prevents entering solar export when only 1–2% above reserve and immediately hitting the reserve floor.
	solarExportReserveMarginPct = 5.0

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

	// priceMaterialityThresholdDollars ($0.03/kWh, or 3.0¢/kWh) is the minimum price differential required for an economic
	// distinction to be meaningful when evaluating rate jumps, standby holds, and decision explanations (e.g. waiting to charge vs charging now).
	// Differences below 3 cents are typically minor hourly wholesale fluctuations or insignificant tariff riders.
	priceMaterialityThresholdDollars = 0.03

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

	// reserveDeficitChargeTolerancePct is the SOC tolerance (1.05% SOC) below interval.minSOC required
	// before triggering emergency reserve grid top-ups (PlanActionReserveTargetCharge) and hard deficit
	// trajectory pruning. ESS BMS telemetry (e.g. Tesla Powerwall) frequently reports a resting battery
	// 1.0% below its configured reserve floor (such as 19.0% on a 20% reserve or 9.0% on a 10% reserve)
	// due to integer SOC quantization and standby tare. Allowing 1.05% tolerance prevents 1% BMS drift
	// from triggering nuisance grid top-ups (including during peak rates), while still immediately
	// recharging genuine reserve deficits (e.g. <= 18.9% on a 20% reserve).
	reserveDeficitChargeTolerancePct = 1.05

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

	// almostFullSOC is the threshold below which a battery is considered full enough to not meaninginfully need
	// solar refill and also to avoid hold penalty
	almostFullSOC = 95.0

	// minShoulderChargeRefDollarsPerKWH ($0.05/kWh) is the minimum reference grid-charging import rate used when
	// computing the post-horizon shoulder threshold in detectPlanningAnchors. On real-time pricing tariffs (such as
	// ComEd or Ameren), a single near-zero overnight hour (e.g., $0.005/kWh at 03:00) would otherwise pull the raw
	// chargeHurdleThreshold down to ~$0.036/kWh and misclassify ordinary $0.04-$0.07/kWh daytime off-peak rates
	// at T+24h as "elevated" shoulder peaks. Flooring the reference charge rate at $0.05/kWh keeps the post-horizon
	// hurdle at ~$0.086/kWh while still allowing flat-TOU off-peak rates (e.g., $0.08-$0.25/kWh) to scale normally.
	minShoulderChargeRefDollarsPerKWH = 0.05
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
	earliestSolarRefillTime time.Time
	vppEventDeadline        time.Time
	earliestArbitrageTime   time.Time
	vppRechargeDeadline     time.Time
	homeKW                  float64
	stepKW                  float64
	headroomKWH             float64
	refillExportRate        float64
	minSOC                  float64
	higherFutureRate        float64
	roundTripEff            float64
	rechargeCost            float64
	futureExportRate        float64
	futurePeakRate          float64
	solarExportDegradation  float64
	gridChargeDegradation   float64
	minAlternativeValue     float64
	cycleHurdle             float64
	exportReplacementCost   float64
	surplusSolarKWH         float64
	solarRefillSOC          float64
	vppMandatory            bool
	isAlreadyCharging       bool
	hasUpcomingSolarRefill  bool
	hasVPPAhead             bool
	vppBeforeSolarRefill    bool
	isAlreadyStandby        bool
	canDischarge            bool
}

// actionCandidate represents a permissible control action pair for an interval.
type actionCandidate struct {
	batteryMode        types.BatteryMode
	solarMode          types.SolarMode
	reason             types.ActionReason
	description        string
	actionName         PlanActionName
	targetSOC          float64
	overrideReserveSOC float64
	isTruePeak         bool
	futurePrice        *types.Price
}

// precedingAction contains only the action state guaranteed to be known from
// the previous interval (both from lastAction at step 0 and from parent nodes during DP rollout).
type precedingAction struct {
	BatteryMode types.BatteryMode
	SolarMode   types.SolarMode
	ImportRate  float64
	ExportRate  float64
	NoPrice     bool
}

func (c *Controller) toPrecedingAction(action *types.Action, settings types.Settings, timeline []planInterval) precedingAction {
	if action == nil {
		return precedingAction{}
	}
	var importRate, exportRate float64
	if action.CurrentPrice != nil {
		importRate = action.CurrentPrice.ImportRateDollars()
		// ExportRateDollars only inspects allPrices under 1:1 flat net metering (to find the
		// horizon-wide min/max retail rate). Guarding slice allocation here avoids building
		// an unused []types.Price slice on non-flat-NEM calls.
		var allPrices []types.Price
		if settings.UtilityRateOptions.IsFlatNetMetering() && len(timeline) > 0 {
			allPrices = make([]types.Price, len(timeline))
			for i := range timeline {
				if !timeline[i].price.TSStart.IsZero() || !timeline[i].price.TSEnd.IsZero() || timeline[i].price.DollarsPerKWH != 0 || timeline[i].price.GridUseDollarsPerKWH != 0 {
					allPrices[i] = timeline[i].price
				} else {
					allPrices[i] = types.Price{DollarsPerKWH: timeline[i].importRate}
				}
			}
		}
		exportRate = action.CurrentPrice.ExportRateDollars(settings, allPrices)
	}
	return precedingAction{
		BatteryMode: action.BatteryMode,
		SolarMode:   action.SolarMode,
		ImportRate:  importRate,
		ExportRate:  exportRate,
		NoPrice:     action.CurrentPrice == nil,
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

func isSamePrice(importA, exportA, importB, exportB float64) bool {
	return math.Abs(importA-importB) <= priceEpsilonForEquality &&
		math.Abs(exportA-exportB) <= priceEpsilonForEquality
}

// isAbsSignificantDiffPrice returns true when either the import rate or export rate
// differs between A and B by at least priceMaterialityThresholdDollars ($0.03/kWh).
func isAbsSignificantDiffPrice(importA, exportA, importB, exportB float64) bool {
	return math.Abs(importA-importB) >= priceMaterialityThresholdDollars ||
		math.Abs(exportA-exportB) >= priceMaterialityThresholdDollars
}

// isSignificantDiffPrice returns true when either importA exceeds importB or exportA exceeds exportB
// by at least priceMaterialityThresholdDollars ($0.03/kWh).
func isSignificantDiffPrice(importA, exportA, importB, exportB float64) bool {
	return importA-importB >= priceMaterialityThresholdDollars ||
		exportA-exportB >= priceMaterialityThresholdDollars
}

// wearRates holds the battery degradation rates ($/kWh) for DP scoring and the candidate-level
// export spread hurdles ($/kWh) for gating battery exports:
//   - exportLoadHurdle is the physical battery export degradation floor (BatteryExportDegradationDollarsPerKWH),
//     used when evaluating whether to export already-stored battery energy vs saving it for cheaper off-peak
//     home load or refilling from surplus solar (a single discharge cycle that does not add a grid-charge cycle).
//   - exportGatePerKWH is the full grid-charge-to-export spread hurdle (max(BatteryExportDegradation,
//     MinBatteryExportDifferenceDollarsPerKWH)), used when gating grid pre-charging for battery export or
//     exporting below a future reserve floor where the grid must recharge the exported energy.
//   - batteryExportPerKWH is the physical export-half wear rate (BatteryExportDegradation - GridChargeDegradation)
//     applied in stepWearCostDollars on BatteryModeExport, while any grid charging above loadNeededSOC for
//     battery export pays the remaining exportGatePerKWH margin at the charging step.
type wearRates struct {
	gridChargePerKWH    float64
	arbitrageGatePerKWH float64
	solarExportPerKWH   float64
	batteryExportPerKWH float64
	exportLoadHurdle    float64
	exportGatePerKWH    float64
}

func getWearRates(settings types.Settings, optParams types.OptimizationParams) wearRates {
	solarExport := max(priceEpsilonForEquality, optParams.SolarExportDegradationDollarsPerKWH)
	exportLoadHurdle := max(priceEpsilonForEquality, optParams.BatteryExportDegradationDollarsPerKWH)
	effectiveExportHurdle := max(optParams.BatteryExportDegradationDollarsPerKWH, settings.MinBatteryExportDifferenceDollarsPerKWH)
	effectiveArbitrageHurdle := max(optParams.GridChargeDegradationDollarsPerKWH, settings.MinArbitrageDifferenceDollarsPerKWH)
	return wearRates{
		gridChargePerKWH:    max(priceEpsilonForEquality, optParams.GridChargeDegradationDollarsPerKWH),
		arbitrageGatePerKWH: max(priceEpsilonForEquality, effectiveArbitrageHurdle),
		solarExportPerKWH:   solarExport,
		batteryExportPerKWH: max(solarExport, optParams.BatteryExportDegradationDollarsPerKWH-optParams.GridChargeDegradationDollarsPerKWH),
		exportLoadHurdle:    exportLoadHurdle,
		exportGatePerKWH:    max(priceEpsilonForEquality, effectiveExportHurdle),
	}
}

// stepExportFloor holds per-step precomputed lookahead values for DirectSolarExport, BatteryModeExport,
// GridArbitragePreCharge target clamping, and DP intra-bucket energy comparison.
type stepExportFloor struct {
	minAlternativeValue float64
	reqSOC              float64
	peakImportRate      float64
	refillCost          float64
	// neededSOC is the SOC (%) required at interval.endTime to cover downstream
	// expensive loads, reserve step-ups, and export opportunities (net of intervening cheap solar
	// and cheaper/tail grid charging).
	neededSOC float64
	// loadNeededSOC is the SOC (%) required at interval.endTime to cover downstream
	// expensive home loads, reserve step-ups, and peak survival buffers (excluding 100% battery export jumps).
	// Grid charging above loadNeededSOC is for battery export and pays the exportGatePerKWH wear margin.
	loadNeededSOC float64
	// chargeTargetSOC is the continuous targetSOC clamp for PlanActionGridArbitragePreCharge when
	// neededSOC is strictly between interval.minSOC and 100% (0 when unconstrained/100%).
	chargeTargetSOC float64
}

// computeStepExportFloor precomputes the per-step lookahead floors and targets used by candidate
// generation (appendActionCandidates) and DP intra-bucket pruning at stepIdx:
//   - minAlternativeValue: lowest future opportunity/replacement cost of battery energy across the
//     remaining horizon (minimum of knownPostHorizonRate, future import rates, and future surplus
//     solar export rates, or $0 if solar export is disabled), used to gate DirectSolarExport.
//   - reqSOC, peakImportRate, refillCost: ending SOC floor and replacement cost from exportFloorSOC
//     when battery grid export is enabled and the current interval has a positive export rate.
//   - neededSOC and loadNeededSOC: total arbitrage target SOC (including 100% battery export targets)
//     and load-only target SOC (home load, reserve step-ups, and peak survival buffers) from horizonTargetSOC.
//   - chargeTargetSOC: partial targetSOC clamp for PlanActionGridArbitragePreCharge when neededSOC lies
//     strictly between interval.minSOC and 100% (outside socTargetTolerancePct of either bound).
func computeStepExportFloor(
	stepIdx int,
	interval planInterval,
	timeline []planInterval,
	state planState,
	anchors planningAnchors,
	settings types.Settings,
) stepExportFloor {
	minAlt := anchors.knownPostHorizonRate
	if len(timeline) > 0 {
		if minAlt <= 0 {
			minAlt = timeline[stepIdx].importRate
		}
		for i := stepIdx; i < len(timeline); i++ {
			if timeline[i].importRate < minAlt {
				minAlt = timeline[i].importRate
			}
			// When surplus solar is available ahead, refilling the battery only costs the foregone solar
			// export credit (or $0 if solar export is disabled) rather than the retail grid import rate.
			if timeline[i].solarKWH-timeline[i].loadKWH >= minSignificantSolarKW*timeline[i].durationHours {
				solarRefillCost := max(0.0, timeline[i].exportRate)
				if !settings.GridExportSolar {
					solarRefillCost = 0.0
				}
				if solarRefillCost < minAlt {
					minAlt = solarRefillCost
				}
			}
		}
	} else if minAlt <= 0 {
		minAlt = interval.importRate
	}
	var reqSOC, peakImportRate, refillCost float64
	if settings.ManageTOUSchedules && settings.GridExportBatteries && interval.exportRate > 0 {
		reqSOC, peakImportRate, refillCost = exportFloorSOC(stepIdx, timeline, state, settings, minAlt)
	}
	neededSOC, _, _, loadNeededSOC := horizonTargetSOC(stepIdx, timeline, state, anchors, settings, minAlt, true)
	var chargeTargetSOC float64
	// Only set a partial chargeTargetSOC clamp when grid charging is enabled and neededSOC is strictly
	// between the reserve floor and full capacity (outside the 0.5% bucket tolerance of either boundary).
	// When neededSOC <= minSOC, no arbitrage charge above reserve is needed (reserve deficits use
	// PlanActionReserveTargetCharge); when neededSOC >= 100%, chargeTargetSOC remains 0 (unclamped 100%).
	if settings.GridChargeBatteries && !state.chargingDisabled && neededSOC > interval.minSOC+socTargetTolerancePct && neededSOC < 100.0-socTargetTolerancePct {
		chargeTargetSOC = neededSOC
	}
	return stepExportFloor{
		minAlternativeValue: minAlt,
		reqSOC:              reqSOC,
		peakImportRate:      peakImportRate,
		refillCost:          refillCost,
		neededSOC:           neededSOC,
		loadNeededSOC:       loadNeededSOC,
		chargeTargetSOC:     chargeTargetSOC,
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

// postHorizonLoadSlice pairs a forecasted post-horizon net load requirement (kWh)
// with the import rate ($/kWh) of the hour in which it is consumed.
type postHorizonLoadSlice struct {
	loadKWH    float64
	importRate float64
}

// planningAnchors holds fixed operational markers detected across the horizon.
type planningAnchors struct {
	vppEvents                  []vppAnchor
	horizonEnd                 time.Time
	knownPostHorizonRate       float64 // Known rate at H+1 if tariff has a fixed weekly schedule
	minHorizonImportRate       float64
	maxHorizonImportRate       float64
	peakWindows                []peakWindowAnchor
	postHorizonSolarSurplusKWH float64
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
	logData           []candidateLogData
	states            []planState
	metrics           []intervalMetrics
	timeline          []planInterval
	stepFloors        []stepExportFloor
	totalCost         float64
	strikes           int
	initialCandidates []actionCandidate
	modeScores        map[types.BatteryMode]float64
	modeStrikes       map[types.BatteryMode]int
	candScores        map[PlanActionName]float64
	candStrikes       map[PlanActionName]int
	bestScore         float64
	anchors           planningAnchors
}

// logCandidate emits structured slog messages for an action candidate, distinguishing whether
// the candidate was ultimately chosen for execution or evaluated but not chosen.
func logCandidate(ctx context.Context, cand actionCandidate, ld candidateLogData, interval planInterval, state planState, selected bool) {
	if cand.actionName == "" {
		return
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
			slog.Float64("minSOC", ld.minSOC),
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
			slog.Float64("minSOC", ld.minSOC),
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
			slog.Float64("solarExportDegradationDollarsPerKWH", ld.solarExportDegradation),
			slog.Float64("gridChargeDegradationDollarsPerKWH", ld.gridChargeDegradation),
			slog.Time("earliestArbitrageTime", ld.earliestArbitrageTime),
			slog.Time("vppRechargeDeadline", ld.vppRechargeDeadline),
		)
	case PlanActionVPPPreChargeBeforeDeadline:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Time("vppDeadline", ld.vppDeadline),
			slog.Float64("targetSOC", cand.targetSOC),
			slog.Float64("surplusSolarKWH", ld.surplusSolarKWH),
			slog.Float64("solarRefillSOC", ld.solarRefillSOC),
			slog.Float64("importRate", interval.importRate),
			slog.Float64("roundTripEff", ld.roundTripEff),
			slog.Float64("rechargeCost", ld.rechargeCost),
			slog.Float64("futureExportRate", ld.futureExportRate),
			slog.Float64("futurePeakRate", ld.futurePeakRate),
			slog.Float64("solarExportDegradationDollarsPerKWH", ld.solarExportDegradation),
			slog.Float64("gridChargeDegradationDollarsPerKWH", ld.gridChargeDegradation),
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
			slog.Float64("minSOC", ld.minSOC),
			slog.Bool("canDischarge", ld.canDischarge),
			slog.Time("vppRechargeDeadline", ld.vppRechargeDeadline),
		)
	case PlanActionBatteryGridExportDump:
		planLog(ctx, selected, cand.actionName,
			slog.Time("stepTime", interval.startTime),
			slog.Float64("exportRate", interval.exportRate),
			slog.Float64("targetSOC", cand.targetSOC),
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
		var step0LD candidateLogData
		if len(p.logData) > 0 {
			step0LD = p.logData[0]
		}
		for _, cand := range p.initialCandidates {
			isChosen := cand.batteryMode == chosenAction.batteryMode &&
				cand.solarMode == chosenAction.solarMode &&
				cand.actionName == chosenAction.actionName &&
				(math.Abs(cand.targetSOC-chosenAction.targetSOC) < 1e-6 || (cand.batteryMode == types.BatteryModeChargeAny && cand.targetSOC <= 0))
			if isChosen {
				continue
			}
			logCandidate(ctx, cand, step0LD, step0Interval, step0State, false)
			var score float64
			var evaluated bool
			candStrikes := 0
			if p.candScores != nil && cand.actionName != "" {
				score, evaluated = p.candScores[cand.actionName]
				if p.candStrikes != nil {
					candStrikes = p.candStrikes[cand.actionName]
				}
			}
			if !evaluated {
				score, evaluated = p.modeScores[cand.batteryMode]
				if p.modeStrikes != nil {
					candStrikes = p.modeStrikes[cand.batteryMode]
				}
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
				slog.Float64("targetSOC", cand.targetSOC),
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
				math.Abs(act.targetSOC-prev.targetSOC) < 1e-6 {
				continue
			}
		}
		var stepLD candidateLogData
		if i < len(p.logData) {
			stepLD = p.logData[i]
		}
		logCandidate(ctx, act, stepLD, timeline[i], p.states[i], true)
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
	actions := p.actions
	states := p.states
	metrics := p.metrics

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
		maxChargeExportRate := timeline[i].exportRate
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
			if timeline[chargeEndIdx].exportRate > maxChargeExportRate {
				maxChargeExportRate = timeline[chargeEndIdx].exportRate
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
			// Stop if rate increases, export opportunity occurs, a VPP event begins, a new grid charge episode begins,
			// or if surplus solar begins recharging the battery (so exitSOC is measured before net solar refill).
			hasSurplusSolar := timeline[k].solarKWH-timeline[k].loadKWH > minSignificantBatteryPowerKW*timeline[k].durationHours
			if timeline[k].importRate > maxChargeImportRate+priceHourlyVariationToleranceDollars ||
				timeline[k].exportRate > maxChargeExportRate+priceHourlyVariationToleranceDollars ||
				actions[k].batteryMode == types.BatteryModeChargeAny ||
				actions[k].batteryMode == types.BatteryModeExport ||
				actions[k].solarMode == types.SolarModeExport ||
				actions[k].reason == types.ActionReasonVPPActive ||
				hasSurplusSolar {
				break
			}
			windowEndIdx = k
			// Check if there was discharge to net home load (matching Step 2 criteria)
			isDischarging := actions[k].batteryMode == types.BatteryModeLoad &&
				actions[k].reason != types.ActionReasonVPPActive &&
				actions[k].solarMode != types.SolarModeExport &&
				!hasSurplusSolar &&
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
		if !hasSameRateDischarge || exitSOC >= peakSOC-socTargetTolerancePct {
			i = windowEndIdx + 1
			continue
		}

		// Target SOC needed at the end of the charge episode is exitSOC, clamped to the physical
		// reserve floor (minSOC) across the episode and lookahead window without adding ReserveBufferPercent
		// so that the battery can discharge all the way down to minSOC.
		minReserve := timeline[chargeEndIdx].minSOC
		for j := chargeStartIdx; j <= windowEndIdx && j < len(timeline); j++ {
			if timeline[j].minSOC > minReserve {
				minReserve = timeline[j].minSOC
			}
		}
		targetSOCFloat := min(100.0, max(minReserve, exitSOC))
		// Skip refinement if clamping to minReserve leaves the cap at or above peakSOC,
		// meaning no meaningful overcharge reduction is possible.
		if targetSOCFloat >= peakSOC-socTargetTolerancePct {
			i = windowEndIdx + 1
			continue
		}

		if !adjusted {
			actions = make([]actionCandidate, len(p.actions))
			copy(actions, p.actions)
			states = make([]planState, len(p.states))
			copy(states, p.states)
			metrics = make([]intervalMetrics, len(p.metrics))
			copy(metrics, p.metrics)
		}

		// 1. Cap charge intervals to targetSOCFloat
		for j := chargeStartIdx; j <= chargeEndIdx; j++ {
			if actions[j].targetSOC == 0 {
				actions[j].targetSOC = targetSOCFloat
			} else if math.Abs(actions[j].targetSOC-targetSOCFloat) > 1e-6 {
				log.Ctx(ctx).DebugContext(ctx, "encountered non-zero targetSOC during overcharge refinement",
					slog.Time("intervalStart", timeline[j].startTime),
					slog.String("actionName", string(actions[j].actionName)),
					slog.Float64("existingTargetSOC", actions[j].targetSOC),
					slog.Float64("refinedTargetSOC", targetSOCFloat),
				)
				// If existing target was higher than refined target, clamp it down; never raise an existing lower cap
				if actions[j].targetSOC > targetSOCFloat {
					actions[j].targetSOC = targetSOCFloat
				}
			}
		}

		// 2. Convert subsequent same-price net-load intervals to Standby (behind the meter, Standby still uses all solar on home load)
		for j := chargeEndIdx + 1; j <= windowEndIdx && j < len(actions); j++ {
			if actions[j].batteryMode == types.BatteryModeLoad &&
				actions[j].reason != types.ActionReasonVPPActive &&
				actions[j].solarMode != types.SolarModeExport &&
				timeline[j].solarKWH-timeline[j].loadKWH <= minSignificantBatteryPowerKW*timeline[j].durationHours {

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
			// If previous intervals already brought the battery to targetSOCFloat before interval j begins,
			// switch interval j from ChargeAny to Standby. If interval j starts below targetSOCFloat,
			// it remains ChargeAny and stepPhysics clamps the charge to targetSOCFloat.
			//
			// Why j > 0 is required here (do not change to j >= 0):
			// 1. In a full DP search (searchOptimalPlan), if initial SOC is already at or above exitSOC,
			//    the trajectory that simply rests in Standby at step 0 naturally beats ChargeAny -> Load
			//    because it avoids round-trip efficiency losses.
			// 2. Because reserveFloorTolerancePct is 0.75%, states[0].soc can start slightly below
			//    targetSOCFloat while still satisfying states[0].soc >= targetSOCFloat - reserveFloorTolerancePct.
			//    Keeping j > 0 ensures step 0 remains in ChargeAny (with targetSOC clamped to targetSOCFloat)
			//    to finish topping up to the target rather than prematurely flipping step 0 to Standby.
			if j > 0 && j <= chargeEndIdx && actions[j].batteryMode == types.BatteryModeChargeAny &&
				states[j].soc >= targetSOCFloat-reserveFloorTolerancePct {
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
			slog.Float64("overchargeDeltaSOC", peakSOC-targetSOCFloat),
			slog.Float64("refinedTargetSOC", targetSOCFloat),
		)

		i = windowEndIdx + 1
	}

	if !adjusted {
		return p, false
	}

	optParams := settings.GetOptimizationParams()
	wear := getWearRates(settings, optParams)
	var origWearCost, newTotalCost, newWearCost float64
	for j, m := range p.metrics {
		if j < len(p.actions) && j+1 < len(p.states) && j < len(timeline) {
			var loadNeededSOC float64
			if j < len(p.stepFloors) {
				loadNeededSOC = p.stepFloors[j].loadNeededSOC
			}
			origWearCost += stepWearCostDollars(p.actions[j], p.states[j].soc, p.states[j+1].soc, timeline[j].minSOC, loadNeededSOC, m, roundTripEff, wear)
		}
	}
	for j, m := range metrics {
		newTotalCost += m.costDollars
		if j < len(actions) && j+1 < len(states) && j < len(timeline) {
			var loadNeededSOC float64
			if j < len(p.stepFloors) {
				loadNeededSOC = p.stepFloors[j].loadNeededSOC
			}
			newWearCost += stepWearCostDollars(actions[j], states[j].soc, states[j+1].soc, timeline[j].minSOC, loadNeededSOC, m, roundTripEff, wear)
		}
	}
	capacityKWH := states[len(states)-1].capacityKWH
	origTermVal := calculateTerminalValuation(p.states[len(p.states)-1], p.anchors, settings, timeline, capacityKWH, roundTripEff)
	newTermVal := calculateTerminalValuation(states[len(states)-1], p.anchors, settings, timeline, capacityKWH, roundTripEff)
	costSaved := (originalTotalCost + origWearCost + origTermVal) - (newTotalCost + newWearCost + newTermVal)

	// Reject refinement if the updated trajectory (including terminal battery valuation) costs more than the original plan
	if costSaved < -priceEpsilonForEquality {
		log.Ctx(ctx).WarnContext(ctx, "rejecting overcharge refinement because refined cost exceeds original cost",
			slog.Float64("originalTotalCostDollars", originalTotalCost),
			slog.Float64("refinedTotalCostDollars", newTotalCost),
			slog.Float64("originalTerminalValuationDollars", origTermVal),
			slog.Float64("refinedTerminalValuationDollars", newTermVal),
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
	anchors := c.detectPlanningAnchors(timeline, futurePrices, currentStatus, settings, model)

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

	// 2. Convert into discrete intervals across the horizon
	// The horizon covers up to maxPlanningHorizonHours (24h).
	// Intervals that begin before the 24-hour mark are allowed to complete their full,
	// untruncated period without being artificially cut off at 24 hours.
	maxEnd := now.Add(time.Duration(maxPlanningHorizonHours) * time.Hour)

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
		q3LoadKWH := profile.P75HomeLoadKWH * durationHrs
		if len(history) == 0 && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
			q3LoadKWH = currentStatus.HomeKW * durationHrs
		} else if loadKWH <= 0 && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
			q3LoadKWH = currentStatus.HomeKW * durationHrs
		}

		importRate := currentPrice.ImportRateDollars()
		exportRate := currentPrice.ExportRateDollars(settings, allPrices)
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

// detectPlanningAnchors scans the timeline and future prices to locate fixed operational markers
// (VPP deadlines and the post-horizon replacement rate).
func (c *Controller) detectPlanningAnchors(
	timeline []planInterval,
	futurePrices []types.Price,
	currentStatus types.SystemStatus,
	settings types.Settings,
	model []TimeProfile,
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
	anchors.horizonEnd = horizonEnd

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
		// Fallback when no post-horizon future prices exist (in practice rare, because TOU tariffs provide 48h
		// of prices and ComEd/Ameren estimate the next full day in GetFuturePrices).
		// We intentionally use the last timeline interval's rate rather than averaging across the 24h timeline:
		// on a 3-tier TOU tariff where the horizon ends during a peak period (5-10x off-peak), averaging across
		// the day would pull the valuation toward off-peak and cause the planner to dump the battery right before
		// the peak continues.
		// TODO: Approximate post-horizon pricing using site pricing historical data or time-of-day profile
		// once site pricing data storage is optimized.
		anchors.knownPostHorizonRate = timeline[len(timeline)-1].importRate
	}
	// Stored battery energy has a non-negative option value (>= $0.00/kWh) even when post-horizon grid prices
	// are negative, because discharging is optional (the battery can always rest in Standby while the home consumes
	// negative-priced grid power). Allowing a negative valuation rate would penalize stored energy in
	// calculateTerminalValuation and shrink maxAllowedEffCost below minEffCost during DP frontier pruning.
	anchors.knownPostHorizonRate = max(0.0, anchors.knownPostHorizonRate)

	// Calculate forecasted post-horizon net home load slices paired with each hour's import rate
	// for terminal battery valuation. When no historical energy model data is available (nil/empty model or all hours
	// sitting at BuildHourlyEnergyModel's 0.09 kWh empty-history standby floor), fall back to
	// defaultPostHorizonLoadKWH distributed evenly across the post-horizon hours.
	minPeakDiff := priceMaterialityThresholdDollars
	minDeficitDiff := minDeficitPriceSpread(settings)
	baselineImport := max(0.0, minImport)
	wear := getWearRates(settings, optParams)
	rtEff := optParams.RoundTripEfficiency
	effRT := rtEff
	if effRT <= 0 || effRT > 1.0 {
		effRT = 0.90
	}
	oneWayEff := math.Sqrt(effRT)
	shoulderThreshold := baselineImport + minDeficitDiff
	if settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled && timeline[0].importRate > 0 {
		// Floor the reference charge rate at minShoulderChargeRefDollarsPerKWH ($0.05/kWh) so a single near-zero
		// real-time overnight hour (e.g. $0.005/kWh on ComEd) cannot lower shoulderThreshold into normal
		// $0.04-$0.07/kWh daytime off-peak rates.
		effectiveChargeRate := max(minShoulderChargeRefDollarsPerKWH, timeline[0].importRate)
		chargeHurdleThreshold := (effectiveChargeRate / effRT) + max(priceMaterialityThresholdDollars, wear.arbitrageGatePerKWH) - priceEpsilonForEquality
		shoulderThreshold = min(shoulderThreshold, chargeHurdleThreshold)
	}

	hasLoadModel := false
	for _, p := range model {
		if p.AvgHomeLoadKWH > 0.1 {
			hasLoadModel = true
			break
		}
	}
	var postHorizonSolarSurplusKWH float64
	lastHourRate := anchors.knownPostHorizonRate
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
			anchors.postHorizonSlices = append(anchors.postHorizonSlices, postHorizonLoadSlice{
				loadKWH:    netLoadKWH,
				importRate: hourRate,
			})
		}
	}
	anchors.postHorizonSolarSurplusKWH = postHorizonSolarSurplusKWH
	// Sort slices from highest to lowest rate so banked battery energy is valued against the most
	// expensive post-horizon hours first (preserving strict concavity of terminal valuation).
	sort.SliceStable(anchors.postHorizonSlices, func(i, j int) bool {
		return anchors.postHorizonSlices[i].importRate > anchors.postHorizonSlices[j].importRate
	})

	// 3. Detect Peak Pricing Windows in Horizon:
	// A peak pricing window is a contiguous block of intervals where import rates are significantly
	// elevated (clearing the arbitrage hurdle over baselineImport, or baselineImport + minDeficitDiff
	// when negative rates are present).
	//
	// Why baselineImport is floored at 0.0 and negative-min horizons use minDeficitDiff:
	// On real-time tariffs (e.g. ComEd/Ameren), a single overnight negative-price hour (e.g. -$0.03/kWh)
	// would otherwise make every normal daytime off-peak hour ($0.04/kWh >= -$0.03 + $0.05) look "elevated",
	// merging the entire day into a single 18-hour peak window.
	//
	// Why mid-peak (shoulder) tiers and arbitrage-profitable daytime blocks are included:
	// When a tariff steps Super Off-Peak ($0.06) -> Mid-Peak ($0.16) -> Peak ($0.40) -> Mid-Peak ($0.16)
	// (or on weekend 2-tier schedules stepping $0.06 -> $0.16), keeping all arbitrage-profitable elevated
	// periods inside the contiguous window (with endIndex at the end of the elevated block) ensures the
	// soft $0.02/kWh survival cushion is applied at pw.endIndex so the battery plans to survive the entire
	// elevated block until cheap charging rates return.
	if (maxImport-baselineImport >= minDeficitDiff || maxImport >= shoulderThreshold) && len(timeline) > 0 {
		peakThreshold := maxImport - minPeakDiff
		bufferMinutes := optParams.PeakSurvivalBufferMinutes

		inPeak := false
		hasTruePeak := false
		startIdx := 0
		var currentWindowMaxRate float64
		var currentWindowMinRate float64

		for i, it := range timeline {
			isAtHorizonPeak := it.importRate >= peakThreshold
			isElevated := isAtHorizonPeak || it.importRate >= shoulderThreshold

			if isElevated {
				if !inPeak {
					inPeak = true
					hasTruePeak = isAtHorizonPeak
					startIdx = i
					currentWindowMaxRate = it.importRate
					currentWindowMinRate = it.importRate
				} else {
					if isAtHorizonPeak {
						hasTruePeak = true
					}
					if it.importRate > currentWindowMaxRate {
						currentWindowMaxRate = it.importRate
					}
					if it.importRate < currentWindowMinRate {
						currentWindowMinRate = it.importRate
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
							if rate >= peakThreshold || rate >= shoulderThreshold {
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
	if state.capacityKWH <= 0 {
		state.capacityKWH = currentStatus.BatteryCapacityKWH
	}
	floor := computeStepExportFloor(stepIdx, interval, timeline, state, anchors, settings)
	candidates, _ := c.appendActionCandidates(
		make([]actionCandidate, 0, 5),
		ctx,
		stepIdx,
		interval,
		timeline,
		state,
		anchors,
		settings,
		currentStatus,
		history,
		prevAction,
		floor,
	)
	return candidates
}

func (c *Controller) appendActionCandidates(
	candidates []actionCandidate,
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
	precomputedFloor stepExportFloor,
) ([]actionCandidate, candidateLogData) {
	currentSOC := state.soc

	capacityKWH := state.capacityKWH
	if capacityKWH <= 0 {
		capacityKWH = currentStatus.BatteryCapacityKWH
	}
	optParams := settings.GetOptimizationParams()
	roundTripEff := optParams.RoundTripEfficiency
	minSOC := interval.minSOC
	isAboveReserve := currentSOC > minSOC

	ld := candidateLogData{
		minSOC:       minSOC,
		roundTripEff: roundTripEff,
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

	isFlatNEM := settings.UtilityRateOptions.IsFlatNetMetering()

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

	isReserveDeficit := currentSOC < interval.minSOC-reserveDeficitChargeTolerancePct
	requiredHeadroom := calculateStartChargeHeadroomKWH(capacityKWH, maxChargeKW, settings)
	if isAlreadyCharging || isReserveDeficit {
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
	maxReserveKWH := capacityKWH * (100.0 - minSOC) / 100.0
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
		})
		return candidates, ld
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
				targetSOC = minSOC
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
				})
				return candidates, ld
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
				targetSOC:          targetSOC,
				overrideReserveSOC: targetSOC,
				isTruePeak:         isTruePeak,
			})
			return candidates, ld
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
		if vpp.deadline.Before(firmwareTakeover) {
			firmwareTakeover = vpp.deadline
		}
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
				})
				return candidates, ld
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
			})
			if canDirectSolarExport && isAboveReserve {
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeStandby,
					solarMode:   types.SolarModeExport,
					reason:      types.ActionReasonDirectExport,
					description: "Direct Solar Export during VPP prep.",
					actionName:  PlanActionVPP2HourPrepDirectSolarExport,
					isTruePeak:  isTruePeak,
				})
			}
			return candidates, ld
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
	if stepIdx == 0 && IsEVStandbyEligible(settings, interval.startTime) {
		if isEV, stepKW := DetectEVCharging(ctx, currentStatus.HomeKW, history); isEV {
			ld.homeKW = currentStatus.HomeKW
			ld.stepKW = stepKW

			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeStandby,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonEVChargingStandby,
				description: "EV Charging Active. Standby to protect battery.",
				actionName:  PlanActionActiveEVChargingStandby,
				isTruePeak:  isTruePeak,
			})
			return candidates, ld
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
			})
			// If user configured an explicit AlwaysChargeUnder threshold and battery has headroom,
			// enforce charging as the designated action.
			if isForceChargeThreshold {
				return candidates, ld
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
		})
		return candidates, ld
	}

	// --- PRUNING RULE 5: Reserve Floor Protection ---
	// Direct solar export requires a safety margin above the reserve floor, while battery grid export
	// can discharge down to the reserve floor once the peak load buffer hurdle in Branch E is met.
	canBatteryExport := isAboveReserve
	canSolarExport := currentSOC > minSOC+solarExportReserveMarginPct
	roomLeftToFill := currentSOC < almostFullSOC

	// Evaluate daytime solar refill:
	// If daytime solar is expected and export credit clears tonight's import rate adjusted for round-trip efficiency:
	// Holding battery charge tonight to export more solar tomorrow is only justified when exportRate >= importRate / roundTripEff.
	var hasUpcomingSolarRefill bool
	var refillExportRate float64
	var earliestSolarRefillTime time.Time
	if isAboveReserve && roomLeftToFill && settings.GridExportSolar && !isFlatNEM && roundTripEff > 0 {
		for i := stepIdx + 1; i < len(timeline); i++ {
			if timeline[i].solarKWH > minSignificantSolarKW*timeline[i].durationHours &&
				interval.importRate/roundTripEff <= timeline[i].exportRate+priceEpsilonForEquality {
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
		})
	}

	// Branch B: Standby (Preserve battery charge for upcoming peak rates, solar export hold, or export arbitrage)
	// Pruned immediately if current importRate >= maxFutureRate with no upcoming VPP or export opportunity,
	// because there is never an economic incentive to hold battery energy during the highest rate in the horizon.
	var maxFutureRate float64
	var maxFutureExportRate float64
	var hasHigherFutureMinSOC bool
	cumNetSolarKWH := max(0.0, interval.solarKWH-interval.loadKWH) * oneWayEff
	headroomKWH = max(0.0, capacityKWH-state.energyKWH)
	headroomAfterLoadKWH := headroomKWH + max(0.0, interval.loadKWH-interval.solarKWH)/oneWayEff
	hasSolarRefillBeforePeak := false
	for i := stepIdx + 1; i < len(timeline); i++ {
		// If surplus solar before interval i is sufficient to refill the battery to capacity,
		// the battery will enter interval i full from rooftop solar. Standing by now cannot increase
		// energy available at interval i and would needlessly force grid imports now.
		if cumNetSolarKWH > 0 && cumNetSolarKWH >= headroomAfterLoadKWH {
			hasSolarRefillBeforePeak = true
			break
		}
		if timeline[i].importRate > maxFutureRate {
			maxFutureRate = timeline[i].importRate
		}
		if timeline[i].exportRate > maxFutureExportRate {
			maxFutureExportRate = timeline[i].exportRate
		}
		// Offer Standby (hasHigherFutureMinSOC) when a future interval steps up the reserve floor
		// above the current interval's minSOC, the battery's current SOC is at or below that upcoming
		// reserve target (plus profile buffer), and the future interval's post-RTE grid recharge cost
		// (timeline[i].importRate / effRT) is at least as expensive as importing grid power now.
		// Without this, a battery sitting below an upcoming elevated reserve floor during a cheaper or
		// equal-rate interval would discharge for home load now only to be forced to grid-recharge back
		// up to timeline[i].minSOC when the reserve window begins.
		if settings.GridChargeBatteries &&
			timeline[i].minSOC > interval.minSOC+socTargetTolerancePct &&
			currentSOC <= timeline[i].minSOC+optParams.ReserveBufferPercent+socTargetTolerancePct &&
			timeline[i].importRate/effRT >= interval.importRate-priceEpsilonForEquality {
			hasHigherFutureMinSOC = true
		}
		netSolar := max(0.0, timeline[i].solarKWH-timeline[i].loadKWH) * oneWayEff
		cumNetSolarKWH += netSolar
	}
	effectiveMaxFutureRate := maxFutureRate
	if !hasSolarRefillBeforePeak {
		effectiveMaxFutureRate = max(maxFutureRate, anchors.knownPostHorizonRate)
	}
	wear := getWearRates(settings, optParams)

	// hasHigherFutureRate requires canDischarge because Standby only holds existing charge.
	// If the battery is already at or below reserve floor, holding it in Standby cannot save energy
	// for future peak rates (hardware won't allow discharging below reserve anyway) and would
	// needlessly lock the site into buying grid power while resting at reserve.
	hasHigherFutureRate := canDischarge && effectiveMaxFutureRate > interval.importRate+priceMaterialityThresholdDollars
	// hasHigherFutureExport requires canBatteryExport because Standby only holds existing charge (without adding energy).
	// If the battery does not currently have exportable energy above the reserve margin, holding it in Standby
	// would needlessly prevent home self-consumption for an export opportunity it cannot participate in.
	// It also requires maxFutureExportRate to beat interval.importRate by the export spread hurdle (wear.exportGatePerKWH)
	// and exceed interval.exportRate so the battery does not idle in Standby during peak rates or when already in the best export window.
	hasHigherFutureExport := canBatteryExport && settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM &&
		maxFutureExportRate >= interval.importRate+wear.exportGatePerKWH &&
		maxFutureExportRate > interval.exportRate+priceMaterialityThresholdDollars
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
	// peak rates forces peak grid purchases. At future rollout steps (`stepIdx > 0`), when all four forward-looking hold
	// reasons are false, we also only maintain standby if the battery wouldn't discharge to cover net home load anyway
	// (`!canDischarge || interval.loadKWH <= interval.solarKWH`), preventing an overnight hold from staying locked in
	// Standby all day when a weekend/moderate TOU spread is below minPeakRateSpreadDollars.
	canMaintainStandby := isAlreadyStandby && !isTruePeak && !hasSolarRefillBeforePeak && (stepIdx == 0 || interval.loadKWH <= interval.solarKWH)
	shouldOfferStandby := canDischarge && (len(timeline) == 0 || hasHigherFutureRate || hasHigherFutureExport || hasUpcomingSolarRefill || hasVPPAhead || hasHigherFutureMinSOC || canMaintainStandby)
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
		} else if hasHigherFutureMinSOC {
			reason = types.ActionReasonDeficitSaveForPeak
			desc = "Preserving battery in standby for upcoming reserve window."
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
				// electrochemically; round-trip conversion losses plus the configured export spread hurdle (wear.exportGatePerKWH)
				// must be overcome to guarantee the homeowner achieves the configured profit margin.
				// Note: canExport is intentionally not checked here because current SOC may be low/empty right now;
				// the purpose of pre-charging is to add energy so that canExport becomes true when reaching step i.
				canBatteryExportAhead := settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM && canCompleteBeforeVPPRecharge
				if canBatteryExportAhead && timeline[i].exportRate-rechargeCost >= wear.exportGatePerKWH {
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
					timeline[i].exportRate-interval.importRate >= max(wear.arbitrageGatePerKWH, wear.solarExportPerKWH)
				if canSolarExportAhead {
					futureExportRate = timeline[i].exportRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonArbitrageChargeExport
					chargeDesc = "Pre-charging for upcoming solar export arbitrage."
					earliestArbitrageTime = timeline[i].startTime
					break
				}

				// 3. Peak import rate arbitrage: charging now avoids buying expensive peak grid power later
				if timeline[i].importRate-rechargeCost >= wear.arbitrageGatePerKWH {
					futurePeakRate = timeline[i].importRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonDeficitChargeNow
					chargeDesc = "Pre-charging for upcoming peak rates."
					earliestArbitrageTime = timeline[i].startTime
					break
				}
			}
			if stepIdx > 0 && !hasArbitrageAhead && !hasSolarRefillBeforePeak && nearestMandatoryVPP == nil &&
				anchors.knownPostHorizonRate-rechargeCost >= wear.arbitrageGatePerKWH {
				futurePeakRate = anchors.knownPostHorizonRate
				hasArbitrageAhead = true
				chargeReason = types.ActionReasonDeficitChargeNow
				chargeDesc = "Pre-charging for upcoming peak rates."
				earliestArbitrageTime = timeline[len(timeline)-1].endTime
			}

			// Target reserve recharge: if currentSOC is below the interval's target reserve (e.g. during
			// a scheduled off-peak charging window or when the reserve floor steps up), offer a
			// ChargeAny candidate capped at the configured reserve floor so the DP can restore reserve
			// without being forced to overcharge for the full 20-minute interval when solar or a cheaper
			// window will fill the rest.
			// Only emit PlanActionReserveTargetCharge when its cap actually binds within this interval
			// (willExceedReserveBucket) or when no unconstrained arbitrage/VPP charge candidate is offered;
			// otherwise it is physically identical to the unconstrained charge in this step and would collide
			// in the same DP bucket.
			hasReserveCharge := currentSOC < interval.minSOC-reserveDeficitChargeTolerancePct
			reserveTargetSOC := interval.minSOC
			maxChargeKW := resolveBatteryPowerKW(state.maxChargeKW, capacityKWH)
			unclampedEndSOC := currentSOC
			solarOnlyEndSOC := currentSOC
			if capacityKWH > 0 {
				unclampedEndSOC += ((maxChargeKW * interval.durationHours * eff) / capacityKWH) * 100.0
				solarSurplusKWH := max(0.0, interval.solarKWH-interval.loadKWH)
				solarChargeKWH := min(solarSurplusKWH, maxChargeKW*interval.durationHours)
				solarOnlyEndSOC = min(100.0, currentSOC+((solarChargeKWH*eff)/capacityKWH)*100.0)
			}
			// searchOptimalPlan discretizes battery SOC into 0.5% buckets (201 buckets from 0.0% to 100.0%)
			// by multiplying SOC by 2.0 and rounding to the nearest integer: int(math.Round(soc * 2.0)).
			// Compare the rounded 2x bucket indices of unclampedEndSOC and reserveTargetSOC so we only emit
			// a separate reserve-clamped candidate (PlanActionReserveTargetCharge) when an unclamped full-interval
			// charge would advance into a strictly higher 0.5% DP bucket than the reserve target; otherwise both
			// candidates would land in the exact same DP bucket at the end of this step.
			willExceedReserveBucket := int(math.Round(unclampedEndSOC*2.0)) > int(math.Round(reserveTargetSOC*2.0))
			hasReserveCandidate := false
			if hasReserveCharge && currentSOC < reserveTargetSOC && (willExceedReserveBucket || (!hasArbitrageAhead && !hasVPPAhead)) {
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   defaultSolarMode,
					reason:      types.ActionReasonDeficitChargeNow,
					description: "Charging battery to target reserve.",
					actionName:  PlanActionReserveTargetCharge,
					targetSOC:   reserveTargetSOC,
					isTruePeak:  isTruePeak,
				})
				hasReserveCandidate = true
			}

			// When both an upcoming VPP event and an upcoming arbitrage/peak rate exist, consolidate
			// into a single arbitrage/VPP BatteryModeChargeAny candidate to prevent redundant state-space expansion.
			if hasArbitrageAhead || hasVPPAhead {
				selectedReason := types.ActionReasonVPPPrep
				selectedDesc := "VPP Pre-charging before deadline."
				selectedTargetSOC := 0.0

				if hasArbitrageAhead && (!hasVPPAhead || earliestArbitrageTime.Before(nearestVPPRechargeDeadline)) {
					selectedReason = chargeReason
					selectedDesc = chargeDesc
				}

				var actionName PlanActionName
				if selectedReason == chargeReason {
					actionName = PlanActionGridArbitragePreCharge
					// When pre-charging for peak load coverage (DeficitChargeNow without VPP ahead) during a
					// non-surplus-solar interval, clamp targetSOC to the precomputed chargeTargetSOC so stepPhysics
					// stops charging at the exact needed SOC rather than overcharging a full 20-minute step.
					// Daytime surplus-solar intervals (solarKWH > loadKWH) are excluded because horizonTargetSOC
					// already subtracts their solar refill gain from earlier off-peak charging targets; applying a
					// partial clamp inside the solar window would let the DP defer pre-sunrise grid charging into daytime.
					if selectedReason == types.ActionReasonDeficitChargeNow && !hasVPPAhead && interval.solarKWH <= interval.loadKWH && precomputedFloor.chargeTargetSOC > 0 && precomputedFloor.chargeTargetSOC < 100 {
						selectedTargetSOC = precomputedFloor.chargeTargetSOC
					}
					ld.rechargeCost = rechargeCost
					ld.futureExportRate = futureExportRate
					ld.futurePeakRate = futurePeakRate
					ld.solarExportDegradation = wear.solarExportPerKWH
					ld.gridChargeDegradation = wear.gridChargePerKWH
					ld.earliestArbitrageTime = earliestArbitrageTime
				} else {
					actionName = PlanActionVPPPreChargeBeforeDeadline
					if nearestVPP != nil {
						ld.vppDeadline = nearestVPP.deadline
					}
					ld.rechargeCost = rechargeCost
					ld.futureExportRate = futureExportRate
					ld.futurePeakRate = futurePeakRate
					ld.solarExportDegradation = wear.solarExportPerKWH
					ld.gridChargeDegradation = wear.gridChargePerKWH
					ld.earliestArbitrageTime = earliestArbitrageTime
				}

				// VPP Solar Headroom Candidate:
				// When pre-charging ahead of a VPP deadline, check if daytime rooftop solar will provide
				// net surplus energy before the event deadline.
				//
				// Why this belongs in Candidate Generation rather than post-processing refinement:
				// 1. Target SOC is an absolute battery charge ceiling, not an incremental delta. Setting a target
				//    (e.g., 56%) across multi-hour charging windows charges the battery until it reaches
				//    56% and then draws 0 kW of grid charge, letting incoming solar fill the rest.
				// 2. Setting selectedTargetSOC to the solar headroom target allows the DP optimizer to simulate
				//    charging up to the exact headroom ceiling. If free solar fills the remaining capacity,
				//    the battery reaches 100% by the VPP deadline with 0 shortfall strikes and minimal grid import costs.
				// 3. This keeps candidate evaluation native to the DP forward search, avoiding brittle
				//    post-hoc plan surgery, duplicate ChargeAny candidates, and redundant simulation passes.
				// 4. Why we set selectedTargetSOC = solarTargetSOC rather than 0 (and do not add a duplicate targetSOC: 0 candidate):
				//    - In early charging intervals, both candidates charge at maxChargeKW and reach the exact same SOC after
				//      20 minutes, colliding in the same DP bucket (nextBuckets[mIdx][b]).
				//    - In the interval that crosses solarTargetSOC, stepPhysics only clamps grid charging when action.targetSOC > 0;
				//      a targetSOC: 0 candidate cannot stop mid-interval and would overcharge from the grid, wasting solar headroom.
				//    - If an arbitrage peak intervenes before the VPP deadline, earliestArbitrageTime.Before(nearestVPPRechargeDeadline)
				//      (above) already routes the candidate to PlanActionGridArbitragePreCharge with selectedTargetSOC = 0 (100%).
				if actionName == PlanActionVPPPreChargeBeforeDeadline && nearestVPP != nil && capacityKWH > 0 {
					var surplusSolarKWH float64
					for k := stepIdx; k < len(timeline); k++ {
						if !timeline[k].startTime.Before(nearestVPP.deadline) {
							break
						}
						surplus := max(0.0, timeline[k].solarKWH-timeline[k].loadKWH)
						if surplus > 0 {
							stepCapKW := resolveBatteryPowerKW(state.maxChargeKW, capacityKWH)
							surplusSolarKWH += min(surplus, stepCapKW*timeline[k].durationHours)
						}
					}
					solarRefillSOC := (surplusSolarKWH * eff / capacityKWH) * 100.0
					ld.surplusSolarKWH = surplusSolarKWH
					ld.solarRefillSOC = solarRefillSOC
					if solarRefillSOC >= 5.0 {
						// The target entering SOC ahead of any VPP event is full capacity (100%),
						// ensuring maximum energy is banked to export during the event down to vppSoc.
						targetVPPSOC := vppMinFullCapacitySOC
						minReserve := interval.minSOC
						solarTargetSOC := min(100.0, max(minReserve, targetVPPSOC-solarRefillSOC))
						if solarTargetSOC < targetVPPSOC-socTargetTolerancePct {
							selectedTargetSOC = solarTargetSOC
						}
					}
				}

				canExceedReserve := !hasReserveCandidate || selectedTargetSOC == 0 || selectedTargetSOC > reserveTargetSOC+socTargetTolerancePct
				// For PlanActionGridArbitragePreCharge with a clamped targetSOC, require solarOnlyEndSOC to remain
				// at least socTargetTolerancePct below selectedTargetSOC so floating-point rounding dust at the clamp
				// boundary or interval solar cannot emit a 0-kWh grid-charge candidate.
				if canExceedReserve && (selectedTargetSOC == 0 || (currentSOC < selectedTargetSOC-1e-6 && (actionName != PlanActionGridArbitragePreCharge || solarOnlyEndSOC < selectedTargetSOC-socTargetTolerancePct))) {
					candidates = append(candidates, actionCandidate{
						batteryMode: types.BatteryModeChargeAny,
						solarMode:   defaultSolarMode,
						reason:      selectedReason,
						description: selectedDesc,
						actionName:  actionName,
						targetSOC:   selectedTargetSOC,
						isTruePeak:  isTruePeak,
					})
				}
			}
		}
	}

	// Lowest possible alternative value of stored battery energy (avoided load or replacement)
	// and replacement recharge cost accounting for round-trip efficiency, precomputed once per stepIdx.
	floor := precomputedFloor
	minAlternativeValue := floor.minAlternativeValue

	// Branch D: Direct Solar Export (with BatteryModeLoad)
	// Requires canSolarExport (currentSOC > minSOC + solarExportReserveMarginPct) so the battery has enough
	// cushion above reserve to cover home load without immediately hitting the reserve floor.
	//
	// Direct Solar Export exports rooftop solar directly through the inverter to the grid (without passing through
	// the battery electrochemically) while discharging the battery to cover daytime home load. Evaluated on the
	// nominal opportunity cost of stored energy (minAlternativeValue) plus solarExportDegradation because the
	// battery is only discharged once to cover home load (just as it would be during off-peak self-consumption),
	// whereas dividing minAlternativeValue by effRT here would double-count round-trip conversion losses.
	if canDirectSolarExport && canSolarExport && beforeVPPRechargeDeadline && interval.exportRate >= (minAlternativeValue+wear.solarExportPerKWH) {
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeExport,
			reason:      types.ActionReasonDirectExport,
			description: "Direct Solar Export: battery covers load; solar exports.",
			actionName:  PlanActionDirectSolarExport,
			isTruePeak:  isTruePeak,
		})
	}

	// Branch E: Battery Grid Export Dump (Opportunity cost of home offset + degradation hurdle)
	if settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM && canBatteryExport && beforeVPPRechargeDeadline && interval.exportRate > 0 {
		cycleHurdle := wear.exportGatePerKWH
		loadExportHurdle := min(cycleHurdle, wear.exportLoadHurdle)

		var step0NetLoadSOC float64
		if capacityKWH > 0 && eff > 0 {
			step0NetLoadKWH := max(0.0, interval.loadKWH-interval.solarKWH)
			step0NetLoadSOC = ((step0NetLoadKWH / eff) / capacityKWH) * 100.0
		}
		unbufferedStartFloorSOC := min(100.0, max(minSOC, floor.reqSOC)+step0NetLoadSOC)
		samePriceAsPrev := isSamePrice(prevAction.ImportRate, prevAction.ExportRate, interval.importRate, interval.exportRate)
		bufferedPeakReserveSOC := unbufferedStartFloorSOC + batteryExportReserveMarginPct
		if samePriceAsPrev && prevAction.BatteryMode == types.BatteryModeExport {
			bufferedPeakReserveSOC = unbufferedStartFloorSOC
		}
		effectiveRechargeCost := floor.refillCost
		if currentSOC <= bufferedPeakReserveSOC {
			effectiveRechargeCost = max(floor.refillCost, floor.peakImportRate/effRT)
		}

		// Export is a viable candidate if export rate beats the replacement cost by at least the physical export hurdle
		// (note: pure grid-refill replacements include gridRefillExtraHurdle in floor.refillCost so they still require cycleHurdle).
		if interval.exportRate >= (effectiveRechargeCost + loadExportHurdle) {
			targetSOC := max(minSOC, floor.reqSOC)
			// Require at least socTargetTolerancePct of exportable surplus above targetSOC + step0NetLoadSOC
			// so floating-point dust at the export floor boundary cannot emit a zero-export BatteryModeExport candidate.
			if currentSOC > targetSOC+step0NetLoadSOC+socTargetTolerancePct || (math.Abs(targetSOC-minSOC) <= 1e-6 && interval.exportRate >= (floor.peakImportRate/effRT)+cycleHurdle) {
				ld.exportReplacementCost = effectiveRechargeCost
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
				})
			}
		}
	}

	return candidates, ld
}

// exportFloorSOC scans forward to the first interval where the grid import rate is cheap enough to
// profitably replace exported energy (when grid charging is enabled), then works backwards to stepIdx+1
// to compute the required ending SOC floor (reqSOC) at interval.endTime that preserves coverage for
// intervening non-profitable home loads and reserve floor step-ups (offset by any intervening cheap
// surplus solar), along with the highest intervening non-profitable import rate (peakImportRate) and
// the post-RTE replacement cost per kWh (refillCost) of the refill source.
func exportFloorSOC(
	stepIdx int,
	timeline []planInterval,
	state planState,
	settings types.Settings,
	fallbackAlternativeRate float64,
) (reqSOC float64, peakImportRate float64, refillCost float64) {
	reqSOC, peakImportRate, refillCost, _ = horizonTargetSOC(stepIdx, timeline, state, planningAnchors{}, settings, fallbackAlternativeRate, false)
	return reqSOC, peakImportRate, refillCost
}

// horizonTargetSOC computes both the overall required ending SOC (reqSOC) and the home-load-only
// required ending SOC (loadReqSOC, excluding 100% battery export jumps) at interval.endTime using a shared
// forward and backward pass across the remaining horizon:
//   - When forGridCharge is false (called by exportFloorSOC), it finds the first future interval cheap
//     enough to profitably replace exported energy from the grid (using cycleHurdle = exportGatePerKWH) and
//     walks backward from there to reserve battery SOC for intervening non-profitable loads and reserve step-ups
//     (using loadExportHurdle = exportLoadHurdle for already-stored energy displacing cheaper home load or solar).
//   - When forGridCharge is true (called by computeStepExportFloor), it walks backward across the full
//     horizon to determine how much SOC must be reached by interval.endTime to cover upcoming expensive
//     loads, peak survival buffers, and profitable battery/solar export windows, subtracting intervening
//     cheap surplus solar and cheaper/tail grid-charging capacity.
func horizonTargetSOC(
	stepIdx int,
	timeline []planInterval,
	state planState,
	anchors planningAnchors,
	settings types.Settings,
	fallbackAlternativeRate float64,
	forGridCharge bool,
) (reqSOC float64, peakImportRate float64, refillCost float64, loadReqSOC float64) {
	var interval planInterval
	if stepIdx >= 0 && stepIdx < len(timeline) {
		interval = timeline[stepIdx]
	}
	peakImportRate = interval.importRate
	reqSOC = interval.minSOC
	loadReqSOC = interval.minSOC
	capacityKWH := state.capacityKWH
	optParams := settings.GetOptimizationParams()
	effRT := optParams.RoundTripEfficiency
	if effRT <= 0 || effRT > 1.0 {
		effRT = 0.90
	}
	oneWayEff := math.Sqrt(effRT)
	wear := getWearRates(settings, optParams)
	cycleHurdle := wear.exportGatePerKWH
	if forGridCharge {
		cycleHurdle = wear.arbitrageGatePerKWH
	}
	gridChargePerKWH := wear.gridChargePerKWH
	loadExportHurdle := min(cycleHurdle, wear.exportLoadHurdle)
	gridRefillExtraHurdle := max(0.0, cycleHurdle-loadExportHurdle)
	if effRT > 0 {
		refillCost = (fallbackAlternativeRate / effRT) + gridRefillExtraHurdle
	}
	if len(timeline) == 0 || capacityKWH <= 0 || oneWayEff <= 0 || effRT <= 0 {
		return reqSOC, peakImportRate, refillCost, loadReqSOC
	}
	if forGridCharge && interval.importRate <= 0 {
		return 100.0, peakImportRate, refillCost, 100.0
	}

	canGridCharge := settings.GridChargeBatteries && !state.chargingDisabled
	maxChargeKW := resolveBatteryPowerKW(state.maxChargeKW, capacityKWH)
	isFlatNEM := settings.UtilityRateOptions.IsFlatNetMetering()
	maxCheapImportRate := (interval.exportRate - cycleHurdle + priceEpsilonForEquality) * effRT
	maxCheapLoadImportRate := (interval.exportRate - loadExportHurdle + priceEpsilonForEquality) * effRT
	if forGridCharge {
		maxCheapImportRate = (interval.importRate / effRT) + cycleHurdle - priceEpsilonForEquality
		maxCheapLoadImportRate = maxCheapImportRate
	}

	// 1. Forward pass: find the first future interval cheap enough to profitably recharge from the grid
	// (stopping only when grid charging is enabled), and track the highest non-profitable import rate.
	gridRefillIdx := len(timeline)
	lastExpensiveIdx := stepIdx
	minGridImportRate := math.MaxFloat64
	for i := stepIdx + 1; i < len(timeline); i++ {
		if timeline[i].importRate < minGridImportRate {
			minGridImportRate = timeline[i].importRate
		}
		if timeline[i].importRate <= maxCheapImportRate {
			if !forGridCharge && canGridCharge {
				gridRefillIdx = i
				break
			}
			if forGridCharge {
				hasBatteryExport := settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM &&
					timeline[i].exportRate-(interval.importRate/effRT) >= wear.exportGatePerKWH
				hasSolarExport := settings.GridExportSolar && !isFlatNEM &&
					timeline[i].solarKWH > minSignificantSolarKW*timeline[i].durationHours &&
					timeline[i].exportRate-interval.importRate >= max(cycleHurdle, wear.solarExportPerKWH)-priceEpsilonForEquality
				// Even when timeline[i].importRate is cheap (<= maxCheapImportRate), an interval with a
				// profitable battery or solar export opportunity requires pre-charged battery energy (either
				// 100% SOC for BatteryModeExport or load coverage for DirectSolarExport). In the backward pass
				// below, same-price intervals at i > lastExpensiveIdx are treated as post-peak tail recharge
				// windows whose grid-charging capacity is subtracted from reqSOC. Advancing lastExpensiveIdx
				// across profitable export intervals ensures earlier cheap intervals do not subtract charging
				// capacity needed before (or during) the export window.
				if hasBatteryExport || hasSolarExport {
					lastExpensiveIdx = i
				}
			}
		} else {
			lastExpensiveIdx = i
			if timeline[i].importRate > peakImportRate {
				peakImportRate = timeline[i].importRate
			}
		}
	}

	// Seed refillCost from the grid refill interval (or fallback/peak when no in-horizon grid refill exists).
	// When gridRefillIdx == len(timeline), include gridRefillExtraHurdle so pure grid-recharge replacements
	// in Branch E still require the full cycleHurdle (exportGatePerKWH) rather than only loadExportHurdle.
	if gridRefillIdx < len(timeline) {
		refillCost = timeline[gridRefillIdx].importRate / effRT
	} else if stepIdx+1 < len(timeline) {
		if canGridCharge && minGridImportRate < math.MaxFloat64 {
			if fallbackAlternativeRate > 0 {
				refillCost = (min(minGridImportRate, fallbackAlternativeRate) / effRT) + gridRefillExtraHurdle
			} else {
				refillCost = (minGridImportRate / effRT) + gridRefillExtraHurdle
			}
		} else if !canGridCharge {
			refillCost = peakImportRate / effRT
		}
	}

	if forGridCharge {
		// Seed end-of-horizon SOC from the terminal reserve floor, or 100% if the post-horizon rate
		// clears the arbitrage hurdle over the current charging rate.
		reqSOC = max(settings.MinBatterySOC, timeline[len(timeline)-1].minSOC)
		if anchors.knownPostHorizonRate*terminalTier1DiscountFactor > maxCheapImportRate {
			reqSOC = 100.0
		}
		loadReqSOC = reqSOC
	}

	// 2. Backward pass: walk from just before gridRefillIdx back to stepIdx+1 to accumulate the SOC
	// needed at interval.endTime to cover expensive loads and reserve step-ups before the refill.
	minReservedImportRate := math.MaxFloat64
	for i := gridRefillIdx - 1; i > stepIdx; i-- {
		stepFloorSOC := timeline[i].minSOC
		if forGridCharge {
			// Include the peak survival buffer target at the end of each peak window when the buffer
			// penalty + post-peak value clears the current charging cost + hurdle.
			for _, pw := range anchors.peakWindows {
				if i == pw.endIndex && pw.bufferEnergyKWH > 0 {
					postPeakRate := anchors.knownPostHorizonRate
					if pw.endIndex+1 < len(timeline) {
						postPeakRate = timeline[pw.endIndex+1].importRate
					}
					if peakSurvivalBufferShortfallPenaltyDollarsPerKWH+oneWayEff*postPeakRate >= (interval.importRate/oneWayEff)+oneWayEff*cycleHurdle {
						peakFloorSOC := min(50.0, timeline[i].minSOC+(pw.bufferEnergyKWH/capacityKWH)*100.0)
						stepFloorSOC = max(stepFloorSOC, peakFloorSOC)
					}
				}
			}
			// If a future interval can profitably export battery energy to the grid, target full capacity (100%)
			// in reqSOC (while keeping loadReqSOC restricted to home-load / reserve coverage).
			isBatteryExportStep := settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM &&
				timeline[i].exportRate-(interval.importRate/effRT) >= wear.exportGatePerKWH
			if isBatteryExportStep {
				reqSOC = 100.0
			}
			// Subtract grid-charging capacity available in strictly cheaper future intervals, or in same-price
			// intervals after the last expensive interval (so terminal/post-horizon charging is assigned to the
			// tail of the horizon rather than forcing earlier charging windows to 100%).
			if !isBatteryExportStep && canGridCharge && ((i > lastExpensiveIdx && timeline[i].importRate <= interval.importRate+priceEpsilonForEquality) || timeline[i].importRate < interval.importRate-priceMaterialityThresholdDollars) {
				gridGainSOC := ((maxChargeKW * timeline[i].durationHours * oneWayEff) / capacityKWH) * 100.0
				reqSOC = max(stepFloorSOC, reqSOC-gridGainSOC)
				loadReqSOC = max(stepFloorSOC, loadReqSOC-gridGainSOC)
				continue
			}
		}
		netLoadKWH := timeline[i].loadKWH - timeline[i].solarKWH
		if netLoadKWH >= 0 {
			// Cheap-import interval: profitable to export now and buy grid power for this load later,
			// so do not reserve energy for its load (only enforce its reserve floor).
			if timeline[i].importRate <= maxCheapLoadImportRate {
				reqSOC = max(stepFloorSOC, reqSOC)
				loadReqSOC = max(stepFloorSOC, loadReqSOC)
				if canGridCharge && netLoadKWH > 0 {
					refillCost = min(refillCost, timeline[i].importRate/effRT)
				} else if !canGridCharge && netLoadKWH > 0 {
					refillCost = min(refillCost, timeline[i].importRate)
				}
			} else {
				// Expensive-import interval: reserve enough battery SOC to cover this interval's net load.
				netLoadSOC := ((netLoadKWH / oneWayEff) / capacityKWH) * 100.0
				reqSOC = min(100.0, max(stepFloorSOC, reqSOC)+netLoadSOC)
				loadReqSOC = min(100.0, max(stepFloorSOC, loadReqSOC)+netLoadSOC)
				if netLoadKWH > 0 && timeline[i].importRate < minReservedImportRate {
					minReservedImportRate = timeline[i].importRate
				}
			}
		} else {
			// Surplus-solar interval: if refilling from solar beats the current export/charge rate,
			// future solar will replenish later loads/reserves so less SOC needs to be held now.
			solarRefillCost := max(0.0, timeline[i].exportRate)
			if !settings.GridExportSolar {
				solarRefillCost = 0.0
			}
			// Solar charging pays no wear in stepWearCostDollars, so add gridChargePerKWH here so that
			// exporting battery energy and refilling from solar accounts for the full cycle's wear.
			solarReplacementCost := (solarRefillCost / effRT) + gridChargePerKWH
			isCheapSolar := solarReplacementCost+loadExportHurdle <= interval.exportRate+priceEpsilonForEquality
			if forGridCharge {
				isCheapSolar = !settings.GridExportSolar || isFlatNEM ||
					timeline[i].solarKWH <= minSignificantSolarKW*timeline[i].durationHours ||
					timeline[i].exportRate-interval.importRate < max(cycleHurdle, wear.solarExportPerKWH)-priceEpsilonForEquality
			}
			if isCheapSolar {
				surplusKWH := min(maxChargeKW*timeline[i].durationHours, -netLoadKWH)
				solarGainSOC := ((surplusKWH * oneWayEff) / capacityKWH) * 100.0
				if reqSOC-solarGainSOC <= stepFloorSOC {
					reqSOC = stepFloorSOC
					minReservedImportRate = math.MaxFloat64
				} else {
					reqSOC -= solarGainSOC
				}
				if loadReqSOC-solarGainSOC <= stepFloorSOC {
					loadReqSOC = stepFloorSOC
				} else {
					loadReqSOC -= solarGainSOC
				}
				refillCost = min(refillCost, solarReplacementCost)
			} else if forGridCharge {
				if settings.ManageTOUSchedules {
					// When ManageTOUSchedules is enabled, DirectSolarExport (SolarModeExport) exports all rooftop
					// solar directly to the grid while discharging the battery to cover timeline[i].loadKWH, so
					// pre-charging only needs to cover timeline[i].loadKWH above stepFloorSOC (minSOC).
					loadSOC := ((timeline[i].loadKWH / oneWayEff) / capacityKWH) * 100.0
					reqSOC = min(100.0, max(stepFloorSOC, reqSOC)+loadSOC)
					loadReqSOC = min(100.0, max(stepFloorSOC, loadReqSOC)+loadSOC)
				} else {
					reqSOC = 100.0
					loadReqSOC = 100.0
				}
			} else if settings.GridExportSolar && timeline[i].exportRate >= minReservedImportRate*effRT {
				// High-export solar interval: when exporting rooftop solar directly to the grid at timeline[i].exportRate
				// beats storing solar (after round-trip efficiency effRT) for a reserved downstream load (minReservedImportRate*effRT),
				// DirectSolarExport (SolarModeExport) will export all solar while discharging the battery to cover timeline[i].loadKWH.
				loadSOC := ((timeline[i].loadKWH / oneWayEff) / capacityKWH) * 100.0
				reqSOC = min(100.0, max(stepFloorSOC, reqSOC)+loadSOC)
				loadReqSOC = min(100.0, max(stepFloorSOC, loadReqSOC)+loadSOC)
			} else {
				reqSOC = max(stepFloorSOC, reqSOC)
				loadReqSOC = max(stepFloorSOC, loadReqSOC)
			}
		}
	}
	reqSOC = max(interval.minSOC, reqSOC)
	loadReqSOC = max(interval.minSOC, loadReqSOC)
	return reqSOC, peakImportRate, refillCost, loadReqSOC
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

	minSOC := interval.minSOC
	if math.IsNaN(minSOC) || minSOC < 0 {
		minSOC = 20.0
	} else if minSOC > 100.0 {
		minSOC = 100.0
	}
	reserveKWH := capacityKWH * (minSOC / 100.0)

	// Half efficiency for one-way conversions: sqrt(roundTripEfficiency)
	rtEff := roundTripEfficiency
	if math.IsNaN(rtEff) || rtEff <= 0 || rtEff > 1.0 {
		rtEff = 0.85
	}
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

		exportFloorKWH := effectiveLoadReserveKWH
		if action.targetSOC > 0 {
			exportFloorKWH = max(exportFloorKWH, capacityKWH*(action.targetSOC/100.0))
		}
		usableExportEnergy := max(0.0, newEnergy-exportFloorKWH)
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
// Note: logData and metric are intentionally stored directly on each dpNode rather than
// re-running appendActionCandidates or stepPhysics on the winning trajectory after the search.
// We do not want to force candidate generation or physics simulation to be unnecessarily
// perfectly deterministic, and we need the log data and metrics to come from the actual
// actions that were generated during the search to guarantee accuracy for debugging purposes.
type dpNode struct {
	state     planState
	totalCost float64
	strikes   int
	action    actionCandidate
	logData   candidateLogData
	metric    intervalMetrics
	parent    *dpNode
}

var dpNodeSlabPool = sync.Pool{
	New: func() any {
		return new([512]dpNode)
	},
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

// modeIndex maps a candidate's BatteryMode and SolarMode to an integer slot (0..4) for mode-isolated DP buckets.
func modeIndex(mode types.BatteryMode, solarMode types.SolarMode) int {
	// Direct solar export pairs with BatteryModeLoad but carries its own transition penalties and
	// continuation rules at the next step, and can land in the same 0.5% SOC bucket as SolarModeAny
	// during low-solar intervals; isolating it prevents the immediate export credit from overwriting
	// a trajectory that absorbs solar for a later peak.
	if solarMode == types.SolarModeExport {
		return 4
	}
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
	lastPreceding := c.toPrecedingAction(lastAction, settings, timeline)

	optParams := settings.GetOptimizationParams()
	roundTripEff := optParams.RoundTripEfficiency
	effRT := roundTripEff
	if effRT <= 0 || effRT > 1.0 {
		effRT = 0.90
	}
	oneWayEff := math.Sqrt(effRT)
	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		capacityKWH = initial.capacityKWH
	}
	floorState := initial
	if floorState.capacityKWH <= 0 {
		floorState.capacityKWH = capacityKWH
	}
	if anchors.horizonEnd.IsZero() {
		anchors.horizonEnd = timeline[len(timeline)-1].endTime
	}
	wear := getWearRates(settings, optParams)
	isFlatNEM := settings.UtilityRateOptions.IsFlatNetMetering()

	// Precompute stepExportFloor once per step (depends only on stepIdx, not path state).
	stepFloors := make([]stepExportFloor, len(timeline))
	for i := range timeline {
		stepFloors[i] = computeStepExportFloor(i, timeline[i], timeline, floorState, anchors, settings)
	}

	// Step 0 candidate generation
	candidates0, ld0 := c.appendActionCandidates(
		make([]actionCandidate, 0, 5),
		ctx,
		0,
		timeline[0],
		timeline,
		initial,
		anchors,
		settings,
		currentStatus,
		history,
		lastPreceding,
		stepFloors[0],
	)
	if len(candidates0) == 0 {
		return nil, fmt.Errorf("no viable plan found: all candidate actions pruned at step 0")
	}

	type modeBestTrajectory struct {
		actions []actionCandidate
		logData []candidateLogData
		states  []planState
		metrics []intervalMetrics
	}
	modeBestScores := make(map[types.BatteryMode]float64)
	modeBestStrikes := make(map[types.BatteryMode]int)
	modeBestPaths := make(map[types.BatteryMode]modeBestTrajectory)
	candBestScores := make(map[PlanActionName]float64)
	candBestStrikes := make(map[PlanActionName]int)
	totalPathsEvaluated := 0

	// Pre-calculate whether each interval falls within the final chargeCompletionBufferDuration (40 minutes)
	// before a material import rate increase (> priceMaterialityThresholdDollars). Charging within this
	// tail window incurs a minute buffer cost (lateChargeBufferPenaltyPerKWH) so the planner completes overnight
	// or off-peak charging with a 20-40 minute safety cushion rather than waiting until the very last interval.
	inPrePeakChargeBuffer := make([]bool, len(timeline))
	for i := 0; i < len(timeline); i++ {
		for j := i + 1; j < len(timeline); j++ {
			if timeline[j].startTime.Sub(timeline[i].startTime) > chargeCompletionBufferDuration {
				break
			}
			if timeline[j].importRate < timeline[i].importRate-priceMaterialityThresholdDollars {
				break
			}
			if timeline[j].importRate > timeline[i].importRate+priceMaterialityThresholdDollars {
				inPrePeakChargeBuffer[i] = true
				break
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
	// Used in DP frontier bounding to ensure high-SOC paths are never falsely pruned before upcoming peaks.
	// Floored at 0.0 because stored battery energy always has non-negative option value even during negative grid prices.
	maxFutureValuationRates := make([]float64, len(timeline))
	canBatteryExport := settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM
	for i := len(timeline) - 1; i >= 0; i-- {
		rate := max(0.0, timeline[i].importRate)
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

	// Slab allocator for dpNode structs:
	// Rather than calling new(dpNode) for every newly populated (mode, SOC bucket) slot—which would trigger
	// thousands of individual heap allocations per Plan() call—we allocate dpNodes in contiguous slabs of 512
	// pooled via dpNodeSlabPool.
	//
	// Why 512 is both sufficient and well-sized:
	// 1. Unbounded capacity: This is a rolling slab allocator, not a fixed cap. Whenever a 512-node slab fills up
	//    (nodeChunkIdx >= len(nodeChunk)), another [512]dpNode slab is leased from dpNodeSlabPool and nodeChunkIdx
	//    resets to 0, while existing *dpNode parent pointers into earlier slabs remain valid for backtracking.
	// 2. In-place bucket reuse: Within each timeline step, only the first trajectory to land in a given
	//    nextBuckets[mIdx][b] slot claims a node from nodeChunk; subsequent trajectories landing in the same
	//    bucket overwrite *node in-place without consuming another slot.
	// 3. Per-cand0 & cross-site recycling: At the end of each cand0 rollout, the winning trajectory's states,
	//    actions, metrics, and logData are extracted, allowing the next cand0 rollout to reset slabIdx to 0 and
	//    reuse the same slabs in-place. Returning the slabs to dpNodeSlabPool on function exit allows concurrent
	//    workers to reuse the same slabs across sites without retaining both intervalMetrics and candidateLogData on the heap.
	var nodeChunk *[512]dpNode
	usedSlabs := make([]*[512]dpNode, 0, 6)
	defer func() {
		for _, slab := range usedSlabs {
			clear(slab[:])
			dpNodeSlabPool.Put(slab)
		}
	}()
	var nextBuckets [5][201]*dpNode
	activeNodes := make([]*dpNode, 0, 201)

	// Calculate how long the battery has already been in a reserve deficit prior to step 0 without charging.
	// This prevents chaining cheaper-rate deficit waits indefinitely across consecutive polls or after a
	// reserve window started more than chargeCompletionBufferDuration (40 minutes) ago.
	var initialDeficitWaitHours float64
	if initial.soc < timeline[0].minSOC-reserveDeficitChargeTolerancePct {
		now := timeline[0].startTime
		if lastAction != nil && lastAction.BatteryMode != types.BatteryModeChargeAny &&
			lastAction.ReserveSOC > lastAction.SystemStatus.BatterySOC+reserveDeficitChargeTolerancePct {
			lastTS := lastAction.SystemTimestamp
			if lastTS.IsZero() {
				lastTS = lastAction.Timestamp
			}
			if !lastTS.IsZero() && now.After(lastTS) {
				elapsed := now.Sub(lastTS)
				if elapsed <= chargeCompletionBufferDuration {
					initialDeficitWaitHours = max(initialDeficitWaitHours, elapsed.Hours())
				}
			}
		}
		// Probe backward in 5-minute increments up to chargeCompletionBufferDuration (40 minutes) to see
		// how long the active reserve window has already been in deficit. Probing beyond 40 minutes is
		// unnecessary because hasCheaperImmediateNextStep caps total deficit waiting at 40 minutes.
		if len(settings.MinBatterySOCPeriods) > 0 && (lastAction == nil || (lastAction.BatteryMode != types.BatteryModeChargeAny && lastAction.ReserveSOC > lastAction.SystemStatus.BatterySOC+reserveDeficitChargeTolerancePct)) {
			loc := now.Location()
			var windowWait time.Duration
			for back := 5 * time.Minute; back <= chargeCompletionBufferDuration; back += 5 * time.Minute {
				probeTime := now.Add(-back)
				inVPP := false
				for _, ev := range currentStatus.VPPEvents {
					if !ev.OptOut && !probeTime.Before(ev.TSStart) && probeTime.Before(ev.TSEnd) {
						inVPP = true
						break
					}
				}
				if inVPP {
					break
				}
				probeMinSOC := settings.GetMinBatterySOC(ctx, probeTime, loc, timeline[0].price)
				if probeMinSOC <= initial.soc+reserveDeficitChargeTolerancePct {
					break
				}
				windowWait = back
			}
			if windowWait > 0 {
				initialDeficitWaitHours = max(initialDeficitWaitHours, windowWait.Hours())
			}
		}
	}

	// Evaluate forward rollout independently for each initial candidate action at step 0.
	// This ensures every distinct immediate dispatch action explores its optimal future trajectory.
	for _, cand0 := range candidates0 {
		cand0Slice := []actionCandidate{cand0}
		slabIdx := 0
		nodeChunkIdx := 512

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
		activeNodes = append(activeNodes[:0], root)

		// Roll forward through the timeline steps starting at step 0 using mode-aware fine bucketing.
		for stepIdx := 0; stepIdx < len(timeline); stepIdx++ {
			interval := timeline[stepIdx]

			// nextBuckets discretizes continuous battery State of Charge into 0.5% increments
			// (201 buckets from 0.0% to 100.0%) partitioned independently by candidate dispatch mode (5 slots,
			// isolating Load, Standby, ChargeAny, Export, and Direct Solar Export).
			// This eliminates mode collisions (e.g. Standby vs Load, or SolarModeExport vs SolarModeAny)
			// while maintaining sub-millisecond execution.
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
			nextBuckets = [5][201]*dpNode{}
			var candBuf [5]actionCandidate

			for _, parent := range activeNodes {
				var candidates []actionCandidate
				var ld candidateLogData
				prevAction := lastPreceding
				if stepIdx == 0 {
					candidates = cand0Slice
					ld = ld0
				} else {
					prevAction = toPrecedingAction(parent.action, timeline[stepIdx-1])
					candidates, ld = c.appendActionCandidates(
						candBuf[:0],
						ctx,
						stepIdx,
						interval,
						timeline,
						parent.state,
						anchors,
						settings,
						currentStatus,
						history,
						prevAction,
						stepFloors[stepIdx],
					)
				}
				allStepCandidates := candidates
				if stepIdx == 0 {
					allStepCandidates = candidates0
				}
				hasSurplusSolar := interval.durationHours > 0 && (interval.solarKWH-interval.loadKWH) >= minSurplusSolarForReserveKW*interval.durationHours
				hasCheaperImmediateNextStep := false
				if stepIdx+1 < len(timeline) {
					nextInterval := timeline[stepIdx+1]
					if nextInterval.importRate < interval.importRate-priceMaterialityThresholdDollars {
						priorDeficitWaitHours := 0.0
						currStep := stepIdx - 1
						for p := parent; p != nil && currStep >= 0; p = p.parent {
							if p.state.soc < timeline[currStep].minSOC-reserveDeficitChargeTolerancePct &&
								p.action.batteryMode != types.BatteryModeChargeAny {
								priorDeficitWaitHours += timeline[currStep].durationHours
								currStep--
							} else {
								break
							}
						}
						if currStep < 0 {
							priorDeficitWaitHours += initialDeficitWaitHours
						}
						if priorDeficitWaitHours+interval.durationHours <= chargeCompletionBufferDuration.Hours()+1e-6 {
							hasCheaperImmediateNextStep = true
						}
					}
				}
				hasRechargeCandidate := false
				for _, cnd := range allStepCandidates {
					if cnd.batteryMode == types.BatteryModeChargeAny || (hasSurplusSolar && cnd.solarMode != types.SolarModeExport) {
						hasRechargeCandidate = true
						break
					}
				}
				for _, cand := range candidates {
					// Anti-churn re-entry guard: within a continuous price window (same import rate for ChargeAny,
					// or same import and export rates for Export), once a trajectory exits a discretionary grid mode
					// (ChargeAny or Export), it cannot re-enter that same mode at the same rates. This prevents
					// fragmented Charge/Export -> Load -> Charge/Export fluttering during identical price periods,
					// while preserving the solver's freedom to stop charging at the optimal partial SOC or stop
					// exporting when the profitable price window concludes.
					isDiscretionaryGridMode := cand.batteryMode == types.BatteryModeChargeAny || cand.batteryMode == types.BatteryModeExport
					if stepIdx > 0 && isDiscretionaryGridMode && parent.action.batteryMode != cand.batteryMode {
						currImport := interval.importRate
						currExport := interval.exportRate
						currStep := stepIdx - 1
						isReentry := false
						sameDiscretionaryPrice := func(impA, expA, impB, expB float64) bool {
							if cand.batteryMode == types.BatteryModeChargeAny {
								return math.Abs(impA-impB) <= priceEpsilonForEquality
							}
							return isSamePrice(impA, expA, impB, expB)
						}
						for p := parent; p != nil && currStep >= 0; p = p.parent {
							if !sameDiscretionaryPrice(timeline[currStep].importRate, timeline[currStep].exportRate, currImport, currExport) {
								break // Tariff changed
							}
							if p.action.batteryMode == cand.batteryMode {
								wasReserveOnlyCharge := cand.batteryMode == types.BatteryModeChargeAny &&
									(p.action.actionName == PlanActionReserveTargetCharge ||
										p.state.soc <= timeline[currStep].minSOC+reserveDeficitChargeTolerancePct)
								if !wasReserveOnlyCharge {
									isReentry = true
									break
								}
							}
							currStep--
						}
						// If ancestor trace reaches step 0, check lastAction from previous polling cycle
						if !isReentry && currStep < 0 && lastAction != nil && lastAction.CurrentPrice != nil {
							wasLastReserveOnly := cand.batteryMode == types.BatteryModeChargeAny &&
								initial.soc <= timeline[0].minSOC+reserveDeficitChargeTolerancePct
							if !wasLastReserveOnly && sameDiscretionaryPrice(lastPreceding.ImportRate, lastPreceding.ExportRate, currImport, currExport) && lastAction.BatteryMode == cand.batteryMode {
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
								parent.state.soc < interval.minSOC-reserveDeficitChargeTolerancePct
							if !isReserveDeficit {
								continue
							}
						}
					}

					nextState, metrics := stepPhysics(parent.state, cand, interval, settings, roundTripEff)

					transitionCost := stepTransitionCost(stepIdx, cand, prevAction, interval, inPrePeakChargeBuffer[stepIdx])
					wearCost := stepWearCostDollars(cand, parent.state.soc, nextState.soc, interval.minSOC, stepFloors[stepIdx].loadNeededSOC, metrics, roundTripEff, wear)

					// Penalizes idling or running below the configured reserve floor (interval.minSOC) without recharging.
					// If the battery is below reserve and a recharge candidate is available (either from the grid or from surplus
					// solar), the system must actively restore the homeowner's backup reserve immediately.
					inDeficit := nextState.soc < interval.minSOC-reserveDeficitChargeTolerancePct
					isActivelyGridCharging := cand.batteryMode == types.BatteryModeChargeAny
					isRechargingFromSolar := hasSurplusSolar && cand.solarMode != types.SolarModeExport
					isWaitingForCheaperRate := hasCheaperImmediateNextStep && nextState.soc >= parent.state.soc-1e-6

					if inDeficit && hasRechargeCandidate && !isActivelyGridCharging && !isRechargingFromSolar && !isWaitingForCheaperRate {
						// Hard constraint: when below reserve floor and recharging is physically possible,
						// discard any trajectory that fails to restore the homeowner's backup reserve.
						continue
					}

					penaltyCost := stepPenaltyCost(
						stepIdx,
						cand,
						nextState,
						metrics,
						interval,
						inPrePeakChargeBuffer[stepIdx],
						anchors,
						optParams,
					)

					newCost := parent.totalCost + metrics.costDollars + wearCost + transitionCost + penaltyCost

					var strikeInc int
					// Check VPP deadline feasibility: apply strikes for nearest upcoming event deadline
					if hasNearestVPP && settings.GridChargeBatteries && !initial.chargingDisabled {
						if interval.endTime.Equal(nearestVPPDeadline) || (interval.startTime.Before(nearestVPPDeadline) && interval.endTime.After(nearestVPPDeadline)) {
							strikeInc += calculateVPPShortfallStrikes(initial, nextState, nearestVPPDeadline, oneWayEff, settings.GridChargeBatteries)
						}
					}

					newStrikes := parent.strikes + strikeInc

					mIdx := modeIndex(cand.batteryMode, cand.solarMode)
					b := int(math.Round(nextState.soc * 2))
					if b < 0 {
						b = 0
					} else if b > 200 {
						b = 200
					}
					// Keep clamped boundary buckets (minSOC reserve floor and 100% full capacity) isolated
					// from unclamped trajectories:
					// - Bucket 200 is reserved strictly for full capacity (soc >= 100%), preventing a 99.76% trajectory
					//   (which paid slightly less grid import cost) from overwriting 100.00% SOC.
					// - Bucket minBucket is reserved for trajectories at or below reserve (soc <= minSOC, where
					//   canDischarge is false), preventing a trajectory still discharging above reserve (e.g. 20.20%)
					//   from colliding with a clamped at-reserve trajectory until it actually finishes draining to minSOC.
					if b == 200 && nextState.soc < 100.0-1e-6 {
						b = 199
					}
					minBucket := int(math.Round(interval.minSOC * 2))
					if nextState.soc > interval.minSOC+1e-6 && b <= minBucket && minBucket < 199 {
						b = minBucket + 1
					}

					isBetter := false
					if nextBuckets[mIdx][b] == nil {
						isBetter = true
					} else {
						curr := nextBuckets[mIdx][b]
						if newStrikes < curr.strikes {
							isBetter = true
						} else if newStrikes == curr.strikes {
							// Intra-bucket energy adjustment when two trajectories collide in the same (mIdx, b) bucket:
							// 1. Non-charging modes (Standby, Load, Export): The surviving node carries its exact continuous
							//    nextState.energyKWH into stepIdx+1. Without crediting (nextState.energyKWH - curr.state.energyKWH)
							//    at interval.importRate * oneWayEff, a path that drained < 0.5% SOC in Load to save a fraction of
							//    a cent on grid import would overwrite a higher-SOC Standby path in the same 0.5% bucket for free.
							// 2. Below neededSOC (min(nextState.soc, curr.state.soc) < stepFloors[stepIdx].neededSOC): Once both
							//    paths are at or above neededSOC (e.g. free daytime surplus solar will fill the rest to 100%
							//    before peak), extra intra-bucket energy has $0 marginal value because it will be curtailed at 100%.
							// 3. Exclude ChargeAny: Inside an active grid-charging bucket, both paths are charging toward a shared
							//    ceiling (targetSOC or 100%) where the final step clamps both to the same SOC, so crediting a 0.2%
							//    head start would falsely reward skipping an earlier peak discharge or charging at a worse rate.
							var intraBucketCreditRate float64
							if cand.batteryMode != types.BatteryModeChargeAny && min(nextState.soc, curr.state.soc) < stepFloors[stepIdx].neededSOC {
								intraBucketCreditRate = max(0.0, interval.importRate) * oneWayEff
							}
							adjCostDiff := (newCost - curr.totalCost) - (nextState.energyKWH-curr.state.energyKWH)*intraBucketCreditRate
							if adjCostDiff < 0 {
								isBetter = true
							}
						}
					}

					if isBetter {
						node := nextBuckets[mIdx][b]
						if node == nil {
							if nodeChunkIdx >= 512 {
								if slabIdx < len(usedSlabs) {
									nodeChunk = usedSlabs[slabIdx]
								} else {
									nodeChunk = dpNodeSlabPool.Get().(*[512]dpNode)
									usedSlabs = append(usedSlabs, nodeChunk)
								}
								slabIdx++
								nodeChunkIdx = 0
							}
							node = &nodeChunk[nodeChunkIdx]
							nodeChunkIdx++
							nextBuckets[mIdx][b] = node
						}
						*node = dpNode{
							state:     nextState,
							totalCost: newCost,
							strikes:   newStrikes,
							action:    cand,
							logData:   ld,
							metric:    metrics,
							parent:    parent,
						}
					}
				}
			}

			// Collect surviving bucket nodes for the next time step with frontier dominance pruning.
			//
			// What is Frontier Dominance Pruning?
			// At each step, multiple path trajectories land in discrete SOC buckets across 5 mode slots.
			// To compare paths with different SOCs on an equal footing, we compute each node's "effective cost" (effCost):
			//   effCost = totalCost - (storedEnergyKWH * oneWayEff * valuationRate)
			// where valuationRate is the maximum tariff rate appearing anywhere in the remaining horizon.
			// This represents the most optimistic theoretical future net cost if the stored energy is discharged
			// at the highest possible future price.
			//
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

			minStrikes := math.MaxInt
			for m := 0; m < 5; m++ {
				for _, n := range nextBuckets[m] {
					if n != nil && n.strikes < minStrikes {
						minStrikes = n.strikes
					}
				}
			}

			var minEffCost float64 = math.MaxFloat64
			for m := 0; m < 5; m++ {
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

			for m := 0; m < 5; m++ {
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
			if cand0.actionName != "" {
				candBestScores[cand0.actionName] = bestScoreForCand
				candBestStrikes[cand0.actionName] = bestStrikesForCand
			}
			prevStrikes, exists := modeBestStrikes[cand0.batteryMode]
			prevScore := modeBestScores[cand0.batteryMode]
			isBetter := !exists || bestStrikesForCand < prevStrikes || (bestStrikesForCand == prevStrikes && bestScoreForCand < prevScore-planScoreTieEpsilon)
			if !isBetter && exists && bestStrikesForCand == prevStrikes && math.Abs(bestScoreForCand-prevScore) <= planScoreTieEpsilon {
				prevTraj := modeBestPaths[cand0.batteryMode]
				var prevCand actionCandidate
				if len(prevTraj.actions) > 0 {
					prevCand = prevTraj.actions[0]
				}
				// Tie-breaker: prefer PlanActionReserveTargetCharge over unconstrained charging on ties;
				// otherwise prefer candidate matching lastAction to avoid unnecessary step-0 mode switches.
				if cand0.actionName == PlanActionReserveTargetCharge && prevCand.actionName != PlanActionReserveTargetCharge {
					isBetter = true
				} else if prevCand.actionName != PlanActionReserveTargetCharge &&
					lastAction != nil && cand0.batteryMode == lastAction.BatteryMode && cand0.solarMode == lastAction.SolarMode &&
					(prevCand.batteryMode != lastAction.BatteryMode || prevCand.solarMode != lastAction.SolarMode) {
					isBetter = true
				}
			}
			if isBetter {
				n := len(timeline)
				traj, hasTraj := modeBestPaths[cand0.batteryMode]
				if !hasTraj {
					traj = modeBestTrajectory{
						actions: make([]actionCandidate, n),
						logData: make([]candidateLogData, n),
						states:  make([]planState, n+1),
						metrics: make([]intervalMetrics, n),
					}
				}
				curr := bestNodeForCand
				traj.states[n] = curr.state
				for i := n - 1; i >= 0; i-- {
					traj.actions[i] = curr.action
					traj.logData[i] = curr.logData
					traj.metrics[i] = curr.metric
					curr = curr.parent
					traj.states[i] = curr.state
				}
				modeBestStrikes[cand0.batteryMode] = bestStrikesForCand
				modeBestScores[cand0.batteryMode] = bestScoreForCand
				modeBestPaths[cand0.batteryMode] = traj
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

	var bestOverallMode types.BatteryMode
	var hasBestOverall bool
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
		tieEps := planScoreTieEpsilon
		if hasBestOverall && bestOverallMode == types.BatteryModeLoad && mode == types.BatteryModeStandby &&
			(lastAction == nil || lastAction.BatteryMode != types.BatteryModeStandby) {
			// Require at least priceEpsilonForEquality ($0.001) of net savings before pulling the battery
			// out of active self-consumption (BatteryModeLoad) into idle Standby over sub-mill-dollar noise,
			// unless the battery is already resting in Standby from the previous polling cycle.
			tieEps = priceEpsilonForEquality
		}
		if strikes < bestOverallStrikes || (strikes == bestOverallStrikes && score < bestOverallScore-tieEps) {
			if hasBestOverall {
				runnerUpScore = bestOverallScore
				runnerUpStrikes = bestOverallStrikes
			}
			bestOverallStrikes = strikes
			bestOverallScore = score
			bestOverallMode = mode
			hasBestOverall = true
		} else if strikes < runnerUpStrikes || (strikes == runnerUpStrikes && score < runnerUpScore-planScoreTieEpsilon) {
			runnerUpScore = score
			runnerUpStrikes = strikes
		}
	}

	winningTraj := modeBestPaths[bestOverallMode]
	actions := winningTraj.actions
	logData := winningTraj.logData
	states := winningTraj.states
	metrics := winningTraj.metrics

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
				chargePath, exists := modeBestPaths[types.BatteryModeChargeAny]
				chargeScore := modeBestScores[types.BatteryModeChargeAny]
				chargeStrikes := modeBestStrikes[types.BatteryModeChargeAny]
				if !exists || len(chargePath.actions) == 0 {
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

					bestOverallMode = types.BatteryModeChargeAny
					bestOverallScore = modeBestScores[types.BatteryModeChargeAny]
					bestOverallStrikes = chargeStrikes
					actions = chargePath.actions
					logData = chargePath.logData
					states = chargePath.states
					metrics = chargePath.metrics

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
		logData:           logData,
		states:            states,
		metrics:           metrics,
		timeline:          timeline,
		stepFloors:        stepFloors,
		totalCost:         totalPhysicalCost,
		strikes:           winningStrikes,
		initialCandidates: candidates0,
		modeScores:        modeBestScores,
		modeStrikes:       modeBestStrikes,
		candScores:        candBestScores,
		candStrikes:       candBestStrikes,
		bestScore:         bestOverallScore,
		anchors:           anchors,
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

// stepTransitionCost calculates the mode-switching and step-0 inertia cost ($) for a single interval transition.
func stepTransitionCost(
	stepIdx int,
	cand actionCandidate,
	prev precedingAction,
	interval planInterval,
	inPrePeakBuffer bool,
) float64 {
	var transitionCost float64
	if stepIdx == 0 {
		if prev.BatteryMode != types.BatteryModeNoChange {
			// A material tariff change (>= $0.03/kWh in import or export rate) marks a legitimate TOU or
			// real-time price boundary where switching battery modes is expected and free of inertia penalties.
			isPriceBoundary := !prev.NoPrice &&
				isAbsSignificantDiffPrice(interval.importRate, interval.exportRate, prev.ImportRate, prev.ExportRate)
			// Asymmetric price-drop check for SolarMode: exiting SolarModeExport when rates drop by >= $0.03/kWh
			// is free, whereas entering SolarModeExport on a price spike still pays solarModeSwitchPenalty ($0.01)
			// so brief end-of-hour real-time blips must earn at least a cent before toggling hardware solar modes.
			isSolarPriceDrop := !prev.NoPrice &&
				isSignificantDiffPrice(prev.ImportRate, prev.ExportRate, interval.importRate, interval.exportRate)
			if !isPriceBoundary && cand.batteryMode != prev.BatteryMode {
				// Exception for leaving Standby -> Load or Standby -> ChargeAny at step 0:
				// - Standby -> Load: if a prior poll placed the battery in Standby, a full $0.025 inertia threshold
				//   could exceed a single interval's baseload savings and permanently trap the battery in Standby.
				// - Standby -> ChargeAny: during a flat off-peak window, the planner waits in Standby (WaitingToCharge)
				//   until the just-in-time charge start interval arrives; because the price does not change inside
				//   that flat window, charging must be able to start without a large inertia hurdle.
				// All other same-price mode changes (such as Load -> Standby or stopping an active ChargeAny early)
				// pay the duration-scaled inertia threshold to prevent polling chatter.
				if prev.BatteryMode == types.BatteryModeStandby &&
					(cand.batteryMode == types.BatteryModeLoad || cand.batteryMode == types.BatteryModeChargeAny) {
					transitionCost += modeSwitchPenalty
				} else {
					transitionCost += max(modeSwitchPenalty, defaultInertiaThresholdDollars*min(1.0, interval.durationHours))
				}
			}
			// DirectSolarExport shares BatteryModeLoad with normal self-consumption, so SolarMode changes are
			// penalized separately unless exiting on a material price drop.
			if !isSolarPriceDrop && prev.SolarMode > 0 && cand.solarMode > 0 && cand.solarMode != prev.SolarMode {
				transitionCost += solarModeSwitchPenalty
			}
		}
		return transitionCost
	}

	isPriceBoundary := isAbsSignificantDiffPrice(interval.importRate, interval.exportRate, prev.ImportRate, prev.ExportRate)
	isSolarPriceDrop := isSignificantDiffPrice(prev.ImportRate, prev.ExportRate, interval.importRate, interval.exportRate)
	isLatePeakExport := prev.BatteryMode == types.BatteryModeLoad && cand.batteryMode == types.BatteryModeExport
	if !isPriceBoundary && prev.BatteryMode != cand.batteryMode {
		if prev.BatteryMode == types.BatteryModeChargeAny && cand.batteryMode == types.BatteryModeStandby && inPrePeakBuffer {
			// Exemption 1 (Pre-peak charge completion): lateChargeBufferPenaltyPerKWH steers grid charging to finish
			// 20-40 minutes before a peak rate jump (inside the cheap window, before the price boundary). Exempting
			// ChargeAny -> Standby in inPrePeakBuffer ensures finishing early isn't penalized vs charging up to the boundary.
		} else if isLatePeakExport {
			// Exemption 2 (Late-peak export): during a multi-hour peak window, earlyExportCost steers the battery to
			// cover home load first (BatteryModeLoad) and export surplus in the final intervals of the peak window.
			// Exempting Load -> Export mid-window ensures late export doesn't pay $0.005 more than exporting at the
			// start of the peak window (where isPriceBoundary is true and transitionCost is $0).
		} else {
			transitionCost += modeSwitchPenalty
		}
	}
	// Exemption 2 also covers transitioning from DirectSolarExport (BatteryModeLoad + SolarModeExport) into
	// late-peak BatteryModeExport (which uses SolarModeAny by default but exports all surplus solar identically);
	// otherwise exiting SolarModeExport before the end-of-peak price drop would pay an extra $0.01 solarModeSwitchPenalty.
	if !isSolarPriceDrop && !isLatePeakExport && prev.SolarMode > 0 && cand.solarMode > 0 && prev.SolarMode != cand.solarMode {
		transitionCost += solarModeSwitchPenalty
	}
	return transitionCost
}

// stepWearCostDollars calculates the battery wear/degradation cost ($) for a single interval transition.
func stepWearCostDollars(
	cand actionCandidate,
	startSOC float64,
	endSOC float64,
	minSOC float64,
	loadNeededSOC float64,
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
		isReserveOnly := cand.actionName == PlanActionReserveTargetCharge ||
			endSOC <= minSOC+reserveDeficitChargeTolerancePct
		if isReserveOnly {
			chargeDegradationCost = metrics.gridChargeKWH * roundTripEff * wear.gridChargePerKWH
		} else if cand.actionName == PlanActionVPPPreChargeBeforeDeadline {
			chargeDegradationCost = metrics.gridChargeKWH * roundTripEff * wear.gridChargePerKWH
		} else {
			// Partition the interval's SOC gain across three bands:
			//   1. Reserve recovery [startSOC, minSOC]: pays physical gridChargePerKWH.
			//   2. Home-load arbitrage [minSOC, loadNeededSOC]: pays arbitrageGatePerKWH.
			//   3. Battery-export excess [loadNeededSOC, endSOC]: pays exportChargeWearRate =
			//      max(arbitrageGatePerKWH, exportGatePerKWH - batteryExportPerKWH), ensuring a full
			//      grid-charge-to-battery-export cycle pays at least exportGatePerKWH (MinBatteryExportDifference)
			//      without penalizing solar-charged battery exports in BatteryModeExport.
			exportChargeWearRate := max(wear.arbitrageGatePerKWH, wear.exportGatePerKWH-wear.batteryExportPerKWH)
			totalDeltaSOC := endSOC - startSOC
			if totalDeltaSOC <= 1e-6 {
				chargeDegradationCost = metrics.gridChargeKWH * roundTripEff * wear.arbitrageGatePerKWH
			} else {
				var reserveDelta, loadArbDelta, exportArbDelta float64
				if startSOC < minSOC-reserveDeficitChargeTolerancePct {
					reserveDelta = max(0.0, min(endSOC, minSOC)-startSOC)
				}
				arbStart := max(startSOC, startSOC+reserveDelta)
				loadCeiling := endSOC
				if loadNeededSOC > 0 && endSOC > loadNeededSOC+socTargetTolerancePct {
					loadCeiling = max(minSOC, loadNeededSOC)
				}
				if loadCeiling > arbStart {
					loadArbDelta = min(endSOC, loadCeiling) - arbStart
				}
				exportStart := max(arbStart, loadCeiling)
				if endSOC > exportStart {
					exportArbDelta = endSOC - exportStart
				}
				effectiveWearRate := (reserveDelta*wear.gridChargePerKWH + loadArbDelta*wear.arbitrageGatePerKWH + exportArbDelta*exportChargeWearRate) / totalDeltaSOC
				chargeDegradationCost = metrics.gridChargeKWH * roundTripEff * effectiveWearRate
			}
		}
	}

	return exportDegradationCost + chargeDegradationCost
}

// stepPenaltyCost calculates the soft timing and safety-buffer tie-breaker penalties ($) for a single interval transition.
func stepPenaltyCost(
	stepIdx int,
	cand actionCandidate,
	nextState planState,
	metrics intervalMetrics,
	interval planInterval,
	inPrePeakBuffer bool,
	anchors planningAnchors,
	optParams types.OptimizationParams,
) float64 {
	effRT := optParams.RoundTripEfficiency
	if effRT <= 0 || effRT > 1.0 {
		effRT = 0.90
	}
	oneWayEff := math.Sqrt(effRT)
	capacityKWH := nextState.capacityKWH
	horizonEnd := anchors.horizonEnd
	// 1. Pre-peak charge completion buffer: grid charging inside the final 40 minutes (inPrePeakBuffer) before a
	// material rate increase pays a tiny per-kWh penalty (just under 1 hour of holdingCost) so the planner completes
	// off-peak charging 20-40 minutes early rather than waiting until the very last interval before rates jump.
	var lateChargeBufferCost float64
	if inPrePeakBuffer && metrics.gridChargeKWH > 0 {
		lateChargeBufferCost = metrics.gridChargeKWH * oneWayEff * lateChargeBufferPenaltyPerKWH
	}

	// 2. Late-export preference: because holdingCost charges 1.0x batteryHoldingCostPerHourPerKWH on stored energy,
	// dumping the battery at the start of a multi-hour peak export window would otherwise save holdingCost for the
	// rest of the window. Charging 1.1x batteryHoldingCostPerHourPerKWH over remainingHorizonHours on exported DC
	// energy more than offsets that holdingCost savings (net +0.1x per hour), steering the battery to cover peak
	// home load first and defer BatteryModeExport to the end of the equal-rate export window.
	var earlyExportCost float64
	if cand.batteryMode == types.BatteryModeExport {
		batGridExportKWH := metrics.batExportKWH + min(metrics.batSuppliedHomeKWH, metrics.solarExportKWH)
		if batGridExportKWH > 0 {
			exportedDCKWH := batGridExportKWH / oneWayEff
			remainingHorizonHours := max(0.0, horizonEnd.Sub(interval.endTime).Hours())
			earlyExportCost = exportedDCKWH * remainingHorizonHours * (1.1 * batteryHoldingCostPerHourPerKWH)
		}
	}

	// 3. Peak survival safety buffer: at the final step of each true peak window (pw.endIndex), apply a soft
	// penalty ($0.02/kWh) to any shortfall below the target peak survival cushion (reserve + bufferEnergyKWH,
	// capped at 50% SOC). This encourages pre-charging a small safety margin ahead of peak so higher-than-forecast
	// home load does not exhaust the battery mid-peak and force expensive peak grid imports.
	var peakBufferCost float64
	for _, pw := range anchors.peakWindows {
		if stepIdx == pw.endIndex && pw.bufferEnergyKWH > 0 {
			reserveKWH := capacityKWH * (interval.minSOC / 100.0)
			targetEnergyKWH := min(capacityKWH*0.50, reserveKWH+pw.bufferEnergyKWH)
			if nextState.energyKWH < targetEnergyKWH {
				shortfallKWH := targetEnergyKWH - nextState.energyKWH
				peakBufferCost += shortfallKWH * peakSurvivalBufferShortfallPenaltyDollarsPerKWH
			}
		}
	}

	// 4. Holding cost ($0.0001/kWh-hour): breaks ties across flat off-peak windows in favor of just-in-time
	// charging closer to peak or VPP windows rather than charging hours earlier than needed.
	holdingCost := nextState.energyKWH * interval.durationHours * batteryHoldingCostPerHourPerKWH
	return lateChargeBufferCost + earlyExportCost + peakBufferCost + holdingCost
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

	// Determine replacement rate at horizon end.
	// Floor at 0.0 because negative post-horizon rates would invert terminal credits into
	// penalties for ending with stored battery energy.
	replacementRate := max(0.0, anchors.knownPostHorizonRate)
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
	for _, slice := range anchors.postHorizonSlices {
		if remainingKWH <= 0 {
			break
		}
		usedKWH := min(remainingKWH, slice.loadKWH)
		creditRate := max(0.0, slice.importRate) * oneWayEff * terminalTier1DiscountFactor
		totalCredit += usedKWH * creditRate
		remainingKWH -= usedKWH
	}
	if remainingKWH > 0 {
		usableCapacityKWH := max(0.0, capacityKWH-targetEnergyKWH)
		unfilledBySolarKWH := max(0.0, usableCapacityKWH-anchors.postHorizonSolarSurplusKWH)
		tier2KWH := min(remainingKWH, unfilledBySolarKWH)
		tier2BaseRate := replacementRate
		if n := len(anchors.postHorizonSlices); n > 0 {
			tier2BaseRate = min(tier2BaseRate, max(0.0, anchors.postHorizonSlices[n-1].importRate))
		}
		tier2CreditRate := tier2BaseRate * oneWayEff * terminalTier1DiscountFactor * terminalSurplusDiscountFactor
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
	minSOC := interval.minSOC
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

		_, futPrice := findUpcomingPeak(timeline, stepIdx)
		isReserveTargetStep := func(idx int) bool {
			act := winningPath.actions[idx]
			if act.targetSOC <= 0 || math.Abs(act.targetSOC-timeline[idx].minSOC) > 1e-6 {
				return false
			}
			if act.actionName != "" {
				return act.actionName == PlanActionReserveTargetCharge
			}
			return idx < len(winningPath.states) && winningPath.states[idx].soc < timeline[idx].minSOC-reserveFloorTolerancePct
		}
		if isReserveTargetStep(stepIdx) {
			return types.ActionReasonDeficitChargeNow,
				"Charging battery to target reserve.", futPrice
		}
		for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
			if winningPath.actions[j].batteryMode != types.BatteryModeChargeAny {
				break
			}
			if isReserveTargetStep(j) {
				return types.ActionReasonDeficitChargeNow,
					"Charging battery to target reserve.", futPrice
			}
		}

		// Scan chronologically for the next upcoming event (battery export, peak import discharge,
		// or profitable solar export arbitrage) before the next distinct grid-charge episode.
		// Checking chronologically prevents a routine afternoon solar export from overshadowing
		// an earlier morning peak discharge.
		optParams := settings.GetOptimizationParams()
		roundTripEff := optParams.RoundTripEfficiency
		rechargeCost := importRate
		if roundTripEff > 0 && roundTripEff <= 1.0 {
			rechargeCost = importRate / roundTripEff
		}
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
				timeline[j].importRate-rechargeCost >= wear.arbitrageGatePerKWH &&
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
		if state.soc <= minSOC+reserveFloorTolerancePct && metrics.batSuppliedHomeKWH <= minSignificantBatteryPowerKW*interval.durationHours {
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
						if k+1 < len(winningPath.states) && winningPath.states[k+1].soc <= minSOC+reserveFloorTolerancePct {
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

	var targetSOC int
	if immediateAction.batteryMode == types.BatteryModeStandby {
		targetSOC = 0
	} else if immediateAction.batteryMode == types.BatteryModeChargeAny && (immediateAction.targetSOC <= 0 || immediateAction.actionName == PlanActionGridArbitragePreCharge) {
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
		if immediateAction.targetSOC > 0 && peakSOC > immediateAction.targetSOC {
			peakSOC = immediateAction.targetSOC
		}
		targetSOC = int(math.Ceil(peakSOC - 1e-9))
		if targetSOC > 100 {
			targetSOC = 100
		}
	} else if immediateAction.overrideReserveSOC > 0 {
		targetSOC = int(math.Round(immediateAction.overrideReserveSOC))
	} else if immediateAction.targetSOC > 0 {
		if immediateAction.actionName == PlanActionReserveTargetCharge {
			targetSOC = int(math.Round(immediateAction.targetSOC))
		} else {
			targetSOC = min(100, int(math.Ceil(immediateAction.targetSOC-1e-9)))
		}
	} else {
		targetSOC = int(math.Round(immediateInterval.minSOC))
	}

	// Deficit and capacity hit times detection from winning path trajectory
	var hitDeficitAt time.Time
	var hitCapacityAt time.Time

	minSOC := immediateInterval.minSOC
	if initialStatus.BatterySOC <= minSOC+reserveFloorTolerancePct {
		hitDeficitAt = timeline[0].startTime
	}
	if initialStatus.BatterySOC >= 100.0-socTargetTolerancePct {
		hitCapacityAt = timeline[0].startTime
	}

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		stepReserveSOC := timeline[i].minSOC
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
