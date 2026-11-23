package utility

// Tariff Audit Metadata
// - Provider(s): heco (Hawaiian Electric (HECO) (O‘ahu)), helco (Hawai‘i Electric Light (HELCO)), meco (Maui Electric (MECO))
// - Official Sources:
//   - Effective Rate Summary Portal: https://www.hawaiianelectric.com/billing-and-payment/rates-and-regulations/effective-rate-summary
//   - PUC E-Filing F-351295 (Nov 1, 2026 Effective Rate Summary): https://www.hawaiianelectric.com/documents/billing_and_payment/rates/effective_rate_summary/2026/efs_2026_11.pdf (plus Jan–Oct 2026 monthly summaries https://www.hawaiianelectric.com/documents/billing_and_payment/rates/effective_rate_summary/2026/efs_2026_01.pdf through https://www.hawaiianelectric.com/documents/billing_and_payment/rates/effective_rate_summary/2026/efs_2026_10.pdf)
//   - HECO (O‘ahu) Tariffs: Schedule R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rates/heco_rates_sch_r.pdf; Schedule ARD TOU R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rates/heco_rates_ard_tou_r.pdf; Rule 32 (SRE) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rules/32.pdf; Rule 33 (BYOD Plus) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rules/33.pdf; Rules 23 (CGS) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rules/23.pdf, 24 (CGS Plus) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rules/24.pdf, and 25 (Smart Export) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaiian_electric_rules/25.pdf
//   - HELCO (Hawai‘i Island) Tariffs: Schedule R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaii_electric_light_rates/helco_rates_sch_r.pdf; Schedule ARD TOU R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaii_electric_light_rates/helco_rates_ard_tou_r.pdf; Rule 32 (SRE) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/hawaii_electric_light_rules/32.pdf
//   - MECO (Maui, Lāna‘i, Moloka‘i) Tariffs: Maui Schedule R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rates_maui/maui_rates_sch_r.pdf & ARD TOU R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rates_maui/maui_rates_ard_tou_r.pdf; Lāna‘i Schedule R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rates_lanai/lanai_rates_sch_r.pdf & ARD TOU R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rates_lanai/lanai_rates_ard_tou_r.pdf; Moloka‘i Schedule R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rates_molokai/molokai_rates_sch_r.pdf & ARD TOU R https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rates_molokai/molokai_rates_ard_tou_r.pdf; Rule 32 (SRE) https://www.hawaiianelectric.com/documents/billing_and_payment/rates/maui_electric_rules/32.pdf
// - Effective Window: 2026-01-01 through 2027-12-31 (Jan–Nov 2026 monthly effective rates modeled explicitly; Nov 2026 rates carried into Dec 2026 and full-year 2027 until superseded)
// - Revision Cadence: Monthly (ECRC & PPAC Effective Rate Summary filings on the 1st of each month, plus annual Jun 1 RBA Rate Adjustment)
// - Next Audit Due: 2026-12-05 (Check December 1, 2026 Effective Rate Summary efs_2026_12.pdf)
// - Last Verified: 2026-11-21
// - Modeling Notes: Models Schedule R Tier 1 volumetric energy charges (first 350 kWh/mo on O‘ahu and Maui, first 300 kWh/mo on Hawai‘i Island, first 250 kWh/mo on Lāna‘i and Moloka‘i) and Schedule ARD TOU R volumetric TOU charges (09:00–17:00 Daytime, 17:00–21:00 Evening Peak, 21:00–09:00 Overnight) inclusive of monthly ECRC, PPAC, RBA (updated Jun 1, 2026), DSM, DRAC, REIS, and PBF surcharges; excludes fixed monthly Customer Charges, Grid Access Charges, Minimum Charges, and Green Infrastructure Fees; excludes non-volumetric per-kW capacity/grid-service commitment credits under Rule 33 (BYOD Plus); models per-kWh export credits under Rules 23 (CGS), 24 (CGS Plus), 25 (Smart Export), and 32 (Smart Renewable Energy) alongside 1:1 NEM.

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

