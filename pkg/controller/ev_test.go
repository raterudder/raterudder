package controller

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
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
		isEV, step := DetectEVCharging(ctx, 12.0, history)
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
		isEV, step := DetectEVCharging(ctx, 16.0, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 11.5, step, 0.1)
		}
	})

	t.Run("MidPower_32A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 0.8},
		}
		isEV, step := DetectEVCharging(ctx, 8.2, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 7.2, step, 0.1)
		}
	})

	t.Run("MidPower_24A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
		}
		isEV, step := DetectEVCharging(ctx, 6.5, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 5.5, step, 0.1)
		}
	})

	t.Run("PHEV_16A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.2},
		}
		isEV, step := DetectEVCharging(ctx, 5.0, history)
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
		isEV, step := DetectEVCharging(ctx, 8.0, history)
		assert.False(t, isEV)
		assert.InDelta(t, 0.0, step, 0.1)
	})

	t.Run("OvenOrDryer_BelowStepThreshold_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.5},
		}
		isEV, _ := DetectEVCharging(ctx, 4.2, history)
		assert.False(t, isEV)
	})

	t.Run("NormalSleeping_Baseline_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 0.8},
		}
		isEV, _ := DetectEVCharging(ctx, 0.8, history)
		assert.False(t, isEV)
	})

	t.Run("EmptyHistory_UsesDefaultBaseline", func(t *testing.T) {
		isEV, step := DetectEVCharging(ctx, 8.0, nil)
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
		isEV, _ := DetectEVCharging(ctx, 5.7, history)
		assert.False(t, isEV, "Sustained 5.0-5.8 kW afternoon AC continuing at 20:00 (5.7 kW) must not be flagged as EV charging")
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
			ignored, refLoads, hourBaseRef := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)

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
		ignored, _, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
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
		ignored, refLoads, _ := detectIntermittentEVAndLoadSpikes(ctx, dayMap, weatherByHour, types.Settings{}, 0.5)

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
		// without configured EVChargingPeriods is automatically detected even on warm summer days
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
		ignored, refLoads, hourBaseRef := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
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
		ignored, refLoads, hourBaseRef := detectIntermittentEVAndLoadSpikes(ctx, dayMap, nil, types.Settings{}, 0.5)
		assert.InDelta(t, 0.6, hourBaseRef[1], 0.05, "75%% frequency nighttime EV should still anchor hour 1 baseline to 0.6 kWh non-EV nights")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 1}], "full EV hour 1 should be flagged")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 2}], "full EV hour 2 should be flagged")
		assert.True(t, ignored[dayHourKey{date: "2026-07-01", hour: 3}], "adjacent partial-charge shoulder hour 3 (3.8 kWh) should be flagged")
		assert.InDelta(t, 0.6, refLoads[dayHourKey{date: "2026-07-01", hour: 3}], 0.05)
		assert.False(t, ignored[dayHourKey{date: "2026-07-01", hour: 4}], "2-hop hour 4 must not chain from shoulder hour 3")
	})
}
