package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTEPHolidays(t *testing.T) {
	t.Run("2026 observed holidays", func(t *testing.T) {
		// In 2026: July 4 is Saturday -> observed Friday July 3
		holidays2026 := getTEPHolidays(2026)
		assert.Contains(t, holidays2026, "2026-01-01")
		assert.Contains(t, holidays2026, "2026-05-25")
		assert.Contains(t, holidays2026, "2026-07-03")
		assert.NotContains(t, holidays2026, "2026-07-04")
		assert.Contains(t, holidays2026, "2026-09-07")
		assert.Contains(t, holidays2026, "2026-11-26")
		assert.Contains(t, holidays2026, "2026-12-25")
		// Veterans Day and MLK Day are not off-peak holidays on TEP tariffs
		assert.NotContains(t, holidays2026, "2026-01-19")
		assert.NotContains(t, holidays2026, "2026-11-11")
	})

	t.Run("Saturday New Year's Day shifts into preceding December", func(t *testing.T) {
		// Jan 1, 2028 is Saturday -> observed Friday Dec 31, 2027
		holidays2027 := getTEPHolidays(2027)
		assert.Contains(t, holidays2027, "2027-12-31")
		// Christmas 2027 (Dec 25) is Saturday -> observed Friday Dec 24, 2027
		assert.Contains(t, holidays2027, "2027-12-24")
		assert.NotContains(t, holidays2027, "2027-12-25")
	})

	t.Run("Sunday holiday shifts to Monday", func(t *testing.T) {
		// July 4, 2027 is Sunday -> observed Monday July 5, 2027
		holidays2027 := getTEPHolidays(2027)
		assert.Contains(t, holidays2027, "2027-07-05")
		assert.NotContains(t, holidays2027, "2027-07-04")
	})
}

func TestTEPPeriods(t *testing.T) {
	u := &genericTOU{}

	t.Run("Demand TOU (TRRESDT) rates and windows", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trresdt",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])

		// Summer weekday On-Peak: Wed, July 15, 2026 at 3:00 PM (15:00) -> $0.044556 + $0.099166 = $0.143722
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.143722, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Summer weekday Off-Peak before 3 PM: Wed, July 15, 2026 at 2:00 PM (14:00) -> $0.044556 + $0.028296 = $0.072852
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.072852, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Summer weekday Off-Peak at 7 PM: Wed, July 15, 2026 at 7:00 PM (19:00) -> $0.072852
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 19, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.072852, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Summer weekend: Sat, July 18, 2026 at 4:00 PM -> Off-Peak $0.072852
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.072852, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Summer observed Independence Day holiday: Fri, July 3, 2026 at 4:00 PM -> Off-Peak $0.072852
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 16, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.072852, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// October is Winter on TEP! Thu, Oct 15, 2026:
		// Morning On-Peak 6:00 AM - 9:00 AM -> $0.044556 + $0.039694 = $0.084250
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 7, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.084250, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Midday Off-Peak 12:00 PM -> $0.044556 + $0.031266 = $0.075822
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.075822, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Evening On-Peak 6:00 PM - 9:00 PM -> $0.084250
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 19, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.084250, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Winter Thanksgiving Holiday: Thu, Nov 26, 2026 at 7:00 PM -> Off-Peak $0.075822
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 19, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.075822, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Veterans Day is NOT a TEP off-peak holiday: Wed, Nov 11, 2026 at 7:00 PM -> On-Peak $0.084250
		p, err = u.priceForTime(time.Date(2026, time.November, 11, 19, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.084250, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)
	})

	t.Run("Time-of-Use (TRREST) rates", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trrest",
		})
		require.NoError(t, err)

		// Summer On-Peak: $0.093556 + $0.099166 = $0.192722
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.192722, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: $0.093556 + $0.028296 = $0.121852
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.121852, p.DollarsPerKWH, 1e-6)

		// Winter On-Peak: $0.093556 + $0.039694 = $0.133250
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 7, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.133250, p.DollarsPerKWH, 1e-6)

		// Winter Off-Peak: $0.093556 + $0.031266 = $0.124822
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.124822, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Peak Demand (TRRESD) rates and demand period names", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trresd",
		})
		require.NoError(t, err)

		// Summer On-Peak: $0.044556 + $0.043711 = $0.088267
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.088267, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Summer Off-Peak: $0.088267
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.088267, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Winter On-Peak: $0.044556 + $0.039632 = $0.084188
		p, err = u.priceForTime(time.Date(2026, time.December, 16, 19, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.084188, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "On-Peak", p.PeriodName)
	})

	t.Run("Basic (TRRES) rates", func(t *testing.T) {
		// Summer $0.086720 + $0.043711 = $0.130431, Winter $0.086720 + $0.039632 = $0.126352
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trres",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.130431, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 16, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.126352, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Rider-14 RCP Export Cohorts vs Rider-4 Net Metering", func(t *testing.T) {
		// Default RCP ($0.0462/kWh)
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trresdt",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0462, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Locked Oct 2025 - Sep 2026 RCP cohort ($0.0513/kWh)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trresdt",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rcp_2025",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0513, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Grandfathered Rider-4 1:1 Net Metering
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tep",
			UtilityRate:     "tep_trresdt",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.November, 12, 12, 0, 0, 0, mstLocation))
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)
	})
}
