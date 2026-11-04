package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestAdminListSites(t *testing.T) {
	mockStorage := &mockStorage{}
	mockStorage.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
	sites := []types.Site{
		{ID: "site1"},
		{ID: "site2"},
	}
	mockStorage.On("ListSites", mock.Anything).Return(sites, nil)
	mockStorage.On("GetLatestAction", mock.Anything, "site1").Return(&types.Action{Description: "test1 action"}, nil)
	mockStorage.On("GetLatestAction", mock.Anything, "site2").Return((*types.Action)(nil), nil)
	mockStorage.On("GetAdminSettings", mock.Anything).Return(types.AdminSettings{
		Aliases: map[string]string{
			"site1": "my-cool-site-alias",
		},
	}, nil)
	// Setup OIDC for tests
	srvUrl, priv := setupOIDCTest(t)
	defer srvUrl.Close()
	provider, err := oidc.NewProvider(context.Background(), srvUrl.URL)
	require.NoError(t, err)

	validAdminToken := generateTestToken(t, srvUrl.URL, priv, "admin@example.com", "admin1")
	validUserToken := generateTestToken(t, srvUrl.URL, priv, "user@example.com", "user1")

	srv := &Server{
		storage:     mockStorage,
		adminEmails: []string{"admin@example.com"},
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
	}
	handler := srv.setupHandler()

	t.Run("Unauthorized - Not Admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/sites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validUserToken})

		mockStorage.On("GetUser", mock.Anything, "google:user1").Return(types.User{
			ID:    "google:user1",
			Email: "user@example.com",
		}, nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusForbidden, rr.Code)

		var resp map[string]string
		err := json.NewDecoder(rr.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "forbidden", resp["error"])
	})

	t.Run("Authorized - Admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/sites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validAdminToken})

		mockStorage.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var sites []AdminSite
		err := json.NewDecoder(rr.Body).Decode(&sites)
		require.NoError(t, err)

		if assert.Len(t, sites, 2) {
			siteIDs := []string{sites[0].ID, sites[1].ID}
			assert.Contains(t, siteIDs, "site1")
			assert.Contains(t, siteIDs, "site2")

			for _, s := range sites {
				if s.ID == "site1" {
					require.NotNil(t, s.LastAction)
					assert.Equal(t, "test1 action", s.LastAction.Description)
					assert.Equal(t, "my-cool-site-alias", s.Alias)
				}
				if s.ID == "site2" {
					assert.Nil(t, s.LastAction)
					assert.Empty(t, s.Alias)
				}
			}
		}
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/sites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validAdminToken})

		mockStorage.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()

		rr := httptest.NewRecorder()
		authHandler := srv.authMiddleware(http.HandlerFunc(srv.handleListSites))
		authHandler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var adminSites []AdminSite
		err := json.NewDecoder(rr.Body).Decode(&adminSites)
		require.NoError(t, err)

		if assert.Len(t, adminSites, 2) {
			siteIDs := []string{adminSites[0].ID, adminSites[1].ID}
			assert.Contains(t, siteIDs, "site1")
			assert.Contains(t, siteIDs, "site2")
		}
		mockStorage.AssertExpectations(t)
	})
}

func TestAdminSetSiteAlias(t *testing.T) {
	mockStorage := &mockStorage{}
	srvUrl, priv := setupOIDCTest(t)
	defer srvUrl.Close()
	provider, err := oidc.NewProvider(context.Background(), srvUrl.URL)
	require.NoError(t, err)

	validAdminToken := generateTestToken(t, srvUrl.URL, priv, "admin@example.com", "admin1")
	validUserToken := generateTestToken(t, srvUrl.URL, priv, "user@example.com", "user1")

	srv := &Server{
		storage:     mockStorage,
		adminEmails: []string{"admin@example.com"},
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
	}
	handler := srv.setupHandler()

	t.Run("Unauthorized - Not Admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/site/alias", strings.NewReader(`{"siteID":"site1","alias":"my-alias"}`))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validUserToken})

		mockStorage.On("GetUser", mock.Anything, "google:user1").Return(types.User{
			ID:    "google:user1",
			Email: "user@example.com",
		}, nil).Once()
		mockStorage.On("GetSite", mock.Anything, "site1").Return(types.Site{ID: "site1"}, nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusForbidden, rr.Code)
		mockStorage.AssertExpectations(t)
	})

	t.Run("Authorized - Admin Update Alias", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/site/alias", strings.NewReader(`{"siteID":"site1","alias":"my-alias"}`))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validAdminToken})

		mockStorage.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()
		mockStorage.On("GetSite", mock.Anything, "site1").Return(types.Site{ID: "site1"}, nil).Once()
		mockStorage.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()

		mockStorage.On("GetAdminSettings", mock.Anything).Return(types.AdminSettings{
			Aliases: map[string]string{
				"site2": "another-alias",
			},
		}, nil).Once()

		mockStorage.On("UpdateAdminSettings", mock.Anything, types.AdminSettings{
			Aliases: map[string]string{
				"site1": "my-alias",
				"site2": "another-alias",
			},
		}).Return(nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		mockStorage.AssertExpectations(t)
	})

	t.Run("Authorized - Admin Delete Alias", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/site/alias", strings.NewReader(`{"siteID":"site1","alias":""}`))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validAdminToken})

		mockStorage.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()
		mockStorage.On("GetSite", mock.Anything, "site1").Return(types.Site{ID: "site1"}, nil).Once()

		mockStorage.On("GetAdminSettings", mock.Anything).Return(types.AdminSettings{
			Aliases: map[string]string{
				"site1": "my-alias",
				"site2": "another-alias",
			},
		}, nil).Once()

		mockStorage.On("UpdateAdminSettings", mock.Anything, types.AdminSettings{
			Aliases: map[string]string{
				"site2": "another-alias",
			},
		}).Return(nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		mockStorage.AssertExpectations(t)
	})
}

