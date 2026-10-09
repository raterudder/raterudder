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

		tEnd := now.Add(6 * time.Hour)
		timeline := []planInterval{
			{startTime: now, endTime: tEnd, importRate: 0.10},
		}

		mockModel := make([]TimeProfile, 24)
		for h := 0; h < 24; h++ {
			mockModel[h] = TimeProfile{Hour: h, AvgHomeLoadKWH: 1.5}
		}

		anchors := c.detectPlanningAnchors(timeline, nil, types.SystemStatus{Timestamp: now}, types.Settings{}, mockModel)
		// 8 hours * 1.5 kWh/hour = 12.0 kWh
		assert.InDelta(t, 12.0, anchors.postHorizonLoadKWH, 0.001, "must forecast 8 hours of home consumption from model")
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
		assert.Equal(t, PlanActionActiveEVChargingStandby, candidates[0].actionName)
		logCandidate(ctx, candidates[0], candidateLogData{}, interval, planState{energyKWH: 10.0, soc: 74}, true)
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
		assert.Contains(t, candidates15[0].description, "Mandatory VPP Event Active")

		// 2. Battery reached 5% SOC:
		// Target reached, switches to Standby at 5%.
		candidates5 := c.generateActionCandidates(ctx, 0, vppInterval, nil, planState{energyKWH: 13.5 * 0.05, soc: 5.0}, anchors, userReserveSettings, status, nil, precedingAction{})
		require.Len(t, candidates5, 1)
		assert.Equal(t, types.BatteryModeStandby, candidates5[0].batteryMode)
		assert.Equal(t, types.ActionReasonVPPActive, candidates5[0].reason)
		assert.Equal(t, 0, candidates5[0].targetSOC)
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
		exportSettings.OptimizationProfile = "conservative" // 0.85 round-trip efficiency, 0.05 battery export degradation
		// Replacement cost = 0.10 / 0.85 = 0.1176
		// Total hurdle = 0.1176 + 0.05 = 0.1676
		anchors := planningAnchors{knownPostHorizonRate: 0.10}

		lowMarginInterval := interval
		lowMarginInterval.exportRate = 0.16 // 0.16 < 0.1676, fails hurdle
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
		holdSettings.GridChargeBatteries = false
		holdSettings.GridExportSolar = true

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
				exportRate:    0.10, // Tomorrow solar export credit is 10c; clears 8c import + 1c hurdle to hold
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

		// 1. At or below reserve: Direct solar export must NOT be offered
		reserveState := planState{energyKWH: 2.7, soc: 20}
		candidates := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, reserveState, planningAnchors{}, touSettings, status, nil, precedingAction{})

		var directSolarCand *actionCandidate
		for i := range candidates {
			if candidates[i].solarMode == types.SolarModeExport {
				directSolarCand = &candidates[i]
				break
			}
		}
		assert.Nil(t, directSolarCand, "Direct solar export must NOT be created when battery is at or below reserve")

		// 2. Above reserve: Direct solar export IS offered
		aboveReserveState := planState{energyKWH: 4.5, soc: 35}
		candsAbove := c.generateActionCandidates(ctx, 0, sunnyInterval, nil, aboveReserveState, planningAnchors{}, touSettings, status, nil, precedingAction{})

		var directSolarAbove *actionCandidate
		for i := range candsAbove {
			if candsAbove[i].solarMode == types.SolarModeExport {
				directSolarAbove = &candsAbove[i]
				break
			}
		}
		require.NotNil(t, directSolarAbove, "Direct solar export must be offered when battery is above reserve")
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

		// 4. Low export rate below recharge cost + arbitrage diff: Direct solar export must NOT be offered even when above reserve
		lowExportInterval := sunnyInterval
		lowExportInterval.exportRate = 0.05 // Below rechargeCost (0.12 / 0.85 = 0.141) + margin
		candsLowExport := c.generateActionCandidates(ctx, 0, lowExportInterval, nil, aboveReserveState, planningAnchors{}, touSettings, status, nil, precedingAction{})
		var directSolarLow *actionCandidate
		for i := range candsLowExport {
			if candsLowExport[i].solarMode == types.SolarModeExport {
				directSolarLow = &candsLowExport[i]
				break
			}
		}
		assert.Nil(t, directSolarLow, "Direct solar export must NOT be created when export rate fails to clear recharge cost + arbitrage difference")
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
		assert.Equal(t, 0, arbCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

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
		assert.Equal(t, 0, chargeCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

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
		var chargeCand *actionCandidate
		chargeCount := 0
		for i := range candidates {
			if candidates[i].batteryMode == types.BatteryModeChargeAny {
				chargeCount++
				if candidates[i].reason == types.ActionReasonDeficitChargeNow {
					chargeCand = &candidates[i]
				}
			}
		}
		assert.Equal(t, 1, chargeCount, "Must consolidate into exactly one ChargeAny candidate")
		require.NotNil(t, chargeCand)
		assert.Equal(t, 0, chargeCand.targetSOC, "Option B candidate generation leaves targetSOC 0 for dynamic DP resolution")

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
				{refillExportRate: 0.10, effectiveReserveSOC: 20},
				{effectiveReserveSOC: 20},
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
		assert.Equal(t, 77, refinedPath.actions[0].targetSOC, "Target SOC must be clamped to exit SOC (~76.5% rounded to 77%)")
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode, "Same-rate discharge interval must be converted to Standby")
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, refinedPath.actions[1].reason)
		assert.Equal(t, 0, refinedPath.actions[1].targetSOC)
		assert.InDelta(t, 77.0, refinedPath.states[1].soc, 0.5, "Ending SOC of charge interval must reflect clamped targetSOC")
		assert.InDelta(t, 77.0, refinedPath.states[2].soc, 0.5, "Standby interval must maintain SOC")
		assert.Greater(t, originalCost, refinedPath.totalCost, "Refined plan must save money by avoiding round-trip conversion loss")
		assert.Equal(t, 0, p.actions[0].targetSOC, "Original plan must remain unmutated")
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
		// Peak export: $0.15/kWh.
		// Net physical spread after round-trip loss: $0.15 - $0.0556 = $0.0944/kWh.
		// With balanced OptimizationParams (GridChargeDegradation = $0.02 on grid charge, BatteryExportDegradation = $0.05 on export):
		// Total full-cycle wear hurdle = $0.02/0.90 + $0.05 = $0.0722/kWh (< $0.0944/kWh -> profitable!).
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
				exportRate:    0.15,
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
			BatterySOC:         30.0,
			HomeKW:             1.0,
			TimeLocation:       "America/Chicago",
		}

		settings := types.Settings{
			MinBatterySOC: 20,
		}

		lastAction := &types.Action{
			BatteryMode: types.BatteryModeLoad,
		}

		// Next hour rate is slightly higher (0.116 + 0.04 = 0.156 vs 0.140).
		// Benefit of holding for next hour is 1.0 * (0.156 - 0.140) = $0.016,
		// which is less than the duration-scaled inertia threshold ($0.01667).
		futurePrices := []types.Price{
			{
				TSStart:              now.Add(time.Hour),
				TSEnd:                now.Add(2 * time.Hour),
				DollarsPerKWH:        0.116,
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
			BatterySOC:         30.0,
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
				DollarsPerKWH:        0.116,
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
		assert.Equal(t, 77, refinedPath.actions[0].targetSOC)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, types.ActionReasonDeficitSaveForPeak, refinedPath.actions[1].reason)
		assert.InDelta(t, 77.0, refinedPath.states[1].soc, 0.5)
		assert.InDelta(t, 77.0, refinedPath.states[2].soc, 0.5)
		assert.Greater(t, origCost, refinedPath.totalCost)
		assert.Equal(t, 0, p.actions[0].targetSOC, "Original plan must remain unmutated")
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
		assert.Equal(t, 77, refinedPath.actions[0].targetSOC)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, types.ActionReasonVPPPrep, refinedPath.actions[1].reason)
		assert.Equal(t, "Preserving battery in standby ahead of VPP event.", refinedPath.actions[1].description)
		assert.Equal(t, 0, p.actions[0].targetSOC, "Original plan must remain unmutated")
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
		assert.Equal(t, 100, p.actions[0].targetSOC)
	})

	t.Run("ChangingReserveSOC_ReserveStepUp_RespectsElevatedReserveAndProfileBuffer", func(t *testing.T) {
		t.Parallel()
		timeline := []planInterval{
			{index: 0, startTime: start, endTime: start.Add(time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 0.0, minSOC: 20},
			{index: 1, startTime: start.Add(time.Hour), endTime: start.Add(2 * time.Hour), durationHours: 1.0, importRate: 0.055, loadKWH: 2.0, minSOC: 50},
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
		assert.GreaterOrEqual(t, refinedPath.actions[0].targetSOC, 60, "Must respect conservative profile buffer on reserve step-up")
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, 0, p.actions[0].targetSOC, "Original plan must remain unmutated")
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
		assert.Equal(t, int(math.Ceil(refinedPath.states[2].soc)), refinedPath.actions[0].targetSOC)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.Equal(t, 0, p.actions[0].targetSOC, "Original plan must remain unmutated")
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
		assert.Equal(t, 77, refinedPath.actions[0].targetSOC)
		assert.Equal(t, types.BatteryModeStandby, refinedPath.actions[1].batteryMode)
		assert.InDelta(t, 100.0, refinedPath.states[3].soc, 0.5)
		assert.Greater(t, origCost, refinedPath.totalCost)
		assert.Equal(t, 0, p.actions[0].targetSOC, "Original plan must remain unmutated")
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
		assert.Equal(t, 76, refinedPath.actions[0].targetSOC, "Higher non-zero targetSOC (90) must be clamped down to 76")
		assert.Equal(t, 90, p.actions[0].targetSOC, "Original plan targetSOC must remain untouched")
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
		assert.Equal(t, 86, refinedPath.actions[0].targetSOC)
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
}
