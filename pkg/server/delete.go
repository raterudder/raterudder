package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if !user.Admin && !s.isMultiSiteAdmin(user) {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized for site deletion", slog.String("userID", user.ID), slog.String("email", user.Email))
		writeJSONError(w, "unauthorized", http.StatusForbidden)
		return
	}

	siteID := s.getSiteID(r)
	if siteID == "" || siteID == types.SiteIDNone {
		writeJSONError(w, "invalid site ID", http.StatusBadRequest)
		return
	}

	// Fetch site to find users with access
	site, err := s.storage.GetSite(ctx, siteID)
	if err != nil {
		if errors.Is(err, storage.ErrSiteNotFound) {
			writeJSONError(w, "site not found", http.StatusNotFound)
			return
		}
		log.Ctx(ctx).ErrorContext(ctx, "failed to get site for deletion", slog.String("siteID", siteID), slog.Any("error", err))
		writeJSONError(w, "failed to get site", http.StatusInternalServerError)
		return
	}

	// First, update every user with access to remove the site from their Sites slice.
	for _, perm := range site.Permissions {
		u, err := s.storage.GetUser(ctx, perm.UserID)
		if err != nil {
			if errors.Is(err, storage.ErrUserNotFound) {
				log.Ctx(ctx).DebugContext(ctx, "user not found during site cleanup", slog.String("userID", perm.UserID), slog.String("siteID", siteID))
				continue
			}
			log.Ctx(ctx).ErrorContext(ctx, "failed to get user during site cleanup", slog.String("userID", perm.UserID), slog.String("siteID", siteID), slog.Any("error", err))
			continue
		}

		// Filter out the deleted site from user's Sites
		newSites := make([]types.UserSite, 0, len(u.Sites))
		for _, us := range u.Sites {
			if us.ID != siteID {
				newSites = append(newSites, us)
			}
		}

		u.Sites = newSites
		if err := s.storage.UpdateUser(ctx, u); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update user during site cleanup", slog.String("userID", u.ID), slog.String("siteID", siteID), slog.Any("error", err))
		} else {
			log.Ctx(ctx).DebugContext(ctx, "updated user sites list during site cleanup", slog.String("userID", u.ID), slog.String("siteID", siteID))
		}
	}

	// Delete the site
	log.Ctx(ctx).InfoContext(ctx, "deleting site", slog.String("siteID", siteID), slog.String("deletedBy", user.ID))
	if err := s.storage.DeleteSite(ctx, siteID); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to delete site", slog.String("siteID", siteID), slog.Any("error", err))
		writeJSONError(w, "failed to delete site", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Error if there are sites still left on the user
	if len(user.Sites) > 0 {
		log.Ctx(ctx).WarnContext(ctx, "cannot delete user with remaining sites", slog.String("userID", user.ID), slog.Int("siteCount", len(user.Sites)))
		writeJSONError(w, "all sites must be deleted first", http.StatusBadRequest)
		return
	}

	// Delete the user
	log.Ctx(ctx).InfoContext(ctx, "deleting user account", slog.String("userID", user.ID), slog.String("email", user.Email))
	if err := s.storage.DeleteUser(ctx, user.ID); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to delete user", slog.String("userID", user.ID), slog.Any("error", err))
		writeJSONError(w, "failed to delete user", http.StatusInternalServerError)
		return
	}

	// Log out the user after the deletion is done
	s.clearCookie(w)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
}
