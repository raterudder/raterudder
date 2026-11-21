package utility

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type captureSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureSlogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *captureSlogHandler) WithGroup(string) slog.Handler {
	return h
}

func TestAmeren(t *testing.T) {
	t.Run("GetPeriods returns nil nil", func(t *testing.T) {
		s := &SiteFees{}
		periods, err := s.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Nil(t, periods)
	})

	now := time.Date(2026, time.January, 15, 12, 30, 0, 0, ctLocation)
	nowEST := now.In(estLocation)
	todayEST := truncateDay(nowEST)
	tomorrowEST := todayEST.AddDate(0, 0, 1)
	todayStr := todayEST.Format("20060102")
	tomorrowStr := tomorrowEST.Format("20060102")

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
		c.nowFunc = func() time.Time { return now }

		ctx := context.Background()
		// Test current price
		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		expectedTodayVal := 10 + float64(now.In(estLocation).Hour())
		assert.InDelta(t, (expectedTodayVal/1000.0)*amerenLossFactor(now), price.DollarsPerKWH, 0.00001)
		assert.Equal(t, "ameren_psp", price.Provider)
		assert.Equal(t, ctLocation, price.TSStart.Location())
		assert.Equal(t, ctLocation, price.TSEnd.Location())

		// Test future prices
		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.True(t, len(futures) > 0)

		nextHour := now.Truncate(time.Hour).Add(time.Hour)
		expectedNextHourVal := 10 + float64(nextHour.In(estLocation).Hour())
		if now.In(estLocation).Hour() == 23 {
			expectedNextHourVal = 40
		}
		if assert.NotEmpty(t, futures) {
			assert.InDelta(t, (expectedNextHourVal/1000.0)*amerenLossFactor(nextHour), futures[0].DollarsPerKWH, 0.00001)
		}

		// Test caching - changing the server won't affect it since it's cached
		api.Close()

		// Should still work due to cache
		price2, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, (expectedTodayVal/1000.0)*amerenLossFactor(now), price2.DollarsPerKWH, 0.00001)
	})

	t.Run("Concurrency singleflight", func(t *testing.T) {
		requests := 0
		doneCh := make(chan struct{})

		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			<-doneCh
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL
		c.nowFunc = func() time.Time { return now }

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
			assert.Equal(t, "ameren_psp", res.price.Provider)
		}

		assert.Equal(t, 1, requests, "expected only 1 request to the API due to singleflight")
	})

	t.Run("GetConfirmedPrices", func(t *testing.T) {
		// Use a hardcoded non-DST transition date to avoid hour count variations (23/24/25)
		// when testing with fixed 24-column mock CSV data.
		start := time.Date(2024, time.January, 15, 0, 0, 0, 0, ctLocation)
		end := start.Add(24 * time.Hour)
		startStr := start.In(estLocation).Format("20060102")
		endStr := end.In(estLocation).Format("20060102")

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
		c.nowFunc = func() time.Time { return now }

		ctx := context.Background()
		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)

		if assert.Len(t, prices, 24) {
			assert.InDelta(t, (10.0/1000.0)*amerenLossFactor(start), prices[0].DollarsPerKWH, 0.00001)
			assert.Equal(t, ctLocation, prices[0].TSStart.Location())
			assert.Equal(t, ctLocation, prices[0].TSEnd.Location())
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
		c.nowFunc = func() time.Time { return now }

		ctx := context.Background()
		_, err := c.GetCurrentPrice(ctx)
		assert.ErrorContains(t, err, "miso csv missing column for hour")
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
		cError.nowFunc = func() time.Time { return now }

		ctx := context.Background()
		futuresError, err := cError.GetFuturePrices(ctx)
		require.NoError(t, err)

		if now.In(estLocation).Hour() < 23 {
			if assert.NotEmpty(t, futuresError) {
				nextHour := now.Truncate(time.Hour).Add(time.Hour)
				assert.InDelta(t, (float64(10+nextHour.In(estLocation).Hour())/1000.0)*amerenLossFactor(nextHour), futuresError[0].DollarsPerKWH, 0.00001)
			}
		} else {
			assert.Empty(t, futuresError)
		}
	})

	t.Run("Integration_RealAPI", func(t *testing.T) {
		c := configuredAmerenSmart(nil)
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
		c.nowFunc = func() time.Time { return now }

		ctx := context.Background()

		// 1. GetCurrentPrice - DB Empty -> API -> DB Upsert
		start := time.Date(nowEST.Year(), nowEST.Month(), nowEST.Day(), 0, 0, 0, 0, estLocation).In(ctLocation)
		end := start.Add(24 * time.Hour)
		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", start, end).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren_psp", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 24
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
		start = time.Date(nowEST.Year(), nowEST.Month(), nowEST.Day(), 0, 0, 0, 0, estLocation).In(ctLocation)
		end = start.Add(24 * time.Hour)
		for i := 0; i < 24; i++ {
			fullDayPrices = append(fullDayPrices, types.PriceState{
				Price: types.Price{
					TSStart:       start.Add(time.Duration(i) * time.Hour).UTC(),
					TSEnd:         start.Add(time.Duration(i+1) * time.Hour).UTC(),
					DollarsPerKWH: 0.01 * amerenLossFactor(start.Add(time.Duration(i)*time.Hour)),
				},
				Confirmed: true,
				TSUpdated: now,
			},
			)
		}

		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", start, end).Return(fullDayPrices, nil).Once()

		price2, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, 0.01*amerenLossFactor(now), price2.DollarsPerKWH, 0.0000001)
		assert.Equal(t, ctLocation, price2.TSStart.Location())
		assert.Equal(t, ctLocation, price2.TSEnd.Location())
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
		c.nowFunc = func() time.Time { return now }

		handler := &captureSlogHandler{}
		ctx := log.With(context.Background(), slog.New(handler))

		// 1. Mock DB returns only 1 price state (partial data)
		start := time.Date(nowEST.Year(), nowEST.Month(), nowEST.Day(), 0, 0, 0, 0, estLocation).In(ctLocation)
		end := start.Add(24 * time.Hour)
		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", start, end).Return([]types.PriceState{
			{
				Price: types.Price{
					TSStart:       now.Truncate(time.Hour),
					DollarsPerKWH: 0.99, // dummy value
				},
				Confirmed: true,
				TSUpdated: now,
			},
		}, nil).Once()

		// 2. Expect fallback to API and UPSERT the full day
		m.On("UpsertUtilityPrices", mock.Anything, "ameren_psp", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 24 // expect full day
		}), 0).Return(nil).Once()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		// Should have matched price from API (10 -> 0.01 * amerenLossFactor) not from partial DB (0.99)
		assert.InDelta(t, 0.01*amerenLossFactor(now), price.DollarsPerKWH, 0.0000001)
		m.AssertExpectations(t)

		foundWarn := false
		for _, rec := range handler.records {
			assert.NotEqual(t, slog.LevelError, rec.Level, "incomplete DB cache should not log at ERROR")
			if rec.Level == slog.LevelWarn && rec.Message == "incomplete ameren prices found in database, falling back to api" {
				foundWarn = true
			}
		}
		assert.True(t, foundWarn, "expected incomplete DB cache fallback warning")
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
		c.nowFunc = func() time.Time { return now }
		ctx := context.Background()

		start := time.Date(2024, time.January, 15, 0, 0, 0, 0, ctLocation)
		end := start.Add(27 * time.Hour) // asking for 24 hours of first day + 3 hours of next day

		dateStr := start.In(estLocation).Format("20060102")

		misoStart := time.Date(2024, time.January, 15, 0, 0, 0, 0, estLocation).In(ctLocation)
		var cached []types.Price
		for i := 0; i < 24; i++ {
			cached = append(cached, types.Price{TSStart: misoStart.Add(time.Duration(i) * time.Hour), TSEnd: misoStart.Add(time.Duration(i+1) * time.Hour), DollarsPerKWH: 0.1})
		}

		// Fill cache to contain the whole first day
		c.mu.Lock()
		c.cachedPrices[dateStr] = cached
		c.mu.Unlock()

		// DB query should be for the SECOND day (the remainder in EST)
		expectedDBStart := time.Date(2024, time.January, 16, 0, 0, 0, 0, estLocation).In(ctLocation)
		expectedDBEnd := expectedDBStart.Add(24 * time.Hour)

		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", expectedDBStart, expectedDBEnd).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren_psp", mock.MatchedBy(func(p []types.PriceState) bool {
			return len(p) == 24
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(ctx, start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 27) {
			assert.Equal(t, 0.1, prices[0].DollarsPerKWH)                                                     // from cache
			assert.Equal(t, 0.1, prices[22].DollarsPerKWH)                                                    // from cache
			assert.InDelta(t, 0.01*amerenLossFactor(prices[23].TSStart), prices[23].DollarsPerKWH, 0.0000001) // from API fallback
		}

		m.AssertExpectations(t)
	})

	t.Run("Summer EDT Fixed EST Alignment", func(t *testing.T) {
		// On July 15, 2026, America/New_York is in EDT (UTC-4) and America/Chicago is in CDT (UTC-5).
		// MISO publishes in fixed EST (UTC-5), so HE 1 (00:00 - 01:00 EST) equals 00:00 - 01:00 CDT,
		// not 23:00 - 00:00 CDT of July 14.
		nowSummer := time.Date(2026, time.July, 15, 0, 30, 0, 0, ctLocation) // 05:30Z = 00:30 EST = 01:30 EDT

		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, "20260715_da_expost_lmp.csv") {
				_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
				return
			}
			if strings.HasSuffix(r.URL.Path, "20260716_da_expost_lmp.csv") {
				_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63
`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL
		c.nowFunc = func() time.Time { return nowSummer }

		ctx := context.Background()
		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, time.July, 15, 0, 0, 0, 0, ctLocation), price.TSStart)
		assert.Equal(t, time.Date(2026, time.July, 15, 1, 0, 0, 0, ctLocation), price.TSEnd)
		assert.Equal(t, ctLocation, price.TSStart.Location())
		assert.InDelta(t, (10.0/1000.0)*amerenLossFactor(nowSummer), price.DollarsPerKWH, 1e-7)

		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		if assert.Len(t, futures, 47) {
			assert.Equal(t, time.Date(2026, time.July, 15, 1, 0, 0, 0, ctLocation), futures[0].TSStart)
			assert.InDelta(t, (11.0/1000.0)*amerenLossFactor(futures[0].TSStart), futures[0].DollarsPerKWH, 1e-7)
		}

		confirmedStart := time.Date(2026, time.July, 15, 0, 0, 0, 0, ctLocation)
		confirmedEnd := time.Date(2026, time.July, 16, 0, 0, 0, 0, ctLocation)
		confirmed, err := c.GetConfirmedPrices(ctx, confirmedStart, confirmedEnd)
		require.NoError(t, err)
		if assert.Len(t, confirmed, 24) {
			for i, p := range confirmed {
				expectedStart := confirmedStart.Add(time.Duration(i) * time.Hour)
				assert.Equal(t, expectedStart, p.TSStart)
				assert.Equal(t, expectedStart.Add(time.Hour), p.TSEnd)
				assert.InDelta(t, (float64(10+i)/1000.0)*amerenLossFactor(expectedStart), p.DollarsPerKWH, 1e-7)
			}
		}
	})

	t.Run("Spring Forward 2026-03-08", func(t *testing.T) {
		// On 2026-03-08, Central Time springs forward at 02:00 CST -> 03:00 CDT (08:00Z).
		// MISO 20260308 has 24 fixed EST hours starting at 05:00Z (2026-03-07 23:00 CST).
		// HE 3 (02:00 - 03:00 EST = 07:00Z - 08:00Z) spans 01:00 CST to 03:00 CDT.
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, "20260308_da_expost_lmp.csv") {
				_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		// 07:30Z = 02:30 EST = 01:30 CST (just before spring-forward) -> HE 3 (12 $/MWh)
		nowBeforeJump := time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
		c.nowFunc = func() time.Time { return nowBeforeJump }

		ctx := context.Background()
		priceBefore, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, time.March, 8, 1, 0, 0, 0, ctLocation), priceBefore.TSStart)
		assert.Equal(t, time.Date(2026, time.March, 8, 3, 0, 0, 0, ctLocation), priceBefore.TSEnd)
		assert.Equal(t, time.Hour, priceBefore.TSEnd.Sub(priceBefore.TSStart))
		assert.InDelta(t, (12.0/1000.0)*amerenLossFactor(priceBefore.TSStart), priceBefore.DollarsPerKWH, 1e-7)

		// 08:30Z = 03:30 EST = 03:30 CDT (just after spring-forward) -> HE 4 (13 $/MWh)
		nowAfterJump := time.Date(2026, time.March, 8, 8, 30, 0, 0, time.UTC)
		c.nowFunc = func() time.Time { return nowAfterJump }

		priceAfter, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, time.March, 8, 3, 0, 0, 0, ctLocation), priceAfter.TSStart)
		assert.Equal(t, time.Date(2026, time.March, 8, 4, 0, 0, 0, ctLocation), priceAfter.TSEnd)
		assert.InDelta(t, (13.0/1000.0)*amerenLossFactor(priceAfter.TSStart), priceAfter.DollarsPerKWH, 1e-7)

		// Central Time local day [2026-03-08 00:00 CST, 2026-03-09 00:00 CDT) has 23 elapsed hours (HE 2..HE 24).
		ctDayStart := time.Date(2026, time.March, 8, 0, 0, 0, 0, ctLocation)
		ctDayEnd := time.Date(2026, time.March, 9, 0, 0, 0, 0, ctLocation)
		c.nowFunc = func() time.Time { return ctDayEnd }
		confirmed, err := c.GetConfirmedPrices(ctx, ctDayStart, ctDayEnd)
		require.NoError(t, err)
		if assert.Len(t, confirmed, 23) {
			for i, p := range confirmed {
				expectedStart := ctDayStart.Add(time.Duration(i) * time.Hour)
				assert.True(t, expectedStart.Equal(p.TSStart))
				assert.Equal(t, ctLocation, p.TSStart.Location())
				assert.InDelta(t, (float64(11+i)/1000.0)*amerenLossFactor(p.TSStart), p.DollarsPerKWH, 1e-7)
			}
		}
	})

	t.Run("Fall Back 2026-11-01T06:30:00Z", func(t *testing.T) {
		// On 2026-11-01, Central Time falls back from 01:59 CDT (UTC-5) to 01:00 CST (UTC-6) at 07:00Z.
		// At 2026-11-01T06:30:00Z (01:30 EST / 01:30 CDT), HE 2 (01:00 - 02:00 EST = 06:00Z - 07:00Z) must match.
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, "20261101_da_expost_lmp.csv") {
				_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
				return
			}
			if strings.HasSuffix(r.URL.Path, "20261102_da_expost_lmp.csv") {
				_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63
`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		c := configuredAmerenSmart(nil)
		c.misoAPIURL = api.URL

		nowFallCDT := time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC)
		c.nowFunc = func() time.Time { return nowFallCDT }

		ctx := context.Background()
		priceCDT, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.True(t, time.Date(2026, time.November, 1, 6, 0, 0, 0, time.UTC).Equal(priceCDT.TSStart))
		assert.True(t, time.Date(2026, time.November, 1, 7, 0, 0, 0, time.UTC).Equal(priceCDT.TSEnd))
		assert.Equal(t, ctLocation, priceCDT.TSStart.Location())
		assert.Equal(t, ctLocation, priceCDT.TSEnd.Location())
		assert.InDelta(t, (11.0/1000.0)*amerenLossFactor(priceCDT.TSStart), priceCDT.DollarsPerKWH, 1e-7)

		futures, err := c.GetFuturePrices(ctx)
		require.NoError(t, err)
		if assert.Len(t, futures, 46) {
			assert.True(t, time.Date(2026, time.November, 1, 7, 0, 0, 0, time.UTC).Equal(futures[0].TSStart))
			assert.InDelta(t, (12.0/1000.0)*amerenLossFactor(futures[0].TSStart), futures[0].DollarsPerKWH, 1e-7)
		}

		// Repeated 1 AM Central Time hour after fall-back (07:30Z = 02:30 EST = 01:30 CST) -> HE 3 (12 $/MWh)
		nowFallCST := time.Date(2026, time.November, 1, 7, 30, 0, 0, time.UTC)
		c.nowFunc = func() time.Time { return nowFallCST }

		priceCST, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.True(t, time.Date(2026, time.November, 1, 7, 0, 0, 0, time.UTC).Equal(priceCST.TSStart))
		assert.True(t, time.Date(2026, time.November, 1, 8, 0, 0, 0, time.UTC).Equal(priceCST.TSEnd))
		assert.InDelta(t, (12.0/1000.0)*amerenLossFactor(priceCST.TSStart), priceCST.DollarsPerKWH, 1e-7)

		// Central Time local day [2026-11-01 00:00 CDT, 2026-11-02 00:00 CST) has 25 elapsed hours.
		ctDayStart := time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation)
		ctDayEnd := time.Date(2026, time.November, 2, 0, 0, 0, 0, ctLocation)
		c.nowFunc = func() time.Time { return ctDayEnd }
		confirmed, err := c.GetConfirmedPrices(ctx, ctDayStart, ctDayEnd)
		require.NoError(t, err)
		if assert.Len(t, confirmed, 25) {
			for i, p := range confirmed {
				expectedStart := ctDayStart.Add(time.Duration(i) * time.Hour)
				assert.True(t, expectedStart.Equal(p.TSStart))
				assert.Equal(t, ctLocation, p.TSStart.Location())
			}
			// 25th hour comes from 20261102 HE 1 (40 $/MWh)
			assert.InDelta(t, (40.0/1000.0)*amerenLossFactor(confirmed[24].TSStart), confirmed[24].DollarsPerKWH, 1e-7)
		}
	})

	t.Run("DB 24-Hour Caching Across DST", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		apiRequests := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiRequests++
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
		}))
		defer api.Close()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL

		nowFall := time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC)
		c.nowFunc = func() time.Time { return nowFall }

		handler := &captureSlogHandler{}
		ctx := log.With(context.Background(), slog.New(handler))

		fallStart := time.Date(2026, time.November, 1, 0, 0, 0, 0, estLocation).In(ctLocation)
		fallEnd := fallStart.Add(24 * time.Hour)

		// 1. Legacy 23-record DB result on 2026-11-01 (missing 06:00Z) logs WARN, fetches API, and upserts 24 EST hours.
		var legacy23 []types.PriceState
		for i := 0; i < 24; i++ {
			ts := fallStart.Add(time.Duration(i) * time.Hour)
			if ts.Equal(time.Date(2026, time.November, 1, 6, 0, 0, 0, time.UTC)) {
				continue
			}
			legacy23 = append(legacy23, types.PriceState{
				Price: types.Price{
					Provider:      "ameren_psp",
					TSStart:       ts.UTC(),
					TSEnd:         ts.Add(time.Hour).UTC(),
					DollarsPerKWH: 0.99,
				},
				Confirmed: true,
				TSUpdated: nowFall,
			})
		}
		require.Len(t, legacy23, 23)

		var upserted []types.PriceState
		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", fallStart, fallEnd).Return(legacy23, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren_psp", mock.MatchedBy(func(p []types.PriceState) bool {
			upserted = p
			return len(p) == 24
		}), 0).Return(nil).Once()

		price, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, apiRequests)
		assert.InDelta(t, (11.0/1000.0)*amerenLossFactor(price.TSStart), price.DollarsPerKWH, 1e-7)
		m.AssertExpectations(t)

		foundWarn := false
		for _, rec := range handler.records {
			assert.NotEqual(t, slog.LevelError, rec.Level)
			if rec.Level == slog.LevelWarn && rec.Message == "incomplete ameren prices found in database, falling back to api" {
				foundWarn = true
			}
		}
		assert.True(t, foundWarn)

		// 2. Clear memory cache and simulate Firestore returning the 24 upserted prices with fixed/UTC offsets.
		c.mu.Lock()
		c.cachedPrices = make(map[string][]types.Price)
		c.mu.Unlock()

		fixedZone := time.FixedZone("", -5*60*60)
		db24 := make([]types.PriceState, len(upserted))
		for i, ps := range upserted {
			ps.Price.TSStart = ps.Price.TSStart.In(fixedZone)
			ps.Price.TSEnd = ps.Price.TSEnd.In(fixedZone)
			db24[i] = ps
		}

		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", fallStart, fallEnd).Return(db24, nil).Once()

		priceFromDB, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, apiRequests, "expected 24-hour DB cache hit without extra API request")
		assert.Equal(t, ctLocation, priceFromDB.TSStart.Location())
		assert.Equal(t, ctLocation, priceFromDB.TSEnd.Location())
		assert.InDelta(t, (11.0/1000.0)*amerenLossFactor(priceFromDB.TSStart), priceFromDB.DollarsPerKWH, 1e-7)
		m.AssertExpectations(t)

		// 3. Spring-forward 2026-03-08 also queries and caches all 24 fixed EST hours.
		nowSpring := time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
		c.nowFunc = func() time.Time { return nowSpring }
		springStart := time.Date(2026, time.March, 8, 0, 0, 0, 0, estLocation).In(ctLocation)
		springEnd := springStart.Add(24 * time.Hour)

		var springDB24 []types.PriceState
		for i := 0; i < 24; i++ {
			ts := springStart.Add(time.Duration(i) * time.Hour)
			springDB24 = append(springDB24, types.PriceState{
				Price: types.Price{
					Provider:      "ameren_psp",
					TSStart:       ts.UTC(),
					TSEnd:         ts.Add(time.Hour).UTC(),
					DollarsPerKWH: (float64(10+i) / 1000.0) * amerenLossFactor(ts),
				},
				Confirmed: true,
				TSUpdated: nowSpring,
			})
		}

		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", springStart, springEnd).Return(springDB24, nil).Once()

		springPriceFromDB, err := c.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, apiRequests, "expected spring-forward 24-hour DB cache hit without extra API request")
		assert.Equal(t, ctLocation, springPriceFromDB.TSStart.Location())
		assert.Equal(t, ctLocation, springPriceFromDB.TSEnd.Location())
		assert.Equal(t, time.Date(2026, time.March, 8, 1, 0, 0, 0, ctLocation), springPriceFromDB.TSStart)
		assert.Equal(t, time.Date(2026, time.March, 8, 3, 0, 0, 0, ctLocation), springPriceFromDB.TSEnd)
		assert.InDelta(t, (12.0/1000.0)*amerenLossFactor(springPriceFromDB.TSStart), springPriceFromDB.DollarsPerKWH, 1e-7)
		m.AssertExpectations(t)
	})

	t.Run("Summer DB Cache Targets ameren_psp Instead Of Legacy ameren", func(t *testing.T) {
		// Verify GetUtilityPrices and UpsertUtilityPrices target "ameren_psp" so any legacy shifted
		// summer (EDT) rows stored under "ameren" across adjacent days are completely bypassed.
		m := &storagemock.MockDatabase{}
		apiRequests := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiRequests++
			w.Header().Set("Content-Type", "text/csv")
			if strings.HasSuffix(r.URL.Path, "20260715_da_expost_lmp.csv") {
				_, _ = w.Write([]byte(`Node,Type,Value,HE 1,HE 2,HE 3,HE 4,HE 5,HE 6,HE 7,HE 8,HE 9,HE 10,HE 11,HE 12,HE 13,HE 14,HE 15,HE 16,HE 17,HE 18,HE 19,HE 20,HE 21,HE 22,HE 23,HE 24
AMIL.BGS6,Loadzone,LMP,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33
`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		// Populate legacy "ameren" mock with 24 shifted summer rows spanning [2026-07-15 05:00Z, 2026-07-16 05:00Z)
		// (HE 2..24 of 2026-07-15 + HE 1 of 2026-07-16 under the old EDT mapping) to prove it is never read.
		summerStart := time.Date(2026, time.July, 15, 0, 0, 0, 0, estLocation).In(ctLocation)
		summerEnd := summerStart.Add(24 * time.Hour)
		var shiftedLegacy24 []types.PriceState
		for i := 0; i < 24; i++ {
			ts := summerStart.Add(time.Duration(i) * time.Hour)
			shiftedLegacy24 = append(shiftedLegacy24, types.PriceState{
				Price: types.Price{
					Provider:      "ameren_psp",
					TSStart:       ts.UTC(),
					TSEnd:         ts.Add(time.Hour).UTC(),
					DollarsPerKWH: 0.99, // shifted wrong price
				},
				Confirmed: true,
				TSUpdated: summerStart,
			})
		}
		m.On("GetUtilityPrices", mock.Anything, "ameren", mock.Anything, mock.Anything).Return(shiftedLegacy24, nil).Maybe()

		var upsertedSummer []types.PriceState
		m.On("GetUtilityPrices", mock.Anything, "ameren_psp", summerStart, summerEnd).Return([]types.PriceState{}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "ameren_psp", mock.MatchedBy(func(p []types.PriceState) bool {
			upsertedSummer = p
			return len(p) == 24 &&
				p[0].TSStart.Equal(time.Date(2026, time.July, 15, 0, 0, 0, 0, ctLocation)) &&
				p[23].TSStart.Equal(time.Date(2026, time.July, 15, 23, 0, 0, 0, ctLocation))
		}), 0).Return(nil).Once()

		c := configuredAmerenSmart(m)
		c.misoAPIURL = api.URL
		nowSummer := time.Date(2026, time.July, 15, 0, 30, 0, 0, ctLocation)
		c.nowFunc = func() time.Time { return nowSummer }

		price, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, apiRequests)
		assert.InDelta(t, (10.0/1000.0)*amerenLossFactor(nowSummer), price.DollarsPerKWH, 1e-7)
		require.Len(t, upsertedSummer, 24)
		m.AssertNotCalled(t, "GetUtilityPrices", mock.Anything, "ameren", mock.Anything, mock.Anything)
		m.AssertNotCalled(t, "UpsertUtilityPrices", mock.Anything, "ameren", mock.Anything, mock.Anything)
		m.AssertExpectations(t)
	})
}

func TestAmerenBGSAndLossFactor(t *testing.T) {
	t.Run("Loss factor transitions", func(t *testing.T) {
		// Before June 1, 2026 (CST/CDT) -> 1.05009
		t1 := time.Date(2026, time.May, 31, 23, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.05009, amerenLossFactor(t1))

		// On/After June 1, 2026 -> 1.04895
		t2 := time.Date(2026, time.June, 1, 0, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.04895, amerenLossFactor(t2))

		// Later in 2026 and 2027 -> 1.04895
		t3 := time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.04895, amerenLossFactor(t3))

		t4 := time.Date(2027, time.August, 15, 12, 0, 0, 0, ctLocation)
		assert.Equal(t, 1.04895, amerenLossFactor(t4))
	})

	nonSummer2026Riders := 0.00644 + 0.00470 + 0.0013567 + (0.04572+0.0013567)*(0.0365+0.0309)
	summer2026Riders := 0.00644 + 0.00470 + 0.0013567 + (0.07811+0.0013567)*(0.0365+0.0309)
	nonSummer2027Riders := 0.00644 + 0.00470 + 0.00126 + (0.04687+0.00126)*(0.0365+0.0135)
	summer2027Riders := 0.00644 + 0.00470 + 0.00126 + (0.08009+0.00126)*(0.0365+0.0135)

	assert.InDelta(t, 0.01567, nonSummer2026Riders, 1e-5)
	assert.InDelta(t, 0.01785, summer2026Riders, 1e-5)
	assert.InDelta(t, 0.01481, nonSummer2027Riders, 1e-5)
	assert.InDelta(t, 0.01647, summer2027Riders, 1e-5)

	t.Run("BGS-1 2026-2027 monthly supply, delivery riders, and export credits", func(t *testing.T) {
		defaultSettings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_bgs",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), defaultSettings))

		t.Run("Jan-May 2026 Non-Summer monthly BGS-1 and Rider TS", func(t *testing.T) {
			expectedGridUse := 0.04572 + nonSummer2026Riders

			pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.05684+0.02629, pJan.DollarsPerKWH, 1e-6)
			assert.InDelta(t, expectedGridUse, pJan.GridUseDollarsPerKWH, 1e-6)

			pFeb, err := u.priceForTime(time.Date(2026, time.February, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.05621+0.02629, pFeb.DollarsPerKWH, 1e-6)

			pMar, err := u.priceForTime(time.Date(2026, time.March, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.05561+0.02629, pMar.DollarsPerKWH, 1e-6)

			pApr, err := u.priceForTime(time.Date(2026, time.April, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.05260+0.02629, pApr.DollarsPerKWH, 1e-6)
			assert.InDelta(t, expectedGridUse, pApr.GridUseDollarsPerKWH, 1e-6)

			pMay, err := u.priceForTime(time.Date(2026, time.May, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.06279+0.02629, pMay.DollarsPerKWH, 1e-6)
		})

		t.Run("Jun-Sep 2026 Summer monthly BGS-1 and Rider TS", func(t *testing.T) {
			expectedGridUse := 0.07811 + summer2026Riders

			pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.08506+0.02765, pJun.DollarsPerKWH, 1e-6)
			assert.InDelta(t, expectedGridUse, pJun.GridUseDollarsPerKWH, 1e-6)

			pJul, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.08466+0.02765, pJul.DollarsPerKWH, 1e-6)
			assert.InDelta(t, expectedGridUse, pJul.GridUseDollarsPerKWH, 1e-6)

			pAug, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.08417+0.02765, pAug.DollarsPerKWH, 1e-6)

			pSep, err := u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.08361+0.02765, pSep.DollarsPerKWH, 1e-6)
		})

		t.Run("Oct-Dec 2026 and 2027 carry-forward BGS-1 and delivery transitions", func(t *testing.T) {
			pOct, err := u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.07648+0.02765, pOct.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04572+nonSummer2026Riders, pOct.GridUseDollarsPerKWH, 1e-6)

			pNov, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.07938+0.02765, pNov.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04572+nonSummer2026Riders, pNov.GridUseDollarsPerKWH, 1e-6)

			pDec, err := u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.07938+0.02765, pDec.DollarsPerKWH, 1e-6)

			pSpring2027, err := u.priceForTime(time.Date(2027, time.March, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.07938+0.02765, pSpring2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04687+nonSummer2027Riders, pSpring2027.GridUseDollarsPerKWH, 1e-6)

			pSummer2027, err := u.priceForTime(time.Date(2027, time.July, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.08823+0.02765, pSummer2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.08009+summer2027Riders, pSummer2027.GridUseDollarsPerKWH, 1e-6)

			pFall2027, err := u.priceForTime(time.Date(2027, time.November, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.07938+0.02765, pFall2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04687+nonSummer2027Riders, pFall2027.GridUseDollarsPerKWH, 1e-6)
		})

		t.Run("Rider NMOS vs Pre-2025 NetMeteringCredits export valuation on BGS-1", func(t *testing.T) {
			pNov, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
			require.NoError(t, err)

			// Default Rider NMOS (NetMeteringCredits: false) credits supply + Rider TS, excluding delivery
			assert.InDelta(t, 0.07938+0.02765+0.04572+nonSummer2026Riders, pNov.ImportRateDollars(), 1e-6)
			assert.InDelta(t, 0.07938+0.02765, pNov.ExportRateDollars(defaultSettings, []types.Price{pNov}), 1e-6)

			// Pre-2025 grandfathered full net metering (NetMeteringCredits: true) credits supply + transmission + delivery
			fullNEMSettings := defaultSettings
			fullNEMSettings.UtilityRateOptions.NetMeteringCredits = true
			assert.InDelta(t, pNov.ImportRateDollars(), pNov.ExportRateDollars(fullNEMSettings, []types.Price{pNov}), 1e-6)
		})
	})

	t.Run("PSP 2026-2027 Rider RTP, Rider TS, delivery riders, and export credits", func(t *testing.T) {
		pspSettings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_psp",
		}
		s := &SiteFees{}
		require.NoError(t, s.ApplySettings(context.Background(), pspSettings))

		// SiteFees.GetPeriods must remain nil for ameren_psp because Rider TS and Rider RTP have empty Name
		periods, err := s.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Nil(t, periods)

		fees, err := getAmerenAdditionalFees(types.UtilityRateOptions{})
		require.NoError(t, err)

		applyRaw := func(ts time.Time, rawDollarsPerKWH float64) types.Price {
			p, err := types.ApplyUtilityFeesPeriods(types.Price{
				Provider:      "ameren_psp",
				TSStart:       ts,
				TSEnd:         ts.Add(time.Hour),
				DollarsPerKWH: rawDollarsPerKWH,
			}, fees)
			require.NoError(t, err)
			return p
		}

		t.Run("Jan-May 2026 Rider RTP and Rider TS", func(t *testing.T) {
			raw := 0.03000
			expectedGridUse := 0.04572 + nonSummer2026Riders

			pJan := applyRaw(time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02629+0.00745, pJan.DollarsPerKWH, 1e-6)
			assert.InDelta(t, expectedGridUse, pJan.GridUseDollarsPerKWH, 1e-6)

			pFeb := applyRaw(time.Date(2026, time.February, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02629+0.00682, pFeb.DollarsPerKWH, 1e-6)

			pMar := applyRaw(time.Date(2026, time.March, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02629+0.00622, pMar.DollarsPerKWH, 1e-6)

			pApr := applyRaw(time.Date(2026, time.April, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02629+0.00321, pApr.DollarsPerKWH, 1e-6)

			pMay := applyRaw(time.Date(2026, time.May, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02629+0.01340, pMay.DollarsPerKWH, 1e-6)
			assert.InDelta(t, expectedGridUse, pMay.GridUseDollarsPerKWH, 1e-6)
		})

		t.Run("Jun-Nov 2026 and 2027 carry-forward Rider RTP and Rider TS", func(t *testing.T) {
			raw := 0.04000

			pJun := applyRaw(time.Date(2026, time.June, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.03056, pJun.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.07811+summer2026Riders, pJun.GridUseDollarsPerKWH, 1e-6)

			pJul := applyRaw(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.03016, pJul.DollarsPerKWH, 1e-6)

			pAug := applyRaw(time.Date(2026, time.August, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.02967, pAug.DollarsPerKWH, 1e-6)

			pSep := applyRaw(time.Date(2026, time.September, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.02911, pSep.DollarsPerKWH, 1e-6)

			pOct := applyRaw(time.Date(2026, time.October, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.00591, pOct.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04572+nonSummer2026Riders, pOct.GridUseDollarsPerKWH, 1e-6)

			pNov := applyRaw(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.00881, pNov.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04572+nonSummer2026Riders, pNov.GridUseDollarsPerKWH, 1e-6)

			pSpring2027 := applyRaw(time.Date(2027, time.April, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.00881, pSpring2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04687+nonSummer2027Riders, pSpring2027.GridUseDollarsPerKWH, 1e-6)

			pSummer2027 := applyRaw(time.Date(2027, time.July, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.03373, pSummer2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.08009+summer2027Riders, pSummer2027.GridUseDollarsPerKWH, 1e-6)

			pFall2027 := applyRaw(time.Date(2027, time.November, 15, 12, 0, 0, 0, ctLocation), raw)
			assert.InDelta(t, raw+0.02765+0.00881, pFall2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.04687+nonSummer2027Riders, pFall2027.GridUseDollarsPerKWH, 1e-6)
		})

		t.Run("Rider NMOS vs Pre-2025 NetMeteringCredits export valuation on PSP", func(t *testing.T) {
			raw := 0.03500
			pNov := applyRaw(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation), raw)

			// Default Rider NMOS (NetMeteringCredits: false) credits LMP*loss + Rider TS + Rider RTP
			assert.InDelta(t, raw+0.02765+0.00881+0.04572+nonSummer2026Riders, pNov.ImportRateDollars(), 1e-6)
			assert.InDelta(t, raw+0.02765+0.00881, pNov.ExportRateDollars(pspSettings, []types.Price{pNov}), 1e-6)

			// Pre-2025 grandfathered full net metering (NetMeteringCredits: true) credits full retail
			fullNEMSettings := pspSettings
			fullNEMSettings.UtilityRateOptions.NetMeteringCredits = true
			assert.InDelta(t, pNov.ImportRateDollars(), pNov.ExportRateDollars(fullNEMSettings, []types.Price{pNov}), 1e-6)
		})
	})
}
