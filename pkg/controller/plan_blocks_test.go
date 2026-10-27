package controller

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTimelineBlocks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	baseSettings := types.Settings{
		OptimizationProfile: "balanced",
	}

	t.Run("AdaptiveBanding_ComEdHourlyPricing", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 48)
		for i := 0; i < 48; i++ {
			ti := now.Add(time.Duration(i*30) * time.Minute)
			rate := 0.05 + 0.35*math.Sin(float64(i)/48.0*math.Pi)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    rate,
				exportRate:    rate * 0.9,
				solarKWH:      0.0,
				loadKWH:       0.5,
				minSOC:        20.0,
			}
		}

		blocks := buildTimelineBlocks(ctx, timeline, nil, baseSettings, types.SystemStatus{})
		assert.GreaterOrEqual(t, len(blocks), 4, "Should build at least 4 blocks")
		assert.LessOrEqual(t, len(blocks), 16, "Should coalesce to <= 16 blocks (target ~10)")

		totalIntervals := 0
		for _, b := range blocks {
			totalIntervals += (b.endIdx - b.startIdx + 1)
		}
		assert.Equal(t, 48, totalIntervals, "All intervals must be partitioned without gaps")
	})

	t.Run("HardBoundaries_VPP_Reserve", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 24)
		for i := 0; i < 24; i++ {
			ti := now.Add(time.Duration(i) * time.Hour)
			minSOC := 20.0
			if i >= 12 {
				minSOC = 50.0 // Reserve change at i=12
			}
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(1 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.12,
				solarKWH:      0.0,
				loadKWH:       0.5,
				minSOC:        minSOC,
			}
		}

		vppDeadline := now.Add(6 * time.Hour)
		vppEvents := []vppAnchor{
			{eventStart: now.Add(4 * time.Hour), eventEnd: now.Add(6 * time.Hour), deadline: vppDeadline, mandatory: true},
		}

		blocks := buildTimelineBlocks(ctx, timeline, vppEvents, baseSettings, types.SystemStatus{})

		hasVPPBoundary := false
		hasReserveBoundary := false

		for _, b := range blocks {
			if b.endIdx == 3 || b.startIdx == 4 || b.endIdx == 5 || b.startIdx == 6 {
				hasVPPBoundary = true
			}
			if b.startIdx == 12 {
				hasReserveBoundary = true
			}
		}

		assert.True(t, hasVPPBoundary, "Must split block at VPP event boundary")
		assert.True(t, hasReserveBoundary, "Must split block at reserve change boundary")
	})

	t.Run("HardBoundaries_StepZeroActiveEV", func(t *testing.T) {
		t.Parallel()
		nightNow := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
		evSettings := baseSettings
		evSettings.EVChargingStandby = true
		timelineEV := make([]planInterval, 6)
		for i := 0; i < 6; i++ {
			ti := nightNow.Add(time.Duration(i*30) * time.Minute)
			timelineEV[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    0.15,
				exportRate:    0.12,
				minSOC:        20.0,
			}
		}
		statusEV := types.SystemStatus{HomeKW: 9.5} // Large EV draw
		blocksEV := buildTimelineBlocks(ctx, timelineEV, nil, evSettings, statusEV)
		if assert.Len(t, blocksEV, 2) {
			// Inclusive [0..0] is 1 period long (timelineEV[0]), and [1..5] covers the remaining 5 periods
			assert.Equal(t, 0, blocksEV[0].startIdx)
			assert.Equal(t, 0, blocksEV[0].endIdx, "Step 0 active EV charging must trigger its own 1-period block boundary")
			assert.InDelta(t, 0.5, blocksEV[0].durationHours, 1e-6)
			assert.Equal(t, 1, blocksEV[1].startIdx)
			assert.Equal(t, 5, blocksEV[1].endIdx)
		}
	})

	t.Run("HardBoundaries_NegativePrice", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 6)
		rates := []float64{0.10, 0.10, -0.02, -0.02, 0.10, 0.10}
		for i := 0; i < 6; i++ {
			ti := now.Add(time.Duration(i*30) * time.Minute)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    rates[i],
				exportRate:    rates[i] * 0.9,
				minSOC:        20.0,
			}
		}
		blocks := buildTimelineBlocks(ctx, timeline, nil, baseSettings, types.SystemStatus{})
		require.NotEmpty(t, blocks)
		hasNegativeBlock := false
		for _, b := range blocks {
			if b.startIdx == 2 && b.endIdx == 3 {
				hasNegativeBlock = true
				assert.LessOrEqual(t, b.maxImportRate, 0.0)
			}
		}
		assert.True(t, hasNegativeBlock, "Negative price intervals must form their own isolated timeline block")
	})

	t.Run("ExceedsTargetBlocks_LogsSplitCauses", func(t *testing.T) {
		t.Parallel()
		// Create 12 intervals with alternating reserve floors (20% and 50%) plus a negative price at interval 0
		// so that every interval boundary is a hard split and len(blocks) == 12 > targetTimelineBlocks (10).
		timeline := make([]planInterval, 12)
		for i := 0; i < 12; i++ {
			ti := now.Add(time.Duration(i*30) * time.Minute)
			minSOC := 20.0
			if i%2 == 1 {
				minSOC = 50.0
			}
			rate := 0.15
			if i == 0 {
				rate = -0.05
			}
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    rate,
				exportRate:    0.05,
				minSOC:        minSOC,
			}
		}

		var logBuf bytes.Buffer
		debugCtx := log.With(ctx, slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		blocks := buildTimelineBlocks(debugCtx, timeline, nil, baseSettings, types.SystemStatus{})
		assert.Len(t, blocks, 12)
		assert.Contains(t, logBuf.String(), "timeline blocks exceeded target count")
		assert.Contains(t, logBuf.String(), "initBlocksCount=12")
		assert.Contains(t, logBuf.String(), "finalBlocksCount=12")
		assert.Contains(t, logBuf.String(), "minSOC(20->50)")
	})

	t.Run("SecondPass_SplitsFlatRateDayWithAndWithoutSolar", func(t *testing.T) {
		t.Parallel()

		// 1. Flat-rate 24h timeline with daytime solar (09:00-16:00):
		// Second pass must split at solar start (09:00) and 1h-padded solar end (17:00) into 3 blocks.
		timelineSolar := make([]planInterval, 24)
		for i := 0; i < 24; i++ {
			ti := now.Add(time.Duration(i) * time.Hour)
			solar := 0.0
			if i >= 9 && i <= 15 {
				solar = 2.0
			}
			timelineSolar[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.05,
				solarKWH:      solar,
				loadKWH:       0.8,
				minSOC:        20.0,
			}
		}
		var solarLogBuf bytes.Buffer
		solarDebugCtx := log.With(ctx, slog.New(slog.NewTextHandler(&solarLogBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		blocksSolar := buildTimelineBlocks(solarDebugCtx, timelineSolar, nil, baseSettings, types.SystemStatus{})
		if assert.Len(t, blocksSolar, 3) {
			assert.Equal(t, 0, blocksSolar[0].startIdx)
			assert.Equal(t, 8, blocksSolar[0].endIdx, "Pre-solar block must end immediately before solar starts (idx 8 = 08:00-09:00)")
			assert.Equal(t, 9, blocksSolar[1].startIdx)
			assert.Equal(t, 16, blocksSolar[1].endIdx, "Solar block must include 1 hour after solar ends (idx 16 = 16:00-17:00)")
			assert.Equal(t, 17, blocksSolar[2].startIdx)
			assert.Equal(t, 23, blocksSolar[2].endIdx)
			assert.Contains(t, solarLogBuf.String(), "timeline blocks built")
			assert.Contains(t, solarLogBuf.String(), "initBlocksCount=1")
			assert.Contains(t, solarLogBuf.String(), "numSplitBlocks=1")
			assert.Contains(t, solarLogBuf.String(), "splitOnFirstSun=1")
			assert.Contains(t, solarLogBuf.String(), "splitOnLastSun=1")
			assert.Contains(t, solarLogBuf.String(), "finalBlocksCount=3")
		}

		// 2. Flat-rate 24h timeline without solar:
		// Second pass must split into 3 segments (an initial ~2h segment plus two halves).
		timelineNoSolar := make([]planInterval, 24)
		for i := 0; i < 24; i++ {
			ti := now.Add(time.Duration(i) * time.Hour)
			timelineNoSolar[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.05,
				solarKWH:      0.0,
				loadKWH:       0.8,
				minSOC:        20.0,
			}
		}
		blocksNoSolar := buildTimelineBlocks(ctx, timelineNoSolar, nil, baseSettings, types.SystemStatus{})
		if assert.Len(t, blocksNoSolar, 3) {
			assert.InDelta(t, 2.0, blocksNoSolar[0].durationHours, 0.5)
			assert.Equal(t, 23, blocksNoSolar[2].endIdx)
		}

		// 3. Multi-block TOU horizon (3 initial blocks: 12h daytime Off-Peak 06:00-18:00, 4h On-Peak 18:00-22:00,
		// 8h overnight Off-Peak 22:00-06:00) where daytime solar exceeds load from 08:00-16:00 and drops below
		// load from 16:00-18:00. Second pass must split the 12h daytime Off-Peak block around the net-solar window.
		timelineTOU := make([]planInterval, 24)
		touStart := time.Date(2026, 6, 15, 6, 0, 0, 0, time.UTC)
		for i := 0; i < 24; i++ {
			ti := touStart.Add(time.Duration(i) * time.Hour)
			rate := 0.12
			if i >= 12 && i < 16 {
				rate = 0.35 // 18:00-22:00 On-Peak
			}
			solar := 0.0
			if i >= 1 && i <= 10 {
				solar = 0.3 // Weak early/late sun at 07:00 and 16:00
			}
			if i >= 2 && i <= 9 {
				solar = 3.0 // Net-positive solar 08:00-16:00 (idx 2..9)
			}
			timelineTOU[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(time.Hour),
				durationHours: 1.0,
				importRate:    rate,
				exportRate:    0.05,
				solarKWH:      solar,
				loadKWH:       1.0,
				minSOC:        20.0,
			}
		}
		blocksTOU := buildTimelineBlocks(ctx, timelineTOU, nil, baseSettings, types.SystemStatus{})
		if assert.Len(t, blocksTOU, 5) {
			assert.Equal(t, 0, blocksTOU[0].startIdx)
			assert.Equal(t, 1, blocksTOU[0].endIdx, "Pre-solar block must end immediately before significant solar starts (idx 0..1 = 06:00-08:00)")
			assert.Equal(t, 2, blocksTOU[1].startIdx)
			assert.Equal(t, 10, blocksTOU[1].endIdx, "Solar block must start at significant solar and include 1 hour after solar ends (idx 2..10 = 08:00-17:00)")
			assert.Equal(t, 11, blocksTOU[2].startIdx)
			assert.Equal(t, 11, blocksTOU[2].endIdx, "Post-solar block before Peak must cover the trailing dark hour (idx 11 = 17:00-18:00)")
		}
	})
}

func TestExpandBlockAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	settings := types.Settings{
		GridChargeBatteries: true,
		GridExportBatteries: true,
		GridExportSolar:     true,
		ManageTOUSchedules:  true,
		OptimizationProfile: "balanced",
	}
	state20 := planState{soc: 20.0, energyKWH: 13.5 * 0.20, capacityKWH: 13.5, time: now}
	state80 := planState{soc: 80.0, energyKWH: 13.5 * 0.80, capacityKWH: 13.5, time: now}

	t.Run("BlockCharge_PlacesWindowAtCheapest", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 6)
		rates := []float64{0.20, 0.20, 0.05, 0.05, 0.20, 0.20}
		for i := 0; i < 6; i++ {
			ti := now.Add(time.Duration(i*20) * time.Minute)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    rates[i],
				exportRate:    rates[i] * 0.8,
				solarKWH:      0.0,
				loadKWH:       0.2,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{startIdx: 0, endIdx: 5, durationHours: 2.0, minSOC: 20.0, minImportRate: 0.05, avgImportRate: 0.15, maxImportRate: 0.20}

		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 30.0}, state20, timeline, nil, settings, types.SystemStatus{}, nil)
		require.Len(t, acts, 6)

		hasChargeInCheapest := false
		for i := 2; i <= 3; i++ {
			if acts[i].batteryMode == types.BatteryModeChargeAny {
				hasChargeInCheapest = true
			}
		}
		assert.True(t, hasChargeInCheapest, "Charging must be placed in cheapest intervals (indices 2-3)")
		assert.Equal(t, types.BatteryModeStandby, acts[0].batteryMode, "More expensive interval 0 should be Standby")
	})

	t.Run("BlockCharge_BuffersBeforeBlockEndOnTies", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 6)
		for i := 0; i < 6; i++ {
			ti := now.Add(time.Duration(i*20) * time.Minute)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.10,
				exportRate:    0.08,
				solarKWH:      0.0,
				loadKWH:       0.2,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{startIdx: 0, endIdx: 5, durationHours: 2.0, minSOC: 20.0, minImportRate: 0.10, avgImportRate: 0.10, maxImportRate: 0.10}

		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 30.0}, state20, timeline, nil, settings, types.SystemStatus{}, nil)
		require.Len(t, acts, 6)

		assert.NotEqual(t, types.BatteryModeChargeAny, acts[5].batteryMode, "End-of-block buffering must leave interval at the end free")
	})

	t.Run("BlockCharge_StepZeroContinuity", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 4)
		for i := 0; i < 4; i++ {
			ti := now.Add(time.Duration(i*20) * time.Minute)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.10,
				exportRate:    0.08,
				solarKWH:      0.0,
				loadKWH:       0.2,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{startIdx: 0, endIdx: 3, durationHours: 4.0 / 3.0, minSOC: 20.0, minImportRate: 0.10, avgImportRate: 0.10}
		lastAct := &types.Action{BatteryMode: types.BatteryModeChargeAny}

		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 30.0}, state20, timeline, nil, settings, types.SystemStatus{}, lastAct)
		require.Len(t, acts, 4)
		assert.Equal(t, types.BatteryModeChargeAny, acts[0].batteryMode, "Step 0 must continue charging without interruption when rates are tied")

		// If step 0 rate turns higher than a later interval in the block, window search must pick the cheaper window
		timeline[0].importRate = 0.15
		timeline[1].importRate = 0.05
		actsCheaperLater := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 30.0}, state20, timeline, nil, settings, types.SystemStatus{}, lastAct)
		require.Len(t, actsCheaperLater, 4)
		assert.Equal(t, types.BatteryModeStandby, actsCheaperLater[0].batteryMode, "Must not lock onto step 0 when a later interval in the block is cheaper")
		assert.Equal(t, types.BatteryModeChargeAny, actsCheaperLater[1].batteryMode, "Must place charge in the cheaper interval")
	})

	t.Run("BlockExport_WithLoadPrefix_And_WithDirectSolarExportPrefix", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 4)
		for i := 0; i < 4; i++ {
			ti := now.Add(time.Duration(i*20) * time.Minute)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.35,
				exportRate:    0.30,
				solarKWH:      0.0,
				loadKWH:       0.5,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{startIdx: 0, endIdx: 3, durationHours: 4.0 / 3.0, minSOC: 20.0, avgImportRate: 0.35, maxExportRate: 0.30}

		actsLoadPre := expandBlockAction(block, blockActionChoice{actionType: blockActionExport, targetSOC: 40.0, useSolarExportPrefix: false}, state80, timeline, nil, settings, types.SystemStatus{}, nil)
		require.Len(t, actsLoadPre, 4)
		assert.Equal(t, types.BatteryModeLoad, actsLoadPre[0].batteryMode)
		assert.Equal(t, types.BatteryModeExport, actsLoadPre[3].batteryMode)

		timelineSolar := make([]planInterval, 4)
		copy(timelineSolar, timeline)
		for i := 0; i < 4; i++ {
			timelineSolar[i].solarKWH = 2.0
		}
		actsDSEPre := expandBlockAction(block, blockActionChoice{actionType: blockActionExport, targetSOC: 60.0, useSolarExportPrefix: true}, state80, timelineSolar, nil, settings, types.SystemStatus{}, nil)
		require.Len(t, actsDSEPre, 4)
		assert.Equal(t, types.BatteryModeLoad, actsDSEPre[0].batteryMode)
		assert.Equal(t, types.SolarModeExport, actsDSEPre[0].solarMode, "Prefix must run DirectSolarExport when enabled")
		assert.Equal(t, types.BatteryModeExport, actsDSEPre[3].batteryMode)
	})

	t.Run("BlockDirectSolarExport_BanksSolarBeforeExportingToSurvivePeak", func(t *testing.T) {
		t.Parallel()
		// 6-interval peak block: intervals 0..3 are sunny (2.5 kWh solar, 0.5 kWh load),
		// intervals 4..5 are dark evening peak (0.0 kWh solar, 1.5 kWh load).
		// Starting at 22% SOC (minSOC = 20%), the battery does not have enough charge to survive
		// intervals 4..5 if it exports all solar from interval 0. It must charge from solar in
		// BatteryModeLoad + SolarModeAny for the early intervals and then switch to SolarModeExport
		// for the remaining sunny intervals once enough energy is banked.
		timeline := make([]planInterval, 6)
		for i := 0; i < 6; i++ {
			ti := now.Add(time.Duration(i*20) * time.Minute)
			solar := 2.5
			load := 0.5
			if i >= 4 {
				solar = 0.0
				load = 1.5
			}
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.35,
				exportRate:    0.30,
				solarKWH:      solar,
				loadKWH:       load,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{
			startIdx:      0,
			endIdx:        5,
			durationHours: 2.0,
			minSOC:        20.0,
			avgImportRate: 0.35,
			maxExportRate: 0.30,
		}
		stateLow := planState{soc: 22.0, energyKWH: 13.5 * 0.22, capacityKWH: 13.5, time: now}
		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionDirectSolarExport, targetSOC: 20.0}, stateLow, timeline, nil, settings, types.SystemStatus{}, nil)
		require.Len(t, acts, 6)
		assert.Equal(t, types.BatteryModeLoad, acts[0].batteryMode)
		assert.Equal(t, types.SolarModeAny, acts[0].solarMode, "Early interval must bank solar in SolarModeAny when battery cannot yet survive the peak block")
		assert.Equal(t, types.SolarModeExport, acts[3].solarMode, "Later sunny interval must switch to SolarModeExport once enough energy is banked to survive the block")
	})

	t.Run("BlockCharge_HeadroomClampedForSolarSurplus", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 8)
		for i := 0; i < 8; i++ {
			ti := now.Add(time.Duration(i*30) * time.Minute)
			solar := 0.0
			if i >= 4 && i < 6 {
				solar = 3.0 // 3.0 kWh solar in intervals 4-5
			}
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    0.10,
				exportRate:    0.08,
				solarKWH:      solar,
				loadKWH:       0.2,
				minSOC:        20.0,
			}
		}
		vppEvents := []vppAnchor{
			{eventStart: now.Add(4 * time.Hour), eventEnd: now.Add(6 * time.Hour), deadline: now.Add(4 * time.Hour)},
		}
		block := timelineBlock{startIdx: 0, endIdx: 5, durationHours: 3.0, minSOC: 20.0, minImportRate: 0.10, avgImportRate: 0.10}
		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, state20, timeline, vppEvents, settings, types.SystemStatus{}, nil)
		require.Len(t, acts, 6)
		for _, act := range acts {
			if act.batteryMode == types.BatteryModeChargeAny {
				assert.Less(t, act.targetSOC, 100.0, "Pre-charge target must be clamped to reserve headroom for daytime solar surplus")
			}
		}
	})

	t.Run("ActiveEVCharging_LocksLoadIntoStandby_ButAllowsChargeToBypass", func(t *testing.T) {
		t.Parallel()
		// 3:00 AM timestamp (eligible for EV standby) with active EV load (10 kW)
		evTime := time.Date(2026, 6, 15, 3, 0, 0, 0, time.UTC)
		timeline := []planInterval{
			{
				startTime:     evTime,
				endTime:       evTime.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.05,
				exportRate:    0.04,
				solarKWH:      0.0,
				loadKWH:       3.33,
				minSOC:        20.0,
			},
		}
		block := timelineBlock{startIdx: 0, endIdx: 0, durationHours: 1.0 / 3.0, minSOC: 20.0, minImportRate: 0.05, avgImportRate: 0.05}
		evSettings := types.Settings{EVChargingStandby: true}
		evStatus := types.SystemStatus{HomeKW: 10.0}

		loadActs := expandBlockAction(block, blockActionChoice{actionType: blockActionLoad}, state80, timeline, nil, evSettings, evStatus, nil)
		require.Len(t, loadActs, 1)
		assert.Equal(t, types.BatteryModeStandby, loadActs[0].batteryMode, "Load block must be overridden to Standby during active EV charging")
		assert.Equal(t, types.ActionReasonEVChargingStandby, loadActs[0].reason)

		chargeActs := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, state20, timeline, nil, evSettings, evStatus, nil)
		require.Len(t, chargeActs, 1)
		assert.Equal(t, types.BatteryModeChargeAny, chargeActs[0].batteryMode, "Charge block must bypass EV charging standby when charging is required")
	})

	t.Run("VPPPrepBlock_TransitionsFromChargeToStandbyMidBlock", func(t *testing.T) {
		t.Parallel()
		// 2-interval mandatory VPP preparation block (1 hour before eventStart) starting at 90% SOC:
		// Interval 0 charges the battery from 90% to 100% SOC (>= 99% vppMinFullCapacitySOC),
		// so Interval 1 must transition from ChargeAny ("VPP Prep emergency top-up.") to Standby ("VPP Standby Lock.").
		vppEventStart := now.Add(1 * time.Hour)
		vppEvents := []vppAnchor{
			{
				eventStart: vppEventStart,
				eventEnd:   vppEventStart.Add(2 * time.Hour),
				deadline:   vppEventStart.Add(-vppStandbyLeadTime),
				mandatory:  true,
				vppSoc:     20.0,
			},
		}
		timeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    0.15,
				exportRate:    0.05,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(30 * time.Minute),
				endTime:       now.Add(60 * time.Minute),
				durationHours: 0.5,
				importRate:    0.15,
				exportRate:    0.05,
				minSOC:        20.0,
			},
		}
		block := timelineBlock{startIdx: 0, endIdx: 1, durationHours: 1.0, minSOC: 20.0, minImportRate: 0.15, avgImportRate: 0.15}
		state90 := planState{soc: 90.0, energyKWH: 13.5 * 0.90, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}

		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionLoad}, state90, timeline, vppEvents, settings, types.SystemStatus{}, nil)
		require.Len(t, acts, 2)
		assert.Equal(t, types.BatteryModeChargeAny, acts[0].batteryMode, "Interval 0 at 90% SOC must perform VPP Prep emergency top-up")
		assert.Equal(t, types.ActionReasonVPPPrep, acts[0].reason)
		assert.Equal(t, types.BatteryModeStandby, acts[1].batteryMode, "Interval 1 after reaching 100% SOC must switch to VPP Standby Lock")
		assert.Equal(t, types.ActionReasonVPPPrep, acts[1].reason)
	})

	t.Run("BlockCharge_ReserveDeficitRecoveryHoldsStandbyAfterCharge", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 4)
		for i := 0; i < 4; i++ {
			ti := now.Add(time.Duration(i*20) * time.Minute)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.12,
				exportRate:    0.05,
				solarKWH:      0.0,
				loadKWH:       0.2,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{startIdx: 0, endIdx: 3, durationHours: 4.0 / 3.0, minSOC: 20.0, minImportRate: 0.12, avgImportRate: 0.12, maxImportRate: 0.12}
		stateDeficit := planState{soc: 15.0, energyKWH: 13.5 * 0.15, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}

		acts := expandBlockAction(block, blockActionChoice{actionType: blockActionCharge, targetSOC: 20.0}, stateDeficit, timeline, nil, settings, types.SystemStatus{}, nil)
		require.Len(t, acts, 4)
		assert.Equal(t, types.BatteryModeChargeAny, acts[0].batteryMode, "Interval 0 must recharge reserve deficit to minSOC")
		for i := 1; i < 4; i++ {
			assert.Equal(t, types.BatteryModeStandby, acts[i].batteryMode, "Non-charging intervals in blockActionCharge must hold in BatteryModeStandby")
		}
	})

	t.Run("DirectSolarExport_ManageTOUSchedules", func(t *testing.T) {
		t.Parallel()
		timeline := make([]planInterval, 3)
		for i := 0; i < 3; i++ {
			ti := now.Add(time.Duration(i) * time.Hour)
			timeline[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				exportRate:    0.25,
				solarKWH:      4.0,
				loadKWH:       1.0,
				minSOC:        20.0,
			}
		}
		block := timelineBlock{startIdx: 0, endIdx: 2, durationHours: 3.0, minSOC: 20.0, minImportRate: 0.35, avgImportRate: 0.35, maxImportRate: 0.35, maxExportRate: 0.25}
		stateFull := planState{soc: 90.0, energyKWH: 13.5 * 0.90, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}

		// When ManageTOUSchedules is true, findSolarExportStartIdx returns (0, true) and DirectSolarExport exports solar.
		touSettings := settings
		touSettings.ManageTOUSchedules = true
		touSettings.GridExportSolar = true
		startIdx, found := findSolarExportStartIdx(block, stateFull, timeline, touSettings, types.SolarModeAny, 20.0, 0.90)
		assert.True(t, found)
		assert.Equal(t, 0, startIdx)

		touActs := expandBlockAction(block, blockActionChoice{actionType: blockActionDirectSolarExport, targetSOC: 20.0}, stateFull, timeline, nil, touSettings, types.SystemStatus{}, nil)
		require.Len(t, touActs, 3)
		for i := 0; i < 3; i++ {
			assert.Equal(t, types.SolarModeExport, touActs[i].solarMode)
		}

		// When ManageTOUSchedules is false, findSolarExportStartIdx returns (0, false) and solar export is not scheduled.
		noTouSettings := touSettings
		noTouSettings.ManageTOUSchedules = false
		startIdxNoTOU, foundNoTOU := findSolarExportStartIdx(block, stateFull, timeline, noTouSettings, types.SolarModeAny, 20.0, 0.90)
		assert.False(t, foundNoTOU)
		assert.Equal(t, 0, startIdxNoTOU)

		noTouActs := expandBlockAction(block, blockActionChoice{actionType: blockActionDirectSolarExport, targetSOC: 20.0}, stateFull, timeline, nil, noTouSettings, types.SystemStatus{}, nil)
		require.Len(t, noTouActs, 3)
		for i := 0; i < 3; i++ {
			assert.Equal(t, types.SolarModeAny, noTouActs[i].solarMode, "When ManageTOUSchedules is false, solar mode must fall back to default SolarModeAny")
		}
	})
}

