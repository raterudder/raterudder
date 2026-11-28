package utility

// Tariff Audit Metadata
// - Provider(s): consumers_energy (Consumers Energy)
// - Official Sources:
//   - https://www.consumersenergy.com/-/media/CE/Documents/rates/electric-rate-book.pdf (M.P.S.C. No. 14 - Electric: Sheets C-60.00–C-64.85 Rules C11.2 Legacy Net Metering, C11.3 Distributed Generation Program, C11.4 Simple Renewable Energy Credit Program, & C14 Holidays; Sheets D-2.10–D-7.10 Surcharges & Sheet D-6.00 PSCR; Sheet D-9.00 Non-Transmitting Meter Provision; Sheets D-14.00–D-18.00 Rate RSP & Rate RSM; Sheets D-36.00–D-44.30 Rate RSH & Rate RPM)
//   - https://mi-psc.my.site.com/sfc/servlet.shepherd/version/download/068cs00001rd2XbAAI (MPSC Case No. U-21870 Final Order dated March 27, 2026 approving residential supply, distribution, and Rule C11.3 rates effective May 1, 2026)
//   - https://mi-psc.my.site.com/s/case/500cs00000aIRZiAAO/in-the-matter-of-the-application-of-consumers-energy-company-for-authority-to-increase-its-rates-for-the-generation-and-distribution-of-electricity-and-for-other-relief (MPSC Case No. U-21870 General Electric Rate Case Docket)
//   - https://mi-psc.my.site.com/sfc/servlet.shepherd/version/download/068cs00000oc4aUAAQ (MPSC Case No. U-21585 Corrected Attachment B pre-May 2026 residential supply and distribution)
//   - https://mi-psc.my.site.com/s/case/500cs00000bPPkHAAW/in-the-matter-of-the-application-of-consumers-energy-company-for-approval-to-implement-a-power-supply-cost-recovery-plan-for-the-twelve-months-ending-december-31-2026 (MPSC Case No. U-21873 2026 PSCR Plan Docket)
//   - https://www.consumersenergy.com/residential/account-and-billing/rates/summer-rate (Rate RSP Summer Peak Rate)
//   - https://www.consumersenergy.com/residential/account-and-billing/rates/smart-hours (Rate RSH Smart Hours)
//   - https://www.consumersenergy.com/residential/account-and-billing/rates/nighttime-savers (Rate RPM Nighttime Savers)
// - Effective Window: 2026-01-01 through 2027-12-31 ([]int{2026, 2027}; Case No. U-21585 rates 2026-01-01 to 2026-05-01 and Case No. U-21870 Final Order rates effective 2026-05-01 carried through 2027 until superseded)
// - Revision Cadence: Monthly / Periodic (Sheet D-6.00 PSCR Factor & Sheets D-2.10–D-7.10 Surcharges) & MPSC General Rate Case Orders
// - Next Audit Due: 2027-03-01 (Check 2027 PSCR Plan factor on Sheet D-6.00, annual securitization/delivery surcharge updates on Sheets D-2.10–D-7.10, and any new MPSC rate case order)
// - Last Verified: 2026-11-26
// - Modeling Notes: Models four M.P.S.C. No. 14 residential schedules in Eastern Time (etLocation): RSP Residential Summer On-Peak Basic Rate, RSH Residential Smart Hours Rate, RPM Residential Nighttime Savers Rate, and RSM Residential Service Secondary Non-Transmitting Meter Rate (Tier 1). Weekday holidays remain On-Peak on RSP, RSH, and RPM because Rule C14 holiday exclusions apply only to non-residential schedules (Sheets D-18.00, D-39.00, D-44.00). Excludes fixed monthly charges and omits the January 2027 Sheet D-4.00 FCM delta (-$0.000179/kWh, <0.1%). Credits Power Supply + PSCR on outflow under dg (Rule C11.3, default), Power Supply + PSCR + Distribution under legacy_nem (Rule C11.2), $0.077/kWh (SeparateGenerationCredit: true) under simple_renewable (Rule C11.4), and $0.00/kWh (SeparateGenerationCredit: true) with no netMeteringScheme option on ineligible Rate RSM (Sheet D-9.00).

