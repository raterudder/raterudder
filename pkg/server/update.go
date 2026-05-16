package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
)

type updateResult struct {
	Status string        `json:"status"`
	Action *types.Action `json:"action,omitempty"`
	Price  *types.Price  `json:"price,omitempty"`
}

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
	var result updateResult
	if action != nil {
		if status == "" {
			status = "success"
		}
		result = updateResult{
			Status: status,
			Action: action,
			Price:  action.CurrentPrice,
		}
	} else {
		// No action taken
		result = updateResult{
			Status: status,
		}
	}
	if err := json.NewEncoder(w).Encode(result); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleUpdateSites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.updateSpecificEmail != "" {
		if !s.getUpdateSpecificAuth(r) {
			writeJSONError(w, "update-specific authentication required", http.StatusUnauthorized)
			return
		}
	} else if !s.bypassAuth {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	sites, err := s.storage.ListSites(ctx)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list sites", slog.Any("error", err))
		writeJSONError(w, "failed to list sites", http.StatusInternalServerError)
		return
	}

	results := make(map[string]string)
	for _, site := range sites {
		ctx := log.With(ctx, log.Ctx(ctx).With(slog.Group("update", slog.String("siteID", site.ID))))

		settings, creds, err := s.getSettingsWithMigration(ctx, site.ID)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
			continue
		}

		if settings.Release != s.release {
			continue
		}

		if settings.ESS == "" {
			log.Ctx(ctx).DebugContext(ctx, "site update skipped: no ESS configured", slog.String("siteID", site.ID))
			results[site.ID] = "skipped: no ESS configured"
			continue
		}

		log.Ctx(ctx).DebugContext(ctx, "processing site update")
		_, status, err := s.performSiteUpdate(ctx, site.ID, settings, creds)
		if err != nil {
			if errors.Is(err, ess.ErrCredentialsMissing) {
				log.Ctx(ctx).DebugContext(ctx, "site update skipped: credentials missing", slog.Any("error", err))
				results[site.ID] = "skipped: credentials missing"
			} else {
				log.Ctx(ctx).ErrorContext(ctx, "site update failed", slog.Any("error", err))
				results[site.ID] = fmt.Sprintf("failed: %v", err)
			}
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

	if err := s.updatePriceHistory(ctx, siteID, utility, false); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update price history", slog.Any("error", err))
		// continue even if price history sync fails
	}

	log.Ctx(ctx).DebugContext(ctx, "update: energy history synced")

	// fetch current ESS status
	status, err := essSystem.GetStatus(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get ess status: %w", err)
	}

	log.Ctx(ctx).DebugContext(ctx, "update: ess status fetched", slog.Any("status", status))

	// get current price (fetched early so all actions can include the latest price)
	currentPrice, err := utility.GetCurrentPrice(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get price: %w", err)
	}

	log.Ctx(ctx).DebugContext(ctx, "update: current price fetched", slog.Any("price", currentPrice))

	// get History for Controller (Last 5 days from Storage)
	historyStart := time.Now().AddDate(0, 0, -forecastHistoryDays)
	historyEnd := time.Now()
	energyHistoryDaily, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, historyEnd)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
	}

	var energyHistory []types.EnergyStats
	for _, day := range energyHistoryDaily {
		energyHistory = append(energyHistory, day.Hourly...)
	}

	// fetch weather history/forecast if location is configured
	// We pass the history here to sync any new solar data into the weather actuals
	var weatherHistory []types.Weather
	if settings.Location != nil {
		if err := s.updateWeatherHistory(ctx, siteID, *settings.Location); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update weather history", slog.Any("error", err))
		}

		weatherHistory, err = s.storage.GetWeather(ctx, siteID, historyStart, historyEnd)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get weather history from storage", slog.Any("error", err))
		}
	}

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

	if status.GridUnavailable {
		log.Ctx(ctx).InfoContext(ctx, "update: grid unavailable")
		action := types.Action{
			Timestamp:    time.Now(),
			Description:  "Grid is unavailable",
			Reason:       types.ActionReasonGridUnavailable,
			SystemStatus: status,
			Fault:        true,
			CurrentPrice: &currentPrice,
		}
		if err := s.storage.InsertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		return nil, "grid unavailable", nil
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

	// decide Action
	decision, err := s.controller.Decide(ctx, status, currentPrice, futurePrices, energyHistory, weatherHistory, settings.Settings)
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

func (s *Server) updatePriceHistory(ctx context.Context, siteID string, provider utility.Utility, refreshNow bool) error {
	lastPriceTime, lastVersion, err := s.storage.GetLatestPriceHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest price history time: %w", err)
	}

	now := time.Now()
	fiveDaysAgo := now.AddDate(0, 0, -5)
	syncStart := truncateDay(fiveDaysAgo)

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

	if refreshNow && syncStart.After(now) {
		syncStart = now
	}

	// We fetch 12 hours into the future. Previously we fetched until the end of the day (UTC),
	// but that's end of day UTC (or whatever the server timezone is) which might not match
	// up which is confusing. 12 hours is enough so that we aren't calling this every single
	// hour, but conservative enough in case prices change.
	fetchEnd := now.Add(12 * time.Hour)

	if !syncStart.Before(fetchEnd) {
		return nil
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing price history", slog.Time("since", syncStart), slog.Time("to", fetchEnd))

	newPrices, err := provider.GetConfirmedPrices(ctx, syncStart, fetchEnd)
	if err != nil {
		return fmt.Errorf("failed to get confirmed prices: %w", err)
	}

	if len(newPrices) > 0 {
		if err := s.storage.UpsertPrices(ctx, siteID, newPrices, types.CurrentPriceHistoryVersion); err != nil {
			return fmt.Errorf("failed to upsert prices: %w", err)
		}
	}
	return nil
}

func (s *Server) updateWeatherHistory(ctx context.Context, siteID string, loc types.SiteLocation) error {
	timeLoc, err := time.LoadLocation(loc.TimeZone)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to load timezone for location", slog.Any("error", err), slog.String("timezone", loc.TimeZone))
		timeLoc = time.UTC
	}

	lastWeatherTime, lastVersion, err := s.storage.GetLatestWeatherTime(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get latest weather time", slog.Any("error", err))
	}

	now := time.Now().In(timeLoc)
	todayMidnight := truncateDay(now)
	syncStart := todayMidnight.AddDate(0, 0, -5) // Default to 5 days ago backfill

	if !lastWeatherTime.IsZero() && lastVersion >= types.CurrentWeatherVersion && lastWeatherTime.After(syncStart) {
		syncStart = truncateDay(lastWeatherTime.In(timeLoc))

		// we want to make sure we fetch the weather for today and tomorrow but if we
		// already have tomorrows weather then we don't need to fetch it again since we
		// know we already fetched it
		// if the syncStart (i.e. the latest weather time we have) is after today then
		// we know we already have tomorrow
		if syncStart.After(todayMidnight) {
			return nil
		}
	} else if !lastWeatherTime.IsZero() && lastVersion < types.CurrentWeatherVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling weather history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentWeatherVersion),
		)
	}

	// the end date is exclusive so we need to go to start of the day AFTER
	// tomorrow to make sure that we get all of tomorrow
	fetchEnd := todayMidnight.AddDate(0, 0, 2)

	log.Ctx(ctx).DebugContext(ctx, "syncing weather history", slog.Any("since", syncStart), slog.Any("to", fetchEnd))

	newWeathers, err := s.weather.Forecast(ctx, loc, syncStart, fetchEnd)
	if err != nil {
		return fmt.Errorf("failed to get weather history: %w", err)
	}

	if len(newWeathers) > 0 {
		if err := s.storage.UpsertWeather(ctx, siteID, newWeathers, types.CurrentWeatherVersion); err != nil {
			return fmt.Errorf("failed to upsert weather: %w", err)
		}
	}
	return nil
}

