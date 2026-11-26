package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
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
	mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
	// Default setup for most tests
	mockS.On("GetSite", mock.Anything, mock.Anything).Return(types.Site{}, nil)
	mockS.On("UpdateSite", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
		DryRun:          false,
		MinBatterySOC:   10.0,
		UtilityProvider: "test",
	}, types.CurrentSettingsVersion, time.Time{}, nil)
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
		privB64, _ := generateTestVAPIDKeys(t)
		privKey, pubKey, err := parseVAPIDPrivateKey(privB64)
		require.NoError(t, err)
		srv.vapidKey = privKey
		srv.vapidPublicKey = pubKey
		req := httptest.NewRequest("GET", "/api/settings", nil)
		ctx := context.WithValue(req.Context(), siteIDContextKey, types.SiteIDNone)
		ctx = context.WithValue(ctx, userContextKey, types.User{
			ID: "user-123",
			Subscriptions: []types.PushSubscription{
				{ID: "sub-1", Endpoint: "https://example.com/sub-1"},
			},
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		var resp SettingsRes
		err = json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		// Verify fields populated by default mockS.GetSettings
		assert.Equal(t, 10.0, resp.MinBatterySOC)
		assert.Equal(t, false, resp.DryRun)
		assert.Equal(t, "test", resp.UtilityProvider)
		// Verify hasCredentials flags accurately reflect the empty mock credentials
		assert.False(t, resp.HasCredentials["franklin"])
		assert.False(t, resp.HasCredentials["mock"])
	})

	t.Run("Get Settings with Notifications", func(t *testing.T) {
		mockSWithNotif := &mockStorage{}
		srv, _ := newAuthServer("", nil, nil)
		srv.storage = mockSWithNotif

		mockSWithNotif.On("GetSettings", mock.Anything, "site1").Return(types.Settings{
			DryRun: false,
			Notifications: map[string]types.UserNotificationSettings{
				"user1": {MorningSummaryEnabled: true},
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		req := httptest.NewRequest("GET", "/api/settings", nil)
		req = req.WithContext(context.WithValue(req.Context(), siteIDContextKey, "site1"))
		w := httptest.NewRecorder()

		srv.handleGetSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			var resp SettingsRes
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Len(t, resp.Notifications, 1)
			assert.True(t, resp.Notifications["user1"].MorningSummaryEnabled)
		}
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
		encrypted, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, creds)
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
		mockS2.On("GetSettings", mock.Anything, mock.Anything).Return(settingsWithCreds, types.CurrentSettingsVersion, time.Time{}, nil)
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

	t.Run("Update Settings - Preserves Notifications", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newAuthServer("", nil, nil)
		srv.storage = mockS

		existingNotifs := map[string]types.UserNotificationSettings{
			"user1": {MorningSummaryEnabled: true, MorningSummaryHour: 8},
		}
		mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{
			DryRun:        false,
			Notifications: existingNotifs,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		var savedSettings types.Settings
		expectUpdateSettings(mockS, mock.Anything, "site1", types.Settings{
			DryRun:        false,
			Notifications: existingNotifs,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedSettings = s
			return true
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		newSettings := types.Settings{
			DryRun:                   true,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
		}
		b, _ := json.Marshal(newSettings)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		ctx := context.WithValue(req.Context(), siteIDContextKey, "site1")
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.True(t, savedSettings.DryRun)
		assert.Equal(t, existingNotifs, savedSettings.Notifications)
	})

	t.Run("Update Settings - Logs Before And After Settings", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newAuthServer("", nil, nil)
		srv.storage = mockS

		encrypted, err := srv.encryptCredentials(context.Background(), "site1", types.Credentials{
			Mock: &types.MockCredentials{Strategy: "s1", Location: "l1"},
		})
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, "site1").Return(types.Settings{
			DryRun:               false,
			MinBatterySOC:        15.0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		expectUpdateSettings(mockS, mock.Anything, "site1", types.Settings{
			DryRun:               false,
			MinBatterySOC:        15.0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil)

		newSettings := types.Settings{
			DryRun:                   true,
			MinBatterySOC:            25.0,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
		}
		b, err := json.Marshal(newSettings)
		require.NoError(t, err)

		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		ctx := context.WithValue(req.Context(), siteIDContextKey, "site1")
		ctx = log.With(ctx, logger)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		var logEntry map[string]any
		err = json.Unmarshal(logBuf.Bytes(), &logEntry)
		require.NoError(t, err)
		assert.Equal(t, "settings updated", logEntry["msg"])

		beforeMap, ok := logEntry["before"].(map[string]any)
		if assert.True(t, ok) {
			assert.Equal(t, false, beforeMap["dryRun"])
			assert.Equal(t, 15.0, beforeMap["minBatterySOC"])
			assert.NotContains(t, beforeMap, "encryptedCredentials")
		}

		afterMap, ok := logEntry["after"].(map[string]any)
		if assert.True(t, ok) {
			assert.Equal(t, true, afterMap["dryRun"])
			assert.Equal(t, 25.0, afterMap["minBatterySOC"])
			assert.NotContains(t, afterMap, "encryptedCredentials")
		}
	})

	t.Run("getESSSystem Backoff Logic", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil)
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESSAuthStatus: types.ESSAuthStatus{ConsecutiveFailures: 1},
		}, 0, nil, mock.Anything, time.Time{}, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESSAuthStatus: types.ESSAuthStatus{ConsecutiveFailures: 2},
		}, 0, nil, mock.Anything, time.Time{}, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESSAuthStatus: types.ESSAuthStatus{ConsecutiveFailures: 5},
		}, 0, nil, mock.Anything, time.Time{}, nil).Once()
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
		_, _, err := srv.getESSSystem(ctx, "site1", s0, creds)
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
		_, _, err = srv.getESSSystem(ctx, "site1", s1, creds)
		require.NoError(t, err)

		// 3. Failures = 2, wait 30m. If last attempt was 20m ago, should fail (exceeds 20m poll cadence).
		s2 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         testTime.Add(-20 * time.Minute),
				},
			},
		}
		_, _, err = srv.getESSSystem(ctx, "site1", s2, creds)
		require.ErrorContains(t, err, "try again in 10m0s")

		// 4. Failures = 2, wait 30m. If last attempt was 31m ago, should succeed.
		s2_expired := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 2,
					LastAttempt:         testTime.Add(-31 * time.Minute),
				},
			},
		}
		_, _, err = srv.getESSSystem(ctx, "site1", s2_expired, creds)
		require.NoError(t, err)

		// 5. Failures = 5, backoff should be 45m. If last attempt was 20m ago, should fail.
		s5 := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 5,
					LastAttempt:         testTime.Add(-20 * time.Minute),
				},
			},
		}
		_, _, err = srv.getESSSystem(ctx, "site1", s5, creds)
		require.ErrorContains(t, err, "try again in 25m0s")

		// 6. Failures = 5, backoff should be 45m. If last attempt was 46m ago, should succeed.
		s5_expired := settingsWithVersion{
			Settings: types.Settings{
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 5,
					LastAttempt:         testTime.Add(-46 * time.Minute),
				},
			},
		}
		_, _, err = srv.getESSSystem(ctx, "site1", s5_expired, creds)
		require.NoError(t, err)
	})

	t.Run("getESSSystem Authentication Failure", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{}

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		// Test that authentication failure correctly updates ConsecutiveFailures and LastAttempt
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("auth failed")).Once()

		var savedSettings types.Settings
		// Validate that the storage saves the updated auth status
		expectUpdateSettings(mockS, mock.Anything, "site1", types.Settings{
			ESSAuthStatus: types.ESSAuthStatus{ConsecutiveFailures: 0},
		}, 1, func(s types.Settings) bool {
			savedSettings = s
			return s.ESSAuthStatus.ConsecutiveFailures == 1 && s.ESSAuthStatus.LastAttempt.Equal(testTime)
		}, 1, time.Time{}, nil).Once()

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

		sys, _, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.ErrorContains(t, err, "failed to apply settings: auth failed")
		assert.Nil(t, sys)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))

		// Ensure fields were updated
		assert.Equal(t, 1, savedSettings.ESSAuthStatus.ConsecutiveFailures, "ConsecutiveFailures should be incremented")
		assert.Equal(t, testTime, savedSettings.ESSAuthStatus.LastAttempt, "LastAttempt should be updated to now")
	})

	t.Run("getESSSystem Permanent Auth 1 Retry And Lockout Jump To 40", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{}
		essMap.SetSystem("site1", mockES)
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
			nowFunc:       func() time.Time { return testTime },
		}

		var logBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})))
		permErr := fmt.Errorf("bad credentials: %w", ess.ErrUnauthorized)

		// 1. First permanent error from 0 increments to 1 so next poll retries once
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, permErr).Once()
		expectUpdateSettings(mockS, mock.Anything, "site1", types.Settings{}, 1, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}, 1, testTime, nil).Once()

		_, out1, err := srv.getESSSystem(ctx, "site1", settingsWithVersion{version: 1}, types.Credentials{})
		require.ErrorIs(t, err, ess.ErrUnauthorized)
		assert.Equal(t, 1, out1.ESSAuthStatus.ConsecutiveFailures)

		// 2. Retry on next poll (ConsecutiveFailures == 1 has 0s backoff): if it succeeds, resets to 0
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, "site1", out1.Settings, 1, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}, 1, testTime, nil).Once()

		sys, outRecovered, err := srv.getESSSystem(ctx, "site1", out1, types.Credentials{})
		require.NoError(t, err)
		require.NotNil(t, sys)
		assert.Equal(t, 0, outRecovered.ESSAuthStatus.ConsecutiveFailures)

		// 3. Second permanent error when ConsecutiveFailures >= 1 jumps directly to 40
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, permErr).Once()
		expectUpdateSettings(mockS, mock.Anything, "site1", out1.Settings, 1, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 40
		}, 1, testTime, nil).Once()

		_, outPaused, err := srv.getESSSystem(ctx, "site1", out1, types.Credentials{})
		require.ErrorIs(t, err, ess.ErrUnauthorized)
		assert.Equal(t, 40, outPaused.ESSAuthStatus.ConsecutiveFailures)

		// 4. Explicit lockout error jumps directly from 0 to 40 without a retry
		lockedErr := fmt.Errorf("password locked: %w: %w", ess.ErrAccountLocked, ess.ErrUnauthorized)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, lockedErr).Once()
		expectUpdateSettings(mockS, mock.Anything, "site1", types.Settings{}, 1, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 40
		}, 1, testTime, nil).Once()

		_, outLocked, err := srv.getESSSystem(ctx, "site1", settingsWithVersion{version: 1}, types.Credentials{})
		require.ErrorIs(t, err, ess.ErrAccountLocked)
		assert.Equal(t, 40, outLocked.ESSAuthStatus.ConsecutiveFailures)
		assert.Empty(t, logBuf.String(), "permanent auth failures must emit zero ERROR logs")
	})

	t.Run("getESSSystem Saves Rotated Credentials When Authenticate Fails With Updated True", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		essMap := ess.NewMap()
		mockES := &mockESS{MockName: "tesla"}
		essMap.SetSystem("site1", mockES)

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
			nowFunc:       func() time.Time { return testTime },
		}

		rotated := types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "rotated-access",
				RefreshToken: "rotated-refresh",
				EnergySiteID: 99,
			},
		}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(rotated, true, fmt.Errorf("validation failed")).Once()

		var savedSettings types.Settings
		expectUpdateSettings(mockS, mock.Anything, "site1", types.Settings{ESS: "tesla"}, 1, func(s types.Settings) bool {
			savedSettings = s
			return s.ESSAuthStatus.ConsecutiveFailures == 1 && len(s.EncryptedCredentials) > 0
		}, 1, testTime, nil).Once()

		_, outSV, err := srv.getESSSystem(context.Background(), "site1", settingsWithVersion{
			Settings: types.Settings{ESS: "tesla"},
			version:  1,
		}, types.Credentials{Tesla: &types.TeslaCredentials{AccessToken: "old-access"}})
		require.ErrorContains(t, err, "validation failed")

		decrypted, _, decErr := srv.decryptCredentials(context.Background(), "site1", savedSettings.EncryptedCredentials)
		require.NoError(t, decErr)
		require.NotNil(t, decrypted.Tesla)
		assert.Equal(t, "rotated-access", decrypted.Tesla.AccessToken)
		assert.Equal(t, "rotated-refresh", decrypted.Tesla.RefreshToken)
		assert.Equal(t, testTime, outSV.updatedAt)
	})

	t.Run("getESSSystem Authentication Failure - Max Failures Reached", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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

		sys, _, err := srv.getESSSystem(ctx, "site1", s0, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in")
		assert.Nil(t, sys)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("getESSSystem Backoff Logic - Tiered Backoffs", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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
		_, _, err := srv.getESSSystem(ctx, "site1", s10, creds)
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
		_, _, err = srv.getESSSystem(ctx, "site1", s15, creds)
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
		_, _, err = srv.getESSSystem(ctx, "site1", s30, creds)
		require.ErrorIs(t, err, errESSRateLimited)
		assert.ErrorContains(t, err, "try again in 2h")
	})

	t.Run("Update Settings - Validation Error", func(t *testing.T) {
		srv := &Server{
			release: "test",
		}

		base := types.Settings{
			SolarTrendRatioMax: 1,
			MinBatterySOC:      20,
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

		// Uncovered hours
		sUncovered := base
		sUncovered.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
				MinBatterySOC: 20.0,
			},
		}
		bUncovered, _ := json.Marshal(sUncovered)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bUncovered))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC periods must cover all 24 hours")

		// Overlapping periods
		sOverlap := base
		sOverlap.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}}},
				MinBatterySOC: 20.0,
			},
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
				MinBatterySOC: 40.0,
			},
		}
		bOverlap, _ := json.Marshal(sOverlap)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bOverlap))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC periods cannot overlap")

		// Invalid period SOC value (negative)
		sPeriodNeg := base
		sPeriodNeg.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
				MinBatterySOC: -10.0,
			},
		}
		bPeriodNeg, _ := json.Marshal(sPeriodNeg)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bPeriodNeg))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery SOC period value must be between 0 and 100")

		// Invalid MinBatteryExportDifferenceDollarsPerKWH (negative)
		sExportNeg := base
		sExportNeg.MinBatteryExportDifferenceDollarsPerKWH = -0.05
		bExportNeg, _ := json.Marshal(sExportNeg)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bExportNeg))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery export difference cannot be negative")

		// Valid 24-hour custom period (0 to 24)
		mockStorage := &mockStorage{}
		mockStorage.On("GetSettings", mock.Anything, mock.Anything).Return(base, 1, time.Time{}, nil)
		expectUpdateSettings(mockStorage, mock.Anything, mock.Anything, base, 1, nil, mock.Anything, time.Time{}, nil)
		srvValid := &Server{
			storage: mockStorage,
			release: "test",
		}
		s24h := base
		s24h.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
				MinBatterySOC: 20.0,
			},
		}
		b24h, _ := json.Marshal(s24h)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b24h))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srvValid.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		// Invalid period SOC value (> 100)
		sPeriodHigh := base
		sPeriodHigh.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
				MinBatterySOC: 110.0,
			},
		}
		bPeriodHigh, _ := json.Marshal(sPeriodHigh)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bPeriodHigh))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "minimum battery SOC period value must be between 0 and 100")

		// Period specifying non-zero minutes
		sNonZeroMin := base
		sNonZeroMin.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, MinuteStart: 15, HourEnd: 24, MinuteEnd: 0}}},
				MinBatterySOC: 20.0,
			},
		}
		bNonZeroMin, _ := json.Marshal(sNonZeroMin)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bNonZeroMin))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC period start and end minutes must be 0")

		// Period specifying zero hours
		sNoHours := base
		sNoHours.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{
				TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{}},
				MinBatterySOC: 20.0,
			},
		}
		bNoHours, _ := json.Marshal(sNoHours)
		req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bNoHours))
		req = withUser(req, "admin@example.com", true)
		w = httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "reserve SOC period must specify hours")

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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
			UpdateGroup:   5,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv := &Server{
			storage: mockS,
			release: "test",
		}

		s := types.Settings{
			MinBatterySOC:            80,
			DryRun:                   true,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		// Expect UpdateSettings with version
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
			UpdateGroup:   5,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.MinBatterySOC == 80.0 && s.DryRun == true && s.UpdateGroup == 5
		}, types.CurrentSettingsVersion, time.Time{}, nil)

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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

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
				MinBatterySOC:            80,
				DryRun:                   true,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
				ESS:                      "mock",
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

		// Mock the storage calls during history summary backfill
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(energyHistories, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		// Expect UpdateSettings to be called
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESS == "mock" && len(s.EncryptedCredentials) > 0
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockES.AssertExpectations(t))
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Fails with Missing Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		// Default setup for most tests
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

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
				MinBatterySOC:            80,
				DryRun:                   true,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
				ESS:                      "mock",
			},
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, types.Credentials{}).Return(types.Credentials{}, false, ess.ErrCredentialsMissing)

		// Expect UpdateSettings to be called to update auth status after failure
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			DryRun:        false,
			MinBatterySOC: 10.0,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "failed to verify ess credentials: credentials missing")

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Does Not Backfill History on Unchanged Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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
				MinBatterySOC:            80,
				DryRun:                   true,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
				ESS:                      "mock",
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
		encrypted, err := srv.encryptCredentials(req.Context(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                  "mock",
			UtilityProvider:      "test",
			EncryptedCredentials: encrypted,
		}

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil)

		// Expect UpdateSettings to be called
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, existingSettings, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockES.AssertExpectations(t))
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Syncs Grid Settings from ESS when CustomGridSettings is False", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "mock",
				CustomGridSettings:       false,
				GridExportSolar:          false,
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "simple"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{Mock: &types.MockCredentials{Strategy: "simple"}}, true, nil)
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{ESS: "mock"}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.GridChargeBatteries == true && s.GridExportSolar == true && s.GridExportBatteries == true
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Auto-Enables ManageTOUSchedules When Syncing Export Grid Settings for Supported ESS", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{MockName: "franklin"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "franklin",
				CustomGridSettings:       false,
				ManageTOUSchedules:       false,
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
			},
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{Username: "user@example.com", Password: "pw"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "user@example.com", Password: "pw"},
		}, true, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: false,
		}, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.GridChargeBatteries == true && s.GridExportSolar == true && s.GridExportBatteries == false && s.ManageTOUSchedules == true
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Auto-Enables ManageTOUSchedules for Enphase when feature flag enabled", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "enphase",
				CustomGridSettings:       false,
				ManageTOUSchedules:       false,
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
			},
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{Username: "user@example.com", Password: "pw"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		req = req.WithContext(feats.Bool("enphaseExportSchedules", false).With(req.Context(), true))
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Enphase: &types.EnphaseCredentials{Username: "user@example.com", SessionID: "sess"},
		}, true, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.GridChargeBatteries && s.GridExportSolar && s.GridExportBatteries && s.ManageTOUSchedules
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Does not auto-enable ManageTOUSchedules for Enphase when feature flag disabled", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "enphase",
				CustomGridSettings:       false,
				ManageTOUSchedules:       false,
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
			},
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{Username: "user@example.com", Password: "pw"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Enphase: &types.EnphaseCredentials{Username: "user@example.com", SessionID: "sess"},
		}, true, nil)
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.GridChargeBatteries && s.GridExportSolar && s.GridExportBatteries && !s.ManageTOUSchedules
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Preserves Manual Grid Settings when CustomGridSettings is True", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "mock",
				CustomGridSettings:       true,
				GridChargeBatteries:      false,
				GridExportSolar:          true,
				GridExportBatteries:      false,
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "simple"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{Mock: &types.MockCredentials{Strategy: "simple"}}, true, nil)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{ESS: "mock"}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.CustomGridSettings == true && s.GridChargeBatteries == false && s.GridExportSolar == true && s.GridExportBatteries == false
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Preserves Existing Grid Settings When Migrating from v15 Configured ESS", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockES := &mockESS{}
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{
			GridChargeBatteries: true,
			GridExportSolar:     false,
			GridExportBatteries: false,
		}, nil).Maybe()
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		mockUMap := utility.NewMap(mockS)
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     mockUMap,
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Request sent without CustomGridSettings (e.g. from an existing client saving credentials)
		reqBody := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "mock",
				GridChargeBatteries:      false,
				GridExportSolar:          true,
				GridExportBatteries:      true,
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3.0,
				SolarBellCurveMultiplier: 1.0,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{Strategy: "simple"},
			},
		}
		b, err := json.Marshal(reqBody)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{Mock: &types.MockCredentials{Strategy: "simple"}}, true, nil)

		// Storage returns version 15 settings (CustomGridSettings is false)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                 "mock",
			GridChargeBatteries: false,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, 15, time.Time{}, nil)
		// Migration saves migrated settings with CustomGridSettings: true, then handleUpdateSettings saves updated settings
		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESS:                 "mock",
			GridChargeBatteries: false,
			GridExportSolar:     true,
			GridExportBatteries: true,
		}, 15, func(s types.Settings) bool {
			return s.CustomGridSettings == true && s.GridChargeBatteries == false && s.GridExportSolar == true && s.GridExportBatteries == true
		}, types.CurrentSettingsVersion, time.Time{}, nil).Twice()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Rate Options Validation Failure", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:   mockS,
			utilities: uMap,
		}

		// Create a request with valid settings but invalid rate options
		s := types.Settings{
			MinBatterySOC:            80,
			DryRun:                   true,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "test",
			UtilityRateOptions:       types.UtilityRateOptions{RateClass: "invalid"},
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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		bodyData := types.Settings{
			PostalCode:         "90210",
			CountryCode:        "US",
			SolarTrendRatioMax: 1.0,
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

		// Mock the storage calls during history summary backfill
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "90210"
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("Update Location - Preservation", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		existingLoc := types.SiteLocation{
			PostalCode:  "90210",
			CountryCode: "US",
			Latitude:    34.0736,
			Longitude:   -118.4004,
		}

		srv := &Server{
			storage: mockS,
		}

		bodyData := types.Settings{
			PostalCode:         "90210",
			CountryCode:        "US",
			SolarTrendRatioMax: 1.0,
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
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			UtilityProvider: "test",
			PostalCode:      "90210",
			CountryCode:     "US",
			Location:        &existingLoc,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			// Verify that geo data is preserved when zip/country did not change
			return s.Location != nil &&
				s.Location.Latitude == 34.0736 &&
				s.Location.Longitude == -118.4004
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Location - With Zip Change", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		bodyData := types.Settings{
			PostalCode:         "60601",
			CountryCode:        "US",
			SolarTrendRatioMax: 1.0,
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
		}, types.CurrentSettingsVersion, time.Time{}, nil)

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
		mockW.On("Forecast", mock.Anything, newLoc, mock.Anything, mock.Anything).Return(weather, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)

		// Mock the storage calls during history summary backfill
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			UtilityProvider: "test",
			PostalCode:      "90210",
			CountryCode:     "US",
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.0736,
				Longitude:   -118.4004,
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.Location != nil &&
				s.Location.PostalCode == "60601" &&
				s.Location.Latitude == 41.8818
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("ESS Rate Limit First Retry Allowed (200)", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                      "mock",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
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
		encrypted, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 1,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil).Once()

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 1,
				LastAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Rate Limit Active (429) after 2 Failures", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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
		encrypted, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		s := types.Settings{
			ESS:                      "mock",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
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
				ConsecutiveFailures:     2,
				LastAttempt:             time.Now().UTC().Add(-10 * time.Second),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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
		encrypted, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		s := types.Settings{
			ESS:                      "mock",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
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
				ConsecutiveFailures:     2,
				LastAttempt:             time.Now().UTC().Add(-10 * time.Second),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         time.Now().UTC().Add(-10 * time.Second), // 20s remaining of 30s
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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
			ESS:                      "mock",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
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
		encrypted, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     5,
				LastAttempt:             time.Now().UTC().Add(-5 * time.Minute),
				ConsecutiveUserFailures: 5,
				LastUserAttempt:         time.Now().UTC().Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock"
		})).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil)

		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     5,
				LastAttempt:             time.Now().UTC().Add(-5 * time.Minute),
				ConsecutiveUserFailures: 5,
				LastUserAttempt:         time.Now().UTC().Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}, mock.Anything, time.Time{}, nil)

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	})

	t.Run("Rate Limit Capped at 15 Minutes for High Failure Counts", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockES := &mockESS{}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)

		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			ess:           essMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
			nowFunc:       func() time.Time { return testTime },
		}

		existingCreds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "bar"},
		}
		encrypted, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "mock",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			},
			Credentials: &types.Credentials{
				Mock: &types.MockCredentials{
					Strategy: "foo",
				},
			},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		// 1. With 10 interactive failures and LastUserAttempt 5 minutes ago, interactive backoff is capped at 15m (10m remaining)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-5 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		req1 := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req1 = withUser(req1, "admin@example.com", true)
		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, req1)
		assert.Equal(t, http.StatusTooManyRequests, w1.Result().StatusCode)
		assert.Contains(t, w1.Body.String(), "try again in 10m0s")

		// 2. At 16 minutes after 10 interactive failures (cooldown expired, but within 30m reset window):
		// Retry is allowed, and if it fails, ConsecutiveUserFailures increments to 11 (staying at 15m cap, not resetting to 1)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-1 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-16 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, false, fmt.Errorf("bad password")).Once()

		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-1 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-16 * time.Minute),
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 41 && s.ESSAuthStatus.ConsecutiveUserFailures == 11
		}, mock.Anything, time.Time{}, nil).Once()

		req2 := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		w2 := httptest.NewRecorder()
		srv.handleUpdateSettings(w2, req2)
		assert.Equal(t, http.StatusBadRequest, w2.Result().StatusCode)

		// 3. After > 30 minutes of inactivity (2 * maxInteractiveESSBackoff), user gets a fresh interactive curve:
		// First attempt fails -> sets ConsecutiveUserFailures=1 (0s backoff)
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-1 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-31 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, false, fmt.Errorf("bad password")).Once()

		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-1 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-31 * time.Minute),
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 41 && s.ESSAuthStatus.ConsecutiveUserFailures == 1
		}, mock.Anything, time.Time{}, nil).Once()

		req3 := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req3 = withUser(req3, "admin@example.com", true)
		w3 := httptest.NewRecorder()
		srv.handleUpdateSettings(w3, req3)
		assert.Equal(t, http.StatusBadRequest, w3.Result().StatusCode)

		// 4. Immediate second try after 1 interactive failure (ConsecutiveUserFailures=1 has 0s backoff) succeeds and resets both counters
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     41,
				LastAttempt:             testTime,
				ConsecutiveUserFailures: 1,
				LastUserAttempt:         testTime,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Mock.Strategy == "foo"
		})).Return(types.Credentials{}, true, nil).Once()

		expectUpdateSettings(mockS, mock.Anything, mock.Anything, types.Settings{
			ESS:                  "mock",
			EncryptedCredentials: encrypted,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     41,
				LastAttempt:             testTime,
				ConsecutiveUserFailures: 1,
				LastUserAttempt:         testTime,
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}, mock.Anything, time.Time{}, nil).Once()

		req4 := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req4 = withUser(req4, "admin@example.com", true)
		w4 := httptest.NewRecorder()
		srv.handleUpdateSettings(w4, req4)
		assert.Equal(t, http.StatusOK, w4.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Update Settings - Utility Provider Changed Re-fetches Prices", func(t *testing.T) {
		mockU := &mockUtility{}
		uMap := utility.NewMap(nil)
		uMap.SetProvider(types.SiteIDNone, mockU)
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		// Create a request with new utility provider
		s := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.MatchedBy(func(start time.Time) bool {
			return math.Abs(time.Until(start).Seconds()) < 1
		}), mock.Anything).Return(prices, nil)

		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Transition to Configured Utility Clears Interest", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		s := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			UtilityProvider: "",
		}, types.CurrentSettingsVersion, func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Expect DeleteInterest to be called since transitioning from "" to "new-utility"
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Transition to Configured Utility Sets UpdateGroup when ESS is already configured", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     uMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "new-utility",
			ESS:                      "tesla",
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
		encrypted, err := srv.encryptCredentials(req.Context(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "",
			UpdateGroup:          0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "",
			UpdateGroup:          0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility" && set.UpdateGroup > 0 && set.UpdateGroup <= 16
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Expect DeleteInterest to be called since transitioning from "" to "new-utility"
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockU.AssertExpectations(t))
		}
	})

	t.Run("Update Settings - Transition to ComEd Sets UpdateGroup in 13..16", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     uMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
			release:       "production",
		}

		s := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "comed",
			ESS:                      "tesla",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		existingCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "token"},
		}
		encrypted, err := srv.encryptCredentials(req.Context(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "",
			UpdateGroup:          0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "comed"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "",
			UpdateGroup:          0,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, func(set types.Settings) bool {
			return set.UtilityProvider == "comed" && set.UpdateGroup >= 13 && set.UpdateGroup <= 16
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()

		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Result().StatusCode, w.Body.String())
		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Switching from ComEd to TOU Updates UpdateGroup to 1..12", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities:     uMap,
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
			release:       "production",
		}

		s := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "pge",
			ESS:                      "tesla",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		existingCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "token"},
		}
		encrypted, err := srv.encryptCredentials(req.Context(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "comed",
			UtilityRate:          "comed_besh",
			UpdateGroup:          14, // ComEd group
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "pge"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "comed",
			UtilityRate:          "comed_besh",
			UpdateGroup:          14,
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, func(set types.Settings) bool {
			return set.UtilityProvider == "pge" && set.UpdateGroup >= 1 && set.UpdateGroup <= 12
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		srv.handleUpdateSettings(w, req)
		if assert.Equal(t, http.StatusOK, w.Result().StatusCode) {
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockU.AssertExpectations(t))
		}
	})

	t.Run("Update Settings - Already Configured Utility Does Not Clear Interest", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockU := &mockUtility{}
		uMap := utility.NewMap(mockS)
		uMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			utilities: uMap,
			storage:   mockS,
		}

		s := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3.0,
			SolarBellCurveMultiplier: 1.0,
			UtilityProvider:          "new-utility",
		}
		b, err := json.Marshal(s)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		req = withUser(req, "admin@example.com", true)
		w := httptest.NewRecorder()

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()

		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			UtilityProvider: "old-utility",
		}, types.CurrentSettingsVersion, func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// DeleteInterest should NOT be called

		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - Triggers History Summary Update Once when Weather and Price are Updated Concurrently", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockW := &mockWeather{}
		mockU := &mockUtility{}

		mockUMap := utility.NewMap(mockS)
		mockUMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:   mockS,
			weather:   mockW,
			utilities: mockUMap,
		}

		// Change ZIP code (weather backfill) and utility settings (price update)
		bodyData := types.Settings{
			PostalCode:         "60601",
			CountryCode:        "US",
			UtilityProvider:    "new-utility",
			UtilityRate:        "mock",
			SolarTrendRatioMax: 1.0,
		}
		body, err := json.Marshal(bodyData)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewBuffer(body))
		req = withUser(req, "test@test.com", true)

		// Mock existing settings to have old ZIP code and old utility provider
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			PostalCode:      "90210",
			CountryCode:     "US",
			UtilityProvider: "old-utility",
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.0736,
				Longitude:   -118.4004,
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// 1. Weather setup
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
		mockW.On("Forecast", mock.Anything, newLoc, mock.Anything, mock.Anything).Return(weather, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, mock.Anything).Return(time.Time{}, time.Time{}, 0, nil)
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpsertWeather", mock.Anything, types.SiteIDNone, weather, types.CurrentWeatherVersion).Return(nil)

		// 2. Price setup
		mockU.On("ApplySettings", mock.Anything, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		})).Return(nil).Once()
		prices := []types.Price{{DollarsPerKWH: 0.1, TSStart: time.Now()}}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return(prices, nil)
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
		mockS.On("UpsertPrices", mock.Anything, types.SiteIDNone, prices, types.CurrentPriceHistoryVersion).Return(nil)

		// 3. UpdateSettings expectation
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			PostalCode:      "90210",
			CountryCode:     "US",
			UtilityProvider: "old-utility",
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.0736,
				Longitude:   -118.4004,
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "60601" && s.UtilityProvider == "new-utility"
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// 4. History Summary mocks (should be called exactly once since they are waiting for both to finish)
		mockS.On("GetEnergyHistory", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Once()
		mockS.On("UpdateHistorySummary", mock.Anything, types.SiteIDNone, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
		assert.True(t, mockU.AssertExpectations(t))
	})

	t.Run("Update Settings - NetMetering Scheme Unsets Credits", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		s := types.Settings{
			ESS:                      "mock",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme:  "nem2",
				NetMeteringCredits: true,
			},
		}

		body := struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: s,
		}

		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
		user := types.User{ID: "admin@example.com", Email: "admin@example.com", Admin: true}
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), userContextKey, user), siteIDContextKey, types.SiteIDNone))

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		// Assert that UpdateSettings receives the updated settings with NetMeteringCredits unset (false)
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS: "mock",
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.UtilityRateOptions.NetMeteringScheme == "nem2" && !s.UtilityRateOptions.NetMeteringCredits
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Update Settings - Pause Reverts ManagedTOUMode", func(t *testing.T) {
		setupPauseTest := func() (*Server, *mockESS, *mockStorage) {
			mockES := &mockESS{MockName: "mock"}
			essMap := ess.NewMap()
			essMap.SetSystem(types.SiteIDNone, mockES)
			mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Maybe()

			mockS := &mockStorage{}
			mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
			return &Server{
				ess:           essMap,
				storage:       mockS,
				encryptionKey: "test-secret-key-1234567890123456",
			}, mockES, mockS
		}

		t.Run("Reverts When Pause Changes False To True And ManagedTOUMode Is True", func(t *testing.T) {
			srv, mockES, mockS := setupPauseTest()

			mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
				BatterySOC:     75,
				ManagedTOUMode: true,
			}, nil).Once()
			mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{
				MinimumSOC: 25,
			}).Return(true, nil).Once()

			s := types.Settings{
				ESS:                      "mock",
				Pause:                    true,
				ManageTOUSchedules:       true,
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			b, err := json.Marshal(s)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
			req = withUser(req, "admin@example.com", true)
			w := httptest.NewRecorder()

			srv.handleUpdateSettings(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockES.AssertExpectations(t))
		})

		t.Run("Preserves Native TOU Mode When ManagedTOUMode Is False", func(t *testing.T) {
			srv, mockES, mockS := setupPauseTest()

			mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
				BatterySOC:     75,
				ManagedTOUMode: false,
			}, nil).Once()

			s := types.Settings{
				ESS:                      "mock",
				Pause:                    true,
				ManageTOUSchedules:       true,
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			b, err := json.Marshal(s)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
			req = withUser(req, "admin@example.com", true)
			w := httptest.NewRecorder()

			srv.handleUpdateSettings(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockES.AssertExpectations(t))
			mockES.AssertNotCalled(t, "SetModes")
		})

		t.Run("Does Not Check Status Or SetModes When ManageTOUSchedules Is False", func(t *testing.T) {
			srv, mockES, mockS := setupPauseTest()

			mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: false,
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: false,
			}, types.CurrentSettingsVersion, func(s types.Settings) bool {
				return s.Pause && !s.ManageTOUSchedules
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			s := types.Settings{
				ESS:                      "mock",
				Pause:                    true,
				ManageTOUSchedules:       false,
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			b, err := json.Marshal(s)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
			req = withUser(req, "admin@example.com", true)
			w := httptest.NewRecorder()

			srv.handleUpdateSettings(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.True(t, mockS.AssertExpectations(t))
			mockES.AssertNotCalled(t, "GetStatus")
			mockES.AssertNotCalled(t, "SetModes")
		})

		t.Run("Does Not Check Status Or SetModes When Already Paused", func(t *testing.T) {
			srv, mockES, mockS := setupPauseTest()

			mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				ESS:                "mock",
				Pause:              true,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
				ESS:                "mock",
				Pause:              true,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			s := types.Settings{
				ESS:                      "mock",
				Pause:                    true,
				ManageTOUSchedules:       true,
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			b, err := json.Marshal(s)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
			req = withUser(req, "admin@example.com", true)
			w := httptest.NewRecorder()

			srv.handleUpdateSettings(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.True(t, mockS.AssertExpectations(t))
			mockES.AssertNotCalled(t, "GetStatus")
			mockES.AssertNotCalled(t, "SetModes")
		})

		t.Run("Logs Error And Succeeds When GetStatus Fails", func(t *testing.T) {
			srv, mockES, mockS := setupPauseTest()

			mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{}, fmt.Errorf("status error")).Once()

			s := types.Settings{
				ESS:                      "mock",
				Pause:                    true,
				ManageTOUSchedules:       true,
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			b, err := json.Marshal(s)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
			req = withUser(req, "admin@example.com", true)
			w := httptest.NewRecorder()

			srv.handleUpdateSettings(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockES.AssertExpectations(t))
			mockES.AssertNotCalled(t, "SetModes")
		})

		t.Run("Logs Error And Succeeds When SetModes Fails", func(t *testing.T) {
			srv, mockES, mockS := setupPauseTest()

			mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
				ESS:                "mock",
				Pause:              false,
				ManageTOUSchedules: true,
			}, types.CurrentSettingsVersion, func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

			mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
			mockES.On("GetStatus", mock.Anything).Return(types.SystemStatus{
				BatterySOC:     75,
				ManagedTOUMode: true,
			}, nil).Once()
			mockES.On("SetModes", mock.Anything, types.BatteryModeLoad, types.SolarModeAny, types.ModesOptions{
				MinimumSOC: 25,
			}).Return(false, fmt.Errorf("set modes error")).Once()

			s := types.Settings{
				ESS:                      "mock",
				Pause:                    true,
				ManageTOUSchedules:       true,
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			b, err := json.Marshal(s)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(b))
			req = withUser(req, "admin@example.com", true)
			w := httptest.NewRecorder()

			srv.handleUpdateSettings(w, req)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.True(t, mockS.AssertExpectations(t))
			assert.True(t, mockES.AssertExpectations(t))
		})
	})

	t.Run("Update Settings - Failed Provider Switch Saves Rotated Target Credentials And Preserves Preexisting Provider", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		mockEnphase := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockEnphase)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Existing site uses Franklin with valid stored credentials
		preexisting := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:  "owner@example.com",
				Password:  "franklin-pass",
				Token:     "franklin-session-tok",
				GatewayID: "GW-001",
			},
		}
		encPreexisting, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, preexisting)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                      "franklin",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			EncryptedCredentials:     encPreexisting,
		}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Attempt 1: Switch to Enphase with single-use OTP code; OTP redeems (updated=true) but data.json fails
		mockEnphase.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "enphase"
		})).Return(nil)

		redeemedEnphase := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				SessionID:    "enphase-sess-1",
				ManagerToken: "enphase-mgr-1",
				SystemID:     888,
			},
		}
		mockEnphase.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil && c.Enphase.Code == "112233" && c.Franklin == nil
		})).Return(redeemedEnphase, true, fmt.Errorf("credential validation failed: upstream 503")).Once()

		var savedOnFailure types.Settings
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedOnFailure = s
			return s.ESS == "franklin" && s.ESSAuthStatus.ConsecutiveFailures == 1
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqBody1, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "enphase",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{
					Username: "owner@example.com",
					Code:     "112233",
				},
			},
		})
		require.NoError(t, err)

		req1 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody1)), "admin@example.com", true)
		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, req1)
		assert.Equal(t, http.StatusBadRequest, w1.Result().StatusCode)
		var errResp1 struct {
			Error            string `json:"error"`
			CredentialsSaved bool   `json:"credentialsSaved"`
		}
		require.NoError(t, json.NewDecoder(w1.Body).Decode(&errResp1))
		assert.Contains(t, errResp1.Error, "upstream 503")
		assert.True(t, errResp1.CredentialsSaved)

		// Verify BOTH preexisting Franklin credentials AND redeemed Enphase session tokens were saved!
		savedCreds, _, err := srv.decryptCredentials(context.Background(), types.SiteIDNone, savedOnFailure.EncryptedCredentials)
		require.NoError(t, err)
		require.NotNil(t, savedCreds.Franklin, "preexisting Franklin credentials must be preserved when provider switch fails")
		assert.Equal(t, "franklin-session-tok", savedCreds.Franklin.Token)
		require.NotNil(t, savedCreds.Enphase, "redeemed Enphase session credentials must be saved on failure")
		assert.Equal(t, "enphase-sess-1", savedCreds.Enphase.SessionID)

		// Attempt 2: User clicks Save again without OTP code (cleared by UI); merged Enphase session tokens succeed!
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(savedOnFailure, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockEnphase.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil && c.Enphase.SessionID == "enphase-sess-1" && c.Enphase.Code == "" && c.Franklin == nil
		})).Return(redeemedEnphase, false, nil).Once()
		mockEnphase.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Times(3)
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, 0, nil).Once()
		mockS.On("UpsertEnergyHistories", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentEnergyStatsVersion).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		var savedOnSuccess types.Settings
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, savedOnFailure, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedOnSuccess = s
			return s.ESS == "enphase" && s.ESSAuthStatus.ConsecutiveFailures == 0
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqBody2, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "enphase",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{
					Username: "owner@example.com",
				},
			},
		})
		require.NoError(t, err)

		req2 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody2)), "admin@example.com", true)
		w2 := httptest.NewRecorder()
		srv.handleUpdateSettings(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)

		finalCreds, _, err := srv.decryptCredentials(context.Background(), types.SiteIDNone, savedOnSuccess.EncryptedCredentials)
		require.NoError(t, err)
		assert.Nil(t, finalCreds.Franklin, "old provider credentials cleared once switch succeeds")
		require.NotNil(t, finalCreds.Enphase)
		assert.Equal(t, "enphase-sess-1", finalCreds.Enphase.SessionID)
	})

	t.Run("Update Settings - Franklin Blank GatewayID Clears Existing Gateway And Triggers Re-Discovery", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		mockFranklin := &mockESS{MockName: "franklin"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockFranklin)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:  "owner@example.com",
				Password:  "old-pass",
				Token:     "cached-tok",
				GatewayID: "GW-OLD",
			},
		}
		encExisting, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                      "franklin",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			CustomGridSettings:       true,
			EncryptedCredentials:     encExisting,
		}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockFranklin.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "franklin"
		})).Return(nil).Once()

		discoveredCreds := types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:  "owner@example.com",
				Password:  "new-pass",
				Token:     "fresh-tok",
				GatewayID: "GW-AUTO",
			},
		}
		// Blank GatewayID with the same username must NOT preserve GW-OLD or cached-tok, so Authenticate auto-discovers GW-AUTO.
		mockFranklin.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Franklin != nil &&
				c.Franklin.Username == "owner@example.com" &&
				c.Franklin.Password == "new-pass" &&
				c.Franklin.GatewayID == "" &&
				c.Franklin.Token == ""
		})).Return(discoveredCreds, true, nil).Once()

		var savedSettings types.Settings
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedSettings = s
			return s.ESS == "franklin"
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqBody, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "franklin",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:  "owner@example.com",
					Password:  "new-pass",
					GatewayID: "",
				},
			},
		})
		require.NoError(t, err)

		req := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody)), "admin@example.com", true)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)

		savedCreds, _, err := srv.decryptCredentials(context.Background(), types.SiteIDNone, savedSettings.EncryptedCredentials)
		require.NoError(t, err)
		if assert.NotNil(t, savedCreds.Franklin) {
			assert.Equal(t, "GW-AUTO", savedCreds.Franklin.GatewayID)
			assert.Equal(t, "fresh-tok", savedCreds.Franklin.Token)
		}
		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockFranklin.AssertExpectations(t))
	})

	t.Run("Update Settings - Tesla Fresh AuthCode Resets Saved EnergySiteID While Retry Preserves It", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		mockTesla := &mockESS{MockName: "tesla"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockTesla)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		// Previously linked Tesla account had site 11111 and SN-OLD
		existingCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{
				AccessToken:  "old-access",
				RefreshToken: "old-refresh",
				EnergySiteID: 11111,
				SerialNumber: "SN-OLD",
			},
		}
		encExisting, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                      "tesla",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			CustomGridSettings:       true,
			EncryptedCredentials:     encExisting,
		}
		mockTesla.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "tesla"
		})).Return(nil)

		// Attempt 0: Failed AuthCode exchange (credsUpdated=false) omits credentialsSaved and preserves existing encrypted credentials.
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockTesla.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Tesla != nil &&
				c.Tesla.AuthCode == "expired-oauth-code" &&
				c.Tesla.EnergySiteID == 0 &&
				c.Tesla.SerialNumber == "" &&
				c.Tesla.AccessToken == "" &&
				c.Tesla.RefreshToken == ""
		})).Return(types.Credentials{}, false, fmt.Errorf("authorization code is invalid")).Once()
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESS == "tesla" &&
				bytes.Equal(s.EncryptedCredentials, encExisting) &&
				s.ESSAuthStatus.ConsecutiveFailures == 1
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqBody0, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "tesla",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Tesla: &types.TeslaCredentials{
					AuthCode: "expired-oauth-code",
				},
			},
		})
		require.NoError(t, err)

		req0 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody0)), "admin@example.com", true)
		w0 := httptest.NewRecorder()
		srv.handleUpdateSettings(w0, req0)
		assert.Equal(t, http.StatusBadRequest, w0.Result().StatusCode)
		var errResp0 struct {
			Error            string `json:"error"`
			CredentialsSaved bool   `json:"credentialsSaved"`
		}
		require.NoError(t, json.NewDecoder(w0.Body).Decode(&errResp0))
		assert.Contains(t, errResp0.Error, "authorization code is invalid")
		assert.False(t, errResp0.CredentialsSaved)

		existingSettings.ESSAuthStatus.ConsecutiveFailures = 1
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Attempt 1: Fresh OAuth AuthCode without SerialNumber must clear old EnergySiteID/SerialNumber/tokens so Authenticate discovers site 22222.
		redeemedTesla := types.Credentials{
			Tesla: &types.TeslaCredentials{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
				EnergySiteID: 22222,
			},
		}
		mockTesla.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Tesla != nil &&
				c.Tesla.AuthCode == "fresh-oauth-code" &&
				c.Tesla.EnergySiteID == 0 &&
				c.Tesla.SerialNumber == "" &&
				c.Tesla.AccessToken == "" &&
				c.Tesla.RefreshToken == ""
		})).Return(redeemedTesla, true, fmt.Errorf("upstream status 503")).Once()

		var savedOnFailure types.Settings
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedOnFailure = s
			return s.ESS == "tesla" && s.ESSAuthStatus.ConsecutiveFailures == 2
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqBody1, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "tesla",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Tesla: &types.TeslaCredentials{
					AuthCode: "fresh-oauth-code",
				},
			},
		})
		require.NoError(t, err)

		req1 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody1)), "admin@example.com", true)
		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, req1)
		assert.Equal(t, http.StatusBadRequest, w1.Result().StatusCode)
		var errResp1 struct {
			Error            string `json:"error"`
			CredentialsSaved bool   `json:"credentialsSaved"`
		}
		require.NoError(t, json.NewDecoder(w1.Body).Decode(&errResp1))
		assert.Contains(t, errResp1.Error, "upstream status 503")
		assert.True(t, errResp1.CredentialsSaved)

		// Attempt 2: Retry without AuthCode preserves newly redeemed tokens and discovered EnergySiteID 22222.
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(savedOnFailure, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockTesla.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Tesla != nil &&
				c.Tesla.AuthCode == "" &&
				c.Tesla.AccessToken == "new-access" &&
				c.Tesla.RefreshToken == "new-refresh" &&
				c.Tesla.EnergySiteID == 22222
		})).Return(redeemedTesla, false, nil).Once()

		var savedOnSuccess types.Settings
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, savedOnFailure, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedOnSuccess = s
			return s.ESS == "tesla" && s.ESSAuthStatus.ConsecutiveFailures == 0
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqBody2, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "tesla",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Tesla: &types.TeslaCredentials{},
			},
		})
		require.NoError(t, err)

		req2 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody2)), "admin@example.com", true)
		w2 := httptest.NewRecorder()
		srv.handleUpdateSettings(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)

		finalCreds, _, err := srv.decryptCredentials(context.Background(), types.SiteIDNone, savedOnSuccess.EncryptedCredentials)
		require.NoError(t, err)
		if assert.NotNil(t, finalCreds.Tesla) {
			assert.Equal(t, int64(22222), finalCreds.Tesla.EnergySiteID)
			assert.Equal(t, "new-access", finalCreds.Tesla.AccessToken)
		}
		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockTesla.AssertExpectations(t))
	})

	t.Run("Update Settings - ErrNeedsNextStage Does Not Penalize Auth Failure Counters", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		mockEnphase := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockEnphase)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingSettings := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
		}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockEnphase.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "enphase"
		})).Return(nil).Once()
		mockEnphase.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil && c.Enphase.Username == "owner@example.com" && c.Enphase.Code == ""
		})).Return(types.Credentials{}, false, ess.ErrNeedsNextStage).Once()

		reqBody, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "enphase",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			},
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{
					Username: "owner@example.com",
				},
			},
		})
		require.NoError(t, err)

		req := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody)), "admin@example.com", true)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		assert.Contains(t, w.Body.String(), "A verification code was sent to your email")
		mockS.AssertNotCalled(t, "UpdateSettings", mock.Anything, mock.Anything, mock.Anything)
		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockEnphase.AssertExpectations(t))
	})

	t.Run("Update Settings - CredentialsSaved Is False When Persistence Fails Or Creds Not Updated", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		mockTesla := &mockESS{MockName: "tesla"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockTesla)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingSettings := types.Settings{
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
		}
		reqBody, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "tesla",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			},
			Credentials: &types.Credentials{
				Tesla: &types.TeslaCredentials{
					AuthCode: "oauth-code",
				},
			},
		})
		require.NoError(t, err)

		// Case 1: credsUpdated == false (token exchange failed) -> credentialsSaved must be false
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()
		mockTesla.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("failed to exchange auth code")).Once()
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody)), "admin@example.com", true))
		assert.Equal(t, http.StatusBadRequest, w1.Result().StatusCode)
		var resp1 struct {
			Error            string `json:"error"`
			CredentialsSaved bool   `json:"credentialsSaved"`
		}
		require.NoError(t, json.NewDecoder(w1.Body).Decode(&resp1))
		assert.False(t, resp1.CredentialsSaved)

		// Case 2: credsUpdated == true but UpdateSettings fails -> credentialsSaved must be false
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()
		mockTesla.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "tok", RefreshToken: "ref"},
		}, true, fmt.Errorf("upstream 503")).Once()
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, fmt.Errorf("db write failed")).Once()

		w2 := httptest.NewRecorder()
		srv.handleUpdateSettings(w2, withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody)), "admin@example.com", true))
		assert.Equal(t, http.StatusBadRequest, w2.Result().StatusCode)
		var resp2 struct {
			Error            string `json:"error"`
			CredentialsSaved bool   `json:"credentialsSaved"`
		}
		require.NoError(t, json.NewDecoder(w2.Body).Decode(&resp2))
		assert.False(t, resp2.CredentialsSaved)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockTesla.AssertExpectations(t))
	})

	t.Run("Update Settings - Enphase Fresh Code Or Changed Password Clears Stale SystemID And UserID", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()

		mockEnphase := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockEnphase)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		existingCreds := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				Password:     "old-pass",
				SessionID:    "old-sess",
				ManagerToken: "old-mgr",
				SystemID:     111,
				UserID:       999,
			},
		}
		encExisting, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, existingCreds)
		require.NoError(t, err)

		existingSettings := types.Settings{
			ESS:                      "enphase",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			CustomGridSettings:       true,
			EncryptedCredentials:     encExisting,
		}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Twice()
		mockEnphase.On("ApplySettings", mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "enphase"
		})).Return(nil)

		// 1. Fresh OTP Code for same username clears SessionID, ManagerToken, SystemID, and UserID
		mockEnphase.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil &&
				c.Enphase.Username == "owner@example.com" &&
				c.Enphase.Code == "778899" &&
				c.Enphase.SessionID == "" &&
				c.Enphase.ManagerToken == "" &&
				c.Enphase.SystemID == 0 &&
				c.Enphase.UserID == 0
		})).Return(types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				SessionID:    "new-sess",
				ManagerToken: "new-mgr",
				SystemID:     222,
				UserID:       555,
			},
		}, true, nil).Once()

		// 2. Changed Password for same username also clears SessionID, ManagerToken, SystemID, and UserID
		mockEnphase.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil &&
				c.Enphase.Username == "owner@example.com" &&
				c.Enphase.Password == "new-pass" &&
				c.Enphase.SessionID == "" &&
				c.Enphase.ManagerToken == "" &&
				c.Enphase.SystemID == 0 &&
				c.Enphase.UserID == 0
		})).Return(types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				Password:     "new-pass",
				SessionID:    "pwd-sess",
				ManagerToken: "pwd-mgr",
				SystemID:     333,
				UserID:       666,
			},
		}, true, nil).Once()

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingSettings, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESS == "enphase" && s.ESSAuthStatus.ConsecutiveFailures == 0
		}, types.CurrentSettingsVersion, time.Time{}, nil).Twice()

		reqOTP, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: existingSettings,
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{
					Username: "owner@example.com",
					Code:     "778899",
				},
			},
		})
		require.NoError(t, err)
		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqOTP)), "admin@example.com", true))
		assert.Equal(t, http.StatusOK, w1.Result().StatusCode)

		reqPwd, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: existingSettings,
			Credentials: &types.Credentials{
				Enphase: &types.EnphaseCredentials{
					Username: "owner@example.com",
					Password: "new-pass",
				},
			},
		})
		require.NoError(t, err)
		w2 := httptest.NewRecorder()
		srv.handleUpdateSettings(w2, withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqPwd)), "admin@example.com", true))
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockEnphase.AssertExpectations(t))
	})

	t.Run("Concurrent UpdateSiteNotificationSettings And getESSSystem Persist Both Without Error Logs", func(t *testing.T) {
		mockS := &mockStorage{}
		mockTesla := &mockESS{MockName: "tesla"}
		essMap := ess.NewMap()
		essMap.SetSystem("site-conc-1", mockTesla)

		fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
			nowFunc:       func() time.Time { return fixedNow },
		}

		origCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "expired-access",
				RefreshToken: "old-refresh",
				Expiry:       fixedNow.Add(-time.Minute),
				EnergySiteID: 42,
			},
		}
		encOrig, err := srv.encryptCredentials(context.Background(), "site-conc-1", origCreds)
		require.NoError(t, err)

		initialSettings := types.Settings{
			ESS:                  "tesla",
			UtilityProvider:      "comed",
			MinBatterySOC:        20,
			EncryptedCredentials: encOrig,
		}
		store := attachAtomicSettingsStore(mockS, "site-conc-1", initialSettings, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

		authStarted := make(chan struct{})
		notifCommitted := make(chan struct{})
		rotatedCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "rotated-access",
				RefreshToken: "rotated-refresh",
				Expiry:       fixedNow.Add(8 * time.Hour),
				EnergySiteID: 42,
			},
		}
		mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockTesla.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			close(authStarted)
			<-notifCommitted
		}).Return(rotatedCreds, true, nil).Once()

		var logBuf bytes.Buffer
		errCtx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})))

		svBefore, credsBefore, err := srv.getSettingsWithMigration(errCtx, "site-conc-1")
		require.NoError(t, err)

		var wg sync.WaitGroup
		var essErr, notifErr1, notifErr2 error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, essErr = srv.getESSSystem(errCtx, "site-conc-1", svBefore, credsBefore)
		}()
		go func() {
			defer wg.Done()
			<-authStarted
			notifErr1 = srv.storage.UpdateSiteNotificationSettings(errCtx, "site-conc-1", "member1@example.com", types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
			})
			notifErr2 = srv.storage.UpdateSiteNotificationSettings(errCtx, "site-conc-1", "member2@example.com", types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    9,
			})
			close(notifCommitted)
		}()
		wg.Wait()

		require.NoError(t, essErr)
		require.NoError(t, notifErr1)
		require.NoError(t, notifErr2)
		assert.Empty(t, logBuf.String(), "concurrent notification and token updates must emit zero ERROR logs")

		finalSettings, _, _, _ := store.snapshot()
		require.Len(t, finalSettings.Notifications, 2)
		assert.True(t, finalSettings.Notifications["member1@example.com"].MorningSummaryEnabled)
		assert.Equal(t, 9, finalSettings.Notifications["member2@example.com"].MorningSummaryHour)

		decFinal, _, err := srv.decryptCredentials(context.Background(), "site-conc-1", finalSettings.EncryptedCredentials)
		require.NoError(t, err)
		require.NotNil(t, decFinal.Tesla)
		assert.Equal(t, "rotated-access", decFinal.Tesla.AccessToken)
		assert.Equal(t, "rotated-refresh", decFinal.Tesla.RefreshToken)
		assert.True(t, rotatedCreds.Tesla.Expiry.Equal(decFinal.Tesla.Expiry))
	})

	t.Run("getESSSystem Aborts Write-Back When Concurrent Admin Request Switched ESS Provider", func(t *testing.T) {
		mockS := &mockStorage{}
		mockTesla := &mockESS{MockName: "tesla"}
		mockFranklin := &mockESS{MockName: "franklin"}
		essMap := ess.NewMap()
		essMap.SetSystem("site-switch", mockTesla)

		fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
			nowFunc:       func() time.Time { return fixedNow },
		}

		encTesla, err := srv.encryptCredentials(context.Background(), "site-switch", types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "old-tesla-access",
				RefreshToken: "old-tesla-refresh",
				Expiry:       fixedNow.Add(-time.Minute),
				EnergySiteID: 77,
			},
		})
		require.NoError(t, err)

		store := attachAtomicSettingsStore(mockS, "site-switch", types.Settings{
			ESS:                      "tesla",
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			CustomGridSettings:       true,
			EncryptedCredentials:     encTesla,
		}, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

		svTesla, credsTesla, err := srv.getSettingsWithMigration(context.Background(), "site-switch")
		require.NoError(t, err)

		authStarted := make(chan struct{})
		switchDone := make(chan struct{})
		mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockTesla.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			close(authStarted)
			<-switchDone
		}).Return(types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "rotated-tesla-access",
				RefreshToken: "rotated-tesla-refresh",
				Expiry:       fixedNow.Add(8 * time.Hour),
				EnergySiteID: 77,
			},
		}, true, nil).Once()

		mockFranklin.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockFranklin.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Franklin: &types.FranklinCredentials{
				Username:  "owner@example.com",
				Password:  "pw",
				Token:     "franklin-live-token",
				GatewayID: "GW-999",
			},
		}, true, nil).Once()
		mockFranklin.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-switch").Return(fixedNow, types.CurrentEnergyStatsVersion, nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, "site-switch", mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, "site-switch", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, "site-switch", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, "site-switch", mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, gErr := srv.getESSSystem(context.Background(), "site-switch", svTesla, credsTesla)
			assert.NoError(t, gErr)
		}()

		<-authStarted
		essMap.SetSystem("site-switch", mockFranklin)
		adminBody, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "franklin",
				MinBatterySOC:            25,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username: "owner@example.com",
					Password: "pw",
				},
			},
		})
		require.NoError(t, err)
		adminReq := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(adminBody)), "admin@example.com", true)
		adminReq = adminReq.WithContext(context.WithValue(adminReq.Context(), siteIDContextKey, "site-switch"))
		adminW := httptest.NewRecorder()
		srv.handleUpdateSettings(adminW, adminReq)
		require.Equal(t, http.StatusOK, adminW.Result().StatusCode)
		close(switchDone)

		wg.Wait()

		finalSettings, _, _, _ := store.snapshot()
		assert.Equal(t, "franklin", finalSettings.ESS)
		assert.Equal(t, 25.0, finalSettings.MinBatterySOC)
		decFinal, _, err := srv.decryptCredentials(context.Background(), "site-switch", finalSettings.EncryptedCredentials)
		require.NoError(t, err)
		assert.Nil(t, decFinal.Tesla, "aborted Tesla write-back must not overwrite Franklin credentials")
		require.NotNil(t, decFinal.Franklin)
		assert.Equal(t, "franklin-live-token", decFinal.Franklin.Token)
	})

	t.Run("Update Settings Preserves Concurrent Notifications And Rotated Credentials During External API Calls", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}
		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		uMap := utility.NewMap(mockS)
		uMap.SetProvider("site-admin-merge", mockU)

		mockTesla := &mockESS{MockName: "tesla"}
		essMap := ess.NewMap()
		essMap.SetSystem("site-admin-merge", mockTesla)

		fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv := &Server{
			storage:       mockS,
			weather:       mockW,
			utilities:     uMap,
			ess:           essMap,
			encryptionKey: "test-secret-key-1234567890123456",
			nowFunc:       func() time.Time { return fixedNow },
		}

		origCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "initial-access",
				RefreshToken: "initial-refresh",
				Expiry:       fixedNow.Add(-time.Minute),
				EnergySiteID: 1234,
			},
		}
		encOrig, err := srv.encryptCredentials(context.Background(), "site-admin-merge", origCreds)
		require.NoError(t, err)

		initialSettings := types.Settings{
			ESS:                      "tesla",
			UtilityProvider:          "mock",
			MinBatterySOC:            20,
			PostalCode:               "90210",
			CountryCode:              "US",
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
			CustomGridSettings:       true,
			EncryptedCredentials:     encOrig,
			Location: &types.SiteLocation{
				PostalCode:  "90210",
				CountryCode: "US",
				Latitude:    34.07,
				Longitude:   -118.40,
			},
		}
		store := attachAtomicSettingsStore(mockS, "site-admin-merge", initialSettings, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

		var logBuf bytes.Buffer
		errCtx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})))

		svBg, credsBg, err := srv.getSettingsWithMigration(errCtx, "site-admin-merge")
		require.NoError(t, err)

		rotatedCreds := types.Credentials{
			Tesla: &types.TeslaCredentials{
				Region:       "NA",
				AccessToken:  "bg-rotated-access",
				RefreshToken: "bg-rotated-refresh",
				Expiry:       fixedNow.Add(8 * time.Hour),
				EnergySiteID: 1234,
			},
		}
		mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockTesla.On("Authenticate", mock.Anything, mock.Anything).Return(rotatedCreds, true, nil).Once()

		newLoc := types.SiteLocation{
			PostalCode:  "60601",
			CountryCode: "US",
			Latitude:    41.88,
			Longitude:   -87.62,
		}
		mockW.On("Location", mock.Anything, "US", "60601").Run(func(args mock.Arguments) {
			// While weather.Location is in flight, background poll rotates Tesla credentials and a user updates notifications!
			_, _, bgErr := srv.getESSSystem(errCtx, "site-admin-merge", svBg, credsBg)
			require.NoError(t, bgErr)
			require.NoError(t, srv.storage.UpdateSiteNotificationSettings(errCtx, "site-admin-merge", "member@example.com", types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    8,
			}))
		}).Return(newLoc, nil).Once()
		mockW.On("Forecast", mock.Anything, newLoc, mock.Anything, mock.Anything).Return([]types.Weather{}, nil)
		mockS.On("GetLatestWeatherTime", mock.Anything, "site-admin-merge").Return(fixedNow.Add(24*time.Hour), fixedNow, types.CurrentWeatherVersion, nil)
		mockS.On("GetWeather", mock.Anything, "site-admin-merge", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, "site-admin-merge", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, "site-admin-merge", mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		// Omit credentials when saving non-credential settings (matching Settings.tsx when ESS is not dirty)
		reqPayload, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials,omitempty"`
		}{
			Settings: types.Settings{
				ESS:                      "tesla",
				UtilityProvider:          "mock",
				MinBatterySOC:            35,
				PostalCode:               "60601",
				CountryCode:              "US",
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
		})
		require.NoError(t, err)

		req := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqPayload)), "admin@example.com", true)
		req = req.WithContext(log.With(context.WithValue(req.Context(), siteIDContextKey, "site-admin-merge"), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))))
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode, w.Body.String())
		assert.Empty(t, logBuf.String(), "settings update with concurrent mutations must emit zero ERROR logs")

		finalSettings, _, _, _ := store.snapshot()
		assert.Equal(t, 35.0, finalSettings.MinBatterySOC)
		require.NotNil(t, finalSettings.Location)
		assert.Equal(t, "60601", finalSettings.Location.PostalCode)
		assert.True(t, finalSettings.Notifications["member@example.com"].MorningSummaryEnabled, "concurrent notification update must be preserved")

		decFinal, _, err := srv.decryptCredentials(context.Background(), "site-admin-merge", finalSettings.EncryptedCredentials)
		require.NoError(t, err)
		require.NotNil(t, decFinal.Tesla)
		assert.Equal(t, "bg-rotated-access", decFinal.Tesla.AccessToken, "background rotated access token must be preserved")
		assert.Equal(t, "bg-rotated-refresh", decFinal.Tesla.RefreshToken, "background rotated refresh token must be preserved")
	})

	t.Run("EmitsStructuredEventsForESSUtilityAndPauseTransitions", func(t *testing.T) {
		mockS := &mockStorage{}
		mockES := &mockESS{MockName: "franklin"}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "u", Password: "p"},
		}, true, nil).Once()
		mockES.On("GridSettings", mock.Anything).Return(types.GridSettings{}, nil).Once()
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()

		mockU := &mockUtility{}
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()

		// 1. First time ESS + Utility configured
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, mock.Anything).Return(time.Time{}, 0, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetPriceHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, nil).Maybe()
		mockS.On("UpsertPrices", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("UpsertEnergyHistories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()
		mockS.On("UpsertHistorySummaries", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)
		utilMap := utility.NewMap(mockS)
		utilMap.SetProvider(types.SiteIDNone, mockU)

		srv := &Server{
			storage:       mockS,
			ess:           essMap,
			utilities:     utilMap,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		body1, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                "franklin",
				UtilityProvider:    "comed",
				UtilityRate:        "comed_bes",
				SolarTrendRatioMax: 1.5,
			},
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{Username: "u", Password: "p"},
			},
		})
		require.NoError(t, err)

		ctx1, logBuf1 := captureJSONLogs(context.Background())
		req1 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(body1)).WithContext(ctx1), "admin@example.com", true)
		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Result().StatusCode, w1.Body.String())

		if entry, ok := findLoggedEvent(t, logBuf1.String(), eventESSConnected); assert.True(t, ok) {
			assert.Equal(t, "franklin", entry["ess"])
		}
		if entry, ok := findLoggedEvent(t, logBuf1.String(), eventUtilityConfigured); assert.True(t, ok) {
			assert.Equal(t, "comed", entry["utilityProvider"])
			assert.Equal(t, "comed_bes", entry["utilityRate"])
		}

		// 2. Subsequent settings save with Pause=true should emit site_automation_paused and NOT re-emit ess_connected or utility_configured
		existingConnected := types.Settings{
			ESS:                "franklin",
			UtilityProvider:    "comed",
			UtilityRate:        "comed_bes",
			SolarTrendRatioMax: 1.5,
		}
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingConnected, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, existingConnected, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		pausedSettings := existingConnected
		pausedSettings.Pause = true
		body2, err := json.Marshal(pausedSettings)
		require.NoError(t, err)

		ctx2, logBuf2 := captureJSONLogs(context.Background())
		req2 := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(body2)).WithContext(ctx2), "admin@example.com", true)
		w2 := httptest.NewRecorder()
		srv.handleUpdateSettings(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)

		_, hasESS := findLoggedEvent(t, logBuf2.String(), eventESSConnected)
		assert.False(t, hasESS, "ess_connected should only emit on first ESS connection")
		_, hasUtil := findLoggedEvent(t, logBuf2.String(), eventUtilityConfigured)
		assert.False(t, hasUtil, "utility_configured should only emit on first utility configuration")
		_, hasPaused := findLoggedEvent(t, logBuf2.String(), eventSiteAutomationPaused)
		assert.True(t, hasPaused, "expected site_automation_paused event")
	})

	t.Run("Windowed Backfill Saves Completed 5Day Windows Within Settings Sync Timeout", func(t *testing.T) {
		now := time.Now().UTC()
		const testSyncTimeout = 2600 * time.Millisecond

		origTransport := http.DefaultTransport
		t.Cleanup(func() {
			http.DefaultTransport = origTransport
		})

		teslaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/1/energy_sites/1234/site_info":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"default_real_mode":      "self_consumption",
						"backup_reserve_percent": 20.0,
						"nameplate_energy":       13500.0,
						"installation_time_zone": "UTC",
					},
				})
			case "/api/1/energy_sites/1234/live_status":
				json.NewEncoder(w).Encode(map[string]any{
					"response": map[string]any{
						"percentage_charged": 80.0,
						"grid_status":        "Active",
					},
				})
			case "/api/1/energy_sites/1234/calendar_history":
				// 120ms per Tesla call + rate.Limit(4) burst 4: Window 1 (11 calls) finishes in ~1.58s (< 2.6s), while 3 windows (31 calls > 3.7s) times out.
				select {
				case <-time.After(120 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				startParam := r.URL.Query().Get("start_date")
				parsedStart, err := time.Parse(time.RFC3339, startParam)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.URL.Query().Get("kind") == "energy" {
					ts := parsedStart.Add(time.Hour)
					if ts.After(now) {
						ts = parsedStart
					}
					json.NewEncoder(w).Encode(map[string]any{
						"response": map[string]any{
							"time_series": []map[string]any{
								{
									"timestamp":                           ts.Format(time.RFC3339),
									"solar_energy_exported":               1000.0,
									"consumer_energy_imported_from_solar": 1000.0,
								},
							},
						},
					})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"time_series": []any{}}})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		})

		franklinHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/hes-gateway/terminal/getDeviceCompositeInfo":
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"valid": true,
						"runtimeData": map[string]any{
							"soc":  80.0,
							"mode": 20.0,
						},
					},
				})
			case "/hes-gateway/terminal/getDeviceInfoV2":
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "result": map[string]any{"zoneInfo": "UTC"}})
			case "/api-energy/power/getFhpPowerByDay":
				// 220ms per Franklin call + rate.Limit(4) burst 4: Window 1 (6 calls) finishes in ~1.32s (< 2.6s), while 15 days (17 calls > 3.7s) times out.
				select {
				case <-time.After(220 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				dayTime := r.URL.Query().Get("dayTime")
				dayParsed, err := time.Parse("2006-01-02", dayTime)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				nextDay := dayParsed.AddDate(0, 0, 1).Format("2006-01-02")
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"success": true,
					"result": map[string]any{
						"deviceTimeArray":     []string{dayTime + " 00:00:00", dayTime + " 00:05:00", dayTime + " 23:55:00", nextDay + " 00:00:00"},
						"socArray":            []float64{50.0, 52.0, 58.0, 60.0},
						"powerSolarHomeArray": []float64{12.0, 12.0, 12.0, 12.0},
						"powerFhpHomeArray":   []float64{0.0, 0.0, 0.0, 0.0},
						"powerSolarGirdArray": []float64{0.0, 0.0, 0.0, 0.0},
						"powerSolarFhpArray":  []float64{0.0, 0.0, 0.0, 0.0},
						"powerGirdFhpArray":   []float64{0.0, 0.0, 0.0, 0.0},
						"powerGirdHomeArray":  []float64{0.0, 0.0, 0.0, 0.0},
						"powerFhpGirdArray":   []float64{0.0, 0.0, 0.0, 0.0},
					},
				})
			default:
				http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
			}
		})

		http.DefaultTransport = syncTestRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Host {
			case "fleet-api.prd.na.vn.cloud.tesla.com":
				rec := httptest.NewRecorder()
				teslaHandler.ServeHTTP(rec, r)
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				return rec.Result(), nil
			case "energy.franklinwh.com":
				rec := httptest.NewRecorder()
				franklinHandler.ServeHTTP(rec, r)
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				return rec.Result(), nil
			default:
				return origTransport.RoundTrip(r)
			}
		})

		configuredESS := ess.Configured()

		var wg sync.WaitGroup
		var teslaSavedBatches [][]types.DailyEnergyStats
		var franklinSavedBatches [][]types.DailyEnergyStats
		var teslaWarnBuf, franklinWarnBuf bytes.Buffer
		var teslaStatus, franklinStatus int

		wg.Add(2)

		// Run Tesla and Franklin settings backfills concurrently under testSyncTimeout
		go func() {
			defer wg.Done()
			var mu sync.Mutex
			mockS := &mockStorage{}
			mockS.On("GetSettings", mock.Anything, "site-tesla-30s").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
			expectUpdateSettings(mockS, mock.Anything, "site-tesla-30s", types.Settings{}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-tesla-30s").Return(time.Time{}, 0, nil).Once()
			mockS.On("UpsertEnergyHistories", mock.Anything, "site-tesla-30s", mock.Anything, types.CurrentEnergyStatsVersion).Run(func(args mock.Arguments) {
				mu.Lock()
				defer mu.Unlock()
				batch := append([]types.DailyEnergyStats(nil), args.Get(2).([]types.DailyEnergyStats)...)
				teslaSavedBatches = append(teslaSavedBatches, batch)
			}).Return(nil)
			mockS.On("GetEnergyHistory", mock.Anything, "site-tesla-30s", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, "site-tesla-30s", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
			mockS.On("GetActionHistory", mock.Anything, "site-tesla-30s", mock.Anything, mock.Anything).Return([]types.Action{}, nil).Maybe()
			mockS.On("UpdateHistorySummary", mock.Anything, "site-tesla-30s", mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

			srv := &Server{
				storage:             mockS,
				ess:                 configuredESS,
				utilities:           utility.NewMap(mockS),
				bypassAuth:          true,
				encryptionKey:       "test-secret-key-1234567890123456",
				nowFunc:             func() time.Time { return now },
				settingsSyncTimeout: testSyncTimeout,
			}

			bodyBytes, _ := json.Marshal(struct {
				types.Settings
				Credentials *types.Credentials `json:"credentials"`
			}{
				Settings: types.Settings{
					ESS:                      "tesla",
					MinBatterySOC:            20,
					SolarTrendRatioMax:       3,
					SolarBellCurveMultiplier: 1,
					CustomGridSettings:       true,
				},
				Credentials: &types.Credentials{
					Tesla: &types.TeslaCredentials{
						Region:       "NA",
						AccessToken:  "valid-tesla-access",
						RefreshToken: "valid-tesla-refresh",
						Expiry:       now.Add(2 * time.Hour),
						EnergySiteID: 1234,
					},
				},
			})

			ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&teslaWarnBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
			ctx = context.WithValue(ctx, siteIDContextKey, "site-tesla-30s")
			ctx = context.WithValue(ctx, userContextKey, types.User{ID: "admin", Email: "admin@example.com", Admin: true})
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bodyBytes)).WithContext(ctx)
			w := httptest.NewRecorder()
			srv.handleUpdateSettings(w, req)
			teslaStatus = w.Result().StatusCode
		}()

		go func() {
			defer wg.Done()
			var mu sync.Mutex
			mockS := &mockStorage{}
			mockS.On("GetSettings", mock.Anything, "site-franklin-30s").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
			expectUpdateSettings(mockS, mock.Anything, "site-franklin-30s", types.Settings{}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()
			mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-franklin-30s").Return(time.Time{}, 0, nil).Once()
			mockS.On("UpsertEnergyHistories", mock.Anything, "site-franklin-30s", mock.Anything, types.CurrentEnergyStatsVersion).Run(func(args mock.Arguments) {
				mu.Lock()
				defer mu.Unlock()
				batch := append([]types.DailyEnergyStats(nil), args.Get(2).([]types.DailyEnergyStats)...)
				franklinSavedBatches = append(franklinSavedBatches, batch)
			}).Return(nil)
			mockS.On("GetEnergyHistory", mock.Anything, "site-franklin-30s", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
			mockS.On("GetWeather", mock.Anything, "site-franklin-30s", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
			mockS.On("GetActionHistory", mock.Anything, "site-franklin-30s", mock.Anything, mock.Anything).Return([]types.Action{}, nil).Maybe()
			mockS.On("UpdateHistorySummary", mock.Anything, "site-franklin-30s", mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

			srv := &Server{
				storage:             mockS,
				ess:                 configuredESS,
				utilities:           utility.NewMap(mockS),
				bypassAuth:          true,
				encryptionKey:       "test-secret-key-1234567890123456",
				nowFunc:             func() time.Time { return now },
				settingsSyncTimeout: testSyncTimeout,
			}

			bodyBytes, _ := json.Marshal(struct {
				types.Settings
				Credentials *types.Credentials `json:"credentials"`
			}{
				Settings: types.Settings{
					ESS:                      "franklin",
					MinBatterySOC:            20,
					SolarTrendRatioMax:       3,
					SolarBellCurveMultiplier: 1,
					CustomGridSettings:       true,
				},
				Credentials: &types.Credentials{
					Franklin: &types.FranklinCredentials{
						Username:    "owner@example.com",
						MD5Password: "md5password",
						GatewayID:   "gw-1",
						Token:       "valid-franklin-token",
					},
				},
			})

			ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&franklinWarnBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
			ctx = context.WithValue(ctx, siteIDContextKey, "site-franklin-30s")
			ctx = context.WithValue(ctx, userContextKey, types.User{ID: "admin", Email: "admin@example.com", Admin: true})
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bodyBytes)).WithContext(ctx)
			w := httptest.NewRecorder()
			srv.handleUpdateSettings(w, req)
			franklinStatus = w.Result().StatusCode
		}()

		wg.Wait()

		assert.Equal(t, http.StatusOK, teslaStatus)
		assert.GreaterOrEqual(t, len(teslaSavedBatches), 1, "Tesla must persist at least one 5-day window before settingsSyncTimeout")
		if len(teslaSavedBatches) > 0 {
			assert.Len(t, teslaSavedBatches[0], 5)
		}
		teslaLogs := strings.Split(strings.TrimSpace(teslaWarnBuf.String()), "\n")
		if assert.Len(t, teslaLogs, 1, "Tesla settings backfill timeout must emit 1 WARN and 0 ERROR logs") {
			assert.Contains(t, teslaLogs[0], `"level":"WARN"`)
			assert.NotContains(t, teslaLogs[0], `"level":"ERROR"`)
		}

		assert.Equal(t, http.StatusOK, franklinStatus)
		assert.GreaterOrEqual(t, len(franklinSavedBatches), 1, "Franklin must persist at least one 5-day window before settingsSyncTimeout")
		if len(franklinSavedBatches) > 0 {
			assert.Len(t, franklinSavedBatches[0], 5, "Franklin 5-day window must filter out lookahead day beyond endDay")
		}
		franklinLogs := strings.Split(strings.TrimSpace(franklinWarnBuf.String()), "\n")
		if assert.NotEmpty(t, franklinLogs, "Franklin settings backfill timeout must emit WARN and 0 ERROR logs") {
			for _, line := range franklinLogs {
				assert.Contains(t, line, `"level":"WARN"`)
				assert.NotContains(t, line, `"level":"ERROR"`)
			}
			assert.Contains(t, franklinWarnBuf.String(), "failed to sync energy history after settings update")
		}
	})

	t.Run("NonFatal Weather Energy And Price Sync Failures In HandleUpdateSettings Emit Warn And Zero Error Logs", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, "site-settings-warn").Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, "site-settings-warn", types.Settings{}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockS.On("DeleteInterest", mock.Anything, "admin@example.com").Return(nil).Maybe()
		mockS.On("GetLatestWeatherTime", mock.Anything, "site-settings-warn").Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockS.On("GetLatestEnergyHistoryTime", mock.Anything, "site-settings-warn").Return(time.Time{}, 0, nil).Once()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-settings-warn").Return(time.Time{}, 0, nil).Once()
		mockS.On("GetEnergyHistory", mock.Anything, "site-settings-warn", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("GetWeather", mock.Anything, "site-settings-warn", mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetActionHistory", mock.Anything, "site-settings-warn", mock.Anything, mock.Anything).Return([]types.Action{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, "site-settings-warn", mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()

		mockW := &mockWeather{}
		mockW.On("Location", mock.Anything, "US", "90210").Return(types.SiteLocation{
			PostalCode:  "90210",
			CountryCode: "US",
			Latitude:    34.07,
			Longitude:   -118.40,
			TimeZone:    "America/Los_Angeles",
		}, nil).Once()
		mockW.On("Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, fmt.Errorf("weather api returned status 503")).Times(2)

		mockES := &mockESS{MockName: "franklin"}
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "u@example.com", Token: "tok"},
		}, false, nil).Once()
		mockES.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, fmt.Errorf("upstream 502")).Once()

		essMap := ess.NewMap()
		essMap.SetSystem("site-settings-warn", mockES)

		mockU := &mockUtility{}
		mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
		mockU.On("GetConfirmedPrices", mock.Anything, mock.Anything, mock.Anything).Return([]types.Price{}, fmt.Errorf("utility 503")).Once()

		uMap := utility.NewMap(mockS)
		uMap.SetProvider("site-settings-warn", mockU)

		srv := &Server{
			storage:           mockS,
			weather:           mockW,
			ess:               essMap,
			utilities:         uMap,
			bypassAuth:        true,
			encryptionKey:     "test-secret-key-1234567890123456",
			weatherRetryDelay: time.Millisecond,
		}

		bodyBytes, err := json.Marshal(struct {
			types.Settings
			Credentials *types.Credentials `json:"credentials"`
		}{
			Settings: types.Settings{
				ESS:                      "franklin",
				UtilityProvider:          "mock",
				CountryCode:              "US",
				PostalCode:               "90210",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
			},
			Credentials: &types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username: "u@example.com",
					Password: "secret",
				},
			},
		})
		require.NoError(t, err)

		var logBuf bytes.Buffer
		ctx := log.With(context.Background(), slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		ctx = context.WithValue(ctx, siteIDContextKey, "site-settings-warn")
		ctx = context.WithValue(ctx, userContextKey, types.User{ID: "admin", Email: "admin@example.com", Admin: true})
		req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bodyBytes)).WithContext(ctx)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
		if assert.Len(t, lines, 4, "weather (retry + final), energy, and price non-fatal sync failures must emit 4 WARNs and 0 ERRORs") {
			for _, line := range lines {
				assert.Contains(t, line, `"level":"WARN"`)
				assert.NotContains(t, line, `"level":"ERROR"`)
			}
		}
	})

	t.Run("getESSSystem Aborts Write-Back When Same-Provider Credentials Change Concurrently", func(t *testing.T) {
		fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		t.Run("Franklin ErrAccountLocked with updated=false aborts when admin saves new credentials or unlocks token", func(t *testing.T) {
			mockS := &mockStorage{}
			mockBgFranklin := &mockESS{MockName: "franklin"}
			mockAdminFranklin := &mockESS{MockName: "franklin"}
			essMap := ess.NewMap()
			essMap.SetSystem("site-franklin-supersede", mockBgFranklin)

			srv := &Server{
				storage:       mockS,
				ess:           essMap,
				encryptionKey: "test-secret-key-1234567890123456",
				nowFunc:       func() time.Time { return fixedNow },
			}

			encOrig, err := srv.encryptCredentials(context.Background(), "site-franklin-supersede", types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:  "owner@example.com",
					Password:  "mock-same-pw",
					Token:     "mock-expired-franklin-token",
					GatewayID: "GW-1",
				},
			})
			require.NoError(t, err)

			store := attachAtomicSettingsStore(mockS, "site-franklin-supersede", types.Settings{
				ESS:                      "franklin",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
				CustomGridSettings:       true,
				EncryptedCredentials:     encOrig,
				ESSAuthStatus: types.ESSAuthStatus{
					ConsecutiveFailures: 1,
					LastAttempt:         fixedNow.Add(-5 * time.Minute),
				},
			}, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

			svBg, credsBg, err := srv.getSettingsWithMigration(context.Background(), "site-franklin-supersede")
			require.NoError(t, err)

			authStarted := make(chan struct{})
			adminSaved := make(chan struct{})
			lockedErr := fmt.Errorf("account locked: %w: %w", ess.ErrAccountLocked, ess.ErrUnauthorized)
			mockBgFranklin.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockBgFranklin.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				close(authStarted)
				<-adminSaved
			}).Return(types.Credentials{}, false, lockedErr).Once()

			// Admin unlocks account using the SAME username & password, producing a fresh Token
			mockAdminFranklin.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
			mockAdminFranklin.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
				Franklin: &types.FranklinCredentials{
					Username:  "owner@example.com",
					Password:  "mock-same-pw",
					Token:     "mock-unlocked-franklin-token",
					GatewayID: "GW-1",
				},
			}, true, nil).Once()

			var wg sync.WaitGroup
			var bgErr error
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, bgErr = srv.getESSSystem(context.Background(), "site-franklin-supersede", svBg, credsBg)
			}()

			<-authStarted
			essMap.SetSystem("site-franklin-supersede", mockAdminFranklin)
			adminBody, err := json.Marshal(struct {
				types.Settings
				Credentials *types.Credentials `json:"credentials"`
			}{
				Settings: types.Settings{
					ESS:                      "franklin",
					MinBatterySOC:            20,
					SolarTrendRatioMax:       3,
					SolarBellCurveMultiplier: 1,
					CustomGridSettings:       true,
				},
				Credentials: &types.Credentials{
					Franklin: &types.FranklinCredentials{
						Username: "owner@example.com",
						Password: "mock-same-pw",
					},
				},
			})
			require.NoError(t, err)

			adminReq := withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(adminBody)), "admin@example.com", true)
			adminReq = adminReq.WithContext(context.WithValue(adminReq.Context(), siteIDContextKey, "site-franklin-supersede"))
			adminW := httptest.NewRecorder()
			srv.handleUpdateSettings(adminW, adminReq)
			require.Equal(t, http.StatusOK, adminW.Result().StatusCode, adminW.Body.String())
			close(adminSaved)

			wg.Wait()
			require.ErrorIs(t, bgErr, ess.ErrAccountLocked)

			finalSettings, _, _, _ := store.snapshot()
			assert.Equal(t, 0, finalSettings.ESSAuthStatus.ConsecutiveFailures, "stale ErrAccountLocked must not jump ConsecutiveFailures to 40 on unlocked credentials")
			decFinal, _, err := srv.decryptCredentials(context.Background(), "site-franklin-supersede", finalSettings.EncryptedCredentials)
			require.NoError(t, err)
			require.NotNil(t, decFinal.Franklin)
			assert.Equal(t, "mock-unlocked-franklin-token", decFinal.Franklin.Token)
		})

		t.Run("Enphase OTP re-auth with empty Password aborts stale ErrUnauthorized failure and updated=true failure", func(t *testing.T) {
			for _, updatedBeforeErr := range []bool{false, true} {
				name := "updated_false"
				if updatedBeforeErr {
					name = "updated_true"
				}
				t.Run(name, func(t *testing.T) {
					siteID := "site-enphase-otp-" + name
					mockS := &mockStorage{}
					mockBgEnphase := &mockESS{MockName: "enphase"}
					mockStageEnphase := &mockESS{MockName: "enphase"}
					essMap := ess.NewMap()
					essMap.SetSystem(siteID, mockBgEnphase)

					srv := &Server{
						storage:       mockS,
						ess:           essMap,
						encryptionKey: "test-secret-key-1234567890123456",
						nowFunc:       func() time.Time { return fixedNow },
					}

					// OTP account has empty Password before and after re-authentication
					origCreds := types.Credentials{
						Enphase: &types.EnphaseCredentials{
							Username:     "owner@example.com",
							Password:     "",
							SessionID:    "mock-expired-otp-sess",
							ManagerToken: "mock-expired-otp-mgr",
							SystemID:     555,
							UserID:       42,
						},
					}
					encOrig, err := srv.encryptCredentials(context.Background(), siteID, origCreds)
					require.NoError(t, err)

					store := attachAtomicSettingsStore(mockS, siteID, types.Settings{
						ESS:                  "enphase",
						MinBatterySOC:        20,
						EncryptedCredentials: encOrig,
						ESSAuthStatus: types.ESSAuthStatus{
							ConsecutiveFailures: 1,
							LastAttempt:         fixedNow.Add(-5 * time.Minute),
						},
					}, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

					svBg, credsBg, err := srv.getSettingsWithMigration(context.Background(), siteID)
					require.NoError(t, err)

					authStarted := make(chan struct{})
					otpSaved := make(chan struct{})
					bgReturnedCreds := types.Credentials{}
					if updatedBeforeErr {
						bgReturnedCreds = types.Credentials{
							Enphase: &types.EnphaseCredentials{
								Username:     "owner@example.com",
								Password:     "",
								SessionID:    "mock-stale-rotated-sess",
								ManagerToken: "mock-stale-rotated-mgr",
								SystemID:     555,
								UserID:       42,
							},
						}
					}
					mockBgEnphase.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
					mockBgEnphase.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
						close(authStarted)
						<-otpSaved
					}).Return(bgReturnedCreds, updatedBeforeErr, fmt.Errorf("session expired: %w", ess.ErrUnauthorized)).Once()

					mockStageEnphase.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
					mockStageEnphase.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
						Enphase: &types.EnphaseCredentials{
							Username:     "owner@example.com",
							Password:     "",
							SessionID:    "mock-fresh-otp-sess",
							ManagerToken: "mock-fresh-otp-mgr",
							SystemID:     555,
							UserID:       42,
						},
					}, true, nil).Once()

					var wg sync.WaitGroup
					var bgErr error
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, _, bgErr = srv.getESSSystem(context.Background(), siteID, svBg, credsBg)
					}()

					<-authStarted
					essMap.SetSystem(siteID, mockStageEnphase)
					stageBody, err := json.Marshal(map[string]any{
						"ess": "enphase",
						"credentials": map[string]any{
							"enphase": map[string]any{
								"username": "owner@example.com",
								"code":     "123456",
							},
						},
					})
					require.NoError(t, err)

					stageReq := withUser(httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(stageBody)), "admin@example.com", true)
					stageReq = stageReq.WithContext(context.WithValue(stageReq.Context(), siteIDContextKey, siteID))
					stageW := httptest.NewRecorder()
					srv.handleESSStage(stageW, stageReq)
					require.Equal(t, http.StatusOK, stageW.Result().StatusCode, stageW.Body.String())
					close(otpSaved)

					wg.Wait()
					require.ErrorIs(t, bgErr, ess.ErrUnauthorized)

					finalSettings, _, _, _ := store.snapshot()
					assert.Equal(t, 0, finalSettings.ESSAuthStatus.ConsecutiveFailures, "stale Enphase failure must not jump ConsecutiveFailures to 40 after OTP re-auth")
					decFinal, _, err := srv.decryptCredentials(context.Background(), siteID, finalSettings.EncryptedCredentials)
					require.NoError(t, err)
					require.NotNil(t, decFinal.Enphase)
					assert.Empty(t, decFinal.Enphase.Password)
					assert.Equal(t, "mock-fresh-otp-sess", decFinal.Enphase.SessionID)
					assert.Equal(t, "mock-fresh-otp-mgr", decFinal.Enphase.ManagerToken)
				})
			}
		})

		t.Run("Tesla aborts stale failure and stale rotation when RefreshToken or EnergySiteID/Region changes without later Expiry", func(t *testing.T) {
			runCase := func(t *testing.T, name string, bgSucceeds, bgUpdated bool, adminTesla types.TeslaCredentials) {
				t.Helper()
				siteID := "site-tesla-supersede-" + name
				mockS := &mockStorage{}
				mockBgTesla := &mockESS{MockName: "tesla"}
				essMap := ess.NewMap()
				essMap.SetSystem(siteID, mockBgTesla)

				srv := &Server{
					storage:       mockS,
					ess:           essMap,
					encryptionKey: "test-secret-key-1234567890123456",
					nowFunc:       func() time.Time { return fixedNow },
				}

				origCreds := types.Credentials{
					Tesla: &types.TeslaCredentials{
						Region:       "NA",
						AccessToken:  "mock-orig-access",
						RefreshToken: "mock-orig-refresh",
						Expiry:       fixedNow.Add(-time.Minute),
						EnergySiteID: 1234,
					},
				}
				encOrig, err := srv.encryptCredentials(context.Background(), siteID, origCreds)
				require.NoError(t, err)

				store := attachAtomicSettingsStore(mockS, siteID, types.Settings{
					ESS:                  "tesla",
					EncryptedCredentials: encOrig,
					ESSAuthStatus: types.ESSAuthStatus{
						ConsecutiveFailures: 1,
						LastAttempt:         fixedNow.Add(-5 * time.Minute),
					},
				}, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

				svBg, credsBg, err := srv.getSettingsWithMigration(context.Background(), siteID)
				require.NoError(t, err)

				authStarted := make(chan struct{})
				adminUpdated := make(chan struct{})
				bgNewCreds := types.Credentials{}
				if bgUpdated {
					bgNewCreds = types.Credentials{
						Tesla: &types.TeslaCredentials{
							Region:       "NA",
							AccessToken:  "mock-bg-rotated-access",
							RefreshToken: "mock-bg-rotated-refresh",
							Expiry:       fixedNow.Add(8 * time.Hour),
							EnergySiteID: 1234,
						},
					}
				}
				var bgRetErr error
				if !bgSucceeds {
					bgRetErr = fmt.Errorf("unauthorized: %w", ess.ErrUnauthorized)
				}

				mockBgTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
				mockBgTesla.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					close(authStarted)
					<-adminUpdated
				}).Return(bgNewCreds, bgUpdated, bgRetErr).Once()

				var wg sync.WaitGroup
				var bgErr error
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _, bgErr = srv.getESSSystem(context.Background(), siteID, svBg, credsBg)
				}()

				<-authStarted
				encAdmin, err := srv.encryptCredentials(context.Background(), siteID, types.Credentials{
					Tesla: &adminTesla,
				})
				require.NoError(t, err)
				_, _, _, err = store.update(func(cur types.Settings, ver int) (types.Settings, int, bool, error) {
					cur.EncryptedCredentials = encAdmin
					cur.ESSAuthStatus.ConsecutiveFailures = 0
					cur.ESSAuthStatus.LastAttempt = fixedNow
					return cur, ver, true, nil
				})
				require.NoError(t, err)
				close(adminUpdated)

				wg.Wait()
				if bgSucceeds {
					require.NoError(t, bgErr)
				} else {
					require.ErrorIs(t, bgErr, ess.ErrUnauthorized)
				}

				finalSettings, _, _, _ := store.snapshot()
				assert.Equal(t, 0, finalSettings.ESSAuthStatus.ConsecutiveFailures)
				decFinal, _, err := srv.decryptCredentials(context.Background(), siteID, finalSettings.EncryptedCredentials)
				require.NoError(t, err)
				require.NotNil(t, decFinal.Tesla)
				assert.Equal(t, adminTesla.RefreshToken, decFinal.Tesla.RefreshToken)
				assert.Equal(t, adminTesla.AccessToken, decFinal.Tesla.AccessToken)
				assert.Equal(t, adminTesla.Region, decFinal.Tesla.Region)
				assert.Equal(t, adminTesla.EnergySiteID, decFinal.Tesla.EnergySiteID)
				assert.True(t, adminTesla.Expiry.Equal(decFinal.Tesla.Expiry))
			}

			t.Run("error_updated_true_new_refresh_token_earlier_expiry", func(t *testing.T) {
				runCase(t, "error_updated_true_new_refresh_token_earlier_expiry", false, true, types.TeslaCredentials{
					Region:       "NA",
					AccessToken:  "mock-admin-access",
					RefreshToken: "mock-admin-refresh-reauth",
					// Minted 50ms BEFORE the background refresh expiry!
					Expiry:       fixedNow.Add(8*time.Hour - 50*time.Millisecond),
					EnergySiteID: 1234,
				})
			})

			t.Run("error_updated_false_changed_site_and_region", func(t *testing.T) {
				runCase(t, "error_updated_false_changed_site_and_region", false, false, types.TeslaCredentials{
					Region:       "EU",
					AccessToken:  "mock-orig-access",
					RefreshToken: "mock-orig-refresh",
					Expiry:       fixedNow.Add(-time.Minute),
					EnergySiteID: 9999,
				})
			})

			t.Run("success_updated_true_new_refresh_token_earlier_expiry", func(t *testing.T) {
				runCase(t, "success_updated_true_new_refresh_token_earlier_expiry", true, true, types.TeslaCredentials{
					Region:       "NA",
					AccessToken:  "mock-admin-access-ok",
					RefreshToken: "mock-admin-refresh-ok",
					Expiry:       fixedNow.Add(8*time.Hour - 50*time.Millisecond),
					EnergySiteID: 1234,
				})
			})
		})
	})

	t.Run("getESSSystem Rotating Active Credentials Preserves Concurrent Stage Credentials For Another Provider", func(t *testing.T) {
		fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

		for _, bgErrCase := range []bool{false, true} {
			name := "bg_success"
			if bgErrCase {
				name = "bg_error_after_rotation"
			}
			t.Run(name, func(t *testing.T) {
				siteID := "site-stage-merge-" + name
				mockS := &mockStorage{}
				mockTesla := &mockESS{MockName: "tesla"}
				mockEnphase := &mockESS{MockName: "enphase"}
				essMap := ess.NewMap()
				essMap.SetSystem(siteID, mockTesla)

				srv := &Server{
					storage:       mockS,
					ess:           essMap,
					encryptionKey: "test-secret-key-1234567890123456",
					nowFunc:       func() time.Time { return fixedNow },
				}

				encOrig, err := srv.encryptCredentials(context.Background(), siteID, types.Credentials{
					Tesla: &types.TeslaCredentials{
						Region:       "NA",
						AccessToken:  "mock-old-tesla-access",
						RefreshToken: "mock-old-tesla-refresh",
						Expiry:       fixedNow.Add(-time.Minute),
						EnergySiteID: 1234,
					},
				})
				require.NoError(t, err)

				store := attachAtomicSettingsStore(mockS, siteID, types.Settings{
					ESS:                  "tesla",
					MinBatterySOC:        20,
					EncryptedCredentials: encOrig,
				}, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

				svBg, credsBg, err := srv.getSettingsWithMigration(context.Background(), siteID)
				require.NoError(t, err)

				authStarted := make(chan struct{})
				stageDone := make(chan struct{})
				var authRetErr error
				if bgErrCase {
					authRetErr = fmt.Errorf("upstream 502 after token refresh")
				}

				mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
				mockTesla.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					close(authStarted)
					<-stageDone
				}).Return(types.Credentials{
					Tesla: &types.TeslaCredentials{
						Region:       "NA",
						AccessToken:  "mock-rotated-tesla-access",
						RefreshToken: "mock-rotated-tesla-refresh",
						Expiry:       fixedNow.Add(8 * time.Hour),
						EnergySiteID: 1234,
					},
				}, true, authRetErr).Once()

				mockEnphase.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)
				mockEnphase.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
					Enphase: &types.EnphaseCredentials{
						Username:     "owner@example.com",
						SessionID:    "mock-staged-enphase-sess",
						ManagerToken: "mock-staged-enphase-mgr",
						SystemID:     888,
					},
				}, true, nil).Once()

				var wg sync.WaitGroup
				var bgErr error
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _, bgErr = srv.getESSSystem(context.Background(), siteID, svBg, credsBg)
				}()

				<-authStarted
				// Concurrently run handleESSStage for Enphase while active ESS remains "tesla"
				essMap.SetSystem(siteID, mockEnphase)
				stageBody, err := json.Marshal(map[string]any{
					"ess": "enphase",
					"credentials": map[string]any{
						"enphase": map[string]any{
							"username": "owner@example.com",
							"code":     "654321",
						},
					},
				})
				require.NoError(t, err)
				stageReq := withUser(httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(stageBody)), "admin@example.com", true)
				stageReq = stageReq.WithContext(context.WithValue(stageReq.Context(), siteIDContextKey, siteID))
				stageW := httptest.NewRecorder()
				srv.handleESSStage(stageW, stageReq)
				require.Equal(t, http.StatusOK, stageW.Result().StatusCode, stageW.Body.String())
				close(stageDone)

				wg.Wait()
				if bgErrCase {
					require.ErrorContains(t, bgErr, "upstream 502")
				} else {
					require.NoError(t, bgErr)
				}

				finalSettings, _, _, _ := store.snapshot()
				assert.Equal(t, "tesla", finalSettings.ESS)
				decFinal, _, err := srv.decryptCredentials(context.Background(), siteID, finalSettings.EncryptedCredentials)
				require.NoError(t, err)
				require.NotNil(t, decFinal.Tesla, "rotated active Tesla credentials must be saved")
				assert.Equal(t, "mock-rotated-tesla-access", decFinal.Tesla.AccessToken)
				assert.Equal(t, "mock-rotated-tesla-refresh", decFinal.Tesla.RefreshToken)
				require.NotNil(t, decFinal.Enphase, "concurrently staged Enphase credentials must not be wiped out")
				assert.Equal(t, "mock-staged-enphase-sess", decFinal.Enphase.SessionID)
				assert.Equal(t, "mock-staged-enphase-mgr", decFinal.Enphase.ManagerToken)
			})
		}
	})
}

