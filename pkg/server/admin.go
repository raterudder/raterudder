package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/errgroup"
)

// AdminSite is a site that is visible to admins along with the LastAction
type AdminSite struct {
	types.Site
	LastAction *types.Action   `json:"lastAction,omitempty"`
	Alias      string          `json:"alias,omitempty"`
	Features   map[string]bool `json:"features,omitempty"`
}

// AdminFeatureFlag represents a defined boolean feature flag and its current criteria for admins.
type AdminFeatureFlag struct {
	Name         string           `json:"name"`
	DefaultValue bool             `json:"defaultValue"`
	Criteria     []feats.Criteria `json:"criteria,omitempty"`
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

	featsEval := s.getFeatsEvaluator(ctx)
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
				Features:   featsEval.Evaluate(site.ID),
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
		userSites = append(userSites, u.Sites...)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(userSites); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleListFeatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to list features", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	release := s.featsRelease()

	cfg, _, err := s.storage.GetFeatureConfig(ctx, release)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get feature config", slog.String("release", release), slog.Any("error", err))
		writeJSONError(w, "failed to get feature config", http.StatusInternalServerError)
		return
	}

	defined := feats.Defined()
	flags := make([]AdminFeatureFlag, 0, len(defined))
	for name, flag := range defined {
		flags = append(flags, AdminFeatureFlag{
			Name:         name,
			DefaultValue: flag.Default(),
			Criteria:     cfg.Features[name],
		})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(flags); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleUpdateFeatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to update features", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Features map[string][]feats.Criteria `json:"features"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	defined := feats.Defined()
	normalizedUpdates := make(map[string][]feats.Criteria, len(req.Features))
	for name, criteria := range req.Features {
		if _, ok := defined[name]; !ok {
			writeJSONError(w, fmt.Sprintf("unknown feature flag: %s", name), http.StatusBadRequest)
			return
		}
		normalized, err := feats.NormalizeCriteria(criteria)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		normalizedUpdates[name] = normalized
	}

	release := s.featsRelease()

	cfg, updatedAt, err := s.storage.GetFeatureConfig(ctx, release)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get feature config for update", slog.String("release", release), slog.Any("error", err))
		writeJSONError(w, "failed to update feature config", http.StatusInternalServerError)
		return
	}

	if cfg.Features == nil {
		cfg.Features = make(map[string][]feats.Criteria)
	}
	for name, normalized := range normalizedUpdates {
		if len(normalized) == 0 {
			delete(cfg.Features, name)
		} else {
			cfg.Features[name] = normalized
		}
	}

	if _, err := s.storage.SetFeatureConfig(ctx, release, cfg, updatedAt); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save feature config", slog.String("release", release), slog.Any("error", err))
		writeJSONError(w, "failed to update feature config", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
