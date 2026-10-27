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

		timeline, simParams, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, settings, status, nil)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)
		assert.Equal(t, "none", simParams.DetectedShift)

		// With >=40m interval splitting, now (10:15) to top of hour (11:00) is 45m (>= 40m), splitting off 20m at 10:35
		assert.Equal(t, now, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 10, 35, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 20.0/60.0, timeline[0].durationHours, 0.01)

		// Remaining 10:35 to 11:00 is 25m (< 40m)
		if len(timeline) > 1 {
			assert.InDelta(t, 25.0/60.0, timeline[1].durationHours, 0.01)
			assert.Equal(t, time.Date(2026, 6, 15, 10, 35, 0, 0, chicagoLoc), timeline[1].startTime)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc), timeline[1].endTime)
		}
		// Subsequent intervals should be 20-minute blocks (:20, :40, :00)
		if len(timeline) > 2 {
			assert.InDelta(t, 20.0/60.0, timeline[2].durationHours, 0.01)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc), timeline[2].startTime)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 20, 0, 0, chicagoLoc), timeline[2].endTime)
		}
		if len(timeline) > 3 {
			assert.InDelta(t, 20.0/60.0, timeline[3].durationHours, 0.01)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 20, 0, 0, chicagoLoc), timeline[3].startTime)
			assert.Equal(t, time.Date(2026, 6, 15, 11, 40, 0, 0, chicagoLoc), timeline[3].endTime)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, startHour, halfHourPrice1, future30MinPrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5}, nil)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, startHour, rate45Price, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5}, nil)
		require.NoError(t, err)
		require.True(t, len(timeline) >= 3)

		// 14:00 to 14:45 is 45m (>= 40m), so it chops off 20m into 14:00-14:20 (20m) and 14:20-14:45 (25m) intervals
		assert.Equal(t, startHour, timeline[0].startTime)
		assert.Equal(t, startHour.Add(20*time.Minute), timeline[0].endTime)
		assert.InDelta(t, 20.0/60.0, timeline[0].durationHours, 0.01)

		assert.Equal(t, startHour.Add(20*time.Minute), timeline[1].startTime)
		assert.Equal(t, startHour.Add(45*time.Minute), timeline[1].endTime)
		assert.InDelta(t, 25.0/60.0, timeline[1].durationHours, 0.01)

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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, nil, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// 10:55 to 11:00 is 5m (< 10m). Price continues to 18:00, so it merges with 11:00-11:20 -> 10:55 to 11:20 (25m <= 40m)
		assert.Equal(t, runTime, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 11, 20, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 25.0/60.0, timeline[0].durationHours, 0.01)
	})

	t.Run("ShortInterval_DoesNotMergeAcrossVPPEventBoundary", func(t *testing.T) {
		t.Parallel()

		// Run at 10:55 (5m before hour top) with uniform price continuing through 18:00,
		// but a VPP event starts at 11:00.
		runTime := time.Date(2026, 6, 15, 10, 55, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 18, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.10,
		}
		status := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			VPPEvents: []types.VPPEvent{
				{
					TSStart:       time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
					TSEnd:         time.Date(2026, 6, 15, 13, 0, 0, 0, chicagoLoc),
					DollarsPerKWH: 2.0,
				},
			},
		}

		vppSettings := types.Settings{MinBatterySOC: 20}
		vppEvents := extractVPPAnchors(runTime, status, vppSettings)
		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, nil, nil, nil, vppSettings, status, vppEvents)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// 10:55 to 11:00 is 5m (< 10m). Although price is unchanged, 11:00 is a VPP event boundary so it must NOT merge across it.
		assert.Equal(t, runTime, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 5.0/60.0, timeline[0].durationHours, 0.01)
	})

	t.Run("ShortInterval_DoesNotMergeAcrossUpcomingPriceChange", func(t *testing.T) {
		t.Parallel()

		// Run at 10:55 (5m before hour top) where current price ends at 11:00 and next price begins at 11:00
		runTime := time.Date(2026, 6, 15, 10, 55, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.10,
		}
		futurePrices := []types.Price{
			{
				TSStart:       time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.40,
			},
		}

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// 10:55 to 11:00 is 5m (< 10m). It must NOT merge across the 11:00 price boundary into the 11:00-15:00 period.
		assert.Equal(t, runTime, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 5.0/60.0, timeline[0].durationHours, 0.01)
	})

	t.Run("ShortInterval_MergesAcrossConsecutiveHourlyPriceStructsWithSameRate", func(t *testing.T) {
		t.Parallel()

		// Run at 10:55 (5m before hour top) where current price ends at 11:00 and next hourly price struct
		// begins at 11:00 with the exact same import rate, export rate, and minSOC.
		runTime := time.Date(2026, 6, 15, 10, 55, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.10,
		}
		futurePrices := []types.Price{
			{
				TSStart:       time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.10,
			},
		}

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// 10:55 to 11:00 is 5m (< 10m). Since rate/minSOC do not change at 11:00, it merges with 11:00-11:20 -> 10:55 to 11:20 (25m)
		assert.Equal(t, runTime, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 11, 20, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 25.0/60.0, timeline[0].durationHours, 0.01)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, settings, status, nil)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, shortPrice, shortFuture, nil, nil, settings, status, nil)
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
		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, types.Price{}, nil, nil, nil, settings, status, nil)
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
					TSStart:   vppStart,
					TSEnd:     vppEnd,
					VPPSoc:    20,
					Mandatory: true,
				},
			},
		}

		vppSettings := types.Settings{MinBatterySOC: 20}
		vppEvents := extractVPPAnchors(status.Timestamp, status, vppSettings)
		timeline, _, _, err := c.buildPlanningTimeline(ctx, status.Timestamp, currentPrice, futurePrices, nil, nil, vppSettings, status, vppEvents)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, zeroPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status, nil)
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

		credit := peakPrice.ExportRateDollars(nem2Settings, nil)
		assert.Equal(t, 0.65, credit, "Export credit must reflect tariff value and not be zeroed out for NEM")

		// BatteryModeExport / blockActionExport is gated when ManageTOUSchedules is false
		block := timelineBlock{startIdx: 0, endIdx: 0, maxExportRate: credit, avgImportRate: 0.65, minSOC: 20}
		timeline := []planInterval{{startTime: now, endTime: now.Add(time.Hour), importRate: 0.65, exportRate: credit, minSOC: 20}}
		allowed := isBlockActionAllowed(
			0,
			block,
			blockActionChoice{actionType: blockActionExport},
			[]timelineBlock{block},
			planState{energyKWH: 10.0, soc: 80.0, capacityKWH: 13.5},
			nil,
			0.0,
			nem2Settings,
			types.SystemStatus{},
			timeline,
			nil,
		)
		assert.False(t, allowed, "blockActionExport is gated when ManageTOUSchedules is false")
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

		credit := riderPrice.ExportRateDollars(nemSettings, []types.Price{riderPrice})
		assert.InDelta(t, -0.0102, credit, 0.0001, "Tariff charging for surplus export must preserve negative export credit to enable curtailment")
	})

	t.Run("OverlappingNowPrice_PrioritizesNowPrice", func(t *testing.T) {
		t.Parallel()

		runNow := time.Date(2026, 6, 15, 14, 15, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:              runNow,
			TSEnd:                runNow.Add(15 * time.Minute), // 14:15 - 14:30
			DollarsPerKWH:        0.35,
			GridUseDollarsPerKWH: 0.05,
		}

		// Future prices has an hourly block covering 14:00 - 15:00 at cheap price
		var futurePrices []types.Price
		futurePrices = append(futurePrices, types.Price{
			TSStart:              time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc),
			TSEnd:                time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
			DollarsPerKWH:        0.05,
			GridUseDollarsPerKWH: 0.05,
		})
		for h := 15; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		status := types.SystemStatus{
			Timestamp:          runNow,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status, nil)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// First interval must use the real-time nowPrice (0.35 + 0.05 = 0.40), not the future price (0.05 + 0.05 = 0.10)
		assert.Equal(t, runNow, timeline[0].startTime)
		assert.Equal(t, runNow.Add(15*time.Minute), timeline[0].endTime)
		assert.InDelta(t, 0.40, timeline[0].importRate, 0.001)
	})

	t.Run("UpcomingPriceStart_ClampsIntervalEnd", func(t *testing.T) {
		t.Parallel()

		runNow := time.Date(2026, 6, 15, 14, 0, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:              runNow,
			TSEnd:                runNow.Add(time.Hour), // 14:00 - 15:00
			DollarsPerKWH:        0.10,
			GridUseDollarsPerKWH: 0.05,
		}

		// Sub-hourly price starting at 14:20
		futurePrices := []types.Price{
			{
				TSStart:              time.Date(2026, 6, 15, 14, 20, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, 15, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.45,
				GridUseDollarsPerKWH: 0.05,
			},
		}
		for h := 15; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.05,
			})
		}

		status := types.SystemStatus{
			Timestamp:          runNow,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status, nil)
		require.NoError(t, err)
		require.NotEmpty(t, timeline)

		// First interval must be clamped to 14:20 because the sub-hourly price starts then
		assert.Equal(t, runNow, timeline[0].startTime)
		assert.Equal(t, time.Date(2026, 6, 15, 14, 20, 0, 0, chicagoLoc), timeline[0].endTime)
		assert.InDelta(t, 0.15, timeline[0].importRate, 0.001)

		// Second interval starts at 14:20 with the higher rate (0.45 + 0.05 = 0.50)
		assert.Equal(t, time.Date(2026, 6, 15, 14, 20, 0, 0, chicagoLoc), timeline[1].startTime)
		assert.InDelta(t, 0.50, timeline[1].importRate, 0.001)
	})

	t.Run("SolarCapacityBufferMinutes_ShiftsSolarProfile", func(t *testing.T) {
		t.Parallel()

		// Run at 00:00 so all subsequent intervals in the day are projected without immediate telemetry override
		runNow := time.Date(2026, 6, 15, 0, 0, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       runNow,
			TSEnd:         runNow.Add(time.Hour),
			DollarsPerKWH: 0.10,
		}
		var futurePrices []types.Price
		for h := 1; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.10,
			})
		}

		// Generate 14 days of solar history:
		// Hour 9: 2.0 kWh, Hour 10: 4.0 kWh, Hour 14: 6.0 kWh, Hour 15: 4.0 kWh
		var history []types.EnergyStats
		for d := 1; d <= 14; d++ {
			day := runNow.Add(time.Duration(-d*24) * time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				switch h {
				case 9:
					solar = 2.0
				case 10:
					solar = 4.0
				case 14:
					solar = 6.0
				case 15:
					solar = 4.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, chicagoLoc),
					SolarKWH:    solar,
					HomeKWH:     1.0,
				})
			}
		}

		status := types.SystemStatus{
			Timestamp:          runNow,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}

		// Aggressive (0 min buffer)
		timelineAgg, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, history, nil, types.Settings{
			OptimizationProfile: "aggressive",
		}, status, nil)
		require.NoError(t, err)

		// Balanced (15 min buffer)
		timelineBal, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, history, nil, types.Settings{
			OptimizationProfile: "balanced",
		}, status, nil)
		require.NoError(t, err)

		// Conservative (30 min buffer)
		timelineCons, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, history, nil, types.Settings{
			OptimizationProfile: "conservative",
		}, status, nil)
		require.NoError(t, err)

		// Find interval starting at 10:00 (duration 20m = 1/3 hr)
		// Aggressive (0m shift): 4.0 * (20/60) = 1.333 kWh
		// Balanced (0m shift): 4.0 * (20/60) = 1.333 kWh
		// Conservative (20m shift): ((2/3)*4.0 + (1/3)*2.0) = 3.333 kWh/hr -> 3.333 * (20/60) = 1.111 kWh
		var intAgg10, intBal10, intCons10 *planInterval
		for i := range timelineAgg {
			if timelineAgg[i].startTime.Hour() == 10 && timelineAgg[i].startTime.Minute() == 0 {
				intAgg10 = &timelineAgg[i]
				break
			}
		}
		for i := range timelineBal {
			if timelineBal[i].startTime.Hour() == 10 && timelineBal[i].startTime.Minute() == 0 {
				intBal10 = &timelineBal[i]
				break
			}
		}
		for i := range timelineCons {
			if timelineCons[i].startTime.Hour() == 10 && timelineCons[i].startTime.Minute() == 0 {
				intCons10 = &timelineCons[i]
				break
			}
		}

		require.NotNil(t, intAgg10)
		require.NotNil(t, intBal10)
		require.NotNil(t, intCons10)

		assert.InDelta(t, 4.0*(20.0/60.0), intAgg10.solarKWH, 0.05, "aggressive should have unshifted solar at hour 10")
		assert.InDelta(t, 4.0*(20.0/60.0), intBal10.solarKWH, 0.05, "balanced should have unshifted solar at hour 10")
		assert.InDelta(t, (10.0/3.0)*(20.0/60.0), intCons10.solarKWH, 0.05, "conservative should have 20m shifted solar at hour 10")

		// In afternoon at Hour 15 (Hour 14 is 6.0, Hour 15 is 4.0, peak solar is hour 14):
		// Buffer is only applied in morning ramp up to peak solar (h <= peakSolarHour).
		// Afternoon hours remain unshifted at the true forecast (4.0 * 20/60).
		var intCons15 *planInterval
		for i := range timelineCons {
			if timelineCons[i].startTime.Hour() == 15 && timelineCons[i].startTime.Minute() == 0 {
				intCons15 = &timelineCons[i]
				break
			}
		}
		require.NotNil(t, intCons15)
		assert.InDelta(t, 4.0*(20.0/60.0), intCons15.solarKWH, 0.05, "conservative should keep unshifted solar in afternoon after peak solar")
	})

	t.Run("DepressedLiveSolar_DepressesNextTwoPeriods", func(t *testing.T) {
		t.Parallel()

		runNow := time.Date(2026, 6, 15, 12, 0, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:       runNow,
			TSEnd:         runNow.Add(time.Hour),
			DollarsPerKWH: 0.10,
		}
		var futurePrices []types.Price
		for h := 13; h < 24; h++ {
			futurePrices = append(futurePrices, types.Price{
				TSStart:       time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.10,
			})
		}

		// 14 days of history with 0.6 kWh at hour 7, 3.0 kWh at hour 8, and 6.0 kWh at hours 12 and 13
		var history []types.EnergyStats
		for d := 1; d <= 14; d++ {
			day := runNow.Add(time.Duration(-d*24) * time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				switch h {
				case 7:
					solar = 0.6
				case 8:
					solar = 3.0
				case 12, 13:
					solar = 6.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, chicagoLoc),
					SolarKWH:    solar,
					HomeKWH:     1.0,
				})
			}
		}

		// 1. Live solar at 12:00 is 1.2 kW (< 25% of 6.0 kW predicted at idx == 0)
		status := types.SystemStatus{
			Timestamp:          runNow,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			SolarKW:            1.2,
			TimeLocation:       "America/Chicago",
		}

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, history, nil, types.Settings{
			OptimizationProfile: "aggressive",
		}, status, nil)
		require.NoError(t, err)
		if assert.GreaterOrEqual(t, len(timeline), 4) {
			// idx 0 (12:00-12:20), idx 1 (12:20-12:40), idx 2 (12:40-13:00) should all be depressed to 1.2 kW * (1/3 h) = 0.4 kWh
			assert.InDelta(t, 1.2*(20.0/60.0), timeline[0].solarKWH, 0.05)
			assert.InDelta(t, 1.2*(20.0/60.0), timeline[1].solarKWH, 0.05)
			assert.InDelta(t, 1.2*(20.0/60.0), timeline[2].solarKWH, 0.05)
			// idx 3 (13:00-13:20) should return to normal predicted solar: 6.0 kW * (1/3 h) = 2.0 kWh
			assert.InDelta(t, 6.0*(20.0/60.0), timeline[3].solarKWH, 0.05)
		}

		// 2. Morning ramp at 07:45 AM: live solar is 0.60 kW (100% of hour 7's 0.60 kW forecast, NOT depressed),
		// while hour 8's forecast is 3.00 kW (> 4x 0.60 kW). Intervals idx 1 (08:00-08:20) and idx 2 (08:20-08:40)
		// must NOT be depressed to 0.60 kW.
		morningNow := time.Date(2026, 6, 15, 7, 45, 0, 0, chicagoLoc)
		morningPrice := types.Price{
			TSStart:       time.Date(2026, 6, 15, 7, 0, 0, 0, chicagoLoc),
			TSEnd:         time.Date(2026, 6, 15, 8, 0, 0, 0, chicagoLoc),
			DollarsPerKWH: 0.10,
		}
		var morningFuturePrices []types.Price
		for h := 8; h < 20; h++ {
			morningFuturePrices = append(morningFuturePrices, types.Price{
				TSStart:       time.Date(2026, 6, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:         time.Date(2026, 6, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH: 0.10,
			})
		}
		morningStatus := types.SystemStatus{
			Timestamp:          morningNow,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			SolarKW:            0.60,
			TimeLocation:       "America/Chicago",
		}
		morningTimeline, _, _, err := c.buildPlanningTimeline(ctx, morningNow, morningPrice, morningFuturePrices, history, nil, types.Settings{
			OptimizationProfile: "aggressive",
		}, morningStatus, nil)
		require.NoError(t, err)
		if assert.GreaterOrEqual(t, len(morningTimeline), 3) {
			// idx 0 (07:45-08:00, 15m): 0.60 kW * 0.25h = 0.15 kWh
			assert.InDelta(t, 0.60*0.25, morningTimeline[0].solarKWH, 0.02)
			// idx 1 (08:00-08:20, 20m) and idx 2 (08:20-08:40, 20m): must remain at hour 8's 3.0 kW * (1/3h) = 1.0 kWh
			assert.InDelta(t, 3.0*(20.0/60.0), morningTimeline[1].solarKWH, 0.05)
			assert.InDelta(t, 3.0*(20.0/60.0), morningTimeline[2].solarKWH, 0.05)
		}
	})

	t.Run("FlexiblePostPeakHorizon_ExtendsPastDay2PeakIntoOffPeakValley", func(t *testing.T) {
		t.Parallel()

		// TOU tariff with Peak 14:00-19:00 ($0.45/kWh) and Off-Peak ($0.10/kWh) otherwise.
		// Case 1: now = 16:00 Day 1 (mid-peak) with 36h of prices -> defaultEnd (16:00 Day 2) lands mid-peak,
		// so buildPlanningTimeline extends the horizon to 20:00 Day 2 (1 hour after Day 2's 14:00-19:00 Peak).
		priceForHour := func(ts time.Time) types.Price {
			h := ts.Hour()
			rate := 0.10
			if h >= 14 && h < 19 {
				rate = 0.45
			}
			return types.Price{
				TSStart:       ts,
				TSEnd:         ts.Add(time.Hour),
				DollarsPerKWH: rate,
			}
		}

		t1600 := time.Date(2026, 6, 15, 16, 0, 0, 0, chicagoLoc)
		var future1600 []types.Price
		for i := 1; i < 36; i++ {
			future1600 = append(future1600, priceForHour(t1600.Add(time.Duration(i)*time.Hour)))
		}
		status1600 := types.SystemStatus{
			Timestamp:          t1600,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}
		tl1600, _, model1600, err := c.buildPlanningTimeline(ctx, t1600, priceForHour(t1600), future1600, nil, nil, types.Settings{MinBatterySOC: 20}, status1600, nil)
		require.NoError(t, err)
		require.NotEmpty(t, tl1600)
		expectedEnd1600 := time.Date(2026, 6, 16, 20, 0, 0, 0, chicagoLoc)
		assert.Equal(t, expectedEnd1600, tl1600[len(tl1600)-1].endTime,
			"When 24h lands mid-peak (16:00 Day 2), horizon must extend to 1 hour after Peak (20:00 Day 2 = 28h)")
		ph1600 := c.buildPostHorizonValuation(tl1600, future1600, types.Settings{MinBatterySOC: 20}, status1600, model1600)
		assert.InDelta(t, 0.10, ph1600.knownPostHorizonRate, 0.001,
			"Post-horizon valuation after extending past Day 2 Peak must see purely Off-Peak ($0.10/kWh) rates")

		// Case 2: now = 11:00 Day 1 (within 4h before Peak) with 36h of prices -> defaultEnd (11:00 Day 2) is 3h before
		// Day 2's 14:00-19:00 Peak, so horizon extends to 20:00 Day 2 (29h) rather than ending right before Peak.
		t1100 := time.Date(2026, 6, 15, 11, 0, 0, 0, chicagoLoc)
		var future1100 []types.Price
		for i := 1; i < 36; i++ {
			future1100 = append(future1100, priceForHour(t1100.Add(time.Duration(i)*time.Hour)))
		}
		status1100 := types.SystemStatus{
			Timestamp:          t1100,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}
		tl1100, _, _, err := c.buildPlanningTimeline(ctx, t1100, priceForHour(t1100), future1100, nil, nil, types.Settings{MinBatterySOC: 20}, status1100, nil)
		require.NoError(t, err)
		require.NotEmpty(t, tl1100)
		assert.Equal(t, expectedEnd1600, tl1100[len(tl1100)-1].endTime,
			"When 24h lands within 4h before Day 2 Peak (11:00 Day 2), horizon must extend to 20:00 Day 2 (29h)")

		// Case 3: now = 21:00 Day 1 (already in post-peak Off-Peak valley) -> defaultEnd (21:00 Day 2) is 2h after Peak
		// and 17h before Day 3 Peak, so horizon remains at 24h (21:00 Day 2).
		t2100 := time.Date(2026, 6, 15, 21, 0, 0, 0, chicagoLoc)
		var future2100 []types.Price
		for i := 1; i < 36; i++ {
			future2100 = append(future2100, priceForHour(t2100.Add(time.Duration(i)*time.Hour)))
		}
		status2100 := types.SystemStatus{
			Timestamp:          t2100,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			TimeLocation:       "America/Chicago",
		}
		tl2100, _, _, err := c.buildPlanningTimeline(ctx, t2100, priceForHour(t2100), future2100, nil, nil, types.Settings{MinBatterySOC: 20}, status2100, nil)
		require.NoError(t, err)
		require.NotEmpty(t, tl2100)
		assert.Equal(t, t2100.Add(24*time.Hour), tl2100[len(tl2100)-1].endTime,
			"When 24h already sits in the post-peak Off-Peak valley (21:00 Day 2), horizon must stay at 24h")

		// Case 4: Full c.Plan(...) at t1600 (16:00 Day 1) with 36h of prices:
		// Solver optimizes across the extended 28h timeline through 20:00 Day 2 (so it preserves battery for
		// the 16:00-19:00 Day 2 Peak rather than dumping at 15:40-16:00 Day 2), and truncates the returned
		// plan.Periods back to 24 hours (ending at 16:00 Day 2).
		planSettings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
		}
		_, plan1600, err := c.Plan(ctx, status1600, priceForHour(t1600), future1600, nil, nil, planSettings, nil)
		require.NoError(t, err)
		require.NotEmpty(t, plan1600.Periods)
		assert.Equal(t, 24, plan1600.HorizonHours, "Returned Plan.HorizonHours must be truncated to 24 hours")
		assert.Equal(t, t1600.Add(24*time.Hour), plan1600.Periods[len(plan1600.Periods)-1].TSEnd,
			"Returned Plan.Periods must be truncated at now + 24h (16:00 Day 2)")
	})
}

