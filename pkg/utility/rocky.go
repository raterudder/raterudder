package utility

// Tariff Audit Metadata
// - Provider(s): rocky_mountain_power (Rocky Mountain Power (RMP))
// - Official Sources:
//   - Utah Price Summary (eff. 2026-09-25): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/utah/Utah_Price_Summary.pdf
//   - Utah P.S.C.U. No. 52 Schedule 1 (3rd Rev. Sheet No. 1.2, Docket No. 26-035-T08, eff. 2026-08-10): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/utah/rates/001_Residential_Service.pdf
//   - Utah Schedule 80 (1st Rev. Sheet No. 80, eff. 2026-08-10) & Schedule 92 (1st Rev. Sheet No. 92, eff. 2026-09-25 through 2026-12-31): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/utah/rates/080_Summary_of_Effective_Rate_Adjustments.pdf; https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/utah/rates/092_GRC_Settlement_Deferral_Adjustment.pdf
//   - Utah Schedule 136 (Orig. Sheet No. 136.3, Docket No. 24-035-04, $0.0920/kWh through 2032-12-31): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/utah/rates/136_Transition_Program_for_Customer_Generators.pdf
//   - Idaho Price Summary (eff. 2026-06-01): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/idaho/ID_Price_Summary.pdf
//   - Idaho I.P.U.C. No. 1 Schedule 1 (13th Rev. Sheet No. 1.1, Case No. PAC-E-24-04) & Schedule 36 (15th Rev. Sheet No. 36.2, Case No. PAC-E-24-04; adoption in Case No. PAC-E-26-05 Exh. 3): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/idaho/rates/001_Residential_Service.pdf; https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/idaho/rates/036_Optional_Time_of_Day_Residential_Service.pdf
//   - Wyoming P.S.C. No. 19 Price Summary (6th Rev. Sheet No. PS-1, Dockets 20000-671-ER-24 & 20000-707-EM-26, eff. 2026-07-01) & Schedule 19 (1st Rev. Sheet No. 19-1 & Orig. Sheet No. 19-2): https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/wyoming/price-summary/Wyoming_Price_Summary.pdf; https://www.rockymountainpower.net/content/dam/pcorp/documents/en/rockymountainpower/rates-regulation/wyoming/rates/019_Residential_Service_Time-of-Use_Pilot.pdf
// - Effective Window: 2026-01-01 through 2027-12-31
// - Revision Cadence: Annual / Semi-Annual (UT Jan 1 Schedule 92 expiration & Jul/Aug EBA/GRC adjustments; ID Jun 1 rate steps & ECAM; WY Jan 1 & Jul 1 ECAM/REC riders)
// - Next Audit Due: 2027-01-05 (Verify Utah Schedule 92 expiration after 2026-12-31 and Jan 1, 2027 UT/WY rider filings)
// - Last Verified: 2026-11-24
// - Modeling Notes: Models Tier 1 volumetric energy charges (first 400 kWh in UT Schedule 1; first 700 kWh summer / 1,000 kWh winter in ID Schedule 1) with combined mandatory Schedule 80 (UT), Schedules 92/94/98/191/34 (ID), and Schedules 95/93/191/198 (WY) riders; excludes fixed monthly customer/metering fees, <1% or <$0.0025/kWh de minimis rider splits (including ID Schedule 97 at $0.000923/kWh), and unapproved 2027 filings; models UT exports under Schedules 135 (`net`), 136 (`schedule_136`, $0.0920/kWh), and 137 (`net_billing`, $0.04855/$0.04033/kWh), ID exports under Schedules 135 (`net`) and 136 (`net_billing`), and WY exports under Schedule 135 (`net`).

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Utah Schedule 80 combined rider multipliers (Schedules 94, 98, 193, 198, plus Schedule 92 from 2026-09-25 to 2026-12-31).
	rockyUTJanJun2026Multiplier     = 1.26486561
	rockyUTBaseSchedule80Multiplier = 1.1139471
	rockyUTSepDec2026Multiplier     = 1.1317471

	// Utah solar export credit rates ($/kWh).
	rockyUTSchedule136ExportDollarsPerKWH       = 0.0920
	rockyUTSchedule137SummerExportDollarsPerKWH = 0.04855
	rockyUTSchedule137WinterExportDollarsPerKWH = 0.04033
)

