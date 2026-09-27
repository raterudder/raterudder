package controller

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildPlanningTimeline tests the timeline construction, midnight synthesis,
// price alignment, and load/solar model integration.
func TestBuildPlanningTimeline(t *testing.T) {
	t.Parallel()

	chicagoLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	now := time.Date(2026, 6, 15, 10, 15, 0, 0, chicagoLoc)

	c := NewController()
	ctx := context.Background()

	t.Run("ContiguousHourlyPrices_BuildsFullHorizon", func(t *testing.T) {
		t.Parallel()

		currentPrice := types.Price{
			TSStart:              time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:                time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH:        0.08,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 11; h < 34; h++ {
			day := 15
			hour := h
			if hour >= 24 {
				day = 16
				hour -= 24
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, day, hour, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, day, hour+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
		}

		timeline, simParams, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, settings, status)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)
		assert.Equal(t, "none", simParams.DetectedShift)

		// With >40m interval splitting, now (10:15) to top of hour (11:00) is 45m (> 40m), splitting at 10:30
		assert.Equal(t, now, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 10, 30, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 15.0/60.0, timeline[0].durationHours, 0.01)

		// Subsequent intervals should be 30-minute blocks (:30 and :00)
		if len(timeline) > 1 {
			assert.InDelta(t, 30.0/60.0, timeline[1].durationHours, 0.01)
			assert.Equal(t, time.Date(2026, 6, 15, 10, 30, 0, 0, chicagoLoc), timeline[1].startTime)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc), timeline[1].endTime)
		}
		if len(timeline) > 2 {
			assert.InDelta(t, 30.0/60.0, timeline[2].durationHours, 0.01)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc), timeline[2].startTime)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 30, 0, 0, chicagoLoc), timeline[2].endTime)
		}
	})

	t.Run("RateSplitsHourDownTheMiddle_BuildsTwo30MinuteIntervals", func(t *testing.T) {
		t.Parallel()

		startHour := time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc)
		// 14:00 to 14:30 is $0.10, 14:30 to 15:00 is $0.40 (rate splits hour down the middle)
		halfHourPrice1 := types.Price{
			TSStart:       startHour,
			TSEnd:         startHour.Add(30 * time.Minute),
			DollarsPerKWH: 0.10,
		}
		future30MinPrices := []types.Price{
			{
				TSStart:       startHour.Add(30 * time.Minute),
				TSEnd:         startHour.Add(60 * time.Minute),
				DollarsPerKWH: 0.40,
			},
			{
				TSStart:       startHour.Add(60 * time.Minute),
				TSEnd:         startHour.Add(5 * time.Hour),
				DollarsPerKWH: 0.15,
			},
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, startHour, halfHourPrice1, future30MinPrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
		require.NoError(t, err)
		require.True(t, len(timeline) >= 2)

		// Hour 14:00 must be split into exactly 2 thirty-minute evaluations
		assert.Equal(t, startHour, timeline[0].startTime)
		assert.Equal(t, startHour.Add(30*time.Minute), timeline[0].endTime)
		assert.InDelta(t, 0.5, timeline[0].durationHours, 0.01)

		assert.Equal(t, startHour.Add(30*time.Minute), timeline[1].startTime)
		assert.Equal(t, startHour.Add(60*time.Minute), timeline[1].endTime)
		assert.InDelta(t, 0.5, timeline[1].durationHours, 0.01)
	})

	t.Run("RateSplitsAt45Minutes_SplitsLongSegment", func(t *testing.T) {
		t.Parallel()

		startHour := time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc)
		// Rate splits at :45 (14:00 to 14:45 is $0.10, 14:45 to 15:00 is $0.50)
		rate45Price := types.Price{
			TSStart:       startHour,
			TSEnd:         startHour.Add(45 * time.Minute),
			DollarsPerKWH: 0.10,
		}
		futurePrices := []types.Price{
			{
				TSStart:       startHour.Add(45 * time.Minute),
				TSEnd:         startHour.Add(60 * time.Minute),
				DollarsPerKWH: 0.50,
			},
			{
				TSStart:       startHour.Add(60 * time.Minute),
				TSEnd:         startHour.Add(5 * time.Hour),
				DollarsPerKWH: 0.15,
			},
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, startHour, rate45Price, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
		require.NoError(t, err)
		require.True(t, len(timeline) >= 3)

		// 14:00 to 14:45 is 45m (> 40m), so it splits at 14:30 into 30m and 15m intervals
		assert.Equal(t, startHour, timeline[0].startTime)
		assert.Equal(t, startHour.Add(30*time.Minute), timeline[0].endTime)
		assert.InDelta(t, 0.5, timeline[0].durationHours, 0.01)

		assert.Equal(t, startHour.Add(30*time.Minute), timeline[1].startTime)
		assert.Equal(t, startHour.Add(45*time.Minute), timeline[1].endTime)
		assert.InDelta(t, 15.0/60.0, timeline[1].durationHours, 0.01)

		assert.Equal(t, startHour.Add(45*time.Minute), timeline[2].startTime)
		assert.Equal(t, startHour.Add(60*time.Minute), timeline[2].endTime)
		assert.InDelta(t, 15.0/60.0, timeline[2].durationHours, 0.01)
	})

	t.Run("ShortInterval_MergesWhenSamePriceAndUnder40Minutes", func(t *testing.T) {
		t.Parallel()

		// Run at 10:55 (5m before hour top) with uniform price continuing through 18:00
		runTime := time.Date(2026, 6, 15, 10, 55, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 18, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.10,
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, nil, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// 10:55 to 11:00 is 5m (< 10m). Price continues to 14:00, so it merges with 11:00-11:30 -> 10:55 to 11:30 (35m <= 40m)
		assert.Equal(t, runTime, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 11, 30, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 35.0/60.0, timeline[0].durationHours, 0.01)
	})

	t.Run("ComEdMidnightTruncation_PlansOverAvailableHours", func(t *testing.T) {
		t.Parallel()

		// Prices truncate at midnight (10:15 to midnight = 13.75 hours ahead, >= 4h minimum)
		currentPrice := types.Price{
			TSStart:              time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:                time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH:        0.07,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 11; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.09,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, settings, status)
		require.NoError(t, err)

		// Timeline should plan over available hours up to midnight without synthesizing fake future prices
		lastInterval := timeline[len(timeline)-1]
		assert.Equal(t, time.Date(2026, 6, 16, 0, 0, 0, 0, chicagoLoc), lastInterval.endTime,
			"timeline should end at midnight without synthesizing past known prices")
	})

	t.Run("InsufficientHorizon_ErrorsOut", func(t *testing.T) {
		t.Parallel()

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}
		settings := types.Settings{
			MinBatterySOC: 20,
		}

		// Only 2 hours of pricing data (now 10:15 to 12:00) < 4h minimum
		shortPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.10,
		}
		shortFuture := []types.Price{
			{
				TSStart:       time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, 12, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.12,
			},
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, now, shortPrice, shortFuture, nil, nil, settings, status)
		assert.Error(t, err)
		assert.Nil(t, timeline)
		assert.ErrorContains(t, err, "insufficient pricing horizon")
	})

	t.Run("MissingPricesFallback_ErrorsOut", func(t *testing.T) {
		t.Parallel()

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}
		settings := types.Settings{
			MinBatterySOC: 20,
		}

		// Empty prices
		timeline, _, err := c.buildPlanningTimeline(ctx, now, types.Price{}, nil, nil, nil, settings, status)
		assert.Error(t, err)
		assert.Nil(t, timeline)
		assert.ErrorContains(t, err, "insufficient pricing horizon")
	})

	t.Run("VPPEventBoundary_SplitsIntervalsCleanly", func(t *testing.T) {
		t.Parallel()

		currentPrice := types.Price{
			TSStart:              time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc),
			TSEnd:                time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
			DollarsPerKWH:        0.10,
			GridUseDollarsPerKWH: 0.04,
		}
		futurePrices := []types.Price{
			{
				TSStart:              time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, 16, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.12,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              time.Date(2026, 6, 15, 16, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, 17, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.14,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              time.Date(2026, 6, 15, 17, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, 18, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.14,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              time.Date(2026, 6, 15, 18, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, 19, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
		}

		vppStart := time.Date(2026, 6, 15, 16, 30, 0, 0, chicagoLoc)
		vppEnd := time.Date(2026, 6, 15, 18, 30, 0, 0, chicagoLoc)
		status := types.SystemStatus{
			Timestamp:          time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc),
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
			VPPEvents: []types.VPPEvent{
				{
					TSStart: vppStart,
					TSEnd:   vppEnd,
					VPPSoc:  20,
				},
			},
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, status.Timestamp, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
		require.NoError(t, err)

		// Verify that an interval boundary cleanly matches VPP prep deadline (14:30) and VPP start (16:30)
		has1430Boundary := false
		has1630Boundary := false
		deadline1430 := vppStart.Add(-2 * time.Hour)
		for _, interval := range timeline {
			if interval.endTime.Equal(deadline1430) {
				has1430Boundary = true
			}
			if interval.endTime.Equal(vppStart) {
				has1630Boundary = true
			}
		}
		assert.True(t, has1430Boundary, "Timeline must split at VPP prep deadline (14:30)")
		assert.True(t, has1630Boundary, "Timeline must split at VPP event start (16:30)")
	})

	t.Run("OpenEndedPrice_HandlesGracefullyWithoutInfiniteLoop", func(t *testing.T) {
		t.Parallel()

		// Open-ended ongoing price with zero TSEnd, with future prices spanning >= 4 hours
		currentPrice := types.Price{
			TSStart:              time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:                time.Time{}, // Open-ended
			DollarsPerKWH:        0.10,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 11; h < 16; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
		require.NoError(t, err)
		assert.NotEmpty(t, timeline)
		assert.LessOrEqual(t, len(timeline), 30, "Timeline intervals should be bounded")
	})

	t.Run("OpenEndedFuturePrices_NotDropped", func(t *testing.T) {
		t.Parallel()

		currentPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.12,
		}

		openEndedFuture := []types.Price{
			{
				TSStart:       time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Time{}, // Open-ended future price
				DollarsPerKWH: 0.18,
			},
			{
				TSStart:       time.Date(2026, 6, 15, 12, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, 13, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.18,
			},
			{
				TSStart:       time.Date(2026, 6, 15, 13, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.18,
			},
			{
				TSStart:       time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.18,
			},
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, openEndedFuture, nil, nil, types.Settings{MinBatterySOC: 20}, status)
		require.NoError(t, err)
		// Interval at 11:00 should have the open-ended future price ($0.18)
		var intervalAt11 *planInterval
		for i := range timeline {
			if timeline[i].startTime.Equal(time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc)) {
				intervalAt11 = &timeline[i]
				break
			}
		}
		require.NotNil(t, intervalAt11, "Interval at 11:00 must exist")
		assert.Equal(t, 0.18, intervalAt11.importRate, "Open-ended future price must not be dropped by timeline builder")
	})

	t.Run("ZeroPrice_PreservedWithoutFallback", func(t *testing.T) {
		t.Parallel()

		zeroPrice := types.Price{
			TSStart:              time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:                time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH:        0.0,
			GridUseDollarsPerKWH: 0.0,
		}

		var futurePrices []types.Price
		for h := 11; h < 15; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.0,
				GridUseDollarsPerKWH: 0.0,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		timeline, _, err := c.buildPlanningTimeline(ctx, now, zeroPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)
		assert.Equal(t, 0.0, timeline[0].importRate, "Zero price (e.g. Texas Free Nights) must be preserved as 0.0 without artificial fallback")
		assert.Equal(t, 0.0, timeline[1].importRate, "Future zero price must also be preserved as 0.0")
	})

	t.Run("ExportCredit_SameForSolarAndBattery_NotZeroedForNEM", func(t *testing.T) {
		t.Parallel()

		nem2Settings := types.Settings{
			GridExportBatteries: true,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		}

		peakPrice := types.Price{
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
			DollarsPerKWH: 0.65, // Peak retail rate
		}

		credit := c.calculateExportCredit(peakPrice, nem2Settings, nil)
		assert.Equal(t, 0.65, credit, "Export credit must reflect tariff value and not be zeroed out for NEM")

		// BatteryModeExport is gated when ManageTOUSchedules is false
		candidates := c.generateActionCandidates(ctx, 0, planInterval{
			startTime:  now,
			endTime:    now.Add(time.Hour),
			importRate: 0.65,
			exportRate: credit,
			minSOC:     20,
		}, nil, planState{energyKWH: 10.0, soc: 80.0, capacityKWH: 13.5}, planningAnchors{}, nem2Settings, types.SystemStatus{BatteryCapacityKWH: 13.5, BatterySOC: 80}, nil, precedingAction{})

		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode, "BatteryModeExport is gated when ManageTOUSchedules is false")
		}
	})

	t.Run("NetMeteringRider_NegativeAdjustment_PreservesNegativeRate", func(t *testing.T) {
		t.Parallel()

		nemSettings := types.Settings{
			GridExportSolar: true,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
		}

		riderPrice := types.Price{
			TSStart:                           now,
			TSEnd:                             now.Add(time.Hour),
			DollarsPerKWH:                     0.02,
			GridUseDollarsPerKWH:              0.01,
			GenerationAdjustmentDollarsPerKWH: -0.0402, // Negative rider
		}

		credit := c.calculateExportCredit(riderPrice, nemSettings, []types.Price{riderPrice})
		assert.InDelta(t, -0.0102, credit, 0.0001, "Tariff charging for surplus export must preserve negative export credit to enable curtailment")
	})
}

// TestDetectPlanningAnchors tests the detection of VPP deadlines, negative prices, and super-spikes.
func TestDetectPlanningAnchors(t *testing.T) {
	t.Parallel()

	chicagoLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	now := time.Date(2026, 6, 15, 8, 0, 0, 0, chicagoLoc)
	c := NewController()

	t.Run("MultipleVPPEventsInHorizon", func(t *testing.T) {
		t.Parallel()

		vppStart1 := time.Date(2026, 6, 15, 16, 0, 0, 0, chicagoLoc)
		vppEnd1 := time.Date(2026, 6, 15, 19, 0, 0, 0, chicagoLoc)

		vppStart2 := time.Date(2026, 6, 15, 20, 0, 0, 0, chicagoLoc)
		vppEnd2 := time.Date(2026, 6, 15, 22, 0, 0, 0, chicagoLoc)

		status := types.SystemStatus{
			Timestamp: now,
			VPPEvents: []types.VPPEvent{
				{
					TSStart:   vppStart1,
					TSEnd:     vppEnd1,
					VPPSoc:    20,
					Mandatory: false,
				},
				{
					TSStart:   vppStart2,
					TSEnd:     vppEnd2,
					VPPSoc:    25,
					Mandatory: true,
				},
			},
		}

		timeline := []planInterval{
			{startTime: now, endTime: vppEnd2.Add(2 * time.Hour)},
		}

		anchors := c.detectPlanningAnchors(timeline, nil, status, types.Settings{})
		require.Len(t, anchors.vppEvents, 2)

		// First VPP is optional, so deadline is event start (minus buffer)
		assert.Equal(t, vppStart1, anchors.vppEvents[0].deadline)
		assert.Equal(t, 20.0, anchors.vppEvents[0].vppSoc)
		assert.False(t, anchors.vppEvents[0].mandatory)

		// Second VPP is mandatory, so deadline is 2 hours before event start (vppStandbyLeadTime)
		assert.Equal(t, vppStart2.Add(-2*time.Hour), anchors.vppEvents[1].deadline)
		assert.Equal(t, 25.0, anchors.vppEvents[1].vppSoc)
		assert.True(t, anchors.vppEvents[1].mandatory)
	})

	t.Run("KnownPostHorizonRate_UsesFuturePriceAfterHorizon", func(t *testing.T) {
		t.Parallel()

		tEnd := now.Add(6 * time.Hour)
		timeline := []planInterval{
			{startTime: now, endTime: tEnd, importRate: 0.10},
		}

		futurePrices := []types.Price{
			{
				TSStart:              tEnd,
				TSEnd:                tEnd.Add(time.Hour),
				DollarsPerKWH:        0.22,
				GridUseDollarsPerKWH: 0.03,
			},
		}

		anchors := c.detectPlanningAnchors(timeline, futurePrices, types.SystemStatus{Timestamp: now}, types.Settings{})
		assert.InDelta(t, 0.25, anchors.knownPostHorizonRate, 0.001, "must use real future price after horizon")
	})

	t.Run("KnownPostHorizonRate_FallsBackToLatestTimelineRate", func(t *testing.T) {
		t.Parallel()

		tEnd := now.Add(6 * time.Hour)
		timeline := []planInterval{
			{startTime: now, endTime: now.Add(3 * time.Hour), importRate: 0.10},
			{startTime: now.Add(3 * time.Hour), endTime: tEnd, importRate: 0.14},
		}

		// No future prices after horizon
		anchors := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: now}, types.Settings{})
		assert.InDelta(t, 0.14, anchors.knownPostHorizonRate, 0.001, "must fall back to latest timeline rate")
	})
}

