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

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComEd(t *testing.T) {
	t.Run("GetCurrentPrice_Parsing", func(t *testing.T) {
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

		c := &BaseComEdHourly{
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

		c := &BaseComEdHourly{
			apiURL:           ts.URL,
			client:           ts.Client(),
			historicalPrices: make(map[int64]types.Price),
		}

		// First call
		_, err := c.getCachedCurrentPrices(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, requests)

		// Second call (immediate)
		_, err = c.getCachedCurrentPrices(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, requests, "expected cached response")
	})

	t.Run("GetFuturePrices_NoPJM", func(t *testing.T) {
		c := &BaseComEdHourly{
			apiURL:           "http://example.com", // irrelevant
			client:           &http.Client{},
			historicalPrices: make(map[int64]types.Price),
		}

		prices, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Nil(t, prices)
	})

	t.Run("GetFuturePrices_PJM_Mock", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/da_hrl_lmps" {
				t.Errorf("expected path /api/v1/da_hrl_lmps, got %s", r.URL.Path)
			}
			if r.Header.Get("Ocp-Apim-Subscription-Key") != "test-key" {
				t.Errorf("missing or wrong api key header")
			}

			// Valid response captured from actual API
			response := `[
				{
					"datetime_beginning_ept": "2026-02-02T00:00:00",
					"total_lmp_da": 34.999970
				},
				{
					"datetime_beginning_ept": "2026-02-02T01:00:00",
					"total_lmp_da": 19.775851
				}
			]`
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		defer ts.Close()

		c := &BaseComEdHourly{
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
		// 0.03499997 $/kWh x (1.0124 * 1.0002 * 1.047) -> 0.0371067860737561 $/kWh
		assert.InDelta(t, 0.0371067860737561, prices[0].DollarsPerKWH, 0.0000001)

		// Time check
		// 2026-02-02 00:00:00 EPT (America/New_York)
		loc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		expectedTime := time.Date(2026, 2, 2, 0, 0, 0, 0, loc)
		assert.Equal(t, expectedTime, prices[0].TSStart)
		expectedTime = time.Date(2026, 2, 2, 1, 0, 0, 0, loc)
		assert.Equal(t, expectedTime, prices[0].TSEnd)
	})

	t.Run("Integration_RealAPI", func(t *testing.T) {
		c := &BaseComEdHourly{
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
		assert.NotZero(t, price.DollarsPerKWH)
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

		c := &BaseComEdHourly{
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
		// - Partial (3h ago) should be ignored because < 11 entries (has 10).
		// - Valid (2h ago) should be accepted (has 12).
		// - Almost Full (4h ago) should be accepted (has 11).
		assert.Len(t, prices, 2)

		// Sort prices by time to make identification easier
		sort.Slice(prices, func(i, j int) bool {
			return prices[i].TSStart.Before(prices[j].TSStart)
		})

		// 4h ago (Almost Full)
		assert.InDelta(t, 0.05, prices[0].DollarsPerKWH, 0.0001)
		assert.Equal(t, now.Add(-4*time.Hour).Truncate(time.Hour).Unix(), prices[0].TSStart.Unix())

		// 2h ago (Valid)
		assert.InDelta(t, 0.02, prices[1].DollarsPerKWH, 0.0001)
		assert.Equal(t, now.Add(-2*time.Hour).Truncate(time.Hour).Unix(), prices[1].TSStart.Unix())
	})
}
