package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRutherfordElectricUtility(t *testing.T) {
	ctx := context.Background()

	t.Run("May to September Summer Rates", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		}))
		assert.Equal(t, "rutherford_electric_tod", u.Name())
		assert.NotEmpty(t, u.periods)

		// Weekday On-Peak (1:00 PM - 7:00 PM): Wed Jul 15, 2026 at 2:00 PM ET
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31443, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.09400, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekday Morning is Off-Peak in summer: Wed Jul 15, 2026 at 8:00 AM ET
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.10481, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekend Afternoon is Off-Peak: Sat Jul 18, 2026 at 2:00 PM ET
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 14, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.10481, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Super Off-Peak (10:00 PM - 5:00 AM): Wed Jul 15, 2026 at 11:00 PM ET
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.05500, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("October and April Shoulder Rates", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		}))

		// Weekday Morning On-Peak (7:00 AM - 9:00 AM): Wed Apr 15, 2026 at 8:00 AM ET
		p, err := u.priceForTime(time.Date(2026, time.April, 15, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31443, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.09400, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekday Afternoon On-Peak (1:00 PM - 7:00 PM): Thu Oct 15, 2026 at 3:00 PM ET
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 15, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31443, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.09400, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Midday Off-Peak (9:00 AM - 1:00 PM): Thu Oct 15, 2026 at 11:00 AM ET
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 11, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.10481, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Super Off-Peak (12:00 AM - 5:00 AM): Thu Oct 15, 2026 at 2:00 AM ET
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 2, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.05500, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("November to March Winter Rates", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		}))

		// Weekday Morning On-Peak (7:00 AM - 9:00 AM): Tue Nov 10, 2026 at 8:00 AM ET
		p, err := u.priceForTime(time.Date(2026, time.November, 10, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31443, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.09400, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Weekday Afternoon is Off-Peak in winter: Tue Nov 10, 2026 at 2:00 PM ET
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 14, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.10481, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Super Off-Peak: Tue Nov 10, 2026 at 3:00 AM ET
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 3, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.05500, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("2027 Forward Continuity and Site Integration", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		}))

		// 2027 Winter On-Peak: Tue Feb 9, 2027 at 8:00 AM ET
		pWinter, err := u.priceForTime(time.Date(2027, time.February, 9, 8, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31443, pWinter.DollarsPerKWH, 1e-6)
			assert.True(t, pWinter.SeparateGenerationCredit)
			assert.InDelta(t, 0.09400, pWinter.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2027 Shoulder On-Peak: Wed Apr 14, 2027 at 2:00 PM ET
		pShoulder, err := u.priceForTime(time.Date(2027, time.April, 14, 14, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.31443, pShoulder.DollarsPerKWH, 1e-6)
			assert.True(t, pShoulder.SeparateGenerationCredit)
			assert.InDelta(t, 0.09400, pShoulder.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2027 Summer Super Off-Peak: Wed Jul 14, 2027 at 11:00 PM ET
		pSummer, err := u.priceForTime(time.Date(2027, time.July, 14, 23, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.05500, pSummer.DollarsPerKWH, 1e-6)
			assert.True(t, pSummer.SeparateGenerationCredit)
			assert.InDelta(t, 0.02375, pSummer.GenerationCreditDollarsPerKWH, 1e-6)
		}

		m := NewMap(nil)
		siteUtil, err := m.Site(ctx, "site-rutherford", types.Settings{
			UtilityProvider: "rutherford_electric",
			UtilityRate:     "rutherford_electric_tod",
		})
		require.NoError(t, err)
		assert.Equal(t, "rutherford_electric_tod", siteUtil.Name())

		periods, err := siteUtil.GetPeriods(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, periods)

		future, err := siteUtil.GetFuturePrices(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, future)
	})
}
