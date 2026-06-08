package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
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

func TestHandleGetSettings(t *testing.T) {
	mockU := &mockUtility{}
	mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil)
	mockS := &mockStorage{}
	// Default setup for most tests
	mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
	mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          false,
		MinBatterySOC:   10.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, nil)
	// Add expectations for background sync
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
	mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
	mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
	mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator tokenVerifier) (*Server, *mockESS) {
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)
		// Expect some ESS calls if they happen, e.g. ApplySettings
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		return &Server{
			utilities:   mockUMap,
			ess:         essMap,
			storage:     mockS,
			controller:  controller.NewController(),
			adminEmails: emails,
			oidcVerifiers: map[string]tokenVerifier{
				"google": validator,
			},
			encryptionKey: "test-secret-key-1234567890123456",
		}, mockES
	}

	t.Run("Get Settings", func(t *testing.T) {
		srv, _ := newAuthServer("", nil, nil)
		req := httptest.NewRequest("GET", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp SettingsRes
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		// Verify fields populated by default mockS.GetSettings
		assert.Equal(t, 10.0, resp.MinBatterySOC)
		assert.Equal(t, false, resp.DryRun)
		assert.Equal(t, "test", resp.UtilityProvider)
		// Verify hasCredentials flags accurately reflect the empty mock credentials
		assert.False(t, resp.HasCredentials["franklin"])
		assert.False(t, resp.HasCredentials["mock"])
	})

	t.Run("Get Settings with Credentials", func(t *testing.T) {
		mockS2 := &mockStorage{}
		srv, _ := newAuthServer("", nil, nil)
		srv.storage = mockS2

		// Set up mock credentials
		creds := types.Credentials{
			Mock: &types.MockCredentials{
				Strategy: "test-strategy",
				Location: "test-location",
			},
		}

		// Encrypt the mock credentials
		encrypted, err := srv.encryptCredentials(context.Background(), creds)
		require.NoError(t, err)

		// Create settings with the encrypted credentials
		settingsWithCreds := types.Settings{
			DryRun:               false,
			MinBatterySOC:        10.0,
			UtilityProvider:      "test",
			EncryptedCredentials: encrypted,
		}

		// Setup a specific mock for GetSettings to return our settings with credentials
		mockS2.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(settingsWithCreds, types.CurrentSettingsVersion, nil)
		mockS2.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockS2.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS2.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

		req := httptest.NewRequest("GET", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp SettingsRes
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)

		// Verify hasCredentials flags accurately reflect the mock credentials
		assert.False(t, resp.HasCredentials["franklin"], "franklin credentials should not be present")
		assert.True(t, resp.HasCredentials["mock"], "mock credentials should be present")

		// Ensure encrypted credentials are removed from the response
		assert.Empty(t, resp.EncryptedCredentials, "encrypted credentials should not be leaked in the response")
	})

}

