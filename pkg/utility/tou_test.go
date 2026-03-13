package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOUUtility(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	t.Run("Basic TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tou",
			UtilityRate:     "example",
		})
		require.NoError(t, err)

		// Test GetCurrentPrice
		p, err := u.GetCurrentPrice(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "tou", p.Provider)

		// Test GetFuturePrices
		future, err := u.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Len(t, future, 48)

		// Test GetConfirmedPrices
		start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2023, 1, 2, 0, 40, 0, 0, time.UTC)
		confirmed, err := u.GetConfirmedPrices(context.Background(), start, end)
		require.NoError(t, err)
		assert.Len(t, confirmed, 25)

		// Verify price changes over a day
		for _, cp := range confirmed {
			// New York location should be set
			h := cp.TSStart.In(u.location).Hour()
			if h >= 0 && h < 6 {
				assert.Equal(t, 0.01, cp.DollarsPerKWH)
			} else if h >= 6 && h < 12 {
				assert.Equal(t, 0.02, cp.DollarsPerKWH)
			} else {
				assert.Equal(t, 0.10, cp.DollarsPerKWH)
			}
		}
	})

	t.Run("GenerationCredit period sets GenerationCreditDollarsPerKWH", func(t *testing.T) {
		u := &genericTOU{
			name:     "test",
			location: loc,
			periods: []types.UtilityFeesPeriod{
				{
					UtilityPeriod: types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH: 0.10,
					Description:   "Base",
				},
				{
					UtilityPeriod:    types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH:    0.03,
					GenerationCredit: true,
					Description:      "Generation Credit",
				},
			},
		}

		target := time.Date(2026, 3, 9, 10, 0, 0, 0, loc)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.10, p.DollarsPerKWH, 0.0001, "base supply rate set by first period")
		assert.InDelta(t, 0.03, p.GenerationCreditDollarsPerKWH, 0.0001, "generation credit set by second period")
		assert.False(t, p.SeparateGenerationCredit, "SeparateGenerationCredit should be false when not set on period")
	})

	t.Run("SeparateGenerationCredit propagates to Price", func(t *testing.T) {
		u := &genericTOU{
			name:     "test",
			location: loc,
			periods: []types.UtilityFeesPeriod{
				{
					UtilityPeriod: types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH: 0.08,
					Description:   "Base",
				},
				{
					UtilityPeriod:            types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH:            0.025,
					GenerationCredit:         true,
					SeparateGenerationCredit: true,
					Description:              "Post-2025 Credit",
				},
			},
		}

		target := time.Date(2026, 3, 9, 14, 0, 0, 0, loc)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.08, p.DollarsPerKWH, 0.0001)
		assert.InDelta(t, 0.025, p.GenerationCreditDollarsPerKWH, 0.0001)
		assert.True(t, p.SeparateGenerationCredit, "SeparateGenerationCredit must be set")
	})

	t.Run("GridAdditional and GenerationCredit and base all apply together", func(t *testing.T) {
		u := &genericTOU{
			name:     "test",
			location: loc,
			periods: []types.UtilityFeesPeriod{
				{
					UtilityPeriod: types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH: 0.08,
					Description:   "Base supply",
				},
				{
					UtilityPeriod:  types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH:  0.05,
					GridAdditional: true,
					Description:    "Delivery",
				},
				{
					UtilityPeriod:            types.UtilityPeriod{HourStart: 0, HourEnd: 24},
					DollarsPerKWH:            0.02,
					GenerationCredit:         true,
					SeparateGenerationCredit: true,
					Description:              "Generation credit",
				},
			},
		}

		target := time.Date(2026, 3, 9, 10, 0, 0, 0, loc)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.InDelta(t, 0.08, p.DollarsPerKWH, 0.0001, "base supply only")
		assert.InDelta(t, 0.05, p.GridUseDollarsPerKWH, 0.0001, "delivery charge only")
		assert.InDelta(t, 0.02, p.GenerationCreditDollarsPerKWH, 0.0001, "generation credit only")
		assert.True(t, p.SeparateGenerationCredit)
	})
}
