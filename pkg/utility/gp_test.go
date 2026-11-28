package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPHolidays(t *testing.T) {
	t.Run("Independence Day observed shifts", func(t *testing.T) {
		// 2026 July 4th is Saturday -> observed Friday July 3rd
		holidays2026 := getGPHolidays(2026)
		assert.Contains(t, holidays2026, "2026-07-03")
		assert.NotContains(t, holidays2026, "2026-07-04")

		// 2027 July 4th is Sunday -> observed Monday July 5th
		holidays2027 := getGPHolidays(2027)
		assert.Contains(t, holidays2027, "2027-07-05")
		assert.NotContains(t, holidays2027, "2027-07-04")

		// 2025 July 4th is Friday -> remains Friday July 4th
		holidays2025 := getGPHolidays(2025)
		assert.Contains(t, holidays2025, "2025-07-04")
	})

	t.Run("Labor Day first Monday in September", func(t *testing.T) {
		// 2026 Labor Day is Sep 7
		holidays2026 := getGPHolidays(2026)
		assert.Contains(t, holidays2026, "2026-09-07")

		// 2027 Labor Day is Sep 6
		holidays2027 := getGPHolidays(2027)
		assert.Contains(t, holidays2027, "2027-09-06")
	})
}

func TestGeorgiaPowerRates(t *testing.T) {
	u := &genericTOU{}
	const (
		outsideCityMFFMultiplier = 1.011995
		insideCityMFFMultiplier  = 1.030843
		baseRidersJan26          = 1.144508 // 1 + 0.132343 (ECCR-14) + 0.012165 (DSM-R-15)
		baseRidersJun26          = 1.142174 // 1 + 0.130205 (ECCR-15) + 0.011969 (DSM-R-16)
	)

	t.Run("UtilityInfo registers four plans with location and netMeteringScheme options", func(t *testing.T) {
		info := gpUtilityInfo()
		assert.Equal(t, "gp", info.ID)
		if assert.Len(t, info.Rates, 4) {
			assert.Equal(t, "gp_r", info.Rates[0].ID)
			assert.Equal(t, "R (Residential Service)", info.Rates[0].Name)
			assert.Equal(t, "gp_tou_oa", info.Rates[1].ID)
			assert.Equal(t, "TOU-OA (Overnight Advantage)", info.Rates[1].Name)
			assert.Equal(t, "gp_tou_rd", info.Rates[2].ID)
			assert.Equal(t, "TOU-RD (Residential Demand / Smart Usage)", info.Rates[2].Name)
			assert.Equal(t, "gp_tou_reo", info.Rates[3].ID)
			assert.Equal(t, "TOU-REO (Nights & Weekends)", info.Rates[3].Name)

			for _, rate := range info.Rates {
				if assert.Len(t, rate.Options, 2, "expected location and netMeteringScheme options on %s", rate.ID) {
					assert.Equal(t, "location", rate.Options[0].Field)
					assert.Equal(t, "outside_city", rate.Options[0].Default)
					assert.Equal(t, "netMeteringScheme", rate.Options[1].Field)
					assert.Equal(t, "gp_instantaneous", rate.Options[1].Default)
				}
			}
		}
	})

	t.Run("R (Residential Service) across R-30 and R-31 summer and winter", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_r",
		})
		require.NoError(t, err)

		// Non-TOU gp_r must leave OtherName empty so GetPeriods returns empty
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)

		// 1. R-30 Winter (Wednesday, January 15, 2026 at 3:00 PM & May 31, 2026 at 11:00 PM before June 1 boundary)
		// Base: $0.080602 * 1.144508 * 1.011995
		// FCR-26 Winter: $0.042859 * 1.011995
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.080602*baseRidersJan26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042859*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
		assert.Empty(t, p.PeriodName)

		pMayEnd, err := u.priceForTime(time.Date(2026, time.May, 31, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.080602*baseRidersJan26*outsideCityMFFMultiplier, pMayEnd.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042859*outsideCityMFFMultiplier, pMayEnd.GridUseDollarsPerKWH, 1e-6)

		// 2. R-31 Summer Tier 1 starting June 1, 2026 (June 1, 2026 at 12:00 AM & July 15, 2026 at 3:00 PM)
		// Base: $0.087738 * 1.142174 * 1.011995
		// FCR-27 Summer: $0.038069 * 1.011995
		pJunStart, err := u.priceForTime(time.Date(2026, time.June, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087738*baseRidersJun26*outsideCityMFFMultiplier, pJunStart.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038069*outsideCityMFFMultiplier, pJunStart.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087738*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038069*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
		assert.Empty(t, p.PeriodName)

		// 3. R-31 Winter (Tuesday, Dec 15, 2026 at 3:00 PM & Friday, Jan 15, 2027 at 3:00 PM)
		// Base: $0.082116 * 1.142174 * 1.011995
		// FCR-27 Winter: $0.038561 * 1.011995
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.082116*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038561*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		p2027, err := u.priceForTime(time.Date(2027, time.January, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.082116*baseRidersJun26*outsideCityMFFMultiplier, p2027.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038561*outsideCityMFFMultiplier, p2027.GridUseDollarsPerKWH, 1e-6)

		// 4. R-30 Summer & Winter pre-2026 regime via gpPeriods("gp_r", ..., []int{2025})
		// Summer Tier 1: $0.086121 * 1.144508 * 1.011995, FCR-26 Summer: $0.045876 * 1.011995
		// Winter: $0.080602 * 1.144508 * 1.011995, FCR-26 Winter: $0.042859 * 1.011995
		r30Periods := gpPeriods("gp_r", types.UtilityRateOptions{Location: "outside_city"}, []int{2025})
		summer2025Start := time.Date(2025, time.July, 15, 15, 0, 0, 0, etLocation)
		pR30Summer, err := types.ApplyUtilityFeesPeriods(types.Price{
			TSStart: summer2025Start,
			TSEnd:   summer2025Start.Add(time.Hour),
		}, r30Periods)
		require.NoError(t, err)
		assert.InDelta(t, 0.086121*baseRidersJan26*outsideCityMFFMultiplier, pR30Summer.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.045876*outsideCityMFFMultiplier, pR30Summer.GridUseDollarsPerKWH, 1e-6)

		winter2025Start := time.Date(2025, time.December, 15, 15, 0, 0, 0, etLocation)
		pR30Winter, err := types.ApplyUtilityFeesPeriods(types.Price{
			TSStart: winter2025Start,
			TSEnd:   winter2025Start.Add(time.Hour),
		}, r30Periods)
		require.NoError(t, err)
		assert.InDelta(t, 0.080602*baseRidersJan26*outsideCityMFFMultiplier, pR30Winter.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042859*outsideCityMFFMultiplier, pR30Winter.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Location option selects outside_city or inside_city MFF-11 on gp_r and TOU", func(t *testing.T) {
		// Explicit outside_city on gp_r
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "outside_city",
			},
		})
		require.NoError(t, err)

		pOutside, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087738*baseRidersJun26*outsideCityMFFMultiplier, pOutside.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038069*outsideCityMFFMultiplier, pOutside.GridUseDollarsPerKWH, 1e-6)

		// inside_city on gp_r (3.0843% MFF-11 on both base+riders and FCR)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_r",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "inside_city",
			},
		})
		require.NoError(t, err)

		pInsideR, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.087738*baseRidersJun26*insideCityMFFMultiplier, pInsideR.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038069*insideCityMFFMultiplier, pInsideR.GridUseDollarsPerKWH, 1e-6)

		// inside_city on gp_tou_oa (3.0843% MFF-11 on both TOU-OA-15 base+riders and TOU-FCR-TP-5)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "inside_city",
			},
		})
		require.NoError(t, err)

		pInsideOA, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*baseRidersJun26*insideCityMFFMultiplier, pInsideOA.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*insideCityMFFMultiplier, pInsideOA.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("TOU-OA (Overnight Advantage) New ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
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
		assert.True(t, names["Super Off-Peak"])
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)))
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.September, 7, 12, 0, 0, 0, etLocation)))
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)))

		// --- Regime 2 (On/After June 1, 2026): ECCR-15 (13.0205%) + DSM-R-16 (1.1969%) = 14.2174%, TOU-FCR-TP-5 ($0.052269 / $0.038690 / $0.034747) ---

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 3. Summer weekday Super Off-Peak (Wednesday, July 15, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.022272*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.034747*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 4. Summer weekend Off-Peak (Saturday, July 18, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 5. Summer Holiday Off-Peak (Labor Day Monday, Sep 7, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 6. Winter daily Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 7. Winter daily Super Off-Peak (Tuesday, Dec 15, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.022272*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.034747*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// --- Regime 1 (Before June 1, 2026): ECCR-14 (13.2343%) + DSM-R-15 (1.2165%) = 14.4508%, FCR-26 ($0.044284 / $0.038252) ---

		// 8. Winter weekday Off-Peak (Wednesday, January 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.101676*baseRidersJan26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.044284*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 9. Winter weekday Super Off-Peak (Wednesday, January 15, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.021859*baseRidersJan26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038252*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("TOU-RD (Residential Demand / Smart Usage) New ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_rd",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)))
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)))

		// --- Regime 2 (TOU-RD-12 + ECCR-15 + DSM-R-16 + TOU-FCR-7) ---

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.145620*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.015569*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 3. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.015569*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// --- Regime 1 (TOU-RD-11 + ECCR-14 + DSM-R-15 + TOU-FCR-6) ---

		// 4. Winter weekday Off-Peak (Wednesday, January 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.015288*baseRidersJan26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042398*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("TOU-REO (Nights & Weekends) New ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_reo",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)))
		assert.True(t, namedPeriodsCoverDay(periods, time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation)))

		// --- Regime 2 (TOU-REO-19 + ECCR-15 + DSM-R-16 + TOU-FCR-7) ---

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.077702*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 3. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.077702*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// --- Regime 1 (TOU-REO-18 + ECCR-14 + DSM-R-15 + TOU-FCR-6) ---

		// 4. Winter weekday Off-Peak (Wednesday, January 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.076281*baseRidersJan26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042398*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Backwards Compatibility: TOU-OA-14 Old ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa_14",
		})
		require.NoError(t, err)

		// Summer weekday On-Peak should work identical to gp_tou_oa
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Backwards Compatibility: TOU-RD-11 Old ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_rd_11",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.145620*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Backwards Compatibility: TOU-REO-18 Old ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_reo_18",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*baseRidersJun26*outsideCityMFFMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*outsideCityMFFMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})
}

func TestGPExportRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("RNR-Instantaneous Netting 2026", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "gp_instantaneous",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.07219, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("RNR-Instantaneous Netting 2027 and fallback", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "gp_instantaneous",
			},
		})
		require.NoError(t, err)

		target := time.Date(2027, time.July, 15, 15, 0, 0, 0, etLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.07930, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Future years beyond 2027 fall back to 2027 RNR rate ($0.07930/kWh)
		periods2028 := gpPeriods("gp_r", types.UtilityRateOptions{}, []int{2028})
		ts2028 := time.Date(2028, time.July, 15, 15, 0, 0, 0, etLocation)
		p2028, err := types.ApplyUtilityFeesPeriods(types.Price{
			TSStart: ts2028,
			TSEnd:   ts2028.Add(time.Hour),
		}, periods2028)
		require.NoError(t, err)
		if assert.True(t, p2028.SeparateGenerationCredit) {
			assert.InDelta(t, 0.07930, p2028.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("RNR-Monthly Netting", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)
	})
}