// TestExtractVPPAnchors tests the extraction of VPP events and prep deadlines across the horizon.
func TestExtractVPPAnchors(t *testing.T) {
	t.Parallel()

	chicagoLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	now := time.Date(2026, 6, 15, 8, 0, 0, 0, chicagoLoc)

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
					TSStart:   vppStart2,
					TSEnd:     vppEnd2,
					VPPSoc:    25,
					Mandatory: true,
				},
				{
					TSStart:   vppStart1,
					TSEnd:     vppEnd1,
					VPPSoc:    20,
					Mandatory: false,
				},
			},
		}

		// Default balanced profile has 20-minute VPPChargingBufferMinutes; events must be sorted chronologically
		vppEvents := extractVPPAnchors(now, status, types.Settings{})
		require.Len(t, vppEvents, 2)

		// First VPP is optional, so deadline is event start minus 20m buffer
		assert.Equal(t, vppStart1.Add(-20*time.Minute), vppEvents[0].deadline)
		assert.Equal(t, 20.0, vppEvents[0].vppSoc)
		assert.False(t, vppEvents[0].mandatory)
		assert.Equal(t, []time.Time{vppStart1.Add(-20 * time.Minute), vppStart1, vppEnd1}, vppEvents[0].boundaries)

		// Second VPP is mandatory, so deadline is 2 hours before event start (vppStandbyLeadTime)
		assert.Equal(t, vppStart2.Add(-2*time.Hour), vppEvents[1].deadline)
		assert.Equal(t, 25.0, vppEvents[1].vppSoc)
		assert.True(t, vppEvents[1].mandatory)
		assert.Equal(t, []time.Time{vppStart2.Add(-2 * time.Hour), vppStart2, vppEnd2}, vppEvents[1].boundaries)
	})

	t.Run("FiltersOutEventsOutsideHorizonAndLeadWindow", func(t *testing.T) {
		t.Parallel()

		horizonEnd := now.Add(maxPlanningHorizonHours * time.Hour)
		status := types.SystemStatus{
			Timestamp: now,
			VPPEvents: []types.VPPEvent{
				{TSStart: now.Add(-2 * time.Hour), TSEnd: now},                                                       // Already ended
				{TSStart: horizonEnd.Add(vppStandbyLeadTime), TSEnd: horizonEnd.Add(vppStandbyLeadTime + time.Hour)}, // Starts at/after horizonEnd + 2h lead window
				{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(4 * time.Hour), OptOut: true},                       // Opted out
				{TSStart: now.Add(2 * time.Hour), TSEnd: now.Add(4 * time.Hour)},                                     // Inside horizon
			},
		}

		vppEvents := extractVPPAnchors(now, status, types.Settings{})
		if assert.Len(t, vppEvents, 1) {
			assert.Equal(t, now.Add(2*time.Hour), vppEvents[0].eventStart)
		}
	})
}

