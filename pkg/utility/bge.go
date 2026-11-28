package utility

// Tariff Audit Metadata
// - Provider(s): bge (Baltimore Gas and Electric (BGE))
// - Official Sources:
//   - BGE Electric Service Rates and Tariffs Portal: https://www.bge.com/my-account/my-dashboard/rates-tariffs/electric-service-rates-tariffs
//   - Schedule R (Supplement 709, P.S.C. Md. E-6): https://azure-na-assets.contentstack.com/v3/assets/blt71bfe6e8a1c2d265/blt10129d1f47260cd7/P3_SCH_R.pdf
//   - Schedule RL (Supplement 717, P.S.C. Md. E-6): https://azure-na-assets.contentstack.com/v3/assets/blt71bfe6e8a1c2d265/blte58f7cf25bd43b09/P3_SCH_RL.pdf
//   - Schedule EV (Supplement 709, P.S.C. Md. E-6): https://azure-na-assets.contentstack.com/v3/assets/blt71bfe6e8a1c2d265/blt79846fd9655c69d8/P3_SCH_EV.pdf
//   - Schedule RD (Supplement 749, P.S.C. Md. E-6): https://azure-na-assets.contentstack.com/v3/assets/blt71bfe6e8a1c2d265/bltd9617a8f40ce427f/6a5fb7de0ddda05c707141b1/P3_SCH_RD_1.pdf
//   - Rider 1 Standard Offer Service (Page 77-A, Supplement 755, Non-Summer SOS Oct 1, 2026 - May 31, 2027): https://azure-na-assets.contentstack.com/v3/assets/blt71bfe6e8a1c2d265/blt970ee32f55c20fde/6b070356fde6fedcd6f9ef13/Rdr_1.pdf
//   - Summer 2027 SOS Posting (Schedules R & RL, Nov 6, 2026): https://www.bge.com/cdn/assets/v3/assets/blt71bfe6e8a1c2d265/blt7e1fce040c197149/6aecd7665ae32a13083d11cc/SOS_Posting.pdf?branch=prod_alias
//   - Electric Rates Information / EmPOWER Maryland (Riders 2, 15, and 26, Supplement 742): https://www.bge.com/cdn/assets/v3/assets/blt71bfe6e8a1c2d265/blte65b1dd8c5037101/6aea4c0d81bf5329b4901d33/POLR_Rates_PTC_MiscCharges.pdf?branch=prod_alias
//   - Rider 8 Rate Adjustment Chart (Oct 2026 - Jan 2027 SOS credit): https://www.bge.com/cdn/assets/v3/assets/blt71bfe6e8a1c2d265/bltf75afad89af14ef3/6acce6326214c42d62238ff5/POLR_RateAdjustmentChart_1.pdf?branch=prod_alias
//   - Rider 18 Net Energy Metering: https://azure-na-assets.contentstack.com/v3/assets/blt71bfe6e8a1c2d265/bltd67b5b4d9b4c52b5/Rdr_18.pdf
// - Effective Window: 2026-01-01 through 2028-12-31 (Supplement 755 Non-Summer SOS effective 2026-10-01 through 2027-05-31 with Rider 8 credit through 2027-01-31; Summer 2027 SOS for R & RL effective 2027-06-01 through 2027-09-30; carried through 2028-12-31 until superseded)
// - Revision Cadence: Semi-Annual / Annual (Jan 1 MYP delivery & EmPOWER Maryland riders; Feb 1 / Jun 1 / Oct 1 MD PSC SOS & Rider 8 resets)
// - Next Audit Due: 2027-01-05 (Verify Jan 1, 2027 MYP base delivery step, EmPOWER Maryland surcharges, Rider 8 post-Jan 2027 adjustment, and Summer 2027 EV/RD SOS filings)
// - Last Verified: 2026-11-26
// - Modeling Notes: Preserves pre-2026-10-01 historical rates; models base delivery (Supplements 709, 717, 749) + $0.01310/kWh EmPOWER Maryland (Riders 2, 15, and 26, Supplement 742) + Type I Residential SOS (Rider 1 Supplement 755 & Summer 2027 SOS posting, carrying Supplement 755 SOS into Summer 2027/2028 for EV and RD until published) + -$0.01050/kWh Rider 8 credit (2026-10-01 to 2027-02-01); shifts Non-Summer weekday TOU windows +1 hour during spring (2nd Sun Mar - 1st Sun Apr) and fall (last Sun Oct - 1st Sun Nov) DST transitions on RL, EV, and RD; omits sub-mill Riders 3, 10, and 16 and fixed monthly customer charges; 1:1 monthly kWh net metering under Rider 18.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Pre-2026-10-01 historical baseline rates ($/kWh).
	bgeRPreOct2026SummerDollarsPerKWH              = 0.19700
	bgeRPreOct2026NonSummerDollarsPerKWH           = 0.19103
	bgeRLPreOct2026SummerOnPeakDollarsPerKWH       = 0.32152
	bgeRLPreOct2026SummerInterPeakDollarsPerKWH    = 0.22152
	bgeRLPreOct2026SummerOffPeakDollarsPerKWH      = 0.15152
	bgeRLPreOct2026NonSummerOnPeakDollarsPerKWH    = 0.29152
	bgeRLPreOct2026NonSummerInterPeakDollarsPerKWH = 0.21152
	bgeRLPreOct2026NonSummerOffPeakDollarsPerKWH   = 0.15652
	bgeEVPreOct2026SummerOnPeakDollarsPerKWH       = 0.32181
	bgeEVPreOct2026SummerOffPeakDollarsPerKWH      = 0.15181
	bgeEVPreOct2026NonSummerOnPeakDollarsPerKWH    = 0.29181
	bgeEVPreOct2026NonSummerOffPeakDollarsPerKWH   = 0.15681
	bgeRDPreOct2026OnPeakDollarsPerKWH             = 0.38353
	bgeRDPreOct2026OffPeakDollarsPerKWH            = 0.13664

	// Base delivery charges ($/kWh): Supplements 709 (R, EV), 717 (RL), and 749 (RD).
	bgeRDeliveryDollarsPerKWH         = 0.04859
	bgeRLDeliveryDollarsPerKWH        = 0.04830
	bgeEVDeliveryDollarsPerKWH        = 0.04859
	bgeRDOnPeakDeliveryDollarsPerKWH  = 0.08874
	bgeRDOffPeakDeliveryDollarsPerKWH = 0.03736

	// Material volumetric riders ($/kWh) effective 2026-10-01:
	// EmPOWER Maryland (Riders 2, 15, and 26, Supplement 742) and Rider 8 SOS credit (Oct 2026 - Jan 2027).
	// Sub-mill Riders 3, 10, and 16 and fixed monthly customer charges are omitted.
	bgeEmPOWERDollarsPerKWH      = 0.01310
	bgeRider8CreditDollarsPerKWH = -0.01050

	// Rider 1 Type I Residential SOS supply charges ($/kWh):
	// Supplement 755 Non-Summer SOS (effective October 1, 2026 through May 31, 2027).
	bgeRSOSNonSummerSupp755DollarsPerKWH           = 0.15190
	bgeRLSOSNonSummerOnPeakSupp755DollarsPerKWH    = 0.25277
	bgeRLSOSNonSummerInterPeakSupp755DollarsPerKWH = 0.12269
	bgeRLSOSNonSummerOffPeakSupp755DollarsPerKWH   = 0.11581
	bgeEVSOSNonSummerOnPeakSupp755DollarsPerKWH    = 0.25450
	bgeEVSOSNonSummerOffPeakSupp755DollarsPerKWH   = 0.11698
	bgeRDSOSNonSummerOnPeakSupp755DollarsPerKWH    = 0.27734
	bgeRDSOSNonSummerOffPeakSupp755DollarsPerKWH   = 0.11396

	// Summer 2027 SOS Posting (effective June 1, 2027 through September 30, 2027 for Schedules R and RL).
	bgeRSOSSummer2027DollarsPerKWH           = 0.15502
	bgeRLSOSSummer2027OnPeakDollarsPerKWH    = 0.23885
	bgeRLSOSSummer2027InterPeakDollarsPerKWH = 0.11960
	bgeRLSOSSummer2027OffPeakDollarsPerKWH   = 0.11820

	// Combined volumetric rates ($/kWh) for 2026-10-01 to 2027-02-01 (Supplement 755 + EmPOWER + Rider 8 credit):
	bgeROct2026Jan2027DollarsPerKWH           = bgeRSOSNonSummerSupp755DollarsPerKWH + bgeRDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeRLOct2026Jan2027OnPeakDollarsPerKWH    = bgeRLSOSNonSummerOnPeakSupp755DollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeRLOct2026Jan2027InterPeakDollarsPerKWH = bgeRLSOSNonSummerInterPeakSupp755DollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeRLOct2026Jan2027OffPeakDollarsPerKWH   = bgeRLSOSNonSummerOffPeakSupp755DollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeEVOct2026Jan2027OnPeakDollarsPerKWH    = bgeEVSOSNonSummerOnPeakSupp755DollarsPerKWH + bgeEVDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeEVOct2026Jan2027OffPeakDollarsPerKWH   = bgeEVSOSNonSummerOffPeakSupp755DollarsPerKWH + bgeEVDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeRDOct2026Jan2027OnPeakDollarsPerKWH    = bgeRDSOSNonSummerOnPeakSupp755DollarsPerKWH + bgeRDOnPeakDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH
	bgeRDOct2026Jan2027OffPeakDollarsPerKWH   = bgeRDSOSNonSummerOffPeakSupp755DollarsPerKWH + bgeRDOffPeakDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH + bgeRider8CreditDollarsPerKWH

	// Combined volumetric rates ($/kWh) from 2027-02-01 (Rider 8 expired; carried forward for Non-Summer 2027/2028 and EV/RD Summer 2027/2028):
	bgeRNonSummerPostFeb2027DollarsPerKWH           = bgeRSOSNonSummerSupp755DollarsPerKWH + bgeRDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRLNonSummerPostFeb2027OnPeakDollarsPerKWH    = bgeRLSOSNonSummerOnPeakSupp755DollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRLNonSummerPostFeb2027InterPeakDollarsPerKWH = bgeRLSOSNonSummerInterPeakSupp755DollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRLNonSummerPostFeb2027OffPeakDollarsPerKWH   = bgeRLSOSNonSummerOffPeakSupp755DollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeEVPostFeb2027OnPeakDollarsPerKWH             = bgeEVSOSNonSummerOnPeakSupp755DollarsPerKWH + bgeEVDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeEVPostFeb2027OffPeakDollarsPerKWH            = bgeEVSOSNonSummerOffPeakSupp755DollarsPerKWH + bgeEVDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRDPostFeb2027OnPeakDollarsPerKWH             = bgeRDSOSNonSummerOnPeakSupp755DollarsPerKWH + bgeRDOnPeakDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRDPostFeb2027OffPeakDollarsPerKWH            = bgeRDSOSNonSummerOffPeakSupp755DollarsPerKWH + bgeRDOffPeakDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH

	// Combined volumetric rates ($/kWh) for Summer 2027 and carried-forward Summer 2028 on Schedules R and RL:
	bgeRSummer2027DollarsPerKWH           = bgeRSOSSummer2027DollarsPerKWH + bgeRDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRLSummer2027OnPeakDollarsPerKWH    = bgeRLSOSSummer2027OnPeakDollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRLSummer2027InterPeakDollarsPerKWH = bgeRLSOSSummer2027InterPeakDollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
	bgeRLSummer2027OffPeakDollarsPerKWH   = bgeRLSOSSummer2027OffPeakDollarsPerKWH + bgeRLDeliveryDollarsPerKWH + bgeEmPOWERDollarsPerKWH
)

func shiftBGESundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

func getBGEHolidays(year int) []string {
	holidays := []time.Time{
		shiftBGESundayHoliday(newYearsDay(year)),
		shiftBGESundayHoliday(presidentsDay(year)),
		shiftBGESundayHoliday(goodFriday(year)),
		shiftBGESundayHoliday(memorialDay(year)),
		shiftBGESundayHoliday(independenceDay(year)),
		shiftBGESundayHoliday(laborDay(year)),
		shiftBGESundayHoliday(thanksgivingDay(year)),
		shiftBGESundayHoliday(christmasDay(year)),
	}
	var out []string
	for _, h := range holidays {
		out = append(out, h.Format("2006-01-02"))
	}
	return out
}

func bgeNthSundayOfMonth(year int, month time.Month, n int) time.Time {
	firstOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, etLocation)
	daysUntilSunday := (int(time.Sunday) - int(firstOfMonth.Weekday()) + 7) % 7
	day := 1 + daysUntilSunday + (n-1)*7
	return time.Date(year, month, day, 0, 0, 0, 0, etLocation)
}

func bgeLastSundayOfMonth(year int, month time.Month) time.Time {
	lastOfMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, etLocation)
	day := lastOfMonth.Day() - int(lastOfMonth.Weekday())
	return time.Date(year, month, day, 0, 0, 0, 0, etLocation)
}

