package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Tariff Audit Metadata
// - Provider(s): origin (Origin Energy)
// - Official Sources:
//   - https://www.originenergy.com.au/solar/battery-plans/ (Origin Battery Earner & Origin Battery Charge overview)
//   - https://www.originenergy.com.au/solar/feed-in-tariff-rates/ (Origin solar & battery feed-in tariff rates)
//   - https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1134409MRE2%40EME & https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1135258MRE2%40EME (QLD Energex Origin Go Variable & Origin Solar Boost, eff. 2026-10-01)
//   - https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1140595MRE2%40EME & https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1140621MRE2%40EME (NSW Ausgrid Origin Go Variable & Origin Solar Boost, eff. 2026-10-01)
//   - https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1140752MRE2%40EME & https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1140776MRE2%40EME (NSW Endeavour Origin Go Variable & Origin Solar Boost, eff. 2026-10-01)
//   - https://www.originenergy.com.au/content/dam/pricing/2026/01Oct26/OR2800702MR.pdf, https://www.originenergy.com.au/content/dam/pricing/2026/01Oct26/OR2800631MR.pdf, & https://www.originenergy.com.au/content/dam/pricing/2026/01Oct26/OR2800796MR.pdf (VIC Origin Go Variable, Origin Solar Boost, & Battery Starter fact sheets, eff. 2026-10-01)
//   - https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1206303MRE1%40EME, https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1206296MRE1%40EME, https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1206297MRE1%40EME, & https://www.originenergy.com.au/content/dam/pricing/2026/26Oct26/OR2803040MR.pdf (Origin Battery Earner QLD/NSW/VIC, eff. Oct 2026)
//   - https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1202131MRE2%40EME, https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1202123MRE2%40EME, https://cdr.energymadeeasy.gov.au/origin/cds-au/v1/energy/plans/ORI1202127MRE2%40EME, https://www.originenergy.com.au/content/dam/pricing/2026/26Oct26/OR2803127MR.pdf, https://www.originenergy.com.au/content/dam/pricing/2026/26Oct26/OR2803133MR.pdf, & https://www.originenergy.com.au/content/dam/pricing/2026/26Oct26/OR2803131MR.pdf (Origin Battery Charge QLD/NSW/VIC, eff. Oct 2026)
// - Effective Window: 2026-10-01 through until superseded (pre-2026-10-01 historical periods preserved; QLD/NSW Battery Starter transitions 2026-10-27)
// - Revision Cadence: Annual (Jul 1 AER DMO / ESC VDO Market Offer Cycle + Periodic Retail Repricings)
// - Next Audit Due: 2027-07-01 (Check FY 2027-28 AER DMO / ESC VDO July 1 market offer updates across QLD, NSW, and VIC distributors)
// - Last Verified: 2026-11-21
// - Modeling Notes: All volumetric rates are stored in GST-inclusive AUD/kWh; fixed daily supply charges ($/day) are excluded from volumetric periods; Tier 1 assumptions apply where plans include daily cumulative kWh caps that cannot be tracked across independent hourly UtilityFeesPeriod evaluations (uncapped standard feed-in tariff modeled on Origin Solar Boost and legacy Origin Go Solar Variable instead of initial daily kWh export boost tiers, Tier 1 peak export modeled on Origin Battery Earner/Starter, and Tier 1 11:00-15:00 $0.00/kWh free import and 18:00-21:00 $0.11/kWh peak export windows modeled on Origin Battery Charge); legacy rate IDs (origin_battery_maximiser, origin_battery_starter, origin_go_solar_variable) remain registered for existing site compatibility.

type originRateConfig struct {
	Timezone                      *time.Location
	ImportPeakDollarsPerKWH       float64
	ImportWinterPeakDollarsPerKWH float64
	ImportOffPeakDollarsPerKWH    float64
	ImportShoulderDollarsPerKWH   float64
	ImportDaytimeDollarsPerKWH    float64
	ImportOvernightDollarsPerKWH  float64
	ExportPeakDollarsPerKWH       float64
	ExportOffPeakDollarsPerKWH    float64
	ExportFlatDollarsPerKWH       float64

	PeakHours     []types.UtilityHourPeriod
	OffPeakHours  []types.UtilityHourPeriod
	ShoulderHours []types.UtilityHourPeriod
	DaytimeHours  []types.UtilityHourPeriod

	WeekdayPeakHours     []types.UtilityHourPeriod
	WeekdayShoulderHours []types.UtilityHourPeriod
	WeekendShoulderHours []types.UtilityHourPeriod
	AllWeekOffPeakHours  []types.UtilityHourPeriod
}

const (
	originBatteryChargePeakExportDollarsPerKWH = 0.1100
)

