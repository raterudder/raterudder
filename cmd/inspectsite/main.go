package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
)

func main() {
	siteFlag := lflag.String("site", "", "Site ID to inspect (required)")
	daysFlag := lflag.Int("days", 3, "Number of past days of action history to fetch (default: 3)")
	startFlag := lflag.String("start", "", "Start date (YYYY-MM-DD) for action history (optional)")
	endFlag := lflag.String("end", "", "End date (YYYY-MM-DD) for action history (optional)")
	settingsOnlyFlag := lflag.Bool("settings-only", false, "Only print site settings without action history")

	if os.Getenv("FIRESTORE_PROJECT_ID") == "" {
		os.Setenv("FIRESTORE_PROJECT_ID", "raterudder")
	}
	if os.Getenv("STORAGE_PROVIDER") == "" {
		os.Setenv("STORAGE_PROVIDER", "firestore")
	}

	db := storage.Configured()
	lflag.Configure()

	var siteID, startDateStr, endDateStr string
	var days int
	var settingsOnly bool

	lflag.Do(func() {
		siteID = strings.TrimSpace(*siteFlag)
		days = *daysFlag
		startDateStr = strings.TrimSpace(*startFlag)
		endDateStr = strings.TrimSpace(*endFlag)
		settingsOnly = *settingsOnlyFlag
	})

	if siteID == "" {
		fmt.Fprintln(os.Stderr, "Error: --site flag is required.")
		fmt.Fprintf(os.Stderr, "Usage: %s --site <siteID> [--days=3] [--start=YYYY-MM-DD] [--end=YYYY-MM-DD]\n", os.Args[0])
		os.Exit(1)
	}

	ctx := context.Background()
	defer func() {
		_ = db.Close()
	}()

	settings, version, updatedTime, err := db.GetSettings(ctx, siteID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch settings for site %q: %v\n", siteID, err)
		os.Exit(1)
	}

	fmt.Printf("=== SITE SETTINGS (%s) ===\n", siteID)
	fmt.Printf("Version:        %d (Last Updated: %s)\n", version, updatedTime.Format(time.RFC3339))
	fmt.Printf("Release:        %s\n", settings.Release)
	fmt.Printf("Automation:     Paused=%v\n", settings.Pause)
	fmt.Printf("Engine Mode:    PlanMode=%v\n", settings.PlanMode)
	fmt.Printf("ESS Provider:   %s\n", settings.ESS)
	fmt.Printf("Utility:        %s (Rate: %s)\n", settings.UtilityProvider, settings.UtilityRate)
	fmt.Printf("Grid Settings:  ChargeBatteries=%v, ExportBatteries=%v, ExportSolar=%v\n",
		settings.GridChargeBatteries, settings.GridExportBatteries, settings.GridExportSolar)
	fmt.Printf("Min Battery SOC: %.1f%%\n", settings.MinBatterySOC)
	if len(settings.MinBatterySOCPeriods) > 0 {
		fmt.Printf("Min SOC Periods: %+v\n", settings.MinBatterySOCPeriods)
	}
	fmt.Printf("Thresholds:     AlwaysChargeUnder=$%.4f/kWh, MinArbitrageDiff=$%.4f/kWh\n",
		settings.AlwaysChargeUnderDollarsPerKWH, settings.MinArbitrageDifferenceDollarsPerKWH)
	fmt.Printf("Manage TOU:     %v\n", settings.ManageTOUSchedules)

	if settingsOnly {
		return
	}

	latestAction, err := db.GetLatestAction(ctx, siteID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get latest action: %v\n", err)
	}

	loc := time.UTC
	if latestAction != nil && latestAction.SystemStatus.TimeLocation != "" {
		if l, err := time.LoadLocation(latestAction.SystemStatus.TimeLocation); err == nil {
			loc = l
		}
	}

	now := time.Now().In(loc)
	var start, end time.Time

	if startDateStr != "" {
		parsedStart, err := time.ParseInLocation("2006-01-02", startDateStr, loc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --start date format (expected YYYY-MM-DD): %v\n", err)
			os.Exit(1)
		}
		start = parsedStart
	} else {
		start = now.AddDate(0, 0, -days).Truncate(24 * time.Hour)
	}

	if endDateStr != "" {
		parsedEnd, err := time.ParseInLocation("2006-01-02", endDateStr, loc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid --end date format (expected YYYY-MM-DD): %v\n", err)
			os.Exit(1)
		}
		end = parsedEnd.AddDate(0, 0, 1) // inclusive of the end day
	} else {
		end = now.Add(time.Hour)
	}

	actions, err := db.GetActionHistory(ctx, siteID, start, end)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get action history: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== ACTION HISTORY (%d entries in %s, %s to %s) ===\n",
		len(actions), loc.String(), start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04"))

	if len(actions) == 0 {
		fmt.Println("No actions found in the specified window.")
		return
	}

	for _, a := range actions {
		t := a.Timestamp.In(loc)
		soc := a.SystemStatus.BatterySOC
		impRate := 0.0
		if a.CurrentPrice != nil {
			impRate = a.CurrentPrice.ImportRateDollars()
		}

		modeStr := formatBatteryMode(a.BatteryMode)
		reasonStr := string(a.Reason)
		if reasonStr == "" {
			reasonStr = "-"
		}

		pausedMarker := " "
		if a.Paused {
			pausedMarker = "P"
		}

		fmt.Printf("[%s] [%s] %-12s | SOC: %5.1f%% | Imp: $%6.4f | %-24s | %s\n",
			t.Format("2006-01-02 15:04"),
			pausedMarker,
			modeStr,
			soc,
			impRate,
			reasonStr,
			a.Description,
		)
	}
}

func formatBatteryMode(mode types.BatteryMode) string {
	switch mode {
	case types.BatteryModeLoad:
		return "Load"
	case types.BatteryModeChargeAny:
		return "ChargeAny"
	case types.BatteryModeExport:
		return "Export"
	case types.BatteryModeStandby:
		return "Standby"
	default:
		return fmt.Sprintf("Mode(%d)", mode)
	}
}
