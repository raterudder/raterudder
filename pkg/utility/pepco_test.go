package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPepcoDC(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	getPriceForTime := func(periods []types.UtilityFeesPeriod, target time.Time) types.Price {
		start := time.Date(target.Year(), target.Month(), target.Day(), target.Hour(), 0, 0, 0, target.Location())
		p := types.Price{
			Provider:      "tou",
			TSStart:       start,
			TSEnd:         start.Add(time.Hour),
			DollarsPerKWH: 0,
		}
		res, err := types.ApplyUtilityFeesPeriods(p, periods)
		require.NoError(t, err)
		return res
	}

	t.Run("pepco_dc_r residential rate", func(t *testing.T) {
		periods := pepcoDCPeriods("pepco_dc_r", types.UtilityRateOptions{}, []int{2026})

		// July 15, 2026 (Summer): $0.18930
		pSummer := getPriceForTime(periods, time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		assert.InDelta(t, 0.18930, pSummer.DollarsPerKWH, 1e-6)

		// January 15, 2026 (Winter): $0.19487
		pWinter := getPriceForTime(periods, time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		assert.InDelta(t, 0.19487, pWinter.DollarsPerKWH, 1e-6)
	})

	t.Run("GetPeriods TOU and non-TOU for Pepco DC", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pepco",
			UtilityRate:     "pepco_dc_r",
		})
		require.NoError(t, err)
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pepco",
			UtilityRate:     "pepco_dc_r_piv",
		})
		require.NoError(t, err)
		periods, err = u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.Equal(t, "On-Peak", periods[0].Name)
		}
	})

	t.Run("pepco_dc_r_piv plug-in vehicle rates and schedules", func(t *testing.T) {
		periods := pepcoDCPeriods("pepco_dc_r_piv", types.UtilityRateOptions{}, []int{2026})

		// Summer Weekday (Monday July 13, 2026)
		// 2 PM (On-Peak): $0.29314
		pSumOn := getPriceForTime(periods, time.Date(2026, time.July, 13, 14, 0, 0, 0, ny))
		assert.InDelta(t, 0.29314, pSumOn.DollarsPerKWH, 1e-6)

		// 8 AM (Off-Peak): $0.14707
		pSumOff := getPriceForTime(periods, time.Date(2026, time.July, 13, 8, 0, 0, 0, ny))
		assert.InDelta(t, 0.14707, pSumOff.DollarsPerKWH, 1e-6)

		// Winter Weekday (Monday January 12, 2026)
		// 2 PM (On-Peak): $0.36010
		pWinOn := getPriceForTime(periods, time.Date(2026, time.January, 12, 14, 0, 0, 0, ny))
		assert.InDelta(t, 0.36010, pWinOn.DollarsPerKWH, 1e-6)

		// 8 AM (Off-Peak): $0.14870
		pWinOff := getPriceForTime(periods, time.Date(2026, time.January, 12, 8, 0, 0, 0, ny))
		assert.InDelta(t, 0.14870, pWinOff.DollarsPerKWH, 1e-6)

		// Summer Weekend (Saturday July 18, 2026)
		// 2 PM (Off-Peak): $0.14707
		pSumWE := getPriceForTime(periods, time.Date(2026, time.July, 18, 14, 0, 0, 0, ny))
		assert.InDelta(t, 0.14707, pSumWE.DollarsPerKWH, 1e-6)

		// Summer Holiday (Independence Day Friday July 3, 2026)
		// 2 PM (Off-Peak): $0.14707
		pSumHol := getPriceForTime(periods, time.Date(2026, time.July, 3, 14, 0, 0, 0, ny))
		assert.InDelta(t, 0.14707, pSumHol.DollarsPerKWH, 1e-6)
	})

	t.Run("Applying Settings via genericTOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pepco_dc",
			UtilityRate:     "pepco_dc_r_piv",
		})
		require.NoError(t, err)
		assert.Equal(t, "pepco_dc_r_piv", u.Name())

		p, err := u.priceForTime(time.Date(2026, time.July, 13, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.29314, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)
	})
}
