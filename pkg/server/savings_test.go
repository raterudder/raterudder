package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSavingsStorage struct {
	*mockStorage
	prices  []types.Price
	stats   []types.EnergyStats
	actions []types.Action
}

func (m *mockSavingsStorage) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	return m.prices, nil
}

func (m *mockSavingsStorage) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.EnergyStats, error) {
	return m.stats, nil
}

func (m *mockSavingsStorage) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	return m.actions, nil
}

func TestHandleHistorySavings(t *testing.T) {
	start := time.Now().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)

	tests := []struct {
		name                 string
		setupMock            func(*mockSavingsStorage)
		expectedCost         float64
		expectedCredit       float64
		expectedAvoidedCost  float64
		expectedChargingCost float64
		expectedBattSavings  float64
		expectedSolarSavings float64
	}{
		{
			name: "Basic Charge and Discharge",
			setupMock: func(m *mockSavingsStorage) {
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
			},
			expectedCost:         1.00,
			expectedCredit:       0.0,
			expectedAvoidedCost:  1.00,
			expectedChargingCost: 0.50, // 5kWh discharged from 10kWh pool charged @ $0.10
			expectedBattSavings:  0.50, // 1.00 - 0.50
			expectedSolarSavings: 0.0,
		},
		{
			name: "Charge Only - No Battery Savings Penalty",
			setupMock: func(m *mockSavingsStorage) {
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
			},
			expectedCost:         1.00,
			expectedCredit:       0.0,
			expectedAvoidedCost:  0.0,
			expectedChargingCost: 0.0, // Nothing discharged, so no charging cost attributed to use
			expectedBattSavings:  0.0, // Used to be -1.00
			expectedSolarSavings: 0.0,
		},
		{
			name: "Discharge Only (with 24h lookback stack)",
			setupMock: func(m *mockSavingsStorage) {
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
			},
			expectedCost:         0.0, // Cost of charge was in lookback period
			expectedCredit:       0.0,
			expectedAvoidedCost:  2.00, // 10kWh * 0.20
			expectedChargingCost: 0.50, // 10kWh * 0.05 (pulled from lookback stack)
			expectedBattSavings:  1.50,
			expectedSolarSavings: 0.0,
		},
		{
			name: "Partial Paused Hour",
			setupMock: func(m *mockSavingsStorage) {
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
			},
			expectedCost:         0.0,
			expectedCredit:       0.0,
			expectedAvoidedCost:  1.00, // 10kWh * 50% active * $0.20
			expectedChargingCost: 0.0,
			expectedBattSavings:  1.00,
			expectedSolarSavings: 0.0,
		},
		{
			name: "Storm Hedge Ignoring",
			setupMock: func(m *mockSavingsStorage) {
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
			},
			expectedCost:         1.00, // Actual grid cost is still recorded
			expectedCredit:       0.0,
			expectedAvoidedCost:  0.0,
			expectedChargingCost: 0.0, // No charging cost attributed because it was a storm
			expectedBattSavings:  0.0,
			expectedSolarSavings: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStoreBase := &mockStorage{}
			mockStoreBase.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, nil)
			mockStore := &mockSavingsStorage{mockStorage: mockStoreBase}

			tt.setupMock(mockStore)

			s := &Server{storage: mockStore, bypassAuth: true}

			req, _ := http.NewRequest("GET", "/api/history/savings?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			rr := httptest.NewRecorder()

			s.handleHistorySavings(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)

			var savings types.SavingsStats
			err := json.Unmarshal(rr.Body.Bytes(), &savings)
			require.NoError(t, err)

			assert.InDelta(t, tt.expectedCost, savings.Cost, 0.001, "Cost mismatch")
			assert.InDelta(t, tt.expectedCredit, savings.Credit, 0.001, "Credit mismatch")
			assert.InDelta(t, tt.expectedAvoidedCost, savings.AvoidedCost, 0.001, "AvoidedCost mismatch")
			assert.InDelta(t, tt.expectedChargingCost, savings.ChargingCost, 0.001, "ChargingCost mismatch")
			assert.InDelta(t, tt.expectedBattSavings, savings.BatterySavings, 0.001, "BatterySavings mismatch")
			assert.InDelta(t, tt.expectedSolarSavings, savings.SolarSavings, 0.001, "SolarSavings mismatch")
		})
	}
}

func TestHandleHistorySavingsAll(t *testing.T) {
	mockStore := &mockStorage{}
	s := &Server{storage: mockStore, bypassAuth: true}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)

	// Site 1 data
	mockStore.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{
		{TSStart: start, DollarsPerKWH: 0.10},
	}, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.EnergyStats{
		{TSHourStart: start, HomeKWH: 10, GridImportKWH: 10},
	}, nil)

	// Site 2 data
	mockStore.On("GetPriceHistory", mock.Anything, "site2", mock.Anything, mock.Anything).Return([]types.Price{
		{TSStart: start, DollarsPerKWH: 0.20},
	}, nil)
	mockStore.On("GetEnergyHistory", mock.Anything, "site2", mock.Anything, mock.Anything).Return([]types.EnergyStats{
		{TSHourStart: start, HomeKWH: 20, GridImportKWH: 20},
	}, nil)

	mockStore.On("GetActionHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Action{}, nil)

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
