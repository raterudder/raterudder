package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Octopus Energy REST API Reference:
// Octopus publishes public REST endpoints for product tariffs and unit rates:
// Base URL: https://api.octopus.energy/v1/products/
//
// Public Product Codes:
//   - Octopus Flux Import: FLUX-IMPORT-23-02-14
//   - Octopus Flux Export: FLUX-EXPORT-23-02-14
//   - Intelligent Octopus Go: INTELLI-VAR-22-10-14
//   - Outgoing Octopus Export: OUTGOING-VAR-24-10-26
//
// Tariff Code Format: E-1R-{PRODUCT_CODE}-{REGION_LETTER} (e.g. E-1R-FLUX-IMPORT-23-02-14-C for London)
// Example Tariff Unit Rates URL:
//   https://api.octopus.energy/v1/products/FLUX-IMPORT-23-02-14/electricity-tariffs/E-1R-FLUX-IMPORT-23-02-14-C/standard-unit-rates/
//   https://api.octopus.energy/v1/products/INTELLI-VAR-22-10-14/electricity-tariffs/E-1R-INTELLI-VAR-22-10-14-C/standard-unit-rates/

type octopusRateConfig struct {
	ImportPeakRate     float64
	ImportOffPeakRate  float64
	ImportStandardRate float64

	ExportPeakRate     float64
	ExportOffPeakRate  float64
	ExportStandardRate float64

	IOGPeakRate    float64
	IOGOffPeakRate float64
}

// octopusRates stores current unit rates in base currency (£/kWh, e.g. 0.2450 = 24.5p/kWh)
// indexed by the 14 UK distribution network (DNO / PES) regions A through P.
var octopusRates = map[string]octopusRateConfig{
	"eastern": { // PES A / DNO 10
		ImportStandardRate: 0.2429,
		ImportPeakRate:     0.3401,
		ImportOffPeakRate:  0.1458,
		ExportStandardRate: 0.1011,
		ExportPeakRate:     0.2932,
		ExportOffPeakRate:  0.0499,
		IOGPeakRate:        0.2896,
		IOGOffPeakRate:     0.0700,
	},
	"east_midlands": { // PES B / DNO 11
		ImportStandardRate: 0.2306,
		ImportPeakRate:     0.3229,
		ImportOffPeakRate:  0.1384,
		ExportStandardRate: 0.0976,
		ExportPeakRate:     0.2770,
		ExportOffPeakRate:  0.0434,
		IOGPeakRate:        0.2777,
		IOGOffPeakRate:     0.0700,
	},
	"london": { // PES C / DNO 12
		ImportStandardRate: 0.2425,
		ImportPeakRate:     0.3396,
		ImportOffPeakRate:  0.1456,
		ExportStandardRate: 0.0980,
		ExportPeakRate:     0.2860,
		ExportOffPeakRate:  0.0524,
		IOGPeakRate:        0.2802,
		IOGOffPeakRate:     0.0700,
	},
	"merseyside_wales": { // PES D / DNO 13
		ImportStandardRate: 0.2552,
		ImportPeakRate:     0.3573,
		ImportOffPeakRate:  0.1532,
		ExportStandardRate: 0.1054,
		ExportPeakRate:     0.3068,
		ExportOffPeakRate:  0.0512,
		IOGPeakRate:        0.3017,
		IOGOffPeakRate:     0.0700,
	},
	"west_midlands": { // PES E / DNO 14
		ImportStandardRate: 0.2328,
		ImportPeakRate:     0.3259,
		ImportOffPeakRate:  0.1397,
		ExportStandardRate: 0.0979,
		ExportPeakRate:     0.2781,
		ExportOffPeakRate:  0.0439,
		IOGPeakRate:        0.2785,
		IOGOffPeakRate:     0.0700,
	},
	"north_east": { // PES F / DNO 15
		ImportStandardRate: 0.2319,
		ImportPeakRate:     0.3248,
		ImportOffPeakRate:  0.1392,
		ExportStandardRate: 0.0971,
		ExportPeakRate:     0.2769,
		ExportOffPeakRate:  0.0421,
		IOGPeakRate:        0.2769,
		IOGOffPeakRate:     0.0700,
	},
	"north_west": { // PES G / DNO 16
		ImportStandardRate: 0.2407,
		ImportPeakRate:     0.3370,
		ImportOffPeakRate:  0.1445,
		ExportStandardRate: 0.1033,
		ExportPeakRate:     0.2925,
		ExportOffPeakRate:  0.0466,
		IOGPeakRate:        0.2957,
		IOGOffPeakRate:     0.0700,
	},
	"southern": { // PES H / DNO 20
		ImportStandardRate: 0.2434,
		ImportPeakRate:     0.3408,
		ImportOffPeakRate:  0.1461,
		ExportStandardRate: 0.1008,
		ExportPeakRate:     0.2900,
		ExportOffPeakRate:  0.0474,
		IOGPeakRate:        0.2879,
		IOGOffPeakRate:     0.0700,
	},
	"south_east": { // PES J / DNO 19
		ImportStandardRate: 0.2458,
		ImportPeakRate:     0.3441,
		ImportOffPeakRate:  0.1475,
		ExportStandardRate: 0.1024,
		ExportPeakRate:     0.2979,
		ExportOffPeakRate:  0.0505,
		IOGPeakRate:        0.2937,
		IOGOffPeakRate:     0.0700,
	},
	"south_wales": { // PES K / DNO 21
		ImportStandardRate: 0.2426,
		ImportPeakRate:     0.3397,
		ImportOffPeakRate:  0.1456,
		ExportStandardRate: 0.1021,
		ExportPeakRate:     0.2935,
		ExportOffPeakRate:  0.0469,
		IOGPeakRate:        0.2919,
		IOGOffPeakRate:     0.0700,
	},
	"south_west": { // PES L / DNO 22
		ImportStandardRate: 0.2432,
		ImportPeakRate:     0.3405,
		ImportOffPeakRate:  0.1460,
		ExportStandardRate: 0.1020,
		ExportPeakRate:     0.2922,
		ExportOffPeakRate:  0.0455,
		IOGPeakRate:        0.2917,
		IOGOffPeakRate:     0.0700,
	},
	"yorkshire": { // PES M / DNO 23
		ImportStandardRate: 0.2323,
		ImportPeakRate:     0.3253,
		ImportOffPeakRate:  0.1395,
		ExportStandardRate: 0.0969,
		ExportPeakRate:     0.2717,
		ExportOffPeakRate:  0.0419,
		IOGPeakRate:        0.2748,
		IOGOffPeakRate:     0.0700,
	},
	"southern_scotland": { // PES N / DNO 18
		ImportStandardRate: 0.2377,
		ImportPeakRate:     0.3328,
		ImportOffPeakRate:  0.1427,
		ExportStandardRate: 0.0955,
		ExportPeakRate:     0.2719,
		ExportOffPeakRate:  0.0442,
		IOGPeakRate:        0.2722,
		IOGOffPeakRate:     0.0700,
	},
	"northern_scotland": { // PES P / DNO 17
		ImportStandardRate: 0.2437,
		ImportPeakRate:     0.3412,
		ImportOffPeakRate:  0.1463,
		ExportStandardRate: 0.0998,
		ExportPeakRate:     0.2873,
		ExportOffPeakRate:  0.0473,
		IOGPeakRate:        0.2868,
		IOGOffPeakRate:     0.0700,
	},
}

