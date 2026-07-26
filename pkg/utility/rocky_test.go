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
	t.Run("Utah Residential Service flat rate with Net Billing Schedule 137", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		targetSummer := time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation)
		pSummer, err := u.priceForTime(targetSummer)
		require.NoError(t, err)
		assert.InDelta(t, 0.093199, pSummer.DollarsPerKWH, 1e-6)
		if assert.True(t, pSummer.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04855, pSummer.GenerationCreditDollarsPerKWH, 1e-6)
		}

		targetWinter := time.Date(2026, time.December, 15, 14, 0, 0, 0, mtLocation)
		pWinter, err := u.priceForTime(targetWinter)
		require.NoError(t, err)
		assert.InDelta(t, 0.082477, pWinter.DollarsPerKWH, 1e-6)
		if assert.True(t, pWinter.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04033, pWinter.GenerationCreditDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Utah Residential Service flat rate with 1:1 Net Metering Schedule 135", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		targetSummer := time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation)
		pSummer, err := u.priceForTime(targetSummer)
		require.NoError(t, err)
		assert.InDelta(t, 0.093199, pSummer.DollarsPerKWH, 1e-6)
		assert.False(t, pSummer.SeparateGenerationCredit)
	})

	t.Run("Utah Residential Service TOU with Net Billing Schedule 137", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_utah_residential_tou",
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

		targetSummerPeak := time.Date(2026, time.July, 15, 19, 0, 0, 0, mtLocation)
		pSummerPeak, err := u.priceForTime(targetSummerPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.320834, pSummerPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pSummerPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04855, pSummerPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		targetSummerOffpeak := time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation)
		pSummerOffpeak, err := u.priceForTime(targetSummerOffpeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.071296, pSummerOffpeak.DollarsPerKWH, 1e-6)

		targetSummerWeekend := time.Date(2026, time.July, 18, 19, 0, 0, 0, mtLocation)
		pSummerWeekend, err := u.priceForTime(targetSummerWeekend)
		require.NoError(t, err)
		assert.InDelta(t, 0.071296, pSummerWeekend.DollarsPerKWH, 1e-6)

		targetSummerHoliday := time.Date(2026, time.July, 24, 19, 0, 0, 0, mtLocation)
		pSummerHoliday, err := u.priceForTime(targetSummerHoliday)
		require.NoError(t, err)
		assert.InDelta(t, 0.071296, pSummerHoliday.DollarsPerKWH, 1e-6)
	})

	t.Run("Idaho Residential Service with Net Billing Schedule 136", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_idaho_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		targetSummerPeak := time.Date(2026, time.July, 15, 16, 0, 0, 0, mtLocation)
		pSummerPeak, err := u.priceForTime(targetSummerPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.105453, pSummerPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pSummerPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.14666, pSummerPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		targetSummerOff := time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation)
		pSummerOff, err := u.priceForTime(targetSummerOff)
		require.NoError(t, err)
		assert.InDelta(t, 0.105453, pSummerOff.DollarsPerKWH, 1e-6)
		if assert.True(t, pSummerOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03664, pSummerOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		targetWinterPeak := time.Date(2026, time.November, 15, 19, 0, 0, 0, mtLocation)
		pWinterPeak, err := u.priceForTime(targetWinterPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.087877, pWinterPeak.DollarsPerKWH, 1e-6)
		if assert.True(t, pWinterPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05597, pWinterPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer 2027 check
		targetSummer2027 := time.Date(2027, time.July, 15, 16, 0, 0, 0, mtLocation)
		pSummer2027, err := u.priceForTime(targetSummer2027)
		require.NoError(t, err)
		assert.InDelta(t, 0.100048, pSummer2027.DollarsPerKWH, 1e-6)

		// Winter 2027 check
		targetWinter2027 := time.Date(2027, time.November, 15, 19, 0, 0, 0, mtLocation)
		pWinter2027, err := u.priceForTime(targetWinter2027)
		require.NoError(t, err)
		assert.InDelta(t, 0.083373, pWinter2027.DollarsPerKWH, 1e-6)
	})

	t.Run("Wyoming Residential Service with Net Metering Schedule 135", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rocky_mountain_power",
			UtilityRate:     "rocky_mountain_power_wyoming_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 14, 0, 0, 0, mtLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.08136, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)
	})
}
