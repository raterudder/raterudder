package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/utility"
)

func TestHandleUpdate(t *testing.T) {
	// Scenario: High price -> Should Discharge
	mockU := &mockUtility{}
	// Replaced mock.Anything with explicit matching for the expected UtilityProvider from settings
	mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool { return s.UtilityProvider == "test" })).Return(nil)
	mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
	mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

	mockS := &mockStorage{}
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          true,
		MinBatterySOC:   5.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, nil)
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockES := &mockESS{}
	mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
	// Add GetEnergyHistory expectation
	mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
	// We might need strict matching for SetModes later, but for now:
	mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockP := ess.NewMap()
	mockP.SetSystem(types.SiteIDNone, mockES)

	mockUMap := utility.NewMap(mockS)
	mockUMap.SetProvider(types.SiteIDNone, mockU)

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockP,
		storage:    mockS,
		listenAddr: ":8080",
		controller: controller.NewController(),
		bypassAuth: true,
	}

	req := httptest.NewRequest("GET", "/api/update", nil)
	req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
	w := httptest.NewRecorder()

	srv.handleUpdate(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	t.Run("Handle Update - Auth", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{DryRun: true, UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		// InsertAction might not be called if validation fails, so we can't strict expect it or we use .Maybe()
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		// Mock GetUser for notadmin check
		mockS.On("GetUser", mock.Anything, "notadmin@example.com").Return(types.User{}, fmt.Errorf("user not found"))

		// Helper to create server with auth config
		newAuthServer := func(audience, email string, adminEmails []string, srvURL string) *Server {
			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			provider, err := oidc.NewProvider(context.Background(), srvURL)
			require.NoError(t, err)

			return &Server{
				utilities:           mockUMap,
				ess:                 mockP,
				storage:             mockS,
				controller:          controller.NewController(),
				updateSpecificEmail: email,
				adminEmails:         adminEmails,
				oidcVerifiers: map[string]tokenVerifier{
					"google": provider.Verifier(&oidc.Config{ClientID: audience}).Verify,
				},
				singleSite: true,
			}
		}

		t.Run("Missing Authorization Header - Specific Email", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Invalid Authorization Header Format", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			req.Header.Set("Authorization", "Basic user:pass")
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		})

		t.Run("Invalid Token", func(t *testing.T) {
			oidcSrv, _ := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "check@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			req.Header.Set("Authorization", "Bearer bad-token")
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Admin Email Fallback - Valid", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "", []string{"admin@example.com"}, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "admin@example.com", "admin")
			req.Header.Set("Authorization", "Bearer "+validToken)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()

			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})

		t.Run("Valid Token, Specific Email Wrong", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "right@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "wrong@example.com", "wrong")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("Valid Token, Correct Specific Email", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "right@example.com", nil, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			validToken := generateTestToken(t, oidcSrv.URL, priv, "right@example.com", "right")
			req.Header.Set("Authorization", "Bearer "+validToken)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()

			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})

		t.Run("Admin Email Fallback - Invalid", func(t *testing.T) {
			oidcSrv, priv := setupOIDCTest(t)
			defer oidcSrv.Close()
			srv := newAuthServer("my-audience", "", []string{"admin@example.com"}, oidcSrv.URL)
			req := httptest.NewRequest("GET", "/api/update", nil)
			// Token for a different user
			validToken := generateTestToken(t, oidcSrv.URL, priv, "notadmin@example.com", "notadmin")
			req.Header.Set("Authorization", "Bearer "+validToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})

		t.Run("No Auth Configured - Blocked", func(t *testing.T) {
			// In the new model, we always have oidcVerifiers if we use newAuthServer
			// so we just test with a bad token or missing header (already tested)
			// But for completeness, we can create a server with empty verifiers
			srv := &Server{
				storage:    mockS,
				utilities:  utility.NewMap(mockS),
				ess:        ess.NewMap(),
				singleSite: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdate))
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
		})
	})

	t.Run("Paused Updates", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			Pause:           true,
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		// GetStatus should be called even when paused
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 75}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		// Expect a paused action to be inserted with paused=true
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Paused && a.Description == "Automation is paused"
		})).Return(nil)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		// Status should still be "paused"
		assert.Equal(t, "paused", resp["status"])
		// An action should be returned with the paused flag
		require.NotNil(t, resp["action"], "a paused action should be returned")
		actionMap, ok := resp["action"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, actionMap["paused"], "action should have paused=true")

		// GetStatus and GetCurrentPrice should have been called even when paused
		mockES.AssertCalled(t, "GetStatus", mock.Anything)
		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		// SetModes should NOT be called
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Emergency Mode", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{EmergencyMode: true}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "In emergency mode" && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "emergency mode", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Alarms Present", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
			Alarms: []types.SystemAlarm{{Name: "Test Alarm"}},
		}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "1 alarms present" && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "alarms present", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Action - Grid Unavailable", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{GridUnavailable: true}, nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		// Expect InsertAction with specific description
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.MatchedBy(func(a types.Action) bool {
			return a.Description == "Grid is unavailable" && a.Reason == types.ActionReasonGridUnavailable && a.Fault
		})).Return(nil)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: time.Now()}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()
		srv.handleUpdate(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		assert.Equal(t, "grid unavailable", resp["status"])

		mockU.AssertCalled(t, "GetCurrentPrice", mock.Anything)
		mockES.AssertNotCalled(t, "SetModes")
	})

	t.Run("Handle Update - Backfill Logic", func(t *testing.T) {
		t.Run("Version Mismatch - Backfill Triggered", func(t *testing.T) {
			mockS := &mockStorage{}
			// Set up old version
			lastTime := time.Date(2023, 10, 27, 12, 0, 0, 0, time.UTC)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(lastTime, 0, nil) // Version 0 < CurrentVersion
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(lastTime, 0, nil)
			mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			// Expect backfill from 5 days ago, not lastTime
			// We can verify this by checking the start time in GetEnergyHistory call to ESS
			// But for now, let's just ensure it calls GetEnergyHistory
			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Capture arguments to Verify start time
			var startTimes []time.Time
			mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				startTimes = append(startTimes, args.Get(1).(time.Time))
			}).Return([]types.DailyEnergyStats{}, nil)

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, nil)
			mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			// Other storage expectations
			mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				listenAddr: ":8080",
				controller: controller.NewController(),
				bypassAuth: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()
			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)

			// Verify that at least one call started from ~5 days ago (midnight)
			require.NotEmpty(t, startTimes, "GetEnergyHistory should have been called")
			earliest := startTimes[0]
			for _, t := range startTimes {
				if t.Before(earliest) {
					earliest = t
				}
			}
			now := time.Now()
			fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
			expected := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())
			assert.Equal(t, expected, earliest, "Backfill should start from midnight 5 days ago")
		})

		t.Run("Version Match - No Backfill", func(t *testing.T) {
			mockS := &mockStorage{}
			// Set up current version
			// Make lastTime recent enough that it would normally just resume from there
			lastTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(lastTime, types.CurrentEnergyStatsVersion, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(lastTime, types.CurrentPriceHistoryVersion, nil)
			mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			mockES := &mockESS{}
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
			mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			// Capture arguments to Verify start time
			mockES.On("GetEnergyHistory", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
				// Should be equal to lastTime (or close to it due to truncation logic)
				return start.Equal(lastTime)
			}), mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()

			mockP := ess.NewMap()
			mockP.SetSystem(types.SiteIDNone, mockES)

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{}, nil)
			mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
			mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

			mockUMap := utility.NewMap(mockS)
			mockUMap.SetProvider(types.SiteIDNone, mockU)

			// Other storage expectations
			mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
			mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
			mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			srv := &Server{
				utilities:  mockUMap,
				ess:        mockP,
				storage:    mockS,
				listenAddr: ":8080",
				controller: controller.NewController(),
				bypassAuth: true,
			}
			req := httptest.NewRequest("GET", "/api/update", nil)
			req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
			w := httptest.NewRecorder()
			srv.handleUpdate(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})
	})

	t.Run("Weather Query Future Range", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		timeLoc := "America/Chicago"

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:          true,
			MinBatterySOC:   5.0,
			UtilityProvider: "test",
			Location: &types.SiteLocation{
				Latitude:    41.8781,
				Longitude:   -87.6298,
				TimeZone:    timeLoc,
				PostalCode:  "60601",
				CountryCode: "US",
			},
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// Mock the weather update call
		mockW := &mockWeather{}
		mockW.On("Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		// Verify GetWeather is called with an end time in the future
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(end time.Time) bool {
			// end time should be in the future (roughly now + 2 days)
			return end.After(time.Now().Add(24 * time.Hour))
		})).Return([]types.Weather{}, nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			weather:    mockW,
			listenAddr: ":8080",
			controller: controller.NewController(),
			bypassAuth: true,
		}

		req := httptest.NewRequest("GET", "/api/update", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdate(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		mockS.AssertExpectations(t)
	})
}

