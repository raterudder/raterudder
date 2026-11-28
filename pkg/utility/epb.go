package utility

// Tariff Audit Metadata
// - Provider(s): epb (EPB Chattanooga)
// - Official Sources:
//   - https://epb.com/energy/energy-rates/
//   - https://epb.com/energy/energy-rates/residential-energy-rate-schedules/
//   - https://static.epb.com/media/documents/EPB_Base_Rate_Plan_-_Schedule_RS.pdf (Schedule RS, effective October 2024)
//   - https://static.epb.com/media/documents/Time_Shift_Rate_Plan_-_Schedule_TRS.pdf (Schedule TRS, effective October 2024)
//   - https://static.epb.com/media/documents/EPB_Night_Shift_Rate_-_Schedule_NRS.pdf (Schedule NRS, effective October 2024)
//   - https://static.epb.com/media/documents/Monthly_FCA_Rates_-_November.pdf (Monthly Fuel Cost Adjustment effective November 1, 2026)
//   - https://energyright.com/business-industry/energy-programs/business-and-industry-renewables/dispersed-power-production/
//   - https://energyright.com/wp-content/uploads/2026/08/August-2026-DPP-Guidelines-for-Publish.pdf (TVA DPP Schedule CSPP effective August 1, 2026)
//   - https://energyright.com/wp-content/uploads/2026/09/September-2026-DPP-Guidelines-for-Publish.pdf (TVA DPP Schedule CSPP effective September 1, 2026)
//   - https://energyright.com/wp-content/uploads/2026/10/October-2026-DPP-Guidelines-for-Publish.pdf (TVA DPP Schedule CSPP effective October 1, 2026)
//   - https://energyright.com/wp-content/uploads/2026/11/November-2026-DPP-Guidelines-for-Publish.pdf (TVA DPP Schedule CSPP effective November 1, 2026)
// - Effective Window: 2025-01-01 through 2026-11-30 (Oct 2024 base schedules RS/TRS/NRS until superseded; Nov 2026 FCA and TVA DPP Schedule CSPP carried forward through 2028-12-31)
// - Revision Cadence: Monthly (EPB/TVA Total Monthly Fuel Cost & TVA DPP Schedule CSPP) & Annual (Oct 1 TVA/EPB Base Rate Review)
// - Next Audit Due: 2026-12-05 (Check December 2026 Monthly FCA sheet and December 2026 TVA DPP Guidelines Schedule CSPP)
// - Last Verified: 2026-11-21
// - Modeling Notes: Retail import schedules RS ($0.09464/kWh), TRS ($0.17744/$0.08075/kWh, exempting 6 federal holidays), and NRS ($0.10516 Day / $0.06306 Night per kWh) plus monthly FCA (GridAdditional: true) are evaluated in Eastern Time (etLocation); fixed monthly customer charges are excluded; TVA Dispersed Power Production Schedule CSPP export credits (dpp_a flat and dpp_b TOU up to 100,000 kWh/hr) are evaluated in Central Time (ctLocation) and clamped to the Eastern Time horizon boundaries.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftEPBWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getEPBHolidays returns the six federal holidays exempted from On-Peak hours under EPB Schedule TRS:
// New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, and Christmas Day.
func getEPBHolidays(year int) []string {
	holidays := []time.Time{
		shiftEPBWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftEPBWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftEPBWeekendHoliday(christmasDay(year)),
		shiftEPBWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

// Fuel Cost Adjustment (FCA) is published monthly at https://epb.com/energy/energy-rates/residential-energy-rate-schedules/
// (latest sheet: https://static.epb.com/media/documents/Monthly_FCA_Rates_-_November.pdf).
func epbFCAPeriods(years []int) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod
	for _, year := range years {
		if year == 2025 {
			simplified = append(simplified, touSimplifiedPeriod{
				Year:       2025,
				MonthStart: time.January,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH: 0.02900,
						Description:   "EPB Fuel Cost Adjustment",
					},
				},
			})
		} else if year == 2026 {
			simplified = append(simplified, []touSimplifiedPeriod{
				{
					Year:       2026,
					MonthStart: time.January,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.03021,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.June,
					MonthEnd:   time.June,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02793,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.July,
					MonthEnd:   time.July,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02825,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.August,
					MonthEnd:   time.August,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02631,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.September,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02535,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.October,
					MonthEnd:   time.October,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02164,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
				{
					Year:       2026,
					MonthStart: time.November,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.02754,
							Description:   "EPB Fuel Cost Adjustment",
						},
					},
				},
			}...)
		} else {
			// 2027 and later (assumes the latest rate from November 2026)
			simplified = append(simplified, touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.January,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH: 0.02754,
						Description:   "EPB Fuel Cost Adjustment",
					},
				},
			})
		}
	}

	fcaPeriods := buildPeriods(etLocation, simplified)
	for i := range fcaPeriods {
		fcaPeriods[i].GridAdditional = true
	}
	return fcaPeriods
}