// originLegacyRates preserves pre-October 2026 historical rates and retired offer structures.
var originLegacyRates = map[string]map[string]originRateConfig{
	"energex": {
		"origin_battery_maximiser": {
			Timezone:                   bneLocation,
			ImportPeakDollarsPerKWH:    0.3410,
			ImportOffPeakDollarsPerKWH: 0.1870,
			ExportPeakDollarsPerKWH:    0.2200,
			ExportOffPeakDollarsPerKWH: 0.0700,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:                   bneLocation,
			ImportPeakDollarsPerKWH:    0.4290,
			ImportOffPeakDollarsPerKWH: 0.3152,
			ExportPeakDollarsPerKWH:    0.1800,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.4580,
			ImportShoulderDollarsPerKWH: 0.3302,
			ImportOffPeakDollarsPerKWH:  0.2780,
			ExportFlatDollarsPerKWH:     0.0300,
			WeekdayPeakHours:            []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}},
			WeekdayShoulderHours:        []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 16}, {HourStart: 20, HourEnd: 22}},
			WeekendShoulderHours:        []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}},
			AllWeekOffPeakHours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.4486,
			ImportShoulderDollarsPerKWH: 0.3234,
			ImportOffPeakDollarsPerKWH:  0.2723,
			ExportFlatDollarsPerKWH:     0.0300,
			WeekdayPeakHours:            []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}},
			WeekdayShoulderHours:        []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 16}, {HourStart: 20, HourEnd: 22}},
			WeekendShoulderHours:        []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}},
			AllWeekOffPeakHours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.4998,
			ImportOffPeakDollarsPerKWH:  0.2826,
			ImportShoulderDollarsPerKWH: 0.3240,
			ExportFlatDollarsPerKWH:     0.0300,
			PeakHours:                   []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:                []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			ShoulderHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"citipower": {
		"origin_battery_maximiser": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.2640,
			ImportOffPeakDollarsPerKWH: 0.1870,
			ExportPeakDollarsPerKWH:    0.2200,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.4015,
			ImportOffPeakDollarsPerKWH: 0.2322,
			ExportPeakDollarsPerKWH:    0.1800,
			ExportOffPeakDollarsPerKWH: 0.0300,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3561,
			ImportOffPeakDollarsPerKWH: 0.2162,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3269,
			ImportOffPeakDollarsPerKWH: 0.1986,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3633,
			ImportOffPeakDollarsPerKWH: 0.2206,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"united_energy": {
		"origin_battery_maximiser": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.2970,
			ImportOffPeakDollarsPerKWH: 0.1870,
			ExportPeakDollarsPerKWH:    0.2200,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.4202,
			ImportOffPeakDollarsPerKWH: 0.2435,
			ExportPeakDollarsPerKWH:    0.1800,
			ExportOffPeakDollarsPerKWH: 0.0300,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3760,
			ImportOffPeakDollarsPerKWH: 0.2253,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3453,
			ImportOffPeakDollarsPerKWH: 0.2069,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3837,
			ImportOffPeakDollarsPerKWH: 0.2299,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"powercor": {
		"origin_battery_maximiser": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.2970,
			ImportOffPeakDollarsPerKWH: 0.1870,
			ExportPeakDollarsPerKWH:    0.2200,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.4268,
			ImportOffPeakDollarsPerKWH: 0.2621,
			ExportPeakDollarsPerKWH:    0.1800,
			ExportOffPeakDollarsPerKWH: 0.0300,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3959,
			ImportOffPeakDollarsPerKWH: 0.2318,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3636,
			ImportOffPeakDollarsPerKWH: 0.2129,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.4040,
			ImportOffPeakDollarsPerKWH: 0.2365,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}},
		},
	},
	"ausgrid": {
		"origin_battery_maximiser": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.5390,
			ImportOffPeakDollarsPerKWH: 0.1870,
			ExportPeakDollarsPerKWH:    0.2200,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.5731,
			ImportOffPeakDollarsPerKWH: 0.3300,
			ExportPeakDollarsPerKWH:    0.1800,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.6944,
			ImportShoulderDollarsPerKWH: 0.3633,
			ImportOffPeakDollarsPerKWH:  0.2119,
			ExportFlatDollarsPerKWH:     0.0300,
		},
		"origin_go_variable": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.6340,
			ImportShoulderDollarsPerKWH: 0.3318,
			ImportOffPeakDollarsPerKWH:  0.1935,
			ExportFlatDollarsPerKWH:     0.0300,
		},
		"origin_solar_boost": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.7548,
			ImportShoulderDollarsPerKWH: 0.3949,
			ImportOffPeakDollarsPerKWH:  0.2303,
			ExportFlatDollarsPerKWH:     0.0300,
		},
	},
	"endeavour": {
		"origin_battery_maximiser": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.4400,
			ImportOffPeakDollarsPerKWH: 0.2090,
			ExportPeakDollarsPerKWH:    0.2200,
			ExportOffPeakDollarsPerKWH: 0.0600,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_starter": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.5588,
			ImportOffPeakDollarsPerKWH: 0.2750,
			ExportPeakDollarsPerKWH:    0.1800,
			ExportOffPeakDollarsPerKWH: 0.0500,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_go_solar_variable": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.5214,
			ImportShoulderDollarsPerKWH: 0.4179,
			ImportOffPeakDollarsPerKWH:  0.2770,
			ExportFlatDollarsPerKWH:     0.0300,
		},
		"origin_go_variable": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.4874,
			ImportShoulderDollarsPerKWH: 0.3906,
			ImportOffPeakDollarsPerKWH:  0.2589,
			ExportFlatDollarsPerKWH:     0.0300,
		},
		"origin_solar_boost": {
			Timezone:                    sydLocation,
			ImportPeakDollarsPerKWH:     0.5667,
			ImportShoulderDollarsPerKWH: 0.4542,
			ImportOffPeakDollarsPerKWH:  0.3011,
			ExportFlatDollarsPerKWH:     0.0300,
		},
	},
}

