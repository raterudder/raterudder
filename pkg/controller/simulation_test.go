package controller

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateSolarTrend(t *testing.T) {
	c := NewController()
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 13, 0, 0, 0, time.UTC)
	historyStart := now.Add(-2 * time.Hour)

	// Mock model
	model := map[int]TimeProfile{
		11: {AvgSolarKWH: 2.0},
		12: {AvgSolarKWH: 3.0},
		13: {AvgSolarKWH: 4.0},
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
		nightModel := map[int]TimeProfile{
			0: {AvgSolarKWH: 0.0},
			1: {AvgSolarKWH: 0.0},
			2: {AvgSolarKWH: 0.0},
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

		simData, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

		// Verify first few hours
		// Hour 0: Start 5.0. Net -1. End 4.0.
		assert.Equal(t, 0, simData[0].Hour)
		assert.InDelta(t, 5.0, simData[0].StartBatteryKWH, 0.01, "Hour 0: Start should be 5.0")
		assert.InDelta(t, 4.0, simData[0].BatteryKWH, 0.01, "Hour 0: Should drain to 4.0")

		// Hour 1: Start 4.0. Net -1. End 3.0.
		assert.Equal(t, 1, simData[1].Hour)
		assert.InDelta(t, 4.0, simData[1].StartBatteryKWH, 0.01, "Hour 1: Start should be 4.0")
		assert.InDelta(t, 3.0, simData[1].BatteryKWH, 0.01, "Hour 1: Should drain to 3.0")

		// Hour 2: Start 3.0. Net -1. End 2.0.
		assert.Equal(t, 2, simData[2].Hour)
		assert.InDelta(t, 3.0, simData[2].StartBatteryKWH, 0.01, "Hour 2: Start should be 3.0")
		assert.InDelta(t, 2.0, simData[2].BatteryKWH, 0.01, "Hour 2: Should drain to 2.0")

		// Hour 3: Start 2.0. Net +1. End 3.0.
		assert.Equal(t, 3, simData[3].Hour)
		assert.InDelta(t, 2.0, simData[3].StartBatteryKWH, 0.01, "Hour 3: Start should be 2.0")
		assert.InDelta(t, 3.0, simData[3].BatteryKWH, 0.01, "Hour 3: Should charge to 3.0")

		// Hour 4: Start 3.0. Net +1. End 4.0.
		assert.Equal(t, 4, simData[4].Hour)
		assert.InDelta(t, 3.0, simData[4].StartBatteryKWH, 0.01, "Hour 4: Start should be 3.0")
		assert.InDelta(t, 4.0, simData[4].BatteryKWH, 0.01, "Hour 4: Should charge to 4.0")

		// Hour 5: Start 4.0. Net +1. End 5.0.
		assert.Equal(t, 5, simData[5].Hour)
		assert.InDelta(t, 4.0, simData[5].StartBatteryKWH, 0.01, "Hour 5: Start should be 4.0")
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
		simData, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

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

		simData, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

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

				simData, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, []types.Price{futurePrice}, nil, nil, settings)
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

				simData, _ := c.SimulateState(ctx, now, currentStatus, price, futurePrices, nil, nil, settings)
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

				simData, _ := c.SimulateState(ctx, now, currentStatus, price, nil, nil, nil, settings)
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

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

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

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

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

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

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

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

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

		simData, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, nil, nil, types.Settings{})

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
			GridExportSolar:  true,
			MinBatterySOC:    20.0, // minKWH = 2.1 kWh
			SOCBufferPercent: 4.0,  // bufferedMinKWH = 2.5 kWh, thresholdMinKWH = 2.3 kWh
		}

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

		if assert.NotEmpty(t, simData) && assert.GreaterOrEqual(t, len(simData), 2) {
			// Hour 12: Hits capacity, deficitKWH reset to 0.0
			assert.Equal(t, 0.0, simData[0].TotalBatteryDeficitKWH)
			assert.False(t, simData[0].HitCapacityAt.IsZero())
			assert.False(t, simData[0].HitBufferedCapacityAt.IsZero())
			assert.False(t, simData[0].HitThresholdCapacityAt.IsZero())

			// Hour 13: Drains from 10.0 kWh. Load is 10.0 kWh.
			// minKWH is 2.1 kWh, bufferedMinKWH is 2.5 kWh.
			// Deficit relative to minKWH should be 2.1 - 0.0 = 2.1 kWh.
			// Deficit relative to bufferedMinKWH should be 2.5 - 0.0 = 2.5 kWh.
			assert.InDelta(t, 2.1, simData[1].TotalBatteryDeficitKWH, 0.0001)
			assert.InDelta(t, 2.5, simData[1].TotalBufferedDeficitKWH, 0.0001)
			assert.InDelta(t, 2.3, simData[1].TotalThresholdDeficitKWH, 0.0001)
			assert.False(t, simData[1].HitDeficitAt.IsZero())

			// Verify battery energies at the end of hour 13 are clamped to their respective reserves
			assert.InDelta(t, 2.1, simData[1].BatteryKWH, 0.0001)
			assert.InDelta(t, 2.5, simData[1].BufferedBatteryKWH, 0.0001)
			assert.InDelta(t, 2.3, simData[1].ThresholdBatteryKWH, 0.0001)

			// Assert on all three deficit fields in hour 13
			assert.False(t, simData[1].HitBufferedDeficitAt.IsZero())
			assert.False(t, simData[1].HitThresholdDeficitAt.IsZero())
			assert.False(t, simData[1].HitDeficitAt.IsZero())

			// Verify they hit at different times (Buffered first, then Threshold, then Deficit)
			assert.True(t, simData[1].HitBufferedDeficitAt.Before(simData[1].HitThresholdDeficitAt))
			assert.True(t, simData[1].HitThresholdDeficitAt.Before(simData[1].HitDeficitAt))
		}
	})

	t.Run("DeficitThresholdBufferPercent", func(t *testing.T) {
		// Scenario:
		// 10.0 kWh capacity. MinBatterySOC = 20.0%.
		// minKWH = 2.1 kWh.
		// SOCBufferPercent = 4.0 -> bufferedMinKWH = 2.5 kWh, thresholdMinKWH = 2.3 kWh.
		// Start SOC = 24.5% (2.45 kWh).
		// Hour 0: Load 1.0 kW.
		// HitDeficitAt: drops below 2.1 kWh at 21 minutes.
		// ThresholdHitDeficitAt: drops below 2.3 kWh at 9 minutes.
		// BufferedHitDeficitAt: starts below 2.5 kWh, so hits immediately (at now).
		now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            24.5,
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
			GridExportSolar:  true,
			MinBatterySOC:    20.0,
			SOCBufferPercent: 4.0,
		}

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)
		assert.NotEmpty(t, simData)
		// Hour 0: Deficit is registered
		assert.Greater(t, simData[0].TotalBatteryDeficitKWH, 0.0)

		// Verify precise deficit sizes
		assert.InDelta(t, 0.65, simData[0].TotalBatteryDeficitKWH, 0.0001)
		assert.InDelta(t, 1.05, simData[0].TotalBufferedDeficitKWH, 0.0001)
		assert.InDelta(t, 0.85, simData[0].TotalThresholdDeficitKWH, 0.0001)

		// Verify battery energies at the end of the hour
		assert.InDelta(t, 2.1, simData[0].BatteryKWH, 0.0001)
		assert.InDelta(t, 2.5, simData[0].BufferedBatteryKWH, 0.0001)
		assert.InDelta(t, 2.3, simData[0].ThresholdBatteryKWH, 0.0001)

		// Starts at 2.45 kWh.
		// bufferedMinKWH is 2.5 kWh, so it is already below bufferedMinKWH at start.
		if assert.False(t, simData[0].HitBufferedDeficitAt.IsZero()) {
			assert.Equal(t, now, simData[0].HitBufferedDeficitAt)
		}

		// thresholdMinKWH is 2.3 kWh, hit at 9 minutes.
		if assert.False(t, simData[0].HitThresholdDeficitAt.IsZero()) {
			expectedHitTime := now.Add(9 * time.Minute)
			assert.WithinDuration(t, expectedHitTime, simData[0].HitThresholdDeficitAt, time.Second)
		}

		// minKWH is 2.1 kWh, hit at 21 minutes.
		if assert.False(t, simData[0].HitDeficitAt.IsZero()) {
			expectedHitTime := now.Add(21 * time.Minute)
			assert.WithinDuration(t, expectedHitTime, simData[0].HitDeficitAt, time.Second)
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

		simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

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

	t.Run("VPPEvents", func(t *testing.T) {
		t.Run("VPP Charging Blackout Discharging and Restore", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    0,
						HomeKWH:     1.0,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            50.0, // 5.0 kWh
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 5.0,
				VPPEvents: []types.VPPEvent{
					{
						Description: "VPP Test Event",
						TSStart:     now.Add(5 * time.Hour), // 17:00
						TSEnd:       now.Add(8 * time.Hour), // 20:00
						VPPSoc:      10.0,                   // 10% SOC = 1.0 kWh reserve during event
					},
				},
			}

			settings := types.Settings{
				MinBatterySOC:            20.0, // 2.1 kWh regular reserve
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 0,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)

			if assert.Len(t, simData, 24) {
				// starting at 12:00, with 1 kW load and 2 kW max charge rate,
				// the start charging time is calculated to be 12:24 (24 minutes past 12:00).
				assert.WithinDuration(t, now.Add(24*time.Minute), simData[0].StartedVPPChargingAt, time.Second)
				assert.InDelta(t, 5.0, simData[0].StartBatteryKWH, 0.001)

				// Hour 1: Charging continues
				assert.InDelta(t, 5.8, simData[1].StartBatteryKWH, 0.001)

				// Hour 2: Charging completes
				assert.InDelta(t, 7.8, simData[2].StartBatteryKWH, 0.001)

				// Hour 3 (15:00-16:00): Blackout window starts at 15:00.
				// Battery cannot discharge during this hour.
				assert.Equal(t, now.Add(3*time.Hour), simData[3].VPPStandbyAt)
				assert.InDelta(t, 9.8, simData[3].StartBatteryKWH, 0.001)
				assert.InDelta(t, 9.8, simData[3].BatteryKWH, 0.001) // held at 9.8 kWh

				// Hour 4 (16:00-17:00): Blackout window continues.
				assert.Equal(t, now.Add(3*time.Hour), simData[4].VPPStandbyAt)
				assert.InDelta(t, 9.8, simData[4].StartBatteryKWH, 0.001)
				assert.InDelta(t, 9.8, simData[4].BatteryKWH, 0.001) // held at 9.8 kWh

				// Hour 5 (17:00-18:00): VPP Event starts.
				assert.InDelta(t, 9.8, simData[5].StartBatteryKWH, 0.001)
				assert.InDelta(t, 4.8, simData[5].BatteryKWH, 0.001)

				// Hour 6 (18:00-19:00): VPP Event continues.
				assert.InDelta(t, 4.8, simData[6].StartBatteryKWH, 0.001)
				assert.InDelta(t, 1.0, simData[6].BatteryKWH, 0.001)

				// Hour 7 (19:00-20:00): VPP Event finishes discharge
				assert.InDelta(t, 1.0, simData[7].StartBatteryKWH, 0.001)
				assert.InDelta(t, 1.0, simData[7].BatteryKWH, 0.001)

				// Hour 8 (20:00-21:00): VPP Event is over (ended at 20:00).
				assert.True(t, simData[8].VPPStandbyAt.IsZero())
				assert.InDelta(t, 1.0, simData[8].StartBatteryKWH, 0.001)
				assert.InDelta(t, 2.1, simData[8].BatteryKWH, 0.001)
				assert.Greater(t, simData[8].TotalBatteryDeficitKWH, 0.0)
			}
		})

		t.Run("VPP No Charging Needed", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    0,
						HomeKWH:     0,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            100.0,
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 5.0,
				VPPEvents: []types.VPPEvent{
					{
						Description: "VPP Test Event",
						TSStart:     now.Add(3 * time.Hour),
						TSEnd:       now.Add(5 * time.Hour),
						VPPSoc:      20.0,
					},
				},
			}

			settings := types.Settings{
				MinBatterySOC:            20.0,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 0,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)
			if assert.Len(t, simData, 24) {
				assert.True(t, simData[0].StartedVPPChargingAt.IsZero() || simData[0].StartedVPPChargingAt.Equal(now.Add(3*time.Hour).Add(-30*time.Minute)))
			}
		})

		t.Run("Multiple VPP Events", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    0,
						HomeKWH:     0.5,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            80.0,
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 5.0,
				VPPEvents: []types.VPPEvent{
					{
						Description: "VPP Event 1",
						TSStart:     now.Add(3 * time.Hour),
						TSEnd:       now.Add(5 * time.Hour),
						VPPSoc:      20.0,
					},
					{
						Description: "VPP Event 2",
						TSStart:     now.Add(9 * time.Hour),
						TSEnd:       now.Add(11 * time.Hour),
						VPPSoc:      20.0,
					},
				},
			}

			settings := types.Settings{
				MinBatterySOC:            20.0,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 0,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)
			if assert.Len(t, simData, 24) {
				assert.Equal(t, now.Add(1*time.Hour), simData[2].VPPStandbyAt)
				assert.Equal(t, now.Add(5*time.Hour), simData[2].VPPEndAt)
				assert.True(t, simData[6].VPPStandbyAt.IsZero())
				assert.Equal(t, now.Add(7*time.Hour), simData[8].VPPStandbyAt)
				assert.Equal(t, now.Add(11*time.Hour), simData[8].VPPEndAt)
			}
		})

		t.Run("Solar Clamping in Pre-VPP States", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

			// We define a history that has solar generation.
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    4.0, // High solar generation
						HomeKWH:     0.5,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            85.0, // 8.5 kWh
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 5.0,
				VPPEvents: []types.VPPEvent{
					{
						Description: "VPP Solar Clamping Test",
						TSStart:     now.Add(4 * time.Hour), // 16:00
						TSEnd:       now.Add(7 * time.Hour), // 19:00
						VPPSoc:      20.0,
					},
				},
			}

			settings := types.Settings{
				MinBatterySOC:                      20.0,
				SolarTrendRatioMax:                 3.0,
				SolarBellCurveMultiplier:           0.0,
				GridChargeBatteries:                false,
				GridExportSolar:                    false, // EXPORT DISABLED -> solar clamping active!
				SolarFullyChargeHeadroomBatterySOC: 10.0,  // Headroom of 10% (curtailed at 90% SOC = 9.0 kWh)
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)
			if assert.Len(t, simData, 24) {
				// The pre-VPP charging phase or pre-VPP standby phase should reach 9.0 kWh and trigger solar clamping.
				// Since we start at 8.5 kWh, generating 4.0 kWh - 0.5 kWh load = 3.5 kWh net solar per hour.
				// We should quickly cross the 90% SOC threshold (9.0 kWh) during the very first hour (12:00-13:00).
				// Let's verify that HitSolarCapacityAt is not zero.
				if assert.False(t, simData[0].HitSolarCapacityAt.IsZero(), "Should record solar capacity limit hit") {
					// Solar capacity limit is 9.0 kWh.
					// We start at 8.5 kWh. Net charging rate is solar - load = 4.0 - 0.5 = 3.5 kW,
					// but clamped at MaxBatteryChargeKW = 2.0 kW.
					// Time to reach 9.0 kWh: (9.0 - 8.5) / 2.0 = 0.25 hours = 15 minutes past 12:00.
					expectedHitTime := now.Add(15 * time.Minute)
					assert.WithinDuration(t, expectedHitTime, simData[0].HitSolarCapacityAt, time.Second)
				}
			}
		})

		t.Run("VPP Discharging Limit", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    0,
						HomeKWH:     1.0,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            50.0, // 5.0 kWh
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 2.0,
				VPPEvents: []types.VPPEvent{
					{
						Description: "Active VPP",
						TSStart:     now.Add(-1 * time.Hour), // Started at 11:00
						TSEnd:       now.Add(2 * time.Hour),  // Ends at 14:00
						VPPSoc:      20.0,                    // 20% SOC = 2.0 kWh target
					},
				},
			}

			settings := types.Settings{
				MinBatterySOC:            30.0, // Regular reserve is 30% (3.0 kWh)
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 0,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)
			if assert.Len(t, simData, 24) {
				// Hour 0 (12:00-13:00): Battery starts at 5.0 kWh.
				// Max discharge power is 2.0 kW. Load is 1.0 kW.
				// Since we are in a VPP event, the battery discharges to cover load + export at MaxDischarge = 2.0 kW.
				// Ending energy: 5.0 - 2.0 = 3.0 kWh.
				assert.InDelta(t, 3.0, simData[0].BatteryKWH, 0.001)

				// Hour 1 (13:00-14:00): Battery starts at 3.0 kWh.
				// Since VPP target is 2.0 kWh, we can only discharge 1.0 kWh.
				// Discharge power is limited to 1.0 kW.
				// Ending energy: 3.0 - 1.0 = 2.0 kWh.
				assert.InDelta(t, 2.0, simData[1].BatteryKWH, 0.001)

				// Hour 2 (14:00-15:00): VPP is over. Regular reserve is 30% + 1% buffer = 3.1 kWh.
				// We now have immediate deficit charging back to 3.1 kWh.
				assert.InDelta(t, 3.1, simData[2].BatteryKWH, 0.001)
				assert.InDelta(t, 2.1, simData[2].TotalBatteryDeficitKWH, 0.001)
			}
		})

		t.Run("VPP Event Opt-Out is Ignored", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    0,
						HomeKWH:     1.0,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            50.0, // 5.0 kWh
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 2.0,
				VPPEvents: []types.VPPEvent{
					{
						Description: "Opted Out VPP",
						TSStart:     now.Add(-1 * time.Hour), // Started at 11:00
						TSEnd:       now.Add(2 * time.Hour),  // Ends at 14:00
						VPPSoc:      20.0,                    // 20% SOC = 2.0 kWh target
						OptOut:      true,
					},
				},
			}

			settings := types.Settings{
				MinBatterySOC:            30.0, // Regular reserve is 30% (3.0 kWh)
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 0,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, types.Price{}, nil, history, nil, settings)
			if assert.Len(t, simData, 24) {
				// Since VPP is opted out, it should be ignored.
				// Regular reserve is 30% + 1% buffer = 31% (3.1 kWh).
				// In Hour 0, the battery starts at 5.0 kWh.
				// Since VPP is ignored and we are above the min SOC (3.1 kWh),
				// the battery should only discharge to cover the home load (1.0 kW).
				// We should NOT export extra energy as if VPP was active.
				// Ending energy: 5.0 - 1.0 = 4.0 kWh.
				assert.InDelta(t, 4.0, simData[0].BatteryKWH, 0.001)

				// Ensure no VPP charging start time or VPP event ends are tracked since the event was ignored.
				for _, slot := range simData {
					assert.True(t, slot.StartedVPPChargingAt.IsZero())
				}
			}
		})

		t.Run("Sub-Hourly Dynamic Stepping", func(t *testing.T) {
			now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: pastDay.Add(time.Duration(h) * time.Hour),
						SolarKWH:    0,
						HomeKWH:     1.0,
					})
				}
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            50.0, // 5.0 kWh
				BatteryKW:             0,
				Timestamp:             now,
				MaxBatteryChargeKW:    2.0,
				MaxBatteryDischargeKW: 2.0,
			}

			settings := types.Settings{
				MinBatterySOC:            30.0,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 0,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
			}

			// Define prices with a split at 12:30
			currentPrice := types.Price{
				Provider:      "tou",
				TSStart:       now,
				TSEnd:         now.Add(time.Hour),
				DollarsPerKWH: 0.10,
			}

			futurePrices := []types.Price{
				{
					Provider:      "tou",
					TSStart:       now.Add(time.Hour),                       // 12:00
					TSEnd:         now.Add(time.Hour).Add(30 * time.Minute), // 12:30
					DollarsPerKWH: 0.05,
				},
				{
					Provider:      "tou",
					TSStart:       now.Add(time.Hour).Add(30 * time.Minute), // 12:30
					TSEnd:         now.Add(2 * time.Hour),                   // 13:00
					DollarsPerKWH: 0.25,
				},
			}
			// Fill remaining future prices up to 24 hours
			for i := 2; i < 24; i++ {
				hStart := now.Add(time.Duration(i) * time.Hour)
				futurePrices = append(futurePrices, types.Price{
					Provider:      "tou",
					TSStart:       hStart,
					TSEnd:         hStart.Add(time.Hour),
					DollarsPerKWH: 0.10,
				})
			}

			simData, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)
			// Total steps should be 25 (24 standard steps, plus 1 extra due to the 30-min split)
			assert.Len(t, simData, 25)

			// Step 0: 11:00 to 12:00
			assert.Equal(t, 0.10, simData[0].GridChargeDollarsPerKWH)
			assert.Equal(t, 1.0, simData[0].EnergyApplyRatio)

			// Step 1: 12:00 to 12:30 (Split - 30 minutes)
			assert.Equal(t, 0.05, simData[1].GridChargeDollarsPerKWH)
			assert.Equal(t, 0.5, simData[1].EnergyApplyRatio)
			assert.Equal(t, now.Add(time.Hour), simData[1].TS)

			// Step 2: 12:30 to 13:00 (Split - 30 minutes)
			assert.Equal(t, 0.25, simData[2].GridChargeDollarsPerKWH)
			assert.Equal(t, 0.5, simData[2].EnergyApplyRatio)
			assert.Equal(t, now.Add(time.Hour).Add(30*time.Minute), simData[2].TS)
		})

		t.Run("BufferedPredictedSolarInterpolation", func(t *testing.T) {
			now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

			// Setup history with different solar for Hour 10, Hour 11, Hour 12
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour)
				// Hour 10: 4.0 kWh
				history = append(history, types.EnergyStats{
					TSHourStart: pastDay.Truncate(24 * time.Hour).Add(10 * time.Hour),
					SolarKWH:    4.0,
					HomeKWH:     1.0,
				})
				// Hour 11: 8.0 kWh
				history = append(history, types.EnergyStats{
					TSHourStart: pastDay.Truncate(24 * time.Hour).Add(11 * time.Hour),
					SolarKWH:    8.0,
					HomeKWH:     1.0,
				})
				// Hour 12: 12.0 kWh
				history = append(history, types.EnergyStats{
					TSHourStart: pastDay.Truncate(24 * time.Hour).Add(12 * time.Hour),
					SolarKWH:    12.0,
					HomeKWH:     1.0,
				})
			}

			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    10.0,
				BatterySOC:            50.0,
				Timestamp:             now,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}

			currentPrice := types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)}
			futurePrices := make([]types.Price, 24)
			for i := 0; i < 24; i++ {
				futurePrices[i] = types.Price{
					DollarsPerKWH: 0.10,
					TSStart:       now.Add(time.Duration(i) * time.Hour),
					TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
				}
			}

			// Scenario A: bufferMinutes = 30
			settings30 := types.Settings{
				SolarCapacityBufferMinutes: 30,
				SolarTrendRatioMax:         3.0,
				SolarBellCurveMultiplier:   0,
			}
			simData30, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings30)

			// For Hour 12 (index 0 of simData30 since we start at 12:00)
			// targetFractionalHour = 12.0 - 0.5 = 11.5
			// Shifted solar is 50/50 between Hour 11 (8.0) and Hour 12 (12.0) = 10.0
			assert.InDelta(t, 10.0, simData30[0].BufferedPredictedSolarKWH, 0.01)
			// Unshifted is Hour 12 (12.0)
			assert.InDelta(t, 12.0, simData30[0].PredictedSolarKWH, 0.01)

			// Scenario B: bufferMinutes = 100 (1 hour 40 minutes)
			settings100 := types.Settings{
				SolarCapacityBufferMinutes: 100,
				SolarTrendRatioMax:         3.0,
				SolarBellCurveMultiplier:   0,
			}
			simData100, _ := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings100)

			// targetFractionalHour = 12.0 - 1.67 = 10.33
			// Shifted solar is interpolated between Hour 10 (4.0) and Hour 11 (8.0)
			// fraction = 0.3333
			// shiftedSolar = 0.6667 * 4.0 + 0.3333 * 8.0 = 2.6667 + 2.6667 = 5.333
			assert.InDelta(t, 5.33, simData100[0].BufferedPredictedSolarKWH, 0.01)
			assert.InDelta(t, 12.0, simData100[0].PredictedSolarKWH, 0.01)
		})

		t.Run("ComprehensiveFieldsVerification", func(t *testing.T) {
			now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
			capacityKWH := 10.0
			vppEvents := []types.VPPEvent{
				{
					TSStart: now.Add(4 * time.Hour),
					TSEnd:   now.Add(6 * time.Hour),
					VPPSoc:  30.0,
				},
			}
			currentStatus := types.SystemStatus{
				BatteryCapacityKWH:    capacityKWH,
				BatterySOC:            50.0, // 5.0 kWh
				Timestamp:             now,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
				VPPEvents:             vppEvents,
			}

			currentPrice := types.Price{
				TSStart:       now,
				TSEnd:         now.Add(time.Hour),
				DollarsPerKWH: 0.15,
			}

			var futurePrices []types.Price
			for i := 1; i <= 24; i++ {
				futurePrices = append(futurePrices, types.Price{
					TSStart:       now.Add(time.Duration(i) * time.Hour),
					TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: 0.10,
				})
			}

			// Simple history with solar and home loads
			history := []types.EnergyStats{}
			for i := 1; i <= 3; i++ {
				pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
				for h := 0; h < 24; h++ {
					history = append(history, types.EnergyStats{
						TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), h, 0, 0, 0, time.UTC),
						SolarKWH:    2.0,
						HomeKWH:     1.0,
					})
				}
			}

			settings := types.Settings{
				GridChargeBatteries:        true,
				GridExportSolar:            true,
				MinBatterySOC:              20.0, // 2.0 kWh min
				SOCBufferPercent:           4.0,  // bufferedMin = 2.4 kWh, thresholdMin = 2.2 kWh
				SolarTrendRatioMax:         3.0,
				SolarBellCurveMultiplier:   1.0,
				SolarCapacityBufferMinutes: 20,
			}

			simData, simParams := c.SimulateState(ctx, now, currentStatus, currentPrice, futurePrices, history, nil, settings)

			assert.NotEmpty(t, simData)
			assert.NotNil(t, simParams)

			// Assert on fields for the first hour
			firstHour := simData[0]
			assert.Equal(t, now, firstHour.TS)
			assert.Equal(t, now.Hour(), firstHour.Hour)
			assert.Equal(t, capacityKWH, firstHour.BatteryCapacityKWH)
			assert.Equal(t, 9.8, firstHour.CapacityThresholdKWH)
			assert.Equal(t, 2.0, firstHour.BatteryReserveKWH)
			assert.Equal(t, 5.0, firstHour.StartBatteryKWH)
			assert.Equal(t, 0.15, firstHour.GridChargeDollarsPerKWH)
			assert.Equal(t, 1.0, firstHour.AvgHomeLoadKWH)
			assert.Equal(t, 2.0, firstHour.PredictedSolarKWH)
			assert.Greater(t, firstHour.BufferedPredictedSolarKWH, 0.0)
			assert.Greater(t, firstHour.ThresholdPredictedSolarKWH, 0.0)

			// Net loads
			assert.Equal(t, firstHour.AvgHomeLoadKWH-firstHour.PredictedSolarKWH, firstHour.NetLoadSolarKWH)
			assert.Equal(t, firstHour.AvgHomeLoadKWH-firstHour.BufferedPredictedSolarKWH, firstHour.BufferedNetLoadSolarKWH)
			assert.Equal(t, firstHour.AvgHomeLoadKWH-firstHour.ThresholdPredictedSolarKWH, firstHour.ThresholdNetLoadSolarKWH)

			// Check VPP-related output
			var foundVPPStandby, foundVPPActive bool
			for _, hour := range simData {
				if !hour.VPPStandbyAt.IsZero() {
					foundVPPStandby = true
				}
				if !hour.VPPEndAt.IsZero() {
					foundVPPActive = true
				}
			}
			assert.True(t, foundVPPStandby, "Should have marked VPP standby window")
			assert.True(t, foundVPPActive, "Should have marked VPP end time")
			for _, hour := range simData {
				assert.True(t, hour.HitDeficitAt.IsZero(), "HitDeficitAt should not be set due to VPP event discharge ending")
			}
		})
	})

	t.Run("HitDeficitTrajectorySeparation", func(t *testing.T) {
		// Scenario: Ensure that the unbuffered, buffered, and threshold deficit hit times
		// diverge correctly due to their independent trajectories being clamped at different minimums.
		// Capacity: 10.0. MinBatterySOC: 20% + 1% baseline = 21% (2.1 kWh).
		// SOCBufferPercent: 4.0%.
		// minKWH = 2.1
		// thresholdMinKWH = 2.3
		// bufferedMinKWH = 2.5
		// Initial SOC: 25% (2.5 kWh).
		// Hour 10: Net load 0.3 kWh.
		//   - unbuffered starts at 2.5, drops to 2.2 kWh. No deficit.
		//   - threshold starts at 2.5, drops to 2.2 kWh -> clamped at 2.3 kWh, deficit = 0.1 kWh.
		//   - HitThresholdDeficitAt is at fraction (2.5 - 2.3)/0.3 = 2/3 hr = 40 mins.
		//   - buffered starts at 2.5, drops to 2.2 kWh -> clamped at 2.5 kWh, deficit = 0.3 kWh.
		//   - HitBufferedDeficitAt is at fraction (2.5 - 2.5)/0.3 = 0 hr = 0 mins.
		// Hour 11: Net load 0.2 kWh.
		//   - unbuffered starts at 2.2 kWh, drops to 2.0 kWh -> clamped at 2.1 kWh, deficit = 0.1 kWh.
		//   - HitDeficitAt is at fraction (2.2 - 2.1)/0.2 = 0.5 hr = 30 mins.
		//   - threshold starts at 2.3 kWh, drops to 2.1 kWh -> clamped at 2.3 kWh, deficit = 0.2 kWh.
		//   - HitThresholdDeficitAt remains 10:40 (from previous hour).
		//   - buffered starts at 2.5 kWh, drops to 2.3 kWh -> clamped at 2.5 kWh, deficit = 0.2 kWh.
		//   - HitBufferedDeficitAt remains 10:00 (from previous hour).
		now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            25.0,
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
			for h := 0; h < 24; h++ {
				load := 0.0
				if h == 10 {
					load = 0.3
				} else if h == 11 {
					load = 0.2
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), h, 0, 0, 0, time.UTC),
					SolarKWH:    0.0,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			GridExportSolar:  true,
			MinBatterySOC:    20.0,
			SOCBufferPercent: 4.0,
		}

		simData, _ := c.SimulateState(context.Background(), now, currentStatus, types.Price{}, nil, history, nil, settings)
		if assert.NotEmpty(t, simData) && assert.GreaterOrEqual(t, len(simData), 2) {
			// Hour 0 (10:00)
			assert.Equal(t, 10, simData[0].Hour)
			assert.InDelta(t, 2.2, simData[0].BatteryKWH, 0.001)
			assert.InDelta(t, 2.3, simData[0].ThresholdBatteryKWH, 0.001)
			assert.InDelta(t, 2.5, simData[0].BufferedBatteryKWH, 0.001)

			assert.InDelta(t, 0.0, simData[0].TotalBatteryDeficitKWH, 0.001)
			assert.InDelta(t, 0.1, simData[0].TotalThresholdDeficitKWH, 0.001)
			assert.InDelta(t, 0.3, simData[0].TotalBufferedDeficitKWH, 0.001)

			assert.True(t, simData[0].HitDeficitAt.IsZero())
			assert.WithinDuration(t, now.Add(40*time.Minute), simData[0].HitThresholdDeficitAt, time.Millisecond)
			assert.Equal(t, now, simData[0].HitBufferedDeficitAt)

			// Hour 1 (11:00)
			assert.Equal(t, 11, simData[1].Hour)
			assert.InDelta(t, 2.1, simData[1].BatteryKWH, 0.001)
			assert.InDelta(t, 2.3, simData[1].ThresholdBatteryKWH, 0.001)
			assert.InDelta(t, 2.5, simData[1].BufferedBatteryKWH, 0.001)

			assert.InDelta(t, 0.1, simData[1].TotalBatteryDeficitKWH, 0.001)
			assert.InDelta(t, 0.3, simData[1].TotalThresholdDeficitKWH, 0.001) // 0.1 + 0.2
			assert.InDelta(t, 0.5, simData[1].TotalBufferedDeficitKWH, 0.001)  // 0.3 + 0.2

			assert.WithinDuration(t, now.Add(1*time.Hour+30*time.Minute), simData[1].HitDeficitAt, time.Millisecond)
			assert.WithinDuration(t, now.Add(40*time.Minute), simData[1].HitThresholdDeficitAt, time.Millisecond)
			assert.Equal(t, now, simData[1].HitBufferedDeficitAt)
		}
	})

	t.Run("ShiftedEnergyClampingAndCapacityHit", func(t *testing.T) {
		// Scenario: Verify that thresholdShiftedEnergyKWH is correctly clamped
		// and does not get overridden when capacity is not hit. We detect this
		// by verifying the exact hit capacity time on the threshold shifted trajectory.
		now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
		capacityKWH := 10.0
		currentStatus := types.SystemStatus{
			BatteryCapacityKWH:    capacityKWH,
			BatterySOC:            26.0, // 2.6 kWh
			Timestamp:             now,
			MaxBatteryChargeKW:    5.0,
			MaxBatteryDischargeKW: 5.0,
		}

		history := []types.EnergyStats{}
		for i := 1; i <= 3; i++ {
			pastDay := now.Add(time.Duration(-24*i) * time.Hour).Truncate(time.Hour)
			for h := 0; h < 24; h++ {
				solar := 0.0
				load := 0.0
				if h == 10 {
					load = 2.0 // Drains battery
				} else if h == 11 || h == 12 {
					solar = 10.0 // Charges battery
				}
				history = append(history, types.EnergyStats{
					TSHourStart: time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), h, 0, 0, 0, time.UTC),
					SolarKWH:    solar,
					HomeKWH:     load,
				})
			}
		}

		settings := types.Settings{
			GridExportSolar:            true,
			MinBatterySOC:              20.0, // minKWH = 2.0
			SOCBufferPercent:           10.0, // thresholdMinKWH = 2.5
			SolarCapacityBufferMinutes: 0,    // no shifting of solar to keep math simple
		}

		simData, _ := c.SimulateState(context.Background(), now, currentStatus, types.Price{}, nil, history, nil, settings)
		if assert.NotEmpty(t, simData) && assert.GreaterOrEqual(t, len(simData), 3) {
			// Without the bug, thresholdShiftedEnergyKWH starts Hour 1 (11:00) at 2.5 kWh
			// (clamped from 2.6 - 2.0 = 0.6 to threshold reserve 2.5? Wait, starting SOC is 2.6, load 2.0 -> clamped to 2.5 (or actually 2.6 because thresholdMinKWH = 2.1 + 0.5 = 2.6).
			// Yes: threshold reserve is 2.1 + 0.5 = 2.6. So starting Hour 1 (11:00) is 2.6.
			// During Hour 1, it charges 5.0 kWh to end Hour 1 at 7.6 kWh.
			// During Hour 2 (12:00), it charges from 7.6 kWh to capacity threshold 9.8 kWh.
			// Time to reach capacity: (9.8 - 7.6) / 5.0 = 0.44 hours (26.4 minutes).
			// Expected HitThresholdCapacityAt is 10:00 + 2 hours + 26.4 minutes = 12:26:24.
			expectedTime := now.Add(2*time.Hour + 26*time.Minute + 24*time.Second)
			assert.WithinDuration(t, expectedTime, simData[2].HitThresholdCapacityAt, 2*time.Second)
		}
	})

	t.Run("VariableMinSOC", func(t *testing.T) {
		now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
		capacityKWH := 10.0

		t.Run("TimeBasedReserveStepIncrease_DeficitRecordedAndBatteryRaised", func(t *testing.T) {
			status := types.SystemStatus{
				BatteryCapacityKWH:    capacityKWH,
				BatterySOC:            10.0, // 1.0 kWh
				Timestamp:             now,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}

			settings := types.Settings{
				MinBatterySOC: 10.0,
				MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}}},
						MinBatterySOC: 10.0,
					},
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 24}}},
						MinBatterySOC: 30.0,
					},
				},
			}

			simData, _ := c.SimulateState(context.Background(), now, status, types.Price{}, nil, nil, nil, settings)
			require.NotEmpty(t, simData)

			// Hour 0 (10:00 - 11:00): Min SOC is 10%
			assert.InDelta(t, 10.0*0.10, simData[0].BatteryReserveKWH, 0.01)

			// Hour 1 (11:00 - 12:00): Min SOC jumps to 30%.
			// Battery was simulated at 1.0 kWh, so step deficit is accumulated.
			assert.Greater(t, simData[1].TotalBatteryDeficitKWH, 0.0)
			assert.GreaterOrEqual(t, simData[1].BatteryKWH, 10.0*0.30-0.01)
		})

		t.Run("NameBasedReserve_PeriodNameMatchesPrice", func(t *testing.T) {
			status := types.SystemStatus{
				BatteryCapacityKWH:    capacityKWH,
				BatterySOC:            50.0,
				Timestamp:             now,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}

			settings := types.Settings{
				MinBatterySOC: 20.0,
				MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
					{UtilityPeriodName: "Off-Peak", MinBatterySOC: 15.0},
					{UtilityPeriodName: "Peak", MinBatterySOC: 60.0},
				},
			}

			curPrice := types.Price{TSStart: now, TSEnd: now.Add(time.Hour), DollarsPerKWH: 0.10, PeriodName: "Off-Peak"}
			futurePrices := []types.Price{
				{TSStart: now.Add(time.Hour), TSEnd: now.Add(2 * time.Hour), DollarsPerKWH: 0.40, PeriodName: "Peak"},
			}

			simData, _ := c.SimulateState(context.Background(), now, status, curPrice, futurePrices, nil, nil, settings)
			require.GreaterOrEqual(t, len(simData), 2)

			assert.InDelta(t, 10.0*0.15, simData[0].BatteryReserveKWH, 0.01)
			assert.InDelta(t, 10.0*0.60, simData[1].BatteryReserveKWH, 0.01)
		})

		t.Run("ReserveDrop_BatteryNaturallyDischargesToNewLowerMinimum", func(t *testing.T) {
			status := types.SystemStatus{
				BatteryCapacityKWH:    capacityKWH,
				BatterySOC:            50.0, // 5.0 kWh
				Timestamp:             now,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}

			// History provides 1.0 kWh/hr home load, 0 solar
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
				MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}}},
						MinBatterySOC: 50.0, // 5.0 kWh reserve until 11:00
					},
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 24}}},
						MinBatterySOC: 20.0, // 2.0 kWh reserve after 11:00
					},
				},
			}

			simData, _ := c.SimulateState(context.Background(), now, status, types.Price{}, nil, history, nil, settings)
			require.GreaterOrEqual(t, len(simData), 4)

			// Hour 0 (10:00): Reserve 5.0 kWh, load 1.0 kWh, clamped at unbuffered min reserve 5.1 kWh, 1.1 kWh deficit
			assert.InDelta(t, 5.0, simData[0].BatteryReserveKWH, 0.01)
			assert.InDelta(t, 5.1, simData[0].BatteryKWH, 0.01)

			// Hour 1 (11:00): Reserve drops to 2.0 kWh. Battery starts at 5.1 kWh, load 1.0 kWh -> ends at 4.1 kWh. No deficit!
			assert.InDelta(t, 2.0, simData[1].BatteryReserveKWH, 0.01)
			assert.InDelta(t, 4.1, simData[1].BatteryKWH, 0.01)
			assert.InDelta(t, 0.0, simData[1].TotalBatteryDeficitKWH-simData[0].TotalBatteryDeficitKWH, 0.01)

			// Hour 2 (12:00): Battery starts at 4.1 kWh, load 1.0 kWh -> ends at 3.1 kWh. No deficit!
			assert.InDelta(t, 2.0, simData[2].BatteryReserveKWH, 0.01)
			assert.InDelta(t, 3.1, simData[2].BatteryKWH, 0.01)
			assert.InDelta(t, 0.0, simData[2].TotalBatteryDeficitKWH-simData[0].TotalBatteryDeficitKWH, 0.01)

			// Hour 3 (13:00): Battery starts at 3.1 kWh, load 1.0 kWh -> reaches new lower reserve 2.1 kWh cleanly!
			assert.InDelta(t, 2.0, simData[3].BatteryReserveKWH, 0.01)
			assert.InDelta(t, 2.1, simData[3].BatteryKWH, 0.01)
		})

		t.Run("OvernightReserveSchedule_CrossesMidnight", func(t *testing.T) {
			tLate := time.Date(2026, 5, 20, 23, 0, 0, 0, time.UTC)
			status := types.SystemStatus{
				BatteryCapacityKWH:    capacityKWH,
				BatterySOC:            50.0,
				Timestamp:             tLate,
				MaxBatteryChargeKW:    5.0,
				MaxBatteryDischargeKW: 5.0,
			}

			settings := types.Settings{
				MinBatterySOC: 20.0,
				MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 22, HourEnd: 6}}},
						MinBatterySOC: 40.0, // 4.0 kWh overnight reserve
					},
					{
						TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}}},
						MinBatterySOC: 20.0, // 2.0 kWh day reserve
					},
				},
			}

			var futurePrices []types.Price
			for i := 1; i <= 10; i++ {
				ts := tLate.Add(time.Duration(i) * time.Hour)
				futurePrices = append(futurePrices, types.Price{
					TSStart:       ts,
					TSEnd:         ts.Add(time.Hour),
					DollarsPerKWH: 0.15,
				})
			}

			simData, _ := c.SimulateState(context.Background(), tLate, status, types.Price{TSStart: tLate, TSEnd: tLate.Add(time.Hour)}, futurePrices, nil, nil, settings)
			require.GreaterOrEqual(t, len(simData), 8)

			// 23:00 (before midnight): Overnight reserve 4.0 kWh (40%)
			assert.InDelta(t, 4.0, simData[0].BatteryReserveKWH, 0.01)

			// 00:00 (after midnight): Overnight reserve remains 4.0 kWh (40%)
			assert.InDelta(t, 4.0, simData[1].BatteryReserveKWH, 0.01)

			// 05:00: Overnight reserve remains 4.0 kWh (40%)
			assert.InDelta(t, 4.0, simData[6].BatteryReserveKWH, 0.01)

			// 06:00: Day reserve starts, reserve drops to 2.0 kWh (20%)
			assert.InDelta(t, 2.0, simData[7].BatteryReserveKWH, 0.01)
		})
	})

	t.Run("EVChargingPeriods", func(t *testing.T) {
		loc := time.UTC
		evNow := time.Date(2026, 8, 20, 22, 0, 0, 0, loc)

		evStatus := types.SystemStatus{
			Timestamp:          evNow,
			BatterySOC:         80.0,
			BatteryCapacityKWH: 13.5,
			BatteryAboveMinSOC: true,
			HomeKW:             1.0,
		}

		evCurrentPrice := types.Price{
			TSStart:       evNow,
			TSEnd:         evNow.Add(time.Hour),
			DollarsPerKWH: 0.10,
		}

		var evFuturePrices []types.Price
		for i := 1; i <= 24; i++ {
			tFuture := evNow.Add(time.Duration(i) * time.Hour)
			evFuturePrices = append(evFuturePrices, types.Price{
				TSStart:       tFuture,
				TSEnd:         tFuture.Add(time.Hour),
				DollarsPerKWH: 0.10,
			})
		}

		// Create 14 days of history where midnight to 4 AM had 11.5 kW EV charges
		var evHistory []types.EnergyStats
		startHist := evNow.AddDate(0, 0, -14).Truncate(24 * time.Hour)
		for d := 0; d < 14; d++ {
			dayStart := startHist.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				hStart := dayStart.Add(time.Duration(h) * time.Hour)
				load := 0.8
				if h >= 0 && h <= 3 {
					load = 11.5
				}
				evHistory = append(evHistory, types.EnergyStats{
					TSHourStart: hStart,
					HomeKWH:     load,
				})
			}
		}

		t.Run("WithoutEVChargingPeriods_HighEVLoadPredicted", func(t *testing.T) {
			settingsWithoutEV := types.Settings{
				MinBatterySOC: 20.0,
			}
			model, _ := c.BuildHourlyEnergyModel(ctx, evNow, evHistory, nil, settingsWithoutEV)
			// Model predicts heavy 11.5 kW load at midnight
			assert.GreaterOrEqual(t, model[0].AvgHomeLoadKWH, 5.0)
		})

		t.Run("WithEVChargingPeriods_EVLoadClampedToBaseline", func(t *testing.T) {
			settingsWithEV := types.Settings{
				MinBatterySOC: 20.0,
				EVChargingPeriods: []types.TimePeriod{
					{
						Name: "Night EV Charging",
						Hours: []types.UtilityHourPeriod{
							{
								HourStart: 23,
								HourEnd:   6,
							},
						},
					},
				},
			}

			model, _ := c.BuildHourlyEnergyModel(ctx, evNow, evHistory, nil, settingsWithEV)
			// Model filters out EV load in window, predicting normal household baseline (~0.8 kW)
			assert.LessOrEqual(t, model[0].AvgHomeLoadKWH, 2.0)

			simData, _ := c.SimulateState(ctx, evNow, evStatus, evCurrentPrice, evFuturePrices, evHistory, nil, settingsWithEV)
			require.GreaterOrEqual(t, len(simData), 24)

			// Battery should not be drained to 0 during midnight hours
			for _, hour := range simData {
				if hour.Hour >= 0 && hour.Hour <= 5 {
					assert.GreaterOrEqual(t, hour.BatteryKWH, 3.5)
				}
			}
		})

		t.Run("WithEVChargingPeriods_ClampedToHourSpecificBaseline", func(t *testing.T) {
			// History where night baseline is 1.5 kW (due to nighttime AC), but midday has 0.1 kW standby
			var hist []types.EnergyStats
			for d := 0; d < 14; d++ {
				dayStart := startHist.AddDate(0, 0, d)
				for h := 0; h < 24; h++ {
					hStart := dayStart.Add(time.Duration(h) * time.Hour)
					load := 1.5 // nighttime baseline
					if h >= 10 && h <= 16 {
						load = 0.1 // empty house standby during day
					}
					if h >= 0 && h <= 3 {
						load = 11.5 // 4-hour EV charge
					}
					hist = append(hist, types.EnergyStats{
						TSHourStart: hStart,
						HomeKWH:     load,
					})
				}
			}

			settingsWithEV := types.Settings{
				MinBatterySOC: 20.0,
				EVChargingPeriods: []types.TimePeriod{
					{
						Name: "Night EV Charging",
						Hours: []types.UtilityHourPeriod{
							{
								HourStart: 23,
								HourEnd:   6,
							},
						},
					},
				},
			}

			model, _ := c.BuildHourlyEnergyModel(ctx, evNow, hist, nil, settingsWithEV)
			// For hour 2 (which is hour 3 of the EV charge), it looks back earlier to 23:00 (1.5 kW)
			// and uses ~1.5 kW instead of 0.1 kW site minimum!
			assert.InDelta(t, 1.5, model[2].AvgHomeLoadKWH, 0.3)
		})
	})
}
