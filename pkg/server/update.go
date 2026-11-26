package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
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

const (
	energyBackfillWindowDays = 5
	siteUpdateControlReserve = 30 * time.Second
	defaultWeatherRetryDelay = 3 * time.Second
)

func (s *Server) getWeatherRetryDelay() time.Duration {
	if s.weatherRetryDelay > 0 {
		return s.weatherRetryDelay
	}
	return defaultWeatherRetryDelay
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var wg sync.WaitGroup
	ctx := common.CtxWithWaitGroup(r.Context(), &wg)

	if s.updateSpecificEmail != "" {
		if !s.getUpdateSpecificAuth(r) {
			writeJSONError(w, "update-specific authentication required", http.StatusUnauthorized)
			return
		}
	} else if !s.bypassAuth {
		user := s.getUser(r)
		if user.ID == "" {
			writeJSONError(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if !user.Admin {
			log.Ctx(ctx).WarnContext(ctx, "unauthorized for site update", slog.String("userID", user.ID), slog.String("email", user.Email))
			writeJSONError(w, "unauthorized", http.StatusForbidden)
			return
		}
	}

	siteID := s.getSiteID(r)

	// 1. Get Settings and Credentials
	settings, creds, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
		writeJSONError(w, "failed to get site settings", http.StatusInternalServerError)
		return
	}
	action, status, err := s.performSiteUpdate(ctx, siteID, settings, creds, false)
	if err != nil {
		if errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).DebugContext(ctx, "update skipped: ESS rate limited", slog.Any("error", err))
			writeJSONError(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		if errors.Is(err, ess.ErrUnauthorized) {
			log.Ctx(ctx).WarnContext(ctx, "update failed: ESS unauthorized", slog.Any("error", err))
		} else {
			log.Ctx(ctx).ErrorContext(ctx, "update failed", slog.Any("error", err))
		}
		writeJSONError(w, "update failed", http.StatusInternalServerError)
		return
	}

	// Wait for any background verification goroutines before writing the HTTP response
	// so Cloud Run considers the HTTP request active and does not throttle CPU.
	wg.Wait()

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
	var wg sync.WaitGroup
	ctx := common.CtxWithWaitGroup(r.Context(), &wg)

	if s.updateSpecificEmail != "" {
		if !s.getUpdateSpecificAuth(r) {
			writeJSONError(w, "update-specific authentication required", http.StatusUnauthorized)
			return
		}
	} else if !s.bypassAuth {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	kindParam := r.URL.Query().Get("kind")
	idxParam := r.URL.Query().Get("idx")
	totalParam := r.URL.Query().Get("total")
	cronParam := r.URL.Query().Get("cron")

	var kind string
	var idx, total int
	if kindParam != "" {
		kind = kindParam
		if kind == "all" && idxParam == "" && totalParam == "" {
			idx = 0
			total = 1
		} else {
			if idxParam == "" || totalParam == "" {
				writeJSONError(w, "idx and total are required when kind is specified", http.StatusBadRequest)
				return
			}
			var err error
			idx, err = strconv.Atoi(idxParam)
			if err != nil {
				writeJSONError(w, "invalid idx parameter", http.StatusBadRequest)
				return
			}
			total, err = strconv.Atoi(totalParam)
			if err != nil {
				writeJSONError(w, "invalid total parameter", http.StatusBadRequest)
				return
			}
		}
	} else if cronParam != "" {
		switch cronParam {
		case "1":
			kind = "tou"
			idx = 0
			total = 2
		case "2":
			kind = "tou"
			idx = 1
			total = 2
		case "3":
			kind = "comed"
			idx = 0
			total = 1
		default:
			writeJSONError(w, "invalid cron parameter", http.StatusBadRequest)
			return
		}
	}

	groups, err := getCronGroups(s.now(), kind, idx, total)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "invalid group partitioning parameters", slog.Any("error", err))
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Ctx(ctx).DebugContext(ctx, "handling update sites",
		slog.String("kind", kind),
		slog.Int("idx", idx),
		slog.Int("total", total),
		slog.Any("groups", groups),
	)
	settingsMap, versionsMap, updatedTimesMap, err := s.storage.ListSitesSettings(ctx, s.release, groups)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list sites settings", slog.Any("error", err))
		writeJSONError(w, "failed to list sites settings", http.StatusInternalServerError)
		return
	}

	results := make(map[string]string)
	var mu sync.Mutex

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for siteID, settings := range settingsMap {
		version := versionsMap[siteID]
		updatedTime := updatedTimesMap[siteID]
		g.Go(func() error {
			if err := gCtx.Err(); err != nil {
				return err
			}

			siteCtx, cancel := context.WithTimeout(gCtx, 90*time.Second)
			defer cancel()

			ctx := log.With(siteCtx, log.Ctx(siteCtx).With(slog.Group("update", slog.String("siteID", siteID))))
			ctx = s.initFeats(ctx, siteID)

			defer func() {
				if r := recover(); r != nil {
					log.Ctx(ctx).ErrorContext(ctx, "site update panicked", slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
					mu.Lock()
					results[siteID] = fmt.Sprintf("failed: panic: %v", r)
					mu.Unlock()
				}
			}()

			sv, creds, err := s.migrateAndDecryptSettings(ctx, siteID, settings, version, updatedTime)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get site settings", slog.Any("error", err))
				return nil
			}

			if sv.Release != s.release {
				return nil
			}

			if sv.ESS == "" {
				log.Ctx(ctx).DebugContext(ctx, "site update skipped: no ESS configured", slog.String("siteID", siteID), slog.Int("updateGroup", sv.UpdateGroup))
				mu.Lock()
				results[siteID] = "skipped: no ESS configured"
				mu.Unlock()
				return nil
			}

			log.Ctx(ctx).DebugContext(ctx, "processing site update", slog.Int("updateGroup", sv.UpdateGroup))
			_, status, err := s.performSiteUpdate(ctx, siteID, sv, creds, true)

			var resVal string
			if err != nil {
				if errors.Is(err, errESSRateLimited) {
					log.Ctx(ctx).DebugContext(ctx, "site update skipped: ESS rate limited", slog.Any("error", err))
					resVal = "skipped: ESS rate limited"
				} else if errors.Is(err, ess.ErrCredentialsMissing) {
					log.Ctx(ctx).DebugContext(ctx, "site update skipped: credentials missing", slog.Any("error", err))
					resVal = "skipped: credentials missing"
				} else if errors.Is(err, ess.ErrUnauthorized) {
					log.Ctx(ctx).WarnContext(ctx, "site update failed: ESS unauthorized", slog.Any("error", err))
					resVal = fmt.Sprintf("failed: %v", err)
				} else {
					log.Ctx(ctx).ErrorContext(ctx, "site update failed", slog.Any("error", err))
					resVal = fmt.Sprintf("failed: %v", err)
				}
			} else if strings.HasPrefix(status, "skipped:") {
				resVal = status
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

	// Wait for any background verification goroutines before writing the HTTP response
	// so Cloud Run considers the HTTP request active and does not throttle CPU.
	wg.Wait()

	log.Ctx(ctx).InfoContext(ctx, "site update heartbeat",
		slog.String("event", eventSiteUpdateHeartbeat),
		slog.String("kind", kind),
		slog.Int("idx", idx),
		slog.Int("total", total),
		slog.Int("siteCount", len(results)),
	)

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
	skipIfRecent bool,
) (*types.Action, string, error) {
	latestAction := s.getLatestAction(ctx, siteID)
	if skipIfRecent && latestAction != nil && !latestAction.Timestamp.IsZero() && s.now().Sub(latestAction.Timestamp) < 5*time.Minute {
		log.Ctx(ctx).DebugContext(ctx, "site update skipped: recently updated", slog.Time("lastActionTime", latestAction.Timestamp))
		return nil, "skipped: recently updated", nil
	}

	notifData := &dataForNotifications{
		settings: settings.Settings,
	}

	// get ESS System
	essSystem, settings, err := s.getESSSystem(ctx, siteID, settings, creds)
	if err != nil {
		// TODO: how should we alert the user when this fails?
		return nil, "", fmt.Errorf("failed to get ESS system: %w", err)
	}
	notifData.essSystem = essSystem

	// get utility
	utility, err := s.utilities.Site(ctx, siteID, settings.Settings)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get utility system (%s): %w", settings.UtilityProvider, err)
	}

	// Cap optional history syncs so at least 30 seconds remain on ctx for GetStatus and setESSModes.
	syncCtx := ctx
	cancelSync := func() {}
	runOptionalSyncs := true
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		syncDeadline := deadline.Add(-siteUpdateControlReserve)
		if !time.Now().Before(syncDeadline) {
			runOptionalSyncs = false
			if ctx.Err() == nil {
				log.Ctx(ctx).WarnContext(ctx, "skipping optional history syncs due to insufficient remaining update budget",
					slog.Duration("remaining", time.Until(deadline)),
				)
			}
		} else {
			syncCtx, cancelSync = context.WithDeadline(ctx, syncDeadline)
		}
	}
	defer cancelSync()

	if runOptionalSyncs {
		var syncWG sync.WaitGroup
		syncWG.Go(func() {
			if err := s.updateEnergyHistory(syncCtx, siteID, essSystem); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to sync energy history", slog.Any("error", err))
			}
		})
		syncWG.Go(func() {
			if err := s.updatePriceHistory(syncCtx, siteID, utility, false); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to update price history", slog.Any("error", err))
			}
		})
		if settings.Location != nil {
			loc := *settings.Location
			syncWG.Go(func() {
				if err := s.updateWeatherHistory(syncCtx, siteID, loc); err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to update weather history", slog.Any("error", err))
				}
			})
		}
		syncWG.Wait()
	}
	cancelSync()

	log.Ctx(ctx).DebugContext(ctx, "update: energy and weather history synced")

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
	notifData.currentPrice = currentPrice

	log.Ctx(ctx).DebugContext(ctx, "update: current price fetched", slog.Any("price", currentPrice))

	// merge utility mandatory VPP events and finalize VPP pricing
	vppInfo, err := utility.GetVPPInfo(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get utility VPP info", slog.Any("error", err))
	}
	status = s.mergeUtilityVPPEvents(ctx, status, vppInfo)
	notifData.status = status
	notifData.vppInfo = vppInfo

	// get History for Controller (Last 35 days from monthly summaries + today's/tomorrow's unsummarized data)
	siteLoc := resolveStatusLocation(settings, status)
	now := s.now().In(siteLoc)
	todayStart := truncateDay(now)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	historyStart := todayStart.AddDate(0, 0, -forecastHistoryDays)

	// Fetch existing summaries in-memory first to determine if backfill/update is needed
	existingSummaries, err := s.storage.GetHistorySummaries(ctx, siteID, historyStart, now)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get history summaries: %w", err)
	}
	if existingSummaries == nil {
		existingSummaries = []types.HistorySummary{}
	}

	var hasUpdateOrBackfill bool
	var newSummaries []types.HistorySummary

	latestEnergyDay, latestWeatherDay := getSummaryLatestDates(existingSummaries, siteLoc)
	if len(existingSummaries) == 0 || (latestEnergyDay.IsZero() && latestWeatherDay.IsZero()) {
		// Trigger initial backfill only when no summarized energy or weather exists yet
		newSummaries, err = s.backfillHistorySummariesWithExisting(ctx, siteID, now, settings.Location != nil, existingSummaries)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to backfill summaries", slog.Any("error", err))
		} else if len(newSummaries) > 0 {
			hasUpdateOrBackfill = true
		}
	} else {
		// Track archived energy and weather dates separately so weather-only months never
		// repeat full backfills while still backfilling late ESS energy across the 35-day window.
		needEnergy := latestEnergyDay.IsZero() || latestEnergyDay.Before(yesterdayStart)
		energyStart := historyStart
		if !latestEnergyDay.IsZero() {
			energyStart = latestEnergyDay.AddDate(0, 0, 1)
		}

		needWeather := settings.Location != nil && (latestWeatherDay.IsZero() || latestWeatherDay.Before(yesterdayStart))
		weatherStart := historyStart
		if !latestWeatherDay.IsZero() {
			weatherStart = latestWeatherDay.AddDate(0, 0, 1)
		}

		if (needEnergy && !energyStart.After(yesterdayStart)) || (needWeather && !weatherStart.After(yesterdayStart)) {
			newSummaries, err = s.syncHistorySummaryUpdate(
				ctx,
				siteID,
				energyStart,
				needEnergy,
				weatherStart,
				needWeather,
				now,
				existingSummaries,
			)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update history summary", slog.Any("error", err))
			} else if len(newSummaries) > 0 {
				hasUpdateOrBackfill = true
			}
		}
	}

	if hasUpdateOrBackfill {
		// Merge updated summaries in memory
		summaryMap := make(map[string]types.HistorySummary)
		for _, sm := range existingSummaries {
			if d := summaryMonthKey(sm, siteLoc); d != "" {
				summaryMap[d] = sm
			}
		}
		for _, sm := range newSummaries {
			if d := summaryMonthKey(sm, siteLoc); d != "" {
				summaryMap[d] = sm
			}
		}

		var merged []types.HistorySummary
		for _, sm := range summaryMap {
			merged = append(merged, sm)
		}
		slices.SortFunc(merged, func(a, b types.HistorySummary) int {
			return summarySortTime(a, siteLoc).Compare(summarySortTime(b, siteLoc))
		})
		existingSummaries = merged
	}

	energyHistory, weatherHistory, err := s.getCombinedHistory(ctx, siteID, settings, historyStart, now, existingSummaries)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get combined history", slog.Any("error", err))
		return nil, "failed to get history", fmt.Errorf("failed to get combined history: %w", err)
	}
	notifData.energyHistory = energyHistory
	notifData.weatherHistory = weatherHistory
	configuredReserveSOC := settings.Settings.GetMinBatterySOC(ctx, s.now(), siteLoc, currentPrice)

	// don't update if we're in emergency mode
	if status.EmergencyMode {
		log.Ctx(ctx).InfoContext(ctx, "update: emergency mode, ensuring grid charge is enabled")

		minSOC := int(math.Round(configuredReserveSOC))
		if _, err := essSystem.SetModes(ctx, types.BatteryModeChargeAny, types.SolarModeNoChange, types.ModesOptions{MinimumSOC: minSOC}); err != nil {
			if errors.Is(err, ess.ErrUnauthorized) {
				log.Ctx(ctx).WarnContext(ctx, "failed to set modes in emergency mode", slog.Any("error", err))
			} else {
				log.Ctx(ctx).ErrorContext(ctx, "failed to set modes in emergency mode", slog.Any("error", err))
			}
		}

		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			ReserveSOC:      configuredReserveSOC,
			Description:     "In emergency mode",
			Reason:          types.ActionReasonEmergencyMode,
			SystemStatus:    status,
			Fault:           true,
			CurrentPrice:    &currentPrice,
		}
		if err := s.insertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		s.handleNotifications(ctx, siteID, notifData)
		return nil, "emergency mode", nil
	}

	if len(status.Alarms) > 0 {
		log.Ctx(ctx).InfoContext(ctx, "update: alarms present", slog.Any("alarms", status.Alarms))
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			ReserveSOC:      configuredReserveSOC,
			Description:     fmt.Sprintf("%d alarms present", len(status.Alarms)),
			Reason:          types.ActionReasonHasAlarms,
			SystemStatus:    status,
			Fault:           true,
			CurrentPrice:    &currentPrice,
		}
		if err := s.insertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		// don't send notifications if alarms present
		// TODO: should we send an alarm notification when we detect something is wrong?
		return nil, "alarms present", nil
	}

	if status.GridUnavailable {
		log.Ctx(ctx).InfoContext(ctx, "update: grid unavailable")
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			ReserveSOC:      configuredReserveSOC,
			Description:     "Grid is unavailable",
			Reason:          types.ActionReasonGridUnavailable,
			SystemStatus:    status,
			Fault:           true,
			CurrentPrice:    &currentPrice,
		}
		if err := s.insertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		s.handleNotifications(ctx, siteID, notifData)
		return nil, "grid unavailable", nil
	}

	flatEnergyHistory := flattenDailyEnergyStats(energyHistory)

	// get Future Prices for controller and plan
	futurePrices, err := s.getFuturePrices(ctx, siteID, utility)
	if err != nil {
		return nil, "", err
	}
	notifData.futurePrices = futurePrices

	var planDecision controller.Decision
	var planErr error
	var hasPlan bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				planErr = fmt.Errorf("controller plan panicked: %v", r)
				log.Ctx(ctx).ErrorContext(ctx, "controller plan panicked", slog.Any("panic", r))
			}
		}()
		planDecision, _, planErr = s.controller.Plan(ctx, status, currentPrice, futurePrices, flatEnergyHistory, weatherHistory, settings.Settings, latestAction)
		if planErr == nil {
			hasPlan = true
			notifData.plan = planDecision.Action.Plan
		}
	}()

	// still generate a plan when paused and VPPActive
	if settings.Pause {
		log.Ctx(ctx).InfoContext(ctx, "update: paused")
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			ReserveSOC:      configuredReserveSOC,
			Description:     "Automation is paused",
			SystemStatus:    status,
			CurrentPrice:    &currentPrice,
			Paused:          true,
			BatteryMode:     types.BatteryModeLoad,
			SolarMode:       types.SolarModeAny,
		}
		if hasPlan {
			action.Plan = planDecision.Action.Plan
			action.SimulationParams = planDecision.SimulationParams
		} else if planErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to generate plan while paused", slog.Any("error", planErr))
		}
		if err := s.insertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert paused action", slog.Any("error", err))
		}
		// we are purposefully skipping notifications if things are paused
		return &action, "paused", nil
	}

	// don't update if we're in a VPP event
	if status.VPPActive {
		log.Ctx(ctx).InfoContext(ctx, "update: VPP event active")
		action := types.Action{
			Timestamp:       s.now(),
			SystemTimestamp: s.now().In(status.Timestamp.Location()),
			ReserveSOC:      configuredReserveSOC,
			Description:     "VPP event active",
			Reason:          types.ActionReasonVPPActive,
			SystemStatus:    status,
			CurrentPrice:    &currentPrice,
		}
		if hasPlan {
			action.Plan = planDecision.Action.Plan
			action.SimulationParams = planDecision.SimulationParams
		} else if planErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to generate plan during VPP active", slog.Any("error", planErr))
		}
		if err := s.insertAction(ctx, siteID, action); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
		}
		s.handleNotifications(ctx, siteID, notifData)
		return &action, "vpp event", nil
	}

	if err := s.canSetModes(settings); err != nil {
		return nil, "", err
	}

	if planErr != nil {
		return nil, "", fmt.Errorf("controller plan failed: %w", planErr)
	}
	decision := planDecision

	action := decision.Action
	action.SimulationParams = decision.SimulationParams
	if action.ReserveSOC == 0 {
		action.ReserveSOC = configuredReserveSOC
	}
	// Ensure timestamps match if not set
	if action.Timestamp.IsZero() {
		action.Timestamp = s.now()
	}
	if action.SystemTimestamp.IsZero() {
		action.SystemTimestamp = action.Timestamp.In(status.Timestamp.Location())
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
	minSOC := int(math.Round(configuredReserveSOC))
	if (action.BatteryMode == types.BatteryModeExport || action.BatteryMode == types.BatteryModeStandby) && action.ChargeToSOC > minSOC {
		minSOC = action.ChargeToSOC
	}
	var sched *types.TOUSchedule
	if settings.ManageTOUSchedules && hasPlan && action.Plan != nil {
		sched = controller.BuildTOUSchedule(action.Plan, s.now(), status.Timestamp.Location())
	}
	modesChanged, err := s.setESSModes(ctx, siteID, essSystem, action.BatteryMode, action.SolarMode, types.ModesOptions{
		ChargeToSOC: action.ChargeToSOC,
		MinimumSOC:  minSOC,
		Schedule:    sched,
	}, settings)
	if err != nil {
		if errors.Is(err, ess.ErrUnauthorized) || errors.Is(err, errESSRateLimited) {
			log.Ctx(ctx).WarnContext(ctx, "failed to set mode", slog.Any("error", err))
		} else {
			log.Ctx(ctx).ErrorContext(ctx, "failed to set mode", slog.Any("error", err))
		}
		action.Description += fmt.Sprintf(" (FAILED: %v)", err)
		action.Failed = true
		action.Error = err.Error()
	}
	if modesChanged {
		log.Ctx(ctx).InfoContext(ctx, "ess modes updated", slog.String("siteID", siteID))
	}
	if settings.DryRun {
		action.DryRun = true
	}

	// log Action
	if err := s.insertAction(ctx, siteID, action); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to insert action", slog.Any("error", err))
	}

	// evaluate & send notifications (e.g. morning summary, evening summary, anomalies)
	s.handleNotifications(ctx, siteID, notifData)

	return &action, "", nil
}

