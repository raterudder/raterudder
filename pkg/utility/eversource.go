package utility

// Tariff Audit Metadata
// - Provider(s): eversource (Eversource)
// - Official Sources:
//   - https://www.eversource.com/docs/default-source/rates-tariffs/ct-electric/rate-7-ct.pdf?sfvrsn=8b3bb93f_20 (CT Rate 7 Residential Time-of-Day, PURA Docket No. 26-01-01, eff. 2026-07-01)
//   - https://www.eversource.com/Content/docs/default-source/rates-tariffs/vpp.pdf?sfvrsn=2 (CT Variable Peak Pricing Rider, PURA Docket No. 03-07-02-RE11)
//   - https://www.eversource.com/clp/vpp/vpphistory.aspx (CT Daily VPP Residential Supply Rates)
//   - https://www.eversource.com/docs/default-source/rates-tariffs/nh-summary-rates.pdf?sfvrsn=eefadaef_45 (NH 2026 Summary of Electric Rates, eff. 2026-10-01)
//   - https://www.eversource.com/docs/default-source/rates-tariffs/electric-delivery-service-tariff-nh.pdf?sfvrsn=b0faa55d_44 (NHPUC No. 11: 4th Rev. Pages 41 & 47B Rates R & R-OTOD 2, 1st Rev. Page 87 Default Energy Service, Original Pages 24A-24B Alternative Net Metering)
//   - https://www.eversource.com/residential/about/doing-business-with-us/interconnections/new-hampshire/new-hampshire-net-metering (NH Net Metering Credit Structure)
//   - https://www.eversource.com/docs/default-source/rates-tariffs/ma-electric/1-tariff-ma.pdf?sfvrsn=2c5b9f03_22 (MA Summary of Electric Service Delivery Rates M.D.P.U. No. 1-26-F, eff. 2026-09-01)
//   - https://www.eversource.com/residential/account-billing/manage-bill/about-your-bill/rates-tariffs/electric-supply-rates (MA Basic Service Fixed & Monthly Variable Supply Rates through 2027-01-31)
// - Effective Window: 2026-01-01 through 2027-12-31 (CT eff. 2026-07-01, NH eff. 2026-10-01, MA Basic Service eff. 2026-08-01 through 2027-01-31 carried into 2027 until superseded)
// - Revision Cadence: Semi-Annual (CT Jan 1 & Jul 1; NH & MA Feb 1 & Aug 1; NH Riders Oct 1) & Monthly (MA Variable Supply & Daily CT VPP)
// - Next Audit Due: 2026-12-20 (Check CT Jan 1, 2027 rate/RRES filings and MA Feb 1, 2027 Basic Service supply filing)
// - Last Verified: 2026-11-21
// - Modeling Notes: Preserves Jan-Jun 2026 CT, Jan-Sep 2026 NH, and 2026 EMA historical periods; excludes fixed monthly customer charges; omits immaterial +$0.00055/kWh (+0.15%) Sep 1, 2026 EMA/WMA delivery delta vs. $0.18580/kWh; models CT RRES application-year export adjustments (2022-2026 & legacy net) and NH post-Sep 2017 Alternative Net Metering (SCRC/SBC on gross imports via GridAdditional, export = Default Energy Service + Transmission + 25% Distribution) vs. pre-Sep 2017 1:1 net metering.

