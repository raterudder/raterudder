package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	maxSavingsPeriods    = 3
	maxSavingsWindowDays = 35
)

// savingsPeriod defines a [Start, End) time window for savings calculation.
type savingsPeriod struct {
	Start time.Time
	End   time.Time
}

// parseSavingsPeriods parses a semicolon-delimited list of comma-separated RFC3339 start,end pairs
// (e.g., "2026-10-11T00:00:00Z,2026-10-12T00:00:00Z;2026-10-05T00:00:00Z,2026-10-12T00:00:00Z").
func parseSavingsPeriods(r *http.Request, defaultStart, defaultEnd time.Time) ([]savingsPeriod, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("savingsPeriods"))
	if raw == "" {
		return []savingsPeriod{{Start: defaultStart, End: defaultEnd}}, nil
	}
	parts := strings.Split(raw, ";")
	periods := make([]savingsPeriod, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		bounds := strings.Split(part, ",")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("invalid savingsPeriod %d: expected start,end", i)
		}
		start, err := time.Parse(time.RFC3339, strings.TrimSpace(bounds[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid start time in savingsPeriod %d: %w", i, err)
		}
		end, err := time.Parse(time.RFC3339, strings.TrimSpace(bounds[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid end time in savingsPeriod %d: %w", i, err)
		}
		periods = append(periods, savingsPeriod{Start: start, End: end})
	}
	if err := validateSavingsPeriods(periods); err != nil {
		return nil, err
	}
	return periods, nil
}

func validateSavingsPeriods(periods []savingsPeriod) error {
	if len(periods) == 0 {
		return fmt.Errorf("at least one savings period is required")
	}
	if len(periods) > maxSavingsPeriods {
		return fmt.Errorf("cannot request more than %d savings periods", maxSavingsPeriods)
	}

	minStart := periods[0].Start
	maxEnd := periods[0].End
	for i, p := range periods {
		if p.Start.IsZero() || p.End.IsZero() {
			return fmt.Errorf("savings period %d start and end are required", i)
		}
		if !p.Start.Before(p.End) {
			return fmt.Errorf("savings period %d start must be before end", i)
		}
		if p.Start.Before(minStart) {
			minStart = p.Start
		}
		if p.End.After(maxEnd) {
			maxEnd = p.End
		}
	}

	if maxEnd.Sub(minStart) > maxSavingsWindowDays*24*time.Hour {
		return fmt.Errorf("total savings period range cannot exceed %d days", maxSavingsWindowDays)
	}

	// Require every pair of periods to be either nested within one another or contiguous.
	for i := 0; i < len(periods); i++ {
		for j := i + 1; j < len(periods); j++ {
			a := periods[i]
			b := periods[j]
			aInB := !a.Start.Before(b.Start) && !a.End.After(b.End)
			bInA := !b.Start.Before(a.Start) && !b.End.After(a.End)
			contiguous := a.End.Sub(b.Start).Abs() <= time.Second || b.End.Sub(a.Start).Abs() <= time.Second
			if !aInB && !bInA && !contiguous {
				return fmt.Errorf("savings periods must be contiguous or nested within one another")
			}
		}
	}
	return nil
}

func accumulateSavingsStats(dst *types.SavingsStats, src types.SavingsStats) {
	dst.HomeUsed += src.HomeUsed
	dst.SolarGenerated += src.SolarGenerated
	dst.GridImported += src.GridImported
	dst.GridExported += src.GridExported
	dst.BatteryUsed += src.BatteryUsed
	dst.Cost += src.Cost
	dst.Credit += src.Credit
	dst.AvoidedCost += src.AvoidedCost
	dst.ChargingCost += src.ChargingCost
	dst.SolarSavings += src.SolarSavings
	dst.BatterySavings += src.BatterySavings
	dst.TotalSavingsDollars += src.TotalSavingsDollars
	dst.SolarAndBatterySavingsDollars += src.SolarAndBatterySavingsDollars
	dst.RateRudderSavingsDollars += src.RateRudderSavingsDollars
	dst.SelfConsumptionCostDollars += src.SelfConsumptionCostDollars
	dst.SelfConsumptionCreditDollars += src.SelfConsumptionCreditDollars
	dst.GridOnlyCostDollars += src.GridOnlyCostDollars
	dst.StoredEnergyValueDollars += src.StoredEnergyValueDollars
	if src.Estimated {
		dst.Estimated = true
	}
}

// Deprecated: handleHistorySavings is superseded by handleHistoryActionsAndSavings.
func (s *Server) handleHistorySavings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	siteID := s.getSiteID(r)
	start, end, err := s.parseTimeRange(r)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("invalid time range: %v", err), http.StatusBadRequest)
		return
	}

	newCalc := r.URL.Query().Get("newCalculation") == "true"

	var siteIDs []string
	if siteID == SiteIDAll {
		sites := s.getAllUserSites(r)
		siteIDs = make([]string, len(sites))
		for i, site := range sites {
			siteIDs[i] = site.ID
		}
	} else {
		siteIDs = []string{siteID}
	}

	var totalSavings types.SavingsStats
	totalSavings.Timestamp = start

	for _, id := range siteIDs {
		var stats types.SavingsStats
		if newCalc {
			periods := []savingsPeriod{{Start: start, End: end}}
			periodStats, _, err := s.getSiteSavingsNew(ctx, id, start, end, periods)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get new savings for site", slog.String("siteID", id), slog.Any("error", err))
				writeJSONError(w, fmt.Sprintf("failed to get savings for site %s", id), http.StatusInternalServerError)
				return
			}
			if len(periodStats) > 0 {
				stats = periodStats[0]
				totalSavings.EndTimestamp = stats.EndTimestamp
			}
		} else {
			var err error
			stats, _, err = s.getSiteSavings(ctx, id, start, end)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get savings for site", slog.String("siteID", id), slog.Any("error", err))
				// If one site fails, maybe continue or fail fast? Failing fast for now to be safe.
				writeJSONError(w, fmt.Sprintf("failed to get savings for site %s", id), http.StatusInternalServerError)
				return
			}
		}

		accumulateSavingsStats(&totalSavings, stats)

		// Only include hourly debugging if it's a single site request
		if siteID != SiteIDAll {
			totalSavings.HourlyDebugging = stats.HourlyDebugging
		}
	}

	if !newCalc {
		totalSavings.BatterySavings = totalSavings.AvoidedCost - totalSavings.ChargingCost
	}

	w.Header().Set("Content-Type", "application/json")

	// Set Cache-Control
	today := truncateDay(s.now())
	if end.Before(today) {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=60")
	}

	if err := json.NewEncoder(w).Encode(totalSavings); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// getIgnoredFraction calculates the fraction of the hour [hStart, hStart+1h)