func (s *Server) insertAction(ctx context.Context, siteID string, action types.Action) error {
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return s.storage.InsertAction(insertCtx, siteID, action)
}

func (s *Server) getFuturePrices(ctx context.Context, siteID string, utility utility.Utility) ([]types.Price, error) {
	var futurePrices []types.Price
	if utility != nil {
		var err error
		futurePrices, err = utility.GetFuturePrices(ctx)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get future prices", slog.Any("error", err))
			// Continue with empty future prices
		}
	}

	nowTime := s.now()
	loc := nowTime.Location()
	if len(futurePrices) > 0 && futurePrices[0].TSStart.Location() != nil {
		loc = futurePrices[0].TSStart.Location()
	}
	nowInLoc := nowTime.In(loc)
	todayStart := truncateDay(nowInLoc)
	tomorrowEnd := todayStart.AddDate(0, 0, 2)

	priceEnd := func(p types.Price) time.Time {
		if !p.TSEnd.IsZero() && p.TSEnd.After(p.TSStart) && p.TSEnd.Sub(p.TSStart) <= 24*time.Hour {
			return p.TSEnd
		}
		return p.TSStart.Add(time.Hour)
	}

	sortPrices := func(prices []types.Price) {
		slices.SortStableFunc(prices, func(a, b types.Price) int {
			if a.TSStart.Equal(b.TSStart) {
				return a.TSEnd.Compare(b.TSEnd)
			}
			return a.TSStart.Compare(b.TSStart)
		})
	}
	sortPrices(futurePrices)

	hasAllTomorrow := len(futurePrices) > 0
	var missingCoverage time.Time
	nowHourStart := nowInLoc.Add(-time.Duration(nowInLoc.Minute())*time.Minute - time.Duration(nowInLoc.Second())*time.Second - time.Duration(nowInLoc.Nanosecond()))
	nextHourStart := nowHourStart.Add(time.Hour)
	if hasAllTomorrow {
		cursor := futurePrices[0].TSStart.In(loc)
		if cursor.After(nextHourStart) {
			cursor = nextHourStart
		}
		for _, p := range futurePrices {
			pEnd := priceEnd(p).In(loc)
			if !pEnd.After(cursor) {
				continue
			}
			// if its not contiguous then we don't have all tomorrow
			if p.TSStart.In(loc).After(cursor) {
				hasAllTomorrow = false
				missingCoverage = cursor
				break
			}
			cursor = pEnd
			if !cursor.Before(tomorrowEnd) {
				break
			}
		}
		// if we didn't make it to the end of tomorrow then we don't have all tomorrow
		if hasAllTomorrow && cursor.Before(tomorrowEnd) {
			hasAllTomorrow = false
			missingCoverage = cursor
		}
	}

	if !hasAllTomorrow {
		log.Ctx(ctx).InfoContext(ctx, "less than all tomorrow prices available, estimating missing hours using 168h history",
			slog.Int("existingFuturePricesCount", len(futurePrices)),
			slog.Time("missingCoverage", missingCoverage),
		)
		// Use 7 days (168h) of historical prices plus today's already-known future prices
		// to compute a more robust median profile for missing tomorrow intervals
		// (e.g. before PJM publishes tomorrow's Day-Ahead prices around 1:30 PM ET,
		// or before Octopus publishes tomorrow's Agile rates around 4:00 PM UK time).
		histStart := nowTime.Add(-168 * time.Hour)
		histPrices, histErr := s.storage.GetPriceHistory(ctx, siteID, histStart, nowTime)
		if histErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get historical prices", slog.Any("error", histErr))
		} else if len(histPrices) == 0 {
			log.Ctx(ctx).WarnContext(ctx, "no historical prices found for the last 168 hours")
		} else {
			if len(futurePrices) == 0 && histPrices[0].TSStart.Location() != nil {
				loc = histPrices[0].TSStart.Location()
				nowInLoc = nowTime.In(loc)
				todayStart = truncateDay(nowInLoc)
				tomorrowEnd = todayStart.AddDate(0, 0, 2)
				nowHourStart = nowInLoc.Add(-time.Duration(nowInLoc.Minute())*time.Minute - time.Duration(nowInLoc.Second())*time.Second - time.Duration(nowInLoc.Nanosecond()))
				nextHourStart = nowHourStart.Add(time.Hour)
			}

			allCandidates := make([]types.Price, 0, len(histPrices)+len(futurePrices))
			allCandidates = append(allCandidates, histPrices...)
			allCandidates = append(allCandidates, futurePrices...)

			var overallDollars []float64
			var overallGenCredits []float64
			for _, p := range allCandidates {
				overallDollars = append(overallDollars, p.DollarsPerKWH)
				overallGenCredits = append(overallGenCredits, p.GenerationCreditDollarsPerKWH)
			}
			overallMedDollars := computeMedian(overallDollars)
			overallMedGenCredits := computeMedian(overallGenCredits)
			overallTemplate := findClosestPrice(allCandidates, overallMedDollars, overallMedGenCredits)
			overallDur := priceEnd(overallTemplate).Sub(overallTemplate.TSStart)
			if overallDur <= 0 {
				overallDur = time.Hour
			}

			// alignCandidate projects a candidate price's time-of-day window onto target's date in loc.
			alignCandidate := func(cp types.Price, target time.Time) (time.Time, time.Time) {
				tLoc := target.In(loc)
				cpStart := cp.TSStart.In(loc)
				cpDur := priceEnd(cp).Sub(cp.TSStart)
				if cpDur <= 0 {
					cpDur = time.Hour
				}
				alignedStart := time.Date(tLoc.Year(), tLoc.Month(), tLoc.Day(), cpStart.Hour(), cpStart.Minute(), cpStart.Second(), cpStart.Nanosecond(), loc)
				// Skip candidates whose wall-clock time falls in a nonexistent spring-forward DST gap on target's date.
				if alignedStart.Hour() != cpStart.Hour() || alignedStart.Minute() != cpStart.Minute() {
					return time.Time{}, time.Time{}
				}
				// On fall-back DST days where a wall-clock hour repeats, align to the second occurrence when target is in the second pass.
				if plus1h := alignedStart.Add(time.Hour); plus1h.Hour() == cpStart.Hour() && plus1h.Minute() == cpStart.Minute() && !tLoc.Before(plus1h) {
					alignedStart = plus1h
				}
				// Handle intervals that cross midnight (e.g. 23:30–00:30 when tLoc is 00:15)
				// by checking if starting on the previous calendar day extends past tLoc.
				if alignedStart.After(tLoc) {
					prevStart := alignedStart.AddDate(0, 0, -1)
					if prevStart.Add(cpDur).After(tLoc) {
						alignedStart = prevStart
					}
				}
				return alignedStart, alignedStart.Add(cpDur)
			}

			cursor := nowHourStart
			if len(futurePrices) > 0 {
				firstStart := futurePrices[0].TSStart.In(loc)
				if !firstStart.After(nextHourStart) {
					cursor = firstStart
				} else {
					cursor = nextHourStart
				}
			}

			var estimatedPrices []types.Price
			for cursor.Before(tomorrowEnd) {
				// Advance past any interval already covered by futurePrices.
				covered := false
				gapEnd := tomorrowEnd
				for _, fp := range futurePrices {
					fpStart := fp.TSStart.In(loc)
					fpEnd := priceEnd(fp).In(loc)
					if !fpStart.After(cursor) && fpEnd.After(cursor) {
						cursor = fpEnd
						covered = true
						break
					}
					if fpStart.After(cursor) && fpStart.Before(gapEnd) {
						gapEnd = fpStart
					}
				}
				if covered {
					continue
				}

				// Find all historical/known prices whose time-of-day overlaps cursor (the previous price's TSEnd).
				var matching []types.Price
				var dollarVals []float64
				var genCreditVals []float64
				var minEnd time.Time
				var minNextStart time.Time
				for _, cp := range allCandidates {
					aStart, aEnd := alignCandidate(cp, cursor)
					if aStart.IsZero() {
						continue
					}
					if !cursor.Before(aStart) && cursor.Before(aEnd) {
						matching = append(matching, cp)
						dollarVals = append(dollarVals, cp.DollarsPerKWH)
						if cp.SeparateGenerationCredit {
							genCreditVals = append(genCreditVals, cp.GenerationCreditDollarsPerKWH)
						}
						if minEnd.IsZero() || aEnd.Before(minEnd) {
							minEnd = aEnd
						}
					} else if aStart.After(cursor) {
						// Track the earliest upcoming candidate start time after cursor so a fallback
						// interval doesn't overshoot a sub-hour historical price starting later in the hour.
						if minNextStart.IsZero() || aStart.Before(minNextStart) {
							minNextStart = aStart
						}
					}
				}

				var est types.Price
				var estEnd time.Time
				if len(matching) > 0 {
					medDollars := computeMedian(dollarVals)
					medGenCredits := computeMedian(genCreditVals)
					est = findClosestPrice(matching, medDollars, medGenCredits)
					estEnd = minEnd
				} else {
					est = overallTemplate
					estEnd = cursor.Add(overallDur)
					// Clamp the fallback interval to the next candidate's start time (e.g. if cursor is
					// 07:00 with no history for 07:00–07:30, stop at 07:30 so 07:30–08:00 can match next).
					if !minNextStart.IsZero() && estEnd.After(minNextStart) {
						estEnd = minNextStart
					}
				}

				if estEnd.After(gapEnd) {
					estEnd = gapEnd
				}
				if !estEnd.After(cursor) {
					estEnd = cursor.Add(time.Hour)
					if estEnd.After(gapEnd) {
						estEnd = gapEnd
					}
				}

				est.TSStart = cursor
				est.TSEnd = estEnd
				estimatedPrices = append(estimatedPrices, est)
				cursor = estEnd
			}

			futurePrices = append(futurePrices, estimatedPrices...)
			sortPrices(futurePrices)
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
		return nil, fmt.Errorf("insufficient future pricing data")
	}
	return futurePrices, nil
}

func computeMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	slices.Sort(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}

func findClosestPrice(prices []types.Price, targetDollars, targetGenCredit float64) types.Price {
	if len(prices) == 0 {
		return types.Price{}
	}
	best := prices[0]
	bestDiff := math.Abs(best.DollarsPerKWH - targetDollars)
	bestGenDiff := math.Abs(best.GenerationCreditDollarsPerKWH - targetGenCredit)
	for _, p := range prices[1:] {
		diff := math.Abs(p.DollarsPerKWH - targetDollars)
		genDiff := math.Abs(p.GenerationCreditDollarsPerKWH - targetGenCredit)
		if diff < bestDiff || (diff == bestDiff && genDiff <= bestGenDiff) {
			best = p
			bestDiff = diff
			bestGenDiff = genDiff
		}
	}
	if best.SeparateGenerationCredit {
		bestGen := prices[0].GenerationCreditDollarsPerKWH
		bestGenDiff = math.Abs(bestGen - targetGenCredit)
		for _, p := range prices[1:] {
			genDiff := math.Abs(p.GenerationCreditDollarsPerKWH - targetGenCredit)
			if genDiff <= bestGenDiff {
				bestGen = p.GenerationCreditDollarsPerKWH
				bestGenDiff = genDiff
			}
		}
		best.GenerationCreditDollarsPerKWH = bestGen
	}
	return best
}

