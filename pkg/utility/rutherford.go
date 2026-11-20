package utility

// Tariff Audit Metadata
// - Provider(s): rutherford_electric (Rutherford Electric)
// - Official Sources:
//   - https://www.remc.com/residential/residential-rate-schedule
//   - https://rutherfordemc.s3.amazonaws.com/files/1/2023/Rate%20Schedule%20E.pdf
//   - https://rutherfordemc.s3.amazonaws.com/files/1/2024/Rate%20Schedule%20WPTA_2021.pdf
//   - Rutherford EMC Schedule E / E-TOD (Time of Day Residential Service, eff. July 1, 2023) & Rider WPTA
// - Effective Window: 2023-07-01 through until superseded (modeled for 2026-01-01 through 2027-12-31)
// - Revision Cadence: Annual / Quarterly WPTA Review (Base tariff unchanged since July 2023; WPTA reviewed Mar/Jun/Sep/Jan)
// - Next Audit Due: 2027-03-15 (Check Rutherford EMC annual/Q1 WPTA review and Residential Rate Schedule page)
// - Last Verified: 2026-11-19
// - Modeling Notes: Models Schedule E-TOD volumetric energy charges ($0.31443/kWh On-Peak, $0.05500/kWh Super Off-Peak, $0.10481/kWh Off-Peak) and solar generation credits ($0.09400/kWh On-Peak, $0.02375/kWh Off-Peak); excludes fixed Basic Facilities Charge ($24.00/mo).

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	rutherfordTODOnPeakDollarsPerKWH           = 0.31443
	rutherfordTODOffPeakDollarsPerKWH          = 0.10481
	rutherfordTODSuperOffPeakDollarsPerKWH     = 0.05500
	rutherfordTODOnPeakGenCreditDollarsPerKWH  = 0.09400
	rutherfordTODOffPeakGenCreditDollarsPerKWH = 0.02375
)

// rutherfordElectricPeriods generates Schedule E-TOD pricing periods for Rutherford Electric.
func rutherfordElectricPeriods(years []int) []types.UtilityFeesPeriod {
	superOffPeakHours := []types.UtilityHourPeriod{
		{HourStart: 0, HourEnd: 5},
		{HourStart: 22, HourEnd: 24},
	}

	simplified := make([]touSimplifiedPeriod, 0, len(years)*3)
	for _, year := range years {
		simplified = append(simplified,
			// May - September
			touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.May,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 13, HourEnd: 19},
						},
						Weekday:                       true,
						DollarsPerKWH:                 rutherfordTODOnPeakDollarsPerKWH,
						GenerationCreditDollarsPerKWH: rutherfordTODOnPeakGenCreditDollarsPerKWH,
						Description:                   "May - September On-Peak",
					},
					{
						Name:                          "Super Off-Peak",
						Hours:                         superOffPeakHours,
						DollarsPerKWH:                 rutherfordTODSuperOffPeakDollarsPerKWH,
						GenerationCreditDollarsPerKWH: rutherfordTODOffPeakGenCreditDollarsPerKWH,
						Description:                   "May - September Super Off-Peak",
					},
				},
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 rutherfordTODOffPeakDollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: rutherfordTODOffPeakGenCreditDollarsPerKWH,
				OtherDescription:                   "May - September Off-Peak",
				SeparateGenerationCredit:           true,
			},
			// October and April
			touSimplifiedPeriod{
				Year:   year,
				Months: []time.Month{time.October, time.April},
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 9},
							{HourStart: 13, HourEnd: 19},
						},
						Weekday:                       true,
						DollarsPerKWH:                 rutherfordTODOnPeakDollarsPerKWH,
						GenerationCreditDollarsPerKWH: rutherfordTODOnPeakGenCreditDollarsPerKWH,
						Description:                   "October and April On-Peak",
					},
					{
						Name:                          "Super Off-Peak",
						Hours:                         superOffPeakHours,
						DollarsPerKWH:                 rutherfordTODSuperOffPeakDollarsPerKWH,
						GenerationCreditDollarsPerKWH: rutherfordTODOffPeakGenCreditDollarsPerKWH,
						Description:                   "October and April Super Off-Peak",
					},
				},
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 rutherfordTODOffPeakDollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: rutherfordTODOffPeakGenCreditDollarsPerKWH,
				OtherDescription:                   "October and April Off-Peak",
				SeparateGenerationCredit:           true,
			},
			// November - March
			touSimplifiedPeriod{
				Year:       year,
				MonthStart: time.November,
				MonthEnd:   time.March,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "On-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 9},
						},
						Weekday:                       true,
						DollarsPerKWH:                 rutherfordTODOnPeakDollarsPerKWH,
						GenerationCreditDollarsPerKWH: rutherfordTODOnPeakGenCreditDollarsPerKWH,
						Description:                   "November - March On-Peak",
					},
					{
						Name:                          "Super Off-Peak",
						Hours:                         superOffPeakHours,
						DollarsPerKWH:                 rutherfordTODSuperOffPeakDollarsPerKWH,
						GenerationCreditDollarsPerKWH: rutherfordTODOffPeakGenCreditDollarsPerKWH,
						Description:                   "November - March Super Off-Peak",
					},
				},
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 rutherfordTODOffPeakDollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: rutherfordTODOffPeakGenCreditDollarsPerKWH,
				OtherDescription:                   "November - March Off-Peak",
				SeparateGenerationCredit:           true,
			},
		)
	}

	return buildPeriods(etLocation, simplified)
}

// rutherfordElectricUtilityInfo returns metadata and rates for Rutherford Electric.
func rutherfordElectricUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "rutherford_electric",
		Name: "Rutherford Electric",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "rutherford_electric_tod",
				Name:    "Time of Day Service",
				GetFees: getStaticGetFees(rutherfordElectricPeriods([]int{2026, 2027})),
			},
		},
	}
}
