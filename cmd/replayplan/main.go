package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/raterudder/raterudder/pkg/utility"
)

func main() {
	siteFlag := lflag.String("site", "", "Site ID to replay (required)")
	dateFlag := lflag.String("date", "", "Single local date (YYYY-MM-DD) to replay (optional)")
	startFlag := lflag.String("start", "", "Start date (YYYY-MM-DD) for replay window (optional)")
	endFlag := lflag.String("end", "", "End date (YYYY-MM-DD) for replay window (optional)")
	daysFlag := lflag.Int("days", 1, "Number of past days to replay if --date/--start not set (default: 1)")
	detailAtFlag := lflag.String("detail-at", "", "Comma-separated HH:MM (or YYYY-MM-DD HH:MM, or 'all') timestamps to print full horizon schedule")
	maxPeriodsFlag := lflag.Int("max-periods", 36, "Maximum number of horizon periods to print per detailed schedule (default: 36)")
	showRecordedPlanFlag := lflag.Bool("show-recorded-plan", false, "Also print the recorded Firestore plan.Periods at --detail-at timestamps")
	chainActionsFlag := lflag.Bool("chain-actions", true, "Feed each replayed action as lastAction into the next poll (false uses recorded previous action)")
	verboseLogsFlag := lflag.Bool("verbose-logs", false, "Emit structured slog output from controller.Plan (default: false)")

	// Optional setting overrides
	minSOCFlag := lflag.String("min-soc", "", "Override settings.MinBatterySOC (optional)")
	periodMinSOCFlag := lflag.String("period-min-soc", "", "Override MinBatterySOCPeriods, e.g. 'On-Peak=20,Off-Peak=50' (optional)")
	exportMarginDollarsFlag := lflag.String("export-margin", "", "Override settings.MinBatteryExportDifferenceDollarsPerKWH (optional)")
	arbitrageDiffDollarsFlag := lflag.String("arbitrage-diff", "", "Override settings.MinArbitrageDifferenceDollarsPerKWH (optional)")
	alwaysChargeUnderDollarsFlag := lflag.String("always-charge-under", "", "Override settings.AlwaysChargeUnderDollarsPerKWH (optional)")
	gridChargeFlag := lflag.String("grid-charge", "", "Override settings.GridChargeBatteries ('true' or 'false')")
	gridExportBatteriesFlag := lflag.String("grid-export-batteries", "", "Override settings.GridExportBatteries ('true' or 'false')")
	gridExportSolarFlag := lflag.String("grid-export-solar", "", "Override settings.GridExportSolar ('true' or 'false')")

	if os.Getenv("FIRESTORE_PROJECT_ID") == "" {
		_ = os.Setenv("FIRESTORE_PROJECT_ID", "raterudder")
	}
	if os.Getenv("STORAGE_PROVIDER") == "" {
		_ = os.Setenv("STORAGE_PROVIDER", "firestore")
	}

	db := storage.Configured()
	lflag.Configure()

	var (
		siteID, dateStr, startDateStr, endDateStr string
		detailAtStr, periodMinSOCStr              string
		minSOCStr, exportMarginStr                string
		arbitrageDiffStr, alwaysChargeStr         string
		gridChargeStr, gridExportBatStr           string
		gridExportSolStr                          string
		days, maxPeriods                          int
		showRecordedPlan, chainActions            bool
		verboseLogs                               bool
	)

	lflag.Do(func() {
		siteID = strings.TrimSpace(*siteFlag)
		dateStr = strings.TrimSpace(*dateFlag)
		startDateStr = strings.TrimSpace(*startFlag)
		endDateStr = strings.TrimSpace(*endFlag)
		days = *daysFlag
		detailAtStr = strings.TrimSpace(*detailAtFlag)
		maxPeriods = *maxPeriodsFlag
		showRecordedPlan = *showRecordedPlanFlag
		chainActions = *chainActionsFlag
		verboseLogs = *verboseLogsFlag
		minSOCStr = strings.TrimSpace(*minSOCFlag)
		periodMinSOCStr = strings.TrimSpace(*periodMinSOCFlag)
		exportMarginStr = strings.TrimSpace(*exportMarginDollarsFlag)
		arbitrageDiffStr = strings.TrimSpace(*arbitrageDiffDollarsFlag)
		alwaysChargeStr = strings.TrimSpace(*alwaysChargeUnderDollarsFlag)
		gridChargeStr = strings.TrimSpace(*gridChargeFlag)
		gridExportBatStr = strings.TrimSpace(*gridExportBatteriesFlag)
		gridExportSolStr = strings.TrimSpace(*gridExportSolarFlag)
	})

	if !verboseLogs {
		log.SetDefaultLogLevel(slog.LevelError)
	}

	if siteID == "" {
		fmt.Fprintln(os.Stderr, "Error: --site flag is required.")
		fmt.Fprintf(os.Stderr, "Usage: %s --site=<siteID> [--date=YYYY-MM-DD] [--detail-at=HH:MM,HH:MM]\n", os.Args[0])
		os.Exit(1)
	}

	ctx := context.Background()
	defer func() {
		_ = db.Close()
	}()

	settings, _, _, err := db.GetSettings(ctx, siteID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch settings for site %q: %v\n", siteID, err)
		os.Exit(1)
	}

	// Apply in-memory setting overrides
	settings.Pause = false
	var overridesApplied []string
	if minSOCStr != "" {
		val, err := strconv.ParseFloat(minSOCStr, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --min-soc value %q: %v\n", minSOCStr, err)
			os.Exit(1)
		}
		settings.MinBatterySOC = val
		overridesApplied = append(overridesApplied, fmt.Sprintf("MinBatterySOC=%.1f%%", val))
	}
	if periodMinSOCStr != "" {
		pairs := strings.Split(periodMinSOCStr, ",")
		for _, pair := range pairs {
			kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(kv) != 2 {
				continue
			}
			pName := strings.TrimSpace(kv[0])
			val, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid --period-min-soc entry %q: %v\n", pair, err)
				os.Exit(1)
			}
			matched := false
			for i := range settings.MinBatterySOCPeriods {
				if strings.EqualFold(settings.MinBatterySOCPeriods[i].UtilityPeriodName, pName) {
					settings.MinBatterySOCPeriods[i].MinBatterySOC = val
					matched = true
				}
			}
			if !matched {
				settings.MinBatterySOCPeriods = append(settings.MinBatterySOCPeriods, types.MinBatterySOCPeriod{
					UtilityPeriodName: pName,
					MinBatterySOC:     val,
				})
			}
			overridesApplied = append(overridesApplied, fmt.Sprintf("PeriodMinSOC[%s]=%.1f%%", pName, val))
		}
	}
	if exportMarginStr != "" {
		val, err := strconv.ParseFloat(exportMarginStr, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --export-margin value %q: %v\n", exportMarginStr, err)
			os.Exit(1)
		}
		settings.MinBatteryExportDifferenceDollarsPerKWH = val
		overridesApplied = append(overridesApplied, fmt.Sprintf("ExportMargin=$%.4f", val))
	}
	if arbitrageDiffStr != "" {
		val, err := strconv.ParseFloat(arbitrageDiffStr, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --arbitrage-diff value %q: %v\n", arbitrageDiffStr, err)
			os.Exit(1)
		}
		settings.MinArbitrageDifferenceDollarsPerKWH = val
		overridesApplied = append(overridesApplied, fmt.Sprintf("ArbitrageDiff=$%.4f", val))
	}
	if alwaysChargeStr != "" {
		val, err := strconv.ParseFloat(alwaysChargeStr, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --always-charge-under value %q: %v\n", alwaysChargeStr, err)
			os.Exit(1)
		}
		settings.AlwaysChargeUnderDollarsPerKWH = val
		overridesApplied = append(overridesApplied, fmt.Sprintf("AlwaysChargeUnder=$%.4f", val))
	}
	if gridChargeStr != "" {
		b, err := strconv.ParseBool(gridChargeStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --grid-charge value %q: %v\n", gridChargeStr, err)
			os.Exit(1)
		}
		settings.GridChargeBatteries = b
		overridesApplied = append(overridesApplied, fmt.Sprintf("GridChargeBatteries=%v", b))
	}
	if gridExportBatStr != "" {
		b, err := strconv.ParseBool(gridExportBatStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --grid-export-batteries value %q: %v\n", gridExportBatStr, err)
			os.Exit(1)
		}
		settings.GridExportBatteries = b
		overridesApplied = append(overridesApplied, fmt.Sprintf("GridExportBatteries=%v", b))
	}
	if gridExportSolStr != "" {
		b, err := strconv.ParseBool(gridExportSolStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --grid-export-solar value %q: %v\n", gridExportSolStr, err)
			os.Exit(1)
		}
		settings.GridExportSolar = b
		overridesApplied = append(overridesApplied, fmt.Sprintf("GridExportSolar=%v", b))
	}

	latestAction, _ := db.GetLatestAction(ctx, siteID)
	loc := time.UTC
	if settings.Location != nil && settings.Location.TimeZone != "" {
		if l, err := time.LoadLocation(settings.Location.TimeZone); err == nil {
			loc = l
		}
	} else if latestAction != nil && latestAction.SystemStatus.TimeLocation != "" {
		if l, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
			loc = l
		}
	}

	now := time.Now().In(loc)
	var start, end time.Time
	if dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --date format (expected YYYY-MM-DD): %v\n", err)
			os.Exit(1)
		}
		start = parsed
		end = parsed.AddDate(0, 0, 1)
	} else {
		if startDateStr != "" {
			parsedStart, err := time.ParseInLocation("2006-01-02", startDateStr, loc)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid --start format (expected YYYY-MM-DD): %v\n", err)
				os.Exit(1)
			}
			start = parsedStart
		} else {
			start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
		}
		if endDateStr != "" {
			parsedEnd, err := time.ParseInLocation("2006-01-02", endDateStr, loc)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid --end format (expected YYYY-MM-DD): %v\n", err)
				os.Exit(1)
			}
			end = parsedEnd.AddDate(0, 0, 1)
		} else {
			end = now.Add(time.Hour)
		}
	}

	uMap := utility.NewMap(db)
	u, err := uMap.Site(ctx, siteID, settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize utility (%s): %v\n", settings.UtilityProvider, err)
		os.Exit(1)
	}

	// Fetch actions including 2 hours before start so we have the prior action for the first poll
	allActions, err := db.GetActionHistory(ctx, siteID, start.Add(-2*time.Hour), end)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch action history: %v\n", err)
		os.Exit(1)
	}

	var prevRecordedAction *types.Action
	var actions []types.Action
	for i := range allActions {
		a := allActions[i]
		if a.Timestamp.Before(start) {
			prevRecordedAction = &allActions[i]
		} else {
			actions = append(actions, a)
		}
	}

	if len(actions) == 0 {
		fmt.Printf("No actions found for site %s in window [%s, %s) (%s).\n",
			siteID, start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04"), loc.String())
		return
	}

	// Load combined 45-day energy & weather history (summaries + unsummarized)
	histStart := start.AddDate(0, 0, -45)
	flatEnergyHist, weatherHist := loadCombinedHistory(ctx, db, siteID, histStart, end, loc)

	// Load stored price history covering 7 days before start through 48h after end
	storedPrices, _ := db.GetPriceHistory(ctx, siteID, start.Add(-168*time.Hour), end.Add(48*time.Hour))

	detailSet := make(map[string]bool)
	detailAll := false
	if detailAtStr != "" {
		if strings.EqualFold(detailAtStr, "all") {
			detailAll = true
		} else {
			for _, tok := range strings.Split(detailAtStr, ",") {
				tok = strings.TrimSpace(tok)
				if tok != "" {
					detailSet[tok] = true
				}
			}
		}
	}

	fmt.Printf("=== REPLAYING PLAN FOR SITE %s (%s) ===\n", siteID, loc.String())
	fmt.Printf("Window:         %s to %s (%d polls)\n", start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04"), len(actions))
	fmt.Printf("Utility:        %s (Rate: %s) | ESS: %s | PlanMode=%v\n",
		settings.UtilityProvider, settings.UtilityRate, settings.ESS, settings.PlanMode)
	fmt.Printf("Settings:       MinSOC=%.1f%% | Periods=%+v | ExportMargin=$%.4f | ArbDiff=$%.4f | AlwaysChargeUnder=$%.4f\n",
		settings.MinBatterySOC, settings.MinBatterySOCPeriods,
		settings.MinBatteryExportDifferenceDollarsPerKWH,
		settings.MinArbitrageDifferenceDollarsPerKWH,
		settings.AlwaysChargeUnderDollarsPerKWH)
	if len(overridesApplied) > 0 {
		fmt.Printf("Overrides:      %s\n", strings.Join(overridesApplied, ", "))
	}
	fmt.Println()

	c := controller.NewController()
	simLastAction := prevRecordedAction

	for i, a := range actions {
		t := a.Timestamp.In(loc)
		hhmm := t.Format("15:04")
		fullTS := t.Format("2006-01-02 15:04")

		if a.Reason == types.ActionReasonHasAlarms || a.Reason == types.ActionReasonGridUnavailable || a.Reason == types.ActionReasonEmergencyMode {
			fmt.Printf("[%s] SOC:%5.1f%% | Skipped fault/outage poll (%s: %s)\n",
				fullTS, a.SystemStatus.BatterySOC, a.Reason, a.Description)
			continue
		}

		curPrice, futurePrices, err := buildPricesAt(ctx, u, storedPrices, a, t, loc)
		if err != nil {
			fmt.Printf("[%s] SOC:%5.1f%% | Price build error: %v\n", fullTS, a.SystemStatus.BatterySOC, err)
			continue
		}

		var histBefore []types.EnergyStats
		for _, h := range flatEnergyHist {
			if h.TSHourStart.Before(a.Timestamp) {
				histBefore = append(histBefore, h)
			}
		}

		lastAct := simLastAction
		if !chainActions {
			if i == 0 {
				lastAct = prevRecordedAction
			} else {
				prev := actions[i-1]
				lastAct = &prev
			}
		}

		dec, plan, err := c.Plan(ctx, a.SystemStatus, curPrice, futurePrices, histBefore, weatherHist, settings, lastAct)
		if err != nil {
			fmt.Printf("[%s] SOC:%5.1f%% | Plan error: %v\n", fullTS, a.SystemStatus.BatterySOC, err)
			continue
		}

		replayedAct := dec.Action
		replayedAct.Timestamp = a.Timestamp
		replayedAct.CurrentPrice = &curPrice
		simLastAction = &replayedAct

		recSummary := fmt.Sprintf("Bat=%2d Sol=%d (%-26s)", a.BatteryMode, a.SolarMode, truncateStr(string(a.Reason), 26))
		if a.Paused {
			recSummary = fmt.Sprintf("PAUSED       (%-26s)", truncateStr(a.Description, 26))
		}

		newSummary := fmt.Sprintf("Bat=%2d Sol=%d Target=%3d%% (%-26s)",
			replayedAct.BatteryMode, replayedAct.SolarMode, replayedAct.ChargeToSOC, truncateStr(string(replayedAct.Reason), 26))

		diffMarker := " "
		if !a.Paused && (a.BatteryMode != replayedAct.BatteryMode || a.SolarMode != replayedAct.SolarMode || a.Reason != replayedAct.Reason) {
			diffMarker = "*"
		}

		fmt.Printf("[%s] %s SOC:%5.1f%% (min %2.0f%%) Home:%5.2fkW | Imp:$%.4f Exp:$%.4f | Recorded: %s | Replayed: %s\n",
			fullTS, diffMarker, a.SystemStatus.BatterySOC, a.ReserveSOC, a.SystemStatus.HomeKW,
			curPrice.ImportRateDollars(), curPrice.ExportRateDollars(settings, futurePrices),
			recSummary, newSummary)

		shouldDetail := detailAll || detailSet[hhmm] || detailSet[fullTS]
		if shouldDetail {
			if showRecordedPlan && a.Plan != nil && len(a.Plan.Periods) > 0 {
				fmt.Printf("  --- Recorded Firestore Plan at %s (%d periods) ---\n", fullTS, len(a.Plan.Periods))
				printPlanPeriods(a.Plan.Periods, loc, maxPeriods)
			}
			fmt.Printf("  --- Replayed Plan at %s (%d periods) ---\n", fullTS, len(plan.Periods))
			printPlanPeriods(plan.Periods, loc, maxPeriods)
		}
	}
}

func printPlanPeriods(periods []types.PlanPeriod, loc *time.Location, maxPeriods int) {
	limit := len(periods)
	if maxPeriods > 0 && limit > maxPeriods {
		limit = maxPeriods
	}
	for i := 0; i < limit; i++ {
		p := periods[i]
		fmt.Printf("    [%02d] %s-%s | Bat=%2d Sol=%d | SOC %5.1f%%->%5.1f%% (min %2.0f%%) | Sol:%5.2f Load:%5.2f | Imp:$%.4f Exp:$%.4f | %s\n",
			i,
			p.TSStart.In(loc).Format("15:04"),
			p.TSEnd.In(loc).Format("15:04"),
			p.BatteryMode,
			p.SolarMode,
			p.StartSOC,
			p.EndSOC,
			p.ReserveSOC,
			p.SolarKWH,
			p.LoadKWH,
			p.ImportDollars,
			p.ExportDollars,
			p.Reason,
		)
	}
}

func loadCombinedHistory(
	ctx context.Context,
	db storage.Database,
	siteID string,
	histStart, end time.Time,
	loc *time.Location,
) ([]types.EnergyStats, []types.Weather) {
	summaries, _ := db.GetHistorySummaries(ctx, siteID, histStart, end)
	unsummarizedEnergy, _ := db.GetEnergyHistory(ctx, siteID, histStart, end.AddDate(0, 0, 1))
	unsummarizedWeather, _ := db.GetWeather(ctx, siteID, histStart, end.AddDate(0, 0, 3))

	var allDays []types.DailyEnergyStats
	var allWeather []types.Weather
	for _, s := range summaries {
		allDays = append(allDays, s.Energy...)
		allWeather = append(allWeather, s.Weather...)
	}
	allDays = append(allDays, unsummarizedEnergy...)
	allWeather = append(allWeather, unsummarizedWeather...)

	slices.SortFunc(allDays, func(a, b types.DailyEnergyStats) int {
		return a.TSDayStart.Compare(b.TSDayStart)
	})
	slices.SortFunc(allWeather, func(a, b types.Weather) int {
		return a.TSDayStart.Compare(b.TSDayStart)
	})

	// Deduplicate daily energy stats by date, preferring the entry with more hourly records
	seenEnergy := make(map[string]int)
	var dedupedDays []types.DailyEnergyStats
	for _, day := range allDays {
		dateKey := day.TSDayStart.In(loc).Format("2006-01-02")
		if idx, ok := seenEnergy[dateKey]; ok {
			if len(day.Hourly) >= len(dedupedDays[idx].Hourly) {
				dedupedDays[idx] = day
			}
		} else {
			seenEnergy[dateKey] = len(dedupedDays)
			dedupedDays = append(dedupedDays, day)
		}
	}

	seenWeather := make(map[string]int)
	var dedupedWeather []types.Weather
	for _, w := range allWeather {
		dateKey := w.TSDayStart.Format("2006-01-02")
		if idx, ok := seenWeather[dateKey]; ok {
			if len(w.ForecastHours) >= len(dedupedWeather[idx].ForecastHours) {
				dedupedWeather[idx] = w
			}
		} else {
			seenWeather[dateKey] = len(dedupedWeather)
			dedupedWeather = append(dedupedWeather, w)
		}
	}

	var flatEnergy []types.EnergyStats
	for _, d := range dedupedDays {
		flatEnergy = append(flatEnergy, d.Hourly...)
	}
	return flatEnergy, dedupedWeather
}

func buildPricesAt(
	ctx context.Context,
	u utility.Utility,
	storedPrices []types.Price,
	action types.Action,
	t time.Time,
	loc *time.Location,
) (types.Price, []types.Price, error) {
	hourStart := t.Truncate(time.Hour)
	todayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	tomorrowEnd := todayStart.AddDate(0, 0, 2)

	// Collect candidate prices from stored Firestore price history and utility.GetConfirmedPrices
	var candidates []types.Price
	for _, p := range storedPrices {
		pEnd := p.TSEnd
		if pEnd.IsZero() {
			pEnd = p.TSStart.Add(time.Hour)
		}
		if pEnd.After(hourStart) && p.TSStart.Before(tomorrowEnd.Add(4*time.Hour)) {
			candidates = append(candidates, p)
		}
	}

	if u != nil {
		confirmed, err := u.GetConfirmedPrices(ctx, hourStart, tomorrowEnd.Add(2*time.Hour))
		if err == nil && len(confirmed) > 0 {
			for _, cp := range confirmed {
				cpEnd := cp.TSEnd
				if cpEnd.IsZero() {
					cpEnd = cp.TSStart.Add(time.Hour)
				}
				covered := false
				for _, ex := range candidates {
					exEnd := ex.TSEnd
					if exEnd.IsZero() {
						exEnd = ex.TSStart.Add(time.Hour)
					}
					if ex.TSStart.Before(cpEnd) && exEnd.After(cp.TSStart) {
						covered = true
						break
					}
				}
				if !covered {
					candidates = append(candidates, cp)
				}
			}
		}
	}

	slices.SortStableFunc(candidates, func(a, b types.Price) int {
		return a.TSStart.Compare(b.TSStart)
	})

	// Determine currentPrice
	var curPrice types.Price
	hasCur := false
	if action.CurrentPrice != nil {
		curPrice = *action.CurrentPrice
		hasCur = true
	} else {
		for _, p := range candidates {
			pEnd := p.TSEnd
			if pEnd.IsZero() {
				pEnd = p.TSStart.Add(time.Hour)
			}
			if !p.TSStart.After(t) && pEnd.After(t) {
				curPrice = p
				hasCur = true
				break
			}
		}
		if !hasCur && len(candidates) > 0 {
			curPrice = candidates[0]
			hasCur = true
		}
	}
	if !hasCur {
		return types.Price{}, nil, fmt.Errorf("no current price found at %s", t.Format(time.RFC3339))
	}

	// Build futurePrices starting after hourStart
	var futurePrices []types.Price
	for _, p := range candidates {
		pEnd := p.TSEnd
		if pEnd.IsZero() {
			pEnd = p.TSStart.Add(time.Hour)
		}
		if pEnd.After(hourStart.Add(time.Minute)) && !p.TSStart.Before(hourStart) {
			if p.TSStart.Equal(hourStart) && pEnd.Equal(hourStart.Add(time.Hour)) {
				continue
			}
			futurePrices = append(futurePrices, p)
		}
	}

	hasCoverage := func(checkHour time.Time) bool {
		hourEnd := checkHour.Add(time.Hour)
		for _, p := range futurePrices {
			pEnd := p.TSEnd
			if pEnd.IsZero() {
				pEnd = p.TSStart.Add(time.Hour)
			}
			if p.TSStart.Before(hourEnd) && pEnd.After(checkHour) {
				return true
			}
		}
		return false
	}

	hasAllTomorrow := len(futurePrices) > 0
	if hasAllTomorrow {
		for h := tomorrowStart; h.Before(tomorrowEnd); h = h.Add(time.Hour) {
			if !hasCoverage(h) {
				hasAllTomorrow = false
				break
			}
		}
	}

	// Estimate missing tomorrow hours using 168h stored price history (same logic as Server.getFuturePrices)
	if !hasAllTomorrow {
		histStart := t.Add(-168 * time.Hour)
		var histWindow []types.Price
		for _, p := range storedPrices {
			if !p.TSStart.Before(histStart) && !p.TSStart.After(t) {
				histWindow = append(histWindow, p)
			}
		}
		if len(histWindow) > 0 {
			buckets := make(map[int][]types.Price, 24)
			var overallDollars []float64
			for _, p := range histWindow {
				hr := p.TSStart.In(loc).Hour()
				buckets[hr] = append(buckets[hr], p)
				overallDollars = append(overallDollars, p.DollarsPerKWH)
			}
			for _, p := range futurePrices {
				hr := p.TSStart.In(loc).Hour()
				buckets[hr] = append(buckets[hr], p)
				overallDollars = append(overallDollars, p.DollarsPerKWH)
			}
			overallMed := computeMedian(overallDollars)
			overallTmpl := findClosestPrice(histWindow, overallMed)

			profiles := make(map[int]types.Price, 24)
			for hr := 0; hr < 24; hr++ {
				inHr := buckets[hr]
				if len(inHr) > 0 {
					var d []float64
					for _, p := range inHr {
						d = append(d, p.DollarsPerKWH)
					}
					profiles[hr] = findClosestPrice(inHr, computeMedian(d))
				} else {
					profiles[hr] = overallTmpl
				}
			}

			for h := hourStart.Add(time.Hour); h.Before(tomorrowEnd); h = h.Add(time.Hour) {
				if !hasCoverage(h) {
					est := profiles[h.In(loc).Hour()]
					est.TSStart = h
					est.TSEnd = h.Add(time.Hour)
					futurePrices = append(futurePrices, est)
				}
			}
			slices.SortStableFunc(futurePrices, func(a, b types.Price) int {
				return a.TSStart.Compare(b.TSStart)
			})
		}
	}

	return curPrice, futurePrices, nil
}

func computeMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := slices.Clone(vals)
	slices.Sort(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}

func findClosestPrice(prices []types.Price, target float64) types.Price {
	best := prices[0]
	bestDiff := math.Abs(best.DollarsPerKWH - target)
	for _, p := range prices[1:] {
		diff := math.Abs(p.DollarsPerKWH - target)
		if diff <= bestDiff {
			best = p
			bestDiff = diff
		}
	}
	return best
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
