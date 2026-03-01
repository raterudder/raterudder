package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleSubmitFeedback(t *testing.T) {
	mockDB := new(storagemock.MockDatabase)
	server := &Server{
		storage: mockDB,
	}

	payload := map[string]interface{}{
		"sentiment": "happy",
		"comment":   "Great app!",
		"extra": map[string]string{
			"userAgent": "test-agent",
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "/api/feedback", bytes.NewBuffer(body))

	// Set up context
	ctx := req.Context()
	ctx = context.WithValue(ctx, userContextKey, types.User{ID: "user123"})
	ctx = context.WithValue(ctx, siteIDContextKey, "site123")
	req = req.WithContext(ctx)

	mockDB.On("InsertFeedback", mock.Anything, mock.MatchedBy(func(f types.Feedback) bool {
		return f.SiteID == "site123" && f.Sentiment == "happy" && f.Comment == "Great app!" && f.UserID == "user123" && f.Extra["userAgent"] == "test-agent"
	})).Return(nil)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.handleSubmitFeedback)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	mockDB.AssertExpectations(t)
}

func TestHandleListFeedback(t *testing.T) {
	mockDB := new(storagemock.MockDatabase)
	server := &Server{
		storage:     mockDB,
		adminEmails: []string{"admin@example.com"},
	}

	req, _ := http.NewRequest("GET", "/api/list/feedback", nil)

	ctx := req.Context()
	ctx = context.WithValue(ctx, userContextKey, types.User{ID: "admin1", Email: "admin@example.com"})
	req = req.WithContext(ctx)

	t.Run("Authorized", func(t *testing.T) {
		expectedFeedback := []types.Feedback{
			{
				ID:        "fb1",
				Sentiment: "happy",
				Comment:   "Good",
				SiteID:    "site1",
				UserID:    "user1",
				Timestamp: time.Now(),
			},
		}

		mockDB.On("ListFeedback", mock.Anything, 50, "").Return(expectedFeedback, nil).Once()

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleListFeedback)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp []types.Feedback
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Len(t, resp, 1)
		assert.Equal(t, "fb1", resp[0].ID)
		mockDB.AssertExpectations(t)
	})

	t.Run("Pagination", func(t *testing.T) {
		reqPagination, _ := http.NewRequest("GET", "/api/list/feedback?limit=10&lastFeedbackID=fb1", nil)
		ctxPagination := reqPagination.Context()
		ctxPagination = context.WithValue(ctxPagination, userContextKey, types.User{ID: "admin1", Email: "admin@example.com"})
		reqPagination = reqPagination.WithContext(ctxPagination)

		mockDB.On("ListFeedback", mock.Anything, 10, "fb1").Return([]types.Feedback{}, nil).Once()

		rrPagination := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleListFeedback)
		handler.ServeHTTP(rrPagination, reqPagination)

		assert.Equal(t, http.StatusOK, rrPagination.Code)
		mockDB.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		reqUnauth, _ := http.NewRequest("GET", "/api/list/feedback", nil)
		ctxUnauth := reqUnauth.Context()
		ctxUnauth = context.WithValue(ctxUnauth, userContextKey, types.User{ID: "user1", Email: "user@example.com"})
		reqUnauth = reqUnauth.WithContext(ctxUnauth)

		rrUnauth := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleListFeedback)
		handler.ServeHTTP(rrUnauth, reqUnauth)

		assert.Equal(t, http.StatusForbidden, rrUnauth.Code)
	})
}
