package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShiftEversourceSundayHoliday(t *testing.T) {
	t.Run("Sunday shifts to Monday", func(t *testing.T) {
		sunday := time.Date(2027, time.July, 4, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(sunday)
		expected := time.Date(2027, time.July, 5, 0, 0, 0, 0, time.UTC)
		assert.Equal(t, expected, shifted)
	})

	t.Run("Saturday does not shift", func(t *testing.T) {
		saturday := time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(saturday)
		assert.Equal(t, saturday, shifted)
	})

	t.Run("Weekday does not shift", func(t *testing.T) {
		wednesday := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
		shifted := shiftEversourceSundayHoliday(wednesday)
		assert.Equal(t, wednesday, shifted)
	})
}

func TestGetEversourceHolidays(t *testing.T) {
	t.Run("Holidays for 2027 contains shifted Independence Day", func(t *testing.T) {
		holidays := getEversourceHolidays(2027)
		// July 4, 2027 is a Sunday, so it shifts to July 5, 2027
		assert.Contains(t, holidays, "2027-07-05")
		assert.NotContains(t, holidays, "2027-07-04")
	})

	t.Run("Holidays for 2026 contains Saturday Independence Day (no shift)", func(t *testing.T) {
		holidays := getEversourceHolidays(2026)
		// July 4, 2026 is Saturday, so it does not shift
		assert.Contains(t, holidays, "2026-07-04")
	})
}

func TestEversourceUtilityInfo(t *testing.T) {
	etLoc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	u := &genericTOU{}

	t.Run("CT Residential Flat Rate 1", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_1",
		})
		require.NoError(t, err)

		// Check any time of the year (flat rate)
		target := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLoc)
		p, err := u.priceForTime(target)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24666, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential Heating Rate 5", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_5",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLoc)
		p, err := u.priceForTime(target)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.22277, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("CT Residential TOU Rate 7", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_rate_7",
		})
		require.NoError(t, err)

		// On-peak: Weekdays 12 Noon - 8 p.m.
		onPeak := time.Date(2026, time.June, 15, 13, 0, 0, 0, etLoc) // Monday 1 PM
		pOn, err := u.priceForTime(onPeak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31099, pOn.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOn.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekdays other hours
		offPeak := time.Date(2026, time.June, 15, 9, 0, 0, 0, etLoc) // Monday 9 AM
		pOff, err := u.priceForTime(offPeak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21871, pOff.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pOff.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekend
		weekend := time.Date(2026, time.June, 14, 13, 0, 0, 0, etLoc) // Sunday 1 PM
		pWe, err := u.priceForTime(weekend)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21871, pWe.DollarsPerKWH, 1e-6)
			assert.InDelta(t, -0.0402, pWe.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH Residential Standard Rate R", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLoc)
		p, err := u.priceForTime(target)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.23128, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH Residential TOU Rate R-OTOD", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_otod",
		})
		require.NoError(t, err)

		// On-peak: Weekdays 1 PM - 7 PM (excluding holidays)
		onPeak := time.Date(2026, time.June, 15, 14, 0, 0, 0, etLoc) // Monday 2 PM
		pOn, err := u.priceForTime(onPeak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.34792, pOn.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekday morning
		offPeak := time.Date(2026, time.June, 15, 10, 0, 0, 0, etLoc) // Monday 10 AM
		pOff, err := u.priceForTime(offPeak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19583, pOff.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Weekend
		weekend := time.Date(2026, time.June, 14, 14, 0, 0, 0, etLoc) // Sunday 2 PM
		pWe, err := u.priceForTime(weekend)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19583, pWe.DollarsPerKWH, 1e-6)
		}

		// Off-peak: Holiday (e.g. New Year's Day, Jan 1st 2026 is Thursday)
		holiday := time.Date(2026, time.January, 1, 14, 0, 0, 0, etLoc) // 2 PM
		pHol, err := u.priceForTime(holiday)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.19583, pHol.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("NH Residential Rate R-EV", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_nh_rate_r_ev",
		})
		require.NoError(t, err)

		// On-Peak: Weekdays 2 p.m. – 7 p.m. excluding holidays
		onPeak := time.Date(2026, time.June, 15, 15, 0, 0, 0, etLoc) // Monday 3 PM
		pOn, err := u.priceForTime(onPeak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.33000, pOn.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Weekdays 7 a.m. – 2 p.m.
		midPeak1 := time.Date(2026, time.June, 15, 10, 0, 0, 0, etLoc) // Monday 10 AM
		pMid1, err := u.priceForTime(midPeak1)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMid1.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Weekdays 7 p.m. – 11 p.m.
		midPeak2 := time.Date(2026, time.June, 15, 20, 0, 0, 0, etLoc) // Monday 8 PM
		pMid2, err := u.priceForTime(midPeak2)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMid2.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Weekends 7 a.m. – 11 p.m.
		midPeakWe := time.Date(2026, time.June, 14, 12, 0, 0, 0, etLoc) // Sunday 12 PM
		pMidWe, err := u.priceForTime(midPeakWe)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMidWe.DollarsPerKWH, 1e-6)
		}

		// Mid-Peak: Holidays 7 a.m. – 11 p.m.
		midPeakHol := time.Date(2026, time.January, 1, 12, 0, 0, 0, etLoc) // Jan 1st 12 PM
		pMidHol, err := u.priceForTime(midPeakHol)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.21000, pMidHol.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: Daily 11 p.m. – 7 a.m.
		offPeak1 := time.Date(2026, time.June, 15, 2, 0, 0, 0, etLoc) // Monday 2 AM
		pOff1, err := u.priceForTime(offPeak1)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11000, pOff1.DollarsPerKWH, 1e-6)
		}

		offPeakWe := time.Date(2026, time.June, 14, 2, 0, 0, 0, etLoc) // Sunday 2 AM
		pOffWe, err := u.priceForTime(offPeakWe)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11000, pOffWe.DollarsPerKWH, 1e-6)
		}

		offPeakHol := time.Date(2026, time.January, 1, 2, 0, 0, 0, etLoc) // Holiday 2 AM
		pOffHol, err := u.priceForTime(offPeakHol)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11000, pOffHol.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("MA Residential Standard Rate R-1", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ma_residential",
		})
		require.NoError(t, err)

		target := time.Date(2026, time.June, 15, 12, 0, 0, 0, etLoc)
		p, err := u.priceForTime(target)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.30471, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p.GenerationAdjustmentDollarsPerKWH, 1e-6)
		}
	})
}
