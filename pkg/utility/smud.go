package utility

// Tariff Audit Metadata
// - Provider(s): smud (Sacramento Municipal Utility District (SMUD))
// - Official Sources:
//   - https://www.smud.org/-/media/Documents/Rate-Information/Rates/1-R-TOD.ashx (Schedule R-TOD Residential Time-of-Day Service, Resolution No. 25-06-15)
//   - https://www.smud.org/Rate-Information/Residential-rates/Time-of-Day-5-8pm-Rate/Rate-details (Time-of-Day 5-8 p.m. Rate Details & Holiday Calendar)
//   - https://www.smud.org/-/media/Documents/Rate-Information/Rates/1-R.ashx (Schedule R Residential Service, Resolution No. 25-06-15)
//   - https://www.smud.org/-/media/Documents/Rate-Information/Rates/01_SSR.ashx (Schedule SSR Solar and Storage Rate, Resolution No. 26-04-04)
//   - https://www.smud.org/-/media/Documents/Rate-Information/Rates/01_NEM1.ashx (Schedule NEM1 Net Energy Metering, Resolution No. 21-09-07)
//   - https://www.smud.org/-/media/Documents/Rate-Information/Rates/01_HGA.ashx (Schedule HGA Hydro Generation Adjustment)
// - Effective Window: 2026-01-01 through 2027-12-31 (2026 & 2027 retail rate steps per Resolution No. 25-06-15; Schedule SSR export step effective 2026-06-01 per Resolution No. 26-04-04)
// - Revision Cadence: Annual (Jan 1 Multi-Year Rate Steps & May Water-Year Schedule HGA Review)
// - Next Audit Due: 2027-05-01 (Annual water-year review of Schedule HGA Hydro Generation Adjustment, currently $0.00/kWh)
// - Last Verified: 2026-11-21
// - Modeling Notes: Models Schedule R-TOD (RT02 default and RTL1 low-use option, plus -$0.0150/kWh PEV Super Off-Peak credit 00:00-06:00 daily) and Schedule R (RF01 closed seasonal flat rate) with Schedule HGA at $0.00/kWh; excludes fixed monthly System Infrastructure Fixed Charges ($27.00/mo in 2026, $27.80/mo in 2027, and $17.00/mo on RTL1); models Schedule SSR export credits ($0.0740/kWh Jan-May 2026 and $0.0960/kWh effective 2026-06-01) and Schedule NEM1 1:1 retail net metering.

import (
	"fmt"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	smudEVCreditDiscountDollarsPerKWH = 0.0150
	smudSSRPreJune2026DollarsPerKWH   = 0.0740
	smudSSREffJune2026DollarsPerKWH   = 0.0960
)

type smudRTODYearRates struct {
	summerPeakDollarsPerKWH       float64
	summerMidPeakDollarsPerKWH    float64
	summerOffPeakDollarsPerKWH    float64
	nonSummerPeakDollarsPerKWH    float64
	nonSummerOffPeakDollarsPerKWH float64
}

// getSMUDHolidays returns the 11 holidays observed by SMUD on Schedule R-TOD.
// Per Schedule R-TOD and SMUD Rate Details, fixed-date holidays falling on a weekend
// do not shift to an adjacent weekday.
func getSMUDHolidays(year int) []string {
	holidays := []time.Time{
		newYearsDay(year),
		martinLutherKingDay(year),
		presidentsDay(year),
		memorialDay(year),
		juneteenth(year),
		independenceDay(year),
		laborDay(year),
		columbusDay(year),
		veteransDay(year),
		thanksgivingDay(year),
		christmasDay(year),
	}
	return formatHolidays(holidays, year)
}

func smudRTODRatesForYear(rateClass string, year int) (smudRTODYearRates, error) {
	switch rateClass {
	case "", "rt02":
		if year >= 2027 {
			return smudRTODYearRates{
				summerPeakDollarsPerKWH:       0.3878,
				summerMidPeakDollarsPerKWH:    0.2203,
				summerOffPeakDollarsPerKWH:    0.1596,
				nonSummerPeakDollarsPerKWH:    0.1829,
				nonSummerOffPeakDollarsPerKWH: 0.1324,
			}, nil
		}
		return smudRTODYearRates{
			summerPeakDollarsPerKWH:       0.3765,
			summerMidPeakDollarsPerKWH:    0.2139,
			summerOffPeakDollarsPerKWH:    0.1550,
			nonSummerPeakDollarsPerKWH:    0.1776,
			nonSummerOffPeakDollarsPerKWH: 0.1285,
		}, nil
	case "rtl1":
		if year >= 2027 {
			return smudRTODYearRates{
				summerPeakDollarsPerKWH:       0.4314,
				summerMidPeakDollarsPerKWH:    0.2610,
				summerOffPeakDollarsPerKWH:    0.1993,
				nonSummerPeakDollarsPerKWH:    0.2231,
				nonSummerOffPeakDollarsPerKWH: 0.1718,
			}, nil
		}
		return smudRTODYearRates{
			summerPeakDollarsPerKWH:       0.4154,
			summerMidPeakDollarsPerKWH:    0.2514,
			summerOffPeakDollarsPerKWH:    0.1920,
			nonSummerPeakDollarsPerKWH:    0.2148,
			nonSummerOffPeakDollarsPerKWH: 0.1654,
		}, nil
	default:
		return smudRTODYearRates{}, fmt.Errorf("unknown SMUD rate class: %s", rateClass)
	}
}

func smudPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	rateClass := strings.ToLower(strings.TrimSpace(opts.RateClass))
	scheme := strings.ToLower(strings.TrimSpace(opts.NetMeteringScheme))

	switch plan {
	case "smud_r_tod":
		if _, err := smudRTODRatesForYear(rateClass, 2026); err != nil {
			return nil, fmt.Errorf("unknown SMUD rate class: %s", opts.RateClass)
		}
		if scheme == "" {
			scheme = "ssr"
		}
		if scheme != "ssr" && scheme != "nem1" {
			return nil, fmt.Errorf("unknown SMUD net metering scheme: %s", opts.NetMeteringScheme)
		}
	case "smud_r":
		if rateClass != "" && rateClass != "rf01" {
			return nil, fmt.Errorf("unknown SMUD rate class: %s", opts.RateClass)
		}
		if scheme == "" {
			scheme = "nem1"
		}
		if scheme != "nem1" {
			return nil, fmt.Errorf("unknown SMUD net metering scheme: %s", opts.NetMeteringScheme)
		}
	default:
		return nil, fmt.Errorf("unknown SMUD rate plan: %s", plan)
	}

	var periods []types.UtilityFeesPeriod

	peakHours := []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 20}}
	summerMidPeakHours := []types.UtilityHourPeriod{
		{HourStart: 12, HourEnd: 17},
		{HourStart: 20, HourEnd: 24},
	}
	superOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}

	for _, year := range years {
		holidays := getSMUDHolidays(year)

		switch plan {
		case "smud_r_tod":
			rates, err := smudRTODRatesForYear(rateClass, year)
			if err != nil {
				return nil, err
			}

			var summerHolidayHoursAndDays []touSimplifiedHoursAndDays
			summerWeekdayHoursAndDays := []touSimplifiedHoursAndDays{
				{
					Name:          "Peak",
					Hours:         peakHours,
					Weekday:       true,
					DollarsPerKWH: rates.summerPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Summer Peak",
				},
				{
					Name:          "Mid-Peak",
					Hours:         summerMidPeakHours,
					Weekday:       true,
					DollarsPerKWH: rates.summerMidPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Summer Mid-Peak",
				},
			}

			var nonSummerHolidayHoursAndDays []touSimplifiedHoursAndDays
			nonSummerWeekdayHoursAndDays := []touSimplifiedHoursAndDays{
				{
					Name:          "Peak",
					Hours:         peakHours,
					Weekday:       true,
					DollarsPerKWH: rates.nonSummerPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Non-Summer Peak",
				},
			}

			if opts.EVCredit {
				summerSuperOffPeakDollarsPerKWH := rates.summerOffPeakDollarsPerKWH - smudEVCreditDiscountDollarsPerKWH
				nonSummerSuperOffPeakDollarsPerKWH := rates.nonSummerOffPeakDollarsPerKWH - smudEVCreditDiscountDollarsPerKWH

				summerHolidayHoursAndDays = append(summerHolidayHoursAndDays, touSimplifiedHoursAndDays{
					Name:          "Super Off-Peak",
					Hours:         superOffPeakHours,
					DollarsPerKWH: summerSuperOffPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Summer Super Off-Peak (Holiday)",
				})
				summerWeekdayHoursAndDays = append(summerWeekdayHoursAndDays, touSimplifiedHoursAndDays{
					Name:          "Super Off-Peak",
					Hours:         superOffPeakHours,
					DollarsPerKWH: summerSuperOffPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Summer Super Off-Peak",
				})
				nonSummerHolidayHoursAndDays = append(nonSummerHolidayHoursAndDays, touSimplifiedHoursAndDays{
					Name:          "Super Off-Peak",
					Hours:         superOffPeakHours,
					DollarsPerKWH: nonSummerSuperOffPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Non-Summer Super Off-Peak (Holiday)",
				})
				nonSummerWeekdayHoursAndDays = append(nonSummerWeekdayHoursAndDays, touSimplifiedHoursAndDays{
					Name:          "Super Off-Peak",
					Hours:         superOffPeakHours,
					DollarsPerKWH: nonSummerSuperOffPeakDollarsPerKWH,
					Description:   "SMUD R-TOD Non-Summer Super Off-Peak",
				})
			}

			simplifiedConsumption := []touSimplifiedPeriod{
				// Summer (June 1 - September 30) Holidays
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					HoursAndDays:       summerHolidayHoursAndDays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: rates.summerOffPeakDollarsPerKWH,
					OtherDescription:   "SMUD R-TOD Summer Off-Peak (Holiday)",
				},
				// Summer (June 1 - September 30) Regular Days
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					SpecificDates:      holidays,
					SpecificDatesNot:   true,
					HoursAndDays:       summerWeekdayHoursAndDays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: rates.summerOffPeakDollarsPerKWH,
					OtherDescription:   "SMUD R-TOD Summer Off-Peak",
				},
				// Non-Summer (October 1 - May 31) Holidays
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					HoursAndDays:       nonSummerHolidayHoursAndDays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: rates.nonSummerOffPeakDollarsPerKWH,
					OtherDescription:   "SMUD R-TOD Non-Summer Off-Peak (Holiday)",
				},
				// Non-Summer (October 1 - May 31) Regular Days
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					SpecificDates:      holidays,
					SpecificDatesNot:   true,
					HoursAndDays:       nonSummerWeekdayHoursAndDays,
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: rates.nonSummerOffPeakDollarsPerKWH,
					OtherDescription:   "SMUD R-TOD Non-Summer Off-Peak",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplifiedConsumption)...)

			if scheme == "ssr" {
				var simplifiedExport []touSimplifiedPeriod
				if year == 2026 {
					simplifiedExport = []touSimplifiedPeriod{
						{
							Year:                               2026,
							MonthStart:                         time.January,
							MonthEnd:                           time.May,
							OnlySeparateGenerationCredit:       true,
							OtherGenerationCreditDollarsPerKWH: smudSSRPreJune2026DollarsPerKWH,
							OtherDescription:                   "SMUD Schedule SSR Export Credit",
						},
						{
							Year:                               2026,
							MonthStart:                         time.June,
							MonthEnd:                           time.December,
							OnlySeparateGenerationCredit:       true,
							OtherGenerationCreditDollarsPerKWH: smudSSREffJune2026DollarsPerKWH,
							OtherDescription:                   "SMUD Schedule SSR Export Credit",
						},
					}
				} else {
					simplifiedExport = []touSimplifiedPeriod{
						{
							Year:                               year,
							MonthStart:                         time.January,
							MonthEnd:                           time.December,
							OnlySeparateGenerationCredit:       true,
							OtherGenerationCreditDollarsPerKWH: smudSSREffJune2026DollarsPerKWH,
							OtherDescription:                   "SMUD Schedule SSR Export Credit",
						},
					}
				}
				periods = append(periods, buildPeriods(ptLocation, simplifiedExport)...)
			}

		case "smud_r":
			summerDollarsPerKWH := 0.2189
			nonSummerDollarsPerKWH := 0.1371
			if year >= 2027 {
				summerDollarsPerKWH = 0.2255
				nonSummerDollarsPerKWH = 0.1412
			}

			simplified := []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.June,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: summerDollarsPerKWH,
					OtherDescription:   "SMUD Schedule R Summer Energy Charge",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.May,
					OtherDollarsPerKWH: nonSummerDollarsPerKWH,
					OtherDescription:   "SMUD Schedule R Non-Summer Energy Charge",
				},
			}
			periods = append(periods, buildPeriods(ptLocation, simplified)...)
		}
	}

	return periods, nil
}

