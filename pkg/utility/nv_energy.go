package utility

// Tariff Audit Metadata
// - Provider(s): nv_energy (NV Energy)
// - Official Sources:
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-south/StatementofRates.pdf (Nevada Power Company Tariff No. 1-B Statement of Rates, Advice No. 26-03, Effective 2026-10-01, PUCN Sheet Nos. 10 & 10H)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-south/ORS_TOU_South.pdf (NPC Schedule ORS-TOU, 14th Rev. PUCN Sheet No. 11D, 6:01 p.m. to 9:00 p.m. Daily June-September)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-south/REVRR_TOU_SOUTH.pdf (NPC Schedule REVRR-TOU, 6th Rev. PUCN Sheet No. 11O, 12:01 a.m. to 8:00 a.m. Daily)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-south/NMR-405_South.pdf (NPC Schedule NMR-405, Tranche 4 75% excess energy credit)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-south/NMR_G_South.pdf (NPC Schedule NMR-G, Legacy 1:1 Net Metering)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-north/Statement_of_Rates_Electric_North.pdf (Sierra Pacific Power Company Tariff No. Electric No. 1 Statement of Rates, Advice No. 691-E-R, Effective 2026-10-01, PUCN Sheet No. 63G)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-north/OD_1_TOU_Electric_North.pdf (SPPC Schedule OD-1-TOU, 14th Rev. PUCN Sheet No. 76A, 6:01 p.m. to 9:00 p.m. Daily June-September)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-north/OD_REVRR_TOU.pdf (SPPC Schedule OD-REVRR-TOU, 4th Rev. PUCN Sheet No. 64E(1), 12:01 a.m. to 8:00 a.m. Daily)
//   - https://www.nvenergy.com/publish/content/dam/nvenergy/brochures_arch/about-nvenergy/rates-regulatory/electric-schedules-north/NMR-2025_North.pdf (SPPC Schedule NMR-2025, 75% excess energy credit)
//   - https://www.nvenergy.com/about-nvenergy/rates-regulatory/daily-demand (NV Energy Daily Demand starting January 2027)
// - Effective Window: 2026-10-01 through until superseded (modeled across 2026-2027)
// - Revision Cadence: Quarterly (Jan/Apr/Jul/Oct 1 DEAA & BTER Statement of Rates)
// - Next Audit Due: 2027-01-05 (Q1 2027 Statement of Rates & NV Energy Daily Demand implementation)
// - Last Verified: 2026-11-21
// - Modeling Notes: NV Energy Daily Demand (https://www.nvenergy.com/about-nvenergy/rates-regulatory/daily-demand) starts in January 2027 and fixed monthly customer charges and per-kW demand charges stay excluded. Models 6:01 p.m. to 9:00 p.m. Summer On-Peak and 12:01 a.m. to 8:00 a.m. EV Recharge windows as [18:00, 21:00) and [00:00, 08:00) daily in America/Los_Angeles without holiday shifts. Supports 75% excess energy export credits under NPC Schedule NMR-405 and SPPC Schedule NMR-2025 alongside legacy Schedule NMR-G 1:1 net metering (omitting the de minimis $0.00039/kWh UEC delivery fee under the < $0.01/kWh materiality rule).

