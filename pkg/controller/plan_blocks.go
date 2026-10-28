package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// vppMinFullCapacitySOC is the minimum state of charge (99.0%) considered approximately full
	// when entering the VPP lead-time preparation window. If the battery enters this window below
	// 99.0% SOC, ESS firmware takes autonomous control to execute an emergency grid top-up to 100%
	// regardless of tariff rates. Standby is withheld below this threshold so the planner reflects
	// actual hardware dispatch rather than proposing unrealistic idle trajectories during expensive rates.
	vppMinFullCapacitySOC = 99.0

	// chargeCompletionBufferDuration (20 minutes, or 1 standard 20-minute interval) is the maximum
	// wait duration allowed before starting an emergency reserve deficit recharge.
	//
	// WARNING: This duration must never exceed 1-2 planning periods (and at 1 period / 20 minutes,
	// computeInitialDeficitWait only needs to inspect lastAction). Because RateRudder only persists the
	// most recent action (typically ~20 minutes ago), setting chargeCompletionBufferDuration to 3+ periods
	// (e.g. 60 minutes) would mean each new plan only sees 20 minutes of prior wait from lastAction and
	// allows delaying another 20-40 minutes—perpetually rolling the delay forward every poll and never
	// charging the battery. Keeping this at 20 minutes also ensures a user-configured 1-hour reserve window
	// is not unexpectedly delayed by 40 minutes before charging begins.
	chargeCompletionBufferDuration = 20 * time.Minute

	// planScoreTieEpsilon ($1e-6) is the floating-point tie-breaker tolerance when comparing
	// total horizon plan scores across candidate modes, allowing sub-mill-dollar holding and
	// survival buffer penalties (~$1e-4) to steer timing while breaking true floating-point ties deterministically.
	planScoreTieEpsilon = 1e-6

	// solarExportReserveMarginPct is the minimum SOC margin (in % SOC) required above the reserve floor
	// to initiate or sustain direct solar export (BatteryModeLoad + SolarModeExport). Because direct solar
	// export routes all solar to the grid while the battery covers household load, requiring a 5% buffer
	// prevents entering solar export when only 1–2% above reserve and immediately hitting the reserve floor.
	solarExportReserveMarginPct = 5.0

	// reserveDeficitChargeTolerancePct is the SOC tolerance (1.05% SOC) below interval.minSOC required
	// before triggering emergency reserve grid top-ups and hard deficit trajectory pruning. ESS BMS
	// telemetry (e.g. Tesla Powerwall) frequently reports a resting battery 1.0% below its configured
	// reserve floor (such as 19.0% on a 20% reserve or 9.0% on a 10% reserve) due to integer SOC
	// quantization and standby tare. Allowing 1.05% tolerance prevents 1% BMS drift from triggering
	// nuisance grid top-ups (including during peak rates), while still immediately recharging genuine
	// reserve deficits (e.g. <= 18.9% on a 20% reserve).
	reserveDeficitChargeTolerancePct = 1.05

	// almostFullSOC is the SOC threshold (98.0%) at or above which a battery is considered nearly full
	// for initiating a new grid charge, pruning redundant Standby holds, and detecting full solar refills.
	almostFullSOC = 98.0

	// dominatedPeriodPriceGapDollars is the minimum price difference ($/kWh) required to flag a
	// HoldThenCheaperExport or ExportThenCostlierHold sequence as dominated.
	dominatedPeriodPriceGapDollars = 0.005

	// targetTimelineBlocks is the target maximum number of timeline blocks after adaptive price-band merging
	// on a standard ~24-hour dynamic hourly tariff horizon (such as ComEd or Ameren).
	targetTimelineBlocks = 10

	// targetTimelineBlocksExtendedHorizon is the target maximum number of timeline blocks when
	// selectPlanningHorizonEnd extends the planning horizon past 26 hours (up to 34 hours) to include
	// the full Day 2 peak window and trailing off-peak valley. Allocating 2 extra blocks for those
	// appended Day 2 regimes prevents the greedy price-band merger from coalescing distinct Day 1
	// blocks just to fit the extended tail into the 24-hour block budget.
	targetTimelineBlocksExtendedHorizon = 12

	// maxPriceBandMergeDollars ($0.06/kWh) is the maximum rate difference allowed when merging adjacent
	// blocks toward targetTimelineBlocks. Blocks separated by more than $0.06/kWh are never merged so that
	// true wholesale price spikes remain isolated from adjacent off-peak or shoulder hours.
	maxPriceBandMergeDollars = 0.06
)

// timelineBlock represents a contiguous segment of timeline periods that share
// compatible tariff rates and operational constraints (same reserve floor, VPP state, and solar window).
type timelineBlock struct {
	startIdx       int     // Inclusive start interval index in timeline
	endIdx         int     // Inclusive end interval index in timeline
	durationHours  float64 // Total duration across [startIdx..endIdx] in hours
	minSOC         float64 // Active reserve floor (%) across this block
	avgImportRate  float64 // Duration-weighted average import rate ($/kWh)
	minImportRate  float64 // Lowest period import rate ($/kWh) inside the block
	maxImportRate  float64 // Highest period import rate ($/kWh) inside the block
	maxExportRate  float64 // Highest period export rate ($/kWh) inside the block
	minExportRate  float64 // Lowest period export rate ($/kWh) inside the block
	netSolarKWH    float64 // Total (solarKWH - loadKWH) across the block
	isLowestPrice  bool    // True if this block contains the horizon-wide minimum import rate
	isHighestPrice bool    // True if this block contains the horizon-wide maximum import rate (when spread is material)
}

type blockActionType int

const (
	blockActionLoad blockActionType = iota
	blockActionStandby
	blockActionDirectSolarExport
	blockActionCharge
	blockActionExport
)

func (t blockActionType) String() string {
	switch t {
	case blockActionLoad:
		return "Load"
	case blockActionStandby:
		return "Standby"
	case blockActionDirectSolarExport:
		return "DirectSolarExport"
	case blockActionCharge:
		return "Charge"
	case blockActionExport:
		return "Export"
	default:
		return "Unknown"
	}
}

// blockActionChoice specifies the canonical dispatch strategy for a single timelineBlock.
// Rather than branching at every 20-minute interval, each timelineBlock selects one canonical choice
// and expandBlockAction places any charging or exporting into a single contiguous sub-window.
type blockActionChoice struct {
	actionType           blockActionType
	targetSOC            float64 // Target ceiling SOC for blockActionCharge, or export floor SOC for blockActionExport
	useSolarExportPrefix bool    // For blockActionExport: if true, run DirectSolarExport before the battery export tail
}

func sameBlockChoice(a, b blockActionChoice) bool {
	return a.actionType == b.actionType &&
		math.Abs(a.targetSOC-b.targetSOC) < planScoreTieEpsilon &&
		a.useSolarExportPrefix == b.useSolarExportPrefix
}

// getHardOverride checks whether a specific interval is governed by a non-negotiable operational
// override (grid outage, active VPP event, mandatory 2-hour VPP firmware lock, step-0 active EV
// charging standby, or negative/free electricity import rate).
// Both buildTimelineBlocks (via hasHardSplit) and expandBlockAction call this function as the single
// source of truth so that every hard override boundary splits adjacent intervals into separate blocks.
func getHardOverride(
	stepIdx int,
	choice blockActionChoice,
	startState planState,
	timeline []planInterval,
	vppEvents []vppAnchor,
	settings types.Settings,
	currentStatus types.SystemStatus,
	defaultSolarMode types.SolarMode,
) (actionCandidate, bool) {
	it := timeline[stepIdx]
	if currentStatus.GridUnavailable {
		return actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonGridUnavailable,
			description: "Grid unavailable. Discharging battery to power home backup circuits.",
		}, true
	}
	// Active VPP event or mandatory 2-hour firmware preparation window
	for _, vpp := range vppEvents {
		if !it.startTime.Before(vpp.eventStart) && it.startTime.Before(vpp.eventEnd) {
			targetSOC := vpp.vppSoc
			if !vpp.mandatory {
				targetSOC = it.minSOC
			}
			if startState.soc <= targetSOC {
				desc := "VPP Target SOC reached. Preserving reserve in standby."
				if vpp.mandatory {
					desc = "Mandatory VPP Target SOC reached. Preserving reserve in standby."
				}
				return actionCandidate{
					batteryMode:        types.BatteryModeStandby,
					solarMode:          defaultSolarMode,
					reason:             types.ActionReasonVPPActive,
					description:        desc,
					overrideReserveSOC: targetSOC,
				}, true
			}
			batMode := types.BatteryModeLoad
			if (settings.ManageTOUSchedules && settings.GridExportBatteries) || vpp.price > 0 {
				batMode = types.BatteryModeExport
			}
			return actionCandidate{
				batteryMode:        batMode,
				solarMode:          defaultSolarMode,
				reason:             types.ActionReasonVPPActive,
				description:        "VPP Event Active.",
				targetSOC:          targetSOC,
				overrideReserveSOC: targetSOC,
			}, true
		}
		if vpp.mandatory {
			takeover := vpp.eventStart.Add(-vppStandbyLeadTime)
			if !it.startTime.Before(takeover) && it.startTime.Before(vpp.eventStart) {
				if startState.soc < vppMinFullCapacitySOC && settings.GridChargeBatteries && !currentStatus.BatteryChargingDisabled {
					return actionCandidate{
						batteryMode: types.BatteryModeChargeAny,
						solarMode:   defaultSolarMode,
						reason:      types.ActionReasonVPPPrep,
						description: "VPP Prep emergency top-up.",
						targetSOC:   100,
					}, true
				}
				return actionCandidate{
					batteryMode: types.BatteryModeStandby,
					solarMode:   defaultSolarMode,
					reason:      types.ActionReasonVPPPrep,
					description: "VPP Standby Lock.",
				}, true
			}
		}
	}
	// Step 0 active EV charging: lock into Standby when the block would otherwise discharge
	// (Load / DirectSolarExport) or sit in normal Standby, so the home battery does not dump
	// into the EV. Allow blockActionCharge and blockActionExport to bypass EV standby when the
	// planner must start charging or exporting immediately at step 0 (e.g. when a full hour of
	// charging is required before an upcoming rate increase).
	if stepIdx == 0 && choice.actionType != blockActionCharge && choice.actionType != blockActionExport && IsEVStandbyEligible(settings, timeline[0].startTime) {
		if isEV, _ := DetectEVCharging(context.Background(), currentStatus.HomeKW, nil); isEV {
			return actionCandidate{
				batteryMode: types.BatteryModeStandby,
				solarMode:   defaultSolarMode,
				reason:      types.ActionReasonEVChargingStandby,
				description: "EV Charging Active. Standby to protect battery.",
			}, true
		}
	}
	// Negative or free import price (<= $0.00/kWh)
	if it.importRate <= 0.0 {
		return actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   defaultSolarMode,
			reason:      types.ActionReasonAlwaysChargeBelowThreshold,
			description: "Price below threshold. Charging battery from grid.",
			targetSOC:   100,
		}, true
	}
	return actionCandidate{}, false
}

