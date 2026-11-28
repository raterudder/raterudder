package utility

// Tariff Audit Metadata
// - Provider(s): gp (Georgia Power (GP))
// - Official Sources:
//   - https://www.georgiapower.com/residential/rate-plans.html
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/r.pdf (Schedule R-31, Residential Service)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/tou-oa.pdf (Schedule TOU-OA-15, Overnight Advantage)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/tou-rd.pdf (Schedule TOU-RD-12, Residential Demand / Smart Usage)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/tou-reo.pdf (Schedule TOU-REO-19, Nights & Weekends)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/fcr.pdf (Schedule FCR-27, Fuel Cost Recovery)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/tou-fcr.pdf (Schedule TOU-FCR-7, Time of Use Fuel Cost Recovery)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/tou-fcr-tp.pdf (Schedule TOU-FCR-TP-5, Three-Part Time of Use Fuel Cost Recovery)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/eccr.pdf (Schedule ECCR-15, Environmental Compliance Cost Recovery)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/dsm-r.pdf (Schedule DSM-R-16, Demand Side Management Residential)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/mff.pdf (Schedule MFF-11, Municipal Franchise Fee)
//   - https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/rnr.pdf (Schedule RNR-11, Renewable and Nonrenewable Resources)
//   - https://psc.ga.gov/search/facts-document/?documentId=227048 & https://psc.ga.gov/search/facts-docket/?docketId=16573 (Georgia PSC Docket No. 16573 Document 227048, 2027 Solar Avoided Energy Cost $39.30/MWh filed June 26, 2026)
//   - https://psc.ga.gov/utilities/electric/georgia-power-bill-calculator/ & https://psc.ga.gov/utilities/electric/georgia-power-bill-calculator-archive/ (Georgia PSC Electric Bill Calculators)
//   - https://www.georgiapower.com/about/company/filings/rate-request.html (Georgia PSC Docket No. 44280 July 1, 2025 order freezing base rates through 2028)
// - Effective Window: 2026-01-01 through 2027-12-31 (Jan-May 2026 Schedules R-30, TOU-OA-14, TOU-RD-11, TOU-REO-18, ECCR-14, DSM-R-15, and FCR-26; eff. 2026-06-01 Schedules R-31, TOU-OA-15, TOU-RD-12, TOU-REO-19, ECCR-15, DSM-R-16, and FCR-27/TOU-FCR-7/TOU-FCR-TP-5; base rates frozen through 2028 per Docket No. 44280)
// - Revision Cadence: Annual / Periodic (Jun 1 FCR/ECCR/DSM-R tariff riders as filed; Jun/Jul Docket No. 16573 RNR avoided-cost filing for following Jan 1)
// - Next Audit Due: 2027-06-01 (Check June 2027 FCR/ECCR/DSM-R rider filings and Docket No. 16573 2028 RNR avoided-cost filing)
// - Last Verified: 2026-11-28
// - Modeling Notes: Base rates frozen through 2028 per Georgia PSC Docket No. 44280 order; Schedule R-30/R-31 summer volumetric energy assumes Tier 1 (<= 650 kWh/month); additive base riders apply (1 + ECCR + DSM-R) with 0.0000% NCCR per Georgia PSC calculator, while Schedule NU-1 applies only to commercial/industrial tariffs and is omitted; Schedule MFF-11 applies 1.1995% outside city limits (default) or 3.0843% inside city limits via options.Location to both base+rider and FCR charges; fixed daily/monthly basic service charges ($0.4603/day) and per-kW demand charges (e.g., $12.21/kW on TOU-RD) are excluded; Schedule RNR-11 models Instantaneous Netting (gp_instantaneous, default: $0.07219/kWh in 2026 and $0.07930/kWh in 2027) or legacy Monthly Netting (net, 1:1).

