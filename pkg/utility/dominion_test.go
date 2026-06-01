package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDominion(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	t.Run("Schedule 1 Residential Service", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1",
		})
		require.NoError(t, err)

		// Summer (July 15, 2026 at 12:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.076602, p.DollarsPerKWH, 1e-6)

		// Non-Summer (November 15, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.075454, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule 1G TOU Summer", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1g",
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (Monday July 13, 2026 at 4:00 PM) -> 3 PM to 6 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.210415, p.DollarsPerKWH, 1e-6)

		// Summer Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.051367, p.DollarsPerKWH, 1e-6)

		// Summer Weekday Super Off-Peak (Monday July 13, 2026 at 2:00 AM) -> 12 AM to 5 AM
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.033486, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.051367, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Super Off-Peak (Saturday July 18, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.033486, p.DollarsPerKWH, 1e-6)

		// Summer Holiday (Labor Day: Monday Sept 7, 2026 at 4:00 PM) -> should be Off-Peak (no peak on holidays)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.051367, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule 1G TOU Winter", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1g",
		})
		require.NoError(t, err)

		// Winter Weekday On-Peak Morning (Monday January 12, 2026 at 7:00 AM) -> 6 AM to 9 AM
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.170938, p.DollarsPerKWH, 1e-6)

		// Winter Weekday On-Peak Evening (Monday January 12, 2026 at 6:00 PM) -> 5 PM to 8 PM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 18, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.170938, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Off-Peak (Monday January 12, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.055752, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Super Off-Peak (Monday January 12, 2026 at 2:00 AM) -> 12 AM to 5 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.049529, p.DollarsPerKWH, 1e-6)

		// Winter Weekend Off-Peak (Saturday January 17, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.055752, p.DollarsPerKWH, 1e-6)

		// Winter Holiday (Thanksgiving Day: Thursday November 26, 2026 at 6:00 PM) -> should be Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 18, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.055752, p.DollarsPerKWH, 1e-6)
	})
}
