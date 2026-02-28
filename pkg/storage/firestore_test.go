package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, f.SetSettings(ctx, "test-site", settings, 1))

		gotSettings, version, err := f.GetSettings(ctx, "test-site")
		require.NoError(t, err)
		assert.Equal(t, 1, version)
		assert.Equal(t, settings.AlwaysChargeUnderDollarsPerKWH, gotSettings.AlwaysChargeUnderDollarsPerKWH)
		assert.Equal(t, settings.MinBatterySOC, gotSettings.MinBatterySOC)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
		assert.Equal(t, settings.DryRun, gotSettings.DryRun)
	})

	t.Run("EmptySiteID", func(t *testing.T) {
		_, _, err := f.GetSettings(ctx, "")
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
			// Temporarily lower batch size
			origBatchSize := maxBatchSize
			maxBatchSize = 2
			defer func() { maxBatchSize = origBatchSize }()

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
	})

	t.Run("EnergyHistory", func(t *testing.T) {
		now := time.Now().Truncate(time.Hour).UTC() // Truncate to hour since energy stats are hourly
		stats := types.EnergyStats{
			TSHourStart:       now,
			SolarKWH:          5.0,
			BatteryChargedKWH: 2.0,
		}
		require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", []types.EnergyStats{stats}, 0))

		t.Run("UpsertMultipleBatches", func(t *testing.T) {
			// Temporarily lower batch size
			origBatchSize := maxBatchSize
			maxBatchSize = 2
			defer func() { maxBatchSize = origBatchSize }()

			var batchStats []types.EnergyStats
			for i := 0; i < 5; i++ {
				batchStats = append(batchStats, types.EnergyStats{
					TSHourStart:       now.Add(time.Duration(i+1) * time.Hour),
					SolarKWH:          float64(i) * 1.0,
				})
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", batchStats, 0))

			res, err := f.GetEnergyHistory(ctx, "test-site", now.Add(1*time.Hour), now.Add(6*time.Hour))
			require.NoError(t, err)
			assert.Len(t, res, 5)
			for i := 0; i < 5; i++ {
				assert.Equal(t, float64(i)*1.0, res[i].SolarKWH)
			}
		})

		t.Run("GetEnergyHistory", func(t *testing.T) {
			energyHistory, err := f.GetEnergyHistory(ctx, "test-site", now.Add(-1*time.Minute), now.Add(2*time.Hour))
			require.NoError(t, err)

			foundS := false
			for _, s := range energyHistory {
				if s.SolarKWH == 5.0 && s.TSHourStart.Equal(stats.TSHourStart) {
					foundS = true
				}
			}
			assert.True(t, foundS, "did not find inserted energy stats")
		})

		t.Run("GetLatestEnergyHistoryTime", func(t *testing.T) {
			// Since we just inserted 'now', and it's the latest in this test,
			// let's insert an older one to assume 'now' is still the latest,
			// or insert a newer one to verify update.
			future := now.Add(24 * time.Hour)
			futureStats := types.EnergyStats{
				TSHourStart:       future,
				SolarKWH:          1.0,
				BatteryChargedKWH: 1.0,
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, "test-site", []types.EnergyStats{futureStats}, 0))

			latestTime, version, err := f.GetLatestEnergyHistoryTime(ctx, "test-site")
			require.NoError(t, err)
			assert.Equal(t, future, latestTime, "latest time should match the future timestamp we just inserted")
			assert.Equal(t, 0, version, "version should be 0 because we didn't set it explicitly")
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
	})
}

func TestFirestore_Feedback(t *testing.T) {
	// Use a test project ID
	projectID := "test-project-id"

	// Use a random database for isolation
	randDB := fmt.Sprintf("test-db-%d", time.Now().UnixNano())
	provider := &FirestoreProvider{
		projectID: projectID,
		database:  randDB,
	}

	ctx := context.Background()
	err := provider.Init(ctx)
	require.NoError(t, err)

	defer provider.Close()

	// Insert feedbacks
	fb1 := types.Feedback{
		ID:        "2023-10-27T10:00:00Z_site1",
		SiteID:    "site1",
		UserID:    "user1",
		Sentiment: "happy",
		Comment:   "Great job!",
		Time:      time.Date(2023, 10, 27, 10, 0, 0, 0, time.UTC),
	}
	fb2 := types.Feedback{
		ID:        "2023-10-27T11:00:00Z_site1",
		SiteID:    "site1",
		UserID:    "user2",
		Sentiment: "sad",
		Comment:   "Needs work.",
		Time:      time.Date(2023, 10, 27, 11, 0, 0, 0, time.UTC),
	}
	fb3 := types.Feedback{
		ID:        "2023-10-27T12:00:00Z_site1",
		SiteID:    "site1",
		UserID:    "user3",
		Sentiment: "neutral",
		Comment:   "It's okay.",
		Time:      time.Date(2023, 10, 27, 12, 0, 0, 0, time.UTC),
	}

	err = provider.InsertFeedback(ctx, fb1)
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
}
