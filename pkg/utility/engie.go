// Tariff Audit Metadata
// - Provider(s): engie (ENGIE)
// - Official Sources:
//   - https://cdr.energymadeeasy.gov.au/engie/cds-au/v1/energy/plans
//   - https://www.energymadeeasy.gov.au/plan?id=ENG1207410MRE2&postcode=2000
//   - https://compare.energy.vic.gov.au/
//   - https://engie.com.au/help-centre/policies-and-commitments/solar-fit-policy
//   - AER and VEC CDR Plans: ENG1207410MRE2@EME, ENG1207431MRE2@EME, ENG1208445MRE2@EME, ENG1208451MRE2@EME, ENG1207926MRE2@EME, ENG1207930MRE2@EME, ENG764475MR@VEC, ENG764477MR@VEC, ENG764479MR@VEC, ENG764483MR@VEC, ENG764495MR@VEC, ENG764497MR@VEC, ENG764499MR@VEC, ENG764503MR@VEC
// - Effective Window: 2026-07-01 (VIC) and 2026-10-27 (NSW/QLD) through until superseded (modeled through 2027-12-31)
// - Revision Cadence: Annual (Jul 1 AER DMO and ESC VDO market offer cycle)
// - Next Audit Due: 2027-07-01 (Check FY2027-28 AER DMO and ESC VDO ENGIE Solar plan updates)
// - Last Verified: 2026-11-21
// - Modeling Notes: GST-inclusive volumetric usage rates across 7 NEM distributors (ausgrid, endeavour, energex, powercor, united_energy, citipower, jemena). Fixed daily supply charges ($/day) are excluded. Stepped solar FiT upper tier (10c/kWh in NSW and 8c/kWh in QLD/VIC for first 8 kWh/day) is omitted because daily export volume is not tracked, so uncapped lower-tier credits (3c/kWh NSW, 1c/kWh QLD/VIC) apply.

package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type engieRateConfig struct {
	Timezone                      *time.Location
	ImportSingleDollarsPerKWH     float64
	ImportPeakDollarsPerKWH       float64
	ImportWinterPeakDollarsPerKWH float64
	ImportOffPeakDollarsPerKWH    float64
	ImportShoulderDollarsPerKWH   float64
	ImportSpongeDollarsPerKWH     float64
	ImportSaverDollarsPerKWH      float64
	ExportFlatDollarsPerKWH       float64
}

// engieLegacyRates preserves pre-cutoff 2026 rates before 2026-07-01 (VIC) and 2026-10-27 (NSW/QLD).
// Prior to 2026-07-01, Citipower and Jemena fell back to United Energy rates.
var engieLegacyRates = map[string]map[string]engieRateConfig{
	"engie_solar_elec_single": {
		"ausgrid": {
			Timezone:                  sydLocation,
			ImportSingleDollarsPerKWH: 0.4011,
			ExportFlatDollarsPerKWH:   0.0300,
		},
		"endeavour": {
			Timezone:                  sydLocation,
			ImportSingleDollarsPerKWH: 0.4048,
			ExportFlatDollarsPerKWH:   0.0300,
		},
		"energex": {
			Timezone:                  bneLocation,
			ImportSingleDollarsPerKWH: 0.3711,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"powercor": {
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.3009,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"united_energy": {
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.2883,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"citipower": {
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.2883,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"jemena": {
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.2883,
			ExportFlatDollarsPerKWH:   0.0100,
		},
	},
	"engie_solar_elec_tou": {
		"ausgrid": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.4288,
			ImportOffPeakDollarsPerKWH: 0.3927,
			ExportFlatDollarsPerKWH:    0.0300,
		},
		"endeavour": {
			Timezone:                      sydLocation,
			ImportPeakDollarsPerKWH:       0.6564,
			ImportWinterPeakDollarsPerKWH: 0.4204,
			ImportOffPeakDollarsPerKWH:    0.3838,
			ExportFlatDollarsPerKWH:       0.0300,
		},
		"energex": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.5613,
			ImportOffPeakDollarsPerKWH:  0.2621,
			ImportShoulderDollarsPerKWH: 0.3059,
			ExportFlatDollarsPerKWH:     0.0100,
		},
		"powercor": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.4039,
			ImportOffPeakDollarsPerKWH: 0.2365,
			ExportFlatDollarsPerKWH:    0.0100,
		},
		"united_energy": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3837,
			ImportOffPeakDollarsPerKWH: 0.2299,
			ExportFlatDollarsPerKWH:    0.0100,
		},
		"citipower": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3837,
			ImportOffPeakDollarsPerKWH: 0.2299,
			ExportFlatDollarsPerKWH:    0.0100,
		},
		"jemena": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3837,
			ImportOffPeakDollarsPerKWH: 0.2299,
			ExportFlatDollarsPerKWH:    0.0100,
		},
	},
}