import (
	"math"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// roundEversourceDollarsPerKWH rounds per-kWh tariff sums to 5 decimal places ($0.00001/kWh).
func roundEversourceDollarsPerKWH(valDollarsPerKWH float64) float64 {
	return math.Round(valDollarsPerKWH*1e5) / 1e5
}

// shiftEversourceSundayHoliday shifts holidays falling on a Sunday to Monday.
// Saturday holidays do not shift.
func shiftEversourceSundayHoliday(t time.Time) time.Time {
	if t.Weekday() == time.Sunday {
		return t.AddDate(0, 0, 1)
	}
	return t
}

// getEversourceHolidays returns the recognized holidays for Eversource billing.
func getEversourceHolidays(year int) []string {
	holidays := []time.Time{
		shiftEversourceSundayHoliday(newYearsDay(year)),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		shiftEversourceSundayHoliday(independenceDay(year)),
		laborDay(year),
		columbusDay(year),
		shiftEversourceSundayHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftEversourceSundayHoliday(christmasDay(year)),
	}
	return formatHolidays(holidays, year)
}

// eversourceCTGenerationAdjustmentDollarsPerKWH returns the Solar Energy Adjustment Rate in $/kWh
// under the Connecticut Residential Renewable Energy Solutions Rider based on the tariff application year.
//
// Application Year | Netting REC Incentive Rate ($/kWh) | Solar Energy Adjustment Rate ($/kWh)
// 2022             | $0.0318                            | N/A ($0.0000)
// 2023             | $0.0318                            | N/A ($0.0000)
// 2024             | $0.000                             | $0.0000
// 2025             | $0.000                             | ($0.0050)
// 2026             | $0.000                             | ($0.0402)
//
// Prior to January 1, 2022, systems are not eligible under this rider and receive standard net metering ($0.0000 adjustment).
func eversourceCTGenerationAdjustmentDollarsPerKWH(opts types.UtilityRateOptions) float64 {
	yearStr := opts.NetMeteringScheme
	if yearStr == "" {
		yearStr = opts.ApplicationYear
	}
	switch strings.ToLower(strings.TrimSpace(yearStr)) {
	case "2026":
		return -0.0402
	case "2025":
		return -0.0050
	case "2024", "2023", "2022", "net":
		return 0.0
	default:
		// Default to 2026 tariff rate (-$0.0402/kWh)
		return -0.0402
	}
}

// eversourcePeriods generates the fees period slice for a specific Eversource rate plan.
func eversourcePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		switch plan {
		case "eversource_ct_rate_1", "eversource_ct_rate_5", "eversource_ct_rate_7", "eversource_ct_vpp":
			periods = append(periods, eversourceCTPeriods(plan, opts, year)...)
		case "eversource_nh_rate_r", "eversource_nh_rate_r_otod":
			periods = append(periods, eversourceNHPeriods(plan, opts, year)...)
		case "eversource_ma_residential":
			periods = append(periods, eversourceMAPeriods(opts, year)...)
		}
	}
	return periods
}