// TestBuildPostHorizonValuation tests post-horizon rate blending, net load slice ordering, and solar surplus calculation.
func TestBuildPostHorizonValuation(t *testing.T) {
	t.Parallel()

	chicagoLoc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	now := time.Date(2026, 6, 15, 8, 0, 0, 0, chicagoLoc)
	c := NewController()

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
			{
				TSStart:              tEnd.Add(time.Hour),
				TSEnd:                tEnd.Add(2 * time.Hour),
				DollarsPerKWH:        0.28,
				GridUseDollarsPerKWH: 0.07,
			},
		}

		ph := c.buildPostHorizonValuation(timeline, futurePrices, types.Settings{}, types.SystemStatus{}, nil)
		// First hour import rate: 0.22 + 0.03 = 0.25
		// Second hour import rate: 0.28 + 0.07 = 0.35
		// Average over post-horizon rollup window: (0.25 + 0.35) / 2 = 0.30
		assert.InDelta(t, 0.30, ph.knownPostHorizonRate, 0.001, "must average full ImportRateDollars() across post-horizon rollup window")
	})

	t.Run("KnownPostHorizonRate_FallsBackToLatestTimelineRate", func(t *testing.T) {
		t.Parallel()

		tEnd := now.Add(6 * time.Hour)
		timeline := []planInterval{
			{startTime: now, endTime: now.Add(3 * time.Hour), importRate: 0.10},
			{startTime: now.Add(3 * time.Hour), endTime: tEnd, importRate: 0.14},
		}

		// No future prices after horizon
		ph := c.buildPostHorizonValuation(timeline, nil, types.Settings{}, types.SystemStatus{}, nil)
		assert.InDelta(t, 0.14, ph.knownPostHorizonRate, 0.001, "must fall back to latest timeline rate")
	})

	t.Run("PostHorizonLoadForecast", func(t *testing.T) {
		t.Parallel()

		tEnd := time.Date(2026, 7, 10, 6, 0, 0, 0, chicagoLoc)
		timeline := []planInterval{
			{startTime: tEnd.Add(-6 * time.Hour), endTime: tEnd, importRate: 0.10},
		}

		mockModel := make([]TimeProfile, 24)
		for h := 0; h < 24; h++ {
			mockModel[h] = TimeProfile{Hour: h, AvgHomeLoadKWH: 1.5}
		}

		ph := c.buildPostHorizonValuation(timeline, nil, types.Settings{}, types.SystemStatus{}, mockModel)
		// 4 hours * 1.5 kWh/hour = 6.0 kWh
		if assert.Len(t, ph.postHorizonSlices, 4) {
			var totalLoad float64
			for _, s := range ph.postHorizonSlices {
				totalLoad += s.loadKWH
			}
			assert.InDelta(t, 6.0, totalLoad, 0.001, "must forecast 4 hours of home consumption from model")
		}

		// Verify net load (AvgHomeLoadKWH - AvgSolarKWH) paired with per-hour futurePrices and sorted by rate descending
		solarModel := make([]TimeProfile, 24)
		for h := 0; h < 24; h++ {
			solar := 0.0
			if h >= 8 && h < 14 {
				solar = 3.0 // Net solar surplus during 08:00-14:00
			}
			solarModel[h] = TimeProfile{Hour: h, AvgHomeLoadKWH: 1.0, AvgSolarKWH: solar}
		}
		postPrices := []types.Price{
			{TSStart: tEnd, TSEnd: tEnd.Add(time.Hour), DollarsPerKWH: 0.10},                        // 06:00-07:00: $0.10, netLoad 1.0
			{TSStart: tEnd.Add(time.Hour), TSEnd: tEnd.Add(2 * time.Hour), DollarsPerKWH: 0.30},     // 07:00-08:00: $0.30, netLoad 1.0
			{TSStart: tEnd.Add(2 * time.Hour), TSEnd: tEnd.Add(8 * time.Hour), DollarsPerKWH: 0.30}, // 08:00-14:00: $0.30, netLoad <= 0 (sunny)
		}
		solarPH := c.buildPostHorizonValuation(timeline, postPrices, types.Settings{}, types.SystemStatus{}, solarModel)
		if assert.Len(t, solarPH.postHorizonSlices, 2, "sunny post-horizon hours with net solar surplus must not create net load slices") {
			assert.InDelta(t, 0.30, solarPH.postHorizonSlices[0].importRate, 1e-6, "highest-rate post-horizon net load hour must be ordered first")
			assert.InDelta(t, 1.0, solarPH.postHorizonSlices[0].loadKWH, 1e-6)
			assert.InDelta(t, 0.10, solarPH.postHorizonSlices[1].importRate, 1e-6)
			assert.InDelta(t, 1.0, solarPH.postHorizonSlices[1].loadKWH, 1e-6)
		}

		// Verify brand-new site with no energy history (BuildHourlyEnergyModel returns 24 profiles at the 0.09 kWh standby floor)
		// falls back to defaultPostHorizonLoadKWH (6.0 kWh / 4h = 1.5 kWh/hr) paired with post-horizon rates
		emptyModel, _ := c.BuildHourlyEnergyModel(context.Background(), tEnd.Add(-6*time.Hour), nil, nil, types.Settings{})
		coldStartPH := c.buildPostHorizonValuation(timeline, postPrices, types.Settings{}, types.SystemStatus{}, emptyModel)
		if assert.Len(t, coldStartPH.postHorizonSlices, 4, "brand-new site with empty energy history must still populate 4 default load slices") {
			assert.InDelta(t, 0.30, coldStartPH.postHorizonSlices[0].importRate, 1e-6)
			assert.InDelta(t, 1.5, coldStartPH.postHorizonSlices[0].loadKWH, 1e-6)
			assert.InDelta(t, 0.10, coldStartPH.postHorizonSlices[3].importRate, 1e-6)
			assert.InDelta(t, 1.5, coldStartPH.postHorizonSlices[3].loadKWH, 1e-6)
		}
	})

	t.Run("NegativeRates_FloorsPostHorizonRateAtZero", func(t *testing.T) {
		t.Parallel()

		t0 := time.Date(2026, 7, 10, 2, 0, 0, 0, chicagoLoc)
		timeline := []planInterval{
			{startTime: t0, endTime: t0.Add(2 * time.Hour), durationHours: 2.0, importRate: -0.05, loadKWH: 2.0},
			{startTime: t0.Add(2 * time.Hour), endTime: t0.Add(6 * time.Hour), durationHours: 4.0, importRate: 0.06, loadKWH: 4.0},
			{startTime: t0.Add(6 * time.Hour), endTime: t0.Add(8 * time.Hour), durationHours: 2.0, importRate: -0.03, loadKWH: 2.0},
		}

		ph := c.buildPostHorizonValuation(timeline, nil, types.Settings{}, types.SystemStatus{}, nil)
		assert.Equal(t, 0.0, ph.knownPostHorizonRate, "Negative post-horizon rate must be floored at 0.0")
	})
}