func getRockyHolidays(year int) []string {
	shiftRockyHoliday := func(t time.Time) time.Time {
		switch t.Weekday() {
		case time.Saturday:
			return t.AddDate(0, 0, -1)
		case time.Sunday:
			return t.AddDate(0, 0, 1)
		default:
			return t
		}
	}

	holidays := []time.Time{
		shiftRockyHoliday(newYearsDay(year)),
		shiftRockyHoliday(presidentsDay(year)),
		memorialDay(year),
		shiftRockyHoliday(independenceDay(year)),
		shiftRockyHoliday(pioneerDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftRockyHoliday(christmasDay(year)),
		shiftRockyHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func clipRockyPeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
	var clipped []types.UtilityFeesPeriod
	for _, p := range periods {
		if !windowStart.IsZero() && (p.Start.IsZero() || p.Start.Before(windowStart)) {
			p.Start = windowStart
		}
		if !windowEnd.IsZero() && (p.End.IsZero() || p.End.After(windowEnd)) {
			p.End = windowEnd
		}
		if p.End.After(p.Start) {
			clipped = append(clipped, p)
		}
	}
	return clipped
}

func rockyUtahExportCredits(scheme string) (separate bool, summerDollarsPerKWH, winterDollarsPerKWH float64) {
	switch scheme {
	case "net":
		return false, 0, 0
	case "schedule_136":
		return true, rockyUTSchedule136ExportDollarsPerKWH, rockyUTSchedule136ExportDollarsPerKWH
	default:
		// Default "net_billing" (Schedule 137)
		return true, rockyUTSchedule137SummerExportDollarsPerKWH, rockyUTSchedule137WinterExportDollarsPerKWH
	}
}

type rockyUtahWindow struct {
	start                             time.Time
	end                               time.Time
	flatSummerBaseDollarsPerKWH       float64
	flatWinterBaseDollarsPerKWH       float64
	touSummerPeakBaseDollarsPerKWH    float64
	touSummerOffPeakBaseDollarsPerKWH float64
	touWinterPeakBaseDollarsPerKWH    float64
	touWinterOffPeakBaseDollarsPerKWH float64
	schedule80Multiplier              float64
}

func rockyUtahWindowsForYear(year int) []rockyUtahWindow {
	if year == 2026 {
		jan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, mtLocation)
		jul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, mtLocation)
		aug10 := time.Date(2026, time.August, 10, 0, 0, 0, 0, mtLocation)
		sep25 := time.Date(2026, time.September, 25, 0, 0, 0, 0, mtLocation)
		nextJan1 := time.Date(2027, time.January, 1, 0, 0, 0, 0, mtLocation)

		return []rockyUtahWindow{
			{
				start:                             jan1,
				end:                               jul1,
				flatSummerBaseDollarsPerKWH:       0.093199,
				flatWinterBaseDollarsPerKWH:       0.082477,
				touSummerPeakBaseDollarsPerKWH:    0.320834,
				touSummerOffPeakBaseDollarsPerKWH: 0.071296,
				touWinterPeakBaseDollarsPerKWH:    0.283924,
				touWinterOffPeakBaseDollarsPerKWH: 0.063094,
				schedule80Multiplier:              rockyUTJanJun2026Multiplier,
			},
			{
				start:                             jul1,
				end:                               aug10,
				flatSummerBaseDollarsPerKWH:       0.093199,
				flatWinterBaseDollarsPerKWH:       0.082477,
				touSummerPeakBaseDollarsPerKWH:    0.320834,
				touSummerOffPeakBaseDollarsPerKWH: 0.071296,
				touWinterPeakBaseDollarsPerKWH:    0.283924,
				touWinterOffPeakBaseDollarsPerKWH: 0.063094,
				schedule80Multiplier:              rockyUTBaseSchedule80Multiplier,
			},
			{
				start:                             aug10,
				end:                               sep25,
				flatSummerBaseDollarsPerKWH:       0.098332,
				flatWinterBaseDollarsPerKWH:       0.087020,
				touSummerPeakBaseDollarsPerKWH:    0.336173,
				touSummerOffPeakBaseDollarsPerKWH: 0.074705,
				touWinterPeakBaseDollarsPerKWH:    0.297498,
				touWinterOffPeakBaseDollarsPerKWH: 0.066111,
				schedule80Multiplier:              rockyUTBaseSchedule80Multiplier,
			},
			{
				start:                             sep25,
				end:                               nextJan1,
				flatSummerBaseDollarsPerKWH:       0.098332,
				flatWinterBaseDollarsPerKWH:       0.087020,
				touSummerPeakBaseDollarsPerKWH:    0.336173,
				touSummerOffPeakBaseDollarsPerKWH: 0.074705,
				touWinterPeakBaseDollarsPerKWH:    0.297498,
				touWinterOffPeakBaseDollarsPerKWH: 0.066111,
				schedule80Multiplier:              rockyUTSepDec2026Multiplier,
			},
		}
	}

	if year < 2026 {
		return []rockyUtahWindow{
			{
				flatSummerBaseDollarsPerKWH:       0.093199,
				flatWinterBaseDollarsPerKWH:       0.082477,
				touSummerPeakBaseDollarsPerKWH:    0.320834,
				touSummerOffPeakBaseDollarsPerKWH: 0.071296,
				touWinterPeakBaseDollarsPerKWH:    0.283924,
				touWinterOffPeakBaseDollarsPerKWH: 0.063094,
				schedule80Multiplier:              rockyUTJanJun2026Multiplier,
			},
		}
	}

	return []rockyUtahWindow{
		{
			flatSummerBaseDollarsPerKWH:       0.098332,
			flatWinterBaseDollarsPerKWH:       0.087020,
			touSummerPeakBaseDollarsPerKWH:    0.336173,
			touSummerOffPeakBaseDollarsPerKWH: 0.074705,
			touWinterPeakBaseDollarsPerKWH:    0.297498,
			touWinterOffPeakBaseDollarsPerKWH: 0.066111,
			schedule80Multiplier:              rockyUTBaseSchedule80Multiplier,
		},
	}
}

