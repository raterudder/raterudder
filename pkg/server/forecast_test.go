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
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil)

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
		mockUMap.SetProvider("test", mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		// Inject siteID
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "private, max-age=300", resp.Header.Get("Cache-Control"))

		var hours []controller.SimHour
		err := json.NewDecoder(resp.Body).Decode(&hours)
		require.NoError(t, err)
		assert.Len(t, hours, 24, "should return exactly 24 simulated hours")

		// Verify all expected mocks were called
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
		// Inject siteID
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
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)

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
		// Inject siteID
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
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)

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
		mockUMap.SetProvider("test", mockU)

		srv := &Server{
			storage:    mockS,
			ess:        mockP,
			utilities:  mockUMap,
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		// Inject siteID
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
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil)

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
		mockUMap.SetProvider("test", mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/forecast", nil)
		// Inject siteID
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleForecast(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Verify no backfill-related calls were made
		mockS.AssertNotCalled(t, "GetLatestEnergyHistoryTime")
		mockS.AssertNotCalled(t, "GetLatestPriceHistoryTime")
		mockS.AssertNotCalled(t, "UpsertEnergyHistories")
		mockS.AssertNotCalled(t, "UpsertPrices")
		mockS.AssertNotCalled(t, "InsertAction")
		mockES.AssertNotCalled(t, "GetEnergyHistory")
		mockES.AssertNotCalled(t, "SetModes")
		mockU.AssertNotCalled(t, "GetConfirmedPrices")
	})
}
