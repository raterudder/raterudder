package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iterator"
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
			start := time.Now().Add(-1000 * 24 * time.Hour).Truncate(24 * time.Hour).UTC()
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

	t.Run("Migration", func(t *testing.T) {
		siteID := fmt.Sprintf("migration-site-%d", time.Now().UnixNano())
		now := time.Now().Truncate(24 * time.Hour).UTC()

		// 1. Manually insert Version 2 (hourly) data
		coll, err := f.getCollection(siteID, "energy_history")
		require.NoError(t, err)

		h1 := now.Add(10 * time.Hour)
		h2 := now.Add(11 * time.Hour)
		stats1 := types.EnergyStats{TSHourStart: h1, SolarKWH: 1.0}
		stats2 := types.EnergyStats{TSHourStart: h2, SolarKWH: 2.0}

		for _, s := range []types.EnergyStats{stats1, stats2} {
			jsonBytes, _ := json.Marshal(s)
			docID := s.TSHourStart.UTC().Format(time.RFC3339)
			_, err := coll.Doc(docID).Set(ctx, map[string]any{
				"json":      string(jsonBytes),
				"timestamp": s.TSHourStart,
				"version":   2,
			})
			require.NoError(t, err)
		}

		// Verify GetLatestEnergyHistoryTime identifies the latest Version 2 record and triggers migration
		latestTime, version, err := f.GetLatestEnergyHistoryTime(ctx, siteID)
		require.NoError(t, err)
		assert.Equal(t, h2, latestTime)
		assert.Equal(t, 3, version) // Should be upgraded to 3

		// Verify that data is now in Version 3 format
		docs, err := f.GetEnergyHistory(ctx, siteID, now, now.Add(24*time.Hour))
		require.NoError(t, err)
		require.Len(t, docs, 1)
		assert.Equal(t, now, docs[0].TSDayStart)
		require.Len(t, docs[0].Hourly, 2)

		// Verify Version 2 docs are deleted
		iter := coll.Where("version", "==", 2).Documents(ctx)
		_, err = iter.Next()
		assert.ErrorIs(t, err, iterator.Done)
		iter.Stop()

		t.Run("MigrationWithMixedData", func(t *testing.T) {
			siteID := fmt.Sprintf("mixed-migration-%d", time.Now().UnixNano())
			day1 := now.Add(-48 * time.Hour)
			day2 := now.Add(-24 * time.Hour)

			// Day 1: Version 2 (Hourly)
			coll, _ := f.getCollection(siteID, "energy_history")
			s1 := types.EnergyStats{TSHourStart: day1.Add(12 * time.Hour), SolarKWH: 10.0}
			jsonBytes, _ := json.Marshal(s1)
			_, err := coll.Doc(s1.TSHourStart.UTC().Format(time.RFC3339)).Set(ctx, map[string]any{
				"json":      string(jsonBytes),
				"timestamp": s1.TSHourStart,
				"version":   2,
			})
			require.NoError(t, err)

			// Day 2: Version 3 (Daily)
			s3 := types.DailyEnergyStats{
				TSDayStart: day2,
				Hourly: []types.EnergyStats{
					{TSHourStart: day2.Add(12 * time.Hour), SolarKWH: 20.0},
				},
			}
			require.NoError(t, f.UpsertEnergyHistories(ctx, siteID, []types.DailyEnergyStats{s3}, 3))

			// 1. Query for range including both: only returns Day 2 because Day 1 is still v2 and we are lazy.
			all, err := f.GetEnergyHistory(ctx, siteID, day1, now)
			require.NoError(t, err)
			assert.Len(t, all, 1, "should have 1 items before migration because we're lazy")

			// 2. Query only Day 1 range: should find NO v3 docs, find v2 docs, and trigger migration.
			onlyDay1, err := f.GetEnergyHistory(ctx, siteID, day1, day1.Add(24*time.Hour))
			require.NoError(t, err)
			require.Len(t, onlyDay1, 1, "should have migrated and returned Day 1")
			assert.Equal(t, day1, onlyDay1[0].TSDayStart)

			// 3. Query all again: should now have both.
			allNow, err := f.GetEnergyHistory(ctx, siteID, day1, now)
			require.NoError(t, err)
			assert.Len(t, allNow, 2)
		})
	})
}