// TestGenerateActionCandidates tests pruning rules (negative price, VPP deadlines, reserve floors, headroom).
func TestGenerateActionCandidates(t *testing.T) {
	t.Parallel()

	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	interval := planInterval{
		startTime:     now,
		endTime:       now.Add(time.Hour),
		durationHours: 1.0,
		importRate:    0.12,
		exportRate:    0.06,
		minSOC:        20,
	}

	settings := types.Settings{
		MinBatterySOC:       20,
		GridChargeBatteries: true,
		GridExportSolar:     true,
	}

	status := types.SystemStatus{
		Timestamp:          now,
		BatteryCapacityKWH: 13.5,
		BatterySOC:         50,
	}

	t.Run("NegativePrice_OffersChargeAnyAndStandby", func(t *testing.T) {
		t.Parallel()

		negInterval := interval
		negInterval.importRate = -0.02

		candidates := c.generateActionCandidates(ctx, 0, negInterval, nil, planState{energyKWH: 6.75, soc: 50}, planningAnchors{}, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 2)
		assert.Equal(t, types.BatteryModeChargeAny, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, candidates[0].reason)
		require.NotNil(t, candidates[0].logFn)
		candidates[0].logFn(ctx, true)

		assert.Equal(t, types.BatteryModeStandby, candidates[1].batteryMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, candidates[1].reason)
		require.NotNil(t, candidates[1].logFn)
		candidates[1].logFn(ctx, false)
	})

	t.Run("ForceChargeThreshold_EnforcesChargeAny", func(t *testing.T) {
		t.Parallel()

		forceSettings := settings
		forceSettings.GridChargeBatteries = true
		forceSettings.AlwaysChargeUnderDollarsPerKWH = 0.09

		cheapInterval := interval
		cheapInterval.importRate = 0.074

		candidates := c.generateActionCandidates(ctx, 0, cheapInterval, nil, planState{energyKWH: 6.75, soc: 50}, planningAnchors{}, forceSettings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeChargeAny, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, candidates[0].reason)
	})

	t.Run("NegativeForceChargeThreshold_EnforcesChargeAny", func(t *testing.T) {
		t.Parallel()

		forceSettings := settings
		forceSettings.GridChargeBatteries = true
		forceSettings.AlwaysChargeUnderDollarsPerKWH = -0.01

		// Price is -0.02, which is <= -0.01
		deeplyNegativeInterval := interval
		deeplyNegativeInterval.importRate = -0.02

		candidates := c.generateActionCandidates(ctx, 0, deeplyNegativeInterval, nil, planState{energyKWH: 6.75, soc: 50}, planningAnchors{}, forceSettings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeChargeAny, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, candidates[0].reason)
	})

	t.Run("VPPActive_EnforcesDischargeToVPPSoc", func(t *testing.T) {
		t.Parallel()

		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(-30 * time.Minute),
					eventEnd:   now.Add(2 * time.Hour),
					vppSoc:     20,
				},
			},
		}

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, planState{energyKWH: 10.0, soc: 74}, anchors, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.ActionReasonVPPActive, candidates[0].reason)
	})

	t.Run("VPPStandbyLock_PrunesDischarge2HoursPrior", func(t *testing.T) {
		t.Parallel()

		vppEventStart := now.Add(90 * time.Minute) // In 1.5 hours (< 2h lead time)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppEventStart,
					deadline:   vppEventStart.Add(-2 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
		}

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, planState{energyKWH: 13.5, soc: 100}, anchors, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPPrep, candidates[0].reason)
	})

	t.Run("HeadroomInsufficient_PrunesChargeAny", func(t *testing.T) {
		t.Parallel()

		// Battery is at 99% SOC (headroom = 0.135 kWh, less than 0.3 kWh required to start)
		stateAlmostFull := planState{
			energyKWH: 13.5 * 0.99,
			soc:       99,
		}

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, stateAlmostFull, planningAnchors{}, settings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode,
				"ChargeAny should be pruned when starting with < 0.3 kWh headroom")
		}
	})

	t.Run("HeadroomHysteresis_AllowsContinuingCharge", func(t *testing.T) {
		t.Parallel()

		// Battery is at 98.5% SOC (headroom = 0.2 kWh, > 0.1 kWh continue threshold)
		stateContinuing := planState{
			energyKWH: 13.5 * 0.985,
			soc:       98.5,
		}

		lastActionCharging := precedingAction{
			BatteryMode: types.BatteryModeChargeAny,
		}

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, stateContinuing, planningAnchors{}, settings, status, nil, lastActionCharging)
		hasCharge := false
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeChargeAny {
				hasCharge = true
				break
			}
		}
		assert.True(t, hasCharge, "ChargeAny should be permitted when continuing active charge with > 0.1 kWh headroom")
	})

	t.Run("HeadroomHysteresis_FutureStep_RequiresParentCharging", func(t *testing.T) {
		t.Parallel()

		// Headroom is ~0.2025 kWh (between continue threshold 0.1 kWh and start threshold 0.6 kWh)
		stateMarginal := planState{
			capacityKWH: 13.5,
			energyKWH:   13.5 * 0.985,
			soc:         98.5,
		}

		// When parent in trajectory was charging (prevAction = ChargeAny), continuing charge is permitted at stepIdx > 0
		prevActionCharging := precedingAction{BatteryMode: types.BatteryModeChargeAny}
		candidatesContinue := c.generateActionCandidates(ctx, 1, interval, nil, stateMarginal, planningAnchors{}, settings, status, nil, prevActionCharging)
		hasChargeContinue := false
		for _, cand := range candidatesContinue {
			if cand.batteryMode == types.BatteryModeChargeAny {
				hasChargeContinue = true
				break
			}
		}
		assert.True(t, hasChargeContinue, "ChargeAny should be permitted at stepIdx > 0 if parent was charging")

		// When parent in trajectory was NOT charging (prevAction = Load), starting charge is pruned due to insufficient start headroom
		prevActionIdle := precedingAction{BatteryMode: types.BatteryModeLoad}
		candidatesStart := c.generateActionCandidates(ctx, 1, interval, nil, stateMarginal, planningAnchors{}, settings, status, nil, prevActionIdle)
		hasChargeStart := false
		for _, cand := range candidatesStart {
			if cand.batteryMode == types.BatteryModeChargeAny {
				hasChargeStart = true
				break
			}
		}
		assert.False(t, hasChargeStart, "ChargeAny should NOT be permitted at stepIdx > 0 if parent was not charging and headroom < start threshold")
	})

	t.Run("PrecedingStandby_MaintainsStandbyCandidate", func(t *testing.T) {
		t.Parallel()

		// Flat pricing timeline (future rates equal current rate), no solar refill, no VPP.
		flatTimeline := []planInterval{
			interval,
			{
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    interval.importRate,
				exportRate:    interval.exportRate,
				minSOC:        interval.minSOC,
			},
		}

		stateNormal := planState{
			capacityKWH: 13.5,
			energyKWH:   13.5 * 0.50,
			soc:         50.0,
		}

		// Preceding action Load: Standby is not offered under flat rates without peak/refill/VPP
		candsFromLoad := c.generateActionCandidates(ctx, 0, flatTimeline[0], flatTimeline, stateNormal, planningAnchors{}, settings, status, nil, precedingAction{BatteryMode: types.BatteryModeLoad})
		hasStandbyFromLoad := false
		for _, cand := range candsFromLoad {
			if cand.batteryMode == types.BatteryModeStandby {
				hasStandbyFromLoad = true
				break
			}
		}
		assert.False(t, hasStandbyFromLoad, "Standby should not be offered under flat rates when not already in standby")

		// Preceding action Standby: Standby IS offered to maintain standby mode
		candsFromStandby := c.generateActionCandidates(ctx, 0, flatTimeline[0], flatTimeline, stateNormal, planningAnchors{}, settings, status, nil, precedingAction{BatteryMode: types.BatteryModeStandby})
		hasStandbyFromStandby := false
		for _, cand := range candsFromStandby {
			if cand.batteryMode == types.BatteryModeStandby {
				hasStandbyFromStandby = true
				assert.Equal(t, types.ActionReasonHoldSimilarPrice, cand.reason)
				assert.Equal(t, "Maintaining standby mode.", cand.description)
				break
			}
		}
		assert.True(t, hasStandbyFromStandby, "Standby should be offered when preceding action was standby")
	})

	t.Run("ReserveFloor_PrunesActiveDischarge", func(t *testing.T) {
		t.Parallel()

		// Battery at 18% SOC (below MinSOC 20% + buffer)
		stateAtReserve := planState{
			energyKWH: 13.5 * 0.18,
			soc:       18,
		}

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, stateAtReserve, planningAnchors{}, settings, status, nil, precedingAction{})
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeLoad {
				assert.Equal(t, types.ActionReasonBatteryAtReserve, cand.reason,
					"Load mode at reserve must declare BatteryAtReserve passthrough")
			}
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode,
				"BatteryModeExport must be pruned when below reserve")
		}
	})

	t.Run("ActiveEVCharging_EnforcesStandbyAtT0", func(t *testing.T) {
		t.Parallel()

		evSettings := settings
		evSettings.EVChargingPeriods = []types.TimePeriod{
			{Start: now.Add(-time.Hour), End: now.Add(4 * time.Hour)},
		}

		// Current home load shows EV charging active (8.5 kW)
		evStatus := status
		evStatus.HomeKW = 8.5

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, evSettings, evStatus, nil, precedingAction{})
		require.NotEmpty(t, candidates)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, candidates[0].reason)
		require.NotNil(t, candidates[0].logFn)
		candidates[0].logFn(ctx, true)
		// Discharging modes (Load, Export) must be strictly pruned during EV charging
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeLoad, cand.batteryMode)
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode)
		}
	})

	t.Run("EVChargingPeriod_NoActiveSession_AllowsNormalSelfConsumption", func(t *testing.T) {
		t.Parallel()

		evSettings := settings
		evSettings.EVChargingPeriods = []types.TimePeriod{
			{Start: now.Add(-time.Hour), End: now.Add(4 * time.Hour)},
		}

		// Current home load is baseline (1.2 kW, no EV charging active)
		idleStatus := status
		idleStatus.HomeKW = 1.2

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, evSettings, idleStatus, nil, precedingAction{})
		require.NotEmpty(t, candidates)

		// On nights without active EV charging, battery should cover household baseline load
		hasLoad := false
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeLoad {
				hasLoad = true
				break
			}
		}
		assert.True(t, hasLoad, "Must offer BatteryModeLoad on nights when vehicle is not charging")
	})

	t.Run("EVChargingPeriod_FutureSteps_NotForcedToStandby", func(t *testing.T) {
		t.Parallel()

		evSettings := settings
		evSettings.EVChargingPeriods = []types.TimePeriod{
			{Start: now.Add(-time.Hour), End: now.Add(4 * time.Hour)},
		}

		// Even if step 0 has active EV charging, step 1 (future step) must not be unconditionally forced
		// to Standby because user might not charge across future intervals.
		evStatus := status
		evStatus.HomeKW = 8.5

		candidatesFuture := c.generateActionCandidates(ctx, 1, interval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, evSettings, evStatus, nil, precedingAction{})
		require.NotEmpty(t, candidatesFuture)

		hasLoad := false
		for _, cand := range candidatesFuture {
			if cand.batteryMode == types.BatteryModeLoad {
				hasLoad = true
				break
			}
		}
		assert.True(t, hasLoad, "Future intervals (stepIdx > 0) must explore normal load coverage rather than assuming charging continues forever")
	})

	t.Run("NegativePrice_RespectsGridChargeDisabled", func(t *testing.T) {
		t.Parallel()

		negInterval := interval
		negInterval.importRate = -0.02

		noChargeSettings := settings
		noChargeSettings.GridChargeBatteries = false

		candidates := c.generateActionCandidates(ctx, 0, negInterval, nil, planState{energyKWH: 6.75, soc: 50}, planningAnchors{}, noChargeSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode,
				"ChargeAny must not be scheduled when GridChargeBatteries is disabled")
		}
	})

	t.Run("VPPPrep_RequiresChargeAnyWhenUndercharged", func(t *testing.T) {
		t.Parallel()

		vppEventStart := now.Add(90 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppEventStart,
					deadline:   vppEventStart.Add(-2 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.15,
		}

		// Battery is only at 60% SOC during prep window: hardware takes over and force charges to 100%.
		// Standby must not be offered.
		candidates := c.generateActionCandidates(ctx, 0, interval, nil, planState{energyKWH: 8.1, soc: 60}, anchors, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeChargeAny, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPPrep, candidates[0].reason)
		assert.Equal(t, 100, candidates[0].targetSOC)
	})

	t.Run("BatteryExportDump_HurdleRequiresRechargeCostPlusMargin", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true
		exportSettings.MinBatteryExportDifferenceDollarsPerKWH = 0.07

		anchors := planningAnchors{
			knownPostHorizonRate: 0.04, // recharge cost = 0.04 / 0.85 = 0.047
		}

		// Battery at 80% SOC (> effective reserve + 5%)
		stateHigh := planState{energyKWH: 10.8, soc: 80}

		// Low export rate ($0.08) < 0.047 + 0.07 ($0.117): should NOT offer battery export
		lowExportInterval := interval
		lowExportInterval.exportRate = 0.08
		candidatesLow := c.generateActionCandidates(ctx, 0, lowExportInterval, nil, stateHigh, anchors, exportSettings, status, nil, precedingAction{})
		for _, cand := range candidatesLow {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode)
		}

		// High export rate ($0.25) > 0.117: MUST offer battery export dump
		highExportInterval := interval
		highExportInterval.exportRate = 0.25
		candidatesHigh := c.generateActionCandidates(ctx, 0, highExportInterval, nil, stateHigh, anchors, exportSettings, status, nil, precedingAction{})
		hasExport := false
		for _, cand := range candidatesHigh {
			if cand.batteryMode == types.BatteryModeExport {
				hasExport = true
				break
			}
		}
		assert.True(t, hasExport, "Must offer battery export dump when export rate clears degradation hurdle")
	})

	t.Run("GridExportSolarDisabled_UsesSolarModeNoExport", func(t *testing.T) {
		t.Parallel()

		noExportSettings := settings
		noExportSettings.GridExportSolar = false

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, noExportSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.Equal(t, types.SolarModeNoExport, cand.solarMode,
				"All candidates must specify SolarModeNoExport when GridExportSolar is false")
		}
	})

	t.Run("NegativePrice_BatteryFull_OffersStandbyOnly", func(t *testing.T) {
		t.Parallel()

		negInterval := interval
		negInterval.importRate = -0.02
		fullState := planState{energyKWH: 13.5, soc: 100.0, capacityKWH: 13.5}

		candidates := c.generateActionCandidates(ctx, 0, negInterval, nil, fullState, planningAnchors{}, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
		assert.NotEqual(t, types.BatteryModeLoad, candidates[0].batteryMode, "Battery must not discharge into home when grid price is negative")
	})

	t.Run("NegativePrice_BMSChargingDisabled_OffersStandbyOnly", func(t *testing.T) {
		t.Parallel()

		negInterval := interval
		negInterval.importRate = -0.02
		bmsStatus := status
		bmsStatus.BatteryChargingDisabled = true

		candidates := c.generateActionCandidates(ctx, 0, negInterval, nil, planState{energyKWH: 6.75, soc: 50.0}, planningAnchors{}, settings, bmsStatus, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
	})

	t.Run("VPPActive_BatteryAtTargetSOC_SwitchesToStandby", func(t *testing.T) {
		t.Parallel()

		vppStart := now.Add(-10 * time.Minute)
		vppEnd := now.Add(50 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppEnd,
					vppSoc:     30.0,
					mandatory:  true,
				},
			},
		}

		vppInterval := interval
		vppInterval.startTime = now
		vppInterval.endTime = now.Add(time.Hour)

		// Battery already at target SOC 30% for mandatory event
		candidates := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 4.05, soc: 30.0}, anchors, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
		assert.Equal(t, 0, candidates[0].targetSOC)
		assert.NotEqual(t, types.SolarModeExport, candidates[0].solarMode)
	})

	t.Run("VPPActive_NonMandatory_ContinuesDischargeDownToUserReserve", func(t *testing.T) {
		t.Parallel()

		vppStart := now.Add(-10 * time.Minute)
		vppEnd := now.Add(50 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppEnd,
					vppSoc:     30.0,
					mandatory:  false, // Non-mandatory (paid per kW)
				},
			},
		}

		vppInterval := interval
		vppInterval.startTime = now
		vppInterval.endTime = now.Add(time.Hour)

		exportSettings := settings
		exportSettings.MinBatterySOC = 20
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true

		// Battery at 30% SOC (at vppSoc, but above user reserve of 20%): must keep exporting
		candidates := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 4.05, soc: 30.0}, anchors, exportSettings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeExport, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPActive, candidates[0].reason)
		assert.Equal(t, 20, candidates[0].targetSOC)
		assert.NotEqual(t, types.SolarModeExport, candidates[0].solarMode)
	})

	t.Run("VPPActive_Mandatory_DrainsToVPPSocEvenIfBelowUserReserve", func(t *testing.T) {
		t.Parallel()

		vppStart := now.Add(-10 * time.Minute)
		vppEnd := now.Add(50 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppEnd,
					vppSoc:     5.0,
					mandatory:  true, // Mandatory utility dispatch: drains to 5% regardless of reserve
				},
			},
		}

		vppInterval := interval
		vppInterval.startTime = now
		vppInterval.endTime = now.Add(time.Hour)

		userReserveSettings := settings
		userReserveSettings.MinBatterySOC = 20
		userReserveSettings.ManageTOUSchedules = true
		userReserveSettings.GridExportBatteries = true

		// 1. Battery at 15% SOC (below user reserve of 20%, but above mandatory vppSoc of 5%):
		// Must continue discharging/exporting down to mandatory 5% target SOC.
		candidates15 := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 13.5 * 0.15, soc: 15.0}, anchors, userReserveSettings, status, nil, precedingAction{})
		require.Len(t, candidates15, 1)
		assert.Equal(t, types.BatteryModeExport, candidates15[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPActive, candidates15[0].reason)
		assert.Equal(t, 5, candidates15[0].targetSOC)
		assert.Contains(t, candidates15[0].description, "Mandatory VPP Event Active (Discharging to 5%)")

		// 2. Battery reached 5% SOC:
		// Target reached, switches to Standby at 5%.
		candidates5 := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 13.5 * 0.05, soc: 5.0}, anchors, userReserveSettings, status, nil, precedingAction{})
		require.Len(t, candidates5, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates5[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPActive, candidates5[0].reason)
		assert.Equal(t, 0, candidates5[0].targetSOC)
		assert.Contains(t, candidates5[0].description, "Mandatory VPP Target SOC reached (5%)")
	})

	t.Run("VPPActive_WithoutManageTOUSchedules_SelectsBatteryModeLoad", func(t *testing.T) {
		t.Parallel()

		vppStart := now.Add(-10 * time.Minute)
		vppEnd := now.Add(50 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppEnd,
					vppSoc:     20.0,
					mandatory:  true,
				},
			},
		}

		vppInterval := interval
		vppInterval.startTime = now
		vppInterval.endTime = now.Add(time.Hour)

		// Without ManageTOUSchedules (e.g. Enphase), ESS must use BatteryModeLoad rather than BatteryModeExport
		unmanagedSettings := settings
		unmanagedSettings.ManageTOUSchedules = false
		unmanagedSettings.GridExportBatteries = true

		candidates := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 8.1, soc: 60.0}, anchors, unmanagedSettings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeLoad, candidates[0].batteryMode, "Without managed TOU schedules, active VPP must discharge as load self-consumption")
		assert.Equal(t, types.ActionReasonVPPActive, candidates[0].reason)
	})

	t.Run("VPPActive_DuringNegativePrice_PrioritizesDischarge", func(t *testing.T) {
		t.Parallel()

		vppStart := now.Add(-10 * time.Minute)
		vppEnd := now.Add(50 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppEnd,
					vppSoc:     20.0,
					mandatory:  true,
				},
			},
		}

		vppInterval := interval
		vppInterval.startTime = now
		vppInterval.endTime = now.Add(time.Hour)
		vppInterval.importRate = -0.05 // Negative retail price during active VPP event

		vppSettings := settings
		vppSettings.ManageTOUSchedules = true
		vppSettings.GridExportBatteries = true

		candidates := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 8.1, soc: 60.0}, anchors, vppSettings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeExport, candidates[0].batteryMode, "Active VPP event must discharge rather than charging during negative retail price")
		assert.Equal(t, types.ActionReasonVPPActive, candidates[0].reason)
	})

	t.Run("NegativeExportRate_CurtailsSurplusSolar", func(t *testing.T) {
		t.Parallel()

		costlyExportInterval := interval
		costlyExportInterval.exportRate = -0.02 // Tariff charges $0.02/kWh to export surplus

		exportSettings := settings
		exportSettings.GridExportSolar = true

		candidates := c.generateActionCandidates(ctx, 0, costlyExportInterval, nil, planState{energyKWH: 6.75, soc: 50}, planningAnchors{}, exportSettings, status, nil, precedingAction{})
		require.NotEmpty(t, candidates)
		for _, cand := range candidates {
			assert.Equal(t, types.SolarModeNoExport, cand.solarMode, "When export rate is negative, solar mode must curtail to avoid penalty")
		}
	})

	t.Run("VPPActive_NonMandatory_SwitchesToStandbyAtUserReserve", func(t *testing.T) {
		t.Parallel()

		vppStart := now.Add(-10 * time.Minute)
		vppEnd := now.Add(50 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppEnd,
					vppSoc:     30.0,
					mandatory:  false,
				},
			},
		}

		vppInterval := interval
		vppInterval.startTime = now
		vppInterval.endTime = now.Add(time.Hour)

		reserveSettings := settings
		reserveSettings.MinBatterySOC = 20

		// Battery has reached user reserve of 20%: switches to standby
		candidates := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 2.7, soc: 20.0}, anchors, reserveSettings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPActive, candidates[0].reason)
		assert.Equal(t, 0, candidates[0].targetSOC)
	})

	t.Run("VPPPrep_ChargesImmediatelyRegardlessOfRate", func(t *testing.T) {
		t.Parallel()

		vppEventStart := now.Add(90 * time.Minute)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppEventStart,
					deadline:   vppEventStart.Add(-2 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.08,
		}

		peakPrepInterval := interval
		peakPrepInterval.importRate = 0.45 // High on-peak rate during prep window

		candidates := c.generateActionCandidates(ctx, 0, peakPrepInterval, nil, planState{energyKWH: 8.1, soc: 60}, anchors, settings, status, nil, precedingAction{})
		require.Len(t, candidates, 1)
		assert.Equal(t, types.BatteryModeChargeAny, candidates[0].batteryMode, "Must force grid charge even at 45c/kWh during VPP prep deadline")
		assert.Equal(t, types.ActionReasonVPPPrep, candidates[0].reason)
		assert.Equal(t, 100, candidates[0].targetSOC)
	})

	t.Run("VPPPrep_AllowsChargeWhenCheaperThanUpcomingPreVPPRates", func(t *testing.T) {
		t.Parallel()

		vppEventStart := now.Add(4 * time.Hour)
		anchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppEventStart,
					deadline:   vppEventStart.Add(-2 * time.Hour), // Deadline at now + 2 hours
					vppSoc:     20,
				},
			},
			knownPostHorizonRate: 0.01, // Midnight rate is cheap ($0.01)
		}

		timelineWithUpcomingSpike := []planInterval{
			{
				startTime:  now,
				endTime:    now.Add(time.Hour),
				importRate: 0.10, // $0.10 now
				minSOC:     20,
			},
			{
				startTime:  now.Add(time.Hour),
				endTime:    now.Add(2 * time.Hour),
				importRate: 0.20, // $0.20 before VPP event
				minSOC:     20,
			},
		}

		candidates := c.generateActionCandidates(ctx, 0, timelineWithUpcomingSpike[0], timelineWithUpcomingSpike, planState{energyKWH: 8.1, soc: 60}, anchors, settings, status, nil, precedingAction{})
		hasCharge := false
		chargeCount := 0
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeChargeAny {
				chargeCount++
				if cand.reason == types.ActionReasonVPPPrep {
					hasCharge = true
					assert.Equal(t, 0, cand.targetSOC, "Option B candidate leaves targetSOC 0 for trajectory resolution")
				}
			}
		}
		assert.Equal(t, 1, chargeCount, "Must consolidate into exactly one ChargeAny candidate")
		assert.True(t, hasCharge, "Must offer VPP prep charge at $0.10 even if midnight rate is $0.01, because upcoming pre-VPP rate is $0.20")
	})

	t.Run("ArbitrageAndVPP_ConsolidatesChargeAnyCandidate_PreferenceByTiming", func(t *testing.T) {
		t.Parallel()

		// Case 1: Arbitrage happens sooner (far before VPP deadline)
		// VPP event is in 14 hours (deadline in 12 hours).
		// Arbitrage rate spike is in 2 hours.
		// Result: Exactly ONE ChargeAny candidate, with reason ActionReasonDeficitChargeNow.
		vppFarStart := now.Add(14 * time.Hour)
		vppFarAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppFarStart,
					deadline:   vppFarStart.Add(-2 * time.Hour), // 12 hours from now
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.08,
		}

		timelineFar := []planInterval{
			{
				startTime:  now,
				endTime:    now.Add(time.Hour),
				importRate: 0.10,
				minSOC:     20,
			},
			{
				startTime:  now.Add(time.Hour),
				endTime:    now.Add(2 * time.Hour),
				importRate: 0.10,
				minSOC:     20,
			},
			{
				startTime:  now.Add(2 * time.Hour),
				endTime:    now.Add(3 * time.Hour),
				importRate: 0.35, // Rate spike well before pre-VPP recharge buffer
				minSOC:     20,
			},
		}

		candsFar := c.generateActionCandidates(ctx, 0, timelineFar[0], timelineFar, planState{energyKWH: 8.1, soc: 60}, vppFarAnchors, settings, status, nil, precedingAction{})
		chargeCountFar := 0
		var chargeCandFar actionCandidate
		for _, cand := range candsFar {
			if cand.batteryMode == types.BatteryModeChargeAny {
				chargeCountFar++
				chargeCandFar = cand
			}
		}
		assert.Equal(t, 1, chargeCountFar, "Must consolidate into exactly 1 ChargeAny candidate")
		assert.Equal(t, types.ActionReasonDeficitChargeNow, chargeCandFar.reason, "Arbitrage happens sooner with ample runway, so reason should be deficit/arbitrage charge")

		// Case 2: Rate spike is within the pre-VPP recharge window (VPP deadline is in 2 hours).
		// Result: Exactly ONE ChargeAny candidate, with reason ActionReasonVPPPrep.
		vppNearStart := now.Add(4 * time.Hour)
		vppNearAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppNearStart,
					deadline:   vppNearStart.Add(-2 * time.Hour), // 2 hours from now
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.08,
		}

		timelineNear := []planInterval{
			{
				startTime:  now,
				endTime:    now.Add(time.Hour),
				importRate: 0.10,
				minSOC:     20,
			},
			{
				startTime:  now.Add(time.Hour),
				endTime:    now.Add(2 * time.Hour),
				importRate: 0.35, // Rate spike is inside the pre-VPP window
				minSOC:     20,
			},
		}

		candsNear := c.generateActionCandidates(ctx, 0, timelineNear[0], timelineNear, planState{energyKWH: 8.1, soc: 60}, vppNearAnchors, settings, status, nil, precedingAction{})
		chargeCountNear := 0
		var chargeCandNear actionCandidate
		for _, cand := range candsNear {
			if cand.batteryMode == types.BatteryModeChargeAny {
				chargeCountNear++
				chargeCandNear = cand
			}
		}
		assert.Equal(t, 1, chargeCountNear, "Must consolidate into exactly 1 ChargeAny candidate")
		assert.Equal(t, types.ActionReasonVPPPrep, chargeCandNear.reason, "VPP deadline is sooner/governing, so reason must be VPPPrep")
	})

	t.Run("VPPRechargeBuffer_SuppressesBatteryExportDump", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true

		exportInterval := interval
		exportInterval.exportRate = 0.50 // Very high export rate

		// Case 1: Interval is within pre-VPP recharge buffer (e.g. VPP deadline in 2 hours).
		// Export dump must NOT be offered because the battery cannot recharge in time.
		nearVPPAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(4 * time.Hour),
					deadline:   now.Add(2 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.05,
		}
		candsNear := c.generateActionCandidates(ctx, 0, exportInterval, nil, planState{energyKWH: 13.5, soc: 100}, nearVPPAnchors, exportSettings, status, nil, precedingAction{})
		for _, cand := range candsNear {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode, "Must not offer battery export dump within pre-VPP recharge buffer")
		}

		// Case 2: Interval is 12 hours before VPP deadline (ample time to dump and recharge).
		// Export dump SHOULD be offered.
		farVPPAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(14 * time.Hour),
					deadline:   now.Add(12 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.05,
		}
		candsFar := c.generateActionCandidates(ctx, 0, exportInterval, nil, planState{energyKWH: 13.5, soc: 100}, farVPPAnchors, exportSettings, status, nil, precedingAction{})
		hasExport := false
		for _, cand := range candsFar {
			if cand.batteryMode == types.BatteryModeExport {
				hasExport = true
				break
			}
		}
		assert.True(t, hasExport, "Must offer battery export dump when VPP deadline is far away (12+ hours)")
	})

	t.Run("VPPRechargeBuffer_SuppressesDirectSolarExportDischarge", func(t *testing.T) {
		t.Parallel()

		solarSettings := settings
		solarSettings.ManageTOUSchedules = true
		solarSettings.GridExportSolar = true

		solarStatus := status
		solarStatus.SolarKW = 4.0

		solarInterval := interval
		solarInterval.exportRate = 0.20
		solarInterval.solarKWH = 4.0

		// Case 1: Interval is within pre-VPP recharge buffer.
		// Direct solar export with battery discharge must NOT be offered.
		nearVPPAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(4 * time.Hour),
					deadline:   now.Add(2 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.05,
		}
		candsNear := c.generateActionCandidates(ctx, 0, solarInterval, nil, planState{energyKWH: 10.0, soc: 75}, nearVPPAnchors, solarSettings, solarStatus, nil, precedingAction{})
		for _, cand := range candsNear {
			assert.NotEqual(t, types.SolarModeExport, cand.solarMode, "Must not offer direct solar export during pre-VPP recharge buffer")
		}

		// Case 2: Interval is 12 hours before VPP deadline.
		// Direct solar export SHOULD be offered.
		farVPPAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(14 * time.Hour),
					deadline:   now.Add(12 * time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
			knownPostHorizonRate: 0.05,
		}
		candsFar := c.generateActionCandidates(ctx, 0, solarInterval, nil, planState{energyKWH: 10.0, soc: 75}, farVPPAnchors, solarSettings, solarStatus, nil, precedingAction{})
		hasSolarExport := false
		for _, cand := range candsFar {
			if cand.solarMode == types.SolarModeExport {
				hasSolarExport = true
				break
			}
		}
		assert.True(t, hasSolarExport, "Must offer direct solar export when VPP deadline is far away (12+ hours)")
	})

	t.Run("EVCharging_PeakRate_PrunesChargeAny", func(t *testing.T) {
		t.Parallel()

		evSettings := settings
		evSettings.EVChargingPeriods = []types.TimePeriod{{Start: now.Add(-time.Hour), End: now.Add(2 * time.Hour)}}

		peakEVInterval := interval
		peakEVInterval.importRate = 0.40
		peakEVInterval.loadKWH = 6.0

		anchors := planningAnchors{knownPostHorizonRate: 0.08}
		evStatus := status
		evStatus.HomeKW = 6.5

		candidates := c.generateActionCandidates(ctx, 0, peakEVInterval, nil, planState{energyKWH: 8.1, soc: 60}, anchors, evSettings, evStatus, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode, "Must not charge battery from grid at 40c/kWh during EV charging")
		}
	})

	t.Run("BatteryExportDump_TargetSOCIncludes5PercentBuffer", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true
		anchors := planningAnchors{knownPostHorizonRate: 0.04}

		highExportInterval := interval
		highExportInterval.exportRate = 0.50
		highExportInterval.importRate = 0.15

		candidates := c.generateActionCandidates(ctx, 0, highExportInterval, nil, planState{energyKWH: 10.8, soc: 80}, anchors, exportSettings, status, nil, precedingAction{})
		var dumpCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeExport {
				dumpCand = &candidates[i]
				break
			}
		}
		require.NotNil(t, dumpCand, "Battery export dump must be offered")
		assert.Equal(t, 25, dumpCand.targetSOC, "TargetSOC must preserve 5% reserve buffer (20% + 5% = 25%)")
	})

	t.Run("BatteryExportDump_FlatNEM_Pruned", func(t *testing.T) {
		t.Parallel()

		nemSettings := settings
		nemSettings.ManageTOUSchedules = true
		nemSettings.GridExportBatteries = true
		nemSettings.UtilityRateOptions = types.UtilityRateOptions{
			NetMeteringCredits: true,
		}
		anchors := planningAnchors{knownPostHorizonRate: 0.04}

		highExportInterval := interval
		highExportInterval.exportRate = 0.50
		highExportInterval.importRate = 0.15

		candidates := c.generateActionCandidates(ctx, 0, highExportInterval, nil, planState{energyKWH: 10.8, soc: 80}, anchors, nemSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode, "Battery export dump must NEVER be offered under 1:1 Flat Net Metering")
		}
	})

	t.Run("BatteryExportDump_AccountsForRoundTripEfficiencyAndHurdle", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true
		exportSettings.MinBatteryExportDifferenceDollarsPerKWH = 0.10
		exportSettings.OptimizationProfile = "conservative" // 0.85 round-trip efficiency
		// Replacement cost = 0.10 / 0.85 = 0.1176
		// Total hurdle = 0.1176 + 0.10 = 0.2176
		anchors := planningAnchors{knownPostHorizonRate: 0.10}

		lowMarginInterval := interval
		lowMarginInterval.exportRate = 0.20 // 0.20 < 0.2176, fails hurdle
		lowMarginInterval.importRate = 0.10

		candidates := c.generateActionCandidates(ctx, 0, lowMarginInterval, nil, planState{energyKWH: 10.8, soc: 80}, anchors, exportSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode, "Must not export when export rate does not clear replacement cost + hurdle")
		}
	})

	t.Run("GridUnavailable_DuringNegativePrice_LocksInLoadBackupMode", func(t *testing.T) {
		t.Parallel()

		outageStatus := status
		outageStatus.GridUnavailable = true

		negInterval := interval
		negInterval.importRate = -0.05 // Negative wholesale price during blackout

		candidates := c.generateActionCandidates(ctx, 0, negInterval, nil, planState{energyKWH: 10.8, soc: 80}, planningAnchors{}, settings, outageStatus, nil, precedingAction{})
		require.Len(t, candidates, 1, "Must offer exactly one action during grid outage")
		assert.Equal(t, types.BatteryModeLoad, candidates[0].batteryMode, "Must operate in BatteryModeLoad to back up home during grid outage")
		assert.Equal(t, types.ActionReasonGridUnavailable, candidates[0].reason)
	})

	t.Run("SolarRefillHold_MinExportHoldDiff_PrunesSelfConsumptionTonight", func(t *testing.T) {
		t.Parallel()

		holdSettings := settings
		holdSettings.GridExportSolar = true
		holdSettings.MinExportHoldDifferenceDollarsPerKWH = 0.03

		holdTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.09, // Tonight's import rate is 9c
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.08, // Tomorrow solar export credit is 8c; 9c <= 8c + 3c = 11c
				solarKWH:      5.0,  // Daytime solar refill
				minSOC:        20.0,
			},
		}

		fullState := planState{energyKWH: 10.8, soc: 80.0, capacityKWH: 13.5}
		candidates := c.generateActionCandidates(ctx, 0, holdTimeline[0], holdTimeline, fullState, planningAnchors{}, holdSettings, status, nil, precedingAction{})

		// Both BatteryModeLoad and BatteryModeStandby should be offered as candidates
		var hasLoad, hasStandby bool
		var standbyCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeLoad {
				hasLoad = true
			}
			if candidates[i].batteryMode == types.BatteryModeStandby {
				hasStandby = true
				standbyCand = &candidates[i]
			}
		}
		assert.True(t, hasLoad, "Must offer Load candidate for DP optimization")
		require.True(t, hasStandby, "Must offer Standby candidate to hold battery for solar export")
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, standbyCand.reason)

		// The DP solver should select BatteryModeStandby because holding avoids cycling when solar refills tomorrow
		winningPath, err := c.searchOptimalPlan(ctx, holdTimeline, fullState, planningAnchors{}, holdSettings, status, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, winningPath)
		require.NotEmpty(t, winningPath.actions)
		assert.Equal(t, types.BatteryModeStandby, winningPath.actions[0].batteryMode)
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, winningPath.actions[0].reason)
	})

	t.Run("StandbyReason_VPPBeforeSolarExport_PrefersVPPPrep", func(t *testing.T) {
		t.Parallel()

		holdSettings := settings
		holdSettings.GridExportSolar = true
		holdSettings.MinExportHoldDifferenceDollarsPerKWH = 0.03

		holdTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.09, // Tonight's import rate is 9c
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.08, // Tomorrow solar export credit is 8c; 9c <= 8c + 3c = 11c
				solarKWH:      5.0,  // Daytime solar refill
				minSOC:        20.0,
			},
		}

		fullState := planState{energyKWH: 10.8, soc: 80.0, capacityKWH: 13.5}

		// Case 1: VPP event deadline (4h) is BEFORE solar refill (10h) -> reason should be ActionReasonVPPPrep
		anchorsVPPBefore := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(6 * time.Hour),
					eventEnd:   now.Add(8 * time.Hour),
					deadline:   now.Add(4 * time.Hour),
					vppSoc:     20.0,
				},
			},
		}
		candsVPPBefore := c.generateActionCandidates(ctx, 0, holdTimeline[0], holdTimeline, fullState, anchorsVPPBefore, holdSettings, status, nil, precedingAction{})
		var standbyCandBefore *actionCandidate
		for i := range candsVPPBefore {
			if candsVPPBefore[i].batteryMode == types.BatteryModeStandby {
				standbyCandBefore = &candsVPPBefore[i]
				break
			}
		}
		require.NotNil(t, standbyCandBefore, "Must offer Standby candidate")
		assert.Equal(t, types.ActionReasonVPPPrep, standbyCandBefore.reason, "When VPP deadline is before solar refill time, reason must be VPPPrep")

		// Case 2: VPP event deadline (14h) is AFTER solar refill (10h) -> reason should be ActionReasonHoldSimilarPrice
		anchorsVPPAfter := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(16 * time.Hour),
					eventEnd:   now.Add(18 * time.Hour),
					deadline:   now.Add(14 * time.Hour),
					vppSoc:     20.0,
				},
			},
		}
		candsVPPAfter := c.generateActionCandidates(ctx, 0, holdTimeline[0], holdTimeline, fullState, anchorsVPPAfter, holdSettings, status, nil, precedingAction{})
		var standbyCandAfter *actionCandidate
		for i := range candsVPPAfter {
			if candsVPPAfter[i].batteryMode == types.BatteryModeStandby {
				standbyCandAfter = &candsVPPAfter[i]
				break
			}
		}
		require.NotNil(t, standbyCandAfter, "Must offer Standby candidate")
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, standbyCandAfter.reason, "When VPP deadline is after solar refill time, reason must be HoldSimilarPrice")
	})

	t.Run("DirectSolarExport_AtReserve_OffersExportMode", func(t *testing.T) {
		t.Parallel()

		sunnyInterval := interval
		sunnyInterval.solarKWH = 4.0
		touSettings := settings
		touSettings.ManageTOUSchedules = true
		touSettings.GridExportSolar = true

		reserveState := planState{energyKWH: 2.7, soc: 20}
		candidates := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, reserveState, planningAnchors{}, touSettings, status, nil, precedingAction{})

		var directSolarCand *actionCandidate
		for i := range candidates {
			if candidates[i].solarMode == types.SolarModeExport {
				directSolarCand = &candidates[i]
				break
			}
		}
		require.NotNil(t, directSolarCand, "Direct solar export must be offered even when battery is at reserve")
		assert.Equal(t, types.ActionReasonBatteryAtReserve, directSolarCand.reason)
	})

	t.Run("DeficitCharge_CalculatesExactNeededTargetSOC", func(t *testing.T) {
		t.Parallel()

		timeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				loadKWH:       0.5,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				// Peak window (4 hours at $0.35/kWh, 2 kW load, 0 solar -> 8 kWh net load)
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 4.0,
				importRate:    0.35,
				loadKWH:       8.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				// Off-peak evening (rate drops back to $0.05)
				index:         2,
				startTime:     now.Add(5 * time.Hour),
				endTime:       now.Add(13 * time.Hour),
				durationHours: 8.0,
				importRate:    0.05,
				loadKWH:       8.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		// Battery starts at 20% SOC (2.7 kWh on a 13.5 kWh battery)
		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		lowStatus := status
		lowStatus.BatterySOC = 20.0

		candidates := c.generateActionCandidates(ctx, 0, timeline[0], timeline, lowState, planningAnchors{}, chargeSettings, lowStatus, nil, precedingAction{})

		var deficitCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeChargeAny && candidates[i].reason == types.ActionReasonDeficitChargeNow {
				deficitCand = &candidates[i]
				break
			}
		}

		require.NotNil(t, deficitCand, "Deficit charge candidate must be offered")
		assert.Equal(t, 0, deficitCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

		bestPath, err := c.searchOptimalPlan(ctx, timeline, lowState, planningAnchors{}, chargeSettings, lowStatus, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(bestPath, timeline, lowStatus, types.Price{DollarsPerKWH: 0.05}, now, chargeSettings)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Greater(t, decision.Action.ChargeToSOC, 20, "Derived target SOC must be above initial reserve")
		assert.Less(t, decision.Action.ChargeToSOC, 100, "Derived target SOC must NOT blindly charge to 100% when only deficit is needed")
	})

	t.Run("DeficitCharge_PrunedWhenNoFutureDeficit", func(t *testing.T) {
		t.Parallel()

		// Flat off-peak rate timeline all day (no peak prices ahead)
		flatTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				loadKWH:       0.5,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(12 * time.Hour),
				durationHours: 11.0,
				importRate:    0.05,
				loadKWH:       5.5,
				minSOC:        20.0,
			},
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		candidates := c.generateActionCandidates(ctx, 0, flatTimeline[0], flatTimeline, lowState, planningAnchors{}, chargeSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode,
				"ChargeAny must be pruned when rates are flat and there is no expensive peak deficit")
		}
	})

	t.Run("DeficitCharge_AccountsForSurplusSolar", func(t *testing.T) {
		t.Parallel()

		solarTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				loadKWH:       0.5,
				minSOC:        20.0,
			},
			{
				// Heavy daytime solar filling the battery naturally to 100% capacity
				index:         1,
				startTime:     now.Add(6 * time.Hour),
				endTime:       now.Add(12 * time.Hour),
				durationHours: 6.0,
				importRate:    0.05,
				loadKWH:       6.0,
				solarKWH:      36.0, // Surplus solar = 5 kW * 6h = 30 kWh (way exceeds 13.5 kWh)
				minSOC:        20.0,
			},
			{
				// Peak window
				index:         2,
				startTime:     now.Add(14 * time.Hour),
				endTime:       now.Add(18 * time.Hour),
				durationHours: 4.0,
				importRate:    0.35,
				loadKWH:       8.0,
				minSOC:        20.0,
			},
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		bestPath, err := c.searchOptimalPlan(ctx, solarTimeline, lowState, planningAnchors{}, chargeSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(bestPath, solarTimeline, status, types.Price{DollarsPerKWH: 0.05}, now, chargeSettings)
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode,
			"Solver must not choose ChargeAny when daytime solar will naturally fill battery before peak")
	})

	t.Run("ArbitragePrecharge_OfferedWhenProfitableSolarExportAhead", func(t *testing.T) {
		t.Parallel()

		exportTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(3 * time.Hour),
				durationHours: 3.0,
				importRate:    0.05,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(8 * time.Hour),
				endTime:       now.Add(14 * time.Hour),
				durationHours: 6.0,
				importRate:    0.25,
				exportRate:    0.25, // Profitable solar export credit rate
				solarKWH:      24.0,
				minSOC:        20.0,
			},
		}

		exportSettings := settings
		exportSettings.GridChargeBatteries = true
		exportSettings.GridExportSolar = true
		exportSettings.UtilityRateOptions.NetMeteringCredits = false

		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		candidates := c.generateActionCandidates(ctx, 0, exportTimeline[0], exportTimeline, lowState, planningAnchors{}, exportSettings, status, nil, precedingAction{})

		var arbCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeChargeAny && candidates[i].reason == types.ActionReasonArbitrageChargeExport {
				arbCand = &candidates[i]
				break
			}
		}

		require.NotNil(t, arbCand, "Arbitrage pre-charge must be offered when profitable solar export is ahead")
		assert.Equal(t, 0, arbCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

		bestPath, err := c.searchOptimalPlan(ctx, exportTimeline, lowState, planningAnchors{}, exportSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(bestPath, exportTimeline, status, types.Price{DollarsPerKWH: 0.05}, now, exportSettings)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, 100, decision.Action.ChargeToSOC, "DP must dynamically charge to 100% to maximize solar export credits")
	})

	t.Run("ArbitragePrecharge_PrunedWhenFlatNetMetering", func(t *testing.T) {
		t.Parallel()

		exportTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(8 * time.Hour),
				endTime:       now.Add(14 * time.Hour),
				durationHours: 6.0,
				importRate:    0.25,
				exportRate:    0.25,
				solarKWH:      24.0,
				minSOC:        20.0,
			},
		}

		nemSettings := settings
		nemSettings.GridChargeBatteries = true
		nemSettings.GridExportSolar = true
		nemSettings.UtilityRateOptions.NetMeteringCredits = true // 1:1 Net Metering

		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		candidates := c.generateActionCandidates(ctx, 0, exportTimeline[0], exportTimeline, lowState, planningAnchors{}, nemSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.ActionReasonArbitrageChargeExport, cand.reason,
				"Arbitrage pre-charge must not be offered under 1:1 flat net metering")
		}
	})

	t.Run("DeficitSaveForPeak_RetainedWhenBatteryFull", func(t *testing.T) {
		t.Parallel()

		peakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 4.0,
				importRate:    0.50,
				loadKWH:       10.0, // 10 kWh load during peak
				minSOC:        20.0,
			},
		}

		fullState := planState{
			energyKWH:   13.5,
			soc:         100.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		candidates := c.generateActionCandidates(ctx, 0, peakTimeline[0], peakTimeline, fullState, planningAnchors{}, settings, status, nil, precedingAction{})
		hasStandbySaveForPeak := false
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeStandby && cand.reason == types.ActionReasonDeficitSaveForPeak {
				hasStandbySaveForPeak = true
				break
			}
		}
		assert.True(t, hasStandbySaveForPeak, "BatteryModeStandby must be offered when 100% full to preserve charge for upcoming peak")
	})

	t.Run("Standby_OfferedWhenTonightPriceSimilarToTomorrowSolarExport", func(t *testing.T) {
		t.Parallel()

		// Flat pricing tonight ($0.10) and tomorrow solar export ($0.10).
		// Discharging tonight to save $0.10 grid power when solar will refill tomorrow
		// causes unnecessary battery cycling and round-trip efficiency losses.
		holdTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(8 * time.Hour),
				endTime:       now.Add(14 * time.Hour),
				durationHours: 6.0,
				importRate:    0.10,
				exportRate:    0.10,
				solarKWH:      18.0, // Plentiful solar generation will refill battery
				loadKWH:       2.0,
				minSOC:        20.0,
			},
		}

		holdSettings := settings
		holdSettings.GridExportSolar = true
		holdSettings.UtilityRateOptions.NetMeteringCredits = false
		holdSettings.MinExportHoldDifferenceDollarsPerKWH = 0.02

		// Battery starts with 80% charge (10.8 kWh)
		chargedState := planState{
			energyKWH:   10.8,
			soc:         80.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		candidates := c.generateActionCandidates(ctx, 0, holdTimeline[0], holdTimeline, chargedState, planningAnchors{}, holdSettings, status, nil, precedingAction{})
		var holdCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeStandby && candidates[i].reason == types.ActionReasonHoldSimilarPrice {
				holdCand = &candidates[i]
				break
			}
		}
		require.NotNil(t, holdCand, "Must offer BatteryModeStandby with ActionReasonHoldSimilarPrice when tonight's rate is close to tomorrow's export rate")

		// Verify DP solver chooses Standby to avoid cycling the battery for a wash
		bestPath, err := c.searchOptimalPlan(ctx, holdTimeline, chargedState, planningAnchors{}, holdSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(bestPath, holdTimeline, status, types.Price{DollarsPerKWH: 0.10}, now, holdSettings)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode, "DP solver must choose Standby over Load when discharging is a wash with tomorrow's solar export")
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, decision.Action.Reason)
	})

	t.Run("DeficitCharge_SubHeadroomDeficitPrunedToPreventShortCycling", func(t *testing.T) {
		t.Parallel()

		// Peak requires small deficit (e.g. ~0.35 kWh) which is less than requiredHeadroom (0.60 kWh)
		smallDeficitTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.40,
				loadKWH:       3.05, // Usable energy is 2.7 kWh; deficit is ~0.35 kWh
				minSOC:        20.0,
			},
		}

		// Battery starts at 97.4% SOC (headroom to 100% is 0.35 kWh, less than requiredHeadroom 0.60 kWh)
		stateNearFull := planState{
			energyKWH:   13.15,
			soc:         97.4,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		candidates := c.generateActionCandidates(ctx, 0, smallDeficitTimeline[0], smallDeficitTimeline, stateNearFull, planningAnchors{}, chargeSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode,
				"ChargeAny must be pruned when headroom is below requiredHeadroom buffer to prevent short-cycling")
		}
	})

	t.Run("DeficitCharge_MultiPeakShoulderAccumulatesDeficit", func(t *testing.T) {
		t.Parallel()

		multiPeakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        20.0,
			},
			{
				// Peak 1: 2 hours at $0.35 (6 kWh load)
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 2.0,
				importRate:    0.35,
				loadKWH:       6.0,
				minSOC:        20.0,
			},
			{
				// Shoulder: 1 hour at $0.15 (rate > 0.10, but not peak; 2 kWh load)
				index:         2,
				startTime:     now.Add(3 * time.Hour),
				endTime:       now.Add(4 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				loadKWH:       2.0,
				minSOC:        20.0,
			},
			{
				// Peak 2: 2 hours at $0.40 (6 kWh load)
				index:         3,
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(6 * time.Hour),
				durationHours: 2.0,
				importRate:    0.40,
				loadKWH:       6.0,
				minSOC:        20.0,
			},
		}

		// Battery starts at 50% SOC (6.75 kWh; reserve 2.7 kWh; usable 4.05 kWh)
		stateAt50 := planState{
			energyKWH:   6.75,
			soc:         50.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		candidates := c.generateActionCandidates(ctx, 0, multiPeakTimeline[0], multiPeakTimeline, stateAt50, planningAnchors{}, chargeSettings, status, nil, precedingAction{})
		var chargeCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeChargeAny && candidates[i].reason == types.ActionReasonDeficitChargeNow {
				chargeCand = &candidates[i]
				break
			}
		}
		require.NotNil(t, chargeCand, "Must offer deficit pre-charge covering multi-peak demand")
		assert.Equal(t, 0, chargeCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

		bestPath, err := c.searchOptimalPlan(ctx, multiPeakTimeline, stateAt50, planningAnchors{}, chargeSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(bestPath, multiPeakTimeline, status, types.Price{DollarsPerKWH: 0.10}, now, chargeSettings)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Greater(t, decision.Action.ChargeToSOC, 80, "Multi-peak deficit must accumulate across shoulder period and target high SOC")
	})

	t.Run("DeficitCharge_StartingBelowReserveDoesNotCreateFreeEnergy", func(t *testing.T) {
		t.Parallel()

		// Starting below reserve: 15% SOC (2.025 kWh) vs 20% MinSOC (2.7 kWh)
		subReserveState := planState{
			energyKWH:   2.025,
			soc:         15.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		timelinePrePeakLoad := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       2.0, // 2 kWh pre-peak load
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 2.0,
				importRate:    0.40,
				loadKWH:       6.0, // 6 kWh peak load
				minSOC:        20.0,
			},
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		subStatus := status
		subStatus.BatterySOC = 15.0

		candidates := c.generateActionCandidates(ctx, 0, timelinePrePeakLoad[0], timelinePrePeakLoad, subReserveState, planningAnchors{}, chargeSettings, subStatus, nil, precedingAction{})
		var chargeCand *actionCandidate
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeChargeAny && candidates[i].reason == types.ActionReasonDeficitChargeNow {
				chargeCand = &candidates[i]
				break
			}
		}
		require.NotNil(t, chargeCand)
		assert.Equal(t, 0, chargeCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

		bestPath, err := c.searchOptimalPlan(ctx, timelinePrePeakLoad, subReserveState, planningAnchors{}, chargeSettings, subStatus, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(bestPath, timelinePrePeakLoad, subStatus, types.Price{DollarsPerKWH: 0.10}, now, chargeSettings)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.GreaterOrEqual(t, decision.Action.ChargeToSOC, 50)
	})

	t.Run("ArbitragePrecharge_AccountsForRoundTripEfficiencyLoss", func(t *testing.T) {
		t.Parallel()

		// Export rate $0.11, current import rate $0.10.
		// Default roundtrip efficiency is ~0.90 -> 0.10 / 0.90 = 0.111 > 0.11.
		// Since export rate $0.11 < $0.111, this is financially negative arbitrage!
		arbTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(6 * time.Hour),
				endTime:       now.Add(10 * time.Hour),
				durationHours: 4.0,
				importRate:    0.15,
				exportRate:    0.11,
				solarKWH:      16.0,
				minSOC:        20.0,
			},
		}

		arbSettings := settings
		arbSettings.GridChargeBatteries = true
		arbSettings.GridExportSolar = true

		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		candidates := c.generateActionCandidates(ctx, 0, arbTimeline[0], arbTimeline, lowState, planningAnchors{}, arbSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.ActionReasonArbitrageChargeExport, cand.reason,
				"Arbitrage pre-charge must be pruned when export rate does not clear AC round-trip recharge cost")
		}
	})
}

