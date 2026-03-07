package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// AdminSite is a site that is visible to admins along with the LastAction
type AdminSite struct {
	types.Site
	LastAction *types.Action `json:"lastAction,omitempty"`
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

	var adminSites []AdminSite
	for _, site := range sites {
		action, err := s.storage.GetLatestAction(ctx, site.ID)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get latest action", slog.String("siteID", site.ID), slog.Any("error", err))
		}
		adminSites = append(adminSites, AdminSite{
			Site:       site,
			LastAction: action,
		})
	}

	// Always return an array, even if empty
	if adminSites == nil {
		adminSites = []AdminSite{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(adminSites); err != nil {
		panic(http.ErrAbortHandler)
	}
}