func TestHandleUpdateSettings(t *testing.T) {
	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator tokenVerifier) *Server {
		return &Server{
			controller:  controller.NewController(),
			adminEmails: emails,
			oidcVerifiers: map[string]tokenVerifier{
				"google": validator,
			},
			encryptionKey: "test-secret-key-1234567890123456",
		}
	}

	// Helper to add user to context
	withUser := func(req *http.Request, email string, isAdmin bool) *http.Request {
		user := types.User{
			ID:    email,
			Email: email,
			Admin: isAdmin,
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, siteIDContextKey, types.SiteIDNone)
		return req.WithContext(ctx)
	}

	t.Run("Update Settings - Disabled (No Admin)", func(t *testing.T) {
		srv := &Server{}
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = withUser(req, "user@example.com", false) // Not admin
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("Update Settings - Missing Auth", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("getESSSystem Backoff Logic", func(t *testing.T) {
		mockS := &mockStorage{}
		essMap := ess.NewMap()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		essMap.SetSystem("site1", mockES)

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		// 1. Failures = 0, should succeed
		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 0,
				},
			},
		}
		_, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.NoError(t, err)

		// 2. Failures = 1, should succeed immediately
		s1 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 1,
					LastAttempt:         testTime,
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s1, creds)
		require.NoError(t, err)

		// 3. Failures = 2, wait 30s. If last attempt was 10s ago, should fail.
		s2 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         testTime.Add(-10 * time.Second),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s2, creds)
		require.ErrorContains(t, err, "try again in 20s")

		// 4. Failures = 2, wait 30s. If last attempt was 31s ago, should succeed.
		s2_expired := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         testTime.Add(-31 * time.Second),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s2_expired, creds)
		require.NoError(t, err)

		// 5. Failures = 5, backoff should be 240s (4 mins). If last attempt was 1 min ago, should fail.
		s5 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 5,
					LastAttempt:         testTime.Add(-1 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s5, creds)
		require.ErrorContains(t, err, "try again in 3m0s")

		// 6. Failures = 5, backoff should be 240s. If last attempt was 5 mins ago, should succeed.
		s5_expired := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 5,
					LastAttempt:         testTime.Add(-5 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s5_expired, creds)
		require.NoError(t, err)
	})

	t.Run("getESSSystem Authentication Failure", func(t *testing.T) {
		mockS := &mockStorage{}
		essMap := ess.NewMap()
		mockES := &mockESS{}

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		// Test that authentication failure correctly updates ConsecutiveFailures and LastAttempt
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("auth failed")).Once()

		var savedSettings types.Settings
		// Validate that the storage saves the updated auth status
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return s.ESSAuthStatus.ConsecutiveFailures == 1 && s.ESSAuthStatus.LastAttempt.Equal(testTime)
		}), 1).Return(nil).Once()

		essMap.SetSystem("site1", mockES)

		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 0,
				},
			},
			version: 1,
		}

		sys, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.ErrorContains(t, err, "failed to apply settings: auth failed")
		assert.Nil(t, sys)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))

		// Ensure fields were updated
		assert.Equal(t, 1, savedSettings.ESSAuthStatus.ConsecutiveFailures, "ConsecutiveFailures should be incremented")
		assert.Equal(t, testTime, savedSettings.ESSAuthStatus.LastAttempt, "LastAttempt should be updated to now")
	})

	t.Run("getESSSystem Authentication Failure - Max Failures Reached", func(t *testing.T) {
		mockS := &mockStorage{}
		essMap := ess.NewMap()
		mockES := &mockESS{}

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		essMap.SetSystem("site1", mockES)

		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		s0 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 40,
					LastAttempt:         testTime.Add(-1 * time.Hour),
				},
			},
			version: 1,
		}

		sys, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in")
		assert.Nil(t, sys)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("getESSSystem Backoff Logic - Tiered Backoffs", func(t *testing.T) {
		mockS := &mockStorage{}
		essMap := ess.NewMap()

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		srv := &Server{
			storage: mockS,
			ess:     essMap,
			nowFunc: func() time.Time { return testTime },
		}

		ctx := context.Background()
		creds := types.Credentials{}

		// 1. Failures = 10, backoff should be 1 hour. If last attempt was 45 mins ago, should fail.
		s10 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 10,
					LastAttempt:         testTime.Add(-45 * time.Minute),
				},
			},
		}
		_, err := srv.getESSSystem(ctx, "site1", s10, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 15m")

		// 2. Failures = 15, backoff should be 2 hours. If last attempt was 90 mins ago, should fail.
		s15 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 15,
					LastAttempt:         testTime.Add(-90 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s15, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 30m")

		// 3. Failures = 30, backoff should be 12 hours. If last attempt was 10 hours ago, should fail.
		s30 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 30,
					LastAttempt:         testTime.Add(-10 * time.Hour),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s30, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 2h")
	})

	t.Run("Update Settings - Validation Error", func(t *testing.T) {
		srv := &Server{
			release: "test",
		}

		base := types.Settings{
			IgnoreHourUsageOverMultiple: 1,
			SolarTrendRatioMax:          1,
			MinBatterySOC:               20,
		}

		// Invalid value (negative battery SOC)
		s1 := base
		s1.MinBatterySOC = -5
		b1, _ := json.Marshal(s1)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b1))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery SOC must be between 0 and 100")

		// Invalid value (IgnoreHourUsageOverMultiple < 1)
		s2 := base
		s2.IgnoreHourUsageOverMultiple = 0
		b2, _ := json.Marshal(s2)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b2))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "ignore hour usage over multiple must be at least 1")

		// Invalid value (MinArbitrageDifferenceDollarsPerKWH < 0)
		s3 := base
		s3.MinArbitrageDifferenceDollarsPerKWH = -0.01
		b3, _ := json.Marshal(s3)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b3))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum arbitrage difference cannot be negative")

		// Invalid value (SolarBellCurveMultiplier < 0)
		s4 := base
		s4.SolarBellCurveMultiplier = -1
		b4, _ := json.Marshal(s4)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b4))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "solar bell curve multiplier cannot be negative")

		// Invalid value (SolarTrendRatioMax < 1)
		s5 := base
		s5.SolarTrendRatioMax = 0.5
		b5, _ := json.Marshal(s5)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b5))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "solar trend ratio max must be at least 1")

		// Invalid value (Release mismatch)
		s6 := base
		s6.Release = "wrong"
		b6, _ := json.Marshal(s6)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b6))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "settings release mismatch")
	})

	t.Run("Update Settings - Success", func(t *testing.T) {
		mockS := &mockStorage{}
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
			UpdateGroup:   5,
		}, types.CurrentSettingsVersion, nil)

		srv := &Server{
			storage: mockS,
			release: "test",
		}

		s := types.Settings{
			MinBatterySOC:               80,
			DryRun:                      true,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Expect SetSettings with version
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.MinBatterySOC == 80.0 && s.DryRun == true && s.UpdateGroup == 5
		}), types.CurrentSettingsVersion).Return(nil)

		srv.handleUpdateSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			// Verify storage updated
			assert.True(t, mockS.AssertExpectations(t))
		}
	})

	t.Run("Auth Status - Not Logged In", func(t *testing.T) {
		srv := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		req := httptest.NewRequest("GET", "/api/auth/status", nil)
		w := httptest.NewRecorder()

		srv.handleAuthStatus(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp authStatusResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.LoggedIn)
	})

	t.Run("Update Settings - Backfills History on New Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, nil)

		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Create a request with mock credentials and valid settings
		s := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				MinBatterySOC:               80,
				DryRun:                      true,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
				ESS:                         "mock",
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "foo"},
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		// Expect Authenticate to be called with the provided credentials
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock != nil && c.Mock.Strategy == "foo"
		})).Return(types.Credentials{
			Mock: &types.MockCredentials{Strategy: "foo"},
		}, true, nil)

		// Expect GetEnergyHistory (Sync) because we are providing new credentials
		// and the default mock storage returns no EncryptedCredentials
		energyHistories := []types.DailyEnergyStats{
			{
				TSDayStart: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
				Hourly: []types.EnergyStats{
					{
						TSHourStart: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
						HomeKWH:     10,
					},
				},
			},
		}
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return(energyHistories, nil)

		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil)
		mockS.On("UpsertEnergyHistories", mock.Anything, types.SiteIDNone, energyHistories, types.CurrentEnergyStatsVersion).Return(nil)

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock" && len(s.EncryptedCredentials) > 0
		}), types.CurrentSettingsVersion).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockES.AssertExpectations(t))
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Fails with Missing Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, nil)

		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Select "franklin" but don't provide credentials
		s := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				MinBatterySOC:               80,
				DryRun:                      true,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
				ESS:                         "mock",
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, types.Credentials{}).Return(types.Credentials{}, false, ess.ErrCredentialsMissing)

		// Expect SetSettings to be called to update auth status after failure
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "failed to verify ess credentials: credentials missing")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Does Not Backfill History on Unchanged Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Create a request with mock credentials
		s := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				MinBatterySOC:               80,
				DryRun:                      true,
				IgnoreHourUsageOverMultiple: 5,
				SolarTrendRatioMax:          3.0,
				SolarBellCurveMultiplier:    1.0,
				ESS:                         "mock",
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Setup Mock Storage to return existing credentials (so they are not new)
		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "foo"},
		}
		encrypted, err := srv.encryptCredentials(req.Context(), existingCreds)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                  "mock",
			UtilityProvider:      "test",
			EncryptedCredentials: encrypted,
		}

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(existingSettings, types.CurrentSettingsVersion, nil)

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, types.CurrentSettingsVersion).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockES.AssertExpectations(t))
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Rate Options Validation Failure", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:   mockS,
			utilities: uMap,
		}

		// Create a request with valid settings but invalid rate options
		s := types.Settings{
			MinBatterySOC:               80,
			DryRun:                      true,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "test",
			UtilityRateOptions:          types.UtilityRateOptions{RateClass: "invalid"},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		// Expect validation to fail
		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(opts types.Settings) bool {
			return opts.UtilityRateOptions.RateClass == "invalid"
		})).Return(assert.AnError)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Site Location With Postal Code", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, nil)
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		bodyData := types.Settings{
			PostalCode:                  "90210",
			CountryCode:                 "US",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		location := types.SiteLocation{
			PostalCode:  "90210",
			CountryCode: "US",
		}
		mockW.On("Location", mock.Anything, mock.Anything, mock.Anything).Return(location, nil)
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						GHI:         800,
					},
				},
			},
		}
		mockW.On("Forecast", mock.Anything, location, mock.Anything, mock.Anything).Return(weather, nil)

		mockS.On("GetLatestWeatherTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "90210"
		}), types.CurrentSettingsVersion).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("Update Solar Direction - Preservation", func(t *testing.T) {
		mockS := &mockStorage{}

		existingLoc := types.SiteLocation{
			PostalCode:   "90210",
			CountryCode:  "US",
			Latitude:     34.0736,
			Longitude:    -118.4004,
			SolarAzimuth: 180,
			SolarTilt:    25,
		}

		srv := &Server{
			storage: mockS,
		}

		bodyData := types.Settings{
			PostalCode:                  "90210",
			CountryCode:                 "US",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
			SolarAzimuth:                270,
			SolarTilt:                   30,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			PostalCode:      "90210",
			CountryCode:     "US",
			Location:        &existingLoc,
		}, types.CurrentSettingsVersion, nil)

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			// Verify that the new azimuth and tilt are saved, but geo data is preserved
			return s.Location != nil &&
				s.Location.SolarAzimuth == 270 &&
				s.Location.SolarTilt == 30 &&
				s.Location.Latitude == 34.0736 &&
				s.Location.Longitude == -118.4004
		}), types.CurrentSettingsVersion).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Solar Direction - With Zip Change", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		bodyData := types.Settings{
			PostalCode:                  "60601",
			CountryCode:                 "US",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
			SolarAzimuth:                270,
			SolarTilt:                   15,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			PostalCode:      "90210",
			CountryCode:     "US",
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.0736,
				Longitude:   -118.4004,
			},
		}, types.CurrentSettingsVersion, nil)

		newLoc := types.SiteLocation{
			PostalCode:  "60601",
			CountryCode: "US",
			Latitude:    41.8818,
			Longitude:   -87.6231,
		}
		mockW.On("Location", mock.Anything, "US", "60601").Return(newLoc, nil).Once()
		weather := []types.Weather{
			{
				TSDayStart:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TimeLocation: "UTC",
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						GHI:         800,
					},
				},
			},
		}
		newLoc.SolarAzimuth = 270
		newLoc.SolarTilt = 15
		mockW.On("Forecast", mock.Anything, newLoc, mock.Anything, mock.Anything).Return(weather, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			// Verify that the new azimuth and tilt are preserved despite a zip change re-fetching location
			return s.Location != nil &&
				s.Location.SolarAzimuth == 270 &&
				s.Location.SolarTilt == 15 &&
				s.Location.PostalCode == "60601" &&
				s.Location.Latitude == 41.8818
		}), types.CurrentSettingsVersion).Return(nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("ESS Rate Limit First Retry Allowed (200)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "foo",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "bar"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 1,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, nil)

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil).Once()

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}), types.CurrentSettingsVersion).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Rate Limit Active (429) after 2 Failures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "sameuser"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "sameuser",
				},
			},
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider:      "test",
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		err = json.NewDecoder(w.Result().Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Contains(t, errResp.Error, "try again in 20s")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Rate Limit Active (429) after 2 Failures Even With Different Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "sameuser"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "completely_different_user",
				},
			},
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		err = json.NewDecoder(w.Result().Body).Decode(&errResp)
		require.NoError(t, err)
		// Assert rate limit remains active even when changing credentials to bypass it
		assert.Contains(t, errResp.Error, "try again in 20s")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Rate Limit Expired (200) after 5 Minutes", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, true, nil)

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                         "mock",
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 2,
			SolarTrendRatioMax:          3,
			SolarBellCurveMultiplier:    1,
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "foo",
				},
			},
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "bar"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 5,
				LastAttempt:         time.Now().UTC().Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, nil)

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil)

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}), mock.Anything).Return(nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Update Settings - Utility Provider Changed Re-fetches Prices", func(t *testing.T) {
		mockU := &mockUtility{}
		uMap := utility.NewMap(nil)
		uMap.SetProvider(types.SiteIDNone, mockU)
		mockS := &mockStorage{}

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		// Create a request with new utility provider
		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return math.Abs(time.Until(start).Seconds()) < 1
		}), mock.Anything).Return(prices, nil)

		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion).Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Transition to Configured Utility Clears Interest", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "",
		}, types.CurrentSettingsVersion, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion).Return(nil).Once()

		// Expect DeleteInterest to be called since transitioning from "" to "new-utility"
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Transition to Configured Utility Sets UpdateGroup when ESS is already configured", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     uMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
			ESS:                         "tesla",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Mock existing credentials for Tesla
		existingCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "token"},
		}
		encrypted, err := srv.encryptCredentials(req.Context(), existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "",
			UpdateGroup:          0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility" && set.UpdateGroup > 0 && set.UpdateGroup <= 16
		}), types.CurrentSettingsVersion).Return(nil).Once()

		// Expect DeleteInterest to be called since transitioning from "" to "new-utility"
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockU.AssertExpectations(t))
		}
	})

	t.Run("Update Settings - Already Configured Utility Does Not Clear Interest", func(t *testing.T) {
		mockS := &mockStorage{}
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		s := types.Settings{
			MinBatterySOC:               20,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion).Return(nil).Once()

		// DeleteInterest should NOT be called

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})
}