func TestHandleUpdateSites(t *testing.T) {
	mockU := &mockUtility{}
	mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
	mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

	mockS := &mockStorage{}
	mockS.On("ListSites", mock.Anything).Return([]types.Site{
		{ID: "site1"},
		{ID: "site2"},
		{ID: "site3"},
	}, nil)

	// In production mode (default), site1 and site3 (default) should run. site2 should be skipped.
	mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{ESS: "mock", UtilityProvider: "test", Release: "production"}, types.CurrentSettingsVersion, nil)
	mockS.On("GetSettings", mock.Anything, "site2").Return(types.Settings{ESS: "mock", UtilityProvider: "test", Release: "staging"}, types.CurrentSettingsVersion, nil)
	mockS.On("GetSettings", mock.Anything, "site3").Return(types.Settings{ESS: "mock", UtilityProvider: "test", Release: "production"}, types.CurrentSettingsVersion, nil)

	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Now().Add(-1*time.Hour), types.CurrentPriceHistoryVersion, nil)
	mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockS.On("InsertAction", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockES := &mockESS{}
	mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
	mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
	mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
	mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
	mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockP := ess.NewMap()
	mockP.SetSystem("site1", mockES)
	mockP.SetSystem("site3", mockES)

	mockUMap := utility.NewMap(mockS)
	mockUMap.SetProvider("site1", mockU)
	mockUMap.SetProvider("site2", mockU)
	mockUMap.SetProvider("site3", mockU)

	srv := &Server{
		utilities:  mockUMap,
		ess:        mockP,
		storage:    mockS,
		listenAddr: ":8080",
		controller: controller.NewController(),
		bypassAuth: true,
		release:    "production",
	}

	req := httptest.NewRequest("POST", "/api/updateSites", nil)
	w := httptest.NewRecorder()

	srv.handleUpdateSites(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var results map[string]string
	err := json.NewDecoder(w.Body).Decode(&results)
	require.NoError(t, err)

	assert.Equal(t, "success", results["site1"])
	assert.NotContains(t, results, "site2")
	assert.Equal(t, "success", results["site3"])

	// Verify caching: GetCurrentPrice should be called twice (once for site1 and once for site3)
	mockU.AssertNumberOfCalls(t, "GetCurrentPrice", 2)

	t.Run("Staging Release", func(t *testing.T) {
		srv.release = "staging"
		// Reset mocks if necessary, but here we just want to verify site2 is picked up
		mockS.On("GetSettings", mock.Anything, "site2").Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
		mockP.SetSystem("site2", mockES)

		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)

		assert.NotContains(t, results, "site1")
		assert.Equal(t, "success", results["site2"])
		assert.NotContains(t, results, "site3")
	})

	t.Run("No ESS Configured", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("ListSites", mock.Anything).Return([]types.Site{{ID: "site-no-ess"}}, nil)
		mockS.On("GetSettings", mock.Anything, "site-no-ess").Return(types.Settings{
			Release: "production",
			ESS:     "", // Empty ESS
		}, types.CurrentSettingsVersion, nil)

		srv := &Server{
			storage:    mockS,
			release:    "production",
			bypassAuth: true,
		}

		req := httptest.NewRequest("POST", "/api/updateSites", nil)
		w := httptest.NewRecorder()
		srv.handleUpdateSites(w, req)

		var results map[string]string
		err := json.NewDecoder(w.Body).Decode(&results)
		require.NoError(t, err)
		assert.Equal(t, "skipped: no ESS configured", results["site-no-ess"])
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		updateEmail := "updater@example.com"
		token := generateTestToken(t, oidcSrv.URL, priv, updateEmail, "updater")

		mockS := &mockStorage{}
		mockS.On("ListSites", mock.Anything).Return([]types.Site{{ID: "site1"}}, nil)
		mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{
			Release:         "production",
			ESS:             "mock",
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, nil)

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Return(nil)

		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 80}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.15, TSStart: time.Now()}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{{DollarsPerKWH: 0.15, TSStart: time.Now().Add(time.Hour)}}, nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)

		srv := &Server{
			storage:             mockS,
			utilities:           mockUMap,
			ess:                 mockP,
			controller:          controller.NewController(),
			release:             "production",
			updateSpecificEmail: updateEmail,
			oidcVerifiers: map[string]tokenVerifier{
				"google_update_specific": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
			oidcAudiences: map[string]string{
				"google_update_specific": "test-audience",
			},
		}

		t.Run("Valid Token", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("Mismatch Token", func(t *testing.T) {
			badToken := generateTestToken(t, oidcSrv.URL, priv, "wrong@example.com", "wrong")
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			req.Header.Set("Authorization", "Bearer "+badToken)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Missing Token", func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/updateSites", nil)
			w := httptest.NewRecorder()

			handler := srv.authMiddleware(http.HandlerFunc(srv.handleUpdateSites))
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	})
}

