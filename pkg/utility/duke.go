package utility

// Tariff Audit Metadata
// - Provider(s): duke_carolinas_nc (Duke Energy Carolinas (North Carolina)), duke_carolinas_sc (Duke Energy Carolinas (South Carolina)), duke_progress_nc (Duke Energy Progress (North Carolina)), duke_progress_sc (Duke Energy Progress (South Carolina)), duke_indiana (Duke Energy Indiana), duke_florida (Duke Energy Florida)
// - Official Sources:
//   - DEC NC: https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-nc/ncridersummary.pdf?rev=9740307e8ae742b79e935d170582e235 (69th Rev. Leaf No. 99 Summary of Riders, $0.019256/kWh eff. 2026-09-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-nc/ncridersts.pdf?rev=535c015809e149e4b485e3921ac20f95 (13th Rev. Leaf No. 133 Rider STS, $0.000433/kWh eff. 2026-07-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-nc/dec-nc-rider-sts.pdf?rev=ea834be170ed476599c124fbba9ccf44 (1st Rev. Leaf No. 119 Rider STS-2, $0.001375/kWh eff. 2026-07-01; Schedules RS, RE, RT, RT-EV, RSTC, RETC, Rider RSC, Rider NMB, & Schedule PP)
//   - DEC SC: https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-sc/scschedulers.pdf?rev=8f2bb99764f646a193294d87ed187bd4 (Leaf No. 11 Schedule RS per PSCSC Order No. 2026-636, +$0.005799/kWh fuel step eff. 2026-11-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-sc/scschedulertev.pdf?rev=2078b734d3ec4f3d8dc591ce158e23b4 (SC 1st Rev. Leaf No. 12 Schedule RT-EV eff. 2026-11-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-sc/scridersummary.pdf?rev=eb46412a1b304fe1971b8e86573250fe (Leaf No. 99 Summary of Riders, $0.002504/kWh eff. 2026-08-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-sc/dec-recovery-sts.pdf?rev=c66661847c5e412ba6c11d4d7023a46f (Leaf No. 101 Recovery STS-1, $0.004670/kWh eff. 2026-09-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-sc/scriderrnm.pdf?rev=1c2c5458b92c4d7cabf666a0ad480e49 (Leaf No. 119 Rider RNM 1:1 net metering), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-sc/dec-sc-rider-nmt.pdf?rev=2fdab92a46cc41a3a670c6eb9b8dad99 (SC 1st Rev. Leaf No. 138 Rider NMT, $0.0419/kWh eff. 2026-08-01; Schedules RE, RT, R-STOU, RSTC, RETC, & Rider RSC)
//   - DEP NC: https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-600-summary-of-riders.pdf?rev=bbf8e3e94560464f914b3c8b1465865a (10th Rev. Leaf No. 600 Summary of Riders, $0.03144/kWh eff. 2026-06-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-607-rider-sts-ry1.pdf?rev=5e8e13dcee03418f9e508d15e0842b5d (Leaf No. 607 Rider STS-RY1 eff. 2026-07-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-613-rider-sts.pdf?rev=59650b94d3b74b428388a9227b5ccf61 (Leaf No. 613 Rider STS eff. 2026-07-01, raising total riders to $0.03481/kWh), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-501-schedule-r-toud.pdf?rev=069d90482702418bbb7964e185c750b9 (NC 2nd Rev. Leaf No. 501 Schedule R-TOUD), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-504-schedule-r-tou-ev.pdf?rev=56e484c535db4ac199816565472a8adb (NC 1st Rev. Leaf No. 504 Schedule R-TOU-EV), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-670-rider-rsc-ry1.pdf?rev=b0d0c9dcca614d70ab62738d8957f5ca (Leaf No. 670 Rider RSC, $0.0394/kWh), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-nc/leaf-no-669-rider-nmb-ry1.pdf?rev=d4d6d5b9b1da4e44a5559507a32981c4 (Leaf No. 605 / 669 Rider NMB, $0.0394/kWh; Schedules RES, R-TOU, R-TOU-CPP, & Schedule PP)
//   - DEP SC: https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-500-schedule-res.pdf?rev=d9c7d7e697c944ecb38832b3c0c5e69c (Leaf No. 500 Schedule RES per PSCSC Order No. 2026-417, +$0.00629/kWh fuel eff. 2026-08-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-600-rider-summary.pdf?rev=bca0370f301848b0a940204815f5678c (Leaf No. 600 Rider Summary, $0.00335/kWh eff. 2026-08-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-667-rider-sts.pdf?rev=f4ef2dfcf2874ea089d09b8bb8fc27e8 (Leaf No. 667 Recovery STS, $0.00458/kWh eff. 2026-09-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-501-schedule-r-toud.pdf?rev=f94d082aa4354e61a629e4c5fdc3f840 (SC 6th Rev. Leaf No. 501 Schedule R-TOUD), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-502-schedule-r-stou.pdf?rev=1f161c254a7b4d72b257663a5f81bbf1 (SC 10th Rev. Leaf No. 502 Schedule R-STOU), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-503-schedule-r-tou-cpp.pdf?rev=e9d10212166c4c3baeea9690d2e5ece0 (SC 6th Rev. Leaf No. 503 Schedule R-TOU-CPP), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-663-rider-rsc.pdf?rev=beefea4cde824554a7461e128fbdbfc9 (Leaf No. 663 Rider RSC, $0.0401/kWh), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/dep-sc/leaf-no-590-schedule-pp.pdf?rev=6f7faec791b742fc8419e8df1c0bf733 (Leaf No. 590 Schedule PP, $0.0337/kWh Shoulder Off-Peak less $0.00162/kWh integration charge; Schedule R-TOU-EV)
//   - DEI: https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-in/iurc-16/tariff-no-06-rate-rs-residential-electric-svc.pdf?rev=851baa8e27684488a2b1a9a61c7f8db1 (IURC No. 16 Tariff No. 6 Rate RS), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-in/iurc-16/tariff-no-06-5-optional-rate-rs-time-of-use-svc.pdf?rev=fa906d696d7843d1990beaab3094b5a2 (Tariff No. 6.5 Optional Rate RS-TOU), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-in/iurc-16/tariff-no-54-excess-distributed-generation.pdf?rev=0771f91d75a04efe80923ff8774fe493 (Tariff No. 54 Rider EDG, $0.055143/kWh), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/electric-in/iurc-16/2026-quarterly-rate-adjustment-trackers.pdf?rev=06267b91c8684f65a95dddd4d6af8ca2 (Q4 2026 Quarterly Rate Adjustment Trackers, $0.006072/kWh eff. 2026-11-01)
//   - DEF: https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/rates-fl/pe-rates-ba-1.pdf?rev=e31e4437dfb04fb58d72435b7e4d1640 (115th Rev. Sheet No. 6.105 Schedule BA-1 eff. 2026-10-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/rates-fl/pe-rates-rs-1.pdf?rev=cd5147a614994cf3971d9922748c9938 (47th Rev. Sheet No. 6.120 Schedule RS-1 eff. 2026-10-01), https://www.duke-energy.com/-/media/pdfs/for-your-home/rates/rates-fl/pe-rates-rst-1.pdf?rev=e1ebbcfe64ab4b77abbd457f1861ddea (41st Rev. Sheet No. 6.140 Schedule RST-1 eff. 2026-10-01), https://www.floridapsc.com/pscfiles/library/Orders/2026/08010-2026.pdf (Order No. PSC-2026-0416-FOF-EI), https://www.floridapsc.com/pscfiles/library/Orders/2026/08045-2026.pdf (Order No. PSC-2026-0427-FOF-EG), https://www.floridapsc.com/pscfiles/library/Orders/2026/07950-2026.pdf (Order No. PSC-2026-0407-FOF-EI), https://www.floridapsc.com/pscfiles/library/Orders/2026/07101-2026.pdf (Order No. PSC-2026-0349-FOF-EI, 2027 BA-1 clauses eff. 2027-01-01)
// - Effective Window: 2025-01-01 through 2028-12-31 for duke_florida ([]int{2025, 2026, 2027, 2028}); 2026-01-01 through 2027-12-31 for duke_carolinas_nc, duke_carolinas_sc, duke_progress_nc, duke_progress_sc, and duke_indiana ([]int{2026, 2027} until superseded)
// - Revision Cadence: Quarterly (DEI IURC trackers Feb/May/Aug/Nov) & Semi-Annual / Annual (Jan 1 FPSC BA-1 clauses & NCUC MYRP/riders; Jun/Jul/Aug/Sep/Nov NCUC & PSCSC fuel, rider, and storm securitization updates; Oct 1 DEF SoBRA base steps)
// - Next Audit Due: 2027-01-05 (Check final 2027 NCUC compliance leaves following the 2026-11-16 NCUC settlement, Q1 2027 DEI quarterly trackers, and Jan 2027 DEC/DEP rider filings)
// - Last Verified: 2026-11-28
// - Modeling Notes: Models Tier 1 volumetric energy charges (first-block rates on DEC NC/SC Schedule RE, DEC SC Schedule RS, DEI Rate RS, and DEF Schedule RS-1 <=1,000 kWh/mo) plus verified mandatory volumetric riders, storm securitization charges (STS), fuel adjustments, and FPSC BA-1 clauses while preserving pre-effective 2026 historical windows intact. Excludes fixed monthly customer/REPS/CEPS/DERP charges, three-phase adders, per-kW demand fees (DEP NC & DEP SC Schedule R-TOUD On-Peak and Max kW billing demands), per-kW solar non-bypassable/grid-access/minimum-bill fees, unscheduled Critical Peak event dispatches, and statutory sales/gross-receipts/franchise taxes. Retains verified 2026 NC leaves until final 2027 NCUC settlement compliance leaves publish. Models solar exports under Rider RSC, Rider NMB, Schedule PP (SCG), Rider RNM (1:1 net metering), Rider NMT, Tariff 54 EDG, and 1:1 net metering in IN and FL.