// buildTimelineBlocks segments the 20-minute planning timeline into ~6-10 contiguous timeline blocks
// separated by hard operational boundaries (VPP, active EV, reserve step-up/down, negative price)
// and groups similar tariff intervals within a configurable price band.
func buildTimelineBlocks(
	ctx context.Context,
	timeline []planInterval,
	vppEvents []vppAnchor,
	settings types.Settings,
	currentStatus types.SystemStatus,
) []timelineBlock {
	if len(timeline) == 0 {
		return nil
	}

	// 1. Identify hard override / boundary changes between adjacent intervals that must never be merged.
	// Calling getHardOverride on i and i+1 guarantees that every hard override window (VPP prep, VPP active,
	// step-0 EV standby, negative/free import rate) starts and ends on a block boundary.
	//
	// Why hardcoding blockActionLoad and soc=100 is safe here:
	//   - blockActionLoad: In getHardOverride, choice.actionType is only inspected at stepIdx == 0
	//     to allow blockActionCharge and blockActionExport to bypass EV standby. Probing with blockActionLoad
	//     ensures that when EV charging is active right now, getHardOverride(0) returns EVChargingStandby while
	//     getHardOverride(1) does not, splitting step 0 into its own 1-interval block (startIdx: 0, endIdx: 0,
	//     since startIdx and endIdx are both inclusive). That way, if the solver picks Load/Standby on block 0,
	//     only interval 0 is locked into EV standby while interval 1+ can resume normal Load; and if the solver
	//     picks Charge/Export on block 0, expandBlockAction passes the real choice so interval 0 can still charge/export.
	//   - soc=100: In getHardOverride, startState.soc only affects whether a VPP prep interval
	//     returns ChargeAny vs Standby (both with ActionReasonVPPPrep) and whether a VPP active interval returns
	//     Export/Load vs Standby (both with ActionReasonVPPActive and overrideReserveSOC = targetSOC). Using a
	//     fixed probe SOC detects every VPP window entry/exit boundary (via reason and overrideReserveSOC changes)
	//     without needing the simulated trajectory SOC upfront, while expandBlockAction advances simState step-by-step
	//     within the override block to handle mid-window SOC transitions.
	hardSplitReason := func(i int) string {
		if i+1 >= len(timeline) {
			return "endOfTimeline"
		}

		// Reserve floor step-up or step-down
		if math.Abs(timeline[i].minSOC-timeline[i+1].minSOC) > socTargetTolerancePct {
			return fmt.Sprintf("minSOC(%.0f->%.0f)", timeline[i].minSOC, timeline[i+1].minSOC)
		}

		probeChoice := blockActionChoice{actionType: blockActionLoad}
		probeState := planState{soc: 100.0}
		ovA, okA := getHardOverride(i, probeChoice, probeState, timeline, vppEvents, settings, currentStatus, types.SolarModeAny)
		ovB, okB := getHardOverride(i+1, probeChoice, probeState, timeline, vppEvents, settings, currentStatus, types.SolarModeAny)
		if okA != okB || ovA.batteryMode != ovB.batteryMode || ovA.reason != ovB.reason || ovA.overrideReserveSOC != ovB.overrideReserveSOC {
			tagA := "none"
			if okA {
				tagA = string(ovA.reason)
			}
			tagB := "none"
			if okB {
				tagB = string(ovB.reason)
			}
			return fmt.Sprintf("override(%s->%s)", tagA, tagB)
		}
		return ""
	}
	hasHardSplit := func(i int) bool {
		return hardSplitReason(i) != ""
	}

	// 2. Initial grouping by identical prices (within $0.001/kWh) and hard splits.
	// On flat or standard 2/3-tier TOU tariffs, this naturally yields 4-8 exact blocks with zero price loss.
	type initialBlock struct {
		startIdx int
		endIdx   int
	}
	var initBlocks []initialBlock
	start := 0
	for i := 0; i < len(timeline); i++ {
		split := hasHardSplit(i)
		if !split && i+1 < len(timeline) {
			samePrice := isSamePrice(timeline[i].importRate, timeline[i].exportRate, timeline[i+1].importRate, timeline[i+1].exportRate)
			if !samePrice {
				split = true
			}
		}
		if split || i == len(timeline)-1 {
			initBlocks = append(initBlocks, initialBlock{startIdx: start, endIdx: i})
			start = i + 1
		}
	}

	// 3. Adaptive merging for hourly dynamic tariffs (e.g. ComEd / Ameren).
	computeBlockStats := func(ib initialBlock) timelineBlock {
		var durHrs, impWeighted, netSolar float64
		minImp := math.MaxFloat64
		maxImp := -math.MaxFloat64
		minExp := math.MaxFloat64
		maxExp := -math.MaxFloat64
		for k := ib.startIdx; k <= ib.endIdx; k++ {
			d := timeline[k].durationHours
			durHrs += d
			impWeighted += timeline[k].importRate * d
			netSolar += timeline[k].solarKWH - timeline[k].loadKWH
			if timeline[k].importRate < minImp {
				minImp = timeline[k].importRate
			}
			if timeline[k].importRate > maxImp {
				maxImp = timeline[k].importRate
			}
			if timeline[k].exportRate < minExp {
				minExp = timeline[k].exportRate
			}
			if timeline[k].exportRate > maxExp {
				maxExp = timeline[k].exportRate
			}
		}
		avgImp := minImp
		if durHrs > 0 {
			avgImp = impWeighted / durHrs
		}
		return timelineBlock{
			startIdx:      ib.startIdx,
			endIdx:        ib.endIdx,
			durationHours: durHrs,
			minSOC:        timeline[ib.startIdx].minSOC,
			avgImportRate: avgImp,
			minImportRate: minImp,
			maxImportRate: maxImp,
			minExportRate: minExp,
			maxExportRate: maxExp,
			netSolarKWH:   netSolar,
		}
	}

	blocks := make([]timelineBlock, len(initBlocks))
	for idx, ib := range initBlocks {
		blocks[idx] = computeBlockStats(ib)
	}

	// When selectPlanningHorizonEnd extends the timeline past 26 hours to capture a Day 2 peak window
	// and its post-peak off-peak valley, allow targetTimelineBlocksExtendedHorizon (12) blocks so the
	// extra Day 2 peak + valley regimes at the tail do not force the merger to collapse Day 1 blocks.
	totalTimelineHours := timeline[len(timeline)-1].endTime.Sub(timeline[0].startTime).Hours()
	effectiveTargetBlocks := targetTimelineBlocks
	if totalTimelineHours > 26.0 {
		effectiveTargetBlocks = targetTimelineBlocksExtendedHorizon
	}

	// Repeatedly merge adjacent compatible blocks with the smallest price gap until effectiveTargetBlocks is reached,
	// stopping early if the smallest gap exceeds maxPriceBandMergeDollars.
	initBlocksCount := len(initBlocks)
	numPriceMerged := 0
	maxPriceDiffMerged := 0.0
	for len(blocks) > effectiveTargetBlocks {
		bestPair := -1
		minDiff := math.MaxFloat64
		for k := 0; k < len(blocks)-1; k++ {
			if hasHardSplit(blocks[k].endIdx) {
				continue
			}
			diff := max(math.Abs(blocks[k].avgImportRate-blocks[k+1].avgImportRate), math.Abs(blocks[k].maxExportRate-blocks[k+1].maxExportRate))
			if diff < minDiff {
				minDiff = diff
				bestPair = k
			}
		}
		if bestPair == -1 || minDiff > maxPriceBandMergeDollars {
			break // No further safe merges possible
		}
		numPriceMerged++
		if minDiff > maxPriceDiffMerged {
			maxPriceDiffMerged = minDiff
		}
		merged := initialBlock{startIdx: blocks[bestPair].startIdx, endIdx: blocks[bestPair+1].endIdx}
		blocks[bestPair] = computeBlockStats(merged)
		blocks = append(blocks[:bestPair+1], blocks[bestPair+2:]...)
	}

	// Second pass for long non-peak blocks: when the block count is below targetTimelineBlocks, split long
	// off-peak/shoulder blocks around the daytime solar window (into at most pre-solar, midday solar, and
	// post-solar segments) so multi-hour TOU off-peak blocks (e.g. a 12h daytime Off-Peak or 18h overnight-to-afternoon
	// Off-Peak) and flat-rate horizons can switch modes when solar starts or ends. Peak blocks (isHighestPrice)
	// are not split here because expandBlockAction already manages intra-block transitions (banking solar ->
	// DirectSolarExport -> end-of-peak battery Export) while keeping a unified TOU schedule period across sunset.
	globalMinImport, globalMaxImport := calculateTimelinePriceRange(timeline)
	minSpread := minDeficitPriceSpread(settings)
	numSplitBlocks := 0
	splitOnFirstSun := 0
	splitOnFirstNetSun := 0
	splitOnLastSun := 0
	splitOnLastNetSun := 0
	splitInHalf := 0
	if len(blocks) < targetTimelineBlocks && totalTimelineHours >= 12.0 {
		flatHorizon := len(blocks) <= 2
		var splitBlocks []timelineBlock
		for idx, b := range blocks {
			remainingBlocks := len(blocks) - idx - 1
			isPeakBlock := globalMaxImport-globalMinImport >= priceMaterialityThresholdDollars && b.maxImportRate >= globalMaxImport-priceMaterialityThresholdDollars
			if len(splitBlocks)+1+remainingBlocks >= targetTimelineBlocks || b.durationHours < 5.0 || isPeakBlock {
				splitBlocks = append(splitBlocks, b)
				continue
			}
			firstSun, lastSun := -1, -1
			firstNetSun, lastNetSun := -1, -1
			for k := b.startIdx; k <= b.endIdx; k++ {
				if timeline[k].solarKWH >= minSignificantSolarKW*timeline[k].durationHours {
					if firstSun == -1 {
						firstSun = k
					}
					lastSun = k
				}
				if timeline[k].solarKWH > timeline[k].loadKWH+minSignificantSolarKW*timeline[k].durationHours {
					if firstNetSun == -1 {
						firstNetSun = k
					}
					lastNetSun = k
				}
			}
			if firstSun != -1 && (b.durationHours >= 8.0 || firstNetSun != -1) {
				// Start the solar block directly at firstSun (without backward padding) so the dark morning hour
				// before sunrise stays in the pre-solar block (e.g. holding in Standby or finishing an overnight
				// charge) rather than being forced into the daytime block's Load/DirectSolarExport mode while dark.
				// We do pad the end by 1 hour after lastSun because continuing Load or SolarModeExport for an extra
				// hour after sunset has identical physics when solarKWH is zero and avoids an unnecessary dusk split.
				solarStart := firstSun
				padEnd := lastSun
				var postDur float64
				for padEnd < b.endIdx && postDur+timeline[padEnd+1].durationHours <= 1.0+1e-6 {
					padEnd++
					postDur += timeline[padEnd].durationHours
				}
				// If a daytime TOU block starts/ends near sunrise/sunset so the solar window
				// reaches the block boundary, fall back to splitting where net solar (solarKWH > loadKWH)
				// starts or ends when the dark/net-load tail is at least 1 hour long.
				usedFirstNetSun := false
				if solarStart == b.startIdx && firstNetSun > b.startIdx {
					var netPreDur float64
					for k := b.startIdx; k < firstNetSun; k++ {
						netPreDur += timeline[k].durationHours
					}
					if netPreDur >= 1.0-1e-6 {
						solarStart = firstNetSun
						usedFirstNetSun = true
					}
				}
				usedLastNetSun := false
				if padEnd == b.endIdx && lastNetSun != -1 && lastNetSun < b.endIdx {
					var netPostDur float64
					for k := lastNetSun + 1; k <= b.endIdx; k++ {
						netPostDur += timeline[k].durationHours
					}
					if netPostDur >= 1.0-1e-6 {
						padEnd = lastNetSun
						usedLastNetSun = true
					}
				}
				if solarStart > b.startIdx || padEnd < b.endIdx {
					numSplitBlocks++
				}
				if solarStart > b.startIdx {
					if usedFirstNetSun {
						splitOnFirstNetSun++
					} else {
						splitOnFirstSun++
					}
					splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: b.startIdx, endIdx: solarStart - 1}))
				}
				splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: solarStart, endIdx: padEnd}))
				if padEnd < b.endIdx {
					if usedLastNetSun {
						splitOnLastNetSun++
					} else {
						splitOnLastSun++
					}
					splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: padEnd + 1, endIdx: b.endIdx}))
				}
			} else if flatHorizon && b.durationHours >= 12.0 {
				// No significant solar in this long flat-rate block: split off the initial ~2h window
				// (so any step-0 reserve recovery does not span the whole day) and split the remainder in half.
				firstCut := b.startIdx
				dur := timeline[firstCut].durationHours
				for firstCut+1 < b.endIdx-2 && dur+timeline[firstCut+1].durationHours <= 2.0+1e-6 {
					firstCut++
					dur += timeline[firstCut].durationHours
				}
				midCut := (firstCut + 1 + b.endIdx) / 2
				splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: b.startIdx, endIdx: firstCut}))
				if midCut > firstCut && midCut < b.endIdx {
					numSplitBlocks++
					splitInHalf++
					splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: firstCut + 1, endIdx: midCut}))
					splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: midCut + 1, endIdx: b.endIdx}))
				} else if firstCut < b.endIdx {
					numSplitBlocks++
					splitInHalf++
					splitBlocks = append(splitBlocks, computeBlockStats(initialBlock{startIdx: firstCut + 1, endIdx: b.endIdx}))
				}
			} else {
				splitBlocks = append(splitBlocks, b)
			}
		}
		blocks = splitBlocks
	}

	finalBlocksCount := len(blocks)
	logAttrs := []any{
		slog.Int("initBlocksCount", initBlocksCount),
		slog.Float64("totalTimelineHours", totalTimelineHours),
		slog.Int("numPriceMerged", numPriceMerged),
		slog.Float64("maxPriceDiffMerged", maxPriceDiffMerged),
		slog.Int("numSplitBlocks", numSplitBlocks),
		slog.Int("splitOnFirstSun", splitOnFirstSun),
		slog.Int("splitOnFirstNetSun", splitOnFirstNetSun),
		slog.Int("splitOnLastSun", splitOnLastSun),
		slog.Int("splitOnLastNetSun", splitOnLastNetSun),
		slog.Int("splitInHalf", splitInHalf),
		slog.Int("finalBlocksCount", finalBlocksCount),
		slog.Int("targetBlocks", targetTimelineBlocks),
		slog.Int("effectiveTargetBlocks", effectiveTargetBlocks),
	}
	if len(blocks) > targetTimelineBlocks {
		splitCauses := make([]string, 0, len(blocks)-1)
		for k := 0; k < len(blocks)-1; k++ {
			endIdx := blocks[k].endIdx
			cause := hardSplitReason(endIdx)
			if cause == "" {
				diff := max(math.Abs(blocks[k].avgImportRate-blocks[k+1].avgImportRate), math.Abs(blocks[k].maxExportRate-blocks[k+1].maxExportRate))
				cause = fmt.Sprintf("priceGap($%.3f)", diff)
			}
			splitCauses = append(splitCauses, fmt.Sprintf("b%d->b%d(idx=%d,%s):%s",
				k, k+1, endIdx, timeline[endIdx].endTime.Format("15:04"), cause))
		}
		log.Ctx(ctx).DebugContext(ctx, "timeline blocks exceeded target count",
			append(logAttrs,
				slog.Int("blockCount", len(blocks)),
				slog.Any("splitCauses", splitCauses),
			)...,
		)
	} else {
		log.Ctx(ctx).DebugContext(ctx, "timeline blocks built", logAttrs...)
	}

	// 4. Global price extrema tagging for block-level pre-filtering (isBlockActionAllowed).
	for idx := range blocks {
		if blocks[idx].minImportRate <= globalMinImport+priceEpsilonForEquality {
			blocks[idx].isLowestPrice = true
		}
		if globalMaxImport-globalMinImport >= minSpread && blocks[idx].maxImportRate >= globalMaxImport-priceMaterialityThresholdDollars {
			blocks[idx].isHighestPrice = true
		}
	}

	return blocks
}

// solarExportOrLoadCandForStep returns DirectSolarExport (BatteryModeLoad + SolarModeExport) for
// intervals at or after solarExportStartIdx when hasSolarExport is true, and normal self-consumption
// (BatteryModeLoad + defaultSolarMode) otherwise. Keeping SolarModeExport through the
// remainder of the block after solarExportStartIdx avoids an unnecessary hardware TOU schedule split at sunset
// (when solarKWH is zero and stepPhysics is identical between SolarModeExport and SolarModeAny).
func solarExportOrLoadCandForStep(
	step int,
	solarExportStartIdx int,
	hasSolarExport bool,
	defaultSolarMode types.SolarMode,
) actionCandidate {
	if hasSolarExport && step >= solarExportStartIdx {
		return actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeExport,
		}
	}
	return actionCandidate{
		batteryMode: types.BatteryModeLoad,
		solarMode:   defaultSolarMode,
	}
}

