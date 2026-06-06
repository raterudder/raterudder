package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// getSDGEHolidays calculates designated holidays for SDG&E.
// The following 8 holidays are observed:
// 1. New Year's Day (January 1)
// 2. Presidents' Day (Third Monday in February)
// 3. Memorial Day (Last Monday in May)
// 4. Independence Day (July 4)
// 5. Labor Day (First Monday in September)
// 6. Veterans Day (November 11)
// 7. Thanksgiving Day (Fourth Thursday in November)
// 8. Christmas Day (December 25)
//
// Shift rules:
// - If a holiday falls on a Sunday, the following Monday is recognized as the holiday.
// - If a holiday falls on a Saturday, it is not shifted (Saturday is already treated as weekend).
func getSDGEHolidays(year int) []string {
	shiftSDGEHoliday := func(t time.Time) time.Time {
		if t.Weekday() == time.Sunday {
			return t.AddDate(0, 0, 1)
		}
		return t
	}

	holidays := []time.Time{
		shiftSDGEHoliday(newYearsDay(year)),
		shiftSDGEHoliday(presidentsDay(year)),
		memorialDay(year),
		shiftSDGEHoliday(independenceDay(year)),
		laborDay(year),
		shiftSDGEHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftSDGEHoliday(christmasDay(year)),
	}

	return formatHolidays(holidays, year)
}

