package utility

// Tariff Audit Metadata
// - Provider(s): dte (DTE Electric)
// - Official Sources:
//   - https://www.dteenergy.com/content/dam/dteenergy/deg/website/common/service-and-price/rate-cards/DTEElectricRateBook.pdf (M.P.S.C. No. 1 - Electric, Final Order Case No. U-21860: Sheet C-62.00 PSCR, Sheet C-65.00 Power Supply Surcharges, Sheet C-70.00 Delivery Surcharges, Sheet C-72.00 Section C11 Holidays, Rate Schedules D1, D1.2, D1.8, D1.9, D1.11, D1.13, Standard Contract Rider No. 16 Sheets D-101.00–D-106.00, and Standard Contract Rider No. 18 Sheets D-112.00–D-116.03)
//   - https://www.dteenergy.com/content/dam/dteenergy/deg/website/residential/Service-Request/pricing/residential-pricing-options/ResidentialElectricRateCard.pdf (DTE Electric Residential Electric Rate Card)
//   - https://www.dteenergy.com/us/en/residential/service-request/pricing/rate-options/residential-pricing-options.html (DTE Electric Residential Pricing Options)
// - Effective Window: 2026-01-01 through 2027-12-31 ([]int{2026, 2027}; Sheet C-62.00 PSCR $0.00250/kWh Jan–Feb 2026 and $0.01877/kWh effective 2026-03-01 carried through 2027 until superseded)
// - Revision Cadence: Monthly / Periodic (Sheet C-62.00 PSCR Factor & Sheets C-65.00 / C-70.00 Surcharges) & MPSC General Rate Case Orders
// - Next Audit Due: 2027-03-01 (Check 2027 PSCR Plan factor on Sheet C-62.00, annual securitization/delivery surcharge updates on Sheets C-65.00 & C-70.00, and any new MPSC rate case order)
// - Last Verified: 2026-11-24
// - Modeling Notes: Models six M.P.S.C. No. 1 residential schedules in Eastern Time (etLocation): D1.11 Standard TOU, D1.2 Enhanced TOU, D1.13 Overnight Savers, D1.8 Dynamic Peak Pricing (excluding dynamic Critical Peak events; Section C11 holidays observed Off-Peak with Sunday holidays shifted to Monday), D1.9 Electric Vehicle Option 1 TOU, and D1 Non-Transmitting Meter (Tier 1 <=17 kWh/day). Excludes fixed monthly charges ($8.50/mo, $1.95/mo on D1.9, and $1.50/mo LIEAF) and omits <1% interim surcharge steps. Credits Power Supply + PSCR on outflow under Rider 18 (default) and Power Supply + PSCR + Distribution under Rider 16 while keeping surcharges non-creditable (GridAdditional: true).