import (
	"math"
	"slices"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Duke Energy Carolinas (DEC) and Progress (DEP) Solar Export Credit Rates ($/kWh)
const (
	dukeRSCNCCredit             = 0.0453
	depNCRSCCreditDollarsPerKWH = 0.0394
	depNCNMBCreditDollarsPerKWH = 0.0394
	dukeRSCSCCredit             = 0.0419
	decSCNMTCreditDollarsPerKWH = 0.0419
	depSCRSCCreditDollarsPerKWH = 0.0401
)

// Duke Energy Mandatory Volumetric Riders, Fuel Steps, Trackers, and Clause Adjustments ($/kWh)
const (
	// DEC NC: Leaf 99 ($0.019256 eff. 2026-09-01) + Leaf 133 STS ($0.000433) + Leaf 119 STS-2 ($0.001375)
	decNC2026SepRidersDollarsPerKWH = 0.021064

	// DEC SC: Leaf 99 ($0.002504 eff. 2026-08-01), + Leaf 101 STS-1 ($0.004670 eff. 2026-09-01 => $0.007174),
	// + Leaf 11/12 Order 2026-636 fuel step ($0.005799 eff. 2026-11-01 => $0.012973)
	decSC2026AugRidersDollarsPerKWH        = 0.002504
	decSC2026SepRidersDollarsPerKWH        = 0.007174
	decSC2026NovFuelAndRidersDollarsPerKWH = 0.012973

	// DEP NC: Leaf 600 ($0.03144 eff. 2026-06-01), + Leaf 607 STS-RY1 & Leaf 613 STS (eff. 2026-07-01 => $0.03481)
	depNC2026JunRidersDollarsPerKWH = 0.03144
	depNC2026JulRidersDollarsPerKWH = 0.03481

	// DEP SC: Leaf 500 fuel ($0.00629) + Leaf 600 riders ($0.00335) eff. 2026-08-01 ($0.00964),
	// + Leaf 667 Rider STS ($0.00458 eff. 2026-09-01 => $0.01422)
	depSC2026AugFuelAndRidersDollarsPerKWH = 0.00964
	depSC2026SepFuelAndRidersDollarsPerKWH = 0.01422

	// DEI: Q4 2026 Quarterly Rate Adjustment Trackers eff. 2026-11-01
	dei2026NovTrackersDollarsPerKWH = 0.006072

	// DEF: Sheet 6.120 RS-1 & Sheet 6.140 RST-1 base energy charges eff. 2026-10-01
	defOct2026RS1WinterBaseDollarsPerKWH    = 0.08805
	defOct2026RS1NonWinterBaseDollarsPerKWH = 0.07731
	defOct2026RST1OnPeakBaseDollarsPerKWH   = 0.11155
	defOct2026RST1OffPeakBaseDollarsPerKWH  = 0.08263
	defOct2026RST1DiscountBaseDollarsPerKWH = 0.05013
	def2026BA1RS1DollarsPerKWH              = 0.05860
	def2026BA1RST1OnPeakDollarsPerKWH       = 0.06770
	def2026BA1RST1OffPeakDollarsPerKWH      = 0.06120
	def2026BA1RST1DiscountDollarsPerKWH     = 0.05788
	def2027BA1RS1DollarsPerKWH              = 0.05767
	def2027BA1RST1OnPeakDollarsPerKWH       = 0.07361
	def2027BA1RST1OffPeakDollarsPerKWH      = 0.06000
	def2027BA1RST1DiscountDollarsPerKWH     = 0.05251
)

// Duke Energy Carolinas NC Avoided Cost Schedule PP Rates (Uncontrolled Solar, 2-Year Fixed, Interconnected to Distribution)
const (
	decNCPPSummerOnPeak      = 0.0479
	decNCPPWinterMorningPeak = 0.0457
	decNCPPWinterEveningPeak = 0.0476
	decNCPPPremiumSummer     = 0.0615
	decNCPPPremiumWinter     = 0.0605
	decNCPPShoulderOnPeak    = 0.0472
	decNCPPSummerOffPeak     = 0.0414
	decNCPPWinterOffPeak     = 0.0418
	decNCPPShoulderOffPeak   = 0.0391
)

// Duke Energy Progress SC Avoided Cost Schedule PP Rates (Leaf No. 590)
// Note: Billed monthly exports are reduced by the Solar Integration Services Charge ($0.00162 per kWh)
const (
	depSCIntegrationCharge = 0.00162

	depSCPPSummerOnPeak      = 0.0532 - depSCIntegrationCharge
	depSCPPWinterMorningPeak = 0.0491 - depSCIntegrationCharge
	depSCPPWinterEveningPeak = 0.0578 - depSCIntegrationCharge
	depSCPPPremiumSummer     = 0.0749 - depSCIntegrationCharge
	depSCPPPremiumWinter     = 0.0675 - depSCIntegrationCharge
	depSCPPShoulderOnPeak    = 0.0426 - depSCIntegrationCharge
	depSCPPSummerOffPeak     = 0.0364 - depSCIntegrationCharge
	depSCPPWinterOffPeak     = 0.0434 - depSCIntegrationCharge
	depSCPPShoulderOffPeak   = 0.0337 - depSCIntegrationCharge
)

// Duke Energy Progress NC Avoided Cost Schedule PP Rates (Uncontrolled Solar, 2-Year Fixed, Interconnected to Distribution)
const (
	depNCPPSummerOnPeak      = 0.0464
	depNCPPWinterMorningPeak = 0.0407
	depNCPPWinterEveningPeak = 0.0452
	depNCPPPremiumSummer     = 0.0597
	depNCPPPremiumWinter     = 0.0590
	depNCPPShoulderOnPeak    = 0.0424
	depNCPPSummerOffPeak     = 0.0401
	depNCPPWinterOffPeak     = 0.0407
	depNCPPShoulderOffPeak   = 0.0382
)

// Duke Energy Indiana (DEI) Credit Rates
const (
	dukeIndianaEDGCredit = 0.055143
)

type dukeEffectiveWindow struct {
	start              time.Time
	end                time.Time
	adderDollarsPerKWH float64
}

func roundDukeDollarsPerKWH(val float64) float64 {
	return math.Round(val*1e6) / 1e6
}

func clipDukePeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
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

// shiftDukeWeekendHoliday shifts a holiday to Friday if it falls on a Saturday,
// or to Monday if it falls on a Sunday.
func shiftDukeWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getDukeHolidays returns Duke's holiday calendar for Carolinas/Progress (NC/SC)
func getDukeHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)

	holidays := []time.Time{
		shiftDukeWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftDukeWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		shiftDukeWeekendHoliday(christmasDay(year)),
		shiftDukeWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// getIndianaHolidays returns Duke Energy Indiana's holiday list.
// Holidays are: New Year's Day, Memorial Day, Independence Day, Labor Day, Thanksgiving Day, Christmas Day.
func getIndianaHolidays(year int) []string {
	holidays := []time.Time{
		shiftDukeWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftDukeWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftDukeWeekendHoliday(christmasDay(year)),
		shiftDukeWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// buildDukeTOUPeriods generates the standard On-Peak/Discount/Off-Peak periods
// used by DEC NC/SC RT, RSTC, RETC, and DEP NC R-TOU/R-TOU-CPP.
func buildDukeTOUPeriods(year int, holidays []string, onPeakRate, offPeakRate, discountRate float64) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// Summer: May - September (Months 5 - 9)
		{
			Year:          year,
			MonthStart:    time.May,
			MonthEnd:      time.September,
			SpecificDates: holidays,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "Discount",
					// Discount period applies to holidays too
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 6},
					},
					DollarsPerKWH: discountRate,
					Description:   "Summer Holiday Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Summer Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.May,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "On-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 21},
					},
					Weekday:       true,
					DollarsPerKWH: onPeakRate,
					Description:   "Summer Weekday On-Peak",
				},
				{
					Name: "Discount",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 6},
					},
					DollarsPerKWH: discountRate,
					Description:   "Summer Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Summer Off-Peak",
		},

		// Non-Summer: October - April (Months 10 - 4)
		{
			Year:          year,
			MonthStart:    time.October,
			MonthEnd:      time.April,
			SpecificDates: holidays,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "Discount",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 3},
						{HourStart: 11, HourEnd: 16},
					},
					DollarsPerKWH: discountRate,
					Description:   "Winter Holiday Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Winter Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.October,
			MonthEnd:         time.April,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "On-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:       true,
					DollarsPerKWH: onPeakRate,
					Description:   "Winter Weekday On-Peak",
				},
				{
					Name: "Discount",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 1, HourEnd: 3},
						{HourStart: 11, HourEnd: 16},
					},
					DollarsPerKWH: discountRate,
					Description:   "Winter Discount",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   "Winter Off-Peak",
		},
	}
}