// sdgePeriods generates the pricing periods for SDG&E / SDCP.
func sdgePeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	locStr := ptLocation.String()

	// Default options if empty
	genRate := options.GenerationRate
	if genRate == "" {
		genRate = "sdcp_power_on"
	}
	location := options.Location
	if location == "" {
		location = "san_diego"
	}

	const nbc = 0.00591
	const pcia = 0.04987

	for _, year := range years {
		holidays := getSDGEHolidays(year)

		// Define seasonal periods
		// Summer: June 1 - October 31
		// Winter: November 1 - May 31
		for _, season := range []string{"summer", "winter"} {
			var monthStart, monthEnd time.Month
			if season == "summer" {
				monthStart = time.June
				monthEnd = time.October
			} else {
				// Winter wraps around from November to May
				monthStart = time.November
				monthEnd = time.May
			}

			// Helper to get generation rate
			getGenRate := func(touPeriod string) float64 {
				if genRate == "sdge" {
					return getSDGEBundledEECC(plan, season, touPeriod)
				}
				// SDCP Rate: Base / On / 100
				baseRate := getSDCPRate(plan, location, genRate, season, touPeriod)
				return baseRate + pcia
			}

			// 1. Holiday / Weekend Period (Weekends and observed holidays)
			holidayPeriod := touSimplifiedPeriod{
				Year:          year,
				MonthStart:    monthStart,
				MonthEnd:      monthEnd,
				SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// On-Peak: 4:00 PM - 9:00 PM (16:00 to 21:00)
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						DollarsPerKWH: getUDCRate(plan, season, "On-Peak") + nbc + getGenRate("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend On-Peak", plan, season),
					},
					{
						// Off-Peak: 2:00 PM - 4:00 PM & 9:00 PM - 12:00 AM (14:00 to 16:00 & 21:00 to 24:00)
						Hours: []types.UtilityHourPeriod{
							{HourStart: 14, HourEnd: 16},
							{HourStart: 21, HourEnd: 24},
						},
						DollarsPerKWH: getUDCRate(plan, season, "Off-Peak") + nbc + getGenRate("Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Off-Peak", plan, season),
					},
				},
				// Super Off-Peak: Midnight - 2:00 PM (00:00 to 14:00)
				OtherDollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak") + nbc + getGenRate("Super Off-Peak"),
				OtherDescription:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Super Off-Peak", plan, season),
			}

			// 2. Regular Days (non-holidays)
			regularPeriod := touSimplifiedPeriod{
				Year:             year,
				MonthStart:       monthStart,
				MonthEnd:         monthEnd,
				SpecificDates:    holidays,
				SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						// On-Peak: 4:00 PM - 9:00 PM (16:00 to 21:00) Weekdays
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						Weekday:       true,
						DollarsPerKWH: getUDCRate(plan, season, "On-Peak") + nbc + getGenRate("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekday On-Peak", plan, season),
					},
					{
						// Super Off-Peak: Midnight - 6:00 AM & 10:00 AM - 2:00 PM Weekdays
						Hours: []types.UtilityHourPeriod{
							{HourStart: 0, HourEnd: 6},
							{HourStart: 10, HourEnd: 14},
						},
						Weekday:       true,
						DollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak") + nbc + getGenRate("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekday Super Off-Peak", plan, season),
					},
					{
						// Weekend behavior for regular weekends (handled by holidayPeriod since specificDates only has holidays)
						Weekend:       true,
						DollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak") + nbc + getGenRate("Super Off-Peak"), // fallback/default to super off-peak, buildPeriods handles weekday/weekend separation
						Description:   fmt.Sprintf("SDG&E %s %s Weekend Super Off-Peak", plan, season),
					},
				},
				// Off-Peak: All other hours Weekdays (6-10 AM, 2-4 PM, 9-12 AM)
				OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak") + nbc + getGenRate("Off-Peak"),
				OtherDescription:   fmt.Sprintf("SDG&E %s %s Weekday Off-Peak", plan, season),
			}

			// Adjust regularPeriod weekend behavior to match holidayPeriod exactly
			regularPeriod.HoursAndDays = []touSimplifiedHoursAndDays{
				{
					// On-Peak: 4:00 PM - 9:00 PM Weekdays
					Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
					Weekday:       true,
					DollarsPerKWH: getUDCRate(plan, season, "On-Peak") + nbc + getGenRate("On-Peak"),
					Description:   fmt.Sprintf("SDG&E %s %s Weekday On-Peak", plan, season),
				},
				{
					// Super Off-Peak: Midnight - 6:00 AM & 10:00 AM - 2:00 PM Weekdays
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 6},
						{HourStart: 10, HourEnd: 14},
					},
					Weekday:       true,
					DollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak") + nbc + getGenRate("Super Off-Peak"),
					Description:   fmt.Sprintf("SDG&E %s %s Weekday Super Off-Peak", plan, season),
				},
				{
					// Weekend On-Peak: 4:00 PM - 9:00 PM Weekends
					Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
					Weekend:       true,
					DollarsPerKWH: getUDCRate(plan, season, "On-Peak") + nbc + getGenRate("On-Peak"),
					Description:   fmt.Sprintf("SDG&E %s %s Weekend On-Peak", plan, season),
				},
				{
					// Weekend Off-Peak: 2:00 PM - 4:00 PM & 9:00 PM - 12:00 AM Weekends
					Hours: []types.UtilityHourPeriod{
						{HourStart: 14, HourEnd: 16},
						{HourStart: 21, HourEnd: 24},
					},
					Weekend:       true,
					DollarsPerKWH: getUDCRate(plan, season, "Off-Peak") + nbc + getGenRate("Off-Peak"),
					Description:   fmt.Sprintf("SDG&E %s %s Weekend Off-Peak", plan, season),
				},
			}
			// Regular Period Weekend Other (Midnight - 2:00 PM)
			regularPeriod.OtherDollarsPerKWH = getUDCRate(plan, season, "Off-Peak") + nbc + getGenRate("Off-Peak")
			regularPeriod.OtherDescription = fmt.Sprintf("SDG&E %s %s Weekday Off-Peak", plan, season)

			// TOU-DR2 is a simpler 2-period rate (On-Peak and Off-Peak)
			if plan == "sdge_tou_dr2" {
				holidayPeriod = touSimplifiedPeriod{
					Year:          year,
					MonthStart:    monthStart,
					MonthEnd:      monthEnd,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: getUDCRate(plan, season, "On-Peak") + nbc + getGenRate("On-Peak"),
							Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend On-Peak", plan, season),
						},
					},
					OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak") + nbc + getGenRate("Off-Peak"),
					OtherDescription:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Off-Peak", plan, season),
				}
				regularPeriod = touSimplifiedPeriod{
					Year:             year,
					MonthStart:       monthStart,
					MonthEnd:         monthEnd,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: getUDCRate(plan, season, "On-Peak") + nbc + getGenRate("On-Peak"),
							Description:   fmt.Sprintf("SDG&E %s %s Daily On-Peak", plan, season),
						},
					},
					OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak") + nbc + getGenRate("Off-Peak"),
					OtherDescription:   fmt.Sprintf("SDG&E %s %s Daily Off-Peak", plan, season),
				}
			}

			periods = append(periods, buildPeriods(locStr, []touSimplifiedPeriod{holidayPeriod, regularPeriod})...)
		}
	}

	return periods
}

// getUDCRate returns the UDC rate component for SDG&E rate sheets.
func getUDCRate(plan, season, period string) float64 {
	switch plan {
	case "sdge_ev_tou":
		switch period {
		case "On-Peak":
			return 0.38692
		case "Off-Peak":
			return 0.38692
		case "Super Off-Peak":
			return 0.23027
		}
	case "sdge_ev_tou_2":
		switch period {
		case "On-Peak":
			return 0.31343
		case "Off-Peak":
			return 0.31343
		case "Super Off-Peak":
			return 0.17246
		}
	case "sdge_ev_tou_5":
		switch period {
		case "On-Peak":
			return 0.32682
		case "Off-Peak":
			return 0.32682
		case "Super Off-Peak":
			return 0.04114
		}
	case "sdge_tou_dr":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.34061
		}
	case "sdge_tou_dr1":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.34061
		}
	case "sdge_tou_dr2":
		switch period {
		case "On-Peak":
			if season == "summer" {
				return 0.34510
			}
			return 0.34061
		case "Off-Peak":
			if season == "summer" {
				return 0.33863
			}
			return 0.34061
		}
	case "sdge_tou_elec":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.26288
		}
	}
	return 0.0
}