import (
	"fmt"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type nvEnergyFlatSchedule struct {
	importDollarsPerKWH float64
	creditDollarsPerKWH float64
	description         string
}

type nvEnergyTOUSchedule struct {
	summerPeakDollarsPerKWH          float64
	summerPeakCreditDollarsPerKWH    float64
	summerOffPeakDollarsPerKWH       float64
	summerOffPeakCreditDollarsPerKWH float64
	winterDollarsPerKWH              float64
	winterCreditDollarsPerKWH        float64
	summerEVDollarsPerKWH            float64
	summerEVCreditDollarsPerKWH      float64
	winterEVDollarsPerKWH            float64
	winterEVCreditDollarsPerKWH      float64
	label                            string
}

func nvEnergyPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	scheme := strings.ToLower(strings.TrimSpace(opts.NetMeteringScheme))

	isSouth := strings.HasPrefix(plan, "nv_energy_south_")
	isNorth := strings.HasPrefix(plan, "nv_energy_north_")
	if !isSouth && !isNorth {
		return nil, fmt.Errorf("unknown NV Energy rate plan: %s", plan)
	}

	if isSouth {
		if scheme == "" {
			scheme = "nmr_405"
		}
		if scheme != "nmr_405" && scheme != "net" {
			return nil, fmt.Errorf("unknown NV Energy net metering scheme: %s", opts.NetMeteringScheme)
		}
	} else {
		if scheme == "" {
			scheme = "nmr_2025"
		}
		if scheme != "nmr_2025" && scheme != "nmr_405" && scheme != "net" {
			return nil, fmt.Errorf("unknown NV Energy net metering scheme: %s", opts.NetMeteringScheme)
		}
	}

	separateCredit := scheme != "net"
	creditOrZero := func(creditDollarsPerKWH float64) float64 {
		if !separateCredit {
			return 0
		}
		return creditDollarsPerKWH
	}

	flatSchedules := map[string]nvEnergyFlatSchedule{
		"nv_energy_south_rs": {
			importDollarsPerKWH: 0.11441,
			creditDollarsPerKWH: 0.08347,
			description:         "NV Energy South RS Flat Rate",
		},
		"nv_energy_south_rm": {
			importDollarsPerKWH: 0.10507,
			creditDollarsPerKWH: 0.07680,
			description:         "NV Energy South RM Flat Rate",
		},
		"nv_energy_north_d1": {
			importDollarsPerKWH: 0.11386,
			creditDollarsPerKWH: 0.08055,
			description:         "NV Energy North D-1 Flat Rate",
		},
		"nv_energy_north_dm1": {
			importDollarsPerKWH: 0.11004,
			creditDollarsPerKWH: 0.07759,
			description:         "NV Energy North DM-1 Flat Rate",
		},
	}

	touSchedules := map[string]nvEnergyTOUSchedule{
		"nv_energy_south_ors_tou": {
			summerPeakDollarsPerKWH:          0.46974,
			summerPeakCreditDollarsPerKWH:    0.34997,
			summerOffPeakDollarsPerKWH:       0.07124,
			summerOffPeakCreditDollarsPerKWH: 0.05109,
			winterDollarsPerKWH:              0.08477,
			winterCreditDollarsPerKWH:        0.06124,
			summerEVDollarsPerKWH:            0.06408,
			summerEVCreditDollarsPerKWH:      0.04572,
			winterEVDollarsPerKWH:            0.07626,
			winterEVCreditDollarsPerKWH:      0.05486,
			label:                            "NV Energy South ORS-TOU",
		},
		"nv_energy_south_orm_tou": {
			summerPeakDollarsPerKWH:          0.36323,
			summerPeakCreditDollarsPerKWH:    0.27042,
			summerOffPeakDollarsPerKWH:       0.09569,
			summerOffPeakCreditDollarsPerKWH: 0.06976,
			winterDollarsPerKWH:              0.06850,
			winterCreditDollarsPerKWH:        0.04937,
			summerEVDollarsPerKWH:            0.08604,
			summerEVCreditDollarsPerKWH:      0.06252,
			winterEVDollarsPerKWH:            0.06157,
			winterEVCreditDollarsPerKWH:      0.04417,
			label:                            "NV Energy South ORM-TOU",
		},
		"nv_energy_north_od1_tou": {
			summerPeakDollarsPerKWH:          0.49660,
			summerPeakCreditDollarsPerKWH:    0.36761,
			summerOffPeakDollarsPerKWH:       0.08856,
			summerOffPeakCreditDollarsPerKWH: 0.06158,
			winterDollarsPerKWH:              0.08851,
			winterCreditDollarsPerKWH:        0.06154,
			summerEVDollarsPerKWH:            0.07918,
			summerEVCreditDollarsPerKWH:      0.05454,
			winterEVDollarsPerKWH:            0.07914,
			winterEVCreditDollarsPerKWH:      0.05451,
			label:                            "NV Energy North OD-1-TOU",
		},
		"nv_energy_north_odm1_tou": {
			summerPeakDollarsPerKWH:          0.33484,
			summerPeakCreditDollarsPerKWH:    0.24619,
			summerOffPeakDollarsPerKWH:       0.10579,
			summerOffPeakCreditDollarsPerKWH: 0.07440,
			winterDollarsPerKWH:              0.09197,
			winterCreditDollarsPerKWH:        0.06404,
			summerEVDollarsPerKWH:            0.09470,
			summerEVCreditDollarsPerKWH:      0.06608,
			winterEVDollarsPerKWH:            0.08227,
			winterEVCreditDollarsPerKWH:      0.05676,
			label:                            "NV Energy North ODM-1-TOU",
		},
	}

	flatSched, isFlat := flatSchedules[plan]
	touSched, isTOU := touSchedules[plan]
	if !isFlat && !isTOU {
		return nil, fmt.Errorf("unknown NV Energy rate plan: %s", plan)
	}

	peakHours := []types.UtilityHourPeriod{{HourStart: 18, HourEnd: 21}}
	evHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 8}}

	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		if isFlat {
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					SeparateGenerationCredit:           separateCredit,
					OtherDollarsPerKWH:                 flatSched.importDollarsPerKWH,
					OtherGenerationCreditDollarsPerKWH: creditOrZero(flatSched.creditDollarsPerKWH),
					OtherDescription:                   flatSched.description,
				},
			})...)
			continue
		}

		summerHoursAndDays := []touSimplifiedHoursAndDays{
			{
				Name:                          "On-Peak",
				Hours:                         peakHours,
				DollarsPerKWH:                 touSched.summerPeakDollarsPerKWH,
				GenerationCreditDollarsPerKWH: creditOrZero(touSched.summerPeakCreditDollarsPerKWH),
				Description:                   touSched.label + " Summer On-Peak",
			},
		}
		var winterHoursAndDays []touSimplifiedHoursAndDays

		if opts.EVCredit {
			summerHoursAndDays = append(summerHoursAndDays, touSimplifiedHoursAndDays{
				Name:                          "EV Recharge",
				Hours:                         evHours,
				DollarsPerKWH:                 touSched.summerEVDollarsPerKWH,
				GenerationCreditDollarsPerKWH: creditOrZero(touSched.summerEVCreditDollarsPerKWH),
				Description:                   touSched.label + " Summer EV Recharge",
			})
			winterHoursAndDays = append(winterHoursAndDays, touSimplifiedHoursAndDays{
				Name:                          "EV Recharge",
				Hours:                         evHours,
				DollarsPerKWH:                 touSched.winterEVDollarsPerKWH,
				GenerationCreditDollarsPerKWH: creditOrZero(touSched.winterEVCreditDollarsPerKWH),
				Description:                   touSched.label + " Winter EV Recharge",
			})
		}

		periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{
			{
				Year:                               year,
				MonthStart:                         time.June,
				MonthEnd:                           time.September,
				SeparateGenerationCredit:           separateCredit,
				HoursAndDays:                       summerHoursAndDays,
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 touSched.summerOffPeakDollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: creditOrZero(touSched.summerOffPeakCreditDollarsPerKWH),
				OtherDescription:                   touSched.label + " Summer Off-Peak",
			},
			{
				Year:                               year,
				MonthStart:                         time.October,
				MonthEnd:                           time.May,
				SeparateGenerationCredit:           separateCredit,
				HoursAndDays:                       winterHoursAndDays,
				OtherName:                          "Off-Peak",
				OtherDollarsPerKWH:                 touSched.winterDollarsPerKWH,
				OtherGenerationCreditDollarsPerKWH: creditOrZero(touSched.winterCreditDollarsPerKWH),
				OtherDescription:                   touSched.label + " Winter Off-Peak",
			},
		})...)
	}

	return periods, nil
}