// where the system was paused or in emergency mode (storm hedge).
// It assumes actions are sorted by timestamp.
func getIgnoredFraction(hStart time.Time, dur time.Duration, actions []types.Action) float64 {
	hEnd := hStart.Add(dur)
	ignoredDuration := time.Duration(0)

	// We evaluate the state piece-wise over the interval.
	// To know the state at hStart, we find the last action before hStart.
	var lastStatePausedOrEmergency bool
	var lastActionTime time.Time

	for _, a := range actions {
		if a.Timestamp.Before(hStart) {
			lastStatePausedOrEmergency = a.Paused || a.Reason == types.ActionReasonEmergencyMode || a.SystemStatus.EmergencyMode || a.Reason == types.ActionReasonVPPActive || a.SystemStatus.VPPActive
			lastActionTime = a.Timestamp
		} else if a.Timestamp.Before(hEnd) {
			// This action falls within the interval
			if lastStatePausedOrEmergency {
				// The time from max(hStart, lastActionTime) up to a.Timestamp was ignored
				startPeriod := hStart
				if lastActionTime.After(hStart) {
					startPeriod = lastActionTime
				}
				ignoredDuration += a.Timestamp.Sub(startPeriod)
			}
			lastStatePausedOrEmergency = a.Paused || a.Reason == types.ActionReasonEmergencyMode || a.SystemStatus.EmergencyMode || a.Reason == types.ActionReasonVPPActive || a.SystemStatus.VPPActive
			lastActionTime = a.Timestamp
		} else {
			// action is at or after hEnd, we just evaluate the remaining part of the interval
			break
		}
	}

	// Handle the remainder of the interval up to hEnd
	if lastStatePausedOrEmergency {
		startPeriod := hStart
		if lastActionTime.After(hStart) {
			startPeriod = lastActionTime
		}
		if startPeriod.Before(hEnd) {
			ignoredDuration += hEnd.Sub(startPeriod)
		}
	}

	fraction := float64(ignoredDuration) / float64(dur)

	// EDGE CASE NOTE:
	// If there is no action preceding hStart within the query results, we assume the initial
	// state at hStart was NOT paused or emergency.
	// In reality, the system might have been in a paused or emergency state that was entered
	// prior to our query window (e.g. during an extended gateway/API outage or a long pause).
	// We are explicitly okay with this edge case having a slightly lower-than-potentially-accurate
	// fraction for that first hour to avoid the complexity and overhead of fetching further back.
	return fraction
}

func (s *Server) getSiteSavings(ctx context.Context, siteID string, start, end time.Time) (types.SavingsStats, []types.Action, error) {
	// Look back 1day to track battery inventory correctly.
	lookbackStart := start.AddDate(0, 0, -1)

	// Fetch settings for this site
	settings, _, _, err := s.storage.GetSettings(ctx, siteID)
	if err != nil {
		return types.SavingsStats{}, nil, err
	}

	fetchEnd := end
	if settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net" {
		// We look ahead another 24 hours to support the 24h window for net metering valuation.
		fetchEnd = end.AddDate(0, 0, 1)
	}

	// Fetch prices (these are hourly)
	prices, err := s.storage.GetPriceHistory(ctx, siteID, lookbackStart, fetchEnd)
	if err != nil {
		return types.SavingsStats{}, nil, err
	}

	// Only fetch future prices if net metering is enabled and we are looking at recent data
	if (settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net") && fetchEnd.After(time.Now()) {
		needFuture := true
		for _, p := range prices {
			// Check if we already have a price covering the end of our required window
			if p.Contains(fetchEnd.Add(-time.Hour)) {
				needFuture = false
				break
			}
		}

		if needFuture {
			u, err := s.utilities.Site(ctx, siteID, settings)
			if err == nil {
				futurePrices, err := u.GetFuturePrices(ctx)
				if err == nil {
					prices = append(prices, futurePrices...)
				}
			}
		}
	}

	// Fetch energy history
	energyStatsDaily, err := s.storage.GetEnergyHistory(ctx, siteID, lookbackStart, end)
	if err != nil {
		return types.SavingsStats{}, nil, err
	}

	var energyStats []types.EnergyStats
	for _, day := range energyStatsDaily {
		energyStats = append(energyStats, day.Hourly...)
	}

	// Fetch action history (look back 2 hours first to cover the state at lookbackStart)
	actionStart := lookbackStart.Add(-2 * time.Hour)
	actions, err := s.storage.GetActionHistory(ctx, siteID, actionStart, end)
	if err == nil {
		// getIgnoredFraction assumes actions are sorted by timestamp
		sort.Slice(actions, func(i, j int) bool {
			return actions[i].Timestamp.Before(actions[j].Timestamp)
		})
	} else {
		// Log error but don't fail, we can just proceed without pause logic
		log.Ctx(ctx).WarnContext(ctx, "failed to get action history for savings", slog.String("siteID", siteID), slog.Any("error", err))
	}

	type energyChunk struct {
		amount  float64
		price   float64
		ignored bool
	}

	var chargeStack []energyChunk
	var stats types.SavingsStats
	stats.Timestamp = start
	type statSubInterval struct {
		stat     types.EnergyStats
		price    types.Price
		duration float64 // Fraction of the hour (e.g. 0.5)
	}

	for _, stat := range energyStats {
		ts := stat.TSHourStart.Truncate(time.Hour)

		// Check if we have sub-hourly prices for this hour
		var hourPrices []types.Price
		for _, p := range prices {
			if !p.TSStart.Before(ts) && p.TSStart.Before(ts.Add(time.Hour)) {
				hourPrices = append(hourPrices, p)
			}
		}

		var intervals []statSubInterval
		if len(hourPrices) > 1 {
			// Sort hour prices chronologically
			sort.Slice(hourPrices, func(i, j int) bool {
				return hourPrices[i].TSStart.Before(hourPrices[j].TSStart)
			})
			for _, hp := range hourPrices {
				dur := hp.TSEnd.Sub(hp.TSStart).Hours()
				scaledStat := types.EnergyStats{
					TSHourStart:       hp.TSStart,
					MinBatterySOC:     stat.MinBatterySOC,
					MaxBatterySOC:     stat.MaxBatterySOC,
					BatteryChargedKWH: stat.BatteryChargedKWH * dur,
					BatteryUsedKWH:    stat.BatteryUsedKWH * dur,
					SolarKWH:          stat.SolarKWH * dur,
					HomeKWH:           stat.HomeKWH * dur,
					GridExportKWH:     stat.GridExportKWH * dur,
					GridImportKWH:     stat.GridImportKWH * dur,
					VPPExportKWH:      stat.VPPExportKWH * dur,
					BatteryToHomeKWH:  stat.BatteryToHomeKWH * dur,
					SolarToHomeKWH:    stat.SolarToHomeKWH * dur,
					SolarToBatteryKWH: stat.SolarToBatteryKWH * dur,
					SolarToGridKWH:    stat.SolarToGridKWH * dur,
					BatteryToGridKWH:  stat.BatteryToGridKWH * dur,
					Alarms:            stat.Alarms,
				}
				intervals = append(intervals, statSubInterval{
					stat:     scaledStat,
					price:    hp,
					duration: dur,
				})
			}
		} else {
			var currentPrice types.Price
			var found bool
			for _, p := range prices {
				if p.Contains(ts) {
					currentPrice = p
					found = true
					break
				}
			}
			if !found {
				continue
			}
			intervals = append(intervals, statSubInterval{
				stat:     stat,
				price:    currentPrice,
				duration: 1.0,
			})
		}

		for _, interval := range intervals {
			subStat := interval.stat
			subPrice := interval.price
			subTS := subStat.TSHourStart
			inRequestedPeriod := !subTS.Before(start) && subTS.Before(end)

			gridImportPrice, gridExportPrice := calculateIntervalPrices(subPrice, subTS, settings, prices)

			ignoredFraction := getIgnoredFraction(subTS, time.Duration(interval.duration*float64(time.Hour)), actions)
			activeFraction := 1.0 - ignoredFraction

			activeGridToBattery := max(0, subStat.BatteryChargedKWH-subStat.SolarToBatteryKWH) * activeFraction
			activeSolarToBattery := min(subStat.BatteryChargedKWH, subStat.SolarToBatteryKWH) * activeFraction
			// Emergency/paused charging: the energy still enters the battery, but
			// we treat it as $0 cost since it was forced (storm hedge, pause, etc.)
			// and not a deliberate arbitrage decision.
			ignoredGridToBattery := max(0, subStat.BatteryChargedKWH-subStat.SolarToBatteryKWH) * ignoredFraction
			ignoredSolarToBattery := min(subStat.BatteryChargedKWH, subStat.SolarToBatteryKWH) * ignoredFraction

			// Push to LIFO stack if we charged the battery.
			// We push 'active' first then 'ignored' so that the 'ignored' portion
			// (which often happens later in the hour, e.g. storm starts) is popped first (LIFO).
			if activeGridToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  activeGridToBattery,
					price:   gridImportPrice,
					ignored: false,
				})
			}
			if activeSolarToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  activeSolarToBattery,
					price:   0.0, // Solar costs $0
					ignored: false,
				})
			}
			if ignoredGridToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  ignoredGridToBattery,
					price:   gridImportPrice, // Track matching rate, but flag as ignored
					ignored: true,
				})
			}
			if ignoredSolarToBattery > 0 {
				chargeStack = append(chargeStack, energyChunk{
					amount:  ignoredSolarToBattery,
					price:   0.0,
					ignored: true,
				})
			}

			activeBatteryToHome := subStat.BatteryToHomeKWH * activeFraction

			// Pop from LIFO stack to determine cost of the used battery energy.
			// We pop the TOTAL amount to keep our inventory stack in sync with the physical battery,
			// but we separate 'ignored' volume from 'active' cost.
			activeDischargeCost := 0.0
			ignoredDischargeKWH := 0.0
			amountToDischarge := subStat.BatteryUsedKWH
			for amountToDischarge > 0 && len(chargeStack) > 0 {
				top := &chargeStack[len(chargeStack)-1]
				take := min(amountToDischarge, top.amount)
				if !top.ignored {
					activeDischargeCost += take * top.price
				} else {
					ignoredDischargeKWH += take
				}
				top.amount -= take
				amountToDischarge -= take
				if top.amount <= 0 {
					chargeStack = chargeStack[:len(chargeStack)-1]
				}
			}

			// Calculate performance metrics by subtracting ignored volume from raterudder's results.
			// We subtract ignored volume from both used and to-home (conservative assumption).
			totalIgnoredHome := max(subStat.BatteryToHomeKWH*ignoredFraction, min(subStat.BatteryToHomeKWH, ignoredDischargeKWH))
			effBatteryToHome := max(0, subStat.BatteryToHomeKWH-totalIgnoredHome)

			totalIgnoredUsed := max(subStat.BatteryUsedKWH*ignoredFraction, ignoredDischargeKWH)
			effBatteryUsed := max(0, subStat.BatteryUsedKWH-totalIgnoredUsed)

			// Calculate charging cost for home based on the active (non-ignored) portion.
			chargingCostForHome := 0.0
			if effBatteryUsed > 0 {
				chargingCostForHome = activeDischargeCost * (effBatteryToHome / effBatteryUsed)
			}

			avoided := effBatteryToHome * gridImportPrice

			// Accumulate Energy Amounts (raw, unscaled for general stats)
			if inRequestedPeriod {
				stats.HomeUsed += subStat.HomeKWH
				stats.SolarGenerated += subStat.SolarKWH
				stats.GridImported += subStat.GridImportKWH
				stats.GridExported += subStat.GridExportKWH
				stats.BatteryUsed += subStat.BatteryUsedKWH

				// Cost and Credit
				stats.Cost += subStat.GridImportKWH * gridImportPrice
				stats.Credit += subStat.GridExportKWH * gridExportPrice

				stats.AvoidedCost += avoided
				stats.ChargingCost += chargingCostForHome

				solarToHome := subStat.SolarToHomeKWH
				solarSavings := solarToHome * gridImportPrice
				stats.SolarSavings += solarSavings

				stats.HourlyDebugging = append(stats.HourlyDebugging, types.HourlySavingsStatsDebugging{
					ExportPrice:     gridExportPrice,
					ImportPrice:     gridImportPrice,
					BatteryToHome:   activeBatteryToHome,
					Avoided:         avoided,
					GridToBattery:   activeGridToBattery,
					ChargingCost:    chargingCostForHome,
					SolarToHome:     solarToHome,
					SolarSavings:    solarSavings,
					IgnoredFraction: ignoredFraction,
				})
			}
		}
	}

	var filteredActions []types.Action
	for _, a := range actions {
		if !a.Timestamp.Before(start) && a.Timestamp.Before(end) {
			filteredActions = append(filteredActions, a)
		}
	}

	stats.BatterySavings = stats.AvoidedCost - stats.ChargingCost
	return stats, filteredActions, nil
}

