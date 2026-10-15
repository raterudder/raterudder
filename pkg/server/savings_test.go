package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSavingsStorage struct {
	*mockStorage
	settings  types.Settings
	prices    []types.Price
	stats     []types.EnergyStats
	actions   []types.Action
	summaries []types.HistorySummary
}

func (m *mockSavingsStorage) GetHistorySummaries(ctx context.Context, siteID string, start, end time.Time) ([]types.HistorySummary, error) {
	return m.summaries, nil
}

func (m *mockSavingsStorage) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	return m.prices, nil
}

func (m *mockSavingsStorage) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.DailyEnergyStats, error) {
	dailyMap := make(map[string][]types.EnergyStats)
	var sortedDayKeys []string

	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	for _, s := range m.stats {
		dayStart := time.Date(s.TSHourStart.Year(), s.TSHourStart.Month(), s.TSHourStart.Day(), 0, 0, 0, 0, s.TSHourStart.Location())
		if dayStart.Before(startDay) || dayStart.After(endDay) {
			continue
		}
		dayKey := dayStart.Format("2006-01-02")
		if _, exists := dailyMap[dayKey]; !exists {
			sortedDayKeys = append(sortedDayKeys, dayKey)
		}
		dailyMap[dayKey] = append(dailyMap[dayKey], s)
	}

	var result []types.DailyEnergyStats
	for _, key := range sortedDayKeys {
		dayStart, err := time.ParseInLocation("2006-01-02", key, time.Local)
		if err != nil {
			return nil, fmt.Errorf("failed to parse day key %s: %v", key, err)
		}
		result = append(result, types.DailyEnergyStats{
			TSDayStart: dayStart,
			Hourly:     dailyMap[key],
		})
	}
	return result, nil
}

func (m *mockSavingsStorage) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	var result []types.Action
	for _, a := range m.actions {
		if !a.Timestamp.Before(start) && a.Timestamp.Before(end) {
			result = append(result, a)
		}
	}
	return result, nil
}

