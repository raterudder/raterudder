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

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

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

		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			ESS:             "mock",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

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
				Latitude:     1,
				Longitude:    1,
				TimeZone:     "UTC",
				SolarTilt:    30,
				SolarAzimuth: 180,
			},
			ESS: "mock",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{
			{Hourly: []types.EnergyStats{
				{TSHourStart: pastHour2, SolarKWH: 1.5, HomeKWH: 2.0, MinBatterySOC: 40, MaxBatterySOC: 60},
				{TSHourStart: pastHour1, SolarKWH: 2.0, HomeKWH: 3.0, MinBatterySOC: 50, MaxBatterySOC: 70},
			}},
		}, nil)
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{
			{TSStart: pastHour2, DollarsPerKWH: 0.1},
			{TSStart: pastHour1, DollarsPerKWH: 0.1},
		}, nil)
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

		foundActual := false
		foundForecast := false
		for _, w := range data.Weather {
			if w.TSHourStart.Equal(pastHour2) {
				assert.Equal(t, float64(100), w.Irradiance)
				foundActual = true
			}
			if w.TSHourStart.Equal(futureHour1) {
				assert.Equal(t, float64(200), w.Irradiance)
				foundForecast = true
			}
		}
		assert.True(t, foundActual, "should have mapped actual Irradiance")
		assert.True(t, foundForecast, "should have mapped forecast Irradiance")

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
	fixedTime := time.Date(2026, 3, 18, 18, 0, 0, 0, time.UTC)
	now := fixedTime
	loc := types.SiteLocation{
		Latitude:     41.85,
		Longitude:    -87.65,
		TimeZone:     "America/Chicago",
		SolarTilt:    30,
		SolarAzimuth: 180,
	}

	t.Run("Returns empty map when no weather provided", func(t *testing.T) {
		results := calculateImprovedSolar(ctx, []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), SolarKWH: 1.0},
		}, nil, loc)
		assert.Empty(t, results)
	})

	t.Run("Returns results with zero ImprovedSolar when no calibration history", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now, GTI: 800, GHI: 800, TemperatureC: 20},
		}}}
		results := calculateImprovedSolar(ctx, nil, weather, loc)
		if assert.Len(t, results, 1) {
			assert.Equal(t, 0.0, results[now.Unix()].ImprovedSolar)
		}
	})

	t.Run("Geometry: GTI usage near solar noon", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now, GTI: 1000, GHI: 800, TemperatureC: 20},
		}}}
		history := []types.EnergyStats{{TSHourStart: now.Add(-24 * time.Hour), SolarKWH: 3.0}}
		histWeather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now.Add(-24 * time.Hour), GTI: 1000, GHI: 800, TemperatureC: 20},
		}}}
		allWeather := append(weather, histWeather...)

		results := calculateImprovedSolar(ctx, history, allWeather, loc)
		if assert.NotNil(t, results[now.Unix()]) {
			r := results[now.Unix()]
			assert.Equal(t, 1000.0, r.Irradiance, "Irradiance should be equal to GTI")
			assert.Greater(t, r.ImprovedSolar, 0.0)
		}
	})

	t.Run("Fallbacks: Uses GHI if no GTI", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now, GTI: 0, GHI: 800, TemperatureC: 20},
		}}}
		history := []types.EnergyStats{{TSHourStart: now.Add(-24 * time.Hour), SolarKWH: 3.0}}
		histWeather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now.Add(-24 * time.Hour), GTI: 0, GHI: 800, TemperatureC: 20},
		}}}
		allWeather := append(weather, histWeather...)

		results := calculateImprovedSolar(ctx, history, allWeather, loc)
		if assert.NotNil(t, results[now.Unix()]) {
			assert.Equal(t, 800.0, results[now.Unix()].Irradiance, "Irradiance should fallback to GHI")
		}
	})

	t.Run("Temperature: NOCT clamping at -40 and 80 C", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now, GTI: 0, GHI: 0, TemperatureC: -100},                     // Extreme cold
			{TSHourStart: now.Add(time.Hour), GTI: 1000, GHI: 1000, TemperatureC: 100}, // Extreme heat
		}}}
		results := calculateImprovedSolar(ctx, nil, weather, loc)

		assert.Equal(t, -40.0, results[now.Unix()].TCell, "TCell should be clamped at -40C")
		// STC = 25C. Coeff = 0.0035.
		// If TCell = -40, TDiff = -65. TF = 1 - (-65 * 0.0035) = 1 + 0.2275 = 1.2275.
		assert.InDelta(t, 1.2275, results[now.Unix()].TempFactor, 0.001)

		assert.Equal(t, 80.0, results[now.Add(time.Hour).Unix()].TCell, "TCell should be clamped at 80C")
		// If TCell = 80, TDiff = 55. TF = 1 - (55 * 0.0035) = 1 - 0.1925 = 0.8075.
		assert.InDelta(t, 0.8075, results[now.Add(time.Hour).Unix()].TempFactor, 0.001)
	})

	t.Run("Filters: Ignores low light", func(t *testing.T) {
		// Valid baseline point
		h1 := now.Add(-24 * time.Hour)
		// Point with GTI = 15 (ignored)
		h2 := now.Add(-21 * time.Hour)
		// Point with GTI = 5 (ignored)
		h3 := now.Add(-20 * time.Hour)

		history := []types.EnergyStats{
			{TSHourStart: h1, SolarKWH: 2.0},
			{TSHourStart: h2, SolarKWH: 0.1},
			{TSHourStart: h3, SolarKWH: 0.1},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: h1, GTI: 800, TemperatureC: 20},
			{TSHourStart: h2, GTI: 15, TemperatureC: 20},
			{TSHourStart: h3, GTI: 5, TemperatureC: 20},
			{TSHourStart: now, GTI: 800, TemperatureC: 20},
		}}}

		results := calculateImprovedSolar(ctx, history, weather, loc)
		// If h2 or h3 were used, efficiency would be garbage.
		// h1 efficiency = 2.0 / (800 * TF * SF).
		assert.Greater(t, results[now.Unix()].ImprovedSolar, 1.5)
	})

	t.Run("Clipping Detection: Plateaus at peak", func(t *testing.T) {
		history := make([]types.EnergyStats, 0, 10)
		histWeather := make([]types.HourlyWeather, 0, 10)
		for i := 0; i < 8; i++ {
			ts := now.Add(time.Duration(-24-i) * time.Hour)
			history = append(history, types.EnergyStats{TSHourStart: ts, SolarKWH: 5.0})
			histWeather = append(histWeather, types.HourlyWeather{TSHourStart: ts, GTI: 1200})
		}
		tsValid := now.Add(-48 * time.Hour)
		history = append(history, types.EnergyStats{TSHourStart: tsValid, SolarKWH: 2.0})
		histWeather = append(histWeather, types.HourlyWeather{TSHourStart: tsValid, GTI: 400})

		weather := []types.Weather{{ForecastHours: append(histWeather, types.HourlyWeather{TSHourStart: now, GTI: 1500})}}

		results := calculateImprovedSolar(ctx, history, weather, loc)

		assert.Equal(t, 5.0, results[now.Unix()].ImprovedSolar, "Generation should be capped at learned 5.0 kWh")
		assert.Greater(t, results[now.Unix()].UnclippedSolar, 5.0, "Unclipped should exceed the cap")
	})

	t.Run("Clipping Detection: Clamps irradiance for clipped hours", func(t *testing.T) {
		history := make([]types.EnergyStats, 0, 10)
		histWeather := make([]types.HourlyWeather, 0, 10)
		// Establish clipping cap of 5.0 and a minClippedIrradiance of 1200
		for i := 0; i < 8; i++ {
			ts := now.Add(time.Duration(-24-i) * time.Hour)
			history = append(history, types.EnergyStats{TSHourStart: ts, SolarKWH: 5.0})
			histWeather = append(histWeather, types.HourlyWeather{TSHourStart: ts, GTI: 1200 + float64(i*50), TemperatureC: 25})
		}

		// Establish baseline efficiency from unclipped hours
		tsValid1 := now.Add(-48 * time.Hour)
		history = append(history, types.EnergyStats{TSHourStart: tsValid1, SolarKWH: 2.0})
		histWeather = append(histWeather, types.HourlyWeather{TSHourStart: tsValid1, GTI: 400, TemperatureC: 25})

		weather := []types.Weather{{ForecastHours: append(histWeather, types.HourlyWeather{TSHourStart: now, GTI: 1500, TemperatureC: 25})}}

		results := calculateImprovedSolar(ctx, history, weather, loc)

		// The minClippedIrradiance will be 1200.
		// For the 8 clipped history hours, their effective irradiance will be clamped to 1200.
		// Efficiency of the clipped hours is 5.0 / 1200 = ~0.00416.
		// This should be accurately captured as the efficiency instead of dropping due to the old 'ignore' logic
		assert.Equal(t, 5.0, results[now.Unix()].ImprovedSolar)
		assert.Greater(t, results[now.Unix()].UnclippedSolar, 5.0)
	})

	t.Run("Clipping Threshold: Not learned with 3 peaks", func(t *testing.T) {
		// Only 3 points at plateau - should NOT learn cap (requires 6)
		history := make([]types.EnergyStats, 0, 10)
		histWeather := make([]types.HourlyWeather, 0, 10)
		for i := 0; i < 3; i++ {
			ts := now.Add(time.Duration(-24-i) * time.Hour)
			history = append(history, types.EnergyStats{TSHourStart: ts, SolarKWH: 5.0})
			histWeather = append(histWeather, types.HourlyWeather{TSHourStart: ts, GTI: 1200})
		}
		tsValid := now.Add(-48 * time.Hour)
		history = append(history, types.EnergyStats{TSHourStart: tsValid, SolarKWH: 2.0})
		histWeather = append(histWeather, types.HourlyWeather{TSHourStart: tsValid, GTI: 400})

		weather := []types.Weather{{ForecastHours: append(histWeather, types.HourlyWeather{TSHourStart: now, GTI: 1500})}}

		results := calculateImprovedSolar(ctx, history, weather, loc)

		assert.Greater(t, results[now.Unix()].ImprovedSolar, 5.0, "No cap learned with only 3 peaks")
		assert.Equal(t, results[now.Unix()].UnclippedSolar, results[now.Unix()].ImprovedSolar)
	})

	t.Run("Snow: Accurate SF switch cases", func(t *testing.T) {
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now, GTI: 800, SnowDepthCM: 0.1},                     // Trace
			{TSHourStart: now.Add(time.Hour), GTI: 800, SnowDepthCM: 2.0},      // Partial
			{TSHourStart: now.Add(2 * time.Hour), GTI: 800, SnowDepthCM: 10.0}, // Blocked
		}}}
		history := []types.EnergyStats{{TSHourStart: now.Add(-24 * time.Hour), SolarKWH: 1.0}}
		histWeather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now.Add(-24 * time.Hour), GTI: 800},
		}}}
		allWeather := append(weather, histWeather...)

		results := calculateImprovedSolar(ctx, history, allWeather, loc)
		assert.Equal(t, 0.7, results[now.Unix()].SnowFactor)
		assert.Equal(t, 0.1, results[now.Add(time.Hour).Unix()].SnowFactor)
		assert.Equal(t, 0.0, results[now.Add(2*time.Hour).Unix()].SnowFactor)
	})

	t.Run("Multi-Day Averaging: Smooths out daily differences", func(t *testing.T) {
		// Day 1: High efficiency (2.0 kWh / 500 = 0.004)
		// Day 2: Low efficiency (1.0 kWh / 500 = 0.002)
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-24 * time.Hour), SolarKWH: 2.0},
			{TSHourStart: now.Add(-48 * time.Hour), SolarKWH: 1.0},
		}
		weather := []types.Weather{{ForecastHours: []types.HourlyWeather{
			{TSHourStart: now.Add(-24 * time.Hour), GTI: 500, TemperatureC: 25},
			{TSHourStart: now.Add(-48 * time.Hour), GTI: 500, TemperatureC: 25},
			{TSHourStart: now, GTI: 500, TemperatureC: 25},
		}}}

		results := calculateImprovedSolar(ctx, history, weather, loc)
		// Average efficiency should be ~0.003
		// Forecast generation = 500 * 0.003 = 1.5
		assert.InDelta(t, 1.5, results[now.Unix()].ImprovedSolar, 0.1)
	})
}