func TestGetSettingsWithMigration(t *testing.T) {
	t.Run("Returns Settings And Decrypts Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		ctx := context.Background()
		siteID := "test-site"

		creds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "test-strategy"},
		}
		encrypted, err := srv.encryptCredentials(ctx, creds)
		require.NoError(t, err)

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, nil)

		sv, returnedCreds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.Equal(t, "test-utility", sv.Settings.UtilityProvider)
		assert.Equal(t, "test-strategy", returnedCreds.Mock.Strategy)
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Migrates Settings Below Current Version And Saves", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := &Server{
			storage: mockS,
		}

		ctx := context.Background()
		siteID := "test-site"

		// Mock returning an old version of settings
		oldVersion := 1 // Assume CurrentSettingsVersion > 1
		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, nil)

		// Mock the SetSettings call that should happen after migration
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.MinDeficitPriceDifferenceDollarsPerKWH == 0.02
		}), types.CurrentSettingsVersion).Return(nil)

		sv, creds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		// We expect SetSettings to have been called with the migrated settings
		assert.True(t, mockS.AssertExpectations(t))

		hasMap := creds.Has()
		assert.Contains(t, hasMap, "franklin")
		assert.Contains(t, hasMap, "mock")
		assert.Contains(t, hasMap, "tesla")
		for key, v := range hasMap {
			assert.False(t, v, "Expected %s to be false", key)
		}
	})

	t.Run("Returns Migrated Settings Even If Save Fails", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := &Server{
			storage: mockS,
		}

		ctx := context.Background()
		siteID := "test-site"

		oldVersion := 1
		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, nil)

		// Mock the SetSettings call to return an error
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.MinDeficitPriceDifferenceDollarsPerKWH == 0.02
		}), types.CurrentSettingsVersion).Return(fmt.Errorf("save failed"))

		sv, creds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.True(t, mockS.AssertExpectations(t))

		hasMap := creds.Has()
		assert.Contains(t, hasMap, "franklin")
		assert.Contains(t, hasMap, "mock")
		assert.Contains(t, hasMap, "tesla")
		for key, v := range hasMap {
			assert.False(t, v, "Expected %s to be false", key)
		}
	})

	t.Run("Fails to Decrypt Invalid Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		ctx := context.Background()
		siteID := "test-site"

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: []byte("invalid-encrypted-data"),
		}, types.CurrentSettingsVersion, nil)

		_, _, err := srv.getSettingsWithMigration(ctx, siteID)

		require.Error(t, err)
		assert.ErrorContains(t, err, "cipher")
		assert.True(t, mockS.AssertExpectations(t))
	})
}