import (
	"fmt"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Sheet C-62.00 Power Supply Cost Recovery (PSCR) factors ($/kWh).
	dtePSCRJanFeb2026DollarsPerKWH    = 0.00250
	dtePSCRMar2026OnwardDollarsPerKWH = 0.01877

	// Standard residential volumetric distribution charge ($/kWh) for D1, D1.2, D1.8, D1.9, and D1.11.
	dteFlatDistributionDollarsPerKWH = 0.09726

	// Combined volumetric surcharges ($/kWh) from Sheet C-65.00 (River Rouge + TCSC Securitization,
	// excluding PSCR) plus Sheet C-70.00 (NS + EWRS + Base Securitization + IRM Delivery Surcharges).
	dteD111SurchargesDollarsPerKWH = 0.012270 // $0.002513 + $0.009757
	dteD12SurchargesDollarsPerKWH  = 0.011374 // $0.001731 + $0.009643
	dteD113SurchargesDollarsPerKWH = 0.012270 // $0.002513 + $0.009757
	dteD18SurchargesDollarsPerKWH  = 0.011851 // $0.002121 + $0.009730
	dteD19SurchargesDollarsPerKWH  = 0.011439 // $0.001770 + $0.009669
	dteD1SurchargesDollarsPerKWH   = 0.011406 // $0.002527 + $0.008879

	// Rate D1.11 Standard TOU base power supply charges ($/kWh, Sheet D-14.05 & Sheet D-115.00).
	dteD111SummerOnPeakSupplyDollarsPerKWH  = 0.14407 // $0.05360 capacity + $0.09047 non-capacity
	dteD111SummerOffPeakSupplyDollarsPerKWH = 0.08709 // $0.03240 capacity + $0.05469 non-capacity
	dteD111WinterOnPeakSupplyDollarsPerKWH  = 0.10319 // $0.03839 capacity + $0.06480 non-capacity
	dteD111WinterOffPeakSupplyDollarsPerKWH = 0.08709 // $0.03240 capacity + $0.05469 non-capacity

	// Rate D1.2 Enhanced TOU base power supply charges ($/kWh, Sheet D-6.00 & Sheet D-115.00).
	dteD12SummerOnPeakSupplyDollarsPerKWH  = 0.16203 // $0.04677 capacity + $0.11526 non-capacity
	dteD12SummerOffPeakSupplyDollarsPerKWH = 0.05456 // $0.01575 capacity + $0.03881 non-capacity
	dteD12WinterOnPeakSupplyDollarsPerKWH  = 0.13687 // $0.03951 capacity + $0.09736 non-capacity
	dteD12WinterOffPeakSupplyDollarsPerKWH = 0.05241 // $0.01513 capacity + $0.03728 non-capacity

	// Rate D1.13 Overnight Savers base power supply charges ($/kWh, Sheet D-14.08 & Sheet D-115.00).
	dteD113SummerOnPeakSupplyDollarsPerKWH       = 0.15991 // $0.05949 capacity + $0.10042 non-capacity
	dteD113SummerOffPeakSupplyDollarsPerKWH      = 0.10784 // $0.04012 capacity + $0.06772 non-capacity
	dteD113SummerSuperOffPeakSupplyDollarsPerKWH = 0.06852 // $0.02549 capacity + $0.04303 non-capacity
	dteD113WinterOnPeakSupplyDollarsPerKWH       = 0.09274 // $0.03450 capacity + $0.05824 non-capacity
	dteD113WinterOffPeakSupplyDollarsPerKWH      = 0.08214 // $0.03056 capacity + $0.05158 non-capacity
	dteD113WinterSuperOffPeakSupplyDollarsPerKWH = 0.06852 // $0.02549 capacity + $0.04303 non-capacity

	// Rate D1.13 Overnight Savers TOU distribution charges ($/kWh, Sheet D-14.08).
	dteD113SummerOnPeakDistDollarsPerKWH       = 0.19568
	dteD113SummerOffPeakDistDollarsPerKWH      = 0.14676
	dteD113SummerSuperOffPeakDistDollarsPerKWH = 0.04892
	dteD113WinterOnPeakDistDollarsPerKWH       = 0.09784
	dteD113WinterOffPeakDistDollarsPerKWH      = 0.07338
	dteD113WinterSuperOffPeakDistDollarsPerKWH = 0.04892

	// Rate D1.8 Dynamic Peak Pricing base power supply charges ($/kWh, Sheet D-14.01 & Sheet D-115.00).
	dteD18OnPeakSupplyDollarsPerKWH  = 0.17198 // $0.06398 capacity + $0.10800 non-capacity
	dteD18MidPeakSupplyDollarsPerKWH = 0.08884 // $0.03305 capacity + $0.05579 non-capacity
	dteD18OffPeakSupplyDollarsPerKWH = 0.04583 // $0.01705 capacity + $0.02878 non-capacity

	// Rate D1.9 Electric Vehicle Option 1 TOU base power supply charges ($/kWh, Sheet D-14.03 & Sheet D-115.00).
	dteD19OnPeakSupplyDollarsPerKWH  = 0.17359 // $0.06455 capacity + $0.10904 non-capacity
	dteD19OffPeakSupplyDollarsPerKWH = 0.04340 // $0.01615 capacity + $0.02725 non-capacity

	// Rate D1 Residential Service Tier 1 (first 17 kWh/day) base power supply charge ($/kWh, Sheet D-1.00 & Sheet D-115.00).
	dteD1Tier1SupplyDollarsPerKWH = 0.08782 // $0.02940 capacity + $0.05842 non-capacity
)

