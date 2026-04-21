package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestHandleListESS(t *testing.T) {
	mockUMap := utility.NewMap(nil)
	mockE := ess.NewMap()

	t.Run("Returns JSON array of ESS providers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/ess", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			controller: controller.NewController(),
			showHidden: false,
		}

		srv.handleListESS(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		var systems []types.ESSProviderInfo
		err := json.NewDecoder(w.Body).Decode(&systems)
		require.NoError(t, err)
		assert.NotEmpty(t, systems, "expected at least one ESS provider in the response")
	})

	t.Run("Filters or respects hidden flag when showHidden=false", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/ess", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			showHidden: false,
		}

		srv.handleListESS(w, req)

		var systems []types.ESSProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&systems))

		var foundMock bool
		for _, s := range systems {
			if s.ID == "mock" {
				foundMock = true
				assert.True(t, s.Hidden, "mock provider should maintain hidden=true when showHidden=false")
			}
		}
		assert.True(t, foundMock, "expected to find mock provider in output")
	})

	t.Run("Clears hidden flag when showHidden=true", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/list/ess", nil)
		w := httptest.NewRecorder()

		srv := &Server{
			utilities:  mockUMap,
			ess:        mockE,
			showHidden: true,
		}

		srv.handleListESS(w, req)

		var systems []types.ESSProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&systems))

		var foundMock bool
		for _, s := range systems {
			if s.ID == "mock" {
				foundMock = true
				assert.False(t, s.Hidden, "mock provider should have hidden=false when showHidden=true")
			}
		}
		assert.True(t, foundMock, "expected to find mock provider in output")
	})

	t.Run("Passes authMiddleware for unregistered user", func(t *testing.T) {
		oidcSrv, priv := setupOIDCTest(t)
		defer oidcSrv.Close()
		provider, err := oidc.NewProvider(context.Background(), oidcSrv.URL)
		require.NoError(t, err)

		mockStorage := &mockStorage{}
		mockStorage.On("GetUser", mock.Anything, "google:unregistered@example.com").Return(types.User{}, storage.ErrUserNotFound).Once()

		mockP := ess.NewMap()
		mockP.SetSystem(types.SiteIDNone, &mockESS{}) // just to populate providers

		srv := &Server{
			utilities:          mockUMap,
			ess:                mockP,
			storage:            mockStorage,
			singleSite:         false,
			generalRateLimit:   rate.Every(time.Minute / 30),
			generalBurst:       30,
			sensitiveRateLimit: rate.Every(time.Minute / 5),
			sensitiveBurst:     5,
			oidcAudiences: map[string]string{
				"google": "test-audience",
			},
			oidcVerifiers: map[string]tokenVerifier{
				"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
			},
		}

		handler := srv.setupHandler()

		req := httptest.NewRequest("GET", "/api/list/ess", nil)
		token := generateTestToken(t, oidcSrv.URL, priv, "unregistered@example.com", "unregistered@example.com")
		req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: token})

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		var systems []types.ESSProviderInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&systems))
		assert.NotEmpty(t, systems)
		assert.True(t, mockStorage.AssertExpectations(t))
	})
}
