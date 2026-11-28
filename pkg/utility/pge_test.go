package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPGEAdjustments(t *testing.T) {
	t.Run("2025 adjustments", func(t *testing.T) {
		t2025 := time.Date(2025, time.June, 15, 12, 0, 0, 0, ptLocation)

		onPeakAdj := getPGEAdjustments(t2025, "On-Peak")
		midPeakAdj := getPGEAdjustments(t2025, "Mid-Peak")
		offPeakAdj := getPGEAdjustments(t2025, "Off-Peak")

		assert.InDelta(t, 0.14340, onPeakAdj, 1e-6)
		assert.InDelta(t, 0.06807, midPeakAdj, 1e-6)
		assert.InDelta(t, 0.04604, offPeakAdj, 1e-6)
	})

	t.Run("April 2026 adjustments", func(t *testing.T) {
		tApr := time.Date(2026, time.April, 15, 12, 0, 0, 0, ptLocation)

		onPeakAdj := getPGEAdjustments(tApr, "On-Peak")
		midPeakAdj := getPGEAdjustments(tApr, "Mid-Peak")
		offPeakAdj := getPGEAdjustments(tApr, "Off-Peak")

		assert.InDelta(t, 0.14104, onPeakAdj, 1e-6)
		assert.InDelta(t, 0.06705, midPeakAdj, 1e-6)
		assert.InDelta(t, 0.04541, offPeakAdj, 1e-6)
	})

	t.Run("May 2026 adjustments", func(t *testing.T) {
		tMay := time.Date(2026, time.May, 18, 12, 0, 0, 0, ptLocation)

		onPeakAdj := getPGEAdjustments(tMay, "On-Peak")
		midPeakAdj := getPGEAdjustments(tMay, "Mid-Peak")
		offPeakAdj := getPGEAdjustments(tMay, "Off-Peak")

		assert.InDelta(t, 0.14102, onPeakAdj, 1e-6)
		assert.InDelta(t, 0.06703, midPeakAdj, 1e-6)
		assert.InDelta(t, 0.04539, offPeakAdj, 1e-6)
	})

	t.Run("July 8 2026 onwards adjustments", func(t *testing.T) {
		tJul := time.Date(2026, time.July, 10, 12, 0, 0, 0, ptLocation)

		defaultAdj := getPGEAdjustments(tJul, "Default")
		onPeakAdj := getPGEAdjustments(tJul, "On-Peak")
		midPeakAdj := getPGEAdjustments(tJul, "Mid-Peak")
		offPeakAdj := getPGEAdjustments(tJul, "Off-Peak")

		assert.InDelta(t, 0.06693, defaultAdj, 1e-6)
		assert.InDelta(t, 0.13942, onPeakAdj, 1e-6)
		assert.InDelta(t, 0.06629, midPeakAdj, 1e-6)
		assert.InDelta(t, 0.04490, offPeakAdj, 1e-6)
	})

	t.Run("October 15 2026 adjustments", func(t *testing.T) {
		tOct := time.Date(2026, time.November, 16, 12, 0, 0, 0, ptLocation)

		defaultAdj := getPGEAdjustments(tOct, "Default")
		onPeakAdj := getPGEAdjustments(tOct, "On-Peak")
		midPeakAdj := getPGEAdjustments(tOct, "Mid-Peak")
		offPeakAdj := getPGEAdjustments(tOct, "Off-Peak")

		assert.InDelta(t, 0.08199, defaultAdj, 1e-6)
		assert.InDelta(t, 0.15448, onPeakAdj, 1e-6)
		assert.InDelta(t, 0.08135, midPeakAdj, 1e-6)
		assert.InDelta(t, 0.05996, offPeakAdj, 1e-6)
	})

	t.Run("2027 adjustments", func(t *testing.T) {
		t2027 := time.Date(2027, time.January, 18, 12, 0, 0, 0, ptLocation)

		defaultAdj := getPGEAdjustments(t2027, "Default")
		onPeakAdj := getPGEAdjustments(t2027, "On-Peak")
		midPeakAdj := getPGEAdjustments(t2027, "Mid-Peak")
		offPeakAdj := getPGEAdjustments(t2027, "Off-Peak")

		assert.InDelta(t, 0.07611, defaultAdj, 1e-6)
		assert.InDelta(t, 0.14422, onPeakAdj, 1e-6)
		assert.InDelta(t, 0.07555, midPeakAdj, 1e-6)
		assert.InDelta(t, 0.05540, offPeakAdj, 1e-6)
	})
}

