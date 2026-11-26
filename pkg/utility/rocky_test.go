package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRockyMountainPowerHolidays(t *testing.T) {
	t.Run("Verify Utah holidays in 2026", func(t *testing.T) {
		holidays := getRockyHolidays(2026)
		if assert.NotEmpty(t, holidays) {
			assert.Contains(t, holidays, "2026-01-01") // New Year's Day (Thursday)
			assert.Contains(t, holidays, "2026-02-16") // President's Day (Monday)
			assert.Contains(t, holidays, "2026-05-25") // Memorial Day (Monday)
			assert.Contains(t, holidays, "2026-07-03") // Independence Day (Saturday July 4 shifts to Friday July 3)
			assert.Contains(t, holidays, "2026-07-24") // Pioneer Day (Friday)
			assert.Contains(t, holidays, "2026-09-07") // Labor Day (Monday)
			assert.Contains(t, holidays, "2026-11-26") // Thanksgiving (Thursday)
			assert.Contains(t, holidays, "2026-12-25") // Christmas Day (Friday)
		}
	})

	t.Run("Verify Saturday shift to Friday", func(t *testing.T) {
		holidays := getRockyHolidays(2026)
		if assert.Contains(t, holidays, "2026-07-03") {
			assert.NotContains(t, holidays, "2026-07-04")
		}
	})

	t.Run("Verify Sunday shift to Monday", func(t *testing.T) {
		holidays := getRockyHolidays(2027)
		if assert.Contains(t, holidays, "2027-07-05") {
			assert.NotContains(t, holidays, "2027-07-04")
		}
		if assert.Contains(t, holidays, "2027-07-23") {
			assert.NotContains(t, holidays, "2027-07-24")
		}
	})
}

