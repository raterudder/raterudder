package utility

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestComEd(t *testing.T) {
	t.Run("GetCurrentPrice Parsing", func(t *testing.T) {
		// Mock server returning a sample response
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return JSON mimicking ComEd 5-min feed
			// Two entries in the same hour: 2.0 and 3.0 -> Average 2.5
			// timestamps: 1706227200000 (00:00), 1706227500000 (00:05)
			response := `[
			{"millisUTC":"1706227500000","price":"2.0"},
			{"millisUTC":"1706227800000","price":"3.0"}
		]`
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		defer ts.Close()

		c := &baseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
		}

		ctx := context.Background()
		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)

		assert.Equal(t, 0.025, price.DollarsPerKWH) // 2.5 cents = 0.025 dollars

		// Takes timestamp of the hour start
		// 1706227200000 is 2024-01-26 00:00:00 UTC
		// CT is UTC-6 (Standard) or UTC-5 (Daylight). Jan is Standard (UTC-6).
		// So 2024-01-25 18:00:00 CT.
		expectedTime := time.UnixMilli(1706227200000).In(ctLocation)
		assert.Equal(t, expectedTime, price.TSStart)
	})

	t.Run("Caching", func(t *testing.T) {
		requests := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			_, _ = w.Write([]byte(`[{"millisUTC":"1706227200000","price":"2.0"}]`))
		}))
		defer ts.Close()

		c := &baseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
		}

		// First call
		_, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, requests)

		// Second call (immediate)
		_, err = c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, requests, "expected cached response")
	})

	t.Run("GetFuturePrices No PJM", func(t *testing.T) {
		c := &baseComEdHourly{
			apiURL:           "http://example.com", // irrelevant
			client:           &http.Client{},
			historicalPrices: make(map[int64]types.Price),
		}

		prices, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Nil(t, prices)
	})

	t.Run("GetFuturePrices PJM Mock", func(t *testing.T) {
		// PJM prices are filtered to be after the current hour
		now := time.Now().In(etLocation).Truncate(time.Hour)
		time1 := now.Add(time.Hour)
		time2 := now.Add(2 * time.Hour)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/da_hrl_lmps" {
				t.Errorf("expected path /api/v1/da_hrl_lmps, got %s", r.URL.Path)
			}
			if r.Header.Get("Ocp-Apim-Subscription-Key") != "test-key" {
				t.Errorf("missing or wrong api key header")
			}

			// Valid response captured from actual API
			response := fmt.Sprintf(`[
				{
					"datetime_beginning_ept": "%s",
					"total_lmp_da": 34.999970
				},
				{
					"datetime_beginning_ept": "%s",
					"total_lmp_da": 19.775851
				}
			]`, time1.Format("2006-01-02T15:04:05"), time2.Format("2006-01-02T15:04:05"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		defer ts.Close()

		c := &baseComEdHourly{
			pjmAPIKey:        "test-key",
			pjmAPIURL:        ts.URL + "/api/v1/da_hrl_lmps", // Mock server address
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
		}

		prices, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		require.Len(t, prices, 2)

		// Verification
		// Item 1: 00:00 EPT. 34.999970 $/MWh -> 0.03499997 $/kWh
		// 0.03499997 $/kWh x (1.0124 * 1.0002 * 1.0406) -> 0.03687996331265578 $/kWh
		assert.InDelta(t, 0.03687996331265578, prices[0].DollarsPerKWH, 0.0000001)

		// Time check
		assert.Equal(t, time1.Unix(), prices[0].TSStart.Unix())
		assert.Equal(t, time1.Add(time.Hour).Unix(), prices[0].TSEnd.Unix())
	})

	t.Run("Integration Real API", func(t *testing.T) {
		c := &baseComEdHourly{
			apiURL:           "https://hourlypricing.comed.com/api?",
			client:           &http.Client{Timeout: 10 * time.Second},
			historicalPrices: make(map[int64]types.Price),
		}

		var price types.Price
		var err error

		// Implement retry loop to mitigate intermittent network errors
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			price, err = c.GetCurrentPrice(ctx)
			cancel()
			if err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
		require.NoError(t, err)

		// Basic sanity checks
		assert.False(t, price.TSStart.IsZero())
	})

	t.Run("GetConfirmedPrices", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now().UTC()

			makeEntry := func(t time.Time, price string) string {
				ms := t.UnixMilli()
				return fmt.Sprintf(`{"millisUTC":"%d","price":"%s"}`, ms, price)
			}

			var entries []string

			// 1. Valid Past Hour (2 hours ago) - 12 entries
			// 0, 5, 10, ..., 55 minutes past the hour = 12 entries
			validStart := now.Add(-2 * time.Hour).Truncate(time.Hour)
			for i := 0; i < 12; i++ {
				t := validStart.Add(time.Duration(i*5) * time.Minute)
				entries = append(entries, makeEntry(t, "2.0"))
			}

			// 2. Partial Past Hour (3 hours ago) - 10 entries
			// Missing two entries but includes the 55-min one to pass duration check
			partialStart := now.Add(-3 * time.Hour).Truncate(time.Hour)
			for i := 0; i < 9; i++ {
				t := partialStart.Add(time.Duration(i*5) * time.Minute)
				entries = append(entries, makeEntry(t, "3.0"))
			}
			entries = append(entries, makeEntry(partialStart.Add(55*time.Minute), "3.0"))

			// 3. Almost Full Past Hour (4 hours ago) - 11 entries
			// Missing one entry but includes the 55-min one to pass duration check
			almostFullStart := now.Add(-4 * time.Hour).Truncate(time.Hour)
			for i := 0; i < 10; i++ {
				t := almostFullStart.Add(time.Duration(i*5) * time.Minute)
				entries = append(entries, makeEntry(t, "5.0"))
			}
			entries = append(entries, makeEntry(almostFullStart.Add(55*time.Minute), "5.0"))

			// 4. Future Hour (1 hour ahead) - 12 entries
			// Should be ignored even if full because it's in the future
			futureStart := now.Add(1 * time.Hour).Truncate(time.Hour)
			for i := 0; i < 12; i++ {
				t := futureStart.Add(time.Duration(i*5) * time.Minute)
				entries = append(entries, makeEntry(t, "4.0"))
			}

			jsonStr := fmt.Sprintf("[%s]", strings.Join(entries, ","))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(jsonStr))
		}))
		defer ts.Close()

		c := &baseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
		}

		ctx := context.Background()
		now := time.Now()
		// Request broad range covering everything
		prices, err := c.GetConfirmedPrices(ctx, now.AddDate(0, 0, -1), now.AddDate(0, 0, 1))
		require.NoError(t, err)

		// Assertions:
		// - Future (1h ahead) should be ignored.
		// - Partial (3h ago) should be accepted (has 10).
		// - Valid (2h ago) should be accepted (has 12).
		// - Almost Full (4h ago) should be accepted (has 11).
		assert.Len(t, prices, 3)

		// Sort prices by time to make identification easier
		sort.Slice(prices, func(i, j int) bool {
			return prices[i].TSStart.Before(prices[j].TSStart)
		})

		// 4h ago (Almost Full)
		assert.InDelta(t, 0.05, prices[0].DollarsPerKWH, 0.0001)
		assert.Equal(t, now.Add(-4*time.Hour).Truncate(time.Hour).Unix(), prices[0].TSStart.Unix())

		// 3h ago (Partial)
		assert.InDelta(t, 0.03, prices[1].DollarsPerKWH, 0.0001)
		assert.Equal(t, now.Add(-3*time.Hour).Truncate(time.Hour).Unix(), prices[1].TSStart.Unix())

		// 2h ago (Valid)
		assert.InDelta(t, 0.02, prices[2].DollarsPerKWH, 0.0001)
		assert.Equal(t, now.Add(-2*time.Hour).Truncate(time.Hour).Unix(), prices[2].TSStart.Unix())
	})

	t.Run("DB Caching", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[{"millisUTC":"1706227200000","price":"2.0"}]`))
		}))
		defer ts.Close()

		c := configuredComEdHourly(m)
		c.apiURL = ts.URL
		c.client = ts.Client()

		ctx := context.Background()
		start := time.UnixMilli(1706227200000).In(ctLocation)
		end := start.Add(time.Hour)

		// 1. Test GetConfirmedPrices - DB Empty -> API -> DB Upsert
		m.On("GetUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", start, end).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 1 && p[0].Price.DollarsPerKWH == 0.02
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		assert.Len(t, prices, 1)
		m.AssertExpectations(t)

		// 2. Test GetConfirmedPrices - DB Full
		// Clear memory cache to force DB check
		c.historicalPrices = make(map[int64]types.Price)
		m.On("GetUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", start, end).Return([]types.PriceState{
			{Price: prices[0], Confirmed: true, TSUpdated: time.Now()},
		}, nil)

		prices2, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		assert.Len(t, prices2, 1)
		assert.Equal(t, prices[0].DollarsPerKWH, prices2[0].DollarsPerKWH)
		m.AssertExpectations(t)

		// 3. Test GetConfirmedPrices - Memory Cache
		// No GetUtilityPrices call expected
		prices3, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		assert.Len(t, prices3, 1)
		assert.Equal(t, prices[0].DollarsPerKWH, prices3[0].DollarsPerKWH)
		m.AssertExpectations(t)

		// 4. Test GetConfirmedPrices - Partial Memory Cache
		// 1 hour in cache, fetch 2 hours
		end2 := start.Add(2 * time.Hour)
		c.historicalPrices = map[int64]types.Price{
			start.Unix(): prices[0],
		}

		// DB mock should start from `start.Add(time.Hour)`
		m.On("GetUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", start.Add(time.Hour), end2).Return([]types.PriceState{
			{Price: types.Price{TSStart: start.Add(time.Hour), DollarsPerKWH: 0.05}, Confirmed: true, TSUpdated: time.Now()},
		}, nil)

		prices4, err := c.GetConfirmedPrices(ctx, start, end2)
		require.NoError(t, err)
		assert.Len(t, prices4, 2)
		assert.Equal(t, prices[0].DollarsPerKWH, prices4[0].DollarsPerKWH)
		assert.Equal(t, 0.05, prices4[1].DollarsPerKWH)
		m.AssertExpectations(t)
	})

	t.Run("GetFuturePrices DB Caching", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		now := time.Now().In(ctLocation)
		futureEpt := now.Add(24 * time.Hour).Format("2006-01-02T15:04:05")

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(fmt.Sprintf(`[{"datetime_beginning_ept":"%s","total_lmp_da":10.0}]`, futureEpt)))
		}))
		defer ts.Close()

		c := configuredComEdHourly(m)
		c.pjmAPIURL = ts.URL
		c.pjmAPIKey = "dummy" // Ensure logic isn't skipped
		c.client = ts.Client()

		ctx := context.Background()

		m.On("GetUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", now.Truncate(time.Hour), now.Truncate(time.Hour).Add(48*time.Hour)).Return([]types.PriceState{
			{Price: types.Price{TSStart: now, DollarsPerKWH: 0.0105009}, Confirmed: false, TSUpdated: time.Now()}, // Using a stable price for test
		}, nil).Once()
		m.On("UpsertUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) > 0
		}), 0).Return(nil).Once()

		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, futures)
		m.AssertExpectations(t)

		// 2. DB Full ( >= 11 ) -> Use DB
		// Clear memory cache to force DB check
		c.mu.Lock()
		c.cachedFuture = nil
		c.lastFutureFetch = time.Time{}
		c.mu.Unlock()

		var dbPrices []types.PriceState
		for i := 0; i < 11; i++ {
			dbPrices = append(dbPrices, types.PriceState{
				Price:     types.Price{TSStart: now.Truncate(time.Hour).Add(time.Duration(i+1) * time.Hour), DollarsPerKWH: 0.05},
				Confirmed: false,
				TSUpdated: time.Now(),
			})
		}
		m.On("GetUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", now.Truncate(time.Hour), now.Truncate(time.Hour).Add(48*time.Hour)).Return(dbPrices, nil).Once()

		futures2, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.Len(t, futures2, 11)
		assert.Equal(t, 0.05, futures2[0].DollarsPerKWH)
		m.AssertExpectations(t)
	})

	t.Run("GetFuturePrices Memory Caching", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		c := configuredComEdHourly(m)
		c.pjmAPIKey = "dummy"

		now := time.Now().In(ctLocation).Truncate(time.Hour)
		var prices []types.Price
		for i := 0; i < 11; i++ {
			prices = append(prices, types.Price{
				TSStart:       now.Add(time.Duration(i+1) * time.Hour),
				DollarsPerKWH: 0.10,
			})
		}

		c.mu.Lock()
		c.cachedFuture = prices
		c.lastFutureFetch = time.Now()
		c.mu.Unlock()

		// GetFuturePrices should return from memory without calling DB
		ctx := context.Background()
		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.Len(t, futures, 11)
		assert.Equal(t, 0.10, futures[0].DollarsPerKWH)

		// Assert no DB calls were made
		m.AssertNotCalled(t, "GetUtilityPrices")
	})

	t.Run("GetFuturePrices Hybrid Caching", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		c := configuredComEdHourly(m)
		c.pjmAPIKey = "dummy"

		// Use a fixed time for stability
		loc := ctLocation
		now := time.Now().In(loc).Truncate(time.Hour)
		nowHour := now

		// 1. Setup memory cache with 5 prices starting from now
		var cachedPrices []types.Price
		for i := 0; i < 5; i++ {
			pStart := nowHour.Add(time.Duration(i) * time.Hour)
			cachedPrices = append(cachedPrices, types.Price{
				TSStart:       pStart,
				TSEnd:         pStart.Add(time.Hour),
				DollarsPerKWH: 0.10,
			})
		}
		c.mu.Lock()
		c.cachedFuture = cachedPrices
		c.mu.Unlock()

		// 2. Expect DB query to start AFTER the cached prices (at the 6th hour)
		// latestFutureHour is nowHour + 4h, so dbStart is nowHour + 5h
		expectedDBStart := nowHour.Add(5 * time.Hour)

		// Setup DB with 6 more prices
		var dbPrices []types.PriceState
		for i := 0; i < 6; i++ {
			pStart := expectedDBStart.Add(time.Duration(i) * time.Hour)
			dbPrices = append(dbPrices, types.PriceState{
				Price: types.Price{
					TSStart:       pStart,
					TSEnd:         pStart.Add(time.Hour),
					DollarsPerKWH: 0.20,
				},
				Confirmed: false,
				TSUpdated: time.Now(),
			})
		}

		m.On("GetUtilityPrices", mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil /* Ensure a non-nil valid context is passed */ }), "comed", expectedDBStart, nowHour.Add(48*time.Hour)).Return(dbPrices, nil).Once()

		// 3. Execution should return 11 prices (5 cached + 6 DB)
		ctx := context.Background()
		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		require.Len(t, futures, 11)

		// First 5 should be 0.10 (cached)
		for i := 0; i < 5; i++ {
			assert.Equal(t, 0.10, futures[i].DollarsPerKWH, "expected cached price for index %d", i)
		}
		// Next 6 should be 0.20 (DB)
		for i := 5; i < 11; i++ {
			assert.Equal(t, 0.20, futures[i].DollarsPerKWH, "expected DB price for index %d", i)
		}

		m.AssertExpectations(t)
	})
}