type hawaiiConsumptionDollarsPerKWH struct {
	scheduleRDollarsPerKWH   float64
	daytimeDollarsPerKWH     float64
	eveningPeakDollarsPerKWH float64
	overnightDollarsPerKWH   float64
}

// hawaiiMonthlyConsumptionDollarsPerKWH stores verified monthly Schedule R (Tier 1) and
// Schedule ARD TOU R effective consumption rates ($/kWh) across all five island divisions.
var hawaiiMonthlyConsumptionDollarsPerKWH = map[int]map[time.Month]map[string]hawaiiConsumptionDollarsPerKWH{
	2026: {
		time.January: {
			"oahu":    {scheduleRDollarsPerKWH: 0.353593, daytimeDollarsPerKWH: 0.179857, eveningPeakDollarsPerKWH: 0.529911, overnightDollarsPerKWH: 0.354884},
			"hawaii":  {scheduleRDollarsPerKWH: 0.403344, daytimeDollarsPerKWH: 0.236800, eveningPeakDollarsPerKWH: 0.693702, overnightDollarsPerKWH: 0.465251},
			"maui":    {scheduleRDollarsPerKWH: 0.368544, daytimeDollarsPerKWH: 0.207922, eveningPeakDollarsPerKWH: 0.611322, overnightDollarsPerKWH: 0.409622},
			"lanai":   {scheduleRDollarsPerKWH: 0.458528, daytimeDollarsPerKWH: 0.244271, eveningPeakDollarsPerKWH: 0.715680, overnightDollarsPerKWH: 0.479976},
			"molokai": {scheduleRDollarsPerKWH: 0.423768, daytimeDollarsPerKWH: 0.235613, eveningPeakDollarsPerKWH: 0.661778, overnightDollarsPerKWH: 0.471971},
		},
		time.February: {
			"oahu":    {scheduleRDollarsPerKWH: 0.355492, daytimeDollarsPerKWH: 0.181706, eveningPeakDollarsPerKWH: 0.531878, overnightDollarsPerKWH: 0.356792},
			"hawaii":  {scheduleRDollarsPerKWH: 0.413272, daytimeDollarsPerKWH: 0.240845, eveningPeakDollarsPerKWH: 0.711001, overnightDollarsPerKWH: 0.475923},
			"maui":    {scheduleRDollarsPerKWH: 0.368690, daytimeDollarsPerKWH: 0.208533, eveningPeakDollarsPerKWH: 0.610923, overnightDollarsPerKWH: 0.409728},
			"lanai":   {scheduleRDollarsPerKWH: 0.435258, daytimeDollarsPerKWH: 0.231643, eveningPeakDollarsPerKWH: 0.677796, overnightDollarsPerKWH: 0.454720},
			"molokai": {scheduleRDollarsPerKWH: 0.425648, daytimeDollarsPerKWH: 0.236593, eveningPeakDollarsPerKWH: 0.692648, overnightDollarsPerKWH: 0.464621},
		},
		time.March: {
			"oahu":    {scheduleRDollarsPerKWH: 0.357883, daytimeDollarsPerKWH: 0.183052, eveningPeakDollarsPerKWH: 0.535574, overnightDollarsPerKWH: 0.359313},
			"hawaii":  {scheduleRDollarsPerKWH: 0.404602, daytimeDollarsPerKWH: 0.236700, eveningPeakDollarsPerKWH: 0.696664, overnightDollarsPerKWH: 0.466682},
			"maui":    {scheduleRDollarsPerKWH: 0.363414, daytimeDollarsPerKWH: 0.205777, eveningPeakDollarsPerKWH: 0.602687, overnightDollarsPerKWH: 0.404232},
			"lanai":   {scheduleRDollarsPerKWH: 0.435448, daytimeDollarsPerKWH: 0.231750, eveningPeakDollarsPerKWH: 0.678117, overnightDollarsPerKWH: 0.454934},
			"molokai": {scheduleRDollarsPerKWH: 0.390288, daytimeDollarsPerKWH: 0.218183, eveningPeakDollarsPerKWH: 0.637418, overnightDollarsPerKWH: 0.427801},
		},
		time.April: {
			"oahu":    {scheduleRDollarsPerKWH: 0.412922, daytimeDollarsPerKWH: 0.211966, eveningPeakDollarsPerKWH: 0.623298, overnightDollarsPerKWH: 0.417632},
			"hawaii":  {scheduleRDollarsPerKWH: 0.402245, daytimeDollarsPerKWH: 0.235288, eveningPeakDollarsPerKWH: 0.693122, overnightDollarsPerKWH: 0.464205},
			"maui":    {scheduleRDollarsPerKWH: 0.395134, daytimeDollarsPerKWH: 0.222232, eveningPeakDollarsPerKWH: 0.652272, overnightDollarsPerKWH: 0.437252},
			"lanai":   {scheduleRDollarsPerKWH: 0.459928, daytimeDollarsPerKWH: 0.245031, eveningPeakDollarsPerKWH: 0.717960, overnightDollarsPerKWH: 0.481496},
			"molokai": {scheduleRDollarsPerKWH: 0.414798, daytimeDollarsPerKWH: 0.230943, eveningPeakDollarsPerKWH: 0.675698, overnightDollarsPerKWH: 0.453321},
		},
		time.May: {
			"oahu":    {scheduleRDollarsPerKWH: 0.477544, daytimeDollarsPerKWH: 0.248030, eveningPeakDollarsPerKWH: 0.723642, overnightDollarsPerKWH: 0.485836},
			"hawaii":  {scheduleRDollarsPerKWH: 0.431413, daytimeDollarsPerKWH: 0.251562, eveningPeakDollarsPerKWH: 0.738451, overnightDollarsPerKWH: 0.495006},
			"maui":    {scheduleRDollarsPerKWH: 0.467531, daytimeDollarsPerKWH: 0.260549, eveningPeakDollarsPerKWH: 0.764529, overnightDollarsPerKWH: 0.512539},
			"lanai":   {scheduleRDollarsPerKWH: 0.641098, daytimeDollarsPerKWH: 0.343348, eveningPeakDollarsPerKWH: 1.012911, overnightDollarsPerKWH: 0.678130},
			"molokai": {scheduleRDollarsPerKWH: 0.532338, daytimeDollarsPerKWH: 0.292143, eveningPeakDollarsPerKWH: 0.859298, overnightDollarsPerKWH: 0.575721},
		},
		time.June: {
			"oahu":    {scheduleRDollarsPerKWH: 0.464958, daytimeDollarsPerKWH: 0.241434, eveningPeakDollarsPerKWH: 0.703578, overnightDollarsPerKWH: 0.472506},
			"hawaii":  {scheduleRDollarsPerKWH: 0.464296, daytimeDollarsPerKWH: 0.269270, eveningPeakDollarsPerKWH: 0.791223, overnightDollarsPerKWH: 0.530246},
			"maui":    {scheduleRDollarsPerKWH: 0.483025, daytimeDollarsPerKWH: 0.268832, eveningPeakDollarsPerKWH: 0.789026, overnightDollarsPerKWH: 0.528929},
			"lanai":   {scheduleRDollarsPerKWH: 0.705946, daytimeDollarsPerKWH: 0.378513, eveningPeakDollarsPerKWH: 1.118170, overnightDollarsPerKWH: 0.748341},
			"molokai": {scheduleRDollarsPerKWH: 0.620533, daytimeDollarsPerKWH: 0.338250, eveningPeakDollarsPerKWH: 0.997383, overnightDollarsPerKWH: 0.667817},
		},
		time.July: {
			"oahu":    {scheduleRDollarsPerKWH: 0.440551, daytimeDollarsPerKWH: 0.225487, eveningPeakDollarsPerKWH: 0.668588, overnightDollarsPerKWH: 0.447037},
			"hawaii":  {scheduleRDollarsPerKWH: 0.464137, daytimeDollarsPerKWH: 0.266365, eveningPeakDollarsPerKWH: 0.794505, overnightDollarsPerKWH: 0.530435},
			"maui":    {scheduleRDollarsPerKWH: 0.450632, daytimeDollarsPerKWH: 0.248860, eveningPeakDollarsPerKWH: 0.742094, overnightDollarsPerKWH: 0.495477},
			"lanai":   {scheduleRDollarsPerKWH: 0.691504, daytimeDollarsPerKWH: 0.367660, eveningPeakDollarsPerKWH: 1.098797, overnightDollarsPerKWH: 0.733229},
			"molokai": {scheduleRDollarsPerKWH: 0.539500, daytimeDollarsPerKWH: 0.292903, eveningPeakDollarsPerKWH: 0.874525, overnightDollarsPerKWH: 0.583714},
		},
		time.August: {
			"oahu":    {scheduleRDollarsPerKWH: 0.425501, daytimeDollarsPerKWH: 0.217341, eveningPeakDollarsPerKWH: 0.644916, overnightDollarsPerKWH: 0.431128},
			"hawaii":  {scheduleRDollarsPerKWH: 0.460841, daytimeDollarsPerKWH: 0.265043, eveningPeakDollarsPerKWH: 0.788734, overnightDollarsPerKWH: 0.526889},
			"maui":    {scheduleRDollarsPerKWH: 0.448050, daytimeDollarsPerKWH: 0.247232, eveningPeakDollarsPerKWH: 0.738396, overnightDollarsPerKWH: 0.492814},
			"lanai":   {scheduleRDollarsPerKWH: 0.579374, daytimeDollarsPerKWH: 0.306813, eveningPeakDollarsPerKWH: 0.916256, overnightDollarsPerKWH: 0.611535},
			"molokai": {scheduleRDollarsPerKWH: 0.439613, daytimeDollarsPerKWH: 0.240907, eveningPeakDollarsPerKWH: 0.718471, overnightDollarsPerKWH: 0.479689},
		},
		time.September: {
			"oahu":    {scheduleRDollarsPerKWH: 0.430628, daytimeDollarsPerKWH: 0.220087, eveningPeakDollarsPerKWH: 0.653020, overnightDollarsPerKWH: 0.436554},
			"hawaii":  {scheduleRDollarsPerKWH: 0.466365, daytimeDollarsPerKWH: 0.267987, eveningPeakDollarsPerKWH: 0.797495, overnightDollarsPerKWH: 0.532741},
			"maui":    {scheduleRDollarsPerKWH: 0.446665, daytimeDollarsPerKWH: 0.246795, eveningPeakDollarsPerKWH: 0.735895, overnightDollarsPerKWH: 0.491345},
			"lanai":   {scheduleRDollarsPerKWH: 0.494904, daytimeDollarsPerKWH: 0.260973, eveningPeakDollarsPerKWH: 0.778736, overnightDollarsPerKWH: 0.519855},
			"molokai": {scheduleRDollarsPerKWH: 0.516354, daytimeDollarsPerKWH: 0.280867, eveningPeakDollarsPerKWH: 0.838349, overnightDollarsPerKWH: 0.559608},
		},
		time.October: {
			"oahu":    {scheduleRDollarsPerKWH: 0.458110, daytimeDollarsPerKWH: 0.234684, eveningPeakDollarsPerKWH: 0.696627, overnightDollarsPerKWH: 0.465655},
			"hawaii":  {scheduleRDollarsPerKWH: 0.468411, daytimeDollarsPerKWH: 0.269140, eveningPeakDollarsPerKWH: 0.800663, overnightDollarsPerKWH: 0.534901},
			"maui":    {scheduleRDollarsPerKWH: 0.445901, daytimeDollarsPerKWH: 0.246403, eveningPeakDollarsPerKWH: 0.734707, overnightDollarsPerKWH: 0.490555},
			"lanai":   {scheduleRDollarsPerKWH: 0.607054, daytimeDollarsPerKWH: 0.321833, eveningPeakDollarsPerKWH: 0.961316, overnightDollarsPerKWH: 0.641575},
			"molokai": {scheduleRDollarsPerKWH: 0.584223, daytimeDollarsPerKWH: 0.316204, eveningPeakDollarsPerKWH: 0.944362, overnightDollarsPerKWH: 0.630283},
		},
		time.November: {
			"oahu":    {scheduleRDollarsPerKWH: 0.486472, daytimeDollarsPerKWH: 0.247695, eveningPeakDollarsPerKWH: 0.744196, overnightDollarsPerKWH: 0.495945},
			"hawaii":  {scheduleRDollarsPerKWH: 0.442048, daytimeDollarsPerKWH: 0.250482, eveningPeakDollarsPerKWH: 0.764635, overnightDollarsPerKWH: 0.507558},
			"maui":    {scheduleRDollarsPerKWH: 0.470357, daytimeDollarsPerKWH: 0.258069, eveningPeakDollarsPerKWH: 0.774114, overnightDollarsPerKWH: 0.516091},
			"lanai":   {scheduleRDollarsPerKWH: 0.581504, daytimeDollarsPerKWH: 0.307968, eveningPeakDollarsPerKWH: 0.919721, overnightDollarsPerKWH: 0.613845},
			"molokai": {scheduleRDollarsPerKWH: 0.505740, daytimeDollarsPerKWH: 0.275323, eveningPeakDollarsPerKWH: 0.821785, overnightDollarsPerKWH: 0.548554},
		},
	},
}

