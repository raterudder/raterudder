package utility

// Tariff Audit Metadata
// - Provider(s): dlc (Duquesne Light Co. (DLC))
// - Official Sources:
//   - https://www.duquesnelight.com/service-reliability/service-map/rates/tariff-resources (Duquesne Light Tariff Resources)
//   - https://www.duquesnelight.com/docs/default-source/default-document-library/tariff-no-25-(110)-effective-10-01-2026---rider-no-22---distribution-system-improvement-charge-(dsic)-quarterly-filing.pdf (Electric - Pa. P.U.C. No. 25 through Supplement No. 110)
//   - https://www.duquesnelight.com/docs/default-source/default-document-library/supplementno-111totariffno-25-riderno-8-defautservicesupply(dss)semi-annualupdateeffective12-01-2026.pdf?sfvrsn=a1942347_2 (Supplement No. 111 to Tariff No. 25, Rider No. 8 DSS effective December 1, 2026)
//   - https://www.duquesnelight.com/docs/default-source/default-document-library/actual---default-service-rate-calculation-model---residential---effective-12-1-26.pdf?sfvrsn=36602347_0 (DLC Default Service Rate Calculation Model - Residential effective December 1, 2026)
//   - https://www.papowerswitch.com/umbraco/api/shopapi/ZipSearch?serviceType=residential&zipcode=15222 (PA PUC PAPowerSwitch Residential PTC)
//   - Electric - Pa. P.U.C. No. 25 Supplements 85 through 111: Supplement No. 85 (4th Rev. Page No. 38 Rate RS, eff. Dec 20, 2024), Supplement No. 89 (4th Rev. Pages No. 135-136 Rider No. 21 Net Metering, eff. Jun 1, 2025), Supplement No. 92 (14th Rev. Page No. 98 Rider No. 8 DSS, eff. Jun 1, 2025), Supplement No. 97 (8th Rev. Page No. 93 Rider No. 5 USC, eff. Jan 1, 2026), Supplement No. 98 (12th Rev. Page No. 112 Rider No. 10 STAS, eff. Jan 1, 2026), Supplements No. 99/101/107/110 (27th-30th Rev. Page No. 137 Rider No. 22 DSIC, Q1-Q4 2026), Supplement No. 102 (13th Rev. Page No. 98A Rider No. 8 DSS, eff. Jun 1, 2026), Supplement No. 104 (7th Rev. Page No. 118 Rider No. 15A Phase V EE&C, eff. Jun 1, 2026), Supplement No. 105 (9th Rev. Page No. 142 Appendix A TSC, eff. Jun 1, 2026), Supplement No. 111 (14th Rev. Page No. 98A Rider No. 8 DSS, Docket No. P-2024-3048592, eff. Dec 1, 2026 - May 31, 2027)
// - Effective Window: 2026-01-01 through 2027-12-31 (Supplement No. 111 DSS effective 2026-12-01 through 2027-05-31 and Q4 2026 delivery/rider rates carried through 2027-12-31 until superseded)
// - Revision Cadence: Quarterly (Jan 1, Apr 1, Jul 1, Oct 1 Rider No. 22 DSIC) & Semi-Annual (Jun 1 & Dec 1 Rider No. 8 DSS; Jan 1 Rider No. 5 USC & Rider No. 10 STAS; Jun 1 Rider No. 15A EE&C & Appendix A TSC)
// - Next Audit Due: 2027-01-05 (Check Jan 1, 2027 Q1 Rider No. 22 DSIC, annual Rider No. 5 USC, and annual Rider No. 10 STAS filings)
// - Last Verified: 2026-11-21
// - Modeling Notes: Models Rate RS residential distribution + non-bypassable riders (USC, EE&C, quarterly DSIC applied to distribution+USC+EE&C before -0.0964% STAS, plus DSIC/STAS-exempt Appendix A TSC) and Rider No. 8 Default Service Supply / Time-of-Use Supply Rate Pilot charges; fixed $13.00/month Customer Charge is excluded from volumetric per-kWh rates; 1:1 kWh net metering under Rider No. 21.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Rate RS base distribution energy charge (Supplement No. 85, 4th Rev. Page No. 38, eff. Dec 20, 2024).
	// Fixed $13.00/month Customer Charge is excluded from per-kWh volumetric periods.
	dlcRSDistributionDollarsPerKWH = 0.082479

	// Rider No. 5 Universal Service Charge (Supplement No. 97, 8th Rev. Page No. 93, eff. Jan 1, 2026).
	dlcUSCDollarsPerKWH = 0.015990

	// Rider No. 15A Energy Efficiency and Conservation (EE&C) Surcharge:
	// Phase IV through May 31, 2026: 0.19 ¢/kWh ($0.001900/kWh).
	// Phase V effective June 1, 2026 (Supplement No. 104, 7th Rev. Page No. 118): 0.17 ¢/kWh ($0.001700/kWh).
	dlcEECJanMay2026DollarsPerKWH = 0.001900
	dlcEECJun2026DollarsPerKWH    = 0.001700

	// Rider No. 22 Distribution System Improvement Charge (DSIC) quarterly factors,
	// applied to distribution + USC + EE&C before STAS:
	// Q1 2026 (Jan-Mar, Supp. 99): 0.49%; Q2 2026 (Apr-Jun, Supp. 101): 1.39%;
	// Q3 2026 (Jul-Sep, Supp. 107): 2.17%; Q4 2026 (Oct-Dec, Supp. 110, 30th Rev. Page No. 137): 3.36%.
	dlcDSICQ12026Factor = 1.0 + 0.0049
	dlcDSICQ22026Factor = 1.0 + 0.0139
	dlcDSICQ32026Factor = 1.0 + 0.0217
	dlcDSICQ42026Factor = 1.0 + 0.0336

	// Rider No. 10 State Tax Adjustment Surcharge (Supplement No. 98, 12th Rev. Page No. 112, eff. Jan 1, 2026):
	// Part 1 (-0.0964%) applies to distribution charges (including distribution riders and DSIC); Part 2 is 0.0000%.
	dlcSTASFactor = 1.0 - 0.000964

	// Appendix A Transmission Service Charge (TSC) for Rate RS (exempt from DSIC and STAS per Original Page No. 146):
	// Dec 1, 2025 - May 31, 2026: $0.027225/kWh; effective June 1, 2026 (Supplement No. 105, 9th Rev. Page No. 142): $0.031841/kWh.
	dlcTSCJanMay2026DollarsPerKWH = 0.027225
	dlcTSCJun2026DollarsPerKWH    = 0.031841

	// Non-supply volumetric totals per tariff window:
	// (Distribution + USC + EE&C) * DSICFactor * STASFactor + TSC.
	dlcNonSupplyQ12026DollarsPerKWH     = (dlcRSDistributionDollarsPerKWH+dlcUSCDollarsPerKWH+dlcEECJanMay2026DollarsPerKWH)*dlcDSICQ12026Factor*dlcSTASFactor + dlcTSCJanMay2026DollarsPerKWH
	dlcNonSupplyAprMay2026DollarsPerKWH = (dlcRSDistributionDollarsPerKWH+dlcUSCDollarsPerKWH+dlcEECJanMay2026DollarsPerKWH)*dlcDSICQ22026Factor*dlcSTASFactor + dlcTSCJanMay2026DollarsPerKWH
	dlcNonSupplyJun2026DollarsPerKWH    = (dlcRSDistributionDollarsPerKWH+dlcUSCDollarsPerKWH+dlcEECJun2026DollarsPerKWH)*dlcDSICQ22026Factor*dlcSTASFactor + dlcTSCJun2026DollarsPerKWH
	dlcNonSupplyQ32026DollarsPerKWH     = (dlcRSDistributionDollarsPerKWH+dlcUSCDollarsPerKWH+dlcEECJun2026DollarsPerKWH)*dlcDSICQ32026Factor*dlcSTASFactor + dlcTSCJun2026DollarsPerKWH
	dlcNonSupplyQ42026DollarsPerKWH     = (dlcRSDistributionDollarsPerKWH+dlcUSCDollarsPerKWH+dlcEECJun2026DollarsPerKWH)*dlcDSICQ42026Factor*dlcSTASFactor + dlcTSCJun2026DollarsPerKWH

	// Rider No. 8 Default Service Supply (DSS) & Time-of-Use Supply Rate Pilot tariffed rates:
	// Jan 1, 2026 - May 31, 2026 (Supplement No. 102 / 110, 13th Rev. Page No. 98A; 2.88 / 0.53 / 0.39 TOU factors):
	dlcSupplyJanMay2026RSDollarsPerKWH              = 0.110275
	dlcSupplyJanMay2026TOUPeakDollarsPerKWH         = 0.317565
	dlcSupplyJanMay2026TOUOffPeakDollarsPerKWH      = 0.058450
	dlcSupplyJanMay2026TOUSuperOffPeakDollarsPerKWH = 0.043015

	// Jun 1, 2026 - Nov 30, 2026 (Supplement No. 102 / 110, 13th Rev. Page No. 98A; 3.01 / 0.50 / 0.36 TOU factors):
	dlcSupplyJunNov2026RSDollarsPerKWH              = 0.109568
	dlcSupplyJunNov2026TOUPeakDollarsPerKWH         = 0.329685
	dlcSupplyJunNov2026TOUOffPeakDollarsPerKWH      = 0.054814
	dlcSupplyJunNov2026TOUSuperOffPeakDollarsPerKWH = 0.039487

	// Dec 1, 2026 - May 31, 2027, carried through 2027-12-31 until superseded
	// (Supplement No. 111, 14th Rev. Page No. 98A, Docket No. P-2024-3048592; 3.01 / 0.50 / 0.36 TOU factors):
	dlcSupplyDec2026RSDollarsPerKWH              = 0.113718
	dlcSupplyDec2026TOUPeakDollarsPerKWH         = 0.342182
	dlcSupplyDec2026TOUOffPeakDollarsPerKWH      = 0.056889
	dlcSupplyDec2026TOUSuperOffPeakDollarsPerKWH = 0.040975

	// Total volumetric rates ($/kWh) for each 2026 and 2027 tariff window:
	dlcRSQ12026DollarsPerKWH              = dlcSupplyJanMay2026RSDollarsPerKWH + dlcNonSupplyQ12026DollarsPerKWH
	dlcTOUPeakQ12026DollarsPerKWH         = dlcSupplyJanMay2026TOUPeakDollarsPerKWH + dlcNonSupplyQ12026DollarsPerKWH
	dlcTOUOffPeakQ12026DollarsPerKWH      = dlcSupplyJanMay2026TOUOffPeakDollarsPerKWH + dlcNonSupplyQ12026DollarsPerKWH
	dlcTOUSuperOffPeakQ12026DollarsPerKWH = dlcSupplyJanMay2026TOUSuperOffPeakDollarsPerKWH + dlcNonSupplyQ12026DollarsPerKWH

	dlcRSAprMay2026DollarsPerKWH              = dlcSupplyJanMay2026RSDollarsPerKWH + dlcNonSupplyAprMay2026DollarsPerKWH
	dlcTOUPeakAprMay2026DollarsPerKWH         = dlcSupplyJanMay2026TOUPeakDollarsPerKWH + dlcNonSupplyAprMay2026DollarsPerKWH
	dlcTOUOffPeakAprMay2026DollarsPerKWH      = dlcSupplyJanMay2026TOUOffPeakDollarsPerKWH + dlcNonSupplyAprMay2026DollarsPerKWH
	dlcTOUSuperOffPeakAprMay2026DollarsPerKWH = dlcSupplyJanMay2026TOUSuperOffPeakDollarsPerKWH + dlcNonSupplyAprMay2026DollarsPerKWH

	dlcRSJun2026DollarsPerKWH              = dlcSupplyJunNov2026RSDollarsPerKWH + dlcNonSupplyJun2026DollarsPerKWH
	dlcTOUPeakJun2026DollarsPerKWH         = dlcSupplyJunNov2026TOUPeakDollarsPerKWH + dlcNonSupplyJun2026DollarsPerKWH
	dlcTOUOffPeakJun2026DollarsPerKWH      = dlcSupplyJunNov2026TOUOffPeakDollarsPerKWH + dlcNonSupplyJun2026DollarsPerKWH
	dlcTOUSuperOffPeakJun2026DollarsPerKWH = dlcSupplyJunNov2026TOUSuperOffPeakDollarsPerKWH + dlcNonSupplyJun2026DollarsPerKWH

	dlcRSQ32026DollarsPerKWH              = dlcSupplyJunNov2026RSDollarsPerKWH + dlcNonSupplyQ32026DollarsPerKWH
	dlcTOUPeakQ32026DollarsPerKWH         = dlcSupplyJunNov2026TOUPeakDollarsPerKWH + dlcNonSupplyQ32026DollarsPerKWH
	dlcTOUOffPeakQ32026DollarsPerKWH      = dlcSupplyJunNov2026TOUOffPeakDollarsPerKWH + dlcNonSupplyQ32026DollarsPerKWH
	dlcTOUSuperOffPeakQ32026DollarsPerKWH = dlcSupplyJunNov2026TOUSuperOffPeakDollarsPerKWH + dlcNonSupplyQ32026DollarsPerKWH

	dlcRSOctNov2026DollarsPerKWH              = dlcSupplyJunNov2026RSDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
	dlcTOUPeakOctNov2026DollarsPerKWH         = dlcSupplyJunNov2026TOUPeakDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
	dlcTOUOffPeakOctNov2026DollarsPerKWH      = dlcSupplyJunNov2026TOUOffPeakDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
	dlcTOUSuperOffPeakOctNov2026DollarsPerKWH = dlcSupplyJunNov2026TOUSuperOffPeakDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH

	dlcRSDec2026DollarsPerKWH              = dlcSupplyDec2026RSDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
	dlcTOUPeakDec2026DollarsPerKWH         = dlcSupplyDec2026TOUPeakDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
	dlcTOUOffPeakDec2026DollarsPerKWH      = dlcSupplyDec2026TOUOffPeakDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
	dlcTOUSuperOffPeakDec2026DollarsPerKWH = dlcSupplyDec2026TOUSuperOffPeakDollarsPerKWH + dlcNonSupplyQ42026DollarsPerKWH
)

