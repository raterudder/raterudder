package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleOAuthServerMetadata(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("AppsNotEnabled", func(t *testing.T) {
		srv := newTestAppsServer(&mockStorage{}, now)
		srv.appsEncryptionKey = ""
		handler := srv.setupHandler()

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})

	t.Run("DynamicOriginWithForwardedProto", func(t *testing.T) {
		srv := newTestAppsServer(&mockStorage{}, now)
		handler := srv.setupHandler()

		req := httptest.NewRequest(http.MethodGet, "http://staging.raterudder.com/.well-known/oauth-authorization-server", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var meta map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&meta))
		assert.Equal(t, "https://staging.raterudder.com", meta["issuer"])
		assert.Equal(t, "https://staging.raterudder.com/oauth/authorize", meta["authorization_endpoint"])
		assert.Equal(t, "https://staging.raterudder.com/oauth/token", meta["token_endpoint"])
		assert.Equal(t, "https://staging.raterudder.com/oauth/register", meta["registration_endpoint"])
	})
}

func TestHandleOAuthProtectedResource(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("AppsNotEnabled", func(t *testing.T) {
		srv := newTestAppsServer(&mockStorage{}, now)
		srv.appsEncryptionKey = ""
		handler := srv.setupHandler()

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})

	t.Run("DynamicResourceMetadata", func(t *testing.T) {
		srv := newTestAppsServer(&mockStorage{}, now)
		handler := srv.setupHandler()

		reqRes := httptest.NewRequest(http.MethodGet, "https://energy.example.org/.well-known/oauth-protected-resource", nil)
		wRes := httptest.NewRecorder()
		handler.ServeHTTP(wRes, reqRes)
		require.Equal(t, http.StatusOK, wRes.Code)

		var resMeta struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
		}
		require.NoError(t, json.NewDecoder(wRes.Body).Decode(&resMeta))
		assert.Equal(t, "https://energy.example.org/api/mcp", resMeta.Resource)
		assert.Equal(t, []string{"https://energy.example.org"}, resMeta.AuthorizationServers)
	})
}

func TestHandleOAuthRegister(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	srv := newTestAppsServer(&mockStorage{}, now)
	handler := srv.setupHandler()

	t.Run("RejectsNonLocalhostHTTP", func(t *testing.T) {
		reqHTTP := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{
			"client_name": "Evil Client",
			"redirect_uris": ["http://evil.example.com/callback"]
		}`))
		wHTTP := httptest.NewRecorder()
		handler.ServeHTTP(wHTTP, reqHTTP)
		assert.Equal(t, http.StatusBadRequest, wHTTP.Code)
	})

	t.Run("RejectsFragmentAndWildcard", func(t *testing.T) {
		reqFrag := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{
			"client_name": "Fragment Client",
			"redirect_uris": ["https://claude.ai/api/mcp/auth_callback#frag"]
		}`))
		wFrag := httptest.NewRecorder()
		handler.ServeHTTP(wFrag, reqFrag)
		assert.Equal(t, http.StatusBadRequest, wFrag.Code)

		reqWild := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{
			"client_name": "Wildcard Client",
			"redirect_uris": ["https://*.example.com/callback"]
		}`))
		wWild := httptest.NewRecorder()
		handler.ServeHTTP(wWild, reqWild)
		assert.Equal(t, http.StatusBadRequest, wWild.Code)
	})

	t.Run("RegistersValidClient", func(t *testing.T) {
		reqValid := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{
			"client_name": "Claude",
			"redirect_uris": ["https://claude.ai/api/mcp/auth_callback", "http://127.0.0.1:4567/callback"]
		}`))
		wValid := httptest.NewRecorder()
		handler.ServeHTTP(wValid, reqValid)
		require.Equal(t, http.StatusCreated, wValid.Code)

		var regRes struct {
			ClientID     string   `json:"client_id"`
			ClientName   string   `json:"client_name"`
			RedirectURIs []string `json:"redirect_uris"`
		}
		require.NoError(t, json.NewDecoder(wValid.Body).Decode(&regRes))
		assert.True(t, strings.HasPrefix(regRes.ClientID, oauthClientIDPrefix))
		assert.Equal(t, "Claude", regRes.ClientName)
		assert.Len(t, regRes.RedirectURIs, 2)
	})
}

