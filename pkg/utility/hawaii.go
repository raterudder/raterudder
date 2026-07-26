package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

var hawaiiFlatRates = map[string]float64{
	"oahu":    0.412922,
	"hawaii":  0.402245,
	"maui":    0.395134,
	"lanai":   0.459928,
	"molokai": 0.414798,
}

type hawaiiTOURates struct {
	daytime     float64
	eveningPeak float64
	overnight   float64
}

var hawaiiTOURatesMap = map[string]hawaiiTOURates{
	"oahu": {
		daytime:     0.211966,
		eveningPeak: 0.623298,
		overnight:   0.417632,
	},
	"hawaii": {
		daytime:     0.235288,
		eveningPeak: 0.693122,
		overnight:   0.464205,
	},
	"maui": {
		daytime:     0.222232,
		eveningPeak: 0.652272,
		overnight:   0.437252,
	},
	"lanai": {
		daytime:     0.245031,
		eveningPeak: 0.717960,
		overnight:   0.481496,
	},
	"molokai": {
		daytime:     0.230943,
		eveningPeak: 0.675698,
		overnight:   0.453321,
	},
}

var hawaiiSREExportRates = map[string]hawaiiTOURates{
	"oahu":    {daytime: 0.135, eveningPeak: 0.329, overnight: 0.189},
	"hawaii":  {daytime: 0.106, eveningPeak: 0.231, overnight: 0.148},
	"maui":    {daytime: 0.066, eveningPeak: 0.182, overnight: 0.131},
	"lanai":   {daytime: 0.267, eveningPeak: 0.408, overnight: 0.259},
	"molokai": {daytime: 0.179, eveningPeak: 0.272, overnight: 0.174},
}

var hawaiiCGSPlusRates = map[string]float64{
	"oahu":    0.1008,
	"hawaii":  0.1055,
	"maui":    0.1217,
	"lanai":   0.2080,
	"molokai": 0.1677,
}

var hawaiiCGSRates = map[string]float64{
	"oahu":    0.1507,
	"hawaii":  0.1514,
	"maui":    0.1716,
	"lanai":   0.2788,
	"molokai": 0.2407,
}

var hawaiiSmartExportRates = map[string]float64{
	"oahu":    0.1497,
	"hawaii":  0.1100,
	"maui":    0.1441,
	"lanai":   0.2079,
	"molokai": 0.1664,
}