func TestIsBlockActionAllowed(t *testing.T) {
	t.Parallel()

	settings := types.Settings{
		GridChargeBatteries: true,
		GridExportBatteries: true,
		GridExportSolar:     true,
		ManageTOUSchedules:  true,
		OptimizationProfile: "balanced",
	}
	var vppEvents []vppAnchor
	postHorizonRate := 0.0
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	blockCheap := timelineBlock{
		startIdx:       0,
		endIdx:         2,
		minImportRate:  0.05,
		avgImportRate:  0.05,
		maxImportRate:  0.05,
		minSOC:         20.0,
		durationHours:  1.0,
		isLowestPrice:  true,
		isHighestPrice: false,
	}
	blockExpensive := timelineBlock{
		startIdx:       3,
		endIdx:         5,
		minImportRate:  0.40,
		avgImportRate:  0.40,
		maxImportRate:  0.40,
		minExportRate:  0.35,
		maxExportRate:  0.35,
		minSOC:         20.0,
		durationHours:  1.0,
		netSolarKWH:    5.0,
		isLowestPrice:  false,
		isHighestPrice: true,
	}
	blocks := []timelineBlock{blockCheap, blockExpensive}
	baseTimeline := make([]planInterval, 6)
	for i := 0; i < 6; i++ {
		ti := now.Add(time.Duration(i*20) * time.Minute)
		imp := 0.05
		exp := 0.04
		solar := 0.0
		if i >= 3 {
			imp = 0.40
			exp = 0.35
			solar = 2.0
		}
		baseTimeline[i] = planInterval{
			startTime:     ti,
			endTime:       ti.Add(20 * time.Minute),
			durationHours: 1.0 / 3.0,
			importRate:    imp,
			exportRate:    exp,
			solarKWH:      solar,
			loadKWH:       0.3,
			minSOC:        20.0,
		}
	}

	t.Run("Charge_AllowedWhenArbitrageAhead", func(t *testing.T) {
		t.Parallel()
		state := planState{soc: 25.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.True(t, allowed, "Charging in cheap block should be allowed when expensive block is ahead")

		// Verify arbitrageGatePerKWH is compared against the raw (non-RTE-adjusted) rate difference:
		// Block 0 import = $0.20, Block 1 import = $0.25, MinArbitrageDifference = $0.05 (raw spread = $0.05 >= $0.05).
		arbSettings := settings
		arbSettings.MinArbitrageDifferenceDollarsPerKWH = 0.05
		b0 := timelineBlock{startIdx: 0, endIdx: 2, minImportRate: 0.20, avgImportRate: 0.20, maxImportRate: 0.20, minSOC: 20.0, durationHours: 1.0, isLowestPrice: true}
		b1 := timelineBlock{startIdx: 3, endIdx: 5, minImportRate: 0.25, avgImportRate: 0.25, maxImportRate: 0.25, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true}
		assert.True(t, isBlockActionAllowed(0, b0, blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, []timelineBlock{b0, b1}, state, nil, 0.20, arbSettings, types.SystemStatus{}, baseTimeline, nil),
			"Arbitrage gate must compare raw rate spread ($0.25 - $0.20 = $0.05 >= $0.05) without dividing import rate by RTE")
	})

	t.Run("Charge_DisallowedWhenGridChargeDisabled", func(t *testing.T) {
		t.Parallel()
		noGridCharge := settings
		noGridCharge.GridChargeBatteries = false
		state := planState{soc: 25.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, blocks, state, vppEvents, postHorizonRate, noGridCharge, types.SystemStatus{}, baseTimeline, nil)
		assert.False(t, allowed, "Charging must be disallowed when GridChargeBatteries is false")
	})

	t.Run("Charge_DisallowedInHighestPriceBlock", func(t *testing.T) {
		t.Parallel()
		state := planState{soc: 25.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(1, blockExpensive, blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.False(t, allowed, "Charging must be disallowed in the highest price block")
	})

	t.Run("Export_AllowedWhenProfitableAndAboveMinSOC", func(t *testing.T) {
		t.Parallel()
		state := planState{soc: 80.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(1, blockExpensive, blockActionChoice{actionType: blockActionExport, targetSOC: 20.0}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.True(t, allowed, "Export should be allowed in expensive block with high SOC")
	})

	t.Run("Export_DisallowedInLowestPriceBlock", func(t *testing.T) {
		t.Parallel()
		state := planState{soc: 80.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionExport, targetSOC: 20.0}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.False(t, allowed, "Export must be disallowed in lowest price block")
	})

	t.Run("Export_DisallowedWhenSOCAtMinSOC", func(t *testing.T) {
		t.Parallel()
		state := planState{soc: 20.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(1, blockExpensive, blockActionChoice{actionType: blockActionExport, targetSOC: 20.0}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.False(t, allowed, "Export must be disallowed when starting SOC is at or below minSOC")
	})

	t.Run("DirectSolarExport_AllowedWithAdequateSolarAndSOC", func(t *testing.T) {
		t.Parallel()
		state := planState{soc: 50.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(1, blockExpensive, blockActionChoice{actionType: blockActionDirectSolarExport}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.True(t, allowed, "DirectSolarExport should be allowed when solar is present and SOC above reserve margin")
	})

	t.Run("DirectSolarExport_DisallowedWithInsufficientSolar", func(t *testing.T) {
		t.Parallel()
		noSolarBlock := blockExpensive
		noSolarBlock.netSolarKWH = 0.0
		noSolarTimeline := make([]planInterval, len(baseTimeline))
		copy(noSolarTimeline, baseTimeline)
		for i := range noSolarTimeline {
			noSolarTimeline[i].solarKWH = 0.0
		}
		state := planState{soc: 50.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(1, noSolarBlock, blockActionChoice{actionType: blockActionDirectSolarExport}, blocks, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, noSolarTimeline, nil)
		assert.False(t, allowed, "DirectSolarExport must be disallowed when there is no solar")
	})

	t.Run("Charge_DisallowedDuringActiveVPPEvent", func(t *testing.T) {
		t.Parallel()
		activeVPP := []vppAnchor{
			{eventStart: now, eventEnd: now.Add(2 * time.Hour), mandatory: true},
		}
		state := planState{soc: 25.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, blocks, state, activeVPP, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.False(t, allowed, "Grid charging must be disallowed during active VPP event window")
	})

	t.Run("Charge_AllowedForReserveDeficit", func(t *testing.T) {
		t.Parallel()
		stateInDeficit := planState{soc: 10.0, capacityKWH: 13.5, time: now}
		// In a high-price block without arbitrage, charging to minSOC is allowed to recover from reserve deficit
		allowed := isBlockActionAllowed(1, blockExpensive, blockActionChoice{actionType: blockActionCharge, targetSOC: 20.0}, blocks, stateInDeficit, vppEvents, postHorizonRate, settings, types.SystemStatus{}, baseTimeline, nil)
		assert.True(t, allowed, "Charging to minSOC must be allowed to recover from reserve deficit even in expensive block")
	})

	t.Run("Charge_AllowedForNegativePrice", func(t *testing.T) {
		t.Parallel()
		negBlock := timelineBlock{
			startIdx:      0,
			endIdx:        1,
			minImportRate: -0.02,
			maxImportRate: -0.01,
			minSOC:        20.0,
		}
		timelineNeg := []planInterval{
			{startTime: now, endTime: now.Add(30 * time.Minute), importRate: -0.02, minSOC: 20.0},
			{startTime: now.Add(30 * time.Minute), endTime: now.Add(time.Hour), importRate: -0.01, minSOC: 20.0},
		}
		state := planState{soc: 50.0, capacityKWH: 13.5, time: now}
		allowed := isBlockActionAllowed(0, negBlock, blockActionChoice{actionType: blockActionCharge, targetSOC: 100}, []timelineBlock{negBlock}, state, vppEvents, postHorizonRate, settings, types.SystemStatus{}, timelineNeg, nil)
		assert.True(t, allowed, "Charging must be allowed when price is negative")
	})

	t.Run("Standby_AlmostFullAllowedBeforePeakAndForProfitableSolarOverflow", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		c := NewController()
		state98 := planState{
			soc:            98.0,
			energyKWH:      13.5 * 0.98,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
			time:           now,
		}

		// Case 1: Almost-full battery (98% SOC) 1 hour before a heavy Peak block that needs the full battery.
		// Standby must be allowed (and chosen) in Block 0 to preserve the battery for the upcoming peak.
		timelinePrePeak := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.05, loadKWH: 2.0, solarKWH: 0.0, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 3.0, importRate: 0.40, exportRate: 0.05, loadKWH: 10.5, solarKWH: 0.0, minSOC: 20.0},
		}
		blocksPrePeak := buildTimelineBlocks(ctx, timelinePrePeak, nil, settings, types.SystemStatus{})
		require.Len(t, blocksPrePeak, 2)
		allowedPrePeak := isBlockActionAllowed(0, blocksPrePeak[0], blockActionChoice{actionType: blockActionStandby}, blocksPrePeak, state98, nil, 0.10, settings, types.SystemStatus{}, timelinePrePeak, nil)
		assert.True(t, allowedPrePeak, "Standby must be allowed at 98% SOC when an upcoming unrefilled peak block needs the battery")

		pathPrePeak := c.searchOptimalPlanBlocks(ctx, timelinePrePeak, state98, nil, postHorizonValuation{knownPostHorizonRate: 0.10}, settings, types.SystemStatus{}, nil)
		require.NotEmpty(t, pathPrePeak.actions)
		assert.Equal(t, types.BatteryModeStandby, pathPrePeak.actions[0].batteryMode, "Planner must hold 98% battery in Standby during $0.10/kWh hour to cover the upcoming $0.40/kWh peak")

		// Case 2: Almost-full battery (98% SOC) with ManageTOUSchedules=false before a sunny block whose export rate
		// ($0.28/kWh) exceeds the current import rate ($0.08/kWh). Standby must be allowed (and chosen) so the
		// battery stays near 100% SOC and excess solar overflows to the grid at $0.28/kWh.
		noTOUSettings := settings
		noTOUSettings.ManageTOUSchedules = false
		timelineSolarOverflow := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.08, exportRate: 0.08, loadKWH: 1.5, solarKWH: 0.0, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 3.0, importRate: 0.15, exportRate: 0.28, loadKWH: 1.5, solarKWH: 12.0, minSOC: 20.0},
		}
		blocksSolarOverflow := buildTimelineBlocks(ctx, timelineSolarOverflow, nil, noTOUSettings, types.SystemStatus{})
		require.Len(t, blocksSolarOverflow, 2)
		allowedOverflow := isBlockActionAllowed(0, blocksSolarOverflow[0], blockActionChoice{actionType: blockActionStandby}, blocksSolarOverflow, state98, nil, 0.08, noTOUSettings, types.SystemStatus{}, timelineSolarOverflow, nil)
		assert.True(t, allowedOverflow, "Standby must be allowed at 98% SOC when ManageTOUSchedules is false and upcoming solar overflow exports at a higher rate")

		pathOverflow := c.searchOptimalPlanBlocks(ctx, timelineSolarOverflow, state98, nil, postHorizonValuation{knownPostHorizonRate: 0.08}, noTOUSettings, types.SystemStatus{}, nil)
		require.NotEmpty(t, pathOverflow.actions)
		assert.Equal(t, types.BatteryModeStandby, pathOverflow.actions[0].batteryMode, "Planner must hold 98% battery in Standby at $0.08/kWh so upcoming solar overflows at $0.28/kWh")
	})

	t.Run("Export_DisallowedOn3TierTOUOffPeakAndRequiresValidSolarPrefix", func(t *testing.T) {
		t.Parallel()
		// 3-tier TOU tariff:
		// - Super Off-Peak (Block 0): import $0.055, export $0.024 (isLowestPrice = true)
		// - Off-Peak (Block 1): import $0.105, export $0.024 (isLowestPrice = false, but export $0.024 < minHorizonImport $0.055 and is at the lowest export tier)
		// - On-Peak (Block 2): import $0.314, export $0.094 (isHighestPrice = true, dark evening block with 0 solar)
		threeTierBlocks := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.055, avgImportRate: 0.055, maxImportRate: 0.055, minExportRate: 0.024, maxExportRate: 0.024, minSOC: 20.0, durationHours: 1.0, isLowestPrice: true},
			{startIdx: 1, endIdx: 1, minImportRate: 0.105, avgImportRate: 0.105, maxImportRate: 0.105, minExportRate: 0.024, maxExportRate: 0.024, minSOC: 20.0, durationHours: 1.0, netSolarKWH: 3.0},
			{startIdx: 2, endIdx: 2, minImportRate: 0.314, avgImportRate: 0.314, maxImportRate: 0.314, minExportRate: 0.094, maxExportRate: 0.094, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true},
		}
		state80 := planState{soc: 80.0, capacityKWH: 13.5, time: now}

		assert.False(t, isBlockActionAllowed(1, threeTierBlocks[1], blockActionChoice{actionType: blockActionExport, targetSOC: 20.0}, threeTierBlocks, state80, nil, 0.055, settings, types.SystemStatus{}, baseTimeline, nil),
			"Battery export must be disallowed in Off-Peak when export rate ($0.024) is below minHorizonImportRate ($0.055) and at the lowest export tier")
		assert.False(t, isBlockActionAllowed(1, threeTierBlocks[1], blockActionChoice{actionType: blockActionDirectSolarExport, targetSOC: 20.0}, threeTierBlocks, state80, nil, 0.055, settings, types.SystemStatus{}, baseTimeline, nil),
			"Direct solar export must be disallowed in Off-Peak when export rate ($0.024) is below minHorizonImportRate ($0.055) and at the lowest export tier")

		// In On-Peak (Block 2, dark with 0 solar), normal battery export is allowed, but useSolarExportPrefix: true is disallowed because DirectSolarExport is disallowed without solar.
		assert.True(t, isBlockActionAllowed(2, threeTierBlocks[2], blockActionChoice{actionType: blockActionExport, targetSOC: 20.0, useSolarExportPrefix: false}, threeTierBlocks, state80, nil, 0.055, settings, types.SystemStatus{}, baseTimeline, nil))
		assert.False(t, isBlockActionAllowed(2, threeTierBlocks[2], blockActionChoice{actionType: blockActionExport, targetSOC: 20.0, useSolarExportPrefix: true}, threeTierBlocks, state80, nil, 0.055, settings, types.SystemStatus{}, baseTimeline, nil),
			"Hybrid DirectSolarExport->Export prefix must be disallowed when DirectSolarExport itself is disallowed in the block")
	})

	t.Run("Charge_DisallowedWhenAlreadyAlmostFullInLaterBlockOrTargetBelowMinSOC", func(t *testing.T) {
		t.Parallel()
		state96 := planState{soc: 96.0, capacityKWH: 13.5, time: now}
		state98 := planState{soc: 98.0, capacityKWH: 13.5, time: now}
		// At 96% SOC (< 98% almostFullSOC), charging is allowed.
		assert.True(t, isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, []timelineBlock{blockCheap, blockExpensive}, state96, nil, 0.40, settings, types.SystemStatus{}, baseTimeline, nil),
			"Grid charging must be allowed at 96%% SOC (< 98%% almostFullSOC)")
		// At 98% SOC (>= 98% almostFullSOC), starting a new charge is disallowed, but continuing an active charge at block 0 is allowed up to 99%.
		assert.False(t, isBlockActionAllowed(1, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, []timelineBlock{blockCheap, blockCheap, blockExpensive}, state98, nil, 0.40, settings, types.SystemStatus{}, baseTimeline, nil),
			"Grid charging must be disallowed in future block when battery already enters at >= 98%% SOC")
		assert.False(t, isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, []timelineBlock{blockCheap, blockExpensive}, state98, nil, 0.40, settings, types.SystemStatus{}, baseTimeline, nil),
			"Starting a new grid charge at block 0 must be disallowed at >= 98%% SOC")
		assert.True(t, isBlockActionAllowed(0, blockCheap, blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, []timelineBlock{blockCheap, blockExpensive}, state98, nil, 0.40, settings, types.SystemStatus{}, baseTimeline, &types.Action{BatteryMode: types.BatteryModeChargeAny}),
			"Continuing an active grid charge at block 0 must be allowed at 98%% SOC (up to 99%%)")

		// When block.minSOC is 80%, a candidate charge with targetSOC = 50% (< minSOC) must be disallowed (Bug D).
		highReserveBlock := blockCheap
		highReserveBlock.minSOC = 80.0
		state20 := planState{soc: 20.0, capacityKWH: 13.5, time: now}
		assert.False(t, isBlockActionAllowed(0, highReserveBlock, blockActionChoice{actionType: blockActionCharge, targetSOC: 50.0}, []timelineBlock{highReserveBlock, blockExpensive}, state20, nil, 0.40, settings, types.SystemStatus{}, baseTimeline, nil),
			"Candidate charge targetSOC (50%%) below block.minSOC (80%%) must be disallowed")
		assert.True(t, isBlockActionAllowed(0, highReserveBlock, blockActionChoice{actionType: blockActionCharge, targetSOC: 80.0}, []timelineBlock{highReserveBlock, blockExpensive}, state20, nil, 0.40, settings, types.SystemStatus{}, baseTimeline, nil),
			"Candidate charge targetSOC == block.minSOC (80%%) must be allowed when in reserve deficit")
	})
}

func TestValidateCandidateTrajectory(t *testing.T) {
	t.Parallel()

	settings := types.Settings{
		GridChargeBatteries: true,
		GridExportBatteries: true,
		GridExportSolar:     true,
		ManageTOUSchedules:  true,
		OptimizationProfile: "balanced",
	}
	wear := wearRates{exportGatePerKWH: 0.05, arbitrageGatePerKWH: 0.05}
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("Rejects_HoldThenCheaperExport", func(t *testing.T) {
		t.Parallel()
		// Interval 0: expensive import rate ($0.40), action Standby with home load importing from grid
		// Interval 1: cheaper export rate ($0.20), action Export
		timeline := []planInterval{
			{startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.40, exportRate: 0.35, loadKWH: 1.0, minSOC: 20.0},
			{startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.25, exportRate: 0.20, loadKWH: 0.2, minSOC: 20.0},
		}
		actions := []actionCandidate{
			{batteryMode: types.BatteryModeStandby},
			{batteryMode: types.BatteryModeExport, targetSOC: 20.0},
		}
		state := planState{soc: 80.0, energyKWH: 13.5 * 0.80, capacityKWH: 13.5, time: now}
		valid, reason := validateCandidateTrajectory(actions, timeline, settings, wear, 0.90, state, 0)
		assert.False(t, valid, "Should reject HoldThenCheaperExport")
		assert.Equal(t, "HoldThenCheaperExport", reason)
	})

	t.Run("Rejects_ExportThenCostlierHold", func(t *testing.T) {
		t.Parallel()
		// Interval 0: export at $0.20
		// Interval 1: standby hold at high import rate ($0.45)
		timeline := []planInterval{
			{startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.25, exportRate: 0.20, loadKWH: 0.2, minSOC: 20.0},
			{startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.45, exportRate: 0.40, loadKWH: 1.0, minSOC: 20.0},
		}
		actions := []actionCandidate{
			{batteryMode: types.BatteryModeExport, targetSOC: 20.0},
			{batteryMode: types.BatteryModeStandby},
		}
		state := planState{soc: 80.0, energyKWH: 13.5 * 0.80, capacityKWH: 13.5, time: now}
		valid, reason := validateCandidateTrajectory(actions, timeline, settings, wear, 0.90, state, 0)
		assert.False(t, valid, "Should reject ExportThenCostlierHold")
		assert.Equal(t, "ExportThenCostlierHold", reason)
	})

	t.Run("Rejects_UnprofitableGridChargeToExport", func(t *testing.T) {
		t.Parallel()
		// Grid charge at $0.20, export at $0.21. With wear.exportGatePerKWH = 0.05, raw spread ($0.01) is below gate!
		timeline := []planInterval{
			{startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.20, exportRate: 0.15, loadKWH: 0.1, minSOC: 20.0},
			{startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.25, exportRate: 0.21, loadKWH: 0.1, minSOC: 20.0},
		}
		actions := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, targetSOC: 100.0},
			{batteryMode: types.BatteryModeExport, targetSOC: 20.0},
		}
		state := planState{soc: 25.0, energyKWH: 13.5 * 0.25, capacityKWH: 13.5, time: now}
		valid, reason := validateCandidateTrajectory(actions, timeline, settings, wear, 0.90, state, 0)
		assert.False(t, valid, "Should reject UnprofitableGridChargeToExport")
		assert.Equal(t, "UnprofitableGridChargeToExport", reason)

		// Grid charge at $0.40, export at $0.46 with exportGatePerKWH = $0.05:
		// Raw spread is $0.06 >= $0.05 (even though $0.46 - $0.40/0.90 = $0.0156 < $0.05 if RTE were applied to the gate).
		timelineRawSpread := []planInterval{
			{startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.40, exportRate: 0.15, loadKWH: 0.1, minSOC: 20.0},
			{startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.50, exportRate: 0.46, loadKWH: 0.1, minSOC: 20.0},
		}
		validRaw, reasonRaw := validateCandidateTrajectory(actions, timelineRawSpread, settings, wear, 0.90, state, 0)
		assert.True(t, validRaw, "Export gate must compare raw rate spread ($0.46 - $0.40 = $0.06 >= $0.05) without dividing charge rate by RTE: "+reasonRaw)
	})

	t.Run("Accepts_ProfitableArbitrageTrajectory", func(t *testing.T) {
		t.Parallel()
		// Grid charge at $0.05, export at $0.40. Margin is huge.
		timeline := []planInterval{
			{startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.05, exportRate: 0.04, loadKWH: 0.1, minSOC: 20.0},
			{startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 1.0 / 3.0, importRate: 0.45, exportRate: 0.40, loadKWH: 0.1, minSOC: 20.0},
		}
		actions := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, targetSOC: 100.0},
			{batteryMode: types.BatteryModeExport, targetSOC: 20.0},
		}
		state := planState{soc: 25.0, energyKWH: 13.5 * 0.25, capacityKWH: 13.5, time: now}
		valid, reason := validateCandidateTrajectory(actions, timeline, settings, wear, 0.90, state, 0)
		assert.True(t, valid, "Should accept highly profitable arbitrage trajectory: "+reason)
	})

	t.Run("HoldThenCheaperExport_And_ExportThenCostlierHold_EnforceExportGateWithoutSummingWear", func(t *testing.T) {
		t.Parallel()
		// User configured MinBatteryExportDifference = $0.07/kWh with BatteryExportDegradation = $0.05/kWh.
		// getWearRates must set exportGatePerKWH = max($0.05, $0.07) = $0.07/kWh (without summing them to $0.12/kWh).
		exportGateSettings := settings
		exportGateSettings.MinBatteryExportDifferenceDollarsPerKWH = 0.07
		wearGate := getWearRates(exportGateSettings, exportGateSettings.GetOptimizationParams())
		assert.InDelta(t, 0.07, wearGate.exportGatePerKWH, 1e-6)

		state80 := planState{soc: 80.0, energyKWH: 13.5 * 0.80, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}

		// 1. Spread $0.0438/kWh ($0.5038 export - $0.4600 import) is below $0.07/kWh exportGatePerKWH -> rejected.
		timelineBelowGate := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.4600, exportRate: 0.4600, loadKWH: 1.5, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.5900, exportRate: 0.5038, loadKWH: 1.0, minSOC: 20.0},
		}
		actionsHoldExport := []actionCandidate{
			{batteryMode: types.BatteryModeStandby},
			{batteryMode: types.BatteryModeExport, targetSOC: 20.0},
		}
		validBelow, reasonBelow := validateCandidateTrajectory(actionsHoldExport, timelineBelowGate, exportGateSettings, wearGate, 0.90, state80, 0)
		assert.False(t, validBelow)
		assert.Equal(t, "HoldThenCheaperExport", reasonBelow)

		// 2. Spread $0.0800/kWh ($0.6700 export - $0.5900 import) clears $0.07/kWh exportGatePerKWH -> accepted.
		timelineAboveGate := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.5900, exportRate: 0.5000, loadKWH: 1.5, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.5900, exportRate: 0.6700, loadKWH: 1.0, minSOC: 20.0},
		}
		validAbove, reasonAbove := validateCandidateTrajectory(actionsHoldExport, timelineAboveGate, exportGateSettings, wearGate, 0.90, state80, 0)
		assert.True(t, validAbove, "Spread of $0.08/kWh must clear $0.07/kWh export gate without adding degradation on top: "+reasonAbove)
	})

	t.Run("EnforcesReserveDeficitRecovery_AndAllowsContinuousChargeWhenMinSOCUnreachable", func(t *testing.T) {
		t.Parallel()
		state20 := planState{soc: 20.0, energyKWH: 13.5 * 0.20, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}

		// 1. Stopping ChargeAny at ~53% SOC inside an 80% minSOC window must be rejected as UnrecoveredReserveDeficit.
		timeline80 := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 80.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 80.0},
		}
		actionsStopEarly := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, targetSOC: 50.0},
			{batteryMode: types.BatteryModeLoad},
		}
		validStop, reasonStop := validateCandidateTrajectory(actionsStopEarly, timeline80, settings, wear, 0.90, state20, 0)
		assert.False(t, validStop)
		assert.Equal(t, "UnrecoveredReserveDeficit", reasonStop)

		// 2. Failing to charge during a future minSOC step-up (20% -> 50% in hour 1) must be rejected as UnrecoveredReserveDeficit.
		timelineFutureStepUp := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 50.0},
		}
		actionsIgnoreFuture := []actionCandidate{
			{batteryMode: types.BatteryModeLoad},
			{batteryMode: types.BatteryModeLoad},
		}
		validFuture, reasonFuture := validateCandidateTrajectory(actionsIgnoreFuture, timelineFutureStepUp, settings, wear, 0.90, state20, 0)
		assert.False(t, validFuture)
		assert.Equal(t, "UnrecoveredReserveDeficit", reasonFuture)

		// 3. Impossible-to-reach minSOC (minSOC = 100% in a single 1-hour window starting at 20% SOC, where 5 kW
		// charging on a 13.5 kWh battery only reaches ~55% SOC): continuous ChargeAny must be allowed!
		timelineUnreachable := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 100.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 20.0},
		}
		actionsContinuousCharge := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, targetSOC: 100.0},
			{batteryMode: types.BatteryModeLoad},
		}
		validUnreach, reasonUnreach := validateCandidateTrajectory(actionsContinuousCharge, timelineUnreachable, settings, wear, 0.90, state20, 0)
		assert.True(t, validUnreach, "Continuous ChargeAny during an unreachable minSOC window must be allowed: "+reasonUnreach)
	})
}