func TestRockyMountainPowerRates(t *testing.T) {
	t.Run("Utah Residential Service flat rate across 2026 and 2027 windows with Schedule 137", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// Jan-May 2026 Winter: pre-Aug base $0.082477 * 1.26486561
		pFeb2026, err := u.priceForTime(time.Date(2026, time.February, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.082477*1.26486561, pFeb2026.DollarsPerKWH, 1e-6)
		if assert.True(t, pFeb2026.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04033, pFeb2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Jun 2026 Summer: pre-Aug base $0.093199 * 1.26486561
		pJun2026, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.093199*1.26486561, pJun2026.DollarsPerKWH, 1e-6)
		if assert.True(t, pJun2026.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04855, pJun2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2026-07-01 through 2026-08-09 Summer: pre-Aug base $0.093199 * 1.1139471
		pAug9LastHour, err := u.priceForTime(time.Date(2026, time.August, 9, 23, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.093199*1.1139471, pAug9LastHour.DollarsPerKWH, 1e-6)

		// 2026-08-10 through 2026-09-24 Summer: Aug 10 base $0.098332 * 1.1139471
		pAug10FirstHour, err := u.priceForTime(time.Date(2026, time.August, 10, 0, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098332*1.1139471, pAug10FirstHour.DollarsPerKWH, 1e-6)

		pSep24LastHour, err := u.priceForTime(time.Date(2026, time.September, 24, 23, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098332*1.1139471, pSep24LastHour.DollarsPerKWH, 1e-6)

		// 2026-09-25 through 2026-09-30 Summer: Aug 10 base $0.098332 * 1.1317471 (with Schedule 92)
		pSep25FirstHour, err := u.priceForTime(time.Date(2026, time.September, 25, 0, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098332*1.1317471, pSep25FirstHour.DollarsPerKWH, 1e-6)
		if assert.True(t, pSep25FirstHour.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04855, pSep25FirstHour.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Oct-Dec 2026 Winter: Aug 10 base $0.087020 * 1.1317471
		pDec2026, err := u.priceForTime(time.Date(2026, time.December, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087020*1.1317471, pDec2026.DollarsPerKWH, 1e-6)
		if assert.True(t, pDec2026.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04033, pDec2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2027 Winter & Summer: Schedule 92 expires; multiplier reverts to 1.1139471
		pJan2027, err := u.priceForTime(time.Date(2027, time.January, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087020*1.1139471, pJan2027.DollarsPerKWH, 1e-6)

		pJul2027, err := u.priceForTime(time.Date(2027, time.July, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098332*1.1139471, pJul2027.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Utah Residential Service flat rate with Schedule 136 and Schedule 135 export options", func(t *testing.T) {
		u136 := &genericTOU{}
		err := u136.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "schedule_136",
			},
		})
		require.NoError(t, err)

		pSummer136, err := u136.priceForTime(time.Date(2026, time.August, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098332*1.1139471, pSummer136.DollarsPerKWH, 1e-6)
		if assert.True(t, pSummer136.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0920, pSummer136.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pWinter136, err := u136.priceForTime(time.Date(2026, time.December, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087020*1.1317471, pWinter136.DollarsPerKWH, 1e-6)
		if assert.True(t, pWinter136.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0920, pWinter136.GenerationCreditDollarsPerKWH, 1e-6)
		}

		u135 := &genericTOU{}
		err = u135.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pSummer135, err := u135.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.093199*1.1139471, pSummer135.DollarsPerKWH, 1e-6)
		assert.False(t, pSummer135.SeparateGenerationCredit)
	})

	t.Run("Utah Residential Service TOU across 2026 and 2027 windows, holidays, and Schedule 136", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "schedule_136",
			},
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

		// Jan-May 2026 Winter Weekday Peak (19:00) vs Off-Peak (14:00)
		pFebPeak, err := u.priceForTime(time.Date(2026, time.February, 18, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.283924*1.26486561, pFebPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pFebPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0920, pFebPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pFebOff, err := u.priceForTime(time.Date(2026, time.February, 18, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.063094*1.26486561, pFebOff.DollarsPerKWH, 1e-6)

		// Jul 2026 Summer Weekday Peak, Off-Peak, Weekend, and Pioneer Day Holiday (2026-07-24)
		pJulPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.320834*1.1139471, pJulPeak.DollarsPerKWH, 1e-6)

		pJulOff, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.071296*1.1139471, pJulOff.DollarsPerKWH, 1e-6)

		pJulWeekend, err := u.priceForTime(time.Date(2026, time.July, 18, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.071296*1.1139471, pJulWeekend.DollarsPerKWH, 1e-6)

		pPioneerDay, err := u.priceForTime(time.Date(2026, time.July, 24, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.071296*1.1139471, pPioneerDay.DollarsPerKWH, 1e-6)

		// Aug 10 - Sep 24, 2026 Summer Weekday Peak & Labor Day Holiday (2026-09-07)
		pAugPeak, err := u.priceForTime(time.Date(2026, time.August, 12, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.336173*1.1139471, pAugPeak.DollarsPerKWH, 1e-6)

		pLaborDay, err := u.priceForTime(time.Date(2026, time.September, 7, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.074705*1.1139471, pLaborDay.DollarsPerKWH, 1e-6)

		// Late Sep 2026 Summer Weekday Peak (2026-09-25 Friday) with Schedule 92
		pSep25Peak, err := u.priceForTime(time.Date(2026, time.September, 25, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.336173*1.1317471, pSep25Peak.DollarsPerKWH, 1e-6)

		// Nov 2026 Winter Weekday Peak & Thanksgiving Holiday (2026-11-26) with Schedule 92
		pNovPeak, err := u.priceForTime(time.Date(2026, time.November, 25, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.297498*1.1317471, pNovPeak.DollarsPerKWH, 1e-6)

		pThanksgiving, err := u.priceForTime(time.Date(2026, time.November, 26, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.066111*1.1317471, pThanksgiving.DollarsPerKWH, 1e-6)

		// 2027 Winter & Summer Weekday Peaks (1.1139471x)
		pFeb2027Peak, err := u.priceForTime(time.Date(2027, time.February, 17, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.297498*1.1139471, pFeb2027Peak.DollarsPerKWH, 1e-6)

		pJul2027Peak, err := u.priceForTime(time.Date(2027, time.July, 14, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.336173*1.1139471, pJul2027Peak.DollarsPerKWH, 1e-6)
	})

	t.Run("Idaho Residential Service June 1 rate steps and Net Billing Schedule 136", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_idaho_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// Jan-May 2026: $0.097400/kWh + Winter Net Billing credits
		pMay2026, err := u.priceForTime(time.Date(2026, time.May, 31, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.097400, pMay2026.DollarsPerKWH, 1e-6)
		if assert.True(t, pMay2026.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05597, pMay2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Jun-Oct 2026: $0.109977/kWh + Summer Net Billing credits ($0.14666 peak / $0.03664 off-peak)
		pJulPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.109977, pJulPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pJulPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.14666, pJulPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pJulOff, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.109977, pJulOff.DollarsPerKWH, 1e-6)
		if assert.True(t, pJulOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03664, pJulOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Nov 2026 - May 2027: $0.092064/kWh across Jan 1, 2027 until June 1, 2027
		pNov2026, err := u.priceForTime(time.Date(2026, time.November, 15, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.092064, pNov2026.DollarsPerKWH, 1e-6)
		if assert.True(t, pNov2026.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05597, pNov2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pMar2027, err := u.priceForTime(time.Date(2027, time.March, 15, 12, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.092064, pMar2027.DollarsPerKWH, 1e-6)
		if assert.True(t, pMar2027.SeparateGenerationCredit) {
			assert.InDelta(t, 0.01228, pMar2027.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pMay312027, err := u.priceForTime(time.Date(2027, time.May, 31, 23, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.092064, pMay312027.DollarsPerKWH, 1e-6)

		// From 2027-06-01: $0.104468/kWh (Jun-Oct) and $0.087473/kWh (Nov-Dec)
		pJun12027, err := u.priceForTime(time.Date(2027, time.June, 1, 0, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.104468, pJun12027.DollarsPerKWH, 1e-6)

		pNov2027, err := u.priceForTime(time.Date(2027, time.November, 15, 19, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087473, pNov2027.DollarsPerKWH, 1e-6)
	})

	t.Run("Idaho Optional Time of Day Residential Service Schedule 36 TOU and exports", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_idaho_residential_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
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

		// Summer 2026 (Jun-Oct): 15:00-23:00 daily (including weekends and holidays)
		pJulOff, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.060845, pJulOff.DollarsPerKWH, 1e-6)
		if assert.True(t, pJulOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03664, pJulOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pJulWeekendPeak, err := u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, mtLocation)) // Saturday
		require.NoError(t, err)
		assert.InDelta(t, 0.186804, pJulWeekendPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pJulWeekendPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.14666, pJulWeekendPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pJulHolidayPeak, err := u.priceForTime(time.Date(2026, time.July, 3, 22, 0, 0, 0, mtLocation)) // Observed July 4th
		require.NoError(t, err)
		assert.InDelta(t, 0.186804, pJulHolidayPeak.DollarsPerKWH, 1e-6)

		pJulLateOff, err := u.priceForTime(time.Date(2026, time.July, 3, 23, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.060845, pJulLateOff.DollarsPerKWH, 1e-6)

		// Winter Jan 2026 - May 2027: 06:00-09:00 and 18:00-23:00 daily (including holidays)
		pFebMorningPeak, err := u.priceForTime(time.Date(2026, time.February, 16, 7, 0, 0, 0, mtLocation)) // President's Day
		require.NoError(t, err)
		assert.InDelta(t, 0.160330, pFebMorningPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pFebMorningPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05597, pFebMorningPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pMar2027MiddayOff, err := u.priceForTime(time.Date(2027, time.March, 14, 12, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.056102, pMar2027MiddayOff.DollarsPerKWH, 1e-6)
		if assert.True(t, pMar2027MiddayOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.01228, pMar2027MiddayOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pMay2027EveningPeak, err := u.priceForTime(time.Date(2027, time.May, 31, 19, 0, 0, 0, mtLocation)) // Memorial Day 2027
		require.NoError(t, err)
		assert.InDelta(t, 0.160330, pMay2027EveningPeak.DollarsPerKWH, 1e-6)

		// Step on 2027-06-01: $0.183038/$0.059646 (Jun-Oct) and $0.157104/$0.054999 (Nov-Dec)
		pJun2027Peak, err := u.priceForTime(time.Date(2027, time.June, 1, 15, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.183038, pJun2027Peak.DollarsPerKWH, 1e-6)

		pJun2027Off, err := u.priceForTime(time.Date(2027, time.June, 1, 10, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.059646, pJun2027Off.DollarsPerKWH, 1e-6)

		pNov2027Peak, err := u.priceForTime(time.Date(2027, time.November, 25, 19, 0, 0, 0, mtLocation)) // Thanksgiving 2027
		require.NoError(t, err)
		assert.InDelta(t, 0.157104, pNov2027Peak.DollarsPerKWH, 1e-6)

		pNov2027Off, err := u.priceForTime(time.Date(2027, time.November, 25, 12, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.054999, pNov2027Off.DollarsPerKWH, 1e-6)

		// Verify 1:1 Net Metering (Schedule 135) option
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_idaho_residential_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)
		pNet, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.186804, pNet.DollarsPerKWH, 1e-6)
		assert.False(t, pNet.SeparateGenerationCredit)
	})

	t.Run("Wyoming Residential Service Schedule 2 with July 1, 2026 rider update", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_wyoming_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pJun2026, err := u.priceForTime(time.Date(2026, time.June, 30, 23, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12174, pJun2026.DollarsPerKWH, 1e-6)
		assert.False(t, pJun2026.SeparateGenerationCredit)

		pJul2026, err := u.priceForTime(time.Date(2026, time.July, 1, 0, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11515, pJul2026.DollarsPerKWH, 1e-6)
		assert.False(t, pJul2026.SeparateGenerationCredit)

		pMar2027, err := u.priceForTime(time.Date(2027, time.March, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11515, pMar2027.DollarsPerKWH, 1e-6)
	})

	t.Run("Wyoming Residential Service Time-of-Use Pilot Schedule 19", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_wyoming_residential_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
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

		// Jan-Jun 2026: $0.16091/kWh On-Peak (07:00-09:00 & 17:00-23:00 daily), $0.09869/kWh Off-Peak
		pMayMorningPeak, err := u.priceForTime(time.Date(2026, time.May, 25, 7, 0, 0, 0, mtLocation)) // Memorial Day
		require.NoError(t, err)
		assert.InDelta(t, 0.16091, pMayMorningPeak.DollarsPerKWH, 1e-6)
		assert.False(t, pMayMorningPeak.SeparateGenerationCredit)

		pMayMiddayOff, err := u.priceForTime(time.Date(2026, time.May, 25, 9, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09869, pMayMiddayOff.DollarsPerKWH, 1e-6)

		// From 2026-07-01 through 2027: $0.15432/kWh On-Peak, $0.09210/kWh Off-Peak (including weekends & holidays)
		pJulWeekendEveningPeak, err := u.priceForTime(time.Date(2026, time.July, 19, 17, 0, 0, 0, mtLocation)) // Sunday
		require.NoError(t, err)
		assert.InDelta(t, 0.15432, pJulWeekendEveningPeak.DollarsPerKWH, 1e-6)

		pJulNightOff, err := u.priceForTime(time.Date(2026, time.July, 19, 23, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09210, pJulNightOff.DollarsPerKWH, 1e-6)

		pChristmasPeak, err := u.priceForTime(time.Date(2026, time.December, 25, 18, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15432, pChristmasPeak.DollarsPerKWH, 1e-6)

		pOct2027MorningPeak, err := u.priceForTime(time.Date(2027, time.October, 10, 8, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15432, pOct2027MorningPeak.DollarsPerKWH, 1e-6)

		pOct2027Off, err := u.priceForTime(time.Date(2027, time.October, 10, 12, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09210, pOct2027Off.DollarsPerKWH, 1e-6)
	})

	t.Run("Continuous hourly coverage across mid-month and seasonal transitions in 2026-2027", func(t *testing.T) {
		ctx := context.Background()
		info := rockyUtilityInfo()
		require.Len(t, info.Rates, 6)

		transitionWindows := []struct {
			start time.Time
			end   time.Time
		}{
			{
				start: time.Date(2026, time.May, 30, 0, 0, 0, 0, mtLocation),
				end:   time.Date(2026, time.June, 3, 0, 0, 0, 0, mtLocation),
			},
			{
				start: time.Date(2026, time.June, 29, 0, 0, 0, 0, mtLocation),
				end:   time.Date(2026, time.July, 3, 0, 0, 0, 0, mtLocation),
			},
			{
				start: time.Date(2026, time.August, 8, 0, 0, 0, 0, mtLocation),
				end:   time.Date(2026, time.August, 12, 0, 0, 0, 0, mtLocation),
			},
			{
				start: time.Date(2026, time.September, 23, 0, 0, 0, 0, mtLocation),
				end:   time.Date(2026, time.October, 3, 0, 0, 0, 0, mtLocation),
			},
			{
				start: time.Date(2026, time.December, 30, 0, 0, 0, 0, mtLocation),
				end:   time.Date(2027, time.January, 3, 0, 0, 0, 0, mtLocation),
			},
			{
				start: time.Date(2027, time.May, 30, 0, 0, 0, 0, mtLocation),
				end:   time.Date(2027, time.June, 3, 0, 0, 0, 0, mtLocation),
			},
		}

		for _, rate := range info.Rates {
			for _, choice := range rate.Options[0].Choices {
				u := &genericTOU{}
				err := u.ApplySettings(ctx, types.Settings{
					UtilityProvider: "rocky_mountain_power",
					UtilityRate:     rate.ID,
					UtilityRateOptions: types.UtilityRateOptions{
						NetMeteringScheme: choice.Value,
					},
				})
				require.NoError(t, err)

				for _, w := range transitionWindows {
					prices, err := u.GetConfirmedPrices(ctx, w.start, w.end)
					require.NoError(t, err)
					if assert.NotEmpty(t, prices) {
						assert.Equal(t, w.start, prices[0].TSStart)
						assert.Equal(t, w.end, prices[len(prices)-1].TSEnd)
						for i := 1; i < len(prices); i++ {
							assert.Equal(t, prices[i-1].TSEnd, prices[i].TSStart)
							assert.Greater(t, prices[i].DollarsPerKWH, 0.0)
						}
					}
				}
			}
		}
	})
}