// TODO: we have this duplicated in several spots, we should consolidate
func calculateIntervalPrices(subPrice types.Price, subTS time.Time, settings types.Settings, prices []types.Price) (float64, float64) {
	gridImportPrice := subPrice.DollarsPerKWH + subPrice.GridUseDollarsPerKWH
	var gridExportPrice float64

	if !settings.GridExportSolar {
		gridExportPrice = 0
	} else if settings.UtilityRateOptions.NetMeteringCredits || settings.UtilityRateOptions.NetMeteringScheme == "net" {
		// For net metering, we value the export based on the min/max price of the day (24h window)
		maxPrice := subPrice.DollarsPerKWH + subPrice.GridUseDollarsPerKWH
		minPrice := maxPrice
		windowEnd := subTS.Add(24 * time.Hour)
		for _, p := range prices {
			if !p.TSStart.Before(subTS) && p.TSStart.Before(windowEnd) {
				cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
				if cost > maxPrice {
					maxPrice = cost
				}
				if cost < minPrice {
					minPrice = cost
				}
			}
		}

		switch settings.SolarNetMeteringCreditsValue {
		case "highest":
			gridExportPrice = maxPrice
		case "none":
			gridExportPrice = 0
		default:
			// Default to conservative value ("lowest")
			gridExportPrice = minPrice
		}
		if gridExportPrice != 0 {
			// Apply generation adjustment if net metering is active and not valued at 0.
			gridExportPrice += subPrice.GenerationAdjustmentDollarsPerKWH
		}
	} else if subPrice.SeparateGenerationCredit {
		// Post-2025 style: utility pays a distinct generation credit rate for
		// solar exported to the grid, separate from the supply rate.
		gridExportPrice = subPrice.GenerationCreditDollarsPerKWH
	} else {
		// Default export price should NOT include the gridUse fees, but includes the generation adjustment (if any).
		gridExportPrice = subPrice.DollarsPerKWH + subPrice.GenerationAdjustmentDollarsPerKWH
	}
	return gridImportPrice, gridExportPrice
}