func TestGetESSBackoff(t *testing.T) {
	tests := []struct {
		failures int
		expected time.Duration
	}{
		{0, 0},
		{1, 0},
		{2, 30 * time.Second},
		{3, 60 * time.Second},
		{4, 120 * time.Second},
		{5, 240 * time.Second},
		{6, 480 * time.Second},
		{7, 900 * time.Second}, // Max capped at 15m
		{10, time.Hour},
		{13, 2 * time.Hour},
		{22, 12 * time.Hour},
		{39, 12 * time.Hour},
		{40, 365 * 24 * time.Hour},
		{64, 365 * 24 * time.Hour},
		{65, 365 * 24 * time.Hour},
		{100, 365 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d failures", tt.failures), func(t *testing.T) {
			result := getESSBackoff(tt.failures)
			// assert that the expected backoff matches the calculated backoff based on the given failure count
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleESSStage(t *testing.T) {
	newAuthServer := func() (*Server, *mockESS, *mockStorage) {
		mockES := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		mockS := &mockStorage{}
		return &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}, mockES, mockS
	}

	withUser := func(req *http.Request, email string, isAdmin bool) *http.Request {
		user := types.User{
			ID:    email,
			Email: email,
			Admin: isAdmin,
		}
		ctx := context.WithValue(req.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, siteIDContextKey, types.SiteIDNone)
		return req.WithContext(ctx)
	}

	t.Run("Stage success on ErrNeedsNextStage", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		// GetSettings expectation
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, nil)

		// Authenticate returns ErrNeedsNextStage
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil && c.Enphase.Username == "test@example.com"
		})).Return(types.Credentials{}, false, ess.ErrNeedsNextStage)

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp map[string]bool
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp["success"])
	})

	t.Run("Stage failure on other error", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, nil)

		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("some other error"))

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion).Return(nil).Once()

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "some other error")
	})

	t.Run("Stage failure on unsupported or unknown ESS", func(t *testing.T) {
		srv, _, mockS := newAuthServer()

		// GetSettings expectation
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, nil)

		// Test unsupported ESS (e.g. franklin)
		body := map[string]any{
			"ess": "franklin",
			"credentials": map[string]any{
				"franklin": map[string]any{
					"password": "123",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "Franklin does not support stages")

		// Test unknown ESS
		bodyUnknown := map[string]any{
			"ess": "unknown_ess",
		}
		bUnknown, err := json.Marshal(bodyUnknown)
		require.NoError(t, err)

		reqUnknown := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(bUnknown))
		reqUnknown = withUser(reqUnknown, "admin@example.com", true)
		wUnknown := httptest.NewRecorder()

		srv.handleESSStage(wUnknown, reqUnknown)
		assert.Equal(t, http.StatusBadRequest, wUnknown.Result().StatusCode)
		assert.Contains(t, wUnknown.Body.String(), "unknown ess")
	})

	t.Run("Stage failure increments consecutive failures and rate limits", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		// Initial request: GetSettings returns 0 failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 0,
			},
		}, types.CurrentSettingsVersion, nil).Once()

		// Authenticate returns error
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("auth failed")).Once()

		// SetSettings must be called to update auth status
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion).Return(nil).Once()

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		// Second request: GetSettings returns 2 failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, nil).Once()

		req2 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		w2 := httptest.NewRecorder()

		srv.handleESSStage(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Result().StatusCode)
		assert.Contains(t, w2.Body.String(), "ESS rate limited")
	})

	t.Run("Stage success resets consecutive failures", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		// GetSettings returns 2 failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Minute), // wait has expired
			},
		}, types.CurrentSettingsVersion, nil).Once()

		// Authenticate returns ErrNeedsNextStage (success)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, ess.ErrNeedsNextStage).Once()

		// SetSettings must be called to reset auth status
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion).Return(nil).Once()

		body := map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "test@example.com",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Let's also test a complete success (err = nil) which resets failures to 0
		srv2, mockES2, mockS2 := newAuthServer()
		mockS2.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Minute),
			},
		}, types.CurrentSettingsVersion, nil).Once()

		mockES2.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockS2.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion).Return(nil).Once()

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		srv2.handleESSStage(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)
	})
}
