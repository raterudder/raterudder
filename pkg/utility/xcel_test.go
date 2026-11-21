package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXcelNSPHolidays(t *testing.T) {
	t.Run("Check NSP holidays 2026", func(t *testing.T) {
		holidays := getXcelNSPHolidays(2026)
		assert.Contains(t, holidays, "2026-01-01") // New Year's Day
		assert.Contains(t, holidays, "2026-04-03") // Good Friday
		assert.Contains(t, holidays, "2026-05-25") // Memorial Day
		assert.Contains(t, holidays, "2026-07-03") // Independence Day (shifts to Friday)
		assert.Contains(t, holidays, "2026-09-07") // Labor Day
		assert.Contains(t, holidays, "2026-11-26") // Thanksgiving
		assert.Contains(t, holidays, "2026-12-25") // Christmas Day
	})
}

func TestXcelWIHolidays(t *testing.T) {
	t.Run("Check Wisconsin holidays 2026", func(t *testing.T) {
		holidays := getXcelWIHolidays(2026)
		assert.Contains(t, holidays, "2026-11-27") // Friday after Thanksgiving
		assert.Contains(t, holidays, "2026-12-24") // Christmas Eve
		assert.Contains(t, holidays, "2026-12-31") // New Year's Eve
	})
}

func TestXcelCOHolidays(t *testing.T) {
	t.Run("Check Colorado holidays 2026", func(t *testing.T) {
		holidays := getXcelCOHolidays(2026)
		assert.Contains(t, holidays, "2026-01-19") // MLK Day
		assert.Contains(t, holidays, "2026-02-16") // Presidents' Day
		assert.Contains(t, holidays, "2026-06-19") // Juneteenth
		assert.Contains(t, holidays, "2026-10-12") // Columbus Day
		assert.Contains(t, holidays, "2026-11-11") // Veterans Day
	})
}

func TestXcelMichiganPeakHours(t *testing.T) {
	t.Run("Michigan Option 1", func(t *testing.T) {
		opt := getXcelMIPeakHours("1")
		assert.Equal(t, 9, opt.HourStart)
		assert.Equal(t, 21, opt.HourEnd)
	})
	t.Run("Michigan Option 2", func(t *testing.T) {
		opt := getXcelMIPeakHours("2")
		assert.Equal(t, 8, opt.HourStart)
		assert.Equal(t, 20, opt.HourEnd)
	})
}

