package utility

// Tariff Audit Metadata
// - Provider(s): coned (Consolidated Edison (Con Edison))
// - Official Sources:
//   - https://www.coned.com/en/rates-tariffs/rates/electric-rates-schedule/electric-psc-10 (P.S.C. No. 10 - Electricity: Leaves 244-253.6 Rider R Net Metering, Leaf 330.1 MSC-Capacity, Leaf 335 MFC, and Leaves 388, 389.1-389.3 SC 1 Rates I, III, and IV)
//   - https://documents.dps.ny.gov/public/MatterManagement/CaseMaster.aspx?MatterCaseNo=25-E-0072 (NYPSC Case 25-E-0072 Order Approving Three-Year Electric Rate Plan for 2026-2028)
//   - https://documents.dps.ny.gov/public/Common/ViewDoc.aspx?DocRefId={B0A8B29B-0000-C458-A21C-75C88C4AD427} (NYPSC Case 25-E-0072 Exhibit 1257: SC 1 Rate Year 1 eff. 2026-02-01 & Rate Year 2 eff. 2027-01-01 Delivery Rates)
//   - https://www.coned.com/en/accounts-billing/your-bill/rate-calculators/market-supply-charge (Con Edison Market Supply Charge Calculator & LDS_APPND.CSV Zonal Supply Rates for Zones H, I, and J)
//   - https://www.coned.com/en/accounts-billing/your-bill/time-of-use (Con Edison Time-of-Use Rate Guide for SC 1 Rate III)
//   - https://www.coned.com/-/media/files/coned/documents/rates/electric/psc-10/supply/msc-cap/msccap-202607.pdf (MSC-Capacity Statement No. 135: SC 1 Rate III Jun-Sep Mon-Fri 14:00-18:00 Super-Peak Capacity)
//   - https://www.coned.com/-/media/files/coned/documents/rates/electric/psc-10/delivery/monthly-adjustment-clause/mac-202611.pdf (MAC Statement No. 178 & General Rule 25/26 Surcharge Statements: MFC Stmt 182, CESS Stmt 12, SBC Stmt 18, RDM Stmt 38, DLM Stmt 11, EVS Stmt 7, AMP Stmt 3, VDER-CR Stmt 12)
//   - https://www.coned.com/-/media/files/coned/documents/rates/electric/psc-10/other/customer-benefit-contribution/cbc-202603.pdf (CBC Statement No. 8: Fixed $/kW DC/month solar CBC & $0.00/kW residential energy storage CBC)
// - Effective Window: 2026-01-01 through 2027-12-31 (Jan 2026 baseline delivery through 2026-01-31; Case 25-E-0072 Rate Year 1 eff. 2026-02-01; Rate Year 2 eff. 2027-01-01; Nov 2026 zonal MSC & General Rule 25/26 riders carried forward until superseded)
// - Revision Cadence: Monthly (MSC, MAC, and MFC Statements) & Annual (Jan 1 Case 25-E-0072 Multi-Year Delivery Steps; Jun-Sep Summer MSC-Capacity)
// - Next Audit Due: 2026-12-15 (Check December 2026 / January 2027 MSC, MAC, MFC, and RDM statements under P.S.C. No. 10)
// - Last Verified: 2026-11-26
// - Modeling Notes: Models P.S.C. No. 10 SC 1 Rate I (Tier 1 <= 250 kWh/month), Rate III (Voluntary Time-of-Use with Jun-Sep Mon-Fri 14:00-18:00 Super-Peak MSC-Capacity), and Rate IV (Optional Demand-Based 001IPP TOU supply + volumetric surcharges, omitting $/kW Billable Demand and $/kW SBC charges) across NYISO Zones J (nyc), I (westchester), and H (upper_westchester). SC 1 Rate II (Leaf 389, Voluntary Time-of-Day) was ignored because it has been closed to new applicants for more than 10 years (since March 1, 2014). Excludes fixed monthly Customer Charges ($20.00/$21.00/$29.00/$31.00/month), fixed monthly solar CBC ($1.41/kW DC/month per CBC Statement No. 8), and de minimis <1% riders (CESD $0.000201/kWh & SDR -$0.000112/kWh). Applies 1:1 retail net metering under Rider R (SeparateGenerationCredit: false, GridAdditional: false).

