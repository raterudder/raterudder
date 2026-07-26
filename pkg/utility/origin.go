package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type originRateConfig struct {
	Timezone           *time.Location
	ImportPeakRate     float64
	ImportOffPeakRate  float64
	ImportShoulderRate float64
	ExportPeakRate     float64
	ExportOffPeakRate  float64
	ExportFlatRate     float64

	PeakHours     []types.UtilityHourPeriod
	OffPeakHours  []types.UtilityHourPeriod
	ShoulderHours []types.UtilityHourPeriod

	WeekdayPeakHours     []types.UtilityHourPeriod
	WeekdayShoulderHours []types.UtilityHourPeriod
	WeekendShoulderHours []types.UtilityHourPeriod
	AllWeekOffPeakHours  []types.UtilityHourPeriod
}

var originRates = map[string]map[string]originRateConfig{
	"energex": {
		"origin_battery_maximiser": {
			Timezone:          bneLocation,
			ImportPeakRate:    0.3410,
			ImportOffPeakRate: 0.1870,
			ExportPeakRate:    0.2200,
			ExportOffPeakRate: 0.0700,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:          bneLocation,
			ImportPeakRate:    0.4290,
			ImportOffPeakRate: 0.3152,
			ExportPeakRate:    0.1800,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:             bneLocation,
			ImportPeakRate:       0.4580,
			ImportShoulderRate:   0.3302,
			ImportOffPeakRate:    0.2780,
			ExportFlatRate:       0.0300,
			WeekdayPeakHours:     []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}},
			WeekdayShoulderHours: []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 16}, {HourStart: 20, HourEnd: 22}},
			WeekendShoulderHours: []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}},
			AllWeekOffPeakHours:  []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:             bneLocation,
			ImportPeakRate:       0.4486,
			ImportShoulderRate:   0.3234,
			ImportOffPeakRate:    0.2723,
			ExportFlatRate:       0.0300,
			WeekdayPeakHours:     []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}},
			WeekdayShoulderHours: []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 16}, {HourStart: 20, HourEnd: 22}},
			WeekendShoulderHours: []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}},
			AllWeekOffPeakHours:  []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:           bneLocation,
			ImportPeakRate:     0.4998,
			ImportOffPeakRate:  0.2826,
			ImportShoulderRate: 0.3240,
			ExportFlatRate:     0.0300,
			PeakHours:          []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:       []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			ShoulderHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"citipower": {
		"origin_battery_maximiser": {
			Timezone:          melLocation,
			ImportPeakRate:    0.2640,
			ImportOffPeakRate: 0.1870,
			ExportPeakRate:    0.2200,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:          melLocation,
			ImportPeakRate:    0.4015,
			ImportOffPeakRate: 0.2322,
			ExportPeakRate:    0.1800,
			ExportOffPeakRate: 0.0300,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3561,
			ImportOffPeakRate: 0.2162,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3269,
			ImportOffPeakRate: 0.1986,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3633,
			ImportOffPeakRate: 0.2206,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"united_energy": {
		"origin_battery_maximiser": {
			Timezone:          melLocation,
			ImportPeakRate:    0.2970,
			ImportOffPeakRate: 0.1870,
			ExportPeakRate:    0.2200,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:          melLocation,
			ImportPeakRate:    0.4202,
			ImportOffPeakRate: 0.2435,
			ExportPeakRate:    0.1800,
			ExportOffPeakRate: 0.0300,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3760,
			ImportOffPeakRate: 0.2253,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3453,
			ImportOffPeakRate: 0.2069,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3837,
			ImportOffPeakRate: 0.2299,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"powercor": {
		"origin_battery_maximiser": {
			Timezone:          melLocation,
			ImportPeakRate:    0.2970,
			ImportOffPeakRate: 0.1870,
			ExportPeakRate:    0.2200,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:          melLocation,
			ImportPeakRate:    0.4268,
			ImportOffPeakRate: 0.2621,
			ExportPeakRate:    0.1800,
			ExportOffPeakRate: 0.0300,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3959,
			ImportOffPeakRate: 0.2318,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3636,
			ImportOffPeakRate: 0.2129,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:          melLocation,
			ImportPeakRate:    0.4040,
			ImportOffPeakRate: 0.2365,
			ExportFlatRate:    0.0100,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"ausgrid": {
		"origin_battery_maximiser": {
			Timezone:          sydLocation,
			ImportPeakRate:    0.5390,
			ImportOffPeakRate: 0.1870,
			ExportPeakRate:    0.2200,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:          sydLocation,
			ImportPeakRate:    0.5731,
			ImportOffPeakRate: 0.3300,
			ExportPeakRate:    0.1800,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.6944,
			ImportShoulderRate: 0.3633,
			ImportOffPeakRate:  0.2119,
			ExportFlatRate:     0.0300,
		},
		"origin_go_variable": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.6340,
			ImportShoulderRate: 0.3318,
			ImportOffPeakRate:  0.1935,
			ExportFlatRate:     0.0300,
		},
		"origin_solar_boost": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.7548,
			ImportShoulderRate: 0.3949,
			ImportOffPeakRate:  0.2303,
			ExportFlatRate:     0.0300,
		},
	},
	"endeavour": {
		"origin_battery_maximiser": {
			Timezone:          sydLocation,
			ImportPeakRate:    0.4400,
			ImportOffPeakRate: 0.2090,
			ExportPeakRate:    0.2200,
			ExportOffPeakRate: 0.0600,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:          sydLocation,
			ImportPeakRate:    0.5588,
			ImportOffPeakRate: 0.2750,
			ExportPeakRate:    0.1800,
			ExportOffPeakRate: 0.0500,
			PeakHours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:      []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.5214,
			ImportShoulderRate: 0.4179,
			ImportOffPeakRate:  0.2770,
			ExportFlatRate:     0.0300,
		},
		"origin_go_variable": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.4874,
			ImportShoulderRate: 0.3906,
			ImportOffPeakRate:  0.2589,
			ExportFlatRate:     0.0300,
		},
		"origin_solar_boost": {
			Timezone:           sydLocation,
			ImportPeakRate:     0.5667,
			ImportShoulderRate: 0.4542,
			ImportOffPeakRate:  0.3011,
			ExportFlatRate:     0.0300,
		},
	},
}

func planToName(plan string) string {
	switch plan {
	case "origin_battery_maximiser":
		return "Origin Battery Maximiser"
	case "origin_battery_starter":
		return "Origin Battery Starter"
	case "origin_go_solar_variable":
		return "Origin Go Solar Variable"
	case "origin_go_variable":
		return "Origin Go Variable"
	case "origin_solar_boost":
		return "Origin Solar Boost"
	default:
		return "Origin"
	}
}

func originPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	loc := opts.Location
	if loc == "" {
		loc = "energex"
	}

	distMap, ok := originRates[loc]
	if !ok {
		return nil, fmt.Errorf("unsupported location: %s", loc)
	}

	cfg, ok := distMap[plan]
	if !ok {
		return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
	}

	if loc == "ausgrid" && (plan == "origin_go_solar_variable" || plan == "origin_go_variable" || plan == "origin_solar_boost") {
		return ausgridSeasonalPeriods(plan, cfg, years)
	}

	if loc == "endeavour" && (plan == "origin_go_solar_variable" || plan == "origin_go_variable" || plan == "origin_solar_boost") {
		return endeavourWeekdayWeekendPeriods(plan, cfg, years)
	}

	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		if len(cfg.WeekdayPeakHours) > 0 {
			// 3-Period Weekday/Weekend TOU (e.g., Energex Go and Go Solar Variable)
			// Import Weekdays
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "On-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         cfg.WeekdayPeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakRate,
				Description:   fmt.Sprintf("%s Weekday Peak Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Shoulder",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         cfg.WeekdayShoulderHours,
				},
				DollarsPerKWH: cfg.ImportShoulderRate,
				Description:   fmt.Sprintf("%s Weekday Shoulder Rate", planToName(plan)),
			})

			// Import Weekends
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Shoulder",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
					Hours:         cfg.WeekendShoulderHours,
				},
				DollarsPerKWH: cfg.ImportShoulderRate,
				Description:   fmt.Sprintf("%s Weekend Shoulder Rate", planToName(plan)),
			})

			// Import Off-Peak (All Week)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.AllWeekOffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})

			// Export Flat Rate
			if plan == "origin_go_solar_variable" {
				// ====================================================================================
				// TODO: We do not support the premium 4c/kWh rate for the first 8kWh/day solar export limit
				// because it requires tracking daily cumulative solar exports in the fee calculation.
				// We statically use the uncapped standard feed-in tariff of $0.03/kWh.
				// ====================================================================================
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
					},
					DollarsPerKWH:            cfg.ExportFlatRate,
					SeparateGenerationCredit: true,
					Description:              "Origin Go Solar Variable Solar Export Tariff",
				})
			} else {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
					},
					DollarsPerKWH:            cfg.ExportFlatRate,
					SeparateGenerationCredit: true,
					Description:              fmt.Sprintf("%s Solar Export Tariff", planToName(plan)),
				})
			}

		} else if len(cfg.ShoulderHours) > 0 {
			// 3-Period Everyday TOU (e.g., Energex Solar Boost)
			// Import
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.PeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakRate,
				Description:   fmt.Sprintf("%s Peak Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.OffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Shoulder",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.ShoulderHours,
				},
				DollarsPerKWH: cfg.ImportShoulderRate,
				Description:   fmt.Sprintf("%s Shoulder Rate", planToName(plan)),
			})

			// Export
			// ====================================================================================
			// TODO: We do not support the premium 8c/kWh rate for the first 8kWh/day solar export limit
			// because it requires tracking daily cumulative solar exports in the fee calculation.
			// We statically use the uncapped standard feed-in tariff of $0.03/kWh.
			// ====================================================================================
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              "Origin Solar Boost Solar Export Tariff",
			})

		} else {
			// 2-Period Everyday TOU (e.g., Maximiser/Starter or Citipower Go/Go Solar/Solar Boost)
			// Import
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.PeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakRate,
				Description:   fmt.Sprintf("%s Peak Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.OffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})

			// Export
			if cfg.ExportFlatRate > 0 {
				if plan == "origin_go_solar_variable" {
					// ====================================================================================
					// TODO: We do not support the premium 4c/kWh rate for the first 8kWh/day solar export limit
					// because it requires tracking daily cumulative solar exports in the fee calculation.
					// We statically use the uncapped standard feed-in tariff of $0.01/kWh.
					// ====================================================================================
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:       allYearStart,
							End:         allYearEnd,
							LocationPtr: cfg.Timezone,
						},
						DollarsPerKWH:            cfg.ExportFlatRate,
						SeparateGenerationCredit: true,
						Description:              "Origin Go Solar Variable Solar Export Tariff",
					})
				} else if plan == "origin_solar_boost" {
					// ====================================================================================
					// TODO: We do not support the premium 5c/kWh rate for the first 8kWh/day solar export limit
					// because it requires tracking daily cumulative solar exports in the fee calculation.
					// We statically use the uncapped standard feed-in tariff of $0.01/kWh.
					// ====================================================================================
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:       allYearStart,
							End:         allYearEnd,
							LocationPtr: cfg.Timezone,
						},
						DollarsPerKWH:            cfg.ExportFlatRate,
						SeparateGenerationCredit: true,
						Description:              "Origin Solar Boost Solar Export Tariff",
					})
				} else {
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:       allYearStart,
							End:         allYearEnd,
							LocationPtr: cfg.Timezone,
						},
						DollarsPerKWH:            cfg.ExportFlatRate,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("%s Solar Export Tariff", planToName(plan)),
					})
				}
			} else {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       cfg.PeakHours,
					},
					DollarsPerKWH:            cfg.ExportPeakRate,
					SeparateGenerationCredit: true,
					Description:              fmt.Sprintf("%s Solar Export Peak Tariff", planToName(plan)),
				})
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       cfg.OffPeakHours,
					},
					DollarsPerKWH:            cfg.ExportOffPeakRate,
					SeparateGenerationCredit: true,
					Description:              fmt.Sprintf("%s Solar Export Off-Peak Tariff", planToName(plan)),
				})
			}
		}
	}

	return periods, nil
}

