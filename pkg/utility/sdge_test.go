package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSDGEHolidays(t *testing.T) {
	t.Run("SDG&E Holiday shifts", func(t *testing.T) {
		// 2026 Veterans Day Nov 11 is Wednesday -> remains Nov 11
		holidays2026 := getSDGEHolidays(2026)
		assert.Contains(t, holidays2026, "2026-11-11")

		// 2027 Independence Day July 4th is Sunday -> observed Monday July 5th
		holidays2027 := getSDGEHolidays(2027)
		if assert.Contains(t, holidays2027, "2027-07-05") {
			assert.NotContains(t, holidays2027, "2027-07-04")
		}

		// Memorial Day and Presidents' Day
		assert.Contains(t, holidays2026, "2026-05-25") // Memorial Day 2026
		assert.Contains(t, holidays2026, "2026-02-16") // Presidents' Day 2026
	})
}

func TestSDGERates(t *testing.T) {
	loc := ptLocation

	u := &genericTOU{}

	t.Run("Bundled EV-TOU-5 Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou_5",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (July 7, 2026 17:00, Tuesday)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.32682 + NBC 0.00591 + EECC 0.47019 = 0.80292
		assert.InDelta(t, 0.80292, p1.DollarsPerKWH, 0.0001)

		// Summer Weekday Super Off-Peak (July 7, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.04114 + NBC 0.00591 + EECC 0.08147 = 0.12852
		assert.InDelta(t, 0.12852, p2.DollarsPerKWH, 0.0001)

		// Winter Weekday On-Peak (Jan 6, 2026 17:00, Tuesday)
		p3, err := u.priceForTime(time.Date(2026, 1, 6, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.32682 + NBC 0.00591 + EECC 0.19990 = 0.53263
		assert.InDelta(t, 0.53263, p3.DollarsPerKWH, 0.0001)
	})

	t.Run("SDCP PowerOn EV-TOU-5 Rates (San Diego Location)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou_5",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdcp_power_on",
				Location:       "san_diego",
			},
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (July 7, 2026 17:00, Tuesday)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.32682 + NBC 0.00591 + PCIA 0.04987 + SDCP 0.41063 = 0.79323
		assert.InDelta(t, 0.79323, p1.DollarsPerKWH, 0.0001)

		// Summer Weekday Super Off-Peak (July 7, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.04114 + NBC 0.00591 + PCIA 0.04987 + SDCP 0.04168 = 0.13860
		assert.InDelta(t, 0.13860, p2.DollarsPerKWH, 0.0001)
	})

	t.Run("SDCP PowerBase EV-TOU-5 Rates (Unincorporated Location)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou_5",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdcp_power_base",
				Location:       "unincorporated",
			},
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (July 7, 2026 17:00, Tuesday)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.32682 + NBC 0.00591 + PCIA 0.04987 + SDCP 0.38801 = 0.77061
		assert.InDelta(t, 0.77061, p1.DollarsPerKWH, 0.0001)
	})

	t.Run("SDCP Power100 EV-TOU-5 Rates (San Diego Location)", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou_5",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdcp_power_100",
				Location:       "san_diego",
			},
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (July 7, 2026 17:00, Tuesday)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.32682 + NBC 0.00591 + PCIA 0.04987 + (SDCP PowerOn 0.41063 + Premium 0.01) = 0.80323
		assert.InDelta(t, 0.80323, p1.DollarsPerKWH, 0.0001)
	})

	t.Run("TOU-DR2 2-Period TOU Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_dr2",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (July 7, 2026 17:00)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.34510 + NBC 0.00591 + EECC 0.34920 = 0.70021
		assert.InDelta(t, 0.70021, p1.DollarsPerKWH, 0.0001)

		// Summer Weekday Off-Peak (July 7, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.33863 + NBC 0.00591 + EECC 0.08432 = 0.42886
		assert.InDelta(t, 0.42886, p2.DollarsPerKWH, 0.0001)
	})
}