func getHawaiiConsumptionDollarsPerKWH(location string, year int, month time.Month) hawaiiConsumptionDollarsPerKWH {
	if byMonth, ok := hawaiiMonthlyConsumptionDollarsPerKWH[year]; ok {
		if byLocation, ok := byMonth[month]; ok {
			return byLocation[location]
		}
	}
	if year < 2026 {
		return hawaiiMonthlyConsumptionDollarsPerKWH[2026][time.January][location]
	}
	return hawaiiMonthlyConsumptionDollarsPerKWH[2026][time.November][location]
}

type hawaiiTOURates struct {
	daytime     float64
	eveningPeak float64
	overnight   float64
}

var hawaiiSREExportRates = map[string]hawaiiTOURates{
	"oahu":    {daytime: 0.135, eveningPeak: 0.329, overnight: 0.189},
	"hawaii":  {daytime: 0.106, eveningPeak: 0.231, overnight: 0.148},
	"maui":    {daytime: 0.066, eveningPeak: 0.182, overnight: 0.131},
	"lanai":   {daytime: 0.267, eveningPeak: 0.408, overnight: 0.259},
	"molokai": {daytime: 0.179, eveningPeak: 0.272, overnight: 0.174},
}

var hawaiiCGSPlusRates = map[string]float64{
	"oahu":    0.1008,
	"hawaii":  0.1055,
	"maui":    0.1217,
	"lanai":   0.2080,
	"molokai": 0.1677,
}

