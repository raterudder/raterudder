package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleSubmitInterest(t *testing.T) {
	mockDB := new(storagemock.MockDatabase)
	s := &Server{
		storage:     mockDB,
		adminEmails: []string{"admin@example.com"},
	}

	t.Run("Authenticated", func(t *testing.T) {
		mockDB.ExpectedCalls = nil
		reqBody := map[string]string{
			"utility":             "other",
			"battery":             "other",
			"utilityProviderName": "Test Utility",
			"state":               "CA",
			"planName":            "Test Plan",
			"batteryName":         "Test Battery",
			"comments":            "Test Comment",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/interest", bytes.NewBuffer(body))

		// Setup user in context
		user := types.User{ID: "test-user", Email: "test@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))

		mockDB.On("UpsertInterest", mock.Anything, mock.MatchedBy(func(it types.InterestSubmission) bool {
			return it.Email == "test@example.com" && it.Utility == "other" && it.UtilityProviderName == "Test Utility"
		})).Return(nil)

		w := httptest.NewRecorder()
		s.handleSubmitInterest(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("ToRegister", func(t *testing.T) {
		mockDB.ExpectedCalls = nil
		reqBody := map[string]string{
			"utility": "other",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/interest", bytes.NewBuffer(body))

		// Setup userToRegister in context
		user := types.User{ID: "new-user", Email: "new@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userToRegisterContextKey, user))

		mockDB.On("UpsertInterest", mock.Anything, mock.MatchedBy(func(it types.InterestSubmission) bool {
			return it.Email == "new@example.com" && it.Utility == "other"
		})).Return(nil)

		w := httptest.NewRecorder()
		s.handleSubmitInterest(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		mockDB.AssertExpectations(t)
	})
	t.Run("EmptyValidation", func(t *testing.T) {
		mockDB.ExpectedCalls = nil
		reqBody := map[string]string{}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/interest", bytes.NewBuffer(body))

		user := types.User{ID: "test-user", Email: "test@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))

		w := httptest.NewRecorder()
		s.handleSubmitInterest(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "at least one field must be provided")
	})
}

func TestHandleSubmitInterestAuth(t *testing.T) {
	srv, priv := setupOIDCTest(t)
	defer srv.Close()
	provider, err := oidc.NewProvider(context.Background(), srv.URL)
	require.NoError(t, err)

	validToken := generateTestToken(t, srv.URL, priv, "new@example.com", "new-user-id")

	mockDB := new(storagemock.MockDatabase)
	s := &Server{
		storage: mockDB,
		oidcAudiences: map[string]string{
			"google": "test-audience",
		},
		oidcVerifiers: map[string]tokenVerifier{
			"google": provider.Verifier(&oidc.Config{ClientID: "test-audience"}).Verify,
		},
	}

	// Verify it ignores user lookup error and siteID requirement
	mockDB.On("GetUser", mock.Anything, "new-user-id").Return(types.User{}, storage.ErrUserNotFound).Once()
	mockDB.On("UpsertInterest", mock.Anything, mock.MatchedBy(func(it types.InterestSubmission) bool {
		return it.Email == "new@example.com"
	})).Return(nil).Once()

	reqBody := map[string]string{"comments": "Test"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/interest", bytes.NewBuffer(body))
	req.AddCookie(&http.Cookie{Name: authTokenCookie, Value: validToken})

	w := httptest.NewRecorder()
	// Wrap with authMiddleware
	handler := s.authMiddleware(http.HandlerFunc(s.handleSubmitInterest))
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	mockDB.AssertExpectations(t)
}

func TestHandleListInterest(t *testing.T) {
	mockDB := new(storagemock.MockDatabase)
	s := &Server{
		storage:     mockDB,
		adminEmails: []string{"admin@example.com"},
	}

	t.Run("AdminOnly", func(t *testing.T) {
		mockDB.ExpectedCalls = nil
		// Non-admin request
		req := httptest.NewRequest("GET", "/api/list/interest", nil)
		user := types.User{ID: "test-user", Email: "test@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))

		w := httptest.NewRecorder()
		s.handleListInterest(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)

		// Admin request
		req = httptest.NewRequest("GET", "/api/list/interest", nil)
		admin := types.User{ID: "admin-user", Email: "admin@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, admin))

		expected := []types.InterestSubmission{{Email: "test@example.com"}}
		mockDB.On("ListInterest", mock.Anything, 50).Return(expected, nil).Once()

		w = httptest.NewRecorder()
		s.handleListInterest(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var list []types.InterestSubmission
		err := json.NewDecoder(w.Body).Decode(&list)
		require.NoError(t, err)
		assert.NotEmpty(t, list)
		mockDB.AssertExpectations(t)
	})

	t.Run("WithLimit", func(t *testing.T) {
		mockDB.ExpectedCalls = nil
		req := httptest.NewRequest("GET", "/api/list/interest?limit=10", nil)
		admin := types.User{ID: "admin-user", Email: "admin@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, admin))

		// Expect custom limit to be passed
		expected := []types.InterestSubmission{{Email: "test@example.com"}}
		mockDB.On("ListInterest", mock.Anything, 10).Return(expected, nil).Once()

		w := httptest.NewRecorder()
		s.handleListInterest(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("InvalidLimitFallback", func(t *testing.T) {
		mockDB.ExpectedCalls = nil
		req := httptest.NewRequest("GET", "/api/list/interest?limit=invalid", nil)
		admin := types.User{ID: "admin-user", Email: "admin@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, admin))

		// Expect default limit (50) to be passed
		expected := []types.InterestSubmission{{Email: "test@example.com"}}
		mockDB.On("ListInterest", mock.Anything, 50).Return(expected, nil).Once()

		w := httptest.NewRecorder()
		s.handleListInterest(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		mockDB.AssertExpectations(t)
	})
}