// Helpers for Recording Mocks
type RecordingMockESS struct {
	mockESS
	status        types.SystemStatus
	setModes      bool
	setBatMode    types.BatteryMode
	setSolMode    types.SolarMode
	GetStatusFunc func(ctx context.Context) (types.SystemStatus, error)
	SetModesFunc  func(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error
}

func (m *RecordingMockESS) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc(ctx)
	}
	return m.status, nil
}

func (m *RecordingMockESS) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	if m.SetModesFunc != nil {
		return m.SetModesFunc(ctx, bat, sol)
	}
	m.setModes = true
	m.setBatMode = bat
	m.setSolMode = sol
	return nil
}

type RecordingMockStorage struct {
	mockStorage
	insertedAction   *types.Action
	InsertActionFunc func(ctx context.Context, action types.Action) error
}

func (m *RecordingMockStorage) InsertAction(ctx context.Context, action types.Action) error {
	if m.InsertActionFunc != nil {
		return m.InsertActionFunc(ctx, action)
	}
	m.insertedAction = &action
	return nil
}

func TestUpdateSitePrices(t *testing.T) {
	t.Run("Backfill - No History", func(t *testing.T) {
		mockU := &mockUtility{}
		// Expect GetConfirmedPrices to be called for ~5 days
		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{
			{DollarsPerKWH: 0.1, TSStart: time.Now()},
		}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("UpsertPrices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Verify that it started from 5 days ago
		now := time.Now()
		fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
		expectedStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)

		expectedEnd := now.Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("Incremental Update", func(t *testing.T) {
		mockU := &mockUtility{}
		lastTime := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Hour)

		// Expect GetConfirmedPrices to start from lastTime
		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		expectedEnd := time.Now().Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("No Future Update", func(t *testing.T) {
		mockU := &mockUtility{}
		// If last time is now (or very close), we might still get a call for the current partial hour/day
		lastTime := time.Now().Truncate(time.Hour)

		// Allow calls for past/present/future within the day
		mockU.On("GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return !start.After(time.Now())
		}), mock.Anything).Return([]types.Price{}, nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Ensure strictly no calls with start time in the future
		mockU.AssertNotCalled(t, "GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return start.After(time.Now())
		}), mock.Anything)
	})

	t.Run("Version Mismatch Backfill", func(t *testing.T) {
		mockU := &mockUtility{}
		// Recent time but old version
		lastTime := time.Now().Add(-1 * time.Hour)
		oldVersion := types.CurrentPriceHistoryVersion - 1

		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, oldVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, false)
		require.NoError(t, err)

		// Should have triggered backfill from 5 days ago, not 1 hour ago
		now := time.Now()
		fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
		expectedStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())

		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		expectedEnd := now.Add(12 * time.Hour)
		assert.WithinDuration(t, expectedEnd, endTime, time.Second)
	})

	t.Run("Refresh Now", func(t *testing.T) {
		mockU := &mockUtility{}
		// If last time is in the future, normally we wouldn't fetch.
		// But with refreshNow=true, we should fetch starting from now.
		lastTime := time.Now().Add(5 * time.Hour).Truncate(time.Hour)

		var startTime time.Time
		var endTime time.Time
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.Price{}, nil).Once()

		mockS := &mockStorage{}
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentPriceHistoryVersion, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: mockUMap,
			storage:   mockS,
		}

		err := srv.updatePriceHistory(context.Background(), "site1", mockU, true)
		require.NoError(t, err)

		// Should have started from now, not lastTime
		assert.WithinDuration(t, time.Now(), startTime, time.Second)
		assert.WithinDuration(t, time.Now().Add(12*time.Hour), endTime, time.Second)
	})
}

