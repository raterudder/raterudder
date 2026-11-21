package utility

// Tariff Audit Metadata
// - Provider(s): xcel (Xcel Energy)
// - Official Sources:
//   - Rate Books Portal: https://www.xcelenergy.com/company/rates_and_regulations/rates/rate_books
//   - TX (SPS): 2026 Secondary Fuel Cost Factors https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000IdXwE/.1W6sow_Mk0ZuGumdOqwoyxA6WgdzkPcur4oOO.O8pU; Sheet IV-69 Rev. 61 Fuel Cost Recovery Factor https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000X0qBk/drFfHREWUzzeZbfu4ZDH16ohltajKcuCG3ZWyXv1fNQ; Sheet IV-195 Rev. 15 EECRF https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000ITaH4/gbo33gIfz7N.2QfZmNOFjImEmujrs9N2hQS9EfSeDOU; Sheet IV-231 Rev. 3 DCRF https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/8b000003BcVk/HZUvHpmAxv1or9dCBChoQZ6gxtib5S.o7dMHmOndiYM; Sheet IV-236 Original GCRR https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000SXhSh/ji4CGmU1J8ioZ6cjTxSJgVSV1nRqx3.qUHy1fdYpjtc; Sheet IV-234 Rev. 1 TCRF https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000TVtty/Fgh4DZtcgCwt_DWR1MqPmfc1ZxhqJOMHtmrPpIrDklY
//   - CO (PSCo): Colo. PUC No. 8 Electric Schedule RE-TOU Sheets 33B-33C https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/8b000002Y8xL/kYe61yf.9xyigvh2701Az49XLgU2izDS8ShGaCXiwsQ; Summary of Electric Rates eff. 2026-07-01 https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000RlUER/5G4ru_.jqcaA1xENmOJaXHZ1xgEeRfm5coulNeJ2Wz4, eff. 2026-10-01 https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000X5c02/KPScvoWaY94FdMUkdkOQP5absPAn08ZRRTHeCCKlqPA, and eff. 2026-10-19 https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R300000YLSLv/pSAmvUy.Aj2efQEs2fh8MVzV4TzrQ6j1JkmLbs3w0js
//   - SD (NSP): SDPUC No. 2 Section 5 Sheets 5-1.1 & 5-2 (Docket EL25-024, eff. 2026-07-01) https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/8b000002yLby/bvZvXKZo01_QDFUYHpzwG181OQuyK5oE3qLcA2xjUyQ
//   - MI (NSP-W): M.P.S.C. No. 2 Section D Sheets D-2.0 (PSCR), D-3.1 (EWR, Case U-22013), D-4.0 (MR-1), D-5.0 (MR-2), and D-52.3 (DG-1) https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/8b000002gW33/I5GWTbv9zPNXnLlUjb3GEyNpAYUJ6j8oPCRQjf4EAfg
//   - MN (NSP): MPUC No. 2 Section 5 Sheets 5-91.1 (2026 Monthly Fuel Cost Factors) & 5-94 (7.14% Interim Rate Surcharge) https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R3000009e89p/6CCLEW5S0IzVq.omDH.mYhUSD0E_R2DThBnsFiWShaY; Section 9 33rd Rev. Sheet 9-3 (Rate Codes A51 & A52, Docket E002/PR-26-9, eff. 2026-04-01) https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/R3000009e6zE/WgTa73fgm8j3hO9iNY0sPPBKh0hZNRq5cFu7RWrXSoM
//   - ND (NSP): NDPSC No. 2 Section 5 12th Rev. Sheet 5-2 (Case PU-24-376) https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/8b000002zMEp/6OSkctgmQy7WLZuJ._IuvzEmeneZwoG1YOfY7e9nGQc
//   - WI (NSP-W): PSCW Vol. 8 Schedule Pg-2B Rev. 4 Sheet E 56.6 & Rev. 2 Sheet E 56.61 (Amendment 787, Docket 4220-TE-2026, eff. 2026-01-01) https://xcelnew.my.salesforce.com/sfc/p/1U0000011ttV/a/8b000003BF6D/KWwCDj3uMqk2klcrd9kHb0WRgMm1s6O55xoIIp_FVPM
// - Effective Window: 2026-01-01 through 2027-12-31 (monthly TX/MN fuel and MI PSCR factors carried forward until superseded)
// - Revision Cadence: Monthly (TX Fuel, MN Fuel, MI PSCR) & Quarterly/Periodic (CO ECA/GRSA-E, State Riders & Solar Export Credits)
// - Next Audit Due: 2026-12-05 (December 2026 TX Secondary Fuel Factor, MI PSCR factor, and Q1 2027 CO/MN/WI rate book updates)
// - Last Verified: 2026-11-20
// - Modeling Notes: Preserves pre-effective 2026 tariff windows for historical price determinism; models Tier 1 (<1,000 kWh in SD) volumetric energy charges and excludes fixed monthly customer/meter fees; credits Supply + PSCR under Michigan DG-1 (`mi_dg`) and secondary loss-adjusted avoided energy + transmission credits under Wisconsin Schedule Pg-2B (`wi_pg2b`).

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const xcelMNInterimSurchargeMultiplier = 1.0714

// Xcel Energy Texas Net Billing and Secondary Fuel Cost Factors by year and month.
// Link to Texas rate books: https://www.xcelenergy.com/company/rates_and_regulations/rates/rate_books
var xcelTXNetBillingRates = map[int]map[time.Month]float64{
	2026: {
		time.January:   0.016221,
		time.February:  0.016221,
		time.March:     0.016221,
		time.April:     0.009824,
		time.May:       0.009824,
		time.June:      0.009824,
		time.July:      0.009824,
		time.August:    0.009824,
		time.September: 0.018083,
		time.October:   0.018083,
		time.November:  0.018083,
	},
}

// Xcel Energy Minnesota Residential Fuel Cost Factors ($/kWh) per MPUC No. 2 Sheet 5-91.1.
var xcelMNFuelDollarsPerKWH = map[int]map[time.Month]float64{
	2026: {
		time.January:   0.01568,
		time.February:  0.01575,
		time.March:     0.01681,
		time.April:     0.02499,
		time.May:       0.02695,
		time.June:      0.02973,
		time.July:      0.02927,
		time.August:    0.02780,
		time.September: 0.02448,
		time.October:   -0.00179,
		time.November:  0.01926,
		time.December:  0.02162,
	},
}