// buildDukeEVPeriods generates the daily 23:00-05:00 Super Off-Peak and Off-Peak periods
// used by DEC NC/SC RT-EV and DEP NC/SC R-TOU-EV.
func buildDukeEVPeriods(year int, superOffPeakDollarsPerKWH, offPeakDollarsPerKWH float64) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		{
			Year:       year,
			MonthStart: time.January,
			MonthEnd:   time.December,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "Super Off-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 23, HourEnd: 24},
						{HourStart: 0, HourEnd: 5},
					},
					DollarsPerKWH: superOffPeakDollarsPerKWH,
					Description:   "Discount Charging Period",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Standard Charging Period",
		},
	}
}

// buildDukeSTOUPeriods generates the Solar TOU (Schedule R-STOU) periods
// used by DEC SC and DEP SC.
func buildDukeSTOUPeriods(year int, holidays []string, onPeakDollarsPerKWH, offPeakDollarsPerKWH, superOffPeakDollarsPerKWH float64) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// Non-Winter Months (March - November)
		{
			Year:          year,
			MonthStart:    time.March,
			MonthEnd:      time.November,
			SpecificDates: holidays,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "Super Off-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 6},
					},
					DollarsPerKWH: superOffPeakDollarsPerKWH,
					Description:   "Non-Winter Holiday Super Off-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Non-Winter Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.March,
			MonthEnd:         time.November,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "On-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 21},
					},
					Weekday:       true,
					DollarsPerKWH: onPeakDollarsPerKWH,
					Description:   "Non-Winter Weekday On-Peak",
				},
				{
					Name: "Super Off-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 6},
					},
					DollarsPerKWH: superOffPeakDollarsPerKWH,
					Description:   "Non-Winter Super Off-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Non-Winter Off-Peak",
		},

		// Winter Months (December - February)
		{
			Year:               year,
			MonthStart:         time.December,
			MonthEnd:           time.February,
			SpecificDates:      holidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Winter Holiday Off-Peak",
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name: "On-Peak",
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
						{HourStart: 18, HourEnd: 21},
					},
					Weekday:       true,
					DollarsPerKWH: onPeakDollarsPerKWH,
					Description:   "Winter Weekday On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakDollarsPerKWH,
			OtherDescription:   "Winter Off-Peak",
		},
	}
}

func dukeCarolinasNCWindows(year int) []dukeEffectiveWindow {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)
	if year < 2026 {
		return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: 0}}
	}
	if year == 2026 {
		sep1 := time.Date(2026, time.September, 1, 0, 0, 0, 0, etLocation)
		return []dukeEffectiveWindow{
			{start: yearStart, end: sep1, adderDollarsPerKWH: 0},
			{start: sep1, end: yearEnd, adderDollarsPerKWH: decNC2026SepRidersDollarsPerKWH},
		}
	}
	return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: decNC2026SepRidersDollarsPerKWH}}
}

