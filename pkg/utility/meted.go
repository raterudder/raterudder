package utility

// Tariff Audit Metadata
// - Provider(s): meted (Metropolitan Edison (Met-Ed))
// - Official Sources:
//   - https://www.firstenergycorp.com/customer_choice/pennsylvania/pennsylvania_tariffs.html
//   - https://www.firstenergycorp.com/content/dam/customer/Customer%20Choice/Files/PA/tariffs/fe-pa-retail-tariff.pdf (FirstEnergy Pennsylvania Electric Company Tariff Electric Pa. P.U.C. No. 1 consolidated through Supplement No. 40: Rate RS Base Distribution Supp. 14; TOU Default Service Rider Supp. 20; SPVRC Rider Supp. 21; USC Rider Supp. 31, p. 185; STAS Rider Supp. 33, p. 182; LEDC Rider Supp. 34, p. 245; Summer 2026 PTC Supp. 36, p. 203; EE&C-C Phase V Rider Supp. 37, p. 194; DSS Rider Supp. 38, p. 213; Q4 2026 DSIC Rider Supp. 40, p. 239)
//   - https://www.firstenergycorp.com/content/dam/customer/Customer%20Choice/Files/PA/tariffs/Met-Ed-Rate-District-PTC-HP-Illustrative.pdf (Met-Ed Rate District PTC & HP Illustrative Calculations, Page 2, Line 15: $0.14422/kWh base Residential PTC_Default eff. Dec 1, 2026 - May 31, 2027)
//   - https://www.papowerswitch.com/umbraco/api/shopapi/ZipSearch?serviceType=residential&zipcode=19605 (PA PUC PAPowerSwitch API for ZIP 19605: Met-Ed Rate RS base PTC rates $0.12965, $0.13951, and $0.14422/kWh)
// - Effective Window: 2026-01-01 through 2026-12-31 (USC Supp. 31 & Q4 2026 DSIC Supp. 40 end 2026-12-31; LEDC Supp. 34 through 2027-03-31; Winter PTC & EE&C-C Supp. 37 through 2027-05-31; carried through 2027-12-31 until superseded)
// - Revision Cadence: Quarterly (Jan/Apr/Jul/Oct 1 DSIC) & Semi-Annual (Jun 1 & Dec 1 PTC_Default; annual Jan 1 USC, Apr 1 LEDC, Jun 1 EE&C-C/DSS)
// - Next Audit Due: 2026-12-22 (Check January 1, 2027 annual USC and Q1 2027 DSIC tariff supplements before 2026-12-31 expiration)
// - Last Verified: 2026-11-21
// - Modeling Notes: Applies -0.02% STAS credit (0.9998 per Supp. 33) to non-supply riders (after 2.02% Q4 2026 DSIC per Supp. 40) and base PTC_Default rates ($0.12965 Jan-May 2026, $0.13951 Jun-Nov 2026, $0.14422 Dec 2026+); retains verified active riders as pre-Oct 2026 fallbacks where consolidated Tariff No. 1 omits superseded sheets and carries Q4 2026 riders + Winter 2026-2027 PTC into 2027 until replacement filings arrive; fixed monthly charges ($11.25/mo Customer Charge, $0.06/mo SMT Rider) excluded; 1:1 kWh full retail net metering.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Met-Ed Rate District (FirstEnergy Pennsylvania Electric Company) residential tariff constants.
// Source: FirstEnergy PA Electric Service Tariff (Electric Pa. P.U.C. No. 1) & Met-Ed Price to Compare
// documents on firstenergycorp.com and papowerswitch.com.
const (
	// Non-supply volumetric charges for Rate Schedule RS (Distribution + non-bypassable riders):
	// Fixed monthly charges (Customer Charge $11.25/mo, SMT Rider $0.06/mo) are excluded from per-kWh rates.
	metedRSBaseDistDollarsPerKWH = 0.05138 // Rate RS Base Distribution (Supp. 14, eff. Jan 1, 2025)
	metedUSCDollarsPerKWH        = 0.01283 // Universal Service Cost Rider (Supp. 31, p. 185, eff. Jan 1, 2026 - Dec 31, 2026)
	metedEECCDollarsPerKWH       = 0.00215 // EE&C-C Phase V Rider (Supp. 37, p. 194, eff. Jun 1, 2026 - May 31, 2027)
	metedDSSDollarsPerKWH        = 0.00546 // Default Service Support Rider (Supp. 38, p. 213, eff. Jun 1, 2026)
	metedSPVRCDollarsPerKWH      = 0.00000 // Solar Photovoltaic Requirements Charge Rider (Supp. 21)
	metedLEDCDollarsPerKWH       = 0.00013 // LED Conversion Charge Rider (Supp. 34, p. 245, eff. Apr 1, 2026 - Mar 31, 2027)

	// Distribution System Improvement Charge (DSIC) Rider (Supp. 40, p. 239, eff. Oct 1, 2026 - Dec 31, 2026): 2.02%
	metedDSICFactor = 1.0202
	// State Tax Adjustment Surcharge (STAS) credit (Supp. 33, p. 182): -0.02%
	metedSTASFactor = 0.9998

	// Residential Default Service Price to Compare (PTC_Default) base rates before STAS:
	// Dec 1, 2025 - May 31, 2026 base PTC ($0.12965/kWh per PA PUC PAPowerSwitch API)
	metedPTCWinter2025BaseDollarsPerKWH = 0.12965
	// Jun 1, 2026 - Nov 30, 2026 base PTC (Supp. 36, p. 203 & PA PUC PAPowerSwitch API: $0.13951/kWh)
	metedPTCSummer2026BaseDollarsPerKWH = 0.13951
	// Dec 1, 2026 - May 31, 2027 base PTC (Met-Ed PTC Illustrative Sheet, p. 2, line 15 & PA PUC PAPowerSwitch API: $0.14422/kWh)
	metedPTCWinter2026BaseDollarsPerKWH = 0.14422

	// Residential Time-of-Use Default Service Rider (TOU_Default, Supp. 20, eff. Jun 1, 2025)
	// Multipliers applied to PTC_Default:
	metedTOUOnPeakMultiplier       = 1.7060 // 2 PM - 9 PM Mon-Fri
	metedTOUOffPeakMultiplier      = 0.8397 // All other hours
	metedTOUSuperOffPeakMultiplier = 0.6215 // 11 PM - 6 AM every day
)

