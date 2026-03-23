package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
}
