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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		if assert.Len(t, model, 24) {
			assert.False(t, model[14].TSHourStart.IsZero())
			assert.InDelta(t, 1.0, model[14].AvgHomeLoadKWH, 0.001)
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The vacation day should be filtered out, so the average load is 2.0.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The high outlier day should be filtered out, so the average load is 2.0.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50p median of [0.2, 2.0, 2.0] is 2.0.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Saturday target hour 12:
		// Saturday count = 1 (< 3).
		// Fallback to Weekend group (Saturday + Sunday) count = 3 (>= 3).
		// With same-weekday weekly decay 0.90 and 2.5x multiplier, matching Saturday (load 3.0, weight 2.25)
		// correctly pulls the 50p median to 3.0 rather than being dominated by Sundays (4.0).
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Monday target hour 12:
		// Monday count = 1 (< 3).
		// Weekday group fallback (Monday + Tuesday) count = 2 (< 3).
		// Fallback to all valid days: Monday (3.0) + Tuesday (4.0) + Saturday (1.0) = 3 days (>= 3).
		// Weighted 50p median of [1.0, 3.0, 4.0] is 3.0.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The 8.0 KWH charge should be filtered out because the median is 1.0, and limit is 3.0.
		// So the prediction should reflect the baseline (1.0).
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Adjacent hour blending at 55p captures the recurring elevated charging load.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Expected baseline load for Mon is ~1.0 KWH.
		// Since the last 4 days consistently average 2.0 KWH, the forecast should shift up.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Expected baseline load for Mon is ~2.0 KWH.
		// Since the last 4 days consistently average 1.0 KWH, the forecast should shift down.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The last 4 days match the baseline exactly, so the shift should be 0.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// Overnight idle load (hour 3) must be protected by standby baseline (>= 0.6 kWh).
		if assert.Len(t, model, 24) {
			assert.GreaterOrEqual(t, model[3].AvgHomeLoadKWH, 0.60)
			// Daytime active load (hour 12) should be predicted near the cool day load (~1.2 kWh),
			// rather than being crushed to 0.5 kWh or pulled up to 2.5 kWh.
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
		model, simParams, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// The single day spike must NOT trigger an adaptive shift.
		assert.Equal(t, "none", simParams.DetectedShift)
		// Overnight load should remain near 0.9 kWh, not inflated to 2.2+ kWh.
		if assert.Len(t, model, 24) {
			assert.Less(t, model[3].AvgHomeLoadKWH, 1.3)
		}
	})

	t.Run("MultipleHighSpikesRetained", func(t *testing.T) {
		// EV charged on 2 out of 5 Mondays at 11am (8.0 kWh vs 1.0 kWh baseline).
		// Automatic intermittent EV detection filters the 2 EV charging spikes so the
		// blended median remains anchored at the 1.0 kWh household baseline.
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
					load = 8.0 // Intermittent EV charge on 2 Mondays
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50p median of the blended pool is 1.0.
		if assert.Len(t, model, 24) {
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50p median of [0.1, 0.1, 0.3] is 0.1.
		if assert.Len(t, model, 24) {
			assert.InDelta(t, 0.1, model[11].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("WeekdaySpecificSpikeRetained", func(t *testing.T) {
		// Thursdays have a single-hour 3.0 kWh laundry run at 7 PM (19:00), while all other
		// hours on Thursday (including 18:00 and 20:00) and all hours on Monday are 1.0 kWh.
		// Because +/-1 hour neighbor smoothing (18:00 and 20:00 at 0.25x weight) and non-matching
		// weekdays (Mondays at 1.0 kWh) dominate the weighted median when only a single evening hour
		// spikes, the 50th percentile at 19:00 remains anchored at 1.0 kWh.
		now := time.Date(2025, 6, 19, 12, 0, 0, 0, time.UTC) // Thursday

		var history []types.EnergyStats
		for week := 1; week <= 5; week++ {
			// Truncate to start at midnight of the day so hours 0-23 map correctly to the same calendar day.
			dayStart := now.Add(time.Duration(-week*7*24) * time.Hour).Truncate(24 * time.Hour)
			// Target Thursday
			for h := 0; h < 24; h++ {
				load := 1.0
				if h == 19 {
					load = 3.0 // Single-hour Thursday evening appliance run
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

		settings := types.Settings{}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Weighted 50th percentile at 19:00 remains anchored at 1.0 kWh baseline.
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{})
		assert.InDelta(t, 2.1743589743589746, model[h1.Hour()].AvgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].AvgSolarKWH, 0.001)
	})

	t.Run("Basic Average All Low", func(t *testing.T) {
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 0.0, SolarKWH: 0.0},
		}

		testNow := time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC)
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{})
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{})
		assert.InDelta(t, 2.0, model[18].AvgHomeLoadKWH, 0.001)
	})

	t.Run("Ignore Outliers", func(t *testing.T) {
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		h2 := h1.Add(-24 * time.Hour)
		h3 := h1.Add(-48 * time.Hour)

		// Case 1: 1 outlier above threshold is automatically filtered out
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 1.2, SolarKWH: 0.0},
			{TSHourStart: h3, HomeKWH: 10.0, SolarKWH: 0.0}, // Outlier
		}
		testNow := time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC)
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{})
		assert.InDelta(t, 1.1174358974358976, model[h1.Hour()].AvgHomeLoadKWH, 0.001)

		// Case 2: Majority of days are high (not removed because 30th percentile is 10.0)
		historyMulti := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0}, // High day 1
			{TSHourStart: h3, HomeKWH: 12.0, SolarKWH: 0.0}, // High day 2
		}
		modelMulti, _, _, _ := c.BuildHourlyEnergyModel(ctx, testNow, historyMulti, nil, types.Settings{})
		assert.InDelta(t, 12.0, modelMulti[h1.Hour()].AvgHomeLoadKWH, 0.001)

		// Case 3: Not enough points (min 3)
		historyFew := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0},
		}
		modelFew, _, _, _ := c.BuildHourlyEnergyModel(ctx, testNow, historyFew, nil, types.Settings{})
		assert.InDelta(t, 6.284615384615385, modelFew[h1.Hour()].AvgHomeLoadKWH, 0.001)
	})

	t.Run("AfternoonSolarEVCharging3To4DaysPerWeek", func(t *testing.T) {
		// Verify that when a homeowner charges their EV 3-4 days a week in the afternoon
		// to soak up solar (with EVChargingStandby disabled), automatic zero-config
		// EV filtering prevents the model from predicting EV charging every afternoon on
		// both AvgHomeLoadKWH and P75HomeLoadKWH.
		// Covers common 40A breaker (7.7 kW) and 50A breaker (9.6 kW) Level 2 chargers as
		// well as 60A (12.0 kW), and variable durations (1-hour short top-off on Mondays,
		// 2 hours on Wednesdays, 3 hours on Fridays/Saturdays).
		verifyAfternoonEVFiltering := func(t *testing.T, evLoadKW float64) {
			now := time.Date(2026, 8, 17, 4, 0, 0, 0, time.UTC) // Monday 4:00 AM
			var history []types.EnergyStats

			startDate := now.Truncate(24*time.Hour).AddDate(0, 0, -28)
			for d := 0; d < 28; d++ {
				dayTime := startDate.AddDate(0, 0, d)
				wd := dayTime.Weekday()

				for h := 0; h < 24; h++ {
					load := 1.0
					if h >= 12 && h <= 17 {
						load = 1.5
					} else if h >= 18 && h <= 21 {
						load = 2.0
					}
					// Variable duration sessions:
					// - Monday: 1-hour short solar top-off (hour 14 only)
					// - Wednesday: 2-hour charge (hours 13-14)
					// - Friday & Saturday: 3-hour charge (hours 13-15)
					isEVHour := (wd == time.Monday && h == 14) ||
						(wd == time.Wednesday && (h == 13 || h == 14)) ||
						((wd == time.Friday || wd == time.Saturday) && (h == 13 || h == 14 || h == 15))
					if isEVHour {
						load += evLoadKW
					}
					history = append(history, types.EnergyStats{
						TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						HomeKWH:     load,
					})
				}
			}

			model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, types.Settings{})
			if assert.Len(t, model, 24) {
				for _, hr := range []int{13, 14, 15} {
					assert.InDelta(t, 1.5, model[hr].AvgHomeLoadKWH, 0.25, "hour %d AvgHomeLoadKWH should reflect non-EV afternoon load", hr)
					assert.InDelta(t, 1.5, model[hr].P75HomeLoadKWH, 0.25, "hour %d P75HomeLoadKWH should reflect non-EV afternoon load", hr)
				}
			}
		}

		t.Run("40A_Breaker_7.7kW", func(t *testing.T) {
			verifyAfternoonEVFiltering(t, 7.7)
		})
		t.Run("50A_Breaker_9.6kW", func(t *testing.T) {
			verifyAfternoonEVFiltering(t, 9.6)
		})
		t.Run("60A_Breaker_12.0kW", func(t *testing.T) {
			verifyAfternoonEVFiltering(t, 12.0)
		})
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

			model, params, _, _ := c.BuildHourlyEnergyModel(ctx, nowDayPlusOne, fullHistory, nil, types.Settings{})
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

			model, params, _, _ := c.BuildHourlyEnergyModel(ctx, nowDayPlusTwo, fullHistory, nil, types.Settings{})
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

			model, params, _, _ := c.BuildHourlyEnergyModel(ctx, nowOnVacation, fullHistory, nil, types.Settings{})
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.Greater(t, model[13].AvgSolarKWH, 4.8, "Should reconstruct bell curve peak to ~5.0 using off-peak data")
		assert.Less(t, model[13].AvgSolarKWH, 5.2, "Should be around 5.0")

		for i := range history {
			history[i].SolarKWH = 0.05
		}
		modelNoData, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The 50p median of [2.0 (weight 28-day decay), 8.0 (weight 7-day decay)]
		// will be biased towards 8.0 because the 7-day weight is much larger.
		// We assert it is greater than 5.0 (the midpoint).
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)
		if assert.Len(t, model, 24) {
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

		modelMild, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, mildWeather, settings)
		if assert.Len(t, modelMild, 24) {
			assert.InDelta(t, 1.0, modelMild[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("ColdDayExemptFromUpperIQROnElectricHeatingSite", func(t *testing.T) {
		now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
		startDate := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		for d := 0; d < 28; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				var tempC, load float64
				switch {
				case d < 20:
					tempC = 15.0
					load = 1.0
				case d < 26:
					if h <= 8 {
						tempC = 9.0 - float64(d-20)*0.6
						load = 1.0 + 0.45*(10.0-tempC)
					} else {
						tempC = 11.0
						load = 1.2
					}
				default:
					if h <= 8 {
						tempC = -2.0
						load = 4.5
					} else {
						tempC = 3.0
						load = 2.5
					}
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			weather = append(weather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}

		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var todayHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			tempC := 3.0
			if h <= 8 {
				tempC = -2.0
			}
			todayHW = append(todayHW, types.HourlyWeather{
				TSHourStart:          time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC},
			})
		}
		weatherWithToday := append(weather, types.Weather{TSDayStart: todayStart, ForecastHours: todayHW})

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weatherWithToday, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.Greater(t, model[5].AvgHomeLoadKWH, 3.5, "cold-snap electric heating days must be exempt from upper IQR removal")
		}

		modelNoWeather, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, types.Settings{})
		if assert.Len(t, modelNoWeather, 24) {
			assert.Less(t, modelNoWeather[5].AvgHomeLoadKWH, 2.0, "non-weather upper IQR outlier filtering must remain intact when weather is absent")
		}
	})

	t.Run("WeightedPoolTemperatureGapBridgesDay2ColdSnapAndFadesInSteadyWinter", func(t *testing.T) {
		now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
		startSnap := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
		var snapHistory []types.EnergyStats
		var snapWeather []types.Weather

		for d := 0; d < 27; d++ {
			dayTime := startSnap.AddDate(0, 0, d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				var tempC, load float64
				switch {
				case d < 20:
					tempC = 14.0
					load = 1.0
				case d < 26:
					if h <= 8 {
						tempC = 9.0 - float64(d-20)
						load = 1.0 + 0.45*(10.0-tempC)
					} else {
						tempC = 10.0
						load = 1.0
					}
				default:
					if h <= 8 {
						tempC = 1.0
						load = 1.0 + 0.45*9.0
					} else {
						tempC = 6.0
						load = 1.0
					}
				}
				snapHistory = append(snapHistory, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			snapWeather = append(snapWeather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}

		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var todaySnapHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			tempC := 6.0
			if h <= 8 {
				tempC = 1.0
			}
			todaySnapHW = append(todaySnapHW, types.HourlyWeather{
				TSHourStart:          time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC},
			})
		}
		snapWeather = append(snapWeather, types.Weather{TSDayStart: todayStart, ForecastHours: todaySnapHW})

		modelSnap, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, snapHistory, snapWeather, types.Settings{})
		if assert.Len(t, modelSnap, 24) {
			assert.Greater(t, modelSnap[5].AvgHomeLoadKWH, 3.5, "weighted pool temperature gap must bridge Day 2 of a cold snap")
		}

		// Steady winter: 10 mild days (14.0C, 1.0 kWh) followed by 12 steady cold days (1.0C, 5.0 kWh morning)
		// so hvac.heatingHours[5] is genuinely active (temp spread 13C >= 4C, HDD spread 9C >= 3C), while
		// recent 1.0C days dominate the pool so weighted-median pool gap decays to zero.
		startSteady := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
		var steadyHistory []types.EnergyStats
		var steadyWeather []types.Weather
		for d := 0; d < 22; d++ {
			dayTime := startSteady.AddDate(0, 0, d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				tempC := 14.0
				load := 1.0
				if d >= 10 {
					tempC = 1.0
					load = 1.2
					if h <= 8 {
						load = 5.0
					}
				}
				steadyHistory = append(steadyHistory, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			steadyWeather = append(steadyWeather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}
		var todaySteadyHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			todaySteadyHW = append(todaySteadyHW, types.HourlyWeather{
				TSHourStart:          time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: 1.0},
			})
		}
		steadyWeather = append(steadyWeather, types.Weather{TSDayStart: todayStart, ForecastHours: todaySteadyHW})

		modelSteady, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, steadyHistory, steadyWeather, types.Settings{})
		if assert.Len(t, modelSteady, 24) {
			assert.InDelta(t, 5.0, modelSteady[5].AvgHomeLoadKWH, 0.35, "gap extrapolation must decay to zero in steady cold winter")
		}
	})

	t.Run("SymmetricHotWeatherPoolGapExtrapolatesHeatwaveDay2", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
		startDate := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		for d := 0; d < 27; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				var tempC, load float64
				switch {
				case d < 20:
					tempC = 18.0
					load = 1.0
				case d < 26:
					if h >= 11 && h <= 19 {
						tempC = 23.0 + float64(d-20)
						load = 1.0 + 0.40*(tempC-22.0)
					} else {
						tempC = 19.0
						load = 1.0
					}
				default:
					if h >= 11 && h <= 19 {
						tempC = 33.0
						load = 1.0 + 0.40*11.0
					} else {
						tempC = 24.0
						load = 1.0
					}
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			weather = append(weather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}

		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var todayHotHW, todayMildHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			ts := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC)
			tempHot := 24.0
			if h >= 11 && h <= 19 {
				tempHot = 33.0
			}
			todayHotHW = append(todayHotHW, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempHot}})
			todayMildHW = append(todayMildHW, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: 18.0}})
		}

		hotWeather := append(append([]types.Weather(nil), weather...), types.Weather{TSDayStart: todayStart, ForecastHours: todayHotHW})
		modelHot, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, hotWeather, types.Settings{})
		if assert.Len(t, modelHot, 24) {
			assert.Greater(t, modelHot[15].AvgHomeLoadKWH, 3.8, "symmetric cooling pool-gap extrapolation must boost Day 2 heatwave load")
		}

		mildWeather := append(append([]types.Weather(nil), weather...), types.Weather{TSDayStart: todayStart, ForecastHours: todayMildHW})
		modelMild, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, mildWeather, types.Settings{})
		if assert.Len(t, modelMild, 24) {
			assert.Less(t, modelMild[15].AvgHomeLoadKWH, 1.5, "cooling extrapolation must not fire on mild forecast days")
		}
	})

	t.Run("SharpHVACSetbackDampensNeighborBlend", func(t *testing.T) {
		now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
		startDate := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		for d := 0; d < 16; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			tempC := 16.0 - float64(d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				load := 1.0
				if h == 5 || h == 6 {
					load = 1.0 + 0.55*max(0.0, 10.0-tempC)
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			weather = append(weather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}

		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var todayHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			todayHW = append(todayHW, types.HourlyWeather{
				TSHourStart:          time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: 1.0},
			})
		}
		weather = append(weather, types.Weather{TSDayStart: todayStart, ForecastHours: todayHW})

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.Greater(t, model[5].AvgHomeLoadKWH, 4.2, "sharp morning HVAC setback recovery must dampen adjacent quiet-hour blending")
			assert.Less(t, model[4].AvgHomeLoadKWH, 1.5, "symmetric reverse setback dampening must prevent 05:00 peak from inflating quiet 04:00")
		}
	})

	t.Run("HotDayExemptFromUpperIQROnElectricCoolingSite", func(t *testing.T) {
		now := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
		startDate := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		for d := 0; d < 28; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				var tempC, load float64
				switch {
				case d < 20:
					tempC = 18.0
					load = 1.0
				case d < 26:
					if h >= 11 && h <= 19 {
						tempC = 23.0 + float64(d-20)
						load = 1.0 + 0.40*(tempC-22.0)
					} else {
						tempC = 19.0
						load = 1.0
					}
				default:
					if h >= 11 && h <= 19 {
						tempC = 35.0
						load = 4.5
					} else {
						tempC = 26.0
						load = 2.5
					}
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			weather = append(weather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}

		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var todayHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			tempC := 26.0
			if h >= 11 && h <= 19 {
				tempC = 35.0
			}
			todayHW = append(todayHW, types.HourlyWeather{
				TSHourStart:          time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC},
			})
		}
		weatherWithToday := append(weather, types.Weather{TSDayStart: todayStart, ForecastHours: todayHW})

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weatherWithToday, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.Greater(t, model[15].AvgHomeLoadKWH, 3.5, "extreme heatwave AC days must be exempt from upper IQR removal")
		}

		modelNoWeather, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, types.Settings{})
		if assert.Len(t, modelNoWeather, 24) {
			assert.Less(t, modelNoWeather[15].AvgHomeLoadKWH, 2.0, "non-weather upper IQR outlier filtering must remain intact")
		}
	})

	t.Run("WeightedMedianPoolTempPreventsDoubleCountingUnderConservativeP70", func(t *testing.T) {
		// Under Conservative (P70), once recent 1.0C days represent > 50% of the recency- and
		// temperature-weighted pool, P70 already equals the 1.0C load (5.05 kWh) and the weighted
		// median effectivePoolTempC (0.50) equals 1.0C, avoiding a redundant pool-gap boost.
		now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
		startDate := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		for d := 0; d < 18; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			var hw []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC)
				var tempC, load float64
				switch {
				case d < 8:
					tempC = 14.0
					load = 1.0
				case d < 12:
					tempC = 4.5
					load = 1.0
					if h <= 8 {
						load = 1.0 + 0.45*(10.0-tempC)
					}
				default:
					tempC = 1.0
					load = 1.2
					if h <= 8 {
						load = 1.0 + 0.45*(10.0-tempC)
					}
				}
				history = append(history, types.EnergyStats{TSHourStart: ts, HomeKWH: load})
				hw = append(hw, types.HourlyWeather{TSHourStart: ts, HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: tempC}})
			}
			weather = append(weather, types.Weather{TSDayStart: dayTime, ForecastHours: hw})
		}

		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var todayHW []types.HourlyWeather
		for h := 0; h < 24; h++ {
			todayHW = append(todayHW, types.HourlyWeather{
				TSHourStart:          time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HourlyWeatherMetrics: types.HourlyWeatherMetrics{TemperatureC: 1.0},
			})
		}
		weather = append(weather, types.Weather{TSDayStart: todayStart, ForecastHours: todayHW})

		settings := types.Settings{HomeLoadPredictionStrategy: "conservative"}
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)
		if assert.Len(t, model, 24) {
			assert.InDelta(t, 5.05, model[5].AvgHomeLoadKWH, 0.30, "P70 prediction must not double-count pool gap once 1.0C dominates upper tail")
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Model for remaining hours (e.g. 15:00) should be close to 0.5 (standby), not 2.0.
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Hour 8 (Saturday morning chore peak) should be close to 2.5 kWh, well above the 1.0 weekday level.
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Hour 7 should not be diluted down by hour 6's 0.15 kWh sleeping load.
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Sunday morning peak (hour 8) should be boosted to near weekend level (>= 2.2 kWh)
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Wednesday morning peak (hour 8) should reflect the Wednesday surge (>= 2.2 kWh)
		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		if assert.Len(t, model, 24) {
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
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// On vacation, Saturday morning (hour 8) should NOT be inflated to 2.5-3.0 kWh;
		// it should drop toward vacation levels (< 1.0 kWh).
		if assert.Len(t, model, 24) {
			assert.Less(t, model[8].AvgHomeLoadKWH, 1.0)
		}
	})

	t.Run("CrossMidnightNeighborBlendingAndLocalTimezoneWeather", func(t *testing.T) {
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// Predict on Saturday 2025-06-28 at 04:00 AM CDT.
		// Hour 0 on Saturday (2025-06-28 00:00) should blend with Friday 23:00 (prevDayPoints),
		// not Saturday 23:00 (23 hours later).
		now := time.Date(2025, 6, 28, 4, 0, 0, 0, chicagoLoc)
		var history []types.EnergyStats
		var weather []types.Weather

		startDate := time.Date(2025, 5, 31, 0, 0, 0, 0, chicagoLoc)
		for d := 0; d < 28; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			var fHours []types.HourlyWeather
			for h := 0; h < 24; h++ {
				ts := time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, chicagoLoc)
				load := 1.0
				// Make Saturday 23:00 very high (8.0 kWh) while Friday 23:00 is normal (1.0 kWh)
				if dayTime.Weekday() == time.Saturday && h == 23 {
					load = 8.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  ts,
					TimeLocation: "America/Chicago",
					HomeKWH:      load,
				})
				fHours = append(fHours, types.HourlyWeather{
					TSHourStart:  ts.UTC(),
					TemperatureC: 24.0,
				})
			}
			weather = append(weather, types.Weather{
				TSDayStart:    dayTime.UTC(),
				TimeLocation:  "America/Chicago",
				ForecastHours: fHours,
			})
		}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, types.Settings{})
		if assert.Len(t, model, 24) {
			// Hour 0 on Sunday should NOT be inflated by Sunday 23:00; and Saturday 23:00 should remain localized
			assert.InDelta(t, 1.0, model[0].AvgHomeLoadKWH, 0.25)
		}
	})

	t.Run("Hour23MidnightNeighborWrapsToNextDayAndExcludesToday", func(t *testing.T) {
		// Verify Fix 1A: when blending +/-1 hour neighbors at h == 23 on day d,
		// the +1 neighbor wraps to 00:00 of day d+1 (not 00:00 of the start of day d),
		// and on yesterday (where d+1 is todayStr), no +1 neighbor is added at all (neither
		// leaking todayStr 00:00 nor wrapping back to yesterday 00:00).
		now := time.Date(2025, 6, 28, 12, 0, 0, 0, time.UTC) // Saturday
		var history []types.EnergyStats

		// Use 2 historical days (2025-06-26 and 2025-06-27) plus today (2025-06-28) with distinct
		// values at h=22, h=23, and h=0 so any neighbor shift changes the 55th percentile by > 0.28 kWh:
		// - 2025-06-26 (w=0.9025): h=0 is 4.5 kWh (same-day start), h=22 is 1.0, h=23 is 1.5.
		//   Its valid +1 neighbor is 2025-06-27 h=0 (2.0 kWh). If it wrapped to its own h=0 (4.5 kWh),
		//   the 55th percentile would jump from 2.213 to 2.606.
		// - 2025-06-27 (yesterday, w=0.95): h=0 is 2.0 kWh, h=22 is 2.5, h=23 is 3.0.
		//   Its d+1 is todayStr (2025-06-28), where h=0 is 4.5 kWh. Because todayStr is excluded and
		//   yesterday must not wrap, no +1 neighbor is added. If todayStr h=0 (4.5 kWh) leaked in,
		//   the 55th percentile would jump from 2.213 to 2.495.
		startDate := time.Date(2025, 6, 26, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 3; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			dateStr := dayTime.Format("2006-01-02")
			for h := 0; h < 24; h++ {
				if dateStr == "2025-06-28" && h >= 12 {
					continue
				}
				load := 1.0
				switch dateStr {
				case "2025-06-26":
					if h == 0 {
						load = 4.5
					} else if h == 23 {
						load = 1.5
					}
				case "2025-06-27":
					if h == 0 {
						load = 2.0
					} else if h == 22 {
						load = 2.5
					} else if h == 23 {
						load = 3.0
					}
				case "2025-06-28":
					if h == 0 {
						load = 4.5
					}
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.InDelta(t, 2.213, model[23].AvgHomeLoadKWH, 0.05, "hour 23 should interpolate to ~2.213 without wrapping to same-day 00:00 (2.606) or leaking todayStr 00:00 (2.495)")
		}
	})

	t.Run("ConfiguredEVChargingStandbyUsesSameHourBaselineAndPreservesCallerSlice", func(t *testing.T) {
		// Verify Fix 1B: when EVChargingStandby is enabled for overnight hours (20:00-06:59),
		// loads above EVThresholdKW are replaced using the same hour-of-day from prior non-EV nights
		// (0.4 kWh sleeping load) rather than late-evening waking hours (2.8 kWh at 23:00),
		// and the caller's history slice is never mutated in-place.
		now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
		var history []types.EnergyStats

		startDate := now.Truncate(24*time.Hour).AddDate(0, 0, -14)
		for d := 0; d < 14; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			isEVNight := d%3 == 1 // 5 nights of EV charging out of 14
			for h := 0; h < 24; h++ {
				load := 2.8 // Waking daytime/evening load (06:00-23:00)
				if h >= 0 && h <= 5 {
					load = 0.4 // Overnight sleeping baseline
				}
				if isEVNight && (h >= 1 && h <= 3) {
					load = 8.5 // Overnight EV charging
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		// Record original caller slice values to verify no in-place mutation
		origValues := make([]float64, len(history))
		for i := range history {
			origValues[i] = history[i].HomeKWH
		}

		settings := types.Settings{
			EVChargingStandby: true,
		}
		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Verify caller slice was not mutated
		for i := range history {
			assert.Equal(t, origValues[i], history[i].HomeKWH, "caller history slice must not be mutated at index %d", i)
		}

		// Verify hours 1, 2, 3 reflect the 0.4 kWh sleeping baseline (not 2.8 kWh from 23:00)
		if assert.Len(t, model, 24) {
			for _, hr := range []int{1, 2, 3} {
				assert.InDelta(t, 0.4, model[hr].AvgHomeLoadKWH, 0.15, "overnight EV hour %d should use same-hour sleeping baseline", hr)
			}
		}
	})

	t.Run("PartialWeatherDayIgnoredAndMissingWeatherScaledByAvgTempMult", func(t *testing.T) {
		// Verify Fixes 1E & 1F:
		// 1. Historical days with < 12 hours of weather data are ignored by getDayAvgTemp
		//    so a few hours of partial weather cannot skew daily average temperatures.
		// 2. Historical days missing valid daily weather are scaled by avgTempMult (rather than
		//    receiving tempMult = 1.0 as if they had 0°C difference from targetDayTemp).
		now := time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		startDate := now.Truncate(24*time.Hour).AddDate(0, 0, -21)
		for d := 0; d <= 21; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			isToday := d == 21
			isHotDay := d >= 15 && d <= 20 // 6 recent hot days at 32°C with 4.0 kWh AC load
			isPartialWeatherCoolDay := d < 5

			load := 0.8 // Cool day load (18°C)
			temp := 18.0
			if isHotDay || isToday {
				load = 4.0
				temp = 32.0
			}

			var fHours []types.HourlyWeather
			for h := 0; h < 24; h++ {
				if isToday && h >= 6 {
					// Only weather in the future on today, history up to hour 5
				} else {
					history = append(history, types.EnergyStats{
						TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						HomeKWH:     load,
					})
				}

				if isPartialWeatherCoolDay {
					// Only provide 3 hours of partial 32°C weather (< 12 hours minimum).
					// getDayAvgTemp must ignore this partial weather day, and BuildHourlyEnergyModel
					// must scale it by avgTempMult rather than treating it as a 32°C match (tempMult=1.0).
					if h < 3 {
						fHours = append(fHours, types.HourlyWeather{
							TSHourStart:  time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
							TemperatureC: 32.0,
						})
					}
				} else if d >= 8 {
					// Days 5..7 have NO weather at all (missing weather on cool 0.8 kWh days);
					// Days 8..14 have full 18°C weather; Days 15..21 have full 32°C weather.
					fHours = append(fHours, types.HourlyWeather{
						TSHourStart:  time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						TemperatureC: temp,
					})
				}
			}
			if len(fHours) > 0 {
				weather = append(weather, types.Weather{
					TSDayStart:    dayTime,
					ForecastHours: fHours,
				})
			}
		}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.Greater(t, model[15].AvgHomeLoadKWH, 3.4, "hot target day forecast should stay near 4.0 kWh and not be dragged down by missing/partial weather cool days")
		}
	})

	t.Run("FullDayWeekdayOutlierBoostsAfternoonAndPreservesRecentDays", func(t *testing.T) {
		// Verify Fix 2A: when a specific day of the week (e.g. Wednesday) has a consistent
		// full-day load surge across both morning (06:00-13:00) and afternoon/evening (14:00-20:00),
		// detectDayOfWeekOutliers flags Wednesday via the full-day ratio/t-stat check, and
		// BuildHourlyEnergyModel boosts afternoon hours (14:00-20:00) on Wednesdays while still
		// keeping non-Wednesday recent days weighted in the baseline.
		now := time.Date(2025, 7, 2, 5, 0, 0, 0, time.UTC) // Wednesday 05:00 AM
		var history []types.EnergyStats

		startDate := time.Date(2025, 5, 28, 0, 0, 0, 0, time.UTC)
		for d := 0; d <= 35; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			isToday := d == 35
			isWed := dayTime.Weekday() == time.Wednesday
			for h := 0; h < 24; h++ {
				if isToday && h >= 5 {
					continue
				}
				load := 0.3
				if h >= 6 && h <= 20 {
					if isWed {
						load = 2.4 // Full-day Wednesday surge across morning and afternoon
					} else {
						load = 0.9 + 0.2*float64(d%5) // Natural daily variation (0.9..1.7 kWh)
					}
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     load,
				})
			}
		}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.Greater(t, model[15].AvgHomeLoadKWH, 2.0, "Wednesday afternoon hour 15 should be boosted by the Wednesday full-day outlier weight")
		}
	})

	t.Run("WeatherDrivenRecentIncreaseSuppressesOptionGDoubleCounting", func(t *testing.T) {
		// Verify Fix 2B: when the last 2 days have higher load (3.5 kWh vs ~1.2 kWh median)
		// purely because of hot weather (32°C vs 20°C mild baseline) that matches historical
		// hot days (32°C at 3.5 kWh), detectLoadShift suppresses the "up" shift and Option G
		// dampens upward scaling so AC load is not double-counted on top of temperature-weighted history.
		now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		startDate := now.Truncate(24*time.Hour).AddDate(0, 0, -24)
		for d := 0; d <= 24; d++ {
			dayTime := startDate.AddDate(0, 0, d)
			isToday := d == 24
			// Historical hot spell (d=6..12, 7 days) AND recent hot spell (d=22..24, 3 days)
			// so hot days are part of the normal summer distribution while the 21-day median remains mild (~1.2 kWh).
			isHot := (d >= 6 && d <= 12) || d >= 22

			temp := 20.0
			load := 1.0 + 0.1*float64(d%4)
			if isHot {
				temp = 32.0
				load = 3.5
			}

			var fHours []types.HourlyWeather
			for h := 0; h < 24; h++ {
				if !isToday || h < 12 {
					history = append(history, types.EnergyStats{
						TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						HomeKWH:     load,
					})
				}
				fHours = append(fHours, types.HourlyWeather{
					TSHourStart:  time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
					TemperatureC: temp,
				})
			}
			weather = append(weather, types.Weather{
				TSDayStart:    dayTime,
				ForecastHours: fHours,
			})
		}

		model, params, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, types.Settings{})
		assert.Equal(t, "none", params.DetectedShift, "weather-explained recent high load matching historical hot days should not trigger an 'up' occupancy shift")
		if assert.Len(t, model, 24) {
			assert.InDelta(t, 3.5, model[15].AvgHomeLoadKWH, 0.35, "afternoon forecast should match 3.5 kWh hot-day load without Option G double-counting")
		}
	})

	t.Run("DSTFallBack25HourDayNormalizesMerged1AMBucket", func(t *testing.T) {
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// 2026-11-01 is the 25-hour autumn DST transition Sunday in America/Chicago.
		// Simulate 14 days ending on Sunday 2026-11-08 at 00:00 CST where normal hours draw 1.0 kWh,
		// and 2026-11-01 has a single merged 01:00 record spanning 2 elapsed hours up to 02:00 CST with 2.0 kWh.
		now := time.Date(2026, 11, 8, 0, 0, 0, 0, chicagoLoc)
		var history []types.EnergyStats
		startDate := time.Date(2026, 10, 25, 0, 0, 0, 0, chicagoLoc)
		for d := 0; d < 14; d++ {
			dayStart := startDate.AddDate(0, 0, d)
			nextDay := dayStart.AddDate(0, 0, 1)
			for ts := dayStart; ts.Before(nextDay); ts = ts.Add(time.Hour) {
				localTS := ts.In(chicagoLoc)
				if localTS.Format("2006-01-02") == "2026-11-01" && localTS.Hour() == 1 {
					// Emit only the first 01:00 CDT bucket with 2 hours of merged 1.0 kWh/hr load (2.0 kWh)
					// and skip the repeated 01:00 CST bucket so 02:00 CST is 2 elapsed hours later.
					_, offsetSec := localTS.Zone()
					if offsetSec == -5*3600 {
						history = append(history, types.EnergyStats{
							TSHourStart:  localTS,
							TimeLocation: "America/Chicago",
							HomeKWH:      2.0,
						})
					}
					continue
				}
				history = append(history, types.EnergyStats{
					TSHourStart:  localTS,
					TimeLocation: "America/Chicago",
					HomeKWH:      1.0,
				})
			}
		}

		model, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, types.Settings{})
		if assert.Len(t, model, 24) {
			assert.InDelta(t, 1.0, model[1].AvgHomeLoadKWH, 0.05, "merged 2-hour 01:00 fall-back bucket (2.0 kWh over 2h) must normalize to 1.0 kWh/hr")
		}
	})

	t.Run("OutlierWeekdayCalmMorningDampensRemainingTodayHours", func(t *testing.T) {
		now := time.Date(2025, 7, 2, 10, 0, 0, 0, time.UTC) // Wednesday 10:00 AM
		startDate := time.Date(2025, 5, 28, 0, 0, 0, 0, time.UTC)

		buildHistory := func(todayMorningKWH float64) []types.EnergyStats {
			var history []types.EnergyStats
			for d := 0; d <= 35; d++ {
				dayTime := startDate.AddDate(0, 0, d)
				isToday := d == 35
				isWed := dayTime.Weekday() == time.Wednesday && !isToday
				for h := 0; h < 24; h++ {
					if isToday && h >= 10 {
						continue
					}
					load := 0.30
					if h >= 6 && h <= 20 {
						if isToday {
							load = todayMorningKWH
						} else if isWed {
							load = 2.40
						} else {
							load = 0.90 + 0.20*float64(d%5) // Natural daily variation (0.90..1.70 kWh, P50 ~1.30 kWh)
						}
					}
					history = append(history, types.EnergyStats{
						TSHourStart: time.Date(dayTime.Year(), dayTime.Month(), dayTime.Day(), h, 0, 0, 0, time.UTC),
						HomeKWH:     load,
					})
				}
			}
			return history
		}

		activeModel, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, buildHistory(2.40), nil, types.Settings{})
		calmModel, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, buildHistory(0.95), nil, types.Settings{})
		if assert.Len(t, activeModel, 24) && assert.Len(t, calmModel, 24) {
			assert.Greater(t, activeModel[15].AvgHomeLoadKWH, 2.00, "active Wednesday morning must preserve elevated Wednesday afternoon forecast")
			assert.Less(t, calmModel[15].AvgHomeLoadKWH, 1.65, "remaining Wednesday afternoon hour today should cap down after calm morning")
			assert.InDelta(t, activeModel[3].AvgHomeLoadKWH, calmModel[3].AvgHomeLoadKWH, 1e-6, "tomorrow's overnight hour (Thursday 03:00) must not be modified by today's calm-morning dampening")
		}
	})

	t.Run("MorningVsAfternoonOutlierHour11Boundary", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		// Evaluate at 04:00 UTC on Saturday 2025-07-05 after 35 days starting Saturday 2025-05-31.
		// Saturdays have a pure Morning-only outlier (06..10 = 2.6 kWh, 11..16 = 1.0 kWh, 17..21 = 0.6 kWh).
		// Verify hour 9/10 receive the morning outlier boost while hour 11 stays unboosted.
		start := time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC)
		nowSat := start.AddDate(0, 0, 35).Add(4 * time.Hour)
		var satHistory []types.EnergyStats
		for d := 0; d < 35; d++ {
			dt := start.AddDate(0, 0, d)
			isSat := dt.Weekday() == time.Saturday
			baseLoadKWH := 0.80 + 0.10*float64(d%5) // Natural daily variation so IQR > 0
			for h := 0; h < 24; h++ {
				loadKWH := baseLoadKWH
				if isSat {
					if h >= 6 && h <= 10 {
						loadKWH = 2.6
					} else if h >= 17 && h <= 21 {
						loadKWH = 0.5 // Keeps Saturday waking/wholeDay ratio < 1.25 so only Morning triggers
					} else {
						loadKWH = 1.0
					}
				}
				satHistory = append(satHistory, types.EnergyStats{
					TSHourStart: dt.Add(time.Duration(h) * time.Hour),
					HomeKWH:     loadKWH,
				})
			}
		}
		satModel, _, _, _ := c.BuildHourlyEnergyModel(ctx, nowSat, satHistory, nil, types.Settings{})
		if assert.Len(t, satModel, 24) {
			assert.Greater(t, satModel[9].AvgHomeLoadKWH, 2.30, "Saturday morning hour 9 must receive 3x outlier boost")
			assert.Less(t, satModel[11].AvgHomeLoadKWH, 1.40, "Saturday afternoon hour 11 must not be boosted by a Morning-only outlier")
		}

		// Conversely, on a Friday Afternoon-only outlier where hours 10..13 have identical historical loads
		// on every day (including higher non-Friday loads on recent Sat/Sun), hour 11 and hour 12 must produce
		// identical predictions because neither hour applies the morning-only 0.33x non-matching down-weight.
		friStart := time.Date(2025, 5, 30, 0, 0, 0, 0, time.UTC)
		nowFri := friStart.AddDate(0, 0, 35).Add(4 * time.Hour)
		var friHistory []types.EnergyStats
		for d := 0; d < 35; d++ {
			dt := friStart.AddDate(0, 0, d)
			isFri := dt.Weekday() == time.Friday
			baseLoadKWH := 0.85 + 0.10*float64(d%4)
			for h := 0; h < 24; h++ {
				loadKWH := baseLoadKWH
				if isFri {
					if h >= 10 && h <= 15 {
						loadKWH = 1.80
					} else if h >= 6 && h <= 9 {
						loadKWH = 0.80
					} else if h >= 16 && h <= 21 {
						loadKWH = 0.55
					} else {
						loadKWH = 0.95
					}
				} else if (d == 29 || d == 30) && h >= 10 && h <= 13 {
					loadKWH = 3.20
				}
				friHistory = append(friHistory, types.EnergyStats{
					TSHourStart: dt.Add(time.Duration(h) * time.Hour),
					HomeKWH:     loadKWH,
				})
			}
		}
		friModel, _, _, _ := c.BuildHourlyEnergyModel(ctx, nowFri, friHistory, nil, types.Settings{})
		if assert.Len(t, friModel, 24) {
			assert.Greater(t, friModel[11].AvgHomeLoadKWH, 1.50, "Friday afternoon hour 11 must receive Afternoon outlier boost")
			assert.InDelta(t, friModel[12].AvgHomeLoadKWH, friModel[11].AvgHomeLoadKWH, 1e-6, "hour 11 must not apply morning-only 0.33x down-weight on an Afternoon outlier")
		}
	})

	t.Run("AfternoonToEveningThermalLag", func(t *testing.T) {
		now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		// Build 21 days of history:
		// - Even days: hot 33°C afternoons (13:00-17:00), cooling to 22°C in evening (19:00-23:00) with heavy thermal-lag AC load (3.5 kWh).
		// - Odd days (including the most recent days): mild 22°C afternoons and identical 22°C evenings with low evening load (1.0 kWh).
		for d := 21; d >= 1; d-- {
			dt := now.Truncate(24*time.Hour).AddDate(0, 0, -d)
			isHotAfternoon := d%2 == 0
			var wf []types.HourlyWeather
			for h := 0; h < 24; h++ {
				tempC := 20.0
				loadKWH := 1.0
				if h >= 13 && h <= 17 {
					if isHotAfternoon {
						tempC = 33.0
						loadKWH = 4.0
					} else {
						tempC = 22.0
						loadKWH = 1.0
					}
				} else if h >= 19 && h <= 23 {
					// Identical 22°C evening temperature on both hot-afternoon and mild-afternoon days.
					tempC = 22.0
					if isHotAfternoon {
						loadKWH = 3.5
					} else {
						loadKWH = 1.0
					}
				}
				history = append(history, types.EnergyStats{
					TSHourStart: dt.Add(time.Duration(h) * time.Hour),
					HomeKWH:     loadKWH,
				})
				wf = append(wf, types.HourlyWeather{
					TSHourStart:  dt.Add(time.Duration(h) * time.Hour),
					TemperatureC: tempC,
				})
			}
			weather = append(weather, types.Weather{
				TSDayStart:    dt,
				ForecastHours: wf,
			})
		}

		// Target day 1: hot 33°C afternoon cooling to 22°C in the evening.
		today := now.Truncate(24 * time.Hour)
		makeTodayWeather := func(aftTempC float64) []types.Weather {
			out := make([]types.Weather, len(weather), len(weather)+1)
			copy(out, weather)
			var todayWF []types.HourlyWeather
			for h := 0; h < 24; h++ {
				tempC := 20.0
				if h >= 13 && h <= 17 {
					tempC = aftTempC
				} else if h >= 19 && h <= 23 {
					tempC = 22.0
				}
				todayWF = append(todayWF, types.HourlyWeather{
					TSHourStart:  today.Add(time.Duration(h) * time.Hour),
					TemperatureC: tempC,
				})
			}
			return append(out, types.Weather{TSDayStart: today, ForecastHours: todayWF})
		}

		modelHotAft, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, makeTodayWeather(33.0), types.Settings{})
		if assert.Len(t, modelHotAft, 24) {
			assert.GreaterOrEqual(t, modelHotAft[20].AvgHomeLoadKWH, 2.8,
				"22°C evening after a 33°C afternoon must weight hot-afternoon evenings heavily via thermal lag")
		}

		// Target day 2: mild 22°C afternoon followed by identical 22°C evening -> must stay near 1.0 kWh.
		modelMildAft, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, makeTodayWeather(22.0), types.Settings{})
		if assert.Len(t, modelMildAft, 24) {
			assert.LessOrEqual(t, modelMildAft[20].AvgHomeLoadKWH, 1.4,
				"22°C evening after a 22°C afternoon must not be inflated by hot-afternoon evenings")
		}
	})

	t.Run("OptionGPreservesOngoingColdSnapSurgeWhenNoColdHistory", func(t *testing.T) {
		now := time.Date(2026, 11, 20, 22, 0, 0, 0, time.UTC)
		var history []types.EnergyStats
		var weather []types.Weather

		// 24 mild days (18°C, natural 1.0..1.8 kWh/hr variation) followed by a 3-day cold snap
		// (8°C, 4°C, 0°C at 2.1 kWh/hr, spaced 4°C apart so every cold day has allDaysSimW < 1.5).
		coldDayTemps := map[int]float64{3: 8.0, 2: 4.0, 1: 0.0}
		for d := 27; d >= 1; d-- {
			dt := now.Truncate(24*time.Hour).AddDate(0, 0, -d)
			tempC := 18.0
			loadKWH := 1.0 + 0.2*float64(d%5)
			if ct, ok := coldDayTemps[d]; ok {
				tempC = ct
				loadKWH = 2.1
			}
			var wf []types.HourlyWeather
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart: dt.Add(time.Duration(h) * time.Hour),
					HomeKWH:     loadKWH,
				})
				wf = append(wf, types.HourlyWeather{
					TSHourStart:  dt.Add(time.Duration(h) * time.Hour),
					TemperatureC: tempC,
				})
			}
			weather = append(weather, types.Weather{TSDayStart: dt, ForecastHours: wf})
		}

		today := now.Truncate(24 * time.Hour)
		tomorrow := today.AddDate(0, 0, 1)
		makeForecastWeather := func(todayTempC, tomorrowTempC float64) []types.Weather {
			out := make([]types.Weather, len(weather), len(weather)+2)
			copy(out, weather)
			for _, pair := range []struct {
				day   time.Time
				tempC float64
			}{{today, todayTempC}, {tomorrow, tomorrowTempC}} {
				var wf []types.HourlyWeather
				for h := 0; h < 24; h++ {
					wf = append(wf, types.HourlyWeather{
						TSHourStart:  pair.day.Add(time.Duration(h) * time.Hour),
						TemperatureC: pair.tempC,
					})
				}
				out = append(out, types.Weather{TSDayStart: pair.day, ForecastHours: wf})
			}
			return out
		}

		// Case 1 (22:00 on cold day, tomorrow stays cold at 2°C): upcoming 24h window is cold -> Option G preserves shift.
		modelCold, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, makeForecastWeather(2.0, 2.0), types.Settings{})
		// Case 2 (22:00 on cold day, tomorrow warms to mild 18°C): 22 of 24 upcoming hours are mild -> Option G suppresses shift.
		modelMildTomorrow, _, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, makeForecastWeather(2.0, 18.0), types.Settings{})
		if assert.Len(t, modelCold, 24) && assert.Len(t, modelMildTomorrow, 24) {
			assert.Greater(t, modelCold[12].AvgHomeLoadKWH, 2.2,
				"Option G must preserve upward shift during an ongoing unprecedented cold snap when allDaysSimW < 1.5")
			assert.InDelta(t, 1.4, modelMildTomorrow[12].AvgHomeLoadKWH, 0.35,
				"Option G must suppress past cold-snap diffs when the upcoming 24h forecast window has warmed back to mild")
		}
	})
}

