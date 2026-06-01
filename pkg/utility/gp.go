package utility

import (
	"fmt"
	"slices"
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

type gpOARates struct {
	SummerOnPeak   float64
	SummerOffPeak  float64
	SummerSuperOff float64
	WinterOffPeak  float64
	WinterSuperOff float64
}

type gpRDRates struct {
	SummerOnPeak  float64
	SummerOffPeak float64
	WinterOffPeak float64
}

type gpREORates struct {
	SummerOnPeak  float64
	SummerOffPeak float64
	WinterOffPeak float64
}

var gpOAVersions = map[int]gpOARates{
	// TOU-OA-14: Applicable to billing months prior to June 2026 (starting from January 2025)
	14: {
		SummerOnPeak:   0.297868,
		SummerOffPeak:  0.101676,
		SummerSuperOff: 0.021859,
		WinterOffPeak:  0.101676,
		WinterSuperOff: 0.021859,
	},
	// TOU-OA-15: Applicable to billing months June 2026 onwards
	15: {
		SummerOnPeak:   0.303495,
		SummerOffPeak:  0.103598,
		SummerSuperOff: 0.022272,
		WinterOffPeak:  0.103598,
		WinterSuperOff: 0.022272,
	},
}

var gpRDVersions = map[int]gpRDRates{
	// TOU-RD-11: Applicable to billing months prior to June 2026
	11: {
		SummerOnPeak:  0.142986,
		SummerOffPeak: 0.015288,
		WinterOffPeak: 0.015288,
	},
	// TOU-RD-12: Applicable to billing months June 2026 onwards
	12: {
		SummerOnPeak:  0.145620,
		SummerOffPeak: 0.015569,
		WinterOffPeak: 0.015569,
	},
}

var gpREOVersions = map[int]gpREORates{
	// TOU-REO-18: Applicable to billing months prior to June 2026
	18: {
		SummerOnPeak:  0.297868,
		SummerOffPeak: 0.076281,
		WinterOffPeak: 0.076281,
	},
	// TOU-REO-19: Applicable to billing months June 2026 onwards (renamed to Nights & Weekends)
	19: {
		SummerOnPeak:  0.303495,
		SummerOffPeak: 0.077702,
		WinterOffPeak: 0.077702,
	},
}

// ECCR percentage rates (as multipliers)
const (
	eccrMultiplierJan26 = 1.132343 // ECCR-14: 13.2343%
	eccrMultiplierJun26 = 1.130205 // ECCR-15p: 13.0205%
)

// TODO: Support asking the user for their location (inside/outside city limits) to determine
// the exact Municipal Franchise Fee (MFF). Inside city limits is 3.0843% (multiplier 1.030843),
// and outside city limits is 1.1995% (multiplier 1.011995).
// For now, we conservatively use the outside city limits rate of 1.1995% (multiplier 1.011995) to prevent overestimating savings.
const mffMultiplier = 1.011995

var (
	summerMonths = []time.Month{time.June, time.July, time.August, time.September}
	winterMonths = []time.Month{time.October, time.November, time.December, time.January, time.February, time.March, time.April, time.May}
)

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
	holidays := []time.Time{
		shiftGPWeekendHoliday(independenceDay(year)),
		laborDay(year),
	}

	return formatHolidays(holidays, year)
}

func intersectMonths(months []time.Month, allowed []time.Month) []time.Month {
	if len(allowed) == 0 {
		return months
	}
	var res []time.Month
	for _, m := range months {
		if slices.Contains(allowed, m) {
			res = append(res, m)
		}
	}
	return res
}

func gpPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	// Handle backwards compatibility for old plan IDs (excluding utility info)
	if plan == "gp_tou_oa_14" {
		plan = "gp_tou_oa"
	} else if plan == "gp_tou_rd_11" {
		plan = "gp_tou_rd"
	} else if plan == "gp_tou_reo_18" {
		plan = "gp_tou_reo"
	}

	var periods []types.UtilityFeesPeriod
	loc := etLocation

	for _, year := range years {
		holidays := getGPHolidays(year)

		type yearBlock struct {
			months         []time.Month
			eccrMultiplier float64
			versionDiff    int
		}

		var blocks []yearBlock
		if year < 2026 {
			blocks = []yearBlock{
				{months: nil, eccrMultiplier: eccrMultiplierJan26, versionDiff: 0},
			}
		} else if year == 2026 {
			blocks = []yearBlock{
				{
					months:         []time.Month{time.January, time.February, time.March, time.April, time.May},
					eccrMultiplier: eccrMultiplierJan26,
					versionDiff:    0,
				},
				{
					months:         []time.Month{time.June, time.July, time.August, time.September, time.October, time.November, time.December},
					eccrMultiplier: eccrMultiplierJun26,
					versionDiff:    1,
				},
			}
		} else {
			blocks = []yearBlock{
				{months: nil, eccrMultiplier: eccrMultiplierJun26, versionDiff: 1},
			}
		}

		for _, block := range blocks {
			// 1. Build base simplified periods with ECCR applied
			switch plan {
			case "gp_tou_oa":
				r := gpOAVersions[14+block.versionDiff]
				onPeakRate := r.SummerOnPeak * block.eccrMultiplier * mffMultiplier
				offPeakRate := r.SummerOffPeak * block.eccrMultiplier * mffMultiplier
				superOffPeakRate := r.SummerSuperOff * block.eccrMultiplier * mffMultiplier
				winterOffPeakRate := r.WinterOffPeak * block.eccrMultiplier * mffMultiplier
				winterSuperOffPeakRate := r.WinterSuperOff * block.eccrMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				if len(summerIntersection) > 0 {
					summerHolidayPeriod := touSimplifiedPeriod{
						Year:          year,
						Months:        summerIntersection,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								// Off-Peak: 7 AM - 11 PM
								Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
								DollarsPerKWH: offPeakRate,
								Description:   "Summer Holiday Off-Peak",
							},
						},
						OtherDollarsPerKWH: superOffPeakRate,
						OtherDescription:   "Summer Holiday Super Off-Peak",
					}

					summerRegularPeriod := touSimplifiedPeriod{
						Year:             year,
						Months:           summerIntersection,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								// On-Peak: 2 PM - 7 PM, weekdays
								Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: onPeakRate,
								Description:   "Summer On-Peak",
							},
							{
								// Off-Peak: 7 AM - 2 PM, 7 PM - 11 PM weekdays
								Hours: []types.UtilityHourPeriod{
									{HourStart: 7, HourEnd: 14},
									{HourStart: 19, HourEnd: 23},
								},
								Weekday:       true,
								DollarsPerKWH: offPeakRate,
								Description:   "Summer Weekday Off-Peak",
							},
							{
								// Off-Peak: 7 AM - 11 PM weekends
								Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
								Weekend:       true,
								DollarsPerKWH: offPeakRate,
								Description:   "Summer Weekend Off-Peak",
							},
						},
						OtherDollarsPerKWH: superOffPeakRate,
						OtherDescription:   "Summer Super Off-Peak",
					}

					periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:   year,
						Months: winterIntersection,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								// Off-Peak: 7 AM - 11 PM
								Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
								DollarsPerKWH: winterOffPeakRate,
								Description:   "Winter Off-Peak",
							},
						},
						OtherDollarsPerKWH: winterSuperOffPeakRate,
						OtherDescription:   "Winter Super Off-Peak",
					}

					periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{winterPeriod})...)
				}

			case "gp_tou_rd":
				r := gpRDVersions[11+block.versionDiff]
				onPeakRate := r.SummerOnPeak * block.eccrMultiplier * mffMultiplier
				offPeakRate := r.SummerOffPeak * block.eccrMultiplier * mffMultiplier
				winterOffPeakRate := r.WinterOffPeak * block.eccrMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				if len(summerIntersection) > 0 {
					summerHolidayPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             summerIntersection,
						SpecificDates:      holidays,
						OtherDollarsPerKWH: offPeakRate,
						OtherDescription:   "Summer Holiday Off-Peak",
					}

					summerRegularPeriod := touSimplifiedPeriod{
						Year:             year,
						Months:           summerIntersection,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								// On-Peak: 2 PM - 7 PM weekdays
								Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: onPeakRate,
								Description:   "Summer On-Peak",
							},
						},
						OtherDollarsPerKWH: offPeakRate,
						OtherDescription:   "Summer Off-Peak",
					}

					periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             winterIntersection,
						OtherDollarsPerKWH: winterOffPeakRate,
						OtherDescription:   "Winter Off-Peak",
					}

					periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{winterPeriod})...)
				}

			case "gp_tou_reo":
				r := gpREOVersions[18+block.versionDiff]
				onPeakRate := r.SummerOnPeak * block.eccrMultiplier * mffMultiplier
				offPeakRate := r.SummerOffPeak * block.eccrMultiplier * mffMultiplier
				winterOffPeakRate := r.WinterOffPeak * block.eccrMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				if len(summerIntersection) > 0 {
					summerHolidayPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             summerIntersection,
						SpecificDates:      holidays,
						OtherDollarsPerKWH: offPeakRate,
						OtherDescription:   "Summer Holiday Off-Peak",
					}

					summerRegularPeriod := touSimplifiedPeriod{
						Year:             year,
						Months:           summerIntersection,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								// On-Peak: 2 PM - 7 PM weekdays
								Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: onPeakRate,
								Description:   "Summer On-Peak",
							},
						},
						OtherDollarsPerKWH: offPeakRate,
						OtherDescription:   "Summer Off-Peak",
					}

					periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             winterIntersection,
						OtherDollarsPerKWH: winterOffPeakRate,
						OtherDescription:   "Winter Off-Peak",
					}

					periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{winterPeriod})...)
				}
			}

			// 2. Build and append FCR fee periods for this block
			periods = append(periods, gpFCRPeriods(plan, year, block.months, block.versionDiff, holidays)...)
		}
	}

	// Add dynamic NBT export rates for RNR-Instantaneous Netting scheme
	// Note: gp_monthly is handled implicitly by it having a value of "net"
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

