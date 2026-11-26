package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAmerenMOHolidays(t *testing.T) {
	t.Run("2025 observed tariff holidays", func(t *testing.T) {
		holidays2025 := getAmerenMOHolidays(2025)
		assert.Equal(t, []string{
			"2025-01-01", // New Year's Day
			"2025-04-18", // Good Friday
			"2025-05-26", // Memorial Day
			"2025-07-04", // Independence Day
			"2025-09-01", // Labor Day
			"2025-11-27", // Thanksgiving Day
			"2025-11-28", // Thanksgiving Friday
			"2025-12-24", // Christmas Eve Day
			"2025-12-25", // Christmas Day
		}, holidays2025)
	})

	t.Run("2026 observed tariff holidays", func(t *testing.T) {
		// July 4, 2026 is Saturday -> observed Friday July 3, 2026
		holidays2026 := getAmerenMOHolidays(2026)
		assert.Equal(t, []string{
			"2026-01-01", // New Year's Day
			"2026-04-03", // Good Friday
			"2026-05-25", // Memorial Day
			"2026-07-03", // Independence Day (observed)
			"2026-09-07", // Labor Day
			"2026-11-26", // Thanksgiving Day
			"2026-11-27", // Thanksgiving Friday
			"2026-12-24", // Christmas Eve Day
			"2026-12-25", // Christmas Day
		}, holidays2026)
	})

	t.Run("2027 Saturday Christmas and 2028 Saturday New Year shift", func(t *testing.T) {
		// In 2027: July 4 is Sunday -> observed July 5; Dec 25 is Saturday -> Christmas Eve Dec 23, Christmas Day Dec 24;
		// Jan 1, 2028 is Saturday -> observed Friday Dec 31, 2027
		holidays2027 := getAmerenMOHolidays(2027)
		assert.Equal(t, []string{
			"2027-01-01", // New Year's Day
			"2027-03-26", // Good Friday
			"2027-05-31", // Memorial Day
			"2027-07-05", // Independence Day (observed)
			"2027-09-06", // Labor Day
			"2027-11-25", // Thanksgiving Day
			"2027-11-26", // Thanksgiving Friday
			"2027-12-23", // Christmas Eve Day (observed)
			"2027-12-24", // Christmas Day (observed)
			"2027-12-31", // New Year's Day 2028 (observed)
		}, holidays2027)
	})

	t.Run("2028 Sunday Christmas Eve shifts to Friday Dec 22", func(t *testing.T) {
		// In 2028: Jan 1 was observed Dec 31, 2027; Dec 24 is Sunday and Dec 25 is Monday -> Dec 22 and Dec 25
		holidays2028 := getAmerenMOHolidays(2028)
		assert.Equal(t, []string{
			"2028-04-14", // Good Friday
			"2028-05-29", // Memorial Day
			"2028-07-04", // Independence Day
			"2028-09-04", // Labor Day
			"2028-11-23", // Thanksgiving Day
			"2028-11-24", // Thanksgiving Friday
			"2028-12-22", // Christmas Eve Day (observed)
			"2028-12-25", // Christmas Day
		}, holidays2028)
	})
}

