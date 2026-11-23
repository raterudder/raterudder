package utility

// Tariff Audit Metadata
// - Provider(s): ameren (Ameren Missouri)
// - Official Sources:
//   - https://www.ameren.com/bill/rates/residential (Ameren Residential Electric Rates Portal)
//   - https://www.ameren.com/-/media/rates/missouri/residential/electric-rates/rates/uecsheet54rate1mres.ashx (MO.P.S.C. Schedule No. 6, Service Classification No. 1(M) Sheets 54 - 54.17, Case No. ER-2024-0319, eff. June 1, 2025)
//   - https://www.ameren.com/-/media/rates/missouri/residential/electric-rates/electric-power-purchases/uecsheet171eppnetmetering.ashx (7th Revised Sheet No. 171 Net Metering Avoided Fuel Cost, eff. June 1, 2026)
//   - https://www.ameren.com/-/media/rates/missouri/residential/electric-rates/riders/uecsheet71riderfac.ashx (Rider FAC, 4th Rev. Sheet No. 72.14, eff. Oct 1, 2026 - Jan 31, 2027: $0.00445/kWh)
//   - https://www.ameren.com/-/media/rates/missouri/residential/electric-rates/riders/uecesheetno91ridereeiceeinvestmentcharge.ashx (Rider EEIC, 7th Rev. Sheet No. 91.23, eff. Feb 1, 2026 - Jan 31, 2027: $0.001905/kWh)
//   - https://www.ameren.com/-/media/rates/missouri/residential/electric-rates/riders/uecesheet93blanksheets.ashx (Rider RESRAM, Sheet No. 93.4, eff. Feb 1, 2026 - Jan 31, 2027: $0.00207/kWh)
//   - https://www.ameren.com/-/media/rates/missouri/residential/electric-rates/riders/uec-e-sheet-no-90-rider-sur-securitized-ut.ashx (Rider SUR, 8th Rev. Sheet No. 90.4, eff. Oct 1, 2026 - Mar 31, 2027: $0.00157/kWh)
// - Effective Window: 2026-01-01 through 2027-12-31 (Rate 1(M) base charges eff. 2025-06-01 until superseded; Sheet 171 avoided fuel cost eff. 2026-06-01; Oct 1, 2026 FAC/EEIC/RESRAM/SUR rider sum $0.009995/kWh carried through 2027-12-31 until superseded)
// - Revision Cadence: Tri-Annual / Periodic (Feb 1, Jun 1, Oct 1 Rider FAC; Feb 1 Rider EEIC & Rider RESRAM; Apr 1 & Oct 1 Rider SUR; Jun 1 Sheet 171 Net Metering)
// - Next Audit Due: 2026-12-05 (Aligned with provider ameren monthly check; next MO Rider FAC/EEIC/RESRAM filings take effect Feb 1, 2027)
// - Last Verified: 2026-11-21
// - Modeling Notes: Models Rate 1(M) Tier 1 (<=750 kWh/mo winter on Anytime and Evening/Morning Savers) base volumetric rates plus mandatory volumetric riders (Rider FAC + Rider EEIC + Rider RESRAM + Rider SUR: $0.009681 Jan 2026, $0.010455 Feb-Mar 2026, $0.010555 Apr-May 2026, $0.014095 Jun-Sep 2026, $0.009995 Oct 2026 - Dec 2027); excludes fixed monthly Customer Charges and Ultimate Saver per-kW demand charges; supports 1:1 monthly ("net") or TOU-bucket ("tou_net") net metering and Sheet 171 excess generation avoided fuel cost ("avoided_cost": $0.0384 Summer / $0.0339 Winter).

