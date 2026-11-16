package utility

import (
	"github.com/raterudder/raterudder/pkg/types"
)

// Austin Energy City of Austin Electric Tariff (FY 2026-2027, effective October 1, 2026)
// Official sources:
//   - https://austinenergy.com/en/rates/approved-rates-schedules
//   - City of Austin Electric Tariff (October 1, 2026), Residential Service (pp. 6-8),
//     Power Supply Adjustment (p. 31), Administrative Adjustment to the PSA ($0.00/kWh, p. 33),
//     Community Benefit Charge (p. 34), Regulatory Charge (p. 36), Value-of-Solar Rider (p. 41),
//     and Pilot Programs Residential Time-of-Use Rate (pp. 45-47).
//
// Value-of-Solar / Export Credit Note:
// Austin Energy uses Dual-Metered Buy-All / Sell-All billing (`BuyAllSellAll: true`).
// Under the Value-of-Solar (VoS) Rider ($0.1289/kWh effective Oct 1, 2026), 100% of solar PV
// production is metered separately and credited at $0.1289/kWh (`GenerationCreditDollarsPerKWH`),
// while all on-site consumption (including solar consumed by the home or battery) is billed at
// the residential retail rate. Batteries are excluded from VoS credits, so battery exports to
// the grid earn $0.00/kWh (`SeparateBatteryGenerationCredit: true`).
const (
	// Power Supply Adjustment (Secondary Voltage, effective October 1, 2026)
	austinEnergyPSADollarsPerKWH = 0.03912

	// Pilot Residential Time-of-Use Power Supply Charges (effective October 1, 2026)
	austinEnergyTOUOffPeakPSADollarsPerKWH = 0.02543
	austinEnergyTOUMidPeakPSADollarsPerKWH = 0.03912
	austinEnergyTOUOnPeakPSADollarsPerKWH  = 0.08020

	// Community Benefit Charges (Secondary Voltage Residential, effective October 1, 2026)
	austinEnergyCBCCAPDollarsPerKWH                       = 0.00415
	austinEnergyCBCServiceAreaLightingInsideDollarsPerKWH = 0.00272
	austinEnergyCBCEnergyEfficiencyDollarsPerKWH          = 0.00410
	austinEnergyCBCInsideCityDollarsPerKWH                = austinEnergyCBCCAPDollarsPerKWH + austinEnergyCBCServiceAreaLightingInsideDollarsPerKWH + austinEnergyCBCEnergyEfficiencyDollarsPerKWH
	austinEnergyCBCOutsideCityDollarsPerKWH               = austinEnergyCBCCAPDollarsPerKWH + austinEnergyCBCEnergyEfficiencyDollarsPerKWH

	// Regulatory Charge (Secondary Voltage Energy, effective October 1, 2026)
	austinEnergyRegulatoryDollarsPerKWH = 0.01799

	// Non-Demand Value-of-Solar (VoS) Rider (effective October 1, 2026: $0.0959 Avoided Cost + $0.0330 Societal Benefit)
	austinEnergyVoSDollarsPerKWH = 0.1289
)

func austinEnergyNonPSARate(options types.UtilityRateOptions) float64 {
	outsideCity := options.Location == "outside_city"

	var baseEnergyDollarsPerKWH float64
	if outsideCity {
		switch options.RateClass {
		case "tier2":
			baseEnergyDollarsPerKWH = 0.04897
		case "tier3":
			baseEnergyDollarsPerKWH = 0.06369
		case "tier4":
			baseEnergyDollarsPerKWH = 0.08334
		default: // tier1 (0 - 300 kWh)
			baseEnergyDollarsPerKWH = 0.04872
		}
		return baseEnergyDollarsPerKWH + austinEnergyCBCOutsideCityDollarsPerKWH + austinEnergyRegulatoryDollarsPerKWH
	}

	switch options.RateClass {
	case "tier2":
		baseEnergyDollarsPerKWH = 0.05395
	case "tier3":
		baseEnergyDollarsPerKWH = 0.07901
	case "tier4":
		baseEnergyDollarsPerKWH = 0.11428
	default: // tier1 (0 - 300 kWh)
		baseEnergyDollarsPerKWH = 0.04872
	}
	return baseEnergyDollarsPerKWH + austinEnergyCBCInsideCityDollarsPerKWH + austinEnergyRegulatoryDollarsPerKWH
}

