package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestDeleteHandlers(t *testing.T) {
	newServer := func(store *mockStorage) *Server {
		return &Server{
			storage: store,
		}
	}

	withUser := func(req *http.Request, user types.User) *http.Request {
		ctx := context.WithValue(req.Context(), userContextKey, user)
		return req.WithContext(ctx)
	}

	withSiteID := func(req *http.Request, siteID string) *http.Request {
		ctx := context.WithValue(req.Context(), siteIDContextKey, siteID)
		return req.WithContext(ctx)
	}

	t.Run("DeleteSite", func(t *testing.T) {
		t.Run("Unauthorized", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			req := httptest.NewRequest(http.MethodPost, "/api/delete/site", nil)
			w := httptest.NewRecorder()

			s.handleDeleteSite(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("Forbidden", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			req := httptest.NewRequest(http.MethodPost, "/api/delete/site", nil)
			req = withUser(req, types.User{ID: "user1", Admin: false})
			w := httptest.NewRecorder()

			s.handleDeleteSite(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})

		t.Run("InvalidSiteID", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			req := httptest.NewRequest(http.MethodPost, "/api/delete/site", nil)
			req = withUser(req, types.User{ID: "user1", Admin: true})
			req = withSiteID(req, "")
			w := httptest.NewRecorder()

			s.handleDeleteSite(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("SiteNotFound", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			store.On("GetSite", mock.Anything, "missing-site").Return(types.Site{}, storage.ErrSiteNotFound).Once()

			req := httptest.NewRequest(http.MethodPost, "/api/delete/site", nil)
			req = withUser(req, types.User{ID: "user1", Admin: true})
			req = withSiteID(req, "missing-site")
			w := httptest.NewRecorder()

			s.handleDeleteSite(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
			store.AssertExpectations(t)
		})

		t.Run("Success", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			siteID := "site-to-delete"
			site := types.Site{
				ID: siteID,
				Permissions: []types.SitePermissions{
					{UserID: "user1"},
					{UserID: "user2"},
				},
			}

			user1 := types.User{
				ID:    "user1",
				Admin: true,
				Sites: []types.UserSite{{ID: "site-to-delete", Name: "Deleted Site"}, {ID: "other-site", Name: "Other Site"}},
			}
			user2 := types.User{
				ID:    "user2",
				Sites: []types.UserSite{{ID: "site-to-delete", Name: "Deleted Site"}},
			}

			store.On("GetSite", mock.Anything, siteID).Return(site, nil).Once()
			store.On("GetUser", mock.Anything, "user1").Return(user1, nil).Once()
			store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(u types.User) bool {
				return u.ID == "user1" && len(u.Sites) == 1 && u.Sites[0].ID == "other-site"
			})).Return(nil).Once()

			store.On("GetUser", mock.Anything, "user2").Return(user2, nil).Once()
			store.On("UpdateUser", mock.Anything, mock.MatchedBy(func(u types.User) bool {
				return u.ID == "user2" && len(u.Sites) == 0
			})).Return(nil).Once()

			store.On("DeleteSite", mock.Anything, siteID).Return(nil).Once()

			req := httptest.NewRequest(http.MethodPost, "/api/delete/site", nil)
			req = withUser(req, types.User{ID: "user1", Admin: true})
			req = withSiteID(req, siteID)
			w := httptest.NewRecorder()

			s.handleDeleteSite(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			var resp map[string]string
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, "success", resp["status"])

			store.AssertExpectations(t)
		})
	})

	t.Run("DeleteUser", func(t *testing.T) {
		t.Run("Unauthorized", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			req := httptest.NewRequest(http.MethodPost, "/api/delete/user", nil)
			w := httptest.NewRecorder()

			s.handleDeleteUser(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("RemainingSitesError", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			user := types.User{
				ID:    "user1",
				Sites: []types.UserSite{{ID: "site1"}},
			}

			req := httptest.NewRequest(http.MethodPost, "/api/delete/user", nil)
			req = withUser(req, user)
			w := httptest.NewRecorder()

			s.handleDeleteUser(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			var resp map[string]string
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Contains(t, resp["error"], "all sites must be deleted first")
		})

		t.Run("Success", func(t *testing.T) {
			store := &mockStorage{}
			s := newServer(store)

			user := types.User{
				ID:    "user1",
				Sites: []types.UserSite{}, // no sites left
			}

			store.On("DeleteUser", mock.Anything, "user1").Return(nil).Once()

			req := httptest.NewRequest(http.MethodPost, "/api/delete/user", nil)
			req = withUser(req, user)
			w := httptest.NewRecorder()

			s.handleDeleteUser(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			var resp map[string]string
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, "success", resp["status"])

			// Verify cookie was cleared
			cookies := w.Result().Cookies()
			foundClearedCookie := false
			for _, c := range cookies {
				if c.Name == authTokenCookie && c.Value == "" && c.MaxAge == -1 {
					foundClearedCookie = true
				}
			}
			assert.True(t, foundClearedCookie)

			store.AssertExpectations(t)
		})
	})
}
