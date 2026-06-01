package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPHolidays(t *testing.T) {
	t.Run("Independence Day observed shifts", func(t *testing.T) {
		// 2026 July 4th is Saturday -> observed Friday July 3rd
		holidays2026 := getGPHolidays(2026)
		assert.Contains(t, holidays2026, "2026-07-03")
		assert.NotContains(t, holidays2026, "2026-07-04")

		// 2027 July 4th is Sunday -> observed Monday July 5th
		holidays2027 := getGPHolidays(2027)
		assert.Contains(t, holidays2027, "2027-07-05")
		assert.NotContains(t, holidays2027, "2027-07-04")

		// 2025 July 4th is Friday -> remains Friday July 4th
		holidays2025 := getGPHolidays(2025)
		assert.Contains(t, holidays2025, "2025-07-04")
	})

	t.Run("Labor Day first Monday in September", func(t *testing.T) {
		// 2026 Labor Day is Sep 7
		holidays2026 := getGPHolidays(2026)
		assert.Contains(t, holidays2026, "2026-09-07")

		// 2027 Labor Day is Sep 6
		holidays2027 := getGPHolidays(2027)
		assert.Contains(t, holidays2027, "2027-09-06")
	})
}

func TestGeorgiaPowerRates(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	u := &genericTOU{}
	const mffMultiplier = 1.011995

	t.Run("TOU-OA (Overnight Advantage) New ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
		})
		require.NoError(t, err)

		// --- Regime 2 (On/After June 1, 2026): ECCR-15p (13.0205% surcharge), FCR-27 ($0.052269 / $0.038690 / $0.034747) ---

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM)
		// Base: 30.3495¢ * 1.130205 = 34.3011¢
		// FCR (GridUse): 5.2269¢ = $0.052269
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
		// Base: 10.3598¢ * 1.130205 = 11.7087¢
		// FCR (GridUse): 3.8690¢ = $0.038690
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 3. Summer weekday Super Off-Peak (Wednesday, July 15, 2026 at 2:00 AM)
		// Base: 2.2272¢ * 1.130205 = 2.51719¢
		// FCR (GridUse): 3.4747¢ = $0.034747
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.022272*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.034747*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 4. Summer weekend Off-Peak (Saturday, July 18, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 5. Summer Holiday Off-Peak (Labor Day Monday, Sep 7, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 6. Winter daily Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.103598*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038690*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 7. Winter daily Super Off-Peak (Tuesday, Dec 15, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.022272*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.034747*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// --- Regime 1 (Before June 1, 2026): ECCR-14 (13.2343% surcharge), FCR-26 ($0.044284 / $0.038252) ---

		// 8. Winter weekday Off-Peak (Wednesday, January 15, 2026 at 3:00 PM)
		// Base: 10.1676¢ * 1.132343 = 11.5132¢
		// FCR (GridUse): 4.4284¢ = $0.044284
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.101676*1.132343*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.044284*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 9. Winter weekday Super Off-Peak (Wednesday, January 15, 2026 at 2:00 AM)
		// Base: 2.1859¢ * 1.132343 = 2.47519¢
		// FCR (GridUse): 3.8252¢ = $0.038252
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.021859*1.132343*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.038252*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("TOU-RD (Residential Demand) New ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_rd",
		})
		require.NoError(t, err)

		// --- Regime 2 ---

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM)
		// Base: 14.5620¢ * 1.130205 = 16.4580¢
		// FCR (GridUse): 5.2269¢ = $0.052269
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.145620*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
		// Base: 1.5569¢ * 1.130205 = 1.7596¢
		// FCR (GridUse): 3.7441¢ = $0.037441
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.015569*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 3. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.015569*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// --- Regime 1 ---

		// 4. Winter weekday Off-Peak (Wednesday, January 15, 2026 at 3:00 PM)
		// Base: 1.5288¢ * 1.132343 = 1.73113¢
		// FCR (GridUse): 4.2398¢ = $0.042398
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.015288*1.132343*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042398*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("TOU-REO (Nights & Weekends) New ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_reo",
		})
		require.NoError(t, err)

		// --- Regime 2 ---

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM)
		// Base: 30.3495¢ * 1.130205 = 34.3011¢
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM)
		// Base: 7.7702¢ * 1.130205 = 8.7819¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.077702*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// 3. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.077702*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.037441*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)

		// --- Regime 1 ---

		// 4. Winter weekday Off-Peak (Wednesday, January 15, 2026 at 3:00 PM)
		// Base: 7.6281¢ * 1.132343 = 8.6376¢
		// FCR (GridUse): 4.2398¢ = $0.042398
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.076281*1.132343*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.042398*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Backwards Compatibility: TOU-OA-14 Old ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa_14",
		})
		require.NoError(t, err)

		// Summer weekday On-Peak should work identical to gp_tou_oa
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Backwards Compatibility: TOU-RD-11 Old ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_rd_11",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.145620*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})

	t.Run("Backwards Compatibility: TOU-REO-18 Old ID", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_reo_18",
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.303495*1.130205*mffMultiplier, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.052269*mffMultiplier, p.GridUseDollarsPerKWH, 1e-6)
	})
}

func TestGPExportRates(t *testing.T) {
	ny, err := time.LoadLocation("America/Low_Angeles") // load timezone
	if err != nil {
		ny, _ = time.LoadLocation("America/New_York")
	}

	u := &genericTOU{}

	t.Run("RNR-Instantaneous Netting 2026", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "gp_instantaneous",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 15, 0, 0, 0, ny)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.07219, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("RNR-Instantaneous Netting 2027", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "gp_instantaneous",
			},
		})
		require.NoError(t, err)

		target := time.Date(2027, time.July, 15, 15, 0, 0, 0, ny)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.07471, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("RNR-Monthly Netting", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		target := time.Date(2026, time.July, 15, 15, 0, 0, 0, ny)
		p, err := u.priceForTime(target)
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
		assert.Equal(t, 0.0, p.GenerationCreditDollarsPerKWH)
	})
}
