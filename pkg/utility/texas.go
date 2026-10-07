package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type texasTDUConfig struct {
	ID                    string
	Name                  string
	DeliveryDollarsPerKWH float64
	LoadZone              string
}

// texasTDUs defines the PUCT-regulated volumetric delivery charges per kWh for the 5 ERCOT TDUs.
//
// Validity Period:
// These rates are effective as of September 1, 2026 and are valid through February 28, 2027.
// In ERCOT, residential TDU delivery charges are regulated by the Public Utility Commission of Texas (PUCT)
// and typically update twice a year—taking effect on March 1 and September 1. When updating these values,
// check for new filings under the semi-annual TCOS/DCRF/EECRF adjustments.
//
// Where to find updated values:
//  1. Official PUCT Power to Choose portal: https://www.powertochoose.org
//     (View the Electricity Facts Label (EFL) of any current plan for the TDU territory; current delivery rates are itemized).
//  2. Public Utility Commission of Texas (PUCT): https://www.puc.texas.gov
//  3. Individual TDU Electric Tariff Books:
//     - CenterPoint Energy: https://www.centerpointenergy.com/en-us/Services/Pages/Tariffs-Electric.aspx
//     - Oncor Electric:      https://www.oncor.com/en-us/Pages/Tariffs.aspx
//     - AEP Texas:           https://www.aeptexas.com/company/about/rates/
//     - TNMP:                https://www.tnmp.com/customers/rates-and-tariffs
var texasTDUs = map[string]texasTDUConfig{
	"centerpoint": {
		ID:                    "centerpoint",
		Name:                  "CenterPoint Energy",
		DeliveryDollarsPerKWH: 0.06533,
		LoadZone:              "LZ_HOUSTON",
	},
	"oncor": {
		ID:                    "oncor",
		Name:                  "Oncor Electric Delivery",
		DeliveryDollarsPerKWH: 0.06867,
		LoadZone:              "LZ_NORTH",
	},
	"aep_central": {
		ID:                    "aep_central",
		Name:                  "AEP Texas Central",
		DeliveryDollarsPerKWH: 0.05690,
		LoadZone:              "LZ_SOUTH",
	},
	"aep_north": {
		ID:                    "aep_north",
		Name:                  "AEP Texas North",
		DeliveryDollarsPerKWH: 0.05575,
		LoadZone:              "LZ_WEST",
	},
	"tnmp": {
		ID:                    "tnmp",
		Name:                  "Texas-New Mexico Power (TNMP)",
		DeliveryDollarsPerKWH: 0.07771,
		LoadZone:              "LZ_NORTH",
	},
}

func loadZoneForTDU(tdu string) (string, error) {
	if cfg, ok := texasTDUs[tdu]; ok {
		return cfg.LoadZone, nil
	}
	if tdu == "" {
		return "", fmt.Errorf("tdu location is required to determine ercot load zone")
	}
	return "", fmt.Errorf("unknown TDU delivery utility: %q", tdu)
}

func texasTDUOptions() []types.UtilityRateOption {
	return []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "TDU / Delivery Utility",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your Transmission and Distribution Utility (wires company).",
			Choices: []types.UtilityOptionChoice{
				{Value: "centerpoint", Name: "CenterPoint Energy (Houston Area)"},
				{Value: "oncor", Name: "Oncor Electric Delivery (DFW & North/Central TX)"},
				{Value: "aep_central", Name: "AEP Texas Central (Corpus Christi / Valley)"},
				{Value: "aep_north", Name: "AEP Texas North (Abilene / West TX)"},
				{Value: "tnmp", Name: "Texas-New Mexico Power (TNMP)"},
			},
			Default: "centerpoint",
		},
	}
}

func getTexasTDU(opts types.UtilityRateOptions) texasTDUConfig {
	loc := opts.Location
	if loc == "" {
		loc = "centerpoint"
	}
	if tdu, ok := texasTDUs[loc]; ok {
		return tdu
	}
	return texasTDUs["centerpoint"]
}