import (
	"fmt"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Sheet D-6.00 Power Supply Cost Recovery (PSCR) factor ($/kWh).
	consumersEnergyPSCRDollarsPerKWH = 0.01456

	// Combined volumetric surcharges ($/kWh) from Sheets D-2.10 through D-7.10.
	consumersEnergySurchargesDollarsPerKWH = 0.014397

	// Residential volumetric distribution charges ($/kWh): Case U-21585 before 2026-05-01
	// and Case U-21870 Final Order effective 2026-05-01 onward.
	consumersEnergyPreMay2026DistDollarsPerKWH = 0.078955
	consumersEnergyEffMay2026DistDollarsPerKWH = 0.093333

	// Rule C11.4 Simple Renewable Energy Credit Program outflow credit ($/kWh).
	consumersEnergySimpleRenewableExportDollarsPerKWH = 0.077

	// Rate RSP Residential Summer On-Peak Basic Rate base power supply charges ($/kWh, Sheet D-14.00–D-18.00).
	consumersEnergyRSPPreMay2026OctMaySupplyDollarsPerKWH        = 0.085252
	consumersEnergyRSPEffMay2026SummerOnPeakSupplyDollarsPerKWH  = 0.151904
	consumersEnergyRSPEffMay2026SummerOffPeakSupplyDollarsPerKWH = 0.103970
	consumersEnergyRSPEffMay2026OctMaySupplyDollarsPerKWH        = 0.082996

	// Rate RSH Residential Smart Hours Rate base power supply charges ($/kWh, Sheets D-36.00–D-39.00).
	consumersEnergyRSHPreMay2026WinterOnPeakSupplyDollarsPerKWH  = 0.095326
	consumersEnergyRSHPreMay2026WinterOffPeakSupplyDollarsPerKWH = 0.082971
	consumersEnergyRSHEffMay2026SummerOnPeakSupplyDollarsPerKWH  = 0.151904
	consumersEnergyRSHEffMay2026SummerOffPeakSupplyDollarsPerKWH = 0.103970
	consumersEnergyRSHEffMay2026WinterOnPeakSupplyDollarsPerKWH  = 0.096396
	consumersEnergyRSHEffMay2026WinterOffPeakSupplyDollarsPerKWH = 0.080437

	// Rate RPM Residential Nighttime Savers Rate base power supply charges ($/kWh, Sheets D-40.00–D-44.30).
	consumersEnergyRPMPreMay2026WinterOnPeakSupplyDollarsPerKWH       = 0.095326
	consumersEnergyRPMPreMay2026WinterOffPeakSupplyDollarsPerKWH      = 0.092809
	consumersEnergyRPMPreMay2026WinterSuperOffPeakSupplyDollarsPerKWH = 0.071387
	consumersEnergyRPMEffMay2026SummerOnPeakSupplyDollarsPerKWH       = 0.151904
	consumersEnergyRPMEffMay2026SummerOffPeakSupplyDollarsPerKWH      = 0.116161
	consumersEnergyRPMEffMay2026SummerSuperOffPeakSupplyDollarsPerKWH = 0.093996
	consumersEnergyRPMEffMay2026WinterOnPeakSupplyDollarsPerKWH       = 0.096396
	consumersEnergyRPMEffMay2026WinterOffPeakSupplyDollarsPerKWH      = 0.090423
	consumersEnergyRPMEffMay2026WinterSuperOffPeakSupplyDollarsPerKWH = 0.072645

	// Rate RSM Residential Service Secondary Non-Transmitting Meter Tier 1 base power supply charges ($/kWh, Sheet D-14.00).
	consumersEnergyRSMPreMay2026SupplyDollarsPerKWH = 0.085252
	consumersEnergyRSMEffMay2026SupplyDollarsPerKWH = 0.082996
)

