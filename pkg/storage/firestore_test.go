package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFirestoreProvider(t *testing.T) {
	// Check if emulator is running or configured
	// We assume it is running on localhost:8087 as per task
	os.Setenv("FIRESTORE_EMULATOR_HOST", "127.0.0.1:8087")

	// Use a test project ID
	projectID := "test-project-id"

	// Use a random database for isolation
	randDB := fmt.Sprintf("test-db-%d", time.Now().UnixNano())
	f := &FirestoreProvider{
		projectID: projectID,
		database:  randDB,
	}

	ctx := context.Background()
	require.NoError(t, f.Init(ctx))
	defer f.Close()

	t.Run("Validate", func(t *testing.T) {
		require.NoError(t, f.Validate())
	})

	t.Run("Settings", func(t *testing.T) {
		settings := types.Settings{
			DryRun:                         true,
			AlwaysChargeUnderDollarsPerKWH: 1.2,
			MinBatterySOC:                  5.5,
		}
		// Pass version 1
		setUpdate1, err := f.SetSettings(ctx, "test-site", settings, 1, time.Time{})
		require.NoError(t, err)
		assert.False(t, setUpdate1.IsZero())

		gotSettings, version, updatedTime, err := f.GetSettings(ctx, "test-site")
		require.NoError(t, err)
		assert.Equal(t, 1, version)
		assert.False(t, updatedTime.IsZero())
		assert.Equal(t, settings.AlwaysChargeUnderDollarsPerKWH, gotSettings.AlwaysChargeUnderDollarsPerKWH)
		assert.Equal(t, settings.MinBatterySOC, gotSettings.MinBatterySOC)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
	})

	t.Run("SettingsConflict", func(t *testing.T) {
		siteID := "conflict-site"
		settings := types.Settings{DryRun: true}
		initUpdate, err := f.SetSettings(ctx, siteID, settings, 1, time.Time{})
		require.NoError(t, err)
		assert.False(t, initUpdate.IsZero())

		gotSettings, version, updatedTime, err := f.GetSettings(ctx, siteID)
		require.NoError(t, err)

		// Successful update with matching updatedTime
		gotSettings.DryRun = false
		updatedTime2, err := f.SetSettings(ctx, siteID, gotSettings, version, updatedTime)
		require.NoError(t, err)
		assert.False(t, updatedTime2.IsZero())

		// Verify that the returned updatedTime2 matches doc.UpdateTime in Firestore exactly
		gotSettings2, _, docUpdateTime, err := f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		assert.True(t, updatedTime2.Equal(docUpdateTime), "commitTime %v should equal doc.UpdateTime %v", updatedTime2, docUpdateTime)

		// And verify that using updatedTime2 in a subsequent write succeeds without conflict
		gotSettings2.DryRun = true
		updatedTime3, err := f.SetSettings(ctx, siteID, gotSettings2, version, updatedTime2)
		require.NoError(t, err)
		assert.False(t, updatedTime3.IsZero())

		// Conflicting update with stale updatedTime
		staleSettings := gotSettings
		staleSettings.DryRun = true
		_, err = f.SetSettings(ctx, siteID, staleSettings, version, updatedTime)
		assert.ErrorIs(t, err, ErrSettingsConflict)
	})

	t.Run("EmptySiteID", func(t *testing.T) {
		_, _, _, err := f.GetSettings(ctx, "")
		assert.ErrorContains(t, err, "siteID cannot be empty")
	})

	t.Run("Prices", func(t *testing.T) {
		now := time.Now().Truncate(time.Second).UTC() // Firestore timestamp precision (RFC3339 is seconds)
		p1 := types.Price{TSStart: now.Add(-1 * time.Hour), DollarsPerKWH: 0.10, Provider: "test"}
		p2 := types.Price{TSStart: now, DollarsPerKWH: 0.12, Provider: "test"}

		require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{p1}, 0))
		require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{p2}, 0))

		prices, err := f.GetPriceHistory(ctx, "test-site", now.Add(-2*time.Hour), now.Add(1*time.Minute))
		require.NoError(t, err)

		t.Run("UpsertMultipleBatches", func(t *testing.T) {

			var batchPrices []types.Price
			for i := 0; i < 5; i++ {
				batchPrices = append(batchPrices, types.Price{
					TSStart:       now.Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: float64(i) * 0.1,
					Provider:      "batch-test",
				})
			}
			require.NoError(t, f.UpsertPrices(ctx, "test-site", batchPrices, 0))

			res, err := f.GetPriceHistory(ctx, "test-site", now.Add(1*time.Hour), now.Add(6*time.Hour))
			require.NoError(t, err)
			assert.Len(t, res, 5)
			for i := 0; i < 5; i++ {
				assert.Equal(t, float64(i)*0.1, res[i].DollarsPerKWH)
			}
		})

		// Note: We depend on emulator state. It might have data from previous runs if not cleared.
		// But we should find at least our 2 inserts.
		foundP1 := false
		foundP2 := false
		for _, p := range prices {
			if p.DollarsPerKWH == 0.10 && p.TSStart.Equal(p1.TSStart) {
				foundP1 = true
			}
			if p.DollarsPerKWH == 0.12 && p.TSStart.Equal(p2.TSStart) {
				foundP2 = true
			}
		}
		assert.True(t, foundP1, "did not find inserted p1")
		assert.True(t, foundP2, "did not find inserted p2")

		t.Run("UpsertOverwrite", func(t *testing.T) {
			p2Updated := types.Price{TSStart: p2.TSStart, DollarsPerKWH: 0.99, Provider: "test"}
			require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{p2Updated}, 0))

			pricesUpdated, err := f.GetPriceHistory(ctx, "test-site", now.Add(-2*time.Hour), now.Add(1*time.Minute))
			require.NoError(t, err)

			foundP2Updated := false
			for _, p := range pricesUpdated {
				if p.TSStart.Equal(p2.TSStart) {
					if p.DollarsPerKWH == 0.99 {
						foundP2Updated = true
					} else {
						assert.Fail(t, "expected updated price 0.99", "got %f", p.DollarsPerKWH)
					}
				}
			}
			assert.True(t, foundP2Updated, "did not find updated price p2")
		})

		t.Run("GetLatestPriceHistoryTime", func(t *testing.T) {
			// Insert a future price
			future := now.Add(24 * time.Hour)
			pFuture := types.Price{TSStart: future, DollarsPerKWH: 0.99, Provider: "test"}
			require.NoError(t, f.UpsertPrices(ctx, "test-site", []types.Price{pFuture}, 0))

			latestTime, version, err := f.GetLatestPriceHistoryTime(ctx, "test-site")
			require.NoError(t, err)
			assert.Equal(t, future, latestTime, "latest time should match the future timestamp we just inserted")
			assert.Equal(t, 0, version, "version should be 0 because we didn't set it explicitly on upsert in this test")
		})

		t.Run("CrossMonthRange", func(t *testing.T) {
			siteID := "cross-month-site"
			tMay := time.Date(2026, 5, 31, 23, 0, 0, 0, time.UTC)
			tJun := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
			tJul := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

			pMay := types.Price{TSStart: tMay, DollarsPerKWH: 0.15, Provider: "test"}
			pJun := types.Price{TSStart: tJun, DollarsPerKWH: 0.25, Provider: "test"}
			pJul := types.Price{TSStart: tJul, DollarsPerKWH: 0.35, Provider: "test"}

			require.NoError(t, f.UpsertPrices(ctx, siteID, []types.Price{pMay, pJun, pJul}, 2))

			// Query range covering May and June, but stopping before July
			res, err := f.GetPriceHistory(ctx, siteID, tMay, tJun.Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, res, 2)
			assert.Equal(t, tMay, res[0].TSStart)
			assert.Equal(t, 0.15, res[0].DollarsPerKWH)
			assert.Equal(t, tJun, res[1].TSStart)
			assert.Equal(t, 0.25, res[1].DollarsPerKWH)

			latestTime, ver, err := f.GetLatestPriceHistoryTime(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, tJul, latestTime)
			assert.Equal(t, 2, ver)

			priceDoc, err := f.client.Collection("sites").Doc(siteID).Collection("monthly_prices").Doc("2026-06").Get(ctx)
			require.NoError(t, err)
			rawEarliest, err := priceDoc.DataAt("earliestDate")
			require.NoError(t, err)
			rawLatest, err := priceDoc.DataAt("latestDate")
			require.NoError(t, err)
			assert.Equal(t, tJun, rawEarliest.(time.Time).UTC())
			assert.Equal(t, tJun, rawLatest.(time.Time).UTC())
			_, err = priceDoc.DataAt("month")
			assert.Error(t, err)
		})
	})

	t.Run("Actions", func(t *testing.T) {
		now := time.Now().Truncate(time.Second).UTC()
		a1 := types.Action{
			Timestamp:    now,
			BatteryMode:  types.BatteryModeChargeAny,
			SolarMode:    types.SolarModeAny,
			Description:  "Charging test",
			CurrentPrice: &types.Price{DollarsPerKWH: 0.05, TSStart: now},
		}
		require.NoError(t, f.InsertAction(ctx, "test-site", a1))

		actions, err := f.GetActionHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(1*time.Minute))
		require.NoError(t, err)

		foundA1 := false
		for _, a := range actions {
			if a.Description == "Charging test" && a.BatteryMode == types.BatteryModeChargeAny {
				foundA1 = true
			}
		}
		assert.True(t, foundA1, "did not find inserted action in history")

		t.Run("ActionRangeFiltering", func(t *testing.T) {
			a2 := types.Action{
				Timestamp:    now.Add(-2 * time.Hour),
				BatteryMode:  types.BatteryModeLoad,
				SolarMode:    types.SolarModeAny,
				Description:  "Old action outside range",
				CurrentPrice: &types.Price{DollarsPerKWH: 0.08, TSStart: now.Add(-2 * time.Hour)},
			}
			a3 := types.Action{
				Timestamp:    now.Add(10 * time.Second),
				BatteryMode:  types.BatteryModeChargeAny,
				SolarMode:    types.SolarModeAny,
				Description:  "Second action in range",
				CurrentPrice: &types.Price{DollarsPerKWH: 0.06, TSStart: now.Add(10 * time.Second)},
			}
			require.NoError(t, f.InsertAction(ctx, "test-site", a2))
			require.NoError(t, f.InsertAction(ctx, "test-site", a3))

			// Query should return a1 and a3, but not a2 (which is outside range)
			actionsFiltered, err := f.GetActionHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(1*time.Minute))
			require.NoError(t, err)

			// Check that a2 (outside range) is not returned
			for _, a := range actionsFiltered {
				assert.NotEqual(t, "Old action outside range", a.Description, "action outside range should not be returned")
			}
			// Verify we found the actions we just inserted
			foundA1InFiltered := false
			foundA3InFiltered := false
			for _, a := range actionsFiltered {
				if a.Description == "Charging test" {
					foundA1InFiltered = true
				}
				if a.Description == "Second action in range" {
					foundA3InFiltered = true
				}
			}
			assert.True(t, foundA1InFiltered, "did not find a1 in filtered results")
			assert.True(t, foundA3InFiltered, "did not find a3 in filtered results")
		})

		t.Run("GetLatestAction", func(t *testing.T) {
			now := time.Now().Truncate(time.Second).UTC()
			a1 := types.Action{
				Timestamp:   now.Add(-1 * time.Hour),
				BatteryMode: types.BatteryModeChargeAny,
				Description: "Old action",
			}
			a2 := types.Action{
				Timestamp:   now,
				BatteryMode: types.BatteryModeLoad,
				Description: "New action",
			}
			require.NoError(t, f.InsertAction(ctx, "test-site-latest", a1))
			require.NoError(t, f.InsertAction(ctx, "test-site-latest", a2))

			latest, err := f.GetLatestAction(ctx, "test-site-latest")
			require.NoError(t, err)
			require.NotNil(t, latest)
			assert.Equal(t, "New action", latest.Description)

			// test empty
			empty, err := f.GetLatestAction(ctx, "test-site-empty")
			require.NoError(t, err)
			require.Nil(t, empty)
		})

		t.Run("ActionWithPlanStoredFormat", func(t *testing.T) {
			loc, err := time.LoadLocation("America/Chicago")
			require.NoError(t, err)
			now := time.Now().Truncate(time.Second).In(loc)
			act := types.Action{
				Timestamp:       now.UTC(),
				SystemTimestamp: now,
				BatteryMode:     types.BatteryModeLoad,
				SolarMode:       types.SolarModeAny,
				Reason:          types.ActionReasonArbitrageHoldExport,
				Description:     "Stored action with plan test",
				Plan: &types.Plan{
					TSCreated:          now.UTC(),
					HorizonHours:       24,
					TotalProjectedCost: 2.50,
					Periods: []types.PlanPeriod{
						{
							TSStart:       now,
							TSEnd:         now.Add(time.Hour),
							DurationHours: 1.0,
							ImportDollars: 0.20,
							ExportDollars: 0.05,
							BatteryMode:   types.BatteryModeLoad,
							SolarMode:     types.SolarModeAny,
							Reason:        types.ActionReasonArbitrageHoldExport,
							StartSOC:      80.0,
							EndSOC:        70.0,
							ReserveSOC:    20.0,
							LoadKWH:       1.2,
							SolarKWH:      0.5,
						},
					},
				},
			}
			require.NoError(t, f.InsertAction(ctx, "test-site-plan", act))

			// Verify GetLatestAction returns full domain Action with Plan and restored time.Location
			latest, err := f.GetLatestAction(ctx, "test-site-plan")
			require.NoError(t, err)
			require.NotNil(t, latest)
			assert.Equal(t, act, *latest)

			// Verify direct Firestore document in daily_actions stores "shortPeriods" and "location" and NOT "periods" or "bucket"
			bucketID, _ := actionBucket(now.UTC())
			docRef := f.client.Collection("sites").Doc("test-site-plan").Collection("daily_actions").Doc(bucketID)
			snap, err := docRef.Get(ctx)
			require.NoError(t, err)
			rawEarliest, err := snap.DataAt("earliestDate")
			require.NoError(t, err)
			rawLatest, err := snap.DataAt("latestDate")
			require.NoError(t, err)
			assert.Equal(t, now.UTC(), rawEarliest.(time.Time).UTC())
			assert.Equal(t, now.UTC(), rawLatest.(time.Time).UTC())
			_, err = snap.DataAt("bucket")
			assert.Error(t, err)

			rawJSON, err := snap.DataAt("json")
			require.NoError(t, err)
			rawJSONStr, ok := rawJSON.(string)
			require.True(t, ok)
			assert.Contains(t, rawJSONStr, `"shortPeriods"`)
			assert.Contains(t, rawJSONStr, `"location":"America/Chicago"`)
			assert.NotContains(t, rawJSONStr, `"periods":`)
			assert.NotContains(t, rawJSONStr, `"bucket":`)
		})

		t.Run("CrossBucketAndMigrateLegacyActions", func(t *testing.T) {
			siteID := "test-site-migrate-actions"
			require.NoError(t, f.CreateSite(ctx, siteID, types.Site{ID: siteID}))

			tFeb := time.Date(2026, 2, 6, 5, 15, 11, 513218608, time.UTC)
			tSep := time.Date(2026, 9, 15, 0, 9, 6, 822395586, time.UTC)
			tOct1 := time.Date(2026, 10, 10, 21, 47, 9, 123456789, time.UTC)
			tOct2 := time.Date(2026, 10, 10, 22, 7, 11, 343112381, time.UTC)

			legacyColl, err := f.getCollection(siteID, "action_history")
			require.NoError(t, err)

			// 1. Earliest production Decide-mode action (no systemTimestamp, no timeLocation)
			legacyDocFeb := `{"timestamp":"2026-02-06T05:15:11.513218608Z","batteryMode":1,"solarMode":0,"description":"Deficit predicted and higher prices later (0.020 < 0.065).","currentPrice":{"tsStart":"2026-02-05T23:00:00-06:00","tsEnd":"2026-02-05T23:05:00-06:00","dollarsPerKWH":0.02},"systemStatus":{"timestamp":"0001-01-01T00:00:00Z","batterySOC":0,"eachBatterySOC":null,"batteryKW":0,"eachBatteryKW":null,"batteryCapacityKWH":0,"maxBatteryChargeKW":0,"maxBatteryDischargeKW":0,"solarKW":0,"gridKW":0,"homeKW":0,"canExportSolar":false,"canExportBattery":false,"canImportBattery":false,"emergencyMode":false}}`

			// 2. Sep 2026 production Decide-mode action (with systemTimestamp, timeLocation, simulationParams, capacityAt)
			legacyDocSep := `{"timestamp":"2026-09-15T00:09:06.822395586Z","systemTimestamp":"2026-09-14T19:09:06.822395586-05:00","batteryMode":-1,"solarMode":2,"reason":"sufficientBattery","description":"Sufficient battery.","currentPrice":{"provider":"comed_besh","tsStart":"2026-09-14T19:00:00-05:00","tsEnd":"2026-09-14T20:00:00-05:00","dollarsPerKWH":0.06592,"gridUseDollarsPerKWH":0.041169209660000004},"systemStatus":{"timestamp":"2026-09-14T19:09:06.822395586-05:00","timeLocation":"America/Chicago","batterySOC":81.474,"batteryKW":0.75,"batteryCapacityKWH":30,"maxBatteryChargeKW":16,"maxBatteryDischargeKW":20,"solarKW":0,"gridKW":0.01,"homeKW":0.746,"elevatedMinBatterySOC":false,"batteryAboveMinSOC":true,"vppSOC":20},"deficitAt":"0001-01-01T00:00:00Z","hitBufferedDeficitAt":"0001-01-01T00:00:00Z","hitThresholdDeficitAt":"0001-01-01T00:00:00Z","capacityAt":"2026-09-15T12:39:26.343947672-05:00","simulationParams":{"clippingCapKWH":10.247333333333334,"panelAzimuth":135,"panelTilt":25,"averageSolarEfficiency":0.010545211265857335,"detectedShift":"none"}}`

			// 3. Oct 2026 production Plan-mode action #1 (in 2026-10-10-12 bucket)
			legacyDocOct1 := `{"batteryMode":1,"capacityAt":"2026-10-11T11:00:00-05:00","chargeToSoc":25,"currentPrice":{"dollarsPerKWH":0.031,"gridUseDollarsPerKWH":0.11654290735999999,"provider":"comed_besh","tsEnd":"2026-10-10T17:00:00-05:00","tsStart":"2026-10-10T16:00:00-05:00"},"deficitAt":"0001-01-01T00:00:00Z","description":"Preserving battery in standby for upcoming peak rates.","hitBufferedDeficitAt":"0001-01-01T00:00:00Z","hitThresholdDeficitAt":"0001-01-01T00:00:00Z","q3HomeUsageKWH":3.1,"reason":"deficitSaveForPeak","recentHomeUsageKWH":1.9,"shortPlan":{"h":25,"nb":0.92,"p":[{"bm":1,"d":0.3333333333333333,"e":0.031,"es":99.474,"i":0.14754290736,"l":0.35,"r":"deficitSaveForPeak","rs":25,"s":0.40,"sm":2,"ss":99.474,"te":"2026-10-10T17:07:09.123456789-05:00","ts":"2026-10-10T16:47:09.123456789-05:00"}],"te":0.92,"ts":"2026-10-10T21:47:09.123456789Z"},"simulationParams":{"averageSolarEfficiency":0.012228873447686464,"clippingCapKWH":10.243749999999999,"detectedShift":"none","panelAzimuth":90,"panelTilt":25},"solarMode":2,"strategyBenefitDollars":0.92,"systemStatus":{"batteryAboveMinSOC":true,"batteryCapacityKWH":30,"batteryKW":0.0,"batterySOC":99.474,"elevatedMinBatterySOC":false,"gridKW":0.0,"homeKW":1.1,"maxBatteryChargeKW":16,"maxBatteryDischargeKW":20,"solarKW":1.1,"timeLocation":"America/Chicago","timestamp":"2026-10-10T16:47:09.123456789-05:00","vppSOC":20},"systemTimestamp":"2026-10-10T16:47:09.123456789-05:00","timestamp":"2026-10-10T21:47:09.123456789Z"}`

			// 4. Oct 2026 production Plan-mode action #2 (in the SAME 2026-10-10-12 bucket, 20 minutes later)
			legacyDocOct2 := `{"batteryMode":-1,"capacityAt":"2026-10-11T11:00:00-05:00","chargeToSoc":25,"currentPrice":{"dollarsPerKWH":0.04810999999999999,"gridUseDollarsPerKWH":0.11654290735999999,"provider":"comed_besh","tsEnd":"2026-10-10T18:00:00-05:00","tsStart":"2026-10-10T17:00:00-05:00"},"deficitAt":"0001-01-01T00:00:00Z","description":"Discharging battery to cover household load.","hitBufferedDeficitAt":"0001-01-01T00:00:00Z","hitThresholdDeficitAt":"0001-01-01T00:00:00Z","q3HomeUsageKWH":3.214642537876033,"reason":"sufficientBattery","recentHomeUsageKWH":2.071416666666667,"shortPlan":{"h":25,"nb":0.8787479867152629,"p":[{"bm":-1,"d":0.3333333333333333,"e":0.04810999999999999,"es":99.2625745604791,"i":0.16465290736,"l":0.3818394016436127,"r":"sufficientBattery","rs":25,"s":0.32166666666666666,"sm":2,"ss":99.474,"te":"2026-10-10T17:27:11.343112381-05:00","ts":"2026-10-10T17:07:11.343112381-05:00"},{"bm":-1,"d":0.5468491354497222,"e":0.04810999999999999,"es":97.78417974884252,"i":0.16465290736,"l":0.6264256400083466,"r":"sufficientBattery","rs":25,"s":0.20566710029110363,"sm":2,"ss":99.2625745604791,"te":"2026-10-10T18:00:00-05:00","ts":"2026-10-10T17:27:11.343112381-05:00"}],"te":0.8787479867152629,"ts":"2026-10-10T22:07:11.343112381Z"},"simulationParams":{"averageSolarEfficiency":0.012228873447686464,"clippingCapKWH":10.243749999999999,"detectedShift":"none","panelAzimuth":90,"panelTilt":25},"solarMode":2,"strategyBenefitDollars":0.8787479867152629,"systemStatus":{"batteryAboveMinSOC":true,"batteryCapacityKWH":30,"batteryKW":1.365,"batterySOC":99.474,"elevatedMinBatterySOC":false,"gridKW":0.054,"homeKW":2.383,"maxBatteryChargeKW":16,"maxBatteryDischargeKW":20,"solarKW":0.965,"timeLocation":"America/Chicago","timestamp":"2026-10-10T17:07:11.343112381-05:00","vppSOC":20},"systemTimestamp":"2026-10-10T17:07:11.343112381-05:00","timestamp":"2026-10-10T22:07:11.343112381Z"}`

			_, err = legacyColl.Doc(tFeb.Format(time.RFC3339)).Set(ctx, map[string]any{"json": legacyDocFeb, "timestamp": tFeb})
			require.NoError(t, err)
			_, err = legacyColl.Doc(tSep.Format(time.RFC3339)).Set(ctx, map[string]any{"json": legacyDocSep, "timestamp": tSep})
			require.NoError(t, err)
			_, err = legacyColl.Doc(tOct1.Format(time.RFC3339)).Set(ctx, map[string]any{"json": legacyDocOct1, "timestamp": tOct1})
			require.NoError(t, err)
			_, err = legacyColl.Doc(tOct2.Format(time.RFC3339)).Set(ctx, map[string]any{"json": legacyDocOct2, "timestamp": tOct2})
			require.NoError(t, err)

			// 1. Dry-run migration should report 4 actions across 3 buckets without writing daily_actions
			dryStats, err := f.MigrateLegacyActions(ctx, ActionMigrationOptions{
				DryRun: true,
				SiteID: siteID,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, dryStats.SitesInspected)
			assert.Equal(t, 4, dryStats.SiteActionsMigrated)
			assert.Equal(t, 3, dryStats.SiteBucketsCreated)
			assert.Equal(t, 0, dryStats.SitesPurged)

			emptyHist, err := f.GetActionHistory(ctx, siteID, tFeb.Add(-time.Hour), tOct2.Add(time.Hour))
			require.NoError(t, err)
			assert.Empty(t, emptyHist)

			// 2. Migration with Since filter should only migrate the 2 Oct actions in 1 bucket
			sinceStats, err := f.MigrateLegacyActions(ctx, ActionMigrationOptions{
				DryRun: false,
				SiteID: siteID,
				Since:  tOct1.Truncate(time.Second),
			})
			require.NoError(t, err)
			assert.Equal(t, 1, sinceStats.SitesInspected)
			assert.Equal(t, 2, sinceStats.SiteActionsMigrated)
			assert.Equal(t, 1, sinceStats.SiteBucketsCreated)

			octHist, err := f.GetActionHistory(ctx, siteID, tFeb.Add(-time.Hour), tOct2.Add(time.Hour))
			require.NoError(t, err)
			assert.Len(t, octHist, 2)

			// 3. Full migration without Since migrates all 4 actions across 3 buckets and is idempotent on the Oct bucket
			migStats, err := f.MigrateLegacyActions(ctx, ActionMigrationOptions{
				DryRun: false,
				SiteID: siteID,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, migStats.SitesInspected)
			assert.Equal(t, 4, migStats.SiteActionsMigrated)
			assert.Equal(t, 3, migStats.SiteBucketsCreated)
			assert.Equal(t, 0, migStats.SitesPurged)

			// Verify cross-bucket GetActionHistory returns all 4 actions with proper Location and Plan.Periods
			hist, err := f.GetActionHistory(ctx, siteID, tFeb.Add(-time.Minute), tOct2.Add(time.Minute))
			require.NoError(t, err)
			if assert.Len(t, hist, 4) {
				// Action 0: Feb 2026 Decide mode without systemTimestamp/timeLocation
				assert.Equal(t, "Deficit predicted and higher prices later (0.020 < 0.065).", hist[0].Description)
				assert.True(t, hist[0].SystemTimestamp.IsZero())
				assert.Nil(t, hist[0].Plan)

				// Action 1: Sep 2026 Decide mode with America/Chicago
				assert.Equal(t, "Sufficient battery.", hist[1].Description)
				assert.Equal(t, "America/Chicago", hist[1].SystemTimestamp.Location().String())
				assert.Equal(t, "America/Chicago", hist[1].HitCapacityAt.Location().String())
				assert.Nil(t, hist[1].Plan)

				// Action 2: Oct 2026 Plan mode #1
				assert.Equal(t, "Preserving battery in standby for upcoming peak rates.", hist[2].Description)
				assert.Equal(t, "America/Chicago", hist[2].SystemTimestamp.Location().String())
				if assert.NotNil(t, hist[2].Plan) && assert.Len(t, hist[2].Plan.Periods, 1) {
					assert.Equal(t, "America/Chicago", hist[2].Plan.Periods[0].TSStart.Location().String())
					assert.Equal(t, 0.1475, hist[2].Plan.Periods[0].ImportDollars)
				}

				// Action 3: Oct 2026 Plan mode #2
				assert.Equal(t, "Discharging battery to cover household load.", hist[3].Description)
				assert.Equal(t, "America/Chicago", hist[3].SystemTimestamp.Location().String())
				if assert.NotNil(t, hist[3].Plan) && assert.Len(t, hist[3].Plan.Periods, 2) {
					assert.Equal(t, "America/Chicago", hist[3].Plan.Periods[0].TSStart.Location().String())
					assert.Equal(t, 0.1647, hist[3].Plan.Periods[0].ImportDollars)
					assert.Equal(t, 0.0481, hist[3].Plan.Periods[0].ExportDollars)
					assert.Equal(t, 99.3, hist[3].Plan.Periods[0].EndSOC)
					assert.Equal(t, 97.8, hist[3].Plan.Periods[1].EndSOC)
				}
			}

			// Verify GetLatestAction returns the second Oct action
			latest, err := f.GetLatestAction(ctx, siteID)
			require.NoError(t, err)
			require.NotNil(t, latest)
			assert.Equal(t, "Discharging battery to cover household load.", latest.Description)

			// 4. Dry-run PurgeLegacy does not delete action_history
			dryPurgeStats, err := f.MigrateLegacyActions(ctx, ActionMigrationOptions{
				DryRun:      true,
				PurgeLegacy: true,
				SiteID:      siteID,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, dryPurgeStats.SitesInspected)
			assert.Equal(t, 0, dryPurgeStats.SiteActionsMigrated)
			assert.Equal(t, 0, dryPurgeStats.SiteBucketsCreated)
			assert.Equal(t, 1, dryPurgeStats.SitesPurged)

			legacyDocsBefore, err := legacyColl.Documents(ctx).GetAll()
			require.NoError(t, err)
			assert.Len(t, legacyDocsBefore, 4)

			// Delete the Feb daily_actions bucket before running real PurgeLegacy to prove PurgeLegacy does NOT re-insert
			dailyColl, err := f.getCollection(siteID, "daily_actions")
			require.NoError(t, err)
			_, err = dailyColl.Doc("2026-02-06-00").Delete(ctx)
			require.NoError(t, err)

			// 5. Real PurgeLegacy ONLY deletes legacy action_history and does not re-insert into daily_actions
			purgeStats, err := f.MigrateLegacyActions(ctx, ActionMigrationOptions{
				PurgeLegacy: true,
				SiteID:      siteID,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, purgeStats.SitesInspected)
			assert.Equal(t, 0, purgeStats.SiteActionsMigrated)
			assert.Equal(t, 0, purgeStats.SiteBucketsCreated)
			assert.Equal(t, 1, purgeStats.SitesPurged)

			// Verify action_history collection is now empty
			legacyDocsAfter, err := legacyColl.Documents(ctx).GetAll()
			require.NoError(t, err)
			assert.Empty(t, legacyDocsAfter)

			// Verify 2026-02-06-00 was NOT recreated by PurgeLegacy
			_, err = dailyColl.Doc("2026-02-06-00").Get(ctx)
			assert.Equal(t, codes.NotFound, status.Code(err))
		})
	})

	t.Run("EnergyHistory", func(t *testing.T) {
		now := time.Now().Truncate(24 * time.Hour).UTC() // Truncate to day since we now use DailyEnergyStats
		stats := types.DailyEnergyStats{
			TSDayStart: now,
			Hourly: []types.EnergyStats{
				{
					TSHourStart:       now.Add(10 * time.Hour),
					SolarKWH:          5.0,
					BatteryChargedKWH: 2.0,
				},
			},
		}
		require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", []types.DailyEnergyStats{stats}, types.CurrentEnergyStatsVersion))

		t.Run("UpsertMultipleDays", func(t *testing.T) {
			var batchStats []types.DailyEnergyStats
			for i := 1; i <= 5; i++ {
				day := now.Add(time.Duration(i*24) * time.Hour)
				batchStats = append(batchStats, types.DailyEnergyStats{
					TSDayStart: day,
					Hourly: []types.EnergyStats{
						{
							TSHourStart: day.Add(12 * time.Hour),
							SolarKWH:    float64(i) * 1.0,
						},
					},
				})
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", batchStats, types.CurrentEnergyStatsVersion))

			res, err := f.GetEnergyHistory(ctx, "test-site", now.Add(24*time.Hour), now.Add(6*24*time.Hour))
			if assert.NoError(t, err) {
				assert.Len(t, res, 5)
				for i := 0; i < 5; i++ {
					assert.Equal(t, float64(i+1)*1.0, res[i].Hourly[0].SolarKWH)
				}
			}
		})

		t.Run("GetEnergyHistory", func(t *testing.T) {
			energyHistory, err := f.GetEnergyHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(24*time.Hour))
			if assert.NoError(t, err) {
				foundS := false
				for _, s := range energyHistory {
					if len(s.Hourly) > 0 && s.Hourly[0].SolarKWH == 5.0 {
						foundS = true
					}
				}
				assert.True(t, foundS, "did not find inserted energy stats")
			}
		})

		t.Run("GetLatestEnergyHistoryTime", func(t *testing.T) {
			future := now.Add(10 * 24 * time.Hour)
			futureStats := types.DailyEnergyStats{
				TSDayStart: future,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: future.Add(15 * time.Hour),
						SolarKWH:    1.0,
					},
				},
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", []types.DailyEnergyStats{futureStats}, types.CurrentEnergyStatsVersion))

			latestTime, version, err := f.GetLatestEnergyHistoryTime(ctx, "test-site")
			if assert.NoError(t, err) {
				assert.Equal(t, future.Add(15*time.Hour), latestTime, "latest time should be the last recorded hour")
				assert.Equal(t, int(types.CurrentEnergyStatsVersion), version)
			}
		})

		t.Run("GetEnergyHistoryTimezoneShift", func(t *testing.T) {
			locChicago, err := time.LoadLocation("America/Chicago")
			require.NoError(t, err)
			locLA, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)

			// Test case 1: Chicago timezone
			chicagoMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locChicago)
			chicagoStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locChicago)

			statsChicago := types.DailyEnergyStats{
				TSDayStart: chicagoMidnight,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: chicagoMidnight.Add(10 * time.Hour),
						SolarKWH:    10.0,
					},
				},
			}
			err = f.UpsertEnergyHistories(ctx, "test-site-chicago-eh-tz", []types.DailyEnergyStats{statsChicago}, types.CurrentEnergyStatsVersion)
			require.NoError(t, err)

			resChicago, err := f.GetEnergyHistory(ctx, "test-site-chicago-eh-tz", chicagoStartQuery, chicagoStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resChicago, 1, "should retrieve energy history for Chicago when querying at 4pm")

			// Test case 2: Los Angeles timezone
			laMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locLA)
			laStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locLA)

			statsLA := types.DailyEnergyStats{
				TSDayStart: laMidnight,
				Hourly: []types.EnergyStats{
					{
						TSHourStart: laMidnight.Add(10 * time.Hour),
						SolarKWH:    15.0,
					},
				},
			}
			err = f.UpsertEnergyHistories(ctx, "test-site-la-eh-tz", []types.DailyEnergyStats{statsLA}, types.CurrentEnergyStatsVersion)
			require.NoError(t, err)

			resLA, err := f.GetEnergyHistory(ctx, "test-site-la-eh-tz", laStartQuery, laStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resLA, 1, "should retrieve energy history for LA when querying at 4pm")
		})
	})

	t.Run("Sites", func(t *testing.T) {
		// First, manually create a site via SetSettings so it exists
		site := types.Site{
			ID:         "test-site-crud",
			InviteCode: "invite123",
			Permissions: []types.SitePermissions{
				{UserID: "owner@test.com"},
			},
		}

		t.Run("UpdateSite", func(t *testing.T) {
			// UpdateSite uses MergeAll so it creates or updates
			require.NoError(t, f.UpdateSite(ctx, "test-site-crud", site))

			got, err := f.GetSite(ctx, "test-site-crud")
			require.NoError(t, err)
			assert.Equal(t, "invite123", got.InviteCode)
			assert.Len(t, got.Permissions, 1)
			assert.Equal(t, "owner@test.com", got.Permissions[0].UserID)
		})

		t.Run("UpdateSiteAddPermission", func(t *testing.T) {
			site.Permissions = append(site.Permissions, types.SitePermissions{UserID: "newuser@test.com"})
			require.NoError(t, f.UpdateSite(ctx, "test-site-crud", site))

			got, err := f.GetSite(ctx, "test-site-crud")
			require.NoError(t, err)
			assert.Len(t, got.Permissions, 2)
			assert.Equal(t, "newuser@test.com", got.Permissions[1].UserID)
		})

		t.Run("ListSites", func(t *testing.T) {
			// Create another site to ensure we have at least 2
			site2 := types.Site{ID: "site2"}
			require.NoError(t, f.UpdateSite(ctx, "site2", site2))

			sites, err := f.ListSites(ctx)
			require.NoError(t, err)

			// We expect at least test-site-crud and site2
			foundTestSite := false
			foundSite2 := false
			for _, s := range sites {
				if s.ID == "test-site-crud" {
					foundTestSite = true
				}
				if s.ID == "site2" {
					foundSite2 = true
				}
			}
			assert.True(t, foundTestSite, "ListSites did not return test-site-crud")
			assert.True(t, foundSite2, "ListSites did not return site2")
		})

		t.Run("ListSitesSettings", func(t *testing.T) {
			// Set settings with specific UpdateGroup and Release values
			set1 := types.Settings{UpdateGroup: 3, Release: "production"}
			set2 := types.Settings{UpdateGroup: 7, Release: "staging"}
			set3 := types.Settings{UpdateGroup: 0, Release: "production"}

			_, err := f.SetSettings(ctx, "site-group-3", set1, 1, time.Time{})
			require.NoError(t, err)
			_, err = f.SetSettings(ctx, "site-group-7", set2, 1, time.Time{})
			require.NoError(t, err)
			_, err = f.SetSettings(ctx, "site-group-0", set3, 1, time.Time{})
			require.NoError(t, err)

			// Query with empty release and nil updateGroup: should return all
			allSettings, allVersions, allTimes, err := f.ListSitesSettings(ctx, "", nil)
			require.NoError(t, err)
			assert.Contains(t, allSettings, "site-group-3")
			assert.Contains(t, allSettings, "site-group-7")
			assert.Contains(t, allSettings, "site-group-0")
			assert.False(t, allTimes["site-group-3"].IsZero())
			assert.Equal(t, 3, allSettings["site-group-3"].UpdateGroup)
			assert.Equal(t, 7, allSettings["site-group-7"].UpdateGroup)
			assert.Equal(t, 0, allSettings["site-group-0"].UpdateGroup)
			assert.Equal(t, 1, allVersions["site-group-3"])

			// Query with release "staging": should only return site-group-7
			stagingSettings, _, _, err := f.ListSitesSettings(ctx, "staging", nil)
			require.NoError(t, err)
			assert.NotContains(t, stagingSettings, "site-group-3")
			assert.Contains(t, stagingSettings, "site-group-7")
			assert.NotContains(t, stagingSettings, "site-group-0")

			// Query with release "production" and updateGroup [3, 4]: should only return site-group-3
			prodGroupSettings, _, _, err := f.ListSitesSettings(ctx, "production", []int{3, 4})
			require.NoError(t, err)
			assert.Contains(t, prodGroupSettings, "site-group-3")
			assert.NotContains(t, prodGroupSettings, "site-group-7")
			assert.NotContains(t, prodGroupSettings, "site-group-0")

			// Query with empty release and [3, 4] updateGroup: should error
			filteredSettings, filteredVersions, filteredTimes, err := f.ListSitesSettings(ctx, "", []int{3, 4})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "release cannot be empty")
			assert.Nil(t, filteredSettings)
			assert.Nil(t, filteredVersions)
			assert.Nil(t, filteredTimes)
		})

		t.Run("DeleteSite", func(t *testing.T) {
			siteID := "delete-site-test"
			site := types.Site{
				ID:         siteID,
				InviteCode: "invite-del",
			}
			require.NoError(t, f.UpdateSite(ctx, siteID, site))

			// Create some settings (subcollection config)
			_, err := f.SetSettings(ctx, siteID, types.Settings{UpdateGroup: 5}, 1, time.Time{})
			require.NoError(t, err)

			// Verify site exists
			gotSite, err := f.GetSite(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, siteID, gotSite.ID)

			// Delete site
			require.NoError(t, f.DeleteSite(ctx, siteID))

			// Verify site document is deleted
			_, err = f.GetSite(ctx, siteID)
			assert.ErrorContains(t, err, "site not found")

			// Verify config/settings is deleted
			_, ver, _, err := f.GetSettings(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, 0, ver)
		})
	})

	t.Run("Users", func(t *testing.T) {
		t.Run("CreateUser", func(t *testing.T) {
			user := types.User{
				ID:    "newuser@test.com",
				Email: "newuser@test.com",
				Sites: []types.UserSite{
					{
						ID: "site1",
					},
				},
			}
			require.NoError(t, f.CreateUser(ctx, user))

			got, err := f.GetUser(ctx, "newuser@test.com")
			require.NoError(t, err)
			assert.Equal(t, "newuser@test.com", got.ID)
			assert.Equal(t, "newuser@test.com", got.Email)
			assert.Equal(t, []types.UserSite{{ID: "site1"}}, got.Sites)
		})

		t.Run("CreateUserDuplicate", func(t *testing.T) {
			user := types.User{
				ID:    "newuser@test.com",
				Email: "newuser@test.com",
				Sites: []types.UserSite{
					{
						ID: "site1",
					},
				},
			}
			// Create uses Firestore's Create which should fail on duplicates
			err := f.CreateUser(ctx, user)
			assert.Error(t, err)
		})

		t.Run("UpdateUser", func(t *testing.T) {
			user := types.User{
				ID:    "newuser@test.com",
				Email: "newuser@test.com",
				Sites: []types.UserSite{
					{
						ID: "site1",
					},
					{
						ID: "site2",
					},
				},
			}
			require.NoError(t, f.UpdateUser(ctx, user))

			got, err := f.GetUser(ctx, "newuser@test.com")
			require.NoError(t, err)
			assert.Equal(t, []types.UserSite{{ID: "site1"}, {ID: "site2"}}, got.Sites)
		})

		t.Run("GetUserNotFound", func(t *testing.T) {
			_, err := f.GetUser(ctx, "nonexistent@test.com")
			assert.ErrorContains(t, err, "user not found")
		})

		t.Run("DeleteUser", func(t *testing.T) {
			userID := "delete-me@test.com"
			user := types.User{
				ID:    userID,
				Email: userID,
			}
			require.NoError(t, f.CreateUser(ctx, user))

			// Verify user exists
			_, err := f.GetUser(ctx, userID)
			require.NoError(t, err)

			// Delete user
			require.NoError(t, f.DeleteUser(ctx, userID))

			// Verify user no longer exists
			_, err = f.GetUser(ctx, userID)
			assert.ErrorContains(t, err, "user not found")
		})

		t.Run("ListUsers", func(t *testing.T) {
			users, err := f.ListUsers(ctx)
			require.NoError(t, err)

			// We should find the "newuser@test.com" we created earlier
			found := false
			for _, u := range users {
				if u.ID == "newuser@test.com" {
					found = true
					break
				}
			}
			assert.True(t, found, "ListUsers should return newuser@test.com")
		})
	})

	t.Run("AdminSettings", func(t *testing.T) {
		t.Run("GetAdminSettingsDefault", func(t *testing.T) {
			// Clean up settings if they exist to test default
			_, _ = f.client.Collection("admin").Doc("settings").Delete(ctx)

			settings, err := f.GetAdminSettings(ctx)
			require.NoError(t, err)
			assert.NotNil(t, settings.Aliases)
			assert.Empty(t, settings.Aliases)
		})

		t.Run("UpdateAndGetAdminSettings", func(t *testing.T) {
			settings := types.AdminSettings{
				Aliases: map[string]string{
					"site1": "alias1",
					"site2": "alias2",
				},
			}
			err := f.UpdateAdminSettings(ctx, settings)
			require.NoError(t, err)

			got, err := f.GetAdminSettings(ctx)
			require.NoError(t, err)
			assert.Equal(t, "alias1", got.Aliases["site1"])
			assert.Equal(t, "alias2", got.Aliases["site2"])
		})
	})

	t.Run("Feedback", func(t *testing.T) {
		provider := f

		// Insert feedbacks
		fb1 := types.Feedback{
			ID:        "2023-10-27T10:00:00Z_site1",
			SiteID:    "site1",
			UserID:    "user1",
			Sentiment: "happy",
			Comment:   "Great job!",
			Timestamp: time.Date(2023, 10, 27, 10, 0, 0, 0, time.UTC),
		}
		fb2 := types.Feedback{
			ID:        "2023-10-27T11:00:00Z_site1",
			SiteID:    "site1",
			UserID:    "user2",
			Sentiment: "sad",
			Comment:   "Needs work.",
			Timestamp: time.Date(2023, 10, 27, 11, 0, 0, 0, time.UTC),
		}
		fb3 := types.Feedback{
			ID:        "2023-10-27T12:00:00Z_site1",
			SiteID:    "site1",
			UserID:    "user3",
			Sentiment: "neutral",
			Comment:   "It's okay.",
			Timestamp: time.Date(2023, 10, 27, 12, 0, 0, 0, time.UTC),
		}

		err := provider.InsertFeedback(ctx, fb1)
		require.NoError(t, err)
		err = provider.InsertFeedback(ctx, fb2)
		require.NoError(t, err)
		err = provider.InsertFeedback(ctx, fb3)
		require.NoError(t, err)

		// List feedback
		fbs, err := provider.ListFeedback(ctx, 2, "")
		require.NoError(t, err)
		require.Len(t, fbs, 2)
		assert.Equal(t, fb3.ID, fbs[0].ID) // Descending order
		assert.Equal(t, fb2.ID, fbs[1].ID)

		// List with pagination
		fbs2, err := provider.ListFeedback(ctx, 2, fb2.ID)
		require.NoError(t, err)
		require.Len(t, fbs2, 1)
		assert.Equal(t, fb1.ID, fbs2[0].ID)
	})

	t.Run("Weather", func(t *testing.T) {
		siteID := "test-site-weather"

		t.Run("Upsert and Get Single", func(t *testing.T) {
			start := time.Now().Truncate(24 * time.Hour).UTC()
			w := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/Los_Angeles",
				Latitude:     34.0,
				Longitude:    -118.0,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: start.Add(1 * time.Hour),
						GHI:         150.5,
					},
				},
			}

			err := f.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			results, err := f.GetWeather(ctx, siteID, start, start.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, w.Latitude, results[0].Latitude)
			assert.Equal(t, w.Longitude, results[0].Longitude)
			assert.Equal(t, w.TimeLocation, results[0].TimeLocation)
			assert.Len(t, results[0].ForecastHours, 1)
			assert.Equal(t, 150.5, results[0].ForecastHours[0].GHI)
		})

		t.Run("Upsert and Get Batch", func(t *testing.T) {
			var weathers []types.Weather
			// Ensure time is not nicely truncated to UTC day boundaries to test truncation bug
			timeLoc, err := time.LoadLocation("America/New_York")
			require.NoError(t, err)
			localStart := time.Date(2024, 1, 1, 0, 0, 0, 0, timeLoc)
			start := localStart.UTC() // This will be 2024-01-01T05:00:00Z
			end := localStart.Add(48 * time.Hour).Add(6 * time.Hour).UTC()

			// generate 3 days
			for i := 0; i < 3; i++ {
				day := start.Add(time.Duration(i*24) * time.Hour)
				weathers = append(weathers, types.Weather{
					TSDayStart:   day,
					TimeLocation: "America/New_York",
					Latitude:     34.0,
					Longitude:    -118.0,
					ForecastHours: []types.HourlyWeather{
						{
							TSHourStart: day.Add(12 * time.Hour),
							GHI:         800.0,
						},
					},
				})
			}

			err = f.UpsertWeather(ctx, siteID, weathers, types.CurrentWeatherVersion)
			require.NoError(t, err)

			// Get all 3 days
			results, err := f.GetWeather(ctx, siteID, start, end)
			require.NoError(t, err)
			require.Len(t, results, 3)

			// Get only middle day
			middleDay := start.Add(24 * time.Hour)
			resultsMid, err := f.GetWeather(ctx, siteID, middleDay, middleDay.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, resultsMid, 1)
			assert.True(t, resultsMid[0].TSDayStart.Equal(middleDay))
		})

		t.Run("Upsert Overwrite", func(t *testing.T) {
			start := time.Now().Truncate(24 * time.Hour).UTC().Add(100 * 24 * time.Hour)
			w1 := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     34.0,
				Longitude:    -118.0,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: start.Add(1 * time.Hour),
						GHI:         100.0,
					},
				},
			}

			err := f.UpsertWeather(ctx, siteID, []types.Weather{w1}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			// Overwrite the same day
			w2 := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     35.0,
				Longitude:    -118.0,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart: start.Add(1 * time.Hour),
						GHI:         200.0,
					},
				},
			}
			err = f.UpsertWeather(ctx, siteID, []types.Weather{w2}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			results, err := f.GetWeather(ctx, siteID, start, start.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, 35.0, results[0].Latitude)
			assert.Equal(t, -118.0, results[0].Longitude)
			assert.Equal(t, 200.0, results[0].ForecastHours[0].GHI)
		})

		t.Run("Get Empty Range", func(t *testing.T) {
			start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			results, err := f.GetWeather(ctx, siteID, start, start.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, results, 0)
		})

		t.Run("Timezone Comparisons", func(t *testing.T) {
			locEast, err := time.LoadLocation("America/New_York")
			require.NoError(t, err)
			locWest, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)

			// Same actual time, different timezone representations
			tEast := time.Date(2024, 3, 5, 0, 0, 0, 0, locEast)
			tWest := time.Date(2024, 3, 4, 21, 0, 0, 0, locWest)

			// Ensure they are the same absolute time
			require.True(t, tEast.Equal(tWest))

			w := types.Weather{
				TSDayStart:   tWest, // Save with West Coast time
				TimeLocation: "America/Los_Angeles",
				Latitude:     37.0,
				Longitude:    -122.0,
			}
			err = f.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			// Query using East Coast time representation
			// We query for the exact same point in time up to 1 hour later
			results, err := f.GetWeather(ctx, siteID, tEast, tEast.Add(1*time.Hour))
			require.NoError(t, err)
			require.Len(t, results, 1, "should retrieve weather regardless of timezone representation")

			// Verify timestamp matches
			assert.True(t, results[0].TSDayStart.Equal(tEast))
		})

		t.Run("GetLatestWeatherTime", func(t *testing.T) {
			start := time.Now().Truncate(24 * time.Hour).UTC().Add(200 * 24 * time.Hour)
			updatedTime := time.Now().Truncate(time.Second).UTC()
			w := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     34.0,
				Longitude:    -118.0,
				TSUpdated:    updatedTime,
			}

			err := f.UpsertWeather(ctx, siteID, []types.Weather{w}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			latestTime, lastUpdated, version, err := f.GetLatestWeatherTime(ctx, siteID)
			require.NoError(t, err)
			assert.Equal(t, start, latestTime)
			assert.Equal(t, updatedTime, lastUpdated.UTC())
			assert.Equal(t, int(types.CurrentWeatherVersion), version)
		})

		t.Run("GetLatestWeatherTime Fallback", func(t *testing.T) {
			fallbackSiteID := "test-site-weather-fallback"
			start := time.Now().Truncate(24 * time.Hour).UTC().Add(300 * 24 * time.Hour)
			updatedTime := time.Now().Truncate(time.Second).UTC()
			w := types.Weather{
				TSDayStart:   start,
				TimeLocation: "America/New_York",
				Latitude:     34.0,
				Longitude:    -118.0,
				TSUpdated:    updatedTime,
			}

			coll, err := f.getCollection(fallbackSiteID, "weather")
			require.NoError(t, err)

			jsonBytes, err := json.Marshal(w)
			require.NoError(t, err)

			docID := start.Format("2006-01-02")
			_, err = coll.Doc(docID).Set(ctx, map[string]any{
				"json":       string(jsonBytes),
				"tsDayStart": w.TSDayStart,
				"version":    types.CurrentWeatherVersion,
			})
			require.NoError(t, err)

			latestTime, lastUpdated, version, err := f.GetLatestWeatherTime(ctx, fallbackSiteID)
			require.NoError(t, err)
			assert.Equal(t, start, latestTime)
			assert.Equal(t, updatedTime, lastUpdated.UTC())
			assert.Equal(t, int(types.CurrentWeatherVersion), version)
		})

		t.Run("GetWeatherTimezoneShift", func(t *testing.T) {
			locChicago, err := time.LoadLocation("America/Chicago")
			require.NoError(t, err)
			locLA, err := time.LoadLocation("America/Los_Angeles")
			require.NoError(t, err)

			// Test case 1: Chicago timezone
			chicagoMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locChicago)
			chicagoStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locChicago)

			wChicago := types.Weather{
				TSDayStart:   chicagoMidnight,
				TimeLocation: "America/Chicago",
				Latitude:     41.8781,
				Longitude:    -87.6298,
			}
			err = f.UpsertWeather(ctx, "test-site-chicago-tz", []types.Weather{wChicago}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			resChicago, err := f.GetWeather(ctx, "test-site-chicago-tz", chicagoStartQuery, chicagoStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resChicago, 1, "should retrieve weather for Chicago when querying at 4pm")

			// Test case 2: Los Angeles timezone
			laMidnight := time.Date(2024, 6, 22, 0, 0, 0, 0, locLA)
			laStartQuery := time.Date(2024, 6, 22, 16, 0, 0, 0, locLA)

			wLA := types.Weather{
				TSDayStart:   laMidnight,
				TimeLocation: "America/Los_Angeles",
				Latitude:     34.0522,
				Longitude:    -118.2437,
			}
			err = f.UpsertWeather(ctx, "test-site-la-tz", []types.Weather{wLA}, types.CurrentWeatherVersion)
			require.NoError(t, err)

			resLA, err := f.GetWeather(ctx, "test-site-la-tz", laStartQuery, laStartQuery.Add(24*time.Hour))
			require.NoError(t, err)
			assert.Len(t, resLA, 1, "should retrieve weather for LA when querying at 4pm")
		})
	})

	t.Run("UtilityPrices", func(t *testing.T) {
		utilityID := "comed"
		now := time.Now().Truncate(time.Hour).UTC()
		p1 := types.PriceState{
			Price: types.Price{
				TSStart:       now,
				DollarsPerKWH: 0.10,
				Provider:      "comed_besh",
			},
			Confirmed: true,
			TSUpdated: now,
		}
		p2 := types.PriceState{
			Price: types.Price{
				TSStart:       now.Add(time.Hour),
				DollarsPerKWH: 0.12,
				Provider:      "comed_besh",
			},
			Confirmed: false,
			TSUpdated: now,
		}

		require.NoError(t, f.UpsertUtilityPrices(ctx, utilityID, []types.PriceState{p1, p2}, 0))

		// Get both
		prices, err := f.GetUtilityPrices(ctx, utilityID, now, now.Add(2*time.Hour))
		require.NoError(t, err)
		require.Len(t, prices, 2)
		assert.True(t, prices[0].Confirmed)
		assert.False(t, prices[1].Confirmed)
		assert.Equal(t, 0.10, prices[0].DollarsPerKWH)
		assert.Equal(t, 0.12, prices[1].DollarsPerKWH)
		assert.Equal(t, now, prices[0].TSUpdated)
		assert.Equal(t, now, prices[1].TSUpdated)

		// Get range
		pricesRange, err := f.GetUtilityPrices(ctx, utilityID, now, now.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, pricesRange, 1)
		assert.Equal(t, 0.10, pricesRange[0].DollarsPerKWH)

		t.Run("CrossMonthRange", func(t *testing.T) {
			uID := "cross-month-util"
			tAug := time.Date(2026, 8, 31, 23, 0, 0, 0, time.UTC)
			tSep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

			pAug := types.PriceState{
				Price:     types.Price{TSStart: tAug, DollarsPerKWH: 0.05, Provider: "util"},
				Confirmed: true,
			}
			pSep := types.PriceState{
				Price:     types.Price{TSStart: tSep, DollarsPerKWH: 0.08, Provider: "util"},
				Confirmed: false,
			}

			require.NoError(t, f.UpsertUtilityPrices(ctx, uID, []types.PriceState{pAug, pSep}, 0))

			res, err := f.GetUtilityPrices(ctx, uID, tAug, tSep.Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, res, 2)
			assert.Equal(t, tAug, res[0].TSStart)
			assert.Equal(t, 0.05, res[0].DollarsPerKWH)
			assert.True(t, res[0].Confirmed)
			assert.Equal(t, tSep, res[1].TSStart)
			assert.Equal(t, 0.08, res[1].DollarsPerKWH)
			assert.False(t, res[1].Confirmed)
		})

		t.Run("MergeOverwrites", func(t *testing.T) {
			uID := "merge-util"
			tHour := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)

			pInitial := types.PriceState{
				Price:     types.Price{TSStart: tHour, DollarsPerKWH: 0.04, Provider: "util"},
				Confirmed: false,
			}
			require.NoError(t, f.UpsertUtilityPrices(ctx, uID, []types.PriceState{pInitial}, 0))

			// Update same hour with confirmed true and final price
			pFinal := types.PriceState{
				Price:     types.Price{TSStart: tHour, DollarsPerKWH: 0.045, Provider: "util"},
				Confirmed: true,
			}
			require.NoError(t, f.UpsertUtilityPrices(ctx, uID, []types.PriceState{pFinal}, 0))

			res, err := f.GetUtilityPrices(ctx, uID, tHour, tHour.Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, res, 1)
			assert.Equal(t, 0.045, res[0].DollarsPerKWH)
			assert.True(t, res[0].Confirmed)
		})
	})

	t.Run("MigrateLegacyPricing", func(t *testing.T) {
		uID := fmt.Sprintf("legacy-util-%d", time.Now().UnixNano())
		sID := fmt.Sprintf("legacy-site-%d", time.Now().UnixNano())

		// Create site
		require.NoError(t, f.CreateSite(ctx, sID, types.Site{ID: sID}))

		// Seed legacy utility documents directly
		uColl := f.client.Collection("utilities").Doc(uID).Collection("hourly_prices")
		t1 := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 5, 20, 15, 0, 0, 0, time.UTC)

		uP1 := types.PriceState{
			Price:     types.Price{TSStart: t1, DollarsPerKWH: 0.11, Provider: "util"},
			Confirmed: true,
			TSUpdated: t1,
		}
		uP2 := types.PriceState{
			Price:     types.Price{TSStart: t2, DollarsPerKWH: 0.22, Provider: "util"},
			Confirmed: false,
			TSUpdated: t2,
		}
		b1, err := json.Marshal(uP1)
		require.NoError(t, err)
		b2, err := json.Marshal(uP2)
		require.NoError(t, err)

		_, err = uColl.Doc(t1.Format(time.RFC3339)).Set(ctx, map[string]any{"json": string(b1), "timestamp": t1})
		require.NoError(t, err)
		_, err = uColl.Doc(t2.Format(time.RFC3339)).Set(ctx, map[string]any{"json": string(b2), "timestamp": t2})
		require.NoError(t, err)

		// Seed legacy site documents directly
		sColl, err := f.getCollection(sID, "price_history")
		require.NoError(t, err)

		sP1 := types.Price{TSStart: t1, DollarsPerKWH: 0.13, Provider: "site"}
		sP2 := types.Price{TSStart: t2, DollarsPerKWH: 0.26, Provider: "site"}
		sb1, err := json.Marshal(sP1)
		require.NoError(t, err)
		sb2, err := json.Marshal(sP2)
		require.NoError(t, err)

		_, err = sColl.Doc(t1.Format(time.RFC3339)).Set(ctx, map[string]any{"json": string(sb1), "timestamp": t1})
		require.NoError(t, err)
		_, err = sColl.Doc(t2.Format(time.RFC3339)).Set(ctx, map[string]any{"json": string(sb2), "timestamp": t2})
		require.NoError(t, err)

		// 1. Dry run
		dryStats, err := f.MigrateLegacyPricing(ctx, PricingMigrationOptions{
			DryRun:    true,
			UtilityID: uID,
			SiteID:    sID,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, dryStats.UtilitiesInspected)
		assert.Equal(t, 2, dryStats.UtilityHoursMigrated)
		assert.Equal(t, 2, dryStats.UtilityMonthsCreated)
		assert.Equal(t, 1, dryStats.SitesInspected)
		assert.Equal(t, 2, dryStats.SiteHoursMigrated)
		assert.Equal(t, 2, dryStats.SiteMonthsCreated)

		// Verify dry run did not write to monthly_prices
		resU, err := f.GetUtilityPrices(ctx, uID, t1, t2.Add(time.Hour))
		require.NoError(t, err)
		assert.Empty(t, resU)
		resS, err := f.GetPriceHistory(ctx, sID, t1, t2.Add(time.Hour))
		require.NoError(t, err)
		assert.Empty(t, resS)

		// 2. Real migration without purge
		migStats, err := f.MigrateLegacyPricing(ctx, PricingMigrationOptions{
			DryRun:      false,
			PurgeLegacy: false,
			UtilityID:   uID,
			SiteID:      sID,
		})
		require.NoError(t, err)
		assert.Equal(t, 2, migStats.UtilityHoursMigrated)
		assert.Equal(t, 2, migStats.SiteHoursMigrated)

		// Verify monthly_prices now has the data
		resU, err = f.GetUtilityPrices(ctx, uID, t1, t2.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, resU, 2)
		assert.Equal(t, 0.11, resU[0].DollarsPerKWH)
		assert.Equal(t, 0.22, resU[1].DollarsPerKWH)

		resS, err = f.GetPriceHistory(ctx, sID, t1, t2.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, resS, 2)
		assert.Equal(t, 0.13, resS[0].DollarsPerKWH)
		assert.Equal(t, 0.26, resS[1].DollarsPerKWH)

		// 3. Migration with purge legacy
		purgeStats, err := f.MigrateLegacyPricing(ctx, PricingMigrationOptions{
			DryRun:      false,
			PurgeLegacy: true,
			UtilityID:   uID,
			SiteID:      sID,
		})
		require.NoError(t, err)
		assert.Equal(t, 2, purgeStats.UtilityHoursMigrated)
		assert.Equal(t, 2, purgeStats.SiteHoursMigrated)

		// Verify legacy collections are now empty
		uDocs, err := uColl.Limit(1).Documents(ctx).GetAll()
		require.NoError(t, err)
		assert.Empty(t, uDocs)

		sDocs, err := sColl.Limit(1).Documents(ctx).GetAll()
		require.NoError(t, err)
		assert.Empty(t, sDocs)

		// Verify monthly_prices still works
		resUAfter, err := f.GetUtilityPrices(ctx, uID, t1, t2.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, resUAfter, 2)
	})

	t.Run("Interest", func(t *testing.T) {
		// 1. Empty results
		list, err := f.ListInterest(ctx, 10)
		require.NoError(t, err)
		assert.Empty(t, list)

		// 2. Insert multiple for sorting/pagination
		for i := 1; i <= 5; i++ {
			submission := types.InterestSubmission{
				Email:     fmt.Sprintf("user%d@example.com", i),
				Utility:   "other",
				Timestamp: time.Now().Add(time.Duration(i) * time.Hour).Truncate(time.Second).UTC(),
			}
			require.NoError(t, f.UpsertInterest(ctx, submission))
		}

		// 3. Sorting (newest first)
		list, err = f.ListInterest(ctx, 10)
		require.NoError(t, err)
		require.Len(t, list, 5)
		assert.Equal(t, "user5@example.com", list[0].Email)
		assert.Equal(t, "user1@example.com", list[4].Email)

		// 4. Pagination (limit)
		list, err = f.ListInterest(ctx, 2)
		require.NoError(t, err)
		assert.Len(t, list, 2)
		assert.Equal(t, "user5@example.com", list[0].Email)
		assert.Equal(t, "user4@example.com", list[1].Email)

		// 5. DeleteInterest
		t.Run("DeleteInterest", func(t *testing.T) {
			require.NoError(t, f.DeleteInterest(ctx, "user3@example.com"))

			all, err := f.ListInterest(ctx, 10)
			require.NoError(t, err)
			assert.Len(t, all, 4)
			for _, item := range all {
				assert.NotEqual(t, "user3@example.com", item.Email)
			}

			require.NoError(t, f.DeleteInterest(ctx, "nonexistent@example.com"))
		})
	})

	t.Run("HistorySummaries", func(t *testing.T) {
		siteID := "test-site-summaries"

		// 1. Check empty summaries
		start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		res, err := f.GetHistorySummaries(ctx, siteID, start, end)
		require.NoError(t, err)
		assert.Empty(t, res)

		// 2. Perform UpdateHistorySummary to insert fresh data
		es1 := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 15.0},
			},
		}
		hs1 := types.HistorySummary{
			Energy: []types.DailyEnergyStats{es1},
		}

		updated, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hs1)
		require.NoError(t, err)
		if assert.Len(t, updated.Energy, 1) {
			if assert.Len(t, updated.Energy[0].Hourly, 1) {
				assert.Equal(t, 15.0, updated.Energy[0].Hourly[0].SolarKWH)
			}
		}

		// 3. Perform UpdateHistorySummary to merge/overwrite data
		es2 := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), SolarKWH: 25.0},
			},
		}
		hs2 := types.HistorySummary{
			Energy: []types.DailyEnergyStats{es2},
		}
		updated2, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hs2)
		require.NoError(t, err)
		if assert.Len(t, updated2.Energy, 2) {
			if assert.Len(t, updated2.Energy[0].Hourly, 1) {
				assert.Equal(t, 15.0, updated2.Energy[0].Hourly[0].SolarKWH)
			}
			if assert.Len(t, updated2.Energy[1].Hourly, 1) {
				assert.Equal(t, 25.0, updated2.Energy[1].Hourly[0].SolarKWH)
			}
		}

		// 4. Retrieve summaries and check overlapping months
		resSummaries, err := f.GetHistorySummaries(ctx, siteID, start, end)
		require.NoError(t, err)
		if assert.Len(t, resSummaries, 1) {
			assert.Len(t, resSummaries[0].Energy, 2)
		}

		// Verify that GetHistorySummaries does not truncate dates outside [start, end)
		// Querying [2026-06-02, 2026-06-03) still returns the full month including 2026-06-01
		subRangeStart := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
		subRangeEnd := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
		noTruncRes, err := f.GetHistorySummaries(ctx, siteID, subRangeStart, subRangeEnd)
		require.NoError(t, err)
		if assert.Len(t, noTruncRes, 1) {
			assert.Len(t, noTruncRes[0].Energy, 2)
		}

		// 5. Merge duplicate day (verify it overwrites rather than appends)
		es1Overwrite := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 99.0},
			},
		}
		hsOverwrite := types.HistorySummary{
			Energy: []types.DailyEnergyStats{es1Overwrite},
		}
		updated3, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hsOverwrite)
		require.NoError(t, err)
		if assert.Len(t, updated3.Energy, 2) { // 2026-06-01 (overwritten) and 2026-06-02 (kept)
			var found01 *types.DailyEnergyStats
			for i := range updated3.Energy {
				if updated3.Energy[i].TSDayStart.Format("2006-01-02") == "2026-06-01" {
					found01 = &updated3.Energy[i]
				}
			}
			if assert.NotNil(t, found01) && assert.Len(t, found01.Hourly, 1) {
				assert.Equal(t, 99.0, found01.Hourly[0].SolarKWH) // Verified overwrite
			}
		}

		// 6. Test weather data merging and chronological sorting
		w1 := types.Weather{
			TSDayStart: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
			ForecastHours: []types.HourlyWeather{
				{TSHourStart: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC), TemperatureC: 20.0},
			},
		}
		w2 := types.Weather{
			TSDayStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			ForecastHours: []types.HourlyWeather{
				{TSHourStart: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC), TemperatureC: 10.0},
			},
		}
		hsWeather := types.HistorySummary{
			Weather: []types.Weather{w1, w2}, // out of order input
		}
		updated4, err := f.UpdateHistorySummary(ctx, siteID, "2026-06", hsWeather)
		require.NoError(t, err)
		if assert.Len(t, updated4.Weather, 2) {
			assert.Equal(t, "2026-06-01", updated4.Weather[0].TSDayStart.Format("2006-01-02"))
			assert.Equal(t, "2026-06-02", updated4.Weather[1].TSDayStart.Format("2006-01-02"))
		}

		// 7. Insert and retrieve across multiple months
		esPrevMonth := types.DailyEnergyStats{
			TSDayStart: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
			Hourly: []types.EnergyStats{
				{TSHourStart: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			},
		}
		hsPrev := types.HistorySummary{
			Energy: []types.DailyEnergyStats{esPrevMonth},
		}
		_, err = f.UpdateHistorySummary(ctx, siteID, "2026-05", hsPrev)
		require.NoError(t, err)

		multiRes, err := f.GetHistorySummaries(ctx, siteID, start, end)
		require.NoError(t, err)
		assert.Len(t, multiRes, 2) // both months returned

		// Query starting more than 23 hours into June excludes previous month (May)
		midMonthStart := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
		juneOnlyRes, err := f.GetHistorySummaries(ctx, siteID, midMonthStart, end)
		require.NoError(t, err)
		assert.Len(t, juneOnlyRes, 1)

		// 8. Assert raw document properties (latestDate, earliestDate fields for querying)
		coll, err := f.getCollection(siteID, "history_summary")
		require.NoError(t, err)

		doc202606, err := coll.Doc("2026-06").Get(ctx)
		require.NoError(t, err)

		rawEarliest, err := doc202606.DataAt("earliestDate")
		require.NoError(t, err)
		rawLatest, err := doc202606.DataAt("latestDate")
		require.NoError(t, err)

		assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), rawEarliest.(time.Time).UTC())
		assert.Equal(t, time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), rawLatest.(time.Time).UTC())
	})

	t.Run("UserPushSubscriptions", func(t *testing.T) {
		siteID := "site-push-cleanup"
		require.NoError(t, f.CreateSite(ctx, siteID, types.Site{
			ID: siteID,
		}))
		_, err := f.SetSettings(ctx, siteID, types.Settings{
			Notifications: map[string]types.UserNotificationSettings{
				"push-user@test.com": {
					MorningSummaryEnabled: true,
					MorningSummaryHour:    7,
					MorningSummaryFlavor:  "metrics_heavy",
				},
			},
		}, 1, time.Time{})
		require.NoError(t, err)

		userID := "push-user@test.com"
		user := types.User{
			ID:    userID,
			Email: userID,
			Sites: []types.UserSite{{ID: siteID, Name: "My Site"}},
		}
		require.NoError(t, f.CreateUser(ctx, user))

		sub1 := types.PushSubscription{
			ID:        "sub-1",
			Endpoint:  "https://fcm.googleapis.com/fcm/send/sub-1",
			Keys:      types.PushSubscriptionKeys{P256DH: "key1", Auth: "auth1"},
			UserAgent: "Chrome Mac",
			TSCreated: time.Now().UTC(),
		}
		require.NoError(t, f.AddUserPushSubscription(ctx, userID, sub1))

		gotUser, err := f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Subscriptions, 1) {
			assert.Equal(t, "sub-1", gotUser.Subscriptions[0].ID)
			assert.Equal(t, "https://fcm.googleapis.com/fcm/send/sub-1", gotUser.Subscriptions[0].Endpoint)
		}

		// Add second subscription
		sub2 := types.PushSubscription{
			ID:        "sub-2",
			Endpoint:  "https://web.push.apple.com/sub-2",
			Keys:      types.PushSubscriptionKeys{P256DH: "key2", Auth: "auth2"},
			UserAgent: "Safari iOS",
			TSCreated: time.Now().UTC(),
		}
		require.NoError(t, f.AddUserPushSubscription(ctx, userID, sub2))

		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		assert.Len(t, gotUser.Subscriptions, 2)

		// Remove first subscription - user still has sub2, so site notification settings remain
		require.NoError(t, f.RemoveUserPushSubscription(ctx, userID, sub1.Endpoint))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Subscriptions, 1) {
			assert.Equal(t, "sub-2", gotUser.Subscriptions[0].ID)
		}
		gotSettings, _, _, err := f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		assert.Contains(t, gotSettings.Notifications, userID)

		// Remove last subscription - user has 0 subscriptions, so site notification settings should be cleaned up!
		require.NoError(t, f.RemoveUserPushSubscription(ctx, userID, sub2.Endpoint))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		assert.Len(t, gotUser.Subscriptions, 0)

		gotSettings, _, _, err = f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		assert.NotContains(t, gotSettings.Notifications, userID)

		// Test ReplaceUserPushSubscription (renewal and revocation)
		require.NoError(t, f.AddUserPushSubscription(ctx, userID, sub1))

		sub1Rotated := types.PushSubscription{
			ID:        "sub-1-rotated",
			Endpoint:  "https://fcm.googleapis.com/fcm/send/sub-1-rotated",
			Keys:      types.PushSubscriptionKeys{P256DH: "key1-rot", Auth: "auth1-rot"},
			UserAgent: "Chrome Mac",
		}

		// Wrong auth secret fails
		err = f.ReplaceUserPushSubscription(ctx, sub1.Endpoint, "wrong-auth", &sub1Rotated)
		assert.Error(t, err)

		// Valid replacement succeeds
		require.NoError(t, f.ReplaceUserPushSubscription(ctx, sub1.Endpoint, sub1.Keys.Auth, &sub1Rotated))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Subscriptions, 1) {
			assert.Equal(t, "sub-1-rotated", gotUser.Subscriptions[0].ID)
			assert.Equal(t, "https://fcm.googleapis.com/fcm/send/sub-1-rotated", gotUser.Subscriptions[0].Endpoint)
		}

		// Revocation with nil newSub removes subscription
		require.NoError(t, f.ReplaceUserPushSubscription(ctx, sub1Rotated.Endpoint, "auth1-rot", nil))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		assert.Len(t, gotUser.Subscriptions, 0)
	})

	t.Run("UserApps", func(t *testing.T) {
		userID := "apps-user@test.com"
		now := time.Now().UTC().Truncate(time.Second)
		require.NoError(t, f.CreateUser(ctx, types.User{
			ID:    userID,
			Email: userID,
			Sites: []types.UserSite{{ID: "site-1", Name: "Site 1"}},
		}))

		pendingApp := types.UserApp{
			ID:               "app-oauth",
			Name:             "Claude",
			Type:             types.AppTypeOAuth,
			SiteID:           "site-1",
			TSCreated:        now,
			PendingCodeHash:  "valid-hash-123",
			PendingExpiresAt: now.Add(time.Minute),
			CodeChallenge:    "challenge-123",
			RedirectURI:      "https://claude.ai/api/mcp/auth_callback",
		}
		require.NoError(t, f.AddUserApp(ctx, userID, pendingApp))

		gotUser, err := f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Apps, 1) {
			assert.Equal(t, "app-oauth", gotUser.Apps[0].ID)
			assert.Equal(t, "valid-hash-123", gotUser.Apps[0].PendingCodeHash)
		}

		redeemedApp := types.UserApp{
			ID:        "app-oauth",
			Name:      "Claude",
			Type:      types.AppTypeOAuth,
			SiteID:    "site-1",
			TSCreated: now,
		}

		// Mismatched ExpectedPendingCodeHash returns ErrAppConflict
		wrongHash := "wrong-hash"
		err = f.ReplaceUserApp(ctx, userID, redeemedApp, ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &wrongHash,
		})
		assert.ErrorIs(t, err, ErrAppConflict)

		// Expecting empty PendingCodeHash when app is still pending returns ErrAppConflict
		emptyHash := ""
		err = f.ReplaceUserApp(ctx, userID, redeemedApp, ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &emptyHash,
		})
		assert.ErrorIs(t, err, ErrAppConflict)

		// Attempting to move app to a different siteID returns ErrAppConflict
		wrongSiteApp := redeemedApp
		wrongSiteApp.SiteID = "site-other"
		validHash := "valid-hash-123"
		err = f.ReplaceUserApp(ctx, userID, wrongSiteApp, ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &validHash,
		})
		assert.ErrorIs(t, err, ErrAppConflict)

		// Matching ExpectedPendingCodeHash replaces the app
		require.NoError(t, f.ReplaceUserApp(ctx, userID, redeemedApp, ReplaceUserAppOptions{
			ExpectedPendingCodeHash: &validHash,
		}))

		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		if assert.Len(t, gotUser.Apps, 1) {
			assert.Equal(t, "app-oauth", gotUser.Apps[0].ID)
			assert.Empty(t, gotUser.Apps[0].PendingCodeHash)
		}

		// Removing app with wrong siteID returns ErrAppNotFound
		err = f.RemoveUserApp(ctx, userID, "site-other", "app-oauth")
		assert.ErrorIs(t, err, ErrAppNotFound)

		// Removing app with matching siteID succeeds
		require.NoError(t, f.RemoveUserApp(ctx, userID, "site-1", "app-oauth"))
		gotUser, err = f.GetUser(ctx, userID)
		require.NoError(t, err)
		assert.Empty(t, gotUser.Apps)
	})

	t.Run("SiteNotificationSettings", func(t *testing.T) {
		siteID := "site-notif-test"
		site := types.Site{
			ID:         siteID,
			InviteCode: "invite-123",
		}
		require.NoError(t, f.CreateSite(ctx, siteID, site))
		_, err := f.SetSettings(ctx, siteID, types.Settings{}, 1, time.Time{})
		require.NoError(t, err)

		user1 := "user1@test.com"
		settings1 := types.UserNotificationSettings{
			MorningSummaryEnabled: true,
			MorningSummaryHour:    7,
			MorningSummaryFlavor:  "metrics_heavy",
		}
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user1, settings1))

		gotSettings, _, _, err := f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		if assert.NotNil(t, gotSettings.Notifications) {
			assert.Equal(t, settings1, gotSettings.Notifications[user1])
		}

		user2 := "user2@test.com"
		settings2 := types.UserNotificationSettings{
			MorningSummaryEnabled: true,
			MorningSummaryHour:    8,
			MorningSummaryFlavor:  "home_planner",
		}
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user2, settings2))

		gotSettings, _, _, err = f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		if assert.Len(t, gotSettings.Notifications, 2) {
			assert.Equal(t, settings1, gotSettings.Notifications[user1])
			assert.Equal(t, settings2, gotSettings.Notifications[user2])
		}

		// Update with empty struct deletes user1 from site notifications
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user1, types.UserNotificationSettings{}))
		gotSettings, _, _, err = f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		if assert.Len(t, gotSettings.Notifications, 1) {
			assert.NotContains(t, gotSettings.Notifications, user1)
			assert.Equal(t, settings2, gotSettings.Notifications[user2])
		}

		// Setting identical settings is a no-op that succeeds without error
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user2, settings2))

		// Removing a user ID that was not present is a no-op that succeeds without error
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, "nonexistent@test.com", types.UserNotificationSettings{}))
		gotSettings, _, _, err = f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		if assert.Len(t, gotSettings.Notifications, 1) {
			assert.Equal(t, settings2, gotSettings.Notifications[user2])
		}

		// Update user2 with empty struct deletes user2 and leaves Notifications nil/empty
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, user2, types.UserNotificationSettings{}))
		gotSettings, _, _, err = f.GetSettings(ctx, siteID)
		require.NoError(t, err)
		assert.Empty(t, gotSettings.Notifications)

		// Removing a user when Notifications is already empty is also a no-op
		require.NoError(t, f.UpdateSiteNotificationSettings(ctx, siteID, "nonexistent@test.com", types.UserNotificationSettings{}))
	})

	t.Run("NotificationLogs", func(t *testing.T) {
		siteID := "site-notif-logs"
		now := time.Date(2026, 9, 4, 7, 15, 0, 0, time.UTC)
		log1 := types.NotificationLog{
			ID:         "log-1",
			TSCreated:  now,
			UserID:     "user1@test.com",
			Type:       "morning_summary",
			Flavor:     "metrics_heavy",
			Title:      "Morning Summary",
			Body:       "74% SOC",
			Success:    true,
			StatusCode: 201,
		}

		require.NoError(t, f.AppendNotificationLog(ctx, siteID, log1))

		logs, err := f.GetNotificationLogs(ctx, siteID, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.Equal(t, "log-1", logs[0].ID)
			assert.False(t, logs[0].Clicked)
		}

		// Click tracking: first click marks it clicked and sets TSClicked
		clickTime := now.Add(5 * time.Minute)
		require.NoError(t, f.RecordNotificationClick(ctx, siteID, "2026-09", "log-1", clickTime))

		logs, err = f.GetNotificationLogs(ctx, siteID, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.True(t, logs[0].Clicked)
			assert.Equal(t, clickTime, logs[0].TSClicked)
		}

		// Subsequent click when Clicked is already true is a no-op and preserves original TSClicked
		laterClickTime := now.Add(15 * time.Minute)
		require.NoError(t, f.RecordNotificationClick(ctx, siteID, "2026-09", "log-1", laterClickTime))

		logs, err = f.GetNotificationLogs(ctx, siteID, now.Add(-1*time.Hour), now.Add(1*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, logs, 1) {
			assert.True(t, logs[0].Clicked)
			assert.Equal(t, clickTime, logs[0].TSClicked) // Unchanged
		}

		// Range query within same month but after latestDate returns empty slice
		futureDayLogs, err := f.GetNotificationLogs(ctx, siteID, now.AddDate(0, 0, 5), now.AddDate(0, 0, 6))
		require.NoError(t, err)
		assert.Empty(t, futureDayLogs)

		// Range query across months with no logs returns empty slice
		pastLogs, err := f.GetNotificationLogs(ctx, siteID, now.AddDate(-2, 0, 0), now.AddDate(-1, 0, 0))
		require.NoError(t, err)
		assert.Empty(t, pastLogs)

		// Range query with inverted start/end returns empty slice
		invertedLogs, err := f.GetNotificationLogs(ctx, siteID, now.Add(1*time.Hour), now.Add(-1*time.Hour))
		require.NoError(t, err)
		assert.Empty(t, invertedLogs)

		// Verify earliestDate and latestDate stored at document root
		coll, err := f.getCollection(siteID, "notification_logs")
		require.NoError(t, err)
		docSnap, err := coll.Doc("2026-09").Get(ctx)
		require.NoError(t, err)
		rawEarliest, err := docSnap.DataAt("earliestDate")
		require.NoError(t, err)
		rawLatest, err := docSnap.DataAt("latestDate")
		require.NoError(t, err)
		assert.Equal(t, now, rawEarliest.(time.Time).UTC())
		assert.Equal(t, now, rawLatest.(time.Time).UTC())

		// Verify non-UTC inputs are correctly converted to UTC, stored in UTC,
		// and that GetNotificationLogs truncates logs outside [start, end)
		cst := time.FixedZone("CDT", -5*3600)
		log2Time := time.Date(2026, 10, 15, 14, 0, 0, 0, cst) // 19:00 UTC
		log3Time := time.Date(2026, 10, 15, 16, 0, 0, 0, cst) // 21:00 UTC
		require.NoError(t, f.AppendNotificationLog(ctx, siteID, types.NotificationLog{
			ID:        "log-2",
			TSCreated: log2Time,
			UserID:    "user2@test.com",
		}))
		require.NoError(t, f.AppendNotificationLog(ctx, siteID, types.NotificationLog{
			ID:        "log-3",
			TSCreated: log3Time,
			UserID:    "user3@test.com",
		}))

		// Query using non-UTC start and end times, verifying truncation of log-2
		queryStartCST := time.Date(2026, 10, 15, 15, 0, 0, 0, cst) // 20:00 UTC (after log2 at 19:00 UTC)
		queryEndCST := time.Date(2026, 10, 15, 18, 0, 0, 0, cst)   // 23:00 UTC (after log3 at 21:00 UTC)
		cstLogs, err := f.GetNotificationLogs(ctx, siteID, queryStartCST, queryEndCST)
		require.NoError(t, err)
		if assert.Len(t, cstLogs, 1) {
			assert.Equal(t, "log-3", cstLogs[0].ID)
		}

		// Verify that end boundary is exclusive [start, end)
		// A query ending exactly at log3Time must exclude log-3
		exclusiveLogs, err := f.GetNotificationLogs(ctx, siteID, queryStartCST, log3Time)
		require.NoError(t, err)
		assert.Empty(t, exclusiveLogs)

		// Verify that document root properties for 2026-10 were stored in UTC
		docSnap10, err := coll.Doc("2026-10").Get(ctx)
		require.NoError(t, err)
		rawEarliest10, err := docSnap10.DataAt("earliestDate")
		require.NoError(t, err)
		rawLatest10, err := docSnap10.DataAt("latestDate")
		require.NoError(t, err)
		assert.Equal(t, log2Time.UTC(), rawEarliest10.(time.Time).UTC())
		assert.Equal(t, log3Time.UTC(), rawLatest10.(time.Time).UTC())
	})
}