// eversourceCTPeriods builds Connecticut residential tariff periods for a single calendar year.
func eversourceCTPeriods(plan string, opts types.UtilityRateOptions, year int) []types.UtilityFeesPeriod {
	ctGenAdjustment := eversourceCTGenerationAdjustmentDollarsPerKWH(opts)

	switch plan {
	case "eversource_ct_rate_1":
		// CT Residential Flat (Rate 1)
		// Total rate: $0.24666/kWh (before July 1, 2026), $0.23602/kWh (starting July 1, 2026)
		return eversourceCTFlatPeriods(year, 0.24666, 0.23602, "Eversource CT Rate 1 Flat", ctGenAdjustment)

	case "eversource_ct_rate_5":
		// CT Residential Heating (Rate 5)
		// Total rate: $0.22277/kWh (before July 1, 2026), $0.21213/kWh (starting July 1, 2026)
		return eversourceCTFlatPeriods(year, 0.22277, 0.21213, "Eversource CT Rate 5 Heating Flat", ctGenAdjustment)

	case "eversource_ct_rate_7":
		// CT Residential Time-of-Day (Rate 7)
		// On-Peak: Weekdays 12 Noon – 8 p.m. Rate: $0.31099/kWh (before July 1, 2026), $0.29982/kWh (starting July 1, 2026)
		// Off-Peak: All other hours. Rate: $0.21871/kWh (before July 1, 2026), $0.20754/kWh (starting July 1, 2026)
		var simplified []touSimplifiedPeriod
		if year == 2026 {
			simplified = []touSimplifiedPeriod{
				eversourceCTRate7Period(year, time.January, time.June, 0.31099, 0.21871, "Eversource CT Rate 7 On-Peak", "Eversource CT Rate 7 Off-Peak", ctGenAdjustment),
				eversourceCTRate7Period(year, time.July, time.December, 0.29982, 0.20754, "Eversource CT Rate 7 On-Peak", "Eversource CT Rate 7 Off-Peak", ctGenAdjustment),
			}
		} else {
			simplified = []touSimplifiedPeriod{
				eversourceCTRate7Period(year, time.January, time.December, 0.29982, 0.20754, "Eversource CT Rate 7 On-Peak", "Eversource CT Rate 7 Off-Peak", ctGenAdjustment),
			}
		}
		return buildPeriods(etLocation, simplified)

	case "eversource_ct_vpp":
		// CT Residential Variable Peak Pricing (Rate 7 VPP)
		// Rider VPP replaces only the Rate 7 Generation Service charge and bills Rate 7 delivery & FMCC time-of-day periods:
		// Before July 1, 2026: historical flat $0.12078/kWh preserved for determinism.
		// Starting July 1, 2026 (PURA Docket No. 26-01-01):
		//   - On-Peak (Weekdays 12:00–20:00 ET): $0.15853 delivery - $0.00210 FMCC-Generation = $0.15643/kWh
		//   - Off-Peak (All other hours):        $0.10125 delivery - $0.00210 FMCC-Generation = $0.09915/kWh
		var simplified []touSimplifiedPeriod
		if year == 2026 {
			simplified = []touSimplifiedPeriod{
				{
					Year:                                   year,
					MonthStart:                             time.January,
					MonthEnd:                               time.June,
					OtherDollarsPerKWH:                     0.12078,
					OtherDescription:                       "Eversource CT Rate 7 Delivery & FMCC Adjustment",
					OtherGenerationAdjustmentDollarsPerKWH: ctGenAdjustment,
				},
				eversourceCTRate7Period(
					year,
					time.July,
					time.December,
					0.15643,
					0.09915,
					"Eversource CT Rate 7 On-Peak Delivery & FMCC Adjustment",
					"Eversource CT Rate 7 Off-Peak Delivery & FMCC Adjustment",
					ctGenAdjustment,
				),
			}
		} else {
			simplified = []touSimplifiedPeriod{
				eversourceCTRate7Period(
					year,
					time.January,
					time.December,
					0.15643,
					0.09915,
					"Eversource CT Rate 7 On-Peak Delivery & FMCC Adjustment",
					"Eversource CT Rate 7 Off-Peak Delivery & FMCC Adjustment",
					ctGenAdjustment,
				),
			}
		}
		return buildPeriods(etLocation, simplified)
	}
	return nil
}

func eversourceCTFlatPeriods(year int, janJun2026DollarsPerKWH, jul2026PlusDollarsPerKWH float64, desc string, ctGenAdjustment float64) []types.UtilityFeesPeriod {
	var simplified []touSimplifiedPeriod
	if year == 2026 {
		simplified = []touSimplifiedPeriod{
			{
				Year:                                   year,
				MonthStart:                             time.January,
				MonthEnd:                               time.June,
				OtherDollarsPerKWH:                     janJun2026DollarsPerKWH,
				OtherDescription:                       desc,
				OtherGenerationAdjustmentDollarsPerKWH: ctGenAdjustment,
			},
			{
				Year:                                   year,
				MonthStart:                             time.July,
				MonthEnd:                               time.December,
				OtherDollarsPerKWH:                     jul2026PlusDollarsPerKWH,
				OtherDescription:                       desc,
				OtherGenerationAdjustmentDollarsPerKWH: ctGenAdjustment,
			},
		}
	} else {
		simplified = []touSimplifiedPeriod{
			{
				Year:                                   year,
				MonthStart:                             time.January,
				MonthEnd:                               time.December,
				OtherDollarsPerKWH:                     jul2026PlusDollarsPerKWH,
				OtherDescription:                       desc,
				OtherGenerationAdjustmentDollarsPerKWH: ctGenAdjustment,
			},
		}
	}
	return buildPeriods(etLocation, simplified)
}