func clipConsumersEnergyPeriods(periods []types.UtilityFeesPeriod, windowStart, windowEnd time.Time) []types.UtilityFeesPeriod {
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

func consumersEnergyDistributionDollarsPerKWH(effMay2026 bool) float64 {
	if effMay2026 {
		return consumersEnergyEffMay2026DistDollarsPerKWH
	}
	return consumersEnergyPreMay2026DistDollarsPerKWH
}

func buildConsumersEnergyBaseYearPeriods(plan, scheme string, year int, effMay2026 bool) []types.UtilityFeesPeriod {
	baseAdderDollarsPerKWH := consumersEnergyPSCRDollarsPerKWH
	if scheme == "legacy_nem" {
		baseAdderDollarsPerKWH += consumersEnergyDistributionDollarsPerKWH(effMay2026)
	}

	peak2To7Hours := []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 19}}
	rpmOffPeakHours := []types.UtilityHourPeriod{
		{HourStart: 6, HourEnd: 14},
		{HourStart: 19, HourEnd: 23},
	}

	switch plan {
	case "consumers_energy_rsp":
		octMaySupplyDollarsPerKWH := consumersEnergyRSPPreMay2026OctMaySupplyDollarsPerKWH
		if effMay2026 {
			octMaySupplyDollarsPerKWH = consumersEnergyRSPEffMay2026OctMaySupplyDollarsPerKWH
		}
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak2To7Hours,
						Weekday:       true,
						DollarsPerKWH: consumersEnergyRSPEffMay2026SummerOnPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RSP Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: consumersEnergyRSPEffMay2026SummerOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
				OtherDescription:   "Consumers Energy Rate RSP Summer Off-Peak",
			},
			{
				Year:               year,
				MonthStart:         time.October,
				MonthEnd:           time.May,
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: octMaySupplyDollarsPerKWH + baseAdderDollarsPerKWH,
				OtherDescription:   "Consumers Energy Rate RSP Oct-May Off-Peak",
			},
		})

	case "consumers_energy_rsh":
		winterOnPeakSupplyDollarsPerKWH := consumersEnergyRSHPreMay2026WinterOnPeakSupplyDollarsPerKWH
		winterOffPeakSupplyDollarsPerKWH := consumersEnergyRSHPreMay2026WinterOffPeakSupplyDollarsPerKWH
		if effMay2026 {
			winterOnPeakSupplyDollarsPerKWH = consumersEnergyRSHEffMay2026WinterOnPeakSupplyDollarsPerKWH
			winterOffPeakSupplyDollarsPerKWH = consumersEnergyRSHEffMay2026WinterOffPeakSupplyDollarsPerKWH
		}
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak2To7Hours,
						Weekday:       true,
						DollarsPerKWH: consumersEnergyRSHEffMay2026SummerOnPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RSH Summer On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: consumersEnergyRSHEffMay2026SummerOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
				OtherDescription:   "Consumers Energy Rate RSH Summer Off-Peak",
			},
			{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak2To7Hours,
						Weekday:       true,
						DollarsPerKWH: winterOnPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RSH Oct-May On-Peak",
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: winterOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
				OtherDescription:   "Consumers Energy Rate RSH Oct-May Off-Peak",
			},
		})

	case "consumers_energy_rpm":
		winterOnPeakSupplyDollarsPerKWH := consumersEnergyRPMPreMay2026WinterOnPeakSupplyDollarsPerKWH
		winterOffPeakSupplyDollarsPerKWH := consumersEnergyRPMPreMay2026WinterOffPeakSupplyDollarsPerKWH
		winterSuperOffPeakSupplyDollarsPerKWH := consumersEnergyRPMPreMay2026WinterSuperOffPeakSupplyDollarsPerKWH
		if effMay2026 {
			winterOnPeakSupplyDollarsPerKWH = consumersEnergyRPMEffMay2026WinterOnPeakSupplyDollarsPerKWH
			winterOffPeakSupplyDollarsPerKWH = consumersEnergyRPMEffMay2026WinterOffPeakSupplyDollarsPerKWH
			winterSuperOffPeakSupplyDollarsPerKWH = consumersEnergyRPMEffMay2026WinterSuperOffPeakSupplyDollarsPerKWH
		}
		return buildPeriods(etLocation, []touSimplifiedPeriod{
			{
				Year:       year,
				MonthStart: time.June,
				MonthEnd:   time.September,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak2To7Hours,
						Weekday:       true,
						DollarsPerKWH: consumersEnergyRPMEffMay2026SummerOnPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RPM Summer On-Peak",
					},
					{
						Name:          "Off-Peak",
						Hours:         rpmOffPeakHours,
						Weekday:       true,
						DollarsPerKWH: consumersEnergyRPMEffMay2026SummerOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RPM Summer Off-Peak",
					},
				},
				OtherName:          "Super Off-Peak",
				OtherDollarsPerKWH: consumersEnergyRPMEffMay2026SummerSuperOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
				OtherDescription:   "Consumers Energy Rate RPM Summer Super Off-Peak",
			},
			{
				Year:       year,
				MonthStart: time.October,
				MonthEnd:   time.May,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         peak2To7Hours,
						Weekday:       true,
						DollarsPerKWH: winterOnPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RPM Oct-May On-Peak",
					},
					{
						Name:          "Off-Peak",
						Hours:         rpmOffPeakHours,
						Weekday:       true,
						DollarsPerKWH: winterOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
						Description:   "Consumers Energy Rate RPM Oct-May Off-Peak",
					},
				},
				OtherName:          "Super Off-Peak",
				OtherDollarsPerKWH: winterSuperOffPeakSupplyDollarsPerKWH + baseAdderDollarsPerKWH,
				OtherDescription:   "Consumers Energy Rate RPM Oct-May Super Off-Peak",
			},
		})

	case "consumers_energy_rsm":
		rsmSupplyDollarsPerKWH := consumersEnergyRSMPreMay2026SupplyDollarsPerKWH
		if effMay2026 {
			rsmSupplyDollarsPerKWH = consumersEnergyRSMEffMay2026SupplyDollarsPerKWH
		}
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					Start:       time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation),
					End:         time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation),
					LocationPtr: etLocation,
				},
				DollarsPerKWH: rsmSupplyDollarsPerKWH + consumersEnergyPSCRDollarsPerKWH,
				Description:   "Consumers Energy Rate RSM Tier 1 Energy Charge",
			},
		}
	}

	return nil
}

