package utility

// Tariff Audit Metadata
// - Provider(s): globird (GloBird Energy)
// - Official Sources:
//   - https://www.globirdenergy.com.au/offers-rates/all-tariffs/all-tariffs-resident/
//   - AER Energy Made Easy CDR FOUR4FREE (eff. 2026-11-01): https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1215750MRE1%40EME (Ausgrid), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1215752MRE1%40EME (Endeavour), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1215756MRE1%40EME (Essential Energy), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1215754MRE1%40EME (Energex), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1215758MRE1%40EME (SA Power Networks)
//   - Victorian Energy Fact Sheets FOUR4FREE (eff. 2026-11-01): https://www.globirdenergy.com.au/shared-assets/Energy_Fact_Sheets/Victorian_Energy_Fact_Sheet_GLO812620MR_Electricity_CZ_6.pdf (CitiPower), https://www.globirdenergy.com.au/shared-assets/Energy_Fact_Sheets/Victorian_Energy_Fact_Sheet_GLO812636MR_Electricity_CZ_6.pdf (Powercor), https://www.globirdenergy.com.au/shared-assets/Energy_Fact_Sheets/Victorian_Energy_Fact_Sheet_GLO812612MR_Electricity_CZ_6.pdf (AusNet), https://www.globirdenergy.com.au/shared-assets/Energy_Fact_Sheets/Victorian_Energy_Fact_Sheet_GLO812644MR_Electricity_CZ_6.pdf (United Energy), https://www.globirdenergy.com.au/shared-assets/Energy_Fact_Sheets/Victorian_Energy_Fact_Sheet_GLO812632MR_Electricity_CZ_6.pdf (Jemena)
//   - AER Energy Made Easy CDR SOLARPLUS (eff. 2026-11-12): https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1218561MRE1%40EME (Ausgrid), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1218569MRE1%40EME (Endeavour), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1218570MRE1%40EME (Essential Energy), https://cdr.energymadeeasy.gov.au/globird/cds-au/v1/energy/plans/GLO1218566MRE1%40EME (Energex)
// - Effective Window: 2026-01-01 through until superseded (FOUR4FREE revised 2026-11-01; SOLARPLUS revised 2026-11-12; modeled through 2027-12-31)
// - Revision Cadence: Annual (Jul 1 AER DMO / ESC VDO Market Offer Cycle) & Periodic Market Offer Updates
// - Next Audit Due: 2027-07-01 (Check FY2027-28 AER DMO / ESC VDO market offer updates effective July 1, 2027)
// - Last Verified: 2026-11-20
// - Modeling Notes: All rates are GST-inclusive in AUD $/kWh; daily fixed supply charges excluded. Preserves pre-November 2026 FOUR4FREE (10:00-14:00 free window, Tier 1 <=50 kWh/day, legacy CitiPower fallbacks for United Energy & Jemena) and SOLARPLUS rates, transitioning at local midnight on 2026-11-01 (FOUR4FREE: 11:00-15:00 free Tier 1 first 30 kWh/day, 16:00-23:00 peak import & export, $0.0000/kWh off-peak export) and 2026-11-12 (SOLARPLUS: Ausgrid Tier 1 first 15 kWh/day flat import; uncapped Tier 2 >8 kWh/day flat solar export credit).

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

var adlLocation = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Adelaide")
	if err != nil {
		panic(fmt.Errorf("failed to load Adelaide time location: %w", err))
	}
	return loc
}()

type globirdRateConfig struct {
	Timezone                    *time.Location
	ImportPeakDollarsPerKWH     float64
	ImportOffPeakDollarsPerKWH  float64
	ImportShoulderDollarsPerKWH float64
	ExportPeakDollarsPerKWH     float64
	ExportOffPeakDollarsPerKWH  float64
	ExportFlatDollarsPerKWH     float64
}