func TestEstimatePairedBlockStartState(t *testing.T) {
	t.Parallel()

	initialState := planState{
		soc:            20.0,
		energyKWH:      20.0 * 0.20,
		capacityKWH:    20.0,
		maxChargeKW:    5.0,
		maxDischargeKW: 5.0,
	}
	roundTripEff := 1.0 // Use 1.0 so one-way efficiency is 1.0 for exact arithmetic checks.

	t.Run("ChargeAtA_NonChargeAtB_RaisesStateBToTargetSOC", func(t *testing.T) {
		t.Parallel()
		blocks := []timelineBlock{
			{startIdx: 0, endIdx: 0, durationHours: 2.0, minSOC: 20.0},
			{startIdx: 1, endIdx: 1, durationHours: 2.0, minSOC: 20.0},
		}
		currentChoices := []blockActionChoice{
			{actionType: blockActionLoad},
			{actionType: blockActionLoad},
		}
		currentStates := []planState{
			{soc: 20.0},
			{soc: 20.0},
			{soc: 20.0},
		}
		pt := [2]blockActionChoice{
			{actionType: blockActionCharge, targetSOC: 100.0},
			{actionType: blockActionExport, targetSOC: 20.0},
		}
		stateB := estimatePairedBlockStartState(0, 1, pt, blocks, currentChoices, currentStates, initialState, roundTripEff)
		assert.InDelta(t, 100.0, stateB.soc, 1e-6, "Charge at a paired with Export at b should raise stateB.soc to pt[0].targetSOC")
	})

	t.Run("ChargeAtA_ChargeAtB_RespectsPowerLimitAndInterveningLoadDrain", func(t *testing.T) {
		t.Parallel()
		// 20 kWh battery with 5 kW maxChargeKW:
		// - Block 0 (1h): can gain at most 5 kWh = +25% SOC (from 20% to 45% SOC), even though targetSOC is 100%.
		// - Block 1 (intervening Load block): netSolarKWH = -2.0 kWh (-10% SOC drain, reducing 45% to 35% SOC).
		// - Block 2 (second Charge block): enters at 35% SOC.
		blocks := []timelineBlock{
			{startIdx: 0, endIdx: 0, durationHours: 1.0, minSOC: 20.0},
			{startIdx: 1, endIdx: 1, durationHours: 1.0, minSOC: 20.0, netSolarKWH: -2.0},
			{startIdx: 2, endIdx: 2, durationHours: 1.0, minSOC: 20.0},
		}
		currentChoices := []blockActionChoice{
			{actionType: blockActionLoad},
			{actionType: blockActionLoad},
			{actionType: blockActionLoad},
		}
		currentStates := []planState{
			{soc: 20.0},
			{soc: 20.0},
			{soc: 20.0},
			{soc: 20.0},
		}
		pt := [2]blockActionChoice{
			{actionType: blockActionCharge, targetSOC: 100.0},
			{actionType: blockActionCharge, targetSOC: 75.0},
		}
		stateB := estimatePairedBlockStartState(0, 2, pt, blocks, currentChoices, currentStates, initialState, roundTripEff)
		assert.InDelta(t, 35.0, stateB.soc, 1e-6, "Second Charge block should see power-limited gain (+25%) minus intervening Load drain (-10%)")
	})

	t.Run("StandbyAtA_PreservesStateASOCIntoBlockB", func(t *testing.T) {
		t.Parallel()
		blocks := []timelineBlock{
			{startIdx: 0, endIdx: 0, durationHours: 1.0, minSOC: 20.0},
			{startIdx: 1, endIdx: 1, durationHours: 1.0, minSOC: 20.0},
		}
		currentChoices := []blockActionChoice{
			{actionType: blockActionLoad},
			{actionType: blockActionLoad},
		}
		currentStates := []planState{
			{soc: 65.0},
			{soc: 20.0},
			{soc: 20.0},
		}
		pt := [2]blockActionChoice{
			{actionType: blockActionStandby},
			{actionType: blockActionExport, targetSOC: 20.0},
		}
		stateB := estimatePairedBlockStartState(0, 1, pt, blocks, currentChoices, currentStates, initialState, roundTripEff)
		assert.InDelta(t, 65.0, stateB.soc, 1e-6, "Standby at a should preserve stateA.soc (65%) into stateB")
	})

	t.Run("LoadAtA_DrainsStateAToBlockB", func(t *testing.T) {
		t.Parallel()
		// Current trajectory had Standby at Block 0 (keeping SOC at 60%).
		// Testing Load at Block 0 (with -4.0 kWh net load = -20% SOC on 20 kWh battery) reduces stateB.soc from 60% to 40%.
		blocks := []timelineBlock{
			{startIdx: 0, endIdx: 0, durationHours: 1.0, minSOC: 20.0, netSolarKWH: -4.0},
			{startIdx: 1, endIdx: 1, durationHours: 1.0, minSOC: 20.0},
		}
		currentChoices := []blockActionChoice{
			{actionType: blockActionStandby},
			{actionType: blockActionLoad},
		}
		currentStates := []planState{
			{soc: 60.0},
			{soc: 60.0},
			{soc: 40.0},
		}
		pt := [2]blockActionChoice{
			{actionType: blockActionLoad},
			{actionType: blockActionStandby},
		}
		stateB := estimatePairedBlockStartState(0, 1, pt, blocks, currentChoices, currentStates, initialState, roundTripEff)
		assert.InDelta(t, 40.0, stateB.soc, 1e-6, "Switching Block 0 from Standby to Load should drain stateB.soc by 20% (to 40%)")
	})

	t.Run("ExportAtA_LowersStateBSOCToExportFloor", func(t *testing.T) {
		t.Parallel()
		blocks := []timelineBlock{
			{startIdx: 0, endIdx: 0, durationHours: 1.0, minSOC: 20.0},
			{startIdx: 1, endIdx: 1, durationHours: 1.0, minSOC: 50.0},
		}
		currentChoices := []blockActionChoice{
			{actionType: blockActionLoad},
			{actionType: blockActionLoad},
		}
		currentStates := []planState{
			{soc: 80.0},
			{soc: 70.0},
			{soc: 60.0},
		}
		pt := [2]blockActionChoice{
			{actionType: blockActionExport, targetSOC: 20.0},
			{actionType: blockActionCharge, targetSOC: 50.0},
		}
		stateB := estimatePairedBlockStartState(0, 1, pt, blocks, currentChoices, currentStates, initialState, roundTripEff)
		assert.InDelta(t, 20.0, stateB.soc, 1e-6, "Exporting down to 20% at Block 0 should lower stateB.soc to 20% so reserve recovery Charge at Block 1 is allowed")
	})
}

