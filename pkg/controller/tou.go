package controller

import (
	"math"
	"sort"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// minTOUPeakSpreadDollars ($0.02/kWh, or 2¢/kWh) is the minimum price spread across the 24-hour horizon
	// required to identify distinct on-peak pricing windows.
	minTOUPeakSpreadDollars = 0.02
)

// roundTOUPeriodStart rounds a start timestamp for ESS TOU periods down to the nearest 00 or 30 minute boundary.
// It rounds down (never up) so that peak periods and export dispatches are immediately active without delay.
func roundTOUPeriodStart(t time.Time) time.Time {
	truncated := t.Truncate(time.Hour)
	if t.Minute() < 30 {
		return truncated
	}
	return truncated.Add(30 * time.Minute)
}

// roundTOUPeriodEnd rounds an ending timestamp for ESS TOU periods to 00 or 30 minute boundaries.
// - Minutes < 15 round down to the top of the hour (:00).
// - Minutes between 15 and 30 round to half past (:30).
// - Minutes >= 31 round up to the next hour (:00).
// This ensures peak and export windows are given sufficient duration on the hardware clock.
func roundTOUPeriodEnd(t time.Time) time.Time {
	minute := t.Minute()
	truncated := t.Truncate(time.Hour)
	switch {
	case minute < 15:
		return truncated
	case minute <= 30:
		return truncated.Add(30 * time.Minute)
	default: // minute >= 31
		return truncated.Add(time.Hour)
	}
}

// rawTOUSlice represents an unmerged time slice mapped into local clock minutes (0 to 1440).
type rawTOUSlice struct {
	startMin          int // 0 to 1440
	endMin            int // 0 to 1440
	importRateDollars float64
	exportRateDollars float64
	batteryMode       types.BatteryMode
	solarMode         types.SolarMode
	peak              bool
}

