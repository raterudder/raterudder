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
	t.Run("Colorado TOU pricing summer on-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_co_tou",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 16, 0, 0, 0, mtLocation) // 4 PM
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.16430, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Minnesota Standard summer pricing", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_standard",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.13069, p.DollarsPerKWH, 1e-6)
	})

	t.Run("South Dakota Standard flat winter rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_sd_standard",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.December, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.09585, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Michigan TOU supply and delivery", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mi_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				PeakPeriodOption: "3", // 8 AM - 8 PM
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 9, 0, 0, 0, ctLocation) // 9 AM weekday (on-peak)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.1607, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04801, p.GridUseDollarsPerKWH, 1e-6)
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
	})

	t.Run("Texas TOU peak surcharge and fuel cost recovery factor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_tx_tou",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 14, 0, 0, 0, ctLocation) // 2 PM weekday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.25826, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.014978, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Texas Standard service and fuel cost recovery factor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_tx_standard",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.114967, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.014978, p.GridUseDollarsPerKWH, 1e-6)
	})
}

func TestXcelExportRates(t *testing.T) {
	t.Run("Minnesota Occasional Delivery", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_standard",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "mn_occasional",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0360, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Minnesota Time of Delivery on-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_mn_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "mn_time_of_delivery",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 10, 0, 0, 0, ctLocation) // 10 AM weekday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0527, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Wisconsin PG-2B summer on-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "xcel",
			UtilityRate:     "xcel_wi_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "wi_pg2b",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 8, 0, 0, 0, ctLocation) // 8 AM weekday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.07798, p.GenerationCreditDollarsPerKWH, 1e-6)
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

	t.Run("Texas Net Billing fallback", func(t *testing.T) {
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
		targetJan := time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation)
		pJan, err := u.priceForTime(targetJan)
		require.NoError(t, err)
		if assert.True(t, pJan.SeparateGenerationCredit) {
			assert.InDelta(t, 0.016221, pJan.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// July (undefined, fallback to May)
		targetJuly := time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation)
		pJuly, err := u.priceForTime(targetJuly)
		require.NoError(t, err)
		if assert.True(t, pJuly.SeparateGenerationCredit) {
			assert.InDelta(t, 0.009824, pJuly.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})
}
