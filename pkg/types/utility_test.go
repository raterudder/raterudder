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
			Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
		}
		// Any time should be contained if within the hour range
		now := time.Now()
		contained, startEnd, err := p.Contains(now)
		require.NoError(t, err)
		assert.True(t, contained)
		expectedStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		expectedEnd := time.Date(now.Year(), now.Month(), now.Day(), 24, 0, 0, 0, now.Location())
		assert.Equal(t, expectedStart, startEnd.Start)
		assert.Equal(t, expectedEnd, startEnd.End)
	})

	t.Run("empty period", func(t *testing.T) {
		p := &UtilityPeriod{}
		// Test various times: past, present, future, leap years
		times := []time.Time{
			time.Now(),
			time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC), // Leap day
			time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC),
		}
		for _, ts := range times {
			contained, startEnd, err := p.Contains(ts)
			require.NoError(t, err)
			assert.True(t, contained, "Empty period should contain %v", ts)
			assert.True(t, startEnd.Start.IsZero())
			assert.True(t, startEnd.End.IsZero())
		}
	})

	t.Run("date range", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
		p := &UtilityPeriod{
			Start: start,
			End:   end,
			Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
		}

		// Exactly at start
		contained, startEnd, err := p.Contains(start)
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, start, startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), startEnd.End)

		// Exactly at end does NOT contain since the end is exclusive
		contained, _, err = p.Contains(end)
		require.NoError(t, err)
		assert.False(t, contained)

		// Before start
		contained, _, err = p.Contains(start.Add(-time.Second))
		require.NoError(t, err)
		assert.False(t, contained)

		// After end
		contained, _, err = p.Contains(end.Add(time.Second))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("hour range", func(t *testing.T) {
		p := &UtilityPeriod{
			Hours: []UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
		}

		// 9:00 AM (at Start)
		contained, startEnd, err := p.Contains(time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC), startEnd.End)

		// 4:59 PM (within range)
		contained, startEnd, err = p.Contains(time.Date(2024, 1, 1, 16, 59, 59, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC), startEnd.End)

		// 5:00 PM (at End - exclusive)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// 8:59 AM (before Start)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 8, 59, 59, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("days of the week", func(t *testing.T) {
		p := &UtilityPeriod{
			DaysOfTheWeek: []time.Weekday{time.Monday, time.Wednesday, time.Friday},
			Hours:         []UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
		}

		// Monday
		contained, startEnd, err := p.Contains(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)) // 2024-01-01 is Monday
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 24, 0, 0, 0, time.UTC), startEnd.End)

		// Tuesday
		contained, _, err = p.Contains(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Wednesday
		contained, startEnd, err = p.Contains(time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 3, 24, 0, 0, 0, time.UTC), startEnd.End)

		// Sunday
		contained, _, err = p.Contains(time.Date(2023, 12, 31, 12, 0, 0, 0, time.UTC)) // 2023-12-31 is Sunday
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("location", func(t *testing.T) {
		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		p := &UtilityPeriod{
			LocationPtr: chi,
			Hours:       []UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
		}

		// 10:00 AM Central is 16:00 UTC (Standard Time)
		t1 := time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC)
		contained, startEnd, err := p.Contains(t1)
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, chi), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 17, 0, 0, 0, chi), startEnd.End)

		// 8:00 AM Central is 14:00 UTC
		t2 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)
		contained, _, err = p.Contains(t2)
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("combination", func(t *testing.T) {
		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		p := &UtilityPeriod{
			Start:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:           time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			Hours:         []UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
			DaysOfTheWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
			LocationPtr:   chi,
		}

		// Monday Jan 1, 2024 10:00 AM Central (16:00 UTC) -> Should be true
		contained, startEnd, err := p.Contains(time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, chi), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 17, 0, 0, 0, chi), startEnd.End)

		// Saturday Jan 6, 2024 10:00 AM Central -> Should be false (wrong day)
		contained, _, err = p.Contains(time.Date(2024, 1, 6, 16, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Monday Jan 1, 2024 8:00 AM Central -> Should be false (wrong hour)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Monday Jan 1, 2023 10:00 AM Central -> Should be false (before Start)
		contained, _, err = p.Contains(time.Date(2023, 1, 1, 16, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("empty days of week", func(t *testing.T) {
		p := &UtilityPeriod{
			DaysOfTheWeek: []time.Weekday{},
			Hours:         []UtilityHourPeriod{{HourStart: 0, HourEnd: 24}},
		}
		contained, _, err := p.Contains(time.Now())
		require.NoError(t, err)
		assert.True(t, contained)
	})

	t.Run("HoursNot", func(t *testing.T) {
		p := &UtilityPeriod{
			Hours:    []UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
			HoursNot: true,
		}

		// 8:00 AM (Outside 9-17, should be true because HoursNot is true)
		contained, startEnd, err := p.Contains(time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.True(t, startEnd.Start.IsZero())

		// 10:00 AM (Inside 9-17, should be false)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// 5:00 PM (Start of exclusive end, should be true)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
	})

	t.Run("multiple hour ranges", func(t *testing.T) {
		p := &UtilityPeriod{
			Hours: []UtilityHourPeriod{
				{HourStart: 7, HourEnd: 10},
				{HourStart: 17, HourEnd: 21},
			},
		}

		// 8:00 AM (in first range)
		contained, startEnd, err := p.Contains(time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 7, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), startEnd.End)

		// 12:00 PM (out of range)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// 7:00 PM (in second range)
		contained, startEnd, err = p.Contains(time.Date(2024, 1, 1, 19, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 21, 0, 0, 0, time.UTC), startEnd.End)
	})

	t.Run("Overlapping hour ranges", func(t *testing.T) {
		p := &UtilityPeriod{
			Hours: []UtilityHourPeriod{
				{HourStart: 9, HourEnd: 15},
				{HourStart: 12, HourEnd: 17},
			},
		}
		// 14:00 (In both ranges, should pick the one that contains it first)
		contained, startEnd, _ := p.Contains(time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC))
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC), startEnd.End)

		// 16:00 (In second range only)
		contained, startEnd, _ = p.Contains(time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC))
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC), startEnd.End)
	})

	t.Run("DST Transition - America/Chicago Spring Forward", func(t *testing.T) {
		// March 10, 2024: 02:00:00 -> 03:00:00
		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		p := &UtilityPeriod{
			LocationPtr: chi,
			Hours:       []UtilityHourPeriod{{HourStart: 2, HourEnd: 3}},
		}

		// 1:59 AM (should be false)
		t1 := time.Date(2024, 3, 10, 1, 59, 0, 0, chi)
		contained, _, _ := p.Contains(t1)
		assert.False(t, contained)

		// 3:00 AM (The hour 2:00-3:00 is skipped, so 3:00 is the first valid hour after 1:59)
		t2 := time.Date(2024, 3, 10, 3, 0, 0, 0, chi)
		contained, _, _ = p.Contains(t2)
		assert.False(t, contained, "Hour 3 is outside range [2, 3)")
	})

	t.Run("SpecificDates", func(t *testing.T) {
		p := &UtilityPeriod{
			SpecificDates: []string{"2026-01-01", "2026-12-25"},
		}

		// Matching date
		contained, startEnd, err := p.Contains(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.True(t, startEnd.Start.IsZero())

		// Another matching date
		contained, _, err = p.Contains(time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)

		// Non-matching date
		contained, _, err = p.Contains(time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)
	})

	t.Run("SpecificDatesNot", func(t *testing.T) {
		p := &UtilityPeriod{
			SpecificDates:    []string{"2026-01-01", "2026-12-25"},
			SpecificDatesNot: true,
		}

		// Matching date (should be excluded)
		contained, _, err := p.Contains(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// Non-matching date (should be included)
		contained, _, err = p.Contains(time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
	})

	t.Run("sub-hour and 15-minute periods", func(t *testing.T) {
		p := &UtilityPeriod{
			Hours: []UtilityHourPeriod{
				{HourStart: 6, MinuteStart: 30, HourEnd: 9, MinuteEnd: 0},
				{HourStart: 10, MinuteStart: 15, HourEnd: 10, MinuteEnd: 45},
			},
		}

		// 6:15 AM (before range 1)
		contained, _, err := p.Contains(time.Date(2024, 1, 1, 6, 15, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// 6:30 AM (exactly at range 1 start)
		contained, startEnd, err := p.Contains(time.Date(2024, 1, 1, 6, 30, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 6, 30, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC), startEnd.End)

		// 8:59 AM (within range 1)
		contained, startEnd, err = p.Contains(time.Date(2024, 1, 1, 8, 59, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 6, 30, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC), startEnd.End)

		// 9:00 AM (at range 1 end - exclusive)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)

		// 10:15 AM (exactly at range 2 start)
		contained, startEnd, err = p.Contains(time.Date(2024, 1, 1, 10, 15, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.True(t, contained)
		assert.Equal(t, time.Date(2024, 1, 1, 10, 15, 0, 0, time.UTC), startEnd.Start)
		assert.Equal(t, time.Date(2024, 1, 1, 10, 45, 0, 0, time.UTC), startEnd.End)

		// 10:45 AM (at range 2 end - exclusive)
		contained, _, err = p.Contains(time.Date(2024, 1, 1, 10, 45, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.False(t, contained)
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

func TestUtilityFeesPeriodApply(t *testing.T) {
	testTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	basePrice := 0.20

	t.Run("basic DollarsPerKWH", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod: UtilityPeriod{},
			DollarsPerKWH: 0.05,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.Equal(t, 0.25, result.DollarsPerKWH)
	})

	t.Run("GridAdditional fixed fee", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:  UtilityPeriod{},
			DollarsPerKWH:  0.03,
			GridAdditional: true,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.Equal(t, 0.20, result.DollarsPerKWH)
		assert.Equal(t, 0.03, result.GridUseDollarsPerKWH)
	})

	t.Run("SeparateGenerationCredit", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:            UtilityPeriod{},
			DollarsPerKWH:            0.08,
			SeparateGenerationCredit: true,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.Equal(t, 0.20, result.DollarsPerKWH)
		assert.Equal(t, 0.08, result.GenerationCreditDollarsPerKWH)
		assert.True(t, result.SeparateGenerationCredit)
	})

	t.Run("DollarsPerKWHPreMultiple", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:            UtilityPeriod{},
			DollarsPerKWHPreMultiple: 0.1, // 10%
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		// 0.20 + (0.20 * 0.1) = 0.22
		assert.InDelta(t, 0.22, result.DollarsPerKWH, 0.0001)
	})

	t.Run("GridAdditional with multiplier", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:            UtilityPeriod{},
			DollarsPerKWHPreMultiple: 0.05, // 5%
			GridAdditional:           true,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		// GridUse += 0.20 * 0.05 = 0.01
		assert.InDelta(t, 0.01, result.GridUseDollarsPerKWH, 0.0001)
	})

	t.Run("Combination: fixed fee and multiplier precedence", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:            UtilityPeriod{},
			DollarsPerKWH:            0.10, // Should be ignored
			DollarsPerKWHPreMultiple: 0.1,  // Should be used
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		// 0.20 + (0.20 * 0.1) = 0.22 (if fixed was used, would be 0.30)
		assert.InDelta(t, 0.22, result.DollarsPerKWH, 0.0001)
	})

	t.Run("Combination: GridAdditional precedence", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:            UtilityPeriod{},
			DollarsPerKWH:            0.10, // Should be ignored
			DollarsPerKWHPreMultiple: 0.1,  // Should be used
			GridAdditional:           true,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.InDelta(t, 0.02, result.GridUseDollarsPerKWH, 0.0001)
	})

	t.Run("Contains logic: outside hour range", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod: UtilityPeriod{Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}}, // 12:00 is exclusive
			DollarsPerKWH: 0.10,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice} // 12:00
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.Equal(t, basePrice, result.DollarsPerKWH) // Unchanged
	})

	t.Run("Contains logic: different location", func(t *testing.T) {
		// testTime is 12:00 UTC.
		// America/Chicago is UTC-6 (Standard) or UTC-5 (Daylight).
		// Jan 1st is Standard Time. 12:00 UTC is 6:00 AM CST.
		chi, err := time.LoadLocation("America/Chicago")
		require.NoError(t, err)
		up := UtilityFeesPeriod{
			UtilityPeriod: UtilityPeriod{
				LocationPtr: chi,
				Hours:       []UtilityHourPeriod{{HourStart: 9, HourEnd: 17}},
			},
			DollarsPerKWH: 0.10,
		}
		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.Equal(t, basePrice, result.DollarsPerKWH) // Unchanged because 6:00 AM is outside 9-17 range
	})

	t.Run("Multiple periods in sequence", func(t *testing.T) {
		periods := []UtilityFeesPeriod{
			{DollarsPerKWH: 0.10},
			{DollarsPerKWHPreMultiple: 0.05}, // 5% of original base
			{DollarsPerKWH: 0.02, GridAdditional: true},
		}

		p := Price{TSStart: testTime, DollarsPerKWH: basePrice}
		current := p
		var err error
		for _, up := range periods {
			current, err = up.Apply(current, p)
			require.NoError(t, err)
		}

		// Final DollarsPerKWH: base (0.20) + fixed (0.10) + percentage (0.20 * 0.05 = 0.01) = 0.31
		assert.InDelta(t, 0.31, current.DollarsPerKWH, 0.0001)
		// Final GridUse: 0.02
		assert.InDelta(t, 0.02, current.GridUseDollarsPerKWH, 0.0001)
	})

	t.Run("Apply with zero base price and multiplier", func(t *testing.T) {
		up := UtilityFeesPeriod{
			UtilityPeriod:            UtilityPeriod{},
			DollarsPerKWHPreMultiple: 0.5, // 50%
		}
		p := Price{TSStart: testTime, DollarsPerKWH: 0.0}
		result, err := up.Apply(p, p)
		require.NoError(t, err)
		assert.Equal(t, 0.0, result.DollarsPerKWH, "Multiplier on zero should be zero")
	})

	t.Run("Apply with multiple multipliers", func(t *testing.T) {
		periods := []UtilityFeesPeriod{
			{DollarsPerKWHPreMultiple: 0.1}, // 10%
			{DollarsPerKWHPreMultiple: 0.2}, // 20%
		}
		orig := Price{TSStart: testTime, DollarsPerKWH: 1.00}
		curr := orig
		var err error
		for _, up := range periods {
			curr, err = up.Apply(curr, orig)
			require.NoError(t, err)
		}
		// 1.00 + (1.00 * 0.1) + (1.00 * 0.2) = 1.30
		assert.InDelta(t, 1.30, curr.DollarsPerKWH, 0.0001)
	})
}

func TestApplyUtilityFeesPeriods(t *testing.T) {
	testTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	basePrice := 0.20
	p := Price{TSStart: testTime, DollarsPerKWH: basePrice}

	t.Run("Empty periods", func(t *testing.T) {
		result, err := ApplyUtilityFeesPeriods(p, nil)
		require.NoError(t, err)
		assert.Equal(t, p, result)
	})

	t.Run("Multiple periods - cumulative and separate", func(t *testing.T) {
		periods := []UtilityFeesPeriod{
			{
				UtilityPeriod: UtilityPeriod{},
				DollarsPerKWH: 0.10,
				Description:   "Fixed 1",
			},
			{
				UtilityPeriod:            UtilityPeriod{},
				DollarsPerKWHPreMultiple: 0.1, // 10% of 0.20 = 0.02
				Description:              "10% Markup",
			},
			{
				UtilityPeriod:  UtilityPeriod{},
				DollarsPerKWH:  0.05,
				GridAdditional: true,
				Description:    "Grid Delivery",
			},
			{
				UtilityPeriod:            UtilityPeriod{},
				DollarsPerKWH:            0.08,
				SeparateGenerationCredit: true,
				Description:              "Solar Credit",
			},
		}

		result, err := ApplyUtilityFeesPeriods(p, periods)
		require.NoError(t, err)

		// Base (0.20) + Fixed (0.10) + Markup (0.02) = 0.32
		assert.InDelta(t, 0.32, result.DollarsPerKWH, 0.0001)
		// Grid Delivery = 0.05
		assert.Equal(t, 0.05, result.GridUseDollarsPerKWH)
		// Generation Credit = 0.08
		assert.Equal(t, 0.08, result.GenerationCreditDollarsPerKWH)
	})

	t.Run("Periods outside timing", func(t *testing.T) {
		periods := []UtilityFeesPeriod{
			{
				UtilityPeriod: UtilityPeriod{Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}}, // Should not apply (testTime is 12:00)
				DollarsPerKWH: 1.00,
			},
			{
				UtilityPeriod: UtilityPeriod{Hours: []UtilityHourPeriod{{HourStart: 6, HourEnd: 18}}}, // Should apply
				DollarsPerKWH: 0.05,
			},
		}

		result, err := ApplyUtilityFeesPeriods(p, periods)
		require.NoError(t, err)

		// Base (0.20) + 0.05 = 0.25
		assert.InDelta(t, 0.25, result.DollarsPerKWH, 0.0001)
	})
}
