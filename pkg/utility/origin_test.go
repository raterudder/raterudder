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
}
