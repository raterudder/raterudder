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

	// fastTrackDelayThreshold is the maximum delay duration below which a planned charge
	// at an equal price will be triggered immediately instead of waiting for a subsequent cycle.
	fastTrackDelayThreshold = 25 * time.Minute

	// minSolarGenerationKW is the threshold below which solar generation is treated as negligible.
	minSolarGenerationKW = 0.05

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

	// chargeSessionStartPenalty is a nominal physical transition penalty ($0.01 = 1 cent)
	// applied during dynamic programming forward search when initiating a new grid-charging
	// session from an idle or discharging state. This eliminates mode chattering and strictly
	// prioritizes contiguous charging blocks over fragmented start-stop charging during flat
	// or similarly-priced off-peak periods, while still permitting split charging if an intervening
	// rate spike exceeds 1 cent/kWh.
	chargeSessionStartPenalty = 0.01

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

	// socBucketResolutionPct is the width (in % SOC) of each discrete state bin used in forward dynamic programming
	// (yielding 201 discrete buckets between 0% and 100%).
	socBucketResolutionPct = 0.5

	// socTargetTolerancePct is the tolerance (in % SOC) allowed when verifying whether a forward path reached its target SOC.
	// It matches socBucketResolutionPct (0.5%) so that trajectories ending at e.g. 99.6% due to bin quantization are not discarded.
	socTargetTolerancePct = 0.5

	// priceMaterialityThresholdDollars ($0.005/kWh, or 0.5¢/kWh) is the minimum price differential required for an economic
	// distinction to be meaningful to homeowners when generating decision explanations (e.g. waiting to charge vs charging now).
	// Differences below half a cent are typically floating-point noise or insignificant tariff riders.
	priceMaterialityThresholdDollars = 0.005

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

	// reserveFloorTolerancePct is the numerical tolerance (0.1% SOC) used when evaluating if
	// the battery is resting at its emergency reserve floor.
	reserveFloorTolerancePct = 0.1

	// minBatteryDeliveredEnergyKWH is the energy threshold (10 Wh) below which battery discharge
	// to home load is considered negligible (e.g. at reserve floor).
	minBatteryDeliveredEnergyKWH = 0.01

	// postHorizonPriceRollupTime is the duration used to blend prices beyond the planning horizon.
	postHorizonPriceRollupTime = 4 * time.Hour
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

// actionCandidate represents a permissible control action pair for an interval.
type actionCandidate struct {
	batteryMode        types.BatteryMode
	solarMode          types.SolarMode
	reason             types.ActionReason
	description        string
	targetSOC          int
	overrideReserveSOC float64
	isTruePeak         bool
	logFn              func(ctx context.Context, selected bool)
}

// precedingAction contains only the action state guaranteed to be known from
// the previous interval (both from lastAction at step 0 and from parent nodes during DP rollout).
type precedingAction struct {
	BatteryMode types.BatteryMode
	SolarMode   types.SolarMode
}

func toPrecedingAction(action *types.Action) precedingAction {
	if action == nil {
		return precedingAction{}
	}
	return precedingAction{
		BatteryMode: action.BatteryMode,
		SolarMode:   action.SolarMode,
	}
}

func (a actionCandidate) toPrecedingAction() precedingAction {
	return precedingAction{
		BatteryMode: a.batteryMode,
		SolarMode:   a.solarMode,
	}
}

