package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftIdahoSundayHoliday shifts a holiday to Monday if it falls on a Sunday.
func shiftIdahoSundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

// getIdahoHolidays returns observed holidays for Idaho Power.
// If New Year's Day, Independence Day, or Christmas Day falls on a Sunday, the following Monday is observed.
func getIdahoHolidays(year int) []string {
	holidays := []time.Time{
		shiftIdahoSundayHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftIdahoSundayHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftIdahoSundayHoliday(christmasDay(year)),
	}
	return formatHolidays(holidays, year)
}

// idahoPeriods generates pricing periods for Idaho Power.
func idahoPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	locStr := mtLocation.String() // Idaho Power operates in Mountain Time (America/Denver)

	for _, year := range years {
		holidays := getIdahoHolidays(year)

		switch plan {
		case "idaho_std":
			// Schedule 1 (Standard Plan)
			// Summer (June 1 - September 30): Flat rate estimate of $0.121195/kWh
			// Non-Summer (October 1 - May 31): Flat rate estimate of $0.099332/kWh
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.099332,
					OtherDescription:   "Idaho Power Standard Plan Non-Summer Rate",
				},
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.121195,
					OtherDescription:   "Idaho Power Standard Plan Summer Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.099332,
					OtherDescription:   "Idaho Power Standard Plan Non-Summer Rate",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "idaho_tou":
			// Schedule 6 (Time-of-Use Plan)
			// Summer (June 1 - September 30):
			//   On-Peak: $0.299185/kWh (Mon-Sat 7 PM - 11 PM, excl. holidays)
			//   Mid-Peak: $0.149594/kWh (Mon-Sat 3 PM - 7 PM, excl. holidays)
			//   Off-Peak: $0.074797/kWh (all other times)
			// Non-Summer (October 1 - May 31):
			//   On-Peak: $0.138347/kWh (Mon-Sat 6 AM - 9 AM & 5 PM - 8 PM, excl. holidays)
			//   Off-Peak: $0.092231/kWh (all other times)
			summerHoliday := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: 0.074797,
				OtherDescription:   "Idaho Power TOU Summer Holiday Off-Peak",
			}
			summerRegular := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 19, HourEnd: 23}},
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.299185,
						Description:   "Idaho Power TOU Summer On-Peak",
					},
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.149594,
						Description:   "Idaho Power TOU Summer Mid-Peak",
					},
				},
				OtherDollarsPerKWH: 0.074797,
				OtherDescription:   "Idaho Power TOU Summer Off-Peak",
			}

			winterHoliday1 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.May,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: 0.092231,
				OtherDescription:   "Idaho Power TOU Non-Summer Holiday Off-Peak",
			}
			winterRegular1 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.May,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 6, HourEnd: 9},
							{HourStart: 17, HourEnd: 20},
						},
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.138347,
						Description:   "Idaho Power TOU Non-Summer On-Peak",
					},
				},
				OtherDollarsPerKWH: 0.092231,
				OtherDescription:   "Idaho Power TOU Non-Summer Off-Peak",
			}

			winterHoliday2 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.December,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: 0.092231,
				OtherDescription:   "Idaho Power TOU Non-Summer Holiday Off-Peak",
			}
			winterRegular2 := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.October,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 6, HourEnd: 9},
							{HourStart: 17, HourEnd: 20},
						},
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						DollarsPerKWH: 0.138347,
						Description:   "Idaho Power TOU Non-Summer On-Peak",
					},
				},
				OtherDollarsPerKWH: 0.092231,
				OtherDescription:   "Idaho Power TOU Non-Summer Off-Peak",
			}

			periods = append(periods, buildPeriods(locStr, []touSimplifiedPeriod{
				summerHoliday,
				summerRegular,
				winterHoliday1,
				winterRegular1,
				winterHoliday2,
				winterRegular2,
			})...)
		}

		// Export scheme: "on_site" (Residential Service On-Site Generation Net Billing)
		// "net" (Standard 1:1 Net Metering) is handled automatically at controller level without separate credit periods.
		scheme := opts.NetMeteringScheme
		if scheme == "" {
			scheme = "on_site"
		}

		if scheme == "on_site" {
			// Summer (June 1 - September 30):
			//   On-Peak: $0.156836/kWh (Mon-Sat 3 PM - 11 PM, excl. holidays)
			//   Off-Peak: $0.033920/kWh (all other times)
			summerExportHoliday := touSimplifiedPeriod{
				Year:                               year,
				MonthStart:                         time.June,
				MonthEnd:                           time.September,
				SpecificDates:                      holidays,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: 0.033920,
				OtherDescription:                   "Idaho Power Net Billing Summer Holiday Export Credit",
			}
			summerExportRegular := touSimplifiedPeriod{
				Year:                         year,
				MonthStart:                   time.June,
				MonthEnd:                     time.September,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:                         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 23}},
						DaysOfTheWeek:                 []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
						GenerationCreditDollarsPerKWH: 0.156836,
						Description:                   "Idaho Power Net Billing Summer On-Peak Export Credit",
					},
				},
				OtherGenerationCreditDollarsPerKWH: 0.033920,
				OtherDescription:                   "Idaho Power Net Billing Summer Off-Peak Export Credit",
			}

			// Non-Summer (October 1 - May 31):
			//   Off-Peak: $0.029019/kWh (all hours)
			winterExport1 := touSimplifiedPeriod{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.May,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: 0.029019,
				OtherDescription:                   "Idaho Power Net Billing Non-Summer Export Credit",
			}
			winterExport2 := touSimplifiedPeriod{
				Year:                               year,
				MonthStart:                         time.October,
				MonthEnd:                           time.December,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: 0.029019,
				OtherDescription:                   "Idaho Power Net Billing Non-Summer Export Credit",
			}

			periods = append(periods, buildPeriods(locStr, []touSimplifiedPeriod{
				summerExportHoliday,
				summerExportRegular,
				winterExport1,
				winterExport2,
			})...)
		}
	}

	return periods
}

// idahoUtilityInfo returns metadata for Idaho Power.
func idahoUtilityInfo() types.UtilityProviderInfo {
	idahoOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your net metering or solar billing plan program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "on_site", Name: "Residential Service On-Site Generation"},
				{Value: "net", Name: "Standard Net Metering (1:1)"},
			},
			Default: "on_site",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "idaho",
		Name: "Idaho Power",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "idaho_std",
				Name:    "Residential Service Standard Plan (Schedule 1)",
				Options: idahoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return idahoPeriods("idaho_std", opts, []int{2026}), nil
				},
			},
			{
				ID:      "idaho_tou",
				Name:    "Residential Service On-Site Generation (Schedule 6 Time-of-Use)",
				Options: idahoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return idahoPeriods("idaho_tou", opts, []int{2026}), nil
				},
			},
		},
	}
}