func austinEnergyPeriods(plan string, options types.UtilityRateOptions) []types.UtilityFeesPeriod {
	nonPSA := austinEnergyNonPSARate(options)

	switch plan {
	case "austin_energy_tou":
		// Residential Time-of-Use Rate (Pilot Program):
		// Weekdays (Mon-Fri):
		//   Off-Peak: 10:00 PM - 7:00 AM CT ($0.02543 PSA)
		//   Mid-Peak: 7:00 AM - 3:00 PM & 6:00 PM - 10:00 PM CT ($0.03912 PSA)
		//   On-Peak:  3:00 PM - 6:00 PM CT ($0.08020 PSA)
		// Weekends (Sat-Sun):
		//   Off-Peak: All day ($0.02543 PSA)
		return buildPeriods(ctLocation, []touSimplifiedPeriod{
			{
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name: "Mid-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 7, HourEnd: 15},
							{HourStart: 18, HourEnd: 22},
						},
						Weekday:                       true,
						DollarsPerKWH:                 nonPSA + austinEnergyTOUMidPeakPSADollarsPerKWH,
						GenerationCreditDollarsPerKWH: austinEnergyVoSDollarsPerKWH,
						Description:                   "Austin Energy Residential TOU Mid-Peak",
					},
					{
						Name: "On-Peak",
						Hours: []types.UtilityHourPeriod{
							{HourStart: 15, HourEnd: 18},
						},
						Weekday:                       true,
						DollarsPerKWH:                 nonPSA + austinEnergyTOUOnPeakPSADollarsPerKWH,
						GenerationCreditDollarsPerKWH: austinEnergyVoSDollarsPerKWH,
						Description:                   "Austin Energy Residential TOU On-Peak",
					},
				},
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 nonPSA + austinEnergyTOUOffPeakPSADollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: austinEnergyVoSDollarsPerKWH,
				OtherDescription:                   "Austin Energy Residential TOU Off-Peak",
				SeparateGenerationCredit:           true,
				SeparateBatteryGenerationCredit:    true,
				BuyAllSellAll:                      true,
			},
		})

	default: // austin_energy_residential
		return buildPeriods(ctLocation, []touSimplifiedPeriod{
			{
				OtherName:                          "Base",
				OtherDollarsPerKWH:                 nonPSA + austinEnergyPSADollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: austinEnergyVoSDollarsPerKWH,
				OtherDescription:                   "Austin Energy Residential Service",
				SeparateGenerationCredit:           true,
				SeparateBatteryGenerationCredit:    true,
				BuyAllSellAll:                      true,
			},
		})
	}
}

func austinEnergyUtilityInfo() types.UtilityProviderInfo {
	locationOption := types.UtilityRateOption{
		Field:       "location",
		Name:        "Service Location",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Whether your home is inside or outside the City of Austin limits.",
		Choices: []types.UtilityOptionChoice{
			{Value: "inside_city", Name: "Inside City Limits"},
			{Value: "outside_city", Name: "Outside City Limits"},
		},
		Default: "inside_city",
	}

	tierOption := types.UtilityRateOption{
		Field:       "rateClass",
		Name:        "Monthly Usage Tier",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select the Austin Energy consumption tier matching your typical monthly usage.",
		Choices: []types.UtilityOptionChoice{
			{Value: "tier1", Name: "Tier 1 (0 - 300 kWh/month)"},
			{Value: "tier2", Name: "Tier 2 (301 - 900 kWh/month)"},
			{Value: "tier3", Name: "Tier 3 (901 - 2,000 kWh/month)"},
			{Value: "tier4", Name: "Tier 4 (Over 2,000 kWh/month)"},
		},
		Default: "tier1",
	}

	options := []types.UtilityRateOption{locationOption, tierOption}

	return types.UtilityProviderInfo{
		ID:   "austin_energy",
		Name: "Austin Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "austin_energy_residential",
				Name:    "Residential Service (Standard)",
				Options: options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return austinEnergyPeriods("austin_energy_residential", opts), nil
				},
			},
			{
				ID:      "austin_energy_tou",
				Name:    "Residential Time-of-Use (Pilot)",
				Options: options,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return austinEnergyPeriods("austin_energy_tou", opts), nil
				},
			},
		},
	}
}