// originUtilityInfo returns metadata for Origin Energy.
func originUtilityInfo() types.UtilityProviderInfo {
	originOptions := []types.UtilityRateOption{
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
			},
			Default: "energex",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "origin",
		Name: "Origin Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "origin_battery_maximiser",
				Name:    "Origin Battery Maximiser",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_battery_maximiser", opts, []int{2026})
				},
			},
			{
				ID:      "origin_battery_starter",
				Name:    "Origin Battery Starter",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_battery_starter", opts, []int{2026})
				},
			},
			{
				ID:      "origin_go_solar_variable",
				Name:    "Origin Go Solar Variable",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_go_solar_variable", opts, []int{2026})
				},
			},
			{
				ID:      "origin_go_variable",
				Name:    "Origin Go Variable",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_go_variable", opts, []int{2026})
				},
			},
			{
				ID:      "origin_solar_boost",
				Name:    "Origin Solar Boost",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_solar_boost", opts, []int{2026})
				},
			},
		},
	}
}

func ausgridSeasonalPeriods(plan string, cfg originRateConfig, years []int) ([]types.UtilityFeesPeriod, error) {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		// Seasons:
		// Summer: 31 Oct - 30 Mar
		// Winter: 31 May - 30 Aug
		// Non-Summer Non-Winter: 31 Mar - 30 May & 31 Aug - 30 Oct

		// Hours:
		offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}}

		// Summer peak / shoulder
		summerPeakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}}
		summerShoulderHoursWeekday := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 14}, {HourStart: 20, HourEnd: 22}}
		summerShoulderHoursWeekend := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}}

		// Winter peak / shoulder
		winterPeakHours := []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}}
		winterShoulderHoursWeekday := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 17}, {HourStart: 21, HourEnd: 22}}
		winterShoulderHoursWeekend := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}}

		// Non-Summer Non-Winter shoulder (all week)
		shoulderHoursAllWeek := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}}

		// Define the date ranges for the seasons:
		seasons := []struct {
			start      time.Time
			end        time.Time
			isSummer   bool
			isWinter   bool
			isShoulder bool
		}{
			// Summer Part 1: Jan 1 to Mar 31
			{start: time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.March, 31, 0, 0, 0, 0, sydLocation), isSummer: true},
			// Non-Summer/Winter: Mar 31 to May 31
			{start: time.Date(year, time.March, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.May, 31, 0, 0, 0, 0, sydLocation), isShoulder: true},
			// Winter: May 31 to Aug 31
			{start: time.Date(year, time.May, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.August, 31, 0, 0, 0, 0, sydLocation), isWinter: true},
			// Non-Summer/Winter: Aug 31 to Oct 31
			{start: time.Date(year, time.August, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.October, 31, 0, 0, 0, 0, sydLocation), isShoulder: true},
			// Summer Part 2: Oct 31 to Dec 31
			{start: time.Date(year, time.October, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation), isSummer: true},
		}

		for _, s := range seasons {
			if s.isSummer {
				// Weekday Peak
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "On-Peak",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         summerPeakHours,
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   fmt.Sprintf("%s Summer Weekday Peak Rate", planToName(plan)),
				})
				// Weekday Shoulder
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         summerShoulderHoursWeekday,
					},
					DollarsPerKWH: cfg.ImportShoulderRate,
					Description:   fmt.Sprintf("%s Summer Weekday Shoulder Rate", planToName(plan)),
				})
				// Weekend Shoulder
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
						Hours:         summerShoulderHoursWeekend,
					},
					DollarsPerKWH: cfg.ImportShoulderRate,
					Description:   fmt.Sprintf("%s Summer Weekend Shoulder Rate", planToName(plan)),
				})
			} else if s.isWinter {
				// Weekday Peak
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "On-Peak",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         winterPeakHours,
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   fmt.Sprintf("%s Winter Weekday Peak Rate", planToName(plan)),
				})
				// Weekday Shoulder
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         winterShoulderHoursWeekday,
					},
					DollarsPerKWH: cfg.ImportShoulderRate,
					Description:   fmt.Sprintf("%s Winter Weekday Shoulder Rate", planToName(plan)),
				})
				// Weekend Shoulder
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
						Hours:         winterShoulderHoursWeekend,
					},
					DollarsPerKWH: cfg.ImportShoulderRate,
					Description:   fmt.Sprintf("%s Winter Weekend Shoulder Rate", planToName(plan)),
				})
			} else if s.isShoulder {
				// All week Shoulder
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Shoulder",
						Start:       s.start,
						End:         s.end,
						LocationPtr: sydLocation,
						Hours:       shoulderHoursAllWeek,
					},
					DollarsPerKWH: cfg.ImportShoulderRate,
					Description:   fmt.Sprintf("%s Shoulder Months Rate", planToName(plan)),
				})
			}

			// Off-Peak (all week, all seasons)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       s.start,
					End:         s.end,
					LocationPtr: sydLocation,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakRate,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})
		}

		// Solar flat export credit
		// We statically use standard feed-in tariff of 3c/kWh ($0.03/kWh)
		if plan == "origin_go_solar_variable" {
			// TODO: We do not support the premium 5c/kWh rate for the first 8kWh/day solar export limit
			// because it requires tracking daily cumulative solar exports in the fee calculation.
			// We statically use the uncapped standard feed-in tariff of $0.03/kWh.
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation),
					LocationPtr: sydLocation,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              "Origin Go Solar Variable Solar Export Tariff",
			})
		} else if plan == "origin_solar_boost" {
			// TODO: We do not support the premium 8c/kWh rate for the first 8kWh/day solar export limit
			// because it requires tracking daily cumulative solar exports in the fee calculation.
			// We statically use the uncapped standard feed-in tariff of $0.03/kWh.
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation),
					LocationPtr: sydLocation,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              "Origin Solar Boost Solar Export Tariff",
			})
		} else {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation),
					LocationPtr: sydLocation,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              fmt.Sprintf("%s Solar Export Tariff", planToName(plan)),
			})
		}
	}

	return periods, nil
}

