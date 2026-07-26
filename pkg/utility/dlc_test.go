package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDLCHolidays(t *testing.T) {
	t.Run("PJM Holiday shifts", func(t *testing.T) {
		// 2026 July 4th is Saturday -> observed Friday July 3rd
		holidays2026 := getDLCHolidays(2026)
		if assert.Contains(t, holidays2026, "2026-07-03") {
			assert.NotContains(t, holidays2026, "2026-07-04")
		}

		// 2027 July 4th is Sunday -> observed Monday July 5th
		holidays2027 := getDLCHolidays(2027)
		if assert.Contains(t, holidays2027, "2027-07-05") {
			assert.NotContains(t, holidays2027, "2027-07-04")
		}

		// 2025 July 4th is Friday -> remains Friday July 4th
		holidays2025 := getDLCHolidays(2025)
		assert.Contains(t, holidays2025, "2025-07-04")
	})

	t.Run("Memorial Day and Labor Day", func(t *testing.T) {
		holidays2026 := getDLCHolidays(2026)
		assert.Contains(t, holidays2026, "2026-05-25") // Memorial Day 2026
		assert.Contains(t, holidays2026, "2026-09-07") // Labor Day 2026
	})
}

func TestDLCRates(t *testing.T) {
	u := &genericTOU{}

	t.Run("RS Flat Rate", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dlc",
			UtilityRate:     "dlc_rs",
		})
		require.NoError(t, err)

		// Flat rate should be $0.241769 for any time
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.241769, p.DollarsPerKWH, 1e-6)

		p2, err := u.priceForTime(time.Date(2026, time.December, 15, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.241769, p2.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("TOU Supply Rate Pilot", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "dlc",
			UtilityRate:     "dlc_tou",
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

		// 1. Weekday On-Peak (Wednesday, July 15, 2026 at 4:00 PM) -> 44.7740¢
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.447740, p.DollarsPerKWH, 1e-6)

		// 2. Weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM) -> 19.0276¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.190276, p.DollarsPerKWH, 1e-6)

		// 3. Weekday Super Off-Peak (Wednesday, July 15, 2026 at 2:00 AM) -> 17.4938¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.174938, p.DollarsPerKWH, 1e-6)

		// 4. Weekend Super Off-Peak (Saturday, July 18, 2026 at 2:00 AM) -> 17.4938¢
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.174938, p.DollarsPerKWH, 1e-6)

		// 5. Weekend Off-Peak (Saturday, July 18, 2026 at 12:00 PM) -> 19.0276¢
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.190276, p.DollarsPerKWH, 1e-6)

		// 6. Holiday Off-Peak (Labor Day Monday, Sep 7, 2026 at 12:00 PM) -> 19.0276¢
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.190276, p.DollarsPerKWH, 1e-6)

		// 7. Holiday Super Off-Peak (Labor Day Monday, Sep 7, 2026 at 2:00 AM) -> 17.4938¢
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.174938, p.DollarsPerKWH, 1e-6)
	})
}

func TestDLCMetadata(t *testing.T) {
	info := dlcUtilityInfo()
	assert.Equal(t, "dlc", info.ID)
	assert.Equal(t, "Duquesne Light Co. (DLC)", info.Name)
	if assert.Len(t, info.Rates, 2) {
		assert.Equal(t, "dlc_rs", info.Rates[0].ID)
		if assert.Len(t, info.Rates[0].Options, 1) {
			assert.Equal(t, "netMeteringCredits", info.Rates[0].Options[0].Field)
			assert.Equal(t, true, info.Rates[0].Options[0].Default)
			assert.True(t, info.Rates[0].Options[0].Hidden)
		}
		assert.Equal(t, "dlc_tou", info.Rates[1].ID)
		if assert.Len(t, info.Rates[1].Options, 1) {
			assert.Equal(t, "netMeteringCredits", info.Rates[1].Options[0].Field)
			assert.Equal(t, true, info.Rates[1].Options[0].Default)
			assert.True(t, info.Rates[1].Options[0].Hidden)
		}
	}
}
