package utility

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestParseVPPHistory(t *testing.T) {
	t.Run("Parse valid VPP history HTML", func(t *testing.T) {
		sampleHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.12700</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/29/2026</td>
  <td>0.08540</td>
  <td>0.08860</td>
  <td>0.12040</td>
  <td>0.08330</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(sampleHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		if assert.Len(t, rates, 2) {
			assert.Equal(t, time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation), rates[0].date)
			assert.InDelta(t, 0.12700, rates[0].onPeakPrice, 1e-6)
			assert.InDelta(t, 0.08330, rates[0].offPeakPrice, 1e-6)

			assert.Equal(t, time.Date(2026, time.June, 29, 0, 0, 0, 0, etLocation), rates[1].date)
			assert.InDelta(t, 0.12040, rates[1].onPeakPrice, 1e-6)
			assert.InDelta(t, 0.08330, rates[1].offPeakPrice, 1e-6)
		}
	})

	t.Run("Short-circuits and filters by date range", func(t *testing.T) {
		sampleHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.12700</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/29/2026</td>
  <td>0.12040</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/28/2026</td>
  <td>0.11000</td>
  <td>0.08000</td>
</tr>
</tbody>
</table>
</body>
</html>`

		// Filter for June 29 only (targetStart = 06/29/2026, targetEnd = 06/29/2026 23:59:59)
		start := time.Date(2026, time.June, 29, 0, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 29, 23, 59, 59, 0, etLocation)
		rates, err := parseVPPHistory(context.Background(), strings.NewReader(sampleHTML), start, end)
		require.NoError(t, err)
		if assert.Len(t, rates, 1) {
			assert.Equal(t, start, rates[0].date)
			assert.InDelta(t, 0.12040, rates[0].onPeakPrice, 1e-6)
		}
	})

	t.Run("Returns nil when Residential header is missing", func(t *testing.T) {
		missingResHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(missingResHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, rates)
	})

	t.Run("Returns nil when On-Peak or Off-Peak sub-header is missing", func(t *testing.T) {
		missingSubPeakHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>Mid-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.12700</td>
  <td>0.08330</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(missingSubPeakHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, rates)
	})

	t.Run("Skips non-overlapping month tables and streams multi-megabyte HTML", func(t *testing.T) {
		var sb strings.Builder
		sb.Grow(2_200_000)
		sb.WriteString(`<html><body><table><tr><td>`)

		for month := time.January; month <= time.December; month++ {
			sb.WriteString(`<table class="table"><thead><tr><th>Date</th><th colspan="2">Last Resort Service</th><th colspan="2">Residential</th></tr>`)
			sb.WriteString(`<tr><td>&nbsp;</td><td>On-Peak</td><td>Off-Peak</td><td>On-Peak</td><td>Off-Peak</td></tr></thead><tbody>`)
			// Write ~1,800 historical rows per month table (~2.1 MB total across 12 tables)
			for i := 0; i < 1800; i++ {
				year := 2026 - (i / 20)
				day := (i % 20) + 1
				fmt.Fprintf(&sb, "<tr><td>%02d/%02d/%04d</td><td>0.09301</td><td>0.08860</td><td>0.15000</td><td>0.08330</td></tr>\n", int(month), day, year)
			}
			sb.WriteString(`</tbody></table>`)
		}
		sb.WriteString(`</td></tr></table></body></html>`)
		largeHTML := sb.String()
		require.Greater(t, len(largeHTML), 2_000_000)

		start := time.Date(2026, time.June, 18, 0, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 20, 0, 0, 0, 0, etLocation)

		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(largeHTML), start, end)
		require.NoError(t, err)

		runtime.ReadMemStats(&after)
		allocBytes := after.TotalAlloc - before.TotalAlloc
		// Streaming tokenizer skipping 11 non-overlapping months stays far below html.Parse (~16.24 MiB),
		// including when Go's -race detector allocates a 4 MiB instrumentation slab.
		assert.Less(t, allocBytes, uint64(6*1024*1024))

		if assert.Len(t, rates, 3) {
			assert.Equal(t, time.Date(2026, time.June, 18, 0, 0, 0, 0, etLocation), rates[0].date)
			assert.Equal(t, time.Date(2026, time.June, 19, 0, 0, 0, 0, etLocation), rates[1].date)
			assert.Equal(t, time.Date(2026, time.June, 20, 0, 0, 0, 0, etLocation), rates[2].date)
		}
	})

	t.Run("Logs warnings on malformed cells and preserves explicit weekend rows", func(t *testing.T) {
		mixedHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>not-a-date</td>
  <td>0.15000</td>
  <td>0.08000</td>
</tr>
<tr>
  <td>06/29/2026</td>
  <td>0.14500</td>
</tr>
<tr>
  <td>06/28/2026</td>
  <td>N/A</td>
  <td>0.08000</td>
</tr>
<tr>
  <td>06/27/2026</td>
  <td>0.09123</td>
  <td>0.09123</td>
</tr>
<tr>
  <td>06/26/2026</td>
  <td>0.15000</td>
  <td>0.08000</td>
</tr>
</tbody>
</table>
</body>
</html>`

		rates, err := parseVPPHistory(context.Background(), strings.NewReader(mixedHTML), time.Time{}, time.Time{})
		require.NoError(t, err)
		if assert.Len(t, rates, 2) {
			assert.Equal(t, time.Date(2026, time.June, 27, 0, 0, 0, 0, etLocation), rates[0].date)
			assert.InDelta(t, 0.09123, rates[0].offPeakPrice, 1e-6)
			assert.Equal(t, time.Date(2026, time.June, 26, 0, 0, 0, 0, etLocation), rates[1].date)
			assert.InDelta(t, 0.15000, rates[1].onPeakPrice, 1e-6)
			assert.InDelta(t, 0.08000, rates[1].offPeakPrice, 1e-6)
		}
	})
}