func rockyUtahFlatPeriods(year int, options types.UtilityRateOptions) []types.UtilityFeesPeriod {
	separateExport, genCreditSummerDollarsPerKWH, genCreditWinterDollarsPerKWH := rockyUtahExportCredits(options.NetMeteringScheme)
	var periods []types.UtilityFeesPeriod

	for _, w := range rockyUtahWindowsForYear(year) {
		raw := buildPeriods(mtLocation, []touSimplifiedPeriod{
			{
				Year:                     year,
				MonthStart:               time.June,
				MonthEnd:                 time.September,
				SeparateGenerationCredit: separateExport,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH:                 w.flatSummerBaseDollarsPerKWH * w.schedule80Multiplier,
						GenerationCreditDollarsPerKWH: genCreditSummerDollarsPerKWH,
						Description:                   "Utah Summer Residential Energy Charge",
					},
				},
			},
			{
				Year:                     year,
				MonthStart:               time.October,
				MonthEnd:                 time.May,
				SeparateGenerationCredit: separateExport,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH:                 w.flatWinterBaseDollarsPerKWH * w.schedule80Multiplier,
						GenerationCreditDollarsPerKWH: genCreditWinterDollarsPerKWH,
						Description:                   "Utah Winter Residential Energy Charge",
					},
				},
			},
		})
		periods = append(periods, clipRockyPeriods(raw, w.start, w.end)...)
	}

	return periods
}