func (s *Server) updateEnergyHistory(ctx context.Context, siteID string, essSystem ess.System) error {
	lastEnergyTime, lastVersion, err := s.storage.GetLatestEnergyHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest energy history time: %w", err)
	}

	now := time.Now()
	fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
	syncStart := time.Date(fiveDaysAgo.Year(), fiveDaysAgo.Month(), fiveDaysAgo.Day(), 0, 0, 0, 0, fiveDaysAgo.Location())

	if !lastEnergyTime.IsZero() && lastVersion >= types.CurrentEnergyStatsVersion && lastEnergyTime.After(syncStart) {
		syncStart = lastEnergyTime.Truncate(time.Hour)
	} else if !lastEnergyTime.IsZero() && lastVersion < types.CurrentEnergyStatsVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling energy history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentEnergyStatsVersion),
		)
	}

	if !syncStart.Before(now) {
		return nil
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing energy history", slog.Any("since", syncStart), slog.Any("to", now))

	newHistory, err := essSystem.GetEnergyHistory(ctx, syncStart, now)
	if err != nil {
		return fmt.Errorf("failed to get energy history: %w", err)
	}

	if len(newHistory) > 0 {
		if err := s.storage.UpsertEnergyHistories(ctx, siteID, newHistory, types.CurrentEnergyStatsVersion); err != nil {
			return fmt.Errorf("failed to upsert energy histories: %w", err)
		}
	}
	return nil
}
