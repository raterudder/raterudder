package utility

// Tariff Audit Metadata
// - Provider(s): peco (PECO Energy Company)
// - Official Sources:
//   - https://www.peco.com/my-account/my-dashboard/rates-tariffs/electric-service/current-electric
//   - https://www.peco.com/cdn/assets/v3/assets/blt1b5616c79bacadb4/bltc70b88d49d2a1491/6abbe397d463362a37c0f102/Current_Electric_tariff_eff_Oct_1_2026_(003).pdf?branch=prod_alias
//   - https://www.peco.com/my-account/my-dashboard/rates-tariffs/electric-service/proposed-electric
//   - https://azure-na-assets.contentstack.com/v3/assets/blt1b5616c79bacadb4/bltf0a3b82010822345/6af61350a4c7f761ffba775c/TSC_Supp_no._40_eff_December_1_2026_clean.pdf
//   - Tariff Electric Pa. P.U.C. No. 8: Supplement No. 25 (4th Rev. Pages 37-39 GSA Class 1), Supplement No. 27 (3rd Rev. Page 46 TSC), Supplement No. 32 (14th Rev. Pages 52-53 Rate R/RH), Supplement No. 40 (4th Rev. Page 46 TSC eff. Dec 1, 2026), Rate RS-2 Net Metering (Pages 54-56)
// - Effective Window: 2025-12-01 through 2027-12-31 (Supplement No. 40 TSC $0.00995/kWh effective 2026-12-01)
// - Revision Cadence: Semi-Annual (June 1 and December 1 for GSA Procurement Class 1 and TSC; quarterly distribution/GSA reconciliations as filed)
// - Next Audit Due: 2026-12-01 (Check December 1, 2026 semi-annual GSA Procurement Class 1 supplement for Rate R/RH and Residential TOU)
// - Last Verified: 2026-11-19
// - Modeling Notes: Models PECO Default Service Price to Compare (GSA Procurement Class 1 + Transmission Service Charge: Jun-Nov 2026 TSC $0.00974/kWh, Dec 2026+ TSC $0.00995/kWh per Supplement No. 40); fixed monthly customer charge ($11.75/mo) excluded; 1:1 kWh net metering under Rate RS-2.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// shiftPECOWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftPECOWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getPECOHolidays returns observed PJM holidays for PECO.
// The holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getPECOHolidays(year int) []string {
	holidays := []time.Time{
		shiftPECOWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftPECOWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftPECOWeekendHoliday(christmasDay(year)),
		shiftPECOWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// pecoPeriods generates pricing periods for PECO.
func pecoPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getPECOHolidays(year)

		switch plan {
		case "peco_r":
			// Standard Flat Rate (Rate R/RH)
			simplified := []touSimplifiedPeriod{}

			if year == 2025 {
				// Dec 1, 2025 - Dec 31, 2025: $0.11024 / kWh
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2025,
					MonthStart:         time.December,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.11024,
					OtherDescription:   "PECO Rate R/RH Standard Rate",
				})
			} else if year == 2026 {
				// Jan 1, 2026 - May 31, 2026: $0.11024 / kWh
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2026,
					MonthStart:         time.January,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.11024,
					OtherDescription:   "PECO Rate R/RH Standard Rate (Winter/Spring)",
				})
				// Jun 1, 2026 - Nov 30, 2026: $0.11759 / kWh ($0.10785 GSA + $0.00974 TSC)
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2026,
					MonthStart:         time.June,
					MonthEnd:           time.November,
					OtherDollarsPerKWH: 0.11759,
					OtherDescription:   "PECO Rate R/RH Standard Rate (Summer/Fall)",
				})
				// Dec 1, 2026 - Dec 31, 2026: $0.11780 / kWh ($0.10785 GSA + $0.00995 TSC per Supplement No. 40)
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               2026,
					MonthStart:         time.December,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.11780,
					OtherDescription:   "PECO Rate R/RH Standard Rate (Winter)",
				})
			} else if year >= 2027 {
				// 2027+: $0.11780 / kWh ($0.10785 GSA + $0.00995 TSC per Supplement No. 40 until superseded)
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: 0.11780,
					OtherDescription:   "PECO Rate R/RH Standard Rate",
				})
			}

			periods = append(periods, buildPeriods(etLocation, simplified)...)

		case "peco_tou":
			// Time-of-Use Option (Rate R/RH TOU)
			type touRates struct {
				mStart, mEnd time.Month
				peak         float64
				offPeak      float64
				superOffPeak float64
				desc         string
			}

			var schedules []touRates
			if year == 2025 {
				schedules = []touRates{
					{time.December, time.December, 0.32747, 0.08382, 0.06061, "Winter 2025"},
				}
			} else if year == 2026 {
				schedules = []touRates{
					{time.January, time.May, 0.32747, 0.08382, 0.06061, "Winter/Spring 2026"},
					{time.June, time.November, 0.32404, 0.09336, 0.06741, "Summer/Fall 2026"},
					{time.December, time.December, 0.32425, 0.09357, 0.06762, "Winter 2026"},
				}
			} else if year >= 2027 {
				schedules = []touRates{
					{time.January, time.December, 0.32425, 0.09357, 0.06762, "2027"},
				}
			}

			for _, s := range schedules {
				// Peak Period: 2:00 p.m. to 6:00 p.m. (14:00 to 18:00) weekdays, excluding PJM holidays.
				peakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}}
				// Super Off-Peak: 12:00 a.m. to 6:00 a.m. (0:00 to 6:00) daily.
				superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}

				// 1. PJM Holidays (treat day hours as Off-Peak, night hours as Super Off-Peak)
				holidayPeriod := touSimplifiedPeriod{
					Year:          year,
					MonthStart:    s.mStart,
					MonthEnd:      s.mEnd,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         superOffPeakHours,
							DollarsPerKWH: s.superOffPeak,
							Description:   "PECO TOU Super Off-Peak (" + s.desc + " Holiday)",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: s.offPeak,
					OtherDescription:   "PECO TOU Off-Peak (" + s.desc + " Holiday)",
				}

				// 2. Regular Days (non-holidays)
				regularPeriod := touSimplifiedPeriod{
					Year:             year,
					MonthStart:       s.mStart,
					MonthEnd:         s.mEnd,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         superOffPeakHours,
							DollarsPerKWH: s.superOffPeak,
							Description:   "PECO TOU Super Off-Peak (" + s.desc + ")",
						},
						{
							Name:          "On-Peak",
							Hours:         peakHours,
							Weekday:       true,
							DollarsPerKWH: s.peak,
							Description:   "PECO TOU Peak (" + s.desc + ")",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: s.offPeak,
					OtherDescription:   "PECO TOU Off-Peak (" + s.desc + ")",
				}

				periods = append(periods, buildPeriods(etLocation, []touSimplifiedPeriod{
					holidayPeriod,
					regularPeriod,
				})...)
			}
		}
	}
	return periods
}

// pecoUtilityInfo returns the metadata and rates for PECO.
func pecoUtilityInfo() types.UtilityProviderInfo {
	pecoOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "PECO net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "peco",
		Name: "PECO Energy Company",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "peco_r",
				Name:    "Residential Service (Rate R/RH)",
				Options: pecoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pecoPeriods("peco_r", opts, []int{2025, 2026, 2027}), nil
				},
			},
			{
				ID:      "peco_tou",
				Name:    "Residential Time-of-Use (TOU)",
				Options: pecoOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return pecoPeriods("peco_tou", opts, []int{2025, 2026, 2027}), nil
				},
			},
		},
	}
}