func TestAdminListUserSites(t *testing.T) {
	mockStorage := &mockStorage{}
	mockStorage.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, time.Time{}, nil).Once()
	srvUrl, priv := setupOIDCTest(t)
	defer srvUrl.Close()
	provider, err := oidc.NewProvider(context.Background(), srvUrl.URL)
	require.NoError(t, err)

	validAdminToken := generateTestToken(t, srvUrl.URL, priv, "admin@example.com", "admin1")
	validUserToken := generateTestToken(t, srvUrl.URL, priv, "user@example.com", "user1")

	srv := &Server{
		storage:     mockStorage,
		adminEmails: []string{"admin@example.com"},
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 5),
		sensitiveBurst:     5,
	}
	handler := srv.setupHandler()

	t.Run("Unauthorized - Not Admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/userSites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validUserToken})

		mockStorage.On("GetUser", mock.Anything, "google:user1").Return(types.User{
			ID:    "google:user1",
			Email: "user@example.com",
		}, nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Logf("Response body on unauthorized failure: %s", rr.Body.String())
		}
		assert.Equal(t, http.StatusForbidden, rr.Code)
		mockStorage.AssertExpectations(t)
	})

	t.Run("Authorized - Admin List User Sites", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/userSites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validAdminToken})

		mockStorage.On("GetUser", mock.Anything, "google:admin1").Return(types.User{
			ID:    "google:admin1",
			Email: "admin@example.com",
		}, nil).Once()

		mockStorage.On("ListUsers", mock.Anything).Return([]types.User{
			{
				ID: "user1",
				Sites: []types.UserSite{
					{ID: "site1", Name: "User 1 Site 1"},
					{ID: "site2", Name: "User 1 Site 2"},
				},
			},
			{
				ID: "user2",
				Sites: []types.UserSite{
					{ID: "site2", Name: "User 2 Site 2"}, // Duplicate ID, gets overwritten/deduped
					{ID: "site3", Name: "User 2 Site 3"},
				},
			},
		}, nil).Once()

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Logf("Response body on failure: %s", rr.Body.String())
		}
		assert.Equal(t, http.StatusOK, rr.Code)

		var userSites []types.UserSite
		err := json.NewDecoder(rr.Body).Decode(&userSites)
		require.NoError(t, err)

		// Verification: should contain all 4 site entries (no deduping)
		assert.Len(t, userSites, 4)

		siteNamesForSite2 := make(map[string]bool)
		for _, us := range userSites {
			if us.ID == "site2" {
				siteNamesForSite2[us.Name] = true
			}
		}
		assert.True(t, siteNamesForSite2["User 1 Site 2"])
		assert.True(t, siteNamesForSite2["User 2 Site 2"])

		mockStorage.AssertExpectations(t)
	})
}

func TestHandleListFeatures(t *testing.T) {
	oidcSrv, priv := setupOIDCTest(t)
	defer oidcSrv.Close()
	provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
	require.NoError(t, err)

	adminToken := generateTestToken(t, oidcSrv.URL, priv, "admin@example.com", "admin-1")
	userToken := generateTestToken(t, oidcSrv.URL, priv, "user@example.com", "user-1")

	mockS := &mockStorage{}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	srv := &Server{
		storage:              mockS,
		release:              "production",
		adminEmails:          []string{"admin@example.com"},
		sessionEncryptionKey: "12345678901234567890123456789012",
		encryptionKey:        "12345678901234567890123456789012",
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 10),
		sensitiveBurst:     10,
		nowFunc:            func() time.Time { return now },
	}
	handler := srv.setupHandler()

	t.Run("Unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/features", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("Forbidden - Not Admin", func(t *testing.T) {
		mockS.On("GetUser", mock.Anything, "google:user-1").Return(types.User{
			ID:    "google:user-1",
			Email: "user@example.com",
		}, nil).Once()
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, now, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/list/features", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: userToken})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("Authorized - Admin List Features", func(t *testing.T) {
		mockS.On("GetUser", mock.Anything, "google:admin-1").Return(types.User{
			ID:    "google:admin-1",
			Email: "admin@example.com",
		}, nil).Once()
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1"}, Value: true},
				},
			},
		}, now, nil).Once()

		req := httptest.NewRequest(http.MethodGet, "/api/list/features", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: adminToken})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		var listResp []AdminFeatureFlag
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&listResp))
		flagsByName := make(map[string]AdminFeatureFlag, len(listResp))
		for _, f := range listResp {
			flagsByName[f.Name] = f
		}
		if assert.Contains(t, flagsByName, "serverTestAlpha") {
			assert.False(t, flagsByName["serverTestAlpha"].DefaultValue)
			assert.Equal(t, []feats.Criteria{{SiteIDs: []string{"site-1"}, Value: true}}, flagsByName["serverTestAlpha"].Criteria)
		}
		if assert.Contains(t, flagsByName, "serverTestBeta") {
			assert.True(t, flagsByName["serverTestBeta"].DefaultValue)
			assert.Empty(t, flagsByName["serverTestBeta"].Criteria)
		}
		mockS.AssertExpectations(t)
	})
}