// findSolarExportStartIdx finds the earliest interval index s in [block.startIdx .. block.endIdx]
// where a block can transition from normal self-consumption (BatteryModeLoad + defaultSolarMode, which
// allows surplus rooftop solar to charge the battery) into DirectSolarExport (BatteryModeLoad + SolarModeExport,
// where the battery covers home load and 100% of solar exports to the grid).
// Returns (s, true) if a viable start index is found, or (0, false) if DirectSolarExport cannot be sustained.
//
// Why this is done inside expandBlockAction rather than splitting blocks in the outer solver:
// Just as blockActionCharge splits a block into (Standby -> ChargeAny) and blockActionExport splits a block
// into (Load -> Export), finding the transition index s here allows a single peak block (e.g. 1 PM - 8 PM)
// that starts with insufficient battery SOC to first bank rooftop solar in BatteryModeLoad for a few intervals
// and then automatically switch to DirectSolarExport once enough energy is stored to survive the remainder of
// the block plus requiredEndSOC—without needing to recursively split and re-merge timelineBlock structs in
// critiqueBlockPlan or duplicate logic between blockActionDirectSolarExport and blockActionExport.
func findSolarExportStartIdx(
	block timelineBlock,
	startState planState,
	timeline []planInterval,
	settings types.Settings,
	defaultSolarMode types.SolarMode,
	requiredEndSOC float64,
	effRT float64,
) (int, bool) {
	if !settings.ManageTOUSchedules || !settings.GridExportSolar || block.maxExportRate <= 0 {
		return 0, false
	}

	targetEndSOC := max(requiredEndSOC, block.minSOC)

	// Simulate pure self-consumption across the block to establish the
	// baseline grid import and maximum reachable ending SOC if all surplus solar is banked.
	pureSt := startState
	var pureGridImportKWH float64
	for k := block.startIdx; k <= block.endIdx; k++ {
		cand := solarExportOrLoadCandForStep(k, 0, false, defaultSolarMode)
		var m intervalMetrics
		pureSt, m = stepPhysics(pureSt, cand, timeline[k], settings, effRT)
		pureGridImportKWH += m.gridImportKWH
	}
	if pureSt.soc < targetEndSOC-socTargetTolerancePct {
		return 0, false
	}

	for s := block.startIdx; s <= block.endIdx; s++ {
		simSt := startState
		var simGridImportKWH float64
		marginOK := true
		hasSunnyExport := false
		for k := block.startIdx; k <= block.endIdx; k++ {
			cand := solarExportOrLoadCandForStep(k, s, true, defaultSolarMode)
			if cand.solarMode == types.SolarModeExport && timeline[k].solarKWH >= minSignificantSolarKW*timeline[k].durationHours {
				if simSt.soc <= timeline[k].minSOC+solarExportReserveMarginPct {
					marginOK = false
					break
				}
				hasSunnyExport = true
			}
			var m intervalMetrics
			simSt, m = stepPhysics(simSt, cand, timeline[k], settings, effRT)
			simGridImportKWH += m.gridImportKWH
		}
		if marginOK && hasSunnyExport &&
			simSt.soc >= targetEndSOC-socTargetTolerancePct &&
			simGridImportKWH <= pureGridImportKWH+0.01 {
			return s, true
		}
	}

	return 0, false
}

// findBestRateWindows scans all contiguous K-interval windows in [startIdx .. endIdx] and returns
// the start indices of all windows whose average import rate (when importRate is true) or average
// export rate (when importRate is false) is within priceEpsilonForEquality of the block's best K-window rate.
func findBestRateWindows(timeline []planInterval, startIdx, endIdx, K int, importRate bool) []int {
	bestAvg := -math.MaxFloat64
	if importRate {
		bestAvg = math.MaxFloat64
	}
	for s := startIdx; s <= endIdx-K+1; s++ {
		var sum float64
		for t := s; t < s+K; t++ {
			if importRate {
				sum += timeline[t].importRate
			} else {
				sum += timeline[t].exportRate
			}
		}
		avg := sum / float64(K)
		if (importRate && avg < bestAvg) || (!importRate && avg > bestAvg) {
			bestAvg = avg
		}
	}
	var tiedPositions []int
	for s := startIdx; s <= endIdx-K+1; s++ {
		var sum float64
		for t := s; t < s+K; t++ {
			if importRate {
				sum += timeline[t].importRate
			} else {
				sum += timeline[t].exportRate
			}
		}
		avg := sum / float64(K)
		if (importRate && avg <= bestAvg+priceEpsilonForEquality) || (!importRate && avg >= bestAvg-priceEpsilonForEquality) {
			tiedPositions = append(tiedPositions, s)
		}
	}
	return tiedPositions
}

// expandBlockAction expands a single canonical blockActionChoice for a timeline block into concrete
// interval actionCandidates, enforcing strictly contiguous charging or exporting windows.
func expandBlockAction(
	block timelineBlock,
	choice blockActionChoice,
	startState planState,
	timeline []planInterval,
	vppEvents []vppAnchor,
	settings types.Settings,
	currentStatus types.SystemStatus,
	lastAction *types.Action,
) []actionCandidate {
	L := block.endIdx - block.startIdx + 1
	actions := make([]actionCandidate, L)
	defaultSolarMode := types.SolarModeAny
	if !settings.GridExportSolar || block.maxExportRate < 0 {
		defaultSolarMode = types.SolarModeNoExport
	}

	// Because buildTimelineBlocks (hasHardSplit) splits on every boundary where a hard override window
	// starts or ends, if block.startIdx is governed by a hard override then the entire block belongs to
	// that override window. We still evaluate getHardOverride at each interval i while advancing simState
	// so that SOC-dependent transitions within a multi-interval VPP window (e.g. VPPPrep switching from
	// ChargeAny to Standby once simState.soc reaches 99%, or VPPActive switching from Export/Load to Standby
	// once simState.soc reaches targetSOC) occur on the exact interval where the threshold is crossed.
	if _, ok := getHardOverride(block.startIdx, choice, startState, timeline, vppEvents, settings, currentStatus, defaultSolarMode); ok {
		effRT := resolveRoundTripEfficiency(settings.GetOptimizationParams().RoundTripEfficiency, currentStatus)
		simState := startState
		for i := 0; i < L; i++ {
			ov, _ := getHardOverride(block.startIdx+i, choice, simState, timeline, vppEvents, settings, currentStatus, defaultSolarMode)
			actions[i] = ov
			simState, _ = stepPhysics(simState, ov, timeline[block.startIdx+i], settings, effRT)
		}
		return actions
	}

	// Note: For normal (non-override) block actions below, reason and description are intentionally left
	// unset here; resolvePlanActionReason in finalizeDecisionAndPlan populates the contextual reason and
	// homeowner-facing description once the full winning trajectory is finalized.
	switch choice.actionType {
	case blockActionLoad:
		for i := 0; i < L; i++ {
			actions[i] = actionCandidate{
				batteryMode: types.BatteryModeLoad,
				solarMode:   defaultSolarMode,
			}
		}

	case blockActionStandby:
		for i := 0; i < L; i++ {
			actions[i] = actionCandidate{
				batteryMode: types.BatteryModeStandby,
				solarMode:   defaultSolarMode,
			}
		}

	case blockActionDirectSolarExport:
		effRT := resolveRoundTripEfficiency(settings.GetOptimizationParams().RoundTripEfficiency, currentStatus)
		floorSOC := max(choice.targetSOC, block.minSOC)
		solarExportStartIdx, hasSolarExport := findSolarExportStartIdx(block, startState, timeline, settings, defaultSolarMode, floorSOC, effRT)
		for i := 0; i < L; i++ {
			actions[i] = solarExportOrLoadCandForStep(block.startIdx+i, solarExportStartIdx, hasSolarExport, defaultSolarMode)
		}

	case blockActionCharge:
		// 1. Calculate the maximum physically reachable SOC across the block (testState.soc) if all L
		// intervals charge at full power. Note that stepPhysics caps SOC at 100.0%, so on a typical multi-hour
		// off-peak block (e.g. an 8-hour overnight block of L=24 intervals), testState.soc hits 100% after just
		// a few intervals and effectiveTarget = min(choice.targetSOC, testState.soc) equals choice.targetSOC
		// (so step 2 below finds a short window K << L). Clamping to testState.soc only comes into play when
		// the block is so short or inverter-power-limited (e.g. a 1-hour block where charging all L=3 intervals
		// only raises SOC from 20% to 55%) that even charging for the entire block cannot reach choice.targetSOC.
		effRT := resolveRoundTripEfficiency(settings.GetOptimizationParams().RoundTripEfficiency, currentStatus)
		testState := startState
		for k := block.startIdx; k <= block.endIdx; k++ {
			cand := actionCandidate{batteryMode: types.BatteryModeChargeAny, solarMode: defaultSolarMode, targetSOC: 100}
			testState, _ = stepPhysics(testState, cand, timeline[k], settings, effRT)
		}
		effectiveTarget := min(choice.targetSOC, testState.soc)

		// If the battery enters the block already at or above effectiveTarget (e.g. after prior solar gain
		// or when Pass 2 tests a lower targetSOC candidate) or if charging is disabled so no SOC can be
		// added, no ChargeAny intervals are needed; hold the block in Standby.
		if effectiveTarget <= startState.soc+socTargetTolerancePct {
			for i := 0; i < L; i++ {
				actions[i] = actionCandidate{batteryMode: types.BatteryModeStandby, solarMode: defaultSolarMode}
			}
			break
		}

		// 2. Find the smallest contiguous window length K (1..L) and start index bestStart that reaches effectiveTarget.
		// Intervals outside [bestStart .. bestStart+K-1] rest in Standby (where behind-the-meter solar still charges
		// the battery during stepPhysics).
		chosenK := L
		chosenStart := block.startIdx
		chosenTarget := max(effectiveTarget, min(choice.targetSOC, block.minSOC))

		isContinuingCharge := block.startIdx == 0 && lastAction != nil && lastAction.BatteryMode == types.BatteryModeChargeAny
		inReserveDeficit := startState.soc < block.minSOC-reserveDeficitChargeTolerancePct
		hasVPPAhead := false
		for _, vpp := range vppEvents {
			if vpp.eventStart.After(timeline[block.startIdx].startTime) {
				hasVPPAhead = true
				break
			}
		}

		for K := 1; K <= L; K++ {
			// Note: buildTimelineBlocks never merges intervals that differ by more than maxPriceBandMergeDollars
			// so any materially more expensive hour is already in a separate block. Within a merged
			// dynamic-tariff block (spread <= $0.06/kWh), sliding this K-interval window locks onto the cheapest
			// sub-window (e.g. the cheapest hour plus the cheaper of its two neighbors if K > 3) while keeping
			// charging contiguous.
			tiedPositions := findBestRateWindows(timeline, block.startIdx, block.endIdx, K, true)
			// Tie-breaking among equal-rate windows:
			// - If the battery enters the block in an active reserve deficit (startState.soc < block.minSOC),
			//   charging must begin immediately at block.startIdx (or at step 1 only when block.startIdx == 0
			//   and step 0 fits within the 20-minute initial reserve deferral buffer).
			// - If an active grid charge session is already underway at step 0, or when a VPP event is ahead,
			//   break ties to the earliest tied position (tiedPositions[0]).
			// - Otherwise, prefer the latest tied window (just-in-time charging minimizes high-SOC holding time)
			//   that finishes 1-2 intervals (20-40m) before block.endIdx so charging completes with a safety
			//   buffer before the block boundary (where rates increase or a higher reserve tier begins). If the
			//   final intervals are strictly cheaper, tiedPositions only contains the end of the block and bestStart
			//   remains tiedPositions[len(tiedPositions)-1].
			bestStart := tiedPositions[len(tiedPositions)-1]
			if inReserveDeficit {
				bestStart = block.startIdx
				// check to see if it's beneficial to wait 1 period, assuming we haven't
				// already waited
				// tiedPositions is ordered by timeline so is the 1st index is the cheapest
				// and cheaper than 0th index than it should be the first element in the slice
				if !isContinuingCharge && block.startIdx == 0 && tiedPositions[0] == 1 {
					waiting := timeline[0].endTime.Sub(timeline[0].startTime) + computeInitialDeficitWait(startState, timeline, lastAction)
					if waiting <= chargeCompletionBufferDuration+time.Minute {
						bestStart = 1
					}
				}
			} else if isContinuingCharge || choice.targetSOC <= block.minSOC || hasVPPAhead {
				bestStart = tiedPositions[0]
			} else {
				bufferSteps := min(2, L-K)
				for i := len(tiedPositions) - 1; i >= 0; i-- {
					if tiedPositions[i]+K-1 <= block.endIdx-bufferSteps {
						bestStart = tiedPositions[i]
						break
					}
				}
			}

			// Simulate K intervals at bestStart (with Standby before and after) to verify whether effectiveTarget
			// is reached across the block (accounting for both pre-window and post-window solar gain).
			st := startState
			peakReached := st.soc
			windowEndSOC := effectiveTarget
			for k := block.startIdx; k <= block.endIdx; k++ {
				cand := actionCandidate{batteryMode: types.BatteryModeStandby, solarMode: defaultSolarMode}
				if k >= bestStart && k < bestStart+K {
					cand = actionCandidate{batteryMode: types.BatteryModeChargeAny, solarMode: defaultSolarMode, targetSOC: max(effectiveTarget, min(choice.targetSOC, block.minSOC))}
				}
				st, _ = stepPhysics(st, cand, timeline[k], settings, effRT)
				if k == bestStart+K-1 {
					windowEndSOC = min(effectiveTarget, st.soc)
				}
				if st.soc > peakReached {
					peakReached = st.soc
				}
			}
			// if we already hit 100 then there's no need to keep finding a longer window
			if windowEndSOC >= effectiveTarget-socTargetTolerancePct || peakReached >= 100.0-socTargetTolerancePct {
				chosenK = K
				chosenStart = bestStart
				chosenTarget = max(windowEndSOC, min(choice.targetSOC, block.minSOC))
				break
			}
		}

		// Intervals outside the contiguous K-step charge window [chosenStart .. chosenStart+chosenK-1]
		// hold in BatteryModeStandby so the battery does not immediately discharge newly charged energy.
		for i := 0; i < L; i++ {
			step := block.startIdx + i
			if step >= chosenStart && step < chosenStart+chosenK {
				actions[i] = actionCandidate{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   defaultSolarMode,
					targetSOC:   chosenTarget,
				}
			} else {
				actions[i] = actionCandidate{
					batteryMode: types.BatteryModeStandby,
					solarMode:   defaultSolarMode,
				}
			}
		}

	case blockActionExport:
		// In an export block, non-export intervals run prefixCandForStep: either normal self-consumption
		// (BatteryModeLoad) or DirectSolarExport (BatteryModeLoad + SolarModeExport) during sunny intervals
		// from solarExportStartIdx onward when useSolarExportPrefix is true.
		// Using findSolarExportStartIdx ensures that if the battery enters a sunny peak block too low to survive
		// the block while exporting all solar, it first charges from solar in BatteryModeLoad and switches to
		// DirectSolarExport once sufficient energy is banked, before any contiguous BatteryModeExport tail.
		effRT := resolveRoundTripEfficiency(settings.GetOptimizationParams().RoundTripEfficiency, currentStatus)
		floorSOC := max(choice.targetSOC, block.minSOC)
		solarExportStartIdx := 0
		hasSolarExport := false
		if choice.useSolarExportPrefix {
			solarExportStartIdx, hasSolarExport = findSolarExportStartIdx(block, startState, timeline, settings, defaultSolarMode, floorSOC, effRT)
		}
		prefixCandForStep := func(step int) actionCandidate {
			return solarExportOrLoadCandForStep(step, solarExportStartIdx, hasSolarExport, defaultSolarMode)
		}

		// Simulate prefix mode across the block to find endLoadSOC (the ending SOC if no battery export is performed).
		st := startState
		for k := block.startIdx; k <= block.endIdx; k++ {
			st, _ = stepPhysics(st, prefixCandForStep(k), timeline[k], settings, effRT)
		}
		endLoadSOC := st.soc

		if endLoadSOC <= floorSOC+socTargetTolerancePct {
			// Home load alone drains the battery down to or below floorSOC; run prefixCandForStep across the entire block.
			for i := 0; i < L; i++ {
				actions[i] = prefixCandForStep(block.startIdx + i)
			}
			break
		}

		// Find the smallest contiguous window length K (1..L) of BatteryModeExport that reaches floorSOC.
		chosenK := L
		chosenStart := block.startIdx
		isContinuingExport := block.startIdx == 0 && lastAction != nil && lastAction.BatteryMode == types.BatteryModeExport
		isSuperSpike := block.maxExportRate > block.avgImportRate+priceMaterialityThresholdDollars

		for K := 1; K <= L; K++ {
			tiedPositions := findBestRateWindows(timeline, block.startIdx, block.endIdx, K, false)
			// Tie-breaking among windows that share the block's highest average export rate:
			// - Default to the latest tied position (tiedPositions[len(tiedPositions)-1]) so on a normal TOU peak
			//   block (where exportRate <= importRate), the battery covers peak home load first and dumps remaining
			//   surplus at the tail of the highest-export-rate window.
			// - When continuing an already-active battery export session at step 0 or during a wholesale export
			//   super-spike (maxExportRate > avgImportRate + $0.03/kWh), break ties to tiedPositions[0] (the earliest
			//   window among those tied for maxAvgExp) so the active export continues or the spike is captured right away.
			bestS := tiedPositions[len(tiedPositions)-1]
			if isContinuingExport || isSuperSpike {
				bestS = tiedPositions[0]
			}

			simSt := startState
			for k := block.startIdx; k <= block.endIdx; k++ {
				c := prefixCandForStep(k)
				if k >= bestS && k < bestS+K {
					c = actionCandidate{batteryMode: types.BatteryModeExport, solarMode: defaultSolarMode, targetSOC: floorSOC}
				}
				simSt, _ = stepPhysics(simSt, c, timeline[k], settings, effRT)
			}
			if simSt.soc <= floorSOC+socTargetTolerancePct {
				chosenK = K
				chosenStart = bestS
				break
			}
		}

		for i := 0; i < L; i++ {
			step := block.startIdx + i
			if step >= chosenStart && step < chosenStart+chosenK {
				actions[i] = actionCandidate{
					batteryMode: types.BatteryModeExport,
					solarMode:   defaultSolarMode,
					targetSOC:   floorSOC,
				}
			} else {
				actions[i] = prefixCandForStep(step)
			}
		}
	}

	return actions
}

