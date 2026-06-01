package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftDominionWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getDominionHolidays(year int) []string {
	holidays := []time.Time{
		shiftDominionWeekendHoliday(newYearsDay(year)),
		shiftDominionWeekendHoliday(memorialDay(year)),
		shiftDominionWeekendHoliday(independenceDay(year)),
		shiftDominionWeekendHoliday(laborDay(year)),
		shiftDominionWeekendHoliday(thanksgivingDay(year)),
		shiftDominionWeekendHoliday(christmasDay(year)),
	}
	var out []string
	for _, h := range holidays {
		out = append(out, h.Format("2006-01-02"))
	}
	return out
}

func dominionPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	locStr := etLocation.String()

	for _, year := range years {
		holidays := getDominionHolidays(year)
		if plan == "dominion_1" {
			// Schedule 1 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.076602,
							Description:   "Dominion Schedule 1 Summer (June-September)",
						},
					},
				},
				// Non-Summer (October - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.075454,
							Description:   "Dominion Schedule 1 Non-Summer (October-May)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)
		} else if plan == "dominion_1g" {
			// Schedule 1G (Residential Service TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 18}},
							Weekday:       true,
							DollarsPerKWH: 0.210415,
							Description:   "Dominion Schedule 1G Summer On-Peak",
						},
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.033486,
							Description:   "Dominion Schedule 1G Summer Super Off-Peak",
						},
					},
					OtherDollarsPerKWH: 0.051367,
					OtherDescription:   "Dominion Schedule 1G Summer Off-Peak",
				},
				// Summer (May - September) - Holidays
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: false,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.033486,
							Description:   "Dominion Schedule 1G Summer Super Off-Peak",
						},
					},
					OtherDollarsPerKWH: 0.051367,
					OtherDescription:   "Dominion Schedule 1G Summer Off-Peak",
				},
				// Winter (October - April) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.170938,
							Description:   "Dominion Schedule 1G Winter On-Peak",
						},
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.049529,
							Description:   "Dominion Schedule 1G Winter Super Off-Peak",
						},
					},
					OtherDollarsPerKWH: 0.055752,
					OtherDescription:   "Dominion Schedule 1G Winter Off-Peak",
				},
				// Winter (October - April) - Holidays
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: false,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.049529,
							Description:   "Dominion Schedule 1G Winter Super Off-Peak",
						},
					},
					OtherDollarsPerKWH: 0.055752,
					OtherDescription:   "Dominion Schedule 1G Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)
		}
	}
	return periods
}

func dominionUtilityInfo() types.UtilityProviderInfo {
	dominionOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Dominion net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "dominion",
		Name: "Dominion Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "dominion_1",
				Name:    "Schedule 1 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1g",
				Name:    "Schedule 1G (Residential Service TOU)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1g", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
		},
	}
}