// dukeCarolinasNCPeriods returns pricing periods for Duke Energy Carolinas (North Carolina)
func dukeCarolinasNCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)
		for _, w := range dukeCarolinasNCWindows(year) {
			add := func(base float64) float64 {
				return roundDukeDollarsPerKWH(base + w.adderDollarsPerKWH)
			}
			var raw []types.UtilityFeesPeriod

			switch plan {
			case "duke_carolinas_nc_rs":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: add(0.122603),
						OtherDescription:   "Residential Flat Rate",
					},
				})

			case "duke_carolinas_nc_re":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: add(0.117845), // using first-tier rate as flat estimate
						OtherDescription:   "Residential Space Conditioning & Water Heating",
					},
				})

			case "duke_carolinas_nc_rt":
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.171204), add(0.078411), add(0.053929)))

			case "duke_carolinas_nc_rt_ev":
				raw = buildPeriods(etLocation, buildDukeEVPeriods(year, add(0.061752), add(0.123504)))

			case "duke_carolinas_nc_rstc":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.234984), add(0.102875), add(0.074375)))

			case "duke_carolinas_nc_retc":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.213412), add(0.097428), add(0.070480)))
			}

			periods = append(periods, clipDukePeriods(raw, w.start, w.end)...)
		}
	}

	// Solar Export Credits
	switch options.NetMeteringScheme {
	case "rsc", "":
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCNCCredit, "Solar Choice Net Excess Credit")...)
	case "nmb":
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCNCCredit, "Net Metering Bridge Excess Credit")...)
	case "scg":
		for _, year := range years {
			holidays := getDukeHolidays(year)
			periods = append(periods, buildDECNCPPExportPeriods(year, holidays)...)
		}
	}

	return periods
}

func dukeCarolinasSCWindows(year int) []dukeEffectiveWindow {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)
	if year < 2026 {
		return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: 0}}
	}
	if year == 2026 {
		aug1 := time.Date(2026, time.August, 1, 0, 0, 0, 0, etLocation)
		sep1 := time.Date(2026, time.September, 1, 0, 0, 0, 0, etLocation)
		nov1 := time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)
		return []dukeEffectiveWindow{
			{start: yearStart, end: aug1, adderDollarsPerKWH: 0},
			{start: aug1, end: sep1, adderDollarsPerKWH: decSC2026AugRidersDollarsPerKWH},
			{start: sep1, end: nov1, adderDollarsPerKWH: decSC2026SepRidersDollarsPerKWH},
			{start: nov1, end: yearEnd, adderDollarsPerKWH: decSC2026NovFuelAndRidersDollarsPerKWH},
		}
	}
	return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: decSC2026NovFuelAndRidersDollarsPerKWH}}
}

// dukeCarolinasSCPeriods returns pricing periods for Duke Energy Carolinas (South Carolina)
func dukeCarolinasSCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)
		for _, w := range dukeCarolinasSCWindows(year) {
			add := func(base float64) float64 {
				return roundDukeDollarsPerKWH(base + w.adderDollarsPerKWH)
			}
			var raw []types.UtilityFeesPeriod

			switch plan {
			case "duke_carolinas_sc_rs":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: add(0.138125), // first-tier rate
						OtherDescription:   "Residential Flat Rate",
					},
				})

			case "duke_carolinas_sc_re":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: add(0.128547), // first-tier rate
						OtherDescription:   "Residential Space Conditioning & Water Heating",
					},
				})

			case "duke_carolinas_sc_rt":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.216118), add(0.096597), add(0.059814)))

			case "duke_carolinas_sc_rt_ev":
				// Leaf No. 12 Schedule RT-EV ($0.106552 & $0.155415/kWh eff. Nov 1, 2026 incl. $0.005799 fuel step)
				raw = buildPeriods(etLocation, buildDukeEVPeriods(year, add(0.100753), add(0.149616)))

			case "duke_carolinas_sc_r_stou":
				// Solar TOU Schedule R-STOU
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeSTOUPeriods(year, holidays, add(0.209021), add(0.128191), add(0.093782)))

			case "duke_carolinas_sc_rstc":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.264869), add(0.131171), add(0.088409)))

			case "duke_carolinas_sc_retc":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.257296), add(0.123157), add(0.080971)))
			}

			periods = append(periods, clipDukePeriods(raw, w.start, w.end)...)
		}
	}

	// Solar Export Credits
	switch options.NetMeteringScheme {
	case "rsc", "":
		periods = append(periods, buildExportCreditPeriod(years, dukeRSCSCCredit, "Solar Choice Net Excess Credit")...)
	case "nmt":
		periods = append(periods, buildExportCreditPeriod(years, decSCNMTCreditDollarsPerKWH, "Net Metering Transition Excess Credit")...)
	case "rnm":
		// Leaf No. 119 Rider RNM is 1:1 retail net metering (SeparateGenerationCredit: false).
	}

	return periods
}

func dukeProgressNCWindows(year int) []dukeEffectiveWindow {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)
	if year < 2026 {
		return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: 0}}
	}
	if year == 2026 {
		jun1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, etLocation)
		jul1 := time.Date(2026, time.July, 1, 0, 0, 0, 0, etLocation)
		return []dukeEffectiveWindow{
			{start: yearStart, end: jun1, adderDollarsPerKWH: 0},
			{start: jun1, end: jul1, adderDollarsPerKWH: depNC2026JunRidersDollarsPerKWH},
			{start: jul1, end: yearEnd, adderDollarsPerKWH: depNC2026JulRidersDollarsPerKWH},
		}
	}
	return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: depNC2026JulRidersDollarsPerKWH}}
}

// dukeProgressNCPeriods returns pricing periods for Duke Energy Progress (North Carolina)
func dukeProgressNCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)
		for _, w := range dukeProgressNCWindows(year) {
			add := func(base float64) float64 {
				return roundDukeDollarsPerKWH(base + w.adderDollarsPerKWH)
			}
			var raw []types.UtilityFeesPeriod

			switch plan {
			case "duke_progress_nc_res":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: add(0.12623),
						OtherDescription:   "Residential Flat Rate",
					},
				})

			case "duke_progress_nc_r_tou":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.29905), add(0.11321), add(0.07372)))

			case "duke_progress_nc_r_toud":
				// Leaf No. 501 Schedule R-TOUD (excludes per-kW On-Peak and Max demand charges)
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.15638), add(0.06633), add(0.04347)))

			case "duke_progress_nc_r_tou_ev":
				// Leaf No. 504 Schedule R-TOU-EV
				raw = buildPeriods(etLocation, buildDukeEVPeriods(year, add(0.06548), add(0.13096)))

			case "duke_progress_nc_r_tou_cpp":
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.21952), add(0.11000), add(0.08274)))
			}

			periods = append(periods, clipDukePeriods(raw, w.start, w.end)...)
		}
	}

	// Solar Export Credits
	switch options.NetMeteringScheme {
	case "rsc", "":
		periods = append(periods, buildExportCreditPeriod(years, depNCRSCCreditDollarsPerKWH, "Solar Choice Net Excess Credit")...)
	case "nmb":
		periods = append(periods, buildExportCreditPeriod(years, depNCNMBCreditDollarsPerKWH, "Net Metering Bridge Excess Credit")...)
	case "scg":
		// DEP NC Schedule PP Variable Avoided Cost Export Rates
		for _, year := range years {
			holidays := getDukeHolidays(year)
			periods = append(periods, buildDEPNCPPExportPeriods(year, holidays)...)
		}
	}

	return periods
}

