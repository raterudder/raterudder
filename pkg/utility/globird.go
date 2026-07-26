package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type globirdRateConfig struct {
	Timezone           *time.Location
	ImportPeakRate     float64
	ImportOffPeakRate  float64
	ImportShoulderRate float64
	ExportPeakRate     float64
	ExportOffPeakRate  float64
	ExportFlatRate     float64
}

// globirdRates maps plan ID -> location -> configuration.
// United Energy and Jemena are not explicitly in the GloBird fact sheets,
// so they fallback to Citipower rates as Melbourne distributor approximations.
var globirdRates = map[string]map[string]globirdRateConfig{
	"globird_four4free": {
		"ausgrid": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.5995,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.3751,
			ExportPeakRate:     0.0800,
			ExportOffPeakRate:  0.0000,
		},
		"endeavour": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.5962,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.3861,
			ExportPeakRate:     0.0800,
			ExportOffPeakRate:  0.0000,
		},
		"energex": {
			Timezone:           bneLocation,
			ImportPeakRate:     0.5555,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.3410,
			ExportPeakRate:     0.0800,
			ExportOffPeakRate:  0.0000,
		},
		"citipower": {
			Timezone:           melLocation,
			ImportPeakRate:     0.4400,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.2640,
			ExportPeakRate:     0.0300,
			ExportOffPeakRate:  0.0000,
		},
		"powercor": {
			Timezone:           melLocation,
			ImportPeakRate:     0.4950,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.3080,
			ExportPeakRate:     0.0300,
			ExportOffPeakRate:  0.0000,
		},
		"ausnet": {
			Timezone:           melLocation,
			ImportPeakRate:     0.5335,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.2915,
			ExportPeakRate:     0.0300,
			ExportOffPeakRate:  0.0000,
		},
		"united_energy": {
			Timezone:           melLocation,
			ImportPeakRate:     0.4400,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.2640,
			ExportPeakRate:     0.0300,
			ExportOffPeakRate:  0.0000,
		},
		"jemena": {
			Timezone:           melLocation,
			ImportPeakRate:     0.4400,
			ImportOffPeakRate:  0.0000,
			ImportShoulderRate: 0.2640,
			ExportPeakRate:     0.0300,
			ExportOffPeakRate:  0.0000,
		},
	},
	"globird_solarplus": {
		"ausgrid": {
			Timezone:       sydLocation,
			ImportPeakRate: 0.4939,
			ExportFlatRate: 0.0200,
		},
		"endeavour": {
			Timezone:          sydLocation,
			ImportPeakRate:    0.6083,
			ImportOffPeakRate: 0.4246,
			ExportFlatRate:    0.0200,
		},
		"energex": {
			Timezone:          bneLocation,
			ImportPeakRate:    0.5610,
			ImportOffPeakRate: 0.3674,
			ExportFlatRate:    0.0200,
		},
	},
}

func globirdFour4FreePeriods(cfg globirdRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		// Peak (4 PM to 11 PM) everyday
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "On-Peak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 23}},
			},
			DollarsPerKWH: cfg.ImportPeakRate,
			Description:   "GloBird FOUR4FREE Everyday Peak Rate",
		})

		// Off-Peak (10 AM to 2 PM) everyday
		// ====================================================================================
		// NOTE: GloBird FOUR4FREE offers 4 hours of free electricity everyday between 10am-2pm,
		// up to a limit of 50kWh/day. We assume the household optimizes for low grid usage and
		// does not exceed this limit, hence we statically set the rate to $0.00/kWh.
		// ====================================================================================
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Super Off-Peak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 14}},
			},
			DollarsPerKWH: cfg.ImportOffPeakRate,
			Description:   "GloBird FOUR4FREE Everyday Free Off-Peak Rate",
		})

		// Shoulder (all other times) everyday
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Shoulder",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 14, HourEnd: 16}, {HourStart: 23, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportShoulderRate,
			Description:   "GloBird FOUR4FREE Everyday Shoulder Rate",
		})

		// Export Peak (4 PM to 11 PM) everyday
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 23}},
			},
			DollarsPerKWH:            cfg.ExportPeakRate,
			SeparateGenerationCredit: true,
			Description:              "GloBird FOUR4FREE Everyday Solar Export Peak Tariff",
		})

		// Export Off-Peak (all other times) everyday
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 23, HourEnd: 24}},
			},
			DollarsPerKWH:            cfg.ExportOffPeakRate,
			SeparateGenerationCredit: true,
			Description:              "GloBird FOUR4FREE Everyday Solar Export Off-Peak Tariff",
		})
	}

	return periods
}