var octopusLocationChoices = []types.UtilityOptionChoice{
	{Value: "london", Name: "London (PES C / DNO 12)"},
	{Value: "eastern", Name: "Eastern England (PES A / DNO 10)"},
	{Value: "east_midlands", Name: "East Midlands (PES B / DNO 11)"},
	{Value: "merseyside_wales", Name: "Merseyside & North Wales (PES D / DNO 13)"},
	{Value: "west_midlands", Name: "West Midlands (PES E / DNO 14)"},
	{Value: "north_east", Name: "North East England (PES F / DNO 15)"},
	{Value: "north_west", Name: "North West England (PES G / DNO 16)"},
	{Value: "southern", Name: "Southern England (PES H / DNO 20)"},
	{Value: "south_east", Name: "South East England (PES J / DNO 19)"},
	{Value: "south_wales", Name: "South Wales (PES K / DNO 21)"},
	{Value: "south_west", Name: "South West England (PES L / DNO 22)"},
	{Value: "yorkshire", Name: "Yorkshire (PES M / DNO 23)"},
	{Value: "southern_scotland", Name: "Southern Scotland (PES N / DNO 18)"},
	{Value: "northern_scotland", Name: "Northern Scotland (PES P / DNO 17)"},
}

func octopusFluxFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	loc := options.Location
	if loc == "" {
		loc = "london"
	}
	cfg, ok := octopusRates[loc]
	if !ok {
		return nil, fmt.Errorf("unknown octopus location: %s", loc)
	}

	var periods []types.UtilityFeesPeriod
	years := []int{2026, 2027}

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, lonLocation)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, lonLocation)

		// 1. Off-Peak: 02:00 – 05:00
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 2, HourEnd: 5}},
			},
			DollarsPerKWH: cfg.ImportOffPeakRate,
			Description:   "Octopus Flux Off-Peak Rate",
		})
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak Export",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 2, HourEnd: 5}},
			},
			DollarsPerKWH:            cfg.ExportOffPeakRate,
			SeparateGenerationCredit: true,
			Description:              "Octopus Flux Off-Peak Export Tariff",
		})

		// 2. Peak: 16:00 – 19:00
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
			},
			DollarsPerKWH: cfg.ImportPeakRate,
			Description:   "Octopus Flux Peak Rate",
		})
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Peak Export",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
			},
			DollarsPerKWH:            cfg.ExportPeakRate,
			SeparateGenerationCredit: true,
			Description:              "Octopus Flux Peak Export Tariff",
		})

		// 3. Standard / Day: 05:00 – 16:00 and 19:00 – 02:00
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Standard",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 5, HourEnd: 16},
					{HourStart: 19, HourEnd: 24},
					{HourStart: 0, HourEnd: 2},
				},
			},
			DollarsPerKWH: cfg.ImportStandardRate,
			Description:   "Octopus Flux Standard Rate",
		})
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Standard Export",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 5, HourEnd: 16},
					{HourStart: 19, HourEnd: 24},
					{HourStart: 0, HourEnd: 2},
				},
			},
			DollarsPerKWH:            cfg.ExportStandardRate,
			SeparateGenerationCredit: true,
			Description:              "Octopus Flux Standard Export Tariff",
		})
	}

	return periods, nil
}

func octopusIntelligentGoFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	loc := options.Location
	if loc == "" {
		loc = "london"
	}
	cfg, ok := octopusRates[loc]
	if !ok {
		return nil, fmt.Errorf("unknown octopus location: %s", loc)
	}

	var periods []types.UtilityFeesPeriod
	years := []int{2026, 2027}

	// Export Rate
	exportRate := 0.1500 // Default: Outgoing Octopus Fixed 15p/kWh
	if options.GenerationRate == "outgoing_12p" {
		exportRate = 0.1200
	} else if options.GenerationRate == "none" {
		exportRate = 0.0
	}

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, lonLocation)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, lonLocation)

		// 1. Overnight Off-Peak: 23:30 – 05:30 (6 hours)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 23, MinuteStart: 30, HourEnd: 24},
					{HourStart: 0, HourEnd: 5, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: cfg.IOGOffPeakRate,
			Description:   "Intelligent Octopus Go Off-Peak Rate",
		})

		// 2. Day Peak: 05:30 – 23:30 (18 hours)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 5, MinuteStart: 30, HourEnd: 23, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: cfg.IOGPeakRate,
			Description:   "Intelligent Octopus Go Day Rate",
		})

		// 3. Export
		if exportRate > 0 {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: lonLocation,
				},
				DollarsPerKWH:            exportRate,
				SeparateGenerationCredit: true,
				Description:              "Outgoing Octopus Export Tariff",
			})
		}
	}

	return periods, nil
}

// octopusUtilityInfo returns metadata for Octopus Energy (UK).
func octopusUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:          "octopus",
		Name:        "Octopus Energy (UK)",
		RatesNotice: "Intelligent Octopus Flux is not supported at this time.",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "octopus_flux",
				Name: "Octopus Flux",
				Options: []types.UtilityRateOption{
					{
						Field:       "location",
						Name:        "Distribution Network (DNO / PES Region)",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your regional electricity distribution network area.",
						Choices:     octopusLocationChoices,
						Default:     "london",
					},
				},
				GetFees: octopusFluxFees,
			},
			{
				ID:   "octopus_intelligent_go",
				Name: "Intelligent Octopus Go",
				Options: []types.UtilityRateOption{
					{
						Field:       "location",
						Name:        "Distribution Network (DNO / PES Region)",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your regional electricity distribution network area.",
						Choices:     octopusLocationChoices,
						Default:     "london",
					},
					{
						Field:       "generationRate",
						Name:        "Export Tariff",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your export credit agreement (e.g. Outgoing Octopus).",
						Choices: []types.UtilityOptionChoice{
							{Value: "outgoing_15p", Name: "Outgoing Octopus Fixed (15p/kWh)"},
							{Value: "outgoing_12p", Name: "Outgoing Octopus Fixed (12p/kWh)"},
							{Value: "none", Name: "No Export Credit"},
						},
						Default: "outgoing_15p",
					},
				},
				GetFees: octopusIntelligentGoFees,
			},
		},
	}
}
