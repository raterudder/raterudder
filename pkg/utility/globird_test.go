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
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Super Off-Peak"])
		assert.True(t, names["Shoulder"])

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

	t.Run("SOLARPLUS - Unsupported Location SAPN", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "sapn",
			},
		})
		assert.ErrorContains(t, err, "location sapn is not supported for plan globird_solarplus")
	})

	t.Run("FOUR4FREE - November 2026 Transition and 2027 Ausgrid", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Oct 31, 2026 (pre-revision): 10:30 AM is free ($0.0000), 2:30 PM is shoulder ($0.3751), 6:00 PM is peak ($0.5995 / $0.0800 FiT)
		p, err := u.priceForTime(time.Date(2026, time.October, 31, 10, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.October, 31, 14, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3751, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.October, 31, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5995, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0800, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Nov 1, 2026 (effective 2026-11-01): 10:30 AM is shoulder ($0.3190), 11:30 AM & 2:30 PM are free ($0.0000), 3:30 PM is shoulder ($0.3190), 6:00 PM is peak ($0.4620 / $0.1000 FiT)
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 10, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3190, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 11, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 14, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 15, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3190, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4620, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Mid-2027 schedule check (Jul 15, 2027)
		p, err = u.priceForTime(time.Date(2027, time.July, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4620, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.July, 15, 13, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.July, 15, 23, 30, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3190, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("FOUR4FREE - Endeavour Energex Powercor AusNet Citipower Revised 2027", func(t *testing.T) {
		// Endeavour
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "endeavour"},
		}))
		p, err := u.priceForTime(time.Date(2027, time.March, 10, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4510, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 9, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3190, p.DollarsPerKWH, 1e-6)
		}

		// Energex (default location when empty)
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "globird",
			UtilityRate:     "globird_four4free",
		}))
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 18, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4400, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0800, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 9, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2750, p.DollarsPerKWH, 1e-6)
		}

		// CitiPower
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "citipower"},
		}))
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3663, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 9, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2200, p.DollarsPerKWH, 1e-6)
		}

		// Powercor
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "powercor"},
		}))
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3850, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 9, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2310, p.DollarsPerKWH, 1e-6)
		}

		// AusNet
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausnet"},
		}))
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4400, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 10, 9, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2288, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("FOUR4FREE - United Energy and Jemena Distinct November 2026 Rates", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "united_energy"},
		}))

		// Nov 2026 & 2027 United Energy: Peak $0.3795, Free 11-15 $0.0000, Shoulder $0.2233, Export Peak $0.0500
		p, err := u.priceForTime(time.Date(2026, time.November, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3795, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.February, 15, 8, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2233, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
		}

		// Jemena: legacy pre-Nov 2026 ($0.4400 / $0.2640 / $0.0300) vs Nov 2026+ ($0.3575 / $0.2200 / $0.0500)
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "jemena"},
		}))
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4400, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3575, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.February, 15, 8, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2200, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("FOUR4FREE - SAPN and Essential Energy Across 2026-2027", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "sapn"},
		}))

		// SAPN (Australia/Adelaide): modeled across 2026-01-01 to 2028-01-01
		p, err := u.priceForTime(time.Date(2026, time.March, 15, 18, 0, 0, 0, adlLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5995, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0800, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.August, 15, 12, 0, 0, 0, adlLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.August, 15, 10, 0, 0, 0, adlLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3036, p.DollarsPerKWH, 1e-6)
		}

		// Essential Energy (Australia/Sydney): modeled across 2026-01-01 to 2028-01-01
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_four4free",
			UtilityRateOptions: types.UtilityRateOptions{Location: "essential"},
		}))
		p, err = u.priceForTime(time.Date(2026, time.March, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4510, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1000, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.August, 15, 14, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0000, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.August, 15, 10, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3080, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("SOLARPLUS - November 2026 Transitions and 2027 Schedules", func(t *testing.T) {
		u := &genericTOU{}

		// Ausgrid transition on 2026-11-12: $0.4939/$0.0200 -> $0.3388/$0.0300
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausgrid"},
		}))
		p, err := u.priceForTime(time.Date(2026, time.November, 11, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4939, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3388, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3388, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Endeavour transition on 2026-11-12: $0.6083/$0.4246/$0.0200 -> $0.4015/$0.3146/$0.0300
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{Location: "endeavour"},
		}))
		// Wednesday Nov 11, 2026 vs Thursday Nov 12, 2026
		p, err = u.priceForTime(time.Date(2026, time.November, 11, 15, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.6083, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 15, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4015, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.June, 16, 10, 0, 0, 0, sydLocation)) // Wednesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3146, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.June, 19, 15, 0, 0, 0, sydLocation)) // Saturday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3146, p.DollarsPerKWH, 1e-6)
		}

		// Energex transition on 2026-11-12: $0.5610/$0.3674/$0.0200 -> $0.3597/$0.2475/$0.0200
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{Location: "energex"},
		}))
		p, err = u.priceForTime(time.Date(2026, time.November, 11, 18, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5610, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 12, 18, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3597, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 10, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2475, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("SOLARPLUS - Essential Energy Across 2026-2027", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "globird",
			UtilityRate:        "globird_solarplus",
			UtilityRateOptions: types.UtilityRateOptions{Location: "essential"},
		}))

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Shoulder"])
		assert.True(t, names["Off-Peak"])

		// Early 2026 weekday: Wednesday Jan 14, 2026 at 6:00 PM -> Peak $0.4180, export $0.0300
		p, err := u.priceForTime(time.Date(2026, time.January, 14, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4180, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2027 weekday: Wednesday Jun 16, 2027
		weekday2027 := time.Date(2027, time.June, 16, 0, 0, 0, 0, sydLocation)
		// Peak (17:00-20:00): 6:00 PM -> $0.4180
		p, err = u.priceForTime(weekday2027.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4180, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		// Shoulder (07:00-17:00 & 20:00-22:00): 10:00 AM and 9:00 PM -> $0.3300
		p, err = u.priceForTime(weekday2027.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3300, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(weekday2027.Add(21 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3300, p.DollarsPerKWH, 1e-6)
		}
		// Off-Peak (22:00-07:00): 3:00 AM and 10:30 PM -> $0.2970
		p, err = u.priceForTime(weekday2027.Add(3 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2970, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(weekday2027.Add(22*time.Hour + 30*time.Minute))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2970, p.DollarsPerKWH, 1e-6)
		}

		// 2027 weekend: Saturday Jun 19, 2027 at 6:00 PM -> Off-Peak $0.2970
		p, err = u.priceForTime(time.Date(2027, time.June, 19, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2970, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Hourly Continuity Across All Supported Distributors 2026-2027", func(t *testing.T) {
		info := globirdUtilityInfo()
		for _, rate := range info.Rates {
			require.Len(t, rate.Options, 1)
			for _, choice := range rate.Options[0].Choices {
				periods, err := rate.GetFees(types.UtilityRateOptions{Location: choice.Value})
				require.NoError(t, err, "plan=%s location=%s", rate.ID, choice.Value)
				require.NotEmpty(t, periods)

				loc := periods[0].LocationPtr
				require.NotNil(t, loc)

				checkDays := []time.Time{
					time.Date(2026, time.January, 1, 0, 0, 0, 0, loc),
					time.Date(2026, time.October, 31, 0, 0, 0, 0, loc),
					time.Date(2026, time.November, 1, 0, 0, 0, 0, loc),
					time.Date(2026, time.November, 11, 0, 0, 0, 0, loc),
					time.Date(2026, time.November, 12, 0, 0, 0, 0, loc),
					time.Date(2026, time.December, 31, 0, 0, 0, 0, loc),
					time.Date(2027, time.January, 1, 0, 0, 0, 0, loc),
					time.Date(2027, time.June, 19, 0, 0, 0, 0, loc),
					time.Date(2027, time.December, 31, 0, 0, 0, 0, loc),
				}
				for _, day := range checkDays {
					for hour := 0; hour < 24; hour++ {
						ts := day.Add(time.Duration(hour) * time.Hour)
						baseMatches := 0
						exportMatches := 0
						for i := range periods {
							ok, _, err := periods[i].Contains(ts)
							require.NoError(t, err)
							if ok {
								if periods[i].SeparateGenerationCredit {
									exportMatches++
								} else {
									baseMatches++
								}
							}
						}
						assert.Equal(t, 1, baseMatches, "plan=%s loc=%s ts=%v expected 1 base period", rate.ID, choice.Value, ts)
						assert.Equal(t, 1, exportMatches, "plan=%s loc=%s ts=%v expected 1 export period", rate.ID, choice.Value, ts)
					}
				}
			}
		}
	})
}