func texasPeriods(rep, plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	tdu := getTexasTDU(opts)
	var periods []types.UtilityFeesPeriod

	for _, year := range years {
		yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, ctLocation)
		yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, ctLocation)

		switch plan {
		case "tesla_dynamic":
			// On-Peak (6:00 PM - 9:00 PM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "On-Peak",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
				},
				DollarsPerKWH: 0.1800,
				Description:   "Tesla Dynamic On-Peak Energy Rate",
			})
			// Off-Peak (other hours)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH: 0.1200,
				Description:   "Tesla Dynamic Off-Peak Energy Rate",
			})
			// Regulated TDU Delivery Charge applies to all imports
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge", tdu.Name),
			})
			// Export credit: when live wholesale ERCOT feed is active, scales wholesale SPP by 0.90 (Tesla Dynamic 90% sellback).
			// When live feed is not active, falls back to static DollarsPerKWH values.
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}},
				},
				DollarsPerKWH:                            0.2500,
				SeparateGenerationCredit:                 true,
				GenerationCreditDollarsPerKWHPreMultiple: 0.90,
				Description:                              "Tesla Dynamic Peak Export Credit",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 18}, {HourStart: 21, HourEnd: 24}},
				},
				DollarsPerKWH:                            0.0800,
				SeparateGenerationCredit:                 true,
				GenerationCreditDollarsPerKWHPreMultiple: 0.90,
				Description:                              "Tesla Dynamic Off-Peak Export Credit",
			})

		case "tesla_fixed":
			// Flat energy rate
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Standard",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH: 0.1150,
				Description:   "Tesla Fixed Energy Rate",
			})
			// TDU Delivery
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge", tdu.Name),
			})
			// Fixed Solar Buyback Credit
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.0900,
				SeparateGenerationCredit: true,
				Description:              "Tesla Fixed Solar Buyback Credit",
			})

		case "green_mountain_free_nights":
			// Free Nights (8:00 PM - 6:00 AM): $0.0000 import rate
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Free Night",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 20, HourEnd: 24}, {HourStart: 0, HourEnd: 6}},
				},
				DollarsPerKWH: 0.0000,
				Description:   "Green Mountain Free Night Energy Rate",
			})
			// Daytime (6:00 AM - 8:00 PM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Day",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 20}},
				},
				DollarsPerKWH: 0.1950,
				Description:   "Green Mountain Daytime Energy Rate",
			})
			// On Green Mountain Free Nights, TDU delivery is credited 100% during free hours,
			// so TDU delivery only applies during paid daytime hours (6:00 AM - 8:00 PM).
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 20}},
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge (Daytime)", tdu.Name),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 20, HourEnd: 24}, {HourStart: 0, HourEnd: 6}},
				},
				DollarsPerKWH:  0.0000,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge (Free Night Waived)", tdu.Name),
			})
			// Solar Buyback Credit during Daytime
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 20}},
				},
				DollarsPerKWH:            0.0500,
				SeparateGenerationCredit: true,
				Description:              "Green Mountain Solar Days Export Credit",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 20, HourEnd: 24}, {HourStart: 0, HourEnd: 6}},
				},
				DollarsPerKWH:            0.0000,
				SeparateGenerationCredit: true,
				Description:              "Green Mountain Solar Export Credit (Night)",
			})

		case "green_mountain_fixed":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Standard",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH: 0.1250,
				Description:   "Green Mountain Reliable Energy Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge", tdu.Name),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:            0.0300,
				SeparateGenerationCredit: true,
				Description:              "Green Mountain Solar Export Credit",
			})

		case "direct_energy_twelve_hour":
			// Free 12-Hour Window (9:00 PM - 9:00 AM): $0.0000 import rate
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Free Window",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 21, HourEnd: 24}, {HourStart: 0, HourEnd: 9}},
				},
				DollarsPerKWH: 0.0000,
				Description:   "Direct Energy Twelve Hour Free Power",
			})
			// Daytime (9:00 AM - 9:00 PM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Day",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
				},
				DollarsPerKWH: 0.2150,
				Description:   "Direct Energy Daytime Energy Rate",
			})
			// TDU delivery is credited during free window, applies during daytime
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge (Daytime)", tdu.Name),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 21, HourEnd: 24}, {HourStart: 0, HourEnd: 9}},
				},
				DollarsPerKWH:  0.0000,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge (Free Hours Waived)", tdu.Name),
			})
			// Daytime Solar Export Credit
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 21}},
				},
				DollarsPerKWH:            0.0300,
				SeparateGenerationCredit: true,
				Description:              "Direct Energy Solar Export Credit",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 21, HourEnd: 24}, {HourStart: 0, HourEnd: 9}},
				},
				DollarsPerKWH:            0.0000,
				SeparateGenerationCredit: true,
				Description:              "Direct Energy Solar Export Credit (Night)",
			})

		case "direct_energy_live_brighter":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Standard",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH: 0.1200,
				Description:   "Direct Energy Live Brighter Energy Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge", tdu.Name),
			})

		case "txu_free_nights":
			// Free Nights (8:00 PM - 6:00 AM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Free Night",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 20, HourEnd: 24}, {HourStart: 0, HourEnd: 6}},
				},
				DollarsPerKWH: 0.0000,
				Description:   "TXU Free Night Energy Rate",
			})
			// Daytime (6:00 AM - 8:00 PM)
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Day",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 20}},
				},
				DollarsPerKWH: 0.2050,
				Description:   "TXU Daytime Energy Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 20}},
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge (Daytime)", tdu.Name),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 20, HourEnd: 24}, {HourStart: 0, HourEnd: 6}},
				},
				DollarsPerKWH:  0.0000,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge (Free Night Waived)", tdu.Name),
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 6, HourEnd: 20}},
				},
				DollarsPerKWH:            0.0400,
				SeparateGenerationCredit: true,
				Description:              "TXU Solar Export Credit",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 20, HourEnd: 24}, {HourStart: 0, HourEnd: 6}},
				},
				DollarsPerKWH:            0.0000,
				SeparateGenerationCredit: true,
				Description:              "TXU Solar Export Credit (Night)",
			})

		case "txu_season_pass":
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Name:        "Standard",
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH: 0.1300,
				Description:   "TXU Season Pass Energy Rate",
			})
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: ctLocation,
				},
				DollarsPerKWH:  tdu.DeliveryDollarsPerKWH,
				GridAdditional: true,
				Description:    fmt.Sprintf("%s TDU Delivery Charge", tdu.Name),
			})

		default:
			return nil, fmt.Errorf("unknown Texas plan %s for provider %s", plan, rep)
		}
	}

	return periods, nil
}

