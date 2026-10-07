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

func TestBuildHourlyEnergyModel(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	t.Run("DayOfWeekSelection", func(t *testing.T) {
		// Friday June 13, 2025 at 1:00 PM UTC.
		now := time.Date(2025, 6, 13, 13, 0, 0, 0, time.UTC)
		var history []types.EnergyStats

		// Generate 5 weeks of history.
		// Fridays will have high load (10.0) at hour 14 (2 PM).
		// Saturdays will have medium load (5.0) at hour 2 (2 AM).
		// Other days have baseline low load (1.0).
		startDate := now.Add(-35 * 24 * time.Hour)
		for d := 0; d < 35; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			wd := dayTime.Weekday()

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 1.0

				if wd == time.Friday && h == 14 {
					load = 10.0
				} else if wd == time.Saturday && h == 2 {
					load = 5.0
				}

				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0, // Disable hourly outlier filtering
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		if assert.Contains(t, model, 14) {
			assert.InDelta(t, 1.0, model[14].AvgHomeLoadKWH, 0.001)
		}
		if assert.Contains(t, model, 2) {
			assert.InDelta(t, 1.0, model[2].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("OutlierDayFiltering", func(t *testing.T) {
		// Test that a day with significantly below-average usage (like a vacation day)
		// is ignored using the IQR method.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			load := 2.0
			if idx == 0 {
				load = 0.2 // Outlier day (vacation)
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The vacation day should be filtered out, so the average load is 2.0.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 2.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("HighOutlierDayFiltering", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			load := 2.0
			if idx == 0 {
				load = 10.0 // Outlier day (abnormally high usage)
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The high outlier day should be filtered out, so the average load is 2.0.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 2.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("NoOutlierFilteringWhenFewDays", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		// 3 Mondays in history.
		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			load := 2.0
			if idx == 0 {
				load = 0.2
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50p median of [0.2, 2.0, 2.0] is 2.0.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 2.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("WeekdayWeekendFallback", func(t *testing.T) {
		// Saturday target. We have 1 Saturday, 2 Sundays, and 1 Monday.
		now := time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC) // Saturday

		var history []types.EnergyStats
		// 1 Saturday (load 3.0)
		sat := now.Add(-7 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(sat.Year(), sat.Month(), sat.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     3.0,
			})
		}
		// 2 Sundays (load 4.0)
		sun1 := now.Add(-6 * 24 * time.Hour)
		sun2 := now.Add(-13 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(sun1.Year(), sun1.Month(), sun1.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     4.0,
			})
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(sun2.Year(), sun2.Month(), sun2.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     4.0,
			})
		}
		// 1 Monday (load 1.0)
		mon := now.Add(-5 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(mon.Year(), mon.Month(), mon.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     1.0,
			})
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Saturday target hour 12:
		// Saturday count = 1 (< 3).
		// Fallback to Weekend group (Saturday + Sunday) count = 3 (>= 3).
		// With same-weekday weekly decay 0.90 and 2.5x multiplier, matching Saturday (load 3.0, weight 2.25)
		// correctly pulls the 50p median to 3.0 rather than being dominated by Sundays (4.0).
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 3.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("InadequateDaySpecificDataAllDaysFallback", func(t *testing.T) {
		// Target is Monday (weekday). We have 1 Monday (load 3.0), 1 Tuesday (load 4.0), and 1 Saturday (load 1.0).
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		// 1 Monday (load 3.0)
		mon := now.Add(-7 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(mon.Year(), mon.Month(), mon.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     3.0,
			})
		}
		// 1 Tuesday (load 4.0)
		tue := now.Add(-6 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(tue.Year(), tue.Month(), tue.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     4.0,
			})
		}
		// 1 Saturday (load 1.0)
		sat := now.Add(-2 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(sat.Year(), sat.Month(), sat.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     1.0,
			})
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Monday target hour 12:
		// Monday count = 1 (< 3).
		// Weekday group fallback (Monday + Tuesday) count = 2 (< 3).
		// Fallback to all valid days: Monday (3.0) + Tuesday (4.0) + Saturday (1.0) = 3 days (>= 3).
		// Weighted 50p median of [1.0, 3.0, 4.0] is 3.0.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 3.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("InfrequentChargingOutlier", func(t *testing.T) {
		// EV charged on only 1 out of 5 Mondays at 11am.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				load := 1.0
				if idx == 0 && h == 11 {
					load = 8.0 // Infrequent charging spike
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 3.0,
			IgnoreHourUsageFloorKWH:     0.5,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The 8.0 KWH charge should be filtered out because the median is 1.0, and limit is 3.0.
		// So the prediction should reflect the baseline (1.0).
		if assert.Contains(t, model, 11) {
			assert.InDelta(t, 1.0, model[11].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("FrequentChargingNoOutlier", func(t *testing.T) {
		// EV charged on all Mondays at 11am.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				load := 1.0
				if h == 11 {
					load = 8.0 // Frequent charging
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 3.0,
			IgnoreHourUsageFloorKWH:     0.5,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Adjacent hour blending at 55p captures the recurring elevated charging load.
		if assert.Contains(t, model, 11) {
			assert.InDelta(t, 5.202787294076979, model[11].AvgHomeLoadKWH, 0.001)
			assert.Greater(t, model[11].P75HomeLoadKWH, 1.0)
		}
	})

	t.Run("AdaptiveShiftShiftedUp", func(t *testing.T) {
		// History baseline for Mondays is 1.0 KWH.
		// The last 4 valid days have actual load 2.0 KWH.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),  // Mon 1 (recent) -> actual load 2.0
			now.Add(-14 * 24 * time.Hour), // Mon 2 (recent) -> actual load 2.0
			now.Add(-21 * 24 * time.Hour), // Mon 3 (recent) -> actual load 2.0
			now.Add(-28 * 24 * time.Hour), // Mon 4 (recent) -> actual load 2.0
			now.Add(-35 * 24 * time.Hour), // Mon 5 (baseline reference) -> load 1.0
			now.Add(-42 * 24 * time.Hour), // Mon 6 (baseline reference) -> load 1.0
			now.Add(-49 * 24 * time.Hour), // Mon 7 (baseline reference) -> load 1.0
			now.Add(-56 * 24 * time.Hour), // Mon 8 (baseline reference) -> load 1.0
		}

		for idx, m := range mondays {
			load := 1.0
			if idx < 4 {
				load = 2.0
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Expected baseline load for Mon is ~1.0 KWH.
		// Since the last 4 days consistently average 2.0 KWH, the forecast should shift up.
		if assert.Contains(t, model, 12) {
			assert.Greater(t, model[12].AvgHomeLoadKWH, 1.5)
		}
	})

	t.Run("AdaptiveShiftShiftedDown", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),  // Mon 1 (recent) -> actual load 1.0
			now.Add(-14 * 24 * time.Hour), // Mon 2 (recent) -> actual load 1.0
			now.Add(-21 * 24 * time.Hour), // Mon 3 (recent) -> actual load 1.0
			now.Add(-28 * 24 * time.Hour), // Mon 4 (recent) -> actual load 1.0
			now.Add(-35 * 24 * time.Hour), // Mon 5 (baseline reference) -> load 2.0
			now.Add(-42 * 24 * time.Hour), // Mon 6 (baseline reference) -> load 2.0
			now.Add(-49 * 24 * time.Hour), // Mon 7 (baseline reference) -> load 2.0
			now.Add(-56 * 24 * time.Hour), // Mon 8 (baseline reference) -> load 2.0
		}

		// Add standby baseline history
		history = append(history, types.EnergyStats{
			TSHourStart: now.Add(-60 * 24 * time.Hour),
			HomeKWH:     0.1,
		})

		for idx, m := range mondays {
			load := 2.0
			if idx < 4 {
				load = 1.2
			}
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Expected baseline load for Mon is ~2.0 KWH.
		// Since the last 4 days consistently average 1.0 KWH, the forecast should shift down.
		if assert.Contains(t, model, 12) {
			assert.Less(t, model[12].AvgHomeLoadKWH, 1.5)
		}
	})

	t.Run("AdaptiveShiftNoShiftNormal", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     1.5,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The last 4 days match the baseline exactly, so the shift should be 0.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 1.5, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("AdaptiveShiftTemperatureSimilarityAndStandbyProtection", func(t *testing.T) {
		// Tests that:
		// 1. When weather history has a seasonal drop (cool recent days vs warm historical days),
		//    temperature similarity weighting prevents a false large negative shift from double-counting.
		// 2. Standby protection ensures that even with a negative shift, overnight idle loads are never
		//    reduced below the empirical standby baseline.
		now := time.Date(2025, 10, 6, 12, 0, 0, 0, time.UTC) // Monday

		// Build 8 Mondays of history:
		// Older 4 Mondays: Warm summer days (28°C), active load 2.5 kWh, overnight idle 0.6 kWh.
		// Recent 4 Mondays: Cool autumn days (14°C), active load 1.2 kWh, overnight idle 0.6 kWh.
		var history []types.EnergyStats
		var weather []types.Weather

		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
			now.Add(-42 * 24 * time.Hour),
			now.Add(-49 * 24 * time.Hour),
			now.Add(-56 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			isRecent := idx < 4
			dayTemp := 28.0
			activeLoad := 2.5
			if isRecent {
				dayTemp = 14.0
				activeLoad = 1.2
			}

			var fHours []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				load := 0.6 // Standby idle
				if h >= 10 && h <= 20 {
					load = activeLoad
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
				fHours = append(fHours, types.HourlyWeather{
					TSHourStart:  ts,
					TemperatureC: dayTemp,
				})
			}
			weather = append(weather, types.Weather{
				TSDayStart:    time.Date(m.Year(), m.Month(), m.Day(), 0, 0, 0, 0, time.UTC),
				ForecastHours: fHours,
			})
		}

		// Today's forecast is also 14°C
		var todayFHours []types.HourlyWeather
		for h := 0; h < 24; h++ {
			todayFHours = append(todayFHours, types.HourlyWeather{
				TSHourStart:  time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				TemperatureC: 14.0,
			})
		}
		weather = append(weather, types.Weather{
			TSDayStart:    time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			ForecastHours: todayFHours,
		})

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 0.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// Overnight idle load (hour 3) must be protected by standby baseline (>= 0.6 kWh).
		if assert.Contains(t, model, 3) {
			assert.GreaterOrEqual(t, model[3].AvgHomeLoadKWH, 0.60)
		}
		// Daytime active load (hour 12) should be predicted near the cool day load (~1.2 kWh),
		// rather than being crushed to 0.5 kWh or pulled up to 2.5 kWh.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 1.2, model[12].AvgHomeLoadKWH, 0.25)
		}
	})

	t.Run("AdaptiveShiftRejectsIsolatedSpikeWhenOtherDaysDropAccountedByWeather", func(t *testing.T) {
		// When recent days are cooler than historical summer days, their lower consumption is
		// accounted for by weather similarity. If ONE of the recent 4 days has a sporadic spike
		// (e.g. EV charge or high usage day), Option G must NOT discard the other 3 days and treat
		// the single spike as a consistent permanent shift.
		now := time.Date(2025, 10, 6, 0, 0, 0, 0, time.UTC)

		// 8 days of history: 4 older summer days (28°C, load 2.5 kWh),
		// 3 recent cool days (14°C, load 0.9 kWh), and 1 recent spike day (14°C, load 3.2 kWh).
		var history []types.EnergyStats
		var weather []types.Weather

		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),  // Recent day 1: cool (14°C), normal low load (0.9 kWh)
			now.Add(-14 * 24 * time.Hour), // Recent day 2: cool (14°C), normal low load (0.9 kWh)
			now.Add(-21 * 24 * time.Hour), // Recent day 3: cool (14°C), normal low load (0.9 kWh)
			now.Add(-28 * 24 * time.Hour), // Recent day 4: cool (14°C), SPORADIC SPIKE (3.2 kWh)
			now.Add(-35 * 24 * time.Hour), // Historical summer (28°C), 2.5 kWh
			now.Add(-42 * 24 * time.Hour), // Historical summer (28°C), 2.5 kWh
			now.Add(-49 * 24 * time.Hour), // Historical summer (28°C), 2.5 kWh
			now.Add(-56 * 24 * time.Hour), // Historical summer (28°C), 2.5 kWh
		}

		for idx, m := range mondays {
			dayTemp := 28.0
			activeLoad := 2.5
			if idx < 4 {
				dayTemp = 14.0
				if idx == 3 {
					activeLoad = 3.2 // Isolated spike on day 4
				} else {
					activeLoad = 1.0 // Normal cool usage
				}
			}

			var fHours []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				load := 0.6 // Standby idle
				if h >= 10 && h <= 20 {
					load = activeLoad
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
				fHours = append(fHours, types.HourlyWeather{
					TSHourStart:  ts,
					TemperatureC: dayTemp,
				})
			}
			weather = append(weather, types.Weather{
				TSDayStart:    time.Date(m.Year(), m.Month(), m.Day(), 0, 0, 0, 0, time.UTC),
				ForecastHours: fHours,
			})
		}

		// Today is 14°C
		var todayFHours []types.HourlyWeather
		for h := 0; h < 24; h++ {
			todayFHours = append(todayFHours, types.HourlyWeather{
				TSHourStart:  time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				TemperatureC: 14.0,
			})
		}
		weather = append(weather, types.Weather{
			TSDayStart:    time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			ForecastHours: todayFHours,
		})

		settings := types.Settings{}
		model, simParams := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// The single day spike must NOT trigger an adaptive shift.
		assert.Equal(t, "none", simParams.DetectedShift)
		// Overnight load should remain near 0.9 kWh, not inflated to 2.2+ kWh.
		if assert.Contains(t, model, 3) {
			assert.Less(t, model[3].AvgHomeLoadKWH, 1.3)
		}
	})

	t.Run("MultipleHighSpikesRetained", func(t *testing.T) {
		// EV charged on 2 out of 5 Mondays at 11am.
		// Since there are multiple high spikes (not exactly one outlier),
		// they should both be retained.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
			now.Add(-28 * 24 * time.Hour),
			now.Add(-35 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				load := 1.0
				if (idx == 0 || idx == 1) && h == 11 {
					load = 8.0 // Consistent/frequent charge on 2 Mondays
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 3.0,
			IgnoreHourUsageFloorKWH:     0.5,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50p median of the blended pool is 1.0.
		if assert.Contains(t, model, 11) {
			assert.InDelta(t, 1.0, model[11].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("LowSpikeFloor", func(t *testing.T) {
		// Verify that small hourly loads (e.g. 0.3 KWH when other days are 0.1 KWH)
		// are not incorrectly flagged as outliers because they are below the safety floor.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for idx, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				load := 0.1
				if idx == 0 && h == 11 {
					load = 0.3 // Higher but small spike
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 2.0,
			IgnoreHourUsageFloorKWH:     0.5, // Floor is 0.5, so 0.3 is not filtered since 0.3 <= Max(0.1, 0.5) * 2
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50p median of [0.1, 0.1, 0.3] is 0.1.
		if assert.Contains(t, model, 11) {
			assert.InDelta(t, 0.1, model[11].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("WeekdaySpecificSpikeRetained", func(t *testing.T) {
		// Thursdays have a consistent laundry peak at 7 PM to 3.0 kWh.
		// Other weekdays are 1.0 kWh.
		// The target day is Thursday, so the prediction should be based on Thursdays (where peak is kept).
		now := time.Date(2025, 6, 19, 12, 0, 0, 0, time.UTC) // Thursday

		var history []types.EnergyStats
		for week := 1; week <= 5; week++ {
			// Truncate to start at midnight of the day so hours 0-23 map correctly to the same calendar day.
			dayStart := now.Add(time.Duration(-week*7*24) * time.Hour).Truncate(24 * time.Hour)
			// Target Thursday
			for h := 0; h < 24; h++ {
				load := 1.0
				if h == 19 {
					load = 3.0 // Consistent Thursday peak
				}
				history = append(history, types.EnergyStats{
					TSHourStart: dayStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:     load,
				})
			}
			// Other weekday (Monday)
			monStart := dayStart.Add(-3 * 24 * time.Hour).Truncate(24 * time.Hour)
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart: monStart.Add(time.Duration(h) * time.Hour),
					HomeKWH:     1.0,
				})
			}
		}

		settings := types.Settings{
			IgnoreHourUsageOverMultiple: 2.0,
			IgnoreHourUsageFloorKWH:     0.5,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Monday at 19:00 remains near 1.0 KWH baseline.
		if assert.Contains(t, model, 19) {
			assert.InDelta(t, 1.0, model[19].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("Basic Average with Filter", func(t *testing.T) {
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		h2 := h1.Add(-24 * time.Hour)
		h3 := h1.Add(-48 * time.Hour)

		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 3.0, SolarKWH: 0.0},
			{TSHourStart: h3, HomeKWH: 0.0, SolarKWH: 0.0}, // Should be filtered (<= 0.0)
		}

		testNow := time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC)
		model, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{IgnoreHourUsageOverMultiple: 0.0})
		assert.InDelta(t, 2.1743589743589746, model[h1.Hour()].AvgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].AvgSolarKWH, 0.001)
	})

	t.Run("Basic Average All Low", func(t *testing.T) {
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 0.0, SolarKWH: 0.0},
		}

		testNow := time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC)
		model, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{IgnoreHourUsageOverMultiple: 0.0})
		assert.InDelta(t, 0.09, model[h1.Hour()].AvgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].AvgSolarKWH, 0.001)
	})

	t.Run("Sparse History Falls Back To Overall Site Average", func(t *testing.T) {
		h1 := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC) // Wednesday
		h2 := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC) // Thursday
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 2.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 2.0, SolarKWH: 0.0},
		}

		testNow := time.Date(2026, 7, 3, 18, 0, 0, 0, time.UTC) // Friday hour 18
		model, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{IgnoreHourUsageOverMultiple: 0.0})
		assert.InDelta(t, 2.0, model[18].AvgHomeLoadKWH, 0.001)
	})

	t.Run("Ignore Outliers", func(t *testing.T) {
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		h2 := h1.Add(-24 * time.Hour)
		h3 := h1.Add(-48 * time.Hour)

		// Case 1: Exactly 1 outlier above 3x
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 1.2, SolarKWH: 0.0},
			{TSHourStart: h3, HomeKWH: 10.0, SolarKWH: 0.0}, // Outlier
		}
		testNow := time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC)
		model, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		assert.InDelta(t, 10.0, model[h1.Hour()].AvgHomeLoadKWH, 0.001)

		// Case 2: Multiple outliers (not removed)
		historyMulti := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0}, // Outlier 1
			{TSHourStart: h3, HomeKWH: 12.0, SolarKWH: 0.0}, // Outlier 2
		}
		modelMulti, _ := c.BuildHourlyEnergyModel(ctx, testNow, historyMulti, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		assert.InDelta(t, 12.0, modelMulti[h1.Hour()].AvgHomeLoadKWH, 0.001)

		// Case 3: Not enough points (min 3)
		historyFew := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0},
		}
		modelFew, _ := c.BuildHourlyEnergyModel(ctx, testNow, historyFew, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		assert.InDelta(t, 6.284615384615385, modelFew[h1.Hour()].AvgHomeLoadKWH, 0.001)
	})

	t.Run("PostVacationRecovery", func(t *testing.T) {
		// Verify that historical vacation days are excluded when detectedShift == "none" (normal mode),
		// ensuring load predictions immediately recover to normal baseline levels on Day +1 after returning from vacation.
		c := NewController()
		ctx := context.Background()

		// Construct history: 14 normal days (2.0 kWh/hr), followed by 7 vacation days (0.3 kWh/hr)
		baseDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats

		// 14 normal days (July 1 - July 14)
		for d := 0; d < 14; d++ {
			dayTime := baseDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart: dayTime.Add(time.Duration(h) * time.Hour),
					HomeKWH:     2.0,
				})
			}
		}

		// 7 vacation days (July 15 - July 21)
		for d := 14; d < 21; d++ {
			dayTime := baseDate.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart: dayTime.Add(time.Duration(h) * time.Hour),
					HomeKWH:     0.3,
				})
			}
		}

		t.Run("DayPlusOne", func(t *testing.T) {
			// Day 1 after returning from vacation (July 22 at 12:00 PM).
			// Today (July 22) has high morning consumption (2.0 kWh/hr), so detectedShift is "none".
			nowDayPlusOne := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

			// Add today's morning points (7 AM to 11 AM at 2.0 kWh/hr)
			var fullHistory []types.EnergyStats
			fullHistory = append(fullHistory, history...)
			for h := 0; h < 12; h++ {
				fullHistory = append(fullHistory, types.EnergyStats{
					TSHourStart: nowDayPlusOne.Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:     2.0,
				})
			}

			model, params := c.BuildHourlyEnergyModel(ctx, nowDayPlusOne, fullHistory, nil, types.Settings{})
			assert.Equal(t, "none", params.DetectedShift)

			// Because past vacation days (July 15-21) are excluded from the prediction pool,
			// the predicted load for hour 14 is close to normal baseline 2.0 kWh/hr (not dragged down to ~0.3)
			assert.GreaterOrEqual(t, model[14].AvgHomeLoadKWH, 1.8)
		})

		t.Run("DayPlusTwo", func(t *testing.T) {
			// Day 2 after returning from vacation (July 23 at 12:00 PM).
			nowDayPlusTwo := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

			// Add July 22 (full normal day) and July 23 morning
			var fullHistory []types.EnergyStats
			fullHistory = append(fullHistory, history...)
			for h := 0; h < 24; h++ {
				fullHistory = append(fullHistory, types.EnergyStats{
					TSHourStart: time.Date(2026, 7, 22, h, 0, 0, 0, time.UTC),
					HomeKWH:     2.0,
				})
			}
			for h := 0; h < 12; h++ {
				fullHistory = append(fullHistory, types.EnergyStats{
					TSHourStart: nowDayPlusTwo.Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:     2.0,
				})
			}

			model, params := c.BuildHourlyEnergyModel(ctx, nowDayPlusTwo, fullHistory, nil, types.Settings{})
			assert.Equal(t, "none", params.DetectedShift)
			assert.GreaterOrEqual(t, model[14].AvgHomeLoadKWH, 1.8)
		})

		t.Run("ActiveVacationPreservation", func(t *testing.T) {
			// While ON vacation (e.g. July 17 at 12:00 PM), yesterday (July 16) was low and today morning is low.
			// detectedShift is "down". Past vacation days ARE retained to keep predictions low.
			nowOnVacation := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

			var fullHistory []types.EnergyStats
			// Include up to July 16 full day and July 17 morning (0.3 kWh/hr)
			for d := 0; d < 16; d++ {
				dayTime := baseDate.AddDate(0, 0, d)
				val := 2.0
				if d >= 14 {
					val = 0.3
				}
				for h := 0; h < 24; h++ {
					fullHistory = append(fullHistory, types.EnergyStats{
						TSHourStart: dayTime.Add(time.Duration(h) * time.Hour),
						HomeKWH:     val,
					})
				}
			}
			for h := 0; h < 12; h++ {
				fullHistory = append(fullHistory, types.EnergyStats{
					TSHourStart: nowOnVacation.Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour),
					HomeKWH:     0.3,
				})
			}

			model, params := c.BuildHourlyEnergyModel(ctx, nowOnVacation, fullHistory, nil, types.Settings{})
			assert.Equal(t, "down", params.DetectedShift)
			// While on vacation, load prediction remains low (< 0.5 kWh/hr)
			assert.LessOrEqual(t, model[14].AvgHomeLoadKWH, 0.5)
		})
	})

	t.Run("Smoothes Solar With Bell Curve", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-24 * time.Hour)

		history := []types.EnergyStats{}
		mu := 13.5
		sigma := 5.0
		peak := 5.0

		for h := 0; h < 24; h++ {
			solar := 0.0
			if h >= 6 && h <= 20 {
				solar = peak * math.Exp(-math.Pow(float64(h)-mu, 2)/(2*math.Pow(sigma, 2)))
			}

			batterySOC := 50.0
			gridExport := 0.0
			if h >= 11 && h <= 15 {
				batterySOC = 99.0
				solar = 0.5 // Curtailed
			} else {
				if solar > 0 {
					batterySOC = 80.0
				}
			}

			history = append(history, types.EnergyStats{
				TSHourStart:   start.Add(time.Duration(h) * time.Hour),
				SolarKWH:      solar,
				HomeKWH:       0.5,
				MaxBatterySOC: batterySOC,
				GridExportKWH: gridExport,
			})
		}

		settings := types.Settings{
			GridExportSolar:          false,
			SolarBellCurveMultiplier: 1.0,
		}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.Greater(t, model[13].AvgSolarKWH, 4.8, "Should reconstruct bell curve peak to ~5.0 using off-peak data")
		assert.Less(t, model[13].AvgSolarKWH, 5.2, "Should be around 5.0")

		for i := range history {
			history[i].SolarKWH = 0.05
		}
		modelNoData, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)
		assert.Equal(t, 0.0, modelNoData[13].AvgSolarKWH)
	})

	t.Run("No Daylight In History", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-24 * time.Hour)

		history := []types.EnergyStats{}
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: start.Add(time.Duration(h) * time.Hour),
				SolarKWH:    0.0,
				HomeKWH:     1.0,
			})
		}

		settings := types.Settings{GridExportSolar: false, SolarBellCurveMultiplier: 1.0}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		for h := 0; h < 24; h++ {
			assert.InDelta(t, 0.0, model[h].AvgSolarKWH, 0.001, "Hour %d should have no solar", h)
		}
	})

	t.Run("Fallback To Raw Data When All Curtailed", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-24 * time.Hour)

		history := []types.EnergyStats{}
		for h := 0; h < 24; h++ {
			solar := 0.0
			if h >= 8 && h <= 18 {
				solar = 3.0
			}

			history = append(history, types.EnergyStats{
				TSHourStart:   start.Add(time.Duration(h) * time.Hour),
				SolarKWH:      solar,
				HomeKWH:       1.0,
				MaxBatterySOC: 99.0,
				GridExportKWH: 0.0,
			})
		}

		settings := types.Settings{GridExportSolar: false, SolarBellCurveMultiplier: 1.0}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.GreaterOrEqual(t, model[13].AvgSolarKWH, 3.0, "Should use fallback data and maintain at least the raw average")
	})

	t.Run("Noisy Edge Data", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-24 * time.Hour)

		history := []types.EnergyStats{}
		mu := 13.5
		sigma := 11.0 / 3.0

		for h := 0; h < 24; h++ {
			solar := 0.0
			hourFactor := math.Exp(-math.Pow(float64(h)-mu, 2) / (2 * math.Pow(sigma, 2)))

			if h >= 8 && h <= 18 {
				solar = 2.0 * hourFactor
			}

			if h == 9 {
				solar = 10.0
			}

			history = append(history, types.EnergyStats{
				TSHourStart:   start.Add(time.Duration(h) * time.Hour),
				SolarKWH:      solar,
				HomeKWH:       1.0,
				MaxBatterySOC: 50.0,
				GridExportKWH: 2.0,
			})
		}

		settings := types.Settings{GridExportSolar: true, SolarBellCurveMultiplier: 1.0}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.Less(t, model[13].AvgSolarKWH, 25.0, "Peak should not explode from noisy edge data")
		assert.Greater(t, model[13].AvgSolarKWH, 5.0, "Should still boost above the baseline peak of 2.0")
	})

	t.Run("Solar Peak Estimation With Outliers", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-7 * 24 * time.Hour)

		history := []types.EnergyStats{}
		mu := 13.5
		sigma := 5.0

		for day := 0; day < 6; day++ {
			peak := 5.0
			if day == 0 {
				peak = 10.0
			}

			dayStart := start.Add(time.Duration(day) * 24 * time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 6 && h <= 20 {
					solar = peak * math.Exp(-math.Pow(float64(h)-mu, 2)/(2*math.Pow(sigma, 2)))
				}

				history = append(history, types.EnergyStats{
					TSHourStart:   dayStart.Add(time.Duration(h) * time.Hour),
					SolarKWH:      solar,
					HomeKWH:       0.5,
					MaxBatterySOC: 50.0,
					GridExportKWH: 2.0,
				})
			}
		}

		settings := types.Settings{
			GridExportSolar:          true,
			SolarBellCurveMultiplier: 1.0,
		}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.Less(t, model[13].AvgSolarKWH, 7.0, "Should be closer to 5.0 than 10.0")
		assert.Greater(t, model[13].AvgSolarKWH, 5.0, "Should capture the average including outlier")
	})

	t.Run("RecencyDecayInfluence", func(t *testing.T) {
		// Monday target. We have two historical Mondays in the selected weekday dates.
		// One Monday is 7 days ago, having load 8.0.
		// Another Monday is 28 days ago, having load 2.0.
		// The age-decayed weights should give the recent day much higher influence.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		// Monday 7 days ago
		m7 := now.Add(-7 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(m7.Year(), m7.Month(), m7.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     8.0,
			})
		}
		// Monday 28 days ago
		m28 := now.Add(-28 * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(m28.Year(), m28.Month(), m28.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     2.0,
			})
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The 50p median of [2.0 (weight 28-day decay), 8.0 (weight 7-day decay)]
		// will be biased towards 8.0 because the 7-day weight is much larger.
		// We assert it is greater than 5.0 (the midpoint).
		if assert.Contains(t, model, 12) {
			assert.Greater(t, model[12].AvgHomeLoadKWH, 5.0)
		}
	})

	t.Run("GatedHeatwaveSafeguard", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		// 3 identical historical Mondays, all with load 1.0 and temp 25.0C
		var history []types.EnergyStats
		for week := 1; week <= 3; week++ {
			dTime := now.Add(time.Duration(-week*7*24) * time.Hour)
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     1.0,
				})
			}
		}

		// Historical weather for these days: 25.0C constant
		var weather []types.Weather
		for week := 1; week <= 3; week++ {
			dTime := now.Add(time.Duration(-week*7*24) * time.Hour)
			var forecastHours []types.HourlyWeather
			for h := 0; h < 24; h++ {
				forecastHours = append(forecastHours, types.HourlyWeather{
					TSHourStart:  time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, time.UTC),
					TemperatureC: 25.0,
				})
			}
			weather = append(weather, types.Weather{
				TSDayStart:    time.Date(dTime.Year(), dTime.Month(), dTime.Day(), 0, 0, 0, 0, time.UTC),
				ForecastHours: forecastHours,
			})
		}

		// Add today's weather forecast: extreme heatwave of 31.0C at hour 12 (which is > 25.0 + 2.0 and > 28.0)
		var todayForecastHours []types.HourlyWeather
		for h := 0; h < 24; h++ {
			temp := 25.0
			if h == 12 {
				temp = 31.0 // 6C hotter than historical max (25.0C)
			}
			todayForecastHours = append(todayForecastHours, types.HourlyWeather{
				TSHourStart:  time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				TemperatureC: temp,
			})
		}
		weather = append(weather, types.Weather{
			TSDayStart:    time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			ForecastHours: todayForecastHours,
		})

		settings := types.Settings{}

		// 1. Extreme Heatwave: Today is 31.0C, historical max is 25.0C.
		// This is > 2.0C threshold and > 28.0C min temp gate.
		// Expected boost: 1.0 * 1.2 = 1.2.
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 1.2, model[12].AvgHomeLoadKWH, 0.001)
		}

		// 2. Not Hot Enough for Gate: Today is 27.0C at hour 12 (historically max is 24.0C).
		// Difference is 3.0C (> 2.0C threshold), but 27.0C is below 28.0C min temp gate.
		// Expected boost: None (1.0).
		var mildWeather []types.Weather
		mildWeather = append(mildWeather, weather[:3]...)
		var todayMildForecast []types.HourlyWeather
		for h := 0; h < 24; h++ {
			temp := 24.0
			if h == 12 {
				temp = 27.0
			}
			todayMildForecast = append(todayMildForecast, types.HourlyWeather{
				TSHourStart:  time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				TemperatureC: temp,
			})
		}
		mildWeather = append(mildWeather, types.Weather{
			TSDayStart:    time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			ForecastHours: todayMildForecast,
		})

		modelMild, _ := c.BuildHourlyEnergyModel(ctx, now, history, mildWeather, settings)
		if assert.Contains(t, modelMild, 12) {
			assert.InDelta(t, 1.0, modelMild[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("VacationModeShift", func(t *testing.T) {
		// History baseline has load 2.0.
		// Yesterday is low (0.5), and today is also low (0.5).
		// Vacation mode should trigger, bypass IQR, and decay to 0.30.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday, 12 PM

		var history []types.EnergyStats
		startDate := now.Add(-35 * 24 * time.Hour)
		for d := 0; d < 35; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			dateStr := dayTime.Format("2006-01-02")
			todayStr := now.Format("2006-01-02")
			yesterdayStr := now.Add(-24 * time.Hour).Format("2006-01-02")

			load := 2.0
			if dateStr == yesterdayStr {
				load = 0.5
			} else if dateStr == todayStr {
				continue
			}

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		// Today's loads before 12 PM are also low (0.5)
		for h := 0; h < 12; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Model for remaining hours (e.g. 15:00) should be close to 0.5 (standby), not 2.0.
		if assert.Contains(t, model, 15) {
			assert.Less(t, model[15].AvgHomeLoadKWH, 1.0)
		}
	})

	t.Run("DayOfWeekMorningOutlierBoost", func(t *testing.T) {
		// Saturday morning chores: 5 weeks of history where Saturdays have high morning load (3.5 kWh)
		// during hours 7..9, while preceding weekdays (Thu/Fri) have baseline morning load (1.0 kWh).
		// The model should identify Saturday as a morning outlier (ratio >= 1.25), scale matching Saturday
		// weights by dayOfWeekOutlierWeightMultiplier (3.0x), down-weight preceding non-matching days,
		// and predict close to the true Saturday chore load rather than being diluted by weekday mornings.
		now := time.Date(2025, 6, 14, 6, 0, 0, 0, time.UTC) // Saturday at 6:00 AM
		var history []types.EnergyStats

		startDate := now.Add(-35 * 24 * time.Hour)
		for d := 0; d < 35; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			wd := dayTime.Weekday()

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 0.2 // Standby overnight
				if h >= 6 && h <= 10 {
					if wd == time.Saturday {
						load = 2.5 // Saturday morning surge (2.5x vs 1.0 weekday)
					} else {
						load = 1.0 // Normal weekday morning
					}
				} else if h > 10 {
					load = 1.0 + 0.1*float64(d%6) // Natural baseline variation across days
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Hour 8 (Saturday morning chore peak) should be close to 2.5 kWh, well above the 1.0 weekday level.
		if assert.Contains(t, model, 8) {
			assert.GreaterOrEqual(t, model[8].AvgHomeLoadKWH, 2.0)
		}
	})

	t.Run("StandbyNeighborSleepFilter", func(t *testing.T) {
		// Verify that sleeping hours (06:00 at 0.15 kWh standby) are excluded by
		// neighborHourStandbyFloorMultiplier (1.8x) from diluting the adjacent waking hour (07:00 at 2.5 kWh).
		now := time.Date(2025, 6, 14, 6, 0, 0, 0, time.UTC) // Saturday at 6:00 AM
		var history []types.EnergyStats

		startDate := now.Add(-28 * 24 * time.Hour)
		for d := 0; d < 28; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 0.15 // Standby base
				if h == 6 {
					load = 0.15 // Sleep hour
				} else if h >= 7 && h <= 10 {
					load = 2.5 // Waking hours
				} else if h > 10 {
					load = 1.5
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Hour 7 should not be diluted down by hour 6's 0.15 kWh sleeping load.
		if assert.Contains(t, model, 7) {
			assert.GreaterOrEqual(t, model[7].AvgHomeLoadKWH, 2.0)
		}
	})

	t.Run("SundayOutlierWithThuFriFallback", func(t *testing.T) {
		// Test Sunday morning outlier where Saturday is ALSO high (both weekend mornings are 3.0 kWh),
		// while weekdays (Thu/Fri) are baseline (1.0 kWh).
		// Sunday morning vs Saturday morning has ratio ~1.0, but vs Thursday/Friday has ratio ~3.0.
		// Sunday should trigger the Thursday/Friday fallback and receive the outlier boost.
		now := time.Date(2025, 6, 15, 6, 0, 0, 0, time.UTC) // Sunday at 6:00 AM
		var history []types.EnergyStats

		startDate := now.Add(-35 * 24 * time.Hour)
		for d := 0; d < 35; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			wd := dayTime.Weekday()

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 0.2
				if h >= 6 && h <= 10 {
					if wd == time.Saturday || wd == time.Sunday {
						load = 3.0 // High weekend routine
					} else {
						load = 1.0 // Normal weekday morning
					}
				} else if h > 10 {
					load = 1.2 + 0.05*float64(d%5)
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Sunday morning peak (hour 8) should be boosted to near weekend level (>= 2.2 kWh)
		if assert.Contains(t, model, 8) {
			assert.GreaterOrEqual(t, model[8].AvgHomeLoadKWH, 2.2)
		}
	})

	t.Run("WeekdayOutlierDetection", func(t *testing.T) {
		// Test generic weekday outlier: Wednesday has consistent morning chore routine (3.0 kWh),
		// while Mon/Tue/Thu/Fri have standard load (1.0 kWh).
		now := time.Date(2025, 6, 18, 6, 0, 0, 0, time.UTC) // Wednesday at 6:00 AM
		var history []types.EnergyStats

		startDate := now.Add(-35 * 24 * time.Hour)
		for d := 0; d < 35; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			wd := dayTime.Weekday()

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 0.2
				if h >= 6 && h <= 10 {
					if wd == time.Wednesday {
						load = 3.0 // Wednesday chore surge
					} else {
						load = 1.0 // Other days standard
					}
				} else if h > 10 {
					load = 1.0 + 0.15*float64(d%5) // Natural daily variation across all days
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Wednesday morning peak (hour 8) should reflect the Wednesday surge (>= 2.2 kWh)
		if assert.Contains(t, model, 8) {
			assert.GreaterOrEqual(t, model[8].AvgHomeLoadKWH, 2.2)
		}
	})

	t.Run("UniformDaysNoOutlierBoost", func(t *testing.T) {
		// When all days have uniform morning load (1.0 kWh), no day should qualify as an outlier.
		// Standard weights should produce ~1.0 kWh.
		now := time.Date(2025, 6, 14, 6, 0, 0, 0, time.UTC) // Saturday at 6:00 AM
		var history []types.EnergyStats

		startDate := now.Add(-28 * 24 * time.Hour)
		for d := 0; d < 28; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 0.2
				if h >= 6 && h <= 10 {
					load = 1.0 // Uniform across all days
				} else if h > 10 {
					load = 1.2
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		if assert.Contains(t, model, 8) {
			assert.InDelta(t, 1.0, model[8].AvgHomeLoadKWH, 0.2)
		}
	})

	t.Run("VacationShiftOverridesOutlierBoost", func(t *testing.T) {
		// Saturday is historically an outlier (3.0 kWh mornings), but yesterday (Friday) was a vacation
		// day (low standby only, ~0.08 kWh), and earlier this morning (hours 0..5) is also standby.
		// The active vacation shift should take precedence over the day-of-week outlier boost.
		now := time.Date(2025, 6, 14, 6, 0, 0, 0, time.UTC) // Saturday at 6:00 AM
		var history []types.EnergyStats

		startDate := now.Add(-28 * 24 * time.Hour)
		for d := 0; d < 28; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			wd := dayTime.Weekday()
			isYesterday := d == 27 // Last day in history (Friday)

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 0.08 // Standby baseline
				if isYesterday {
					// Vacation day: empty home all day
					load = 0.08
				} else {
					if h >= 6 && h <= 10 {
						if wd == time.Saturday {
							load = 3.0 // Historical Saturday morning
						} else {
							load = 1.0
						}
					} else if h > 10 {
						load = 1.5 + 0.1*float64(d%4)
					}
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		// Also include early morning hours of today (Saturday) prior to 6:00 AM (0..5 at standby 0.08 kWh)
		for h := 0; h < 6; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 14, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.08,
			})
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// On vacation, Saturday morning (hour 8) should NOT be inflated to 2.5-3.0 kWh;
		// it should drop toward vacation levels (< 1.0 kWh).
		if assert.Contains(t, model, 8) {
			assert.Less(t, model[8].AvgHomeLoadKWH, 1.0)
		}
	})
}

func TestDetectLoadShift(t *testing.T) {
	ctx := context.Background()
	loc := time.UTC

	// Helper to generate baseline history data
	createBaseData := func(baselineAvg float64) (map[string]*dayPoints, map[string]float64) {
		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)
		startDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 33; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			dateStr := dayTime.Format("2006-01-02")
			dayAveragesMap[dateStr] = baselineAvg
			var pts []types.EnergyStats
			var loads []float64
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     baselineAvg,
				})
				loads = append(loads, baselineAvg)
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts, loads: loads}
		}
		return dayMap, dayAveragesMap
	}

	t.Run("VacationModeDetection", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // 12 PM
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is low (0.5)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		var yLoads []float64
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
			yLoads = append(yLoads, 0.5)
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: yLoads}

		// Today before 12 PM has low load (0.5)
		var tPts []types.EnergyStats
		var tLoads []float64
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
			tLoads = append(tLoads, 0.5)
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: tLoads}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "down", shift)
	})
	t.Run("FourHourEscape", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is low (0.5)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		var yLoads []float64
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
			yLoads = append(yLoads, 0.5)
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: yLoads}

		// Today's loads: Hours 0-7 low (0.5), Hours 8-11 normal (2.0)
		var tPts []types.EnergyStats
		var tLoads []float64
		for h := 0; h < 12; h++ {
			load := 0.5
			if h >= 8 {
				load = 2.0
			}
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     load,
			})
			tLoads = append(tLoads, load)
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: tLoads}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("NoOutlierYesterday_NoShift", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is normal (2.0) - not an outlier
		dayAveragesMap[yesterdayStr] = 2.0
		var yPts []types.EnergyStats
		var yLoads []float64
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     2.0,
			})
			yLoads = append(yLoads, 2.0)
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: yLoads}

		// Today is low (0.5)
		var tPts []types.EnergyStats
		var tLoads []float64
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
			tLoads = append(tLoads, 0.5)
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: tLoads}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("MissingTelemetry_NoShift", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is low (0.5)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		var yLoads []float64
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
			yLoads = append(yLoads, 0.5)
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: yLoads}

		// Today has no data (missing telemetry)
		var tPts []types.EnergyStats
		var tLoads []float64
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: tLoads}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("NotEnoughBaselineDays", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		// Only 2 baseline days (less than dailyAveragesRequired = 4)
		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		for d := 0; d < 2; d++ {
			dayTime := time.Date(2025, 6, 10+d, 0, 0, 0, 0, time.UTC)
			dateStr := dayTime.Format("2006-01-02")
			dayAveragesMap[dateStr] = 2.0
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     2.0,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts, loads: []float64{2.0}}
		}

		// Yesterday is low (0.5)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: []float64{0.5}}

		// Today is low (0.5)
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: []float64{0.5}}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("OutlierHoursBefore9AM_Contradiction", func(t *testing.T) {
		// Run at 8:00 AM. Yesterday is low (0.5).
		// But today at 5:00 AM we had a high load (4.0), which contradicts vacation.
		now := time.Date(2025, 6, 16, 8, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is low (0.5)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: []float64{0.5}}

		// Today has low loads except Hour 5 which has 4.0
		var tPts []types.EnergyStats
		for h := 0; h < 8; h++ {
			load := 0.5
			if h == 5 {
				load = 4.0 // contradicts vacation
			}
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     load,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: []float64{0.5}}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 8, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("OutlierHoursAtEndOfTheDayYesterday_EarlyMorningCheck", func(t *testing.T) {
		// Run at 2:00 AM. Yesterday is low (0.5) overall.
		// But at 11:00 PM yesterday (within the 6-hour lookback), there was a high spike (4.0).
		now := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday average is low (0.5) but contains a late night spike of 4.0 at Hour 23
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			load := 0.5
			if h == 23 {
				load = 4.0 // contradicts vacation
			}
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     load,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: []float64{0.5}}

		// Today's loads before 2 AM: low (0.5)
		var tPts []types.EnergyStats
		for h := 0; h < 2; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: []float64{0.5}}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 2, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("TelemetryGapInLookback", func(t *testing.T) {
		// Run at 2:00 AM. Yesterday is low (0.5).
		// We only have 3 completed hours of data (missing hours in dayMap points).
		now := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is low (0.5)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		// Missing Hour 23, Hour 22, Hour 21 to create a gap!
		for h := 0; h < 20; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: []float64{0.5}}

		// Today's loads before 2 AM: low (0.5)
		var tPts []types.EnergyStats
		for h := 0; h < 2; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: []float64{0.5}}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 2, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("VacationFloorContained", func(t *testing.T) {
		// Tests that a vacation still triggers under a high-variance (high IQR) history
		// because of the 0.02 absolute floor.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// Create 6 highly variable baseline days: 1.0, 1.5, 8.0, 10.0, 2.0, 5.0
		// Sorted: 1.0, 1.5, 2.0, 5.0, 8.0, 10.0
		// Q1 = 1.5, Q3 = 8.0, IQR = 6.5
		// Q1 - 1.2*IQR = -6.3 -> Capped at 0.02 absolute floor.
		baselineVals := []float64{1.0, 1.5, 8.0, 10.0, 2.0, 5.0}
		for i, val := range baselineVals {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = val
			// Populate hourly loads so baselineSums calculation works
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     val, // Flat load equal to daily average
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday is low (0.1 -> active energy 0.0, which is < 0.02 bound)
		dayAveragesMap[yesterdayStr] = 0.1
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.1,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today is also low (0.1 -> active energy 0.0, which is < 0.02 bound)
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.1,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		// Standby load is 0.1
		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "down", shift)
	})

	t.Run("VacationCeilingContained", func(t *testing.T) {
		// Tests that a small dip (e.g. 10%) on a highly consistent baseline (IQR = 0)
		// does NOT trigger a vacation because of the q1 * 0.4 ceiling constraint.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// Create 6 highly consistent baseline days: 2.1 (all equal, standby 0.1 -> active average 2.0)
		// Q1 = 2.0, IQR = 0.
		// Standard formula: Q1 - 1.2*IQR = 2.0.
		// Ceiling constraint: Q1 * 0.4 = 0.8.
		// Lower bound becomes 0.8.
		for i := 0; i < 6; i++ {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = 2.1
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     2.1,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday average has a 10% dip: 1.9 (active average 1.8).
		// Since 1.8 is NOT < 0.8, it should NOT trigger a vacation.
		dayAveragesMap[yesterdayStr] = 1.9
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     1.9,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today also has 1.9 load
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     1.9,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("OutlierFilteringBaselineDays", func(t *testing.T) {
		// Tests that a past high visitor day is correctly filtered out from baselineDays.
		// If we only provide 4 historical baseline days and 1 is a high outlier, it gets
		// filtered, leaving only 3 baseline days, which is less than dailyAveragesRequired (4),
		// so it should immediately abort and return "none".
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 3 normal days (2.1 average -> 2.0 active) and 1 visitor day (10.1 average -> 10.0 active)
		// Q1 = 2.0, Q3 = 2.0 (since 3 out of 4 are 2.0), IQR = 0.
		// Upper bound is Q3 + 1.2*IQR = 2.0. The 10.0 active day is a high outlier (> 2.0) and should be filtered out.
		baselineVals := []float64{2.1, 2.1, 2.1, 10.1}
		for i, val := range baselineVals {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = val
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     val,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday is low (0.1 average -> 0.0 active)
		dayAveragesMap[yesterdayStr] = 0.1
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.1,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today is low (0.1 average -> 0.0 active)
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.1,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		// Because the visitor day was filtered, baselineDays count is 3 < 4, so it returns "none"
		assert.Equal(t, "none", shift)
	})

	t.Run("VacationFloorFractionContained", func(t *testing.T) {
		// Tests that floorFraction * Q1 is used as the lower bound when IQR is very large
		// and active energy is below that floor fraction (e.g. 0.4 active avg, Q1 active avg is 2.0, floor is 0.5).
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// Create 6 variable baseline days: 2.1, 2.1, 2.1, 7.1, 7.1, 7.1 (standby 0.1 -> active 2.0 and 7.0)
		// Sorted active: 2.0, 2.0, 2.0, 7.0, 7.0, 7.0
		// Q1 = 2.0, Q3 = 7.0, IQR = 5.0
		// q1 - 1.2*iqr = 2.0 - 6.0 = -4.0
		// floorFraction * Q1 = 0.25 * 2.0 = 0.5
		// standbyActiveEnergyFloor = 0.02
		// Lower bound = min(2.0*0.55, max(0.02, 0.5, -4.0)) = min(1.1, 0.5) = 0.5
		baselineVals := []float64{2.1, 2.1, 2.1, 7.1, 7.1, 7.1}
		for i, val := range baselineVals {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = val
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     val,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday is low (0.5 average -> 0.4 active, which is < 0.5 bound)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today is low (0.5 average -> 0.4 active, which is < 0.5 bound)
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.5,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "down", shift)
	})

	t.Run("NoShiftUpAllowed", func(t *testing.T) {
		// Confirm that even if yesterday was high and today is high, detectLoadShift returns "none".
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is high (6.0)
		dayAveragesMap[yesterdayStr] = 6.0
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     6.0,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today is also high (6.0)
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     6.0,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("StdDevLowLoadVacationTrip", func(t *testing.T) {
		// Moderate active load (< 50% Q1) with flat StdDev (< 0.25 * Q1StdDev) triggers vacation mode
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 6 baseline days with high volatility: 1.0 to 3.0 (avg 2.0, Q1 ~2.0, Q1StdDev ~0.8)
		for i := 0; i < 6; i++ {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = 2.0
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				val := 1.0
				if h%2 == 0 {
					val = 3.0
				}
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     val,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday is moderately low (0.8 kWh/hr, which is < 50% of Q1 active) and flat (StdDev = 0.0)
		dayAveragesMap[yesterdayStr] = 0.8
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.8,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today morning is also flat (0.8 kWh/hr)
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.8,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "down", shift)
	})

	t.Run("StdDevHighUsageEVSafeguard", func(t *testing.T) {
		// Continuous high EV charging (7.2 kW all day) has zero StdDev, but high active load (> 50% Q1).
		// Must be BLOCKED from triggering vacation mode!
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 6 baseline normal days (avg 2.0)
		for i := 0; i < 6; i++ {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = 2.0
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				val := 1.0
				if h%2 == 0 {
					val = 3.0
				}
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     val,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday is continuous flat EV charging at 7.2 kW (StdDev = 0.0, but yActive = 7.1 > 50% Q1)
		dayAveragesMap[yesterdayStr] = 7.2
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     7.2,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today morning is also 7.2 kW
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     7.2,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("MidnightWrapEscape", func(t *testing.T) {
		// Test currentHour = 2 (2 AM) with loadShiftEscapeHours = 4 across midnight.
		// Hours checked are today h=1, h=0 and yesterday h=23, h=22.
		// Prevents checkHour negative index out-of-bounds or zero-value map lookup bugs.
		now := time.Date(2025, 6, 16, 2, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 6 baseline normal days
		for i := 0; i < 6; i++ {
			dateStr := time.Date(2025, 5, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			dayAveragesMap[dateStr] = 2.0
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 5, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     2.0,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday was low (0.3) for hours 0-17, but high (3.5) for hours 18-23 (returned home at 6 PM yesterday)
		dayAveragesMap[yesterdayStr] = 0.5
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			val := 0.3
			if h >= 18 {
				val = 3.5 // Returned home at 6 PM
			}
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     val,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today hours 0-1 are also high (3.5)
		var tPts []types.EnergyStats
		for h := 0; h < 2; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     3.5,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		// At currentHour = 2, lookback = 4 hours:
		// h=1 (today, 2.5), h=0 (today, 2.5), h=23 (yesterday, 2.5), h=22 (yesterday, 2.5).
		// All 4 hours are >= Q1 (2.0), so early escape triggers, returning "none".
		shift := detectLoadShift(ctx, now, time.UTC, dayMap, dayAveragesMap, todayStr, yesterdayStr, 2, 0.1)
		assert.Equal(t, "none", shift)
	})
}

func TestDualTimezoneHistoryAndSimulation(t *testing.T) {
	chicagoLoc, err := time.LoadLocation("America/Chicago")
	assert.NoError(t, err)
	laLoc, err := time.LoadLocation("America/Los_Angeles")
	assert.NoError(t, err)

	ctx := context.Background()
	c := NewController()

	// 1. History point in Chicago timezone (8 PM CDT = 01:00 UTC next day)
	chicagoTime := time.Date(2026, 8, 3, 20, 0, 0, 0, chicagoLoc)
	h1 := types.EnergyStats{
		TSHourStart:  chicagoTime.UTC(),
		TimeLocation: "America/Chicago",
		HomeKWH:      2.5,
	}

	// 2. History point in LA timezone (8 PM PDT = 03:00 UTC next day)
	laTime := time.Date(2026, 8, 3, 20, 0, 0, 0, laLoc)
	h2 := types.EnergyStats{
		TSHourStart:  laTime.UTC(),
		TimeLocation: "America/Los_Angeles",
		HomeKWH:      3.5,
	}

	// Build model using Chicago as latest site location
	nowChicago := time.Date(2026, 8, 4, 10, 0, 0, 0, chicagoLoc)
	history := []types.EnergyStats{h1, h2}
	settings := types.Settings{}

	model, _ := c.BuildHourlyEnergyModel(ctx, nowChicago, history, nil, settings)
	// Hour 20 should have aggregated both 8 PM local points
	assert.Contains(t, model, 20)
}

func TestIdentifyHistoricalVacationDays(t *testing.T) {
	ctx := context.Background()
	todayStr := "2025-06-16"
	yesterdayStr := "2025-06-15"
	standbyLoad := 0.1

	t.Run("MagnitudeDropVacation", func(t *testing.T) {
		dayAveragesMap := map[string]float64{
			"2025-06-01": 2.0,
			"2025-06-02": 2.2,
			"2025-06-03": 2.1,
			"2025-06-04": 2.3,
			"2025-06-05": 0.2, // Active avg = 0.1 (well below 25% of Q3 active 2.0 = 0.5)
			"2025-06-15": 2.0,
			"2025-06-16": 2.0,
		}
		var dailyAverages []float64
		for _, avg := range dayAveragesMap {
			dailyAverages = append(dailyAverages, avg)
		}
		dayMap := make(map[string]*dayPoints)

		vacDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)
		assert.True(t, vacDays["2025-06-05"])
		assert.False(t, vacDays["2025-06-01"])
		assert.False(t, vacDays["2025-06-02"])
	})

	t.Run("GatedVolatilityVacation", func(t *testing.T) {
		dayAveragesMap := map[string]float64{
			"2025-06-01": 2.0,
			"2025-06-02": 2.2,
			"2025-06-03": 2.1,
			"2025-06-04": 2.3,
			"2025-06-05": 0.8, // Active avg 0.7 < 55% of Q3 (1.1). Flat stddev = 0.02 (< 0.25 * q1StdDev)
			"2025-06-15": 2.0,
			"2025-06-16": 2.0,
		}
		var dailyAverages []float64
		for _, avg := range dayAveragesMap {
			dailyAverages = append(dailyAverages, avg)
		}
		dayMap := make(map[string]*dayPoints)

		// Create 24 hours of loads for normal days with volatility, and flat loads for 2025-06-05
		for dStr, avg := range dayAveragesMap {
			var loads []float64
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				l := avg
				if dStr != "2025-06-05" && h%4 == 0 {
					l += 2.0 // Add normal daily volatility
				}
				loads = append(loads, l)
				pts = append(pts, types.EnergyStats{HomeKWH: l})
			}
			dayMap[dStr] = &dayPoints{date: dStr, loads: loads, points: pts}
		}

		vacDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)
		assert.True(t, vacDays["2025-06-05"])
		assert.False(t, vacDays["2025-06-01"])
	})

	t.Run("NormalOccupancyNoVacation", func(t *testing.T) {
		dayAveragesMap := map[string]float64{
			"2025-06-01": 2.0,
			"2025-06-02": 2.2,
			"2025-06-03": 2.1,
			"2025-06-04": 2.3,
			"2025-06-05": 1.9,
			"2025-06-15": 2.0,
			"2025-06-16": 2.0,
		}
		var dailyAverages []float64
		for _, avg := range dayAveragesMap {
			dailyAverages = append(dailyAverages, avg)
		}
		dayMap := make(map[string]*dayPoints)

		vacDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)
		assert.Empty(t, vacDays)
	})

	t.Run("PostVacationRecoveryOptionGNoFalseFloor", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		var history []types.EnergyStats

		// Generate 30 days of history:
		// Days 7-11 ago were a 5-day vacation (0.1 kWh flat).
		// All other days (including yesterday & today) are normal occupancy (2.5 kWh baseline).
		startDate := now.Add(-30 * 24 * time.Hour)
		for d := 0; d < 30; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			daysAgo := int(now.Sub(dayTime).Hours() / 24)

			load := 2.5
			if daysAgo >= 7 && daysAgo <= 11 {
				load = 0.1 // 5-day vacation
			}

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			HomeLoadPredictionStrategy: "default",
		}

		c := NewController()
		model, params := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.Equal(t, "none", params.DetectedShift)
		// Option G must NOT drag predictions down to the 0.09 kWh floor.
		// Forecast for all hours must remain near normal baseline (>= 2.0 kWh).
		if assert.Contains(t, model, 17) {
			assert.Greater(t, model[17].AvgHomeLoadKWH, 2.0)
		}
		if assert.Contains(t, model, 2) {
			assert.Greater(t, model[2].AvgHomeLoadKWH, 2.0)
		}
	})

	t.Run("HistoricalVacationDetectionThresholds", func(t *testing.T) {
		// Verify that moderately-low usage days with high volatility (spikes/HVAC/EV)
		// are NOT incorrectly flagged as historical vacation days, while flat low-draw days ARE.
		now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
		var history []types.EnergyStats

		// Baseline normal days (Q3 active ~ 2.5 kWh)
		for d := 1; d <= 20; d++ {
			dayTime := now.Add(time.Duration(-d) * 24 * time.Hour)
			load := 3.0
			if d == 5 || d == 6 {
				// Real flat vacation day (0.3 kWh flat, low stddev)
				load = 0.3
			} else if d == 10 {
				// Moderate average (1.2 kWh) with high volatility spikes (0.2 to 4.0 kWh)
				for h := 0; h < 24; h++ {
					hLoad := 0.2
					if h%4 == 0 {
						hLoad = 4.2
					}
					ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
					history = append(history, types.EnergyStats{
						TSHourStart: ts,
						HomeKWH:     hLoad,
					})
				}
				continue
			}

			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)
		var dailyAverages []float64
		for _, h := range history {
			dateStr := h.TSHourStart.Format("2006-01-02")
			d, ok := dayMap[dateStr]
			if !ok {
				d = &dayPoints{date: dateStr}
				dayMap[dateStr] = d
			}
			d.points = append(d.points, h)
			d.loads = append(d.loads, h.HomeKWH)
		}
		for _, d := range dayMap {
			var sum float64
			for _, l := range d.loads {
				sum += l
			}
			avg := sum / float64(len(d.loads))
			dayAveragesMap[d.date] = avg
			dailyAverages = append(dailyAverages, avg)
		}

		todayStr := now.Format("2006-01-02")
		yesterdayStr := now.AddDate(0, 0, -1).Format("2006-01-02")

		vacationDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, 0.1)

		day5Str := now.Add(-5 * 24 * time.Hour).Format("2006-01-02")
		day6Str := now.Add(-6 * 24 * time.Hour).Format("2006-01-02")
		day10Str := now.Add(-10 * 24 * time.Hour).Format("2006-01-02")

		// Flat vacation days should be detected
		assert.True(t, vacationDays[day5Str], "day 5 (flat 0.3 kWh) should be detected as historical vacation")
		assert.True(t, vacationDays[day6Str], "day 6 (flat 0.3 kWh) should be detected as historical vacation")

		// High-volatility day (day 10) must NOT be detected as vacation
		assert.False(t, vacationDays[day10Str], "day 10 (volatile 1.2 kWh avg with spikes) must NOT be detected as vacation")
	})
}

func TestGetWeightedPercentile(t *testing.T) {
	t.Run("Empty slice returns zero", func(t *testing.T) {
		res := getWeightedPercentile(nil, 0.5)
		assert.Equal(t, 0.0, res)
	})

	t.Run("Single point returns its value", func(t *testing.T) {
		pts := []weightedPoint{{Value: 42.0, Weight: 1.0}}
		res := getWeightedPercentile(pts, 0.5)
		assert.Equal(t, 42.0, res)
	})

	t.Run("Clamps below lowest midpoint", func(t *testing.T) {
		pts := []weightedPoint{
			{Value: 10.0, Weight: 1.0},
			{Value: 20.0, Weight: 1.0},
		}
		res := getWeightedPercentile(pts, 0.0)
		assert.Equal(t, 10.0, res)
	})

	t.Run("Clamps above highest midpoint", func(t *testing.T) {
		pts := []weightedPoint{
			{Value: 10.0, Weight: 1.0},
			{Value: 20.0, Weight: 1.0},
		}
		res := getWeightedPercentile(pts, 1.0)
		assert.Equal(t, 20.0, res)
	})

	t.Run("Interpolates linearly between midpoints", func(t *testing.T) {
		pts := []weightedPoint{
			{Value: 10.0, Weight: 1.0},
			{Value: 20.0, Weight: 1.0},
		}
		// pList[0] = 0.25, pList[1] = 0.75. At percentile = 0.5, ratio = 0.5 -> 15.0
		res := getWeightedPercentile(pts, 0.5)
		assert.InDelta(t, 15.0, res, 1e-6)
	})

	t.Run("Prevents division by zero and NaN when midpoints are identical", func(t *testing.T) {
		pts := []weightedPoint{
			{Value: 10.0, Weight: 1.0},
			{Value: 20.0, Weight: 0.0},
			{Value: 30.0, Weight: 1.0},
		}
		// Midpoints between pt[0] and pt[1] might be close or identical when weight is 0
		res := getWeightedPercentile(pts, 0.5)
		assert.False(t, math.IsNaN(res), "result should not be NaN")
		assert.False(t, math.IsInf(res, 0), "result should not be infinite")
	})
}

func TestDetectDayOfWeekOutliers(t *testing.T) {
	ctx := context.Background()
	utc := time.UTC

	makeDay := func(dateStr string, mornLoad float64, restLoad float64, loc *time.Location) *dayPoints {
		dTime, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			return nil
		}
		dp := &dayPoints{date: dateStr}
		for h := 0; h < 24; h++ {
			load := restLoad
			if h >= 6 && h <= 10 {
				load = mornLoad
			}
			ts := time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, loc)
			dp.loads = append(dp.loads, load)
			dp.points = append(dp.points, types.EnergyStats{
				TSHourStart: ts,
				HomeKWH:     load,
			})
		}
		return dp
	}

	t.Run("EmptyAndNilInputs", func(t *testing.T) {
		res1 := detectDayOfWeekOutliers(ctx, utc, nil, nil, "2025-06-01")
		assert.Empty(t, res1)

		res2 := detectDayOfWeekOutliers(ctx, utc, make(map[string]*dayPoints), make(map[string]bool), "2025-06-01")
		assert.Empty(t, res2)
	})

	t.Run("ExcludeTodayDate", func(t *testing.T) {
		// History contains 3 Saturdays (2025-06-07, 2025-06-14, 2025-06-21).
		// Today is 2025-06-21 (Saturday) with high morning usage.
		// Since today must be excluded, only 2 Saturdays remain in history (len < 3).
		// Even if Saturday load is high, it must not be flagged due to insufficient historical samples.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21"}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 3.5, 1.0, utc)
			validDaysMap[s] = true
		}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-21")
		assert.False(t, res[time.Saturday], "Excluding today leaves only 2 Saturdays; should not be flagged")
	})

	t.Run("ExcludeInvalidOrVacationDays", func(t *testing.T) {
		// 4 Saturdays in dayMap, but 2 are flagged false in validDaysMap (e.g. vacation or corrupt).
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for i, s := range saturdays {
			dayMap[s] = makeDay(s, 3.5, 1.0, utc)
			// Mark the last 2 Saturdays as invalid
			validDaysMap[s] = (i < 2)
		}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.False(t, res[time.Saturday], "Invalid days must be excluded, leaving < 3 target samples")
	})

	t.Run("InsufficientTargetSamples", func(t *testing.T) {
		// Only 2 valid Saturdays exist in history (minimum required is 3).
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14"}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 10.0, 1.0, utc)
			validDaysMap[s] = true
		}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.False(t, res[time.Saturday], "Must require at least 3 target weekday occurrences")
	})

	t.Run("InsufficientPrecedingSamples", func(t *testing.T) {
		// 4 valid Saturdays exist, but only 2 Thursdays and 2 Fridays (total 4 < 6 required preceding samples).
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 3.5, 1.0, utc)
			validDaysMap[s] = true
		}
		thursdays := []string{"2025-06-05", "2025-06-12"}
		fridays := []string{"2025-06-06", "2025-06-13"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.False(t, res[time.Saturday], "Must require at least 6 preceding weekday samples (p1 + p2)")
	})

	t.Run("InsufficientMorningHoursInDay", func(t *testing.T) {
		// Saturdays have high load but only 2 non-zero points in hours 6..10 (mornCount < 3).
		// A day requires >= 3 points in hours 6..10 to qualify as hasMorn.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dTime, err := time.ParseInLocation("2006-01-02", s, utc)
			require.NoError(t, err)
			dp := &dayPoints{date: s}
			for h := 0; h < 24; h++ {
				load := 1.0
				if h == 6 || h == 7 {
					load = 5.0
				} else if h >= 8 && h <= 10 {
					load = 0.0 // missing or zero
				}
				ts := time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, utc)
				dp.loads = append(dp.loads, load)
				dp.points = append(dp.points, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
			dayMap[s] = dp
			validDaysMap[s] = true
		}

		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.False(t, res[time.Saturday], "Days with fewer than 3 morning points must not have hasMorn=true")
	})

	t.Run("ZeroAndNegativeEnergyIgnored", func(t *testing.T) {
		// Morning hours have non-positive values (0.0 or negative from net metering or sensor disconnect).
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dTime, err := time.ParseInLocation("2006-01-02", s, utc)
			require.NoError(t, err)
			dp := &dayPoints{date: s}
			for h := 0; h < 24; h++ {
				load := 1.0
				if h >= 6 && h <= 10 {
					load = -0.5 // negative or zero ignored
				}
				ts := time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, utc)
				dp.loads = append(dp.loads, load)
				dp.points = append(dp.points, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
			dayMap[s] = dp
			validDaysMap[s] = true
		}

		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.False(t, res[time.Saturday], "Zero and negative HomeKWH points must be ignored")
	})

	t.Run("SaturdayMorningOutlierDetected", func(t *testing.T) {
		// Standard case: 4 Saturdays with morning load ~2.5 kWh, Thursdays and Fridays ~1.0 kWh.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 2.5, 1.0, utc)
			validDaysMap[s] = true
		}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.True(t, res[time.Saturday], "Saturday morning surge must be detected as an outlier")
		assert.False(t, res[time.Friday], "Friday should not be detected as an outlier")
		assert.False(t, res[time.Thursday], "Thursday should not be detected as an outlier")
	})

	t.Run("SundayMorningOutlierViaFallback", func(t *testing.T) {
		// Both Saturday and Sunday are high morning chore days (2.2 kWh and 1.8 kWh).
		// Sunday vs preceding (Saturday + Friday): meanPrev = (2.2 + 1.0)/2 = 1.6 kWh.
		// Sunday primary ratio: 1.8 / 1.6 = 1.125 < 1.25 (primary check fails).
		// Sunday fallback vs Thursday + Friday (both 1.0 kWh):
		// Fallback ratio: 1.8 / 1.0 = 1.8 >= 1.25, and Welch's t >> 1.70.
		// Both Saturday and Sunday should be detected as outliers.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		sundays := []string{"2025-06-01", "2025-06-08", "2025-06-15", "2025-06-22"}
		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}

		for _, s := range sundays {
			dayMap[s] = makeDay(s, 1.8, 1.0, utc)
			validDaysMap[s] = true
		}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 2.2, 1.0, utc)
			validDaysMap[s] = true
		}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.0, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.True(t, res[time.Saturday], "Saturday morning must be detected via primary check")
		assert.True(t, res[time.Sunday], "Sunday morning must be detected via fallback to Thursday/Friday")
	})

	t.Run("WeekdayOutlierDetected", func(t *testing.T) {
		// Generic day-of-week test: Wednesday morning is consistently high (3.0 kWh)
		// while Monday and Tuesday are baseline (1.0 kWh).
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		mondays := []string{"2025-06-02", "2025-06-09", "2025-06-16", "2025-06-23"}
		tuesdays := []string{"2025-06-03", "2025-06-10", "2025-06-17", "2025-06-24"}
		wednesdays := []string{"2025-06-04", "2025-06-11", "2025-06-18", "2025-06-25"}

		for _, m := range mondays {
			dayMap[m] = makeDay(m, 1.0, 1.0, utc)
			validDaysMap[m] = true
		}
		for _, tues := range tuesdays {
			dayMap[tues] = makeDay(tues, 1.0, 1.0, utc)
			validDaysMap[tues] = true
		}
		for _, w := range wednesdays {
			dayMap[w] = makeDay(w, 3.0, 1.0, utc)
			validDaysMap[w] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.True(t, res[time.Wednesday], "Wednesday should be detected as an outlier")
		assert.False(t, res[time.Monday], "Monday should not be detected")
		assert.False(t, res[time.Tuesday], "Tuesday should not be detected")
	})

	t.Run("UniformLoadNoOutliers", func(t *testing.T) {
		// All 7 days across 4 weeks have identical morning load (1.2 kWh).
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		startDate := time.Date(2025, 6, 1, 0, 0, 0, 0, utc)
		for d := 0; d < 28; d++ {
			dStr := startDate.AddDate(0, 0, d).Format("2006-01-02")
			dayMap[dStr] = makeDay(dStr, 1.2, 1.2, utc)
			validDaysMap[dStr] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.Empty(t, res, "Uniform consumption across days should yield no outliers")
	})

	t.Run("HighRatioRejectedByWelchTDueToSingleSpike", func(t *testing.T) {
		// Ratio threshold (>= 1.25) is met due to a single 5.0 kWh appliance run,
		// but Welch's t-test rejects it due to high sample variance.
		// 4 Saturdays: [5.0, 1.0, 1.0, 1.0] -> mean = 2.0.
		// Preceding (4 Thursdays, 4 Fridays): all 1.3 kWh -> mean = 1.3.
		// Ratio = 2.0 / 1.3 = 1.538 >= 1.25.
		// Saturday std dev = 2.0 -> SE = sqrt(4.0/4 + 0) = 1.0.
		// t = (2.0 - 1.3)/1.0 = 0.70 < 1.70.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdayLoads := []float64{5.0, 1.0, 1.0, 1.0}
		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for i, s := range saturdays {
			dayMap[s] = makeDay(s, saturdayLoads[i], 1.0, utc)
			validDaysMap[s] = true
		}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 1.3, 1.0, utc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
		assert.False(t, res[time.Saturday], "Single-spike day must be rejected by Welch's t-test (t ~ 0.70 < 1.70)")
	})

	t.Run("TimezoneHandling", func(t *testing.T) {
		// Ensure timestamps with timezone offset (e.g. America/Chicago, UTC-5 during CDT)
		// correctly map hours 6..10 in local time rather than UTC time.
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 2.8, 0.8, chicagoLoc)
			validDaysMap[s] = true
		}
		thursdays := []string{"2025-06-05", "2025-06-12", "2025-06-19", "2025-06-26"}
		fridays := []string{"2025-06-06", "2025-06-13", "2025-06-20", "2025-06-27"}
		for _, d := range append(thursdays, fridays...) {
			dayMap[d] = makeDay(d, 0.8, 0.8, chicagoLoc)
			validDaysMap[d] = true
		}

		res := detectDayOfWeekOutliers(ctx, chicagoLoc, dayMap, validDaysMap, "2025-06-30")
		assert.True(t, res[time.Saturday], "Saturday morning outlier should be detected using Chicago local hours")
	})

	t.Run("MalformedDatesAndNilEntries", func(t *testing.T) {
		// ValidDaysMap contains malformed date strings and nil entries in dayMap.
		// Function should safely skip them without panicking.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)

		validDaysMap["not-a-date"] = true
		validDaysMap["2025-99-99"] = true
		validDaysMap["2025-06-07"] = true
		dayMap["2025-06-07"] = nil // nil entry in map

		// Add some valid days
		saturdays := []string{"2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dayMap[s] = makeDay(s, 2.5, 1.0, utc)
			validDaysMap[s] = true
		}

		assert.NotPanics(t, func() {
			res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30")
			assert.NotNil(t, res)
		})
	})

	t.Run("WelchTStatisticDirect", func(t *testing.T) {
		// Test calculateWelchT edge cases directly
		assert.Equal(t, 0.0, calculateWelchT(nil, []float64{1.0, 2.0}))
		assert.Equal(t, 0.0, calculateWelchT([]float64{1.0}, []float64{1.0, 2.0}))
		assert.Equal(t, 0.0, calculateWelchT([]float64{1.0, 2.0}, []float64{1.0}))

		// Identical sample distributions -> t = 0.0
		tIdentical := calculateWelchT([]float64{1.0, 2.0, 3.0}, []float64{1.0, 2.0, 3.0})
		assert.InDelta(t, 0.0, tIdentical, 1e-6)

		// A significantly higher than B with small variance -> high positive t
		tHigh := calculateWelchT([]float64{5.0, 5.1, 4.9, 5.0}, []float64{1.0, 1.1, 0.9, 1.0})
		assert.Greater(t, tHigh, 10.0)

		// A lower than B -> negative t
		tLow := calculateWelchT([]float64{1.0, 1.1, 0.9}, []float64{5.0, 5.1, 4.9})
		assert.Less(t, tLow, -10.0)
	})
}