func eversourceCTRate7Period(
	year int,
	monthStart, monthEnd time.Month,
	onPeakDollarsPerKWH, offPeakDollarsPerKWH float64,
	onPeakDesc, offPeakDesc string,
	ctGenAdjustment float64,
) touSimplifiedPeriod {
	return touSimplifiedPeriod{
		Year:       year,
		MonthStart: monthStart,
		MonthEnd:   monthEnd,
		HoursAndDays: []touSimplifiedHoursAndDays{
			{
				Name:                              "On-Peak",
				Hours:                             []types.UtilityHourPeriod{{HourStart: 12, HourEnd: 20}},
				Weekday:                           true,
				DollarsPerKWH:                     onPeakDollarsPerKWH,
				Description:                       onPeakDesc,
				GenerationAdjustmentDollarsPerKWH: ctGenAdjustment,
			},
		},
		OtherName:                              "Off-Peak",
		OtherDollarsPerKWH:                     offPeakDollarsPerKWH,
		OtherDescription:                       offPeakDesc,
		OtherGenerationAdjustmentDollarsPerKWH: ctGenAdjustment,
	}
}

// eversourceNHPeriods builds New Hampshire residential tariff periods for a single calendar year.
func eversourceNHPeriods(plan string, opts types.UtilityRateOptions, year int) []types.UtilityFeesPeriod {
	isStandardNetMetering := strings.EqualFold(strings.TrimSpace(opts.NetMeteringScheme), "net")

	switch plan {
	case "eversource_nh_rate_r":
		return eversourceNHRateRPeriods(year, isStandardNetMetering)
	case "eversource_nh_rate_r_otod":
		return eversourceNHRateROTODPeriods(year, isStandardNetMetering, getEversourceHolidays(year))
	}
	return nil
}

func eversourceNHGridAdditionalPeriods(year int, oct2026PlusAdditionalDollarsPerKWH float64, desc string) []types.UtilityFeesPeriod {
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)
	if year < 2026 {
		return []types.UtilityFeesPeriod{
			{
				TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: etLocation},
				DollarsPerKWH:  0.0,
				GridAdditional: true,
				Description:    desc,
			},
		}
	}
	if year == 2026 {
		oct1 := time.Date(2026, time.October, 1, 0, 0, 0, 0, etLocation)
		return []types.UtilityFeesPeriod{
			{
				TimePeriod:     types.TimePeriod{Start: yearStart, End: oct1, LocationPtr: etLocation},
				DollarsPerKWH:  0.0,
				GridAdditional: true,
				Description:    desc,
			},
			{
				TimePeriod:     types.TimePeriod{Start: oct1, End: yearEnd, LocationPtr: etLocation},
				DollarsPerKWH:  oct2026PlusAdditionalDollarsPerKWH,
				GridAdditional: true,
				Description:    desc,
			},
		}
	}
	return []types.UtilityFeesPeriod{
		{
			TimePeriod:     types.TimePeriod{Start: yearStart, End: yearEnd, LocationPtr: etLocation},
			DollarsPerKWH:  oct2026PlusAdditionalDollarsPerKWH,
			GridAdditional: true,
			Description:    desc,
		},
	}
}