// getCombinedEnergyHistory fetches energy history across [historyStart, historyEnd) by combining
// monthly history_summary documents with unsummarized daily energy_history documents.
func (s *Server) getCombinedEnergyHistory(ctx context.Context, siteID string, historyStart, historyEnd time.Time) ([]types.DailyEnergyStats, error) {
	todayStart := truncateDay(s.now())

	var summaries []types.HistorySummary
	// Only query monthly summaries if the requested window starts before today.
	if historyStart.Before(todayStart) {
		var err error
		summaries, err = s.storage.GetHistorySummaries(ctx, siteID, historyStart, historyEnd)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get history summaries for savings, falling back to energy history", slog.String("siteID", siteID), slog.Any("error", err))
		}
	}

	var combinedEnergy []types.DailyEnergyStats
	var latestSummaryEnergyDay time.Time

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

	// determine if we can get all of the energy data we need from the history summaries
	for _, summary := range summaries {
		for _, day := range summary.Energy {
			dLoc := getLocation(day.TimeLocation, historyStart.Location())
			if day.TSDayStart.In(dLoc).Before(todayStart) {
				combinedEnergy = append(combinedEnergy, day)
				if latestSummaryEnergyDay.IsZero() || day.TSDayStart.In(dLoc).After(latestSummaryEnergyDay) {
					latestSummaryEnergyDay = day.TSDayStart.In(dLoc)
				}
			}
		}
	}
	energyFetchStart := historyStart
	if !latestSummaryEnergyDay.IsZero() {
		energyFetchStart = latestSummaryEnergyDay.AddDate(0, 0, 1)
	}
	if energyFetchStart.Before(historyStart) {
		energyFetchStart = historyStart
	}
	if energyFetchStart.Before(historyEnd) {
		unsummarized, err := s.storage.GetEnergyHistory(ctx, siteID, energyFetchStart, historyEnd)
		if err != nil {
			if len(combinedEnergy) == 0 {
				return nil, err
			}
			log.Ctx(ctx).WarnContext(ctx, "failed to get unsummarized energy history for savings", slog.String("siteID", siteID), slog.Any("error", err))
		} else {
			combinedEnergy = append(combinedEnergy, unsummarized...)
		}
	}

	// de-duplicate in case we had a bit of overlap
	if len(combinedEnergy) > 0 {
		seen := make(map[string]int)
		var deduped []types.DailyEnergyStats
		for _, day := range combinedEnergy {
			dateStr := day.TSDayStart.Format("2006-01-02")
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

	return combinedEnergy, nil
}

// resolveDirectSolarToHomeKWH returns the solar energy that directly served home load during the interval.
// When source-to-destination breakdown fields are populated on EnergyStats, we use subStat.SolarToHomeKWH
// so intra-hour solar/load mismatch (e.g., a 10 kW oven spike for 15 minutes during a 3 kWh solar hour)
// is preserved instead of assuming uniform 1-hour overlap min(SolarKWH, HomeKWH).
func resolveDirectSolarToHomeKWH(subStat types.EnergyStats, effectiveHomeKWH float64) float64 {
	if subStat.SolarToHomeKWH > 0 || subStat.SolarToBatteryKWH > 0 || subStat.SolarToGridKWH > 0 {
		return max(0, min(subStat.SolarKWH, min(effectiveHomeKWH, subStat.SolarToHomeKWH)))
	}
	return min(subStat.SolarKWH, effectiveHomeKWH)
}

// inferRoundTripEfficiency estimates the battery's realized round-trip efficiency across the window
// when at least 2 full battery cycles of charge and discharge telemetry are present, adjusting for
// any net change in stored SOC between the first and last hour and clamping between 75% and defaultRTEff.
func inferRoundTripEfficiency(energyStats []types.EnergyStats, capacityKWH, defaultRTEff float64) float64 {
	if defaultRTEff <= 0 || defaultRTEff > 1.0 {
		defaultRTEff = 0.90
	}
	if capacityKWH <= 0 || len(energyStats) == 0 {
		return defaultRTEff
	}
	var totalCharged, totalUsed float64
	var startSOC, endSOC float64
	hasStartSOC := false
	for _, st := range energyStats {
		totalCharged += st.BatteryChargedKWH
		totalUsed += st.BatteryUsedKWH
		if st.MinBatterySOC > 0 || st.MaxBatterySOC > 0 {
			if !hasStartSOC {
				hasStartSOC = true
				if st.BatteryChargedKWH >= st.BatteryUsedKWH && st.MinBatterySOC > 0 {
					startSOC = st.MinBatterySOC
				} else if st.MaxBatterySOC > 0 {
					startSOC = st.MaxBatterySOC
				}
			}
			if st.BatteryChargedKWH > st.BatteryUsedKWH && st.MaxBatterySOC > 0 {
				endSOC = st.MaxBatterySOC
			} else if st.BatteryUsedKWH > st.BatteryChargedKWH && st.MinBatterySOC > 0 {
				endSOC = st.MinBatterySOC
			} else if st.MaxBatterySOC > 0 {
				endSOC = st.MaxBatterySOC
			}
		}
	}
	if totalCharged >= 2*capacityKWH && totalUsed >= 2*capacityKWH {
		netSOCDeltaKWH := 0.0
		if hasStartSOC {
			netSOCDeltaKWH = (endSOC - startSOC) / 100.0 * capacityKWH
		}
		obs := (totalUsed + netSOCDeltaKWH*math.Sqrt(defaultRTEff)) / totalCharged
		if obs >= 0.75 && obs < defaultRTEff {
			return obs
		}
	}
	return defaultRTEff
}

// actionReserveSOC extracts the active battery reserve SOC percentage from an Action when present.
// It checks:
//  1. Explicit Action.ReserveSOC (recorded on new actions)
//  2. Plan.Periods[0].ReserveSOC (recorded on PlanMode actions)
//  3. SystemStatus.BatterySOC when the controller is holding at ActionReasonBatteryAtReserve (legacy Decide actions)
func actionReserveSOC(a types.Action) (float64, bool) {
	if a.ReserveSOC > 0 && a.ReserveSOC <= 100 {
		return a.ReserveSOC, true
	}
	if a.Plan != nil && len(a.Plan.Periods) > 0 && a.Plan.Periods[0].ReserveSOC > 0 && a.Plan.Periods[0].ReserveSOC <= 100 {
		return a.Plan.Periods[0].ReserveSOC, true
	}
	if a.Reason == types.ActionReasonBatteryAtReserve && a.BatteryMode != types.BatteryModeChargeAny && a.SystemStatus.BatterySOC > 0 && a.SystemStatus.BatterySOC <= 100 {
		return math.Round(a.SystemStatus.BatterySOC), true
	}
	return 0, false
}

// inferDailyBaselineReserve infers the daily baseline reserve SOC for the shadow battery from historical
// actions in each local day bucket across [lookbackStart, maxEnd). Taking the daily median across actions
// matches the user's normal daytime reserve (identical to the highest reserve in a standard 2-tier setup
// where the user only lowers reserve during a 4-8h peak export window) while rejecting brief overnight
// 90%-100% charge-forcing spikes or temporary 5% peak-export dips.
func inferDailyBaselineReserve(
	lookbackStart, maxEnd time.Time,
	periods []savingsPeriod,
	siteLoc *time.Location,
	actions []types.Action,
	fallbackMinSOC float64,
) func(time.Time) float64 {
	dayBuckets := buildDailySavingsPeriods(lookbackStart, maxEnd, periods, siteLoc)
	dayReserves := make([]float64, len(dayBuckets))
	dayHasObs := make([]bool, len(dayBuckets))

	seedSOC := fallbackMinSOC
	foundSeed := false

	for i, db := range dayBuckets {
		var vals []float64
		for _, a := range actions {
			if !a.Timestamp.Before(db.Start) && a.Timestamp.Before(db.End) {
				if r, ok := actionReserveSOC(a); ok {
					vals = append(vals, r)
				}
			}
		}
		if len(vals) > 0 {
			sort.Float64s(vals)
			med := vals[len(vals)/2]
			dayReserves[i] = med
			dayHasObs[i] = true
			if !foundSeed {
				seedSOC = med
				foundSeed = true
			}
		}
	}

	if !foundSeed {
		for _, a := range actions {
			if r, ok := actionReserveSOC(a); ok {
				seedSOC = r
				break
			}
		}
	}

	cur := seedSOC
	for i := range dayBuckets {
		if dayHasObs[i] {
			cur = dayReserves[i]
		} else {
			dayReserves[i] = cur
		}
	}

	return func(ts time.Time) float64 {
		for i, db := range dayBuckets {
			if !ts.Before(db.Start) && ts.Before(db.End) {
				return dayReserves[i]
			}
		}
		return cur
	}
}

// stepShadowSelfConsumption simulates one interval of pure solar + battery self-consumption.
// Excess solar charges the battery up to 100% SOC (with oneWayEff) and exports the rest only if gridExportSolar is true.
// Net home load deficit discharges the battery down to minSOC (with oneWayEff) and imports any remaining deficit from the grid.
func stepShadowSelfConsumption(
	shadowSOC float64,
	homeKWH, solarKWH, directSolarToHomeKWH float64,
	minSOC, capacityKWH, oneWayEff float64,
	gridExportSolar bool,
) (nextShadowSOC, shadowImportKWH, shadowExportKWH float64) {
	if capacityKWH <= 0 {
		return shadowSOC, 0, 0
	}
	if oneWayEff <= 0 || oneWayEff > 1.0 {
		oneWayEff = math.Sqrt(0.90)
	}
	if minSOC < 0 {
		minSOC = 0
	} else if minSOC > 100 {
		minSOC = 100
	}

	shadowEnergyKWH := max(0, min(capacityKWH, (shadowSOC/100.0)*capacityKWH))
	reserveKWH := (minSOC / 100.0) * capacityKWH

	solarToHomeKWH := max(0, min(solarKWH, min(homeKWH, directSolarToHomeKWH)))
	solarSurplusKWH := max(0, solarKWH-solarToHomeKWH)

	if solarSurplusKWH > 0 {
		roomInBatKWH := max(0, capacityKWH-shadowEnergyKWH)
		solarToBatKWH := min(solarSurplusKWH, roomInBatKWH/oneWayEff)
		shadowEnergyKWH += solarToBatKWH * oneWayEff
		if gridExportSolar {
			shadowExportKWH = solarSurplusKWH - solarToBatKWH
		}
	}

	unmetHomeLoadKWH := max(0, homeKWH-solarToHomeKWH)
	if unmetHomeLoadKWH > 0 {
		usableBatEnergyKWH := max(0, shadowEnergyKWH-reserveKWH)
		maxBatDischargeKWH := usableBatEnergyKWH * oneWayEff
		batSuppliedHomeKWH := min(unmetHomeLoadKWH, maxBatDischargeKWH)
		shadowEnergyKWH -= batSuppliedHomeKWH / oneWayEff
		shadowImportKWH = unmetHomeLoadKWH - batSuppliedHomeKWH
	}

	nextShadowSOC = max(0, min(100, (shadowEnergyKWH/capacityKWH)*100.0))
	return nextShadowSOC, shadowImportKWH, shadowExportKWH
}

// getSiteSavingsNew calculates savings across one or more periods using a continuous
// Shadow Self-Consumption simulation. Kept separate from getSiteSavings so the legacy
// LIFO calculation can be easily removed once all sites are migrated to PlanMode.
func (s *Server) getSiteSavingsNew(
	ctx context.Context,
	siteID string,
	actionStart, actionEnd time.Time,
	periods []savingsPeriod,
) ([]types.SavingsStats, []types.Action, error) {
	if len(periods) == 0 {
		periods = []savingsPeriod{{Start: actionStart, End: actionEnd}}
	}

	minStart := periods[0].Start
	maxEnd := periods[0].End
	for _, p := range periods {
		if p.Start.Before(minStart) {
			minStart = p.Start
		}
		if p.End.After(maxEnd) {
			maxEnd = p.End
		}
	}

	// Cap lookback at 2 days prior to the earliest period to warm up shadowSOC.
	lookbackStart := minStart.AddDate(0, 0, -2)
	// Look ahead 24 hours past maxEnd for next-day stored energy valuation and 24h net metering.
	fetchEnd := maxEnd.AddDate(0, 0, 1)

	settings, _, err := s.getSettingsWithMigration(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}

	prices, err := s.storage.GetPriceHistory(ctx, siteID, lookbackStart, fetchEnd)
	if err != nil {
		return nil, nil, err
	}

	// for net metering we need future prices to determine the export credits
	if settings.UtilityRateOptions.IsFlatNetMetering() && fetchEnd.After(s.now()) {
		needFuture := true
		for _, p := range prices {
			if p.Contains(fetchEnd.Add(-time.Hour)) {
				needFuture = false
				break
			}
		}
		if needFuture && s.utilities != nil {
			u, err := s.utilities.Site(ctx, siteID, settings.Settings)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get utility for site", slog.String("siteID", siteID), slog.Any("error", err))
			} else {
				futurePrices, err := u.GetFuturePrices(ctx)
				if err == nil {
					prices = append(prices, futurePrices...)
				}
			}
		}
	}

	energyStatsDaily, err := s.getCombinedEnergyHistory(ctx, siteID, lookbackStart, fetchEnd)
	if err != nil {
		return nil, nil, err
	}

	var energyStats []types.EnergyStats
	for _, day := range energyStatsDaily {
		for _, h := range day.Hourly {
			if !h.TSHourStart.Before(lookbackStart) && h.TSHourStart.Before(fetchEnd) {
				energyStats = append(energyStats, h)
			}
		}
	}
	slices.SortFunc(energyStats, func(a, b types.EnergyStats) int {
		return a.TSHourStart.Compare(b.TSHourStart)
	})

	actionQueryEnd := actionEnd
	if maxEnd.After(actionQueryEnd) {
		actionQueryEnd = maxEnd
	}
	actions, err := s.storage.GetActionHistory(ctx, siteID, lookbackStart, actionQueryEnd)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get action history for new savings", slog.String("siteID", siteID), slog.Any("error", err))
	} else {
		sort.Slice(actions, func(i, j int) bool {
			return actions[i].Timestamp.Before(actions[j].Timestamp)
		})
	}

	var filteredActions []types.Action
	var latestActionWithPlan types.Action
	var capacityKWH float64
	for i := range actions {
		if !actions[i].Timestamp.Before(actionStart) && actions[i].Timestamp.Before(actionEnd) {
			filteredActions = append(filteredActions, actions[i])
		}
		if actions[i].Plan != nil && len(actions[i].Plan.Periods) > 0 {
			latestActionWithPlan = actions[i]
		}
		if actions[i].SystemStatus.BatteryCapacityKWH > 0 {
			capacityKWH = actions[i].SystemStatus.BatteryCapacityKWH
		}
	}

	if capacityKWH <= 0 {
		if len(actions) > 0 {
			log.Ctx(ctx).ErrorContext(ctx, "missing battery capacity on actions for savings calculation", slog.String("siteID", siteID), slog.Int("actionsCount", len(actions)))
		}
		results := make([]types.SavingsStats, len(periods))
		for i, p := range periods {
			results[i] = types.SavingsStats{
				Timestamp:    p.Start,
				EndTimestamp: p.End,
			}
		}
		return results, filteredActions, nil
	}
	optParams := settings.GetOptimizationParams()
	rtEff := inferRoundTripEfficiency(energyStats, capacityKWH, optParams.RoundTripEfficiency)
	oneWayEff := math.Sqrt(rtEff)

	siteLoc := minStart.Location()
	for _, day := range energyStatsDaily {
		if day.TimeLocation != "" {
			if l, err := time.LoadLocation(day.TimeLocation); err == nil {
				siteLoc = l
				break
			}
		}
	}

	baselineReserveForTime := inferDailyBaselineReserve(
		lookbackStart,
		maxEnd,
		periods,
		siteLoc,
		actions,
		settings.MinBatterySOC,
	)

	initialSOC := max(baselineReserveForTime(lookbackStart), 20.0)
	for _, stat := range energyStats {
		if stat.MinBatterySOC > 0 || stat.MaxBatterySOC > 0 {
			if stat.BatteryChargedKWH >= stat.BatteryUsedKWH && stat.MinBatterySOC > 0 {
				initialSOC = stat.MinBatterySOC
			} else if stat.MaxBatterySOC > 0 {
				initialSOC = stat.MaxBatterySOC
			}
			break
		}
	}

	actualSOC := initialSOC
	shadowSOC := initialSOC

	dailyPeriods := buildDailySavingsPeriods(minStart, maxEnd, periods, siteLoc)
	type dailyTracker struct {
		period          savingsPeriod
		stats           types.SavingsStats
		hasStarted      bool
		lastIntervalEnd time.Time
	}
	dailyTrackers := make([]dailyTracker, len(dailyPeriods))
	for i, dp := range dailyPeriods {
		dailyTrackers[i].period = dp
		dailyTrackers[i].stats.Timestamp = dp.Start
		dailyTrackers[i].stats.EndTimestamp = dp.End
	}

	var allIntervals []savingsSubInterval

	for _, stat := range energyStats {
		ts := stat.TSHourStart.Truncate(time.Hour)

		// Collect all price intervals starting within [ts, ts+1h). We check TSStart bounds
		// rather than p.Contains(ts) so we capture all sub-hourly intervals (e.g., 12:30-13:00)
		// instead of only the interval containing the start-of-hour instant ts.
		var hourPrices []types.Price
		for _, p := range prices {
			if !p.TSStart.Before(ts) && p.TSStart.Before(ts.Add(time.Hour)) {
				hourPrices = append(hourPrices, p)
			}
		}

		var intervals []savingsSubInterval
		if len(hourPrices) > 1 {
			sort.Slice(hourPrices, func(i, j int) bool {
				return hourPrices[i].TSStart.Before(hourPrices[j].TSStart)
			})
			for _, hp := range hourPrices {
				dur := hp.TSEnd.Sub(hp.TSStart).Hours()
				scaledStat := types.EnergyStats{
					TSHourStart:       hp.TSStart,
					MinBatterySOC:     stat.MinBatterySOC,
					MaxBatterySOC:     stat.MaxBatterySOC,
					BatteryChargedKWH: stat.BatteryChargedKWH * dur,
					BatteryUsedKWH:    stat.BatteryUsedKWH * dur,
					SolarKWH:          stat.SolarKWH * dur,
					HomeKWH:           stat.HomeKWH * dur,
					GridExportKWH:     stat.GridExportKWH * dur,
					GridImportKWH:     stat.GridImportKWH * dur,
					VPPExportKWH:      stat.VPPExportKWH * dur,
					BatteryToHomeKWH:  stat.BatteryToHomeKWH * dur,
					SolarToHomeKWH:    stat.SolarToHomeKWH * dur,
					SolarToBatteryKWH: stat.SolarToBatteryKWH * dur,
					SolarToGridKWH:    stat.SolarToGridKWH * dur,
					BatteryToGridKWH:  stat.BatteryToGridKWH * dur,
					Alarms:            stat.Alarms,
				}
				intervals = append(intervals, savingsSubInterval{
					stat:            scaledStat,
					price:           hp,
					duration:        dur,
					gridImportPrice: hp.ImportRateDollars(),
					gridExportPrice: hp.ExportRateDollars(settings.Settings, prices),
				})
			}
		} else if len(hourPrices) > 0 {
			currentPrice := hourPrices[0]
			intervals = append(intervals, savingsSubInterval{
				stat:            stat,
				price:           currentPrice,
				duration:        1.0,
				gridImportPrice: currentPrice.ImportRateDollars(),
				gridExportPrice: currentPrice.ExportRateDollars(settings.Settings, prices),
			})
		} else {
			continue
		}

		hourStartSOC := actualSOC
		hourEndSOC := actualSOC
		// Anchor hourEndSOC to reported SOC telemetry each hour so cumulative inverter
		// conversion losses, BMS standby draw, and meter rounding do not cause actualSOC
		// to drift over multi-day windows.
		if stat.MinBatterySOC > 0 || stat.MaxBatterySOC > 0 {
			if stat.BatteryChargedKWH > stat.BatteryUsedKWH {
				hourEndSOC = stat.MaxBatterySOC
			} else if stat.BatteryUsedKWH > stat.BatteryChargedKWH && stat.MinBatterySOC > 0 {
				hourEndSOC = stat.MinBatterySOC
			} else if stat.MaxBatterySOC > 0 {
				hourEndSOC = stat.MaxBatterySOC
			}
		} else {
			hourEndSOC = max(0, min(100, actualSOC+(stat.BatteryChargedKWH-stat.BatteryUsedKWH)/capacityKWH*100.0))
		}
		var cumDur float64

		for _, interval := range intervals {
			allIntervals = append(allIntervals, interval)
			subStat := interval.stat
			subTS := subStat.TSHourStart

			if !subTS.Before(maxEnd) {
				// Interval is in the post-maxEnd lookahead window (used only for next-day valuation).
				continue
			}

			prevActualSOC := actualSOC
			prevShadowSOC := shadowSOC

			cumDur += interval.duration
			if cumDur > 1.0 {
				cumDur = 1.0
			}
			actualSOC = hourStartSOC + cumDur*(hourEndSOC-hourStartSOC)

			effectiveHomeKWH := max(subStat.HomeKWH, subStat.SolarToHomeKWH+subStat.BatteryToHomeKWH)
			directSolarToHomeKWH := resolveDirectSolarToHomeKWH(subStat, effectiveHomeKWH)
			minSOC := baselineReserveForTime(subTS)

			ignoredFraction := getShadowIgnoredFraction(subTS, time.Duration(interval.duration*float64(time.Hour)), actions)
			// TODO: We do not specifically account for increased bonus credits/income from VPP event
			// participation yet; VPP exports are skipped
			if subStat.VPPExportKWH > 0 {
				ignoredFraction = 1.0
			}
			activeFraction := 1.0 - ignoredFraction

			simShadowSOC, simImportKWH, simExportKWH := stepShadowSelfConsumption(
				prevShadowSOC,
				effectiveHomeKWH,
				subStat.SolarKWH,
				directSolarToHomeKWH,
				minSOC,
				capacityKWH,
				oneWayEff,
				settings.GridExportSolar,
			)

			// Natural convergence sync: when both real and shadow batteries are empty at reserve or full,
			// snap simShadowSOC to actualSOC to prevent efficiency approximation drift over long windows.
			if (math.Abs(actualSOC-minSOC) <= 0.5 && math.Abs(simShadowSOC-minSOC) <= 0.5) || (actualSOC >= 99.0 && simShadowSOC >= 99.0) {
				simShadowSOC = actualSOC
			}

			shadowImportKWH := activeFraction*simImportKWH + ignoredFraction*subStat.GridImportKWH
			shadowExportKWH := activeFraction*simExportKWH + ignoredFraction*subStat.GridExportKWH
			rawShadowSOC := prevShadowSOC + activeFraction*(simShadowSOC-prevShadowSOC) + ignoredFraction*(actualSOC-prevActualSOC)
			effectiveFloorSOC := min(minSOC, actualSOC)
			// TODO: When an ignored interval (such as a VPP export event) drains the actual battery
			// more than the shadow battery had stored, we currently force the shadow battery to buy
			// that shortfall during the VPP export interval at that interval's import price.
			// This is a simple approximation, not entirely accurate: in reality, the ESS in native
			// self-consumption mode would force a grid charge ahead of the VPP event (typically up
			// to ~2 hours before the event starts) rather than during the VPP export interval itself.
			if ignoredFraction > 0 && rawShadowSOC < effectiveFloorSOC {
				shortfallSOC := effectiveFloorSOC - rawShadowSOC
				shadowImportKWH += ((shortfallSOC / 100.0) * capacityKWH) / oneWayEff
				rawShadowSOC = effectiveFloorSOC
			}
			shadowSOC = max(0, min(100, rawShadowSOC))
			deltaRRKWH := ((actualSOC - prevActualSOC) - (shadowSOC - prevShadowSOC)) / 100.0 * capacityKWH

			for dIdx := range dailyTrackers {
				dt := &dailyTrackers[dIdx]
				if !subTS.Before(dt.period.Start) && subTS.Before(dt.period.End) {
					if !dt.hasStarted {
						dt.hasStarted = true
						dt.stats.StartBatterySOC = prevActualSOC
						dt.stats.StartShadowSOC = prevShadowSOC
						dt.stats.BatteryCapacityKWH = capacityKWH
					}
					dt.lastIntervalEnd = subTS.Add(time.Duration(interval.duration * float64(time.Hour)))
					dt.stats.EndBatterySOC = actualSOC
					dt.stats.EndShadowSOC = shadowSOC
					dt.stats.StoredEnergyDeltaKWH += deltaRRKWH

					dt.stats.HomeUsed += effectiveHomeKWH
					dt.stats.SolarGenerated += subStat.SolarKWH
					dt.stats.GridImported += subStat.GridImportKWH
					dt.stats.GridExported += subStat.GridExportKWH
					dt.stats.BatteryUsed += subStat.BatteryUsedKWH

					dt.stats.Cost += subStat.GridImportKWH * interval.gridImportPrice
					dt.stats.Credit += subStat.GridExportKWH * interval.gridExportPrice
					dt.stats.GridOnlyCostDollars += effectiveHomeKWH * interval.gridImportPrice
					dt.stats.SelfConsumptionCostDollars += shadowImportKWH * interval.gridImportPrice
					dt.stats.SelfConsumptionCreditDollars += shadowExportKWH * interval.gridExportPrice
					dt.stats.SolarSavings += subStat.SolarToHomeKWH * interval.gridImportPrice
					dt.stats.AvoidedCost += subStat.BatteryToHomeKWH * interval.gridImportPrice
					break
				}
			}
		}
	}

	// Finalize per-day SavingsStats and roll up into requested SavingsPeriods.
	// TODO: Consider using controller.BuildHourlyEnergyModel to forecast next-day load and solar
	// when evaluating current-day windows if a 24h plan is not available.
	dailyStats := make([]types.SavingsStats, len(dailyTrackers))
	for dIdx := range dailyTrackers {
		dt := &dailyTrackers[dIdx]
		if dt.hasStarted {
			valPeriod := dt.period
			if !dt.lastIntervalEnd.IsZero() && dt.lastIntervalEnd.Before(valPeriod.End) {
				valPeriod.End = dt.lastIntervalEnd
			}
			dt.stats.NextDayValuationDollarsPerKWH = calculateNextDayValuationPrice(
				valPeriod,
				allIntervals,
				latestActionWithPlan,
				dt.stats,
				prices,
				oneWayEff,
			)
		}
		finalizeSavingsStats(&dt.stats)
		dailyStats[dIdx] = dt.stats
	}

	results := make([]types.SavingsStats, len(periods))
	for pIdx, p := range periods {
		results[pIdx] = rollupDailySavingsStats(p, dailyStats)
	}

	return results, filteredActions, nil
}