// globirdLegacyRates preserves pre-November 2026 historical configurations
// (2026-01-01 to 2026-11-01 for FOUR4FREE; 2026-01-01 to 2026-11-12 for SOLARPLUS).
// Historically, United Energy and Jemena fell back to CitiPower rates prior to 2026-11-01.
var globirdLegacyRates = map[string]map[string]globirdRateConfig{
	"globird_four4free": {
		"ausgrid": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.5995,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3751,
			ExportPeakDollarsPerKWH:     0.0800,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"endeavour": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.5962,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3861,
			ExportPeakDollarsPerKWH:     0.0800,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"energex": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.5555,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3410,
			ExportPeakDollarsPerKWH:     0.0800,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"citipower": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.4400,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2640,
			ExportPeakDollarsPerKWH:     0.0300,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"powercor": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.4950,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3080,
			ExportPeakDollarsPerKWH:     0.0300,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"ausnet": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.5335,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2915,
			ExportPeakDollarsPerKWH:     0.0300,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"united_energy": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.4400,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2640,
			ExportPeakDollarsPerKWH:     0.0300,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"jemena": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.4400,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2640,
			ExportPeakDollarsPerKWH:     0.0300,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
	},
	"globird_solarplus": {
		"ausgrid": {
			Timezone:                sydLocation,
			ImportPeakDollarsPerKWH: 0.4939,
			ExportFlatDollarsPerKWH: 0.0200,
		},
		"endeavour": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.6083,
			ImportOffPeakDollarsPerKWH: 0.4246,
			ExportFlatDollarsPerKWH:    0.0200,
		},
		"energex": {
			Timezone:                   bneLocation,
			ImportPeakDollarsPerKWH:    0.5610,
			ImportOffPeakDollarsPerKWH: 0.3674,
			ExportFlatDollarsPerKWH:    0.0200,
		},
	},
}

// globirdRates maps plan ID -> location -> November 2026+ GST-inclusive configuration
// (effective 2026-11-01 for FOUR4FREE and 2026-11-12 for SOLARPLUS; modeled from 2026-01-01
// for newly added distributors essential and sapn).
var globirdRates = map[string]map[string]globirdRateConfig{
	"globird_four4free": {
		"ausgrid": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.4620,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3190,
			ExportPeakDollarsPerKWH:     0.1000,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"endeavour": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.4510,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3190,
			ExportPeakDollarsPerKWH:     0.1000,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"essential": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.4510,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3080,
			ExportPeakDollarsPerKWH:     0.1000,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"energex": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.4400,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2750,
			ExportPeakDollarsPerKWH:     0.0800,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"sapn": {
			Timezone:                    adlLocation,
			ImportPeakDollarsPerKWH:     0.5995,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.3036,
			ExportPeakDollarsPerKWH:     0.0800,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"citipower": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.3663,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2200,
			ExportPeakDollarsPerKWH:     0.0500,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"powercor": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.3850,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2310,
			ExportPeakDollarsPerKWH:     0.0500,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"ausnet": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.4400,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2288,
			ExportPeakDollarsPerKWH:     0.0500,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"united_energy": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.3795,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2233,
			ExportPeakDollarsPerKWH:     0.0500,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
		"jemena": {
			Timezone:                    melLocation,
			ImportPeakDollarsPerKWH:     0.3575,
			ImportOffPeakDollarsPerKWH:  0.0000,
			ImportShoulderDollarsPerKWH: 0.2200,
			ExportPeakDollarsPerKWH:     0.0500,
			ExportOffPeakDollarsPerKWH:  0.0000,
		},
	},
	"globird_solarplus": {
		"ausgrid": {
			Timezone:                sydLocation,
			ImportPeakDollarsPerKWH: 0.3388,
			ExportFlatDollarsPerKWH: 0.0300,
		},
		"endeavour": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.4015,
			ImportOffPeakDollarsPerKWH: 0.3146,
			ExportFlatDollarsPerKWH:    0.0300,
		},
		"essential": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.4180,
			ImportShoulderDollarsPerKWH: 0.3300,
			ImportOffPeakDollarsPerKWH:  0.2970,
			ExportFlatDollarsPerKWH:     0.0300,
		},
		"energex": {
			Timezone:                   bneLocation,
			ImportPeakDollarsPerKWH:    0.3597,
			ImportOffPeakDollarsPerKWH: 0.2475,
			ExportFlatDollarsPerKWH:    0.0200,
		},
	},
}

