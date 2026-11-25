package utility

// Tariff Audit Metadata
// - Provider(s): dominion (Dominion Energy)
// - Official Sources:
//   - VA Schedule 1 (eff. 2026-07-01): https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/residential/schedule-1.pdf?rev=8d2a08dc7d4841c4988a5fe9ccacef09&hash=C140BEBDFE1B959182EA80DC80691C31
//   - VA Schedule 1G & Off-Peak Plan: https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/residential/schedule-1g.pdf?rev=d41d09dbc4ac47e493fb11a279437ae5&hash=E3191F8EA616C426C4E9F35095714F01 & https://www.dominionenergy.com/virginia/rates-and-tariffs/off-peak-plan
//   - VA Schedule 1T (eff. 2026-01-01): https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/residential/schedule-1t.pdf?rev=fe7dc78cf078487f91a858a11a6b390d&hash=64B04E63A97AB9FBC9891C8BA234D045
//   - VA Exhibit of Applicable Riders (eff. 2026-11-01), Fuel Charge Rider A (eff. 2026-07-01), Sales & Use Tax Surcharge (eff. 2026-01-01), & Section XXV Net Metering (eff. 2026-02-05): https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/shared/exhibit-of-applicable-riders.pdf?rev=ff66a90282624ac3966adc7908852601&hash=C26BF734EC72726D198D5A02622013A8, https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/shared/rider-a.pdf?rev=9705931cba824652add0ec269b78a854&hash=1FE3B32A727BF5ACAF89055BEF18B3FD, https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/shared/sales-and-use.pdf?rev=9a8ce8841aa142a1a598e40e9820d6b3&hash=4E58107C0565C4ED3DAD6B801F03258F, & https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/virginia/shared/entire-filed-tariff.pdf?rev=143765f25b014defb8575effdf495677&hash=21457C28E5649423F43D1F0221C12F39
//   - NC Exhibit of Applicable Riders (eff. 2026-08-01, Docket E-22, Sub 765) & NC Entire Filing (Section XXV Net Metering): https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/north-carolina/shared/exhibit-of-applicable-riders.pdf?rev=cb5b508611284a3f97bc7a09eb53b932&hash=88F0072913D0C4F0CFAA96A7FFCDDC25 & https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/north-carolina/shared/entire-filing.pdf?rev=18f08051b0064c60a721c6d899e883f0&hash=33F38B36596CB861565063BCA57E9D76
//   - SC Rate 5 & Solar Choice Rider (eff. Jul 2026, PSCSC Order No. 2026-374): https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/south-carolina/electric/rates/rate5.pdf?rev=a67167d53f2542c4b67103d5aad1b9e6&hash=A4A3E23169F85D94B1A6BA39ACC6D3AA & https://cdn-dominionenergy-prd-001.azureedge.net/-/media/content/rates-and-tariffs/pdfs/south-carolina/electric/riders/rider-to-residential-rate5-solar-choice.pdf?rev=ff876095b7344c66b72595420275dbaa&hash=96A706B8098B64FA9312FD5EEEDBEEBC
// - Effective Window: 2025-01-01 through 2028-12-31 (VA riders eff. 2026-11-01, NC riders eff. 2026-08-01, SC Rate 5 eff. Jul 2026 until superseded)
// - Revision Cadence: Periodic / Annual (NC Feb 1 & Aug 1 Fuel/DSM/CCR Riders; VA Jul 1 / Sep 1 / Nov 1 Riders; SC Jul Fuel/DSM Riders)
// - Next Audit Due: 2027-02-01 (NC Riders A, B, C, CCR, CE, RP, RPE annual update effective Feb 1, 2027)
// - Last Verified: 2026-11-24
// - Modeling Notes: Models Tier 1 volumetric charges plus mandatory riders ($0.099873/kWh in VA incl. Rider A $0.037648/kWh & Sales/Use Surcharge $0.000921/kWh; $0.014377/kWh in NC for Riders A, B, B1, C, CCR, CE; embedded fuel/DSM/storm/pension riders on SC Rates 5, 6, 8); excludes fixed monthly/daily customer charges, per-kW demand charges on NC Schedule 1P, and <1% VA consumption tax ($0.001565/kWh); preserves 1:1 net metering on VA Schedules 1 & 1G and NC Schedules 1, 1P & 1T per Section XXV, sets $0.00/kWh export on VA Schedule 1T (excluded from net metering under Section XXV), and supports Solar Choice TOU export credits or 1:1 NEM on SC Rate 5.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// dominionVARidersDollarsPerKWH sums mandatory residential riders in Virginia:
	// Rider A Fuel ($0.037648/kWh), Section I & IV riders from Exhibit of Applicable Riders,
	// and Virginia Sales and Use Tax Surcharge ($0.000921/kWh).
	dominionVARidersDollarsPerKWH = 0.099873

	// dominionNCRidersDollarsPerKWH sums mandatory residential riders in North Carolina
	// (Riders A, B, B1, C, CCR, and CE under NCUC Docket E-22, Sub 765).
	dominionNCRidersDollarsPerKWH = 0.014377
)

func shiftDominionWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getDominionHolidays(year int) []string {
	holidays := []time.Time{
		shiftDominionWeekendHoliday(newYearsDay(year)),
		shiftDominionWeekendHoliday(memorialDay(year)),
		shiftDominionWeekendHoliday(independenceDay(year)),
		shiftDominionWeekendHoliday(laborDay(year)),
		shiftDominionWeekendHoliday(thanksgivingDay(year)),
		shiftDominionWeekendHoliday(christmasDay(year)),
		shiftDominionWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func getDominionNCHolidays(year int) []string {
	thanksgiving := thanksgivingDay(year)
	holidays := []time.Time{
		shiftDominionWeekendHoliday(newYearsDay(year)),
		goodFriday(year),
		memorialDay(year),
		shiftDominionWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgiving,
		thanksgiving.AddDate(0, 0, 1),
		shiftDominionWeekendHoliday(christmasEve(year)),
		shiftDominionWeekendHoliday(christmasDay(year)),
		shiftDominionWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

func dominionPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getDominionHolidays(year)
		ncHolidays := getDominionNCHolidays(year)

		if plan == "dominion_1" {
			// Schedule 1 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.076602 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1 Summer (June-September)",
						},
					},
				},
				// Non-Summer (October - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.075454 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1 Non-Summer (October-May)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1g" {
			// Schedule 1G (Residential Service TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 18}},
							Weekday:       true,
							DollarsPerKWH: 0.210415 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1G Summer On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.033486 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1G Summer Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.051367 + dominionVARidersDollarsPerKWH,
					OtherDescription:   "Dominion Schedule 1G Summer Off-Peak",
				},
				// Summer (May - September) - Holidays
				{
					Year:             year,
					MonthStart:       time.May,
					MonthEnd:         time.September,
					SpecificDates:    holidays,
					SpecificDatesNot: false,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.033486 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1G Summer Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.051367 + dominionVARidersDollarsPerKWH,
					OtherDescription:   "Dominion Schedule 1G Summer Off-Peak",
				},
				// Winter (October - April) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 17, HourEnd: 20}},
							Weekday:       true,
							DollarsPerKWH: 0.170938 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1G Winter On-Peak",
						},
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.049529 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1G Winter Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.055752 + dominionVARidersDollarsPerKWH,
					OtherDescription:   "Dominion Schedule 1G Winter Off-Peak",
				},
				// Winter (October - April) - Holidays
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.April,
					SpecificDates:    holidays,
					SpecificDatesNot: false,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Super Off-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 5}},
							DollarsPerKWH: 0.049529 + dominionVARidersDollarsPerKWH,
							Description:   "Dominion Schedule 1G Winter Super Off-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.055752 + dominionVARidersDollarsPerKWH,
					OtherDescription:   "Dominion Schedule 1G Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1t" {
			// VA Schedule 1T (Residential Service TOU):
			// Base On-Peak = $0.080512 dist + $0.056744 gen + $0.009700 trans = $0.146956/kWh
			// Base Off-Peak = $0.008440 dist + $0.014167 gen + $0.009700 trans = $0.032307/kWh
			// Section XXV excludes energy-only Schedule 1T from net metering ($0.00/kWh export).
			onPeakDollarsPerKWH := 0.146956 + dominionVARidersDollarsPerKWH
			offPeakDollarsPerKWH := 0.032307 + dominionVARidersDollarsPerKWH
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays & Weekends (except holidays)
				{
					Year:                     year,
					MonthStart:               time.June,
					MonthEnd:                 time.September,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 22}},
							Weekday:                       true,
							DollarsPerKWH:                 onPeakDollarsPerKWH,
							GenerationCreditDollarsPerKWH: 0,
							Description:                   "Dominion Schedule 1T Summer On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 offPeakDollarsPerKWH,
					OtherGenerationCreditDollarsPerKWH: 0,
					OtherDescription:                   "Dominion Schedule 1T Summer Off-Peak",
				},
				// Summer (June - September) - Holidays
				{
					Year:                               year,
					MonthStart:                         time.June,
					MonthEnd:                           time.September,
					SpecificDates:                      holidays,
					SpecificDatesNot:                   false,
					SeparateGenerationCredit:           true,
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 offPeakDollarsPerKWH,
					OtherGenerationCreditDollarsPerKWH: 0,
					OtherDescription:                   "Dominion Schedule 1T Summer Off-Peak",
				},
				// Non-Summer (October - May) - Weekdays & Weekends (except holidays)
				{
					Year:                     year,
					MonthStart:               time.October,
					MonthEnd:                 time.May,
					SpecificDates:            holidays,
					SpecificDatesNot:         true,
					SeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 11}, {HourStart: 17, HourEnd: 21}},
							Weekday:                       true,
							DollarsPerKWH:                 onPeakDollarsPerKWH,
							GenerationCreditDollarsPerKWH: 0,
							Description:                   "Dominion Schedule 1T Winter On-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 offPeakDollarsPerKWH,
					OtherGenerationCreditDollarsPerKWH: 0,
					OtherDescription:                   "Dominion Schedule 1T Winter Off-Peak",
				},
				// Non-Summer (October - May) - Holidays
				{
					Year:                               year,
					MonthStart:                         time.October,
					MonthEnd:                           time.May,
					SpecificDates:                      holidays,
					SpecificDatesNot:                   false,
					SeparateGenerationCredit:           true,
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 offPeakDollarsPerKWH,
					OtherGenerationCreditDollarsPerKWH: 0,
					OtherDescription:                   "Dominion Schedule 1T Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1_nc" {
			// NC Schedule 1 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September)
				{
					Year:       year,
					MonthStart: time.June,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.121533 + dominionNCRidersDollarsPerKWH,
							Description:   "Dominion NC Schedule 1 Summer (June-September)",
						},
					},
				},
				// Non-Summer (October - May)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.May,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.105123 + dominionNCRidersDollarsPerKWH,
							Description:   "Dominion NC Schedule 1 Non-Summer (October-May)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1p_nc" {
			// NC Schedule 1P (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.077548 + dominionNCRidersDollarsPerKWH,
							Description:   "Dominion NC Schedule 1P Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1P Summer Off-Peak",
				},
				// Summer (June - September) - Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1P Summer Off-Peak",
				},
				// Winter (October - May) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, MinuteStart: 30, HourEnd: 12}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.077548 + dominionNCRidersDollarsPerKWH,
							Description:   "Dominion NC Schedule 1P Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1P Winter Off-Peak",
				},
				// Winter (October - May) - Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.053685 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1P Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_1t_nc" {
			// NC Schedule 1T (Residential Service TOU)
			simplified := []touSimplifiedPeriod{
				// Summer (June - September) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.June,
					MonthEnd:         time.September,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.269467 + dominionNCRidersDollarsPerKWH,
							Description:   "Dominion NC Schedule 1T Summer On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.062731 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1T Summer Off-Peak",
				},
				// Summer (June - September) - Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.062731 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1T Summer Off-Peak",
				},
				// Winter (October - May) - Weekdays & Weekends (except holidays)
				{
					Year:             year,
					MonthStart:       time.October,
					MonthEnd:         time.May,
					SpecificDates:    ncHolidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 6, MinuteStart: 30, HourEnd: 12}, {HourStart: 17, HourEnd: 21}},
							Weekday:       true,
							DollarsPerKWH: 0.223556 + dominionNCRidersDollarsPerKWH,
							Description:   "Dominion NC Schedule 1T Winter On-Peak",
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.057137 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1T Winter Off-Peak",
				},
				// Winter (October - May) - Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      ncHolidays,
					SpecificDatesNot:   false,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: 0.057137 + dominionNCRidersDollarsPerKWH,
					OtherDescription:   "Dominion NC Schedule 1T Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_5_sc" {
			// SC Rate 5 (Residential Service TOU)
			isSolarChoice := opts.NetMeteringScheme == "solar_choice" || opts.NetMeteringScheme == ""

			summerPeak := 0.29907
			summerSuperOffPeak := 0.09623
			summerOffPeak := 0.15074
			winterPeak := 0.29907
			winterSuperOffPeak := 0.09623
			winterOffPeak := 0.15074

			var summerPeakCredit, summerSuperOffPeakCredit, summerOffPeakCredit float64
			var winterPeakCredit, winterSuperOffPeakCredit, winterOffPeakCredit float64
			if isSolarChoice {
				summerPeakCredit = summerPeak
				summerSuperOffPeakCredit = summerSuperOffPeak
				summerOffPeakCredit = summerOffPeak
				winterPeakCredit = winterPeak
				winterSuperOffPeakCredit = winterSuperOffPeak
				winterOffPeakCredit = winterOffPeak
			}

			simplified := []touSimplifiedPeriod{
				// Summer (May - September)
				{
					Year:                     year,
					MonthStart:               time.May,
					MonthEnd:                 time.September,
					SeparateGenerationCredit: isSolarChoice,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}},
							DollarsPerKWH:                 summerPeak,
							GenerationCreditDollarsPerKWH: summerPeakCredit,
							Description:                   "Dominion SC Rate 5 Summer On-Peak",
						},
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 5}},
							DollarsPerKWH:                 summerSuperOffPeak,
							GenerationCreditDollarsPerKWH: summerSuperOffPeakCredit,
							Description:                   "Dominion SC Rate 5 Summer Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 summerOffPeak,
					OtherGenerationCreditDollarsPerKWH: summerOffPeakCredit,
					OtherDescription:                   "Dominion SC Rate 5 Summer Off-Peak",
				},
				// Winter (October - April)
				{
					Year:                     year,
					MonthStart:               time.October,
					MonthEnd:                 time.April,
					SeparateGenerationCredit: isSolarChoice,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:                          "On-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}},
							DollarsPerKWH:                 winterPeak,
							GenerationCreditDollarsPerKWH: winterPeakCredit,
							Description:                   "Dominion SC Rate 5 Winter On-Peak",
						},
						{
							Name:                          "Super Off-Peak",
							Hours:                         []types.UtilityHourPeriod{{HourStart: 1, HourEnd: 5}, {HourStart: 12, HourEnd: 15}},
							DollarsPerKWH:                 winterSuperOffPeak,
							GenerationCreditDollarsPerKWH: winterSuperOffPeakCredit,
							Description:                   "Dominion SC Rate 5 Winter Super Off-Peak",
						},
					},
					OtherName:                          "Off-Peak",
					OtherDollarsPerKWH:                 winterOffPeak,
					OtherGenerationCreditDollarsPerKWH: winterOffPeakCredit,
					OtherDescription:                   "Dominion SC Rate 5 Winter Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_8_sc" {
			// SC Rate 8 (Residential Service)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September)
				{
					Year:       year,
					MonthStart: time.May,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15878,
							Description:   "Dominion SC Rate 8 Summer (May-September)",
						},
					},
				},
				// Winter (October - April)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.April,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15878,
							Description:   "Dominion SC Rate 8 Winter (October-April)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		} else if plan == "dominion_6_sc" {
			// SC Rate 6 (Residential Service Energy Saver)
			simplified := []touSimplifiedPeriod{
				// Summer (May - September)
				{
					Year:       year,
					MonthStart: time.May,
					MonthEnd:   time.September,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15333,
							Description:   "Dominion SC Rate 6 Summer (May-September)",
						},
					},
				},
				// Winter (October - April)
				{
					Year:       year,
					MonthStart: time.October,
					MonthEnd:   time.April,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							DollarsPerKWH: 0.15333,
							Description:   "Dominion SC Rate 6 Winter (October-April)",
						},
					},
				},
			}
			periods = append(periods, buildPeriods(etLocation, simplified)...)
		}
	}
	return periods
}