// bgeSpringDSTTransitionBounds returns [start, end) in etLocation from the second Sunday in March
// to the first Sunday in April, when Non-Summer TOU weekday periods shift 1 hour later.
func bgeSpringDSTTransitionBounds(year int) (time.Time, time.Time) {
	return bgeNthSundayOfMonth(year, time.March, 2), bgeNthSundayOfMonth(year, time.April, 1)
}

// bgeFallDSTTransitionBounds returns [start, end) in etLocation from the last Sunday in October
// to the first Sunday in November, when Non-Summer TOU weekday periods shift 1 hour later.
func bgeFallDSTTransitionBounds(year int) (time.Time, time.Time) {
	return bgeLastSundayOfMonth(year, time.October), bgeNthSundayOfMonth(year, time.November, 1)
}

type bgeSubWindow struct {
	start      time.Time
	end        time.Time
	dstShifted bool
}

func splitBGENonSummerWindow(year int, windowStart, windowEnd time.Time) []bgeSubWindow {
	springStart, springEnd := bgeSpringDSTTransitionBounds(year)
	fallStart, fallEnd := bgeFallDSTTransitionBounds(year)
	shifts := [][2]time.Time{
		{springStart, springEnd},
		{fallStart, fallEnd},
	}

	var out []bgeSubWindow
	cursor := windowStart
	for _, shift := range shifts {
		if !shift[1].After(cursor) || !shift[0].Before(windowEnd) {
			continue
		}
		if shift[0].After(cursor) {
			out = append(out, bgeSubWindow{
				start:      cursor,
				end:        shift[0],
				dstShifted: false,
			})
			cursor = shift[0]
		}
		segEnd := shift[1]
		if segEnd.After(windowEnd) {
			segEnd = windowEnd
		}
		out = append(out, bgeSubWindow{
			start:      cursor,
			end:        segEnd,
			dstShifted: true,
		})
		cursor = segEnd
	}
	if cursor.Before(windowEnd) {
		out = append(out, bgeSubWindow{
			start:      cursor,
			end:        windowEnd,
			dstShifted: false,
		})
	}
	return out
}

