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

	t.Run("Bundled EV-TOU-5 Rates across 2026 and 2027 windows", func(t *testing.T) {
		settings := types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou_5",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		}
		err := u.ApplySettings(context.Background(), settings)
		require.NoError(t, err)

		// 1/1/2026 Winter Weekday On-Peak (Jan 6, 2026 17:00, Tuesday):
		// UDC 0.32322 + EECC 0.20013 + WF-NBC/DWR-BC 0.00591 = 0.52926 (GridUse = 0.02099, Base = 0.50827)
		pJanOn, err := u.priceForTime(time.Date(2026, 1, 6, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.50827, pJanOn.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, pJanOn.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.52926, pJanOn.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.50827, pJanOn.ExportRateDollars(settings, nil), 1e-6)

		// 1/1/2026 Winter Weekday Super Off-Peak (Jan 6, 2026 11:00):
		// UDC 0.03676 + EECC 0.07419 + 0.00591 = 0.11686
		pJanSOP, err := u.priceForTime(time.Date(2026, 1, 6, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.11686, pJanSOP.ImportRateDollars(), 1e-6)

		// 4/1/2026 Winter Weekday On-Peak (April 7, 2026 17:00, Tuesday):
		// UDC 0.32682 + EECC 0.19990 + 0.00591 = 0.53263 (Base = 0.51164)
		pAprOn, err := u.priceForTime(time.Date(2026, 4, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.51164, pAprOn.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.53263, pAprOn.ImportRateDollars(), 1e-6)

		// 6/1/2026 Summer Weekday On-Peak (July 7, 2026 17:00, Tuesday):
		// UDC 0.31711 + EECC 0.47019 + 0.00591 = 0.79321 (Base = 0.77222, GridUse = 0.02099)
		pJulOn, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.77222, pJulOn.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, pJulOn.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.79321, pJulOn.ImportRateDollars(), 1e-6)

		// 6/1/2026 Summer Weekday Super Off-Peak (July 7, 2026 11:00 AM):
		// UDC 0.04114 + EECC 0.08147 + 0.00591 = 0.12852 (Base = 0.10753)
		pJulSOP, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.10753, pJulSOP.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.12852, pJulSOP.ImportRateDollars(), 1e-6)

		// 11/1/2026 Winter Weekday On-Peak under 8/1/26 table (Nov 3, 2026 17:00):
		// UDC 0.31218 + EECC 0.20574 + 0.00591 = 0.52383 (Base = 0.50284)
		pNovOn, err := u.priceForTime(time.Date(2026, 11, 3, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.50284, pNovOn.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.52383, pNovOn.ImportRateDollars(), 1e-6)

		// 2027 Winter & Summer carry-forward (Feb 9, 2027 & July 13, 2027 17:00)
		pFeb2027, err := u.priceForTime(time.Date(2027, 2, 9, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.52383, pFeb2027.ImportRateDollars(), 1e-6)

		pJul2027, err := u.priceForTime(time.Date(2027, 7, 13, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.80205, pJul2027.ImportRateDollars(), 1e-6)
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

		// 1/1/2026 Winter Weekday On-Peak (Jan 6, 2026 17:00): UDC 0.32322 + 2021v PCIA 0.03557 + SDCP 0.15409 + 0.00591 = 0.51879
		pJan, err := u.priceForTime(time.Date(2026, 1, 6, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.51879, pJan.ImportRateDollars(), 1e-6)

		// 6/1/2026 Summer Weekday On-Peak (July 7, 2026 17:00):
		// UDC 0.31711 + 2021v PCIA 0.03564 + SDCP 0.41063 - 0.01508 = 0.74830 (GridUse = 0.02099, Import = 0.76929)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.74830, p1.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, p1.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.76929, p1.ImportRateDollars(), 1e-6)

		// 6/1/2026 Summer Weekday Super Off-Peak (July 7, 2026 11:00 AM):
		// UDC 0.04114 + 2021v PCIA 0.03564 + SDCP 0.04168 - 0.01508 = 0.10338 (Import = 0.12437)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.10338, p2.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.12437, p2.ImportRateDollars(), 1e-6)

		// 8/1/2026 Summer Weekday On-Peak (Aug 4, 2026 17:00):
		// UDC 0.31218 + 2021v PCIA 0.03348 + SDCP 0.41063 + 0.00591 = 0.76220
		pAug, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.76220, pAug.ImportRateDollars(), 1e-6)
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

		// 1/1/2026 Winter Weekday On-Peak (Jan 6, 2026 17:00): UDC 0.32322 + 2022v PCIA 0.02999 + SDCP 0.14769 + 0.00591 = 0.50681
		pJan, err := u.priceForTime(time.Date(2026, 1, 6, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.50681, pJan.ImportRateDollars(), 1e-6)

		// 6/1/2026 Summer Weekday On-Peak (July 7, 2026 17:00):
		// UDC 0.31711 + 2022v PCIA 0.03005 + SDCP 0.38801 - 0.01508 = 0.72009 (GridUse = 0.02099, Import = 0.74108)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.72009, p1.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, p1.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.74108, p1.ImportRateDollars(), 1e-6)

		// 8/1/2026 Summer Weekday On-Peak (Aug 4, 2026 17:00):
		// UDC 0.31218 + 2022v PCIA 0.02789 + SDCP 0.38801 + 0.00591 = 0.73399
		pAug, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.73399, pAug.ImportRateDollars(), 1e-6)
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

		// 6/1/2026 Summer Weekday On-Peak (July 7, 2026 17:00):
		// UDC 0.31711 + 2021v PCIA 0.03564 + (SDCP PowerOn 0.41063 + 0.01) - 0.01508 = 0.75830 (Import = 0.77929)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.75830, p1.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, p1.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.77929, p1.ImportRateDollars(), 1e-6)
	})

	t.Run("TOU-DR2 2-Period TOU Rates and Tier 1 TRAC Credits", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_dr2",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		})
		require.NoError(t, err)

		// 6/1/2026 Summer Weekday On-Peak (July 7, 2026 17:00):
		// UDC (0.33396 - 0.10663) + EECC 0.34920 + 0.00591 = 0.58244 (Base = 0.56145, GridUse = 0.02099)
		p1, err := u.priceForTime(time.Date(2026, 7, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.56145, p1.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, p1.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.58244, p1.ImportRateDollars(), 1e-6)

		// 6/1/2026 Summer Weekday Off-Peak (July 7, 2026 11:00 AM):
		// UDC (0.32750 - 0.10663) + EECC 0.08432 + 0.00591 = 0.31110 (Base = 0.29011)
		p2, err := u.priceForTime(time.Date(2026, 7, 7, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.29011, p2.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.31110, p2.ImportRateDollars(), 1e-6)

		// 8/1/2026 Summer Weekday On-Peak (August 2, 2026 17:00):
		// UDC (0.33063 - 0.10702) + EECC 0.35943 + 0.00591 = 0.58895 (Base = 0.56796)
		p3, err := u.priceForTime(time.Date(2026, 8, 2, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.56796, p3.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.58895, p3.ImportRateDollars(), 1e-6)

		// 8/1/2026 Summer Weekday Off-Peak (August 2, 2026 11:00 AM):
		// UDC (0.32397 - 0.10702) + EECC 0.08678 + 0.00591 = 0.30964 (Base = 0.28865)
		p4, err := u.priceForTime(time.Date(2026, 8, 2, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.28865, p4.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.30964, p4.ImportRateDollars(), 1e-6)
	})

	t.Run("Tier 1 TRAC Credits on TOU-DR and TOU-DR1 across all four 2026 windows", func(t *testing.T) {
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_dr1",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		}))

		// 1/1/26 Winter On-Peak: (0.34101 - 0.10905) + 0.27508 + 0.00591 = 0.51295
		pJan, err := u.priceForTime(time.Date(2026, 2, 3, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.51295, pJan.ImportRateDollars(), 1e-6)

		// 4/1/26 Winter On-Peak: (0.34061 - 0.10892) + 0.27475 + 0.00591 = 0.51235
		pApr, err := u.priceForTime(time.Date(2026, 4, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.51235, pApr.ImportRateDollars(), 1e-6)

		// 6/1/26 Summer On-Peak: (0.32948 - 0.10663) + 0.34920 + 0.00591 = 0.57796
		pJun, err := u.priceForTime(time.Date(2026, 6, 9, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.57796, pJun.ImportRateDollars(), 1e-6)

		// 8/1/26 Summer On-Peak: (0.32601 - 0.10702) + 0.35943 + 0.00591 = 0.58433; Super Off-Peak: 0.21899 + 0.04241 + 0.00591 = 0.26731
		pAugOn, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.58433, pAugOn.ImportRateDollars(), 1e-6)

		pAugSOP, err := u.priceForTime(time.Date(2026, 8, 4, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.26731, pAugSOP.ImportRateDollars(), 1e-6)

		// TOU-DR 8/1/26 Summer On-Peak: (0.32601 - 0.10702) + 0.23565 + 0.00591 = 0.46055
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_dr",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		}))
		pTOUDR, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.46055, pTOUDR.ImportRateDollars(), 1e-6)
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

		// Post-Aug 1, 2026 On-Peak (August 2, 2026 17:00): Import = 0.31218 + 0.48396 + 0.00591 = 0.80205 (Base = 0.78106)
		p1, err := u.priceForTime(time.Date(2026, 8, 2, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.78106, p1.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.80205, p1.ImportRateDollars(), 1e-6)

		// Post-Aug 1, 2026 Super Off-Peak (August 2, 2026 11:00 AM): Import = 0.04114 + 0.08385 + 0.00591 = 0.13090 (Base = 0.10991)
		p2, err := u.priceForTime(time.Date(2026, 8, 2, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.10991, p2.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.13090, p2.ImportRateDollars(), 1e-6)

		// Saturday August 1, 2026 01:00 AM (Weekend Midnight - 2:00 PM) -> Super Off-Peak
		p3, err := u.priceForTime(time.Date(2026, 8, 1, 1, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p3.PeriodName)
		assert.InDelta(t, 0.10991, p3.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.13090, p3.ImportRateDollars(), 1e-6)
	})

	t.Run("Schedule TOU-ELEC Rates across 2026 windows including Post-August EECC", func(t *testing.T) {
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_elec",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		}))

		// 1/1/26 Winter On-Peak: 0.25858 + 0.24340 + 0.00591 = 0.50789
		pJan, err := u.priceForTime(time.Date(2026, 2, 3, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.50789, pJan.ImportRateDollars(), 1e-6)

		// 6/1/26 Summer On-Peak: 0.25317 + 0.45690 + 0.00591 = 0.71598
		pJun, err := u.priceForTime(time.Date(2026, 6, 9, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.71598, pJun.ImportRateDollars(), 1e-6)

		// 8/1/26 Summer On-Peak ($0.47028 EECC), Off-Peak ($0.13324 EECC), Super Off-Peak ($0.08889 EECC)
		pSumOn, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.24970+0.47028+0.00591, pSumOn.ImportRateDollars(), 1e-6)

		pSumOff, err := u.priceForTime(time.Date(2026, 8, 4, 15, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.24970+0.13324+0.00591, pSumOff.ImportRateDollars(), 1e-6)

		pSumSOP, err := u.priceForTime(time.Date(2026, 8, 4, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.24970+0.08889+0.00591, pSumSOP.ImportRateDollars(), 1e-6)

		// 11/1/26 Winter On-Peak ($0.25023 EECC), Off-Peak ($0.12118 EECC), Super Off-Peak ($0.08085 EECC)
		pWinOn, err := u.priceForTime(time.Date(2026, 11, 3, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.24970+0.25023+0.00591, pWinOn.ImportRateDollars(), 1e-6)

		pWinOff, err := u.priceForTime(time.Date(2026, 11, 3, 15, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.24970+0.12118+0.00591, pWinOff.ImportRateDollars(), 1e-6)

		pWinSOP, err := u.priceForTime(time.Date(2026, 11, 3, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.24970+0.08085+0.00591, pWinSOP.ImportRateDollars(), 1e-6)
	})

	t.Run("Schedule DR-SES Bundled and SDCP EV-TOU Mapping", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_dr_ses",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		})
		require.NoError(t, err)

		// 1/1/26 Winter On-Peak: 0.26840 + 0.20013 + 0.00591 = 0.47444
		pJan, err := u.priceForTime(time.Date(2026, 2, 3, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.47444, pJan.ImportRateDollars(), 1e-6)

		// 4/1/26 Winter On-Peak: 0.27299 + 0.19990 + 0.00591 = 0.47880
		pApr, err := u.priceForTime(time.Date(2026, 4, 7, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.47880, pApr.ImportRateDollars(), 1e-6)

		// 6/1/26 Summer On-Peak: 0.26328 + 0.47019 + 0.00591 = 0.73938
		pJun, err := u.priceForTime(time.Date(2026, 6, 9, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.73938, pJun.ImportRateDollars(), 1e-6)

		// 8/1/26 On-Peak (August 2, 2026 17:00): 0.25957 + 0.48396 + 0.00591 = 0.74944 (Base = 0.72845)
		p1, err := u.priceForTime(time.Date(2026, 8, 2, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.72845, p1.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.74944, p1.ImportRateDollars(), 1e-6)

		// 8/1/26 Super Off-Peak (August 2, 2026 11:00 AM): 0.25957 + 0.08385 + 0.00591 = 0.34933 (Base = 0.32834)
		p2, err := u.priceForTime(time.Date(2026, 8, 2, 11, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.32834, p2.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.34933, p2.ImportRateDollars(), 1e-6)

		// SDCP PowerOn DR-SES maps to EV-TOU generation ($0.41063 Summer On-Peak on 2021v + $0.03348 PCIA)
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_dr_ses",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdcp_power_on",
				Location:       "san_diego",
			},
		}))
		pSDCP, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.25957+0.41063+0.03348+0.00591, pSDCP.ImportRateDollars(), 1e-6)
	})

	t.Run("Schedule DR Flat Tier 1 Bundled and SDCP Rates", func(t *testing.T) {
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_dr",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdge",
			},
		}))

		// 1/1/26 ($0.42084), 4/1/26 ($0.42034), 6/1/26 ($0.41150), 8/1/26 ($0.41299)
		pJan, err := u.priceForTime(time.Date(2026, 1, 15, 18, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.Equal(t, "Tier 1", pJan.PeriodName)
		assert.InDelta(t, 0.42084, pJan.ImportRateDollars(), 1e-6)

		pApr, err := u.priceForTime(time.Date(2026, 4, 15, 18, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.42034, pApr.ImportRateDollars(), 1e-6)

		pJun, err := u.priceForTime(time.Date(2026, 6, 15, 18, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.41150, pJun.ImportRateDollars(), 1e-6)

		pAug, err := u.priceForTime(time.Date(2026, 8, 15, 18, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.39200, pAug.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02099, pAug.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.41299, pAug.ImportRateDollars(), 1e-6)

		// SDCP 2021v (san_diego) and 2022v (unincorporated) parity on Schedule DR
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_dr",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdcp_power_on",
				Location:       "san_diego",
			},
		}))
		pSDCP2021, err := u.priceForTime(time.Date(2026, 8, 15, 18, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.21900+0.13780+0.03348+0.00591, pSDCP2021.ImportRateDollars(), 1e-6)

		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_dr",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "sdcp_power_on",
				Location:       "unincorporated",
			},
		}))
		pSDCP2022, err := u.priceForTime(time.Date(2026, 8, 15, 18, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.21900+0.14339+0.02789+0.00591, pSDCP2022.ImportRateDollars(), 1e-6)
	})

	t.Run("Schedule NEM-ST Charges on EV-TOU and NEM 1.0 1:1 Retail Credits", func(t *testing.T) {
		nem2EVTOUSettings := types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_ev_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate:    "sdge",
				NetMeteringScheme: "nem2",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), nem2EVTOUSettings))

		// EV-TOU 8/1/26 Summer On-Peak: UDC 0.37290 + EECC 0.48396 + 0.00591 = 0.86277
		// GridUse = $0.02297/kWh, Base DollarsPerKWH = 0.86277 - 0.02297 = 0.83980
		pEVTOU, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.InDelta(t, 0.83980, pEVTOU.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02297, pEVTOU.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.86277, pEVTOU.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.83980, pEVTOU.ExportRateDollars(nem2EVTOUSettings, nil), 1e-6)

		// NEM 1.0 ("net") preserves full 1:1 retail export credits including NBCs
		nem1EVTOUSettings := types.Settings{
			UtilityProvider:              "sdge",
			UtilityRate:                  "sdge_ev_tou",
			SolarNetMeteringCreditsValue: "highest",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate:    "sdge",
				NetMeteringScheme: "net",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), nem1EVTOUSettings))
		pNEM1, err := u.priceForTime(time.Date(2026, 8, 4, 17, 0, 0, 0, loc))
		require.NoError(t, err)
		assert.False(t, pNEM1.SeparateGenerationCredit)
		assert.InDelta(t, 0.86277, pNEM1.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.86277, pNEM1.ExportRateDollars(nem1EVTOUSettings, []types.Price{pNEM1}), 1e-6)
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

	t.Run("Application year cohorts and Nov_Weekend_1 fall-back deduplication", func(t *testing.T) {
		nov2026Hour1 := time.Date(2026, 11, 1, 1, 0, 0, 0, loc)
		nov2027Hour1 := time.Date(2027, 11, 7, 1, 0, 0, 0, loc)

		// NBT25 / NBT26 deduplicated Nov_Weekend_1: $0.076610 in 2026 and $0.077590 in 2027
		assert.InDelta(t, 0.076610, getSDGENBTExportRate(nov2026Hour1, "2026"), 1e-6)
		assert.InDelta(t, 0.076610, getSDGENBTExportRate(nov2026Hour1, "2025"), 1e-6)
		assert.InDelta(t, 0.077590, getSDGENBTExportRate(nov2027Hour1, "2026"), 1e-6)
		assert.InDelta(t, 0.077590, getSDGENBTExportRate(nov2027Hour1, "2025"), 1e-6)

		// NBT23 / NBT24 cohorts
		assert.InDelta(t, 0.053665, getSDGENBTExportRate(nov2026Hour1, "2023"), 1e-6)
		assert.InDelta(t, 0.053665, getSDGENBTExportRate(nov2026Hour1, "2024"), 1e-6)
		assert.InDelta(t, 0.056036, getSDGENBTExportRate(nov2027Hour1, "2023"), 1e-6)
		assert.InDelta(t, 0.056036, getSDGENBTExportRate(nov2027Hour1, "2024"), 1e-6)

		// NBT27 and non_locked (NBT00) fall back to NBT26 in 2026 and use NBT27/NBT00 in 2027 ($0.00/kWh ACC Plus)
		assert.InDelta(t, 0.076610, getSDGENBTExportRate(nov2026Hour1, "2027"), 1e-6)
		assert.InDelta(t, 0.076610, getSDGENBTExportRate(nov2026Hour1, "non_locked"), 1e-6)
		assert.InDelta(t, 0.195046, getSDGENBTExportRate(nov2027Hour1, "2027"), 1e-6)
		assert.InDelta(t, 0.195046, getSDGENBTExportRate(nov2027Hour1, "non_locked"), 1e-6)
	})
}

func TestSDGENBTPeriods(t *testing.T) {
	t.Run("NEM 3.0 Export Credit Periods across cohorts", func(t *testing.T) {
		opts := types.UtilityRateOptions{
			NetMeteringScheme: "sbp",
			GenerationRate:    "sdge",
		}
		periods := sdgePeriods("sdge_ev_tou_5", opts, []int{2026, 2027})

		foundAprWeekday := false
		foundJanWeekday := false
		foundNovWeekend2026 := false
		foundNovWeekend2027 := false
		for _, p := range periods {
			if p.SeparateGenerationCredit && p.Description == "SDG&E NBT Weekday Export Credit (Hour 0)" {
				if p.Start.Year() == 2026 && p.Start.Month() == time.April {
					foundAprWeekday = true
					assert.InDelta(t, 0.073861, p.DollarsPerKWH, 1e-6)
				}
				if p.Start.Year() == 2026 && p.Start.Month() == time.January {
					foundJanWeekday = true
					// Non-holiday January weekdays must use Jan_Weekday_0 ($0.088830), not Jan 1 holiday ($0.087919)
					assert.InDelta(t, 0.088830, p.DollarsPerKWH, 1e-6)
				}
			}
			if p.SeparateGenerationCredit && p.Description == "SDG&E NBT Weekend Export Credit (Hour 1)" && p.Start.Month() == time.November {
				if p.Start.Year() == 2026 {
					foundNovWeekend2026 = true
					assert.InDelta(t, 0.076610, p.DollarsPerKWH, 1e-6)
				}
				if p.Start.Year() == 2027 {
					foundNovWeekend2027 = true
					assert.InDelta(t, 0.077590, p.DollarsPerKWH, 1e-6)
				}
			}
		}
		assert.True(t, foundAprWeekday, "expected to find April 2026 SDG&E NBT Weekday Export Credit period")
		assert.True(t, foundJanWeekday, "expected to find January 2026 SDG&E NBT Weekday Export Credit period")
		assert.True(t, foundNovWeekend2026, "expected to find November 2026 SDG&E NBT Weekend Export Credit (Hour 1)")
		assert.True(t, foundNovWeekend2027, "expected to find November 2027 SDG&E NBT Weekend Export Credit (Hour 1)")

		// Verify 2023 and 2027 cohorts through genericTOU.priceForTime
		u := &genericTOU{}
		s2023 := types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_elec",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate:    "sdge",
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2023",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), s2023))
		p2023, err := u.priceForTime(time.Date(2026, 11, 1, 1, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.True(t, p2023.SeparateGenerationCredit) {
			assert.InDelta(t, 0.053665, p2023.ExportRateDollars(s2023, nil), 1e-6)
		}

		s2027 := types.Settings{
			UtilityProvider: "sdge",
			UtilityRate:     "sdge_tou_elec",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate:    "sdge",
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2027",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), s2027))
		p2027In2026, err := u.priceForTime(time.Date(2026, 11, 1, 1, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.076610, p2027In2026.ExportRateDollars(s2027, nil), 1e-6)

		p2027In2027, err := u.priceForTime(time.Date(2027, 11, 7, 1, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.195046, p2027In2027.ExportRateDollars(s2027, nil), 1e-6)

		// Verify continuous, non-overlapping SBP export credit coverage across weekdays, weekends, holidays, and DST transitions
		checkDates := []time.Time{
			time.Date(2026, 1, 1, 0, 0, 0, 0, ptLocation),  // New Year's Day (Thursday holiday)
			time.Date(2026, 1, 2, 0, 0, 0, 0, ptLocation),  // Regular Friday
			time.Date(2026, 3, 8, 0, 0, 0, 0, ptLocation),  // Spring-forward Sunday
			time.Date(2026, 7, 4, 0, 0, 0, 0, ptLocation),  // Saturday Independence Day
			time.Date(2026, 11, 1, 0, 0, 0, 0, ptLocation), // Fall-back Sunday
			time.Date(2027, 7, 5, 0, 0, 0, 0, ptLocation),  // Observed Independence Day (Monday holiday)
			time.Date(2027, 11, 7, 0, 0, 0, 0, ptLocation), // 2027 Fall-back Sunday
		}
		for _, day := range checkDates {
			nextDay := day.AddDate(0, 0, 1)
			for ts := day; ts.Before(nextDay); ts = ts.Add(time.Hour) {
				solarMatches := 0
				for i := range periods {
					if periods[i].SeparateGenerationCredit {
						ok, _, err := periods[i].Contains(ts)
						require.NoError(t, err)
						if ok {
							solarMatches++
						}
					}
				}
				assert.Equal(t, 1, solarMatches, "expected exactly 1 SBP export period at %v", ts)
			}
		}
	})
}

func TestSdgeUtilityInfo(t *testing.T) {
	t.Run("Schedules and conditional location and applicationYear options", func(t *testing.T) {
		info := sdgeUtilityInfo()
		require.Len(t, info.Rates, 9)

		rateIDs := make([]string, 0, len(info.Rates))
		for _, rate := range info.Rates {
			rateIDs = append(rateIDs, rate.ID)

			var locationOpt *types.UtilityRateOption
			var appYearOpt *types.UtilityRateOption
			for i := range rate.Options {
				switch rate.Options[i].Field {
				case "location":
					locationOpt = &rate.Options[i]
				case "applicationYear":
					appYearOpt = &rate.Options[i]
				}
			}
			require.NotNil(t, locationOpt, "rate %q should include location option", rate.ID)
			assert.Equal(t, map[string][]any{
				"generationRate": {"sdcp_power_base", "sdcp_power_on", "sdcp_power_100"},
			}, locationOpt.DependsOn)

			require.NotNil(t, appYearOpt, "rate %q should include applicationYear option", rate.ID)
			assert.Equal(t, types.UtilityOptionTypeSelect, appYearOpt.Type)
			assert.Equal(t, "2026", appYearOpt.Default)
			assert.Equal(t, map[string][]any{"netMeteringScheme": {"sbp"}}, appYearOpt.DependsOn)
			if assert.Len(t, appYearOpt.Choices, 6) {
				assert.Equal(t, "2023", appYearOpt.Choices[0].Value)
				assert.Equal(t, "2024", appYearOpt.Choices[1].Value)
				assert.Equal(t, "2025", appYearOpt.Choices[2].Value)
				assert.Equal(t, "2026", appYearOpt.Choices[3].Value)
				assert.Equal(t, "2027", appYearOpt.Choices[4].Value)
				assert.Equal(t, "non_locked", appYearOpt.Choices[5].Value)
			}
		}
		assert.Contains(t, rateIDs, "sdge_dr")
	})
}