func getEPBDPPHolidays(year int) []string {
	return getEPBHolidays(year)
}

func clampEPBDPPPeriodsToETHorizon(periods []types.UtilityFeesPeriod, years []int) []types.UtilityFeesPeriod {
	if len(years) == 0 {
		return nil
	}
	minYear := years[0]
	maxYear := years[0]
	for _, y := range years[1:] {
		if y < minYear {
			minYear = y
		}
		if y > maxYear {
			maxYear = y
		}
	}
	horizonStart := time.Date(minYear, time.January, 1, 0, 0, 0, 0, etLocation).In(ctLocation)
	horizonEnd := time.Date(maxYear+1, time.January, 1, 0, 0, 0, 0, etLocation).In(ctLocation)

	clamped := make([]types.UtilityFeesPeriod, 0, len(periods))
	for _, p := range periods {
		if !p.End.IsZero() && !p.End.After(horizonStart) {
			continue
		}
		if !p.Start.IsZero() && !p.Start.Before(horizonEnd) {
			continue
		}
		if p.Start.IsZero() || p.Start.Before(horizonStart) {
			p.Start = horizonStart
		}
		if p.End.IsZero() || p.End.After(horizonEnd) {
			p.End = horizonEnd
		}
		clamped = append(clamped, p)
	}
	return clamped
}

func buildEPBDPPBPeriod(
	year int,
	startMonth, endMonth time.Month,
	holidays []string,
	superPeakDollarsPerKWH, onPeakDollarsPerKWH, offPeakDollarsPerKWH, superOffPeakDollarsPerKWH float64,
	superPeakHours, onPeakHours, offPeakHours, superOffPeakHours []types.UtilityHourPeriod,
	weekendOffPeakHours, weekendSuperOffPeakHours []types.UtilityHourPeriod,
) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// 1. Weekdays (except holidays)
		{
			Year:                         year,
			MonthStart:                   startMonth,
			MonthEnd:                     endMonth,
			SpecificDates:                holidays,
			SpecificDatesNot:             true,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         superPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: superPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Super Peak (Weekday)",
				},
				{
					Hours:                         onPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: onPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B On-Peak (Weekday)",
				},
				{
					Hours:                         offPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: offPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Off-Peak (Weekday)",
				},
				{
					Hours:                         superOffPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: superOffPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Super Off-Peak (Weekday)",
				},
			},
		},
		// 2. Weekends (Saturdays/Sundays)
		{
			Year:                         year,
			MonthStart:                   startMonth,
			MonthEnd:                     endMonth,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         weekendOffPeakHours,
					Weekend:                       true,
					GenerationCreditDollarsPerKWH: offPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Off-Peak (Weekend)",
				},
				{
					Hours:                         weekendSuperOffPeakHours,
					Weekend:                       true,
					GenerationCreditDollarsPerKWH: superOffPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Super Off-Peak (Weekend)",
				},
			},
		},
		// 3. Holidays (weekdays behaving like weekends)
		{
			Year:                         year,
			MonthStart:                   startMonth,
			MonthEnd:                     endMonth,
			SpecificDates:                holidays,
			SpecificDatesNot:             false,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         weekendOffPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: offPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Off-Peak (Holiday)",
				},
				{
					Hours:                         weekendSuperOffPeakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: superOffPeakDollarsPerKWH,
					Description:                   "EPB DPP Part B Super Off-Peak (Holiday)",
				},
			},
		},
	}
}