// engieRates maps plan ID -> location -> official 2026-2027 GST-inclusive CDR configuration
// effective 2026-07-01 (VIC) and 2026-10-27 (NSW/QLD).
var engieRates = map[string]map[string]engieRateConfig{
	"engie_solar_elec_single": {
		"ausgrid": { // ENG1207410MRE2@EME (EA010)
			Timezone:                  sydLocation,
			ImportSingleDollarsPerKWH: 0.33132,
			ExportFlatDollarsPerKWH:   0.0300,
		},
		"endeavour": { // ENG1208445MRE2@EME (N70)
			Timezone:                  sydLocation,
			ImportSingleDollarsPerKWH: 0.33726,
			ExportFlatDollarsPerKWH:   0.0300,
		},
		"energex": { // ENG1207926MRE2@EME (8400)
			Timezone:                  bneLocation,
			ImportSingleDollarsPerKWH: 0.27973,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"powercor": { // ENG764479MR@VEC (PR)
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.28215,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"united_energy": { // ENG764483MR@VEC (UR)
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.27346,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"citipower": { // ENG764475MR@VEC (CR)
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.25960,
			ExportFlatDollarsPerKWH:   0.0100,
		},
		"jemena": { // ENG764477MR@VEC (JR)
			Timezone:                  melLocation,
			ImportSingleDollarsPerKWH: 0.27467,
			ExportFlatDollarsPerKWH:   0.0100,
		},
	},
	"engie_solar_elec_tou": {
		"ausgrid": { // ENG1207431MRE2@EME (EA025)
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.60192,
			ImportOffPeakDollarsPerKWH: 0.24035,
			ExportFlatDollarsPerKWH:    0.0300,
		},
		"endeavour": { // ENG1208451MRE2@EME (N71)
			Timezone:                      sydLocation,
			ImportPeakDollarsPerKWH:       0.47135,
			ImportWinterPeakDollarsPerKWH: 0.46563,
			ImportSpongeDollarsPerKWH:     0.12342,
			ImportOffPeakDollarsPerKWH:    0.35684,
			ExportFlatDollarsPerKWH:       0.0300,
		},
		"energex": { // ENG1207930MRE2@EME (6900)
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.47784,
			ImportOffPeakDollarsPerKWH:  0.06974,
			ImportShoulderDollarsPerKWH: 0.25289,
			ExportFlatDollarsPerKWH:     0.0100,
		},
		"powercor": { // ENG764499MR@VEC
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.41668,
			ImportSaverDollarsPerKWH:   0.17259,
			ImportOffPeakDollarsPerKWH: 0.22484,
			ExportFlatDollarsPerKWH:    0.0100,
		},
		"united_energy": { // ENG764503MR@VEC
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.40315,
			ImportSaverDollarsPerKWH:   0.17259,
			ImportOffPeakDollarsPerKWH: 0.22132,
			ExportFlatDollarsPerKWH:    0.0100,
		},
		"citipower": { // ENG764495MR@VEC
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.38302,
			ImportSaverDollarsPerKWH:   0.16588,
			ImportOffPeakDollarsPerKWH: 0.21164,
			ExportFlatDollarsPerKWH:    0.0100,
		},
		"jemena": { // ENG764497MR@VEC
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.37565,
			ImportSaverDollarsPerKWH:   0.17644,
			ImportOffPeakDollarsPerKWH: 0.21758,
			ExportFlatDollarsPerKWH:    0.0100,
		},
	},
}

func isEngieVictoriaLocation(loc string) bool {
	switch loc {
	case "powercor", "united_energy", "citipower", "jemena":
		return true
	default:
		return false
	}
}

func engie2026Cutoff(loc string, tz *time.Location) time.Time {
	if isEngieVictoriaLocation(loc) {
		return time.Date(2026, time.July, 1, 0, 0, 0, 0, tz)
	}
	return time.Date(2026, time.October, 27, 0, 0, 0, 0, tz)
}

func clipEngiePeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
	var clipped []types.UtilityFeesPeriod
	for _, p := range periods {
		if !windowStart.IsZero() && (p.Start.IsZero() || p.Start.Before(windowStart)) {
			p.Start = windowStart
		}
		if !windowEnd.IsZero() && (p.End.IsZero() || p.End.After(windowEnd)) {
			p.End = windowEnd
		}
		if p.End.After(p.Start) {
			clipped = append(clipped, p)
		}
	}
	return clipped
}

func engieLegacyYearPeriods(plan, loc string, cfg engieRateConfig, year int) []types.UtilityFeesPeriod {
	allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
	allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

	var periods []types.UtilityFeesPeriod

	switch plan {
	case "engie_solar_elec_single":
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
			},
			DollarsPerKWH: cfg.ImportSingleDollarsPerKWH,
			Description:   fmt.Sprintf("%s ENGIE Solar Elec Single Rate Usage Charge", loc),
		})

	case "engie_solar_elec_tou":
		if loc == "ausgrid" {
			peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}}
			offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}}

			// Segment 1: Summer Peak (Jan 1 - Mar 30, inclusive)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone),
					LocationPtr: cfg.Timezone,
					Hours:       peakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Peak Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone),
					LocationPtr: cfg.Timezone,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Off-Peak Rate",
			})

			// Segment 2: Non-Summer Non-Winter (Mar 31 - May 30, inclusive)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone),
					End:         time.Date(year, time.May, 31, 0, 0, 0, 0, cfg.Timezone),
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Non-Summer Non-Winter Rate",
			})

			// Segment 3: Winter Peak (May 31 - Aug 30, inclusive)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       time.Date(year, time.May, 31, 0, 0, 0, 0, cfg.Timezone),
					End:         time.Date(year, time.August, 31, 0, 0, 0, 0, cfg.Timezone),
					LocationPtr: cfg.Timezone,
					Hours:       peakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Winter Peak Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       time.Date(year, time.May, 31, 0, 0, 0, 0, cfg.Timezone),
					End:         time.Date(year, time.August, 31, 0, 0, 0, 0, cfg.Timezone),
					LocationPtr: cfg.Timezone,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Winter Off-Peak Rate",
			})

			// Segment 4: Non-Summer Non-Winter (Aug 31 - Oct 30, inclusive)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       time.Date(year, time.August, 31, 0, 0, 0, 0, cfg.Timezone),
					End:         time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone),
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Non-Summer Non-Winter Rate",
			})

			// Segment 5: Summer Peak (Oct 31 - Dec 31, inclusive)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone),
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       peakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Peak Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone),
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Off-Peak Rate",
			})

		} else if loc == "endeavour" {
			peakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}}
			offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 20, HourEnd: 24}}

			summerPeriods := []struct {
				start time.Time
				end   time.Time
			}{
				{start: allYearStart, end: time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone)},
				{start: time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone), end: allYearEnd},
			}

			for _, sp := range summerPeriods {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "On-Peak",
						Start:         sp.start,
						End:           sp.end,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         peakHours,
					},
					DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
					Description:   "Endeavour ENGIE Summer Weekday Peak Rate",
				})

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Off-Peak",
						Start:         sp.start,
						End:           sp.end,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         offPeakHours,
					},
					DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
					Description:   "Endeavour ENGIE Summer Weekday Off-Peak Rate",
				})

				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Off-Peak",
						Start:         sp.start,
						End:           sp.end,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
						Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
					},
					DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
					Description:   "Endeavour ENGIE Summer Weekend Off-Peak Rate",
				})
			}

			winterStart := time.Date(year, time.March, 31, 0, 0, 0, 0, cfg.Timezone)
			winterEnd := time.Date(year, time.October, 31, 0, 0, 0, 0, cfg.Timezone)

			winterPeakDollarsPerKWH := cfg.ImportWinterPeakDollarsPerKWH
			if winterPeakDollarsPerKWH == 0 {
				winterPeakDollarsPerKWH = cfg.ImportPeakDollarsPerKWH
			}
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "On-Peak",
					Start:         winterStart,
					End:           winterEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         peakHours,
				},
				DollarsPerKWH: winterPeakDollarsPerKWH,
				Description:   "Endeavour ENGIE Winter Weekday Peak Rate",
			})

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         winterStart,
					End:           winterEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Endeavour ENGIE Winter Weekday Off-Peak Rate",
			})

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         winterStart,
					End:           winterEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
					Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Endeavour ENGIE Winter Weekend Off-Peak Rate",
			})

		} else if loc == "energex" {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Energex ENGIE Everyday Peak Rate",
			})

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Energex ENGIE Everyday Off-Peak Rate",
			})

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Mid-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
				Description:   "Energex ENGIE Everyday Shoulder Rate",
			})

		} else {
			// Legacy VIC (Citipower, Powercor, United Energy, Jemena): 2-Period Everyday TOU
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s ENGIE Everyday Peak Rate", loc),
			})

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s ENGIE Everyday Off-Peak Rate", loc),
			})
		}
	}

	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       allYearStart,
			End:         allYearEnd,
			LocationPtr: cfg.Timezone,
		},
		DollarsPerKWH:            cfg.ExportFlatDollarsPerKWH,
		SeparateGenerationCredit: true,
		Description:              fmt.Sprintf("%s ENGIE Solar Feed-in Tariff (Lower Tier)", loc),
	})

	return periods
}