func buildConsumersEnergyGridAdditionalPeriod(scheme string, effMay2026 bool, windowStart, windowEnd time.Time) types.UtilityFeesPeriod {
	if scheme == "legacy_nem" {
		return types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Start:       windowStart,
				End:         windowEnd,
				LocationPtr: etLocation,
			},
			DollarsPerKWH:  consumersEnergySurchargesDollarsPerKWH,
			GridAdditional: true,
			Description:    "Consumers Energy Volumetric Surcharges",
		}
	}

	return types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Start:       windowStart,
			End:         windowEnd,
			LocationPtr: etLocation,
		},
		DollarsPerKWH:  consumersEnergyDistributionDollarsPerKWH(effMay2026) + consumersEnergySurchargesDollarsPerKWH,
		GridAdditional: true,
		Description:    "Consumers Energy Distribution & Surcharges",
	}
}

func consumersEnergyPeriods(plan string, opts types.UtilityRateOptions, years []int) ([]types.UtilityFeesPeriod, error) {
	switch plan {
	case "consumers_energy_rsp", "consumers_energy_rsh", "consumers_energy_rpm", "consumers_energy_rsm":
	default:
		return nil, fmt.Errorf("unknown Consumers Energy rate plan: %s", plan)
	}

	scheme := strings.ToLower(strings.TrimSpace(opts.NetMeteringScheme))
	if plan == "consumers_energy_rsm" {
		if scheme != "" {
			return nil, fmt.Errorf("net metering scheme %q is not supported on non-transmitting Consumers Energy Rate RSM", opts.NetMeteringScheme)
		}
	} else {
		if scheme == "" {
			scheme = "dg"
		}
		if scheme != "dg" && scheme != "legacy_nem" && scheme != "simple_renewable" {
			return nil, fmt.Errorf("unknown Consumers Energy net metering scheme: %s", opts.NetMeteringScheme)
		}
	}

	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, etLocation)
		yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, etLocation)

		switch {
		case year < 2026:
			periods = append(periods, buildConsumersEnergyBaseYearPeriods(plan, scheme, year, false)...)
			periods = append(periods, buildConsumersEnergyGridAdditionalPeriod(scheme, false, yearStart, yearEnd))
		case year == 2026:
			may1 := time.Date(2026, time.May, 1, 0, 0, 0, 0, etLocation)
			preMayBase := buildConsumersEnergyBaseYearPeriods(plan, scheme, 2026, false)
			postMayBase := buildConsumersEnergyBaseYearPeriods(plan, scheme, 2026, true)
			periods = append(periods, clipConsumersEnergyPeriods(preMayBase, yearStart, may1)...)
			periods = append(periods, clipConsumersEnergyPeriods(postMayBase, may1, yearEnd)...)
			periods = append(periods, buildConsumersEnergyGridAdditionalPeriod(scheme, false, yearStart, may1))
			periods = append(periods, buildConsumersEnergyGridAdditionalPeriod(scheme, true, may1, yearEnd))
		default:
			periods = append(periods, buildConsumersEnergyBaseYearPeriods(plan, scheme, year, true)...)
			periods = append(periods, buildConsumersEnergyGridAdditionalPeriod(scheme, true, yearStart, yearEnd))
		}

		if plan == "consumers_energy_rsm" {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: etLocation,
				},
				DollarsPerKWH:            0,
				SeparateGenerationCredit: true,
				Description:              "Consumers Energy Rate RSM Ineligible Export Credit",
			})
		} else if scheme == "simple_renewable" {
			periods = append(periods, types.UtilityFeesPeriod{
				TimePeriod: types.TimePeriod{
					Start:       yearStart,
					End:         yearEnd,
					LocationPtr: etLocation,
				},
				DollarsPerKWH:            consumersEnergySimpleRenewableExportDollarsPerKWH,
				SeparateGenerationCredit: true,
				Description:              "Consumers Energy Rule C11.4 Simple Renewable Energy Credit",
			})
		}
	}

	return periods, nil
}

