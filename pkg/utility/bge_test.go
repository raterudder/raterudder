package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBGE(t *testing.T) {
	t.Run("Schedule R Residential Service", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_r",
		})
		require.NoError(t, err)

		// Summer (July 15, 2026 at 12:00 PM) -> June - Sept
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19700, p.DollarsPerKWH, 1e-6)

		// Non-Summer (November 15, 2026 at 12:00 PM) -> Oct - May
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19103, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Schedule RL Residential Optional TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_rl",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Inter-Peak"])
		assert.True(t, names["Off-Peak"])

		// Summer Peak: Weekday (Monday July 13, 2026 at 12:00 PM) -> 10 AM to 8 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32152, p.DollarsPerKWH, 1e-6)

		// Summer Intermediate: Weekday (Monday July 13, 2026 at 8:00 AM) -> 7 AM to 10 AM
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.22152, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: Weekday (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15152, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15152, p.DollarsPerKWH, 1e-6)

		// Non-Summer Peak: Weekday (Monday January 12, 2026 at 8:00 AM) -> 7 AM to 11 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29152, p.DollarsPerKWH, 1e-6)

		// Non-Summer Intermediate: Weekday (Monday January 12, 2026 at 12:00 PM) -> 11 AM to 5 PM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21152, p.DollarsPerKWH, 1e-6)

		// Non-Summer Off-Peak: Weekday (Monday January 12, 2026 at 10:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 22, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15652, p.DollarsPerKWH, 1e-6)

		// Holiday (President's Day: Monday February 16, 2026 at 12:00 PM) -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.February, 16, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15652, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule EV Residential EV TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_ev",
		})
		require.NoError(t, err)

		// Summer Peak: Weekday (Monday July 13, 2026 at 4:00 PM) -> 10 AM to 8 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32181, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: Weekday (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15181, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule RD Residential Delivery & Energy TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_rd",
		})
		require.NoError(t, err)

		// Summer Peak: Weekday (Monday July 13, 2026 at 4:00 PM) -> 3 PM to 8 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.38353, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: Weekday (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.13664, p.DollarsPerKWH, 1e-6)
	})
}