func TestUpdateEnergyHistory(t *testing.T) {
	t.Run("Backfill - No History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		// Expect call for ~5 days
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		now := time.Now()
		fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
		expectedStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		assert.WithinDuration(t, now, endTime, time.Second)
	})

	t.Run("Incremental Update - Recent History", func(t *testing.T) {
		mockS := &mockStorage{}
		lastTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, lastTime, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		assert.True(t, startTime.Equal(lastTime))
		assert.WithinDuration(t, time.Now(), endTime, time.Second)
	})

	t.Run("Version Mismatch - Partial Backfill", func(t *testing.T) {
		mockS := &mockStorage{}
		// Recent time but old version
		lastTime := time.Now().Add(-1 * time.Hour)
		oldVersion := types.CurrentEnergyStatsVersion - 1
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, oldVersion, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, "site1", mock.Anything, mock.Anything).Return(nil)

		mockES := &mockESS{}
		var startTime time.Time
		var endTime time.Time
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			startTime = args.Get(1).(time.Time)
			endTime = args.Get(2).(time.Time)
		}).Return([]types.DailyEnergyStats{{}}, nil).Once()

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		now := time.Now()
		fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
		expectedStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())
		assert.True(t, expectedStart.Equal(startTime), "Expected start time %v, got %v", expectedStart, startTime)
		assert.WithinDuration(t, now, endTime, time.Second)
	})

	t.Run("Future Time - No Update", func(t *testing.T) {
		mockS := &mockStorage{}
		lastTime := time.Now().Add(1 * time.Hour) // Future
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(lastTime, types.CurrentEnergyStatsVersion, nil)

		mockES := &mockESS{}
		// Should NOT call GetEnergyHistory

		srv := &Server{
			storage: mockS,
		}

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		err := srv.updateEnergyHistory(context.Background(), "site1", mockES)
		require.NoError(t, err)

		mockES.AssertNotCalled(t, "GetEnergyHistory")
	})

	t.Run("Future Price Fallback", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}
		mockU := &mockUtility{}

		now := time.Now().Truncate(time.Hour)
		pastPrice := types.Price{
			TSStart:       now.Add(-12 * time.Hour),
			TSEnd:         now.Add(-11 * time.Hour),
			DollarsPerKWH: 0.25,
		}

		mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{UtilityProvider: "test"}, types.CurrentSettingsVersion, nil)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site1").Return(time.Time{}, 0, nil)
		mockS.On("GetEnergyHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockS.On("InsertAction", mock.Anything, "site1", mock.Anything).Return(nil)
		mockS.On("GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything).Return([]types.Price{pastPrice}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{BatterySOC: 50}, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil)
		mockES.On("SetModes", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetCurrentPrice", mock.Anything).Return(types.Price{DollarsPerKWH: 0.10, TSStart: now}, nil)
		mockU.On("GetFuturePrices", mock.Anything).Return([]types.Price{}, nil) // Trigger fallback
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider("site1", mockU)
		mockP := ess.NewMap()
		mockP.SetSystem("site1", mockES)

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockP,
			storage:    mockS,
			controller: controller.NewController(),
		}

		_, _, err := srv.performSiteUpdate(context.Background(), "site1", settingsWithVersion{Settings: types.Settings{UtilityProvider: "test"}}, types.Credentials{})
		assert.NoError(t, err)

		mockS.AssertCalled(t, "GetPriceHistory", mock.Anything, "site1", mock.Anything, mock.Anything)
	})
}