// shiftDTESundayHoliday shifts a traditional Section C11 holiday falling on Sunday
// to the legally recognized Monday holiday (Sheet C-72.00). Saturday holidays remain
// on Saturday, which is already Off-Peak.
func shiftDTESundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

// getDTESectionC11Holidays returns the M.P.S.C. No. 1 Section C11 (Sheet C-72.00) legal
// holidays observed by Rate Schedule D1.8: New Year's Day, Good Friday, Memorial Day,
// Independence Day, Labor Day, Thanksgiving Day, and Christmas Day.
func getDTESectionC11Holidays(year int) []string {
	holidays := []time.Time{
		shiftDTESundayHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftDTESundayHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftDTESundayHoliday(christmasDay(year)),
	}
	return formatHolidays(holidays, year)
}

func clipDTEPeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
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

func buildDTEBaseYearPeriods(plan, scheme string, year int, pscrDollarsPerKWH float64) []types.UtilityFeesPeriod {
	includeDistInBase := scheme == "rider_16"
	flatBaseAdderDollarsPerKWH := pscrDollarsPerKWH
	if includeDistInBase {
		flatBaseAdderDollarsPerKWH += dteFlatDistributionDollarsPerKWH
	}

	peak3To7Hours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}}
	peak11To7Hours := []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 19}}
	superOffPeak1To7Hours := []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 7}}
	d18MidPeakHours := []types.UtilityHourPeriod{
		{HourStart: 7, HourEnd: 15},
		{HourStart: 19, HourEnd: 23},
	}
	d19PeakHours := []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 23}}

	switch plan {
	case "dte_d1_11":
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD111SummerOnPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.11 Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD111SummerOffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.11 Summer Off-Peak",
			},
			{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD111WinterOnPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.11 Oct-May On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD111WinterOffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.11 Oct-May Off-Peak",
			},
		})

	case "dte_d1_2":
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.October,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak11To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD12SummerOnPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.2 Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD12SummerOffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.2 Summer Off-Peak",
			},
			{
				Year:       year,
				MonthStart: time.November,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak11To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD12WinterOnPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.2 Winter On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD12WinterOffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.2 Winter Off-Peak",
			},
		})

	case "dte_d1_13":
		summerOnPeakDollarsPerKWH := dteD113SummerOnPeakSupplyDollarsPerKWH + pscrDollarsPerKWH
		summerOffPeakDollarsPerKWH := dteD113SummerOffPeakSupplyDollarsPerKWH + pscrDollarsPerKWH
		summerSuperOffPeakDollarsPerKWH := dteD113SummerSuperOffPeakSupplyDollarsPerKWH + pscrDollarsPerKWH
		winterOnPeakDollarsPerKWH := dteD113WinterOnPeakSupplyDollarsPerKWH + pscrDollarsPerKWH
		winterOffPeakDollarsPerKWH := dteD113WinterOffPeakSupplyDollarsPerKWH + pscrDollarsPerKWH
		winterSuperOffPeakDollarsPerKWH := dteD113WinterSuperOffPeakSupplyDollarsPerKWH + pscrDollarsPerKWH
		if includeDistInBase {
			summerOnPeakDollarsPerKWH += dteD113SummerOnPeakDistDollarsPerKWH
			summerOffPeakDollarsPerKWH += dteD113SummerOffPeakDistDollarsPerKWH
			summerSuperOffPeakDollarsPerKWH += dteD113SummerSuperOffPeakDistDollarsPerKWH
			winterOnPeakDollarsPerKWH += dteD113WinterOnPeakDistDollarsPerKWH
			winterOffPeakDollarsPerKWH += dteD113WinterOffPeakDistDollarsPerKWH
			winterSuperOffPeakDollarsPerKWH += dteD113WinterSuperOffPeakDistDollarsPerKWH
		}
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: summerOnPeakDollarsPerKWH,
						Description:   "DTE Rate D1.13 Summer On-Peak",
					},
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeak1To7Hours,
						DollarsPerKWH: summerSuperOffPeakDollarsPerKWH,
						Description:   "DTE Rate D1.13 Summer Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: summerOffPeakDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.13 Summer Off-Peak",
			},
			{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: winterOnPeakDollarsPerKWH,
						Description:   "DTE Rate D1.13 Oct-May On-Peak",
					},
					{
						Name:          "Super Off-Peak",
						Hours:         superOffPeak1To7Hours,
						DollarsPerKWH: winterSuperOffPeakDollarsPerKWH,
						Description:   "DTE Rate D1.13 Oct-May Super Off-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.13 Oct-May Off-Peak",
			},
		})

	case "dte_d1_8":
		holidays := getDTESectionC11Holidays(year)
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.December,
				SpecificDates:      holidays,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD18OffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.8 Holiday Off-Peak",
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
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD18OnPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.8 On-Peak",
					},
					{
						Name:          "Mid-Peak",
						Hours:         d18MidPeakHours,
						Weekday:       true,
						DollarsPerKWH: dteD18MidPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.8 Mid-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD18OffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.8 Off-Peak",
			},
		})

	case "dte_d1_9":
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.January,
				MonthEnd:   time.December,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         d19PeakHours,
						Weekday:       true,
						DollarsPerKWH: dteD19OnPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
						Description:   "DTE Rate D1.9 On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: dteD19OffPeakSupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.9 Off-Peak",
			},
		})

	case "dte_d1":
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
					LocationPtr: etLocation,
				},
				DollarsPerKWH: dteD1Tier1SupplyDollarsPerKWH + flatBaseAdderDollarsPerKWH,
				Description:   "DTE Rate D1 Tier 1 Energy Charge",
			},
		}
	}

	return nil
}