// teslaElectricUtilityInfo returns metadata for Tesla Electric (Texas).
func teslaElectricUtilityInfo() types.UtilityProviderInfo {
	opts := texasTDUOptions()
	years := []int{2026, 2027}

	return types.UtilityProviderInfo{
		ID:   "tesla_electric_tx",
		Name: "Tesla Electric (Texas)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "tesla_dynamic",
				Name:    "Tesla Electric Dynamic Plan",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("tesla_electric_tx", "tesla_dynamic", options, years)
				},
			},
			{
				ID:      "tesla_fixed",
				Name:    "Tesla Electric Fixed Plan",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("tesla_electric_tx", "tesla_fixed", options, years)
				},
			},
		},
	}
}

// greenMountainUtilityInfo returns metadata for Green Mountain Energy (Texas).
func greenMountainUtilityInfo() types.UtilityProviderInfo {
	opts := texasTDUOptions()
	years := []int{2026, 2027}

	return types.UtilityProviderInfo{
		ID:   "green_mountain_tx",
		Name: "Green Mountain Energy (Texas)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "green_mountain_free_nights",
				Name:    "Free Nights & Solar Days / Pollution Free All Night",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("green_mountain_tx", "green_mountain_free_nights", options, years)
				},
			},
			{
				ID:      "green_mountain_fixed",
				Name:    "Pollution Free Reliable Fixed",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("green_mountain_tx", "green_mountain_fixed", options, years)
				},
			},
		},
	}
}

// directEnergyUtilityInfo returns metadata for Direct Energy (Texas).
func directEnergyUtilityInfo() types.UtilityProviderInfo {
	opts := texasTDUOptions()
	years := []int{2026, 2027}

	return types.UtilityProviderInfo{
		ID:   "direct_energy_tx",
		Name: "Direct Energy (Texas)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "direct_energy_twelve_hour",
				Name:    "Twelve Hour Power (9 PM - 9 AM Free)",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("direct_energy_tx", "direct_energy_twelve_hour", options, years)
				},
			},
			{
				ID:      "direct_energy_live_brighter",
				Name:    "Live Brighter Fixed",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("direct_energy_tx", "direct_energy_live_brighter", options, years)
				},
			},
		},
	}
}

// txuUtilityInfo returns metadata for TXU Energy (Texas).
func txuUtilityInfo() types.UtilityProviderInfo {
	opts := texasTDUOptions()
	years := []int{2026, 2027}

	return types.UtilityProviderInfo{
		ID:   "txu_tx",
		Name: "TXU Energy (Texas)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "txu_free_nights",
				Name:    "Free Nights & Solar Days (8 PM - 6 AM Free)",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("txu_tx", "txu_free_nights", options, years)
				},
			},
			{
				ID:      "txu_season_pass",
				Name:    "Season Pass Fixed",
				Options: opts,
				GetFees: func(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return texasPeriods("txu_tx", "txu_season_pass", options, years)
				},
			},
		},
	}
}
