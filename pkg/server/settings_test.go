package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/raterudder/raterudder/pkg/weather"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return true
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		mockS.On("SetSettings", mock.Anything, "site1", mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(time.Time{}, nil)
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
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return s.ESSAuthStatus.ConsecutiveFailures == 1 && s.ESSAuthStatus.LastAttempt.Equal(testTime)
		}), 1, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}), 1, mock.Anything).Return(testTime, nil).Once()

		_, out1, err := srv.getESSSystem(ctx, "site1", settingsWithVersion{version: 1}, types.Credentials{})
		require.ErrorIs(t, err, ess.ErrUnauthorized)
		assert.Equal(t, 1, out1.ESSAuthStatus.ConsecutiveFailures)

		// 2. Retry on next poll (ConsecutiveFailures == 1 has 0s backoff): if it succeeds, resets to 0
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, nil).Once()
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}), 1, mock.Anything).Return(testTime, nil).Once()

		sys, outRecovered, err := srv.getESSSystem(ctx, "site1", out1, types.Credentials{})
		require.NoError(t, err)
		require.NotNil(t, sys)
		assert.Equal(t, 0, outRecovered.ESSAuthStatus.ConsecutiveFailures)

		// 3. Second permanent error when ConsecutiveFailures >= 1 jumps directly to 40
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, permErr).Once()
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 40
		}), 1, mock.Anything).Return(testTime, nil).Once()

		_, outPaused, err := srv.getESSSystem(ctx, "site1", out1, types.Credentials{})
		require.ErrorIs(t, err, ess.ErrUnauthorized)
		assert.Equal(t, 40, outPaused.ESSAuthStatus.ConsecutiveFailures)

		// 4. Explicit lockout error jumps directly from 0 to 40 without a retry
		lockedErr := fmt.Errorf("password locked: %w: %w", ess.ErrAccountLocked, ess.ErrUnauthorized)
		mockES.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{}, false, lockedErr).Once()
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 40
		}), 1, mock.Anything).Return(testTime, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, "site1", mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return s.ESSAuthStatus.ConsecutiveFailures == 1 && len(s.EncryptedCredentials) > 0
		}), 1, mock.Anything).Return(testTime, nil).Once()

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
		mockStorage.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(time.Time{}, nil)
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

		// Expect SetSettings with version
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.MinBatterySOC == 80.0 && s.DryRun == true && s.UpdateGroup == 5
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "mock" && len(s.EncryptedCredentials) > 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		// Expect SetSettings to be called to update auth status after failure
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		// Expect SetSettings to be called
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.GridChargeBatteries == true && s.GridExportSolar == true && s.GridExportBatteries == true
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.GridChargeBatteries == true && s.GridExportSolar == true && s.GridExportBatteries == false && s.ManageTOUSchedules == true
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.GridChargeBatteries && s.GridExportSolar && s.GridExportBatteries && s.ManageTOUSchedules
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.GridChargeBatteries && s.GridExportSolar && s.GridExportBatteries && !s.ManageTOUSchedules
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.CustomGridSettings == true && s.GridChargeBatteries == false && s.GridExportSolar == true && s.GridExportBatteries == false
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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
		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.CustomGridSettings == true && s.GridChargeBatteries == false && s.GridExportSolar == true && s.GridExportBatteries == true
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Twice()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "90210"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			// Verify that geo data is preserved when zip/country did not change
			return s.Location != nil &&
				s.Location.Latitude == 34.0736 &&
				s.Location.Longitude == -118.4004
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil &&
				s.Location.PostalCode == "60601" &&
				s.Location.Latitude == 41.8818
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("Update Location - Rejects Invalid and Oversized Inputs Early", func(t *testing.T) {
		mockS := &mockStorage{}
		mockW := &mockWeather{}
		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		postSettings := func(countryCode, postalCode string) *httptest.ResponseRecorder {
			payload, err := json.Marshal(map[string]any{
				"countryCode":        countryCode,
				"postalCode":         postalCode,
				"solarTrendRatioMax": 3,
			})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(payload))
			req = withUser(req, "test@test.com", true)
			rec := httptest.NewRecorder()
			srv.handleUpdateSettings(rec, req)
			return rec
		}

		recOversized := postSettings("US", strings.Repeat("%", 400000))
		assert.Equal(t, http.StatusBadRequest, recOversized.Code)
		assert.Contains(t, recOversized.Body.String(), "postal code")

		recCtrlPostal := postSettings("US", "60601\n")
		assert.Equal(t, http.StatusBadRequest, recCtrlPostal.Code)
		assert.Contains(t, recCtrlPostal.Body.String(), "control characters")

		recCtrlCountry := postSettings("U\x00", "60601")
		assert.Equal(t, http.StatusBadRequest, recCtrlCountry.Code)
		assert.Contains(t, recCtrlCountry.Body.String(), "control characters")

		recLongCountry := postSettings("USA1", "60601")
		assert.Equal(t, http.StatusBadRequest, recLongCountry.Code)
		assert.Contains(t, recLongCountry.Body.String(), "country code")

		mockS.AssertNotCalled(t, "GetSettings", mock.Anything, mock.Anything)
		mockW.AssertNotCalled(t, "Location", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Update Location - Allows International Alphanumeric Postal Codes", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)
		mockW := &mockWeather{}

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		loc := types.SiteLocation{
			PostalCode:  "SW1A 1AA",
			CountryCode: "GB",
			Latitude:    51.5014,
			Longitude:   -0.1419,
			TimeZone:    "Europe/London",
		}
		mockW.On("Location", mock.Anything, "GB", "SW1A 1AA").Return(loc, nil).Once()
		mockW.On("Forecast", mock.Anything, loc, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Once()
		mockS.On("GetLatestWeatherTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockS.On("GetWeather", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "SW1A 1AA" && s.Location.CountryCode == "GB"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

		payload, err := json.Marshal(types.Settings{
			CountryCode:        "GB",
			PostalCode:         "SW1A 1AA",
			SolarTrendRatioMax: 3,
		})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(payload))
		req = withUser(req, "test@test.com", true)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		// Also verify hyphenated postal codes (e.g. "K1A-0B1")
		locHyphen := types.SiteLocation{
			PostalCode:  "K1A-0B1",
			CountryCode: "CA",
			Latitude:    45.4215,
			Longitude:   -75.6972,
			TimeZone:    "America/Toronto",
		}
		mockW.On("Location", mock.Anything, "CA", "K1A-0B1").Return(locHyphen, nil).Once()
		mockW.On("Forecast", mock.Anything, locHyphen, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Once()
		mockS.On("GetLatestWeatherTime", mock.Anything, types.SiteIDNone).Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "K1A-0B1" && s.Location.CountryCode == "CA"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

		payloadHyphen, err := json.Marshal(types.Settings{
			CountryCode:        "CA",
			PostalCode:         "K1A-0B1",
			SolarTrendRatioMax: 3,
		})
		require.NoError(t, err)
		reqHyphen := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(payloadHyphen))
		reqHyphen = withUser(reqHyphen, "test@test.com", true)
		wHyphen := httptest.NewRecorder()
		srv.handleUpdateSettings(wHyphen, reqHyphen)
		require.Equal(t, http.StatusOK, wHyphen.Code, wHyphen.Body.String())

		assert.True(t, mockS.AssertExpectations(t))
		assert.True(t, mockW.AssertExpectations(t))
	})

	t.Run("Update Location - Preserves No Location Found Message", func(t *testing.T) {
		mockS := &mockStorage{}
		mockS.On("GetSettings", mock.Anything, mock.Anything).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockW := &mockWeather{}
		mockW.On("Location", mock.Anything, "US", "00000").Return(
			types.SiteLocation{},
			fmt.Errorf("%w for zip code 00000 and country code US", weather.ErrNoLocationFound),
		).Once()

		srv := &Server{
			storage: mockS,
			weather: mockW,
		}

		payload, err := json.Marshal(types.Settings{
			CountryCode:        "US",
			PostalCode:         "00000",
			SolarTrendRatioMax: 3,
		})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(payload))
		req = withUser(req, "test@test.com", true)
		w := httptest.NewRecorder()
		srv.handleUpdateSettings(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "no location found")
	})

	t.Run("Update Location - Redacts Weather API Keys Across HTTP2 TLS and Closed Port", func(t *testing.T) {
		const (
			gmapsMarker     = "FAKE_GMAPS_KEY_MARKER_9911"
			openMeteoMarker = "FAKE_OPENMETEO_KEY_MARKER_8822"
			siteID          = "site-weather-redact"
		)

		logBuf := &lockedLogBuffer{}
		testLogger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		// Start a loopback HTTP/2 TLS server that aborts streams (triggering HTTP/2 *url.Error)
		// and a closed loopback listener (triggering dial *url.Error).
		h2Upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))
		h2Upstream.EnableHTTP2 = true
		h2Upstream.StartTLS()
		defer h2Upstream.Close()

		closedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		closedBaseURL := closedUpstream.URL
		closedClient := closedUpstream.Client()
		closedUpstream.Close()

		h2GeoURL, err := url.Parse(h2Upstream.URL + "/v4/geocode/address")
		require.NoError(t, err)
		h2TZURL, err := url.Parse(h2Upstream.URL + "/maps/api/timezone/json")
		require.NoError(t, err)
		closedGeoURL, err := url.Parse(closedBaseURL + "/v4/geocode/address")
		require.NoError(t, err)
		closedTZURL, err := url.Parse(closedBaseURL + "/maps/api/timezone/json")
		require.NoError(t, err)
		closedOMGeoURL, err := url.Parse(closedBaseURL + "/v1/search")
		require.NoError(t, err)
		closedOMForecastURL, err := url.Parse(closedBaseURL + "/v1/forecast")
		require.NoError(t, err)

		mockS := &mockStorage{}
		mockS.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
		mockS.On("GetLatestAction", mock.Anything, mock.Anything).Return((*types.Action)(nil), nil).Maybe()
		mockS.On("GetUser", mock.Anything, "admin-1").Return(types.User{
			ID:            "admin-1",
			Email:         "admin@example.com",
			SessionSecret: "fake-sec-weather",
			Sites:         []types.UserSite{{ID: siteID, Name: "Home"}},
		}, nil)
		mockS.On("GetSite", mock.Anything, siteID).Return(types.Site{
			ID:          siteID,
			Permissions: []types.SitePermissions{{UserID: "admin-1"}},
		}, nil)
		mockS.On("GetSettings", mock.Anything, siteID).Return(types.Settings{}, types.CurrentSettingsVersion, time.Time{}, nil)

		omClosed := &weather.OpenMeteo{
			GeocodingURL: closedOMGeoURL,
			ForecastURL:  closedOMForecastURL,
			HTTPClient:   closedClient,
			APIKey:       openMeteoMarker,
		}
		gmapsH2 := &weather.GoogleGeocodingWrapper{
			OpenMeteo:    omClosed,
			APIKey:       gmapsMarker,
			GeocodingURL: h2GeoURL,
			TimezoneURL:  h2TZURL,
			HTTPClient:   h2Upstream.Client(),
		}

		srv := &Server{
			storage:              mockS,
			weather:              gmapsH2,
			adminEmails:          []string{"admin@example.com"},
			oidcAudiences:        map[string]string{"google": "test-client"},
			encryptionKey:        "12345678901234567890123456789012",
			sessionEncryptionKey: "fake-session-key-123456789012345",
			sessionDuration:      time.Hour,
			release:              "production",
			generalRateLimit:     rate.Inf,
			generalBurst:         100,
			sensitiveRateLimit:   rate.Inf,
			sensitiveBurst:       100,
		}

		sessionToken, err := srv.createSessionToken("admin-1", "admin@example.com", "fake-sec-weather", time.Hour)
		require.NoError(t, err)

		apiServer := httptest.NewUnstartedServer(srv.setupHandler())
		apiServer.Config.BaseContext = func(net.Listener) context.Context {
			return log.With(context.Background(), testLogger)
		}
		apiServer.Start()
		defer apiServer.Close()

		reqWithSessionCookie := func(method, targetURL string, payload []byte) *http.Request {
			req, reqErr := http.NewRequest(method, targetURL, bytes.NewReader(payload))
			require.NoError(t, reqErr)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
			return req
		}

		// 1. Oversized postalCode against loopback HTTP/2 TLS Google geocoder
		oversizedPayload, err := json.Marshal(map[string]any{
			"countryCode":        "US",
			"postalCode":         strings.Repeat("%", 400000),
			"solarTrendRatioMax": 3,
		})
		require.NoError(t, err)
		resp1, err := apiServer.Client().Do(reqWithSessionCookie(http.MethodPost, apiServer.URL+"/api/settings?siteID="+siteID, oversizedPayload))
		require.NoError(t, err)
		body1, err := io.ReadAll(resp1.Body)
		_ = resp1.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp1.StatusCode)
		assert.NotContains(t, string(body1), gmapsMarker)
		assert.NotContains(t, string(body1), openMeteoMarker)

		// 2. Valid postalCode ("60601") against loopback HTTP/2 TLS stream error
		validPayload, err := json.Marshal(map[string]any{
			"countryCode":        "US",
			"postalCode":         "60601",
			"solarTrendRatioMax": 3,
		})
		require.NoError(t, err)
		respH2, err := apiServer.Client().Do(reqWithSessionCookie(http.MethodPost, apiServer.URL+"/api/settings?siteID="+siteID, validPayload))
		require.NoError(t, err)
		bodyH2, err := io.ReadAll(respH2.Body)
		_ = respH2.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, respH2.StatusCode)
		assert.Contains(t, string(bodyH2), "failed to fetch location data")
		assert.NotContains(t, string(bodyH2), gmapsMarker)
		assert.NotContains(t, string(bodyH2), openMeteoMarker)
		assert.NotContains(t, string(bodyH2), h2Upstream.URL)

		// 3. Valid postalCode ("60601") against closed loopback port with GoogleGeocodingWrapper
		srv.weather = &weather.GoogleGeocodingWrapper{
			OpenMeteo:    omClosed,
			APIKey:       gmapsMarker,
			GeocodingURL: closedGeoURL,
			TimezoneURL:  closedTZURL,
			HTTPClient:   closedClient,
		}
		resp2, err := apiServer.Client().Do(reqWithSessionCookie(http.MethodPost, apiServer.URL+"/api/settings?siteID="+siteID, validPayload))
		require.NoError(t, err)
		body2, err := io.ReadAll(resp2.Body)
		_ = resp2.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
		assert.Contains(t, string(body2), "failed to fetch location data")
		assert.NotContains(t, string(body2), gmapsMarker)
		assert.NotContains(t, string(body2), openMeteoMarker)
		assert.NotContains(t, string(body2), closedBaseURL)

		// 4. Valid postalCode ("60601") against closed loopback port with OpenMeteo directly
		srv.weather = omClosed
		resp3, err := apiServer.Client().Do(reqWithSessionCookie(http.MethodPost, apiServer.URL+"/api/settings?siteID="+siteID, validPayload))
		require.NoError(t, err)
		body3, err := io.ReadAll(resp3.Body)
		_ = resp3.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp3.StatusCode)
		assert.Contains(t, string(body3), "failed to fetch location data")
		assert.NotContains(t, string(body3), gmapsMarker)
		assert.NotContains(t, string(body3), openMeteoMarker)

		// 5. Geocoding succeeds while Forecast fails in updateWeatherHistory: Open-Meteo key must not leak in logs
		okGeoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v4/geocode/address" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []map[string]any{
						{
							"postalAddress": map[string]any{"locality": "Chicago"},
							"location":      map[string]any{"latitude": 41.8818, "longitude": -87.6231},
						},
					},
				})
				return
			}
			if r.URL.Path == "/maps/api/timezone/json" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":     "OK",
					"timeZoneId": "America/Chicago",
				})
				return
			}
		}))
		defer okGeoServer.Close()

		okGeoURL, err := url.Parse(okGeoServer.URL + "/v4/geocode/address")
		require.NoError(t, err)
		okTZURL, err := url.Parse(okGeoServer.URL + "/maps/api/timezone/json")
		require.NoError(t, err)

		mockS.On("GetLatestWeatherTime", mock.Anything, siteID).Return(time.Time{}, time.Time{}, 0, nil).Once()
		mockS.On("GetWeather", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.Weather{}, nil).Maybe()
		mockS.On("GetEnergyHistory", mock.Anything, siteID, mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{}, nil).Maybe()
		mockS.On("UpdateHistorySummary", mock.Anything, siteID, mock.Anything, mock.Anything).Return(types.HistorySummary{}, nil).Maybe()
		mockS.On("SetSettings", mock.Anything, siteID, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

		srv.weather = &weather.GoogleGeocodingWrapper{
			OpenMeteo:    omClosed,
			APIKey:       gmapsMarker,
			GeocodingURL: okGeoURL,
			TimezoneURL:  okTZURL,
			HTTPClient:   okGeoServer.Client(),
		}
		resp4, err := apiServer.Client().Do(reqWithSessionCookie(http.MethodPost, apiServer.URL+"/api/settings?siteID="+siteID, validPayload))
		require.NoError(t, err)
		body4, err := io.ReadAll(resp4.Body)
		_ = resp4.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp4.StatusCode)
		assert.NotContains(t, string(body4), gmapsMarker)
		assert.NotContains(t, string(body4), openMeteoMarker)

		loggedOutput := logBuf.String()
		assert.NotEmpty(t, loggedOutput)
		assert.NotContains(t, loggedOutput, gmapsMarker)
		assert.NotContains(t, loggedOutput, openMeteoMarker)
		assert.Contains(t, loggedOutput, "failed to sync weather history after settings update")
		assert.Contains(t, loggedOutput, "REDACTED")
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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil)

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

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}), mock.Anything, mock.Anything).Return(time.Time{}, nil)

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

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 41 && s.ESSAuthStatus.ConsecutiveUserFailures == 11
		}), mock.Anything, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 41 && s.ESSAuthStatus.ConsecutiveUserFailures == 1
		}), mock.Anything, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, mock.Anything, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}), mock.Anything, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility" && set.UpdateGroup > 0 && set.UpdateGroup <= 16
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "comed" && set.UpdateGroup >= 13 && set.UpdateGroup <= 16
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "pge" && set.UpdateGroup >= 1 && set.UpdateGroup <= 12
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(set types.Settings) bool {
			return set.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		// 3. SetSettings expectation
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.Location != nil && s.Location.PostalCode == "60601" && s.UtilityProvider == "new-utility"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		// Assert that SetSettings receives the updated settings with NetMeteringCredits unset (false)
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.UtilityRateOptions.NetMeteringScheme == "nem2" && !s.UtilityRateOptions.NetMeteringCredits
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

			mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

			mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

			mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
				return s.Pause && !s.ManageTOUSchedules
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

			mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

			mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

			mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
				return s.Pause && s.ManageTOUSchedules
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			savedOnFailure = s
			return s.ESS == "franklin" && s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			savedOnSuccess = s
			return s.ESS == "enphase" && s.ESSAuthStatus.ConsecutiveFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return s.ESS == "franklin"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "tesla" &&
				bytes.Equal(s.EncryptedCredentials, encExisting) &&
				s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			savedOnFailure = s
			return s.ESS == "tesla" && s.ESSAuthStatus.ConsecutiveFailures == 2
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			savedOnSuccess = s
			return s.ESS == "tesla" && s.ESSAuthStatus.ConsecutiveFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.AssertNotCalled(t, "SetSettings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

		w1 := httptest.NewRecorder()
		srv.handleUpdateSettings(w1, withUser(httptest.NewRequest("POST", "/api/settings", bytes.NewReader(reqBody)), "admin@example.com", true))
		assert.Equal(t, http.StatusBadRequest, w1.Result().StatusCode)
		var resp1 struct {
			Error            string `json:"error"`
			CredentialsSaved bool   `json:"credentialsSaved"`
		}
		require.NoError(t, json.NewDecoder(w1.Body).Decode(&resp1))
		assert.False(t, resp1.CredentialsSaved)

		// Case 2: credsUpdated == true but SetSettings fails -> credentialsSaved must be false
		mockS.On("GetSettings", mock.Anything, types.SiteIDNone).Return(existingSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockTesla.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()
		mockTesla.On("Authenticate", mock.Anything, mock.Anything).Return(types.Credentials{
			Tesla: &types.TeslaCredentials{AccessToken: "tok", RefreshToken: "ref"},
		}, true, fmt.Errorf("upstream 503")).Once()
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, fmt.Errorf("db write failed")).Once()

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESS == "enphase" && s.ESSAuthStatus.ConsecutiveFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Twice()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()
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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
			mockS.On("SetSettings", mock.Anything, "site-tesla-30s", mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()
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
			mockS.On("SetSettings", mock.Anything, "site-franklin-30s", mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()
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
		mockS.On("SetSettings", mock.Anything, "site-settings-warn", mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()
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

	t.Run("Failed POST Settings Preserves Cached Site Utility Provider", func(t *testing.T) {
		mockS := &mockStorage{}
		initialSettings := types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: utility.ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat,
			},
			MinBatterySOC:            20,
			SolarTrendRatioMax:       3,
			SolarBellCurveMultiplier: 1,
		}

		uMap := utility.Configured(mockS)
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		logCtx := log.With(context.Background(), logger)

		cached, err := uMap.Site(logCtx, "site-preserve-cache", initialSettings)
		require.NoError(t, err)

		srv := &Server{
			storage:    mockS,
			utilities:  uMap,
			bypassAuth: true,
		}

		postSettings := func(candidate types.Settings) int {
			bodyBytes, err := json.Marshal(candidate)
			require.NoError(t, err)
			reqCtx := context.WithValue(logCtx, siteIDContextKey, "site-preserve-cache")
			reqCtx = context.WithValue(reqCtx, userContextKey, types.User{ID: "admin", Email: "admin@example.com", Admin: true})
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bodyBytes)).WithContext(reqCtx)
			w := httptest.NewRecorder()
			srv.handleUpdateSettings(w, req)
			return w.Result().StatusCode
		}

		// 1. Invalid candidate utility rate fails with 400 and preserves cached provider
		invalidRate := initialSettings
		invalidRate.UtilityProvider = "pg_e"
		invalidRate.UtilityRate = "unknown_rate"
		assert.Equal(t, http.StatusBadRequest, postSettings(invalidRate))

		// 2. Valid candidate utility rate but incomplete named MinBatterySOCPeriods fails with 400 and preserves cached provider
		missingPeriod := initialSettings
		missingPeriod.UtilityProvider = "pg_e"
		missingPeriod.UtilityRate = "pg_e_e_tou_c"
		missingPeriod.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
			{UtilityPeriodName: "Peak", MinBatterySOC: 50},
		}
		assert.Equal(t, http.StatusBadRequest, postSettings(missingPeriod))

		// 3. Valid candidate utility rate where SetSettings fails with 500 preserves cached provider
		mockS.On("GetSettings", mock.Anything, "site-preserve-cache").Return(initialSettings, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-preserve-cache").Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Maybe()
		mockS.On("UpsertPrices", mock.Anything, "site-preserve-cache", mock.Anything, types.CurrentPriceHistoryVersion).Return(nil).Maybe()
		mockS.On("SetSettings", mock.Anything, "site-preserve-cache", mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, errors.New("firestore unavailable")).Once()

		saveFail := initialSettings
		saveFail.UtilityProvider = "pg_e"
		saveFail.UtilityRate = "pg_e_ev2"
		assert.Equal(t, http.StatusInternalServerError, postSettings(saveFail))

		logBuf.Reset()
		after, err := uMap.Site(logCtx, "site-preserve-cache", initialSettings)
		require.NoError(t, err)
		assert.Same(t, cached, after)
		assert.NotContains(t, logBuf.String(), "site changed utility rate")
		mockS.AssertExpectations(t)
	})

	t.Run("Named MinBatterySOCPeriods Update Validates Tariffs Once", func(t *testing.T) {
		t.Run("CustomMockProviderValidatedOnce", func(t *testing.T) {
			mockS := &mockStorage{}
			existing := types.Settings{
				UtilityProvider:          "mock",
				UtilityRate:              "mock",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			mockS.On("GetSettings", mock.Anything, "site-named-once").Return(existing, types.CurrentSettingsVersion, time.Time{}, nil).Once()
			mockS.On("SetSettings", mock.Anything, "site-named-once", mock.MatchedBy(func(saved types.Settings) bool {
				return saved.MinBatterySOC == 15 && len(saved.MinBatterySOCPeriods) == 2
			}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

			mockU := &mockUtility{}
			mockU.On("ApplySettings", mock.Anything, mock.Anything).Return(nil).Once()
			mockU.On("GetPeriods", mock.Anything).Return([]types.TimePeriod{
				{Name: "On-Peak", Hours: []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}}},
				{Name: "Off-Peak", Hours: []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}}},
			}, nil).Once()

			uMap := utility.NewMap(mockS)
			uMap.SetProvider("site-named-once", mockU)

			srv := &Server{
				storage:    mockS,
				utilities:  uMap,
				bypassAuth: true,
			}

			updated := existing
			updated.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "On-Peak", MinBatterySOC: 40},
				{UtilityPeriodName: "Off-Peak", MinBatterySOC: 15},
			}
			bodyBytes, err := json.Marshal(updated)
			require.NoError(t, err)

			reqCtx := context.WithValue(context.Background(), siteIDContextKey, "site-named-once")
			reqCtx = context.WithValue(reqCtx, userContextKey, types.User{ID: "admin", Email: "admin@example.com", Admin: true})
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bodyBytes)).WithContext(reqCtx)
			w := httptest.NewRecorder()
			srv.handleUpdateSettings(w, req)

			assert.Equal(t, http.StatusOK, w.Result().StatusCode, w.Body.String())
			mockS.AssertExpectations(t)
			mockU.AssertExpectations(t)
		})

		t.Run("ConfiguredUtilityRebuiltOnNextSiteLookup", func(t *testing.T) {
			mockS := &mockStorage{}
			existing := types.Settings{
				UtilityProvider:          "comed",
				UtilityRate:              "comed_besh",
				MinBatterySOC:            20,
				SolarTrendRatioMax:       3,
				SolarBellCurveMultiplier: 1,
			}
			var savedSettings types.Settings
			mockS.On("GetSettings", mock.Anything, "site-rebuild-tou").Return(existing, types.CurrentSettingsVersion, time.Time{}, nil).Once()
			mockS.On("GetLatestPriceHistoryTime", mock.Anything, "site-rebuild-tou").Return(time.Now().Add(8*time.Hour), types.CurrentPriceHistoryVersion, nil).Once()
			mockS.On("UpsertPrices", mock.Anything, "site-rebuild-tou", mock.Anything, types.CurrentPriceHistoryVersion).Return(nil).Maybe()
			mockS.On("SetSettings", mock.Anything, "site-rebuild-tou", mock.Anything, types.CurrentSettingsVersion, mock.Anything).Run(func(args mock.Arguments) {
				savedSettings = args.Get(2).(types.Settings)
			}).Return(time.Time{}, nil).Once()

			uMap := utility.Configured(mockS)
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			logCtx := log.With(context.Background(), logger)

			prevU, err := uMap.Site(logCtx, "site-rebuild-tou", existing)
			require.NoError(t, err)

			srv := &Server{
				storage:    mockS,
				utilities:  uMap,
				bypassAuth: true,
			}

			candidate := existing
			candidate.UtilityProvider = "ameren"
			candidate.UtilityRate = "ameren_mo_overnight"
			candidate.UtilityRateOptions = types.UtilityRateOptions{PeakPeriodOption: "option_b"}
			candidate.MinBatterySOCPeriods = []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "On-Peak", MinBatterySOC: 35},
				{UtilityPeriodName: "Off-Peak", MinBatterySOC: 20},
				{UtilityPeriodName: "Anytime", MinBatterySOC: 15},
			}
			bodyBytes, err := json.Marshal(candidate)
			require.NoError(t, err)

			logBuf.Reset()
			reqCtx := context.WithValue(logCtx, siteIDContextKey, "site-rebuild-tou")
			reqCtx = context.WithValue(reqCtx, userContextKey, types.User{ID: "admin", Email: "admin@example.com", Admin: true})
			req := httptest.NewRequest("POST", "/api/settings", bytes.NewReader(bodyBytes)).WithContext(reqCtx)
			w := httptest.NewRecorder()
			srv.handleUpdateSettings(w, req)

			require.Equal(t, http.StatusOK, w.Result().StatusCode, w.Body.String())
			assert.Equal(t, 15.0, savedSettings.MinBatterySOC)
			assert.NotContains(t, logBuf.String(), "site changed utility rate")

			logBuf.Reset()
			nextU, err := uMap.Site(logCtx, "site-rebuild-tou", savedSettings)
			require.NoError(t, err)
			assert.NotSame(t, prevU, nextU)
			assert.Contains(t, logBuf.String(), `"msg":"site changed utility rate"`)
			assert.Contains(t, logBuf.String(), `"level":"INFO"`)
			assert.NotContains(t, logBuf.String(), `"level":"WARN"`)
			mockS.AssertExpectations(t)
		})
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
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			return s.UtilityProvider == "test-utility" &&
				bytes.HasPrefix(s.EncryptedCredentials, []byte(credsEncryptionPrefixV2))
		}), types.CurrentSettingsVersion, mock.Anything).Return(upgradedTime, nil)

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

		// Mock the SetSettings call that should happen after migration
		migratedTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.OptimizationProfile == "balanced"
		}), types.CurrentSettingsVersion, mock.Anything).Return(migratedTime, nil)

		sv, creds, err := srv.getSettingsWithMigration(ctx, siteID)

		require.NoError(t, err)
		assert.Equal(t, types.CurrentSettingsVersion, sv.version)
		assert.Equal(t, migratedTime, sv.updatedAt)
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

		// Mock the SetSettings call to return an error
		mockS.On("SetSettings", ctx, siteID, mock.MatchedBy(func(s types.Settings) bool {
			// Basic validation to ensure settings are actually migrated/passed correctly
			return s.UtilityProvider == "old-utility" && s.OptimizationProfile == "balanced"
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, fmt.Errorf("save failed"))

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

		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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

		// SetSettings must be called to update auth status
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.Anything, types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS2.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			return s.ESSAuthStatus.ConsecutiveFailures == 0 && s.ESSAuthStatus.ConsecutiveUserFailures == 0
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
		mockS.On("SetSettings", mock.Anything, types.SiteIDNone, mock.MatchedBy(func(s types.Settings) bool {
			savedSettings = s
			return s.ESS == "franklin" && s.ESSAuthStatus.ConsecutiveFailures == 1
		}), types.CurrentSettingsVersion, mock.Anything).Return(time.Time{}, nil).Once()

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
}

func TestValidateAndCalculateMinBatterySOCPeriods(t *testing.T) {
	ctx := context.Background()
	srv := &Server{}

	t.Run("EmptyPeriodsReturnsDefaultSOC", func(t *testing.T) {
		s := types.Settings{MinBatterySOC: 25.0}
		minSOC, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		minSOC, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := srv.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
		assert.ErrorContains(t, err, "missing minimum battery SOC for utility period name:")
	})

	t.Run("RateBasedMinSOC_SiteFeesNoPeriods_ReturnsError", func(t *testing.T) {
		mockUMap := utility.Configured(nil)
		sTest := &Server{utilities: mockUMap}
		s := types.Settings{
			UtilityProvider: "comed",
			UtilityRate:     "comed_besh",
			MinBatterySOCPeriods: []types.MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		_, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
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
		minSOC, err := sTest.validateAndCalculateMinBatterySOCPeriods(ctx, "site1", s, nil)
		require.NoError(t, err)
		assert.Equal(t, 15.0, minSOC)
	})
}
