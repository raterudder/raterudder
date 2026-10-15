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
	t.Run("GetPeriods returns nil nil", func(t *testing.T) {
		s := &SiteFees{}
		periods, err := s.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Nil(t, periods)
	})

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

	t.Run("Concurrency singleflight", func(t *testing.T) {
		requests := 0
		doneCh := make(chan struct{})

		// The mock server will block until we signal it to continue, ensuring that
		// concurrent requests definitely overlap in time.
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			<-doneCh
			_, _ = w.Write([]byte(`[{"millisUTC":"1706227200000","price":"2.0"}]`))
		}))
		defer ts.Close()

		c := &baseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
		}

		// Run multiple concurrent requests
		numGoroutines := 5
		type result struct {
			price types.Price
			err   error
		}
		resCh := make(chan result, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				price, err := c.GetCurrentPrice(context.Background())
				resCh <- result{price: price, err: err}
			}()
		}

		// Give them a moment to start and block on the mock server
		time.Sleep(100 * time.Millisecond)

		// Close doneCh to unblock the mock server handler
		close(doneCh)

		// Collect results
		for i := 0; i < numGoroutines; i++ {
			res := <-resCh
			require.NoError(t, res.err)
			assert.Equal(t, 0.02, res.price.DollarsPerKWH)
		}

		assert.Equal(t, 1, requests, "expected only 1 request to the API due to singleflight")
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
		assert.Equal(t, ctLocation, prices[0].TSStart.Location())
		assert.Equal(t, ctLocation, prices[0].TSEnd.Location())
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
		if err != nil && strings.Contains(err.Error(), "comed api returned status: 5") {
			t.Skipf("comed API returned 5xx error: %v", err)
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
		m.On("GetUtilityPrices", mock.Anything, "comed", start, end).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 1 && p[0].Price.DollarsPerKWH == 0.02
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		assert.Len(t, prices, 1)
		m.AssertExpectations(t)

		// 2. Test GetConfirmedPrices - DB Full
		// Clear memory cache to force DB check
		c.historicalPrices = make(map[int64]types.Price)
		m.On("GetUtilityPrices", mock.Anything, "comed", start, end).Return([]types.PriceState{
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
		m.On("GetUtilityPrices", mock.Anything, "comed", start.Add(time.Hour), end2).Return([]types.PriceState{
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

		m.On("GetUtilityPrices", mock.Anything, "comed", now.Truncate(time.Hour).Add(-time.Hour), now.Truncate(time.Hour).Add(48*time.Hour)).Return([]types.PriceState{
			{Price: types.Price{TSStart: now, DollarsPerKWH: 0.0105009}, Confirmed: false, TSUpdated: time.Now()}, // Using a stable price for test
		}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.MatchedBy(func(p []types.PriceState) bool {
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
		m.On("GetUtilityPrices", mock.Anything, "comed", now.Truncate(time.Hour).Add(-time.Hour), now.Truncate(time.Hour).Add(48*time.Hour)).Return(dbPrices, nil).Once()

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
		c.recordRecentPriceStates([]types.PriceState{
			{Price: types.Price{TSStart: now.Add(-time.Hour), DollarsPerKWH: 0.10}, Confirmed: true, TSUpdated: time.Now()},
			{Price: types.Price{TSStart: now, DollarsPerKWH: 0.10}, Confirmed: false, TSUpdated: time.Now()},
		})

		// GetFuturePrices should return from memory without calling DB when recentPriceStates are also cached
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

		m.On("GetUtilityPrices", mock.Anything, "comed", expectedDBStart, nowHour.Add(48*time.Hour)).Return(dbPrices, nil).Once()
		// Since recentPriceStates for [nowHour - 1h, nowHour] were not cached, adjustFuturePrices fetches them from Firestore directly
		m.On("GetUtilityPrices", mock.Anything, "comed", nowHour.Add(-time.Hour), nowHour.Add(time.Hour)).Return([]types.PriceState{}, nil).Once()

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

	t.Run("ComEd BESH BES BEST Fees", func(t *testing.T) {
		// BESH Fees
		// Test getComEdAdditionalFees for BESH (before and after June 2026)
		opts := types.UtilityRateOptions{
			RateClass:            ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat,
			VariableDeliveryRate: false,
		}
		fees, err := getComEdAdditionalFees(opts)
		require.NoError(t, err)
		assert.NotEmpty(t, fees)

		// Find transmission services charge (PSC) for BESH Jan-May 2026
		var pscJanMay, pscJune2026, pscSept2026 *types.UtilityFeesPeriod
		var mpccJanMay, mpccJune2026 *types.UtilityFeesPeriod
		var hpeaJan, hpeaJune, hpeaAug, hpeaSept, hpeaOct *types.UtilityFeesPeriod

		for i := range fees {
			f := &fees[i]
			if strings.Contains(f.Description, "Transmission Services Charge (PSC)") {
				if f.Start.Month() == time.January {
					pscJanMay = f
				} else if f.Start.Month() == time.June {
					pscJune2026 = f
				} else if f.Start.Month() == time.September {
					pscSept2026 = f
				}
			}
			if strings.Contains(f.Description, "Miscellaneous Procurement Components Charge") {
				if f.Start.Month() == time.January {
					mpccJanMay = f
				} else if f.Start.Month() == time.June {
					mpccJune2026 = f
				}
			}
			if strings.Contains(f.Description, "Hourly Purchased Electricity Adjustment (HPEA)") {
				if f.Start.Month() == time.January {
					hpeaJan = f
				} else if f.Start.Month() == time.June {
					hpeaJune = f
				} else if f.Start.Month() == time.August {
					hpeaAug = f
				} else if f.Start.Month() == time.September {
					hpeaSept = f
				} else if f.Start.Month() == time.October {
					hpeaOct = f
				}
			}
		}

		if assert.NotNil(t, pscJanMay) {
			assert.InDelta(t, 0.01083, pscJanMay.DollarsPerKWH, 0.00001)
			assert.False(t, pscJanMay.GridAdditional)
		}
		if assert.NotNil(t, pscJune2026) {
			assert.InDelta(t, 0.01074, pscJune2026.DollarsPerKWH, 0.00001)
			assert.False(t, pscJune2026.GridAdditional)
		}
		if assert.NotNil(t, pscSept2026) {
			assert.InDelta(t, 0.01073, pscSept2026.DollarsPerKWH, 0.00001)
			assert.False(t, pscSept2026.GridAdditional)
		}
		if assert.NotNil(t, mpccJanMay) {
			assert.InDelta(t, 0.00062, mpccJanMay.DollarsPerKWH, 0.00001)
			assert.False(t, mpccJanMay.GridAdditional)
		}
		if assert.NotNil(t, mpccJune2026) {
			assert.InDelta(t, 0.00134, mpccJune2026.DollarsPerKWH, 0.00001)
			assert.False(t, mpccJune2026.GridAdditional)
		}
		if assert.NotNil(t, hpeaJan) {
			assert.InDelta(t, 0.00743, hpeaJan.DollarsPerKWH, 0.00001)
			assert.False(t, hpeaJan.GridAdditional)
		}
		if assert.NotNil(t, hpeaJune) {
			assert.InDelta(t, -0.00191, hpeaJune.DollarsPerKWH, 0.00001)
			assert.False(t, hpeaJune.GridAdditional)
		}
		if assert.NotNil(t, hpeaAug) {
			assert.InDelta(t, -0.00376, hpeaAug.DollarsPerKWH, 0.00001)
			assert.False(t, hpeaAug.GridAdditional)
		}
		if assert.NotNil(t, hpeaSept) {
			assert.InDelta(t, 0.00385, hpeaSept.DollarsPerKWH, 0.00001)
			assert.False(t, hpeaSept.GridAdditional)
		}
		if assert.NotNil(t, hpeaOct) {
			assert.InDelta(t, -0.02296, hpeaOct.DollarsPerKWH, 0.00001)
			assert.False(t, hpeaOct.GridAdditional)
		}

		// BES Fees
		besFees, err := getComEdBESFees(opts)
		require.NoError(t, err)
		assert.NotEmpty(t, besFees)

		var pscBESJanMay, pscBESJune2026, pscBESSept2026 *types.UtilityFeesPeriod
		var peaJan, peaJune, peaAug, peaSept, peaOct *types.UtilityFeesPeriod
		var pecSummerJuneAug, pecSummerSept, pecNonsummerJanMay, pecNonsummerOctMay *types.UtilityFeesPeriod

		for i := range besFees {
			f := &besFees[i]
			if strings.Contains(f.Description, "Transmission Services Charge (PSC)") {
				if f.Start.Month() == time.January {
					pscBESJanMay = f
				} else if f.Start.Month() == time.June {
					pscBESJune2026 = f
				} else if f.Start.Month() == time.September {
					pscBESSept2026 = f
				}
			}
			if strings.Contains(f.Description, "Purchased Electricity Adjustment (PEA)") {
				if f.Start.Month() == time.January {
					peaJan = f
				} else if f.Start.Month() == time.June {
					peaJune = f
				} else if f.Start.Month() == time.August {
					peaAug = f
				} else if f.Start.Month() == time.September {
					peaSept = f
				} else if f.Start.Month() == time.October {
					peaOct = f
				}
			}
			if strings.Contains(f.Description, "Electricity Supply Charge (PEC)") {
				if strings.Contains(f.Description, "Summer June-Aug") {
					pecSummerJuneAug = f
				} else if strings.Contains(f.Description, "Summer Sept") {
					pecSummerSept = f
				} else if strings.Contains(f.Description, "Nonsummer Jan-May") {
					pecNonsummerJanMay = f
				} else if strings.Contains(f.Description, "Nonsummer Oct") {
					pecNonsummerOctMay = f
				}
			}
		}

		if assert.NotNil(t, pscBESJanMay) {
			assert.InDelta(t, 0.01819, pscBESJanMay.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, pscBESJune2026) {
			assert.InDelta(t, 0.01722, pscBESJune2026.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, pscBESSept2026) {
			assert.InDelta(t, 0.01723, pscBESSept2026.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, peaJan) {
			assert.InDelta(t, 0.00357, peaJan.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, peaJune) {
			assert.InDelta(t, 0.00230, peaJune.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, peaAug) {
			assert.InDelta(t, -0.00142, peaAug.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, peaSept) {
			assert.InDelta(t, -0.00087, peaSept.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, peaOct) {
			assert.InDelta(t, -0.01551, peaOct.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, pecSummerJuneAug) {
			assert.InDelta(t, 0.08677, pecSummerJuneAug.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, pecSummerSept) {
			assert.InDelta(t, 0.08678, pecSummerSept.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, pecNonsummerJanMay) {
			assert.InDelta(t, 0.07841, pecNonsummerJanMay.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, pecNonsummerOctMay) {
			assert.InDelta(t, 0.08241, pecNonsummerOctMay.DollarsPerKWH, 0.00001)
		}

		// BEST Fees
		bestFees, err := getComEdBESTFees(opts)
		require.NoError(t, err)
		assert.NotEmpty(t, bestFees)

		var mpecBESTSummerJuneAug, mdppecBESTSummerJuneAug, epecBESTSummerJuneAug, opecBESTSummerJuneAug *types.UtilityFeesPeriod
		var mpecBESTSummerSept, mdppecBESTSummerSept, epecBESTSummerSept, opecBESTSummerSept *types.UtilityFeesPeriod
		var mpecBESTNonsummer, mdppecBESTNonsummer, epecBESTNonsummer, opecBESTNonsummer *types.UtilityFeesPeriod
		var pjmBEST, tpeaBESTJanSep, tpeaBESTOct *types.UtilityFeesPeriod

		for i := range bestFees {
			f := &bestFees[i]
			if strings.Contains(f.Description, "PJM Capacity & Transmission") {
				pjmBEST = f
			}
			if strings.Contains(f.Description, "Adjustment Factor (TPEA)") {
				if f.Start.Month() == time.January {
					tpeaBESTJanSep = f
				} else if f.Start.Month() == time.October {
					tpeaBESTOct = f
				}
			}
			if strings.Contains(f.Description, "BEST Summer Morning") {
				if strings.Contains(f.Description, "June-Aug") {
					mpecBESTSummerJuneAug = f
				} else if strings.Contains(f.Description, "Sept") {
					mpecBESTSummerSept = f
				}
			}
			if strings.Contains(f.Description, "BEST Summer Mid-Day") {
				if strings.Contains(f.Description, "June-Aug") {
					mdppecBESTSummerJuneAug = f
				} else if strings.Contains(f.Description, "Sept") {
					mdppecBESTSummerSept = f
				}
			}
			if strings.Contains(f.Description, "BEST Summer Evening") {
				if strings.Contains(f.Description, "June-Aug") {
					epecBESTSummerJuneAug = f
				} else if strings.Contains(f.Description, "Sept") {
					epecBESTSummerSept = f
				}
			}
			if strings.Contains(f.Description, "BEST Summer Overnight") {
				if strings.Contains(f.Description, "June-Aug") {
					opecBESTSummerJuneAug = f
				} else if strings.Contains(f.Description, "Sept") {
					opecBESTSummerSept = f
				}
			}
			if strings.Contains(f.Description, "BEST Nonsummer Morning") && f.Start.Month() == time.October {
				mpecBESTNonsummer = f
			}
			if strings.Contains(f.Description, "BEST Nonsummer Mid-Day") && f.Start.Month() == time.October {
				mdppecBESTNonsummer = f
			}
			if strings.Contains(f.Description, "BEST Nonsummer Evening") && f.Start.Month() == time.October {
				epecBESTNonsummer = f
			}
			if strings.Contains(f.Description, "BEST Nonsummer Overnight") && f.Start.Month() == time.October {
				opecBESTNonsummer = f
			}
		}

		if assert.NotNil(t, pjmBEST) {
			assert.InDelta(t, 0.01875, pjmBEST.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, tpeaBESTJanSep) {
			assert.InDelta(t, 0.0, tpeaBESTJanSep.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, tpeaBESTOct) {
			assert.InDelta(t, 0.00110, tpeaBESTOct.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, mpecBESTSummerJuneAug) {
			assert.InDelta(t, 0.03778, mpecBESTSummerJuneAug.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, mdppecBESTSummerJuneAug) {
			assert.InDelta(t, 0.16594, mdppecBESTSummerJuneAug.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, epecBESTSummerJuneAug) {
			assert.InDelta(t, 0.05793, epecBESTSummerJuneAug.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, opecBESTSummerJuneAug) {
			assert.InDelta(t, 0.02829, opecBESTSummerJuneAug.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, mpecBESTSummerSept) {
			assert.InDelta(t, 0.03778, mpecBESTSummerSept.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, mdppecBESTSummerSept) {
			assert.InDelta(t, 0.16596, mdppecBESTSummerSept.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, epecBESTSummerSept) {
			assert.InDelta(t, 0.05793, epecBESTSummerSept.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, opecBESTSummerSept) {
			assert.InDelta(t, 0.02830, opecBESTSummerSept.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, mpecBESTNonsummer) {
			assert.InDelta(t, 0.04656, mpecBESTNonsummer.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, mdppecBESTNonsummer) {
			assert.InDelta(t, 0.14700, mdppecBESTNonsummer.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, epecBESTNonsummer) {
			assert.InDelta(t, 0.06009, epecBESTNonsummer.DollarsPerKWH, 0.00001)
		}
		if assert.NotNil(t, opecBESTNonsummer) {
			assert.InDelta(t, 0.03394, opecBESTNonsummer.DollarsPerKWH, 0.00001)
		}
	})

	t.Run("GetCurrentPrice API Down Memory Fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		now := time.Now()
		c := &baseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
			cachedPrices: []types.Price{
				{
					Provider:      "comed_besh",
					TSStart:       now.Add(-90 * time.Minute),
					TSEnd:         now.Add(-30 * time.Minute),
					DollarsPerKWH: 0.045,
				},
			},
			lastFetchTime: now.Add(-10 * time.Minute),
		}

		price, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0.045, price.DollarsPerKWH)
	})

	t.Run("GetCurrentPrice API Down DB Fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		m := &storagemock.MockDatabase{}
		now := time.Now().In(ctLocation)

		c := configuredComEdHourly(m)
		c.apiURL = ts.URL
		c.client = ts.Client()

		dbStart := now.Add(-6 * time.Hour).Truncate(time.Hour)
		dbPrices := []types.PriceState{
			{
				Price: types.Price{
					Provider:      "comed_besh",
					TSStart:       now.Truncate(time.Hour).Add(-1 * time.Hour),
					TSEnd:         now.Truncate(time.Hour),
					DollarsPerKWH: 0.055,
				},
				Confirmed: true,
				TSUpdated: now.Add(-45 * time.Minute),
			},
		}

		dbEnd := now.Truncate(time.Hour).Add(time.Hour)
		m.On("GetUtilityPrices", mock.Anything, "comed", mock.MatchedBy(func(t time.Time) bool {
			return t.After(dbStart.Add(-5*time.Second)) && t.Before(dbStart.Add(5*time.Second))
		}), mock.MatchedBy(func(t time.Time) bool {
			return t.After(dbEnd.Add(-5*time.Second)) && t.Before(dbEnd.Add(5*time.Second))
		})).Return(dbPrices, nil).Once()

		price, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0.055, price.DollarsPerKWH)
		m.AssertExpectations(t)
	})

	t.Run("GetCurrentPrice API Down Stale Cache No Fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		now := time.Now()
		c := &baseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
			cachedPrices: []types.Price{
				{
					Provider:      "comed_besh",
					TSStart:       now.Add(-3 * time.Hour),
					TSEnd:         now.Add(-2 * time.Hour),
					DollarsPerKWH: 0.045,
				},
			},
			lastFetchTime: now.Add(-10 * time.Minute),
		}

		_, err := c.GetCurrentPrice(context.Background())
		assert.Error(t, err)
		assert.ErrorContains(t, err, "comed api returned status: 500")
	})

	t.Run("OrigEstDollarsPerKWH and SubDollarsPerKWH", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		start := time.UnixMilli(1706227200000).In(ctLocation)
		end := start.Add(time.Hour)

		// Return 5-minute entries out of chronological order to verify sorting
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := fmt.Sprintf(`[
				{"millisUTC":"%d","price":"3.5"},
				{"millisUTC":"%d","price":"2.5"}
			]`, start.Add(10*time.Minute).UnixMilli(), start.Add(5*time.Minute).UnixMilli())
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		defer ts.Close()

		c := configuredComEdHourly(m)
		c.apiURL = ts.URL
		c.client = ts.Client()

		ctx := context.Background()

		// 1. DB currently has an unconfirmed day-ahead estimate (0.042) with no SubDollarsPerKWH
		m.On("GetUtilityPrices", mock.Anything, "comed", start, end).Return([]types.PriceState{
			{
				Price:     types.Price{TSStart: start, TSEnd: end, DollarsPerKWH: 0.042},
				Confirmed: false,
				TSUpdated: start.Add(-12 * time.Hour),
			},
		}, nil).Once()

		var upserted []types.PriceState
		m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
			upserted = args.Get(2).([]types.PriceState)
		}).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		assert.InDelta(t, 0.03, prices[0].DollarsPerKWH, 0.0001)

		if assert.Len(t, upserted, 1) {
			assert.InDelta(t, 0.03, upserted[0].DollarsPerKWH, 0.0001)
			assert.InDelta(t, 0.042, upserted[0].OrigEstDollarsPerKWH, 0.0001)
			assert.Equal(t, []float64{0.025, 0.035}, upserted[0].SubDollarsPerKWH)
			assert.True(t, upserted[0].Confirmed)
		}
		m.AssertExpectations(t)

		// 2. If a fresh instance reads a PriceState that already has OrigEstDollarsPerKWH set, it preserves it
		c2 := configuredComEdHourly(m)
		c2.apiURL = ts.URL
		c2.client = ts.Client()

		m.On("GetUtilityPrices", mock.Anything, "comed", start, end).Return([]types.PriceState{
			{
				Price:                types.Price{TSStart: start, TSEnd: end, DollarsPerKWH: 0.025},
				Confirmed:            false,
				OrigEstDollarsPerKWH: 0.042,
				SubDollarsPerKWH:     []float64{0.025},
				TSUpdated:            start.Add(5 * time.Minute),
			},
		}, nil).Once()

		var upserted2 []types.PriceState
		m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
			upserted2 = args.Get(2).([]types.PriceState)
		}).Return(nil).Once()

		_, err = c2.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		if assert.Len(t, upserted2, 1) {
			assert.InDelta(t, 0.03, upserted2[0].DollarsPerKWH, 0.0001)
			assert.InDelta(t, 0.042, upserted2[0].OrigEstDollarsPerKWH, 0.0001)
			assert.Equal(t, []float64{0.025, 0.035}, upserted2[0].SubDollarsPerKWH)
		}
		m.AssertExpectations(t)
	})

	t.Run("GetCurrentPrice Intra-Hour Whole-Hour Prediction", func(t *testing.T) {
		ctx := context.Background()
		now := time.Now().In(ctLocation)
		nowHour := now.Truncate(time.Hour)
		prevHour := nowHour.Add(-2 * time.Hour)

		t.Run("Upward Spike Reverts Toward OrigEst While Storing Raw SubDollarsPerKWH", func(t *testing.T) {
			m := &storagemock.MockDatabase{}

			// ComEd API returns:
			// - A completed confirmed hour (prevHour, 2 hours ago) with 12 samples of 3.0 cents ($0.03)
			// - Current in-progress hour (nowHour) with k=2 samples: 4.0 cents ($0.04) and 16.0 cents ($0.16)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var entries []string
				for i := 0; i < 12; i++ {
					sampleTime := prevHour.Add(time.Duration(i*5) * time.Minute)
					entries = append(entries, fmt.Sprintf(`{"millisUTC":"%d","price":"3.0"}`, sampleTime.UnixMilli()))
				}
				entries = append(entries,
					fmt.Sprintf(`{"millisUTC":"%d","price":"4.0"}`, nowHour.Add(5*time.Minute).UnixMilli()),
					fmt.Sprintf(`{"millisUTC":"%d","price":"16.0"}`, nowHour.Add(10*time.Minute).UnixMilli()),
				)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[" + strings.Join(entries, ",") + "]"))
			}))
			defer ts.Close()

			c := configuredComEdHourly(m)
			c.apiURL = ts.URL
			c.client = ts.Client()

			// DB has unconfirmed day-ahead estimates for prevHour ($0.05) and nowHour ($0.04)
			m.On("GetUtilityPrices", mock.Anything, "comed", mock.Anything, mock.Anything).Return([]types.PriceState{
				{
					Price:     types.Price{TSStart: prevHour, TSEnd: prevHour.Add(time.Hour), DollarsPerKWH: 0.05},
					Confirmed: false,
					TSUpdated: nowHour.Add(-12 * time.Hour),
				},
				{
					Price:     types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: 0.04},
					Confirmed: false,
					TSUpdated: nowHour.Add(-12 * time.Hour),
				},
			}, nil).Once()

			var upserted []types.PriceState
			m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
				upserted = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

			price, err := c.GetCurrentPrice(ctx)
			require.NoError(t, err)

			// Expected whole-hour prediction for nowHour:
			// k = 2, r = 10, sumSoFar = 0.04 + 0.16 = 0.20, latestSample = 0.16, E_t = 0.04
			// Since 0.16 > 0.04, c = 3.5 -> predRem = (3.5*0.16 + 10*0.04) / 13.5 = 0.96 / 13.5 = 0.0711111...
			// predHour = (0.20 + 10 * 0.0711111...) / 12 = 0.9111111... / 12 = 0.0759259...
			expectedPred := (0.20 + 10.0*((3.5*0.16+10.0*0.04)/13.5)) / 12.0
			assert.InDelta(t, expectedPred, price.DollarsPerKWH, 0.000001)

			if assert.Len(t, upserted, 2) {
				// 1. Confirmed past hour MUST keep exact arithmetic mean ($0.03), never altered byOrigEst ($0.05)
				assert.True(t, upserted[0].Confirmed)
				assert.InDelta(t, 0.03, upserted[0].DollarsPerKWH, 0.000001)
				assert.InDelta(t, 0.05, upserted[0].OrigEstDollarsPerKWH, 0.000001)
				assert.Len(t, upserted[0].SubDollarsPerKWH, 12)

				// 2. Unconfirmed current hour stores predicted whole-hour DollarsPerKWH alongside raw SubDollarsPerKWH
				assert.False(t, upserted[1].Confirmed)
				assert.InDelta(t, expectedPred, upserted[1].DollarsPerKWH, 0.000001)
				assert.InDelta(t, 0.04, upserted[1].OrigEstDollarsPerKWH, 0.000001)
				assert.Equal(t, []float64{0.04, 0.16}, upserted[1].SubDollarsPerKWH)
			}
			m.AssertExpectations(t)

			// 3. Another Cloud Run instance reading that fresh unconfirmed DB record (< 2m old)
			// immediately gets the predicted whole-hour price without calling the ComEd API.
			c2 := configuredComEdHourly(m)
			c2.apiURL = "http://invalid-should-not-be-called"
			m.On("GetUtilityPrices", mock.Anything, "comed", mock.Anything, mock.Anything).Return(upserted, nil).Once()

			cachedPrice, err := c2.GetCurrentPrice(ctx)
			require.NoError(t, err)
			assert.InDelta(t, expectedPred, cachedPrice.DollarsPerKWH, 0.000001)
			m.AssertExpectations(t)
		})

		t.Run("Calm Below-Estimate Hour Persists And Suppresses False Alarm Peak", func(t *testing.T) {
			m := &storagemock.MockDatabase{}

			// ComEd API returns k=2 calm samples of 3.0 cents ($0.03) during a $0.12 day-ahead forecast peak
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := fmt.Sprintf(`[
					{"millisUTC":"%d","price":"3.0"},
					{"millisUTC":"%d","price":"3.0"}
				]`, nowHour.Add(5*time.Minute).UnixMilli(), nowHour.Add(10*time.Minute).UnixMilli())
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}))
			defer ts.Close()

			c := configuredComEdHourly(m)
			c.apiURL = ts.URL
			c.client = ts.Client()

			m.On("GetUtilityPrices", mock.Anything, "comed", mock.Anything, mock.Anything).Return([]types.PriceState{
				{
					Price:     types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: 0.12},
					Confirmed: false,
					TSUpdated: nowHour.Add(-12 * time.Hour),
				},
			}, nil).Once()

			var upserted []types.PriceState
			m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
				upserted = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

			price, err := c.GetCurrentPrice(ctx)
			require.NoError(t, err)

			// k = 2, r = 10, sumSoFar = 0.06, latestSample = 0.03 <= E_t (0.12) -> c = 18.0
			// predRem = (18.0*0.03 + 10.0*0.12) / 28.0 = 1.74 / 28.0 = 0.062142857...
			// predHour = (0.06 + 10.0 * 0.062142857...) / 12.0 = 0.0567857...
			expectedPred := (0.06 + 10.0*((18.0*0.03+10.0*0.12)/28.0)) / 12.0
			assert.InDelta(t, expectedPred, price.DollarsPerKWH, 0.000001)
			if assert.Len(t, upserted, 1) {
				assert.False(t, upserted[0].Confirmed)
				assert.InDelta(t, expectedPred, upserted[0].DollarsPerKWH, 0.000001)
				assert.InDelta(t, 0.12, upserted[0].OrigEstDollarsPerKWH, 0.000001)
				assert.Equal(t, []float64{0.03, 0.03}, upserted[0].SubDollarsPerKWH)
			}
			m.AssertExpectations(t)
		})

		t.Run("Sudden Mid-Hour Negative Spike Reverts Fast With c=3.5", func(t *testing.T) {
			m := &storagemock.MockDatabase{}

			// ComEd API returns k=2 samples where hour starts positive (+2.0c = $0.02)
			// and then suddenly spikes negative (-4.0c = -$0.04) under a normal $0.02 day-ahead estimate.
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := fmt.Sprintf(`[
					{"millisUTC":"%d","price":"2.0"},
					{"millisUTC":"%d","price":"-4.0"}
				]`, nowHour.Add(5*time.Minute).UnixMilli(), nowHour.Add(10*time.Minute).UnixMilli())
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}))
			defer ts.Close()

			c := configuredComEdHourly(m)
			c.apiURL = ts.URL
			c.client = ts.Client()

			m.On("GetUtilityPrices", mock.Anything, "comed", mock.Anything, mock.Anything).Return([]types.PriceState{
				{
					Price:     types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: 0.02},
					Confirmed: false,
					TSUpdated: nowHour.Add(-12 * time.Hour),
				},
			}, nil).Once()

			var upserted []types.PriceState
			m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
				upserted = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

			price, err := c.GetCurrentPrice(ctx)
			require.NoError(t, err)

			// k = 2, r = 10, sumSoFar = 0.02 + (-0.04) = -0.02
			// sub[0] = 0.02 >= 0 and latestSample = -0.04 < 0 -> sudden mid-hour negative drop uses c = 3.5, targetEst = 0.02
			// predRem = (3.5*(-0.04) + 10.0*0.02) / 13.5 = 0.06 / 13.5
			// predHour = (-0.02 + 10.0 * (0.06 / 13.5)) / 12.0
			expectedPred := (-0.02 + 10.0*((3.5*(-0.04)+10.0*0.02)/13.5)) / 12.0
			assert.InDelta(t, expectedPred, price.DollarsPerKWH, 0.000001)
			if assert.Len(t, upserted, 1) {
				assert.False(t, upserted[0].Confirmed)
				assert.InDelta(t, expectedPred, upserted[0].DollarsPerKWH, 0.000001)
				assert.Equal(t, []float64{0.02, -0.04}, upserted[0].SubDollarsPerKWH)
			}
			m.AssertExpectations(t)
		})

		t.Run("Hour Starting Negative Uses Moderate Reversion c=8.0 And Caps Target At 3 Cents", func(t *testing.T) {
			m := &storagemock.MockDatabase{}

			// ComEd API returns k=2 negative samples (-3.0c and -5.0c) while day-ahead OrigEst was an
			// inflated $0.15/kWh peak (e.g., winter polar vortex wind curtailment).
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := fmt.Sprintf(`[
					{"millisUTC":"%d","price":"-3.0"},
					{"millisUTC":"%d","price":"-5.0"}
				]`, nowHour.Add(5*time.Minute).UnixMilli(), nowHour.Add(10*time.Minute).UnixMilli())
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}))
			defer ts.Close()

			c := configuredComEdHourly(m)
			c.apiURL = ts.URL
			c.client = ts.Client()

			m.On("GetUtilityPrices", mock.Anything, "comed", mock.Anything, mock.Anything).Return([]types.PriceState{
				{
					Price:     types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: 0.15},
					Confirmed: false,
					TSUpdated: nowHour.Add(-12 * time.Hour),
				},
			}, nil).Once()

			var upserted []types.PriceState
			m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
				upserted = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

			price, err := c.GetCurrentPrice(ctx)
			require.NoError(t, err)

			// k = 2, r = 10, sumSoFar = -0.03 + (-0.05) = -0.08
			// sub[0] = -0.03 < 0 and latestSample = -0.05 < 0 -> c = 8.0, targetEst capped at min(0.15, 0.03) = 0.03
			// predRem = (8.0*(-0.05) + 10.0*0.03) / 18.0 = -0.10 / 18.0
			expectedPred := (-0.08 + 10.0*((8.0*(-0.05)+10.0*0.03)/18.0)) / 12.0
			assert.InDelta(t, expectedPred, price.DollarsPerKWH, 0.000001)
			if assert.Len(t, upserted, 1) {
				assert.False(t, upserted[0].Confirmed)
				assert.InDelta(t, expectedPred, upserted[0].DollarsPerKWH, 0.000001)
				assert.InDelta(t, 0.15, upserted[0].OrigEstDollarsPerKWH, 0.000001)
				assert.Equal(t, []float64{-0.03, -0.05}, upserted[0].SubDollarsPerKWH)
			}
			m.AssertExpectations(t)
		})
	})

	t.Run("GetFuturePrices Raw Storage and On-The-Fly Error Adjustment", func(t *testing.T) {
		ctx := context.Background()
		now := time.Now().In(ctLocation)
		nowHour := now.Truncate(time.Hour)
		prevHour := nowHour.Add(-time.Hour)

		// Helper to convert desired ComEd BESH $/kWh back to PJM total_lmp_da ($/MWh)
		hecFactor := (1.0 / 1000.0) * 1.0124 * 1.0002 * (1.0 + 0.0406)
		toLMP := func(dollarsPerKWH float64) float64 {
			return dollarsPerKWH / hecFactor
		}

		t.Run("Negative Error Persistence And Peak-To-OffPeak Ratio Scaling With Raw DB Storage", func(t *testing.T) {
			m := &storagemock.MockDatabase{}

			// PJM Day-Ahead schedule for L = 0..5:
			// L=0 (nowHour): $0.10/kWh
			// L=1 (+1h):     $0.10/kWh (same peak level -> scale = 1.0)
			// L=2 (+2h):     $0.10/kWh (same peak level -> scale = 1.0)
			// L=3 (+3h):     $0.025/kWh (off-peak level -> scale = 0.025 / 0.10 = 0.25)
			// L=4 (+4h):     $0.10/kWh (scale = 1.0)
			// L=5 (+5h):     $0.10/kWh (beyond 4h horizon -> unadjusted $0.10)
			rawEst := []float64{0.10, 0.10, 0.10, 0.025, 0.10, 0.10}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var items []string
				for i, est := range rawEst {
					tEPT := nowHour.Add(time.Duration(i) * time.Hour).In(etLocation)
					items = append(items, fmt.Sprintf(`{"datetime_beginning_ept":"%s","total_lmp_da":%.8f}`,
						tEPT.Format("2006-01-02T15:04:05"), toLMP(est)))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
			}))
			defer ts.Close()

			c := configuredComEdHourly(m)
			c.pjmAPIURL = ts.URL
			c.pjmAPIKey = "test-key"
			c.client = ts.Client()

			// DB returns:
			// - prevHour (t-1): Confirmed actual $0.04 vs OrigEst $0.10 -> e_{-1} = -0.06 -> e0Prior = 0.75 * (-0.06) = -0.045
			// - nowHour (t): Unconfirmed with k=3 samples of $0.04 vs OrigEst $0.10 -> e0Obs = -0.06, w_3 = 3/(3+3) = 0.5
			//   -> e0 = 0.5*(-0.06) + 0.5*(-0.045) = -0.0525
			curPredStored := predictWholeHourPrice(0.04, []float64{0.04, 0.04, 0.04}, 0.10)
			m.On("GetUtilityPrices", mock.Anything, "comed", prevHour, nowHour.Add(48*time.Hour)).Return([]types.PriceState{
				{
					Price:                types.Price{TSStart: prevHour, TSEnd: nowHour, DollarsPerKWH: 0.04},
					Confirmed:            true,
					OrigEstDollarsPerKWH: 0.10,
					SubDollarsPerKWH:     []float64{0.04, 0.04, 0.04, 0.04, 0.04, 0.04, 0.04, 0.04, 0.04, 0.04, 0.04, 0.04},
					TSUpdated:            now,
				},
				{
					Price:                types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: curPredStored},
					Confirmed:            false,
					OrigEstDollarsPerKWH: 0.10,
					SubDollarsPerKWH:     []float64{0.04, 0.04, 0.04},
					TSUpdated:            now,
				},
			}, nil).Once()

			var upsertedFuture []types.PriceState
			m.On("UpsertUtilityPrices", mock.Anything, "comed", mock.Anything, 0).Run(func(args mock.Arguments) {
				upsertedFuture = args.Get(2).([]types.PriceState)
			}).Return(nil).Once()

			futures, err := c.GetFuturePrices(ctx)
			require.NoError(t, err)
			require.Len(t, futures, 6)

			// 1. Verify that UpsertUtilityPrices stored the RAW unadjusted PJM prices for L=1..5
			if assert.Len(t, upsertedFuture, 5) {
				for i, u := range upsertedFuture {
					assert.InDelta(t, rawEst[i+1], u.DollarsPerKWH, 0.000001, "DB must store raw PJM price at L=%d", i+1)
					assert.False(t, u.Confirmed)
				}
			}

			// 2. Verify that c.cachedFuture also holds the RAW unadjusted PJM prices
			c.mu.Lock()
			require.Len(t, c.cachedFuture, 6)
			for i, rawP := range c.cachedFuture {
				assert.InDelta(t, rawEst[i], rawP.DollarsPerKWH, 0.000001, "memory cache must store raw PJM price at L=%d", i)
			}
			c.mu.Unlock()

			// 3. Verify that the returned slice has on-the-fly adjustments applied:
			// e0 = -0.0525, negWeights = [0.75, 0.45, 0.30, 0.20]
			e0 := -0.0525
			// L=0: matches predictWholeHourPrice for nowHour
			assert.InDelta(t, curPredStored, futures[0].DollarsPerKWH, 0.000001)
			// L=1: 0.10 + 0.75 * (-0.0525) * 1.0
			assert.InDelta(t, 0.10+0.75*e0, futures[1].DollarsPerKWH, 0.000001)
			// L=2: 0.10 + 0.45 * (-0.0525) * 1.0
			assert.InDelta(t, 0.10+0.45*e0, futures[2].DollarsPerKWH, 0.000001)
			// L=3 (off-peak $0.025, scale = 0.025 / 0.10 = 0.25): 0.025 + 0.30 * (-0.0525 * 0.25)
			assert.InDelta(t, 0.025+0.30*(e0*0.25), futures[3].DollarsPerKWH, 0.000001)
			// L=4: 0.10 + 0.20 * (-0.0525) * 1.0
			assert.InDelta(t, 0.10+0.20*e0, futures[4].DollarsPerKWH, 0.000001)
			// L=5: unadjusted raw PJM price ($0.10)
			assert.InDelta(t, 0.10, futures[5].DollarsPerKWH, 0.000001)

			m.AssertExpectations(t)
		})

		t.Run("Positive Spike Clamping At 8 Cents And Rapid Decay plus Dynamic Intra-Hour Update", func(t *testing.T) {
			m := &storagemock.MockDatabase{}
			c := configuredComEdHourly(m)
			c.pjmAPIKey = "test-key"

			// Populate c.cachedFuture with 12 raw PJM hours at $0.05/kWh
			var cachedRaw []types.Price
			for i := 0; i < 12; i++ {
				hStart := nowHour.Add(time.Duration(i) * time.Hour)
				cachedRaw = append(cachedRaw, types.Price{
					Provider:      "comed_besh",
					TSStart:       hStart,
					TSEnd:         hStart.Add(time.Hour),
					DollarsPerKWH: 0.05,
				})
			}
			c.mu.Lock()
			c.cachedFuture = cachedRaw
			c.lastFutureFetch = now
			c.mu.Unlock()

			// Case A: At top of hour (k=0 samples in nowHour), prevHour had a massive +$0.25/kWh spike
			// ($0.30 actual vs $0.05 OrigEst).
			// Clamped at +$0.08 -> e0Prior = 0.35 * 0.08 = +0.028.
			c.recordRecentPriceStates([]types.PriceState{
				{
					Price:                types.Price{TSStart: prevHour, TSEnd: nowHour, DollarsPerKWH: 0.30},
					Confirmed:            true,
					OrigEstDollarsPerKWH: 0.05,
					TSUpdated:            now,
				},
			})

			futuresA, err := c.GetFuturePrices(ctx)
			require.NoError(t, err)
			require.Len(t, futuresA, 12)

			// At k=0:
			// L=0 (nowHour): 0.05 + e0Prior (0.028) = 0.078
			// L=1 (+1h):     0.05 + 0.35 * 0.028 = 0.0598
			// L=2 (+2h):     0.05 + 0.15 * 0.028 = 0.0542
			// L=3 (+3h):     0.05 + 0.05 * 0.028 = 0.0514
			// L=4 (+4h):     0.05 + 0.00 * 0.028 = 0.0500
			assert.InDelta(t, 0.05+0.028, futuresA[0].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.05+0.35*0.028, futuresA[1].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.05+0.15*0.028, futuresA[2].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.05+0.05*0.028, futuresA[3].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.05, futuresA[4].DollarsPerKWH, 0.000001)

			// Case B: Now 3 five-minute samples arrive in nowHour where the spike has subsided
			// to $0.03/kWh (below the $0.05 estimate!).
			// Without re-fetching PJM, GetFuturePrices dynamically updates using the new state:
			// e0Obs = 0.03 - 0.05 = -0.02, w_3 = 0.5 -> e0 = 0.5*(-0.02) + 0.5*(0.028) = +0.004
			c.recordRecentPriceStates([]types.PriceState{
				{
					Price:                types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: 0.03},
					Confirmed:            false,
					OrigEstDollarsPerKWH: 0.05,
					SubDollarsPerKWH:     []float64{0.03, 0.03, 0.03},
					TSUpdated:            now,
				},
			})

			futuresB, err := c.GetFuturePrices(ctx)
			require.NoError(t, err)
			require.Len(t, futuresB, 12)

			expectedL0 := predictWholeHourPrice(0.03, []float64{0.03, 0.03, 0.03}, 0.05)
			assert.InDelta(t, expectedL0, futuresB[0].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.05+0.35*0.004, futuresB[1].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.05+0.15*0.004, futuresB[2].DollarsPerKWH, 0.000001)
		})

		t.Run("Direct Firestore Lookup When Uncached And Negative OrigEstDollarsPerKWH", func(t *testing.T) {
			// Verify that if recentPriceStates is not cached (e.g. cold Cloud Run request
			// hitting PJM directly or when cachedFuture is used after recentPriceStates is empty),
			// adjustFuturePrices queries Firestore directly for [nowHour-1h, nowHour+1h).
			// Also verify that negative OrigEstDollarsPerKWH is properly handled (since != 0 is checked).
			m := &storagemock.MockDatabase{}
			c := configuredComEdHourly(m)
			c.pjmAPIKey = "test-key"
			now := time.Now().In(ctLocation)
			nowHour := now.Truncate(time.Hour)
			prevHour := nowHour.Add(-time.Hour)

			// Seed cachedFuture directly (simulating a hit where recentPriceStates was empty)
			// with negative day-ahead estimate (-$0.01/kWh) for nowHour and L=1, and $0.02/kWh thereafter.
			var cached []types.Price
			for i := 0; i < 12; i++ {
				est := 0.02
				if i <= 1 {
					est = -0.01
				}
				cached = append(cached, types.Price{
					Provider:      "comed_besh",
					TSStart:       nowHour.Add(time.Duration(i) * time.Hour),
					TSEnd:         nowHour.Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: est,
				})
			}
			c.cachedFuture = cached
			c.lastFutureFetch = now

			// Mock Firestore returning prevHour and nowHour with negative OrigEstDollarsPerKWH (-0.01)
			// and actual prices even more negative (-0.03), so error = -0.03 - (-0.01) = -0.02.
			dbRecent := []types.PriceState{
				{
					Price:                types.Price{TSStart: prevHour, TSEnd: nowHour, DollarsPerKWH: -0.03},
					Confirmed:            true,
					OrigEstDollarsPerKWH: -0.01,
					TSUpdated:            now,
				},
				{
					Price:                types.Price{TSStart: nowHour, TSEnd: nowHour.Add(time.Hour), DollarsPerKWH: -0.03},
					Confirmed:            false,
					OrigEstDollarsPerKWH: -0.01,
					SubDollarsPerKWH:     []float64{-0.03, -0.03, -0.03},
					TSUpdated:            now,
				},
			}
			m.On("GetUtilityPrices", mock.Anything, "comed", prevHour, nowHour.Add(time.Hour)).Return(dbRecent, nil).Once()

			futures, err := c.GetFuturePrices(ctx)
			require.NoError(t, err)
			require.Len(t, futures, 12)

			// L=0 (nowHour): predictWholeHourPrice with negative OrigEst (-0.01) and negative samples (-0.03):
			// targetEst = min(-0.01, 0.03) = -0.01, c = 8.0, w = 8/(8+9) = 8/17
			// remPred = (8/17)*(-0.03) + (9/17)*(-0.01)
			expectedL0 := predictWholeHourPrice(-0.03, []float64{-0.03, -0.03, -0.03}, -0.01)
			assert.InDelta(t, expectedL0, futures[0].DollarsPerKWH, 0.000001)

			// ePrev = 0.75*(-0.03 - (-0.01)) = 0.75*(-0.02) = -0.015
			// e0Obs = -0.03 - (-0.01) = -0.02, w_3 = 3/(3+3) = 0.5
			// e0 = 0.5*(-0.02) + 0.5*(-0.015) = -0.0175
			// L=1: scale = max(-0.01, 0.015) / max(-0.01, 0.030) = 0.5 -> eEff = -0.0175 * 0.5
			// L=2: scale = max(0.02, 0.015) / max(-0.01, 0.030) = 2/3 -> eEff = -0.0175 * (2/3)
			// Since eEff < 0, negative decay weights (0.75 for L=1, 0.45 for L=2) apply:
			assert.InDelta(t, -0.01+0.75*(-0.0175*0.5), futures[1].DollarsPerKWH, 0.000001)
			assert.InDelta(t, 0.02+0.45*(-0.0175*(0.02/0.03)), futures[2].DollarsPerKWH, 0.000001)

			m.AssertExpectations(t)
		})
	})
}

