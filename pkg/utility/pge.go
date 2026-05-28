package utility

import (
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

func shiftPGEWeekendHoliday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, -1)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	default:
		return t
	}
}

func getPortlandGeneralHolidays(year int) []string {
	var holidays []time.Time

	// New Year's Day
	holidays = append(holidays, shiftPGEWeekendHoliday(time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)))

	// Memorial Day: last Monday in May
	memDay := time.Date(year, time.May, 31, 0, 0, 0, 0, time.UTC)
	for memDay.Weekday() != time.Monday {
		memDay = memDay.AddDate(0, 0, -1)
	}
	holidays = append(holidays, memDay)

	// Independence Day: July 4
	holidays = append(holidays, shiftPGEWeekendHoliday(time.Date(year, time.July, 4, 0, 0, 0, 0, time.UTC)))

	// Labor Day: first Monday in September
	laborDay := time.Date(year, time.September, 1, 0, 0, 0, 0, time.UTC)
	for laborDay.Weekday() != time.Monday {
		laborDay = laborDay.AddDate(0, 0, 1)
	}
	holidays = append(holidays, laborDay)

	// Thanksgiving: fourth Thursday in November
	tgDay := time.Date(year, time.November, 1, 0, 0, 0, 0, time.UTC)
	for tgDay.Weekday() != time.Thursday {
		tgDay = tgDay.AddDate(0, 0, 1)
	}
	tgDay = tgDay.AddDate(0, 0, 21)
	holidays = append(holidays, tgDay)

	// Christmas Day: Dec 25
	holidays = append(holidays, shiftPGEWeekendHoliday(time.Date(year, time.December, 25, 0, 0, 0, 0, time.UTC)))

	// Check if next year's New Year's Day holiday falls in this year (Dec 31)
	nextNY := time.Date(year+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	if nextNY.Weekday() == time.Saturday {
		holidays = append(holidays, nextNY.AddDate(0, 0, -1))
	}

	// Filter holidays to only keep those that fall in the requested year,
	// and format them as "2006-01-02"
	var holidayStrings []string
	for _, h := range holidays {
		if h.Year() == year {
			holidayStrings = append(holidayStrings, h.Format("2006-01-02"))
		}
	}
	return holidayStrings
}

func portlandGeneralPeriods(years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod
	for _, year := range years {
		holidays := getPortlandGeneralHolidays(year)

		// 1. Holiday period: applies all day on the specific holiday dates
		holidayPeriod := touSimplifiedPeriod{
			Year:          year,
			MonthStart:    time.January,
			MonthEnd:      time.December,
			SpecificDates: holidays,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					// All day (empty Hours means all day)
					DollarsPerKWH: 9.01 / 100.0,
					Description:   "Off-Peak (Holiday)",
				},
			},
		}

		// 2. Regular periods: apply on non-holiday dates
		regularPeriod := touSimplifiedPeriod{
			Year:             year,
			MonthStart:       time.January,
			MonthEnd:         time.December,
			SpecificDates:    holidays,
			SpecificDatesNot: true,
			HoursAndDays: []touSimplifiedHoursAndDays{
				{
					// On-Peak: 5-9 p.m. Monday-Friday (17:00 to 21:00)
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 21},
					},
					Weekday:       true,
					DollarsPerKWH: 43.65 / 100.0,
					Description:   "On-Peak",
				},
				{
					// Mid-Peak: 7 a.m. to 5 p.m. Monday-Friday (07:00 to 17:00)
					Hours: []types.UtilityHourPeriod{
						{HourStart: 7, HourEnd: 17},
					},
					Weekday:       true,
					DollarsPerKWH: 16.89 / 100.0,
					Description:   "Mid-Peak",
				},
				{
					// Weekend: Saturday and Sunday all-day Off-Peak
					Weekend:       true,
					DollarsPerKWH: 9.01 / 100.0,
					Description:   "Off-Peak (Weekend)",
				},
			},
			OtherDollarsPerKWH: 9.01 / 100.0,
			OtherDescription:   "Off-Peak",
		}

		// PGE timezone is Pacific Time (America/Los_Angeles)
		periods = append(periods, buildPeriods(ptLocation.String(), []touSimplifiedPeriod{holidayPeriod, regularPeriod})...)
	}
	return periods
}
