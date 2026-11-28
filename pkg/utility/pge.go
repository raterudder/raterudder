package utility

// Tariff Audit Metadata
// - Provider(s): portland_general_electric (Portland General Electric (PGE))
// - Official Sources:
//   - https://portlandgeneral.com/about/info/rates-and-regulatory/tariff
//   - https://assets.ctfassets.net/416ywc1laqmd/6RgTNk5RU1bldl0LdPpIY9/b74c37e10cf7d581750b620c0cdeb8e6/Sched_007.pdf (P.U.C. Oregon No. E-19 3rd Rev. Sheets 7-1 & 7-2, Residential Service & Time-of-Day)
//   - https://assets.ctfassets.net/416ywc1laqmd/Lv0QYI9KQYsRZoiR1HVBh/8cd005d140448a0dd6651d674cb06d96/Sched_100.pdf (4th Rev. Sheet No. 100-1 & 3rd Rev. Sheet No. 100-2, Summary of Applicable Adjustments)
//   - https://assets.ctfassets.net/416ywc1laqmd/3iIZy9k8dCDMillMpvNW0B/ef058b2f1a32a4dd05fef0c7f05f878e/Sched_108.pdf (Original Sheet No. 108-1, 1.5% Public Purpose Charge)
//   - https://assets.ctfassets.net/416ywc1laqmd/7tSwvDJlcYTZOyVV9Piyj6/7daac4d256c04be84c5fd062f07ae529/2026-10-15-standard-service-schedules.pdf & https://assets.ctfassets.net/416ywc1laqmd/2ISvS7QQL5aBsJCE8qSI1a/76ff23a0cc88e960eb0205ef2f200300/2026-10-31-standard-service-schedules.pdf (Standard Service Price Summaries eff. Oct 15 & Oct 31, 2026)
//   - https://assets.ctfassets.net/416ywc1laqmd/1XYym4ps9BhGGK3yLX5ggI/52c9a500c2f2e11994040b58585f8f32/PGE_Advice_No_26-45_Annual_Update_Tariff_OL_11_13_26.pdf & https://apps.puc.state.or.us/edockets/orders.asp?OrderNumber=26-393 (Advice No. 26-45 / OPUC Order No. 26-393 in Docket UE 465, 4th Rev. Sheets 7-1/7-2 & 5th Rev. Sheet 125-3 eff. Jan 1, 2027)
//   - https://assets.ctfassets.net/416ywc1laqmd/6VdcgRq1GjVQIaOdRGuhm3/af780118ef83c16f4e3967c46fba1093/Sched_203.pdf (Original Sheets 203-1 to 203-3, Net Metering Service)
// - Effective Window: 2025-01-01 through 2027-12-31 (Advice No. 26-45 / Order No. 26-393 effective 2027-01-01 until superseded)
// - Revision Cadence: Annual / Periodic (January 1 Annual Power Cost Update Tariff under Schedule 125; periodic Schedule 100 rider updates)
// - Next Audit Due: 2027-04-01 (Check Spring 2027 OPUC P.U.C. Oregon No. E-19 Schedule 100 adjustment filings)
// - Last Verified: 2026-11-26
// - Modeling Notes: Residential Service (Schedule 7) models Tier 1 volumetric energy charges (<=2,000 kWh/month); excludes fixed monthly Basic Charge, Schedule 115, and Schedule 118 fees; applies Schedule 100 riders (+2.580c/kWh plus Schedule 125 AUT without <1% de minimis splits) and 1.5% Schedule 108 Public Purpose Charge (excluding Schedule 109 $0.01004/kWh) from 2026-10-15 onward while preserving pre-2026-10-15 windows unchanged; 1:1 kWh net metering under Schedule 203.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	pgeSched109DollarsPerKWH = 1.004 / 100.0 // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh, excluded from Schedule 108 PPC)
	pgeSched108PPCRate       = 0.015         // Schedule 108: Public Purpose Charge (1.5%)
)