func populateSettingsReflectValue(v reflect.Value, seed int, boolVal bool) {
	if !v.CanSet() {
		return
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		v.Set(reflect.ValueOf(time.Date(2026, time.Month((seed%12)+1), (seed%27)+1, 12, 0, 0, 0, time.UTC)))
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(boolVal)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(seed))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(seed))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(seed) + 0.25)
	case reflect.String:
		v.SetString(fmt.Sprintf("mock-val-%d", seed))
	case reflect.Pointer:
		elem := reflect.New(v.Type().Elem())
		populateSettingsReflectValue(elem.Elem(), seed+1, boolVal)
		v.Set(elem)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes(fmt.Appendf(nil, "mock-bytes-%d", seed))
			return
		}
		sl := reflect.MakeSlice(v.Type(), 1, 1)
		populateSettingsReflectValue(sl.Index(0), seed+1, boolVal)
		v.Set(sl)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		populateSettingsReflectValue(k, seed+1, boolVal)
		val := reflect.New(v.Type().Elem()).Elem()
		populateSettingsReflectValue(val, seed+2, boolVal)
		m.SetMapIndex(k, val)
		v.Set(m)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				populateSettingsReflectValue(v.Field(i), seed*10+i+1, boolVal)
			}
		}
	default:
		panic(fmt.Sprintf("unsupported reflect kind %v for %v", v.Kind(), v.Type()))
	}
}