func lookupXcelMonthlyDollarsPerKWH(table map[int]map[time.Month]float64, year int, m time.Month) float64 {
	if rates, ok := table[year]; ok && rates != nil {
		if dollarsPerKWH, ok := rates[m]; ok {
			return dollarsPerKWH
		}
		for prevMonth := m - 1; prevMonth >= time.January; prevMonth-- {
			if dollarsPerKWH, ok := rates[prevMonth]; ok {
				return dollarsPerKWH
			}
		}
	}

	// Find the largest defined year <= target year and carry its latest month forward.
	bestYear := 0
	for y := range table {
		if y <= year && y > bestYear {
			bestYear = y
		}
	}
	if bestYear > 0 {
		rates := table[bestYear]
		for prevMonth := time.December; prevMonth >= time.January; prevMonth-- {
			if dollarsPerKWH, ok := rates[prevMonth]; ok {
				return dollarsPerKWH
			}
		}
	}

	// Fallback if target year precedes all defined years.
	earliestYear := 0
	for y := range table {
		if earliestYear == 0 || y < earliestYear {
			earliestYear = y
		}
	}
	if rates := table[earliestYear]; rates != nil {
		if dollarsPerKWH, ok := rates[m]; ok {
			return dollarsPerKWH
		}
		for nextMonth := time.January; nextMonth <= time.December; nextMonth++ {
			if dollarsPerKWH, ok := rates[nextMonth]; ok {
				return dollarsPerKWH
			}
		}
	}
	return 0.0
}

func getXcelTXNetBillingRate(year int, m time.Month) float64 {
	return lookupXcelMonthlyDollarsPerKWH(xcelTXNetBillingRates, year, m)
}

func getXcelMNFuelDollarsPerKWH(year int, m time.Month) float64 {
	return lookupXcelMonthlyDollarsPerKWH(xcelMNFuelDollarsPerKWH, year, m)
}

func xcelMNTotalDollarsPerKWH(baseDollarsPerKWH, fuelDollarsPerKWH float64) float64 {
	return baseDollarsPerKWH*xcelMNInterimSurchargeMultiplier + fuelDollarsPerKWH
}

func clipXcelPeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
	var clipped []types.UtilityFeesPeriod
	for _, p := range periods {
		if !windowStart.IsZero() && (p.Start.IsZero() || p.Start.Before(windowStart)) {
			p.Start = windowStart
		}
		if !windowEnd.IsZero() && (p.End.IsZero() || p.End.After(windowEnd)) {
			p.End = windowEnd
		}
		if p.End.After(p.Start) {
			clipped = append(clipped, p)
		}
	}
	return clipped
}

func defaultXcelNetMeteringScheme(plan, scheme string) string {
	if scheme != "" {
		return scheme
	}
	switch plan {
	case "xcel_mn_standard", "xcel_mn_standard_heating", "xcel_mn_tou", "xcel_mn_tou_heating":
		return "mn_occasional"
	case "xcel_nd_standard", "xcel_nd_standard_heating", "xcel_nd_tou", "xcel_nd_tou_heating":
		return "nd_net_energy_billing"
	case "xcel_sd_standard", "xcel_sd_tou", "xcel_sd_tou_heating":
		return "sd_occasional"
	case "xcel_mi_standard", "xcel_mi_tou":
		return "mi_dg"
	case "xcel_wi_standard", "xcel_wi_tou":
		return "wi_pg2b"
	case "xcel_tx_standard", "xcel_tx_tou":
		return "tx_net_billing"
	default:
		return ""
	}
}

func shiftXcelWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getXcelNSPHolidays returns Xcel's holiday calendar for Northern States Power (NSP)
func getXcelNSPHolidays(year int) []string {
	holidays := []time.Time{
		shiftXcelWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftXcelWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftXcelWeekendHoliday(christmasDay(year)),
		shiftXcelWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func getXcelWIHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)

	holidays := []time.Time{
		shiftXcelWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftXcelWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		shiftXcelWeekendHoliday(christmasEve(year)),
		shiftXcelWeekendHoliday(christmasDay(year)),
		shiftXcelWeekendHoliday(newYearsEve(year)),
		shiftXcelWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func getXcelCOHolidays(year int) []string {
	holidays := []time.Time{
		shiftXcelWeekendHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftXcelWeekendHoliday(juneteenth(year)),
		shiftXcelWeekendHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftXcelWeekendHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftXcelWeekendHoliday(christmasDay(year)),
		shiftXcelWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func getXcelMIPeakHours(option string) types.UtilityHourPeriod {
	// Defaults to Option 1: 9 AM - 9 PM
	// Option 2 (8:30 AM – 8:30 PM) is rounded to 8 AM – 8 PM due to hourly simulation limits.
	// Option 4 (7:30 AM – 7:30 PM) is rounded to 7 AM – 7 PM.
	switch option {
	case "2": // Option 2 (8:30 AM - 8:30 PM) -> rounded to 8 AM - 8 PM
		return types.UtilityHourPeriod{HourStart: 8, HourEnd: 20}
	case "3": // Option 3 (8:00 AM - 8:00 PM) -> 8 AM - 8 PM
		return types.UtilityHourPeriod{HourStart: 8, HourEnd: 20}
	case "4": // Option 4 (7:30 AM - 7:30 PM) -> rounded to 7 AM - 7 PM
		return types.UtilityHourPeriod{HourStart: 7, HourEnd: 19}
	case "5": // Option 5 (7:00 AM - 7:00 PM) -> 7 AM - 7 PM
		return types.UtilityHourPeriod{HourStart: 7, HourEnd: 19}
	default: // Option 1: 9:00 AM - 9:00 PM
		return types.UtilityHourPeriod{HourStart: 9, HourEnd: 21}
	}
}

func buildXcelCOTOUPeriods(
	year int,
	coHolidays []string,
	summerOnPeakDollarsPerKWH float64,
	summerOffPeakDollarsPerKWH float64,
	winterOnPeakDollarsPerKWH float64,
	winterOffPeakDollarsPerKWH float64,
) []types.UtilityFeesPeriod {
	// Schedule RE-TOU Sheet 33C: On-Peak is 17:00-21:00 MT weekdays (excluding CO holidays) year-round.
	return buildPeriods(mtLocation, []touSimplifiedPeriod{
		{
			Year:               year,
			MonthStart:         time.June,
			MonthEnd:           time.September,
			SpecificDates:      coHolidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: summerOffPeakDollarsPerKWH,
			OtherDescription:   "Summer Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    coHolidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: summerOnPeakDollarsPerKWH,
					Description:   "Summer On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: summerOffPeakDollarsPerKWH,
			OtherDescription:   "Summer Off-Peak",
		},
		{
			Year:               year,
			MonthStart:         time.October,
			MonthEnd:           time.May,
			SpecificDates:      coHolidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
			OtherDescription:   "Winter Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.October,
			MonthEnd:         time.May,
			SpecificDates:    coHolidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: winterOnPeakDollarsPerKWH,
					Description:   "Winter On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
			OtherDescription:   "Winter Off-Peak",
		},
	})
}

func buildXcelSDTOUPeriods(
	year int,
	nspHolidays []string,
	summerOnPeakDollarsPerKWH float64,
	winterOnPeakDollarsPerKWH float64,
	offPeakDollarsPerKWH float64,
	winterOnPeakDescription string,
) []types.UtilityFeesPeriod {
	return buildPeriods(ctLocation, []touSimplifiedPeriod{
		{
			Year:               year,
			MonthStart:         time.June,
			MonthEnd:           time.September,
			SpecificDates:      nspHolidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Summer Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    nspHolidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: summerOnPeakDollarsPerKWH,
					Description:   "Summer On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Summer Off-Peak",
		},
		{
			Year:               year,
			MonthStart:         time.October,
			MonthEnd:           time.May,
			SpecificDates:      nspHolidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Winter Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.October,
			MonthEnd:         time.May,
			SpecificDates:    nspHolidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: winterOnPeakDollarsPerKWH,
					Description:   winterOnPeakDescription,
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Winter Off-Peak",
		},
	})
}

func xcelPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		nspHolidays := getXcelNSPHolidays(year)
		coHolidays := getXcelCOHolidays(year)

		switch plan {
		// --- COLORADO (America/Denver) ---
		case "xcel_co_tou":
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, mtLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, mtLocation)
			jul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, mtLocation)
			oct1 := time.Date(2026, time.October, 1, 0, 0, 0, 0, mtLocation)
			oct19 := time.Date(2026, time.October, 19, 0, 0, 0, 0, mtLocation)

			if year < 2026 {
				periods = append(periods, buildXcelCOTOUPeriods(year, coHolidays, 0.16430, 0.08218, 0.10481, 0.08218)...)
			} else if year == 2026 {
				// Pre-July 2026 baseline
				periods = append(periods, clipXcelPeriods(
					buildXcelCOTOUPeriods(year, coHolidays, 0.16430, 0.08218, 0.10481, 0.08218),
					yearStart, jul1,
				)...)
				// Effective 2026-07-01 Summary of Electric Rates
				periods = append(periods, clipXcelPeriods(
					buildXcelCOTOUPeriods(year, coHolidays, 0.36254, 0.13408, 0.31174, 0.11551),
					jul1, oct1,
				)...)
				// Effective 2026-10-01 Summary of Electric Rates
				periods = append(periods, clipXcelPeriods(
					buildXcelCOTOUPeriods(year, coHolidays, 0.36313, 0.14373, 0.31431, 0.12590),
					oct1, oct19,
				)...)
				// Effective 2026-10-19 Summary of Electric Rates (29.42% ECA + $0.01576/kWh GRSA-E)
				periods = append(periods, clipXcelPeriods(
					buildXcelCOTOUPeriods(year, coHolidays, 0.36441, 0.14501, 0.31559, 0.12718),
					oct19, yearEnd,
				)...)
			} else {
				periods = append(periods, buildXcelCOTOUPeriods(year, coHolidays, 0.36441, 0.14501, 0.31559, 0.12718)...)
			}

		// --- MINNESOTA (America/Chicago) ---
		// MPUC No. 2 Sheet 5-94 applies a 7.14% Interim Rate Surcharge to base energy, plus Sheet 5-91.1 monthly Fuel Cost Factors.
		case "xcel_mn_standard", "xcel_mn_standard_heating":
			winterBaseDollarsPerKWH := 0.11364
			winterDescription := "Winter Standard Rate"
			if plan == "xcel_mn_standard_heating" {
				winterBaseDollarsPerKWH = 0.06537
				winterDescription = "Winter Heating Standard Rate"
			}
			for month := time.January; month <= time.December; month++ {
				fuelDollarsPerKWH := getXcelMNFuelDollarsPerKWH(year, month)
				baseDollarsPerKWH := winterBaseDollarsPerKWH
				desc := winterDescription
				if month >= time.June && month <= time.September {
					baseDollarsPerKWH = 0.13069
					desc = "Summer Standard Rate"
				}
				periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						Months:             []time.Month{month},
						OtherDollarsPerKWH: xcelMNTotalDollarsPerKWH(baseDollarsPerKWH, fuelDollarsPerKWH),
						OtherDescription:   desc,
					},
				})...)
			}

		case "xcel_mn_tou", "xcel_mn_tou_heating":
			winterOnPeakBaseDollarsPerKWH := 0.21408
			winterOnPeakDescription := "Winter On-Peak"
			if plan == "xcel_mn_tou_heating" {
				winterOnPeakBaseDollarsPerKWH = 0.13577
				winterOnPeakDescription = "Winter Heating On-Peak"
			}
			for month := time.January; month <= time.December; month++ {
				fuelDollarsPerKWH := getXcelMNFuelDollarsPerKWH(year, month)
				offPeakTotalDollarsPerKWH := xcelMNTotalDollarsPerKWH(0.05171, fuelDollarsPerKWH)
				onPeakBaseDollarsPerKWH := winterOnPeakBaseDollarsPerKWH
				onPeakDesc := winterOnPeakDescription
				offPeakDesc := "Winter Off-Peak"
				holidayOffPeakDesc := "Winter Holiday Off-Peak"
				if month >= time.June && month <= time.September {
					onPeakBaseDollarsPerKWH = 0.25879
					onPeakDesc = "Summer On-Peak"
					offPeakDesc = "Summer Off-Peak"
					holidayOffPeakDesc = "Summer Holiday Off-Peak"
				}
				onPeakTotalDollarsPerKWH := xcelMNTotalDollarsPerKWH(onPeakBaseDollarsPerKWH, fuelDollarsPerKWH)

				periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						Months:             []time.Month{month},
						SpecificDates:      nspHolidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakTotalDollarsPerKWH,
						OtherDescription:   holidayOffPeakDesc,
					},
					{
						Year:             year,
						Months:           []time.Month{month},
						SpecificDates:    nspHolidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
								Weekday:       true,
								DollarsPerKWH: onPeakTotalDollarsPerKWH,
								Description:   onPeakDesc,
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakTotalDollarsPerKWH,
						OtherDescription:   offPeakDesc,
					},
				})...)
			}

		// --- NORTH DAKOTA (America/Chicago) ---
		case "xcel_nd_standard":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.09536,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.07937,
					OtherDescription:   "Winter Standard Rate",
				},
			})...)

		case "xcel_nd_standard_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.09536,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.07236,
					OtherDescription:   "Winter Heating Standard Rate",
				},
			})...)

		case "xcel_nd_tou":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.18180,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.13908,
							Description:   "Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		case "xcel_nd_tou_heating":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.18180,
							Description:   "Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.12418,
							Description:   "Winter Heating On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.04365,
					OtherDescription:   "Winter Off-Peak",
				},
			})...)

		// --- SOUTH DAKOTA (America/Chicago) ---
		// SDPUC No. 2 Docket EL25-024 updates residential Standard and TOD schedules effective 2026-07-01.
		case "xcel_sd_standard":
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)
			sdJul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation)

			preJulPeriods := buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.11153,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.09585,
					OtherDescription:   "Winter Standard Rate (<1000 kWh)",
				},
			})
			postJulPeriods := buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.13655,
					OtherDescription:   "Summer Standard Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.12096,
					OtherDescription:   "Winter Standard Rate (<1000 kWh)",
				},
			})

			if year < 2026 {
				periods = append(periods, preJulPeriods...)
			} else if year == 2026 {
				periods = append(periods, clipXcelPeriods(preJulPeriods, yearStart, sdJul1)...)
				periods = append(periods, clipXcelPeriods(postJulPeriods, sdJul1, yearEnd)...)
			} else {
				periods = append(periods, postJulPeriods...)
			}

		case "xcel_sd_tou":
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)
			sdJul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation)

			preJulPeriods := buildXcelSDTOUPeriods(year, nspHolidays, 0.21806, 0.17590, 0.04610, "Winter On-Peak")
			postJulPeriods := buildXcelSDTOUPeriods(year, nspHolidays, 0.24724, 0.20738, 0.06210, "Winter On-Peak")

			if year < 2026 {
				periods = append(periods, preJulPeriods...)
			} else if year == 2026 {
				periods = append(periods, clipXcelPeriods(preJulPeriods, yearStart, sdJul1)...)
				periods = append(periods, clipXcelPeriods(postJulPeriods, sdJul1, yearEnd)...)
			} else {
				periods = append(periods, postJulPeriods...)
			}

		case "xcel_sd_tou_heating":
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)
			sdJul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, ctLocation)

			preJulPeriods := buildXcelSDTOUPeriods(year, nspHolidays, 0.21806, 0.15839, 0.04610, "Winter Heating On-Peak")
			postJulPeriods := buildXcelSDTOUPeriods(year, nspHolidays, 0.24724, 0.19427, 0.06210, "Winter Heating On-Peak")

			if year < 2026 {
				periods = append(periods, preJulPeriods...)
			} else if year == 2026 {
				periods = append(periods, clipXcelPeriods(preJulPeriods, yearStart, sdJul1)...)
				periods = append(periods, clipXcelPeriods(postJulPeriods, sdJul1, yearEnd)...)
			} else {
				periods = append(periods, postJulPeriods...)
			}

		// --- MICHIGAN (America/Chicago) ---
		// M.P.S.C. No. 2 Section D:
		// Delivery = $0.0581/kWh; EWR = $0.0000/kWh before 2026-10-01 and $0.0080/kWh from 2026-10-01 (Sheet D-3.1);
		// PSCR = -$0.01009/kWh before 2026-11-01 and -$0.02500/kWh from 2026-11-01 (Sheet D-2.0, carried forward until superseded).
		case "xcel_mi_standard":
			miLocPtr := ctLocation
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, miLocPtr)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, miLocPtr)
			miOct1 := time.Date(2026, time.October, 1, 0, 0, 0, 0, miLocPtr)
			miNov1 := time.Date(2026, time.November, 1, 0, 0, 0, 0, miLocPtr)

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: miLocPtr,
				},
				DollarsPerKWH: 0.09425,
				Description:   "Michigan Standard Supply Rate",
			})

			const miDeliveryDollarsPerKWH = 0.0581
			if year < 2026 {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: miLocPtr},
					DollarsPerKWH:  miDeliveryDollarsPerKWH - 0.01009,
					GridAdditional: true,
					Description:    "Michigan Standard Delivery & PSCR Charge",
				})
			} else if year == 2026 {
				periods = append(periods,
					types.UtilityFeesPeriod{
						TimePeriod:     types.TimePeriod{Start: yearStart, End: miOct1, LocationPtr: miLocPtr},
						DollarsPerKWH:  miDeliveryDollarsPerKWH - 0.01009,
						GridAdditional: true,
						Description:    "Michigan Standard Delivery & PSCR Charge",
					},
					types.UtilityFeesPeriod{
						TimePeriod:     types.TimePeriod{Start: miOct1, End: miNov1, LocationPtr: miLocPtr},
						DollarsPerKWH:  miDeliveryDollarsPerKWH + 0.008 - 0.01009,
						GridAdditional: true,
						Description:    "Michigan Standard Delivery, EWR & PSCR Charge",
					},
					types.UtilityFeesPeriod{
						TimePeriod:     types.TimePeriod{Start: miNov1, End: yearEnd, LocationPtr: miLocPtr},
						DollarsPerKWH:  miDeliveryDollarsPerKWH + 0.008 - 0.02500,
						GridAdditional: true,
						Description:    "Michigan Standard Delivery, EWR & PSCR Charge",
					},
				)
			} else {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: miLocPtr},
					DollarsPerKWH:  miDeliveryDollarsPerKWH + 0.008 - 0.02500,
					GridAdditional: true,
					Description:    "Michigan Standard Delivery, EWR & PSCR Charge",
				})
			}

		case "xcel_mi_tou":
			miLocPtr := ctLocation
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, miLocPtr)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, miLocPtr)
			miOct1 := time.Date(2026, time.October, 1, 0, 0, 0, 0, miLocPtr)
			miNov1 := time.Date(2026, time.November, 1, 0, 0, 0, 0, miLocPtr)

			const miDeliveryDollarsPerKWH = 0.0581
			if year < 2026 {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: miLocPtr},
					DollarsPerKWH:  miDeliveryDollarsPerKWH - 0.01009,
					GridAdditional: true,
					Description:    "Michigan TOU Delivery & PSCR Charge",
				})
			} else if year == 2026 {
				periods = append(periods,
					types.UtilityFeesPeriod{
						TimePeriod:     types.TimePeriod{Start: yearStart, End: miOct1, LocationPtr: miLocPtr},
						DollarsPerKWH:  miDeliveryDollarsPerKWH - 0.01009,
						GridAdditional: true,
						Description:    "Michigan TOU Delivery & PSCR Charge",
					},
					types.UtilityFeesPeriod{
						TimePeriod:     types.TimePeriod{Start: miOct1, End: miNov1, LocationPtr: miLocPtr},
						DollarsPerKWH:  miDeliveryDollarsPerKWH + 0.008 - 0.01009,
						GridAdditional: true,
						Description:    "Michigan TOU Delivery, EWR & PSCR Charge",
					},
					types.UtilityFeesPeriod{
						TimePeriod:     types.TimePeriod{Start: miNov1, End: yearEnd, LocationPtr: miLocPtr},
						DollarsPerKWH:  miDeliveryDollarsPerKWH + 0.008 - 0.02500,
						GridAdditional: true,
						Description:    "Michigan TOU Delivery, EWR & PSCR Charge",
					},
				)
			} else {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: miLocPtr},
					DollarsPerKWH:  miDeliveryDollarsPerKWH + 0.008 - 0.02500,
					GridAdditional: true,
					Description:    "Michigan TOU Delivery, EWR & PSCR Charge",
				})
			}

			// Generate on-peak and off-peak supply periods based on option
			peakHours := getXcelMIPeakHours(options.PeakPeriodOption)

			// Weekday Non-Holiday On-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.December,
				SpecificDates:    nspHolidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{peakHours},
					Weekday:       true,
					DollarsPerKWH: 0.1607,
					Description:   "Michigan Supply On-Peak",
				}},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0402,
				OtherDescription:   "Michigan Supply Off-Peak",
			}})...)

			// Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.December,
				SpecificDates:      nspHolidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.0402,
				OtherDescription:   "Michigan Supply Holiday Off-Peak",
			}})...)

		// --- WISCONSIN (America/Chicago) ---
		case "xcel_wi_standard":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  0.065500,
				GridAdditional: true,
				Description:    "Wisconsin Delivery Charge",
			})

			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.097900,
					OtherDescription:   "Wisconsin Supply Summer Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.085400,
					OtherDescription:   "Wisconsin Supply Winter Rate",
				},
			})...)

		case "xcel_wi_tou":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  0.065500,
				GridAdditional: true,
				Description:    "Wisconsin Delivery Charge",
			})

			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.185500,
							Description:   "Wisconsin Supply Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Summer Off-Peak",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      nspHolidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Holiday Off-Peak",
				},
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    nspHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.158300,
							Description:   "Wisconsin Supply Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.037500,
					OtherDescription:   "Wisconsin Supply Winter Off-Peak",
				},
			})...)

		// --- TEXAS (America/Chicago) ---
		case "xcel_tx_standard":
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: 0.114967,
					OtherDescription:   "Texas Supply Summer Rate",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: 0.098842,
					OtherDescription:   "Texas Supply Winter Rate",
				},
			})...)

		case "xcel_tx_tou":
			// Summer (June 1 - Sept 30)
			summerHolidayPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.June,
				MonthEnd:           time.September,
				SpecificDates:      nspHolidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.082251,
				OtherDescription:   "Texas Supply Summer Holiday Off-Peak",
			}
			summerRegularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       time.June,
				MonthEnd:         time.September,
				SpecificDates:    nspHolidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						// On-Peak: 1 PM - 7 PM weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
						Weekday:       true,
						DollarsPerKWH: 0.25826,
						Description:   "Texas Supply Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.082251,
				OtherDescription:   "Texas Supply Summer Off-Peak",
			}
			// Winter (Oct 1 - May 31)
			winterPeriod := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.May,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: 0.082251,
				OtherDescription:   "Texas Supply Winter Off-Peak",
			}
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{summerHolidayPeriod, summerRegularPeriod, winterPeriod})...)
		}

		if plan == "xcel_tx_standard" || plan == "xcel_tx_tou" {
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)

			// Monthly Secondary Fuel Cost Recovery Factor per SPS Sheet IV-69 Rev. 61 & 2026 Fuel Factors table.
			for month := time.January; month <= time.December; month++ {
				fuelDollarsPerKWH := getXcelTXNetBillingRate(year, month)
				startMonth := time.Date(year, month, 1, 0, 0, 0, 0, ctLocation)
				endMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, ctLocation)
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       startMonth,
						End:         endMonth,
						LocationPtr: ctLocation,
					},
					DollarsPerKWH:  fuelDollarsPerKWH,
					GridAdditional: true,
					Description:    fmt.Sprintf("Texas Fuel Cost Recovery Factor (%s)", month.String()[:3]),
				})
			}

			// PUCT Volumetric Riders:
			// EECRF: $0.001382/kWh (Sheet IV-195 Rev. 15, eff. 2026-01-01)
			// DCRF:  $0.005740/kWh (Sheet IV-231 Rev. 3, eff. 2025-12-12)
			// GCRR:  $0.003117/kWh (Sheet IV-236 Original, eff. 2026-04-01)
			// TCRF:  $0.004428/kWh (Sheet IV-234 Rev. 1, eff. 2026-07-10)
			txJan1 := time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation)
			txApr1 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation)
			txJul10 := time.Date(2026, time.July, 10, 0, 0, 0, 0, ctLocation)

			periods = append(periods, clipXcelPeriods([]types.UtilityFeesPeriod{
				{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: ctLocation},
					DollarsPerKWH:  0.001382,
					GridAdditional: true,
					Description:    "Texas EECRF Rider",
				},
			}, txJan1, time.Time{})...)

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: ctLocation},
				DollarsPerKWH:  0.005740,
				GridAdditional: true,
				Description:    "Texas DCRF Rider",
			})

			periods = append(periods, clipXcelPeriods([]types.UtilityFeesPeriod{
				{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: ctLocation},
					DollarsPerKWH:  0.003117,
					GridAdditional: true,
					Description:    "Texas GCRR Rider",
				},
			}, txApr1, time.Time{})...)

			periods = append(periods, clipXcelPeriods([]types.UtilityFeesPeriod{
				{
					TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: ctLocation},
					DollarsPerKWH:  0.004428,
					GridAdditional: true,
					Description:    "Texas TCRF Rider",
				},
			}, txJul10, time.Time{})...)
		}
	}

	// --- SOLAR EXPORT PLANS (Separate Generation Credits) ---
	scheme := defaultXcelNetMeteringScheme(plan, options.NetMeteringScheme)

	switch scheme {
	case "mn_occasional":
		// Pre-2026-04-01: flat $0.0360/kWh.
		// Effective 2026-04-01 (MPUC No. 2 33rd Rev. Sheet 9-3 Rate Code A51):
		// Summer (Jun-Sep) $0.03794/kWh, Winter (Oct-May) $0.03572/kWh.
		mnApr1 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation)
		for _, year := range years {
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)

			preAprPeriods := []types.UtilityFeesPeriod{{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.0360,
				SeparateGenerationCredit: true,
				Description:              "Minnesota Occasional Delivery Solar Export Credit",
			}}
			postAprPeriods := buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.September,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: 0.03794,
					OtherDescription:                   "Minnesota Occasional Delivery Summer Solar Export Credit (A51)",
				},
				{
					Year:                               year,
					MonthStart:                         time.October,
					MonthEnd:                           time.May,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: 0.03572,
					OtherDescription:                   "Minnesota Occasional Delivery Winter Solar Export Credit (A51)",
				},
			})

			if year < 2026 {
				periods = append(periods, preAprPeriods...)
			} else if year == 2026 {
				periods = append(periods, clipXcelPeriods(preAprPeriods, yearStart, mnApr1)...)
				periods = append(periods, clipXcelPeriods(postAprPeriods, mnApr1, yearEnd)...)
			} else {
				periods = append(periods, postAprPeriods...)
			}
		}

	case "mn_time_of_delivery":
		// Pre-2026-04-01: On-Peak $0.0527/kWh, Off-Peak $0.0302/kWh.
		// Effective 2026-04-01 (MPUC No. 2 33rd Rev. Sheet 9-3 Rate Code A52, 09:00-21:00 weekdays excl. NSP holidays):
		// Summer (Jun-Sep): On-Peak $0.05143/kWh, Off-Peak $0.03065/kWh.
		// Winter (Oct-May): On-Peak $0.04307/kWh, Off-Peak $0.03186/kWh.
		mnApr1 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ctLocation)
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)

			preAprPeriods := buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					SpecificDates:                      holidays,
					OtherGenerationCreditDollarsPerKWH: 0.0302,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "Minnesota TOD Export Credit (Holiday Off-Peak)",
				},
				{
					Year:                         year,
					MonthStart:                   time.January,
					MonthEnd:                     time.December,
					SpecificDates:                holidays,
					SpecificDatesNot:             true,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{{
						Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
						Weekday:                       true,
						GenerationCreditDollarsPerKWH: 0.0527,
						Description:                   "Minnesota TOD Export Credit (On-Peak)",
					}},
					OtherGenerationCreditDollarsPerKWH: 0.0302,
					OtherDescription:                   "Minnesota TOD Export Credit (Off-Peak)",
				},
			})

			postAprPeriods := buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.September,
					SpecificDates:                      holidays,
					OtherGenerationCreditDollarsPerKWH: 0.03065,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "Minnesota TOD Summer Export Credit (Holiday Off-Peak)",
				},
				{
					Year:                         year,
					MonthStart:                   time.June,
					MonthEnd:                     time.September,
					SpecificDates:                holidays,
					SpecificDatesNot:             true,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{{
						Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
						Weekday:                       true,
						GenerationCreditDollarsPerKWH: 0.05143,
						Description:                   "Minnesota TOD Summer Export Credit (On-Peak)",
					}},
					OtherGenerationCreditDollarsPerKWH: 0.03065,
					OtherDescription:                   "Minnesota TOD Summer Export Credit (Off-Peak)",
				},
				{
					Year:                               year,
					MonthStart:                         time.October,
					MonthEnd:                           time.May,
					SpecificDates:                      holidays,
					OtherGenerationCreditDollarsPerKWH: 0.03186,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "Minnesota TOD Winter Export Credit (Holiday Off-Peak)",
				},
				{
					Year:                         year,
					MonthStart:                   time.October,
					MonthEnd:                     time.May,
					SpecificDates:                holidays,
					SpecificDatesNot:             true,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{{
						Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
						Weekday:                       true,
						GenerationCreditDollarsPerKWH: 0.04307,
						Description:                   "Minnesota TOD Winter Export Credit (On-Peak)",
					}},
					OtherGenerationCreditDollarsPerKWH: 0.03186,
					OtherDescription:                   "Minnesota TOD Winter Export Credit (Off-Peak)",
				},
			})

			if year < 2026 {
				periods = append(periods, preAprPeriods...)
			} else if year == 2026 {
				periods = append(periods, clipXcelPeriods(preAprPeriods, yearStart, mnApr1)...)
				periods = append(periods, clipXcelPeriods(postAprPeriods, mnApr1, yearEnd)...)
			} else {
				periods = append(periods, postAprPeriods...)
			}
		}

	case "mi_dg":
		// M.P.S.C. No. 2 Sheet D-52.3 Distributed Generation Program (DG-1):
		// Outflow is credited at Supply Energy Charge + PSCR factor (-$0.01009/kWh before 2026-11-01; -$0.02500/kWh from 2026-11-01).
		miNov1 := time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation)
		peakHours := getXcelMIPeakHours(options.PeakPeriodOption)

		buildMIDGPeriods := func(year int, pscrDollarsPerKWH float64) []types.UtilityFeesPeriod {
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)
			if plan == "xcel_mi_tou" {
				holidays := getXcelNSPHolidays(year)
				return buildPeriods(ctLocation, []touSimplifiedPeriod{
					{
						Year:                               year,
						MonthStart:                         time.January,
						MonthEnd:                           time.December,
						SpecificDates:                      holidays,
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: 0.0402 + pscrDollarsPerKWH,
						OtherDescription:                   "Michigan DG-1 TOU Export Credit (Holiday Off-Peak)",
					},
					{
						Year:                         year,
						MonthStart:                   time.January,
						MonthEnd:                     time.December,
						SpecificDates:                holidays,
						SpecificDatesNot:             true,
						OnlySeparateGenerationCredit: true,
						HoursAndDays: []touSimplifiedHoursAndDays{{
							Hours:                         []types.UtilityHourPeriod{peakHours},
							Weekday:                       true,
							GenerationCreditDollarsPerKWH: 0.1607 + pscrDollarsPerKWH,
							Description:                   "Michigan DG-1 TOU Export Credit (On-Peak)",
						}},
						OtherGenerationCreditDollarsPerKWH: 0.0402 + pscrDollarsPerKWH,
						OtherDescription:                   "Michigan DG-1 TOU Export Credit (Off-Peak)",
					},
				})
			}
			return []types.UtilityFeesPeriod{{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.09425 + pscrDollarsPerKWH,
				SeparateGenerationCredit: true,
				Description:              "Michigan DG-1 Standard Export Credit",
			}}
		}

		for _, year := range years {
			yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
			yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)
			if year < 2026 {
				periods = append(periods, buildMIDGPeriods(year, -0.01009)...)
			} else if year == 2026 {
				periods = append(periods, clipXcelPeriods(buildMIDGPeriods(year, -0.01009), yearStart, miNov1)...)
				periods = append(periods, clipXcelPeriods(buildMIDGPeriods(year, -0.02500), miNov1, yearEnd)...)
			} else {
				periods = append(periods, buildMIDGPeriods(year, -0.02500)...)
			}
		}

	case "wi_pg2b":
		// Wisconsin Schedule Pg-2B (Sheets E 56.6-E 56.61, eff. 2026-01-01):
		// Peak hours: 09:00-21:00 weekdays excluding NSP holidays.
		// Secondary voltage loss-adjusted (1.07911) Energy + $0.00705 Transmission credits:
		// Summer (Jun-Sep) On-Peak: ($0.05297 + $0.00705) * 1.07911 = $0.064768/kWh
		// Winter (Oct-May) On-Peak: ($0.04106 + $0.00705) * 1.07911 = $0.051916/kWh
		// Off-Peak (all months):    ($0.03153 + $0.00705) * 1.07911 = $0.041632/kWh
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)

			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.September,
					SpecificDates:                      holidays,
					OtherGenerationCreditDollarsPerKWH: 0.041632,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "Wisconsin PG-2B Summer Export Credit (Holiday Off-Peak)",
				},
				{
					Year:                         year,
					MonthStart:                   time.June,
					MonthEnd:                     time.September,
					SpecificDates:                holidays,
					SpecificDatesNot:             true,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{{
						Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
						Weekday:                       true,
						GenerationCreditDollarsPerKWH: 0.064768,
						Description:                   "Wisconsin PG-2B Summer Export Credit (On-Peak)",
					}},
					OtherGenerationCreditDollarsPerKWH: 0.041632,
					OtherDescription:                   "Wisconsin PG-2B Summer Export Credit (Off-Peak)",
				},
				{
					Year:                               year,
					MonthStart:                         time.October,
					MonthEnd:                           time.May,
					SpecificDates:                      holidays,
					OtherGenerationCreditDollarsPerKWH: 0.041632,
					OnlySeparateGenerationCredit:       true,
					OtherDescription:                   "Wisconsin PG-2B Winter Export Credit (Holiday Off-Peak)",
				},
				{
					Year:                         year,
					MonthStart:                   time.October,
					MonthEnd:                     time.May,
					SpecificDates:                holidays,
					SpecificDatesNot:             true,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{{
						Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
						Weekday:                       true,
						GenerationCreditDollarsPerKWH: 0.051916,
						Description:                   "Wisconsin PG-2B Winter Export Credit (On-Peak)",
					}},
					OtherGenerationCreditDollarsPerKWH: 0.041632,
					OtherDescription:                   "Wisconsin PG-2B Winter Export Credit (Off-Peak)",
				},
			})...)
		}

	case "sd_occasional":
		// flat credit of $0.0316/kWh for all hours
		for _, year := range years {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.0316,
				SeparateGenerationCredit: true,
				Description:              "South Dakota Occasional Solar Export Credit",
			})
		}

	case "sd_time_of_delivery":
		// On-peak: $0.0397/kWh, Off-peak: $0.0272/kWh. Peak: weekdays 9 AM - 9 PM, excluding NSP holidays.
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)

			// Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.0272,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "South Dakota TOD Export Credit (Holiday Off-Peak)",
			}})...)

			// Regular Days On-Peak and Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.January,
				MonthEnd:                     time.December,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.0397,
					Description:                   "South Dakota TOD Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.0272,
				OtherDescription:                   "South Dakota TOD Export Credit (Off-Peak)",
			}})...)
		}

	case "nd_net_energy_billing":
		// flat credit of $0.03646/kWh for all hours
		for _, year := range years {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation),
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.03646,
				SeparateGenerationCredit: true,
				Description:              "North Dakota Net Energy Billing Solar Export Credit",
			})
		}

	case "nd_time_of_day_purchase":
		// Seasonal TOD Purchase rates:
		// Summer (Jun-Sep): On-Peak $0.05143, Off-Peak $0.03065
		// Winter (Oct-May): On-Peak $0.04307, Off-Peak $0.03186
		for _, year := range years {
			holidays := getXcelNSPHolidays(year)

			// --- Summer (June 1 - Sept 30) ---
			// Summer Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.June,
				MonthEnd:                           time.September,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.03065,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "North Dakota TOD Summer Export Credit (Holiday Off-Peak)",
			}})...)

			// Summer Regular Days
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.June,
				MonthEnd:                     time.September,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.05143,
					Description:                   "North Dakota TOD Summer Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.03065,
				OtherDescription:                   "North Dakota TOD Summer Export Credit (Off-Peak)",
			}})...)

			// --- Winter (Oct 1 - May 31) ---
			// Winter Holidays and Weekends Off-Peak
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                               year,
				MonthStart:                         time.October,
				MonthEnd:                           time.May,
				SpecificDates:                      holidays,
				OtherGenerationCreditDollarsPerKWH: 0.03186,
				OnlySeparateGenerationCredit:       true,
				OtherDescription:                   "North Dakota TOD Winter Export Credit (Holiday Off-Peak)",
			}})...)

			// Winter Regular Days
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{{
				Year:                         year,
				MonthStart:                   time.October,
				MonthEnd:                     time.May,
				SpecificDates:                holidays,
				SpecificDatesNot:             true,
				OnlySeparateGenerationCredit: true,
				HoursAndDays: []touSimplifiedHoursAndDays{{
					Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: 0.04307,
					Description:                   "North Dakota TOD Winter Export Credit (On-Peak)",
				}},
				OtherGenerationCreditDollarsPerKWH: 0.03186,
				OtherDescription:                   "North Dakota TOD Winter Export Credit (Off-Peak)",
			}})...)
		}

	case "tx_net_billing":
		// Texas Net Billing credits follow monthly SPS Secondary Fuel Cost Factors (carried forward until superseded).
		for _, year := range years {
			for month := time.January; month <= time.December; month++ {
				exportDollarsPerKWH := getXcelTXNetBillingRate(year, month)
				startMonth := time.Date(year, month, 1, 0, 0, 0, 0, ctLocation)
				endMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, ctLocation)

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       startMonth,
						End:         endMonth,
						LocationPtr: ctLocation,
					},
					DollarsPerKWH:            exportDollarsPerKWH,
					SeparateGenerationCredit: true,
					Description:              fmt.Sprintf("Texas Net Billing Export Credit (%s)", month.String()[:3]),
				})
			}
		}
	}

	return periods
}