func (s *Server) getLatestAction(ctx context.Context, siteID string) *types.Action {
	latestAction, err := s.storage.GetLatestAction(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get latest action from storage, using nil", slog.Any("error", err))
		return nil
	}
	if latestAction != nil && latestAction.SystemStatus.TimeLocation != "" {
		if loc, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
			latestAction.Timestamp = latestAction.Timestamp.In(loc)
			latestAction.SystemTimestamp = latestAction.SystemTimestamp.In(loc)
			latestAction.SystemStatus.Timestamp = latestAction.SystemStatus.Timestamp.In(loc)
		}
	}
	return latestAction
}

func (s *Server) mergeUtilityVPPEvents(ctx context.Context, status types.SystemStatus, vppInfo types.UtilityVPPInfo) types.SystemStatus {
	nowTime := s.now()
	limitTime := nowTime.Add(24 * time.Hour)

	for _, period := range vppInfo.Mandatory {
		var inEvent bool
		var eventStart time.Time

		scanStart := nowTime.Truncate(time.Hour).Add(-24 * time.Hour)
		scanEnd := nowTime.Add(48 * time.Hour).Truncate(time.Hour)

		var lastEnd time.Time
		for h := scanStart; !h.After(scanEnd); h = h.Add(time.Hour) {
			contains, startEnd, err := period.Contains(h)
			if err != nil {
				contains = false
			}
			if contains {
				if !inEvent {
					inEvent = true
					eventStart = startEnd.Start
				}
				lastEnd = startEnd.End
			} else if inEvent {
				eventEnd := lastEnd
				if eventEnd.IsZero() {
					eventEnd = h
				}

				if !eventStart.Before(nowTime) && !eventStart.After(limitTime) {
					candidate := types.VPPEvent{
						Description: "Mandatory Utility VPP Event",
						TSStart:     eventStart,
						TSEnd:       eventEnd,
						VPPSoc:      period.ReserveSOC,
						Mandatory:   true,
					}

					var overlapsVPP bool
					var overlapEvent types.VPPEvent
					for i, existing := range status.VPPEvents {
						if candidate.TSStart.Before(existing.TSEnd) && existing.TSStart.Before(candidate.TSEnd) {
							overlapsVPP = true
							overlapEvent = existing
							status.VPPEvents[i].Mandatory = true
							status.VPPEvents[i].DollarsPerKWH = 0
							break
						}
					}
					if overlapsVPP {
						// vpp events returned from ess override utility ones
						log.Ctx(ctx).InfoContext(ctx, "ignoring mandatory utility VPP event because it overlaps with an existing ESS VPP event",
							slog.Any("utilityEvent", candidate),
							slog.Any("existingEvent", overlapEvent),
						)
						inEvent = false
						continue
					}

					var overlapsStorm bool
					var overlapStorm types.Storm
					for _, storm := range status.Storms {
						if candidate.TSStart.Before(storm.TSEnd) && storm.TSStart.Before(candidate.TSEnd) {
							overlapsStorm = true
							overlapStorm = storm
							break
						}
					}
					if overlapsStorm {
						// storms override vpp events even if they're mandatory
						log.Ctx(ctx).InfoContext(ctx, "ignoring mandatory utility VPP event because it overlaps with a storm warning",
							slog.Any("utilityEvent", candidate),
							slog.Any("storm", overlapStorm),
						)
						inEvent = false
						continue
					}

					status.VPPEvents = append(status.VPPEvents, candidate)
					log.Ctx(ctx).DebugContext(ctx,
						"added utility mandatory VPP event to SystemStatus",
						slog.Any("event", candidate),
					)
				}
				inEvent = false
			}
		}
	}

	// TODO: Find a better way to determine the export compensation price per VPP program (e.g. via utility API query or tariff schedule lookup)
	for i := range status.VPPEvents {
		if !status.VPPEvents[i].Mandatory && status.VPPEvents[i].DollarsPerKWH == 0 {
			status.VPPEvents[i].DollarsPerKWH = 2.0
		}
	}

	slices.SortFunc(status.VPPEvents, func(a, b types.VPPEvent) int {
		return a.TSStart.Compare(b.TSStart)
	})
	return status
}

