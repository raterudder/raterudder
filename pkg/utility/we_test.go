package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWE(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	t.Run("RG1 Flat Rate Net Metering", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// RG1 flat rate: 0.19342 + 0.00199 + 0.00052 = 0.19593
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG1 Flat Rate with EV-R Credit", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
				EVCredit:          true,
			},
		})
		require.NoError(t, err)

		// Midnight to 8:00 a.m. has $0.04000/kWh discount -> 0.19593 - 0.04000 = 0.15593
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 3, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.15593, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Other hours are regular flat rate -> 0.19593
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG1 Flat Rate with CGS Credit", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "cgs",
			},
		})
		require.NoError(t, err)

		// CGS flat credit for RG1: $0.03636
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03636, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("RG2 TOU Rate Peak 9-9", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
				PeakPeriodOption:  "peak_9_9",
			},
		})
		require.NoError(t, err)

		// On-peak: Weekday (Monday Jan 12, 2026 at 2 PM) -> 0.30084 + 0.00199 + 0.00052 = 0.30335
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Off-peak: Weekday (Monday Jan 12, 2026 at 8 AM) -> 0.10028 + 0.00199 + 0.00052 = 0.10279
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Off-peak: Weekend (Saturday Jan 17, 2026 at 2 PM) -> 0.10279
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 14, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG2 TOU Rate Peak 7-7", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
				PeakPeriodOption:  "peak_7_7",
			},
		})
		require.NoError(t, err)

		// On-peak: Weekday (Monday Jan 12, 2026 at 8 AM) -> 0.30335
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Off-peak: Weekday (Monday Jan 12, 2026 at 8 PM) -> 0.10279
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 20, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG2 Holiday Shift", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
				PeakPeriodOption:  "peak_9_9",
			},
		})
		require.NoError(t, err)

		// Christmas 2026 is Friday Dec 25. Should be Holiday Off-peak at 2 PM -> 0.10279
		p, err := u.priceForTime(time.Date(2026, time.December, 25, 14, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Independence Day Saturday July 4, 2026 (observed Friday July 3).
		// Friday July 3 at 2 PM should be Holiday Off-peak -> 0.10279
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 14, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG2 with EV-R Credit", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
				PeakPeriodOption:  "peak_7_7",
				EVCredit:          true,
			},
		})
		require.NoError(t, err)

		// Monday Jan 12, 2026 at 3 AM: Off-peak ($0.10279) - EV credit ($0.01000) = $0.09279
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 3, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Monday Jan 12, 2026 at 7 AM: On-peak ($0.30335) - EV credit ($0.01000) = $0.29335
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.29335, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG2 with CGS Credit (Summer vs Non-Summer)", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "cgs",
				PeakPeriodOption:  "peak_9_9",
			},
		})
		require.NoError(t, err)

		// Summer On-Peak: Monday June 15, 2026 at 2 PM -> CGS credit $0.04906
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04906, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Off-Peak: Monday June 15, 2026 at 8 AM -> CGS credit $0.03290
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 8, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03290, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Non-Summer On-Peak: Monday Jan 12, 2026 at 2 PM -> CGS credit $0.03863
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03863, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Non-Summer Off-Peak: Monday Jan 12, 2026 at 8 AM -> CGS credit $0.03289
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, chicago))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03289, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})
}
