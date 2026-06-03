package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
	"golang.org/x/sync/errgroup"
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
		if errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).DebugContext(ctx, "update skipped: ESS rate limited", slog.Any("error", err))
			writeJSONError(w, err.Error(), http.StatusTooManyRequests)
			return
		}
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

	// TODO: later accept an argument that includes which update group ids to grab.
	// This will allow us to update sites in smaller batches.
	settingsMap, versionsMap, err := s.storage.ListSitesSettings(ctx, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list sites settings", slog.Any("error", err))
		writeJSONError(w, "failed to list sites settings", http.StatusInternalServerError)
		return
	}

	results := make(map[string]string)
	var mu sync.Mutex

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	for siteID, settings := range settingsMap {
		version := versionsMap[siteID]
		g.Go(func() error {
			if err := gCtx.Err(); err != nil {
				return err
			}

			ctx := log.With(gCtx, log.Ctx(gCtx).With(slog.Group("update", slog.String("siteID", siteID))))

			sv, creds, err := s.migrateAndDecryptSettings(ctx, siteID, settings, version)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
				return nil
			}

			if sv.Release != s.release {
				return nil
			}

			if sv.ESS == "" {
				log.Ctx(ctx).DebugContext(ctx, "site update skipped: no ESS configured", slog.String("siteID", siteID))
				mu.Lock()
				results[siteID] = "skipped: no ESS configured"
				mu.Unlock()
				return nil
			}

			log.Ctx(ctx).DebugContext(ctx, "processing site update")
			_, status, err := s.performSiteUpdate(ctx, siteID, sv, creds)

			var resVal string
			if err != nil {
				if errors.Is(err, errESSRateLimited) {
					log.Ctx(ctx).DebugContext(ctx, "site update skipped: ESS rate limited", slog.Any("error", err))
					resVal = "skipped: ESS rate limited"
				} else if errors.Is(err, ess.ErrCredentialsMissing) {
					log.Ctx(ctx).DebugContext(ctx, "site update skipped: credentials missing", slog.Any("error", err))
					resVal = "skipped: credentials missing"
				} else {
					log.Ctx(ctx).ErrorContext(ctx, "site update failed", slog.Any("error", err))
					resVal = fmt.Sprintf("failed: %v", err)
				}
			} else {
				log.Ctx(ctx).InfoContext(ctx, "site update success")
				if status == "" {
					status = "success"
				}
				resVal = status
			}

			mu.Lock()
			results[siteID] = resVal
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to run site updates in parallel", slog.Any("error", err))
		writeJSONError(w, "failed to run site updates in parallel", http.StatusInternalServerError)
		return
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
	// in case the timestamp is off, we just use the location of it
	now := time.Now().In(status.Timestamp.Location())
	historyStart := now.AddDate(0, 0, -forecastHistoryDays).Truncate(time.Hour)
	energyHistoryDaily, err := s.storage.GetEnergyHistory(ctx, siteID, historyStart, now)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get energy history from storage", slog.Any("error", err))
	}

	var energyHistory []types.EnergyStats
	for _, day := range energyHistoryDaily {
		energyHistory = append(energyHistory, day.Hourly...)
	}

	// fetch weather history/forecast if location is configured
	var weatherHistory []types.Weather
	if settings.Location != nil {
		if err := s.updateWeatherHistory(ctx, siteID, *settings.Location); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update weather history", slog.Any("error", err))
		}

		if timeLoc, err := time.LoadLocation(settings.Location.TimeZone); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to load location timezone", slog.Any("error", err), slog.String("timeZone", settings.Location.TimeZone))
			// fallback to at least fetching something and 2 days in the future
			weatherHistory, err = s.storage.GetWeather(ctx, siteID, historyStart, now.AddDate(0, 0, 2))
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to get weather history from storage", slog.Any("error", err))
			}
		} else {
			start := time.Date(historyStart.Year(), historyStart.Month(), historyStart.Day(), 0, 0, 0, 0, timeLoc)
			end := time.Now().In(timeLoc).AddDate(0, 0, 2)
			weatherHistory, err = s.storage.GetWeather(ctx, siteID, start, end)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to get weather history from storage", slog.Any("error", err))
			}
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

	if err := s.canSetModes(settings); err != nil {
		return nil, "", err
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
		slog.String("description", action.Description),
		slog.Float64("price", currentPrice.DollarsPerKWH),
		slog.Float64("batterySOC", status.BatterySOC),
	)

	// execute Action
	err = s.setESSModes(ctx, siteID, essSystem, action.BatteryMode, settings)
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

	lastWeatherTime, lastWeatherUpdated, lastVersion, err := s.storage.GetLatestWeatherTime(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get latest weather time", slog.Any("error", err))
	}

	now := time.Now().In(timeLoc)
	todayMidnight := truncateDay(now)
	syncStart := todayMidnight.AddDate(0, 0, -5) // Default to 5 days ago backfill

	if !lastWeatherTime.IsZero() && lastVersion >= types.CurrentWeatherVersion && lastWeatherTime.After(syncStart) {
		// We extract the date parts from lastWeatherTime (which is parsed in UTC from the YYYY-MM-DD doc ID)
		// and construct the time in timeLoc. Using truncateDay(lastWeatherTime.In(timeLoc)) would shift
		// the midnight time to the previous day under negative UTC offsets (e.g. 00:00 UTC -> 20:00 local).
		syncStart = time.Date(lastWeatherTime.Year(), lastWeatherTime.Month(), lastWeatherTime.Day(), 0, 0, 0, 0, timeLoc)

		// we want to make sure we fetch the weather for today and tomorrow but if we
		// already have tomorrows weather then we don't need to fetch it again since we
		// know we already fetched it
		// if the syncStart (i.e. the latest weather time we have) is after today then
		// we know we already have tomorrow
		if syncStart.After(todayMidnight) {
			// We already have tomorrow's weather, but we want to refresh the forecast at least once
			// in the morning after 6 AM local time.
			sixAMToday := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, timeLoc)
			lastUpdateLocal := lastWeatherUpdated.In(timeLoc)
			if now.After(sixAMToday) && lastUpdateLocal.Before(sixAMToday) {
				syncStart = todayMidnight
			} else {
				return nil
			}
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
		// Fetch old weather for today to check if there is a significant change.
		oldWeathers, err := s.storage.GetWeather(ctx, siteID, todayMidnight, todayMidnight.Add(24*time.Hour))
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to fetch old weather for today for comparison", slog.Any("error", err))
		} else if len(oldWeathers) > 0 {
			oldWeather := oldWeathers[0]
			var newWeather *types.Weather
			for i := range newWeathers {
				if newWeathers[i].TSDayStart.Equal(todayMidnight) {
					newWeather = &newWeathers[i]
					break
				}
			}

			if newWeather != nil {
				oldHoursMap := make(map[time.Time]types.HourlyWeather)
				for _, hw := range oldWeather.ForecastHours {
					oldHoursMap[hw.TSHourStart.UTC()] = hw
				}

				var hours []string
				var oldGTIs []float64
				var newGTIs []float64
				anySignificantChange := false

				for _, newHw := range newWeather.ForecastHours {
					if oldHw, found := oldHoursMap[newHw.TSHourStart.UTC()]; found {
						isDaylight := oldHw.GTI > 0 || newHw.GTI > 0
						if isDaylight {
							hours = append(hours, newHw.TSHourStart.In(timeLoc).Format("15:04"))
							oldGTIs = append(oldGTIs, oldHw.GTI)
							newGTIs = append(newGTIs, newHw.GTI)

							if oldHw.GTI > 10 || newHw.GTI > 10 {
								if oldHw.GTI == 0 {
									if newHw.GTI > 10 {
										anySignificantChange = true
									}
								} else {
									ratio := newHw.GTI / oldHw.GTI
									if ratio > 1.20 || ratio < 0.80 {
										anySignificantChange = true
									}
								}
							}
						}
					}
				}

				if anySignificantChange {
					log.Ctx(ctx).InfoContext(
						ctx,
						"weather forecast changed significantly after morning refresh",
						slog.Any("hours", hours),
						slog.Any("oldGti", oldGTIs),
						slog.Any("newGti", newGTIs),
					)
				}
			}
		}

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

