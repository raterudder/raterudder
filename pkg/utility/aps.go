package utility

// Tariff Audit Metadata
// - Provider(s): aps (Arizona Public Service (APS))
// - Official Sources:
//   - https://www.aps.com/en/Utility/Regulatory-and-Legal/Rates-Schedules-and-Adjustors (APS Rates, Schedules and Adjustors Portal: Schedules R-1, TOU-E, R-3, and R-EV)
//   - https://www.aps.com/en/Utility/Regulatory-and-Legal/Rates-Schedules-and-Adjustors/Bill-Adjustors (APS Bill Adjustors Summary)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Adjustors/PSA1_PowerSupplyAdjustment.pdf?sc_lang=en (Adjustment Schedule PSA-1, A.C.C. No. 6243 Rev. 31: +$0.013977/kWh Jan 2026, +$0.016977/kWh eff. 2026-02-01)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Adjustors/TCA_TransmissionCostAdjustment.pdf?sc_lang=en (Adjustment Schedule TCA-1, A.C.C. No. 6247 Rev. 24: +$0.004395/kWh through May 2026, +$0.005358/kWh eff. 2026-06-01)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Adjustors/LFCR_LostFixedCostRecoveryMechanism.pdf?sc_lang=en (Adjustment Schedule LFCR, A.C.C. No. 6242 Rev. 15: +$0.002460/kWh on R-1, TOU-E, and R-EV; $0.755/kW demand and $0.00/kWh on three-part R-3)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Adjustors/CRS-1.pdf?sc_lang=en (Adjustment Schedule CRS-1, A.C.C. No. 6228 Rev. 1: +$0.001480/kWh)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Adjustors/DSMAC_DemandSideManagementCharge.pdf?sc_lang=en (Adjustment Schedule DSMAC-1, A.C.C. No. 6244 Rev. 15: +$0.000754/kWh eff. 2026-01-01)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Renewable-Plans-and-Riders/rcp_RateSchedule.pdf?sc_lang=en (Rate Rider RCP, A.C.C. No. 6248 Rev. 11: Tranche 2026 $0.05554/kWh eff. 2026-09-01; locked 2017-2025 cohorts)
//   - https://www.aps.com/-/media/APS/APSCOM-PDFs/Utility/Regulatory-and-Legal/Regulatory-Plan-Details-Tariffs/Residential/Frozen-Plans-and-Riders/epr6_Grandfathered.pdf?sc_lang=en (Rate Rider Legacy EPR-6, A.C.C. No. 6194 Rev. 2: grandfathered 1:1 retail net metering)
// - Effective Window: 2026-01-01 through 2027-12-31 (base schedules until superseded; PSA-1 eff. 2026-02-01, TCA-1 eff. 2026-06-01, and RCP Tranche 2026 eff. 2026-09-01 carried through 2027-12-31 until superseded)
// - Revision Cadence: Annual / Periodic (Feb 1 PSA-1, Jun 1 TCA-1, Sep 1 Rate Rider RCP, plus annual LFCR/DSMAC-1/CRS-1 updates)
// - Next Audit Due: 2027-02-01 (Annual PSA-1 Power Supply Adjustment reset)
// - Last Verified: 2026-11-26
// - Modeling Notes: Applies base volumetric energy charges plus mandatory ACC volumetric bill adjustors (PSA-1 + TCA-1 + LFCR + CRS-1 + DSMAC-1: +$0.023066/kWh Jan 2026, +$0.026066/kWh Feb-May 2026, +$0.027029/kWh from 2026-06-01 on R-1, TOU-E, and R-EV; +$0.020606/kWh Jan 2026, +$0.023606/kWh Feb-May 2026, +$0.024569/kWh from 2026-06-01 on three-part R-3 where LFCR is $0.755/kW instead of per-kWh). Excludes fixed daily/monthly basic service charges, per-kW demand charges (R-3 base demand and $0.755/kW LFCR), zero TEAM and SRB-1 riders ($0.000000/kWh), and capped REAC-1 fees ($0.00022/kWh capped at $0.08/month). Default "rcp" solar export credit steps down from $0.06171/kWh (Tranche 2025) to $0.05554/kWh (Tranche 2026) on 2026-09-01, with explicit 10-year locked cohorts "rcp_2026" through "rcp_2017" and grandfathered 1:1 retail net metering ("net") under Legacy EPR-6.

