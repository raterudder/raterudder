package controller

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectEVCharging(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 23, 15, 0, 0, time.UTC)

	t.Run("HighPower_48A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 0.9},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 1.1},
		}
		isEV, step := DetectEVCharging(ctx, 12.0, history, types.Settings{})
		if assert.True(t, isEV) {
			assert.InDelta(t, 11.0, step, 0.1)
		}
	})

	t.Run("HighPower_48A_WithHeavyAC", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 4.5},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 4.2},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 4.6},
		}
		isEV, step := DetectEVCharging(ctx, 16.0, history, types.Settings{})
		if assert.True(t, isEV) {
			assert.InDelta(t, 11.5, step, 0.1)
		}
	})

	t.Run("MidPower_32A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 0.8},
		}
		isEV, step := DetectEVCharging(ctx, 8.2, history, types.Settings{})
		if assert.True(t, isEV) {
			assert.InDelta(t, 7.2, step, 0.1)
		}
	})

	t.Run("MidPower_24A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
		}
		isEV, step := DetectEVCharging(ctx, 6.5, history, types.Settings{})
		if assert.True(t, isEV) {
			assert.InDelta(t, 5.5, step, 0.1)
		}
	})

	t.Run("PHEV_16A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.2},
		}
		isEV, step := DetectEVCharging(ctx, 5.0, history, types.Settings{})
		if assert.True(t, isEV) {
			assert.InDelta(t, 3.8, step, 0.1)
		}
	})

	t.Run("CoincidingAC_And_Dryer_NoStep_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 8.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 8.2},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 8.1},
		}
		isEV, step := DetectEVCharging(ctx, 8.0, history, types.Settings{})
		assert.False(t, isEV)
		assert.InDelta(t, 0.0, step, 0.1)
	})

	t.Run("OvenOrDryer_BelowStepThreshold_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.5},
		}
		isEV, _ := DetectEVCharging(ctx, 4.2, history, types.Settings{})
		assert.False(t, isEV)
	})

	t.Run("NormalSleeping_Baseline_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 0.8},
		}
		isEV, _ := DetectEVCharging(ctx, 0.8, history, types.Settings{})
		assert.False(t, isEV)
	})

	t.Run("EmptyHistory_UsesDefaultBaseline", func(t *testing.T) {
		isEV, step := DetectEVCharging(ctx, 8.0, nil, types.Settings{})
		if assert.True(t, isEV) {
			assert.InDelta(t, 7.0, step, 0.1)
		}
	})

	t.Run("SustainedAfternoonHVACInto20h_RejectedInDetectEVCharging", func(t *testing.T) {
		checkAt20h := time.Date(2026, 8, 20, 20, 0, 0, 0, time.UTC)
		// Preceding 6 afternoon hours (14:00-19:00): 14:00 was 2.0 kW, 15:00-19:00 had sustained 5.0-5.8 kW AC
		history := []types.EnergyStats{
			{TSHourStart: checkAt20h.Add(-1 * time.Hour), HomeKWH: 5.6},
			{TSHourStart: checkAt20h.Add(-2 * time.Hour), HomeKWH: 5.8},
			{TSHourStart: checkAt20h.Add(-3 * time.Hour), HomeKWH: 5.4},
			{TSHourStart: checkAt20h.Add(-4 * time.Hour), HomeKWH: 5.0},
			{TSHourStart: checkAt20h.Add(-5 * time.Hour), HomeKWH: 4.9},
			{TSHourStart: checkAt20h.Add(-6 * time.Hour), HomeKWH: 2.0},
		}
		isEV, _ := DetectEVCharging(ctx, 5.7, history, types.Settings{})
		assert.False(t, isEV, "Sustained 5.0-5.8 kW afternoon AC continuing at 20:00 (5.7 kW) must not be flagged as EV charging")
	})

	t.Run("DaytimeEVChargingDoesNotTriggerSustainedHVACIntoNighttimeEV", func(t *testing.T) {
		checkAt22h := time.Date(2026, 8, 20, 22, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		// Prior 7 days of normal 1.2 kW evening baseline at hour 21
		for d := 1; d <= 7; d++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(2026, 8, 20-d, 21, 0, 0, 0, time.UTC),
				HomeKWH:     1.2,
			})
		}
		// Preceding 6 hours (16:00-21:00) had high-power daytime EV charging (9-12 kW) and a 6.6 kW ramp hour
		history = append(history,
			types.EnergyStats{TSHourStart: checkAt22h.Add(-1 * time.Hour), HomeKWH: 9.2},
			types.EnergyStats{TSHourStart: checkAt22h.Add(-2 * time.Hour), HomeKWH: 6.6},
			types.EnergyStats{TSHourStart: checkAt22h.Add(-3 * time.Hour), HomeKWH: 9.1},
			types.EnergyStats{TSHourStart: checkAt22h.Add(-4 * time.Hour), HomeKWH: 11.4},
			types.EnergyStats{TSHourStart: checkAt22h.Add(-5 * time.Hour), HomeKWH: 12.1},
			types.EnergyStats{TSHourStart: checkAt22h.Add(-6 * time.Hour), HomeKWH: 9.5},
		)
		isEV, step := DetectEVCharging(ctx, 8.0, history, types.Settings{})
		if assert.True(t, isEV, "Daytime EV charging > 7.5 kW must not classify as sustained HVAC and mask nighttime EV charging") {
			assert.InDelta(t, 6.8, step, 0.1)
		}
	})

	t.Run("FilterTODHomeLoadEVSpikes_ReplacesIntermittentEVSpikes", func(t *testing.T) {
		// 5 normal days at 1.0 kW (3 hours/day = 15 points) and 2 EV days at 9.5 kW (6 points)
		loads := []float64{
			1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0,
			9.5, 9.5, 9.5, 9.5, 9.5, 9.5,
		}
		filtered := FilterTODHomeLoadEVSpikes(loads, 14)
		for _, v := range filtered {
			assert.InDelta(t, 1.0, v, 0.01, "Intermittent EV charging spikes should be replaced with the 30th percentile non-EV baseline")
		}
	})

	t.Run("FilterTODHomeLoadEVSpikes_PreservesRoutineDailyHighLoad", func(t *testing.T) {
		// Every day has a routine 5.5 kW load at this hour
		loads := []float64{5.5, 5.5, 5.5, 5.5, 5.5, 5.5, 5.5, 5.5, 5.5}
		filtered := FilterTODHomeLoadEVSpikes(loads, 18)
		for _, v := range filtered {
			assert.InDelta(t, 5.5, v, 0.01, "Routine daily loads should not be filtered out")
		}
	})

	t.Run("AdaptiveThresholdOn48AChargerSiteRejectsMidTierAppliance", func(t *testing.T) {
		// Build 7 days (168 hours) with 0.8 kWh base load and 2 overnight 11.2 kW EV sessions (01:00-02:00).
		// With >= 4 local core hours >= 8.2 kW and >= 6.5 kW step, adaptive thresholds rise to 6.8 / 5.4 kW.
		baseTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		for d := 0; d < 7; d++ {
			for h := 0; h < 24; h++ {
				load := 0.8
				if (d == 1 || d == 3) && (h == 1 || h == 2) {
					load = 11.2
				}
				history = append(history, types.EnergyStats{
					TSHourStart: baseTime.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
					HomeKWH:     load,
				})
			}
		}

		isEV, _ := DetectEVCharging(ctx, 5.4, history, types.Settings{})
		assert.False(t, isEV, "5.4 kW appliance spike should be ignored on an adaptive 6.8 kW fast-charger site")

		isEV, step := DetectEVCharging(ctx, 9.6, history, types.Settings{})
		assert.True(t, isEV, "9.6 kW EV session should still trigger on an adaptive 6.8 kW fast-charger site")
		assert.InDelta(t, 8.8, step, 0.05)
	})

	t.Run("SlowNightGuardPreservesThresholdOnMixedChargerSite", func(t *testing.T) {
		// Household has BOTH a 48A charger (11.2 kW on 2 nights) AND a slower EV/Level 2 session (6.0 kW
		// across 2 consecutive overnight hours on 2 other nights without adjacent >= 8.2 kW hours).
		// GuardSlowNight2H keeps the default 4.8 / 3.5 kW thresholds so the slower EV is still detected.
		baseTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		for d := 0; d < 7; d++ {
			for h := 0; h < 24; h++ {
				load := 0.8
				if (d == 1 || d == 3) && (h == 1 || h == 2) {
					load = 11.2
				}
				if (d == 2 || d == 4) && (h == 3 || h == 4) {
					load = 6.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart: baseTime.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
					HomeKWH:     load,
				})
			}
		}

		isEV, step := DetectEVCharging(ctx, 5.8, history, types.Settings{})
		assert.True(t, isEV, "slow-night guard should preserve 4.8/3.5 kW thresholds and detect 5.8 kW charging")
		assert.InDelta(t, 5.0, step, 0.05)
	})

	t.Run("FilterCompletedHourlyStats_FallBackDSTAndBoundaries", func(t *testing.T) {
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// Fall-Back DST in America/Chicago: 2026-11-01 01:00 CDT (-0500) repeats as 01:00 CST (-0600).
		// At 01:20 CST (07:20 UTC), the first 01:00 CDT bucket (06:00 UTC) is completed and MUST be kept,
		// while the in-progress second 01:00 CST bucket (07:00 UTC), exact-boundary, future, and zero buckets are dropped.
		first01CDT := time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC).In(chicagoLoc)  // 01:00 -0500 CDT
		second01CST := time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC).In(chicagoLoc) // 01:00 -0600 CST
		nowCST := second01CST.Add(20 * time.Minute)                                // 01:20 -0600 CST

		stats := []types.EnergyStats{
			{}, // Zero TSHourStart
			{TSHourStart: first01CDT.Add(-time.Hour), HomeKWH: 1.0},
			{TSHourStart: first01CDT, HomeKWH: 2.0},
			{TSHourStart: second01CST, HomeKWH: 3.0},
			{TSHourStart: second01CST.Add(time.Hour), HomeKWH: 4.0},
		}
		filtered := FilterCompletedHourlyStats(stats, nowCST)
		if assert.Len(t, filtered, 2) {
			assert.InDelta(t, 1.0, filtered[0].HomeKWH, 0.001)
			assert.InDelta(t, 2.0, filtered[1].HomeKWH, 0.001, "completed first 01:00 CDT hour must be retained during 01:xx CST")
		}

		// Zero now returns history unchanged
		assert.Equal(t, stats, FilterCompletedHourlyStats(stats, time.Time{}))
	})

	t.Run("UTCTimestampsWithTimeLocationSustainedDaytimeHVAC", func(t *testing.T) {
		// Afternoon HVAC 15:00-19:00 CDT stored as UTC (20:00-00:00 UTC) with TimeLocation set:
		// must be localized so sustainedDaytimeHVAC holds and 5.7 kW at 20:00 CDT (01:00 UTC) is NOT flagged as EV.
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		endLocal := time.Date(2026, 7, 15, 20, 0, 0, 0, chicagoLoc)
		var history []types.EnergyStats
		for h := 5; h >= 1; h-- {
			history = append(history, types.EnergyStats{
				TSHourStart:  endLocal.Add(-time.Duration(h) * time.Hour).UTC(),
				TimeLocation: "America/Chicago",
				HomeKWH:      5.2,
			})
		}

		isEV, _ := DetectEVCharging(ctx, 5.7, history, types.Settings{})
		assert.False(t, isEV, "UTC-encoded afternoon CDT AC hours must still prevent false 20:00 EV detection")
	})

	t.Run("EveryNightConfiguredEVWithSettingsAdaptsThreshold", func(t *testing.T) {
		// When EVChargingStandby is enabled and a 48A EV charges all 7/7 nights at 01:00-02:00,
		// DetectEVCharging anchors 01:00-02:00 and raises thresholds to 6.8 / 5.4 kW.
		baseTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		for d := 0; d < 7; d++ {
			for h := 0; h < 24; h++ {
				load := 0.8
				if h == 1 || h == 2 {
					load = 11.2
				}
				history = append(history, types.EnergyStats{
					TSHourStart: baseTime.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{EVChargingStandby: true}
		isEV, _ := DetectEVCharging(ctx, 5.6, history, settings)
		assert.False(t, isEV, "5.6 kW appliance spike should be rejected when 7/7 nightly EV adapts thresholds to 6.8/5.4 kW")

		isEV, _ = DetectEVCharging(ctx, 9.6, history, settings)
		assert.True(t, isEV, "9.6 kW EV charging should still trigger with 7/7 nightly EV adaptive thresholds")

		// Short (< 72h) history keeps default 4.8 / 3.5 kW thresholds even when high-load hours exist.
		shortHist := history[:48]
		isEV, _ = DetectEVCharging(ctx, 5.6, shortHist, settings)
		assert.True(t, isEV, "< 72h history must stay on default 4.8/3.5 kW thresholds")
	})

	t.Run("InferThresholdsPrefersESSTimezoneOverSettingsLocation", func(t *testing.T) {
		nyLoc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		baseNY := time.Date(2026, 7, 1, 0, 0, 0, 0, nyLoc)
		var history []types.EnergyStats
		for d := 0; d < 7; d++ {
			for h := 0; h < 24; h++ {
				load := 0.8
				if (d == 1 || d == 3) && (h == 10 || h == 11) {
					load = 11.2 // High-power daytime session
				}
				if (d == 2 || d == 4) && (h == 20 || h == 21) {
					load = 5.5 // Slow-night session in America/New_York (20:00-21:59 EDT = 17:00-18:59 PDT)
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  baseNY.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
					TimeLocation: "America/New_York",
					HomeKWH:      load,
				})
			}
		}

		// Even when settings.Location disagrees (America/Los_Angeles, making 20:00-21:00 EDT look like
		// daytime 17:00-18:00 PDT), ESS TimeLocation must win so the slow-night guard keeps 4.8 / 3.5 kW.
		settings := types.Settings{
			Location: &types.SiteLocation{TimeZone: "America/Los_Angeles"},
		}
		isEV, _ := DetectEVCharging(ctx, 5.5, history, settings)
		assert.True(t, isEV, "ESS history TimeLocation must trigger slow-night guard over conflicting settings.Location")
	})
}

func TestDetectIntermittentEVAndLoadSpikes(t *testing.T) {
	ctx := context.Background()

	buildDayMap := func(history []types.EnergyStats) map[string]*dayPoints {
		dayMap := make(map[string]*dayPoints)
		for _, h := range history {
			if h.HomeKWH <= 0 {
				continue
			}
			dateStr := h.TSHourStart.Format("2006-01-02")
			d, ok := dayMap[dateStr]
			if !ok {
				dTime, _ := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
				d = &dayPoints{
					date:    dateStr,
					dayTime: dTime,
					weekday: dTime.Weekday(),
				}
				dayMap[dateStr] = d
			}
			d.points = append(d.points, h)
			d.loads = append(d.loads, h.HomeKWH)
		}
		return dayMap
	}

	t.Run("CommonLevel2Chargers40AAnd50ABreakersAndShortTopOffs", func(t *testing.T) {
		// Verify that both 40A breaker (7.7 kW) and 50A breaker (9.6 kW) Level 2 chargers
		// are detected across variable session lengths (1-hour top-off on Mondays,
		// 2 hours on Wednesdays, 3 hours on Fridays/Saturdays).
		checkChargerPower := func(t *testing.T, evKW float64) {
			var history []types.EnergyStats
			startDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) // Monday
			for d := 0; d < 28; d++ {
				dayTime := startDate.AddDate(0, 0, d)
				wd := dayTime.Weekday()
				for h := 0; h < 24; h++ {
					load := 1.0
					if h >= 12 && h <= 17 {
						load = 1.5
					}
					isEV := (wd == time.Monday && h == 14) ||
						(wd == time.Wednesday && (h == 13 || h == 14)) ||
						((wd == time.Friday || wd == time.Saturday) && (h == 13 || h == 14 || h == 15))
					if isEV {
						load += evKW
					}
					history = append(history, types.EnergyStats{
						TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						HomeKWH:     load,
					})
				}
			}

			dayMap := buildDayMap(history)
			ignored, refLoads, hourBaseRef, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)

			// Monday 2026-07-20 had a 1-hour top-off at hour 14 only
			assert.True(t, ignored[dayHourKey{date: "2026-07-20", hour: 14}], "1-hour Monday solar top-off should be detected")
			assert.False(t, ignored[dayHourKey{date: "2026-07-20", hour: 13}], "non-EV hour 13 on Monday must not be flagged")
			assert.InDelta(t, 1.5, refLoads[dayHourKey{date: "2026-07-20", hour: 14}], 0.05)

			// Wednesday 2026-07-22 had a 2-hour charge at hours 13-14
			assert.True(t, ignored[dayHourKey{date: "2026-07-22", hour: 13}])
			assert.True(t, ignored[dayHourKey{date: "2026-07-22", hour: 14}])
			assert.False(t, ignored[dayHourKey{date: "2026-07-22", hour: 15}])

			// Friday 2026-07-24 had a 3-hour charge at hours 13-15
			for _, hr := range []int{13, 14, 15} {
				assert.True(t, ignored[dayHourKey{date: "2026-07-24", hour: hr}])
				assert.InDelta(t, 1.5, hourBaseRef[hr], 0.05)
			}
		}

		t.Run("40A_Breaker_7.7kW", func(t *testing.T) {
			checkChargerPower(t, 7.7)
		})
		t.Run("50A_Breaker_9.6kW", func(t *testing.T) {
			checkChargerPower(t, 9.6)
		})
	})

	t.Run("Level1ChargingAndNormalAppliancesNotFiltered", func(t *testing.T) {
		// Verify that Level 1 EV charging (~1.4 kW on a 120V/15A circuit, bringing a 1.0 kW base to 2.4 kW)
		// and normal household cycling appliances (3.5 kW electric dryer) are NOT flagged as EV outliers.
		var history []types.EnergyStats
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 14; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				load := 1.0
				if d == 2 && (h == 13 || h == 14 || h == 15) {
					load = 2.4 // Level 1 charger (1.0 base + 1.4 kW)
				}
				if d == 5 && h == 16 {
					load = 3.5 // Electric dryer / oven (< EVMinThresholdKW 4.8 kW)
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		dayMap := buildDayMap(history)
		ignored, _, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
		assert.Empty(t, ignored, "Level 1 charging (2.4 kWh) and normal dryer loads (3.5 kWh) must not be flagged")
	})

	t.Run("HotDayHVACPreservedWhileEVOnHotDayFiltered", func(t *testing.T) {
		// Verify that:
		// 1. Sustained hot-day HVAC (5.2 kWh across 9 hours 11:00-19:00 on a 31°C day) is NOT flagged as an EV spike.
		// 2. A 2-hour 9.6 kW EV session on top of a 2.5 kW hot-day AC load (12.1 kWh total at hours 14-15)
		//    IS flagged, and its replacement outlierRefLoad preserves the 2.5 kWh AC baseline (dayP65).
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

		for d := 0; d < 14; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 1.0
				temp := 21.0

				if d == 4 {
					// Sustained hot day with heavy AC (5.2 kWh for 9 hours: 11:00..19:00 -> dayP65 = 5.2)
					temp = 31.0
					if h >= 11 && h <= 19 {
						load = 5.2
					}
				} else if d == 8 {
					// Hot day with 2.5 kWh AC for 10 hours (10:00..19:00 -> dayP65 = 2.5)
					// PLUS a 2-hour 9.6 kW EV charge at 14:00-15:00 (total 12.1 kWh)
					temp = 31.0
					if h >= 10 && h <= 19 {
						load = 2.5
					}
					if h == 14 || h == 15 {
						load = 12.1
					}
				}

				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
				weatherByHour[ts.UTC()] = temp
			}
		}

		dayMap := buildDayMap(history)
		ignored, refLoads, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, weatherByHour, types.Settings{}, 0.5)

		// Day 4 (2026-07-05): sustained 5.2 kWh AC must NOT be flagged
		for h := 11; h <= 19; h++ {
			assert.False(t, ignored[dayHourKey{date: "2026-07-05", hour: h}], "sustained hot-day AC at hour %d must not be flagged", h)
		}

		// Day 8 (2026-07-09): 2-hour EV charge on top of AC at hours 14-15 MUST be flagged,
		// and its replacement reference load must preserve the 2.5 kWh same-day AC baseline (dayP65).
		for _, hr := range []int{14, 15} {
			key := dayHourKey{date: "2026-07-09", hour: hr}
			assert.True(t, ignored[key], "EV charge on hot day at hour %d should be flagged", hr)
			assert.InDelta(t, 2.5, refLoads[key], 0.05, "replacement reference load should preserve 2.5 kWh same-day AC load")
		}
	})

	t.Run("UnconfiguredNighttimeLevel2EVChargingFiltered", func(t *testing.T) {
		// Verify that nighttime Level 2 EV charging (7.7 kW at 01:00-02:00 3 nights/week)
		// with EVChargingStandby disabled is automatically detected even on warm summer days
		// where afternoon AC runs at 3.5 kWh for 9 hours (dayP65 = 3.5 kWh), and is replaced
		// with the 0.5 kWh overnight sleeping baseline rather than daytime AC levels.
		var history []types.EnergyStats
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 14; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			isEVNight := d%3 == 0
			for h := 0; h < 24; h++ {
				load := 1.2
				if h >= 0 && h <= 5 {
					load = 0.5
				} else if h >= 12 && h <= 20 {
					load = 3.5 // Afternoon/evening AC across 9 hours (pushes daytime dayP65 to 3.5 kWh)
				}
				if isEVNight && (h == 1 || h == 2) {
					load += 7.7 // 8.2 kWh total on a 40A circuit
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		dayMap := buildDayMap(history)
		ignored, refLoads, hourBaseRef, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 1}], "overnight 7.7 kW EV charge must not be masked by daytime 3.5 kWh AC")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 2}], "overnight 7.7 kW EV charge must not be masked by daytime 3.5 kWh AC")
		assert.InDelta(t, 0.5, hourBaseRef[1], 0.05, "cross-day baseline reference for hour 1 should be 0.5 kWh")
		assert.InDelta(t, 0.5, refLoads[dayHourKey{date: "2026-07-01", hour: 1}], 0.05, "overnight replacement reference should use 0.5 kWh overnight baseline, not daytime dayP65")
	})

	t.Run("HighFrequencyNighttimeEVAndAdjacentShoulderHours", func(t *testing.T) {
		// Verify that:
		// 1. Even when a commuter charges their EV 5-6 nights/week (75% of nights, pulling the raw
		//    30th percentile at 01:00-02:00 into the EV tier), the non-EV nights (< 4.8 kW) anchor
		//    allRefByHour[hr] to the true 0.6 kWh sleeping baseline so all EV nights are detected.
		// 2. Partial-charge shoulder hours (e.g. 3.8 kWh at 03:00 when charging finishes 25 minutes
		//    into the hour) immediately adjacent to a confirmed full-hour EV spike are also detected,
		//    without chaining to a second adjacent hour (04:00).
		var history []types.EnergyStats
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 12; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			// 9 out of 12 nights (75%) have EV charging at 01:00-02:00 (plus a 3.8 kWh shoulder at 03:00)
			isEVNight := d%4 != 3
			for h := 0; h < 24; h++ {
				load := 1.1
				if h >= 0 && h <= 6 {
					load = 0.6
				}
				if isEVNight {
					if h == 1 || h == 2 {
						load = 8.3 // 0.6 base + 7.7 kW EV
					} else if h == 3 {
						load = 3.8 // Partial-charge shoulder hour (< 4.8 kW, adjacent to hour 2)
					} else if h == 4 {
						load = 3.5 // 2 hops away from hour 2 (must NOT chain)
					}
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		dayMap := buildDayMap(history)
		ignored, refLoads, hourBaseRef, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
		assert.InDelta(t, 0.6, hourBaseRef[1], 0.05, "75%% frequency nighttime EV should still anchor hour 1 baseline to 0.6 kWh non-EV nights")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 1}], "full EV hour 1 should be flagged")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 2}], "full EV hour 2 should be flagged")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 3}], "adjacent partial-charge shoulder hour 3 (3.8 kWh) should be flagged")
		assert.InDelta(t, 0.6, refLoads[dayHourKey{date: "2026-07-01", hour: 3}], 0.05)
		assert.False(t, ignored[dayHourKey{date: "2026-07-01", hour: 4}], "2-hop hour 4 must not chain from shoulder hour 3")
	})

	t.Run("AdaptiveThresholdRescuesAppliancesAndStripsDaytimeSolarShoulder", func(t *testing.T) {
		// Site with 48A charger (11.4 kWh sessions -> adaptive 6.8 / 5.4 kW thresholds):
		// - One-off 5.4 kWh daytime appliance (15:00) is NOT stripped (< 6.8 kW).
		// - Daytime solar EV charge (11.2 kWh at 13:00 >= 8.0 kW) anchors adjacent partial shoulder
		//   4.2 kWh at 12:00 (>= 3.2 kWh, 1-hop) without chaining to 2-hop 4.0 kWh at 11:00.
		// - Isolated [6.8, 8.0) spike (7.2 kWh at 18:00) is stripped in Pass 1 (>= 6.8 kW), but does
		//   NOT qualify as an actual EV anchor (< 8.0 kW) and does NOT pull adjacent 4.0 kWh at 19:00 into Pass 2.
		var history []types.EnergyStats
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 10; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				load := 0.8
				if (d == 1 || d == 3) && (h == 1 || h == 2) {
					load = 11.4
				}
				if d == 4 {
					switch h {
					case 15:
						load = 5.4 // Dryer/oven spike below 6.8 kW adaptive threshold
					case 18:
						load = 7.2 // Mid-tier Pass 1 spike below 8.0 kW actual EV anchor floor
					case 19:
						load = 4.0 // Adjacent to 7.2 kWh, must NOT be flagged by Pass 2
					}
				}
				if d == 6 {
					switch h {
					case 11:
						load = 4.0 // 2 hops from 13:00 anchor (must NOT chain)
					case 12:
						load = 4.2 // Adjacent daytime ramp-in shoulder (must be flagged)
					case 13:
						load = 11.2 // Daytime solar EV anchor
					}
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		dayMap := buildDayMap(history)
		ignored, _, _, thrKW, stepKW := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
		assert.InDelta(t, 6.8, thrKW, 0.01)
		assert.InDelta(t, 5.4, stepKW, 0.01)

		assert.False(t, ignored[dayHourKey{date: "2026-07-05", hour: 15}], "5.4 kWh appliance spike should be preserved on adaptive 6.8 kW site")
		assert.True(t, ignored[dayHourKey{date: "2026-07-05", hour: 18}], "7.2 kWh >= 6.8 kW spike should be flagged in Pass 1")
		assert.False(t, ignored[dayHourKey{date: "2026-07-05", hour: 19}], "4.0 kWh next to 7.2 kWh (< 8.0 kW) must not be flagged as EV shoulder")

		assert.True(t, ignored[dayHourKey{date: "2026-07-07", hour: 13}], "daytime 11.2 kWh EV charge should be flagged")
		assert.True(t, ignored[dayHourKey{date: "2026-07-07", hour: 12}], "daytime 4.2 kWh shoulder adjacent to 11.2 kWh EV anchor should be flagged")
		assert.False(t, ignored[dayHourKey{date: "2026-07-07", hour: 11}], "2-hop hour 11 must not chain from daytime shoulder hour 12")
	})

	t.Run("SlowNightGuardProtectsMixedTwoEVHousehold", func(t *testing.T) {
		var history []types.EnergyStats
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 10; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				load := 0.8
				if (d == 1 || d == 3) && (h == 1 || h == 2) {
					load = 11.4 // Fast EV sessions (4 high-core hours)
				}
				if (d == 2 || d == 5) && (h == 1 || h == 2) {
					load = 6.0 // Slow night EV sessions (2 nights x 2 consecutive hours < 7.8 kW)
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		dayMap := buildDayMap(history)
		ignored, _, _, thrKW, stepKW := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
		assert.InDelta(t, EVMinThresholdKW, thrKW, 0.01)
		assert.InDelta(t, EVMinStepKW, stepKW, 0.01)
		assert.True(t, ignored[dayHourKey{date: "2026-07-02", hour: 1}], "fast EV session should be flagged")
		assert.True(t, ignored[dayHourKey{date: "2026-07-03", hour: 1}], "slow 6.0 kW overnight EV session must remain flagged when GuardSlowNight2H triggers")
		assert.True(t, ignored[dayHourKey{date: "2026-07-03", hour: 2}], "slow 6.0 kW overnight EV session hour 2 must remain flagged")
	})

	t.Run("EVChargingStandbyRequiresRealStepAboveBaseline", func(t *testing.T) {
		// Even with EVChargingStandby enabled, hours without a >= 3.5 kWh step and > 2.5x refLoad
		// must not be stripped:
		// - Sustained 4.2 kWh daytime/evening HVAC on day 2 hitting 5.2 kWh at 20:00 (step 1.0 kWh) -> preserved.
		// - Routine 2.4 kWh late-evening baseline at 23:00 hitting 5.0 kWh (step 2.6 kWh) -> preserved.
		// - Overnight 0.8 kWh baseline at 01:00 hitting 8.4 kWh EV charge (step 7.6 kWh) -> stripped.
		var history []types.EnergyStats
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 7; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				load := 0.8
				if h >= 12 && h <= 21 {
					load = 1.5
				}
				if h == 23 {
					load = 2.4
				}
				if d == 2 {
					if h >= 11 && h <= 21 {
						load = 4.2 // Sustained HVAC raising dayP65 = 4.2 kWh
					}
					if h == 20 {
						load = 5.2 // Only +1.0 kW step above 4.2 kWh HVAC
					}
					if h == 23 {
						load = 5.0 // Only +2.6 kW step above 2.4 kWh baseline
					}
				}
				if d == 4 && h == 1 {
					load = 8.4 // Genuine overnight EV charge (+7.6 kW step above 0.8 kWh baseline)
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		dayMap := buildDayMap(history)
		settings := types.Settings{EVChargingStandby: true}
		ignored, _, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, settings, 0.5)
		assert.False(t, ignored[dayHourKey{date: "2026-07-03", hour: 20}], "5.2 kWh evening HVAC bump (+1.0 kW step) must not be stripped under EVChargingStandby")
		assert.False(t, ignored[dayHourKey{date: "2026-07-03", hour: 23}], "5.0 kWh late-evening bump (+2.6 kW step) must not be stripped under EVChargingStandby")
		assert.True(t, ignored[dayHourKey{date: "2026-07-05", hour: 1}], "8.4 kWh genuine overnight EV charge (+7.6 kW step) should be stripped")
	})

	t.Run("AdaptiveThresholdBoundaryConditionsAndSlowNightEdgeCases", func(t *testing.T) {
		buildBase := func(days int, baseKW float64) []types.EnergyStats {
			startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			var hist []types.EnergyStats
			for d := 0; d < days; d++ {
				dayTime := startDate.AddDate(0, 0, d)
				for h := 0; h < 24; h++ {
					hist = append(hist, types.EnergyStats{
						TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						HomeKWH:     baseKW,
					})
				}
			}
			return hist
		}
		setLoad := func(hist []types.EnergyStats, day, hour int, kw float64) {
			hist[day*24+hour].HomeKWH = kw
		}

		t.Run("UnsaturatedQuantileAndMidnightShoulderCrossing", func(t *testing.T) {
			// 10 days x 24h at 1.2 kWh baseline; 4 core hours at 8.2 kWh (step 7.0 -> 7.0*0.72 = 5.04 step,
			// 5.04 + 1.4 = 6.44 threshold), plus a midnight-crossing session on day 5 (23:00=11.2, next 00:00=4.2).
			// On day 5, 23:00=11.2 is 1 core hour, and on days 1 & 3 hours 1..2=8.2 (or day 1 01:00, day 3 01:00,
			// day 4 01:00, day 6 01:00 -> 4 hours at 8.2 kWh, q35 across [7.0,7.0,7.0,7.0,10.0] = 7.0*1.4...):
			// Use 4 equal 8.2 kWh cores + verify unsaturated threshold between 4.8 and 6.8 kW.
			hist := buildBase(10, 1.2)
			for _, d := range []int{1, 3} {
				setLoad(hist, d, 1, 8.2)
				setLoad(hist, d, 2, 8.2)
			}
			// Midnight-crossing shoulder: day 5 hour 23 = 11.2 (step 10.0) -> day 6 hour 0 = 4.2 kWh shoulder.
			// Core steps sorted: [7.0, 7.0, 7.0, 7.0, 10.0]; pos 0.35*4=1.4 -> q35 = 7.0 -> step=5.04, thr=6.44.
			setLoad(hist, 5, 23, 11.2)
			setLoad(hist, 6, 0, 4.2)

			ignored, _, _, thrKW, stepKW := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(hist), nil, types.Settings{}, 0.5)
			assert.InDelta(t, 6.44, thrKW, 0.01)
			assert.InDelta(t, 5.04, stepKW, 0.01)
			assert.True(t, ignored[dayHourKey{date: "2026-07-06", hour: 23}], "23:00 EV anchor should be flagged")
			assert.True(t, ignored[dayHourKey{date: "2026-07-07", hour: 0}], "midnight-crossing 00:00 shoulder adjacent to 23:00 EV anchor should be flagged")
		})

		t.Run("NeighborDominanceMarginExcludesShoulderFromCores", func(t *testing.T) {
			// Only 2 true local cores >= 8.2 kW (10.2 kWh on days 1 & 3 h=2), while adjacent h=1 is 8.5 kWh
			// (10.2 >= 8.5 + 1.5), so those 2 ramp hours are excluded from core count (< 4 -> keeps 4.8/3.5).
			hist := buildBase(10, 0.8)
			for _, d := range []int{1, 3} {
				setLoad(hist, d, 1, 8.5)
				setLoad(hist, d, 2, 10.2)
			}
			_, _, _, thrKW, stepKW := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(hist), nil, types.Settings{}, 0.5)
			assert.InDelta(t, EVMinThresholdKW, thrKW, 0.01)
			assert.InDelta(t, EVMinStepKW, stepKW, 0.01)
		})

		t.Run("SingleOvernightMidnightCollapsingSlowSessionDoesNotTriggerGuard", func(t *testing.T) {
			// 4 high-core hours PLUS a single slow overnight session spanning midnight (day 2 23:00 + day 3 00:00 at 6.0 kW)
			// plus a [4.8, 7.8) hour adjacent to >= 8.2 kW (day 4 01:00=6.0 next to 02:00=11.4).
			// Midnight collapses to 1 slowNightSession (< 2), and 01:00 next to 11.4 is excluded -> adaptive stays active.
			hist := buildBase(10, 0.8)
			for _, d := range []int{1, 5} {
				setLoad(hist, d, 1, 11.4)
				setLoad(hist, d, 2, 11.4)
			}
			setLoad(hist, 2, 23, 6.0)
			setLoad(hist, 3, 0, 6.0)
			setLoad(hist, 4, 1, 6.0)
			setLoad(hist, 4, 2, 11.4)

			_, _, _, thrKW, stepKW := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(hist), nil, types.Settings{}, 0.5)
			assert.InDelta(t, 6.8, thrKW, 0.01)
			assert.InDelta(t, 5.4, stepKW, 0.01)
		})

		t.Run("ShortHistoryConfiguredEVAnchors30To48TierOvernight", func(t *testing.T) {
			// 4 days (< 7 days, >= 72h) with EVChargingStandby=true: hour 1 has [0.8, 3.6, 8.2, 8.2]
			// so P30 at index 1 is 3.6 (< 4.8, >= 3.0) while hour 3 is 0.8 (minOvernightRef < 2.5).
			// hasConfiguredEVAtHour anchors hour 1 to quiet reading 0.8 kWh so all 8.2 kWh EV hours pass step check.
			hist := buildBase(4, 0.8)
			setLoad(hist, 1, 1, 3.6)
			setLoad(hist, 2, 1, 8.2)
			setLoad(hist, 3, 1, 8.2)

			settings := types.Settings{EVChargingStandby: true}
			ignored, _, hourBaseRef, _, _ := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(hist), nil, settings, 0.5)
			assert.InDelta(t, 0.8, hourBaseRef[1], 0.05)
			assert.True(t, ignored[dayHourKey{date: "2026-07-03", hour: 1}], "8.2 kWh EV session must be detected once 3.6 kWh P30 is anchored down")
			assert.True(t, ignored[dayHourKey{date: "2026-07-04", hour: 1}])
		})
	})

	t.Run("ElectricHeatingColdMorningsPreservedWhileStackedEVFiltered", func(t *testing.T) {
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.Truncate(time.Hour).UTC()] = tempC
				load := 1.2
				if h <= 8 {
					load = 1.0 + 0.55*max(0.0, 10.0-tempC)
				}
				if d == 15 && h == 2 {
					load += 9.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		ignored, ignoredRef, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(history), weatherByHour, types.Settings{}, 0.5)
		assert.False(t, ignored[dayHourKey{date: "2026-11-16", hour: 5}], "cold electric heating at 05:00 must be preserved")
		assert.False(t, ignored[dayHourKey{date: "2026-11-16", hour: 6}], "cold electric heating at 06:00 must be preserved")
		assert.True(t, ignored[dayHourKey{date: "2026-11-16", hour: 2}], "EV stacked on top of electric heating must be flagged")
		assert.GreaterOrEqual(t, ignoredRef[dayHourKey{date: "2026-11-16", hour: 2}], 3.0)
	})

	t.Run("MorningOnlyElectricHeatingPreservedWhileOvernightEVFiltered", func(t *testing.T) {
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		evDays := map[int]bool{2: true, 6: true, 10: true, 14: true}
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.Truncate(time.Hour).UTC()] = tempC
				load := 1.0
				if h <= 4 {
					load = 0.8
					if evDays[d] && (h == 1 || h == 2) {
						load = 8.5
					}
				} else if h >= 6 && h <= 8 {
					load = 1.4 + 0.50*max(0.0, 10.0-tempC)
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		ignored, ignoredRef, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(history), weatherByHour, types.Settings{}, 0.5)
		for _, dStr := range []string{"2026-11-03", "2026-11-07", "2026-11-11", "2026-11-15"} {
			for _, hr := range []int{1, 2} {
				k := dayHourKey{date: dStr, hour: hr}
				assert.True(t, ignored[k], "overnight EV %s h=%d must be flagged", dStr, hr)
				assert.Less(t, ignoredRef[k], 1.5, "overnight EV ref load %s h=%d must stay quiet", dStr, hr)
			}
		}
		for _, dStr := range []string{"2026-11-15", "2026-11-16"} {
			for _, hr := range []int{6, 7, 8} {
				assert.False(t, ignored[dayHourKey{date: dStr, hour: hr}], "morning heating %s h=%d must be preserved", dStr, hr)
			}
		}
	})

	t.Run("GasFurnaceBlowerSlopeDoesNotMaskColdNightEV", func(t *testing.T) {
		startDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		evDays := map[int]bool{11: true, 13: true, 15: true}
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.Truncate(time.Hour).UTC()] = tempC
				load := 0.8 + 0.025*max(0.0, 10.0-tempC)
				if evDays[d] && (h == 1 || h == 2) {
					load = 8.2
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		ignored, _, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(history), weatherByHour, types.Settings{}, 0.5)
		for _, dStr := range []string{"2026-11-12", "2026-11-14", "2026-11-16"} {
			for _, hr := range []int{1, 2} {
				assert.True(t, ignored[dayHourKey{date: dStr, hour: hr}], "cold-night EV %s h=%d must still be flagged", dStr, hr)
			}
		}
	})

	t.Run("ConfirmedCoolingLateEvening23hPreserved", func(t *testing.T) {
		startDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		weatherByHour := make(map[time.Time]float64)
		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 18.0 + float64(d)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				weatherByHour[ts.Truncate(time.Hour).UTC()] = tempC
				load := 0.8
				if h >= 11 {
					load = 1.2 + 0.42*max(0.0, tempC-22.0)
				}
				if d == 15 && h == 2 {
					load = 8.8
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		ignored, _, _, _, _ := detectIntermittentEVAndLoadSpikes(ctx, buildDayMap(history), weatherByHour, types.Settings{}, 0.5)
		assert.False(t, ignored[dayHourKey{date: "2026-07-15", hour: 23}], "hot late-evening cooling on 2026-07-15 h=23 must be preserved")
		assert.False(t, ignored[dayHourKey{date: "2026-07-16", hour: 23}], "hot late-evening cooling on 2026-07-16 h=23 must be preserved")
		assert.True(t, ignored[dayHourKey{date: "2026-07-16", hour: 2}], "overnight EV session at 02:00 must be flagged")
	})
}