import (
	"fmt"
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// GP Renewable and Nonrenewable Resources (Schedule RNR-11) Solar Avoided Energy Cost
// filed in Georgia PSC Docket No. 16573:
//   - 2026: $32.19/MWh ($0.03219/kWh) + $0.04/kWh Renewable Adder = $0.07219/kWh
//   - 2027: $39.30/MWh ($0.03930/kWh, Document 227048 filed June 26, 2026) + $0.04/kWh Renewable Adder = $0.07930/kWh
//     (https://psc.ga.gov/search/facts-document/?documentId=227048, https://www.georgiapower.com/content/dam/georgia-power/pdfs/tariffs/rnr.pdf)
var gpInstantaneousRates = map[int]float64{
	2026: 0.07219,
	2027: 0.07930,
}

type gpRRates struct {
	SummerTier1DollarsPerKWH float64
	WinterDollarsPerKWH      float64
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

var gpRVersions = map[int]gpRRates{
	// R-30: Applicable to billing months prior to June 2026 (Tier 1 <= 650 kWh in summer)
	30: {
		SummerTier1DollarsPerKWH: 0.086121,
		WinterDollarsPerKWH:      0.080602,
	},
	// R-31: Applicable to billing months June 2026 onwards (Tier 1 <= 650 kWh in summer)
	31: {
		SummerTier1DollarsPerKWH: 0.087738,
		WinterDollarsPerKWH:      0.082116,
	},
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

// Additive base revenue rider percentages (1 + ECCR + DSM-R; NCCR is 0.0000%) per Georgia PSC calculator:
// - Through May 2026: 1 + 0.132343 (ECCR-14: 13.2343%) + 0.012165 (DSM-R-15: 1.2165%) = 1.144508
// - June 2026 onwards: 1 + 0.130205 (ECCR-15: 13.0205%) + 0.011969 (DSM-R-16: 1.1969%) = 1.142174
const (
	eccrFactorJan26 = 0.132343 // ECCR-14: 13.2343%
	dsmrFactorJan26 = 0.012165 // DSM-R-15: 1.2165%
	eccrFactorJun26 = 0.130205 // ECCR-15: 13.0205%
	dsmrFactorJun26 = 0.011969 // DSM-R-16: 1.1969%

	gpBaseRiderMultiplierJan26 = 1 + eccrFactorJan26 + dsmrFactorJan26 // 1.144508
	gpBaseRiderMultiplierJun26 = 1 + eccrFactorJun26 + dsmrFactorJun26 // 1.142174
)

// Schedule MFF-11 (Municipal Franchise Fee) multipliers applied to base+rider revenue and FCR fuel revenue:
const (
	gpMFFOutsideCityMultiplier = 1.011995 // Outside city limits: 1.1995% (default)
	gpMFFInsideCityMultiplier  = 1.030843 // Inside city limits: 3.0843%
)

func gpMFFMultiplier(location string) float64 {
	if location == "inside_city" {
		return gpMFFInsideCityMultiplier
	}
	return gpMFFOutsideCityMultiplier
}

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

	mffMultiplier := gpMFFMultiplier(options.Location)
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getGPHolidays(year)

		type yearBlock struct {
			months              []time.Month
			baseRiderMultiplier float64
			versionDiff         int
		}

		var blocks []yearBlock
		if year < 2026 {
			blocks = []yearBlock{
				{months: nil, baseRiderMultiplier: gpBaseRiderMultiplierJan26, versionDiff: 0},
			}
		} else if year == 2026 {
			blocks = []yearBlock{
				{
					months:              []time.Month{time.January, time.February, time.March, time.April, time.May},
					baseRiderMultiplier: gpBaseRiderMultiplierJan26,
					versionDiff:         0,
				},
				{
					months:              []time.Month{time.June, time.July, time.August, time.September, time.October, time.November, time.December},
					baseRiderMultiplier: gpBaseRiderMultiplierJun26,
					versionDiff:         1,
				},
			}
		} else {
			blocks = []yearBlock{
				{months: nil, baseRiderMultiplier: gpBaseRiderMultiplierJun26, versionDiff: 1},
			}
		}

		for _, block := range blocks {
			// 1. Build base simplified periods with additive ECCR + DSM-R and MFF-11 applied
			switch plan {
			case "gp_r":
				r := gpRVersions[30+block.versionDiff]
				summerDollarsPerKWH := r.SummerTier1DollarsPerKWH * block.baseRiderMultiplier * mffMultiplier
				winterDollarsPerKWH := r.WinterDollarsPerKWH * block.baseRiderMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				// Leave OtherName empty on non-TOU gp_r so GetPeriods returns empty.
				if len(summerIntersection) > 0 {
					summerPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             summerIntersection,
						OtherDollarsPerKWH: summerDollarsPerKWH,
						OtherDescription:   "Summer Tier 1 Energy",
					}
					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             winterIntersection,
						OtherDollarsPerKWH: winterDollarsPerKWH,
						OtherDescription:   "Winter Energy",
					}
					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
				}

			case "gp_tou_oa":
				r := gpOAVersions[14+block.versionDiff]
				onPeakDollarsPerKWH := r.SummerOnPeak * block.baseRiderMultiplier * mffMultiplier
				offPeakDollarsPerKWH := r.SummerOffPeak * block.baseRiderMultiplier * mffMultiplier
				superOffPeakDollarsPerKWH := r.SummerSuperOff * block.baseRiderMultiplier * mffMultiplier
				winterOffPeakDollarsPerKWH := r.WinterOffPeak * block.baseRiderMultiplier * mffMultiplier
				winterSuperOffPeakDollarsPerKWH := r.WinterSuperOff * block.baseRiderMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				if len(summerIntersection) > 0 {
					summerHolidayPeriod := touSimplifiedPeriod{
						Year:          year,
						Months:        summerIntersection,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "Off-Peak",
								// Off-Peak: 7 AM - 11 PM
								Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
								DollarsPerKWH: offPeakDollarsPerKWH,
								Description:   "Summer Holiday Off-Peak",
							},
						},
						OtherName:          "Super Off-Peak",
						OtherDollarsPerKWH: superOffPeakDollarsPerKWH,
						OtherDescription:   "Summer Holiday Super Off-Peak",
					}

					summerRegularPeriod := touSimplifiedPeriod{
						Year:             year,
						Months:           summerIntersection,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "On-Peak",
								// On-Peak: 2 PM - 7 PM, weekdays
								Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: onPeakDollarsPerKWH,
								Description:   "Summer On-Peak",
							},
							{
								Name: "Off-Peak",
								// Off-Peak: 7 AM - 2 PM, 7 PM - 11 PM weekdays
								Hours: []types.UtilityHourPeriod{
									{HourStart: 7, HourEnd: 14},
									{HourStart: 19, HourEnd: 23},
								},
								Weekday:       true,
								DollarsPerKWH: offPeakDollarsPerKWH,
								Description:   "Summer Weekday Off-Peak",
							},
							{
								Name: "Off-Peak",
								// Off-Peak: 7 AM - 11 PM weekends
								Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
								Weekend:       true,
								DollarsPerKWH: offPeakDollarsPerKWH,
								Description:   "Summer Weekend Off-Peak",
							},
						},
						OtherName:          "Super Off-Peak",
						OtherDollarsPerKWH: superOffPeakDollarsPerKWH,
						OtherDescription:   "Summer Super Off-Peak",
					}

					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:   year,
						Months: winterIntersection,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "Off-Peak",
								// Off-Peak: 7 AM - 11 PM
								Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
								DollarsPerKWH: winterOffPeakDollarsPerKWH,
								Description:   "Winter Off-Peak",
							},
						},
						OtherName:          "Super Off-Peak",
						OtherDollarsPerKWH: winterSuperOffPeakDollarsPerKWH,
						OtherDescription:   "Winter Super Off-Peak",
					}

					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
				}

			case "gp_tou_rd":
				r := gpRDVersions[11+block.versionDiff]
				onPeakDollarsPerKWH := r.SummerOnPeak * block.baseRiderMultiplier * mffMultiplier
				offPeakDollarsPerKWH := r.SummerOffPeak * block.baseRiderMultiplier * mffMultiplier
				winterOffPeakDollarsPerKWH := r.WinterOffPeak * block.baseRiderMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				if len(summerIntersection) > 0 {
					summerHolidayPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             summerIntersection,
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakDollarsPerKWH,
						OtherDescription:   "Summer Holiday Off-Peak",
					}

					summerRegularPeriod := touSimplifiedPeriod{
						Year:             year,
						Months:           summerIntersection,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "On-Peak",
								// On-Peak: 2 PM - 7 PM weekdays
								Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: onPeakDollarsPerKWH,
								Description:   "Summer On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakDollarsPerKWH,
						OtherDescription:   "Summer Off-Peak",
					}

					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             winterIntersection,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
						OtherDescription:   "Winter Off-Peak",
					}

					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
				}

			case "gp_tou_reo":
				r := gpREOVersions[18+block.versionDiff]
				onPeakDollarsPerKWH := r.SummerOnPeak * block.baseRiderMultiplier * mffMultiplier
				offPeakDollarsPerKWH := r.SummerOffPeak * block.baseRiderMultiplier * mffMultiplier
				winterOffPeakDollarsPerKWH := r.WinterOffPeak * block.baseRiderMultiplier * mffMultiplier

				summerIntersection := intersectMonths(summerMonths, block.months)
				winterIntersection := intersectMonths(winterMonths, block.months)

				if len(summerIntersection) > 0 {
					summerHolidayPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             summerIntersection,
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakDollarsPerKWH,
						OtherDescription:   "Summer Holiday Off-Peak",
					}

					summerRegularPeriod := touSimplifiedPeriod{
						Year:             year,
						Months:           summerIntersection,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "On-Peak",
								// On-Peak: 2 PM - 7 PM weekdays
								Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}},
								Weekday:       true,
								DollarsPerKWH: onPeakDollarsPerKWH,
								Description:   "Summer On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakDollarsPerKWH,
						OtherDescription:   "Summer Off-Peak",
					}

					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
				}

				if len(winterIntersection) > 0 {
					winterPeriod := touSimplifiedPeriod{
						Year:               year,
						Months:             winterIntersection,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
						OtherDescription:   "Winter Off-Peak",
					}

					periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
				}
			}

			// 2. Build and append FCR fee periods for this block
			periods = append(periods, gpFCRPeriods(plan, year, block.months, block.versionDiff, mffMultiplier, holidays)...)
		}
	}

	// Add dynamic NBT export rates for RNR-Instantaneous Netting scheme
	// Note: gp_monthly is handled implicitly by it having a value of "net"
	if options.NetMeteringScheme == "gp_instantaneous" || options.NetMeteringScheme == "" {
		for _, year := range years {
			exportCreditDollarsPerKWH, ok := gpInstantaneousRates[year]
			if !ok {
				exportCreditDollarsPerKWH = gpInstantaneousRates[2027] // fallback
			}
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
					LocationPtr: etLocation,
				},
				DollarsPerKWH:            exportCreditDollarsPerKWH,
				SeparateGenerationCredit: true,
				Description:              fmt.Sprintf("GP RNR Instantaneous Export Credit (%d)", year),
			})
		}
	}

	return periods
}

