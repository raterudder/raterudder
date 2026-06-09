package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGloBirdUtility(t *testing.T) {
	t.Run("FOUR4FREE - Ausgrid", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Peak (4 PM - 11 PM): 6:00 PM -> $0.5995, export $0.0800
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5995, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0800, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Free Off-Peak (10 AM - 2 PM): 12:00 PM -> $0.0000, export $0.0000
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Shoulder (other times): 8:00 AM -> $0.3751, export $0.0000
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 8, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3751, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("FOUR4FREE - Citipower", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		// Peak (4 PM - 11 PM): 6:00 PM -> $0.4400, export $0.0300
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4400, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Free Off-Peak (10 AM - 2 PM): 12:00 PM -> $0.0000, export $0.0000
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Shoulder (other times): 8:00 AM -> $0.2640, export $0.0000
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 8, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2640, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("FOUR4FREE - United Energy Fallback", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		// Verify fallback matches Citipower: Peak (4 PM - 11 PM): 6:00 PM -> $0.4400, export $0.0300
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4400, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("SOLARPLUS - Ausgrid Flat", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Verify flat pricing at any hour (e.g., 6:00 PM and 10:00 AM)
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4939, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("SOLARPLUS - Endeavour", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, Jan 14, 2026
		weekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, sydLocation)

		// Peak (Weekday 1:00 PM - 7:59 PM): 3:00 PM -> $0.6083, export $0.0200
		p, err := u.priceForTime(weekday.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.6083, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (Weekday other times): 10:00 AM -> $0.4246
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4246, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Saturday, Jan 17, 2026
		weekend := time.Date(2026, time.January, 17, 0, 0, 0, 0, sydLocation)

		// Weekend Off-Peak (All Day): 3:00 PM -> $0.4246
		p, err = u.priceForTime(weekend.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4246, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("SOLARPLUS - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		// Peak Everyday (4 PM - 9 PM): 6:00 PM -> $0.5610, export $0.0200
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5610, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak Everyday: 10:00 AM -> $0.3674
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3674, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("SOLARPLUS - Unsupported Location Citipower", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		assert.ErrorContains(t, err, "location citipower is not supported for plan globird_solarplus")
	})
}