func dukeProgressSCWindows(year int) []dukeEffectiveWindow {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)
	if year < 2026 {
		return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: 0}}
	}
	if year == 2026 {
		aug1 := time.Date(2026, time.August, 1, 0, 0, 0, 0, etLocation)
		sep1 := time.Date(2026, time.September, 1, 0, 0, 0, 0, etLocation)
		return []dukeEffectiveWindow{
			{start: yearStart, end: aug1, adderDollarsPerKWH: 0},
			{start: aug1, end: sep1, adderDollarsPerKWH: depSC2026AugFuelAndRidersDollarsPerKWH},
			{start: sep1, end: yearEnd, adderDollarsPerKWH: depSC2026SepFuelAndRidersDollarsPerKWH},
		}
	}
	return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: depSC2026SepFuelAndRidersDollarsPerKWH}}
}

// dukeProgressSCPeriods returns pricing periods for Duke Energy Progress (South Carolina)
func dukeProgressSCPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeHolidays(year)
		for _, w := range dukeProgressSCWindows(year) {
			add := func(base float64) float64 {
				return roundDukeDollarsPerKWH(base + w.adderDollarsPerKWH)
			}
			var raw []types.UtilityFeesPeriod

			switch plan {
			case "duke_progress_sc_res":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: add(0.14320),
						OtherDescription:   "Residential Flat Rate",
					},
				})

			case "duke_progress_sc_r_toud":
				// Leaf No. 501 Schedule R-TOUD (excludes per-kW On-Peak and Max demand charges)
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.19773), add(0.08940), add(0.06139)))

			case "duke_progress_sc_r_stou":
				// Leaf No. 502 Schedule R-STOU
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeSTOUPeriods(year, holidays, add(0.20415), add(0.12955), add(0.09959)))

			case "duke_progress_sc_r_tou_cpp":
				// Leaf No. 503 Schedule R-TOU-CPP
				// TODO: no support for Critical Peak pricing notifications
				raw = buildPeriods(etLocation, buildDukeTOUPeriods(year, holidays, add(0.28851), add(0.12672), add(0.08643)))

			case "duke_progress_sc_r_tou_ev":
				raw = buildPeriods(etLocation, buildDukeEVPeriods(year, add(0.09615), add(0.15127)))
			}

			periods = append(periods, clipDukePeriods(raw, w.start, w.end)...)
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "rsc" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, depSCRSCCreditDollarsPerKWH, "Solar Choice Net Excess Credit")...)
	} else if options.NetMeteringScheme == "scg" {
		// DEP SC Schedule PP Variable Avoided Cost Export Rates (reduced by Solar Integration charge)
		for _, year := range years {
			holidays := getDukeHolidays(year)
			periods = append(periods, buildDEPSCPPExportPeriods(year, holidays)...)
		}
	}

	return periods
}

// isDEIWinter determines if the given time falls in Duke Energy Indiana's winter period
// (from the first Sunday in November to the second Sunday in March).
func isDEIWinter(t time.Time) bool {
	y := t.Year()
	month := t.Month()

	if month == time.January || month == time.February {
		return true
	}
	if month != time.November && month != time.December && month != time.March {
		return false
	}

	var firstSundayNov, secondSundayMar int
	switch y {
	case 2025:
		firstSundayNov, secondSundayMar = 2, 9
	case 2026:
		firstSundayNov, secondSundayMar = 1, 8
	case 2027:
		firstSundayNov, secondSundayMar = 7, 14
	case 2028:
		firstSundayNov, secondSundayMar = 5, 12
	case 2029:
		firstSundayNov, secondSundayMar = 4, 11
	case 2030:
		firstSundayNov, secondSundayMar = 3, 10
	case 2031:
		firstSundayNov, secondSundayMar = 2, 9
	default:
		panic("unsupported year for Duke Energy Indiana winter calculation")
	}

	if month == time.November || month == time.December {
		startWinter := time.Date(y, time.November, firstSundayNov, 0, 0, 0, 0, t.Location())
		return !t.Before(startWinter)
	}
	if month == time.March {
		endWinter := time.Date(y, time.March, secondSundayMar, 0, 0, 0, 0, t.Location())
		return t.Before(endWinter)
	}
	return false
}

func dukeIndianaWindows(year int) []dukeEffectiveWindow {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)
	if year < 2026 {
		return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: 0}}
	}
	if year == 2026 {
		nov1 := time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)
		return []dukeEffectiveWindow{
			{start: yearStart, end: nov1, adderDollarsPerKWH: 0},
			{start: nov1, end: yearEnd, adderDollarsPerKWH: dei2026NovTrackersDollarsPerKWH},
		}
	}
	return []dukeEffectiveWindow{{start: yearStart, end: yearEnd, adderDollarsPerKWH: dei2026NovTrackersDollarsPerKWH}}
}

