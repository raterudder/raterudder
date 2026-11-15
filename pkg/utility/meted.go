package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Met-Ed Rate District (FirstEnergy Pennsylvania Electric Company) residential tariff constants.
// Source: FirstEnergy PA Electric Service Tariff (Electric Pa. P.U.C. No. 1) & Met-Ed Price to Compare
// documents on firstenergycorp.com.
const (
	// Non-supply volumetric charges for Rate Schedule RS (Distribution + non-bypassable riders):
	// Fixed monthly charges (Customer Charge $11.25/mo, SMT Rider $0.06/mo) are excluded from per-kWh rates.
	metedRSBaseDistDollarsPerKWH = 0.05138 // Rate RS Base Distribution (Supp. 14, eff. Jan 1, 2025)
	metedUSCDollarsPerKWH        = 0.01283 // Universal Service Cost Rider (Supp. 31, eff. Jan 1, 2026 - Dec 31, 2026)
	metedEECCDollarsPerKWH       = 0.00215 // EE&C-C Phase V Rider (Supp. 37, eff. Jun 1, 2026 - May 31, 2027)
	metedDSSDollarsPerKWH        = 0.00546 // Default Service Support Rider (Supp. 38, eff. Jun 1, 2026)
	metedSPVRCDollarsPerKWH      = 0.00000 // Solar Photovoltaic Requirements Charge Rider (Supp. 21)
	metedLEDCDollarsPerKWH       = 0.00013 // LED Conversion Charge Rider (Supp. 34, eff. Apr 1, 2026 - Mar 31, 2027)

	// Distribution System Improvement Charge (DSIC) Rider (Supp. 40, eff. Oct 1, 2026 - Dec 31, 2026): 2.02%
	metedDSICFactor = 1.0202
	// State Tax Adjustment Surcharge (STAS) reflected on Met-Ed PTC sheet (Supp. 36 + 0.02% STAS)
	metedSTASFactor = 1.0002

	// Residential Default Service Price to Compare (PTC_Default):
	// Dec 1, 2025 - May 31, 2026 published PTC (includes STAS)
	metedPTCWinter2025DollarsPerKWH = 0.12968
	// Jun 1, 2026 - Nov 30, 2026 published PTC (Supp. 36 base $0.13951/kWh + 0.02% STAS = $0.13954/kWh)
	metedPTCSummer2026DollarsPerKWH = 0.13954
	// Dec 1, 2026 - May 31, 2027 base PTC ($0.14422/kWh) before STAS
	metedPTCWinter2026BaseDollarsPerKWH = 0.14422

	// Residential Time-of-Use Default Service Rider (TOU_Default, Supp. 20, eff. Jun 1, 2025)
	// Multipliers applied to PTC_Default:
	metedTOUOnPeakMultiplier       = 1.7060 // 2 PM - 9 PM Mon-Fri
	metedTOUOffPeakMultiplier      = 0.8397 // All other hours
	metedTOUSuperOffPeakMultiplier = 0.6215 // 11 PM - 6 AM every day
)

type metedSeasonRate struct {
	monthStart          time.Month
	monthEnd            time.Month
	supplyDollarsPerKWH float64
}

// metedPeriods generates the pricing periods for Met-Ed (FirstEnergy Pennsylvania).
func metedPeriods(plan string, _ types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	baseNonSupply := metedRSBaseDistDollarsPerKWH +
		metedUSCDollarsPerKWH +
		metedEECCDollarsPerKWH +
		metedDSSDollarsPerKWH +
		metedSPVRCDollarsPerKWH +
		metedLEDCDollarsPerKWH
	nonSupplyDollarsPerKWH := baseNonSupply * metedDSICFactor * metedSTASFactor
	winter2026PTC := metedPTCWinter2026BaseDollarsPerKWH * metedSTASFactor

	var simplified []touSimplifiedPeriod

	for _, year := range years {
		janMaySupply := metedPTCWinter2025DollarsPerKWH
		if year >= 2027 {
			janMaySupply = winter2026PTC
		}

		seasons := []metedSeasonRate{
			{
				monthStart:          time.January,
				monthEnd:            time.May,
				supplyDollarsPerKWH: janMaySupply,
			},
			{
				monthStart:          time.June,
				monthEnd:            time.November,
				supplyDollarsPerKWH: metedPTCSummer2026DollarsPerKWH,
			},
			{
				monthStart:          time.December,
				monthEnd:            time.December,
				supplyDollarsPerKWH: winter2026PTC,
			},
		}

		for _, season := range seasons {
			switch plan {
			case "meted_rs":
				simplified = append(simplified, touSimplifiedPeriod{
					Year:       year,
					MonthStart: season.monthStart,
					MonthEnd:   season.monthEnd,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: nonSupplyDollarsPerKWH + season.supplyDollarsPerKWH,
							Description:   "Flat residential service rate (Distribution + Riders + Default PTC Supply)",
							Name:          "Flat Rate RS",
						},
					},
				})
			case "meted_tou":
				onPeakDollarsPerKWH := nonSupplyDollarsPerKWH + (season.supplyDollarsPerKWH * metedTOUOnPeakMultiplier)
				offPeakDollarsPerKWH := nonSupplyDollarsPerKWH + (season.supplyDollarsPerKWH * metedTOUOffPeakMultiplier)
				superOffPeakDollarsPerKWH := nonSupplyDollarsPerKWH + (season.supplyDollarsPerKWH * metedTOUSuperOffPeakMultiplier)

				simplified = append(simplified, touSimplifiedPeriod{
					Year:       year,
					MonthStart: season.monthStart,
					MonthEnd:   season.monthEnd,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours: []types.UtilityHourPeriod{
								{HourStart: 0, HourEnd: 6},
								{HourStart: 23, HourEnd: 24},
							},
							DollarsPerKWH: superOffPeakDollarsPerKWH,
							Description:   "Super Off-Peak (11:00 PM - 6:00 AM daily)",
							Name:          "Super Off-Peak",
						},
						{
							Hours: []types.UtilityHourPeriod{
								{HourStart: 14, HourEnd: 21},
							},
							Weekday:       true,
							DollarsPerKWH: onPeakDollarsPerKWH,
							Description:   "On-Peak (2:00 PM - 9:00 PM Mon-Fri)",
							Name:          "On-Peak",
						},
					},
					OtherDollarsPerKWH: offPeakDollarsPerKWH,
					OtherDescription:   "Off-Peak (All other hours)",
					OtherName:          "Off-Peak",
				})
			default:
				panic("unknown meted plan: " + plan)
			}
		}
	}

	return buildPeriods(etLocation, simplified)
}

// metedUtilityInfo returns the metadata and rates for Met-Ed (FirstEnergy Pennsylvania).
func metedUtilityInfo() types.UtilityProviderInfo {
	metedOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Met-Ed net metering tracks energy exports as kWh 1:1 full retail credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "meted",
		Name: "Metropolitan Edison (Met-Ed)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "meted_rs",
				Name:    "Residential Service (Rate RS)",
				Options: metedOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return metedPeriods("meted_rs", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "meted_tou",
				Name:    "Residential Time-of-Use (TOU)",
				Options: metedOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return metedPeriods("meted_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
