package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/utility"
)

func TestHandleForecast(t *testing.T) {
	t.Run("Returns 24 SimHours", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          time.Now(),
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap()
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

		var data struct {
			Simulation []controller.SimHour `json:"simulation"`
		}
		err := json.NewDecoder(resp.Body).Decode(&data)
		require.NoError(t, err)
		assert.Len(t, data.Simulation, 24, "should return exactly 24 simulated hours")

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockU.AssertCalled(t, "GetFuturePrices", mock.Anything)
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockS.AssertCalled(t, "GetSettings", mock.Anything, mock.Anything)
		mockS.AssertCalled(t, "GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Settings Error Returns 500", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, 0, assert.AnError)

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
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test", ESS: "mock"}, types.CurrentSettingsVersion, nil)

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
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test", ESS: "mock"}, types.CurrentSettingsVersion, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{Timestamp: time.Now()}, nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, assert.AnError)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap()
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
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         80,
			BatteryCapacityKWH: 10.0,
			Timestamp:          time.Now(),
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap()
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
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil)

		now := time.Now().Truncate(time.Hour)
		pastHour1 := now.Add(-1 * time.Hour)
		pastHour2 := now.Add(-2 * time.Hour)
		futureHour1 := now.Add(1 * time.Hour)
		futureHour2 := now.Add(2 * time.Hour)

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			Location: &types.SiteLocation{
				Latitude:  1,
				Longitude: 1,
				TimeZone:  "UTC",
			},
			ESS: "mock",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{
			{TSHourStart: pastHour2, SolarKWH: 1.5, HomeKWH: 2.0, MinBatterySOC: 40, MaxBatterySOC: 60},
			{TSHourStart: pastHour1, SolarKWH: 2.0, HomeKWH: 3.0, MinBatterySOC: 50, MaxBatterySOC: 70},
		}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: pastHour2, DollarsPerKWH: 0.1},
			{TSStart: pastHour1, DollarsPerKWH: 0.1},
		}, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: pastHour2, GHI: 100},
					{TSHourStart: pastHour1, GHI: 150},
					{TSHourStart: futureHour1, GHI: 200},
					{TSHourStart: futureHour2, GHI: 250},
				},
			},
		}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			BatterySOC:         50,
			BatteryCapacityKWH: 10.0,
			Timestamp:          time.Now(),
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap()
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

		foundActual := false
		foundForecast := false
		for _, w := range data.Weather {
			if w.TSHourStart.Equal(pastHour2) {
				assert.Equal(t, float64(100), w.ForecastGHI)
				foundActual = true
			}
			if w.TSHourStart.Equal(futureHour1) {
				assert.Equal(t, float64(200), w.ForecastGHI)
				foundForecast = true
			}
		}
		assert.True(t, foundActual, "should have mapped actual GHI")
		assert.True(t, foundForecast, "should have mapped forecast GHI")

		assert.Len(t, data.EnergyHistory, 2)
		for _, eh := range data.EnergyHistory {
			if eh.TSHourStart.Equal(pastHour2) {
				assert.Equal(t, 1.5, eh.SolarKWH)
				assert.Equal(t, 2.0, eh.HomeLoadKWH)
				assert.Equal(t, 50.0, eh.AvgBatterySOC) // (40+60)/2
			}
			if eh.TSHourStart.Equal(pastHour1) {
				assert.Equal(t, 2.0, eh.SolarKWH)
				assert.Equal(t, 3.0, eh.HomeLoadKWH)
				assert.Equal(t, 60.0, eh.AvgBatterySOC) // (50+70)/2
			}
		}
	})
}