type metedEffectiveWindow struct {
	monthStart             time.Month
	monthEnd               time.Month
	nonSupplyDollarsPerKWH float64
	supplyDollarsPerKWH    float64
}

func metedNonSupplyDollarsPerKWH(uscDollarsPerKWH, eeccDollarsPerKWH, dssDollarsPerKWH, ledcDollarsPerKWH, dsicFactor float64) float64 {
	baseNonSupply := metedRSBaseDistDollarsPerKWH +
		uscDollarsPerKWH +
		eeccDollarsPerKWH +
		dssDollarsPerKWH +
		metedSPVRCDollarsPerKWH +
		ledcDollarsPerKWH
	return baseNonSupply * dsicFactor * metedSTASFactor
}

// metedEffectiveWindows returns bounded effective supplement and PTC windows for a given year.
// Consolidated Tariff No. 1 omits superseded pre-October 2026 rider sheets, so pre-October 2026
// windows retain the verified active riders as fallbacks instead of zeroing prior charges.
// USC (Supp. 31) and Q4 2026 DSIC (Supp. 40) end on 2026-12-31, LEDC (Supp. 34) ends on 2027-03-31,
// and Winter 2026-2027 PTC + EE&C-C (Supp. 37) end on 2027-05-31; 2027 windows carry Q4 2026 riders
// and Winter 2026-2027 PTC forward until replacement 2027 filings arrive.
func metedEffectiveWindows(year int) []metedEffectiveWindow {
	winter2025PTCDollarsPerKWH := metedPTCWinter2025BaseDollarsPerKWH * metedSTASFactor
	summer2026PTCDollarsPerKWH := metedPTCSummer2026BaseDollarsPerKWH * metedSTASFactor
	winter2026PTCDollarsPerKWH := metedPTCWinter2026BaseDollarsPerKWH * metedSTASFactor

	if year < 2026 {
		fallbackNonSupply := metedNonSupplyDollarsPerKWH(
			metedUSCDollarsPerKWH,
			metedEECCDollarsPerKWH,
			metedDSSDollarsPerKWH,
			metedLEDCDollarsPerKWH,
			metedDSICFactor,
		)
		return []metedEffectiveWindow{
			{
				monthStart:             time.January,
				monthEnd:               time.December,
				nonSupplyDollarsPerKWH: fallbackNonSupply,
				supplyDollarsPerKWH:    winter2025PTCDollarsPerKWH,
			},
		}
	}

	if year == 2026 {
		return []metedEffectiveWindow{
			{
				// Jan 1 - Mar 31, 2026: USC Supp. 31 active; pre-Apr LEDC, pre-Jun EE&C-C/DSS, and Q1 DSIC fallback to verified sheets; Winter 2025-2026 PTC.
				monthStart: time.January,
				monthEnd:   time.March,
				nonSupplyDollarsPerKWH: metedNonSupplyDollarsPerKWH(
					metedUSCDollarsPerKWH,
					metedEECCDollarsPerKWH,
					metedDSSDollarsPerKWH,
					metedLEDCDollarsPerKWH,
					metedDSICFactor,
				),
				supplyDollarsPerKWH: winter2025PTCDollarsPerKWH,
			},
			{
				// Apr 1 - May 31, 2026: LEDC Supp. 34 effective Apr 1, 2026; pre-Jun EE&C-C/DSS and Q2 DSIC fallback to verified sheets; Winter 2025-2026 PTC.
				monthStart: time.April,
				monthEnd:   time.May,
				nonSupplyDollarsPerKWH: metedNonSupplyDollarsPerKWH(
					metedUSCDollarsPerKWH,
					metedEECCDollarsPerKWH,
					metedDSSDollarsPerKWH,
					metedLEDCDollarsPerKWH,
					metedDSICFactor,
				),
				supplyDollarsPerKWH: winter2025PTCDollarsPerKWH,
			},
			{
				// Jun 1 - Sep 30, 2026: Summer 2026 PTC (Supp. 36), EE&C-C Phase V (Supp. 37), and DSS (Supp. 38) effective Jun 1, 2026; Q2/Q3 DSIC fallback to Q4 Supp. 40.
				monthStart: time.June,
				monthEnd:   time.September,
				nonSupplyDollarsPerKWH: metedNonSupplyDollarsPerKWH(
					metedUSCDollarsPerKWH,
					metedEECCDollarsPerKWH,
					metedDSSDollarsPerKWH,
					metedLEDCDollarsPerKWH,
					metedDSICFactor,
				),
				supplyDollarsPerKWH: summer2026PTCDollarsPerKWH,
			},
			{
				// Oct 1 - Nov 30, 2026: Q4 2026 DSIC (Supp. 40, 2.02%) effective Oct 1 - Dec 31, 2026 alongside verified USC, LEDC, EE&C-C, DSS, and Summer 2026 PTC.
				monthStart: time.October,
				monthEnd:   time.November,
				nonSupplyDollarsPerKWH: metedNonSupplyDollarsPerKWH(
					metedUSCDollarsPerKWH,
					metedEECCDollarsPerKWH,
					metedDSSDollarsPerKWH,
					metedLEDCDollarsPerKWH,
					metedDSICFactor,
				),
				supplyDollarsPerKWH: summer2026PTCDollarsPerKWH,
			},
			{
				// Dec 1 - Dec 31, 2026: Winter 2026-2027 PTC ($0.14422 base * 0.9998 STAS) effective Dec 1, 2026 alongside verified Q4 2026 riders through Dec 31, 2026.
				monthStart: time.December,
				monthEnd:   time.December,
				nonSupplyDollarsPerKWH: metedNonSupplyDollarsPerKWH(
					metedUSCDollarsPerKWH,
					metedEECCDollarsPerKWH,
					metedDSSDollarsPerKWH,
					metedLEDCDollarsPerKWH,
					metedDSICFactor,
				),
				supplyDollarsPerKWH: winter2026PTCDollarsPerKWH,
			},
		}
	}

	// 2027+: Carry Q4 2026 riders and Winter 2026-2027 PTC across supplement expiration boundaries
	// until January/April/June 2027 tariff supplements are filed.
	q4CarryNonSupply := metedNonSupplyDollarsPerKWH(
		metedUSCDollarsPerKWH,
		metedEECCDollarsPerKWH,
		metedDSSDollarsPerKWH,
		metedLEDCDollarsPerKWH,
		metedDSICFactor,
	)
	return []metedEffectiveWindow{
		{
			// Jan 1 - Mar 31, 2027: LEDC Supp. 34, EE&C-C Supp. 37, DSS Supp. 38, and Winter 2026-2027 PTC active; Q4 2026 USC & DSIC carried forward.
			monthStart:             time.January,
			monthEnd:               time.March,
			nonSupplyDollarsPerKWH: q4CarryNonSupply,
			supplyDollarsPerKWH:    winter2026PTCDollarsPerKWH,
		},
		{
			// Apr 1 - May 31, 2027: EE&C-C Supp. 37, DSS Supp. 38, and Winter 2026-2027 PTC active through May 31, 2027; Q4 2026 USC, DSIC & LEDC carried forward.
			monthStart:             time.April,
			monthEnd:               time.May,
			nonSupplyDollarsPerKWH: q4CarryNonSupply,
			supplyDollarsPerKWH:    winter2026PTCDollarsPerKWH,
		},
		{
			// Jun 1 - Dec 31, 2027: DSS Supp. 38 active; Q4 2026 riders and Winter 2026-2027 PTC carried forward until superseded.
			monthStart:             time.June,
			monthEnd:               time.December,
			nonSupplyDollarsPerKWH: q4CarryNonSupply,
			supplyDollarsPerKWH:    winter2026PTCDollarsPerKWH,
		},
	}
}