// dukeIndianaPeriods returns pricing periods for Duke Energy Indiana (DEI)
func dukeIndianaPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	nov12026 := time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)

	for _, year := range years {
		holidays := getIndianaHolidays(year)

		switch plan {
		case "duke_indiana_rs":
			for _, w := range dukeIndianaWindows(year) {
				raw := buildPeriods(etLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: roundDukeDollarsPerKWH(0.186556 + w.adderDollarsPerKWH), // first-tier rate
						OtherDescription:   "Residential Flat Rate",
					},
				})
				periods = append(periods, clipDukePeriods(raw, w.start, w.end)...)
			}

		case "duke_indiana_rs_tou":
			// Time of Use (Tariff 6.5) has dynamic winter/non-winter periods:
			// Discount: 12 am - 4 am all days.
			// On-Peak:
			//   Non-Winter: Mon-Fri 5 pm - 9 pm
			//   Winter: Mon-Fri 6 am - 8 am AND 5 pm - 9 pm
			// Winter: On and after the first Sunday in November to the second Sunday in March
			startDay := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
			endDay := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

			for d := startDay; d.Before(endDay); d = d.AddDate(0, 0, 1) {
				dateStr := d.Format("2006-01-02")
				isHoliday := slices.Contains(holidays, dateStr)
				isWeekend := d.Weekday() == time.Saturday || d.Weekday() == time.Sunday
				isWinter := isDEIWinter(d)

				var trackerAdderDollarsPerKWH float64
				if !d.Before(nov12026) {
					trackerAdderDollarsPerKWH = dei2026NovTrackersDollarsPerKWH
				}

				// Discount period: 12 am - 4 am (all days)
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Super Off-Peak",
						Start:       d,
						End:         d.AddDate(0, 0, 1),
						Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 4}},
						LocationPtr: etLocation,
					},
					DollarsPerKWH: roundDukeDollarsPerKWH(0.085679 + trackerAdderDollarsPerKWH),
					Description:   "DEI Discount Hours",
				})

				// On-Peak hours (weekdays only, excluding holidays)
				if !isHoliday && !isWeekend {
					if isWinter {
						// On-Peak in Winter: 6 am - 8 am AND 5 pm - 9 pm
						periods = append(periods, types.UtilityFeesPeriod{
							TimePeriod: types.TimePeriod{
								Name:  "On-Peak",
								Start: d,
								End:   d.AddDate(0, 0, 1),
								Hours: []types.UtilityHourPeriod{
									{HourStart: 6, HourEnd: 8},
									{HourStart: 17, HourEnd: 21},
								},
								LocationPtr: etLocation,
							},
							DollarsPerKWH: roundDukeDollarsPerKWH(0.214198 + trackerAdderDollarsPerKWH),
							Description:   "DEI Winter On-Peak",
						})
					} else {
						// On-Peak in Non-Winter: 5 pm - 9 pm
						periods = append(periods, types.UtilityFeesPeriod{
							TimePeriod: types.TimePeriod{
								Name:  "On-Peak",
								Start: d,
								End:   d.AddDate(0, 0, 1),
								Hours: []types.UtilityHourPeriod{
									{HourStart: 17, HourEnd: 21},
								},
								LocationPtr: etLocation,
							},
							DollarsPerKWH: roundDukeDollarsPerKWH(0.214198 + trackerAdderDollarsPerKWH),
							Description:   "DEI Summer On-Peak",
						})
					}
				}

				var offPeakGaps []types.UtilityHourPeriod
				if isHoliday || isWeekend {
					offPeakGaps = []types.UtilityHourPeriod{{HourStart: 4, HourEnd: 24}}
				} else if isWinter {
					offPeakGaps = []types.UtilityHourPeriod{
						{HourStart: 4, HourEnd: 6},
						{HourStart: 8, HourEnd: 17},
						{HourStart: 21, HourEnd: 24},
					}
				} else {
					offPeakGaps = []types.UtilityHourPeriod{
						{HourStart: 4, HourEnd: 17},
						{HourStart: 21, HourEnd: 24},
					}
				}

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Off-Peak",
						Start:       d,
						End:         d.AddDate(0, 0, 1),
						Hours:       offPeakGaps,
						LocationPtr: etLocation,
					},
					DollarsPerKWH: roundDukeDollarsPerKWH(0.142799 + trackerAdderDollarsPerKWH),
					Description:   "DEI Off-Peak",
				})
			}
		}
	}

	// Solar Export Credits
	if options.NetMeteringScheme == "edg" || options.NetMeteringScheme == "" {
		periods = append(periods, buildExportCreditPeriod(years, dukeIndianaEDGCredit, "Excess Distributed Generation Credit")...)
	}

	return periods
}

// buildExportCreditPeriod creates a types.UtilityFeesPeriod slice for flat export credit rate.
func buildExportCreditPeriod(years []int, rate float64, description string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
				End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
				LocationPtr: etLocation,
			},
			DollarsPerKWH:            rate,
			SeparateGenerationCredit: true,
			Description:              description,
		})
	}
	return periods
}

// buildDECNCPPExportPeriods returns the DEC NC Schedule PP variable avoided cost export periods for a given year.
func buildDECNCPPExportPeriods(year int, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	simplified := []touSimplifiedPeriod{
		// --- Summer (June - Sept) ---
		{
			Year:                               year,
			MonthStart:                         time.June,
			MonthEnd:                           time.September,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: decNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 12, HourEnd: 17},
						{HourStart: 21, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPSummerOnPeak,
					Description:                   "Avoided Cost Summer On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 21},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPPremiumSummer,
					Description:                   "Avoided Cost Summer Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: decNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Winter (Dec - Feb) ---
		{
			Year:                               year,
			MonthStart:                         time.December,
			MonthEnd:                           time.February,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: decNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 5, HourEnd: 6},
						{HourStart: 9, HourEnd: 10},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPWinterMorningPeak,
					Description:                   "Avoided Cost Winter Morning On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPWinterEveningPeak,
					Description:                   "Avoided Cost Winter Evening On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPPremiumWinter,
					Description:                   "Avoided Cost Winter Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: decNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Shoulder (March - May, Oct - Nov) ---
		{
			Year:                               year,
			Months:                             []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: decNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			Months:           []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 5, HourEnd: 9},
						{HourStart: 17, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: decNCPPShoulderOnPeak,
					Description:                   "Avoided Cost Shoulder On-Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: decNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
	}

	periods = append(periods, buildPeriods(etLocation, simplified)...)
	return periods
}

// buildDEPNCPPExportPeriods returns the DEP NC Schedule PP variable avoided cost export periods for a given year.
func buildDEPNCPPExportPeriods(year int, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// We can build these using buildPeriods with touSimplifiedPeriod:
	// Summer: June - September (Months 6 - 9)
	// Winter: December - February (Months 12, 1, 2)
	// Shoulder: March - May, October - November (Months 3 - 5, 10 - 11)
	simplified := []touSimplifiedPeriod{
		// --- Summer (June - Sept) ---
		{
			Year:                               year,
			MonthStart:                         time.June,
			MonthEnd:                           time.September,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 14, HourEnd: 18},
						{HourStart: 22, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPSummerOnPeak,
					Description:                   "Avoided Cost Summer On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 22},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPPremiumSummer,
					Description:                   "Avoided Cost Summer Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depNCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Winter (Dec - Feb) ---
		{
			Year:                               year,
			MonthStart:                         time.December,
			MonthEnd:                           time.February,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 6},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPWinterMorningPeak,
					Description:                   "Avoided Cost Winter Morning On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPWinterEveningPeak,
					Description:                   "Avoided Cost Winter Evening On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPPremiumWinter,
					Description:                   "Avoided Cost Winter Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depNCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Shoulder (March - May, Oct - Nov) ---
		{
			Year:                               year,
			Months:                             []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			Months:           []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 9},
						{HourStart: 17, HourEnd: 24},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depNCPPShoulderOnPeak,
					Description:                   "Avoided Cost Shoulder On-Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depNCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
	}

	periods = append(periods, buildPeriods(etLocation, simplified)...)
	return periods
}

// buildDEPSCPPExportPeriods returns the DEP SC Schedule PP variable avoided cost export periods for a given year.
func buildDEPSCPPExportPeriods(year int, holidays []string) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	simplified := []touSimplifiedPeriod{
		// --- Summer (June - Sept) ---
		{
			Year:                               year,
			MonthStart:                         time.June,
			MonthEnd:                           time.September,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depSCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.June,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 13, HourEnd: 17},
						{HourStart: 21, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPSummerOnPeak,
					Description:                   "Avoided Cost Summer On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 21},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPPremiumSummer,
					Description:                   "Avoided Cost Summer Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depSCPPSummerOffPeak,
			OtherDescription:                   "Avoided Cost Summer Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Winter (Dec - Feb) ---
		{
			Year:                               year,
			MonthStart:                         time.December,
			MonthEnd:                           time.February,
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depSCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 4, HourEnd: 6},
						{HourStart: 9, HourEnd: 11},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPWinterMorningPeak,
					Description:                   "Avoided Cost Winter Morning On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 18, HourEnd: 22},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPWinterEveningPeak,
					Description:                   "Avoided Cost Winter Evening On-Peak Export",
				},
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 6, HourEnd: 9},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPPremiumWinter,
					Description:                   "Avoided Cost Winter Premium Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depSCPPWinterOffPeak,
			OtherDescription:                   "Avoided Cost Winter Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},

		// --- Shoulder (March - May, Oct - Nov) ---
		{
			Year:                               year,
			Months:                             []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:                      holidays,
			OtherGenerationCreditDollarsPerKWH: depSCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Holiday Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
		{
			Year:             year,
			Months:           []time.Month{time.March, time.April, time.May, time.October, time.November},
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Hours: []types.UtilityHourPeriod{
						{HourStart: 5, HourEnd: 10},
						{HourStart: 17, HourEnd: 23},
					},
					Weekday:                       true,
					GenerationCreditDollarsPerKWH: depSCPPShoulderOnPeak,
					Description:                   "Avoided Cost Shoulder On-Peak Export",
				},
			},
			OtherGenerationCreditDollarsPerKWH: depSCPPShoulderOffPeak,
			OtherDescription:                   "Avoided Cost Shoulder Off-Peak Export",
			OnlySeparateGenerationCredit:       true,
		},
	}

	periods = append(periods, buildPeriods(etLocation, simplified)...)
	return periods
}