func TestCalculateImprovedSolar(t *testing.T) {
	ctx := context.Background()
	// Use a fixed UTC time to avoid day-boundary issues with Truncate(24*time.Hour)
	fixedTime := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	now := fixedTime
	past1 := now.Add(-1 * time.Hour)
	past2 := now.Add(-2 * time.Hour)
	past3 := now.Add(-3 * time.Hour)
	future1 := now.Add(1 * time.Hour)
	future2 := now.Add(2 * time.Hour)

	t.Run("Returns empty map when no weather provided", func(t *testing.T) {
		results := calculateImprovedSolar(ctx, []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.0},
		}, nil)
		assert.Empty(t, results)
	})

	t.Run("Returns results with zero ImprovedSolar when no calibration history", func(t *testing.T) {
		// Weather exists but history is empty → no efficiency calibration → ImprovedSolar=0.
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: future1, GHI: 200, GTI: 200, TemperatureC: 20},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 0.0, results[future1.Unix()].ImprovedSolar,
				"no history means no peak efficiency, so projection must be zero")
		}
	})

	t.Run("Calculates robust efficiency and projects future hours", func(t *testing.T) {
		// Provide 7 hours so the middle ones aren't "edges" (edge = first/last 2 hours of GTI > 50)
		h3 := now.Add(-3 * time.Hour)
		h2 := now.Add(-2 * time.Hour) // past2
		h1 := now.Add(-1 * time.Hour) // past1
		f1 := now.Add(1 * time.Hour)  // future1
		f2 := now.Add(2 * time.Hour)
		f3 := now.Add(3 * time.Hour)
		f4 := now.Add(4 * time.Hour)

		// history:
		// h2: actual=1.0, GTI=100. eff=0.009934
		// h1: actual=2.2, GTI=200. eff=0.011048
		history := []types.EnergyStats{
			{TSHourStart: h2, SolarKWH: 1.0},
			{TSHourStart: h1, SolarKWH: 2.2},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: h3, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: h2, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: h1, GHI: 100, GTI: 200, TemperatureC: 20},
			{TSHourStart: f1, GHI: 100, GTI: 300, TemperatureC: 20},
			{TSHourStart: f2, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: f3, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: f4, GHI: 100, GTI: 100, TemperatureC: 20},
		}}}

		// Window: [h3, f4].
		// Edges: h3, h2, f3, f4.
		// Non-edges: h1, f1, f2.
		// h1 is history + non-edge → eff = 0.011048.
		// Since only 1 valid efficiency point (h1), finalEff = 0.011048.

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 7) {
			assert.InDelta(t, 3.26, results[f1.Unix()].ImprovedSolar, 0.01)
		}
	})

	t.Run("Projects historical hours too (for algorithm vs. reality comparison)", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 25}
		}

		history := []types.EnergyStats{
			{TSHourStart: h[3], SolarKWH: 1.0},
			{TSHourStart: h[4], SolarKWH: 2.0},
			{TSHourStart: h[5], SolarKWH: 2.2},
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			assert.Greater(t, results[h[4].Unix()].ImprovedSolar, 0.0,
				"historical hours should receive an ImprovedSolar value for comparison")
			assert.InDelta(t, 2.2, results[h[5].Unix()].ImprovedSolar, 0.33,
				"peak hour projection should be close to the robustly calibrated reading")
		}
	})

	t.Run("Applies temperature efficiency loss via NOCT at high irradiance", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 20}
		}
		// Future hour with high irradiance
		weather[0].ForecastHours[9].GTI = 800

		history := []types.EnergyStats{
			{TSHourStart: h[5], SolarKWH: 1.0},
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			assert.InDelta(t, 7.39, results[h[9].Unix()].ImprovedSolar, 0.02)
			assert.InDelta(t, 0.93, results[h[9].Unix()].TempFactor, 0.001)
		}
	})

	t.Run("Applies temperature efficiency gain at cold temperature", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 20}
		}
		// Future hour with cold temp
		weather[0].ForecastHours[9].TemperatureC = -10

		history := []types.EnergyStats{
			{TSHourStart: h[5], SolarKWH: 1.0},
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			assert.Greater(t, results[h[9].Unix()].ImprovedSolar, 1.0,
				"cold panels run above rated efficiency → output exceeds baseline")
			assert.InDelta(t, 1.104, results[h[9].Unix()].ImprovedSolar, 0.01)
		}
	})

	t.Run("Clamps cell temperature at -40 C lower bound for TempFactor", func(t *testing.T) {
		// Tamb=-60 C, GTI=100. Raw Tcell would be -56.875 C, clamped to -40 C.
		// TF = 1 - (-40-25)*0.0035 = 1 + 0.2275 = 1.2275
		history := []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 100, GTI: 100, TemperatureC: -60},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.InDelta(t, 1.2275, results[future1.Unix()].TempFactor, 0.001,
				"TempFactor must be computed with Tcell clamped at -40 C")
		}
	})

	t.Run("Clamps cell temperature at 80 C upper bound for TempFactor", func(t *testing.T) {
		// Tamb=70 C, GTI=800. Raw Tcell = 70+25 = 95 C, clamped to 80 C.
		// TF = 1 - (80-25)*0.0035 = 1 - 0.1925 = 0.8075
		history := []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 800, GTI: 800, TemperatureC: 70},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.InDelta(t, 0.8075, results[future1.Unix()].TempFactor, 0.001,
				"TempFactor must be computed with Tcell clamped at 80 C")
		}
	})

	t.Run("Falls back from GTI to GHI when GTI is zero", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GHI: 100, GTI: 0, TemperatureC: 20}
		}
		weather[0].ForecastHours[9].GHI = 200

		history := []types.EnergyStats{
			{TSHourStart: h[5], SolarKWH: 1.0},
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			assert.InDelta(t, 1.979, results[h[9].Unix()].ImprovedSolar, 0.02,
				"GTI=0 should fall back to GHI for both calibration and projection")
			assert.Equal(t, 0.0, results[h[5].Unix()].GTI,
				"GHI value should be stored as effective GTI when GTI is zero")
			assert.Equal(t, 100.0, results[h[5].Unix()].GHI,
				"GHI value should be stored as effective GTI when GTI is zero")
		}
	})

	t.Run("Applies GTI correction from tilt model (higher GTI than GHI)", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GHI: 100, GTI: 100, TemperatureC: 20}
		}
		weather[0].ForecastHours[9].GTI = 200

		history := []types.EnergyStats{
			{TSHourStart: h[5], SolarKWH: 1.2},
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			assert.InDelta(t, 2.375, results[h[9].Unix()].ImprovedSolar, 0.01,
				"higher GTI from tilt should proportionally increase projected generation")
		}
	})

	t.Run("Ignores calibration hours with low irradiance (GTI < 50)", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 20}
		}
		weather[0].ForecastHours[5].GTI = 40 // low GTI hour

		history := []types.EnergyStats{
			{TSHourStart: h[5], SolarKWH: 10.0}, // eff would be 0.25 if not ignored
			{TSHourStart: h[6], SolarKWH: 1.0},  // eff=0.01
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			// Should use eff=0.01 from h[6]
			assert.InDelta(t, 1.0, results[h[9].Unix()].ImprovedSolar, 0.02,
				"low-GTI noise must not inflate the calibrated efficiency")
		}
	})

	t.Run("Ignores calibration hours with negligible actual solar (<= 0.1 kWh)", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 10)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 20}
		}

		history := []types.EnergyStats{
			{TSHourStart: h[5], SolarKWH: 0.05}, // negligible solar
			{TSHourStart: h[6], SolarKWH: 1.0},  // valid solar
		}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 10) {
			assert.InDelta(t, 1.0, results[h[9].Unix()].ImprovedSolar, 0.02)
		}
	})

	t.Run("Uses robust efficiency instead of absolute maximum", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}

		// efficiencies (sorted): 0.01, 0.011, 0.012, 0.013, 0.014... 0.020 (outlier)
		history := make([]types.EnergyStats, 6)
		// We need hours that are NOT edges. Window is [h0, h9]. Non-edges: [h2, h7].
		// We'll use h2, h3, h4, h5, h6, h7.
		history[0] = types.EnergyStats{TSHourStart: h[2], SolarKWH: 2.0} // eff=2/100=0.02 (peak outlier)
		history[1] = types.EnergyStats{TSHourStart: h[3], SolarKWH: 1.1} // eff=1.1/100=0.011
		history[2] = types.EnergyStats{TSHourStart: h[4], SolarKWH: 1.2} // eff=0.012
		history[3] = types.EnergyStats{TSHourStart: h[5], SolarKWH: 1.3} // eff=0.013
		history[4] = types.EnergyStats{TSHourStart: h[6], SolarKWH: 1.4} // eff=0.014
		history[5] = types.EnergyStats{TSHourStart: h[7], SolarKWH: 1.5} // eff=0.015

		forecastHours := make([]types.HourlyWeather, len(h))
		for i, ts := range h {
			forecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 25} // TF=1.0 at 25C
		}
		weather := []types.Weather{{ForecastHours: forecastHours}}

		results := calculateImprovedSolar(ctx, history, weather)

		// efficiencies collected: [0.011, 0.012, 0.013, 0.014, 0.015, 0.020]
		// len=6. 90th percentile index = 5. index == 6-1 -> index = 4 (value 0.015).
		// Temperature Factor for h[8] at 25C is 1.0.
		// Result should be 100 * 0.015 * 1.0 = 1.5.

		if assert.Len(t, results, 10) {
			assert.InDelta(t, 1.5, results[h[8].Unix()].ImprovedSolar, 0.01,
				"should use second highest efficiency (0.015) instead of peak outlier (0.02)")
		}
	})

	t.Run("Filters out curtailed and snowy hours", func(t *testing.T) {
		h := make([]time.Time, 10)
		for i := range h {
			h[i] = now.Add(time.Duration(i-5) * time.Hour)
		}
		forecastHours := make([]types.HourlyWeather, len(h))
		for i, ts := range h {
			forecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 25}
		}
		weather := []types.Weather{{ForecastHours: forecastHours}}

		history := []types.EnergyStats{
			{TSHourStart: h[2], SolarKWH: 5.0},                                           // eff=0.05 (curtailed?) - No, this one will be the baseline
			{TSHourStart: h[3], SolarKWH: 10.0, MaxBatterySOC: 99.0, GridExportKWH: 0.0}, // Curtailed outlier, should be ignored
			{TSHourStart: h[4], SolarKWH: 5.0},                                           // Another baseline
		}
		// Snow at h[4]
		weather[0].ForecastHours[4].SnowfallCM = 2.0 // This will cause snowAccum > 0.1 for h[4]

		results := calculateImprovedSolar(ctx, history, weather)

		// h[2] is valid (non-edge, non-curtailed, non-snowy). eff=0.05.
		// h[3] is curtailed (SOC=99, Export=0). Ignored.
		// h[4] is snowy (Accum=2.0). Ignored.
		// efficiencies: [0.05]. finalEff=0.05.
		// If h[3] wasn't ignored, finalEff would be 0.10.

		if assert.Len(t, results, 10) {
			assert.InDelta(t, 5.0, results[h[8].Unix()].ImprovedSolar, 0.01,
				"curtailed (h3) and snowy (h4) hours should be ignored in calibration")
		}
	})

	// ─── Snow tests ────────────────────────────────────────────────────────────

	t.Run("Snow: zero snowfall leaves SnowFactor at 1.0", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 5},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 1.0, results[past1.Unix()].SnowFactor)
			assert.Equal(t, 0.0, results[past1.Unix()].SnowAccumulation)
		}
	})

	t.Run("Snow: trace dusting (0 < acc <= 0.2 cm) reduces output by 30%", func(t *testing.T) {
		// 0.1 cm at Tamb=-5 C → Tcell=-5+(100/800)*25=-1.875 C (<=0, no melt) → acc=0.1, SF=0.70
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 0.1, TemperatureC: -5},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 0.70, results[past1.Unix()].SnowFactor)
			assert.InDelta(t, 0.1, results[past1.Unix()].SnowAccumulation, 0.001)
		}
	})

	t.Run("Snow: heavy accumulation (0.2 < acc <= 5 cm) reduces output by 90%", func(t *testing.T) {
		// 3 cm at Tamb=-5 C → Tcell=-1.875 C → no melt → acc=3 cm, SF=0.1
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 3.0, TemperatureC: -5},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 0.1, results[past1.Unix()].SnowFactor)
			assert.InDelta(t, 3.0, results[past1.Unix()].SnowAccumulation, 0.001)
		}
	})

	t.Run("Snow: deep accumulation (> 5 cm) blocks generation entirely", func(t *testing.T) {
		// 6 cm at Tamb=-5 C → no melt → acc=6 cm, SF=0.0
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 6.0, TemperatureC: -5},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 0.0, results[past1.Unix()].SnowFactor)
		}
	})

	t.Run("Snow: melt at TCell > 5 C (2 cm/hr) with slide-off when residual < 1 cm", func(t *testing.T) {
		// Hour 1 (past1): 2.5 cm falls at Tamb=-5 C → Tcell=-1.875 C → no melt → acc=2.5, SF=0.1
		// Hour 2 (past2... wait using now): Tamb=10 C → Tcell=10+(100/800)*25=13.125 C (>5)
		//   melt 2 cm → 2.5-2=0.5 cm; 0.5 < 1 → slide-off → acc=0, SF=1.0
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 2.5, TemperatureC: -5},
			{TSHourStart: now, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 10},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 2) {
			assert.Equal(t, 0.1, results[past1.Unix()].SnowFactor,
				"2.5 cm at -5 C: SF=0.1")
			assert.InDelta(t, 2.5, results[past1.Unix()].SnowAccumulation, 0.001)
			assert.Equal(t, 1.0, results[now.Unix()].SnowFactor,
				"after 2 cm melt, 0.5 cm residual < 1 cm → slides off → SF=1.0")
			assert.Equal(t, 0.0, results[now.Unix()].SnowAccumulation)
		}
	})

	t.Run("Snow: melt at TCell > 5 C does NOT fully clear deep snow in one hour", func(t *testing.T) {
		// Hour 1: 5 cm at -5 C → acc=5, SF=0.1
		// Hour 2: Tamb=10 C → Tcell=13.125 C (>5) → 5-2=3 cm; 3 >= 1 → stays → acc=3, SF=0.1
		// Hour 3: Tamb=10 C → 3-2=1 cm; 1 is NOT < 1 → stays → acc=1, SF=0.1
		// Hour 4: Tamb=10 C → 1-2=-1 → clamped to 0 → acc=0, SF=1.0
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past3, GHI: 100, GTI: 100, SnowfallCM: 5.0, TemperatureC: -5},
			{TSHourStart: past2, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 10},
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 10},
			{TSHourStart: now, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 10},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 4) {
			assert.InDelta(t, 5.0, results[past3.Unix()].SnowAccumulation, 0.001)
			assert.Equal(t, 0.1, results[past3.Unix()].SnowFactor)

			assert.InDelta(t, 3.0, results[past2.Unix()].SnowAccumulation, 0.001,
				"5 cm − 2 cm melt = 3 cm; 3 >= 1 so no slide-off")
			assert.Equal(t, 0.1, results[past2.Unix()].SnowFactor)

			assert.InDelta(t, 1.0, results[past1.Unix()].SnowAccumulation, 0.001,
				"3 cm − 2 cm melt = 1 cm; 1 is NOT < 1 so no slide-off")
			assert.Equal(t, 0.1, results[past1.Unix()].SnowFactor)

			assert.Equal(t, 0.0, results[now.Unix()].SnowAccumulation,
				"1 cm − 2 cm melt = -1 → clamped to 0")
			assert.Equal(t, 1.0, results[now.Unix()].SnowFactor)
		}
	})

	t.Run("Snow: partial melt at TCell 2–5 C (1 cm/hr) with slide-off when residual < 1 cm", func(t *testing.T) {
		// past2: 1.5 cm at Tamb=-10 C → Tcell=-10+(100/800)*25=-6.875 C → no melt → acc=1.5, SF=0.1
		// past1: Tamb=0 C → Tcell=0+3.125=3.125 C (in (2,5] bracket)
		//   melt 1 cm → 1.5-1=0.5 cm; 0.5 < 1 → slide-off → acc=0, SF=1.0
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 100, GTI: 100, SnowfallCM: 1.5, TemperatureC: -10},
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 0},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 2) {
			assert.InDelta(t, 1.5, results[past2.Unix()].SnowAccumulation, 0.001)
			assert.Equal(t, 0.1, results[past2.Unix()].SnowFactor)

			assert.Equal(t, 0.0, results[past1.Unix()].SnowAccumulation,
				"0.5 cm residual after 1 cm/hr melt is < 1 cm → slides off")
			assert.Equal(t, 1.0, results[past1.Unix()].SnowFactor)
		}
	})

	t.Run("Snow: slow surface melt at TCell 0–2 C (0.5 cm/hr) does not cause slide-off", func(t *testing.T) {
		// past2: 3 cm at Tamb=-20 C → Tcell=-16.875 C → no melt → acc=3, SF=0.1
		// past1: Tamb=-2 C → Tcell=-2+3.125=1.125 C (in (0,2] bracket)
		//   melt 0.5 cm → acc=2.5; no slide-off rule for this bracket → acc=2.5, SF=0.1
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 100, GTI: 100, SnowfallCM: 3.0, TemperatureC: -20},
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: -2},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 2) {
			assert.InDelta(t, 3.0, results[past2.Unix()].SnowAccumulation, 0.001)
			assert.InDelta(t, 2.5, results[past1.Unix()].SnowAccumulation, 0.001,
				"0-2 C melts 0.5 cm/hr; no slide-off rule at this temperature")
			assert.Equal(t, 0.1, results[past1.Unix()].SnowFactor)
		}
	})

	t.Run("Snow: accumulation capped at 10 cm", func(t *testing.T) {
		// 12 cm in one hour at -20 C → acc capped at 10 cm, SF=0.0
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 12.0, TemperatureC: -20},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 10.0, results[past1.Unix()].SnowAccumulation,
				"snowAccumulation must be capped at 10 cm")
			assert.Equal(t, 0.0, results[past1.Unix()].SnowFactor)
		}
	})

	t.Run("Snow and temperature combine correctly in projection", func(t *testing.T) {
		h := make([]time.Time, 15)
		for i := range h {
			h[i] = now.Add(time.Duration(i-7) * time.Hour)
		}
		weather := []types.Weather{{ForecastHours: make([]types.HourlyWeather, 15)}}
		for i, ts := range h {
			weather[0].ForecastHours[i] = types.HourlyWeather{TSHourStart: ts, GTI: 100, TemperatureC: 20}
		}

		// Hour 7 (middle): calibration point.
		history := []types.EnergyStats{
			{TSHourStart: h[7], SolarKWH: 1.0},
		}

		// Snow and temp adjustments
		weather[0].ForecastHours[8].SnowfallCM = 2.5
		weather[0].ForecastHours[8].TemperatureC = -5
		weather[0].ForecastHours[9].TemperatureC = 10
		weather[0].ForecastHours[10].TemperatureC = 10

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 15) {
			assert.InDelta(t, 0.109, results[h[8].Unix()].ImprovedSolar, 0.002,
				"2.5 cm snow + cold temp → 90% reduction from SnowFactor=0.1")
			assert.Equal(t, 0.1, results[h[8].Unix()].SnowFactor)

			assert.InDelta(t, 1.035, results[h[9].Unix()].ImprovedSolar, 0.005,
				"warm hour: residual 0.5 cm < 1 cm → slides off → full generation")
			assert.Equal(t, 1.0, results[h[9].Unix()].SnowFactor)
			assert.Equal(t, 0.0, results[h[9].Unix()].SnowAccumulation)

			assert.InDelta(t, 1.035, results[h[10].Unix()].ImprovedSolar, 0.005,
				"snow stays gone in subsequent hours")
		}
	})

	t.Run("Snow persists across multiple weather day records", func(t *testing.T) {
		// Two separate Weather structs (different days) merged into one timeline.
		// Snow from day 1 must carry into day 2 via sequential processing.
		// day1Hour: 3 cm at Tamb=-5 C → Tcell=-1.875 C → no melt → acc=3
		// day2Hour: Tamb=-2 C → Tcell=1.125 C (0-2 bracket) → melt 0.5 → acc=2.5, SF=0.1
		day1Hour := now.Add(-25 * time.Hour)
		day2Hour := now.Add(-1 * time.Hour)

		weather := []types.Weather{
			{ForecastHours: []types.HourlyWeather{
				{TSHourStart: day1Hour, GHI: 100, GTI: 100, SnowfallCM: 3.0, TemperatureC: -5},
			}},
			{ForecastHours: []types.HourlyWeather{
				{TSHourStart: day2Hour, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: -2},
			}},
		}

		results := calculateImprovedSolar(ctx, nil, weather)

		if assert.Len(t, results, 2) {
			assert.InDelta(t, 3.0, results[day1Hour.Unix()].SnowAccumulation, 0.001)
			assert.InDelta(t, 2.5, results[day2Hour.Unix()].SnowAccumulation, 0.001,
				"snow carries across the gap between weather day records")
		}
	})

	t.Run("GTI stored correctly on result struct", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 300, GTI: 450, TemperatureC: 15},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 450.0, results[past1.Unix()].GTI,
				"GTI must be stored so callers can inspect the irradiance value used")
		}
	})

	t.Run("SnowFactor and SnowAccumulation carried on every result entry", func(t *testing.T) {
		// Verify both fields are populated even when callerless without calibration history.
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: future1, GHI: 100, GTI: 100, SnowfallCM: 2.0, TemperatureC: -10},
			{TSHourStart: future2, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: -10},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather)
		if assert.Len(t, results, 2) {
			r1 := results[future1.Unix()]
			// Tcell at -10+(100/800)*25 = -6.875 C → no melt → acc=2.0
			assert.Equal(t, 2.0, r1.SnowAccumulation)
			assert.Equal(t, 0.1, r1.SnowFactor)

			r2 := results[future2.Unix()]
			// Still -6.875 C, no melt → acc stays 2.0
			assert.Equal(t, 2.0, r2.SnowAccumulation)
			assert.Equal(t, 0.1, r2.SnowFactor)
		}
	})
}
