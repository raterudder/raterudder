package controller

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
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
		// Weighted 50p median of [3.0, 4.0, 4.0] is 4.0.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 4.0, model[12].AvgHomeLoadKWH, 0.001)
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

		// Adjacent hour blending dilutes the peak.
		if assert.Contains(t, model, 11) {
			assert.InDelta(t, 3.3333333333333277, model[11].AvgHomeLoadKWH, 0.001)
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

		for idx, m := range mondays {
			load := 2.0
			if idx < 4 {
				load = 1.0
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

	t.Run("ACAdjustmentConstantTemp", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     2.0,
				})
			}
		}

		// Baseline rolling temp: 24.0. Today's temp is 24.0.
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					// Today
					{TSHourStart: now.Add(-1 * time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-2 * time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-3 * time.Hour), TemperatureC: 24.0},
					// Monday -7 days
					{TSHourStart: now.Add(-7*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -14 days
					{TSHourStart: now.Add(-14*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -21 days
					{TSHourStart: now.Add(-21*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
				},
			},
		}

		settings := types.Settings{
			ACBaseTemperatureC:              22.0,
			ACUsageIncreasePercentPerDegree: 10.0,
			ACUsageMaxIncreasePercent:       50.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 2.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("ACAdjustmentCloseTempNoShift", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     2.0,
				})
			}
		}

		// Baseline rolling temp: 24.0. Today's temp is 26.8.
		// Diff is 2.8C, which is within the 3.0C deadband.
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					// Today
					{TSHourStart: now.Add(-1 * time.Hour), TemperatureC: 26.8},
					{TSHourStart: now.Add(-2 * time.Hour), TemperatureC: 26.8},
					{TSHourStart: now.Add(-3 * time.Hour), TemperatureC: 26.8},
					// Monday -7 days
					{TSHourStart: now.Add(-7*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -14 days
					{TSHourStart: now.Add(-14*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -21 days
					{TSHourStart: now.Add(-21*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
				},
			},
		}

		settings := types.Settings{
			ACBaseTemperatureC:              22.0,
			ACUsageIncreasePercentPerDegree: 10.0,
			ACUsageMaxIncreasePercent:       50.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// Inside deadband, no adjustment.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 2.0, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("ACAdjustmentHigherTemp", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     2.0,
				})
			}
		}

		// Baseline rolling temp: 24.0. Today's rolling temp: 28.0.
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					// Today
					{TSHourStart: now.Add(-1 * time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-2 * time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-3 * time.Hour), TemperatureC: 28.0},
					// Monday -7 days
					{TSHourStart: now.Add(-7*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -14 days
					{TSHourStart: now.Add(-14*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -21 days
					{TSHourStart: now.Add(-21*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
				},
			},
		}

		settings := types.Settings{
			ACBaseTemperatureC:              22.0,
			ACUsageIncreasePercentPerDegree: 10.0,
			ACUsageMaxIncreasePercent:       50.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// Effective increase: 28.0 - Max(24.0, 22.0) = 4.0 degrees.
		// Increase = 4.0 * 10% = 40%.
		// Load = 2.0 * 1.40 = 2.80.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 2.80, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("ACAdjustmentLowerTemp", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     2.0,
				})
			}
		}

		// Baseline rolling temp: 24.0. Today's rolling temp: 20.0.
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					// Today
					{TSHourStart: now.Add(-1 * time.Hour), TemperatureC: 20.0},
					{TSHourStart: now.Add(-2 * time.Hour), TemperatureC: 20.0},
					{TSHourStart: now.Add(-3 * time.Hour), TemperatureC: 20.0},
					// Monday -7 days
					{TSHourStart: now.Add(-7*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -14 days
					{TSHourStart: now.Add(-14*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
					// Monday -21 days
					{TSHourStart: now.Add(-21*24*time.Hour - 1*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 2*time.Hour), TemperatureC: 24.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 3*time.Hour), TemperatureC: 24.0},
				},
			},
		}

		settings := types.Settings{
			ACBaseTemperatureC:              22.0,
			ACUsageIncreasePercentPerDegree: 10.0,
			ACUsageMaxIncreasePercent:       50.0,
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// Baseline rolling temp: 24.0. Today's rolling temp: 20.0.
		// Diff is -4.0C (outside 1.0C deadband).
		// effDec = 24.0 - Max(20.0, 22.0) = 2.0C.
		// reduction = 2.0 * 10% = 20% reduction.
		// Expected: 2.0 * 0.8 = 1.6 KWH.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 1.6, model[12].AvgHomeLoadKWH, 0.001)
		}
	})

	t.Run("ACAdjustmentLowerTempSafetyCap", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday

		var history []types.EnergyStats
		mondays := []time.Time{
			now.Add(-7 * 24 * time.Hour),
			now.Add(-14 * 24 * time.Hour),
			now.Add(-21 * 24 * time.Hour),
		}

		for _, m := range mondays {
			for h := 0; h < 24; h++ {
				ts := time.Date(m.Year(), m.Month(), m.Day(), h, 0, 0, 0, time.UTC)
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     2.0,
				})
			}
		}

		// Baseline rolling temp: 28.0. Today's rolling temp: 18.0.
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					// Today
					{TSHourStart: now.Add(-1 * time.Hour), TemperatureC: 18.0},
					{TSHourStart: now.Add(-2 * time.Hour), TemperatureC: 18.0},
					{TSHourStart: now.Add(-3 * time.Hour), TemperatureC: 18.0},
					// Monday -7 days
					{TSHourStart: now.Add(-7*24*time.Hour - 1*time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 2*time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-7*24*time.Hour - 3*time.Hour), TemperatureC: 28.0},
					// Monday -14 days
					{TSHourStart: now.Add(-14*24*time.Hour - 1*time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 2*time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-14*24*time.Hour - 3*time.Hour), TemperatureC: 28.0},
					// Monday -21 days
					{TSHourStart: now.Add(-21*24*time.Hour - 1*time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 2*time.Hour), TemperatureC: 28.0},
					{TSHourStart: now.Add(-21*24*time.Hour - 3*time.Hour), TemperatureC: 28.0},
				},
			},
		}

		settings := types.Settings{
			ACBaseTemperatureC:              22.0,
			ACUsageIncreasePercentPerDegree: 10.0,
			ACUsageMaxIncreasePercent:       30.0, // cap at 30% reduction
		}

		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, weather, settings)

		// effDec = 28.0 - Max(18.0, 22.0) = 6.0C.
		// uncapped reduction = 60%.
		// capped reduction = 30% (since settings.ACUsageMaxIncreasePercent is 30.0).
		// Expected: 2.0 * 0.7 = 1.4 KWH.
		if assert.Contains(t, model, 12) {
			assert.InDelta(t, 1.4, model[12].AvgHomeLoadKWH, 0.001)
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

		// Adjacent hour blending and 50p median results in 1.0.
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
		assert.InDelta(t, 1.9743589743589745, model[h1.Hour()].AvgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].AvgSolarKWH, 0.001)
	})

	t.Run("Basic Average All Low", func(t *testing.T) {
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 0.0, SolarKWH: 0.0},
		}

		testNow := time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC)
		model, _ := c.BuildHourlyEnergyModel(ctx, testNow, history, nil, types.Settings{IgnoreHourUsageOverMultiple: 0.0})
		assert.InDelta(t, 0.1, model[h1.Hour()].AvgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].AvgSolarKWH, 0.001)
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
		assert.InDelta(t, 2.5512750949538803, model[h1.Hour()].AvgHomeLoadKWH, 0.001)

		// Case 2: Multiple outliers (not removed)
		historyMulti := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0}, // Outlier 1
			{TSHourStart: h3, HomeKWH: 12.0, SolarKWH: 0.0}, // Outlier 2
		}
		modelMulti, _ := c.BuildHourlyEnergyModel(ctx, testNow, historyMulti, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		assert.InDelta(t, 10.307107976125883, modelMulti[h1.Hour()].AvgHomeLoadKWH, 0.001)

		// Case 3: Not enough points (min 3)
		historyFew := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0},
		}
		modelFew, _ := c.BuildHourlyEnergyModel(ctx, testNow, historyFew, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		assert.InDelta(t, 5.384615384615385, modelFew[h1.Hour()].AvgHomeLoadKWH, 0.001)
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
}