func rockyUtahTOUPeriods(year int, options types.UtilityRateOptions) []types.UtilityFeesPeriod {
	holidays := getRockyHolidays(year)
	separateExport, genCreditSummerDollarsPerKWH, genCreditWinterDollarsPerKWH := rockyUtahExportCredits(options.NetMeteringScheme)
	var periods []types.UtilityFeesPeriod

	for _, w := range rockyUtahWindowsForYear(year) {
		summerPeakDollarsPerKWH := w.touSummerPeakBaseDollarsPerKWH * w.schedule80Multiplier
		summerOffPeakDollarsPerKWH := w.touSummerOffPeakBaseDollarsPerKWH * w.schedule80Multiplier
		winterPeakDollarsPerKWH := w.touWinterPeakBaseDollarsPerKWH * w.schedule80Multiplier
		winterOffPeakDollarsPerKWH := w.touWinterOffPeakBaseDollarsPerKWH * w.schedule80Multiplier

		summerHoliday := touSimplifiedPeriod{
			Year:                     year,
			MonthStart:               time.June,
			MonthEnd:                 time.September,
			SpecificDates:            holidays,
			SeparateGenerationCredit: separateExport,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:                          "Off-Peak",
					DollarsPerKWH:                 summerOffPeakDollarsPerKWH,
					GenerationCreditDollarsPerKWH: genCreditSummerDollarsPerKWH,
					Description:                   "Utah Summer TOU Holiday Off-Peak",
				},
			},
		}

		summerRegular := touSimplifiedPeriod{
			Year:                     year,
			MonthStart:               time.June,
			MonthEnd:                 time.September,
			SpecificDates:            holidays,
			SpecificDatesNot:         true,
			SeparateGenerationCredit: separateExport,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:                          "On-Peak",
					Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 22}},
					Weekday:                       true,
					DollarsPerKWH:                 summerPeakDollarsPerKWH,
					GenerationCreditDollarsPerKWH: genCreditSummerDollarsPerKWH,
					Description:                   "Utah Summer TOU On-Peak",
				},
			},
			OtherName:                          "Off-Peak",
			OtherDollarsPerKWH:                 summerOffPeakDollarsPerKWH,
			OtherGenerationCreditDollarsPerKWH: genCreditSummerDollarsPerKWH,
			OtherDescription:                   "Utah Summer TOU Off-Peak",
		}

		winterHoliday := touSimplifiedPeriod{
			Year:                     year,
			MonthStart:               time.October,
			MonthEnd:                 time.May,
			SpecificDates:            holidays,
			SeparateGenerationCredit: separateExport,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:                          "Off-Peak",
					DollarsPerKWH:                 winterOffPeakDollarsPerKWH,
					GenerationCreditDollarsPerKWH: genCreditWinterDollarsPerKWH,
					Description:                   "Utah Winter TOU Holiday Off-Peak",
				},
			},
		}

		winterRegular := touSimplifiedPeriod{
			Year:                     year,
			MonthStart:               time.October,
			MonthEnd:                 time.May,
			SpecificDates:            holidays,
			SpecificDatesNot:         true,
			SeparateGenerationCredit: separateExport,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:                          "On-Peak",
					Hours:                         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 22}},
					Weekday:                       true,
					DollarsPerKWH:                 winterPeakDollarsPerKWH,
					GenerationCreditDollarsPerKWH: genCreditWinterDollarsPerKWH,
					Description:                   "Utah Winter TOU On-Peak",
				},
			},
			OtherName:                          "Off-Peak",
			OtherDollarsPerKWH:                 winterOffPeakDollarsPerKWH,
			OtherGenerationCreditDollarsPerKWH: genCreditWinterDollarsPerKWH,
			OtherDescription:                   "Utah Winter TOU Off-Peak",
		}

		raw := buildPeriods(mtLocation, []touSimplifiedPeriod{summerHoliday, summerRegular, winterHoliday, winterRegular})
		periods = append(periods, clipRockyPeriods(raw, w.start, w.end)...)
	}

	return periods
}

