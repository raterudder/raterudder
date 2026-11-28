package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPSHolidays(t *testing.T) {
	t.Run("Cesar Chavez Day shifting", func(t *testing.T) {
		// March 31, 2026 is Tuesday -> remains March 31
		holidays2026 := getAPSHolidays(2026)
		assert.Contains(t, holidays2026, "2026-03-31")

		// March 31, 2029 is Saturday -> observed Friday March 30
		holidays2029 := getAPSHolidays(2029)
		assert.Contains(t, holidays2029, "2029-03-30")
		assert.NotContains(t, holidays2029, "2029-03-31")

		// March 31, 2030 is Sunday -> observed Monday April 1
		holidays2030 := getAPSHolidays(2030)
		assert.Contains(t, holidays2030, "2030-04-01")
		assert.NotContains(t, holidays2030, "2030-03-31")
	})

	t.Run("Christmas Eve and New Year's Eve do not shift, but Christmas Day shifts", func(t *testing.T) {
		// In 2033:
		// - Dec 24 (Christmas Eve) is Saturday -> remains Saturday Dec 24
		// - Dec 25 (Christmas Day) is Sunday -> shifts to Monday Dec 26
		holidays2033 := getAPSHolidays(2033)
		assert.Contains(t, holidays2033, "2033-12-24")
		assert.Contains(t, holidays2033, "2033-12-26")
		assert.NotContains(t, holidays2033, "2033-12-25")

		// Dec 31, 2033 is Saturday -> remains Saturday Dec 31
		assert.Contains(t, holidays2033, "2033-12-31")
	})
}

func TestAPSRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("Schedule R-1 Flat rates across 2026 and 2027 ACC adjustor windows", func(t *testing.T) {
		// XS / Small default ($0.12925 base + two-part adjustors)
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_1",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "small",
			},
		})
		require.NoError(t, err)

		// Jan 2026: $0.12925 + $0.023066 = $0.152316
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12925+0.023066, p.DollarsPerKWH, 1e-6)

		// Feb-May 2026: $0.12925 + $0.026066 = $0.155316
		p, err = u.priceForTime(time.Date(2026, time.March, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12925+0.026066, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.May, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12925+0.026066, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 & 2027: $0.12925 + $0.027029 = $0.156279
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12925+0.027029, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12925+0.027029, p.DollarsPerKWH, 1e-6)

		// Medium ($0.14052 base)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_1",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "medium",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14052+0.023066, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.April, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14052+0.026066, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14052+0.027029, p.DollarsPerKWH, 1e-6)

		// Large ($0.15418 base)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_1",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "large",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15418+0.023066, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.May, 20, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15418+0.026066, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15418+0.027029, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Schedule TOU-E rates across Jan, Feb-May, and post-June windows", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
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

		// Jan 2026 Winter weekday On-Peak (Wed, Jan 14, 2026, 5:00 PM) -> $0.32543 + $0.023066
		p, err := u.priceForTime(time.Date(2026, time.January, 14, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32543+0.023066, p.DollarsPerKWH, 1e-6)

		// Jan 2026 Winter weekday Super Off-Peak (Wed, Jan 14, 2026, 12:00 PM) -> $0.03495 + $0.023066
		p, err = u.priceForTime(time.Date(2026, time.January, 14, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.03495+0.023066, p.DollarsPerKWH, 1e-6)

		// Feb-Apr 2026 Winter weekday On-Peak (Wed, Mar 18, 2026, 5:00 PM) -> $0.32543 + $0.026066
		p, err = u.priceForTime(time.Date(2026, time.March, 18, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32543+0.026066, p.DollarsPerKWH, 1e-6)

		// May 2026 Summer weekday On-Peak with pre-June TCA-1 (Wed, May 13, 2026, 5:00 PM) -> $0.34396 + $0.026066
		p, err = u.priceForTime(time.Date(2026, time.May, 13, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.34396+0.026066, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Summer weekday On-Peak (Wed, July 15, 2026, 5:00 PM) -> $0.34396 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.34396+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Summer weekday Off-Peak (Wed, July 15, 2026, 9:00 AM) -> $0.12345 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 9, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday On-Peak (Wed, Dec 16, 2026, 5:00 PM) -> $0.32543 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32543+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday Super Off-Peak (Wed, Dec 16, 2026, 12:00 PM) -> $0.03495 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.03495+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday Off-Peak (Wed, Dec 16, 2026, 8:00 AM) -> $0.12351 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 8, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12351+0.027029, p.DollarsPerKWH, 1e-6)

		// Summer Weekend (Sat, July 18, 2026, 5:00 PM) -> Off-Peak $0.12345 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345+0.027029, p.DollarsPerKWH, 1e-6)

		// Summer Holiday (Labor Day Monday, Sep 7, 2026, 5:00 PM) -> Off-Peak $0.12345 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345+0.027029, p.DollarsPerKWH, 1e-6)

		// 2027 Summer weekday On-Peak (Wed, July 14, 2027, 5:00 PM) -> $0.34396 + $0.027029
		p, err = u.priceForTime(time.Date(2027, time.July, 14, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.34396+0.027029, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule R-3 three-part rates exclude per-kWh LFCR", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_3",
		})
		require.NoError(t, err)

		// Jan 2026 Winter weekday On-Peak (Wed, Jan 14, 2026, 5:00 PM) -> $0.09932 + $0.020606
		p, err := u.priceForTime(time.Date(2026, time.January, 14, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09932+0.020606, p.DollarsPerKWH, 1e-6)

		// Feb-Apr 2026 Winter weekday Off-Peak (Wed, Mar 18, 2026, 8:00 AM) -> $0.05938 + $0.023606
		p, err = u.priceForTime(time.Date(2026, time.March, 18, 8, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.05938+0.023606, p.DollarsPerKWH, 1e-6)

		// May 2026 Summer weekday On-Peak with pre-June TCA-1 (Wed, May 13, 2026, 5:00 PM) -> $0.14227 + $0.023606
		p, err = u.priceForTime(time.Date(2026, time.May, 13, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14227+0.023606, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Summer weekday On-Peak (Wed, July 15, 2026, 5:00 PM) -> $0.14227 + $0.024569
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14227+0.024569, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Summer weekday Off-Peak (Wed, July 15, 2026, 9:00 AM) -> $0.05943 + $0.024569
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 9, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.05943+0.024569, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday On-Peak (Wed, Dec 16, 2026, 5:00 PM) -> $0.09932 + $0.024569
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09932+0.024569, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday Super Off-Peak (Wed, Dec 16, 2026, 12:00 PM) -> $0.03495 + $0.024569
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.03495+0.024569, p.DollarsPerKWH, 1e-6)

		// 2027 Winter weekday Off-Peak (Wed, Jan 20, 2027, 8:00 AM) -> $0.05938 + $0.024569
		p, err = u.priceForTime(time.Date(2027, time.January, 20, 8, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.05938+0.024569, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule R-EV rates across Jan, Feb-May, and post-June windows", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_r_ev",
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
		assert.True(t, names["Overnight"])
		assert.True(t, names["Super Off-Peak"])

		// Jan 2026 Winter weekday Overnight (Wed, Jan 14, 2026, 2:00 AM) -> $0.08468 + $0.023066
		p, err := u.priceForTime(time.Date(2026, time.January, 14, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08468+0.023066, p.DollarsPerKWH, 1e-6)

		// Feb-Apr 2026 Winter weekday On-Peak (Wed, Mar 18, 2026, 5:00 PM) -> $0.34820 + $0.026066
		p, err = u.priceForTime(time.Date(2026, time.March, 18, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.34820+0.026066, p.DollarsPerKWH, 1e-6)

		// May 2026 Summer weekday On-Peak with pre-June TCA-1 (Wed, May 13, 2026, 5:00 PM) -> $0.36824 + $0.026066
		p, err = u.priceForTime(time.Date(2026, time.May, 13, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.36824+0.026066, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Summer weekday On-Peak (Wed, July 15, 2026, 5:00 PM) -> $0.36824 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.36824+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Summer weekday Overnight (Wed, July 15, 2026, 2:00 AM) -> $0.08468 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08468+0.027029, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Overnight (Sat, July 18, 2026, 2:00 AM) -> Off-Peak $0.12345 + $0.027029 (Overnight is weekdays only)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12345+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday Super Off-Peak (Wed, Dec 16, 2026, 12:00 PM) -> $0.03495 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.03495+0.027029, p.DollarsPerKWH, 1e-6)

		// Post-June 2026 Winter weekday Overnight (Wed, Dec 16, 2026, 2:00 AM) -> $0.08468 + $0.027029
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 2, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08468+0.027029, p.DollarsPerKWH, 1e-6)
	})
}

func TestAPSExportRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("Default RCP Net Billing steps down on 2026-09-01", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rcp",
			},
		})
		require.NoError(t, err)

		// Jan-Aug 2026: $0.06171/kWh
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.06171, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.August, 31, 23, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.06171, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Effective 2026-09-01 through 2027: Tranche 2026 $0.05554/kWh
		p, err = u.priceForTime(time.Date(2026, time.September, 1, 0, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.05554, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.05554, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Empty netMeteringScheme defaults to RCP transition", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.06171, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.05554, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Locked 10-year RCP cohorts rcp_2026 through rcp_2017", func(t *testing.T) {
		info := apsUtilityInfo()
		require.NotEmpty(t, info.Rates)
		var choiceValues []string
		var defaultValue any
		for _, opt := range info.Rates[1].Options {
			if opt.Field == "netMeteringScheme" {
				defaultValue = opt.Default
				for _, c := range opt.Choices {
					choiceValues = append(choiceValues, c.Value)
				}
			}
		}
		assert.Equal(t, "rcp_2026", defaultValue)
		assert.Equal(t, []string{
			"rcp_2026",
			"rcp_2025",
			"rcp_2024",
			"rcp_2023",
			"rcp_2022",
			"rcp_2021",
			"rcp_2020",
			"rcp_2019",
			"rcp_2018",
			"rcp_2017",
			"net",
		}, choiceValues)

		verifyLockedCohort := func(scheme string, expectedDollarsPerKWH float64) {
			err := u.ApplySettings(context.Background(), types.Settings{
				UtilityProvider: "aps",
				UtilityRate:     "aps_tou_e",
				UtilityRateOptions: types.UtilityRateOptions{
					NetMeteringScheme: scheme,
				},
			})
			require.NoError(t, err)

			for _, ts := range []time.Time{
				time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation),
				time.Date(2026, time.October, 15, 12, 0, 0, 0, mstLocation),
				time.Date(2027, time.October, 15, 12, 0, 0, 0, mstLocation),
			} {
				p, err := u.priceForTime(ts)
				require.NoError(t, err)
				assert.True(t, p.SeparateGenerationCredit, "cohort %s at %v", scheme, ts)
				assert.InDelta(t, expectedDollarsPerKWH, p.GenerationCreditDollarsPerKWH, 1e-6, "cohort %s at %v", scheme, ts)
			}
		}

		t.Run("rcp_2026", func(t *testing.T) { verifyLockedCohort("rcp_2026", 0.05554) })
		t.Run("rcp_2025", func(t *testing.T) { verifyLockedCohort("rcp_2025", 0.06171) })
		t.Run("rcp_2024", func(t *testing.T) { verifyLockedCohort("rcp_2024", 0.06857) })
		t.Run("rcp_2023", func(t *testing.T) { verifyLockedCohort("rcp_2023", 0.07619) })
		t.Run("rcp_2022", func(t *testing.T) { verifyLockedCohort("rcp_2022", 0.08465) })
		t.Run("rcp_2021", func(t *testing.T) { verifyLockedCohort("rcp_2021", 0.09405) })
		t.Run("rcp_2020", func(t *testing.T) { verifyLockedCohort("rcp_2020", 0.10450) })
		t.Run("rcp_2019", func(t *testing.T) { verifyLockedCohort("rcp_2019", 0.10450) })
		t.Run("rcp_2018", func(t *testing.T) { verifyLockedCohort("rcp_2018", 0.11610) })
		t.Run("rcp_2017", func(t *testing.T) { verifyLockedCohort("rcp_2017", 0.12900) })
	})

	t.Run("Net Metering scheme (Legacy EPR-6)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "aps",
			UtilityRate:     "aps_tou_e",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)

		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)
	})
}
