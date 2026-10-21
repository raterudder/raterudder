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

		timeline, simParams, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, settings, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, startHour, halfHourPrice1, future30MinPrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, startHour, rate45Price, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, nil, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, nil, nil, nil, types.Settings{MinBatterySOC: 20}, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runTime, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, types.SystemStatus{BatteryCapacityKWH: 13.5})
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, currentPrice, futurePrices, nil, nil, settings, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, shortPrice, shortFuture, nil, nil, settings, status)
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
		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, types.Price{}, nil, nil, nil, settings, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, status.Timestamp, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, now, zeroPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
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

		timeline, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, nil, nil, types.Settings{MinBatterySOC: 20}, status)
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
		}, status)
		require.NoError(t, err)

		// Balanced (15 min buffer)
		timelineBal, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, history, nil, types.Settings{
			OptimizationProfile: "balanced",
		}, status)
		require.NoError(t, err)

		// Conservative (30 min buffer)
		timelineCons, _, _, err := c.buildPlanningTimeline(ctx, runNow, currentPrice, futurePrices, history, nil, types.Settings{
			OptimizationProfile: "conservative",
		}, status)
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
		}, status)
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
		}, morningStatus)
		require.NoError(t, err)
		if assert.GreaterOrEqual(t, len(morningTimeline), 3) {
			// idx 0 (07:45-08:00, 15m): 0.60 kW * 0.25h = 0.15 kWh
			assert.InDelta(t, 0.60*0.25, morningTimeline[0].solarKWH, 0.02)
			// idx 1 (08:00-08:20, 20m) and idx 2 (08:20-08:40, 20m): must remain at hour 8's 3.0 kW * (1/3h) = 1.0 kWh
			assert.InDelta(t, 3.0*(20.0/60.0), morningTimeline[1].solarKWH, 0.05)
			assert.InDelta(t, 3.0*(20.0/60.0), morningTimeline[2].solarKWH, 0.05)
		}
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

		// Default balanced profile has 20-minute VPPChargingBufferMinutes
		anchors := c.detectPlanningAnchors(timeline, nil, status, types.Settings{}, nil)
		require.Len(t, anchors.vppEvents, 2)

		// First VPP is optional, so deadline is event start minus 20m buffer
		assert.Equal(t, vppStart1.Add(-20*time.Minute), anchors.vppEvents[0].deadline)
		assert.Equal(t, 20.0, anchors.vppEvents[0].vppSoc)
		assert.False(t, anchors.vppEvents[0].mandatory)

		// Second VPP is mandatory, so deadline is 2 hours before event start (vppStandbyLeadTime) minus 20m buffer
		assert.Equal(t, vppStart2.Add(-2*time.Hour).Add(-20*time.Minute), anchors.vppEvents[1].deadline)
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

		anchors := c.detectPlanningAnchors(timeline, futurePrices, types.SystemStatus{Timestamp: now}, types.Settings{}, nil)
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
		anchors := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: now}, types.Settings{}, nil)
		assert.InDelta(t, 0.14, anchors.knownPostHorizonRate, 0.001, "must fall back to latest timeline rate")
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

		anchors := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: tEnd.Add(-6 * time.Hour)}, types.Settings{}, mockModel)
		// 4 hours * 1.5 kWh/hour = 6.0 kWh
		if assert.Len(t, anchors.postHorizonSlices, 4) {
			var totalLoad float64
			for _, s := range anchors.postHorizonSlices {
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
		solarAnchors := c.detectPlanningAnchors(timeline, postPrices, types.SystemStatus{Timestamp: tEnd.Add(-6 * time.Hour)}, types.Settings{}, solarModel)
		if assert.Len(t, solarAnchors.postHorizonSlices, 2, "sunny post-horizon hours with net solar surplus must not create net load slices") {
			assert.InDelta(t, 0.30, solarAnchors.postHorizonSlices[0].importRate, 1e-6, "highest-rate post-horizon net load hour must be ordered first")
			assert.InDelta(t, 1.0, solarAnchors.postHorizonSlices[0].loadKWH, 1e-6)
			assert.InDelta(t, 0.10, solarAnchors.postHorizonSlices[1].importRate, 1e-6)
			assert.InDelta(t, 1.0, solarAnchors.postHorizonSlices[1].loadKWH, 1e-6)
		}

		// Verify brand-new site with no energy history (BuildHourlyEnergyModel returns 24 profiles at the 0.09 kWh standby floor)
		// falls back to defaultPostHorizonLoadKWH (6.0 kWh / 4h = 1.5 kWh/hr) paired with post-horizon rates
		emptyModel, _ := c.BuildHourlyEnergyModel(context.Background(), tEnd.Add(-6*time.Hour), nil, nil, types.Settings{})
		coldStartAnchors := c.detectPlanningAnchors(timeline, postPrices, types.SystemStatus{Timestamp: tEnd.Add(-6 * time.Hour)}, types.Settings{}, emptyModel)
		if assert.Len(t, coldStartAnchors.postHorizonSlices, 4, "brand-new site with empty energy history must still populate 4 default load slices") {
			assert.InDelta(t, 0.30, coldStartAnchors.postHorizonSlices[0].importRate, 1e-6)
			assert.InDelta(t, 1.5, coldStartAnchors.postHorizonSlices[0].loadKWH, 1e-6)
			assert.InDelta(t, 0.10, coldStartAnchors.postHorizonSlices[3].importRate, 1e-6)
			assert.InDelta(t, 1.5, coldStartAnchors.postHorizonSlices[3].loadKWH, 1e-6)
		}
	})

	t.Run("PeakSurvivalBufferWindows", func(t *testing.T) {
		t.Parallel()

		t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, chicagoLoc)
		// 12:00-16:00 Off-peak, 16:00-21:00 Peak (5 hours), 21:00-24:00 Off-peak
		timeline := []planInterval{
			{index: 0, startTime: t0, endTime: t0.Add(4 * time.Hour), durationHours: 4.0, importRate: 0.10, loadKWH: 4.0, solarKWH: 2.0},
			{index: 1, startTime: t0.Add(4 * time.Hour), endTime: t0.Add(9 * time.Hour), durationHours: 5.0, importRate: 0.45, loadKWH: 10.0, solarKWH: 2.5},
			{index: 2, startTime: t0.Add(9 * time.Hour), endTime: t0.Add(12 * time.Hour), durationHours: 3.0, importRate: 0.10, loadKWH: 3.0, solarKWH: 0.0},
		}

		settConservative := types.Settings{
			OptimizationProfile: "conservative", // buffer = 30 mins
		}
		anchorsCons := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: t0}, settConservative, nil)
		require.Len(t, anchorsCons.peakWindows, 1)
		pwCons := anchorsCons.peakWindows[0]
		assert.Equal(t, 1, pwCons.startIndex)
		assert.Equal(t, 1, pwCons.endIndex)
		assert.Equal(t, 0.45, pwCons.minPeakRate)
		assert.Equal(t, 0.45, pwCons.maxPeakRate)
		// Buffer 30 mins look-back: avg load during interval 1 is 10.0 kWh / 5h = 2.0 kW -> 2.0 kW * 0.5h = 1.0 kWh
		assert.InDelta(t, 1.0, pwCons.bufferEnergyKWH, 0.01)

		settAggressive := types.Settings{
			OptimizationProfile: "aggressive", // buffer = 0 mins
		}
		anchorsAgg := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: t0}, settAggressive, nil)
		require.Len(t, anchorsAgg.peakWindows, 1)
		pwAgg := anchorsAgg.peakWindows[0]
		// Buffer 0 mins: 0.0 kWh
		assert.InDelta(t, 0.0, pwAgg.bufferEnergyKWH, 0.01)

		settBalanced := types.Settings{
			OptimizationProfile: "balanced", // buffer = 15 mins
		}
		anchorsBal := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: t0}, settBalanced, nil)
		require.Len(t, anchorsBal.peakWindows, 1)
		pwBal := anchorsBal.peakWindows[0]
		// Buffer 15 mins: 2.0 kW * 0.25h = 0.5 kWh
		assert.InDelta(t, 0.5, pwBal.bufferEnergyKWH, 0.01)
	})

	t.Run("NegativeRates_FloorsPostHorizonRateAndBaselineImportAtZero", func(t *testing.T) {
		t.Parallel()

		t0 := time.Date(2026, 7, 10, 2, 0, 0, 0, chicagoLoc)
		// Overnight negative price (-$0.05/kWh) followed by normal $0.06/kWh rate, and ending at -$0.03/kWh.
		// With minDeficitDiff = $0.08, $0.06 - (-$0.05) = $0.11 >= $0.08 would falsely trigger a peak window
		// if baselineImport were not floored at 0.0 ($0.06 - $0.00 = $0.06 < $0.08).
		timeline := []planInterval{
			{index: 0, startTime: t0, endTime: t0.Add(2 * time.Hour), durationHours: 2.0, importRate: -0.05, loadKWH: 2.0},
			{index: 1, startTime: t0.Add(2 * time.Hour), endTime: t0.Add(6 * time.Hour), durationHours: 4.0, importRate: 0.06, loadKWH: 4.0},
			{index: 2, startTime: t0.Add(6 * time.Hour), endTime: t0.Add(8 * time.Hour), durationHours: 2.0, importRate: -0.03, loadKWH: 2.0},
		}

		sett := types.Settings{}
		anchors := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: t0}, sett, nil)
		assert.Equal(t, 0.0, anchors.knownPostHorizonRate, "Negative post-horizon rate must be floored at 0.0")
		assert.Empty(t, anchors.peakWindows, "Normal $0.06/kWh rate after -$0.05/kWh overnight dip must not be classified as a peak window")
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
		assert.Equal(t, PlanActionNegativeOrForceChargeThresholdCharging, candidates[0].actionName)
		logCandidate(ctx, candidates[0], candidateLogData{}, negInterval, planState{energyKWH: 6.75, soc: 50}, true)

		assert.Equal(t, types.BatteryModeStandby, candidates[1].batteryMode)
		assert.Equal(t, types.ActionReasonAlwaysChargeBelowThreshold, candidates[1].reason)
		assert.Equal(t, PlanActionNegativeOrForceChargeThresholdStandby, candidates[1].actionName)
		logCandidate(ctx, candidates[1], candidateLogData{}, negInterval, planState{energyKWH: 6.75, soc: 50}, false)
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

	t.Run("FullBatteryWithSolarRefillBeforePeak_PrunesStandby", func(t *testing.T) {
		t.Parallel()

		// Battery is at 100% SOC during midday solar (loadKWH = 1.0, solarKWH = 3.0) with an upcoming
		// evening peak ($0.35/kWh) and no solar export incentive (GridExportSolar = false).
		// Because surplus solar keeps the battery full before the peak, Standby must be pruned even when
		// preceding action was Standby so the battery switches to Load and covers real-time load spikes.
		noExportSettings := settings
		noExportSettings.GridExportSolar = false
		solarTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       1.0,
				solarKWH:      3.0,
				minSOC:        20,
			},
			{
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.11,
				loadKWH:       1.0,
				solarKWH:      2.5,
				minSOC:        20,
			},
			{
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				loadKWH:       2.0,
				solarKWH:      0.0,
				minSOC:        20,
			},
		}

		stateFull := planState{
			capacityKWH: 13.5,
			energyKWH:   13.5,
			soc:         100.0,
		}

		cands := c.generateActionCandidates(ctx, 0, solarTimeline[0], solarTimeline, stateFull, planningAnchors{}, noExportSettings, status, nil, precedingAction{BatteryMode: types.BatteryModeStandby})
		for _, cand := range cands {
			assert.NotEqual(t, types.BatteryModeStandby, cand.batteryMode, "Standby should be pruned when battery is full and surplus solar precedes the peak")
		}
	})

	t.Run("ReserveFloor_PrunesActiveDischarge", func(t *testing.T) {
		t.Parallel()

		// Battery at 18% SOC (below MinSOC 20% + buffer)
		stateAtReserve := planState{
			energyKWH: 13.5 * 0.18,
			soc:       18,
		}

		candidates := c.generateActionCandidates(ctx, 0, interval, nil, stateAtReserve, planningAnchors{}, settings, status, nil, precedingAction{BatteryMode: types.BatteryModeStandby})
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeLoad {
				assert.Equal(t, types.ActionReasonBatteryAtReserve, cand.reason,
					"Load mode at reserve must declare BatteryAtReserve passthrough")
			}
			assert.NotEqual(t, types.BatteryModeStandby, cand.batteryMode,
				"BatteryModeStandby must be pruned when at or below reserve because Load already acts as passthrough")
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode,
				"BatteryModeExport must be pruned when below reserve")
		}
	})

	t.Run("ActiveEVCharging_EnforcesStandbyAtT0", func(t *testing.T) {
		t.Parallel()

		nightNow := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
		nightInterval := interval
		nightInterval.startTime = nightNow
		nightInterval.endTime = nightNow.Add(time.Hour)

		evSettings := settings
		evSettings.EVChargingStandby = true

		// Current home load shows EV charging active (8.5 kW)
		evStatus := status
		evStatus.Timestamp = nightNow
		evStatus.HomeKW = 8.5

		candidates := c.generateActionCandidates(ctx, 0, nightInterval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, evSettings, evStatus, nil, precedingAction{})
		require.NotEmpty(t, candidates)
		assert.Equal(t, types.BatteryModeStandby, candidates[0].batteryMode)
		assert.Equal(t, types.ActionReasonEVChargingStandby, candidates[0].reason)
		assert.Equal(t, PlanActionActiveEVChargingStandby, candidates[0].actionName)
		logCandidate(ctx, candidates[0], candidateLogData{}, nightInterval, planState{energyKWH: 10.0, soc: 74}, true)
		// Discharging modes (Load, Export) must be strictly pruned during EV charging
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeLoad, cand.batteryMode)
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode)
		}
	})

	t.Run("EVChargingPeriod_NoActiveSession_AllowsNormalSelfConsumption", func(t *testing.T) {
		t.Parallel()

		nightNow := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
		nightInterval := interval
		nightInterval.startTime = nightNow
		nightInterval.endTime = nightNow.Add(time.Hour)

		evSettings := settings
		evSettings.EVChargingStandby = true

		// Current home load is baseline (1.2 kW, no EV charging active)
		idleStatus := status
		idleStatus.Timestamp = nightNow
		idleStatus.HomeKW = 1.2

		candidates := c.generateActionCandidates(ctx, 0, nightInterval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, evSettings, idleStatus, nil, precedingAction{})
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

		nightNow := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
		nightInterval := interval
		nightInterval.startTime = nightNow
		nightInterval.endTime = nightNow.Add(time.Hour)

		evSettings := settings
		evSettings.EVChargingStandby = true

		// Even if step 0 has active EV charging, step 1 (future step) must not be unconditionally forced
		// to Standby because user might not charge across future intervals.
		evStatus := status
		evStatus.Timestamp = nightNow
		evStatus.HomeKW = 8.5

		candidatesFuture := c.generateActionCandidates(ctx, 1, nightInterval, nil, planState{energyKWH: 10.0, soc: 74}, planningAnchors{}, evSettings, evStatus, nil, precedingAction{})
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
		assert.Equal(t, 100.0, candidates[0].targetSOC)
	})

	t.Run("BatteryExportDump_HurdleRequiresRechargeCostPlusMargin", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true

		anchors := planningAnchors{
			knownPostHorizonRate: 0.04, // recharge cost = 0.04 / 0.90 = 0.0444
		}

		// Battery at 80% SOC (> effective reserve + 5%)
		stateHigh := planState{energyKWH: 10.8, soc: 80}

		// Low export rate ($0.06) < 0.0444 + 0.03 ($0.0744): should NOT offer battery export
		lowExportInterval := interval
		lowExportInterval.exportRate = 0.06
		candidatesLow := c.generateActionCandidates(ctx, 0, lowExportInterval, nil, stateHigh, anchors, exportSettings, status, nil, precedingAction{})
		for _, cand := range candidatesLow {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode)
		}

		// High export rate ($0.25) > 0.0744: MUST offer battery export dump
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
		assert.Equal(t, 0.0, candidates[0].targetSOC)
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
		assert.Equal(t, 20.0, candidates[0].targetSOC)
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
		assert.Equal(t, 5.0, candidates15[0].targetSOC)
		assert.Contains(t, candidates15[0].description, "Mandatory VPP Event Active")

		// 2. Battery reached 5% SOC:
		// Target reached, switches to Standby at 5%.
		candidates5 := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 13.5 * 0.05, soc: 5.0}, anchors, userReserveSettings, status, nil, precedingAction{})
		require.Len(t, candidates5, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates5[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPActive, candidates5[0].reason)
		assert.Equal(t, 0.0, candidates5[0].targetSOC)
		assert.Contains(t, candidates5[0].description, "Mandatory VPP Target SOC reached")
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
		assert.Equal(t, 0.0, candidates[0].targetSOC)
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
		assert.Equal(t, 100.0, candidates[0].targetSOC)
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
					assert.Equal(t, 0.0, cand.targetSOC, "Option B candidate leaves targetSOC 0 for trajectory resolution")
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

		nightNow := time.Date(2026, 6, 15, 20, 0, 0, 0, time.UTC)
		evSettings := settings
		evSettings.EVChargingStandby = true

		peakEVInterval := interval
		peakEVInterval.startTime = nightNow
		peakEVInterval.endTime = nightNow.Add(time.Hour)
		peakEVInterval.importRate = 0.40
		peakEVInterval.loadKWH = 6.0

		anchors := planningAnchors{knownPostHorizonRate: 0.08}
		evStatus := status
		evStatus.Timestamp = nightNow
		evStatus.HomeKW = 6.5

		candidates := c.generateActionCandidates(ctx, 0, peakEVInterval, nil, planState{energyKWH: 8.1, soc: 60}, anchors, evSettings, evStatus, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode, "Must not charge battery from grid at 40c/kWh during EV charging")
		}
	})

	t.Run("BatteryExportDump_TargetSOCAndPeakLoadBuffer", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true
		anchors := planningAnchors{
			knownPostHorizonRate: 0.055,
			minHorizonImportRate: 0.055,
		}

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
		assert.Equal(t, 20.0, dumpCand.targetSOC, "TargetSOC must allow exporting down to minSOC (20%)")

		// Even when the TOU peak-to-off-peak spread is under $0.10/kWh (e.g. $0.149/kWh peak vs $0.055/kWh off-peak = $0.094/kWh),
		// remainingPeakLoadKWH must sum across the entire remaining contiguous peak block (2 x 0.19 kWh = 0.38 kWh AC, ~3% SOC
		// on a 13.5 kWh battery at sqrt(0.90) one-way efficiency), budgeting 3% + 5% = 8% SOC above minSOC (28% total).
		// At 26% SOC (within the 8% planned peak buffer) with exportRate == importRate ($0.149/kWh),
		// exporting would force peak grid imports at $0.149 / 0.90 = $0.1656/kWh and must be pruned.
		peakStep0 := interval
		peakStep0.loadKWH = 0.19
		peakStep0.importRate = 0.149
		peakStep0.exportRate = 0.149
		peakStep1 := peakStep0
		offPeakStep2 := interval
		offPeakStep2.importRate = 0.055
		offPeakStep2.exportRate = 0.03
		subTenCentSpreadTimeline := []planInterval{peakStep0, peakStep1, offPeakStep2}

		candsWithinBuffer := c.generateActionCandidates(ctx, 0, peakStep0, subTenCentSpreadTimeline, planState{energyKWH: 3.51, soc: 26, capacityKWH: 13.5}, anchors, exportSettings, status, nil, precedingAction{})
		for i := range candsWithinBuffer {
			assert.NotEqual(t, types.BatteryModeExport, candsWithinBuffer[i].batteryMode,
				"Must not export at 26% SOC when 3% multi-step peak load + 5% buffer (28% SOC) requires comparing against peak grid imports")
		}

		// Above 28% SOC (e.g. 30% SOC), the battery has surplus above the 3% + 5% = 8% planned peak load,
		// so export is evaluated against off-peak replacement cost ($0.055 / 0.90 + $0.03 = $0.0911) and offered.
		candsAboveBuffer := c.generateActionCandidates(ctx, 0, peakStep0, subTenCentSpreadTimeline, planState{energyKWH: 4.05, soc: 30, capacityKWH: 13.5}, anchors, exportSettings, status, nil, precedingAction{})
		hasExportAboveBuffer := false
		for i := range candsAboveBuffer {
			if candsAboveBuffer[i].batteryMode == types.BatteryModeExport {
				hasExportAboveBuffer = true
				break
			}
		}
		assert.True(t, hasExportAboveBuffer, "Must offer battery export at 30% SOC when above the 28% (20% + 3% + 5%) buffered peak threshold")
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
		exportSettings.OptimizationProfile = "conservative" // 0.90 round-trip efficiency, 0.04 battery export degradation
		// Replacement cost = 0.10 / 0.90 = 0.1111
		// Total hurdle = 0.1111 + 0.04 = 0.1511
		anchors := planningAnchors{knownPostHorizonRate: 0.10}

		lowMarginInterval := interval
		lowMarginInterval.exportRate = 0.14 // 0.14 < 0.1511, fails hurdle
		lowMarginInterval.importRate = 0.10

		candidates := c.generateActionCandidates(ctx, 0, lowMarginInterval, nil, planState{energyKWH: 10.8, soc: 80}, anchors, exportSettings, status, nil, precedingAction{})
		for _, cand := range candidates {
			assert.NotEqual(t, types.BatteryModeExport, cand.batteryMode, "Must not export when export rate does not clear replacement cost + hurdle")
		}

		// Verify full round-trip cycle (grid charge -> battery export) treats BatteryExportDegradationDollarsPerKWH ($0.04)
		// as the total cycle degradation hurdle by subtracting gridChargeDegradation ($0.02) at the export step,
		// and scales chargeDegradationCost by roundTripEff so it is per delivered AC kWh ($0.02, not $0.02/0.90 = $0.0222):
		// Import = $0.09 -> rechargeCost = $0.09 / 0.90 = $0.10.
		// Full-cycle hurdle = $0.10 + $0.04 = $0.140 (plus ~$0.0021 pre-peak buffer penalty on the single pre-peak hour = $0.1421).
		// At exportRate = $0.143 (above $0.1421, but below $0.1443 if chargeDegradationCost were not scaled by roundTripEff),
		// both candidate generation and DP search must select grid charging for battery export arbitrage.
		fullCycleSettings := exportSettings
		fullCycleSettings.GridChargeBatteries = true
		fullCycleSettings.GridExportSolar = false
		fullCycleTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.09,
				exportRate:    0.02,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.143,
				minSOC:        20.0,
			},
		}
		initLow := planState{energyKWH: 3.375, soc: 25.0, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0}
		statusLow := status
		statusLow.BatterySOC = 25.0
		path, err := c.searchOptimalPlan(ctx, fullCycleTimeline, initLow, planningAnchors{knownPostHorizonRate: 0.09}, fullCycleSettings, statusLow, nil, nil)
		require.NoError(t, err)
		require.Len(t, path.actions, 2)
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode)
		assert.Equal(t, types.ActionReasonArbitrageChargeExport, path.actions[0].reason)
		assert.Equal(t, types.BatteryModeExport, path.actions[1].batteryMode)
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
		holdSettings.GridChargeBatteries = false
		holdSettings.GridExportSolar = true

		// Case 1: Tonight's import rate is $0.18/kWh; round-trip efficiency (0.90) requires exportRate >= $0.18 / 0.90 = $0.20/kWh.
		// At exportRate = $0.195/kWh (which would have passed a flat +$0.01 hurdle of $0.19, but fails $0.18 / 0.90 = $0.20),
		// Standby (HoldSimilarPrice) must NOT be offered.
		belowRTETimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				exportRate:    0.195, // < 0.18 / 0.90 = 0.20
				solarKWH:      5.0,
				minSOC:        20.0,
			},
		}

		fullState := planState{energyKWH: 10.8, soc: 80.0, capacityKWH: 13.5}
		candsBelowRTE := c.generateActionCandidates(ctx, 0, belowRTETimeline[0], belowRTETimeline, fullState, planningAnchors{}, holdSettings, status, nil, precedingAction{})
		for i := range candsBelowRTE {
			assert.False(t, candsBelowRTE[i].batteryMode == types.BatteryModeStandby && candsBelowRTE[i].reason == types.ActionReasonHoldSimilarPrice,
				"Must not offer HoldSimilarPrice Standby when exportRate ($0.195) is below importRate/roundTripEff ($0.20)")
		}

		// Case 2: At exportRate = $0.205/kWh (>= $0.18 / 0.90 = $0.20) and plentiful solar (5.0 kWh fills 2.7 kWh headroom and overflows),
		// Standby is offered and selected by the DP solver via round-trip efficiency.
		holdTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				exportRate:    0.205, // >= 0.18 / 0.90 = 0.20
				solarKWH:      5.0,   // Fills battery and overflows to export
				minSOC:        20.0,
			},
		}

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

		// Case 3: Cloudy day where solar (1.0 kWh) does NOT fill the battery (starts at 50% SOC = 6.75 kWh, 6.75 kWh empty room)
		// so zero solar overflows to export either way. Without an artificial holdCost penalty on BatteryModeLoad,
		// the DP solver discharges normally tonight rather than sitting in Standby for an export overflow that never happens.
		cloudyTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				exportRate:    0.205,
				solarKWH:      1.0, // Insufficient solar to reach 100% SOC; 0 kWh exported
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(11 * time.Hour),
				endTime:       now.Add(12 * time.Hour),
				durationHours: 1.0,
				importRate:    0.18,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
		}
		halfState := planState{energyKWH: 6.75, soc: 50.0, capacityKWH: 13.5, maxChargeKW: 5.0, maxDischargeKW: 5.0}
		cloudyStatus := status
		cloudyStatus.BatterySOC = 50.0
		cloudyPath, err := c.searchOptimalPlan(ctx, cloudyTimeline, halfState, planningAnchors{knownPostHorizonRate: 0.18}, holdSettings, cloudyStatus, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, cloudyPath.actions)
		assert.Equal(t, types.BatteryModeLoad, cloudyPath.actions[0].batteryMode,
			"Must discharge tonight when tomorrow's solar does not fill the battery to overflow")
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
				importRate:    0.08, // Tonight's import rate is 8c
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(10 * time.Hour),
				endTime:       now.Add(11 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				exportRate:    0.10, // Tomorrow solar export credit is 10c (clears 8c import + 1c hurdle)
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

	t.Run("DirectSolarExport_AtOrBelowReserve_Pruned", func(t *testing.T) {
		t.Parallel()

		sunnyInterval := interval
		sunnyInterval.solarKWH = 4.0
		sunnyInterval.exportRate = 0.20
		touSettings := settings
		touSettings.ManageTOUSchedules = true
		touSettings.GridExportSolar = true

		// 1. At or below reserve (20%), or within solarExportReserveMarginPct (<= 25%): Direct solar export must NOT be offered
		reserveState := planState{energyKWH: 2.7, soc: 20}
		candidates := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, reserveState, planningAnchors{}, touSettings, status, nil, precedingAction{})
		for i := range candidates {
			assert.NotEqual(t, types.SolarModeExport, candidates[i].solarMode, "Direct solar export must NOT be created at 20% SOC")
		}

		withinMarginState := planState{energyKWH: 3.375, soc: 25}
		candsWithinMargin := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, withinMarginState, planningAnchors{}, touSettings, status, nil, precedingAction{})
		for i := range candsWithinMargin {
			assert.NotEqual(t, types.SolarModeExport, candsWithinMargin[i].solarMode, "Direct solar export must NOT be created at 25% SOC (minSOC + solarExportReserveMarginPct)")
		}

		// 2. Above reserve + solarExportReserveMarginPct (> 25%): Direct solar export IS offered
		aboveReserveState := planState{energyKWH: 4.5, soc: 35}
		candsAbove := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, aboveReserveState, planningAnchors{}, touSettings, status, nil, precedingAction{})

		var directSolarAbove *actionCandidate
		for i := range candsAbove {
			if candsAbove[i].solarMode == types.SolarModeExport {
				directSolarAbove = &candsAbove[i]
				break
			}
		}
		require.NotNil(t, directSolarAbove, "Direct solar export must be offered when battery is above reserve + solarExportReserveMarginPct")
		assert.Equal(t, types.ActionReasonDirectExport, directSolarAbove.reason)

		// 3. During VPP prep window (Rule 2) when at or below reserve: Direct solar export must NOT be offered
		vppPrepAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: sunnyInterval.startTime.Add(time.Hour),
					eventEnd:   sunnyInterval.startTime.Add(3 * time.Hour),
					deadline:   sunnyInterval.startTime.Add(time.Hour),
					mandatory:  true,
				},
			},
		}
		noGridChargeSettings := touSettings
		noGridChargeSettings.GridChargeBatteries = false
		vppCandidates := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, reserveState, vppPrepAnchors, noGridChargeSettings, status, nil, precedingAction{})
		var vppDirectSolarCand *actionCandidate
		for i := range vppCandidates {
			if vppCandidates[i].solarMode == types.SolarModeExport {
				vppDirectSolarCand = &vppCandidates[i]
				break
			}
		}
		assert.Nil(t, vppDirectSolarCand, "Direct solar export must NOT be created during VPP prep when battery is at or below reserve")

		// 4. Verify SolarExportDegradationDollarsPerKWH ($0.005 in balanced) gates DirectSolarExport,
		// while grid charging for solar export still requires max(gridChargeDegradation, solarExportDegradation) ($0.02 in balanced):
		anchorsTenCents := planningAnchors{knownPostHorizonRate: 0.10}
		lowExportInterval := sunnyInterval
		lowExportInterval.importRate = 0.10
		lowExportInterval.exportRate = 0.104 // $0.004 spread < $0.005 balanced hurdle
		candsLowExport := c.generateActionCandidates(ctx, 0, lowExportInterval, nil, aboveReserveState, anchorsTenCents, touSettings, status, nil, precedingAction{})
		var directSolarLow *actionCandidate
		for i := range candsLowExport {
			if candsLowExport[i].solarMode == types.SolarModeExport {
				directSolarLow = &candsLowExport[i]
				break
			}
		}
		assert.Nil(t, directSolarLow, "Direct solar export must NOT be created when spread ($0.004) fails to clear $0.005 balanced hurdle")

		clearsSolarHurdle := lowExportInterval
		clearsSolarHurdle.exportRate = 0.106 // $0.006 spread >= $0.005 balanced hurdle
		candsClearsSolar := c.generateActionCandidates(ctx, 0, clearsSolarHurdle, nil, aboveReserveState, anchorsTenCents, touSettings, status, nil, precedingAction{})
		var hasDirectSolarAt6Mills bool
		for i := range candsClearsSolar {
			if candsClearsSolar[i].solarMode == types.SolarModeExport {
				hasDirectSolarAt6Mills = true
				break
			}
		}
		assert.True(t, hasDirectSolarAt6Mills, "Direct solar export must be offered when spread ($0.006) >= $0.005 balanced hurdle")

		gridChargeSettings := touSettings
		gridChargeSettings.GridChargeBatteries = true
		gridChargeSolarTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				exportRate:    0.05,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				exportRate:    0.11, // $0.01 spread over $0.10 import (above $0.005 solarExportDegradation, below $0.02 gridChargeDegradation)
				solarKWH:      4.0,
				minSOC:        20.0,
			},
		}
		candsGridChargeSolar := c.generateActionCandidates(ctx, 0, gridChargeSolarTimeline[0], gridChargeSolarTimeline, aboveReserveState, anchorsTenCents, gridChargeSettings, status, nil, precedingAction{})
		for i := range candsGridChargeSolar {
			assert.NotEqual(t, types.BatteryModeChargeAny, candsGridChargeSolar[i].batteryMode,
				"Must not offer grid charging for solar export when spread ($0.01) is below gridChargeDegradation ($0.02)")
		}
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
		assert.InDelta(t, 82.46, deficitCand.targetSOC, 0.5, "Candidate generation precomputes needed targetSOC via horizonTargetSOC")

		bestPath, err := c.searchOptimalPlan(ctx, timeline, lowState, planningAnchors{}, chargeSettings, lowStatus, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(ctx, bestPath, timeline, lowStatus, types.Price{DollarsPerKWH: 0.05}, now, chargeSettings, nil, nil)
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
		decision, _ := finalizeDecisionAndPlan(ctx, bestPath, solarTimeline, status, types.Price{DollarsPerKWH: 0.05}, now, chargeSettings, nil, nil)
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
		assert.Equal(t, 0.0, arbCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

		bestPath, err := c.searchOptimalPlan(ctx, exportTimeline, lowState, planningAnchors{}, exportSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(ctx, bestPath, exportTimeline, status, types.Price{DollarsPerKWH: 0.05}, now, exportSettings, nil, nil)
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

	t.Run("SelfConsumption_PreferredOverStandbyWhenExportEqualsImport", func(t *testing.T) {
		t.Parallel()

		// Flat pricing tonight ($0.10) and tomorrow solar export ($0.10).
		// Discharging to cover home load is preferred over standing by to export credits
		// when export credit does not clear the hurdle over import rate.
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
		assert.Nil(t, holdCand, "Must not offer BatteryModeStandby with ActionReasonHoldSimilarPrice when export does not clear import + hurdle")

		// Verify DP solver chooses BatteryModeLoad to serve home load (self-consumption)
		bestPath, err := c.searchOptimalPlan(ctx, holdTimeline, chargedState, planningAnchors{}, holdSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(ctx, bestPath, holdTimeline, status, types.Price{DollarsPerKWH: 0.10}, now, holdSettings, nil, nil)
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode, "DP solver must choose Load over Standby when export equals import")
		assert.Equal(t, types.ActionReasonSufficientBattery, decision.Action.Reason)
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
		assert.Equal(t, 0.0, chargeCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

		bestPath, err := c.searchOptimalPlan(ctx, multiPeakTimeline, stateAt50, planningAnchors{}, chargeSettings, status, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(ctx, bestPath, multiPeakTimeline, status, types.Price{DollarsPerKWH: 0.10}, now, chargeSettings, nil, nil)
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
		var reserveCand, arbCand *actionCandidate
		chargeCount := 0
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeChargeAny {
				chargeCount++
				switch candidates[i].actionName {
				case PlanActionReserveTargetCharge:
					reserveCand = &candidates[i]
				case PlanActionGridArbitragePreCharge:
					arbCand = &candidates[i]
				}
			}
		}
		assert.Equal(t, 2, chargeCount, "Must offer both reserve-capped and unconstrained arbitrage ChargeAny candidates when below reserve with peak ahead")
		require.NotNil(t, reserveCand)
		assert.Equal(t, 20.0, reserveCand.targetSOC, "Reserve candidate must clamp targetSOC to interval minSOC (20%)")
		require.NotNil(t, arbCand)
		assert.InDelta(t, 66.85, arbCand.targetSOC, 0.5, "Arbitrage candidate precomputes peak load targetSOC via horizonTargetSOC")

		bestPath, err := c.searchOptimalPlan(ctx, timelinePrePeakLoad, subReserveState, planningAnchors{}, chargeSettings, subStatus, nil, nil)
		require.NoError(t, err)
		decision, _ := finalizeDecisionAndPlan(ctx, bestPath, timelinePrePeakLoad, subStatus, types.Price{DollarsPerKWH: 0.10}, now, chargeSettings, nil, nil)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.GreaterOrEqual(t, decision.Action.ChargeToSOC, 50)
	})

	t.Run("ArbitragePrecharge_AccountsForRoundTripEfficiencyLoss", func(t *testing.T) {
		t.Parallel()

		// Import rate $0.10, export rate $0.135, minArbitrageDiff = $0.03.
		// Nominal spread: $0.135 - $0.10 = $0.035 >= $0.030.
		// Round-trip efficiency ~0.90 -> recharge cost $0.10 / 0.90 = $0.1111.
		// Post-loss spread: $0.135 - $0.1111 = $0.0239 < $0.030.
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
				exportRate:    0.135,
				solarKWH:      16.0,
				minSOC:        20.0,
			},
		}

		lowState := planState{
			energyKWH:   2.7,
			soc:         20.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		// Case 1: Direct Battery Export Arbitrage (evaluated post-losses).
		// Pruned because post-loss spread ($0.0239) does not clear the $0.030 hurdle.
		batterySettings := settings
		batterySettings.GridChargeBatteries = true
		batterySettings.ManageTOUSchedules = true
		batterySettings.GridExportBatteries = true
		batterySettings.GridExportSolar = false

		candidatesBattery := c.generateActionCandidates(ctx, 0, arbTimeline[0], arbTimeline, lowState, planningAnchors{}, batterySettings, status, nil, precedingAction{})
		for _, cand := range candidatesBattery {
			assert.NotEqual(t, types.ActionReasonArbitrageChargeExport, cand.reason,
				"Direct battery export pre-charge must be pruned when export rate does not clear AC round-trip recharge cost + hurdle")
		}

		// Case 2: Solar Export Credit Arbitrage (evaluated on nominal spread).
		// Offered because nominal spread ($0.035) clears the $0.030 hurdle.
		solarSettings := settings
		solarSettings.GridChargeBatteries = true
		solarSettings.ManageTOUSchedules = true
		solarSettings.GridExportBatteries = false
		solarSettings.GridExportSolar = true

		candidatesSolar := c.generateActionCandidates(ctx, 0, arbTimeline[0], arbTimeline, lowState, planningAnchors{}, solarSettings, status, nil, precedingAction{})
		hasSolarArb := false
		for _, cand := range candidatesSolar {
			if cand.reason == types.ActionReasonArbitrageChargeExport {
				hasSolarArb = true
				break
			}
		}
		assert.True(t, hasSolarArb, "Solar export pre-charge must be offered based on nominal spread")
	})

	t.Run("ReserveStepUp_RechargesImmediatelyAtOffPeakStart", func(t *testing.T) {
		t.Parallel()

		// Scenario: Battery has discharged down to 5.0% during an On-Peak period (e.g. 5% reserve).
		// At 19:00, Off-Peak begins ($0.10/kWh) with a 25% target reserve.
		// At 22:00, Super Off-Peak begins ($0.05/kWh) with a 90% target reserve.
		// The battery MUST immediately start charging at 19:00 to reach 25%,
		// and must not idle in standby/deficit waiting for Super Off-Peak.
		timelineReserveStep := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        25.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        25.0,
			},
			{
				index:         2,
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				minSOC:        25.0,
			},
			{
				index:         3,
				startTime:     now.Add(3 * time.Hour),
				endTime:       now.Add(6 * time.Hour),
				durationHours: 3.0,
				importRate:    0.05,
				minSOC:        90.0,
			},
		}

		stateAt5 := planState{
			energyKWH:   13.5 * 0.05,
			soc:         5.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true
		statusAt5 := status
		statusAt5.BatterySOC = 5.0

		bestPath, err := c.searchOptimalPlan(ctx, timelineReserveStep, stateAt5, planningAnchors{}, chargeSettings, statusAt5, nil, nil)
		require.NoError(t, err)
		decision, plan := finalizeDecisionAndPlan(ctx, bestPath, timelineReserveStep, statusAt5, types.Price{DollarsPerKWH: 0.10}, now, chargeSettings, nil, nil)

		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode, "Battery must immediately charge at start of Off-Peak when below 25% reserve")
		assert.Equal(t, types.ActionReasonDeficitChargeNow, decision.Action.Reason)
		require.NotEmpty(t, plan.Periods)
		assert.Equal(t, types.BatteryModeChargeAny, plan.Periods[0].BatteryMode)

		// Scenario 2: Morning On-Peak (5% reserve) ends at 09:00 with battery at 14.7% SOC, and Daytime Off-Peak
		// (09:00-13:00, $0.105/kWh) raises minSOC to 20%, followed by an Afternoon On-Peak (13:00-15:00, $0.314/kWh, 5% reserve)
		// while daytime rooftop solar between 09:20 and 13:00 refills the battery to 100%.
		// Even though hasArbitrageAhead is true (due to the 13:00 peak), the planner must select PlanActionReserveTargetCharge
		// with ChargeToSOC = 20 (stopping at 20% after ~0.8 kWh) rather than overcharging at full 5 kW power to ~32% SOC.
		daytimeSolarReserveTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.105,
				exportRate:    0.111,
				loadKWH:       0.15,
				solarKWH:      0.20,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(20 * time.Minute),
				endTime:       now.Add(4 * time.Hour),
				durationHours: 11.0 / 3.0,
				importRate:    0.105,
				exportRate:    0.111,
				loadKWH:       2.0,
				solarKWH:      22.0, // Surplus solar fills the battery to 100% before 13:00 peak
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(6 * time.Hour),
				durationHours: 2.0,
				importRate:    0.314,
				exportRate:    0.111,
				loadKWH:       2.0,
				solarKWH:      8.0,
				minSOC:        5.0,
			},
		}

		stateAt147 := planState{
			energyKWH:      14.4 * 0.147,
			soc:            14.7,
			capacityKWH:    14.4,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		statusAt147 := status
		statusAt147.BatteryCapacityKWH = 14.4
		statusAt147.BatterySOC = 14.7
		solarChargeSettings := chargeSettings
		solarChargeSettings.GridExportSolar = true

		solarPath, err := c.searchOptimalPlan(ctx, daytimeSolarReserveTimeline, stateAt147, planningAnchors{knownPostHorizonRate: 0.105}, solarChargeSettings, statusAt147, nil, nil)
		require.NoError(t, err)
		solarDecision, _ := finalizeDecisionAndPlan(ctx, solarPath, daytimeSolarReserveTimeline, statusAt147, types.Price{DollarsPerKWH: 0.105}, now, solarChargeSettings, nil, nil)

		assert.Equal(t, types.BatteryModeChargeAny, solarDecision.Action.BatteryMode)
		assert.Equal(t, types.ActionReasonDeficitChargeNow, solarDecision.Action.Reason)
		assert.Equal(t, "Charging battery to target reserve.", solarDecision.Action.Description)
		assert.Equal(t, 20, solarDecision.Action.ChargeToSOC, "Must stop grid charging at 20% target reserve rather than overcharging to 32% before solar refills the battery")
		assert.InDelta(t, 20.0, solarPath.states[1].soc, 0.5, "Step 0 ending SOC must clamp at 20% target reserve")
	})

	t.Run("PostHorizonPeak_OffersStandby", func(t *testing.T) {
		t.Parallel()

		flatTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				exportRate:    0.04,
				minSOC:        20.0,
				loadKWH:       1.0,
			},
			{
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				exportRate:    0.04,
				minSOC:        20.0,
				loadKWH:       1.0,
			},
		}

		stateAt80 := planState{
			energyKWH:   10.8,
			soc:         80.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		statusAt80 := status
		statusAt80.BatterySOC = 80.0

		anchors := planningAnchors{
			knownPostHorizonRate: 0.35,
		}

		candidates := c.generateActionCandidates(ctx, 0, flatTimeline[0], flatTimeline, stateAt80, anchors, settings, statusAt80, nil, precedingAction{})
		var hasStandby bool
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeStandby {
				hasStandby = true
				assert.Equal(t, types.ActionReasonDeficitSaveForPeak, cand.reason)
			}
		}
		assert.True(t, hasStandby, "Standby must be offered when knownPostHorizonRate is significantly higher than current rate")
	})

	t.Run("PostHorizonPeak_DoesNotOfferChargeAny", func(t *testing.T) {
		t.Parallel()

		cheapTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				exportRate:    0.02,
				minSOC:        20.0,
				loadKWH:       1.0,
			},
		}

		stateAt30 := planState{
			energyKWH:   4.05,
			soc:         30.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		statusAt30 := status
		statusAt30.BatterySOC = 30.0

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		anchors := planningAnchors{
			knownPostHorizonRate: 0.40,
		}

		candidates := c.generateActionCandidates(ctx, 0, cheapTimeline[0], cheapTimeline, stateAt30, anchors, chargeSettings, statusAt30, nil, precedingAction{})
		var hasCharge bool
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeChargeAny {
				hasCharge = true
			}
		}
		assert.False(t, hasCharge, "ChargeAny must not be offered for speculative post-horizon peak rates")
	})

	t.Run("PreChargeForExport_DoesNotRequireCurrentSOCAboveReserve", func(t *testing.T) {
		t.Parallel()

		exportTimeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				exportRate:    0.02,
				minSOC:        20.0,
				loadKWH:       0.5,
			},
			{
				startTime:     now.Add(14 * time.Hour),
				endTime:       now.Add(15 * time.Hour),
				durationHours: 1.0,
				importRate:    0.50,
				exportRate:    0.50,
				minSOC:        20.0,
				loadKWH:       0.5,
			},
		}

		stateAtReserve := planState{
			energyKWH:   2.7,
			soc:         20.0, // At reserve!
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		statusAtReserve := status
		statusAtReserve.BatterySOC = 20.0

		exportSettings := settings
		exportSettings.GridChargeBatteries = true
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true

		candidates := c.generateActionCandidates(ctx, 0, exportTimeline[0], exportTimeline, stateAtReserve, planningAnchors{}, exportSettings, statusAtReserve, nil, precedingAction{})
		var hasExportPreCharge bool
		for _, cand := range candidates {
			if cand.batteryMode == types.BatteryModeChargeAny && cand.reason == types.ActionReasonArbitrageChargeExport {
				hasExportPreCharge = true
			}
		}
		assert.True(t, hasExportPreCharge, "Battery at reserve must still be allowed to pre-charge for future battery export arbitrage")
	})

	t.Run("ReserveTargetCharge_SkippedWhenCapDoesNotBind", func(t *testing.T) {
		t.Parallel()

		// 20-minute interval at 15% SOC with 40% reserve target: at 5 kW on a 13.5 kWh battery,
		// unclampedEndSOC is ~26.7% (< 40% reserve cap), and a peak is ahead so hasArbitrageAhead is true.
		// Because the 40% cap cannot bind during this 20-minute step, only PlanActionGridArbitragePreCharge
		// should be emitted (not a duplicate PlanActionReserveTargetCharge that would collide in the same DP bucket).
		shortStepTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.055,
				exportRate:    0.024,
				minSOC:        40.0,
			},
			{
				index:         1,
				startTime:     now.Add(20 * time.Minute),
				endTime:       now.Add(80 * time.Minute),
				durationHours: 1.0,
				importRate:    0.314,
				exportRate:    0.094,
				minSOC:        5.0,
			},
		}

		stateAt15 := planState{
			energyKWH:   13.5 * 0.15,
			soc:         15.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}
		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true

		candidates := c.generateActionCandidates(ctx, 0, shortStepTimeline[0], shortStepTimeline, stateAt15, planningAnchors{}, chargeSettings, status, nil, precedingAction{})
		var reserveCands, arbCands int
		for _, cand := range candidates {
			if cand.actionName == PlanActionReserveTargetCharge {
				reserveCands++
			}
			if cand.actionName == PlanActionGridArbitragePreCharge {
				arbCands++
			}
		}
		assert.Equal(t, 0, reserveCands, "PlanActionReserveTargetCharge must not be emitted when unclamped step cannot reach reserveTargetSOC and arbitrage charge is offered")
		assert.Equal(t, 1, arbCands, "PlanActionGridArbitragePreCharge must still be offered")
	})

	t.Run("BatteryExportDump_FloorCoversReserveStepUpAndPostPeakLoad", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true
		exportSettings.GridExportSolar = false
		exportSettings.GridChargeBatteries = true
		exportSettings.MinBatteryExportDifferenceDollarsPerKWH = 0.02

		// Case 1: Reserve step-up from 5% (On-Peak) to 40% (post-peak) before grid refill
		stepUpTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.314,
				exportRate:    0.094,
				loadKWH:       0.2,
				minSOC:        5.0,
			},
			{
				index:         1,
				startTime:     now.Add(20 * time.Minute),
				endTime:       now.Add(80 * time.Minute),
				durationHours: 1.0,
				importRate:    0.105, // Above exportRate ($0.094), so not a profitable grid refill
				exportRate:    0.024,
				loadKWH:       0.0,
				minSOC:        40.0,
			},
			{
				index:         2,
				startTime:     now.Add(80 * time.Minute),
				endTime:       now.Add(140 * time.Minute),
				durationHours: 1.0,
				importRate:    0.055, // Super Off-Peak grid refill ($0.055 / 0.90 + $0.02 = $0.081 <= $0.094)
				exportRate:    0.024,
				loadKWH:       0.0,
				minSOC:        40.0,
			},
		}

		highState := planState{
			energyKWH:      13.5 * 0.85,
			soc:            85.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		candsStepUp := c.generateActionCandidates(ctx, 0, stepUpTimeline[0], stepUpTimeline, highState, planningAnchors{knownPostHorizonRate: 0.055}, exportSettings, status, nil, precedingAction{})
		var exportCandStepUp *actionCandidate
		for i := range candsStepUp {
			if candsStepUp[i].batteryMode == types.BatteryModeExport {
				exportCandStepUp = &candsStepUp[i]
				break
			}
		}
		require.NotNil(t, exportCandStepUp)
		assert.Equal(t, 40.0, exportCandStepUp.targetSOC, "Export targetSOC must clamp at post-peak 40% reserve step-up")

		// Case 2: Post-peak Off-Peak load (3.0 kWh at $0.105/kWh > $0.094/kWh export) on top of 20% Off-Peak reserve
		postPeakLoadTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.314,
				exportRate:    0.094,
				loadKWH:       0.2,
				minSOC:        5.0,
			},
			{
				index:         1,
				startTime:     now.Add(20 * time.Minute),
				endTime:       now.Add(200 * time.Minute),
				durationHours: 3.0,
				importRate:    0.105, // More expensive than $0.094 export
				exportRate:    0.024,
				loadKWH:       3.0,
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(200 * time.Minute),
				endTime:       now.Add(260 * time.Minute),
				durationHours: 1.0,
				importRate:    0.055,
				exportRate:    0.024,
				loadKWH:       0.5,
				minSOC:        40.0,
			},
		}

		candsPostPeak := c.generateActionCandidates(ctx, 0, postPeakLoadTimeline[0], postPeakLoadTimeline, highState, planningAnchors{knownPostHorizonRate: 0.055}, exportSettings, status, nil, precedingAction{})
		var exportCandPostPeak *actionCandidate
		for i := range candsPostPeak {
			if candsPostPeak[i].batteryMode == types.BatteryModeExport {
				exportCandPostPeak = &candsPostPeak[i]
				break
			}
		}
		require.NotNil(t, exportCandPostPeak)
		// 20% Off-Peak reserve + 3.0 kWh / sqrt(0.90) / 13.5 kWh (~23.4% SOC) = ~43.4% > 40% Super Off-Peak reserve
		assert.GreaterOrEqual(t, exportCandPostPeak.targetSOC, 43.0, "Export targetSOC must cover post-peak Off-Peak load priced above exportRate")
	})

	t.Run("StandbyForFutureExport_RequiresExportGateSpread", func(t *testing.T) {
		t.Parallel()

		exportSettings := settings
		exportSettings.ManageTOUSchedules = true
		exportSettings.GridExportBatteries = true
		exportSettings.MinBatteryExportDifferenceDollarsPerKWH = 0.07

		// Current interval is On-Peak ($0.55/kWh import, $0.14/kWh export).
		// Future interval has $0.59/kWh export (only $0.04/kWh above $0.55/kWh import, below $0.07 exportGatePerKWH)
		// and identical $0.55/kWh import. Standby must NOT be offered.
		narrowExportTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.55,
				exportRate:    0.14,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.55,
				exportRate:    0.59,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
		}

		stateAt80 := planState{
			energyKWH:   13.5 * 0.80,
			soc:         80.0,
			capacityKWH: 13.5,
		}
		cands := c.generateActionCandidates(ctx, 0, narrowExportTimeline[0], narrowExportTimeline, stateAt80, planningAnchors{knownPostHorizonRate: 0.30, minHorizonImportRate: 0.30, maxHorizonImportRate: 0.55}, exportSettings, status, nil, precedingAction{})
		for _, cand := range cands {
			assert.NotEqual(t, types.BatteryModeStandby, cand.batteryMode, "Standby must not be offered when future export rate does not clear importRate + exportGatePerKWH")
		}
	})

	t.Run("ReserveDeficitTolerance_SkipsOnePercentBMSDriftAndChargesGenuineDeficit", func(t *testing.T) {
		t.Parallel()

		// Peak hour ($0.35/kWh) followed by off-peak ($0.10/kWh) on a 20% reserve.
		peakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
		}
		anchors := planningAnchors{knownPostHorizonRate: 0.15, minHorizonImportRate: 0.10, maxHorizonImportRate: 0.35}

		// 1. At 19.0% SOC (1.0% BMS drift below 20% reserve), ChargeAny must NOT be generated during the peak hour.
		stateAt19 := planState{
			energyKWH:   13.5 * 0.19,
			soc:         19.0,
			capacityKWH: 13.5,
		}
		statusAt19 := status
		statusAt19.BatterySOC = 19.0
		cands19 := c.generateActionCandidates(ctx, 0, peakTimeline[0], peakTimeline, stateAt19, anchors, settings, statusAt19, nil, precedingAction{})
		for _, cand := range cands19 {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode,
				"19.0%% SOC on a 20%% reserve (<= 1.05%% BMS drift tolerance) must not trigger grid charging during a peak hour")
		}

		// 2. At 18.0% SOC (2.0% deficit > 1.05% tolerance), ChargeAny MUST be generated to restore the reserve.
		stateAt18 := planState{
			energyKWH:   13.5 * 0.18,
			soc:         18.0,
			capacityKWH: 13.5,
		}
		statusAt18 := status
		statusAt18.BatterySOC = 18.0
		cands18 := c.generateActionCandidates(ctx, 0, peakTimeline[0], peakTimeline, stateAt18, anchors, settings, statusAt18, nil, precedingAction{})
		hasDeficitCharge := false
		for _, cand := range cands18 {
			if cand.batteryMode == types.BatteryModeChargeAny {
				hasDeficitCharge = true
				assert.Equal(t, types.ActionReasonDeficitChargeNow, cand.reason)
			}
		}
		assert.True(t, hasDeficitCharge, "18.0%% SOC on a 20%% reserve must generate a deficit ChargeAny candidate")
	})

	t.Run("ArbitragePreCharge_ClampsTargetSOCAndAvoidsSameWindowLoadChurn", func(t *testing.T) {
		t.Parallel()

		// Multi-interval off-peak window ($0.05/kWh) followed by daytime surplus solar and an evening peak ($0.40/kWh).
		// Evening peak requires 4.0 kWh of load on top of 20% reserve (~51.3% SOC before peak), and daytime solar
		// supplies 1.5 kWh of net surplus (~10.5% SOC), so overnight grid charging only needs ~41% SOC (well below 100%).
		// In a multi-step 20-minute off-peak window with small loads (~0.05 kWh/step < 0.5% SOC bucket width),
		// the plan must clamp grid charging to targetSOC and stay in Standby rather than flipping between ChargeAny and Load.
		offPeakToPeakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.05,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(20 * time.Minute),
				endTime:       now.Add(40 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.05,
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(40 * time.Minute),
				endTime:       now.Add(60 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.05,
				minSOC:        20.0,
			},
			{
				index:         3,
				startTime:     now.Add(60 * time.Minute),
				endTime:       now.Add(120 * time.Minute),
				durationHours: 1.0,
				importRate:    0.12,
				exportRate:    0.04,
				loadKWH:       0.5,
				solarKWH:      2.0, // 1.5 kWh net surplus solar
				minSOC:        20.0,
			},
			{
				index:         4,
				startTime:     now.Add(120 * time.Minute),
				endTime:       now.Add(180 * time.Minute),
				durationHours: 1.0,
				importRate:    0.40,
				exportRate:    0.08,
				loadKWH:       4.0,
				minSOC:        20.0,
			},
		}

		chargeSettings := settings
		chargeSettings.GridChargeBatteries = true
		chargeSettings.GridExportBatteries = false

		stateAt20 := planState{
			energyKWH:      13.5 * 0.20,
			soc:            20.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		statusAt20 := status
		statusAt20.BatterySOC = 20.0
		anchors := planningAnchors{
			knownPostHorizonRate: 0.05,
			minHorizonImportRate: 0.05,
			maxHorizonImportRate: 0.40,
		}

		cands0 := c.generateActionCandidates(ctx, 0, offPeakToPeakTimeline[0], offPeakToPeakTimeline, stateAt20, anchors, chargeSettings, statusAt20, nil, precedingAction{})
		var preChargeCand *actionCandidate
		for i := range cands0 {
			if cands0[i].actionName == PlanActionGridArbitragePreCharge {
				preChargeCand = &cands0[i]
				break
			}
		}
		require.NotNil(t, preChargeCand)
		assert.Greater(t, preChargeCand.targetSOC, 35.0, "Pre-charge targetSOC must cover peak deficit net of solar")
		assert.Less(t, preChargeCand.targetSOC, 55.0, "Pre-charge targetSOC must clamp below 100%% using horizonTargetSOC")

		bestPath, err := c.searchOptimalPlan(ctx, offPeakToPeakTimeline, stateAt20, anchors, chargeSettings, statusAt20, nil, nil)
		require.NoError(t, err)
		if assert.Len(t, bestPath.actions, 5) {
			for step := 0; step < 3; step++ {
				assert.NotEqual(t, types.BatteryModeLoad, bestPath.actions[step].batteryMode,
					"Must not discharge (BatteryModeLoad) during the same off-peak price window as grid charging (step %d)", step)
			}
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

	anchors := planningAnchors{
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

	t.Run("EndingWithinReserveTolerance_NoPenaltyOrCredit", func(t *testing.T) {
		t.Parallel()

		// Ending at 19.95% SOC when target reserve is 20.0% (within 0.1% tolerance)
		finalState := planState{
			energyKWH: capacityKWH * 0.1995,
			soc:       19.95,
		}

		valuation := calculateTerminalValuation(finalState, anchors, settings, nil, capacityKWH, roundTripEff)
		assert.Equal(t, 0.0, valuation, "ending within 0.1% reserve tolerance must produce zero penalty or credit")
	})

	t.Run("ScheduledTOUReserveDeficit_ValuedAtReplacementRate", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
		// Base emergency reserve is 20%, but the last interval is Super Off-Peak with 90% minSOC
		sopTimeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), minSOC: 90.0, importRate: 0.055},
		}
		// Ending at 40% SOC (above base reserve 20%, but below scheduled TOU reserve 90%)
		finalState := planState{
			energyKWH: capacityKWH * 0.40,
			soc:       40.0,
		}
		chargeSettings := settings
		chargeSettings.MinBatterySOC = 20.0
		chargeSettings.GridChargeBatteries = true

		valuation := calculateTerminalValuation(finalState, anchors, chargeSettings, sopTimeline, capacityKWH, roundTripEff)
		// Deficit is (90% - 40%) * capacityKWH
		deficitKWH := (0.90 - 0.40) * capacityKWH
		expectedCost := (deficitKWH / roundTripEff) * anchors.knownPostHorizonRate
		assert.InDelta(t, expectedCost, valuation, 1e-4, "ending below scheduled TOU reserve with grid charging enabled must be valued at replacement rate")
	})

	t.Run("NegativePostHorizonRate_DoesNotPenalizeStoredEnergy", func(t *testing.T) {
		t.Parallel()

		negAnchors := planningAnchors{
			knownPostHorizonRate: -0.05,
		}
		surplusState := planState{
			energyKWH: capacityKWH * 0.80,
			soc:       80.0,
		}

		valuation := calculateTerminalValuation(surplusState, negAnchors, settings, nil, capacityKWH, roundTripEff)
		assert.InDelta(t, 0.0, valuation, 1e-6, "Negative post-horizon rate must be floored at 0.0 so banked energy is never penalized")
	})

	t.Run("PerHourPostHorizonSlices_ValuesEachSliceAtConsumptionRate", func(t *testing.T) {
		t.Parallel()

		oneWayEff := math.Sqrt(roundTripEff)
		targetEnergy := capacityKWH * 0.20 // 2.7 kWh (usable capacity = 10.8 kWh)
		sliceAnchors := planningAnchors{
			knownPostHorizonRate:       0.25, // Higher 4h mean than lowest slice (0.10)
			postHorizonSolarSurplusKWH: 6.8,  // Leaves 10.8 - 6.8 = 4.0 kWh of Tier 2 headroom after morning load drains
			postHorizonSlices: []postHorizonLoadSlice{
				{loadKWH: 1.0, importRate: 0.30},
				{loadKWH: 2.0, importRate: 0.10},
			},
		}

		// 1. 2.5 kWh above reserve: 1.0 kWh credited at $0.30 and 1.5 kWh credited at $0.10 (with 0.95 Tier 1 discount)
		state25 := planState{
			energyKWH: targetEnergy + 2.5,
			soc:       ((targetEnergy + 2.5) / capacityKWH) * 100.0,
		}
		val25 := calculateTerminalValuation(state25, sliceAnchors, settings, nil, capacityKWH, roundTripEff)
		expected25 := -((1.0 * 0.30 * oneWayEff * terminalTier1DiscountFactor) + (1.5 * 0.10 * oneWayEff * terminalTier1DiscountFactor))
		assert.InDelta(t, expected25, val25, 1e-6, "Each post-horizon slice must be valued at the rate of the hour when it is consumed")

		// 2. 6.5 kWh above reserve:
		// - 3.0 kWh covered by Tier 1 slices (1.0 kWh @ $0.30 + 2.0 kWh @ $0.10)
		// - Remaining 3.5 kWh <= unfilledBySolarKWH (10.8 - 6.8 = 4.0 kWh), credited at Tier 2 clamped to lowest slice ($0.10 * 0.95 * 0.70)
		state65 := planState{
			energyKWH: targetEnergy + 6.5,
			soc:       ((targetEnergy + 6.5) / capacityKWH) * 100.0,
		}
		val65 := calculateTerminalValuation(state65, sliceAnchors, settings, nil, capacityKWH, roundTripEff)
		tier1Full := (1.0 * 0.30 * oneWayEff * terminalTier1DiscountFactor) + (2.0 * 0.10 * oneWayEff * terminalTier1DiscountFactor)
		tier2Rate := 0.10 * oneWayEff * terminalTier1DiscountFactor * terminalSurplusDiscountFactor
		expected65 := -(tier1Full + 3.5*tier2Rate)
		assert.InDelta(t, expected65, val65, 1e-6, "Tier 2 must clamp base rate to lowest Tier 1 slice and not double-subtract postHorizonLoadKWH from unfilledBySolarKWH")
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

		anchors := c.detectPlanningAnchors(flatTimeline, nil, status, settings, nil)
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

	t.Run("VPPFeasibility_GridChargeDisabled_DoesNotPenalizePlan", func(t *testing.T) {
		t.Parallel()

		vppAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: now.Add(3 * time.Hour),
					deadline:   now.Add(time.Hour),
					vppSoc:     20,
					mandatory:  true,
				},
			},
		}

		// Battery starts at 20% SOC
		lowState := planState{
			energyKWH:   13.5 * 0.20,
			soc:         20,
			capacityKWH: 13.5,
		}

		noGridChargeSettings := settings
		noGridChargeSettings.GridChargeBatteries = false

		path, err := c.searchOptimalPlan(ctx, timeline, lowState, vppAnchors, noGridChargeSettings, status, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, path.actions)

		// Without grid charging, battery cannot charge from grid to meet the deadline.
		// Feasibility penalty must be 0 and not dominate or force prohibitive penalties.
		assert.True(t, path.totalCost < 500.0, "Total cost should not include the $1,000+ VPP feasibility penalty")

		// Also verify calculateVPPShortfallStrikes directly returns 0 when gridChargeBatteries is false
		strikes := calculateVPPShortfallStrikes(lowState, lowState, now.Add(time.Hour), 0.95, false)
		assert.Equal(t, 0, strikes)

		// And returns 0 when chargingDisabled is true
		disabledState := lowState
		disabledState.chargingDisabled = true
		strikesDisabled := calculateVPPShortfallStrikes(disabledState, lowState, now.Add(time.Hour), 0.95, true)
		assert.Equal(t, 0, strikesDisabled)
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

	t.Run("ExecuteLogs_And_LogChosenCandidates", func(t *testing.T) {
		t.Parallel()

		testTimeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, minSOC: 20},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.15, minSOC: 20},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.05, minSOC: 20},
			{startTime: now.Add(3 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.05, minSOC: 20},
		}
		testStates := []planState{
			{soc: 50, energyKWH: 6.75, capacityKWH: 13.5},
			{soc: 45, energyKWH: 6.075, capacityKWH: 13.5},
			{soc: 40, energyKWH: 5.4, capacityKWH: 13.5},
			{soc: 55, energyKWH: 7.425, capacityKWH: 13.5},
		}

		p := &planPath{
			actions: []actionCandidate{
				{
					batteryMode: types.BatteryModeStandby,
					reason:      types.ActionReasonHoldSimilarPrice,
					actionName:  PlanActionBatteryStandby,
				},
				{
					batteryMode: types.BatteryModeLoad,
					reason:      types.ActionReasonSufficientBattery,
					actionName:  PlanActionDischargingBattery,
				},
				{
					batteryMode: types.BatteryModeChargeAny,
					reason:      types.ActionReasonDeficitChargeNow,
					actionName:  PlanActionGridArbitragePreCharge,
				},
				{
					// Consecutive duplicate of previous action: must be skipped by executeLogs
					batteryMode: types.BatteryModeChargeAny,
					reason:      types.ActionReasonDeficitChargeNow,
					actionName:  PlanActionGridArbitragePreCharge,
				},
			},
			logData: []candidateLogData{
				{refillExportRate: 0.10, minSOC: 20},
				{minSOC: 20},
				{roundTripEff: 0.85, rechargeCost: 0.05 / 0.85},
				{roundTripEff: 0.85, rechargeCost: 0.05 / 0.85},
			},
			states:   testStates,
			timeline: testTimeline,
			initialCandidates: []actionCandidate{
				{
					batteryMode: types.BatteryModeStandby,
					reason:      types.ActionReasonHoldSimilarPrice,
					actionName:  PlanActionBatteryStandby,
				},
				{
					batteryMode: types.BatteryModeChargeAny,
					reason:      types.ActionReasonDeficitChargeNow,
					actionName:  PlanActionReserveTargetCharge,
				},
			},
			modeScores: map[types.BatteryMode]float64{
				types.BatteryModeStandby:   1.0,
				types.BatteryModeChargeAny: 1.5,
			},
			bestScore: 1.0,
		}

		// executeLogs must run without panic on winning path and unchosen step 0 candidate
		assert.NotPanics(t, func() {
			p.executeLogs(ctx)
		})

		// Calling executeLogs on nil planPath must be safe
		var nilPath *planPath
		assert.NotPanics(t, func() {
			nilPath.executeLogs(ctx)
		})

		// Verify all defined action names are unique, non-empty, and handled safely in logCandidate
		seenNames := make(map[PlanActionName]bool)
		for _, name := range AllPlanActionNames {
			assert.NotEmpty(t, name)
			assert.False(t, seenNames[name], "duplicate action name: %s", name)
			seenNames[name] = true

			cand := actionCandidate{
				actionName: name,
			}
			assert.NotPanics(t, func() {
				logCandidate(ctx, cand, candidateLogData{}, testTimeline[0], testStates[0], true)
				logCandidate(ctx, cand, candidateLogData{}, testTimeline[0], testStates[0], false)
			})
		}

		// Fallback for custom or unhandled action name logs default parameters safely
		fallbackCand := actionCandidate{
			batteryMode: types.BatteryModeLoad,
			solarMode:   types.SolarModeAny,
			reason:      types.ActionReasonSufficientBattery,
			actionName:  "custom unhandled candidate name",
		}
		assert.NotPanics(t, func() {
			logCandidate(ctx, fallbackCand, candidateLogData{}, testTimeline[0], testStates[0], true)
			logCandidate(ctx, fallbackCand, candidateLogData{}, testTimeline[0], testStates[0], false)
		})
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

	t.Run("ModeSwitchPenalty_ContiguousExport", func(t *testing.T) {
		t.Parallel()

		// 3 equal 1-hour peak intervals at high export rate ($0.30/kWh).
		// Battery has enough capacity to export for 2 hours, but not all 3.
		// modeSwitchPenalty ensures [Export, Export, Load] wins over fragmented [Export, Load, Export].
		peakTimeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.35, exportRate: 0.30, loadKWH: 0.2, minSOC: 10},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.35, exportRate: 0.30, loadKWH: 0.2, minSOC: 10},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, exportRate: 0.30, loadKWH: 0.2, minSOC: 10},
		}

		exportState := planState{
			energyKWH:      10.0,
			soc:            100.0,
			capacityKWH:    10.0,
			maxDischargeKW: 5.0,
		}

		exportStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    10.0,
			BatterySOC:            100.0,
			MaxBatteryDischargeKW: 5.0,
		}

		exportSettings := types.Settings{
			MinBatterySOC:       10,
			GridExportBatteries: true,
		}

		path, err := c.searchOptimalPlan(ctx, peakTimeline, exportState, planningAnchors{}, exportSettings, exportStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, path.actions, 3)

		// Must not fragment export across an intervening load interval
		if path.actions[0].batteryMode == types.BatteryModeExport && path.actions[2].batteryMode == types.BatteryModeExport {
			assert.Equal(t, types.BatteryModeExport, path.actions[1].batteryMode, "export should not be interrupted by load when contiguous export is possible")
		}
	})

	t.Run("DischargeDuringPeak_DespiteSuperOffPeakReserve", func(t *testing.T) {
		t.Parallel()

		// Interval 0: Peak pricing ($0.35/kWh), home load 2.0 kWh, minSOC 5%
		// Interval 1: Super Off-Peak pricing ($0.055/kWh), home load 0.5 kWh, minSOC 90%
		peakThenSOPTimeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 2.0, minSOC: 5},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.5, minSOC: 90},
		}

		sopState := planState{
			energyKWH:      10.0,
			soc:            100.0,
			capacityKWH:    10.0,
			maxDischargeKW: 5.0,
			maxChargeKW:    5.0,
		}

		sopStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    10.0,
			BatterySOC:            100.0,
			MaxBatteryDischargeKW: 5.0,
			MaxBatteryChargeKW:    5.0,
		}

		sopSettings := types.Settings{
			MinBatterySOC:       5,
			GridChargeBatteries: true,
		}

		sopAnchors := planningAnchors{
			knownPostHorizonRate: 0.055,
		}

		path, err := c.searchOptimalPlan(ctx, peakThenSOPTimeline, sopState, sopAnchors, sopSettings, sopStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, path.actions, 2)

		// Interval 0 must discharge to cover peak load rather than staying in Standby
		assert.Equal(t, types.BatteryModeLoad, path.actions[0].batteryMode, "must discharge battery during peak rate despite high scheduled reserve in subsequent off-peak period")
		assert.True(t, path.states[1].soc < 100.0, "SOC must decrease during peak load")
	})

	t.Run("MultiWindowArbitrage_ChargesBothProfitableWindows", func(t *testing.T) {
		t.Parallel()

		// 30m window at $0.05 followed by 30m window at $0.06, followed by 2h peak export at $0.30.
		// Battery starts at 10% SOC. With 5 kW max charge into 13.5 kWh, 30m only charges ~2.37 kWh (+17.5% SOC),
		// leaving ample headroom (~9.8 kWh).
		// Since both $0.05 and $0.06 clear the round-trip recharge cost + $0.05 minArbitrageDiff relative to $0.30,
		// both windows should charge rather than skipping the second window just because a cheaper one occurred earlier.
		multiArbTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(30 * time.Minute),
				durationHours: 0.5,
				importRate:    0.05,
				exportRate:    0.04,
				minSOC:        10,
			},
			{
				index:         1,
				startTime:     now.Add(30 * time.Minute),
				endTime:       now.Add(60 * time.Minute),
				durationHours: 0.5,
				importRate:    0.06,
				exportRate:    0.04,
				minSOC:        10,
			},
			{
				index:         2,
				startTime:     now.Add(60 * time.Minute),
				endTime:       now.Add(180 * time.Minute),
				durationHours: 2.0,
				importRate:    0.35,
				exportRate:    0.30,
				loadKWH:       0.2,
				minSOC:        10,
			},
			{
				index:         3,
				startTime:     now.Add(180 * time.Minute),
				endTime:       now.Add(360 * time.Minute),
				durationHours: 3.0,
				importRate:    0.05,
				loadKWH:       1.0,
				minSOC:        10,
			},
		}

		arbState := planState{
			energyKWH:      13.5 * 0.10,
			soc:            10.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
			maxChargeKW:    5.0,
		}

		arbStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            10.0,
			MaxBatteryDischargeKW: 5.0,
			MaxBatteryChargeKW:    5.0,
		}

		arbSettings := types.Settings{
			MinBatterySOC:       10,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
		}

		anchors := c.detectPlanningAnchors(multiArbTimeline, nil, arbStatus, arbSettings, nil)
		path, err := c.searchOptimalPlan(ctx, multiArbTimeline, arbState, anchors, arbSettings, arbStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, path.actions, 4)

		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode, "Window 0 ($0.05) must charge")
		assert.Equal(t, types.BatteryModeChargeAny, path.actions[1].batteryMode, "Window 1 ($0.06) must also charge since window 0 alone is not enough time to fully charge and $0.06 is still profitable")
		assert.Equal(t, types.BatteryModeExport, path.actions[2].batteryMode, "Window 2 ($0.30) must export stored energy")
	})

	t.Run("ReserveStepUp_ChargesImmediatelyRegardlessOfPeak", func(t *testing.T) {
		t.Parallel()

		// Scenario: Battery is at 5% SOC during peak pricing ($0.45/kWh), but user configured reserve is 25%.
		// Later there is a cheap super off-peak period ($0.05/kWh).
		// When solar cannot cover the home, the battery MUST charge immediately to 25% despite peak rates.
		peakReserveTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				loadKWH:       1.0,
				solarKWH:      0.0,
				minSOC:        25.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				loadKWH:       1.0,
				solarKWH:      0.0,
				minSOC:        25.0,
			},
			{
				index:         2,
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(6 * time.Hour),
				durationHours: 4.0,
				importRate:    0.05,
				loadKWH:       1.0,
				solarKWH:      0.0,
				minSOC:        25.0,
			},
		}

		stateAt5 := planState{
			energyKWH:      13.5 * 0.05,
			soc:            5.0,
			capacityKWH:    13.5,
			maxDischargeKW: 5.0,
			maxChargeKW:    5.0,
		}

		statusAt5 := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            5.0,
			MaxBatteryDischargeKW: 5.0,
			MaxBatteryChargeKW:    5.0,
		}

		reserveSettings := types.Settings{
			MinBatterySOC:       25,
			GridChargeBatteries: true,
		}

		anchors := c.detectPlanningAnchors(peakReserveTimeline, nil, statusAt5, reserveSettings, nil)
		path, err := c.searchOptimalPlan(ctx, peakReserveTimeline, stateAt5, anchors, reserveSettings, statusAt5, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, path.actions)

		assert.Equal(t, types.BatteryModeChargeAny, path.actions[0].batteryMode, "must immediately charge battery to reserve during peak rates when below reserve")
		decision, _ := finalizeDecisionAndPlan(ctx, path, peakReserveTimeline, statusAt5, types.Price{DollarsPerKWH: 0.45}, now, reserveSettings, nil, nil)
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.Equal(t, 25, decision.Action.ChargeToSOC)

		// Scenario 2: With surplus solar (solarKWH > loadKWH), grid charging is not required,
		// but the battery must still recharge from surplus solar.
		surplusSolarTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				loadKWH:       1.0,
				solarKWH:      4.0, // Surplus solar = 3.0 kWh
				minSOC:        25.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.45,
				loadKWH:       1.0,
				solarKWH:      0.0,
				minSOC:        25.0,
			},
		}

		pathSurplus, err := c.searchOptimalPlan(ctx, surplusSolarTimeline, stateAt5, anchors, reserveSettings, statusAt5, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, pathSurplus.actions)
		// Battery charges from solar (SOC increases) without needing forced grid charge
		assert.Greater(t, pathSurplus.states[1].soc, pathSurplus.states[0].soc, "battery must charge back toward reserve via surplus solar")
	})

	t.Run("ReserveInvariant_BatteryChargesWhenBelowReserve", func(t *testing.T) {
		t.Parallel()

		// Verify invariant: whenever state.soc < minSOC - 0.1 and GridChargeBatteries is true,
		// the battery must be charging in some capacity (either BatteryModeChargeAny from grid, or charging via surplus solar).
		// It must never idle in Standby or discharge when below reserve.
		t.Run("PeakNoSolar_ForcesGridCharge", func(t *testing.T) {
			t.Parallel()
			peakTimeline := []planInterval{
				{index: 0, startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.50, loadKWH: 2.0, solarKWH: 0.0, minSOC: 20.0},
				{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 1.0, solarKWH: 0.0, minSOC: 20.0},
			}
			st := planState{
				energyKWH:      13.5 * 0.05,
				soc:            5.0,
				capacityKWH:    13.5,
				maxDischargeKW: 5.0,
				maxChargeKW:    5.0,
			}
			sysStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            5.0,
				MaxBatteryDischargeKW: 5.0,
				MaxBatteryChargeKW:    5.0,
			}
			sett := types.Settings{
				MinBatterySOC:       20.0,
				GridChargeBatteries: true,
			}

			anch := c.detectPlanningAnchors(peakTimeline, nil, sysStatus, sett, nil)
			resPath, err := c.searchOptimalPlan(ctx, peakTimeline, st, anch, sett, sysStatus, nil, nil)
			require.NoError(t, err)

			for i := 0; i < len(resPath.actions); i++ {
				soc := resPath.states[i].soc
				minSOC := peakTimeline[i].minSOC
				if soc < minSOC-0.1 {
					assert.Equal(t, types.BatteryModeChargeAny, resPath.actions[i].batteryMode,
						"step %d must grid charge when below reserve with no solar", i)
				}
			}
		})

		t.Run("SolarSurplus_ChargesViaSolarWithoutGridImport", func(t *testing.T) {
			t.Parallel()
			solarTimeline := []planInterval{
				{index: 0, startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.20, loadKWH: 1.0, solarKWH: 5.0, minSOC: 30.0},
				{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.20, loadKWH: 1.0, solarKWH: 0.0, minSOC: 30.0},
			}
			st := planState{
				energyKWH:      13.5 * 0.15,
				soc:            15.0,
				capacityKWH:    13.5,
				maxDischargeKW: 5.0,
				maxChargeKW:    5.0,
			}
			sysStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            15.0,
				MaxBatteryDischargeKW: 5.0,
				MaxBatteryChargeKW:    5.0,
			}
			sett := types.Settings{
				MinBatterySOC:       30.0,
				GridChargeBatteries: true,
			}

			anch := c.detectPlanningAnchors(solarTimeline, nil, sysStatus, sett, nil)
			resPath, err := c.searchOptimalPlan(ctx, solarTimeline, st, anch, sett, sysStatus, nil, nil)
			require.NoError(t, err)

			for i := 0; i < len(resPath.actions); i++ {
				soc := resPath.states[i].soc
				minSOC := solarTimeline[i].minSOC
				if soc < minSOC-0.1 {
					isGridCharging := resPath.actions[i].batteryMode == types.BatteryModeChargeAny
					isSolarCharging := solarTimeline[i].solarKWH > solarTimeline[i].loadKWH && resPath.states[i+1].energyKWH > resPath.states[i].energyKWH
					assert.True(t, isGridCharging || isSolarCharging,
						"step %d must charge in some capacity back toward reserve", i)
					assert.NotEqual(t, types.BatteryModeStandby, resPath.actions[i].batteryMode,
						"step %d must not idle in standby below reserve", i)
				}
			}
		})

		t.Run("DirectSolarExport_PenalizedDuringReserveDeficit", func(t *testing.T) {
			t.Parallel()
			// High export rate tempting direct solar export, but battery is below reserve
			exportTimeline := []planInterval{
				{index: 0, startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.15, exportRate: 0.30, loadKWH: 1.0, solarKWH: 5.0, minSOC: 30.0},
				{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.15, exportRate: 0.05, loadKWH: 1.0, solarKWH: 0.0, minSOC: 30.0},
			}
			st := planState{
				energyKWH:      13.5 * 0.15,
				soc:            15.0,
				capacityKWH:    13.5,
				maxDischargeKW: 5.0,
				maxChargeKW:    5.0,
			}
			sysStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            15.0,
				MaxBatteryDischargeKW: 5.0,
				MaxBatteryChargeKW:    5.0,
				SolarKW:               5.0,
			}
			sett := types.Settings{
				MinBatterySOC:       30.0,
				GridChargeBatteries: true,
				GridExportSolar:     true,
				ManageTOUSchedules:  true,
			}

			anch := c.detectPlanningAnchors(exportTimeline, nil, sysStatus, sett, nil)
			resPath, err := c.searchOptimalPlan(ctx, exportTimeline, st, anch, sett, sysStatus, nil, nil)
			require.NoError(t, err)

			// Step 0 must NOT select SolarModeExport because that bypasses charging the battery back to reserve
			assert.NotEqual(t, types.SolarModeExport, resPath.actions[0].solarMode,
				"must not direct export solar while in reserve deficit")
			// Battery should charge toward reserve
			assert.Greater(t, resPath.states[1].soc, resPath.states[0].soc,
				"battery must recharge toward reserve instead of bypassing via export")
		})

		t.Run("GridChargingDisabled_NoDeficitPenaltyWhenImpossibleToCharge", func(t *testing.T) {
			t.Parallel()
			// Nighttime with no solar and grid charging disabled: battery is below reserve
			nightTimeline := []planInterval{
				{index: 0, startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.15, loadKWH: 1.0, solarKWH: 0.0, minSOC: 30.0},
				{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.15, loadKWH: 1.0, solarKWH: 0.0, minSOC: 30.0},
			}
			st := planState{
				energyKWH:      13.5 * 0.15,
				soc:            15.0,
				capacityKWH:    13.5,
				maxDischargeKW: 5.0,
				maxChargeKW:    5.0,
			}
			sysStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            15.0,
				MaxBatteryDischargeKW: 5.0,
				MaxBatteryChargeKW:    5.0,
			}
			sett := types.Settings{
				MinBatterySOC:       30.0,
				GridChargeBatteries: false, // Grid charging disabled
			}

			anch := c.detectPlanningAnchors(nightTimeline, nil, sysStatus, sett, nil)
			resPath, err := c.searchOptimalPlan(ctx, nightTimeline, st, anch, sett, sysStatus, nil, nil)
			require.NoError(t, err)
			require.NotEmpty(t, resPath.actions)
			// Planner successfully produces a viable plan without error
			assert.Equal(t, types.BatteryModeLoad, resPath.actions[0].batteryMode)
		})
	})

	t.Run("AntiChurn_PreventsReentryIntoChargeAnyDuringContinuousFlatPrice", func(t *testing.T) {
		t.Parallel()

		// 4 consecutive flat off-peak intervals followed by expensive peak intervals
		flatPriceTimeline := []planInterval{
			{index: 0, startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 0.333, importRate: 0.05, loadKWH: 0.2, minSOC: 20},
			{index: 1, startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 0.333, importRate: 0.05, loadKWH: 0.2, minSOC: 20},
			{index: 2, startTime: now.Add(40 * time.Minute), endTime: now.Add(60 * time.Minute), durationHours: 0.333, importRate: 0.05, loadKWH: 0.2, minSOC: 20},
			{index: 3, startTime: now.Add(60 * time.Minute), endTime: now.Add(80 * time.Minute), durationHours: 0.333, importRate: 0.05, loadKWH: 0.2, minSOC: 20},
			{index: 4, startTime: now.Add(80 * time.Minute), endTime: now.Add(100 * time.Minute), durationHours: 0.333, importRate: 0.50, loadKWH: 1.5, minSOC: 20},
			{index: 5, startTime: now.Add(100 * time.Minute), endTime: now.Add(120 * time.Minute), durationHours: 0.333, importRate: 0.50, loadKWH: 1.5, minSOC: 20},
		}

		st := planState{
			capacityKWH: 13.5,
			energyKWH:   13.5 * 0.30,
			soc:         30.0,
		}
		sysStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            30.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}
		sett := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}

		anch := c.detectPlanningAnchors(flatPriceTimeline, nil, sysStatus, sett, nil)
		planPath, err := c.searchOptimalPlan(ctx, flatPriceTimeline, st, anch, sett, sysStatus, nil, nil)
		require.NoError(t, err)
		require.NotEmpty(t, planPath.actions)

		// Verify invariant: during the contiguous flat-rate off-peak window (steps 0..3),
		// once ChargeAny is exited, it never re-enters ChargeAny (no Charge -> Load -> Charge flutter).
		hasExitedCharge := false
		for i := 0; i <= 3; i++ {
			act := planPath.actions[i]
			if act.batteryMode == types.BatteryModeChargeAny {
				assert.False(t, hasExitedCharge, "Step %d re-entered ChargeAny after exiting charging in the same flat price block", i)
			} else {
				hasExitedCharge = true
			}
		}
	})

	t.Run("AntiChurn_PreventsReentryFromLastAction", func(t *testing.T) {
		t.Parallel()

		// Flat price timeline
		flatPriceTimeline := []planInterval{
			{index: 0, startTime: now, endTime: now.Add(20 * time.Minute), durationHours: 0.333, importRate: 0.05, exportRate: 0.02, loadKWH: 0.2, minSOC: 20},
			{index: 1, startTime: now.Add(20 * time.Minute), endTime: now.Add(40 * time.Minute), durationHours: 0.333, importRate: 0.05, exportRate: 0.02, loadKWH: 0.2, minSOC: 20},
			{index: 2, startTime: now.Add(40 * time.Minute), endTime: now.Add(60 * time.Minute), durationHours: 0.333, importRate: 0.50, exportRate: 0.02, loadKWH: 1.5, minSOC: 20},
		}

		st := planState{
			capacityKWH: 13.5,
			energyKWH:   13.5 * 0.90, // Almost full
			soc:         90.0,
		}
		sysStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            90.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}
		sett := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}

		// lastAction was ChargeAny in the same price block
		lastAct := &types.Action{
			BatteryMode: types.BatteryModeChargeAny,
			CurrentPrice: &types.Price{
				DollarsPerKWH: 0.05,
			},
		}

		anch := c.detectPlanningAnchors(flatPriceTimeline, nil, sysStatus, sett, nil)
		planPath, err := c.searchOptimalPlan(ctx, flatPriceTimeline, st, anch, sett, sysStatus, nil, lastAct)
		require.NoError(t, err)
		require.NotEmpty(t, planPath.actions)

		// If step 0 switched to Load, step 1 must not re-enter ChargeAny in the same flat price block
		if planPath.actions[0].batteryMode != types.BatteryModeChargeAny {
			assert.NotEqual(t, types.BatteryModeChargeAny, planPath.actions[1].batteryMode,
				"Step 1 must not re-enter ChargeAny after step 0 exited ChargeAny from lastAction in the same flat price block")
		}
	})

	t.Run("RefinesOverchargedEpisode_ClampsTargetSOCAndConvertsSamePriceLoadToStandby", func(t *testing.T) {
		t.Parallel()

		start := time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC)
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}

		sett := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}

		// Initial state: 15 kWh battery starting at 40% SOC (6.0 kWh)
		s0 := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		a0 := actionCandidate{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0}
		s1, m0 := stepPhysics(s0, a0, timeline[0], sett, 0.90)

		a1 := actionCandidate{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0}
		s2, m1 := stepPhysics(s1, a1, timeline[1], sett, 0.90)

		a2 := actionCandidate{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0}
		s3, m2 := stepPhysics(s2, a2, timeline[2], sett, 0.90)

		p := &planPath{
			actions:   []actionCandidate{a0, a1, a2},
			states:    []planState{s0, s1, s2, s3},
			metrics:   []intervalMetrics{m0, m1, m2},
			timeline:  timeline,
			totalCost: m0.costDollars + m1.costDollars + m2.costDollars,
			bestScore: m0.costDollars + m1.costDollars + m2.costDollars,
		}

		originalCost := p.totalCost
		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted, "Overcharged episode must be refined")
		assert.InDelta(t, 76.50, refinedPath.actions[0].targetSOC, 0.5, "Target SOC must be clamped to exit SOC (~76.5%)")
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode, "Same-rate discharge interval must be converted to Standby")
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, refinedPath.actions[1].reason)
		assert.Equal(t, 0.0, refinedPath.actions[1].targetSOC)
		assert.InDelta(t, 76.50, refinedPath.states[1].soc, 0.5, "Ending SOC of charge interval must reflect clamped targetSOC")
		assert.InDelta(t, 76.50, refinedPath.states[2].soc, 0.5, "Standby interval must maintain SOC")
		assert.Greater(t, originalCost, refinedPath.totalCost, "Refined plan must save money by avoiding round-trip conversion loss")
		assert.Equal(t, 0.0, p.actions[0].targetSOC, "Original plan must remain unmutated")
	})

	t.Run("Step0_Inertia_DurationScaledThreshold", func(t *testing.T) {
		t.Parallel()

		timeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.10,
				minSOC:        20.0,
				loadKWH:       0.333,
			},
			{
				index:         1,
				startTime:     now.Add(20 * time.Minute),
				endTime:       now.Add(80 * time.Minute),
				durationHours: 1.0,
				importRate:    0.116,
				minSOC:        20.0,
				loadKWH:       1.0,
			},
		}

		state := planState{
			energyKWH:      3.0,
			soc:            30.0,
			capacityKWH:    10.0,
			maxDischargeKW: 5.0,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 10.0,
			BatterySOC:         30.0,
		}

		sett := types.Settings{
			MinBatterySOC:   20.0,
			GridExportSolar: true,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
			SolarMode:   types.SolarModeAny,
			Timestamp:   now.Add(-10 * time.Minute),
		}

		bestPath, err := c.searchOptimalPlan(ctx, timeline, state, planningAnchors{}, sett, status, nil, lastAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, bestPath.actions[0].batteryMode)
	})

	t.Run("Step0_Inertia_SolarModeSwitch", func(t *testing.T) {
		t.Parallel()

		timeline := []planInterval{
			{
				startTime:     now,
				endTime:       now.Add(20 * time.Minute),
				durationHours: 20.0 / 60.0,
				importRate:    0.10,
				exportRate:    0.25,
				minSOC:        20.0,
				loadKWH:       0.3,
				solarKWH:      0.5,
			},
		}

		state := planState{
			energyKWH:   10.8,
			soc:         80.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		status := types.SystemStatus{
			Timestamp:          now,
			BatteryCapacityKWH: 13.5,
			BatterySOC:         80.0,
			SolarKW:            1.5,
		}

		sett := types.Settings{
			MinBatterySOC:      20.0,
			GridExportSolar:    true,
			ManageTOUSchedules: true,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
			SolarMode:   types.SolarModeExport,
			Timestamp:   now.Add(-10 * time.Minute),
		}

		bestPath, err := c.searchOptimalPlan(ctx, timeline, state, planningAnchors{}, sett, status, nil, lastAction)
		require.NoError(t, err)
		assert.Equal(t, types.SolarModeExport, bestPath.actions[0].solarMode)
	})

	t.Run("SolarExport_Continuation_SameOrHigherPeakPriceWindow", func(t *testing.T) {
		t.Parallel()

		sett := types.Settings{
			MinBatterySOC:      20.0,
			GridExportSolar:    true,
			ManageTOUSchedules: true,
		}

		state := planState{
			energyKWH:   10.8,
			soc:         80.0,
			capacityKWH: 13.5,
			maxChargeKW: 5.0,
		}

		t.Run("ForwardPlan_ContinuesAcrossSamePeakWindowAndStopsWhenPriceDrops", func(t *testing.T) {
			t.Parallel()

			timeline := []planInterval{
				{
					startTime:     now,
					endTime:       now.Add(time.Hour),
					durationHours: 1.0,
					importRate:    0.35,
					exportRate:    0.25,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      1.5,
				},
				{
					startTime:     now.Add(time.Hour),
					endTime:       now.Add(2 * time.Hour),
					durationHours: 1.0,
					importRate:    0.35,
					exportRate:    0.25,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      0.0, // Forecasted solar ended
				},
				{
					startTime:     now.Add(2 * time.Hour),
					endTime:       now.Add(3 * time.Hour),
					durationHours: 1.0,
					importRate:    0.10, // Off-peak drop
					exportRate:    0.05,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      0.0,
				},
			}

			status := types.SystemStatus{
				Timestamp:          now,
				BatteryCapacityKWH: 13.5,
				BatterySOC:         80.0,
				SolarKW:            1.5,
			}

			bestPath, err := c.searchOptimalPlan(ctx, timeline, state, planningAnchors{}, sett, status, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 3)

			assert.Equal(t, types.SolarModeExport, bestPath.actions[0].solarMode, "Step 0 with active solar should export solar")
			assert.Equal(t, types.SolarModeExport, bestPath.actions[1].solarMode, "Step 1 with zero solar but same peak rate should continue solar export")
			assert.NotEqual(t, types.SolarModeExport, bestPath.actions[2].solarMode, "Step 2 after price drops to off-peak should terminate solar export")
		})

		t.Run("ForwardPlan_ContinuesWhenPeakPriceGoesHigher", func(t *testing.T) {
			t.Parallel()

			timeline := []planInterval{
				{
					startTime:     now,
					endTime:       now.Add(time.Hour),
					durationHours: 1.0,
					importRate:    0.30,
					exportRate:    0.20,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      1.5,
				},
				{
					startTime:     now.Add(time.Hour),
					endTime:       now.Add(2 * time.Hour),
					durationHours: 1.0,
					importRate:    0.45, // Critical peak higher rate!
					exportRate:    0.35, // Higher export rate!
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      0.0,
				},
				{
					startTime:     now.Add(2 * time.Hour),
					endTime:       now.Add(3 * time.Hour),
					durationHours: 1.0,
					importRate:    0.10,
					exportRate:    0.05,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      0.0,
				},
			}

			status := types.SystemStatus{
				Timestamp:          now,
				BatteryCapacityKWH: 13.5,
				BatterySOC:         80.0,
				SolarKW:            1.5,
			}

			bestPath, err := c.searchOptimalPlan(ctx, timeline, state, planningAnchors{}, sett, status, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 3)

			assert.Equal(t, types.SolarModeExport, bestPath.actions[0].solarMode, "Step 0 with active solar should export solar")
			assert.Equal(t, types.SolarModeExport, bestPath.actions[1].solarMode, "Step 1 where peak price goes higher should continue solar export")
			assert.NotEqual(t, types.SolarModeExport, bestPath.actions[2].solarMode, "Step 2 after price drops to off-peak should terminate solar export")
		})

		t.Run("RealTime_Step0_DoesNotSwitchAwayWhenSolarEndsDuringPeak", func(t *testing.T) {
			t.Parallel()

			timeline := []planInterval{
				{
					startTime:     now,
					endTime:       now.Add(time.Hour),
					durationHours: 1.0,
					importRate:    0.35,
					exportRate:    0.25,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      0.0, // Solar ended
				},
				{
					startTime:     now.Add(time.Hour),
					endTime:       now.Add(2 * time.Hour),
					durationHours: 1.0,
					importRate:    0.10, // Off-peak drop
					exportRate:    0.05,
					minSOC:        20.0,
					loadKWH:       0.5,
					solarKWH:      0.0,
				},
			}

			status := types.SystemStatus{
				Timestamp:          now,
				BatteryCapacityKWH: 13.5,
				BatterySOC:         80.0,
				SolarKW:            0.0, // Zero real-time solar
			}

			lastPrice := types.Price{
				DollarsPerKWH:                 0.25,
				GridUseDollarsPerKWH:          0.10, // Total import = 0.35
				GenerationCreditDollarsPerKWH: 0.25,
				SeparateGenerationCredit:      true, // Total export = 0.25
			}

			lastAction := &types.Action{
				BatteryMode:  types.BatteryModeLoad,
				SolarMode:    types.SolarModeExport,
				Timestamp:    now.Add(-15 * time.Minute),
				CurrentPrice: &lastPrice,
			}

			bestPath, err := c.searchOptimalPlan(ctx, timeline, state, planningAnchors{}, sett, status, nil, lastAction)
			require.NoError(t, err)
			require.NotEmpty(t, bestPath.actions)

			assert.Equal(t, types.BatteryModeLoad, bestPath.actions[0].batteryMode)
			assert.Equal(t, types.SolarModeExport, bestPath.actions[0].solarMode, "Step 0 in real-time must not switch away from SolarModeExport during same peak price")
		})

		t.Run("ContinuesWithLowAfternoonSolarAcross24HourHorizonWithMorningSolarRefill", func(t *testing.T) {
			t.Parallel()

			// 72-step (24-hour) timeline starting at 16:00 during a 12:00-20:00 peak ($0.30 import / $0.295 export),
			// followed by 20:00-12:00 off-peak ($0.208 import / $0.203 export) where the battery depletes to 5% reserve
			// overnight and recharges from morning solar (08:00-12:00) ahead of the next day's 12:00-16:00 peak.
			start := time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)
			var timeline []planInterval
			for i := 0; i < 72; i++ {
				ts := start.Add(time.Duration(i*20) * time.Minute)
				hour := ts.Hour()
				isPeak := hour >= 12 && hour < 20
				impRate := 0.20754
				expRate := 0.20254
				if isPeak {
					impRate = 0.29982
					expRate = 0.29482
				}
				loadKWH := 0.20
				solarKWH := 0.0
				if i == 0 {
					solarKWH = 0.098 // ~0.295 kW (< minDirectSolarExportKW)
					loadKWH = 0.42
				} else if i == 1 || i == 2 {
					solarKWH = 0.066
					loadKWH = 0.22
				} else if hour >= 8 && hour < 12 {
					solarKWH = 0.68 // Morning solar refill (+5.4 kWh net over 4 hours > 2.4 kWh afternoon peak load)
				}
				timeline = append(timeline, planInterval{
					index:         i,
					startTime:     ts,
					endTime:       ts.Add(20 * time.Minute),
					durationHours: 1.0 / 3.0,
					importRate:    impRate,
					exportRate:    expRate,
					minSOC:        5.0,
					loadKWH:       loadKWH,
					solarKWH:      solarKWH,
				})
			}

			capKWH := 28.5
			initSOC := 22.0
			state24 := planState{
				soc:            initSOC,
				energyKWH:      capKWH * (initSOC / 100.0),
				capacityKWH:    capKWH,
				maxChargeKW:    10.0,
				maxDischargeKW: 10.0,
				time:           start,
			}
			status24 := types.SystemStatus{
				Timestamp:             start,
				BatteryCapacityKWH:    capKWH,
				BatterySOC:            initSOC,
				BatteryAboveMinSOC:    true,
				SolarKW:               0.295,
				MaxBatteryChargeKW:    10.0,
				MaxBatteryDischargeKW: 10.0,
			}
			sett24 := types.Settings{
				ManageTOUSchedules:  true,
				GridExportSolar:     true,
				GridChargeBatteries: true,
				MinBatterySOC:       5.0,
			}
			lastPrice := types.Price{
				DollarsPerKWH:                 0.29982,
				GenerationCreditDollarsPerKWH: 0.29482,
				SeparateGenerationCredit:      true,
			}
			lastAction := &types.Action{
				BatteryMode:  types.BatteryModeLoad,
				SolarMode:    types.SolarModeExport,
				Timestamp:    start.Add(-20 * time.Minute),
				CurrentPrice: &lastPrice,
			}
			anchors24 := planningAnchors{
				knownPostHorizonRate:       0.28,
				postHorizonSolarSurplusKWH: 0.0,
				postHorizonSlices: []postHorizonLoadSlice{
					{importRate: 0.29982, loadKWH: 2.5},
				},
			}

			bestPath, err := c.searchOptimalPlan(ctx, timeline, state24, anchors24, sett24, status24, nil, lastAction)
			require.NoError(t, err)
			require.NotEmpty(t, bestPath.actions)
			assert.Equal(t, types.BatteryModeLoad, bestPath.actions[0].batteryMode)
			assert.Equal(t, types.SolarModeExport, bestPath.actions[0].solarMode, "Should stay in direct solar export when afternoon trickle solar exports at peak rate and replaces energy at overnight off-peak rate")
		})

		t.Run("PeakSurvivalBufferPenalty", func(t *testing.T) {
			t.Parallel()

			t0 := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
			// 10:00-14:00 Off-peak ($0.10/kWh, load = 0.5 kW), 14:00-18:00 Peak ($0.50/kWh, load = 2.0 kW), 18:00-22:00 Off-peak ($0.10/kWh)
			timeline := []planInterval{
				{index: 0, startTime: t0, endTime: t0.Add(4 * time.Hour), durationHours: 4.0, importRate: 0.10, loadKWH: 2.0},
				{index: 1, startTime: t0.Add(4 * time.Hour), endTime: t0.Add(8 * time.Hour), durationHours: 4.0, importRate: 0.50, loadKWH: 8.0},
				{index: 2, startTime: t0.Add(8 * time.Hour), endTime: t0.Add(12 * time.Hour), durationHours: 4.0, importRate: 0.10, loadKWH: 2.0},
			}

			sett := types.Settings{
				GridChargeBatteries: true,
				MinBatterySOC:       20.0,
				OptimizationProfile: "conservative", // buffer = 60 mins (2.0 kWh)
			}

			status := types.SystemStatus{
				Timestamp:             t0,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            30.0, // Low initial SOC (4.05 kWh)
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}

			state := planState{
				soc:         30.0,
				energyKWH:   4.05,
				capacityKWH: 13.5,
				time:        t0,
			}

			anchors := c.detectPlanningAnchors(timeline, nil, status, sett, nil)
			require.Len(t, anchors.peakWindows, 1)

			bestPath, err := c.searchOptimalPlan(ctx, timeline, state, anchors, sett, status, nil, nil)
			require.NoError(t, err)
			require.NotEmpty(t, bestPath.actions)

			// With conservative profile (60 min buffer), off-peak pre-charging must be scheduled at step 0
			// to ensure the battery enters peak with enough energy to cover peak load (8.0 kWh) + buffer (2.0 kWh).
			assert.Equal(t, types.BatteryModeChargeAny, bestPath.actions[0].batteryMode, "Expected off-peak pre-charge to satisfy peak survival buffer")
		})
	})

	t.Run("SingleCountedDegradation_BatteryExportArbitrageClearsHurdleOnce", func(t *testing.T) {
		t.Parallel()

		// Off-peak import: $0.05/kWh (round-trip recharge cost at 90% eff = $0.0556/kWh).
		// Peak export: $0.20/kWh.
		// Net physical spread after round-trip loss: $0.20 - $0.0556 = $0.1444/kWh.
		// With balanced OptimizationParams (GridChargeDegradation = $0.02 on grid charge, BatteryExportDegradation = $0.03 full-cycle):
		// Total full-cycle wear hurdle = $0.02 + $0.01 = $0.03/kWh (< $0.1444/kWh -> profitable!).
		arbTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				exportRate:    0.02,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.20,
				exportRate:    0.20,
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				exportRate:    0.02,
				minSOC:        20.0,
			},
		}

		arbSettings := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
		}

		arbStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            20.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		arbState := planState{
			energyKWH:      2.7,
			soc:            20.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}

		anchors := c.detectPlanningAnchors(arbTimeline, nil, arbStatus, arbSettings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, arbTimeline, arbState, anchors, arbSettings, arbStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPath.actions, 3)

		assert.Equal(t, types.BatteryModeChargeAny, bestPath.actions[0].batteryMode, "Must pre-charge when net spread clears the degradation hurdle")
		assert.Equal(t, types.BatteryModeExport, bestPath.actions[1].batteryMode, "Must export stored energy during the profitable export window")
	})

	t.Run("NegativePriceGridCharge_BalancesWearAgainstTotalGain", func(t *testing.T) {
		t.Parallel()

		negSettings := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}

		negStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            40.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		negState := planState{
			energyKWH:      5.4,
			soc:            40.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}

		// Case 1: Slightly negative price (-$0.005/kWh) followed by normal retail load ($0.08/kWh).
		// Total gain per kWh charged = $0.005 + 0.90*$0.08 = $0.077/kWh > $0.02/kWh wear cost -> Charges!
		normalLoadTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    -0.005,
				exportRate:    0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				exportRate:    0.0,
				loadKWH:       5.0,
				minSOC:        20.0,
			},
		}

		anchors := c.detectPlanningAnchors(normalLoadTimeline, nil, negStatus, negSettings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, normalLoadTimeline, negState, anchors, negSettings, negStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPath.actions, 2)
		assert.Equal(t, types.BatteryModeChargeAny, bestPath.actions[0].batteryMode, "Must charge during slightly negative prices when stored energy offsets normal retail load")

		// Case 2: Slightly negative price (-$0.005/kWh) when free solar will fill the battery anyway
		// and solar export is disabled (so grid charging only earns $0.005/kWh and curtails free solar).
		// Total gain ($0.005/kWh) < $0.02/kWh wear cost -> Stays in Standby and lets free solar fill the battery!
		solarRefillTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    -0.005,
				exportRate:    0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 2.0,
				importRate:    0.08,
				exportRate:    0.0,
				solarKWH:      12.0,
				minSOC:        20.0,
			},
		}

		anchorsSolar := c.detectPlanningAnchors(solarRefillTimeline, nil, negStatus, negSettings, nil)
		bestPathSolar, err := c.searchOptimalPlan(ctx, solarRefillTimeline, negState, anchorsSolar, negSettings, negStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPathSolar.actions, 2)
		assert.Equal(t, types.BatteryModeStandby, bestPathSolar.actions[0].batteryMode, "Must stay in Standby when negative price is tiny (-$0.005/kWh) and free solar will fill the battery anyway")
	})

	t.Run("GridChargeWearHurdle_PreventsMarginalGridCycling", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// - Battery starts at 60% SOC (8.1 kWh) with 20% reserve (2.7 kWh), so 5.4 kWh usable (~5.12 kWh delivered).
		// - Step 0 ($0.05/kWh): Off-peak.
		// - Step 1 ($0.065/kWh): Marginal shoulder with 3.0 kWh load.
		// - Step 2 ($0.25/kWh): True peak with 4.0 kWh load.
		// - Step 3 ($0.05/kWh): Post-peak off-peak.
		// Because total future load (7.0 kWh) exceeds usable battery (~5.12 kWh) and Step 2 ($0.25) clears
		// minDeficitPriceSpread, candidate generation offers ChargeAny at Step 0.
		// However, existing battery energy already covers the entire Step 2 ($0.25) peak (4.0 kWh).
		// Any additional grid charging at Step 0 ($0.05/kWh) would only displace Step 1 ($0.065/kWh) shoulder imports.
		// Without gridChargeHurdle, 1 kWh grid charge ($0.05) saving 0.90 * $0.065 = $0.0585 would look profitable.
		// Under balanced profile (90% RTE, $0.02/kWh gridChargeHurdle), 1 kWh grid charge costs
		// $0.05 import + $0.02 wear = $0.070/kWh > $0.0585/kWh, so the DP solver rejects grid charging at Step 0.
		shoulderAndPeakTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.065,
				exportRate:    0.02,
				loadKWH:       3.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(3 * time.Hour),
				durationHours: 1.0,
				importRate:    0.25,
				exportRate:    0.02,
				loadKWH:       4.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         3,
				startTime:     now.Add(3 * time.Hour),
				endTime:       now.Add(4 * time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
		}

		balSettings := types.Settings{
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
			OptimizationProfile: "balanced",
		}

		arbStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            60.0,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		arbState := planState{
			energyKWH:      8.1,
			soc:            60.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}

		anchors := c.detectPlanningAnchors(shoulderAndPeakTimeline, nil, arbStatus, balSettings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, shoulderAndPeakTimeline, arbState, anchors, balSettings, arbStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPath.actions, 4)

		assert.NotEqual(t, types.BatteryModeChargeAny, bestPath.actions[0].batteryMode,
			"Grid charge wear hurdle must reject grid pre-charging at $0.05/kWh when it only displaces marginal $0.065/kWh shoulder load")
	})

	t.Run("IntraBucketTieBreakingAndBoundaryIsolation", func(t *testing.T) {
		t.Parallel()

		t.Run("EqualCostDifferentSOC_PrefersHigherSOCDespiteHoldingCost", func(t *testing.T) {
			t.Parallel()

			// At Step 1 ($0.00/kWh free window with 0.045 kWh load), Branch A (Standby) preserves 50.20% SOC
			// while Branch B (Load) drains 0.35% SOC to 49.85% SOC; both have $0.00 cash/wear/transition cost.
			// At Step 2 ($0.15/kWh with 0 load), both transition to BatteryModeLoad (mIdx=0) and round to the
			// same 0.5% bucket (b=100). Branch A has a ~9.5e-6 higher holding cost from carrying +0.047 kWh,
			// which is within planScoreTieEpsilon (1e-4). The tie-breaker must retain Branch A's higher SOC
			// (50.20% > 49.85%) so the extra energy offsets the Step 3 ($0.50/kWh) peak.
			tieTimeline := []planInterval{
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
					importRate:    0.00,
					loadKWH:       0.045,
					minSOC:        20.0,
				},
				{
					index:         2,
					startTime:     now.Add(2 * time.Hour),
					endTime:       now.Add(3 * time.Hour),
					durationHours: 1.0,
					importRate:    0.15,
					loadKWH:       0.0,
					minSOC:        20.0,
				},
				{
					index:         3,
					startTime:     now.Add(3 * time.Hour),
					endTime:       now.Add(4 * time.Hour),
					durationHours: 1.0,
					importRate:    0.50,
					loadKWH:       5.0,
					minSOC:        20.0,
				},
			}

			tieSettings := types.Settings{
				MinBatterySOC:       20.0,
				GridChargeBatteries: false,
			}
			tieStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            50.20,
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}
			tieState := planState{
				energyKWH:      13.5 * 0.5020,
				soc:            50.20,
				capacityKWH:    13.5,
				maxChargeKW:    5.0,
				maxDischargeKW: 5.0,
				time:           now,
			}

			anchors := c.detectPlanningAnchors(tieTimeline, nil, tieStatus, tieSettings, nil)
			bestPath, err := c.searchOptimalPlan(ctx, tieTimeline, tieState, anchors, tieSettings, tieStatus, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 4)

			assert.Equal(t, types.BatteryModeStandby, bestPath.actions[1].batteryMode,
				"Intra-bucket tie-breaker must keep the higher-SOC trajectory (50.20% vs 49.85%) when cash costs tie within planScoreTieEpsilon")
			assert.InDelta(t, 50.20, bestPath.states[2].soc, 0.01)
		})

		t.Run("BoundaryBucket200_FullCapacityNotOverwrittenByNearFull", func(t *testing.T) {
			t.Parallel()

			// Branch A holds at Step 1 ($0.05/kWh, 0.285 kWh load) and charges to a clamped 100.00% SOC at Step 2 ($0.04/kWh).
			// Branch B discharges at Step 1 and charges at Step 2 to 99.83% SOC.
			// At Step 2, both are in BatteryModeChargeAny (mIdx=2), and Branch B has a ~$0.0025 lower accumulated cost
			// because it charged 0.17% less energy. Because bucket 200 is reserved strictly for full capacity (soc >= 100%),
			// Branch B (99.83%) is placed in bucket 199 and does not overwrite Branch A (100.00%) ahead of the Step 3 ($0.50/kWh) peak.
			fullBoundaryTimeline := []planInterval{
				{
					index:         0,
					startTime:     now,
					endTime:       now.Add(time.Hour),
					durationHours: 1.0,
					importRate:    0.08,
					loadKWH:       0.27,
					minSOC:        20.0,
				},
				{
					index:         1,
					startTime:     now.Add(time.Hour),
					endTime:       now.Add(2 * time.Hour),
					durationHours: 1.0,
					importRate:    0.05,
					loadKWH:       0.285,
					minSOC:        20.0,
				},
				{
					index:         2,
					startTime:     now.Add(2 * time.Hour),
					endTime:       now.Add(3*time.Hour + 12*time.Minute),
					durationHours: 1.2,
					importRate:    0.04,
					loadKWH:       0.0,
					minSOC:        20.0,
				},
				{
					index:         3,
					startTime:     now.Add(3*time.Hour + 12*time.Minute),
					endTime:       now.Add(5*time.Hour + 12*time.Minute),
					durationHours: 2.0,
					importRate:    0.50,
					loadKWH:       11.0,
					minSOC:        20.0,
				},
			}

			fullSettings := types.Settings{
				MinBatterySOC:       20.0,
				GridChargeBatteries: true,
			}
			fullStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            62.0,
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}
			fullState := planState{
				energyKWH:      13.5 * 0.62,
				soc:            62.0,
				capacityKWH:    13.5,
				maxChargeKW:    5.0,
				maxDischargeKW: 5.0,
				time:           now,
			}

			anchors := c.detectPlanningAnchors(fullBoundaryTimeline, nil, fullStatus, fullSettings, nil)
			bestPath, err := c.searchOptimalPlan(ctx, fullBoundaryTimeline, fullState, anchors, fullSettings, fullStatus, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 4)

			assert.InDelta(t, 100.0, bestPath.states[3].soc, 1e-4,
				"Full 100.0% SOC trajectory in bucket 200 must not be overwritten by a cheaper 99.83% trajectory ahead of a high peak")
		})

		t.Run("BoundaryMinBucket_AboveReserveNotMergedWithClampedReserve", func(t *testing.T) {
			t.Parallel()

			// With minSOC = 20.0% (minBucket = 40), Branch A (Standby at Step 1 $0.15, Load at Step 2 $0.16)
			// arrives at Step 2 with 20.175% SOC (0.0236 kWh above reserve) and cumulative cost $0.1040,
			// whereas Branch B (Load at Step 1 and Step 2) clamps at 20.000% SOC with cumulative cost $0.1018.
			// Because minBucket (40) is reserved for soc <= minSOC, Branch A (20.175%) is placed in bucket 41
			// and survives to discharge its remaining energy during the Step 3 ($0.80/kWh) critical peak.
			reserveBoundaryTimeline := []planInterval{
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
					importRate:    0.15,
					loadKWH:       0.66,
					minSOC:        20.0,
				},
				{
					index:         2,
					startTime:     now.Add(2 * time.Hour),
					endTime:       now.Add(3 * time.Hour),
					durationHours: 1.0,
					importRate:    0.16,
					loadKWH:       0.618,
					minSOC:        20.0,
				},
				{
					index:         3,
					startTime:     now.Add(3 * time.Hour),
					endTime:       now.Add(4 * time.Hour),
					durationHours: 1.0,
					importRate:    0.80,
					loadKWH:       1.0,
					minSOC:        20.0,
				},
			}

			resSettings := types.Settings{
				MinBatterySOC:       20.0,
				GridChargeBatteries: false,
			}
			resStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            25.0,
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}
			resState := planState{
				energyKWH:      13.5 * 0.25,
				soc:            25.0,
				capacityKWH:    13.5,
				maxChargeKW:    5.0,
				maxDischargeKW: 5.0,
				time:           now,
			}

			anchors := c.detectPlanningAnchors(reserveBoundaryTimeline, nil, resStatus, resSettings, nil)
			bestPath, err := c.searchOptimalPlan(ctx, reserveBoundaryTimeline, resState, anchors, resSettings, resStatus, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 4)

			assert.Greater(t, bestPath.states[3].soc, 20.10,
				"Unclamped 20.175% SOC trajectory must be isolated in minBucket+1 rather than overwritten by clamped 20.0% SOC before Step 3 peak")
			assert.Greater(t, bestPath.metrics[3].batSuppliedHomeKWH, 0.02,
				"Remaining energy above reserve must be discharged during the Step 3 ($0.80/kWh) peak")
		})

		t.Run("EqualSOC_PrefersLateJustInTimeChargingViaHoldingCost", func(t *testing.T) {
			t.Parallel()

			// Across a flat 3-hour off-peak block ($0.05/kWh) ahead of a peak window ($0.45/kWh),
			// charging at Step 0 vs waiting in Standby at Step 0 and charging at Step 1 both reach
			// the exact same 100.0% SOC in bucket 200. Because nextState.soc == curr.state.soc,
			// newCost < curr.totalCost breaks the tie via batteryHoldingCostPerHourPerKWH in favor of
			// waiting at Step 0 and charging just-in-time (while respecting the 40-min pre-peak buffer).
			jitTimeline := []planInterval{
				{
					index:         0,
					startTime:     now,
					endTime:       now.Add(time.Hour),
					durationHours: 1.0,
					importRate:    0.05,
					loadKWH:       0.0,
					minSOC:        20.0,
				},
				{
					index:         1,
					startTime:     now.Add(time.Hour),
					endTime:       now.Add(2 * time.Hour),
					durationHours: 1.0,
					importRate:    0.05,
					loadKWH:       0.0,
					minSOC:        20.0,
				},
				{
					index:         2,
					startTime:     now.Add(2 * time.Hour),
					endTime:       now.Add(2*time.Hour + 20*time.Minute),
					durationHours: 1.0 / 3.0,
					importRate:    0.05,
					loadKWH:       0.0,
					minSOC:        20.0,
				},
				{
					index:         3,
					startTime:     now.Add(2*time.Hour + 20*time.Minute),
					endTime:       now.Add(4*time.Hour + 20*time.Minute),
					durationHours: 2.0,
					importRate:    0.45,
					loadKWH:       9.0,
					minSOC:        20.0,
				},
			}

			jitSettings := types.Settings{
				MinBatterySOC:       20.0,
				GridChargeBatteries: true,
			}
			jitStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            75.0,
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}
			jitState := planState{
				energyKWH:      13.5 * 0.75,
				soc:            75.0,
				capacityKWH:    13.5,
				maxChargeKW:    5.0,
				maxDischargeKW: 5.0,
				time:           now,
			}

			anchors := c.detectPlanningAnchors(jitTimeline, nil, jitStatus, jitSettings, nil)
			bestPath, err := c.searchOptimalPlan(ctx, jitTimeline, jitState, anchors, jitSettings, jitStatus, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 4)

			assert.NotEqual(t, types.BatteryModeChargeAny, bestPath.actions[0].batteryMode,
				"When two paths reach the exact same 100% SOC, holding cost tie-breaker must prefer waiting at Step 0 and charging later")
			assert.Equal(t, types.BatteryModeChargeAny, bestPath.actions[1].batteryMode,
				"Should charge at Step 1 (completing before the Step 2 20-min pre-peak buffer)")
			assert.Equal(t, types.BatteryModeStandby, bestPath.actions[2].batteryMode,
				"Should rest in Standby during the 20-min pre-peak buffer at Step 2")
		})

		t.Run("ModeSlotIsolation_SolarModeExportDoesNotCollideWithSolarModeAny", func(t *testing.T) {
			t.Parallel()

			// At Step 1 (20-min interval), solar (0.35 kWh = 1.05 kW >= minDirectSolarExportKW) slightly exceeds
			// home load (0.30 kWh), and exportRate ($0.12) exceeds importRate ($0.10) by $0.02.
			// On a 27 kWh battery, the SOC difference between SolarModeAny (absorbing 0.05 kWh net solar -> +0.17% SOC)
			// and SolarModeExport (exporting 0.35 kWh solar while battery covers 0.30 kWh load -> -1.17% SOC)
			// or between trajectories reaching the same bucket in Load+SolarModeAny (mIdx=0) vs Load+SolarModeExport (mIdx=4)
			// is isolated into separate mode slots so SolarModeExport's immediate credit cannot overwrite SolarModeAny's
			// trajectory ahead of an evening $0.60/kWh peak.
			modeSlotTimeline := []planInterval{
				{
					index:         0,
					startTime:     now,
					endTime:       now.Add(20 * time.Minute),
					durationHours: 1.0 / 3.0,
					importRate:    0.10,
					exportRate:    0.05,
					loadKWH:       0.10,
					solarKWH:      0.10,
					minSOC:        20.0,
				},
				{
					index:         1,
					startTime:     now.Add(20 * time.Minute),
					endTime:       now.Add(40 * time.Minute),
					durationHours: 1.0 / 3.0,
					importRate:    0.10,
					exportRate:    0.12,
					loadKWH:       0.02,
					solarKWH:      0.35, // 1.05 kW solar
					minSOC:        20.0,
				},
				{
					index:         2,
					startTime:     now.Add(40 * time.Minute),
					endTime:       now.Add(100 * time.Minute),
					durationHours: 1.0,
					importRate:    0.60,
					exportRate:    0.05,
					loadKWH:       3.0,
					solarKWH:      0.0,
					minSOC:        20.0,
				},
			}

			slotSettings := types.Settings{
				MinBatterySOC:       20.0,
				ManageTOUSchedules:  true,
				GridExportSolar:     true,
				GridChargeBatteries: false,
			}
			slotStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    27.0,
				BatterySOC:            30.0,
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    10.0,
				MaxBatteryDischargeKW: 10.0,
			}
			slotState := planState{
				energyKWH:      27.0 * 0.30,
				soc:            30.0,
				capacityKWH:    27.0,
				maxChargeKW:    10.0,
				maxDischargeKW: 10.0,
				time:           now,
			}

			// Verify modeIndex isolates SolarModeExport (slot 4) from SolarModeAny (slot 0)
			assert.Equal(t, 0, modeIndex(types.BatteryModeLoad, types.SolarModeAny))
			assert.Equal(t, 4, modeIndex(types.BatteryModeLoad, types.SolarModeExport))

			anchors := c.detectPlanningAnchors(modeSlotTimeline, nil, slotStatus, slotSettings, nil)
			bestPath, err := c.searchOptimalPlan(ctx, modeSlotTimeline, slotState, anchors, slotSettings, slotStatus, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 3)

			// Step 1 should absorb the 0.33 kWh net solar for the $0.60/kWh Step 2 peak rather than exporting at $0.12/kWh
			assert.Equal(t, types.SolarModeAny, bestPath.actions[1].solarMode,
				"SolarModeAny must survive Step 1 and beat SolarModeExport when Step 2 has a $0.60/kWh peak deficit")
		})

		t.Run("CostOnlyTieBreaker_RejectsMidPeriodStandbyChurnAheadOfSolarRefill", func(t *testing.T) {
			t.Parallel()

			// Across a flat off-peak block ($0.10/kWh) with small baseload (0.045 kWh per 20-min step = 0.35% SOC on 13.5 kWh)
			// followed by a free solar refill (12.0 kWh) that fills the battery to 100% before an evening $0.50/kWh peak,
			// switching to Standby for one step saves 0.35% SOC (landing in the same 0.5% bucket as continuous Load)
			// at the cost of importing 0.045 kWh ($0.0045) + modeSwitchPenalty.
			// Because intra-bucket tie-breaking compares accumulated cost directly rather than crediting intra-bucket
			// SOC differences at maxFutureValuationRates ($0.50/kWh), the solver must remain in continuous Load without Standby churn.
			churnTimeline := []planInterval{
				{
					index:         0,
					startTime:     now,
					endTime:       now.Add(20 * time.Minute),
					durationHours: 1.0 / 3.0,
					importRate:    0.10,
					loadKWH:       0.045,
					minSOC:        20.0,
				},
				{
					index:         1,
					startTime:     now.Add(20 * time.Minute),
					endTime:       now.Add(40 * time.Minute),
					durationHours: 1.0 / 3.0,
					importRate:    0.10,
					loadKWH:       0.045,
					minSOC:        20.0,
				},
				{
					index:         2,
					startTime:     now.Add(40 * time.Minute),
					endTime:       now.Add(3*time.Hour + 40*time.Minute),
					durationHours: 3.0,
					importRate:    0.10,
					loadKWH:       0.5,
					solarKWH:      12.0, // Fills battery to 100% for free
					minSOC:        20.0,
				},
				{
					index:         3,
					startTime:     now.Add(3*time.Hour + 40*time.Minute),
					endTime:       now.Add(5*time.Hour + 40*time.Minute),
					durationHours: 2.0,
					importRate:    0.50,
					loadKWH:       6.0,
					minSOC:        20.0,
				},
			}

			churnSettings := types.Settings{
				MinBatterySOC: 20.0,
			}
			churnStatus := types.SystemStatus{
				Timestamp:             now,
				BatteryCapacityKWH:    13.5,
				BatterySOC:            50.20,
				BatteryAboveMinSOC:    true,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}
			churnState := planState{
				energyKWH:      13.5 * 0.5020,
				soc:            50.20,
				capacityKWH:    13.5,
				maxChargeKW:    5.0,
				maxDischargeKW: 5.0,
				time:           now,
			}

			anchors := c.detectPlanningAnchors(churnTimeline, nil, churnStatus, churnSettings, nil)
			bestPath, err := c.searchOptimalPlan(ctx, churnTimeline, churnState, anchors, churnSettings, churnStatus, nil, nil)
			require.NoError(t, err)
			require.Len(t, bestPath.actions, 4)

			assert.Equal(t, types.BatteryModeLoad, bestPath.actions[0].batteryMode)
			assert.Equal(t, types.BatteryModeLoad, bestPath.actions[1].batteryMode,
				"Must remain in continuous BatteryModeLoad rather than churning into Standby before a free solar refill")
		})
	})

	t.Run("BatteryExport_DelaysDumpToEndOfFlatPeakWindow", func(t *testing.T) {
		t.Parallel()

		// 8-hour flat peak window (12:00 PM - 8:00 PM, 24 x 20-min intervals) at $0.15/kWh import & export,
		// followed by off-peak ($0.05/kWh import, $0.02/kWh export).
		// Battery starts at 90% SOC on a 27.0 kWh system (10 kW max discharge).
		// The planner must stay in BatteryModeLoad at the start of the peak window (12:00 PM) to cover
		// afternoon home load and delay BatteryModeExport until the end of the peak window, discharging
		// down to the 20% reserve floor right as peak concludes.
		var peakTimeline []planInterval
		idx := 0
		for i := 0; i < 24; i++ {
			start := now.Add(time.Duration(i*20) * time.Minute)
			peakTimeline = append(peakTimeline, planInterval{
				index:         idx,
				startTime:     start,
				endTime:       start.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.15,
				exportRate:    0.15,
				loadKWH:       0.15,
				solarKWH:      0.0,
				minSOC:        20.0,
			})
			idx++
		}
		for i := 0; i < 6; i++ {
			start := now.Add(8*time.Hour + time.Duration(i*20)*time.Minute)
			peakTimeline = append(peakTimeline, planInterval{
				index:         idx,
				startTime:     start,
				endTime:       start.Add(20 * time.Minute),
				durationHours: 1.0 / 3.0,
				importRate:    0.05,
				exportRate:    0.02,
				loadKWH:       0.15,
				solarKWH:      0.0,
				minSOC:        20.0,
			})
			idx++
		}

		exportSettings := types.Settings{
			MinBatterySOC:       20.0,
			ManageTOUSchedules:  true,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			OptimizationProfile: "balanced",
		}
		exportStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    27.0,
			BatterySOC:            90.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 10.0,
		}
		exportState := planState{
			time:           now,
			energyKWH:      27.0 * 0.90,
			soc:            90.0,
			capacityKWH:    27.0,
			maxChargeKW:    10.0,
			maxDischargeKW: 10.0,
		}

		anchors := c.detectPlanningAnchors(peakTimeline, nil, exportStatus, exportSettings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, peakTimeline, exportState, anchors, exportSettings, exportStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPath.actions, len(peakTimeline))

		// Must remain in BatteryModeLoad across the first 19 intervals (12:00 PM - 6:20 PM)
		// and only switch to BatteryModeExport in the final 5 intervals (6:20 PM - 8:00 PM)
		// needed to drain the remaining SOC down to the 20% reserve floor right as peak ends.
		for i := 0; i < 19; i++ {
			assert.Equal(t, types.BatteryModeLoad, bestPath.actions[i].batteryMode,
				"Must remain in BatteryModeLoad before the final export window at the end of peak (step %d)", i)
		}
		for i := 19; i < 24; i++ {
			assert.Equal(t, types.BatteryModeExport, bestPath.actions[i].batteryMode,
				"Must delay BatteryModeExport to the final intervals of the flat peak window (step %d)", i)
		}
		assert.InDelta(t, 20.0, bestPath.states[24].soc, 0.5,
			"Late peak export must discharge down to the 20% reserve floor by the end of peak")
	})

	t.Run("ReserveStepUp_CountsPostPeakRechargeAsWearWhenDrainedFromAbove", func(t *testing.T) {
		t.Parallel()

		// Scenario: Battery starts at 90% SOC (12.15 kWh on 13.5 kWh pack).
		// - Step 0 (Peak): minSOC = 20%.
		// - Step 1 (Post-peak off-peak charging schedule): importRate = $0.06/kWh, minSOC = 50% (reserve step-up).
		// - Step 2 (Morning solar): plentiful free solar (minAlternativeValue = $0.00/kWh).
		//
		// Case 1: Marginal peak export ($0.082/kWh, or $0.075/kWh with $0.01 export gate) with 0 home load
		// across two 1-hour peak intervals before a 22:00 super-off-peak refill interval ($0.055/kWh, minSOC = 50%).
		// Notice that $0.055/0.90 + $0.02 = $0.0811/kWh <= $0.082/kWh, so exportFloorSOC treats Step 2 ($0.055/kWh)
		// as the cheap refill interval (gridRefillIdx = 2) and does NOT clamp the export floor to Step 2's 50% reserve.
		// At Step 0, exporting from 90% -> ~51% is profitable because energy above 50% is refilled for free
		// by Step 3's solar. At Step 1 (entering at ~51% SOC), however, exporting down to 20% would force
		// an immediate 20% -> 50% grid recharge at Step 2 ($0.055/kWh = $0.0611/kWh post-RTE).
		// Without high-water-mark wear tracking, that 20% -> 50% recharge would look wear-free ($0.0611 + $0.005 = $0.0661/kWh < $0.082/kWh).
		// Counting that induced 20% -> 50% grid recharge as wear ($0.018/kWh charge wear + $0.005/kWh export wear = $0.023/kWh,
		// total cost $0.0611 + $0.023 = $0.0841/kWh > $0.082/kWh) causes the solver to stop exporting at ~51%
		// rather than draining down to 20%.
		marginalExportTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.082,
				loadKWH:       0.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.15,
				exportRate:    0.082,
				loadKWH:       0.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         2,
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(4 * time.Hour),
				durationHours: 2.0,
				importRate:    0.055,
				exportRate:    0.02,
				loadKWH:       0.0,
				solarKWH:      0.0,
				minSOC:        50.0,
			},
			{
				index:         3,
				startTime:     now.Add(4 * time.Hour),
				endTime:       now.Add(8 * time.Hour),
				durationHours: 4.0,
				importRate:    0.055,
				exportRate:    0.02,
				loadKWH:       0.0,
				solarKWH:      15.0,
				minSOC:        20.0,
			},
		}

		stepUpSettings := types.Settings{
			MinBatterySOC:       20.0,
			ManageTOUSchedules:  true,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			OptimizationProfile: "balanced",
		}
		stepUpStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    13.5,
			BatterySOC:            90.0,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}
		stepUpState := planState{
			time:           now,
			energyKWH:      13.5 * 0.90,
			soc:            90.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}

		anchors := c.detectPlanningAnchors(marginalExportTimeline, nil, stepUpStatus, stepUpSettings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, marginalExportTimeline, stepUpState, anchors, stepUpSettings, stepUpStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPath.actions, 4)
		assert.GreaterOrEqual(t, bestPath.states[2].soc, 50.0,
			"Must not export below 50% at $0.085/kWh when post-peak 50% reserve step-up forces a $0.06/kWh grid recharge + $0.03/kWh full-cycle wear")

		// Case 2: Peak self-consumption (BatteryModeLoad) during a $0.15/kWh peak with 9.0 kWh home load
		// (which drains the battery from 90% down to 20% before the 50% post-peak reserve step-up).
		// Offsetting $0.15/kWh peak imports easily overcomes the $0.06/kWh ($0.0667/kWh post-RTE) + $0.02/kWh
		// post-peak recharge cost ($0.0867/kWh total), so the solver MUST still discharge via BatteryModeLoad
		// rather than forcing Standby during peak.
		peakLoadTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(2 * time.Hour),
				durationHours: 2.0,
				importRate:    0.15,
				exportRate:    0.04,
				loadKWH:       9.0,
				solarKWH:      0.0,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(2 * time.Hour),
				endTime:       now.Add(4 * time.Hour),
				durationHours: 2.0,
				importRate:    0.06,
				exportRate:    0.02,
				loadKWH:       0.0,
				solarKWH:      0.0,
				minSOC:        50.0,
			},
		}

		anchorsLoad := c.detectPlanningAnchors(peakLoadTimeline, nil, stepUpStatus, stepUpSettings, nil)
		bestPathLoad, err := c.searchOptimalPlan(ctx, peakLoadTimeline, stepUpState, anchorsLoad, stepUpSettings, stepUpStatus, nil, nil)
		require.NoError(t, err)
		require.Len(t, bestPathLoad.actions, 2)
		assert.Equal(t, types.BatteryModeLoad, bestPathLoad.actions[0].batteryMode,
			"Must still discharge via BatteryModeLoad during $0.15/kWh peak when savings exceed post-peak $0.06/kWh recharge + $0.02/kWh wear")
		assert.InDelta(t, 20.0, bestPathLoad.states[1].soc, 0.5,
			"Should discharge down to 20% peak reserve to cover the $0.15/kWh peak load")
		assert.Equal(t, types.BatteryModeChargeAny, bestPathLoad.actions[1].batteryMode,
			"Should recharge back to 50% post-peak reserve during off-peak")
	})

	t.Run("SolarModeSwitchPenalty_BlocksShortMinorRealTimeBlipWhilePreservingSpike", func(t *testing.T) {
		t.Parallel()

		// Scenario: Battery is at 83.3% SOC on a 27.0 kWh system and currently in BatteryModeLoad + SolarModeAny.
		// Step 0 is a 13-minute end-of-hour interval (09:47 - 10:00) with 1.85 kWh solar and 0.21 kWh load,
		// followed by midday solar intervals (10:00 - 12:00) at $0.0653/kWh import / $0.0213/kWh export that
		// fill the battery to 100% SOC and overflow excess solar at $0.0213/kWh.
		//
		// Case 1 (Minor 0.82c/kWh blip at 09:47: $0.0735/kWh import, $0.0295/kWh export):
		// Exporting 1.85 kWh at $0.0295 vs $0.0213 only gains +$0.0152 gross (-$0.0011 wear = +$0.0141 net).
		// Because the $0.0082/kWh drop at 10:00 is below priceMaterialityThresholdDollars ($0.03/kWh),
		// switching SolarModeAny -> SolarModeExport -> SolarModeAny incurs 2 x solarModeSwitchPenalty ($0.0200),
		// which exceeds the +$0.0141 gain and keeps the system in SolarModeAny without chattering.
		minorBlipTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(13 * time.Minute),
				durationHours: 13.0 / 60.0,
				importRate:    0.0735,
				exportRate:    0.0295,
				loadKWH:       0.21,
				solarKWH:      1.85,
				minSOC:        25.0,
			},
			{
				index:         1,
				startTime:     now.Add(13 * time.Minute),
				endTime:       now.Add(73 * time.Minute),
				durationHours: 1.0,
				importRate:    0.0653,
				exportRate:    0.0213,
				loadKWH:       1.10,
				solarKWH:      9.00,
				minSOC:        25.0,
			},
			{
				index:         2,
				startTime:     now.Add(73 * time.Minute),
				endTime:       now.Add(133 * time.Minute),
				durationHours: 1.0,
				importRate:    0.0649,
				exportRate:    0.0198,
				loadKWH:       1.08,
				solarKWH:      8.46,
				minSOC:        25.0,
			},
		}

		solarSettings := types.Settings{
			MinBatterySOC:       25.0,
			ManageTOUSchedules:  true,
			GridExportSolar:     true,
			GridChargeBatteries: true,
			OptimizationProfile: "balanced",
		}
		solarStatus := types.SystemStatus{
			Timestamp:             now,
			BatteryCapacityKWH:    27.0,
			BatterySOC:            83.3,
			BatteryAboveMinSOC:    true,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 10.0,
			SolarKW:               8.5,
			HomeKW:                1.0,
		}
		solarState := planState{
			time:           now,
			energyKWH:      27.0 * 0.833,
			soc:            83.3,
			capacityKWH:    27.0,
			maxChargeKW:    10.0,
			maxDischargeKW: 10.0,
		}
		prevLoadPrice := types.Price{
			DollarsPerKWH:                 0.0650,
			GenerationCreditDollarsPerKWH: 0.0210,
			SeparateGenerationCredit:      true,
		}
		lastLoadAction := &types.Action{
			Timestamp:    now.Add(-20 * time.Minute),
			BatteryMode:  types.BatteryModeLoad,
			SolarMode:    types.SolarModeAny,
			CurrentPrice: &prevLoadPrice,
		}

		anchorsBlip := c.detectPlanningAnchors(minorBlipTimeline, nil, solarStatus, solarSettings, nil)
		bestPathBlip, err := c.searchOptimalPlan(ctx, minorBlipTimeline, solarState, anchorsBlip, solarSettings, solarStatus, nil, lastLoadAction)
		require.NoError(t, err)
		require.Len(t, bestPathBlip.actions, 3)
		assert.Equal(t, types.SolarModeAny, bestPathBlip.actions[0].solarMode,
			"Must stay in SolarModeAny during a 13-minute minor price blip (+$0.0141 net gain < $0.02 round-trip solarModeSwitchPenalty)")

		// Case 2 (Genuine morning price spike at 07:27: $0.1088/kWh import, $0.0648/kWh export):
		// Exporting 0.85 kWh at $0.0648 vs $0.0198 gains +$0.0383 gross, and the $0.045/kWh drop at 08:00
		// exceeds priceMaterialityThresholdDollars ($0.03/kWh) so exiting at Step 1 is free (isSolarPriceDrop == true).
		// The +$0.0383 gain easily clears the single $0.01 entry penalty.
		spikeTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(33 * time.Minute),
				durationHours: 33.0 / 60.0,
				importRate:    0.1088,
				exportRate:    0.0648,
				loadKWH:       0.47,
				solarKWH:      0.85,
				minSOC:        25.0,
			},
			minorBlipTimeline[1],
			minorBlipTimeline[2],
		}
		anchorsSpike := c.detectPlanningAnchors(spikeTimeline, nil, solarStatus, solarSettings, nil)
		bestPathSpike, err := c.searchOptimalPlan(ctx, spikeTimeline, solarState, anchorsSpike, solarSettings, solarStatus, nil, lastLoadAction)
		require.NoError(t, err)
		require.Len(t, bestPathSpike.actions, 3)
		assert.Equal(t, types.SolarModeExport, bestPathSpike.actions[0].solarMode,
			"Must switch to SolarModeExport during a genuine morning price spike (+$0.0383 gain > $0.01 solarModeSwitchPenalty)")
	})

	t.Run("DeficitWithoutChargeCandidate_And_ExportRateChangeInFlatImportWindow_DoNotPruneAllPaths", func(t *testing.T) {
		t.Parallel()

		// Reproduces a scenario where:
		// 1. A mid-morning reserve step-up from 5% to 20% leaves the battery in deficit during an expensive solar hour
		//    where ChargeAny is not offered, which previously pruned all non-charging candidates when GridChargeBatteries=true.
		// 2. An overnight flat-import off-peak window ($0.064/kWh) has varying hourly export rates ($0.025 -> $0.026/kWh)
		//    and a fractional first interval that fills the battery to ~98.6% (leaving < 0.25 kWh headroom so Step 1 drops
		//    ChargeAny to Standby, then Step 2's load re-opens headroom at the same $0.064/kWh import rate).
		flatImportVaryingExportTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(55 * time.Minute),
				durationHours: 55.0 / 60.0,
				importRate:    0.064,
				exportRate:    0.025,
				loadKWH:       0.40,
				minSOC:        5.0,
			},
			{
				index:         1,
				startTime:     now.Add(55 * time.Minute),
				endTime:       now.Add(115 * time.Minute),
				durationHours: 1.0,
				importRate:    0.064,
				exportRate:    0.026, // Different export rate in the same flat $0.064/kWh import window
				loadKWH:       0.50,
				minSOC:        5.0,
			},
			{
				index:         2,
				startTime:     now.Add(115 * time.Minute),
				endTime:       now.Add(175 * time.Minute),
				durationHours: 1.0,
				importRate:    0.064,
				exportRate:    0.027,
				loadKWH:       0.50,
				minSOC:        5.0,
			},
			{
				index:         3,
				startTime:     now.Add(175 * time.Minute),
				endTime:       now.Add(235 * time.Minute),
				durationHours: 1.0,
				importRate:    0.250,
				exportRate:    0.080,
				loadKWH:       1.50,
				solarKWH:      1.60, // Slight net solar (+0.10 kWh), not enough to reach 20% if entering below 18.9%
				minSOC:        20.0, // Reserve step-up to 20%
			},
		}

		// Start near full (92%) so Step 0 ChargeAny reaches ~98.6% (< 0.25 kWh headroom on 13.5 kWh battery).
		nearFullState := planState{
			energyKWH:      13.5 * 0.92,
			soc:            92.0,
			capacityKWH:    13.5,
			maxChargeKW:    1.1,
			maxDischargeKW: 5.0,
		}
		nearFullStatus := types.SystemStatus{
			BatteryCapacityKWH:    13.5,
			BatterySOC:            92.0,
			MaxBatteryChargeKW:    1.1,
			MaxBatteryDischargeKW: 5.0,
		}
		nearFullSettings := types.Settings{
			MinBatterySOC:       5.0,
			GridChargeBatteries: true,
			GridExportSolar:     true,
		}
		anchors := c.detectPlanningAnchors(flatImportVaryingExportTimeline, nil, nearFullStatus, nearFullSettings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, flatImportVaryingExportTimeline, nearFullState, anchors, nearFullSettings, nearFullStatus, nil, nil)
		require.NoError(t, err, "searchOptimalPlan must not prune all candidate paths when export rates vary in a flat import window or ChargeAny is unavailable during a deficit")
		require.NotNil(t, bestPath)
		assert.Len(t, bestPath.actions, len(flatImportVaryingExportTimeline))

		// Also verify direct deficit step where SOC is 12% (< 20% minSOC) during an expensive solar hour where ChargeAny is not offered.
		deficitNoChargeTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.35,
				exportRate:    0.30,
				loadKWH:       1.0,
				solarKWH:      2.5, // Net solar > 0 suppresses grid ChargeAny while GridExportSolar=true offers SolarModeExport
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.10,
				exportRate:    0.04,
				loadKWH:       1.0,
				solarKWH:      3.0,
				minSOC:        20.0,
			},
		}
		deficitState := planState{
			energyKWH:      13.5 * 0.12,
			soc:            12.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		deficitStatus := nearFullStatus
		deficitStatus.BatterySOC = 12.0
		deficitStatus.MaxBatteryChargeKW = 5.0
		deficitAnchors := c.detectPlanningAnchors(deficitNoChargeTimeline, nil, deficitStatus, nearFullSettings, nil)
		deficitPath, err := c.searchOptimalPlan(ctx, deficitNoChargeTimeline, deficitState, deficitAnchors, nearFullSettings, deficitStatus, nil, nil)
		require.NoError(t, err, "searchOptimalPlan must not fail when SOC is in deficit and solar recharge candidates are available instead of ChargeAny")
		require.NotNil(t, deficitPath)
		assert.NotEqual(t, types.SolarModeExport, deficitPath.actions[0].solarMode,
			"When in reserve deficit during a solar hour, planner must retain solar to recharge the battery rather than exporting solar")
	})

	t.Run("ReserveDeficitTolerance_DoesNotForcePeakGridTopUpOnOnePercentDrift", func(t *testing.T) {
		t.Parallel()

		// Battery is at 19.0% on a 20.0% reserve during a $0.30/kWh peak hour followed by $0.08/kWh off-peak.
		// Planner must choose Standby (not ChargeAny) during the peak hour.
		driftTimeline := []planInterval{
			{
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.30,
				exportRate:    0.05,
				loadKWH:       1.5,
				minSOC:        20.0,
			},
			{
				index:         1,
				startTime:     now.Add(time.Hour),
				endTime:       now.Add(2 * time.Hour),
				durationHours: 1.0,
				importRate:    0.08,
				exportRate:    0.04,
				loadKWH:       1.0,
				minSOC:        20.0,
			},
		}
		driftState := planState{
			energyKWH:      13.5 * 0.19,
			soc:            19.0,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		driftStatus := status
		driftStatus.BatterySOC = 19.0
		driftAnchors := c.detectPlanningAnchors(driftTimeline, nil, driftStatus, settings, nil)
		bestPath, err := c.searchOptimalPlan(ctx, driftTimeline, driftState, driftAnchors, settings, driftStatus, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, bestPath)
		assert.Equal(t, types.BatteryModeLoad, bestPath.actions[0].batteryMode,
			"19.0%% SOC on a 20.0%% reserve must rest in reserve-protected passthrough (BatteryModeLoad) during a peak hour rather than forcing a 1%% grid top-up")
		assert.Equal(t, types.ActionReasonBatteryAtReserve, bestPath.actions[0].reason)
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

		decision, _ := finalizeDecisionAndPlan(ctx, unconstrainedPath, timeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)

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

		decision, plan := finalizeDecisionAndPlan(ctx, chargePath, chargeTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)
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

		decision, plan := finalizeDecisionAndPlan(ctx, reachPath, reachTimeline, status, currentPrice, now, types.Settings{GridChargeBatteries: true}, nil, nil)
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

		decision, plan := finalizeDecisionAndPlan(ctx, solarPath, solarTimeline, status, currentPrice, now, types.Settings{
			GridExportSolar:                      true,
			MinExportHoldDifferenceDollarsPerKWH: 0.02,
		}, nil, nil)
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
				index:         0,
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
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				minSOC:        20.0,
			},
			{
				index:         1,
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
				index:         2,
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
				index:         0,
				startTime:     now,
				endTime:       now.Add(time.Hour),
				durationHours: 1.0,
				importRate:    0.05,
				minSOC:        30.0,
			},
			{
				index:         1,
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
			BatteryCapacityKWH: 10.0,
			BatterySOC:         22.0,
			HomeKW:             1.0,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
		}

		// Next hour rate is $0.04/kWh higher (0.14 + 0.04 = 0.18 vs 0.14, exceeding priceMaterialityThresholdDollars = $0.03).
		// With 0.2 kWh above reserve (22% vs 20% on 10 kWh pack), benefit of holding for next hour is ~0.2 * $0.04 = $0.008,
		// which exceeds modeSwitchPenalty ($0.005) when lastAction is nil, but is less than the step-0 inertia + switch
		// threshold ($0.00833 + $0.005 = $0.01333) when lastAction is Load.
		futurePrices := []types.Price{
			{
				TSStart:              now.Add(time.Hour),
				TSEnd:                now.Add(2 * time.Hour),
				DollarsPerKWH:        0.14,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              now.Add(2 * time.Hour),
				TSEnd:                now.Add(3 * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              now.Add(3 * time.Hour),
				TSEnd:                now.Add(4 * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              now.Add(4 * time.Hour),
				TSEnd:                now.Add(5 * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
		}

		decision, plan, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, lastAction)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Inertia prevents chattering away from BatteryModeLoad into Standby on small delta
		assert.Equal(t, types.BatteryModeLoad, decision.Action.BatteryMode)
	})

	t.Run("Plan_FiltersFaultsPausedAndStaleActions", func(t *testing.T) {
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
			BatteryCapacityKWH: 10.0,
			BatterySOC:         22.0,
			HomeKW:             1.0,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
		}

		futurePrices := []types.Price{
			{
				TSStart:              now.Add(time.Hour),
				TSEnd:                now.Add(2 * time.Hour),
				DollarsPerKWH:        0.14,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              now.Add(2 * time.Hour),
				TSEnd:                now.Add(3 * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              now.Add(3 * time.Hour),
				TSEnd:                now.Add(4 * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
			{
				TSStart:              now.Add(4 * time.Hour),
				TSEnd:                now.Add(5 * time.Hour),
				DollarsPerKWH:        0.10,
				GridUseDollarsPerKWH: 0.04,
			},
		}

		// Baseline: When lastAction is valid Load, inertia maintains Load
		validLoad := &types.Action{
			BatteryMode: types.BatteryModeLoad,
			Timestamp:   now.Add(-15 * time.Minute),
		}
		decValid, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, validLoad)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeLoad, decValid.Action.BatteryMode)

		// 1. Fault action is filtered -> treated as nil -> chooses BatteryModeStandby to save for higher rate
		faultAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
			Fault:       true,
			Timestamp:   now.Add(-15 * time.Minute),
		}
		decFault, _, err := c.Plan(ctx, status, currentPrice, futurePrices, nil, nil, settings, faultAction)
		require.NoError(t, err)
		assert.Equal(t, types.BatteryModeStandby, decFault.Action.BatteryMode)

		// 2. Paused action is filtered -> chooses BatteryModeStandby
		pausedAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
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
			BatteryMode: types.BatteryModeLoad,
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

		// Controller at 9:00 AM should charge toward the solar-headroom target (~56-65%), not 100%
		assert.Equal(t, types.BatteryModeChargeAny, decision.Action.BatteryMode)
		assert.LessOrEqual(t, decision.Action.ChargeToSOC, 70, "chargeToSOC must leave headroom for daytime solar")

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

func buildSyntheticPlanPath(timeline []planInterval, initial planState, actions []actionCandidate, settings types.Settings, eff float64) *planPath {
	n := len(timeline)
	states := make([]planState, n+1)
	metrics := make([]intervalMetrics, n)
	states[0] = initial
	var totalCost float64
	for i := 0; i < n; i++ {
		st, m := stepPhysics(states[i], actions[i], timeline[i], settings, eff)
		metrics[i] = m
		states[i+1] = st
		totalCost += m.costDollars
	}
	return &planPath{
		actions:   actions,
		states:    states,
		metrics:   metrics,
		timeline:  timeline,
		totalCost: totalCost,
		bestScore: totalCost,
	}
}

func TestDetermineRefinedStandbyReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC)
	timeline := []planInterval{
		{index: 0, startTime: now, endTime: now.Add(time.Hour), importRate: 0.05},
		{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), importRate: 0.05},
		{index: 2, startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), importRate: 0.35},
	}

	t.Run("ChargeReasonVPPPrep_AlwaysPreservesVPPReason", func(t *testing.T) {
		t.Parallel()
		reason, desc := determineRefinedStandbyReason(types.ActionReasonVPPPrep, timeline[0], timeline, 0)
		assert.Equal(t, types.ActionReasonVPPPrep, reason)
		assert.Equal(t, "Preserving battery in standby ahead of VPP event.", desc)
	})

	t.Run("UpcomingPeakRate_ReturnsDeficitSaveForPeak", func(t *testing.T) {
		t.Parallel()
		reason, desc := determineRefinedStandbyReason(types.ActionReasonDeficitChargeNow, timeline[0], timeline, 0)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, reason)
		assert.Equal(t, "Preserving battery in standby for upcoming peak rates.", desc)
	})

	t.Run("FlatOrDecreasingRate_ReturnsHoldSimilarPrice", func(t *testing.T) {
		t.Parallel()
		flatTimeline := []planInterval{
			{index: 0, startTime: now, endTime: now.Add(time.Hour), importRate: 0.10},
			{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), importRate: 0.10},
			{index: 2, startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), importRate: 0.08},
		}
		reason, desc := determineRefinedStandbyReason(types.ActionReasonDeficitChargeNow, flatTimeline[0], flatTimeline, 0)
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, reason)
		assert.Equal(t, "Preserving battery in standby.", desc)
	})

	t.Run("ImmaterialRateIncrease_ReturnsHoldSimilarPrice", func(t *testing.T) {
		t.Parallel()
		immaterialTimeline := []planInterval{
			{index: 0, startTime: now, endTime: now.Add(time.Hour), importRate: 0.100},
			{index: 1, startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), importRate: 0.102},
		}
		reason, desc := determineRefinedStandbyReason(types.ActionReasonDeficitChargeNow, immaterialTimeline[0], immaterialTimeline, 0)
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, reason)
		assert.Equal(t, "Preserving battery in standby.", desc)
	})

	t.Run("LastIntervalInHorizon_ReturnsHoldSimilarPrice", func(t *testing.T) {
		t.Parallel()
		reason, desc := determineRefinedStandbyReason(types.ActionReasonDeficitChargeNow, timeline[2], timeline, 2)
		assert.Equal(t, types.ActionReasonHoldSimilarPrice, reason)
		assert.Equal(t, "Preserving battery in standby.", desc)
	})
}

func TestRefineOverchargedEpisodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC)

	t.Run("NoVPP_OverchargedDiscretionaryPrecharge_ClampsAndConvertsToStandby", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)
		origCost := p.totalCost

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		assert.InDelta(t, 76.50, refinedPath.actions[0].targetSOC, 0.5)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, refinedPath.actions[1].reason)
		assert.InDelta(t, 76.50, refinedPath.states[1].soc, 0.5)
		assert.InDelta(t, 76.50, refinedPath.states[2].soc, 0.5)
		assert.Greater(t, origCost, refinedPath.totalCost)
		assert.Equal(t, 0.0, p.actions[0].targetSOC, "Original plan must remain unmutated")
		assert.Equal(t, types.BatteryModeLoad, p.actions[1].batteryMode, "Original plan must remain unmutated")
	})

	t.Run("VPP_DiscretionaryPrechargeAheadOfVPP_ClampsAndPreservesVPPReason", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.10, solarKWH: 6.0, loadKWH: 1.0, minSOC: 20},
			{index: 3, startTime: start.Add(3 * time.Hour), endTime: start.Add(5 * time.Hour), durationHours: 2.0, importRate: 0.50, exportRate: 2.00, loadKWH: 2.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonVPPPrep, description: "VPP Pre-charging before deadline.", targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeStandby, solarMode: types.SolarModeAny, reason: types.ActionReasonVPPPrep, targetSOC: 0},
			{batteryMode: types.BatteryModeExport, reason: types.ActionReasonVPPActive, targetSOC: 20},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		assert.InDelta(t, 76.50, refinedPath.actions[0].targetSOC, 0.5)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, types.ActionReasonVPPPrep, refinedPath.actions[1].reason)
		assert.Equal(t, "Preserving battery in standby ahead of VPP event.", refinedPath.actions[1].description)
		assert.Equal(t, 0.0, p.actions[0].targetSOC, "Original plan must remain unmutated")
	})

	t.Run("VPP_DeadlineApproaching_EmergencyTopUpNotTrimmed", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonVPPPrep, actionName: PlanActionVPP2HourPrepEmergencyTopUp, targetSOC: 100},
			{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonVPPPrep, actionName: PlanActionVPP2HourPrepStandbyLock, targetSOC: 100},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "Must not modify emergency top-up in the immediate T-2h firmware window")
		assert.Same(t, p, refinedPath)
		assert.Equal(t, 100.0, p.actions[0].targetSOC)
	})

	t.Run("ChangingReserveSOC_ReserveStepUp_RespectsElevatedReserveWithoutAddingBufferToTargetSOC", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 8.0, minSOC: 50},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 50},
		}
		sett := types.Settings{
			MinBatterySOC:       20,
			OptimizationProfile: "conservative",
			GridChargeBatteries: true,
		}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.85)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.85)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		assert.Equal(t, 50.0, refinedPath.actions[0].targetSOC, "Must clamp targetSOC to the elevated 50% minSOC without adding the 10% conservative buffer")
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, 0.0, p.actions[0].targetSOC, "Original plan must remain unmutated")

		// Also verify on a flat 20% reserve when exitSOC (~25.8%) is between minSOC (20%) and minSOC+ReserveBufferPercent (30%):
		// targetSOC must clamp to exitSOC (~25.8%) rather than 30%, and then discharge down to the 20% minSOC during peak.
		flatReserveTimeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 6.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		initSt20 := planState{time: start, energyKWH: 3.0, soc: 20.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		p20 := buildSyntheticPlanPath(flatReserveTimeline, initSt20, acts, sett, 0.85)
		refined20, adjusted20 := p20.refineOverchargedEpisodes(ctx, sett, 0.85)
		require.True(t, adjusted20)
		assert.InDelta(t, 25.78, refined20.actions[0].targetSOC, 0.5, "Conservative refinement must not inflate targetSOC to 30% (20% minSOC + 10% buffer)")
		assert.Equal(t, types.BatteryModeStandby, refined20.actions[1].batteryMode)
		assert.InDelta(t, 25.78, refined20.states[2].soc, 0.5, "Must enter peak window at unbuffered exitSOC")
		assert.InDelta(t, 20.0, refined20.states[3].soc, 0.5, "Must discharge down to the 20% physical minSOC during peak")
	})

	t.Run("ChangingReserveSOC_LoweringReserveSOC_AllowsLowerClamp", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 5.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 4.5, soc: 30.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		assert.InDelta(t, refinedPath.states[2].soc, refinedPath.actions[0].targetSOC, 1e-4)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, 0.0, p.actions[0].targetSOC, "Original plan must remain unmutated")
	})

	t.Run("SolarHittingCapacity_AvoidsOverflowAndSavesRoundTripLoss", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.10, solarKWH: 8.0, loadKWH: 0.5, minSOC: 20},
			{index: 3, startTime: start.Add(3 * time.Hour), endTime: start.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.40, loadKWH: 8.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeStandby, solarMode: types.SolarModeAny, reason: types.ActionReasonDeficitSaveForPeak, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)
		origCost := p.totalCost

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		assert.InDelta(t, 76.50, refinedPath.actions[0].targetSOC, 0.5)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.InDelta(t, 100.0, refinedPath.states[3].soc, 0.5)
		assert.Greater(t, origCost, refinedPath.totalCost)
		assert.Equal(t, 0.0, p.actions[0].targetSOC, "Original plan must remain unmutated")
	})

	t.Run("SolarNotHittingCapacity_NoOverflow_NoDischarge_NoRefinement", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.10, solarKWH: 1.0, loadKWH: 0.5, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonDeficitSaveForPeak, targetSOC: 0},
			{batteryMode: types.BatteryModeStandby, solarMode: types.SolarModeAny, reason: types.ActionReasonDeficitSaveForPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "No churn occurred; refinement must return false")
		assert.Same(t, p, refinedPath)
	})

	t.Run("ChargingBeforePeak_HoldsStandbyDirectly_NoRefinement", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 0.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.45, loadKWH: 4.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonDeficitSaveForPeak, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "Battery already held standby directly before peak, no churn")
		assert.Same(t, p, refinedPath)
	})

	t.Run("SafetyRollback_RejectsRefinementIfCostIncreases", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)
		p.totalCost = 0.01

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "Must reject refinement when refined cost exceeds original cost")
		assert.Same(t, p, refinedPath, "Must return original plan pointer without modifications")
		assert.Equal(t, 0.01, p.totalCost, "Original cost must be preserved")
		assert.Equal(t, types.BatteryModeLoad, p.actions[1].batteryMode, "Original actions must remain untouched")
	})

	t.Run("NonZeroTargetSOC_ClampsWhenHigher_AndPreservesLower", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 90},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		assert.InDelta(t, 75.95, refinedPath.actions[0].targetSOC, 0.5, "Higher non-zero targetSOC (90) must be clamped down to ~75.95")
		assert.Equal(t, 90.0, p.actions[0].targetSOC, "Original plan targetSOC must remain untouched")
	})

	t.Run("NegativePricing_NotRefined", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: -0.02, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: -0.02, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonAlwaysChargeBelowThreshold, actionName: PlanActionNegativeOrForceChargeThresholdCharging, targetSOC: 100},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "Negative pricing charging must never be trimmed")
		assert.Same(t, p, refinedPath)
	})

	t.Run("Step0_StartingAboveOrAtRefinedTarget_ClampsTargetSOC_DoesNotFlipBatteryMode", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		// Battery starts at 86% SOC; charges in interval 0 to 100% then discharges 2.1 kWh in interval 1 down to exitSOC ~86%
		initSt := planState{time: start, energyKWH: 12.9, soc: 86.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted)
		assert.NotSame(t, p, refinedPath)
		// Step 0 must keep ChargeAny with clamped targetSOC (not flipped to Standby)
		assert.Equal(t, types.BatteryModeChargeAny, refinedPath.actions[0].batteryMode)
		assert.InDelta(t, 85.95, refinedPath.actions[0].targetSOC, 0.5)
		// Step 1 is converted to Standby
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
	})

	t.Run("NilOrEmptyPath_SafelyReturnsFalse", func(t *testing.T) {
		t.Parallel()
		var nilPath *planPath
		resNil, adjNil := nilPath.refineOverchargedEpisodes(ctx, types.Settings{}, 0.90)
		assert.False(t, adjNil)
		assert.Nil(t, resNil)

		emptyPath := &planPath{}
		resEmpty, adjEmpty := emptyPath.refineOverchargedEpisodes(ctx, types.Settings{}, 0.90)
		assert.False(t, adjEmpty)
		assert.Same(t, emptyPath, resEmpty)
	})

	t.Run("SolarModeExport_NotMisclassifiedAsPrematureLoadChurn", func(t *testing.T) {
		t.Parallel()
		// Interval 0: ChargeAny at $0.05
		// Interval 1: Direct Solar Export (BatteryModeLoad + SolarModeExport) where battery covers load so solar can export at $0.25
		// Interval 2: Peak at $0.35
		// Interval 1 is an intentional solar export arbitrage window, NOT premature load churn.
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.05, exportRate: 0.25, solarKWH: 0.3, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 3.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true, GridExportSolar: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonArbitrageChargeExport, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeExport, reason: types.ActionReasonDirectExport, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "Direct Solar Export (SolarModeExport) must not be misclassified as premature load churn")
		assert.Same(t, p, refinedPath)
	})

	t.Run("TerminalValuation_RejectsRefinementThatDrainsEndingBattery", func(t *testing.T) {
		t.Parallel()
		// Interval 0: ChargeAny at $0.05
		// Interval 1: Load at $0.05 (1 kWh load)
		// Interval 2: Peak at $0.35 (8 kWh load, drains battery to reserve if targetSOC is clamped!)
		// Without terminal valuation or when post-horizon rate is high, draining ending battery energy
		// below what the original trajectory banked must be accounted for in costSaved.
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 2.0, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.35, loadKWH: 0.5, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		// Start at 40% SOC; interval 0 can charge up to 8 kW (reaching ~90% SOC), interval 1 discharges 2 kWh (~76% SOC).
		// Interval 2 has only 0.5 kWh load, so clamping interval 0 to 77% would leave the battery with ~13% less terminal SOC,
		// losing banked energy valued at the $0.35/kWh post-horizon replacement rate!
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)
		p.anchors = planningAnchors{knownPostHorizonRate: 0.35}
		p.states[len(p.states)-1].energyKWH = 15.0 // Simulate original path ending with 100% banked energy valued at $0.35/kWh

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		assert.False(t, adjusted, "Must reject refinement when terminal valuation loss exceeds cashflow savings")
		assert.Same(t, p, refinedPath)
	})

	t.Run("CloudyMorningSubLoadSolar_RefinesOverchargeAndStopsBeforeNetSolarRefill", func(t *testing.T) {
		t.Parallel()

		// Overnight off-peak charge at $0.05/kWh reaches ~90.6% SOC on a 15 kWh battery.
		// Step 1 is an early-morning off-peak hour at the same $0.05/kWh rate with weak solar (0.30 kWh > 0.05 kWh,
		// but < 1.50 kWh load) where the battery discharges in BatteryModeLoad down to ~82.2% SOC.
		// Step 2 is a sunny mid-morning off-peak hour at the same $0.05/kWh rate with 8.0 kWh solar (> 1.0 kWh load)
		// that refills the battery to 100%.
		// Step 3 is an afternoon peak hour ($0.30/kWh) with 2.0 kWh load.
		// refineOverchargedEpisodes must:
		// 1. Include Step 1 in the same-rate window (because solarKWH - loadKWH <= 0.05 kWh) and convert Step 1
		//    from BatteryModeLoad to BatteryModeStandby.
		// 2. Stop the lookahead before Step 2's net solar refill (where solarKWH - loadKWH > 0.05 kWh) so exitSOC
		//    is measured at the pre-solar trough (~82.2% at states[2], clamping Step 0 targetSOC to 83%) rather than
		//    after Step 2 refills the battery to 100%.
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 0.0, solarKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 1.5, solarKWH: 0.3, minSOC: 20},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.05, loadKWH: 1.0, solarKWH: 8.0, minSOC: 20},
			{index: 3, startTime: start.Add(3 * time.Hour), endTime: start.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.30, loadKWH: 2.0, solarKWH: 0.0, minSOC: 20},
		}
		sett := types.Settings{MinBatterySOC: 20, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeAny, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)
		p.anchors = planningAnchors{knownPostHorizonRate: 0.05, minHorizonImportRate: 0.05}

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted, "Must refine overcharge across sub-load morning solar and stop lookahead before net solar refill")
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode,
			"Sub-load morning solar interval at the same $0.05/kWh charge rate must be converted from Load to Standby")
		assert.InDelta(t, 82.16, refinedPath.actions[0].targetSOC, 0.5,
			"Charge episode targetSOC must be clamped to the pre-solar trough (~82.2%%) rather than 91%%")
	})

	t.Run("ExportRateAboveImportRate_RefinesSamePriceOverchargeBleed", func(t *testing.T) {
		t.Parallel()

		// When a tariff has exportRate ($0.0722/kWh) > importRate ($0.0606/kWh) + $0.01 (such as Georgia Power TOU),
		// refineOverchargedEpisodes must compare timeline[k].exportRate against maxChargeExportRate (not maxChargeImportRate)
		// so same-price ChargeAny -> Load -> Standby bleed is properly eliminated.
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.0606, exportRate: 0.0722, loadKWH: 0.0, solarKWH: 0.0, minSOC: 10},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.0606, exportRate: 0.0722, loadKWH: 2.0, solarKWH: 0.0, minSOC: 10},
			{index: 2, startTime: start.Add(2 * time.Hour), endTime: start.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.0606, exportRate: 0.0722, loadKWH: 1.0, solarKWH: 0.0, minSOC: 10},
			{index: 3, startTime: start.Add(3 * time.Hour), endTime: start.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.4000, exportRate: 0.0722, loadKWH: 2.0, solarKWH: 0.0, minSOC: 10},
		}
		sett := types.Settings{MinBatterySOC: 10, GridChargeBatteries: true}
		initSt := planState{time: start, energyKWH: 6.0, soc: 40.0, capacityKWH: 15.0, maxChargeKW: 8.0, maxDischargeKW: 8.0}
		acts := []actionCandidate{
			{batteryMode: types.BatteryModeChargeAny, reason: types.ActionReasonDeficitChargeNow, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonSufficientBattery, targetSOC: 0},
			{batteryMode: types.BatteryModeStandby, reason: types.ActionReasonDeficitSaveForPeak, targetSOC: 0},
			{batteryMode: types.BatteryModeLoad, reason: types.ActionReasonDischargeAtPeak, targetSOC: 0},
		}
		p := buildSyntheticPlanPath(timeline, initSt, acts, sett, 0.90)
		p.anchors = planningAnchors{knownPostHorizonRate: 0.0606, minHorizonImportRate: 0.0606}

		refinedPath, adjusted := p.refineOverchargedEpisodes(ctx, sett, 0.90)
		require.True(t, adjusted, "Must refine same-rate overcharge even when exportRate > importRate + $0.01")
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode,
			"Same-rate Load bleed interval must be converted to Standby")
		assert.Greater(t, refinedPath.actions[0].targetSOC, 0.0, "Charge episode must be clamped to targetSOC")
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

		path, err := c.searchOptimalPlan(ctx, timeline, initSt, planningAnchors{}, sett, status, nil, nil)
		require.NoError(t, err)
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

		path, err := c.searchOptimalPlan(ctx, timeline, initSt, planningAnchors{knownPostHorizonRate: 0.2075, minHorizonImportRate: 0.2075}, sett, status, nil, nil)
		require.NoError(t, err)
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
		vppAnchors := planningAnchors{
			vppEvents: []vppAnchor{
				{
					eventStart: vppStart,
					eventEnd:   vppStart.Add(2 * time.Hour),
					deadline:   vppDeadline,
					mandatory:  true,
					vppSoc:     20.0,
				},
			},
		}

		timeline := []planInterval{
			{startTime: now, endTime: now.Add(time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 0.8, minSOC: 20.0},
			{startTime: now.Add(time.Hour), endTime: now.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.10, loadKWH: 0.8, minSOC: 20.0},
			{startTime: now.Add(2 * time.Hour), endTime: now.Add(3 * time.Hour), durationHours: 1.0, importRate: 0.15, loadKWH: 0.8, minSOC: 20.0},
			{startTime: now.Add(3 * time.Hour), endTime: now.Add(4 * time.Hour), durationHours: 1.0, importRate: 0.15, loadKWH: 0.8, minSOC: 20.0},
		}

		path, err := c.searchOptimalPlan(ctx, timeline, initSt, vppAnchors, sett, status, nil, nil)
		require.NoError(t, err)
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

		path, err := c.searchOptimalPlan(ctx, timeline, initSt, planningAnchors{}, sett, status, nil, nil)
		require.NoError(t, err)
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

		t.Logf("Decision: BatteryMode=%v, SolarMode=%v, Reason=%v, Desc=%s",
			decision.Action.BatteryMode, decision.Action.SolarMode, decision.Action.Reason, decision.Action.Description)
		for i, p := range plan.Periods {
			if i < 10 {
				t.Logf("Period %d: [%s - %s] Bat=%v Sol=%v Reason=%v",
					i, p.TSStart.Format("15:04"), p.TSEnd.Format("15:04"), p.BatteryMode, p.SolarMode, p.Reason)
			}
		}

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
		maxOvernightEndSOC := 0.0
		for _, p := range plan1900.Periods {
			if !p.TSStart.Before(t2200) && p.TSStart.Before(t0500Next) && p.BatteryMode == types.BatteryModeChargeAny {
				if p.EndSOC > maxOvernightEndSOC {
					maxOvernightEndSOC = p.EndSOC
				}
			}
		}
		assert.Greater(t, maxOvernightEndSOC, 50.0,
			"Super Off-Peak (22:00-05:00) must recharge well above the 40%% reserve target to cover morning peak")
		assert.Less(t, maxOvernightEndSOC, 65.0,
			"Super Off-Peak (22:00-05:00) must leave headroom for 09:00-13:00 free Off-Peak solar (+68%% SOC)")

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

		c := NewController()
		stepUpSettings := types.Settings{
			GridChargeBatteries: true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       60.0,
		}
		// Step 0 has minSOC = 60% at $0.12/kWh; Step 1 steps up to minSOC = 75% at the same $0.12/kWh import rate.
		// When currentSOC is 72% (above 60% current minSOC, but at/below the 75% upcoming minSOC + reserve buffer),
		// generateActionCandidates must offer BatteryModeStandby so the battery can hold its 72% SOC rather than
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
		status := types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         72.0,
		}
		cands := c.generateActionCandidates(context.Background(), 0, timeline[0], timeline, st, planningAnchors{}, stepUpSettings, status, nil, precedingAction{})
		foundStandby := false
		for _, cand := range cands {
			if cand.batteryMode == types.BatteryModeStandby {
				foundStandby = true
				break
			}
		}
		assert.True(t, foundStandby, "Must offer BatteryModeStandby when an upcoming interval has a higher minSOC at >= current importRate*effRT")
	})

	t.Run("MinArbitrageDifference_GatesArbitrageCandidateWithoutBlockingReserveCharge", func(t *testing.T) {
		t.Parallel()

		c := NewController()
		// Off-peak $0.10/kWh -> Peak $0.135/kWh: post-RTE spread is $0.135 - ($0.10/0.90) = $0.0239/kWh,
		// which exceeds balanced GridChargeDegradation ($0.02/kWh) but is below MinArbitrageDifference ($0.03/kWh).
		arbSettings := types.Settings{
			GridChargeBatteries:                 true,
			OptimizationProfile:                 "balanced",
			MinBatterySOC:                       20.0,
			MinArbitrageDifferenceDollarsPerKWH: 0.03,
		}
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.04, loadKWH: 0.5, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.135, exportRate: 0.04, loadKWH: 2.0, minSOC: 20.0},
		}
		// Above reserve (50% SOC): arbitrage candidate must be suppressed because spread ($0.0239) < $0.03
		aboveReserveState := planState{
			soc:            50.0,
			energyKWH:      0.50 * 13.5,
			capacityKWH:    13.5,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		candsAbove := c.generateActionCandidates(context.Background(), 0, timeline[0], timeline, aboveReserveState, planningAnchors{}, arbSettings, types.SystemStatus{BatteryCapacityKWH: 13.5, BatterySOC: 50.0}, nil, precedingAction{})
		for _, cand := range candsAbove {
			assert.NotEqual(t, types.BatteryModeChargeAny, cand.batteryMode,
				"Must not propose arbitrage ChargeAny candidate when spread is below MinArbitrageDifferenceDollarsPerKWH")
		}

		// Below reserve (10% SOC < 20% minSOC): reserve recovery candidate must still be offered
		belowReserveState := aboveReserveState
		belowReserveState.soc = 10.0
		belowReserveState.energyKWH = 0.10 * 13.5
		candsBelow := c.generateActionCandidates(context.Background(), 0, timeline[0], timeline, belowReserveState, planningAnchors{}, arbSettings, types.SystemStatus{BatteryCapacityKWH: 13.5, BatterySOC: 10.0}, nil, precedingAction{})
		foundReserveCharge := false
		for _, cand := range candsBelow {
			if cand.batteryMode == types.BatteryModeChargeAny && cand.actionName == PlanActionReserveTargetCharge {
				foundReserveCharge = true
				break
			}
		}
		assert.True(t, foundReserveCharge, "Must still offer ReserveTargetCharge when below minSOC regardless of MinArbitrageDifferenceDollarsPerKWH")
	})

	t.Run("EversourceRate7_SolarChargesOffPeakAndExportsExcessAtEndOfPeakToReserveFloor", func(t *testing.T) {
		t.Parallel()

		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Eversource CT Rate 7:
		// - Off-Peak (20:00 - 12:00): $0.20754/kWh import, $0.20254/kWh export, MinSOC = 50%
		// - On-Peak (12:00 - 20:00): $0.29982/kWh import, $0.29482/kWh export, MinSOC = 25%
		// Grid-charging at Off-Peak to export at On-Peak has a post-RTE spread of
		// $0.29482 - ($0.20754 / 0.90) = $0.06422/kWh, which is below MinBatteryExportDifference = $0.07/kWh.
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
			MinBatteryExportDifferenceDollarsPerKWH: 0.07,
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
			"At 11:00 AM with 68%% SOC, must not grid-charge for battery export when spread ($0.0642/kWh) < MinBatteryExportDifference ($0.07/kWh)")

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

func TestExportFloorSOC(t *testing.T) {
	t.Parallel()

	capacityKWH := 13.5
	effRT := 0.90
	oneWayEff := math.Sqrt(effRT)
	gridChargePerKWH := 0.02
	state := planState{
		capacityKWH:    capacityKWH,
		maxChargeKW:    5.0,
		maxDischargeKW: 5.0,
	}
	settings := types.Settings{
		GridChargeBatteries: true,
		GridExportSolar:     false,
		GridExportBatteries: true,
	}

	t.Run("PropagatesReserveStepUpAndExpensiveLoadUntilGridRefill", func(t *testing.T) {
		t.Parallel()

		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.314, exportRate: 0.094, loadKWH: 0.5, minSOC: 5.0},
			{durationHours: 2.0, importRate: 0.105, exportRate: 0.024, loadKWH: 2.0, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.055, exportRate: 0.024, loadKWH: 1.0, minSOC: 40.0},
			// Later solar interval after gridRefillIdx must not lower refillCost below the 22:00 grid refill rate
			{durationHours: 2.0, importRate: 0.105, exportRate: 0.024, solarKWH: 8.0, loadKWH: 0.5, minSOC: 20.0},
		}
		reqSOC, peakImportRate, refillCost := exportFloorSOC(0, timeline, state, settings, 0.0)
		expectedLoadSOC := ((2.0 / oneWayEff) / capacityKWH) * 100.0
		assert.InDelta(t, 20.0+expectedLoadSOC, reqSOC, 0.01)
		assert.InDelta(t, 0.314, peakImportRate, 0.001)
		assert.InDelta(t, 0.055/effRT, refillCost, 0.001, "refillCost must reflect the gridRefillIdx import rate, not post-refill solar")
	})

	t.Run("SubtractsCheapSolarWithGridChargeWearAndIgnoresExpensiveSolar", func(t *testing.T) {
		t.Parallel()

		solarSettings := settings
		solarSettings.GridExportSolar = true
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.30, exportRate: 0.16, minSOC: 10.0},
			// Step 1: solar export ($0.11/kWh -> $0.11/0.90 + $0.02 gridChargeWear + $0.03 cycleHurdle = $0.1722 > $0.16),
			//         would pass without gridChargePerKWH ($0.1522 <= $0.16), must NOT reduce reqSOC
			{durationHours: 1.0, importRate: 0.25, exportRate: 0.11, solarKWH: 3.0, loadKWH: 0.0, minSOC: 10.0},
			// Step 2: cheap solar export ($0.02/kWh -> $0.02/0.90 + $0.02 + $0.03 = $0.0722 <= $0.16), reduces reqSOC
			{durationHours: 1.0, importRate: 0.25, exportRate: 0.02, solarKWH: 2.0, loadKWH: 0.0, minSOC: 10.0},
			// Step 3: expensive load requiring 50% SOC
			{durationHours: 1.0, importRate: 0.30, exportRate: 0.02, loadKWH: (0.40 * capacityKWH) * oneWayEff, minSOC: 10.0},
		}
		reqSOC, _, refillCost := exportFloorSOC(0, timeline, state, solarSettings, 0.30)
		expectedSolarGainSOC := ((2.0 * oneWayEff) / capacityKWH) * 100.0
		assert.InDelta(t, 50.0-expectedSolarGainSOC, reqSOC, 0.05)
		assert.InDelta(t, (0.02/effRT)+gridChargePerKWH, refillCost, 0.001, "solar refillCost must include gridChargePerKWH")
	})

	t.Run("GridChargeDisabled_SkipsCheapLoadAndScansThroughToMorningPeakAndSolarRefill", func(t *testing.T) {
		t.Parallel()

		noGridChargeSettings := settings
		noGridChargeSettings.GridChargeBatteries = false
		noGridChargeSettings.GridExportSolar = true

		// Step 0: Evening peak export ($0.30/kWh)
		// Step 1: Cheap overnight off-peak ($0.10/kWh import, 2.5 kWh load) -> skipped because exporting at $0.30 beats importing at $0.10,
		//         but must NOT stop the scan since the battery cannot recharge from the grid at Step 1!
		// Step 2: Next morning expensive peak ($0.40/kWh import, 2.0 kWh load) -> must be preserved in reqSOC!
		// Step 3: Midday cheap solar refill (6.0 kWh surplus solar at $0.02/kWh) -> offsets Step 4 afternoon load.
		// Step 4: Next afternoon expensive peak ($0.40/kWh import, 2.0 kWh load) -> covered by Step 3 solar.
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.40, exportRate: 0.30, loadKWH: 0.5, minSOC: 20.0},
			{durationHours: 4.0, importRate: 0.10, exportRate: 0.02, loadKWH: 2.5, minSOC: 20.0},
			{durationHours: 2.0, importRate: 0.40, exportRate: 0.15, loadKWH: 2.0, minSOC: 20.0},
			{durationHours: 3.0, importRate: 0.15, exportRate: 0.02, solarKWH: 6.0, loadKWH: 0.0, minSOC: 20.0},
			{durationHours: 2.0, importRate: 0.40, exportRate: 0.15, loadKWH: 2.0, minSOC: 20.0},
		}
		reqSOC, peakImportRate, refillCost := exportFloorSOC(0, timeline, state, noGridChargeSettings, 0.15)
		expectedMorningPeakSOC := ((2.0 / oneWayEff) / capacityKWH) * 100.0
		assert.InDelta(t, 20.0+expectedMorningPeakSOC, reqSOC, 0.01,
			"When grid charging is disabled, exportFloorSOC must skip cheap Step 1 load but keep scanning to cover Step 2 morning peak load")
		assert.InDelta(t, 0.40, peakImportRate, 0.001)
		assert.InDelta(t, (0.02/effRT)+gridChargePerKWH, refillCost, 0.001)
	})

	t.Run("HighExportRate_PreservesReserveStepUpFloorWithoutFullDrainShortcut", func(t *testing.T) {
		t.Parallel()

		c := NewController()
		exportSettings := types.Settings{
			ManageTOUSchedules:  true,
			GridChargeBatteries: false,
			GridExportBatteries: true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       5.0,
		}
		// Step 0 has minSOC = 5% and high exportRate ($0.50/kWh >= peakImportRate/effRT + cycleHurdle),
		// while Step 1 has a reserve step-up to minSOC = 20% before Step 2's solar refill.
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.25, exportRate: 0.50, loadKWH: 0.0, minSOC: 5.0},
			{durationHours: 1.0, importRate: 0.25, exportRate: 0.10, loadKWH: 0.0, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.02, solarKWH: 5.0, loadKWH: 0.0, minSOC: 20.0},
		}
		st := planState{
			soc:            60.0,
			energyKWH:      0.60 * capacityKWH,
			capacityKWH:    capacityKWH,
			maxChargeKW:    5.0,
			maxDischargeKW: 5.0,
		}
		status := types.SystemStatus{
			BatteryCapacityKWH: capacityKWH,
			BatterySOC:         60.0,
		}
		cands := c.generateActionCandidates(context.Background(), 0, timeline[0], timeline, st, planningAnchors{}, exportSettings, status, nil, precedingAction{})
		var exportCand *actionCandidate
		for i := range cands {
			if cands[i].batteryMode == types.BatteryModeExport {
				exportCand = &cands[i]
				break
			}
		}
		if assert.NotNil(t, exportCand, "Expected BatteryModeExport candidate") {
			assert.Equal(t, 20.0, exportCand.targetSOC,
				"BatteryModeExport targetSOC must respect upcoming 20%% reserve step-up in reqSOC even when exportRate beats peakImportRate/effRT + cycleHurdle")
		}
	})

	t.Run("HighExportSolarInterval_ReservesHomeLoadForDirectSolarExport", func(t *testing.T) {
		t.Parallel()

		// Step 0 is an export interval ($0.155/kWh export).
		// Step 1 is a high-export solar hour ($0.150/kWh import, $0.145/kWh export) with solarKWH=2.0 > loadKWH=1.0.
		// Step 2 is an evening peak hour ($0.155/kWh import, $0.05/kWh export) with loadKWH=2.0 and no solar.
		// Step 2 is reserved because 0.155*0.90 + 0.02 = $0.1595 > $0.155 (setting minReservedImportRate = $0.155).
		// In Step 1, solarReplacementCost + cycleHurdle ($0.145/0.90 + $0.02 = $0.1811) > Step 0 exportRate ($0.155),
		// AND because GridExportSolar=true and Step 1 exportRate ($0.145) >= minReservedImportRate * effRT ($0.155 * 0.90 = $0.1395),
		// the planner will run DirectSolarExport (Bat=-1 Sol=3) in Step 1, exporting all 2.0 kWh of solar and discharging
		// the battery to cover Step 1's 1.0 kWh home load.
		// Therefore, exportFloorSOC at Step 0 must reserve battery energy for BOTH Step 2's 2.0 kWh load AND Step 1's 1.0 kWh load
		// (3.0 kWh / sqrt(0.90) = 3.162 kWh = +31.6% above the 20% reserve -> ~51.6% floor), rather than assuming Step 1's solar
		// covers Step 1's load for free.
		solarExportSettings := types.Settings{
			ManageTOUSchedules:  true,
			GridChargeBatteries: true,
			GridExportBatteries: true,
			GridExportSolar:     true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.160, exportRate: 0.155, loadKWH: 0.5, solarKWH: 0.0, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.150, exportRate: 0.145, loadKWH: 1.0, solarKWH: 2.0, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.155, exportRate: 0.050, loadKWH: 2.0, solarKWH: 0.0, minSOC: 20.0},
		}
		floorWithSolarExport, _, _ := exportFloorSOC(0, timeline, state, solarExportSettings, 0.15)
		expectedFloor := 20.0 + (((2.0+1.0)/oneWayEff)/capacityKWH)*100.0
		assert.InDelta(t, expectedFloor, floorWithSolarExport, 0.2,
			"exportFloorSOC must reserve Step 1 home load when GridExportSolar will export all Step 1 solar via DirectSolarExport")

		// Contrast: when Step 1's exportRate ($0.130) is below minReservedImportRate * effRT ($0.1395)
		// (while still above the cheap-solar refill threshold vs Step 0), SolarModeAny covers Step 1's load
		// behind the meter so only Step 2's 2.0 kWh load is reserved.
		moderateSolarTimeline := []planInterval{
			timeline[0],
			{durationHours: 1.0, importRate: 0.150, exportRate: 0.130, loadKWH: 1.0, solarKWH: 2.0, minSOC: 20.0},
			timeline[2],
		}
		floorModerateSolar, _, _ := exportFloorSOC(0, moderateSolarTimeline, state, solarExportSettings, 0.15)
		expectedFloorModerate := 20.0 + ((2.0/oneWayEff)/capacityKWH)*100.0
		assert.InDelta(t, expectedFloorModerate, floorModerateSolar, 0.2,
			"When Step 1 exportRate < minReservedImportRate*effRT, Step 1 solar covers Step 1 load behind the meter and only Step 2 load is reserved")
	})

	t.Run("ReserveDeficitCheaperLookahead_CapsWaitAt40MinutesAndChecksWindowStart", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		c := NewController()
		chiLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// Subcase A: Descending prices every 20 minutes ($0.060 -> $0.050 -> $0.040 -> $0.030).
		// Without a cumulative deficit wait cap, hasCheaperImmediateNextStep would allow the battery to stay
		// in deficit across all 3 steps (60 minutes). With the 40-minute (chargeCompletionBufferDuration) cap,
		// the plan can wait at most 2 steps (40 minutes) before being forced to start charging in step 2.
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
			{index: 0, startTime: t0100, endTime: t0100.Add(20 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.160, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.160}},
			{index: 1, startTime: t0100.Add(20 * time.Minute), endTime: t0100.Add(40 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.120, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.120}},
			{index: 2, startTime: t0100.Add(40 * time.Minute), endTime: t0100.Add(60 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.080, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.080}},
			{index: 3, startTime: t0100.Add(60 * time.Minute), endTime: t0100.Add(80 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.040, exportRate: 0.020, loadKWH: 0.3, minSOC: 50.0, price: types.Price{TSStart: t0100.Add(time.Hour), TSEnd: t0100.Add(2 * time.Hour), DollarsPerKWH: 0.040}},
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
		anchors := c.detectPlanningAnchors(timeline, nil, status0100, settings, nil)
		pathAtStart, err := c.searchOptimalPlan(ctx, timeline, initial, anchors, settings, status0100, nil, nil)
		require.NoError(t, err)
		if assert.Len(t, pathAtStart.actions, 4) {
			assert.Equal(t, types.BatteryModeChargeAny, pathAtStart.actions[2].batteryMode,
				"After waiting 40 minutes (steps 0 and 1) for a cheaper rate, step 2 must begin recharging even if step 3 is cheaper still")
		}

		// Subcase B: Reserve window started at 01:00, and current time is 01:45 (45 minutes into the reserve window)
		// without having charged yet. Even though 02:00 is cheaper ($0.040 vs $0.060), initialDeficitWaitHours detects
		// that the reserve window started 45 minutes ago (>= 40m cap) and forces immediate charging at step 0.
		t0145 := time.Date(2026, 10, 20, 1, 45, 0, 0, chiLoc)
		timeline0145 := []planInterval{
			{index: 0, startTime: t0145, endTime: t0145.Add(15 * time.Minute), durationHours: 15.0 / 60.0, importRate: 0.060, exportRate: 0.020, loadKWH: 0.25, minSOC: 50.0, price: types.Price{TSStart: t0100, TSEnd: t0100.Add(time.Hour), DollarsPerKWH: 0.060}},
			{index: 1, startTime: t0145.Add(15 * time.Minute), endTime: t0145.Add(35 * time.Minute), durationHours: 20.0 / 60.0, importRate: 0.040, exportRate: 0.020, loadKWH: 0.30, minSOC: 50.0, price: types.Price{TSStart: t0100.Add(time.Hour), TSEnd: t0100.Add(2 * time.Hour), DollarsPerKWH: 0.040}},
		}
		initial0145 := initial
		initial0145.time = t0145
		status0145 := status0100
		status0145.Timestamp = t0145
		pathAt0145, err := c.searchOptimalPlan(ctx, timeline0145, initial0145, anchors, settings, status0145, nil, nil)
		require.NoError(t, err)
		if assert.NotEmpty(t, pathAt0145.actions) {
			assert.Equal(t, types.BatteryModeChargeAny, pathAt0145.actions[0].batteryMode,
				"When the reserve window started 45 minutes ago (>= 40m cap), step 0 must charge immediately rather than delaying again for next hour's cheaper rate")
		}
	})

	t.Run("ExtendedPeakSurvival_IncludesContiguousArbitrageProfitablePeriodsDuringCheapPeriod", func(t *testing.T) {
		t.Parallel()

		c := NewController()
		chiLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		t1200 := time.Date(2026, 9, 25, 12, 0, 0, 0, chiLoc)

		// Cheap period at step 0 ($0.055/kWh), followed by afternoon elevated block ($0.115-$0.130/kWh)
		// and evening shoulder ($0.100/kWh) before returning to $0.060/kWh overnight.
		// Even though maxImport - minImport = $0.130 - $0.055 = $0.075 < $0.10 (minPeakRateSpreadDollars),
		// the 14:00-21:00 block clears the arbitrage hurdle ($0.055/0.90 + $0.03 = $0.0911/kWh) over step 0's charge price.
		settings := types.Settings{
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
			GridChargeBatteries: true,
		}
		timeline := []planInterval{
			{index: 0, startTime: t1200, endTime: t1200.Add(2 * time.Hour), durationHours: 2.0, importRate: 0.055, loadKWH: 2.0, minSOC: 20.0},
			{index: 1, startTime: t1200.Add(2 * time.Hour), endTime: t1200.Add(7 * time.Hour), durationHours: 5.0, importRate: 0.130, loadKWH: 6.0, minSOC: 20.0},
			{index: 2, startTime: t1200.Add(7 * time.Hour), endTime: t1200.Add(9 * time.Hour), durationHours: 2.0, importRate: 0.100, loadKWH: 2.4, minSOC: 20.0},
			{index: 3, startTime: t1200.Add(9 * time.Hour), endTime: t1200.Add(12 * time.Hour), durationHours: 3.0, importRate: 0.060, loadKWH: 3.0, minSOC: 20.0},
		}
		status := types.SystemStatus{
			Timestamp:          t1200,
			TimeLocation:       "America/Chicago",
			BatteryCapacityKWH: 13.5,
			BatterySOC:         30.0,
		}
		anchors := c.detectPlanningAnchors(timeline, nil, status, settings, nil)
		if assert.Len(t, anchors.peakWindows, 1, "Must detect contiguous arbitrage-profitable elevated block during cheap period") {
			assert.Equal(t, 1, anchors.peakWindows[0].startIndex)
			assert.Equal(t, 2, anchors.peakWindows[0].endIndex, "Peak survival window must extend across the $0.100/kWh shoulder at index 2")
			assert.Greater(t, anchors.peakWindows[0].bufferEnergyKWH, 0.0)
		}
	})
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
		// - exportLoadHurdle = $0.03 (gating already-stored battery energy exported vs off-peak load / solar refill)
		// - batteryExportPerKWH = $0.03 - $0.02 = $0.01 (physical export-half wear on BatteryModeExport)
		assert.InDelta(t, 0.07, wear.exportGatePerKWH, 1e-6)
		assert.InDelta(t, 0.03, wear.exportLoadHurdle, 1e-6)
		assert.InDelta(t, 0.005, wear.solarExportPerKWH, 1e-6)
		assert.InDelta(t, 0.02, wear.gridChargePerKWH, 1e-6)
		assert.InDelta(t, 0.01, wear.batteryExportPerKWH, 1e-6)

		cand := actionCandidate{batteryMode: types.BatteryModeExport, solarMode: types.SolarModeAny}
		metrics := intervalMetrics{batExportKWH: 4.0}
		cost := stepWearCostDollars(cand, 80.0, 50.0, 20.0, 0.0, metrics, roundTripEff, wear)
		assert.InDelta(t, 4.0*0.01, cost, 1e-6)

		// Grid charging above loadNeededSOC (for battery export) pays exportGatePerKWH - batteryExportPerKWH ($0.06/kWh)
		// so that a full grid-charge-to-export cycle pays $0.06 + $0.01 = $0.07/kWh (exportGatePerKWH).
		chargeCand := actionCandidate{batteryMode: types.BatteryModeChargeAny}
		exportChargeCost := stepWearCostDollars(chargeCand, 60.0, 90.0, 20.0, 60.0, intervalMetrics{gridChargeKWH: 4.0}, roundTripEff, wear)
		assert.InDelta(t, 4.0*roundTripEff*(wear.exportGatePerKWH-wear.batteryExportPerKWH), exportChargeCost, 1e-6)

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

	t.Run("GridCharge_ReservePaysPhysicalWearAndArbitrageUsesArbitrageGate", func(t *testing.T) {
		t.Parallel()

		cand := actionCandidate{batteryMode: types.BatteryModeChargeAny}
		reserveAC := 2.70 / oneWayEff

		// Arbitrage charging strictly above minSOC pays roundTripEff * arbitrageGatePerKWH
		arbSettings := types.Settings{
			OptimizationProfile:                 "balanced",
			MinArbitrageDifferenceDollarsPerKWH: 0.05,
		}
		arbWear := getWearRates(arbSettings, arbSettings.GetOptimizationParams())
		assert.InDelta(t, 0.02, arbWear.gridChargePerKWH, 1e-6)
		assert.InDelta(t, 0.05, arbWear.arbitrageGatePerKWH, 1e-6)

		// Charging only up to minSOC (reserve recovery) pays physical gridChargePerKWH ($0.02), not arbitrageGatePerKWH ($0.05)
		costReserve := stepWearCostDollars(cand, 10.0, 20.0, 20.0, 0.0, intervalMetrics{gridChargeKWH: reserveAC}, roundTripEff, arbWear)
		assert.InDelta(t, reserveAC*roundTripEff*arbWear.gridChargePerKWH, costReserve, 1e-6,
			"Reserve grid charging up to minSOC must incur physical gridChargePerKWH wear ( preventing free drain-and-recharge churn) without paying the user's arbitrage hurdle")

		costArb := stepWearCostDollars(cand, 20.0, 50.0, 20.0, 0.0, intervalMetrics{gridChargeKWH: reserveAC}, roundTripEff, arbWear)
		assert.InDelta(t, reserveAC*roundTripEff*arbWear.arbitrageGatePerKWH, costArb, 1e-6,
			"Arbitrage grid charging above minSOC must pay roundTripEff * arbitrageGatePerKWH")

		// Crossing minSOC (10% -> 30% with minSOC=20%) blends gridChargePerKWH on the 50% reserve portion and arbitrageGatePerKWH on the 50% arbitrage portion
		costCross := stepWearCostDollars(cand, 10.0, 30.0, 20.0, 0.0, intervalMetrics{gridChargeKWH: reserveAC}, roundTripEff, arbWear)
		expectedBlend := 0.5*arbWear.gridChargePerKWH + 0.5*arbWear.arbitrageGatePerKWH
		assert.InDelta(t, reserveAC*roundTripEff*expectedBlend, costCross, 1e-6,
			"Grid charging across minSOC must charge gridChargePerKWH on the reserve fraction and arbitrageGatePerKWH on the fraction above minSOC")
	})
}

