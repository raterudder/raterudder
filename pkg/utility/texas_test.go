package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTexasUtility(t *testing.T) {
	t.Run("Tesla Electric Dynamic - CenterPoint", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "centerpoint",
			},
		})
		require.NoError(t, err)

		// On-Peak (6:00 PM - 9:00 PM): 7:00 PM
		pPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 19, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1800, pPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06533, pPeak.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.24533, pPeak.ImportRateDollars(), 1e-6)
		assert.True(t, pPeak.SeparateGenerationCredit)
		assert.InDelta(t, 0.2500, pPeak.GenerationCreditDollarsPerKWH, 1e-6)

		// Off-Peak (other hours): 2:00 PM
		pOffPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1200, pOffPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06533, pOffPeak.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.18533, pOffPeak.ImportRateDollars(), 1e-6)
		assert.True(t, pOffPeak.SeparateGenerationCredit)
		assert.InDelta(t, 0.0800, pOffPeak.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Tesla Electric Fixed - Oncor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_fixed",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "oncor",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.March, 10, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1150, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06867, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.18367, p.ImportRateDollars(), 1e-6)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0900, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Green Mountain Free Nights - CenterPoint", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "green_mountain_tx",
			UtilityRate:     "green_mountain_free_nights",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "centerpoint",
			},
		})
		require.NoError(t, err)

		// Free Night window (8:00 PM - 6:00 AM): 11:00 PM
		pNight, err := u.priceForTime(time.Date(2026, time.June, 20, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0000, pNight.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0000, pNight.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.0000, pNight.ImportRateDollars(), 1e-6)

		// Free Night early morning: 3:00 AM
		pMorning, err := u.priceForTime(time.Date(2026, time.June, 21, 3, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0000, pMorning.ImportRateDollars(), 1e-6)

		// Daytime (6:00 AM - 8:00 PM): 1:00 PM
		pDay, err := u.priceForTime(time.Date(2026, time.June, 20, 13, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1950, pDay.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06533, pDay.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.26033, pDay.ImportRateDollars(), 1e-6)
		assert.True(t, pDay.SeparateGenerationCredit)
		assert.InDelta(t, 0.0500, pDay.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Direct Energy Twelve Hour Power - Oncor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "direct_energy_tx",
			UtilityRate:     "direct_energy_twelve_hour",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "oncor",
			},
		})
		require.NoError(t, err)

		// Free Window (9:00 PM - 9:00 AM): 10:00 PM
		pNight, err := u.priceForTime(time.Date(2026, time.September, 10, 22, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0000, pNight.ImportRateDollars(), 1e-6)

		// Free Window morning: 7:00 AM
		pMorning, err := u.priceForTime(time.Date(2026, time.September, 11, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0000, pMorning.ImportRateDollars(), 1e-6)

		// Daytime (9:00 AM - 9:00 PM): 2:00 PM
		pDay, err := u.priceForTime(time.Date(2026, time.September, 10, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2150, pDay.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06867, pDay.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.28367, pDay.ImportRateDollars(), 1e-6)
		assert.True(t, pDay.SeparateGenerationCredit)
		assert.InDelta(t, 0.0300, pDay.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("TXU Energy Free Nights - AEP Texas Central", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "txu_tx",
			UtilityRate:     "txu_free_nights",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "aep_central",
			},
		})
		require.NoError(t, err)

		// Free Night (8:00 PM - 6:00 AM): 1:00 AM
		pNight, err := u.priceForTime(time.Date(2026, time.August, 5, 1, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0000, pNight.ImportRateDollars(), 1e-6)

		// Daytime (6:00 AM - 8:00 PM): 11:00 AM
		pDay, err := u.priceForTime(time.Date(2026, time.August, 5, 11, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2050, pDay.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.05690, pDay.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.26190, pDay.ImportRateDollars(), 1e-6)
		assert.True(t, pDay.SeparateGenerationCredit)
		assert.InDelta(t, 0.0400, pDay.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Map Site Integration for Tesla Dynamic", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		m := Configured(mockDB)

		// Query site with Tesla Electric Dynamic on CenterPoint
		provider, err := m.Site(context.Background(), "test-site-tx", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "centerpoint",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, provider)

		// Verified that it returned a SiteFees wrapper
		siteFees, ok := provider.(*SiteFees)
		require.True(t, ok, "tesla_dynamic should be wrapped in SiteFees")
		assert.Equal(t, "tesla_dynamic", siteFees.Name())

		// Verify that SiteFees applies GenerationCreditDollarsPerKWHPreMultiple (0.90) to wholesale price
		rawWholesalePrice := types.Price{
			TSStart:                       time.Date(2026, time.July, 15, 17, 0, 0, 0, ctLocation),
			TSEnd:                         time.Date(2026, time.July, 15, 18, 0, 0, 0, ctLocation),
			DollarsPerKWH:                 2.50,
			SeparateGenerationCredit:      true,
			GenerationCreditDollarsPerKWH: 2.50,
		}
		finalPrice, err := siteFees.applyFees(rawWholesalePrice)
		require.NoError(t, err)
		assert.InDelta(t, 2.25, finalPrice.GenerationCreditDollarsPerKWH, 1e-6) // 90% of $2.50
		assert.InDelta(t, 0.06533, finalPrice.GridUseDollarsPerKWH, 1e-6)       // CenterPoint delivery
	})

	t.Run("Tesla Electric Dynamic - Missing or Invalid TDU", func(t *testing.T) {
		mockDB := &storagemock.MockDatabase{}
		m := Configured(mockDB)

		// Missing location
		_, err := m.Site(context.Background(), "test-site-empty", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "tdu location is required to determine ercot load zone")

		// Invalid location
		_, err = m.Site(context.Background(), "test-site-invalid", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "nonexistent_tdu",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown TDU delivery utility")
	})
}
