package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

const testAppsEncryptionKey = "0123456789abcdef0123456789abcdef"

func newTestAppsServer(mockS *mockStorage, now time.Time) *Server {
	return &Server{
		storage:              mockS,
		sessionEncryptionKey: "fedcba9876543210fedcba9876543210",
		appsEncryptionKey:    testAppsEncryptionKey,
		generalRateLimit:     rate.Inf,
		generalBurst:         100,
		sensitiveRateLimit:   rate.Inf,
		sensitiveBurst:       100,
		nowFunc:              func() time.Time { return now },
	}
}

func TestValidateAppName(t *testing.T) {
	t.Run("EmptyName", func(t *testing.T) {
		_, err := validateAppName("   ")
		assert.ErrorContains(t, err, "name is required")
	})

	t.Run("ExceedsMaxLength", func(t *testing.T) {
		_, err := validateAppName(strings.Repeat("a", 65))
		assert.ErrorContains(t, err, "64 characters or fewer")
	})

	t.Run("ContainsControlCharacters", func(t *testing.T) {
		_, err := validateAppName("bad\nname")
		assert.ErrorContains(t, err, "control characters")
	})

	t.Run("ValidTrimmedName", func(t *testing.T) {
		valid, err := validateAppName("  Muse Agent  ")
		require.NoError(t, err)
		assert.Equal(t, "Muse Agent", valid)
	})
}

func TestHandleListApps(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("AppsNotEnabled", func(t *testing.T) {
		srv := newTestAppsServer(&mockStorage{}, now)
		srv.appsEncryptionKey = ""
		srv.bypassAuth = true
		srv.singleSite = true
		handler := srv.setupHandler()

		req := httptest.NewRequest(http.MethodGet, "/api/apps", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})

	t.Run("FiltersBySiteAndExcludesUnexpiredPending", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)

		sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:            "user-1",
			Email:         "user1@example.com",
			SessionSecret: "sec-1",
			Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
			Apps: []types.UserApp{
				{
					ID:        "site1-active",
					Name:      "Claude MCP",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-1",
					TSCreated: now,
				},
				{
					ID:        "other-site-app",
					Name:      "Other Site Key",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-2",
					TSCreated: now,
				},
				{
					ID:               "pending-oauth-app",
					Name:             "Pending OAuth",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-1",
					TSCreated:        now,
					PendingCodeHash:  "hash123",
					PendingExpiresAt: now.Add(time.Minute),
				},
			},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()

		handler := srv.setupHandler()
		reqList := httptest.NewRequest(http.MethodGet, "/api/apps?siteID=site-1", nil)
		reqList.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wList := httptest.NewRecorder()
		handler.ServeHTTP(wList, reqList)
		require.Equal(t, http.StatusOK, wList.Code)

		var listRes struct {
			Apps []types.UserApp `json:"apps"`
		}
		require.NoError(t, json.NewDecoder(wList.Body).Decode(&listRes))
		if assert.Len(t, listRes.Apps, 1) {
			assert.Equal(t, "site1-active", listRes.Apps[0].ID)
			assert.Equal(t, "Claude MCP", listRes.Apps[0].Name)
		}
		mockS.AssertExpectations(t)
	})

	t.Run("RemovesUpToThreeExpiredPendingApps", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)

		sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:            "user-1",
			Email:         "user1@example.com",
			SessionSecret: "sec-1",
			Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
			Apps: []types.UserApp{
				{
					ID:        "site1-active",
					Name:      "Claude MCP",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-1",
					TSCreated: now,
				},
				{
					ID:               "expired-1",
					Name:             "Expired 1",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-1",
					TSCreated:        now.Add(-5 * time.Minute),
					PendingCodeHash:  "hash1",
					PendingExpiresAt: now.Add(-1 * time.Minute),
				},
				{
					ID:               "expired-2",
					Name:             "Expired 2",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-2",
					TSCreated:        now.Add(-5 * time.Minute),
					PendingCodeHash:  "hash2",
					PendingExpiresAt: now.Add(-1 * time.Minute),
				},
				{
					ID:               "expired-3",
					Name:             "Expired 3",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-1",
					TSCreated:        now.Add(-5 * time.Minute),
					PendingCodeHash:  "hash3",
					PendingExpiresAt: now, // exactly at now -> expired
				},
				{
					ID:               "expired-4-skipped",
					Name:             "Expired 4",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-1",
					TSCreated:        now.Add(-5 * time.Minute),
					PendingCodeHash:  "hash4",
					PendingExpiresAt: now.Add(-1 * time.Minute),
				},
			},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()

		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "expired-1").Return(nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-2", "expired-2").Return(nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "expired-3").Return(nil).Once()

		handler := srv.setupHandler()
		reqList := httptest.NewRequest(http.MethodGet, "/api/apps?siteID=site-1", nil)
		reqList.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wList := httptest.NewRecorder()
		handler.ServeHTTP(wList, reqList)
		require.Equal(t, http.StatusOK, wList.Code)

		var listRes struct {
			Apps []types.UserApp `json:"apps"`
		}
		require.NoError(t, json.NewDecoder(wList.Body).Decode(&listRes))
		if assert.Len(t, listRes.Apps, 1) {
			assert.Equal(t, "site1-active", listRes.Apps[0].ID)
		}
		mockS.AssertExpectations(t)
	})
}