func TestResolveRoundTripEfficiency(t *testing.T) {
	t.Parallel()

	t.Run("SanitizeAndDefaultForACCoupled", func(t *testing.T) {
		t.Parallel()
		status := types.SystemStatus{DCCoupled: false}
		assert.Equal(t, defaultRoundTripEfficiency, resolveRoundTripEfficiency(0, status))
		assert.Equal(t, defaultRoundTripEfficiency, resolveRoundTripEfficiency(-0.5, status))
		assert.Equal(t, defaultRoundTripEfficiency, resolveRoundTripEfficiency(1.05, status))
		assert.Equal(t, defaultRoundTripEfficiency, resolveRoundTripEfficiency(math.NaN(), status))
		assert.Equal(t, 0.88, resolveRoundTripEfficiency(0.88, status))
		assert.Equal(t, 0.90, resolveRoundTripEfficiency(0.90, status))
		assert.Equal(t, 0.97, resolveRoundTripEfficiency(0.97, status))
		assert.Equal(t, 1.0, resolveRoundTripEfficiency(1.0, status))
	})

	t.Run("DCCoupledUsesMaxOfEffAndDCCoupledDefault", func(t *testing.T) {
		t.Parallel()
		status := types.SystemStatus{DCCoupled: true}
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(0, status))
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(-0.5, status))
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(1.05, status))
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(math.NaN(), status))
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(0.88, status))
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(0.90, status))
		assert.Equal(t, defaultDCCoupledRoundTripEfficiency, resolveRoundTripEfficiency(0.96, status))
		assert.Equal(t, 0.98, resolveRoundTripEfficiency(0.98, status))
		assert.Equal(t, 1.0, resolveRoundTripEfficiency(1.0, status))
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
		assert.InDelta(t, initialState.soc, nextState.soc, 0.001)
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
		assert.InDelta(t, 3.0, metricsA.solarExportKWH, 0.01, "100% of solar should direct export")
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
		assert.InDelta(t, 2.0, metricsB.solarExportKWH, 0.01, "Remaining solar exports to grid")
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
		assert.InDelta(t, 0.0, metricsC.solarExportKWH, 0.01)
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
		// All 3 kWh surplus solar charges battery (3.0 * sqrt(0.90) = 2.846 kWh stored).
		// Since battery still has room (3.5 kWh room), 0 kWh solar is exported!
		assert.InDelta(t, 0.0, metrics.gridExportKWH, 0.01, "No solar export when battery is charging behind bidirectional meter")
		assert.InDelta(t, 13.5, nextState.energyKWH, 0.01)
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
		assert.InDelta(t, 0.0, metrics.gridChargeKWH, 0.001, "No grid charging when already above target SOC")
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
			nextState, metrics := stepPhysics(initialState, action, testInterval, exportSettings, 0.90)

			// AC power balance at site POI meter:
			// NetGridKW = GridImportKW - GridExportKW
			// Kirchhoff: NetGridKW == HomeKW - (SolarKW - SolarCurtailedKW) + BatChargeKW - BatDischargeKW
			oneWayEff := math.Sqrt(0.90)
			batDischargeKWH := metrics.batSuppliedHomeKWH + metrics.batExportKWH
			batChargeKWH := max(0.0, (nextState.energyKWH-initialState.energyKWH+(batDischargeKWH/oneWayEff))/oneWayEff)
			gridImportKW := metrics.gridImportKWH / testInterval.durationHours
			gridExportKW := metrics.gridExportKWH / testInterval.durationHours
			netGridKW := gridImportKW - gridExportKW

			effectiveSolarKW := (testInterval.solarKWH / testInterval.durationHours) - metrics.solarCurtailedKW
			expectedNetGridKW := (testInterval.loadKWH / testInterval.durationHours) - effectiveSolarKW + (batChargeKWH-batDischargeKWH)/testInterval.durationHours

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

	t.Run("Export_HonorsTargetSOCFloor", func(t *testing.T) {
		t.Parallel()

		lowReserveInterval := interval
		lowReserveInterval.minSOC = 5.0
		lowReserveInterval.loadKWH = 0.0
		lowReserveInterval.exportRate = 0.25

		exportSettings := settings
		exportSettings.MinBatterySOC = 5.0
		exportSettings.GridExportBatteries = true

		// Start at 50% SOC (6.75 kWh on 13.5 kWh battery) with 5 kW max discharge for 1 hour:
		// Unclamped discharge would drain 5.0 / sqrt(0.90) = 5.27 kWh (down to ~11% SOC),
		// but targetSOC = 40 must clamp ending SOC at 40.0% (5.40 kWh).
		stateAt50 := planState{
			energyKWH:      13.5 * 0.50,
			soc:            50.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
		}
		action := actionCandidate{
			batteryMode: types.BatteryModeExport,
			solarMode:   types.SolarModeAny,
			targetSOC:   40,
		}

		nextState, metrics := stepPhysics(stateAt50, action, lowReserveInterval, exportSettings, 0.90)
		assert.InDelta(t, 40.0, nextState.soc, 0.01, "BatteryModeExport must stop discharging at action.targetSOC (40%) even when interval.minSOC is 5%")
		assert.InDelta(t, 13.5*0.10*math.Sqrt(0.90), metrics.batExportKWH, 0.01)
	})

	t.Run("Export_TargetBelowMinSOC_UsesMinSOC", func(t *testing.T) {
		t.Parallel()

		exportInterval := interval
		exportInterval.minSOC = 20.0
		exportInterval.loadKWH = 0.0
		exportInterval.exportRate = 0.25

		exportSettings := settings
		exportSettings.MinBatterySOC = 20.0
		exportSettings.GridExportBatteries = true

		stateAt30 := planState{
			energyKWH:      13.5 * 0.30,
			soc:            30.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
		}
		// Non-VPP export with targetSOC = 10 (< interval.minSOC = 20) must still clamp at minSOC (20%)
		action := actionCandidate{
			batteryMode: types.BatteryModeExport,
			solarMode:   types.SolarModeAny,
			targetSOC:   10,
		}

		nextState, _ := stepPhysics(stateAt30, action, exportInterval, exportSettings, 0.90)
		assert.InDelta(t, 20.0, nextState.soc, 0.01, "Non-VPP BatteryModeExport must never discharge below interval.minSOC (20%) even if targetSOC is 10")
	})
}

// TestCalculateTerminalValuation tests ending battery valuation (replacement cost vs banked credit).
func TestCalculateTerminalValuation(t *testing.T) {
	t.Parallel()

	settings := types.Settings{
		MinBatterySOC: 20,
	}

	postHorizon := postHorizonValuation{
		knownPostHorizonRate: 0.08,
		postHorizonSlices: []postHorizonLoadSlice{
			{loadKWH: defaultPostHorizonLoadKWH, importRate: 0.08},
		},
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

		valuation := calculateTerminalValuation(finalState, postHorizon, settings, nil, capacityKWH, roundTripEff)
		assert.True(t, valuation > 0, "ending below reserve must produce a positive penalty cost")
	})

	t.Run("EndingAboveReserve_CreditedForBankedEnergy", func(t *testing.T) {
		t.Parallel()

		// Ending at 80% SOC (surplus energy banked in battery)
		finalState := planState{
			energyKWH: capacityKWH * 0.80,
			soc:       80,
		}

		valuation := calculateTerminalValuation(finalState, postHorizon, settings, nil, capacityKWH, roundTripEff)
		assert.True(t, valuation < 0, "ending above reserve must produce a negative cost (credit)")
	})

	t.Run("TerminalValuation_StrictConcavityAndPenalty", func(t *testing.T) {
		t.Parallel()

		// 1. Deficit penalty rate >= $1.00/kWh
		deficitState := planState{
			energyKWH: capacityKWH * 0.10, // 10% below 20% reserve
			soc:       10,
		}
		penaltyVal := calculateTerminalValuation(deficitState, postHorizon, settings, nil, capacityKWH, roundTripEff)
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

		valReserve := calculateTerminalValuation(stateAtReserve, postHorizon, settings, nil, capacityKWH, roundTripEff)
		valPeakCap := calculateTerminalValuation(stateAtPeakCap, postHorizon, settings, nil, capacityKWH, roundTripEff)
		valExcess := calculateTerminalValuation(stateAtExcess, postHorizon, settings, nil, capacityKWH, roundTripEff)

		marginalCreditFirst6KWH := (-valPeakCap - -valReserve) / 6.0
		marginalCreditExcess2KWH := (-valExcess - -valPeakCap) / 2.0

		assert.Greater(t, marginalCreditFirst6KWH, marginalCreditExcess2KWH, "Terminal credit must exhibit strict concavity (diminishing returns)")
	})

	t.Run("FreeNights_ZeroPostHorizonRate_DoesNotCreditEndingAboveReserve", func(t *testing.T) {
		t.Parallel()

		zeroPostHorizon := postHorizonValuation{
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

		valuation := calculateTerminalValuation(surplusState, zeroPostHorizon, settings, peakTimeline, capacityKWH, roundTripEff)
		assert.InDelta(t, 0.0, valuation, 0.0001, "Free post-horizon electricity ($0) must not be overridden with timeline rate to credit surplus energy")
	})

	t.Run("EndingWithinReserveTolerance_NoPenaltyOrCredit", func(t *testing.T) {
		t.Parallel()

		// Ending at 19.95% SOC when target reserve is 20.0% (within 0.1% tolerance)
		finalState := planState{
			energyKWH: capacityKWH * 0.1995,
			soc:       19.95,
		}

		valuation := calculateTerminalValuation(finalState, postHorizon, settings, nil, capacityKWH, roundTripEff)
		assert.Equal(t, 0.0, valuation, "ending within 0.1% reserve tolerance must produce zero penalty or credit")
	})

	t.Run("ScheduledTOUReserveDeficit_ValuedAtReplacementRate", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
		// Lowest reserve on the timeline is 20% (Off-Peak), and the last interval is Super Off-Peak with 90% minSOC.
		// Even if settings.MinBatterySOC is left at a stale 60% from Simple mode before the user switched to
		// variable reserve, calculateTerminalValuation must use the lowest minSOC on the timeline (20%) as
		// baseReserveSOC and the final interval's minSOC (90%) as targetReserveSOC.
		sopTimeline := []planInterval{
			{startTime: now.Add(-time.Hour), endTime: now, minSOC: 20.0, importRate: 0.12},
			{startTime: now, endTime: now.Add(time.Hour), minSOC: 90.0, importRate: 0.055},
		}
		// Ending at 40% SOC (above timeline lowest reserve 20%, but below scheduled TOU reserve 90%)
		finalState := planState{
			energyKWH: capacityKWH * 0.40,
			soc:       40.0,
		}
		chargeSettings := settings
		chargeSettings.MinBatterySOC = 60.0 // Stale Simple-mode value that must be ignored
		chargeSettings.GridChargeBatteries = true

		valuation := calculateTerminalValuation(finalState, postHorizon, chargeSettings, sopTimeline, capacityKWH, roundTripEff)
		// Deficit is (90% - 40%) * capacityKWH in stored DC energy, requiring deficitKWH / oneWayEff of AC grid import
		deficitKWH := (0.90 - 0.40) * capacityKWH
		expectedCost := (deficitKWH / math.Sqrt(roundTripEff)) * postHorizon.knownPostHorizonRate
		assert.InDelta(t, expectedCost, valuation, 1e-4, "ending below scheduled TOU reserve with grid charging enabled must be valued at replacement rate using timeline minSOC")
	})

	t.Run("NegativePostHorizonRate_DoesNotPenalizeStoredEnergy", func(t *testing.T) {
		t.Parallel()

		negPostHorizon := postHorizonValuation{
			knownPostHorizonRate: -0.05,
		}
		surplusState := planState{
			energyKWH: capacityKWH * 0.80,
			soc:       80.0,
		}

		valuation := calculateTerminalValuation(surplusState, negPostHorizon, settings, nil, capacityKWH, roundTripEff)
		assert.InDelta(t, 0.0, valuation, 1e-6, "Negative post-horizon rate must be floored at 0.0 so banked energy is never penalized")
	})

	t.Run("PerHourPostHorizonSlices_ValuesEachSliceAtConsumptionRate", func(t *testing.T) {
		t.Parallel()

		oneWayEff := math.Sqrt(roundTripEff)
		targetEnergy := capacityKWH * 0.20 // 2.7 kWh (usable capacity = 10.8 kWh)
		slicePostHorizon := postHorizonValuation{
			knownPostHorizonRate:       0.25, // Higher 4h mean than lowest slice (0.10)
			postHorizonSolarSurplusKWH: 6.8,  // Leaves 10.8 - 6.8 = 4.0 kWh of Tier 2 headroom after morning load drains
			postHorizonSlices: []postHorizonLoadSlice{
				{loadKWH: 1.0, importRate: 0.30},
				{loadKWH: 2.0, importRate: 0.10},
			},
		}

		// 1. 2.5 kWh DC above reserve:
		// - Slice 0 (1.0 kWh AC load) consumes 1.0 / oneWayEff kWh DC, displacing 1.0 kWh AC at $0.30
		// - Remaining (2.5 - 1.0/oneWayEff) kWh DC is credited at $0.10 * oneWayEff (with 0.95 Tier 1 discount)
		state25 := planState{
			energyKWH: targetEnergy + 2.5,
			soc:       ((targetEnergy + 2.5) / capacityKWH) * 100.0,
		}
		val25 := calculateTerminalValuation(state25, slicePostHorizon, settings, nil, capacityKWH, roundTripEff)
		remAfterSlice0 := 2.5 - (1.0 / oneWayEff)
		expected25 := -((1.0 * 0.30 * terminalTier1DiscountFactor) + (remAfterSlice0 * 0.10 * oneWayEff * terminalTier1DiscountFactor))
		assert.InDelta(t, expected25, val25, 1e-6, "Each post-horizon slice must be valued at the rate of the hour when it is consumed")

		// 2. 6.5 kWh DC above reserve:
		// - 3.0 / oneWayEff kWh DC covered by Tier 1 slices (displacing 1.0 kWh AC @ $0.30 + 2.0 kWh AC @ $0.10)
		// - Remaining (6.5 - 3.0/oneWayEff) kWh DC <= unfilledBySolarKWH (10.8 - 6.8 = 4.0 kWh), credited at Tier 2 replacementRate ($0.25 * oneWayEff * 0.95 * 0.70)
		state65 := planState{
			energyKWH: targetEnergy + 6.5,
			soc:       ((targetEnergy + 6.5) / capacityKWH) * 100.0,
		}
		val65 := calculateTerminalValuation(state65, slicePostHorizon, settings, nil, capacityKWH, roundTripEff)
		tier1Full := (1.0 * 0.30 * terminalTier1DiscountFactor) + (2.0 * 0.10 * terminalTier1DiscountFactor)
		tier2Rate := 0.25 * oneWayEff * terminalTier1DiscountFactor * terminalSurplusDiscountFactor
		remAfterTier1 := 6.5 - (3.0 / oneWayEff)
		expected65 := -(tier1Full + remAfterTier1*tier2Rate)
		assert.InDelta(t, expected65, val65, 1e-6, "Tier 2 must value surplus at replacementRate and not double-subtract postHorizonLoadKWH from unfilledBySolarKWH")
	})
}