func rockyIdahoExportPeriods(year int) []types.UtilityFeesPeriod {
	// Idaho Net Billing (Schedule 136) export credits (TOU, all days):
	// Summer (June - Oct): On-Peak (15:00-23:00) $0.14666/kWh, Off-Peak $0.03664/kWh
	// Winter (Nov - May): On-Peak (06:00-09:00 & 18:00-23:00) $0.05597/kWh, Off-Peak $0.01228/kWh
	return buildPeriods(mtLocation, []touSimplifiedPeriod{
		{
			Year:                         year,
			MonthStart:                   time.June,
			MonthEnd:                     time.October,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 23}},
					GenerationCreditDollarsPerKWH: 0.14666,
					Description:                   "Idaho Summer Net Billing On-Peak Credit",
				},
			},
			OtherGenerationCreditDollarsPerKWH: 0.03664,
			OtherDescription:                   "Idaho Summer Net Billing Off-Peak Credit",
		},
		{
			Year:                         year,
			MonthStart:                   time.November,
			MonthEnd:                     time.May,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
						{HourStart: 18, HourEnd: 23},
					},
					GenerationCreditDollarsPerKWH: 0.05597,
					Description:                   "Idaho Winter Net Billing On-Peak Credit",
				},
			},
			OtherGenerationCreditDollarsPerKWH: 0.01228,
			OtherDescription:                   "Idaho Winter Net Billing Off-Peak Credit",
		},
	})
}

func rockyIdahoFlatPeriods(year int, isNetBilling bool) []types.UtilityFeesPeriod {
	// Idaho Schedule 1 (first-tier values + combined Schedules 92, 94, 98, 191, and 34 riders):
	// - Jan-May 2026: $0.097400/kWh
	// - 2026-06-01 to 2027-05-31: $0.109977/kWh (Jun-Oct) and $0.092064/kWh (Nov-May)
	// - From 2027-06-01: $0.104468/kWh (Jun-Oct) and $0.087473/kWh (Nov-May)
	janMayDollarsPerKWH := 0.097400
	junOctDollarsPerKWH := 0.109977
	novDecDollarsPerKWH := 0.092064
	if year == 2027 {
		janMayDollarsPerKWH = 0.092064
		junOctDollarsPerKWH = 0.104468
		novDecDollarsPerKWH = 0.087473
	} else if year > 2027 {
		janMayDollarsPerKWH = 0.087473
		junOctDollarsPerKWH = 0.104468
		novDecDollarsPerKWH = 0.087473
	}

	periods := buildPeriods(mtLocation, []touSimplifiedPeriod{
		{
			Year:       year,
			MonthStart: time.January,
			MonthEnd:   time.May,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					DollarsPerKWH: janMayDollarsPerKWH,
					Description:   "Idaho Winter Residential Energy Charge",
				},
			},
		},
		{
			Year:       year,
			MonthStart: time.June,
			MonthEnd:   time.October,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					DollarsPerKWH: junOctDollarsPerKWH,
					Description:   "Idaho Summer Residential Energy Charge",
				},
			},
		},
		{
			Year:       year,
			MonthStart: time.November,
			MonthEnd:   time.December,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					DollarsPerKWH: novDecDollarsPerKWH,
					Description:   "Idaho Winter Residential Energy Charge",
				},
			},
		},
	})

	if isNetBilling {
		periods = append(periods, rockyIdahoExportPeriods(year)...)
	}
	return periods
}