func (s *Server) canSetModes(settings settingsWithVersion) error {
	failures := settings.ESSAuthStatus.ConsecutiveSetFailures
	if failures > 1 {
		backoff := getESSBackoff(failures)
		timeLeft := backoff - time.Since(settings.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			timeLeft = timeLeft.Round(time.Second)
			return fmt.Errorf("%w, try again in %v", errESSRateLimited, timeLeft)
		}
	}
	return nil
}

func (s *Server) setESSModes(
	ctx context.Context,
	siteID string,
	essSystem ess.System,
	batteryMode types.BatteryMode,
	settings settingsWithVersion,
) error {
	if err := s.canSetModes(settings); err != nil {
		return err
	}

	var err error
	switch batteryMode {
	case types.BatteryModeChargeAny:
		err = essSystem.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeAny) // Force charge
	case types.BatteryModeLoad:
		err = essSystem.SetModes(ctx, types.BatteryModeLoad, types.SolarModeAny) // Use battery
	case types.BatteryModeStandby:
		// "self_consumption" is usually safe for idle too (just don't force charge)
		err = essSystem.SetModes(ctx, types.BatteryModeStandby, types.SolarModeAny)
	}

	if err != nil {
		if errors.Is(err, ess.ErrUnauthorized) {
			settings.ESSAuthStatus.ConsecutiveSetFailures++
			settings.ESSAuthStatus.LastAttempt = time.Now().UTC()
			if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); dbErr != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status after set modes failure", slog.Any("error", dbErr))
			}
		}
		return err
	}

	if settings.ESSAuthStatus.ConsecutiveSetFailures > 0 {
		settings.ESSAuthStatus.ConsecutiveSetFailures = 0
		settings.ESSAuthStatus.LastAttempt = time.Now().UTC()
		if dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status after set modes success", slog.Any("error", dbErr))
		}
	}
	return nil
}
