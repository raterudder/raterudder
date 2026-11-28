package utility

// Tariff Audit Metadata
// - Provider(s): we (WE Energies (WE))
// - Official Sources:
//   - https://www.we-energies.com/pdfs/etariffs/wisconsin/elecrateswi.pdf (WE Energies Electric Rates Volume 19: Sheets 19-27 Fuel Cost Plan & Adjustment, Sheet 138 Rg-1, Sheets 201.4-201.14 CGS-NP / CGS-NM / CGS-CU, Sheet 221 Rg-2, and Sheets 225.1-225.2 EV-R)
//   - https://www.we-energies.com/services/pdf/net-metering.pdf (WE Energies Understanding Bidirectional Net Metering guide for CGS-NM 1:1 monthly kWh netting)
//   - https://apps.psc.wi.gov/ERF/ERFview/viewdoc.aspx?docid=589882 (PSCW Docket 6630-TE-2026 Approval Letter and Amendment 813 approving $0.04440/kWh CGS-CU On-Peak capacity credits effective June 1, 2026)
//   - https://apps.psc.wi.gov/ERF/ERFview/viewdoc.aspx?docid=607128 (PSCW Docket 6630-FR-2025 Final Decision ordering -$0.02065/kWh October 2026 fuel cost refund)
//   - https://apps.psc.wi.gov/ERF/ERFview/viewdoc.aspx?docid=619420 (PSCW Docket 5-UR-112 Open Meeting Minutes, Determination 81 shifting 2027 EV-R charging window to 00:00-07:00)
//   - https://apps.psc.wi.gov/ERF/ERFview/viewdoc.aspx?docid=605523 (PSCW Docket 5-UR-112 Ex.-PSC-Jurvich-1 2027 revenue and rate design schedules)
//   - https://apps.psc.wi.gov/ERF/ERFview/viewdoc.aspx?docid=586059 (PSCW Docket 5-UR-112 Ex.-WEPCO WG-Nelson-7 proposed 2027 tariff schedules)
// - Effective Window: 2026-01-01 through 2027-12-31
// - Revision Cadence: Annual (Jan 1 PSCW Base/Fuel Rate Order & Jun 1 CGS-CU Avoided Capacity Filing) + Periodic Fuel Cost Refunds
// - Next Audit Due: 2026-12-28 (Check PSCW Docket 5-UR-112 and Docket 6630-TE-2027 for signed final 2027 compliance tariff sheets for Rg-1, Rg-2, EV-R, and CGS)
// - Last Verified: 2026-11-28
// - Modeling Notes: Models composite volumetric energy charges (base energy + $0.00199/kWh + $0.00052/kWh riders) on we_rg1 ($0.19593/kWh standard; $0.17528/kWh in Oct 2026 with -$0.02065/kWh Docket 6630-FR-2025 fuel refund) and we_rg2 ($0.30335/kWh On-Peak, $0.10279/kWh Off-Peak standard; $0.28270/$0.08214/kWh in Oct 2026) with Sunday-only holiday shifts per Volume 19 Sheet 221 (Saturday holidays stay on Saturday). Carries verified 2026 base rates into 2027 until PSCW dockets signed 2027 compliance sheets for Ex.-PSC-Jurvich-1, Ex.-WEPCO WG-Nelson-7, and Docket 6630-TE-2027, while applying Determination 81 (00:00-07:00 EV-R discount window effective 2027-01-01 vs. 00:00-08:00 in 2026). Solar export schemes model 1:1 monthly netting under "net" (CGS-NM), separate generation + CGS-CU On-Peak capacity credits under "cgs" ($0.03636/kWh flat on we_rg1; $0.08093/kWh Jan-May 2026 On-Peak, $0.09346/kWh Jun-Sep On-Peak, $0.08303/kWh Oct 2026-2027 Non-Summer On-Peak, and $0.03290/$0.03289/kWh Summer/Non-Summer Off-Peak on we_rg2), and $0.00/kWh non-purchase exports under "cgs_np" (CGS-NP, Sheet 201.4). Excludes fixed $0.49315/day ($15.00/mo) Facilities Charge, closed COEV-R and WHEV-R EV riders, and expired legacy CGS 2, CGS 6, and CGS 8 plans.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Volume 19 Sheet 138 (Rg-1) & Sheet 221 (Rg-2) base energy charges and volumetric riders.
	weRG1BaseDollarsPerKWH        = 0.19342
	weRG2OnPeakBaseDollarsPerKWH  = 0.30084
	weRG2OffPeakBaseDollarsPerKWH = 0.10028
	weFuelRiderDollarsPerKWH      = 0.00199
	weEnvRiderDollarsPerKWH       = 0.00052

	// Volume 19 Sheet 20 & PSCW Docket 6630-FR-2025 October 2026 fuel cost refund credit.
	weOct2026FuelRefundDollarsPerKWH = -0.02065

	// Volume 19 Sheet 201.13 & PSCW Docket 6630-TE-2026 (Amendment 813) CGS-CU export credits.
	weRG1CGSDollarsPerKWH                       = 0.03636
	weRG2CGSSummerEnergyOnPeakDollarsPerKWH     = 0.04906
	weRG2CGSSummerOffPeakDollarsPerKWH          = 0.03290
	weRG2CGSNonSummerEnergyOnPeakDollarsPerKWH  = 0.03863
	weRG2CGSNonSummerOffPeakDollarsPerKWH       = 0.03289
	weRG2CGSCapacityPreJun2026DollarsPerKWH     = 0.04230
	weRG2CGSCapacityJun2026OnwardsDollarsPerKWH = 0.04440

	// Volume 19 Sheet 225.1 EV-R off-peak charging discounts.
	weRG1EVDiscountDollarsPerKWH = -0.04000
	weRG2EVDiscountDollarsPerKWH = -0.01000
)

func shiftWEWeekendHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

func getWEHolidays(year int) []string {
	holidays := []time.Time{
		shiftWEWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftWEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftWEWeekendHoliday(christmasDay(year)),
		shiftWEWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func weRG2ConsumptionSpan(year int, monthStart, monthEnd time.Month, holidays []string, peakHours []types.UtilityHourPeriod, onPeakDollarsPerKWH, offPeakDollarsPerKWH float64) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// Holiday (all-day off-peak)
		{
			Year:               year,
			MonthStart:         monthStart,
			MonthEnd:           monthEnd,
			SpecificDates:      holidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "WE Energies RG2 Off-Peak (Holiday)",
		},
		// Regular weekdays (peak and off-peak) and weekends
		{
			Year:             year,
			MonthStart:       monthStart,
			MonthEnd:         monthEnd,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         peakHours,
					Weekday:       true,
					DollarsPerKWH: onPeakDollarsPerKWH,
					Description:   "WE Energies RG2 On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "WE Energies RG2 Off-Peak",
		},
	}
}

func weRG2CGSSpan(year int, monthStart, monthEnd time.Month, holidays []string, peakHours []types.UtilityHourPeriod, onPeakCreditDollarsPerKWH, offPeakCreditDollarsPerKWH float64, seasonLabel string) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// Holiday Credit
		{
			Year:                               year,
			MonthStart:                         monthStart,
			MonthEnd:                           monthEnd,
			SpecificDates:                      holidays,
			OnlySeparateGenerationCredit:       true,
			OtherGenerationCreditDollarsPerKWH: offPeakCreditDollarsPerKWH,
			OtherDescription:                   "WE Energies CGS " + seasonLabel + " Off-Peak Credit (Holiday)",
		},
		// Weekday & Weekend Credit
		{
			Year:                         year,
			MonthStart:                   monthStart,
			MonthEnd:                     monthEnd,
			SpecificDates:                holidays,
			SpecificDatesNot:             true,
			OnlySeparateGenerationCredit: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours:                         peakHours,
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: onPeakCreditDollarsPerKWH,
					Description:                   "WE Energies CGS " + seasonLabel + " On-Peak Credit",
				},
			},
			OtherGenerationCreditDollarsPerKWH: offPeakCreditDollarsPerKWH,
			OtherDescription:                   "WE Energies CGS " + seasonLabel + " Off-Peak Credit",
		},
	}
}

func wePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getWEHolidays(year)

		switch plan {
		case "we_rg1":
			// RG1: Flat Rate ($0.19593/kWh standard; $0.17528/kWh in Oct 2026)
			standardDollarsPerKWH := weRG1BaseDollarsPerKWH + weFuelRiderDollarsPerKWH + weEnvRiderDollarsPerKWH
			var simplified []touSimplifiedPeriod
			if year == 2026 {
				simplified = []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.September,
						OtherDollarsPerKWH: standardDollarsPerKWH,
						OtherDescription:   "WE Energies RG1 Flat Rate",
					},
					{
						Year:               year,
						MonthStart:         time.October,
						MonthEnd:           time.October,
						OtherDollarsPerKWH: standardDollarsPerKWH + weOct2026FuelRefundDollarsPerKWH,
						OtherDescription:   "WE Energies RG1 Flat Rate (Oct 2026 Fuel Refund)",
					},
					{
						Year:               year,
						MonthStart:         time.November,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: standardDollarsPerKWH,
						OtherDescription:   "WE Energies RG1 Flat Rate",
					},
				}
			} else {
				simplified = []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: standardDollarsPerKWH,
						OtherDescription:   "WE Energies RG1 Flat Rate",
					},
				}
			}

			switch opts.NetMeteringScheme {
			case "cgs":
				simplified = append(simplified, touSimplifiedPeriod{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: weRG1CGSDollarsPerKWH,
					OtherDescription:                   "WE Energies CGS Flat Credit",
				})
			case "cgs_np":
				simplified = append(simplified, touSimplifiedPeriod{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: 0.0,
					OtherDescription:                   "WE Energies CGS-NP Non-Purchase Export ($0.00/kWh)",
				})
			}

			periods = append(periods, buildPeriods(ctLocation, simplified)...)

		case "we_rg2":
			// RG2: Time-of-Use Rate ($0.30335/$0.10279/kWh standard; $0.28270/$0.08214/kWh in Oct 2026)
			onPeakDollarsPerKWH := weRG2OnPeakBaseDollarsPerKWH + weFuelRiderDollarsPerKWH + weEnvRiderDollarsPerKWH
			offPeakDollarsPerKWH := weRG2OffPeakBaseDollarsPerKWH + weFuelRiderDollarsPerKWH + weEnvRiderDollarsPerKWH

			// Determine peak hours
			var peakStart, peakEnd int
			switch opts.PeakPeriodOption {
			case "peak_7_7":
				peakStart, peakEnd = 7, 19
			case "peak_8_8":
				peakStart, peakEnd = 8, 20
			case "peak_10_10":
				peakStart, peakEnd = 10, 22
			default: // default is 9 a.m. to 9 p.m.
				peakStart, peakEnd = 9, 21
			}

			peakHours := []types.UtilityHourPeriod{{HourStart: peakStart, HourEnd: peakEnd}}

			var simplifiedConsumption []touSimplifiedPeriod
			if year == 2026 {
				simplifiedConsumption = append(simplifiedConsumption,
					weRG2ConsumptionSpan(year, time.January, time.September, holidays, peakHours, onPeakDollarsPerKWH, offPeakDollarsPerKWH)...,
				)
				simplifiedConsumption = append(simplifiedConsumption,
					weRG2ConsumptionSpan(year, time.October, time.October, holidays, peakHours, onPeakDollarsPerKWH+weOct2026FuelRefundDollarsPerKWH, offPeakDollarsPerKWH+weOct2026FuelRefundDollarsPerKWH)...,
				)
				simplifiedConsumption = append(simplifiedConsumption,
					weRG2ConsumptionSpan(year, time.November, time.December, holidays, peakHours, onPeakDollarsPerKWH, offPeakDollarsPerKWH)...,
				)
			} else {
				simplifiedConsumption = weRG2ConsumptionSpan(year, time.January, time.December, holidays, peakHours, onPeakDollarsPerKWH, offPeakDollarsPerKWH)
			}
			periods = append(periods, buildPeriods(ctLocation, simplifiedConsumption)...)

			switch opts.NetMeteringScheme {
			case "cgs":
				// CGS-CU Generation + On-Peak Avoided Capacity Credits (Sheet 201.13 & Docket 6630-TE-2026)
				summerCGSOnPeakDollarsPerKWH := weRG2CGSSummerEnergyOnPeakDollarsPerKWH + weRG2CGSCapacityJun2026OnwardsDollarsPerKWH       // 0.09346
				nonSummerCGSOnPeakDollarsPerKWH := weRG2CGSNonSummerEnergyOnPeakDollarsPerKWH + weRG2CGSCapacityJun2026OnwardsDollarsPerKWH // 0.08303

				var simplifiedCGS []touSimplifiedPeriod
				if year == 2026 {
					janMayCGSOnPeakDollarsPerKWH := weRG2CGSNonSummerEnergyOnPeakDollarsPerKWH + weRG2CGSCapacityPreJun2026DollarsPerKWH // 0.08093
					simplifiedCGS = append(simplifiedCGS,
						weRG2CGSSpan(year, time.January, time.May, holidays, peakHours, janMayCGSOnPeakDollarsPerKWH, weRG2CGSNonSummerOffPeakDollarsPerKWH, "Non-Summer")...,
					)
					simplifiedCGS = append(simplifiedCGS,
						weRG2CGSSpan(year, time.June, time.September, holidays, peakHours, summerCGSOnPeakDollarsPerKWH, weRG2CGSSummerOffPeakDollarsPerKWH, "Summer")...,
					)
					simplifiedCGS = append(simplifiedCGS,
						weRG2CGSSpan(year, time.October, time.December, holidays, peakHours, nonSummerCGSOnPeakDollarsPerKWH, weRG2CGSNonSummerOffPeakDollarsPerKWH, "Non-Summer")...,
					)
				} else {
					simplifiedCGS = append(simplifiedCGS,
						weRG2CGSSpan(year, time.June, time.September, holidays, peakHours, summerCGSOnPeakDollarsPerKWH, weRG2CGSSummerOffPeakDollarsPerKWH, "Summer")...,
					)
					simplifiedCGS = append(simplifiedCGS,
						weRG2CGSSpan(year, time.October, time.May, holidays, peakHours, nonSummerCGSOnPeakDollarsPerKWH, weRG2CGSNonSummerOffPeakDollarsPerKWH, "Non-Summer")...,
					)
				}
				periods = append(periods, buildPeriods(ctLocation, simplifiedCGS)...)

			case "cgs_np":
				periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
					{
						Year:                               year,
						MonthStart:                         time.January,
						MonthEnd:                           time.December,
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: 0.0,
						OtherDescription:                   "WE Energies CGS-NP Non-Purchase Export ($0.00/kWh)",
					},
				})...)
			}
		}

		// Apply EV-R Discount Credit to Consumption Rates if enabled:
		// 00:00-08:00 in 2026 (Sheet 225.1), shifting to 00:00-07:00 effective 2027-01-01 (Docket 5-UR-112 Det. 81).
		if opts.EVCredit {
			evDiscountDollarsPerKWH := weRG1EVDiscountDollarsPerKWH
			if plan == "we_rg2" {
				evDiscountDollarsPerKWH = weRG2EVDiscountDollarsPerKWH
			}
			evHourEnd := 8
			if year >= 2027 {
				evHourEnd = 7
			}
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: evHourEnd}},
							DollarsPerKWH: evDiscountDollarsPerKWH,
							Description:   "WE Energies EV-R Charging Discount",
						},
					},
				},
			})...)
		}
	}
	return periods
}

func weUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Energy Metering (CGS-NM)"},
			{Value: "cgs", Name: "Customer Generating Systems (CGS-CU)"},
			{Value: "cgs_np", Name: "Non-Purchase Customer Generating Systems (CGS-NP)"},
		},
		Default: "net",
	}

	peakPeriodOption := types.UtilityRateOption{
		Field:       "peakPeriodOption",
		Name:        "On-Peak Period Choice",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select the on-peak period chosen for your account.",
		Choices: []types.UtilityOptionChoice{
			{Value: "peak_7_7", Name: "7:00 a.m. to 7:00 p.m."},
			{Value: "peak_8_8", Name: "8:00 a.m. to 8:00 p.m."},
			{Value: "peak_9_9", Name: "9:00 a.m. to 9:00 p.m."},
			{Value: "peak_10_10", Name: "10:00 a.m. to 10:00 p.m."},
		},
		Default: "peak_9_9",
	}

	evCreditOption := types.UtilityRateOption{
		Field:       "evCredit",
		Name:        "Electric Vehicle Residential (EV-R)",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Are you enrolled in the EV-R credit pilot program?",
		Default:     false,
	}

	return types.UtilityProviderInfo{
		ID:   "we",
		Name: "WE Energies (WE)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "we_rg1",
				Name:    "Schedule RG1 (Residential and Farm Service)",
				Options: []types.UtilityRateOption{evCreditOption, exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return wePeriods("we_rg1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "we_rg2",
				Name:    "Schedule RG2 (Residential Time-of-Use)",
				Options: []types.UtilityRateOption{peakPeriodOption, evCreditOption, exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return wePeriods("we_rg2", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