func TestStepTransitionCost(t *testing.T) {
	t.Parallel()

	t.Run("Step0_StandbyToLoadOrCharge_PaysOnlyModeSwitchPenalty", func(t *testing.T) {
		t.Parallel()

		lastPrec := precedingAction{
			BatteryMode: types.BatteryModeStandby,
			SolarMode:   types.SolarModeAny,
			ImportRate:  0.10,
			ExportRate:  0.04,
		}
		interval := planInterval{durationHours: 1.0, importRate: 0.10, exportRate: 0.04}

		costToLoad := stepTransitionCost(0, actionCandidate{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeAny}, lastPrec, interval, false)
		assert.InDelta(t, modeSwitchPenalty, costToLoad, 1e-6)

		costToCharge := stepTransitionCost(0, actionCandidate{batteryMode: types.BatteryModeChargeAny, solarMode: types.SolarModeAny}, lastPrec, interval, false)
		assert.InDelta(t, modeSwitchPenalty, costToCharge, 1e-6)

		// Load -> Standby at same price pays full inertia threshold
		lastLoadPrec := lastPrec
		lastLoadPrec.BatteryMode = types.BatteryModeLoad
		costLoadToStandby := stepTransitionCost(0, actionCandidate{batteryMode: types.BatteryModeStandby, solarMode: types.SolarModeAny}, lastLoadPrec, interval, false)
		assert.InDelta(t, defaultInertiaThresholdDollars, costLoadToStandby, 1e-6)
	})

	t.Run("RolloutStep_LoadToExportAndPrePeakChargeToStandby_Waived", func(t *testing.T) {
		t.Parallel()

		interval := planInterval{durationHours: 20.0 / 60.0, importRate: 0.30, exportRate: 0.15}
		prevInterval := interval

		costLoadToExport := stepTransitionCost(1, actionCandidate{batteryMode: types.BatteryModeExport, solarMode: types.SolarModeAny}, toPrecedingAction(actionCandidate{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeAny}, prevInterval), interval, false)
		assert.InDelta(t, 0.0, costLoadToExport, 1e-6, "Transitioning from Load to Export within a peak window must not incur modeSwitchPenalty")

		costDirectSolarToExport := stepTransitionCost(1, actionCandidate{batteryMode: types.BatteryModeExport, solarMode: types.SolarModeAny}, toPrecedingAction(actionCandidate{batteryMode: types.BatteryModeLoad, solarMode: types.SolarModeExport}, prevInterval), interval, false)
		assert.InDelta(t, 0.0, costDirectSolarToExport, 1e-6, "Transitioning from DirectSolarExport to BatteryModeExport within a peak window must not incur solarModeSwitchPenalty")

		costPrePeakRest := stepTransitionCost(1, actionCandidate{batteryMode: types.BatteryModeStandby}, toPrecedingAction(actionCandidate{batteryMode: types.BatteryModeChargeAny}, prevInterval), interval, true)
		assert.InDelta(t, 0.0, costPrePeakRest, 1e-6, "Resting in Standby after completing a charge in the pre-peak buffer must not incur modeSwitchPenalty")
	})
}

