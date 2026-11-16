package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAustinEnergy(t *testing.T) {
	ctx := context.Background()

	t.Run("Provider Metadata", func(t *testing.T) {
		info := austinEnergyUtilityInfo()
		assert.Equal(t, "austin_energy", info.ID)
		assert.Equal(t, "Austin Energy", info.Name)
		assert.Empty(t, info.RatesNotice)
		if assert.Len(t, info.Rates, 2) {
			assert.Equal(t, "austin_energy_residential", info.Rates[0].ID)
			assert.Equal(t, "austin_energy_tou", info.Rates[1].ID)
		}
	})

	t.Run("Residential Standard Inside City Tiers", func(t *testing.T) {
		info := austinEnergyUtilityInfo()
		resRate := info.Rates[0]
		ts := time.Date(2026, time.October, 15, 16, 0, 0, 0, ctLocation)

		// Tier 1: Base 0.04872 + PSA 0.03912 + CBC 0.01097 + Reg 0.01799 = 0.11680
		feesT1, err := resRate.GetFees(types.UtilityRateOptions{Location: "inside_city", RateClass: "tier1"})
		require.NoError(t, err)
		uT1 := &genericTOU{siteID: "test", periods: feesT1}
		pT1, err := uT1.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.11680, pT1.DollarsPerKWH, 1e-6)
		assert.True(t, pT1.BuyAllSellAll)
		assert.True(t, pT1.SeparateGenerationCredit)
		assert.InDelta(t, 0.1289, pT1.GenerationCreditDollarsPerKWH, 1e-6)
		assert.True(t, pT1.SeparateBatteryGenerationCredit)
		assert.InDelta(t, 0.0, pT1.BatteryGenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.1289, pT1.ExportRateDollars(types.Settings{}, nil), 1e-6)
		assert.InDelta(t, 0.0, pT1.BatteryExportRateDollars(types.Settings{}, nil), 1e-6)
		assert.Equal(t, "Base", pT1.PeriodName)

		// Tier 2: Base 0.05395 + PSA 0.03912 + CBC 0.01097 + Reg 0.01799 = 0.12203
		feesT2, err := resRate.GetFees(types.UtilityRateOptions{Location: "inside_city", RateClass: "tier2"})
		require.NoError(t, err)
		uT2 := &genericTOU{siteID: "test", periods: feesT2}
		pT2, err := uT2.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.12203, pT2.DollarsPerKWH, 1e-6)

		// Tier 3: Base 0.07901 + PSA 0.03912 + CBC 0.01097 + Reg 0.01799 = 0.14709
		feesT3, err := resRate.GetFees(types.UtilityRateOptions{Location: "inside_city", RateClass: "tier3"})
		require.NoError(t, err)
		uT3 := &genericTOU{siteID: "test", periods: feesT3}
		pT3, err := uT3.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.14709, pT3.DollarsPerKWH, 1e-6)

		// Tier 4: Base 0.11428 + PSA 0.03912 + CBC 0.01097 + Reg 0.01799 = 0.18236
		feesT4, err := resRate.GetFees(types.UtilityRateOptions{Location: "inside_city", RateClass: "tier4"})
		require.NoError(t, err)
		uT4 := &genericTOU{siteID: "test", periods: feesT4}
		pT4, err := uT4.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.18236, pT4.DollarsPerKWH, 1e-6)
	})

	t.Run("Residential Standard Outside City Tiers", func(t *testing.T) {
		info := austinEnergyUtilityInfo()
		resRate := info.Rates[0]
		ts := time.Date(2026, time.November, 10, 12, 0, 0, 0, ctLocation)

		// Tier 1: Base 0.04872 + PSA 0.03912 + CBC 0.00825 + Reg 0.01799 = 0.11408
		feesT1, err := resRate.GetFees(types.UtilityRateOptions{Location: "outside_city", RateClass: "tier1"})
		require.NoError(t, err)
		uT1 := &genericTOU{siteID: "test", periods: feesT1}
		pT1, err := uT1.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.11408, pT1.DollarsPerKWH, 1e-6)
		assert.True(t, pT1.BuyAllSellAll)
		assert.True(t, pT1.SeparateGenerationCredit)
		assert.InDelta(t, 0.1289, pT1.GenerationCreditDollarsPerKWH, 1e-6)
		assert.True(t, pT1.SeparateBatteryGenerationCredit)
		assert.InDelta(t, 0.0, pT1.BatteryGenerationCreditDollarsPerKWH, 1e-6)

		// Tier 2: Base 0.04897 + PSA 0.03912 + CBC 0.00825 + Reg 0.01799 = 0.11433
		feesT2, err := resRate.GetFees(types.UtilityRateOptions{Location: "outside_city", RateClass: "tier2"})
		require.NoError(t, err)
		uT2 := &genericTOU{siteID: "test", periods: feesT2}
		pT2, err := uT2.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.11433, pT2.DollarsPerKWH, 1e-6)

		// Tier 3: Base 0.06369 + PSA 0.03912 + CBC 0.00825 + Reg 0.01799 = 0.12905
		feesT3, err := resRate.GetFees(types.UtilityRateOptions{Location: "outside_city", RateClass: "tier3"})
		require.NoError(t, err)
		uT3 := &genericTOU{siteID: "test", periods: feesT3}
		pT3, err := uT3.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.12905, pT3.DollarsPerKWH, 1e-6)

		// Tier 4: Base 0.08334 + PSA 0.03912 + CBC 0.00825 + Reg 0.01799 = 0.14870
		feesT4, err := resRate.GetFees(types.UtilityRateOptions{Location: "outside_city", RateClass: "tier4"})
		require.NoError(t, err)
		uT4 := &genericTOU{siteID: "test", periods: feesT4}
		pT4, err := uT4.priceForTime(ts)
		require.NoError(t, err)
		assert.InDelta(t, 0.14870, pT4.DollarsPerKWH, 1e-6)
	})

	t.Run("Time-of-Use Inside City Periods", func(t *testing.T) {
		info := austinEnergyUtilityInfo()
		touRate := info.Rates[1]
		fees, err := touRate.GetFees(types.UtilityRateOptions{Location: "inside_city", RateClass: "tier1"})
		require.NoError(t, err)
		u := &genericTOU{siteID: "test", periods: fees}

		// Non-PSA Inside City Tier 1 = 0.04872 + 0.01097 + 0.01799 = 0.07768
		// Off-Peak (weekday 6:00 AM CT): 0.07768 + 0.02543 = 0.10311
		weekdayMorningOffPeak := time.Date(2026, time.October, 14, 6, 0, 0, 0, ctLocation) // Wednesday
		p, err := u.priceForTime(weekdayMorningOffPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.True(t, p.BuyAllSellAll)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.1289, p.GenerationCreditDollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateBatteryGenerationCredit)
		assert.InDelta(t, 0.0, p.BatteryGenerationCreditDollarsPerKWH, 1e-6)

		// Mid-Peak (weekday 7:00 AM CT): 0.07768 + 0.03912 = 0.11680
		weekdayMidPeakAM := time.Date(2026, time.October, 14, 7, 0, 0, 0, ctLocation)
		p, err = u.priceForTime(weekdayMidPeakAM)
		require.NoError(t, err)
		assert.InDelta(t, 0.11680, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)
		assert.InDelta(t, 0.1289, p.GenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0, p.BatteryGenerationCreditDollarsPerKWH, 1e-6)

		// On-Peak (weekday 3:00 PM CT): 0.07768 + 0.08020 = 0.15788
		weekdayOnPeak := time.Date(2026, time.October, 14, 15, 0, 0, 0, ctLocation)
		p, err = u.priceForTime(weekdayOnPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.15788, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.1289, p.GenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0, p.BatteryGenerationCreditDollarsPerKWH, 1e-6)

		// Mid-Peak evening (weekday 6:00 PM CT): 0.07768 + 0.03912 = 0.11680
		weekdayMidPeakPM := time.Date(2026, time.October, 14, 18, 0, 0, 0, ctLocation)
		p, err = u.priceForTime(weekdayMidPeakPM)
		require.NoError(t, err)
		assert.InDelta(t, 0.11680, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)

		// Off-Peak late night (weekday 10:00 PM CT): 0.07768 + 0.02543 = 0.10311
		weekdayNightOffPeak := time.Date(2026, time.October, 14, 22, 0, 0, 0, ctLocation)
		p, err = u.priceForTime(weekdayNightOffPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Weekend all day Off-Peak (Saturday 4:00 PM CT): 0.07768 + 0.02543 = 0.10311
		weekendAfternoon := time.Date(2026, time.October, 17, 16, 0, 0, 0, ctLocation) // Saturday
		p, err = u.priceForTime(weekendAfternoon)
		require.NoError(t, err)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
	})

	t.Run("Time-of-Use Outside City and ApplySettings", func(t *testing.T) {
		u := &genericTOU{siteID: "test-austin"}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "austin_energy",
			UtilityRate:     "austin_energy_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:  "outside_city",
				RateClass: "tier2",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		// Outside City Tier 2 non-PSA = 0.04897 + 0.00825 + 0.01799 = 0.07521
		// On-Peak = 0.07521 + 0.08020 = 0.15541
		p, err := u.priceForTime(time.Date(2026, time.November, 12, 16, 30, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15541, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.True(t, p.BuyAllSellAll)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.1289, p.GenerationCreditDollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateBatteryGenerationCredit)
		assert.InDelta(t, 0.0, p.BatteryGenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Residential Standard ApplySettings and Default Options", func(t *testing.T) {
		u := &genericTOU{siteID: "test-austin-default"}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "austin_energy",
			UtilityRate:     "austin_energy_residential",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		// Empty options default to inside_city + tier1: 0.04872 + 0.03912 + 0.01097 + 0.01799 = 0.11680
		p, err := u.priceForTime(time.Date(2026, time.November, 15, 16, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.11680, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Base", p.PeriodName)
		assert.True(t, p.BuyAllSellAll)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.1289, p.GenerationCreditDollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateBatteryGenerationCredit)
		assert.InDelta(t, 0.0, p.BatteryGenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("TOU Boundary Minutes, Weekend Transitions, and DST", func(t *testing.T) {
		u := &genericTOU{siteID: "test-austin-boundaries"}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "austin_energy",
			UtilityRate:     "austin_energy_tou",
		})
		require.NoError(t, err)

		// Inside City Tier 1: Off-Peak = 0.10311, Mid-Peak = 0.11680, On-Peak = 0.15788
		p, err := u.priceForTime(time.Date(2026, time.October, 14, 6, 59, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 14, 14, 59, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", p.PeriodName)
		assert.InDelta(t, 0.11680, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 14, 17, 59, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.15788, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 14, 21, 59, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", p.PeriodName)
		assert.InDelta(t, 0.11680, p.DollarsPerKWH, 1e-6)

		// Sunday evening -> Monday morning transition
		p, err = u.priceForTime(time.Date(2026, time.October, 18, 23, 59, 0, 0, ctLocation)) // Sunday
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 19, 7, 0, 0, 0, ctLocation)) // Monday 7:00 AM
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", p.PeriodName)
		assert.InDelta(t, 0.11680, p.DollarsPerKWH, 1e-6)

		// America/Chicago Fall-Back DST day (Sunday Nov 1, 2026) and Spring-Forward DST day (Sunday Mar 8, 2026)
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 1, 30, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.March, 8, 3, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10311, p.DollarsPerKWH, 1e-6)
	})
}