func TestBaseEversourceVPP(t *testing.T) {
	sampleHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/30/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.15000</td>
  <td>0.08000</td>
</tr>
<tr>
  <td>07/01/2026</td>
  <td>0.09500</td>
  <td>0.08900</td>
  <td>0.16000</td>
  <td>0.08500</td>
</tr>
</tbody>
</table>
</body>
</html>`

	base := &baseEversourceVPP{
		vppHistoryURL: "",
		client:        common.HTTPClient(10 * time.Second),
		cachedPrices:  make(map[string][]types.Price),
	}

	// Pre-populate parsed rates in memory cache for test date
	rates, err := parseVPPHistory(context.Background(), strings.NewReader(sampleHTML), time.Time{}, time.Time{})
	require.NoError(t, err)
	for _, r := range rates {
		dateStr := r.date.Format("2006-01-02")
		base.cachedPrices[dateStr] = dailyRateToHourlyPrices(r)
	}

	t.Run("Hourly prices for weekday contain on-peak and off-peak periods", func(t *testing.T) {
		date := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation) // Tuesday
		prices, err := base.getPricesForDate(context.Background(), date)
		require.NoError(t, err)
		if assert.Len(t, prices, 24) {
			// Hour 14 (2 PM) is On-Peak (12-20)
			assert.InDelta(t, 0.15000, prices[14].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Eversource CT VPP On-Peak", prices[14].PeriodName)

			// Hour 8 (8 AM) is Off-Peak
			assert.InDelta(t, 0.08000, prices[8].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Eversource CT VPP Off-Peak", prices[8].PeriodName)
		}
	})

	t.Run("SiteFees applies delivery fee and generation adjustment to eversource_ct_vpp", func(t *testing.T) {
		siteFees := &SiteFees{
			base:   base,
			siteID: "test-site",
		}
		err := siteFees.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_vpp",
		})
		require.NoError(t, err)

		// Check June 30, 2026 On-Peak (Delivery 0.12228 - FMCC 0.00150 = 0.12078)
		start := time.Date(2026, time.June, 30, 14, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 30, 15, 0, 0, 0, etLocation)
		prices, err := siteFees.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			// Base supply (0.15000) + Delivery&FMCC (0.12078) = 0.27078
			assert.InDelta(t, 0.27078, prices[0].DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, prices[0].GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Check July 1, 2026 On-Peak (Delivery 0.15853 - FMCC 0.00210 = 0.15643)
		startJulOn := time.Date(2026, time.July, 1, 14, 0, 0, 0, etLocation)
		endJulOn := time.Date(2026, time.July, 1, 15, 0, 0, 0, etLocation)
		pricesJulOn, err := siteFees.GetConfirmedPrices(context.Background(), startJulOn, endJulOn)
		require.NoError(t, err)
		if assert.Len(t, pricesJulOn, 1) {
			// Base supply (0.16000) + On-Peak Delivery&FMCC (0.15643) = 0.31643
			assert.InDelta(t, 0.31643, pricesJulOn[0].DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pricesJulOn[0].GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", pricesJulOn[0].PeriodName)
		}

		// Check July 1, 2026 Off-Peak (Delivery 0.10125 - FMCC 0.00210 = 0.09915)
		startJulOff := time.Date(2026, time.July, 1, 8, 0, 0, 0, etLocation)
		endJulOff := time.Date(2026, time.July, 1, 9, 0, 0, 0, etLocation)
		pricesJulOff, err := siteFees.GetConfirmedPrices(context.Background(), startJulOff, endJulOff)
		require.NoError(t, err)
		if assert.Len(t, pricesJulOff, 1) {
			// Base supply (0.08500) + Off-Peak Delivery&FMCC (0.09915) = 0.18415
			assert.InDelta(t, 0.18415, pricesJulOff[0].DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pricesJulOff[0].GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pricesJulOff[0].PeriodName)
		}

		// Verify SiteFees.GetPeriods exposes named On-Peak and Off-Peak TOU periods
		periods, err := siteFees.GetPeriods(context.Background())
		require.NoError(t, err)
		periodNames := make(map[string]bool)
		for _, p := range periods {
			periodNames[p.Name] = true
		}
		assert.True(t, periodNames["On-Peak"])
		assert.True(t, periodNames["Off-Peak"])

		// Check with 2025 application year (adjustment -$0.0050/kWh)
		siteFees2025 := &SiteFees{
			base:   base,
			siteID: "test-site",
		}
		err = siteFees2025.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_vpp",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "2025",
			},
		})
		require.NoError(t, err)
		prices2025, err := siteFees2025.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices2025, 1) {
			assert.InDelta(t, 0.27078, prices2025[0].DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0050, prices2025[0].GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Memory Cache hit avoids DB query and HTTP fetch", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: "http://invalid-url-should-not-be-called",
			client:        common.HTTPClient(time.Second),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
		}

		testDate := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation)
		dateStr := testDate.Format("2006-01-02")
		c.cachedPrices[dateStr] = []types.Price{
			{
				TSStart:       time.Date(2026, time.June, 30, 12, 0, 0, 0, etLocation),
				TSEnd:         time.Date(2026, time.June, 30, 13, 0, 0, 0, etLocation),
				DollarsPerKWH: 0.15,
				PeriodName:    "Eversource CT VPP On-Peak",
			},
		}

		start := time.Date(2026, time.June, 30, 12, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 30, 13, 0, 0, 0, etLocation)
		prices, err := c.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			assert.InDelta(t, 0.15, prices[0].DollarsPerKWH, 1e-6)
		}
		m.AssertExpectations(t)
	})

	t.Run("Memory Cache miss falls back to DB if complete", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: "http://invalid-url-should-not-be-called",
			client:        common.HTTPClient(time.Second),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
		}

		start := time.Date(2026, time.June, 30, 12, 0, 0, 0, etLocation)
		end := time.Date(2026, time.June, 30, 13, 0, 0, 0, etLocation)
		dayStart := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation)
		dayEnd := time.Date(2026, time.July, 1, 0, 0, 0, 0, etLocation)

		dbState := []types.PriceState{
			{
				Price: types.Price{
					TSStart:       start,
					TSEnd:         end,
					DollarsPerKWH: 0.15,
					PeriodName:    "Eversource CT VPP On-Peak",
				},
				Confirmed: true,
			},
		}

		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", dayStart, dayEnd).Return(dbState, nil).Once()

		prices, err := c.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		if assert.Len(t, prices, 1) {
			assert.InDelta(t, 0.15, prices[0].DollarsPerKWH, 1e-6)
		}
		// Verify cached in memory
		assert.Len(t, c.cachedPrices["2026-06-30"], 1)
		m.AssertExpectations(t)
	})

	t.Run("Memory Cache miss and DB miss falls back to HTTP and limits upserts", func(t *testing.T) {
		now := time.Now().In(etLocation)
		todayStr := now.Format("01/02/2006")
		dynamicHTML := fmt.Sprintf(`
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>%s</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.15000</td>
  <td>0.08000</td>
</tr>
</tbody>
</table>
</body>
</html>`, todayStr)

		httpCalled := false
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpCalled = true
			_, _ = w.Write([]byte(dynamicHTML))
		}))
		defer ts.Close()

		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: ts.URL,
			client:        ts.Client(),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
		}

		start := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, etLocation)
		end := time.Date(now.Year(), now.Month(), now.Day(), 13, 0, 0, 0, etLocation)
		dayStart := truncateDay(now)
		dayEnd := dayStart.AddDate(0, 0, 1)

		// 1. Initial range lookup in DB returns empty (DB miss)
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", dayStart, dayEnd).Return([]types.PriceState{}, nil).Once()

		// 2. Lookup for recent history in fetchHistoryAndCache returns existing DB prices (so past 30 days are ignored)
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.Anything, mock.Anything).Return([]types.PriceState{
			{Confirmed: true},
		}, nil).Once()

		// 3. Upsert is called with filtered recent prices (NOT all 30 days / 720 items)
		m.On("UpsertUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.MatchedBy(func(toUpsert []types.PriceState) bool {
			// Must upsert recent prices only (e.g. <= 96 items for 3-4 days, not full 720)
			return len(toUpsert) > 0 && len(toUpsert) <= 96
		}), 0).Return(nil).Once()

		prices, err := c.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		assert.True(t, httpCalled, "HTTP server should be called on DB miss")
		expectedPrice := 0.08
		if now.Weekday() >= time.Monday && now.Weekday() <= time.Friday {
			expectedPrice = 0.15
		}
		if assert.Len(t, prices, 1) {
			assert.InDelta(t, expectedPrice, prices[0].DollarsPerKWH, 1e-6)
		}
		m.AssertExpectations(t)
	})

	t.Run("Synthesizes weekend Off-Peak prices across GetCurrentPrice GetFuturePrices and GetConfirmedPrices", func(t *testing.T) {
		// Real vpphistory.aspx omits Saturday (06/27/2026) and Sunday (06/28/2026) rows
		weekdayOnlyHTML := `
<html>
<body>
<table class="table">
<thead>
<tr>
  <th>Date</th>
  <th colspan="2">Last Resort Service</th>
  <th colspan="2">Residential</th>
</tr>
<tr>
  <td>&nbsp;</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
  <td>On-Peak</td>
  <td>Off-Peak</td>
</tr>
</thead>
<tbody>
<tr>
  <td>06/29/2026</td>
  <td>0.09100</td>
  <td>0.08860</td>
  <td>0.14500</td>
  <td>0.08330</td>
</tr>
<tr>
  <td>06/26/2026</td>
  <td>0.09301</td>
  <td>0.08860</td>
  <td>0.15000</td>
  <td>0.08330</td>
</tr>
</tbody>
</table>
</body>
</html>`

		var httpCalls atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpCalls.Add(1)
			_, _ = w.Write([]byte(weekdayOnlyHTML))
		}))
		defer ts.Close()

		satAfternoon := time.Date(2026, time.June, 27, 14, 30, 0, 0, etLocation) // Saturday 2:30 PM ET
		currentNow := satAfternoon

		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: ts.URL,
			client:        ts.Client(),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
			nowFunc:       func() time.Time { return currentNow },
		}

		satStart := time.Date(2026, time.June, 27, 0, 0, 0, 0, etLocation)
		sunStart := time.Date(2026, time.June, 28, 0, 0, 0, 0, etLocation)
		monStart := time.Date(2026, time.June, 29, 0, 0, 0, 0, etLocation)

		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", satStart, sunStart).Return([]types.PriceState{}, nil).Once()
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", satStart.AddDate(0, 0, -3), satAfternoon).Return([]types.PriceState{{Confirmed: true}}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.Anything, 0).Return(nil).Once()

		// 1. Saturday GetCurrentPrice during weekday peak window (14:30 ET) returns synthesized Off-Peak rate
		curPrice, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.InDelta(t, 0.08330, curPrice.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Eversource CT VPP Off-Peak", curPrice.PeriodName)
		assert.Equal(t, int32(1), httpCalls.Load())

		// 2. Saturday GetFuturePrices covers rest of Saturday (14:00..24:00 = 10h) + all 24h of Sunday = 34h from cache
		satFuture, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		if assert.Len(t, satFuture, 34) {
			for _, p := range satFuture {
				assert.InDelta(t, 0.08330, p.DollarsPerKWH, 1e-6)
				assert.Equal(t, "Eversource CT VPP Off-Peak", p.PeriodName)
			}
		}
		assert.Equal(t, int32(1), httpCalls.Load())

		// 3. GetConfirmedPrices across full weekend [Saturday 00:00, Monday 00:00) hits cache with 48 Off-Peak hours
		weekendConfirmed, err := c.GetConfirmedPrices(context.Background(), satStart, monStart)
		require.NoError(t, err)
		if assert.Len(t, weekendConfirmed, 48) {
			assert.Equal(t, satStart, weekendConfirmed[0].TSStart)
			assert.Equal(t, monStart, weekendConfirmed[47].TSEnd)
		}
		assert.Equal(t, int32(1), httpCalls.Load())

		// 4. Advance clock to Sunday 15:00 ET: GetCurrentPrice and GetFuturePrices (Sun + Mon) hit cache
		currentNow = time.Date(2026, time.June, 28, 15, 0, 0, 0, etLocation)
		sunCurPrice, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.InDelta(t, 0.08330, sunCurPrice.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Eversource CT VPP Off-Peak", sunCurPrice.PeriodName)

		sunFuture, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		// Sunday 14:00-15:00 (TSEnd == 15:00) + 15:00..24:00 (9h) + Monday 24h = 34h
		assert.Len(t, sunFuture, 34)
		assert.Equal(t, int32(1), httpCalls.Load())

		// 5. Verify SiteFees wraps synthesized weekend prices cleanly
		siteFees := &SiteFees{base: c, siteID: "test-site"}
		require.NoError(t, siteFees.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_vpp",
		}))
		siteCurPrice, err := siteFees.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		// June 2026 Off-Peak supply (0.08330) + Delivery&FMCC (0.12078) = 0.20408
		assert.InDelta(t, 0.20408, siteCurPrice.DollarsPerKWH, 1e-6)

		m.AssertExpectations(t)
	})

	t.Run("Weekend synthesis prefers same half-year weekday and falls back across half-year boundary", func(t *testing.T) {
		// Case 1: July 1, 2028 is Saturday; both Friday 06/30/2028 (H1: 0.08000) and Monday 07/03/2028 (H2: 0.09500) exist.
		// Saturday 07/01/2028 is 1 day from Friday (H1) and 2 days from Monday (H2), so same-half-year rule picks Monday (0.09500).
		bothHalvesRates := []vppDailyRate{
			{date: time.Date(2028, time.June, 30, 0, 0, 0, 0, etLocation), onPeakPrice: 0.15000, offPeakPrice: 0.08000},
			{date: time.Date(2028, time.July, 3, 0, 0, 0, 0, etLocation), onPeakPrice: 0.16500, offPeakPrice: 0.09500},
		}
		filledBoth := fillEversourceVPPWeekendRates(
			bothHalvesRates,
			time.Date(2028, time.June, 30, 0, 0, 0, 0, etLocation),
			time.Date(2028, time.July, 3, 0, 0, 0, 0, etLocation),
		)
		byDateBoth := make(map[string]vppDailyRate)
		for _, r := range filledBoth {
			byDateBoth[r.date.Format("2006-01-02")] = r
		}
		if assert.Contains(t, byDateBoth, "2028-07-01") && assert.Contains(t, byDateBoth, "2028-07-02") {
			assert.InDelta(t, 0.09500, byDateBoth["2028-07-01"].offPeakPrice, 1e-6)
			assert.InDelta(t, 0.09500, byDateBoth["2028-07-02"].offPeakPrice, 1e-6)
		}

		// Case 2: January 1, 2028 is Saturday before Monday 01/03/2028 is published; falls back to Friday 12/31/2027.
		beforeMondayRates := []vppDailyRate{
			{date: time.Date(2027, time.December, 31, 0, 0, 0, 0, etLocation), onPeakPrice: 0.14000, offPeakPrice: 0.07900},
		}
		filledFallback := fillEversourceVPPWeekendRates(
			beforeMondayRates,
			time.Date(2027, time.December, 31, 0, 0, 0, 0, etLocation),
			time.Date(2028, time.January, 2, 0, 0, 0, 0, etLocation),
		)
		byDateFallback := make(map[string]vppDailyRate)
		for _, r := range filledFallback {
			byDateFallback[r.date.Format("2006-01-02")] = r
		}
		if assert.Contains(t, byDateFallback, "2028-01-01") && assert.Contains(t, byDateFallback, "2028-01-02") {
			assert.InDelta(t, 0.07900, byDateFallback["2028-01-01"].offPeakPrice, 1e-6)
			assert.InDelta(t, 0.07900, byDateFallback["2028-01-02"].offPeakPrice, 1e-6)
		}

		// Case 3: Explicit Saturday row in HTML is preserved while missing Sunday is synthesized.
		explicitSatRates := []vppDailyRate{
			{date: time.Date(2026, time.June, 26, 0, 0, 0, 0, etLocation), onPeakPrice: 0.15000, offPeakPrice: 0.08000},
			{date: time.Date(2026, time.June, 27, 0, 0, 0, 0, etLocation), onPeakPrice: 0.09123, offPeakPrice: 0.09123},
		}
		filledExplicit := fillEversourceVPPWeekendRates(
			explicitSatRates,
			time.Date(2026, time.June, 26, 0, 0, 0, 0, etLocation),
			time.Date(2026, time.June, 28, 0, 0, 0, 0, etLocation),
		)
		byDateExplicit := make(map[string]vppDailyRate)
		for _, r := range filledExplicit {
			byDateExplicit[r.date.Format("2006-01-02")] = r
		}
		if assert.Contains(t, byDateExplicit, "2026-06-27") && assert.Contains(t, byDateExplicit, "2026-06-28") {
			assert.InDelta(t, 0.09123, byDateExplicit["2026-06-27"].offPeakPrice, 1e-6)
			assert.InDelta(t, 0.08000, byDateExplicit["2026-06-28"].offPeakPrice, 1e-6)
		}
	})

	t.Run("Exclusive midnight bounds and empty ranges avoid repeat DB or HTTP calls", func(t *testing.T) {
		m := &storagemock.MockDatabase{}
		now := time.Date(2026, time.June, 30, 10, 15, 0, 0, etLocation)
		c := &baseEversourceVPP{
			vppHistoryURL: "http://invalid-url-should-not-be-called",
			client:        common.HTTPClient(time.Second),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
			nowFunc:       func() time.Time { return now },
		}

		dayStart := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation)
		dayEnd := dayStart.AddDate(0, 0, 1) // 2026-07-01 00:00:00 ET (exclusive)

		// Empty and inverted ranges return empty slice without touching DB or HTTP
		emptyEqual, err := c.GetConfirmedPrices(context.Background(), dayStart, dayStart)
		require.NoError(t, err)
		assert.Empty(t, emptyEqual)

		emptyInverted, err := c.GetConfirmedPrices(context.Background(), dayEnd, dayStart)
		require.NoError(t, err)
		assert.Empty(t, emptyInverted)

		// Populate ONLY 2026-06-30 (not 2026-07-01): [dayStart, dayStart+1d) and GetCurrentPrice must hit cache
		c.cachedPrices["2026-06-30"] = dailyRateToHourlyPrices(vppDailyRate{
			date:         dayStart,
			onPeakPrice:  0.15000,
			offPeakPrice: 0.08000,
		})

		dayPrices, err := c.GetConfirmedPrices(context.Background(), dayStart, dayEnd)
		require.NoError(t, err)
		assert.Len(t, dayPrices, 24)

		curPrice, err := c.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.InDelta(t, 0.08000, curPrice.DollarsPerKWH, 1e-6)

		// Populate 2026-07-01 (tomorrow) without 2026-07-02: [today, today+2d) via GetFuturePrices must hit cache
		c.cachedPrices["2026-07-01"] = dailyRateToHourlyPrices(vppDailyRate{
			date:         dayEnd,
			onPeakPrice:  0.16000,
			offPeakPrice: 0.08500,
		})

		twoDayPrices, err := c.GetConfirmedPrices(context.Background(), dayStart, dayStart.AddDate(0, 0, 2))
		require.NoError(t, err)
		assert.Len(t, twoDayPrices, 48)

		futurePrices, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		// 10:00..24:00 today (14h) + 24h tomorrow = 38h
		assert.Len(t, futurePrices, 38)

		m.AssertExpectations(t)
	})

	t.Run("Cooldown skips DB and HTTP fetches for 15 minutes when only unpublished future dates are missing", func(t *testing.T) {
		var publishTomorrow atomic.Bool
		var httpCalls atomic.Int32

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpCalls.Add(1)
			tomorrowRow := ""
			if publishTomorrow.Load() {
				tomorrowRow = `<tr><td>07/01/2026</td><td>0.09500</td><td>0.08900</td><td>0.16000</td><td>0.08500</td></tr>`
			}
			_, _ = fmt.Fprintf(w, `
<html><body><table class="table">
<thead>
<tr><th>Date</th><th colspan="2">Last Resort Service</th><th colspan="2">Residential</th></tr>
<tr><td>&nbsp;</td><td>On-Peak</td><td>Off-Peak</td><td>On-Peak</td><td>Off-Peak</td></tr>
</thead>
<tbody>
%s
<tr><td>06/30/2026</td><td>0.09301</td><td>0.08860</td><td>0.15000</td><td>0.08000</td></tr>
</tbody>
</table></body></html>`, tomorrowRow)
		}))
		defer ts.Close()

		// Start on Tuesday 2026-06-30 at 12:30 ET before tomorrow (07/01/2026) is published
		currentNow := time.Date(2026, time.June, 30, 12, 30, 0, 0, etLocation)
		today := time.Date(2026, time.June, 30, 0, 0, 0, 0, etLocation)
		tomorrowEnd := today.AddDate(0, 0, 2)

		m := &storagemock.MockDatabase{}
		c := &baseEversourceVPP{
			vppHistoryURL: ts.URL,
			client:        ts.Client(),
			cachedPrices:  make(map[string][]types.Price),
			db:            m,
			nowFunc:       func() time.Time { return currentNow },
		}

		// Initial GetFuturePrices misses cache & DB, fetches HTTP once, caches 06/30/2026, sets lastHistoryFetch
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", today, tomorrowEnd).Return([]types.PriceState{}, nil).Once()
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", today.AddDate(0, 0, -3), currentNow).Return([]types.PriceState{{Confirmed: true}}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.Anything, 0).Return(nil).Once()

		future1, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		// 12:00..24:00 today = 12 hours (tomorrow omitted)
		assert.Len(t, future1, 12)
		assert.Equal(t, int32(1), httpCalls.Load())

		// Within 15-minute cooldown (+5m = 12:35 ET): GetFuturePrices and 12h lookahead GetConfirmedPrices
		// ([12:35 ET, tomorrow 00:35 ET), matching pkg/server/update.go) must make ZERO DB or HTTP calls
		currentNow = currentNow.Add(5 * time.Minute)
		future2, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Len(t, future2, 12)
		assert.Equal(t, int32(1), httpCalls.Load())

		lookaheadPrices, err := c.GetConfirmedPrices(context.Background(), currentNow.Truncate(time.Hour), currentNow.Add(12*time.Hour))
		require.NoError(t, err)
		assert.Len(t, lookaheadPrices, 12)
		assert.Equal(t, int32(1), httpCalls.Load())

		// Advance past 15-minute cooldown (+11m more = +16m total = 12:46 ET) and publish tomorrow
		publishTomorrow.Store(true)
		currentNow = currentNow.Add(11 * time.Minute)

		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", today, tomorrowEnd).Return([]types.PriceState{}, nil).Once()
		m.On("GetUtilityPrices", mock.Anything, "eversource_ct_vpp", today.AddDate(0, 0, -3), currentNow).Return([]types.PriceState{{Confirmed: true}}, nil).Once()
		m.On("UpsertUtilityPrices", mock.Anything, "eversource_ct_vpp", mock.Anything, 0).Return(nil).Once()

		future3, err := c.GetFuturePrices(context.Background())
		require.NoError(t, err)
		// 12:00..24:00 today (12h) + 24h tomorrow = 36h
		assert.Len(t, future3, 36)
		assert.Equal(t, int32(2), httpCalls.Load())

		m.AssertExpectations(t)
	})

	t.Run("DST spring-forward emits 23 hourly prices and fall-back emits 25 hourly prices", func(t *testing.T) {
		// 2026-03-08: Spring-forward Sunday (clocks jump 01:59:59 EST -> 03:00:00 EDT)
		springDay := time.Date(2026, time.March, 8, 0, 0, 0, 0, etLocation)
		springPrices := dailyRateToHourlyPrices(vppDailyRate{
			date:         springDay,
			onPeakPrice:  0.15000,
			offPeakPrice: 0.08000,
		})
		if assert.Len(t, springPrices, 23) {
			assert.Equal(t, springDay, springPrices[0].TSStart)
			assert.Equal(t, springDay.AddDate(0, 0, 1), springPrices[22].TSEnd)
			for i, p := range springPrices {
				assert.Equal(t, time.Hour, p.TSEnd.Sub(p.TSStart))
				assert.NotEqual(t, 2, p.TSStart.In(etLocation).Hour(), "02:00 local hour should not exist on spring-forward day")
				if i > 0 {
					assert.Equal(t, springPrices[i-1].TSEnd, p.TSStart)
				}
			}
		}

		// 2026-11-01: Fall-back Sunday (clocks repeat 01:00 EDT -> 01:00 EST)
		fallDay := time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)
		fallPrices := dailyRateToHourlyPrices(vppDailyRate{
			date:         fallDay,
			onPeakPrice:  0.15000,
			offPeakPrice: 0.08500,
		})
		if assert.Len(t, fallPrices, 25) {
			assert.Equal(t, fallDay, fallPrices[0].TSStart)
			assert.Equal(t, fallDay.AddDate(0, 0, 1), fallPrices[24].TSEnd)

			// Index 1 is 01:00 EDT (-04:00), Index 2 is 01:00 EST (-05:00)
			zone1, offset1 := fallPrices[1].TSStart.Zone()
			zone2, offset2 := fallPrices[2].TSStart.Zone()
			assert.Equal(t, 1, fallPrices[1].TSStart.Hour())
			assert.Equal(t, "EDT", zone1)
			assert.Equal(t, -4*3600, offset1)

			assert.Equal(t, 1, fallPrices[2].TSStart.Hour())
			assert.Equal(t, "EST", zone2)
			assert.Equal(t, -5*3600, offset2)

			for i, p := range fallPrices {
				assert.Equal(t, time.Hour, p.TSEnd.Sub(p.TSStart))
				if i > 0 {
					assert.Equal(t, fallPrices[i-1].TSEnd, p.TSStart)
				}
			}

			// Verify GetCurrentPrice finds 01:30 EST (-05:00) during repeated fall-back hour
			fallBackSecondOneThirty := fallPrices[2].TSStart.Add(30 * time.Minute)
			c := &baseEversourceVPP{
				cachedPrices: map[string][]types.Price{
					"2026-11-01": fallPrices,
				},
				nowFunc: func() time.Time { return fallBackSecondOneThirty },
			}
			curPrice, err := c.GetCurrentPrice(context.Background())
			require.NoError(t, err)
			assert.Equal(t, fallPrices[2].TSStart, curPrice.TSStart)
			assert.InDelta(t, 0.08500, curPrice.DollarsPerKWH, 1e-6)
		}
	})
}

func TestEversourceVPP(t *testing.T) {
	t.Run("default options return no VPP periods", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
		})
		require.NoError(t, err)

		vppInfo, err := u.GetVPPInfo(context.Background())
		require.NoError(t, err)
		assert.Empty(t, vppInfo.Mandatory)
	})

	t.Run("ess-passive option returns correct periods", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
			UtilityRateOptions: types.UtilityRateOptions{
				VPPProgram: "ess-passive",
			},
		})
		require.NoError(t, err)

		vppInfo, err := u.GetVPPInfo(context.Background())
		require.NoError(t, err)
		// 3 months (June, July, August) per year * 2 years (2026, 2027) = 6 periods
		require.Len(t, vppInfo.Mandatory, 6)

		for _, p := range vppInfo.Mandatory {
			assert.Equal(t, 20.0, p.ReserveSOC)
			require.Len(t, p.Hours, 1)
			assert.Equal(t, 17, p.Hours[0].HourStart)
			assert.Equal(t, 20, p.Hours[0].HourEnd)
			assert.Equal(t, []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, p.DaysOfTheWeek)
		}

		// Helper to check if a specific time is covered by VPP periods
		isVPPHour := func(tVal time.Time) bool {
			for _, p := range vppInfo.Mandatory {
				contains, _, err := p.Contains(tVal)
				if err == nil && contains {
					return true
				}
			}
			return false
		}

		// 2026 tests (Juneteenth June 19 is Friday -> holiday, Independence Day July 4 is Saturday -> holiday)
		// June 18, 2026 is Thursday, 6 PM -> Weekday, VPP hour -> should be VPP
		assert.True(t, isVPPHour(time.Date(2026, time.June, 18, 18, 0, 0, 0, etLocation)))
		// June 18, 2026 is Thursday, 4 PM -> outside hours -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 18, 16, 0, 0, 0, etLocation)))
		// June 18, 2026 is Thursday, 9 PM -> outside hours -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 18, 21, 0, 0, 0, etLocation)))

		// Weekend check: June 20, 2026 is Saturday, 6 PM -> weekend -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 20, 18, 0, 0, 0, etLocation)))

		// Holiday check 2026: Juneteenth June 19, 2026 is a Friday -> holiday -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.June, 19, 18, 0, 0, 0, etLocation)))

		// July 3, 2026 is Friday, 6 PM -> not holiday -> should be VPP
		assert.True(t, isVPPHour(time.Date(2026, time.July, 3, 18, 0, 0, 0, etLocation)))
		// July 4, 2026 is Saturday -> weekend + holiday -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2026, time.July, 4, 18, 0, 0, 0, etLocation)))
		// July 6, 2026 is Monday -> not holiday -> should be VPP
		assert.True(t, isVPPHour(time.Date(2026, time.July, 6, 18, 0, 0, 0, etLocation)))

		// 2027 tests (July 4, 2027 is Sunday -> not shifted, so July 4 is the holiday, and July 5 is a regular weekday VPP day)
		// July 2, 2027 is Friday, 6 PM -> should be VPP
		assert.True(t, isVPPHour(time.Date(2027, time.July, 2, 18, 0, 0, 0, etLocation)))
		// July 4, 2027 is Sunday -> weekend -> should NOT be VPP
		assert.False(t, isVPPHour(time.Date(2027, time.July, 4, 18, 0, 0, 0, etLocation)))
		// July 5, 2027 is Monday -> regular weekday VPP day (July 4 not shifted to Monday for VPP) -> should be VPP
		assert.True(t, isVPPHour(time.Date(2027, time.July, 5, 18, 0, 0, 0, etLocation)))
		// July 6, 2027 is Tuesday, 6 PM -> should be VPP
		assert.True(t, isVPPHour(time.Date(2027, time.July, 6, 18, 0, 0, 0, etLocation)))
	})
}