// originCurrentRates stores active October 2026+ rates for Origin Go Variable, Origin Solar Boost,
// Origin Battery Earner (also used for post-transition Origin Battery Starter), and Origin Battery Charge.
var originCurrentRates = map[string]map[string]originRateConfig{
	"energex": {
		"origin_go_variable": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.4587,
			ImportOffPeakDollarsPerKWH:  0.0671,
			ImportShoulderDollarsPerKWH: 0.24288,
			ExportFlatDollarsPerKWH:     0.0300,
			PeakHours:                   []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:                []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			ShoulderHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                    bneLocation,
			ImportPeakDollarsPerKWH:     0.477851,
			ImportOffPeakDollarsPerKWH:  0.069839,
			ImportShoulderDollarsPerKWH: 0.252956,
			ExportFlatDollarsPerKWH:     0.0300,
			PeakHours:                   []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:                []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			ShoulderHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_earner": {
			Timezone:                   bneLocation,
			ImportPeakDollarsPerKWH:    0.3289,
			ImportOffPeakDollarsPerKWH: 0.2849,
			ExportPeakDollarsPerKWH:    0.2800,
			ExportOffPeakDollarsPerKWH: 0.0300,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_charge": {
			Timezone:                     bneLocation,
			ImportPeakDollarsPerKWH:      0.43605,
			ImportShoulderDollarsPerKWH:  0.28260,
			ImportOvernightDollarsPerKWH: 0.31200,
			ExportPeakDollarsPerKWH:      originBatteryChargePeakExportDollarsPerKWH,
			ExportOffPeakDollarsPerKWH:   0.0300,
		},
	},
	"citipower": {
		"origin_go_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.35629,
			ImportDaytimeDollarsPerKWH: 0.15433,
			ImportOffPeakDollarsPerKWH: 0.19690,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			DaytimeHours:               []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.383097,
			ImportDaytimeDollarsPerKWH: 0.165891,
			ImportOffPeakDollarsPerKWH: 0.211695,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			DaytimeHours:               []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_earner": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.2992,
			ImportOffPeakDollarsPerKWH: 0.2728,
			ExportPeakDollarsPerKWH:    0.2650,
			ExportOffPeakDollarsPerKWH: 0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_charge": {
			Timezone:                     melLocation,
			ImportPeakDollarsPerKWH:      0.37335,
			ImportShoulderDollarsPerKWH:  0.24660,
			ImportOvernightDollarsPerKWH: 0.29100,
			ExportPeakDollarsPerKWH:      originBatteryChargePeakExportDollarsPerKWH,
			ExportOffPeakDollarsPerKWH:   0.0100,
		},
	},
	"united_energy": {
		"origin_go_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.37499,
			ImportDaytimeDollarsPerKWH: 0.16049,
			ImportOffPeakDollarsPerKWH: 0.20592,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			DaytimeHours:               []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.403194,
			ImportDaytimeDollarsPerKWH: 0.172590,
			ImportOffPeakDollarsPerKWH: 0.221397,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			DaytimeHours:               []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_earner": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3190,
			ImportOffPeakDollarsPerKWH: 0.2728,
			ExportPeakDollarsPerKWH:    0.2650,
			ExportOffPeakDollarsPerKWH: 0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_charge": {
			Timezone:                     melLocation,
			ImportPeakDollarsPerKWH:      0.38475,
			ImportShoulderDollarsPerKWH:  0.26640,
			ImportOvernightDollarsPerKWH: 0.31000,
			ExportPeakDollarsPerKWH:      originBatteryChargePeakExportDollarsPerKWH,
			ExportOffPeakDollarsPerKWH:   0.0100,
		},
	},
	"powercor": {
		"origin_go_variable": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.38753,
			ImportDaytimeDollarsPerKWH: 0.16049,
			ImportOffPeakDollarsPerKWH: 0.20911,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			DaytimeHours:               []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_solar_boost": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.416691,
			ImportDaytimeDollarsPerKWH: 0.172590,
			ImportOffPeakDollarsPerKWH: 0.224895,
			ExportFlatDollarsPerKWH:    0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
			DaytimeHours:               []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 16}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 11}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_earner": {
			Timezone:                   melLocation,
			ImportPeakDollarsPerKWH:    0.3663,
			ImportOffPeakDollarsPerKWH: 0.2750,
			ExportPeakDollarsPerKWH:    0.2650,
			ExportOffPeakDollarsPerKWH: 0.0100,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_charge": {
			Timezone:                     melLocation,
			ImportPeakDollarsPerKWH:      0.39045,
			ImportShoulderDollarsPerKWH:  0.27900,
			ImportOvernightDollarsPerKWH: 0.32200,
			ExportPeakDollarsPerKWH:      originBatteryChargePeakExportDollarsPerKWH,
			ExportOffPeakDollarsPerKWH:   0.0100,
		},
	},
	"ausgrid": {
		"origin_go_variable": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.5412,
			ImportOffPeakDollarsPerKWH: 0.21637,
			ExportFlatDollarsPerKWH:    0.0300,
		},
		"origin_solar_boost": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.601359,
			ImportOffPeakDollarsPerKWH: 0.240383,
			ExportFlatDollarsPerKWH:    0.0300,
		},
		"origin_battery_earner": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.5401,
			ImportOffPeakDollarsPerKWH: 0.2981,
			ExportPeakDollarsPerKWH:    0.2900,
			ExportOffPeakDollarsPerKWH: 0.0300,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_charge": {
			Timezone:                     sydLocation,
			ImportPeakDollarsPerKWH:      0.49400,
			ImportShoulderDollarsPerKWH:  0.33210,
			ImportOvernightDollarsPerKWH: 0.36300,
			ExportPeakDollarsPerKWH:      originBatteryChargePeakExportDollarsPerKWH,
			ExportOffPeakDollarsPerKWH:   0.0300,
		},
	},
	"endeavour": {
		"origin_go_variable": {
			Timezone:                      sydLocation,
			ImportPeakDollarsPerKWH:       0.42845,
			ImportWinterPeakDollarsPerKWH: 0.43362,
			ImportDaytimeDollarsPerKWH:    0.11363,
			ImportOffPeakDollarsPerKWH:    0.32835,
			ExportFlatDollarsPerKWH:       0.0300,
		},
		"origin_solar_boost": {
			Timezone:                      sydLocation,
			ImportPeakDollarsPerKWH:       0.465718,
			ImportWinterPeakDollarsPerKWH: 0.471372,
			ImportDaytimeDollarsPerKWH:    0.123475,
			ImportOffPeakDollarsPerKWH:    0.356906,
			ExportFlatDollarsPerKWH:       0.0300,
		},
		"origin_battery_earner": {
			Timezone:                   sydLocation,
			ImportPeakDollarsPerKWH:    0.5544,
			ImportOffPeakDollarsPerKWH: 0.3036,
			ExportPeakDollarsPerKWH:    0.2900,
			ExportOffPeakDollarsPerKWH: 0.0300,
			PeakHours:                  []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
			OffPeakHours:               []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 17}, {HourStart: 21, HourEnd: 24}},
		},
		"origin_battery_charge": {
			Timezone:                     sydLocation,
			ImportPeakDollarsPerKWH:      0.52250,
			ImportShoulderDollarsPerKWH:  0.33930,
			ImportOvernightDollarsPerKWH: 0.36900,
			ExportPeakDollarsPerKWH:      originBatteryChargePeakExportDollarsPerKWH,
			ExportOffPeakDollarsPerKWH:   0.0300,
		},
	},
}

