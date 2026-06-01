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

	t.Run("Basic Average with Filter", func(t *testing.T) {
		// Use a fixed nighttime hour (2 AM) so bell curve smoothing doesn't affect results
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		h2 := h1.Add(-24 * time.Hour)
		h3 := h1.Add(-48 * time.Hour)

		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 3.0, SolarKWH: 0.0},
			{TSHourStart: h3, HomeKWH: 0.05, SolarKWH: 0.0}, // Should be filtered (<= 0.1)
		}

		// Avg Load: (1+3)/2 = 2.0. Solar: 0 (no solar at night).
		// The 0.05 values are ignored.
		model := c.buildHourlyEnergyModel(ctx, time.Now().UTC(), history, nil, types.Settings{IgnoreHourUsageOverMultiple: 0.0})
		assert.InDelta(t, 2.0, model[h1.Hour()].avgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].avgSolarKWH, 0.001)
	})

	t.Run("Basic Average All Low", func(t *testing.T) {
		// Use a fixed nighttime hour (2 AM) so bell curve smoothing doesn't affect results
		h1 := time.Date(2025, 6, 15, 2, 0, 0, 0, time.UTC)
		history := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 0.05, SolarKWH: 0.05},
		}

		model := c.buildHourlyEnergyModel(ctx, time.Now().UTC(), history, nil, types.Settings{IgnoreHourUsageOverMultiple: 0.0})
		// Should be 0.0 because all filtered
		assert.InDelta(t, 0.0, model[h1.Hour()].avgHomeLoadKWH, 0.001)
		assert.InDelta(t, 0.0, model[h1.Hour()].avgSolarKWH, 0.001)
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
		model := c.buildHourlyEnergyModel(ctx, time.Now().UTC(), history, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		// (1.0 + 1.2) / 2 = 1.1
		assert.InDelta(t, 1.1, model[h1.Hour()].avgHomeLoadKWH, 0.001)

		// Case 2: Multiple outliers (not removed)
		historyMulti := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0}, // Outlier 1
			{TSHourStart: h3, HomeKWH: 12.0, SolarKWH: 0.0}, // Outlier 2
		}
		modelMulti := c.buildHourlyEnergyModel(ctx, time.Now().UTC(), historyMulti, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		// (1.0 + 10.0 + 12.0) / 3 = 7.666...
		assert.InDelta(t, 7.666, modelMulti[h1.Hour()].avgHomeLoadKWH, 0.001)

		// Case 3: Not enough points (min 3)
		historyFew := []types.EnergyStats{
			{TSHourStart: h1, HomeKWH: 1.0, SolarKWH: 0.0},
			{TSHourStart: h2, HomeKWH: 10.0, SolarKWH: 0.0},
		}
		modelFew := c.buildHourlyEnergyModel(ctx, time.Now().UTC(), historyFew, nil, types.Settings{IgnoreHourUsageOverMultiple: 3.0})
		// (1.0 + 10.0) / 2 = 5.5
		assert.InDelta(t, 5.5, modelFew[h1.Hour()].avgHomeLoadKWH, 0.001)
	})

	t.Run("Smoothes Solar With Bell Curve", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-24 * time.Hour)

		history := []types.EnergyStats{}
		// daylight 6-20 (15 hours).
		// mu = 6 + 15/2.0 = 13.5
		mu := 13.5
		sigma := 5.0 // Updated to match controller refinements
		peak := 5.0

		for h := 0; h < 24; h++ {
			solar := 0.0
			// hours 6-20
			if h >= 6 && h <= 20 {
				solar = peak * math.Exp(-math.Pow(float64(h)-mu, 2)/(2*math.Pow(sigma, 2)))
			}

			// Simulate curtailment:
			// At hour 11, battery gets full (99%), and we stop generating solar (curtailment)
			// until hour 15 when load picks up.
			// Peak is around 13-14, so we are chopping off the top.
			batterySOC := 50.0
			gridExport := 0.0
			if h >= 11 && h <= 15 {
				batterySOC = 99.0
				solar = 0.5 // Curtailed
			} else {
				// Normal operation
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

		// Run 1: Export Disabled (Aggressive Smoothing)
		settings := types.Settings{
			GridExportSolar:          false,
			SolarBellCurveMultiplier: 1.0,
		}
		model := c.buildHourlyEnergyModel(ctx, now, history, nil, settings)

		// Controller will detect 6-20. mu=13.5, sigma=3.75.
		// It will see valid data at 6,7,8,9,10, 16,17,18,19,20.
		// At hour 10: Actual=3.23 (5.0 * exp(-(10-13.5)^2 / (2*3.75^2)))
		// Factor=0.647. Estimated Peak = 3.23 / 0.647 = 5.0.
		// So it should reconstruct exactly 5.0.

		// Check hour 13 (near peak). Actual data is 0.5 (curtailed).
		// Predicted peak ~13.5 (between 13 and 14).
		// Predicted at 13: 5.0 * exp(-(13-13.5)^2/...) = 5.0 * 0.99 = ~4.95.

		assert.Greater(t, model[13].avgSolarKWH, 4.8,
			"Should reconstruct bell curve peak to ~5.0 using off-peak data")
		assert.Less(t, model[13].avgSolarKWH, 5.2, "Should be around 5.0")

		// Run 2: No valid data (Everything curtailed or low)
		// Set all solar to 0.05 (filtered)
		for i := range history {
			history[i].SolarKWH = 0.05
		}
		modelNoData := c.buildHourlyEnergyModel(ctx, now, history, nil, settings)
		// Should be 0.0 (filtered) and no smoothing (no valid max)
		assert.Equal(t, 0.0, modelNoData[13].avgSolarKWH)
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
		model := c.buildHourlyEnergyModel(ctx, now, history, nil, settings)

		// No solar detected, should not smooth
		for h := 0; h < 24; h++ {
			assert.InDelta(t, 0.0, model[h].avgSolarKWH, 0.001, "Hour %d should have no solar", h)
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
				solar = 3.0 // All hours have same solar (flat)
			}

			history = append(history, types.EnergyStats{
				TSHourStart:   start.Add(time.Duration(h) * time.Hour),
				SolarKWH:      solar,
				HomeKWH:       1.0,
				MaxBatterySOC: 99.0, // Always full -> curtailed
				GridExportKWH: 0.0,  // No export
			})
		}

		settings := types.Settings{GridExportSolar: false, SolarBellCurveMultiplier: 1.0}
		model := c.buildHourlyEnergyModel(ctx, now, history, nil, settings)

		// All data is curtailed (SOC=99%, no export), so the first pass finds nothing.
		// The fallback pass should still find valid data and smooth.
		// Peak hour should be at least 3.0
		assert.GreaterOrEqual(t, model[13].avgSolarKWH, 3.0,
			"Should use fallback data and maintain at least the raw average")
	})

	t.Run("Noisy Edge Data", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-24 * time.Hour)

		history := []types.EnergyStats{}
		// Daylight hours 8 to 18 (duration = 11)
		// mu = 8 + 11/2 = 13.5
		// sigma = 11 / 3 = 3.66...
		mu := 13.5
		sigma := 11.0 / 3.0

		for h := 0; h < 24; h++ {
			solar := 0.0
			hourFactor := math.Exp(-math.Pow(float64(h)-mu, 2) / (2 * math.Pow(sigma, 2)))

			if h >= 8 && h <= 18 {
				// Base peak is 2.0
				solar = 2.0 * hourFactor
			}

			// Add a "noisy" reading at the edge (hour 9)
			if h == 9 {
				solar = 10.0 // Noise!
			}

			history = append(history, types.EnergyStats{
				TSHourStart:   start.Add(time.Duration(h) * time.Hour),
				SolarKWH:      solar,
				HomeKWH:       1.0,
				MaxBatterySOC: 50.0,
				GridExportKWH: 2.0, // Valid data
			})
		}

		settings := types.Settings{GridExportSolar: true, SolarBellCurveMultiplier: 1.0}
		model := c.buildHourlyEnergyModel(ctx, now, history, nil, settings)

		// maxOriginalPeak = 10.0 (at hour 9)
		// factor at hour 9 = 0.47 (calculated above)
		// estimatedPeak = 10.0 / 0.47 = 21.2

		// Let's just verify the peak isn't totally insane.
		// Estimated peak should be around 21.2.
		assert.Less(t, model[13].avgSolarKWH, 25.0, "Peak should not explode from noisy edge data")
		assert.Greater(t, model[13].avgSolarKWH, 5.0, "Should still boost above the baseline peak of 2.0")
	})

	t.Run("Solar Peak Estimation With Outliers", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Add(-7 * 24 * time.Hour)

		history := []types.EnergyStats{}
		// daylight 6-20 (15 hours).
		// mu = 13.5
		mu := 13.5
		sigma := 5.0

		// 1 day of "Outlier" high solar (Peak 10.0)
		// 5 days of "Normal" solar (Peak 5.0)
		for day := 0; day < 6; day++ {
			peak := 5.0
			if day == 0 {
				peak = 10.0 // Outlier
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
					MaxBatterySOC: 50.0, // Not full, so valid
					GridExportKWH: 2.0,  // Exporting, so valid
				})
			}
		}

		settings := types.Settings{
			GridExportSolar:          true,
			SolarBellCurveMultiplier: 1.0,
		}
		model := c.buildHourlyEnergyModel(ctx, now, history, nil, settings)

		// The new logic should pick the hour with the most valid data points.
		// All hours have 6 valid data points (1 outlier, 5 normal).
		// Tie-breaker: Largest average solar.
		// Normal days: ~5.0 peak. Outlier: ~10.0 peak.
		// Average for peak hour: (5*5.0 + 10.0) / 6 = 35/6 = 5.83.
		// Estimated Peak should be based on this average.
		// 5.83 is much closer to 5.0 than 10.0.
		// If we took the max (old logic), it would be 10.0.

		assert.Less(t, model[13].avgSolarKWH, 7.0, "Should be closer to 5.0 than 10.0")
		assert.Greater(t, model[13].avgSolarKWH, 5.0, "Should capture the average including outlier")
	})
}