// TestStepPhysics tests the physical energy flows, round-trip efficiency losses, and accounting.
func TestStepPhysics(t *testing.T) {
	t.Parallel()

	interval := planInterval{
		durationHours: 1.0,
		importRate:    0.10,
		exportRate:    0.05,
		loadKWH:       2.0,
		solarKWH:      0.0,
		minSOC:        20,
	}

	settings := types.Settings{
		MinBatterySOC:      20,
		ManageTOUSchedules: true,
	}

	initialState := planState{
		energyKWH:   10.0,
		soc:         (10.0 / 13.5) * 100.0,
		capacityKWH: 13.5,
	}

	t.Run("ChargeAny_AppliesRoundTripLosses", func(t *testing.T) {
		t.Parallel()

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeAny,
		}

		roundTripEff := 0.90
		oneWayEff := math.Sqrt(roundTripEff)

		nextState, metrics := stepPhysics(initialState, action, interval, settings, roundTripEff)

		// Battery should have gained energy clamped by max charge rate (5 kW * 1h * sqrt(0.90))
		expectedGain := 3.5 // remaining capacity is 13.5 - 10.0 = 3.5 kWh
		assert.InDelta(t, 13.5, nextState.energyKWH, 0.01)

		// Grid import should cover home load (2 kWh) + battery grid charge (3.5 / sqrt(0.90))
		expectedGridCharge := expectedGain / oneWayEff
		assert.InDelta(t, 2.0+expectedGridCharge, metrics.gridImportKWH, 0.05)
	})

	t.Run("Load_SuppliesHomeLoadDownToReserve", func(t *testing.T) {
		t.Parallel()

		action := actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeAny,
		}

		roundTripEff := 0.90
		oneWayEff := math.Sqrt(roundTripEff)

		nextState, metrics := stepPhysics(initialState, action, interval, settings, roundTripEff)

		// Battery supplied 2 kWh of home load: delta E = -2.0 / sqrt(0.90)
		expectedLoss := 2.0 / oneWayEff
		assert.InDelta(t, 10.0-expectedLoss, nextState.energyKWH, 0.01)

		// Zero grid import needed because battery covered full load
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.001)
	})

	t.Run("DirectSolarExport_HomeLoadFromBattery", func(t *testing.T) {
		t.Parallel()

		sunnyInterval := interval
		sunnyInterval.solarKWH = 4.0
		exportSettings := settings
		exportSettings.GridExportSolar = true

		action := actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeExport,
		}

		_, metrics := stepPhysics(initialState, action, sunnyInterval, exportSettings, 0.90)

		// 100% of solar (4 kWh) exported to grid
		assert.InDelta(t, 4.0, metrics.gridExportKWH, 0.01)
		// Zero grid import because battery supplied home load
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.01)
	})

	t.Run("DirectSolarExport_HomeLoadServedBySolarFirstOnStandby", func(t *testing.T) {
		t.Parallel()

		sunnyInterval := interval
		sunnyInterval.solarKWH = 4.0
		exportSettings := settings
		exportSettings.GridExportSolar = true

		action := actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   types.SolarModeExport,
		}

		nextState, metrics := stepPhysics(initialState, action, sunnyInterval, exportSettings, 0.90)

		// Surplus solar (4 kWh solar - 2 kWh home load = 2 kWh) exported to grid behind bidirectional meter
		assert.InDelta(t, 2.0, metrics.gridExportKWH, 0.01)
		// Zero grid import because solar supplied home load first
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.01)
		// Battery energy unchanged
		assert.InDelta(t, initialState.energyKWH, nextState.energyKWH, 0.01)
	})

	t.Run("SolarExportCurtailed_WhenSolarModeNoExport", func(t *testing.T) {
		t.Parallel()

		sunnyInterval := interval
		sunnyInterval.solarKWH = 4.0

		action := actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   types.SolarModeNoExport, // Explicit SolarModeNoExport
		}

		nextState, metrics := stepPhysics(initialState, action, sunnyInterval, settings, 0.90)

		// Solar export must be 0 because SolarMode is NoExport
		assert.InDelta(t, 0.0, metrics.gridExportKWH, 0.01)
		// Home served from solar (2 kWh), remaining solar (2 kWh) charged into battery
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.01)
		assert.Greater(t, nextState.energyKWH, initialState.energyKWH)
	})

	t.Run("ZeroDuration_ReturnsUnchangedState", func(t *testing.T) {
		t.Parallel()

		zeroInterval := interval
		zeroInterval.durationHours = 0.0

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeAny,
		}

		nextState, metrics := stepPhysics(initialState, action, zeroInterval, settings, 0.90)

		assert.InDelta(t, initialState.energyKWH, nextState.energyKWH, 0.001)
		assert.InDelta(t, initialState.soc, metrics.endingSOC, 0.001)
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.001)
	})

	t.Run("BatteryExportDump_CreditsWholesaleRate", func(t *testing.T) {
		t.Parallel()

		dumpInterval := interval
		dumpInterval.exportRate = 0.15 // Export rate
		dumpInterval.loadKWH = 1.0     // 1 kW home load

		exportSettings := settings
		exportSettings.GridExportBatteries = true

		action := actionCandidate{
			batteryMode: types.BatteryModeExport,
			solarMode:   types.SolarModeAny,
		}

		// Battery starts with 10 kWh, reserve is 20% (2.7 kWh). Usable is 7.3 kWh.
		// Max discharge is 5 kW * 1h = 5 kWh.
		// Home takes 1 kWh, remaining 4 kWh is exported to grid.
		_, metrics := stepPhysics(initialState, action, dumpInterval, exportSettings, 0.90)

		assert.InDelta(t, 4.0, metrics.gridExportKWH, 0.01)
		// Gross export credits must use ExportRate (4.0 * 0.15 = $0.60)
		assert.InDelta(t, 0.60, metrics.grossExportCreditDollars, 0.01)
	})

	t.Run("BatteryExport_PrioritizesBatteryForHomeLoadAndDirectExportsSolar", func(t *testing.T) {
		t.Parallel()

		// Case A: Home load (2 kW) < max battery discharge (5 kW), Solar = 3 kW
		// Battery covers full 2 kW home load + exports remaining 3 kW.
		// Solar (3 kW) direct exports 100% to grid. Total export = 6 kW. Grid import = 0.
		sunnyInterval := interval
		sunnyInterval.loadKWH = 2.0
		sunnyInterval.solarKWH = 3.0

		exportAction := actionCandidate{
			batteryMode: types.BatteryModeExport,
			solarMode:   types.SolarModeAny,
		}

		_, metricsA := stepPhysics(initialState, exportAction, sunnyInterval, settings, 0.90)
		assert.InDelta(t, 2.0, metricsA.batSuppliedHomeKWH, 0.01, "Battery must cover home load first")
		assert.InDelta(t, 0.0, metricsA.solarToHomeKW, 0.01, "Solar should not serve home load when battery has sufficient capacity")
		assert.InDelta(t, 3.0, metricsA.solarExportKW, 0.01, "100% of solar should direct export")
		assert.InDelta(t, 3.0, metricsA.batExportKWH, 0.01, "Remaining battery discharge should export to grid")
		assert.InDelta(t, 6.0, metricsA.gridExportKWH, 0.01, "Total grid export should be solar + battery export")
		assert.InDelta(t, 0.0, metricsA.gridImportKWH, 0.01)

		// Case B: Home load (7 kW) > max battery discharge (5 kW), Solar = 4 kW
		// Battery covers 5 kW of home load (max discharge).
		// Solar covers remaining 2 kW home load + exports remaining 2 kW.
		// Total export = 2 kW. Grid import = 0.
		heavyInterval := interval
		heavyInterval.loadKWH = 7.0
		heavyInterval.solarKWH = 4.0

		_, metricsB := stepPhysics(initialState, exportAction, heavyInterval, settings, 0.90)
		assert.InDelta(t, 5.0, metricsB.batSuppliedHomeKWH, 0.01, "Battery supplies max discharge to home load")
		assert.InDelta(t, 2.0, metricsB.solarToHomeKW, 0.01, "Solar covers remaining home load exceeding battery discharge")
		assert.InDelta(t, 2.0, metricsB.solarExportKW, 0.01, "Remaining solar exports to grid")
		assert.InDelta(t, 0.0, metricsB.batExportKWH, 0.01, "No remaining battery capacity to export")
		assert.InDelta(t, 2.0, metricsB.gridExportKWH, 0.01)
		assert.InDelta(t, 0.0, metricsB.gridImportKWH, 0.01)

		// Case C: Home load (10 kW) > battery (5 kW) + solar (3 kW)
		// Battery covers 5 kW, Solar covers 3 kW. Remaining 2 kW pulls from grid.
		// Total export = 0. Grid import = 2 kW.
		extremeInterval := interval
		extremeInterval.loadKWH = 10.0
		extremeInterval.solarKWH = 3.0

		_, metricsC := stepPhysics(initialState, exportAction, extremeInterval, settings, 0.90)
		assert.InDelta(t, 5.0, metricsC.batSuppliedHomeKWH, 0.01)
		assert.InDelta(t, 3.0, metricsC.solarToHomeKW, 0.01)
		assert.InDelta(t, 0.0, metricsC.gridExportKWH, 0.01)
		assert.InDelta(t, 2.0, metricsC.gridImportKWH, 0.01, "Remaining home load beyond battery + solar pulls from grid")
	})

	t.Run("ChargeAny_BehindMeter_SolarChargesBatteryFirst", func(t *testing.T) {
		t.Parallel()

		sunnyInterval := interval
		sunnyInterval.solarKWH = 4.0
		sunnyInterval.loadKWH = 1.0

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeExport,
		}

		exportSettings := settings
		exportSettings.GridExportSolar = true

		// Battery starts with 10.0 kWh (capacity 13.5 kWh, room = 3.5 kWh)
		nextState, metrics := stepPhysics(initialState, action, sunnyInterval, exportSettings, 0.90)

		// Home takes 1 kWh solar. Surplus solar is 3 kWh.
		// All 3 kWh surplus solar charges battery (solarToBatKW = 3.0).
		// Since battery still has room (3.5 kWh room), 0 kWh solar is exported!
		assert.InDelta(t, 0.0, metrics.gridExportKWH, 0.01, "No solar export when battery is charging behind bidirectional meter")
		assert.InDelta(t, 3.0, metrics.solarToBatKW, 0.01)
		assert.Greater(t, nextState.energyKWH, initialState.energyKWH)
	})

	t.Run("ChargeAny_ClampsGridChargeToTargetSOC", func(t *testing.T) {
		t.Parallel()

		// Start with 20% SOC (2.7 kWh on a 13.5 kWh battery)
		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 10.0, // High enough to charge 10 kWh in 1 hour if not clamped
		}

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeAny,
			targetSOC:   60, // Clamp grid charging to 60% SOC (8.1 kWh)
		}

		roundTripEff := 0.90
		oneWayEff := math.Sqrt(roundTripEff)

		nextState, metrics := stepPhysics(lowState, action, interval, settings, roundTripEff)

		// Target limit is 13.5 * 0.60 = 8.1 kWh. Room is 8.1 - 2.7 = 5.4 kWh.
		assert.InDelta(t, 8.1, nextState.energyKWH, 0.05, "Battery energy must clamp at 60% target SOC")
		assert.InDelta(t, 60.0, nextState.soc, 0.5, "Ending SOC must match target SOC")

		// Grid import should cover home load (2 kWh) + battery grid charge (5.4 / oneWayEff)
		expectedGridCharge := 5.4 / oneWayEff
		assert.InDelta(t, 2.0+expectedGridCharge, metrics.gridImportKWH, 0.05)
	})

	t.Run("ChargeAny_ZeroChargeWhenAlreadyAtOrAboveTargetSOC", func(t *testing.T) {
		t.Parallel()

		// Start with 70% SOC (9.45 kWh on a 13.5 kWh battery)
		highState := planState{
			energyKWH:   9.45,
			soc:         70.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeAny,
			targetSOC:   60, // Target is 60%, but battery is already at 70%
		}

		nextState, metrics := stepPhysics(highState, action, interval, settings, 0.90)

		// Zero grid energy charged into battery
		assert.InDelta(t, 0.0, metrics.batteryChargeKW, 0.001, "No grid charging when already above target SOC")
		assert.InDelta(t, 9.45, nextState.energyKWH, 0.01)
		assert.InDelta(t, 70.0, nextState.soc, 0.1)

		// Home load (2 kWh) is imported from grid
		assert.InDelta(t, 2.0, metrics.gridImportKWH, 0.01)
	})

	t.Run("ChargeAny_FullTargetSOCOn100", func(t *testing.T) {
		t.Parallel()

		// Start with 20% SOC (2.7 kWh)
		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeAny,
			targetSOC:   100,
		}

		roundTripEff := 0.90
		oneWayEff := math.Sqrt(roundTripEff)

		nextState, metrics := stepPhysics(lowState, action, interval, settings, roundTripEff)

		// Max charge is 5 kW * 1h = 5 kWh from grid -> 5 * oneWayEff added to battery
		expectedGain := 5.0 * oneWayEff
		assert.InDelta(t, 2.7+expectedGain, nextState.energyKWH, 0.05)
		assert.InDelta(t, 2.0+5.0, metrics.gridImportKWH, 0.05)
	})

	t.Run("ChargeAny_SurplusSolarChargesPastTargetSOCToCapacity", func(t *testing.T) {
		t.Parallel()

		// Start at 50% SOC (6.75 kWh). Grid targetSOC is 60% (8.1 kWh).
		// Huge surplus solar of 8 kW for 1 hour (9 kW solar - 1 kW load).
		solarInterval := interval
		solarInterval.loadKWH = 1.0
		solarInterval.solarKWH = 9.0

		action := actionCandidate{
			batteryMode: types.BatteryModeChargeAny,
			solarMode:   types.SolarModeAny,
			targetSOC:   60,
		}

		nextState, metrics := stepPhysics(planState{energyKWH: 6.75, soc: 50.0, capacityKWH: 13.5, maxChargeKW: 10.0}, action, solarInterval, settings, 0.90)
		// Surplus solar is not curtailed; it charges the battery past 60% towards 100% capacity
		assert.Greater(t, nextState.soc, 60.0, "Surplus solar must not be curtailed and should charge battery past grid targetSOC")
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.001, "No grid import needed when solar surplus covers battery charging")
	})

	t.Run("NegativeHomeLoad_ClampedToZero", func(t *testing.T) {
		t.Parallel()

		negLoadInterval := interval
		negLoadInterval.loadKWH = -2.0
		negLoadInterval.solarKWH = 0.0

		action := actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   types.SolarModeAny,
		}

		_, metrics := stepPhysics(initialState, action, negLoadInterval, settings, 0.90)
		assert.InDelta(t, 0.0, metrics.solarToHomeKW, 0.001)
		assert.InDelta(t, 0.0, metrics.gridImportKWH, 0.001)
	})

	t.Run("VPPActive_ClampsDischargeToTargetSOC", func(t *testing.T) {
		t.Parallel()

		stateAt45 := planState{
			energyKWH:      13.5 * 0.45,
			soc:            45.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
		}

		vppAction := actionCandidate{
			batteryMode:        types.BatteryModeLoad,
			reason:             types.ActionReasonVPPActive,
			targetSOC:          40, // VPP contracted reserve is 40%
			overrideReserveSOC: 40.0,
		}

		bigLoadInterval := interval
		bigLoadInterval.loadKWH = 5.0 // Big load trying to discharge 5 kWh

		nextState, _ := stepPhysics(stateAt45, vppAction, bigLoadInterval, settings, 0.90)
		// Battery must not discharge below 40% target SOC
		assert.InDelta(t, 40.0, nextState.soc, 0.5, "Active VPP event must not discharge below contracted TargetSOC")
	})

	t.Run("VPPActive_ExportMode_ClampsDischargeToTargetSOC", func(t *testing.T) {
		t.Parallel()

		stateAt45 := planState{
			energyKWH:      13.5 * 0.45,
			soc:            45.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
		}

		vppExportAction := actionCandidate{
			batteryMode:        types.BatteryModeExport,
			reason:             types.ActionReasonVPPActive,
			targetSOC:          40, // VPP contracted reserve is 40%
			overrideReserveSOC: 40.0,
		}

		exportSettings := settings
		exportSettings.GridExportBatteries = true

		exportInterval := interval
		exportInterval.exportRate = 0.50
		exportInterval.loadKWH = 0.0 // No home load; pure export

		nextState, metrics := stepPhysics(stateAt45, vppExportAction, exportInterval, exportSettings, 0.90)
		// Battery must discharge down to 40% (not 40% + 5% buffer)
		assert.InDelta(t, 40.0, nextState.soc, 0.5, "Active VPP event must discharge down to contracted TargetSOC without 5% buffer")
		assert.Greater(t, metrics.gridExportKWH, 0.0)
	})

	t.Run("KirchhoffPowerBalance_HoldsAcrossAllModes", func(t *testing.T) {
		t.Parallel()

		modes := []types.BatteryMode{
			types.BatteryModeChargeAny,
			types.BatteryModeLoad,
			types.BatteryModeExport,
			types.BatteryModeStandby,
		}

		testInterval := planInterval{
			durationHours: 1.0,
			importRate:    0.20,
			exportRate:    0.08,
			loadKWH:       3.0,
			solarKWH:      2.5,
			minSOC:        20,
		}

		exportSettings := settings
		exportSettings.GridExportSolar = true
		exportSettings.GridExportBatteries = true
		exportSettings.GridChargeBatteries = true

		for _, mode := range modes {
			action := actionCandidate{
				batteryMode: mode,
				solarMode:   types.SolarModeAny,
			}
			_, metrics := stepPhysics(initialState, action, testInterval, exportSettings, 0.90)

			// AC power balance at site POI meter:
			// NetGridKW = GridImportKW - GridExportKW
			// Kirchhoff: NetGridKW == HomeKW - (SolarKW - SolarCurtailedKW) + BatChargeKW - BatDischargeKW
			gridImportKW := metrics.gridImportKWH / testInterval.durationHours
			gridExportKW := metrics.gridExportKWH / testInterval.durationHours
			netGridKW := gridImportKW - gridExportKW

			effectiveSolarKW := testInterval.avgSolarKW() - metrics.solarCurtailedKW
			expectedNetGridKW := testInterval.avgLoadKW() - effectiveSolarKW + metrics.batteryChargeKW - metrics.batteryDischargeKW

			assert.InDelta(t, expectedNetGridKW, netGridKW, 0.001, "Kirchhoff POI meter balance violated for mode %s", mode)
		}
	})

	t.Run("VPPActive_TargetSOC5_DischargesToMinimumReserve", func(t *testing.T) {
		t.Parallel()

		stateAt20 := planState{
			energyKWH:      13.5 * 0.20,
			soc:            20.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
		}

		vppMinAction := actionCandidate{
			batteryMode:        types.BatteryModeExport,
			reason:             types.ActionReasonVPPActive,
			targetSOC:          5, // Contracted target is 5% minimum
			overrideReserveSOC: 5.0,
		}

		exportSettings := settings
		exportSettings.GridExportBatteries = true

		exportInterval := interval
		exportInterval.exportRate = 1.00
		exportInterval.loadKWH = 0.0

		nextState, metrics := stepPhysics(stateAt20, vppMinAction, exportInterval, exportSettings, 0.90)
		assert.InDelta(t, 5.0, nextState.soc, 0.5, "Active VPP event with target 5% must discharge down to 5% rather than halting at customer reserve (20%)")
		assert.Greater(t, metrics.gridExportKWH, 0.0)
	})

	t.Run("StepPhysics_NaNResilience", func(t *testing.T) {
		t.Parallel()

		nanState := planState{
			energyKWH:      math.NaN(),
			soc:            math.NaN(),
			capacityKWH:    math.NaN(),
			maxChargeKW:    math.NaN(),
			maxDischargeKW: math.NaN(),
		}

		nanInterval := planInterval{
			durationHours: math.NaN(),
			loadKWH:       math.NaN(),
			solarKWH:      math.NaN(),
			minSOC:        math.NaN(),
		}

		action := actionCandidate{
			batteryMode: types.BatteryModeStandby,
			solarMode:   types.SolarModeAny,
		}

		nextState, metrics := stepPhysics(nanState, action, nanInterval, settings, math.NaN())
		assert.False(t, math.IsNaN(nextState.capacityKWH), "Capacity must not be NaN")
		assert.False(t, math.IsNaN(metrics.costDollars), "Cost must not be NaN")
	})
}