func globirdSolarPlusPeriods(loc string, cfg globirdRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		if loc == "ausgrid" {
			// ====================================================================================
			// NOTE: GloBird SOLARPLUS on Ausgrid uses a tiered import rate. We assume the home
			// is optimizing for low grid usage and does not exceed the first 15kWh/day tier,
			// hence we use the first tier rate of $0.4939/kWh as a flat rate.
			// ====================================================================================
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH: cfg.ImportPeakRate,
				Description:   "GloBird SOLARPLUS Flat Rate",
			})
		} else if loc == "endeavour" {
			// Weekday Peak (1 PM to 8 PM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "On-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 20}},
				},
				DollarsPerKWH: cfg.ImportPeakRate,
				Description:   "GloBird SOLARPLUS Weekday Peak Rate",
			})

			// Weekday Off-Peak (other times)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 13}, {HourStart: 20, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   "GloBird SOLARPLUS Weekday Off-Peak Rate",
			})

			// Weekend Off-Peak (All Day)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   "GloBird SOLARPLUS Weekend Off-Peak Rate",
			})
		} else if loc == "energex" {
			// Peak Everyday (4 PM to 9 PM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
				},
				DollarsPerKWH: cfg.ImportPeakRate,
				Description:   "GloBird SOLARPLUS Everyday Peak Rate",
			})

			// Off-Peak Everyday (all other times)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   "GloBird SOLARPLUS Everyday Off-Peak Rate",
			})
		}

		// Solar flat export credit
		// ====================================================================================
		// NOTE: GloBird SOLARPLUS offers a premium 10c/kWh feed-in tariff for the first 8kWh/day.
		// As per our global feed-in credit logic, we do not support daily tiered limits
		// and statically apply the flat lower feed-in rate of $0.02/kWh.
		// ====================================================================================
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
			},
			DollarsPerKWH:            cfg.ExportFlatRate,
			SeparateGenerationCredit: true,
			Description:              "GloBird SOLARPLUS Solar Export Tariff",
		})
	}

	return periods
}

func globirdPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	loc := opts.Location
	if loc == "" {
		loc = "energex"
	}

	distMap, ok := globirdRates[plan]
	if !ok {
		return nil, fmt.Errorf("unsupported plan: %s", plan)
	}

	cfg, ok := distMap[loc]
	if !ok {
		return nil, fmt.Errorf("location %s is not supported for plan %s", loc, plan)
	}

	switch plan {
	case "globird_four4free":
		return globirdFour4FreePeriods(cfg, years), nil
	case "globird_solarplus":
		return globirdSolarPlusPeriods(loc, cfg, years), nil
	default:
		return nil, fmt.Errorf("unsupported plan: %s", plan)
	}
}

// globirdUtilityInfo returns metadata for GloBird Energy.
func globirdUtilityInfo() types.UtilityProviderInfo {
	four4FreeOptions := []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "Location",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your distribution network / location.",
			Choices: []types.UtilityOptionChoice{
				{Value: "energex", Name: "Brisbane, Gold Coast & Sunshine Coast"},
				{Value: "citipower", Name: "Melbourne CBD & Inner Suburbs"},
				{Value: "united_energy", Name: "Melbourne Southern Suburbs & Mornington"},
				{Value: "powercor", Name: "Melbourne West, Geelong & Western VIC"},
				{Value: "ausgrid", Name: "Sydney Metro & Central Coast"},
				{Value: "endeavour", Name: "Western Sydney & South Coast"},
				{Value: "ausnet", Name: "Victoria East & North (AusNet)"},
				{Value: "jemena", Name: "Melbourne Northern & Western Suburbs"},
			},
			Default: "energex",
		},
	}

	solarPlusOptions := []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "Location",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your distribution network / location.",
			Choices: []types.UtilityOptionChoice{
				{Value: "energex", Name: "Brisbane, Gold Coast & Sunshine Coast"},
				{Value: "ausgrid", Name: "Sydney Metro & Central Coast"},
				{Value: "endeavour", Name: "Western Sydney & South Coast"},
			},
			Default: "energex",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "globird",
		Name: "GloBird Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "globird_four4free",
				Name:    "FOUR4FREE",
				Options: four4FreeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return globirdPeriods("globird_four4free", opts, []int{2026})
				},
			},
			{
				ID:      "globird_solarplus",
				Name:    "SOLARPLUS",
				Options: solarPlusOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return globirdPeriods("globird_solarplus", opts, []int{2026})
				},
			},
		},
	}
}