func gpFCRPeriods(plan string, year int, months []time.Month, versionDiff int, mffMultiplier float64, holidays []string) []types.UtilityFeesPeriod {
	// Handle backwards compatibility for old plan IDs
	if plan == "gp_tou_oa_14" {
		plan = "gp_tou_oa"
	} else if plan == "gp_tou_rd_11" {
		plan = "gp_tou_rd"
	} else if plan == "gp_tou_reo_18" {
		plan = "gp_tou_reo"
	}

	var periods []types.UtilityFeesPeriod

	switch plan {
	case "gp_r":
		// Standard residential Fuel Cost Recovery (Schedule FCR-26 before June 2026, Schedule FCR-27 from June 2026)
		var summerFCRDollarsPerKWH, winterFCRDollarsPerKWH float64
		var suffix string
		if versionDiff == 0 {
			summerFCRDollarsPerKWH = 0.045876 * mffMultiplier
			winterFCRDollarsPerKWH = 0.042859 * mffMultiplier
			suffix = " (FCR-26)"
		} else {
			summerFCRDollarsPerKWH = 0.038069 * mffMultiplier
			winterFCRDollarsPerKWH = 0.038561 * mffMultiplier
			suffix = " (FCR-27)"
		}

		summerIntersection := intersectMonths(summerMonths, months)
		winterIntersection := intersectMonths(winterMonths, months)

		if len(summerIntersection) > 0 {
			summerPeriod := touSimplifiedPeriod{
				Year:               year,
				Months:             summerIntersection,
				OtherDollarsPerKWH: summerFCRDollarsPerKWH,
				OtherDescription:   "Summer FCR" + suffix,
			}
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerPeriod})...)
		}

		if len(winterIntersection) > 0 {
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				Months:             winterIntersection,
				OtherDollarsPerKWH: winterFCRDollarsPerKWH,
				OtherDescription:   "Winter FCR" + suffix,
			}
			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
		}

	case "gp_tou_oa":
		// Three-part TOU FCR (Schedule TOU-FCR-TP-4 / TOU-FCR-TP-5)
		var onPeakDollarsPerKWH, offPeakDollarsPerKWH, superOffPeakDollarsPerKWH float64
		var suffix string
		if versionDiff == 0 {
			onPeakDollarsPerKWH = 0.066871 * mffMultiplier
			offPeakDollarsPerKWH = 0.044284 * mffMultiplier
			superOffPeakDollarsPerKWH = 0.038252 * mffMultiplier
			suffix = " (FCR-26)"
		} else {
			onPeakDollarsPerKWH = 0.052269 * mffMultiplier
			offPeakDollarsPerKWH = 0.038690 * mffMultiplier
			superOffPeakDollarsPerKWH = 0.034747 * mffMultiplier
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
						DollarsPerKWH: offPeakDollarsPerKWH,
						Description:   "Summer Holiday FCR Off-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: superOffPeakDollarsPerKWH,
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
						DollarsPerKWH: onPeakDollarsPerKWH,
						Description:   "Summer FCR On-Peak" + suffix,
					},
					{
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 14},
							{HourStart: 19, HourEnd: 23},
						},
						Weekday:       true,
						DollarsPerKWH: offPeakDollarsPerKWH,
						Description:   "Summer Weekday FCR Off-Peak" + suffix,
					},
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						Weekend:       true,
						DollarsPerKWH: offPeakDollarsPerKWH,
						Description:   "Summer Weekend FCR Off-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: superOffPeakDollarsPerKWH,
				OtherDescription:   "Summer FCR Super Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
		}

		if len(winterIntersection) > 0 {
			winterPeriod := touSimplifiedPeriod{
				Year:   year,
				Months: winterIntersection,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 23}},
						DollarsPerKWH: offPeakDollarsPerKWH,
						Description:   "Winter FCR Off-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: superOffPeakDollarsPerKWH,
				OtherDescription:   "Winter FCR Super Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
		}

	case "gp_tou_rd", "gp_tou_reo":
		// Two-part TOU FCR (Schedule TOU-FCR-6 / TOU-FCR-7) for RD and REO
		var onPeakDollarsPerKWH, offPeakDollarsPerKWH float64
		var suffix string
		if versionDiff == 0 {
			onPeakDollarsPerKWH = 0.066871 * mffMultiplier
			offPeakDollarsPerKWH = 0.042398 * mffMultiplier
			suffix = " (FCR-26)"
		} else {
			onPeakDollarsPerKWH = 0.052269 * mffMultiplier
			offPeakDollarsPerKWH = 0.037441 * mffMultiplier
			suffix = " (FCR-27)"
		}

		summerIntersection := intersectMonths(summerMonths, months)
		winterIntersection := intersectMonths(winterMonths, months)

		if len(summerIntersection) > 0 {
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				Months:             summerIntersection,
				SpecificDates:      holidays,
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
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
						DollarsPerKWH: onPeakDollarsPerKWH,
						Description:   "Summer FCR On-Peak" + suffix,
					},
				},
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
				OtherDescription:   "Summer FCR Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod})...)
		}

		if len(winterIntersection) > 0 {
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				Months:             winterIntersection,
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
				OtherDescription:   "Winter FCR Off-Peak" + suffix,
			}

			periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{winterPeriod})...)
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
	options := []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "Service Location (Municipal Franchise Fee)",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Whether your service address is inside or outside municipal city limits for Schedule MFF-11.",
			Choices: []types.UtilityOptionChoice{
				{Value: "outside_city", Name: "Outside City Limits (1.1995%)"},
				{Value: "inside_city", Name: "Inside City Limits (3.0843%)"},
			},
			Default: "outside_city",
		},
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
	}

	return types.UtilityProviderInfo{
		ID:   "gp",
		Name: "Georgia Power (GP)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "gp_r",
				Name:    "R (Residential Service)",
				Options: options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_r", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "gp_tou_oa",
				Name:    "TOU-OA (Overnight Advantage)",
				Options: options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_tou_oa", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "gp_tou_rd",
				Name:    "TOU-RD (Residential Demand / Smart Usage)",
				Options: options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_tou_rd", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "gp_tou_reo",
				Name:    "TOU-REO (Nights & Weekends)",
				Options: options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return gpPeriods("gp_tou_reo", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