func TestCalculateSolarTrend(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 13, 0, 0, 0, time.UTC)
	historyStart := now.Add(-2 * time.Hour)

	// Mock model
	model := map[int]timeProfile{
		11: {avgSolarKWH: 2.0},
		12: {avgSolarKWH: 3.0},
		13: {avgSolarKWH: 4.0},
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
		nightModel := map[int]timeProfile{
			0: {avgSolarKWH: 0.0},
			1: {avgSolarKWH: 0.0},
			2: {avgSolarKWH: 0.0},
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

func TestSimulateState(t *testing.T) {
	c := NewController()
	ctx := context.Background()

	t.Run("BasicSimulation", func(t *testing.T) {
		// Scenario:
		// Hours 0-2: Night. Solar 0, Load 1. Battery drains -1/hr.
		// Hours 3-5: Day. Solar 2, Load 1. Battery charges +1/hr.
		now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

		// Since we can't inject the model directly into SimulateState (it builds it internally),
		// we must populate History such that buildHourlyEnergyModel produces the desired model.
		startOfDay := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := startOfDay.Add(time.Duration(-24*i) * time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				if h >= 3 && h <= 5 {
					solar = 2.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
					SolarKWH:    solar,
					HomeKWH:     1.0,
				})
			}
		}

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    10.0,
			BatterySOC:            50.0, // 5.0 kWh
			BatteryKW:             0,
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		settings := types.Settings{
			MinBatterySOC:            0.0,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 0,
			GridChargeBatteries:      false,
			GridExportSolar:          true,
		}

		// Use dummy prices
		currentPrice := types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)}
		futurePrices := []types.Price{}

		simData := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

		// Verify first few hours
		// Hour 0: Start 5.0. Net -1. End 4.0.
		assert.Equal(t, 0, simData[0].Hour)
		assert.InDelta(t, 4.0, simData[0].BatteryKWH, 0.01, "Hour 0: Should drain to 4.0")

		// Hour 1: Start 4.0. Net -1. End 3.0.
		assert.Equal(t, 1, simData[1].Hour)
		assert.InDelta(t, 3.0, simData[1].BatteryKWH, 0.01, "Hour 1: Should drain to 3.0")

		// Hour 2: Start 3.0. Net -1. End 2.0.
		assert.Equal(t, 2, simData[2].Hour)
		assert.InDelta(t, 2.0, simData[2].BatteryKWH, 0.01, "Hour 2: Should drain to 2.0")

		// Hour 3: Start 2.0. Net +1. End 3.0.
		assert.Equal(t, 3, simData[3].Hour)
		assert.InDelta(t, 3.0, simData[3].BatteryKWH, 0.01, "Hour 3: Should charge to 3.0")

		// Hour 4: Start 3.0. Net +1. End 4.0.
		assert.Equal(t, 4, simData[4].Hour)
		assert.InDelta(t, 4.0, simData[4].BatteryKWH, 0.01, "Hour 4: Should charge to 4.0")

		// Hour 5: Start 4.0. Net +1. End 5.0.
		assert.Equal(t, 5, simData[5].Hour)
		assert.InDelta(t, 5.0, simData[5].BatteryKWH, 0.01, "Hour 5: Should charge to 5.0")
	})

	t.Run("SolarTrendResetNextDay", func(t *testing.T) {
		// Setup:
		// 1. Current Time: 2025-06-15 10:00:00 UTC (Day 1)
		// 2. High historical solar average (model) for 8am-9am today.
		// 3. Low actual solar for 8am-9am today (cloudy).
		//    -> This should trigger a low solar trend (e.g. 0.5).
		// 4. Run simulation for 24 hours (10am Day 1 -> 9am Day 2).
		// 5. Verify that Day 1 hours use the low trend.
		// 6. Verify that Day 2 hours reset to trend 1.0.

		now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
		startOfDay := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

		// Model: High solar
		// We can't easily mock the internal buildHourlyEnergyModel without refactoring,
		// so we'll just populate history such that the model is built with high averages.
		// To get a model with high average, we need "older" history (days ago) with high solar.
		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := startOfDay.Add(time.Duration(-24*i) * time.Hour)
			// Add high solar for 8am and 9am
			history = append(history, types.EnergyStats{
				TSHourStart: pastDay.Add(8 * time.Hour),
				SolarKWH:    10.0,
				HomeKWH:     0.5,
			})
			history = append(history, types.EnergyStats{
				TSHourStart: pastDay.Add(9 * time.Hour),
				SolarKWH:    10.0,
				HomeKWH:     0.5,
			})
		}

		// Recent history (today): Low solar
		history = append(history, types.EnergyStats{
			TSHourStart: startOfDay.Add(8 * time.Hour),
			SolarKWH:    5.0, // 50% of 10.0
			HomeKWH:     0.5,
		})
		history = append(history, types.EnergyStats{
			TSHourStart: startOfDay.Add(9 * time.Hour),
			SolarKWH:    5.0, // 50% of 10.0
			HomeKWH:     0.5,
		})

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    13.5,
			BatterySOC:            50.0,
			BatteryKW:             0,
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}
		currentPrice := types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)}
		futurePrices := []types.Price{} // Flat price
		settings := types.Settings{
			MinBatterySOC:            20.0,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 0, // Disable bell curve to keep math simple
		}

		// Let's ensure we populate the model for the hours we want to test.
		// We want to test Day 1 afternoon (e.g. 2pm) and Day 2 morning (e.g. 8am).
		// Add history for 2pm
		for i := 1; i <= 3; i++ {
			pastDay := startOfDay.Add(time.Duration(-24*i) * time.Hour)
			history = append(history, types.EnergyStats{
				TSHourStart: pastDay.Add(14 * time.Hour), // 2pm
				SolarKWH:    10.0,
				HomeKWH:     0.5,
			})
		}

		// Run simulation
		simData := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

		// Day 1: 2pm (Hour index 4, since starting at 10am: 10, 11, 12, 13, 14)
		// simData[0] is 10am. simData[4] is 2pm.
		day1Hour := simData[4]
		assert.Equal(t, 14, day1Hour.Hour)
		// Model average is ~7.5 (due to implicit 0 for today? unconfirmed but consistent). Trend is ~0.57.
		// Predicted ~ 4.28.
		// Main check: It should be significantly lower than the raw average (10.0) or even the suppressed average (8.75).
		// Verify SolarTrend property is significantly less than 1.0 (approx 0.57)
		assert.Less(t, day1Hour.TodaySolarTrend, 0.7, "Day 1 SolarTrend should be low (e.g. 0.57)")
		assert.Greater(t, day1Hour.TodaySolarTrend, 0.4, "Day 1 SolarTrend should be somewhat reasonable")

		// Day 2: 8am (Hour index 22: 10am + 22h = 8am next day)
		day2Hour := simData[22]
		assert.Equal(t, 8, day2Hour.Hour)

		// Model average calculation:
		// 3 days of 10.0 + 1 day of 5.0 = 35.0 / 4 = 8.75 KWH.
		// Since we reset trend to 1.0, PredictedSolarKWH should match the model average exactly (8.75).
		assert.InDelta(t, 8.75, day2Hour.PredictedSolarKWH, 0.01, "Day 2 PredictedSolarKWH should match model average (8.75)")

		// Verify SolarTrend property is explicitly 1.0
		assert.Equal(t, 1.0, day2Hour.TodaySolarTrend, "Day 2 SolarTrend should be explicitly 1.0")
	})

	t.Run("SolarTrendOnlyToday", func(t *testing.T) {
		// Provide 48 hours of future prices to allow a longer simulation
		now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
		futurePrices := make([]types.Price, 48)
		for i := 0; i < 48; i++ {
			futurePrices[i] = types.Price{
				DollarsPerKWH: 0.10,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			}
		}

		// Setup history to have a trend for today (e.g. 0.5)
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 5.0},
			{TSHourStart: now.Add(-2 * time.Hour), SolarKWH: 5.0},
		}
		// Note: SimulateState builds its own model, so we need to provide history
		// that produces those averages across all hours.
		history = append(history, types.EnergyStats{TSHourStart: now.Add(-25 * time.Hour), SolarKWH: 15.0})
		history = append(history, types.EnergyStats{TSHourStart: now.Add(-26 * time.Hour), SolarKWH: 15.0})

		settings := types.Settings{SolarTrendRatioMax: 3.0}
		currentStatus := types.SystemStatus{BatteryCapacityKWH: 10, BatterySOC: 50, Timestamp: now}
		currentPrice := futurePrices[0]

		simData := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

		for _, hour := range simData {
			if hour.TS.Year() == now.Year() && hour.TS.YearDay() == now.YearDay() {
				// Today: trend could be anything (depending on above setup), but we check it's returned
				// We don't strictly assert the value here as it's complex to setup the exact trend via history,
				// but we know it's applied to Today.
			} else {
				// Not Today: MUST BE 1.0
				assert.Equal(t, 1.0, hour.TodaySolarTrend, "Trend must be 1.0 for %v", hour.TS)
			}
		}
	})

	t.Run("SolarOppCostNetMetering", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH: 10.0,
			BatterySOC:         50.0,
			Timestamp:          now,
		}

		currentPrice := types.Price{
			DollarsPerKWH:        0.10,
			GridUseDollarsPerKWH: 0.05,
			TSStart:              now,
			TSEnd:                now.Add(time.Hour),
		}

		// Add a peak price later to test 1:1 net metering valuation
		futurePrice := types.Price{
			DollarsPerKWH:        0.40,
			GridUseDollarsPerKWH: 0.10,
			TSStart:              now.Add(2 * time.Hour),
			TSEnd:                now.Add(3 * time.Hour),
		}

		tests := []struct {
			name            string
			gridExportSolar bool
			netMetering     bool
			nmValueSetting  string
			expectedOppCost float64
		}{
			{
				name:            "Export Disabled",
				gridExportSolar: false,
				netMetering:     false,
				expectedOppCost: 0.0,
			},
			{
				name:            "Export Enabled No Net Metering",
				gridExportSolar: true,
				netMetering:     false,
				expectedOppCost: 0.10, // Just current DollarsPerKWH
			},
			{
				name:            "Export Enabled With Net Metering Default (Lowest)",
				gridExportSolar: true,
				netMetering:     true,
				nmValueSetting:  "",
				expectedOppCost: 0.15, // Current price (0.10 + 0.05) is lowest
			},
			{
				name:            "Export Enabled With Net Metering (Lowest)",
				gridExportSolar: true,
				netMetering:     true,
				nmValueSetting:  "lowest",
				expectedOppCost: 0.15,
			},
			{
				name:            "Export Enabled With Net Metering (Highest)",
				gridExportSolar: true,
				netMetering:     true,
				nmValueSetting:  "highest",
				expectedOppCost: 0.50, // Peak DollarsPerKWH (0.40) + GridUse (0.10)
			},
			{
				name:            "Export Enabled With Net Metering (None)",
				gridExportSolar: true,
				netMetering:     true,
				nmValueSetting:  "none",
				expectedOppCost: 0.0,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				settings := types.Settings{
					GridExportSolar: tt.gridExportSolar,
					UtilityRateOptions: types.UtilityRateOptions{
						NetMeteringCredits: tt.netMetering,
					},
					SolarNetMeteringCreditsValue: tt.nmValueSetting,
				}

				simData := c.SimulateState(ctx, now, currentStatus, currentPrice, []types.Price{futurePrice}, nil, nil, settings)
				assert.NotEmpty(t, simData)
				assert.InDelta(t, tt.expectedOppCost, simData[0].SolarOppDollarsPerKWH, 0.001)
			})
		}
	})

	t.Run("SolarOppCost", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH: 10.0,
			BatterySOC:         50.0,
			Timestamp:          now,
		}

		tests := []struct {
			name                     string
			separateGenerationCredit bool
			generationCreditDollars  float64
			baseSupplyDollars        float64
			gridExportSolar          bool
			netMetering              bool
			nmValue                  string
			expectedOppCost          float64
			description              string
		}{
			{
				name:                     "SeparateGenerationCredit uses GenerationCreditDollarsPerKWH",
				separateGenerationCredit: true,
				generationCreditDollars:  0.03,
				baseSupplyDollars:        0.10,
				gridExportSolar:          true,
				netMetering:              false,
				expectedOppCost:          0.03,
				description:              "When SeparateGenerationCredit=true the generation credit rate is used, not the supply rate",
			},
			{
				name:                     "SeparateGenerationCredit=false uses DollarsPerKWH",
				separateGenerationCredit: false,
				generationCreditDollars:  0.03, // set but ignored
				baseSupplyDollars:        0.10,
				gridExportSolar:          true,
				netMetering:              false,
				expectedOppCost:          0.10, // base supply used
				description:              "When SeparateGenerationCredit=false, GenerationCreditDollarsPerKWH is ignored",
			},
			{
				name:                     "NetMetering overrides SeparateGenerationCredit",
				separateGenerationCredit: true,
				generationCreditDollars:  0.03,
				baseSupplyDollars:        0.10,
				gridExportSolar:          true,
				netMetering:              true,
				nmValue:                  "none", // explicit none to get 0
				expectedOppCost:          0.0,
				description:              "Net metering path takes precedence over SeparateGenerationCredit",
			},
			{
				name:                     "Export disabled yields 0 even with SeparateGenerationCredit",
				separateGenerationCredit: true,
				generationCreditDollars:  0.05,
				baseSupplyDollars:        0.10,
				gridExportSolar:          false,
				netMetering:              false,
				expectedOppCost:          0.0,
				description:              "Export disabled always gives 0 opportunity cost",
			},
			{
				name:                     "Zero GenerationCreditDollarsPerKWH with SeparateGenerationCredit",
				separateGenerationCredit: true,
				generationCreditDollars:  0.0,
				baseSupplyDollars:        0.10,
				gridExportSolar:          true,
				netMetering:              false,
				expectedOppCost:          0.0,
				description:              "Zero generation credit is valid and not replaced by supply rate",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				price := types.Price{
					DollarsPerKWH:                 tt.baseSupplyDollars,
					GenerationCreditDollarsPerKWH: tt.generationCreditDollars,
					SeparateGenerationCredit:      tt.separateGenerationCredit,
					TSStart:                       now,
					TSEnd:                         now.Add(time.Hour),
				}
				futurePrices := []types.Price{
					{
						DollarsPerKWH:                 tt.baseSupplyDollars,
						GenerationCreditDollarsPerKWH: tt.generationCreditDollars,
						SeparateGenerationCredit:      tt.separateGenerationCredit,
						TSStart:                       now.Add(time.Hour),
						TSEnd:                         now.Add(2 * time.Hour),
					},
				}
				settings := types.Settings{
					GridExportSolar: tt.gridExportSolar,
					UtilityRateOptions: types.UtilityRateOptions{
						NetMeteringCredits: tt.netMetering,
					},
					SolarNetMeteringCreditsValue: tt.nmValue,
				}

				simData := c.SimulateState(ctx, now, currentStatus, price, futurePrices, nil, nil, settings)
				assert.NotEmpty(t, simData)
				// there should be 2 because one for the current hour and one for the next hour
				if assert.Len(t, simData, 2) {
					assert.InDelta(t, tt.expectedOppCost, simData[0].SolarOppDollarsPerKWH, 0.0001, tt.description)
					assert.InDelta(t, tt.expectedOppCost, simData[1].SolarOppDollarsPerKWH, 0.0001, tt.description)
				}
			})
		}
	})

	t.Run("SolarOppCostGenerationAdjustment", func(t *testing.T) {
		c := NewController()
		ctx := context.Background()
		now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH: 10.0,
			BatterySOC:         50.0,
			Timestamp:          now,
		}

		tests := []struct {
			name            string
			baseSupply      float64
			gridUse         float64
			adjustment      float64
			gridExportSolar bool
			netMetering     bool
			nmValue         string
			expectedOppCost float64
		}{
			{
				name:            "Standard Export with Adjustment",
				baseSupply:      0.10,
				adjustment:      -0.0402,
				gridExportSolar: true,
				netMetering:     false,
				expectedOppCost: 0.0598,
			},
			{
				name:            "Net Metering Lowest with Adjustment",
				baseSupply:      0.10,
				gridUse:         0.05,
				adjustment:      -0.0402,
				gridExportSolar: true,
				netMetering:     true,
				nmValue:         "lowest",
				expectedOppCost: 0.1098, // 0.10 + 0.05 - 0.0402
			},
			{
				name:            "Net Metering None with Adjustment",
				baseSupply:      0.10,
				gridUse:         0.05,
				adjustment:      -0.0402,
				gridExportSolar: true,
				netMetering:     true,
				nmValue:         "none",
				expectedOppCost: 0.0, // Should stay 0.0 and ignore adjustment
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				price := types.Price{
					DollarsPerKWH:                     tt.baseSupply,
					GridUseDollarsPerKWH:              tt.gridUse,
					GenerationAdjustmentDollarsPerKWH: tt.adjustment,
					TSStart:                           now,
					TSEnd:                             now.Add(time.Hour),
				}
				settings := types.Settings{
					GridExportSolar: tt.gridExportSolar,
					UtilityRateOptions: types.UtilityRateOptions{
						NetMeteringCredits: tt.netMetering,
					},
					SolarNetMeteringCreditsValue: tt.nmValue,
				}

				simData := c.SimulateState(ctx, now, currentStatus, price, nil, nil, nil, settings)
				assert.NotEmpty(t, simData)
				assert.InDelta(t, tt.expectedOppCost, simData[0].SolarOppDollarsPerKWH, 0.0001)
			})
		}
	})

	t.Run("HitCapacityAt", func(t *testing.T) {
		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		// Start at 5.0 kWh (SOC 50). Net charge is 10.0 kWh/hr.
		// We should hit capacity at 50% into the hour (12:30).
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            50.0,
			Timestamp:             now,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour)
			history = append(history, types.EnergyStats{
				TSHourStart: pastDay,
				SolarKWH:    11.0,
				HomeKWH:     1.0,
			})
		}

		settings := types.Settings{
			GridExportSolar: true,
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		if assert.NotEmpty(t, simData) {
			assert.False(t, simData[0].HitCapacityAt.IsZero())
			expected := now.Add(28*time.Minute + 48*time.Second)
			assert.Equal(t, expected, simData[0].HitCapacityAt)
		}
	})

	t.Run("HitSolarCapacityAt", func(t *testing.T) {
		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		// Start at 5.0 kWh (SOC 50). Net charge is 10.0 kWh/hr.
		// SolarHeadroomSOC is 10%, so target is 90% (9.0 kWh).
		// Need +4.0 kWh. 4.0 / 10.0 = 0.4 hrs = 24 minutes.
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            50.0,
			Timestamp:             now,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour)
			history = append(history, types.EnergyStats{
				TSHourStart: pastDay,
				SolarKWH:    11.0,
				HomeKWH:     1.0,
			})
		}

		settings := types.Settings{
			GridExportSolar:                    false,
			SolarFullyChargeHeadroomBatterySOC: 10.0,
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		if assert.NotEmpty(t, simData) {
			assert.False(t, simData[0].HitSolarCapacityAt.IsZero())
			expected := now.Add(24 * time.Minute)
			assert.Equal(t, expected, simData[0].HitSolarCapacityAt)
		}
	})

	t.Run("HitStandbyCapacityAt", func(t *testing.T) {
		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		// Start at 5.0 kWh (SOC 50).
		// Hour 12: Load is 2.0 kWh, Solar is 0.0 kWh.
		// Hour 13: Load is 1.0 kWh, Solar is 11.0 kWh.
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            50.0,
			Timestamp:             now,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), 12, 0, 0, 0, time.UTC),
				SolarKWH:    0.0,
				HomeKWH:     2.0,
			})
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), 13, 0, 0, 0, time.UTC),
				SolarKWH:    11.0,
				HomeKWH:     1.0,
			})
		}

		settings := types.Settings{
			GridExportSolar: true,
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		if assert.NotEmpty(t, simData) && assert.Len(t, simData, 24) {
			// Normal battery discharges during hour 12: 5.0 - 2.0 = 3.0 kWh.
			// Charges during hour 13 starting from 3.0 kWh.
			// Target is 9.8 kWh. Net charge is 10.0 kW.
			// Time to hit capacity: 1 hour + (9.8 - 3.0)/10.0 hr = 1 hr + 0.68 hr = 1 hr 40 min 48 sec.
			assert.False(t, simData[1].HitCapacityAt.IsZero())
			expectedNormal := now.Add(1*time.Hour + 40*time.Minute + 48*time.Second)
			assert.Equal(t, expectedNormal, simData[1].HitCapacityAt)

			// Standby battery does not discharge during hour 12, so it stays at 5.0 kWh.
			// Charges during hour 13 starting from 5.0 kWh.
			// Target is 9.8 kWh. Net charge is 10.0 kW.
			// Time to hit standby capacity: 1 hour + (9.8 - 5.0)/10.0 hr = 1 hr + 0.48 hr = 1 hr 28 min 48 sec.
			assert.False(t, simData[1].HitStandbyCapacityAt.IsZero())
			expectedStandby := now.Add(1*time.Hour + 28*time.Minute + 48*time.Second)
			assert.Equal(t, expectedStandby, simData[1].HitStandbyCapacityAt)
		}
	})

	t.Run("FirstHourProportionate", func(t *testing.T) {
		// Scenario:
		// current time: 12:30 (30 minutes into the hour).
		// Load is 2.0 kWh/hr.
		// Since only 30 minutes remain, we should only apply 1.0 kWh of drain.
		now := time.Date(2025, 6, 15, 12, 30, 0, 0, time.UTC)
		capacityKWH := 10.0

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            50.0, // 5.0 kWh
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for h := 0; h < 24; h++ {
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
				// Set high load for all hours to be safe
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), h, 0, 0, 0, time.UTC),
					SolarKWH:    0.0,
					HomeKWH:     2.0,
				})
			}
		}

		settings := types.Settings{
			MinBatterySOC: 0.0,
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		// Verification:
		// Hour 12 (first hour): Start 5.0. 30 mins (0.5 hrs) left.
		// Drain = 2.0 * 0.5 = 1.0.
		// End BatteryKWH = 5.0 - 1.0 = 4.0.
		assert.Equal(t, 12, simData[0].Hour)
		assert.InDelta(t, 4.0, simData[0].BatteryKWH, 0.001, "First hour should only apply remaining 30 mins of load")

		// Hour 13 (second hour): Start 4.0. Full hour (1.0 ratio).
		// Drain = 2.0 * 1.0 = 2.0.
		// End BatteryKWH = 4.0 - 2.0 = 2.0.
		assert.Equal(t, 13, simData[1].Hour)
		assert.InDelta(t, 2.0, simData[1].BatteryKWH, 0.001, "Second hour should apply full 1 hour of load")
	})

	t.Run("LimitHoursBasedOnPricing", func(t *testing.T) {
		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

		// Provide only 12 hours of future prices
		futurePrices := make([]types.Price, 12)
		for i := 0; i < 12; i++ {
			futurePrices[i] = types.Price{
				DollarsPerKWH: 0.10,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			}
		}

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH: 10.0,
			BatterySOC:         50.0,
			Timestamp:          now,
		}

		// Current price covers the first hour
		currentPrice := futurePrices[0]

		simData := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, nil, nil, types.Settings{})

		// Should only simulate 12 hours, not 24
		assert.Len(t, simData, 12)
	})

	t.Run("PostCapacityDeficitAccumulation", func(t *testing.T) {
		// Scenario:
		// 12:00 (Start): 50% SOC (5.0 kWh). Capacity 10.0 kWh.
		// Hour 12: Solar 11.0 kWh, Load 1.0 kWh -> Net charge +10.0 kWh/hr. Hits capacity at 12:30.
		// Hour 13: Solar 0.0 kWh, Load 10.0 kWh -> Net drain -10.0 kWh/hr.
		// Since we hit capacity in hour 12, the deficit for hour 12 is reset to 0.
		// Then in hour 13, the battery drains from 10.0 kWh down to min SOC 20% (2.0 kWh).
		// Drain is 10.0 kWh, so it goes below 2.0 kWh, hitting a deficit.
		// We expect the simulator to accumulate this post-capacity deficit.
		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            50.0,
			Timestamp:             now,
			MaxBatteryChargeKW:    10.0,
			MaxBatteryDischargeKW: 10.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
			// Hour 12: Solar 11.0, Load 1.0
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), 12, 0, 0, 0, time.UTC),
				SolarKWH:    11.0,
				HomeKWH:     1.0,
			})
			// Hour 13: Solar 0.0, Load 10.0
			history = append(history, types.EnergyStats{
				TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), 13, 0, 0, 0, time.UTC),
				SolarKWH:    0.0,
				HomeKWH:     10.0,
			})
		}

		settings := types.Settings{
			GridExportSolar: true,
			MinBatterySOC:   20.0, // minKWH = 2.0 kWh
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		if assert.NotEmpty(t, simData) && assert.GreaterOrEqual(t, len(simData), 2) {
			// Hour 12: Hits capacity, deficitKWH reset to 0.0
			assert.Equal(t, 0.0, simData[0].TotalBatteryDeficitKWH)
			assert.False(t, simData[0].HitCapacityAt.IsZero())

			// Hour 13: Drains from 10.0 kWh. Load is 10.0 kWh.
			// newSimEnergy = 10.0 - 10.0 = 0.0 kWh.
			// minKWH is 2.1 kWh (due to safety buffer: MinBatterySOC + 1%).
			// Deficit should be 2.1 - 0.0 = 2.1 kWh.
			assert.Equal(t, 2.1, simData[1].TotalBatteryDeficitKWH)
			assert.False(t, simData[1].HitDeficitAt.IsZero())

			// Assert on all three deficit fields in hour 13
			assert.False(t, simData[1].HitAboveDeficitAt.IsZero())
			assert.False(t, simData[1].HitDeficitAt.IsZero())
			assert.False(t, simData[1].HitBelowDeficitAt.IsZero())

			// Verify they hit at different times (AboveDeficit first, then Deficit, then BelowDeficit)
			assert.True(t, simData[1].HitAboveDeficitAt.Before(simData[1].HitDeficitAt))
			assert.True(t, simData[1].HitDeficitAt.Before(simData[1].HitBelowDeficitAt))
		}
	})

	t.Run("DeficitThresholdBuffer3Percent", func(t *testing.T) {
		// Scenario:
		// 10.0 kWh capacity. MinBatterySOC = 20.0%.
		// minKWH = 10.0 * 21% = 2.1 kWh.
		// deficitThresholdKWH = 2.1 - 10.0 * 0.03 = 1.8 kWh.
		// Start SOC = 20.5% (2.05 kWh).
		// Hour 0: Load 1.0 kW. Ends at 1.05 kWh (below 1.8, deficit!).
		// HitDeficitAt should be Hour 0 start + 15 minutes (fraction = (2.05 - 1.8) / 1.0 = 0.25 hours = 15 minutes).
		now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            20.5,
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
			for h := 0; h < 24; h++ {
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), h, 0, 0, 0, time.UTC),
					SolarKWH:    0.0,
					HomeKWH:     1.0,
				})
			}
		}

		settings := types.Settings{
			GridExportSolar: true,
			MinBatterySOC:   20.0,
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		for idx, sd := range simData {
			t.Logf("Hour %d (%s): BatteryKWH=%.3f, Deficit=%.3f, HitDeficitAt=%s",
				idx, sd.TS.Format("15:04"), sd.BatteryKWH, sd.TotalBatteryDeficitKWH, sd.HitDeficitAt.Format("15:04:05"))
		}

		assert.NotEmpty(t, simData)
		// Hour 0: Deficit is registered
		assert.Greater(t, simData[0].TotalBatteryDeficitKWH, 0.0)

		// Starts at 2.05 kWh.
		// minKWH is 2.1 kWh, so it is already below minKWH at start.
		// aboveDeficitThresholdKWH is 2.2 kWh, so it is already below that at start.
		// Thus, HitAboveDeficitAt and HitDeficitAt should hit immediately (at now).
		if assert.False(t, simData[0].HitAboveDeficitAt.IsZero()) {
			assert.Equal(t, now, simData[0].HitAboveDeficitAt)
		}
		if assert.False(t, simData[0].HitDeficitAt.IsZero()) {
			assert.Equal(t, now, simData[0].HitDeficitAt)
		}

		// deficitThresholdKWH is 1.8 kWh, so we hit it at 15 minutes.
		if assert.False(t, simData[0].HitBelowDeficitAt.IsZero()) {
			expectedHitTime := now.Add(15 * time.Minute)
			assert.WithinDuration(t, expectedHitTime, simData[0].HitBelowDeficitAt, time.Second)
		}
	})

	t.Run("ContinuousDeficitAccumulation", func(t *testing.T) {
		now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

		// Create history where home load is 0.4 kWh in hour 0, 0.2 kWh in hour 1, and 0.15 kWh in hour 2
		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
			for h := 0; h < 24; h++ {
				load := 0.0
				if h == 0 {
					load = 0.4
				} else if h == 1 {
					load = 0.2
				} else if h == 2 {
					load = 0.15
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), h, 0, 0, 0, time.UTC),
					SolarKWH:    0.0,
					HomeKWH:     load,
				})
			}
		}

		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    10.0,
			BatterySOC:            21.0, // Start exactly at minKWH (2.1 kWh)
			BatteryKW:             0,
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		settings := types.Settings{
			GridExportSolar: true,
			MinBatterySOC:   20.0,
		}

		simData := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		if assert.NotEmpty(t, simData) && assert.GreaterOrEqual(t, len(simData), 3) {
			// Hour 0: Drains 0.4 kWh. Drops below 1.8 kWh (deficit threshold).
			// Deficit should be 0.4 kWh.
			assert.InDelta(t, 0.4, simData[0].TotalBatteryDeficitKWH, 0.001)

			// Hour 1: Drains 0.2 kWh. Since it's already below deficit, it should accumulate 0.2 kWh.
			// Total Deficit should be 0.6 kWh.
			assert.InDelta(t, 0.6, simData[1].TotalBatteryDeficitKWH, 0.001)

			// Hour 2: Drains 0.15 kWh.
			// Total Deficit should be 0.75 kWh.
			assert.InDelta(t, 0.75, simData[2].TotalBatteryDeficitKWH, 0.001)
		}
	})

}