// planLog formats and emits structured slog messages for action candidates, distinguishing whether
// the candidate was ultimately chosen for execution or evaluated but not chosen.
func planLog(ctx context.Context, selected bool, actionName string, attrs ...slog.Attr) {
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
	batSuppliedHomeKWH       float64
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

// planningAnchors holds fixed operational markers detected across the horizon.
type planningAnchors struct {
	vppEvents            []vppAnchor
	knownPostHorizonRate float64 // Known rate at H+1 if tariff has a fixed weekly schedule
	minHorizonImportRate float64
	maxHorizonImportRate float64
}

// planPath represents a complete simulated trajectory over the entire horizon.
type planPath struct {
	actions           []actionCandidate
	states            []planState
	metrics           []intervalMetrics
	totalCost         float64
	initialCandidates []actionCandidate
	modeScores        map[types.BatteryMode]float64
	bestScore         float64
}

// executeLogs invokes the deferred logging callback on each action candidate in the winning trajectory
// as well as any candidate actions evaluated at step 0 that were not chosen.
func (p *planPath) executeLogs(ctx context.Context) {
	if p == nil {
		return
	}
	chosenAction := actionCandidate{}
	if len(p.actions) > 0 {
		chosenAction = p.actions[0]
	}

	// 1. Log candidates evaluated at step 0 that were not chosen
	for _, cand := range p.initialCandidates {
		isChosen := cand.batteryMode == chosenAction.batteryMode &&
			cand.solarMode == chosenAction.solarMode &&
			cand.reason == chosenAction.reason &&
			cand.targetSOC == chosenAction.targetSOC
		if isChosen {
			continue
		}
		if cand.logFn != nil {
			cand.logFn(ctx, false)
		}
		score, evaluated := p.modeScores[cand.batteryMode]
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
			slog.Float64("candidateScoreDollars", score),
			slog.Float64("winningScoreDollars", p.bestScore),
			slog.Float64("costDeltaDollars", delta),
		)
	}

	// 2. Log chosen actions in the winning trajectory
	for _, act := range p.actions {
		if act.logFn != nil {
			act.logFn(ctx, true)
		}
	}
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
			GeneratedAt:        now.UTC(),
			HorizonHours:       0,
			TotalProjectedCost: 0,
			Periods:            []types.PlanPeriod{},
		}
		return Decision{Action: fallbackAction}, fallbackPlan, nil
	}

	// 2. Build the discrete timeline across the horizon (including price synthesis for midnight cutoffs)
	timeline, simParams, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, history, weather, settings, currentStatus)
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
	decision, plan := finalizeDecisionAndPlan(winningPath, timeline, currentStatus, currentPrice, now, settings)
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
) ([]planInterval, types.SimulationParams, error) {
	// Helper to normalize price timestamps: if TSEnd is zero or invalid, default to 1 hour
	normalizePrice := func(p types.Price) types.Price {
		if p.TSStart.IsZero() {
			p.TSStart = now
		}
		if p.TSEnd.IsZero() || !p.TSEnd.After(p.TSStart) {
			p.TSEnd = p.TSStart.Add(time.Hour)
		}
		return p
	}

	// 1. Collate pricing records
	var allPrices []types.Price
	if !nowPrice.TSStart.IsZero() || nowPrice.DollarsPerKWH != 0 || nowPrice.GridUseDollarsPerKWH != 0 {
		allPrices = append(allPrices, normalizePrice(nowPrice))
	}

	// Filter and sort future prices
	for _, fp := range futurePrices {
		norm := normalizePrice(fp)
		if !norm.TSEnd.Before(now) {
			allPrices = append(allPrices, norm)
		}
	}

	sort.Slice(allPrices, func(i, j int) bool {
		return allPrices[i].TSStart.Before(allPrices[j].TSStart)
	})

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
		return nil, types.SimulationParams{}, fmt.Errorf("insufficient pricing horizon: %.2f hours available, minimum required is %d hours", availableHours, minPlanningHorizonHours)
	}

	// Build energy model for hourly load and solar forecasts
	model, simParams := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

	// Build solar trend adjustment
	todaySolarTrend := 1.0
	if len(weather) == 0 {
		todaySolarTrend = c.calculateSolarTrend(ctx, now, history, model, settings)
	}

	// 2. Convert into discrete intervals across the horizon
	maxEnd := now.Add(time.Duration(maxPlanningHorizonHours) * time.Hour)
	horizonEnd := latestPriceTime
	if horizonEnd.After(maxEnd) {
		horizonEnd = maxEnd
	}

	var timeline []planInterval
	currentTime := now
	currentPrice := nowPrice
	idx := 0

	for currentTime.Before(horizonEnd) {
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

		if !foundPrice {
			// just continue using the last found price if we don't have one
			log.Ctx(ctx).WarnContext(ctx, "no price found at current time when planning",
				slog.Time("currentTime", currentTime),
				slog.Int("totalPricesInHorizon", len(allPrices)),
				slog.Float64("availableHours", availableHours),
				slog.Time("horizonEnd", horizonEnd),
				slog.Any("lastPrice", currentPrice),
			)
		}

		// Ensure matched price has valid timestamp boundaries
		if currentPrice.TSStart.IsZero() {
			currentPrice.TSStart = currentTime
			currentPrice.TSEnd = stepEnd
		}

		// Check operational VPP boundaries (prep deadline, event start, event end) to split intervals cleanly
		buffer := time.Duration(settings.VPPChargingBufferMinutes) * time.Minute
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
		if stepEnd.After(horizonEnd) {
			stepEnd = horizonEnd
		}

		// If duration is greater than or equal to 40 minutes, split it to provide finer sub-hourly resolution.
		// Chop off 20 minutes from the start so that an hour breaks into 3 20-minute segments.
		duration := stepEnd.Sub(currentTime)
		if duration >= 40*time.Minute {
			stepEnd = currentTime.Add(20 * time.Minute)
		}

		// Short interval handling:
		// If duration is under 10 minutes, check if we can merge with the next boundary without crossing a price boundary.
		duration = stepEnd.Sub(currentTime)
		if duration < 10*time.Minute && !stepEnd.Equal(horizonEnd) {
			nextBoundary := stepEnd.Add(20 * time.Minute)
			if nextBoundary.After(horizonEnd) {
				nextBoundary = horizonEnd
			}
			if (currentPrice.TSEnd.After(stepEnd) || currentPrice.TSEnd.IsZero()) && nextBoundary.Sub(currentTime) <= 40*time.Minute {
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

		// Projected solar generation energy for this interval (kWh)
		solarKWH := profile.AvgSolarKWH * durationHrs
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
		if len(history) == 0 && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
		} else if loadKWH <= 0 && currentStatus.HomeKW > 0 {
			loadKWH = currentStatus.HomeKW * durationHrs
		}

		importRate := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
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
			solarKWH:      solarKWH,
			minSOC:        minSOC,
		}

		timeline = append(timeline, interval)
		currentTime = stepEnd
		idx++
	}

	totalHorizonDuration := horizonEnd.Sub(now)
	log.Ctx(ctx).DebugContext(ctx, "plan timeline built",
		slog.Int("horizonIntervals", len(timeline)),
		slog.Duration("totalHorizonDuration", totalHorizonDuration),
		slog.Float64("initialSOC", currentStatus.BatterySOC),
		slog.Float64("capacityKWH", currentStatus.BatteryCapacityKWH),
	)

	return timeline, simParams, nil
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

	if price.SeparateGenerationCredit {
		return price.GenerationCreditDollarsPerKWH
	}

	return price.DollarsPerKWH + price.GenerationAdjustmentDollarsPerKWH
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

	minImport := timeline[0].importRate
	maxImport := timeline[0].importRate
	for _, it := range timeline {
		if it.importRate > maxImport {
			maxImport = it.importRate
		}
		if it.importRate < minImport {
			minImport = it.importRate
		}
	}
	anchors.minHorizonImportRate = minImport
	anchors.maxHorizonImportRate = maxImport

	horizonStart := timeline[0].startTime
	horizonEnd := timeline[len(timeline)-1].endTime

	// 1. Detect VPP Events in Horizon (extended by vppStandbyLeadTime so events starting right after horizon end are caught)
	vppScanEnd := horizonEnd.Add(vppStandbyLeadTime)
	buffer := time.Duration(settings.VPPChargingBufferMinutes) * time.Minute
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
	stepTime := interval.startTime
	currentSOC := state.soc

	capacityKWH := state.capacityKWH
	if capacityKWH <= 0 {
		capacityKWH = currentStatus.BatteryCapacityKWH
	}
	roundTripEff, reserveBufferPct, _ := settings.GetOptimizationParams()
	effectiveReserveSOC := interval.minSOC + reserveBufferPct

	minImport := anchors.minHorizonImportRate
	maxImport := anchors.maxHorizonImportRate
	if maxImport <= 0 && len(timeline) > 0 {
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
	}
	minDeficitDiff := max(minPeakRateSpreadDollars, settings.MinDeficitPriceDifferenceDollarsPerKWH)
	isTruePeak := maxImport > 0 &&
		interval.importRate >= maxImport-priceMaterialityThresholdDollars &&
		maxImport >= minImport+minDeficitDiff

	// Determine default solar mode respecting user preference and export rates
	defaultSolarMode := types.SolarModeAny
	if !settings.GridExportSolar || interval.exportRate < 0 {
		defaultSolarMode = types.SolarModeNoExport
	}

	isFlatNEM := isFlatNetMetering(settings.UtilityRateOptions)

	// Determine if direct solar export is enabled. Flat net metering customers bank surplus 1:1 against consumption
	// and have no incentive to export solar while discharging battery for home load.
	hasSolar := interval.avgSolarKW() > minSolarGenerationKW || (stepIdx == 0 && currentStatus.SolarKW > minSolarGenerationKW)
	canDirectSolarExport := settings.ManageTOUSchedules && settings.GridExportSolar && hasSolar && !isFlatNEM && interval.exportRate > 0

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
	requiredHeadroom := calculateStartChargeHeadroomKWH(capacityKWH, maxChargeKW, settings)
	if isAlreadyCharging {
		requiredHeadroom = continueChargeHeadroomKWH
	}
	gridChargeAllowed := settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled && !state.chargingDisabled && !currentStatus.GridUnavailable
	canGridCharge := gridChargeAllowed && headroomKWH >= requiredHeadroom

	// Identify the nearest upcoming VPP event whose deadline has not yet passed.
	effRT := roundTripEff
	if effRT <= 0 || effRT > 1.0 {
		effRT = 0.90
	}
	oneWayEff := math.Sqrt(effRT)

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
				desc := "Preserving reserve in standby."
				if stepIdx == 0 {
					desc = fmt.Sprintf("VPP Target SOC reached (%.0f%%). Preserving reserve in standby.", targetSOC)
					if vpp.mandatory {
						desc = fmt.Sprintf("Mandatory VPP Target SOC reached (%.0f%%). Preserving reserve in standby.", targetSOC)
					}
				}
				candidates = append(candidates, actionCandidate{
					batteryMode:        types.BatteryModeStandby,
					solarMode:          defaultSolarMode,
					reason:             types.ActionReasonVPPActive,
					description:        desc,
					overrideReserveSOC: targetSOC,
					isTruePeak:         isTruePeak,
					logFn: func(ctx context.Context, selected bool) {
						planLog(ctx, selected, "VPP target SOC reached",
							slog.Time("stepTime", stepTime),
							slog.Time("vppStart", vpp.eventStart),
							slog.Time("vppEnd", vpp.eventEnd),
							slog.Float64("currentSOC", currentSOC),
							slog.Float64("vppTargetSOC", targetSOC),
							slog.Bool("mandatory", vpp.mandatory),
						)
					},
				})
				return candidates
			}

			batMode := types.BatteryModeLoad
			if (settings.ManageTOUSchedules && settings.GridExportBatteries) || vpp.price > 0 {
				batMode = types.BatteryModeExport
			}
			desc := "VPP Event Active."
			if stepIdx == 0 {
				desc = fmt.Sprintf("VPP Event Active (Discharging to %.0f%%)", targetSOC)
				if vpp.mandatory {
					desc = fmt.Sprintf("Mandatory VPP Event Active (Discharging to %.0f%%)", targetSOC)
				}
			}
			candidates = append(candidates, actionCandidate{
				batteryMode:        batMode,
				solarMode:          defaultSolarMode,
				reason:             types.ActionReasonVPPActive,
				description:        desc,
				targetSOC:          int(math.Round(targetSOC)),
				overrideReserveSOC: targetSOC,
				isTruePeak:         isTruePeak,
				logFn: func(ctx context.Context, selected bool) {
					planLog(ctx, selected, "VPP active event discharging",
						slog.Time("stepTime", stepTime),
						slog.Time("vppStart", vpp.eventStart),
						slog.Time("vppEnd", vpp.eventEnd),
						slog.Float64("currentSOC", currentSOC),
						slog.Float64("vppTargetSOC", targetSOC),
						slog.Bool("mandatory", vpp.mandatory),
					)
				},
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
			isApproximatelyFull := currentSOC >= vppMinFullCapacitySOC
			if canGridCharge && !isApproximatelyFull {
				topUpDesc := "VPP Prep emergency top-up."
				if stepIdx == 0 {
					topUpDesc = fmt.Sprintf("VPP Prep emergency top-up before %s ($%.3f/kWh)", vpp.eventStart.Format("15:04"), interval.importRate)
				}
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   defaultSolarMode,
					reason:      types.ActionReasonVPPPrep,
					description: topUpDesc,
					targetSOC:   100,
					isTruePeak:  isTruePeak,
					logFn: func(ctx context.Context, selected bool) {
						planLog(ctx, selected, "VPP 2-hour prep emergency top-up",
							slog.Time("stepTime", stepTime),
							slog.Time("vppDeadline", vpp.deadline),
							slog.Time("vppStart", vpp.eventStart),
							slog.Float64("currentSOC", currentSOC),
						)
					},
				})
				return candidates
			}

			// When approximately full (or if grid charging is physically unavailable), enforce standby lock
			// until the VPP event starts.
			standbyDesc := "VPP Standby Lock."
			if stepIdx == 0 {
				standbyDesc = fmt.Sprintf("VPP Standby Lock until %s", vpp.eventStart.Format("15:04"))
			}
			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeStandby,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonVPPPrep,
				description: standbyDesc,
				isTruePeak:  isTruePeak,
				logFn: func(ctx context.Context, selected bool) {
					planLog(ctx, selected, "VPP 2-hour prep standby lock",
						slog.Time("stepTime", stepTime),
						slog.Time("vppDeadline", vpp.deadline),
						slog.Time("vppStart", vpp.eventStart),
						slog.Float64("currentSOC", currentSOC),
					)
				},
			})
			if canDirectSolarExport {
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeStandby,
					solarMode:   types.SolarModeExport,
					reason:      types.ActionReasonDirectExport,
					description: "Direct Solar Export during VPP prep.",
					isTruePeak:  isTruePeak,
					logFn: func(ctx context.Context, selected bool) {
						planLog(ctx, selected, "VPP 2-hour prep direct solar export",
							slog.Time("stepTime", stepTime),
							slog.Time("vppDeadline", vpp.deadline),
							slog.Time("vppStart", vpp.eventStart),
							slog.Float64("currentSOC", currentSOC),
						)
					},
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
					homeKW := currentStatus.HomeKW
					candidates = append(candidates, actionCandidate{
						batteryMode: types.BatteryModeStandby,
						solarMode:   defaultSolarMode,
						reason:      types.ActionReasonEVChargingStandby,
						description: fmt.Sprintf("EV Charging Active (%.1fkW load). Standby to protect battery.", stepKW),
						isTruePeak:  isTruePeak,
						logFn: func(ctx context.Context, selected bool) {
							planLog(ctx, selected, "active EV charging detected, battery locked in standby",
								slog.Time("stepTime", stepTime),
								slog.Float64("homeKW", homeKW),
								slog.Float64("stepKW", stepKW),
								slog.Time("periodStart", period.Start),
								slog.Time("periodEnd", period.End),
							)
						},
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
		importRate := interval.importRate
		if canChargeNegative {
			desc := "Price below threshold. Charging battery from grid."
			if stepIdx == 0 {
				desc = fmt.Sprintf("Price below threshold (%.3f). Charging battery from grid.", interval.importRate)
			}
			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeChargeAny,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonAlwaysChargeBelowThreshold,
				description: desc,
				targetSOC:   100,
				isTruePeak:  isTruePeak,
				logFn: func(ctx context.Context, selected bool) {
					planLog(ctx, selected, "negative or force-charge threshold charging",
						slog.Time("stepTime", stepTime),
						slog.Float64("importRate", importRate),
						slog.Float64("currentSOC", currentSOC),
						slog.Float64("headroomKWH", headroomKWH),
						slog.Bool("isAlreadyCharging", isAlreadyCharging),
					)
				},
			})
			// If user configured an explicit AlwaysChargeUnder threshold and battery has headroom,
			// enforce charging as the designated action.
			if isForceChargeThreshold {
				return candidates
			}
		}

		// Standby preserves battery and allows home to consume free/cheap grid power
		desc := "Price below threshold. Preserving battery in standby; home powered from grid."
		if stepIdx == 0 {
			desc = fmt.Sprintf("Price below threshold (%.3f). Preserving battery in standby; home powered from grid.", interval.importRate)
		}
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonAlwaysChargeBelowThreshold,
			description: desc,
			isTruePeak:  isTruePeak,
			logFn: func(ctx context.Context, selected bool) {
				planLog(ctx, selected, "negative or force-charge threshold standby",
					slog.Time("stepTime", stepTime),
					slog.Float64("importRate", importRate),
					slog.Float64("currentSOC", currentSOC),
					slog.Float64("headroomKWH", headroomKWH),
					slog.Bool("isAlreadyCharging", isAlreadyCharging),
				)
			},
		})
		return candidates
	}

	// --- PRUNING RULE 5: Reserve Floor Protection ---
	isAboveReserve := currentSOC > effectiveReserveSOC
	// Battery grid export requires an export safety margin above the effective reserve floor.
	canExport := currentSOC > effectiveReserveSOC+exportReserveMarginPct

	// Evaluate daytime solar refill for MinExportHoldDifferenceDollarsPerKWH:
	// If daytime solar is expected and tonight's import rate is within minHoldDiff of export credit:
	// Discharging tonight to save cheap grid power when solar will refill tomorrow causes unnecessary cycling.
	hasUpcomingSolarRefill := false
	var refillExportRate float64
	var earliestSolarRefillTime time.Time
	if settings.GridExportSolar && !isFlatNEM && isAboveReserve && settings.MinExportHoldDifferenceDollarsPerKWH > 0 {
		minHoldDiff := settings.MinExportHoldDifferenceDollarsPerKWH
		for i := stepIdx + 1; i < len(timeline); i++ {
			if timeline[i].solarKWH > minSignificantSolarKW*timeline[i].durationHours && interval.importRate <= timeline[i].exportRate+minHoldDiff+priceEpsilonForEquality {
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
	vppBeforeSolarRefill := hasVPPAhead && hasUpcomingSolarRefill && !earliestSolarRefillTime.Before(nearestVPP.deadline)

	// Self-consumption respects effective reserve floor
	canDischarge := isAboveReserve

	// Build permissible candidate branches:
	// Branch A: Standard Self-Consumption (BatteryModeLoad)
	if canDischarge {
		reason := types.ActionReasonSufficientBattery
		desc := "Discharging battery to cover household load."
		if isTruePeak {
			reason = types.ActionReasonDischargeAtPeak
			desc = fmt.Sprintf("Discharging battery to power home during peak rate ($%.3f/kWh).", interval.importRate)
		}
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   defaultSolarMode,
			reason:      reason,
			description: desc,
			isTruePeak:  isTruePeak,
			logFn: func(ctx context.Context, selected bool) {
				if !selected {
					return
				}
				planLog(ctx, selected, "discharging battery",
					slog.Time("stepTime", stepTime),
					slog.Float64("refillExportRate", refillExportRate),
					slog.Float64("currentSOC", currentSOC),
					slog.Float64("effectiveReserveSOC", effectiveReserveSOC),
				)
			},
		})
	} else if !isAboveReserve {
		// At or below reserve: battery hardware protects reserve; passthrough mode
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonBatteryAtReserve,
			description: "Battery at reserve. Home powered from grid/solar.",
			isTruePeak:  isTruePeak,
			logFn: func(ctx context.Context, selected bool) {
				if !selected {
					return
				}
				planLog(ctx, selected, "battery at reserve, standby",
					slog.Time("stepTime", stepTime),
					slog.Float64("refillExportRate", refillExportRate),
					slog.Float64("currentSOC", currentSOC),
					slog.Float64("effectiveReserveSOC", effectiveReserveSOC),
				)
			},
		})
	}

	// Branch B: Standby (Preserve battery charge for upcoming peak rates, solar export hold, or evaluate inertia)
	var maxFutureRate float64
	var peakTime time.Time
	for i := stepIdx + 1; i < len(timeline); i++ {
		if timeline[i].importRate > maxFutureRate {
			maxFutureRate = timeline[i].importRate
			peakTime = timeline[i].startTime
		}
	}
	hasHigherFutureRate := maxFutureRate > interval.importRate+priceMaterialityThresholdDollars

	shouldOfferStandby := len(timeline) == 0 || hasHigherFutureRate || hasUpcomingSolarRefill || hasVPPAhead || isAlreadyStandby
	if shouldOfferStandby {
		reason := types.ActionReasonDeficitSaveForPeak
		desc := "Preserving battery in standby for upcoming peak rates."
		if vppBeforeSolarRefill {
			reason = types.ActionReasonVPPPrep
			desc = "Preserving battery in standby ahead of VPP event."
		} else if hasUpcomingSolarRefill {
			reason = types.ActionReasonHoldSimilarPrice
			desc = fmt.Sprintf("Grid price ($%.3f) is close to solar export credit ($%.3f). Preserving battery in standby for daytime solar export.", interval.importRate, refillExportRate)
		} else if hasHigherFutureRate {
			reason = types.ActionReasonDeficitSaveForPeak
			if !peakTime.IsZero() {
				desc = fmt.Sprintf("Preserving battery in standby for upcoming peak rates at %s ($%.3f/kWh).", peakTime.Format("15:04"), maxFutureRate)
			}
		} else if hasVPPAhead {
			reason = types.ActionReasonVPPPrep
			desc = "Preserving battery in standby ahead of VPP event."
		} else if isAlreadyStandby && len(timeline) > 0 {
			reason = types.ActionReasonHoldSimilarPrice
			desc = "Maintaining standby mode."
		}
		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   defaultSolarMode,
			reason:      reason,
			description: desc,
			isTruePeak:  isTruePeak,
			logFn: func(ctx context.Context, selected bool) {
				if !selected {
					return
				}
				planLog(ctx, selected, "battery standby",
					slog.Time("stepTime", stepTime),
					slog.Float64("refillExportRate", refillExportRate),
					slog.Time("earliestSolarRefillTime", earliestSolarRefillTime),
					slog.Float64("currentSOC", currentSOC),
					slog.Float64("effectiveReserveSOC", effectiveReserveSOC),
					slog.Float64("higherFutureRate", maxFutureRate),
					slog.Time("vppEventDeadline", vppEventDeadline),
					slog.Bool("hasUpcomingSolarRefill", hasUpcomingSolarRefill),
					slog.Bool("hasVPPAhead", hasVPPAhead),
					slog.Bool("vppBeforeSolarRefill", vppBeforeSolarRefill),
					slog.Bool("isAlreadyStandby", isAlreadyStandby),
					slog.String("reason", string(reason)),
				)
			},
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
			minDeficitDiff := max(priceEpsilonForEquality, settings.MinDeficitPriceDifferenceDollarsPerKWH)
			minArbitrageDiff := max(priceEpsilonForEquality, settings.MinArbitrageDifferenceDollarsPerKWH)
			rechargeCost := interval.importRate
			if roundTripEff > 0 && roundTripEff <= 1.0 {
				rechargeCost = interval.importRate / roundTripEff
			}

			hasArbitrageAhead := false
			chargeReason := types.ActionReasonDeficitChargeNow
			chargeDesc := ""
			var earliestArbitrageTime time.Time
			var futureExportRate float64
			var futurePeakRate float64

			var maxFutureImportRate float64
			var maxFutureExportRate float64

			for i := stepIdx + 1; i < len(timeline); i++ {
				if timeline[i].importRate > maxFutureImportRate {
					maxFutureImportRate = timeline[i].importRate
				}
				if timeline[i].exportRate > maxFutureExportRate {
					maxFutureExportRate = timeline[i].exportRate
				}

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
				canBatteryExportAhead := canExport && settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM && canCompleteBeforeVPPRecharge
				if canBatteryExportAhead && timeline[i].exportRate-rechargeCost >= minArbitrageDiff {
					futureExportRate = timeline[i].exportRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonArbitrageChargeExport
					chargeDesc = fmt.Sprintf("Pre-charging for upcoming battery export arbitrage ($%.3f/kWh now vs $%.3f/kWh export ahead).", interval.importRate, timeline[i].exportRate)
					earliestArbitrageTime = timeline[i].startTime
					break
				}

				// 2. Solar export credit arbitrage: charging now enables daytime rooftop solar export at high export rates.
				// Evaluated on nominal price spread (pre-losses) because charging from the grid displaces solar energy that would
				// have recharged the battery anyway. In both cases, the battery is cycled once, and the surplus solar exports
				// directly through the inverter without electrochemical storage losses. Applying a round-trip efficiency
				// penalty here would double-count battery losses and be overly conservative.
				canSolarExportAhead := canCompleteBeforeVPPRecharge && settings.GridExportSolar && !isFlatNEM && timeline[i].solarKWH > minSignificantSolarKW*timeline[i].durationHours && timeline[i].exportRate-interval.importRate >= minArbitrageDiff
				if canSolarExportAhead {
					futureExportRate = timeline[i].exportRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonArbitrageChargeExport
					chargeDesc = fmt.Sprintf("Pre-charging for upcoming solar export arbitrage ($%.3f/kWh now vs $%.3f/kWh export ahead).", interval.importRate, timeline[i].exportRate)
					earliestArbitrageTime = timeline[i].startTime
					break
				}
				// 3. Peak import rate arbitrage: charging now avoids buying expensive peak grid power later
				if timeline[i].importRate-rechargeCost >= minDeficitDiff {
					futurePeakRate = timeline[i].importRate
					hasArbitrageAhead = true
					chargeReason = types.ActionReasonDeficitChargeNow
					chargeDesc = fmt.Sprintf("Pre-charging for upcoming peak rates ($%.3f/kWh now vs higher peak ahead).", interval.importRate)
					earliestArbitrageTime = timeline[i].startTime
					break
				}
			}

			// When both an upcoming VPP event and an upcoming arbitrage/peak rate exist, consolidate
			// into a single BatteryModeChargeAny candidate to prevent redundant state-space expansion.
			// Arbitrage takes preference only if it occurs sooner with sufficient runway before the
			// pre-VPP recharge deadline (allowing the battery to discharge and fully recharge to 100%
			// before the VPP event). Otherwise, VPP prep takes precedence.
			hasVPPAhead := nearestVPP != nil
			if hasArbitrageAhead || hasVPPAhead {
				selectedReason := types.ActionReasonVPPPrep
				selectedDesc := "VPP Pre-charging before deadline."
				if stepIdx == 0 && hasVPPAhead {
					selectedDesc = fmt.Sprintf("VPP Pre-charging before deadline %s ($%.3f/kWh)", nearestVPP.deadline.Format("15:04"), interval.importRate)
				}

				isArbitrageSooner := hasArbitrageAhead && (!hasVPPAhead || earliestArbitrageTime.Before(nearestVPPRechargeDeadline))
				if isArbitrageSooner {
					selectedReason = chargeReason
					selectedDesc = chargeDesc
				}

				importRate := interval.importRate
				candidates = append(candidates, actionCandidate{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   defaultSolarMode,
					reason:      selectedReason,
					description: selectedDesc,
					isTruePeak:  isTruePeak,
					logFn: func(ctx context.Context, selected bool) {
						if isArbitrageSooner {
							planLog(ctx, selected, "grid arbitrage pre-charge",
								slog.Time("stepTime", stepTime),
								slog.Float64("importRate", importRate),
								slog.String("reason", string(selectedReason)),
								slog.Float64("roundTripEff", roundTripEff),
								slog.Float64("rechargeCost", rechargeCost),
								slog.Float64("futureExportRate", futureExportRate),
								slog.Float64("futurePeakRate", futurePeakRate),
								slog.Float64("minArbitrageDiff", minArbitrageDiff),
								slog.Float64("minDeficitDiff", minDeficitDiff),
								slog.Time("earliestArbitrageTime", earliestArbitrageTime),
								slog.Time("vppRechargeDeadline", vppRechargeDeadline),
							)
						} else {
							var vppDeadline time.Time
							if nearestVPP != nil {
								vppDeadline = nearestVPP.deadline
							}
							planLog(ctx, selected, "VPP pre-charge before deadline",
								slog.Time("stepTime", stepTime),
								slog.Time("vppDeadline", vppDeadline),
								slog.Float64("importRate", importRate),
								slog.Float64("roundTripEff", roundTripEff),
								slog.Float64("rechargeCost", rechargeCost),
								slog.Float64("futureExportRate", futureExportRate),
								slog.Float64("futurePeakRate", futurePeakRate),
								slog.Float64("minArbitrageDiff", minArbitrageDiff),
								slog.Float64("minDeficitDiff", minDeficitDiff),
								slog.Time("earliestArbitrageTime", earliestArbitrageTime),
								slog.Time("vppRechargeDeadline", vppRechargeDeadline),
							)
						}
					},
				})
			}
		}
	}

	// Branch D: Direct Solar Export (with BatteryModeLoad)
	if canDirectSolarExport && beforeVPPRechargeDeadline {
		reason := types.ActionReasonDirectExport
		desc := "Direct Solar Export: battery covers load; solar exports."
		if !canDischarge {
			reason = types.ActionReasonBatteryAtReserve
			desc = "Direct Solar Export at reserve: home powered from solar/grid; surplus solar exports."
		}

		exportRate := interval.exportRate
		importRate := interval.importRate
		solarKWH := interval.solarKWH

		candidates = append(candidates, actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeExport,
			reason:      reason,
			description: desc,
			isTruePeak:  isTruePeak,
			logFn: func(ctx context.Context, selected bool) {
				planLog(ctx, selected, "direct solar export",
					slog.Time("stepTime", stepTime),
					slog.String("reason", string(reason)),
					slog.Float64("exportRate", exportRate),
					slog.Float64("importRate", importRate),
					slog.Float64("solarKWH", solarKWH),
					slog.Float64("currentSOC", currentSOC),
					slog.Float64("effectiveReserveSOC", effectiveReserveSOC),
					slog.Bool("canDischarge", canDischarge),
					slog.Time("vppRechargeDeadline", vppRechargeDeadline),
				)
			},
		})
	}

	// Branch E: Battery Grid Export Dump (Opportunity cost of home offset + degradation hurdle)
	if settings.ManageTOUSchedules && settings.GridExportBatteries && !isFlatNEM && canExport && beforeVPPRechargeDeadline && interval.exportRate > 0 {
		cycleHurdle := settings.MinBatteryExportDifferenceDollarsPerKWH

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
		rechargeCost := minAlternativeValue
		if roundTripEff > 0 && roundTripEff <= 1.0 {
			rechargeCost = minAlternativeValue / roundTripEff
		}

		// Export is a viable candidate if export rate beats the replacement cost by at least the degradation hurdle
		if interval.exportRate >= (rechargeCost + cycleHurdle) {
			exportRate := interval.exportRate
			targetSOC := int(math.Round(effectiveReserveSOC + exportReserveMarginPct))
			desc := "Battery Grid Export Dump."
			if stepIdx == 0 {
				desc = fmt.Sprintf("Battery Grid Export Dump ($%.3f/kWh)", interval.exportRate)
			}
			candidates = append(candidates, actionCandidate{
				batteryMode: types.BatteryModeExport,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonDirectExport,
				description: desc,
				targetSOC:   targetSOC,
				isTruePeak:  isTruePeak,
				logFn: func(ctx context.Context, selected bool) {
					planLog(ctx, selected, "battery grid export dump",
						slog.Time("stepTime", stepTime),
						slog.Float64("exportRate", exportRate),
						slog.Int("targetSOC", targetSOC),
						slog.Float64("rechargeCost", rechargeCost),
						slog.Float64("minAlternativeValue", minAlternativeValue),
						slog.Float64("roundTripEff", roundTripEff),
						slog.Float64("cycleHurdle", cycleHurdle),
					)
				},
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

	_, reserveBufferPct, _ := settings.GetOptimizationParams()
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
		batSuppliedHomeKWH:       batSuppliedHomeKWH,
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
	action    actionCandidate
	metric    intervalMetrics
	parent    *dpNode
}

// calculateVPPFeasibilityPenalty calculates a prohibitive penalty for failing to meet a VPP deadline.
// It determines if 100% SOC is physically reachable given initial SOC, lead time, and max charge power.
// If reachable target is not met, a prohibitive penalty ($1,000 base + $100 per % deficit) is applied,
// effectively rejecting any plan that fails to charge by the deadline because the ESS hardware takes over.
func calculateVPPFeasibilityPenalty(
	initialState planState,
	candidateState planState,
	deadline time.Time,
	oneWayEff float64,
) float64 {
	if initialState.chargingDisabled {
		return 0.0
	}
	leadTimeHours := deadline.Sub(initialState.time).Hours()
	if leadTimeHours <= 0 {
		targetSOC := 100.0
		if candidateState.soc < targetSOC-socTargetTolerancePct {
			return 1000.0 + (targetSOC-candidateState.soc)*100.0
		}
		return 0.0
	}
	capacityKWH := initialState.capacityKWH
	maxChargeKW := resolveBatteryPowerKW(initialState.maxChargeKW, capacityKWH)
	maxChargeEnergy := maxChargeKW * leadTimeHours * oneWayEff
	targetSOC := min(100.0, initialState.soc+(maxChargeEnergy/capacityKWH)*100.0)

	if candidateState.soc < targetSOC-socTargetTolerancePct {
		deficitSOC := targetSOC - candidateState.soc
		// If 100% was physically reachable from the start of the horizon, apply a prohibitive penalty
		// ($1,000 base + $100 per % deficit) so the solver rejects any undercharged trajectory.
		if targetSOC >= 100.0-socTargetTolerancePct {
			return 1000.0 + (deficitSOC * 100.0)
		}
		// If physical lead time was constrained (cannot reach 100%), heavily penalize any deficit
		// below the maximum achievable charge so the solver maximizes stored energy.
		return deficitSOC * 10.0
	}
	return 0.0
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

	// Step 0 candidate generation
	candidates0 := c.generateActionCandidates(ctx, 0, timeline[0], timeline, initial, anchors, settings, currentStatus, history, toPrecedingAction(lastAction))
	if len(candidates0) == 0 {
		return nil, fmt.Errorf("no viable plan found: all candidate actions pruned at step 0")
	}

	modeBestScores := make(map[types.BatteryMode]float64)
	modeBestNodes := make(map[types.BatteryMode]*dpNode)
	totalPathsEvaluated := 0

	roundTripEff, _, _ := settings.GetOptimizationParams()
	oneWayEff := math.Sqrt(roundTripEff)
	capacityKWH := currentStatus.BatteryCapacityKWH
	if capacityKWH <= 0 {
		capacityKWH = initial.capacityKWH
	}
	cycleHurdle := settings.MinBatteryExportDifferenceDollarsPerKWH
	holdHurdle := settings.MinExportHoldDifferenceDollarsPerKWH
	isFlatNEM := isFlatNetMetering(settings.UtilityRateOptions)

	// Pre-calculate whether daytime solar refill is projected ahead for each timeline interval
	// Discharging is only penalized if tonight's import rate is within holdHurdle of daytime solar export credit.
	solarRefillAhead := make([]bool, len(timeline))
	for i := 0; i < len(timeline); i++ {
		for j := i + 1; j < len(timeline); j++ {
			if timeline[j].solarKWH > minSignificantSolarKW*timeline[j].durationHours && timeline[i].importRate <= timeline[j].exportRate+holdHurdle+priceEpsilonForEquality {
				solarRefillAhead[i] = true
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
	// Pre-calculate the maximum future import rate for each step across the remaining horizon.
	// Used in DP intra-bucket dominance comparisons to accurately value retained battery energy
	// against upcoming peak periods rather than undervaluing it at the current off-peak rate.
	maxFutureImportRates := make([]float64, len(timeline))
	for i := len(timeline) - 1; i >= 0; i-- {
		rate := timeline[i].importRate
		if i < len(timeline)-1 && maxFutureImportRates[i+1] > rate {
			rate = maxFutureImportRates[i+1]
		}
		if anchors.knownPostHorizonRate > rate {
			rate = anchors.knownPostHorizonRate
		}
		maxFutureImportRates[i] = rate
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
						parent.action.toPrecedingAction(),
					)
				}
				for _, cand := range candidates {
					nextState, metrics := stepPhysics(parent.state, cand, interval, settings, roundTripEff)

					var holdCost float64
					if settings.GridExportSolar && !isFlatNEM && solarRefillAhead[stepIdx] && holdHurdle > 0 {
						holdCost = metrics.batSuppliedHomeKWH * holdHurdle
					}

					var transitionCost float64
					if stepIdx == 0 {
						// Inertia penalty: if switching away from lastAction at step 0, add default penalty
						// scaled by interval duration with a minimum floor to prevent rapid mode oscillation.
						if lastAction != nil && cand.batteryMode != lastAction.BatteryMode {
							transitionCost = max(0.01, defaultInertiaThresholdDollars*min(1.0, interval.durationHours))
						} else if lastAction == nil && cand.batteryMode == types.BatteryModeChargeAny {
							transitionCost = chargeSessionStartPenalty
						}
					} else if parent.action.batteryMode != types.BatteryModeChargeAny && cand.batteryMode == types.BatteryModeChargeAny {
						transitionCost = chargeSessionStartPenalty
					}

					newCost := parent.totalCost + metrics.costDollars + (metrics.batExportKWH * cycleHurdle) + holdCost + transitionCost + (nextState.energyKWH * interval.durationHours * batteryHoldingCostPerHourPerKWH)

					// Check VPP deadline feasibility: apply penalty only for nearest upcoming event deadline
					if hasNearestVPP {
						if interval.endTime.Equal(nearestVPPDeadline) || (interval.startTime.Before(nearestVPPDeadline) && interval.endTime.After(nearestVPPDeadline)) {
							newCost += calculateVPPFeasibilityPenalty(initial, nextState, nearestVPPDeadline, oneWayEff)
						}
					}

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
						// Dominance comparison within the same mode: adjust by marginal residual value of SOC difference.
						socDelta := nextState.soc - nextBuckets[mIdx][b].state.soc
						energyDeltaKWH := (socDelta / 100.0) * capacityKWH
						valuationRate := maxFutureImportRates[stepIdx]
						marginalValue := energyDeltaKWH * oneWayEff * valuationRate
						effectiveNewCost := newCost - marginalValue
						if effectiveNewCost < nextBuckets[mIdx][b].totalCost {
							isBetter = true
						}
					}

					if isBetter {
						nextBuckets[mIdx][b] = &dpNode{
							state:     nextState,
							totalCost: newCost,
							action:    cand,
							metric:    metrics,
							parent:    parent,
						}
					}
				}
			}

			// Collect surviving bucket nodes for the next time step
			activeNodes = activeNodes[:0]
			for m := 0; m < 4; m++ {
				for _, n := range nextBuckets[m] {
					if n != nil {
						activeNodes = append(activeNodes, n)
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

		for _, node := range activeNodes {
			totalPathsEvaluated++
			termVal := calculateTerminalValuation(node.state, anchors, settings, timeline, capacityKWH, roundTripEff)
			score := node.totalCost + termVal
			if score < bestScoreForCand {
				bestScoreForCand = score
				bestNodeForCand = node
			}
		}

		if bestNodeForCand != nil {
			prevScore, exists := modeBestScores[cand0.batteryMode]
			if !exists || bestScoreForCand < prevScore {
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
	var runnerUpScore float64 = math.MaxFloat64

	for _, mode := range modePriority {
		score, exists := modeBestScores[mode]
		if !exists {
			continue
		}
		node := modeBestNodes[mode]
		if score < bestOverallScore-priceEpsilonForEquality {
			if bestOverallNode != nil {
				runnerUpScore = bestOverallScore
			}
			bestOverallScore = score
			bestOverallNode = node
		} else if score < runnerUpScore-priceEpsilonForEquality {
			runnerUpScore = score
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
				if !exists || chargeNode == nil {
					slog.Warn("Should fast-track grid charge but no valid ChargeAny node exists in plan search",
						slog.Time("scheduledChargeTime", fastTrackStartTime),
						slog.Duration("delay", fastTrackDelay),
						slog.Float64("step0ImportRate", timeline[0].importRate),
						slog.Float64("batterySOC", currentStatus.BatterySOC),
						slog.Float64("headroomKWH", headroomKWH),
						slog.String("currentMode", drModeString(actions[0].batteryMode)))
				} else {
					originalMode := actions[0].batteryMode
					originalScore := bestOverallScore
					var originalCost float64
					for _, m := range metrics {
						originalCost += m.costDollars
					}

					bestOverallNode = chargeNode
					bestOverallScore = modeBestScores[types.BatteryModeChargeAny]

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

					slog.Debug("Fast-tracking scheduled grid charge to step 0 due to polling cycle",
						slog.Time("scheduledChargeTime", fastTrackStartTime),
						slog.Duration("delay", fastTrackDelay),
						slog.Float64("step0ImportRate", timeline[0].importRate),
						slog.Float64("batterySOC", currentStatus.BatterySOC),
						slog.Float64("headroomKWH", headroomKWH),
						slog.String("originalMode", drModeString(originalMode)),
						slog.Float64("originalScore", originalScore),
						slog.Float64("originalCostDollars", originalCost),
						slog.String("promotedMode", drModeString(actions[0].batteryMode)),
						slog.String("promotedReason", string(actions[0].reason)),
						slog.Float64("promotedScore", bestOverallScore),
						slog.Float64("promotedCostDollars", promotedCost))
				}
			}
		}
	}

	var totalPhysicalCost float64
	for _, m := range metrics {
		totalPhysicalCost += m.costDollars
	}

	bestPath := &planPath{
		actions:           actions,
		states:            states,
		metrics:           metrics,
		totalCost:         totalPhysicalCost,
		initialCandidates: candidates0,
		modeScores:        modeBestScores,
		bestScore:         bestOverallScore,
	}

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
	targetReserveSOC := settings.MinBatterySOC
	if len(timeline) > 0 && timeline[len(timeline)-1].minSOC > 0 {
		targetReserveSOC = timeline[len(timeline)-1].minSOC
	}
	targetEnergyKWH := capacityKWH * (targetReserveSOC / 100.0)

	// Determine replacement rate at horizon end
	replacementRate := anchors.knownPostHorizonRate

	minAcceptableReserveKWH := capacityKWH * ((targetReserveSOC - reserveFloorTolerancePct) / 100.0)
	energyDeltaKWH := finalState.energyKWH - targetEnergyKWH
	oneWayEff := math.Sqrt(roundTripEfficiency)

	if finalState.energyKWH < minAcceptableReserveKWH {
		// Ending below target reserve (beyond the 0.1% ESS hardware deadband): penalize heavily to eliminate arbitrage
		deficitKWH := targetEnergyKWH - finalState.energyKWH
		deficitPenaltyRate := max(1.0, replacementRate*3.0)
		return (deficitKWH / oneWayEff) * deficitPenaltyRate
	}

	if energyDeltaKWH <= 0 {
		// Within the +/- 0.1% ESS hardware tolerance of the reserve floor: no penalty, no credit
		return 0.0
	}

	// Ending above target reserve: credit for banked energy displacing future imports at replacement rate.
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
) (types.ActionReason, string) {
	if winningPath == nil || stepIdx >= len(winningPath.actions) || stepIdx >= len(timeline) {
		return types.ActionReasonSufficientBattery, "Discharging battery to cover household load."
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
		return action.reason, action.description
	}

	importRate := interval.importRate
	exportRate := interval.exportRate

	_, reserveBufferPct, _ := settings.GetOptimizationParams()
	effectiveReserveSOC := interval.minSOC + reserveBufferPct

	minDeficitDiff := max(minPeakRateSpreadDollars, settings.MinDeficitPriceDifferenceDollarsPerKWH)
	isTruePeak := action.isTruePeak || (maxHorizonImportRate > 0 &&
		importRate >= maxHorizonImportRate-priceMaterialityThresholdDollars &&
		maxHorizonImportRate >= minHorizonImportRate+minDeficitDiff)

	switch action.batteryMode {
	case types.BatteryModeExport:
		return types.ActionReasonDirectExport, fmt.Sprintf("Battery Grid Export Dump ($%.3f/kWh)", exportRate)

	case types.BatteryModeChargeAny:
		if action.reason == types.ActionReasonAlwaysChargeBelowThreshold ||
			importRate < 0 ||
			(settings.AlwaysChargeUnderDollarsPerKWH > 0 && importRate <= settings.AlwaysChargeUnderDollarsPerKWH) {
			return types.ActionReasonAlwaysChargeBelowThreshold,
				fmt.Sprintf("Grid price ($%.3f) is below threshold ($%.3f); pre-charging battery.", importRate, settings.AlwaysChargeUnderDollarsPerKWH)
		}
		if action.reason == types.ActionReasonVPPPrep {
			return types.ActionReasonVPPPrep, action.description
		}

		// Battery export or solar export arbitrage ahead in the plan
		for j := stepIdx + 1; j < len(timeline); j++ {
			jExportRate := timeline[j].exportRate
			if settings.GridExportBatteries && j < len(winningPath.metrics) && winningPath.metrics[j].batExportKWH > 0.1 {
				return types.ActionReasonArbitrageChargeExport,
					fmt.Sprintf("Pre-charging for upcoming battery export arbitrage ($%.3f/kWh now vs $%.3f/kWh export ahead).", importRate, jExportRate)
			}
			if settings.GridExportSolar && j < len(winningPath.metrics) && (winningPath.metrics[j].gridExportKWH-winningPath.metrics[j].batExportKWH > 0.1 || winningPath.metrics[j].gridExportKWH > 0.1) {
				return types.ActionReasonArbitrageChargeExport,
					fmt.Sprintf("Pre-charging for upcoming solar export arbitrage ($%.3f/kWh now vs $%.3f/kWh export ahead).", importRate, jExportRate)
			}
		}

		return types.ActionReasonDeficitChargeNow,
			fmt.Sprintf("Pre-charging for upcoming peak rates ($%.3f/kWh now vs higher peak ahead).", importRate)

	case types.BatteryModeStandby:
		if action.reason == types.ActionReasonVPPPrep {
			return types.ActionReasonVPPPrep, action.description
		}
		if action.reason == types.ActionReasonHoldSimilarPrice && action.description != "" {
			return action.reason, action.description
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
				chargeImportRate := timeline[chargeIdx].importRate
				hasInterveningPeak := false
				for p := stepIdx + 1; p < chargeIdx; p++ {
					if timeline[p].importRate > importRate+priceMaterialityThresholdDollars {
						hasInterveningPeak = true
						break
					}
				}
				if !hasInterveningPeak {
					if chargeImportRate < importRate-priceMaterialityThresholdDollars {
						return types.ActionReasonWaitingToCharge,
							fmt.Sprintf("Waiting to charge at %s ($%.3f < $%.3f).",
								timeline[chargeIdx].startTime.Format("15:04"), chargeImportRate, importRate)
					}
					return types.ActionReasonWaitingToCharge,
						fmt.Sprintf("Waiting to charge at %s ($%.3f/kWh).",
							timeline[chargeIdx].startTime.Format("15:04"), chargeImportRate)
				}
			}
		}

		// 2. ArbitrageHoldExport: Upcoming export window (battery or solar export) scheduled in plan
		for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
			if winningPath.actions[j].batteryMode == types.BatteryModeExport ||
				(winningPath.actions[j].solarMode == types.SolarModeExport && timeline[j].solarKWH > minSignificantSolarKW*timeline[j].durationHours) {
				return types.ActionReasonArbitrageHoldExport,
					fmt.Sprintf("Preserving battery in standby for upcoming export window at %s ($%.3f/kWh).",
						timeline[j].startTime.Format("15:04"), timeline[j].exportRate)
			}
		}

		// 3. Fallback to candidate reason and description (e.g. DeficitSaveForPeak with peak time and rate)
		return action.reason, action.description

	case types.BatteryModeLoad:
		// 1. Direct Solar Export
		if action.solarMode == types.SolarModeExport && interval.solarKWH > 0 {
			if state.soc > effectiveReserveSOC {
				return types.ActionReasonDirectExport, "Direct Solar Export: battery covers load; solar exports."
			}
			return types.ActionReasonBatteryAtReserve, "Direct Solar Export at reserve: home powered from solar/grid; surplus solar exports."
		}

		// 2. Battery At Reserve
		if state.soc <= effectiveReserveSOC+reserveFloorTolerancePct && metrics.batSuppliedHomeKWH <= minBatteryDeliveredEnergyKWH {
			return types.ActionReasonBatteryAtReserve, "Battery is at reserve. Home powered from solar/grid."
		}

		// 3. DischargeAtPeak: Currently at or near peak import rate
		if isTruePeak {
			return types.ActionReasonDischargeAtPeak,
				fmt.Sprintf("Discharging battery to power home during peak rate ($%.3f/kWh).", importRate)
		}

		// 4. SufficientBatteryTillCharge: A future ChargeAny is planned, and battery will reach it without hitting reserve
		for j := stepIdx + 1; j < len(winningPath.actions) && j < len(timeline); j++ {
			if winningPath.actions[j].batteryMode == types.BatteryModeChargeAny {
				jChargeRate := timeline[j].importRate
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
							fmt.Sprintf("Sufficient battery to reach scheduled charge at %s ($%.3f/kWh).",
								timeline[j].startTime.Format("15:04"), jChargeRate)
					}
				}
				break
			}
		}

		// 5. PreventSolarCurtailment: Upcoming solar will exceed battery headroom and would be curtailed
		for j := stepIdx + 1; j < len(winningPath.metrics) && j < len(timeline); j++ {
			if winningPath.metrics[j].solarCurtailedKW > minSolarGenerationKW {
				return types.ActionReasonPreventSolarCurtailment,
					fmt.Sprintf("Solar generation forecast to exceed capacity by %s; discharging now to create headroom.",
						timeline[j].startTime.Format("15:04"))
			}
		}

		// 6. Default Load
		return types.ActionReasonSufficientBattery, "Discharging battery to cover household load."
	}

	return action.reason, action.description
}

// finalizeDecisionAndPlan packages the winning path into Decision and types.Plan.
func finalizeDecisionAndPlan(
	winningPath *planPath,
	timeline []planInterval,
	initialStatus types.SystemStatus,
	currentPrice types.Price,
	now time.Time,
	settings types.Settings,
) (Decision, types.Plan) {
	if len(winningPath.actions) == 0 || len(timeline) == 0 {
		return Decision{}, types.Plan{GeneratedAt: now.UTC()}
	}

	immediateInterval := timeline[0]

	// Contextual post-plan reason resolution:
	// Dynamic programming selects optimal modes across time, but homeowner-facing explanations
	// are contextual to the full trajectory (e.g., peak discharge, waiting for scheduled charge,
	// sufficient battery until charge, holding for daytime solar refill, or preventing solar curtailment).
	minHorizonImportRate := timeline[0].importRate
	maxHorizonImportRate := timeline[0].importRate
	for _, it := range timeline {
		if it.importRate > maxHorizonImportRate {
			maxHorizonImportRate = it.importRate
		}
		if it.importRate < minHorizonImportRate {
			minHorizonImportRate = it.importRate
		}
	}

	for i := 0; i < len(winningPath.actions) && i < len(timeline); i++ {
		reason, desc := resolvePlanActionReason(winningPath, i, timeline, settings, maxHorizonImportRate, minHorizonImportRate)
		winningPath.actions[i].reason = reason
		winningPath.actions[i].description = desc
	}
	immediateAction := winningPath.actions[0]

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

	// Extend TSScheduleModeUntil across the contiguous block of identical dispatch parameters
	scheduleUntil := immediateInterval.endTime
	for i := 1; i < len(winningPath.actions) && i < len(timeline); i++ {
		if winningPath.actions[i].batteryMode == immediateAction.batteryMode &&
			winningPath.actions[i].solarMode == immediateAction.solarMode &&
			(winningPath.actions[i].targetSOC == immediateAction.targetSOC || immediateAction.batteryMode == types.BatteryModeChargeAny || immediateAction.batteryMode == types.BatteryModeStandby) &&
			winningPath.actions[i].reason == immediateAction.reason {
			scheduleUntil = timeline[i].endTime
		} else {
			break
		}
	}

	act := types.Action{
		Timestamp:           now.UTC(),
		SystemTimestamp:     now,
		BatteryMode:         immediateAction.batteryMode,
		SolarMode:           immediateAction.solarMode,
		Reason:              immediateAction.reason,
		Description:         immediateAction.description,
		CurrentPrice:        &currentPrice,
		SystemStatus:        initialStatus,
		ChargeToSOC:         targetSOC,
		TSScheduleModeUntil: scheduleUntil,
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
			StartTime:        interval.startTime,
			EndTime:          interval.endTime,
			DurationHours:    interval.durationHours,
			Price:            interval.price,
			BatteryMode:      action.batteryMode,
			SolarMode:        action.solarMode,
			Reason:           action.reason,
			Description:      action.description,
			StartSOC:         startState.soc,
			EndSOC:           endState.soc,
			LoadKWH:          interval.loadKWH,
			SolarKWH:         interval.solarKWH,
			ProjectedLoadKW:  interval.avgLoadKW(),
			ProjectedSolarKW: interval.avgSolarKW(),
			GridImportKWH:    metrics.gridImportKWH,
			GridExportKWH:    metrics.gridExportKWH,
			CostDollars:      metrics.costDollars,
		})
	}

	horizonHours := int(math.Ceil(timeline[len(timeline)-1].endTime.Sub(timeline[0].startTime).Hours()))

	plan := types.Plan{
		GeneratedAt:        now.UTC(),
		HorizonHours:       horizonHours,
		TotalProjectedCost: totalCostDollars,
		TotalExportCredits: totalExportCredits,
		NetEconomicBenefit: totalExportCredits - totalCostDollars,
		Periods:            periods,
	}

	act.Plan = &plan

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