func TestHandleNewApp(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("AppsNotEnabled", func(t *testing.T) {
		srv := newTestAppsServer(&mockStorage{}, now)
		srv.appsEncryptionKey = ""
		srv.bypassAuth = true
		srv.singleSite = true
		handler := srv.setupHandler()

		reqNew := httptest.NewRequest(http.MethodPost, "/api/apps/new", strings.NewReader(`{"name":"Claude"}`))
		wNew := httptest.NewRecorder()
		handler.ServeHTTP(wNew, reqNew)
		assert.Equal(t, http.StatusNotImplemented, wNew.Code)
	})

	t.Run("CreatesAPIKeyAndToken", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)

		sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:            "user-1",
			Email:         "user1@example.com",
			SessionSecret: "sec-1",
			Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()
		mockS.On("AddUserApp", mock.Anything, "user-1", mock.MatchedBy(func(app types.UserApp) bool {
			return app.Name == "Claude MCP" && app.SiteID == "site-1" && app.Type == types.AppTypeAPIKey && app.ID != ""
		})).Return(nil).Once()

		handler := srv.setupHandler()
		body := `{"siteID":"site-1","name":"  Claude MCP  "}`
		reqNew := httptest.NewRequest(http.MethodPost, "/api/apps/new", strings.NewReader(body))
		reqNew.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wNew := httptest.NewRecorder()
		handler.ServeHTTP(wNew, reqNew)
		require.Equal(t, http.StatusOK, wNew.Code)

		var createRes struct {
			App   types.UserApp `json:"app"`
			Token string        `json:"token"`
		}
		require.NoError(t, json.NewDecoder(wNew.Body).Decode(&createRes))
		assert.Equal(t, "Claude MCP", createRes.App.Name)
		assert.Equal(t, types.AppTypeAPIKey, createRes.App.Type)
		assert.Equal(t, "site-1", createRes.App.SiteID)
		assert.True(t, strings.HasPrefix(createRes.Token, appTokenPrefix))

		tokenData, err := srv.verifyAppToken(createRes.Token)
		require.NoError(t, err)
		assert.Equal(t, "user-1", tokenData.UserID)
		assert.Equal(t, "site-1", tokenData.SiteID)
		assert.Equal(t, createRes.App.ID, tokenData.AppID)
		mockS.AssertExpectations(t)
	})
}

