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

	t.Run("VisitorModeShift", func(t *testing.T) {
		// History baseline has load 1.0.
		// Yesterday is high (4.0), and today is also high (4.0).
		// Visitor mode should trigger and predict high load.
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC) // Monday, 12 PM

		var history []types.EnergyStats
		startDate := now.Add(-35 * 24 * time.Hour)
		for d := 0; d < 35; d++ {
			dayTime := startDate.Add(time.Duration(d) * 24 * time.Hour)
			dateStr := dayTime.Format("2006-01-02")
			todayStr := now.Format("2006-01-02")
			yesterdayStr := now.Add(-24 * time.Hour).Format("2006-01-02")

			load := 1.0
			if dateStr == yesterdayStr {
				load = 4.0
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

		// Today's loads before 12 PM are also high (4.0)
		for h := 0; h < 12; h++ {
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC),
				HomeKWH:     4.0,
			})
		}

		settings := types.Settings{}
		model, _ := c.BuildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Model for remaining hours (e.g. 15:00) should be close to 4.0.
		if assert.Contains(t, model, 15) {
			assert.Greater(t, model[15].AvgHomeLoadKWH, 3.0)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12)
		assert.Equal(t, "down", shift)
	})

	t.Run("VisitorModeDetection", func(t *testing.T) {
		now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
		todayStr := "2025-06-16"
		yesterdayStr := "2025-06-15"

		dayMap, dayAveragesMap := createBaseData(1.0)

		// Yesterday is high (4.0)
		dayAveragesMap[yesterdayStr] = 4.0
		var yPts []types.EnergyStats
		var yLoads []float64
		for h := 0; h < 24; h++ {
			yPts = append(yPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 15, h, 0, 0, 0, time.UTC),
				HomeKWH:     4.0,
			})
			yLoads = append(yLoads, 4.0)
		}
		dayMap[yesterdayStr] = &dayPoints{date: yesterdayStr, points: yPts, loads: yLoads}

		// Today before 12 PM has high load (4.0)
		var tPts []types.EnergyStats
		var tLoads []float64
		for h := 0; h < 12; h++ {
			tPts = append(tPts, types.EnergyStats{
				TSHourStart: time.Date(2025, 6, 16, h, 0, 0, 0, time.UTC),
				HomeKWH:     4.0,
			})
			tLoads = append(tLoads, 4.0)
		}
		dayMap[todayStr] = &dayPoints{date: todayStr, points: tPts, loads: tLoads}

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12)
		assert.Equal(t, "up", shift)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 12)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 8)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 2)
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

		shift := detectLoadShift(ctx, now, loc, dayMap, dayAveragesMap, todayStr, yesterdayStr, 2)
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