func gpFCRPeriods(plan string, year int, months []time.Month, versionDiff int, holidays []string) []types.UtilityFeesPeriod {
	// Handle backwards compatibility for old plan IDs
	if plan == "gp_tou_oa_14" {
		plan = "gp_tou_oa"
	} else if plan == "gp_tou_rd_11" {
		plan = "gp_tou_rd"
	} else if plan == "gp_tou_reo_18" {
		plan = "gp_tou_reo"
	}

	var periods []types.UtilityFeesPeriod
	loc := etLocation

	if plan == "gp_tou_oa" {
		var onPeakRate, offPeakRate, superOffPeakRate float64
		var suffix string
		if versionDiff == 0 {
			onPeakRate, offPeakRate, superOffPeakRate = 0.066871*mffMultiplier, 0.044284*mffMultiplier, 0.038252*mffMultiplier
			suffix = " (FCR-26)"
		} else {
			onPeakRate, offPeakRate, superOffPeakRate = 0.052269*mffMultiplier, 0.038690*mffMultiplier, 0.034747*mffMultiplier
			suffix = " (FCR-27)"
		}

		summerIntersection := intersectMonths(summerMonths, months)
		winterIntersection := intersectMonths(winterMonths, months)

		if len(summerIntersection) > 0 {
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:          year,
				Months:        summerIntersection,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						DollarsPerKWH: offPeakRate,
						Description:   "Summer Holiday FCR Off-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: superOffPeakRate,
				OtherDescription:   "Summer Holiday FCR Super Off-Peak" + suffix,
			}

			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				Months:           summerIntersection,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: onPeakRate,
						Description:   "Summer FCR On-Peak" + suffix,
					},
					{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 14},
							{HourStart: 19, HourEnd: 23},
						},
						Weekday:       true,
						DollarsPerKWH: offPeakRate,
						Description:   "Summer Weekday FCR Off-Peak" + suffix,
					},
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						Weekend:       true,
						DollarsPerKWH: offPeakRate,
						Description:   "Summer Weekend FCR Off-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: superOffPeakRate,
				OtherDescription:   "Summer FCR Super Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
		}

		if len(winterIntersection) > 0 {
			winterPeriod := touSimplifiedPeriod{
				Year:   year,
				Months: winterIntersection,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						DollarsPerKWH: offPeakRate,
						Description:   "Winter FCR Off-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: superOffPeakRate,
				OtherDescription:   "Winter FCR Super Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{winterPeriod})...)
		}
	} else {
		// two-part FCR (TOU-FCR-6 / TOU-FCR-7) for RD and REO
		var onPeakRate, offPeakRate float64
		var suffix string
		if versionDiff == 0 {
			onPeakRate, offPeakRate = 0.066871*mffMultiplier, 0.042398*mffMultiplier
			suffix = " (FCR-26)"
		} else {
			onPeakRate, offPeakRate = 0.052269*mffMultiplier, 0.037441*mffMultiplier
			suffix = " (FCR-27)"
		}

		summerIntersection := intersectMonths(summerMonths, months)
		winterIntersection := intersectMonths(winterMonths, months)

		if len(summerIntersection) > 0 {
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				Months:             summerIntersection,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "Summer Holiday FCR Off-Peak" + suffix,
			}

			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				Months:           summerIntersection,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: onPeakRate,
						Description:   "Summer FCR On-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "Summer FCR Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
		}

		if len(winterIntersection) > 0 {
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				Months:             winterIntersection,
				OtherDollarsPerKWH: offPeakRate,
				OtherDescription:   "Winter FCR Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(loc.String(), []touSimplifiedPeriod{winterPeriod})...)
		}
	}

	// Mark all of these as GridAdditional
	for i := range periods {
		periods[i].GridAdditional = true
	}

	return periods
}

// gpUtilityInfo returns the metadata and rate options for Georgia Power.
func gpUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "gp",
		Name: "Georgia Power",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "gp_tou_oa",
				Name: "TOU-OA (Overnight Advantage)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "gp_instantaneous", Name: "Instantaneous Netting (RNR)"},
							{Value: "net", Name: "Monthly Netting (RNR)"},
						},
						Default: "gp_instantaneous",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_tou_oa", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "gp_tou_rd",
				Name: "TOU-RD (Residential Demand)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "gp_instantaneous", Name: "Instantaneous Netting (RNR)"},
							{Value: "net", Name: "Monthly Netting (RNR)"},
						},
						Default: "gp_instantaneous",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_tou_rd", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "gp_tou_reo",
				Name: "TOU-REO (Nights & Weekends)",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your net metering or solar billing plan program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "gp_instantaneous", Name: "Instantaneous Netting (RNR)"},
							{Value: "net", Name: "Monthly Netting (RNR)"},
						},
						Default: "gp_instantaneous",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_tou_reo", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
