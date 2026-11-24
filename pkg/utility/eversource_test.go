package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShiftEversourceSundayHoliday(t *testing.T) {
	t.Run("Sunday shifts to Monday", func(t *testing.T) {
		sunday := time.Date(2027, time.July, 4, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(sunday)
		expected := time.Date(2027, time.July, 5, 0, 0, 0, 0, time.UTC)
		assert.Equal(t, expected, shifted)
	})

	t.Run("Saturday does not shift", func(t *testing.T) {
		saturday := time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(saturday)
		assert.Equal(t, saturday, shifted)
	})

	t.Run("Weekday does not shift", func(t *testing.T) {
		wednesday := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(wednesday)
		assert.Equal(t, wednesday, shifted)
	})
}

func TestGetEversourceHolidays(t *testing.T) {
	t.Run("Holidays for 2027 contains shifted Independence Day", func(t *testing.T) {
		holidays := getEversourceHolidays(2027)
		// July 4, 2027 is a Sunday, so it shifts to July 5, 2027
		assert.Contains(t, holidays, "2027-07-05")
		assert.NotContains(t, holidays, "2027-07-04")
	})

	t.Run("Holidays for 2026 contains Saturday Independence Day (no shift)", func(t *testing.T) {
		holidays := getEversourceHolidays(2026)
		// July 4, 2026 is Saturday, so it does not shift
		assert.Contains(t, holidays, "2026-07-04")
	})
}

func TestEversourceUtilityInfo(t *testing.T) {
	u := &genericTOU{}

	t.Run("CT Residential Flat Rate 1", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
		})
		require.NoError(t, err)

		// Before July 1st, 2026
		targetBefore := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)
		pBefore, err := u.priceForTime(targetBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24666, pBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// After July 1st, 2026
		targetAfter := time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)
		pAfter, err := u.priceForTime(targetAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23602, pAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// In 2027
		target2027 := time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation)
		p2027, err := u.priceForTime(target2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23602, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("CT Residential Heating Rate 5", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_5",
		})
		require.NoError(t, err)

		// Before July 1st, 2026
		targetBefore := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation)
		pBefore, err := u.priceForTime(targetBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.22277, pBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// After July 1st, 2026
		targetAfter := time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation)
		pAfter, err := u.priceForTime(targetAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21213, pAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// In 2027
		target2027 := time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation)
		p2027, err := u.priceForTime(target2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21213, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("CT Residential TOU Rate 7", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_7",
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

		// --- Before July 1st, 2026 ---
		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeakBefore := time.Date(2026, time.June, 15, 13, 0, 0, 0, etLocation) // Monday 1 PM
		pOnBefore, err := u.priceForTime(onPeakBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31099, pOnBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOnBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeakBefore := time.Date(2026, time.June, 15, 9, 0, 0, 0, etLocation) // Monday 9 AM
		pOffBefore, err := u.priceForTime(offPeakBefore)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21871, pOffBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOffBefore.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// --- After July 1st, 2026 ---
		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeakAfter := time.Date(2026, time.July, 15, 13, 0, 0, 0, etLocation) // Wednesday 1 PM
		pOnAfter, err := u.priceForTime(onPeakAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.29982, pOnAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOnAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeakAfter := time.Date(2026, time.July, 15, 9, 0, 0, 0, etLocation) // Wednesday 9 AM
		pOffAfter, err := u.priceForTime(offPeakAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.20754, pOffAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOffAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekend
		weekendAfter := time.Date(2026, time.July, 12, 13, 0, 0, 0, etLocation) // Sunday 1 PM
		pWeAfter, err := u.priceForTime(weekendAfter)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.20754, pWeAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pWeAfter.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// --- In 2027 ---
		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeak2027 := time.Date(2027, time.June, 15, 13, 0, 0, 0, etLocation) // Tuesday 1 PM
		pOn2027, err := u.priceForTime(onPeak2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.29982, pOn2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOn2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeak2027 := time.Date(2027, time.June, 15, 9, 0, 0, 0, etLocation) // Tuesday 9 AM
		pOff2027, err := u.priceForTime(offPeak2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.20754, pOff2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOff2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential VPP Rate 7 Delivery", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_vpp",
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

		// Before July 1, 2026: historical flat $0.12078/kWh preserved
		pJunOn, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation)) // Monday 2 PM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12078, pJunOn.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pJunOn.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
		pJunOff, err := u.priceForTime(time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation)) // Monday 8 AM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12078, pJunOff.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pJunOff.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Starting July 1, 2026: On-Peak $0.15643/kWh (weekdays 12:00-20:00 ET), Off-Peak $0.09915/kWh
		pJulOn, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, etLocation)) // Wednesday 2 PM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.15643, pJulOn.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pJulOn.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", pJulOn.PeriodName)
		}
		pJulOff, err := u.priceForTime(time.Date(2026, time.July, 15, 9, 0, 0, 0, etLocation)) // Wednesday 9 AM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.09915, pJulOff.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pJulOff.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pJulOff.PeriodName)
		}
		pJulWeekend, err := u.priceForTime(time.Date(2026, time.July, 12, 14, 0, 0, 0, etLocation)) // Sunday 2 PM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.09915, pJulWeekend.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pJulWeekend.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pJulWeekend.PeriodName)
		}

		// In 2027
		p2027On, err := u.priceForTime(time.Date(2027, time.June, 15, 14, 0, 0, 0, etLocation)) // Tuesday 2 PM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.15643, p2027On.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p2027On.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p2027On.PeriodName)
		}
		p2027Off, err := u.priceForTime(time.Date(2027, time.June, 15, 9, 0, 0, 0, etLocation)) // Tuesday 9 AM
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.09915, p2027Off.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p2027Off.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p2027Off.PeriodName)
		}
	})

	t.Run("NH Residential Standard Rate R", func(t *testing.T) {
		defaultSettings := types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r",
		}
		err := u.ApplySettings(context.Background(), defaultSettings)
		require.NoError(t, err)

		// Before Feb 1, 2026 (January)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23128, pJan.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pJan.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pJan.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Feb - Sep 2026 (June & September)
		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23390, pJun.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pJun.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pJun.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
		pSep, err := u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23390, pSep.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pSep.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pSep.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Starting Oct 1, 2026 (Default "alternative" post-Sep 2017 net metering)
		pOct, err := u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.25292, pOct.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.00470, pOct.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.25762, pOct.ImportRateDollars(), 1e-6)
			assert.InDelta(t, -0.05045, pOct.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.20247, pOct.ExportRateDollars(defaultSettings, nil), 1e-6)
		}

		// 2027 ("alternative")
		p2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.25292, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.00470, p2027.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.25762, p2027.ImportRateDollars(), 1e-6)
			assert.InDelta(t, -0.05045, p2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.20247, p2027.ExportRateDollars(defaultSettings, nil), 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)

		// Pre-Sep 2017 "net" 1:1 net metering option
		netSettings := types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), netSettings))

		pOctNet, err := u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.25762, pOctNet.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOctNet.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.25762, pOctNet.ImportRateDollars(), 1e-6)
			assert.InDelta(t, 0.0, pOctNet.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.25762, pOctNet.ExportRateDollars(netSettings, []types.Price{pOctNet}), 1e-6)
		}
	})

	t.Run("NH Residential TOU Rate R-OTOD", func(t *testing.T) {
		defaultSettings := types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_otod",
		}
		err := u.ApplySettings(context.Background(), defaultSettings)
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])

		// --- Before Feb 1, 2026 (January) ---
		onPeakJan := time.Date(2026, time.January, 15, 14, 0, 0, 0, etLocation) // Thursday 2 PM
		pOnJan, err := u.priceForTime(onPeakJan)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34792, pOnJan.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOnJan.GridUseDollarsPerKWH, 1e-6)
		}

		offPeakJan := time.Date(2026, time.January, 15, 10, 0, 0, 0, etLocation) // Thursday 10 AM
		pOffJan, err := u.priceForTime(offPeakJan)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19583, pOffJan.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOffJan.GridUseDollarsPerKWH, 1e-6)
		}

		// --- Feb - Sep 2026 (June) ---
		onPeakJun := time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation) // Monday 2 PM
		pOnJun, err := u.priceForTime(onPeakJun)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35054, pOnJun.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOnJun.GridUseDollarsPerKWH, 1e-6)
		}

		offPeakJun := time.Date(2026, time.June, 15, 10, 0, 0, 0, etLocation) // Monday 10 AM
		pOffJun, err := u.priceForTime(offPeakJun)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19845, pOffJun.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOffJun.GridUseDollarsPerKWH, 1e-6)
		}

		weekendJun := time.Date(2026, time.June, 14, 14, 0, 0, 0, etLocation) // Sunday 2 PM
		pWeJun, err := u.priceForTime(weekendJun)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19845, pWeJun.DollarsPerKWH, 1e-6)
		}

		// --- Starting Oct 1, 2026 & 2027 (Default "alternative" post-Sep 2017 net metering) ---
		onPeakOct := time.Date(2026, time.October, 15, 14, 0, 0, 0, etLocation) // Thursday 2 PM
		pOnOct, err := u.priceForTime(onPeakOct)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.36723, pOnOct.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.00368, pOnOct.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.37091, pOnOct.ImportRateDollars(), 1e-6)
			assert.InDelta(t, -0.06076, pOnOct.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.30647, pOnOct.ExportRateDollars(defaultSettings, nil), 1e-6)
		}

		offPeakOct := time.Date(2026, time.October, 15, 10, 0, 0, 0, etLocation) // Thursday 10 AM
		pOffOct, err := u.priceForTime(offPeakOct)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21915, pOffOct.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.00368, pOffOct.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.22283, pOffOct.ImportRateDollars(), 1e-6)
			assert.InDelta(t, -0.04457, pOffOct.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.17458, pOffOct.ExportRateDollars(defaultSettings, nil), 1e-6)
		}

		// Off-peak: Holiday (New Year's Day, Jan 1st 2027 is Friday)
		holiday2027 := time.Date(2027, time.January, 1, 14, 0, 0, 0, etLocation) // 2 PM
		pHol2027, err := u.priceForTime(holiday2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21915, pHol2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.00368, pHol2027.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.22283, pHol2027.ImportRateDollars(), 1e-6)
			assert.InDelta(t, -0.04457, pHol2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.17458, pHol2027.ExportRateDollars(defaultSettings, nil), 1e-6)
		}

		// On-peak in 2027
		onPeak2027 := time.Date(2027, time.June, 15, 14, 0, 0, 0, etLocation) // Tuesday 2 PM
		pOn2027, err := u.priceForTime(onPeak2027)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.36723, pOn2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.00368, pOn2027.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.37091, pOn2027.ImportRateDollars(), 1e-6)
			assert.InDelta(t, -0.06076, pOn2027.GenerationAdjustmentDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.30647, pOn2027.ExportRateDollars(defaultSettings, nil), 1e-6)
		}

		// Pre-Sep 2017 "net" 1:1 net metering option
		netSettings := types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_otod",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), netSettings))

		pOnOctNet, err := u.priceForTime(onPeakOct)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.37091, pOnOctNet.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOnOctNet.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOnOctNet.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
		pOffOctNet, err := u.priceForTime(offPeakOct)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.22283, pOffOctNet.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOffOctNet.GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, pOffOctNet.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH Unfiled Rate R-EV Removed", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_ev",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown utility rate: eversource_nh_rate_r_ev")
	})

	t.Run("MA Residential Standard Rate R-1 - Default / Fixed Supply", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)

		// January 2026 (fallback)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.30471, pJan.DollarsPerKWH, 1e-6)
		}

		// June 2026 (Feb-Jul period)
		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34209, pJun.DollarsPerKWH, 1e-6)
		}

		// August 2026 (Aug-Dec period)
		pAug, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35903, pAug.DollarsPerKWH, 1e-6)
		}

		// June 2027 (2027+ projection)
		p2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35903, p2027.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Explicit Fixed Supply Option", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "ema",
				GenerationRate: "fixed",
			},
		})
		require.NoError(t, err)

		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34209, pJun.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Monthly Supply Option", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				GenerationRate: "monthly",
			},
		})
		require.NoError(t, err)

		// January 2026 (fallback)
		pJan, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.30471, pJan.DollarsPerKWH, 1e-6)
		}

		// February 2026
		pFeb, err := u.priceForTime(time.Date(2026, time.February, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.40198, pFeb.DollarsPerKWH, 1e-6)
		}

		// June 2026
		pJun, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31642, pJun.DollarsPerKWH, 1e-6)
		}

		// August 2026
		pAug, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32835, pAug.DollarsPerKWH, 1e-6)
		}

		// January 2027 ($0.18580 delivery + $0.25510 monthly supply)
		pJan2027, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.44090, pJan2027.DollarsPerKWH, 1e-6)
		}

		// February - December 2027 falls back to fixed supply ($0.18580 + $0.17323 = $0.35903)
		pFeb2027, err := u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35903, pFeb2027.DollarsPerKWH, 1e-6)
		}
		pJun2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35903, pJun2027.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Cape Cod Zone", func(t *testing.T) {
		// Fixed supply ($0.20081 delivery + EMA Basic Service supply)
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "cape",
				GenerationRate: "fixed",
			},
		})
		require.NoError(t, err)

		pJun2026, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35710, pJun2026.DollarsPerKWH, 1e-6) // 0.20081 + 0.15629
		}
		pAug2026, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.37404, pAug2026.DollarsPerKWH, 1e-6) // 0.20081 + 0.17323
		}
		pJun2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.37404, pJun2027.DollarsPerKWH, 1e-6)
		}

		// Monthly supply on Cape Cod
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "cape",
				GenerationRate: "monthly",
			},
		})
		require.NoError(t, err)

		pJan2027, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.45591, pJan2027.DollarsPerKWH, 1e-6) // 0.20081 + 0.25510
		}
		pFeb2027, err := u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.37404, pFeb2027.DollarsPerKWH, 1e-6) // 0.20081 + 0.17323 fallback
		}
	})

	t.Run("MA Residential Standard Rate R-1 - Western MA Zone", func(t *testing.T) {
		// Fixed supply ($0.18580 delivery + WMA Basic Service fixed supply)
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "wma",
				GenerationRate: "fixed",
			},
		})
		require.NoError(t, err)

		pJun2026, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32263, pJun2026.DollarsPerKWH, 1e-6) // 0.18580 + 0.13683
		}
		pAug2026, err := u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34514, pAug2026.DollarsPerKWH, 1e-6) // 0.18580 + 0.15934
		}
		pJun2027, err := u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34514, pJun2027.DollarsPerKWH, 1e-6)
		}

		// Monthly supply ($0.18580 delivery + WMA Basic Service monthly supply)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "wma",
				GenerationRate: "monthly",
			},
		})
		require.NoError(t, err)

		assertWMAMonth := func(month time.Month, expectedDollarsPerKWH float64) {
			pm, err := u.priceForTime(time.Date(2026, month, 15, 12, 0, 0, 0, etLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, expectedDollarsPerKWH, pm.DollarsPerKWH, 1e-6, "month %v", month)
			}
		}
		assertWMAMonth(time.January, 0.30471)   // 0.18580 + 0.11891
		assertWMAMonth(time.February, 0.36605)  // 0.18580 + 0.18025
		assertWMAMonth(time.March, 0.31892)     // 0.18580 + 0.13312
		assertWMAMonth(time.April, 0.30673)     // 0.18580 + 0.12093
		assertWMAMonth(time.May, 0.30034)       // 0.18580 + 0.11454
		assertWMAMonth(time.June, 0.30428)      // 0.18580 + 0.11848
		assertWMAMonth(time.July, 0.32620)      // 0.18580 + 0.14040
		assertWMAMonth(time.August, 0.31771)    // 0.18580 + 0.13191
		assertWMAMonth(time.September, 0.30200) // 0.18580 + 0.11620
		assertWMAMonth(time.October, 0.30197)   // 0.18580 + 0.11617
		assertWMAMonth(time.November, 0.31926)  // 0.18580 + 0.13346
		assertWMAMonth(time.December, 0.37999)  // 0.18580 + 0.19419

		pJan2027, err := u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.42322, pJan2027.DollarsPerKWH, 1e-6) // 0.18580 + 0.23742
		}
		pFeb2027, err := u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34514, pFeb2027.DollarsPerKWH, 1e-6) // 0.18580 + 0.15934 fallback
		}
		pOct2027, err := u.priceForTime(time.Date(2027, time.October, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34514, pOct2027.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH and MA Options Metadata", func(t *testing.T) {
		info := eversourceUtilityInfo()
		ratesByID := make(map[string]types.UtilityRateInfo, len(info.Rates))
		for _, r := range info.Rates {
			ratesByID[r.ID] = r
		}
		assert.NotContains(t, ratesByID, "eversource_nh_rate_r_ev")

		for _, nhID := range []string{"eversource_nh_rate_r", "eversource_nh_rate_r_otod"} {
			nhRate, ok := ratesByID[nhID]
			if assert.True(t, ok, "missing %s", nhID) && assert.Len(t, nhRate.Options, 1) {
				opt := nhRate.Options[0]
				assert.Equal(t, "netMeteringScheme", opt.Field)
				assert.Equal(t, "alternative", opt.Default)
				if assert.Len(t, opt.Choices, 2) {
					assert.Equal(t, "alternative", opt.Choices[0].Value)
					assert.Equal(t, "net", opt.Choices[1].Value)
				}
			}
		}

		maRate, ok := ratesByID["eversource_ma_residential"]
		if assert.True(t, ok) && assert.Len(t, maRate.Options, 2) {
			locOpt := maRate.Options[0]
			assert.Equal(t, "location", locOpt.Field)
			assert.Equal(t, "ema", locOpt.Default)
			if assert.Len(t, locOpt.Choices, 3) {
				assert.Equal(t, "ema", locOpt.Choices[0].Value)
				assert.Equal(t, "cape", locOpt.Choices[1].Value)
				assert.Equal(t, "wma", locOpt.Choices[2].Value)
			}

			genOpt := maRate.Options[1]
			assert.Equal(t, "generationRate", genOpt.Field)
			assert.Equal(t, "fixed", genOpt.Default)
			if assert.Len(t, genOpt.Choices, 2) {
				assert.Equal(t, "fixed", genOpt.Choices[0].Value)
				assert.Equal(t, "monthly", genOpt.Choices[1].Value)
			}
		}
	})

	t.Run("CT Application Year Options Metadata", func(t *testing.T) {
		info := eversourceUtilityInfo()
		var ctRate *types.UtilityRateInfo
		for i := range info.Rates {
			if info.Rates[i].ID == "eversource_ct_rate_1" {
				ctRate = &info.Rates[i]
				break
			}
		}
		require.NotNil(t, ctRate)

		var appYearOpt *types.UtilityRateOption
		for i := range ctRate.Options {
			if ctRate.Options[i].Field == "netMeteringScheme" {
				appYearOpt = &ctRate.Options[i]
				break
			}
		}
		require.NotNil(t, appYearOpt)
		assert.NotEmpty(t, appYearOpt.Name)
		assert.Equal(t, types.UtilityOptionTypeSelect, appYearOpt.Type)
		assert.NotEmpty(t, appYearOpt.Default)
		assert.Len(t, appYearOpt.Choices, 6)

		choiceValues := make(map[string]bool)
		for _, c := range appYearOpt.Choices {
			choiceValues[c.Value] = true
		}
		assert.Contains(t, choiceValues, "2026")
		assert.Contains(t, choiceValues, "2025")
		assert.Contains(t, choiceValues, "2024")
		assert.Contains(t, choiceValues, "2023")
		assert.Contains(t, choiceValues, "2022")
		assert.Contains(t, choiceValues, "net")
		assert.Contains(t, choiceValues, appYearOpt.Default)
	})

	t.Run("CT Residential Flat Rate 1 - Application Year 2025", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "2025",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24666, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0050, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential Flat Rate 1 - Application Year 2024", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "2024",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24666, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential Flat Rate 1 - Application Year Legacy Net", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24666, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential Heating Rate 5 - Application Year 2025 via applicationYear field", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_5",
			UtilityRateOptions: types.UtilityRateOptions{
				ApplicationYear: "2025",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.22277, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0050, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential TOU Rate 7 - Application Year 2025", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_7",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "2025",
			},
		})
		require.NoError(t, err)

		// On-Peak
		pOn, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31099, pOn.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0050, pOn.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-Peak
		pOff, err := u.priceForTime(time.Date(2026, time.June, 15, 10, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21871, pOff.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0050, pOff.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})
}
