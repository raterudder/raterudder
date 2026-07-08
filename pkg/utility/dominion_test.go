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
	t.Run("Schedule 1 Residential Service", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1",
		})
		require.NoError(t, err)

		// Summer (July 15, 2026 at 12:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.076602, p.DollarsPerKWH, 1e-6)

		// Non-Summer (November 15, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, etLocation))
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
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.210415, p.DollarsPerKWH, 1e-6)

		// Summer Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.051367, p.DollarsPerKWH, 1e-6)

		// Summer Weekday Super Off-Peak (Monday July 13, 2026 at 2:00 AM) -> 12 AM to 5 AM
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.033486, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.051367, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Super Off-Peak (Saturday July 18, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.033486, p.DollarsPerKWH, 1e-6)

		// Summer Holiday (Labor Day: Monday Sept 7, 2026 at 4:00 PM) -> should be Off-Peak (no peak on holidays)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 16, 0, 0, 0, etLocation))
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
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.170938, p.DollarsPerKWH, 1e-6)

		// Winter Weekday On-Peak Evening (Monday January 12, 2026 at 6:00 PM) -> 5 PM to 8 PM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 18, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.170938, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Off-Peak (Monday January 12, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.055752, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Super Off-Peak (Monday January 12, 2026 at 2:00 AM) -> 12 AM to 5 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.049529, p.DollarsPerKWH, 1e-6)

		// Winter Weekend Off-Peak (Saturday January 17, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.055752, p.DollarsPerKWH, 1e-6)

		// Winter Holiday (Thanksgiving Day: Thursday November 26, 2026 at 6:00 PM) -> should be Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 18, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.055752, p.DollarsPerKWH, 1e-6)
	})

	t.Run("NC Schedule 1 Residential Service", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1_nc",
		})
		require.NoError(t, err)

		// Summer (July 15, 2026 at 12:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.121533, p.DollarsPerKWH, 1e-6)

		// Non-Summer (November 15, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.105123, p.DollarsPerKWH, 1e-6)
	})

	t.Run("NC Schedule 1P TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1p_nc",
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (Monday July 13, 2026 at 4:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.077548, p.DollarsPerKWH, 1e-6)

		// Summer Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.053685, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 4:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.053685, p.DollarsPerKWH, 1e-6)

		// Winter Weekday On-Peak Morning (Monday January 12, 2026 at 7:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.077548, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Off-Peak Morning Before Peak (Monday January 12, 2026 at 6:15 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 6, 15, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.053685, p.DollarsPerKWH, 1e-6)

		// Winter Weekday On-Peak Morning After Peak Start (Monday January 12, 2026 at 6:45 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 6, 45, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.077548, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Off-Peak (Monday January 12, 2026 at 2:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.053685, p.DollarsPerKWH, 1e-6)

		// Winter Weekend Off-Peak (Saturday January 17, 2026 at 7:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.053685, p.DollarsPerKWH, 1e-6)

		// Winter Holiday (Good Friday: Friday April 3, 2026 at 7:00 AM) -> should be Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.April, 3, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.053685, p.DollarsPerKWH, 1e-6)
	})

	t.Run("NC Schedule 1T TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_1t_nc",
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak (Monday July 13, 2026 at 4:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.269467, p.DollarsPerKWH, 1e-6)

		// Summer Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.062731, p.DollarsPerKWH, 1e-6)

		// Winter Weekday On-Peak Morning (Monday January 12, 2026 at 7:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.223556, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Off-Peak Morning Before Peak (Monday January 12, 2026 at 6:15 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 6, 15, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.057137, p.DollarsPerKWH, 1e-6)

		// Winter Weekday On-Peak Morning After Peak Start (Monday January 12, 2026 at 6:45 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 6, 45, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.223556, p.DollarsPerKWH, 1e-6)

		// Winter Weekday Off-Peak (Monday January 12, 2026 at 2:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.057137, p.DollarsPerKWH, 1e-6)

		// Winter Holiday (Day after Thanksgiving: Friday November 27, 2026 at 7:00 AM) -> should be Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.November, 27, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.057137, p.DollarsPerKWH, 1e-6)
	})

	t.Run("SC Rate 5 TOU - Solar Choice Default", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_5_sc",
		})
		require.NoError(t, err)

		// Summer Peak (Monday July 13, 2026 at 5:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 17, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29907, p.DollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.29907, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Summer Super Off-Peak (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09623, p.DollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.09623, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Summer Off-Peak (Monday July 13, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15074, p.DollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.15074, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Summer Weekend Peak (Saturday July 18, 2026 at 5:00 PM) -> no weekend exemption
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 17, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29907, p.DollarsPerKWH, 1e-6)

		// Winter Peak (Monday January 12, 2026 at 7:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29907, p.DollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.29907, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Winter Super Off-Peak Noon block (Monday January 12, 2026 at 1:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 13, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09623, p.DollarsPerKWH, 1e-6)

		// Winter Off-Peak (Monday January 12, 2026 at 10:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15074, p.DollarsPerKWH, 1e-6)
	})

	t.Run("SC Rate 5 TOU - Net Metering", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_5_sc",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem",
			},
		})
		require.NoError(t, err)

		// Summer Peak (Monday July 13, 2026 at 5:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 17, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29907, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("SC Rate 8 Flat", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_8_sc",
		})
		require.NoError(t, err)

		// Summer (July 15, 2026 at 12:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15878, p.DollarsPerKWH, 1e-6)

		// Winter (January 15, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15878, p.DollarsPerKWH, 1e-6)
	})

	t.Run("SC Rate 6 Flat Saver", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dominion",
			UtilityRate:     "dominion_6_sc",
		})
		require.NoError(t, err)

		// Summer (July 15, 2026 at 12:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15333, p.DollarsPerKWH, 1e-6)

		// Winter (January 15, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15333, p.DollarsPerKWH, 1e-6)
	})
}
