package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/errgroup"
)

// AdminSite is a site that is visible to admins along with the LastAction
type AdminSite struct {
	types.Site
	LastAction *types.Action `json:"lastAction,omitempty"`
	Alias      string        `json:"alias,omitempty"`
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	// Check if user is an admin
	// We aren't specifically checking for singleSite here because this is for
	// listing sites which isn't even supported for singleSite
	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to list sites", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	sites, err := s.storage.ListSites(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list sites", slog.Any("error", err))
		writeJSONError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	adminSettings, err := s.storage.GetAdminSettings(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get admin settings", slog.Any("error", err))
		writeJSONError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	adminSites := make([]AdminSite, len(sites))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(5)
	for i, site := range sites {
		g.Go(func() error {
			action, err := s.storage.GetLatestAction(gCtx, site.ID)
			if err != nil {
				log.Ctx(gCtx).WarnContext(gCtx, "failed to get latest action", slog.String("siteID", site.ID), slog.Any("error", err))
				return err
			}
			adminSites[i] = AdminSite{
				Site:       site,
				LastAction: action,
				Alias:      adminSettings.Aliases[site.ID],
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get latest actions", slog.Any("error", err))
		writeJSONError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(adminSites); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleSetSiteAlias(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	// Check if user is an admin
	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to set site alias", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		SiteID string `json:"siteID"`
		Alias  string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SiteID == "" {
		writeJSONError(w, "siteID is required", http.StatusBadRequest)
		return
	}

	adminSettings, err := s.storage.GetAdminSettings(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get admin settings", slog.Any("error", err))
		writeJSONError(w, "failed to set site alias", http.StatusInternalServerError)
		return
	}

	if adminSettings.Aliases == nil {
		adminSettings.Aliases = make(map[string]string)
	}

	if req.Alias == "" {
		delete(adminSettings.Aliases, req.SiteID)
	} else {
		adminSettings.Aliases[req.SiteID] = req.Alias
	}

	if err := s.storage.UpdateAdminSettings(ctx, adminSettings); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update admin settings", slog.Any("error", err))
		writeJSONError(w, "failed to set site alias", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleListUserSites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	// Check if user is an admin
	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to list user sites", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	users, err := s.storage.ListUsers(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list users for user sites", slog.Any("error", err))
		writeJSONError(w, "failed to list user sites", http.StatusInternalServerError)
		return
	}

	var userSites []types.UserSite
	for _, u := range users {
		for _, us := range u.Sites {
			userSites = append(userSites, us)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(userSites); err != nil {
		panic(http.ErrAbortHandler)
	}
}