// TestCalculateTerminalValuation tests ending battery valuation (replacement cost vs banked credit).
func TestCalculateTerminalValuation(t *testing.T) {
	t.Parallel()

	settings := types.Settings{
		MinBatterySOC: 20,
	}

	anchors := planningAnchors{
		knownPostHorizonRate: 0.08,
	}

	capacityKWH := 13.5
	roundTripEff := 0.90

	t.Run("EndingBelowReserve_PenalizedByRechargeCost", func(t *testing.T) {
		t.Parallel()

		// Ending at 15% SOC (target is 25% with 5% buffer)
		finalState := planState{
			energyKWH: capacityKWH * 0.15,
			soc:       15,
		}

		valuation := calculateTerminalValuation(finalState, anchors, settings, nil, capacityKWH, roundTripEff)
		assert.True(t, valuation > 0, "ending below reserve must produce a positive penalty cost")
	})

	t.Run("EndingAboveReserve_CreditedForBankedEnergy", func(t *testing.T) {
		t.Parallel()

		// Ending at 80% SOC (surplus energy banked in battery)
		finalState := planState{
			energyKWH: capacityKWH * 0.80,
			soc:       80,
		}

		valuation := calculateTerminalValuation(finalState, anchors, settings, nil, capacityKWH, roundTripEff)
		assert.True(t, valuation < 0, "ending above reserve must produce a negative cost (credit)")
	})

	t.Run("TerminalValuation_StrictConcavityAndPenalty", func(t *testing.T) {
		t.Parallel()

		// 1. Deficit penalty rate >= $1.00/kWh
		deficitState := planState{
			energyKWH: capacityKWH * 0.10, // 10% below 20% reserve
			soc:       10,
		}
		penaltyVal := calculateTerminalValuation(deficitState, anchors, settings, nil, capacityKWH, roundTripEff)
		deficitKWH := (capacityKWH * 0.20) - (capacityKWH * 0.10)
		assert.GreaterOrEqual(t, penaltyVal, deficitKWH*1.0, "Deficit penalty rate must be at least $1.00/kWh")

		// 2. Strict concavity: marginal value of first 4 hours of load displacement > marginal value of excess
		// With default avgLoadKW = 1.5, 4 hours = 6.0 kWh of surplus.
		targetEnergy := capacityKWH * 0.20 // 2.7 kWh
		stateAtPeakCap := planState{
			energyKWH: targetEnergy + 6.0,
			soc:       ((targetEnergy + 6.0) / capacityKWH) * 100.0,
		}
		stateAtExcess := planState{
			energyKWH: targetEnergy + 8.0,
			soc:       ((targetEnergy + 8.0) / capacityKWH) * 100.0,
		}
		stateAtReserve := planState{
			energyKWH: targetEnergy,
			soc:       20.0,
		}

		valReserve := calculateTerminalValuation(stateAtReserve, anchors, settings, nil, capacityKWH, roundTripEff)
		valPeakCap := calculateTerminalValuation(stateAtPeakCap, anchors, settings, nil, capacityKWH, roundTripEff)
		valExcess := calculateTerminalValuation(stateAtExcess, anchors, settings, nil, capacityKWH, roundTripEff)

		marginalCreditFirst6KWH := (-valPeakCap - -valReserve) / 6.0
		marginalCreditExcess2KWH := (-valExcess - -valPeakCap) / 2.0

		assert.Greater(t, marginalCreditFirst6KWH, marginalCreditExcess2KWH, "Terminal credit must exhibit strict concavity (diminishing returns)")
	})

	t.Run("FreeNights_ZeroPostHorizonRate_DoesNotCreditEndingAboveReserve", func(t *testing.T) {
		t.Parallel()

		zeroAnchors := planningAnchors{
			knownPostHorizonRate: 0.0, // Free nights / weekends ahead
		}

		peakTimeline := []planInterval{
			{
				importRate: 0.35, // High rate at end of horizon
				minSOC:     20.0,
			},
		}

		surplusState := planState{
			energyKWH: capacityKWH * 0.80, // 80% SOC (surplus above 20% reserve)
			soc:       80.0,
		}

		valuation := calculateTerminalValuation(surplusState, zeroAnchors, settings, peakTimeline, capacityKWH, roundTripEff)
		assert.InDelta(t, 0.0, valuation, 0.0001, "Free post-horizon electricity ($0) must not be overridden with timeline rate to credit surplus energy")
	})
}