import (
	"fmt"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// P.S.C. No. 10 Leaves 388 and 389.1 (Case 22-E-0064 Rate Year 3 baseline effective through January 31, 2026).
	conedJan2026Rate1Tier1DeliveryDollarsPerKWH        = 0.16107
	conedJan2026Rate3SummerOnPeakDeliveryDollarsPerKWH = 0.3523
	conedJan2026Rate3WinterOnPeakDeliveryDollarsPerKWH = 0.1305
	conedJan2026Rate3OffPeakDeliveryDollarsPerKWH      = 0.0249

	// P.S.C. No. 10 Leaves 388, 389.1-389.2 & NYPSC Case 25-E-0072 Exhibit 1257 Rate Year 1 (effective February 1, 2026).
	conedRY1Rate1Tier1DeliveryDollarsPerKWH        = 0.16402
	conedRY1Rate3SummerOnPeakDeliveryDollarsPerKWH = 0.2786
	conedRY1Rate3WinterOnPeakDeliveryDollarsPerKWH = 0.1711
	conedRY1Rate3OffPeakDeliveryDollarsPerKWH      = 0.0522

	// NYPSC Case 25-E-0072 Exhibit 1257 Rate Year 2 (effective January 1, 2027).
	conedRY2Rate1Tier1DeliveryDollarsPerKWH        = 0.17246
	conedRY2Rate3SummerOnPeakDeliveryDollarsPerKWH = 0.2930
	conedRY2Rate3WinterOnPeakDeliveryDollarsPerKWH = 0.1800
	conedRY2Rate3OffPeakDeliveryDollarsPerKWH      = 0.0550

	// MSC - Capacity Statement No. 135 (effective July 1, 2026 for SC 1 Rate III Jun-Sep Mon-Fri 14:00-18:00).
	conedRate3SummerSuperPeakCapNYCDollarsPerKWH         = 0.79957
	conedRate3SummerSuperPeakCapWestchesterDollarsPerKWH = 0.32504

	// General Rule 25 Supply Riders:
	// - Merchant Function Charge (MFC Statement No. 182: $0.005155/kWh in 2026; Leaf 335 RY2: $0.004672/kWh eff. Jan 1, 2027)
	// - Clean Energy Standard Supply Surcharge (CESS Statement No. 12: $0.005041/kWh)
	// - Adjustment Factors - MSC (eff. 11/12/2026: -$0.016841 MSC I + $0.013096 MSC II = -$0.003745/kWh NYC;
	//   +$0.001626 MSC I + $0.013096 MSC II = +$0.014722/kWh Westchester)
	conedMFC2026DollarsPerKWH                  = 0.005155
	conedMFC2027DollarsPerKWH                  = 0.004672
	conedCESSDollarsPerKWH                     = 0.005041
	conedMSCAdjustmentNYCDollarsPerKWH         = -0.003745
	conedMSCAdjustmentWestchesterDollarsPerKWH = 0.014722

	// General Rule 26 Delivery Riders:
	// - SC 1 Rates I and III: MAC ($0.011340, Stmt 178) + Adj Factor-MAC ($0.006317, eff. 11/12/2026) +
	//   SBC ($0.007800, Stmt 18) + RDM (-$0.008150, Stmt 38) + DLM ($0.002300, Stmt 11) +
	//   EVS ($0.002000, Stmt 7) + AMP ($0.001100, Stmt 3) + VDER-CR ($0.001500 NYC / $0.001600 Westchester, Stmt 12)
	//   = $0.024207/kWh (NYC) and $0.024307/kWh (Westchester).
	// - SC 1 Rate IV: volumetric General Rule 26 surcharges = $0.01841/kWh (NYC) and $0.01851/kWh (Westchester)
	//   (Company EE/BE SBC $0.85/kW and Billable Demand $/kW charges are demand-billed and excluded).
	conedRule26Rates1And3NYCDollarsPerKWH         = 0.024207
	conedRule26Rates1And3WestchesterDollarsPerKWH = 0.024307
	conedRule26Rate4NYCDollarsPerKWH              = 0.01841
	conedRule26Rate4WestchesterDollarsPerKWH      = 0.01851
)

type conedDeliveryRates struct {
	rate1Tier1DollarsPerKWH        float64
	rate3SummerOnPeakDollarsPerKWH float64
	rate3WinterOnPeakDollarsPerKWH float64
	rate3OffPeakDollarsPerKWH      float64
}

