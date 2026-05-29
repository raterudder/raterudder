package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	// Register fetch-specific flags using lflag
	siteID := lflag.String("site", "", "Site ID to fetch history for")
	period := lflag.String("period", "", "Period name for the output file (e.g. march, may)")
	startDate := lflag.String("start", "", "Start date (YYYY-MM-DD) (Inclusive)")
	endDate := lflag.String("end", "", "End date (YYYY-MM-DD) (Exclusive)")

	// Configure database
	db := storage.Configured()

	lflag.Configure()

	var sID, p, sDate, eDate string

	lflag.Do(func() {
		sID = *siteID
		p = *period
		sDate = *startDate
		eDate = *endDate
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

	// Extract the underlying *firestore.Client using reflection because storage.Configured
	// returns struct { Database }
	var fsClient *firestore.Client
	val := reflect.ValueOf(db)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct && val.NumField() == 1 {
		underlyingInterface := val.Field(0).Interface()
		if getter, ok := underlyingInterface.(interface{ FirestoreClient() *firestore.Client }); ok {
			fsClient = getter.FirestoreClient()
		}
	}
	if fsClient == nil {
		fmt.Println("Error: could not retrieve firestore client from storage provider")
		os.Exit(1)
	}

	fmt.Printf("Fetching stable site number for site ID '%s'...\n", sID)
	siteNum, err := getOrAssignSiteNumber(ctx, fsClient, sID)
	if err != nil {
		fmt.Printf("Failed to get or assign site number: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Mapped site ID '%s' to site%d\n", sID, siteNum)

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