func consumersEnergyUtilityInfo() types.UtilityProviderInfo {
	netMeteringOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your distributed generation, legacy net metering, or renewable energy credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "dg", Name: "Distributed Generation Program (Rule C11.3)"},
			{Value: "legacy_nem", Name: "Legacy Net Metering (Rule C11.2)"},
			{Value: "simple_renewable", Name: "Simple Renewable Energy Credit Program (Rule C11.4)"},
		},
		Default: "dg",
	}

	return types.UtilityProviderInfo{
		ID:   "consumers_energy",
		Name: "Consumers Energy",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "consumers_energy_rsp",
				Name:    "Summer Peak Rate (Rate RSP)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return consumersEnergyPeriods("consumers_energy_rsp", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "consumers_energy_rsh",
				Name:    "Smart Hours (Rate RSH)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return consumersEnergyPeriods("consumers_energy_rsh", opts, []int{2026, 2027})
				},
			},
			{
				ID:      "consumers_energy_rpm",
				Name:    "Nighttime Savers (Rate RPM)",
				Options: []types.UtilityRateOption{netMeteringOption},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return consumersEnergyPeriods("consumers_energy_rpm", opts, []int{2026, 2027})
				},
			},
			{
				ID:   "consumers_energy_rsm",
				Name: "Residential Service - Non-Transmitting Meter (Rate RSM)",
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return consumersEnergyPeriods("consumers_energy_rsm", opts, []int{2026, 2027})
				},
			},
		},
	}
}