type savingsSubInterval struct {
	stat            types.EnergyStats
	price           types.Price
	duration        float64
	gridImportPrice float64
	gridExportPrice float64
}

func calculateNextDayValuationPrice(
	p savingsPeriod,
	allIntervals []savingsSubInterval,
	latestActionWithPlan types.Action,
	periodStats types.SavingsStats,
	prices []types.Price,
	oneWayEff float64,
) float64 {
	if oneWayEff <= 0 || oneWayEff > 1.0 {
		oneWayEff = math.Sqrt(0.90)
	}
	nextDayEnd := p.End.Add(24 * time.Hour)
	usableCapKWH := periodStats.BatteryCapacityKWH * 0.80
	if usableCapKWH <= 0 {
		usableCapKWH = 10.0
	}

	// 1. Check actual next-24h intervals [p.End, p.End + 24h)
	var nextLoadCost, nextLoadKWH float64
	var grossLoadCost, grossLoadKWH float64
	var solarSurplusKWH, solarExportWeighted, solarExportWeight float64
	for _, iv := range allIntervals {
		ts := iv.stat.TSHourStart
		if !ts.Before(p.End) && ts.Before(nextDayEnd) {
			homeKWH := max(iv.stat.HomeKWH, iv.stat.SolarToHomeKWH+iv.stat.BatteryToHomeKWH)
			grossLoadCost += homeKWH * iv.gridImportPrice
			grossLoadKWH += homeKWH

			directSolarKWH := resolveDirectSolarToHomeKWH(iv.stat, homeKWH)
			netLoadKWH := max(0, homeKWH-directSolarKWH)
			solarSurplus := max(0, iv.stat.SolarKWH-directSolarKWH)
			if solarSurplus > 0 {
				solarSurplusKWH += solarSurplus * oneWayEff
				solarExportWeighted += solarSurplus * iv.gridExportPrice
				solarExportWeight += solarSurplus
			}
			// Only accumulate unmet load before solar surplus refills the battery.
			if solarSurplusKWH < usableCapKWH && netLoadKWH > 0 {
				nextLoadCost += netLoadKWH * iv.gridImportPrice
				nextLoadKWH += netLoadKWH
			}
		}
	}
	if solarSurplusKWH >= usableCapKWH && solarExportWeight > 0 {
		avgSolarExp := solarExportWeighted / solarExportWeight
		avgMorningImp := 0.0
		if nextLoadKWH > 0 {
			avgMorningImp = nextLoadCost / nextLoadKWH
		}
		wLoad := min(1.0, nextLoadKWH/usableCapKWH)
		return wLoad*avgMorningImp + (1.0-wLoad)*avgSolarExp
	}
	if nextLoadKWH > 0 {
		return nextLoadCost / nextLoadKWH
	}
	if grossLoadKWH > 0 {
		return grossLoadCost / grossLoadKWH
	}

	// 2. Check latestActionWithPlan future periods (only when p.End is near/within the plan horizon)
	if latestActionWithPlan.Plan != nil && len(latestActionWithPlan.Plan.Periods) > 0 {
		planStart := latestActionWithPlan.Plan.Periods[0].TSStart
		var planLoadCost, planLoadKWH float64
		for _, pp := range latestActionWithPlan.Plan.Periods {
			if !pp.TSStart.Before(p.End) && pp.TSStart.Before(nextDayEnd) {
				netLoad := max(0, pp.LoadKWH-pp.SolarKWH)
				if netLoad > 0 {
					planLoadCost += netLoad * pp.ImportDollars
					planLoadKWH += netLoad
				}
			}
		}
		if planLoadKWH > 0 {
			return planLoadCost / planLoadKWH
		}
		for _, pp := range latestActionWithPlan.Plan.Periods {
			if !pp.TSStart.Before(p.End) && pp.TSStart.Before(nextDayEnd) {
				planLoadCost += pp.LoadKWH * pp.ImportDollars
				planLoadKWH += pp.LoadKWH
			}
		}
		if planLoadKWH > 0 {
			return planLoadCost / planLoadKWH
		}
		if !p.End.Before(planStart) {
			// If p.End is at the end of the plan horizon, use all periods in the plan
			for _, pp := range latestActionWithPlan.Plan.Periods {
				planLoadCost += pp.LoadKWH * pp.ImportDollars
				planLoadKWH += pp.LoadKWH
			}
			if planLoadKWH > 0 {
				return planLoadCost / planLoadKWH
			}
		}
	}

	// 3. Fallback to the period's own load-weighted average import price
	if periodStats.HomeUsed > 0 && periodStats.GridOnlyCostDollars > 0 {
		return periodStats.GridOnlyCostDollars / periodStats.HomeUsed
	}

	// 4. Fallback to simple average import price in the period
	var priceSum float64
	var priceCount int
	for _, pr := range prices {
		if !pr.TSStart.Before(p.Start) && pr.TSStart.Before(p.End) {
			priceSum += pr.ImportRateDollars()
			priceCount++
		}
	}
	if priceCount > 0 {
		return priceSum / float64(priceCount)
	}
	return 0
}

