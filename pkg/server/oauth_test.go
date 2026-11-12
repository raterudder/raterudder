package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

type cimdRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cimdRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newMockCIMDHTTPClient(rt cimdRoundTripFunc) *http.Client {
	return &http.Client{
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("redirects are not allowed for client metadata documents")
		},
	}
}

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
		assert.Equal(t, true, meta["client_id_metadata_document_supported"])
	})
}

func TestValidateCIMDClientID(t *testing.T) {
	t.Run("ValidHTTPSMetadataURL", func(t *testing.T) {
		u, err := validateCIMDClientID("https://claude.ai/oauth/client-metadata.json")
		require.NoError(t, err)
		assert.Equal(t, "claude.ai", u.Hostname())
		assert.Equal(t, "/oauth/client-metadata.json", u.Path)
	})

	t.Run("RejectsNonHTTPS", func(t *testing.T) {
		_, err := validateCIMDClientID("http://claude.ai/oauth/client-metadata.json")
		assert.ErrorContains(t, err, "https")
	})

	t.Run("RejectsRootOrEmptyPath", func(t *testing.T) {
		_, errNoPath := validateCIMDClientID("https://claude.ai")
		assert.ErrorContains(t, errNoPath, "non-root path")

		_, errRootPath := validateCIMDClientID("https://claude.ai/")
		assert.ErrorContains(t, errRootPath, "non-root path")
	})

	t.Run("RejectsDotPathSegments", func(t *testing.T) {
		_, errSingleDot := validateCIMDClientID("https://claude.ai/./client.json")
		assert.ErrorContains(t, errSingleDot, "dot path segments")

		_, errDoubleDot := validateCIMDClientID("https://claude.ai/oauth/../client.json")
		assert.ErrorContains(t, errDoubleDot, "dot path segments")

		_, errEncodedDot := validateCIMDClientID("https://claude.ai/oauth/%2e%2e/client.json")
		assert.ErrorContains(t, errEncodedDot, "dot path segments")
	})

	t.Run("RejectsFragmentAndUserinfo", func(t *testing.T) {
		_, errFrag := validateCIMDClientID("https://claude.ai/client.json#frag")
		assert.ErrorContains(t, errFrag, "fragment")

		_, errUser := validateCIMDClientID("https://user:pass@claude.ai/client.json")
		assert.ErrorContains(t, errUser, "userinfo")
	})

	t.Run("RejectsIPAndLocalhost", func(t *testing.T) {
		_, errIPv4 := validateCIMDClientID("https://127.0.0.1/client.json")
		assert.ErrorContains(t, errIPv4, "IP address")

		_, errMetadataIP := validateCIMDClientID("https://169.254.169.254/latest/meta-data")
		assert.ErrorContains(t, errMetadataIP, "IP address")

		_, errIPv6 := validateCIMDClientID("https://[::1]/client.json")
		assert.ErrorContains(t, errIPv6, "IP address")

		_, errLocalhost := validateCIMDClientID("https://localhost/client.json")
		assert.ErrorContains(t, errLocalhost, "public domain")

		_, errSubLocalhost := validateCIMDClientID("https://app.localhost/client.json")
		assert.ErrorContains(t, errSubLocalhost, "public domain")

		_, errSingleLabel := validateCIMDClientID("https://metadata/client.json")
		assert.ErrorContains(t, errSingleLabel, "public domain")
	})

	t.Run("RejectsExplicitPort", func(t *testing.T) {
		_, errPort := validateCIMDClientID("https://claude.ai:8443/client.json")
		assert.ErrorContains(t, errPort, "port")

		_, errDefaultPort := validateCIMDClientID("https://claude.ai:443/client.json")
		assert.ErrorContains(t, errDefaultPort, "port")

		_, errEmptyColon := validateCIMDClientID("https://claude.ai:/client.json")
		assert.ErrorContains(t, errEmptyColon, "port")
	})
}

