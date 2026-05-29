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
	holidays := []time.Time{
		shiftPGEWeekendHoliday(newYearsDay(year)),
		memorialDay(year),
		shiftPGEWeekendHoliday(independenceDay(year)),
		laborDay(year),
		thanksgivingDay(year),
		shiftPGEWeekendHoliday(christmasDay(year)),
	}

	nextNY := newYearsDay(year + 1)
	if nextNY.Weekday() == time.Saturday {
		holidays = append(holidays, nextNY.AddDate(0, 0, -1))
	}

	return formatHolidays(holidays, year)
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