func nvEnergyUtilityInfo() types.UtilityProviderInfo {
	southNetMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar net metering or export credit schedule.",
		Choices: []types.UtilityOptionChoice{
			{Value: "nmr_405", Name: "Net Metering Rider-405 (Schedule NMR-405, 75% credit)"},
			{Value: "net", Name: "Legacy Net Metering (Schedule NMR-G, 1:1 credit)"},
		},
		Default: "nmr_405",
	}

	northNetMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar net metering or export credit schedule.",
		Choices: []types.UtilityOptionChoice{
			{Value: "nmr_2025", Name: "Net Metering Rider-2025 (Schedule NMR-2025, 75% credit)"},
			{Value: "nmr_405", Name: "Net Metering Rider-405 (Schedule NMR-405, 75% credit)"},
			{Value: "net", Name: "Legacy Net Metering (Schedule NMR-G, 1:1 credit)"},
		},
		Default: "nmr_2025",
	}

	evCreditOption := types.UtilityRateOption{
		Field:       "evCredit",
		Name:        "Electric Vehicle Recharge Rider (REVRR-TOU / OD-REVRR-TOU)",
		Type:        types.UtilityOptionTypeSwitch,
		Description: "Are you enrolled in the Electric Vehicle Recharge Rider (12:01 a.m. to 8:00 a.m. daily EV rate)?",
		Default:     false,
	}

	years := []int{2026, 2027}

	return types.UtilityProviderInfo{
		ID:   "nv_energy",
		Name: "NV Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "nv_energy_south_rs",
				Name:    "Southern Nevada Single-Family Residential (Schedule RS)",
				Options: []types.UtilityRateOption{southNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_south_rs", opts, years)
				},
			},
			{
				ID:      "nv_energy_south_rm",
				Name:    "Southern Nevada Multi-Family Residential (Schedule RM)",
				Options: []types.UtilityRateOption{southNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_south_rm", opts, years)
				},
			},
			{
				ID:      "nv_energy_south_ors_tou",
				Name:    "Southern Nevada Single-Family Time-of-Use (Schedule ORS-TOU)",
				Options: []types.UtilityRateOption{evCreditOption, southNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_south_ors_tou", opts, years)
				},
			},
			{
				ID:      "nv_energy_south_orm_tou",
				Name:    "Southern Nevada Multi-Family Time-of-Use (Schedule ORM-TOU)",
				Options: []types.UtilityRateOption{evCreditOption, southNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_south_orm_tou", opts, years)
				},
			},
			{
				ID:      "nv_energy_north_d1",
				Name:    "Northern Nevada Single-Family Residential (Schedule D-1)",
				Options: []types.UtilityRateOption{northNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_north_d1", opts, years)
				},
			},
			{
				ID:      "nv_energy_north_dm1",
				Name:    "Northern Nevada Multi-Family Residential (Schedule DM-1)",
				Options: []types.UtilityRateOption{northNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_north_dm1", opts, years)
				},
			},
			{
				ID:      "nv_energy_north_od1_tou",
				Name:    "Northern Nevada Single-Family Time-of-Use (Schedule OD-1-TOU)",
				Options: []types.UtilityRateOption{evCreditOption, northNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_north_od1_tou", opts, years)
				},
			},
			{
				ID:      "nv_energy_north_odm1_tou",
				Name:    "Northern Nevada Multi-Family Time-of-Use (Schedule ODM-1-TOU)",
				Options: []types.UtilityRateOption{evCreditOption, northNetMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return nvEnergyPeriods("nv_energy_north_odm1_tou", opts, years)
				},
			},
		},
	}
}