func dominionUtilityInfo() types.UtilityProviderInfo {
	dominionOptions := []types.UtilityRateOption{
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "Dominion net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	scRate5Options := []types.UtilityRateOption{
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Solar Choice Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select net metering or Solar Choice for solar billing.",
			Choices: []types.UtilityOptionChoice{
				{Value: "solar_choice", Name: "Solar Choice"},
				{Value: "nem", Name: "Net Metering"},
			},
			Default: "solar_choice",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "dominion",
		Name: "Dominion Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "dominion_1",
				Name:    "VA Schedule 1 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1g",
				Name:    "VA Schedule 1G (Residential Service TOU)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1g", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1t",
				Name:    "VA Schedule 1T (Residential Service TOU)",
				Options: nil,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1t", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1_nc",
				Name:    "NC Schedule 1 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1_nc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1p_nc",
				Name:    "NC Schedule 1P (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1p_nc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_1t_nc",
				Name:    "NC Schedule 1T (Residential Service TOU)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_1t_nc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_5_sc",
				Name:    "SC Rate 5 (Residential Service TOU)",
				Options: scRate5Options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_5_sc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_8_sc",
				Name:    "SC Rate 8 (Residential Service)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_8_sc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
			{
				ID:      "dominion_6_sc",
				Name:    "SC Rate 6 (Residential Service Energy Saver)",
				Options: dominionOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return dominionPeriods("dominion_6_sc", opts, []int{2025, 2026, 2027, 2028}), nil
				},
			},
		},
	}
}