// TestSearchOptimalPlan tests the tree rollout selection and inertia against lastAction.
func TestSearchOptimalPlan(t *testing.T) {
	t.Parallel()

	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)

	timeline := []planInterval{
		{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 2.0, minSOC: 20},
		{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.45, loadKWH: 2.0, minSOC: 20},
	}

	initialState := planState{
		energyKWH: 13.5 * 0.50,
		soc:       50,
	}

	status := types.SystemStatus{
		BatteryCapacityKWH: 13.5,
		BatterySOC:         50,
	}

	settings := types.Settings{
		MinBatterySOC:       20,
		GridChargeBatteries: true,
	}

	t.Run("PicksDischargeDuringPeakHour", func(t *testing.T) {
		t.Parallel()

		path, err := c.searchOptimalPlan(ctx, timeline, initialState, planningAnchors{}, settings, status, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, path.actions)

		// In hour 2 (peak rate $0.45), battery should discharge to cover home load
		assert.Equal(t, types.BatteryModeLoad, path.actions[1].batteryMode)
	})

	t.Run("Inertia_KeepsCurrentModeWhenDeltaSmall", func(t *testing.T) {
		t.Parallel()

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeStandby,
		}

		// When two modes are almost tied, inertia penalty ($0.05) keeps standby
		flatTimeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 0.5, minSOC: 20},
		}

		anchors := c.detectPlanningAnchors(flatTimeline, nil, status, settings)
		path, err := c.searchOptimalPlan(ctx, flatTimeline, initialState, anchors, settings, status, nil, lastAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, path.actions[0].batteryMode)
	})

	t.Run("VPPShortLeadTime_ChargesAsMuchAsPossibleWithoutPruningAll", func(t *testing.T) {
		t.Parallel()

		// Battery starts at 20% SOC. Deadline is at 1.0h.
		// Physically, charging at 5 kW for 1 hour into 13.5 kWh battery can only reach ~50% SOC.
		// Soft penalty must avoid "no viable plan found: all candidate paths were pruned" error.
		lowState := planState{
			energyKWH:   13.5 * 0.20,
			soc:         20,
			capacityKWH: 13.5,
		}

		vppAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(3 * time.Hour),
					deadline:   now.Add(time.Hour), // Deadline at end of interval 0
					vppSoc:     20,
					mandatory:  true,
				},
			},
		}

		path, err := c.searchOptimalPlan(ctx, timeline, lowState, vppAnchors, settings, status, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, path.actions)
		// Should choose to pre-charge toward the VPP deadline
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)
	})

	t.Run("VPPFeasibility_MultipleEventsInHorizon_OnlyClosestEventPenalized", func(t *testing.T) {
		t.Parallel()

		multiVPPTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 2.0,
				importRate:    0.20,
				loadKWH:       2.0,
				minSOC:        20.0,
			},
			{
				// Event 1 Active (hours 3-5)
				index:         2,
				startTime:     now.Add(3 * time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 2.0,
				importRate:    0.50,
				exportRate:    0.50,
				loadKWH:       2.0,
				minSOC:        20.0,
			},
			{
				// Between events (hours 5-6)
				index:         3,
				startTime:     now.Add(5 * time.Hour),
				endTime:       now.Add(6 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
		}

		multiVPPAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(3 * time.Hour),
					eventEnd:   now.Add(5 * time.Hour),
					deadline:   now.Add(time.Hour),
					vppSoc:     20.0,
					mandatory:  true,
				},
				{
					eventStart: now.Add(8 * time.Hour),
					eventEnd:   now.Add(10 * time.Hour),
					deadline:   now.Add(6 * time.Hour),
					vppSoc:     20.0,
					mandatory:  true,
				},
			},
		}

		fullState := planState{
			energyKWH:   13.5,
			soc:         100.0,
			capacityKWH: 13.5,
		}

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true

		path, err := c.searchOptimalPlan(ctx, multiVPPTimeline, fullState, multiVPPAnchors, exportSettings, status, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, path.actions)

		// During Event 1 (index 2), the planner must discharge as contracted down to 20%
		assert.Equal(t, types.BatteryModeExport, path.actions[2].batteryMode, "Must discharge during Event 1 without penalty from Event 2")
	})

	t.Run("Inertia_EnforcesMinimumFloorOnShortIntervals", func(t *testing.T) {
		t.Parallel()

		shortTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(5 * time.Minute),
				durationHours: 5.0 / 60.0,
				importRate:    0.20,
				loadKWH:       1.0 * (5.0 / 60.0),
				minSOC:        20,
			},
		}

		// Without lastAction, Load is chosen to serve 1 kW load at $0.20/kWh
		pathNoInertia, err := c.searchOptimalPlan(ctx, shortTimeline, initialState, planningAnchors{}, settings, status, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, pathNoInertia.actions[0].batteryMode)

		// With lastAction = Standby, the $0.03 inertia floor exceeds the ~$0.01 net benefit of discharging,
		// preventing rapid cycling on 5-minute intervals.
		lastAction := &types.Action{
			BatteryMode: types.BatteryModeStandby,
		}
		pathWithInertia, err := c.searchOptimalPlan(ctx, shortTimeline, initialState, planningAnchors{}, settings, status, nil, lastAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, pathWithInertia.actions[0].batteryMode)
	})

	t.Run("ExecuteLogs_InvokesDeferredLogOnWinningActions", func(t *testing.T) {
		t.Parallel()

		var loggedActions []string
		var unchosenActions []string
		p := &planPath{
			actions: []actionCandidate{
				{
					batteryMode: types.BatteryModeStandby,
					reason:      types.ActionReasonHoldSimilarPrice,
					logFn: func(ctx context.Context, selected bool) {
						if selected {
							loggedActions = append(loggedActions, "chosen:action0")
						} else {
							unchosenActions = append(unchosenActions, "unchosen:action0")
						}
					},
				},
				{
					batteryMode: types.BatteryModeLoad,
					logFn:       nil,
				},
				{
					batteryMode: types.BatteryModeChargeAny,
					logFn: func(ctx context.Context, selected bool) {
						if selected {
							loggedActions = append(loggedActions, "chosen:action2")
						}
					},
				},
			},
			initialCandidates: []actionCandidate{
				{
					batteryMode: types.BatteryModeStandby,
					reason:      types.ActionReasonHoldSimilarPrice,
				},
				{
					batteryMode: types.BatteryModeChargeAny,
					reason:      types.ActionReasonDeficitChargeNow,
					logFn: func(ctx context.Context, selected bool) {
						if !selected {
							unchosenActions = append(unchosenActions, "unchosen:chargeCandidate")
						}
					},
				},
			},
			modeScores: map[types.BatteryMode]float64{
				types.BatteryModeStandby:   1.0,
				types.BatteryModeChargeAny: 1.5,
			},
			bestScore: 1.0,
		}

		p.executeLogs(ctx)
		require.Len(t, loggedActions, 2)
		assert.Equal(t, []string{"chosen:action0", "chosen:action2"}, loggedActions)
		require.Len(t, unchosenActions, 1)
		assert.Equal(t, []string{"unchosen:chargeCandidate"}, unchosenActions)

		// Calling executeLogs on nil planPath must be safe
		var nilPath *planPath
		nilPath.executeLogs(ctx)
	})

	t.Run("FastTrackDelay_PromotesChargeAnyWhenDelayWithinThreshold", func(t *testing.T) {
		t.Parallel()

		// 10-minute step 0 followed by 60-minute step 1 at identical rate ($0.05).
		// Followed by peak at $0.50 for 2 hours.
		// Standby is preferred at step 0 due to inertia, but since step 1 charges
		// within 15 minutes at equal price, searchOptimalPlan promotes ChargeAny to step 0.
		fastTrackTimeline := []planInterval{
			{startTime: now, endTime: now.Add(10 * time.Minute), durationHours: 10.0 / 60.0, importRate: 0.05, loadKWH: 1.0 * (10.0 / 60.0), minSOC: 20},
			{startTime: now.Add(10 * time.Minute), endTime: now.Add(70 * time.Minute), durationHours: 1.0, importRate: 0.05, loadKWH: 1.0, minSOC: 20},
			{startTime: now.Add(70 * time.Minute), endTime: now.Add(190 * time.Minute), durationHours: 2.0, importRate: 0.50, loadKWH: 5.0, minSOC: 20},
		}

		statusWithCharge := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50.0,
			MaxBatteryChargeKW: 5.0,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeStandby,
		}

		fastTrackInitialState := planState{
			energyKWH:   13.5 * 0.50,
			soc:         50,
			capacityKWH: 13.5,
		}

		path, err := c.searchOptimalPlan(ctx, fastTrackTimeline, fastTrackInitialState, planningAnchors{}, settings, statusWithCharge, nil, lastAction)
		require.NoError(t, err)
		require.NotEmpty(t, path.actions)

		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, path.actions[0].reason)
		assert.Contains(t, path.actions[0].description, "Pre-charging")
		assert.Greater(t, path.metrics[0].gridImportKWH, 0.5)
		assert.Greater(t, path.states[1].soc, path.states[0].soc)
	})
}