func TestHorizonTargetSOC(t *testing.T) {
	t.Parallel()

	capacityKWH := 13.5
	effRT := 0.90
	oneWayEff := math.Sqrt(effRT)
	state := planState{
		capacityKWH:    capacityKWH,
		maxChargeKW:    5.0,
		maxDischargeKW: 5.0,
	}

	t.Run("LoadAndBatteryExportSeparation", func(t *testing.T) {
		t.Parallel()

		settings := types.Settings{
			GridChargeBatteries: true,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.04, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.35, exportRate: 0.25, loadKWH: 2.0, minSOC: 20.0},
		}
		reqSOC, peakImportRate, _, loadReqSOC := horizonTargetSOC(0, timeline, state, planningAnchors{}, settings, 0.10, true)
		expectedLoadSOC := 20.0 + ((2.0/oneWayEff)/capacityKWH)*100.0
		assert.InDelta(t, 100.0, reqSOC, 1e-4, "reqSOC must target 100% when a future interval is a profitable battery export step")
		assert.InDelta(t, expectedLoadSOC, loadReqSOC, 1e-4, "loadReqSOC must remain restricted to home-load and reserve coverage")
		assert.InDelta(t, 0.35, peakImportRate, 1e-4)
	})

	t.Run("PostPeakTailAndCheaperIntervalSubtraction", func(t *testing.T) {
		t.Parallel()

		settings := types.Settings{
			GridChargeBatteries: true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		// Post-horizon rate ($0.40) seeds end-of-horizon reqSOC at 100%, but Steps 2 and 3 are post-peak
		// tail intervals at the same $0.10/kWh rate (i > lastExpensiveIdx=1) that subtract their gridGainSOC.
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.40, loadKWH: 0.20 * capacityKWH * oneWayEff, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.10, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.10, minSOC: 20.0},
		}
		anchors := planningAnchors{knownPostHorizonRate: 0.40}
		reqSOC, _, _, loadReqSOC := horizonTargetSOC(0, timeline, state, anchors, settings, 0.10, true)
		gridGainPerHourSOC := ((5.0 * 1.0 * oneWayEff) / capacityKWH) * 100.0
		expectedAfterTail := max(20.0, 100.0-2.0*gridGainPerHourSOC)
		expectedAtStep0 := expectedAfterTail + 20.0
		assert.InDelta(t, expectedAtStep0, reqSOC, 0.05, "Post-peak tail intervals at the same rate must subtract their charging capacity from the post-horizon target")
		assert.InDelta(t, expectedAtStep0, loadReqSOC, 0.05)

		// Strictly cheaper future interval (Step 1 at $0.05 < $0.10 - $0.03) before Step 2's peak (requiring 60% SOC)
		// subtracts Step 1's gridGainSOC from the required SOC at Step 0.
		cheaperAheadTimeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.05, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.40, loadKWH: 0.40 * capacityKWH * oneWayEff, minSOC: 20.0},
		}
		reqCheaper, _, _, _ := horizonTargetSOC(0, cheaperAheadTimeline, state, planningAnchors{}, settings, 0.05, true)
		expectedCheaper := max(20.0, 60.0-gridGainPerHourSOC)
		assert.InDelta(t, expectedCheaper, reqCheaper, 0.05, "Strictly cheaper future charging interval must subtract its charging capacity from Step 0 reqSOC")
	})

	t.Run("CheapImportWithProfitableExport_AdvancesLastExpensiveIdx", func(t *testing.T) {
		t.Parallel()

		settings := types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		// All three intervals have cheap $0.10/kWh import rates, but Step 2 has a lucrative $0.25/kWh solar export rate
		// with surplus solar (hasSolarExport == true). Because Step 2 advances lastExpensiveIdx to 2, Step 1 (i=1 < 2)
		// must NOT be treated as a post-peak tail interval that subtracts gridGainSOC.
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.04, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.04, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.25, solarKWH: 4.0, loadKWH: 0.20 * capacityKWH * oneWayEff, minSOC: 20.0},
		}
		reqSOC, _, _, loadReqSOC := horizonTargetSOC(0, timeline, state, planningAnchors{}, settings, 0.10, true)
		assert.InDelta(t, 40.0, reqSOC, 0.05, "Profitable solar export interval at cheap import rate must advance lastExpensiveIdx so Step 1 does not subtract gridGainSOC")
		assert.InDelta(t, 40.0, loadReqSOC, 0.05)
	})

	t.Run("PeakSurvivalBuffer_IncludedWhenProfitable", func(t *testing.T) {
		t.Parallel()

		settings := types.Settings{
			GridChargeBatteries: true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.08, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.35, loadKWH: 0.20 * capacityKWH * oneWayEff, minSOC: 20.0},
		}
		anchors := planningAnchors{
			knownPostHorizonRate: 0.10,
			peakWindows: []peakWindowAnchor{
				{startIndex: 1, endIndex: 1, bufferEnergyKWH: 0.10 * capacityKWH},
			},
		}
		reqSOC, _, _, loadReqSOC := horizonTargetSOC(0, timeline, state, anchors, settings, 0.08, true)
		// At pw.endIndex=1, stepFloorSOC = 20% + 10% buffer = 30%, plus 20% peak load = 50%
		assert.InDelta(t, 50.0, reqSOC, 0.05)
		assert.InDelta(t, 50.0, loadReqSOC, 0.05)
	})
}

