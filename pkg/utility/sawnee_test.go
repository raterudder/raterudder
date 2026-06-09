package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSawneeUtility(t *testing.T) {
	t.Run("Schedule H Base Rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sawnee",
			UtilityRate:     "sawnee_h",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "solar",
			},
		})
		require.NoError(t, err)

		// Flat rate should be $0.0767/kWh at all times
		// Solar export should be separate credit of $0.0379/kWh
		for _, hour := range []int{2, 7, 12, 18, 23} {
			p, err := u.priceForTime(time.Date(2026, time.June, 15, hour, 0, 0, 0, etLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.0767, p.DollarsPerKWH, 1e-6)
				assert.True(t, p.SeparateGenerationCredit)
				assert.InDelta(t, 0.0379, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}
	})

	t.Run("Schedule TU-28 Rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sawnee",
			UtilityRate:     "sawnee_tu",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "solar",
			},
		})
		require.NoError(t, err)

		// Summer Mon, Jun 15, 2026
		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, etLocation)

		// Peak: 3:00 PM ($0.335)
		p, err := u.priceForTime(summerMon.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.335, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM ($0.0445)
		p, err = u.priceForTime(summerMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0445, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Sat, Jun 20, 2026 at 3:00 PM ($0.0445)
		p, err = u.priceForTime(time.Date(2026, time.June, 20, 15, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0445, p.DollarsPerKWH, 1e-6)
		}

		// Holiday: July 4, 2026 at 3:00 PM ($0.0445)
		p, err = u.priceForTime(time.Date(2026, time.July, 4, 15, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0445, p.DollarsPerKWH, 1e-6)
		}

		// Winter: Nov 10, 2026 at 3:00 PM ($0.0445)
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 15, 0, 0, 0, etLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0445, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Schedule CPPR-14 Rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sawnee",
			UtilityRate:     "sawnee_cpp",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "solar",
			},
		})
		require.NoError(t, err)

		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, etLocation)

		// Peak: 3:00 PM ($0.286)
		p, err := u.priceForTime(summerMon.Add(15 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.286, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM ($0.0425)
		p, err = u.priceForTime(summerMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0425, p.DollarsPerKWH, 1e-6)
		}
	})
}
