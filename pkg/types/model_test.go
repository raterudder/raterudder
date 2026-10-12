package types

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestActionStorageCompaction(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	nowUTC := time.Date(2026, 6, 15, 17, 0, 0, 0, time.UTC)
	nowLocal := nowUTC.In(loc)

	t.Run("BidirectionalConversionWithoutPlan", func(t *testing.T) {
		t.Parallel()

		act := Action{
			Timestamp:       nowUTC,
			SystemTimestamp: nowLocal,
			BatteryMode:     BatteryModeStandby,
			SolarMode:       SolarModeAny,
			Reason:          ActionReasonSufficientBattery,
			Description:     "Holding battery in standby.",
			SystemStatus: SystemStatus{
				Timestamp:    nowLocal,
				TimeLocation: loc.String(),
				BatterySOC:   75.0,
			},
		}

		compacted := act.CompactForStorage()
		assert.Equal(t, "America/Chicago", compacted.Location)
		assert.Nil(t, compacted.Plan)

		data, err := json.Marshal(compacted)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"location":"America/Chicago"`)
		assert.NotContains(t, string(data), `"plan"`)

		var restored Action
		require.NoError(t, json.Unmarshal(data, &restored))
		restored.ExpandFromStorage()
		assert.Empty(t, restored.Location)
		assert.True(t, reflect.DeepEqual(act, restored))
		assert.Equal(t, act, restored)
	})

	t.Run("BidirectionalConversionWithPlan", func(t *testing.T) {
		t.Parallel()

		plan := &Plan{
			TSCreated:          nowUTC,
			HorizonHours:       24,
			TotalProjectedCost: 3.4512,
			TotalExportCredits: 1.2099,
			NetEconomicBenefit: 2.2413,
			Periods: []PlanPeriod{
				{
					TSStart:       nowLocal,
					TSEnd:         nowLocal.Add(20 * time.Minute),
					DurationHours: (20 * time.Minute).Hours(),
					ImportDollars: 0.1551,
					ExportDollars: 0.0460,
					BatteryMode:   BatteryModeLoad,
					SolarMode:     SolarModeAny,
					Reason:        ActionReasonDeficitSaveForPeak,
					StartSOC:      95.5,
					EndSOC:        90.0,
					ReserveSOC:    20.0,
					LoadKWH:       1.512,
					SolarKWH:      0.812,
					GridImportKWH: 0.212,
					GridExportKWH: 0.0,
					CostDollars:   0.0329,
				},
			},
		}

		act := Action{
			Timestamp:       nowUTC,
			SystemTimestamp: nowLocal,
			BatteryMode:     BatteryModeLoad,
			SolarMode:       SolarModeAny,
			Reason:          ActionReasonDeficitSaveForPeak,
			Description:     "Discharging to load.",
			HitDeficitAt:    nowLocal.Add(3 * time.Hour),
			HitCapacityAt:   nowLocal.Add(6 * time.Hour),
			CurrentPrice: &Price{
				Provider:             "comed",
				TSStart:              nowLocal,
				TSEnd:                nowLocal.Add(time.Hour),
				DollarsPerKWH:        0.055,
				GridUseDollarsPerKWH: 0.10,
			},
			FuturePrice: &Price{
				Provider:             "comed",
				TSStart:              nowLocal.Add(4 * time.Hour),
				TSEnd:                nowLocal.Add(5 * time.Hour),
				DollarsPerKWH:        0.185,
				GridUseDollarsPerKWH: 0.10,
			},
			SystemStatus: SystemStatus{
				Timestamp:    nowLocal,
				TimeLocation: loc.String(),
				BatterySOC:   95.5,
				Alarms: []SystemAlarm{
					{Name: "TestAlarm", Description: "Desc", Timestamp: nowLocal, Code: "A1"},
				},
				Storms: []Storm{
					{Description: "Storm", TSStart: nowLocal, TSEnd: nowLocal.Add(2 * time.Hour)},
				},
				VPPEvents: []VPPEvent{
					{Description: "VPP", TSStart: nowLocal, TSEnd: nowLocal.Add(time.Hour), VPPSoc: 20},
				},
			},
			Plan: plan,
		}

		compacted := act.CompactForStorage()
		require.NotNil(t, compacted.Plan)
		assert.Nil(t, compacted.Plan.Periods)
		assert.Len(t, compacted.Plan.ShortPeriods, 1)
		assert.Equal(t, "America/Chicago", compacted.Location)
		// Ensure original action's Plan.Periods was not mutated
		assert.Len(t, act.Plan.Periods, 1)
		assert.Nil(t, act.Plan.ShortPeriods)

		data, err := json.Marshal(compacted)
		require.NoError(t, err)
		jsonStr := string(data)

		assert.Contains(t, jsonStr, `"shortPeriods"`)
		assert.NotContains(t, jsonStr, `"periods"`)
		assert.Contains(t, jsonStr, `"us":`)
		assert.Contains(t, jsonStr, `"ue":`)
		assert.Contains(t, jsonStr, `"bm":-1`)
		assert.Contains(t, jsonStr, `"sm":2`)
		assert.Contains(t, jsonStr, `"r":"deficitSaveForPeak"`)

		var restored Action
		require.NoError(t, json.Unmarshal(data, &restored))
		restored.ExpandFromStorage()

		assert.Empty(t, restored.Location)
		if assert.NotNil(t, restored.Plan) {
			assert.Nil(t, restored.Plan.ShortPeriods)
			assert.Len(t, restored.Plan.Periods, 1)
		}
		assert.True(t, reflect.DeepEqual(act, restored))
		assert.Equal(t, act, restored)
	})

	t.Run("TwelveHourRollupSizeUnder720KB", func(t *testing.T) {
		t.Parallel()

		const actionsPer12Hours = 36 // 12 hours at 20-minute intervals
		const periodsPerPlan = 73    // 24 hours of 20-minute intervals + partial step 0

		actions := make([]Action, 0, actionsPer12Hours)
		for aIdx := 0; aIdx < actionsPer12Hours; aIdx++ {
			actTimeUTC := nowUTC.Add(time.Duration(aIdx) * 20 * time.Minute)
			actTimeLocal := actTimeUTC.In(loc)

			periods := make([]PlanPeriod, periodsPerPlan)
			for pIdx := 0; pIdx < periodsPerPlan; pIdx++ {
				pStart := actTimeLocal.Add(time.Duration(pIdx) * 20 * time.Minute)
				pEnd := pStart.Add(20 * time.Minute)
				periods[pIdx] = PlanPeriod{
					TSStart:       pStart,
					TSEnd:         pEnd,
					DurationHours: pEnd.Sub(pStart).Hours(),
					ImportDollars: 0.10355878099999999 + float64(pIdx)*0.0001234567,
					ExportDollars: 0.03689123456789012 + float64(pIdx)*0.0000987654,
					BatteryMode:   BatteryModeLoad,
					SolarMode:     SolarModeAny,
					Reason:        ActionReasonSufficientBatteryTillCharge,
					StartSOC:      84.56789123456789,
					EndSOC:        83.12345678901234,
					ReserveSOC:    20.0,
					LoadKWH:       0.6055082377695578,
					SolarKWH:      0.2456789012345678,
					GridImportKWH: 0.3598293365349900,
					GridExportKWH: 0.0123456789012345,
					CostDollars:   0.06006983134493879,
				}
			}

			act := Action{
				Timestamp:               actTimeUTC,
				SystemTimestamp:         actTimeLocal,
				BatteryMode:             BatteryModeLoad,
				SolarMode:               SolarModeAny,
				ChargeToSOC:             100,
				Reason:                  ActionReasonSufficientBatteryTillCharge,
				Description:             "Discharging battery to cover household load until scheduled charge window.",
				HitDeficitAt:            actTimeLocal.Add(5 * time.Hour),
				HitCapacityAt:           actTimeLocal.Add(14 * time.Hour),
				StrategyBenefitDollars:  4.567890123456789,
				RecentHomeUsageKWH:      2.345678901234567,
				Q3HomeUsageKWH:          1.890123456789012,
				RecentHomeUsageAbnormal: true,
				CurrentPrice: &Price{
					Provider:             "comed",
					TSStart:              actTimeLocal.Truncate(time.Hour),
					TSEnd:                actTimeLocal.Truncate(time.Hour).Add(time.Hour),
					DollarsPerKWH:        0.0456789,
					GridUseDollarsPerKWH: 0.057879881,
					PeriodName:           "Off-Peak",
				},
				FuturePrice: &Price{
					Provider:             "comed",
					TSStart:              actTimeLocal.Truncate(time.Hour).Add(6 * time.Hour),
					TSEnd:                actTimeLocal.Truncate(time.Hour).Add(7 * time.Hour),
					DollarsPerKWH:        0.1456789,
					GridUseDollarsPerKWH: 0.057879881,
					PeriodName:           "Peak",
				},
				SystemStatus: SystemStatus{
					Timestamp:             actTimeLocal,
					TimeLocation:          loc.String(),
					BatterySOC:            84.56789123456789,
					BatteryKW:             1.854321,
					BatteryCapacityKWH:    27.2,
					MaxBatteryChargeKW:    10.0,
					MaxBatteryDischargeKW: 10.0,
					SolarKW:               0.735123,
					GridKW:                0.021345,
					HomeKW:                2.610789,
					ElevatedMinBatterySOC: false,
					BatteryAboveMinSOC:    true,
					ManagedTOUMode:        true,
				},
				SimulationParams: SimulationParams{
					ClippingCapKWH:         8.456789012345678,
					PanelAzimuth:           180.0,
					PanelTilt:              30.0,
					AverageSolarEfficiency: 0.8234567890123456,
					DetectedShift:          "none",
				},
				Plan: &Plan{
					TSCreated:          actTimeUTC,
					HorizonHours:       24,
					TotalProjectedCost: 5.43210987654321,
					TotalExportCredits: 1.23456789012345,
					NetEconomicBenefit: -4.19754198641976,
					Periods:            periods,
				},
			}
			actions = append(actions, act.CompactForStorage())
		}

		rollup := SiteActionBucket{
			TSBucketStart: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			Actions:       actions,
		}

		data, err := json.Marshal(rollup)
		require.NoError(t, err)

		const maxRollupBytes = 720 * 1024 // 720 KB (< 75% of Firestore's 1 MiB limit)
		assert.Less(t, len(data), maxRollupBytes, "12-hour SiteActionRollup JSON size (%d bytes) must stay under 720 KB (%d bytes)", len(data), maxRollupBytes)
	})
}