func TestUpdateWeatherHistory(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	sl := types.SiteLocation{
		PostalCode:  "90210",
		CountryCode: "US",
		Latitude:    34.09,
		Longitude:   -118.4,
		TimeZone:    loc.String(),
	}

	t.Run("Update Missing History", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Cold start: expected fetch range is 5 days ago to end of tomorrow
		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		fiveDaysAgoMidnight := todayMidnight.AddDate(0, 0, -5)
		endOfTomorrow := todayMidnight.AddDate(0, 0, 2)

		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(fiveDaysAgoMidnight)
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(endOfTomorrow)
		})).Return([]types.Weather{{TSDayStart: now}}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Backfill on Version Mismatch", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		// Old version history exists
		lastTime := time.Now().In(loc).AddDate(0, 0, -1)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(lastTime, time.Time{}, 1, nil) // Version 1 < 3
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Should still start from 5 days ago due to version mismatch
		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		fiveDaysAgoMidnight := todayMidnight.AddDate(0, 0, -5)

		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(fiveDaysAgoMidnight)
		}), mock.Anything).Return([]types.Weather{
			// need to return something otherwise UpsertWeather will not be called.
			{TSDayStart: now},
		}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
	})

	t.Run("No Update If Already Recent", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		// Skip only when lastWeatherTime >= fetchEnd (day-after-tomorrow midnight, exclusive end).
		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// fetchEnd = tomorrowMidnight + 1 day = todayMidnight + 2 days
		fetchEnd := todayMidnight.AddDate(0, 0, 2)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(fetchEnd, now, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockW.AssertNotCalled(t, "Forecast")
	})

	t.Run("Resume From Last Weather Time", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		// Recent history exists — should always re-fetch from today midnight
		// regardless of lastWeatherTime, so today's hourly updates are included.
		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		// last time is mid-today, still current version
		lastTime := todayMidnight.Add(6 * time.Hour)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(lastTime, time.Time{}, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		// Sync should always start from today midnight
		mockW.On("Forecast", mock.Anything, sl, mock.MatchedBy(func(t time.Time) bool {
			return t.Equal(todayMidnight)
		}), mock.Anything).Return([]types.Weather{
			// need to return something otherwise UpsertWeather will not be called.
			{TSDayStart: now},
		}, nil)

		mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertExpectations(t)
	})

	t.Run("Should Not Fetch Anything When Up To Date", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		// lastWeatherTime is exactly tomorrow midnight (fully up to date for today)
		// but should still refetch from TODAY to pick up hourly updates?
		// Wait, user said ONLY fetch today.
		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)
		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, now, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertNotCalled(t, "Forecast")
	})

	t.Run("Morning Refresh Logic", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		sixAMToday := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, loc)

		if now.After(sixAMToday) {
			// Scenario A: It is after 6 AM.
			// If last update was before 6 AM (e.g. 5 AM), we expect a Forecast and UpsertWeather call.
			lastUpdate := sixAMToday.Add(-time.Hour)
			mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)
			mockS.On("GetWeather", mock.Anything, "test-site", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

			mockW.On("Forecast", mock.Anything, sl, todayMidnight, todayMidnight.AddDate(0, 0, 2)).Return([]types.Weather{
				{TSDayStart: todayMidnight},
			}, nil)
			mockS.On("UpsertWeather", mock.Anything, "test-site", mock.Anything, types.CurrentWeatherVersion).Return(nil)

			srv := &Server{
				storage: mockS,
				weather: mockW,
			}

			err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
			assert.NoError(t, err)
			mockS.AssertExpectations(t)
			mockW.AssertExpectations(t)
		} else {
			// Scenario B: It is before 6 AM.
			// It should not refresh even if last update was yesterday.
			lastUpdate := todayMidnight.AddDate(0, 0, -1)
			mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, lastUpdate, types.CurrentWeatherVersion, nil)

			srv := &Server{
				storage: mockS,
				weather: mockW,
			}

			err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
			assert.NoError(t, err)
			mockS.AssertExpectations(t)
			mockW.AssertNotCalled(t, "Forecast")
		}
	})

	t.Run("Morning Refresh Logic - Already Up To Date", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		now := time.Now().In(loc)
		todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

		mockS.On("GetLatestWeatherTime", mock.Anything, "test-site").Return(tomorrowMidnight, now, types.CurrentWeatherVersion, nil)

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		err := srv.updateWeatherHistory(context.Background(), "test-site", sl)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockW.AssertNotCalled(t, "Forecast")
	})
}