// expandBlockPlan expands a full slice of timeline block choices into an interval-level action schedule
// across the entire planning timeline.
func expandBlockPlan(
	choices []blockActionChoice,
	blocks []timelineBlock,
	initial planState,
	timeline []planInterval,
	vppEvents []vppAnchor,
	settings types.Settings,
	currentStatus types.SystemStatus,
	lastAction *types.Action,
	roundTripEff float64,
) []actionCandidate {
	full := make([]actionCandidate, 0, len(timeline))
	st := initial
	for b := 0; b < len(blocks); b++ {
		expanded := expandBlockAction(blocks[b], choices[b], st, timeline, vppEvents, settings, currentStatus, lastAction)
		for _, act := range expanded {
			full = append(full, act)
			st, _ = stepPhysics(st, act, timeline[len(full)-1], settings, roundTripEff)
		}
	}
	return full
}

// isBlockActionAllowed verifies fast block-level pre-conditions for whether a candidate blockActionChoice
// is physically and economically viable in a given timelineBlock before running a full 72-step simulation.
func isBlockActionAllowed(
	blockIdx int,
	block timelineBlock,
	cand blockActionChoice,
	blocks []timelineBlock,
	startState planState,
	vppEvents []vppAnchor,
	knownPostHorizonRate float64,
	settings types.Settings,
	currentStatus types.SystemStatus,
	timeline []planInterval,
	lastAction *types.Action,
) bool {
	optParams := settings.GetOptimizationParams()
	effRT := resolveRoundTripEfficiency(optParams.RoundTripEfficiency, currentStatus)
	wear := getWearRates(settings, optParams)
	isFlatNEM := settings.UtilityRateOptions.IsFlatNetMetering()

	blockStart := timeline[block.startIdx].startTime

	hasVPPAhead := false
	for _, vpp := range vppEvents {
		if vpp.eventStart.After(blockStart) {
			hasVPPAhead = true
			break
		}
	}

	switch cand.actionType {
	case blockActionCharge:
		if !settings.GridChargeBatteries || startState.chargingDisabled {
			return false
		}
		isContinuingCharge := blockIdx == 0 && lastAction != nil && lastAction.BatteryMode == types.BatteryModeChargeAny
		almostFullLimit := almostFullSOC
		if hasVPPAhead || isContinuingCharge {
			almostFullLimit = vppMinFullCapacitySOC
		}
		if (blockIdx == 0 || block.minImportRate >= 0) &&
			(startState.soc >= almostFullLimit || cand.targetSOC <= startState.soc+socTargetTolerancePct) {
			return false
		}
		// Never grid-charge during an active VPP event window.
		for _, vpp := range vppEvents {
			if !blockStart.Before(vpp.eventStart) && blockStart.Before(vpp.eventEnd) {
				return false
			}
		}
		// A charge target below the block's active reserve floor is never valid.
		if cand.targetSOC < block.minSOC-socTargetTolerancePct {
			return false
		}
		// Reserve deficit recovery (targetSOC == minSOC) is allowed in any block whenever SOC is below reserve.
		if cand.targetSOC <= block.minSOC+socTargetTolerancePct {
			return startState.soc < block.minSOC-reserveDeficitChargeTolerancePct
		}
		// Discretionary arbitrage charging is forbidden in the horizon's highest-price block.
		if block.isHighestPrice {
			return false
		}
		// Require at least one downstream block, post-horizon rate, or VPP event that clears the
		// minimum price-spread hurdle over this block's minimum import rate.
		hasArbitrageAhead := false
		cumNetKWH := block.netSolarKWH
		oneWayEff := math.Sqrt(effRT)
		for j := blockIdx + 1; j < len(blocks); j++ {
			// if we are going to hit reserve, then this isn't arbitrage and instead it's deficit avoidance which
			// is a smaller gate
			cumNetKWH += blocks[j].netSolarKWH
			usableKWH := (startState.soc - blocks[j].minSOC) / 100.0 * startState.capacityKWH
			willHitDeficit := -cumNetKWH >= usableKWH*oneWayEff
			requiredImportGate := wear.arbitrageGatePerKWH
			if willHitDeficit {
				requiredImportGate = wear.gridChargePerKWH
			}
			if blocks[j].maxImportRate-block.minImportRate >= requiredImportGate-priceEpsilonForEquality ||
				(settings.GridExportBatteries && blocks[j].maxExportRate-block.minImportRate >= wear.exportGatePerKWH-priceEpsilonForEquality) {
				hasArbitrageAhead = true
				break
			}
		}
		if !hasArbitrageAhead && knownPostHorizonRate-block.minImportRate >= wear.arbitrageGatePerKWH-priceEpsilonForEquality {
			hasArbitrageAhead = true
		}
		if hasArbitrageAhead {
			return true
		}
		for _, vpp := range vppEvents {
			if vpp.eventStart.After(blockStart) {
				return true
			}
		}
		return false

	case blockActionExport:
		if !settings.ManageTOUSchedules || !settings.GridExportBatteries || isFlatNEM {
			return false
		}
		if cand.useSolarExportPrefix {
			if !settings.GridExportSolar {
				return false
			}
			// A hybrid DirectSolarExport -> Export block is only allowed if DirectSolarExport
			// itself is allowed in this block.
			choice := blockActionChoice{actionType: blockActionDirectSolarExport}
			if !isBlockActionAllowed(blockIdx, block, choice, blocks, startState, vppEvents, knownPostHorizonRate, settings, currentStatus, timeline, lastAction) {
				return false
			}
		}

		if block.maxExportRate <= 0 {
			return false
		}
		minHorizonImportRate := block.minImportRate
		minHorizonExportRate := block.maxExportRate
		maxHorizonExportRate := block.maxExportRate
		for _, b := range blocks {
			if b.minImportRate < minHorizonImportRate {
				minHorizonImportRate = b.minImportRate
			}
			if b.maxExportRate < minHorizonExportRate {
				minHorizonExportRate = b.maxExportRate
			}
			if b.maxExportRate > maxHorizonExportRate {
				maxHorizonExportRate = b.maxExportRate
			}
		}
		// Never export battery energy during the lowest-price block, below the cheapest horizon import rate,
		// or at the lowest export tier of a multi-tier export tariff unless a wholesale export super-spike
		// (exportRate > avgImportRate + $0.03/kWh) occurs within the block.
		isSuperSpike := block.maxExportRate > block.avgImportRate+priceMaterialityThresholdDollars
		isLowestExportTier := maxHorizonExportRate-minHorizonExportRate >= priceMaterialityThresholdDollars &&
			block.maxExportRate <= minHorizonExportRate+priceEpsilonForEquality
		isLessThanMinImport := (minHorizonImportRate > 0 && block.maxExportRate < minHorizonImportRate)
		if !isSuperSpike && (block.isLowestPrice || isLowestExportTier || isLessThanMinImport) {
			return false
		}
		if startState.soc <= block.minSOC+socTargetTolerancePct {
			return false
		}
		return true

	case blockActionDirectSolarExport:
		if !settings.ManageTOUSchedules || !settings.GridExportSolar || isFlatNEM {
			return false
		}

		if block.maxExportRate <= 0 {
			return false
		}
		minHorizonImportRate := block.minImportRate
		minHorizonExportRate := block.maxExportRate
		maxHorizonExportRate := block.maxExportRate
		for _, b := range blocks {
			if b.minImportRate < minHorizonImportRate {
				minHorizonImportRate = b.minImportRate
			}
			if b.maxExportRate < minHorizonExportRate {
				minHorizonExportRate = b.maxExportRate
			}
			if b.maxExportRate > maxHorizonExportRate {
				maxHorizonExportRate = b.maxExportRate
			}
		}
		// Never export solar in the lowest export tier, the lowest import price, or
		// if the export rate is lower than the minimum import rate
		isLowestExportTier := maxHorizonExportRate-minHorizonExportRate >= priceMaterialityThresholdDollars &&
			block.maxExportRate <= minHorizonExportRate+priceEpsilonForEquality
		isLessThanMinImport := (minHorizonImportRate > 0 && block.maxExportRate < minHorizonImportRate)
		if block.isLowestPrice || isLowestExportTier || isLessThanMinImport {
			return false
		}
		// Because expandBlockAction (via findSolarExportStartIdx) can bank surplus solar in normal
		// BatteryModeLoad during the early intervals of a block before switching to SolarModeExport,
		// a block that enters at minSOC is still eligible if its sunny intervals provide enough
		// positiveNetSolarKWH to lift SOC above minSOC + solarExportReserveMarginPct.
		var hasSunnyInterval bool
		var positiveNetSolarKWH float64
		for k := block.startIdx; k <= block.endIdx; k++ {
			if timeline[k].solarKWH >= minSignificantSolarKW*timeline[k].durationHours {
				hasSunnyInterval = true
			}
			if net := timeline[k].solarKWH - timeline[k].loadKWH; net > 0 {
				positiveNetSolarKWH += net
			}
		}
		if !hasSunnyInterval {
			return false
		}
		maxReachableSOC := startState.soc + (positiveNetSolarKWH*math.Sqrt(effRT)/startState.capacityKWH)*100.0
		if maxReachableSOC <= block.minSOC+solarExportReserveMarginPct {
			return false
		}
		return true

	case blockActionStandby:
		if blockIdx == 0 && startState.soc >= almostFullSOC && !block.isHighestPrice && !hasVPPAhead {
			// 1. If solar in Block 0 already covers load (netSolarKWH >= 0), BatteryModeLoad keeps the
			// nearly-full battery at ~100% SOC without idling in Standby.
			if block.netSolarKWH >= 0 {
				return false
			}
			// 2. Otherwise (netSolarKWH < 0), holding a >=98% battery in Standby during an off-peak/shoulder
			// block is only worthwhile if either:
			//   (a) A more expensive block is ahead and intervening solar will not simply refill the battery
			//       back to 100% SOC for free (cumNetKWH < 0), or
			//   (b) A sunny block is ahead with an export rate higher than the current import rate (e.g. when
			//       ManageTOUSchedules is false and staying near 100% SOC allows excess solar to overflow at a profit).
			hasUnrefilledPeakAhead := false
			hasProfitableSolarOverflowAhead := false
			var cumNetKWH float64
			for j := 1; j < len(blocks); j++ {
				cumNetKWH += blocks[j].netSolarKWH
				if blocks[j].maxImportRate > block.avgImportRate+priceMaterialityThresholdDollars && cumNetKWH < 0 {
					hasUnrefilledPeakAhead = true
				}
				if blocks[j].netSolarKWH > 0 && blocks[j].maxExportRate > block.avgImportRate+priceMaterialityThresholdDollars {
					hasProfitableSolarOverflowAhead = true
				}
			}
			if !hasUnrefilledPeakAhead && !hasProfitableSolarOverflowAhead {
				return false
			}
		}
		return true

	case blockActionLoad:
		return true
	}
	return true
}

