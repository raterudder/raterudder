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

// GP base rates (before ECCR is applied)
const (
	// Overnight Advantage (TOU-OA-14)
	oaSummerOnPeak   = 0.297868
	oaSummerOffPeak  = 0.101676
	oaSummerSuperOff = 0.021859
	oaWinterOffPeak  = 0.101676
	oaWinterSuperOff = 0.021859

	// Residential Demand (TOU-RD-11)
	rdSummerOnPeak  = 0.142986
	rdSummerOffPeak = 0.015288
	rdWinterOffPeak = 0.015288

	// Residential Energy Only (TOU-REO-18)
	reoSummerOnPeak  = 0.297868
	reoSummerOffPeak = 0.076281
	reoWinterOffPeak = 0.076281
)

// ECCR percentage rates (as multipliers)
const (
	eccrMultiplierOld = 1.132343 // ECCR-14: 13.2343%
	eccrMultiplierNew = 1.130205 // ECCR-15p: 13.0205%
)

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
	var periods []types.UtilityFeesPeriod
	loc := etLocation

	for _, year := range years {
		holidays := getGPHolidays(year)

		type yearBlock struct {
			months         []time.Month
			eccrMultiplier float64
			isOld          bool
		}

		var blocks []yearBlock
		if year < 2026 {
			blocks = []yearBlock{
				{months: nil, eccrMultiplier: eccrMultiplierOld, isOld: true},
			}
		} else if year == 2026 {
			blocks = []yearBlock{
				{
					months:         []time.Month{time.January, time.February, time.March, time.April, time.May},
					eccrMultiplier: eccrMultiplierOld,
					isOld:          true,
				},
				{
					months:         []time.Month{time.June, time.July, time.August, time.September, time.October, time.November, time.December},
					eccrMultiplier: eccrMultiplierNew,
					isOld:          false,
				},
			}
		} else {
			blocks = []yearBlock{
				{months: nil, eccrMultiplier: eccrMultiplierNew, isOld: false},
			}
		}

		for _, block := range blocks {
			// 1. Build base simplified periods with ECCR applied
			switch plan {
			case "gp_tou_oa_14":
				onPeakRate := oaSummerOnPeak * block.eccrMultiplier
				offPeakRate := oaSummerOffPeak * block.eccrMultiplier
				superOffPeakRate := oaSummerSuperOff * block.eccrMultiplier

				winterOffPeakRate := oaWinterOffPeak * block.eccrMultiplier
				winterSuperOffPeakRate := oaWinterSuperOff * block.eccrMultiplier

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

			case "gp_tou_rd_11":
				onPeakRate := rdSummerOnPeak * block.eccrMultiplier
				offPeakRate := rdSummerOffPeak * block.eccrMultiplier
				winterOffPeakRate := rdWinterOffPeak * block.eccrMultiplier

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

			case "gp_tou_reo_18":
				onPeakRate := reoSummerOnPeak * block.eccrMultiplier
				offPeakRate := reoSummerOffPeak * block.eccrMultiplier
				winterOffPeakRate := reoWinterOffPeak * block.eccrMultiplier

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
			periods = append(periods, gpFCRPeriods(plan, year, block.months, block.isOld, holidays)...)
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

func gpFCRPeriods(plan string, year int, months []time.Month, isOld bool, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	loc := etLocation

	if plan == "gp_tou_oa_14" {
		var onPeakRate, offPeakRate, superOffPeakRate float64
		var suffix string
		if isOld {
			onPeakRate, offPeakRate, superOffPeakRate = 0.066871, 0.044284, 0.038252
			suffix = " (FCR-26)"
		} else {
			onPeakRate, offPeakRate, superOffPeakRate = 0.052269, 0.038690, 0.034747
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
		if isOld {
			onPeakRate, offPeakRate = 0.066871, 0.042398
			suffix = " (FCR-26)"
		} else {
			onPeakRate, offPeakRate = 0.052269, 0.037441
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