func eversourceNHRateRPeriods(year int, isStandardNetMetering bool) []types.UtilityFeesPeriod {
	// NHPUC No. 11 Rate R (eff. October 1, 2026):
	// Retail import = $0.25762/kWh.
	// Post-Sep 2017 Alternative Net Metering:
	//   - SCRC + SBC non-bypassable gross import charges = $0.00470/kWh (GridAdditional)
	//   - Base creditable rate = $0.25292/kWh
	//   - Export adjustment = -$0.05045/kWh ($0.20247/kWh export = Default Energy Service + Transmission + 25% Distribution)
	// Pre-Sep 2017 Standard Net Metering ("net"): full retail $0.25762/kWh 1:1 without GridAdditional or adjustments.
	octBaseDollarsPerKWH := 0.25292
	octGenAdjDollarsPerKWH := -0.05045
	if isStandardNetMetering {
		octBaseDollarsPerKWH = 0.25762
		octGenAdjDollarsPerKWH = 0.0
	}

	var simplified []touSimplifiedPeriod
	if year == 2026 {
		simplified = []touSimplifiedPeriod{
			{
				Year:               year,
				MonthStart:         time.January,
				MonthEnd:           time.January,
				OtherDollarsPerKWH: 0.23128,
				OtherDescription:   "Eversource NH Rate R Flat",
			},
			{
				Year:               year,
				MonthStart:         time.February,
				MonthEnd:           time.September,
				OtherDollarsPerKWH: 0.23390,
				OtherDescription:   "Eversource NH Rate R Flat",
			},
			{
				Year:                                   year,
				MonthStart:                             time.October,
				MonthEnd:                               time.December,
				OtherDollarsPerKWH:                     octBaseDollarsPerKWH,
				OtherDescription:                       "Eversource NH Rate R Flat",
				OtherGenerationAdjustmentDollarsPerKWH: octGenAdjDollarsPerKWH,
			},
		}
	} else {
		simplified = []touSimplifiedPeriod{
			{
				Year:                                   year,
				MonthStart:                             time.January,
				MonthEnd:                               time.December,
				OtherDollarsPerKWH:                     octBaseDollarsPerKWH,
				OtherDescription:                       "Eversource NH Rate R Flat",
				OtherGenerationAdjustmentDollarsPerKWH: octGenAdjDollarsPerKWH,
			},
		}
	}

	periods := buildPeriods(etLocation, simplified)
	if !isStandardNetMetering {
		periods = append(periods, eversourceNHGridAdditionalPeriods(year, 0.00470, "Eversource NH Rate R SCRC & SBC Non-Bypassable Charges")...)
	}
	return periods
}

func eversourceNHROTODWindowPeriods(
	year int,
	monthStart, monthEnd time.Month,
	holidays []string,
	onPeakDollarsPerKWH, offPeakDollarsPerKWH float64,
	onPeakGenAdjDollarsPerKWH, offPeakGenAdjDollarsPerKWH float64,
) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		{
			Year:                                   year,
			MonthStart:                             monthStart,
			MonthEnd:                               monthEnd,
			SpecificDates:                          holidays,
			OtherName:                              "Off-Peak",
			OtherDollarsPerKWH:                     offPeakDollarsPerKWH,
			OtherDescription:                       "Eversource NH Rate R-OTOD Off-Peak (Holiday)",
			OtherGenerationAdjustmentDollarsPerKWH: offPeakGenAdjDollarsPerKWH,
		},
		{
			Year:             year,
			MonthStart:       monthStart,
			MonthEnd:         monthEnd,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:                              "On-Peak",
					Hours:                             []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 19}},
					Weekday:                           true,
					DollarsPerKWH:                     onPeakDollarsPerKWH,
					Description:                       "Eversource NH Rate R-OTOD On-Peak",
					GenerationAdjustmentDollarsPerKWH: onPeakGenAdjDollarsPerKWH,
				},
			},
			OtherName:                              "Off-Peak",
			OtherDollarsPerKWH:                     offPeakDollarsPerKWH,
			OtherDescription:                       "Eversource NH Rate R-OTOD Off-Peak",
			OtherGenerationAdjustmentDollarsPerKWH: offPeakGenAdjDollarsPerKWH,
		},
	}
}