func planToName(plan string) string {
	switch plan {
	case "origin_battery_earner":
		return "Origin Battery Earner"
	case "origin_battery_charge":
		return "Origin Battery Charge"
	case "origin_battery_maximiser":
		return "Origin Battery Maximiser (Legacy)"
	case "origin_battery_starter":
		return "Origin Battery Starter (Legacy)"
	case "origin_go_solar_variable":
		return "Origin Go Solar Variable (Legacy)"
	case "origin_go_variable":
		return "Origin Go Variable"
	case "origin_solar_boost":
		return "Origin Solar Boost"
	default:
		return "Origin"
	}
}

func clipOriginPeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
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

func originPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	loc := opts.Location
	if loc == "" {
		loc = "energex"
	}

	if _, ok := originCurrentRates[loc]; !ok {
		return nil, fmt.Errorf("unsupported location: %s", loc)
	}

	switch plan {
	case "origin_battery_earner":
		cfg, ok := originCurrentRates[loc][plan]
		if !ok {
			return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
		}
		return buildOriginStandardPeriods(plan, cfg, years), nil

	case "origin_battery_charge":
		cfg, ok := originCurrentRates[loc][plan]
		if !ok {
			return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
		}
		return buildOriginBatteryChargePeriods(cfg, years), nil

	case "origin_battery_starter":
		legacyCfg, ok := originLegacyRates[loc][plan]
		if !ok {
			return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
		}
		earnerCfg := originCurrentRates[loc]["origin_battery_earner"]
		transitionDay := 27
		if legacyCfg.Timezone == melLocation {
			transitionDay = 1
		}
		transitionTS := time.Date(2026, time.October, transitionDay, 0, 0, 0, 0, legacyCfg.Timezone)

		legacyPeriods := clipOriginPeriods(buildOriginStandardPeriods(plan, legacyCfg, years), time.Time{}, transitionTS)
		updatedPeriods := clipOriginPeriods(buildOriginStandardPeriods(plan, earnerCfg, years), transitionTS, time.Time{})
		return append(legacyPeriods, updatedPeriods...), nil

	case "origin_go_variable", "origin_solar_boost":
		legacyCfg, ok := originLegacyRates[loc][plan]
		if !ok {
			return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
		}
		currentCfg := originCurrentRates[loc][plan]
		transitionTS := time.Date(2026, time.October, 1, 0, 0, 0, 0, legacyCfg.Timezone)

		legacyPeriods := clipOriginPeriods(buildOriginLegacyPlanPeriods(plan, loc, legacyCfg, years), time.Time{}, transitionTS)
		updatedPeriods := clipOriginPeriods(buildOriginCurrentPlanPeriods(plan, loc, currentCfg, years), transitionTS, time.Time{})
		return append(legacyPeriods, updatedPeriods...), nil

	case "origin_battery_maximiser", "origin_go_solar_variable":
		legacyCfg, ok := originLegacyRates[loc][plan]
		if !ok {
			return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
		}
		return buildOriginLegacyPlanPeriods(plan, loc, legacyCfg, years), nil

	default:
		return nil, fmt.Errorf("plan %s is not supported for location %s", plan, loc)
	}
}

