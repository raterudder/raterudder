package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacificGasAndElectric(t *testing.T) {
	t.Run("E-1 flat rate calculation", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		targetTime := time.Date(2026, time.July, 15, 12, 0, 0, 0, ptLocation)
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)

		// Retail rate = $0.32561.
		// NBC = $0.01230.
		// DollarsPerKWH = $0.32561 - $0.01230 = $0.31331.
		// GridUseDollarsPerKWH = $0.01230.
		assert.InDelta(t, 0.31331, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01230, p.GridUseDollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)
	})

	t.Run("E-TOU-C summer peak with baseline credit", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_c",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
			},
		})
		require.NoError(t, err)

		// Summer On-Peak: July 15, 2026, 5:00 PM (17:00).
		// Peak = $0.52240. Baseline credit = $0.08140. NBC = $0.01230.
		// DollarsPerKWH = 0.52240 - 0.08140 - 0.01230 = 0.42870.
		targetTime := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)

		assert.InDelta(t, 0.42870, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01230, p.GridUseDollarsPerKWH, 1e-6)
		assert.True(t, p.SeparateGenerationCredit)

		// Expected generation credit should be set to the real July Weekday Hour 17 rate (0.32547)
		assert.InDelta(t, 0.32547, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("E-TOU-D winter holiday weekday", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_tou_d",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		})
		require.NoError(t, err)

		// New Year's Day 2026 (Jan 1) is a Thursday.
		// Winter Off-Peak: $0.34886. NBC = $0.01230.
		// DollarsPerKWH = 0.34886 - 0.01230 = 0.33656.
		targetTime := time.Date(2026, time.January, 1, 17, 0, 0, 0, ptLocation) // 5 PM on holiday
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)

		assert.InDelta(t, 0.33656, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01230, p.GridUseDollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)
	})

	t.Run("E-ELEC winter part-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_e_elec",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		})
		require.NoError(t, err)

		// Winter Part-Peak: Dec 15, 2026, 3:00 PM (15:00).
		// Part-Peak = $0.29854. NBC = $0.01230.
		// DollarsPerKWH = 0.29854 - 0.01230 = 0.28624.
		targetTime := time.Date(2026, time.December, 15, 15, 0, 0, 0, ptLocation)
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)

		assert.InDelta(t, 0.28624, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01230, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("EV2 summer peak vs off-peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "pg_e",
			UtilityRate:     "pg_e_ev2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Summer On-Peak: July 15, 2026, 6:00 PM (18:00).
		// Peak = $0.53809. NBC = $0.01230.
		// DollarsPerKWH = 0.53809 - 0.01230 = 0.52579.
		tPeak := time.Date(2026, time.July, 15, 18, 0, 0, 0, ptLocation)
		pPeak, err := u.priceForTime(tPeak)
		require.NoError(t, err)
		assert.InDelta(t, 0.52579, pPeak.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: July 15, 2026, 8:00 AM (08:00).
		// Off-Peak = $0.22558. NBC = $0.01230.
		// DollarsPerKWH = 0.22558 - 0.01230 = 0.21328.
		tOff := time.Date(2026, time.July, 15, 8, 0, 0, 0, ptLocation)
		pOff, err := u.priceForTime(tOff)
		require.NoError(t, err)
		assert.InDelta(t, 0.21328, pOff.DollarsPerKWH, 1e-6)
	})
}

func TestPGEHolidays(t *testing.T) {
	t.Run("designated holidays for 2026", func(t *testing.T) {
		holidays := getPGEHolidays(2026)
		assert.Contains(t, holidays, "2026-01-01") // New Year's Day
		assert.Contains(t, holidays, "2026-02-16") // Presidents' Day
		assert.Contains(t, holidays, "2026-05-25") // Memorial Day
		assert.Contains(t, holidays, "2026-07-03") // July 4 (Saturday) shifted to Friday July 3
		assert.Contains(t, holidays, "2026-09-07") // Labor Day
		assert.Contains(t, holidays, "2026-11-11") // Veterans Day
		assert.Contains(t, holidays, "2026-11-26") // Thanksgiving Day
		assert.Contains(t, holidays, "2026-12-25") // Christmas Day
	})

	t.Run("holiday weekend shifts", func(t *testing.T) {
		// In 2027, July 4 is a Sunday. It should shift to Monday, July 5.
		h2027 := getPGEHolidays(2027)
		assert.Contains(t, h2027, "2027-07-05")
		assert.NotContains(t, h2027, "2027-07-04")

		// In 2027, Dec 25 is a Saturday. It should shift to Friday, Dec 24.
		assert.Contains(t, h2027, "2027-12-24")
		assert.NotContains(t, h2027, "2027-12-25")
	})
}

func TestGetPGENBTExportRate(t *testing.T) {
	t.Run("Get rate for regular weekday", func(t *testing.T) {
		target := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		rate := getPGENBTExportRate(target)
		assert.InDelta(t, 0.32547, rate, 1e-6)
	})

	t.Run("Get rate for weekend", func(t *testing.T) {
		target := time.Date(2026, time.July, 18, 17, 0, 0, 0, ptLocation)
		rate := getPGENBTExportRate(target)
		assert.InDelta(t, 0.05085, rate, 1e-6)
	})

	t.Run("Get rate for holiday on weekday", func(t *testing.T) {
		target := time.Date(2026, time.January, 1, 17, 0, 0, 0, ptLocation)
		rate := getPGENBTExportRate(target)
		assert.InDelta(t, 0.10588, rate, 1e-6)
	})
}