func appendGlobirdFour4FreeWindow(periods []types.UtilityFeesPeriod, cfg globirdRateConfig, start, end time.Time, freeHourStart, freeHourEnd int) []types.UtilityFeesPeriod {
	// Peak (4 PM to 11 PM) everyday
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "On-Peak",
			Start:       start,
			End:         end,
			LocationPtr: cfg.Timezone,
			Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 23}},
		},
		DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
		Description:   "GloBird FOUR4FREE Everyday Peak Rate",
	})

	// Super Off-Peak (free electricity window: 10:00-14:00 pre-Nov 2026 up to 50 kWh/day;
	// 11:00-15:00 eff. 2026-11-01 Tier 1 first 30 kWh/day) everyday.
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Super Off-Peak",
			Start:       start,
			End:         end,
			LocationPtr: cfg.Timezone,
			Hours:       []types.UtilityHourPeriod{{HourStart: freeHourStart, HourEnd: freeHourEnd}},
		},
		DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
		Description:   "GloBird FOUR4FREE Everyday Free Off-Peak Rate",
	})

	// Shoulder (all other times) everyday
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Shoulder",
			Start:       start,
			End:         end,
			LocationPtr: cfg.Timezone,
			Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: freeHourStart}, {HourStart: freeHourEnd, HourEnd: 16}, {HourStart: 23, HourEnd: 24}},
		},
		DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
		Description:   "GloBird FOUR4FREE Everyday Shoulder Rate",
	})

	// Export Peak (4 PM to 11 PM) everyday
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       start,
			End:         end,
			LocationPtr: cfg.Timezone,
			Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 23}},
		},
		DollarsPerKWH:            cfg.ExportPeakDollarsPerKWH,
		SeparateGenerationCredit: true,
		Description:              "GloBird FOUR4FREE Everyday Solar Export Peak Tariff",
	})

	// Export Off-Peak (all other times) everyday
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       start,
			End:         end,
			LocationPtr: cfg.Timezone,
			Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 23, HourEnd: 24}},
		},
		DollarsPerKWH:            cfg.ExportOffPeakDollarsPerKWH,
		SeparateGenerationCredit: true,
		Description:              "GloBird FOUR4FREE Everyday Solar Export Off-Peak Tariff",
	})

	return periods
}

func globirdFour4FreePeriods(loc string, cfg globirdRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	legacyCfg, hasLegacy := globirdLegacyRates["globird_four4free"][loc]

	for _, year := range years {
		yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		if year < 2026 && hasLegacy {
			periods = appendGlobirdFour4FreeWindow(periods, legacyCfg, yearStart, yearEnd, 10, 14)
		} else if year == 2026 && hasLegacy {
			novStart := time.Date(2026, time.November, 1, 0, 0, 0, 0, cfg.Timezone)
			periods = appendGlobirdFour4FreeWindow(periods, legacyCfg, yearStart, novStart, 10, 14)
			periods = appendGlobirdFour4FreeWindow(periods, cfg, novStart, yearEnd, 11, 15)
		} else {
			periods = appendGlobirdFour4FreeWindow(periods, cfg, yearStart, yearEnd, 11, 15)
		}
	}

	return periods
}