import (
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Schedule R-1 (Fixed Energy Charge Plan) base energy charges ($/kWh)
	apsR1SmallBaseDollarsPerKWH  = 0.12925
	apsR1MediumBaseDollarsPerKWH = 0.14052
	apsR1LargeBaseDollarsPerKWH  = 0.15418

	// Schedule TOU-E (Time-of-Use 4pm-7pm Weekdays) base energy charges ($/kWh)
	apsTOUESummerOnPeakBaseDollarsPerKWH   = 0.34396
	apsTOUESummerOffPeakBaseDollarsPerKWH  = 0.12345
	apsTOUEWinterOnPeakBaseDollarsPerKWH   = 0.32543
	apsTOUEWinterOffPeakBaseDollarsPerKWH  = 0.12351
	apsWinterSuperOffPeakBaseDollarsPerKWH = 0.03495

	// Schedule R-3 (Time-of-Use with Demand Charge) base energy charges ($/kWh)
	apsR3SummerOnPeakBaseDollarsPerKWH  = 0.14227
	apsR3SummerOffPeakBaseDollarsPerKWH = 0.05943
	apsR3WinterOnPeakBaseDollarsPerKWH  = 0.09932
	apsR3WinterOffPeakBaseDollarsPerKWH = 0.05938

	// Schedule R-EV (Electric Vehicle TOU) base energy charges ($/kWh)
	apsREVSummerOnPeakBaseDollarsPerKWH  = 0.36824
	apsREVSummerOffPeakBaseDollarsPerKWH = 0.12345
	apsREVWinterOnPeakBaseDollarsPerKWH  = 0.34820
	apsREVWinterOffPeakBaseDollarsPerKWH = 0.12351
	apsREVOvernightBaseDollarsPerKWH     = 0.08468

	// Mandatory ACC Volumetric Bill Adjustors ($/kWh):
	// - PSA-1 (A.C.C. No. 6243 Rev. 31): +$0.013977/kWh Jan 2026, +$0.016977/kWh eff. 2026-02-01
	// - TCA-1 (A.C.C. No. 6247 Rev. 24): +$0.004395/kWh through May 2026, +$0.005358/kWh eff. 2026-06-01
	// - LFCR  (A.C.C. No. 6242 Rev. 15): +$0.002460/kWh on two-part R-1/TOU-E/R-EV ($0.755/kW & $0.00/kWh on three-part R-3)
	// - CRS-1 (A.C.C. No. 6228 Rev. 1):  +$0.001480/kWh
	// - DSMAC-1 (A.C.C. No. 6244 Rev. 15): +$0.000754/kWh eff. 2026-01-01
	apsPSA1Jan2026DollarsPerKWH    = 0.013977
	apsPSA1Feb2026DollarsPerKWH    = 0.016977
	apsTCA1JanMay2026DollarsPerKWH = 0.004395
	apsTCA1Jun2026DollarsPerKWH    = 0.005358
	apsLFCRTwoPartDollarsPerKWH    = 0.002460
	apsCRS1DollarsPerKWH           = 0.001480
	apsDSMAC1DollarsPerKWH         = 0.000754

	// Combined ACC volumetric adjustors for three-part Schedule R-3 (excludes $0.755/kW LFCR):
	// Jan 2026 = $0.020606/kWh, Feb-May 2026 = $0.023606/kWh, eff. 2026-06-01 = $0.024569/kWh.
	apsR3AdjustorsJan2026DollarsPerKWH    = apsPSA1Jan2026DollarsPerKWH + apsTCA1JanMay2026DollarsPerKWH + apsCRS1DollarsPerKWH + apsDSMAC1DollarsPerKWH
	apsR3AdjustorsFebMay2026DollarsPerKWH = apsPSA1Feb2026DollarsPerKWH + apsTCA1JanMay2026DollarsPerKWH + apsCRS1DollarsPerKWH + apsDSMAC1DollarsPerKWH
	apsR3AdjustorsJun2026DollarsPerKWH    = apsPSA1Feb2026DollarsPerKWH + apsTCA1Jun2026DollarsPerKWH + apsCRS1DollarsPerKWH + apsDSMAC1DollarsPerKWH

	// Combined ACC volumetric adjustors for two-part Schedules R-1, TOU-E, and R-EV (includes LFCR):
	// Jan 2026 = $0.023066/kWh, Feb-May 2026 = $0.026066/kWh, eff. 2026-06-01 = $0.027029/kWh.
	apsTwoPartAdjustorsJan2026DollarsPerKWH    = apsR3AdjustorsJan2026DollarsPerKWH + apsLFCRTwoPartDollarsPerKWH
	apsTwoPartAdjustorsFebMay2026DollarsPerKWH = apsR3AdjustorsFebMay2026DollarsPerKWH + apsLFCRTwoPartDollarsPerKWH
	apsTwoPartAdjustorsJun2026DollarsPerKWH    = apsR3AdjustorsJun2026DollarsPerKWH + apsLFCRTwoPartDollarsPerKWH

	// Rate Rider RCP (A.C.C. No. 6248 Rev. 11) 10-year locked export credit cohorts ($/kWh)
	apsRCP2026DollarsPerKWH = 0.05554 // Tranche 2026 (Sep 1, 2026 - Aug 31, 2027)
	apsRCP2025DollarsPerKWH = 0.06171 // Tranche 2025 (Sep 1, 2025 - Aug 31, 2026)
	apsRCP2024DollarsPerKWH = 0.06857 // Tranche 2024 (Sep 1, 2024 - Aug 31, 2025)
	apsRCP2023DollarsPerKWH = 0.07619 // Tranche 2023 (Sep 1, 2023 - Aug 31, 2024)
	apsRCP2022DollarsPerKWH = 0.08465 // Tranche 2022 (Sep 1, 2022 - Aug 31, 2023)
	apsRCP2021DollarsPerKWH = 0.09405 // Tranche 2021 (Sep 1, 2021 - Aug 31, 2022)
	apsRCP2020DollarsPerKWH = 0.10450 // Tranche 2020 (Sep 1, 2020 - Aug 31, 2021)
	apsRCP2019DollarsPerKWH = 0.10450 // Tranche 2019 (Oct 1, 2019 - Aug 31, 2020)
	apsRCP2018DollarsPerKWH = 0.11610 // Tranche 2018 (Sep 1, 2018 - Sep 30, 2019)
	apsRCP2017DollarsPerKWH = 0.12900 // Tranche 2017 (Sep 1, 2017 - Aug 31, 2018)
)

