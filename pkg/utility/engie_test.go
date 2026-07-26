package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngieUtility(t *testing.T) {
	t.Run("Solar Elec Single Rate - Ausgrid", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Flat $0.4011 import rate and $0.03 export rate year-round
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4011, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Solar Elec Single Rate - Endeavour", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4048, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3711, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - Powercor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "powercor",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3009, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - United Energy", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2883, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - Citipower Fallback", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2883, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Ausgrid Summer", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.Equal(t, "On-Peak", periods[0].Name)
		}

		// Summer Peak Season (Jan 15, 2026)
		summerDay := time.Date(2026, time.January, 15, 0, 0, 0, 0, sydLocation)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM -> $0.4288, export $0.0300
		p, err := u.priceForTime(summerDay.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4288, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.3927
		p, err = u.priceForTime(summerDay.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3927, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Ausgrid Non-Summer Non-Winter", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Non-Summer Non-Winter (April 15, 2026)
		autumnDay := time.Date(2026, time.April, 15, 0, 0, 0, 0, sydLocation)

		// Should be flat $0.3927 at all times
		p, err := u.priceForTime(autumnDay.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3927, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Ausgrid Winter Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Winter Peak Season (July 15, 2026)
		winterDay := time.Date(2026, time.July, 15, 0, 0, 0, 0, sydLocation)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM -> $0.4288, export $0.0300
		p, err := u.priceForTime(winterDay.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4288, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Endeavour Summer", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, Jan 14, 2026
		weekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, sydLocation)

		// Peak weekdays 4 PM - 8 PM: 5:00 PM -> $0.6564, export $0.0300
		p, err := u.priceForTime(weekday.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.6564, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak weekday: 10:00 AM -> $0.3838
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Saturday, Jan 17, 2026 - Off-Peak all day
		weekend := time.Date(2026, time.January, 17, 17, 0, 0, 0, sydLocation)
		p, err = u.priceForTime(weekend)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Endeavour Winter", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, July 15, 2026
		weekday := time.Date(2026, time.July, 15, 0, 0, 0, 0, sydLocation)

		// Peak weekdays 4 PM - 8 PM: 5:00 PM -> $0.4204, export $0.0300
		p, err := u.priceForTime(weekday.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4204, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak weekday: 10:00 AM -> $0.3838
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		// Everyday: Jan 15, 2026
		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, bneLocation)

		// Peak 4 PM - 9 PM: 6:00 PM -> $0.5613, export $0.0100
		p, err := u.priceForTime(day.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5613, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak 11 AM - 4 PM: 12:00 PM -> $0.2621
		p, err = u.priceForTime(day.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2621, p.DollarsPerKWH, 1e-6)
		}

		// Shoulder all other times: 8:00 AM -> $0.3059
		p, err = u.priceForTime(day.Add(8 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3059, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Powercor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "powercor",
			},
		})
		require.NoError(t, err)

		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, melLocation)

		// Peak 3 PM - 9 PM: 6:00 PM -> $0.4039, export $0.0100
		p, err := u.priceForTime(day.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4039, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.2365
		p, err = u.priceForTime(day.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2365, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - United Energy", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, melLocation)

		// Peak 3 PM - 9 PM: 6:00 PM -> $0.3837, export $0.0100
		p, err := u.priceForTime(day.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3837, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.2299
		p, err = u.priceForTime(day.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2299, p.DollarsPerKWH, 1e-6)
		}
	})
}