func TestComputeStepExportFloor(t *testing.T) {
	t.Parallel()

	capacityKWH := 13.5
	effRT := 0.90
	oneWayEff := math.Sqrt(effRT)
	state := planState{
		capacityKWH:    capacityKWH,
		maxChargeKW:    5.0,
		maxDischargeKW: 5.0,
	}

	t.Run("MinAlternativeValue_And_ChargeTargetSOCClamping", func(t *testing.T) {
		t.Parallel()

		settings := types.Settings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: true,
			ManageTOUSchedules:  true,
			OptimizationProfile: "balanced",
			MinBatterySOC:       20.0,
		}
		// Step 0 ($0.10 import, $0.20 export) -> Step 1 ($0.18 import, surplus solar at $0.04 export) -> Step 2 (peak $0.35 import with 30% load).
		timeline := []planInterval{
			{durationHours: 1.0, importRate: 0.10, exportRate: 0.20, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.18, exportRate: 0.04, solarKWH: 2.0, loadKWH: 1.0, minSOC: 20.0},
			{durationHours: 1.0, importRate: 0.35, exportRate: 0.04, loadKWH: 0.30 * capacityKWH * oneWayEff, minSOC: 20.0},
		}
		anchors := planningAnchors{knownPostHorizonRate: 0.11}
		floor := computeStepExportFloor(0, timeline[0], timeline, state, anchors, settings)

		// minAlternativeValue must pick Step 1's $0.04 solar export rate (below $0.10 import and $0.11 post-horizon)
		assert.InDelta(t, 0.04, floor.minAlternativeValue, 1e-6)
		// neededSOC is 20% + 30% peak load - Step 1's 1.0 kWh net solar gain (~7.03%) = ~42.97%, strictly between 20% and 100%
		expectedNeeded := 50.0 - ((1.0*oneWayEff)/capacityKWH)*100.0
		assert.InDelta(t, expectedNeeded, floor.neededSOC, 0.05)
		assert.InDelta(t, expectedNeeded, floor.chargeTargetSOC, 0.05, "chargeTargetSOC must equal neededSOC when strictly between minSOC and 100%")
		assert.Greater(t, floor.reqSOC, 20.0, "reqSOC must be populated when ManageTOUSchedules and GridExportBatteries are enabled")

		// When GridExportSolar is false, surplus solar refill cost is $0.00
		noSolarExportSettings := settings
		noSolarExportSettings.GridExportSolar = false
		floorNoSolarExport := computeStepExportFloor(0, timeline[0], timeline, state, anchors, noSolarExportSettings)
		assert.InDelta(t, 0.0, floorNoSolarExport.minAlternativeValue, 1e-6)

		// When neededSOC reaches 100% (e.g. high post-horizon rate at Step 2 with no downstream solar), chargeTargetSOC remains 0 (unconstrained)
		floorFull := computeStepExportFloor(2, timeline[2], timeline, state, planningAnchors{knownPostHorizonRate: 0.50}, settings)
		assert.InDelta(t, 100.0, floorFull.neededSOC, 1e-4)
		assert.Equal(t, 0.0, floorFull.chargeTargetSOC, "chargeTargetSOC must remain 0 when neededSOC is 100%")
	})
}