func smudUtilityInfo() types.UtilityProviderInfo {
	rateClassOption := types.UtilityRateOption{
		Field:       "rateClass",
		Name:        "Rate Class",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your Schedule R-TOD fixed charge rate option.",
		Choices: []types.UtilityOptionChoice{
			{Value: "rt02", Name: "Standard Residential Time-of-Day (RT02)"},
			{Value: "rtl1", Name: "Optional Low-Use Fixed Charge (RTL1)"},
		},
		Default: "rt02",
	}

	evCreditOption := types.UtilityRateOption{
		Field:       "evCredit",
		Name:        "Plug-In Electric Vehicle (PEV) Credit",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Are you enrolled in the SMUD Plug-In Electric Vehicle $0.0150/kWh midnight–6 a.m. charging credit?",
		Default:     false,
	}

	rTodExportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "ssr", Name: "Solar and Storage Rate (Schedule SSR)"},
			{Value: "nem1", Name: "Legacy Net Energy Metering (Schedule NEM1)"},
		},
		Default: "ssr",
	}

	rExportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "nem1", Name: "Legacy Net Energy Metering (Schedule NEM1)"},
		},
		Default: "nem1",
	}

	return types.UtilityProviderInfo{
		ID:   "smud",
		Name: "Sacramento Municipal Utility District (SMUD)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "smud_r_tod",
				Name:    "Time-of-Day (5-8 p.m.) Rate (Schedule R-TOD)",
				Options: []types.UtilityRateOption{rateClassOption, evCreditOption, rTodExportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return smudPeriods("smud_r_tod", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "smud_r",
				Name:    "Residential Fixed Rate (Schedule R)",
				Options: []types.UtilityRateOption{rExportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return smudPeriods("smud_r", opts, []int{2026, 2027})
				},
			},
		},
	}
}
