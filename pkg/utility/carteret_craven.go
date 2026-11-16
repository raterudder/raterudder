package utility

import (
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// Carteret-Craven Electric Cooperative (CCEC) rate schedules from official tariff
// EC55, SUB. 79 (adopted July 27, 2026; effective with bills rendered on or after
// October 1, 2026) and https://www.ccemc.com/my-account/rate-information/
const (
	// Schedule R - Residential Service
	ccecScheduleRSummerDollarsPerKWH    = 0.1103 // April - October (11.03¢/kWh)
	ccecScheduleRNonSummerDollarsPerKWH = 0.1006 // November - March (10.06¢/kWh)

	// Schedule R-TU-E - Residential Service Time-of-Use (All-Energy TOU)
	ccecScheduleRTUEOnPeakDollarsPerKWH       = 0.4000 // 40.00¢/kWh
	ccecScheduleRTUEOffPeakDollarsPerKWH      = 0.0650 // 6.50¢/kWh
	ccecScheduleRTUESuperOffPeakDollarsPerKWH = 0.0550 // 5.50¢/kWh

	// Schedule R-TU - Residential Service Time-of-Use (Frozen, + $11.84/kW On-Peak Demand)
	ccecScheduleRTUDollarsPerKWH = 0.0545 // 5.45¢/kWh all kWhs

	// Schedule R-EVTU - Residential Service Electric Vehicle Time-of-Use (Frozen, + $11.84/kW On-Peak Demand)
	ccecScheduleREVTUOnOffPeakDollarsPerKWH    = 0.0570 // 5.70¢/kWh
	ccecScheduleREVTUSuperOffPeakDollarsPerKWH = 0.0460 // 4.60¢/kWh

	// Rider RQF - Residential Qualifying Facilities Purchased Power (<= 25 kW solar, Variable Credit)
	ccecRiderRQFExportDollarsPerKWH = 0.0407 // 4.07¢/kWh
)

func isCCECBuyAllSellAll(plan string, opts types.UtilityRateOptions) bool {
	scheme := strings.ToLower(strings.TrimSpace(opts.NetMeteringScheme))
	if scheme == "" {
		return plan == "carteret_craven_r"
	}
	return scheme == "rqf"
}

func applyCCECExportScheme(s *touSimplifiedPeriod, buyAllSellAll bool) {
	s.SeparateGenerationCredit = true
	if !buyAllSellAll {
		s.OtherGenerationCreditDollarsPerKWH = 0.00
		return
	}

	s.BuyAllSellAll = true
	s.SeparateBatteryGenerationCredit = true
	s.OtherGenerationCreditDollarsPerKWH = ccecRiderRQFExportDollarsPerKWH
	s.OtherBatteryGenerationCreditDollarsPerKWH = 0.00
	for i := range s.HoursAndDays {
		s.HoursAndDays[i].GenerationCreditDollarsPerKWH = ccecRiderRQFExportDollarsPerKWH
		s.HoursAndDays[i].BatteryGenerationCreditDollarsPerKWH = 0.00
	}
}

func buildCCECTOUPeriods(
	year int,
	planDesc string,
	onPeakRate, offPeakRate float64,
	hasSuperOffPeak bool,
	superOffPeakRate float64,
	buyAllSellAll bool,
) []touSimplifiedPeriod {
	superOffPeakHours := []types.UtilityHourPeriod{
		{HourStart: 0, HourEnd: 5},
		{HourStart: 22, HourEnd: 24},
	}

	seasons := []struct {
		name       string
		monthStart time.Month
		monthEnd   time.Month
		peakHours  []types.UtilityHourPeriod
	}{
		{
			name:       "Summer",
			monthStart: time.April,
			monthEnd:   time.October,
			peakHours:  []types.UtilityHourPeriod{{HourStart: 15, HourEnd: 18}},
		},
		{
			name:       "Non-Summer",
			monthStart: time.November,
			monthEnd:   time.March,
			peakHours:  []types.UtilityHourPeriod{{HourStart: 7, HourEnd: 9}},
		},
	}

	periods := make([]touSimplifiedPeriod, 0, len(seasons))
	for _, season := range seasons {
		hoursAndDays := []touSimplifiedHoursAndDays{
			{
				Name:          "On-Peak",
				Hours:         season.peakHours,
				DollarsPerKWH: onPeakRate,
				Description:   planDesc + " " + season.name + " On-Peak",
			},
		}
		if hasSuperOffPeak {
			hoursAndDays = append(hoursAndDays, touSimplifiedHoursAndDays{
				Name:          "Super Off-Peak",
				Hours:         superOffPeakHours,
				DollarsPerKWH: superOffPeakRate,
				Description:   planDesc + " " + season.name + " Super Off-Peak",
			})
		}

		p := touSimplifiedPeriod{
			Year:               year,
			MonthStart:         season.monthStart,
			MonthEnd:           season.monthEnd,
			HoursAndDays:       hoursAndDays,
			OtherName:          "Off-Peak",
			OtherDollarsPerKWH: offPeakRate,
			OtherDescription:   planDesc + " " + season.name + " Off-Peak",
		}
		applyCCECExportScheme(&p, buyAllSellAll)
		periods = append(periods, p)
	}

	return periods
}

// carteretCravenPeriods generates pricing periods for Carteret-Craven Electric Cooperative.
func carteretCravenPeriods(plan string, opts types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	buyAllSellAll := isCCECBuyAllSellAll(plan, opts)

	for _, year := range years {
		var simplified []touSimplifiedPeriod
		switch plan {
		case "carteret_craven_r":
			summer := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.April,
				MonthEnd:           time.October,
				OtherName:          "Base",
				OtherDollarsPerKWH: ccecScheduleRSummerDollarsPerKWH,
				OtherDescription:   "CCEC Schedule R Summer Rate",
			}
			applyCCECExportScheme(&summer, buyAllSellAll)

			nonSummer := touSimplifiedPeriod{
				Year:               year,
				MonthStart:         time.November,
				MonthEnd:           time.March,
				OtherName:          "Base",
				OtherDollarsPerKWH: ccecScheduleRNonSummerDollarsPerKWH,
				OtherDescription:   "CCEC Schedule R Non-Summer Rate",
			}
			applyCCECExportScheme(&nonSummer, buyAllSellAll)

			simplified = []touSimplifiedPeriod{summer, nonSummer}

		case "carteret_craven_r_tu_e":
			simplified = buildCCECTOUPeriods(
				year,
				"CCEC Schedule R-TU-E",
				ccecScheduleRTUEOnPeakDollarsPerKWH,
				ccecScheduleRTUEOffPeakDollarsPerKWH,
				true,
				ccecScheduleRTUESuperOffPeakDollarsPerKWH,
				buyAllSellAll,
			)

		case "carteret_craven_r_tu":
			simplified = buildCCECTOUPeriods(
				year,
				"CCEC Schedule R-TU",
				ccecScheduleRTUDollarsPerKWH,
				ccecScheduleRTUDollarsPerKWH,
				false,
				0,
				buyAllSellAll,
			)

		case "carteret_craven_r_evtu":
			simplified = buildCCECTOUPeriods(
				year,
				"CCEC Schedule R-EVTU",
				ccecScheduleREVTUOnOffPeakDollarsPerKWH,
				ccecScheduleREVTUOnOffPeakDollarsPerKWH,
				true,
				ccecScheduleREVTUSuperOffPeakDollarsPerKWH,
				buyAllSellAll,
			)
		}

		periods = append(periods, buildPeriods(etLocation, simplified)...)
	}

	return periods
}

