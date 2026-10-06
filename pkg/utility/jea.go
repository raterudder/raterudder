package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftJEAWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getJEAHolidays(year int) []string {
	holidays := []time.Time{
		shiftJEAWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftJEAWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftJEAWeekendHoliday(christmasDay(year)),
		shiftJEAWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

// Comes from https://www.jea.com/rates "Fuel Rates" and Electric Tariff effective October 1, 2026
func getJEAFuelCharge(year int, month time.Month) float64 {
	if year == 2026 {
		switch month {
		case time.January:
			return 0.04224
		case time.February:
			return 0.04144
		case time.March:
			return 0.04973
		case time.April:
			return 0.05968
		case time.May:
			return 0.05863
		case time.June:
			return 0.04494
		case time.July:
			return 0.04386
		case time.August, time.September:
			return 0.04282
		case time.October:
			return 0.04326
		}
	}
	return 0.04326
}

type jeaBaseRates struct {
	rsBase         float64
	gstOnPeakBase  float64
	gstOffPeakBase float64
}

// JEA base rates are established by approved tariff documents (e.g. Electric Tariff Sheet 4.0 & 5.1).
// These change with periodic tariff revisions (such as at the start of JEA fiscal years on Oct 1),
// not with monthly fuel cost adjustments.
var jeaBaseRateSchedules = []struct {
	effectiveDate time.Time
	rates         jeaBaseRates
}{
	// Prior to October 1, 2026
	{
		effectiveDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		rates: jeaBaseRates{
			rsBase:         0.07237,
			gstOnPeakBase:  0.13776,
			gstOffPeakBase: 0.04535,
		},
	},
	// Effective October 1, 2026 (Electric Tariff Document October 2026, Sheets 4.0 and 5.1)
	{
		effectiveDate: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		rates: jeaBaseRates{
			rsBase:         0.07815,
			gstOnPeakBase:  0.14195,
			gstOffPeakBase: 0.04732,
		},
	},
}

func getJEABaseRates(year int, month time.Month) jeaBaseRates {
	target := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	var effDate time.Time
	chosen := jeaBaseRateSchedules[0].rates
	// start at the end because the latest ones are at the bottom and find the newest rates
	// that have an effective date <= target month
	for i := len(jeaBaseRateSchedules) - 1; i >= 0; i-- {
		schedule := jeaBaseRateSchedules[i]
		if !target.Before(schedule.effectiveDate) && schedule.effectiveDate.After(effDate) {
			chosen = schedule.rates
			effDate = schedule.effectiveDate
		}
	}
	return chosen
}

func jeaPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getJEAHolidays(year)

		for month := time.January; month <= time.December; month++ {
			fuelCharge := getJEAFuelCharge(year, month)
			baseRates := getJEABaseRates(year, month)

			switch plan {
			case "jea_r":
				// Rate R (Residential Service)
				rate := baseRates.rsBase + fuelCharge

				simplified := []touSimplifiedPeriod{
					{
						Year:               year,
						Months:             []time.Month{month},
						OtherDollarsPerKWH: rate,
						OtherDescription:   "JEA RS Energy Charge",
					},
				}

				if opts.NetMeteringScheme == "dg" {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:                               year,
						Months:                             []time.Month{month},
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: fuelCharge,
						OtherDescription:                   "JEA DG Generation Credit",
					})
				}

				periods = append(periods, buildPeriods(etLocation, simplified)...)

			case "jea_gst":
				// Rate GST (General Service Time-of-Day)
				onPeakRate := baseRates.gstOnPeakBase + fuelCharge
				offPeakRate := baseRates.gstOffPeakBase + fuelCharge

				isWinter := month == time.November || month == time.December || month == time.January || month == time.February || month == time.March

				var onPeakHours []types.UtilityHourPeriod
				if isWinter {
					onPeakHours = []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 10},
						{HourStart: 18, HourEnd: 22}, // 6 p.m. to 10 p.m.
					}
				} else {
					onPeakHours = []types.UtilityHourPeriod{
						{HourStart: 12, HourEnd: 21}, // 12 p.m. to 9 p.m.
					}
				}

				simplified := []touSimplifiedPeriod{
					{
						Year:               year,
						Months:             []time.Month{month},
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakRate,
						OtherDescription:   "JEA GST Off-Peak (Holiday)",
					},
					{
						Year:             year,
						Months:           []time.Month{month},
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         onPeakHours,
								Weekday:       true,
								DollarsPerKWH: onPeakRate,
								Description:   "JEA GST On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakRate,
						OtherDescription:   "JEA GST Off-Peak",
					},
				}

				if opts.NetMeteringScheme == "dg" {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:                               year,
						Months:                             []time.Month{month},
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: fuelCharge,
						OtherDescription:                   "JEA DG Generation Credit",
					})
				}

				periods = append(periods, buildPeriods(etLocation, simplified)...)
			}
		}
	}
	return periods
}

func jeaUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Energy Metering"},
			{Value: "dg", Name: "Distributed Generation"},
		},
		Default: "net",
	}

	return types.UtilityProviderInfo{
		ID:   "jea",
		Name: "JEA (Jacksonville)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "jea_r",
				Name:    "Rate R (Residential Service)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return jeaPeriods("jea_r", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "jea_gst",
				Name:    "Rate GST (General Service Time-of-Day)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return jeaPeriods("jea_gst", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