func TestCritiqueBlockPlan(t *testing.T) {
	t.Parallel()

	settings := types.Settings{
		GridChargeBatteries: true,
		GridExportBatteries: true,
		GridExportSolar:     true,
		ManageTOUSchedules:  true,
		OptimizationProfile: "balanced",
	}
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	// 4 intervals of 30 min (2 hours total). Block 0: cheap ($0.05), Block 1: expensive ($0.50)
	timeline := make([]planInterval, 4)
	for i := 0; i < 4; i++ {
		ti := now.Add(time.Duration(i*30) * time.Minute)
		rate := 0.05
		exp := 0.04
		if i >= 2 {
			rate = 0.50
			exp = 0.45
		}
		timeline[i] = planInterval{
			startTime:     ti,
			endTime:       ti.Add(30 * time.Minute),
			durationHours: 0.5,
			importRate:    rate,
			exportRate:    exp,
			solarKWH:      0.0,
			loadKWH:       0.5,
			minSOC:        20.0,
		}
	}
	blocks := []timelineBlock{
		{startIdx: 0, endIdx: 1, minImportRate: 0.05, avgImportRate: 0.05, maxImportRate: 0.05, minExportRate: 0.04, maxExportRate: 0.04, minSOC: 20.0, durationHours: 1.0, isLowestPrice: true},
		{startIdx: 2, endIdx: 3, minImportRate: 0.50, avgImportRate: 0.50, maxImportRate: 0.50, minExportRate: 0.45, maxExportRate: 0.45, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true},
	}
	state := planState{soc: 20.0, energyKWH: 13.5 * 0.20, capacityKWH: 13.5, time: now}

	ctx := context.Background()
	winningChoices, score := critiqueBlockPlan(ctx, blocks, timeline, state, nil, postHorizonValuation{}, settings, types.SystemStatus{}, nil, 0)
	require.Len(t, winningChoices, 2)

	assert.Equal(t, blockActionCharge, winningChoices[0].actionType, "Critique should switch block 0 to Charge")
	assert.True(t, winningChoices[1].actionType == blockActionExport || winningChoices[1].actionType == blockActionLoad, "Critique should choose Export or Load for block 1")
	assert.Less(t, score, 2.0, "Score should reflect substantial savings from arbitrage")

	t.Run("HoldsInterveningOffPeakBlockAfterChargeAndFindsMidExportFloor", func(t *testing.T) {
		t.Parallel()

		// 3-block scenario starting at 20% SOC (minSOC = 20%):
		// - Block 0 (Super Off-Peak): $0.05/kWh import, 3h, 0.4 kWh load (charges to 100%).
		// - Block 1 (Intervening Off-Peak): $0.07/kWh import, 2h, 3.0 kWh load (must be held in Standby so charged energy is not drained before Peak).
		// - Block 2 (On-Peak): $0.50/kWh import, $0.45/kWh export, 2h, 1.0 kWh load (must export/cover peak using the energy charged in Block 0).
		timeline3 := []planInterval{
			{startTime: now, endTime: now.Add(3 * time.Hour), durationHours: 3.0, importRate: 0.05, exportRate: 0.02, loadKWH: 0.4, minSOC: 20.0},
			{startTime: now.Add(3 * time.Hour), endTime: now.Add(5 * time.Hour), durationHours: 2.0, importRate: 0.07, exportRate: 0.02, loadKWH: 3.0, minSOC: 20.0},
			{startTime: now.Add(5 * time.Hour), endTime: now.Add(7 * time.Hour), durationHours: 2.0, importRate: 0.50, exportRate: 0.45, loadKWH: 1.0, minSOC: 20.0},
		}
		blocks3 := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.05, avgImportRate: 0.05, maxImportRate: 0.05, minExportRate: 0.02, maxExportRate: 0.02, minSOC: 20.0, durationHours: 3.0, isLowestPrice: true},
			{startIdx: 1, endIdx: 1, minImportRate: 0.07, avgImportRate: 0.07, maxImportRate: 0.07, minExportRate: 0.02, maxExportRate: 0.02, minSOC: 20.0, durationHours: 2.0},
			{startIdx: 2, endIdx: 2, minImportRate: 0.50, avgImportRate: 0.50, maxImportRate: 0.50, minExportRate: 0.45, maxExportRate: 0.45, minSOC: 20.0, durationHours: 2.0, isHighestPrice: true},
		}
		state20 := planState{soc: 20.0, energyKWH: 13.5 * 0.20, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}

		win3, _ := critiqueBlockPlan(ctx, blocks3, timeline3, state20, nil, postHorizonValuation{knownPostHorizonRate: 0.05}, settings, types.SystemStatus{}, nil, 0)
		require.Len(t, win3, 3)
		assert.Equal(t, blockActionCharge, win3[0].actionType, "Block 0 must charge during Super Off-Peak")
		assert.Equal(t, blockActionStandby, win3[1].actionType, "Intervening Off-Peak Block 1 must be held in Standby to preserve charged energy for On-Peak")
		assert.Equal(t, blockActionExport, win3[2].actionType, "Block 2 must be allowed to Export because simulated block startState (after Block 0 charge) is > minSOC even though initial.soc == minSOC")
	})

	t.Run("RecoversMultiBlockReserveDeficitAndAllowsUnreachableMinSOC", func(t *testing.T) {
		t.Parallel()
		c := NewController()

		// 1. Multi-block 80% reserve deficit on a 27 kWh battery (5 kW maxChargeKW = ~17.5% SOC/hr, requiring >3 hours
		// across 3 distinct dynamic-rate blocks to recover from 20% to 80% SOC).
		timeline80 := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.08, exportRate: 0.03, loadKWH: 0.5, minSOC: 80.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.14, exportRate: 0.03, loadKWH: 0.5, minSOC: 80.0},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 2.0, importRate: 0.09, exportRate: 0.03, loadKWH: 1.0, minSOC: 80.0},
		}
		state27kWh := planState{soc: 20.0, energyKWH: 27.0 * 0.20, capacityKWH: 27.0, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}
		path80 := c.searchOptimalPlanBlocks(ctx, timeline80, state27kWh, nil, postHorizonValuation{knownPostHorizonRate: 0.10}, settings, types.SystemStatus{}, nil)
		require.NotNil(t, path80)
		require.Len(t, path80.states, 4)
		assert.GreaterOrEqual(t, path80.states[3].soc, 80.0-reserveDeficitChargeTolerancePct,
			"Planner must charge across all 3 blocks until the 80%% reserve floor is reached")

		// 2. Impossible-to-reach 100% minSOC in a 1-hour window starting at 20% SOC:
		// Solver must stay in continuous ChargeAny during the 1-hour window and not reject the plan.
		timelineImpossible := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.12, exportRate: 0.04, loadKWH: 0.5, minSOC: 100.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.12, exportRate: 0.04, loadKWH: 0.5, minSOC: 20.0},
		}
		pathImp := c.searchOptimalPlanBlocks(ctx, timelineImpossible, state27kWh, nil, postHorizonValuation{knownPostHorizonRate: 0.12}, settings, types.SystemStatus{}, nil)
		require.NotNil(t, pathImp)
		require.Len(t, pathImp.actions, 2)
		assert.Equal(t, types.BatteryModeChargeAny, pathImp.actions[0].batteryMode,
			"Planner must stay in continuous ChargeAny when minSOC (100%%) cannot be physically reached within the 1-hour window")
	})

	t.Run("HoldsAcrossMultipleCheapOvernightBlocksBeforeMorningPeak", func(t *testing.T) {
		t.Parallel()
		// Two cheap overnight blocks ($0.045/kWh and $0.052/kWh, separated by export rate difference)
		// followed by a $0.12/kWh morning peak, with GridChargeBatteries = false and starting SOC = 45% (minSOC = 20%).
		// Holding in Standby across BOTH cheap blocks is required so the battery does not drain at $0.045-$0.052/kWh
		// before the $0.12/kWh morning peak arrives.
		noChargeSettings := settings
		noChargeSettings.GridChargeBatteries = false
		timelineOvernight := []planInterval{
			{startTime: now, endTime: now.Add(2 * time.Hour), durationHours: 2.0, importRate: 0.045, exportRate: 0.02, loadKWH: 3.5, minSOC: 20.0},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 2.0, importRate: 0.052, exportRate: 0.09, loadKWH: 3.5, minSOC: 20.0},
			{startTime: now.Add(4 * time.Hour), endTime: now.Add(6 * time.Hour), durationHours: 2.0, importRate: 0.120, exportRate: 0.04, loadKWH: 3.5, minSOC: 20.0},
		}
		blocksOvernight := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.045, avgImportRate: 0.045, maxImportRate: 0.045, minExportRate: 0.02, maxExportRate: 0.02, minSOC: 20.0, durationHours: 2.0, isLowestPrice: true},
			{startIdx: 1, endIdx: 1, minImportRate: 0.052, avgImportRate: 0.052, maxImportRate: 0.052, minExportRate: 0.09, maxExportRate: 0.09, minSOC: 20.0, durationHours: 2.0},
			{startIdx: 2, endIdx: 2, minImportRate: 0.120, avgImportRate: 0.120, maxImportRate: 0.120, minExportRate: 0.04, maxExportRate: 0.04, minSOC: 20.0, durationHours: 2.0, isHighestPrice: true},
		}
		state45 := planState{soc: 45.0, energyKWH: 13.5 * 0.45, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}
		winOvernight, _ := critiqueBlockPlan(ctx, blocksOvernight, timelineOvernight, state45, nil, postHorizonValuation{knownPostHorizonRate: 0.05}, noChargeSettings, types.SystemStatus{}, nil, 0)
		require.Len(t, winOvernight, 3)
		assert.Equal(t, blockActionStandby, winOvernight[0].actionType, "Block 0 ($0.045) must hold in Standby")
		assert.Equal(t, blockActionStandby, winOvernight[1].actionType, "Block 1 ($0.052) must also hold in Standby so battery energy reaches the $0.120 morning peak")
		assert.Equal(t, blockActionLoad, winOvernight[2].actionType, "Block 2 ($0.120 peak) must discharge in Load")
	})

	t.Run("SolverNativelySelectsCheaperChargeAndHoldBlocksWithoutValidationRules", func(t *testing.T) {
		t.Parallel()
		c := NewController()

		// 1. WaitForCheaperBlockInsteadOfCostlierCharge:
		// Block 0 ($0.058/kWh, 1h), Block 1 ($0.042/kWh, 2h), Block 2 ($0.068/kWh off-peak, 2h), Block 3 ($0.35/kWh peak, 2h).
		// Starting at 25% SOC (minSOC = 20%) with a partial peak load (3.0 kWh in Block 3, requiring ~45% SOC),
		// the solver must not charge in Block 0 ($0.058), must charge in the cheaper Block 1 ($0.042), must hold in Standby
		// through Block 2 ($0.068, where discharging $0.042/0.90 + $0.025 wear = $0.0717/kWh energy would be unprofitable),
		// and must discharge in Block 3 ($0.35)—without needing ChargeThenCheaperHold or UnprofitableGridChargeToLoad validation rules.
		noExportSettings := settings
		noExportSettings.GridExportBatteries = false
		timeline4 := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.058, exportRate: 0.02, loadKWH: 0.5, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(3 * time.Hour), durationHours: 2.0, importRate: 0.042, exportRate: 0.02, loadKWH: 1.0, minSOC: 20.0},
			{startTime: now.Add(3 * time.Hour), endTime: now.Add(5 * time.Hour), durationHours: 2.0, importRate: 0.068, exportRate: 0.02, loadKWH: 3.0, minSOC: 20.0},
			{startTime: now.Add(5 * time.Hour), endTime: now.Add(7 * time.Hour), durationHours: 2.0, importRate: 0.350, exportRate: 0.02, loadKWH: 3.0, minSOC: 20.0},
		}
		state25 := planState{soc: 25.0, energyKWH: 13.5 * 0.25, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}
		path4 := c.searchOptimalPlanBlocks(ctx, timeline4, state25, nil, postHorizonValuation{knownPostHorizonRate: 0.05}, noExportSettings, types.SystemStatus{}, nil)
		require.NotNil(t, path4)
		require.Len(t, path4.actions, 4)
		assert.NotEqual(t, types.BatteryModeChargeAny, path4.actions[0].batteryMode,
			"Solver must not grid-charge in costlier Block 0 ($0.058) when cheaper Block 1 ($0.042) follows")
		assert.Equal(t, types.BatteryModeChargeAny, path4.actions[1].batteryMode,
			"Solver must grid-charge in the cheaper Block 1 ($0.042)")
		assert.Equal(t, types.BatteryModeStandby, path4.actions[2].batteryMode,
			"Solver must hold in Standby through intermediate Block 2 ($0.100) so the partial charge reaches the $0.350 Peak")
		assert.Equal(t, types.BatteryModeLoad, path4.actions[3].batteryMode,
			"Solver must discharge in Block 3 ($0.350 Peak)")

		// 2. ShiftStandbyHoldToCheaperBlock:
		// With GridChargeBatteries = false and starting SOC = 45% (minSOC = 20%, enough for only ONE 2h block of 3.0 kWh load),
		// Block 0 ($0.09/kWh, 2h), Block 1 ($0.05/kWh, 2h), Block 2 ($0.25/kWh peak, 2h):
		// Even if seeded with {Standby, Load, Load} (holding at $0.09 and discharging at $0.05), critiqueBlockPlan's
		// {Load, Standby} swap pair must shift the Standby hold to the cheaper Block 1 ($0.05) and discharge in Block 0 ($0.09).
		noChargeSettings := noExportSettings
		noChargeSettings.GridChargeBatteries = false
		timelineSwap := []planInterval{
			{startTime: now, endTime: now.Add(2 * time.Hour), durationHours: 2.0, importRate: 0.09, exportRate: 0.02, loadKWH: 3.0, minSOC: 20.0},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 2.0, importRate: 0.05, exportRate: 0.02, loadKWH: 3.0, minSOC: 20.0},
			{startTime: now.Add(4 * time.Hour), endTime: now.Add(6 * time.Hour), durationHours: 2.0, importRate: 0.25, exportRate: 0.02, loadKWH: 0.5, minSOC: 20.0},
		}
		blocksSwap := buildTimelineBlocks(ctx, timelineSwap, nil, noChargeSettings, types.SystemStatus{})
		require.Len(t, blocksSwap, 3)
		state45 := planState{soc: 45.0, energyKWH: 13.5 * 0.45, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}
		winSwap, _ := critiqueBlockPlan(ctx, blocksSwap, timelineSwap, state45, nil, postHorizonValuation{knownPostHorizonRate: 0.05}, noChargeSettings, types.SystemStatus{}, nil, 0)
		require.Len(t, winSwap, 3)
		assert.Equal(t, blockActionLoad, winSwap[0].actionType, "Solver must discharge in costlier Block 0 ($0.09)")
		assert.Equal(t, blockActionStandby, winSwap[1].actionType, "Solver must shift Standby hold to cheaper Block 1 ($0.05)")
	})
}