func TestAmerenMOPeriods(t *testing.T) {
	u := &genericTOU{}

	t.Run("Evening/Morning Savers (ameren_mo_evening_morning)", func(t *testing.T) {
		netSettings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_evening_morning",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		}
		err := u.ApplySettings(context.Background(), netSettings)
		require.NoError(t, err)

		// Summer On-Peak (9 AM - 9 PM all days, including weekends/holidays):
		// Sat, July 4, 2026 at 2:00 PM -> $0.1524 + $0.0057 + $0.014095 = $0.172195
		pSummerOn, err := u.priceForTime(time.Date(2026, time.July, 4, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1524+0.0057+0.014095, pSummerOn.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", pSummerOn.PeriodName)
		assert.False(t, pSummerOn.SeparateGenerationCredit)

		// Summer Off-Peak (9 PM - 9 AM all days):
		// Wed, July 15, 2026 at 9:00 PM (21:00) -> $0.1524 + $0.014095 = $0.166495
		pSummerOff, err := u.priceForTime(time.Date(2026, time.July, 15, 21, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1524+0.014095, pSummerOff.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", pSummerOff.PeriodName)

		// Under 1:1 monthly net metering ("net"), lowest conservative export credit across Summer On/Off-Peak equals Summer Off-Peak
		assert.InDelta(t, 0.1524+0.014095, pSummerOn.ExportRateDollars(netSettings, []types.Price{pSummerOn, pSummerOff}), 1e-6)

		// Winter On-Peak: Thu, Nov 12, 2026 at 10:00 AM -> $0.1045 + $0.0028 + $0.009995 = $0.117295
		p, err := u.priceForTime(time.Date(2026, time.November, 12, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1045+0.0028+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Off-Peak: Thu, Nov 12, 2026 at 8:00 AM -> $0.1045 + $0.009995 = $0.114495
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1045+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Summer 2027 On-Peak carries $0.009995 rider sum
		p2027, err := u.priceForTime(time.Date(2027, time.July, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1524+0.0057+0.009995, p2027.DollarsPerKWH, 1e-6)

		// Avoided Fuel Cost export scheme (Sheet 171: $0.0384 Summer / $0.0339 Winter)
		avoidedSettings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_evening_morning",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "avoided_cost",
			},
		}
		err = u.ApplySettings(context.Background(), avoidedSettings)
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.November, 12, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1045+0.0028+0.009995, p.DollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0339, p.GenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0339, p.ExportRateDollars(avoidedSettings, []types.Price{p}), 1e-6)

		pSummer, err := u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.True(t, pSummer.SeparateGenerationCredit)
		assert.InDelta(t, 0.0384, pSummer.GenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0384, pSummer.ExportRateDollars(avoidedSettings, []types.Price{pSummer}), 1e-6)
	})

	t.Run("Overnight Savers Sheet 54.10 and 2026-2027 rider transitions (ameren_mo_overnight)", func(t *testing.T) {
		touNetSettings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_overnight",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "tou_net",
			},
		}
		err := u.ApplySettings(context.Background(), touNetSettings)
		require.NoError(t, err)

		// Summer 2026 On-Peak (6 AM - 10 PM all days): Sun, July 19, 2026 at 12:00 PM -> $0.1839 + $0.014095
		pSummerOn, err := u.priceForTime(time.Date(2026, time.July, 19, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1839+0.014095, pSummerOn.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", pSummerOn.PeriodName)

		// Summer 2026 Off-Peak (10 PM - 6 AM all days): Sun, July 19, 2026 at 10:00 PM (22:00) -> $0.0732 + $0.014095
		pSummerOff, err := u.priceForTime(time.Date(2026, time.July, 19, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0732+0.014095, pSummerOff.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", pSummerOff.PeriodName)

		// Under tou_net, On-Peak and Off-Peak export credits match their respective TOU bucket rates
		assert.InDelta(t, 0.1839+0.014095, pSummerOn.ExportRateDollars(touNetSettings, []types.Price{pSummerOn, pSummerOff}), 1e-6)
		assert.InDelta(t, 0.0732+0.014095, pSummerOff.ExportRateDollars(touNetSettings, []types.Price{pSummerOn, pSummerOff}), 1e-6)

		// Jan 2026 Winter Option A: $0.1034 On-Peak / $0.0631 Off-Peak + $0.009681 riders
		pJanOn, err := u.priceForTime(time.Date(2026, time.January, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1034+0.009681, pJanOn.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", pJanOn.PeriodName)

		pJanOff, err := u.priceForTime(time.Date(2026, time.January, 15, 2, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0631+0.009681, pJanOff.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", pJanOff.PeriodName)

		// Feb-Mar 2026 rider window ($0.010455)
		pFebOn, err := u.priceForTime(time.Date(2026, time.February, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1034+0.010455, pFebOn.DollarsPerKWH, 1e-6)

		// Apr-May 2026 rider window ($0.010555)
		pAprOn, err := u.priceForTime(time.Date(2026, time.April, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1034+0.010555, pAprOn.DollarsPerKWH, 1e-6)

		// Oct-Dec 2026 and 2027 rider window ($0.009995)
		pOctOn, err := u.priceForTime(time.Date(2026, time.October, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1034+0.009995, pOctOn.DollarsPerKWH, 1e-6)

		pSummer2027On, err := u.priceForTime(time.Date(2027, time.July, 19, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1839+0.009995, pSummer2027On.DollarsPerKWH, 1e-6)

		pWinter2027Off, err := u.priceForTime(time.Date(2027, time.November, 19, 2, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0631+0.009995, pWinter2027Off.DollarsPerKWH, 1e-6)

		// Winter Option B reverts to Anytime Service in winter ($0.1062/kWh + riders)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_overnight",
			UtilityRateOptions: types.UtilityRateOptions{
				PeakPeriodOption: "option_b",
			},
		})
		require.NoError(t, err)

		pOptBJan, err := u.priceForTime(time.Date(2026, time.January, 15, 2, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.009681, pOptBJan.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Anytime", pOptBJan.PeriodName)

		pOptBOct, err := u.priceForTime(time.Date(2026, time.October, 15, 2, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.009995, pOptBOct.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Anytime", pOptBOct.PeriodName)

		for _, optBRate := range []string{"ameren_mo_overnight", "ameren_mo_smart_saver", "ameren_mo_ultimate_saver"} {
			require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
				UtilityProvider: "ameren",
				UtilityRate:     optBRate,
				UtilityRateOptions: types.UtilityRateOptions{
					PeakPeriodOption: "option_b",
				},
			}))
			pWinter, err := u.priceForTime(time.Date(2026, time.November, 15, 14, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.Equal(t, "Anytime", pWinter.PeriodName, "rate %s winter option_b should have Anytime PeriodName", optBRate)
			pSummer, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, ctLocation))
			require.NoError(t, err)
			assert.NotEmpty(t, pSummer.PeriodName, "rate %s summer option_b should have non-empty PeriodName", optBRate)
		}
	})

	t.Run("Smart Savers TOU (ameren_mo_smart_saver)", func(t *testing.T) {
		settings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_smart_saver",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "tou_net",
			},
		}
		err := u.ApplySettings(context.Background(), settings)
		require.NoError(t, err)

		// Summer weekday On-Peak: Wed, July 15, 2026 at 4:00 PM (16:00) -> $0.4051 + $0.014095
		pOnPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.4051+0.014095, pOnPeak.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", pOnPeak.PeriodName)

		// Summer weekday Intermediate before peak: Wed, July 15, 2026 at 12:00 PM -> $0.1216 + $0.014095
		pInter, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216+0.014095, pInter.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", pInter.PeriodName)

		// Under tou_net (Sheet 171 TOU bucket netting), exporting during On-Peak and Intermediate credits bucket rate + riders
		allPrices := []types.Price{pOnPeak, pInter}
		assert.InDelta(t, 0.4051+0.014095, pOnPeak.ExportRateDollars(settings, allPrices), 1e-6)
		assert.InDelta(t, 0.1216+0.014095, pInter.ExportRateDollars(settings, allPrices), 1e-6)

		// Summer weekday Intermediate after peak: Wed, July 15, 2026 at 8:00 PM (20:00) -> $0.1216 + $0.014095
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 20, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216+0.014095, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Summer Off-Peak: Wed, July 15, 2026 at 11:00 PM (23:00) -> $0.0767 + $0.014095
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0767+0.014095, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Summer observed Independence Day holiday (Fri, July 3, 2026 at 4:00 PM): Intermediate $0.1216 + $0.014095 (no On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216+0.014095, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Summer weekend (Sat, July 18, 2026 at 4:00 PM): Intermediate $0.1216 + $0.014095
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216+0.014095, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Winter Option A weekday morning On-Peak: Thu, Nov 12, 2026 at 7:00 AM -> $0.2169 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2169+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Option A weekday midday Intermediate: Thu, Nov 12, 2026 at 12:00 PM -> $0.0778 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Winter Option A weekday evening On-Peak: Thu, Nov 12, 2026 at 6:00 PM (18:00) -> $0.2169 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 18, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2169+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Option A Off-Peak: Thu, Nov 12, 2026 at 11:00 PM (23:00) -> $0.0635 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0635+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Good Friday holiday (Fri, April 3, 2026 at 7:00 AM): Intermediate $0.0778 + $0.010555 (not On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.April, 3, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778+0.010555, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Christmas Eve holiday (Thu, Dec 24, 2026 at 6:00 PM): Intermediate $0.0778 + $0.009995 (not On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.December, 24, 18, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// 2027 observed New Year's Day 2028 shift (Fri, Dec 31, 2027 at 6:00 PM): Intermediate $0.0778 + $0.009995
		p, err = u.priceForTime(time.Date(2027, time.December, 31, 18, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)
	})

	t.Run("Ultimate Savers Demand TOU (ameren_mo_ultimate_saver) and Map.Site integration", func(t *testing.T) {
		// Verify Map.Site resolves Ameren MO plans via genericTOU even when baseAmerenSmart is nil
		m := NewMap(nil)
		siteUtil, err := m.Site(context.Background(), "site-mo", types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_ultimate_saver",
		})
		require.NoError(t, err)
		require.NotNil(t, siteUtil)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_ultimate_saver",
		})
		require.NoError(t, err)

		// Summer On-Peak (3 PM - 7 PM Mon-Fri): Wed, July 15, 2026 at 5:00 PM -> $0.3413 + $0.014095
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.3413+0.014095, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Summer Off-Peak: Wed, July 15, 2026 at 12:00 PM -> $0.0578 + $0.014095
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0578+0.014095, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Winter On-Peak (6-8 AM & 6-8 PM Mon-Fri): Thu, Nov 12, 2026 at 7:00 PM (19:00) -> $0.1856 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 19, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1856+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Off-Peak: Thu, Nov 12, 2026 at 12:00 PM -> $0.0511 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0511+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Thanksgiving Friday holiday (Fri, Nov 27, 2026 at 7:00 PM): Off-Peak $0.0511 + $0.009995
		p, err = u.priceForTime(time.Date(2026, time.November, 27, 19, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0511+0.009995, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
	})

	t.Run("Anytime Users across all 2026-2027 rider windows (ameren_mo_anytime)", func(t *testing.T) {
		netSettings := types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_anytime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		}
		err := u.ApplySettings(context.Background(), netSettings)
		require.NoError(t, err)

		// Jan 2026 Winter: $0.1062 + $0.009681
		pJan, err := u.priceForTime(time.Date(2026, time.January, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.009681, pJan.DollarsPerKWH, 1e-6)

		// Feb-Mar 2026 Winter: $0.1062 + $0.010455
		pMar, err := u.priceForTime(time.Date(2026, time.March, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.010455, pMar.DollarsPerKWH, 1e-6)

		// Apr-May 2026 Winter: $0.1062 + $0.010555
		pMay, err := u.priceForTime(time.Date(2026, time.May, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.010555, pMay.DollarsPerKWH, 1e-6)

		// Jun-Sep 2026 Summer: $0.1560 + $0.014095
		pAug, err := u.priceForTime(time.Date(2026, time.August, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1560+0.014095, pAug.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.1560+0.014095, pAug.ExportRateDollars(netSettings, []types.Price{pAug}), 1e-6)

		// Oct-Dec 2026 Winter: $0.1062 + $0.009995
		pNov, err := u.priceForTime(time.Date(2026, time.November, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.009995, pNov.DollarsPerKWH, 1e-6)

		// 2027 Winter & Summer: $0.1062 / $0.1560 + $0.009995
		pFeb2027, err := u.priceForTime(time.Date(2027, time.February, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062+0.009995, pFeb2027.DollarsPerKWH, 1e-6)

		pJul2027, err := u.priceForTime(time.Date(2027, time.July, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1560+0.009995, pJul2027.DollarsPerKWH, 1e-6)
	})
}
