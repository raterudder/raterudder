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
			UtilityProvider: "tou_example",
			UtilityRate:     "tou_example_1",
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
			h := cp.TSStart.In(loc).Hour()
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
			name: "test",
			periods: []types.UtilityFeesPeriod{
				{
					DollarsPerKWH: 0.10,
					Description:   "Base",
				},
				{
					DollarsPerKWH:            0.03,
					SeparateGenerationCredit: true,
					Description:              "Generation Credit",
				},
			},
		}

		target := time.Date(2026, 3, 9, 10, 0, 0, 0, loc)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.Equal(t, 0.10, p.DollarsPerKWH)
		assert.Equal(t, 0.03, p.GenerationCreditDollarsPerKWH)
		assert.True(t, p.SeparateGenerationCredit)
	})

	t.Run("Rutherford Electric Fallback", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		})
		require.NoError(t, err)
		assert.Equal(t, "rutherford_electric_tod", u.Name())
		assert.NotEmpty(t, u.periods)

		// Verify we can get a price (Nov 1st 2026 at 8 AM ET is on-peak)
		et, _ := time.LoadLocation("America/New_York")
		target := time.Date(2026, 11, 10, 8, 0, 0, 0, et) // Monday
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.Equal(t, 0.31443, p.DollarsPerKWH)
	})

	t.Run("Location consistency sets target location", func(t *testing.T) {
		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		u := &genericTOU{
			periods: []types.UtilityFeesPeriod{
				{
					UtilityPeriod: types.UtilityPeriod{
						LocationPtr: chi,
					},
					DollarsPerKWH: 0.10,
				},
				{
					UtilityPeriod: types.UtilityPeriod{
						LocationPtr: chi,
					},
					DollarsPerKWH:  0.05,
					GridAdditional: true,
				},
			},
		}

		// Use UTC time, it should be converted to Chicago by priceForTime
		target := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		p, err := u.priceForTime(target)
		require.NoError(t, err)

		// 12:00 UTC is 6:00 AM CST (Jan 1st)
		assert.Equal(t, 6, p.TSStart.Hour())
		assert.Equal(t, chi.String(), p.TSStart.Location().String())
	})

	t.Run("Mixed locations do not set target location", func(t *testing.T) {
		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		ny, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		u := &genericTOU{
			periods: []types.UtilityFeesPeriod{
				{
					UtilityPeriod: types.UtilityPeriod{
						LocationPtr: chi,
					},
					DollarsPerKWH: 0.10,
				},
				{
					UtilityPeriod: types.UtilityPeriod{
						LocationPtr: ny,
					},
					DollarsPerKWH:  0.05,
					GridAdditional: true,
				},
			},
		}

		target := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		p, err := u.priceForTime(target)
		require.NoError(t, err)

		// Target remains UTC
		assert.Equal(t, 12, p.TSStart.Hour())
		assert.Equal(t, time.UTC, p.TSStart.Location())
	})

	t.Run("LADWP", func(t *testing.T) {
		la, err := time.LoadLocation("America/Los_Angeles")
		require.NoError(t, err)

		u := &genericTOU{}

		// Test R-1A
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ladwp",
			UtilityRate:     "ladwp_r1a",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.February, 15, 12, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.24771, p.DollarsPerKWH)

		p, err = u.priceForTime(time.Date(2026, time.April, 15, 12, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.24362, p.DollarsPerKWH)

		// Test R-1B
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "ladwp",
			UtilityRate:     "ladwp_r1b",
		})
		require.NoError(t, err)

		// June 1, 2026 is a Monday
		// June High Peak (13:00 - 17:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 14, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.33078, p.DollarsPerKWH)

		// June Low Peak (10:00 - 13:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 11, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.27238, p.DollarsPerKWH)

		// June Low Peak (17:00 - 20:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 18, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.27238, p.DollarsPerKWH)

		// June Base (20:00 - 10:00 Weekdays)
		p, err = u.priceForTime(time.Date(2026, time.June, 1, 21, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.24494, p.DollarsPerKWH)

		// June Base (Weekends) - June 6, 2026 is Saturday
		p, err = u.priceForTime(time.Date(2026, time.June, 6, 14, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.24494, p.DollarsPerKWH)

		// Jan-Mar Peak - February 2, 2026 is Monday
		p, err = u.priceForTime(time.Date(2026, time.February, 2, 14, 0, 0, 0, la))
		require.NoError(t, err)
		assert.Equal(t, 0.27647, p.DollarsPerKWH)
	})
}
