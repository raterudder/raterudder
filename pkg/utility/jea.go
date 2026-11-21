package utility

// Tariff Audit Metadata
// - Provider(s): jea (JEA (Jacksonville))
// - Official Sources:
//   - https://www.jea.com/rates (Monthly Fuel Rates for Nov–Dec 2026 and GST Time-of-Day Fuel Rates for Nov 2026)
//   - https://www.jea.com/Pdf/Download/12884909743 (JEA Electric Tariff Volume 2 effective October 1, 2026: Sheet No. 4.0 Rate R & Fourth Revised Sheet No. 5.1 Rate GST)
//   - https://www.jea.com/Pdf/Download/12884923000 (JEA Distributed Generation Policy, DG-1 monthly fuel rate export credit)
// - Effective Window: 2026-01-01 through 2027-12-31 (Oct 1, 2026 base rates & Sheet 5.1 peak windows; Nov–Dec 2026 monthly fuel rates with Dec 2026 fallback and Nov 2026 GST TOU fuel carried forward until superseded)
// - Revision Cadence: Monthly (Fuel & Purchased Power Charge) & Annual (Oct 1 Fiscal Year Base Tariff)
// - Next Audit Due: 2026-12-05 (Check December 2026 GST TOU fuel rates and January 2027 monthly fuel rate on https://www.jea.com/rates)
// - Last Verified: 2026-11-21
// - Modeling Notes: Models Rate R Tier 1 residential volumetric energy + monthly fuel charge and Rate GST on-peak/off-peak base + TOU fuel charges (flat monthly fuel prior to Nov 2026; $0.05310/$0.04828/kWh TOU fuel from Nov 2026 carried forward until superseded); fixed monthly Basic Monthly Charges excluded; Net Energy Metering ("net") is 1:1 kWh netting and Distributed Generation ("dg") credits exported energy at the levelized monthly fuel rate on both Rate R and Rate GST.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftJEAWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getJEAHolidays(year int) []string {
	holidays := []time.Time{
		shiftJEAWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftJEAWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftJEAWeekendHoliday(christmasDay(year)),
		shiftJEAWeekendHoliday(newYearsDay(year + 1)),
	}
	return formatHolidays(holidays, year)
}

// Comes from https://www.jea.com/rates "Fuel Rates" and Electric Tariff effective October 1, 2026
func getJEAFuelCharge(year int, month time.Month) float64 {
	if year == 2026 {
		switch month {
		case time.January:
			return 0.04224
		case time.February:
			return 0.04144
		case time.March:
			return 0.04973
		case time.April:
			return 0.05968
		case time.May:
			return 0.05863
		case time.June:
			return 0.04494
		case time.July:
			return 0.04386
		case time.August, time.September:
			return 0.04282
		case time.October:
			return 0.04326
		case time.November:
			return 0.04975
		case time.December:
			return 0.04861
		}
	}
	return 0.04861
}

// getJEAGSTFuelDollarsPerKWH returns the On-Peak and Off-Peak fuel charges ($/kWh) for Rate GST.
// Starting November 2026, JEA applies TOU-differentiated fuel rates on Rate GST ($0.05310/kWh on-peak,
// $0.04828/kWh off-peak per https://www.jea.com/rates), carried forward until superseded.
func getJEAGSTFuelDollarsPerKWH(year int, month time.Month) (onPeakDollarsPerKWH, offPeakDollarsPerKWH float64) {
	if year > 2026 || (year == 2026 && month >= time.November) {
		return 0.05310, 0.04828
	}
	flatFuelDollarsPerKWH := getJEAFuelCharge(year, month)
	return flatFuelDollarsPerKWH, flatFuelDollarsPerKWH
}

type jeaBaseRates struct {
	rsBase         float64
	gstOnPeakBase  float64
	gstOffPeakBase float64
}

// JEA base rates are established by approved tariff documents (e.g. Electric Tariff Sheet 4.0 & 5.1).
// These change with periodic tariff revisions (such as at the start of JEA fiscal years on Oct 1),
// not with monthly fuel cost adjustments.
var jeaBaseRateSchedules = []struct {
	effectiveDate time.Time
	rates         jeaBaseRates
}{
	// Prior to October 1, 2026
	{
		effectiveDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		rates: jeaBaseRates{
			rsBase:         0.07237,
			gstOnPeakBase:  0.13776,
			gstOffPeakBase: 0.04535,
		},
	},
	// Effective October 1, 2026 (Electric Tariff Document October 2026, Sheets 4.0 and 5.1)
	{
		effectiveDate: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		rates: jeaBaseRates{
			rsBase:         0.07815,
			gstOnPeakBase:  0.14195,
			gstOffPeakBase: 0.04732,
		},
	},
}

