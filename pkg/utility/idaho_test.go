package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdahoUtility(t *testing.T) {
	t.Run("Schedule 1 Standard Plan Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "idaho",
			UtilityRate:     "idaho_std",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "on_site",
			},
		})
		require.NoError(t, err)

		// Summer: Mon, Jun 15, 2026 -> $0.121195
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.121195, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			// Summer off-peak export credit: $0.033920
			assert.InDelta(t, 0.033920, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Non-Summer: Thu, Jan 15, 2026 -> $0.099332
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.099332, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			// Non-summer export credit: $0.029019
			assert.InDelta(t, 0.029019, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Schedule 6 TOU Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "idaho",
			UtilityRate:     "idaho_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "on_site",
			},
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

		// --- SUMMER PERIODS (June 1 - Sept 30) ---
		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, mtLocation)

		// On-Peak (Mon-Sat 7 PM - 11 PM): 8:00 PM -> $0.299185
		p, err := u.priceForTime(summerMon.Add(20 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.299185, p.DollarsPerKWH, 1e-6)
			// Exportcredit at 8 PM is On-Peak (3 PM - 11 PM): $0.156836
			assert.InDelta(t, 0.156836, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Mid-Peak (Mon-Sat 3 PM - 7 PM): 4:00 PM -> $0.149594
		p, err = u.priceForTime(summerMon.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.149594, p.DollarsPerKWH, 1e-6)
			// Exportcredit at 4 PM is On-Peak (3 PM - 11 PM): $0.156836
			assert.InDelta(t, 0.156836, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (Mon-Sat 11 PM - 3 PM): 10:00 AM -> $0.074797
		p, err = u.priceForTime(summerMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.074797, p.DollarsPerKWH, 1e-6)
			// Exportcredit at 10 AM is Off-Peak: $0.033920
			assert.InDelta(t, 0.033920, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Sunday: Sun, Jun 14, 2026 at 8:00 PM -> $0.074797 (off-peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 14, 20, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.074797, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.033920, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Holiday (July 4th): Sat, Jul 4, 2026 at 8:00 PM -> $0.074797 (off-peak)
		p, err = u.priceForTime(time.Date(2026, time.July, 4, 20, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.074797, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.033920, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// --- NON-SUMMER PERIODS (Oct 1 - May 31) ---
		winterMon := time.Date(2026, time.January, 19, 0, 0, 0, 0, mtLocation)

		// On-Peak (Mon-Sat 6 AM - 9 AM & 5 PM - 8 PM): 7:00 AM -> $0.138347
		p, err = u.priceForTime(winterMon.Add(7 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.138347, p.DollarsPerKWH, 1e-6)
			// Winter export credit is flat: $0.029019
			assert.InDelta(t, 0.029019, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak (other times): 12:00 PM -> $0.092231
		p, err = u.priceForTime(winterMon.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.092231, p.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.029019, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Standard 1:1 Net Metering", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "idaho",
			UtilityRate:     "idaho_std",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, mtLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.121195, p.DollarsPerKWH, 1e-6)
			// 1:1 net metering should not append separate credits to genericTOU
			assert.False(t, p.SeparateGenerationCredit)
		}
	})
}
