package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAdminListSites(t *testing.T) {
	mockStorage := &mockStorage{}
	sites := []types.Site{
		{ID: "site1"},
		{ID: "site2"},
	}
	mockStorage.On("ListSites", mock.Anything).Return(sites, nil)
	mockStorage.On("GetLatestAction", mock.Anything, "site1").Return(&types.Action{Description: "test1 action"}, nil)
	mockStorage.On("GetLatestAction", mock.Anything, "site2").Return((*types.Action)(nil), nil)
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
	}
	handler := srv.setupHandler()

	t.Run("Unauthorized - Not Admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/sites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validUserToken})

		mockStorage.On("GetUser", mock.Anything, "user1").Return(types.User{
			ID:    "user1",
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

		mockStorage.On("GetUser", mock.Anything, "admin1").Return(types.User{
			ID:    "admin1",
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
				}
				if s.ID == "site2" {
					assert.Nil(t, s.LastAction)
				}
			}
		}
	})

	t.Run("Through Auth Middleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/list/sites", nil)
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validAdminToken})

		mockStorage.On("GetUser", mock.Anything, "admin1").Return(types.User{
			ID:    "admin1",
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
