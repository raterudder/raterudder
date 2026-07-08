package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSouthernCaliforniaEdison(t *testing.T) {
	t.Run("TOU-D-PRIME with SBP (NEM 3.0)", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
			},
		})
		require.NoError(t, err)

		// 1. Summer Weekday On-Peak (e.g. Wednesday, July 15, 2026, 5:00 PM)
		// Expected Retail: 59¢ -> Base = 59¢ - 2.74¢ = 56.26¢ = 0.5626, NBC = 2.74¢ = 0.0274
		targetTime := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)
		assert.InDelta(t, 0.5626, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0274, p.GridUseDollarsPerKWH, 1e-6)
		if assert.True(t, p.SeparateGenerationCredit) {
			// GenerationCredit should equal NBT rate (Jul_Weekday_17 for 2026) -> 0.28366
			assert.InDelta(t, 0.28366, p.GenerationCreditDollarsPerKWH, 1e-5)
		}
	})

	t.Run("TOU-D-PRIME with NEM 2.0", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		})
		require.NoError(t, err)

		targetTime := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)
		assert.InDelta(t, 0.5626, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0274, p.GridUseDollarsPerKWH, 1e-6)
		if assert.False(t, p.SeparateGenerationCredit) {
			assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)
		}
	})

	t.Run("TOU-D-4-9PM with NEM 1.0", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_4_9pm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Summer Weekday On-Peak: 58¢ retail -> Base = 55.26¢ = 0.5526, NBC = 0.0274
		targetTime := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		p, err := u.priceForTime(targetTime)
		require.NoError(t, err)
		assert.InDelta(t, 0.5526, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0274, p.GridUseDollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)
	})
}

func TestSCEHolidays(t *testing.T) {
	t.Run("Check designated holidays for 2026", func(t *testing.T) {
		holidays := getSCEHolidays(2026)
		// Check that the 8 expected holidays exist
		assert.Contains(t, holidays, "2026-01-01")
		assert.Contains(t, holidays, "2026-02-16")
		assert.Contains(t, holidays, "2026-05-25")
		assert.Contains(t, holidays, "2026-07-04")
		assert.Contains(t, holidays, "2026-09-07")
		assert.Contains(t, holidays, "2026-11-11")
		assert.Contains(t, holidays, "2026-11-26")
		assert.Contains(t, holidays, "2026-12-25")
	})

	t.Run("Sunday holiday shifts to Monday", func(t *testing.T) {
		holidays := getSCEHolidays(2027)
		if assert.Contains(t, holidays, "2027-07-05") {
			assert.NotContains(t, holidays, "2027-07-04")
		}
	})
}

func TestGetSCENBTExportRate(t *testing.T) {
	t.Run("Get rate for regular weekday", func(t *testing.T) {
		target := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		rate := getSCENBTExportRate(target)
		assert.InDelta(t, 0.28366, rate, 1e-6)
	})

	t.Run("Get rate for weekend", func(t *testing.T) {
		target := time.Date(2026, time.July, 18, 17, 0, 0, 0, ptLocation)
		rate := getSCENBTExportRate(target)
		assert.InDelta(t, 0.13600, rate, 1e-6)
	})

	t.Run("Get rate for holiday on weekday", func(t *testing.T) {
		target := time.Date(2026, time.January, 1, 17, 0, 0, 0, ptLocation)
		rate := getSCENBTExportRate(target)
		assert.InDelta(t, 0.10548, rate, 1e-6)
	})
}
