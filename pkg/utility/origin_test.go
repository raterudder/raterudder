package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOriginUtility(t *testing.T) {
	t.Run("Origin Battery Maximiser - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_maximiser",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		// Peak (4:00 PM - 8:59 PM): 5:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 17, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3410, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1870, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0700, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Origin Battery Starter - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_starter",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		// Peak (4:00 PM - 8:59 PM): 5:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 17, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4290, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1800, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3152, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Origin Go Solar Variable - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_solar_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Shoulder"])
		assert.True(t, names["Off-Peak"])

		// Weekday: Wednesday, Jan 14, 2026
		weekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, bneLocation)

		// Weekday Peak (4:00 PM - 7:59 PM): 5:00 PM -> $0.4580, export $0.03
		p, err := u.priceForTime(weekday.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4580, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekday Shoulder (7:00 AM - 3:59 PM): 10:00 AM -> $0.3302
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3302, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Saturday, Jan 17, 2026
		weekend := time.Date(2026, time.January, 17, 0, 0, 0, 0, bneLocation)

		// Weekend Shoulder (7:00 AM - 9:59 PM): 10:00 AM -> $0.3302
		p, err = u.priceForTime(weekend.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3302, p.DollarsPerKWH, 1e-6)
		}

		// Weekend Off-Peak (10:00 PM - 6:59 AM): 2:00 AM (Sunday Jan 18) -> $0.2780
		p, err = u.priceForTime(weekend.Add(26 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2780, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Origin Go Variable - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, Jan 14, 2026
		weekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, bneLocation)

		// Weekday Peak (4:00 PM - 7:59 PM): 5:00 PM -> $0.4486, export $0.03
		p, err := u.priceForTime(weekday.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4486, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekday Shoulder (7:00 AM - 3:59 PM): 10:00 AM -> $0.3234
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3234, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Saturday, Jan 17, 2026
		weekend := time.Date(2026, time.January, 17, 0, 0, 0, 0, bneLocation)

		// Weekend Shoulder (7:00 AM - 9:59 PM): 10:00 AM -> $0.3234
		p, err = u.priceForTime(weekend.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3234, p.DollarsPerKWH, 1e-6)
		}

		// Weekend Off-Peak (10:00 PM - 6:59 AM): 2:00 AM (Sunday Jan 18) -> $0.2723
		p, err = u.priceForTime(weekend.Add(26 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2723, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Origin Solar Boost - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_solar_boost",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, bneLocation)

		// Peak (4:00 PM - 8:59 PM): 5:00 PM -> $0.4998, export $0.03
		p, err := u.priceForTime(day.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4998, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (11:00 AM - 3:59 PM): 12:00 PM -> $0.2826
		p, err = u.priceForTime(day.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2826, p.DollarsPerKWH, 1e-6)
		}

		// Shoulder (9:00 PM - 10:59 AM): 8:00 AM -> $0.3240
		p, err = u.priceForTime(day.Add(8 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3240, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Citipower Melbourne - Battery Maximiser", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_maximiser",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		// Peak (5:00 PM - 8:59 PM): 6:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2640, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1870, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Citipower Melbourne - Go Solar Variable", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_solar_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 16, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3561, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2162, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Citipower Melbourne - Battery Starter", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_starter",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		// Peak (5:00 PM - 8:59 PM): 6:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4015, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1800, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2322, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Citipower Melbourne - Go Variable", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 16, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3269, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1986, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Citipower Melbourne - Solar Boost", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_solar_boost",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 16, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3633, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2206, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Powercor Melbourne - Battery Maximiser", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_maximiser",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "powercor",
			},
		})
		require.NoError(t, err)

		// Peak (5:00 PM - 8:59 PM): 6:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2970, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1870, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Powercor Melbourne - Go Solar Variable", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_solar_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "powercor",
			},
		})
		require.NoError(t, err)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 16, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3959, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2318, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Ausgrid Sydney - Battery Maximiser", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_maximiser",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Peak (5:00 PM - 8:59 PM): 6:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5390, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1870, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Ausgrid Sydney - Go Solar Variable", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_solar_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Summer Weekday: Wednesday, Jan 14, 2026
		summerWeekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, sydLocation)

		// Summer Peak (2:00 PM - 7:59 PM): 3:00 PM -> $0.6944, export $0.03
		p, err := u.priceForTime(summerWeekday.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.6944, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Shoulder (7:00 AM - 1:59 PM): 10:00 AM -> $0.3633
		p, err = u.priceForTime(summerWeekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3633, p.DollarsPerKWH, 1e-6)
		}

		// Summer Off-Peak (10:00 PM - 6:59 AM): 2:00 AM -> $0.2119
		p, err = u.priceForTime(summerWeekday.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2119, p.DollarsPerKWH, 1e-6)
		}

		// Winter Weekday: Wednesday, July 15, 2026
		winterWeekday := time.Date(2026, time.July, 15, 0, 0, 0, 0, sydLocation)

		// Winter Peak (5:00 PM - 8:59 PM): 6:00 PM -> $0.6944, export $0.03
		p, err = u.priceForTime(winterWeekday.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.6944, p.DollarsPerKWH, 1e-6)
		}

		// Winter Shoulder (7:00 AM - 4:59 PM): 10:00 AM -> $0.3633
		p, err = u.priceForTime(winterWeekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3633, p.DollarsPerKWH, 1e-6)
		}

		// Shoulder Month: Wednesday, April 15, 2026
		shoulderWeekday := time.Date(2026, time.April, 15, 0, 0, 0, 0, sydLocation)

		// Shoulder Month Shoulder (7:00 AM - 9:59 PM): 10:00 AM -> $0.3633
		p, err = u.priceForTime(shoulderWeekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3633, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Endeavour Sydney - Battery Maximiser", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_maximiser",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Peak (5:00 PM - 8:59 PM): 6:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4400, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2090, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0600, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Endeavour Sydney - Go Solar Variable", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_solar_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, Jan 14, 2026
		weekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, sydLocation)

		// Weekday Peak (1:00 PM - 7:59 PM): 3:00 PM -> $0.5214, export $0.03
		p, err := u.priceForTime(weekday.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5214, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekday Shoulder (7:00 AM - 12:59 PM): 10:00 AM -> $0.4179
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4179, p.DollarsPerKWH, 1e-6)
		}

		// Weekday Off-Peak (10:00 PM - 6:59 AM): 2:00 AM -> $0.2770
		p, err = u.priceForTime(weekday.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2770, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Saturday, Jan 17, 2026
		weekend := time.Date(2026, time.January, 17, 0, 0, 0, 0, sydLocation)

		// Weekend Off-Peak (All Day): 10:00 AM -> $0.2770
		p, err = u.priceForTime(weekend.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2770, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("United Energy Melbourne - Battery Maximiser", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_battery_maximiser",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		// Peak (5:00 PM - 8:59 PM): 6:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2970, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1870, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0500, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("United Energy Melbourne - Go Solar Variable", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_go_solar_variable",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 16, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3760, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 10:00 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 10, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2253, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Unsupported Location Invalid", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "origin",
			UtilityRate:     "origin_solar_boost",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "invalid_distributor",
			},
		})
		assert.ErrorContains(t, err, "unsupported location: invalid_distributor")
	})

	t.Run("Energex October 2026 Transition and 2027 - Go Variable and Solar Boost", func(t *testing.T) {
		uGo := &genericTOU{}
		require.NoError(t, uGo.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_go_variable",
			UtilityRateOptions: types.UtilityRateOptions{Location: "energex"},
		}))

		// Pre-Oct 1, 2026: Wednesday Sep 30, 2026 still uses weekday/weekend TOU
		pPrePeak, err := uGo.priceForTime(time.Date(2026, time.September, 30, 17, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4486, pPrePeak.DollarsPerKWH, 1e-6)
		}
		pPreDay, err := uGo.priceForTime(time.Date(2026, time.September, 30, 12, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3234, pPreDay.DollarsPerKWH, 1e-6)
		}

		// From Oct 1, 2026 through 2027: everyday 16-21 peak ($0.4587), 11-16 off-peak ($0.0671), 21-11 shoulder ($0.24288)
		for _, day := range []time.Time{
			time.Date(2026, time.October, 1, 0, 0, 0, 0, bneLocation),
			time.Date(2027, time.January, 16, 0, 0, 0, 0, bneLocation), // Saturday in 2027
		} {
			pPeak, err := uGo.priceForTime(day.Add(20 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "On-Peak", pPeak.PeriodName)
				assert.InDelta(t, 0.4587, pPeak.DollarsPerKWH, 1e-6)
				assert.True(t, pPeak.SeparateGenerationCredit)
				assert.InDelta(t, 0.0300, pPeak.GenerationCreditDollarsPerKWH, 1e-6)
			}
			pOffPeak, err := uGo.priceForTime(day.Add(12 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Off-Peak", pOffPeak.PeriodName)
				assert.InDelta(t, 0.0671, pOffPeak.DollarsPerKWH, 1e-6)
			}
			pShoulder, err := uGo.priceForTime(day.Add(8 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Shoulder", pShoulder.PeriodName)
				assert.InDelta(t, 0.24288, pShoulder.DollarsPerKWH, 1e-6)
			}
		}

		uBoost := &genericTOU{}
		require.NoError(t, uBoost.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_solar_boost",
			UtilityRateOptions: types.UtilityRateOptions{Location: "energex"},
		}))

		pBoostPre, err := uBoost.priceForTime(time.Date(2026, time.September, 30, 17, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4998, pBoostPre.DollarsPerKWH, 1e-6)
		}

		day2027 := time.Date(2027, time.March, 10, 0, 0, 0, 0, bneLocation)
		pBoostPeak, err := uBoost.priceForTime(day2027.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.Equal(t, "On-Peak", pBoostPeak.PeriodName)
			assert.InDelta(t, 0.477851, pBoostPeak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pBoostPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pBoostOff, err := uBoost.priceForTime(day2027.Add(13 * time.Hour))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pBoostOff.PeriodName)
			assert.InDelta(t, 0.069839, pBoostOff.DollarsPerKWH, 1e-6)
		}
		pBoostShoulder, err := uBoost.priceForTime(day2027.Add(22 * time.Hour))
		if assert.NoError(t, err) {
			assert.Equal(t, "Shoulder", pBoostShoulder.PeriodName)
			assert.InDelta(t, 0.252956, pBoostShoulder.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Victoria October 2026 Transition and 2027 - Go Variable and Solar Boost", func(t *testing.T) {
		assertVic := func(loc string, goPeak, goDay, goOff, boostPeak, boostDay, boostOff float64) {
			uGo := &genericTOU{}
			require.NoError(t, uGo.ApplySettings(context.Background(), types.Settings{
				UtilityProvider:    "origin",
				UtilityRate:        "origin_go_variable",
				UtilityRateOptions: types.UtilityRateOptions{Location: loc},
			}))

			for _, day := range []time.Time{
				time.Date(2026, time.October, 1, 0, 0, 0, 0, melLocation),
				time.Date(2027, time.July, 15, 0, 0, 0, 0, melLocation),
			} {
				// 15:00 was peak pre-Oct 2026, now Daytime (11:00-16:00)
				pDay, err := uGo.priceForTime(day.Add(15 * time.Hour))
				if assert.NoError(t, err) {
					assert.Equal(t, "Daytime", pDay.PeriodName)
					assert.InDelta(t, goDay, pDay.DollarsPerKWH, 1e-6)
					assert.InDelta(t, 0.0100, pDay.GenerationCreditDollarsPerKWH, 1e-6)
				}
				pPeak, err := uGo.priceForTime(day.Add(16 * time.Hour))
				if assert.NoError(t, err) {
					assert.Equal(t, "On-Peak", pPeak.PeriodName)
					assert.InDelta(t, goPeak, pPeak.DollarsPerKWH, 1e-6)
				}
				pOff, err := uGo.priceForTime(day.Add(22 * time.Hour))
				if assert.NoError(t, err) {
					assert.Equal(t, "Off-Peak", pOff.PeriodName)
					assert.InDelta(t, goOff, pOff.DollarsPerKWH, 1e-6)
				}
			}

			uBoost := &genericTOU{}
			require.NoError(t, uBoost.ApplySettings(context.Background(), types.Settings{
				UtilityProvider:    "origin",
				UtilityRate:        "origin_solar_boost",
				UtilityRateOptions: types.UtilityRateOptions{Location: loc},
			}))

			day2027 := time.Date(2027, time.February, 10, 0, 0, 0, 0, melLocation)
			pBoostPeak, err := uBoost.priceForTime(day2027.Add(18 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "On-Peak", pBoostPeak.PeriodName)
				assert.InDelta(t, boostPeak, pBoostPeak.DollarsPerKWH, 1e-6)
				assert.InDelta(t, 0.0100, pBoostPeak.GenerationCreditDollarsPerKWH, 1e-6)
			}
			pBoostDay, err := uBoost.priceForTime(day2027.Add(12 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Daytime", pBoostDay.PeriodName)
				assert.InDelta(t, boostDay, pBoostDay.DollarsPerKWH, 1e-6)
			}
			pBoostOff, err := uBoost.priceForTime(day2027.Add(6 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Off-Peak", pBoostOff.PeriodName)
				assert.InDelta(t, boostOff, pBoostOff.DollarsPerKWH, 1e-6)
			}
		}

		assertVic("citipower", 0.35629, 0.15433, 0.19690, 0.383097, 0.165891, 0.211695)
		assertVic("united_energy", 0.37499, 0.16049, 0.20592, 0.403194, 0.172590, 0.221397)
		assertVic("powercor", 0.38753, 0.16049, 0.20911, 0.416691, 0.172590, 0.224895)
	})

	t.Run("Ausgrid October 2026 Transition and 2027 - Go Variable and Solar Boost", func(t *testing.T) {
		uGo := &genericTOU{}
		require.NoError(t, uGo.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_go_variable",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausgrid"},
		}))

		// Pre-Oct 1, 2026: Sep 30, 2026 at 10:00 AM -> legacy shoulder $0.3318
		pPre, err := uGo.priceForTime(time.Date(2026, time.September, 30, 10, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3318, pPre.DollarsPerKWH, 1e-6)
		}

		// Oct 15, 2026 (non-peak month Sep-Oct): 16:00 is Off-Peak $0.21637
		pOct, err := uGo.priceForTime(time.Date(2026, time.October, 15, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pOct.PeriodName)
			assert.InDelta(t, 0.21637, pOct.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pOct.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Nov 14, 2026 (Saturday in Nov-Mar): daily 15:00-21:00 Peak applies on weekends too -> $0.5412
		pNovSatPeak, err := uGo.priceForTime(time.Date(2026, time.November, 14, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "On-Peak", pNovSatPeak.PeriodName)
			assert.InDelta(t, 0.5412, pNovSatPeak.DollarsPerKWH, 1e-6)
		}
		pNovSatOff, err := uGo.priceForTime(time.Date(2026, time.November, 14, 14, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pNovSatOff.PeriodName)
			assert.InDelta(t, 0.21637, pNovSatOff.DollarsPerKWH, 1e-6)
		}

		// Apr 15, 2027 (Apr-May non-peak) vs Jul 15, 2027 (Jun-Aug winter peak)
		pApr2027, err := uGo.priceForTime(time.Date(2027, time.April, 15, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pApr2027.PeriodName)
			assert.InDelta(t, 0.21637, pApr2027.DollarsPerKWH, 1e-6)
		}
		pJul2027, err := uGo.priceForTime(time.Date(2027, time.July, 15, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "On-Peak", pJul2027.PeriodName)
			assert.InDelta(t, 0.5412, pJul2027.DollarsPerKWH, 1e-6)
		}

		uBoost := &genericTOU{}
		require.NoError(t, uBoost.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_solar_boost",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausgrid"},
		}))
		pBoostPeak, err := uBoost.priceForTime(time.Date(2027, time.January, 15, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "On-Peak", pBoostPeak.PeriodName)
			assert.InDelta(t, 0.601359, pBoostPeak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pBoostPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pBoostOff, err := uBoost.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pBoostOff.PeriodName)
			assert.InDelta(t, 0.240383, pBoostOff.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Endeavour October 2026 Transition and 2027 - Go Variable and Solar Boost", func(t *testing.T) {
		uGo := &genericTOU{}
		require.NoError(t, uGo.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_go_variable",
			UtilityRateOptions: types.UtilityRateOptions{Location: "endeavour"},
		}))

		// Pre-Oct 1, 2026: Sep 30, 2026 14:00 -> legacy weekday peak $0.4874
		pPre, err := uGo.priceForTime(time.Date(2026, time.September, 30, 14, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4874, pPre.DollarsPerKWH, 1e-6)
		}

		// Oct 15, 2026 (Thursday, Apr-Oct season): Peak 16-20 ($0.43362), Solar Soak 10-14 ($0.11363), Off-Peak ($0.32835)
		octThu := time.Date(2026, time.October, 15, 0, 0, 0, 0, sydLocation)
		pOctPeak, err := uGo.priceForTime(octThu.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.Equal(t, "On-Peak", pOctPeak.PeriodName)
			assert.InDelta(t, 0.43362, pOctPeak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pOctPeak.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pOctSoak, err := uGo.priceForTime(octThu.Add(11 * time.Hour))
		if assert.NoError(t, err) {
			assert.Equal(t, "Solar Soak", pOctSoak.PeriodName)
			assert.InDelta(t, 0.11363, pOctSoak.DollarsPerKWH, 1e-6)
		}
		pOctOff, err := uGo.priceForTime(octThu.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pOctOff.PeriodName)
			assert.InDelta(t, 0.32835, pOctOff.DollarsPerKWH, 1e-6)
		}

		// Jan 2027 (Nov-Mar season): Thursday Jan 14 weekday peak ($0.42845), Saturday Jan 16 17:00 off-peak & 11:00 solar soak
		pJanPeak, err := uGo.priceForTime(time.Date(2027, time.January, 14, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "On-Peak", pJanPeak.PeriodName)
			assert.InDelta(t, 0.42845, pJanPeak.DollarsPerKWH, 1e-6)
		}
		pJanSatOff, err := uGo.priceForTime(time.Date(2027, time.January, 16, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "Off-Peak", pJanSatOff.PeriodName)
			assert.InDelta(t, 0.32835, pJanSatOff.DollarsPerKWH, 1e-6)
		}
		pJanSatSoak, err := uGo.priceForTime(time.Date(2027, time.January, 16, 11, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.Equal(t, "Solar Soak", pJanSatSoak.PeriodName)
			assert.InDelta(t, 0.11363, pJanSatSoak.DollarsPerKWH, 1e-6)
		}

		uBoost := &genericTOU{}
		require.NoError(t, uBoost.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_solar_boost",
			UtilityRateOptions: types.UtilityRateOptions{Location: "endeavour"},
		}))
		pBoostSumPeak, err := uBoost.priceForTime(time.Date(2027, time.February, 11, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.465718, pBoostSumPeak.DollarsPerKWH, 1e-6)
		}
		pBoostWinPeak, err := uBoost.priceForTime(time.Date(2027, time.June, 10, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.471372, pBoostWinPeak.DollarsPerKWH, 1e-6)
		}
		pBoostSoak, err := uBoost.priceForTime(time.Date(2027, time.June, 10, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.123475, pBoostSoak.DollarsPerKWH, 1e-6)
		}
		pBoostOff, err := uBoost.priceForTime(time.Date(2027, time.June, 10, 21, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.356906, pBoostOff.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Origin Battery Earner - All Distributors", func(t *testing.T) {
		assertEarner := func(loc string, tz *time.Location, peakHour int, impPeak, impOff, expPeak, expOff float64) {
			u := &genericTOU{}
			require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
				UtilityProvider:    "origin",
				UtilityRate:        "origin_battery_earner",
				UtilityRateOptions: types.UtilityRateOptions{Location: loc},
			}))

			day := time.Date(2027, time.March, 15, 0, 0, 0, 0, tz)
			pPeak, err := u.priceForTime(day.Add(time.Duration(peakHour) * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "On-Peak", pPeak.PeriodName)
				assert.InDelta(t, impPeak, pPeak.DollarsPerKWH, 1e-6)
				assert.True(t, pPeak.SeparateGenerationCredit)
				assert.InDelta(t, expPeak, pPeak.GenerationCreditDollarsPerKWH, 1e-6)
			}

			pBeforePeak, err := u.priceForTime(day.Add(time.Duration(peakHour-1) * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Off-Peak", pBeforePeak.PeriodName)
				assert.InDelta(t, impOff, pBeforePeak.DollarsPerKWH, 1e-6)
				assert.True(t, pBeforePeak.SeparateGenerationCredit)
				assert.InDelta(t, expOff, pBeforePeak.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}

		assertEarner("energex", bneLocation, 16, 0.3289, 0.2849, 0.2800, 0.0300)
		assertEarner("ausgrid", sydLocation, 17, 0.5401, 0.2981, 0.2900, 0.0300)
		assertEarner("endeavour", sydLocation, 17, 0.5544, 0.3036, 0.2900, 0.0300)
		assertEarner("citipower", melLocation, 17, 0.2992, 0.2728, 0.2650, 0.0100)
		assertEarner("united_energy", melLocation, 17, 0.3190, 0.2728, 0.2650, 0.0100)
		assertEarner("powercor", melLocation, 17, 0.3663, 0.2750, 0.2650, 0.0100)
	})

	t.Run("Origin Battery Charge - All Distributors", func(t *testing.T) {
		assertCharge := func(loc string, tz *time.Location, impPeak, impShoulder, impOvernight, expOff float64) {
			u := &genericTOU{}
			require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
				UtilityProvider:    "origin",
				UtilityRate:        "origin_battery_charge",
				UtilityRateOptions: types.UtilityRateOptions{Location: loc},
			}))

			day := time.Date(2027, time.April, 20, 0, 0, 0, 0, tz)

			// Morning Peak (06:00-09:00) & Off-Peak Export
			pMorningPeak, err := u.priceForTime(day.Add(7 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "On-Peak", pMorningPeak.PeriodName)
				assert.InDelta(t, impPeak, pMorningPeak.DollarsPerKWH, 1e-6)
				assert.InDelta(t, expOff, pMorningPeak.GenerationCreditDollarsPerKWH, 1e-6)
			}

			// Shoulder (09:00-11:00 and 15:00-16:00)
			for _, h := range []int{10, 15} {
				pShoulder, err := u.priceForTime(day.Add(time.Duration(h) * time.Hour))
				if assert.NoError(t, err) {
					assert.Equal(t, "Shoulder", pShoulder.PeriodName)
					assert.InDelta(t, impShoulder, pShoulder.DollarsPerKWH, 1e-6)
					assert.InDelta(t, expOff, pShoulder.GenerationCreditDollarsPerKWH, 1e-6)
				}
			}

			// Free window (11:00-15:00 -> $0.0000)
			pFree, err := u.priceForTime(day.Add(12 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Free", pFree.PeriodName)
				assert.InDelta(t, 0.0000, pFree.DollarsPerKWH, 1e-6)
				assert.InDelta(t, expOff, pFree.GenerationCreditDollarsPerKWH, 1e-6)
			}

			// Evening Peak Import (16:00-22:00) with Peak Export (18:00-21:00 -> $0.1100) vs Off-Peak Export (17:00 & 21:00)
			pEve17, err := u.priceForTime(day.Add(17 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "On-Peak", pEve17.PeriodName)
				assert.InDelta(t, impPeak, pEve17.DollarsPerKWH, 1e-6)
				assert.InDelta(t, expOff, pEve17.GenerationCreditDollarsPerKWH, 1e-6)
			}
			pEve19, err := u.priceForTime(day.Add(19 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "On-Peak", pEve19.PeriodName)
				assert.InDelta(t, impPeak, pEve19.DollarsPerKWH, 1e-6)
				assert.InDelta(t, 0.1100, pEve19.GenerationCreditDollarsPerKWH, 1e-6)
			}

			// Overnight (22:00-06:00)
			pNight, err := u.priceForTime(day.Add(23 * time.Hour))
			if assert.NoError(t, err) {
				assert.Equal(t, "Overnight", pNight.PeriodName)
				assert.InDelta(t, impOvernight, pNight.DollarsPerKWH, 1e-6)
				assert.InDelta(t, expOff, pNight.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}

		assertCharge("energex", bneLocation, 0.43605, 0.28260, 0.31200, 0.0300)
		assertCharge("ausgrid", sydLocation, 0.49400, 0.33210, 0.36300, 0.0300)
		assertCharge("endeavour", sydLocation, 0.52250, 0.33930, 0.36900, 0.0300)
		assertCharge("citipower", melLocation, 0.37335, 0.24660, 0.29100, 0.0100)
		assertCharge("united_energy", melLocation, 0.38475, 0.26640, 0.31000, 0.0100)
		assertCharge("powercor", melLocation, 0.39045, 0.27900, 0.32200, 0.0100)
	})

	t.Run("Legacy IDs and Battery Starter October 2026 Transitions", func(t *testing.T) {
		info := originUtilityInfo()
		rateNames := make(map[string]string, len(info.Rates))
		for _, r := range info.Rates {
			rateNames[r.ID] = r.Name
		}
		assert.Equal(t, "Origin Go Variable", rateNames["origin_go_variable"])
		assert.Equal(t, "Origin Solar Boost", rateNames["origin_solar_boost"])
		assert.Equal(t, "Origin Battery Earner", rateNames["origin_battery_earner"])
		assert.Equal(t, "Origin Battery Charge", rateNames["origin_battery_charge"])
		assert.Equal(t, "Origin Battery Maximiser (Legacy)", rateNames["origin_battery_maximiser"])
		assert.Equal(t, "Origin Battery Starter (Legacy)", rateNames["origin_battery_starter"])
		assert.Equal(t, "Origin Go Solar Variable (Legacy)", rateNames["origin_go_solar_variable"])

		// Victoria Battery Starter transitions on 2026-10-01 to Battery Earner rates
		uVicStarter := &genericTOU{}
		require.NoError(t, uVicStarter.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_battery_starter",
			UtilityRateOptions: types.UtilityRateOptions{Location: "citipower"},
		}))
		pVicSep30, err := uVicStarter.priceForTime(time.Date(2026, time.September, 30, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4015, pVicSep30.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1800, pVicSep30.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pVicOct1, err := uVicStarter.priceForTime(time.Date(2026, time.October, 1, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2992, pVicOct1.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.2650, pVicOct1.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// QLD & NSW Battery Starter transitions on 2026-10-27 to Battery Earner rates
		uQLDStarter := &genericTOU{}
		require.NoError(t, uQLDStarter.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_battery_starter",
			UtilityRateOptions: types.UtilityRateOptions{Location: "energex"},
		}))
		pQLDOct26, err := uQLDStarter.priceForTime(time.Date(2026, time.October, 26, 17, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4290, pQLDOct26.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1800, pQLDOct26.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pQLDOct27, err := uQLDStarter.priceForTime(time.Date(2026, time.October, 27, 17, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3289, pQLDOct27.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.2800, pQLDOct27.GenerationCreditDollarsPerKWH, 1e-6)
		}

		uNSWStarter := &genericTOU{}
		require.NoError(t, uNSWStarter.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "origin",
			UtilityRate:        "origin_battery_starter",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausgrid"},
		}))
		pNSWOct26, err := uNSWStarter.priceForTime(time.Date(2026, time.October, 26, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5731, pNSWOct26.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1800, pNSWOct26.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pNSWOct27, err := uNSWStarter.priceForTime(time.Date(2026, time.October, 27, 18, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5401, pNSWOct27.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.2900, pNSWOct27.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Continuous 2026-2027 Coverage Across All Distributors and DST Transitions", func(t *testing.T) {
		info := originUtilityInfo()
		locations := []struct {
			id string
			tz *time.Location
		}{
			{"energex", bneLocation},
			{"citipower", melLocation},
			{"united_energy", melLocation},
			{"powercor", melLocation},
			{"ausgrid", sydLocation},
			{"endeavour", sydLocation},
		}

		// Verify critical transition, seasonal, DST spring-forward (Oct 4, 2026), DST fall-back (Apr 5, 2026 & Apr 4, 2027), and year-boundary dates.
		sampleDays := [][3]int{
			{2026, 1, 1},
			{2026, 4, 5},
			{2026, 9, 30},
			{2026, 10, 1},
			{2026, 10, 4},
			{2026, 10, 26},
			{2026, 10, 27},
			{2026, 10, 31},
			{2026, 11, 1},
			{2026, 12, 31},
			{2027, 1, 1},
			{2027, 3, 31},
			{2027, 4, 1},
			{2027, 4, 4},
			{2027, 5, 31},
			{2027, 6, 1},
			{2027, 8, 31},
			{2027, 9, 1},
			{2027, 10, 3},
			{2027, 11, 1},
			{2027, 12, 31},
		}

		for _, r := range info.Rates {
			for _, loc := range locations {
				periods, err := r.GetFees(types.UtilityRateOptions{Location: loc.id})
				require.NoError(t, err, "plan %s loc %s", r.ID, loc.id)

				for _, ymd := range sampleDays {
					dayStart := time.Date(ymd[0], time.Month(ymd[1]), ymd[2], 0, 0, 0, 0, loc.tz)
					dayEnd := time.Date(ymd[0], time.Month(ymd[1]), ymd[2]+1, 0, 0, 0, 0, loc.tz)
					for ts := dayStart; ts.Before(dayEnd); ts = ts.Add(time.Hour) {
						baseMatches := 0
						solarMatches := 0
						namedMatches := 0
						for i := range periods {
							ok, _, err := periods[i].Contains(ts)
							require.NoError(t, err)
							if !ok {
								continue
							}
							if periods[i].SeparateGenerationCredit {
								solarMatches++
							} else {
								baseMatches++
								if periods[i].Name != "" {
									namedMatches++
								}
							}
						}
						assert.Equal(t, 1, baseMatches, "plan %s loc %s at %v must match 1 base period", r.ID, loc.id, ts)
						assert.Equal(t, 1, solarMatches, "plan %s loc %s at %v must match 1 solar export period", r.ID, loc.id, ts)
						assert.Equal(t, 1, namedMatches, "plan %s loc %s at %v must match 1 named import period", r.ID, loc.id, ts)
					}
				}
			}
		}
	})
}
