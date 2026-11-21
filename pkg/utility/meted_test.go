package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetedPeriods(t *testing.T) {
	t.Run("Rate RS Flat Rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_rs",
		})
		require.NoError(t, err)

		// Winter 2025-2026 pre-April window (Feb 10, 2026): $0.073389 + $0.129624 = $0.203013/kWh
		p, err := u.priceForTime(time.Date(2026, time.February, 10, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.203013, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)

		// Spring 2026 LEDC Supp. 34 window (Apr 15, 2026): $0.073389 + $0.129624 = $0.203013/kWh
		p, err = u.priceForTime(time.Date(2026, time.April, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.203013, p.DollarsPerKWH, 1e-6)

		// Summer 2026 Supp. 36/37/38 window (Jul 15, 2026): $0.073389 + $0.139482 = $0.212871/kWh
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.212871, p.DollarsPerKWH, 1e-6)

		// Fall 2026 Q4 DSIC Supp. 40 verified window (Oct 15, 2026): $0.073389 + $0.139482 = $0.212871/kWh
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.212871, p.DollarsPerKWH, 1e-6)

		// Winter 2026-2027 (Dec 15, 2026): $0.073389 + $0.144191 = $0.217580/kWh
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217580, p.DollarsPerKWH, 1e-6)

		// 2027 continuity across Jan-Mar (Feb 10, 2027), Apr-May (Apr 15, 2027), and Jun-Dec (Aug 15, 2027): $0.217580/kWh
		p, err = u.priceForTime(time.Date(2027, time.February, 10, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217580, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.April, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217580, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.August, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217580, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("TOU Winter Spring 2026 Weekday", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Tuesday, Feb 10, 2026 (Jan-Mar window) & Wednesday, Apr 15, 2026 (Apr-May window)
		febTue := time.Date(2026, time.February, 10, 0, 0, 0, 0, etLocation)
		aprWed := time.Date(2026, time.April, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak ($0.153950/kWh)
		p, err := u.priceForTime(febTue.Add(2 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.153950, p.DollarsPerKWH, 1e-6)

		// Off-Peak ($0.182234/kWh)
		p, err = u.priceForTime(febTue.Add(10 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.182234, p.DollarsPerKWH, 1e-6)

		// On-Peak ($0.294527/kWh) across both Jan-Mar and Apr-May 2026 windows
		p, err = u.priceForTime(febTue.Add(15 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.294527, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(aprWed.Add(15 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.294527, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU Summer Fall 2026 Weekday", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Verify period names exist in GetPeriods
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, period := range periods {
			names[period.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])
		assert.True(t, names["Super Off-Peak"])

		// Monday, June 15, 2026 (Jun-Sep window) and Thursday, October 15, 2026 (Oct-Nov Q4 DSIC window)
		mon := time.Date(2026, time.June, 15, 0, 0, 0, 0, etLocation)
		octThu := time.Date(2026, time.October, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak: 02:00 AM and 11:30 PM ($0.160077/kWh)
		p, err := u.priceForTime(mon.Add(2 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.160077, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(23*time.Hour + 30*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.160077, p.DollarsPerKWH, 1e-6)

		// Off-Peak: 10:00 AM, 1:59 PM, and 9:00 PM ($0.190512/kWh)
		p, err = u.priceForTime(mon.Add(10 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.190512, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(13*time.Hour + 59*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.190512, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(21 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.190512, p.DollarsPerKWH, 1e-6)

		// On-Peak: 2:00 PM - 9:00 PM Mon-Fri ($0.311345/kWh)
		p, err = u.priceForTime(mon.Add(14 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.311345, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(20*time.Hour + 59*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.311345, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(octThu.Add(15 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.311345, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU Summer Fall 2026 Weekend", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Saturday, June 20, 2026
		sat := time.Date(2026, time.June, 20, 0, 0, 0, 0, etLocation)

		// 4:00 PM on weekend is Off-Peak ($0.190512/kWh)
		p, err := u.priceForTime(sat.Add(16 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.190512, p.DollarsPerKWH, 1e-6)

		// 11:30 PM on weekend is Super Off-Peak ($0.160077/kWh)
		p, err = u.priceForTime(sat.Add(23*time.Hour + 30*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.160077, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU Winter 2026-2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Tuesday, Dec 15, 2026
		decTue := time.Date(2026, time.December, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak: 05:30 AM ($0.163004/kWh)
		p, err := u.priceForTime(decTue.Add(5*time.Hour + 30*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.163004, p.DollarsPerKWH, 1e-6)

		// Off-Peak: 10:00 PM ($0.194466/kWh)
		p, err = u.priceForTime(decTue.Add(22 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.194466, p.DollarsPerKWH, 1e-6)

		// On-Peak: 6:00 PM ($0.319379/kWh)
		p, err = u.priceForTime(decTue.Add(18 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.319379, p.DollarsPerKWH, 1e-6)

		// Verify 2027 windows (Jan-Mar, Apr-May, Jun-Dec) carry Q4 2026 riders + Winter 2026-2027 PTC forward
		janMon := time.Date(2027, time.January, 11, 18, 0, 0, 0, etLocation)
		p, err = u.priceForTime(janMon)
		require.NoError(t, err)
		assert.InDelta(t, 0.319379, p.DollarsPerKWH, 1e-6)

		aprWed := time.Date(2027, time.April, 14, 18, 0, 0, 0, etLocation)
		p, err = u.priceForTime(aprWed)
		require.NoError(t, err)
		assert.InDelta(t, 0.319379, p.DollarsPerKWH, 1e-6)

		julMon := time.Date(2027, time.July, 12, 18, 0, 0, 0, etLocation)
		p, err = u.priceForTime(julMon)
		require.NoError(t, err)
		assert.InDelta(t, 0.319379, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Supplement Boundary Transitions and Forward Continuity", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_rs",
		})
		require.NoError(t, err)

		// May 31, 2026 23:00 -> Jun 1, 2026 00:00 transition ($0.203013 -> $0.212871)
		pMayEnd, err := u.priceForTime(time.Date(2026, time.May, 31, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.203013, pMayEnd.DollarsPerKWH, 1e-6)

		pJunStart, err := u.priceForTime(time.Date(2026, time.June, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.212871, pJunStart.DollarsPerKWH, 1e-6)

		// Nov 30, 2026 23:00 -> Dec 1, 2026 00:00 transition ($0.212871 -> $0.217580)
		pNovEnd, err := u.priceForTime(time.Date(2026, time.November, 30, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.212871, pNovEnd.DollarsPerKWH, 1e-6)

		pDecStart, err := u.priceForTime(time.Date(2026, time.December, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217580, pDecStart.DollarsPerKWH, 1e-6)

		// Verify 180-day forward continuity across [2026-11-21, 2027-05-20]
		start := time.Date(2026, time.November, 21, 0, 0, 0, 0, etLocation)
		end := start.AddDate(0, 0, 180)
		confirmed, err := u.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		assert.NotEmpty(t, confirmed)
	})

	t.Run("Prior Year Fallback and Unknown Plan", func(t *testing.T) {
		periods2025 := metedPeriods("meted_rs", types.UtilityRateOptions{}, []int{2025})
		if assert.Len(t, periods2025, 1) {
			assert.InDelta(t, 0.203013, periods2025[0].DollarsPerKWH, 1e-6)
		}

		assert.PanicsWithValue(t, "unknown meted plan: invalid", func() {
			metedPeriods("invalid", types.UtilityRateOptions{}, []int{2026})
		})
	})
}

func TestMetedUtilityInfo(t *testing.T) {
	info := metedUtilityInfo()
	assert.Equal(t, "meted", info.ID)
	assert.Equal(t, "Metropolitan Edison (Met-Ed)", info.Name)

	t.Run("Rates and Net Metering Option", func(t *testing.T) {
		if assert.Len(t, info.Rates, 2) {
			assert.Equal(t, "meted_rs", info.Rates[0].ID)
			assert.Equal(t, "Residential Service (Rate RS)", info.Rates[0].Name)
			if assert.Len(t, info.Rates[0].Options, 1) {
				assert.Equal(t, "netMeteringCredits", info.Rates[0].Options[0].Field)
				assert.Equal(t, true, info.Rates[0].Options[0].Default)
				assert.True(t, info.Rates[0].Options[0].Hidden)
			}

			assert.Equal(t, "meted_tou", info.Rates[1].ID)
			assert.Equal(t, "Residential Time-of-Use (TOU)", info.Rates[1].Name)
			if assert.Len(t, info.Rates[1].Options, 1) {
				assert.Equal(t, "netMeteringCredits", info.Rates[1].Options[0].Field)
				assert.Equal(t, true, info.Rates[1].Options[0].Default)
				assert.True(t, info.Rates[1].Options[0].Hidden)
			}
		}
	})
}