// isShadowIgnoredAction returns true when the interval is under a pause, emergency mode,
// grid outage, or active VPP dispatch. Note that ActionReasonVPPPrep is intentionally NOT
// ignored here so RateRudder gets credit for pre-charging during cheap off-peak hours ahead
// of a VPP event rather than forcing a charge at expensive pre-event/event rates.
func isShadowIgnoredAction(a types.Action) bool {
	return a.Paused ||
		a.Reason == types.ActionReasonEmergencyMode ||
		a.SystemStatus.EmergencyMode ||
		a.Reason == types.ActionReasonGridUnavailable ||
		a.SystemStatus.GridUnavailable ||
		a.Reason == types.ActionReasonVPPActive ||
		a.SystemStatus.VPPActive
}

// getShadowIgnoredFraction calculates the fraction of the interval [hStart, hStart+dur)
// where the system was paused, in emergency mode (storm hedge), in a grid outage, or in a VPP event.
func getShadowIgnoredFraction(hStart time.Time, dur time.Duration, actions []types.Action) float64 {
	if dur <= 0 || len(actions) == 0 {
		return 0
	}
	hEnd := hStart.Add(dur)
	ignoredDuration := time.Duration(0)

	var lastStateIgnored bool
	var lastActionTime time.Time

	for _, a := range actions {
		if a.Timestamp.Before(hStart) {
			lastStateIgnored = isShadowIgnoredAction(a)
			lastActionTime = a.Timestamp
		} else if a.Timestamp.Before(hEnd) {
			if lastStateIgnored {
				startPeriod := hStart
				if lastActionTime.After(hStart) {
					startPeriod = lastActionTime
				}
				ignoredDuration += a.Timestamp.Sub(startPeriod)
			}
			lastStateIgnored = isShadowIgnoredAction(a)
			lastActionTime = a.Timestamp
		} else {
			break
		}
	}

	if lastStateIgnored {
		startPeriod := hStart
		if lastActionTime.After(hStart) {
			startPeriod = lastActionTime
		}
		if startPeriod.Before(hEnd) {
			ignoredDuration += hEnd.Sub(startPeriod)
		}
	}

	return max(0, min(1, float64(ignoredDuration)/float64(dur)))
}