func TestApplyPGEPublicPurposeCharge(t *testing.T) {
	t.Run("Before October 15 2026 unchanged", func(t *testing.T) {
		tJul := time.Date(2026, time.July, 10, 12, 0, 0, 0, ptLocation)
		assert.InDelta(t, 0.44205, applyPGEPublicPurposeCharge(tJul, 0.44205), 1e-7)
	})

	t.Run("October 15 2026 onward applies 1.5 percent excluding Schedule 109", func(t *testing.T) {
		tOct := time.Date(2026, time.October, 31, 12, 0, 0, 0, ptLocation)
		// Default Tier 1 pre-PPC = $0.19488/kWh; PPC = 0.015 * (0.19488 - 0.01004) = 0.0027726 -> $0.1976526/kWh
		assert.InDelta(t, 0.1976526, applyPGEPublicPurposeCharge(tOct, 0.19488), 1e-7)
	})
}

func TestPortlandGeneralElectric(t *testing.T) {
	u := &genericTOU{}
	err := u.ApplySettings(context.Background(), types.Settings{
		UtilityProvider: "portland_general_electric",
		UtilityRate:     "portland_general_electric_tod",
	})
	require.NoError(t, err)

	t.Run("2025 Weekday On-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2025, time.May, 19, 18, 0, 0, 0, ptLocation)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.44974, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Weekday On-Peak (May 2026)", func(t *testing.T) {
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])

		p, err := u.priceForTime(time.Date(2026, time.May, 18, 18, 0, 0, 0, ptLocation)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.44731, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Weekday Mid-Peak (May 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 10, 0, 0, 0, ptLocation)) // Monday 10:00 AM
		require.NoError(t, err)
		assert.InDelta(t, 0.17968, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Weekday Off-Peak evening (May 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 22, 0, 0, 0, ptLocation)) // Monday 10:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.10097, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Weekday Off-Peak morning (May 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 18, 4, 0, 0, 0, ptLocation)) // Monday 4:00 AM
		require.NoError(t, err)
		assert.InDelta(t, 0.10097, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Weekend Off-Peak (May 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 23, 14, 0, 0, 0, ptLocation)) // Saturday 2:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.10097, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Holiday Off-Peak (Memorial Day 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.May, 25, 18, 0, 0, 0, ptLocation)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.10097, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Holiday Weekend Shift (Independence Day observed July 3 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.July, 3, 18, 0, 0, 0, ptLocation)) // Friday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.10097, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Post-July 8 2026 Weekday On-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.July, 10, 18, 0, 0, 0, ptLocation)) // Friday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.44205, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Post-July 8 2026 Weekday Mid-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.July, 10, 10, 0, 0, 0, ptLocation)) // Friday 10:00 AM
		require.NoError(t, err)
		assert.InDelta(t, 0.17772, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Post-July 8 2026 Weekday Off-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.July, 10, 22, 0, 0, 0, ptLocation)) // Friday 10:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.10004, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Late 2026 Weekday On-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.November, 16, 18, 0, 0, 0, ptLocation)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.46382, p.DollarsPerKWH, 1e-5)
		assert.False(t, p.SeparateGenerationCredit)
	})

	t.Run("Late 2026 Weekday Mid-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.November, 16, 10, 0, 0, 0, ptLocation)) // Monday 10:00 AM
		require.NoError(t, err)
		assert.InDelta(t, 0.19552, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Late 2026 Weekday Off-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.November, 16, 22, 0, 0, 0, ptLocation)) // Monday 10:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.11668, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Late 2026 Holiday Off-Peak (Thanksgiving 2026)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2026, time.November, 26, 18, 0, 0, 0, ptLocation)) // Thursday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.11668, p.DollarsPerKWH, 1e-5)
	})

	t.Run("2027 Weekday On-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2027, time.January, 18, 18, 0, 0, 0, ptLocation)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.45289, p.DollarsPerKWH, 1e-5)
	})

	t.Run("2027 Weekday Mid-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2027, time.January, 18, 10, 0, 0, 0, ptLocation)) // Monday 10:00 AM
		require.NoError(t, err)
		assert.InDelta(t, 0.18943, p.DollarsPerKWH, 1e-5)
	})

	t.Run("2027 Weekday Off-Peak", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2027, time.January, 18, 22, 0, 0, 0, ptLocation)) // Monday 10:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.11195, p.DollarsPerKWH, 1e-5)
	})

	t.Run("2027 Holiday Off-Peak (Independence Day observed July 5 2027)", func(t *testing.T) {
		p, err := u.priceForTime(time.Date(2027, time.July, 5, 18, 0, 0, 0, ptLocation)) // Monday 6:00 PM
		require.NoError(t, err)
		assert.InDelta(t, 0.11195, p.DollarsPerKWH, 1e-5)
	})

	t.Run("Residential Service (Schedule 7)", func(t *testing.T) {
		res := &genericTOU{}
		err := res.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "portland_general_electric",
			UtilityRate:     "portland_general_electric_residential",
		})
		require.NoError(t, err)

		periods, err := res.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)

		t.Run("2025 Flat Rate", func(t *testing.T) {
			p, err := res.priceForTime(time.Date(2025, time.May, 19, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.18289, p.DollarsPerKWH, 1e-5)
		})

		t.Run("April 2026 Flat Rate", func(t *testing.T) {
			p, err := res.priceForTime(time.Date(2026, time.April, 15, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.18181, p.DollarsPerKWH, 1e-5)
		})

		t.Run("May 2026 Flat Rate", func(t *testing.T) {
			p, err := res.priceForTime(time.Date(2026, time.May, 18, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.18179, p.DollarsPerKWH, 1e-5)
		})

		t.Run("July 8 2026 Flat Rate", func(t *testing.T) {
			p, err := res.priceForTime(time.Date(2026, time.July, 10, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.17982, p.DollarsPerKWH, 1e-5)
		})

		t.Run("Late 2026 Flat Rate", func(t *testing.T) {
			pWeekday, err := res.priceForTime(time.Date(2026, time.November, 16, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.19765, pWeekday.DollarsPerKWH, 1e-5)
			assert.False(t, pWeekday.SeparateGenerationCredit)

			pWeekend, err := res.priceForTime(time.Date(2026, time.November, 21, 14, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.19765, pWeekend.DollarsPerKWH, 1e-5)
		})

		t.Run("2027 Flat Rate", func(t *testing.T) {
			pWeekday, err := res.priceForTime(time.Date(2027, time.January, 18, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.19149, pWeekday.DollarsPerKWH, 1e-5)
			assert.False(t, pWeekday.SeparateGenerationCredit)

			pHoliday, err := res.priceForTime(time.Date(2027, time.July, 5, 18, 0, 0, 0, ptLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.19149, pHoliday.DollarsPerKWH, 1e-5)
		})

		t.Run("Effective date transition boundaries", func(t *testing.T) {
			octPrices, err := res.GetConfirmedPrices(
				context.Background(),
				time.Date(2026, time.October, 14, 23, 0, 0, 0, ptLocation),
				time.Date(2026, time.October, 15, 1, 0, 0, 0, ptLocation),
			)
			require.NoError(t, err)
			if assert.Len(t, octPrices, 2) {
				assert.InDelta(t, 0.17982, octPrices[0].DollarsPerKWH, 1e-5)
				assert.InDelta(t, 0.19765, octPrices[1].DollarsPerKWH, 1e-5)
			}

			janPrices, err := res.GetConfirmedPrices(
				context.Background(),
				time.Date(2026, time.December, 31, 23, 0, 0, 0, ptLocation),
				time.Date(2027, time.January, 1, 1, 0, 0, 0, ptLocation),
			)
			require.NoError(t, err)
			if assert.Len(t, janPrices, 2) {
				assert.InDelta(t, 0.19765, janPrices[0].DollarsPerKWH, 1e-5)
				assert.InDelta(t, 0.19149, janPrices[1].DollarsPerKWH, 1e-5)
			}
		})
	})
}

func TestShiftPGEWeekendHoliday(t *testing.T) {
	t.Run("Saturday shifts to Friday", func(t *testing.T) {
		sat := time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC) // July 4, 2026 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 3, shifted.Day())
			assert.Equal(t, time.July, shifted.Month())
			assert.Equal(t, 2026, shifted.Year())
		}
	})

	t.Run("Sunday shifts to Monday", func(t *testing.T) {
		sun := time.Date(2027, time.July, 4, 0, 0, 0, 0, time.UTC) // July 4, 2027 is Sunday
		shifted := shiftPGEWeekendHoliday(sun)
		if assert.Equal(t, time.Monday, shifted.Weekday()) {
			assert.Equal(t, 5, shifted.Day())
			assert.Equal(t, time.July, shifted.Month())
			assert.Equal(t, 2027, shifted.Year())
		}
	})

	t.Run("Weekdays do not shift", func(t *testing.T) {
		weekdays := []struct {
			day  time.Time
			name string
		}{
			{time.Date(2026, time.July, 6, 0, 0, 0, 0, time.UTC), "Monday"},
			{time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC), "Tuesday"},
			{time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC), "Wednesday"},
			{time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC), "Thursday"},
			{time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC), "Friday"},
		}

		for _, wd := range weekdays {
			t.Run(wd.name, func(t *testing.T) {
				shifted := shiftPGEWeekendHoliday(wd.day)
				if assert.Equal(t, wd.day.Weekday(), shifted.Weekday()) {
					assert.Equal(t, wd.day.Day(), shifted.Day())
				}
			})
		}
	})

	t.Run("Saturday Jan 1 crossing to preceding Friday Dec 31", func(t *testing.T) {
		sat := time.Date(2022, time.January, 1, 0, 0, 0, 0, time.UTC) // Jan 1, 2022 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 31, shifted.Day())
			assert.Equal(t, time.December, shifted.Month())
			assert.Equal(t, 2021, shifted.Year())
		}
	})

	t.Run("Sunday Jan 1 crossing to Monday Jan 2", func(t *testing.T) {
		sun := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC) // Jan 1, 2023 is Sunday
		shifted := shiftPGEWeekendHoliday(sun)
		if assert.Equal(t, time.Monday, shifted.Weekday()) {
			assert.Equal(t, 2, shifted.Day())
			assert.Equal(t, time.January, shifted.Month())
			assert.Equal(t, 2023, shifted.Year())
		}
	})

	t.Run("Saturday May 1 crossing to preceding Friday Apr 30", func(t *testing.T) {
		sat := time.Date(2021, time.May, 1, 0, 0, 0, 0, time.UTC) // May 1, 2021 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 30, shifted.Day())
			assert.Equal(t, time.April, shifted.Month())
			assert.Equal(t, 2021, shifted.Year())
		}
	})

	t.Run("Saturday Feb 29 on leap year shifts to Friday Feb 28", func(t *testing.T) {
		sat := time.Date(2020, time.February, 29, 0, 0, 0, 0, time.UTC) // Feb 29, 2020 is Saturday
		shifted := shiftPGEWeekendHoliday(sat)
		if assert.Equal(t, time.Friday, shifted.Weekday()) {
			assert.Equal(t, 28, shifted.Day())
			assert.Equal(t, time.February, shifted.Month())
			assert.Equal(t, 2020, shifted.Year())
		}
	})
}