var (
	conedJan2026Delivery = conedDeliveryRates{
		rate1Tier1DollarsPerKWH:        conedJan2026Rate1Tier1DeliveryDollarsPerKWH,
		rate3SummerOnPeakDollarsPerKWH: conedJan2026Rate3SummerOnPeakDeliveryDollarsPerKWH,
		rate3WinterOnPeakDollarsPerKWH: conedJan2026Rate3WinterOnPeakDeliveryDollarsPerKWH,
		rate3OffPeakDollarsPerKWH:      conedJan2026Rate3OffPeakDeliveryDollarsPerKWH,
	}

	conedRY1Delivery = conedDeliveryRates{
		rate1Tier1DollarsPerKWH:        conedRY1Rate1Tier1DeliveryDollarsPerKWH,
		rate3SummerOnPeakDollarsPerKWH: conedRY1Rate3SummerOnPeakDeliveryDollarsPerKWH,
		rate3WinterOnPeakDollarsPerKWH: conedRY1Rate3WinterOnPeakDeliveryDollarsPerKWH,
		rate3OffPeakDollarsPerKWH:      conedRY1Rate3OffPeakDeliveryDollarsPerKWH,
	}

	conedRY2Delivery = conedDeliveryRates{
		rate1Tier1DollarsPerKWH:        conedRY2Rate1Tier1DeliveryDollarsPerKWH,
		rate3SummerOnPeakDollarsPerKWH: conedRY2Rate3SummerOnPeakDeliveryDollarsPerKWH,
		rate3WinterOnPeakDollarsPerKWH: conedRY2Rate3WinterOnPeakDeliveryDollarsPerKWH,
		rate3OffPeakDollarsPerKWH:      conedRY2Rate3OffPeakDeliveryDollarsPerKWH,
	}
)

type conedZoneSupplyRates struct {
	isWestchester                        bool
	rate1AllHoursMSCDollarsPerKWH        float64
	rate3OnPeakMSCDollarsPerKWH          float64
	rate3OffPeakMSCDollarsPerKWH         float64
	rate3SummerSuperPeakCapDollarsPerKWH float64
	rate4OnPeakMSCDollarsPerKWH          float64
	rate4OffPeakMSCDollarsPerKWH         float64
}

// Zonal Market Supply Charges from Con Edison's official MSC calculator (LDS_APPND.CSV, Trip 17 20261023-20261123)
// across NYISO Load Zones J (NYC), I (Westchester), and H (Upper Westchester / Millwood).
var conedZoneSupplyByLocation = map[string]conedZoneSupplyRates{
	"nyc": {
		isWestchester:                        false,
		rate1AllHoursMSCDollarsPerKWH:        0.115526,
		rate3OnPeakMSCDollarsPerKWH:          0.074331,
		rate3OffPeakMSCDollarsPerKWH:         0.068771,
		rate3SummerSuperPeakCapDollarsPerKWH: conedRate3SummerSuperPeakCapNYCDollarsPerKWH,
		rate4OnPeakMSCDollarsPerKWH:          0.248837,
		rate4OffPeakMSCDollarsPerKWH:         0.070885,
	},
	"westchester": {
		isWestchester:                        true,
		rate1AllHoursMSCDollarsPerKWH:        0.094260,
		rate3OnPeakMSCDollarsPerKWH:          0.077010,
		rate3OffPeakMSCDollarsPerKWH:         0.070218,
		rate3SummerSuperPeakCapDollarsPerKWH: conedRate3SummerSuperPeakCapWestchesterDollarsPerKWH,
		rate4OnPeakMSCDollarsPerKWH:          0.158744,
		rate4OffPeakMSCDollarsPerKWH:         0.073053,
	},
	"upper_westchester": {
		isWestchester:                        true,
		rate1AllHoursMSCDollarsPerKWH:        0.089572,
		rate3OnPeakMSCDollarsPerKWH:          0.072184,
		rate3OffPeakMSCDollarsPerKWH:         0.066078,
		rate3SummerSuperPeakCapDollarsPerKWH: conedRate3SummerSuperPeakCapWestchesterDollarsPerKWH,
		rate4OnPeakMSCDollarsPerKWH:          0.153678,
		rate4OffPeakMSCDollarsPerKWH:         0.068644,
	},
}

// getConEdHolidays returns the six exact calendar holidays excluded from On-Peak windows
// on P.S.C. No. 10 SC 1 Rate IV (Leaf 389.2):
// New Year's Day (January 1), Memorial Day (last Monday in May), Independence Day (July 4),
// Labor Day (first Monday in September), Thanksgiving Day (fourth Thursday in November),
// and Christmas Day (December 25), without weekend observation shifts.
func getConEdHolidays(year int) []string {
	holidays := []time.Time{
		newYearsDay(year),
		memorialDay(year),
		independenceDay(year),
		laborDay(year),
		thanksgivingDay(year),
		christmasDay(year),
	}
	return formatHolidays(holidays, year)
}

func clipConEdPeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
	var clipped []types.UtilityFeesPeriod
	for _, p := range periods {
		if !p.Start.Before(windowEnd) || !p.End.After(windowStart) {
			continue
		}
		if p.Start.Before(windowStart) {
			p.Start = windowStart
		}
		if p.End.After(windowEnd) {
			p.End = windowEnd
		}
		clipped = append(clipped, p)
	}
	return clipped
}

func conedRule25AdderDollarsPerKWH(year int, isWestchester bool) float64 {
	mfcDollarsPerKWH := conedMFC2026DollarsPerKWH
	if year >= 2027 {
		mfcDollarsPerKWH = conedMFC2027DollarsPerKWH
	}
	mscAdjDollarsPerKWH := conedMSCAdjustmentNYCDollarsPerKWH
	if isWestchester {
		mscAdjDollarsPerKWH = conedMSCAdjustmentWestchesterDollarsPerKWH
	}
	return mfcDollarsPerKWH + conedCESSDollarsPerKWH + mscAdjDollarsPerKWH
}

func conedRule26AdderDollarsPerKWH(plan string, isWestchester bool) float64 {
	if plan == "coned_sc1_rate4" {
		if isWestchester {
			return conedRule26Rate4WestchesterDollarsPerKWH
		}
		return conedRule26Rate4NYCDollarsPerKWH
	}
	if isWestchester {
		return conedRule26Rates1And3WestchesterDollarsPerKWH
	}
	return conedRule26Rates1And3NYCDollarsPerKWH
}

func buildConEdYearPeriods(plan string, zone conedZoneSupplyRates, year int, delivery conedDeliveryRates) []types.UtilityFeesPeriod {
	ridersDollarsPerKWH := conedRule25AdderDollarsPerKWH(year, zone.isWestchester) + conedRule26AdderDollarsPerKWH(plan, zone.isWestchester)

	// Note: P.S.C. No. 10 SC 1 Rate II (Leaf 389, Voluntary Time-of-Day) was ignored because
	// it has been closed to new applicants for more than 10 years (since March 1, 2014).
	switch plan {
	case "coned_sc1_rate1":
		// SC 1 Rate I: Residential and Religious Service (Tier 1 <= 250 kWh/month flat volumetric rate).
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
					LocationPtr: etLocation,
				},
				DollarsPerKWH: delivery.rate1Tier1DollarsPerKWH + zone.rate1AllHoursMSCDollarsPerKWH + ridersDollarsPerKWH,
				Description:   "Con Edison SC 1 Rate I Tier 1 Charge",
			},
		}

	case "coned_sc1_rate3":
		// SC 1 Rate III: Residential and Religious - Voluntary Time-of-Use (Leaf 389.1 & MSC-Capacity Statement 135).
		// On-Peak: All days 08:00-24:00 (including holidays); Off-Peak: All other hours (00:00-08:00).
		// In Summer (June-September), weekdays 14:00-18:00 add MSC-Capacity Super-Peak supply.
		summerWeekdayOnPeakHours := []types.UtilityHourPeriod{
			{HourStart: 8, HourEnd: 14},
			{HourStart: 18, HourEnd: 24},
		}
		summerSuperPeakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 18}}
		dailyOnPeakHours := []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 24}}

		summerOnPeakDollarsPerKWH := delivery.rate3SummerOnPeakDollarsPerKWH + zone.rate3OnPeakMSCDollarsPerKWH + ridersDollarsPerKWH
		summerSuperPeakDollarsPerKWH := summerOnPeakDollarsPerKWH + zone.rate3SummerSuperPeakCapDollarsPerKWH
		winterOnPeakDollarsPerKWH := delivery.rate3WinterOnPeakDollarsPerKWH + zone.rate3OnPeakMSCDollarsPerKWH + ridersDollarsPerKWH
		offPeakDollarsPerKWH := delivery.rate3OffPeakDollarsPerKWH + zone.rate3OffPeakMSCDollarsPerKWH + ridersDollarsPerKWH

		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         summerWeekdayOnPeakHours,
						Weekday:       true,
						DollarsPerKWH: summerOnPeakDollarsPerKWH,
						Description:   "Con Edison SC 1 Rate III Summer Weekday On-Peak",
					},
					{
						Name:          "Super-Peak",
						Hours:         summerSuperPeakHours,
						Weekday:       true,
						DollarsPerKWH: summerSuperPeakDollarsPerKWH,
						Description:   "Con Edison SC 1 Rate III Summer Weekday Super-Peak",
					},
					{
						Name:          "On-Peak",
						Hours:         dailyOnPeakHours,
						Weekend:       true,
						DollarsPerKWH: summerOnPeakDollarsPerKWH,
						Description:   "Con Edison SC 1 Rate III Summer Weekend On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
				OtherDescription:   "Con Edison SC 1 Rate III Summer Off-Peak",
			},
			{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         dailyOnPeakHours,
						DollarsPerKWH: winterOnPeakDollarsPerKWH,
						Description:   "Con Edison SC 1 Rate III Winter On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
				OtherDescription:   "Con Edison SC 1 Rate III Winter Off-Peak",
			},
		})

	case "coned_sc1_rate4":
		// SC 1 Rate IV: Optional Demand-Based (Leaves 389.2-389.3).
		// Volumetric 001IPP TOU supply (Weekdays 12:00-20:00 excluding the six calendar holidays) +
		// General Rule 25/26 volumetric surcharges; $/kW Billable Demand charges omitted.
		holidays := getConEdHolidays(year)
		peak12To20Hours := []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}}

		onPeakDollarsPerKWH := zone.rate4OnPeakMSCDollarsPerKWH + ridersDollarsPerKWH
		offPeakDollarsPerKWH := zone.rate4OffPeakMSCDollarsPerKWH + ridersDollarsPerKWH

		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.December,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
				OtherDescription:   "Con Edison SC 1 Rate IV Holiday Off-Peak",
			},
			{
				Year:             year,
				MonthStart:       time.January,
				MonthEnd:         time.December,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak12To20Hours,
						Weekday:       true,
						DollarsPerKWH: onPeakDollarsPerKWH,
						Description:   "Con Edison SC 1 Rate IV Weekday On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: offPeakDollarsPerKWH,
				OtherDescription:   "Con Edison SC 1 Rate IV Off-Peak",
			},
		})
	}

	return nil
}

func conedPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	switch plan {
	case "coned_sc1_rate1", "coned_sc1_rate3", "coned_sc1_rate4":
	default:
		return nil, fmt.Errorf("unknown Con Edison rate plan: %s", plan)
	}

	locKey := strings.ToLower(strings.TrimSpace(opts.Location))
	if locKey == "" {
		locKey = "nyc"
	}
	zone, ok := conedZoneSupplyByLocation[locKey]
	if !ok {
		return nil, fmt.Errorf("unknown Con Edison location: %s", opts.Location)
	}

	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
		yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

		switch {
		case year < 2026:
			periods = append(periods, buildConEdYearPeriods(plan, zone, year, conedJan2026Delivery)...)
		case year == 2026:
			feb1 := time.Date(2026, time.February, 1, 0, 0, 0, 0, etLocation)
			janPeriods := buildConEdYearPeriods(plan, zone, 2026, conedJan2026Delivery)
			ry1Periods := buildConEdYearPeriods(plan, zone, 2026, conedRY1Delivery)
			periods = append(periods, clipConEdPeriods(janPeriods, yearStart, feb1)...)
			periods = append(periods, clipConEdPeriods(ry1Periods, feb1, yearEnd)...)
		default:
			periods = append(periods, buildConEdYearPeriods(plan, zone, year, conedRY2Delivery)...)
		}
	}

	return periods, nil
}

func conedUtilityInfo() types.UtilityProviderInfo {
	locationOption := types.UtilityRateOption{
		Field:       "location",
		Name:        "Service Zone / Location",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your Con Edison NYISO load zone for Market Supply Charge and regional rider pricing.",
		Choices: []types.UtilityOptionChoice{
			{Value: "nyc", Name: "New York City (NYISO Zone J)"},
			{Value: "westchester", Name: "Westchester County - South/Central (NYISO Zone I)"},
			{Value: "upper_westchester", Name: "Upper Westchester / Millwood (NYISO Zone H)"},
		},
		Default: "nyc",
	}

	// Note: SC 1 Rate II (P.S.C. No. 10 Leaf 389, Voluntary Time-of-Day) was ignored because
	// it has been closed to new applicants for more than 10 years (since March 1, 2014).
	return types.UtilityProviderInfo{
		ID:   "coned",
		Name: "Consolidated Edison (Con Edison)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "coned_sc1_rate1",
				Name:    "Residential & Religious Service (SC 1 Rate I)",
				Options: []types.UtilityRateOption{locationOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return conedPeriods("coned_sc1_rate1", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "coned_sc1_rate3",
				Name:    "Residential Voluntary Time-of-Use (SC 1 Rate III)",
				Options: []types.UtilityRateOption{locationOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return conedPeriods("coned_sc1_rate3", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "coned_sc1_rate4",
				Name:    "Residential Optional Demand-Based (SC 1 Rate IV)",
				Options: []types.UtilityRateOption{locationOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return conedPeriods("coned_sc1_rate4", opts, []int{2026, 2027})
				},
			},
		},
	}
}
