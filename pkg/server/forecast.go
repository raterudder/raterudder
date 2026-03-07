package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
)

func (s *Server) handleForecast(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// 1. Get Settings
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get settings", slog.Any("error", err))
		writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	essSystem, err := s.getESSSystem(ctx, siteID, settings, creds)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get ess system", slog.Any("error", err))
		writeJSONError(w, "failed to get ess system", http.StatusInternalServerError)
		return
	}

	// 2. Fetch current ESS status
	status, err := essSystem.GetStatus(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get ess status", slog.Any("error", err))
		writeJSONError(w, "failed to get ess status", http.StatusInternalServerError)
		return
	}

	// get utility
	utility, err := s.utilities.Site(ctx, siteID, settings.Settings)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get utility system", slog.String("utility", settings.UtilityProvider))
		writeJSONError(w, "failed to get utility system", http.StatusInternalServerError)
		return
	}

	// 3. Get Current Price
	currentPrice, err := utility.GetCurrentPrice(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get price", slog.Any("error", err))
		writeJSONError(w, "failed to get current price", http.StatusInternalServerError)
		return
	}

	// 4. Get Future Prices
	futurePrices, err := utility.GetFuturePrices(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get future prices", slog.Any("error", err))
		// Continue with empty future prices
	}

	// 5. Get History (Last 72 hours from Storage) - no backfill
	historyStart := time.Now().Add(-72 * time.Hour)
	historyEnd := time.Now()
	energyHistory, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, historyEnd)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
		writeJSONError(w, "failed to get energy history", http.StatusInternalServerError)
		return
	}

	// 6. Run Simulation
	now := time.Now().In(status.Timestamp.Location())
	simHours := s.controller.SimulateState(ctx, now, status, currentPrice, futurePrices, energyHistory, settings.Settings)

	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(simHours); err != nil {
		panic(http.ErrAbortHandler)
	}
}