func appendGlobirdSolarPlusWindow(periods []types.UtilityFeesPeriod, loc string, cfg globirdRateConfig, start, end time.Time) []types.UtilityFeesPeriod {
	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	weekends := []time.Weekday{time.Saturday, time.Sunday}

	switch loc {
	case "ausgrid":
		// GloBird SOLARPLUS on Ausgrid uses a tiered import rate; model Tier 1 (first 15 kWh/day) as flat.
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       start,
				End:         end,
				LocationPtr: cfg.Timezone,
			},
			DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Flat Rate",
		})
	case "endeavour":
		// Weekday Peak (1 PM to 8 PM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "On-Peak",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekdays,
				Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 20}},
			},
			DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekday Peak Rate",
		})

		// Weekday Off-Peak (other times)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekdays,
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 13}, {HourStart: 20, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekday Off-Peak Rate",
		})

		// Weekend Off-Peak (All Day)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekends,
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekend Off-Peak Rate",
		})
	case "essential":
		// Weekday Peak (5 PM to 8 PM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "On-Peak",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekdays,
				Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 20}},
			},
			DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekday Peak Rate",
		})

		// Weekday Shoulder (7 AM to 5 PM & 8 PM to 10 PM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Shoulder",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekdays,
				Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 17}, {HourStart: 20, HourEnd: 22}},
			},
			DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekday Shoulder Rate",
		})

		// Weekday Off-Peak (10 PM to 7 AM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekdays,
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekday Off-Peak Rate",
		})

		// Weekend Off-Peak (All Day)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         start,
				End:           end,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: weekends,
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Weekend Off-Peak Rate",
		})
	case "energex":
		// Peak Everyday (4 PM to 9 PM)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "On-Peak",
				Start:       start,
				End:         end,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			},
			DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Everyday Peak Rate",
		})

		// Off-Peak Everyday (all other times)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				Start:       start,
				End:         end,
				LocationPtr: cfg.Timezone,
				Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   "GloBird SOLARPLUS Everyday Off-Peak Rate",
		})
	}

	// Solar flat export credit (uncapped Tier 2 >8 kWh/day feed-in rate)
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       start,
			End:         end,
			LocationPtr: cfg.Timezone,
		},
		DollarsPerKWH:            cfg.ExportFlatDollarsPerKWH,
		SeparateGenerationCredit: true,
		Description:              "GloBird SOLARPLUS Solar Export Tariff",
	})

	return periods
}

func globirdSolarPlusPeriods(loc string, cfg globirdRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	legacyCfg, hasLegacy := globirdLegacyRates["globird_solarplus"][loc]

	for _, year := range years {
		yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		if year < 2026 && hasLegacy {
			periods = appendGlobirdSolarPlusWindow(periods, loc, legacyCfg, yearStart, yearEnd)
		} else if year == 2026 && hasLegacy {
			novStart := time.Date(2026, time.November, 12, 0, 0, 0, 0, cfg.Timezone)
			periods = appendGlobirdSolarPlusWindow(periods, loc, legacyCfg, yearStart, novStart)
			periods = appendGlobirdSolarPlusWindow(periods, loc, cfg, novStart, yearEnd)
		} else {
			periods = appendGlobirdSolarPlusWindow(periods, loc, cfg, yearStart, yearEnd)
		}
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
		return globirdFour4FreePeriods(loc, cfg, years), nil
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
				{Value: "essential", Name: "Regional NSW (Essential Energy)"},
				{Value: "ausnet", Name: "Victoria East & North (AusNet)"},
				{Value: "jemena", Name: "Melbourne Northern & Western Suburbs"},
				{Value: "sapn", Name: "Adelaide & South Australia (SA Power Networks)"},
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
				{Value: "essential", Name: "Regional NSW (Essential Energy)"},
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
					return globirdPeriods("globird_four4free", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "globird_solarplus",
				Name:    "SOLARPLUS",
				Options: solarPlusOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return globirdPeriods("globird_solarplus", opts, []int{2026, 2027})
				},
			},
		},
	}
}