func buildOriginLegacyPlanPeriods(plan, loc string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	if loc == "ausgrid" && (plan == "origin_go_solar_variable" || plan == "origin_go_variable" || plan == "origin_solar_boost") {
		return ausgridLegacySeasonalPeriods(plan, cfg, years)
	}
	if loc == "endeavour" && (plan == "origin_go_solar_variable" || plan == "origin_go_variable" || plan == "origin_solar_boost") {
		return endeavourLegacyWeekdayWeekendPeriods(plan, cfg, years)
	}
	return buildOriginStandardPeriods(plan, cfg, years)
}

func buildOriginCurrentPlanPeriods(plan, loc string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	switch loc {
	case "ausgrid":
		return ausgridOct2026SeasonalPeriods(plan, cfg, years)
	case "endeavour":
		return endeavourOct2026Periods(plan, cfg, years)
	default:
		return buildOriginStandardPeriods(plan, cfg, years)
	}
}

func buildOriginStandardPeriods(plan string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		if len(cfg.WeekdayPeakHours) > 0 {
			// 3-Period Weekday/Weekend TOU (pre-Oct 2026 Energex Go and Go Solar Variable)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "On-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
					Hours:         cfg.WeekdayPeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
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
				DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
				Description:   fmt.Sprintf("%s Weekday Shoulder Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Shoulder",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   cfg.Timezone,
					DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
					Hours:         cfg.WeekendShoulderHours,
				},
				DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
				Description:   fmt.Sprintf("%s Weekend Shoulder Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.AllWeekOffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})
			periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, cfg.Timezone, cfg.ExportFlatDollarsPerKWH))
		} else if len(cfg.DaytimeHours) > 0 {
			// 3-Period Everyday Peak / Daytime / Off-Peak TOU (Victorian Oct 2026+ Go Variable & Solar Boost)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.PeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Peak Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Daytime",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.DaytimeHours,
				},
				DollarsPerKWH: cfg.ImportDaytimeDollarsPerKWH,
				Description:   fmt.Sprintf("%s Daytime Rate", planToName(plan)),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.OffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})
			periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, cfg.Timezone, cfg.ExportFlatDollarsPerKWH))
		} else if len(cfg.ShoulderHours) > 0 {
			// 3-Period Everyday Peak / Off-Peak / Shoulder TOU (Energex Solar Boost & Oct 2026+ Energex Go Variable)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.PeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
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
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
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
				DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
				Description:   fmt.Sprintf("%s Shoulder Rate", planToName(plan)),
			})
			periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, cfg.Timezone, cfg.ExportFlatDollarsPerKWH))
		} else {
			// 2-Period Everyday TOU (Battery Earner / Starter / Maximiser & pre-Oct 2026 VIC residential plans)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       cfg.PeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
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
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})

			if cfg.ExportFlatDollarsPerKWH > 0 {
				periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, cfg.Timezone, cfg.ExportFlatDollarsPerKWH))
			} else {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Start:       allYearStart,
						End:         allYearEnd,
						LocationPtr: cfg.Timezone,
						Hours:       cfg.PeakHours,
					},
					DollarsPerKWH:            cfg.ExportPeakDollarsPerKWH,
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
					DollarsPerKWH:            cfg.ExportOffPeakDollarsPerKWH,
					SeparateGenerationCredit: true,
					Description:              fmt.Sprintf("%s Solar Export Off-Peak Tariff", planToName(plan)),
				})
			}
		}
	}

	return periods
}