// TestFinalizeDecisionAndPlan tests the synthesis of Decision and types.Plan from a winning path.
func TestFinalizeDecisionAndPlan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)
	currentPrice := types.Price{
		TSStart:       now,
		TSEnd:         now.Add(time.Hour),
		DollarsPerKWH: 0.12,
	}

	status := types.SystemStatus{
		BatteryCapacityKWH: 13.5,
		BatterySOC:         50,
	}

	timeline := []planInterval{
		{
			index:         0,
			startTime:     now,
			endTime:       now.Add(time.Hour),
			durationHours: 1.0,
			price:         currentPrice,
			minSOC:        20,
		},
		{
			index:         1,
			startTime:     now.Add(time.Hour),
			endTime:       now.Add(2 * time.Hour),
			durationHours: 1.0,
			price: types.Price{
				TSStart:       now.Add(time.Hour),
				TSEnd:         now.Add(2 * time.Hour),
				DollarsPerKWH: 0.45,
			},
			minSOC: 20,
		},
	}

	winningPath := &planPath{
		actions: []actionCandidate{
			{
				batteryMode: types.BatteryModeChargeAny,
				solarMode:   types.SolarModeAny,
				reason:      types.ActionReasonDeficitChargeNow,
				description: "Pre-charging from grid.",
				targetSOC:   95,
			},
			{
				batteryMode: types.BatteryModeLoad,
				solarMode:   types.SolarModeAny,
				reason:      types.ActionReasonSufficientBattery,
				description: "Covering peak load.",
				targetSOC:   20,
			},
		},
		states: []planState{
			{time: now, soc: 50, energyKWH: 6.75},
			{time: now.Add(time.Hour), soc: 85, energyKWH: 11.47},
			{time: now.Add(2 * time.Hour), soc: 70, energyKWH: 9.45},
		},
		metrics: []intervalMetrics{
			{gridImportKWH: 5.0, costDollars: 0.60, grossImportCostDollars: 0.60},
			{gridImportKWH: 0.0, costDollars: 0.00, grossImportCostDollars: 0.00},
		},
		totalCost: 0.60,
	}

	t.Run("ConstructsImmediateDecision", func(t *testing.T) {
		t.Parallel()

		decision, plan := finalizeDecisionAndPlan(winningPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true})

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeAny, decision.Action.SolarMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Equal(t, 95, decision.Action.ChargeToSOC)
		assert.Equal(t, timeline[0].endTime, decision.Action.TSScheduleModeUntil)

		assert.Len(t, plan.Periods, 2)
		assert.InDelta(t, 0.60, plan.TotalProjectedCost, 0.01)
		require.NotNil(t, decision.Action.Plan, "Action.Plan must be populated")
		assert.Equal(t, plan.Periods, decision.Action.Plan.Periods)
	})

	t.Run("PopulatesPlanPeriodSchedule", func(t *testing.T) {
		t.Parallel()

		_, plan := finalizeDecisionAndPlan(winningPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true})
		require.NotNil(t, plan)
		require.Len(t, plan.Periods, 2)

		p0 := plan.Periods[0]
		assert.Equal(t, now, p0.StartTime)
		assert.Equal(t, now.Add(time.Hour), p0.EndTime)
		assert.Equal(t, types.BatteryModeChargeAny, p0.BatteryMode)
		assert.Equal(t, 50.0, p0.StartSOC)
		assert.Equal(t, 85.0, p0.EndSOC)

		p1 := plan.Periods[1]
		assert.Equal(t, types.BatteryModeLoad, p1.BatteryMode)
		assert.Equal(t, 85.0, p1.StartSOC)
		assert.Equal(t, 70.0, p1.EndSOC)
	})

	t.Run("ExtendsScheduleUntilAcrossContiguousBlock", func(t *testing.T) {
		t.Parallel()

		multiChargePath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow},
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow},
			},
			states: winningPath.states,
			metrics: []intervalMetrics{
				{grossImportCostDollars: 0.50},
				{grossImportCostDollars: 0.50},
			},
		}

		decision, _ := finalizeDecisionAndPlan(multiChargePath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true})
		// Mode is identical across intervals 0 and 1, so scheduleUntil should extend to timeline[1].EndTime
		assert.Equal(t, timeline[1].endTime, decision.Action.TSScheduleModeUntil)
	})

	t.Run("StandbyFloorsFractionalSOC", func(t *testing.T) {
		t.Parallel()

		fractionalStatus := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50.8,
		}

		standbyPath := &planPath{
			actions: []actionCandidate{
				{
					batteryMode: types.BatteryModeStandby,
					solarMode:   types.SolarModeAny,
					reason:      types.ActionReasonAlwaysChargeBelowThreshold,
				},
			},
			states: []planState{
				{time: now, soc: 50.8, energyKWH: 13.5 * 0.508},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 1.0},
			},
		}

		decision, _ := finalizeDecisionAndPlan(standbyPath, timeline[:1], fractionalStatus, currentPrice, now, types.Settings{})
		assert.Equal(t, 0, decision.Action.ChargeToSOC, "Standby mode must not set ChargeToSOC; target SOC is implied")
	})

	t.Run("VPPActive_PreservesContractedReserveSOC", func(t *testing.T) {
		t.Parallel()

		vppPath := &planPath{
			actions: []actionCandidate{
				{
					batteryMode:        types.BatteryModeExport,
					reason:             types.ActionReasonVPPActive,
					targetSOC:          5,
					overrideReserveSOC: 5.0,
				},
			},
			states:  winningPath.states[:2],
			metrics: winningPath.metrics[:1],
		}

		decision, _ := finalizeDecisionAndPlan(vppPath, timeline[:1], status, currentPrice, now, types.Settings{MinBatterySOC: 20})
		assert.Equal(t, 5, decision.Action.ChargeToSOC, "Active VPP event with targetSOC=5 must preserve contracted reserve rather than overwriting with minSOC")
	})

	t.Run("DerivesTargetSOCFromTrajectoryWhenCandidateTargetIsZero", func(t *testing.T) {
		t.Parallel()

		unconstrainedPath := &planPath{
			actions: []actionCandidate{
				{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   types.SolarModeAny,
					reason:      types.ActionReasonVPPPrep,
					description: "Pre-charging for VPP.",
					targetSOC:   0, // Option B: unconstrained candidate
				},
				{
					batteryMode: types.BatteryModeStandby,
					solarMode:   types.SolarModeAny,
					reason:      types.ActionReasonVPPPrep,
					description: "Standby lock.",
					targetSOC:   65,
				},
			},
			states: []planState{
				{time: now, soc: 40, energyKWH: 5.4},
				{time: now.Add(time.Hour), soc: 65, energyKWH: 8.775},
				{time: now.Add(2 * time.Hour), soc: 100, energyKWH: 13.5}, // Rooftop solar filled remaining 35%
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 3.5, costDollars: 0.42, grossImportCostDollars: 0.42},
				{gridImportKWH: 0.0, costDollars: 0.00, grossImportCostDollars: 0.00},
			},
			totalCost: 0.42,
		}

		decision, _ := finalizeDecisionAndPlan(unconstrainedPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true})

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, 65, decision.Action.ChargeToSOC, "ChargeToSOC must be derived from peak SOC in charging episode (65%), not blindly set to 100%")
	})

	t.Run("DischargeAtPeak_AssignsReasonWhenDischargingAtPeakRate", func(t *testing.T) {
		t.Parallel()

		peakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
		}

		peakPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
			},
			states: []planState{
				{time: now, soc: 80.0, energyKWH: 10.8},
				{time: now.Add(time.Hour), soc: 60.0, energyKWH: 8.1},
				{time: now.Add(2 * time.Hour), soc: 40.0, energyKWH: 5.4},
			},
			metrics: []intervalMetrics{
				{batSuppliedHomeKWH: 2.0},
				{batSuppliedHomeKWH: 2.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(peakPath, peakTimeline, status, currentPrice, now, types.Settings{})
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Action.Reason, "Step 0 at peak rate ($0.45) must be labeled DischargeAtPeak")
		assert.Contains(t, decision.Action.Description, "peak rate")
		assert.Equal(t, types.ActionReasonDischargeAtPeak, plan.Periods[0].Reason)
		assert.Equal(t, types.ActionReasonSufficientBattery, plan.Periods[1].Reason, "Step 1 off-peak ($0.15) must be labeled standard SufficientBattery")
	})

	t.Run("WaitingToCharge_AssignsReasonWhenStandbyBeforeScheduledCharge", func(t *testing.T) {
		t.Parallel()

		chargeTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				minSOC:        20.0,
			},
		}

		chargePath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonDeficitSaveForPeak},
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 100},
			},
			states: []planState{
				{time: now, soc: 30.0, energyKWH: 4.05},
				{time: now.Add(time.Hour), soc: 30.0, energyKWH: 4.05},
				{time: now.Add(2 * time.Hour), soc: 80.0, energyKWH: 10.8},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 1.0},
				{gridImportKWH: 5.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(chargePath, chargeTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true})
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision.Action.Reason, "Standby before scheduled cheaper charge must be labeled WaitingToCharge")
		assert.Contains(t, decision.Action.Description, "Waiting to charge at")
		assert.Equal(t, types.ActionReasonWaitingToCharge, plan.Periods[0].Reason)
	})

	t.Run("SufficientBatteryTillCharge_AssignsReasonWhenLoadCanReachCharge", func(t *testing.T) {
		t.Parallel()

		reachTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				minSOC:        20.0,
			},
		}

		reachPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 100},
			},
			states: []planState{
				{time: now, soc: 80.0, energyKWH: 10.8},
				{time: now.Add(time.Hour), soc: 60.0, energyKWH: 8.1}, // well above reserve (20%)
				{time: now.Add(2 * time.Hour), soc: 100.0, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{batSuppliedHomeKWH: 2.0},
				{gridImportKWH: 5.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(reachPath, reachTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true})
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decision.Action.Reason, "Discharging when battery easily reaches scheduled charge must be labeled SufficientBatteryTillCharge")
		assert.Contains(t, decision.Action.Description, "Sufficient battery to reach scheduled charge")
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, plan.Periods[0].Reason)
	})

	t.Run("HoldSimilarPrice_AssignsReasonWhenStandbyTonightWithSolarRefillTomorrow", func(t *testing.T) {
		t.Parallel()

		solarTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				exportRate:    0.075,
				solarKWH:      4.0,
				minSOC:        20.0,
			},
		}

		solarPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonHoldSimilarPrice},
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
			},
			states: []planState{
				{time: now, soc: 70.0, energyKWH: 9.45},
				{time: now.Add(time.Hour), soc: 70.0, energyKWH: 9.45},
				{time: now.Add(11 * time.Hour), soc: 100.0, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 1.0},
				{batSuppliedHomeKWH: 1.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(solarPath, solarTimeline, status, currentPrice, now, types.Settings{
			GridExportSolar:                      true,
			MinExportHoldDifferenceDollarsPerKWH: 0.02,
		})
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, decision.Action.Reason, "Standby tonight when import price ~= tomorrow solar export credit must be labeled HoldSimilarPrice")
		assert.Contains(t, decision.Action.Description, "Preserving battery in standby for daytime solar export")
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, plan.Periods[0].Reason)
	})

	t.Run("ArbitrageHoldExport_AssignsReasonWhenStandbyAheadOfHighExportWindow", func(t *testing.T) {
		t.Parallel()

		exportTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.04,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 1.0,
				importRate:    0.40,
				exportRate:    0.75, // massive export rate
				minSOC:        20.0,
			},
		}

		exportPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonDeficitSaveForPeak},
				{batteryMode: types.BatteryModeExport, reason: types.ActionReasonDirectExport},
			},
			states: []planState{
				{time: now, soc: 80.0, energyKWH: 10.8},
				{time: now.Add(time.Hour), soc: 80.0, energyKWH: 10.8},
				{time: now.Add(5 * time.Hour), soc: 25.0, energyKWH: 3.375},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 1.0},
				{batExportKWH: 6.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(exportPath, exportTimeline, status, currentPrice, now, types.Settings{
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
		})
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonArbitrageHoldExport, decision.Action.Reason, "Standby ahead of lucrative grid export window must be labeled ArbitrageHoldExport")
		assert.Contains(t, decision.Action.Description, "upcoming export window")
		assert.Equal(t, types.ActionReasonArbitrageHoldExport, plan.Periods[0].Reason)
	})

	t.Run("TruePeak_AssignsDischargeAtPeak_EvenWhenChargeScheduled", func(t *testing.T) {
		t.Parallel()

		// 17:00 TOU Peak ($0.55/kWh), followed by midnight charge ($0.08/kWh).
		// Spread is 0.47 >= 0.10 minPeakRateSpreadDollars.
		peakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.55,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(6 * time.Hour),
				endTime:       now.Add(7 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				minSOC:        20.0,
			},
		}

		peakPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 100},
			},
			states: []planState{
				{time: now, soc: 80.0, energyKWH: 10.8},
				{time: now.Add(time.Hour), soc: 60.0, energyKWH: 8.1},
				{time: now.Add(7 * time.Hour), soc: 100.0, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{batSuppliedHomeKWH: 2.0},
				{gridImportKWH: 5.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(peakPath, peakTimeline, status, types.Price{DollarsPerKWH: 0.55}, now, types.Settings{
			GridChargeBatteries:                    true,
			MinDeficitPriceDifferenceDollarsPerKWH: 0.08,
		})
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDischargeAtPeak, decision.Action.Reason, "During a genuine TOU peak, reason must be DischargeAtPeak rather than SufficientBatteryTillCharge")
		assert.Contains(t, decision.Action.Description, "Discharging battery to power home during peak rate")
		assert.Equal(t, types.ActionReasonDischargeAtPeak, plan.Periods[0].Reason)
	})

	t.Run("ArbitrageChargeExport_RequiresActualExportInPlan", func(t *testing.T) {
		t.Parallel()

		// High solar forecast tomorrow, but 100% of solar is self-consumed by heavy home load (0 export).
		// Pre-charging overnight to avoid peak imports must NOT be mislabeled as ArbitrageChargeExport.
		arbTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 1.0,
				importRate:    0.40,
				exportRate:    0.25, // export rate > 0.10 + 0.05
				solarKWH:      6.0,  // high solar
				minSOC:        20.0,
			},
		}

		arbPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 100},
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak},
			},
			states: []planState{
				{time: now, soc: 20.0, energyKWH: 2.7},
				{time: now.Add(time.Hour), soc: 80.0, energyKWH: 10.8},
				{time: now.Add(5 * time.Hour), soc: 40.0, energyKWH: 5.4},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 8.1},
				// Zero grid export! Home consumes all solar.
				{batSuppliedHomeKWH: 4.0, gridExportKWH: 0.0},
			},
		}

		decision, plan := finalizeDecisionAndPlan(arbPath, arbTimeline, status, types.Price{DollarsPerKWH: 0.10}, now, types.Settings{
			GridChargeBatteries:                 true,
			GridExportSolar:                     true,
			MinArbitrageDifferenceDollarsPerKWH: 0.05,
		})
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason, "Must label as DeficitChargeNow when no actual solar export occurs in the plan")
		assert.Contains(t, decision.Action.Description, "Pre-charging for upcoming peak rates")
		assert.Equal(t, types.ActionReasonDeficitChargeNow, plan.Periods[0].Reason)
	})
}