// hawaiiPeriods generates the pricing periods for Hawaiian Electric companies.
func hawaiiPeriods(providerID string, plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// Resolve island location from provider and options
	var location string
	switch providerID {
	case "heco":
		location = "oahu"
	case "helco":
		location = "hawaii"
	case "meco":
		location = options.Location
		if location == "" {
			location = "maui"
		}
	default:
		location = "oahu"
	}

	netMeteringScheme := options.NetMeteringScheme
	if netMeteringScheme == "" {
		netMeteringScheme = "sre_export"
	}

	// Resolve the base plan ID to determine if it is R or ARD TOU R
	isTOU := false
	if plan == "heco_ard_tou_r" || plan == "helco_ard_tou_r" || plan == "meco_ard_tou_r" {
		isTOU = true
	}

	for _, year := range years {
		// 1. Consumption/Import Periods
		if !isTOU {
			rate := hawaiiFlatRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:               year,
					MonthStart:         time.January,
					MonthEnd:           time.December,
					OtherDollarsPerKWH: rate,
					OtherDescription:   "Hawaiian Electric Schedule R Consumption Charge",
				},
			})...)
		} else {
			rates := hawaiiTOURatesMap[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:       year,
					MonthStart: time.January,
					MonthEnd:   time.December,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "Daytime",
							Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
							DollarsPerKWH: rates.daytime,
							Description:   "Hawaiian Electric ARD TOU R Daytime",
						},
						{
							Name:          "Evening Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
							DollarsPerKWH: rates.eveningPeak,
							Description:   "Hawaiian Electric ARD TOU R Evening Peak",
						},
					},
					OtherName:          "Overnight",
					OtherDollarsPerKWH: rates.overnight,
					OtherDescription:   "Hawaiian Electric ARD TOU R Overnight",
				},
			})...)
		}

		// 2. Solar Export Credit Periods (Separate Generation Credits)
		switch netMeteringScheme {
		case "sre_export":
			sreRates := hawaiiSREExportRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                         year,
					MonthStart:                   time.January,
					MonthEnd:                     time.December,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
							GenerationCreditDollarsPerKWH: sreRates.daytime,
							Description:                   "Hawaiian Electric SRE Export Credit Daytime",
						},
						{
							Hours:                         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
							GenerationCreditDollarsPerKWH: sreRates.eveningPeak,
							Description:                   "Hawaiian Electric SRE Export Credit Evening Peak",
						},
					},
					OtherGenerationCreditDollarsPerKWH: sreRates.overnight,
					OtherDescription:                   "Hawaiian Electric SRE Export Credit Overnight",
				},
			})...)

		case "cgs_plus":
			flatCredit := hawaiiCGSPlusRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: flatCredit,
					OtherDescription:                   "Hawaiian Electric CGS Plus Export Credit",
				},
			})...)

		case "cgs":
			flatCredit := hawaiiCGSRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: flatCredit,
					OtherDescription:                   "Hawaiian Electric CGS Export Credit",
				},
			})...)

		case "smart_export":
			exportCredit := hawaiiSmartExportRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                         year,
					MonthStart:                   time.January,
					MonthEnd:                     time.December,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 16}},
							GenerationCreditDollarsPerKWH: 0.0,
							Description:                   "Hawaiian Electric Smart Export Non-Export Window Credit",
						},
					},
					OtherGenerationCreditDollarsPerKWH: exportCredit,
					OtherDescription:                   "Hawaiian Electric Smart Export Export Window Credit",
				},
			})...)

		case "net":
			// Net energy metering is handled automatically by 1:1 net metering logic.
		}
	}

	return periods
}

// hawaiiUtilityInfo returns the metadata and rates for HECO, HELCO, and MECO.
func hawaiiUtilityInfo() []types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "sre_export", Name: "Smart Renewable Energy Export"},
			{Value: "cgs_plus", Name: "Customer Grid-Supply Plus"},
			{Value: "cgs", Name: "Customer Grid-Supply"},
			{Value: "smart_export", Name: "Smart Export"},
			{Value: "net", Name: "Net Energy Metering (NEM / NEM+)"},
		},
		Default: "sre_export",
	}

	mecoOptions := []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "Location / Island Division",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select the specific island division for Maui County.",
			Choices: []types.UtilityOptionChoice{
				{Value: "maui", Name: "Maui"},
				{Value: "lanai", Name: "Lāna‘i"},
				{Value: "molokai", Name: "Moloka‘i"},
			},
			Default: "maui",
		},
		exportOption,
	}

	hecoOptions := []types.UtilityRateOption{exportOption}

	return []types.UtilityProviderInfo{
		{
			ID:   "heco",
			Name: "Hawaiian Electric (HECO) (O‘ahu)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "heco_r",
					Name:    "Schedule R (Residential Standard)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("heco", "heco_r", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "heco_ard_tou_r",
					Name:    "Schedule ARD TOU R (Residential Time of Use)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("heco", "heco_ard_tou_r", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "helco",
			Name: "Hawai‘i Electric Light (HELCO)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "helco_r",
					Name:    "Schedule R (Residential Standard)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("helco", "helco_r", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "helco_ard_tou_r",
					Name:    "Schedule ARD TOU R (Residential Time of Use)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("helco", "helco_ard_tou_r", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "meco",
			Name: "Maui Electric (MECO)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "meco_r",
					Name:    "Schedule R (Residential Standard)",
					Options: mecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("meco", "meco_r", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "meco_ard_tou_r",
					Name:    "Schedule ARD TOU R (Residential Time of Use)",
					Options: mecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("meco", "meco_ard_tou_r", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
	}
}
