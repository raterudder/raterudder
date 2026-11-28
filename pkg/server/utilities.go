package server

import (
	"encoding/json"
	"fmt"
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
	w.Header().Set("Cache-Control", "public, max-age=3600")
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
	query := r.URL.Query()

	if query.Has("utilityProvider") {
		qProvider := query.Get("utilityProvider")
		if qProvider != effectiveSettings.UtilityProvider {
			effectiveSettings.UtilityProvider = qProvider
			effectiveSettings.UtilityRate = ""
			effectiveSettings.UtilityRateOptions = types.UtilityRateOptions{}
		}
	}
	if query.Has("utilityRate") {
		qRate := query.Get("utilityRate")
		if qRate != effectiveSettings.UtilityRate {
			effectiveSettings.UtilityRate = qRate
			effectiveSettings.UtilityRateOptions = types.UtilityRateOptions{}
		}
	}
	if query.Has("utilityRateOptions") {
		var opts types.UtilityRateOptions
		if qRateOpts := query.Get("utilityRateOptions"); qRateOpts != "" {
			if err := json.Unmarshal([]byte(qRateOpts), &opts); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "invalid utility rate options", slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("invalid utility rate options: %v", err), http.StatusBadRequest)
				return
			}
		}
		effectiveSettings.UtilityRateOptions = opts
	}

	if effectiveSettings.UtilityProvider == "" || effectiveSettings.UtilityRate == "" {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(nil); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}

	u, err := s.utilities.PreviewSite(ctx, effectiveSettings)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx,
			"invalid utility provider settings",
			slog.String("utilityProvider", effectiveSettings.UtilityProvider),
			slog.String("utilityRate", effectiveSettings.UtilityRate),
			slog.Any("error", err),
		)
		writeJSONError(w, fmt.Sprintf("invalid utility provider settings: %v", err), http.StatusBadRequest)
		return
	}

	periods, err := u.GetPeriods(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get utility periods", slog.Any("error", err))
		writeJSONError(w, "failed to get utility periods", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=60")
	if err := json.NewEncoder(w).Encode(periods); err != nil {
		panic(http.ErrAbortHandler)
	}
}