func eversourceNHRateROTODPeriods(year int, isStandardNetMetering bool, holidays []string) []types.UtilityFeesPeriod {
	// NHPUC No. 11 Rate R-OTOD 2 (eff. October 1, 2026):
	// Weekdays 1:00 p.m. – 7:00 p.m. ET excluding holidays.
	// Retail import = $0.37091/kWh On-Peak, $0.22283/kWh Off-Peak.
	// Post-Sep 2017 Alternative Net Metering:
	//   - SCRC + SBC non-bypassable gross import charges = $0.00368/kWh (GridAdditional)
	//   - Base creditable rates = $0.36723/kWh On-Peak, $0.21915/kWh Off-Peak
	//   - Export adjustments = -$0.06076/kWh On-Peak ($0.30647/kWh export), -$0.04457/kWh Off-Peak ($0.17458/kWh export)
	// Pre-Sep 2017 Standard Net Metering ("net"): full retail $0.37091/$0.22283/kWh 1:1 without GridAdditional or adjustments.
	octOnPeakDollarsPerKWH := 0.36723
	octOffPeakDollarsPerKWH := 0.21915
	octOnPeakGenAdjDollarsPerKWH := -0.06076
	octOffPeakGenAdjDollarsPerKWH := -0.04457
	if isStandardNetMetering {
		octOnPeakDollarsPerKWH = 0.37091
		octOffPeakDollarsPerKWH = 0.22283
		octOnPeakGenAdjDollarsPerKWH = 0.0
		octOffPeakGenAdjDollarsPerKWH = 0.0
	}

	var simplified []touSimplifiedPeriod
	if year == 2026 {
		simplified = append(simplified, eversourceNHROTODWindowPeriods(year, time.January, time.January, holidays, 0.34792, 0.19583, 0.0, 0.0)...)
		simplified = append(simplified, eversourceNHROTODWindowPeriods(year, time.February, time.September, holidays, 0.35054, 0.19845, 0.0, 0.0)...)
		simplified = append(simplified, eversourceNHROTODWindowPeriods(
			year,
			time.October,
			time.December,
			holidays,
			octOnPeakDollarsPerKWH,
			octOffPeakDollarsPerKWH,
			octOnPeakGenAdjDollarsPerKWH,
			octOffPeakGenAdjDollarsPerKWH,
		)...)
	} else {
		simplified = eversourceNHROTODWindowPeriods(
			year,
			time.January,
			time.December,
			holidays,
			octOnPeakDollarsPerKWH,
			octOffPeakDollarsPerKWH,
			octOnPeakGenAdjDollarsPerKWH,
			octOffPeakGenAdjDollarsPerKWH,
		)
	}

	periods := buildPeriods(etLocation, simplified)
	if !isStandardNetMetering {
		periods = append(periods, eversourceNHGridAdditionalPeriods(year, 0.00368, "Eversource NH Rate R-OTOD SCRC & SBC Non-Bypassable Charges")...)
	}
	return periods
}

var (
	// EMA / Cape Cod 2026 monthly Basic Service supply rates ($/kWh), Feb–Dec 2026.
	eversourceEMA2026MonthlySupplyDollarsPerKWH = [13]float64{
		time.January:   0.11891, // Preserves Jan 2026 baseline ($0.18580 + $0.11891 = $0.30471/kWh on EMA)
		time.February:  0.21618,
		time.March:     0.15208,
		time.April:     0.13302,
		time.May:       0.12538,
		time.June:      0.13062,
		time.July:      0.16217,
		time.August:    0.14255,
		time.September: 0.12888,
		time.October:   0.13002,
		time.November:  0.14815,
		time.December:  0.21054,
	}

	// WMA 2026 monthly Basic Service supply rates ($/kWh), Feb–Dec 2026.
	eversourceWMA2026MonthlySupplyDollarsPerKWH = [13]float64{
		time.January:   0.11891,
		time.February:  0.18025,
		time.March:     0.13312,
		time.April:     0.12093,
		time.May:       0.11454,
		time.June:      0.11848,
		time.July:      0.14040,
		time.August:    0.13191,
		time.September: 0.11620,
		time.October:   0.11617,
		time.November:  0.13346,
		time.December:  0.19419,
	}
)