func buildOriginFlatExportPeriod(plan string, start, end time.Time, tz *time.Location, exportFlatDollarsPerKWH float64) types.UtilityFeesPeriod {
	// For plans with daily cumulative kWh feed-in tiers (e.g. Origin Solar Boost and legacy Origin Go Solar Variable),
	// UtilityFeesPeriod evaluates hourly intervals independently and models the uncapped standard feed-in rate.
	return types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       start,
			End:         end,
			LocationPtr: tz,
		},
		DollarsPerKWH:            exportFlatDollarsPerKWH,
		SeparateGenerationCredit: true,
		Description:              fmt.Sprintf("%s Solar Export Tariff", planToName(plan)),
	}
}

func buildOriginBatteryChargePeriods(cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	peakImportHours := []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 9}, {HourStart: 16, HourEnd: 22}}
	shoulderImportHours := []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 11}, {HourStart: 15, HourEnd: 16}}
	freeImportHours := []types.UtilityHourPeriod{{HourStart: 11, HourEnd: 15}}
	overnightImportHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}, {HourStart: 22, HourEnd: 24}}

	peakExportHours := []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}}
	offPeakExportHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}, {HourStart: 21, HourEnd: 24}}

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		periods = append(periods,
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       peakImportHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   "Origin Battery Charge Peak Rate",
			},
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Shoulder",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       shoulderImportHours,
				},
				DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
				Description:   "Origin Battery Charge Shoulder Rate",
			},
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Free",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       freeImportHours,
				},
				DollarsPerKWH: 0.0000,
				Description:   "Origin Battery Charge Free Usage Window",
			},
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Overnight",
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       overnightImportHours,
				},
				DollarsPerKWH: cfg.ImportOvernightDollarsPerKWH,
				Description:   "Origin Battery Charge Overnight Rate",
			},
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       peakExportHours,
				},
				DollarsPerKWH:            cfg.ExportPeakDollarsPerKWH,
				SeparateGenerationCredit: true,
				Description:              "Origin Battery Charge Peak Export Tariff",
			},
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       allYearStart,
					End:         allYearEnd,
					LocationPtr: cfg.Timezone,
					Hours:       offPeakExportHours,
				},
				DollarsPerKWH:            cfg.ExportOffPeakDollarsPerKWH,
				SeparateGenerationCredit: true,
				Description:              "Origin Battery Charge Off-Peak Export Tariff",
			},
		)
	}

	return periods
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
				ID:      "origin_go_variable",
				Name:    "Origin Go Variable",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_go_variable", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "origin_solar_boost",
				Name:    "Origin Solar Boost",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_solar_boost", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "origin_battery_earner",
				Name:    "Origin Battery Earner",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_battery_earner", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "origin_battery_charge",
				Name:    "Origin Battery Charge",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_battery_charge", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "origin_battery_maximiser",
				Name:    "Origin Battery Maximiser (Legacy)",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_battery_maximiser", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "origin_battery_starter",
				Name:    "Origin Battery Starter (Legacy)",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_battery_starter", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "origin_go_solar_variable",
				Name:    "Origin Go Solar Variable (Legacy)",
				Options: originOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return originPeriods("origin_go_solar_variable", opts, []int{2026, 2027})
				},
			},
		},
	}
}