func (s *Server) updatePriceHistory(ctx context.Context, siteID string, provider utility.Utility, refreshNow bool) error {
	lastPriceTime, lastVersion, err := s.storage.GetLatestPriceHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest price history time: %w", err)
	}

	now := s.now()
	// Backfill 8 days (7 full local days plus 1-day buffer for UTC vs. local timezone offsets)
	eightDaysAgo := now.AddDate(0, 0, -8)
	syncStart := truncateDay(eightDaysAgo)

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

	now := s.now().In(timeLoc)
	todayMidnight := truncateDay(now)
	syncStart := todayMidnight.AddDate(0, 0, -14) // Default to 14 days ago backfill
	fetchEnd := todayMidnight.AddDate(0, 0, 2)

	// Determine the latest passed local slot.
	// Since the current time might just have rolled past midnight, the latest passed slot
	// could be a slot from yesterday (e.g. at 22:00 local yesterday). To handle this, we check
	// the scheduled slot hours for both yesterday (dayOffset = -1) and today (dayOffset = 0).
	hours := []int{0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 20, 22}
	var lastPassedSlot time.Time
	for _, dayOffset := range []int{-1, 0} {
		// targetDay represents the local date (yesterday or today) we are evaluating.
		targetDay := now.AddDate(0, 0, dayOffset)
		for _, hr := range hours {
			// slotTime is the absolute timestamp of the scheduled slot on targetDay in timeLoc.
			slotTime := time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day(), hr, 0, 0, 0, timeLoc)
			// Since hours and days are sorted in ascending order, the first slot we find that is
			// in the future means all subsequent slots will also be in the future.
			if slotTime.After(now) {
				break
			}
			// Keep the most recent slot that has passed.
			if lastPassedSlot.IsZero() || slotTime.After(lastPassedSlot) {
				lastPassedSlot = slotTime
			}
		}
	}

	isNormalUpdate := !lastWeatherTime.IsZero() && lastVersion >= types.CurrentWeatherVersion && lastWeatherTime.After(syncStart)

	var checkSignificantChanges bool
	if isNormalUpdate {
		if !lastWeatherUpdated.Before(lastPassedSlot) {
			return nil
		}
		slotHour := lastPassedSlot.In(timeLoc).Hour()
		if slotHour == 0 || (!lastWeatherUpdated.IsZero() && lastWeatherUpdated.Before(todayMidnight)) {
			// At midnight (or on a post-midnight recovery slot if 00:00 was missed),
			// include yesterday so assimilated evening temperatures are stored before archival.
			syncStart = todayMidnight.AddDate(0, 0, -1)
		} else {
			syncStart = todayMidnight
		}
		// The end date is exclusive so we go to the start of the day AFTER tomorrow
		// to make sure that we update both today and tomorrow together.
		fetchEnd = todayMidnight.AddDate(0, 0, 2)
		// Check for significant irradiance changes before evening just for debugging
		// while skipping nighttime reads.
		checkSignificantChanges = slotHour < 17
	} else if !lastWeatherTime.IsZero() && lastVersion < types.CurrentWeatherVersion {
		log.Ctx(ctx).InfoContext(
			ctx,
			"backfilling weather history due to version mismatch",
			slog.Int("lastVersion", lastVersion),
			slog.Int("currentVersion", types.CurrentWeatherVersion),
		)
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing weather history", slog.Any("since", syncStart), slog.Any("to", fetchEnd))

	newWeathers, err := s.weather.Forecast(ctx, loc, syncStart, fetchEnd)
	if err != nil && ctx.Err() == nil {
		delay := s.getWeatherRetryDelay()
		log.Ctx(ctx).WarnContext(ctx, "weather forecast failed, retrying", slog.Any("error", err), slog.Duration("delay", delay))
		select {
		case <-time.After(delay):
			newWeathers, err = s.weather.Forecast(ctx, loc, syncStart, fetchEnd)
		case <-ctx.Done():
			return fmt.Errorf("failed to get weather history: %w", ctx.Err())
		}
	}
	if err != nil {
		return fmt.Errorf("failed to get weather history: %w", err)
	}

	if len(newWeathers) == 0 {
		log.Ctx(ctx).WarnContext(ctx, "no weather data found for the given time range", slog.Time("syncStart", syncStart), slog.Time("fetchEnd", fetchEnd))
		return nil
	}

	if checkSignificantChanges {
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

							diff := newHw.GTI - oldHw.GTI
							if diff < 0 {
								diff = -diff
							}

							if diff > 50 {
								if oldHw.GTI == 0 || (oldHw.GTI < 50 && newHw.GTI >= 90) {
									anySignificantChange = true
								} else {
									ratio := newHw.GTI / oldHw.GTI
									if ratio > 1.50 || ratio < 0.50 {
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
						"weather forecast changed significantly after refresh",
						slog.Any("hours", hours),
						slog.Any("oldGti", oldGTIs),
						slog.Any("newGti", newGTIs),
					)
				}
			}
		}
	}

	if err := s.storage.UpsertWeather(ctx, siteID, newWeathers, types.CurrentWeatherVersion); err != nil {
		return fmt.Errorf("failed to upsert weather: %w", err)
	}

	return nil
}

func (s *Server) updateEnergyHistory(ctx context.Context, siteID string, essSystem ess.System) error {
	lastEnergyTime, lastVersion, err := s.storage.GetLatestEnergyHistoryTime(ctx, siteID)
	if err != nil {
		return fmt.Errorf("failed to get latest energy history time: %w", err)
	}

	now := s.now()
	fourteenDaysAgo := now.Add(-14 * 24 * time.Hour)
	syncStart := time.Date(fourteenDaysAgo.Year(), fourteenDaysAgo.Month(), fourteenDaysAgo.Day(), 0, 0, 0, 0, fourteenDaysAgo.Location())

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

	// we sync up until the current hour
	syncEnd := now.Truncate(time.Hour)
	if !syncStart.Before(syncEnd) {
		return nil
	}

	for windowStart := syncStart; windowStart.Before(syncEnd); {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("failed to get energy history: %w", err)
		}

		nextWindowStart := truncateDay(windowStart).AddDate(0, 0, energyBackfillWindowDays)
		windowEnd := nextWindowStart.Add(-time.Hour)
		if windowEnd.After(syncEnd) || !windowStart.Before(windowEnd) {
			windowEnd = syncEnd
			nextWindowStart = syncEnd
		}

		log.Ctx(ctx).DebugContext(ctx, "syncing energy history", slog.Any("since", windowStart), slog.Any("to", windowEnd))

		newHistory, err := essSystem.GetEnergyHistory(ctx, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("failed to get energy history: %w", err)
		}

		if len(newHistory) > 0 {
			if err := s.storage.UpsertEnergyHistories(ctx, siteID, newHistory, types.CurrentEnergyStatsVersion); err != nil {
				return fmt.Errorf("failed to upsert energy histories: %w", err)
			}
		}

		windowStart = nextWindowStart
	}
	return nil
}

func (s *Server) canSetModes(settings settingsWithVersion) error {
	failures := settings.ESSAuthStatus.ConsecutiveSetFailures
	if failures > 1 {
		backoff := getESSBackoff(failures)
		timeLeft := backoff - s.now().Sub(settings.ESSAuthStatus.LastAttempt)
		if timeLeft > 0 {
			timeLeft = max(timeLeft.Round(time.Second), time.Second)
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
	solarMode types.SolarMode,
	opts types.ModesOptions,
	settings settingsWithVersion,
) (bool, error) {
	if err := s.canSetModes(settings); err != nil {
		return false, err
	}

	if solarMode == types.SolarModeNoChange {
		solarMode = types.SolarModeAny
	}

	changed, err := essSystem.SetModes(ctx, batteryMode, solarMode, opts)
	if err != nil {
		if errors.Is(err, ess.ErrUnauthorized) {
			settings.ESSAuthStatus.ConsecutiveSetFailures = nextESSFailureCount(settings.ESSAuthStatus.ConsecutiveSetFailures, err)
			settings.ESSAuthStatus.LastAttempt = s.now().UTC()
			if newUpdatedAt, dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version, settings.updatedAt); dbErr != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status after set modes failure", slog.Any("error", dbErr))
			} else {
				settings.updatedAt = newUpdatedAt
			}
		}
		return false, err
	}

	if changed && settings.ESSAuthStatus.ConsecutiveSetFailures > 0 {
		settings.ESSAuthStatus.ConsecutiveSetFailures = 0
		settings.ESSAuthStatus.LastAttempt = s.now().UTC()
		if newUpdatedAt, dbErr := s.storage.SetSettings(ctx, siteID, settings.Settings, settings.version, settings.updatedAt); dbErr != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update settings auth status after set modes success", slog.Any("error", dbErr))
		} else {
			settings.updatedAt = newUpdatedAt
		}
	}
	return changed, nil
}

func resolveStatusLocation(settings settingsWithVersion, status types.SystemStatus) *time.Location {
	if status.TimeLocation != "" {
		if l, err := time.LoadLocation(status.TimeLocation); err == nil {
			return l
		}
	}
	if !status.Timestamp.IsZero() && status.Timestamp.Location() != nil && status.Timestamp.Location() != time.UTC {
		return status.Timestamp.Location()
	}
	if settings.Location != nil && settings.Location.TimeZone != "" {
		if l, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			return l
		}
	}
	if !status.Timestamp.IsZero() && status.Timestamp.Location() != nil {
		return status.Timestamp.Location()
	}
	return time.UTC
}