func buildBGEBoundedPeriods(start, end time.Time, simplified []touSimplifiedPeriod) []types.UtilityFeesPeriod {
	periods := buildPeriods(etLocation, simplified)
	for i := range periods {
		periods[i].Start = start
		periods[i].End = end
	}
	return periods
}

type bgeRateWindow struct {
	start                    time.Time
	end                      time.Time
	summer                   bool
	rDollarsPerKWH           float64
	rlOnPeakDollarsPerKWH    float64
	rlInterPeakDollarsPerKWH float64
	rlOffPeakDollarsPerKWH   float64
	evOnPeakDollarsPerKWH    float64
	evOffPeakDollarsPerKWH   float64
	rdOnPeakDollarsPerKWH    float64
	rdOffPeakDollarsPerKWH   float64
}

func bgeWindowsForYear(year int) []bgeRateWindow {
	jan1 := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	jun1 := time.Date(year, time.June, 1, 0, 0, 0, 0, etLocation)
	oct1 := time.Date(year, time.October, 1, 0, 0, 0, 0, etLocation)
	nextJan1 := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

	switch {
	case year < 2026:
		return []bgeRateWindow{
			{
				start:                    jan1,
				end:                      jun1,
				summer:                   false,
				rDollarsPerKWH:           bgeRPreOct2026NonSummerDollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLPreOct2026NonSummerOnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLPreOct2026NonSummerInterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLPreOct2026NonSummerOffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPreOct2026NonSummerOnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPreOct2026NonSummerOffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPreOct2026OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPreOct2026OffPeakDollarsPerKWH,
			},
			{
				start:                    jun1,
				end:                      oct1,
				summer:                   true,
				rDollarsPerKWH:           bgeRPreOct2026SummerDollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLPreOct2026SummerOnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLPreOct2026SummerInterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLPreOct2026SummerOffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPreOct2026SummerOnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPreOct2026SummerOffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPreOct2026OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPreOct2026OffPeakDollarsPerKWH,
			},
			{
				start:                    oct1,
				end:                      nextJan1,
				summer:                   false,
				rDollarsPerKWH:           bgeRPreOct2026NonSummerDollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLPreOct2026NonSummerOnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLPreOct2026NonSummerInterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLPreOct2026NonSummerOffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPreOct2026NonSummerOnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPreOct2026NonSummerOffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPreOct2026OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPreOct2026OffPeakDollarsPerKWH,
			},
		}

	case year == 2026:
		return []bgeRateWindow{
			{
				start:                    jan1,
				end:                      jun1,
				summer:                   false,
				rDollarsPerKWH:           bgeRPreOct2026NonSummerDollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLPreOct2026NonSummerOnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLPreOct2026NonSummerInterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLPreOct2026NonSummerOffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPreOct2026NonSummerOnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPreOct2026NonSummerOffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPreOct2026OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPreOct2026OffPeakDollarsPerKWH,
			},
			{
				start:                    jun1,
				end:                      oct1,
				summer:                   true,
				rDollarsPerKWH:           bgeRPreOct2026SummerDollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLPreOct2026SummerOnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLPreOct2026SummerInterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLPreOct2026SummerOffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPreOct2026SummerOnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPreOct2026SummerOffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPreOct2026OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPreOct2026OffPeakDollarsPerKWH,
			},
			{
				start:                    oct1,
				end:                      nextJan1,
				summer:                   false,
				rDollarsPerKWH:           bgeROct2026Jan2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLOct2026Jan2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLOct2026Jan2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLOct2026Jan2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVOct2026Jan2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVOct2026Jan2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDOct2026Jan2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDOct2026Jan2027OffPeakDollarsPerKWH,
			},
		}

	case year == 2027:
		feb1 := time.Date(2027, time.February, 1, 0, 0, 0, 0, etLocation)
		return []bgeRateWindow{
			{
				start:                    jan1,
				end:                      feb1,
				summer:                   false,
				rDollarsPerKWH:           bgeROct2026Jan2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLOct2026Jan2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLOct2026Jan2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLOct2026Jan2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVOct2026Jan2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVOct2026Jan2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDOct2026Jan2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDOct2026Jan2027OffPeakDollarsPerKWH,
			},
			{
				start:                    feb1,
				end:                      jun1,
				summer:                   false,
				rDollarsPerKWH:           bgeRNonSummerPostFeb2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLNonSummerPostFeb2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLNonSummerPostFeb2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLNonSummerPostFeb2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPostFeb2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPostFeb2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPostFeb2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPostFeb2027OffPeakDollarsPerKWH,
			},
			{
				start:                    jun1,
				end:                      oct1,
				summer:                   true,
				rDollarsPerKWH:           bgeRSummer2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLSummer2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLSummer2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLSummer2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPostFeb2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPostFeb2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPostFeb2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPostFeb2027OffPeakDollarsPerKWH,
			},
			{
				start:                    oct1,
				end:                      nextJan1,
				summer:                   false,
				rDollarsPerKWH:           bgeRNonSummerPostFeb2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLNonSummerPostFeb2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLNonSummerPostFeb2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLNonSummerPostFeb2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPostFeb2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPostFeb2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPostFeb2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPostFeb2027OffPeakDollarsPerKWH,
			},
		}

	default:
		return []bgeRateWindow{
			{
				start:                    jan1,
				end:                      jun1,
				summer:                   false,
				rDollarsPerKWH:           bgeRNonSummerPostFeb2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLNonSummerPostFeb2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLNonSummerPostFeb2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLNonSummerPostFeb2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPostFeb2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPostFeb2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPostFeb2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPostFeb2027OffPeakDollarsPerKWH,
			},
			{
				start:                    jun1,
				end:                      oct1,
				summer:                   true,
				rDollarsPerKWH:           bgeRSummer2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLSummer2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLSummer2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLSummer2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPostFeb2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPostFeb2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPostFeb2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPostFeb2027OffPeakDollarsPerKWH,
			},
			{
				start:                    oct1,
				end:                      nextJan1,
				summer:                   false,
				rDollarsPerKWH:           bgeRNonSummerPostFeb2027DollarsPerKWH,
				rlOnPeakDollarsPerKWH:    bgeRLNonSummerPostFeb2027OnPeakDollarsPerKWH,
				rlInterPeakDollarsPerKWH: bgeRLNonSummerPostFeb2027InterPeakDollarsPerKWH,
				rlOffPeakDollarsPerKWH:   bgeRLNonSummerPostFeb2027OffPeakDollarsPerKWH,
				evOnPeakDollarsPerKWH:    bgeEVPostFeb2027OnPeakDollarsPerKWH,
				evOffPeakDollarsPerKWH:   bgeEVPostFeb2027OffPeakDollarsPerKWH,
				rdOnPeakDollarsPerKWH:    bgeRDPostFeb2027OnPeakDollarsPerKWH,
				rdOffPeakDollarsPerKWH:   bgeRDPostFeb2027OffPeakDollarsPerKWH,
			},
		}
	}
}

func bgePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getBGEHolidays(year)
		windows := bgeWindowsForYear(year)

		switch plan {
		case "bge_r":
			// Schedule R (Residential Service) - flat delivery/supply
			for _, w := range windows {
				desc := "BGE Schedule R Non-Summer"
				if w.summer {
					desc = "BGE Schedule R Summer"
				}
				periods = append(periods, buildBGEBoundedPeriods(w.start, w.end, []touSimplifiedPeriod{
					{
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								DollarsPerKWH: w.rDollarsPerKWH,
								Description:   desc,
							},
						},
					},
				})...)
			}

		case "bge_rl":
			// Schedule RL (Residential Optional TOU)
			for _, w := range windows {
				if w.summer {
					periods = append(periods, buildBGEBoundedPeriods(w.start, w.end, []touSimplifiedPeriod{
						{
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 20}},
									Weekday:       true,
									DollarsPerKWH: w.rlOnPeakDollarsPerKWH,
									Description:   "BGE Schedule RL Summer Peak",
								},
								{
									Name:          "Inter-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 10}, {HourStart: 20, HourEnd: 23}},
									Weekday:       true,
									DollarsPerKWH: w.rlInterPeakDollarsPerKWH,
									Description:   "BGE Schedule RL Summer Intermediate",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rlOffPeakDollarsPerKWH,
							OtherDescription:   "BGE Schedule RL Summer Off-Peak",
						},
						{
							SpecificDates:      holidays,
							SpecificDatesNot:   false,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rlOffPeakDollarsPerKWH,
							OtherDescription:   "BGE Schedule RL Summer Off-Peak",
						},
					})...)
					continue
				}

				for _, sub := range splitBGENonSummerWindow(year, w.start, w.end) {
					onPeakHours := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 17, HourEnd: 21}}
					interPeakHours := []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 17}}
					peakDesc := "BGE Schedule RL Non-Summer Peak"
					interDesc := "BGE Schedule RL Non-Summer Intermediate"
					offPeakDesc := "BGE Schedule RL Non-Summer Off-Peak"
					if sub.dstShifted {
						onPeakHours = []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 18, HourEnd: 22}}
						interPeakHours = []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 18}}
						peakDesc = "BGE Schedule RL Non-Summer DST Peak"
						interDesc = "BGE Schedule RL Non-Summer DST Intermediate"
						offPeakDesc = "BGE Schedule RL Non-Summer DST Off-Peak"
					}
					periods = append(periods, buildBGEBoundedPeriods(sub.start, sub.end, []touSimplifiedPeriod{
						{
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         onPeakHours,
									Weekday:       true,
									DollarsPerKWH: w.rlOnPeakDollarsPerKWH,
									Description:   peakDesc,
								},
								{
									Name:          "Inter-Peak",
									Hours:         interPeakHours,
									Weekday:       true,
									DollarsPerKWH: w.rlInterPeakDollarsPerKWH,
									Description:   interDesc,
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rlOffPeakDollarsPerKWH,
							OtherDescription:   offPeakDesc,
						},
						{
							SpecificDates:      holidays,
							SpecificDatesNot:   false,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rlOffPeakDollarsPerKWH,
							OtherDescription:   offPeakDesc,
						},
					})...)
				}
			}

		case "bge_ev":
			// Schedule EV (Residential EV TOU)
			for _, w := range windows {
				if w.summer {
					periods = append(periods, buildBGEBoundedPeriods(w.start, w.end, []touSimplifiedPeriod{
						{
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 20}},
									Weekday:       true,
									DollarsPerKWH: w.evOnPeakDollarsPerKWH,
									Description:   "BGE Schedule EV Summer Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.evOffPeakDollarsPerKWH,
							OtherDescription:   "BGE Schedule EV Summer Off-Peak",
						},
						{
							SpecificDates:      holidays,
							SpecificDatesNot:   false,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.evOffPeakDollarsPerKWH,
							OtherDescription:   "BGE Schedule EV Summer Off-Peak",
						},
					})...)
					continue
				}

				for _, sub := range splitBGENonSummerWindow(year, w.start, w.end) {
					onPeakHours := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 17, HourEnd: 21}}
					peakDesc := "BGE Schedule EV Non-Summer Peak"
					offPeakDesc := "BGE Schedule EV Non-Summer Off-Peak"
					if sub.dstShifted {
						onPeakHours = []types.UtilityHourPeriod{{HourStart: 8, HourEnd: 12}, {HourStart: 18, HourEnd: 22}}
						peakDesc = "BGE Schedule EV Non-Summer DST Peak"
						offPeakDesc = "BGE Schedule EV Non-Summer DST Off-Peak"
					}
					periods = append(periods, buildBGEBoundedPeriods(sub.start, sub.end, []touSimplifiedPeriod{
						{
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         onPeakHours,
									Weekday:       true,
									DollarsPerKWH: w.evOnPeakDollarsPerKWH,
									Description:   peakDesc,
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.evOffPeakDollarsPerKWH,
							OtherDescription:   offPeakDesc,
						},
						{
							SpecificDates:      holidays,
							SpecificDatesNot:   false,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.evOffPeakDollarsPerKWH,
							OtherDescription:   offPeakDesc,
						},
					})...)
				}
			}

		case "bge_rd":
			// Schedule RD (Residential Delivery & Energy TOU)
			for _, w := range windows {
				if w.summer {
					periods = append(periods, buildBGEBoundedPeriods(w.start, w.end, []touSimplifiedPeriod{
						{
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 20}},
									Weekday:       true,
									DollarsPerKWH: w.rdOnPeakDollarsPerKWH,
									Description:   "BGE Schedule RD Summer Peak",
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rdOffPeakDollarsPerKWH,
							OtherDescription:   "BGE Schedule RD Summer Off-Peak",
						},
						{
							SpecificDates:      holidays,
							SpecificDatesNot:   false,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rdOffPeakDollarsPerKWH,
							OtherDescription:   "BGE Schedule RD Summer Off-Peak",
						},
					})...)
					continue
				}

				for _, sub := range splitBGENonSummerWindow(year, w.start, w.end) {
					onPeakHours := []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 21}}
					peakDesc := "BGE Schedule RD Non-Summer Peak"
					offPeakDesc := "BGE Schedule RD Non-Summer Off-Peak"
					if sub.dstShifted {
						onPeakHours = []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 10}, {HourStart: 18, HourEnd: 22}}
						peakDesc = "BGE Schedule RD Non-Summer DST Peak"
						offPeakDesc = "BGE Schedule RD Non-Summer DST Off-Peak"
					}
					periods = append(periods, buildBGEBoundedPeriods(sub.start, sub.end, []touSimplifiedPeriod{
						{
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "On-Peak",
									Hours:         onPeakHours,
									Weekday:       true,
									DollarsPerKWH: w.rdOnPeakDollarsPerKWH,
									Description:   peakDesc,
								},
							},
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rdOffPeakDollarsPerKWH,
							OtherDescription:   offPeakDesc,
						},
						{
							SpecificDates:      holidays,
							SpecificDatesNot:   false,
							OtherName:          "Off-Peak",
							OtherDollarsPerKWH: w.rdOffPeakDollarsPerKWH,
							OtherDescription:   offPeakDesc,
						},
					})...)
				}
			}
		}
	}
	return periods
}

func bgeUtilityInfo() types.UtilityProviderInfo {
	bgeOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "BGE net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "bge",
		Name: "Baltimore Gas and Electric (BGE)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "bge_r",
				Name:    "Schedule R (Residential Service)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_r", opts, []int{2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "bge_rl",
				Name:    "Schedule RL (Residential Optional Time-of-Use)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_rl", opts, []int{2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "bge_ev",
				Name:    "Schedule EV (Residential EV Time-of-Use)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_ev", opts, []int{2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "bge_rd",
				Name:    "Schedule RD (Residential Delivery & Energy TOU)",
				Options: bgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return bgePeriods("bge_rd", opts, []int{2026, 2027, 2028}), nil
				},
			},
		},
	}
}