func epbDPPAPeriods(years []int) []types.UtilityFeesPeriod {
	if len(years) == 0 {
		return nil
	}

	minYear := years[0]
	for _, y := range years[1:] {
		if y < minYear {
			minYear = y
		}
	}
	prevDecDollarsPerKWH := 0.03373
	if minYear <= 2026 {
		prevDecDollarsPerKWH = 0.02931
	}

	simplified := []touSimplifiedPeriod{
		{
			Year:                               minYear - 1,
			MonthStart:                         time.December,
			MonthEnd:                           time.December,
			OtherGenerationCreditDollarsPerKWH: prevDecDollarsPerKWH,
			OnlySeparateGenerationCredit:       true,
			OtherDescription:                   "EPB DPP Part A Export Credit",
		},
	}

	for _, year := range years {
		if year == 2025 {
			simplified = append(simplified, touSimplifiedPeriod{
				Year:                               2025,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				OtherGenerationCreditDollarsPerKWH: 0.02931,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "EPB DPP Part A Export Credit",
			})
		} else if year == 2026 {
			simplified = append(simplified, []touSimplifiedPeriod{
				{
					Year:                               2026,
					MonthStart:                         time.January,
					MonthEnd:                           time.May,
					OtherGenerationCreditDollarsPerKWH: 0.02931,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.June,
					MonthEnd:                           time.June,
					OtherGenerationCreditDollarsPerKWH: 0.02930,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.July,
					MonthEnd:                           time.July,
					OtherGenerationCreditDollarsPerKWH: 0.03534,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.August,
					MonthEnd:                           time.August,
					OtherGenerationCreditDollarsPerKWH: 0.03316,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.September,
					MonthEnd:                           time.September,
					OtherGenerationCreditDollarsPerKWH: 0.02677,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.October,
					MonthEnd:                           time.October,
					OtherGenerationCreditDollarsPerKWH: 0.03112,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
				{
					Year:                               2026,
					MonthStart:                         time.November,
					MonthEnd:                           time.December,
					OtherGenerationCreditDollarsPerKWH: 0.03373,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "EPB DPP Part A Export Credit",
				},
			}...)
		} else {
			// 2027 and later (assumes the latest rate from November 2026)
			simplified = append(simplified, touSimplifiedPeriod{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				OtherGenerationCreditDollarsPerKWH: 0.03373,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "EPB DPP Part A Export Credit",
			})
		}
	}
	return clampEPBDPPPeriodsToETHorizon(buildPeriods(ctLocation, simplified), years)
}

func epbDPPBPeriods(years []int) []types.UtilityFeesPeriod {
	if len(years) == 0 {
		return nil
	}

	minYear := years[0]
	for _, y := range years[1:] {
		if y < minYear {
			minYear = y
		}
	}

	var simplified []touSimplifiedPeriod
	prevYear := minYear - 1
	prevHolidays := getEPBDPPHolidays(prevYear)
	if minYear <= 2026 {
		simplified = append(simplified, buildEPBDPPBPeriod(
			prevYear, time.December, time.December, prevHolidays,
			0.02978, 0.02971, 0.02910, 0.02523,
			[]types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}},
			[]types.UtilityHourPeriod{{HourStart: 12, HourEnd: 14}, {HourStart: 18, HourEnd: 21}},
			[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 21, HourEnd: 23}},
			[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 23, HourEnd: 24}},
			[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 22}},
			[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 22, HourEnd: 24}},
		)...)
	} else {
		simplified = append(simplified, buildEPBDPPBPeriod(
			prevYear, time.December, time.December, prevHolidays,
			0.03571, 0.03400, 0.03364, 0.03202,
			[]types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
			[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 12}, {HourStart: 16, HourEnd: 17}, {HourStart: 21, HourEnd: 22}},
			[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 6}, {HourStart: 12, HourEnd: 16}, {HourStart: 22, HourEnd: 23}},
			[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}, {HourStart: 23, HourEnd: 24}},
			[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 12}, {HourStart: 16, HourEnd: 22}},
			[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}, {HourStart: 12, HourEnd: 16}, {HourStart: 22, HourEnd: 24}},
		)...)
	}

	for _, year := range years {
		holidays := getEPBDPPHolidays(year)

		if year == 2025 {
			// fallback to May 2026
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.January, time.December, holidays,
				0.02978, 0.02971, 0.02910, 0.02523,
				[]types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 12, HourEnd: 14}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 22, HourEnd: 24}},
			)...)
		} else if year == 2026 {
			// Jan - May (fallback to May 2026)
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.January, time.May, holidays,
				0.02978, 0.02971, 0.02910, 0.02523,
				[]types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 12, HourEnd: 14}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 22, HourEnd: 24}},
			)...)
			// June
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.June, time.June, holidays,
				0.03754, 0.03267, 0.02799, 0.02248,
				[]types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 13}, {HourStart: 19, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 11}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 10, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 22, HourEnd: 24}},
			)...)
			// July
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.July, time.July, holidays,
				0.04049, 0.03962, 0.03424, 0.02947,
				[]types.UtilityHourPeriod{{HourStart: 13, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 13}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 11}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 9}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 10, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 22, HourEnd: 24}},
			)...)
			// August
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.August, time.August, holidays,
				0.04096, 0.03790, 0.03159, 0.02795,
				[]types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 14}, {HourStart: 19, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 4, HourEnd: 9}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 4}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 22, HourEnd: 24}},
			)...)
			// September
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.September, time.September, holidays,
				0.03211, 0.02996, 0.02647, 0.02290,
				[]types.UtilityHourPeriod{{HourStart: 13, HourEnd: 18}},
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 13}, {HourStart: 18, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 11}, {HourStart: 21, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 10, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 22, HourEnd: 24}},
			)...)
			// October
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.October, time.October, holidays,
				0.03221, 0.03133, 0.03096, 0.02913,
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 21, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 22, HourEnd: 24}},
			)...)
			// November - December
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.November, time.December, holidays,
				0.03571, 0.03400, 0.03364, 0.03202,
				[]types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 12}, {HourStart: 16, HourEnd: 17}, {HourStart: 21, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 6}, {HourStart: 12, HourEnd: 16}, {HourStart: 22, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 12}, {HourStart: 16, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}, {HourStart: 12, HourEnd: 16}, {HourStart: 22, HourEnd: 24}},
			)...)
		} else {
			// 2027 and later: seasonal winter (November - March, from Nov 2026 CSPP)
			// and summer (April - October, from Oct 2026 CSPP) Central Time windows.
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.November, time.March, holidays,
				0.03571, 0.03400, 0.03364, 0.03202,
				[]types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 9, HourEnd: 12}, {HourStart: 16, HourEnd: 17}, {HourStart: 21, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 6}, {HourStart: 12, HourEnd: 16}, {HourStart: 22, HourEnd: 23}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}, {HourStart: 23, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 12}, {HourStart: 16, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}, {HourStart: 12, HourEnd: 16}, {HourStart: 22, HourEnd: 24}},
			)...)
			simplified = append(simplified, buildEPBDPPBPeriod(
				year, time.April, time.October, holidays,
				0.03221, 0.03133, 0.03096, 0.02913,
				[]types.UtilityHourPeriod{{HourStart: 11, HourEnd: 21}},
				[]types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 21, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 5, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
				[]types.UtilityHourPeriod{{HourStart: 8, HourEnd: 22}},
				[]types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}, {HourStart: 22, HourEnd: 24}},
			)...)
		}
	}
	return clampEPBDPPPeriodsToETHorizon(buildPeriods(ctLocation, simplified), years)
}

func epbPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getEPBHolidays(year)

		if plan == "epb_base" {
			simplified := []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.09464,
							Description:   "EPB Base Rate Plan",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "epb_time_shift" {
			simplified := []touSimplifiedPeriod{
				// Summer (April - October) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.April,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.17744,
							Description:   "EPB Time Shift Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.08075,
					OtherDescription:   "EPB Time Shift Summer Off-Peak",
				},
				// Summer (April - October) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.April,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.08075,
					OtherDescription:   "EPB Time Shift Summer Off-Peak",
				},
				// Non-Summer (November - March) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.March,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 10}},
							Weekday:       true,
							DollarsPerKWH: 0.17744,
							Description:   "EPB Time Shift Non-Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.08075,
					OtherDescription:   "EPB Time Shift Non-Summer Off-Peak",
				},
				// Non-Summer (November - March) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.March,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.08075,
					OtherDescription:   "EPB Time Shift Non-Summer Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "epb_night_shift" {
			simplified := []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 23, HourEnd: 24}, {HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.06306,
							Description:   "EPB Night Shift Night Rate",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.10516,
					OtherDescription:   "EPB Night Shift Day Rate",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		}
	}

	periods = append(periods, epbFCAPeriods(years)...)

	// Solar Export Credits
	if opts.NetMeteringScheme == "dpp_a" || opts.NetMeteringScheme == "" {
		periods = append(periods, epbDPPAPeriods(years)...)
	} else if opts.NetMeteringScheme == "dpp_b" {
		periods = append(periods, epbDPPBPeriods(years)...)
	}

	return periods
}

func epbUtilityInfo() types.UtilityProviderInfo {
	epbOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar billing plan or export credit program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "dpp_a", Name: "Dispersed Power Production (DPP) Part A - Flat Rate"},
				{Value: "dpp_b", Name: "Dispersed Power Production (DPP) Part B - Time-of-Use"},
				{Value: "net", Name: "Standard Net Metering (1:1)"},
			},
			Default: "dpp_a",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "epb",
		Name: "EPB Chattanooga",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "epb_base",
				Name:    "Base Rate Plan",
				Options: epbOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return epbPeriods("epb_base", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "epb_time_shift",
				Name:    "Time Shift Plan",
				Options: epbOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return epbPeriods("epb_time_shift", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "epb_night_shift",
				Name:    "Night Shift Plan",
				Options: epbOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return epbPeriods("epb_night_shift", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
		},
	}
}
