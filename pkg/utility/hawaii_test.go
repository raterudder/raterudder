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

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.412922, p1.DollarsPerKWH, 0.00001)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
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

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.402245, p1.DollarsPerKWH, 0.00001)
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

		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.395134, p1.DollarsPerKWH, 0.00001)
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

		// Daytime (e.g. 12:00 PM)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 12, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.211966, p1.DollarsPerKWH, 0.00001)

		// Evening Peak (e.g. 7:00 PM / 19:00)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 19, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.623298, p2.DollarsPerKWH, 0.00001)

		// Overnight (e.g. midnight)
		p3, err := u.priceForTime(time.Date(2026, 7, 7, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.417632, p3.DollarsPerKWH, 0.00001)
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