func ausgridOct2026SeasonalPeriods(plan string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	peakHours := []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 21}}
	offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 15}, {HourStart: 21, HourEnd: 24}}

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation)
		aprStart := time.Date(year, time.April, 1, 0, 0, 0, 0, sydLocation)
		junStart := time.Date(year, time.June, 1, 0, 0, 0, 0, sydLocation)
		sepStart := time.Date(year, time.September, 1, 0, 0, 0, 0, sydLocation)
		novStart := time.Date(year, time.November, 1, 0, 0, 0, 0, sydLocation)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation)

		peakSeasons := []struct {
			start time.Time
			end   time.Time
		}{
			{start: allYearStart, end: aprStart}, // Jan 1 - Mar 31
			{start: junStart, end: sepStart},     // Jun 1 - Aug 31
			{start: novStart, end: allYearEnd},   // Nov 1 - Dec 31
		}
		for _, s := range peakSeasons {
			periods = append(periods,
				types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "On-Peak",
						Start:       s.start,
						End:         s.end,
						LocationPtr: sydLocation,
						Hours:       peakHours,
					},
					DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
					Description:   fmt.Sprintf("%s Peak Rate", planToName(plan)),
				},
				types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Off-Peak",
						Start:       s.start,
						End:         s.end,
						LocationPtr: sydLocation,
						Hours:       offPeakHours,
					},
					DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
					Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
				},
			)
		}

		nonPeakSeasons := []struct {
			start time.Time
			end   time.Time
		}{
			{start: aprStart, end: junStart}, // Apr 1 - May 31
			{start: sepStart, end: novStart}, // Sep 1 - Oct 31
		}
		for _, s := range nonPeakSeasons {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       s.start,
					End:         s.end,
					LocationPtr: sydLocation,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Non-Peak Months Off-Peak Rate", planToName(plan)),
			})
		}

		periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, sydLocation, cfg.ExportFlatDollarsPerKWH))
	}

	return periods
}

func endeavourOct2026Periods(plan string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	weekends := []time.Weekday{time.Saturday, time.Sunday}
	weekdayPeakHours := []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 20}}
	solarSoakHours := []types.UtilityHourPeriod{{HourStart: 10, HourEnd: 14}}
	weekdayOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 14, HourEnd: 16}, {HourStart: 20, HourEnd: 24}}
	weekendOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 10}, {HourStart: 14, HourEnd: 24}}

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation)
		aprStart := time.Date(year, time.April, 1, 0, 0, 0, 0, sydLocation)
		novStart := time.Date(year, time.November, 1, 0, 0, 0, 0, sydLocation)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation)

		// Nov - Mar Summer Weekday Peak
		for _, s := range []struct{ start, end time.Time }{
			{allYearStart, aprStart},
			{novStart, allYearEnd},
		} {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "On-Peak",
					Start:         s.start,
					End:           s.end,
					LocationPtr:   sydLocation,
					DaysOfTheWeek: weekdays,
					Hours:         weekdayPeakHours,
				},
				DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Summer Weekday Peak Rate", planToName(plan)),
			})
		}

		// Apr - Oct Non-Summer Weekday Peak
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "On-Peak",
				Start:         aprStart,
				End:           novStart,
				LocationPtr:   sydLocation,
				DaysOfTheWeek: weekdays,
				Hours:         weekdayPeakHours,
			},
			DollarsPerKWH: cfg.ImportWinterPeakDollarsPerKWH,
			Description:   fmt.Sprintf("%s Non-Summer Weekday Peak Rate", planToName(plan)),
		})

		// Daily Solar Soak (10:00 - 14:00)
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Solar Soak",
				Start:       allYearStart,
				End:         allYearEnd,
				LocationPtr: sydLocation,
				Hours:       solarSoakHours,
			},
			DollarsPerKWH: cfg.ImportDaytimeDollarsPerKWH,
			Description:   fmt.Sprintf("%s Solar Soak Rate", planToName(plan)),
		})

		// Weekday & Weekend Off-Peak
		periods = append(periods,
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   sydLocation,
					DaysOfTheWeek: weekdays,
					Hours:         weekdayOffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Weekday Off-Peak Rate", planToName(plan)),
			},
			types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:          "Off-Peak",
					Start:         allYearStart,
					End:           allYearEnd,
					LocationPtr:   sydLocation,
					DaysOfTheWeek: weekends,
					Hours:         weekendOffPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Weekend Off-Peak Rate", planToName(plan)),
			},
		)

		periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, sydLocation, cfg.ExportFlatDollarsPerKWH))
	}

	return periods
}