// buildDailySavingsPeriods slices the overall range [minStart, maxEnd) into contiguous,
// non-overlapping 1-day savingsPeriod buckets in the site's local timezone (siteLoc).
//
// Why this exists:
// Callers typically request overlapping multi-day windows in a single call—for example:
//   - Period 0 (Today):        [Oct 13 00:00, Oct 14 00:00)
//   - Period 1 (Last 7 Days):  [Oct 07 00:00, Oct 14 00:00)
//   - Period 2 (Last 30 Days): [Sep 14 00:00, Oct 14 00:00)
//
// Instead of tracking those 3 overlapping windows separately during the simulation,
// buildDailySavingsPeriods turns [Sep 14 00:00, Oct 14 00:00) into 30 individual 1-day
// buckets ([Sep 14, Sep 15), [Sep 15, Sep 16), ..., [Oct 13, Oct 14)). Each 1-day bucket
// can be cached independently in Firestore and rolled up via rollupDailySavingsStats into
// any of the requested periods (Today = last 1 bucket, 7d = last 7 buckets, 30d = all 30 buckets).
//
// How it works:
//  1. Collects daily cut points by stepping 1 calendar day at a time from minStart to maxEnd
//     in siteLoc (using AddDate(0, 0, 1) so 23h/25h DST transition days stay aligned to local midnight).
//  2. Also inserts every explicit p.Start and p.End from periods into the cut-point set so no
//     bucket ever straddles the boundary of a requested period.
//  3. Sorts the unique cut points and pairs adjacent timestamps (splits[i], splits[i+1]) into
//     contiguous [Start, End) buckets.
func buildDailySavingsPeriods(minStart, maxEnd time.Time, periods []savingsPeriod, siteLoc *time.Location) []savingsPeriod {
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	// splitSet deduplicates cut points by UnixNano timestamp.
	splitSet := make(map[int64]time.Time)
	addSplit := func(t time.Time) {
		if !t.Before(minStart) && !t.After(maxEnd) {
			splitSet[t.UnixNano()] = t.In(siteLoc)
		}
	}

	// 1. Add outer bounds and step 1 local calendar day at a time across [minStart, maxEnd).
	addSplit(minStart)
	addSplit(maxEnd)
	for cur := minStart.In(siteLoc); cur.Before(maxEnd); {
		next := cur.AddDate(0, 0, 1)
		if !next.After(cur) {
			next = cur.Add(24 * time.Hour)
		}
		if next.Before(maxEnd) {
			addSplit(next)
		}
		cur = next
	}

	// 2. Include all explicit period start/end boundaries so every requested period aligns
	// with exact bucket boundaries.
	for _, p := range periods {
		addSplit(p.Start)
		addSplit(p.End)
	}

	// 3. Sort cut points chronologically and pair adjacent points into [splits[i], splits[i+1]) buckets.
	splits := make([]time.Time, 0, len(splitSet))
	for _, t := range splitSet {
		splits = append(splits, t)
	}
	slices.SortFunc(splits, func(a, b time.Time) int {
		return a.Compare(b)
	})

	if len(splits) < 2 {
		return []savingsPeriod{{Start: minStart, End: maxEnd}}
	}

	daily := make([]savingsPeriod, 0, len(splits)-1)
	for i := 0; i < len(splits)-1; i++ {
		if splits[i].Before(splits[i+1]) {
			daily = append(daily, savingsPeriod{
				Start: splits[i],
				End:   splits[i+1],
			})
		}
	}
	return daily
}