// computeInitialDeficitWait checks whether the previous action (lastAction) was already in a
// reserve deficit without charging. Because lastAction represents the preceding planning period
// (~20 minutes, roughly matching chargeCompletionBufferDuration), if the battery was already below
// its reserve floor on lastAction and did not charge, it has exhausted its 1-period deferral buffer
// and must begin charging immediately at step 0.
func computeInitialDeficitWait(initial planState, timeline []planInterval, lastAction *types.Action) time.Duration {
	if len(timeline) == 0 || initial.soc >= timeline[0].minSOC-reserveDeficitChargeTolerancePct {
		return 0
	}
	if lastAction != nil && lastAction.BatteryMode != types.BatteryModeChargeAny &&
		lastAction.ReserveSOC > lastAction.SystemStatus.BatterySOC+reserveDeficitChargeTolerancePct {
		return chargeCompletionBufferDuration
	}
	return 0
}

// validateCandidateTrajectory checks full-trajectory economic and dominance guardrails,
// rejecting dominated or unprofitable schedules early during Pass 1 and Pass 2 search.
func validateCandidateTrajectory(
	actions []actionCandidate,
	timeline []planInterval,
	settings types.Settings,
	wear wearRates,
	roundTripEff float64,
	initialState planState,
	initialDeficitWait time.Duration,
) (bool, string) {
	if len(actions) != len(timeline) {
		return false, "length mismatch"
	}
	states := make([]planState, len(timeline)+1)
	metrics := make([]intervalMetrics, len(timeline))
	states[0] = initialState

	var maxChargeRate float64
	firstChargeIdx := -1
	for i := 0; i < len(timeline); i++ {
		states[i+1], metrics[i] = stepPhysics(states[i], actions[i], timeline[i], settings, roundTripEff)
		if actions[i].batteryMode == types.BatteryModeChargeAny {
			if firstChargeIdx == -1 {
				firstChargeIdx = i
				maxChargeRate = timeline[i].importRate
			} else if timeline[i].importRate > maxChargeRate {
				maxChargeRate = timeline[i].importRate
			}
		}
	}

	// 1. Export dominance and future-reserve checks:
	// - HoldThenCheaperExport: Holding battery energy in Standby at import rate I,
	//   only to export that battery energy later (before any recharge or full solar refill) at rate E < I - $0.005/kWh
	//   (or below the export gate when Standby imported from the grid).
	//   Discharging to cover load at i and exporting less at j is strictly superior.
	// - ExportThenCostlierHold: Exporting battery energy at rate E, only to sit in Standby above reserve
	//   later (before any recharge or full solar refill) at import rate I > E + $0.005/kWh.
	//   Saving that battery energy at i to cover load at j is strictly superior.
	// - ExportBelowFutureReserve: Exporting battery energy below a downstream interval's minSOC reserve floor
	//   before any profitable grid recharge or full solar refill can recover it.
	for i := 0; i < len(timeline); i++ {
		if actions[i].batteryMode == types.BatteryModeStandby &&
			actions[i].reason != types.ActionReasonVPPPrep &&
			actions[i].reason != types.ActionReasonVPPActive &&
			actions[i].reason != types.ActionReasonEVChargingStandby &&
			states[i].soc > timeline[i].minSOC+1.0 {
			standbyImportedFromGrid := metrics[i].gridImportKWH > 1e-6
			for j := i + 1; j < len(timeline); j++ {
				if actions[j].reason == types.ActionReasonVPPPrep || actions[j].reason == types.ActionReasonVPPActive {
					break
				}
				// Break if the trajectory hits a capacity boundary that decouples earlier and later decisions:
				// - Lower boundary: hitting the reserve floor (minSOC), or starting a new grid charge (ChargeAny).
				// - Upper boundary: rooftop solar refilling the battery to full (states[j+1].soc >= almostFullSOC
				//   with states[j+1].soc > states[j].soc, where states[j+1] is the ending SOC of interval j).
				//   Requiring states[j+1].soc > states[j].soc ensures we only break when solar actually refills
				//   the battery to >=98%, not when the battery was already sitting at >=98% in Standby.
				if states[j].soc <= timeline[j].minSOC+1.0 ||
					actions[j].batteryMode == types.BatteryModeChargeAny ||
					(states[j+1].soc > states[j].soc && states[j+1].soc >= almostFullSOC) {
					break
				}
				if actions[j].batteryMode == types.BatteryModeExport {
					minExportDiff := -dominatedPeriodPriceGapDollars
					if standbyImportedFromGrid {
						minExportDiff = wear.exportGatePerKWH - priceEpsilonForEquality
					}
					if timeline[j].exportRate-timeline[i].importRate < minExportDiff {
						return false, "HoldThenCheaperExport"
					}
				}
				if states[j+1].soc <= timeline[j].minSOC+1.0 {
					break
				}
			}
		}

		if actions[i].batteryMode == types.BatteryModeExport &&
			actions[i].reason != types.ActionReasonVPPActive {
			for j := i + 1; j < len(timeline); j++ {
				if actions[j].reason == types.ActionReasonVPPPrep || actions[j].reason == types.ActionReasonVPPActive {
					break
				}
				// Reject exports that leave a subsequent interval below its reserve floor before a profitable
				// grid-recharge window (exportRate - importRate >= wear.exportGatePerKWH) or full solar refill.
				if settings.GridChargeBatteries && !initialState.chargingDisabled &&
					timeline[i].exportRate-timeline[j].importRate >= wear.exportGatePerKWH-priceEpsilonForEquality {
					break
				}
				if timeline[j].minSOC > timeline[i].minSOC && states[j].soc < timeline[j].minSOC-socTargetTolerancePct {
					return false, "ExportBelowFutureReserve"
				}
				if actions[j].batteryMode == types.BatteryModeChargeAny ||
					(states[j+1].soc > states[j].soc && states[j+1].soc >= almostFullSOC) {
					break
				}
				if actions[j].batteryMode == types.BatteryModeStandby &&
					states[j].soc > timeline[j].minSOC+1.0 {
					minExportDiff := -dominatedPeriodPriceGapDollars
					if metrics[j].gridImportKWH > 1e-6 {
						minExportDiff = wear.exportGatePerKWH - priceEpsilonForEquality
					}
					if timeline[i].exportRate-timeline[j].importRate < minExportDiff {
						return false, "ExportThenCostlierHold"
					}
				}
			}
		}
	}

	// 2. Unprofitable Grid Charge -> Battery Export hurdle:
	// Whenever a trajectory charges from the grid and later dumps battery energy to the grid,
	// the export rate must beat the grid charge rate by at least wear.exportGatePerKWH
	// (max(BatteryExportDegradation, MinBatteryExportDifference)).
	if firstChargeIdx != -1 {
		for j := firstChargeIdx + 1; j < len(timeline); j++ {
			if actions[j].batteryMode == types.BatteryModeExport {
				if timeline[j].exportRate-maxChargeRate < wear.exportGatePerKWH-priceEpsilonForEquality {
					return false, "UnprofitableGridChargeToExport"
				}
			}
		}
	}

	// 3. Reserve deficit recovery enforcement:
	// Whenever grid charging is enabled and an interval i starts and ends below its active reserve floor
	// (timeline[i].minSOC - reserveDeficitChargeTolerancePct), the plan must actively charge (BatteryModeChargeAny)
	// in that interval unless:
	//   (a) Step i is controlled by a hard override (EVChargingStandby, VPPActive, VPPPrep), or
	//   (b) Step i is within the initial 1-period (chargeCompletionBufferDuration) deferral window at the
	//       start of the horizon before charging begins, and the deficit is recovered (or charging starts)
	//       within chargeCompletionBufferDuration.
	// This rejects plans that stop charging before reaching minSOC, while allowing plans that stay in
	// continuous BatteryModeChargeAny when minSOC is physically unreachable within the window.
	if len(timeline) > 0 && settings.GridChargeBatteries && !initialState.chargingDisabled {
		for i := 0; i < len(timeline); i++ {
			if states[i].soc >= timeline[i].minSOC-reserveDeficitChargeTolerancePct ||
				states[i+1].soc >= timeline[i].minSOC-reserveDeficitChargeTolerancePct {
				continue
			}
			if actions[i].batteryMode == types.BatteryModeChargeAny ||
				actions[i].reason == types.ActionReasonEVChargingStandby ||
				actions[i].reason == types.ActionReasonVPPActive ||
				actions[i].reason == types.ActionReasonVPPPrep {
				continue
			}
			if firstChargeIdx == -1 || i < firstChargeIdx {
				recoveredInBuffer := false
				for k := i + 1; k < len(timeline); k++ {
					if timeline[k].startTime.Sub(timeline[0].startTime)+initialDeficitWait > chargeCompletionBufferDuration+time.Minute {
						break
					}
					if actions[k].batteryMode == types.BatteryModeChargeAny ||
						(timeline[k].endTime.Sub(timeline[0].startTime)+initialDeficitWait <= chargeCompletionBufferDuration+time.Minute &&
							states[k+1].soc >= timeline[k].minSOC-reserveDeficitChargeTolerancePct) {
						recoveredInBuffer = true
						break
					}
				}
				if recoveredInBuffer {
					continue
				}
				if i == 0 {
					if firstChargeIdx == -1 {
						return false, "DeficitNeverRecharged"
					}
					return false, "ExceededMaxReserveDeficitWait"
				}
			}
			return false, "UnrecoveredReserveDeficit"
		}
	}

	return true, ""
}

// scoreCandidateTrajectory evaluates the exact physical economics, wear,
// VPP deficit score, and terminal battery valuation for a candidate action schedule.
func scoreCandidateTrajectory(
	actions []actionCandidate,
	timeline []planInterval,
	initial planState,
	vppEvents []vppAnchor,
	postHorizon postHorizonValuation,
	settings types.Settings,
	roundTripEff float64,
) (score float64, totalPhysicalCost float64, states []planState, metrics []intervalMetrics) {
	states = make([]planState, len(timeline)+1)
	metrics = make([]intervalMetrics, len(timeline))
	states[0] = initial
	optParams := settings.GetOptimizationParams()
	wear := getWearRates(settings, optParams)

	var wearSum, penaltySum float64
	for i := 0; i < len(timeline); i++ {
		states[i+1], metrics[i] = stepPhysics(states[i], actions[i], timeline[i], settings, roundTripEff)
		totalPhysicalCost += metrics[i].costDollars
		wearSum += stepWearCostDollars(actions[i], metrics[i], roundTripEff, wear)
		penaltySum += stepPenaltyCost(states[i+1], timeline[i])
	}

	// Mandatory VPP deadline deficit score
	var vppDeficitSOC int
	for _, vpp := range vppEvents {
		if vpp.mandatory && settings.GridChargeBatteries && !initial.chargingDisabled {
			for i := 0; i < len(timeline); i++ {
				if timeline[i].endTime.Equal(vpp.deadline) || (timeline[i].startTime.Before(vpp.deadline) && timeline[i].endTime.After(vpp.deadline)) {
					vppDeficitSOC += calculateVPPDeficitSOC(initial, states[i+1], vpp.deadline, math.Sqrt(roundTripEff), settings.GridChargeBatteries)
				}
			}
		}
	}

	termVal := calculateTerminalValuation(states[len(timeline)], postHorizon, settings, timeline, initial.capacityKWH, roundTripEff)

	score = totalPhysicalCost + wearSum + penaltySum + termVal
	if vppDeficitSOC > 0 {
		// ensure we heavily penalize missing charging for a mandatory vpp event
		score += float64(vppDeficitSOC) * 1000.0
	}

	return score, totalPhysicalCost, states, metrics
}

// estimatePairedBlockStartState approximates the starting state of block b (stateB) after applying
// pt[0] at an earlier block a (a < b). This allows isBlockActionAllowed(b, ...) to evaluate pt[1]
// accurately before running a full trajectory simulation (e.g., allowing Export at b when Charge at a
// raises SOC above minSOC, or allowing a second Charge at b when Charge at a cannot physically reach
// 98% SOC on its own).
func estimatePairedBlockStartState(
	a, b int,
	pt [2]blockActionChoice,
	blocks []timelineBlock,
	currentChoices []blockActionChoice,
	currentStates []planState,
	initialState planState,
	roundTripEff float64,
) planState {
	stateA := initialState
	if blocks[a].startIdx < len(currentStates) {
		stateA = currentStates[blocks[a].startIdx]
	}
	stateB := initialState
	if blocks[b].startIdx < len(currentStates) {
		stateB = currentStates[blocks[b].startIdx]
	}

	switch pt[0].actionType {
	case blockActionCharge:
		if pt[1].actionType == blockActionCharge {
			maxGainSOC := (resolveBatteryPowerKW(initialState.maxChargeKW, initialState.capacityKWH) * blocks[a].durationHours * math.Sqrt(roundTripEff) / initialState.capacityKWH) * 100.0
			simSOCB := min(pt[0].targetSOC, stateA.soc+maxGainSOC)
			for k := a + 1; k < b; k++ {
				if currentChoices[k].actionType == blockActionLoad && blocks[k].netSolarKWH < 0 {
					drainSOC := (-blocks[k].netSolarKWH / math.Sqrt(roundTripEff) / initialState.capacityKWH) * 100.0
					simSOCB = max(blocks[k].minSOC, simSOCB-drainSOC)
				}
			}
			stateB.soc = simSOCB
		} else {
			stateB.soc = max(stateB.soc, pt[0].targetSOC)
		}
	case blockActionStandby:
		stateB.soc = max(stateB.soc, stateA.soc)
	case blockActionLoad:
		simSOCB := stateA.soc
		for k := a; k < b; k++ {
			if (k == a || currentChoices[k].actionType == blockActionLoad) && blocks[k].netSolarKWH < 0 {
				drainSOC := (-blocks[k].netSolarKWH / math.Sqrt(roundTripEff) / initialState.capacityKWH) * 100.0
				simSOCB = max(blocks[k].minSOC, simSOCB-drainSOC)
			}
		}
		stateB.soc = min(stateB.soc, simSOCB)
	case blockActionExport:
		endASOC := stateA.soc
		if blocks[a].endIdx+1 < len(currentStates) {
			endASOC = currentStates[blocks[a].endIdx+1].soc
		}
		socDrop := max(0.0, endASOC-pt[0].targetSOC)
		stateB.soc = max(0.0, min(stateB.soc-socDrop, pt[0].targetSOC))
	}
	return stateB
}

