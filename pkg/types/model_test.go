package types

import (
	"encoding/json"
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

func TestActionJSONSerialization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("OmitsPlanWhenNil", func(t *testing.T) {
		t.Parallel()

		act := Action{
			Timestamp:   now,
			BatteryMode: BatteryModeChargeAny,
			SolarMode:   SolarModeAny,
			Reason:      ActionReasonDeficitChargeNow,
			Description: "Charging",
		}

		data, err := json.Marshal(act)
		assert.NoError(t, err)
		assert.NotContains(t, string(data), `"plan"`)
	})

	t.Run("IncludesPlanWhenPresent", func(t *testing.T) {
		t.Parallel()

		plan := &Plan{
			TSCreated:          now,
			HorizonHours:       24,
			TotalProjectedCost: 1.25,
			Periods: []PlanPeriod{
				{
					TSStart:       now,
					TSEnd:         now.Add(time.Hour),
					DurationHours: 1.0,
					BatteryMode:   BatteryModeChargeAny,
					SolarMode:     SolarModeAny,
					CostDollars:   1.25,
				},
			},
		}

		act := Action{
			Timestamp:   now,
			BatteryMode: BatteryModeChargeAny,
			SolarMode:   SolarModeAny,
			Reason:      ActionReasonDeficitChargeNow,
			Description: "Charging",
			Plan:        plan,
		}

		data, err := json.Marshal(act)
		assert.NoError(t, err)
		assert.Contains(t, string(data), `"plan"`)
		assert.Contains(t, string(data), `"totalProjectedCost":1.25`)

		var roundTrip Action
		err = json.Unmarshal(data, &roundTrip)
		assert.NoError(t, err)
		assert.NotNil(t, roundTrip.Plan)
		assert.Equal(t, 24, roundTrip.Plan.HorizonHours)
		assert.InDelta(t, 1.25, roundTrip.Plan.TotalProjectedCost, 0.001)
		assert.Len(t, roundTrip.Plan.Periods, 1)
	})
}
