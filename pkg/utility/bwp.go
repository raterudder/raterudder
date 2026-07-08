package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// City of Burbank Adopted Citywide Fee Schedule, Article X, Section 11:
// "Net Surplus Electricity Compensation Rate" is currently $0.0455 per kWh.
// This is used for the Solar Net Billing program.
const bwpAvoidedCostRate = 0.0455

func shiftBWPSundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

func getBWPHolidays(year int) []string {
	mlk := martinLutherKingDay(year)
	pres := presidentsDay(year)
	// Dolores Huerta Day: March 31 (observed/shifted if Sunday)
	dolores := shiftBWPSundayHoliday(time.Date(year, time.March, 31, 0, 0, 0, 0, time.UTC))
	mem := memorialDay(year)
	june := shiftBWPSundayHoliday(juneteenth(year))
	labor := laborDay(year)
	vet := shiftBWPSundayHoliday(veteransDay(year))
	thanks := thanksgivingDay(year)
	// Day after Thanksgiving (Friday)
	thanksFriday := thanks.AddDate(0, 0, 1)

	holidays := []time.Time{
		shiftBWPSundayHoliday(newYearsDay(year)),
		mlk,
		pres,
		dolores,
		mem,
		june,
		shiftBWPSundayHoliday(independenceDay(year)),
		labor,
		vet,
		thanks,
		thanksFriday,
		shiftBWPSundayHoliday(christmasDay(year)),
	}
	return formatHolidays(holidays, year)
}

func bwpPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getBWPHolidays(year)

		switch plan {
		case "bwp_residential":
			// Rate: Residential Service (Flat)
			// Composite rate (under 300 kWh): $0.1800/kWh (Base $0.1460 + ECAC $0.0340)
			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.1800,
					OtherDescription:   "BWP Residential Flat Rate",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplified)...)

			// Solar Net Billing export credit: $0.0455/kWh
			if opts.NetMeteringScheme == "net_billing" {
				simplifiedExport := []touSimplifiedPeriod{
					{
						Year:                               year,
						MonthStart:                         time.January,
						MonthEnd:                           time.December,
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: bwpAvoidedCostRate,
						OtherDescription:                   "BWP Solar Net Billing Export Credit",
					},
				}
				periods = append(periods, buildPeriods(ptLocation, simplifiedExport)...)
			}

		case "bwp_res_tou_ev":
			// Rate: Optional TOU Rate for EV Owners (R-TOU-EV)
			// Summer: June 1 – October 31
			// Winter: November 1 – May 31

			// Summer rates (Composite = Base + $0.0340 ECAC):
			// On-Peak: $0.3562 + $0.0340 = $0.3902
			// Mid-Peak: $0.2277 + $0.0340 = $0.2617
			// Off-Peak: $0.1215 + $0.0340 = $0.1555
			summerOnPeak := 0.3562 + 0.0340
			summerMidPeak := 0.2277 + 0.0340
			summerOffPeak := 0.1215 + 0.0340

			// Winter / Non-Summer rates (Composite = Base + $0.0340 ECAC):
			// Mid-Peak: $0.2277 + $0.0340 = $0.2617
			// Off-Peak: $0.1215 + $0.0340 = $0.1555
			winterMidPeak := 0.2277 + 0.0340
			winterOffPeak := 0.1215 + 0.0340

			// On-Peak hours (Summer only): 4 PM - 7 PM (16:00 - 19:00) Weekdays
			summerOnPeakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}}
			// Mid-Peak hours (Summer): 8 AM - 4 PM & 7 PM - 11 PM (08:00 - 16:00 & 19:00 - 23:00) Weekdays
			summerMidPeakHours := []types.UtilityHourPeriod{
				{HourStart: 8, HourEnd: 16},
				{HourStart: 19, HourEnd: 23},
			}
			// Mid-Peak hours (Winter): 8 AM - 11 PM (08:00 - 23:00) Weekdays
			winterMidPeakHours := []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 23}}

			simplifiedConsumption := []touSimplifiedPeriod{
				// --- Summer ---
				// Summer Holiday / Weekend (all hours off-peak)
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					OtherDollarsPerKWH: summerOffPeak,
					OtherDescription:   "BWP R-TOU-EV Summer Off-Peak (Holiday/Weekend)",
				},
				// Summer Weekdays
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         summerOnPeakHours,
							Weekday:       true,
							DollarsPerKWH: summerOnPeak,
							Description:   "BWP R-TOU-EV Summer On-Peak",
						},
						{
							Hours:         summerMidPeakHours,
							Weekday:       true,
							DollarsPerKWH: summerMidPeak,
							Description:   "BWP R-TOU-EV Summer Mid-Peak",
						},
					},
					OtherDollarsPerKWH: summerOffPeak,
					OtherDescription:   "BWP R-TOU-EV Summer Off-Peak",
				},

				// --- Winter ---
				// Winter Holiday / Weekend (all hours off-peak)
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					OtherDollarsPerKWH: winterOffPeak,
					OtherDescription:   "BWP R-TOU-EV Winter Off-Peak (Holiday/Weekend)",
				},
				// Winter Weekdays
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.May,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         winterMidPeakHours,
							Weekday:       true,
							DollarsPerKWH: winterMidPeak,
							Description:   "BWP R-TOU-EV Winter Mid-Peak",
						},
					},
					OtherDollarsPerKWH: winterOffPeak,
					OtherDescription:   "BWP R-TOU-EV Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplifiedConsumption)...)

			// Solar Net Billing export credit: $0.0455/kWh
			if opts.NetMeteringScheme == "net_billing" {
				simplifiedExport := []touSimplifiedPeriod{
					{
						Year:                               year,
						MonthStart:                         time.January,
						MonthEnd:                           time.December,
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: bwpAvoidedCostRate,
						OtherDescription:                   "BWP Solar Net Billing Export Credit",
					},
				}
				periods = append(periods, buildPeriods(ptLocation, simplifiedExport)...)
			}
		}
	}
	return periods
}

func bwpUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Legacy Net Energy Metering (NEM)"},
			{Value: "net_billing", Name: "Solar Net Billing"},
		},
		Default: "net_billing",
	}

	return types.UtilityProviderInfo{
		ID:   "bwp",
		Name: "Burbank Water and Power (BWP)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "bwp_residential",
				Name:    "Residential Service (Flat)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bwpPeriods("bwp_residential", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "bwp_res_tou_ev",
				Name:    "Residential Time-of-Use EV (R-TOU-EV)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bwpPeriods("bwp_res_tou_ev", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
