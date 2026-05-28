package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// GP Renewable Non-Utility Generator (RNR) Solar Avoided Energy Cost
// filed in Georgia Power Docket No. 16573:
// 2026: $32.19/MWh ($0.03219/kWh) + $0.04/kWh Renewable Adder = $0.07219/kWh
// 2027: $34.71/MWh ($0.03471/kWh) + $0.04/kWh Renewable Adder = $0.07471/kWh
var gpInstantaneousRates = map[int]float64{
	2026: 0.07219,
	2027: 0.07471,
}

func shiftGPWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getGPHolidays(year int) []string {
	var holidays []time.Time

	// Independence Day: July 4 (shifted Saturday-to-Friday, Sunday-to-Monday)
	july4 := time.Date(year, time.July, 4, 0, 0, 0, 0, time.UTC)
	holidays = append(holidays, shiftGPWeekendHoliday(july4))

	// Labor Day: first Monday in September
	laborDay := time.Date(year, time.September, 1, 0, 0, 0, 0, time.UTC)
	for laborDay.Weekday() != time.Monday {
		laborDay = laborDay.AddDate(0, 0, 1)
	}
	holidays = append(holidays, laborDay)

	var holidayStrings []string
	for _, h := range holidays {
		if h.Year() == year {
			holidayStrings = append(holidayStrings, h.Format("2006-01-02"))
		}
	}
	return holidayStrings
}

func gpPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	loc := etLocation

	for _, year := range years {
		holidays := getGPHolidays(year)

		switch plan {
		case "gp_tou_oa_14":
			// Summer (June - September)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.June,
				MonthEnd:      time.September,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// Off-Peak: 7 AM - 11 PM
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						DollarsPerKWH: 0.101676,
						Description:   "Summer Holiday Off-Peak",
					},
				},
				OtherDollarsPerKWH: 0.021859,
				OtherDescription:   "Summer Holiday Super Off-Peak",
			}

			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// On-Peak: 2 PM - 7 PM, weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: 0.297868,
						Description:   "Summer On-Peak",
					},
					{
						// Off-Peak: 7 AM - 2 PM, 7 PM - 11 PM weekdays
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 14},
							{HourStart: 19, HourEnd: 23},
						},
						Weekday:       true,
						DollarsPerKWH: 0.101676,
						Description:   "Summer Weekday Off-Peak",
					},
					{
						// Off-Peak: 7 AM - 11 PM weekends
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						Weekend:       true,
						DollarsPerKWH: 0.101676,
						Description:   "Summer Weekend Off-Peak",
					},
				},
				OtherDollarsPerKWH: 0.021859,
				OtherDescription:   "Summer Super Off-Peak",
			}

			// Winter (October - May)
			winterPeriod := touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// Off-Peak: 7 AM - 11 PM
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						DollarsPerKWH: 0.101676,
						Description:   "Winter Off-Peak",
					},
				},
				OtherDollarsPerKWH: 0.021859,
				OtherDescription:   "Winter Super Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod, winterPeriod})...)

		case "gp_tou_rd_11":
			// Summer (June - September)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: 0.015288,
				OtherDescription:   "Summer Holiday Off-Peak",
			}

			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// On-Peak: 2 PM - 7 PM weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: 0.142986,
						Description:   "Summer On-Peak",
					},
				},
				OtherDollarsPerKWH: 0.015288,
				OtherDescription:   "Summer Off-Peak",
			}

			// Winter (October - May)
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.May,
				OtherDollarsPerKWH: 0.015288,
				OtherDescription:   "Winter Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod, winterPeriod})...)

		case "gp_tou_reo_18":
			// Summer (June - September)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: 0.076281,
				OtherDescription:   "Summer Holiday Off-Peak",
			}

			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// On-Peak: 2 PM - 7 PM weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: 0.297868,
						Description:   "Summer On-Peak",
					},
				},
				OtherDollarsPerKWH: 0.076281,
				OtherDescription:   "Summer Off-Peak",
			}

			// Winter (October - May)
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.May,
				OtherDollarsPerKWH: 0.076281,
				OtherDescription:   "Winter Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod, winterPeriod})...)
		}
	}

	// Add dynamic NBT export rates for RNR-Instantaneous Netting scheme
	// Note: gp_monthly is handled implictly by it having a value of "net"
	if options.NetMeteringScheme == "gp_instantaneous" || options.NetMeteringScheme == "" {
		for _, year := range years {
			rate, ok := gpInstantaneousRates[year]
			if !ok {
				rate = gpInstantaneousRates[2026] // fallback
			}
			periods = append(periods, types.UtilityFeesPeriod{
				UtilityPeriod: types.UtilityPeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, loc),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, loc),
					LocationPtr: loc,
				},
				DollarsPerKWH:            rate,
				SeparateGenerationCredit: true,
				Description:              fmt.Sprintf("GP RNR Instantaneous Export Credit (%d)", year),
			})
		}
	}

	return periods
}
