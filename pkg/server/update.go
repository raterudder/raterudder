package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
)

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)

	// 1. Get Settings and Credentials
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
		writeJSONError(w, "failed to get site settings", http.StatusInternalServerError)
		return
	}
	action, status, err := s.performSiteUpdate(ctx, siteID, settings, creds)
	if err != nil {
		// Log the error, but check if we returned an error that should be returned to the client
		log.Ctx(ctx).ErrorContext(ctx, "update failed", slog.Any("error", err))
		writeJSONError(w, "update failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if action != nil {
		if status == "" {
			status = "success"
		}
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"action": action,
			"price":  action.CurrentPrice,
		}); err != nil {
			panic(http.ErrAbortHandler)
		}
	} else {
		// No action taken
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
		}); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

func (s *Server) handleUpdateSites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sites, err := s.storage.ListSites(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list sites", slog.Any("error", err))
		writeJSONError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	results := make(map[string]string)
	for _, site := range sites {
		ctx := log.With(ctx, log.Ctx(ctx).With(slog.String("siteID", site.ID)))

		settings, creds, err := s.getSettingsWithMigration(ctx, site.ID)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
			continue
		}

		if settings.Release != s.release {
			continue
		}

		log.Ctx(ctx).DebugContext(ctx, "processing site update")
		_, status, err := s.performSiteUpdate(ctx, site.ID, settings, creds)
		if err != nil {
			// TODO: don't error when its because of missing credentials
			log.Ctx(ctx).ErrorContext(ctx, "site update failed", slog.Any("error", err))
			results[site.ID] = fmt.Sprintf("failed: %v", err)
		} else {
			log.Ctx(ctx).InfoContext(ctx, "site update success")
			if status == "" {
				status = "success"
			}
			results[site.ID] = status
		}
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(results); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) performSiteUpdate(
	ctx context.Context,
	siteID string,
	settings settingsWithVersion,
	creds types.Credentials,
) (*types.Action, string, error) {

	// get ESS System
	essSystem, err := s.getESSSystem(ctx, siteID, settings, creds)
	if err != nil {
		// TODO: how should we alert the user when this fails?
		return nil, "", fmt.Errorf("failed to get ESS system: %w", err)
	}

	// get utility
	utility, err := s.utilities.Site(ctx, siteID, settings.Settings)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get utility system (%s): %w", settings.UtilityProvider, err)
	}

	// sync energy history
	if err := s.updateEnergyHistory(ctx, siteID, essSystem); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to sync energy history", slog.Any("error", err))
		// continue even if history sync fails
	}

	if err := s.updatePriceHistory(ctx, siteID, utility); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update price history", slog.Any("error", err))
		// continue even if price history sync fails
	}

	log.Ctx(ctx).DebugContext(ctx, "update: energy history synced")

	// fetch current ESS status
	status, err := essSystem.GetStatus(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get ess status: %w", err)
	}

	log.Ctx(ctx).DebugContext(ctx, "update: ess status fetched")

	// get current price (fetched early so all actions can include the latest price)
	currentPrice, err := utility.GetCurrentPrice(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get price: %w", err)
	}

	log.Ctx(ctx).DebugContext(ctx, "update: current price fetched", slog.Float64("price", currentPrice.DollarsPerKWH), slog.Time("start", currentPrice.TSStart))

	if settings.Pause {
		log.Ctx(ctx).InfoContext(ctx, "update: paused")
		action := types.Action{
			Timestamp:    time.Now(),
			Description:  "Automation is paused",
			SystemStatus: status,
			CurrentPrice: &currentPrice,
			Paused:       true,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert paused action", slog.Any("error", err))
		}
		return &action, "paused", nil
	}

	// don't update if we're in emergency mode
	if status.EmergencyMode {
		log.Ctx(ctx).InfoContext(ctx, "update: emergency mode")
		action := types.Action{
			Timestamp:    time.Now(),
			Description:  "In emergency mode",
			Reason:       types.ActionReasonEmergencyMode,
			SystemStatus: status,
			Fault:        true,
			CurrentPrice: &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		return nil, "emergency mode", nil
	}

	if len(status.Alarms) > 0 {
		log.Ctx(ctx).InfoContext(ctx, "update: alarms present", slog.Any("alarms", status.Alarms))
		action := types.Action{
			Timestamp:    time.Now(),
			Description:  fmt.Sprintf("%d alarms present", len(status.Alarms)),
			Reason:       types.ActionReasonHasAlarms,
			SystemStatus: status,
			Fault:        true,
			CurrentPrice: &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		return nil, "alarms present", nil
	}

	// get Future Prices for controller
	futurePrices, err := utility.GetFuturePrices(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get future prices", slog.Any("error", err))
		// Continue with empty future prices
	}

	nowTime := time.Now()
	if len(futurePrices) == 0 {
		log.Ctx(ctx).WarnContext(ctx, "no future prices available, estimating using last 24 hours")
		histStart := nowTime.Add(-24 * time.Hour)
		histPrices, histErr := s.storage.GetPriceHistory(ctx, siteID, histStart, nowTime)
		if histErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get historical prices", slog.Any("error", histErr))
		} else {
			for _, p := range histPrices {
				p.TSStart = p.TSStart.Add(24 * time.Hour)
				if !p.TSEnd.IsZero() {
					p.TSEnd = p.TSEnd.Add(24 * time.Hour)
				}
				futurePrices = append(futurePrices, p)
			}
		}
	}

	hasFuture := false
	for _, fp := range futurePrices {
		if fp.TSStart.After(nowTime) {
			hasFuture = true
			break
		}
	}
	if !hasFuture {
		return nil, "", fmt.Errorf("insufficient future pricing data")
	}

	// get History for Controller (Last 72 hours from Storage)
	historyStart := time.Now().Add(-72 * time.Hour)
	historyEnd := time.Now()
	energyHistory, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, historyEnd)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
	}

	log.Ctx(ctx).DebugContext(ctx, "update: starting decision")

	// decide Action
	decision, err := s.controller.Decide(ctx, status, currentPrice, futurePrices, energyHistory, settings.Settings)
	if err != nil {
		return nil, "", fmt.Errorf("controller decision failed: %w", err)
	}

	action := decision.Action
	// Ensure timestamps match if not set
	if action.Timestamp.IsZero() {
		action.Timestamp = time.Now()
	}

	log.Ctx(ctx).InfoContext(
		ctx,
		"update: decision made",
		slog.Int("batteryMode", int(action.BatteryMode)),
		slog.Int("solarMode", int(action.SolarMode)),
		slog.String("explanation", decision.Explanation),
		slog.String("description", action.Description),
		slog.Float64("price", currentPrice.DollarsPerKWH),
		slog.Float64("batterySOC", status.BatterySOC),
	)

	// execute Action
	switch action.BatteryMode {
	case types.BatteryModeChargeAny:
		err = essSystem.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny) // Force charge
	case types.BatteryModeLoad:
		err = essSystem.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny) // Use battery
	case types.BatteryModeStandby:
		// "self_consumption" is usually safe for idle too (just don't force charge)
		err = essSystem.SetModes(ctx, types.BatteryModeStandby, types.SolarModeAny)
	}
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to set mode", slog.Any("error", err))
		action.Description += fmt.Sprintf(" (FAILED: %v)", err)
		action.Failed = true
		action.Error = err.Error()
	}
	if settings.DryRun {
		action.DryRun = true
	}

	// log Action
	if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
	}

	return &action, "", nil
}