type dlcRateWindow struct {
	monthStart               time.Month
	monthEnd                 time.Month
	rsDollarsPerKWH          float64
	touPeakDollarsPerKWH     float64
	touOffPeakDollarsPerKWH  float64
	touSuperOffDollarsPerKWH float64
}

func dlcWindowsForYear(year int) []dlcRateWindow {
	if year == 2026 {
		return []dlcRateWindow{
			{
				monthStart:               time.January,
				monthEnd:                 time.March,
				rsDollarsPerKWH:          dlcRSQ12026DollarsPerKWH,
				touPeakDollarsPerKWH:     dlcTOUPeakQ12026DollarsPerKWH,
				touOffPeakDollarsPerKWH:  dlcTOUOffPeakQ12026DollarsPerKWH,
				touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakQ12026DollarsPerKWH,
			},
			{
				monthStart:               time.April,
				monthEnd:                 time.May,
				rsDollarsPerKWH:          dlcRSAprMay2026DollarsPerKWH,
				touPeakDollarsPerKWH:     dlcTOUPeakAprMay2026DollarsPerKWH,
				touOffPeakDollarsPerKWH:  dlcTOUOffPeakAprMay2026DollarsPerKWH,
				touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakAprMay2026DollarsPerKWH,
			},
			{
				monthStart:               time.June,
				monthEnd:                 time.June,
				rsDollarsPerKWH:          dlcRSJun2026DollarsPerKWH,
				touPeakDollarsPerKWH:     dlcTOUPeakJun2026DollarsPerKWH,
				touOffPeakDollarsPerKWH:  dlcTOUOffPeakJun2026DollarsPerKWH,
				touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakJun2026DollarsPerKWH,
			},
			{
				monthStart:               time.July,
				monthEnd:                 time.September,
				rsDollarsPerKWH:          dlcRSQ32026DollarsPerKWH,
				touPeakDollarsPerKWH:     dlcTOUPeakQ32026DollarsPerKWH,
				touOffPeakDollarsPerKWH:  dlcTOUOffPeakQ32026DollarsPerKWH,
				touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakQ32026DollarsPerKWH,
			},
			{
				monthStart:               time.October,
				monthEnd:                 time.November,
				rsDollarsPerKWH:          dlcRSOctNov2026DollarsPerKWH,
				touPeakDollarsPerKWH:     dlcTOUPeakOctNov2026DollarsPerKWH,
				touOffPeakDollarsPerKWH:  dlcTOUOffPeakOctNov2026DollarsPerKWH,
				touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakOctNov2026DollarsPerKWH,
			},
			{
				monthStart:               time.December,
				monthEnd:                 time.December,
				rsDollarsPerKWH:          dlcRSDec2026DollarsPerKWH,
				touPeakDollarsPerKWH:     dlcTOUPeakDec2026DollarsPerKWH,
				touOffPeakDollarsPerKWH:  dlcTOUOffPeakDec2026DollarsPerKWH,
				touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakDec2026DollarsPerKWH,
			},
		}
	}
	return []dlcRateWindow{
		{
			monthStart:               time.January,
			monthEnd:                 time.December,
			rsDollarsPerKWH:          dlcRSDec2026DollarsPerKWH,
			touPeakDollarsPerKWH:     dlcTOUPeakDec2026DollarsPerKWH,
			touOffPeakDollarsPerKWH:  dlcTOUOffPeakDec2026DollarsPerKWH,
			touSuperOffDollarsPerKWH: dlcTOUSuperOffPeakDec2026DollarsPerKWH,
		},
	}
}

// shiftDLCWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftDLCWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getDLCHolidays returns DLC's observed PJM holidays.
// The holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getDLCHolidays(year int) []string {
	holidays := []time.Time{
		shiftDLCWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftDLCWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftDLCWeekendHoliday(christmasDay(year)),
		shiftDLCWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// dlcPeriods generates the pricing periods for Duquesne Light Co. (DLC).
func dlcPeriods(plan string, _ types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod

	for _, year := range years {
		holidays := getDLCHolidays(year)

		for _, w := range dlcWindowsForYear(year) {
			switch plan {
			case "dlc_rs":
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         w.monthStart,
					MonthEnd:           w.monthEnd,
					OtherDollarsPerKWH: w.rsDollarsPerKWH,
					OtherDescription:   "DLC RS Base Rate",
				})

			case "dlc_tou":
				// Super Off-Peak Period:
				//   11:00 p.m. to 6:00 a.m. (23:00 to 06:00) daily, including weekends and PJM holidays.
				// Peak Period:
				//   3:00 p.m. to 9:00 p.m. (15:00 to 21:00) weekdays, excluding PJM holidays.
				// Off-Peak Period:
				//   All other hours.
				simplified = append(simplified,
					touSimplifiedPeriod{
						Year:          year,
						MonthStart:    w.monthStart,
						MonthEnd:      w.monthEnd,
						SpecificDates: holidays,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "Super Off-Peak",
								Hours: []types.UtilityHourPeriod{
									{HourStart: 0, HourEnd: 6},
									{HourStart: 23, HourEnd: 24},
								},
								DollarsPerKWH: w.touSuperOffDollarsPerKWH,
								Description:   "DLC TOU Holiday Super Off-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: w.touOffPeakDollarsPerKWH,
						OtherDescription:   "DLC TOU Holiday Off-Peak",
					},
					touSimplifiedPeriod{
						Year:             year,
						MonthStart:       w.monthStart,
						MonthEnd:         w.monthEnd,
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name: "Super Off-Peak",
								Hours: []types.UtilityHourPeriod{
									{HourStart: 0, HourEnd: 6},
									{HourStart: 23, HourEnd: 24},
								},
								DollarsPerKWH: w.touSuperOffDollarsPerKWH,
								Description:   "DLC TOU Super Off-Peak",
							},
							{
								Name: "On-Peak",
								Hours: []types.UtilityHourPeriod{
									{HourStart: 15, HourEnd: 21},
								},
								Weekday:       true,
								DollarsPerKWH: w.touPeakDollarsPerKWH,
								Description:   "DLC TOU Weekday Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: w.touOffPeakDollarsPerKWH,
						OtherDescription:   "DLC TOU Off-Peak",
					},
				)
			}
		}
	}

	return buildPeriods(etLocation, simplified)
}

// dlcUtilityInfo returns the metadata and rates for Duquesne Light Co.
func dlcUtilityInfo() types.UtilityProviderInfo {
	dlcOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "DLC net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "dlc",
		Name: "Duquesne Light Co. (DLC)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "dlc_rs",
				Name:    "Residential Service (RS)",
				Options: dlcOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dlcPeriods("dlc_rs", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "dlc_tou",
				Name:    "Time-of-Use Supply Rate Pilot",
				Options: dlcOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dlcPeriods("dlc_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