// getDukeFLHolidays returns Duke's holiday calendar for Florida (identical to Indiana)
func getDukeFLHolidays(year int) []string {
	return getIndianaHolidays(year)
}

func buildDukeFLRST1Periods(year int, holidays []string, onPeakDollarsPerKWH, offPeakDollarsPerKWH, discountDollarsPerKWH float64) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// === WINTER (December - February) ===
		// 1. Weekday Winter (except holidays)
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 3}},
					Weekday:       true,
					DollarsPerKWH: discountDollarsPerKWH,
					Description:   "RST-1 Winter Weekday Discount",
				},
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 5, HourEnd: 10}, {HourStart: 18, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: onPeakDollarsPerKWH,
					Description:   "RST-1 Winter Weekday On-Peak",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 3, HourEnd: 5}, {HourStart: 10, HourEnd: 18}, {HourStart: 21, HourEnd: 24}},
					Weekday:       true,
					DollarsPerKWH: offPeakDollarsPerKWH,
					Description:   "RST-1 Winter Weekday Off-Peak",
				},
			},
		},
		// 2. Weekend Winter (Saturdays/Sundays)
		{
			Year:       year,
			MonthStart: time.December,
			MonthEnd:   time.February,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 3}},
					Weekend:       true,
					DollarsPerKWH: discountDollarsPerKWH,
					Description:   "RST-1 Winter Weekend Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 3, HourEnd: 24}},
					Weekend:       true,
					DollarsPerKWH: offPeakDollarsPerKWH,
					Description:   "RST-1 Winter Weekend Off-Peak",
				},
			},
		},
		// 3. Holiday Winter
		{
			Year:             year,
			MonthStart:       time.December,
			MonthEnd:         time.February,
			SpecificDates:    holidays,
			SpecificDatesNot: false,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 3}},
					DollarsPerKWH: discountDollarsPerKWH,
					Description:   "RST-1 Winter Holiday Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 3, HourEnd: 24}},
					DollarsPerKWH: offPeakDollarsPerKWH,
					Description:   "RST-1 Winter Holiday Off-Peak",
				},
			},
		},

		// === NON-WINTER (March - November) ===
		// 4. Weekday Non-Winter (except holidays)
		{
			Year:             year,
			MonthStart:       time.March,
			MonthEnd:         time.November,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}},
					Weekday:       true,
					DollarsPerKWH: discountDollarsPerKWH,
					Description:   "RST-1 Non-Winter Weekday Discount",
				},
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: onPeakDollarsPerKWH,
					Description:   "RST-1 Non-Winter Weekday On-Peak",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 18}, {HourStart: 21, HourEnd: 24}},
					Weekday:       true,
					DollarsPerKWH: offPeakDollarsPerKWH,
					Description:   "RST-1 Non-Winter Weekday Off-Peak",
				},
			},
		},
		// 5. Weekend Non-Winter (Saturdays/Sundays)
		{
			Year:       year,
			MonthStart: time.March,
			MonthEnd:   time.November,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}},
					Weekend:       true,
					DollarsPerKWH: discountDollarsPerKWH,
					Description:   "RST-1 Non-Winter Weekend Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 24}},
					Weekend:       true,
					DollarsPerKWH: offPeakDollarsPerKWH,
					Description:   "RST-1 Non-Winter Weekend Off-Peak",
				},
			},
		},
		// 6. Holiday Non-Winter
		{
			Year:             year,
			MonthStart:       time.March,
			MonthEnd:         time.November,
			SpecificDates:    holidays,
			SpecificDatesNot: false,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "Super Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}},
					DollarsPerKWH: discountDollarsPerKWH,
					Description:   "RST-1 Non-Winter Holiday Discount",
				},
				{
					Name:          "Off-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 24}},
					DollarsPerKWH: offPeakDollarsPerKWH,
					Description:   "RST-1 Non-Winter Holiday Off-Peak",
				},
			},
		},
	}
}

type dukeFloridaWindow struct {
	start                    time.Time
	end                      time.Time
	rsWinterDollarsPerKWH    float64
	rsNonWinterDollarsPerKWH float64
	rstOnPeakDollarsPerKWH   float64
	rstOffPeakDollarsPerKWH  float64
	rstDiscountDollarsPerKWH float64
}

func dukeFloridaWindows(year int) []dukeFloridaWindow {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

	preOct2026 := dukeFloridaWindow{
		start:                    yearStart,
		end:                      yearEnd,
		rsWinterDollarsPerKWH:    0.087540,
		rsNonWinterDollarsPerKWH: 0.076860,
		rstOnPeakDollarsPerKWH:   0.11090,
		rstOffPeakDollarsPerKWH:  0.08215,
		rstDiscountDollarsPerKWH: 0.04984,
	}

	if year < 2026 {
		return []dukeFloridaWindow{preOct2026}
	}

	if year == 2026 {
		oct1 := time.Date(2026, time.October, 1, 0, 0, 0, 0, etLocation)
		preOct2026.end = oct1
		q42026 := dukeFloridaWindow{
			start:                    oct1,
			end:                      yearEnd,
			rsWinterDollarsPerKWH:    roundDukeDollarsPerKWH(defOct2026RS1WinterBaseDollarsPerKWH + def2026BA1RS1DollarsPerKWH),
			rsNonWinterDollarsPerKWH: roundDukeDollarsPerKWH(defOct2026RS1NonWinterBaseDollarsPerKWH + def2026BA1RS1DollarsPerKWH),
			rstOnPeakDollarsPerKWH:   roundDukeDollarsPerKWH(defOct2026RST1OnPeakBaseDollarsPerKWH + def2026BA1RST1OnPeakDollarsPerKWH),
			rstOffPeakDollarsPerKWH:  roundDukeDollarsPerKWH(defOct2026RST1OffPeakBaseDollarsPerKWH + def2026BA1RST1OffPeakDollarsPerKWH),
			rstDiscountDollarsPerKWH: roundDukeDollarsPerKWH(defOct2026RST1DiscountBaseDollarsPerKWH + def2026BA1RST1DiscountDollarsPerKWH),
		}
		return []dukeFloridaWindow{preOct2026, q42026}
	}

	return []dukeFloridaWindow{
		{
			start:                    yearStart,
			end:                      yearEnd,
			rsWinterDollarsPerKWH:    roundDukeDollarsPerKWH(defOct2026RS1WinterBaseDollarsPerKWH + def2027BA1RS1DollarsPerKWH),
			rsNonWinterDollarsPerKWH: roundDukeDollarsPerKWH(defOct2026RS1NonWinterBaseDollarsPerKWH + def2027BA1RS1DollarsPerKWH),
			rstOnPeakDollarsPerKWH:   roundDukeDollarsPerKWH(defOct2026RST1OnPeakBaseDollarsPerKWH + def2027BA1RST1OnPeakDollarsPerKWH),
			rstOffPeakDollarsPerKWH:  roundDukeDollarsPerKWH(defOct2026RST1OffPeakBaseDollarsPerKWH + def2027BA1RST1OffPeakDollarsPerKWH),
			rstDiscountDollarsPerKWH: roundDukeDollarsPerKWH(defOct2026RST1DiscountBaseDollarsPerKWH + def2027BA1RST1DiscountDollarsPerKWH),
		},
	}
}

func dukeFloridaPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDukeFLHolidays(year)
		for _, w := range dukeFloridaWindows(year) {
			var raw []types.UtilityFeesPeriod

			switch plan {
			case "duke_florida_rs":
				raw = buildPeriods(etLocation, []touSimplifiedPeriod{
					// Winter (December - February)
					{
						Year:               year,
						MonthStart:         time.December,
						MonthEnd:           time.February,
						OtherDollarsPerKWH: w.rsWinterDollarsPerKWH,
						OtherDescription:   "Residential Service RS-1 Winter (Dec-Feb)",
					},
					// Non-Winter (March - November)
					{
						Year:               year,
						MonthStart:         time.March,
						MonthEnd:           time.November,
						OtherDollarsPerKWH: w.rsNonWinterDollarsPerKWH,
						OtherDescription:   "Residential Service RS-1 Non-Winter (Mar-Nov)",
					},
				})

			case "duke_florida_rst":
				raw = buildPeriods(etLocation, buildDukeFLRST1Periods(year, holidays, w.rstOnPeakDollarsPerKWH, w.rstOffPeakDollarsPerKWH, w.rstDiscountDollarsPerKWH))
			}

			periods = append(periods, clipDukePeriods(raw, w.start, w.end)...)
		}
	}

	return periods
}

// dukeUtilityInfo returns the UtilityProviderInfo slice for Duke Energy
func dukeUtilityInfo() []types.UtilityProviderInfo {
	ncOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export rider.",
			Choices: []types.UtilityOptionChoice{
				{Value: "rsc", Name: "Residential Solar Choice (RSC)"},
				{Value: "nmb", Name: "Net Metering Bridge (NMB)"},
				{Value: "scg", Name: "Small Customer Generator (SCG)"},
			},
			Default: "rsc",
		},
	}
	depSCOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export rider.",
			Choices: []types.UtilityOptionChoice{
				{Value: "rsc", Name: "Residential Solar Choice (RSC)"},
				{Value: "scg", Name: "Small Customer Generator (SCG)"},
			},
			Default: "rsc",
		},
	}
	scOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export rider.",
			Choices: []types.UtilityOptionChoice{
				{Value: "rsc", Name: "Residential Solar Choice (RSC)"},
				{Value: "nmt", Name: "Net Metering Transition (NMT)"},
				{Value: "rnm", Name: "Renewable Net Metering (RNM)"},
			},
			Default: "rsc",
		},
	}
	inOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Export Scheme / Rider",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar export scheme.",
			Choices: []types.UtilityOptionChoice{
				{Value: "edg", Name: "Excess Distributed Generation (EDG)"},
				{Value: "net", Name: "Standard Net Metering (1:1)"},
			},
			Default: "edg",
		},
	}
	flOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Florida net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return []types.UtilityProviderInfo{
		{
			ID:   "duke_carolinas_nc",
			Name: "Duke Energy Carolinas (North Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_carolinas_nc_rs",
					Name:    "Standard Residential",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rs", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_re",
					Name:    "Residential Space Conditioning & Water Heating",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_re", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_rt",
					Name:    "Residential Time-of-Use",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rt", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_rt_ev",
					Name:    "Time-of-Use Electric Vehicle",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rt_ev", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_rstc",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_rstc", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_nc_retc",
					Name:    "Residential All-Electric Time-of-Use (Critical Peak)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasNCPeriods("duke_carolinas_nc_retc", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_carolinas_sc",
			Name: "Duke Energy Carolinas (South Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_carolinas_sc_rs",
					Name:    "Standard Residential",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rs", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_re",
					Name:    "Residential Space Conditioning & Water Heating",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_re", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_rt",
					Name:    "Residential Time-of-Use",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rt", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_rt_ev",
					Name:    "Time-of-Use Electric Vehicle",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rt_ev", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_r_stou",
					Name:    "Solar Time-of-Use",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_r_stou", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_rstc",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_rstc", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_carolinas_sc_retc",
					Name:    "Residential All-Electric Time-of-Use (Critical Peak)",
					Options: scOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeCarolinasSCPeriods("duke_carolinas_sc_retc", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_progress_nc",
			Name: "Duke Energy Progress (North Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_progress_nc_res",
					Name:    "Standard Residential",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_res", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_nc_r_tou",
					Name:    "Residential Time-of-Use",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_r_tou", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_nc_r_toud",
					Name:    "Residential Time-of-Use Demand (R-TOUD)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_r_toud", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_nc_r_tou_ev",
					Name:    "Time-of-Use Electric Vehicle",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_r_tou_ev", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_nc_r_tou_cpp",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: ncOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressNCPeriods("duke_progress_nc_r_tou_cpp", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_progress_sc",
			Name: "Duke Energy Progress (South Carolina)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_progress_sc_res",
					Name:    "Standard Residential",
					Options: depSCOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_res", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_sc_r_toud",
					Name:    "Residential Time-of-Use Demand (R-TOUD)",
					Options: depSCOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_r_toud", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_sc_r_stou",
					Name:    "Solar Time-of-Use",
					Options: depSCOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_r_stou", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_sc_r_tou_cpp",
					Name:    "Residential Time-of-Use (Critical Peak)",
					Options: depSCOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_r_tou_cpp", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_progress_sc_r_tou_ev",
					Name:    "Time-of-Use Electric Vehicle",
					Options: depSCOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeProgressSCPeriods("duke_progress_sc_r_tou_ev", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_indiana",
			Name: "Duke Energy Indiana",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_indiana_rs",
					Name:    "Standard Residential",
					Options: inOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeIndianaPeriods("duke_indiana_rs", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "duke_indiana_rs_tou",
					Name:    "Time-of-Use (Optional)",
					Options: inOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeIndianaPeriods("duke_indiana_rs_tou", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "duke_florida",
			Name: "Duke Energy Florida",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "duke_florida_rs",
					Name:    "Residential Service (RS-1)",
					Options: flOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeFloridaPeriods("duke_florida_rs", opts, []int{2025, 2026, 2027, 2028}), nil
					},
				},
				{
					ID:      "duke_florida_rst",
					Name:    "Residential Service TOU (RST-1)",
					Options: flOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return dukeFloridaPeriods("duke_florida_rst", opts, []int{2025, 2026, 2027, 2028}), nil
					},
				},
			},
		},
	}
}
