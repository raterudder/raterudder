package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type engieRateConfig struct {
	Timezone             *time.Location
	ImportSingleRate     float64
	ImportPeakRate       float64
	ImportWinterPeakRate float64
	ImportOffPeakRate    float64
	ImportShoulderRate   float64
	ExportFlatRate       float64
}

// engieRates maps plan ID -> location -> configuration.
// Citipower and Jemena fallback to United Energy rates as Melbourne distributor approximations.
var engieRates = map[string]map[string]engieRateConfig{
	"engie_solar_elec_single": {
		"ausgrid": {
			Timezone:         sydLocation,
			ImportSingleRate: 0.4011,
			ExportFlatRate:   0.0300,
		},
		"endeavour": {
			Timezone:         sydLocation,
			ImportSingleRate: 0.4048,
			ExportFlatRate:   0.0300,
		},
		"energex": {
			Timezone:         bneLocation,
			ImportSingleRate: 0.3711,
			ExportFlatRate:   0.0100,
		},
		"powercor": {
			Timezone:         melLocation,
			ImportSingleRate: 0.3009,
			ExportFlatRate:   0.0100,
		},
		"united_energy": {
			Timezone:         melLocation,
			ImportSingleRate: 0.2883,
			ExportFlatRate:   0.0100,
		},
		"citipower": {
			Timezone:         melLocation,
			ImportSingleRate: 0.2883,
			ExportFlatRate:   0.0100,
		},
		"jemena": {
			Timezone:         melLocation,
			ImportSingleRate: 0.2883,
			ExportFlatRate:   0.0100,
		},
	},
	"engie_solar_elec_tou": {
		"ausgrid": {
			Timezone:          sydLocation,
			ImportPeakRate:    0.4288,
			ImportOffPeakRate: 0.3927,
			ExportFlatRate:    0.0300,
		},
		"endeavour": {
			Timezone:             sydLocation,
			ImportPeakRate:       0.6564,
			ImportWinterPeakRate: 0.4204,
			ImportOffPeakRate:    0.3838,
			ExportFlatRate:       0.0300,
		},
		"energex": {
			Timezone:           bneLocation,
			ImportPeakRate:     0.5613,
			ImportOffPeakRate:  0.2621,
			ImportShoulderRate: 0.3059,
			ExportFlatRate:     0.0100,
		},
		"powercor": {
			Timezone:          melLocation,
			ImportPeakRate:    0.4039,
			ImportOffPeakRate: 0.2365,
			ExportFlatRate:    0.0100,
		},
		"united_energy": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3837,
			ImportOffPeakRate: 0.2299,
			ExportFlatRate:    0.0100,
		},
		"citipower": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3837,
			ImportOffPeakRate: 0.2299,
			ExportFlatRate:    0.0100,
		},
		"jemena": {
			Timezone:          melLocation,
			ImportPeakRate:    0.3837,
			ImportOffPeakRate: 0.2299,
			ExportFlatRate:    0.0100,
		},
	},
}

func engiePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	loc := opts.Location
	if loc == "" {
		loc = "ausgrid"
	}

	cfg, ok := engieRates[plan][loc]
	if !ok {
		cfg = engieRates[plan]["ausgrid"]
	}

	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		switch plan {
		case "engie_solar_elec_single":
			periods = append(periods, types.UtilityFeesPeriod{
				UtilityPeriod: types.UtilityPeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH: cfg.ImportSingleRate,
				Description:   fmt.Sprintf("%s ENGIE Solar Elec Single Rate Usage Charge", loc),
			})

		case "engie_solar_elec_tou":
			if loc == "ausgrid" {
				// Ausgrid TOU with Seasonal Peak/Off-Peak/Shoulder
				peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}}
				offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}}

				// Segment 1: Summer Peak (Jan 1 - Mar 30, inclusive)
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone),
						LocationPtr: cfg.Timezone,
						Hours:       peakHours,
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   "Ausgrid ENGIE Summer Peak Rate",
				})
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone),
						LocationPtr: cfg.Timezone,
						Hours:       offPeakHours,
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Ausgrid ENGIE Summer Off-Peak Rate",
				})

				// Segment 2: Non-Summer Non-Winter (Mar 31 - May 30, inclusive)
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone),
						End:         time.Date(year, time.May, 31, 0, 0, 0, 0, cfg.Timezone),
						LocationPtr: cfg.Timezone,
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Ausgrid ENGIE Non-Summer Non-Winter Rate",
				})

				// Segment 3: Winter Peak (May 31 - Aug 30, inclusive)
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       time.Date(year, time.May, 31, 0, 0, 0, 0, cfg.Timezone),
						End:         time.Date(year, time.August, 31, 0, 0, 0, 0, cfg.Timezone),
						LocationPtr: cfg.Timezone,
						Hours:       peakHours,
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   "Ausgrid ENGIE Winter Peak Rate",
				})
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       time.Date(year, time.May, 31, 0, 0, 0, 0, cfg.Timezone),
						End:         time.Date(year, time.August, 31, 0, 0, 0, 0, cfg.Timezone),
						LocationPtr: cfg.Timezone,
						Hours:       offPeakHours,
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Ausgrid ENGIE Winter Off-Peak Rate",
				})

				// Segment 4: Non-Summer Non-Winter (Aug 31 - Oct 30, inclusive)
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       time.Date(year, time.August, 31, 0, 0, 0, 0, cfg.Timezone),
						End:         time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone),
						LocationPtr: cfg.Timezone,
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Ausgrid ENGIE Non-Summer Non-Winter Rate",
				})

				// Segment 5: Summer Peak (Oct 31 - Dec 31, inclusive)
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone),
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       peakHours,
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   "Ausgrid ENGIE Summer Peak Rate",
				})
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone),
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       offPeakHours,
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Ausgrid ENGIE Summer Off-Peak Rate",
				})

			} else if loc == "endeavour" {
				// Endeavour TOU with Peak/Off-Peak depending on Summer/Winter season
				// Peak is weekdays 4 PM - 8 PM (16:00 to 20:00)
				peakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}}
				offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 20, HourEnd: 24}}

				// Summer Peak Season: Jan 1 to Mar 31 & Oct 31 to Dec 31
				summerPeriods := []struct {
					start time.Time
					end   time.Time
				}{
					{start: allYearStart, end: time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone)},
					{start: time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone), end: allYearEnd},
				}

				for _, sp := range summerPeriods {
					// Weekday Peak
					periods = append(periods, types.UtilityFeesPeriod{
						UtilityPeriod: types.UtilityPeriod{
							Start:         sp.start,
							End:           sp.end,
							LocationPtr:   cfg.Timezone,
							DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
							Hours:         peakHours,
						},
						DollarsPerKWH: cfg.ImportPeakRate,
						Description:   "Endeavour ENGIE Summer Weekday Peak Rate",
					})

					// Weekday Off-Peak
					periods = append(periods, types.UtilityFeesPeriod{
						UtilityPeriod: types.UtilityPeriod{
							Start:         sp.start,
							End:           sp.end,
							LocationPtr:   cfg.Timezone,
							DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
							Hours:         offPeakHours,
						},
						DollarsPerKWH: cfg.ImportOffPeakRate,
						Description:   "Endeavour ENGIE Summer Weekday Off-Peak Rate",
					})

					// Weekend Off-Peak (All Day)
					periods = append(periods, types.UtilityFeesPeriod{
						UtilityPeriod: types.UtilityPeriod{
							Start:         sp.start,
							End:           sp.end,
							LocationPtr:   cfg.Timezone,
							DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
							Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
						},
						DollarsPerKWH: cfg.ImportOffPeakRate,
						Description:   "Endeavour ENGIE Summer Weekend Off-Peak Rate",
					})
				}

				// Winter Peak Season: Mar 31 to Oct 31
				winterStart := time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone)
				winterEnd := time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone)

				// Weekday Peak
				peakRate := cfg.ImportWinterPeakRate
				if peakRate == 0 {
					peakRate = cfg.ImportPeakRate
				}
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:         winterStart,
						End:           winterEnd,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         peakHours,
					},
					DollarsPerKWH: peakRate,
					Description:   "Endeavour ENGIE Winter Weekday Peak Rate",
				})

				// Weekday Off-Peak
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:         winterStart,
						End:           winterEnd,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         offPeakHours,
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Endeavour ENGIE Winter Weekday Off-Peak Rate",
				})

				// Weekend Off-Peak (All Day)
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:         winterStart,
						End:           winterEnd,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
						Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Endeavour ENGIE Winter Weekend Off-Peak Rate",
				})

			} else if loc == "energex" {
				// Energex 3-Period Everyday TOU
				// Peak: 4 PM - 9 PM
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   "Energex ENGIE Everyday Peak Rate",
				})

				// Off-Peak: 11 AM - 4 PM
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   "Energex ENGIE Everyday Off-Peak Rate",
				})

				// Shoulder: 9 PM - 11 AM
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
					},
					DollarsPerKWH: cfg.ImportShoulderRate,
					Description:   "Energex ENGIE Everyday Shoulder Rate",
				})

			} else {
				// VIC (Citipower, Powercor, United Energy, Jemena): 2-Period Everyday TOU
				// Peak: 3 PM - 9 PM
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
					},
					DollarsPerKWH: cfg.ImportPeakRate,
					Description:   fmt.Sprintf("%s ENGIE Everyday Peak Rate", loc),
				})

				// Off-Peak: all other times
				periods = append(periods, types.UtilityFeesPeriod{
					UtilityPeriod: types.UtilityPeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
					},
					DollarsPerKWH: cfg.ImportOffPeakRate,
					Description:   fmt.Sprintf("%s ENGIE Everyday Off-Peak Rate", loc),
				})
			}
		}

		// Solar flat feed-in credit
		// ====================================================================================
		// NOTE: We do not support the premium tiered feed-in rate (8c/kWh for the first 8kWh exported
		// per day) because we don't track daily cumulative exported kWh.
		// Therefore, we statically apply the flat lower feed-in rate (3c/kWh or 1c/kWh).
		// ====================================================================================
		periods = append(periods, types.UtilityFeesPeriod{
			UtilityPeriod: types.UtilityPeriod{
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
			},
			DollarsPerKWH:            cfg.ExportFlatRate,
			SeparateGenerationCredit: true,
			Description:              fmt.Sprintf("%s ENGIE Solar Feed-in Tariff (Lower Tier)", loc),
		})
	}

	return periods
}

// engieUtilityInfo returns metadata for ENGIE.
func engieUtilityInfo() types.UtilityProviderInfo {
	engieOptions := []types.UtilityRateOption{
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
				{Value: "jemena", Name: "Melbourne Northern & Western Suburbs"},
			},
			Default: "ausgrid",
		},
	}

	return types.UtilityProviderInfo{
		ID:   "engie",
		Name: "ENGIE",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "engie_solar_elec_single",
				Name:    "Solar Flat Rate",
				Options: engieOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return engiePeriods("engie_solar_elec_single", opts, []int{2026}), nil
				},
			},
			{
				ID:      "engie_solar_elec_tou",
				Name:    "Solar Time of Use",
				Options: engieOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return engiePeriods("engie_solar_elec_tou", opts, []int{2026}), nil
				},
			},
		},
	}
}
