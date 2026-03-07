package utility

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAmeren(t *testing.T) {
	now := time.Now().In(etLocation)
	todayStr := now.Format("20060102")
	tomorrowStr := now.Add(24 * time.Hour).Format("20060102")

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

	c := configuredAmerenSmart()
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

		cError := configuredAmerenSmart()
		cError.misoAPIURL = apiError.URL

		futuresError, err := cError.GetFuturePrices(ctx)
		require.NoError(t, err)

		if now.Hour() < 23 {
			assert.True(t, len(futuresError) > 0)
			assert.InDelta(t, (float64(10+now.Hour()+1)/1000.0)*1.05009, futuresError[0].DollarsPerKWH, 0.00001)
		} else {
			assert.Equal(t, 0, len(futuresError))
		}
	})
}