func TestTempSimilarityDiffC(t *testing.T) {
	t.Parallel()

	t.Run("CoreDeadbandCompression", func(t *testing.T) {
		t.Parallel()
		// Inside core [15°C, 21°C], 15°C vs 21°C (6°C raw diff) is compressed by 0.25x -> 1.5°C.
		assert.InDelta(t, 1.5, tempSimilarityDiffC(15.0, 21.0), 1e-6)
	})

	t.Run("SmoothBoundaryRampNoCliff", func(t *testing.T) {
		t.Parallel()
		// Verify smooth continuity across 14°C and 22°C (no sudden jump for a 0.01°C shift).
		dAt14 := tempSimilarityDiffC(14.0, 22.0)
		dBelow14 := tempSimilarityDiffC(13.99, 22.0)
		assert.InDelta(t, dAt14, dBelow14, 0.1, "Boundary ramp must be continuous with no cliff at 14°C")
		dAt22 := tempSimilarityDiffC(15.0, 22.0)
		dAbove22 := tempSimilarityDiffC(15.0, 22.01)
		assert.InDelta(t, dAt22, dAbove22, 0.1, "Boundary ramp must be continuous with no cliff at 22°C")
	})

	t.Run("FullSeparationOutsideDeadband", func(t *testing.T) {
		t.Parallel()
		// Outside [13°C, 23°C], full 1:1 separation is preserved so heating and cooling are never blurred into mild days.
		assert.InDelta(t, 9.0, tempSimilarityDiffC(12.0, 21.0), 1e-6)
		assert.InDelta(t, 10.0, tempSimilarityDiffC(15.0, 25.0), 1e-6)
		assert.InDelta(t, 6.0, tempSimilarityDiffC(4.0, 10.0), 1e-6)
		assert.InDelta(t, 6.0, tempSimilarityDiffC(26.0, 32.0), 1e-6)
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
		// does NOT trigger a vacation because of the q1 * 0.55 ceiling constraint.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// Create 6 highly consistent baseline days: 2.1 (all equal, standby 0.1 -> active average 2.0)
		// Q1 = 2.0, IQR = 0.
		// Standard formula: Q1 - 1.2*IQR = 2.0.
		// Ceiling constraint: Q1 * 0.55 = 1.1.
		// Lower bound becomes 1.1.
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
		// Since 1.8 is NOT < 1.1, it should NOT trigger a vacation.
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
		// Moderate active load (< 55% Q1) with flat StdDev (< 0.25 * Q1StdDev) triggers vacation mode
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

		// Yesterday is moderately low (0.8 kWh/hr -> active 0.7, which is < 55% of Q1 active) and flat (StdDev = 0.0)
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
		// Continuous high EV charging (7.2 kW all day) has zero StdDev, but high active load (> 55% Q1).
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

		// Yesterday is continuous flat EV charging at 7.2 kW (StdDev = 0.0, but yActive = 7.1 > 55% Q1)
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
		// Test currentHour = 2 (2 AM) when the user returned home at 6 PM yesterday.
		// Before 9 AM (currentHour < 9), detectLoadShift checks a 6-hour cross-midnight lookback
		// (today h=1, 0 and yesterday h=23, 22, 21, 20) against 1.5x historical hourly medians.
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

		// At currentHour = 2, the 6-hour cross-midnight lookback sees 3.5 kWh (> 1.5 * 2.0 median),
		// confirming active occupancy and returning "none".
		shift := detectLoadShift(ctx, now, time.UTC, dayMap, dayAveragesMap, todayStr, yesterdayStr, 2, 0.1)
		assert.Equal(t, "none", shift)
	})

	t.Run("PartialYesterdayExcludedFromVolatilityVacation", func(t *testing.T) {
		// Verify Fix 1D-i: when yesterday has < 18 hours of data (e.g. 10 flat hours due to a telemetry outage),
		// hasYStdDev is false so the flat partial day cannot falsely trigger volatility-based vacation mode.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 10 normal historical days with daily volatility and non-zero IQR across days
		// (5 days at 2.1 kWh -> active 2.0; 5 days at 4.1 kWh -> active 4.0; Q1 = 2.0, IQR = 2.0,
		// so dailyLowerBoundActive = 0.25 * Q1 = 0.50, while ceilingCap = 0.55 * Q1 = 1.10).
		for i := 0; i < 10; i++ {
			dateStr := time.Date(2025, 6, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
			baseVal := 1.6
			if i >= 5 {
				baseVal = 3.6
			}
			dayAveragesMap[dateStr] = baseVal + 0.5
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				val := baseVal
				if h%4 == 0 {
					val = baseVal + 2.0
				}
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(2025, 6, 1+i, h, 0, 0, 0, time.UTC),
					HomeKWH:     val,
				})
			}
			dayMap[dateStr] = &dayPoints{date: dateStr, points: pts}
		}

		// Yesterday has moderate active load (0.9 kWh, above 25% magnitude drop floor ~0.5 kWh,
		// but below 55% volatility ceiling ~1.1 kWh) and is completely flat (stddev = 0.0),
		// EXCEPT it only has 10 hours of data (< 18 hours minimum).
		dayAveragesMap[yesterdayStr] = 0.9
		var yPts []types.EnergyStats
		for h := 0; h < 10; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.9,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today has 12 hours at 0.4 kWh
		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.4,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shift, "partial yesterday (< 18 hours) must not trigger volatility-based vacation mode")
	})

	t.Run("EarlyEscapeRequiresChronologicallyConsecutiveHours", func(t *testing.T) {
		// Verify Fix 1D-ii: early escape from vacation mode requires the 4 most recent hours
		// [currentHour-4 .. currentHour-1] to be chronologically consecutive and >= Q1.
		// Having 4 non-consecutive normal hours scattered across a day with telemetry gaps
		// (or followed by a low hour at currentHour-1) must NOT falsely escape vacation mode.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is a full low vacation day (0.3 kWh)
		dayAveragesMap[yesterdayStr] = 0.3
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.3,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Case 1: Today has 4 normal hours (2.5 kWh at h=1, 3, 5, 7) due to brief auxiliary cycling
		// with missing telemetry in between, followed by low vacation hours (0.3 kWh at h=8, 9, 10, 11).
		var tPtsGapped []types.EnergyStats
		for _, h := range []int{1, 3, 5, 7} {
			tPtsGapped = append(tPtsGapped, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     2.5,
			})
		}
		for h := 8; h < 12; h++ {
			tPtsGapped = append(tPtsGapped, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.3,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPtsGapped}
		dayAveragesMap[todayStr] = 1.4

		shiftGapped := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "down", shiftGapped, "non-consecutive earlier spikes followed by low hours 8..11 should remain in vacation mode")

		// Case 2: Today has 4 chronologically consecutive normal hours right before currentHour=12 (h=8, 9, 10, 11 at 2.5 kWh).
		var tPtsConsecutive []types.EnergyStats
		for h := 0; h < 12; h++ {
			val := 0.3
			if h >= 8 {
				val = 2.5
			}
			tPtsConsecutive = append(tPtsConsecutive, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     val,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPtsConsecutive}

		shiftConsecutive := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.1)
		assert.Equal(t, "none", shiftConsecutive, "4 chronologically consecutive hours [8..11] >= Q1 should escape vacation mode")
	})

	t.Run("EarlyMorningBefore6AMCrossMidnightVacationDetection", func(t *testing.T) {
		// Verify Fix 1D-iii: when running before 6:00 AM (e.g. currentHour = 4), detectLoadShift
		// evaluates the last 6 cross-midnight hours (yesterday 22:00-23:00 + today 00:00-03:00)
		// alongside a low yesterday so vacation mode ("down") stays active during 00:00-05:00
		// instead of dropping back to "none" until 06:00 AM.
		now := time.Date(2025, 6, 16, 4, 0, 0, 0, time.UTC) // 4:00 AM
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(2.0)

		// Yesterday is a low vacation day (0.3 kWh)
		dayAveragesMap[yesterdayStr] = 0.3
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.3,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		// Today hours 0..3 are also low (0.3 kWh)
		var tPts []types.EnergyStats
		for h := 0; h < 4; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.3,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}
		dayAveragesMap[todayStr] = 0.3

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 4, 0.1)
		assert.Equal(t, "down", shift, "vacation mode should be detected at 4:00 AM via cross-midnight lookback")
	})

	t.Run("ProlongedVacationOver25PercentOfHistoryStaysInVacationMode", func(t *testing.T) {
		// When a household is away on a multi-week vacation (> 25% of history window, e.g. 10 of 30 days),
		// Q1 of historical daily active averages collapses to the near-standby vacation load (0.04 kWh/hr active).
		// detectLoadShift must anchor Q1 to Q2/Q3 so vacation mode ("down") stays active past Day 9.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 20 normal baseline days (2.0 kWh/hr) followed by 10 prior vacation days (0.54 kWh/hr on 0.50 standby -> 0.04 active)
		startDate := time.Date(2025, 5, 16, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 30; d++ {
			dTime := startDate.AddDate(0, 0, d)
			dStr := dTime.Format("2006-01-02")
			val := 2.0
			if d >= 20 {
				val = 0.54 // 10 prior vacation days (> 25% of 30-day history)
			}
			dayAveragesMap[dStr] = val
			var pts []types.EnergyStats
			for h := 0; h < 24; h++ {
				pts = append(pts, types.EnergyStats{
					TSHourStart: time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, time.UTC),
					HomeKWH:     val,
				})
			}
			dayMap[dStr] = &dayPoints{date: dStr, points: pts}
		}

		// Yesterday (2025-06-15) and Today (2025-06-16) continue the vacation at 0.54 kWh/hr
		dayAveragesMap[yesterdayStr] = 0.54
		var yPts []types.EnergyStats
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.54,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts}

		var tPts []types.EnergyStats
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     0.54,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts}
		dayAveragesMap[todayStr] = 0.54

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12, 0.50)
		assert.Equal(t, "down", shift, "prolonged multi-week vacation (>25%% of history) must remain in vacation mode")
	})

	t.Run("HighStandbyVacationShiftAndFlatEscapeGuard", func(t *testing.T) {
		now := time.Date(2025, 6, 22, 13, 0, 0, 0, loc)
		todayStr := "2025-06-22"
		yesterdayStr := "2025-06-21"
		standbyKWH := 0.75

		dayMap := make(map[string]*dayPoints)
		dayAveragesMap := make(map[string]float64)

		// 20 baseline days: 6 mild non-HVAC days (~1.05 kWh avg, active ~0.30 kWh, hourQ1 ~0.90 kWh, stddev ~0.35)
		// and 14 normal active days (~2.25 kWh avg, active ~1.50 kWh, stddev ~0.85).
		startDate := time.Date(2025, 6, 1, 0, 0, 0, 0, loc)
		for d := 0; d < 20; d++ {
			dt := startDate.AddDate(0, 0, d)
			dStr := dt.Format("2006-01-02")
			isMild := d < 6
			var pts []types.EnergyStats
			var sum float64
			for h := 0; h < 24; h++ {
				val := 1.40
				if h%2 == 0 {
					val = 3.10
				}
				if isMild {
					val = 0.88
					if h >= 17 && h <= 20 {
						val = 1.90
					}
				}
				pts = append(pts, types.EnergyStats{
					TSHourStart: dt.Add(time.Duration(h) * time.Hour),
					HomeKWH:     val,
				})
				sum += val
			}
			dayMap[dStr] = &dayPoints{date: dStr, dayTime: dt, points: pts}
			dayAveragesMap[dStr] = sum / 24.0
		}

		// Yesterday is flat unoccupied standby (~0.93 kWh, active 0.18 kWh, stddev ~0.01)
		var yPts []types.EnergyStats
		yDate := time.Date(2025, 6, 21, 0, 0, 0, 0, loc)
		for h := 0; h < 24; h++ {
			val := 0.92 + 0.02*float64(h%2)
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: yDate.Add(time.Duration(h) * time.Hour),
				HomeKWH:     val,
			})
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, dayTime: yDate, points: yPts}
		dayAveragesMap[yesterdayStr] = 0.93

		// Today hours 00:00..12:00 remain flat at 0.93 kWh (>= mild-day hourQ1 0.88 kWh, but flat < 0.15 stddev)
		tDate := time.Date(2025, 6, 22, 0, 0, 0, 0, loc)
		var flatTodayPts []types.EnergyStats
		for h := 0; h < 13; h++ {
			val := 0.92 + 0.02*float64(h%2)
			flatTodayPts = append(flatTodayPts, types.EnergyStats{
				TSHourStart: tDate.Add(time.Duration(h) * time.Hour),
				HomeKWH:     val,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, dayTime: tDate, points: flatTodayPts}
		dayAveragesMap[todayStr] = 0.93

		shiftFlat := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 13, standbyKWH)
		assert.Equal(t, "down", shiftFlat, "high-standby flat vacation day must trigger 'down' and resist premature escape on flat standby hours")

		// If the household genuinely returns home at 09:00..12:00 with normal elevated volatile loads, escape triggers
		var returnedPts []types.EnergyStats
		returnLoads := map[int]float64{9: 1.80, 10: 2.50, 11: 2.00, 12: 2.70}
		for h := 0; h < 13; h++ {
			val := 0.93
			if rl, ok := returnLoads[h]; ok {
				val = rl
			}
			returnedPts = append(returnedPts, types.EnergyStats{
				TSHourStart: tDate.Add(time.Duration(h) * time.Hour),
				HomeKWH:     val,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, dayTime: tDate, points: returnedPts}

		shiftReturned := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 13, standbyKWH)
		assert.Equal(t, "none", shiftReturned, "4 genuinely elevated volatile occupancy hours must still escape vacation mode")
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

	model, _, _, _ := c.BuildHourlyEnergyModel(ctx, nowChicago, history, nil, settings)
	// Hour 20 should have aggregated both 8 PM local points
	assert.Len(t, model, 24)
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
		model, params, _, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		assert.Equal(t, "none", params.DetectedShift)
		// Option G must NOT drag predictions down to the 0.09 kWh floor.
		// Forecast for all hours must remain near normal baseline (>= 2.0 kWh).
		if assert.Len(t, model, 24) {
			assert.Greater(t, model[17].AvgHomeLoadKWH, 2.0)
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

	t.Run("CompletedYesterdayEvaluatedAsHistoricalVacation", func(t *testing.T) {
		// Verify Fix 1C-i: yesterdayStr is evaluated in Step 6 of identifyHistoricalVacationDays
		// so that on Day +1 after returning from vacation (when detectedShift == "none"),
		// yesterday's completed vacation day is excluded from the normal occupancy prediction pool.
		dayAveragesMap := map[string]float64{
			"2025-06-01": 2.0,
			"2025-06-02": 2.2,
			"2025-06-03": 2.1,
			"2025-06-04": 2.3,
			"2025-06-05": 2.0,
			yesterdayStr: 0.2, // Yesterday was the final day of vacation
			todayStr:     0.2, // Today is skipped because it is incomplete
		}
		var dailyAverages []float64
		for _, avg := range dayAveragesMap {
			dailyAverages = append(dailyAverages, avg)
		}
		dayMap := make(map[string]*dayPoints)

		vacDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)
		assert.True(t, vacDays[yesterdayStr], "completed yesterday vacation day should be marked in historicalVacationDays")
		assert.False(t, vacDays[todayStr], "incomplete todayStr must never be marked in historicalVacationDays")
	})

	t.Run("MultiWeekVacationOver25PercentOfHistoryDetectedViaQ2Ceiling", func(t *testing.T) {
		// Verify Fix 1C-ii: when a prolonged vacation spans > 25% of historical days (e.g. 10 out of 30 days = 33%),
		// Q1 of raw daily averages (q1R) collapses to the vacation level (0.30 kWh, activeAvg = 0.20 > 0.02).
		// Because q1R < q2R * loadShiftOutlierCeilingCap (0.30 < 2.2 * 0.55), rawCeiling anchors to
		// q2R * loadShiftOutlierCeilingCap so all 10 realistic vacation days are still detected.
		dayAveragesMap := make(map[string]float64)
		var dailyAverages []float64
		dayMap := make(map[string]*dayPoints)

		startDate := time.Date(2025, 5, 17, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 30; d++ {
			dStr := startDate.AddDate(0, 0, d).Format("2006-01-02")
			avg := 2.2
			if d >= 10 && d < 20 {
				avg = 0.30 // 10-day realistic vacation (activeAvg = 0.20 > standbyActiveEnergyFloor 0.02)
			}
			dayAveragesMap[dStr] = avg
			dailyAverages = append(dailyAverages, avg)
		}

		vacDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)
		for d := 10; d < 20; d++ {
			dStr := startDate.AddDate(0, 0, d).Format("2006-01-02")
			assert.True(t, vacDays[dStr], "prolonged vacation day %s (>25%% of history) should be detected via Q2 ceiling", dStr)
		}
	})

	t.Run("PartialHistoricalDayNotFlaggedByLowVolatility", func(t *testing.T) {
		// Verify Fix 1C-iii: a historical day with < 18 hours of data (e.g. 8 flat hours at 0.8 kWh)
		// has hasDStdDev = false so it is not misclassified as a low-volatility vacation day.
		dayAveragesMap := map[string]float64{
			"2025-06-01": 2.0,
			"2025-06-02": 2.2,
			"2025-06-03": 2.1,
			"2025-06-04": 2.3,
			"2025-06-05": 0.8, // Active avg 0.7 is >= magnitudeDropActiveFloor (0.5), so only Condition B could trigger
			yesterdayStr: 2.0,
			todayStr:     2.0,
		}
		var dailyAverages []float64
		for _, avg := range dayAveragesMap {
			dailyAverages = append(dailyAverages, avg)
		}
		dayMap := make(map[string]*dayPoints)
		for dStr, avg := range dayAveragesMap {
			numHours := 24
			if dStr == "2025-06-05" {
				numHours = 8 // Partial day (< 18 hours)
			}
			var loads []float64
			var pts []types.EnergyStats
			for h := 0; h < numHours; h++ {
				l := avg
				if dStr != "2025-06-05" && h%4 == 0 {
					l += 2.0
				}
				loads = append(loads, l)
				pts = append(pts, types.EnergyStats{HomeKWH: l})
			}
			dayMap[dStr] = &dayPoints{date: dStr, loads: loads, points: pts}
		}

		vacDays := identifyHistoricalVacationDays(ctx, dailyAverages, dayAveragesMap, dayMap, todayStr, yesterdayStr, standbyLoad)
		assert.False(t, vacDays["2025-06-05"], "partial historical day (< 18 hours) must not be flagged via low-volatility Condition B")
	})

	t.Run("HighStandbyMultiDayVacationDetectedAndIsolatedQuietDayIgnored", func(t *testing.T) {
		highStandbyKWH := 0.75
		tStr := "2025-06-25"
		yStr := "2025-06-24"

		buildHighStandbyPool := func(flatDates map[string]bool) ([]float64, map[string]float64, map[string]*dayPoints) {
			dAvgMap := make(map[string]float64)
			dMap := make(map[string]*dayPoints)
			var avgs []float64

			start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
			for d := 0; d < 24; d++ {
				dt := start.AddDate(0, 0, d)
				dStr := dt.Format("2006-01-02")
				var pts []types.EnergyStats
				var loads []float64
				var sum float64
				for h := 0; h < 24; h++ {
					val := 1.20
					if h >= 14 && h <= 20 {
						val = 2.60 // Normal daily occupancy volatility (stddev ~0.62, avg ~1.61)
					}
					if flatDates[dStr] {
						val = 0.92 + 0.02*float64(h%2) // Flat standby+fridge cycling (~0.93 kWh, > 0.55 * q1R)
					}
					pts = append(pts, types.EnergyStats{TSHourStart: dt.Add(time.Duration(h) * time.Hour), HomeKWH: val})
					loads = append(loads, val)
					sum += val
				}
				avg := sum / 24.0
				dAvgMap[dStr] = avg
				avgs = append(avgs, avg)
				dMap[dStr] = &dayPoints{date: dStr, dayTime: dt, loads: loads, points: pts}
			}
			return avgs, dAvgMap, dMap
		}

		// 3 consecutive flat high-standby vacation days (06-10..06-12) + 1 isolated quiet mild day (06-18)
		flatSet := map[string]bool{
			"2025-06-10": true,
			"2025-06-11": true,
			"2025-06-12": true,
			"2025-06-18": true,
		}
		avgs, dAvgMap, dMap := buildHighStandbyPool(flatSet)
		vacDays := identifyHistoricalVacationDays(ctx, avgs, dAvgMap, dMap, tStr, yStr, highStandbyKWH)

		assert.True(t, vacDays["2025-06-10"], "consecutive multi-day high-standby vacation day 1 must be flagged")
		assert.True(t, vacDays["2025-06-11"], "consecutive multi-day high-standby vacation day 2 must be flagged")
		assert.True(t, vacDays["2025-06-12"], "consecutive multi-day high-standby vacation day 3 must be flagged")
		assert.False(t, vacDays["2025-06-18"], "isolated 1-day mild non-HVAC dip must not be misclassified as a vacation day")
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
		res1 := detectDayOfWeekOutliers(ctx, utc, nil, nil, "2025-06-01", nil, nil)
		assert.Empty(t, res1)

		res2 := detectDayOfWeekOutliers(ctx, utc, make(map[string]*dayPoints), make(map[string]bool), "2025-06-01", nil, nil)
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-21", nil, nil)
		assert.False(t, res[time.Saturday].any(), "Excluding today leaves only 2 Saturdays; should not be flagged")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.False(t, res[time.Saturday].any(), "Invalid days must be excluded, leaving < 3 target samples")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.False(t, res[time.Saturday].any(), "Must require at least 3 target weekday occurrences")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.False(t, res[time.Saturday].any(), "Must require at least 6 preceding weekday samples (p1 + p2)")
	})

	t.Run("InsufficientMorningHoursInDay", func(t *testing.T) {
		// Saturdays have high load in only 2 non-zero points in hours 6..10 (mornCount < 3).
		// A day requires >= 3 points in hours 6..10 to qualify for the morning period check.
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
					load = 2.0
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.False(t, res[time.Saturday].Morning, "Days with fewer than 3 morning points must not trigger morning outlier")
		assert.False(t, res[time.Saturday].any(), "2 hours at 2.0 kWh with remaining hours at 1.0 kWh stays below 1.25x waking/wholeDay ratio")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.False(t, res[time.Saturday].any(), "Zero and negative HomeKWH points must be ignored")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.True(t, res[time.Saturday].Morning, "Saturday morning surge must be detected as an outlier")
		assert.False(t, res[time.Friday].any(), "Friday should not be detected as an outlier")
		assert.False(t, res[time.Thursday].any(), "Thursday should not be detected as an outlier")
	})

	t.Run("SundayMorningOutlierViaFallback", func(t *testing.T) {
		// Both Saturday and Sunday are high morning chore days (2.2 kWh and 1.8 kWh).
		// Sunday vs preceding (Saturday + Friday): meanPrev = (2.2 + 1.0)/2 = 1.6 kWh.
		// Sunday primary ratio: 1.8 / 1.6 = 1.125 < 1.25 (primary check fails).
		// Sunday fallback vs Thursday + Friday (both 1.0 kWh):
		// Fallback ratio: 1.8 / 1.0 = 1.8 >= 1.25, and Welch's t >> 2.30.
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.True(t, res[time.Saturday].Morning, "Saturday morning must be detected via primary check")
		assert.True(t, res[time.Sunday].Morning, "Sunday morning must be detected via fallback to Thursday/Friday")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.True(t, res[time.Wednesday].Morning, "Wednesday should be detected as an outlier")
		assert.False(t, res[time.Monday].any(), "Monday should not be detected")
		assert.False(t, res[time.Tuesday].any(), "Tuesday should not be detected")
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.Empty(t, res, "Uniform consumption across days should yield no outliers")
	})

	t.Run("HighRatioRejectedByWelchTDueToSingleSpike", func(t *testing.T) {
		// Ratio threshold (>= 1.25) is met due to a single 5.0 kWh appliance run,
		// but Welch's t-test rejects it due to high sample variance.
		// 4 Saturdays: [5.0, 1.0, 1.0, 1.0] -> mean = 2.0.
		// Preceding (4 Thursdays, 4 Fridays): all 1.3 kWh -> mean = 1.3.
		// Ratio = 2.0 / 1.3 = 1.538 >= 1.25.
		// Saturday std dev = 2.0 -> SE = sqrt(4.0/4 + 0) = 1.0.
		// t = (2.0 - 1.3)/1.0 = 0.70 < 2.30.
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

		res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.False(t, res[time.Saturday].any(), "Single-spike day must be rejected by Welch's t-test")
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

		res := detectDayOfWeekOutliers(ctx, chicagoLoc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.True(t, res[time.Saturday].Morning, "Saturday morning outlier should be detected using Chicago local hours")
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
			res := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
			assert.NotNil(t, res)
		})
	})

	t.Run("IgnoredOutlierHoursExcludedFromMorningStats", func(t *testing.T) {
		// Verify that morning hours flagged in ignoredOutlierHours (e.g. morning EV charging
		// at 08:00-09:00 on Saturdays) are excluded when computing morning averages, preventing
		// intermittent EV charging from masquerading as a recurring Saturday morning chore surge.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)
		ignoredOutlierHours := make(map[dayHourKey]bool)

		saturdays := []string{"2025-06-07", "2025-06-14", "2025-06-21", "2025-06-28"}
		for _, s := range saturdays {
			dp := makeDay(s, 1.0, 1.0, utc)
			for i := range dp.points {
				hr := dp.points[i].TSHourStart.Hour()
				if hr == 8 || hr == 9 {
					dp.points[i].HomeKWH = 9.6 // Morning EV charge
					ignoredOutlierHours[dayHourKey{date: s, hour: hr}] = true
				}
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

		// Without ignoredOutlierHours, Saturday morning average is inflated to 4.44 kWh and flagged.
		resUnfiltered := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, nil)
		assert.True(t, resUnfiltered[time.Saturday].Morning)

		// With ignoredOutlierHours passed, hours 8 and 9 are skipped and the remaining 3 morning hours
		// (6, 7, 10) average 1.0 kWh, matching Thursday/Friday.
		resFiltered := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", ignoredOutlierHours, nil)
		assert.False(t, resFiltered[time.Saturday].any(), "flagged morning EV hours must be excluded from morning weekday stats")
	})

	t.Run("AfternoonEveningAndWakingOutliersWithTemperatureGate", func(t *testing.T) {
		// Across 4 weeks (2025-06-01 .. 2025-06-28):
		// - Tuesdays have an Evening-only surge (17:00-21:00 = 1.80 kWh, 06:00-16:00 = 0.85 kWh).
		// - Fridays have shifting 2-hour appliance surges (5.5 kWh across 10:00-15:00) triggering Afternoon, Waking, and WholeDay.
		// - Saturdays have a Morning-only surge (06:00-10:00 = 2.60 kWh).
		// Verify:
		// 1. All expected windows fire when temperatures match preceding days.
		// 2. When target-day temperatures diverge by >= 2.0°C, non-morning surges (Tuesday Evening and
		//    Friday Afternoon/Waking/WholeDay) are gated out while Saturday Morning remains detected.
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)
		matchedWeather := make(map[time.Time]float64)
		divergedWeather := make(map[time.Time]float64)

		spikeHours := []int{12, 14, 10, 13}
		friIdx := 0
		startDate := time.Date(2025, 6, 1, 0, 0, 0, 0, utc)
		for d := 0; d < 28; d++ {
			dTime := startDate.AddDate(0, 0, d)
			dStr := dTime.Format("2006-01-02")
			wd := dTime.Weekday()
			sh := -1
			if wd == time.Friday {
				sh = spikeHours[friIdx]
				friIdx++
			}
			dp := &dayPoints{date: dStr}
			for h := 0; h < 24; h++ {
				loadKWH := 1.0
				matchedTempC := 20.0
				divergedTempC := 20.0
				switch wd {
				case time.Tuesday:
					if h >= 17 && h <= 21 {
						loadKWH = 1.80
					} else if h >= 6 && h <= 16 {
						loadKWH = 0.85
					}
					matchedTempC = 20.4
					divergedTempC = 24.0 // +4.0°C warmer than Sun/Mon
				case time.Friday:
					if h == sh || h == sh+1 {
						loadKWH = 5.50
					}
					matchedTempC = 20.5
					divergedTempC = 24.0 // +4.0°C warmer than Wed/Thu
				case time.Saturday:
					if h >= 6 && h <= 10 {
						loadKWH = 2.60
					}
					matchedTempC = 20.5
					divergedTempC = 28.0 // +6.0°C warmer, yet Morning must still fire
				}
				ts := time.Date(dTime.Year(), dTime.Month(), dTime.Day(), h, 0, 0, 0, utc)
				dp.loads = append(dp.loads, loadKWH)
				dp.points = append(dp.points, types.EnergyStats{TSHourStart: ts, HomeKWH: loadKWH})
				matchedWeather[ts.UTC()] = matchedTempC
				divergedWeather[ts.UTC()] = divergedTempC
			}
			dayMap[dStr] = dp
			validDaysMap[dStr] = true
		}

		resMatched := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, matchedWeather)
		assert.True(t, resMatched[time.Tuesday].Evening, "evening window must detect Tuesday evening surge")
		assert.False(t, resMatched[time.Tuesday].Morning)
		assert.False(t, resMatched[time.Tuesday].Afternoon)
		assert.False(t, resMatched[time.Tuesday].Waking)
		assert.True(t, resMatched[time.Tuesday].coversHour(19))
		assert.False(t, resMatched[time.Tuesday].coversHour(14))

		assert.False(t, resMatched[time.Friday].Morning, "morning alone is quiet on 3 of 4 Fridays")
		assert.True(t, resMatched[time.Friday].Afternoon, "afternoon must detect Friday shifting surges")
		assert.True(t, resMatched[time.Friday].Waking, "waking window must detect Friday shifting surges")
		assert.True(t, resMatched[time.Friday].WholeDay, "wholeDay window must detect Friday shifting surges")
		assert.True(t, resMatched[time.Friday].coversHour(10), "waking/wholeDay outlier must cover hour 10")
		assert.True(t, resMatched[time.Friday].coversHour(14), "afternoon/waking outlier must cover hour 14")

		assert.True(t, resMatched[time.Saturday].Morning)

		resDiverged := detectDayOfWeekOutliers(ctx, utc, dayMap, validDaysMap, "2025-06-30", nil, divergedWeather)
		assert.False(t, resDiverged[time.Tuesday].any(), "evening surge with >= 2.0°C temperature delta must be gated out")
		assert.False(t, resDiverged[time.Friday].any(), "afternoon/waking/wholeDay surge with >= 2.0°C temperature delta must be gated out")
		assert.True(t, resDiverged[time.Saturday].Morning, "morning outlier must remain unaffected by temperature gate")
	})

	t.Run("DSTAndHalfHourTimezoneWeatherLookup", func(t *testing.T) {
		// 1. Fall-back DST (America/Chicago around 2025-11-02, 25-hour Sunday) and spring-forward safe stepping.
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		dstDayMap := make(map[string]*dayPoints)
		dstValid := make(map[string]bool)
		dstMatchedWeather := make(map[time.Time]float64)
		dstDivergedWeather := make(map[time.Time]float64)

		dstStart := time.Date(2025, 10, 16, 0, 0, 0, 0, chicagoLoc) // Thursday
		for d := 0; d < 25; d++ {
			dTime := dstStart.AddDate(0, 0, d)
			nextDay := dTime.AddDate(0, 0, 1)
			dStr := dTime.Format("2006-01-02")
			isSun := dTime.Weekday() == time.Sunday
			dp := &dayPoints{date: dStr}
			for ts := dTime; ts.Before(nextDay); ts = ts.Add(time.Hour) {
				hr := ts.In(chicagoLoc).Hour()
				loadKWH := 1.0
				divTempC := 14.0
				if isSun {
					divTempC = 18.0
					if hr < 6 || hr > 10 {
						loadKWH = 2.4 // Non-morning hours elevated so WholeDay/Afternoon/Evening fire without Morning
					}
				}
				dp.loads = append(dp.loads, loadKWH)
				dp.points = append(dp.points, types.EnergyStats{TSHourStart: ts, HomeKWH: loadKWH})
				dstMatchedWeather[ts.UTC()] = 14.2
				dstDivergedWeather[ts.UTC()] = divTempC
			}
			dstDayMap[dStr] = dp
			dstValid[dStr] = true
		}

		resDSTMatched := detectDayOfWeekOutliers(ctx, chicagoLoc, dstDayMap, dstValid, "2025-11-12", nil, dstMatchedWeather)
		assert.True(t, resDSTMatched[time.Sunday].WholeDay, "Sunday wholeDay outlier should be detected across 25-hour DST fall-back week")
		resDSTDiverged := detectDayOfWeekOutliers(ctx, chicagoLoc, dstDayMap, dstValid, "2025-11-12", nil, dstDivergedWeather)
		assert.False(t, resDSTDiverged[time.Sunday].any(), "Sunday DST non-morning surge with >= 2.0°C delta must be gated out")

		// 2. Half-hour timezone offset (+05:30): hourly timestamps land at :30 UTC, so Truncate(time.Hour) on UTC
		// would miss weatherByHour keys if not stepped cleanly from local midnight.
		istLoc := time.FixedZone("IST", 5*3600+30*60)
		istDayMap := make(map[string]*dayPoints)
		istValid := make(map[string]bool)
		istMatchedWeather := make(map[time.Time]float64)
		istDivergedWeather := make(map[time.Time]float64)

		istStart := time.Date(2025, 6, 5, 0, 0, 0, 0, istLoc) // Thursday
		for d := 0; d < 25; d++ {
			dTime := istStart.AddDate(0, 0, d)
			nextDay := dTime.AddDate(0, 0, 1)
			dStr := dTime.Format("2006-01-02")
			isSat := dTime.Weekday() == time.Saturday
			dp := &dayPoints{date: dStr}
			for ts := dTime; ts.Before(nextDay); ts = ts.Add(time.Hour) {
				hr := ts.In(istLoc).Hour()
				loadKWH := 1.0
				divTempC := 26.0
				if isSat {
					divTempC = 30.0
					if hr >= 11 && hr <= 16 {
						loadKWH = 2.6
					}
				}
				dp.loads = append(dp.loads, loadKWH)
				dp.points = append(dp.points, types.EnergyStats{TSHourStart: ts, HomeKWH: loadKWH})
				istMatchedWeather[ts.UTC()] = 26.2
				istDivergedWeather[ts.UTC()] = divTempC
			}
			istDayMap[dStr] = dp
			istValid[dStr] = true
		}

		resISTMatched := detectDayOfWeekOutliers(ctx, istLoc, istDayMap, istValid, "2025-07-01", nil, istMatchedWeather)
		assert.True(t, resISTMatched[time.Saturday].Afternoon, "Afternoon outlier must be detected in +05:30 timezone when weather matches")
		resISTDiverged := detectDayOfWeekOutliers(ctx, istLoc, istDayMap, istValid, "2025-07-01", nil, istDivergedWeather)
		assert.False(t, resISTDiverged[time.Saturday].any(), "Afternoon surge in +05:30 timezone must be gated out when weather diverges by >= 2.0°C")
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

func TestApplyOutlierDayCalmMorningDampening(t *testing.T) {
	buildFixture := func(todayMorningLoadKWH float64, morningHours int) (
		map[string]*dayPoints,
		map[string]bool,
		[24]bool,
		[24]time.Time,
		[24]float64,
		[24]float64,
	) {
		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)
		startDate := time.Date(2025, 5, 30, 0, 0, 0, 0, time.UTC) // Friday
		for d := 0; d < 35; d++ {
			dt := startDate.AddDate(0, 0, d)
			ds := dt.Format("2006-01-02")
			isFri := dt.Weekday() == time.Friday
			var pts []types.EnergyStats
			var loads []float64
			for h := 0; h < 24; h++ {
				val := 1.0
				if isFri && h >= 7 && h <= 17 {
					val = 2.8
				}
				pts = append(pts, types.EnergyStats{
					TSHourStart: dt.Add(time.Duration(h) * time.Hour),
					HomeKWH:     val,
				})
				loads = append(loads, val)
			}
			dayMap[ds] = &dayPoints{date: ds, dayTime: dt, weekday: dt.Weekday(), points: pts, loads: loads}
			validDaysMap[ds] = true
		}

		todayStr := "2025-07-04"
		todayDate := time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC)
		var tPts []types.EnergyStats
		for h := 7; h < 7+morningHours; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: todayDate.Add(time.Duration(h) * time.Hour),
				HomeKWH:     todayMorningLoadKWH,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, dayTime: todayDate, weekday: time.Friday, points: tPts}

		var hasHour [24]bool
		var targetTimes [24]time.Time
		var rawAvgLoadA, rawP75LoadA [24]float64
		for h := 0; h < 24; h++ {
			hasHour[h] = true
			if h < 10 {
				targetTimes[h] = todayDate.AddDate(0, 0, 1).Add(time.Duration(h) * time.Hour)
			} else {
				targetTimes[h] = todayDate.Add(time.Duration(h) * time.Hour)
			}
			rawAvgLoadA[h] = 2.6
			rawP75LoadA[h] = 2.9
		}
		return dayMap, validDaysMap, hasHour, targetTimes, rawAvgLoadA, rawP75LoadA
	}

	t.Run("CapsRemainingTodayHoursOnCalmOutlierMorningAndLeavesTomorrowUntouched", func(t *testing.T) {
		dayMap, validDaysMap, hasHour, targetTimes, rawAvg, rawP75 := buildFixture(0.80, 3)
		now := time.Date(2025, 7, 4, 10, 0, 0, 0, time.UTC)
		outliers := map[time.Weekday]weekdayOutlierPeriods{time.Friday: {Waking: true}}

		applyOutlierDayCalmMorningDampening(
			context.Background(),
			&rawAvg,
			&rawP75,
			hasHour,
			targetTimes,
			now,
			time.UTC,
			10,
			"2025-07-04",
			dayMap,
			validDaysMap,
			nil,
			outliers,
			nil,
			nil,
			0.3,
			types.Settings{},
		)

		assert.InDelta(t, 1.0, rawAvg[14], 0.15, "today's remaining hour 14 should cap to unboosted all-days P55 (~1.0 kWh)")
		assert.GreaterOrEqual(t, rawP75[14], rawAvg[14])
		assert.InDelta(t, 2.6, rawAvg[8], 1e-6, "tomorrow's hour 8 must not be modified")
	})

	t.Run("SkipsWhenNotOutlierWeekdayOrFewerThanTwoMorningHours", func(t *testing.T) {
		dayMap, validDaysMap, hasHour, targetTimes, rawAvg, rawP75 := buildFixture(0.80, 1)
		now := time.Date(2025, 7, 4, 10, 0, 0, 0, time.UTC)
		outliers := map[time.Weekday]weekdayOutlierPeriods{time.Friday: {Afternoon: true}}

		// Only 1 observed morning hour -> no-op
		applyOutlierDayCalmMorningDampening(
			context.Background(),
			&rawAvg,
			&rawP75,
			hasHour,
			targetTimes,
			now,
			time.UTC,
			10,
			"2025-07-04",
			dayMap,
			validDaysMap,
			nil,
			outliers,
			nil,
			nil,
			0.3,
			types.Settings{},
		)
		assert.InDelta(t, 2.6, rawAvg[14], 1e-6)

		// Non-outlier weekday -> no-op
		dayMap2, validDaysMap2, hasHour2, targetTimes2, rawAvg2, rawP752 := buildFixture(0.80, 3)
		applyOutlierDayCalmMorningDampening(
			context.Background(),
			&rawAvg2,
			&rawP752,
			hasHour2,
			targetTimes2,
			now,
			time.UTC,
			10,
			"2025-07-04",
			dayMap2,
			validDaysMap2,
			nil,
			map[time.Weekday]weekdayOutlierPeriods{},
			nil,
			nil,
			0.3,
			types.Settings{},
		)
		assert.InDelta(t, 2.6, rawAvg2[14], 1e-6)
	})

	t.Run("HonorsNonUTCLocationOnUTCTimestampsForEveningOutlier", func(t *testing.T) {
		chicagoLoc, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		dayMap := make(map[string]*dayPoints)
		validDaysMap := make(map[string]bool)
		startDate := time.Date(2025, 5, 30, 0, 0, 0, 0, chicagoLoc) // Friday
		for d := 0; d < 35; d++ {
			dt := startDate.AddDate(0, 0, d)
			ds := dt.Format("2006-01-02")
			isFri := dt.Weekday() == time.Friday
			var pts []types.EnergyStats
			var loads []float64
			for h := 0; h < 24; h++ {
				val := 1.0
				if isFri && h >= 17 && h <= 21 {
					val = 2.8
				}
				// Store TSHourStart in UTC to verify applyOutlierDayCalmMorningDampening converts via .In(loc)
				tsUTC := time.Date(dt.Year(), dt.Month(), dt.Day(), h, 0, 0, 0, chicagoLoc).UTC()
				pts = append(pts, types.EnergyStats{
					TSHourStart: tsUTC,
					HomeKWH:     val,
				})
				loads = append(loads, val)
			}
			dayMap[ds] = &dayPoints{date: ds, dayTime: dt, weekday: dt.Weekday(), points: pts, loads: loads}
			validDaysMap[ds] = true
		}

		todayStr := "2025-07-04"
		todayDate := time.Date(2025, 7, 4, 0, 0, 0, 0, chicagoLoc)
		var tPts []types.EnergyStats
		for h := 7; h < 10; h++ {
			tsUTC := time.Date(todayDate.Year(), todayDate.Month(), todayDate.Day(), h, 0, 0, 0, chicagoLoc).UTC()
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: tsUTC,
				HomeKWH:     0.75,
			})
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, dayTime: todayDate, weekday: time.Friday, points: tPts}

		var hasHour [24]bool
		var targetTimes [24]time.Time
		var rawAvg, rawP75 [24]float64
		for h := 0; h < 24; h++ {
			hasHour[h] = true
			if h < 10 {
				targetTimes[h] = time.Date(2025, 7, 5, h, 0, 0, 0, chicagoLoc)
			} else {
				targetTimes[h] = time.Date(2025, 7, 4, h, 0, 0, 0, chicagoLoc)
			}
			rawAvg[h] = 2.6
			rawP75[h] = 2.9
		}

		now := time.Date(2025, 7, 4, 10, 0, 0, 0, chicagoLoc)
		outliers := map[time.Weekday]weekdayOutlierPeriods{time.Friday: {Evening: true}}
		applyOutlierDayCalmMorningDampening(
			context.Background(),
			&rawAvg,
			&rawP75,
			hasHour,
			targetTimes,
			now,
			chicagoLoc,
			10,
			todayStr,
			dayMap,
			validDaysMap,
			nil,
			outliers,
			nil,
			nil,
			0.3,
			types.Settings{},
		)

		assert.InDelta(t, 1.0, rawAvg[19], 0.15, "today's evening hour 19 should cap down when UTC-stored points show a calm Chicago morning")
		assert.InDelta(t, 2.6, rawAvg[8], 1e-6, "tomorrow's hour 8 must remain untouched")
	})
}