// getSDGEBundledEECC returns the commodity generation rate for SDG&E bundled service.
func getSDGEBundledEECC(plan, season, period string) float64 {
	switch plan {
	case "sdge_ev_tou", "sdge_ev_tou_2", "sdge_ev_tou_5":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.47019
			case "Off-Peak":
				return 0.17311
			case "Super Off-Peak":
				return 0.08147
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.19990
			case "Off-Peak":
				return 0.14337
			case "Super Off-Peak":
				return 0.07410
			}
		}
	case "sdge_tou_dr":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.22895
			case "Off-Peak":
				return 0.17007
			case "Super Off-Peak":
				return 0.11457
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.27453
			case "Off-Peak":
				return 0.19288
			case "Super Off-Peak":
				return 0.10220
			}
		}
	case "sdge_tou_dr1":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.34920
			case "Off-Peak":
				return 0.12853
			case "Super Off-Peak":
				return 0.04121
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.27475
			case "Off-Peak":
				return 0.19304
			case "Super Off-Peak":
				return 0.10228
			}
		}
	case "sdge_tou_dr2":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.34920
			case "Off-Peak":
				return 0.08432
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.27475
			case "Off-Peak":
				return 0.13777
			}
		}
	case "sdge_tou_elec":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.45690
			case "Off-Peak":
				return 0.12945
			case "Super Off-Peak":
				return 0.08637
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.24311
			case "Off-Peak":
				return 0.11774
			case "Super Off-Peak":
				return 0.07856
			}
		}
	}
	return 0.0
}

// getSDCPRate returns the commodity generation rate for SDCP unbundled service.
func getSDCPRate(plan, location, tier, season, period string) float64 {
	// Map options to standard SDCP plan name
	var sdcpPlan string
	switch plan {
	case "sdge_ev_tou":
		sdcpPlan = "EV-TOU"
	case "sdge_ev_tou_2":
		sdcpPlan = "EV-TOU-2"
	case "sdge_ev_tou_5":
		sdcpPlan = "EV-TOU-5"
	case "sdge_tou_dr":
		sdcpPlan = "TOU-DR"
	case "sdge_tou_dr1":
		sdcpPlan = "TOU-DR-1"
	case "sdge_tou_dr2":
		sdcpPlan = "TOU-DR-2"
	case "sdge_tou_elec":
		sdcpPlan = "TOU-ELEC"
	default:
		return 0.0
	}

	rates2021v := map[string]map[string]map[string]map[string]float64{
		"EV-TOU": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"EV-TOU-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"EV-TOU-5": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"TOU-DR": {
			"powerOn": {
				"summer": {"On-Peak": 0.18166, "Off-Peak": 0.12578, "Super Off-Peak": 0.07310},
				"winter": {"On-Peak": 0.22492, "Off-Peak": 0.14743, "Super Off-Peak": 0.06136},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.16792, "Off-Peak": 0.11557, "Super Off-Peak": 0.06623},
				"winter": {"On-Peak": 0.20845, "Off-Peak": 0.13585, "Super Off-Peak": 0.05523},
			},
		},
		"TOU-DR-1": {
			"powerOn": {
				"summer": {"On-Peak": 0.29579, "Off-Peak": 0.08635, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.22513, "Off-Peak": 0.14758, "Super Off-Peak": 0.06144},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.27484, "Off-Peak": 0.07864, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.20865, "Off-Peak": 0.13600, "Super Off-Peak": 0.05530},
			},
		},
		"TOU-DR-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.29579, "Off-Peak": 0.04439},
				"winter": {"On-Peak": 0.22513, "Off-Peak": 0.09512},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.27484, "Off-Peak": 0.03933},
				"winter": {"On-Peak": 0.20865, "Off-Peak": 0.08685},
			},
		},
		"TOU-ELEC": {
			"powerOn": {
				"summer": {"On-Peak": 0.39801, "Off-Peak": 0.08722, "Super Off-Peak": 0.04634},
				"winter": {"On-Peak": 0.19510, "Off-Peak": 0.07611, "Super Off-Peak": 0.03892},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.37060, "Off-Peak": 0.07946, "Super Off-Peak": 0.04115},
				"winter": {"On-Peak": 0.18051, "Off-Peak": 0.06904, "Super Off-Peak": 0.03421},
			},
		},
	}

	rates2022v := map[string]map[string]map[string]map[string]float64{
		"EV-TOU": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"EV-TOU-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"EV-TOU-5": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"TOU-DR": {
			"powerOn": {
				"summer": {"On-Peak": 0.18725, "Off-Peak": 0.13137, "Super Off-Peak": 0.07869},
				"winter": {"On-Peak": 0.23051, "Off-Peak": 0.15302, "Super Off-Peak": 0.06695},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.17351, "Off-Peak": 0.12116, "Super Off-Peak": 0.07182},
				"winter": {"On-Peak": 0.21404, "Off-Peak": 0.14144, "Super Off-Peak": 0.06082},
			},
		},
		"TOU-DR-1": {
			"powerOn": {
				"summer": {"On-Peak": 0.30138, "Off-Peak": 0.09194, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.23072, "Off-Peak": 0.15317, "Super Off-Peak": 0.06703},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.28043, "Off-Peak": 0.08423, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.21424, "Off-Peak": 0.14159, "Super Off-Peak": 0.06089},
			},
		},
		"TOU-DR-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.30138, "Off-Peak": 0.04998},
				"winter": {"On-Peak": 0.23072, "Off-Peak": 0.10071},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.28043, "Off-Peak": 0.04492},
				"winter": {"On-Peak": 0.21424, "Off-Peak": 0.09244},
			},
		},
		"TOU-ELEC": {
			"powerOn": {
				"summer": {"On-Peak": 0.40360, "Off-Peak": 0.09281, "Super Off-Peak": 0.05193},
				"winter": {"On-Peak": 0.20069, "Off-Peak": 0.08170, "Super Off-Peak": 0.04451},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.37619, "Off-Peak": 0.08505, "Super Off-Peak": 0.04674},
				"winter": {"On-Peak": 0.18610, "Off-Peak": 0.07463, "Super Off-Peak": 0.03980},
			},
		},
	}

	rates := rates2021v
	if location == "unincorporated" {
		rates = rates2022v
	}

	tierKey := "powerOn"
	if tier == "sdcp_power_base" {
		tierKey = "powerBase"
	}

	premium := 0.0
	if tier == "sdcp_power_100" {
		premium = 0.01
	}

	if planMap, ok := rates[sdcpPlan]; ok {
		if tierMap, ok := planMap[tierKey]; ok {
			if seasonMap, ok := tierMap[season]; ok {
				if val, ok := seasonMap[period]; ok {
					return val + premium
				}
			}
		}
	}
	return 0.0
}