func TestOptimizeBlockTargets(t *testing.T) {
	t.Parallel()

	settings := types.Settings{
		GridChargeBatteries: true,
		GridExportBatteries: true,
		GridExportSolar:     true,
		ManageTOUSchedules:  true,
		OptimizationProfile: "balanced",
	}
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	timeline := make([]planInterval, 4)
	for i := 0; i < 4; i++ {
		ti := now.Add(time.Duration(i*30) * time.Minute)
		rate := 0.05
		exp := 0.04
		if i >= 2 {
			rate = 0.50
			exp = 0.45
		}
		timeline[i] = planInterval{
			startTime:     ti,
			endTime:       ti.Add(30 * time.Minute),
			durationHours: 0.5,
			importRate:    rate,
			exportRate:    exp,
			solarKWH:      0.0,
			loadKWH:       0.5,
			minSOC:        20.0,
		}
	}
	blocks := []timelineBlock{
		{startIdx: 0, endIdx: 1, minImportRate: 0.05, avgImportRate: 0.05, maxImportRate: 0.05, minSOC: 20.0, durationHours: 1.0, isLowestPrice: true},
		{startIdx: 2, endIdx: 3, minImportRate: 0.50, avgImportRate: 0.50, maxImportRate: 0.50, minExportRate: 0.45, maxExportRate: 0.45, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true},
	}
	state := planState{soc: 20.0, energyKWH: 13.5 * 0.20, capacityKWH: 13.5, time: now}
	choices := []blockActionChoice{
		{actionType: blockActionCharge, targetSOC: 100.0},
		{actionType: blockActionExport, targetSOC: 20.0},
	}

	ctx := context.Background()
	path := optimizeBlockTargets(ctx, choices, blocks, timeline, state, nil, postHorizonValuation{}, settings, types.SystemStatus{}, nil, 0)
	require.NotNil(t, path)
	require.Len(t, path.actions, 4)

	assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)
	assert.Equal(t, types.BatteryModeExport, path.actions[3].batteryMode)

	t.Run("PicksMinimalTargetNeededForPeakLoadAvoidingOvercharge", func(t *testing.T) {
		t.Parallel()

		noExportSettings := types.Settings{
			GridChargeBatteries: true,
			GridExportBatteries: false,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		// Block 0: off-peak $0.05/kWh, load 0.1 kWh.
		// Block 1: peak $0.40/kWh, load 1.0 kWh (only needs ~1.1 kWh from battery = ~28% SOC).
		// Overcharging to 100% (13.5 kWh) would waste grid charge energy and cycle wear.
		timelineLoad := make([]planInterval, 4)
		for i := 0; i < 4; i++ {
			ti := now.Add(time.Duration(i*30) * time.Minute)
			rate := 0.05
			load := 0.1
			if i >= 2 {
				rate = 0.40
				load = 0.5
			}
			timelineLoad[i] = planInterval{
				startTime:     ti,
				endTime:       ti.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    rate,
				exportRate:    0.02,
				solarKWH:      0.0,
				loadKWH:       load,
				minSOC:        20.0,
			}
		}
		blocksLoad := []timelineBlock{
			{startIdx: 0, endIdx: 1, minImportRate: 0.05, avgImportRate: 0.05, maxImportRate: 0.05, minSOC: 20.0, durationHours: 1.0, isLowestPrice: true},
			{startIdx: 2, endIdx: 3, minImportRate: 0.40, avgImportRate: 0.40, maxImportRate: 0.40, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true},
		}
		choicesLoad := []blockActionChoice{
			{actionType: blockActionCharge, targetSOC: 100.0},
			{actionType: blockActionLoad, targetSOC: 20.0},
		}

		pathLoad := optimizeBlockTargets(ctx, choicesLoad, blocksLoad, timelineLoad, state, nil, postHorizonValuation{}, noExportSettings, types.SystemStatus{}, nil, 0)
		require.NotNil(t, pathLoad)
		maxSOC := 0.0
		for _, st := range pathLoad.states {
			if st.soc > maxSOC {
				maxSOC = st.soc
			}
		}
		assert.Less(t, maxSOC, 80.0, "Planner should optimize charge target to cover load without overcharging to 100%")
	})

	t.Run("PreservesPostPeakReserveAfterMiddayCloudDipAndPartialAfternoonSolar", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// Block 0 (midday peak with cloud dip): import $0.35, export $0.30, minSOC 20%, solar 0 kWh, load 0.5 kWh.
		// Block 1 (late afternoon with partial solar recovery): import $0.15, export $0.05, minSOC 20%, solar 1.5 kWh, load 0.5 kWh (net +1.0 kWh solar).
		// Block 2 (post-peak evening with reserve step-up to 50%): import $0.15, export $0.05, minSOC 50%, solar 0 kWh, load 1.0 kWh, GridChargeBatteries = false.
		// Starting at 80% SOC (10.8 kWh of 13.5 kWh), exporting all the way down to 20% during the midday cloud dip
		// would leave the battery unable to reach the 50% post-peak reserve even after the +1.0 kWh afternoon solar recovery.
		// The solver must optimize the midday export floor so that Block 2 enters at >= 50% SOC and covers the expensive load.
		cloudDipSettings := types.Settings{
			GridChargeBatteries: false,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		timelineCloud := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(1 * time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				exportRate:    0.30,
				solarKWH:      0.0,
				loadKWH:       0.5,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(1 * time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.05,
				solarKWH:      1.5,
				loadKWH:       0.5,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.05,
				solarKWH:      0.0,
				loadKWH:       1.0,
				minSOC:        50.0,
			},
		}
		blocksCloud := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.35, avgImportRate: 0.35, maxImportRate: 0.35, minExportRate: 0.30, maxExportRate: 0.30, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true},
			{startIdx: 1, endIdx: 1, minImportRate: 0.15, avgImportRate: 0.15, maxImportRate: 0.15, minExportRate: 0.05, maxExportRate: 0.05, minSOC: 20.0, durationHours: 1.0},
			{startIdx: 2, endIdx: 2, minImportRate: 0.15, avgImportRate: 0.15, maxImportRate: 0.15, minExportRate: 0.05, maxExportRate: 0.05, minSOC: 50.0, durationHours: 1.0},
		}
		startStateCloud := planState{soc: 80.0, energyKWH: 13.5 * 0.80, capacityKWH: 13.5, time: now}
		choicesCloud := []blockActionChoice{
			{actionType: blockActionExport, targetSOC: 50.0},
			{actionType: blockActionLoad, targetSOC: 20.0},
			{actionType: blockActionLoad, targetSOC: 50.0},
		}

		pathCloud := optimizeBlockTargets(ctx, choicesCloud, blocksCloud, timelineCloud, startStateCloud, nil, postHorizonValuation{}, cloudDipSettings, types.SystemStatus{}, nil, 0)
		require.NotNil(t, pathCloud)
		require.Len(t, pathCloud.states, 4)

		// Interval 2 starts at states[2] and has minSOC = 50.0%.
		assert.GreaterOrEqual(t, pathCloud.states[2].soc, 50.0-socTargetTolerancePct,
			"Solver must choose an export floor during midday cloud dip that still reaches the 50%% post-peak reserve after partial afternoon solar")
	})

	t.Run("OptimizesSecondExportBlockWhenChargeBlockAlsoPresent", func(t *testing.T) {
		t.Parallel()

		// When a plan has a Charge block (Block 0) AND two Export blocks (Block 1 and Block 2)
		// followed by a post-peak reserve step-up to 50% in Block 3, the 1D refinement loop must run
		// after the 2D (Block 0, Block 1) search so that Block 2's export floor is also raised from 20% to >= 50%.
		multiExportSettings := types.Settings{
			GridChargeBatteries: true,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		timelineMulti := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(2 * time.Hour),
				durationHours: 2.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.2,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				exportRate:    0.30,
				loadKWH:       0.2,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(3 * time.Hour),
				endTime:       now.Add(4 * time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				exportRate:    0.40,
				loadKWH:       0.2,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 1.0,
				importRate:    0.20,
				exportRate:    0.05,
				loadKWH:       0.5,
				minSOC:        50.0,
			},
		}
		blocksMulti := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.05, avgImportRate: 0.05, maxImportRate: 0.05, minExportRate: 0.02, maxExportRate: 0.02, minSOC: 20.0, durationHours: 2.0, isLowestPrice: true},
			{startIdx: 1, endIdx: 1, minImportRate: 0.35, avgImportRate: 0.35, maxImportRate: 0.35, minExportRate: 0.30, maxExportRate: 0.30, minSOC: 20.0, durationHours: 1.0},
			{startIdx: 2, endIdx: 2, minImportRate: 0.45, avgImportRate: 0.45, maxImportRate: 0.45, minExportRate: 0.40, maxExportRate: 0.40, minSOC: 20.0, durationHours: 1.0, isHighestPrice: true},
			{startIdx: 3, endIdx: 3, minImportRate: 0.20, avgImportRate: 0.20, maxImportRate: 0.20, minExportRate: 0.05, maxExportRate: 0.05, minSOC: 50.0, durationHours: 1.0},
		}
		startStateMulti := planState{soc: 50.0, energyKWH: 13.5 * 0.50, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}
		choicesMulti := []blockActionChoice{
			{actionType: blockActionCharge, targetSOC: 100.0},
			{actionType: blockActionExport, targetSOC: 80.0},
			{actionType: blockActionExport, targetSOC: 20.0}, // Initial target 20% violates Block 3's 50% reserve unless 1D loop optimizes Block 2
			{actionType: blockActionLoad, targetSOC: 50.0},
		}

		pathMulti := optimizeBlockTargets(ctx, choicesMulti, blocksMulti, timelineMulti, startStateMulti, nil, postHorizonValuation{}, multiExportSettings, types.SystemStatus{}, nil, 0)
		require.NotNil(t, pathMulti)
		require.Len(t, pathMulti.states, 5)
		assert.GreaterOrEqual(t, pathMulti.states[3].soc, 50.0-socTargetTolerancePct,
			"Second Export block (Block 2) must be optimized by 1D search to preserve >= 50%% SOC for Block 3 even when a Charge block is also present")
	})

	t.Run("Step0ComparisonDebugLog_RespectsDefaultSolarMode", func(t *testing.T) {
		t.Parallel()

		// When GridExportSolar is false and step 0 has surplus solar with the battery already at 100% SOC,
		// the step-0 comparison debug log must set defaultSolarMode (SolarModeNoExport) on the trial candidates
		// so Load at step 0 matches the winning Load trajectory score rather than simulating illegal solar export.
		noSolarExportSettings := types.Settings{
			GridChargeBatteries: false,
			GridExportBatteries: false,
			GridExportSolar:     false,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		timelineNoExport := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(1 * time.Hour),
				durationHours: 1.0,
				importRate:    0.20,
				exportRate:    0.30,
				solarKWH:      5.0,
				loadKWH:       0.5,
				minSOC:        20.0,
			},
		}
		blocksNoExport := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.20, avgImportRate: 0.20, maxImportRate: 0.20, minExportRate: 0.30, maxExportRate: 0.30, minSOC: 20.0, durationHours: 1.0},
		}
		fullState := planState{soc: 100.0, energyKWH: 13.5, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0, time: now}
		choicesNoExport := []blockActionChoice{
			{actionType: blockActionLoad},
		}

		var logBuf bytes.Buffer
		debugCtx := log.With(ctx, slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		optimizeBlockTargets(debugCtx, choicesNoExport, blocksNoExport, timelineNoExport, fullState, nil, postHorizonValuation{}, noSolarExportSettings, types.SystemStatus{}, nil, 0)

		assert.Contains(t, logBuf.String(), "Load:$0.001(+$0.000)",
			"Step-0 Load comparison candidate must use SolarModeNoExport (curtailing solar, $0.001 holding cost) rather than zero-value SolarModeUnspec (exporting 4.5 kWh * $0.30 = -$1.349)")
	})

	t.Run("Fine2PercentTargetSOCStep_AvoidsSolarOverflowAndPeakOverExport", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// - Block 0 (morning cheap grid charge): $0.24/kWh import, $0.02/kWh export, 2.0h.
		// - Block 1 (afternoon tail solar before peak): $0.24/kWh import, $0.02/kWh export, 1.0h, net solar +0.35 kWh (~1.8% SOC on 18 kWh battery).
		// - Block 2 (peak export): $0.59/kWh import, $0.50/kWh export, 1.0h, load 0.2 kWh.
		// - Block 3 (late peak load-only): $0.59/kWh import, $0.08/kWh export, 1.0h, load 0.32 kWh (~1.9% SOC on 18 kWh battery).
		// With 2% target-SOC quantization (targetSOCStepPct = 2.0):
		// 1. Block 0 charges to 98% (not 100%) so Block 1's +0.35 kWh solar fills the battery to ~99.8% with 0.00 kWh spilled at $0.02/kWh.
		// 2. Block 2 exports down to 17% (minF=15% + 2% step, instead of 15% or 20% under 5% quantization) so Block 3's 0.32 kWh peak load is covered down to the 15% reserve with 0.00 kWh peak grid import.
		fineSettings := types.Settings{
			GridChargeBatteries: true,
			GridExportBatteries: true,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       15.0,
		}
		timelineFine := []planInterval{
			{startTime: now, endTime: now.Add(2 * time.Hour), durationHours: 2.0, importRate: 0.24, exportRate: 0.02, loadKWH: 0.2, solarKWH: 0.0, minSOC: 15.0},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.24, exportRate: 0.02, loadKWH: 0.2, solarKWH: 0.55, minSOC: 15.0},
			{startTime: now.Add(3 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.59, exportRate: 0.50, loadKWH: 0.2, solarKWH: 0.0, minSOC: 15.0},
			{startTime: now.Add(4 * time.Hour), endTime: now.Add(5 * time.Hour), durationHours: 1.0, importRate: 0.59, exportRate: 0.08, loadKWH: 0.32, solarKWH: 0.0, minSOC: 15.0},
		}
		blocksFine := []timelineBlock{
			{startIdx: 0, endIdx: 0, minImportRate: 0.24, avgImportRate: 0.24, maxImportRate: 0.24, minExportRate: 0.02, maxExportRate: 0.02, minSOC: 15.0, durationHours: 2.0, isLowestPrice: true},
			{startIdx: 1, endIdx: 1, minImportRate: 0.24, avgImportRate: 0.24, maxImportRate: 0.24, minExportRate: 0.02, maxExportRate: 0.02, minSOC: 15.0, durationHours: 1.0, netSolarKWH: 0.35, isLowestPrice: true},
			{startIdx: 2, endIdx: 2, minImportRate: 0.59, avgImportRate: 0.59, maxImportRate: 0.59, minExportRate: 0.50, maxExportRate: 0.50, minSOC: 15.0, durationHours: 1.0, isHighestPrice: true},
			{startIdx: 3, endIdx: 3, minImportRate: 0.59, avgImportRate: 0.59, maxImportRate: 0.59, minExportRate: 0.08, maxExportRate: 0.08, minSOC: 15.0, durationHours: 1.0, isHighestPrice: true},
		}
		state18kWh := planState{soc: 50.0, energyKWH: 9.0, capacityKWH: 18.0, maxChargeKW: 15.0, maxDischargeKW: 18.0, time: now}
		choicesFine := []blockActionChoice{
			{actionType: blockActionCharge, targetSOC: 100.0},
			{actionType: blockActionLoad, targetSOC: 15.0},
			{actionType: blockActionExport, targetSOC: 15.0},
			{actionType: blockActionLoad, targetSOC: 15.0},
		}

		pathFine := optimizeBlockTargets(ctx, choicesFine, blocksFine, timelineFine, state18kWh, nil, postHorizonValuation{knownPostHorizonRate: 0.24}, fineSettings, types.SystemStatus{}, nil, 0)
		require.NotNil(t, pathFine)
		require.Len(t, pathFine.metrics, 4)
		assert.InDelta(t, 98.0, pathFine.actions[0].targetSOC, 0.1,
			"2%% step must allow charging to 98%% so +0.35 kWh afternoon solar is banked without overflow")
		assert.InDelta(t, 0.0, pathFine.metrics[1].gridExportKWH, 0.01,
			"No solar should overflow to the grid at $0.02/kWh in Block 1")
		assert.InDelta(t, 17.0, pathFine.actions[2].targetSOC, 0.1,
			"2%% step must allow exporting down to 17%% (15%% + 2%%) so Block 3's 0.32 kWh peak load is covered without importing at $0.59/kWh")
		assert.InDelta(t, 0.0, pathFine.metrics[3].gridImportKWH, 0.01,
			"Late peak Block 3 must have zero grid import")
	})
}