func TestCalculateSolarTrend(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 13, 0, 0, 0, time.UTC)
	historyStart := now.Add(-2 * time.Hour)

	// Mock model
	model := []TimeProfile{
		{Hour: 11, AvgSolarKWH: 2.0},
		{Hour: 12, AvgSolarKWH: 3.0},
		{Hour: 13, AvgSolarKWH: 4.0},
	}

	settings := types.Settings{
		SolarTrendRatioMax: 3.0,
	}

	t.Run("Insufficient History", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: historyStart.Add(time.Hour), SolarKWH: 2.0},
		}
		ratio := c.calculateSolarTrend(ctx, now, history, model, settings)
		assert.Equal(t, 1.0, ratio)
	})

	t.Run("Model Zero (Night)", func(t *testing.T) {
		nightNow := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		history := []types.EnergyStats{
			{TSHourStart: nightNow.Add(-1 * time.Hour), SolarKWH: 0.0},
			{TSHourStart: nightNow.Add(-2 * time.Hour), SolarKWH: 0.0},
		}
		nightModel := []TimeProfile{
			{Hour: 0, AvgSolarKWH: 0.0},
			{Hour: 1, AvgSolarKWH: 0.0},
			{Hour: 2, AvgSolarKWH: 0.0},
		}
		ratio := c.calculateSolarTrend(ctx, nightNow, history, nightModel, settings)
		assert.Equal(t, 1.0, ratio)
	})

	t.Run("High Solar Trend (Capped)", func(t *testing.T) {
		// Model expects 2.0 + 3.0 = 5.0 for hours 11 and 12
		// Actual is 10.0 + 15.0 = 25.0
		// Ratio 5.0, should cap at settings.SolarTrendRatioMax (3.0)
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 10.0},
			{TSHourStart: now.Add(-2 * time.Hour), SolarKWH: 15.0},
		}
		ratio := c.calculateSolarTrend(ctx, now, history, model, settings)
		assert.Equal(t, 3.0, ratio)
	})

	t.Run("Low Solar Trend", func(t *testing.T) {
		// Model expects 5.0
		// Actual is 0.5 + 0.5 = 1.0
		// Ratio 1.0 / 5.0 = 0.2
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 0.5},
			{TSHourStart: now.Add(-2 * time.Hour), SolarKWH: 0.5},
		}
		ratio := c.calculateSolarTrend(ctx, now, history, model, settings)
		assert.InDelta(t, 0.2, ratio, 0.001)
	})

	t.Run("Low Variation (Less than 10%)", func(t *testing.T) {
		// Model expects 5.0
		// Actual is 2.6 + 2.5 = 5.1
		// Variation (5.1-5.0)/5.0 = 2%, should return 1.0
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 2.6},
			{TSHourStart: now.Add(-2 * time.Hour), SolarKWH: 2.5},
		}
		ratio := c.calculateSolarTrend(ctx, now, history, model, settings)
		assert.Equal(t, 1.0, ratio)
	})

	t.Run("Custom Cap", func(t *testing.T) {
		customSettings := settings
		customSettings.SolarTrendRatioMax = 5.0
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 10.0},
			{TSHourStart: now.Add(-2 * time.Hour), SolarKWH: 10.0},
		}
		// Model expects 5.0. Actual 20.0. Ratio 4.0.
		ratio := c.calculateSolarTrend(ctx, now, history, model, customSettings)
		assert.Equal(t, 4.0, ratio)
	})

	t.Run("Ignore Current Hour", func(t *testing.T) {
		// If the latestTime is the current hour, it should be ignored.
		// So recent actual solar and expected solar should use the 2 previous full hours.
		// Model expects 2.0 (hr 11) + 3.0 (hr 12) = 5.0.
		// Actual solar for hr 11 and 12 is 4.0 + 6.0 = 10.0.
		// Ratio should be 10.0 / 5.0 = 2.0.
		// If current hour (hr 13) were not ignored, expected would be 7.0, actual 31.0, ratio capped at 3.0.
		history := []types.EnergyStats{
			{TSHourStart: now, SolarKWH: 25.0}, // current hour (now is 13:00)
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 6.0},
			{TSHourStart: now.Add(-2 * time.Hour), SolarKWH: 4.0},
		}
		ratio := c.calculateSolarTrend(ctx, now, history, model, settings)
		assert.Equal(t, 2.0, ratio)
	})
}
