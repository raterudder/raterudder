package utility

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAmeren(t *testing.T) {
	now := time.Now().In(etLocation)
	todayStr := now.Format("20060102")
	tomorrowStr := now.Add(24 * time.Hour).Format("20060102")

	t.Run("GetCurrentPrice_And_Futures", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, todayStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
IGNORE
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			} else if strings.HasSuffix(r.URL.Path, tomorrowStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63
IGNORE
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		ctx := context.Background()
		// Test current price
		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		expectedTodayVal := 10 + float64(now.Hour())
		assert.InDelta(t, (expectedTodayVal/1000.0)*1.05009, price.DollarsPerKWH, 0.00001)
		assert.Equal(t, "ameren_psp", price.Provider)

		// Test future prices
		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.True(t, len(futures) > 0)

		// Next hour price should be from futures[0]
		// If current hour is 23, the next hour will be tomorrow's first hour (40)
		expectedNextHourVal := expectedTodayVal + 1
		if now.Hour() == 23 {
			expectedNextHourVal = 40
		}
		assert.InDelta(t, (expectedNextHourVal/1000.0)*1.05009, futures[0].DollarsPerKWH, 0.00001)

		// Test caching - changing the server won't affect it since it's cached
		api.Close()

		// Should still work due to cache
		price2, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, (expectedTodayVal/1000.0)*1.05009, price2.DollarsPerKWH, 0.00001)
	})

	t.Run("GetConfirmedPrices", func(t *testing.T) {
		// Use a hardcoded non-DST transition date to avoid hour count variations (23/24/25)
		// when testing with fixed 24-column mock CSV data.
		start := time.Date(2024, time.January, 15, 0, 0, 0, 0, etLocation)
		end := start.Add(24 * time.Hour)
		startStr := start.Format("20060102")
		endStr := end.Format("20060102")

		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, startStr+"_da_expost_lmp.csv") || strings.HasSuffix(r.URL.Path, endStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		ctx := context.Background()
		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)

		// 24 hours in the day (might be 23 or 25 depending on DST transition, but we'll accept the returned length)
		assert.True(t, len(prices) >= 23 && len(prices) <= 25)
		if len(prices) > 0 {
			assert.InDelta(t, (10.0/1000.0)*1.05009, prices[0].DollarsPerKWH, 0.00001)
		}
	})

	t.Run("ParsingErrors", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			// Return bad CSV
			_, _ = w.Write([]byte(`Node,Type,Value
AMIL.BGS6,Loadzone,LMP
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		ctx := context.Background()
		_, err := c.GetCurrentPrice(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "miso csv missing column for hour")
	})

	t.Run("ignores tomorrow errors", func(t *testing.T) {
		apiError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, todayStr+"_da_expost_lmp.csv") {
				_, err := w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer apiError.Close()

		cError := configuredAmerenSmart(nil)
		cError.misoAPIURL = apiError.URL

		ctx := context.Background()
		futuresError, err := cError.GetFuturePrices(ctx)
		require.NoError(t, err)

		if now.Hour() < 23 {
			assert.True(t, len(futuresError) > 0)
			assert.InDelta(t, (float64(10+now.Hour()+1)/1000.0)*1.05009, futuresError[0].DollarsPerKWH, 0.00001)
		} else {
			assert.Equal(t, 0, len(futuresError))
		}
	})

	t.Run("Integration_RealAPI", func(t *testing.T) {
		c := configuredAmerenSmart(nil)
		// misoAPIURL is set by default in configuredAmerenSmart via lflags, but let's ensure it's explicitly set.
		c.misoAPIURL = "https://docs.misoenergy.org/marketreports"

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)

		// Basic sanity checks
		assert.NotZero(t, price.DollarsPerKWH)
		assert.False(t, price.TSStart.IsZero())
	})

	t.Run("DB Caching", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL

		ctx := context.Background()

		// 1. GetCurrentPrice - DB Empty -> API -> DB Upsert
		start := truncateDay(time.Now().In(etLocation))
		end := start.AddDate(0, 0, 1)
		m.On("GetUtilityPrices", mock.Anything, "ameren", start, end).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) >= 23
		}), 0).Return(nil).Once()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.NotZero(t, price.DollarsPerKWH)
		m.AssertExpectations(t)

		// 2. GetCurrentPrice - DB Full
		// Clear memory cache to force DB check
		c.mu.Lock()
		c.cachedPrices = make(map[string][]types.Price)
		c.mu.Unlock()

		var fullDayPrices []types.PriceState
		start = truncateDay(time.Now().In(etLocation))
		for i := 0; i < 24; i++ {
			fullDayPrices = append(fullDayPrices, types.PriceState{
				Price: types.Price{
					TSStart:       start.Add(time.Duration(i) * time.Hour),
					TSEnd:         start.Add(time.Duration(i+1) * time.Hour),
					DollarsPerKWH: 0.0105009,
				},
				Confirmed: true,
				TSUpdated: time.Now(),
			},
			)
		}

		m.On("GetUtilityPrices", mock.Anything, "ameren", start, end).Return(fullDayPrices, nil).Once()

		price2, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, 0.0105009, price2.DollarsPerKWH, 0.0000001)
		m.AssertExpectations(t)
	})

	t.Run("DB Partial Fallback", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL

		ctx := context.Background()

		// 1. Mock DB returns only 1 price state (partial data)
		start := truncateDay(time.Now().In(etLocation))
		end := start.AddDate(0, 0, 1)
		m.On("GetUtilityPrices", mock.Anything, "ameren", start, end).Return([]types.PriceState{
			{
				Price: types.Price{
					TSStart:       time.Now().In(etLocation).Truncate(time.Hour),
					DollarsPerKWH: 0.99, // dummy value
				},
				Confirmed: true,
				TSUpdated: time.Now(),
			},
		}, nil).Once()

		// 2. Expect fallback to API and UPSERT the full day
		m.On("UpsertUtilityPrices", mock.Anything, "ameren", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) >= 23 // expect full day
		}), 0).Return(nil).Once()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		// Should have matched price from API (10 -> 0.0105009) not from partial DB (0.99)
		assert.InDelta(t, 0.0105009, price.DollarsPerKWH, 0.0000001)
		m.AssertExpectations(t)
	})

	t.Run("GetConfirmedPrices Partial Memory Cache", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10,10
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL
		ctx := context.Background()

		start := time.Date(2024, time.January, 15, 0, 0, 0, 0, etLocation)
		end := start.Add(27 * time.Hour) // asking for 24 hours of first day + 3 hours of next day

		dateStr := start.Format("20060102")

		var cached []types.Price
		for i := 0; i < 24; i++ {
			cached = append(cached, types.Price{TSStart: start.Add(time.Duration(i) * time.Hour), TSEnd: start.Add(time.Duration(i+1) * time.Hour), DollarsPerKWH: 0.1})
		}

		// Fill cache to contain the whole first day
		c.mu.Lock()
		c.cachedPrices[dateStr] = cached
		c.mu.Unlock()

		// DB query should be for the SECOND day (the remainder)
		expectedCurr := start.Add(24 * time.Hour)
		expectedDBStart := truncateDay(expectedCurr)
		expectedDBEnd := expectedDBStart.AddDate(0, 0, 1)

		m.On("GetUtilityPrices", mock.Anything, "ameren", expectedDBStart, expectedDBEnd).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) >= 23
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		assert.Len(t, prices, 27)
		assert.Equal(t, 0.1, prices[0].DollarsPerKWH)                     // from cache
		assert.Equal(t, 0.1, prices[23].DollarsPerKWH)                    // from cache
		assert.InDelta(t, 0.0105009, prices[24].DollarsPerKWH, 0.0000001) // from API fallback

		m.AssertExpectations(t)
	})
}