func (s *Server) updatePriceHistory(ctx context.Context, siteID string, provider utility.Utility) error {
	lastPriceTime, lastVersion, err := s.storage.GetLatestPriceHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest price history time: %w", err)
	}

	now := time.Now()
	fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
	syncStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())

	if !lastPriceTime.IsZero() && lastVersion >= types.CurrentPriceHistoryVersion && lastPriceTime.After(syncStart) {
		syncStart = lastPriceTime.Truncate(time.Hour)
	} else if !lastPriceTime.IsZero() && lastVersion < types.CurrentPriceHistoryVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling price history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentPriceHistoryVersion),
		)
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing price history", slog.Any("since", syncStart))

	// Loop day by day
	for t := syncStart; t.Before(now); t = t.Add(24 * time.Hour) {
		// always fetch to the end of the day even if it's in the future
		end := t.Add(24 * time.Hour)

		log.Ctx(ctx).DebugContext(ctx, "syncing price history batch", slog.Time("start", t), slog.Time("end", end))
		newPrices, err := provider.GetConfirmedPrices(ctx, t, end)
		if err != nil {
			return fmt.Errorf("failed to get confirmed prices: %w", err)
		}
		for _, p := range newPrices {
			if err := s.storage.UpsertPrice(ctx, siteID, p, types.CurrentPriceHistoryVersion); err != nil {
				return fmt.Errorf("failed to upsert price: %w", err)
			}
		}
	}
	return nil
}

// does not log siteID so you should pass siteID in a logger to this method
func (s *Server) updateEnergyHistory(ctx context.Context, siteID string, essSystem ess.System) error {
	// First, find out the last time we have history for
	lastHistoryTime, lastVersion, err := s.storage.GetLatestEnergyHistoryTime(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get latest energy history time", slog.Any("error", err))
	}

	// Determine start time for fetching new data
	// We want at most last 5 days, but starting from the last record
	// truncated to the hour in case we previously stored an incomplete hour.
	now := time.Now()
	fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
	syncStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())

	// Only use lastHistoryTime if version matches
	if !lastHistoryTime.IsZero() && lastVersion >= types.CurrentEnergyStatsVersion && lastHistoryTime.After(syncStart) {
		syncStart = lastHistoryTime.Truncate(time.Hour)
	} else if !lastHistoryTime.IsZero() && lastVersion < types.CurrentEnergyStatsVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling energy history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentEnergyStatsVersion),
		)
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing energy history", slog.Any("since", syncStart))

	// Loop day by day
	for t := syncStart; t.Before(now); t = t.Add(24 * time.Hour) {
		end := t.Add(24 * time.Hour)
		if end.After(now) {
			end = now
		}

		log.Ctx(ctx).DebugContext(ctx, "syncing energy history batch", slog.Time("start", t), slog.Time("end", end))
		newHistory, err := essSystem.GetEnergyHistory(ctx, t, end)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get energy history from ess", slog.Any("error", err), slog.Time("start", t), slog.Time("end", end))
			// continue to next day even if this one failed
		} else {
			for _, h := range newHistory {
				if err := s.storage.UpsertEnergyHistory(ctx, siteID, h, types.CurrentEnergyStatsVersion); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to upsert energy history", slog.Any("error", err))
				}
			}
		}
	}

	return nil
}
