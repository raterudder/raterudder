package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFPLHolidays(t *testing.T) {
	t.Run("Independence Day observed shifts", func(t *testing.T) {
		// 2026 July 4th is Saturday -> observed Friday July 3rd
		holidays2026 := getFPLHolidays(2026)
		if assert.Contains(t, holidays2026, "2026-07-03") {
			assert.NotContains(t, holidays2026, "2026-07-04")
		}

		// 2027 July 4th is Sunday -> observed Monday July 5th
		holidays2027 := getFPLHolidays(2027)
		if assert.Contains(t, holidays2027, "2027-07-05") {
			assert.NotContains(t, holidays2027, "2027-07-04")
		}

		// 2025 July 4th is Friday -> remains Friday July 4th
		holidays2025 := getFPLHolidays(2025)
		assert.Contains(t, holidays2025, "2025-07-04")
	})

	t.Run("Labor Day first Monday in September", func(t *testing.T) {
		// 2026 Labor Day is Sep 7
		holidays2026 := getFPLHolidays(2026)
		assert.Contains(t, holidays2026, "2026-09-07")

		// 2027 Labor Day is Sep 6
		holidays2027 := getFPLHolidays(2027)
		assert.Contains(t, holidays2027, "2027-09-06")
	})
}

func TestFPLRates(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	u := &genericTOU{}

	t.Run("RS-1 Flat Rate", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "fpl",
			UtilityRate:     "fpl_rs1",
		})
		require.NoError(t, err)

		// Flat rate should be $0.12298 for any time
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12298, p.DollarsPerKWH, 1e-6)

		p2, err := u.priceForTime(time.Date(2026, time.December, 15, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12298, p2.DollarsPerKWH, 1e-6)
	})

	t.Run("RTR-1 TOU Rate", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "fpl",
			UtilityRate:     "fpl_rtr1",
		})
		require.NoError(t, err)

		// --- Summer (April 1 - October 31) ---
		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM) -> 26.934¢
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.26934, p.DollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM) -> 6.043¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.06043, p.DollarsPerKWH, 1e-6)

		// 3. Summer weekend Off-Peak (Saturday, July 18, 2026 at 3:00 PM) -> 6.043¢
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.06043, p.DollarsPerKWH, 1e-6)

		// 4. Summer Holiday Off-Peak (Labor Day Monday, Sep 7, 2026 at 3:00 PM) -> 6.043¢
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.06043, p.DollarsPerKWH, 1e-6)

		// --- Winter (November 1 - March 31) ---
		// 5. Winter weekday Morning On-Peak (Tuesday, Dec 15, 2026 at 8:00 AM) -> 26.934¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 8, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.26934, p.DollarsPerKWH, 1e-6)

		// 6. Winter weekday Evening On-Peak (Tuesday, Dec 15, 2026 at 8:00 PM / 20:00) -> 26.934¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 20, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.26934, p.DollarsPerKWH, 1e-6)

		// 7. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 12:00 PM) -> 6.043¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.06043, p.DollarsPerKWH, 1e-6)

		// 8. Winter weekend Off-Peak (Saturday, Dec 19, 2026 at 8:00 AM) -> 6.043¢
		p, err = u.priceForTime(time.Date(2026, time.December, 19, 8, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.06043, p.DollarsPerKWH, 1e-6)

		// 9. Winter Holiday Off-Peak (Christmas Day Friday, Dec 25, 2026 at 8:00 AM) -> 6.043¢
		p, err = u.priceForTime(time.Date(2026, time.December, 25, 8, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.06043, p.DollarsPerKWH, 1e-6)
	})
}

func TestFPLMetadata(t *testing.T) {
	info := fplUtilityInfo()
	assert.Equal(t, "fpl", info.ID)
	assert.Equal(t, "Florida Power & Light", info.Name)
	if assert.Len(t, info.Rates, 2) {
		assert.Equal(t, "fpl_rs1", info.Rates[0].ID)
		if assert.Len(t, info.Rates[0].Options, 1) {
			assert.Equal(t, "netMeteringCredits", info.Rates[0].Options[0].Field)
			assert.Equal(t, true, info.Rates[0].Options[0].Default)
			assert.True(t, info.Rates[0].Options[0].Hidden)
		}
		assert.Equal(t, "fpl_rtr1", info.Rates[1].ID)
		if assert.Len(t, info.Rates[1].Options, 1) {
			assert.Equal(t, "netMeteringCredits", info.Rates[1].Options[0].Field)
			assert.Equal(t, true, info.Rates[1].Options[0].Default)
			assert.True(t, info.Rates[1].Options[0].Hidden)
		}
	}
}