func ausgridLegacySeasonalPeriods(plan string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		offPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}}

		summerPeakHours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 20}}
		summerShoulderHoursWeekday := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 14}, {HourStart: 20, HourEnd: 22}}
		summerShoulderHoursWeekend := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}}

		winterPeakHours := []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}}
		winterShoulderHoursWeekday := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 17}, {HourStart: 21, HourEnd: 22}}
		winterShoulderHoursWeekend := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}}

		shoulderHoursAllWeek := []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 22}}

		seasons := []struct {
			start      time.Time
			end        time.Time
			isSummer   bool
			isWinter   bool
			isShoulder bool
		}{
			{start: time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.March, 31, 0, 0, 0, 0, sydLocation), isSummer: true},
			{start: time.Date(year, time.March, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.May, 31, 0, 0, 0, 0, sydLocation), isShoulder: true},
			{start: time.Date(year, time.May, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.August, 31, 0, 0, 0, 0, sydLocation), isWinter: true},
			{start: time.Date(year, time.August, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year, time.October, 31, 0, 0, 0, 0, sydLocation), isShoulder: true},
			{start: time.Date(year, time.October, 31, 0, 0, 0, 0, sydLocation), end: time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation), isSummer: true},
		}

		for _, s := range seasons {
			if s.isSummer {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "On-Peak",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         summerPeakHours,
					},
					DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
					Description:   fmt.Sprintf("%s Summer Weekday Peak Rate", planToName(plan)),
				})
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         summerShoulderHoursWeekday,
					},
					DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
					Description:   fmt.Sprintf("%s Summer Weekday Shoulder Rate", planToName(plan)),
				})
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
						Hours:         summerShoulderHoursWeekend,
					},
					DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
					Description:   fmt.Sprintf("%s Summer Weekend Shoulder Rate", planToName(plan)),
				})
			} else if s.isWinter {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "On-Peak",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         winterPeakHours,
					},
					DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
					Description:   fmt.Sprintf("%s Winter Weekday Peak Rate", planToName(plan)),
				})
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
						Hours:         winterShoulderHoursWeekday,
					},
					DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
					Description:   fmt.Sprintf("%s Winter Weekday Shoulder Rate", planToName(plan)),
				})
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:          "Shoulder",
						Start:         s.start,
						End:           s.end,
						LocationPtr:   sydLocation,
						DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
						Hours:         winterShoulderHoursWeekend,
					},
					DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
					Description:   fmt.Sprintf("%s Winter Weekend Shoulder Rate", planToName(plan)),
				})
			} else if s.isShoulder {
				periods = append(periods, types.UtilityFeesPeriod{
					TimePeriod: types.TimePeriod{
						Name:        "Shoulder",
						Start:       s.start,
						End:         s.end,
						LocationPtr: sydLocation,
						Hours:       shoulderHoursAllWeek,
					},
					DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
					Description:   fmt.Sprintf("%s Shoulder Months Rate", planToName(plan)),
				})
			}

			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       s.start,
					End:         s.end,
					LocationPtr: sydLocation,
					Hours:       offPeakHours,
				},
				DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
				Description:   fmt.Sprintf("%s Off-Peak Rate", planToName(plan)),
			})
		}

		periods = append(periods, buildOriginFlatExportPeriod(
			plan,
			time.Date(year, time.January, 1, 0, 0, 0, 0, sydLocation),
			time.Date(year+1, time.January, 1, 0, 0, 0, 0, sydLocation),
			sydLocation,
			cfg.ExportFlatDollarsPerKWH,
		))
	}

	return periods
}

func endeavourLegacyWeekdayWeekendPeriods(plan string, cfg originRateConfig, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		allYearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, cfg.Timezone)
		allYearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, cfg.Timezone)

		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "On-Peak",
				Start:         allYearStart,
				End:           allYearEnd,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
				Hours:         []types.UtilityHourPeriod{{HourStart: 13, HourEnd: 20}},
			},
			DollarsPerKWH: cfg.ImportPeakDollarsPerKWH,
			Description:   fmt.Sprintf("%s Weekday Peak Rate", planToName(plan)),
		})
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Shoulder",
				Start:         allYearStart,
				End:           allYearEnd,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
				Hours:         []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 13}, {HourStart: 20, HourEnd: 22}},
			},
			DollarsPerKWH: cfg.ImportShoulderDollarsPerKWH,
			Description:   fmt.Sprintf("%s Weekday Shoulder Rate", planToName(plan)),
		})
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         allYearStart,
				End:           allYearEnd,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 7}, {HourStart: 22, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   fmt.Sprintf("%s Weekday Off-Peak Rate", planToName(plan)),
		})
		periods = append(periods, types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:          "Off-Peak",
				Start:         allYearStart,
				End:           allYearEnd,
				LocationPtr:   cfg.Timezone,
				DaysOfTheWeek: []time.Weekday{time.Saturday, time.Sunday},
				Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
			},
			DollarsPerKWH: cfg.ImportOffPeakDollarsPerKWH,
			Description:   fmt.Sprintf("%s Weekend Off-Peak Rate", planToName(plan)),
		})
		periods = append(periods, buildOriginFlatExportPeriod(plan, allYearStart, allYearEnd, cfg.Timezone, cfg.ExportFlatDollarsPerKWH))
	}

	return periods
}