func TestMergeAdminSettingsUpdate(t *testing.T) {
	t.Run("Copies every user-editable field from edited and restores server-managed fields from current", func(t *testing.T) {
		for _, editedBool := range []bool{true, false} {
			var current, edited types.Settings
			populateSettingsReflectValue(reflect.ValueOf(&current).Elem(), 11, !editedBool)
			populateSettingsReflectValue(reflect.ValueOf(&edited).Elem(), 37, editedBool)

			// Verify reflection helper populated every exported field on both structs
			rt := reflect.TypeFor[types.Settings]()
			for i := range rt.NumField() {
				f := rt.Field(i)
				if !f.IsExported() || f.Type.Kind() == reflect.Bool {
					continue
				}
				require.False(t, reflect.ValueOf(current).Field(i).IsZero(), "current.%s should be non-zero", f.Name)
				require.False(t, reflect.ValueOf(edited).Field(i).IsZero(), "edited.%s should be non-zero", f.Name)
			}

			// Set preconditions for unchanged-location, same-ESS CustomGridSettings preservation, and valid TOU UpdateGroup
			current.ESS = "tesla"
			edited.ESS = "tesla"
			current.CustomGridSettings = true
			edited.CustomGridSettings = false
			current.UtilityProvider = "pg_e"
			edited.UtilityProvider = "ameren"
			current.UpdateGroup = 7
			edited.UpdateGroup = 99
			current.CountryCode = "US"
			edited.CountryCode = "US"
			current.PostalCode = "60601"
			edited.PostalCode = "60601"

			merged := mergeAdminSettingsUpdate(
				current,
				edited,
				false,
				false,
				nil,
				time.Time{},
				time.Time{},
				14,
				5,
			)

			rvCur := reflect.ValueOf(current)
			rvEd := reflect.ValueOf(edited)
			rvMerged := reflect.ValueOf(merged)

			for i := range rt.NumField() {
				f := rt.Field(i)
				if !f.IsExported() {
					continue
				}
				curVal := rvCur.Field(i).Interface()
				edVal := rvEd.Field(i).Interface()
				mergedVal := rvMerged.Field(i).Interface()

				switch f.Name {
				case "Notifications", "UpdateGroup", "EncryptedCredentials", "ESSAuthStatus", "Location", "CustomGridSettings":
					assert.NotEqual(t, curVal, edVal, "precondition: %s must differ between current and edited", f.Name)
					assert.Equal(t, curVal, mergedVal, "server-managed field %s must be restored from current", f.Name)
				case "ESS", "CountryCode", "PostalCode":
					assert.Equal(t, edVal, mergedVal, "field %s must match edited", f.Name)
				default:
					assert.NotEqual(t, curVal, edVal, "precondition: %s must differ between current and edited", f.Name)
					assert.Equal(t, edVal, mergedVal, "user-editable field %s must come from edited", f.Name)
				}
			}
		}
	})

	t.Run("Applies new location, credential updates, ESS switch CustomGridSettings, and ComEd UpdateGroup", func(t *testing.T) {
		var current, edited types.Settings
		populateSettingsReflectValue(reflect.ValueOf(&current).Elem(), 15, true)
		populateSettingsReflectValue(reflect.ValueOf(&edited).Elem(), 41, false)

		current.ESS = "tesla"
		edited.ESS = "franklin"
		current.CustomGridSettings = true
		edited.CustomGridSettings = false
		edited.UtilityProvider = "comed"
		edited.UtilityRate = "comed_besh"
		current.UpdateGroup = 4

		authCheck := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		authDone := authCheck.Add(2 * time.Second)
		newEnc := []byte("mock-new-encrypted-creds")

		merged := mergeAdminSettingsUpdate(
			current,
			edited,
			true,
			true,
			newEnc,
			authCheck,
			authDone,
			15,
			6,
		)

		assert.Equal(t, "franklin", merged.ESS)
		assert.False(t, merged.CustomGridSettings, "switching ESS provider should allow CustomGridSettings=false from edited")
		assert.Equal(t, edited.CountryCode, merged.CountryCode)
		assert.Equal(t, edited.PostalCode, merged.PostalCode)
		assert.Equal(t, edited.Location, merged.Location, "fetchedNewLocation=true must use edited.Location")
		assert.Equal(t, newEnc, merged.EncryptedCredentials)
		assert.Equal(t, 0, merged.ESSAuthStatus.ConsecutiveFailures)
		assert.Equal(t, 0, merged.ESSAuthStatus.ConsecutiveSetFailures)
		assert.Equal(t, 0, merged.ESSAuthStatus.ConsecutiveUserFailures)
		assert.Equal(t, authDone, merged.ESSAuthStatus.LastAttempt)
		assert.Equal(t, authDone, merged.ESSAuthStatus.LastUserAttempt)
		assert.Equal(t, 15, merged.UpdateGroup, "ComEd hourly rate must reassign TOU group into 13..16")
		assert.Equal(t, current.Notifications, merged.Notifications, "Notifications must always come from current")
	})
}

