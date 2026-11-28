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

	t.Run("duke_carolinas_nc_rs and re rates and Sep 2026 transition", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{}, []int{2026, 2027})
		// Pre-Sep 2026 baseline
		assert.Equal(t, 0.122603, getPriceForTime(periods, time.Date(2026, time.August, 31, 23, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Sep 1, 2026 transition (+$0.021064 riders) and 2027
		assert.Equal(t, 0.143667, getPriceForTime(periods, time.Date(2026, time.September, 1, 0, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.143667, getPriceForTime(periods, time.Date(2027, time.March, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		periodsRE := dukeCarolinasNCPeriods("duke_carolinas_nc_re", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.117845, getPriceForTime(periodsRE, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.138909, getPriceForTime(periodsRE, time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.138909, getPriceForTime(periodsRE, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
	})

	t.Run("GetPeriods TOU and non-TOU for Duke", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rt", types.UtilityRateOptions{}, []int{2026})
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])
	})

	t.Run("duke_carolinas_nc_rt seasons, hours, and Sep 2026 transition", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rt", types.UtilityRateOptions{}, []int{2026, 2027})

		// Summer Weekday pre-Sep 2026 (June 15, 2026 - Monday)
		assert.Equal(t, 0.171204, getPriceForTime(periods, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.053929, getPriceForTime(periods, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.078411, getPriceForTime(periods, time.Date(2026, time.June, 15, 13, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.078411, getPriceForTime(periods, time.Date(2026, time.June, 14, 19, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Winter Weekday pre-Sep 2026 (Jan 15, 2026 - Thursday)
		assert.Equal(t, 0.171204, getPriceForTime(periods, time.Date(2026, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Post-Sep 1, 2026 Summer & Winter (+$0.021064 riders)
		assert.Equal(t, 0.192268, getPriceForTime(periods, time.Date(2026, time.September, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.074993, getPriceForTime(periods, time.Date(2026, time.September, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.099475, getPriceForTime(periods, time.Date(2026, time.September, 15, 13, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.192268, getPriceForTime(periods, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.192268, getPriceForTime(periods, time.Date(2027, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
	})

	t.Run("duke_carolinas_nc_rt_ev, rstc, and retc transitions", func(t *testing.T) {
		periods := dukeCarolinasNCPeriods("duke_carolinas_nc_rt_ev", types.UtilityRateOptions{}, []int{2026, 2027})
		// Pre-Sep 2026
		assert.Equal(t, 0.061752, getPriceForTime(periods, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.123504, getPriceForTime(periods, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Post-Sep 2026 & 2027
		assert.Equal(t, 0.082816, getPriceForTime(periods, time.Date(2026, time.October, 15, 23, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.144568, getPriceForTime(periods, time.Date(2027, time.February, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		periodsRSTC := dukeCarolinasNCPeriods("duke_carolinas_nc_rstc", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.234984, getPriceForTime(periodsRSTC, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.256048, getPriceForTime(periodsRSTC, time.Date(2026, time.September, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)

		periodsRETC := dukeCarolinasNCPeriods("duke_carolinas_nc_retc", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.213412, getPriceForTime(periodsRETC, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.234476, getPriceForTime(periodsRETC, time.Date(2026, time.September, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
	})

	t.Run("duke_carolinas_nc export credits", func(t *testing.T) {
		// RSC option
		periodsRSC := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{NetMeteringScheme: "rsc"}, []int{2026})
		p := getPriceForTime(periodsRSC, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0453, p.GenerationCreditDollarsPerKWH)

		// NMB option
		periodsNMB := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{NetMeteringScheme: "nmb"}, []int{2026})
		pNMB := getPriceForTime(periodsNMB, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pNMB.SeparateGenerationCredit)
		assert.Equal(t, 0.0453, pNMB.GenerationCreditDollarsPerKWH)

		// SCG option
		periodsSCG := dukeCarolinasNCPeriods("duke_carolinas_nc_rs", types.UtilityRateOptions{NetMeteringScheme: "scg"}, []int{2026})
		pPrem := getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.True(t, pPrem.SeparateGenerationCredit)
		assert.Equal(t, 0.0615, pPrem.GenerationCreditDollarsPerKWH)
		assert.Equal(t, 0.0479, getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH)
		assert.Equal(t, 0.0414, getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_carolinas_sc transitions, new rt_ev schedule, and export schemes", func(t *testing.T) {
		// RS transitions: pre-Aug (0.138125), Aug (+0.002504), Sep (+0.007174), Nov & 2027 (+0.012973)
		periodsRS := dukeCarolinasSCPeriods("duke_carolinas_sc_rs", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.138125, getPriceForTime(periodsRS, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.140629, getPriceForTime(periodsRS, time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.145299, getPriceForTime(periodsRS, time.Date(2026, time.October, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.151098, getPriceForTime(periodsRS, time.Date(2026, time.November, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.151098, getPriceForTime(periodsRS, time.Date(2027, time.March, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// New schedule: duke_carolinas_sc_rt_ev (Leaf 12: 23:00-05:00 Super Off-Peak, Off-Peak other)
		periodsEV := dukeCarolinasSCPeriods("duke_carolinas_sc_rt_ev", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.100753, getPriceForTime(periodsEV, time.Date(2026, time.July, 15, 23, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.149616, getPriceForTime(periodsEV, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.103257, getPriceForTime(periodsEV, time.Date(2026, time.August, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.107927, getPriceForTime(periodsEV, time.Date(2026, time.September, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.113726, getPriceForTime(periodsEV, time.Date(2026, time.November, 15, 4, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.162589, getPriceForTime(periodsEV, time.Date(2026, time.November, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.113726, getPriceForTime(periodsEV, time.Date(2027, time.January, 1, 1, 0, 0, 0, etLocation)).DollarsPerKWH)

		// R-STOU seasons, holidays, and Nov 2026 transition
		periodsSTOU := dukeCarolinasSCPeriods("duke_carolinas_sc_r_stou", types.UtilityRateOptions{}, []int{2026, 2027})
		// Non-winter weekday pre-Aug (June 15, 2026 - Monday)
		assert.Equal(t, 0.209021, getPriceForTime(periodsSTOU, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.093782, getPriceForTime(periodsSTOU, time.Date(2026, time.June, 15, 3, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.128191, getPriceForTime(periodsSTOU, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Winter weekday pre-Aug (Jan 15, 2026 - Thursday)
		assert.Equal(t, 0.209021, getPriceForTime(periodsSTOU, time.Date(2026, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.128191, getPriceForTime(periodsSTOU, time.Date(2026, time.January, 15, 3, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Winter weekday post-Nov 2026 (Dec 15, 2026 - Tuesday, +$0.012973)
		assert.Equal(t, 0.221994, getPriceForTime(periodsSTOU, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.221994, getPriceForTime(periodsSTOU, time.Date(2026, time.December, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.141164, getPriceForTime(periodsSTOU, time.Date(2026, time.December, 15, 3, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Winter holiday post-Nov 2026 (Christmas Dec 25, 2026 - Friday)
		assert.Equal(t, 0.141164, getPriceForTime(periodsSTOU, time.Date(2026, time.December, 25, 19, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Exports: RSC ($0.0419), NMT ($0.0419), RNM (1:1 net metering)
		pRSC := getPriceForTime(dukeCarolinasSCPeriods("duke_carolinas_sc_rs", types.UtilityRateOptions{NetMeteringScheme: "rsc"}, []int{2026}), time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pRSC.SeparateGenerationCredit)
		assert.Equal(t, 0.0419, pRSC.GenerationCreditDollarsPerKWH)

		pNMT := getPriceForTime(dukeCarolinasSCPeriods("duke_carolinas_sc_rs", types.UtilityRateOptions{NetMeteringScheme: "nmt"}, []int{2026}), time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pNMT.SeparateGenerationCredit)
		assert.Equal(t, 0.0419, pNMT.GenerationCreditDollarsPerKWH)

		pRNM := getPriceForTime(dukeCarolinasSCPeriods("duke_carolinas_sc_rs", types.UtilityRateOptions{NetMeteringScheme: "rnm"}, []int{2026}), time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.False(t, pRNM.SeparateGenerationCredit)
		assert.Equal(t, 0.0, pRNM.GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_progress_nc transitions, r_toud and r_tou_ev schedules, and export rates", func(t *testing.T) {
		// RES transitions: pre-Jun (0.12623), Jun (+0.03144 => 0.15767), Jul & 2027 (+0.03481 => 0.16104)
		periodsRES := dukeProgressNCPeriods("duke_progress_nc_res", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.12623, getPriceForTime(periodsRES, time.Date(2026, time.May, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.15767, getPriceForTime(periodsRES, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.16104, getPriceForTime(periodsRES, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.16104, getPriceForTime(periodsRES, time.Date(2027, time.February, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// New schedule: duke_progress_nc_r_toud (Leaf 501)
		periodsTOUD := dukeProgressNCPeriods("duke_progress_nc_r_toud", types.UtilityRateOptions{}, []int{2026, 2027})
		// Pre-Jun 2026 Summer Weekday (May 18, 2026 - Monday) & Memorial Day Holiday (May 25, 2026)
		assert.Equal(t, 0.15638, getPriceForTime(periodsTOUD, time.Date(2026, time.May, 18, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.06633, getPriceForTime(periodsTOUD, time.Date(2026, time.May, 18, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.04347, getPriceForTime(periodsTOUD, time.Date(2026, time.May, 18, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.06633, getPriceForTime(periodsTOUD, time.Date(2026, time.May, 25, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Post-Jul 2026 Summer & Winter (+0.03481)
		assert.Equal(t, 0.19119, getPriceForTime(periodsTOUD, time.Date(2026, time.July, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.10114, getPriceForTime(periodsTOUD, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.07828, getPriceForTime(periodsTOUD, time.Date(2026, time.July, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.19119, getPriceForTime(periodsTOUD, time.Date(2027, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.07828, getPriceForTime(periodsTOUD, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// New schedule: duke_progress_nc_r_tou_ev (Leaf 504)
		periodsEV := dukeProgressNCPeriods("duke_progress_nc_r_tou_ev", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.06548, getPriceForTime(periodsEV, time.Date(2026, time.May, 15, 23, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.13096, getPriceForTime(periodsEV, time.Date(2026, time.May, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.10029, getPriceForTime(periodsEV, time.Date(2026, time.August, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.16577, getPriceForTime(periodsEV, time.Date(2027, time.March, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Exports: RSC ($0.0394), NMB ($0.0394), SCG (Schedule PP)
		pRSC := getPriceForTime(dukeProgressNCPeriods("duke_progress_nc_res", types.UtilityRateOptions{NetMeteringScheme: "rsc"}, []int{2026}), time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pRSC.SeparateGenerationCredit)
		assert.Equal(t, 0.0394, pRSC.GenerationCreditDollarsPerKWH)

		pNMB := getPriceForTime(dukeProgressNCPeriods("duke_progress_nc_res", types.UtilityRateOptions{NetMeteringScheme: "nmb"}, []int{2026}), time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pNMB.SeparateGenerationCredit)
		assert.Equal(t, 0.0394, pNMB.GenerationCreditDollarsPerKWH)

		periodsSCG := dukeProgressNCPeriods("duke_progress_nc_res", types.UtilityRateOptions{NetMeteringScheme: "scg"}, []int{2026})
		pPrem := getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.True(t, pPrem.SeparateGenerationCredit)
		assert.Equal(t, 0.0597, pPrem.GenerationCreditDollarsPerKWH)
		assert.Equal(t, 0.0464, getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 15, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH)
		assert.Equal(t, 0.0401, getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_progress_sc transitions, r_stou/r_toud/r_tou_cpp schedules, and export rates", func(t *testing.T) {
		// RES transitions: pre-Aug (0.14320), Aug (+0.00964 => 0.15284), Sep & 2027 (+0.01422 => 0.15742)
		periodsRES := dukeProgressSCPeriods("duke_progress_sc_res", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.14320, getPriceForTime(periodsRES, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.15284, getPriceForTime(periodsRES, time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.15742, getPriceForTime(periodsRES, time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.15742, getPriceForTime(periodsRES, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// New schedule: duke_progress_sc_r_stou (Leaf 502)
		periodsSTOU := dukeProgressSCPeriods("duke_progress_sc_r_stou", types.UtilityRateOptions{}, []int{2026, 2027})
		// Pre-Aug Non-Winter Weekday (June 15, 2026)
		assert.Equal(t, 0.20415, getPriceForTime(periodsSTOU, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.12955, getPriceForTime(periodsSTOU, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.09959, getPriceForTime(periodsSTOU, time.Date(2026, time.June, 15, 3, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Sep 2026 Non-Winter Weekday & Labor Day Holiday (Sep 7, 2026)
		assert.Equal(t, 0.21837, getPriceForTime(periodsSTOU, time.Date(2026, time.September, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14377, getPriceForTime(periodsSTOU, time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.11381, getPriceForTime(periodsSTOU, time.Date(2026, time.September, 15, 3, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14377, getPriceForTime(periodsSTOU, time.Date(2026, time.September, 7, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.11381, getPriceForTime(periodsSTOU, time.Date(2026, time.September, 7, 3, 0, 0, 0, etLocation)).DollarsPerKWH)
		// Winter Weekday (Dec 15, 2026 & Jan 15, 2027): 06:00-09:00 & 18:00-21:00 On-Peak, Off-Peak overnight
		assert.Equal(t, 0.21837, getPriceForTime(periodsSTOU, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.21837, getPriceForTime(periodsSTOU, time.Date(2027, time.January, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14377, getPriceForTime(periodsSTOU, time.Date(2026, time.December, 15, 3, 0, 0, 0, etLocation)).DollarsPerKWH)

		// New schedule: duke_progress_sc_r_toud (Leaf 501)
		periodsTOUD := dukeProgressSCPeriods("duke_progress_sc_r_toud", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.19773, getPriceForTime(periodsTOUD, time.Date(2026, time.July, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.21195, getPriceForTime(periodsTOUD, time.Date(2026, time.September, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.10362, getPriceForTime(periodsTOUD, time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.07561, getPriceForTime(periodsTOUD, time.Date(2026, time.September, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.21195, getPriceForTime(periodsTOUD, time.Date(2027, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)

		// New schedule: duke_progress_sc_r_tou_cpp (Leaf 503)
		periodsCPP := dukeProgressSCPeriods("duke_progress_sc_r_tou_cpp", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.28851, getPriceForTime(periodsCPP, time.Date(2026, time.July, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.29815, getPriceForTime(periodsCPP, time.Date(2026, time.August, 17, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.13636, getPriceForTime(periodsCPP, time.Date(2026, time.August, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.30273, getPriceForTime(periodsCPP, time.Date(2026, time.September, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14094, getPriceForTime(periodsCPP, time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.10065, getPriceForTime(periodsCPP, time.Date(2026, time.September, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Exports: RSC ($0.0401) and SCG Schedule PP (incl. Shoulder Off-Peak $0.03208)
		pRSC := getPriceForTime(dukeProgressSCPeriods("duke_progress_sc_res", types.UtilityRateOptions{NetMeteringScheme: "rsc"}, []int{2026}), time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pRSC.SeparateGenerationCredit)
		assert.Equal(t, 0.0401, pRSC.GenerationCreditDollarsPerKWH)

		periodsSCG := dukeProgressSCPeriods("duke_progress_sc_res", types.UtilityRateOptions{NetMeteringScheme: "scg"}, []int{2026})
		pPrem := getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		assert.True(t, pPrem.SeparateGenerationCredit)
		assert.InDelta(t, 0.07328, pPrem.GenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.05158, getPriceForTime(periodsSCG, time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH, 1e-6)
		// Shoulder Off-Peak (April 15, 2026 at 12:00) => 0.0337 - 0.00162 = 0.03208
		assert.InDelta(t, 0.03208, getPriceForTime(periodsSCG, time.Date(2026, time.April, 15, 12, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH, 1e-6)
		// Shoulder On-Peak (April 15, 2026 at 07:00) => 0.0426 - 0.00162 = 0.04098
		assert.InDelta(t, 0.04098, getPriceForTime(periodsSCG, time.Date(2026, time.April, 15, 7, 0, 0, 0, etLocation)).GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("duke_indiana rs and rs_tou Nov 2026 tracker transition and exports", func(t *testing.T) {
		periodsRS := dukeIndianaPeriods("duke_indiana_rs", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.Equal(t, 0.186556, getPriceForTime(periodsRS, time.Date(2026, time.October, 31, 23, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.192628, getPriceForTime(periodsRS, time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.192628, getPriceForTime(periodsRS, time.Date(2027, time.May, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// RS TOU - EDG export
		periods := dukeIndianaPeriods("duke_indiana_rs_tou", types.UtilityRateOptions{NetMeteringScheme: "edg"}, []int{2026, 2027})

		// Pre-Nov 2026 Summer Weekday (June 15, 2026 - Monday) & Winter Weekday (Jan 15, 2026 - Thursday)
		assert.Equal(t, 0.085679, getPriceForTime(periods, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.214198, getPriceForTime(periods, time.Date(2026, time.June, 15, 18, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.142799, getPriceForTime(periods, time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.214198, getPriceForTime(periods, time.Date(2026, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Post-Nov 1, 2026 Winter Weekday (Dec 15, 2026 - Tuesday, +$0.006072) & Holiday (Dec 25, 2026)
		assert.Equal(t, 0.091751, getPriceForTime(periods, time.Date(2026, time.December, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.220270, getPriceForTime(periods, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.220270, getPriceForTime(periods, time.Date(2026, time.December, 15, 18, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.148871, getPriceForTime(periods, time.Date(2026, time.December, 25, 18, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.220270, getPriceForTime(periods, time.Date(2027, time.June, 15, 18, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Export rate check (EDG & 1:1 Net Metering)
		pExp := getPriceForTime(periods, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.True(t, pExp.SeparateGenerationCredit)
		assert.Equal(t, 0.055143, pExp.GenerationCreditDollarsPerKWH)

		periodsNet := dukeIndianaPeriods("duke_indiana_rs_tou", types.UtilityRateOptions{NetMeteringScheme: "net"}, []int{2026})
		pNet := getPriceForTime(periodsNet, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		assert.False(t, pNet.SeparateGenerationCredit)
		assert.Equal(t, 0.0, pNet.GenerationCreditDollarsPerKWH)
	})

	t.Run("duke_florida standard and TOU rates across 2025, Q4 2026, and 2027-2028", func(t *testing.T) {
		periodsRS := dukeFloridaPeriods("duke_florida_rs", types.UtilityRateOptions{}, []int{2025, 2026, 2027, 2028})

		// 2025 & Pre-October 2026 baseline preserved
		assert.Equal(t, 0.087540, getPriceForTime(periodsRS, time.Date(2025, time.December, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.087540, getPriceForTime(periodsRS, time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.076860, getPriceForTime(periodsRS, time.Date(2026, time.September, 30, 23, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Q4 2026 (Oct 1, 2026 SoBRA + 2026 BA-1 $0.05860)
		assert.Equal(t, 0.13591, getPriceForTime(periodsRS, time.Date(2026, time.October, 1, 0, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14665, getPriceForTime(periodsRS, time.Date(2026, time.December, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// 2027-2028 (2027 BA-1 $0.05767)
		assert.Equal(t, 0.14572, getPriceForTime(periodsRS, time.Date(2027, time.January, 1, 0, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.13498, getPriceForTime(periodsRS, time.Date(2027, time.July, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14572, getPriceForTime(periodsRS, time.Date(2028, time.December, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// RST-1 TOU
		periodsRST := dukeFloridaPeriods("duke_florida_rst", types.UtilityRateOptions{}, []int{2025, 2026, 2027, 2028})

		// Pre-Oct 2026 Winter (Jan 15, 2026 - Thursday) & Non-Winter (Jun 15, 2026 - Monday)
		assert.Equal(t, 0.04984, getPriceForTime(periodsRST, time.Date(2026, time.January, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.11090, getPriceForTime(periodsRST, time.Date(2026, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.08215, getPriceForTime(periodsRST, time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.04984, getPriceForTime(periodsRST, time.Date(2026, time.June, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.11090, getPriceForTime(periodsRST, time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.08215, getPriceForTime(periodsRST, time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)

		// Q4 2026 Non-Winter Weekday (Oct 15, 2026 - Thursday) & Winter Weekday/Weekend/Holiday (Dec 2026)
		assert.Equal(t, 0.10801, getPriceForTime(periodsRST, time.Date(2026, time.October, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.17925, getPriceForTime(periodsRST, time.Date(2026, time.October, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14383, getPriceForTime(periodsRST, time.Date(2026, time.October, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.10801, getPriceForTime(periodsRST, time.Date(2026, time.December, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.17925, getPriceForTime(periodsRST, time.Date(2026, time.December, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14383, getPriceForTime(periodsRST, time.Date(2026, time.December, 12, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14383, getPriceForTime(periodsRST, time.Date(2026, time.December, 25, 7, 0, 0, 0, etLocation)).DollarsPerKWH)

		// 2027-2028 Winter Weekday (Jan 15, 2027 - Friday) & Non-Winter Weekday (Jun 15, 2028 - Thursday)
		assert.Equal(t, 0.10264, getPriceForTime(periodsRST, time.Date(2027, time.January, 15, 2, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.18516, getPriceForTime(periodsRST, time.Date(2027, time.January, 15, 7, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.14263, getPriceForTime(periodsRST, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)).DollarsPerKWH)
		assert.Equal(t, 0.18516, getPriceForTime(periodsRST, time.Date(2028, time.June, 15, 19, 0, 0, 0, etLocation)).DollarsPerKWH)
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

		pSep, err := u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, 0.143667, pSep.DollarsPerKWH)
	})
}