func TestHandleOAuthClientInfo(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mockS := &mockStorage{}
	srv := newTestAppsServer(mockS, now)
	handler := srv.setupHandler()

	clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
		Name:         "Claude",
		RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
		CreatedAt:    now.Unix(),
	})
	require.NoError(t, err)

	sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
	require.NoError(t, err)

	mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
		ID:            "user-1",
		Email:         "user1@example.com",
		SessionSecret: "sec-1",
		Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
	}, nil)
	mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
		ID:          "site-1",
		Permissions: []types.SitePermissions{{UserID: "user-1"}},
	}, nil)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-valid-pkce-verifier"
	codeChallenge := computePKCES256Challenge(codeVerifier)

	t.Run("RejectsMismatchedRedirectURI", func(t *testing.T) {
		qMismatch := url.Values{
			"client_id":             {clientID},
			"redirect_uri":          {"https://other.example.com/callback"},
			"response_type":         {"code"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+qMismatch.Encode(), nil)
		req.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ReturnsClientInfo", func(t *testing.T) {
		qValid := url.Values{
			"client_id":             {clientID},
			"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
			"response_type":         {"code"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+qValid.Encode(), nil)
		req.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var info struct {
			ClientName  string `json:"clientName"`
			RedirectURI string `json:"redirectURI"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&info))
		assert.Equal(t, "Claude", info.ClientName)
		assert.Equal(t, "https://claude.ai/api/mcp/auth_callback", info.RedirectURI)
	})
}

func TestHandleOAuthApprove(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("CreatesPendingAppAndReturnsRedirectURL", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)

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

		codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-valid-pkce-verifier"
		codeChallenge := computePKCES256Challenge(codeVerifier)

		mockS.On("AddUserApp", mock.Anything, "user-1", mock.MatchedBy(func(app types.UserApp) bool {
			return app.Name == "Claude" &&
				app.Type == types.AppTypeOAuth &&
				app.SiteID == "site-1" &&
				app.PendingCodeHash != "" &&
				app.CodeChallenge == codeChallenge &&
				app.RedirectURI == "https://claude.ai/api/mcp/auth_callback"
		})).Return(nil).Once()

		approvePayload, err := json.Marshal(map[string]string{
			"siteID":              "site-1",
			"clientID":            clientID,
			"redirectURI":         "https://claude.ai/api/mcp/auth_callback",
			"responseType":        "code",
			"codeChallenge":       codeChallenge,
			"codeChallengeMethod": "S256",
			"state":               "xyz-state",
		})
		require.NoError(t, err)

		reqApprove := httptest.NewRequest(http.MethodPost, "/api/oauth/approve", bytes.NewReader(approvePayload))
		reqApprove.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wApprove := httptest.NewRecorder()
		handler.ServeHTTP(wApprove, reqApprove)
		require.Equal(t, http.StatusOK, wApprove.Code)

		var approveRes struct {
			RedirectURL string `json:"redirectURL"`
		}
		require.NoError(t, json.NewDecoder(wApprove.Body).Decode(&approveRes))
		parsedRedirect, err := url.Parse(approveRes.RedirectURL)
		require.NoError(t, err)
		assert.Equal(t, "xyz-state", parsedRedirect.Query().Get("state"))
		assert.True(t, strings.HasPrefix(parsedRedirect.Query().Get("code"), oauthCodePrefix))
		mockS.AssertExpectations(t)
	})
}

func TestHandleOAuthToken(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-valid-pkce-verifier"
	codeChallenge := computePKCES256Challenge(codeVerifier)

	t.Run("ExchangesCodeAndRevokesOnReplay", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)

		rawSecret := "secret-12345"
		authCode, err := srv.encryptAppPayload(oauthCodePrefix, OAuthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-oauth-1",
			Secret:    rawSecret,
			ClientID:  clientID,
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		pendingApp := types.UserApp{
			ID:               "app-oauth-1",
			Name:             "Claude",
			Type:             types.AppTypeOAuth,
			SiteID:           "site-1",
			TSCreated:        now,
			PendingCodeHash:  sha256Hex(rawSecret),
			PendingExpiresAt: now.Add(oauthCodeDuration),
			CodeChallenge:    codeChallenge,
			RedirectURI:      "https://claude.ai/api/mcp/auth_callback",
		}
		expectedRedeemedApp := types.UserApp{
			ID:        "app-oauth-1",
			Name:      "Claude",
			Type:      types.AppTypeOAuth,
			SiteID:    "site-1",
			TSCreated: now,
		}

		storedUser := types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1", Name: "Home"}},
			Apps:  []types.UserApp{pendingApp},
		}

		expectedHash := pendingApp.PendingCodeHash
		mockS.On("GetUser", mock.Anything, "user-1").Return(storedUser, nil).Once()
		mockS.On("ReplaceUserApp", mock.Anything, "user-1", expectedRedeemedApp, storage.ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &expectedHash,
		}).Run(func(_ mock.Arguments) {
			storedUser.Apps[0] = expectedRedeemedApp
		}).Return(nil).Once()

		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {authCode},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
			"client_id":     {clientID},
		}
		reqToken := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
		reqToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wToken := httptest.NewRecorder()
		handler.ServeHTTP(wToken, reqToken)
		require.Equal(t, http.StatusOK, wToken.Code)

		var tokenRes struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
		}
		require.NoError(t, json.NewDecoder(wToken.Body).Decode(&tokenRes))
		assert.Equal(t, "Bearer", tokenRes.TokenType)
		assert.True(t, strings.HasPrefix(tokenRes.AccessToken, appTokenPrefix))

		// Replaying the same authorization code within its validity window fails and revokes the app via RemoveUserApp
		mockS.On("GetUser", mock.Anything, "user-1").Return(storedUser, nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "app-oauth-1").Return(nil).Once()

		reqReplay := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
		reqReplay.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wReplay := httptest.NewRecorder()
		handler.ServeHTTP(wReplay, reqReplay)
		assert.Equal(t, http.StatusBadRequest, wReplay.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("CleansUpAppOnValidationFailure", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		authCode, err := srv.encryptAppPayload(oauthCodePrefix, OAuthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-broken-1",
			Secret:    "secret-abc",
			ClientID:  "rrc_123",
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		// Missing client_id triggers early validation failure and removes the unexpired pending app
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "app-broken-1").Return(nil).Once()

		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {authCode},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
		}
		reqToken := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
		reqToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wToken := httptest.NewRecorder()
		handler.ServeHTTP(wToken, reqToken)
		assert.Equal(t, http.StatusBadRequest, wToken.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("ExpiredHistoricalCodeDoesNotRevokeActiveApp", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Add(-24 * time.Hour).Unix(),
		})
		require.NoError(t, err)

		expiredCode, err := srv.encryptAppPayload(oauthCodePrefix, OAuthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-active-1",
			Secret:    "old-secret",
			ClientID:  clientID,
			ExpiresAt: now.Add(-time.Hour).Unix(),
		})
		require.NoError(t, err)

		// Early validation failure with an expired code must NOT call RemoveUserApp
		badForm := url.Values{
			"grant_type": {"invalid"},
			"code":       {expiredCode},
		}
		reqBad := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(badForm.Encode()))
		reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wBad := httptest.NewRecorder()
		handler.ServeHTTP(wBad, reqBad)
		assert.Equal(t, http.StatusBadRequest, wBad.Code)

		// Replay of an expired historical code against an already-redeemed app must NOT call RemoveUserApp
		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID: "user-1",
			Apps: []types.UserApp{
				{
					ID:        "app-active-1",
					Name:      "Claude",
					Type:      types.AppTypeOAuth,
					SiteID:    "site-1",
					TSCreated: now.Add(-time.Hour),
				},
			},
		}, nil).Once()

		validForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {expiredCode},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
			"client_id":     {clientID},
		}
		reqReplay := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(validForm.Encode()))
		reqReplay.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wReplay := httptest.NewRecorder()
		handler.ServeHTTP(wReplay, reqReplay)
		assert.Equal(t, http.StatusBadRequest, wReplay.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("RejectsDifferentClientIDWithSameName", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		clientID1, err := srv.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)
		clientID2, err := srv.encryptAppPayload(oauthClientIDPrefix, OAuthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix() + 1,
		})
		require.NoError(t, err)

		rawSecret := "secret-12345"
		authCode, err := srv.encryptAppPayload(oauthCodePrefix, OAuthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-oauth-1",
			Secret:    rawSecret,
			ClientID:  clientID1,
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID: "user-1",
			Apps: []types.UserApp{
				{
					ID:               "app-oauth-1",
					Name:             "Claude",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-1",
					TSCreated:        now,
					PendingCodeHash:  sha256Hex(rawSecret),
					PendingExpiresAt: now.Add(oauthCodeDuration),
					CodeChallenge:    codeChallenge,
					RedirectURI:      "https://claude.ai/api/mcp/auth_callback",
				},
			},
		}, nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "app-oauth-1").Return(nil).Once()

		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {authCode},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
			"client_id":     {clientID2},
		}
		reqToken := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
		reqToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wToken := httptest.NewRecorder()
		handler.ServeHTTP(wToken, reqToken)
		assert.Equal(t, http.StatusBadRequest, wToken.Code)
		mockS.AssertExpectations(t)
	})
}
