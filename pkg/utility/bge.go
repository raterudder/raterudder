package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftBGESundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

func getBGEHolidays(year int) []string {
	holidays := []time.Time{
		shiftBGESundayHoliday(newYearsDay(year)),
		shiftBGESundayHoliday(presidentsDay(year)),
		shiftBGESundayHoliday(goodFriday(year)),
		shiftBGESundayHoliday(memorialDay(year)),
		shiftBGESundayHoliday(independenceDay(year)),
		shiftBGESundayHoliday(laborDay(year)),
		shiftBGESundayHoliday(thanksgivingDay(year)),
		shiftBGESundayHoliday(christmasDay(year)),
	}
	var out []string
	for _, h := range holidays {
		out = append(out, h.Format("2006-01-02"))
	}
	return out
}

func bgePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	locStr := etLocation.String() // BGE is in Maryland (Eastern Time)

	for _, year := range years {
		holidays := getBGEHolidays(year)
		switch plan {
		case "bge_r":
			// Schedule R (Residential Service) - flat delivery/supply
			simplified := []touSimplifiedPeriod{
				// Summer (June - September)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.19700,
							Description:   "BGE Schedule R Summer",
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
							DollarsPerKWH: 0.19103,
							Description:   "BGE Schedule R Non-Summer",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "bge_rl":
			// Schedule RL (Residential Optional TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.32152,
							Description:   "BGE Schedule RL Summer Peak",
						},
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 10}, {HourStart: 20, HourEnd: 23}},
							Weekday:       true,
							DollarsPerKWH: 0.22152,
							Description:   "BGE Schedule RL Summer Intermediate",
						},
					},
					OtherDollarsPerKWH: 0.15152,
					OtherDescription:   "BGE Schedule RL Summer Off-Peak",
				},
				// Summer (June - September) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherDollarsPerKWH: 0.15152,
					OtherDescription:   "BGE Schedule RL Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.29152,
							Description:   "BGE Schedule RL Non-Summer Peak",
						},
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 17}},
							Weekday:       true,
							DollarsPerKWH: 0.21152,
							Description:   "BGE Schedule RL Non-Summer Intermediate",
						},
					},
					OtherDollarsPerKWH: 0.15652,
					OtherDescription:   "BGE Schedule RL Non-Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherDollarsPerKWH: 0.15652,
					OtherDescription:   "BGE Schedule RL Non-Summer Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "bge_ev":
			// Schedule EV (Residential EV TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.32181,
							Description:   "BGE Schedule EV Summer Peak",
						},
					},
					OtherDollarsPerKWH: 0.15181,
					OtherDescription:   "BGE Schedule EV Summer Off-Peak",
				},
				// Summer (June - September) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherDollarsPerKWH: 0.15181,
					OtherDescription:   "BGE Schedule EV Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.29181,
							Description:   "BGE Schedule EV Non-Summer Peak",
						},
					},
					OtherDollarsPerKWH: 0.15681,
					OtherDescription:   "BGE Schedule EV Non-Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherDollarsPerKWH: 0.15681,
					OtherDescription:   "BGE Schedule EV Non-Summer Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "bge_rd":
			// Schedule RD (Residential Delivery & Energy TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.38353,
							Description:   "BGE Schedule RD Summer Peak",
						},
					},
					OtherDollarsPerKWH: 0.13664,
					OtherDescription:   "BGE Schedule RD Summer Off-Peak",
				},
				// Summer (June - September) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherDollarsPerKWH: 0.13664,
					OtherDescription:   "BGE Schedule RD Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekdays (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.38353,
							Description:   "BGE Schedule RD Non-Summer Peak",
						},
					},
					OtherDollarsPerKWH: 0.13664,
					OtherDescription:   "BGE Schedule RD Non-Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekends & Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					SpecificDatesNot:   false,
					OtherDollarsPerKWH: 0.13664,
					OtherDescription:   "BGE Schedule RD Non-Summer Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)
		}
	}
	return periods
}

func bgeUtilityInfo() types.UtilityProviderInfo {
	bgeOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "BGE net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "bge",
		Name: "Baltimore Gas and Electric",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "bge_r",
				Name:    "Schedule R (Residential Service)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_r", opts, []int{2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "bge_rl",
				Name:    "Schedule RL (Residential Optional Time-of-Use)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_rl", opts, []int{2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "bge_ev",
				Name:    "Schedule EV (Residential EV Time-of-Use)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_ev", opts, []int{2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "bge_rd",
				Name:    "Schedule RD (Residential Delivery & Energy TOU)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_rd", opts, []int{2026, 2027, 2028}), nil
				},
			},
		},
	}
}