func summaryMonthKey(sm types.HistorySummary, fallbackLoc *time.Location) string {
	if !sm.TSMonthStart.IsZero() {
		return sm.TSMonthStart.Format("2006-01")
	}
	if fallbackLoc == nil {
		fallbackLoc = time.UTC
	}
	if len(sm.Energy) > 0 && !sm.Energy[0].TSDayStart.IsZero() {
		loc := fallbackLoc
		if sm.Energy[0].TimeLocation != "" {
			if l, err := time.LoadLocation(sm.Energy[0].TimeLocation); err == nil {
				loc = l
			}
		}
		return sm.Energy[0].TSDayStart.In(loc).Format("2006-01")
	}
	if len(sm.Weather) > 0 && !sm.Weather[0].TSDayStart.IsZero() {
		loc := fallbackLoc
		if sm.Weather[0].TimeLocation != "" {
			if l, err := time.LoadLocation(sm.Weather[0].TimeLocation); err == nil {
				loc = l
			}
		}
		return sm.Weather[0].TSDayStart.In(loc).Format("2006-01")
	}
	return ""
}

func summarySortTime(sm types.HistorySummary, fallbackLoc *time.Location) time.Time {
	if !sm.TSMonthStart.IsZero() {
		return sm.TSMonthStart
	}
	if len(sm.Energy) > 0 && !sm.Energy[0].TSDayStart.IsZero() {
		return sm.Energy[0].TSDayStart
	}
	if len(sm.Weather) > 0 && !sm.Weather[0].TSDayStart.IsZero() {
		return sm.Weather[0].TSDayStart
	}
	if key := summaryMonthKey(sm, fallbackLoc); key != "" {
		if t, err := time.Parse("2006-01", key); err == nil {
			return t
		}
	}
	return time.Time{}
}

// getCombinedHistory retrieves weather and energy histories by querying the monthly summaries in Firestore,
// and then merges them in memory with unsummarized energy data and unsummarized weather data.
// It returns a chronological slice of daily energy stats and a chronological slice of weather documents
// trimmed to historyStart.
func (s *Server) getCombinedHistory(
	ctx context.Context,
	siteID string,
	settings settingsWithVersion,
	historyStart, now time.Time,
	summaries []types.HistorySummary,
) ([]types.DailyEnergyStats, []types.Weather, error) {
	locCache := make(map[string]*time.Location)
	getLocation := func(tz string, fallback *time.Location) *time.Location {
		if tz == "" {
			return fallback
		}
		if l, ok := locCache[tz]; ok && l != nil {
			return l
		}
		if l, err := time.LoadLocation(tz); err == nil {
			locCache[tz] = l
			return l
		}
		return fallback
	}

	siteLoc := now.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if siteLoc == time.UTC && settings.Location != nil && settings.Location.TimeZone != "" {
		siteLoc = getLocation(settings.Location.TimeZone, siteLoc)
	}

	nowInSite := now.In(siteLoc)
	historyStartInSite := historyStart.In(siteLoc)

	// 1. Fetch monthly summaries that overlap with the range [historyStart, now)
	// if not passed in-memory.
	if summaries == nil {
		var err error
		summaries, err = s.storage.GetHistorySummaries(ctx, siteID, historyStartInSite, nowInSite)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get history summaries: %w", err)
		}
	}

	if siteLoc == time.UTC {
		for _, summary := range summaries {
			for _, day := range summary.Energy {
				if day.TimeLocation != "" {
					if l := getLocation(day.TimeLocation, time.UTC); l != time.UTC {
						siteLoc = l
						break
					}
				}
			}
			if siteLoc != time.UTC {
				nowInSite = now.In(siteLoc)
				historyStartInSite = historyStart.In(siteLoc)
				break
			}
		}
	}

	todayStart := truncateDay(nowInSite)
	historyStartDay := truncateDay(historyStartInSite)

	var combinedEnergy []types.DailyEnergyStats
	var combinedWeather []types.Weather

	var latestSummaryEnergyDay time.Time
	var latestSummaryWeatherDay time.Time

	energyDayStart := func(day types.DailyEnergyStats) time.Time {
		if !day.TSDayStart.IsZero() {
			return day.TSDayStart
		}
		if len(day.Hourly) > 0 {
			return day.Hourly[0].TSHourStart
		}
		return time.Time{}
	}
	weatherDayStart := func(w types.Weather) time.Time {
		if !w.TSDayStart.IsZero() {
			return w.TSDayStart
		}
		if len(w.ForecastHours) > 0 {
			return w.ForecastHours[0].TSHourStart
		}
		return time.Time{}
	}
	normalizeDay := func(ts time.Time, tz string) time.Time {
		dLoc := getLocation(tz, siteLoc)
		d := ts.In(dLoc)
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, siteLoc)
	}

	for _, summary := range summaries {
		// Filter and append energy stats from the summaries
		for _, day := range summary.Energy {
			ts := energyDayStart(day)
			if ts.IsZero() {
				continue
			}
			dDayNorm := normalizeDay(ts, day.TimeLocation)
			if dDayNorm.Before(todayStart) {
				if latestSummaryEnergyDay.IsZero() || dDayNorm.After(latestSummaryEnergyDay) {
					latestSummaryEnergyDay = dDayNorm
				}
				if !dDayNorm.Before(historyStartDay) {
					combinedEnergy = append(combinedEnergy, day)
				}
			}
		}

		// Filter and append weather data from the summaries
		for _, w := range summary.Weather {
			ts := weatherDayStart(w)
			if ts.IsZero() {
				continue
			}
			wDayNorm := normalizeDay(ts, w.TimeLocation)
			if wDayNorm.Before(todayStart) {
				if latestSummaryWeatherDay.IsZero() || wDayNorm.After(latestSummaryWeatherDay) {
					latestSummaryWeatherDay = wDayNorm
				}
				if !wDayNorm.Before(historyStartDay) {
					combinedWeather = append(combinedWeather, w)
				}
			}
		}
	}

	// Only fall back weather summary date from energy when the site actually has a configured location.
	// Never fall back energy summary date from weather so weather-only months still fetch unsummarized energy.
	if settings.Location != nil && latestSummaryWeatherDay.IsZero() {
		latestSummaryWeatherDay = latestSummaryEnergyDay
	}

	// Callers like handleHistoryEnergy may pass targetDate+1d as now, so cap
	// expectedLatestDay at the real expected latest day. Defer behind-schedule warnings
	// for yesterday until 6 hours after local midnight to match syncHistorySummaryRange.
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	realNowLocal := s.now().In(siteLoc)
	realTodayStart := truncateDay(realNowLocal)
	realExpectedLatestDay := realTodayStart.AddDate(0, 0, -1)
	if realNowLocal.Before(realTodayStart.Add(6 * time.Hour)) {
		realExpectedLatestDay = realTodayStart.AddDate(0, 0, -2)
	}
	expectedLatestDay := yesterdayStart
	if realExpectedLatestDay.Before(expectedLatestDay) {
		expectedLatestDay = realExpectedLatestDay
	}

	// Determine start range for fetching unsummarized energy history
	energyFetchStart := historyStartInSite
	if !latestSummaryEnergyDay.IsZero() {
		energyFetchStart = latestSummaryEnergyDay.AddDate(0, 0, 1)
	}
	if energyFetchStart.Before(historyStartInSite) {
		energyFetchStart = historyStartInSite
	}
	if energyFetchStart.After(todayStart) {
		energyFetchStart = todayStart
	}

	// 2. Fetch unsummarized energy history.
	// We query the range [energyFetchStart, todayStart + 24 hours).
	unsummarizedEnergy, err := s.storage.GetEnergyHistory(ctx, siteID, energyFetchStart, todayStart.AddDate(0, 0, 1))
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get unsummarized energy history", slog.Any("error", err))
	} else {
		var hasBehindUnsummarizedEnergy bool
		for _, day := range unsummarizedEnergy {
			ts := energyDayStart(day)
			if ts.IsZero() {
				continue
			}
			dDayNorm := normalizeDay(ts, day.TimeLocation)
			if !dDayNorm.After(expectedLatestDay) {
				hasBehindUnsummarizedEnergy = true
			}
			if !dDayNorm.Before(historyStartDay) {
				combinedEnergy = append(combinedEnergy, day)
			}
		}
		if !latestSummaryEnergyDay.IsZero() && latestSummaryEnergyDay.Before(expectedLatestDay) && hasBehindUnsummarizedEnergy {
			log.Ctx(ctx).WarnContext(ctx, "energy history summary is behind, fetching missing days from database",
				slog.String("siteID", siteID),
				slog.Time("latestSummaryDay", latestSummaryEnergyDay),
				slog.Time("expectedLatestDay", expectedLatestDay),
			)
		}
	}

	// 3. Fetch unsummarized weather data only if location is configured.
	// We query the range [weatherFetchStart, todayStart + 48 hours) to include both days.
	if settings.Location != nil {
		weatherFetchStart := historyStartInSite
		if !latestSummaryWeatherDay.IsZero() {
			weatherFetchStart = latestSummaryWeatherDay.AddDate(0, 0, 1)
		}
		if weatherFetchStart.Before(historyStartInSite) {
			weatherFetchStart = historyStartInSite
		}
		if weatherFetchStart.After(todayStart) {
			weatherFetchStart = todayStart
		}

		unsummarizedWeather, err := s.storage.GetWeather(ctx, siteID, weatherFetchStart, todayStart.AddDate(0, 0, 2))
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get unsummarized weather", slog.Any("error", err))
		} else {
			var hasBehindUnsummarizedWeather bool
			for _, w := range unsummarizedWeather {
				ts := weatherDayStart(w)
				if ts.IsZero() {
					continue
				}
				wDayNorm := normalizeDay(ts, w.TimeLocation)
				if !wDayNorm.After(expectedLatestDay) {
					hasBehindUnsummarizedWeather = true
				}
				if !wDayNorm.Before(historyStartDay) {
					combinedWeather = append(combinedWeather, w)
				}
			}
			if !latestSummaryWeatherDay.IsZero() && latestSummaryWeatherDay.Before(expectedLatestDay) && hasBehindUnsummarizedWeather {
				log.Ctx(ctx).WarnContext(ctx, "weather history summary is behind, fetching missing days from database",
					slog.String("siteID", siteID),
					slog.Time("latestSummaryDay", latestSummaryWeatherDay),
					slog.Time("expectedLatestDay", expectedLatestDay),
				)
			}
		}
	}

	// Sort energy stats and weather chronologically
	slices.SortFunc(combinedEnergy, func(a, b types.DailyEnergyStats) int {
		return energyDayStart(a).Compare(energyDayStart(b))
	})
	slices.SortFunc(combinedWeather, func(a, b types.Weather) int {
		return weatherDayStart(a).Compare(weatherDayStart(b))
	})

	// Deduplicate energy stats by local date
	if len(combinedEnergy) > 0 {
		seen := make(map[string]int)
		var deduped []types.DailyEnergyStats
		for _, day := range combinedEnergy {
			dLoc := getLocation(day.TimeLocation, siteLoc)
			dateStr := energyDayStart(day).In(dLoc).Format("2006-01-02")
			if idx, ok := seen[dateStr]; ok {
				if len(day.Hourly) >= len(deduped[idx].Hourly) {
					deduped[idx] = day
				}
			} else {
				seen[dateStr] = len(deduped)
				deduped = append(deduped, day)
			}
		}
		combinedEnergy = deduped
	}

	// Deduplicate weather by local date
	if len(combinedWeather) > 0 {
		seen := make(map[string]int)
		var deduped []types.Weather
		for _, w := range combinedWeather {
			wLoc := getLocation(w.TimeLocation, siteLoc)
			dateStr := weatherDayStart(w).In(wLoc).Format("2006-01-02")
			if idx, ok := seen[dateStr]; ok {
				if len(w.ForecastHours) >= len(deduped[idx].ForecastHours) {
					deduped[idx] = w
				}
			} else {
				seen[dateStr] = len(deduped)
				deduped = append(deduped, w)
			}
		}
		combinedWeather = deduped
	}

	return combinedEnergy, combinedWeather, nil
}

