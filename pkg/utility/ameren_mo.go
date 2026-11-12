package utility

import (
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Ameren Missouri residential tariff rates under MO.P.S.C. Schedule No. 6,
// Service Classification No. 1(M) (effective June 1, 2025 under Case No. ER-2024-0319)
// and Electric Power Purchases from Qualified Net Metering Units (7th Revised Sheet No. 171,
// effective June 1, 2026).
const (
	// Residential Anytime Service (Sheet No. 54).
	// NOTE: Winter rates on Anytime and Evening/Morning Savers are tiered at 750 kWh/month;
	// we assume Tier 1 (first 750 kWh) usage.
	amerenMOAnytimeSummerDollarsPerKWH = 0.1560 // June - September, all kWh
	amerenMOAnytimeWinterDollarsPerKWH = 0.1062 // October - May, first 750 kWh

	// Residential Evening/Morning Saver Service (Sheet No. 54.4)
	amerenMOEveningMorningSummerBaseDollarsPerKWH    = 0.1524 // June - September, all kWh
	amerenMOEveningMorningSummerPeakAdjDollarsPerKWH = 0.0057 // +0.57¢/kWh 9 AM - 9 PM all days
	amerenMOEveningMorningWinterBaseDollarsPerKWH    = 0.1045 // October - May, first 750 kWh
	amerenMOEveningMorningWinterPeakAdjDollarsPerKWH = 0.0028 // +0.28¢/kWh 9 AM - 9 PM all days

	// Residential Overnight Saver Service (Sheet No. 54.10)
	amerenMOOvernightSummerOnPeakDollarsPerKWH  = 0.2036 // June - September, 6 AM - 10 PM all days
	amerenMOOvernightSummerOffPeakDollarsPerKWH = 0.0810 // June - September, 10 PM - 6 AM all days
	amerenMOOvernightWinterOnPeakDollarsPerKWH  = 0.1145 // October - May (Option A), 6 AM - 10 PM all days
	amerenMOOvernightWinterOffPeakDollarsPerKWH = 0.0699 // October - May (Option A), 10 PM - 6 AM all days

	// Residential Smart Saver Service (3-tier TOU)
	amerenMOSmartSaverSummerOnPeakDollarsPerKWH       = 0.4051 // June - September, 3 PM - 7 PM Mon-Fri excl. holidays
	amerenMOSmartSaverSummerIntermediateDollarsPerKWH = 0.1216 // June - September, 6 AM - 10 PM excl. On-Peak
	amerenMOSmartSaverSummerOffPeakDollarsPerKWH      = 0.0767 // June - September, 10 PM - 6 AM all days
	amerenMOSmartSaverWinterOnPeakDollarsPerKWH       = 0.2169 // October - May (Option A), 6-8 AM & 6-8 PM Mon-Fri excl. holidays
	amerenMOSmartSaverWinterIntermediateDollarsPerKWH = 0.0778 // October - May (Option A), 6 AM - 10 PM excl. On-Peak
	amerenMOSmartSaverWinterOffPeakDollarsPerKWH      = 0.0635 // October - May (Option A), 10 PM - 6 AM all days

	// Residential Ultimate Saver Service (Demand + 2-tier TOU)
	amerenMOUltimateSaverSummerOnPeakDollarsPerKWH  = 0.3413 // June - September, 3 PM - 7 PM Mon-Fri excl. holidays
	amerenMOUltimateSaverSummerOffPeakDollarsPerKWH = 0.0578 // June - September, all other hours
	amerenMOUltimateSaverWinterOnPeakDollarsPerKWH  = 0.1856 // October - May (Option A), 6-8 AM & 6-8 PM Mon-Fri excl. holidays
	amerenMOUltimateSaverWinterOffPeakDollarsPerKWH = 0.0511 // October - May (Option A), all other hours

	// Net Metering Excess Generation Avoided Fuel Cost (Sheet No. 171)
	amerenMOSummerAvoidedCostDollarsPerKWH = 0.0384 // June - September
	amerenMOWinterAvoidedCostDollarsPerKWH = 0.0339 // October - May
)

func shiftAmerenMOWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func amerenMOChristmasHolidays(year int) (time.Time, time.Time) {
	eve := christmasEve(year)
	day := christmasDay(year)
	switch day.Weekday() {
	case time.Saturday:
		// Dec 24 is Friday, Dec 25 is Saturday -> observed Thursday Dec 23 and Friday Dec 24 (e.g. 2027)
		return eve.AddDate(0, 0, -1), day.AddDate(0, 0, -1)
	case time.Sunday:
		// Dec 24 is Saturday, Dec 25 is Sunday -> observed Friday Dec 23 and Monday Dec 26 (e.g. 2033)
		return eve.AddDate(0, 0, -1), day.AddDate(0, 0, 1)
	case time.Monday:
		// Dec 24 is Sunday, Dec 25 is Monday -> observed Friday Dec 22 and Monday Dec 25 (e.g. 2028)
		return eve.AddDate(0, 0, -2), day
	default:
		return eve, day
	}
}

// getAmerenMOHolidays returns Ameren Missouri's 9 observed tariff holidays per
// MO.P.S.C. Schedule No. 6 (New Year's Day, Good Friday, Memorial Day,
// Independence Day, Labor Day, Thanksgiving Day, Thanksgiving Friday,
// Christmas Eve Day, and Christmas Day).
func getAmerenMOHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)
	xmasEve, xmasDay := amerenMOChristmasHolidays(year)

	holidays := []time.Time{
		shiftAmerenMOWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftAmerenMOWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		xmasEve,
		xmasDay,
		shiftAmerenMOWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func amerenMOPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	isOptionB := strings.EqualFold(strings.TrimSpace(options.PeakPeriodOption), "option_b")
	isAvoidedCost := strings.EqualFold(strings.TrimSpace(options.NetMeteringScheme), "avoided_cost")

	for _, year := range years {
		holidays := getAmerenMOHolidays(year)

		switch plan {
		case "ameren_mo_anytime":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: amerenMOAnytimeSummerDollarsPerKWH,
					OtherDescription:   "Ameren MO Anytime Summer Energy Charge",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH,
					OtherDescription:   "Ameren MO Anytime Winter Energy Charge",
				},
			})...)

		case "ameren_mo_evening_morning":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				// Summer (June - September): On-Peak 9:00 AM - 9:00 PM all days
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							DollarsPerKWH: amerenMOEveningMorningSummerBaseDollarsPerKWH + amerenMOEveningMorningSummerPeakAdjDollarsPerKWH,
							Description:   "Ameren MO Evening/Morning Savers Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOEveningMorningSummerBaseDollarsPerKWH,
					OtherDescription:   "Ameren MO Evening/Morning Savers Summer Off-Peak",
				},
				// Winter (October - May): On-Peak 9:00 AM - 9:00 PM all days
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							DollarsPerKWH: amerenMOEveningMorningWinterBaseDollarsPerKWH + amerenMOEveningMorningWinterPeakAdjDollarsPerKWH,
							Description:   "Ameren MO Evening/Morning Savers Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOEveningMorningWinterBaseDollarsPerKWH,
					OtherDescription:   "Ameren MO Evening/Morning Savers Winter Off-Peak",
				},
			})...)

		case "ameren_mo_overnight":
			simplified := []touSimplifiedPeriod{
				// Summer (June - September): On-Peak 6:00 AM - 10:00 PM all days, Off-Peak 10:00 PM - 6:00 AM all days
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
							DollarsPerKWH: amerenMOOvernightSummerOnPeakDollarsPerKWH,
							Description:   "Ameren MO Overnight Savers Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOOvernightSummerOffPeakDollarsPerKWH,
					OtherDescription:   "Ameren MO Overnight Savers Summer Off-Peak",
				},
			}

			if isOptionB {
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH,
					OtherDescription:   "Ameren MO Overnight Savers Option B Winter Anytime Rate",
				})
			} else {
				simplified = append(simplified, touSimplifiedPeriod{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
							DollarsPerKWH: amerenMOOvernightWinterOnPeakDollarsPerKWH,
							Description:   "Ameren MO Overnight Savers Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOOvernightWinterOffPeakDollarsPerKWH,
					OtherDescription:   "Ameren MO Overnight Savers Winter Off-Peak",
				})
			}

			periods = append(periods, buildPeriods(ctLocation, simplified)...)

		case "ameren_mo_smart_saver":
			simplified := []touSimplifiedPeriod{
				// Summer Holidays (June - September): Intermediate 6 AM - 10 PM, Off-Peak 10 PM - 6 AM
				{
					Year:          year,
					MonthStart:    time.June,
					MonthEnd:      time.September,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Intermediate",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
							DollarsPerKWH: amerenMOSmartSaverSummerIntermediateDollarsPerKWH,
							Description:   "Ameren MO Smart Savers Summer Holiday Intermediate",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOSmartSaverSummerOffPeakDollarsPerKWH,
					OtherDescription:   "Ameren MO Smart Savers Summer Holiday Off-Peak",
				},
				// Summer Non-Holidays (June - September):
				// On-Peak 3 PM - 7 PM Mon-Fri; Intermediate 6 AM - 10 PM excl. On-Peak; Off-Peak 10 PM - 6 AM
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: amerenMOSmartSaverSummerOnPeakDollarsPerKWH,
							Description:   "Ameren MO Smart Savers Summer On-Peak",
						},
						{
							Name: "Intermediate",
							Hours: []types.UtilityHourPeriod{
								{HourStart: 6, HourEnd: 15},
								{HourStart: 19, HourEnd: 22},
							},
							Weekday:       true,
							DollarsPerKWH: amerenMOSmartSaverSummerIntermediateDollarsPerKWH,
							Description:   "Ameren MO Smart Savers Summer Weekday Intermediate",
						},
						{
							Name:          "Intermediate",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
							Weekend:       true,
							DollarsPerKWH: amerenMOSmartSaverSummerIntermediateDollarsPerKWH,
							Description:   "Ameren MO Smart Savers Summer Weekend Intermediate",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOSmartSaverSummerOffPeakDollarsPerKWH,
					OtherDescription:   "Ameren MO Smart Savers Summer Off-Peak",
				},
			}

			if isOptionB {
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH,
					OtherDescription:   "Ameren MO Smart Savers Option B Winter Anytime Rate",
				})
			} else {
				simplified = append(simplified,
					// Winter Holidays (October - May): Intermediate 6 AM - 10 PM, Off-Peak 10 PM - 6 AM
					touSimplifiedPeriod{
						Year:          year,
						MonthStart:    time.October,
						MonthEnd:      time.May,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Intermediate",
								Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
								DollarsPerKWH: amerenMOSmartSaverWinterIntermediateDollarsPerKWH,
								Description:   "Ameren MO Smart Savers Winter Holiday Intermediate",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOSmartSaverWinterOffPeakDollarsPerKWH,
						OtherDescription:   "Ameren MO Smart Savers Winter Holiday Off-Peak",
					},
					// Winter Non-Holidays (October - May):
					// On-Peak 6-8 AM & 6-8 PM Mon-Fri; Intermediate 6 AM - 10 PM excl. On-Peak; Off-Peak 10 PM - 6 AM
					touSimplifiedPeriod{
						Year:             year,
						MonthStart:       time.October,
						MonthEnd:         time.May,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "On-Peak",
								Hours: []types.UtilityHourPeriod{
									{HourStart: 6, HourEnd: 8},
									{HourStart: 18, HourEnd: 20},
								},
								Weekday:       true,
								DollarsPerKWH: amerenMOSmartSaverWinterOnPeakDollarsPerKWH,
								Description:   "Ameren MO Smart Savers Winter On-Peak",
							},
							{
								Name: "Intermediate",
								Hours: []types.UtilityHourPeriod{
									{HourStart: 8, HourEnd: 18},
									{HourStart: 20, HourEnd: 22},
								},
								Weekday:       true,
								DollarsPerKWH: amerenMOSmartSaverWinterIntermediateDollarsPerKWH,
								Description:   "Ameren MO Smart Savers Winter Weekday Intermediate",
							},
							{
								Name:          "Intermediate",
								Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
								Weekend:       true,
								DollarsPerKWH: amerenMOSmartSaverWinterIntermediateDollarsPerKWH,
								Description:   "Ameren MO Smart Savers Winter Weekend Intermediate",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOSmartSaverWinterOffPeakDollarsPerKWH,
						OtherDescription:   "Ameren MO Smart Savers Winter Off-Peak",
					},
				)
			}

			periods = append(periods, buildPeriods(ctLocation, simplified)...)

		case "ameren_mo_ultimate_saver":
			simplified := []touSimplifiedPeriod{
				// Summer Holidays (June - September): Off-Peak all day
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOUltimateSaverSummerOffPeakDollarsPerKWH,
					OtherDescription:   "Ameren MO Ultimate Savers Summer Holiday Off-Peak",
				},
				// Summer Non-Holidays (June - September): On-Peak 3 PM - 7 PM Mon-Fri, Off-Peak all other hours
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: amerenMOUltimateSaverSummerOnPeakDollarsPerKWH,
							Description:   "Ameren MO Ultimate Savers Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: amerenMOUltimateSaverSummerOffPeakDollarsPerKWH,
					OtherDescription:   "Ameren MO Ultimate Savers Summer Off-Peak",
				},
			}

			if isOptionB {
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH,
					OtherDescription:   "Ameren MO Ultimate Savers Option B Winter Anytime Rate",
				})
			} else {
				simplified = append(simplified,
					// Winter Holidays (October - May): Off-Peak all day
					touSimplifiedPeriod{
						Year:               year,
						MonthStart:         time.October,
						MonthEnd:           time.May,
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOUltimateSaverWinterOffPeakDollarsPerKWH,
						OtherDescription:   "Ameren MO Ultimate Savers Winter Holiday Off-Peak",
					},
					// Winter Non-Holidays (October - May): On-Peak 6-8 AM & 6-8 PM Mon-Fri, Off-Peak all other hours
					touSimplifiedPeriod{
						Year:             year,
						MonthStart:       time.October,
						MonthEnd:         time.May,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "On-Peak",
								Hours: []types.UtilityHourPeriod{
									{HourStart: 6, HourEnd: 8},
									{HourStart: 18, HourEnd: 20},
								},
								Weekday:       true,
								DollarsPerKWH: amerenMOUltimateSaverWinterOnPeakDollarsPerKWH,
								Description:   "Ameren MO Ultimate Savers Winter On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOUltimateSaverWinterOffPeakDollarsPerKWH,
						OtherDescription:   "Ameren MO Ultimate Savers Winter Off-Peak",
					},
				)
			}

			periods = append(periods, buildPeriods(ctLocation, simplified)...)
		}

		if isAvoidedCost {
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.September,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: amerenMOSummerAvoidedCostDollarsPerKWH,
					OtherDescription:                   "Ameren MO Net Metering Summer Avoided Fuel Cost",
				},
				{
					Year:                               year,
					MonthStart:                         time.October,
					MonthEnd:                           time.May,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: amerenMOWinterAvoidedCostDollarsPerKWH,
					OtherDescription:                   "Ameren MO Net Metering Winter Avoided Fuel Cost",
				},
			})...)
		}
	}

	return periods
}