// TestFinalizeDecisionAndPlan tests the synthesis of Decision and types.Plan from a winning path.
func TestFinalizeDecisionAndPlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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
			startTime:     now,
			endTime:       now.Add(time.Hour),
			durationHours: 1.0,
			price:         currentPrice,
			minSOC:        20,
		},
		{
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

		decision, plan := finalizeDecisionAndPlan(ctx, winningPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.SolarModeAny, decision.Action.SolarMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Equal(t, 95, decision.Action.ChargeToSOC)

		assert.Len(t, plan.Periods, 2)
		assert.InDelta(t, 0.60, plan.TotalProjectedCost, 0.01)
		require.NotNil(t, decision.Action.Plan, "Action.Plan must be populated")
		assert.Equal(t, plan.Periods, decision.Action.Plan.Periods)
	})

	t.Run("PopulatesPlanPeriodSchedule", func(t *testing.T) {
		t.Parallel()

		_, plan := finalizeDecisionAndPlan(ctx, winningPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)
		require.NotNil(t, plan)
		require.Len(t, plan.Periods, 2)

		p0 := plan.Periods[0]
		assert.Equal(t, now, p0.TSStart)
		assert.Equal(t, now.Add(time.Hour), p0.TSEnd)
		assert.Equal(t, types.BatteryModeChargeAny, p0.BatteryMode)
		assert.Equal(t, 50.0, p0.StartSOC)
		assert.Equal(t, 85.0, p0.EndSOC)
		assert.Equal(t, 20.0, p0.ReserveSOC)

		p1 := plan.Periods[1]
		assert.Equal(t, types.BatteryModeLoad, p1.BatteryMode)
		assert.Equal(t, 85.0, p1.StartSOC)
		assert.Equal(t, 70.0, p1.EndSOC)
		assert.Equal(t, 20.0, p1.ReserveSOC)
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

		decision, _ := finalizeDecisionAndPlan(ctx, standbyPath, timeline[:1], fractionalStatus, currentPrice, now, types.Settings{}, nil, nil)
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

		decision, _ := finalizeDecisionAndPlan(ctx, vppPath, timeline[:1], status, currentPrice, now, types.Settings{MinBatterySOC: 20}, nil, nil)
		assert.Equal(t, 5, decision.Action.ChargeToSOC, "Active VPP event with targetSOC=5 must preserve contracted reserve rather than overwriting with minSOC")
	})

	t.Run("CeilsFractionalTargetSOC", func(t *testing.T) {
		t.Parallel()

		fractionalTargetPath := &planPath{
			actions: []actionCandidate{
				{
					batteryMode: types.BatteryModeChargeAny,
					solarMode:   types.SolarModeAny,
					reason:      types.ActionReasonVPPPrep,
					description: "Pre-charging for VPP.",
					targetSOC:   64.2,
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
				{time: now.Add(time.Hour), soc: 64.2, energyKWH: 8.667},
				{time: now.Add(2 * time.Hour), soc: 100, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 3.5, costDollars: 0.42, grossImportCostDollars: 0.42},
				{gridImportKWH: 0.0, costDollars: 0.00, grossImportCostDollars: 0.00},
			},
			totalCost: 0.42,
		}

		decision, _ := finalizeDecisionAndPlan(ctx, fractionalTargetPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, 65, decision.Action.ChargeToSOC, "Fractional targetSOC (64.2%) must be rounded up via math.Ceil to 65%")
	})

	t.Run("DischargeAtPeak_AssignsReasonWhenDischargingAtPeakRate", func(t *testing.T) {
		t.Parallel()

		peakTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				minSOC:        20.0,
			},
			{
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

		decision, plan := finalizeDecisionAndPlan(ctx, peakPath, peakTimeline, status, currentPrice, now, types.Settings{}, nil, nil)
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
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
			{
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

		decision, plan := finalizeDecisionAndPlan(ctx, chargePath, chargeTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)
		assert.Equal(t, types.BatteryModeStandby, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonWaitingToCharge, decision.Action.Reason, "Standby before scheduled cheaper charge must be labeled WaitingToCharge")
		assert.Contains(t, decision.Action.Description, "Waiting to charge at")
		assert.Equal(t, types.ActionReasonWaitingToCharge, plan.Periods[0].Reason)

		// If a non-Standby mode (e.g. DirectSolarExport) occurs between Standby and a later ChargeAny,
		// Step 0 Standby must NOT be labeled WaitingToCharge; it is holding for the intervening export window.
		solarExportThenChargeTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.05,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.25,
				solarKWH:      4.0,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				exportRate:    0.05,
				minSOC:        20.0,
			},
		}
		solarExportThenChargePath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeStandby},
				{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeExport},
				{batteryMode: types.BatteryModeChargeAny, targetSOC: 100},
			},
			states: []planState{
				{time: now, soc: 50.0, energyKWH: 6.75},
				{time: now.Add(time.Hour), soc: 50.0, energyKWH: 6.75},
				{time: now.Add(2 * time.Hour), soc: 35.0, energyKWH: 4.725},
				{time: now.Add(3 * time.Hour), soc: 80.0, energyKWH: 10.8},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 1.0},
				{batSuppliedHomeKWH: 1.5, solarExportKWH: 4.0},
				{gridImportKWH: 6.0},
			},
		}
		decIntervening, _ := finalizeDecisionAndPlan(ctx, solarExportThenChargePath, solarExportThenChargeTimeline, status, currentPrice, now, types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}, nil, nil)
		assert.Equal(t, types.ActionReasonArbitrageHoldExport, decIntervening.Action.Reason,
			"Standby -> DirectSolarExport -> ChargeAny must be labeled ArbitrageHoldExport rather than WaitingToCharge")
	})

	t.Run("SufficientBatteryTillCharge_AssignsReasonWhenLoadCanReachCharge", func(t *testing.T) {
		t.Parallel()

		reachTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
			{
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

		decision, plan := finalizeDecisionAndPlan(ctx, reachPath, reachTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, decision.Action.Reason, "Discharging when battery easily reaches scheduled charge must be labeled SufficientBatteryTillCharge")
		assert.Contains(t, decision.Action.Description, "Sufficient battery to reach scheduled charge")
		assert.Equal(t, types.ActionReasonSufficientBatteryTillCharge, plan.Periods[0].Reason)

		// When a future interval before the scheduled grid charge has a higher minSOC (50%) and the battery
		// drops to 45% (above step 0's 20% minSOC, but below interval 1's 50% minSOC), it does not cleanly
		// stay above reserve until the charge and must NOT be labeled SufficientBatteryTillCharge.
		stepUpReserveTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        50.0,
			},
			{
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				minSOC:        20.0,
			},
		}
		stepUpReservePath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
				{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery},
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 100},
			},
			states: []planState{
				{time: now, soc: 80.0, energyKWH: 10.8},
				{time: now.Add(time.Hour), soc: 55.0, energyKWH: 7.425},
				{time: now.Add(2 * time.Hour), soc: 45.0, energyKWH: 6.075}, // Below interval 1's 50% minSOC
				{time: now.Add(3 * time.Hour), soc: 100.0, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{batSuppliedHomeKWH: 3.0},
				{batSuppliedHomeKWH: 1.2},
				{gridImportKWH: 7.5},
			},
		}
		decStepUp, _ := finalizeDecisionAndPlan(ctx, stepUpReservePath, stepUpReserveTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)
		assert.Equal(t, types.ActionReasonSufficientBattery, decStepUp.Action.Reason, "Must not assign SufficientBatteryTillCharge when battery drops below an intermediate interval's higher minSOC")
	})

	t.Run("ArbitrageHoldExport_AssignsReasonWhenStandbyAheadOfHighExportWindow", func(t *testing.T) {
		t.Parallel()

		exportTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.04,
				minSOC:        20.0,
			},
			{
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

		decision, plan := finalizeDecisionAndPlan(ctx, exportPath, exportTimeline, status, currentPrice, now, types.Settings{
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
		}, nil, nil)
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
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.55,
				minSOC:        20.0,
			},
			{
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

		decision, plan := finalizeDecisionAndPlan(ctx, peakPath, peakTimeline, status, types.Price{DollarsPerKWH: 0.55}, now, types.Settings{
			GridChargeBatteries: true,
		}, nil, nil)
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
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        20.0,
			},
			{
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

		decision, plan := finalizeDecisionAndPlan(ctx, arbPath, arbTimeline, status, types.Price{DollarsPerKWH: 0.10}, now, types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}, nil, nil)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason, "Must label as DeficitChargeNow when no actual solar export occurs in the plan")
		assert.Contains(t, decision.Action.Description, "Pre-charging for upcoming peak rates")
		assert.Equal(t, types.ActionReasonDeficitChargeNow, plan.Periods[0].Reason)
	})

	t.Run("BatteryAtReserve_AbnormalUsage", func(t *testing.T) {
		t.Parallel()

		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		nowChicago := time.Date(2026, 9, 15, 14, 0, 0, 0, chicagoLoc)

		reserveTimeline := []planInterval{
			{
				startTime:     nowChicago,
				endTime:       nowChicago.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				minSOC:        20.0,
			},
		}

		reservePath := &planPath{
			actions: []actionCandidate{
				{
					batteryMode: types.BatteryModeLoad,
					reason:      types.ActionReasonBatteryAtReserve,
					description: "Battery is at reserve.",
				},
			},
			states: []planState{
				{time: nowChicago, soc: 20.0, energyKWH: 2.7},
				{time: nowChicago.Add(time.Hour), soc: 20.0, energyKWH: 2.7},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 1.5},
			},
		}

		// Mock history with high usage in last 2 hours
		history := []types.EnergyStats{
			{
				TSHourStart: nowChicago.Add(-2 * time.Hour),
				HomeKWH:     3.0,
			},
			{
				TSHourStart: nowChicago.Add(-1 * time.Hour),
				HomeKWH:     3.0,
			},
		}

		// Model has Q3 of 0.5 kWh for each hour (total Q3 = 1.0 kWh)
		// Total recent load = 6.0 kWh -> delta = 5.0 kWh >= 1.0 abnormal threshold
		model := []TimeProfile{
			{Hour: 12, P75HomeLoadKWH: 0.5},
			{Hour: 13, P75HomeLoadKWH: 0.5},
			{Hour: 14, P75HomeLoadKWH: 0.5},
		}

		reserveStatus := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         20.0,
		}

		decision, _ := finalizeDecisionAndPlan(ctx, reservePath, reserveTimeline, reserveStatus, currentPrice, nowChicago, types.Settings{MinBatterySOC: 20}, history, model)

		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
		assert.True(t, decision.Action.RecentHomeUsageAbnormal)
		assert.InDelta(t, 6.0, decision.Action.RecentHomeUsageKWH, 0.01)
		assert.InDelta(t, 1.0, decision.Action.Q3HomeUsageKWH, 0.01)
		assert.Equal(t, "Battery is at reserve. Recent usage was well above normal. Home powered from solar/grid.", decision.Action.Description)
	})

	t.Run("ChronologicalNextEvent_PeakDischargeBeforeAfternoonSolarExport", func(t *testing.T) {
		t.Parallel()

		// Step 0: Overnight pre-charge at $0.05/kWh
		// Step 1: Morning peak at $0.35/kWh where battery discharges to cover home load
		// Step 2: Afternoon solar surplus exports to grid at $0.12/kWh
		// Chronological event lookup must see the Step 1 morning peak discharge first and label
		// Step 0 as DeficitChargeNow ("Pre-charging for upcoming peak rates.") rather than ArbitrageChargeExport.
		chronoTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				minSOC:        20.0,
			},
			{
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				price: types.Price{
					TSStart:       now.Add(time.Hour),
					TSEnd:         now.Add(2 * time.Hour),
					DollarsPerKWH: 0.35,
				},
				minSOC: 20.0,
			},
			{
				startTime:     now.Add(6 * time.Hour),
				endTime:       now.Add(7 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.12,
				solarKWH:      5.0,
				minSOC:        20.0,
			},
		}

		chronoPath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 80},
				{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeAny, reason: types.ActionReasonDischargeAtPeak},
				{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeAny, reason: types.ActionReasonSufficientBattery},
			},
			states: []planState{
				{time: now, soc: 20.0, energyKWH: 2.7},
				{time: now.Add(time.Hour), soc: 80.0, energyKWH: 10.8},
				{time: now.Add(2 * time.Hour), soc: 50.0, energyKWH: 6.75},
				{time: now.Add(7 * time.Hour), soc: 100.0, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 8.1},
				{batSuppliedHomeKWH: 4.0},
				{solarExportKWH: 2.0, gridExportKWH: 2.0},
			},
		}

		decision, _ := finalizeDecisionAndPlan(ctx, chronoPath, chronoTimeline, status, types.Price{DollarsPerKWH: 0.05}, now, types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}, nil, nil)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason, "Must label as DeficitChargeNow when morning peak discharge occurs before afternoon solar export")
		assert.Equal(t, "Pre-charging for upcoming peak rates.", decision.Action.Description)
	})

	t.Run("TargetReserveCharge_PrioritizedOverFutureSolarExport", func(t *testing.T) {
		t.Parallel()

		// Step 0: Charging to target reserve (targetSOC == minSOC == 30%)
		// Step 1: Afternoon solar export
		reserveChargeTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				minSOC:        30.0,
			},
			{
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(5 * time.Hour),
				durationHours: 1.0,
				importRate:    0.20,
				exportRate:    0.15,
				solarKWH:      5.0,
				minSOC:        30.0,
			},
		}

		reserveChargePath := &planPath{
			actions: []actionCandidate{
				{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 30},
				{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeAny, reason: types.ActionReasonSufficientBattery},
			},
			states: []planState{
				{time: now, soc: 15.0, energyKWH: 2.025},
				{time: now.Add(time.Hour), soc: 30.0, energyKWH: 4.05},
				{time: now.Add(5 * time.Hour), soc: 100.0, energyKWH: 13.5},
			},
			metrics: []intervalMetrics{
				{gridImportKWH: 2.2},
				{solarExportKWH: 1.5, gridExportKWH: 1.5},
			},
		}

		decision, _ := finalizeDecisionAndPlan(ctx, reserveChargePath, reserveChargeTimeline, status, types.Price{DollarsPerKWH: 0.05}, now, types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}, nil, nil)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Equal(t, "Charging battery to target reserve.", decision.Action.Description)
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
			if period.TSStart.Equal(spikeStart) {
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
		assert.Equal(t, time.Date(2026, 5, 13, 0, 0, 0, 0, chicagoLoc), lastPeriod.TSEnd)
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
			if p.TSStart.Equal(peakTime) {
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
			if p.TSStart.Equal(standbyTime) {
				assert.Equal(t, types.BatteryModeStandby, p.BatteryMode, "must hold Standby 2 hours before VPP event")
				assert.Equal(t, types.ActionReasonVPPPrep, p.Reason)
			}
			if p.TSStart.Equal(vppStart) {
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
			MinBatterySOC:     20,
			EVChargingStandby: true,
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
			if p.TSStart.Equal(lock1Time) || p.TSStart.Equal(lock2Time) {
				assert.Equal(t, types.BatteryModeStandby, p.BatteryMode, "must hold Standby 2h before VPP event at %s", p.TSStart)
				assert.Equal(t, types.ActionReasonVPPPrep, p.Reason)
			}
			if p.TSStart.Equal(vpp1Start) || p.TSStart.Equal(vpp2Start) {
				assert.Equal(t, types.ActionReasonVPPActive, p.Reason, "must discharge during VPP event at %s", p.TSStart)
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

	t.Run("Plan_FiltersFaultsPausedAndStaleActions", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 5, 10, 14, 0, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.04,
			GridUseDollarsPerKWH: 0.02,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50.0,
			MaxBatteryChargeKW: 5.0,
			HomeKW:             1.5,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
		}

		// 14:00-17:00 is flat off-peak ($0.06 delivered), followed by 17:00-20:00 peak ($0.47 delivered).
		// When lastAction is nil, the planner waits in Standby at 14:00 and charges later in the flat block.
		// When lastAction is a valid active ChargeAny, isContinuingCharge anchors the charge at step 0.
		futurePrices := []types.Price{
			{
				TSStart:              now.Add(time.Hour),
				TSEnd:                now.Add(2 * time.Hour),
				DollarsPerKWH:        0.04,
				GridUseDollarsPerKWH: 0.02,
			},
			{
				TSStart:              now.Add(2 * time.Hour),
				TSEnd:                now.Add(3 * time.Hour),
				DollarsPerKWH:        0.04,
				GridUseDollarsPerKWH: 0.02,
			},
			{
				TSStart:              now.Add(3 * time.Hour),
				TSEnd:                now.Add(6 * time.Hour),
				DollarsPerKWH:        0.45,
				GridUseDollarsPerKWH: 0.02,
			},
		}

		// Baseline: When lastAction is valid ChargeAny, step-0 continuity maintains ChargeAny
		validCharge := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Timestamp:   now.Add(-15 * time.Minute),
		}
		decValid, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, validCharge)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, decValid.Action.BatteryMode)

		// 1. Fault action is filtered -> treated as nil -> chooses BatteryModeStandby (WaitingToCharge)
		faultAction := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Fault:       true,
			Timestamp:   now.Add(-15 * time.Minute),
		}
		decFault, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, faultAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decFault.Action.BatteryMode)

		// 2. Paused action is filtered -> chooses BatteryModeStandby
		pausedAction := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Paused:      true,
			Timestamp:   now.Add(-15 * time.Minute),
		}
		decPaused, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, pausedAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decPaused.Action.BatteryMode)

		// 3. BatteryModeNoChange is filtered -> chooses BatteryModeStandby
		noChangeAction := &types.Action{
			BatteryMode: types.BatteryModeNoChange,
			Timestamp:   now.Add(-15 * time.Minute),
		}
		decNoChange, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, noChangeAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decNoChange.Action.BatteryMode)

		// 4. Stale action (>90m) is filtered -> chooses BatteryModeStandby
		staleAction := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			Timestamp:   now.Add(-95 * time.Minute),
		}
		decStale, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, staleAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decStale.Action.BatteryMode)
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
			GridChargeBatteries: true,
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

		// Even with Conservative profile (10% ReserveBufferPercent), when the battery is at 28% SOC
		// above the 20% MinBatterySOC physical reserve with no higher future rate ahead, it must
		// discharge down to the 20% reserve rather than locking into batteryAtReserve at 30%.
		decisionCons, planCons, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settingsConservative, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decisionCons.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBattery, decisionCons.Action.Reason)
		require.NotEmpty(t, planCons.Periods)
		assert.InDelta(t, 20.0, planCons.Periods[len(planCons.Periods)-1].EndSOC, 0.5,
			"Conservative plan must discharge down to the 20% physical reserve floor when covering load")

		settingsAggressive := types.Settings{
			MinBatterySOC:       20,
			OptimizationProfile: "aggressive",
			GridChargeBatteries: true,
		}

		decisionAggr, planAggr, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settingsAggressive, nil)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decisionAggr.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonSufficientBattery, decisionAggr.Action.Reason)
		require.NotEmpty(t, planAggr.Periods)
		assert.InDelta(t, 20.0, planAggr.Periods[len(planAggr.Periods)-1].EndSOC, 0.5)
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
			if p.TSStart.Hour() >= 4 && p.TSStart.Hour() < 6 && p.BatteryMode == types.BatteryModeChargeAny {
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
			if p.TSStart.Hour() == 12 && p.BatteryMode == types.BatteryModeChargeAny {
				chargedHour12 = true
			}
			if p.TSStart.Hour() == 13 && p.BatteryMode == types.BatteryModeChargeAny {
				chargedHour13 = true
			}
			if p.TSStart.Equal(deadline) && p.BatteryMode == types.BatteryModeStandby {
				standbyAtDeadline = true
				assert.GreaterOrEqual(t, p.StartSOC, 99.0, "must reach full 100%% SOC by 14:00 deadline")
			}
		}
		assert.True(t, chargedHour12, "must charge during 12:00-13:00 to satisfy 2-hour charging requirement")
		assert.True(t, chargedHour13, "must charge during 13:00-14:00 (cheapest hour)")
		assert.True(t, standbyAtDeadline, "must hold Standby at 14:00 deadline with battery at 100%%")
	})

	t.Run("VPPPrep_WithUpcomingSolar_ChargesOnlyToSolarHeadroomTarget", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 8, 15, 9, 0, 0, 0, chicagoLoc)
		currentPrice := types.Price{
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
			DollarsPerKWH:        0.06,
			GridUseDollarsPerKWH: 0.02,
		}

		vppStart := time.Date(2026, 8, 15, 13, 0, 0, 0, chicagoLoc)
		vppEnd := time.Date(2026, 8, 15, 15, 0, 0, 0, chicagoLoc)

		var futurePrices []types.Price
		for h := 10; h < 34; h++ {
			rate := 0.06
			if h >= 13 && h < 15 {
				rate = 0.50
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 8, 15, h, 0, 0, 0, chicagoLoc),
				TSEnd:                time.Date(2026, 8, 15, h+1, 0, 0, 0, chicagoLoc),
				DollarsPerKWH:        rate,
				GridUseDollarsPerKWH: 0.02,
			})
		}

		// Battery starts at 20% SOC (15.0 kWh capacity, 3.0 kWh energy)
		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 15.0,
			BatterySOC:         20.0,
			MaxBatteryChargeKW: 8.0,
			HomeKW:             0.5,
			SolarKW:            1.0,
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

		// Historical baseline showing ~4.0 kWh solar per hour from 9:00 to 11:00 (before 11:00 T-2h deadline)
		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			day := now.AddDate(0, 0, -d)
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 9 && h < 11 {
					solar = 4.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, chicagoLoc),
					HomeKWH:      0.5,
					SolarKWH:     solar,
					TimeLocation: "America/Chicago",
				})
			}
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportBatteries: true,
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Controller at 9:00 AM charges from 9:00-10:20 up to ~86% (absorbing both grid power and 9:00-10:20 solar),
		// leaving ~14% headroom for the remaining 10:20-11:00 solar so the battery reaches 100% by the 11:00 T-2h deadline.
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Less(t, decision.Action.ChargeToSOC, 90, "chargeToSOC must leave headroom for remaining daytime solar")

		// Verify that at VPP start (13:00), battery reaches 100% thanks to daytime solar
		var vppPeriod *types.PlanPeriod
		for i := range plan.Periods {
			if plan.Periods[i].TSStart.Equal(vppStart) {
				vppPeriod = &plan.Periods[i]
				break
			}
		}
		require.NotNil(t, vppPeriod, "VPP event period must exist in plan")
		assert.GreaterOrEqual(t, vppPeriod.StartSOC, 99.0, "battery must reach full SOC by VPP start")
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
			assert.Equal(t, chargePeriods[i-1].TSEnd, chargePeriods[i].TSStart, "charging intervals must be contiguous without alternating fragmentation")
		}
	})

	t.Run("BatteryAtReserve_AbnormalUsage", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 9, 20, 20, 15, 0, 0, nyLoc) // 8:15 PM

		currentPrice := types.Price{
			TSStart:              time.Date(2026, 9, 20, 20, 0, 0, 0, nyLoc),
			TSEnd:                time.Date(2026, 9, 20, 21, 0, 0, 0, nyLoc),
			DollarsPerKWH:        0.12,
			GridUseDollarsPerKWH: 0.04,
		}

		var futurePrices []types.Price
		for h := 21; h < 45; h++ {
			day := 20
			hour := h
			if hour >= 24 {
				day = 21
				hour -= 24
			}
			futurePrices = append(futurePrices, types.Price{
				TSStart:              time.Date(2026, 9, day, hour, 0, 0, 0, nyLoc),
				TSEnd:                time.Date(2026, 9, day, hour+1, 0, 0, 0, nyLoc),
				DollarsPerKWH:        0.12,
				GridUseDollarsPerKWH: 0.04,
			})
		}

		// Historical baseline for the past 4 weeks: 1.0 kWh per hour
		var history []types.EnergyStats
		for week := 1; week <= 4; week++ {
			dayStart := now.Add(time.Duration(-week*7*24) * time.Hour).Truncate(24 * time.Hour)
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart:  dayStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:      1.0,
					TimeLocation: "America/New_York",
				})
			}
		}

		// Recent completed hours today: high usage spike (e.g. heavy cooking / appliance use)
		// Hour 18 (6pm-7pm): 3.5 kWh
		// Hour 19 (7pm-8pm): 4.0 kWh
		history = append(history,
			types.EnergyStats{
				TSHourStart:  time.Date(2026, 9, 20, 18, 0, 0, 0, nyLoc),
				HomeKWH:      3.5,
				TimeLocation: "America/New_York",
			},
			types.EnergyStats{
				TSHourStart:  time.Date(2026, 9, 20, 19, 0, 0, 0, nyLoc),
				HomeKWH:      4.0,
				TimeLocation: "America/New_York",
			},
		)

		status := types.SystemStatus{
			Timestamp:          now,
			TimeLocation:       "America/New_York",
			BatteryCapacityKWH: 13.5,
			BatterySOC:         20.0, // Exactly at reserve
			HomeKW:             2.5,
			SolarKW:            0.0,
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: false,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
		}

		decision, _, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
		assert.True(t, decision.Action.RecentHomeUsageAbnormal)
		assert.Equal(t, "Battery is at reserve. Recent usage was well above normal. Home powered from solar/grid.", decision.Action.Description)
		assert.GreaterOrEqual(t, decision.Action.RecentHomeUsageKWH, 7.0)
		assert.Greater(t, decision.Action.Q3HomeUsageKWH, 0.0)
		assert.Greater(t, decision.Action.RecentHomeUsageKWH, decision.Action.Q3HomeUsageKWH)

		// Verify RecentHomeUsageKWH and Q3HomeUsageKWH are always included even when not at reserve
		statusNonReserve := status
		statusNonReserve.BatterySOC = 60.0
		decisionNonReserve, _, err := c.Plan(ctx, statusNonReserve, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		assert.NotEqual(t, types.ActionReasonBatteryAtReserve, decisionNonReserve.Action.Reason)
		assert.True(t, decisionNonReserve.Action.RecentHomeUsageAbnormal)
		assert.GreaterOrEqual(t, decisionNonReserve.Action.RecentHomeUsageKWH, 7.0)
		assert.Greater(t, decisionNonReserve.Action.Q3HomeUsageKWH, 0.0)

		// Verify normal usage history yields RecentHomeUsageAbnormal == false and standard description
		historyNormal := history[:len(history)-2] // exclude recent spike hours
		historyNormal = append(historyNormal,
			types.EnergyStats{
				TSHourStart:  time.Date(2026, 9, 20, 18, 0, 0, 0, nyLoc),
				HomeKWH:      1.0,
				TimeLocation: "America/New_York",
			},
			types.EnergyStats{
				TSHourStart:  time.Date(2026, 9, 20, 19, 0, 0, 0, nyLoc),
				HomeKWH:      1.0,
				TimeLocation: "America/New_York",
			},
		)
		decisionNormal, _, err := c.Plan(ctx, status, currentPrice, futurePrices, historyNormal, nil, settings, nil)
		require.NoError(t, err)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decisionNormal.Action.Reason)
		assert.False(t, decisionNormal.Action.RecentHomeUsageAbnormal)
		assert.Equal(t, "Battery is at reserve. Home powered from solar/grid.", decisionNormal.Action.Description)
	})

	t.Run("BatteryRestingAtReserve_ToleranceSuppressesSpuriousDeficitCharge", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 10, 3, 18, 30, 0, 0, nyLoc)
		currentPrice := types.Price{
			TSStart:       now.Truncate(time.Hour),
			TSEnd:         now.Truncate(time.Hour).Add(time.Hour),
			DollarsPerKWH: 0.105,
		}
		var futurePrices []types.Price
		for h := 1; h < 24; h++ {
			tStep := now.Add(time.Duration(h) * time.Hour)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       tStep,
				TSEnd:         tStep.Add(time.Hour),
				DollarsPerKWH: 0.105,
			})
		}

		// Battery is at 19.789% (resting just below 20% reserve due to normal BMS discretization/tare)
		status := types.SystemStatus{
			Timestamp:          now,
			TimeLocation:       "America/New_York",
			BatteryCapacityKWH: 15.0,
			BatterySOC:         19.789,
			HomeKW:             0.389,
			SolarKW:            0.167,
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true, // Grid charging allowed
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
		}

		decision, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, nil)
		require.NoError(t, err)

		// Must NOT command BatteryModeChargeAny or DeficitCharge
		assert.NotEqual(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.NotEqual(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		assert.Equal(t, types.ActionReasonBatteryAtReserve, decision.Action.Reason)
		assert.Equal(t, "Battery is at reserve. Home powered from solar/grid.", decision.Action.Description)
	})

	t.Run("Franklin_FullBatteryMorningBeforePeak_DischargesLoadNotStandbyForSolarExport", func(t *testing.T) {
		t.Parallel()

		// At 6:01 AM EDT:
		// Battery is at 99.26% SOC (effectively 100% full).
		// Current 6:00 AM import rate is $0.10481/kWh, export rate is $0.024/kWh.
		// Upcoming 7:00 AM peak rate is $0.31443/kWh, export rate is $0.094/kWh.
		// Daytime solar will generate 25+ kWh (peaking at ~5-6 kW), more than enough to refill the battery.
		//
		// RateRudder must NOT hold the battery in Standby to "refill for $0.09/kWh export" while buying
		// $0.105/kWh grid power. The battery has plenty of energy to cover morning load before the 7 AM peak,
		// and solar will easily refill to 100% anyway.
		now := time.Date(2026, 10, 5, 6, 1, 0, 0, nyLoc)

		currentPrice := types.Price{
			TSStart:       time.Date(2026, 10, 5, 6, 0, 0, 0, nyLoc),
			TSEnd:         time.Date(2026, 10, 5, 7, 0, 0, 0, nyLoc),
			DollarsPerKWH: 0.10481,
		}

		var futurePrices []types.Price
		for h := 7; h < 30; h++ {
			day := 5
			hour := h
			if hour >= 24 {
				day = 6
				hour -= 24
			}
			rate := 0.10481
			exportRate := 0.024
			if hour >= 7 && hour < 11 {
				rate = 0.31443
				exportRate = 0.094
			} else if hour >= 11 && hour < 17 {
				rate = 0.15000
				exportRate = 0.094
			} else if hour >= 17 && hour < 20 {
				rate = 0.31443
				exportRate = 0.094
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
			Timestamp:          now,
			TimeLocation:       "America/New_York",
			BatteryCapacityKWH: 15.0,
			BatterySOC:         99.26,
			HomeKW:             0.45,
			SolarKW:            0.0,
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			GridChargeBatteries: true,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
		}

		// Mock 7 days of solar history showing 25 kWh daytime solar
		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 8 && h <= 16 {
					solar = 4.0 // ~36 kWh solar per day
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  now.AddDate(0, 0, -d).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:      0.45,
					SolarKWH:     solar,
					TimeLocation: "America/New_York",
				})
			}
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Must discharge to cover load (BatteryModeLoad)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode,
			"Must discharge battery to cover load at 6 AM; must not stand by and import grid at $0.10/kWh to export at $0.09/kWh")
		// Also verify that even when preceding action was Standby (e.g. overnight idle hold or upcoming export hold),
		// step-0 inertia does not trap the full battery in Standby while off-peak home load is present.
		lastActStandby := &types.Action{
			BatteryMode: types.BatteryModeStandby,
			Reason:      types.ActionReasonArbitrageHoldExport,
			Timestamp:   now.Add(-20 * time.Minute),
		}
		decWithStandby, _, err := c.Plan(ctx, status, currentPrice, futurePrices, history, nil, settings, lastActStandby)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decWithStandby.Action.BatteryMode,
			"Preceding Standby must not block transitioning to BatteryModeLoad when battery is full and off-peak load is present")
	})

	t.Run("WeekendModerateSpread_ChargesAtSuperOffPeakAndDischargesDuringDaytimeOffPeak", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Friday evening 5:30 PM EDT:
		// - Friday 6:00 PM - 7:00 PM is weekday On-Peak ($0.3144/kWh)
		// - Friday 10:00 PM - Saturday 5:00 AM is Super Off-Peak ($0.05500/kWh)
		// - Saturday 5:00 AM - 10:00 PM is weekend Off-Peak ($0.10481/kWh, no weekend On-Peak)
		// Weekend spread ($0.10481 - $0.05500 = $0.04981) is profitable to pre-charge for,
		// but is below minPeakRateSpreadDollars ($0.10).
		friEvening := time.Date(2026, 10, 9, 17, 30, 0, 0, nyLoc)

		rateForHour := func(ts time.Time) float64 {
			h := ts.Hour()
			if h >= 22 || h < 5 {
				return 0.05500
			}
			if ts.Weekday() != time.Saturday && ts.Weekday() != time.Sunday && h == 18 {
				return 0.31440
			}
			return 0.10481
		}

		friHourStart := time.Date(2026, 10, 9, 17, 0, 0, 0, nyLoc)
		currentPrice := types.Price{
			TSStart:       friHourStart,
			TSEnd:         friHourStart.Add(time.Hour),
			DollarsPerKWH: rateForHour(friHourStart),
		}
		var futurePrices []types.Price
		for h := 1; h <= 48; h++ {
			ts := friHourStart.Add(time.Duration(h) * time.Hour)
			futurePrices = append(futurePrices, types.Price{
				TSStart:       ts,
				TSEnd:         ts.Add(time.Hour),
				DollarsPerKWH: rateForHour(ts),
			})
		}

		var history []types.EnergyStats
		for d := 1; d <= 7; d++ {
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 9 && h <= 15 {
					solar = 1.0 // Modest 7 kWh/day solar vs ~38 kWh/day load (net deficit)
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  friHourStart.AddDate(0, 0, -d).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:      1.6,
					SolarKWH:     solar,
					TimeLocation: "America/New_York",
				})
			}
		}

		settings := types.Settings{
			MinBatterySOC:       20,
			OptimizationProfile: "conservative",
			GridChargeBatteries: true,
			GridExportSolar:     false,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringCredits: true,
			},
			SolarNetMeteringCreditsValue: "none",
		}

		friStatus := types.SystemStatus{
			Timestamp:             friEvening,
			TimeLocation:          "America/New_York",
			BatteryCapacityKWH:    15.0,
			BatterySOC:            34.0,
			MaxBatteryChargeKW:    8.0,
			MaxBatteryDischargeKW: 10.0,
			HomeKW:                1.6,
		}

		_, friPlan, err := c.Plan(ctx, friStatus, currentPrice, futurePrices, history, nil, settings, nil)
		require.NoError(t, err)
		require.NotEmpty(t, friPlan.Periods)

		// Verify Friday night charges at $0.055 and Saturday morning (5:00 AM onwards) discharges in BatteryModeLoad
		// all the way down to the 20% physical reserve (not stopping at 30% despite Conservative's 10% ReserveBufferPercent).
		sawSuperOffPeakCharge := false
		minSatAfternoonSOC := 100.0
		satMorningTime := time.Date(2026, 10, 10, 5, 0, 0, 0, nyLoc)
		for _, p := range friPlan.Periods {
			if p.ImportDollars <= 0.056 && p.BatteryMode == types.BatteryModeChargeAny {
				sawSuperOffPeakCharge = true
			}
			if !p.TSStart.Before(satMorningTime) {
				if p.EndSOC < minSatAfternoonSOC {
					minSatAfternoonSOC = p.EndSOC
				}
				if p.StartSOC > 21.0 {
					assert.Equal(t, types.BatteryModeLoad, p.BatteryMode,
						"Saturday (%s) at $0.10481/kWh must discharge down to 20%% reserve, not stay stuck in Standby", p.TSStart.Format(time.RFC3339))
					assert.NotEqual(t, types.ActionReasonBatteryAtReserve, p.Reason,
						"Saturday (%s, StartSOC=%.1f%%) must not report batteryAtReserve while above 20%% reserve", p.TSStart.Format(time.RFC3339), p.StartSOC)
				}
			}
		}
		assert.True(t, sawSuperOffPeakCharge, "Must schedule grid charging during Friday night Super Off-Peak ($0.055/kWh)")
		assert.InDelta(t, 20.0, minSatAfternoonSOC, 0.5, "Saturday trajectory under Conservative must discharge down to the 20% physical reserve, not stop at 30%")

		// Also verify at 4:03 AM Saturday (57 minutes before the 5:00 AM rate increase) at 70.0% SOC
		// with lastAction = Standby (waitingToCharge): the planner must begin charging immediately at 4:03 AM
		// rather than procrastinating until 4:23 AM (the final 37 minutes before 5:00 AM).
		prePeakTime := time.Date(2026, 10, 10, 4, 3, 0, 0, nyLoc)
		prePeakStatus := friStatus
		prePeakStatus.Timestamp = prePeakTime
		prePeakStatus.BatterySOC = 70.0
		prePeakPrice := types.Price{
			TSStart:       time.Date(2026, 10, 10, 4, 0, 0, 0, nyLoc),
			TSEnd:         satMorningTime,
			DollarsPerKWH: 0.05500,
		}
		lastActWaiting := &types.Action{
			BatteryMode:  types.BatteryModeStandby,
			Reason:       types.ActionReasonWaitingToCharge,
			Timestamp:    prePeakTime.Add(-20 * time.Minute),
			CurrentPrice: &types.Price{DollarsPerKWH: 0.05500},
		}
		prePeakDec, _, err := c.Plan(ctx, prePeakStatus, prePeakPrice, futurePrices, history, nil, settings, lastActWaiting)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeChargeAny, prePeakDec.Action.BatteryMode,
			"At 4:03 AM (70.0%% SOC), planner must start charging immediately rather than waiting until 4:23 AM")
		assert.Equal(t, 100, prePeakDec.Action.ChargeToSOC)

		// Also verify step-0 execution at 5:00 AM Saturday when entering from overnight Standby at 100% SOC
		satStatus := friStatus
		satStatus.Timestamp = satMorningTime
		satStatus.BatterySOC = 100.0
		satPrice := types.Price{
			TSStart:       satMorningTime,
			TSEnd:         satMorningTime.Add(time.Hour),
			DollarsPerKWH: 0.10481,
		}
		lastActStandby := &types.Action{
			BatteryMode:  types.BatteryModeStandby,
			Reason:       types.ActionReasonDeficitSaveForPeak,
			Timestamp:    satMorningTime.Add(-20 * time.Minute),
			CurrentPrice: &types.Price{DollarsPerKWH: 0.05500},
		}
		satDec, _, err := c.Plan(ctx, satStatus, satPrice, futurePrices, history, nil, settings, lastActStandby)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, satDec.Action.BatteryMode,
			"At 5:00 AM Saturday, battery must transition out of overnight Standby into BatteryModeLoad")
		assert.NotEqual(t, types.ActionReasonDischargeAtPeak, satDec.Action.Reason,
			"Weekend Off-Peak ($0.10481/kWh) should not be mislabeled as DischargeAtPeak")
	})
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
		MinBatterySOC:       20,
		GridChargeBatteries: true,
		GridExportSolar:     true,
		ManageTOUSchedules:  true,
		GridExportBatteries: true,
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

