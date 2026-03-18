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
	now := time.Now().Truncate(time.Hour)
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

	t.Run("Calculates peak efficiency and projects future hours", func(t *testing.T) {
		// past2: actual=1.0, GTI=100, Tamb=20 C
		//   Tcell = 20 + (100/800)*25 = 23.125 C → TF = 1 - (23.125-25)*0.0035 = 1.0066
		//   eff   = 1.0 / (100 * 1.0066 * 1.0) = 0.009934
		//
		// past1: actual=2.2, GTI=200, Tamb=20 C  ← PEAK
		//   Tcell = 20 + (200/800)*25 = 26.25 C  → TF = 1 - (26.25-25)*0.0035 = 0.995625
		//   eff   = 2.2 / (200 * 0.995625) = 0.011048
		//
		// future1: GTI=300, Tamb=20 C
		//   Tcell = 20 + (300/800)*25 = 29.375 C → TF = 1 - (29.375-25)*0.0035 = 0.984688
		//   ImprovedSolar = 300 * 0.011048 * 0.984688 * 1.0 ≈ 3.26
		history := []types.EnergyStats{
			{TSHourStart: past2, SolarKWH: 1.0},
			{TSHourStart: past1, SolarKWH: 2.2},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: past1, GHI: 200, GTI: 200, TemperatureC: 20},
			{TSHourStart: future1, GHI: 300, GTI: 300, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 3) {
			assert.InDelta(t, 3.26, results[future1.Unix()].ImprovedSolar, 0.01,
				"projected future solar should use peak efficiency from past1")
		}
	})

	t.Run("Projects historical hours too (for algorithm vs. reality comparison)", func(t *testing.T) {
		// Both timestamps are in the past; the function must project all hours so callers
		// can overlay the model prediction against actual readings.
		//
		// past1 is the peak hour; its projection should closely match its actual value.
		// past2 was not the peak but still gets a projection applied.
		history := []types.EnergyStats{
			{TSHourStart: past2, SolarKWH: 1.0},
			{TSHourStart: past1, SolarKWH: 2.2},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: past1, GHI: 200, GTI: 200, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.Greater(t, results[past2.Unix()].ImprovedSolar, 0.0,
				"historical hours should receive an ImprovedSolar value for comparison")
			// peak hour projection should track closely to the actual (within ~15%)
			assert.InDelta(t, 2.2, results[past1.Unix()].ImprovedSolar, 0.33,
				"peak hour projection should be close to the actual reading")
		}
	})

	t.Run("Applies temperature efficiency loss via NOCT at high irradiance", func(t *testing.T) {
		// past1: GTI=100, Tamb=20 C → Tcell=23.125 C, TF=1.0066, eff=0.009934
		// future1: GTI=800, Tamb=20 C
		//   Tcell = 20 + (800/800)*25 = 45 C → TF = 1 - (45-25)*0.0035 = 0.93
		//   ImprovedSolar = 800 * 0.009934 * 0.93 ≈ 7.39
		// At STC (25 C) we would get ~7.95; the 7% temperature penalty at 45 C reduces it.
		history := []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 800, GTI: 800, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.InDelta(t, 7.39, results[future1.Unix()].ImprovedSolar, 0.02)
			assert.InDelta(t, 0.93, results[future1.Unix()].TempFactor, 0.001)
		}
	})

	t.Run("Applies temperature efficiency gain at cold temperature", func(t *testing.T) {
		// past1: GTI=100, Tamb=20 C → eff=0.009934
		// future1: GTI=100, Tamb=-10 C
		//   Tcell = -10 + (100/800)*25 = -6.875 C
		//   TF = 1 - (-6.875-25)*0.0035 = 1 + 0.11156 = 1.1116
		//   ImprovedSolar = 100 * 0.009934 * 1.1116 ≈ 1.104
		history := []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 100, GTI: 100, TemperatureC: -10},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.Greater(t, results[future1.Unix()].ImprovedSolar, 1.0,
				"cold panels run above rated efficiency → output exceeds baseline")
			assert.InDelta(t, 1.104, results[future1.Unix()].ImprovedSolar, 0.01)
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
		// When GTI=0 (no tilt data) the function should use GHI instead.
		// past1: GHI=100. future1: GHI=200. Both Tamb=20 C.
		// eff = 1.0 / (100 * 1.0066) = 0.009934
		// ImprovedSolar = 200 * 0.009934 * TF_future1
		//   TF_future1 at GHI=200 → Tcell=20+(200/800)*25=26.25 C → TF=0.995625
		// ImprovedSolar = 200 * 0.009934 * 0.995625 ≈ 1.979
		history := []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 0, TemperatureC: 20},
			{TSHourStart: future1, GHI: 200, GTI: 0, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.InDelta(t, 1.979, results[future1.Unix()].ImprovedSolar, 0.02,
				"GTI=0 should fall back to GHI for both calibration and projection")
			assert.Equal(t, 100.0, results[past1.Unix()].GTI,
				"GHI value should be stored as effective GTI when GTI is zero")
		}
	})

	t.Run("Applies GTI correction from tilt model (higher GTI than GHI)", func(t *testing.T) {
		// past1: actual=1.2, GTI=100, Tamb=20 C
		//   Tcell=23.125, TF=1.0066, eff=1.2/(100*1.0066)=0.011920
		// future1: GTI=200, Tamb=20 C
		//   Tcell=26.25, TF=0.995625
		//   ImprovedSolar = 200 * 0.011920 * 0.995625 ≈ 2.375
		history := []types.EnergyStats{
			{TSHourStart: past1, SolarKWH: 1.2},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 100, GTI: 200, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 2) {
			assert.InDelta(t, 2.375, results[future1.Unix()].ImprovedSolar, 0.01,
				"higher GTI from tilt should proportionally increase projected generation")
		}
	})

	t.Run("Ignores calibration hours with low irradiance (GTI < 50)", func(t *testing.T) {
		// past2 has GTI=40 (below threshold) and actual=10.0.
		// If it were accepted, eff would be ~0.249 and projections would be wildly inflated.
		// Only past1 (GTI=100) should calibrate: eff=1.0/(100*1.0066)=0.009934.
		history := []types.EnergyStats{
			{TSHourStart: past2, SolarKWH: 10.0},
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 40, GTI: 40, TemperatureC: 20},
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 100, GTI: 100, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 3) {
			// If dawn noise were accepted, ImprovedSolar would be ~25x too large.
			assert.InDelta(t, 1.0, results[future1.Unix()].ImprovedSolar, 0.02,
				"low-GTI noise must not inflate the calibrated efficiency")
		}
	})

	t.Run("Ignores calibration hours with negligible actual solar (<= 0.1 kWh)", func(t *testing.T) {
		// past2 actual=0.05 kWh is below the noise floor and must not be used.
		// Calibration must come from past1 only: eff=1.0/(100*1.0066)=0.009934.
		history := []types.EnergyStats{
			{TSHourStart: past2, SolarKWH: 0.05},
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 200, GTI: 200, TemperatureC: 20},
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 100, GTI: 100, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 3) {
			assert.InDelta(t, 1.0, results[future1.Unix()].ImprovedSolar, 0.02)
		}
	})

	t.Run("Peak efficiency is the maximum observed, not the last observed", func(t *testing.T) {
		// past2 has higher actual/GTI than past1 even though past1 is processed later.
		// past2: actual=3.0, GTI=100, TF=1.0066 → eff=3.0/(100*1.0066)=0.02980 ← PEAK
		// past1: actual=1.0, GTI=100, TF=1.0066 → eff=0.009934
		// future1 at GTI=100, TF=1.0066 → should use eff from past2 → ~3.0
		history := []types.EnergyStats{
			{TSHourStart: past2, SolarKWH: 3.0},
			{TSHourStart: past1, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: past1, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: future1, GHI: 100, GTI: 100, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 3) {
			assert.InDelta(t, 3.0, results[future1.Unix()].ImprovedSolar, 0.05,
				"peak hour (past2) must win over later lower-efficiency hour (past1)")
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
		// past2: calibration point. GTI=100, Tamb=20 C, actual=1.0
		//   Tcell=23.125 C, TF=1.0066, eff=0.009934
		//
		// past1: GTI=100, Tamb=-5 C, 2.5 cm snow
		//   Tcell=-1.875 C → no melt → acc=2.5, SF=0.1, TF=1.0941
		//   ImprovedSolar = 100 * 0.009934 * 1.0941 * 0.1 ≈ 0.1087
		//
		// now: GTI=100, Tamb=10 C
		//   Tcell=13.125 C (>5) → 2.5-2=0.5 < 1 → slide-off → acc=0, SF=1.0, TF=1.0416
		//   ImprovedSolar = 100 * 0.009934 * 1.0416 * 1.0 ≈ 1.035
		//
		// future1: GTI=100, Tamb=10 C. acc=0 (no new snow), SF=1.0, TF=1.0416
		//   ImprovedSolar ≈ 1.035
		history := []types.EnergyStats{
			{TSHourStart: past2, SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: past2, GHI: 100, GTI: 100, TemperatureC: 20},
			{TSHourStart: past1, GHI: 100, GTI: 100, SnowfallCM: 2.5, TemperatureC: -5},
			{TSHourStart: now, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 10},
			{TSHourStart: future1, GHI: 100, GTI: 100, SnowfallCM: 0, TemperatureC: 10},
		}}}

		results := calculateImprovedSolar(ctx, history, weather)

		if assert.Len(t, results, 4) {
			assert.InDelta(t, 0.109, results[past1.Unix()].ImprovedSolar, 0.002,
				"2.5 cm snow + cold temp → 90% reduction from SnowFactor=0.1")
			assert.Equal(t, 0.1, results[past1.Unix()].SnowFactor)

			assert.InDelta(t, 1.035, results[now.Unix()].ImprovedSolar, 0.005,
				"warm hour: residual 0.5 cm < 1 cm → slides off → full generation")
			assert.Equal(t, 1.0, results[now.Unix()].SnowFactor)
			assert.Equal(t, 0.0, results[now.Unix()].SnowAccumulation)

			assert.InDelta(t, 1.035, results[future1.Unix()].ImprovedSolar, 0.005,
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