func engieCurrentYearPeriods(plan, loc string, cfg engieRateConfig, year int) []types.UtilityFeesPeriod {
	allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
	allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

	var periods []types.UtilityFeesPeriod

	switch plan {
	case "engie_solar_elec_single":
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: cfg.Timezone,
			},
			DollarsPerKWH: cfg.ImportSingleDollarsPerKWH,
			Description:   fmt.Sprintf("%s ENGIE Solar Elec Single Rate Usage Charge", loc),
		})

	case "engie_solar_elec_tou":
		if loc == "ausgrid" {
			// Ausgrid EA025 TOU:
			// Daily 15:00-21:00 Peak (Nov 1 - Mar 31 and Jun 1 - Aug 31), Off-Peak all other hours.
			peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}}
			offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}}

			apr1 := time.Date(year, time.April, 1, 0, 0, 0, 0, cfg.Timezone)
			jun1 := time.Date(year, time.June, 1, 0, 0, 0, 0, cfg.Timezone)
			sep1 := time.Date(year, time.September, 1, 0, 0, 0, 0, cfg.Timezone)
			nov1 := time.Date(year, time.November, 1, 0, 0, 0, 0, cfg.Timezone)

			// Segment 1: Summer (Jan 1 - Mar 31, inclusive -> [Jan 1, Apr 1))
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         apr1,
					LocationPtr: cfg.Timezone,
					Hours:       peakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Peak Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         apr1,
					LocationPtr: cfg.Timezone,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Off-Peak Rate",
			})

			// Segment 2: Autumn Off-Peak (Apr 1 - May 31, inclusive -> [Apr 1, Jun 1))
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       apr1,
					End:         jun1,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Non-Summer Non-Winter Rate",
			})

			// Segment 3: Winter (Jun 1 - Aug 31, inclusive -> [Jun 1, Sep 1))
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       jun1,
					End:         sep1,
					LocationPtr: cfg.Timezone,
					Hours:       peakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Winter Peak Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       jun1,
					End:         sep1,
					LocationPtr: cfg.Timezone,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Winter Off-Peak Rate",
			})

			// Segment 4: Spring Off-Peak (Sep 1 - Oct 31, inclusive -> [Sep 1, Nov 1))
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       sep1,
					End:         nov1,
					LocationPtr: cfg.Timezone,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Non-Summer Non-Winter Rate",
			})

			// Segment 5: Summer (Nov 1 - Dec 31, inclusive -> [Nov 1, next Jan 1))
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       nov1,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       peakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Peak Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       nov1,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Ausgrid ENGIE Summer Off-Peak Rate",
			})

		} else if loc == "endeavour" {
			// Endeavour N71 TOU:
			// Weekday 16:00-20:00 Peak (Nov 1 - Mar 31 Summer; Apr 1 - Oct 31 Winter),
			// Daily 10:00-14:00 Sponge, Off-Peak all other hours (split around 10:00-14:00).
			weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
			weekends := []time.Weekday{time.Saturday, time.Sunday}
			peakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}}
			spongeHours := []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 14}}
			weekdayOffPeakHours := []types.UtilityHourPeriod{
				{HourStart: 0, HourEnd: 10},
				{HourStart: 14, HourEnd: 16},
				{HourStart: 20, HourEnd: 24},
			}
			weekendOffPeakHours := []types.UtilityHourPeriod{
				{HourStart: 0, HourEnd: 10},
				{HourStart: 14, HourEnd: 24},
			}

			apr1 := time.Date(year, time.April, 1, 0, 0, 0, 0, cfg.Timezone)
			nov1 := time.Date(year, time.November, 1, 0, 0, 0, 0, cfg.Timezone)

			// Summer Weekday Peak: Jan 1 - Mar 31 ([Jan 1, Apr 1)) and Nov 1 - Dec 31 ([Nov 1, next Jan 1))
			for _, sp := range []struct {
				start time.Time
				end   time.Time
			}{
				{start: allYearStart, end: apr1},
				{start: nov1, end: allYearEnd},
			} {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "On-Peak",
						Start:         sp.start,
						End:           sp.end,
						LocationPtr:   cfg.Timezone,
						DaysOfTheWeek: weekdays,
						Hours:         peakHours,
					},
					DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
					Description:   "Endeavour ENGIE Summer Weekday Peak Rate",
				})
			}

			// Winter Weekday Peak: Apr 1 - Oct 31 ([Apr 1, Nov 1))
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "On-Peak",
					Start:         apr1,
					End:           nov1,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: weekdays,
					Hours:         peakHours,
				},
				DollarsPerKWH: cfg.ImportWinterPeakDollarsPerKWH,
				Description:   "Endeavour ENGIE Winter Weekday Peak Rate",
			})

			// Daily Sponge (10:00-14:00 every day)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Super Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       spongeHours,
				},
				DollarsPerKWH: cfg.ImportSpongeDollarsPerKWH,
				Description:   "Endeavour ENGIE Everyday Sponge Rate",
			})

			// Weekday Off-Peak (split around 10:00-14:00 Sponge and 16:00-20:00 Peak)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: weekdays,
					Hours:         weekdayOffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Endeavour ENGIE Weekday Off-Peak Rate",
			})

			// Weekend Off-Peak (split around 10:00-14:00 Sponge)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: weekends,
					Hours:         weekendOffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Endeavour ENGIE Weekend Off-Peak Rate",
			})

		} else if loc == "energex" {
			// Energex 6900 3-Period Everyday TOU
			// Peak: 16:00 - 21:00
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Energex ENGIE Everyday Peak Rate",
			})

			// Off-Peak: 11:00 - 16:00
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   "Energex ENGIE Everyday Off-Peak Rate",
			})

			// Shoulder: 21:00 - 11:00
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Mid-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
				Description:   "Energex ENGIE Everyday Shoulder Rate",
			})

		} else {
			// VIC (Citipower, Powercor, United Energy, Jemena): 3-Period Everyday Saver TOU
			// Peak: 16:00 - 21:00
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s ENGIE Everyday Peak Rate", loc),
			})

			// Saver: 11:00 - 16:00
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Super Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
				},
				DollarsPerKWH: cfg.ImportSaverDollarsPerKWH,
				Description:   fmt.Sprintf("%s ENGIE Everyday Saver Rate", loc),
			})

			// Off-Peak: 21:00 - 11:00
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s ENGIE Everyday Off-Peak Rate", loc),
			})
		}
	}

	// Solar flat feed-in credit
	// ====================================================================================
	// NOTE: Per the ENGIE Solar FiT policy (https://engie.com.au/help-centre/policies-and-commitments/solar-fit-policy),
	// we do not support the stepped upper-tier feed-in rate ($0.10/kWh in NSW and $0.08/kWh in other
	// states for the first 8 kWh exported per day) because daily cumulative exported kWh is not tracked.
	// Therefore, we statically apply the uncapped lower-tier feed-in credit ($0.03/kWh in NSW or $0.01/kWh in QLD/VIC).
	// ====================================================================================
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       allYearStart,
			End:         allYearEnd,
			LocationPtr: cfg.Timezone,
		},
		DollarsPerKWH:            cfg.ExportFlatDollarsPerKWH,
		SeparateGenerationCredit: true,
		Description:              fmt.Sprintf("%s ENGIE Solar Feed-in Tariff (Lower Tier)", loc),
	})

	return periods
}

func engiePeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	loc := opts.Location
	if loc == "" {
		loc = "ausgrid"
	}

	cfg, ok := engieRates[plan][loc]
	if !ok {
		loc = "ausgrid"
		cfg = engieRates[plan]["ausgrid"]
	}
	legacyCfg, ok := engieLegacyRates[plan][loc]
	if !ok {
		legacyCfg = engieLegacyRates[plan]["ausgrid"]
	}

	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		switch {
		case year < 2026:
			periods = append(periods, engieLegacyYearPeriods(plan, loc, legacyCfg, year)...)
		case year == 2026:
			cutoff := engie2026Cutoff(loc, cfg.Timezone)
			periods = append(periods, clipEngiePeriods(engieLegacyYearPeriods(plan, loc, legacyCfg, year), allYearStart, cutoff)...)
			periods = append(periods, clipEngiePeriods(engieCurrentYearPeriods(plan, loc, cfg, year), cutoff, allYearEnd)...)
		default:
			periods = append(periods, engieCurrentYearPeriods(plan, loc, cfg, year)...)
		}
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
					return engiePeriods("engie_solar_elec_single", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "engie_solar_elec_tou",
				Name:    "Solar Time of Use",
				Options: engieOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return engiePeriods("engie_solar_elec_tou", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