// sdgeUtilityInfo returns the metadata and rates for SDG&E.
func sdgeUtilityInfo() types.UtilityProviderInfo {
	sdgeOptions := []types.UtilityRateOption{
		{
			Field:       "generationRate",
			Name:        "Generation Rate",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your generation provider and rate plan.",
			Choices: []types.UtilityOptionChoice{
				{Value: "sdge", Name: "SDG&E Bundled"},
				{Value: "sdcp_power_base", Name: "SDCP PowerBase"},
				{Value: "sdcp_power_on", Name: "SDCP PowerOn"},
				{Value: "sdcp_power_100", Name: "SDCP Power100"},
			},
			Default: "sdcp_power_on",
		},
		{
			Field:       "location",
			Name:        "Location / Community",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your location context for Community Power rates.",
			Choices: []types.UtilityOptionChoice{
				{Value: "san_diego", Name: "Chula Vista, Encinitas, Imperial Beach, La Mesa and San Diego"},
				{Value: "unincorporated", Name: "National City and Unincorporated County of San Diego"},
			},
			Default: "san_diego",
		},
		{
			Field:       "netMeteringCredits",
			Name:        "Net Metering",
			Type:        types.UtilityOptionTypeSwitch,
			Description: "SDG&E net metering tracks energy exports as kWh 1:1 credits.",
			Default:     true,
			Hidden:      true,
		},
	}

	return types.UtilityProviderInfo{
		ID:   "sdge",
		Name: "San Diego Gas & Electric (SDG&E)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "sdge_ev_tou",
				Name:    "Schedule EV-TOU (Separately Metered EV)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_ev_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_ev_tou_2",
				Name:    "Schedule EV-TOU-2 (Whole House EV)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_ev_tou_2", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_ev_tou_5",
				Name:    "Schedule EV-TOU-5 (Whole House EV with Service Fee)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_ev_tou_5", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_dr",
				Name:    "Schedule TOU-DR (Residential TOU)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_dr", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_dr1",
				Name:    "Schedule TOU-DR1 (Standard Residential TOU)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_dr1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_dr2",
				Name:    "Schedule TOU-DR2 (2-Period TOU)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_dr2", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_elec",
				Name:    "Schedule TOU-ELEC (EV/Storage/Heat Pump)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_elec", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