// critiqueBlockPlan executes Pass 1 of the solver: starting from an all-Load baseline, it
// iteratively tests all valid single-block (O(M)) and paired-block (O(M^2)) modifications, adopting
// the best modification in each round until no modification improves total horizon score.
func critiqueBlockPlan(
	ctx context.Context,
	blocks []timelineBlock,
	timeline []planInterval,
	initialState planState,
	vppEvents []vppAnchor,
	postHorizon postHorizonValuation,
	settings types.Settings,
	currentStatus types.SystemStatus,
	lastAction *types.Action,
	initialDeficitWait time.Duration,
) ([]blockActionChoice, float64) {
	optParams := settings.GetOptimizationParams()
	roundTripEff := resolveRoundTripEfficiency(optParams.RoundTripEfficiency, currentStatus)
	wear := getWearRates(settings, optParams)

	// Start with all blocks defaulting to self-consumption (blockActionLoad).
	currentChoices := make([]blockActionChoice, len(blocks))
	for b := range currentChoices {
		currentChoices[b] = blockActionChoice{actionType: blockActionLoad}
	}

	// ensureReserveRecoveryBlocks returns a copy of tc where any Load block k (excluding skipIdxA and skipIdxB)
	// that starts and ends below blocks[k].minSOC - reserveDeficitChargeTolerancePct (when grid charging is enabled)
	// is upgraded to a reserve recovery charge (blockActionCharge, targetSOC = blocks[k].minSOC), allowing
	// multi-block reserve deficits or downstream minSOC step-ups to be satisfied automatically.
	ensureReserveRecoveryBlocks := func(tc []blockActionChoice, skipIdxA, skipIdxB int) ([]blockActionChoice, bool) {
		if !settings.GridChargeBatteries || initialState.chargingDisabled {
			return tc, false
		}
		tcRec := make([]blockActionChoice, len(tc))
		copy(tcRec, tc)
		changed := false
		blockStartState := initialState
		for k := 0; k < len(blocks); k++ {
			startSOC := blockStartState.soc
			expanded := expandBlockAction(blocks[k], tcRec[k], blockStartState, timeline, vppEvents, settings, currentStatus, lastAction)
			endState := blockStartState
			for idx, act := range expanded {
				endState, _ = stepPhysics(endState, act, timeline[blocks[k].startIdx+idx], settings, roundTripEff)
			}
			if k != skipIdxA && k != skipIdxB &&
				tcRec[k].actionType == blockActionLoad &&
				startSOC < blocks[k].minSOC-reserveDeficitChargeTolerancePct &&
				endState.soc < blocks[k].minSOC-reserveDeficitChargeTolerancePct {
				cand := blockActionChoice{actionType: blockActionCharge, targetSOC: blocks[k].minSOC}
				if isBlockActionAllowed(k, blocks[k], cand, blocks, blockStartState, vppEvents, postHorizon.knownPostHorizonRate, settings, currentStatus, timeline, lastAction) {
					tcRec[k] = cand
					changed = true
					expanded = expandBlockAction(blocks[k], tcRec[k], blockStartState, timeline, vppEvents, settings, currentStatus, lastAction)
					endState = blockStartState
					for idx, act := range expanded {
						endState, _ = stepPhysics(endState, act, timeline[blocks[k].startIdx+idx], settings, roundTripEff)
					}
				}
			}
			blockStartState = endState
		}
		return tcRec, changed
	}

	// Evaluate the initial all-Load baseline. An all-Load schedule has no battery exports, so the only
	// reason it can fail validateCandidateTrajectory is an unrecovered reserve deficit (e.g. starting below
	// minSOC or encountering a downstream minSOC step-up). If invalid, immediately try repairing the baseline
	// with ensureReserveRecoveryBlocks so Pass 1 starts from a valid reserve-compliant schedule.
	baseActs := expandBlockPlan(currentChoices, blocks, initialState, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
	baseValid, baseValidReason := validateCandidateTrajectory(baseActs, timeline, settings, wear, roundTripEff, initialState, initialDeficitWait)
	if !baseValid {
		if tcRec, ok := ensureReserveRecoveryBlocks(currentChoices, -1, -1); ok {
			recActs := expandBlockPlan(tcRec, blocks, initialState, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
			if recValid, recReason := validateCandidateTrajectory(recActs, timeline, settings, wear, roundTripEff, initialState, initialDeficitWait); recValid {
				currentChoices = tcRec
				baseActs = recActs
				baseValid = true
				baseValidReason = recReason
			} else {
				log.Ctx(ctx).DebugContext(ctx, "plan baseline still invalid after forced recovery",
					slog.Bool("valid", recValid),
					slog.String("baseValidReason", recReason),
				)
			}
		}
	}
	baseScore, _, currentStates, _ := scoreCandidateTrajectory(baseActs, timeline, initialState, vppEvents, postHorizon, settings, roundTripEff)
	currentScore := math.MaxFloat64
	if baseValid {
		currentScore = baseScore
	}
	log.Ctx(ctx).DebugContext(ctx, "plan baseline evaluated",
		slog.Bool("valid", baseValid),
		slog.String("baseValidReason", baseValidReason),
		slog.Float64("baselineScoreDollars", currentScore),
	)

	// holdInterveningOffPeakBlocks bridges 1-block and 2-block mutations across multi-block horizons.
	// When we test charging or holding in Standby at holdBlockIdx to prepare for a downstream peak block,
	// any intermediate Load blocks k (holdBlockIdx < k < limitIdx) would otherwise immediately discharge
	// that battery energy before the peak arrives. It returns up to two variants of tc:
	//   - Variant 1 (tcGate): Switches consecutive Load blocks k after holdBlockIdx to Standby only while
	//     their import rate does not clear `gate` over holdBlockIdx's rate (stopping at the first intermediate
	//     block that is expensive enough to profitably discharge, e.g. when a large battery can cover both a
	//     shoulder block and a peak block).
	//   - Variant 2 (tcAll): Switches ALL consecutive Load blocks k after holdBlockIdx (up to limitIdx) that
	//     sit ahead of a materially more expensive block p > k to Standby, preserving finite battery energy
	//     past intermediate shoulder blocks so it reaches the highest peak.
	holdInterveningOffPeakBlocks := func(tc []blockActionChoice, holdBlockIdx, limitIdx int) [][]blockActionChoice {
		tcGate := make([]blockActionChoice, len(tc))
		tcAll := make([]blockActionChoice, len(tc))
		copy(tcGate, tc)
		copy(tcAll, tc)
		changedGate := false
		changedAll := false
		allDiffersFromGate := false
		gateStopped := false
		isChargeBlock := tc[holdBlockIdx].actionType == blockActionCharge
		refCost := blocks[holdBlockIdx].avgImportRate
		gate := priceMaterialityThresholdDollars
		if isChargeBlock {
			refCost = blocks[holdBlockIdx].minImportRate
			gate = wear.arbitrageGatePerKWH
		}
		for k := holdBlockIdx + 1; k < limitIdx; k++ {
			if tc[k].actionType != blockActionLoad {
				break
			}
			hasHigherPeakAhead := false
			for p := k + 1; p < len(blocks); p++ {
				if blocks[p].maxImportRate > blocks[k].avgImportRate+priceMaterialityThresholdDollars ||
					(settings.GridExportBatteries && blocks[p].maxExportRate > blocks[k].avgImportRate+priceMaterialityThresholdDollars) {
					hasHigherPeakAhead = true
					break
				}
			}
			if !hasHigherPeakAhead {
				break
			}
			tcAll[k] = blockActionChoice{actionType: blockActionStandby}
			changedAll = true
			// continue to change gate in lockstep with all until // when we hit a
			// shoulder rate and then we stop changing gate but continue to change all
			// until the peak
			if !gateStopped && blocks[k].avgImportRate-refCost < gate {
				tcGate[k] = blockActionChoice{actionType: blockActionStandby}
				changedGate = true
			} else {
				gateStopped = true
				allDiffersFromGate = true
			}
		}
		var out [][]blockActionChoice
		if changedGate {
			out = append(out, tcGate)
		}
		if changedAll && (!changedGate || allDiffersFromGate) {
			out = append(out, tcAll)
		}
		return out
	}

	// predefine slices with large enough backing arrays
	candidateTypes := make([]blockActionChoice, 0, 24)
	baseTrial := make([]blockActionChoice, len(currentChoices))
	bestIterChoices := make([]blockActionChoice, len(currentChoices))
	trialVariants := make([][]blockActionChoice, 0, 12)
	pairedTrials := make([][2]blockActionChoice, 0, 96)
	// Iterative single-block & paired-block critique loop (up to 5 rounds).
	maxCritiqueIters := 5
	for iter := 0; iter < maxCritiqueIters; iter++ {
		bestIterScore := currentScore
		var bestIterStates []planState
		foundImprovement := false

		// 1. Single-block trials: test replacing one block's action at a time.

		for b := 0; b < len(blocks); b++ {
			minSOC := blocks[b].minSOC
			candidateTypes = candidateTypes[:0]
			candidateTypes = append(candidateTypes,
				blockActionChoice{actionType: blockActionLoad},
				blockActionChoice{actionType: blockActionStandby},
				blockActionChoice{actionType: blockActionDirectSolarExport, targetSOC: minSOC},
				blockActionChoice{actionType: blockActionCharge, targetSOC: 100},
				blockActionChoice{actionType: blockActionCharge, targetSOC: 75},
				blockActionChoice{actionType: blockActionCharge, targetSOC: 50},
				blockActionChoice{actionType: blockActionExport, targetSOC: minSOC, useSolarExportPrefix: false},
				blockActionChoice{actionType: blockActionExport, targetSOC: minSOC, useSolarExportPrefix: true},
			)
			if minSOC > 0 {
				candidateTypes = append(candidateTypes, blockActionChoice{actionType: blockActionCharge, targetSOC: minSOC})
			}
			// Always include a midpoint export floor (e.g. 55%-60% when minSOC is 10%-20%, or 75% when minSOC is 50%)
			// even when minSOC is constant across the horizon, so Pass 1 can discover partial battery exports during
			// mid-peak export spikes without draining the battery to minSOC before the peak concludes.
			midExportFloor := math.Round(((minSOC+100.0)/2.0)/5.0) * 5.0
			if midExportFloor > minSOC+5.0 && midExportFloor < 100.0-5.0 {
				candidateTypes = append(candidateTypes,
					blockActionChoice{actionType: blockActionDirectSolarExport, targetSOC: midExportFloor},
					blockActionChoice{actionType: blockActionExport, targetSOC: midExportFloor, useSolarExportPrefix: false},
					blockActionChoice{actionType: blockActionExport, targetSOC: midExportFloor, useSolarExportPrefix: true},
				)
			}
			// When a downstream block steps up minSOC (e.g. from 20% during peak to 80% post-peak),
			// validateCandidateTrajectory's ExportBelowFutureReserve check rejects any BatteryModeExport
			// (blockActionExport) that drains the battery below that future minSOC. Including coarse floors
			// {20, 40, 60, 80} that are >5% away from minSOC and midExportFloor ensures Pass 1 can find a
			// reserve-compliant BatteryModeExport floor. blockActionDirectSolarExport is omitted here because
			// it runs in BatteryModeLoad (not subject to ExportBelowFutureReserve), already tests minSOC and
			// midExportFloor above, and has its floor tuned across minSOC..100% in Pass 2.
			hasHigherFutureMinSOC := false
			for j := b + 1; j < len(blocks); j++ {
				if blocks[j].minSOC > minSOC+socTargetTolerancePct {
					hasHigherFutureMinSOC = true
					break
				}
			}
			if hasHigherFutureMinSOC {
				for _, coarseFloor := range []float64{20, 40, 60, 80} {
					if coarseFloor > minSOC+5.0 && math.Abs(coarseFloor-midExportFloor) > 5.0 {
						candidateTypes = append(candidateTypes,
							blockActionChoice{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
							blockActionChoice{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
						)
					}
				}
			}
			blockStartState := initialState
			if blocks[b].startIdx < len(currentStates) {
				blockStartState = currentStates[blocks[b].startIdx]
			}
			for _, cand := range candidateTypes {
				// skip any candidates that are equal to the existing block
				if sameBlockChoice(cand, currentChoices[b]) {
					continue
				}
				if !isBlockActionAllowed(b, blocks[b], cand, blocks, blockStartState, vppEvents, postHorizon.knownPostHorizonRate, settings, currentStatus, timeline, lastAction) {
					continue
				}
				copy(baseTrial, currentChoices)
				baseTrial[b] = cand

				// In addition to testing baseTrial directly, when cand charges or holds energy at block b,
				// also test variants that hold subsequent off-peak Load blocks in Standby so the energy is
				// not drained before the upcoming peak, as well as variants that recover any downstream
				// reserve deficits.
				trialVariants = trialVariants[:0]
				trialVariants = append(trialVariants, baseTrial)
				if (cand.actionType == blockActionCharge && cand.targetSOC > minSOC+socTargetTolerancePct) ||
					cand.actionType == blockActionStandby {
					trialVariants = append(trialVariants, holdInterveningOffPeakBlocks(baseTrial, b, len(blocks))...)
				}
				for _, sv := range trialVariants {
					if tcRec, ok := ensureReserveRecoveryBlocks(sv, b, -1); ok {
						trialVariants = append(trialVariants, tcRec)
					}
				}

				for _, trialChoices := range trialVariants {
					trialActs := expandBlockPlan(trialChoices, blocks, initialState, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
					valid, _ := validateCandidateTrajectory(trialActs, timeline, settings, wear, roundTripEff, initialState, initialDeficitWait)
					if !valid {
						continue
					}
					score, _, states, _ := scoreCandidateTrajectory(trialActs, timeline, initialState, vppEvents, postHorizon, settings, roundTripEff)
					if score < bestIterScore-planScoreTieEpsilon {
						bestIterScore = score
						copy(bestIterChoices, trialChoices)
						bestIterStates = states
						foundImprovement = true
					}
				}
			}
		}

		// 2. Paired-block trials: test coupled block modifications (a < b) that neither block would adopt alone,
		// such as pre-charging in block a to export in block b, holding in Standby in block a to export or charge
		// in block b, or splitting a large charge across two cheap blocks (a and b).
		for a := 0; a < len(blocks)-1; a++ {
			for b := a + 1; b < len(blocks); b++ {
				minSOCA := blocks[a].minSOC
				minSOCB := blocks[b].minSOC
				midExportFloorB := math.Round(((minSOCB+100.0)/2.0)/5.0) * 5.0
				pairedTrials = pairedTrials[:0]
				pairedTrials = append(pairedTrials,
					// Charge at a + Export at b
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionExport, targetSOC: minSOCB, useSolarExportPrefix: false},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionExport, targetSOC: minSOCB, useSolarExportPrefix: true},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 50},
						{actionType: blockActionExport, targetSOC: minSOCB, useSolarExportPrefix: false},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 50},
						{actionType: blockActionExport, targetSOC: minSOCB, useSolarExportPrefix: true},
					},
					// Export at a + Charge at b
					[2]blockActionChoice{
						{actionType: blockActionExport, targetSOC: minSOCA, useSolarExportPrefix: false},
						{actionType: blockActionCharge, targetSOC: 100},
					},
					[2]blockActionChoice{
						{actionType: blockActionExport, targetSOC: minSOCA, useSolarExportPrefix: true},
						{actionType: blockActionCharge, targetSOC: 100},
					},
					[2]blockActionChoice{
						{actionType: blockActionExport, targetSOC: minSOCA, useSolarExportPrefix: false},
						{actionType: blockActionCharge, targetSOC: 50},
					},
					[2]blockActionChoice{
						{actionType: blockActionExport, targetSOC: minSOCA, useSolarExportPrefix: true},
						{actionType: blockActionCharge, targetSOC: 50},
					},
					[2]blockActionChoice{
						{actionType: blockActionExport, targetSOC: minSOCA, useSolarExportPrefix: false},
						{actionType: blockActionCharge, targetSOC: minSOCB},
					},
					[2]blockActionChoice{
						{actionType: blockActionExport, targetSOC: minSOCA, useSolarExportPrefix: true},
						{actionType: blockActionCharge, targetSOC: minSOCB},
					},
					// Standby hold at a + Export at b
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionExport, targetSOC: minSOCB, useSolarExportPrefix: false},
					},
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionExport, targetSOC: minSOCB, useSolarExportPrefix: true},
					},
					// Multi-block charging: Charge at a + Charge at b.
					// Note on {Charge 100, Charge 75/50}: targetSOC on a block is a ceiling (expandBlockAction clamps
					// to the maximum SOC physically reachable during the block). When block a is short or inverter-power
					// limited (e.g. a 1-hour cheapest dynamic block that can only raise SOC from 20% to 40%), setting
					// targetSOC: 100 at a charges at full power for the entire cheapest hour, and targetSOC: 75/50 at b
					// finishes the partial charge in the second-cheapest block (or recharges after an intervening Load
					// block between a and b drained the battery).
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionCharge, targetSOC: 100},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionCharge, targetSOC: 75},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 50},
						{actionType: blockActionCharge, targetSOC: 100},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionCharge, targetSOC: 50},
					},
					// Charge at a + Standby/Load at b
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionStandby},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 75},
						{actionType: blockActionStandby},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 50},
						{actionType: blockActionStandby},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 100},
						{actionType: blockActionLoad},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 75},
						{actionType: blockActionLoad},
					},
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: 50},
						{actionType: blockActionLoad},
					},
					// Standby/Load at a + Charge at b (Waiting to charge or shifting charge to cheaper block)
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionCharge, targetSOC: 100},
					},
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionCharge, targetSOC: 75},
					},
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionCharge, targetSOC: 50},
					},
					[2]blockActionChoice{
						{actionType: blockActionLoad},
						{actionType: blockActionCharge, targetSOC: 100},
					},
					[2]blockActionChoice{
						{actionType: blockActionLoad},
						{actionType: blockActionCharge, targetSOC: 75},
					},
					[2]blockActionChoice{
						{actionType: blockActionLoad},
						{actionType: blockActionCharge, targetSOC: 50},
					},
					// Standby/Load swap between a and b (shifting Standby hold to cheaper block)
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionLoad},
					},
					[2]blockActionChoice{
						{actionType: blockActionLoad},
						{actionType: blockActionStandby},
					},
					// Multi-block reserve recharge: Charge at a + Charge at b
					[2]blockActionChoice{
						{actionType: blockActionCharge, targetSOC: minSOCA},
						{actionType: blockActionCharge, targetSOC: minSOCB},
					},
					// Standby hold at a + Reserve recharge at b
					[2]blockActionChoice{
						{actionType: blockActionStandby},
						{actionType: blockActionCharge, targetSOC: minSOCB},
					},
				)
				// Try exporting down to the midpoint between minSOCB and 100% after charging or holding at a
				// so a mid-peak export at b can preserve half the battery for later peak load. (Export at a +
				// Charge at b does not need a midpoint trial here because block a already has its starting SOC
				// and tests midExportFloor in the single-block loop.)
				if midExportFloorB > minSOCB+5.0 && midExportFloorB < 100.0-5.0 {
					pairedTrials = append(pairedTrials,
						[2]blockActionChoice{
							{actionType: blockActionCharge, targetSOC: 100},
							{actionType: blockActionExport, targetSOC: midExportFloorB, useSolarExportPrefix: false},
						},
						[2]blockActionChoice{
							{actionType: blockActionCharge, targetSOC: 100},
							{actionType: blockActionExport, targetSOC: midExportFloorB, useSolarExportPrefix: true},
						},

						[2]blockActionChoice{
							{actionType: blockActionStandby},
							{actionType: blockActionExport, targetSOC: midExportFloorB, useSolarExportPrefix: false},
						},
						[2]blockActionChoice{
							{actionType: blockActionStandby},
							{actionType: blockActionExport, targetSOC: midExportFloorB, useSolarExportPrefix: true},
						},
					)
				}
				// in the case of variable SOC, we try a bunch of different SOCs targets for export
				if len(settings.MinBatterySOCPeriods) > 0 {
					for _, coarseFloor := range []float64{20, 40, 60, 80} {
						if coarseFloor > minSOCB+5.0 && math.Abs(coarseFloor-midExportFloorB) > 5.0 {
							pairedTrials = append(pairedTrials,
								[2]blockActionChoice{
									{actionType: blockActionCharge, targetSOC: 100},
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
								},
								[2]blockActionChoice{
									{actionType: blockActionCharge, targetSOC: 100},
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
								},
								[2]blockActionChoice{
									{actionType: blockActionCharge, targetSOC: 50},
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
								},
								[2]blockActionChoice{
									{actionType: blockActionCharge, targetSOC: 50},
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
								},
								[2]blockActionChoice{
									{actionType: blockActionStandby},
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
								},
								[2]blockActionChoice{
									{actionType: blockActionStandby},
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
								},
							)
						}
						if coarseFloor > minSOCA+5.0 {
							pairedTrials = append(pairedTrials,
								[2]blockActionChoice{
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
									{actionType: blockActionCharge, targetSOC: 100},
								},
								[2]blockActionChoice{
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
									{actionType: blockActionCharge, targetSOC: 100},
								},
								[2]blockActionChoice{
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
									{actionType: blockActionCharge, targetSOC: 50},
								},
								[2]blockActionChoice{
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
									{actionType: blockActionCharge, targetSOC: 50},
								},
								[2]blockActionChoice{
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: false},
									{actionType: blockActionCharge, targetSOC: minSOCB},
								},
								[2]blockActionChoice{
									{actionType: blockActionExport, targetSOC: coarseFloor, useSolarExportPrefix: true},
									{actionType: blockActionCharge, targetSOC: minSOCB},
								},
							)
						}
					}
				}
				stateA := initialState
				if blocks[a].startIdx < len(currentStates) {
					stateA = currentStates[blocks[a].startIdx]
				}
				for _, pt := range pairedTrials {
					// Skip paired trials that do not modify both blocks (already covered by single-block trials),
					// unless pt[0] is Charge/Standby and uses b > a+1 as the stop index for holdInterveningOffPeakBlocks
					// or a > 0 as the stop index for holding preceding Load blocks (0..a-1).
					sameA := sameBlockChoice(pt[0], currentChoices[a])
					sameB := sameBlockChoice(pt[1], currentChoices[b])
					if sameA && sameB {
						continue
					}
					if sameA && (b == a+1 || (pt[0].actionType != blockActionCharge && pt[0].actionType != blockActionStandby)) {
						continue
					}
					if sameB &&
						(b == a+1 || (pt[0].actionType != blockActionCharge && pt[0].actionType != blockActionStandby)) &&
						(a == 0 || pt[0].actionType != blockActionCharge) {
						continue
					}
					if !isBlockActionAllowed(a, blocks[a], pt[0], blocks, stateA, vppEvents, postHorizon.knownPostHorizonRate, settings, currentStatus, timeline, lastAction) {
						continue
					}
					stateB := estimatePairedBlockStartState(a, b, pt, blocks, currentChoices, currentStates, initialState, roundTripEff)
					if !isBlockActionAllowed(b, blocks[b], pt[1], blocks, stateB, vppEvents, postHorizon.knownPostHorizonRate, settings, currentStatus, timeline, lastAction) {
						continue
					}
					copy(baseTrial, currentChoices)
					baseTrial[a] = pt[0]
					baseTrial[b] = pt[1]
					trialVariants = trialVariants[:0]
					trialVariants = append(trialVariants, baseTrial)
					// When testing a charge at block a > 0, also test holding in Standby across preceding Load blocks
					// (0..a-1) so the battery does not drain to reserve at a lower rate and force extra grid charging at a.
					if pt[0].actionType == blockActionCharge && a > 0 {
						tcHold := make([]blockActionChoice, len(baseTrial))
						copy(tcHold, baseTrial)
						hasPrecedingLoad := false
						for k := 0; k < a; k++ {
							if tcHold[k].actionType == blockActionLoad {
								tcHold[k] = blockActionChoice{actionType: blockActionStandby}
								hasPrecedingLoad = true
							}
						}
						if hasPrecedingLoad {
							trialVariants = append(trialVariants, tcHold)
						}
					}
					// Also test holding intervening cheaper Load blocks (a+1 .. b-1) in Standby so energy charged or held in a
					// does not discharge before reaching block b.
					if (pt[0].actionType == blockActionCharge && pt[0].targetSOC > minSOCA+socTargetTolerancePct) ||
						pt[0].actionType == blockActionStandby {
						trialVariants = append(trialVariants, holdInterveningOffPeakBlocks(baseTrial, a, b)...)
					}
					// When block b is a Charge or Standby block, also test holding intervening cheaper Load blocks after b.
					if (pt[1].actionType == blockActionCharge && pt[1].targetSOC > minSOCB+socTargetTolerancePct) ||
						pt[1].actionType == blockActionStandby {
						trialVariants = append(trialVariants, holdInterveningOffPeakBlocks(baseTrial, b, len(blocks))...)
					}
					for _, tv := range trialVariants {
						if tcRec, ok := ensureReserveRecoveryBlocks(tv, a, b); ok {
							trialVariants = append(trialVariants, tcRec)
						}
					}
					for _, trialChoices := range trialVariants {
						trialActs := expandBlockPlan(trialChoices, blocks, initialState, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
						valid, _ := validateCandidateTrajectory(trialActs, timeline, settings, wear, roundTripEff, initialState, initialDeficitWait)
						if !valid {
							continue
						}
						score, _, states, _ := scoreCandidateTrajectory(trialActs, timeline, initialState, vppEvents, postHorizon, settings, roundTripEff)
						if score < bestIterScore-planScoreTieEpsilon {
							bestIterScore = score
							copy(bestIterChoices, trialChoices)
							bestIterStates = states
							foundImprovement = true
						}
					}
				}
			}
		}

		if foundImprovement && (currentScore-bestIterScore >= 0.001) {
			savedDollars := currentScore - bestIterScore
			currentScore = bestIterScore
			copy(currentChoices, bestIterChoices)
			currentStates = bestIterStates
			log.Ctx(ctx).DebugContext(ctx, "plan critique round accepted",
				slog.Int("round", iter+1),
				slog.Float64("newScoreDollars", currentScore),
				slog.Float64("costSavedDollars", savedDollars),
			)
		} else {
			break
		}
	}

	if currentScore == math.MaxFloat64 {
		log.Ctx(ctx).WarnContext(ctx, "critiqueBlockPlan found no valid trajectory; falling back to baseline choices",
			slog.String("baseValidReason", baseValidReason),
		)
	}

	return currentChoices, currentScore
}