func TestXcelPricing(t *testing.T) {
	t.Run("Colorado TOU pricing across 2026 effective windows and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_co_tou",
		})
		require.NoError(t, err)

		// Pre-July 2026 summer: 17:00-21:00 MT weekday on-peak ($0.16430), 16:00 off-peak ($0.08218)
		pJunOff, err := u.priceForTime(time.Date(2026, time.June, 15, 16, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08218, pJunOff.DollarsPerKWH, 1e-6)

		pJunPeak, err := u.priceForTime(time.Date(2026, time.June, 15, 18, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.16430, pJunPeak.DollarsPerKWH, 1e-6)

		// Effective 2026-07-01 summer: 16:00 is off-peak ($0.13408), 18:00 is on-peak ($0.36254)
		pJulOff, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.13408, pJulOff.DollarsPerKWH, 1e-6)

		pJulPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 18, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.36254, pJulPeak.DollarsPerKWH, 1e-6)

		// Effective 2026-10-01 through 2026-10-18 winter: $0.31431 on-peak, $0.12590 off-peak
		pOct1Peak, err := u.priceForTime(time.Date(2026, time.October, 15, 18, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.31431, pOct1Peak.DollarsPerKWH, 1e-6)

		pOct1Off, err := u.priceForTime(time.Date(2026, time.October, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12590, pOct1Off.DollarsPerKWH, 1e-6)

		// Effective 2026-10-19 winter and 2027 summer/winter
		pOct19Peak, err := u.priceForTime(time.Date(2026, time.October, 20, 18, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.31559, pOct19Peak.DollarsPerKWH, 1e-6)

		pOct19Off, err := u.priceForTime(time.Date(2026, time.October, 20, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12718, pOct19Off.DollarsPerKWH, 1e-6)

		p2027SummerPeak, err := u.priceForTime(time.Date(2027, time.July, 15, 18, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.36441, p2027SummerPeak.DollarsPerKWH, 1e-6)

		p2027SummerOff, err := u.priceForTime(time.Date(2027, time.July, 15, 14, 0, 0, 0, mtLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.14501, p2027SummerOff.DollarsPerKWH, 1e-6)
	})

	t.Run("Minnesota Standard and Heating with interim surcharge and monthly fuel", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_standard",
		})
		require.NoError(t, err)

		// January 2026: $0.11364 * 1.0714 + $0.01568
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11364*1.0714+0.01568, pJan.DollarsPerKWH, 1e-6)

		// July 2026: $0.13069 * 1.0714 + $0.02927
		pJul, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.13069*1.0714+0.02927, pJul.DollarsPerKWH, 1e-6)

		// October 2026 negative fuel factor (-$0.00179/kWh)
		pOct, err := u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11364*1.0714-0.00179, pOct.DollarsPerKWH, 1e-6)

		// November 2026 ($0.01926/kWh) and December 2026 / 2027 fallback ($0.02162/kWh)
		pNov, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11364*1.0714+0.01926, pNov.DollarsPerKWH, 1e-6)

		pDec, err := u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11364*1.0714+0.02162, pDec.DollarsPerKWH, 1e-6)

		p2027, err := u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11364*1.0714+0.02162, p2027.DollarsPerKWH, 1e-6)

		uHeat := &genericTOU{}
		err = uHeat.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_tou_heating",
		})
		require.NoError(t, err)

		pHeatPeak, err := uHeat.priceForTime(time.Date(2027, time.January, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.13577*1.0714+0.02162, pHeatPeak.DollarsPerKWH, 1e-6)

		pHeatOff, err := uHeat.priceForTime(time.Date(2027, time.January, 15, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.05171*1.0714+0.02162, pHeatOff.DollarsPerKWH, 1e-6)
	})

	t.Run("North Dakota TOU Heating summer and winter off-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_nd_tou_heating",
		})
		require.NoError(t, err)

		pSummerOff, err := u.priceForTime(time.Date(2026, time.July, 15, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.04365, pSummerOff.DollarsPerKWH, 1e-6)

		pSummerPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.18180, pSummerPeak.DollarsPerKWH, 1e-6)

		p2027WinterPeak, err := u.priceForTime(time.Date(2027, time.January, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12418, p2027WinterPeak.DollarsPerKWH, 1e-6)
	})

	t.Run("South Dakota Standard and TOD pre-July, post-July EL25-024, and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_standard",
		})
		require.NoError(t, err)

		pPreJulWinter, err := u.priceForTime(time.Date(2026, time.February, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09585, pPreJulWinter.DollarsPerKWH, 1e-6)

		pPreJulSummer, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11153, pPreJulSummer.DollarsPerKWH, 1e-6)

		pPostJulSummer, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.13655, pPostJulSummer.DollarsPerKWH, 1e-6)

		pPostJulWinter, err := u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12096, pPostJulWinter.DollarsPerKWH, 1e-6)

		p2027Winter, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.12096, p2027Winter.DollarsPerKWH, 1e-6)

		uTOU := &genericTOU{}
		err = uTOU.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_tou",
		})
		require.NoError(t, err)

		pTOUPreJul, err := uTOU.priceForTime(time.Date(2026, time.June, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21806, pTOUPreJul.DollarsPerKWH, 1e-6)

		pTOUPostJulPeak, err := uTOU.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.24724, pTOUPostJulPeak.DollarsPerKWH, 1e-6)

		pTOUPostJulOff, err := uTOU.priceForTime(time.Date(2026, time.July, 15, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.06210, pTOUPostJulOff.DollarsPerKWH, 1e-6)

		pTOUWinterPeak, err := uTOU.priceForTime(time.Date(2026, time.December, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.20738, pTOUWinterPeak.DollarsPerKWH, 1e-6)

		uTOUHeat := &genericTOU{}
		err = uTOUHeat.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_tou_heating",
		})
		require.NoError(t, err)

		pHeatWinterPeak, err := uTOUHeat.priceForTime(time.Date(2026, time.December, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19427, pHeatWinterPeak.DollarsPerKWH, 1e-6)
	})

	t.Run("Michigan TOU and Standard supply, Oct 1 EWR, Nov 1 PSCR, and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mi_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				PeakPeriodOption: "3", // 8 AM - 8 PM
			},
		})
		require.NoError(t, err)

		// Pre-Oct 1, 2026: $0.0581 - $0.01009 = $0.04801
		pJul, err := u.priceForTime(time.Date(2026, time.July, 15, 9, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1607, pJul.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04801, pJul.GridUseDollarsPerKWH, 1e-6)

		// Oct 1 - Oct 31, 2026: $0.0581 + $0.008 - $0.01009 = $0.05601
		pOct, err := u.priceForTime(time.Date(2026, time.October, 15, 9, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1607, pOct.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.05601, pOct.GridUseDollarsPerKWH, 1e-6)

		// From Nov 1, 2026 through 2027: $0.0581 + $0.008 - $0.02500 = $0.04110
		pNov, err := u.priceForTime(time.Date(2026, time.November, 16, 9, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1607, pNov.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04110, pNov.GridUseDollarsPerKWH, 1e-6)

		p2027, err := u.priceForTime(time.Date(2027, time.March, 15, 9, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1607, p2027.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04110, p2027.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Wisconsin Standard supply and delivery", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_wi_standard",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.097900, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.065500, p.GridUseDollarsPerKWH, 1e-6)

		p2027, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.085400, p2027.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.065500, p2027.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Texas TOU peak surcharge, monthly fuel factor, and PUCT riders", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_tx_tou",
		})
		require.NoError(t, err)

		// January 2026: Fuel $0.016221 + EECRF $0.001382 + DCRF $0.005740 = $0.023343
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.082251, pJan.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.023343, pJan.GridUseDollarsPerKWH, 1e-6)

		// July 9, 2026 (after Apr 1 GCRR $0.003117, before Jul 10 TCRF):
		// Fuel $0.009824 + EECRF $0.001382 + DCRF $0.005740 + GCRR $0.003117 = $0.020063
		pJul9, err := u.priceForTime(time.Date(2026, time.July, 9, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.25826, pJul9.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.020063, pJul9.GridUseDollarsPerKWH, 1e-6)

		// July 15, 2026 (after Jul 10 TCRF $0.004428): $0.020063 + $0.004428 = $0.024491
		pJul15, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.25826, pJul15.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.024491, pJul15.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Texas Standard service, monthly fuel factor, and 2027 carry-forward", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_tx_standard",
		})
		require.NoError(t, err)

		pJul, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.114967, pJul.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.024491, pJul.GridUseDollarsPerKWH, 1e-6)

		// November 2026 and 2027 carry-forward: Fuel $0.018083 + $0.014667 riders = $0.032750
		pNov, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098842, pNov.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.032750, pNov.GridUseDollarsPerKWH, 1e-6)

		p2027, err := u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.098842, p2027.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.032750, p2027.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Continuous confirmed prices across mid-month, DST, and 2027 boundaries", func(t *testing.T) {
		ctx := context.Background()

		// CO mid-month Oct 19, 2026 transition
		uCO := &genericTOU{}
		require.NoError(t, uCO.ApplySettings(ctx, types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_co_tou",
		}))
		coPrices, err := uCO.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.October, 18, 0, 0, 0, 0, mtLocation),
			time.Date(2026, time.October, 20, 0, 0, 0, 0, mtLocation),
		)
		require.NoError(t, err)
		assert.Len(t, coPrices, 48)

		// TX mid-month Jul 10, 2026 TCRF transition
		uTX := &genericTOU{}
		require.NoError(t, uTX.ApplySettings(ctx, types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_tx_tou",
		}))
		txPrices, err := uTX.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.July, 9, 0, 0, 0, 0, ctLocation),
			time.Date(2026, time.July, 11, 0, 0, 0, 0, ctLocation),
		)
		require.NoError(t, err)
		assert.Len(t, txPrices, 48)

		// MI Nov 1, 2026 PSCR + Fall Back DST transition (25 hours on Nov 1)
		uMI := &genericTOU{}
		require.NoError(t, uMI.ApplySettings(ctx, types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mi_tou",
		}))
		miPrices, err := uMI.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.October, 31, 0, 0, 0, 0, ctLocation),
			time.Date(2026, time.November, 2, 0, 0, 0, 0, ctLocation),
		)
		require.NoError(t, err)
		assert.Len(t, miPrices, 49)

		// MN 2026 -> 2027 year-end rollover
		uMN := &genericTOU{}
		require.NoError(t, uMN.ApplySettings(ctx, types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_tou",
		}))
		mnPrices, err := uMN.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.December, 31, 0, 0, 0, 0, ctLocation),
			time.Date(2027, time.January, 2, 0, 0, 0, 0, ctLocation),
		)
		require.NoError(t, err)
		assert.Len(t, mnPrices, 48)
	})
}