type apsAdjustorWindow struct {
	monthStart                    time.Month
	monthEnd                      time.Month
	isSummer                      bool
	twoPartAdjustorsDollarsPerKWH float64
	r3AdjustorsDollarsPerKWH      float64
}

// apsAdjustorWindows splits a calendar year into non-wrapping (MonthStart <= MonthEnd)
// seasonal (Summer May-Oct, Winter Nov-Apr) and ACC adjustor windows so buildPeriods
// emits exactly one base period per hour.
func apsAdjustorWindows(year int) []apsAdjustorWindow {
	if year < 2026 {
		return []apsAdjustorWindow{
			{
				monthStart:                    time.January,
				monthEnd:                      time.April,
				isSummer:                      false,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJan2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJan2026DollarsPerKWH,
			},
			{
				monthStart:                    time.May,
				monthEnd:                      time.October,
				isSummer:                      true,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJan2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJan2026DollarsPerKWH,
			},
			{
				monthStart:                    time.November,
				monthEnd:                      time.December,
				isSummer:                      false,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJan2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJan2026DollarsPerKWH,
			},
		}
	}

	if year == 2026 {
		return []apsAdjustorWindow{
			{
				monthStart:                    time.January,
				monthEnd:                      time.January,
				isSummer:                      false,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJan2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJan2026DollarsPerKWH,
			},
			{
				monthStart:                    time.February,
				monthEnd:                      time.April,
				isSummer:                      false,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsFebMay2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsFebMay2026DollarsPerKWH,
			},
			{
				monthStart:                    time.May,
				monthEnd:                      time.May,
				isSummer:                      true,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsFebMay2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsFebMay2026DollarsPerKWH,
			},
			{
				monthStart:                    time.June,
				monthEnd:                      time.October,
				isSummer:                      true,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJun2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJun2026DollarsPerKWH,
			},
			{
				monthStart:                    time.November,
				monthEnd:                      time.December,
				isSummer:                      false,
				twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJun2026DollarsPerKWH,
				r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJun2026DollarsPerKWH,
			},
		}
	}

	return []apsAdjustorWindow{
		{
			monthStart:                    time.January,
			monthEnd:                      time.April,
			isSummer:                      false,
			twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJun2026DollarsPerKWH,
			r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJun2026DollarsPerKWH,
		},
		{
			monthStart:                    time.May,
			monthEnd:                      time.October,
			isSummer:                      true,
			twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJun2026DollarsPerKWH,
			r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJun2026DollarsPerKWH,
		},
		{
			monthStart:                    time.November,
			monthEnd:                      time.December,
			isSummer:                      false,
			twoPartAdjustorsDollarsPerKWH: apsTwoPartAdjustorsJun2026DollarsPerKWH,
			r3AdjustorsDollarsPerKWH:      apsR3AdjustorsJun2026DollarsPerKWH,
		},
	}
}