func buildDTEGridAdditionalPeriods(plan, scheme string, year int) []types.UtilityFeesPeriod {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

	var surchargesDollarsPerKWH float64
	switch plan {
	case "dte_d1_11":
		surchargesDollarsPerKWH = dteD111SurchargesDollarsPerKWH
	case "dte_d1_2":
		surchargesDollarsPerKWH = dteD12SurchargesDollarsPerKWH
	case "dte_d1_13":
		surchargesDollarsPerKWH = dteD113SurchargesDollarsPerKWH
	case "dte_d1_8":
		surchargesDollarsPerKWH = dteD18SurchargesDollarsPerKWH
	case "dte_d1_9":
		surchargesDollarsPerKWH = dteD19SurchargesDollarsPerKWH
	case "dte_d1":
		surchargesDollarsPerKWH = dteD1SurchargesDollarsPerKWH
	}

	// Under Rider 16 (Sheets D-103.00 & D-106.00), distribution is included in DollarsPerKWH
	// so TOU supply and distribution are credited on outflow, while volumetric surcharges remain
	// non-creditable (GridAdditional: true).
	if scheme == "rider_16" {
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: etLocation,
				},
				DollarsPerKWH:  surchargesDollarsPerKWH,
				GridAdditional: true,
				Description:    "DTE Power Supply & Delivery Surcharges",
			},
		}
	}

	// Under Rider 18 (Sheets D-114.00 & D-115.00), distribution and surcharges are charged
	// only on inflow (GridAdditional: true). Rate D1.13 applies seasonal TOU distribution rates.
	if plan == "dte_d1_13" {
		peak3To7Hours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}}
		superOffPeak1To7Hours := []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 7}}
		distPeriods := buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD113SummerOnPeakDistDollarsPerKWH + surchargesDollarsPerKWH,
						Description:   "DTE Rate D1.13 Summer On-Peak Distribution & Surcharges",
					},
					{
						Hours:         superOffPeak1To7Hours,
						DollarsPerKWH: dteD113SummerSuperOffPeakDistDollarsPerKWH + surchargesDollarsPerKWH,
						Description:   "DTE Rate D1.13 Summer Super Off-Peak Distribution & Surcharges",
					},
				},
				OtherDollarsPerKWH: dteD113SummerOffPeakDistDollarsPerKWH + surchargesDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.13 Summer Off-Peak Distribution & Surcharges",
			},
			{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Hours:         peak3To7Hours,
						Weekday:       true,
						DollarsPerKWH: dteD113WinterOnPeakDistDollarsPerKWH + surchargesDollarsPerKWH,
						Description:   "DTE Rate D1.13 Oct-May On-Peak Distribution & Surcharges",
					},
					{
						Hours:         superOffPeak1To7Hours,
						DollarsPerKWH: dteD113WinterSuperOffPeakDistDollarsPerKWH + surchargesDollarsPerKWH,
						Description:   "DTE Rate D1.13 Oct-May Super Off-Peak Distribution & Surcharges",
					},
				},
				OtherDollarsPerKWH: dteD113WinterOffPeakDistDollarsPerKWH + surchargesDollarsPerKWH,
				OtherDescription:   "DTE Rate D1.13 Oct-May Off-Peak Distribution & Surcharges",
			},
		})
		for i := range distPeriods {
			distPeriods[i].GridAdditional = true
			distPeriods[i].Name = ""
		}
		return distPeriods
	}

	return []types.UtilityFeesPeriod{
		{
			TimePeriod: types.TimePeriod{
				Start:       yearStart,
				End:         yearEnd,
				LocationPtr: etLocation,
			},
			DollarsPerKWH:  dteFlatDistributionDollarsPerKWH + surchargesDollarsPerKWH,
			GridAdditional: true,
			Description:    "DTE Distribution & Surcharges",
		},
	}
}

func dtePeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	switch plan {
	case "dte_d1_11", "dte_d1_2", "dte_d1_13", "dte_d1_8", "dte_d1_9", "dte_d1":
	default:
		return nil, fmt.Errorf("unknown DTE rate plan: %s", plan)
	}

	scheme := strings.ToLower(strings.TrimSpace(opts.NetMeteringScheme))
	if scheme == "" {
		scheme = "rider_18"
	}
	if scheme != "rider_18" && scheme != "rider_16" {
		return nil, fmt.Errorf("unknown DTE net metering scheme: %s", opts.NetMeteringScheme)
	}

	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
		yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

		switch {
		case year < 2026:
			periods = append(periods, buildDTEBaseYearPeriods(plan, scheme, year, dtePSCRJanFeb2026DollarsPerKWH)...)
		case year == 2026:
			mar1 := time.Date(2026, time.March, 1, 0, 0, 0, 0, etLocation)
			preMarPeriods := buildDTEBaseYearPeriods(plan, scheme, 2026, dtePSCRJanFeb2026DollarsPerKWH)
			postMarPeriods := buildDTEBaseYearPeriods(plan, scheme, 2026, dtePSCRMar2026OnwardDollarsPerKWH)
			periods = append(periods, clipDTEPeriods(preMarPeriods, yearStart, mar1)...)
			periods = append(periods, clipDTEPeriods(postMarPeriods, mar1, yearEnd)...)
		default:
			periods = append(periods, buildDTEBaseYearPeriods(plan, scheme, year, dtePSCRMar2026OnwardDollarsPerKWH)...)
		}

		periods = append(periods, buildDTEGridAdditionalPeriods(plan, scheme, year)...)
	}

	return periods, nil
}

func dteUtilityInfo() types.UtilityProviderInfo {
	netMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your distributed generation or legacy net metering rider.",
		Choices: []types.UtilityOptionChoice{
			{Value: "rider_18", Name: "Rider 18 - Distributed Generation Program"},
			{Value: "rider_16", Name: "Rider 16 - Legacy Net Metering"},
		},
		Default: "rider_18",
	}

	return types.UtilityProviderInfo{
		ID:   "dte",
		Name: "DTE Electric",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "dte_d1_11",
				Name:    "Time of Day 3 p.m. - 7 p.m. Standard TOU (Rate D1.11)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dtePeriods("dte_d1_11", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "dte_d1_2",
				Name:    "Time of Day 11 a.m. - 7 p.m. Enhanced TOU (Rate D1.2)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dtePeriods("dte_d1_2", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "dte_d1_13",
				Name:    "Overnight Savers (Rate D1.13)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dtePeriods("dte_d1_13", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "dte_d1_8",
				Name:    "Dynamic Peak Pricing (Rate D1.8)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dtePeriods("dte_d1_8", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "dte_d1_9",
				Name:    "Electric Vehicle Rate (Rate D1.9)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dtePeriods("dte_d1_9", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "dte_d1",
				Name:    "Residential Service - Non-Transmitting Meter (Rate D1)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dtePeriods("dte_d1", opts, []int{2026, 2027})
				},
			},
		},
	}
}