func TestStepPenaltyCost(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	optParams := (types.Settings{OptimizationProfile: "balanced"}).GetOptimizationParams()
	oneWayEff := math.Sqrt(optParams.RoundTripEfficiency)
	capacityKWH := 13.5

	t.Run("CombinesLateChargeEarlyExportPeakBufferAndHoldingPenalties", func(t *testing.T) {
		t.Parallel()

		interval := planInterval{
			startTime:     now,
			endTime:       now.Add(time.Hour),
			durationHours: 1.0,
			minSOC:        20.0,
		}
		anchors := planningAnchors{
			horizonEnd: now.Add(5 * time.Hour),
			peakWindows: []peakWindowAnchor{
				{startIndex: 0, endIndex: 0, bufferEnergyKWH: 1.35}, // Target at endIndex=0: 2.7 + 1.35 = 4.05 kWh (30% SOC)
			},
		}

		// 1. Pre-peak charge completion penalty + holding cost
		chargeCand := actionCandidate{batteryMode: types.BatteryModeChargeAny}
		chargeNextState := planState{energyKWH: 6.0, capacityKWH: capacityKWH}
		chargeMetrics := intervalMetrics{gridChargeKWH: 3.0}
		costCharge := stepPenaltyCost(1, chargeCand, chargeNextState, chargeMetrics, interval, true, anchors, optParams)
		expectedLateCharge := 3.0 * oneWayEff * lateChargeBufferPenaltyPerKWH
		expectedHolding := 6.0 * 1.0 * batteryHoldingCostPerHourPerKWH
		assert.InDelta(t, expectedLateCharge+expectedHolding, costCharge, 1e-7)

		// 2. Early export penalty + peak survival buffer shortfall penalty + holding cost at pw.endIndex (stepIdx=0)
		exportCand := actionCandidate{batteryMode: types.BatteryModeExport}
		// Ending at 2.7 kWh (20% SOC) leaves a 1.35 kWh shortfall below the 4.05 kWh peak survival target
		exportNextState := planState{energyKWH: 2.7, capacityKWH: capacityKWH}
		exportMetrics := intervalMetrics{batExportKWH: 2.0, batSuppliedHomeKWH: 1.0, solarExportKWH: 1.5}
		costExport := stepPenaltyCost(0, exportCand, exportNextState, exportMetrics, interval, false, anchors, optParams)
		exportedDC := (2.0 + 1.0) / oneWayEff
		expectedEarlyExport := exportedDC * 4.0 * (1.1 * batteryHoldingCostPerHourPerKWH)
		expectedPeakShortfall := 1.35 * peakSurvivalBufferShortfallPenaltyDollarsPerKWH
		expectedExportHolding := 2.7 * 1.0 * batteryHoldingCostPerHourPerKWH
		assert.InDelta(t, expectedEarlyExport+expectedPeakShortfall+expectedExportHolding, costExport, 1e-7)
	})
}