func rockyIdahoTOUPeriods(year int, isNetBilling bool) []types.UtilityFeesPeriod {
	// Idaho Schedule 36 Optional Time of Day Residential Service (combined riders, daily peaks all days):
	// - Jun-Oct On-Peak: 15:00-23:00 MT; Nov-May On-Peak: 06:00-09:00 and 18:00-23:00 MT.
	// - Jan 2026 to 2027-05-31: $0.186804/$0.060845/kWh (Jun-Oct) and $0.160330/$0.056102/kWh (Nov-May).
	// - From 2027-06-01: $0.183038/$0.059646/kWh (Jun-Oct) and $0.157104/$0.054999/kWh (Nov-May).
	janMayPeakDollarsPerKWH := 0.160330
	janMayOffPeakDollarsPerKWH := 0.056102
	junOctPeakDollarsPerKWH := 0.186804
	junOctOffPeakDollarsPerKWH := 0.060845
	novDecPeakDollarsPerKWH := 0.160330
	novDecOffPeakDollarsPerKWH := 0.056102
	if year == 2027 {
		junOctPeakDollarsPerKWH = 0.183038
		junOctOffPeakDollarsPerKWH = 0.059646
		novDecPeakDollarsPerKWH = 0.157104
		novDecOffPeakDollarsPerKWH = 0.054999
	} else if year > 2027 {
		janMayPeakDollarsPerKWH = 0.157104
		janMayOffPeakDollarsPerKWH = 0.054999
		junOctPeakDollarsPerKWH = 0.183038
		junOctOffPeakDollarsPerKWH = 0.059646
		novDecPeakDollarsPerKWH = 0.157104
		novDecOffPeakDollarsPerKWH = 0.054999
	}

	winterPeakHours := []types.UtilityHourPeriod{
		{HourStart: 6, HourEnd: 9},
		{HourStart: 18, HourEnd: 23},
	}

	periods := buildPeriods(mtLocation, []touSimplifiedPeriod{
		{
			Year:       year,
			MonthStart: time.January,
			MonthEnd:   time.May,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         winterPeakHours,
					DollarsPerKWH: janMayPeakDollarsPerKWH,
					Description:   "Idaho Winter TOU On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: janMayOffPeakDollarsPerKWH,
			OtherDescription:   "Idaho Winter TOU Off-Peak",
		},
		{
			Year:       year,
			MonthStart: time.June,
			MonthEnd:   time.October,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 23}},
					DollarsPerKWH: junOctPeakDollarsPerKWH,
					Description:   "Idaho Summer TOU On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: junOctOffPeakDollarsPerKWH,
			OtherDescription:   "Idaho Summer TOU Off-Peak",
		},
		{
			Year:       year,
			MonthStart: time.November,
			MonthEnd:   time.December,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         winterPeakHours,
					DollarsPerKWH: novDecPeakDollarsPerKWH,
					Description:   "Idaho Winter TOU On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: novDecOffPeakDollarsPerKWH,
			OtherDescription:   "Idaho Winter TOU Off-Peak",
		},
	})

	if isNetBilling {
		periods = append(periods, rockyIdahoExportPeriods(year)...)
	}
	return periods
}

func rockyWyomingFlatPeriods(year int) []types.UtilityFeesPeriod {
	// Wyoming Schedule 2 Residential Service (combined base + Schedules 95, 93, 191, and 198):
	// - Jan-Jun 2026: $0.12174/kWh
	// - From 2026-07-01 through 2027: $0.11515/kWh
	if year == 2026 {
		return buildPeriods(mtLocation, []touSimplifiedPeriod{
			{
				Year:       2026,
				MonthStart: time.January,
				MonthEnd:   time.June,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH: 0.12174,
						Description:   "Wyoming Residential Energy Charge",
					},
				},
			},
			{
				Year:       2026,
				MonthStart: time.July,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						DollarsPerKWH: 0.11515,
						Description:   "Wyoming Residential Energy Charge",
					},
				},
			},
		})
	}

	dollarsPerKWH := 0.11515
	if year < 2026 {
		dollarsPerKWH = 0.12174
	}
	return buildPeriods(mtLocation, []touSimplifiedPeriod{
		{
			Year:       year,
			MonthStart: time.January,
			MonthEnd:   time.December,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					DollarsPerKWH: dollarsPerKWH,
					Description:   "Wyoming Residential Energy Charge",
				},
			},
		},
	})
}