func TestGetSettingsWithMigration(t *testing.T) {
	t.Run("Returns Settings And Decrypts Credentials", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		ctx := context.Background()
		siteID := "test-site"

		creds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "test-strategy"},
		}
		encrypted, err := srv.encryptCredentials(ctx, siteID, creds)
		require.NoError(t, err)

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: encrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		sv, returnedCreds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.Equal(t, "test-utility", sv.Settings.UtilityProvider)
		assert.Equal(t, "test-strategy", returnedCreds.Mock.Strategy)
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Upgrades Legacy V1 Credentials To V2 And Saves", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		testKey := "test-secret-key-1234567890123456"
		srv := &Server{
			storage:       mockS,
			encryptionKey: testKey,
		}

		ctx := context.Background()
		siteID := "test-site"

		creds := types.Credentials{
			Mock: &types.MockCredentials{Strategy: "legacy-strategy"},
		}
		legacyEncrypted := encryptLegacyV1Credentials(t, testKey, creds)

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: legacyEncrypted,
		}, types.CurrentSettingsVersion, time.Time{}, nil)

		upgradedTime := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
		expectUpdateSettings(mockS, ctx, siteID, types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: legacyEncrypted,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.UtilityProvider == "test-utility" &&
				bytes.HasPrefix(s.EncryptedCredentials, []byte(credsEncryptionPrefixV2))
		}, types.CurrentSettingsVersion, upgradedTime, nil)

		sv, returnedCreds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.Equal(t, upgradedTime, sv.updatedAt)
		assert.True(t, bytes.HasPrefix(sv.Settings.EncryptedCredentials, []byte(credsEncryptionPrefixV2)))
		assert.Equal(t, "legacy-strategy", returnedCreds.Mock.Strategy)
		assert.True(t, mockS.AssertExpectations(t))
	})

	t.Run("Migrates Settings Below Current Version And Saves", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage: mockS,
		}

		ctx := context.Background()
		siteID := "test-site"

		// Mock returning an old version of settings
		oldVersion := 1 // Assume CurrentSettingsVersion > 1
		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, time.Time{}, nil)

		// Mock the UpdateSettings call that should happen after migration
		migratedTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		expectUpdateSettings(mockS, ctx, siteID, types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.OptimizationProfile == "balanced"
		}, types.CurrentSettingsVersion, migratedTime, nil)

		sv, creds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.Equal(t, migratedTime, sv.updatedAt)
		// We expect UpdateSettings to have been called with the migrated settings
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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage: mockS,
		}

		ctx := context.Background()
		siteID := "test-site"

		oldVersion := 1
		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, time.Time{}, nil)

		// Mock the UpdateSettings call to return an error
		expectUpdateSettings(mockS, ctx, siteID, types.Settings{
			UtilityProvider: "old-utility",
		}, oldVersion, func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.OptimizationProfile == "balanced"
		}, types.CurrentSettingsVersion, time.Time{}, fmt.Errorf("save failed"))

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
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		srv := &Server{
			storage:       mockS,
			encryptionKey: "test-secret-key-1234567890123456",
		}

		ctx := context.Background()
		siteID := "test-site"

		mockS.On("GetSettings", ctx, siteID).Return(types.Settings{
			UtilityProvider:      "test-utility",
			EncryptedCredentials: []byte("invalid-encrypted-data"),
		}, types.CurrentSettingsVersion, time.Time{}, nil)

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
		{2, 30 * time.Minute},
		{3, 30 * time.Minute},
		{4, 30 * time.Minute},
		{5, 45 * time.Minute},
		{6, 45 * time.Minute},
		{7, 45 * time.Minute},
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
			assert.Equal(t, tt.expected, result)
			if tt.failures >= 2 {
				assert.Greater(t, result, 20*time.Minute)
			}
		})
	}

	t.Run("Interactive ESS Backoff Preserves 15m Curve", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), getInteractiveESSBackoff(0))
		assert.Equal(t, time.Duration(0), getInteractiveESSBackoff(1))
		assert.Equal(t, 30*time.Second, getInteractiveESSBackoff(2))
		assert.Equal(t, 60*time.Second, getInteractiveESSBackoff(3))
		assert.Equal(t, 120*time.Second, getInteractiveESSBackoff(4))
		assert.Equal(t, 240*time.Second, getInteractiveESSBackoff(5))
		assert.Equal(t, 480*time.Second, getInteractiveESSBackoff(6))
		assert.Equal(t, 15*time.Minute, getInteractiveESSBackoff(7))
		assert.Equal(t, 15*time.Minute, getInteractiveESSBackoff(40))
	})
}