func TestCalculateStartChargeHeadroomKWH(t *testing.T) {
	t.Parallel()

	t.Run("HonorsFloorCapacityPctAndMinStartChargeMinutes", func(t *testing.T) {
		t.Parallel()

		// Small battery (10 kWh, 4% = 0.4 kWh) with 0 MinStartChargeMinutes -> floors at minStartChargeFloorKWH (0.6 kWh)
		assert.InDelta(t, minStartChargeFloorKWH, calculateStartChargeHeadroomKWH(10.0, 5.0, types.Settings{}), 1e-6)

		// Large battery (30 kWh, 4% = 1.2 kWh > 0.6 kWh floor) with 0 MinStartChargeMinutes -> uses 4% capacity (1.2 kWh)
		assert.InDelta(t, 1.2, calculateStartChargeHeadroomKWH(30.0, 5.0, types.Settings{}), 1e-6)

		// MinStartChargeMinutes = 15 (0.25h) at 8.0 kW maxChargeKW -> 2.0 kWh (> 4% of 13.5 kWh)
		assert.InDelta(t, 2.0, calculateStartChargeHeadroomKWH(13.5, 8.0, types.Settings{MinStartChargeMinutes: 15}), 1e-6)

		// Fallback power when maxChargeKW is 0: uses defaultBatteryPowerSmallKW (5.0 kW) for <20 kWh and defaultBatteryPowerLargeKW (10.0 kW) for >=20 kWh
		assert.InDelta(t, 5.0*0.5, calculateStartChargeHeadroomKWH(13.5, 0.0, types.Settings{MinStartChargeMinutes: 30}), 1e-6)
		assert.InDelta(t, 10.0*0.5, calculateStartChargeHeadroomKWH(27.0, 0.0, types.Settings{MinStartChargeMinutes: 30}), 1e-6)
	})
}

