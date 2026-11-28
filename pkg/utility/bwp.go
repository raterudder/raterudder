package utility

// Tariff Audit Metadata
// - Provider(s): bwp (Burbank Water and Power (BWP))
// - Official Sources:
//   - https://www.burbankwaterandpower.com/documents/d/guest/burbank_adopted_fee_schedule (City of Burbank FY 2026-27 Adopted Citywide Fee Schedule, Article X, Sections 1, 11(C), and 13; Resolutions 25-29,668, 26-29,706, and 22-29,321)
//   - https://www.burbankwaterandpower.com/electric-rates (BWP 2026 Electric Rates schedule)
//   - https://www.burbankwaterandpower.com/documents/d/guest/bwp-rules-and-regulations (BWP Rules and Regulations Sections 1.04(e) and 3.25(a)(iv))
//   - https://www.burbankwaterandpower.com/solar-net-bill-breakdown (BWP Solar Net Billing Bill Breakdown)
//   - https://www.burbankwaterandpower.com/documents/d/guest/solar_net_billing_bill_guide (BWP How to Read Your Bill With Solar Net Billing guide, v10.29.26)
// - Effective Window: 2026-01-01 through 2027-12-31
// - Revision Cadence: Annual (Jan 1 Adopted Electric Rate Steps & Jul 1 Citywide Fee Schedule)
// - Next Audit Due: 2027-07-01 (Check FY 2027-28 Adopted Citywide Fee Schedule for Section 11(C) Avoided Cost of Energy export credit updates and any Section 13 ECAC adjustments)
// - Last Verified: 2026-11-26
// - Modeling Notes: Models Tier 1 (first 300 kWh/mo) composite volumetric energy rates including $0.0340/kWh Section 13 ECAC on bwp_residential ($0.1800/kWh in 2026, $0.2084/kWh effective 2027-01-01) and bwp_res_tou_ev ($0.3902/$0.2617/$0.1555/kWh in 2026, $0.4354/$0.2920/$0.1735/kWh effective 2027-01-01 per Section 1(C) R-TOU-EV and Section 1(E) Solar Net Billing); excludes fixed monthly Customer Service Charges ($19.50/mo in 2026, $25.00/mo in 2027) and Service Size Charges ($2.18-$13.37/mo in 2026, $2.60-$15.40/mo in 2027); netMeteringScheme defaults to "net_billing" with monthly TOU Avoided Cost of Energy export credits under Article X, Section 11(C) ($0.1186/$0.0825/$0.0702/kWh Jun-Oct On-Peak/Mid-Peak/Off-Peak and $0.0696/kWh Nov-May all hours, rather than the $0.0455/kWh Section 11(B) annual net surplus true-up rate), while "net" models Legacy NEM 1:1 retail net metering.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Article X, Section 13: Energy Cost Adjustment Charge (ECAC) for 2026 and 2027.
	bwpECACDollarsPerKWH = 0.0340

	// Article X, Section 1(A): Residential Service Tier 1 (first 300 kWh/mo) base energy charges.
	bwpRes2026BaseDollarsPerKWH = 0.1460
	bwpRes2027BaseDollarsPerKWH = 0.1744

	// Article X, Section 1(C) & Section 1(E): R-TOU-EV / Solar Net Billing base energy charges.
	bwpTOU2026SummerOnPeakBaseDollarsPerKWH = 0.3562
	bwpTOU2026MidPeakBaseDollarsPerKWH      = 0.2277
	bwpTOU2026OffPeakBaseDollarsPerKWH      = 0.1215

	bwpTOU2027SummerOnPeakBaseDollarsPerKWH = 0.4014
	bwpTOU2027MidPeakBaseDollarsPerKWH      = 0.2580
	bwpTOU2027OffPeakBaseDollarsPerKWH      = 0.1395

	// Article X, Section 11(C): Avoided Cost of Energy (Solar Net Billing monthly TOU export credits).
	bwpExportSummerOnPeakDollarsPerKWH  = 0.1186
	bwpExportSummerMidPeakDollarsPerKWH = 0.0825
	bwpExportSummerOffPeakDollarsPerKWH = 0.0702
	bwpExportWinterDollarsPerKWH        = 0.0696
)

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

	scheme := opts.NetMeteringScheme
	if scheme == "" {
		scheme = "net_billing"
	}

	// On-Peak hours (Summer only): 4 PM - 7 PM (16:00 - 19:00) Weekdays
	summerOnPeakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}}
	// Mid-Peak hours (Summer): 8 AM - 4 PM & 7 PM - 11 PM (08:00 - 16:00 & 19:00 - 23:00) Weekdays
	summerMidPeakHours := []types.UtilityHourPeriod{
		{HourStart: 8, HourEnd: 16},
		{HourStart: 19, HourEnd: 23},
	}
	// Mid-Peak hours (Winter): 8 AM - 11 PM (08:00 - 23:00) Weekdays
	winterMidPeakHours := []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 23}}

	for _, year := range years {
		holidays := getBWPHolidays(year)

		switch plan {
		case "bwp_residential":
			// Rate: Residential Service (Flat), Tier 1 (first 300 kWh/mo)
			// 2026 Composite: $0.1460 + $0.0340 ECAC = $0.1800/kWh
			// 2027 Composite: $0.1744 + $0.0340 ECAC = $0.2084/kWh
			flatDollarsPerKWH := bwpRes2026BaseDollarsPerKWH + bwpECACDollarsPerKWH
			if year >= 2027 {
				flatDollarsPerKWH = bwpRes2027BaseDollarsPerKWH + bwpECACDollarsPerKWH
			}

			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: flatDollarsPerKWH,
					OtherDescription:   "BWP Residential Flat Rate",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplified)...)

		case "bwp_res_tou_ev":
			// Rate: Optional TOU Rate for EV Owners (R-TOU-EV) / Solar Net Billing
			// Summer: June 1 – October 31
			// Winter / Non-Summer: November 1 – May 31
			// 2026 Composite (Base + $0.0340 ECAC): $0.3902 On-Peak, $0.2617 Mid-Peak, $0.1555 Off-Peak
			// 2027 Composite (Base + $0.0340 ECAC): $0.4354 On-Peak, $0.2920 Mid-Peak, $0.1735 Off-Peak
			summerOnPeakDollarsPerKWH := bwpTOU2026SummerOnPeakBaseDollarsPerKWH + bwpECACDollarsPerKWH
			midPeakDollarsPerKWH := bwpTOU2026MidPeakBaseDollarsPerKWH + bwpECACDollarsPerKWH
			offPeakDollarsPerKWH := bwpTOU2026OffPeakBaseDollarsPerKWH + bwpECACDollarsPerKWH
			if year >= 2027 {
				summerOnPeakDollarsPerKWH = bwpTOU2027SummerOnPeakBaseDollarsPerKWH + bwpECACDollarsPerKWH
				midPeakDollarsPerKWH = bwpTOU2027MidPeakBaseDollarsPerKWH + bwpECACDollarsPerKWH
				offPeakDollarsPerKWH = bwpTOU2027OffPeakBaseDollarsPerKWH + bwpECACDollarsPerKWH
			}

			simplifiedConsumption := []touSimplifiedPeriod{
				// --- Summer ---
				// Summer Holiday (all hours off-peak)
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.October,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: offPeakDollarsPerKWH,
					OtherDescription:   "BWP R-TOU-EV Summer Off-Peak (Holiday/Weekend)",
				},
				// Summer Weekdays & Weekends
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.October,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         summerOnPeakHours,
							Weekday:       true,
							DollarsPerKWH: summerOnPeakDollarsPerKWH,
							Description:   "BWP R-TOU-EV Summer On-Peak",
						},
						{
							Name:          "Mid-Peak",
							Hours:         summerMidPeakHours,
							Weekday:       true,
							DollarsPerKWH: midPeakDollarsPerKWH,
							Description:   "BWP R-TOU-EV Summer Mid-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: offPeakDollarsPerKWH,
					OtherDescription:   "BWP R-TOU-EV Summer Off-Peak",
				},

				// --- Winter ---
				// Winter Holiday (all hours off-peak)
				{
					Year:               year,
					MonthStart:         time.November,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: offPeakDollarsPerKWH,
					OtherDescription:   "BWP R-TOU-EV Winter Off-Peak (Holiday/Weekend)",
				},
				// Winter Weekdays & Weekends
				{
					Year:             year,
					MonthStart:       time.November,
					MonthEnd:         time.May,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Mid-Peak",
							Hours:         winterMidPeakHours,
							Weekday:       true,
							DollarsPerKWH: midPeakDollarsPerKWH,
							Description:   "BWP R-TOU-EV Winter Mid-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: offPeakDollarsPerKWH,
					OtherDescription:   "BWP R-TOU-EV Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplifiedConsumption)...)

		default:
			continue
		}

		// Article X, Section 11(C): Solar Net Billing monthly TOU Avoided Cost of Energy export credits.
		// Legacy NEM ("net") remains 1:1 retail net metering (no SeparateGenerationCredit periods).
		if scheme == "net_billing" {
			simplifiedExport := []touSimplifiedPeriod{
				// Summer Holidays (Jun - Oct): $0.0702/kWh all hours
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.October,
					SpecificDates:                      holidays,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: bwpExportSummerOffPeakDollarsPerKWH,
					OtherDescription:                   "BWP Solar Net Billing Summer Off-Peak Export Credit (Holiday)",
				},
				// Summer Non-Holidays (Jun - Oct): On-Peak $0.1186, Mid-Peak $0.0825, Off-Peak/Weekend $0.0702/kWh
				{
					Year:                         year,
					MonthStart:                   time.June,
					MonthEnd:                     time.October,
					SpecificDates:                holidays,
					SpecificDatesNot:             true,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:                         summerOnPeakHours,
							Weekday:                       true,
							GenerationCreditDollarsPerKWH: bwpExportSummerOnPeakDollarsPerKWH,
							Description:                   "BWP Solar Net Billing Summer On-Peak Export Credit",
						},
						{
							Hours:                         summerMidPeakHours,
							Weekday:                       true,
							GenerationCreditDollarsPerKWH: bwpExportSummerMidPeakDollarsPerKWH,
							Description:                   "BWP Solar Net Billing Summer Mid-Peak Export Credit",
						},
					},
					OtherGenerationCreditDollarsPerKWH: bwpExportSummerOffPeakDollarsPerKWH,
					OtherDescription:                   "BWP Solar Net Billing Summer Off-Peak Export Credit",
				},
				// Winter / Non-Summer (Nov - May): $0.0696/kWh all hours
				{
					Year:                               year,
					MonthStart:                         time.November,
					MonthEnd:                           time.May,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: bwpExportWinterDollarsPerKWH,
					OtherDescription:                   "BWP Solar Net Billing Winter Export Credit",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplifiedExport)...)
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