// TestPlanConstraintMechanisms evaluates the elimination of arbitrary penalties in favor of
// exact constraint enforcement and multi-objective strike evaluation across synthetic real-world edge cases.
func TestPlanConstraintMechanisms(t *testing.T) {
	t.Parallel()

	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)

	t.Run("ReserveDeficit_RechargesImmediatelyWithoutArtificialPenalty", func(t *testing.T) {
		t.Parallel()

		// Battery starts in deficit at 10% SOC; user's configured reserve floor is 25%.
		initSt := planState{
			time:        now,
			energyKWH:   13.5 * 0.10,
			soc:         10.0,
			capacityKWH: 13.5,
		}
		status := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         10.0,
		}
		sett := types.Settings{
			MinBatterySOC:       25.0,
			GridChargeBatteries: true,
		}

		timeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.12, loadKWH: 1.0, minSOC: 25.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.12, loadKWH: 1.0, minSOC: 25.0},
		}

		path := c.searchOptimalPlanBlocks(ctx, timeline, initSt, nil, postHorizonValuation{}, sett, status, nil)
		require.NotEmpty(t, path.actions)

		// Must choose ChargeAny immediately at step 0 to restore backup reserve
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)

		// Total physical cost must reflect true physical kWh bought from the grid (unmet load + battery charge),
		// without any artificial Big-M or 3x deficit penalty markups.
		assert.Less(t, path.totalCost, 10.0, "Total cost must be pure electricity cost, not inflated by penalty amounts")
	})

	t.Run("ReserveDeficit_DefersGridChargeWhenNextIntervalIsMateriallyCheaper", func(t *testing.T) {
		t.Parallel()

		// Battery starts in deficit at 14.1% SOC (< 25% minSOC) during the final 20 minutes of an On-Peak window
		// ($0.2998/kWh) right before the Off-Peak tariff drops to $0.2075/kWh in the next interval.
		// Rather than forcing a deficit grid-charge at the peak rate for 20 minutes, the planner must wait in
		// Load (batteryAtReserve) or Standby at step 0 and recharge at the cheaper Off-Peak rate at step 1.
		initSt := planState{
			time:           now,
			energyKWH:      30.0 * 0.141,
			soc:            14.1,
			capacityKWH:    30.0,
			maxChargeKW:    16.0,
			maxDischargeKW: 20.0,
		}
		status := types.SystemStatus{
			BatteryCapacityKWH:    30.0,
			BatterySOC:            14.1,
			MaxBatteryChargeKW:    16.0,
			MaxBatteryDischargeKW: 20.0,
		}
		sett := types.Settings{
			MinBatterySOC:       25.0,
			GridChargeBatteries: true,
		}

		timeline := []planInterval{
			{startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.2998, exportRate: 0.2948, loadKWH: 0.2, minSOC: 25.0},
			{startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.2075, exportRate: 0.2025, loadKWH: 0.2, minSOC: 25.0},
			{startTime: now.Add(40 * time.Minute), endTime: now.Add(time.Hour), durationHours: 20.0 / 60.0, importRate: 0.2075, exportRate: 0.2025, loadKWH: 0.2, minSOC: 25.0},
		}

		path := c.searchOptimalPlanBlocks(ctx, timeline, initSt, nil, postHorizonValuation{knownPostHorizonRate: 0.2075}, sett, status, nil)
		require.Len(t, path.actions, 3)

		assert.NotEqual(t, types.BatteryModeChargeAny, path.actions[0].batteryMode,
			"Must defer reserve grid charging during the final 20m of On-Peak ($0.2998/kWh) when Off-Peak ($0.2075/kWh) starts next")
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[1].batteryMode,
			"Must recharge to reserve as soon as the cheaper Off-Peak rate begins at step 1")
	})

	t.Run("VPPEvent_ReachesFullChargeWithoutBigMDollarInflation", func(t *testing.T) {
		t.Parallel()

		initSt := planState{
			time:        now,
			energyKWH:   13.5 * 0.60,
			soc:         60.0,
			capacityKWH: 13.5,
		}
		status := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         60.0,
		}
		sett := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}

		vppStart := now.Add(4 * time.Hour)
		vppDeadline := now.Add(2 * time.Hour) // Lead time of 2 hours
		vppEvents := []vppAnchor{
			{
				eventStart: vppStart,
				eventEnd:   vppStart.Add(2 * time.Hour),
				deadline:   vppDeadline,
				mandatory:  true,
				vppSoc:     20.0,
			},
		}

		timeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 0.8, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 0.8, minSOC: 20.0},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.15, loadKWH: 0.8, minSOC: 20.0},
			{startTime: now.Add(3 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.15, loadKWH: 0.8, minSOC: 20.0},
		}

		path := c.searchOptimalPlanBlocks(ctx, timeline, initSt, vppEvents, postHorizonValuation{}, sett, status, nil)
		require.NotEmpty(t, path.actions)

		// Must pre-charge to reach full capacity by VPP preparation window
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)

		// Final score and totalCost must be realistic financial dollars (< $5.00), not distorted by $1,000+ Big-M penalties
		assert.Less(t, path.bestScore, 10.0, "Score must not include $1,000+ Big-M penalty values")
		assert.Less(t, path.totalCost, 10.0, "Physical cost must reflect real electricity costs only")
	})

	t.Run("ExtremePriceSpike_ZeroPenaltyDistortion", func(t *testing.T) {
		t.Parallel()

		// Extreme real-world ComEd spike scenario ($4.50/kWh peak)
		initSt := planState{
			time:        now,
			energyKWH:   13.5 * 0.15,
			soc:         15.0, // starts slightly below 20% reserve
			capacityKWH: 13.5,
		}
		status := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         15.0,
		}
		sett := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}

		timeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 1.0, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 4.50, loadKWH: 2.0, minSOC: 20.0},
		}

		path := c.searchOptimalPlanBlocks(ctx, timeline, initSt, nil, postHorizonValuation{}, sett, status, nil)
		require.NotEmpty(t, path.actions)

		// Step 0 charges cheaply ($0.05/kWh) to restore reserve and prepare for spike
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)
		// Step 1 discharges to shield home from the $4.50/kWh spike
		assert.Equal(t, types.BatteryModeLoad, path.actions[1].batteryMode)
	})

	t.Run("EversourceRate7_NoonDirectSolarExport", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		nowNoon := time.Date(2026, 10, 5, 12, 0, 0, 0, nyLoc)
		currentPrice := types.Price{
			TSStart:                       nowNoon,
			TSEnd:                         nowNoon.Add(time.Hour),
			DollarsPerKWH:                 0.29982,
			GenerationCreditDollarsPerKWH: 0.25962,
			SeparateGenerationCredit:      true,
		}

		var futurePrices []types.Price
		for h := 13; h < 36; h++ {
			day := 5
			hour := h
			if hour >= 24 {
				day = 6
				hour -= 24
			}
			rate := 0.20754
			exportRate := 0.16734
			if hour >= 12 && hour < 20 {
				rate = 0.29982
				exportRate = 0.25962
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:                       time.Date(2026, 10, day, hour, 0, 0, 0, nyLoc),
				TSEnd:                         time.Date(2026, 10, day, hour+1, 0, 0, 0, nyLoc),
				DollarsPerKWH:                 rate,
				GenerationCreditDollarsPerKWH: exportRate,
				SeparateGenerationCredit:      true,
			})
		}

		status := types.SystemStatus{
			Timestamp:          nowNoon,
			TimeLocation:       "America/New_York",
			BatteryCapacityKWH: 30.0,
			BatterySOC:         76.5,
			HomeKW:             0.32,
			SolarKW:            4.1,
		}

		settings := types.Settings{
			MinBatterySOC:      5,
			GridExportSolar:    true,
			ManageTOUSchedules: true,
		}

		// Mock history with typical solar and home load
		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 8 && h <= 17 {
					solar = 4.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  nowNoon.AddDate(0, 0, -d).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:      0.32,
					SolarKWH:     solar,
					TimeLocation: "America/New_York",
				})
			}
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
			SolarMode:   types.SolarModeAny,
			CurrentPrice: &types.Price{
				DollarsPerKWH:                 0.20754,
				GenerationCreditDollarsPerKWH: 0.16734,
				SeparateGenerationCredit:      true,
			},
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, lastAction)
		require.NoError(t, err)
		require.NotNil(t, plan)

		sched := BuildTOUSchedule(&plan, nowNoon, nyLoc)
		require.NotNil(t, sched)
		require.Len(t, sched.Periods, 3)

		// TOU Period 0: Morning off-peak self-consumption
		assert.Equal(t, 0, sched.Periods[0].StartHour)
		assert.Equal(t, 12, sched.Periods[0].EndHour)
		assert.False(t, sched.Periods[0].Peak)
		assert.Equal(t, types.SolarModeAny, sched.Periods[0].SolarMode)

		// TOU Period 1: Afternoon peak (12:00 to 20:00) with direct solar export
		assert.Equal(t, 12, sched.Periods[1].StartHour)
		assert.Equal(t, 20, sched.Periods[1].EndHour)
		assert.True(t, sched.Periods[1].Peak)
		assert.Equal(t, types.BatteryModeLoad, sched.Periods[1].BatteryMode)
		assert.Equal(t, types.SolarModeExport, sched.Periods[1].SolarMode)

		// TOU Period 2: Evening off-peak self-consumption
		assert.Equal(t, 20, sched.Periods[2].StartHour)
		assert.Equal(t, 24, sched.Periods[2].EndHour)
		assert.False(t, sched.Periods[2].Peak)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode, "Battery should discharge to cover home load")
		assert.Equal(t, types.SolarModeExport, decision.Action.SolarMode, "Solar should export starting at noon")
	})

	t.Run("MorningPeakBatteryExport_RefilledByMiddaySurplusSolar", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Start at 15:00 in the afternoon so that by tomorrow morning's 07:00-09:00 peak,
		// tonight's 22:00-05:00 super-off-peak window is in the past (i < stepIdx) and
		// tomorrow night's 22:00 super-off-peak window is beyond the 24h horizon + 4h post-horizon window.
		// Midday surplus solar (10:00-13:00 at $0.02375/kWh export credit) refills the battery.
		startAfternoon := time.Date(2026, 10, 13, 15, 0, 0, 0, nyLoc)

		priceForHour := func(t time.Time) types.Price {
			h := t.Hour()
			imp := 0.10481
			exp := 0.02375
			if h >= 22 || h < 5 {
				imp = 0.05500
				exp = 0.02375
			} else if (h >= 7 && h < 9) || (h >= 13 && h < 19) {
				imp = 0.31443
				exp = 0.09400
			}
			return types.Price{
				TSStart:                       t,
				TSEnd:                         t.Add(time.Hour),
				DollarsPerKWH:                 imp,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: exp,
			}
		}

		currentPrice := priceForHour(startAfternoon)
		var futurePrices []types.Price
		for i := 1; i < 25; i++ {
			futurePrices = append(futurePrices, priceForHour(startAfternoon.Add(time.Duration(i)*time.Hour)))
		}

		status := types.SystemStatus{
			Timestamp:             startAfternoon,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    15.0,
			BatterySOC:            95.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
		}

		settings := types.Settings{
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
		}

		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			dayStart := startAfternoon.AddDate(0, 0, -d).Truncate(24 * time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 10 && h < 16 {
					solar = 6.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  dayStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:      1.0,
					SolarKWH:     solar,
					TimeLocation: "America/New_York",
				})
			}
		}

		_, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		morningPeakStart := time.Date(2026, 10, 14, 7, 0, 0, 0, nyLoc)
		morningPeakEnd := time.Date(2026, 10, 14, 9, 0, 0, 0, nyLoc)
		foundMorningExport := false
		for _, p := range plan.Periods {
			if !p.TSStart.Before(morningPeakStart) && p.TSStart.Before(morningPeakEnd) {
				if p.BatteryMode == types.BatteryModeExport {
					foundMorningExport = true
					break
				}
			}
		}
		assert.True(t, foundMorningExport, "Expected BatteryModeExport during 07:00-09:00 morning peak when midday surplus solar refills the battery")
	})

	t.Run("PeakBatteryExport_DelayedToEndOfPeakWindow", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Start at 12:00 PM at the beginning of a 12:00 PM - 8:00 PM flat peak period ($0.149/kWh import & export,
		// $0.055/kWh off-peak) with afternoon solar (12:00 PM - 5:00 PM).
		// Battery starts at 51% SOC on a 27 kWh pack (10 kW max discharge).
		// Rather than dumping from 51% -> 20% at 12:00 PM, the plan must stay in BatteryModeLoad through the afternoon
		// (absorbing surplus solar and covering afternoon peak load) and delay BatteryModeExport until the end of
		// the peak period (after 6:00 PM).
		nowNoon := time.Date(2026, 10, 14, 12, 0, 0, 0, nyLoc)

		priceForHour := func(ts time.Time) types.Price {
			h := ts.Hour()
			imp := 0.055
			exp := 0.055
			if h >= 12 && h < 20 {
				imp = 0.149
				exp = 0.149
			}
			return types.Price{
				TSStart:                       ts,
				TSEnd:                         ts.Add(time.Hour),
				DollarsPerKWH:                 imp,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: exp,
			}
		}

		currentPrice := priceForHour(nowNoon)
		var futurePrices []types.Price
		for i := 1; i < 28; i++ {
			futurePrices = append(futurePrices, priceForHour(nowNoon.Add(time.Duration(i)*time.Hour)))
		}

		status := types.SystemStatus{
			Timestamp:             nowNoon,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    27.0,
			BatterySOC:            51.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 10.0,
			SolarKW:               3.0,
			HomeKW:                0.5,
		}

		settings := types.Settings{
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
			GridExportSolar:     false,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
		}

		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			dayStart := nowNoon.AddDate(0, 0, -d).Truncate(24 * time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 9 && h < 17 {
					solar = 3.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  dayStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:      0.5,
					SolarKWH:     solar,
					TimeLocation: "America/New_York",
				})
			}
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
			SolarMode:   types.SolarModeAny,
			CurrentPrice: &types.Price{
				DollarsPerKWH: 0.055,
			},
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, lastAction)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Immediate decision at 12:00 PM must be BatteryModeLoad, NOT an early BatteryModeExport dump
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
			"Must remain in BatteryModeLoad at 12:00 PM rather than dumping the battery at the start of peak")

		sixPM := time.Date(2026, 10, 14, 18, 0, 0, 0, nyLoc)
		eightPM := time.Date(2026, 10, 14, 20, 0, 0, 0, nyLoc)
		foundLatePeakExport := false
		for _, p := range plan.Periods {
			if p.TSStart.Before(sixPM) && p.TSEnd.Before(sixPM.Add(time.Minute)) {
				assert.NotEqual(t, types.BatteryModeExport, p.BatteryMode,
					"Must not schedule BatteryModeExport before 6:00 PM (period %s - %s)",
					p.TSStart.Format("15:04"), p.TSEnd.Format("15:04"))
			}
			if !p.TSStart.Before(sixPM) && p.TSStart.Before(eightPM) && p.BatteryMode == types.BatteryModeExport {
				foundLatePeakExport = true
			}
		}
		assert.True(t, foundLatePeakExport,
			"Must schedule BatteryModeExport in the final hours of the peak period (6:00 PM - 8:00 PM)")
	})

	t.Run("ThreeTierTOD_ClampsExportToPostPeakReserveAndPreservesOffPeakLoad", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Three-tier TOD schedule modeled after Rutherford Electric TOD:
		// - Super Off-Peak (22:00 - 05:00): $0.0550/kWh import, $0.0238/kWh export, MinSOC = 40%
		// - Off-Peak (05:00 - 07:00, 09:00 - 13:00, 19:00 - 22:00): $0.1048/kWh import, $0.0238/kWh export, MinSOC = 20%
		// - On-Peak (07:00 - 09:00, 13:00 - 19:00): $0.3144/kWh import, $0.0940/kWh export, MinSOC = 5%
		priceForHour := func(ts time.Time) types.Price {
			h := ts.In(nyLoc).Hour()
			imp := 0.10481
			exp := 0.02375
			periodName := "Off-Peak"
			if h >= 22 || h < 5 {
				imp = 0.05503
				exp = 0.02375
				periodName = "Super Off-Peak"
			} else if (h >= 7 && h < 9) || (h >= 13 && h < 19) {
				imp = 0.31443
				exp = 0.0940
				periodName = "On-Peak"
			}
			return types.Price{
				TSStart:                       ts,
				TSEnd:                         ts.Add(time.Hour),
				DollarsPerKWH:                 imp,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: exp,
				PeriodName:                    periodName,
			}
		}

		settings := types.Settings{
			OptimizationProfile:                     "balanced",
			MinBatterySOC:                           5.0,
			GridChargeBatteries:                     true,
			GridExportSolar:                         true,
			GridExportBatteries:                     true,
			ManageTOUSchedules:                      true,
			MinBatteryExportDifferenceDollarsPerKWH: 0.02,
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "On-Peak", MinBatterySOC: 5},
				{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20},
				{UtilityPeriodName: "Super Off-Peak", MinBatterySOC: 40},
			},
		}

		buildHistory := func(refTime time.Time) []types.EnergyStats {
			var history []types.EnergyStats
			for d := 1; d <= 7; d++ {
				dayStart := time.Date(refTime.Year(), refTime.Month(), refTime.Day(), 0, 0, 0, 0, nyLoc).AddDate(0, 0, -d)
				for h := 0; h < 24; h++ {
					solar := 0.0
					switch h {
					case 9:
						solar = 1.5
					case 10:
						solar = 3.0
					case 11, 12, 13, 14:
						solar = 4.2
					case 15:
						solar = 3.0
					case 16:
						solar = 1.5
					}
					load := 0.8
					if h >= 19 && h < 22 {
						load = 1.5
					}
					history = append(history, types.EnergyStats{
						TSHourStart:  dayStart.Add(time.Duration(h) * time.Hour),
						HomeKWH:      load,
						SolarKWH:     solar,
						TimeLocation: "America/New_York",
					})
				}
			}
			return history
		}

		// Case 1: At 06:00 AM (Off-Peak $0.1048/kWh before 07:00-09:00 On-Peak $0.0940/kWh export),
		// battery at 90% SOC must stay in BatteryModeLoad (not Standby holding for a lower-rate export).
		t0600 := time.Date(2026, 10, 19, 6, 0, 0, 0, nyLoc)
		var future0600 []types.Price
		for i := 1; i < 28; i++ {
			future0600 = append(future0600, priceForHour(t0600.Add(time.Duration(i)*time.Hour)))
		}
		status0600 := types.SystemStatus{
			Timestamp:             t0600,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    13.5,
			BatterySOC:            90.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    6.6,
			MaxBatteryDischargeKW: 8.0,
			HomeKW:                0.8,
		}
		dec0600, plan0600, err := c.Plan(ctx, status0600, priceForHour(t0600), future0600, buildHistory(t0600), nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, dec0600.Action.BatteryMode,
			"At 06:00 Off-Peak ($0.1048/kWh), battery must cover home load rather than entering Standby for a $0.0940/kWh export")
		assert.Empty(t, findDominatedPlanPeriods(plan0600), "Plan at 06:00 must not contain dominated Standby/Export periods")

		// Verify morning peak export in the plan is delayed to 08:00-09:00 and clamped to >= 20% (09:00 Off-Peak reserve)
		t0700 := time.Date(2026, 10, 19, 7, 0, 0, 0, nyLoc)
		t0800 := time.Date(2026, 10, 19, 8, 0, 0, 0, nyLoc)
		t0900 := time.Date(2026, 10, 19, 9, 0, 0, 0, nyLoc)
		foundMorningExport := false
		for _, p := range plan0600.Periods {
			if !p.TSStart.Before(t0700) && p.TSEnd.Before(t0800.Add(time.Minute)) {
				assert.Equal(t, types.BatteryModeLoad, p.BatteryMode,
					"Early morning peak (07:00-08:00) must stay in BatteryModeLoad before late-window export")
			}
			if !p.TSStart.Before(t0800) && p.TSStart.Before(t0900) && p.BatteryMode == types.BatteryModeExport {
				foundMorningExport = true
				assert.GreaterOrEqual(t, p.EndSOC, 19.5,
					"Morning peak export must stop at >= 20%% SOC so it does not trigger a 09:00 Off-Peak deficit recharge")
			}
		}
		assert.True(t, foundMorningExport, "Plan must schedule late-morning peak export (08:00-09:00)")

		// Case 2: At 18:20 (final interval of afternoon On-Peak before 19:00-22:00 Off-Peak load),
		// BatteryModeExport must preserve enough SOC (> 20%) to cover 19:00-22:00 Off-Peak load ($0.1048/kWh > $0.0940/kWh export).
		t1820 := time.Date(2026, 10, 19, 18, 20, 0, 0, nyLoc)
		t1800 := time.Date(2026, 10, 19, 18, 0, 0, 0, nyLoc)
		var future1820 []types.Price
		for i := 1; i < 28; i++ {
			future1820 = append(future1820, priceForHour(t1800.Add(time.Duration(i)*time.Hour)))
		}
		status1820 := types.SystemStatus{
			Timestamp:             t1820,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    13.5,
			BatterySOC:            92.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    6.6,
			MaxBatteryDischargeKW: 8.0,
			HomeKW:                0.8,
		}
		dec1820, plan1820, err := c.Plan(ctx, status1820, priceForHour(t1800), future1820, buildHistory(t1820), nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeExport, dec1820.Action.BatteryMode,
			"At 18:20 On-Peak with 92%% SOC, battery must export surplus energy above 19:00-22:00 Off-Peak load requirement")
		assert.Greater(t, dec1820.Action.ChargeToSOC, 45,
			"Evening peak export ChargeToSOC must preserve energy above 20%% reserve to cover 19:00-22:00 Off-Peak load")
		assert.Empty(t, findDominatedPlanPeriods(plan1820), "Plan at 18:20 must not contain dominated Standby/Export periods")

		// Case 3: At 19:00 (start of Off-Peak at 58% SOC) and 22:00 (start of Super Off-Peak at 20% SOC with 40% reserve):
		// At 19:00 the battery must stay in BatteryModeLoad (not Standby waitingToCharge), and at 22:00 it must charge above 40%.
		t1900 := time.Date(2026, 10, 19, 19, 0, 0, 0, nyLoc)
		var future1900 []types.Price
		for i := 1; i < 28; i++ {
			future1900 = append(future1900, priceForHour(t1900.Add(time.Duration(i)*time.Hour)))
		}
		status1900 := types.SystemStatus{
			Timestamp:             t1900,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    13.5,
			BatterySOC:            58.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    6.6,
			MaxBatteryDischargeKW: 8.0,
			HomeKW:                1.5,
		}
		dec1900, plan1900, err := c.Plan(ctx, status1900, priceForHour(t1900), future1900, buildHistory(t1900), nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, dec1900.Action.BatteryMode,
			"At 19:00 Off-Peak ($0.1048/kWh) with 58%% SOC, battery must supply home load rather than idling in Standby")

		t2200 := time.Date(2026, 10, 19, 22, 0, 0, 0, nyLoc)
		t0500Next := time.Date(2026, 10, 20, 5, 0, 0, 0, nyLoc)
		t0900Next := time.Date(2026, 10, 20, 9, 0, 0, 0, nyLoc)
		maxOvernightEndSOC := 0.0
		socAt0900 := -1.0
		for _, p := range plan1900.Periods {
			if !p.TSStart.Before(t2200) && p.TSStart.Before(t0500Next) && p.BatteryMode == types.BatteryModeChargeAny {
				if p.EndSOC > maxOvernightEndSOC {
					maxOvernightEndSOC = p.EndSOC
				}
			}
			if p.TSStart.Equal(t0900Next) {
				socAt0900 = p.StartSOC
			}
		}
		assert.Greater(t, maxOvernightEndSOC, 50.0,
			"Super Off-Peak (22:00-05:00) must recharge well above the 40%% reserve target to cover morning peak")
		assert.InDelta(t, 20.0, socAt0900, 1.0,
			"By 09:00 (after 07:00-09:00 On-Peak), battery must be at the 20%% Off-Peak reserve to leave 80%% headroom for 09:00-13:00 free Off-Peak solar (+68%% SOC)")

		// Case 4: At 07:00 AM (start of 07:00-09:00 On-Peak window) when 09:00-10:00 Off-Peak has net load
		// before 10:00 solar kicks in (producing a fractional post-peak export floor reqSOC ~ 29.1%),
		// continuous float64 targetSOC ensures BatteryModeExport is deferred to the final 08:40-09:00 peak
		// interval rather than front-loaded to 07:00-07:20 due to math.Ceil integer rounding asymmetry.
		var future0700 []types.Price
		for i := 1; i < 28; i++ {
			future0700 = append(future0700, priceForHour(t0700.Add(time.Duration(i)*time.Hour)))
		}
		historyFractionalFloor := buildHistory(t0700)
		for i := range historyFractionalFloor {
			switch historyFractionalFloor[i].TSHourStart.In(nyLoc).Hour() {
			case 9:
				historyFractionalFloor[i].HomeKWH = 1.67
				historyFractionalFloor[i].SolarKWH = 0.50
			case 11, 12:
				historyFractionalFloor[i].SolarKWH = 6.50
			}
		}
		status0700 := types.SystemStatus{
			Timestamp:             t0700,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    13.5,
			BatterySOC:            58.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    6.6,
			MaxBatteryDischargeKW: 8.0,
			HomeKW:                0.8,
		}
		dec0700, plan0700, err := c.Plan(ctx, status0700, priceForHour(t0700), future0700, historyFractionalFloor, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, dec0700.Action.BatteryMode,
			"At 07:00 On-Peak with 58%% SOC and a fractional post-peak floor, battery must cover load first and defer export to 08:40-09:00")
		t0840 := time.Date(2026, 10, 19, 8, 40, 0, 0, nyLoc)
		found0840Export := false
		for _, p := range plan0700.Periods {
			if !p.TSStart.Before(t0700) && p.TSEnd.Before(t0840.Add(time.Minute)) {
				assert.Equal(t, types.BatteryModeLoad, p.BatteryMode,
					"07:00-08:40 On-Peak intervals must remain in BatteryModeLoad before the 08:40-09:00 export")
			}
			if !p.TSStart.Before(t0840) && p.TSStart.Before(t0900) && p.BatteryMode == types.BatteryModeExport {
				found0840Export = true
			}
		}
		assert.True(t, found0840Export, "Plan at 07:00 must schedule surplus export in the final 08:40-09:00 peak interval")
	})

	t.Run("SmallMinBatteryExportDifference_DoesNotSubsidizeExportBelowPhysicalWear", func(t *testing.T) {
		t.Parallel()

		// Regression test: setting MinBatteryExportDifferenceDollarsPerKWH = $0.01 (smaller than
		// balanced GridChargeDegradation = $0.02) must only act as a candidate gate and must never
		// override physical battery degradation in DP scoring.
		// Scenario:
		// - Hour 0: Export rate = $0.08/kWh, Import rate = $0.20/kWh, Battery at 80% SOC.
		// - Hour 1: Refill import rate = $0.06/kWh (post-RTE cost = $0.06/0.90 = $0.0667/kWh).
		// - Hour 2+: Expensive load ($0.30/kWh) that requires the battery to refill at Hour 1 if it exports at Hour 0.
		// Even though the raw spread ($0.08 - $0.0667 = $0.0133) passes the $0.01 candidate gate,
		// the physical cycle cost ($0.0667 refill + $0.02 grid-charge wear + $0.01 export wear = $0.0967/kWh)
		// exceeds the $0.08/kWh export rate, so the DP must reject the money-losing export+refill cycle.
		loc := time.UTC
		now := time.Date(2026, 10, 19, 18, 0, 0, 0, loc)
		currentPrice := types.Price{
			TSStart:                       now,
			TSEnd:                         now.Add(time.Hour),
			DollarsPerKWH:                 0.20,
			SeparateGenerationCredit:      true,
			GenerationCreditDollarsPerKWH: 0.08,
		}
		var futurePrices []types.Price
		for h := 1; h < 24; h++ {
			ts := now.Add(time.Duration(h) * time.Hour)
			imp := 0.30
			exp := 0.02
			if h == 1 {
				imp = 0.06
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:                       ts,
				TSEnd:                         ts.Add(time.Hour),
				DollarsPerKWH:                 imp,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: exp,
			})
		}
		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			dayStart := now.AddDate(0, 0, -d).Truncate(24 * time.Hour)
			for h := 0; h < 24; h++ {
				ts := dayStart.Add(time.Duration(h) * time.Hour)
				load := 0.5
				if h >= 2 && h <= 8 {
					load = 1.5
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  ts,
					HomeKWH:      load,
					SolarKWH:     0.0,
					TimeLocation: "UTC",
				})
			}
		}

		settings := types.Settings{
			MinBatterySOC:                           20.0,
			GridChargeBatteries:                     true,
			GridExportSolar:                         true,
			GridExportBatteries:                     true,
			OptimizationProfile:                     "balanced",
			MinBatteryExportDifferenceDollarsPerKWH: 0.01,
		}
		status := types.SystemStatus{
			Timestamp:             now,
			TimeLocation:          "UTC",
			BatteryCapacityKWH:    13.5,
			BatterySOC:            80.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
			HomeKW:                0.5,
		}

		c := NewController()
		dec, _, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, dec.Action.BatteryMode,
			"Battery must not export at $0.08/kWh when physical refill + wear costs $0.0967/kWh, even if MinBatteryExportDifference is set to $0.01")
	})

	t.Run("HigherFutureMinSOC_OffersStandbyToAvoidSamePriceRechargeCycle", func(t *testing.T) {
		t.Parallel()

		stepUpSettings := types.Settings{
			GridChargeBatteries: true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       60.0,
		}
		// Step 0 has minSOC = 60% at $0.12/kWh; Step 1 steps up to minSOC = 75% at the same $0.12/kWh import rate.
		// When currentSOC is 72% (above 60% current minSOC, but at/below the 75% upcoming minSOC + reserve buffer),
		// blockActionStandby is allowed and expands to BatteryModeStandby so the battery can hold its 72% SOC rather than
		// discharging to 60% at $0.12/kWh and immediately recharging from the grid at $0.12/kWh in Step 1.
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.12, exportRate: 0.04, loadKWH: 1.5, minSOC: 60.0},
			{durationHours: 1.0, importRate: 0.12, exportRate: 0.04, loadKWH: 1.5, minSOC: 75.0},
		}
		st := planState{
			soc:            72.0,
			energyKWH:      0.72 * 13.5,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		blocks := buildTimelineBlocks(ctx, timeline, nil, stepUpSettings, types.SystemStatus{})
		require.Len(t, blocks, 2)
		allowed := isBlockActionAllowed(0, blocks[0], blockActionChoice{actionType: blockActionStandby}, blocks, st, nil, 0.0, stepUpSettings, types.SystemStatus{}, timeline, nil)
		assert.True(t, allowed, "Must allow blockActionStandby when upcoming minSOC is higher")
		actions := expandBlockAction(blocks[0], blockActionChoice{actionType: blockActionStandby}, st, timeline, nil, stepUpSettings, types.SystemStatus{}, nil)
		require.NotEmpty(t, actions)
		assert.Equal(t, types.BatteryModeStandby, actions[0].batteryMode)
	})

	t.Run("MinArbitrageDifference_GatesArbitrageCandidateWithoutBlockingReserveCharge", func(t *testing.T) {
		t.Parallel()

		// Off-peak $0.10/kWh -> Peak $0.125/kWh: raw spread is $0.125 - $0.10 = $0.025/kWh,
		// which exceeds balanced GridChargeDegradation ($0.02/kWh) but is below MinArbitrageDifference ($0.03/kWh).
		arbSettings := types.Settings{
			GridChargeBatteries:                 true,
			OptimizationProfile:                 "balanced",
			MinBatterySOC:                       20.0,
			MinArbitrageDifferenceDollarsPerKWH: 0.03,
		}
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.125, exportRate: 0.04, loadKWH: 2.0, minSOC: 20.0},
		}
		blocks := buildTimelineBlocks(ctx, timeline, nil, arbSettings, types.SystemStatus{})
		require.Len(t, blocks, 2)

		// Above reserve with sufficient energy to survive peak (50% SOC -> ~30% > 20% minSOC):
		// true arbitrage candidate must be suppressed because spread ($0.025) < $0.03 MinArbitrageDifference.
		aboveReserveState := planState{
			soc:            50.0,
			energyKWH:      0.50 * 13.5,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		allowedAbove := isBlockActionAllowed(0, blocks[0], blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, blocks, aboveReserveState, nil, 0.0, arbSettings, types.SystemStatus{}, timeline, nil)
		assert.False(t, allowedAbove, "Must not allow true arbitrage Charge when battery survives peak and spread is below MinArbitrageDifferenceDollarsPerKWH")

		// Above reserve right now (25% SOC), but 2.5 kWh of cumulative load will drain the battery below 20% minSOC
		// during the upcoming peak block: deficit-avoidance charge must be allowed because spread ($0.025) >= $0.02 GridChargeDegradation.
		deficitAheadState := aboveReserveState
		deficitAheadState.soc = 25.0
		deficitAheadState.energyKWH = 0.25 * 13.5
		allowedDeficitAhead := isBlockActionAllowed(0, blocks[0], blockActionChoice{actionType: blockActionCharge, targetSOC: 100.0}, blocks, deficitAheadState, nil, 0.0, arbSettings, types.SystemStatus{}, timeline, nil)
		assert.True(t, allowedDeficitAhead, "Must allow deficit-avoidance Charge when battery will hit minSOC during peak and spread clears GridChargeDegradation")

		// Below reserve (10% SOC < 20% minSOC): reserve recovery candidate must still be offered
		belowReserveState := aboveReserveState
		belowReserveState.soc = 10.0
		belowReserveState.energyKWH = 0.10 * 13.5
		allowedBelow := isBlockActionAllowed(0, blocks[0], blockActionChoice{actionType: blockActionCharge, targetSOC: 20.0}, blocks, belowReserveState, nil, 0.0, arbSettings, types.SystemStatus{}, timeline, nil)
		assert.True(t, allowedBelow, "Must allow reserve charge when below minSOC regardless of MinArbitrageDifferenceDollarsPerKWH")

		actions := expandBlockAction(blocks[0], blockActionChoice{actionType: blockActionCharge, targetSOC: 20.0}, belowReserveState, timeline, nil, arbSettings, types.SystemStatus{}, nil)
		require.NotEmpty(t, actions)
		assert.Equal(t, types.BatteryModeChargeAny, actions[0].batteryMode)
		assert.InDelta(t, 20.0, actions[0].targetSOC, 0.01)
	})

	t.Run("ReserveDeficitCheaperLookahead_CapsWaitAt40MinutesAndChecksWindowStart", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		c := NewController()
		chiLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// Subcase A: Descending prices every 20 minutes ($0.160 -> $0.120 -> $0.080 -> $0.040).
		// Without a cumulative deficit wait cap, the planner would stay in deficit across all 3 steps (60 minutes).
		// With the 20-minute (chargeCompletionBufferDuration) cap, the plan can wait at most 1 step (20 minutes)
		// before being forced to start charging in step 1.
		t0100 := time.Date(2026, 10, 20, 1, 0, 0, 0, chiLoc)
		settings := types.Settings{
			OptimizationProfile: "balanced",
			MinBatterySOC:       10.0,
			GridChargeBatteries: true,
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 6}}}, MinBatterySOC: 50},
			},
		}
		timeline := []planInterval{
			{startTime: t0100, endTime: t0100.Add(20 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.160, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.160}},
			{startTime: t0100.Add(20 * time.Minute), endTime: t0100.Add(40 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.120, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.120}},
			{startTime: t0100.Add(40 * time.Minute), endTime: t0100.Add(60 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.080, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.080}},
			{startTime: t0100.Add(60 * time.Minute), endTime: t0100.Add(80 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.040, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100.Add(time.Hour), TSEnd: t0100.Add(2 * time.Hour), DollarsPerKWH: 0.040}},
		}
		initial := planState{
			time:           t0100,
			energyKWH:      4.05,
			soc:            30.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		status0100 := types.SystemStatus{
			Timestamp:          t0100,
			TimeLocation:       "America/Chicago",
			BatteryCapacityKWH: 13.5,
			BatterySOC:         30.0,
		}
		vppEvents := extractVPPAnchors(t0100, status0100, settings)
		postHorizon := c.buildPostHorizonValuation(timeline, nil, settings, status0100, nil)
		pathAtStart := c.searchOptimalPlanBlocks(ctx, timeline, initial, vppEvents, postHorizon, settings, status0100, nil)
		if assert.Len(t, pathAtStart.actions, 4) {
			assert.Equal(t, types.BatteryModeChargeAny, pathAtStart.actions[1].batteryMode,
				"After waiting 20 minutes (step 0) for a cheaper rate, step 1 must begin recharging even if later steps are cheaper still")
		}

		// Subcase B: Last action was already in a reserve deficit (SOC 30% < ReserveSOC 50%) and chose not to charge.
		// Even though the next interval is cheaper ($0.040 vs $0.060), computeInitialDeficitWait detects that lastAction
		// already deferred charging during a deficit and forces immediate charging at step 0.
		t0145 := time.Date(2026, 10, 20, 1, 45, 0, 0, chiLoc)
		timeline0145 := []planInterval{
			{startTime: t0145, endTime: t0145.Add(15 * time.Minute), durationHours: 15.0 / 60.0, importRate: 0.060, exportRate: 0.020, loadKWH: 0.25, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.060}},
			{startTime: t0145.Add(15 * time.Minute), endTime: t0145.Add(35 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.040, exportRate: 0.020, loadKWH: 0.30, minSOC: 50.0, price: types.Price{TSStart: t0100.Add(time.Hour), TSEnd: t0100.Add(2 * time.Hour), DollarsPerKWH: 0.040}},
		}
		initial0145 := initial
		initial0145.time = t0145
		status0145 := status0100
		status0145.Timestamp = t0145
		lastActDeficit := &types.Action{
			Timestamp:    t0145.Add(-20 * time.Minute),
			BatteryMode:  types.BatteryModeStandby,
			ReserveSOC:   50.0,
			SystemStatus: types.SystemStatus{BatterySOC: 30.0},
		}
		pathAt0145 := c.searchOptimalPlanBlocks(ctx, timeline0145, initial0145, vppEvents, postHorizon, settings, status0145, lastActDeficit)
		if assert.NotEmpty(t, pathAt0145.actions) {
			assert.Equal(t, types.BatteryModeChargeAny, pathAt0145.actions[0].batteryMode,
				"When lastAction was already in reserve deficit without charging, step 0 must charge immediately rather than delaying again")
		}
	})

	t.Run("VPPDeficitSOC_DirectCalculation", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
		lowState := planState{
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
			time:        now,
		}
		disabledState := lowState
		disabledState.chargingDisabled = true

		// Directly verify calculateVPPDeficitScore returns 0 when gridChargeBatteries is false
		score := calculateVPPDeficitSOC(lowState, lowState, now.Add(time.Hour), 0.95, false)
		assert.Equal(t, 0, score)

		// Directly verify calculateVPPDeficitScore returns 0 when chargingDisabled is true
		scoreDisabled := calculateVPPDeficitSOC(disabledState, lowState, now.Add(time.Hour), 0.95, true)
		assert.Equal(t, 0, scoreDisabled)

		// When grid charging is enabled and battery has a deficit from physically reachable target,
		// deficit score equals ceil(targetSOC - candidateState.soc).
		candStateDeficit := lowState
		candStateDeficit.soc = 20.0
		// Max charge energy in 1 hr: 5 kW * 1h * 0.95 = 4.75 kWh = 35.185% -> reachable target = 55.185%
		// Deficit = 55.185 - 20 = 35.185 -> ceil = 36
		scoreDeficit := calculateVPPDeficitSOC(lowState, candStateDeficit, now.Add(time.Hour), 0.95, true)
		assert.Equal(t, 36, scoreDeficit)
	})

	t.Run("EversourceRate7_SolarChargesOffPeakAndExportsExcessAtEndOfPeakToReserveFloor", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Eversource CT Rate 7:
		// - Off-Peak (20:00 - 12:00): $0.20754/kWh import, $0.20254/kWh export, MinSOC = 50%
		// - On-Peak (12:00 - 20:00): $0.29982/kWh import, $0.29482/kWh export, MinSOC = 25%
		// Grid-charging at Off-Peak to export at On-Peak has a raw spread of
		// $0.29482 - $0.20754 = $0.08728/kWh, which is below MinBatteryExportDifference = $0.09/kWh.
		// Therefore:
		// 1. At 11:00 AM (with 85% SOC from morning solar, above the ~68% SOC needed for peak load + 50% off-peak reserve),
		//    the planner must NOT grid-charge to 100% for battery export, instead charging from free surplus solar.
		// 2. During 12:00 PM - 5:00 PM On-Peak, the planner runs DirectSolarExport (BatteryModeLoad + SolarModeExport).
		// 3. At 7:00 PM (end of On-Peak), the planner exports excess solar-charged battery energy down to the
		//    upcoming 8:00 PM 50% Off-Peak reserve floor (never below 50%, avoiding any 8:00 PM recovery grid charge).
		priceForHour := func(ts time.Time) types.Price {
			h := ts.In(nyLoc).Hour()
			imp := 0.20754
			exp := 0.20254
			periodName := "Off-Peak"
			if h >= 12 && h < 20 {
				imp = 0.29982
				exp = 0.29482
				periodName = "On-Peak"
			}
			return types.Price{
				TSStart:                       ts,
				TSEnd:                         ts.Add(time.Hour),
				DollarsPerKWH:                 imp,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: exp,
				PeriodName:                    periodName,
			}
		}

		t1100 := time.Date(2026, 10, 15, 11, 0, 0, 0, nyLoc)
		var future1100 []types.Price
		for i := 1; i < 28; i++ {
			future1100 = append(future1100, priceForHour(t1100.Add(time.Duration(i)*time.Hour)))
		}
		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			dayStart := time.Date(t1100.Year(), t1100.Month(), t1100.Day(), 0, 0, 0, 0, nyLoc).AddDate(0, 0, -d)
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 8 && h < 17 {
					solar = 6.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  dayStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:      0.5,
					SolarKWH:     solar,
					TimeLocation: "America/New_York",
				})
			}
		}

		settings := types.Settings{
			OptimizationProfile:                     "balanced",
			MinBatterySOC:                           25.0,
			GridChargeBatteries:                     true,
			GridExportSolar:                         true,
			GridExportBatteries:                     true,
			ManageTOUSchedules:                      true,
			MinArbitrageDifferenceDollarsPerKWH:     0.03,
			MinBatteryExportDifferenceDollarsPerKWH: 0.09,
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Off-Peak", MinBatterySOC: 50},
				{UtilityPeriodName: "On-Peak", MinBatterySOC: 25},
			},
		}

		status1100 := types.SystemStatus{
			Timestamp:             t1100,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    30.0,
			BatterySOC:            68.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    16.0,
			MaxBatteryDischargeKW: 20.0,
			HomeKW:                0.5,
			SolarKW:               6.0,
		}

		dec1100, plan1100, err := c.Plan(ctx, status1100, priceForHour(t1100), future1100, history, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.BatteryModeChargeAny, dec1100.Action.BatteryMode,
			"At 11:00 AM with 68%% SOC, must not grid-charge for battery export when spread ($0.0873/kWh) < MinBatteryExportDifference ($0.09/kWh)")

		t1200 := time.Date(2026, 10, 15, 12, 0, 0, 0, nyLoc)
		t1600 := time.Date(2026, 10, 15, 16, 0, 0, 0, nyLoc)
		t1900 := time.Date(2026, 10, 15, 19, 0, 0, 0, nyLoc)
		t2000 := time.Date(2026, 10, 15, 20, 0, 0, 0, nyLoc)
		sawDirectSolarExport := false
		saw7PMBatteryExport := false
		minEndSOCBefore8PM := 100.0
		for _, p := range plan1100.Periods {
			if !p.TSStart.Before(t1200) && p.TSStart.Before(t1600) && p.SolarMode == types.SolarModeExport {
				sawDirectSolarExport = true
			}
			if !p.TSStart.Before(t1900) && p.TSStart.Before(t2000) && p.BatteryMode == types.BatteryModeExport {
				saw7PMBatteryExport = true
			}
			if !p.TSStart.Before(t1900) && !p.TSEnd.After(t2000) && p.EndSOC < minEndSOCBefore8PM {
				minEndSOCBefore8PM = p.EndSOC
			}
			if !p.TSStart.Before(t2000) && p.TSStart.Before(t2000.Add(2*time.Hour)) {
				assert.NotEqual(t, types.BatteryModeChargeAny, p.BatteryMode,
					"Must not trigger an 8:00 PM recovery grid charge after 7:00 PM battery export")
			}
		}
		assert.True(t, sawDirectSolarExport, "Must schedule DirectSolarExport during sunny On-Peak hours")
		assert.True(t, saw7PMBatteryExport, "Must export excess solar-charged battery energy during the final hour of On-Peak (7:00 PM - 8:00 PM)")
		assert.GreaterOrEqual(t, minEndSOCBefore8PM, 49.5, "7:00 PM battery export must stop at the 50%% Off-Peak reserve floor")
	})
}

func BenchmarkSearchOptimalPlanBlocks(b *testing.B) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

	timeline := make([]planInterval, 24)
	for i := 0; i < 24; i++ {
		tStep := now.Add(time.Duration(i) * time.Hour)
		rate := 0.08
		if i >= 9 && i <= 12 {
			rate = 0.45 // peak
		}
		timeline[i] = planInterval{
			startTime:     tStep,
			endTime:       tStep.Add(time.Hour),
			durationHours: 1.0,
			importRate:    rate,
			exportRate:    0.05,
			loadKWH:       1.5,
			solarKWH:      0.0,
			minSOC:        20.0,
		}
	}

	initialState := planState{
		energyKWH:   13.5 * 0.5,
		soc:         50.0,
		capacityKWH: 13.5,
	}

	status := types.SystemStatus{
		BatteryCapacityKWH: 13.5,
		BatterySOC:         50.0,
	}

	settings := types.Settings{
		MinBatterySOC:       20,
		GridChargeBatteries: true,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.searchOptimalPlanBlocks(ctx, timeline, initialState, nil, postHorizonValuation{}, settings, status, nil)
	}
}
