package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSECOUtility(t *testing.T) {
	t.Run("Schedule RS Flat Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "seco",
			UtilityRate:     "seco_rs",
		})
		require.NoError(t, err)

		// Flat $0.1194 year-round
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1194, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.095, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1194, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.095, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Schedule RS-TOU Prices", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "seco",
			UtilityRate:     "seco_rs_tou",
		})
		require.NoError(t, err)

		// Summer (Apr - Oct): Mon, Jun 15, 2026
		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, etLocation)

		// Peak (2 PM - 6 PM weekdays): 3:00 PM -> $0.2370
		p, err := u.priceForTime(summerMon.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2370, p.DollarsPerKWH, 1e-6)
		}

		// Super Off-Peak (12 AM - 6 AM daily): 2:00 AM -> $0.0770
		p, err = u.priceForTime(summerMon.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0770, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak (other hours): 10:00 AM -> $0.0970
		p, err = u.priceForTime(summerMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0970, p.DollarsPerKWH, 1e-6)
		}

		// Winter (Nov - Mar): Tue, Jan 20, 2026
		winterTue := time.Date(2026, time.January, 20, 0, 0, 0, 0, etLocation)

		// Peak (6 AM - 9 AM weekdays): 8:00 AM -> $0.2370
		p, err = u.priceForTime(winterTue.Add(8 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2370, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 12:00 PM -> $0.0970
		p, err = u.priceForTime(winterTue.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0970, p.DollarsPerKWH, 1e-6)
		}

		// Super Off-Peak: 2:00 AM -> $0.0770
		p, err = u.priceForTime(winterTue.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0770, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Holidays Exclusions", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "seco",
			UtilityRate:     "seco_rs_tou",
		})
		require.NoError(t, err)

		// Memorial Day (last Monday in May): Mon, May 25, 2026 at 3:00 PM -> should be Off-Peak ($0.0970)
		p, err := u.priceForTime(time.Date(2026, time.May, 25, 15, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0970, p.DollarsPerKWH, 1e-6)
		}
	})
}
