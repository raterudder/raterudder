package utility

import (
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// TEP residential tariff rates from Tucson Electric Power tariff sheets
// (TRRES, TRREST, TRRESD, TRRESDT) approved under ACC Decision No. 79065
// (effective September 1, 2023) and Rider-14 Resource Comparison Proxy (RCP)
// export rates from TEP's Statement of Charges on tep.com.
const (
	// Delivery Energy Charges ($/kWh).
	// NOTE: TRRES and TRREST delivery charges are tiered by monthly consumption
	// (<= 500, 501-1,000, > 1,000 kWh); we assume Tier 1 usage.
	tepBasicDeliveryDollarsPerKWH  = 0.086720 // Tier 1 (0 - 500 kWh)
	tepTOUDeliveryDollarsPerKWH    = 0.093556 // Tier 1 (0 - 500 kWh)
	tepDemandDeliveryDollarsPerKWH = 0.044556 // Flat across all kWh for TRRESD and TRRESDT

	// Base Power Supply Charges ($/kWh) - Non-TOU (TRRES and TRRESD)
	tepSummerBaseSupplyDollarsPerKWH = 0.043711 // May - September
	tepWinterBaseSupplyDollarsPerKWH = 0.039632 // October - April

	// Base Power Supply Charges ($/kWh) - TOU (TRREST and TRRESDT)
	tepSummerOnPeakSupplyDollarsPerKWH  = 0.099166 // May - September On-Peak
	tepSummerOffPeakSupplyDollarsPerKWH = 0.028296 // May - September Off-Peak
	tepWinterOnPeakSupplyDollarsPerKWH  = 0.039694 // October - April On-Peak
	tepWinterOffPeakSupplyDollarsPerKWH = 0.031266 // October - April Off-Peak
)

func shiftTEPWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

// getTEPHolidays returns Tucson Electric Power's observed off-peak holidays.
// Per TEP tariffs (TRREST, TRRESD, TRRESDT), recognized holidays are Memorial Day,
// Independence Day, and Labor Day in summer, and Thanksgiving Day, Christmas Day,
// and New Year's Day in winter. If a holiday falls on Saturday, the preceding Friday
// is designated Off-Peak; if a holiday falls on Sunday, the following Monday is Off-Peak.
func getTEPHolidays(year int) []string {
	holidays := []time.Time{
		shiftTEPWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftTEPWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftTEPWeekendHoliday(christmasDay(year)),
		shiftTEPWeekendHoliday(newYearsDay(year + 1)),
	}

	return formatHolidays(holidays, year)
}

// tepRCPExportDollarsPerKWH returns the Rider-14 Resource Comparison Proxy (RCP)
// export rate ($/kWh) locked for 10 years based on interconnection cohort, or 0
// along with false if flat 1:1 Rider-4 net metering is selected.
func tepRCPExportDollarsPerKWH(options types.UtilityRateOptions) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(options.NetMeteringScheme)) {
	case "net", "nem", "nem1":
		return 0, false
	case "rcp_2025":
		return 0.0513, true
	case "rcp_2024":
		return 0.0570, true
	case "rcp_2023":
		return 0.0633, true
	case "rcp_2022":
		return 0.0703, true
	case "rcp_2021":
		return 0.0781, true
	case "rcp_2019", "rcp_2020":
		return 0.0868, true
	case "rcp_2018":
		return 0.0964, true
	default:
		// Current Rider-14 RCP rate effective October 1, 2026 - September 30, 2027
		return 0.0462, true
	}
}

func buildTEPTOUPeriods(
	year int,
	holidays []string,
	summerOnPeakDollarsPerKWH float64,
	summerOffPeakDollarsPerKWH float64,
	winterOnPeakDollarsPerKWH float64,
	winterOffPeakDollarsPerKWH float64,
	planLabel string,
) []touSimplifiedPeriod {
	return []touSimplifiedPeriod{
		// Summer Holidays (May - September): Off-Peak all day
		{
			Year:               year,
			MonthStart:         time.May,
			MonthEnd:           time.September,
			SpecificDates:      holidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: summerOffPeakDollarsPerKWH,
			OtherDescription:   planLabel + " Summer Holiday Off-Peak",
		},
		// Summer Regular (May - September): On-Peak 3:00 PM - 7:00 PM Mon-Fri
		{
			Year:             year,
			MonthStart:       time.May,
			MonthEnd:         time.September,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					Name:          "On-Peak",
					Hours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 19}},
					Weekday:       true,
					DollarsPerKWH: summerOnPeakDollarsPerKWH,
					Description:   planLabel + " Summer On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: summerOffPeakDollarsPerKWH,
			OtherDescription:   planLabel + " Summer Off-Peak",
		},
		// Winter Holidays (October - April): Off-Peak all day
		{
			Year:               year,
			MonthStart:         time.October,
			MonthEnd:           time.April,
			SpecificDates:      holidays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
			OtherDescription:   planLabel + " Winter Holiday Off-Peak",
		},
		// Winter Regular (October - April): On-Peak 6:00 AM - 9:00 AM and 6:00 PM - 9:00 PM Mon-Fri
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
						{HourStart: 18, HourEnd: 21},
					},
					Weekday:       true,
					DollarsPerKWH: winterOnPeakDollarsPerKWH,
					Description:   planLabel + " Winter On-Peak",
				},
			},
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: winterOffPeakDollarsPerKWH,
			OtherDescription:   planLabel + " Winter Off-Peak",
		},
	}
}

func tepPeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	exportCreditDollarsPerKWH, isNetBilling := tepRCPExportDollarsPerKWH(options)

	for _, year := range years {
		holidays := getTEPHolidays(year)

		switch plan {
		case "tep_trres":
			// Basic Residential Service (Rate TRRES)
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.May,
					MonthEnd:           time.September,
					OtherDollarsPerKWH: tepBasicDeliveryDollarsPerKWH + tepSummerBaseSupplyDollarsPerKWH,
					OtherDescription:   "TEP Basic (TRRES) Summer Energy Charge",
				},
				{
					Year:               year,
					MonthStart:         time.October,
					MonthEnd:           time.April,
					OtherDollarsPerKWH: tepBasicDeliveryDollarsPerKWH + tepWinterBaseSupplyDollarsPerKWH,
					OtherDescription:   "TEP Basic (TRRES) Winter Energy Charge",
				},
			})...)

		case "tep_trrest":
			// Residential Time-of-Use (Rate TRREST)
			periods = append(periods, buildPeriods(mstLocation, buildTEPTOUPeriods(
				year,
				holidays,
				tepTOUDeliveryDollarsPerKWH+tepSummerOnPeakSupplyDollarsPerKWH,
				tepTOUDeliveryDollarsPerKWH+tepSummerOffPeakSupplyDollarsPerKWH,
				tepTOUDeliveryDollarsPerKWH+tepWinterOnPeakSupplyDollarsPerKWH,
				tepTOUDeliveryDollarsPerKWH+tepWinterOffPeakSupplyDollarsPerKWH,
				"TEP TOU (TRREST)",
			))...)

		case "tep_trresd":
			// Residential Peak Demand (Rate TRRESD)
			// Energy charge is uniform within each season, while On-Peak windows define when monthly kW demand is measured.
			summerDollarsPerKWH := tepDemandDeliveryDollarsPerKWH + tepSummerBaseSupplyDollarsPerKWH
			winterDollarsPerKWH := tepDemandDeliveryDollarsPerKWH + tepWinterBaseSupplyDollarsPerKWH

			periods = append(periods, buildPeriods(mstLocation, buildTEPTOUPeriods(
				year,
				holidays,
				summerDollarsPerKWH,
				summerDollarsPerKWH,
				winterDollarsPerKWH,
				winterDollarsPerKWH,
				"TEP Peak Demand (TRRESD)",
			))...)

		case "tep_trresdt":
			// Residential Demand Time-of-Use (Rate TRRESDT)
			periods = append(periods, buildPeriods(mstLocation, buildTEPTOUPeriods(
				year,
				holidays,
				tepDemandDeliveryDollarsPerKWH+tepSummerOnPeakSupplyDollarsPerKWH,
				tepDemandDeliveryDollarsPerKWH+tepSummerOffPeakSupplyDollarsPerKWH,
				tepDemandDeliveryDollarsPerKWH+tepWinterOnPeakSupplyDollarsPerKWH,
				tepDemandDeliveryDollarsPerKWH+tepWinterOffPeakSupplyDollarsPerKWH,
				"TEP Demand TOU (TRRESDT)",
			))...)
		}

		if isNetBilling {
			periods = append(periods, buildPeriods(mstLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: exportCreditDollarsPerKWH,
					OtherDescription:                   "TEP Rider-14 RCP Net Billing Export Credit",
				},
			})...)
		}
	}

	return periods
}

func tepUtilityInfo() types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your Rider-14 Resource Comparison Proxy (RCP) 10-year locked export cohort or grandfathered Rider-4 Net Metering.",
		Choices: []types.UtilityOptionChoice{
			{Value: "rcp", Name: "Rider-14 RCP Current ($0.0462/kWh, Oct 2026 - Sep 2027)"},
			{Value: "rcp_2025", Name: "Rider-14 RCP Oct 2025 - Sep 2026 ($0.0513/kWh)"},
			{Value: "rcp_2024", Name: "Rider-14 RCP Oct 2024 - Sep 2025 ($0.0570/kWh)"},
			{Value: "rcp_2023", Name: "Rider-14 RCP Oct 2023 - Sep 2024 ($0.0633/kWh)"},
			{Value: "rcp_2022", Name: "Rider-14 RCP Oct 2022 - Sep 2023 ($0.0703/kWh)"},
			{Value: "rcp_2021", Name: "Rider-14 RCP Oct 2021 - Sep 2022 ($0.0781/kWh)"},
			{Value: "rcp_2019", Name: "Rider-14 RCP Oct 2019 - Sep 2021 ($0.0868/kWh)"},
			{Value: "rcp_2018", Name: "Rider-14 RCP Sep 2018 - Sep 2019 ($0.0964/kWh)"},
			{Value: "net", Name: "Rider-4 Net Metering (Grandfathered 1:1 Retail Credits)"},
		},
		Default: "rcp",
	}

	return types.UtilityProviderInfo{
		ID:   "tep",
		Name: "Tucson Electric Power (TEP)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "tep_trresdt",
				Name:    "Demand Time-of-Use (TRRESDT)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return tepPeriods("tep_trresdt", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "tep_trrest",
				Name:    "Time-of-Use (TRREST)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return tepPeriods("tep_trrest", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "tep_trresd",
				Name:    "Peak Demand (TRRESD)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return tepPeriods("tep_trresd", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "tep_trres",
				Name:    "Basic (TRRES)",
				Options: []types.UtilityRateOption{exportOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return tepPeriods("tep_trres", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