// TestPlanScenarios executes end-to-end multi-hour deterministic scenarios across various utilities and operating modes.
func TestPlanScenarios(t *testing.T) {
	t.Parallel()

	chicagoLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	laLoc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	nyLoc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	c := NewController()
	ctx := context.Background()

	t.Run("ComEd_SummerHeatwave_PeakDischarge", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 20, 8, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.06,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 9; h < 32; h++ {
			day := 20
			hour := h
			if hour >= 24 {
				day = 21
				hour -= 24
			}
			rate := 0.08
			if hour >= 17 && hour <= 19 {
				rate = 0.85 // 5pm - 7pm peak price spike!
			} else if hour >= 0 && hour <= 6 {
				rate = 0.04 // overnight off-peak
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 7, day, hour, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 7, day, hour+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.NotEmpty(t, decision.Action.Description)

		// Verify that during the 5pm-7pm spike hours, battery discharges to cover home load
		spikeStart := time.Date(2026, 7, 20, 17, 0, 0, 0, chicagoLoc)
		foundSpikeDischarge := false
		for _, period := range plan.Periods {
			if period.StartTime.Equal(spikeStart) {
				assert.Equal(t, types.BatteryModeLoad, period.BatteryMode, "must discharge battery during $0.85/kWh price spike")
				foundSpikeDischarge = true
				break
			}
		}
		assert.True(t, foundSpikeDischarge)
	})

	t.Run("ComEd_NegativePrice_UnconditionalCharge", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 4, 10, 13, 0, 0, 0, chicagoLoc)

		// Delivered rate is negative: -0.05 + 0.03 = -0.02
		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        -0.05,
			GridUseDollarsPerKWH: 0.03,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         40,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}

		var futurePrices []types.Price
		for h := 14; h < 18; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 4, 10, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 4, 10, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.03,
			})
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Must unconditionally charge
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, decision.Action.Reason)
		assert.Equal(t, 100, decision.Action.ChargeToSOC)
	})

	t.Run("Ameren_MidnightPriceTruncation_PlansOverAvailableHours", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 5, 12, 11, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.07,
			GridUseDollarsPerKWH: 0.04,
		}

		// Prices truncate at midnight
		var futurePrices []types.Price
		for h := 12; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 5, 12, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 5, 12, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.08,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.NotEmpty(t, decision.Action.Description)

		// Horizon plans over available hours ending at midnight (May 13th 00:00)
		lastPeriod := plan.Periods[len(plan.Periods)-1]
		assert.Equal(t, time.Date(2026, 5, 13, 0, 0, 0, 0, chicagoLoc), lastPeriod.EndTime)
	})

	t.Run("PGE_EELEC_SummerSolarSurplus_4to9pmPeak", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 1, 8, 0, 0, 0, laLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.28,
			GridUseDollarsPerKWH: 0.05,
		}

		var futurePrices []types.Price
		for h := 9; h < 32; h++ {
			day := 1
			hour := h
			if hour >= 24 {
				day = 2
				hour -= 24
			}
			rate := 0.28 // off-peak
			if hour >= 16 && hour < 21 {
				rate = 0.62 // 4pm - 9pm peak!
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 8, day, hour, 0, 0, 0, laLoc),
				TSEnd:                time.Date(2026, 8, day, hour+1, 0, 0, 0, laLoc),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Los_Angeles",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.NotEmpty(t, decision.Action.Description)

		// 4pm - 9pm should discharge battery to avoid $0.67 delivered grid import
		peakTime := time.Date(2026, 8, 1, 16, 0, 0, 0, laLoc)
		foundPeakDischarge := false
		for _, p := range plan.Periods {
			if p.StartTime.Equal(peakTime) {
				assert.Equal(t, types.BatteryModeLoad, p.BatteryMode)
				foundPeakDischarge = true
				break
			}
		}
		assert.True(t, foundPeakDischarge)
	})

	t.Run("SCE_TOUDPRIME_DirectSolarExport_ManageTOU", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 10, 12, 0, 0, 0, laLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.22,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 13; h < 24; h++ {
			p := types.Price{
				TSStart:                       time.Date(2026, 7, 10, h, 0, 0, 0, laLoc),
				TSEnd:                         time.Date(2026, 7, 10, h+1, 0, 0, 0, laLoc),
				DollarsPerKWH:                 0.22,
				GridUseDollarsPerKWH:          0.04,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: 0.40, // High solar export credit!
			}
			futurePrices = append(futurePrices, p)
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         65,
			TimeLocation:       "America/Los_Angeles",
			SolarKW:            4.5,
			HomeKW:             1.2,
		}

		settings := types.Settings{
			MinBatterySOC:      20,
			GridExportSolar:    true,
			ManageTOUSchedules: true, // Direct solar export enabled!
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// When direct solar export is active, solar mode can be SolarModeExport
		assert.NotEmpty(t, decision.Action.Description)
	})

	t.Run("SingleVPPEvent_Reaches100PctAtTMinus2h", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 15, 10, 0, 0, 0, chicagoLoc)

		vppStart := time.Date(2026, 8, 15, 16, 0, 0, 0, chicagoLoc) // 4:00 PM
		vppEnd := time.Date(2026, 8, 15, 19, 0, 0, 0, chicagoLoc)   // 7:00 PM

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.10,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 11; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 8, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         40,
			TimeLocation:       "America/Chicago",
			VPPEvents: []types.VPPEvent{
				{
					TSStart:   vppStart,
					TSEnd:     vppEnd,
					VPPSoc:    20,
					Mandatory: true,
				},
			},
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.NotEmpty(t, decision.Action.Description)

		// Between 2:00 PM (T-2h) and 4:00 PM (event start), battery must be in Standby
		standbyTime := time.Date(2026, 8, 15, 14, 0, 0, 0, chicagoLoc)
		for _, p := range plan.Periods {
			if p.StartTime.Equal(standbyTime) {
				assert.Equal(t, types.BatteryModeStandby, p.BatteryMode, "must hold Standby 2 hours before VPP event")
				assert.Equal(t, types.ActionReasonVPPPrep, p.Reason)
			}
			if p.StartTime.Equal(vppStart) {
				assert.Equal(t, types.ActionReasonVPPActive, p.Reason, "must discharge during VPP event")
			}
		}
	})

	t.Run("TeslaElectric_ERCOT_SuperSpikeDump", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 10, 16, 0, 0, 0, chicagoLoc)

		// Wholesale export rate surges to $2.50/kWh!
		currentPrice := types.Price{
			TSStart:                       now,
			TSEnd:                         now.Add(time.Hour),
			DollarsPerKWH:                 0.18,
			GridUseDollarsPerKWH:          0.04,
			SeparateGenerationCredit:      true,
			GenerationCreditDollarsPerKWH: 2.50,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         80,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			ManageTOUSchedules:  true,
			MinBatterySOC:       20,
			GridExportBatteries: true,
		}

		var futurePrices []types.Price
		for h := 18; h < 22; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 8, 10, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 10, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.12,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Super-spike dumps battery to grid
		assert.Equal(t, types.BatteryModeExport, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDirectExport, decision.Action.Reason)
	})

	t.Run("EVCharging_ActiveSession_EnforcesStandby", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 6, 15, 23, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.06,
			GridUseDollarsPerKWH: 0.04,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         75,
			HomeKW:             8.2, // EV charger drawing 8.2 kW!
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
			EVChargingPeriods: []types.TimePeriod{
				{
					Start: time.Date(2026, 6, 15, 22, 0, 0, 0, chicagoLoc),
					End:   time.Date(2026, 6, 16, 6, 0, 0, 0, chicagoLoc),
				},
			},
		}

		var futurePrices []types.Price
		for h := 0; h < 4; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 16, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 16, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.06,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, decision.Action.Reason)
	})

	t.Run("ConEd_NY_StrictReserveFloor_HeavyWinterLoad", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 1, 15, 18, 0, 0, 0, nyLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.35,
			GridUseDollarsPerKWH: 0.08,
		}

		// Battery is at 35% (exactly at the 35% reserve floor)
		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         35,
			HomeKW:             4.5,
			TimeLocation:       "America/New_York",
		}

		settings := types.Settings{
			MinBatterySOC: 35,
		}

		var futurePrices []types.Price
		for h := 19; h < 23; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 1, 15, h, 0, 0, 0, nyLoc),
				TSEnd:                time.Date(2026, 1, 15, h+1, 0, 0, 0, nyLoc),
				DollarsPerKWH:        0.35,
				GridUseDollarsPerKWH: 0.08,
			})
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Must protect reserve floor
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
	})

	t.Run("MultiVPP_BackToBackEvents", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 20, 8, 0, 0, 0, chicagoLoc)

		vpp1Start := time.Date(2026, 8, 20, 12, 0, 0, 0, chicagoLoc)
		vpp1End := time.Date(2026, 8, 20, 14, 0, 0, 0, chicagoLoc)

		vpp2Start := time.Date(2026, 8, 20, 19, 0, 0, 0, chicagoLoc)
		vpp2End := time.Date(2026, 8, 20, 21, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.08,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 9; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 8, 20, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 20, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.08,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         100,
			TimeLocation:       "America/Chicago",
			VPPEvents: []types.VPPEvent{
				{
					TSStart:   vpp1Start,
					TSEnd:     vpp1End,
					VPPSoc:    30,
					Mandatory: true,
				},
				{
					TSStart:   vpp2Start,
					TSEnd:     vpp2End,
					VPPSoc:    30,
					Mandatory: true,
				},
			},
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.NotEmpty(t, decision.Action.Description)

		// Check that both VPP events have their 2-hour standby lock before event start
		// Event 1 lock: 10:00 AM (12:00 - 2h)
		// Event 2 lock: 17:00 PM (19:00 - 2h)
		lock1Time := time.Date(2026, 8, 20, 10, 0, 0, 0, chicagoLoc)
		lock2Time := time.Date(2026, 8, 20, 17, 0, 0, 0, chicagoLoc)

		for _, p := range plan.Periods {
			if p.StartTime.Equal(lock1Time) || p.StartTime.Equal(lock2Time) {
				assert.Equal(t, types.BatteryModeStandby, p.BatteryMode, "must hold Standby 2h before VPP event at %s", p.StartTime)
				assert.Equal(t, types.ActionReasonVPPPrep, p.Reason)
			}
			if p.StartTime.Equal(vpp1Start) || p.StartTime.Equal(vpp2Start) {
				assert.Equal(t, types.ActionReasonVPPActive, p.Reason, "must discharge during VPP event at %s", p.StartTime)
			}
		}
	})

	t.Run("WinterLowSolar_DeficitPreCharging", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 1, 20, 3, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.04,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 4; h < 24; h++ {
			rate := 0.08
			if h >= 17 && h <= 20 {
				rate = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 1, 20, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 1, 20, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         25,
			HomeKW:             1.5,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
	})

	t.Run("ZeroExportCurtailment_ChargesFromSolar", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 6, 20, 12, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.12,
			GridUseDollarsPerKWH: 0.04,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			SolarKW:            6.0,
			HomeKW:             1.0,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:   20,
			GridExportSolar: false,
		}

		var futurePrices []types.Price
		for h := 13; h < 17; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 20, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 20, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.12,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("InertiaPenalty_AvoidsChatterOnSmallDelta", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 5, 10, 14, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.10,
			GridUseDollarsPerKWH: 0.04,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         60,
			HomeKW:             0.2,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeStandby,
		}

		var futurePrices []types.Price
		for h := 15; h < 19; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 5, 10, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 5, 10, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, lastAction)
		require.NoError(t, err)
		require.NotNil(t, plan)

		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
	})

	t.Run("ProfileComparison_ConservativeVsAggressive", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 10, 16, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.30,
			GridUseDollarsPerKWH: 0.04,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         28,
			HomeKW:             2.0,
			TimeLocation:       "America/Chicago",
		}

		settingsConservative := types.Settings{
			MinBatterySOC:       20,
			OptimizationProfile: "conservative",
		}

		var futurePrices []types.Price
		for h := 17; h < 21; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 7, 10, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 7, 10, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.30,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		decisionCons, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settingsConservative, nil)
		require.NoError(t, err)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decisionCons.Action.Reason)

		settingsAggressive := types.Settings{
			MinBatterySOC:       20,
			OptimizationProfile: "aggressive",
		}

		decisionAggr, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settingsAggressive, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decisionAggr.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBattery, decisionAggr.Action.Reason)
	})

	t.Run("TailEndBatteryDump_BeforeNightRatePlummet", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 25, 19, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:                       now,
			TSEnd:                         now.Add(time.Hour),
			DollarsPerKWH:                 0.15,
			GridUseDollarsPerKWH:          0.04,
			SeparateGenerationCredit:      true,
			GenerationCreditDollarsPerKWH: 0.45,
		}

		var futurePrices []types.Price
		for h := 20; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:                       time.Date(2026, 8, 25, h, 0, 0, 0, chicagoLoc),
				TSEnd:                         time.Date(2026, 8, 25, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:                 0.08,
				GridUseDollarsPerKWH:          0.04,
				SeparateGenerationCredit:      true,
				GenerationCreditDollarsPerKWH: 0.03,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         85,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			ManageTOUSchedules:  true,
			MinBatterySOC:       20,
			GridExportBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		assert.Equal(t, types.BatteryModeExport, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDirectExport, decision.Action.Reason)
	})

	t.Run("FastTrackDelay_StartsChargeImmediately", func(t *testing.T) {
		t.Parallel()

		// 7:50 AM: 10 minutes before an 8:00 AM charge window.
		// Off-peak rate ($0.05) is identical at 7:50 and 8:00. Peak ($0.50) begins at 9:00 AM.
		// Battery needs charging to survive the upcoming 9:00 AM peak.
		now := time.Date(2026, 8, 25, 7, 50, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                time.Date(2026, 8, 25, 8, 0, 0, 0, chicagoLoc),
			DollarsPerKWH:        0.05,
			GridUseDollarsPerKWH: 0.02,
		}

		futurePrices := []types.Price{
			{
				TSStart:              time.Date(2026, 8, 25, 8, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 25, 9, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.05,
				GridUseDollarsPerKWH: 0.02,
			},
			{
				TSStart:              time.Date(2026, 8, 25, 9, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 25, 11, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.50,
				GridUseDollarsPerKWH: 0.05,
			},
			{
				TSStart:              time.Date(2026, 8, 25, 11, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 25, 24, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.02,
			},
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 10.0,
			BatterySOC:         50,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             3.5,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeStandby,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, lastAction)
		require.NoError(t, err)
		require.NotNil(t, plan)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode,
			"must fast-track charge immediately at 7:50 AM instead of delaying to 8:00 AM due to 20-25 min polling cycle")
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason,
			"reason must reflect underlying deficit charge need")
		assert.Contains(t, decision.Action.Description, "Pre-charging",
			"description must explain why the battery is charging")
	})

	t.Run("FlatOffPeak_DelaysChargingJustInTimeBeforePeak", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 20, 0, 0, 0, 0, chicagoLoc) // Midnight 00:00

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.04,
			GridUseDollarsPerKWH: 0.02,
		}

		var futurePrices []types.Price
		for h := 1; h < 24; h++ {
			rate := 0.04 // Flat off-peak overnight ($0.06 delivered)
			if h >= 6 && h < 10 {
				rate = 0.45 // Morning peak ($0.47 delivered)
			} else if h >= 10 {
				rate = 0.08
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 7, 20, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 7, 20, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.02,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             1.5,
			SolarKW:            0.0,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// At midnight, planner should standby and wait rather than charging 6 hours prematurely
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode,
			"must standby at midnight instead of charging early when off-peak rate is flat until 6 AM")

		// Later in the night (closer to 6 AM), plan periods must schedule the charge
		foundPlannedCharge := false
		for _, p := range plan.Periods {
			if p.StartTime.Hour() >= 4 && p.StartTime.Hour() < 6 && p.BatteryMode == types.BatteryModeChargeAny {
				foundPlannedCharge = true
				break
			}
		}
		assert.True(t, foundPlannedCharge, "must schedule charging just-in-time in the hours leading up to 6 AM peak")
	})

	t.Run("VPPPrep_MultiHourCharging_SelectsCheapestHoursNeededToReachTarget", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 15, 10, 0, 0, 0, chicagoLoc)      // 10:00 AM
		vppStart := time.Date(2026, 8, 15, 16, 0, 0, 0, chicagoLoc) // 4:00 PM (16:00)
		vppEnd := time.Date(2026, 8, 15, 19, 0, 0, 0, chicagoLoc)   // 7:00 PM (19:00)
		deadline := time.Date(2026, 8, 15, 14, 0, 0, 0, chicagoLoc) // 2:00 PM (14:00, 2h standby lead time)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.18, // 10:00 - 11:00: $0.18
			GridUseDollarsPerKWH: 0.02,
		}

		var futurePrices []types.Price
		for h := 11; h < 24; h++ {
			rate := 0.18
			switch h {
			case 11:
				rate = 0.18 // 11:00 - 12:00: $0.18
			case 12:
				rate = 0.12 // 12:00 - 13:00: $0.12 (2nd cheapest window!)
			case 13:
				rate = 0.10 // 13:00 - 14:00: $0.10 (cheapest, but only 1 hour!)
			case 14, 15:
				rate = 0.15 // 14:00 - 16:00: 2h standby lock
			case 16, 17, 18:
				rate = 0.50 // 16:00 - 19:00: VPP active event
			default:
				rate = 0.10
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 8, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.02,
			})
		}

		// Battery starts at 30% SOC (needs 2 full hours of 5 kW charging to reach 100%)
		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         30.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             1.0,
			SolarKW:            0.0,
			TimeLocation:       "America/Chicago",
			VPPEvents: []types.VPPEvent{
				{
					TSStart:   vppStart,
					TSEnd:     vppEnd,
					VPPSoc:    20,
					Mandatory: true,
				},
			},
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// At 10:00 AM, controller should standby and wait for cheaper upcoming hours (12:00 and 13:00)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode,
			"must standby at 10:00 AM when cheaper pre-VPP charging hours exist ahead")

		// Periods 12:00-13:00 ($0.12) and 13:00-14:00 ($0.10) must BOTH be scheduled for charging
		// because the cheapest 1-hour window ($0.10) alone is insufficient to reach 100% by deadline
		chargedHour12 := false
		chargedHour13 := false
		standbyAtDeadline := false
		for _, p := range plan.Periods {
			if p.StartTime.Hour() == 12 && p.BatteryMode == types.BatteryModeChargeAny {
				chargedHour12 = true
			}
			if p.StartTime.Hour() == 13 && p.BatteryMode == types.BatteryModeChargeAny {
				chargedHour13 = true
			}
			if p.StartTime.Equal(deadline) && p.BatteryMode == types.BatteryModeStandby {
				standbyAtDeadline = true
				assert.GreaterOrEqual(t, p.StartSOC, 99.0, "must reach full 100%% SOC by 14:00 deadline")
			}
		}
		assert.True(t, chargedHour12, "must charge during 12:00-13:00 to satisfy 2-hour charging requirement")
		assert.True(t, chargedHour13, "must charge during 13:00-14:00 (cheapest hour)")
		assert.True(t, standbyAtDeadline, "must hold Standby at 14:00 deadline with battery at 100%%")
	})

	t.Run("ReturnsSimulationParams", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 20, 12, 0, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
			DollarsPerKWH: 0.10,
		}
		var futurePrices []types.Price
		for h := 1; h <= 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       now.Add(time.Duration(h) * time.Hour),
				TSEnd:         now.Add(time.Duration(h+1) * time.Hour),
				DollarsPerKWH: 0.10,
			})
		}
		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}
		settings := types.Settings{
			MinBatterySOC: 20,
		}
		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart:  now.AddDate(0, 0, -d).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:      1.5,
					SolarKWH:     0.5,
					TimeLocation: "America/Chicago",
				})
			}
		}

		decision, _, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, "none", decision.SimulationParams.DetectedShift)
		assert.Equal(t, decision.SimulationParams, decision.Action.SimulationParams)
	})

	t.Run("PrioritizesContiguousChargingOverFragmented", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 7, 20, 0, 0, 0, 0, chicagoLoc)

		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.05,
			GridUseDollarsPerKWH: 0.02,
		}

		var futurePrices []types.Price
		for h := 1; h <= 12; h++ {
			rate := 0.05 // Flat off-peak overnight (01:00 - 06:00)
			if h >= 7 && h <= 10 {
				rate = 0.50 // Peak morning window
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              now.Add(time.Duration(h) * time.Hour),
				TSEnd:                now.Add(time.Duration(h+1) * time.Hour),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.02,
			})
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         20.0,
			MaxBatteryChargeKW: 3.3,
			HomeKW:             1.5,
			SolarKW:            0.0,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.NotEmpty(t, decision.Action.Description)

		// Collect all charging intervals
		var chargePeriods []types.PlanPeriod
		for _, p := range plan.Periods {
			if p.BatteryMode == types.BatteryModeChargeAny {
				chargePeriods = append(chargePeriods, p)
			}
		}

		require.GreaterOrEqual(t, len(chargePeriods), 2, "must schedule charging intervals to cover peak")
		// Verify that all charge intervals are strictly contiguous (no gaps / start-stop chattering)
		for i := 1; i < len(chargePeriods); i++ {
			assert.Equal(t, chargePeriods[i-1].EndTime, chargePeriods[i].StartTime, "charging intervals must be contiguous without alternating fragmentation")
		}
	})
}