func apsLockedRCPExportDollarsPerKWH(scheme string) (float64, bool) {
	switch scheme {
	case "rcp_2026":
		return apsRCP2026DollarsPerKWH, true
	case "rcp_2025":
		return apsRCP2025DollarsPerKWH, true
	case "rcp_2024":
		return apsRCP2024DollarsPerKWH, true
	case "rcp_2023":
		return apsRCP2023DollarsPerKWH, true
	case "rcp_2022":
		return apsRCP2022DollarsPerKWH, true
	case "rcp_2021":
		return apsRCP2021DollarsPerKWH, true
	case "rcp_2020":
		return apsRCP2020DollarsPerKWH, true
	case "rcp_2019":
		return apsRCP2019DollarsPerKWH, true
	case "rcp_2018":
		return apsRCP2018DollarsPerKWH, true
	case "rcp_2017":
		return apsRCP2017DollarsPerKWH, true
	default:
		return 0, false
	}
}

func apsExportSimplifiedPeriods(year int, options types.UtilityRateOptions) []touSimplifiedPeriod {
	scheme := strings.ToLower(strings.TrimSpace(options.NetMeteringScheme))
	switch scheme {
	case "net", "nem", "nem1":
		return nil
	}

	if lockedDollarsPerKWH, ok := apsLockedRCPExportDollarsPerKWH(scheme); ok {
		return []touSimplifiedPeriod{
			{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: lockedDollarsPerKWH,
				OtherDescription:                   "APS RCP Net Billing Export Credit",
			},
		}
	}

	// Default "rcp" (or empty): $0.06171/kWh through August 2026, stepping to Tranche 2026
	// $0.05554/kWh effective September 1, 2026 through 2027.
	if year < 2026 {
		return []touSimplifiedPeriod{
			{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.December,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: apsRCP2025DollarsPerKWH,
				OtherDescription:                   "APS RCP Net Billing Export Credit",
			},
		}
	}
	if year == 2026 {
		return []touSimplifiedPeriod{
			{
				Year:                               year,
				MonthStart:                         time.January,
				MonthEnd:                           time.August,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: apsRCP2025DollarsPerKWH,
				OtherDescription:                   "APS RCP Net Billing Export Credit",
			},
			{
				Year:                               year,
				MonthStart:                         time.September,
				MonthEnd:                           time.December,
				OnlySeparateGenerationCredit:       true,
				OtherGenerationCreditDollarsPerKWH: apsRCP2026DollarsPerKWH,
				OtherDescription:                   "APS RCP Net Billing Export Credit",
			},
		}
	}
	return []touSimplifiedPeriod{
		{
			Year:                               year,
			MonthStart:                         time.January,
			MonthEnd:                           time.December,
			OnlySeparateGenerationCredit:       true,
			OtherGenerationCreditDollarsPerKWH: apsRCP2026DollarsPerKWH,
			OtherDescription:                   "APS RCP Net Billing Export Credit",
		},
	}
}

func shiftAPSWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getAPSHolidays(year int) []string {
	shifted := []time.Time{
		newYearsDay(year),
		time.Date(year, time.March, 31, 0, 0, 0, 0, time.UTC), // Cesar Chavez Day
		juneteenth(year),
		independenceDay(year),
		veteransDay(year),
		christmasDay(year),
		newYearsDay(year + 1),
	}

	var holidays []time.Time
	for _, h := range shifted {
		holidays = append(holidays, shiftAPSWeekendHoliday(h))
	}

	holidays = append(holidays,
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		laborDay(year),
		columbusDay(year),
		thanksgivingDay(year),
		christmasEve(year),
		newYearsEve(year),
	)

	return formatHolidays(holidays, year)
}

func apsPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	rateClass := strings.ToLower(strings.TrimSpace(options.RateClass))

	r1BaseDollarsPerKWH := apsR1SmallBaseDollarsPerKWH // small/default
	switch rateClass {
	case "medium":
		r1BaseDollarsPerKWH = apsR1MediumBaseDollarsPerKWH
	case "large":
		r1BaseDollarsPerKWH = apsR1LargeBaseDollarsPerKWH
	}

	overnightHours := []types.UtilityHourPeriod{
		{HourStart: 23, HourEnd: 24},
		{HourStart: 0, HourEnd: 5},
	}

	for _, year := range years {
		holidays := getAPSHolidays(year)
		windows := apsAdjustorWindows(year)
		var simplified []touSimplifiedPeriod

		for _, w := range windows {
			twoPartAdj := w.twoPartAdjustorsDollarsPerKWH
			r3Adj := w.r3AdjustorsDollarsPerKWH

			switch plan {
			case "aps_r_1":
				// Schedule R-1 (Fixed Energy Charge Plan): keep Name empty so GetPeriods returns empty slice.
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         w.monthStart,
					MonthEnd:           w.monthEnd,
					OtherDollarsPerKWH: r1BaseDollarsPerKWH + twoPartAdj,
					OtherDescription:   "APS Schedule R-1 Consumption Charge",
				})

			case "aps_tou_e":
				// Schedule TOU-E (Time-of-Use 4pm-7pm Weekdays)
				if w.isSummer {
					simplified = append(simplified,
						// Summer Holidays (Off-Peak all day)
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsTOUESummerOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Summer Holiday Off-Peak",
						},
						// Summer Regular
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: apsTOUESummerOnPeakBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Summer TOU On-Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsTOUESummerOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Summer TOU Off-Peak",
						},
					)
				} else {
					simplified = append(simplified,
						// Winter Holidays (Off-Peak all day)
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsTOUEWinterOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Winter Holiday Off-Peak",
						},
						// Winter Regular
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: apsTOUEWinterOnPeakBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Winter TOU On-Peak",
								},
								{
									Name:          "Super Off-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 15}},
									Weekday:       true,
									DollarsPerKWH: apsWinterSuperOffPeakBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Winter TOU Super Off-Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsTOUEWinterOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Winter TOU Off-Peak",
						},
					)
				}

			case "aps_r_3":
				// Schedule R-3 (Time-of-Use with Demand Charge; LFCR is per-kW so r3Adj excludes LFCR)
				if w.isSummer {
					simplified = append(simplified,
						// Summer Holidays
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsR3SummerOffPeakBaseDollarsPerKWH + r3Adj,
							OtherDescription:   "APS Summer Holiday Off-Peak",
						},
						// Summer Regular
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: apsR3SummerOnPeakBaseDollarsPerKWH + r3Adj,
									Description:   "APS Summer TOU On-Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsR3SummerOffPeakBaseDollarsPerKWH + r3Adj,
							OtherDescription:   "APS Summer TOU Off-Peak",
						},
					)
				} else {
					simplified = append(simplified,
						// Winter Holidays
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsR3WinterOffPeakBaseDollarsPerKWH + r3Adj,
							OtherDescription:   "APS Winter Holiday Off-Peak",
						},
						// Winter Regular
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: apsR3WinterOnPeakBaseDollarsPerKWH + r3Adj,
									Description:   "APS Winter TOU On-Peak",
								},
								{
									Name:          "Super Off-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 15}},
									Weekday:       true,
									DollarsPerKWH: apsWinterSuperOffPeakBaseDollarsPerKWH + r3Adj,
									Description:   "APS Winter TOU Super Off-Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsR3WinterOffPeakBaseDollarsPerKWH + r3Adj,
							OtherDescription:   "APS Winter TOU Off-Peak",
						},
					)
				}

			case "aps_r_ev":
				// Schedule R-EV (Electric Vehicle TOU)
				if w.isSummer {
					simplified = append(simplified,
						// Summer Holidays
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsREVSummerOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Summer Holiday Off-Peak",
						},
						// Summer Regular
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: apsREVSummerOnPeakBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Summer TOU On-Peak",
								},
								{
									Name:          "Overnight",
									Hours:         overnightHours,
									Weekday:       true,
									DollarsPerKWH: apsREVOvernightBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Summer TOU Overnight",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsREVSummerOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Summer TOU Off-Peak",
						},
					)
				} else {
					simplified = append(simplified,
						// Winter Holidays
						touSimplifiedPeriod{
							Year:               year,
							MonthStart:         w.monthStart,
							MonthEnd:           w.monthEnd,
							SpecificDates:      holidays,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsREVWinterOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Winter Holiday Off-Peak",
						},
						// Winter Regular
						touSimplifiedPeriod{
							Year:             year,
							MonthStart:       w.monthStart,
							MonthEnd:         w.monthEnd,
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
									Weekday:       true,
									DollarsPerKWH: apsREVWinterOnPeakBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Winter TOU On-Peak",
								},
								{
									Name:          "Super Off-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 15}},
									Weekday:       true,
									DollarsPerKWH: apsWinterSuperOffPeakBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Winter TOU Super Off-Peak",
								},
								{
									Name:          "Overnight",
									Hours:         overnightHours,
									Weekday:       true,
									DollarsPerKWH: apsREVOvernightBaseDollarsPerKWH + twoPartAdj,
									Description:   "APS Winter TOU Overnight",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: apsREVWinterOffPeakBaseDollarsPerKWH + twoPartAdj,
							OtherDescription:   "APS Winter TOU Off-Peak",
						},
					)
				}
			}
		}

		periods = append(periods, buildPeriods(mstLocation, simplified)...)

		if exportSimplified := apsExportSimplifiedPeriods(year, options); len(exportSimplified) > 0 {
			periods = append(periods, buildPeriods(mstLocation, exportSimplified)...)
		}
	}

	return periods
}

func apsUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your Rate Rider RCP 10-year locked solar export cohort or grandfathered EPR-6 Net Metering.",
		Choices: []types.UtilityOptionChoice{
			{Value: "rcp", Name: "Rate Rider RCP Default ($0.06171 through Aug 2026, $0.05554/kWh from Sep 2026)"},
			{Value: "rcp_2026", Name: "Rate Rider RCP Tranche 2026: Sep 2026 - Aug 2027 ($0.05554/kWh)"},
			{Value: "rcp_2025", Name: "Rate Rider RCP Tranche 2025: Sep 2025 - Aug 2026 ($0.06171/kWh)"},
			{Value: "rcp_2024", Name: "Rate Rider RCP Tranche 2024: Sep 2024 - Aug 2025 ($0.06857/kWh)"},
			{Value: "rcp_2023", Name: "Rate Rider RCP Tranche 2023: Sep 2023 - Aug 2024 ($0.07619/kWh)"},
			{Value: "rcp_2022", Name: "Rate Rider RCP Tranche 2022: Sep 2022 - Aug 2023 ($0.08465/kWh)"},
			{Value: "rcp_2021", Name: "Rate Rider RCP Tranche 2021: Sep 2021 - Aug 2022 ($0.09405/kWh)"},
			{Value: "rcp_2020", Name: "Rate Rider RCP Tranche 2020: Sep 2020 - Aug 2021 ($0.10450/kWh)"},
			{Value: "rcp_2019", Name: "Rate Rider RCP Tranche 2019: Oct 2019 - Aug 2020 ($0.10450/kWh)"},
			{Value: "rcp_2018", Name: "Rate Rider RCP Tranche 2018: Sep 2018 - Sep 2019 ($0.11610/kWh)"},
			{Value: "rcp_2017", Name: "Rate Rider RCP Tranche 2017: Sep 2017 - Aug 2018 ($0.12900/kWh)"},
			{Value: "net", Name: "Net Energy Metering (Legacy EPR-6 1:1 Retail Credits)"},
		},
		Default: "rcp",
	}

	rateClassOption := types.UtilityRateOption{
		Field:       "rateClass",
		Name:        "Usage Tier / Rate Class",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your average monthly consumption tier.",
		Choices: []types.UtilityOptionChoice{
			{Value: "small", Name: "XS / Small (<= 600 kWh average monthly usage)"},
			{Value: "medium", Name: "Medium (601 - 999 kWh average monthly usage)"},
			{Value: "large", Name: "Large (>= 1000 kWh average monthly usage)"},
		},
		Default: "small",
	}

	return types.UtilityProviderInfo{
		ID:   "aps",
		Name: "Arizona Public Service (APS)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "aps_r_1",
				Name:    "Schedule R-1 (Fixed Energy Charge Plan)",
				Options: []types.UtilityRateOption{rateClassOption, exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_r_1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "aps_tou_e",
				Name:    "Schedule TOU-E (Time-of-Use 4pm-7pm Weekdays)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_tou_e", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "aps_r_3",
				Name:    "Schedule R-3 (Time-of-Use with Demand Charge)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_r_3", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "aps_r_ev",
				Name:    "Schedule R-EV (Electric Vehicle TOU)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return apsPeriods("aps_r_ev", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
