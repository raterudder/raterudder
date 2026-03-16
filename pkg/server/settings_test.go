package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	mockS := &mockStorage{}
	// Default setup for most tests
	mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil).Maybe()
	mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          false,
		MinBatterySOC:   10.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, nil)
	// Add expectations for background sync
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
	mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator tokenVerifier) (*Server, *mockESS) {
		mockES := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)
		// Expect some ESS calls if they happen, e.g. ApplySettings
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()

		mockUMap := utility.NewMap()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		return &Server{
			utilities:   mockUMap,
			ess:         mockP,
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
		mockS2.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil).Maybe()
		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(settingsWithCreds, types.CurrentSettingsVersion, nil)
		mockS2.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS2.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS2.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

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
	mockU := &mockUtility{}
	mockS := &mockStorage{}
	// Default setup for most tests
	mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil).Maybe()
	mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          false,
		MinBatterySOC:   10.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, nil)
	// Add expectations for background sync
	mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
	mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// Helper to create server with auth config
	newAuthServer := func(audience string, emails []string, validator tokenVerifier) (*Server, *mockESS) {
		mockES := &mockESS{}
		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, mockES)
		// Expect some ESS calls if they happen, e.g. ApplySettings
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()

		mockUMap := utility.NewMap()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		return &Server{
			utilities:   mockUMap,
			ess:         mockP,
			storage:     mockS,
			controller:  controller.NewController(),
			adminEmails: emails,
			oidcVerifiers: map[string]tokenVerifier{
				"google": validator,
			},
			encryptionKey: "test-secret-key-1234567890123456",
		}, mockES
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
		srv, _ := newAuthServer("", nil, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = withUser(req, "user@example.com", false) // Not admin
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("Update Settings - Missing Auth", func(t *testing.T) {
		srv, _ := newAuthServer("my-audience", []string{"admin@example.com"}, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})

	t.Run("Update Settings - Unauthorized Email", func(t *testing.T) {
		srv, _ := newAuthServer("my-audience", []string{"admin@example.com"}, nil)
		req := httptest.NewRequest("POST", "/api/settings", nil)
		req = withUser(req, "hacker@example.com", false)
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
	})

	t.Run("getESSSystem Backoff Logic", func(t *testing.T) {
		mockS := &mockStorage{}
		mockP := ess.NewMap()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Maybe()
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockP.SetSystem("site1", mockES)

		srv := &Server{
			storage: mockS,
			ess:     mockP,
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
					LastAttempt:         time.Now(),
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
					LastAttempt:         time.Now().Add(-10 * time.Second),
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
					LastAttempt:         time.Now().Add(-31 * time.Second),
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
					LastAttempt:         time.Now().Add(-1 * time.Minute),
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
					LastAttempt:         time.Now().Add(-5 * time.Minute),
				},
			},
		}
		_, err = srv.getESSSystem(ctx, "site1", s5_expired, creds)
		require.NoError(t, err)
	})

	t.Run("Update Settings - Validation Error", func(t *testing.T) {
		srv, _ := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		base := types.Settings{
			IgnoreHourUsageOverMultiple: 1,
			SolarTrendRatioMax:          1,
			MinBatterySOC:               20,
			Release:                     srv.release,
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
		srv, mockES := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		s := types.Settings{
			MinBatterySOC:               80,
			DryRun:                      true,
			IgnoreHourUsageOverMultiple: 5,
			SolarTrendRatioMax:          3.0,
			SolarBellCurveMultiplier:    1.0,
			UtilityProvider:             "test",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Expect SetSettings with version
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.MinBatterySOC == 80.0 && s.DryRun == true
		}), types.CurrentSettingsVersion).Return(nil)

		// Expect validation to pass
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Verify storage updated
		mockS.AssertExpectations(t)
		mockES.AssertExpectations(t)
		mockU.AssertExpectations(t)
	})

	t.Run("Auth Status - Not Logged In", func(t *testing.T) {
		srv, _ := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

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
		srv, mockES := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

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
				UtilityProvider:             "test",
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

		// Expect validation to pass
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()

		// Expect Authenticate to be called with the provided credentials
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock != nil && c.Mock.Strategy == "foo"
		})).Return(types.Credentials{
			Mock: &types.MockCredentials{Strategy: "foo"},
		}, true, nil)

		// Expect GetEnergyHistory (Sync) because we are providing new credentials
		// and the default mock storage returns no EncryptedCredentials
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil)

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, types.CurrentSettingsVersion).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		mockES.AssertExpectations(t)
		mockS.AssertExpectations(t)
		mockU.AssertExpectations(t)
	})

	t.Run("Update Settings - Fails with Missing Credentials", func(t *testing.T) {
		srv, _ := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

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
				UtilityProvider:             "test",
				ESS:                         "franklin",
			},
			Credentials: nil,
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Expect validation to pass for utility
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()

		// Setting up existing settings with NO credentials
		mockS.ExpectedCalls = nil
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS: "mock", // different ESS
		}, types.CurrentSettingsVersion, nil)
		mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		// Expect SetSettings to be called to update auth status after failure
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion).Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "failed to verify ess credentials: credentials missing")

		mockU.AssertExpectations(t)
		mockS.AssertExpectations(t)
	})

	t.Run("Update Settings - Does Not Backfill History on Unchanged Credentials", func(t *testing.T) {
		srv, mockES := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

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
				UtilityProvider:             "test",
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
		encrypted, _ := srv.encryptCredentials(req.Context(), existingCreds)

		existingSettings := types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
		}

		// Unset the default mock and add a specific one
		mockS.ExpectedCalls = nil
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(existingSettings, types.CurrentSettingsVersion, nil)

		// Expect validation to pass
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, types.CurrentSettingsVersion).Return(nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		mockES.AssertExpectations(t)
		mockS.AssertExpectations(t)
		mockU.AssertExpectations(t)
	})

	t.Run("Update Settings - Rate Options Validation Failure", func(t *testing.T) {
		srv, _ := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

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
		w := httptest.NewRecorder()

		// Expect validation to fail
		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(opts types.Settings) bool {
			return opts.UtilityRateOptions.RateClass == "invalid"
		})).Return(assert.AnError).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		mockU.AssertExpectations(t)
	})

	t.Run("Update Site Location With Postal Code", func(t *testing.T) {
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap := utility.NewMap()
		mockUMap.SetProvider("test-site", mockU)
		mockS := &mockStorage{}
		mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil).Maybe()
		mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
		}, types.CurrentSettingsVersion, nil)
		mockS.On("GetCredentials", mock.Anything, mock.Anything).Return(types.Credentials{}, types.CurrentSettingsVersion, nil)
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			settings := args.Get(2).(types.Settings)
			assert.NotNil(t, settings.Location)
			assert.Equal(t, "90210", settings.Location.PostalCode)
		})
		mockS.On("SetCredentials", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		mockW := &mockWeather{}
		mockW.On("GetLocationData", mock.Anything, mock.Anything, mock.Anything).Return(&types.SiteLocation{
			PostalCode:  "90210",
			CountryCode: "US",
		}, nil).Maybe()
		mockW.On("FetchWeatherForecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()

		srv := &Server{
			storage:     mockS,
			utilities:   mockUMap,
			controller:  controller.NewController(),
			weather:     mockW,
			bypassAuth:  true,
			singleSite:  true,
			adminEmails: []string{"test@example.com"},
		}

		bodyData := types.Settings{
			PostalCode:                  "90210",
			CountryCode:                 "US",
			UtilityProvider:             "test",
			IgnoreHourUsageOverMultiple: 1.0,
			SolarTrendRatioMax:          1.0,
		}
		body, _ := json.Marshal(bodyData)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		ctx := context.WithValue(req.Context(), userContextKey, types.User{Email: "test@example.com", ID: "admin", Admin: true})
		ctx = context.WithValue(ctx, siteIDContextKey, "test-site")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("Rate Limit First Retry Allowed (200)", func(t *testing.T) {
		mockU2 := &mockUtility{}
		mockU2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap2 := utility.NewMap()
		mockUMap2.SetProvider(types.SiteIDNone, mockU2)

		mockS2 := &mockStorage{}
		mockES2 := &mockESS{}
		mockES2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES2.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, true, nil).Once()
		mockES2.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil).Maybe()
		mockS2.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS2.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS2.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		mockP2 := ess.NewMap()
		mockP2.SetSystem(types.SiteIDNone, mockES2)

		srv := &Server{
			utilities:     mockUMap2,
			ess:           mockP2,
			storage:       mockS2,
			singleSite:    true,
			adminEmails:   []string{"admin@example.com"},
			release:       "production",
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// For the first retry, even if credentials haven't changed, the failures = 1 logic should return backoff = 0
		// Wait, if existingCreds is empty, credentialsActuallyChanged = true anyway.
		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			ESS:             "franklin",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 1,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, nil).Once()

		s := types.Settings{
			UtilityProvider:             "test",
			ESS:                         "mock",
			Release:                     "production",
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
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Rate Limit Active (429) after 2 Failures", func(t *testing.T) {
		mockU2 := &mockUtility{}
		mockU2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap2 := utility.NewMap()
		mockUMap2.SetProvider(types.SiteIDNone, mockU2)

		mockS2 := &mockStorage{}
		mockES2 := &mockESS{}
		mockES2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockP2 := ess.NewMap()
		mockP2.SetSystem(types.SiteIDNone, mockES2)

		srv := &Server{
			utilities:     mockUMap2,
			ess:           mockP2,
			storage:       mockS2,
			singleSite:    true,
			adminEmails:   []string{"admin@example.com"},
			release:       "production",
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "sameuser"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider:      "test",
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, nil).Once()

		s := types.Settings{
			UtilityProvider:             "test",
			ESS:                         "mock",
			Release:                     "production",
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

		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Result().StatusCode)
		var errResp struct {
			Error string `json:"error"`
		}
		err = json.NewDecoder(w.Result().Body).Decode(&errResp)
		require.NoError(t, err)
		assert.Contains(t, errResp.Error, "try again in 20s")
	})

	t.Run("Rate Limit Active (429) after 2 Failures Even With Different Credentials", func(t *testing.T) {
		mockU2 := &mockUtility{}
		mockU2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap2 := utility.NewMap()
		mockUMap2.SetProvider(types.SiteIDNone, mockU2)

		mockS2 := &mockStorage{}
		mockES2 := &mockESS{}
		mockES2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockP2 := ess.NewMap()
		mockP2.SetSystem(types.SiteIDNone, mockES2)

		srv := &Server{
			utilities:     mockUMap2,
			ess:           mockP2,
			storage:       mockS2,
			singleSite:    true,
			adminEmails:   []string{"admin@example.com"},
			release:       "production",
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "sameuser"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), existingCreds)
		require.NoError(t, err)

		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider:      "test",
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 2,
				LastAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, nil).Once()

		s := types.Settings{
			UtilityProvider:             "test",
			ESS:                         "mock",
			Release:                     "production",
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
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))
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
	})

	t.Run("Rate Limit Expired (200) after 5 Minutes", func(t *testing.T) {
		mockU2 := &mockUtility{}
		mockU2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap2 := utility.NewMap()
		mockUMap2.SetProvider(types.SiteIDNone, mockU2)

		mockS2 := &mockStorage{}
		mockES2 := &mockESS{}
		mockES2.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES2.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, true, nil).Once()
		mockES2.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.EnergyStats{}, nil).Maybe()
		mockS2.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS2.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS2.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		mockP2 := ess.NewMap()
		mockP2.SetSystem(types.SiteIDNone, mockES2)

		srv := &Server{
			utilities:     mockUMap2,
			ess:           mockP2,
			storage:       mockS2,
			singleSite:    true,
			adminEmails:   []string{"admin@example.com"},
			release:       "production",
			encryptionKey: "test-secret-key-1234567890123456",
		}

		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			UtilityProvider: "test",
			ESS:             "mock",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 5,
				LastAttempt:         time.Now().UTC().Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, nil).Once()

		s := types.Settings{
			UtilityProvider:             "test",
			ESS:                         "mock",
			Release:                     "production",
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

		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
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
		{7, 900 * time.Second},  // Max capped at 15m
		{10, 900 * time.Second}, // Beyond max is still capped
		{64, 900 * time.Second}, // Prevent overflow wrap to negative
		{65, 900 * time.Second}, // Prevent overflow wrap to zero/positive
		{100, 900 * time.Second},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d failures", tt.failures), func(t *testing.T) {
			result := getESSBackoff(tt.failures)
			// assert that the expected backoff matches the calculated backoff based on the given failure count
			assert.Equal(t, tt.expected, result)
		})
	}
}
