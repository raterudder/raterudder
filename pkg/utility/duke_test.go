package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuke(t *testing.T) {
	t.Run("getDukeHolidays shifts holidays correctly", func(t *testing.T) {
		holidays := getDukeHolidays(2026)
		// 2026 Holidays check:
		// - New Year's Day (Jan 1, Thu)
		// - Good Friday (Apr 3, Fri)
		// - Memorial Day (May 25, Mon)
		// - Independence Day (Jul 4, Sat -> shifts to Jul 3, Fri)
		// - Labor Day (Sep 7, Mon)
		// - Thanksgiving (Nov 26, Thu)
		// - Day after Thanksgiving (Nov 27, Fri)
		// - Christmas Day (Dec 25, Fri)
		if assert.Len(t, holidays, 8) {
			assert.Contains(t, holidays, "2026-01-01")
			assert.Contains(t, holidays, "2026-04-03")
			assert.Contains(t, holidays, "2026-05-25")
			assert.Contains(t, holidays, "2026-07-03")
			assert.Contains(t, holidays, "2026-09-07")
			assert.Contains(t, holidays, "2026-11-26")
			assert.Contains(t, holidays, "2026-11-27")
			assert.Contains(t, holidays, "2026-12-25")
		}
	})

	t.Run("getIndianaHolidays lists expected holidays", func(t *testing.T) {
		holidays := getIndianaHolidays(2026)
		// Indiana has 6 holidays (no day after Thanksgiving or Good Friday)
		if assert.Len(t, holidays, 6) {
			assert.Contains(t, holidays, "2026-01-01")
			assert.Contains(t, holidays, "2026-05-25")
			assert.Contains(t, holidays, "2026-07-03") // Independence Day shifted from Jul 4 Sat
			assert.Contains(t, holidays, "2026-09-07")
			assert.Contains(t, holidays, "2026-11-26")
			assert.Contains(t, holidays, "2026-12-25")
		}
	})

	t.Run("isDEIWinter identifies winter boundaries", func(t *testing.T) {
		// DEI Winter: 1st Sunday of Nov to 2nd Sunday of March.
		// In 2026, 1st Sunday of Nov is Nov 1.
		// In 2026, 2nd Sunday of March is March 8.
		assert.True(t, isDEIWinter(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation)))
		assert.True(t, isDEIWinter(time.Date(2026, time.February, 28, 12, 0, 0, 0, etLocation)))
		assert.True(t, isDEIWinter(time.Date(2026, time.March, 7, 12, 0, 0, 0, etLocation)))
		assert.False(t, isDEIWinter(time.Date(2026, time.March, 8, 12, 0, 0, 0, etLocation)))
		assert.False(t, isDEIWinter(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)))
		assert.False(t, isDEIWinter(time.Date(2026, time.October, 31, 12, 0, 0, 0, etLocation)))
		assert.True(t, isDEIWinter(time.Date(2026, time.November, 1, 12, 0, 0, 0, etLocation)))
		assert.True(t, isDEIWinter(time.Date(2026, time.December, 25, 12, 0, 0, 0, etLocation)))
	})

	getPriceForTime := func(periods []types.UtilityFeesPeriod, target time.Time) types.Price {
		start := time.Date(target.Year(), target.Month(), target.Day(), target.Hour(), 0, 0, 0, target.Location())
		p := types.Price{
			Provider:      "tou",
			TSStart:       start,
			TSEnd:         start.Add(time.Hour),
			DollarsPerKWH: 0,
		}
		res, err := types.ApplyUtilityFeesPeriods(p, periods)
		require.NoError(t, err)
		return res
	}

	t.Run("duke_carolinas_nc_rs and re rates", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{}, []int{2026})
		p := getPriceForTime(periods, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.Equal(t, 0.122603, p.DollarsPerKWH)

		periodsRE := dukeCarolinasNCPeriods("duke_carolinas_nc_re", types.UtilityRateOptions{}, []int{2026})
		pRE := getPriceForTime(periodsRE, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.Equal(t, 0.117845, pRE.DollarsPerKWH)
	})

	t.Run("duke_carolinas_nc_rt seasons and hours", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rt", types.UtilityRateOptions{}, []int{2026})

		// Summer Weekday (June 15, 2026 - Monday)
		// On-peak: 18:00 - 21:00 (6 PM - 9 PM)
		pOn := getPriceForTime(periods, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.Equal(t, 0.171204, pOn.DollarsPerKWH)

		// Discount: 1:00 - 6:00 (1 AM - 6 AM)
		pDisc := getPriceForTime(periods, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation))
		assert.Equal(t, 0.053929, pDisc.DollarsPerKWH)

		// Off-peak: other (1 PM is off-peak in summer)
		pOff13 := getPriceForTime(periods, time.Date(2026, time.June, 15, 13, 0, 0, 0, etLocation))
		assert.Equal(t, 0.078411, pOff13.DollarsPerKWH)

		// Off-peak: other
		pOff := getPriceForTime(periods, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation))
		assert.Equal(t, 0.078411, pOff.DollarsPerKWH)

		// Summer Weekend (June 14, 2026 - Sunday) - no on-peak
		pOnWE := getPriceForTime(periods, time.Date(2026, time.June, 14, 19, 0, 0, 0, etLocation))
		assert.Equal(t, 0.078411, pOnWE.DollarsPerKWH)

		// Winter Weekday (Dec 15, 2026 - Tuesday)
		// On-peak: 6:00 - 9:00
		pWinOn := getPriceForTime(periods, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation))
		assert.Equal(t, 0.171204, pWinOn.DollarsPerKWH)
	})

	t.Run("duke_carolinas_nc_rt_ev charging hours", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rt_ev", types.UtilityRateOptions{}, []int{2026})

		// Discount charging: 23:00 - 5:00
		pOff := getPriceForTime(periods, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation))
		assert.Equal(t, 0.061752, pOff.DollarsPerKWH)

		// Standard: other
		pStd := getPriceForTime(periods, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.Equal(t, 0.123504, pStd.DollarsPerKWH)
	})

	t.Run("duke_carolinas_nc export credits", func(t *testing.T) {
		// RSC option
		periodsRSC := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{NetMeteringScheme: "rsc"}, []int{2026})
		p := getPriceForTime(periodsRSC, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0453, p.GenerationCreditDollarsPerKWH)

		// SCG option
		periodsSCG := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{NetMeteringScheme: "scg"}, []int{2026})

		// June weekday (June 15, 2026 - Monday)
		// Summer Premium Peak (17:00 - 21:00) => 0.0615
		pPrem := getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.True(t, pPrem.SeparateGenerationCredit)
		assert.Equal(t, 0.0615, pPrem.GenerationCreditDollarsPerKWH)

		// Summer On-Peak (12:00 - 17:00) => 0.0479
		pOn := getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.Equal(t, 0.0479, pOn.GenerationCreditDollarsPerKWH)

		// Summer Off-Peak (other) => 0.0414
		pOff := getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation))
		assert.Equal(t, 0.0414, pOff.GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_carolinas_sc_r_stou seasons and hours", func(t *testing.T) {
		periods := dukeCarolinasSCPeriods("duke_carolinas_sc_r_stou", types.UtilityRateOptions{}, []int{2026})

		// Non-winter weekday (June 15, 2026 - Monday)
		// On-peak: 18:00 - 21:00
		pOn := getPriceForTime(periods, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.Equal(t, 0.209021, pOn.DollarsPerKWH)

		// Super off-peak: 0:00 - 6:00
		pSuperOff := getPriceForTime(periods, time.Date(2026, time.June, 15, 3, 0, 0, 0, etLocation))
		assert.Equal(t, 0.093782, pSuperOff.DollarsPerKWH)

		// Off-peak: other
		pOff := getPriceForTime(periods, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.Equal(t, 0.128191, pOff.DollarsPerKWH)

		// Winter weekday (Dec 15, 2026 - Tuesday)
		// On-peak: 6:00 - 9:00, 18:00 - 21:00
		pWinOn1 := getPriceForTime(periods, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation))
		assert.Equal(t, 0.209021, pWinOn1.DollarsPerKWH)
		pWinOn2 := getPriceForTime(periods, time.Date(2026, time.December, 15, 19, 0, 0, 0, etLocation))
		assert.Equal(t, 0.209021, pWinOn2.DollarsPerKWH)

		// No super off-peak in winter (3 AM off-peak rate)
		pWinOff := getPriceForTime(periods, time.Date(2026, time.December, 15, 3, 0, 0, 0, etLocation))
		assert.Equal(t, 0.128191, pWinOff.DollarsPerKWH)
	})

	t.Run("duke_progress_nc export rates (Schedule PP SCG)", func(t *testing.T) {
		periods := dukeProgressNCPeriods("duke_progress_nc_res", types.UtilityRateOptions{NetMeteringScheme: "scg"}, []int{2026})

		// June weekday (June 15, 2026 - Monday)
		// Summer Premium Peak (18:00 - 22:00)
		pPrem := getPriceForTime(periods, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.True(t, pPrem.SeparateGenerationCredit)
		assert.Equal(t, 0.0597, pPrem.GenerationCreditDollarsPerKWH)

		// Summer On-Peak (14:00 - 18:00)
		pOn := getPriceForTime(periods, time.Date(2026, time.June, 15, 15, 0, 0, 0, etLocation))
		assert.Equal(t, 0.0464, pOn.GenerationCreditDollarsPerKWH)

		// Summer Off-Peak (other)
		pOff := getPriceForTime(periods, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation))
		assert.Equal(t, 0.0401, pOff.GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_progress_sc export rates (Schedule PP SCG with integration charge)", func(t *testing.T) {
		periods := dukeProgressSCPeriods("duke_progress_sc_res", types.UtilityRateOptions{NetMeteringScheme: "scg"}, []int{2026})

		// June weekday (June 15, 2026 - Monday)
		// Summer Premium Peak (17:00 - 21:00) => 0.0749 - 0.00162 = 0.07328
		pPrem := getPriceForTime(periods, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.True(t, pPrem.SeparateGenerationCredit)
		assert.InDelta(t, 0.07328, pPrem.GenerationCreditDollarsPerKWH, 1e-6)

		// Summer On-Peak (13:00 - 17:00) => 0.0532 - 0.00162 = 0.05158
		pOn := getPriceForTime(periods, time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		assert.InDelta(t, 0.05158, pOn.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("duke_indiana_rs_tou rate and export options", func(t *testing.T) {
		// RS TOU - EDG export
		periods := dukeIndianaPeriods("duke_indiana_rs_tou", types.UtilityRateOptions{NetMeteringScheme: "edg"}, []int{2026})

		// Summer Weekday (June 15, 2026 - Monday)
		// Discount: 12 am - 4 am
		pDisc := getPriceForTime(periods, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation))
		assert.Equal(t, 0.085679, pDisc.DollarsPerKWH)

		// Summer On-Peak: 5 pm - 9 pm (17:00 - 21:00)
		pOn := getPriceForTime(periods, time.Date(2026, time.June, 15, 18, 0, 0, 0, etLocation))
		assert.Equal(t, 0.214198, pOn.DollarsPerKWH)

		// Summer Off-Peak: other
		pOff := getPriceForTime(periods, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation))
		assert.Equal(t, 0.142799, pOff.DollarsPerKWH)

		// Winter Weekday (Dec 15, 2026 - Tuesday)
		// Winter On-Peak: 6 am - 8 am and 5 pm - 9 pm
		pWinOn1 := getPriceForTime(periods, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation))
		assert.Equal(t, 0.214198, pWinOn1.DollarsPerKWH)
		pWinOn2 := getPriceForTime(periods, time.Date(2026, time.December, 15, 18, 0, 0, 0, etLocation))
		assert.Equal(t, 0.214198, pWinOn2.DollarsPerKWH)

		// Weekend/Holiday (Christmas Day - Dec 25, 2026 - Friday)
		// No on-peak: 6 pm is off-peak
		pHolOff := getPriceForTime(periods, time.Date(2026, time.December, 25, 18, 0, 0, 0, etLocation))
		assert.Equal(t, 0.142799, pHolOff.DollarsPerKWH)

		// Export rate check (EDG)
		pExp := getPriceForTime(periods, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pExp.SeparateGenerationCredit)
		assert.Equal(t, 0.055143, pExp.GenerationCreditDollarsPerKWH)

		// RS TOU - Net Metering 1:1 option
		periodsNet := dukeIndianaPeriods("duke_indiana_rs_tou", types.UtilityRateOptions{NetMeteringScheme: "net"}, []int{2026})
		pNet := getPriceForTime(periodsNet, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.False(t, pNet.SeparateGenerationCredit)
		assert.Equal(t, 0.0, pNet.GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_florida standard and TOU rates", func(t *testing.T) {
		// RS-1
		periodsRS := dukeFloridaPeriods("duke_florida_rs", types.UtilityRateOptions{}, []int{2026})

		// July 15, 2026 (Non-Winter): 7.686 c/kWh = 0.07686
		pRSNonWinter := getPriceForTime(periodsRS, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		assert.InDelta(t, 0.07686, pRSNonWinter.DollarsPerKWH, 1e-6)

		// January 15, 2026 (Winter): 8.754 c/kWh = 0.08754
		pRSWinter := getPriceForTime(periodsRS, time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		assert.InDelta(t, 0.08754, pRSWinter.DollarsPerKWH, 1e-6)

		// RST-1 TOU
		periodsRST := dukeFloridaPeriods("duke_florida_rst", types.UtilityRateOptions{}, []int{2026})

		// Winter Weekday (Tuesday Dec 15, 2026)
		// 2 AM (Discount): 4.984 c/kWh = 0.04984
		assert.InDelta(t, 0.04984, getPriceForTime(periodsRST, time.Date(2026, time.December, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
		// 7 AM (On-Peak): 11.090 c/kWh = 0.11090
		assert.InDelta(t, 0.11090, getPriceForTime(periodsRST, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
		// 12 PM (Off-Peak): 8.215 c/kWh = 0.08215
		assert.InDelta(t, 0.08215, getPriceForTime(periodsRST, time.Date(2026, time.December, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)

		// Winter Weekend (Saturday Dec 12, 2026)
		// 2 AM (Discount): 4.984 c/kWh = 0.04984
		assert.InDelta(t, 0.04984, getPriceForTime(periodsRST, time.Date(2026, time.December, 12, 2, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
		// 7 AM (Off-Peak): 8.215 c/kWh = 0.08215
		assert.InDelta(t, 0.08215, getPriceForTime(periodsRST, time.Date(2026, time.December, 12, 7, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)

		// Winter Holiday (Christmas Day Dec 25, 2026 - Friday)
		// 2 AM (Discount): 4.984 c/kWh = 0.04984
		assert.InDelta(t, 0.04984, getPriceForTime(periodsRST, time.Date(2026, time.December, 25, 2, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
		// 7 AM (Off-Peak): 8.215 c/kWh = 0.08215
		assert.InDelta(t, 0.08215, getPriceForTime(periodsRST, time.Date(2026, time.December, 25, 7, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)

		// Non-Winter Weekday (Tuesday June 15, 2026)
		// 2 AM (Discount): 4.984 c/kWh = 0.04984
		assert.InDelta(t, 0.04984, getPriceForTime(periodsRST, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
		// 7 PM (On-Peak): 11.090 c/kWh = 0.11090
		assert.InDelta(t, 0.11090, getPriceForTime(periodsRST, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
		// 12 PM (Off-Peak): 8.215 c/kWh = 0.08215
		assert.InDelta(t, 0.08215, getPriceForTime(periodsRST, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH, 1e-6)
	})

	t.Run("Applying Settings via genericTOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "duke_carolinas_nc",
			UtilityRate:     "duke_carolinas_nc_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rsc",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "duke_carolinas_nc_rs", u.Name())

		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.122603, p.DollarsPerKWH)
		assert.True(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0453, p.GenerationCreditDollarsPerKWH)
	})
}