func TestHandleUpdateFeatures(t *testing.T) {
	oidcSrv, priv := setupOIDCTest(t)
	defer oidcSrv.Close()
	provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
	require.NoError(t, err)

	adminToken := generateTestToken(t, oidcSrv.URL, priv, "admin@example.com", "admin-1")
	userToken := generateTestToken(t, oidcSrv.URL, priv, "user@example.com", "user-1")

	mockS := &mockStorage{}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	srv := &Server{
		storage:              mockS,
		release:              "production",
		adminEmails:          []string{"admin@example.com"},
		sessionEncryptionKey: "12345678901234567890123456789012",
		encryptionKey:        "12345678901234567890123456789012",
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
		generalRateLimit:   rate.Every(time.Minute / 30),
		generalBurst:       30,
		sensitiveRateLimit: rate.Every(time.Minute / 10),
		sensitiveBurst:     10,
		nowFunc:            func() time.Time { return now },
	}
	handler := srv.setupHandler()

	t.Run("Unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(`{"features":{}}`))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("Forbidden - Not Admin", func(t *testing.T) {
		mockS.On("GetUser", mock.Anything, "google:user-1").Return(types.User{
			ID:    "google:user-1",
			Email: "user@example.com",
		}, nil).Once()
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, now, nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(`{"features":{}}`))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: userToken})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
		mockS.AssertExpectations(t)
	})

	t.Run("Authorized - Combines SiteIDs And Persists Normalized Config", func(t *testing.T) {
		mockS.On("GetUser", mock.Anything, "google:admin-1").Return(types.User{
			ID:    "google:admin-1",
			Email: "admin@example.com",
		}, nil).Once()
		mockS.On("GetFeatureConfig", mock.Anything, "production").Return(feats.Config{}, now, nil).Once()
		expectedCfg := feats.Config{
			Features: map[string][]feats.Criteria{
				"serverTestAlpha": {
					{SiteIDs: []string{"site-1", "site-2"}, Value: true},
					{SiteIDs: []string{"site-3"}, Value: false},
					{SitePercent: 40, Value: true},
				},
			},
		}
		mockS.On("SetFeatureConfig", mock.Anything, "production", expectedCfg, now).Return(now, nil).Once()

		updateBody := `{
			"features": {
				"serverTestAlpha": [
					{"sitePercent": 40, "value": true},
					{"siteIDs": ["site-1"], "value": true},
					{"siteIDs": ["site-3"], "value": false},
					{"siteIDs": ["site-2", "site-1"], "value": true}
				]
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(updateBody))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: adminToken})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		// Evaluator in memory was NOT immediately mutated (picks up change on next synced refresh)
		ctxBeforeRefresh := srv.initFeats(context.Background(), "site-1")
		assert.False(t, ffServerTestAlpha.Enabled(ctxBeforeRefresh))
		mockS.AssertExpectations(t)
	})

	t.Run("Rejects Multiple SitePercent Criteria", func(t *testing.T) {
		mockS.On("GetUser", mock.Anything, "google:admin-1").Return(types.User{
			ID:    "google:admin-1",
			Email: "admin@example.com",
		}, nil).Once()

		multiPercentBody := `{
			"features": {
				"serverTestAlpha": [
					{"sitePercent": 20, "value": true},
					{"sitePercent": 50, "value": false}
				]
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(multiPercentBody))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: adminToken})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Contains(t, rr.Body.String(), "at most one sitePercent criterion is allowed")
		mockS.AssertExpectations(t)
	})

	t.Run("Rejects Unknown Feature Flag", func(t *testing.T) {
		mockS.On("GetUser", mock.Anything, "google:admin-1").Return(types.User{
			ID:    "google:admin-1",
			Email: "admin@example.com",
		}, nil).Once()

		unknownFlagBody := `{"features": {"nonExistentFlag": [{"value": true}]}}`
		req := httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(unknownFlagBody))
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: adminToken})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Contains(t, rr.Body.String(), "unknown feature flag")
		mockS.AssertExpectations(t)
	})
}