func endeavourWeekdayWeekendPeriods(plan string, cfg originRateConfig, years []int) ([]types.UtilityFeesPeriod, error) {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

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
			Description:   fmt.Sprintf("%s Weekday Peak Rate", planToName(plan)),
		})

		// Weekday Shoulder (7 AM to 1 PM & 8 PM to 10 PM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Shoulder",
				Start:         allYearStart,
				End:           allYearEnd,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
				Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 13}, {HourStart: 20, HourEnd: 22}},
			},
			DollarsPerKWH: cfg.ImportShoulderRate,
			Description:   fmt.Sprintf("%s Weekday Shoulder Rate", planToName(plan)),
		})

		// Weekday Off-Peak (10 PM to 7 AM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         allYearStart,
				End:           allYearEnd,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakRate,
			Description:   fmt.Sprintf("%s Weekday Off-Peak Rate", planToName(plan)),
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
			Description:   fmt.Sprintf("%s Weekend Off-Peak Rate", planToName(plan)),
		})

		// Solar flat export credit
		// We statically use standard feed-in tariff of 3c/kWh ($0.03/kWh)
		if plan == "origin_go_solar_variable" {
			// TODO: We do not support the premium 5c/kWh rate for the first 8kWh/day solar export limit
			// because it requires tracking daily cumulative solar exports in the fee calculation.
			// We statically use the uncapped standard feed-in tariff of $0.03/kWh.
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              "Origin Go Solar Variable Solar Export Tariff",
			})
		} else if plan == "origin_solar_boost" {
			// TODO: We do not support the premium 8c/kWh rate for the first 8kWh/day solar export limit
			// because it requires tracking daily cumulative solar exports in the fee calculation.
			// We statically use the uncapped standard feed-in tariff of $0.03/kWh.
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              "Origin Solar Boost Solar Export Tariff",
			})
		} else {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH:            cfg.ExportFlatRate,
				SeparateGenerationCredit: true,
				Description:              fmt.Sprintf("%s Solar Export Tariff", planToName(plan)),
			})
		}
	}

	return periods, nil
}
