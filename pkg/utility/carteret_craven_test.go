package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCarteretCravenUtility(t *testing.T) {
	ctx := context.Background()

	t.Run("Schedule R Seasonal Rates and Default Rider RQF", func(t *testing.T) {
		u := &genericTOU{}
		settings := types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r",
		}
		err := u.ApplySettings(ctx, settings)
		require.NoError(t, err)

		// Summer (July 15) -> $0.1103/kWh, Buy-All / Sell-All Rider RQF $0.0407/kWh
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1103, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Base", p.PeriodName)
			assert.True(t, p.BuyAllSellAll)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0407, p.ExportRateDollars(settings, nil), 1e-6)
			assert.True(t, p.SeparateBatteryGenerationCredit)
			assert.InDelta(t, 0.0, p.BatteryExportRateDollars(settings, nil), 1e-6)
		}

		// Summer boundary (April 1 and October 31) -> $0.1103/kWh
		p, err = u.priceForTime(time.Date(2026, time.April, 1, 0, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1103, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.October, 31, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1103, p.DollarsPerKWH, 1e-6)
		}

		// Non-Summer (November 1, January 15, March 31) -> $0.1006/kWh
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1006, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1006, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 31, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1006, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Schedule R No Export Credit Option", func(t *testing.T) {
		u := &genericTOU{}
		settings := types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "none",
			},
		}
		err := u.ApplySettings(ctx, settings)
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1103, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.BuyAllSellAll)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.ExportRateDollars(settings, nil), 1e-6)
			assert.InDelta(t, 0.0, p.BatteryExportRateDollars(settings, nil), 1e-6)
		}
	})

	t.Run("Schedule R-TU-E Summer Rates Including Weekends and Holidays", func(t *testing.T) {
		u := &genericTOU{}
		settings := types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r_tu_e",
		}
		err := u.ApplySettings(ctx, settings)
		require.NoError(t, err)

		// Weekday On-Peak: Wed Jul 15, 2026 3:00 PM -> $0.4000
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.False(t, p.BuyAllSellAll)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.ExportRateDollars(settings, nil), 1e-6)
		}

		// Weekend On-Peak (no exclusions): Sun Jul 19, 2026 5:00 PM -> $0.4000
		p, err = u.priceForTime(time.Date(2026, time.July, 19, 17, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Holiday On-Peak (no exclusions): Sat Jul 4, 2026 4:00 PM -> $0.4000
		p, err = u.priceForTime(time.Date(2026, time.July, 4, 16, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// On-Peak end exclusive: 6:00 PM -> Off-Peak $0.0650
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 18, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0650, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Off-Peak daytime: 10:00 AM -> $0.0650
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0650, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Super Off-Peak evening: 10:00 PM -> $0.0550
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 22, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0550, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
		}

		// Super Off-Peak early morning: 4:00 AM -> $0.0550
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 4, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0550, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
		}

		// Super Off-Peak end exclusive: 5:00 AM -> Off-Peak $0.0650
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 5, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0650, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}
	})

	t.Run("Schedule R-TU-E Winter Rates Including Weekends and Holidays", func(t *testing.T) {
		u := &genericTOU{}
		settings := types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r_tu_e",
		}
		err := u.ApplySettings(ctx, settings)
		require.NoError(t, err)

		// Winter On-Peak weekday: Mon Nov 16, 2026 7:00 AM -> $0.4000
		p, err := u.priceForTime(time.Date(2026, time.November, 16, 7, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Winter On-Peak weekend (no exclusions): Sat Jan 16, 2027 8:00 AM -> $0.4000
		p, err = u.priceForTime(time.Date(2027, time.January, 16, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Winter On-Peak Christmas Day (no exclusions): Fri Dec 25, 2026 8:00 AM -> $0.4000
		p, err = u.priceForTime(time.Date(2026, time.December, 25, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Winter Off-Peak end of morning peak: 9:00 AM -> $0.0650
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 9, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0650, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Winter afternoon 4:00 PM is Off-Peak (not Summer On-Peak) -> $0.0650
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 16, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0650, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Winter Super Off-Peak 11:00 PM -> $0.0550
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0550, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
		}
	})

	t.Run("Schedule R-TU-E With Rider RQF Buy-All Sell-All", func(t *testing.T) {
		u := &genericTOU{}
		settings := types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r_tu_e",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rqf",
			},
		}
		err := u.ApplySettings(ctx, settings)
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4000, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.BuyAllSellAll)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0407, p.ExportRateDollars(settings, nil), 1e-6)
			assert.True(t, p.SeparateBatteryGenerationCredit)
			assert.InDelta(t, 0.0, p.BatteryExportRateDollars(settings, nil), 1e-6)
		}
	})

	t.Run("Schedule R-TU Frozen Demand Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r_tu",
		})
		require.NoError(t, err)

		// Summer On-Peak (3-6 PM) -> $0.0545, PeriodName "On-Peak"
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0545, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Summer Off-Peak -> $0.0545, PeriodName "Off-Peak"
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0545, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Winter On-Peak (7-9 AM) -> $0.0545, PeriodName "On-Peak"
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 7, 30, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0545, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}
	})

	t.Run("Schedule R-EVTU Frozen EV Demand Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r_evtu",
		})
		require.NoError(t, err)

		// Summer On-Peak (3-6 PM) -> $0.0570
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0570, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Summer Off-Peak (10 AM) -> $0.0570
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0570, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Super Off-Peak (11 PM) -> $0.0460
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0460, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
		}

		// Winter On-Peak (8 AM) -> $0.0570
		p, err = u.priceForTime(time.Date(2027, time.February, 10, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0570, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}
	})

	t.Run("Map Site Integration", func(t *testing.T) {
		m := NewMap(nil)
		u, err := m.Site(ctx, "site-ccec", types.Settings{
			UtilityProvider: "carteret_craven",
			UtilityRate:     "carteret_craven_r_tu_e",
		})
		require.NoError(t, err)
		assert.Equal(t, "carteret_craven_r_tu_e", u.Name())

		periods, err := u.GetPeriods(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		future, err := u.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, future)
	})
}
