package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPSEUtility(t *testing.T) {
	t.Run("Schedule 7 Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pse",
			UtilityRate:     "pse_7",
		})
		require.NoError(t, err)

		// Flat $0.187465 year-round
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.187465, p.DollarsPerKWH, 1e-6)
		}

		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.187465, p.DollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Schedule 307 TOU Prices", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pse",
			UtilityRate:     "pse_307",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])

		// Winter (Oct - Mar): Tue, Jan 20, 2026
		winterTue := time.Date(2026, time.January, 20, 0, 0, 0, 0, ptLocation)

		// Peak (7am-10am, 5pm-8pm): 8:00 AM -> $0.532445
		p, err := u.priceForTime(winterTue.Add(8 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.532445, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 12:00 PM -> $0.108197
		p, err = u.priceForTime(winterTue.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.108197, p.DollarsPerKWH, 1e-6)
		}

		// Summer (Apr - Sep): Mon, Jun 15, 2026
		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, ptLocation)

		// Peak (5pm-8pm): 6:00 PM -> $0.335186
		p, err = u.priceForTime(summerMon.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.335186, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 12:00 PM -> $0.108197
		p, err = u.priceForTime(summerMon.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.108197, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Schedule 327 TOU Prices", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pse",
			UtilityRate:     "pse_327",
		})
		require.NoError(t, err)

		// Winter (Oct - Mar): Tue, Jan 20, 2026
		winterTue := time.Date(2026, time.January, 20, 0, 0, 0, 0, ptLocation)

		// Peak (7am-10am, 5pm-8pm): 8:00 AM -> $0.503575
		p, err := u.priceForTime(winterTue.Add(8 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.503575, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 12:00 PM -> $0.127088
		p, err = u.priceForTime(winterTue.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.127088, p.DollarsPerKWH, 1e-6)
		}

		// Super Off-Peak (Daily 11 PM - 7 AM): 2:00 AM -> $0.075542
		p, err = u.priceForTime(winterTue.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.075542, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Holidays and Shifted Holiday Exclusions", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pse",
			UtilityRate:     "pse_307",
		})
		require.NoError(t, err)

		// MLK Day (Jan 19, 2026): Mon, Jan 19 at 8:00 AM -> Off-Peak ($0.108197)
		p, err := u.priceForTime(time.Date(2026, time.January, 19, 8, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.108197, p.DollarsPerKWH, 1e-6)
		}

		// Independence Day holiday shifting: July 4, 2026 is Saturday, so Friday, July 3rd is the observed holiday!
		// Fri, Jul 3, 2026 at 6:00 PM (normally peak hour) -> should be Off-Peak ($0.108197)
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 18, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.108197, p.DollarsPerKWH, 1e-6)
		}

		// Native American Heritage Day (Friday after Thanksgiving): Fri, Nov 27, 2026 at 8:00 AM -> Off-Peak ($0.108197)
		p, err = u.priceForTime(time.Date(2026, time.November, 27, 8, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.108197, p.DollarsPerKWH, 1e-6)
		}
	})
}
