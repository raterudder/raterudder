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

	t.Run("MVEA", func(t *testing.T) {
		mveaLoc, err := time.LoadLocation("America/Denver")
		require.NoError(t, err)

		u := &genericTOU{}

		// 1. Test Flat Rate 16.01
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "mvea",
			UtilityRate:     "mvea_16_01",
		})
		require.NoError(t, err)

		// Flat rate should be $0.12475/kWh at any time
		p, err := u.priceForTime(time.Date(2026, time.June, 1, 14, 0, 0, 0, mveaLoc))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12475, p.DollarsPerKWH, 1e-6)
			// Standard net metering: SeparateGenerationCredit is false, so it offsets at the consumption rate
			assert.False(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p, err = u.priceForTime(time.Date(2026, time.June, 7, 18, 0, 0, 0, mveaLoc)) // Sunday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12475, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2. Test Time of Day Rate 16.05
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "mvea",
			UtilityRate:     "mvea_16_05",
		})
		require.NoError(t, err)

		// On-peak: Mon-Sat 5:00 p.m. - 9:00 p.m. ($0.32371/kWh)
		// Off-peak: All other times ($0.08346/kWh)
		// Export credit: Always $0.00/kWh (SeparateGenerationCredit = true)

		// Wednesday, June 3, 2026 at 6:00 p.m. (On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 3, 18, 0, 0, 0, mveaLoc))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32371, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Wednesday, June 3, 2026 at 10:00 a.m. (Off-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 3, 10, 0, 0, 0, mveaLoc))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.08346, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Saturday, June 6, 2026 at 8:00 p.m. (On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 6, 20, 0, 0, 0, mveaLoc))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32371, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Sunday, June 7, 2026 at 6:00 p.m. (Off-Peak - Sunday is never on-peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 7, 18, 0, 0, 0, mveaLoc))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.08346, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("NOVEC", func(t *testing.T) {
		u := &genericTOU{}

		// 1. Test Schedule R-1 (Residential Service) flat rate
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "novec",
			UtilityRate:     "novec_r1",
		})
		require.NoError(t, err)
		assert.Equal(t, "novec_r1", u.Name())
		assert.NotEmpty(t, u.periods)

		ny, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// R-1 rate should be flat $0.11079 all day, all year
		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, ny))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.11079, p.DollarsPerKWH, 1e-6)
			}
		}

		// 2. Test Schedule R-1-EV (Residential EV Service) TOU rate
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "novec",
			UtilityRate:     "novec_r1_ev",
		})
		require.NoError(t, err)
		assert.Equal(t, "novec_r1_ev", u.Name())

		// Test On-Peak: 6:00 a.m. to 11:00 p.m. daily ($0.12694)
		// 6:00 a.m.
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 6, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12694, p.DollarsPerKWH, 1e-6)
		}
		// 12:00 p.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12694, p.DollarsPerKWH, 1e-6)
		}
		// 10:59 p.m. (22:59)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 22, 59, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12694, p.DollarsPerKWH, 1e-6)
		}

		// Test Off-Peak: 11:00 p.m. to 6:00 a.m. daily ($0.07320)
		// 11:00 p.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 23, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.07320, p.DollarsPerKWH, 1e-6)
		}
		// 2:00 a.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 2, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.07320, p.DollarsPerKWH, 1e-6)
		}
		// 5:59 a.m.
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 5, 59, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.07320, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("CenterPoint Indiana", func(t *testing.T) {
		u := &genericTOU{}

		// 1. Test standard rate settings application (default export: edg)
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "centerpoint_indiana",
			UtilityRate:     "centerpoint_indiana_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "edg",
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "centerpoint_indiana_rs", u.Name())
		assert.NotEmpty(t, u.periods)

		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)

		// Flat rate should be $0.200142/kWh at any time
		// EDG rate should be separate generation credit of $0.05561
		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, chi))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.200142, p.DollarsPerKWH, 1e-6)
				assert.True(t, p.SeparateGenerationCredit)
				assert.InDelta(t, 0.05561, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}

		// 2. Test net metering option (1:1)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "centerpoint_indiana",
			UtilityRate:     "centerpoint_indiana_rs",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, chi))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.200142, p.DollarsPerKWH, 1e-6)
				assert.False(t, p.SeparateGenerationCredit)
				assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}
	})
}
