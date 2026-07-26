package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) handleListUtilities(w http.ResponseWriter, r *http.Request) {
	utilities := s.utilities.ListUtilities()

	if s.showHidden {
		for i := range utilities {
			utilities[i].Hidden = false
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=300")
	if err := json.NewEncoder(w).Encode(utilities); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleGetPeriods(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	effectiveSettings := settings.Settings

	if qProvider := r.URL.Query().Get("utilityProvider"); qProvider != "" {
		effectiveSettings.UtilityProvider = qProvider
	}
	if qRate := r.URL.Query().Get("utilityRate"); qRate != "" {
		effectiveSettings.UtilityRate = qRate
	}
	if qRateOpts := r.URL.Query().Get("utilityRateOptions"); qRateOpts != "" {
		var opts types.UtilityRateOptions
		if err := json.Unmarshal([]byte(qRateOpts), &opts); err == nil {
			effectiveSettings.UtilityRateOptions = opts
		}
	}

	if effectiveSettings.UtilityProvider == "" {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(nil); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}

	u, err := s.utilities.Site(ctx, siteID, effectiveSettings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get utility provider", slog.Any("error", err))
		writeJSONError(w, "failed to get utility provider", http.StatusInternalServerError)
		return
	}

	periods, err := u.GetPeriods(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get utility periods", slog.Any("error", err))
		writeJSONError(w, "failed to get utility periods", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(periods); err != nil {
		panic(http.ErrAbortHandler)
	}
}