func getJEABaseRates(year int, month time.Month) jeaBaseRates {
	target := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	var effDate time.Time
	chosen := jeaBaseRateSchedules[0].rates
	// start at the end because the latest ones are at the bottom and find the newest rates
	// that have an effective date <= target month
	for i := len(jeaBaseRateSchedules) - 1; i >= 0; i-- {
		schedule := jeaBaseRateSchedules[i]
		if !target.Before(schedule.effectiveDate) && schedule.effectiveDate.After(effDate) {
			chosen = schedule.rates
			effDate = schedule.effectiveDate
		}
	}
	return chosen
}

func jeaPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		holidays := getJEAHolidays(year)

		for month := time.January; month <= time.December; month++ {
			fuelCharge := getJEAFuelCharge(year, month)
			baseRates := getJEABaseRates(year, month)

			switch plan {
			case "jea_r":
				// Rate R (Residential Service)
				rate := baseRates.rsBase + fuelCharge

				simplified := []touSimplifiedPeriod{
					{
						Year:               year,
						Months:             []time.Month{month},
						OtherDollarsPerKWH: rate,
						OtherDescription:   "JEA RS Energy Charge",
					},
				}

				if opts.NetMeteringScheme == "dg" {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:                               year,
						Months:                             []time.Month{month},
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: fuelCharge,
						OtherDescription:                   "JEA DG Generation Credit",
					})
				}

				periods = append(periods, buildPeriods(etLocation, simplified)...)

			case "jea_gst":
				// Rate GST (General Service Time-of-Day)
				gstOnPeakFuelDollarsPerKWH, gstOffPeakFuelDollarsPerKWH := getJEAGSTFuelDollarsPerKWH(year, month)
				onPeakDollarsPerKWH := baseRates.gstOnPeakBase + gstOnPeakFuelDollarsPerKWH
				offPeakDollarsPerKWH := baseRates.gstOffPeakBase + gstOffPeakFuelDollarsPerKWH

				isWinter := month == time.November || month == time.December || month == time.January || month == time.February || month == time.March
				isOct2026OrLater := year > 2026 || (year == 2026 && month >= time.October)

				var onPeakHours []types.UtilityHourPeriod
				if isOct2026OrLater {
					// Effective October 1, 2026 (Fourth Revised Sheet No. 5.1):
					// Winter (Nov–Mar): 6 a.m. to 10 a.m. and 6 p.m. to 9 p.m.
					// Summer (Apr–Oct): 1 p.m. to 8 p.m.
					if isWinter {
						onPeakHours = []types.UtilityHourPeriod{
							{HourStart: 6, HourEnd: 10},
							{HourStart: 18, HourEnd: 21},
						}
					} else {
						onPeakHours = []types.UtilityHourPeriod{
							{HourStart: 13, HourEnd: 20},
						}
					}
				} else {
					if isWinter {
						onPeakHours = []types.UtilityHourPeriod{
							{HourStart: 6, HourEnd: 10},
							{HourStart: 18, HourEnd: 22}, // 6 p.m. to 10 p.m.
						}
					} else {
						onPeakHours = []types.UtilityHourPeriod{
							{HourStart: 12, HourEnd: 21}, // 12 p.m. to 9 p.m.
						}
					}
				}

				simplified := []touSimplifiedPeriod{
					{
						Year:               year,
						Months:             []time.Month{month},
						SpecificDates:      holidays,
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakDollarsPerKWH,
						OtherDescription:   "JEA GST Off-Peak (Holiday)",
					},
					{
						Year:             year,
						Months:           []time.Month{month},
						SpecificDates:    holidays,
						SpecificDatesNot: true,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "On-Peak",
								Hours:         onPeakHours,
								Weekday:       true,
								DollarsPerKWH: onPeakDollarsPerKWH,
								Description:   "JEA GST On-Peak",
							},
						},
						OtherName:          "Off-Peak",
						OtherDollarsPerKWH: offPeakDollarsPerKWH,
						OtherDescription:   "JEA GST Off-Peak",
					},
				}

				if opts.NetMeteringScheme == "dg" {
					simplified = append(simplified, touSimplifiedPeriod{
						Year:                               year,
						Months:                             []time.Month{month},
						OnlySeparateGenerationCredit:       true,
						OtherGenerationCreditDollarsPerKWH: fuelCharge,
						OtherDescription:                   "JEA DG Generation Credit",
					})
				}

				periods = append(periods, buildPeriods(etLocation, simplified)...)
			}
		}
	}
	return periods
}

func jeaUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "net", Name: "Net Energy Metering"},
			{Value: "dg", Name: "Distributed Generation"},
		},
		Default: "net",
	}

	return types.UtilityProviderInfo{
		ID:   "jea",
		Name: "JEA (Jacksonville)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "jea_r",
				Name:    "Rate R (Residential Service)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return jeaPeriods("jea_r", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "jea_gst",
				Name:    "Rate GST (General Service Time-of-Day)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return jeaPeriods("jea_gst", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