// getSummaryLatestDates returns the latest archived energy and weather calendar days
// (normalized to local midnight in loc) across the slice of HistorySummary.
func getSummaryLatestDates(summaries []types.HistorySummary, loc *time.Location) (latestEnergy, latestWeather time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	locCache := make(map[string]*time.Location)
	normalize := func(ts time.Time, tz string) time.Time {
		dLoc := loc
		if tz != "" {
			if cached, ok := locCache[tz]; ok && cached != nil {
				dLoc = cached
			} else if loaded, err := time.LoadLocation(tz); err == nil {
				locCache[tz] = loaded
				dLoc = loaded
			}
		}
		d := ts.In(dLoc)
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	}

	for _, s := range summaries {
		for _, day := range s.Energy {
			if day.TSDayStart.IsZero() {
				continue
			}
			dNorm := normalize(day.TSDayStart, day.TimeLocation)
			if latestEnergy.IsZero() || dNorm.After(latestEnergy) {
				latestEnergy = dNorm
			}
		}
		for _, w := range s.Weather {
			if w.TSDayStart.IsZero() {
				continue
			}
			wNorm := normalize(w.TSDayStart, w.TimeLocation)
			if latestWeather.IsZero() || wNorm.After(latestWeather) {
				latestWeather = wNorm
			}
		}
	}
	return latestEnergy, latestWeather
}

