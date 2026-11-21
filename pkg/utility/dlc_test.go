package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDLCHolidays(t *testing.T) {
	t.Run("PJM Holiday shifts", func(t *testing.T) {
		// 2026 July 4th is Saturday -> observed Friday July 3rd
		holidays2026 := getDLCHolidays(2026)
		if assert.Contains(t, holidays2026, "2026-07-03") {
			assert.NotContains(t, holidays2026, "2026-07-04")
		}

		// 2027 July 4th is Sunday -> observed Monday July 5th
		holidays2027 := getDLCHolidays(2027)
		if assert.Contains(t, holidays2027, "2027-07-05") {
			assert.NotContains(t, holidays2027, "2027-07-04")
		}

		// 2025 July 4th is Friday -> remains Friday July 4th
		holidays2025 := getDLCHolidays(2025)
		assert.Contains(t, holidays2025, "2025-07-04")
	})

	t.Run("Memorial Day and Labor Day", func(t *testing.T) {
		holidays2026 := getDLCHolidays(2026)
		assert.Contains(t, holidays2026, "2026-05-25") // Memorial Day 2026
		assert.Contains(t, holidays2026, "2026-09-07") // Labor Day 2026
	})
}

func TestDLCRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("RS Flat Rate across 2026 and 2027 tariff windows", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dlc",
			UtilityRate:     "dlc_rs",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		t.Run("Q1 2026 Jan-Mar", func(t *testing.T) {
			p, err := u.priceForTime(time.Date(2026, time.February, 15, 14, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.238264, p.DollarsPerKWH, 1e-6)
		})

		t.Run("Q2 2026 Apr-May", func(t *testing.T) {
			p, err := u.priceForTime(time.Date(2026, time.May, 15, 14, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.239166, p.DollarsPerKWH, 1e-6)
		})

		t.Run("Q2 2026 June", func(t *testing.T) {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.242872, p.DollarsPerKWH, 1e-6)
		})

		t.Run("Q3 2026 Jul-Sep", func(t *testing.T) {
			p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.243653, p.DollarsPerKWH, 1e-6)
		})

		t.Run("Q4 2026 Oct-Nov", func(t *testing.T) {
			p, err := u.priceForTime(time.Date(2026, time.November, 18, 15, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.244844, p.DollarsPerKWH, 1e-6)
		})

		t.Run("December 2026 Supplement 111", func(t *testing.T) {
			p, err := u.priceForTime(time.Date(2026, time.December, 1, 0, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.248994, p.DollarsPerKWH, 1e-6)

			pMid, err := u.priceForTime(time.Date(2026, time.December, 15, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.248994, pMid.DollarsPerKWH, 1e-6)
		})

		t.Run("2027 carried forward", func(t *testing.T) {
			pJan, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.248994, pJan.DollarsPerKWH, 1e-6)

			pDec, err := u.priceForTime(time.Date(2027, time.December, 31, 23, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.248994, pDec.DollarsPerKWH, 1e-6)
		})
	})

	t.Run("TOU Supply Rate Pilot across 2026 and 2027 tariff windows", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dlc",
			UtilityRate:     "dlc_tou",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])
		assert.True(t, names["Super Off-Peak"])

		t.Run("Q1 2026 TOU rates", func(t *testing.T) {
			// Wednesday, Feb 18, 2026
			pPeak, err := u.priceForTime(time.Date(2026, time.February, 18, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.445554, pPeak.DollarsPerKWH, 1e-6)

			pOffPeak, err := u.priceForTime(time.Date(2026, time.February, 18, 10, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.186439, pOffPeak.DollarsPerKWH, 1e-6)

			pSuperOff, err := u.priceForTime(time.Date(2026, time.February, 18, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.171004, pSuperOff.DollarsPerKWH, 1e-6)
		})

		t.Run("Q3 2026 TOU weekday weekend and PJM holiday rates", func(t *testing.T) {
			// Weekday On-Peak (Wednesday, July 15, 2026 at 4:00 PM)
			p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.463770, p.DollarsPerKWH, 1e-6)

			// Weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
			p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.188899, p.DollarsPerKWH, 1e-6)

			// Weekday Super Off-Peak (Wednesday, July 15, 2026 at 2:00 AM)
			p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.173572, p.DollarsPerKWH, 1e-6)

			// Weekend Super Off-Peak (Saturday, July 18, 2026 at 2:00 AM)
			p, err = u.priceForTime(time.Date(2026, time.July, 18, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.173572, p.DollarsPerKWH, 1e-6)

			// Weekend Off-Peak (Saturday, July 18, 2026 at 4:00 PM)
			p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.188899, p.DollarsPerKWH, 1e-6)

			// PJM Holiday Off-Peak during weekday peak window (Labor Day Monday, Sep 7, 2026 at 4:00 PM)
			p, err = u.priceForTime(time.Date(2026, time.September, 7, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.188899, p.DollarsPerKWH, 1e-6)

			// PJM Holiday Super Off-Peak (Labor Day Monday, Sep 7, 2026 at 2:00 AM)
			p, err = u.priceForTime(time.Date(2026, time.September, 7, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.173572, p.DollarsPerKWH, 1e-6)
		})

		t.Run("Q4 Oct-Nov 2026 TOU rates and Thanksgiving holiday", func(t *testing.T) {
			// Wednesday, Nov 18, 2026
			pPeak, err := u.priceForTime(time.Date(2026, time.November, 18, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.464961, pPeak.DollarsPerKWH, 1e-6)

			pOffPeak, err := u.priceForTime(time.Date(2026, time.November, 18, 10, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.190090, pOffPeak.DollarsPerKWH, 1e-6)

			pSuperOff, err := u.priceForTime(time.Date(2026, time.November, 18, 23, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.174763, pSuperOff.DollarsPerKWH, 1e-6)

			// Thanksgiving Thursday, Nov 26, 2026 at 4:00 PM -> Off-Peak
			pHoliday, err := u.priceForTime(time.Date(2026, time.November, 26, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.190090, pHoliday.DollarsPerKWH, 1e-6)
		})

		t.Run("December 2026 Supplement 111 TOU rates", func(t *testing.T) {
			// Tuesday, Dec 1, 2026
			pPeak, err := u.priceForTime(time.Date(2026, time.December, 1, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.477458, pPeak.DollarsPerKWH, 1e-6)

			pOffPeak, err := u.priceForTime(time.Date(2026, time.December, 1, 10, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.192165, pOffPeak.DollarsPerKWH, 1e-6)

			pSuperOff, err := u.priceForTime(time.Date(2026, time.December, 1, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.176251, pSuperOff.DollarsPerKWH, 1e-6)

			// Christmas Day Friday, Dec 25, 2026 at 4:00 PM -> Off-Peak
			pXmas, err := u.priceForTime(time.Date(2026, time.December, 25, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.192165, pXmas.DollarsPerKWH, 1e-6)
		})

		t.Run("2027 TOU rates carried forward", func(t *testing.T) {
			// Wednesday, Jun 16, 2027
			pPeak, err := u.priceForTime(time.Date(2027, time.June, 16, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.477458, pPeak.DollarsPerKWH, 1e-6)

			pOffPeak, err := u.priceForTime(time.Date(2027, time.June, 16, 12, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.192165, pOffPeak.DollarsPerKWH, 1e-6)

			pSuperOff, err := u.priceForTime(time.Date(2027, time.June, 16, 23, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.176251, pSuperOff.DollarsPerKWH, 1e-6)

			// Observed Independence Day Monday, July 5, 2027 at 4:00 PM -> Off-Peak
			pJul5, err := u.priceForTime(time.Date(2027, time.July, 5, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.192165, pJul5.DollarsPerKWH, 1e-6)

			// Observed 2028 New Year's Day on Friday, Dec 31, 2027 at 4:00 PM -> Off-Peak
			pDec31, err := u.priceForTime(time.Date(2027, time.December, 31, 16, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.192165, pDec31.DollarsPerKWH, 1e-6)
		})

		t.Run("DST and month boundary continuity", func(t *testing.T) {
			// Spring-forward DST day in America/New_York (Sunday, March 8, 2026: 23 hours)
			springStart := time.Date(2026, time.March, 8, 0, 0, 0, 0, etLocation)
			springEnd := time.Date(2026, time.March, 9, 0, 0, 0, 0, etLocation)
			springPrices, err := u.GetConfirmedPrices(context.Background(), springStart, springEnd)
			require.NoError(t, err)
			assert.Len(t, springPrices, 23)

			// Fall-back DST day in America/New_York (Sunday, November 1, 2026: 25 hours)
			fallStart := time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)
			fallEnd := time.Date(2026, time.November, 2, 0, 0, 0, 0, etLocation)
			fallPrices, err := u.GetConfirmedPrices(context.Background(), fallStart, fallEnd)
			require.NoError(t, err)
			assert.Len(t, fallPrices, 25)

			// Supplement 111 effective boundary: Nov 30, 2026 23:00 ET -> Dec 1, 2026 00:00 ET
			decBoundaryPrices, err := u.GetConfirmedPrices(
				context.Background(),
				time.Date(2026, time.November, 30, 23, 0, 0, 0, etLocation),
				time.Date(2026, time.December, 1, 1, 0, 0, 0, etLocation),
			)
			require.NoError(t, err)
			if assert.Len(t, decBoundaryPrices, 2) {
				assert.InDelta(t, 0.174763, decBoundaryPrices[0].DollarsPerKWH, 1e-6)
				assert.InDelta(t, 0.176251, decBoundaryPrices[1].DollarsPerKWH, 1e-6)
			}
		})
	})
}

func TestDLCMetadata(t *testing.T) {
	info := dlcUtilityInfo()
	assert.Equal(t, "dlc", info.ID)
	assert.Equal(t, "Duquesne Light Co. (DLC)", info.Name)
	if assert.Len(t, info.Rates, 2) {
		assert.Equal(t, "dlc_rs", info.Rates[0].ID)
		if assert.Len(t, info.Rates[0].Options, 1) {
			assert.Equal(t, "netMeteringCredits", info.Rates[0].Options[0].Field)
			assert.Equal(t, true, info.Rates[0].Options[0].Default)
			assert.True(t, info.Rates[0].Options[0].Hidden)
		}
		assert.Equal(t, "dlc_tou", info.Rates[1].ID)
		if assert.Len(t, info.Rates[1].Options, 1) {
			assert.Equal(t, "netMeteringCredits", info.Rates[1].Options[0].Field)
			assert.Equal(t, true, info.Rates[1].Options[0].Default)
			assert.True(t, info.Rates[1].Options[0].Hidden)
		}
	}
}