func TestHandleHistorySavings(t *testing.T) {
	start := time.Now().Truncate(24 * time.Hour).UTC()
	end := start.Add(24 * time.Hour).UTC()

	runTest := func(t *testing.T, setupMock func(*mockSavingsStorage)) types.SavingsStats {
		mockStoreBase := &mockStorage{}
		mockStoreBase.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			GridExportSolar: false, // Default to false as it was before
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockStore := &mockSavingsStorage{mockStorage: mockStoreBase}

		setupMock(mockStore)

		mockUtility := &mockUtility{}
		mockUtility.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockUtility.On("ApplySettings", mock.Anything, mock.AnythingOfType("types.Settings")).Return(nil)

		mockUtilities := utility.NewMap(mockStore)
		mockUtilities.SetProvider(types.SiteIDNone, mockUtility)

		s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true}

		req, err := http.NewRequest("GET", "/api/history/savings?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
		require.NoError(t, err)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		var savings types.SavingsStats
		err = json.Unmarshal(rr.Body.Bytes(), &savings)
		require.NoError(t, err)

		return savings
	}

	t.Run("Basic Charge and Discharge", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10},                    // H1: $0.10
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20}, // H2: $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh @ $0.10 = $1.00
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start.Add(time.Hour), // Discharge 5kWh @ $0.20 = $1.00 avoided
					HomeKWH:          5,
					BatteryUsedKWH:   5,
					BatteryToHomeKWH: 5,
				},
			}
		})
		assert.InDelta(t, 1.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Sub-Hourly Dynamic Price Split", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				// Split the first hour [start, start+1h] at 30 minutes
				{TSStart: start, TSEnd: start.Add(30 * time.Minute), DollarsPerKWH: 0.10},
				{TSStart: start.Add(30 * time.Minute), TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.30},
				// Second hour is standard hourly pricing
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.40},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh in this split hour
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start.Add(time.Hour), // Discharge 5kWh in the second hour
					HomeKWH:          5,
					BatteryUsedKWH:   5,
					BatteryToHomeKWH: 5,
				},
			}
		})
		// First hour: 10kWh split into 5kWh @ $0.10 ($0.50) and 5kWh @ $0.30 ($1.50) = $2.00 total cost
		assert.InDelta(t, 2.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")

		// Second hour avoided cost: 5kWh * $0.40 = $2.00
		assert.InDelta(t, 2.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")

		// Charging cost: LIFO pops the last charged segment first (which is 12:30-13:00 charge @ $0.30)
		// So 5kWh * $0.30 = $1.50 charging cost
		assert.InDelta(t, 1.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Charge Only - No Battery Savings Penalty", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10}, // H1: $0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh @ $0.10 = $1.00
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
			}
		})
		assert.InDelta(t, 1.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Discharge Only (with 24h lookback stack)", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			lookbackStart := start.Add(-2 * time.Hour) // Past charge
			m.prices = []types.Price{
				{TSStart: lookbackStart, TSEnd: lookbackStart.Add(time.Hour), DollarsPerKWH: 0.05}, // Past charge @ $0.05
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                 // Current discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       lookbackStart, // Charge 10kWh @ $0.05 in past
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start, // Discharge 10kWh @ $0.20 in current period
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 2.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 1.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("LIFO Stack Multiple Charges", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.05}, // Past charge 1 @ $0.05
				{TSStart: start.Add(-1 * time.Hour), TSEnd: start, DollarsPerKWH: 0.10},                     // Past charge 2 @ $0.10
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Current discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-2 * time.Hour), // Charge 10kWh @ $0.05
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:       start.Add(-1 * time.Hour), // Charge 10kWh @ $0.10
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start, // Discharge 15kWh @ $0.20
					HomeKWH:          15,
					BatteryUsedKWH:   15,
					BatteryToHomeKWH: 15,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 3.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 1.25, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 1.75, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Partial Paused Hour", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20}, // H1: $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:      start, // Discharged 10kWh, but 30 mins were paused
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(30 * time.Minute), Paused: true}, // Paused half way through
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 1.00, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Storm Hedge Ignoring", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10}, // H1: $0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start, // Charge 10kWh @ $0.10, but in storm
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-1 * time.Minute), Reason: types.ActionReasonEmergencyMode},
			}
		})
		assert.InDelta(t, 1.00, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Storm Charging Then Discharge", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.15}, // Heavy charge @ $0.15 during storm
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Normal discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-2 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start,
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-125 * time.Minute), Reason: types.ActionReasonEmergencyMode}, // Storm started before charge
				{Timestamp: start.Add(-1 * time.Minute), Reason: "normal"},                          // Storm ended before discharge
			}
		})
		// Avoided: 0.0 (because it was all storm energy)
		// ChargingCost: 0.0 (because it was all storm energy)
		// BatterySavings: 0.0
		assert.InDelta(t, 0.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.00, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.00, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Mixed Energy Discharge", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-3 * time.Hour), TSEnd: start.Add(-2 * time.Hour), DollarsPerKWH: 0.10}, // Active charge @ $0.10
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.15}, // Storm charge @ $0.15
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Normal discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-3 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:       start.Add(-2 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:      start,
					HomeKWH:          15,
					BatteryUsedKWH:   15,
					BatteryToHomeKWH: 15,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-2 * time.Hour), Reason: types.ActionReasonEmergencyMode}, // Storm started exactly at second charge hour
				{Timestamp: start, Reason: "normal"},                                            // Storm ended exactly at discharge hour
			}
		})
		// Inventory: 10kWh active ($1.00), 10kWh ignored (atop LIFO)
		// Discharge 15kWh:
		// - 10kWh ignored (from storm) -> excluded
		// - 5kWh active (from first charge) -> counted
		// EffBatteryToHome = 15 - 10 = 5kWh
		// Avoided: 5kWh * $0.20 = $1.00
		// ChargingCost: 5kWh * $0.10 = $0.50
		// BatterySavings: $0.50
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Mixed Energy Discharge With Partial Pause", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start.Add(-2 * time.Hour), TSEnd: start.Add(-1 * time.Hour), DollarsPerKWH: 0.10}, // Active charge @ $0.10
				{TSStart: start.Add(-1 * time.Hour), TSEnd: start, DollarsPerKWH: 0.15},                     // Storm charge @ $0.15
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},                          // Discharge @ $0.20
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start.Add(-2 * time.Hour),
					GridImportKWH:     10,
					BatteryChargedKWH: 10,
				},
				{
					TSHourStart:       start.Add(-1 * time.Hour),
					GridImportKWH:     5,
					BatteryChargedKWH: 5,
				},
				{
					TSHourStart:      start,
					HomeKWH:          10,
					BatteryUsedKWH:   10,
					BatteryToHomeKWH: 10,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start.Add(-1 * time.Hour), Reason: types.ActionReasonEmergencyMode},
				{Timestamp: start, Reason: "normal"},
				{Timestamp: start.Add(45 * time.Minute), Paused: true}, // 15 mins paused (activeFraction = 0.75)
			}
		})
		// 10kWh discharged: 5kWh storm (ignored), 5kWh active.
		// Paused fraction: 15 mins = 2.5kWh.
		// Total ignored is max(2.5, 5) = 5kWh, so effBatteryToHome is 10 - 5 = 5kWh (not double discounted to 3.75kWh).
		// Avoided: 5kWh * $0.20 = $1.00
		// ChargingCost: 5kWh * $0.10 = $0.50
		// BatterySavings: $0.50
		assert.InDelta(t, 1.00, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.50, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.50, savings.BatterySavings, 0.001, "BatterySavings mismatch")
	})

	t.Run("Solar Savings", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10}, // H1: $0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:    start,
					SolarKWH:       10,
					SolarToHomeKWH: 10,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 1.00, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Grid Use Fees Included in Import", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{
					TSStart:              start,
					TSEnd:                start.Add(time.Hour),
					DollarsPerKWH:        0.10,
					GridUseDollarsPerKWH: 0.05,
				}, // Import: 0.15, Export: 0.10
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridImportKWH: 10,
				},
			}
		})
		assert.InDelta(t, 1.50, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Default Export Price (no Grid Use)", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{
					TSStart:              start,
					TSEnd:                start.Add(time.Hour),
					DollarsPerKWH:        0.10,
					GridUseDollarsPerKWH: 0.05,
				},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 1.00, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Net Metering Credits - Highest", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.02},                        // H0: 0.12
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.02},     // H1: 0.22
				{TSStart: start.Add(2 * time.Hour), TSEnd: start.Add(3 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.02}, // H2: 0.07
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringCredits: true,
				},
				SolarNetMeteringCreditsValue: "highest",
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 2.20, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Net Metering Credits - Lowest", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.02},                        // H0: 0.12
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.20, GridUseDollarsPerKWH: 0.02},     // H1: 0.22
				{TSStart: start.Add(2 * time.Hour), TSEnd: start.Add(3 * time.Hour), DollarsPerKWH: 0.05, GridUseDollarsPerKWH: 0.02}, // H2: 0.07
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringCredits: true,
				},
				SolarNetMeteringCreditsValue: "lowest",
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.70, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Net Metering Credits - None", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.02}, // H0: 0.12
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringCredits: true,
				},
				SolarNetMeteringCreditsValue: "none",
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch") // Should be 0 based on simulation logic
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Separate Generation Credit", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{
					TSStart:                       start,
					TSEnd:                         start.Add(time.Hour),
					DollarsPerKWH:                 0.10,
					GridUseDollarsPerKWH:          0.05,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.08,
				},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.80, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Grid Export Solar Disabled", func(t *testing.T) {
		savings := runTest(t, func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					GridExportKWH: 10,
				},
			}
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Unset()
			m.mockStorage.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				GridExportSolar: false,
			}, types.CurrentSettingsVersion, time.Time{}, nil)
		})
		assert.InDelta(t, 0.0, savings.Cost, 0.001, "Cost mismatch")
		assert.InDelta(t, 0.0, savings.Credit, 0.001, "Credit mismatch")
		assert.InDelta(t, 0.0, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
		assert.InDelta(t, 0.0, savings.ChargingCost, 0.001, "ChargingCost mismatch")
		assert.InDelta(t, 0.0, savings.BatterySavings, 0.001, "BatterySavings mismatch")
		assert.InDelta(t, 0.0, savings.SolarSavings, 0.001, "SolarSavings mismatch")
	})

	t.Run("Invalid Time Range", func(t *testing.T) {
		mockStore := &mockSavingsStorage{mockStorage: &mockStorage{}}
		s := &Server{storage: mockStore, bypassAuth: true}

		req, _ := http.NewRequest("GET", "/api/history/savings?start=invalid&end=invalid", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("Storage Error Propagated", func(t *testing.T) {
		mockStore := &mockStorage{}
		mockStore.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		// We explicitly assert the lookback (-24 hours) adjustment instead of using mock.AnythingOfType("time.Time")
		mockStore.On("GetPriceHistory", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(start.AddDate(0, 0, -1))
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(end)
		})).Return([]types.Price(nil), errors.New("db error"))

		mockUtilities := utility.NewMap(mockStore)
		s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true}

		req, _ := http.NewRequest("GET", "/api/history/savings?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	runNewCalcTest := func(t *testing.T, now time.Time, setupMock func(*mockSavingsStorage)) types.SavingsStats {
		mockStoreBase := &mockStorage{}
		mockStore := &mockSavingsStorage{
			mockStorage: mockStoreBase,
			settings: types.Settings{
				GridExportSolar: false,
				MinBatterySOC:   20.0,
			},
			actions: []types.Action{
				{
					Timestamp:    start,
					SystemStatus: types.SystemStatus{BatteryCapacityKWH: 10.0},
				},
			},
		}

		setupMock(mockStore)
		if len(mockStore.actions) > 0 {
			hasCap := false
			for _, a := range mockStore.actions {
				if a.SystemStatus.BatteryCapacityKWH > 0 {
					hasCap = true
					break
				}
			}
			if !hasCap {
				mockStore.actions[0].SystemStatus.BatteryCapacityKWH = 10.0
			}
		}
		mockStoreBase.On("GetSettings", mock.Anything, types.SiteIDNone).Return(mockStore.settings, types.CurrentSettingsVersion, time.Time{}, nil)

		mockUtility := &mockUtility{}
		mockUtility.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockUtility.On("ApplySettings", mock.Anything, mock.AnythingOfType("types.Settings")).Return(nil)

		mockUtilities := utility.NewMap(mockStore)
		mockUtilities.SetProvider(types.SiteIDNone, mockUtility)

		s := &Server{
			storage:    mockStore,
			utilities:  mockUtilities,
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req, err := http.NewRequest("GET", "/api/history/savings?newCalculation=true&start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
		require.NoError(t, err)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		rr := httptest.NewRecorder()

		s.handleHistorySavings(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		var savings types.SavingsStats
		err = json.Unmarshal(rr.Body.Bytes(), &savings)
		require.NoError(t, err)

		return savings
	}

	t.Run("NewCalculation Arbitrage vs Self-Consumption", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.05},
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.30},
			}
			// 5 kWh charge across 50% SOC implies a 10 kWh battery capacity.
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					HomeKWH:           1,
					GridImportKWH:     6,
					BatteryChargedKWH: 5,
				},
				{
					TSHourStart:      start.Add(time.Hour),
					MinBatterySOC:    20,
					MaxBatterySOC:    70,
					HomeKWH:          4.5,
					GridImportKWH:    0,
					BatteryUsedKWH:   4.5,
					BatteryToHomeKWH: 4.5,
				},
			}
		})
		// Grid-only cost: 1 * $0.05 + 4.5 * $0.30 = $1.40
		assert.InDelta(t, 1.40, savings.GridOnlyCostDollars, 0.001)
		// Shadow self-consumption had no solar and started at 20% reserve, so it imported 1 kWh @ $0.05 and 4.5 kWh @ $0.30 = $1.40
		assert.InDelta(t, 1.40, savings.SelfConsumptionCostDollars, 0.001)
		assert.InDelta(t, 0.0, savings.SolarAndBatterySavingsDollars, 0.001)
		// Actual cost: 6 kWh @ $0.05 = $0.30
		assert.InDelta(t, 0.30, savings.Cost, 0.001)
		assert.InDelta(t, 1.10, savings.RateRudderSavingsDollars, 0.001)
		assert.InDelta(t, 1.10, savings.TotalSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Curtails Solar When GridExportSolar Disabled", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},
			}
			// Battery starts at 90% SOC and charges 1 kWh to 100% (10 kWh capacity).
			// Solar generates 5 kWh, home uses 1 kWh -> 4 kWh excess solar, ~1 kWh goes to battery, ~3 kWh curtailed.
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start,
					MinBatterySOC:     90,
					MaxBatterySOC:     100,
					HomeKWH:           1,
					SolarKWH:          5,
					SolarToHomeKWH:    1,
					SolarToBatteryKWH: 1,
					BatteryChargedKWH: 1,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.SelfConsumptionCreditDollars, 0.001)
		assert.InDelta(t, 0.20, savings.SolarAndBatterySavingsDollars, 0.001)
	})

	t.Run("NewCalculation Stored Energy Valuation Across Day Boundary", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.04},
				// Next day price for stored energy valuation
				{TSStart: end, TSEnd: end.Add(time.Hour), DollarsPerKWH: 0.20},
			}
			m.stats = []types.EnergyStats{
				{
					// RateRudder charges 5 kWh (20% -> 70% on 10 kWh battery) @ $0.04/kWh ($0.20) and holds it past midnight
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					HomeKWH:           1,
					GridImportKWH:     6,
					BatteryChargedKWH: 5,
				},
				{
					// Next day hour with home load at $0.20/kWh
					TSHourStart:   end,
					MinBatterySOC: 20,
					MaxBatterySOC: 70,
					HomeKWH:       5,
				},
			}
		})
		// Cash cost delta on Day 1: -$0.20 (5 kWh * $0.04)
		// Stored energy value at end of Day 1: +5 kWh * $0.20 * sqrt(0.90) = +$0.9487
		// Net RateRudder savings: -$0.20 + $0.9487 = +$0.7487
		expectedStoredVal := 1.00 * math.Sqrt(0.90)
		assert.InDelta(t, expectedStoredVal, savings.StoredEnergyValueDollars, 0.001)
		assert.InDelta(t, expectedStoredVal-0.20, savings.RateRudderSavingsDollars, 0.001)
	})

	t.Run("NewCalculation In-Progress Today Uses Plan For Stored Energy Valuation", func(t *testing.T) {
		savings := runNewCalcTest(t, start.Add(time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.05},
			}
			m.stats = []types.EnergyStats{
				{
					// Hour 0: RateRudder grid-charges 5 kWh (20% -> 70% on 10 kWh battery) @ $0.05/kWh ($0.25) while home uses 1 kWh
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					HomeKWH:           1,
					GridImportKWH:     6,
					BatteryChargedKWH: 5,
				},
			}
			m.actions = []types.Action{
				{
					Timestamp:    start.Add(time.Hour),
					SystemStatus: types.SystemStatus{BatteryCapacityKWH: 10.0},
					Plan: &types.Plan{
						Periods: []types.PlanPeriod{
							{
								TSStart:       start.Add(time.Hour),
								TSEnd:         start.Add(2 * time.Hour),
								DurationHours: 1.0,
								ImportDollars: 0.25,
								ExportDollars: 0.10,
								BatteryMode:   types.BatteryModeLoad,
								SolarMode:     types.SolarModeAny,
								StartSOC:      70,
								EndSOC:        20,
								ReserveSOC:    20,
								LoadKWH:       5,
								SolarKWH:      0,
								GridImportKWH: 0,
							},
						},
					},
				},
			}
		})
		assert.False(t, savings.Estimated)
		// Only actuals so far today (Hour 0: 1.0 kWh) are included in HomeUsed
		assert.InDelta(t, 1.0, savings.HomeUsed, 0.001)
		// Stored +5 kWh is valued at upcoming plan period's $0.25/kWh * sqrt(0.90) = $1.1859, offsetting the $0.25 morning charge cost -> +$0.9359 savings
		expectedStoredVal := 1.25 * math.Sqrt(0.90)
		assert.InDelta(t, expectedStoredVal, savings.StoredEnergyValueDollars, 0.001)
		assert.InDelta(t, expectedStoredVal-0.25, savings.RateRudderSavingsDollars, 0.001)
		assert.InDelta(t, expectedStoredVal-0.25, savings.TotalSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Missing BatteryCapacityKWH Returns Zero Stats", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.actions = nil
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.25},
			}
			m.stats = []types.EnergyStats{
				{
					TSHourStart:    start,
					MinBatterySOC:  50,
					MaxBatterySOC:  50,
					HomeKWH:        5.0,
					SolarKWH:       5.0,
					SolarToHomeKWH: 5.0,
				},
			}
		})
		assert.Equal(t, 0.0, savings.TotalSavingsDollars)
		assert.Equal(t, 0.0, savings.SolarAndBatterySavingsDollars)
		assert.Equal(t, 0.0, savings.RateRudderSavingsDollars)
	})

	t.Run("NewCalculation GridExportSolar Enabled", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.MinBatterySOC = 20.0
			m.settings.GridExportSolar = true
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, GridUseDollarsPerKWH: 0.05},
			}
			// Battery starts at 100% SOC; 5 kWh excess solar is exported in both actual and shadow
			m.stats = []types.EnergyStats{
				{
					TSHourStart:    start,
					MinBatterySOC:  100,
					MaxBatterySOC:  100,
					HomeKWH:        2.0,
					SolarKWH:       7.0,
					SolarToHomeKWH: 2.0,
					SolarToGridKWH: 5.0,
					GridExportKWH:  5.0,
				},
			}
		})
		// Export rate = $0.10/kWh -> 5 kWh * $0.10 = $0.50 credit in both actual and shadow
		assert.InDelta(t, 0.50, savings.SelfConsumptionCreditDollars, 0.001)
		// GridOnlyCost = 2.0 * $0.15 = $0.30; SelfConsumptionNetCost = $0.00 - $0.50 = -$0.50 -> Savings = $0.80
		assert.InDelta(t, 0.80, savings.SolarAndBatterySavingsDollars, 0.001)
		assert.InDelta(t, 0.80, savings.TotalSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Variable Reserve Convergence Regression", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.MinBatterySOC = 20.0
			// Variable reserve raises minSOC to 50% during "peak"
			m.settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "peak", MinBatterySOC: 50.0},
			}
			m.actions = []types.Action{
				{Timestamp: start, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 15.0}},
			}
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, PeriodName: "peak"},
				{TSStart: end, TSEnd: end.Add(time.Hour), DollarsPerKWH: 0.20, PeriodName: "peak"},
			}
			// Actual battery charges from 20% to 50% (+4.5 kWh on 15 kWh battery) from grid while shadow stays at 20%
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     50,
					BatteryChargedKWH: 4.5,
					GridImportKWH:     4.5,
				},
				{
					TSHourStart:   end,
					MinBatterySOC: 50,
					MaxBatterySOC: 50,
					HomeKWH:       2.0,
					GridImportKWH: 2.0,
				},
			}
		})
		// Actual has +30% (+4.5 kWh) more stored energy than shadow at end of day, valued at next day's $0.20/kWh * sqrt(0.90) = $0.8538.
		// If shadowSOC had falsely snapped from 20% to 50%, StoredEnergyValueDollars would be $0.00!
		assert.InDelta(t, 0.90*math.Sqrt(0.90), savings.StoredEnergyValueDollars, 0.01)
	})

	t.Run("NewCalculation Paused Arbitrage Zeroes RateRudder Savings", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.05},
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.30},
			}
			// User paused RateRudder before start to run vendor TOU/AI mode
			m.actions = []types.Action{
				{Timestamp: start.Add(-time.Hour), Paused: true},
			}
			m.stats = []types.EnergyStats{
				{
					// Hour 0: Vendor mode grid-charges 5 kWh at $0.05 ($0.25)
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					BatteryChargedKWH: 5.0,
					GridImportKWH:     5.0,
				},
				{
					// Hour 1: Vendor mode discharges 5 kWh to cover 5 kWh peak load at $0.30 ($1.50 avoided)
					TSHourStart:      start.Add(time.Hour),
					MinBatterySOC:    20,
					MaxBatterySOC:    70,
					BatteryUsedKWH:   5.0,
					BatteryToHomeKWH: 5.0,
					HomeKWH:          5.0,
				},
			}
		})
		// Because RateRudder was paused, RateRudderSavingsDollars must be $0.00
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.001)
		// Hardware still saved $1.50 (grid-only) - $0.25 (actual cost) = $1.25
		assert.InDelta(t, 1.25, savings.SolarAndBatterySavingsDollars, 0.001)
		assert.InDelta(t, 1.25, savings.TotalSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Emergency Mode Storm Charge Not Penalized", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.30},
			}
			m.actions = []types.Action{
				{Timestamp: start, Reason: types.ActionReasonEmergencyMode},
			}
			m.stats = []types.EnergyStats{
				{
					// Emergency mode forces 5 kWh grid charge at expensive $0.30/kWh ($1.50)
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					BatteryChargedKWH: 5.0,
					GridImportKWH:     5.0,
				},
			}
		})
		// Emergency mode is ignored so RateRudder is not blamed for the $1.50 storm charge
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.001)
		assert.InDelta(t, 0.0, savings.StoredEnergyValueDollars, 0.001)
	})

	t.Run("NewCalculation Per-Day Cache Rollup Equivalence", func(t *testing.T) {
		day1Start := start.Add(-24 * time.Hour)
		day2Start := start
		day2End := end

		mockStoreBase := &mockStorage{}
		mockStoreBase.On("GetSettings", mock.Anything, "test-site").Return(types.Settings{MinBatterySOC: 20, GridExportSolar: true}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockStore := &mockSavingsStorage{
			mockStorage: mockStoreBase,
			settings:    types.Settings{MinBatterySOC: 20, GridExportSolar: true},
			actions: []types.Action{
				{Timestamp: day1Start, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 10.0}},
			},
			prices: []types.Price{
				{TSStart: day1Start, TSEnd: day1Start.Add(time.Hour), DollarsPerKWH: 0.05},
				{TSStart: day1Start.Add(time.Hour), TSEnd: day1Start.Add(2 * time.Hour), DollarsPerKWH: 0.25},
				{TSStart: day2Start, TSEnd: day2Start.Add(time.Hour), DollarsPerKWH: 0.20},
				{TSStart: day2End, TSEnd: day2End.Add(time.Hour), DollarsPerKWH: 0.15},
			},
			stats: []types.EnergyStats{
				{
					// Day 1 Hour 0: RateRudder grid-charges 5 kWh at $0.05 ($0.25)
					TSHourStart:       day1Start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					BatteryChargedKWH: 5.0,
					GridImportKWH:     5.0,
				},
				{
					// Day 2 Hour 0: RateRudder discharges 3 kWh into 5 kWh load at $0.20 (leaving +2 kWh in battery)
					TSHourStart:      day2Start,
					MinBatterySOC:    40,
					MaxBatterySOC:    70,
					BatteryUsedKWH:   3.0,
					BatteryToHomeKWH: 3.0,
					HomeKWH:          5.0,
					GridImportKWH:    2.0,
				},
				{
					// Day 3 Hour 0 (lookahead): 4 kWh load at $0.15
					TSHourStart:   day2End,
					MinBatterySOC: 40,
					MaxBatterySOC: 40,
					HomeKWH:       4.0,
					GridImportKWH: 4.0,
				},
			},
		}

		srv := &Server{
			storage:   mockStore,
			utilities: utility.NewMap(mockStore),
			nowFunc:   func() time.Time { return day2End.Add(24 * time.Hour) },
		}

		periods := []savingsPeriod{
			{Start: day1Start, End: day2Start}, // Day 1
			{Start: day2Start, End: day2End},   // Day 2
			{Start: day1Start, End: day2End},   // Combined 2-Day Window
		}
		res, _, err := srv.getSiteSavingsNew(context.Background(), "test-site", day2Start, day2End, periods)
		require.NoError(t, err)
		require.Len(t, res, 3)

		day1 := res[0]
		day2 := res[1]
		twoDay := res[2]
		oneWayEff := math.Sqrt(0.90)

		assert.InDelta(t, 5.0*0.20*oneWayEff, day1.StoredEnergyValueDollars, 0.001)
		assert.InDelta(t, -3.0*0.15*oneWayEff, day2.StoredEnergyValueDollars, 0.001)

		trackers := []dailySavingsTracker{
			{
				period:                        savingsPeriod{Start: day1Start, End: day2Start},
				stats:                         day1,
				batteryCapacityKWH:            10.0,
				storedEnergyDeltaKWH:          5.0,
				nextDayValuationDollarsPerKWH: 0.20 * oneWayEff,
			},
			{
				period:                        savingsPeriod{Start: day2Start, End: day2End},
				stats:                         day2,
				batteryCapacityKWH:            10.0,
				storedEnergyDeltaKWH:          -3.0,
				nextDayValuationDollarsPerKWH: 0.15 * oneWayEff,
			},
		}

		// Rolling up daily trackers directly produces the exact same 2-day stats
		rolledUp := rollupDailySavingsStats(savingsPeriod{Start: day1Start, End: day2End}, trackers)
		assert.InDelta(t, twoDay.StoredEnergyValueDollars, rolledUp.StoredEnergyValueDollars, 0.0001)
		assert.InDelta(t, twoDay.RateRudderSavingsDollars, rolledUp.RateRudderSavingsDollars, 0.0001)
		assert.InDelta(t, twoDay.SolarAndBatterySavingsDollars, rolledUp.SolarAndBatterySavingsDollars, 0.0001)
		assert.InDelta(t, twoDay.TotalSavingsDollars, rolledUp.TotalSavingsDollars, 0.0001)

		// Also verify rollupDailySavingsStats matches when period End is 23:59:59 (1 second before day2End)
		rolledUpEndOfDay := rollupDailySavingsStats(savingsPeriod{Start: day1Start, End: day2End.Add(-time.Second)}, trackers)
		assert.InDelta(t, twoDay.TotalSavingsDollars, rolledUpEndOfDay.TotalSavingsDollars, 0.0001)
	})

	t.Run("NewCalculation Pause Boundary Transition Preserves Pre-Pause Charge", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.05},
				{TSStart: start.Add(time.Hour), TSEnd: start.Add(2 * time.Hour), DollarsPerKWH: 0.10},
				{TSStart: start.Add(2 * time.Hour), TSEnd: start.Add(3 * time.Hour), DollarsPerKWH: 0.30},
			}
			// Active in Hour 0, paused in Hour 1, unpaused in Hour 2
			m.actions = []types.Action{
				{Timestamp: start, Paused: false},
				{Timestamp: start.Add(time.Hour), Paused: true},
				{Timestamp: start.Add(2 * time.Hour), Paused: false},
			}
			m.stats = []types.EnergyStats{
				{
					// Hour 0 (Active): RateRudder grid-charges 5 kWh at $0.05 ($0.25)
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					BatteryChargedKWH: 5.0,
					GridImportKWH:     5.0,
				},
				{
					// Hour 1 (Paused): Battery sits idle at 70%
					TSHourStart:   start.Add(time.Hour),
					MinBatterySOC: 70,
					MaxBatterySOC: 70,
				},
				{
					// Hour 2 (Unpaused): Battery discharges 5 kWh to cover 5 kWh peak load at $0.30 ($1.50 avoided)
					TSHourStart:      start.Add(2 * time.Hour),
					MinBatterySOC:    20,
					MaxBatterySOC:    70,
					BatteryUsedKWH:   5.0,
					BatteryToHomeKWH: 5.0,
					HomeKWH:          5.0,
				},
			}
		})
		// Battery starts and ends at 20%, so StoredEnergyValueDollars must be 0.0
		assert.InDelta(t, 0.0, savings.StoredEnergyValueDollars, 0.001)
		// RateRudder saved $1.50 (avoided peak import) - $0.25 (off-peak charge) = $1.25
		assert.InDelta(t, 1.25, savings.RateRudderSavingsDollars, 0.001)
		assert.InDelta(t, 1.25, savings.TotalSavingsDollars, 0.001)
	})

	t.Run("NewCalculation VPP Event On Low Export Rate Site Does Not Hurt RateRudder", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.GridExportSolar = true
			m.prices = []types.Price{
				{
					TSStart:                       start,
					TSEnd:                         start.Add(time.Hour),
					DollarsPerKWH:                 0.15,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.02,
				},
				{
					TSStart:                       start.Add(time.Hour),
					TSEnd:                         start.Add(2 * time.Hour),
					DollarsPerKWH:                 0.20,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.02,
				},
				{
					TSStart:                       start.Add(2 * time.Hour),
					TSEnd:                         start.Add(3 * time.Hour),
					DollarsPerKWH:                 0.35,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.02,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start, Reason: types.ActionReasonSufficientBattery},
				{Timestamp: start.Add(time.Hour), Reason: types.ActionReasonVPPActive, SystemStatus: types.SystemStatus{VPPActive: true}},
				{Timestamp: start.Add(2 * time.Hour), Reason: types.ActionReasonBatteryAtReserve},
			}
			m.stats = []types.EnergyStats{
				{
					// Hour 0: Solar charges battery from 20% -> 70% (5 kWh stored at oneWayEff = sqrt(0.90))
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					SolarKWH:          5.0 / math.Sqrt(0.90),
					SolarToBatteryKWH: 5.0 / math.Sqrt(0.90),
					BatteryChargedKWH: 5.0,
				},

				{
					// Hour 1 (VPP Active): Battery dumps 5 kWh to grid during VPP event (20% SOC left)
					TSHourStart:      start.Add(time.Hour),
					MinBatterySOC:    20,
					MaxBatterySOC:    70,
					BatteryUsedKWH:   5.0,
					BatteryToGridKWH: 5.0,
					GridExportKWH:    5.0,
					VPPExportKWH:     5.0,
				},
				{
					// Hour 2 (Evening Peak $0.35): Drained battery must import 5 kWh for home load
					TSHourStart:   start.Add(2 * time.Hour),
					MinBatterySOC: 20,
					MaxBatterySOC: 20,
					HomeKWH:       5.0,
					GridImportKWH: 5.0,
				},
			}
		})
		// Because VPP dump mirrored shadowSOC down to 20%, RateRudder is not penalized for the evening import
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.01)
		assert.InDelta(t, 0.0, savings.StoredEnergyValueDollars, 0.01)
	})

	t.Run("NewCalculation VPP Off-Peak Pre-Charge Credits Rate Spread", func(t *testing.T) {
		oneWayEff := math.Sqrt(0.90)
		gridImportFor5KWHStored := 5.0 / oneWayEff
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.GridExportSolar = true
			m.prices = []types.Price{
				{
					TSStart:                       start,
					TSEnd:                         start.Add(time.Hour),
					DollarsPerKWH:                 0.05,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.02,
				},
				{
					TSStart:                       start.Add(time.Hour),
					TSEnd:                         start.Add(2 * time.Hour),
					DollarsPerKWH:                 0.35,
					SeparateGenerationCredit:      true,
					GenerationCreditDollarsPerKWH: 0.02,
				},
			}
			m.actions = []types.Action{
				{Timestamp: start, Reason: types.ActionReasonVPPPrep},
				{Timestamp: start.Add(time.Hour), Reason: types.ActionReasonVPPActive, SystemStatus: types.SystemStatus{VPPActive: true}},
			}
			m.stats = []types.EnergyStats{
				{
					// Hour 0 (Off-Peak $0.05): RateRudder pre-charges battery from 20% -> 70% (+5 kWh stored)
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     70,
					GridImportKWH:     gridImportFor5KWHStored,
					BatteryChargedKWH: 5.0,
				},
				{
					// Hour 1 (VPP Active $0.35): Battery dumps 5 kWh to grid during VPP event (70% -> 20% SOC)
					TSHourStart:      start.Add(time.Hour),
					MinBatterySOC:    20,
					MaxBatterySOC:    70,
					BatteryUsedKWH:   5.0,
					BatteryToGridKWH: 5.0,
					GridExportKWH:    5.0,
					VPPExportKWH:     5.0,
				},
			}
		})
		// Both batteries exit at 20% SOC with 5 kWh exported; RateRudder paid $0.05/kWh to pre-charge while
		// shadow self-consumption was forced to buy that 5 kWh stored (5.0 / sqrt(0.90) kWh grid import) at $0.35/kWh.
		expectedSpread := gridImportFor5KWHStored * (0.35 - 0.05)
		assert.InDelta(t, 0.0, savings.StoredEnergyValueDollars, 0.01)
		assert.InDelta(t, expectedSpread, savings.RateRudderSavingsDollars, 0.01)
	})

	t.Run("NewCalculation Historical Reserve Inferred From Actions Ignores Temporary Spike", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			// Current settings were later changed to a 90% overnight period, which would falsely lock
			// the shadow battery at 90% if evaluated retroactively via settings.GetMinBatterySOC.
			m.settings.MinBatterySOC = 20.0
			m.settings.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "offpeak", MinBatterySOC: 90.0},
			}
			// Historical actions recorded a normal 20% reserve for most of the day and only a brief 90% spike.
			m.actions = []types.Action{
				{Timestamp: start, ReserveSOC: 90.0, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 10.0}},
				{Timestamp: start.Add(time.Hour), ReserveSOC: 20.0, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 10.0}},
				{Timestamp: start.Add(2 * time.Hour), ReserveSOC: 20.0, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 10.0}},
			}
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.10, PeriodName: "offpeak"},
			}
			// Battery starts at 50% SOC (5 kWh) and discharges 2 kWh down to 30% SOC to cover 2 kWh load.
			// With the inferred 20% daily baseline reserve (instead of 90%), the shadow battery also
			// discharges down to 30% SOC rather than being locked at 50% and importing from the grid.
			m.stats = []types.EnergyStats{
				{
					TSHourStart:      start,
					MinBatterySOC:    30,
					MaxBatterySOC:    50,
					HomeKWH:          2.0 * math.Sqrt(0.90),
					BatteryUsedKWH:   2.0,
					BatteryToHomeKWH: 2.0 * math.Sqrt(0.90),
				},
			}
		})
		assert.InDelta(t, 0.0, savings.SelfConsumptionCostDollars, 0.001)
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Intra-Hour SolarToHomeKWH Mismatch Uses Direct Solar", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},
			}
			// Within the hour, 2 kWh of solar was generated and 2 kWh of home load occurred,
			// but only 1 kWh overlapped directly (SolarToHomeKWH = 1.0, SolarToBatteryKWH = 1.0, BatteryToHomeKWH = 1.0).
			// Battery starts at 20% reserve, so the 1 kWh non-overlapping load must be met via battery round-trip or grid.
			m.stats = []types.EnergyStats{
				{
					TSHourStart:       start,
					MinBatterySOC:     20,
					MaxBatterySOC:     20,
					HomeKWH:           2.0,
					SolarKWH:          2.0,
					SolarToHomeKWH:    1.0,
					SolarToBatteryKWH: 1.0,
					BatteryChargedKWH: 0.9,
					BatteryUsedKWH:    0.9,
					BatteryToHomeKWH:  0.9,
					GridImportKWH:     0.1,
				},
			}
		})
		// Because direct SolarToHomeKWH is 1.0 kWh, the remaining 1.0 kWh of solar goes through the shadow battery
		// with round-trip losses (1.0 * 0.90 = 0.90 kWh delivered to home), requiring 0.10 kWh of grid import @ $0.20 = $0.02,
		// matching actual cost ($0.02) so RateRudder is not penalized for physical round-trip conversion losses.
		assert.InDelta(t, 0.02, savings.SelfConsumptionCostDollars, 0.001)
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Forced Solar Export Does Not Mimic Solar Export In Shadow Self-Consumption", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.MinBatterySOC = 20.0
			m.settings.GridExportSolar = true
			m.actions = []types.Action{
				{Timestamp: start, ReserveSOC: 20.0, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 20.0}},
			}
			m.prices = []types.Price{
				// Peak hour: export pays $0.50/kWh, import is $0.50/kWh
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.50, SeparateGenerationCredit: true, GenerationCreditDollarsPerKWH: 0.50},
				// Next day load hour: import is $0.10/kWh
				{TSStart: end, TSEnd: end.Add(time.Hour), DollarsPerKWH: 0.10},
			}
			// RateRudder forces SolarMode=Export at 100% SOC: all 3.0 kWh of solar is exported to the grid
			// (SolarToGridKWH = 3.0, SolarToHomeKWH = 0.0) while the battery discharges 1.0 kWh to power the home.
			m.stats = []types.EnergyStats{
				{
					TSHourStart:      start,
					MinBatterySOC:    95,
					MaxBatterySOC:    100,
					HomeKWH:          1.0,
					SolarKWH:         3.0,
					SolarToHomeKWH:   0.0,
					SolarToGridKWH:   3.0,
					GridExportKWH:    3.0,
					BatteryUsedKWH:   1.0,
					BatteryToHomeKWH: 1.0,
				},
				{
					TSHourStart:   end,
					MinBatterySOC: 80,
					MaxBatterySOC: 95,
					HomeKWH:       3.0,
				},
			}
		})
		// In Self-Consumption mode, 1.0 kWh of solar serves the 1.0 kWh home load directly and only 2.0 kWh is exported ($1.00 credit),
		// whereas RateRudder exported all 3.0 kWh ($1.50 credit) and used 1.0 kWh of battery valued at next-day $0.10 * sqrt(0.90).
		assert.InDelta(t, 1.50, savings.Credit, 0.001)
		assert.InDelta(t, 1.00, savings.SelfConsumptionCreditDollars, 0.001)
		assert.InDelta(t, 0.50-0.10*math.Sqrt(0.90), savings.RateRudderSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Next-Day Solar Refills Partially Empty Battery Values Delta At Export Rate", func(t *testing.T) {
		oneWayEff := math.Sqrt(0.90)
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.MinBatterySOC = 20.0
			m.settings.GridExportSolar = true
			m.actions = []types.Action{
				{Timestamp: start, ReserveSOC: 20.0, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 30.0}},
			}
			m.prices = []types.Price{
				// Day 1 peak export hour ($0.40 import, $0.30 export)
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.40, SeparateGenerationCredit: true, GenerationCreditDollarsPerKWH: 0.30},
				// Day 2 morning load hour ($0.45 import, $0.05 export)
				{TSStart: end, TSEnd: end.Add(time.Hour), DollarsPerKWH: 0.45, SeparateGenerationCredit: true, GenerationCreditDollarsPerKWH: 0.05},
				// Day 2 midday solar hour ($0.45 import, $0.05 export)
				{TSStart: end.Add(time.Hour), TSEnd: end.Add(2 * time.Hour), DollarsPerKWH: 0.45, SeparateGenerationCredit: true, GenerationCreditDollarsPerKWH: 0.05},
			}
			m.stats = []types.EnergyStats{
				{
					// Day 1: Both batteries start at 85% SOC on a 30 kWh battery.
					// RateRudder exports 6.0 kWh (85% -> 65% SOC) @ $0.30 = $1.80 credit, while Shadow stays at 85% SOC.
					TSHourStart:      start,
					MinBatterySOC:    65,
					MaxBatterySOC:    85,
					BatteryUsedKWH:   6.0,
					BatteryToGridKWH: 6.0,
					GridExportKWH:    6.0,
				},
				{
					// Day 2 morning: 3.0 kWh load (both 65% and 85% batteries have >13 kWh usable above 20% reserve, so neither imports).
					TSHourStart:   end,
					MinBatterySOC: 55,
					MaxBatterySOC: 65,
					HomeKWH:       3.0,
				},
				{
					// Day 2 midday: 20 kWh solar surplus (< 24 kWh = 0.80*30 kWh, but > 13.5 kWh needed to refill from 55% to 100%).
					TSHourStart:   end.Add(time.Hour),
					MinBatterySOC: 55,
					MaxBatterySOC: 100,
					SolarKWH:      20.0,
				},
			}
		})
		// Both batteries cover Day 2 morning load without grid import and both refill to 100% from Day 2 solar.
		// Therefore the -6.0 kWh stored energy delta is valued at Day 2's $0.05/kWh solar export rate (* oneWayEff),
		// not the $0.45/kWh retail import rate!
		assert.InDelta(t, -6.0*0.05*oneWayEff, savings.StoredEnergyValueDollars, 0.001)
		assert.InDelta(t, 1.80-6.0*0.05*oneWayEff, savings.RateRudderSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Next-Day Solar Refill With GridExportSolar Disabled Values Excess At Zero", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.MinBatterySOC = 20.0
			m.settings.GridExportSolar = false
			m.actions = []types.Action{
				{Timestamp: start, ReserveSOC: 20.0, SystemStatus: types.SystemStatus{BatteryCapacityKWH: 20.0}},
			}
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.30},
				{TSStart: end, TSEnd: end.Add(time.Hour), DollarsPerKWH: 0.30},
			}
			m.stats = []types.EnergyStats{
				{
					// Day 1: Battery starts at 80% SOC; RateRudder discharges 4 kWh (80% -> 60% SOC), Shadow stays at 80%
					TSHourStart:    start,
					MinBatterySOC:  60,
					MaxBatterySOC:  80,
					BatteryUsedKWH: 4.0,
				},
				{
					// Day 2: 15 kWh solar surplus refills both 60% and 80% batteries to 100%, curtailing excess at $0.00/kWh
					TSHourStart:   end,
					MinBatterySOC: 60,
					MaxBatterySOC: 100,
					SolarKWH:      15.0,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.StoredEnergyValueDollars, 0.001)
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.001)
	})

	t.Run("NewCalculation Idle BMS Standby Drift Mirrors To Shadow Battery", func(t *testing.T) {
		savings := runNewCalcTest(t, end.Add(24*time.Hour), func(m *mockSavingsStorage) {
			m.settings.MinBatterySOC = 20.0
			m.prices = []types.Price{
				{TSStart: start, TSEnd: start.Add(time.Hour), DollarsPerKWH: 0.20},
			}
			// Idle battery drifts from 80% down to 79% with zero reported charge/discharge
			m.stats = []types.EnergyStats{
				{
					TSHourStart:   start,
					MinBatterySOC: 79,
					MaxBatterySOC: 80,
					HomeKWH:       1.0,
					SolarKWH:      1.0,
				},
			}
		})
		assert.InDelta(t, 0.0, savings.StoredEnergyValueDollars, 0.001)
		assert.InDelta(t, 0.0, savings.RateRudderSavingsDollars, 0.001)
	})
}