func TestHandleRevokeApp(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("RevokesExistingApp", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)

		sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:            "user-1",
			Email:         "user1@example.com",
			SessionSecret: "sec-1",
			Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "app-1").Return(nil).Once()

		handler := srv.setupHandler()
		revokeBody, err := json.Marshal(map[string]string{
			"siteID": "site-1",
			"id":     "app-1",
		})
		require.NoError(t, err)
		reqRevoke := httptest.NewRequest(http.MethodPost, "/api/apps/revoke", bytes.NewReader(revokeBody))
		reqRevoke.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wRevoke := httptest.NewRecorder()
		handler.ServeHTTP(wRevoke, reqRevoke)
		assert.Equal(t, http.StatusOK, wRevoke.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("AppNotFound", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)

		sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:            "user-1",
			Email:         "user1@example.com",
			SessionSecret: "sec-1",
			Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "missing-id").Return(storage.ErrAppNotFound).Once()

		handler := srv.setupHandler()
		reqRevokeMissing := httptest.NewRequest(http.MethodPost, "/api/apps/revoke", strings.NewReader(`{"siteID":"site-1","id":"missing-id"}`))
		reqRevokeMissing.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wRevokeMissing := httptest.NewRecorder()
		handler.ServeHTTP(wRevokeMissing, reqRevokeMissing)
		assert.Equal(t, http.StatusNotFound, wRevokeMissing.Code)
		mockS.AssertExpectations(t)
	})
}

func TestVerifyAppToken(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	srv := newTestAppsServer(&mockStorage{}, now)

	t.Run("ValidRoundTrip", func(t *testing.T) {
		token, err := srv.createAppToken("user-1", "site-1", "app-1", types.AppTypeAPIKey)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(token, appTokenPrefix))

		data, err := srv.verifyAppToken(token)
		require.NoError(t, err)
		assert.Equal(t, "user-1", data.UserID)
		assert.Equal(t, "site-1", data.SiteID)
		assert.Equal(t, "app-1", data.AppID)
		assert.Equal(t, types.AppTypeAPIKey, data.Type)
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		_, err := srv.createAppToken("", "site-1", "app-1", types.AppTypeAPIKey)
		assert.ErrorContains(t, err, "userID, siteID, and appID are required")
	})

	t.Run("AppsKeyNotConfigured", func(t *testing.T) {
		unconfiguredSrv := newTestAppsServer(&mockStorage{}, now)
		unconfiguredSrv.appsEncryptionKey = ""
		_, err := unconfiguredSrv.createAppToken("user-1", "site-1", "app-1", types.AppTypeAPIKey)
		assert.ErrorContains(t, err, "apps encryption key not configured")

		_, err = unconfiguredSrv.verifyAppToken("rr_something")
		assert.ErrorContains(t, err, "apps encryption key not configured")
	})

	t.Run("InvalidPrefixOrMalformedToken", func(t *testing.T) {
		_, err := srv.verifyAppToken("rrc_wrongprefix")
		assert.ErrorContains(t, err, "invalid token prefix")

		_, err = srv.verifyAppToken("rr_not-base64!!!")
		assert.ErrorContains(t, err, "failed to decode token")

		_, err = srv.verifyAppToken("rr_dG9vc2hvcnQ")
		assert.ErrorContains(t, err, "too short")
	})

	t.Run("IncompleteDecryptedPayload", func(t *testing.T) {
		incompleteToken, err := srv.encryptAppPayload(appTokenPrefix, AppTokenData{
			UserID: "user-1",
		})
		require.NoError(t, err)
		_, err = srv.verifyAppToken(incompleteToken)
		assert.ErrorContains(t, err, "incomplete app token payload")
	})

	t.Run("InvalidAppTokenType", func(t *testing.T) {
		badTypeToken, err := srv.encryptAppPayload(appTokenPrefix, AppTokenData{
			UserID: "user-1",
			SiteID: "site-1",
			AppID:  "app-1",
			Type:   types.AppType(99),
		})
		require.NoError(t, err)
		_, err = srv.verifyAppToken(badTypeToken)
		assert.ErrorContains(t, err, "invalid app token type")
	})

	t.Run("RejectsCrossPrefixTokenSubstitution", func(t *testing.T) {
		oauthCode, err := srv.encryptAppPayload(oauthCodePrefix, OAuthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-1",
			Secret:    "sec-123",
			ClientID:  "rrc_123",
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)
		swapped := appTokenPrefix + strings.TrimPrefix(oauthCode, oauthCodePrefix)
		_, err = srv.verifyAppToken(swapped)
		assert.ErrorContains(t, err, "failed to decrypt token")
	})
}
