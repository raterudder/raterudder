package utility

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHolidays(t *testing.T) {
	t.Run("New Year's Day", func(t *testing.T) {
		assert.Equal(t, "2026-01-01", newYearsDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-01-01", newYearsDay(2027).Format("2006-01-02"))
	})

	t.Run("Martin Luther King Day", func(t *testing.T) {
		assert.Equal(t, "2026-01-19", martinLutherKingDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-01-18", martinLutherKingDay(2027).Format("2006-01-02"))
	})

	t.Run("Presidents' Day", func(t *testing.T) {
		assert.Equal(t, "2026-02-16", presidentsDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-02-15", presidentsDay(2027).Format("2006-01-02"))
	})

	t.Run("Good Friday", func(t *testing.T) {
		assert.Equal(t, "2026-04-03", goodFriday(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-03-26", goodFriday(2027).Format("2006-01-02"))
	})

	t.Run("Memorial Day", func(t *testing.T) {
		assert.Equal(t, "2026-05-25", memorialDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-05-31", memorialDay(2027).Format("2006-01-02"))
	})

	t.Run("Juneteenth", func(t *testing.T) {
		assert.Equal(t, "2026-06-19", juneteenth(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-06-19", juneteenth(2027).Format("2006-01-02"))
	})

	t.Run("Independence Day", func(t *testing.T) {
		assert.Equal(t, "2026-07-04", independenceDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-07-04", independenceDay(2027).Format("2006-01-02"))
	})

	t.Run("Pioneer Day", func(t *testing.T) {
		assert.Equal(t, "2026-07-24", pioneerDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-07-24", pioneerDay(2027).Format("2006-01-02"))
	})

	t.Run("Labor Day", func(t *testing.T) {
		assert.Equal(t, "2026-09-07", laborDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-09-06", laborDay(2027).Format("2006-01-02"))
	})

	t.Run("Columbus Day", func(t *testing.T) {
		assert.Equal(t, "2026-10-12", columbusDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-10-11", columbusDay(2027).Format("2006-01-02"))
	})

	t.Run("Veterans Day", func(t *testing.T) {
		assert.Equal(t, "2026-11-11", veteransDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-11-11", veteransDay(2027).Format("2006-01-02"))
	})

	t.Run("Thanksgiving Day", func(t *testing.T) {
		assert.Equal(t, "2026-11-26", thanksgivingDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-11-25", thanksgivingDay(2027).Format("2006-01-02"))
	})

	t.Run("Christmas Eve", func(t *testing.T) {
		assert.Equal(t, "2026-12-24", christmasEve(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-12-24", christmasEve(2027).Format("2006-01-02"))
	})

	t.Run("Christmas Day", func(t *testing.T) {
		assert.Equal(t, "2026-12-25", christmasDay(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-12-25", christmasDay(2027).Format("2006-01-02"))
	})

	t.Run("New Year's Eve", func(t *testing.T) {
		assert.Equal(t, "2026-12-31", newYearsEve(2026).Format("2006-01-02"))
		assert.Equal(t, "2027-12-31", newYearsEve(2027).Format("2006-01-02"))
	})
}
