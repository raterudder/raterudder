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

	// get History for Controller (Last 72 hours from Storage)
	historyStart := time.Now().Add(-72 * time.Hour)
	historyEnd := time.Now()
	energyHistory, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, historyEnd)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
	}

	// fetch weather history/forecast if location is configured
	// We pass the 72 hours of history here to sync any new solar data into the weather actuals
	if settings.Location != nil {
		if err := s.updateWeatherHistory(ctx, siteID, *settings.Location, energyHistory); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update weather history", slog.Any("error", err))
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
		if len(newPrices) > 0 {
			if err := s.storage.UpsertPrices(ctx, siteID, newPrices, types.CurrentPriceHistoryVersion); err != nil {
				return fmt.Errorf("failed to upsert prices: %w", err)
			}
		}
	}
	return nil
}

func (s *Server) updateWeatherHistory(ctx context.Context, siteID string, loc types.SiteLocation, energyHistory []types.EnergyStats) error {
	timeLoc, err := time.LoadLocation(loc.TimeZone)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to load timezone for location", slog.Any("error", err), slog.String("timezone", loc.TimeZone))
		timeLoc = time.UTC
	}

	now := time.Now().In(timeLoc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, timeLoc)

	weathers, err := s.storage.GetWeather(ctx, siteID, startOfDay, startOfDay.Add(24*time.Hour))
	if err != nil {
		return fmt.Errorf("failed to get today's weather: %w", err)
	}

	// We should update if we don't have today's weather, OR
	// if it is past sunset + 2 hours and we haven't updated yet.
	shouldUpdate := false
	if len(weathers) == 0 {
		shouldUpdate = true
	} else {
		w := weathers[0]
		// Determine if it's sunset + 2 hours
		if !w.TSSunset.IsZero() {
			sunsetPlusTwo := w.TSSunset.Add(2 * time.Hour)
			if now.After(sunsetPlusTwo) {
				// We haven't updated since sunset+2? We can check TSUpdated.
				if w.TSUpdated.Before(sunsetPlusTwo) {
					shouldUpdate = true
				}
			}
		}
	}

	if shouldUpdate {
		log.Ctx(ctx).InfoContext(ctx, "fetching weather forecast and history")

		// Fetch weather from yesterday to tomorrow
		startDay := now.AddDate(0, 0, -1)
		endDay := now.AddDate(0, 0, 2) // exclusive boundary for the day after tomorrow

		newWeathers, err := s.weather.FetchWeatherForecast(ctx, loc.Lat, loc.Long, loc.TimeZone, startDay, endDay)
		if err != nil {
			return fmt.Errorf("failed to fetch weather forecast: %w", err)
		}

		// If we successfully fetched weather, merge any incoming EnergyHistory (SolarKWH) into the new actuals
		if len(energyHistory) > 0 {
			// Optimize: use O(n) hash map lookup instead of O(n^2) nested loops for energy history matching
			ehMap := make(map[int64]float64, len(energyHistory))
			for _, eh := range energyHistory {
				ehMap[eh.TSHourStart.UTC().Unix()] = eh.SolarKWH
			}

			for wi, w := range newWeathers {
				for ahIdx, ah := range w.ActualHours {
					if solarKWH, exists := ehMap[ah.TSHourStart.UTC().Unix()]; exists {
						newWeathers[wi].ActualHours[ahIdx].SolarKWH = solarKWH
					}
				}
			}
		}

		if err := s.storage.UpsertWeather(ctx, siteID, newWeathers, types.CurrentWeatherVersion); err != nil {
			return fmt.Errorf("failed to upsert weather: %w", err)
		}
	} else if len(energyHistory) > 0 && len(weathers) > 0 {
		// Map SolarKWH to existing weather actuals if we aren't fetching new ones
		w := weathers[0]
		updated := false

		// Optimize: use O(n) hash map lookup instead of O(n^2) nested loops for energy history matching
		ehMap := make(map[int64]float64, len(energyHistory))
		for _, eh := range energyHistory {
			ehMap[eh.TSHourStart.UTC().Unix()] = eh.SolarKWH
		}

		for i, ah := range w.ActualHours {
			if solarKWH, exists := ehMap[ah.TSHourStart.UTC().Unix()]; exists {
				if w.ActualHours[i].SolarKWH != solarKWH {
					w.ActualHours[i].SolarKWH = solarKWH
					updated = true
				}
			}
		}

		if updated {
			if err := s.storage.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to upsert existing weather with new solar kwh", slog.Any("error", err))
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
			if len(newHistory) > 0 {
				if err := s.storage.UpsertEnergyHistories(ctx, siteID, newHistory, types.CurrentEnergyStatsVersion); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to upsert energy histories", slog.Any("error", err))
				}

			}
		}
	}

	return nil
}