func rockyWyomingTOUPeriods(year int) []types.UtilityFeesPeriod {
	// Wyoming Schedule 19 Residential Service Time-of-Use Pilot (combined riders, daily peaks all days):
	// - On-Peak: 07:00-09:00 and 17:00-23:00 MT, all days.
	// - Jan-Jun 2026: $0.16091/kWh On-Peak, $0.09869/kWh Off-Peak.
	// - From 2026-07-01 through 2027: $0.15432/kWh On-Peak, $0.09210/kWh Off-Peak.
	peakHours := []types.UtilityHourPeriod{
		{HourStart: 7, HourEnd: 9},
		{HourStart: 17, HourEnd: 23},
	}

	if year == 2026 {
		return buildPeriods(mtLocation, []touSimplifiedPeriod{
			{
				Year:       2026,
				MonthStart: time.January,
				MonthEnd:   time.June,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						DollarsPerKWH: 0.16091,
						Description:   "Wyoming TOU On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.09869,
				OtherDescription:   "Wyoming TOU Off-Peak",
			},
			{
				Year:       2026,
				MonthStart: time.July,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						DollarsPerKWH: 0.15432,
						Description:   "Wyoming TOU On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.09210,
				OtherDescription:   "Wyoming TOU Off-Peak",
			},
		})
	}

	peakDollarsPerKWH := 0.15432
	offPeakDollarsPerKWH := 0.09210
	if year < 2026 {
		peakDollarsPerKWH = 0.16091
		offPeakDollarsPerKWH = 0.09869
	}
	return buildPeriods(mtLocation, []touSimplifiedPeriod{
		{
			Year:       year,
			MonthStart: time.January,
			MonthEnd:   time.December,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         peakHours,
					DollarsPerKWH: peakDollarsPerKWH,
					Description:   "Wyoming TOU On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Wyoming TOU Off-Peak",
		},
	})
}

func rockyPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	isIdahoNetBilling := options.NetMeteringScheme == "net_billing" || options.NetMeteringScheme == ""

	for _, year := range years {
		switch plan {
		case "rocky_mountain_power_utah_residential":
			periods = append(periods, rockyUtahFlatPeriods(year, options)...)
		case "rocky_mountain_power_utah_residential_tou":
			periods = append(periods, rockyUtahTOUPeriods(year, options)...)
		case "rocky_mountain_power_idaho_residential":
			periods = append(periods, rockyIdahoFlatPeriods(year, isIdahoNetBilling)...)
		case "rocky_mountain_power_idaho_residential_tou":
			periods = append(periods, rockyIdahoTOUPeriods(year, isIdahoNetBilling)...)
		case "rocky_mountain_power_wyoming_residential":
			periods = append(periods, rockyWyomingFlatPeriods(year)...)
		case "rocky_mountain_power_wyoming_residential_tou":
			periods = append(periods, rockyWyomingTOUPeriods(year)...)
		}
	}
	return periods
}

func rockyUtilityInfo() types.UtilityProviderInfo {
	utahExportOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your net metering or billing plan program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "net", Name: "Net Metering (Schedule 135)"},
				{Value: "schedule_136", Name: "Transition Program (Schedule 136)"},
				{Value: "net_billing", Name: "Net Billing (Schedule 137)"},
			},
			Default: "net_billing",
		},
	}

	idahoExportOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your net metering or billing plan program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "net", Name: "Net Metering (Schedule 135)"},
				{Value: "net_billing", Name: "Net Billing (Schedule 136)"},
			},
			Default: "net_billing",
		},
	}

	wyomingExportOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your net metering or billing plan program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "net", Name: "Net Metering (Schedule 135)"},
			},
			Default: "net",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "rocky_mountain_power",
		Name: "Rocky Mountain Power (RMP)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "rocky_mountain_power_utah_residential",
				Name:    "Utah Residential Service",
				Options: utahExportOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_utah_residential", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "rocky_mountain_power_utah_residential_tou",
				Name:    "Utah Residential Service Time-Of-Use Option",
				Options: utahExportOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_utah_residential_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "rocky_mountain_power_idaho_residential",
				Name:    "Idaho Residential Service",
				Options: idahoExportOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_idaho_residential", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "rocky_mountain_power_idaho_residential_tou",
				Name:    "Idaho Optional Time of Day Residential Service (Schedule 36)",
				Options: idahoExportOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_idaho_residential_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "rocky_mountain_power_wyoming_residential",
				Name:    "Wyoming Residential Service",
				Options: wyomingExportOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_wyoming_residential", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "rocky_mountain_power_wyoming_residential_tou",
				Name:    "Wyoming Residential Service Time-of-Use Pilot (Schedule 19)",
				Options: wyomingExportOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return rockyPeriods("rocky_mountain_power_wyoming_residential_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
