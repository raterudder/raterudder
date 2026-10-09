package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"slices"
)

func main() {
	// Register fetch-specific flags using lflag
	siteID := lflag.String("site", "", "Site ID to fetch history for")
	period := lflag.String("period", "", "Period name for the output file (e.g. march, may)")
	startDate := lflag.String("start", "", "Start date (YYYY-MM-DD) (Inclusive)")
	endDate := lflag.String("end", "", "End date (YYYY-MM-DD) (Exclusive)")
	energyOnly := lflag.Bool("energy", false, "Fetch energy and weather history dataset for energy modeling tests")

	// Configure database
	db := storage.Configured()

	lflag.Configure()

	var sID, p, sDate, eDate string
	var isEnergy bool

	lflag.Do(func() {
		sID = *siteID
		p = *period
		sDate = *startDate
		eDate = *endDate
		isEnergy = *energyOnly
	})

	if sID == "" || p == "" || sDate == "" || eDate == "" {
		fmt.Println("Error: site, period, start, and end flags are required.")
		os.Exit(1)
	}

	start, err := time.Parse("2006-01-02", sDate)
	if err != nil {
		fmt.Printf("Invalid start date: %v\n", err)
		os.Exit(1)
	}

	end, err := time.Parse("2006-01-02", eDate)
	if err != nil {
		fmt.Printf("Invalid end date: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	fp, ok := storage.Unwrap(db).(*storage.FirestoreProvider)
	if !ok || fp.FirestoreClient() == nil {
		fmt.Println("Error: could not retrieve firestore client from storage provider")
		os.Exit(1)
	}
	fsClient := fp.FirestoreClient()

	fmt.Printf("Fetching stable site number for site ID...\n")
	siteNum, err := getOrAssignSiteNumber(ctx, fsClient, sID)
	if err != nil {
		fmt.Printf("Failed to get or assign site number: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Mapped site ID to site%d\n", siteNum)

	if isEnergy {
		fetchAndSaveEnergyDataset(ctx, db, sID, siteNum, p, start, end)
		return
	}

	fmt.Printf("Fetching histories from storage (start=%s, end=%s)...\n", start.Format(time.RFC3339), end.Format(time.RFC3339))
	energyHistory, err := db.GetEnergyHistory(ctx, sID, start, end)
	if err != nil {
		fmt.Printf("Failed to get energy history: %v\n", err)
		os.Exit(1)
	}

	actionHistory, err := db.GetActionHistory(ctx, sID, start, end)
	if err != nil {
		fmt.Printf("Failed to get action history: %v\n", err)
		os.Exit(1)
	}

	priceHistory, err := db.GetPriceHistory(ctx, sID, start, end)
	if err != nil {
		fmt.Printf("Failed to get price history: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded: %d energy days, %d actions, %d price slots.\n", len(energyHistory), len(actionHistory), len(priceHistory))

	dataset := types.ControllerHistoryDataset{
		SiteID:        fmt.Sprintf("site%d", siteNum),
		Period:        p,
		SimStart:      start,
		SimEnd:        end,
		EnergyHistory: energyHistory,
		ActionHistory: actionHistory,
		PriceHistory:  priceHistory,
	}

	outputFilename := fmt.Sprintf("site%d_%s.json", siteNum, p)
	outputPath := filepath.Join("pkg", "controller", "testdata", "history", outputFilename)

	jsonBytes, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		fmt.Printf("Failed to marshal dataset: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Printf("Failed to create directories: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, jsonBytes, 0644); err != nil {
		fmt.Printf("Failed to write output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Saved anonymized history dataset to %s\n", outputPath)
}

// fetchAndSaveEnergyDataset fetches energy and weather history starting 35 days prior to start
// (combining monthly HistorySummary records with recent unsummarized documents) so that Day 1 of
// the [start, end) simulation window has a full 30-day lookback window for BuildHourlyEnergyModel.
// ActionHistory and PriceHistory are omitted to keep the dataset files compact.
func fetchAndSaveEnergyDataset(ctx context.Context, db storage.Database, sID string, siteNum int, period string, start, end time.Time) {
	settings, _, _, err := db.GetSettings(ctx, sID)
	if err != nil {
		fmt.Printf("Failed to get settings: %v\n", err)
		os.Exit(1)
	}

	historyStart := start.AddDate(0, 0, -35)
	weatherEnd := end.AddDate(0, 0, 2)
	fmt.Printf("Fetching energy and weather histories with lookback (historyStart=%s, simStart=%s, simEnd=%s)...\n",
		historyStart.Format("2006-01-02"), start.Format("2006-01-02"), end.Format("2006-01-02"))

	summaries, err := db.GetHistorySummaries(ctx, sID, historyStart, weatherEnd)
	if err != nil {
		fmt.Printf("Failed to get history summaries: %v\n", err)
		os.Exit(1)
	}

	var combinedEnergy []types.DailyEnergyStats
	var combinedWeather []types.Weather
	var latestEnergyDay, latestWeatherDay time.Time

	for _, sm := range summaries {
		for _, d := range sm.Energy {
			if !d.TSDayStart.Before(historyStart) && d.TSDayStart.Before(end) {
				combinedEnergy = append(combinedEnergy, d)
				if latestEnergyDay.IsZero() || d.TSDayStart.After(latestEnergyDay) {
					latestEnergyDay = d.TSDayStart
				}
			}
		}
		for _, w := range sm.Weather {
			if !w.TSDayStart.Before(historyStart) && w.TSDayStart.Before(weatherEnd) {
				combinedWeather = append(combinedWeather, w)
				if latestWeatherDay.IsZero() || w.TSDayStart.After(latestWeatherDay) {
					latestWeatherDay = w.TSDayStart
				}
			}
		}
	}

	eFetchStart := historyStart
	if !latestEnergyDay.IsZero() {
		eFetchStart = latestEnergyDay.AddDate(0, 0, 1)
	}
	if !eFetchStart.After(end) {
		unsummEnergy, err := db.GetEnergyHistory(ctx, sID, eFetchStart, end)
		if err != nil {
			fmt.Printf("Failed to get unsummarized energy history: %v\n", err)
			os.Exit(1)
		}
		combinedEnergy = append(combinedEnergy, unsummEnergy...)
	}

	wFetchStart := historyStart
	if !latestWeatherDay.IsZero() {
		wFetchStart = latestWeatherDay.AddDate(0, 0, 1)
	}
	if !wFetchStart.After(weatherEnd) {
		unsummWeather, err := db.GetWeather(ctx, sID, wFetchStart, weatherEnd)
		if err != nil {
			fmt.Printf("Failed to get unsummarized weather: %v\n", err)
			os.Exit(1)
		}
		combinedWeather = append(combinedWeather, unsummWeather...)
	}

	slices.SortFunc(combinedEnergy, func(a, b types.DailyEnergyStats) int {
		return a.TSDayStart.Compare(b.TSDayStart)
	})
	slices.SortFunc(combinedWeather, func(a, b types.Weather) int {
		return a.TSDayStart.Compare(b.TSDayStart)
	})

	tz := ""
	if settings.Location != nil && settings.Location.TimeZone != "" {
		tz = settings.Location.TimeZone
	}

	var dedupEnergy []types.DailyEnergyStats
	seenE := make(map[string]int)
	for _, day := range combinedEnergy {
		if tz == "" && day.TimeLocation != "" {
			tz = day.TimeLocation
		}
		dStr := day.TSDayStart.Format("2006-01-02")
		if idx, ok := seenE[dStr]; ok {
			if len(day.Hourly) >= len(dedupEnergy[idx].Hourly) {
				dedupEnergy[idx] = day
			}
		} else {
			seenE[dStr] = len(dedupEnergy)
			dedupEnergy = append(dedupEnergy, day)
		}
	}

	var dedupWeather []types.Weather
	seenW := make(map[string]int)
	for _, w := range combinedWeather {
		if tz == "" && w.TimeLocation != "" {
			tz = w.TimeLocation
		}
		dStr := w.TSDayStart.Format("2006-01-02")
		if idx, ok := seenW[dStr]; ok {
			if len(w.ForecastHours) >= len(dedupWeather[idx].ForecastHours) {
				dedupWeather[idx] = w
			}
		} else {
			seenW[dStr] = len(dedupWeather)
			dedupWeather = append(dedupWeather, w)
		}
	}

	if tz == "" {
		tz = "America/Chicago"
	}

	dataset := controller.EnergyHistoryDataset{
		SiteID:            fmt.Sprintf("site%d", siteNum),
		Period:            period,
		TimeZone:          tz,
		SimStart:          start,
		SimEnd:            end,
		EVChargingPeriods: settings.EVChargingPeriods,
		EnergyHistory:     dedupEnergy,
		WeatherHistory:    dedupWeather,
	}

	outputFilename := fmt.Sprintf("site%d_%s.json", siteNum, period)
	outputPath := filepath.Join("pkg", "controller", "testdata", "energy_history", outputFilename)

	jsonBytes, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		fmt.Printf("Failed to marshal energy dataset: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Printf("Failed to create directories: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, jsonBytes, 0644); err != nil {
		fmt.Printf("Failed to write output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded: %d energy days, %d weather days (tz=%s, evPeriods=%d).\n",
		len(dedupEnergy), len(dedupWeather), tz, len(settings.EVChargingPeriods))
	fmt.Printf("Saved anonymized energy history dataset to %s\n", outputPath)
}

func getOrAssignSiteNumber(ctx context.Context, client *firestore.Client, siteID string) (int, error) {
	siteID = strings.ToLower(strings.TrimSpace(siteID))
	if siteID == "" {
		return 0, fmt.Errorf("site ID cannot be empty")
	}

	docRef := client.Collection("history_tests").Doc("site_mapping")
	var siteNum int

	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(docRef)
		mapping := make(map[string]int)

		if err != nil {
			if status.Code(err) != codes.NotFound {
				return err
			}
			// Document doesn't exist yet, we will initialize it below
		} else {
			// Document exists, retrieve mapping field
			data, err := doc.DataAt("mapping")
			if err == nil {
				if m, ok := data.(map[string]any); ok {
					for k, v := range m {
						if vInt, ok := v.(int64); ok {
							mapping[k] = int(vInt)
						} else if vFloat, ok := v.(float64); ok {
							mapping[k] = int(vFloat)
						}
					}
				}
			}
		}

		// Check if siteID is already mapped
		if num, exists := mapping[siteID]; exists {
			siteNum = num
			return nil
		}

		// Find the next available number (max + 1)
		maxNum := 0
		for _, num := range mapping {
			if num > maxNum {
				maxNum = num
			}
		}
		siteNum = maxNum + 1
		mapping[siteID] = siteNum

		// Save updated mapping
		return tx.Set(docRef, map[string]any{
			"mapping": mapping,
		})
	})

	if err != nil {
		return 0, err
	}
	return siteNum, nil
}