// metedPeriods generates the pricing periods for Met-Ed (FirstEnergy Pennsylvania).
func metedPeriods(plan string, _ types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod

	for _, year := range years {
		for _, window := range metedEffectiveWindows(year) {
			switch plan {
			case "meted_rs":
				simplified = append(simplified, touSimplifiedPeriod{
					Year:       year,
					MonthStart: window.monthStart,
					MonthEnd:   window.monthEnd,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: window.nonSupplyDollarsPerKWH + window.supplyDollarsPerKWH,
							Description:   "Flat residential service rate (Distribution + Riders + Default PTC Supply)",
							Name:          "Flat Rate RS",
						},
					},
				})
			case "meted_tou":
				onPeakDollarsPerKWH := window.nonSupplyDollarsPerKWH + (window.supplyDollarsPerKWH * metedTOUOnPeakMultiplier)
				offPeakDollarsPerKWH := window.nonSupplyDollarsPerKWH + (window.supplyDollarsPerKWH * metedTOUOffPeakMultiplier)
				superOffPeakDollarsPerKWH := window.nonSupplyDollarsPerKWH + (window.supplyDollarsPerKWH * metedTOUSuperOffPeakMultiplier)

				simplified = append(simplified, touSimplifiedPeriod{
					Year:       year,
					MonthStart: window.monthStart,
					MonthEnd:   window.monthEnd,
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