var hawaiiCGSRates = map[string]float64{
	"oahu":    0.1507,
	"hawaii":  0.1514,
	"maui":    0.1716,
	"lanai":   0.2788,
	"molokai": 0.2407,
}

var hawaiiSmartExportRates = map[string]float64{
	"oahu":    0.1497,
	"hawaii":  0.1100,
	"maui":    0.1441,
	"lanai":   0.2079,
	"molokai": 0.1664,
}

// hawaiiPeriods generates the pricing periods for Hawaiian Electric companies.
func hawaiiPeriods(providerID string, plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// Resolve island location from provider and options
	var location string
	switch providerID {
	case "heco":
		location = "oahu"
	case "helco":
		location = "hawaii"
	case "meco":
		location = options.Location
		if location == "" {
			location = "maui"
		}
	default:
		location = "oahu"
	}

	netMeteringScheme := options.NetMeteringScheme
	if netMeteringScheme == "" {
		netMeteringScheme = "sre_export"
	}

	// Resolve the base plan ID to determine if it is R or ARD TOU R
	isTOU := false
	if plan == "heco_ard_tou_r" || plan == "helco_ard_tou_r" || plan == "meco_ard_tou_r" {
		isTOU = true
	}

	for _, year := range years {
		// 1. Consumption/Import Periods
		if year == 2026 {
			for month := time.January; month <= time.December; month++ {
				rates := getHawaiiConsumptionDollarsPerKWH(location, year, month)
				if !isTOU {
					periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
						{
							Year:               year,
							Months:             []time.Month{month},
							OtherDollarsPerKWH: rates.scheduleRDollarsPerKWH,
							OtherDescription:   "Hawaiian Electric Schedule R Consumption Charge",
						},
					})...)
				} else {
					periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
						{
							Year:   year,
							Months: []time.Month{month},
							HoursAndDays: []touSimplifiedHoursAndDays{
								{
									Name:          "Daytime",
									Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
									DollarsPerKWH: rates.daytimeDollarsPerKWH,
									Description:   "Hawaiian Electric ARD TOU R Daytime",
								},
								{
									Name:          "Evening Peak",
									Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
									DollarsPerKWH: rates.eveningPeakDollarsPerKWH,
									Description:   "Hawaiian Electric ARD TOU R Evening Peak",
								},
							},
							OtherName:          "Overnight",
							OtherDollarsPerKWH: rates.overnightDollarsPerKWH,
							OtherDescription:   "Hawaiian Electric ARD TOU R Overnight",
						},
					})...)
				}
			}
		} else {
			rates := getHawaiiConsumptionDollarsPerKWH(location, year, time.January)
			if !isTOU {
				periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
					{
						Year:               year,
						MonthStart:         time.January,
						MonthEnd:           time.December,
						OtherDollarsPerKWH: rates.scheduleRDollarsPerKWH,
						OtherDescription:   "Hawaiian Electric Schedule R Consumption Charge",
					},
				})...)
			} else {
				periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
					{
						Year:       year,
						MonthStart: time.January,
						MonthEnd:   time.December,
						HoursAndDays: []touSimplifiedHoursAndDays{
							{
								Name:          "Daytime",
								Hours:         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
								DollarsPerKWH: rates.daytimeDollarsPerKWH,
								Description:   "Hawaiian Electric ARD TOU R Daytime",
							},
							{
								Name:          "Evening Peak",
								Hours:         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
								DollarsPerKWH: rates.eveningPeakDollarsPerKWH,
								Description:   "Hawaiian Electric ARD TOU R Evening Peak",
							},
						},
						OtherName:          "Overnight",
						OtherDollarsPerKWH: rates.overnightDollarsPerKWH,
						OtherDescription:   "Hawaiian Electric ARD TOU R Overnight",
					},
				})...)
			}
		}

		// 2. Solar Export Credit Periods (Separate Generation Credits)
		switch netMeteringScheme {
		case "sre_export":
			sreRates := hawaiiSREExportRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                         year,
					MonthStart:                   time.January,
					MonthEnd:                     time.December,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
							GenerationCreditDollarsPerKWH: sreRates.daytime,
							Description:                   "Hawaiian Electric SRE Export Credit Daytime",
						},
						{
							Hours:                         []types.UtilityHourPeriod{{HourStart: 17, HourEnd: 21}},
							GenerationCreditDollarsPerKWH: sreRates.eveningPeak,
							Description:                   "Hawaiian Electric SRE Export Credit Evening Peak",
						},
					},
					OtherGenerationCreditDollarsPerKWH: sreRates.overnight,
					OtherDescription:                   "Hawaiian Electric SRE Export Credit Overnight",
				},
			})...)

		case "cgs_plus":
			flatCredit := hawaiiCGSPlusRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: flatCredit,
					OtherDescription:                   "Hawaiian Electric CGS Plus Export Credit",
				},
			})...)

		case "cgs":
			flatCredit := hawaiiCGSRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                               year,
					MonthStart:                         time.January,
					MonthEnd:                           time.December,
					OnlySeparateGenerationCredit:       true,
					OtherGenerationCreditDollarsPerKWH: flatCredit,
					OtherDescription:                   "Hawaiian Electric CGS Export Credit",
				},
			})...)

		case "smart_export":
			exportCredit := hawaiiSmartExportRates[location]
			periods = append(periods, buildPeriods(hstLocation, []touSimplifiedPeriod{
				{
					Year:                         year,
					MonthStart:                   time.January,
					MonthEnd:                     time.December,
					OnlySeparateGenerationCredit: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:                         []types.UtilityHourPeriod{{HourStart: 9, HourEnd: 16}},
							GenerationCreditDollarsPerKWH: 0.0,
							Description:                   "Hawaiian Electric Smart Export Non-Export Window Credit",
						},
					},
					OtherGenerationCreditDollarsPerKWH: exportCredit,
					OtherDescription:                   "Hawaiian Electric Smart Export Export Window Credit",
				},
			})...)

		case "net":
			// Net energy metering is handled automatically by 1:1 net metering logic.
		}
	}

	return periods
}