func BenchmarkSearchOptimalPlan(b *testing.B) {
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
		_, err := c.searchOptimalPlan(ctx, timeline, initialState, planningAnchors{}, settings, status, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlan(b *testing.B) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

	currentPrice := types.Price{
		TSStart:              now,
		TSEnd:                now.Add(time.Hour),
		DollarsPerKWH:        0.08,
		GridUseDollarsPerKWH: 0.04,
	}

	futurePrices := make([]types.Price, 24)
	for i := 0; i < 24; i++ {
		tStep := now.Add(time.Duration(i) * time.Hour)
		rate := 0.08
		if i >= 9 && i <= 12 {
			rate = 0.45 // peak
		}
		futurePrices[i] = types.Price{
			TSStart:              tStep,
			TSEnd:                tStep.Add(time.Hour),
			DollarsPerKWH:        rate,
			GridUseDollarsPerKWH: 0.04,
		}
	}

	status := types.SystemStatus{
		Timestamp:          now,
		BatteryCapacityKWH: 13.5,
		BatterySOC:         50.0,
		HomeKW:             1.5,
		SolarKW:            0.0,
	}

	settings := types.Settings{
		MinBatterySOC:       20,
		GridChargeBatteries: true,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlan_FullScenario(b *testing.B) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

	currentPrice := types.Price{
		TSStart:              now,
		TSEnd:                now.Add(time.Hour),
		DollarsPerKWH:        0.08,
		GridUseDollarsPerKWH: 0.04,
	}

	futurePrices := make([]types.Price, 24)
	for i := 0; i < 24; i++ {
		tStep := now.Add(time.Duration(i) * time.Hour)
		rate := 0.08
		if i >= 9 && i <= 13 {
			rate = 0.50 // peak
		}
		futurePrices[i] = types.Price{
			TSStart:              tStep,
			TSEnd:                tStep.Add(time.Hour),
			DollarsPerKWH:        rate,
			GridUseDollarsPerKWH: 0.04,
		}
	}

	// 24 hours of mock history and weather
	history := make([]types.EnergyStats, 24)
	var forecastHours []types.HourlyWeather
	for i := 0; i < 24; i++ {
		tStep := now.Add(time.Duration(i) * time.Hour)
		solar := 0.0
		if i >= 2 && i <= 10 {
			solar = 4.0 // 4 kW solar generation mid-day
		}
		history[i] = types.EnergyStats{
			TSHourStart: tStep,
			HomeKWH:     1.8,
			SolarKWH:    solar,
		}
		forecastHours = append(forecastHours, types.HourlyWeather{
			TSHourStart: tStep,
			GTI:         100.0 * solar,
		})
	}

	weather := []types.Weather{
		{
			TSDayStart:    now.Truncate(24 * time.Hour),
			ForecastHours: forecastHours,
		},
	}

	status := types.SystemStatus{
		Timestamp:          now,
		BatteryCapacityKWH: 13.5,
		BatterySOC:         35.0,
		HomeKW:             1.8,
		SolarKW:            0.0,
	}

	settings := types.Settings{
		MinBatterySOC:                           20,
		GridChargeBatteries:                     true,
		GridExportSolar:                         true,
		ManageTOUSchedules:                      true,
		GridExportBatteries:                     true,
		MinBatteryExportDifferenceDollarsPerKWH: 0.08,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, err := c.Plan(ctx, status, currentPrice, futurePrices, history, weather, settings, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}