func TestHandleHistorySavingsAll(t *testing.T) {
	mockStore := &mockStorage{}
	mockUtilities := utility.NewMap(mockStore)
	s := &Server{storage: mockStore, utilities: mockUtilities, bypassAuth: true}

	start := time.Now().Truncate(24 * time.Hour).UTC()
	end := start.Add(24 * time.Hour).UTC()

	startQuery := start.AddDate(0, 0, -1)
	endQuery := end

	// Site 1 data
	mockStore.On("GetPriceHistory", mock.Anything, "site1", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.Price{
		{TSStart: start, DollarsPerKWH: 0.10},
	}, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, "site1", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.DailyEnergyStats{
		{Hourly: []types.EnergyStats{{TSHourStart: start, HomeKWH: 10, GridImportKWH: 10}}},
	}, nil)
	mockStore.On("GetActionHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Action{}, nil)
	mockStore.On("GetActionHistory", mock.Anything, "site2", mock.Anything, mock.Anything).Return([]types.Action{}, nil)

	// Site 2 data
	mockStore.On("GetPriceHistory", mock.Anything, "site2", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.Price{
		{TSStart: start, DollarsPerKWH: 0.20},
	}, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, "site2", mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(startQuery)
	}), mock.MatchedBy(func(t time.Time) bool {
		return t.Equal(endQuery)
	})).Return([]types.DailyEnergyStats{
		{Hourly: []types.EnergyStats{{TSHourStart: start, HomeKWH: 20, GridImportKWH: 20}}},
	}, nil)

	mockStore.On("GetSettings", mock.Anything, "site1").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
	mockStore.On("GetSettings", mock.Anything, "site2").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

	req, _ := http.NewRequest("GET", "/api/history/savings?siteID=ALL&start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
	// Mock authMiddleware effects
	ctx := req.Context()
	ctx = context.WithValue(ctx, siteIDContextKey, SiteIDAll)
	ctx = context.WithValue(ctx, allUserSitesContextKey, []types.UserSite{{ID: "site1"}, {ID: "site2"}})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handleHistorySavings(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var savings types.SavingsStats
	err := json.Unmarshal(rr.Body.Bytes(), &savings)
	require.NoError(t, err)

	// Site 1 cost: 10 * 0.10 = 1.00
	// Site 2 cost: 20 * 0.20 = 4.00
	// Total: 5.00
	assert.Equal(t, 5.00, savings.Cost)
	assert.Equal(t, 30.0, savings.HomeUsed) // 10 + 20
	assert.Empty(t, savings.HourlyDebugging)
}

func TestGetIgnoredFraction(t *testing.T) {
	hStart := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("No Actions", func(t *testing.T) {
		assert.Equal(t, 0.0, getIgnoredFraction(hStart, time.Hour, nil))
	})

	t.Run("Always Paused from Before", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(-time.Hour), Paused: true},
		}
		assert.Equal(t, 1.0, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Paused Halfway", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), Paused: true},
		}
		// Before 12:30 it was not paused (assuming no prior actions).
		// After 12:30 it is paused.
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Unpaused Halfway", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(-time.Hour), Paused: true},
			{Timestamp: hStart.Add(30 * time.Minute), Paused: false},
		}
		// Before 12:30 it was paused.
		// After 12:30 it is unpaused.
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Multiple Changes Within Hour", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(15 * time.Minute), Paused: true},  // 0-15 not paused, 15-30 paused
			{Timestamp: hStart.Add(30 * time.Minute), Paused: false}, // 30-45 not paused
			{Timestamp: hStart.Add(45 * time.Minute), Paused: true},  // 45-60 paused
		}
		// Paused for 15-30 and 45-60 = 30 minutes total = 0.5
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Emergency Mode", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), Reason: types.ActionReasonEmergencyMode},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("System Status Emergency Mode", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), SystemStatus: types.SystemStatus{EmergencyMode: true}},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("Action Reason VPP Active", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), Reason: types.ActionReasonVPPActive},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})

	t.Run("System Status VPP Active", func(t *testing.T) {
		actions := []types.Action{
			{Timestamp: hStart.Add(30 * time.Minute), SystemStatus: types.SystemStatus{VPPActive: true}},
		}
		assert.Equal(t, 0.5, getIgnoredFraction(hStart, time.Hour, actions))
	})
}

