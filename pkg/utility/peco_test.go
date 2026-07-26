package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPECOUtility(t *testing.T) {
	t.Run("Standard Flat Rate R", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "peco",
			UtilityRate:     "peco_r",
		})
		require.NoError(t, err)

		// Test Dec 2025: $0.11024
		p, err := u.priceForTime(time.Date(2025, time.December, 15, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11024, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Test Jan-May 2026: $0.11024
		p, err = u.priceForTime(time.Date(2026, time.February, 10, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11024, p.DollarsPerKWH, 1e-6)
		}

		// Test Jun-Dec 2026: $0.11759
		p, err = u.priceForTime(time.Date(2026, time.July, 4, 12, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.11759, p.DollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("TOU Rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "peco",
			UtilityRate:     "peco_tou",
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

		// Dec 2025 (Winter 2025)
		// Monday, Dec 15, 2025
		decMon := time.Date(2025, time.December, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak: 2:00 AM ($0.06061)
		p, err := u.priceForTime(decMon.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.06061, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM ($0.08382)
		p, err = u.priceForTime(decMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.08382, p.DollarsPerKWH, 1e-6)
		}

		// Peak: 3:00 PM (15:00) ($0.32747)
		p, err = u.priceForTime(decMon.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32747, p.DollarsPerKWH, 1e-6)
		}

		// Jun-Dec 2026 (Summer/Fall 2026)
		// Monday, Jun 15, 2026
		junMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak: 2:00 AM ($0.06741)
		p, err = u.priceForTime(junMon.Add(2 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.06741, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM ($0.09336)
		p, err = u.priceForTime(junMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.09336, p.DollarsPerKWH, 1e-6)
		}

		// Peak: 3:00 PM (15:00) ($0.32404)
		p, err = u.priceForTime(junMon.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.32404, p.DollarsPerKWH, 1e-6)
		}

		// Holiday exclusion: Christmas Day 2026 (Friday, Dec 25, 2026)
		christmas := time.Date(2026, time.December, 25, 15, 0, 0, 0, etLocation) // 3:00 PM
		p, err = u.priceForTime(christmas)
		if assert.NoError(t, err) {
			// Peak hours on holidays should be treated as Off-Peak ($0.09336)
			assert.InDelta(t, 0.09336, p.DollarsPerKWH, 1e-6)
		}
	})
}