// eversourceMAPeriods builds Massachusetts Rate R-1 residential tariff periods for a single calendar year.
func eversourceMAPeriods(opts types.UtilityRateOptions, year int) []types.UtilityFeesPeriod {
	// Delivery rates per M.D.P.U. No. 1-26-F:
	// - EMA & WMA retain $0.18580/kWh (omitting the +$0.00055/kWh / +0.15% Sep 1, 2026 adjustment).
	// - Cape Cod & Martha's Vineyard ("cape") delivery is $0.20081/kWh.
	loc := strings.ToLower(strings.TrimSpace(opts.Location))
	deliveryDollarsPerKWH := 0.18580
	if loc == "cape" {
		deliveryDollarsPerKWH = 0.20081
	}
	isWMA := loc == "wma"

	febJul2026FixedSupplyDollarsPerKWH := 0.15629
	aug2026PlusFixedSupplyDollarsPerKWH := 0.17323
	jan2027MonthlySupplyDollarsPerKWH := 0.25510
	monthlySupply2026 := eversourceEMA2026MonthlySupplyDollarsPerKWH
	if isWMA {
		febJul2026FixedSupplyDollarsPerKWH = 0.13683
		aug2026PlusFixedSupplyDollarsPerKWH = 0.15934
		jan2027MonthlySupplyDollarsPerKWH = 0.23742
		monthlySupply2026 = eversourceWMA2026MonthlySupplyDollarsPerKWH
	}

	const maDesc = "Eversource MA Rate R-1 Flat"
	genRate := strings.ToLower(strings.TrimSpace(opts.GenerationRate))

	var simplified []touSimplifiedPeriod
	if genRate == "monthly" {
		switch {
		case year == 2026:
			simplified = make([]touSimplifiedPeriod, 0, 12)
			for m := time.January; m <= time.December; m++ {
				simplified = append(simplified, touSimplifiedPeriod{
					Year:               year,
					MonthStart:         m,
					MonthEnd:           m,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + monthlySupply2026[m]),
					OtherDescription:   maDesc,
				})
			}
		case year == 2027:
			// Published monthly Basic Service rates end January 31, 2027; Feb–Dec 2027 fall back to fixed supply.
			simplified = []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.January,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + jan2027MonthlySupplyDollarsPerKWH),
					OtherDescription:   maDesc,
				},
				{
					Year:               year,
					MonthStart:         time.February,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + aug2026PlusFixedSupplyDollarsPerKWH),
					OtherDescription:   maDesc,
				},
			}
		default:
			simplified = []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + aug2026PlusFixedSupplyDollarsPerKWH),
					OtherDescription:   maDesc,
				},
			}
		}
	} else {
		if year == 2026 {
			simplified = []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.January,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + monthlySupply2026[time.January]),
					OtherDescription:   maDesc,
				},
				{
					Year:               year,
					MonthStart:         time.February,
					MonthEnd:           time.July,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + febJul2026FixedSupplyDollarsPerKWH),
					OtherDescription:   maDesc,
				},
				{
					Year:               year,
					MonthStart:         time.August,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + aug2026PlusFixedSupplyDollarsPerKWH),
					OtherDescription:   maDesc,
				},
			}
		} else {
			simplified = []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: roundEversourceDollarsPerKWH(deliveryDollarsPerKWH + aug2026PlusFixedSupplyDollarsPerKWH),
					OtherDescription:   maDesc,
				},
			}
		}
	}
	return buildPeriods(etLocation, simplified)
}

