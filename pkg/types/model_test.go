package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUserNotificationSettings_IsInQuietPeriod(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if !assert.NoError(t, err) {
		return
	}

	t.Run("EmptyQuietPeriodsReturnsFalse", func(t *testing.T) {
		s := UserNotificationSettings{
			QuietPeriods: nil,
		}
		testTime := time.Date(2026, 9, 14, 2, 30, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(testTime))

		s.QuietPeriods = []TimePeriod{}
		assert.False(t, s.IsInQuietPeriod(testTime))
	})

	t.Run("SameDayPeriod", func(t *testing.T) {
		s := UserNotificationSettings{
			QuietPeriods: []TimePeriod{
				{
					Name:  "WorkFocus",
					Hours: []UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
				},
			},
		}

		before := time.Date(2026, 9, 14, 8, 59, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(before))

		atStart := time.Date(2026, 9, 14, 9, 0, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(atStart))

		midday := time.Date(2026, 9, 14, 13, 15, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(midday))

		beforeEnd := time.Date(2026, 9, 14, 16, 59, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(beforeEnd))

		atEnd := time.Date(2026, 9, 14, 17, 0, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(atEnd))

		evening := time.Date(2026, 9, 14, 21, 0, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(evening))
	})

	t.Run("OvernightMidnightSpanningPeriod", func(t *testing.T) {
		s := UserNotificationSettings{
			QuietPeriods: []TimePeriod{
				{
					Name:  "Overnight",
					Hours: []UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
				},
			},
		}

		eveningBefore := time.Date(2026, 9, 14, 21, 59, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(eveningBefore))

		atStart := time.Date(2026, 9, 14, 22, 0, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(atStart))

		lateNight := time.Date(2026, 9, 14, 23, 45, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(lateNight))

		midnight := time.Date(2026, 9, 15, 0, 0, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(midnight))

		oneAM := time.Date(2026, 9, 15, 1, 0, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(oneAM))

		beforeWakeup := time.Date(2026, 9, 15, 6, 59, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(beforeWakeup))

		atWakeup := time.Date(2026, 9, 15, 7, 0, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(atWakeup))

		morning := time.Date(2026, 9, 15, 8, 30, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(morning))
	})

	t.Run("MultiplePeriods", func(t *testing.T) {
		s := UserNotificationSettings{
			QuietPeriods: []TimePeriod{
				{
					Name:  "Nap",
					Hours: []UtilityHourPeriod{{HourStart: 13, HourEnd: 15}},
				},
				{
					Name:  "Sleep",
					Hours: []UtilityHourPeriod{{HourStart: 22, HourEnd: 7}},
				},
			},
		}

		morning := time.Date(2026, 9, 14, 10, 0, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(morning))

		duringNap := time.Date(2026, 9, 14, 14, 0, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(duringNap))

		afterNap := time.Date(2026, 9, 14, 16, 0, 0, 0, loc)
		assert.False(t, s.IsInQuietPeriod(afterNap))

		duringSleep := time.Date(2026, 9, 14, 23, 0, 0, 0, loc)
		assert.True(t, s.IsInQuietPeriod(duringSleep))
	})
}
