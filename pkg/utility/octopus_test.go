package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOctopusUtility(t *testing.T) {
	t.Run("Provider Metadata and RatesNotice", func(t *testing.T) {
		info := octopusUtilityInfo()
		assert.Equal(t, "octopus", info.ID)
		assert.Equal(t, "Octopus Energy (UK)", info.Name)
		assert.Equal(t, "Intelligent Octopus Flux is not supported at this time.", info.RatesNotice)
		require.Len(t, info.Rates, 2)
		assert.Equal(t, "octopus_flux", info.Rates[0].ID)
		assert.Equal(t, "Octopus Flux", info.Rates[0].Name)
		assert.Equal(t, "octopus_intelligent_go", info.Rates[1].ID)
		assert.Equal(t, "Intelligent Octopus Go", info.Rates[1].Name)
	})

	t.Run("Octopus Flux - London", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["Off-Peak"])
		assert.True(t, names["Peak"])
		assert.True(t, names["Standard"])

		// Off-Peak (02:00 – 05:00): 03:00 -> Import £0.1456, Export £0.0524
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 3, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1456, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0524, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Peak (16:00 – 19:00): 17:00 -> Import £0.3396, Export £0.2860
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3396, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2860, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Standard (05:00 – 16:00, 19:00 – 02:00):
		// 10:00 -> Import £0.2425, Export £0.0980
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// 21:00 -> Import £0.2425, Export £0.0980
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 21, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// 01:00 -> Import £0.2425, Export £0.0980
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 1, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, p.GenerationCreditDollarsPerKWH, 1e-4)
		}
	})

	t.Run("Octopus Flux - Eastern", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "eastern",
			},
		})
		require.NoError(t, err)

		// Off-Peak 03:00 -> Import £0.1458, Export £0.0499
		p, err := u.priceForTime(time.Date(2026, time.November, 10, 3, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1458, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0499, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Peak 17:00 -> Import £0.3401, Export £0.2932
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3401, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2932, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Standard 12:00 -> Import £0.2429, Export £0.1011
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2429, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1011, p.GenerationCreditDollarsPerKWH, 1e-4)
		}
	})

	t.Run("Intelligent Octopus Go - London", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		// Overnight Off-Peak (23:30 – 05:30):
		// 02:00 -> Import £0.0700, Export £0.1500 (default outgoing_15p)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0700, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1500, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// 23:45 -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0700, p.DollarsPerKWH, 1e-4)
		}

		// 05:15 -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 5, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0700, p.DollarsPerKWH, 1e-4)
		}

		// Day Peak (05:30 – 23:30):
		// 05:45 -> Peak (£0.2802)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 5, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2802, p.DollarsPerKWH, 1e-4)
		}

		// 12:00 -> Peak (£0.2802)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2802, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1500, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// 23:15 -> Peak (£0.2802)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2802, p.DollarsPerKWH, 1e-4)
		}
	})

	t.Run("Intelligent Octopus Go - Export Options", func(t *testing.T) {
		// Test 12p export
		u12 := &genericTOU{}
		err := u12.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_12p",
			},
		})
		require.NoError(t, err)
		p12, err := u12.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1200, p12.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Test none export
		uNone := &genericTOU{}
		err = uNone.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "none",
			},
		})
		require.NoError(t, err)
		pNone, err := uNone.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.False(t, pNone.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, pNone.GenerationCreditDollarsPerKWH, 1e-4)
		}
	})
}