// eversourceUtilityInfo returns the metadata and rate options for Eversource.
func eversourceUtilityInfo() types.UtilityProviderInfo {
	eversourceCTOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Installation Year",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your solar tariff application year (typically the year your system was installed).",
			Choices: []types.UtilityOptionChoice{
				{Value: "2026", Name: "2026 (-$0.0402/kWh)"},
				{Value: "2025", Name: "2025 (-$0.0050/kWh)"},
				{Value: "2024", Name: "2024 ($0.0000/kWh)"},
				{Value: "2023", Name: "2023 ($0.0000/kWh)"},
				{Value: "2022", Name: "2022 ($0.0000/kWh)"},
				{Value: "net", Name: "Prior to 2022 (Legacy Net Metering)"},
			},
			Default: "2026",
		},
		{
			Field:       "vppProgram",
			Name:        "Mandatory VPP Events",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your Virtual Power Plant (VPP) program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "none", Name: "None"},
				{Value: "ess-passive", Name: "Energy Storage Solutions Legacy Passive Dispatch"},
			},
			Default: "none",
		},
	}

	eversourceNHOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering Tariff",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your New Hampshire net metering tariff based on when your system was interconnected.",
			Choices: []types.UtilityOptionChoice{
				{Value: "alternative", Name: "Alternative Net Metering (Post-September 2017)"},
				{Value: "net", Name: "Standard 1:1 Net Metering (Pre-September 2017)"},
			},
			Default: "alternative",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "eversource",
		Name: "Eversource",
		Rates: []types.UtilityRateInfo{
			// Connecticut
			{
				ID:      "eversource_ct_rate_1",
				Name:    "CT Residential Flat (Rate 1)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_1", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			{
				ID:      "eversource_ct_rate_5",
				Name:    "CT Residential Heating (Rate 5)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_5", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			{
				ID:      "eversource_ct_rate_7",
				Name:    "CT Residential Time-of-Day (Rate 7)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_rate_7", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			{
				ID:      "eversource_ct_vpp",
				Name:    "CT Residential Variable Peak Pricing (Rate 7 VPP)",
				Options: eversourceCTOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ct_vpp", opts, []int{2026, 2027}), nil
				},
				GetVPP: func(opts types.UtilityRateOptions) (types.UtilityVPPInfo, error) {
					return eversourceVPPPeriods(opts, []int{2026, 2027})
				},
			},
			// New Hampshire
			{
				ID:      "eversource_nh_rate_r",
				Name:    "NH Residential Standard (Rate R)",
				Options: eversourceNHOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "eversource_nh_rate_r_otod",
				Name:    "NH Residential Time-of-Day (Rate R-OTOD)",
				Options: eversourceNHOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_nh_rate_r_otod", opts, []int{2026, 2027}), nil
				},
			},
			// Massachusetts
			{
				ID:   "eversource_ma_residential",
				Name: "MA Residential Standard (Rate R-1)",
				Options: []types.UtilityRateOption{
					{
						Field:       "location",
						Name:        "Service Area",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your Massachusetts Eversource service territory.",
						Choices: []types.UtilityOptionChoice{
							{Value: "ema", Name: "Eastern Massachusetts (Greater Boston / MetroWest / South Shore)"},
							{Value: "cape", Name: "Cape Cod & Martha's Vineyard"},
							{Value: "wma", Name: "Western Massachusetts"},
						},
						Default: "ema",
					},
					{
						Field:       "generationRate",
						Name:        "Supply Rate Option",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your Eversource Basic Service supply rate type.",
						Choices: []types.UtilityOptionChoice{
							{Value: "fixed", Name: "Fixed Supply Rate"},
							{Value: "monthly", Name: "Monthly Supply Rate"},
						},
						Default: "fixed",
					},
				},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return eversourcePeriods("eversource_ma_residential", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