func TestCleanupExpiredCIMDCache(t *testing.T) {
	t.Run("EvictsExpiredEntriesViaCleanupStaleMapKeys", func(t *testing.T) {
		now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
		s := &Server{
			nowFunc: func() time.Time { return now },
		}
		s.cimdCache.Store("https://expired.example.com/client.json", cimdCacheEntry{
			client:    resolvedOAuthClient{Name: "Expired", Verified: true},
			expiresAt: now.Add(-time.Second),
		})
		s.cimdCache.Store("https://fresh.example.com/client.json", cimdCacheEntry{
			client:    resolvedOAuthClient{Name: "Fresh", Verified: true},
			expiresAt: now.Add(time.Minute),
		})

		s.cleanupStaleMapKeys()

		_, expiredOK := s.cimdCache.Load("https://expired.example.com/client.json")
		assert.False(t, expiredOK)
		_, freshOK := s.cimdCache.Load("https://fresh.example.com/client.json")
		assert.True(t, freshOK)
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

	clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
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
			ClientName   string `json:"clientName"`
			RedirectURI  string `json:"redirectURI"`
			ClientDomain string `json:"clientDomain"`
			Verified     bool   `json:"verified"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&info))
		assert.Equal(t, "Claude", info.ClientName)
		assert.Equal(t, "https://claude.ai/api/mcp/auth_callback", info.RedirectURI)
		assert.False(t, info.Verified)
		assert.Empty(t, info.ClientDomain)
	})

	t.Run("CIMDReturnsVerifiedDomainAndCaches", func(t *testing.T) {
		currentTime := now
		srv.nowFunc = func() time.Time { return currentTime }
		defer func() { srv.nowFunc = func() time.Time { return now } }()

		cimdURL := "https://claude.ai/oauth/client-metadata.json"
		fetchCount := 0
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			fetchCount++
			assert.Equal(t, cimdURL, r.URL.String())
			body := `{
				"client_id": "https://claude.ai/oauth/client-metadata.json",
				"client_name": "Claude",
				"redirect_uris": ["https://claude.ai/api/mcp/auth_callback", "claude://oauth/callback"],
				"token_endpoint_auth_method": "none"
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			}, nil
		})

		qCIMD := url.Values{
			"client_id":             {cimdURL},
			"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
			"response_type":         {"code"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
		}
		req1 := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+qCIMD.Encode(), nil)
		req1.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		require.Equal(t, http.StatusOK, w1.Code)
		assert.Equal(t, 1, fetchCount)

		var info struct {
			ClientName   string `json:"clientName"`
			RedirectURI  string `json:"redirectURI"`
			ClientDomain string `json:"clientDomain"`
			Verified     bool   `json:"verified"`
		}
		require.NoError(t, json.NewDecoder(w1.Body).Decode(&info))
		assert.Equal(t, "Claude", info.ClientName)
		assert.Equal(t, "https://claude.ai/api/mcp/auth_callback", info.RedirectURI)
		assert.True(t, info.Verified)
		assert.Equal(t, "claude.ai", info.ClientDomain)

		// Second call within TTL uses cache without making another HTTP fetch
		req2 := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+qCIMD.Encode(), nil)
		req2.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		require.Equal(t, http.StatusOK, w2.Code)
		assert.Equal(t, 1, fetchCount)

		// Advancing past cimdCacheTTL triggers a fresh fetch
		currentTime = now.Add(cimdCacheTTL + time.Second)
		req3 := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+qCIMD.Encode(), nil)
		req3.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		w3 := httptest.NewRecorder()
		handler.ServeHTTP(w3, req3)
		require.Equal(t, http.StatusOK, w3.Code)
		assert.Equal(t, 2, fetchCount)
	})

	t.Run("CIMDRejectsInvalidMetadata", func(t *testing.T) {
		makeQuery := func(cid string) string {
			return url.Values{
				"client_id":             {cid},
				"redirect_uri":          {"https://app.example.com/callback"},
				"response_type":         {"code"},
				"code_challenge":        {codeChallenge},
				"code_challenge_method": {"S256"},
			}.Encode()
		}

		// Mismatched client_id inside JSON document
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"client_id": "https://other.example.com/client.json",
					"redirect_uris": ["https://app.example.com/callback"]
				}`)),
				Header: http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		})
		reqMismatch := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+makeQuery("https://app.example.com/mismatch.json"), nil)
		reqMismatch.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wMismatch := httptest.NewRecorder()
		handler.ServeHTTP(wMismatch, reqMismatch)
		assert.Equal(t, http.StatusBadRequest, wMismatch.Code)

		// Non-"none" token_endpoint_auth_method
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"client_id": "https://app.example.com/secret.json",
					"redirect_uris": ["https://app.example.com/callback"],
					"token_endpoint_auth_method": "client_secret_post"
				}`)),
				Header: http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		})
		reqSecret := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+makeQuery("https://app.example.com/secret.json"), nil)
		reqSecret.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wSecret := httptest.NewRecorder()
		handler.ServeHTTP(wSecret, reqSecret)
		assert.Equal(t, http.StatusBadRequest, wSecret.Code)

		// Non-JSON Content-Type rejected
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"client_id": "https://app.example.com/plain.json",
					"redirect_uris": ["https://app.example.com/callback"]
				}`)),
				Header: http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
			}, nil
		})
		reqPlain := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+makeQuery("https://app.example.com/plain.json"), nil)
		reqPlain.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wPlain := httptest.NewRecorder()
		handler.ServeHTTP(wPlain, reqPlain)
		assert.Equal(t, http.StatusBadRequest, wPlain.Code)
		assert.Contains(t, wPlain.Body.String(), "application/json")

		// Redirect response (302) rejected without leaking status code
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Location", "http://169.254.169.254/latest/meta-data")
			return &http.Response{
				StatusCode: http.StatusFound,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     h,
				Request:    r,
			}, nil
		})
		reqRedirect := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+makeQuery("https://app.example.com/redirect.json"), nil)
		reqRedirect.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wRedirect := httptest.NewRecorder()
		handler.ServeHTTP(wRedirect, reqRedirect)
		assert.Equal(t, http.StatusBadRequest, wRedirect.Code)
		assert.NotContains(t, wRedirect.Body.String(), "302")

		// Oversized (>16 KB) metadata document rejected
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			huge := `{"client_id":"https://app.example.com/huge.json","redirect_uris":["https://app.example.com/callback"],"pad":"` + strings.Repeat("a", maxCIMDResponseBytes) + `"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(huge)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		})
		reqHuge := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+makeQuery("https://app.example.com/huge.json"), nil)
		reqHuge.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wHuge := httptest.NewRecorder()
		handler.ServeHTTP(wHuge, reqHuge)
		assert.Equal(t, http.StatusBadRequest, wHuge.Code)
	})

	t.Run("RejectsUnregisteredUserBeforeFetchingCIMD", func(t *testing.T) {
		unregToken, err := srv.createSessionToken("unreg-user", "unreg@example.com", "sec-u", time.Hour)
		require.NoError(t, err)
		mockS.On("GetUser", mock.Anything, "unreg-user").Return(types.User{}, storage.ErrUserNotFound).Once()

		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("unregistered user should not trigger outbound CIMD fetch: %s", r.URL.String())
			return nil, errors.New("unexpected fetch")
		})

		q := url.Values{
			"client_id":             {"https://claude.ai/oauth/unreg-check.json"},
			"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
			"response_type":         {"code"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+q.Encode(), nil)
		req.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: unregToken})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestHandleOAuthApprove(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	t.Run("CreatesPendingAppAndReturnsRedirectURL", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
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

	t.Run("ApprovesCIMDClientUsingCachedMetadata", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		cimdURL := "https://claude.ai/oauth/client-metadata.json"
		fetchCount := 0
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			fetchCount++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"client_id": "https://claude.ai/oauth/client-metadata.json",
					"redirect_uris": ["https://claude.ai/api/mcp/auth_callback"]
				}`)),
				Header: http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		})

		sessionToken, err := srv.createSessionToken("user-1", "user1@example.com", "sec-1", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:            "user-1",
			Email:         "user1@example.com",
			SessionSecret: "sec-1",
			Sites:         []types.UserSite{{ID: "site-1", Name: "Home"}},
		}, nil).Twice()
		mockS.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Twice()

		codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-valid-pkce-verifier"
		codeChallenge := computePKCES256Challenge(codeVerifier)

		// 1. Fetch client info (populates cimdCache; defaults client_name to domain when omitted)
		qClient := url.Values{
			"client_id":             {cimdURL},
			"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
			"response_type":         {"code"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
		}
		reqInfo := httptest.NewRequest(http.MethodGet, "/api/oauth/client?"+qClient.Encode(), nil)
		reqInfo.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wInfo := httptest.NewRecorder()
		handler.ServeHTTP(wInfo, reqInfo)
		require.Equal(t, http.StatusOK, wInfo.Code)
		assert.Equal(t, 1, fetchCount)

		// 2. Approve uses cached metadata without fetching again
		mockS.On("AddUserApp", mock.Anything, "user-1", mock.MatchedBy(func(app types.UserApp) bool {
			return app.Name == "claude.ai" &&
				app.ClientDomain == "claude.ai" &&
				app.Type == types.AppTypeOAuth &&
				app.SiteID == "site-1" &&
				app.PendingCodeHash != "" &&
				app.CodeChallenge == codeChallenge &&
				app.RedirectURI == "https://claude.ai/api/mcp/auth_callback"
		})).Return(nil).Once()

		approvePayload, err := json.Marshal(map[string]string{
			"siteID":              "site-1",
			"clientID":            cimdURL,
			"redirectURI":         "https://claude.ai/api/mcp/auth_callback",
			"responseType":        "code",
			"codeChallenge":       codeChallenge,
			"codeChallengeMethod": "S256",
			"state":               "cimd-state",
		})
		require.NoError(t, err)

		reqApprove := httptest.NewRequest(http.MethodPost, "/api/oauth/approve", bytes.NewReader(approvePayload))
		reqApprove.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wApprove := httptest.NewRecorder()
		handler.ServeHTTP(wApprove, reqApprove)
		require.Equal(t, http.StatusOK, wApprove.Code)
		assert.Equal(t, 1, fetchCount)
		mockS.AssertExpectations(t)
	})

	t.Run("RejectsMultiSiteAdminWithoutDirectSitePermission", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		srv.adminEmails = []string{"admin@example.com"}
		handler := srv.setupHandler()

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)

		sessionToken, err := srv.createSessionToken("admin-1", "admin@example.com", "sec-admin", time.Hour)
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "admin-1").Return(types.User{
			ID:            "admin-1",
			Email:         "admin@example.com",
			SessionSecret: "sec-admin",
			Sites:         []types.UserSite{{ID: "admin-site", Name: "Admin Home"}},
		}, nil).Once()
		mockS.On("GetSite", mock.Anything, "customer-site").Return(types.Site{
			ID:          "customer-site",
			Permissions: []types.SitePermissions{{UserID: "customer-1"}},
		}, nil).Once()

		codeChallenge := computePKCES256Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-valid-pkce-verifier")
		approvePayload, err := json.Marshal(map[string]string{
			"siteID":              "customer-site",
			"clientID":            clientID,
			"redirectURI":         "https://claude.ai/api/mcp/auth_callback",
			"responseType":        "code",
			"codeChallenge":       codeChallenge,
			"codeChallengeMethod": "S256",
		})
		require.NoError(t, err)

		reqApprove := httptest.NewRequest(http.MethodPost, "/api/oauth/approve", bytes.NewReader(approvePayload))
		reqApprove.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionToken})
		wApprove := httptest.NewRecorder()
		handler.ServeHTTP(wApprove, reqApprove)
		assert.Equal(t, http.StatusForbidden, wApprove.Code)
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

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)

		rawSecret := "secret-12345"
		authCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-oauth-1",
			Secret:    rawSecret,
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		pendingApp := types.UserApp{
			ID:               "app-oauth-1",
			Name:             "Claude",
			Type:             types.AppTypeOAuth,
			SiteID:           "site-1",
			TSCreated:        now,
			PendingCodeHash:  computePendingCodeHash(rawSecret, clientID),
			PendingExpiresAt: now.Add(oauthCodeDuration),
			CodeChallenge:    codeChallenge,
			RedirectURI:      "https://claude.ai/api/mcp/auth_callback",
		}

		storedUser := types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1", Name: "Home"}},
			Apps:  []types.UserApp{pendingApp},
		}

		expectedHash := pendingApp.PendingCodeHash
		mockS.On("GetUser", mock.Anything, "user-1").Return(storedUser, nil).Once()
		mockS.On("ReplaceUserApp", mock.Anything, "user-1", mock.MatchedBy(func(app types.UserApp) bool {
			return app.ID == "app-oauth-1" &&
				app.Name == "Claude" &&
				app.Type == types.AppTypeOAuth &&
				app.SiteID == "site-1" &&
				app.TokenHash != "" &&
				app.PendingCodeHash == "" &&
				app.PendingExpiresAt.IsZero() &&
				app.CodeChallenge == "" &&
				app.RedirectURI == ""
		}), storage.ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &expectedHash,
		}).Run(func(args mock.Arguments) {
			storedUser.Apps[0] = args.Get(2).(types.UserApp)
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

		tokenData, err := srv.verifyAppToken(tokenRes.AccessToken)
		require.NoError(t, err)
		assert.Equal(t, storedUser.Apps[0].TokenHash, sha256Hex(tokenData.Secret))

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

		authCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-broken-1",
			Secret:    "secret-abc",
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		// Missing client_id triggers early validation failure and removes the unexpired pending app
		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID: "user-1",
			Apps: []types.UserApp{
				{
					ID:               "app-broken-1",
					Name:             "Claude",
					Type:             types.AppTypeOAuth,
					SiteID:           "site-1",
					PendingCodeHash:  "pending-hash",
					PendingExpiresAt: now.Add(oauthCodeDuration),
				},
			},
		}, nil).Once()
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

		clientID, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Add(-24 * time.Hour).Unix(),
		})
		require.NoError(t, err)

		expiredCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-active-1",
			Secret:    "old-secret",
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

		clientID1, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)
		clientID2, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
			Name:         "Claude",
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			CreatedAt:    now.Unix() + 1,
		})
		require.NoError(t, err)

		rawSecret := "secret-12345"
		authCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-oauth-1",
			Secret:    rawSecret,
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
					PendingCodeHash:  computePendingCodeHash(rawSecret, clientID1),
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

	t.Run("ExchangesCIMDCodeWithoutOutboundFetchAndRevokesOnReplay", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		// Ensure /oauth/token never triggers an outbound HTTP request even for CIMD client_id URLs
		srv.cimdHTTPClient = newMockCIMDHTTPClient(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected outbound HTTP request on /oauth/token: %s", r.URL.String())
			return nil, errors.New("unexpected request")
		})
		handler := srv.setupHandler()

		cimdURL := "https://claude.ai/oauth/client-metadata.json"
		rawSecret := "secret-cimd-123"
		authCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-cimd-1",
			Secret:    rawSecret,
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		pendingApp := types.UserApp{
			ID:               "app-cimd-1",
			Name:             "Claude",
			Type:             types.AppTypeOAuth,
			SiteID:           "site-1",
			TSCreated:        now,
			PendingCodeHash:  computePendingCodeHash(rawSecret, cimdURL),
			PendingExpiresAt: now.Add(oauthCodeDuration),
			CodeChallenge:    codeChallenge,
			RedirectURI:      "https://claude.ai/api/mcp/auth_callback",
		}

		storedUser := types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1", Name: "Home"}},
			Apps:  []types.UserApp{pendingApp},
		}

		expectedHash := pendingApp.PendingCodeHash
		mockS.On("GetUser", mock.Anything, "user-1").Return(storedUser, nil).Once()
		mockS.On("ReplaceUserApp", mock.Anything, "user-1", mock.MatchedBy(func(app types.UserApp) bool {
			return app.ID == "app-cimd-1" &&
				app.Name == "Claude" &&
				app.TokenHash != "" &&
				app.PendingCodeHash == ""
		}), storage.ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &expectedHash,
		}).Run(func(args mock.Arguments) {
			storedUser.Apps[0] = args.Get(2).(types.UserApp)
		}).Return(nil).Once()

		tokenForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {authCode},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://claude.ai/api/mcp/auth_callback"},
			"client_id":     {cimdURL},
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

		// Replay within validity window revokes the redeemed app without making an outbound fetch
		mockS.On("GetUser", mock.Anything, "user-1").Return(storedUser, nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "app-cimd-1").Return(nil).Once()

		reqReplay := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
		reqReplay.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wReplay := httptest.NewRecorder()
		handler.ServeHTTP(wReplay, reqReplay)
		assert.Equal(t, http.StatusBadRequest, wReplay.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("EarlyValidationFailureDoesNotDeleteRedeemedApp", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		authCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-active-1",
			Secret:    "secret-redeemed",
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		// App is already redeemed (PendingCodeHash == ""), so malformed grant_type must NOT delete it
		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID: "user-1",
			Apps: []types.UserApp{
				{
					ID:        "app-active-1",
					Name:      "Claude",
					Type:      types.AppTypeOAuth,
					SiteID:    "site-1",
					TokenHash: "active-token-hash",
				},
			},
		}, nil).Once()

		badForm := url.Values{
			"grant_type": {"invalid"},
			"code":       {authCode},
		}
		reqBad := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(badForm.Encode()))
		reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wBad := httptest.NewRecorder()
		handler.ServeHTTP(wBad, reqBad)
		assert.Equal(t, http.StatusBadRequest, wBad.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("RevokesRedeemedAppOnReplayWithDifferentDCRClientID", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		attackerClientID, err := srv.encryptAppPayload(oauthClientIDPrefix, oauthClientData{
			Name:         "Attacker",
			RedirectURIs: []string{"https://attacker.example.com/cb"},
			CreatedAt:    now.Unix(),
		})
		require.NoError(t, err)

		stolenCode, err := srv.encryptAppPayload(oauthCodePrefix, oauthCodeData{
			UserID:    "user-1",
			SiteID:    "site-1",
			AppID:     "app-redeemed-1",
			Secret:    "secret-stolen",
			ExpiresAt: now.Add(oauthCodeDuration).Unix(),
		})
		require.NoError(t, err)

		mockS.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID: "user-1",
			Apps: []types.UserApp{
				{
					ID:        "app-redeemed-1",
					Name:      "Claude",
					Type:      types.AppTypeOAuth,
					SiteID:    "site-1",
					TokenHash: "active-token-hash",
				},
			},
		}, nil).Once()
		mockS.On("RemoveUserApp", mock.Anything, "user-1", "site-1", "app-redeemed-1").Return(nil).Once()

		replayForm := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {stolenCode},
			"code_verifier": {codeVerifier},
			"redirect_uri":  {"https://attacker.example.com/cb"},
			"client_id":     {attackerClientID},
		}
		reqReplay := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(replayForm.Encode()))
		reqReplay.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wReplay := httptest.NewRecorder()
		handler.ServeHTTP(wReplay, reqReplay)
		assert.Equal(t, http.StatusBadRequest, wReplay.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("RejectsDuplicateAndQueryFormParams", func(t *testing.T) {
		mockS := &mockStorage{}
		srv := newTestAppsServer(mockS, now)
		handler := srv.setupHandler()

		// Duplicate parameter in body rejected
		dupBody := "grant_type=authorization_code&client_id=https://a.example.com/c.json&client_id=https://b.example.com/c.json&code=abc&code_verifier=" + codeVerifier + "&redirect_uri=https://a.example.com/cb"
		reqDup := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(dupBody))
		reqDup.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wDup := httptest.NewRecorder()
		handler.ServeHTTP(wDup, reqDup)
		assert.Equal(t, http.StatusBadRequest, wDup.Code)
		assert.Contains(t, wDup.Body.String(), "duplicate")

		// Query string parameters ignored when body omits grant_type
		reqQuery := httptest.NewRequest(http.MethodPost, "/oauth/token?grant_type=authorization_code", strings.NewReader("code=abc"))
		reqQuery.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wQuery := httptest.NewRecorder()
		handler.ServeHTTP(wQuery, reqQuery)
		assert.Equal(t, http.StatusBadRequest, wQuery.Code)
		assert.Contains(t, wQuery.Body.String(), "unsupported_grant_type")
	})
}