// xcelUtilityInfo returns the UtilityProviderInfo for Xcel Energy
func xcelUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "xcel",
		Name: "Xcel Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "xcel_co_tou",
				Name: "Colorado Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringCredits",
						Name:        "Net Metering",
						Type:        types.UtilityOptionTypeSwitch,
						Description: "Enable if you are enrolled in net metering.",
						Default:     true,
						Hidden:      true,
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_co_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_standard",
				Name: "Minnesota Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_standard_heating",
				Name: "Minnesota Residential Standard Space Heating Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_standard_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_tou",
				Name: "Minnesota Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mn_tou_heating",
				Name: "Minnesota Residential Time-of-Use Space Heating",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mn_occasional", Name: "Minnesota Occasional Delivery"},
							{Value: "mn_time_of_delivery", Name: "Minnesota Time of Delivery"},
						},
						Default: "mn_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mn_tou_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_standard",
				Name: "North Dakota Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_standard_heating",
				Name: "North Dakota Residential Standard Space Heating Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_standard_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_tou",
				Name: "North Dakota Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_nd_tou_heating",
				Name: "North Dakota Residential Time-of-Use Space Heating",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "nd_net_energy_billing", Name: "North Dakota Net Energy Billing"},
							{Value: "nd_time_of_day_purchase", Name: "North Dakota Time of Day Purchase"},
						},
						Default: "nd_net_energy_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_nd_tou_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_sd_standard",
				Name: "South Dakota Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "sd_occasional", Name: "South Dakota Occasional"},
							{Value: "sd_time_of_delivery", Name: "South Dakota Time of Delivery"},
						},
						Default: "sd_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_sd_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_sd_tou",
				Name: "South Dakota Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "sd_occasional", Name: "South Dakota Occasional"},
							{Value: "sd_time_of_delivery", Name: "South Dakota Time of Delivery"},
						},
						Default: "sd_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_sd_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_sd_tou_heating",
				Name: "South Dakota Residential Time-of-Use Space Heating",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "sd_occasional", Name: "South Dakota Occasional"},
							{Value: "sd_time_of_delivery", Name: "South Dakota Time of Delivery"},
						},
						Default: "sd_occasional",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_sd_tou_heating", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mi_standard",
				Name: "Michigan Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or net metering program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mi_dg", Name: "Distributed Generation Program"},
							{Value: "net", Name: "Net Metering (1:1) (before Jan 1, 2023)"},
						},
						Default: "mi_dg",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mi_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_mi_tou",
				Name: "Michigan Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or net metering program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "mi_dg", Name: "Distributed Generation Program"},
							{Value: "net", Name: "Net Metering (1:1) (before Jan 1, 2023)"},
						},
						Default: "mi_dg",
					},
					{
						Field:       "peakPeriodOption",
						Name:        "On-Peak Period Option",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your chosen 12-hour peak period option.",
						Choices: []types.UtilityOptionChoice{
							{Value: "1", Name: "Option 1 (9:00 a.m. - 9:00 p.m.)"},
							{Value: "2", Name: "Option 2 (8:30 a.m. - 8:30 p.m.)"},
							{Value: "3", Name: "Option 3 (8:00 a.m. - 8:00 p.m.)"},
							{Value: "4", Name: "Option 4 (7:30 a.m. - 7:30 p.m.)"},
							{Value: "5", Name: "Option 5 (7:00 a.m. - 7:00 p.m.)"},
						},
						Default: "1",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_mi_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_wi_standard",
				Name: "Wisconsin Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "wi_pg2b", Name: "Parallel Generation (PG-2B)"},
						},
						Default: "wi_pg2b",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_wi_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_wi_tou",
				Name: "Wisconsin Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "wi_pg2b", Name: "Parallel Generation (PG-2B)"},
						},
						Default: "wi_pg2b",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_wi_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_tx_standard",
				Name: "Texas Residential Standard Service",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "tx_net_billing", Name: "Texas Net Billing"},
						},
						Default: "tx_net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_tx_standard", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:   "xcel_tx_tou",
				Name: "Texas Residential Time-of-Use",
				Options: []types.UtilityRateOption{
					{
						Field:       "netMeteringScheme",
						Name:        "Net Metering / Export Scheme",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your solar billing plan or export credit program.",
						Choices: []types.UtilityOptionChoice{
							{Value: "tx_net_billing", Name: "Texas Net Billing"},
						},
						Default: "tx_net_billing",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return xcelPeriods("xcel_tx_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