func TestStepWearCostDollars(t *testing.T) {
	t.Parallel()

	roundTripEff := 0.90
	oneWayEff := math.Sqrt(roundTripEff)
	settings := types.Settings{
		OptimizationProfile:                     "balanced",
		MinBatteryExportDifferenceDollarsPerKWH: 0.07,
	}
	wear := getWearRates(settings, settings.GetOptimizationParams())

	t.Run("BatteryExportWear_UsesProfileDegradationAsFloorForUserMargin", func(t *testing.T) {
		t.Parallel()

		// With balanced profile (gridCharge = $0.02, batteryExportDegradation = $0.03) and MinBatteryExportDifference = $0.07:
		// - exportGatePerKWH = max($0.03, $0.07) = $0.07 (gating grid-charge-to-export cycles)
		// - batteryExportPerKWH = $0.03 - $0.02 = $0.01 (physical export-half wear on BatteryModeExport)
		assert.InDelta(t, 0.07, wear.exportGatePerKWH, 1e-6)
		assert.InDelta(t, 0.005, wear.solarExportPerKWH, 1e-6)
		assert.InDelta(t, 0.02, wear.gridChargePerKWH, 1e-6)
		assert.InDelta(t, 0.01, wear.batteryExportPerKWH, 1e-6)

		cand := actionCandidate{batteryMode: types.BatteryModeExport, solarMode: types.SolarModeAny}
		metrics := intervalMetrics{batExportKWH: 4.0}
		cost := stepWearCostDollars(cand, metrics, roundTripEff, wear)
		assert.InDelta(t, 4.0*0.01, cost, 1e-6)

		// Conservative profile default: BatteryExportDegradation = $0.04, GridChargeDegradation = $0.02 -> batteryExportPerKWH = $0.02
		consSettings := types.Settings{OptimizationProfile: "conservative"}
		consWear := getWearRates(consSettings, consSettings.GetOptimizationParams())
		assert.InDelta(t, 0.02, consWear.batteryExportPerKWH, 1e-6, "Conservative profile must charge $0.02/kWh export wear ($0.04 full-cycle - $0.02 grid charge)")

		// Small MinBatteryExportDifference ($0.01) is floored at profile's $0.03, so neither exportGatePerKWH nor batteryExportPerKWH drops below physical wear
		lowMarginSettings := types.Settings{OptimizationProfile: "balanced", MinBatteryExportDifferenceDollarsPerKWH: 0.01}
		lowMarginWear := getWearRates(lowMarginSettings, lowMarginSettings.GetOptimizationParams())
		assert.InDelta(t, 0.03, lowMarginWear.exportGatePerKWH, 1e-6)
		assert.InDelta(t, 0.01, lowMarginWear.batteryExportPerKWH, 1e-6)
		assert.InDelta(t, 0.02, lowMarginWear.gridChargePerKWH, 1e-6)
	})

	t.Run("GridCharge_AlwaysPaysPhysicalWearNotArbitrageGate", func(t *testing.T) {
		t.Parallel()

		cand := actionCandidate{batteryMode: types.BatteryModeChargeAny}
		chargeAC := 2.70 / oneWayEff

		// Even when MinArbitrageDifferenceDollarsPerKWH ($0.05) > GridChargeDegradationDollarsPerKWH ($0.02),
		// stepWearCostDollars must only charge physical gridChargePerKWH ($0.02), while arbitrageGatePerKWH ($0.05)
		// is reserved for eligibility gating.
		arbSettings := types.Settings{
			OptimizationProfile:                 "balanced",
			MinArbitrageDifferenceDollarsPerKWH: 0.05,
		}
		arbWear := getWearRates(arbSettings, arbSettings.GetOptimizationParams())
		assert.InDelta(t, 0.02, arbWear.gridChargePerKWH, 1e-6)
		assert.InDelta(t, 0.05, arbWear.arbitrageGatePerKWH, 1e-6)

		cost := stepWearCostDollars(cand, intervalMetrics{gridChargeKWH: chargeAC}, roundTripEff, arbWear)
		assert.InDelta(t, chargeAC*roundTripEff*arbWear.gridChargePerKWH, cost, 1e-6,
			"Grid charging must incur physical gridChargePerKWH wear rather than the user's MinArbitrageDifference eligibility threshold")
	})
}

func TestStepPenaltyCost(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	capacityKWH := 13.5

	t.Run("HoldingPenaltyScalesWithEnergyAndDuration", func(t *testing.T) {
		t.Parallel()

		interval := planInterval{
			startTime:     now,
			endTime:       now.Add(time.Hour),
			durationHours: 1.0,
			minSOC:        20.0,
		}

		chargeNextState := planState{energyKWH: 6.0, capacityKWH: capacityKWH}
		costCharge := stepPenaltyCost(chargeNextState, interval)
		expectedHolding := 6.0 * 1.0 * batteryHoldingCostPerHourPerKWH
		assert.InDelta(t, expectedHolding, costCharge, 1e-7)

		exportNextState := planState{energyKWH: 2.7, capacityKWH: capacityKWH}
		costExport := stepPenaltyCost(exportNextState, interval)
		expectedExportHolding := 2.7 * 1.0 * batteryHoldingCostPerHourPerKWH
		assert.InDelta(t, expectedExportHolding, costExport, 1e-7)
	})
}