func TestSetESSModes(t *testing.T) {
	t.Run("Success clears ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}

		// Expect SetModes to succeed
		mockES.On("SetModes", mock.Anything, types.BatteryModeChargeAny, types.SolarModeAny).Return(nil)

		// Expect settings to be saved with ConsecutiveSetFailures reset to 0
		mockS.On("SetSettings", mock.Anything, "test-site", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 0
		}), 1).Return(nil)

		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 3,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeChargeAny, settings)
		assert.NoError(t, err)
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("Unauthorized error increments ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}

		// Expect SetModes to return unauthorized
		mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny).Return(ess.ErrUnauthorized)

		// Expect settings to be saved with ConsecutiveSetFailures incremented
		mockS.On("SetSettings", mock.Anything, "test-site", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveSetFailures == 2
		}), 1).Return(nil)

		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 1,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeLoad, settings)
		assert.ErrorIs(t, err, ess.ErrUnauthorized)
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
	})

	t.Run("Other error does not increment ConsecutiveSetFailures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}

		// Expect SetModes to return some other error
		otherErr := fmt.Errorf("network timeout")
		mockES.On("SetModes", mock.Anything, types.BatteryModeStandby, types.SolarModeAny).Return(otherErr)

		// SetSettings should NOT be called since it is not an unauthorized error
		srv := &Server{
			storage: mockS,
		}

		settings := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveSetFailures: 1,
					LastAttempt:            time.Now(),
				},
			},
			version: 1,
		}

		err := srv.setESSModes(context.Background(), "test-site", mockES, types.BatteryModeStandby, settings)
		assert.ErrorIs(t, err, otherErr)
		mockS.AssertNotCalled(t, "SetSettings")
		mockES.AssertExpectations(t)
	})
}
