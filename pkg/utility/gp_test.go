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

	t.Run("TOU-OA-14", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_oa_14",
		})
		require.NoError(t, err)

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM) -> 29.7868¢
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.297868, p.DollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM) -> 10.1676¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.101676, p.DollarsPerKWH, 1e-6)

		// 3. Summer weekday Super Off-Peak (Wednesday, July 15, 2026 at 2:00 AM) -> 2.1859¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.021859, p.DollarsPerKWH, 1e-6)

		// 4. Summer weekend Off-Peak (Saturday, July 18, 2026 at 3:00 PM) -> 10.1676¢
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.101676, p.DollarsPerKWH, 1e-6)

		// 5. Summer Holiday Off-Peak (Labor Day Monday, Sep 7, 2026 at 3:00 PM) -> 10.1676¢
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.101676, p.DollarsPerKWH, 1e-6)

		// 6. Winter daily Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM) -> 10.1676¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.101676, p.DollarsPerKWH, 1e-6)

		// 7. Winter daily Super Off-Peak (Tuesday, Dec 15, 2026 at 2:00 AM) -> 2.1859¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.021859, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU-RD-11", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_rd_11",
		})
		require.NoError(t, err)

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM) -> 14.2986¢
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.142986, p.DollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM) -> 1.5288¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.015288, p.DollarsPerKWH, 1e-6)

		// 3. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM) -> 1.5288¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.015288, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU-REO-18", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "gp",
			UtilityRate:     "gp_tou_reo_18",
		})
		require.NoError(t, err)

		// 1. Summer weekday On-Peak (Wednesday, July 15, 2026 at 3:00 PM) -> 29.7868¢
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.297868, p.DollarsPerKWH, 1e-6)

		// 2. Summer weekday Off-Peak (Wednesday, July 15, 2026 at 10:00 AM) -> 7.6281¢
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.076281, p.DollarsPerKWH, 1e-6)

		// 3. Winter weekday Off-Peak (Tuesday, Dec 15, 2026 at 3:00 PM) -> 7.6281¢
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.076281, p.DollarsPerKWH, 1e-6)
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
			UtilityRate:     "gp_tou_oa_14",
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
			UtilityRate:     "gp_tou_oa_14",
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
			UtilityRate:     "gp_tou_oa_14",
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