func TestToPrecedingAction(t *testing.T) {
	t.Parallel()

	c := NewController()

	t.Run("ExtractsRatesAndHandlesFlatNetMeteringAndMissingPrice", func(t *testing.T) {
		t.Parallel()

		// Nil action returns zero struct
		assert.Equal(t, precedingAction{}, c.toPrecedingAction(nil, types.Settings{}, nil))

		// Action without CurrentPrice sets NoPrice = true
		noPriceAct := &types.Action{BatteryMode: types.BatteryModeStandby, SolarMode: types.SolarModeAny}
		precNoPrice := c.toPrecedingAction(noPriceAct, types.Settings{}, nil)
		assert.True(t, precNoPrice.NoPrice)
		assert.Equal(t, types.BatteryModeStandby, precNoPrice.BatteryMode)

		// Action with CurrentPrice under 1:1 flat net metering uses timeline prices to compute export credit
		flatNEMSettings := types.Settings{
			GridExportSolar:              true,
			SolarNetMeteringCreditsValue: "none",
			UtilityRateOptions:           types.UtilityRateOptions{NetMeteringCredits: true},
		}
		currPrice := types.Price{DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.05}
		timeline := []planInterval{
			{importRate: 0.10, price: types.Price{DollarsPerKWH: 0.10}},
			{importRate: 0.25, price: currPrice},
		}
		actWithPrice := &types.Action{
			BatteryMode:  types.BatteryModeLoad,
			SolarMode:    types.SolarModeExport,
			CurrentPrice: &currPrice,
		}
		prec := c.toPrecedingAction(actWithPrice, flatNEMSettings, timeline)
		assert.False(t, prec.NoPrice)
		assert.InDelta(t, 0.25, prec.ImportRate, 1e-6)
		expectedExport := currPrice.ExportRateDollars(flatNEMSettings, []types.Price{{DollarsPerKWH: 0.10}, currPrice})
		assert.InDelta(t, expectedExport, prec.ExportRate, 1e-6)
	})
}