func (s *Server) backfillStartForTime(now time.Time) time.Time {
	loc := now.Location()
	todayStart := truncateDay(now)
	if now.Day() < 15 {
		prevMonth := todayStart.AddDate(0, -1, 0)
		return time.Date(prevMonth.Year(), prevMonth.Month(), 1, 0, 0, 0, 0, loc)
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
}

// backfillHistorySummaries compiles and stores historical summaries for the current month,
// and optionally the previous month if today is less than 15 days into the current month.
func (s *Server) backfillHistorySummaries(ctx context.Context, siteID string, now time.Time) ([]types.HistorySummary, error) {
	backfillStart := s.backfillStartForTime(now)
	return s.syncHistorySummaryRange(ctx, siteID, backfillStart, now, true)
}

func (s *Server) backfillHistorySummariesWithExisting(
	ctx context.Context,
	siteID string,
	now time.Time,
	fetchWeather bool,
	existing []types.HistorySummary,
) ([]types.HistorySummary, error) {
	backfillStart := s.backfillStartForTime(now)
	return s.syncHistorySummaryWindow(ctx, siteID, backfillStart, true, backfillStart, fetchWeather, now, true, existing)
}

// updateHistorySummary checks and commits completed days since latestDayStart
// up to yesterday.
func (s *Server) updateHistorySummary(
	ctx context.Context,
	siteID string,
	latestDayStart,
	now time.Time,
) ([]types.HistorySummary, error) {
	candidateStart := latestDayStart.AddDate(0, 0, 1)
	return s.syncHistorySummaryRange(ctx, siteID, candidateStart, now, false)
}

func (s *Server) syncHistorySummaryRange(
	ctx context.Context,
	siteID string,
	startDay,
	now time.Time,
	isBackfill bool,
) ([]types.HistorySummary, error) {
	return s.syncHistorySummaryWindow(ctx, siteID, startDay, true, startDay, true, now, isBackfill, nil)
}

func (s *Server) syncHistorySummaryUpdate(
	ctx context.Context,
	siteID string,
	energyStart time.Time,
	fetchEnergy bool,
	weatherStart time.Time,
	fetchWeather bool,
	now time.Time,
	existing []types.HistorySummary,
) ([]types.HistorySummary, error) {
	return s.syncHistorySummaryWindow(ctx, siteID, energyStart, fetchEnergy, weatherStart, fetchWeather, now, false, existing)
}

func (s *Server) syncHistorySummaryWindow(
	ctx context.Context,
	siteID string,
	energyStart time.Time,
	fetchEnergy bool,
	weatherStart time.Time,
	fetchWeather bool,
	now time.Time,
	isBackfill bool,
	existing []types.HistorySummary,
) ([]types.HistorySummary, error) {
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	todayStart := truncateDay(now)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	fetchEnergy = fetchEnergy && !energyStart.After(yesterdayStart)
	fetchWeather = fetchWeather && !weatherStart.After(yesterdayStart)
	if !fetchEnergy && !fetchWeather {
		return nil, nil
	}

	startDay := energyStart
	if !fetchEnergy || (fetchWeather && weatherStart.Before(startDay)) {
		startDay = weatherStart
	}

	var errContext string
	if isBackfill {
		errContext = "backfill"
	} else {
		errContext = "update"
		log.Ctx(ctx).DebugContext(ctx, "updating history summary",
			slog.String("siteID", siteID),
			slog.Time("start", startDay),
			slog.Time("end", yesterdayStart),
		)
	}

	// Fetch energy histories in the energy candidate range in one query
	var energyStats []types.DailyEnergyStats
	if fetchEnergy {
		var err error
		energyStats, err = s.storage.GetEnergyHistory(ctx, siteID, energyStart, yesterdayStart.AddDate(0, 0, 1))
		if err != nil {
			return nil, fmt.Errorf("failed to get energy history for %s: %w", errContext, err)
		}
	}

	// Fetch weather in the weather candidate range in one query
	var weatherStats []types.Weather
	if fetchWeather {
		var err error
		weatherStats, err = s.storage.GetWeather(ctx, siteID, weatherStart, yesterdayStart.AddDate(0, 0, 1))
		if err != nil {
			return nil, fmt.Errorf("failed to get weather for %s: %w", errContext, err)
		}
	}

	locCache := make(map[string]*time.Location)
	getLocation := func(tz string, fallback *time.Location) *time.Location {
		if tz == "" {
			return fallback
		}
		if l, ok := locCache[tz]; ok && l != nil {
			return l
		}
		if l, err := time.LoadLocation(tz); err == nil {
			locCache[tz] = l
			return l
		}
		return fallback
	}

	archivedEnergyHours := make(map[string]int)
	archivedEnergyDates := make(map[string]bool)
	archivedWeatherDates := make(map[string]bool)
	for _, sm := range existing {
		for _, e := range sm.Energy {
			if e.TSDayStart.IsZero() {
				continue
			}
			eLoc := getLocation(e.TimeLocation, loc)
			key := e.TSDayStart.In(eLoc).Format("2006-01-02")
			archivedEnergyDates[key] = true
			if len(e.Hourly) > archivedEnergyHours[key] {
				archivedEnergyHours[key] = len(e.Hourly)
			}
		}
		for _, w := range sm.Weather {
			if w.TSDayStart.IsZero() {
				continue
			}
			wLoc := getLocation(w.TimeLocation, loc)
			key := w.TSDayStart.In(wLoc).Format("2006-01-02")
			archivedWeatherDates[key] = true
		}
	}

	weatherStatsByDate := make(map[string]types.Weather)
	for _, stat := range weatherStats {
		wLoc := getLocation(stat.TimeLocation, stat.TSDayStart.Location())
		weatherStatsByDate[stat.TSDayStart.In(wLoc).Format("2006-01-02")] = stat
	}

	energyStatsByDate := make(map[string]types.DailyEnergyStats)
	for _, stat := range energyStats {
		sLoc := getLocation(stat.TimeLocation, loc)
		energyStatsByDate[stat.TSDayStart.In(sLoc).Format("2006-01-02")] = stat
	}

	summariesByMonth := make(map[string]*types.HistorySummary)
	var minEnergyTime, maxEnergyTime time.Time
	for d := startDay; !d.After(yesterdayStart); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		dailyStat, hasData := energyStatsByDate[dateStr]

		// check to see if the 23:00 hour for this day is recorded
		var hasLastHour bool
		if hasData {
			dLoc := getLocation(dailyStat.TimeLocation, loc)
			targetHour := time.Date(d.Year(), d.Month(), d.Day(), 23, 0, 0, 0, dLoc)
			for _, h := range dailyStat.Hourly {
				hLoc := getLocation(h.TimeLocation, dLoc)
				if h.TSHourStart.In(hLoc).Equal(targetHour) {
					hasLastHour = true
					break
				}
			}
		}

		weather, hasWeather := weatherStatsByDate[dateStr]
		var weatherBeforeDayEnd bool
		if hasWeather && !weather.TSUpdated.IsZero() {
			wLoc := getLocation(weather.TimeLocation, loc)
			weatherDayEnd := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, wLoc).AddDate(0, 0, 1)
			weatherBeforeDayEnd = weather.TSUpdated.Before(weatherDayEnd)
		}

		// Local midnight of the next day plus 6 hours of deferral buffer
		pastSixHourThreshold := !now.Before(d.AddDate(0, 0, 1).Add(6 * time.Hour))

		// A day is eligible if it is complete (including post-midnight weather) or at least 6 hours into the next day
		if (!hasLastHour || weatherBeforeDayEnd) && !pastSixHourThreshold {
			break
		}

		includeEnergy := hasData && (!archivedEnergyDates[dateStr] || len(dailyStat.Hourly) > archivedEnergyHours[dateStr])
		includeWeather := hasWeather && !archivedWeatherDates[dateStr]
		if !includeEnergy && !includeWeather {
			continue
		}

		monthKey := d.Format("2006-01")
		summary, ok := summariesByMonth[monthKey]
		if !ok {
			monthStart, parseErr := time.ParseInLocation("2006-01", monthKey, loc)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse month key %s: %w", monthKey, parseErr)
			}
			summary = &types.HistorySummary{
				TSMonthStart: monthStart,
			}
			summariesByMonth[monthKey] = summary
		}
		if includeEnergy {
			dailyStat.Hourly = slices.Clone(dailyStat.Hourly)
			for _, h := range dailyStat.Hourly {
				if h.TSHourStart.IsZero() {
					continue
				}
				if minEnergyTime.IsZero() || h.TSHourStart.Before(minEnergyTime) {
					minEnergyTime = h.TSHourStart
				}
				hEnd := h.TSHourStart.Add(time.Hour)
				if maxEnergyTime.IsZero() || hEnd.After(maxEnergyTime) {
					maxEnergyTime = hEnd
				}
			}
			summary.Energy = append(summary.Energy, dailyStat)
		}
		if includeWeather {
			summary.Weather = append(summary.Weather, weather)
		}
	}

	if len(summariesByMonth) == 0 {
		return nil, nil
	}

	if isBackfill {
		log.Ctx(ctx).InfoContext(ctx, "backfilling history summary",
			slog.String("siteID", siteID),
			slog.Time("start", startDay),
			slog.Time("end", yesterdayStart),
		)
	}

	if !minEnergyTime.IsZero() && minEnergyTime.Before(maxEnergyTime) {
		log.Ctx(ctx).DebugContext(ctx, "pulling action history to enrich energy stats",
			slog.String("siteID", siteID),
			slog.Time("start", minEnergyTime),
			slog.Time("end", maxEnergyTime),
		)
		actions, actErr := s.storage.GetActionHistory(ctx, siteID, minEnergyTime, maxEnergyTime)
		if actErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get action history for history summary", slog.String("siteID", siteID), slog.Any("error", actErr))
		} else if len(actions) > 0 {
			for _, summary := range summariesByMonth {
				enrichDailyEnergyStatsWithActions(summary.Energy, actions)
			}
		}
	}

	var updatedSummaries []types.HistorySummary
	for m, summary := range summariesByMonth {
		if len(summary.Energy) == 0 && len(summary.Weather) == 0 {
			continue
		}

		// Sort energy stats and weather chronologically
		slices.SortFunc(summary.Energy, func(a, b types.DailyEnergyStats) int {
			return a.TSDayStart.Compare(b.TSDayStart)
		})
		slices.SortFunc(summary.Weather, func(a, b types.Weather) int {
			return a.TSDayStart.Compare(b.TSDayStart)
		})

		updated, err := s.storage.UpdateHistorySummary(ctx, siteID, m, *summary)
		if err != nil {
			return nil, fmt.Errorf("failed to update history summary for month %s: %w", m, err)
		}
		updatedSummaries = append(updatedSummaries, updated)
	}

	return updatedSummaries, nil
}

func enrichDailyEnergyStatsWithActions(dailyStats []types.DailyEnergyStats, actions []types.Action) {
	if len(dailyStats) == 0 || len(actions) == 0 {
		return
	}

	actionsByHour := make(map[int64][]types.Action)
	for _, a := range actions {
		ts := a.Timestamp
		if ts.IsZero() {
			ts = a.SystemStatus.Timestamp
		}
		if ts.IsZero() {
			continue
		}
		hourKey := ts.Truncate(time.Hour).Unix()
		actionsByHour[hourKey] = append(actionsByHour[hourKey], a)
	}

	for dIdx := range dailyStats {
		for hIdx := range dailyStats[dIdx].Hourly {
			stat := &dailyStats[dIdx].Hourly[hIdx]
			if stat.TSHourStart.IsZero() {
				continue
			}
			hourActions := actionsByHour[stat.TSHourStart.Truncate(time.Hour).Unix()]
			for _, a := range hourActions {
				if a.SystemStatus.BatteryHeating {
					stat.HadBatteryHeating = true
				}
				if a.SystemStatus.VPPActive || a.Reason == types.ActionReasonVPPActive {
					stat.HadVPPActive = true
				}
				if a.SystemStatus.GridUnavailable || a.Reason == types.ActionReasonGridUnavailable {
					stat.HadGridUnavailable = true
				}
				if a.SystemStatus.EmergencyMode || a.Reason == types.ActionReasonEmergencyMode {
					stat.HadEmergencyMode = true
				}
				if a.SolarMode == types.SolarModeNoExport || a.Reason == types.ActionReasonPreventSolarCurtailment {
					stat.HadSolarNoExport = true
				}
				for _, alarm := range a.SystemStatus.Alarms {
					exists := false
					for _, existing := range stat.Alarms {
						if existing.Code == alarm.Code && existing.Name == alarm.Name && existing.Description == alarm.Description {
							exists = true
							break
						}
					}
					if !exists {
						stat.Alarms = append(stat.Alarms, alarm)
					}
				}
			}
		}
	}
}

func flattenDailyEnergyStats(daily []types.DailyEnergyStats) []types.EnergyStats {
	var flat []types.EnergyStats
	for _, d := range daily {
		flat = append(flat, d.Hourly...)
	}
	return flat
}

func getCronGroups(nowTime time.Time, kind string, idx int, total int) ([]int, error) {
	if kind == "" {
		allGroups := make([]int, 16)
		for i := 0; i < 16; i++ {
			allGroups[i] = i + 1
		}
		return allGroups, nil
	}

	var groups []int
	switch kind {
	case "all":
		groups = make([]int, 16)
		for i := 0; i < 16; i++ {
			groups[i] = i + 1
		}
	case "comed":
		groups = []int{13, 14, 15, 16}
	case "tou":
		groups = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

		seedString := nowTime.UTC().Format("2006-01-02-15")
		h := sha256.Sum256([]byte(seedString))
		seed1 := binary.BigEndian.Uint64(h[0:8])
		seed2 := binary.BigEndian.Uint64(h[8:16])
		pcg := rand.NewPCG(seed1, seed2)
		r := rand.New(pcg)

		r.Shuffle(len(groups), func(i, j int) {
			groups[i], groups[j] = groups[j], groups[i]
		})
	default:
		return nil, fmt.Errorf("invalid kind: %q", kind)
	}

	if total <= 0 || len(groups)%total != 0 {
		return nil, fmt.Errorf("cannot evenly partition %d %s groups into %d parts", len(groups), kind, total)
	}
	if idx < 0 || idx >= total {
		return nil, fmt.Errorf("idx %d out of bounds for total %d", idx, total)
	}

	chunkSize := len(groups) / total
	start := idx * chunkSize
	end := start + chunkSize
	return groups[start:end], nil
}
