package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftEversourceSundayHoliday shifts holidays falling on a Sunday to Monday.
// Saturday holidays do not shift.
func shiftEversourceSundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

// getEversourceHolidays returns the recognized holidays for Eversource billing.
func getEversourceHolidays(year int) []string {
	holidays := []time.Time{
		shiftEversourceSundayHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftEversourceSundayHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftEversourceSundayHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftEversourceSundayHoliday(christmasDay(year)),
	}
	return formatHolidays(holidays, year)
}

// eversourcePeriods generates the fees period slice for a specific Eversource rate plan.
func eversourcePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	locStr := etLocation.String() // America/New_York (Eastern Time)

	for _, year := range years {
		holidays := getEversourceHolidays(year)

		switch plan {
		// ==========================================
		// CONNECTICUT (CT) PLANS
		// ==========================================
		case "eversource_ct_rate_1":
			// CT Residential Flat (Rate 1)
			// Total rate: $0.24666/kWh
			// Exports: Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			simplified := []touSimplifiedPeriod{
				{
					Year:                                   year,
					MonthStart:                             time.January,
					MonthEnd:                               time.December,
					OtherDollarsPerKWH:                     0.24666,
					OtherDescription:                       "Eversource CT Rate 1 Flat",
					OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "eversource_ct_rate_5":
			// CT Residential Heating (Rate 5)
			// Total rate: $0.22277/kWh
			// Exports: Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			simplified := []touSimplifiedPeriod{
				{
					Year:                                   year,
					MonthStart:                             time.January,
					MonthEnd:                               time.December,
					OtherDollarsPerKWH:                     0.22277,
					OtherDescription:                       "Eversource CT Rate 5 Heating Flat",
					OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "eversource_ct_rate_7":
			// CT Residential Time-of-Day (Rate 7)
			// On-Peak: Weekdays 12 Noon – 8 p.m. Rate: $0.31099/kWh
			// Off-Peak: All other hours. Rate: $0.21871/kWh
			// Exports: Renewable Energy Solutions Rider applies a -$0.0402/kWh adjustment fee
			simplified := []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:                             []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}},
							Weekday:                           true,
							DollarsPerKWH:                     0.31099,
							Description:                       "Eversource CT Rate 7 On-Peak",
							GenerationAdjustmentDollarsPerKWH: -0.0402,
						},
					},
					OtherDollarsPerKWH:                     0.21871,
					OtherDescription:                       "Eversource CT Rate 7 Off-Peak",
					OtherGenerationAdjustmentDollarsPerKWH: -0.0402,
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		// ==========================================
		// NEW HAMPSHIRE (NH) PLANS
		// ==========================================
		case "eversource_nh_rate_r":
			// NH Residential Standard (Rate R)
			// Total rate: $0.23128/kWh
			// Plain 1:1 net metering (no adjustment)
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.23128,
					OtherDescription:   "Eversource NH Rate R Flat",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "eversource_nh_rate_r_otod":
			// NH Residential Time-of-Day (Rate R-OTOD, maps to active R-OTOD 2)
			// On-Peak: Weekdays 1 p.m. – 7 p.m. excluding holidays. Rate: $0.34792/kWh
			// Off-Peak: All other hours. Rate: $0.19583/kWh
			// Plain 1:1 net metering (no adjustment)
			simplified := []touSimplifiedPeriod{
				// Holidays are Off-Peak
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					SpecificDates:      holidays,
					OtherDollarsPerKWH: 0.19583,
					OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak (Holiday)",
				},
				// Weekdays and Weekends
				{
					Year:             year,
					MonthStart:       time.January,
					MonthEnd:         time.December,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.34792,
							Description:   "Eversource NH Rate R-OTOD On-Peak",
						},
					},
					OtherDollarsPerKWH: 0.19583,
					OtherDescription:   "Eversource NH Rate R-OTOD Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "eversource_nh_rate_r_ev":
			// NH Residential Plug-in EV & Battery Storage Time-of-Day (Rate R-EV)
			// On-Peak: Weekdays 2 p.m. – 7 p.m. excluding holidays. Rate: $0.33000/kWh
			// Mid-Peak: Weekdays 7 a.m. – 2 p.m. & 7 p.m. – 11 p.m. excluding holidays,
			//           and Weekends/Holidays 7 a.m. – 11 p.m. Rate: $0.21000/kWh
			// Off-Peak: Daily 11 p.m. – 7 a.m. Rate: $0.11000/kWh
			// Plain 1:1 net metering (no adjustment)
			simplified := []touSimplifiedPeriod{
				// Holidays
				{
					Year:          year,
					MonthStart:    time.January,
					MonthEnd:      time.December,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
							DollarsPerKWH: 0.21000,
							Description:   "Eversource NH Rate R-EV Mid-Peak (Holiday)",
						},
					},
					OtherDollarsPerKWH: 0.11000,
					OtherDescription:   "Eversource NH Rate R-EV Off-Peak (Holiday)",
				},
				// Weekdays and Weekends
				{
					Year:             year,
					MonthStart:       time.January,
					MonthEnd:         time.December,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						// On-Peak weekdays
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
							Weekday:       true,
							DollarsPerKWH: 0.33000,
							Description:   "Eversource NH Rate R-EV On-Peak",
						},
						// Mid-Peak weekdays
						{
							Hours: []types.UtilityHourPeriod{
								{HourStart: 7, HourEnd: 14},
								{HourStart: 19, HourEnd: 23},
							},
							Weekday:       true,
							DollarsPerKWH: 0.21000,
							Description:   "Eversource NH Rate R-EV Mid-Peak (Weekday)",
						},
						// Mid-Peak weekends
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
							Weekend:       true,
							DollarsPerKWH: 0.21000,
							Description:   "Eversource NH Rate R-EV Mid-Peak (Weekend)",
						},
					},
					OtherDollarsPerKWH: 0.11000,
					OtherDescription:   "Eversource NH Rate R-EV Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		// ==========================================
		// MASSACHUSETTS (MA) PLANS
		// ==========================================
		case "eversource_ma_residential":
			// MA Residential Standard (Rate R-1)
			// Total rate: $0.30471/kWh
			// Plain 1:1 net metering (no adjustment)
			// We do not support the SMART program yet because it requires enrollment in the ConnectedSolutions demand response program.
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.30471,
					OtherDescription:   "Eversource MA Rate R-1 Flat",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)
		}
	}
	return periods
}

// eversourceUtilityInfo returns the metadata and rate options for Eversource.
func eversourceUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "eversource",
		Name: "Eversource",
		Rates: []types.UtilityRateInfo{
			// Connecticut
			{
				ID:   "eversource_ct_rate_1",
				Name: "CT Residential Flat (Rate 1)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "eversource_ct_rate_5",
				Name: "CT Residential Heating (Rate 5)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_5", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "eversource_ct_rate_7",
				Name: "CT Residential Time-of-Day (Rate 7)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_7", opts, []int{2026, 2027}), nil
				},
			},
			// New Hampshire
			{
				ID:   "eversource_nh_rate_r",
				Name: "NH Residential Standard (Rate R)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "eversource_nh_rate_r_otod",
				Name: "NH Residential Time-of-Day (Rate R-OTOD)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r_otod", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "eversource_nh_rate_r_ev",
				Name: "NH Residential Plug-in EV / Battery Storage TOU (Rate R-EV)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r_ev", opts, []int{2026, 2027}), nil
				},
			},
			// Massachusetts
			{
				ID:   "eversource_ma_residential",
				Name: "MA Residential Standard (Rate R-1)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ma_residential", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
