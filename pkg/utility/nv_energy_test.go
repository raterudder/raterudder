package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNVEnergyPeriods(t *testing.T) {
	ctx := context.Background()

	t.Run("South RS and RM Flat Schedules Default NMR-405 and Net Metering", func(t *testing.T) {
		m := NewMap(nil)

		// South RS default ("" -> nmr_405) across 2026 and 2027
		uRS, err := m.Site(ctx, "nv-south-rs", types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_rs",
		})
		require.NoError(t, err)
		touRS, ok := uRS.(*genericTOU)
		require.True(t, ok)

		p2026, err := touRS.priceForTime(time.Date(2026, time.July, 15, 19, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11441, p2026.DollarsPerKWH, 1e-6) {
			assert.True(t, p2026.SeparateGenerationCredit)
			assert.InDelta(t, 0.08347, p2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p2027, err := touRS.priceForTime(time.Date(2027, time.January, 20, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11441, p2027.DollarsPerKWH, 1e-6) {
			assert.True(t, p2027.SeparateGenerationCredit)
			assert.InDelta(t, 0.08347, p2027.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// South RS with legacy 1:1 net metering ("net")
		err = touRS.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNet, err := touRS.priceForTime(time.Date(2026, time.October, 10, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11441, pNet.DollarsPerKWH, 1e-6) {
			assert.False(t, pNet.SeparateGenerationCredit)
			assert.Zero(t, pNet.GenerationCreditDollarsPerKWH)
		}

		// South RM explicit nmr_405 and net across 2026 and 2027
		uRM := &genericTOU{}
		err = uRM.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_rm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nmr_405",
			},
		})
		require.NoError(t, err)

		pRM2026, err := uRM.priceForTime(time.Date(2026, time.November, 5, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10507, pRM2026.DollarsPerKWH, 1e-6) {
			assert.True(t, pRM2026.SeparateGenerationCredit)
			assert.InDelta(t, 0.07680, pRM2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pRM2027, err := uRM.priceForTime(time.Date(2027, time.August, 8, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10507, pRM2027.DollarsPerKWH, 1e-6) {
			assert.True(t, pRM2027.SeparateGenerationCredit)
			assert.InDelta(t, 0.07680, pRM2027.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("South ORS-TOU Daily Peak and NMR-405 vs Net Credits", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_ors_tou",
		})
		require.NoError(t, err)

		// Summer Weekday Peak: Monday July 13, 2026 at 18:00 PT (6:00 PM - 9:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.46974, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.34997, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Weekend Peak: Saturday July 18, 2026 at 20:00 PT (daily peak applies on weekends)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 20, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.46974, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.InDelta(t, 0.34997, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Holiday Peak: Labor Day Monday Sept 7, 2026 at 19:00 PT (daily without holiday shift)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 19, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.46974, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Summer Off-Peak boundaries: 17:00 PT and 21:00 PT
		pBefore, err := u.priceForTime(time.Date(2026, time.July, 13, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.07124, pBefore.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pBefore.PeriodName)
			assert.True(t, pBefore.SeparateGenerationCredit)
			assert.InDelta(t, 0.05109, pBefore.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pAfter, err := u.priceForTime(time.Date(2026, time.July, 13, 21, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.07124, pAfter.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pAfter.PeriodName)
			assert.InDelta(t, 0.05109, pAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Winter (Oct - May): Nov 2026 and Feb 2027 at 19:00 PT
		pWinter2026, err := u.priceForTime(time.Date(2026, time.November, 18, 19, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08477, pWinter2026.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pWinter2026.PeriodName)
			assert.True(t, pWinter2026.SeparateGenerationCredit)
			assert.InDelta(t, 0.06124, pWinter2026.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pSummer2027, err := u.priceForTime(time.Date(2027, time.June, 20, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.46974, pSummer2027.DollarsPerKWH, 1e-6)

		// Net Metering ("net") disables SeparateGenerationCredit without panicking
		err = u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_ors_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNetPeak, err := u.priceForTime(time.Date(2026, time.July, 13, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.46974, pNetPeak.DollarsPerKWH, 1e-6) {
			assert.False(t, pNetPeak.SeparateGenerationCredit)
			assert.Zero(t, pNetPeak.GenerationCreditDollarsPerKWH)
		}
	})

	t.Run("South ORS-TOU and ORM-TOU with EV Recharge Credit", func(t *testing.T) {
		uORS := &genericTOU{}
		err := uORS.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_ors_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				EVCredit:          true,
				NetMeteringScheme: "nmr_405",
			},
		})
		require.NoError(t, err)

		periods, err := uORS.GetPeriods(ctx)
		require.NoError(t, err)
		periodNames := make(map[string]bool)
		for _, p := range periods {
			periodNames[p.Name] = true
		}
		assert.True(t, periodNames["On-Peak"])
		assert.True(t, periodNames["Off-Peak"])
		assert.True(t, periodNames["EV Recharge"])

		// Summer EV Recharge 00:00-08:00 daily: $0.06408/kWh, credit $0.04572/kWh
		pSummerEV, err := uORS.priceForTime(time.Date(2026, time.August, 2, 3, 0, 0, 0, ptLocation)) // Sunday
		require.NoError(t, err)
		if assert.InDelta(t, 0.06408, pSummerEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pSummerEV.PeriodName)
			assert.True(t, pSummerEV.SeparateGenerationCredit)
			assert.InDelta(t, 0.04572, pSummerEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// At 08:00 PT transitions to Summer Off-Peak ($0.07124)
		pSummer8AM, err := uORS.priceForTime(time.Date(2026, time.August, 2, 8, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.07124, pSummer8AM.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pSummer8AM.PeriodName)
		}

		// Winter EV Recharge 00:00-08:00 daily: $0.07626/kWh, credit $0.05486/kWh
		pWinterEV, err := uORS.priceForTime(time.Date(2027, time.February, 10, 7, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.07626, pWinterEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pWinterEV.PeriodName)
			assert.True(t, pWinterEV.SeparateGenerationCredit)
			assert.InDelta(t, 0.05486, pWinterEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// South ORM-TOU with EV Recharge across Summer & Winter
		uORM := &genericTOU{}
		err = uORM.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_orm_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				EVCredit:          true,
				NetMeteringScheme: "nmr_405",
			},
		})
		require.NoError(t, err)

		pORMPeak, err := uORM.priceForTime(time.Date(2026, time.June, 14, 19, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.36323, pORMPeak.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", pORMPeak.PeriodName)
			assert.InDelta(t, 0.27042, pORMPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pORMOffPeak, err := uORM.priceForTime(time.Date(2026, time.June, 14, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09569, pORMOffPeak.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pORMOffPeak.PeriodName)
			assert.InDelta(t, 0.06976, pORMOffPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pORMSummerEV, err := uORM.priceForTime(time.Date(2026, time.June, 14, 1, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08604, pORMSummerEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pORMSummerEV.PeriodName)
			assert.InDelta(t, 0.06252, pORMSummerEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pORMWinter, err := uORM.priceForTime(time.Date(2026, time.December, 10, 15, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.06850, pORMWinter.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pORMWinter.PeriodName)
			assert.InDelta(t, 0.04937, pORMWinter.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pORMWinterEV, err := uORM.priceForTime(time.Date(2026, time.December, 10, 4, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.06157, pORMWinterEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pORMWinterEV.PeriodName)
			assert.InDelta(t, 0.04417, pORMWinterEV.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("North D-1 and DM-1 Flat Schedules NMR-2025 NMR-405 and Net", func(t *testing.T) {
		uD1 := &genericTOU{}
		err := uD1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_d1",
		})
		require.NoError(t, err)

		// Default is nmr_2025
		pD1, err := uD1.priceForTime(time.Date(2026, time.July, 4, 15, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11386, pD1.DollarsPerKWH, 1e-6) {
			assert.True(t, pD1.SeparateGenerationCredit)
			assert.InDelta(t, 0.08055, pD1.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// nmr_405 also supported on North schedules
		err = uD1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_d1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nmr_405",
			},
		})
		require.NoError(t, err)
		pD1405, err := uD1.priceForTime(time.Date(2027, time.March, 15, 11, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11386, pD1405.DollarsPerKWH, 1e-6) {
			assert.True(t, pD1405.SeparateGenerationCredit)
			assert.InDelta(t, 0.08055, pD1405.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// North DM-1 default and net
		uDM1 := &genericTOU{}
		err = uDM1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_dm1",
		})
		require.NoError(t, err)

		pDM1, err := uDM1.priceForTime(time.Date(2026, time.November, 22, 9, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11004, pDM1.DollarsPerKWH, 1e-6) {
			assert.True(t, pDM1.SeparateGenerationCredit)
			assert.InDelta(t, 0.07759, pDM1.GenerationCreditDollarsPerKWH, 1e-6)
		}

		err = uDM1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_dm1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pDM1Net, err := uDM1.priceForTime(time.Date(2027, time.September, 5, 19, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11004, pDM1Net.DollarsPerKWH, 1e-6) {
			assert.False(t, pDM1Net.SeparateGenerationCredit)
			assert.Zero(t, pDM1Net.GenerationCreditDollarsPerKWH)
		}
	})

	t.Run("North OD-1-TOU and ODM-1-TOU Schedules and EV Options", func(t *testing.T) {
		uOD1 := &genericTOU{}
		err := uOD1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_od1_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				EVCredit:          true,
				NetMeteringScheme: "nmr_2025",
			},
		})
		require.NoError(t, err)

		// Summer Weekday & Weekend Peak (18:00-21:00 daily): $0.49660, credit $0.36761
		pWeekdayPeak, err := uOD1.priceForTime(time.Date(2026, time.July, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.49660, pWeekdayPeak.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", pWeekdayPeak.PeriodName)
			assert.True(t, pWeekdayPeak.SeparateGenerationCredit)
			assert.InDelta(t, 0.36761, pWeekdayPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pWeekendPeak, err := uOD1.priceForTime(time.Date(2026, time.July, 19, 20, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.49660, pWeekendPeak.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", pWeekendPeak.PeriodName)
		}

		// Summer Off-Peak: $0.08856, credit $0.06158
		pSummerOff, err := uOD1.priceForTime(time.Date(2026, time.July, 15, 13, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08856, pSummerOff.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pSummerOff.PeriodName)
			assert.InDelta(t, 0.06158, pSummerOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer EV (00:00-08:00): $0.07918, credit $0.05454
		pSummerEV, err := uOD1.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.07918, pSummerEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pSummerEV.PeriodName)
			assert.InDelta(t, 0.05454, pSummerEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Winter Off-Peak: $0.08851, credit $0.06154 (also verify nmr_405 matches)
		err = uOD1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_od1_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				EVCredit:          true,
				NetMeteringScheme: "nmr_405",
			},
		})
		require.NoError(t, err)

		pWinterOff, err := uOD1.priceForTime(time.Date(2027, time.January, 14, 19, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08851, pWinterOff.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pWinterOff.PeriodName)
			assert.InDelta(t, 0.06154, pWinterOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Winter EV (00:00-08:00): $0.07914, credit $0.05451
		pWinterEV, err := uOD1.priceForTime(time.Date(2027, time.January, 14, 5, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.07914, pWinterEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pWinterEV.PeriodName)
			assert.InDelta(t, 0.05451, pWinterEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// North ODM-1-TOU across Summer & Winter with EV and Net options
		uODM1 := &genericTOU{}
		err = uODM1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_odm1_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				EVCredit:          true,
				NetMeteringScheme: "nmr_2025",
			},
		})
		require.NoError(t, err)

		pODM1Peak, err := uODM1.priceForTime(time.Date(2027, time.August, 14, 18, 0, 0, 0, ptLocation)) // Saturday
		require.NoError(t, err)
		if assert.InDelta(t, 0.33484, pODM1Peak.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", pODM1Peak.PeriodName)
			assert.InDelta(t, 0.24619, pODM1Peak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pODM1Off, err := uODM1.priceForTime(time.Date(2027, time.August, 14, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10579, pODM1Off.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pODM1Off.PeriodName)
			assert.InDelta(t, 0.07440, pODM1Off.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pODM1SummerEV, err := uODM1.priceForTime(time.Date(2027, time.August, 14, 0, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09470, pODM1SummerEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pODM1SummerEV.PeriodName)
			assert.InDelta(t, 0.06608, pODM1SummerEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pODM1Winter, err := uODM1.priceForTime(time.Date(2026, time.October, 20, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09197, pODM1Winter.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", pODM1Winter.PeriodName)
			assert.InDelta(t, 0.06404, pODM1Winter.GenerationCreditDollarsPerKWH, 1e-6)
		}

		pODM1WinterEV, err := uODM1.priceForTime(time.Date(2026, time.October, 20, 6, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08227, pODM1WinterEV.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "EV Recharge", pODM1WinterEV.PeriodName)
			assert.InDelta(t, 0.05676, pODM1WinterEV.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Verify net metering on ODM-1-TOU with EV enabled does not panic
		err = uODM1.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_odm1_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				EVCredit:          true,
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pODM1NetEV, err := uODM1.priceForTime(time.Date(2026, time.October, 20, 6, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08227, pODM1NetEV.DollarsPerKWH, 1e-6) {
			assert.False(t, pODM1NetEV.SeparateGenerationCredit)
			assert.Zero(t, pODM1NetEV.GenerationCreditDollarsPerKWH)
		}
	})

	t.Run("Invalid Options and Unknown Plan Errors", func(t *testing.T) {
		u := &genericTOU{}

		// Southern schedules reject nmr_2025 and unknown schemes
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_ors_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nmr_2025",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown NV Energy net metering scheme")

		err = u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_south_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "invalid_scheme",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown NV Energy net metering scheme")

		// Northern schedules reject unknown schemes
		err = u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "nv_energy",
			UtilityRate:     "nv_energy_north_od1_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "invalid_scheme",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown NV Energy net metering scheme")

		// Direct nvEnergyPeriods rejects unknown plan names
		_, err = nvEnergyPeriods("nv_energy_south_unknown", types.UtilityRateOptions{}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown NV Energy rate plan")

		_, err = nvEnergyPeriods("unknown_plan", types.UtilityRateOptions{}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown NV Energy rate plan")
	})

	t.Run("Non-Overlapping Hourly Coverage With EV Credit Enabled", func(t *testing.T) {
		for _, plan := range []string{
			"nv_energy_south_ors_tou",
			"nv_energy_south_orm_tou",
			"nv_energy_north_od1_tou",
			"nv_energy_north_odm1_tou",
		} {
			periods, err := nvEnergyPeriods(plan, types.UtilityRateOptions{EVCredit: true}, []int{2026, 2027})
			require.NoError(t, err)

			// Verify winter, summer, and year-end boundary days have exactly 1 base and 1 solar period per hour
			for _, day := range []time.Time{
				time.Date(2026, time.May, 31, 0, 0, 0, 0, ptLocation),
				time.Date(2026, time.June, 1, 0, 0, 0, 0, ptLocation),
				time.Date(2026, time.September, 30, 0, 0, 0, 0, ptLocation),
				time.Date(2026, time.October, 1, 0, 0, 0, 0, ptLocation),
				time.Date(2026, time.December, 31, 0, 0, 0, 0, ptLocation),
				time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation),
			} {
				for h := 0; h < 24; h++ {
					ts := day.Add(time.Duration(h) * time.Hour)
					baseCount := 0
					solarCount := 0
					for i := range periods {
						ok, _, err := periods[i].Contains(ts)
						require.NoError(t, err)
						if ok {
							if periods[i].SeparateGenerationCredit {
								solarCount++
							} else {
								baseCount++
							}
						}
					}
					assert.Equal(t, 1, baseCount, "plan %s at %v must match 1 base period", plan, ts)
					assert.Equal(t, 1, solarCount, "plan %s at %v must match 1 solar period", plan, ts)
				}
			}
		}
	})
}