// hawaiiUtilityInfo returns the metadata and rates for HECO, HELCO, and MECO.
func hawaiiUtilityInfo() []types.UtilityProviderInfo {
	exportOption := types.UtilityRateOption{
		Field:       "netMeteringScheme",
		Name:        "Net Metering / Export Scheme",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your solar billing plan or export credit program.",
		Choices: []types.UtilityOptionChoice{
			{Value: "sre_export", Name: "Smart Renewable Energy Export"},
			{Value: "cgs_plus", Name: "Customer Grid-Supply Plus"},
			{Value: "cgs", Name: "Customer Grid-Supply"},
			{Value: "smart_export", Name: "Smart Export"},
			{Value: "net", Name: "Net Energy Metering (NEM / NEM+)"},
		},
		Default: "sre_export",
	}

	mecoOptions := []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "Location / Island Division",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select the specific island division for Maui County.",
			Choices: []types.UtilityOptionChoice{
				{Value: "maui", Name: "Maui"},
				{Value: "lanai", Name: "Lāna‘i"},
				{Value: "molokai", Name: "Moloka‘i"},
			},
			Default: "maui",
		},
		exportOption,
	}

	hecoOptions := []types.UtilityRateOption{exportOption}

	return []types.UtilityProviderInfo{
		{
			ID:   "heco",
			Name: "Hawaiian Electric (HECO) (O‘ahu)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "heco_r",
					Name:    "Schedule R (Residential Standard)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("heco", "heco_r", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "heco_ard_tou_r",
					Name:    "Schedule ARD TOU R (Residential Time of Use)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("heco", "heco_ard_tou_r", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "helco",
			Name: "Hawai‘i Electric Light (HELCO)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "helco_r",
					Name:    "Schedule R (Residential Standard)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("helco", "helco_r", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "helco_ard_tou_r",
					Name:    "Schedule ARD TOU R (Residential Time of Use)",
					Options: hecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("helco", "helco_ard_tou_r", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
		{
			ID:   "meco",
			Name: "Maui Electric (MECO)",
			Rates: []types.UtilityRateInfo{
				{
					ID:      "meco_r",
					Name:    "Schedule R (Residential Standard)",
					Options: mecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("meco", "meco_r", opts, []int{2026, 2027}), nil
					},
				},
				{
					ID:      "meco_ard_tou_r",
					Name:    "Schedule ARD TOU R (Residential Time of Use)",
					Options: mecoOptions,
					GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
						return hawaiiPeriods("meco", "meco_ard_tou_r", opts, []int{2026, 2027}), nil
					},
				},
			},
		},
	}
}
