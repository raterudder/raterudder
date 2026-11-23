package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHawaiiRates(t *testing.T) {
	loc := hstLocation // Pacific/Honolulu
	u := &genericTOU{}

	t.Run("HECO Oahu Schedule R Flat Rate", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "heco",
			UtilityRate:     "heco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sre_export",
			},
		})
		require.NoError(t, err)

		pApr, err := u.priceForTime(time.Date(2026, 4, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.412922, pApr.DollarsPerKWH, 0.000001)

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.440551, p1.DollarsPerKWH, 0.000001)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("HELCO Hawaii Schedule R Flat Rate", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "helco",
			UtilityRate:     "helco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sre_export",
			},
		})
		require.NoError(t, err)

		pApr, err := u.priceForTime(time.Date(2026, 4, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.402245, pApr.DollarsPerKWH, 0.000001)

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.464137, p1.DollarsPerKWH, 0.000001)
	})

	t.Run("MECO Maui Schedule R Flat Rate", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "maui",
			},
		})
		require.NoError(t, err)

		pApr, err := u.priceForTime(time.Date(2026, 4, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.395134, pApr.DollarsPerKWH, 0.000001)

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.450632, p1.DollarsPerKWH, 0.000001)
	})

	t.Run("HECO Oahu Schedule ARD TOU R Import Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "heco",
			UtilityRate:     "heco_ard_tou_r",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["Daytime"])
		assert.True(t, names["Evening Peak"])
		assert.True(t, names["Overnight"])

		// April 2026 checks
		pAprDay, err := u.priceForTime(time.Date(2026, 4, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.211966, pAprDay.DollarsPerKWH, 0.000001)

		pAprEve, err := u.priceForTime(time.Date(2026, 4, 7, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.623298, pAprEve.DollarsPerKWH, 0.000001)

		pAprNight, err := u.priceForTime(time.Date(2026, 4, 7, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.417632, pAprNight.DollarsPerKWH, 0.000001)

		// July 2026 Daytime (12:00 PM)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.225487, p1.DollarsPerKWH, 0.000001)

		// July 2026 Evening Peak (7:00 PM / 19:00)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.668588, p2.DollarsPerKWH, 0.000001)

		// July 2026 Overnight (midnight)
		p3, err := u.priceForTime(time.Date(2026, 7, 7, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.447037, p3.DollarsPerKWH, 0.000001)
	})

	t.Run("November 2026 and 2027 Carry-Forward Oahu Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "heco",
			UtilityRate:     "heco_r",
		})
		require.NoError(t, err)

		pNovR, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.486472, pNovR.DollarsPerKWH, 0.000001)

		pDecR, err := u.priceForTime(time.Date(2026, 12, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.486472, pDecR.DollarsPerKWH, 0.000001)

		p2027R, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.486472, p2027R.DollarsPerKWH, 0.000001)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "heco",
			UtilityRate:     "heco_ard_tou_r",
		})
		require.NoError(t, err)

		pNovDay, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.247695, pNovDay.DollarsPerKWH, 0.000001)

		pNovEve, err := u.priceForTime(time.Date(2026, 11, 15, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.744196, pNovEve.DollarsPerKWH, 0.000001)

		pNovNight, err := u.priceForTime(time.Date(2026, 11, 15, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.495945, pNovNight.DollarsPerKWH, 0.000001)

		p2027Day, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.247695, p2027Day.DollarsPerKWH, 0.000001)
	})

	t.Run("November 2026 and 2027 Carry-Forward Hawaii Island Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "helco",
			UtilityRate:     "helco_r",
		})
		require.NoError(t, err)

		pNovR, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.442048, pNovR.DollarsPerKWH, 0.000001)

		p2027R, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.442048, p2027R.DollarsPerKWH, 0.000001)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "helco",
			UtilityRate:     "helco_ard_tou_r",
		})
		require.NoError(t, err)

		pNovDay, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.250482, pNovDay.DollarsPerKWH, 0.000001)

		pNovEve, err := u.priceForTime(time.Date(2026, 11, 15, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.764635, pNovEve.DollarsPerKWH, 0.000001)

		pNovNight, err := u.priceForTime(time.Date(2026, 11, 15, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.507558, pNovNight.DollarsPerKWH, 0.000001)

		p2027Eve, err := u.priceForTime(time.Date(2027, 6, 15, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.764635, p2027Eve.DollarsPerKWH, 0.000001)
	})

	t.Run("November 2026 and 2027 Carry-Forward Maui Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "maui",
			},
		})
		require.NoError(t, err)

		pNovR, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.470357, pNovR.DollarsPerKWH, 0.000001)

		p2027R, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.470357, p2027R.DollarsPerKWH, 0.000001)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_ard_tou_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "maui",
			},
		})
		require.NoError(t, err)

		pNovDay, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.258069, pNovDay.DollarsPerKWH, 0.000001)

		pNovEve, err := u.priceForTime(time.Date(2026, 11, 15, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.774114, pNovEve.DollarsPerKWH, 0.000001)

		pNovNight, err := u.priceForTime(time.Date(2026, 11, 15, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.516091, pNovNight.DollarsPerKWH, 0.000001)

		p2027Night, err := u.priceForTime(time.Date(2027, 6, 15, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.516091, p2027Night.DollarsPerKWH, 0.000001)
	})

	t.Run("November 2026 and 2027 Carry-Forward Lanai Rates and SRE Export Spread", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "lanai",
			},
		})
		require.NoError(t, err)

		pNovR, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.581504, pNovR.DollarsPerKWH, 0.000001)

		p2027R, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.581504, p2027R.DollarsPerKWH, 0.000001)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_ard_tou_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:          "lanai",
				NetMeteringScheme: "sre_export",
			},
		})
		require.NoError(t, err)

		pNovDay, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.307968, pNovDay.DollarsPerKWH, 0.000001)
		assert.InDelta(t, 0.267, pNovDay.GenerationCreditDollarsPerKWH, 0.000001)
		assert.Greater(t, pNovDay.DollarsPerKWH, pNovDay.GenerationCreditDollarsPerKWH)

		pNovEve, err := u.priceForTime(time.Date(2026, 11, 15, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.919721, pNovEve.DollarsPerKWH, 0.000001)

		pNovNight, err := u.priceForTime(time.Date(2026, 11, 15, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.613845, pNovNight.DollarsPerKWH, 0.000001)

		pDecDay, err := u.priceForTime(time.Date(2026, 12, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.307968, pDecDay.DollarsPerKWH, 0.000001)
		assert.Greater(t, pDecDay.DollarsPerKWH, pDecDay.GenerationCreditDollarsPerKWH)

		p2027Day, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.307968, p2027Day.DollarsPerKWH, 0.000001)
		assert.Greater(t, p2027Day.DollarsPerKWH, p2027Day.GenerationCreditDollarsPerKWH)
	})

	t.Run("November 2026 and 2027 Carry-Forward Molokai Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "molokai",
			},
		})
		require.NoError(t, err)

		pNovR, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.505740, pNovR.DollarsPerKWH, 0.000001)

		p2027R, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.505740, p2027R.DollarsPerKWH, 0.000001)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_ard_tou_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "molokai",
			},
		})
		require.NoError(t, err)

		pNovDay, err := u.priceForTime(time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.275323, pNovDay.DollarsPerKWH, 0.000001)

		pNovEve, err := u.priceForTime(time.Date(2026, 11, 15, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.821785, pNovEve.DollarsPerKWH, 0.000001)

		pNovNight, err := u.priceForTime(time.Date(2026, 11, 15, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.548554, pNovNight.DollarsPerKWH, 0.000001)

		p2027Day, err := u.priceForTime(time.Date(2027, 6, 15, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.275323, p2027Day.DollarsPerKWH, 0.000001)
	})

	t.Run("Export Credits SRE Export (HELCO)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "helco",
			UtilityRate:     "helco_ard_tou_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sre_export",
			},
		})
		require.NoError(t, err)

		// Daytime credit (12:00 PM): 10.6¢
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.106, p1.GenerationCreditDollarsPerKWH, 0.00001)

		// Evening Peak credit (7:00 PM): 23.1¢
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.231, p2.GenerationCreditDollarsPerKWH, 0.00001)

		// Overnight credit (midnight): 14.8¢
		p3, err := u.priceForTime(time.Date(2026, 7, 7, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.148, p3.GenerationCreditDollarsPerKWH, 0.00001)
	})

	t.Run("Export Credits CGS Plus (MECO Lanai)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meco",
			UtilityRate:     "meco_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:          "lanai",
				NetMeteringScheme: "cgs_plus",
			},
		})
		require.NoError(t, err)

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.2080, p1.GenerationCreditDollarsPerKWH, 0.00001)
	})

	t.Run("Export Credits Smart Export (Oahu)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "heco",
			UtilityRate:     "heco_ard_tou_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "smart_export",
			},
		})
		require.NoError(t, err)

		// Daytime credit (12:00 PM): 0.0
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.0, p1.GenerationCreditDollarsPerKWH, 0.00001)

		// Evening credit (7:00 PM): 14.97¢
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.1497, p2.GenerationCreditDollarsPerKWH, 0.00001)
	})
}
