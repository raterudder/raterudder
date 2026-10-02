package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleForecast(t *testing.T) {
	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)

	t.Run("Returns 24 SimHours", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=300", resp.Header.Get("Cache-Control"))

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)
		assert.Len(t, data.Simulation, 24, "should return exactly 24 simulated hours")

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockU.AssertCalled(t, "GetFuturePrices", mock.Anything)
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockS.AssertCalled(t, "GetSettings", mock.Anything, mock.Anything)
		mockS.AssertCalled(t, "GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Overrides Home Load Prediction Strategy", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:              5.0,
			UtilityProvider:            "test",
			ESS:                        "mock",
			HomeLoadPredictionStrategy: "default",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		// Generate mock history with high load variance at hour 12
		history := []types.EnergyStats{}
		baseTime := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
		for day := 0; day < 10; day++ {
			for hour := 0; hour < 24; hour++ {
				ts := baseTime.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour)
				load := 1.0
				if hour == 12 {
					load = float64(day + 1)
				}
				history = append(history, types.EnergyStats{
					TSHourStart: ts,
					HomeKWH:     load,
				})
			}
		}

		dailyHistory := []types.DailyEnergyStats{
			{
				TSDayStart: baseTime,
				Hourly:     history,
			},
		}

		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(dailyHistory, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		// Make request with query param overrideHomeLoadPredictionStrategy=conservative
		req := httptest.NewRequest("GET", "/api/forecast?overrideHomeLoadPredictionStrategy=conservative", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		// Check the predicted load for hour 12.
		var foundHour12 bool
		for _, hour := range data.Simulation {
			if hour.Hour == 12 {
				foundHour12 = true
				assert.Greater(t, hour.AvgHomeLoadKWH, 4.5, "should override home load prediction strategy to conservative")
			}
		}
		assert.True(t, foundHour12, "should find hour 12 in simulation")
	})

	t.Run("Settings Error Returns 500", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, 0, time.Time{}, assert.AnError)

		srv := &Server{
			storage:    mockS,
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "failed to get settings")
	})

	t.Run("ESS Status Error Returns 500", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test", ESS: "mock"}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{}, assert.AnError)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:    mockS,
			ess:        mockP,
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "failed to get ess status")
	})

	t.Run("Price Error Returns 500", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test", ESS: "mock"}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{Timestamp: now}, nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, assert.AnError)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:    mockS,
			ess:        mockP,
			utilities:  mockUMap,
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "failed to get current price")
	})

	t.Run("No Backfill Called", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         80,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		mockS.AssertNotCalled(t, "GetLatestEnergyHistoryTime")
		mockS.AssertNotCalled(t, "GetLatestPriceHistoryTime")
		mockS.AssertNotCalled(t, "UpsertEnergyHistories")
		mockS.AssertNotCalled(t, "UpsertPrices")
		mockS.AssertNotCalled(t, "InsertAction")
		mockES.AssertNotCalled(t, "GetEnergyHistory")
		mockES.AssertNotCalled(t, "SetModes")
		mockU.AssertNotCalled(t, "GetConfirmedPrices")
	})

	t.Run("Returns History with Merged Weather Data", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		pastHour1 := now.Add(-1 * time.Hour)
		pastHour2 := now.Add(-2 * time.Hour)
		futureHour1 := now.Add(1 * time.Hour)
		futureHour2 := now.Add(2 * time.Hour)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			Location: &types.SiteLocation{
				Latitude:     1,
				Longitude:    1,
				TimeZone:     "UTC",
				SolarTilt:    30,
				SolarAzimuth: 180,
			},
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{
			{Hourly: []types.EnergyStats{
				{TSHourStart: pastHour2, SolarKWH: -0.005, HomeKWH: 2.0, MinBatterySOC: 40, MaxBatterySOC: 60},
				{TSHourStart: pastHour1, SolarKWH: 2.0, HomeKWH: 3.0, MinBatterySOC: 50, MaxBatterySOC: 70},
			}},
		}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: pastHour2, DollarsPerKWH: 0.1, GenerationAdjustmentDollarsPerKWH: -0.02},
			{TSStart: pastHour1, DollarsPerKWH: 0.15, SeparateGenerationCredit: true, GenerationCreditDollarsPerKWH: 0.08},
		}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: pastHour2, GTI: 100},
					{TSHourStart: pastHour1, GTI: 150},
					{TSHourStart: futureHour1, GTI: 200},
					{TSHourStart: futureHour2, GTI: 250},
				},
			},
		}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		assert.Len(t, data.Simulation, 24, "should return exactly 24 simulated hours")

		assert.Len(t, data.Simulation, 24, "should return exactly 24 simulated hours")

		assert.Len(t, data.EnergyHistory, 2)
		for _, eh := range data.EnergyHistory {
			if eh.TSHourStart.Equal(pastHour2) {
				assert.Equal(t, 0.0, eh.SolarKWH)
				assert.Equal(t, 2.0, eh.HomeLoadKWH)
				assert.Equal(t, 50.0, eh.AvgBatterySOC) // (40+60)/2
			}
			if eh.TSHourStart.Equal(pastHour1) {
				assert.Equal(t, 2.0, eh.SolarKWH)
				assert.Equal(t, 3.0, eh.HomeLoadKWH)
				assert.Equal(t, 60.0, eh.AvgBatterySOC) // (50+70)/2
			}
		}

		assert.Len(t, data.PriceHistory, 2)
		for _, ph := range data.PriceHistory {
			if ph.TSHourStart.Equal(pastHour2) {
				assert.Equal(t, 0.1, ph.DollarsPerKWH)
				assert.InDelta(t, 0.08, ph.ExportDollarsPerKWH, 0.0001)
			}
			if ph.TSHourStart.Equal(pastHour1) {
				assert.Equal(t, 0.15, ph.DollarsPerKWH)
				assert.Equal(t, 0.08, ph.ExportDollarsPerKWH)
			}
		}
	})

	t.Run("Applies Utility VPP Events", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)

		vppInfo := types.UtilityVPPInfo{
			Mandatory: []types.UtilityVPPPeriod{
				{
					TimePeriod: types.TimePeriod{
						Start: now.Add(2 * time.Hour),
						End:   now.Add(5 * time.Hour),
					},
					ReserveSOC: 20.0,
				},
			},
		}
		mockU.On("GetVPPInfo", mock.Anything).Return(vppInfo, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		mockU.AssertCalled(t, "GetVPPInfo", mock.Anything)

		var vppEventReflected bool
		for _, simHour := range data.Simulation {
			if simHour.TS.Equal(now.Add(2*time.Hour)) || simHour.TS.Equal(now.Add(3*time.Hour)) || simHour.TS.Equal(now.Add(4*time.Hour)) {
				assert.Equal(t, now.Add(5*time.Hour), simHour.VPPEndAt)
				vppEventReflected = true
			}
		}
		assert.True(t, vppEventReflected, "Expected simulation to reflect the VPP event")
	})

	t.Run("Uses Status from Recent Action", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		actionTime := now.Add(-30 * time.Minute)
		action := &types.Action{
			Timestamp: actionTime,
			SystemStatus: types.SystemStatus{
				BatterySOC:         75,
				BatteryCapacityKWH: 12.0,
				Timestamp:          actionTime,
			},
		}

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(action, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		assert.Equal(t, actionTime.Unix(), data.Updated.Unix())
		mockES.AssertNotCalled(t, "GetStatus", mock.Anything)
	})

	t.Run("Falls Back to GetStatus on Old Action", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		actionTime := now.Add(-2 * time.Hour)
		action := &types.Action{
			Timestamp: actionTime,
			SystemStatus: types.SystemStatus{
				BatterySOC:         75,
				BatteryCapacityKWH: 12.0,
				Timestamp:          actionTime,
			},
		}

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(action, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		assert.Equal(t, now.Unix(), data.Updated.Unix())
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
	})

	t.Run("Fast-path returns plan when latestAction is fresh", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		plan := &types.Plan{
			TSCreated:          now.Add(-10 * time.Minute),
			HorizonHours:       24,
			TotalProjectedCost: 2.50,
			Periods: []types.PlanPeriod{
				{
					TSStart:       now,
					TSEnd:         now.Add(time.Hour),
					DurationHours: 1,
					BatteryMode:   types.BatteryModeStandby,
					StartSOC:      50,
					EndSOC:        50,
				},
			},
		}

		action := &types.Action{
			Timestamp:    now.Add(-10 * time.Minute),
			BatteryMode:  types.BatteryModeStandby,
			CurrentPrice: &types.Price{DollarsPerKWH: 0.10, TSStart: now, TSEnd: now.Add(time.Hour)},
			Plan:         plan,
			SystemStatus: types.SystemStatus{
				BatterySOC:         50,
				BatteryCapacityKWH: 10.0,
				Timestamp:          now.Add(-10 * time.Minute),
			},
		}

		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(action, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockES := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		require.NotNil(t, data.Plan, "fast-path should return plan")
		assert.Equal(t, 24, data.Plan.HorizonHours)
		assert.Len(t, data.Plan.Periods, 1)
		require.NotNil(t, data.LatestAction)
		assert.Empty(t, data.Simulation, "fast-path should not return simulation")
		mockES.AssertNotCalled(t, "GetStatus", mock.Anything)
		mockU.AssertNotCalled(t, "GetCurrentPrice", mock.Anything)
	})

	t.Run("Staging real-time plan fallback when plan is stale", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		// Action is 2 hours old (stale)
		staleAction := &types.Action{
			Timestamp:    now.Add(-2 * time.Hour),
			BatteryMode:  types.BatteryModeStandby,
			CurrentPrice: &types.Price{DollarsPerKWH: 0.10, TSStart: now.Add(-2 * time.Hour)},
			SystemStatus: types.SystemStatus{
				BatterySOC:         50,
				BatteryCapacityKWH: 10.0,
				Timestamp:          now.Add(-2 * time.Hour),
			},
		}

		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(staleAction, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         60,
			BatteryCapacityKWH: 10.0,
			Timestamp:          now,
		}, nil)
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		futurePrices := make([]types.Price, 24)
		for i := 0; i < 24; i++ {
			pStart := now.Add(time.Duration(i) * time.Hour)
			futurePrices[i] = types.Price{
				DollarsPerKWH: 0.12,
				TSStart:       pStart,
				TSEnd:         pStart.Add(time.Hour),
			}
		}

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{
			DollarsPerKWH: 0.10,
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
		}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "staging",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		require.NotNil(t, data.Plan, "staging fallback should run controller.Plan and return plan")
		assert.Greater(t, data.Plan.HorizonHours, 0)
		require.NotNil(t, data.LatestAction)
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
	})

	t.Run("Production With PlanMode Runs Plan", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		now := time.Now().Truncate(time.Hour)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{
			DollarsPerKWH: 0.15,
			TSStart:       now,
			TSEnd:         now.Add(time.Hour),
		}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatteryCapacityKWH: 13.5,
			BatterySOC:         50,
			Timestamp:          now,
			TimeLocation:       "UTC",
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockS := &mockStorage{}
		staleAction := &types.Action{
			Timestamp:    now.Add(-2 * time.Hour),
			BatteryMode:  types.BatteryModeStandby,
			CurrentPrice: &types.Price{DollarsPerKWH: 0.10, TSStart: now.Add(-2 * time.Hour)},
			SystemStatus: types.SystemStatus{
				BatterySOC:         50,
				BatteryCapacityKWH: 13.5,
				Timestamp:          now.Add(-2 * time.Hour),
			},
		}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(staleAction, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			MinBatterySOC:   20.0,
			Release:         "production",
			ESS:             "mock",
			PlanMode:        true,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		var futurePrices []types.Price
		for i := 1; i <= 24; i++ {
			futurePrices = append(futurePrices, types.Price{
				DollarsPerKWH: 0.15,
				TSStart:       now.Add(time.Duration(i) * time.Hour),
				TSEnd:         now.Add(time.Duration(i+1) * time.Hour),
			})
		}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{
			{
				DollarsPerKWH: 0.15,
				TSStart:       now,
				TSEnd:         now.Add(time.Hour),
			},
		}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return(futurePrices, nil)
		mockU.On("GetVPPInfo", mock.Anything).Return(types.UtilityVPPInfo{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		require.NotNil(t, data.Plan, "PlanMode=true on production should run controller.Plan and return plan")
		assert.Greater(t, data.Plan.HorizonHours, 0)
		require.NotNil(t, data.LatestAction)
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
	})

	t.Run("Fault In LatestAction Bypasses Planning And Returns Fault Action", func(t *testing.T) {
		now := time.Now().Truncate(time.Hour)
		mockS := &mockStorage{}
		faultAction := &types.Action{
			Timestamp:    now,
			Description:  "Grid is unavailable",
			Reason:       types.ActionReasonGridUnavailable,
			Fault:        true,
			CurrentPrice: &types.Price{DollarsPerKWH: 0.10, TSStart: now},
			SystemStatus: types.SystemStatus{
				GridUnavailable: true,
				Timestamp:       now,
			},
		}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(faultAction, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			ESS:             "mock",
			PlanMode:        true,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		assert.Nil(t, data.Plan, "Fault state must not generate a plan")
		assert.Empty(t, data.Simulation, "Fault state must not run simulation")
		require.NotNil(t, data.LatestAction)
		assert.True(t, data.LatestAction.Fault)
		assert.Equal(t, "Grid is unavailable", data.LatestAction.Description)
		mockES.AssertNotCalled(t, "GetStatus")
	})

	t.Run("Realtime Fault Status Bypasses Planning", func(t *testing.T) {
		now := time.Now().Truncate(time.Hour)
		mockS := &mockStorage{}
		// No recent action so realtime status is fetched
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			ESS:             "mock",
			PlanMode:        true,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			GridUnavailable: true,
			Timestamp:       now,
			TimeLocation:    "UTC",
		}, nil)
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		assert.Nil(t, data.Plan, "Realtime fault state must not generate a plan")
		assert.Empty(t, data.Simulation, "Realtime fault state must not run simulation")
		require.NotNil(t, data.LatestAction)
		assert.True(t, data.LatestAction.Fault)
		assert.Equal(t, "Grid is unavailable", data.LatestAction.Description)
	})

	t.Run("Paused Action With Plan Returns Plan", func(t *testing.T) {
		now := time.Now().Truncate(time.Hour)
		mockS := &mockStorage{}
		pausedAction := &types.Action{
			Timestamp:    now,
			Description:  "Automation is paused",
			Paused:       true,
			CurrentPrice: &types.Price{DollarsPerKWH: 0.10, TSStart: now},
			Plan: &types.Plan{
				HorizonHours: 24,
				Periods: []types.PlanPeriod{
					{
						TSStart:       now,
						TSEnd:         now.Add(time.Hour),
						DurationHours: 1,
						BatteryMode:   types.BatteryModeLoad,
					},
				},
			},
			SystemStatus: types.SystemStatus{
				BatterySOC: 70,
				Timestamp:  now,
			},
		}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return(pausedAction, nil)
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			ESS:             "mock",
			Pause:           true,
			PlanMode:        true,
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		mockES := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
			release:    "production",
			nowFunc:    func() time.Time { return now },
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var data ForecastRes
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)

		require.NotNil(t, data.Plan, "Paused user with plan should receive plan in forecast")
		require.NotNil(t, data.LatestAction)
		assert.True(t, data.LatestAction.Paused)
		assert.Equal(t, "Automation is paused", data.LatestAction.Description)
	})
}