// getPGEAdjustments returns the total Schedule 100 adjustment rate (in $/kWh, before Schedule 108 PPC)
// for a given date/time and Schedule 7 period ("Default", "On-Peak", "Mid-Peak", "Off-Peak").
func getPGEAdjustments(t time.Time, periodName string) float64 {
	t = t.In(ptLocation)

	// January 1, 2027 onwards (Advice No. 26-45 / OPUC Order No. 26-393 in Docket UE 465, 5th Rev. Sheet No. 125-3)
	jan12027 := time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(jan12027) {
		var sched125DollarsPerKWH float64
		switch periodName {
		case "Default":
			sched125DollarsPerKWH = 5.031 / 100.0 // Schedule 125 (AUT) Default: 5.031 ¢/kWh
		case "On-Peak":
			sched125DollarsPerKWH = 11.842 / 100.0 // Schedule 125 (AUT) On-Peak: 11.842 ¢/kWh
		case "Mid-Peak":
			sched125DollarsPerKWH = 4.975 / 100.0 // Schedule 125 (AUT) Mid-Peak: 4.975 ¢/kWh
		default:
			sched125DollarsPerKWH = 2.960 / 100.0 // Schedule 125 (AUT) Off-Peak: 2.960 ¢/kWh
		}

		// Schedule 100 mandatory adjustments (Schedules 102–153, excluding Schedule 125): +2.580 ¢/kWh
		return (2.580 / 100.0) + sched125DollarsPerKWH
	}

	// October 15, 2026 to December 31, 2026 (2026-10-15 & 2026-10-31 Standard Service Price Summaries, Schedules 102–153)
	oct152026 := time.Date(2026, time.October, 15, 0, 0, 0, 0, ptLocation)
	if !t.Before(oct152026) {
		var sched125DollarsPerKWH float64
		switch periodName {
		case "Default":
			sched125DollarsPerKWH = 5.619 / 100.0 // Schedule 125 (AUT) Default: 5.619 ¢/kWh
		case "On-Peak":
			sched125DollarsPerKWH = 12.868 / 100.0 // Schedule 125 (AUT) On-Peak: 12.868 ¢/kWh
		case "Mid-Peak":
			sched125DollarsPerKWH = 5.555 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.555 ¢/kWh
		default:
			sched125DollarsPerKWH = 3.416 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.416 ¢/kWh
		}

		// Schedule 100 mandatory adjustments (Schedules 102–153, excluding Schedule 125): +2.580 ¢/kWh
		return (2.580 / 100.0) + sched125DollarsPerKWH
	}

	// July 8, 2026 to October 14, 2026 (Advice No. 26-24 / Advice No. 26-17 / Advice No. 26-21)
	july82026 := time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation)
	if !t.Before(july82026) {
		var sched125DollarsPerKWH float64
		switch periodName {
		case "Default":
			sched125DollarsPerKWH = 5.619 / 100.0 // Schedule 125 (AUT) Default: 5.619 ¢/kWh
		case "On-Peak":
			sched125DollarsPerKWH = 12.868 / 100.0 // Schedule 125 (AUT) On-Peak: 12.868 ¢/kWh
		case "Mid-Peak":
			sched125DollarsPerKWH = 5.555 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.555 ¢/kWh
		default:
			sched125DollarsPerKWH = 3.416 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.416 ¢/kWh
		}

		return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
			(0.123 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.123 ¢/kWh)
			pgeSched109DollarsPerKWH + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
			(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
			(0.401 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.401 ¢/kWh)
			(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
			(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
			sched125DollarsPerKWH + // Schedule 125: Annual Power Cost Update (AUT)
			(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
	}

	// May 1, 2026 to July 7, 2026 (Advice No. 26-21 updated Schedule 105 Part A to 0.123 ¢/kWh effective May 1, 2026)
	may12026 := time.Date(2026, time.May, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(may12026) {
		var sched125DollarsPerKWH float64
		switch periodName {
		case "Default":
			sched125DollarsPerKWH = 5.685 / 100.0 // Schedule 125 (AUT) Default: 5.685 ¢/kWh
		case "On-Peak":
			sched125DollarsPerKWH = 13.019 / 100.0 // Schedule 125 (AUT) On-Peak: 13.019 ¢/kWh
		case "Mid-Peak":
			sched125DollarsPerKWH = 5.620 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.620 ¢/kWh
		default:
			sched125DollarsPerKWH = 3.456 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.456 ¢/kWh
		}

		return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
			(0.123 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.123 ¢/kWh)
			pgeSched109DollarsPerKWH + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
			(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
			(0.410 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.410 ¢/kWh)
			(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
			(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
			sched125DollarsPerKWH + // Schedule 125: Annual Power Cost Update (AUT)
			(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
	}

	// April 1, 2026 to April 30, 2026 (Advice No. 26-13 / Advice No. 26-06 / Advice No. 26-17)
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(april12026) {
		var sched125DollarsPerKWH float64
		switch periodName {
		case "Default":
			sched125DollarsPerKWH = 5.685 / 100.0 // Schedule 125 (AUT) Default: 5.685 ¢/kWh
		case "On-Peak":
			sched125DollarsPerKWH = 13.019 / 100.0 // Schedule 125 (AUT) On-Peak: 13.019 ¢/kWh
		case "Mid-Peak":
			sched125DollarsPerKWH = 5.620 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.620 ¢/kWh
		default:
			sched125DollarsPerKWH = 3.456 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.456 ¢/kWh
		}

		return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
			(0.125 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.125 ¢/kWh)
			pgeSched109DollarsPerKWH + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
			(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
			(0.410 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.410 ¢/kWh)
			(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
			(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
			sched125DollarsPerKWH + // Schedule 125: Annual Power Cost Update (AUT)
			(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
	}

	// January 1, 2025 to March 31, 2026 (Advice No. 24-39, effective January 1, 2025)
	var sched125DollarsPerKWH float64
	switch periodName {
	case "Default":
		sched125DollarsPerKWH = 5.788 / 100.0 // Schedule 125 (AUT) Default: 5.788 ¢/kWh
	case "On-Peak":
		sched125DollarsPerKWH = 13.255 / 100.0 // Schedule 125 (AUT) On-Peak: 13.255 ¢/kWh
	case "Mid-Peak":
		sched125DollarsPerKWH = 5.722 / 100.0 // Schedule 125 (AUT) Mid-Peak: 5.722 ¢/kWh
	default:
		sched125DollarsPerKWH = 3.519 / 100.0 // Schedule 125 (AUT) Off-Peak: 3.519 ¢/kWh
	}

	return (-1.112 / 100.0) + // Schedule 102: Federal Columbia River Benefits (BPA Credit) (-1.112 ¢/kWh)
		(0.125 / 100.0) + // Schedule 105: Miscellaneous Regulatory Adjustments (0.125 ¢/kWh)
		pgeSched109DollarsPerKWH + // Schedule 109: Energy Efficiency Funding Adjustment (1.004 ¢/kWh)
		(0.230 / 100.0) + // Schedule 120: Seaside Battery Storage Resource Recovery (0.230 ¢/kWh)
		(0.410 / 100.0) + // Schedule 121: Distribution System Plan Alternative Recovery (0.410 ¢/kWh)
		(0.214 / 100.0) + // Schedule 122: Renewable Resources Automatic Adjustment (0.214 ¢/kWh)
		(0.0 / 100.0) + // Schedule 123: Decoupling Adjustment ($0.00)
		sched125DollarsPerKWH + // Schedule 125: Annual Power Cost Update (AUT)
		(0.214 / 100.0) // Schedule 126: Annual Power Cost Variance Mechanism (0.214 ¢/kWh)
}

// applyPGEPublicPurposeCharge applies the 1.5% Schedule 108 Public Purpose Charge (PPC)
// from October 15, 2026 onward to volumetric totals excluding Schedule 109 (1.004 ¢/kWh).
// Windows prior to October 15, 2026 remain unchanged to preserve historical determinism.
func applyPGEPublicPurposeCharge(t time.Time, subtotalDollarsPerKWH float64) float64 {
	t = t.In(ptLocation)
	oct152026 := time.Date(2026, time.October, 15, 0, 0, 0, 0, ptLocation)
	if t.Before(oct152026) {
		return subtotalDollarsPerKWH
	}
	return subtotalDollarsPerKWH + (subtotalDollarsPerKWH-pgeSched109DollarsPerKWH)*pgeSched108PPCRate
}

type pgeBaseRates struct {
	DefaultDollarsPerKWH float64
	OnPeakDollarsPerKWH  float64
	MidPeakDollarsPerKWH float64
	OffPeakDollarsPerKWH float64
}

func getPGEBaseRates(t time.Time) pgeBaseRates {
	t = t.In(ptLocation)
	jan12027 := time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(jan12027) {
		return pgeBaseRates{
			DefaultDollarsPerKWH: 11.270 / 100.0, // 11.270 ¢/kWh (Sheets 7-1 & 7-2 Fourth Revision, Advice No. 26-45, Jan 1, 2027)
			OnPeakDollarsPerKWH:  30.213 / 100.0, // 30.213 ¢/kWh
			MidPeakDollarsPerKWH: 11.123 / 100.0, // 11.123 ¢/kWh
			OffPeakDollarsPerKWH: 5.504 / 100.0,  // 5.504 ¢/kWh
		}
	}
	july82026 := time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation)
	if !t.Before(july82026) {
		return pgeBaseRates{
			DefaultDollarsPerKWH: 11.289 / 100.0, // 11.289 ¢/kWh (Sheets 7-1 & 7-2 Third Revision, July 8, 2026)
			OnPeakDollarsPerKWH:  30.263 / 100.0, // 30.263 ¢/kWh
			MidPeakDollarsPerKWH: 11.143 / 100.0, // 11.143 ¢/kWh
			OffPeakDollarsPerKWH: 5.514 / 100.0,  // 5.514 ¢/kWh
		}
	}
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(april12026) {
		return pgeBaseRates{
			DefaultDollarsPerKWH: 11.411 / 100.0, // 11.411 ¢/kWh (Sheets 7-1 & 7-2 Second Revision, April 1, 2026)
			OnPeakDollarsPerKWH:  30.629 / 100.0, // 30.629 ¢/kWh
			MidPeakDollarsPerKWH: 11.265 / 100.0, // 11.265 ¢/kWh
			OffPeakDollarsPerKWH: 5.558 / 100.0,  // 5.558 ¢/kWh
		}
	}
	// January 1, 2025 to March 31, 2026 (Sheets 7-1 & 7-2 First Revision, January 1, 2025)
	return pgeBaseRates{
		DefaultDollarsPerKWH: 11.416 / 100.0, // 11.416 ¢/kWh
		OnPeakDollarsPerKWH:  30.634 / 100.0, // 30.634 ¢/kWh
		MidPeakDollarsPerKWH: 11.270 / 100.0, // 11.270 ¢/kWh
		OffPeakDollarsPerKWH: 5.563 / 100.0,  // 5.563 ¢/kWh
	}
}

func shiftPGEWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getPortlandGeneralHolidays(year int) []string {
	holidays := []time.Time{
		shiftPGEWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftPGEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftPGEWeekendHoliday(christmasDay(year)),
		shiftPGEWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

type pgeSubPeriod struct {
	Start time.Time
	End   time.Time
}

func getPGESubPeriods(year int) []pgeSubPeriod {
	if year == 2026 {
		return []pgeSubPeriod{
			{
				Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.May, 1, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.May, 1, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.July, 8, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2026, time.October, 15, 0, 0, 0, 0, ptLocation),
			},
			{
				Start: time.Date(2026, time.October, 15, 0, 0, 0, 0, ptLocation),
				End:   time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation),
			},
		}
	}
	return []pgeSubPeriod{
		{
			Start: time.Date(year, time.January, 1, 0, 0, 0, 0, ptLocation),
			End:   time.Date(year+1, time.January, 1, 0, 0, 0, 0, ptLocation),
		},
	}
}

func portlandGeneralPeriods(plan string, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		holidays := getPortlandGeneralHolidays(year)

		for _, sub := range getPGESubPeriods(year) {
			sampleDate := sub.Start.Add(12 * time.Hour)
			base := getPGEBaseRates(sampleDate)

			if plan == "portland_general_electric_residential" {
				defaultTotalDollarsPerKWH := applyPGEPublicPurposeCharge(
					sampleDate,
					base.DefaultDollarsPerKWH+getPGEAdjustments(sampleDate, "Default"),
				)
				flatPeriod := touSimplifiedPeriod{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: defaultTotalDollarsPerKWH,
					OtherDescription:   "Residential Service (Schedule 7)",
				}
				subPeriods := buildPeriods(ptLocation, []touSimplifiedPeriod{flatPeriod})
				for i := range subPeriods {
					subPeriods[i].Start = sub.Start
					subPeriods[i].End = sub.End
				}
				periods = append(periods, subPeriods...)
				continue
			}

			onPeakTotalDollarsPerKWH := applyPGEPublicPurposeCharge(
				sampleDate,
				base.OnPeakDollarsPerKWH+getPGEAdjustments(sampleDate, "On-Peak"),
			)
			midPeakTotalDollarsPerKWH := applyPGEPublicPurposeCharge(
				sampleDate,
				base.MidPeakDollarsPerKWH+getPGEAdjustments(sampleDate, "Mid-Peak"),
			)
			offPeakTotalDollarsPerKWH := applyPGEPublicPurposeCharge(
				sampleDate,
				base.OffPeakDollarsPerKWH+getPGEAdjustments(sampleDate, "Off-Peak"),
			)

			// 1. Holiday period: applies all day on the specific holiday dates
			holidayPeriod := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.January,
				MonthEnd:      time.December,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "Off-Peak",
						DollarsPerKWH: offPeakTotalDollarsPerKWH,
						Description:   "Off-Peak (Holiday)",
					},
				},
			}

			// 2. Regular periods: apply on non-holiday dates
			regularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						// On-Peak: 5-9 p.m. Monday-Friday (17:00 to 21:00)
						Hours: []types.UtilityHourPeriod{
							{HourStart: 17, HourEnd: 21},
						},
						Weekday:       true,
						DollarsPerKWH: onPeakTotalDollarsPerKWH,
						Description:   "On-Peak",
					},
					{
						Name: "Mid-Peak",
						// Mid-Peak: 7 a.m. to 5 p.m. Monday-Friday (07:00 to 17:00)
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 17},
						},
						Weekday:       true,
						DollarsPerKWH: midPeakTotalDollarsPerKWH,
						Description:   "Mid-Peak",
					},
					{
						Name: "Off-Peak",
						// Weekend: Saturday and Sunday all-day Off-Peak
						Weekend:       true,
						DollarsPerKWH: offPeakTotalDollarsPerKWH,
						Description:   "Off-Peak (Weekend)",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakTotalDollarsPerKWH,
				OtherDescription:   "Off-Peak",
			}

			subPeriods := buildPeriods(ptLocation, []touSimplifiedPeriod{holidayPeriod, regularPeriod})
			for i := range subPeriods {
				subPeriods[i].Start = sub.Start
				subPeriods[i].End = sub.End
			}
			periods = append(periods, subPeriods...)
		}
	}
	return periods
}

// pgeUtilityInfo returns the utility info for Portland General Electric
func pgeUtilityInfo() types.UtilityProviderInfo {
	pgeOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Enable if you are enrolled in net metering. PGE net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "portland_general_electric",
		Name: "Portland General Electric (PGE)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "portland_general_electric_residential",
				Name:    "Residential Service (Schedule 7)",
				Options: pgeOptions,
				GetFees: getStaticGetFees(
					portlandGeneralPeriods("portland_general_electric_residential", []int{2025, 2026, 2027}),
				),
			},
			{
				ID:      "portland_general_electric_tod",
				Name:    "Time of Day",
				Options: pgeOptions,
				GetFees: getStaticGetFees(
					portlandGeneralPeriods("portland_general_electric_tod", []int{2025, 2026, 2027}),
				),
			},
		},
	}
}
