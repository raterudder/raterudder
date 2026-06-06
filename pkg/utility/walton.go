package utility

// Walton EMC rates can be found at https://psc.ga.gov/search/facts-docket/?docketId=31536

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// waltonPeriods generates pricing periods for Walton EMC.
func waltonPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	locStr := etLocation.String() // Walton EMC is in Georgia (Eastern Time)

	for _, year := range years {
		switch plan {
		case "walton_a15":
			// Schedule A-15 (Residential Service)
			// Winter (November - April): First-tier rate of $0.1205/kWh
			// Summer (May - October): First-tier rate of $0.1225/kWh
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.April,
					OtherDollarsPerKWH: 0.1205,
					OtherDescription:   "Walton Schedule A-15 Winter Base Rate",
				},
				{
					Year:               year,
					MonthStart:         time.May,
					MonthEnd:           time.October,
					OtherDollarsPerKWH: 0.1225,
					OtherDescription:   "Walton Schedule A-15 Summer Base Rate",
				},
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.1205,
					OtherDescription:   "Walton Schedule A-15 Winter Base Rate",
				},
			}
			periods = append(periods, buildPeriods(locStr, simplified)...)

		case "walton_tu5":
			// Schedule TU-5 (Residential Time-of-Use Service)
			// On-Peak: $0.3225/kWh (Monday - Friday, 3 PM - 8 PM, June 1 - September 10, excluding July 4th and Labor Day)
			// Off-Peak: $0.0885/kWh (all other hours/months)
			peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 20}}
			july4Str := independenceDay(year).Format("2006-01-02")
			laborDayStr := laborDay(year).Format("2006-01-02")

			// 1. Off-Peak Months (Jan 1 - May 31, Oct 1 - Dec 31)
			offPeakMonths1 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.May,
				OtherDollarsPerKWH: 0.0885,
				OtherDescription:   "Walton Schedule TU-5 Off-Peak",
			}
			offPeakMonths2 := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.December,
				OtherDollarsPerKWH: 0.0885,
				OtherDescription:   "Walton Schedule TU-5 Off-Peak",
			}

			// 2. June 1 - August 31 (Summer Holiday & Regular days)
			summerHoliday := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.August,
				SpecificDates:      []string{july4Str},
				OtherDollarsPerKWH: 0.0885,
				OtherDescription:   "Walton Schedule TU-5 Summer Holiday Off-Peak",
			}
			summerRegular := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.August,
				SpecificDates:    []string{july4Str},
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.3225,
						Description:   "Walton Schedule TU-5 Summer On-Peak",
					},
				},
				OtherDollarsPerKWH: 0.0885,
				OtherDescription:   "Walton Schedule TU-5 Summer Off-Peak",
			}

			// 3. September 1 - September 10 (excluding Labor Day)
			var septPeakDates []string
			for d := 1; d <= 10; d++ {
				dateStr := fmt.Sprintf("%d-09-%02d", year, d)
				if dateStr != laborDayStr {
					septPeakDates = append(septPeakDates, dateStr)
				}
			}

			septPeakPeriod := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    time.September,
				MonthEnd:      time.September,
				SpecificDates: septPeakDates,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         peakHours,
						Weekday:       true,
						DollarsPerKWH: 0.3225,
						Description:   "Walton Schedule TU-5 Summer On-Peak",
					},
				},
				OtherDollarsPerKWH: 0.0885,
				OtherDescription:   "Walton Schedule TU-5 Summer Off-Peak",
			}

			// 4. September remaining days (Labor Day + Sept 11 - Sept 30)
			var septOffPeakDates []string
			septOffPeakDates = append(septOffPeakDates, laborDayStr)
			for d := 11; d <= 30; d++ {
				septOffPeakDates = append(septOffPeakDates, fmt.Sprintf("%d-09-%02d", year, d))
			}

			septOffPeakPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.September,
				MonthEnd:           time.September,
				SpecificDates:      septOffPeakDates,
				OtherDollarsPerKWH: 0.0885,
				OtherDescription:   "Walton Schedule TU-5 Off-Peak",
			}

			periods = append(periods, buildPeriods(locStr, []touSimplifiedPeriod{
				offPeakMonths1,
				offPeakMonths2,
				summerHoliday,
				summerRegular,
				septPeakPeriod,
				septOffPeakPeriod,
			})...)
		}

		// Export Credits - Avoided Energy Cost
		// Avoided Energy Cost rate: Flat $0.026/kWh year-round
		periods = append(periods, types.UtilityFeesPeriod{
			UtilityPeriod: types.UtilityPeriod{
				Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
				End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
				LocationPtr: etLocation,
			},
			DollarsPerKWH:            0.026,
			SeparateGenerationCredit: true,
			Description:              "Walton EMC Avoided Energy Cost Export Credit",
		})
	}

	return periods
}

// waltonUtilityInfo returns metadata for Walton EMC.
func waltonUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "walton",
		Name: "Walton EMC",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "walton_a15",
				Name: "Residential Service (Schedule A-15)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return waltonPeriods("walton_a15", opts, []int{2026}), nil
				},
			},
			{
				ID:   "walton_tu5",
				Name: "Residential Time-of-Use (Schedule TU-5)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return waltonPeriods("walton_tu5", opts, []int{2026}), nil
				},
			},
		},
	}
}
