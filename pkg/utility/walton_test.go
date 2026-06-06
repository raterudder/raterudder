package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaltonUtility(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	t.Run("Schedule A-15 Base Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "walton",
			UtilityRate:     "walton_a15",
		})
		require.NoError(t, err)

		// Winter (January) - base rate should be $0.1205
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1205, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.026, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer (July) - base rate should be $0.1225
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1225, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.026, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Winter (November) - base rate should be $0.1205
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1205, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.026, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Schedule TU-5 Peak and Off-Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "walton",
			UtilityRate:     "walton_tu5",
		})
		require.NoError(t, err)

		// On-Peak summer weekday: Mon, Jun 15, 2026 at 4:00 PM -> $0.3225
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3225, p.DollarsPerKWH, 1e-6)
		}

		// On-Peak early September weekday: Wed, Sept 2, 2026 at 4:00 PM -> $0.3225
		p, err = u.priceForTime(time.Date(2026, time.September, 2, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3225, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak weekday morning: Mon, Jun 15, 2026 at 10:00 AM -> $0.0885
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 10, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0885, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak summer weekend: Sat, Jun 20, 2026 at 4:00 PM -> $0.0885
		p, err = u.priceForTime(time.Date(2026, time.June, 20, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0885, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak non-summer month: Wed, Apr 15, 2026 at 4:00 PM -> $0.0885
		p, err = u.priceForTime(time.Date(2026, time.April, 15, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0885, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak late September: Tue, Sept 15, 2026 at 4:00 PM -> $0.0885
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0885, p.DollarsPerKWH, 1e-6)
		}

		// Holiday (July 4th) exclusion: Sat, Jul 4, 2026 at 4:00 PM -> $0.0885
		p, err = u.priceForTime(time.Date(2026, time.July, 4, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0885, p.DollarsPerKWH, 1e-6)
		}

		// Holiday (Labor Day) exclusion: Mon, Sept 7, 2026 at 4:00 PM -> $0.0885
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0885, p.DollarsPerKWH, 1e-6)
		}
	})
}