func TestHandleESSStage(t *testing.T) {
	newAuthServer := func() (*Server, *mockESS, *mockStorage) {
		mockES := &mockESS{MockName: "enphase"}
		essMap := ess.NewMap()
		essMap.SetSystem(types.SiteIDNone, mockES)
		mockES.On("ApplySettings", mock.Anything, mock.Anything).Return(nil)

		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
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
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

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

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("some other error"))

		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()

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
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

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
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Authenticate returns error
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, fmt.Errorf("auth failed")).Once()

		// UpdateSettings must be called to update auth status
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures: 0,
			},
		}, types.CurrentSettingsVersion, nil, types.CurrentSettingsVersion, time.Time{}, nil).Once()

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
				ConsecutiveFailures:     2,
				LastAttempt:             time.Now().UTC(),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         time.Now().UTC(),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

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
				ConsecutiveFailures:     2,
				LastAttempt:             time.Now().UTC().Add(-10 * time.Minute),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         time.Now().UTC().Add(-10 * time.Minute), // wait has expired
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		// Authenticate returns ErrNeedsNextStage (stage 0) -> does NOT reset failure counters prematurely
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, ess.ErrNeedsNextStage).Once()

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
		assert.True(t, mockS.AssertExpectations(t))

		// Complete stage authentication (err = nil) DOES reset failure counters to 0
		srv2, mockES2, mockS2 := newAuthServer()
		mockS2.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     2,
				LastAttempt:             time.Now().UTC().Add(-10 * time.Minute),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         time.Now().UTC().Add(-10 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES2.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		expectUpdateSettings(mockS2, mock.Anything, types.SiteIDNone, types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     2,
				LastAttempt:             time.Now().UTC().Add(-10 * time.Minute),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         time.Now().UTC().Add(-10 * time.Minute),
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		srv2.handleESSStage(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)
		assert.True(t, mockS2.AssertExpectations(t))
	})

	t.Run("Stage rate limit capped at 15 minutes for high failure counts", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()
		testTime := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv.nowFunc = func() time.Time { return testTime }

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

		// 1. With 10 interactive failures and LastUserAttempt 5 minutes ago, rate limit is capped at 15m (10m remaining)
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-5 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-5 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		req1 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req1 = withUser(req1, "admin@example.com", true)
		w1 := httptest.NewRecorder()
		srv.handleESSStage(w1, req1)
		assert.Equal(t, http.StatusTooManyRequests, w1.Result().StatusCode)
		assert.Contains(t, w1.Body.String(), "try again in 10m0s")

		// 2. With 40 background failures and LastUserAttempt 16 minutes ago, stage request is allowed and full auth (err == nil) resets failures
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-1 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-16 * time.Minute),
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS: "enphase",
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     40,
				LastAttempt:             testTime.Add(-1 * time.Minute),
				ConsecutiveUserFailures: 10,
				LastUserAttempt:         testTime.Add(-16 * time.Minute),
			},
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		req2 := httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(b))
		req2 = withUser(req2, "admin@example.com", true)
		w2 := httptest.NewRecorder()
		srv.handleESSStage(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Result().StatusCode)

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Stage saves rotated credentials on error while preserving preexisting provider", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		preexisting := types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "owner@example.com", Token: "frank-tok"},
		}
		encPreexisting, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, preexisting)
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "franklin",
			EncryptedCredentials: encPreexisting,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		rotatedStage := types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				SessionID:    "stage-sess",
				ManagerToken: "stage-mgr",
				SystemID:     555,
			},
		}
		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil && c.Enphase.Code == "654321" && c.Franklin == nil
		})).Return(rotatedStage, true, fmt.Errorf("validation failed")).Once()

		var savedSettings types.Settings
		expectUpdateSettings(mockS, mock.Anything, types.SiteIDNone, types.Settings{
			ESS:                  "franklin",
			EncryptedCredentials: encPreexisting,
		}, types.CurrentSettingsVersion, func(s types.Settings) bool {
			savedSettings = s
			return s.ESS == "franklin" && s.ESSAuthStatus.ConsecutiveFailures == 1
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		body, err := json.Marshal(map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "owner@example.com",
					"code":     "654321",
				},
			},
		})
		require.NoError(t, err)

		req := withUser(httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(body)), "admin@example.com", true)
		w := httptest.NewRecorder()
		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		decrypted, _, err := srv.decryptCredentials(context.Background(), types.SiteIDNone, savedSettings.EncryptedCredentials)
		require.NoError(t, err)
		require.NotNil(t, decrypted.Franklin)
		assert.Equal(t, "frank-tok", decrypted.Franklin.Token)
		require.NotNil(t, decrypted.Enphase)
		assert.Equal(t, "stage-sess", decrypted.Enphase.SessionID)
	})

	t.Run("Stage 0 clears expired Enphase session so fresh OTP can be requested", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()

		existingEnc, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				SessionID:    "expired-session",
				ManagerToken: "expired-token",
				SystemID:     555,
			},
		})
		require.NoError(t, err)

		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(types.Settings{
			ESS:                  "enphase",
			EncryptedCredentials: existingEnc,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		mockES.On("Authenticate", mock.Anything, mock.MatchedBy(func(c types.Credentials) bool {
			return c.Enphase != nil &&
				c.Enphase.Username == "owner@example.com" &&
				c.Enphase.SessionID == "" &&
				c.Enphase.ManagerToken == "" &&
				c.Enphase.Code == ""
		})).Return(types.Credentials{}, false, ess.ErrNeedsNextStage).Once()

		body, err := json.Marshal(map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "owner@example.com",
				},
			},
		})
		require.NoError(t, err)

		req := withUser(httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(body)), "admin@example.com", true)
		w := httptest.NewRecorder()
		srv.handleESSStage(w, req)
		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockES.AssertExpectations(t))
	})

	t.Run("Stage retries stale GetSettings timestamp and preserves concurrent updates without conflict errors", func(t *testing.T) {
		srv, mockES, mockS := newAuthServer()
		fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		srv.nowFunc = func() time.Time { return fixedNow }

		preexisting := types.Credentials{
			Franklin: &types.FranklinCredentials{Username: "owner@example.com", Token: "frank-tok"},
		}
		encPreexisting, err := srv.encryptCredentials(context.Background(), types.SiteIDNone, preexisting)
		require.NoError(t, err)

		store := attachAtomicSettingsStore(mockS, types.SiteIDNone, types.Settings{
			ESS:                  "franklin",
			MinBatterySOC:        20,
			EncryptedCredentials: encPreexisting,
			ESSAuthStatus: types.ESSAuthStatus{
				ConsecutiveFailures:     2,
				LastAttempt:             fixedNow.Add(-10 * time.Minute),
				ConsecutiveUserFailures: 2,
				LastUserAttempt:         fixedNow.Add(-10 * time.Minute),
			},
		}, types.CurrentSettingsVersion, fixedNow.Add(-time.Hour))

		var logBuf bytes.Buffer
		errLogger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))

		mockES.On("Authenticate", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			// Concurrent notification preference + SOC edit commits after handleESSStage called GetSettings
			require.NoError(t, srv.storage.UpdateSiteNotificationSettings(context.Background(), types.SiteIDNone, "member@example.com", types.UserNotificationSettings{
				MorningSummaryEnabled: true,
				MorningSummaryHour:    7,
			}))
			_, _, _, uErr := store.update(func(cur types.Settings, ver int) (types.Settings, int, bool, error) {
				cur.MinBatterySOC = 45
				return cur, ver, true, nil
			})
			require.NoError(t, uErr)
		}).Return(types.Credentials{
			Enphase: &types.EnphaseCredentials{
				Username:     "owner@example.com",
				SessionID:    "stage-verified-sess",
				ManagerToken: "stage-verified-mgr",
				SystemID:     777,
			},
		}, true, nil).Once()

		body, err := json.Marshal(map[string]any{
			"ess": "enphase",
			"credentials": map[string]any{
				"enphase": map[string]any{
					"username": "owner@example.com",
					"code":     "998877",
				},
			},
		})
		require.NoError(t, err)

		req := withUser(httptest.NewRequest("POST", "/api/ess/stage", bytes.NewReader(body)), "admin@example.com", true)
		req = req.WithContext(log.With(req.Context(), errLogger))
		w := httptest.NewRecorder()
		srv.handleESSStage(w, req)

		require.Equal(t, http.StatusOK, w.Result().StatusCode, w.Body.String())
		assert.Empty(t, logBuf.String(), "handleESSStage must emit zero ERROR logs when retrying stale snapshot")

		finalSettings, _, _, _ := store.snapshot()
		assert.Equal(t, "franklin", finalSettings.ESS)
		assert.Equal(t, 45.0, finalSettings.MinBatterySOC)
		assert.Equal(t, 0, finalSettings.ESSAuthStatus.ConsecutiveFailures)
		assert.Equal(t, 0, finalSettings.ESSAuthStatus.ConsecutiveUserFailures)
		assert.True(t, finalSettings.Notifications["member@example.com"].MorningSummaryEnabled)

		decrypted, _, err := srv.decryptCredentials(context.Background(), types.SiteIDNone, finalSettings.EncryptedCredentials)
		require.NoError(t, err)
		require.NotNil(t, decrypted.Franklin)
		assert.Equal(t, "frank-tok", decrypted.Franklin.Token)
		require.NotNil(t, decrypted.Enphase)
		assert.Equal(t, "stage-verified-sess", decrypted.Enphase.SessionID)
	})
}