// coarseSOCGrid returns coarse SOC candidates from minSOC to maxSOC, including minSOC and
// every multiple of 10% in (minSOC, maxSOC] so standard reserve boundaries (20%, 40%, 50%, etc.)
// are always evaluated even when minSOC is not a multiple of 10% (e.g. 5% or 25%).
func coarseSOCGrid(minSOC, maxSOC float64) []float64 {
	grid := []float64{minSOC}
	firstTen := math.Ceil((minSOC+socTargetTolerancePct)/10.0) * 10.0
	for v := firstTen; v <= maxSOC; v += 10.0 {
		grid = append(grid, v)
	}
	if grid[len(grid)-1] < maxSOC-socTargetTolerancePct {
		grid = append(grid, maxSOC)
	}
	return grid
}

// optimizeBlockTargets executes Pass 2 of the solver: it refines the continuous targetSOC on winning
// Charge blocks and floorSOC on winning Export blocks via a coarse (10%) to fine (2%/5%) search.
func optimizeBlockTargets(
	ctx context.Context,
	winningChoices []blockActionChoice,
	blocks []timelineBlock,
	timeline []planInterval,
	initial planState,
	vppEvents []vppAnchor,
	postHorizon postHorizonValuation,
	settings types.Settings,
	currentStatus types.SystemStatus,
	lastAction *types.Action,
	initialDeficitWait time.Duration,
) *planPath {
	optParams := settings.GetOptimizationParams()
	roundTripEff := resolveRoundTripEfficiency(optParams.RoundTripEfficiency, currentStatus)
	wear := getWearRates(settings, optParams)

	choices := make([]blockActionChoice, len(winningChoices))
	copy(choices, winningChoices)
	trialChoices := make([]blockActionChoice, len(choices))

	// 1. Identify active Charge and Export blocks in the winning Pass 1 plan.
	chargeBlockIdx := -1
	exportBlockIdx := -1
	for b := 0; b < len(blocks); b++ {
		if choices[b].actionType == blockActionCharge && chargeBlockIdx == -1 {
			chargeBlockIdx = b
		}
		if choices[b].actionType == blockActionExport && exportBlockIdx == -1 {
			exportBlockIdx = b
		}
	}

	// 2. Joint 2D search when the plan contains both a Charge block and an Export block.
	// Evaluating (C, E) pairs jointly ensures coupled intermediate optima (e.g., Charge to 60% and Export down to 30%,
	// or Export down to 55% before an overnight Charge) are discovered directly without one variable's initial value masking the other.
	fineOffsets := []float64{-8, -6, -5, -4, -2, 0, 2, 4, 5, 6, 8}
	if chargeBlockIdx != -1 && exportBlockIdx != -1 {
		minC := blocks[chargeBlockIdx].minSOC
		maxC := 100.0
		minE := blocks[exportBlockIdx].minSOC
		maxE := 100.0

		bestScore := math.MaxFloat64
		initialC := choices[chargeBlockIdx].targetSOC
		initialE := choices[exportBlockIdx].targetSOC
		bestC := initialC
		bestE := initialE

		// Coarse 10% 2D grid
		for _, C := range coarseSOCGrid(minC, maxC) {
			for _, E := range coarseSOCGrid(minE, maxE) {
				copy(trialChoices, choices)
				trialChoices[chargeBlockIdx].targetSOC = C
				trialChoices[exportBlockIdx].targetSOC = E

				acts := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
				valid, _ := validateCandidateTrajectory(acts, timeline, settings, wear, roundTripEff, initial, initialDeficitWait)
				if !valid {
					continue
				}
				score, _, _, _ := scoreCandidateTrajectory(acts, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				// pick the best score or the tied score with the highest export
				if score < bestScore-planScoreTieEpsilon || (math.Abs(score-bestScore) <= planScoreTieEpsilon && E > bestE) {
					bestScore = score
					bestC = C
					bestE = E
				}
			}
		}

		// Fine 2%/5% local refinement around (coarseC, coarseE)
		coarseC, coarseE := bestC, bestE
		for _, dC := range fineOffsets {
			for _, dE := range fineOffsets {
				C := min(maxC, max(minC, coarseC+dC))
				E := min(100.0, max(minE, coarseE+dE))
				copy(trialChoices, choices)
				trialChoices[chargeBlockIdx].targetSOC = C
				trialChoices[exportBlockIdx].targetSOC = E

				acts := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
				valid, _ := validateCandidateTrajectory(acts, timeline, settings, wear, roundTripEff, initial, initialDeficitWait)
				if !valid {
					continue
				}
				score, _, _, _ := scoreCandidateTrajectory(acts, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				if score < bestScore-planScoreTieEpsilon || (math.Abs(score-bestScore) <= planScoreTieEpsilon && E > bestE) {
					bestScore = score
					bestC = C
					bestE = E
				}
			}
		}

		choices[chargeBlockIdx].targetSOC = bestC
		choices[exportBlockIdx].targetSOC = bestE
		log.Ctx(ctx).DebugContext(ctx, "plan block charge and export targets optimized jointly",
			slog.Int("chargeBlockIdx", chargeBlockIdx),
			slog.Float64("chargeTargetSOC", bestC),
			slog.Int("exportBlockIdx", exportBlockIdx),
			slog.Float64("exportFloorSOC", bestE),
			slog.Float64("bestScoreDollars", bestScore),
			slog.Float64("initialChargeTargetSOC", initialC),
			slog.Float64("initialExportFloorSOC", initialE),
		)
	}

	// Single-block 1D line searches (coarse 10% -> fine 2%/5% refinement) across all Charge and Export blocks.
	// Seeding bestScore at choices[b].targetSOC preserves any joint 2D optimum while still optimizing additional blocks.
	for b := 0; b < len(blocks); b++ {
		switch choices[b].actionType {
		case blockActionCharge:
			minC := blocks[b].minSOC
			maxC := 100.0
			bestScore := math.MaxFloat64
			initialC := choices[b].targetSOC
			bestC := initialC
			if initActs := expandBlockPlan(choices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff); len(initActs) > 0 {
				if valid, _ := validateCandidateTrajectory(initActs, timeline, settings, wear, roundTripEff, initial, initialDeficitWait); valid {
					bestScore, _, _, _ = scoreCandidateTrajectory(initActs, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				}
			}

			for _, C := range coarseSOCGrid(minC, maxC) {
				copy(trialChoices, choices)
				trialChoices[b].targetSOC = C
				acts := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
				valid, _ := validateCandidateTrajectory(acts, timeline, settings, wear, roundTripEff, initial, initialDeficitWait)
				if !valid {
					continue
				}
				score, _, _, _ := scoreCandidateTrajectory(acts, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				if score < bestScore-planScoreTieEpsilon || (math.Abs(score-bestScore) <= planScoreTieEpsilon && C < bestC) {
					bestScore = score
					bestC = C
				}
			}
			// Fine 2%/5% search around coarseC, preferring lower targetSOC on exact ties.
			coarseC := bestC
			for _, dT := range fineOffsets {
				C := min(maxC, max(minC, coarseC+dT))
				copy(trialChoices, choices)
				trialChoices[b].targetSOC = C
				acts := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
				valid, _ := validateCandidateTrajectory(acts, timeline, settings, wear, roundTripEff, initial, initialDeficitWait)
				if !valid {
					continue
				}
				score, _, _, _ := scoreCandidateTrajectory(acts, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				if score < bestScore-planScoreTieEpsilon || (math.Abs(score-bestScore) <= planScoreTieEpsilon && C < bestC) {
					bestScore = score
					bestC = C
				}
			}
			if initialC != bestC {
				choices[b].targetSOC = bestC
				log.Ctx(ctx).DebugContext(ctx, "plan charge block target optimized",
					slog.Int("blockIdx", b),
					slog.Float64("targetSOC", bestC),
					slog.Float64("initialTargetSOC", initialC),
					slog.Float64("bestScoreDollars", bestScore),
				)
			}

		case blockActionExport, blockActionDirectSolarExport:
			minE := blocks[b].minSOC
			maxE := 100.0
			bestScore := math.MaxFloat64
			initialE := choices[b].targetSOC
			bestE := initialE
			if initActs := expandBlockPlan(choices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff); len(initActs) > 0 {
				if valid, _ := validateCandidateTrajectory(initActs, timeline, settings, wear, roundTripEff, initial, initialDeficitWait); valid {
					bestScore, _, _, _ = scoreCandidateTrajectory(initActs, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				}
			}

			for _, E := range coarseSOCGrid(minE, maxE) {
				copy(trialChoices, choices)
				trialChoices[b].targetSOC = E
				acts := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
				valid, _ := validateCandidateTrajectory(acts, timeline, settings, wear, roundTripEff, initial, initialDeficitWait)
				if !valid {
					continue
				}
				score, _, _, _ := scoreCandidateTrajectory(acts, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				if score < bestScore-planScoreTieEpsilon || (math.Abs(score-bestScore) <= planScoreTieEpsilon && E > bestE) {
					bestScore = score
					bestE = E
				}
			}
			// Fine 2%/5% search around coarseE, preferring higher floorSOC on exact ties.
			coarseE := bestE
			for _, dF := range fineOffsets {
				E := min(maxE, max(minE, coarseE+dF))
				copy(trialChoices, choices)
				trialChoices[b].targetSOC = E
				acts := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
				valid, _ := validateCandidateTrajectory(acts, timeline, settings, wear, roundTripEff, initial, initialDeficitWait)
				if !valid {
					continue
				}
				score, _, _, _ := scoreCandidateTrajectory(acts, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
				if score < bestScore-planScoreTieEpsilon || (math.Abs(score-bestScore) <= planScoreTieEpsilon && E > bestE) {
					bestScore = score
					bestE = E
				}
			}
			if initialE != bestE {
				choices[b].targetSOC = bestE
				log.Ctx(ctx).DebugContext(ctx, "plan export block target optimized",
					slog.Int("blockIdx", b),
					slog.Float64("floorSOC", bestE),
					slog.Float64("initialFloorSOC", initialE),
					slog.Float64("bestScoreDollars", bestScore),
				)
			}
		}
	}

	// 3. Final trajectory execution and step-0 mode comparison debug log.
	finalActs := expandBlockPlan(choices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
	score, totalPhys, states, metrics := scoreCandidateTrajectory(finalActs, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)

	if len(blocks) > 0 && len(finalActs) > 0 {
		defaultSolarMode := types.SolarModeAny
		if !settings.GridExportSolar || blocks[0].maxExportRate < 0 {
			defaultSolarMode = types.SolarModeNoExport
		}
		step0Modes := []struct {
			name   string
			choice blockActionChoice
			cand   actionCandidate
		}{
			{"Load", blockActionChoice{actionType: blockActionLoad}, actionCandidate{batteryMode: types.BatteryModeLoad, solarMode: defaultSolarMode}},
			{"Standby", blockActionChoice{actionType: blockActionStandby}, actionCandidate{batteryMode: types.BatteryModeStandby, solarMode: defaultSolarMode}},
			{"ChargeAny", blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, actionCandidate{batteryMode: types.BatteryModeChargeAny, solarMode: defaultSolarMode}},
			{"Export", blockActionChoice{actionType: blockActionExport, targetSOC: blocks[0].minSOC}, actionCandidate{batteryMode: types.BatteryModeExport, solarMode: defaultSolarMode}},
			{"DirectSolarExport", blockActionChoice{actionType: blockActionDirectSolarExport}, actionCandidate{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeExport}},
		}
		var modeDeltas []string
		for _, m := range step0Modes {
			copy(trialChoices, choices)
			trialChoices[0] = m.choice
			tActs := expandBlockPlan(trialChoices, blocks, initial, timeline, vppEvents, settings, currentStatus, lastAction, roundTripEff)
			if len(tActs) > 0 {
				tActs[0] = m.cand
			}
			sc, _, _, _ := scoreCandidateTrajectory(tActs, timeline, initial, vppEvents, postHorizon, settings, roundTripEff)
			modeDeltas = append(modeDeltas, fmt.Sprintf("%s:$%.3f(+$%.3f)", m.name, sc, max(0.0, sc-score)))
		}
		log.Ctx(ctx).DebugContext(ctx, "plan step 0 modes compared",
			slog.Float64("winningScoreDollars", score),
			slog.Any("step0ModeScores", modeDeltas),
		)
	}

	return &planPath{
		actions:   finalActs,
		states:    states,
		metrics:   metrics,
		timeline:  timeline,
		totalCost: totalPhys,
		bestScore: score,
	}
}

// searchOptimalPlanBlocks is the two-pass timeline-block-based plan search engine.
// Pass 1 segments the 20-minute timeline into 6-10 timeline blocks and iteratively critiques
// single-block and paired-block action choices. Pass 2 tunes continuous targetSOC and floorSOC
// thresholds on the winning Charge and Export blocks.
func (c *Controller) searchOptimalPlanBlocks(
	ctx context.Context,
	timeline []planInterval,
	initial planState,
	vppEvents []vppAnchor,
	postHorizon postHorizonValuation,
	settings types.Settings,
	currentStatus types.SystemStatus,
	lastAction *types.Action,
) *planPath {
	if len(timeline) == 0 {
		return &planPath{}
	}

	refTime := initial.time
	if !timeline[0].startTime.IsZero() {
		refTime = timeline[0].startTime
	}
	lastAction = sanitizeLastAction(lastAction, refTime)
	initial.maxChargeKW = resolveBatteryPowerKW(initial.maxChargeKW, initial.capacityKWH)
	initial.maxDischargeKW = resolveBatteryPowerKW(initial.maxDischargeKW, initial.capacityKWH)

	initialDeficitWait := computeInitialDeficitWait(initial, timeline, lastAction)

	// 1. Build timeline blocks
	blocks := buildTimelineBlocks(ctx, timeline, vppEvents, settings, currentStatus)
	log.Ctx(ctx).DebugContext(ctx, "plan timeline blocks built",
		slog.Int("timelineIntervals", len(timeline)),
		slog.Int("timelineBlocks", len(blocks)),
		slog.Float64("initialSOC", initial.soc),
	)

	if len(blocks) == 0 {
		return &planPath{}
	}

	// 2. Pass 1: Critique loop
	winningChoices, _ := critiqueBlockPlan(ctx, blocks, timeline, initial, vppEvents, postHorizon, settings, currentStatus, lastAction, initialDeficitWait)

	// 3. Pass 2: Target SOC line search
	return optimizeBlockTargets(ctx, winningChoices, blocks, timeline, initial, vppEvents, postHorizon, settings, currentStatus, lastAction, initialDeficitWait)
}
