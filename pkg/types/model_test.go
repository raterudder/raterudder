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

func TestStoredActionConversion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("BidirectionalConversionWithoutPlan", func(t *testing.T) {
		t.Parallel()

		act := Action{
			Timestamp:   now,
			BatteryMode: BatteryModeStandby,
			SolarMode:   SolarModeAny,
			Reason:      ActionReasonSufficientBattery,
			Description: "Holding battery in standby.",
		}

		stored := act.ToStored()
		assert.Nil(t, stored.Plan)
		assert.Nil(t, stored.Action.Plan)

		data, err := json.Marshal(stored)
		assert.NoError(t, err)
		assert.NotContains(t, string(data), `"shortPlan"`)
		assert.NotContains(t, string(data), `"plan"`)

		var roundTrip StoredAction
		assert.NoError(t, json.Unmarshal(data, &roundTrip))
		restored := roundTrip.ToAction()
		assert.Nil(t, restored.Plan)
		assert.Equal(t, act.BatteryMode, restored.BatteryMode)
		assert.Equal(t, act.Reason, restored.Reason)
	})

	t.Run("BidirectionalConversionWithPlan", func(t *testing.T) {
		t.Parallel()

		plan := &Plan{
			TSCreated:          now,
			HorizonHours:       24,
			TotalProjectedCost: 3.45,
			TotalExportCredits: 1.20,
			NetEconomicBenefit: 2.25,
			Periods: []PlanPeriod{
				{
					TSStart:       now,
					TSEnd:         now.Add(time.Hour),
					DurationHours: 1.0,
					ImportDollars: 0.155,
					ExportDollars: 0.045,
					BatteryMode:   BatteryModeLoad,
					SolarMode:     SolarModeAny,
					Reason:        ActionReasonDeficitSaveForPeak,
					StartSOC:      95.5,
					EndSOC:        90.0,
					ReserveSOC:    20.0,
					LoadKWH:       1.5,
					SolarKWH:      0.8,
					GridImportKWH: 0.2,
					GridExportKWH: 0.0,
					CostDollars:   0.03,
				},
			},
		}

		act := Action{
			Timestamp:   now,
			BatteryMode: BatteryModeLoad,
			SolarMode:   SolarModeAny,
			Reason:      ActionReasonDeficitSaveForPeak,
			Description: "Discharging to load.",
			Plan:        plan,
		}

		stored := act.ToStored()
		assert.NotNil(t, stored.Plan)
		assert.Nil(t, stored.Action.Plan) // embedded Plan cleared so it won't serialize under "plan"

		data, err := json.Marshal(stored)
		assert.NoError(t, err)
		jsonStr := string(data)

		// Must contain shortPlan and compact tags
		assert.Contains(t, jsonStr, `"shortPlan"`)
		assert.NotContains(t, jsonStr, `"plan":`)
		assert.Contains(t, jsonStr, `"bm":-1`)
		assert.Contains(t, jsonStr, `"sm":2`)
		assert.Contains(t, jsonStr, `"r":"deficitSaveForPeak"`)
		assert.Contains(t, jsonStr, `"ss":95.5`)
		assert.Contains(t, jsonStr, `"es":90`)
		assert.Contains(t, jsonStr, `"rs":20`)
		assert.Contains(t, jsonStr, `"i":0.155`)
		assert.Contains(t, jsonStr, `"e":0.045`)
		assert.Contains(t, jsonStr, `"tc":3.45`)
		assert.Contains(t, jsonStr, `"te":1.2`)
		assert.Contains(t, jsonStr, `"nb":2.25`)

		// Unmarshal back to StoredAction
		var roundTrip StoredAction
		assert.NoError(t, json.Unmarshal(data, &roundTrip))
		assert.NotNil(t, roundTrip.Plan)

		restored := roundTrip.ToAction()
		if assert.NotNil(t, restored.Plan) {
			assert.Equal(t, act.Plan.TSCreated, restored.Plan.TSCreated)
			assert.Equal(t, act.Plan.HorizonHours, restored.Plan.HorizonHours)
			assert.InDelta(t, act.Plan.TotalProjectedCost, restored.Plan.TotalProjectedCost, 0.0001)
			assert.InDelta(t, act.Plan.TotalExportCredits, restored.Plan.TotalExportCredits, 0.0001)
			assert.InDelta(t, act.Plan.NetEconomicBenefit, restored.Plan.NetEconomicBenefit, 0.0001)
			if assert.Len(t, restored.Plan.Periods, 1) {
				pOrig := act.Plan.Periods[0]
				pRest := restored.Plan.Periods[0]
				assert.Equal(t, pOrig.TSStart, pRest.TSStart)
				assert.Equal(t, pOrig.TSEnd, pRest.TSEnd)
				assert.InDelta(t, pOrig.DurationHours, pRest.DurationHours, 0.001)
				assert.InDelta(t, pOrig.ImportDollars, pRest.ImportDollars, 0.0001)
				assert.InDelta(t, pOrig.ExportDollars, pRest.ExportDollars, 0.0001)
				assert.Equal(t, pOrig.BatteryMode, pRest.BatteryMode)
				assert.Equal(t, pOrig.SolarMode, pRest.SolarMode)
				assert.Equal(t, pOrig.Reason, pRest.Reason)
				assert.InDelta(t, pOrig.StartSOC, pRest.StartSOC, 0.01)
				assert.InDelta(t, pOrig.EndSOC, pRest.EndSOC, 0.01)
				assert.InDelta(t, pOrig.ReserveSOC, pRest.ReserveSOC, 0.01)
				assert.InDelta(t, pOrig.LoadKWH, pRest.LoadKWH, 0.01)
				assert.InDelta(t, pOrig.SolarKWH, pRest.SolarKWH, 0.01)
				assert.InDelta(t, pOrig.GridImportKWH, pRest.GridImportKWH, 0.01)
				assert.InDelta(t, pOrig.GridExportKWH, pRest.GridExportKWH, 0.01)
				assert.InDelta(t, pOrig.CostDollars, pRest.CostDollars, 0.01)
			}
		}
	})
}
