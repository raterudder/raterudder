package types

import (
	"fmt"
	"math"
	"time"
)

const (
	// touScheduleRateThresholdDollars defines the minimum rate difference ($/kWh) between
	// two TOU period rates to be considered a significant change warranting an ESS schedule update.
	// A value of $0.0101 (just above 1 cent) avoids unnecessary writes to ESS hardware or cloud APIs
	// for sub-cent rounding or floating-point fluctuations while capturing meaningful tariff shifts.
	touScheduleRateThresholdDollars = 0.0101

	// touScheduleTimeThresholdMinutes defines the minimum shift in period start or end time
	// boundary (in minutes) to be considered a significant schedule change.
	// A 15-minute tolerance prevents excessive writes for minor interval adjustments.
	touScheduleTimeThresholdMinutes = 15

	// TOUCoalesceRateThresholdDollars defines the maximum difference in retail/export rate ($/kWh)
	// between adjacent intervals with identical operating modes to be coalesced into a single period,
	// or between rates grouped into the same TOU tariff tier.
	// A threshold of $0.015 (1.5 cents) combines contiguous hours with similar prices (e.g. dynamic hourly
	// markets like ComEd RTP) while preserving distinct tariff pricing steps.
	TOUCoalesceRateThresholdDollars = 0.015
)

// TOUPeriod represents a normalized time-of-use period within a recurring daily 24-hour schedule.
// Clock boundaries are expressed in local wall-clock hours and minutes (from 00:00 to 24:00).
type TOUPeriod struct {
	// StartHour and StartMinute define the inclusive start of the period in local time (0-23 and 0-59).
	StartHour   int `json:"startHour"`
	StartMinute int `json:"startMinute"`

	// EndHour and EndMinute define the exclusive end of the period in local time (0-24 and 0-59).
	// EndHour = 24 with EndMinute = 0 represents midnight at the end of the day.
	EndHour   int `json:"endHour"`
	EndMinute int `json:"endMinute"`

	// ImportDollars is the delivered retail cost of electricity in $/kWh for this period.
	ImportDollars float64 `json:"importDollars"`

	// ExportDollars is the compensation credit in $/kWh for energy exported during this period.
	ExportDollars float64 `json:"exportDollars"`

	// BatteryMode defines the target battery operating mode for this period (e.g. BatteryModeLoad, BatteryModeExport).
	BatteryMode BatteryMode `json:"batteryMode"`

	// SolarMode defines the target solar routing mode for this period (e.g. SolarModeAny, SolarModeExport).
	SolarMode SolarMode `json:"solarMode"`

	// Peak indicates whether this interval is an on-peak period for ESS charging/discharging prioritization.
	Peak bool `json:"peak"`
}

// StartTimeStr returns the start time formatted as "15:04" (e.g. "06:00").
func (p TOUPeriod) StartTimeStr() string {
	return fmt.Sprintf("%02d:%02d", p.StartHour, p.StartMinute)
}

// EndTimeStr returns the end time formatted as "15:04" (e.g. "24:00" for midnight end-of-day).
func (p TOUPeriod) EndTimeStr() string {
	return fmt.Sprintf("%02d:%02d", p.EndHour, p.EndMinute)
}

// DurationMinutes returns the duration of the period in minutes.
func (p TOUPeriod) DurationMinutes() int {
	start := p.StartHour*60 + p.StartMinute
	end := p.EndHour*60 + p.EndMinute
	return end - start
}

// CoversTime returns true if the clock time of t (in local time) falls within [Start, End).
func (p TOUPeriod) CoversTime(t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	start := p.StartHour*60 + p.StartMinute
	end := p.EndHour*60 + p.EndMinute
	return m >= start && m < end
}

// TOUSchedule represents a normalized 24-hour Time-Of-Use schedule spanning 00:00 to 24:00 local time.
type TOUSchedule struct {
	Periods []TOUPeriod `json:"periods"`
}

// IsSignificantlyDifferent checks whether two schedules differ beyond the standard rate and time thresholds.
// It returns true if:
// 1. Either schedule is nil, or period counts differ.
// 2. Any period differs in BatteryMode, SolarMode, or Peak status.
// 3. Any period's ImportDollars or ExportDollars differs by more than TOUScheduleRateThresholdDollars.
// 4. Any period's start or end boundary has shifted by more than TOUScheduleTimeThresholdMinutes.
func (s *TOUSchedule) IsSignificantlyDifferent(other *TOUSchedule) bool {
	if s == nil || other == nil {
		return true
	}
	if len(s.Periods) != len(other.Periods) {
		return true
	}
	for i := range s.Periods {
		p1 := s.Periods[i]
		p2 := other.Periods[i]

		// Operational intent must match identically
		if p1.BatteryMode != p2.BatteryMode || p1.SolarMode != p2.SolarMode || p1.Peak != p2.Peak {
			return true
		}

		// Rate diff check
		if math.Abs(p1.ImportDollars-p2.ImportDollars) > touScheduleRateThresholdDollars ||
			math.Abs(p1.ExportDollars-p2.ExportDollars) > touScheduleRateThresholdDollars {
			return true
		}

		// Time boundary shifts
		p1Start := p1.StartHour*60 + p1.StartMinute
		p2Start := p2.StartHour*60 + p2.StartMinute
		if math.Abs(float64(p1Start-p2Start)) > float64(touScheduleTimeThresholdMinutes) {
			return true
		}

		p1End := p1.EndHour*60 + p1.EndMinute
		p2End := p2.EndHour*60 + p2.EndMinute
		if math.Abs(float64(p1End-p2End)) > float64(touScheduleTimeThresholdMinutes) {
			return true
		}
	}
	return false
}
