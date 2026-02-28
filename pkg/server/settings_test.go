package server

import (
	"bytes"
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

func TestSettings(t *testing.T) {
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
		mockUMap.SetProvider("test", mockU)

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
		assert.Equal(t, 10.0, resp.MinBatterySOC)
		assert.False(t, resp.HasCredentials["franklin"])
	})

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

		// Create a request with franklin credentials and valid settings
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
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{Username: "foo", MD5Password: "bar"},
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
			return c.Franklin != nil && c.Franklin.Username == "foo" && c.Franklin.MD5Password == "bar"
		})).Return(types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "foo", MD5Password: "bar", GatewayID: "gw-123"},
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

	t.Run("Update Settings - Does Not Backfill History on Unchanged Credentials", func(t *testing.T) {
		srv, mockES := newAuthServer("my-audience", []string{"admin@example.com"}, nil)

		// Create a request with franklin credentials
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
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{Username: "foo", MD5Password: "bar"},
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Setup Mock Storage to return existing credentials (so they are not new)
		existingCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "old", MD5Password: "old"},
		}
		encrypted, _ := srv.encryptCredentials(req.Context(), existingCreds)

		existingSettings := types.Settings{
			EncryptedCredentials: encrypted,
		}

		// Unset the default mock and add a specific one
		mockS.ExpectedCalls = nil
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(existingSettings, types.CurrentSettingsVersion, nil)

		// Expect validation to pass
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()

		// Expect Authenticate to be called with the merged credentials
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Franklin != nil && c.Franklin.Username == "foo" && c.Franklin.MD5Password == "bar"
		})).Return(types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "foo", MD5Password: "bar", GatewayID: "gw-123"},
		}, true, nil)

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
}