import (
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Ameren Missouri residential tariff rates under MO.P.S.C. Schedule No. 6,
// Service Classification No. 1(M) (effective June 1, 2025 under Case No. ER-2024-0319)
// and Electric Power Purchases from Qualified Net Metering Units (7th Revised Sheet No. 171,
// effective June 1, 2026).
const (
	// Residential Anytime Service (Sheet No. 54).
	// NOTE: Winter rates on Anytime and Evening/Morning Savers are tiered at 750 kWh/month;
	// we assume Tier 1 (first 750 kWh) usage.
	amerenMOAnytimeSummerDollarsPerKWH = 0.1560 // June - September, all kWh
	amerenMOAnytimeWinterDollarsPerKWH = 0.1062 // October - May, first 750 kWh

	// Residential Evening/Morning Saver Service (Sheet No. 54.4)
	amerenMOEveningMorningSummerBaseDollarsPerKWH    = 0.1524 // June - September, all kWh
	amerenMOEveningMorningSummerPeakAdjDollarsPerKWH = 0.0057 // +0.57¢/kWh 9 AM - 9 PM all days
	amerenMOEveningMorningWinterBaseDollarsPerKWH    = 0.1045 // October - May, first 750 kWh
	amerenMOEveningMorningWinterPeakAdjDollarsPerKWH = 0.0028 // +0.28¢/kWh 9 AM - 9 PM all days

	// Residential Overnight Saver Service (Sheet No. 54.10)
	amerenMOOvernightSummerOnPeakDollarsPerKWH  = 0.1839 // June - September, 6 AM - 10 PM all days
	amerenMOOvernightSummerOffPeakDollarsPerKWH = 0.0732 // June - September, 10 PM - 6 AM all days
	amerenMOOvernightWinterOnPeakDollarsPerKWH  = 0.1034 // October - May (Option A), 6 AM - 10 PM all days
	amerenMOOvernightWinterOffPeakDollarsPerKWH = 0.0631 // October - May (Option A), 10 PM - 6 AM all days

	// Residential Smart Saver Service (3-tier TOU)
	amerenMOSmartSaverSummerOnPeakDollarsPerKWH       = 0.4051 // June - September, 3 PM - 7 PM Mon-Fri excl. holidays
	amerenMOSmartSaverSummerIntermediateDollarsPerKWH = 0.1216 // June - September, 6 AM - 10 PM excl. On-Peak
	amerenMOSmartSaverSummerOffPeakDollarsPerKWH      = 0.0767 // June - September, 10 PM - 6 AM all days
	amerenMOSmartSaverWinterOnPeakDollarsPerKWH       = 0.2169 // October - May (Option A), 6-8 AM & 6-8 PM Mon-Fri excl. holidays
	amerenMOSmartSaverWinterIntermediateDollarsPerKWH = 0.0778 // October - May (Option A), 6 AM - 10 PM excl. On-Peak
	amerenMOSmartSaverWinterOffPeakDollarsPerKWH      = 0.0635 // October - May (Option A), 10 PM - 6 AM all days

	// Residential Ultimate Saver Service (Demand + 2-tier TOU)
	amerenMOUltimateSaverSummerOnPeakDollarsPerKWH  = 0.3413 // June - September, 3 PM - 7 PM Mon-Fri excl. holidays
	amerenMOUltimateSaverSummerOffPeakDollarsPerKWH = 0.0578 // June - September, all other hours
	amerenMOUltimateSaverWinterOnPeakDollarsPerKWH  = 0.1856 // October - May (Option A), 6-8 AM & 6-8 PM Mon-Fri excl. holidays
	amerenMOUltimateSaverWinterOffPeakDollarsPerKWH = 0.0511 // October - May (Option A), all other hours

	// Net Metering Excess Generation Avoided Fuel Cost (Sheet No. 171)
	amerenMOSummerAvoidedCostDollarsPerKWH = 0.0384 // June - September
	amerenMOWinterAvoidedCostDollarsPerKWH = 0.0339 // October - May

	// Mandatory Missouri Residential Volumetric Riders (MO.P.S.C. Schedule No. 6):
	// Current active component sheets (effective Oct 1, 2026):
	// - Rider FAC (4th Rev. Sheet No. 72.14, Secondary): $0.00445/kWh
	// - Rider EEIC (7th Rev. Sheet No. 91.23, 1(M)-Residential):  $0.001905/kWh
	// - Rider RESRAM (Sheet No. 93.4):                   $0.00207/kWh
	// - Rider SUR (8th Rev. Sheet No. 90.4, Secondary):  $0.00157/kWh
	// Total Oct 2026 - Dec 2027 = $0.009995/kWh.
	amerenMOFACOct2026DollarsPerKWH    = 0.00445
	amerenMOEEIC2026DollarsPerKWH      = 0.001905
	amerenMORESRAMFeb2026DollarsPerKWH = 0.00207
	amerenMOSUROct2026DollarsPerKWH    = 0.00157

	amerenMORidersJan2026DollarsPerKWH    = 0.009681
	amerenMORidersFebMar2026DollarsPerKWH = 0.010455
	amerenMORidersAprMay2026DollarsPerKWH = 0.010555
	amerenMORidersJunSep2026DollarsPerKWH = 0.014095
	amerenMORidersOct2026DollarsPerKWH    = amerenMOFACOct2026DollarsPerKWH + amerenMOEEIC2026DollarsPerKWH + amerenMORESRAMFeb2026DollarsPerKWH + amerenMOSUROct2026DollarsPerKWH
)

type amerenMORiderWindow struct {
	monthStart          time.Month
	monthEnd            time.Month
	isSummer            bool
	ridersDollarsPerKWH float64
}

// getAmerenMORidersDollarsPerKWH returns the combined per-kWh sum of mandatory
// Ameren Missouri residential riders (Rider FAC + Rider EEIC + Rider RESRAM + Rider SUR)
// for a given calendar year and month.
func getAmerenMORidersDollarsPerKWH(year int, month time.Month) float64 {
	if year < 2026 {
		return amerenMORidersJan2026DollarsPerKWH
	}
	if year == 2026 {
		switch {
		case month == time.January:
			return amerenMORidersJan2026DollarsPerKWH
		case month >= time.February && month <= time.March:
			return amerenMORidersFebMar2026DollarsPerKWH
		case month >= time.April && month <= time.May:
			return amerenMORidersAprMay2026DollarsPerKWH
		case month >= time.June && month <= time.September:
			return amerenMORidersJunSep2026DollarsPerKWH
		default:
			return amerenMORidersOct2026DollarsPerKWH
		}
	}
	return amerenMORidersOct2026DollarsPerKWH
}

// amerenMORiderWindows splits a calendar year into non-wrapping (MonthStart <= MonthEnd)
// seasonal and rider windows so buildPeriods emits exactly one base period per hour.
func amerenMORiderWindows(year int) []amerenMORiderWindow {
	if year == 2026 {
		return []amerenMORiderWindow{
			{
				monthStart:          time.January,
				monthEnd:            time.January,
				isSummer:            false,
				ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(2026, time.January),
			},
			{
				monthStart:          time.February,
				monthEnd:            time.March,
				isSummer:            false,
				ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(2026, time.February),
			},
			{
				monthStart:          time.April,
				monthEnd:            time.May,
				isSummer:            false,
				ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(2026, time.April),
			},
			{
				monthStart:          time.June,
				monthEnd:            time.September,
				isSummer:            true,
				ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(2026, time.June),
			},
			{
				monthStart:          time.October,
				monthEnd:            time.December,
				isSummer:            false,
				ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(2026, time.October),
			},
		}
	}

	return []amerenMORiderWindow{
		{
			monthStart:          time.January,
			monthEnd:            time.May,
			isSummer:            false,
			ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(year, time.January),
		},
		{
			monthStart:          time.June,
			monthEnd:            time.September,
			isSummer:            true,
			ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(year, time.June),
		},
		{
			monthStart:          time.October,
			monthEnd:            time.December,
			isSummer:            false,
			ridersDollarsPerKWH: getAmerenMORidersDollarsPerKWH(year, time.October),
		},
	}
}

func shiftAmerenMOWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func amerenMOChristmasHolidays(year int) (time.Time, time.Time) {
	eve := christmasEve(year)
	day := christmasDay(year)
	switch day.Weekday() {
	case time.Saturday:
		// Dec 24 is Friday, Dec 25 is Saturday -> observed Thursday Dec 23 and Friday Dec 24 (e.g. 2027)
		return eve.AddDate(0, 0, -1), day.AddDate(0, 0, -1)
	case time.Sunday:
		// Dec 24 is Saturday, Dec 25 is Sunday -> observed Friday Dec 23 and Monday Dec 26 (e.g. 2033)
		return eve.AddDate(0, 0, -1), day.AddDate(0, 0, 1)
	case time.Monday:
		// Dec 24 is Sunday, Dec 25 is Monday -> observed Friday Dec 22 and Monday Dec 25 (e.g. 2028)
		return eve.AddDate(0, 0, -2), day
	default:
		return eve, day
	}
}

// getAmerenMOHolidays returns Ameren Missouri's 9 observed tariff holidays per
// MO.P.S.C. Schedule No. 6 (New Year's Day, Good Friday, Memorial Day,
// Independence Day, Labor Day, Thanksgiving Day, Thanksgiving Friday,
// Christmas Eve Day, and Christmas Day).
func getAmerenMOHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)
	xmasEve, xmasDay := amerenMOChristmasHolidays(year)

	holidays := []time.Time{
		shiftAmerenMOWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftAmerenMOWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		xmasEve,
		xmasDay,
		shiftAmerenMOWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

func amerenMOPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	isOptionB := strings.EqualFold(strings.TrimSpace(options.PeakPeriodOption), "option_b")
	isAvoidedCost := strings.EqualFold(strings.TrimSpace(options.NetMeteringScheme), "avoided_cost")

	for _, year := range years {
		holidays := getAmerenMOHolidays(year)
		windows := amerenMORiderWindows(year)
		var simplified []touSimplifiedPeriod

		for _, w := range windows {
			riders := w.ridersDollarsPerKWH

			switch plan {
			case "ameren_mo_anytime":
				if w.isSummer {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:               year,
						MonthStart:         w.monthStart,
						MonthEnd:           w.monthEnd,
						OtherDollarsPerKWH: amerenMOAnytimeSummerDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Anytime Summer Energy Charge",
					})
				} else {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:               year,
						MonthStart:         w.monthStart,
						MonthEnd:           w.monthEnd,
						OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Anytime Winter Energy Charge",
					})
				}

			case "ameren_mo_evening_morning":
				if w.isSummer {
					// Summer (June - September): On-Peak 9:00 AM - 9:00 PM all days
					simplified = append(simplified, touSimplifiedPeriod{
						Year:       year,
						MonthStart: w.monthStart,
						MonthEnd:   w.monthEnd,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
								DollarsPerKWH: amerenMOEveningMorningSummerBaseDollarsPerKWH + amerenMOEveningMorningSummerPeakAdjDollarsPerKWH + riders,
								Description:   "Ameren MO Evening/Morning Savers Summer On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOEveningMorningSummerBaseDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Evening/Morning Savers Summer Off-Peak",
					})
				} else {
					// Winter (October - May): On-Peak 9:00 AM - 9:00 PM all days
					simplified = append(simplified, touSimplifiedPeriod{
						Year:       year,
						MonthStart: w.monthStart,
						MonthEnd:   w.monthEnd,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
								DollarsPerKWH: amerenMOEveningMorningWinterBaseDollarsPerKWH + amerenMOEveningMorningWinterPeakAdjDollarsPerKWH + riders,
								Description:   "Ameren MO Evening/Morning Savers Winter On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOEveningMorningWinterBaseDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Evening/Morning Savers Winter Off-Peak",
					})
				}

			case "ameren_mo_overnight":
				if w.isSummer {
					// Summer (June - September): On-Peak 6:00 AM - 10:00 PM all days, Off-Peak 10:00 PM - 6:00 AM all days
					simplified = append(simplified, touSimplifiedPeriod{
						Year:       year,
						MonthStart: w.monthStart,
						MonthEnd:   w.monthEnd,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
								DollarsPerKWH: amerenMOOvernightSummerOnPeakDollarsPerKWH + riders,
								Description:   "Ameren MO Overnight Savers Summer On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOOvernightSummerOffPeakDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Overnight Savers Summer Off-Peak",
					})
				} else if isOptionB {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:               year,
						MonthStart:         w.monthStart,
						MonthEnd:           w.monthEnd,
						OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Overnight Savers Option B Winter Anytime Rate",
					})
				} else {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:       year,
						MonthStart: w.monthStart,
						MonthEnd:   w.monthEnd,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
								DollarsPerKWH: amerenMOOvernightWinterOnPeakDollarsPerKWH + riders,
								Description:   "Ameren MO Overnight Savers Winter On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: amerenMOOvernightWinterOffPeakDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Overnight Savers Winter Off-Peak",
					})
				}

			case "ameren_mo_smart_saver":
				if w.isSummer {
					simplified = append(simplified,
						// Summer Holidays (June - September): Intermediate 6 AM - 10 PM, Off-Peak 10 PM - 6 AM
						touSimplifiedPeriod{
							Year:          year,
							MonthStart:    w.monthStart,
							MonthEnd:      w.monthEnd,
							SpecificDates: holidays,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "Intermediate",
									Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
									DollarsPerKWH: amerenMOSmartSaverSummerIntermediateDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Summer Holiday Intermediate",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOSmartSaverSummerOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Smart Savers Summer Holiday Off-Peak",
						},
						// Summer Non-Holidays (June - September):
						// On-Peak 3 PM - 7 PM Mon-Fri; Intermediate 6 AM - 10 PM excl. On-Peak; Off-Peak 10 PM - 6 AM
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: amerenMOSmartSaverSummerOnPeakDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Summer On-Peak",
								},
								{
									Name: "Intermediate",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, HourEnd: 15},
										{HourStart: 19, HourEnd: 22},
									},
									Weekday:       true,
									DollarsPerKWH: amerenMOSmartSaverSummerIntermediateDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Summer Weekday Intermediate",
								},
								{
									Name:          "Intermediate",
									Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
									Weekend:       true,
									DollarsPerKWH: amerenMOSmartSaverSummerIntermediateDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Summer Weekend Intermediate",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOSmartSaverSummerOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Smart Savers Summer Off-Peak",
						},
					)
				} else if isOptionB {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:               year,
						MonthStart:         w.monthStart,
						MonthEnd:           w.monthEnd,
						OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Smart Savers Option B Winter Anytime Rate",
					})
				} else {
					simplified = append(simplified,
						// Winter Holidays (October - May): Intermediate 6 AM - 10 PM, Off-Peak 10 PM - 6 AM
						touSimplifiedPeriod{
							Year:          year,
							MonthStart:    w.monthStart,
							MonthEnd:      w.monthEnd,
							SpecificDates: holidays,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "Intermediate",
									Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
									DollarsPerKWH: amerenMOSmartSaverWinterIntermediateDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Winter Holiday Intermediate",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOSmartSaverWinterOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Smart Savers Winter Holiday Off-Peak",
						},
						// Winter Non-Holidays (October - May):
						// On-Peak 6-8 AM & 6-8 PM Mon-Fri; Intermediate 6 AM - 10 PM excl. On-Peak; Off-Peak 10 PM - 6 AM
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name: "On-Peak",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, HourEnd: 8},
										{HourStart: 18, HourEnd: 20},
									},
									Weekday:       true,
									DollarsPerKWH: amerenMOSmartSaverWinterOnPeakDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Winter On-Peak",
								},
								{
									Name: "Intermediate",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 8, HourEnd: 18},
										{HourStart: 20, HourEnd: 22},
									},
									Weekday:       true,
									DollarsPerKWH: amerenMOSmartSaverWinterIntermediateDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Winter Weekday Intermediate",
								},
								{
									Name:          "Intermediate",
									Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 22}},
									Weekend:       true,
									DollarsPerKWH: amerenMOSmartSaverWinterIntermediateDollarsPerKWH + riders,
									Description:   "Ameren MO Smart Savers Winter Weekend Intermediate",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOSmartSaverWinterOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Smart Savers Winter Off-Peak",
						},
					)
				}

			case "ameren_mo_ultimate_saver":
				if w.isSummer {
					simplified = append(simplified,
						// Summer Holidays (June - September): Off-Peak all day
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOUltimateSaverSummerOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Ultimate Savers Summer Holiday Off-Peak",
						},
						// Summer Non-Holidays (June - September): On-Peak 3 PM - 7 PM Mon-Fri, Off-Peak all other hours
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: amerenMOUltimateSaverSummerOnPeakDollarsPerKWH + riders,
									Description:   "Ameren MO Ultimate Savers Summer On-Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOUltimateSaverSummerOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Ultimate Savers Summer Off-Peak",
						},
					)
				} else if isOptionB {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:               year,
						MonthStart:         w.monthStart,
						MonthEnd:           w.monthEnd,
						OtherDollarsPerKWH: amerenMOAnytimeWinterDollarsPerKWH + riders,
						OtherDescription:   "Ameren MO Ultimate Savers Option B Winter Anytime Rate",
					})
				} else {
					simplified = append(simplified,
						// Winter Holidays (October - May): Off-Peak all day
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOUltimateSaverWinterOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Ultimate Savers Winter Holiday Off-Peak",
						},
						// Winter Non-Holidays (October - May): On-Peak 6-8 AM & 6-8 PM Mon-Fri, Off-Peak all other hours
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name: "On-Peak",
									Hours: []types.UtilityHourPeriod{
										{HourStart: 6, HourEnd: 8},
										{HourStart: 18, HourEnd: 20},
									},
									Weekday:       true,
									DollarsPerKWH: amerenMOUltimateSaverWinterOnPeakDollarsPerKWH + riders,
									Description:   "Ameren MO Ultimate Savers Winter On-Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: amerenMOUltimateSaverWinterOffPeakDollarsPerKWH + riders,
							OtherDescription:   "Ameren MO Ultimate Savers Winter Off-Peak",
						},
					)
				}
			}
		}

		periods = append(periods, buildPeriods(ctLocation, simplified)...)

		if isAvoidedCost {
			periods = append(periods, buildPeriods(ctLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.September,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: amerenMOSummerAvoidedCostDollarsPerKWH,
					OtherDescription:                   "Ameren MO Net Metering Summer Avoided Fuel Cost",
				},
				{
					Year:                               year,
					MonthStart:                         time.October,
					MonthEnd:                           time.May,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: amerenMOWinterAvoidedCostDollarsPerKWH,
					OtherDescription:                   "Ameren MO Net Metering Winter Avoided Fuel Cost",
				},
			})...)
		}
	}

	return periods
}