func TestValidateAndCalculateMinBatterySOCPeriods(t *testing.T) {
	ctx := context.Background()
	srv := &Server{}

	t.Run("EmptyPeriodsReturnsDefaultSOC", func(t *testing.T) {
		s := types.Settings{MinBatterySOC: 25.0}
		minSOC, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		require.NoError(t, err)
		assert.Equal(t, 25.0, minSOC)
	})

	t.Run("ValidCustomPeriodsCalculatesMinSOC", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOC: 10.0,
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
					MinBatterySOC: 50.0,
				},
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		minSOC, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		require.NoError(t, err)
		assert.Equal(t, 20.0, minSOC)
	})

	t.Run("InvalidSOCValueNegative", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
					MinBatterySOC: -5.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "minimum battery SOC period value must be between 0 and 100")
	})

	t.Run("InvalidSOCValueOver100", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
					MinBatterySOC: 105.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "minimum battery SOC period value must be between 0 and 100")
	})

	t.Run("NonZeroStartMinutes", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, MinuteStart: 15, HourEnd: 24, MinuteEnd: 0}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC period start and end minutes must be 0")
	})

	t.Run("NonZeroEndMinutes", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, MinuteStart: 0, HourEnd: 24, MinuteEnd: 30}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC period start and end minutes must be 0")
	})

	t.Run("EmptyHoursInPeriod", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC period must specify hours")
	})

	t.Run("Uncovered24Hours", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
					MinBatterySOC: 20.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC periods must cover all 24 hours of the day (hour 12 is not covered)")
	})

	t.Run("OverlappingHours", func(t *testing.T) {
		s := types.Settings{
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}}},
					MinBatterySOC: 20.0,
				},
				{
					TimePeriod:    types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
					MinBatterySOC: 40.0,
				},
			},
		}
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "reserve SOC periods cannot overlap (hour 12 has multiple periods defined)")
	})

	t.Run("RateBasedMinSOC_UtilityHasNoPeriods_ReturnsError", func(t *testing.T) {
		mockUMap := utility.NewMap(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_bes",
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "configured utility has no TOU rate periods for rate-based minimum SOC")
	})

	t.Run("RateBasedMinSOC_MissingPeriodName_ReturnsError", func(t *testing.T) {
		mockUMap := utility.NewMap(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_c",
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "missing minimum battery SOC for utility period name:")
	})

	t.Run("RateBasedMinSOC_PartialDaySiteFees_ReturnsError", func(t *testing.T) {
		mockUMap := utility.Configured(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
			UtilityFeesPeriods: []types.UtilityFeesPeriod{
				{
					TimePeriod: types.TimePeriod{
						Name:  "Peak",
						Hours: []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
					},
					DollarsPerKWH: 0.15,
				},
			},
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "configured utility has no TOU rate periods for rate-based minimum SOC")
	})

	t.Run("RateBasedMinSOC_MixedNamedAndUnnamed_ReturnsError", func(t *testing.T) {
		mockUMap := utility.NewMap(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_c",
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
				{TimePeriod: types.TimePeriod{Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}}, MinBatterySOC: 20.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		assert.ErrorContains(t, err, "all reserve SOC periods must specify a utility period name")
	})

	t.Run("RateBasedMinSOC_AmerenMOOptionB_Valid", func(t *testing.T) {
		mockUMap := utility.Configured(nil)
		sTest := &Server{
			utilities: mockUMap,
			nowFunc:   func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) },
		}
		s := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_overnight",
			UtilityRateOptions: types.UtilityRateOptions{
				PeakPeriodOption: "option_b",
			},
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "On-Peak", MinBatterySOC: 15.0},
				{UtilityPeriodName: "Off-Peak", MinBatterySOC: 30.0},
				{UtilityPeriodName: "Anytime", MinBatterySOC: 20.0},
			},
		}
		minSOC, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s)
		require.NoError(t, err)
		assert.Equal(t, 15.0, minSOC)
	})
}