func amerenMORateInfos() []types.UtilityRateInfo {
	seasonalOption := types.UtilityRateOption{
		Field:       "peakPeriodOption",
		Name:        "Seasonal Option",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Option A applies TOU pricing year-round; Option B applies TOU in summer (June - September) and standard Anytime rates in winter (October - May).",
		Choices: []types.UtilityOptionChoice{
			{Value: "option_a", Name: "Option A (Year-Round TOU)"},
			{Value: "option_b", Name: "Option B (Summer TOU / Winter Anytime)"},
		},
		Default: "option_a",
	}

	monthlyNetMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select 1:1 monthly kWh net metering offset or excess generation Avoided Fuel Cost export credit (Sheet 171).",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Metering (1:1 Monthly kWh Offset)"},
			{Value: "avoided_cost", Name: "Excess Generation Avoided Fuel Cost ($0.0384 Summer / $0.0339 Winter)"},
		},
		Default: "net",
	}

	touBucketNetMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Under Sheet 171, solar exports on Overnight, Smart, and Ultimate Saver plans offset consumption 1:1 within each TOU bucket, with net bucket excess credited at Avoided Fuel Cost.",
		Choices: []types.UtilityOptionChoice{
			{Value: "tou_net", Name: "Net Metering (1:1 TOU Bucket Offset)"},
			{Value: "avoided_cost", Name: "Excess Generation Avoided Fuel Cost ($0.0384 Summer / $0.0339 Winter)"},
		},
		Default: "tou_net",
	}

	return []types.UtilityRateInfo{
		{
			ID:      "ameren_mo_evening_morning",
			Name:    "Evening/Morning Savers (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{monthlyNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_evening_morning", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_overnight",
			Name:    "Overnight Savers (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{seasonalOption, touBucketNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_overnight", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_smart_saver",
			Name:    "Smart Savers TOU (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{seasonalOption, touBucketNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_smart_saver", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_ultimate_saver",
			Name:    "Ultimate Savers Demand TOU (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{seasonalOption, touBucketNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_ultimate_saver", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_anytime",
			Name:    "Anytime Users (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{monthlyNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_anytime", opts, []int{2026, 2027}), nil
			},
		},
	}
}