func amerenMORateInfos() []types.UtilityRateInfo {
	seasonalOption := types.UtilityRateOption{
		Field:       "peakPeriodOption",
		Name:        "Seasonal Option",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Option A applies TOU pricing year-round; Option B applies TOU in summer (June - September) and standard Anytime rates in winter (October - May).",
		Choices: []types.UtilityOptionChoice{
			{Value: "option_a", Name: "Option A (Year-Round TOU)"},
			{Value: "option_b", Name: "Option B (Summer TOU / Winter Anytime)"},
		},
		Default: "option_a",
	}

	monthlyNetMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select 1:1 monthly kWh net metering offset or excess generation Avoided Fuel Cost export credit (Sheet 171).",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Metering (1:1 Monthly kWh Offset)"},
			{Value: "avoided_cost", Name: "Excess Generation Avoided Fuel Cost ($0.0384 Summer / $0.0339 Winter)"},
		},
		Default: "net",
	}

	touBucketNetMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Under Sheet 171, solar exports on Overnight, Smart, and Ultimate Saver plans offset consumption 1:1 within each TOU bucket, with net bucket excess credited at Avoided Fuel Cost.",
		Choices: []types.UtilityOptionChoice{
			{Value: "tou_net", Name: "Net Metering (1:1 TOU Bucket Offset)"},
			{Value: "avoided_cost", Name: "Excess Generation Avoided Fuel Cost ($0.0384 Summer / $0.0339 Winter)"},
		},
		Default: "tou_net",
	}

	return []types.UtilityRateInfo{
		{
			ID:      "ameren_mo_evening_morning",
			Name:    "Evening/Morning Savers (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{monthlyNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_evening_morning", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_overnight",
			Name:    "Overnight Savers (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{seasonalOption, touBucketNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_overnight", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_smart_saver",
			Name:    "Smart Savers TOU (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{seasonalOption, touBucketNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_smart_saver", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_ultimate_saver",
			Name:    "Ultimate Savers Demand TOU (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{seasonalOption, touBucketNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_ultimate_saver", opts, []int{2026, 2027}), nil
			},
		},
		{
			ID:      "ameren_mo_anytime",
			Name:    "Anytime Users (Rate 1M) (Ameren MO)",
			Options: []types.UtilityRateOption{monthlyNetMeteringOption},
			GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
				return amerenMOPeriods("ameren_mo_anytime", opts, []int{2026, 2027}), nil
			},
		},
	}
}