func TestComEdUtilityInfo(t *testing.T) {
	info := comEdUtilityInfo()

	assert.Equal(t, "comed", info.ID)
	assert.Equal(t, "Commonwealth Edison (ComEd)", info.Name)
	require.Len(t, info.Rates, 3, "comed should have exactly 3 rates")

	// BESH rate
	rateBESH := info.Rates[0]
	assert.Equal(t, "comed_besh", rateBESH.ID)
	assert.NotEmpty(t, rateBESH.Name)
	require.Len(t, rateBESH.Options, 3, "comed_besh should have exactly 3 options")

	t.Run("BESH RateClass option", func(t *testing.T) {
		opt := rateBESH.Options[0]
		assert.Equal(t, "rateClass", opt.Field)
		assert.Equal(t, types.UtilityOptionTypeSelect, opt.Type)
		require.Len(t, opt.Choices, 4, "rateClass should have 4 choices")

		choiceValues := make([]string, len(opt.Choices))
		for i, c := range opt.Choices {
			choiceValues[i] = c.Value
		}
		assert.Contains(t, choiceValues, ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat)
		assert.Contains(t, choiceValues, ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat)
		assert.Contains(t, choiceValues, ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat)
		assert.Contains(t, choiceValues, ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat)

		assert.Equal(t, ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, opt.Default)
	})

	t.Run("BESH VariableDeliveryRate option", func(t *testing.T) {
		opt := rateBESH.Options[1]
		assert.Equal(t, "variableDeliveryRate", opt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, opt.Type)
		assert.NotEmpty(t, opt.Description)
		assert.Equal(t, false, opt.Default)
	})

	// BES rate
	rateBES := info.Rates[1]
	assert.Equal(t, "comed_bes", rateBES.ID)
	assert.NotEmpty(t, rateBES.Name)
	require.Len(t, rateBES.Options, 3, "comed_bes should have exactly 3 options")

	// BEST rate
	rateBEST := info.Rates[2]
	assert.Equal(t, "comed_best", rateBEST.ID)
	assert.NotEmpty(t, rateBEST.Name)
	require.Len(t, rateBEST.Options, 3, "comed_best should have exactly 3 options")

	t.Run("BEST VariableDeliveryRate hidden option", func(t *testing.T) {
		opt := rateBEST.Options[1]
		assert.Equal(t, "variableDeliveryRate", opt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, opt.Type)
		assert.True(t, opt.Hidden)
		assert.Equal(t, true, opt.Default)
	})
}