func finalizeSavingsStats(st *types.SavingsStats) {
	clampNearZero := func(v float64) float64 {
		if math.Abs(v) < 1e-9 {
			return 0
		}
		return v
	}

	st.StoredEnergyDeltaKWH = clampNearZero(st.StoredEnergyDeltaKWH)
	st.StoredEnergyValueDollars = clampNearZero(st.StoredEnergyDeltaKWH * st.NextDayValuationDollarsPerKWH)

	actualNetCost := st.Cost - st.Credit
	selfConsumptionNetCost := st.SelfConsumptionCostDollars - st.SelfConsumptionCreditDollars

	st.SolarAndBatterySavingsDollars = clampNearZero(st.GridOnlyCostDollars - selfConsumptionNetCost)
	st.RateRudderSavingsDollars = clampNearZero((selfConsumptionNetCost - actualNetCost) + st.StoredEnergyValueDollars)
	st.TotalSavingsDollars = clampNearZero(st.SolarAndBatterySavingsDollars + st.RateRudderSavingsDollars)
	st.BatterySavings = clampNearZero(st.TotalSavingsDollars - st.SolarSavings)
}

// rollupDailySavingsStats aggregates per-day SavingsStats within [p.Start, p.End) into a single period SavingsStats.
// This allows cached daily SavingsStats to be combined with newly calculated days without re-simulating cached days.
func rollupDailySavingsStats(p savingsPeriod, dailyStats []types.SavingsStats) types.SavingsStats {
	agg := types.SavingsStats{
		Timestamp:    p.Start,
		EndTimestamp: p.End,
	}
	hasFirstActiveDay := false
	for _, d := range dailyStats {
		if d.Timestamp.Before(p.Start.Add(-time.Minute)) || d.EndTimestamp.After(p.End.Add(time.Minute)) {
			continue
		}
		agg.HomeUsed += d.HomeUsed
		agg.SolarGenerated += d.SolarGenerated
		agg.GridImported += d.GridImported
		agg.GridExported += d.GridExported
		agg.BatteryUsed += d.BatteryUsed
		agg.Cost += d.Cost
		agg.Credit += d.Credit
		agg.GridOnlyCostDollars += d.GridOnlyCostDollars
		agg.SelfConsumptionCostDollars += d.SelfConsumptionCostDollars
		agg.SelfConsumptionCreditDollars += d.SelfConsumptionCreditDollars
		agg.SolarSavings += d.SolarSavings
		agg.AvoidedCost += d.AvoidedCost
		agg.StoredEnergyDeltaKWH += d.StoredEnergyDeltaKWH
		if d.Estimated {
			agg.Estimated = true
		}
		if d.BatteryCapacityKWH > 0 {
			if !hasFirstActiveDay {
				hasFirstActiveDay = true
				agg.StartBatterySOC = d.StartBatterySOC
				agg.StartShadowSOC = d.StartShadowSOC
			}
			agg.EndBatterySOC = d.EndBatterySOC
			agg.EndShadowSOC = d.EndShadowSOC
			agg.BatteryCapacityKWH = d.BatteryCapacityKWH
			agg.NextDayValuationDollarsPerKWH = d.NextDayValuationDollarsPerKWH
		}
	}
	finalizeSavingsStats(&agg)
	return agg
}