func TestXcelExportRates(t *testing.T) {
	t.Run("Minnesota Occasional Delivery pre-April, post-April A51, and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "mn_occasional",
			},
		})
		require.NoError(t, err)

		pMar, err := u.priceForTime(time.Date(2026, time.March, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pMar.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0360, pMar.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pApr, err := u.priceForTime(time.Date(2026, time.April, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pApr.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03572, pApr.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pJul, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pJul.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03794, pJul.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p2027Winter, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, p2027Winter.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03572, p2027Winter.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Minnesota Time of Delivery pre-April, post-April A52, and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "mn_time_of_delivery",
			},
		})
		require.NoError(t, err)

		pMarPeak, err := u.priceForTime(time.Date(2026, time.March, 16, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pMarPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0527, pMarPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pAprPeak, err := u.priceForTime(time.Date(2026, time.April, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pAprPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.04307, pAprPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pAprOff, err := u.priceForTime(time.Date(2026, time.April, 15, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pAprOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03186, pAprOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pJulPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pJulPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05143, pJulPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pJulOff, err := u.priceForTime(time.Date(2026, time.July, 15, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pJulOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03065, pJulOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p2027SummerPeak, err := u.priceForTime(time.Date(2027, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, p2027SummerPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05143, p2027SummerPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Michigan DG-1 Supply plus PSCR and 1:1 Net Metering", func(t *testing.T) {
		uStd := &genericTOU{}
		err := uStd.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mi_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "mi_dg",
			},
		})
		require.NoError(t, err)

		// Pre-Nov 1, 2026: $0.09425 - $0.01009 = $0.08416
		pStdOct, err := uStd.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pStdOct.SeparateGenerationCredit) {
			assert.InDelta(t, 0.08416, pStdOct.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// From Nov 1, 2026 through 2027: $0.09425 - $0.02500 = $0.06925
		pStdNov, err := uStd.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pStdNov.SeparateGenerationCredit) {
			assert.InDelta(t, 0.06925, pStdNov.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pStd2027, err := uStd.priceForTime(time.Date(2027, time.March, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pStd2027.SeparateGenerationCredit) {
			assert.InDelta(t, 0.06925, pStd2027.GenerationCreditDollarsPerKWH, 1e-6)
		}

		uTOU := &genericTOU{}
		err = uTOU.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mi_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "mi_dg",
				PeakPeriodOption:  "3", // 8 AM - 8 PM
			},
		})
		require.NoError(t, err)

		pTOUOctPeak, err := uTOU.priceForTime(time.Date(2026, time.October, 15, 9, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pTOUOctPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.15061, pTOUOctPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pTOUNovPeak, err := uTOU.priceForTime(time.Date(2026, time.November, 16, 9, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pTOUNovPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.13570, pTOUNovPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pTOUNovOff, err := uTOU.priceForTime(time.Date(2026, time.November, 16, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pTOUNovOff.SeparateGenerationCredit) {
			assert.InDelta(t, 0.01520, pTOUNovOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		uNet := &genericTOU{}
		err = uNet.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mi_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNet, err := uNet.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.False(t, pNet.SeparateGenerationCredit)
	})

	t.Run("Wisconsin PG-2B 09:00-21:00 NSP-holiday weekday peaks and seasonal credits", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_wi_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "wi_pg2b",
			},
		})
		require.NoError(t, err)

		// 8 AM weekday is off-peak ($0.041632) under 09:00-21:00 window
		p8AM, err := u.priceForTime(time.Date(2026, time.July, 15, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, p8AM.SeparateGenerationCredit) {
			assert.InDelta(t, 0.041632, p8AM.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 10 AM summer weekday is on-peak ($0.064768)
		pSummerPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pSummerPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.064768, pSummerPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// July 3, 2026 (observed Independence Day NSP holiday) is off-peak ($0.041632)
		pHoliday, err := u.priceForTime(time.Date(2026, time.July, 3, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pHoliday.SeparateGenerationCredit) {
			assert.InDelta(t, 0.041632, pHoliday.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Friday after Thanksgiving (2026-11-27) is on-peak ($0.051916) under NSP holiday calendar
		pBlackFriday, err := u.priceForTime(time.Date(2026, time.November, 27, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pBlackFriday.SeparateGenerationCredit) {
			assert.InDelta(t, 0.051916, pBlackFriday.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p2027WinterPeak, err := u.priceForTime(time.Date(2027, time.January, 15, 10, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, p2027WinterPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.051916, p2027WinterPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("North Dakota Net Energy Billing", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_nd_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nd_net_energy_billing",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03646, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("North Dakota Time of Day Purchase summer on-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_nd_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nd_time_of_day_purchase",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation) // 10 AM weekday (on-peak)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.05143, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("North Dakota Time of Day Purchase winter off-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_nd_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nd_time_of_day_purchase",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.December, 15, 22, 0, 0, 0, ctLocation) // 10 PM winter weekday (off-peak)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.03186, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("South Dakota Occasional Delivery", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sd_occasional",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0316, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("South Dakota Time of Delivery on-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sd_time_of_delivery",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation) // 10 AM weekday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0397, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("South Dakota Time of Delivery off-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sd_time_of_delivery",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 23, 0, 0, 0, ctLocation) // 11 PM weekday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0272, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Texas Net Billing monthly factors and 2027 carry-forward", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_tx_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "tx_net_billing",
			},
		})
		require.NoError(t, err)

		// January (defined)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pJan.SeparateGenerationCredit) {
			assert.InDelta(t, 0.016221, pJan.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// July (defined)
		pJuly, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pJuly.SeparateGenerationCredit) {
			assert.InDelta(t, 0.009824, pJuly.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// August (defined)
		pAug, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pAug.SeparateGenerationCredit) {
			assert.InDelta(t, 0.009824, pAug.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// September (defined)
		pSep, err := u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pSep.SeparateGenerationCredit) {
			assert.InDelta(t, 0.018083, pSep.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// October (defined)
		pOct, err := u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pOct.SeparateGenerationCredit) {
			assert.InDelta(t, 0.018083, pOct.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// November (defined)
		pNov, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pNov.SeparateGenerationCredit) {
			assert.InDelta(t, 0.018083, pNov.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// December (fallback to November) and 2027 carry-forward
		pDec, err := u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, pDec.SeparateGenerationCredit) {
			assert.InDelta(t, 0.018083, pDec.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p2027Jan, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, p2027Jan.SeparateGenerationCredit) {
			assert.InDelta(t, 0.018083, p2027Jan.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p2027Jul, err := u.priceForTime(time.Date(2027, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.True(t, p2027Jul.SeparateGenerationCredit) {
			assert.InDelta(t, 0.018083, p2027Jul.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})
}