func TestIsRetryableFirestoreErr(t *testing.T) {
	t.Run("Nil error", func(t *testing.T) {
		assert.False(t, isRetryableFirestoreErr(nil))
	})

	t.Run("Unavailable error", func(t *testing.T) {
		err := status.Error(codes.Unavailable, "error reading from server: connection reset by peer")
		assert.True(t, isRetryableFirestoreErr(err))
	})

	t.Run("Wrapped Unavailable error", func(t *testing.T) {
		err := fmt.Errorf("failed doc read: %w", status.Error(codes.Unavailable, "transport is closing"))
		assert.True(t, isRetryableFirestoreErr(err))
	})

	t.Run("ResourceExhausted error", func(t *testing.T) {
		err := status.Error(codes.ResourceExhausted, "quota exceeded")
		assert.True(t, isRetryableFirestoreErr(err))
	})

	t.Run("NotFound error", func(t *testing.T) {
		err := status.Error(codes.NotFound, "document not found")
		assert.False(t, isRetryableFirestoreErr(err))
	})

	t.Run("Generic error", func(t *testing.T) {
		assert.False(t, isRetryableFirestoreErr(errors.New("some regular error")))
	})
}

func TestParseLegacyActionJSON(t *testing.T) {
	t.Run("ProductionShortPlanAction", func(t *testing.T) {
		rawJSON := []byte(`{"batteryMode":-1,"capacityAt":"2026-10-11T11:00:00-05:00","chargeToSoc":25,"currentPrice":{"dollarsPerKWH":0.04810999999999999,"gridUseDollarsPerKWH":0.11654290735999999,"provider":"comed_besh","tsEnd":"2026-10-10T18:00:00-05:00","tsStart":"2026-10-10T17:00:00-05:00"},"deficitAt":"0001-01-01T00:00:00Z","description":"Discharging battery to cover household load.","hitBufferedDeficitAt":"0001-01-01T00:00:00Z","hitThresholdDeficitAt":"0001-01-01T00:00:00Z","q3HomeUsageKWH":3.214642537876033,"reason":"sufficientBattery","recentHomeUsageKWH":2.071416666666667,"shortPlan":{"h":25,"nb":0.8787479867152629,"p":[{"bm":-1,"d":0.3333333333333333,"e":0.04810999999999999,"es":99.2625745604791,"i":0.16465290736,"l":0.3818394016436127,"r":"sufficientBattery","rs":25,"s":0.32166666666666666,"sm":2,"ss":99.474,"te":"2026-10-10T17:27:11.343112381-05:00","ts":"2026-10-10T17:07:11.343112381-05:00"},{"bm":-1,"d":0.5468491354497222,"e":0.04810999999999999,"es":97.78417974884252,"i":0.16465290736,"l":0.6264256400083466,"r":"sufficientBattery","rs":25,"s":0.20566710029110363,"sm":2,"ss":99.2625745604791,"te":"2026-10-10T18:00:00-05:00","ts":"2026-10-10T17:27:11.343112381-05:00"}],"te":0.8787479867152629,"ts":"2026-10-10T22:07:11.343112381Z"},"simulationParams":{"averageSolarEfficiency":0.012228873447686464,"clippingCapKWH":10.243749999999999,"detectedShift":"none","panelAzimuth":90,"panelTilt":25},"solarMode":2,"strategyBenefitDollars":0.8787479867152629,"systemStatus":{"batteryAboveMinSOC":true,"batteryCapacityKWH":30,"batteryKW":1.365,"batterySOC":99.474,"elevatedMinBatterySOC":false,"gridKW":0.054,"homeKW":2.383,"maxBatteryChargeKW":16,"maxBatteryDischargeKW":20,"solarKW":0.965,"timeLocation":"America/Chicago","timestamp":"2026-10-10T17:07:11.343112381-05:00","vppSOC":20},"systemTimestamp":"2026-10-10T17:07:11.343112381-05:00","timestamp":"2026-10-10T22:07:11.343112381Z"}`)

		act, err := parseLegacyActionJSON(rawJSON)
		require.NoError(t, err)

		assert.Empty(t, act.Location)
		assert.Equal(t, "America/Chicago", act.SystemTimestamp.Location().String())
		assert.Equal(t, "America/Chicago", act.SystemStatus.Timestamp.Location().String())
		assert.Equal(t, "America/Chicago", act.CurrentPrice.TSStart.Location().String())
		assert.Equal(t, "America/Chicago", act.HitCapacityAt.Location().String())
		assert.True(t, act.HitDeficitAt.IsZero())

		require.NotNil(t, act.Plan)
		assert.Empty(t, act.Plan.ShortPeriods)
		assert.Equal(t, 25, act.Plan.HorizonHours)
		if assert.Len(t, act.Plan.Periods, 2) {
			p0 := act.Plan.Periods[0]
			assert.Equal(t, "America/Chicago", p0.TSStart.Location().String())
			assert.Equal(t, "America/Chicago", p0.TSEnd.Location().String())
			assert.Equal(t, 0.1647, p0.ImportDollars)
			assert.Equal(t, 0.0481, p0.ExportDollars)
			assert.Equal(t, 99.5, p0.StartSOC)
			assert.Equal(t, 99.3, p0.EndSOC)
			assert.Equal(t, 25.0, p0.ReserveSOC)
			assert.Equal(t, 0.382, p0.LoadKWH)
			assert.Equal(t, 0.322, p0.SolarKWH)
		}

		// Verify round-trip through CompactForStorage -> JSON -> ExpandFromStorage
		compacted := act.CompactForStorage()
		assert.Equal(t, "America/Chicago", compacted.Location)
		data, err := json.Marshal(compacted)
		require.NoError(t, err)

		var restored types.Action
		require.NoError(t, json.Unmarshal(data, &restored))
		restored.ExpandFromStorage()
		assert.True(t, reflect.DeepEqual(act, restored))
		assert.Equal(t, act, restored)
	})

	t.Run("ProductionDecideActionWithTimezone", func(t *testing.T) {
		rawJSON := []byte(`{"timestamp":"2026-09-15T00:09:06.822395586Z","systemTimestamp":"2026-09-14T19:09:06.822395586-05:00","batteryMode":-1,"solarMode":2,"reason":"sufficientBattery","description":"Sufficient battery.","currentPrice":{"provider":"comed_besh","tsStart":"2026-09-14T19:00:00-05:00","tsEnd":"2026-09-14T20:00:00-05:00","dollarsPerKWH":0.06592,"gridUseDollarsPerKWH":0.041169209660000004},"systemStatus":{"timestamp":"2026-09-14T19:09:06.822395586-05:00","timeLocation":"America/Chicago","batterySOC":81.474,"batteryKW":0.75,"batteryCapacityKWH":30,"maxBatteryChargeKW":16,"maxBatteryDischargeKW":20,"solarKW":0,"gridKW":0.01,"homeKW":0.746,"elevatedMinBatterySOC":false,"batteryAboveMinSOC":true,"vppSOC":20},"deficitAt":"0001-01-01T00:00:00Z","hitBufferedDeficitAt":"0001-01-01T00:00:00Z","hitThresholdDeficitAt":"0001-01-01T00:00:00Z","capacityAt":"2026-09-15T12:39:26.343947672-05:00","simulationParams":{"clippingCapKWH":10.247333333333334,"panelAzimuth":135,"panelTilt":25,"averageSolarEfficiency":0.010545211265857335,"detectedShift":"none"}}`)

		act, err := parseLegacyActionJSON(rawJSON)
		require.NoError(t, err)
		assert.Nil(t, act.Plan)
		assert.Equal(t, "America/Chicago", act.SystemTimestamp.Location().String())
		assert.Equal(t, "America/Chicago", act.HitCapacityAt.Location().String())

		compacted := act.CompactForStorage()
		data, err := json.Marshal(compacted)
		require.NoError(t, err)

		var restored types.Action
		require.NoError(t, json.Unmarshal(data, &restored))
		restored.ExpandFromStorage()
		assert.True(t, reflect.DeepEqual(act, restored))
	})

	t.Run("ProductionEarliestDecideActionWithoutTimezone", func(t *testing.T) {
		rawJSON := []byte(`{"timestamp":"2026-02-06T05:15:11.513218608Z","batteryMode":1,"solarMode":0,"description":"Deficit predicted and higher prices later (0.020 < 0.065).","currentPrice":{"tsStart":"2026-02-05T23:00:00-06:00","tsEnd":"2026-02-05T23:05:00-06:00","dollarsPerKWH":0.02},"systemStatus":{"timestamp":"0001-01-01T00:00:00Z","batterySOC":0,"eachBatterySOC":null,"batteryKW":0,"eachBatteryKW":null,"batteryCapacityKWH":0,"maxBatteryChargeKW":0,"maxBatteryDischargeKW":0,"solarKW":0,"gridKW":0,"homeKW":0,"canExportSolar":false,"canExportBattery":false,"canImportBattery":false,"emergencyMode":false}}`)

		act, err := parseLegacyActionJSON(rawJSON)
		require.NoError(t, err)
		assert.True(t, act.SystemTimestamp.IsZero())
		assert.Empty(t, act.Location)
		require.NotNil(t, act.CurrentPrice)
		assert.Equal(t, 0.02, act.CurrentPrice.DollarsPerKWH)

		compacted := act.CompactForStorage()
		assert.Empty(t, compacted.Location)
		assert.True(t, compacted.SystemTimestamp.IsZero())
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := parseLegacyActionJSON([]byte(`{not valid json`))
		assert.ErrorContains(t, err, "failed to unmarshal action json")
	})
}
