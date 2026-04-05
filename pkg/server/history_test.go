package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// historyMockStorage extends verify usage and allows returning data
type historyMockStorage struct {
	*mockStorage
	actions []types.Action
	prices  []types.Price
	err     error

	lastStart time.Time
	lastEnd   time.Time
}

func (m *historyMockStorage) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	m.lastStart = start
	m.lastEnd = end
	return m.actions, m.err
}

func (m *historyMockStorage) GetPriceHistory(ctx context.Context, provider string, start, end time.Time) ([]types.Price, error) {
	m.lastStart = start
	m.lastEnd = end
	return m.prices, m.err
}

func TestHistory(t *testing.T) {
	mockU := &mockUtility{}
	// mockStorage is defined in mock_test.go. We can embed it to satisfy the interface.
	// But we need to use historyMockStorage to override methods.
	mockSBase := &mockStorage{}
	// We need to set expectations on the base mock if it's called
	mockSBase.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)

	mockS := &historyMockStorage{
		mockStorage: mockSBase,
	}

	mockE := &mockESS{}
	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockE)

	mockUMap := utility.NewMap(nil)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	srv := &Server{
		utilities:          mockUMap,
		ess:                mockP,
		storage:            mockS,
		listenAddr:         ":8080",
		controller:         controller.NewController(),
		bypassAuth:         true,
		singleSite:         true,
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
	}

	handler := srv.setupHandler()

	t.Run("Parse Dates", func(t *testing.T) {
		tests := []struct {
			name       string
			start      string
			end        string
			statusCode int
			errMsg     string
		}{
			{
				name:       "Invalid Start String",
				start:      "invalid",
				end:        time.Now().Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid start time",
			},
			{
				name:       "Invalid End String",
				start:      time.Now().Add(-time.Hour).Format(time.RFC3339),
				end:        "invalid",
				statusCode: http.StatusBadRequest,
				errMsg:     "invalid end time",
			},
			{
				name:       "End Before Start",
				start:      time.Now().Format(time.RFC3339),
				end:        time.Now().Add(-time.Hour).Format(time.RFC3339),
				statusCode: http.StatusBadRequest,
				errMsg:     "start time must be before end time",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := make(url.Values)
				q.Set("start", tt.start)
				q.Set("end", tt.end)
				u := "/api/history/actions?" + q.Encode()

				req := httptest.NewRequest("GET", u, nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				resp := w.Result()
				assert.Equal(t, tt.statusCode, resp.StatusCode)
				if tt.statusCode != http.StatusOK {
					var errResp struct {
						Error string `json:"error"`
					}
					require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
					assert.Contains(t, errResp.Error, tt.errMsg)
				}
			})
		}
	})

	t.Run("Parse Dates Default", func(t *testing.T) {
		// Test the default behavior when start and end parameters are omitted
		req := httptest.NewRequest("GET", "/api/history/actions", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()

		// Verify the request succeeds
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Assert that parseTimeRange correctly defaulted end to time.Now() and start to 24 hours prior
		// Using a 1-second tolerance to account for the time difference between the request and the assertion
		now := time.Now()
		assert.WithinDuration(t, now, mockS.lastEnd, time.Second)
		assert.WithinDuration(t, now.Add(-24*time.Hour), mockS.lastStart, time.Second)
	})

	t.Run("Validate 24 Hour Limit", func(t *testing.T) {
		start := time.Now().Add(-25 * time.Hour)
		end := time.Now()

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
		assert.Contains(t, errResp.Error, "time range cannot exceed 24 hours")
	})

	t.Run("Fetch Actions Data", func(t *testing.T) {
		now := time.Now()
		expectedActions := []types.Action{
			{
				Timestamp:         now.Add(-30 * time.Minute),
				BatteryMode:       types.BatteryModeChargeSolar,
				SolarMode:         types.SolarModeAny,
				TargetBatteryMode: types.BatteryModeStandby,
				TargetSolarMode:   types.SolarModeNoExport,
				Reason:            types.ActionReasonSufficientBattery,
				Description:       "Solar charging",
				DryRun:            true,
				Fault:             false,
				Failed:            false,
				Paused:            false,
				Error:             "",
			},
		}
		mockS.actions = expectedActions
		mockS.err = nil

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var actions []types.Action
		err := json.NewDecoder(resp.Body).Decode(&actions)
		require.NoError(t, err)
		assert.Len(t, actions, 1)
		assert.Equal(t, expectedActions[0].BatteryMode, actions[0].BatteryMode)
		assert.Equal(t, expectedActions[0].SolarMode, actions[0].SolarMode)
		assert.Equal(t, expectedActions[0].TargetBatteryMode, actions[0].TargetBatteryMode)
		assert.Equal(t, expectedActions[0].TargetSolarMode, actions[0].TargetSolarMode)
		assert.Equal(t, expectedActions[0].Reason, actions[0].Reason)
		assert.Equal(t, expectedActions[0].Description, actions[0].Description)
		assert.Equal(t, expectedActions[0].DryRun, actions[0].DryRun)
		assert.Equal(t, expectedActions[0].Fault, actions[0].Fault)
		assert.Equal(t, expectedActions[0].Failed, actions[0].Failed)
		assert.Equal(t, expectedActions[0].Paused, actions[0].Paused)
		assert.Equal(t, expectedActions[0].Error, actions[0].Error)

		// Verify storage call
		// GetActionHistory should be called with siteID as well
		assert.WithinDuration(t, start, mockS.lastStart, time.Second)
		assert.WithinDuration(t, end, mockS.lastEnd, time.Second)
	})

	t.Run("Fetch Actions Data Error", func(t *testing.T) {
		// Mock GetActionHistory to return a storage error to verify the 500 status code response
		now := time.Now()

		// Save original state to restore it after the test
		originalActions := mockS.actions
		originalErr := mockS.err
		defer func() {
			mockS.actions = originalActions
			mockS.err = originalErr
		}()

		mockS.actions = nil
		mockS.err = fmt.Errorf("storage error")

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		// Ensure the error message specifically mentions the failure action
		assert.Contains(t, errResp.Error, "failed to get actions")
	})

	t.Run("Fetch Prices Data", func(t *testing.T) {
		now := time.Now()
		expectedPrices := []types.Price{
			{
				Provider:                      "test-provider",
				TSStart:                       now.Add(-30 * time.Minute),
				TSEnd:                         now,
				DollarsPerKWH:                 0.12,
				GridUseDollarsPerKWH:          0.05,
				GenerationCreditDollarsPerKWH: 0.08,
				SeparateGenerationCredit:      true,
			},
		}
		mockS.prices = expectedPrices
		mockS.err = nil

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var prices []types.Price
		err := json.NewDecoder(resp.Body).Decode(&prices)
		require.NoError(t, err)
		assert.Len(t, prices, 1)
		assert.Equal(t, expectedPrices[0].Provider, prices[0].Provider)
		assert.Equal(t, expectedPrices[0].DollarsPerKWH, prices[0].DollarsPerKWH)
		assert.Equal(t, expectedPrices[0].GridUseDollarsPerKWH, prices[0].GridUseDollarsPerKWH)
		assert.Equal(t, expectedPrices[0].GenerationCreditDollarsPerKWH, prices[0].GenerationCreditDollarsPerKWH)
		assert.Equal(t, expectedPrices[0].SeparateGenerationCredit, prices[0].SeparateGenerationCredit)

		// Verify storage call
		assert.WithinDuration(t, start, mockS.lastStart, time.Second)
		assert.WithinDuration(t, end, mockS.lastEnd, time.Second)
	})

	t.Run("Fetch Prices Data Error", func(t *testing.T) {
		// Mock GetPriceHistory to return a storage error to verify the 500 status code response
		now := time.Now()

		// Save original state to restore it after the test
		originalPrices := mockS.prices
		originalErr := mockS.err
		defer func() {
			mockS.prices = originalPrices
			mockS.err = originalErr
		}()

		mockS.prices = nil
		mockS.err = fmt.Errorf("storage error")

		start := now.Add(-time.Hour)
		end := now

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		var errResp struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(resp.Body).Decode(&errResp)
		require.NoError(t, err)
		// Ensure the error message specifically mentions the failure action
		assert.Contains(t, errResp.Error, "failed to get prices")
	})

	t.Run("Cache Control Today", func(t *testing.T) {
		// End time is now
		now := time.Now()
		start := now.Add(-time.Hour)
		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", now.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Cache Control Past", func(t *testing.T) {
		// End time is yesterday
		end := time.Now().Add(-25 * time.Hour)
		start := end.Add(-time.Hour)

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/actions?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=86400", resp.Header.Get("Cache-Control"))
	})

	t.Run("Prices Cache Control Today", func(t *testing.T) {
		// End time is now, which overlaps with the current day, meaning cache should be short (1 min)
		now := time.Now()
		start := now.Add(-time.Hour)
		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", now.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=60", resp.Header.Get("Cache-Control"))
	})

	t.Run("Prices Cache Control Past", func(t *testing.T) {
		// End time is yesterday, so data is final and can be cached longer (24 hrs)
		end := time.Now().Add(-25 * time.Hour)
		start := end.Add(-time.Hour)

		q := make(url.Values)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		u := "/api/history/prices?" + q.Encode()

		req := httptest.NewRequest("GET", u, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=86400", resp.Header.Get("Cache-Control"))
	})
}

func TestHandleHistoryEnergy(t *testing.T) {
	mockS := &mockStorage{}
	srv := &Server{
		storage:            mockS,
		bypassAuth:         true,
		singleSite:         true,
		controller:         controller.NewController(),
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
	}

	handler := srv.setupHandler()

	t.Run("Success", func(t *testing.T) {
		targetDate := "2023-10-27"
		d, err := time.Parse("2006-01-02", targetDate)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, nil).Once()

		startQuery := d.AddDate(0, 0, -4)
		endQuery := d.AddDate(0, 0, 1)

		// Energy History
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, startQuery, endQuery).Return([]types.DailyEnergyStats{
			{TSDayStart: d, Hourly: []types.EnergyStats{{TSHourStart: d, SolarKWH: 5.2, MaxBatterySOC: 85.0}}},
		}, nil).Once()

		actualStart := d.AddDate(0, 0, -3)
		actualEnd := d.AddDate(0, 0, 1)

		// Weather
		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, actualStart, actualEnd).Return([]types.Weather{
			{
				TSDayStart: d,
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: d, TemperatureC: 15, DNI: 500, DHI: 100},
				},
			},
		}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var res HistoryEnergyRes
		err = json.Unmarshal(w.Body.Bytes(), &res)
		require.NoError(t, err)

		require.Len(t, res.Energy, 1)
		assert.Equal(t, 5.2, res.Energy[0].SolarKWH)
		assert.Equal(t, 85.0, res.Energy[0].MaxBatterySOC)
		require.Len(t, res.Weather, 1)
		assert.Equal(t, d.Unix(), res.Weather[0].TSHourStart.Unix())
	})

	t.Run("Invalid Date", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/history/energy?date=invalid", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid date format")
	})

	t.Run("Energy Cache Control Today", func(t *testing.T) {
		// End time is now, which overlaps with the current day, meaning cache should be short (5 mins)
		now := time.Now()
		targetDate := now.Format("2006-01-02")
		today := truncateDay(now)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, nil).Once()

		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]types.DailyEnergyStats{
			{Hourly: []types.EnergyStats{
				{TSHourStart: today, SolarKWH: 5.2, MaxBatterySOC: 85.0},
			}},
		}, nil).Once()

		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: today, TemperatureC: 15, DNI: 500, DHI: 100},
				},
			},
		}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "private, max-age=300", w.Header().Get("Cache-Control"))
	})

	t.Run("Energy Cache Control Past", func(t *testing.T) {
		// End time is in the past, so data is final and can be cached longer (24 hrs)
		past := time.Now().Add(-48 * time.Hour)
		targetDate := past.Format("2006-01-02")
		d, err := time.Parse("2006-01-02", targetDate)
		require.NoError(t, err)
		dUTC := d.UTC()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone:     "UTC",
				Latitude:     41.8781,
				Longitude:    -87.6298,
				SolarTilt:    20,
				SolarAzimuth: 180,
			},
		}, types.CurrentSettingsVersion, nil).Once()

		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]types.DailyEnergyStats{
			{Hourly: []types.EnergyStats{{TSHourStart: dUTC, SolarKWH: 5.2, MaxBatterySOC: 85.0}}},
		}, nil).Once()

		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return([]types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: dUTC, TemperatureC: 15, DNI: 500, DHI: 100},
				},
			},
		}, nil).Once()

		req := httptest.NewRequest("GET", "/api/history/energy?date="+targetDate, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "private, max-age=86400", w.Header().Get("Cache-Control"))
	})
}