// carteretCravenUtilityInfo returns metadata and rate options for Carteret-Craven Electric Cooperative.
func carteretCravenUtilityInfo() types.UtilityProviderInfo {
	makeExportOption := func(defaultScheme string) types.UtilityRateOption {
		return types.UtilityRateOption{
			Field:       "netMeteringScheme",
			Name:        "Solar / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "CCEC does not offer traditional 1:1 net metering. Choose Solar + Battery (no export credit) or dual-metered Buy-All / Sell-All under Rider RQF ($0.0407/kWh).",
			Choices: []types.UtilityOptionChoice{
				{Value: "none", Name: "Solar + Battery / No Export Credit ($0.00/kWh)"},
				{Value: "rqf", Name: "Buy-All / Sell-All (Rider RQF - $0.0407/kWh)"},
			},
			Default: defaultScheme,
		}
	}

	years := []int{2026, 2027}

	return types.UtilityProviderInfo{
		ID:   "carteret_craven",
		Name: "Carteret-Craven Electric Cooperative (CCEC)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "carteret_craven_r_tu_e",
				Name:    "All-Energy Time-of-Use (Schedule R-TU-E)",
				Options: []types.UtilityRateOption{makeExportOption("none")},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return carteretCravenPeriods("carteret_craven_r_tu_e", opts, years), nil
				},
			},
			{
				ID:      "carteret_craven_r",
				Name:    "Residential Service (Schedule R)",
				Options: []types.UtilityRateOption{makeExportOption("rqf")},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return carteretCravenPeriods("carteret_craven_r", opts, years), nil
				},
			},
			{
				ID:      "carteret_craven_r_tu",
				Name:    "Residential Time-of-Use Demand (Schedule R-TU - Frozen)",
				Options: []types.UtilityRateOption{makeExportOption("none")},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return carteretCravenPeriods("carteret_craven_r_tu", opts, years), nil
				},
			},
			{
				ID:      "carteret_craven_r_evtu",
				Name:    "Residential EV Time-of-Use Demand (Schedule R-EVTU - Frozen)",
				Options: []types.UtilityRateOption{makeExportOption("none")},
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return carteretCravenPeriods("carteret_craven_r_evtu", opts, years), nil
				},
			},
		},
	}
}