func TestInferRoundTripEfficiency(t *testing.T) {
	t.Run("EmptyOrZeroCapacityReturnsDefault", func(t *testing.T) {
		assert.InDelta(t, 0.90, inferRoundTripEfficiency(nil, 15.0, 0.90), 0.0001)
		assert.InDelta(t, 0.90, inferRoundTripEfficiency([]types.EnergyStats{}, 0, 0.90), 0.0001)
		assert.InDelta(t, 0.90, inferRoundTripEfficiency(nil, 15.0, 0), 0.0001)
		assert.InDelta(t, 0.90, inferRoundTripEfficiency(nil, 15.0, 1.5), 0.0001)
	})

	t.Run("BelowTwoFullCyclesReturnsDefault", func(t *testing.T) {
		// 15 kWh capacity requires >= 30 kWh charged and >= 30 kWh used
		stats := []types.EnergyStats{
			{MinBatterySOC: 20, MaxBatterySOC: 80, BatteryChargedKWH: 25.0, BatteryUsedKWH: 21.0},
		}
		assert.InDelta(t, 0.90, inferRoundTripEfficiency(stats, 15.0, 0.90), 0.0001)
	})

	t.Run("MultiCycleWithSameStartAndEndSOC", func(t *testing.T) {
		// Hour 0 charges from 20% -> 80% (startSOC = 20%); Hour 1 discharges from 80% -> 20% (endSOC = 20%).
		// TotalCharged = 100 kWh, TotalUsed = 86 kWh -> 86%
		stats := []types.EnergyStats{
			{MinBatterySOC: 20, MaxBatterySOC: 80, BatteryChargedKWH: 60.0, BatteryUsedKWH: 30.0},
			{MinBatterySOC: 20, MaxBatterySOC: 80, BatteryChargedKWH: 40.0, BatteryUsedKWH: 56.0},
		}
		assert.InDelta(t, 0.86, inferRoundTripEfficiency(stats, 15.0, 0.90), 0.0001)
	})

	t.Run("AdjustsUpwardWhenEndingAtHigherSOC", func(t *testing.T) {
		// 15 kWh battery starts at 20% (charging hour -> startSOC=20) and ends at 80% (charging hour -> endSOC=80).
		// Net SOC gain = +60% of 15 kWh = +9.0 kWh still in battery.
		// Charged = 100 kWh, Used = 78.46 kWh. Raw ratio = 78.46%, but with +9.0 * sqrt(0.90) = +8.538 kWh stored,
		// adjusted efficiency = (78.4619 + 8.5381) / 100 = 87.0%.
		storedRemainingOutKWH := 9.0 * math.Sqrt(0.90)
		stats := []types.EnergyStats{
			{MinBatterySOC: 20, MaxBatterySOC: 70, BatteryChargedKWH: 50.0, BatteryUsedKWH: 40.0},
			{MinBatterySOC: 30, MaxBatterySOC: 80, BatteryChargedKWH: 50.0, BatteryUsedKWH: 87.0 - storedRemainingOutKWH - 40.0},
		}
		assert.InDelta(t, 0.87, inferRoundTripEfficiency(stats, 15.0, 0.90), 0.0001)
	})

	t.Run("AdjustsDownwardWhenEndingAtLowerSOC", func(t *testing.T) {
		// 15 kWh battery starts at 80% (discharging hour -> startSOC=80) and ends at 20% (discharging hour -> endSOC=20).
		// Net SOC change = -60% of 15 kWh = -9.0 kWh drained from initial inventory.
		drainedInitialOutKWH := 9.0 * math.Sqrt(0.90)
		stats := []types.EnergyStats{
			{MinBatterySOC: 30, MaxBatterySOC: 80, BatteryChargedKWH: 30.0, BatteryUsedKWH: 45.0},
			{MinBatterySOC: 30, MaxBatterySOC: 80, BatteryChargedKWH: 40.0, BatteryUsedKWH: 10.0},
			{MinBatterySOC: 20, MaxBatterySOC: 70, BatteryChargedKWH: 30.0, BatteryUsedKWH: 86.0 + drainedInitialOutKWH - 55.0},
		}
		assert.InDelta(t, 0.86, inferRoundTripEfficiency(stats, 15.0, 0.90), 0.0001)
	})

	t.Run("ClampsOutOfBoundsRatios", func(t *testing.T) {
		// Between 0.70 and 0.75 (e.g. 73.4% on multi-inverter systems with light loads) is preserved
		lowLoad := []types.EnergyStats{
			{MinBatterySOC: 20, MaxBatterySOC: 20, BatteryChargedKWH: 100.0, BatteryUsedKWH: 73.4},
		}
		assert.InDelta(t, 0.734, inferRoundTripEfficiency(lowLoad, 15.0, 0.90), 0.0001)

		// Positive ratio below 0.70 clamps to 0.70 instead of cliff-jumping back to defaultRTEff
		tooLow := []types.EnergyStats{
			{MinBatterySOC: 20, MaxBatterySOC: 20, BatteryChargedKWH: 100.0, BatteryUsedKWH: 65.0},
		}
		assert.InDelta(t, 0.70, inferRoundTripEfficiency(tooLow, 15.0, 0.90), 0.0001)

		// Above defaultRTEff (e.g. DC-coupled solar reporting >90% AC ratio) caps at defaultRTEff
		aboveDefault := []types.EnergyStats{
			{MinBatterySOC: 20, MaxBatterySOC: 20, BatteryChargedKWH: 100.0, BatteryUsedKWH: 96.0},
		}
		assert.InDelta(t, 0.90, inferRoundTripEfficiency(aboveDefault, 15.0, 0.90), 0.0001)
	})

	t.Run("IdleStartAndEndHour", func(t *testing.T) {
		// Hour 0 is idle (80% -> 78% BMS drift -> startSOC = 80%),
		// Hour 1 cycles 50 kWh, Hour 2 is idle (22% -> 20% BMS drift -> endSOC = 20%).
		// Net SOC delta = -60% of 15 kWh = -9.0 kWh.
		drainedInitialOutKWH := 9.0 * math.Sqrt(0.90)
		stats := []types.EnergyStats{
			{MinBatterySOC: 78, MaxBatterySOC: 80, BatteryChargedKWH: 0, BatteryUsedKWH: 0},
			{MinBatterySOC: 22, MaxBatterySOC: 78, BatteryChargedKWH: 50.0, BatteryUsedKWH: 40.0 + drainedInitialOutKWH},
			{MinBatterySOC: 20, MaxBatterySOC: 22, BatteryChargedKWH: 0, BatteryUsedKWH: 0},
		}
		assert.InDelta(t, 0.80, inferRoundTripEfficiency(stats, 15.0, 0.90), 0.0001)
	})
}
