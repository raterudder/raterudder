package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUtilityPeriodContains(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		p := &UtilityPeriod{
			HourStart: 0,
			HourEnd:   24,
		}
		// Any time should be contained if within the hour range
		now := time.Now()
		contained, err := p.Contains(now)
		require.NoError(t, err)
		assert.True(t, contained)
	})

	t.Run("date range", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
		p := &UtilityPeriod{
			Start:     start,
			End:       end,
			HourStart: 0,
			HourEnd:   24,
		}

		// Exactly at start
		contained, err := p.Contains(start)
		require.NoError(t, err)
		assert.True(t, contained)

		// Exactly at end does NOT contain since the end is exclusive
		contained, err = p.Contains(end)
		require.NoError(t, err)
		assert.False(t, contained)

		// Before start
		contained, err = p.Contains(start.Add(-time.Second))
		require.NoError(t, err)
		assert.False(t, contained)

		// After end
		contained, err = p.Contains(end.Add(time.Second))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("hour range", func(t *testing.T) {
		p := &UtilityPeriod{
			HourStart: 9,
			HourEnd:   17,
		}

		// 9:00 AM (at Start)
		contained, err := p.Contains(time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)

		// 4:59 PM (within range)
		contained, err = p.Contains(time.Date(2024, 1, 1, 16, 59, 59, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)

		// 5:00 PM (at End - exclusive)
		contained, err = p.Contains(time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// 8:59 AM (before Start)
		contained, err = p.Contains(time.Date(2024, 1, 1, 8, 59, 59, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("days of the week", func(t *testing.T) {
		p := &UtilityPeriod{
			DaysOfTheWeek: []time.Weekday{time.Monday, time.Wednesday, time.Friday},
			HourStart:     0,
			HourEnd:       24,
		}

		// Monday
		contained, err := p.Contains(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)) // 2024-01-01 is Monday
		require.NoError(t, err)
		assert.True(t, contained)

		// Tuesday
		contained, err = p.Contains(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Wednesday
		contained, err = p.Contains(time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)

		// Sunday
		contained, err = p.Contains(time.Date(2023, 12, 31, 12, 0, 0, 0, time.UTC)) // 2023-12-31 is Sunday
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("location", func(t *testing.T) {
		p := &UtilityPeriod{
			Location:  "America/Chicago",
			HourStart: 9,
			HourEnd:   17,
		}

		// 10:00 AM Central is 16:00 UTC (Standard Time)
		t1 := time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC)
		contained, err := p.Contains(t1)
		require.NoError(t, err)
		assert.True(t, contained)

		// 8:00 AM Central is 14:00 UTC
		t2 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)
		contained, err = p.Contains(t2)
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("invalid location", func(t *testing.T) {
		p := &UtilityPeriod{
			Location: "Invalid/Location",
		}
		_, err := p.Contains(time.Now())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load location")
	})

	t.Run("combination", func(t *testing.T) {
		p := &UtilityPeriod{
			Start:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:           time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			HourStart:     9,
			HourEnd:       17,
			DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
			Location:      "America/Chicago",
		}

		// Monday Jan 1, 2024 10:00 AM Central (16:00 UTC) -> Should be true
		contained, err := p.Contains(time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)

		// Saturday Jan 6, 2024 10:00 AM Central -> Should be false (wrong day)
		contained, err = p.Contains(time.Date(2024, 1, 6, 16, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Monday Jan 1, 2024 8:00 AM Central -> Should be false (wrong hour)
		contained, err = p.Contains(time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Monday Jan 1, 2023 10:00 AM Central -> Should be false (before Start)
		contained, err = p.Contains(time.Date(2023, 1, 1, 16, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("empty days of week", func(t *testing.T) {
		p := &UtilityPeriod{
			DaysOfTheWeek: []time.Weekday{},
			HourStart:     0,
			HourEnd:       24,
		}
		contained, err := p.Contains(time.Now())
		require.NoError(t, err)
		assert.True(t, contained)
	})
}

func TestPriceContains(t *testing.T) {
	t.Run("time range", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		end := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
		p := &Price{
			TSStart: start,
			TSEnd:   end,
		}

		// Exactly at start
		assert.True(t, p.Contains(start))

		// Exactly at end (exclusive)
		assert.False(t, p.Contains(end))

		// Before start
		assert.False(t, p.Contains(start.Add(-time.Second)))

		// After end
		assert.False(t, p.Contains(end.Add(time.Second)))

		// Within range
		assert.True(t, p.Contains(start.Add(30*time.Minute)))
	})

	t.Run("zero end time", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		p := &Price{
			TSStart: start,
		}

		// Exactly at start
		assert.True(t, p.Contains(start))

		// After start
		assert.True(t, p.Contains(start.Add(24*time.Hour)))

		// Before start
		assert.False(t, p.Contains(start.Add(-time.Second)))
	})
}
