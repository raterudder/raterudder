package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// getSawneeHolidays returns observed holidays for Sawnee.
// Schedule TU/CPPR excludes July 4th from On-Peak.
func getSawneeHolidays(year int) []string {
	return []string{fmt.Sprintf("%d-07-04", year)}
}

// sawneePeriods generates pricing periods for Sawnee EMC.
func sawneePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getSawneeHolidays(year)

		switch plan {
		case "sawnee_h":
			// Schedule H-26 (Residential Service)
			// Flat rate estimate of $0.0767/kWh (first-tier rate).
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.0767,
					OtherDescription:   "Sawnee Schedule H Base Rate",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "sawnee_tu":
			// Schedule TU-14 (Time of Use - Residential)
			// On-Peak: $0.335/kWh (Monday - Friday, 2 PM - 8 PM, June 1 - August 31, excluding July 4)
			// Off-Peak: $0.0445/kWh (all other hours)
			peakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}}

			// Summer (June 1 - August 31) - Holiday (July 4th) (Off-Peak all day)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.August,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0445,
				OtherDescription:   "Sawnee Schedule TU Summer Holiday Off-Peak",
			}

			// Summer (June 1 - August 31) - Regular days
			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.August,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.335,
						Description:   "Sawnee Schedule TU Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0445,
				OtherDescription:   "Sawnee Schedule TU Summer Off-Peak",
			}

			// Winter/Shoulder (September 1 - May 31) - Off-Peak all day
			winterPeriod1 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.May,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0445,
				OtherDescription:   "Sawnee Schedule TU Off-Peak",
			}
			winterPeriod2 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.September,
				MonthEnd:           time.December,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0445,
				OtherDescription:   "Sawnee Schedule TU Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHolidayPeriod,
				summerRegularPeriod,
				winterPeriod1,
				winterPeriod2,
			})...)

		case "sawnee_cpp":
			// Schedule CPPR-14 (Critical Peak Pricing - Residential)
			// On-Peak: $0.286/kWh (Monday - Friday, 2 PM - 8 PM, June 1 - August 31, excluding July 4)
			// Off-Peak: $0.0425/kWh (all other hours)
			peakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}}

			// Summer (June 1 - August 31) - Holiday (July 4th) (Off-Peak all day)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.August,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0425,
				OtherDescription:   "Sawnee Schedule CPPR Summer Holiday Off-Peak",
			}

			// Summer (June 1 - August 31) - Regular days
			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.August,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.286,
						Description:   "Sawnee Schedule CPPR Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0425,
				OtherDescription:   "Sawnee Schedule CPPR Summer Off-Peak",
			}

			// Winter/Shoulder (September 1 - May 31) - Off-Peak all day
			winterPeriod1 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.May,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0425,
				OtherDescription:   "Sawnee Schedule CPPR Off-Peak",
			}
			winterPeriod2 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.September,
				MonthEnd:           time.December,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0425,
				OtherDescription:   "Sawnee Schedule CPPR Off-Peak",
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
				summerHolidayPeriod,
				summerRegularPeriod,
				winterPeriod1,
				winterPeriod2,
			})...)
		}

		// Export Credits under Net Metering Rider NEM-18
		// Solar Photovoltaic: Flat $0.0379/kWh year-round
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
				End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
				LocationPtr: etLocation,
			},
			DollarsPerKWH:            0.0379,
			SeparateGenerationCredit: true,
			Description:              "Sawnee NEM Solar Export Credit",
		})
	}

	return periods
}

// sawneeUtilityInfo returns metadata and options for Sawnee EMC.
func sawneeUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "sawnee",
		Name: "Sawnee EMC",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "sawnee_h",
				Name: "Residential Service (Schedule H)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sawneePeriods("sawnee_h", opts, []int{2026}), nil
				},
			},
			{
				ID:   "sawnee_tu",
				Name: "Residential Time-of-Use (Schedule TU)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sawneePeriods("sawnee_tu", opts, []int{2026}), nil
				},
			},
			{
				ID:   "sawnee_cpp",
				Name: "Critical Peak Pricing (Schedule CPPR)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sawneePeriods("sawnee_cpp", opts, []int{2026}), nil
				},
			},
		},
	}
}
