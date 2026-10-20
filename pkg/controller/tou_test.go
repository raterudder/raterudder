package controller

import (
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTOUSchedule(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	baseTime := time.Date(2026, 10, 5, 0, 0, 0, 0, loc) // Midnight Monday

	t.Run("Nil Or Empty Plan Returns Nil", func(t *testing.T) {
		assert.Nil(t, BuildTOUSchedule(nil, baseTime, loc))
		assert.Nil(t, BuildTOUSchedule(&types.Plan{Periods: nil}, baseTime, loc))
		assert.Nil(t, BuildTOUSchedule(&types.Plan{Periods: []types.PlanPeriod{}}, baseTime, loc))
	})

	t.Run("Standard Self Consumption Two Tier TOU Coalesces Seamlessly", func(t *testing.T) {
		// Scenario: Standard 3-tier utility tariff in self-consumption (no export overrides):
		// - 00:00 to 16:00 (16 hours): Off-Peak ($0.15 import, $0.08 export)
		// - 16:00 to 21:00 (5 hours):  On-Peak  ($0.45 import, $0.35 export)
		// - 21:00 to 24:00 (3 hours):  Off-Peak ($0.15 import, $0.08 export)
		// Built out of 24 individual 1-hour periods.
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			start := baseTime.Add(time.Duration(h) * time.Hour)
			end := start.Add(time.Hour)
			imp := 0.15
			exp := 0.08
			if h >= 16 && h < 21 {
				imp = 0.45
				exp = 0.35
			}
			periods = append(periods, types.PlanPeriod{
				TSStart:       start,
				TSEnd:         end,
				DurationHours: 1.0,
				ImportDollars: imp,
				ExportDollars: exp,
				BatteryMode:   types.BatteryModeLoad,
				SolarMode:     types.SolarModeAny,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, baseTime, loc)
		require.NotNil(t, sched)

		// Coalescing should have combined the 24 hourly periods into exactly 3 distinct blocks:
		// 1. 00:00 - 16:00 (Off-Peak)
		// 2. 16:00 - 21:00 (On-Peak)
		// 3. 21:00 - 24:00 (Off-Peak)
		require.Len(t, sched.Periods, 3)

		// Period 1: Off-Peak morning/afternoon
		p1 := sched.Periods[0]
		assert.Equal(t, 0, p1.StartHour)
		assert.Equal(t, 0, p1.StartMinute)
		assert.Equal(t, 16, p1.EndHour)
		assert.Equal(t, 0, p1.EndMinute)
		assert.Equal(t, "00:00", p1.StartTimeStr())
		assert.Equal(t, "16:00", p1.EndTimeStr())
		assert.InDelta(t, 0.15, p1.ImportDollars, 0.0001)
		assert.InDelta(t, 0.08, p1.ExportDollars, 0.0001)
		assert.Equal(t, types.BatteryModeLoad, p1.BatteryMode)
		assert.Equal(t, types.SolarModeAny, p1.SolarMode)
		assert.False(t, p1.Peak)

		// Period 2: Peak evening
		p2 := sched.Periods[1]
		assert.Equal(t, 16, p2.StartHour)
		assert.Equal(t, 0, p2.StartMinute)
		assert.Equal(t, 21, p2.EndHour)
		assert.Equal(t, 0, p2.EndMinute)
		assert.Equal(t, "16:00", p2.StartTimeStr())
		assert.Equal(t, "21:00", p2.EndTimeStr())
		assert.InDelta(t, 0.45, p2.ImportDollars, 0.0001)
		assert.InDelta(t, 0.35, p2.ExportDollars, 0.0001)
		assert.Equal(t, types.BatteryModeLoad, p2.BatteryMode)
		assert.Equal(t, types.SolarModeAny, p2.SolarMode)
		assert.True(t, p2.Peak) // Detected as utility peak due to rate spread

		// Period 3: Off-Peak late night
		p3 := sched.Periods[2]
		assert.Equal(t, 21, p3.StartHour)
		assert.Equal(t, 0, p3.StartMinute)
		assert.Equal(t, 24, p3.EndHour)
		assert.Equal(t, 0, p3.EndMinute)
		assert.Equal(t, "21:00", p3.StartTimeStr())
		assert.Equal(t, "24:00", p3.EndTimeStr())
		assert.InDelta(t, 0.15, p3.ImportDollars, 0.0001)
		assert.InDelta(t, 0.08, p3.ExportDollars, 0.0001)
		assert.Equal(t, types.BatteryModeLoad, p3.BatteryMode)
		assert.Equal(t, types.SolarModeAny, p3.SolarMode)
		assert.False(t, p3.Peak)
	})

	t.Run("Active Immediate Solar Export And Future Battery Export Windows", func(t *testing.T) {
		// Scenario:
		// Plan starts at 14:00 on Monday.
		// - 14:00 to 16:00 (2 hours): Active immediate Solar Export (SolarModeExport).
		// - 16:00 to 19:00 (3 hours): Normal self-consumption (SolarModeAny, BatteryModeLoad).
		// - 19:00 to 21:00 (2 hours): Future planned export in the solver (BatteryModeExport).
		// - 21:00 to 14:00 Tuesday:  Self-consumption.
		//
		// Both the immediate 14:00 to 16:00 Solar Export and the future 19:00 to 21:00 Battery Export
		// must be preserved in the TOU schedule as peak export dispatches.
		startTime := baseTime.Add(14 * time.Hour) // 14:00 Monday
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			currStart := startTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			clockHour := currStart.Hour()

			bMode := types.BatteryModeLoad
			sMode := types.SolarModeAny

			if h < 2 { // 14:00 - 16:00 (Immediate active block)
				sMode = types.SolarModeExport
			} else if clockHour >= 19 && clockHour < 21 { // 19:00 - 21:00 (Future planned export)
				bMode = types.BatteryModeExport
			}

			imp := 0.20 + float64(clockHour)*0.01
			exp := 0.10 + float64(clockHour)*0.005
			if h < 2 {
				imp = 0.35
				exp = 0.25
			}

			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: imp,
				ExportDollars: exp,
				BatteryMode:   bMode,
				SolarMode:     sMode,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, startTime, loc)
		require.NotNil(t, sched)

		// Find the period covering 14:00 to 16:00
		var activeExportPeriod *types.TOUPeriod
		for i := range sched.Periods {
			p := &sched.Periods[i]
			if p.StartHour == 14 && p.EndHour == 16 {
				activeExportPeriod = p
				break
			}
		}
		require.NotNil(t, activeExportPeriod, "Immediate 14:00-16:00 block should exist in schedule")
		assert.Equal(t, types.SolarModeExport, activeExportPeriod.SolarMode, "Immediate block must have SolarModeExport")
		assert.True(t, activeExportPeriod.Peak, "Export window must be flagged as Peak")

		// Verify the 19:00 to 21:00 window: future planned export must be preserved
		var futureExportPeriod *types.TOUPeriod
		for i := range sched.Periods {
			p := &sched.Periods[i]
			if p.StartHour == 19 && p.EndHour == 21 {
				futureExportPeriod = p
				break
			}
		}
		require.NotNil(t, futureExportPeriod, "Future 19:00-21:00 export block should exist in schedule")
		assert.Equal(t, types.BatteryModeExport, futureExportPeriod.BatteryMode, "Future planned export must retain BatteryModeExport")
		assert.True(t, futureExportPeriod.Peak, "Future planned export must be flagged as Peak")

		// Verify full 24h continuity from 00:00 to 24:00
		assert.Equal(t, 0, sched.Periods[0].StartHour)
		assert.Equal(t, 0, sched.Periods[0].StartMinute)
		assert.Equal(t, 24, sched.Periods[len(sched.Periods)-1].EndHour)
		assert.Equal(t, 0, sched.Periods[len(sched.Periods)-1].EndMinute)
	})

	t.Run("Active Immediate Battery Export Spanning Contiguous Intervals", func(t *testing.T) {
		// Scenario: Immediate high-rate event commanding battery export for 3 consecutive intervals (17:00 to 20:00).
		// The entire contiguous block must be consolidated with BatteryModeExport and Peak: true.
		startTime := baseTime.Add(17 * time.Hour) // 17:00
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			currStart := startTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			bMode := types.BatteryModeLoad
			imp := 0.25
			exp := 0.15

			if h < 3 { // 17:00 - 20:00 (3 hours active contiguous export)
				bMode = types.BatteryModeExport
				imp = 0.60
				exp = 0.55
			}

			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: imp,
				ExportDollars: exp,
				BatteryMode:   bMode,
				SolarMode:     types.SolarModeAny,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, startTime, loc)
		require.NotNil(t, sched)

		// Expect:
		// 1. 00:00 - 17:00 (Load, Any, Off-Peak)
		// 2. 17:00 - 20:00 (BatteryModeExport, Any, Peak)
		// 3. 20:00 - 24:00 (Load, Any, Off-Peak)
		require.Len(t, sched.Periods, 3)

		assert.Equal(t, "00:00", sched.Periods[0].StartTimeStr())
		assert.Equal(t, "17:00", sched.Periods[0].EndTimeStr())
		assert.Equal(t, types.BatteryModeLoad, sched.Periods[0].BatteryMode)

		assert.Equal(t, "17:00", sched.Periods[1].StartTimeStr())
		assert.Equal(t, "20:00", sched.Periods[1].EndTimeStr())
		assert.Equal(t, types.BatteryModeExport, sched.Periods[1].BatteryMode)
		assert.True(t, sched.Periods[1].Peak)
		assert.InDelta(t, 0.60, sched.Periods[1].ImportDollars, 0.0001)
		assert.InDelta(t, 0.55, sched.Periods[1].ExportDollars, 0.0001)

		assert.Equal(t, "20:00", sched.Periods[2].StartTimeStr())
		assert.Equal(t, "24:00", sched.Periods[2].EndTimeStr())
		assert.Equal(t, types.BatteryModeLoad, sched.Periods[2].BatteryMode)
	})

	t.Run("Active Export Crossing Midnight Boundary", func(t *testing.T) {
		// Scenario: Late evening export from 22:00 Monday to 02:00 Tuesday (4 hours).
		// In a 24-hour daily cycle (00:00 to 24:00), this should map to:
		// - 00:00 - 02:00: BatteryModeExport (morning half of midnight cross)
		// - 02:00 - 22:00: BatteryModeLoad (daytime baseline)
		// - 22:00 - 24:00: BatteryModeExport (evening half of midnight cross)
		startTime := baseTime.Add(22 * time.Hour) // 22:00 Monday
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			currStart := startTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			bMode := types.BatteryModeLoad
			imp := 0.20
			exp := 0.10

			if h < 4 { // 22:00 Monday to 02:00 Tuesday
				bMode = types.BatteryModeExport
				imp = 0.50
				exp = 0.45
			}

			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: imp,
				ExportDollars: exp,
				BatteryMode:   bMode,
				SolarMode:     types.SolarModeAny,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, startTime, loc)
		require.NotNil(t, sched)

		// Expect exactly 3 periods covering 00:00 to 24:00
		require.Len(t, sched.Periods, 3)

		// Morning segment
		assert.Equal(t, "00:00", sched.Periods[0].StartTimeStr())
		assert.Equal(t, "02:00", sched.Periods[0].EndTimeStr())
		assert.Equal(t, types.BatteryModeExport, sched.Periods[0].BatteryMode)
		assert.True(t, sched.Periods[0].Peak)

		// Daytime baseline segment
		assert.Equal(t, "02:00", sched.Periods[1].StartTimeStr())
		assert.Equal(t, "22:00", sched.Periods[1].EndTimeStr())
		assert.Equal(t, types.BatteryModeLoad, sched.Periods[1].BatteryMode)
		assert.False(t, sched.Periods[1].Peak)

		// Evening segment
		assert.Equal(t, "22:00", sched.Periods[2].StartTimeStr())
		assert.Equal(t, "24:00", sched.Periods[2].EndTimeStr())
		assert.Equal(t, types.BatteryModeExport, sched.Periods[2].BatteryMode)
		assert.True(t, sched.Periods[2].Peak)
	})

	t.Run("Active Standby With Solar Export Window", func(t *testing.T) {
		// Scenario: Controller decides to hold battery in Standby while exporting excess solar
		// for 2 hours (13:00 to 15:00). Because SolarModeExport is active, the hardware TOU
		// schedule must use the Standby+SolarExport override for 13:00 to 15:00.
		startTime := baseTime.Add(13 * time.Hour) // 13:00
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			currStart := startTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			bMode := types.BatteryModeLoad
			sMode := types.SolarModeAny

			if h < 2 { // 13:00 - 15:00 Standby with Solar Export
				bMode = types.BatteryModeStandby
				sMode = types.SolarModeExport
			}

			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: 0.20,
				ExportDollars: 0.10,
				BatteryMode:   bMode,
				SolarMode:     sMode,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, startTime, loc)
		require.NotNil(t, sched)

		// 13:00 to 15:00 must be marked Standby + SolarModeExport
		var standbyPeriod *types.TOUPeriod
		for i := range sched.Periods {
			if sched.Periods[i].StartHour == 13 && sched.Periods[i].EndHour == 15 {
				standbyPeriod = &sched.Periods[i]
				break
			}
		}
		require.NotNil(t, standbyPeriod)
		assert.Equal(t, types.BatteryModeStandby, standbyPeriod.BatteryMode)
		assert.Equal(t, types.SolarModeExport, standbyPeriod.SolarMode)
		assert.True(t, standbyPeriod.Peak)
	})

	t.Run("Self Consumption Charging And Standby Do Not Override TOU Schedule", func(t *testing.T) {
		// Scenario: Controller transitions through Standby (04:03-04:23), ChargeAny (04:23-05:00),
		// ChargeAny (04:43-05:00), and Load (05:01) in self-consumption mode (SolarModeAny).
		// None of these self-consumption actions should inject dynamic slices into the TOU schedule,
		// and polling at mid-hour timestamps (:03, :23, :43) must not cause the schedule to differ.
		buildPlanAt := func(nowTime time.Time, immMode types.BatteryMode) *types.Plan {
			var periods []types.PlanPeriod
			hourStart := time.Date(nowTime.Year(), nowTime.Month(), nowTime.Day(), nowTime.Hour(), 0, 0, 0, loc)
			firstEnd := hourStart.Add(time.Hour)
			periods = append(periods, types.PlanPeriod{
				TSStart:       nowTime,
				TSEnd:         firstEnd,
				DurationHours: firstEnd.Sub(nowTime).Hours(),
				ImportDollars: 0.055,
				ExportDollars: 0.032,
				BatteryMode:   immMode,
				SolarMode:     types.SolarModeAny,
			})
			for h := 1; h <= 24; h++ {
				currStart := hourStart.Add(time.Duration(h) * time.Hour)
				currEnd := currStart.Add(time.Hour)
				clockHour := currStart.Hour()
				imp := 0.055
				exp := 0.032
				if clockHour >= 18 && clockHour < 20 {
					imp = 0.105
					exp = 0.082
				}
				periods = append(periods, types.PlanPeriod{
					TSStart:       currStart,
					TSEnd:         currEnd,
					DurationHours: 1.0,
					ImportDollars: imp,
					ExportDollars: exp,
					BatteryMode:   types.BatteryModeLoad,
					SolarMode:     types.SolarModeAny,
				})
			}
			return &types.Plan{Periods: periods}
		}

		t0403 := baseTime.Add(4*time.Hour + 3*time.Minute)
		t0423 := baseTime.Add(4*time.Hour + 23*time.Minute)
		t0443 := baseTime.Add(4*time.Hour + 43*time.Minute)
		t0501 := baseTime.Add(5*time.Hour + 1*time.Minute)

		schedStandby := BuildTOUSchedule(buildPlanAt(t0403, types.BatteryModeStandby), t0403, loc)
		schedCharge1 := BuildTOUSchedule(buildPlanAt(t0423, types.BatteryModeChargeAny), t0423, loc)
		schedCharge2 := BuildTOUSchedule(buildPlanAt(t0443, types.BatteryModeChargeAny), t0443, loc)
		schedLoad := BuildTOUSchedule(buildPlanAt(t0501, types.BatteryModeLoad), t0501, loc)

		require.NotNil(t, schedStandby)
		require.NotNil(t, schedCharge1)
		require.NotNil(t, schedCharge2)
		require.NotNil(t, schedLoad)

		for _, p := range schedCharge1.Periods {
			assert.Equal(t, types.BatteryModeLoad, p.BatteryMode, "self-consumption ChargeAny must not override TOU BatteryMode")
			assert.Equal(t, types.SolarModeAny, p.SolarMode)
		}
		for _, p := range schedStandby.Periods {
			assert.Equal(t, types.BatteryModeLoad, p.BatteryMode, "self-consumption Standby must not override TOU BatteryMode")
			assert.Equal(t, types.SolarModeAny, p.SolarMode)
		}

		assert.False(t, schedStandby.IsSignificantlyDifferent(schedCharge1, t0423), "transition from Standby to ChargeAny must not change TOU schedule")
		assert.False(t, schedCharge1.IsSignificantlyDifferent(schedCharge2, t0443), "continuing ChargeAny 20 minutes later must not change TOU schedule")
		assert.False(t, schedCharge2.IsSignificantlyDifferent(schedLoad, t0501), "transition from ChargeAny back to Load must not change TOU schedule")
	})

	t.Run("Short Plan Under 24 Hours Fills Remaining Hours Seamlessly", func(t *testing.T) {
		// Scenario: Plan only has 6 hours of data (06:00 to 12:00).
		// Fallback must ensure 00:00 to 06:00 and 12:00 to 24:00 are cleanly covered.
		startTime := baseTime.Add(6 * time.Hour)
		var periods []types.PlanPeriod
		for h := 0; h < 6; h++ {
			currStart := startTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: 0.18,
				ExportDollars: 0.09,
				BatteryMode:   types.BatteryModeLoad,
				SolarMode:     types.SolarModeAny,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, startTime, loc)
		require.NotNil(t, sched)

		// Schedule must be complete from 00:00 to 24:00
		assert.Equal(t, 0, sched.Periods[0].StartHour)
		assert.Equal(t, 0, sched.Periods[0].StartMinute)
		assert.Equal(t, 24, sched.Periods[len(sched.Periods)-1].EndHour)
		assert.Equal(t, 0, sched.Periods[len(sched.Periods)-1].EndMinute)

		// Verify continuous coverage
		for i := 0; i < len(sched.Periods)-1; i++ {
			assert.Equal(t, sched.Periods[i].EndHour, sched.Periods[i+1].StartHour)
			assert.Equal(t, sched.Periods[i].EndMinute, sched.Periods[i+1].StartMinute)
		}
	})

	t.Run("Dynamic Hourly Pricing (24 Distinct Hourly Rates)", func(t *testing.T) {
		// Scenario: ComEd RTP / Ameren Smart Pricing where every hour has a different rate.
		// All 24 hourly periods should be preserved with their distinct rates.
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			currStart := baseTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			// Rates varying by $0.02 each hour (above coalescing threshold of $0.005)
			imp := 0.10 + float64(h)*0.02
			exp := 0.05 + float64(h)*0.01

			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: imp,
				ExportDollars: exp,
				BatteryMode:   types.BatteryModeLoad,
				SolarMode:     types.SolarModeAny,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, baseTime, loc)
		require.NotNil(t, sched)

		// Since all hourly rates differ by >= $0.02, none should be coalesced
		require.Len(t, sched.Periods, 24)

		for h := 0; h < 24; h++ {
			p := sched.Periods[h]
			assert.Equal(t, h, p.StartHour)
			assert.Equal(t, h+1, p.EndHour)
			expectedImp := 0.10 + float64(h)*0.02
			assert.InDelta(t, expectedImp, p.ImportDollars, 0.0001)
		}
	})

	t.Run("Rounding Helpers Start And End", func(t *testing.T) {
		base := time.Date(2026, 10, 5, 14, 0, 0, 0, loc)

		// RoundTOUPeriodStart must always round DOWN to nearest :00 or :30
		assert.Equal(t, base, roundTOUPeriodStart(base.Add(5*time.Minute)))
		assert.Equal(t, base, roundTOUPeriodStart(base.Add(14*time.Minute)))
		assert.Equal(t, base, roundTOUPeriodStart(base.Add(15*time.Minute)))
		assert.Equal(t, base, roundTOUPeriodStart(base.Add(29*time.Minute)))
		assert.Equal(t, base.Add(30*time.Minute), roundTOUPeriodStart(base.Add(30*time.Minute)))
		assert.Equal(t, base.Add(30*time.Minute), roundTOUPeriodStart(base.Add(45*time.Minute)))
		assert.Equal(t, base.Add(30*time.Minute), roundTOUPeriodStart(base.Add(59*time.Minute)))

		// RoundTOUPeriodEnd rounds up/down based on thresholds
		assert.Equal(t, base, roundTOUPeriodEnd(base.Add(5*time.Minute)))
		assert.Equal(t, base, roundTOUPeriodEnd(base.Add(14*time.Minute)))
		assert.Equal(t, base.Add(30*time.Minute), roundTOUPeriodEnd(base.Add(15*time.Minute)))
		assert.Equal(t, base.Add(30*time.Minute), roundTOUPeriodEnd(base.Add(30*time.Minute)))
		assert.Equal(t, base.Add(time.Hour), roundTOUPeriodEnd(base.Add(31*time.Minute)))
		assert.Equal(t, base.Add(time.Hour), roundTOUPeriodEnd(base.Add(45*time.Minute)))
		assert.Equal(t, base.Add(time.Hour), roundTOUPeriodEnd(base.Add(59*time.Minute)))
	})

	t.Run("Schedule IsSignificantlyDifferent Thresholds", func(t *testing.T) {
		s1 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 16, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 0, EndHour: 21, EndMinute: 0, ImportDollars: 0.450, ExportDollars: 0.350, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}

		// Identical schedule -> not significantly different
		assert.False(t, s1.IsSignificantlyDifferent(s1, time.Time{}))

		// Nil schedule -> significantly different
		assert.True(t, s1.IsSignificantlyDifferent(nil, time.Time{}))

		// Export period rate difference within $0.05 ($0.030) -> not significantly different
		s2 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 16, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 0, EndHour: 21, EndMinute: 0, ImportDollars: 0.480, ExportDollars: 0.350, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}
		assert.False(t, s1.IsSignificantlyDifferent(s2, time.Time{}))

		// Export period rate difference exceeding $0.05 ($0.070) -> significantly different
		s3 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 16, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 0, EndHour: 21, EndMinute: 0, ImportDollars: 0.520, ExportDollars: 0.350, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}
		assert.True(t, s1.IsSignificantlyDifferent(s3, time.Time{}))

		// Time shift within 15 mins (10 min shift from 16:00 to 16:10) -> not significantly different
		s4 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 16, EndMinute: 10, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 10, EndHour: 21, EndMinute: 0, ImportDollars: 0.450, ExportDollars: 0.350, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}
		assert.False(t, s1.IsSignificantlyDifferent(s4, time.Time{}))

		// Time shift exceeding 15 mins (30 min shift from 16:00 to 16:30) -> significantly different
		s5 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 16, EndMinute: 30, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 30, EndHour: 21, EndMinute: 0, ImportDollars: 0.450, ExportDollars: 0.350, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}
		assert.True(t, s1.IsSignificantlyDifferent(s5, time.Time{}))

		// Mode change (SolarModeExport -> BatteryModeExport) -> significantly different
		s6 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 16, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 0, EndHour: 21, EndMinute: 0, ImportDollars: 0.450, ExportDollars: 0.350, BatteryMode: types.BatteryModeExport, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.150, ExportDollars: 0.080, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}
		assert.True(t, s1.IsSignificantlyDifferent(s6, time.Time{}))

		// Non-export sub-splits, merges, Peak flag flips, and rate changes (e.g. ComEd dynamic hourly pricing)
		// must coalesce and NOT be flagged as significantly different when export periods match
		s7 := &types.TOUSchedule{
			Periods: []types.TOUPeriod{
				{StartHour: 0, StartMinute: 0, EndHour: 5, EndMinute: 0, ImportDollars: 0.050, ExportDollars: 0.020, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 5, StartMinute: 0, EndHour: 7, EndMinute: 0, ImportDollars: 0.220, ExportDollars: 0.180, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: true},
				{StartHour: 7, StartMinute: 0, EndHour: 16, EndMinute: 0, ImportDollars: 0.080, ExportDollars: 0.040, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 16, StartMinute: 0, EndHour: 21, EndMinute: 0, ImportDollars: 0.450, ExportDollars: 0.350, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeExport, Peak: true},
				{StartHour: 21, StartMinute: 0, EndHour: 23, EndMinute: 0, ImportDollars: 0.065, ExportDollars: 0.035, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
				{StartHour: 23, StartMinute: 0, EndHour: 24, EndMinute: 0, ImportDollars: 0.090, ExportDollars: 0.055, BatteryMode: types.BatteryModeLoad, SolarMode: types.SolarModeAny, Peak: false},
			},
		}
		assert.False(t, s1.IsSignificantlyDifferent(s7, time.Time{}))
	})

	t.Run("DynamicHourlyPricingComEdStyleCoalescesIntoBlocks", func(t *testing.T) {
		// 24 individual hourly prices simulating dynamic real-time hourly pricing (e.g. ComEd)
		hourlyRates := []float64{
			// 00:00 - 06:00: Overnight dip
			0.021, 0.023, 0.025, 0.024, 0.026, 0.028,
			// 06:00 - 14:00: Morning/midday baseline
			0.060, 0.065, 0.068, 0.070, 0.072, 0.069, 0.065, 0.062,
			// 14:00 - 17:00: Afternoon shoulder
			0.110, 0.118, 0.125,
			// 17:00 - 21:00: Peak evening spike (near max rate of 0.380)
			0.378, 0.380, 0.380, 0.376,
			// 21:00 - 24:00: Night baseline
			0.065, 0.062, 0.058,
		}

		var periods []types.PlanPeriod
		for h, r := range hourlyRates {
			start := baseTime.Add(time.Duration(h) * time.Hour)
			end := start.Add(time.Hour)
			periods = append(periods, types.PlanPeriod{
				TSStart:       start,
				TSEnd:         end,
				DurationHours: 1.0,
				ImportDollars: r,
				ExportDollars: r * 0.8,
				BatteryMode:   types.BatteryModeLoad,
				SolarMode:     types.SolarModeAny,
			})
		}

		plan := &types.Plan{Periods: periods}
		sched := BuildTOUSchedule(plan, baseTime, loc)
		require.NotNil(t, sched)

		// The 24 individual hourly periods should coalesce cleanly into 5 blocks:
		// 1. 00:00 - 06:00 (Overnight dip, weighted avg 0.0245)
		// 2. 06:00 - 14:00 (Midday baseline, weighted avg 0.0664)
		// 3. 14:00 - 17:00 (Afternoon shoulder, weighted avg 0.1177)
		// 4. 17:00 - 21:00 (Evening peak spike, weighted avg 0.3785, Peak: true)
		// 5. 21:00 - 24:00 (Night baseline, weighted avg 0.0617)
		require.Len(t, sched.Periods, 5)

		// Block 1: 00:00 - 06:00
		b1 := sched.Periods[0]
		assert.Equal(t, "00:00", b1.StartTimeStr())
		assert.Equal(t, "06:00", b1.EndTimeStr())
		assert.InDelta(t, (0.021+0.023+0.025+0.024+0.026+0.028)/6.0, b1.ImportDollars, 0.0001)
		assert.False(t, b1.Peak)

		// Block 2: 06:00 - 14:00
		b2 := sched.Periods[1]
		assert.Equal(t, "06:00", b2.StartTimeStr())
		assert.Equal(t, "14:00", b2.EndTimeStr())
		assert.InDelta(t, (0.060+0.065+0.068+0.070+0.072+0.069+0.065+0.062)/8.0, b2.ImportDollars, 0.0001)
		assert.False(t, b2.Peak)

		// Block 3: 14:00 - 17:00
		b3 := sched.Periods[2]
		assert.Equal(t, "14:00", b3.StartTimeStr())
		assert.Equal(t, "17:00", b3.EndTimeStr())
		assert.InDelta(t, (0.110+0.118+0.125)/3.0, b3.ImportDollars, 0.0001)
		assert.False(t, b3.Peak)

		// Block 4: 17:00 - 21:00 (Peak window)
		b4 := sched.Periods[3]
		assert.Equal(t, "17:00", b4.StartTimeStr())
		assert.Equal(t, "21:00", b4.EndTimeStr())
		assert.InDelta(t, (0.378+0.380+0.380+0.376)/4.0, b4.ImportDollars, 0.0001)
		assert.True(t, b4.Peak)

		// Block 5: 21:00 - 24:00
		b5 := sched.Periods[4]
		assert.Equal(t, "21:00", b5.StartTimeStr())
		assert.Equal(t, "24:00", b5.EndTimeStr())
		assert.InDelta(t, (0.065+0.062+0.058)/3.0, b5.ImportDollars, 0.0001)
		assert.False(t, b5.Peak)
	})

	t.Run("Future Export Included While NonExport Remains SelfConsumption", func(t *testing.T) {
		// Scenario: At 08:00 Monday, the immediate action is Load/SolarModeAny, followed by
		// ChargeAny (09:00-11:00), Standby without solar export (11:00-12:00), planned
		// Solar Export (12:00-16:00), and planned Battery Export (18:00-20:00).
		// Only the export windows (12:00-16:00 and 18:00-20:00) should retain their export
		// dispatch in the TOU schedule; non-export periods must stay on Load/SolarModeAny.
		startTime := baseTime.Add(8 * time.Hour) // 08:00 Monday
		var periods []types.PlanPeriod
		for h := 0; h < 24; h++ {
			currStart := startTime.Add(time.Duration(h) * time.Hour)
			currEnd := currStart.Add(time.Hour)
			clockHour := currStart.Hour()

			bMode := types.BatteryModeLoad
			sMode := types.SolarModeAny
			switch {
			case clockHour >= 9 && clockHour < 11:
				bMode = types.BatteryModeChargeAny
			case clockHour == 11:
				bMode = types.BatteryModeStandby
			case clockHour >= 12 && clockHour < 16:
				bMode = types.BatteryModeLoad
				sMode = types.SolarModeExport
			case clockHour >= 18 && clockHour < 20:
				bMode = types.BatteryModeExport
			}

			periods = append(periods, types.PlanPeriod{
				TSStart:       currStart,
				TSEnd:         currEnd,
				DurationHours: 1.0,
				ImportDollars: 0.15,
				ExportDollars: 0.08,
				BatteryMode:   bMode,
				SolarMode:     sMode,
			})
		}

		sched := BuildTOUSchedule(&types.Plan{Periods: periods}, startTime, loc)
		require.NotNil(t, sched)
		if assert.Len(t, sched.Periods, 5) {
			assert.Equal(t, "00:00", sched.Periods[0].StartTimeStr())
			assert.Equal(t, "12:00", sched.Periods[0].EndTimeStr())
			assert.Equal(t, types.BatteryModeLoad, sched.Periods[0].BatteryMode)
			assert.Equal(t, types.SolarModeAny, sched.Periods[0].SolarMode)
			assert.False(t, sched.Periods[0].Peak)

			assert.Equal(t, "12:00", sched.Periods[1].StartTimeStr())
			assert.Equal(t, "16:00", sched.Periods[1].EndTimeStr())
			assert.Equal(t, types.BatteryModeLoad, sched.Periods[1].BatteryMode)
			assert.Equal(t, types.SolarModeExport, sched.Periods[1].SolarMode)
			assert.True(t, sched.Periods[1].Peak)

			assert.Equal(t, "16:00", sched.Periods[2].StartTimeStr())
			assert.Equal(t, "18:00", sched.Periods[2].EndTimeStr())
			assert.Equal(t, types.BatteryModeLoad, sched.Periods[2].BatteryMode)
			assert.Equal(t, types.SolarModeAny, sched.Periods[2].SolarMode)
			assert.False(t, sched.Periods[2].Peak)

			assert.Equal(t, "18:00", sched.Periods[3].StartTimeStr())
			assert.Equal(t, "20:00", sched.Periods[3].EndTimeStr())
			assert.Equal(t, types.BatteryModeExport, sched.Periods[3].BatteryMode)
			assert.True(t, sched.Periods[3].Peak)

			assert.Equal(t, "20:00", sched.Periods[4].StartTimeStr())
			assert.Equal(t, "24:00", sched.Periods[4].EndTimeStr())
			assert.Equal(t, types.BatteryModeLoad, sched.Periods[4].BatteryMode)
			assert.Equal(t, types.SolarModeAny, sched.Periods[4].SolarMode)
			assert.False(t, sched.Periods[4].Peak)
		}
	})

	t.Run("Does Not Update Tomorrows Peak Period Overlapping Now Until Period Ends", func(t *testing.T) {
		// Scenario: Peak export window is 12:00 to 16:00.
		// Today (Monday) 12:00-16:00 is Load + SolarModeExport.
		// Tomorrow (Tuesday) 12:00-16:00 is Standby + SolarModeExport.
		// While now is inside 12:00-16:00 (e.g. 12:20, 13:20, 15:20), tomorrow's overlapping
		// [12:00, now) slice must not split or change the 12:00-16:00 period every 20 minutes.
		// Once now is after 16:00 (e.g. 16:05), the 12:00-16:00 period updates to tomorrow's dispatch.
		buildPlan := func(nowTime time.Time) *types.Plan {
			var periods []types.PlanPeriod
			hourStart := time.Date(nowTime.Year(), nowTime.Month(), nowTime.Day(), nowTime.Hour(), 0, 0, 0, loc)
			for h := 0; h <= 24; h++ {
				currStart := hourStart.Add(time.Duration(h) * time.Hour)
				currEnd := currStart.Add(time.Hour)
				if h == 0 {
					currStart = nowTime
				}
				bMode := types.BatteryModeLoad
				sMode := types.SolarModeAny
				if currStart.Hour() >= 12 && currStart.Hour() < 16 {
					sMode = types.SolarModeExport
					if currStart.Day() == baseTime.Day() {
						bMode = types.BatteryModeLoad
					} else {
						bMode = types.BatteryModeStandby
					}
				}
				periods = append(periods, types.PlanPeriod{
					TSStart:       currStart,
					TSEnd:         currEnd,
					DurationHours: currEnd.Sub(currStart).Hours(),
					ImportDollars: 0.15,
					ExportDollars: 0.08,
					BatteryMode:   bMode,
					SolarMode:     sMode,
				})
			}
			return &types.Plan{Periods: periods}
		}

		t1200 := baseTime.Add(12 * time.Hour)
		t1220 := baseTime.Add(12*time.Hour + 20*time.Minute)
		t1320 := baseTime.Add(13*time.Hour + 20*time.Minute)
		t1520 := baseTime.Add(15*time.Hour + 20*time.Minute)
		t1605 := baseTime.Add(16*time.Hour + 5*time.Minute)

		sched1200 := BuildTOUSchedule(buildPlan(t1200), t1200, loc)
		sched1220 := BuildTOUSchedule(buildPlan(t1220), t1220, loc)
		sched1320 := BuildTOUSchedule(buildPlan(t1320), t1320, loc)
		sched1520 := BuildTOUSchedule(buildPlan(t1520), t1520, loc)
		sched1605 := BuildTOUSchedule(buildPlan(t1605), t1605, loc)

		// Throughout 12:00-16:00, the 12:00-16:00 period must remain Load + SolarModeExport without changing
		assert.False(t, sched1200.IsSignificantlyDifferent(sched1220, t1220))
		assert.False(t, sched1200.IsSignificantlyDifferent(sched1320, t1320))
		assert.False(t, sched1200.IsSignificantlyDifferent(sched1520, t1520))
		if assert.Len(t, sched1320.Periods, 3) {
			assert.Equal(t, "12:00", sched1320.Periods[1].StartTimeStr())
			assert.Equal(t, "16:00", sched1320.Periods[1].EndTimeStr())
			assert.Equal(t, types.BatteryModeLoad, sched1320.Periods[1].BatteryMode)
			assert.Equal(t, types.SolarModeExport, sched1320.Periods[1].SolarMode)
		}

		// After 16:00 (at 16:05), the 12:00-16:00 period updates to tomorrow's Standby + SolarModeExport dispatch
		assert.True(t, sched1200.IsSignificantlyDifferent(sched1605, t1605))
		if assert.Len(t, sched1605.Periods, 3) {
			assert.Equal(t, "12:00", sched1605.Periods[1].StartTimeStr())
			assert.Equal(t, "16:00", sched1605.Periods[1].EndTimeStr())
			assert.Equal(t, types.BatteryModeStandby, sched1605.Periods[1].BatteryMode)
			assert.Equal(t, types.SolarModeExport, sched1605.Periods[1].SolarMode)
		}
	})

	t.Run("Does Not Flag Advancing Active Period Start When Tomorrow Is OffPeak", func(t *testing.T) {
		// Scenario: Today (Friday) enters Direct Solar Export during an on-peak window (12:41 to 20:00),
		// while tomorrow (Saturday) is off-peak self-consumption all day.
		// Because [00:00, now) comes from tomorrow (Off-Peak) and [now, 20:00) comes from today (On-Peak Export),
		// the start of the active period in BuildTOUSchedule advances with now (12:41 -> 13:03 -> 13:23 -> 15:23).
		// IsSignificantlyDifferent must treat these schedules as matching while inside the active period,
		// while still flagging changes to the period's end boundary or future periods.
		fridayMidnight := time.Date(2026, 10, 16, 0, 0, 0, 0, loc)
		buildFridayPlan := func(nowTime time.Time, exportEndHour int) *types.Plan {
			var periods []types.PlanPeriod
			hourStart := time.Date(nowTime.Year(), nowTime.Month(), nowTime.Day(), nowTime.Hour(), 0, 0, 0, loc)
			for h := 0; h <= 24; h++ {
				currStart := hourStart.Add(time.Duration(h) * time.Hour)
				currEnd := currStart.Add(time.Hour)
				if h == 0 {
					currStart = nowTime
				}
				bMode := types.BatteryModeLoad
				sMode := types.SolarModeAny
				imp := 0.20754
				exp := 0.20754
				// Friday 12:00 to exportEndHour is On-Peak Direct Solar Export; Saturday is Off-Peak all day
				if currStart.Day() == fridayMidnight.Day() && currStart.Hour() >= 12 && currStart.Hour() < exportEndHour {
					sMode = types.SolarModeExport
					imp = 0.29982
					exp = 0.29982
				}
				periods = append(periods, types.PlanPeriod{
					TSStart:       currStart,
					TSEnd:         currEnd,
					DurationHours: currEnd.Sub(currStart).Hours(),
					ImportDollars: imp,
					ExportDollars: exp,
					BatteryMode:   bMode,
					SolarMode:     sMode,
				})
			}
			return &types.Plan{Periods: periods}
		}

		t1241 := fridayMidnight.Add(12*time.Hour + 41*time.Minute)
		t1303 := fridayMidnight.Add(13*time.Hour + 3*time.Minute)
		t1323 := fridayMidnight.Add(13*time.Hour + 23*time.Minute)
		t1523 := fridayMidnight.Add(15*time.Hour + 23*time.Minute)

		sched1241 := BuildTOUSchedule(buildFridayPlan(t1241, 20), t1241, loc)
		sched1303 := BuildTOUSchedule(buildFridayPlan(t1303, 20), t1303, loc)
		sched1323 := BuildTOUSchedule(buildFridayPlan(t1323, 20), t1323, loc)
		sched1523 := BuildTOUSchedule(buildFridayPlan(t1523, 20), t1523, loc)

		assert.False(t, sched1241.IsSignificantlyDifferent(sched1303, t1303), "advancing active start from 12:41 to 13:03 should not be significantly different")
		assert.False(t, sched1241.IsSignificantlyDifferent(sched1323, t1323), "advancing active start from 12:41 to 13:23 should not be significantly different")
		assert.False(t, sched1241.IsSignificantlyDifferent(sched1523, t1523), "advancing active start from 12:41 to 15:23 should not be significantly different")

		// If the end of the active export period changes (e.g. 20:00 -> 19:00), it must still be flagged as different
		sched1523Shortened := BuildTOUSchedule(buildFridayPlan(t1523, 19), t1523, loc)
		assert.True(t, sched1241.IsSignificantlyDifferent(sched1523Shortened, t1523), "changing the future end of the active period must be significantly different")
	})
}