// BuildTOUSchedule translates a forward-looking Plan into a normalized, 24-hour recurring Time-Of-Use
// schedule spanning exactly 00:00 to 24:00 local time.
//
// Architectural Principles & Assumptions:
//
//  1. 24-Hour Recurring Horizon:
//     Hardware TOU engines (FranklinWH and Tesla Powerwall) evaluate recurring daily schedules
//     expressed in local wall-clock hours (00:00 to 24:00). We extract the next ~24 hours of
//     pricing from the plan and project it across a standard 24-hour daily cycle, assuming these rates
//     repeat indefinitely until the next update loop.
//     (Note: If weekday vs. weekend tariffs differ, the schedule will naturally be refreshed when the
//     update loop runs on Friday and Sunday).
//
//  2. Short-Term Trust Horizon for Operational Control:
//     Dynamic forecasts (solar irradiance, temperature, home usage) become less reliable several
//     hours out. We do not want to program autonomous export dispatches deep into the future on the
//     hardware, which could misfire if weather or usage suddenly shifts.
//     Therefore, ONLY the immediate contiguous block of the active control action (from plan.Periods[0]
//     onward with identical BatteryMode and SolarMode) is programmed as an active override dispatch
//     (e.g., Solar Export, Battery Export, or Standby).
//     All subsequent periods in the 24-hour cycle automatically revert to standard Self-Consumption
//     (BatteryModeLoad, SolarModeAny), keeping RateRudder in continuous, real-time control.
//
//  3. True Pricing 24/7 for Accurate App Analytics:
//     Even during Self-Consumption periods, the true retail import and export rates from the utility plan
//     are preserved for every hour. This allows mobile apps (like Franklin's) to accurately compute financial
//     savings, utility bill offsets, and real-time rate curves throughout the entire day.
//
//  4. Gap-Free & Coalesced:
//     The resulting schedule is guaranteed to span seamlessly from 00:00 to 24:00 without gaps or overlaps.
//     Adjacent periods sharing identical modes, peak status, and near-identical rates (within $0.005/kWh)
//     are coalesced into clean, compact blocks.
func BuildTOUSchedule(plan *types.Plan, now time.Time, loc *time.Location) *types.TOUSchedule {
	if plan == nil || len(plan.Periods) == 0 {
		return nil
	}

	// Default now and location if not explicitly provided
	if now.IsZero() {
		if !plan.TSCreated.IsZero() {
			now = plan.TSCreated
		} else {
			now = plan.Periods[0].TSStart
		}
	}
	if loc == nil {
		loc = now.Location()
	}
	if loc == nil {
		loc = time.UTC
	}

	imm := plan.Periods[0]
	immBatteryMode := imm.BatteryMode
	immSolarMode := imm.SolarMode

	// Determine if the immediate action is an operational override dispatch
	isOverride := immBatteryMode == types.BatteryModeExport ||
		immSolarMode == types.SolarModeExport ||
		immBatteryMode == types.BatteryModeChargeAny ||
		immBatteryMode == types.BatteryModeStandby

	// Find the end boundary of the immediate contiguous block of identical action parameters.
	// Only periods within this contiguous block will receive the override dispatch.
	activeUntil := imm.TSEnd
	for i := 1; i < len(plan.Periods); i++ {
		p := plan.Periods[i]
		if p.BatteryMode == immBatteryMode && p.SolarMode == immSolarMode {
			activeUntil = p.TSEnd
		} else {
			break
		}
	}
	activeUntilInLoc := activeUntil.In(loc)

	// Define the 24-hour planning horizon in local time
	horizonStart := plan.Periods[0].TSStart.In(loc)
	horizonEnd := horizonStart.Add(24 * time.Hour)

	// Scan 24h horizon to detect utility peak pricing windows (when not exporting)
	var maxImport float64
	minImport := math.MaxFloat64
	for _, p := range plan.Periods {
		pStart := p.TSStart.In(loc)
		pEnd := p.TSEnd.In(loc)
		if pEnd.After(horizonStart) && pStart.Before(horizonEnd) {
			if p.ImportDollars > maxImport {
				maxImport = p.ImportDollars
			}
			if p.ImportDollars < minImport {
				minImport = p.ImportDollars
			}
		}
	}

	// Helper to determine if a period is an on-peak tariff window
	isUtilityPeakRate := func(importRate float64) bool {
		// If there is meaningful variation (>= minTOUPeakSpreadDollars spread), periods within
		// priceMaterialityThresholdDollars (0.5 cents) of the maximum rate represent the utility peak window.
		if maxImport-minImport >= minTOUPeakSpreadDollars && importRate >= maxImport-priceMaterialityThresholdDollars {
			return true
		}
		return false
	}

	// Slice and map plan periods across the 24-hour horizon into clock minutes (0 to 1440)
	var rawSlices []rawTOUSlice

	for _, p := range plan.Periods {
		pStart := p.TSStart.In(loc)
		pEnd := p.TSEnd.In(loc)

		// Skip intervals outside the 24-hour horizon
		if !pEnd.After(horizonStart) || !pStart.Before(horizonEnd) {
			continue
		}

		// Clip interval to the 24-hour window
		if pStart.Before(horizonStart) {
			pStart = horizonStart
		}
		if pEnd.After(horizonEnd) {
			pEnd = horizonEnd
		}
		if !pEnd.After(pStart) {
			continue
		}

		// Operational mode & peak determination:
		// Only intervals within the active contiguous block retain the override dispatch.
		// All subsequent intervals revert to standard self-consumption.
		var bMode types.BatteryMode
		var sMode types.SolarMode
		var isPeak bool

		if isOverride && pStart.Before(activeUntilInLoc) {
			bMode = immBatteryMode
			sMode = immSolarMode
			if immBatteryMode == types.BatteryModeExport || immSolarMode == types.SolarModeExport {
				isPeak = true
			} else {
				isPeak = isUtilityPeakRate(p.ImportDollars)
			}
		} else {
			bMode = types.BatteryModeLoad
			sMode = types.SolarModeAny
			isPeak = isUtilityPeakRate(p.ImportDollars)
		}

		// Split intervals that cross midnight so each slice falls cleanly within a single calendar day
		curr := pStart
		for curr.Before(pEnd) {
			nextMidnight := time.Date(curr.Year(), curr.Month(), curr.Day()+1, 0, 0, 0, 0, loc)
			sliceEnd := pEnd
			if sliceEnd.After(nextMidnight) {
				sliceEnd = nextMidnight
			}

			startMin := curr.Hour()*60 + curr.Minute()
			endMin := sliceEnd.Hour()*60 + sliceEnd.Minute()
			if sliceEnd.Equal(nextMidnight) || (sliceEnd.Hour() == 0 && sliceEnd.Minute() == 0 && sliceEnd.After(curr)) {
				endMin = 1440 // 24:00
			}

			if endMin > startMin {
				rawSlices = append(rawSlices, rawTOUSlice{
					startMin:          startMin,
					endMin:            endMin,
					importRateDollars: p.ImportDollars,
					exportRateDollars: p.ExportDollars,
					batteryMode:       bMode,
					solarMode:         sMode,
					peak:              isPeak,
				})
			}

			curr = sliceEnd
		}
	}

	if len(rawSlices) == 0 {
		return nil
	}

	// Sort slices by start minute which will bring tomorrow's periods first before
	// today's periods
	sort.Slice(rawSlices, func(i, j int) bool {
		if rawSlices[i].startMin != rawSlices[j].startMin {
			return rawSlices[i].startMin < rawSlices[j].startMin
		}
		return rawSlices[i].endMin < rawSlices[j].endMin
	})

	// Fill any gaps and ensure non-overlapping, strictly contiguous coverage from 0 to 1440
	var cleanSlices []rawTOUSlice
	currentMin := 0

	for _, s := range rawSlices {
		if s.endMin <= currentMin {
			// Slices that end before currentMin are redundant duplicates (e.g. from 24h+ boundary)
			continue
		}
		if s.startMin < currentMin {
			// Trim overlap with previous slice
			s.startMin = currentMin
		}
		if s.startMin > currentMin {
			// Fill gap with default self-consumption and rates from the upcoming slice
			cleanSlices = append(cleanSlices, rawTOUSlice{
				startMin:          currentMin,
				endMin:            s.startMin,
				importRateDollars: s.importRateDollars,
				exportRateDollars: s.exportRateDollars,
				batteryMode:       types.BatteryModeLoad,
				solarMode:         types.SolarModeAny,
				peak:              false,
			})
		}

		cleanSlices = append(cleanSlices, s)
		currentMin = s.endMin
	}

	// If the schedule ends before 24:00 (e.g., short plan < 24h), extend to 1440
	if currentMin < 1440 {
		lastRateImport := 0.0
		lastRateExport := 0.0
		if len(cleanSlices) > 0 {
			lastRateImport = cleanSlices[len(cleanSlices)-1].importRateDollars
			lastRateExport = cleanSlices[len(cleanSlices)-1].exportRateDollars
		}
		cleanSlices = append(cleanSlices, rawTOUSlice{
			startMin:          currentMin,
			endMin:            1440,
			importRateDollars: lastRateImport,
			exportRateDollars: lastRateExport,
			batteryMode:       types.BatteryModeLoad,
			solarMode:         types.SolarModeAny,
			peak:              false,
		})
	}

	// Coalesce adjacent slices that share identical modes, peak status, and near-identical rates (within TOUCoalesceRateThresholdDollars)
	var coalesced []rawTOUSlice
	for _, s := range cleanSlices {
		if len(coalesced) == 0 {
			coalesced = append(coalesced, s)
			continue
		}

		prev := &coalesced[len(coalesced)-1]
		canMerge := prev.batteryMode == s.batteryMode &&
			prev.solarMode == s.solarMode &&
			prev.peak == s.peak &&
			math.Abs(prev.importRateDollars-s.importRateDollars) <= types.TOUCoalesceRateThresholdDollars &&
			math.Abs(prev.exportRateDollars-s.exportRateDollars) <= types.TOUCoalesceRateThresholdDollars

		if canMerge {
			prevDur := prev.endMin - prev.startMin
			sDur := s.endMin - s.startMin
			totalDur := prevDur + sDur
			if totalDur > 0 {
				prev.importRateDollars = (prev.importRateDollars*float64(prevDur) + s.importRateDollars*float64(sDur)) / float64(totalDur)
				prev.exportRateDollars = (prev.exportRateDollars*float64(prevDur) + s.exportRateDollars*float64(sDur)) / float64(totalDur)
			}
			prev.endMin = s.endMin
		} else {
			coalesced = append(coalesced, s)
		}
	}

	// Convert coalesced slices to final TOUPeriod objects
	periods := make([]types.TOUPeriod, len(coalesced))
	for i, c := range coalesced {
		periods[i] = types.TOUPeriod{
			StartHour:     c.startMin / 60,
			StartMinute:   c.startMin % 60,
			EndHour:       c.endMin / 60,
			EndMinute:     c.endMin % 60,
			ImportDollars: c.importRateDollars,
			ExportDollars: c.exportRateDollars,
			BatteryMode:   c.batteryMode,
			SolarMode:     c.solarMode,
			Peak:          c.peak,
		}
	}

	return &types.TOUSchedule{
		Periods: periods,
	}
}
