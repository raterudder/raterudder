package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortlandGeneralElectric(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)

	u := &genericTOU{}
	err = u.ApplySettings(context.Background(), types.Settings{
		UtilityProvider: "portland_general_electric",
		UtilityRate:     "portland_general_electric_tod",
	})
	require.NoError(t, err)

	t.Run("Weekday On-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 18, 0, 0, 0, la)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.Equal(t, 0.4365, p.DollarsPerKWH)
	})

	t.Run("Weekday Mid-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 10, 0, 0, 0, la)) // Monday 10:00 AM
		require.NoError(t, err)
		assert.Equal(t, 0.1689, p.DollarsPerKWH)
	})

	t.Run("Weekday Off-Peak evening", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 22, 0, 0, 0, la)) // Monday 10:00 PM
		require.NoError(t, err)
		assert.Equal(t, 0.0901, p.DollarsPerKWH)
	})

	t.Run("Weekday Off-Peak morning", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 4, 0, 0, 0, la)) // Monday 4:00 AM
		require.NoError(t, err)
		assert.Equal(t, 0.0901, p.DollarsPerKWH)
	})

	t.Run("Weekend Off-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 23, 14, 0, 0, 0, la)) // Saturday 2:00 PM
		require.NoError(t, err)
		assert.Equal(t, 0.0901, p.DollarsPerKWH)
	})

	t.Run("Holiday Off-Peak (Memorial Day)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 25, 18, 0, 0, 0, la)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.Equal(t, 0.0901, p.DollarsPerKWH)
	})

	t.Run("Holiday Weekend Shift (Saturday Independence Day shifts to Friday)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.July, 3, 18, 0, 0, 0, la)) // Friday 6:00 PM
		require.NoError(t, err)
		assert.Equal(t, 0.0901, p.DollarsPerKWH)
	})

	t.Run("Regular Friday (not holiday)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.July, 10, 18, 0, 0, 0, la)) // Friday 6:00 PM
		require.NoError(t, err)
		assert.Equal(t, 0.4365, p.DollarsPerKWH)
	})
}

func TestShiftPGEWeekendHoliday(t *testing.T) {
	t.Run("Saturday shifts to Friday", func(t *testing.T) {
		sat := time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC) // July 4, 2026 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 3, shifted.Day())
			assert.Equal(t, time.July, shifted.Month())
			assert.Equal(t, 2026, shifted.Year())
		}
	})

	t.Run("Sunday shifts to Monday", func(t *testing.T) {
		sun := time.Date(2027, time.July, 4, 0, 0, 0, 0, time.UTC) // July 4, 2027 is Sunday
		shifted := shiftPGEWeekendHoliday(sun)
		if assert.Equal(t, time.Monday, shifted.Weekday()) {
			assert.Equal(t, 5, shifted.Day())
			assert.Equal(t, time.July, shifted.Month())
			assert.Equal(t, 2027, shifted.Year())
		}
	})

	t.Run("Weekdays do not shift", func(t *testing.T) {
		weekdays := []struct {
			day  time.Time
			name string
		}{
			{time.Date(2026, time.July, 6, 0, 0, 0, 0, time.UTC), "Monday"},
			{time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC), "Tuesday"},
			{time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC), "Wednesday"},
			{time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC), "Thursday"},
			{time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC), "Friday"},
		}

		for _, wd := range weekdays {
			t.Run(wd.name, func(t *testing.T) {
				shifted := shiftPGEWeekendHoliday(wd.day)
				if assert.Equal(t, wd.day.Weekday(), shifted.Weekday()) {
					assert.Equal(t, wd.day.Day(), shifted.Day())
				}
			})
		}
	})

	t.Run("Saturday Jan 1 crossing to preceding Friday Dec 31", func(t *testing.T) {
		sat := time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC) // Jan 1, 2022 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 31, shifted.Day())
			assert.Equal(t, time.December, shifted.Month())
			assert.Equal(t, 2021, shifted.Year())
		}
	})

	t.Run("Sunday Jan 1 crossing to Monday Jan 2", func(t *testing.T) {
		sun := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC) // Jan 1, 2023 is Sunday
		shifted := shiftPGEWeekendHoliday(sun)
		if assert.Equal(t, time.Monday, shifted.Weekday()) {
			assert.Equal(t, 2, shifted.Day())
			assert.Equal(t, time.January, shifted.Month())
			assert.Equal(t, 2023, shifted.Year())
		}
	})

	t.Run("Saturday May 1 crossing to preceding Friday Apr 30", func(t *testing.T) {
		sat := time.Date(2021, time.May, 1, 0, 0, 0, 0, time.UTC) // May 1, 2021 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 30, shifted.Day())
			assert.Equal(t, time.April, shifted.Month())
			assert.Equal(t, 2021, shifted.Year())
		}
	})

	t.Run("Saturday Feb 29 on leap year shifts to Friday Feb 28", func(t *testing.T) {
		sat := time.Date(2020, time.February, 29, 0, 0, 0, 0, time.UTC) // Feb 29, 2020 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 28, shifted.Day())
			assert.Equal(t, time.February, shifted.Month())
			assert.Equal(t, 2020, shifted.Year())
		}
	})
}
