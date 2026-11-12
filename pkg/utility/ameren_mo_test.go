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
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_evening_morning",
		})
		require.NoError(t, err)

		// Summer On-Peak (9 AM - 9 PM all days, including weekends/holidays):
		// Sat, July 4, 2026 at 2:00 PM -> $0.1524 + $0.0057 = $0.1581
		p, err := u.priceForTime(time.Date(2026, time.July, 4, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1581, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.False(t, p.SeparateGenerationCredit)

		// Summer Off-Peak (9 PM - 9 AM all days):
		// Wed, July 15, 2026 at 9:00 PM (21:00) -> $0.1524
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 21, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1524, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Winter On-Peak: Thu, Nov 12, 2026 at 10:00 AM -> $0.1045 + $0.0028 = $0.1073
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1073, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Off-Peak: Thu, Nov 12, 2026 at 8:00 AM -> $0.1045
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1045, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Avoided Fuel Cost export scheme
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_evening_morning",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "avoided_cost",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.November, 12, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1073, p.DollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0339, p.GenerationCreditDollarsPerKWH, 1e-6)

		pSummer, err := u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.True(t, pSummer.SeparateGenerationCredit)
		assert.InDelta(t, 0.0384, pSummer.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Overnight Savers (ameren_mo_overnight)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_overnight",
		})
		require.NoError(t, err)

		// Summer On-Peak (6 AM - 10 PM all days): Sun, July 19, 2026 at 12:00 PM -> $0.2036
		p, err := u.priceForTime(time.Date(2026, time.July, 19, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2036, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Summer Off-Peak (10 PM - 6 AM all days): Sun, July 19, 2026 at 10:00 PM (22:00) -> $0.0810
		p, err = u.priceForTime(time.Date(2026, time.July, 19, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0810, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Winter Option A (default): Thu, Jan 15, 2026 at 7:00 AM -> On-Peak $0.1145; 2:00 AM -> Off-Peak $0.0699
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1145, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.January, 15, 2, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0699, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Winter Option B reverts to Anytime Service in winter ($0.1062/kWh)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_overnight",
			UtilityRateOptions: types.UtilityRateOptions{
				PeakPeriodOption: "option_b",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.January, 15, 2, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062, p.DollarsPerKWH, 1e-6)
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

		// Summer weekday On-Peak: Wed, July 15, 2026 at 4:00 PM (16:00) -> $0.4051
		pOnPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.4051, pOnPeak.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", pOnPeak.PeriodName)

		// Summer weekday Intermediate before peak: Wed, July 15, 2026 at 12:00 PM -> $0.1216
		pInter, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216, pInter.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", pInter.PeriodName)

		// Under tou_net (Sheet 171 TOU bucket netting), exporting during On-Peak credits $0.4051 while Intermediate credits $0.1216
		allPrices := []types.Price{pOnPeak, pInter}
		assert.InDelta(t, 0.4051, pOnPeak.ExportRateDollars(settings, allPrices), 1e-6)
		assert.InDelta(t, 0.1216, pInter.ExportRateDollars(settings, allPrices), 1e-6)

		// Summer weekday Intermediate after peak: Wed, July 15, 2026 at 8:00 PM (20:00) -> $0.1216
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 20, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Summer Off-Peak: Wed, July 15, 2026 at 11:00 PM (23:00) -> $0.0767
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0767, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Summer observed Independence Day holiday (Fri, July 3, 2026 at 4:00 PM): Intermediate $0.1216 (no On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Summer weekend (Sat, July 18, 2026 at 4:00 PM): Intermediate $0.1216
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1216, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Winter Option A weekday morning On-Peak: Thu, Nov 12, 2026 at 7:00 AM -> $0.2169
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2169, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Option A weekday midday Intermediate: Thu, Nov 12, 2026 at 12:00 PM -> $0.0778
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Winter Option A weekday evening On-Peak: Thu, Nov 12, 2026 at 6:00 PM (18:00) -> $0.2169
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 18, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2169, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Option A Off-Peak: Thu, Nov 12, 2026 at 11:00 PM (23:00) -> $0.0635
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0635, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Good Friday holiday (Fri, April 3, 2026 at 7:00 AM): Intermediate $0.0778 (not On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.April, 3, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Intermediate", p.PeriodName)

		// Christmas Eve holiday (Thu, Dec 24, 2026 at 6:00 PM): Intermediate $0.0778 (not On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.December, 24, 18, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0778, p.DollarsPerKWH, 1e-6)
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

		// Summer On-Peak (3 PM - 7 PM Mon-Fri): Wed, July 15, 2026 at 5:00 PM -> $0.3413
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.3413, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Summer Off-Peak: Wed, July 15, 2026 at 12:00 PM -> $0.0578
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0578, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Winter On-Peak (6-8 AM & 6-8 PM Mon-Fri): Thu, Nov 12, 2026 at 7:00 PM (19:00) -> $0.1856
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 19, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1856, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Off-Peak: Thu, Nov 12, 2026 at 12:00 PM -> $0.0511
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0511, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Thanksgiving Friday holiday (Fri, Nov 27, 2026 at 7:00 PM): Off-Peak $0.0511
		p, err = u.priceForTime(time.Date(2026, time.November, 27, 19, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0511, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
	})

	t.Run("Anytime Users (ameren_mo_anytime)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ameren",
			UtilityRate:     "ameren_mo_anytime",
		})
		require.NoError(t, err)

		// Summer (June - September): $0.1560
		p, err := u.priceForTime(time.Date(2026, time.August, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1560, p.DollarsPerKWH, 1e-6)

		// Winter (October - May): $0.1062
		p, err = u.priceForTime(time.Date(2026, time.May, 10, 15, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1062, p.DollarsPerKWH, 1e-6)
	})
}
