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
		// Expected: UDC 0.32682 + EECC 0.47019 = 0.79701 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.79701, p1.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p1.GridUseDollarsPerKWH, 0.0001)

		// Summer Weekday Super Off-Peak (July 7, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.04114 + EECC 0.08147 = 0.12261 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.12261, p2.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p2.GridUseDollarsPerKWH, 0.0001)

		// Winter Weekday On-Peak (Jan 6, 2026 17:00, Tuesday)
		p3, err := u.priceForTime(time.Date(2026, 1, 6, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.32682 + EECC 0.19990 = 0.52672 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.52672, p3.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p3.GridUseDollarsPerKWH, 0.0001)
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
		// Expected: UDC 0.32682 + PCIA 0.04987 + SDCP 0.41063 = 0.78732 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.78732, p1.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p1.GridUseDollarsPerKWH, 0.0001)

		// Summer Weekday Super Off-Peak (July 7, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.04114 + PCIA 0.04987 + SDCP 0.04168 = 0.13269 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.13269, p2.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p2.GridUseDollarsPerKWH, 0.0001)
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
		// Expected: UDC 0.32682 + PCIA 0.04987 + SDCP 0.38801 = 0.76470 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.76470, p1.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p1.GridUseDollarsPerKWH, 0.0001)
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
		// Expected: UDC 0.32682 + PCIA 0.04987 + (SDCP PowerOn 0.41063 + Premium 0.01) = 0.79732 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.79732, p1.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p1.GridUseDollarsPerKWH, 0.0001)
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

		// Summer Weekday On-Peak Pre-Aug 2026 (July 7, 2026 17:00)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.34510 + EECC 0.34920 = 0.69430 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.69430, p1.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p1.GridUseDollarsPerKWH, 0.0001)

		// Summer Weekday Off-Peak Pre-Aug 2026 (July 7, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.33863 + EECC 0.08432 = 0.42295 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.42295, p2.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.00591, p2.GridUseDollarsPerKWH, 0.0001)

		// Summer Weekday On-Peak Post-Aug 1, 2026 (August 2, 2026 17:00)
		p3, err := u.priceForTime(time.Date(2026, 8, 2, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.33063 + EECC 0.35943 = 0.69006 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.69006, p3.DollarsPerKWH, 0.0001)

		// Summer Weekday Off-Peak Post-Aug 1, 2026 (August 2, 2026 11:00 AM)
		p4, err := u.priceForTime(time.Date(2026, 8, 2, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.32397 + EECC 0.08678 = 0.41075 (NBC 0.00591 as GridUse)
		assert.InDelta(t, 0.41075, p4.DollarsPerKWH, 0.0001)
	})

	t.Run("August 1 2026 EV-TOU-5 & Weekend Super Off-Peak", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou_5",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		})
		require.NoError(t, err)

		// Post-Aug 1, 2026 On-Peak (August 2, 2026 17:00)
		p1, err := u.priceForTime(time.Date(2026, 8, 2, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.31218 + EECC 0.48396 = 0.79614
		assert.InDelta(t, 0.79614, p1.DollarsPerKWH, 0.0001)

		// Post-Aug 1, 2026 Super Off-Peak (August 2, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 8, 2, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected: UDC 0.04114 + EECC 0.08385 = 0.12499
		assert.InDelta(t, 0.12499, p2.DollarsPerKWH, 0.0001)

		// Saturday August 1, 2026 01:00 AM (Weekend Midnight - 2:00 PM) -> Super Off-Peak
		p3, err := u.priceForTime(time.Date(2026, 8, 1, 1, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p3.PeriodName)
		assert.InDelta(t, 0.12499, p3.DollarsPerKWH, 0.0001)
	})

	t.Run("August 1 2026 Schedule DR-SES Rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_dr_ses",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		})
		require.NoError(t, err)

		// Post-Aug 1, 2026 On-Peak (August 2, 2026 17:00)
		p1, err := u.priceForTime(time.Date(2026, 8, 2, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.25957 + EECC 0.48396 = 0.74353
		assert.InDelta(t, 0.74353, p1.DollarsPerKWH, 0.0001)

		// Post-Aug 1, 2026 Super Off-Peak (August 2, 2026 11:00 AM)
		p2, err := u.priceForTime(time.Date(2026, 8, 2, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		// Expected UDC 0.25957 + EECC 0.08385 = 0.34342
		assert.InDelta(t, 0.34342, p2.DollarsPerKWH, 0.0001)
	})
}

func TestGetSDGENBTExportRate(t *testing.T) {
	loc := ptLocation

	t.Run("Weekday, Weekend, Holiday rates in 2026", func(t *testing.T) {
		// New Year's Day (Jan 1, 2026) is a Thursday but observed holiday -> Weekend rates
		rate1 := getSDGENBTExportRate(time.Date(2026, 1, 1, 0, 0, 0, 0, loc))
		assert.InDelta(t, 0.087919, rate1, 0.000001)

		// Jan 2, 2026 (Friday) -> Weekday rates
		rate2 := getSDGENBTExportRate(time.Date(2026, 1, 2, 0, 0, 0, 0, loc))
		assert.InDelta(t, 0.088830, rate2, 0.000001)

		// April 4, 2026 (Saturday) -> Weekend rates
		rate3 := getSDGENBTExportRate(time.Date(2026, 4, 4, 0, 0, 0, 0, loc))
		assert.InDelta(t, 0.064273, rate3, 0.000001)
	})
}

func TestSDGENBTPeriods(t *testing.T) {
	t.Run("NEM 3.0 Export Credit Periods", func(t *testing.T) {
		// Get fees with NetMeteringScheme set to "sbp"
		opts := types.UtilityRateOptions{
			NetMeteringScheme: "sbp",
			GenerationRate:    "sdge",
		}
		periods := sdgePeriods("sdge_ev_tou_5", opts, []int{2026})

		// Assert that we have dynamic NBT export credit periods
		foundNBT := false
		for _, p := range periods {
			if p.SeparateGenerationCredit && p.Description == "SDG&E NBT Weekday Export Credit (Hour 0)" {
				foundNBT = true
				// Expected weekday April Hour 0 NBT export rate is 0.073861
				if p.Start.Month() == time.April {
					assert.InDelta(t, 0.073861, p.DollarsPerKWH, 0.000001)
				}
			}
		}
		assert.True(t, foundNBT, "expected to find SDG&E NBT Weekday Export Credit period")
	})
}
