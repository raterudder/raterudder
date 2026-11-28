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
	t.Run("RG1 Flat Rate Net Metering and October 2026 Fuel Refund", func(t *testing.T) {
		settings := types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		}
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), settings)
		require.NoError(t, err)

		// Standard RG1 flat rate: 0.19342 + 0.00199 + 0.00052 = 0.19593
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
			assert.True(t, settings.UtilityRateOptions.IsFlatNetMetering())
			assert.InDelta(t, 0.19593, p.ExportRateDollars(settings, []types.Price{p}), 1e-6)
		}

		// End of September 2026 stays at $0.19593/kWh
		p, err = u.priceForTime(time.Date(2026, time.September, 30, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6)

		// October 2026 fuel cost refund (-$0.02065/kWh): 0.19593 - 0.02065 = 0.17528
		p, err = u.priceForTime(time.Date(2026, time.October, 1, 0, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.17528, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 31, 23, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.17528, p.DollarsPerKWH, 1e-6)

		// November 2026 returns to $0.19593/kWh and carries into 2027
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 0, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.October, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6)
	})

	t.Run("RG1 Flat Rate with EV-R Credit in 2026 and 2027", func(t *testing.T) {
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

		// 2026: Midnight to 8:00 a.m. has $0.04000/kWh discount -> 0.19593 - 0.04000 = 0.15593
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 3, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.15593, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2026 at 7:00 a.m. is still inside 00:00-08:00 EV-R window -> 0.15593
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15593, p.DollarsPerKWH, 1e-6)

		// 2026 at 8:00 a.m. is outside EV-R window -> 0.19593
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6)

		// 2027: EV-R window shifts to 00:00-07:00 per Determination 81
		p, err = u.priceForTime(time.Date(2027, time.January, 15, 6, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15593, p.DollarsPerKWH, 1e-6)

		// 2027 at 7:00 a.m. is outside 00:00-07:00 EV-R window -> 0.19593
		p, err = u.priceForTime(time.Date(2027, time.January, 15, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6)
	})

	t.Run("RG1 Flat Rate with CGS and CGS-NP Export Schemes", func(t *testing.T) {
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
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03636, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// CGS-NP non-purchase scheme for RG1: $0.00/kWh separate generation credit
		cgsNPSettings := types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "cgs_np",
			},
		}
		err = u.ApplySettings(context.Background(), cgsNPSettings)
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19593, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p.ExportRateDollars(cgsNPSettings, []types.Price{p}), 1e-6)
		}
	})

	t.Run("RG2 TOU Rate Peak 9-9 and October 2026 Fuel Refund", func(t *testing.T) {
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
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Off-peak: Weekday (Monday Jan 12, 2026 at 8 AM) -> 0.10028 + 0.00199 + 0.00052 = 0.10279
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Off-peak: Weekend (Saturday Jan 17, 2026 at 2 PM) -> 0.10279
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// October 2026 On-Peak (Thursday Oct 15, 2026 at 2 PM): 0.30335 - 0.02065 = 0.28270
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.28270, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// October 2026 Off-Peak (Thursday Oct 15, 2026 at 8 AM): 0.10279 - 0.02065 = 0.08214
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08214, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// November 2026 On-Peak (Monday Nov 2, 2026 at 2 PM) returns to 0.30335
		p, err = u.priceForTime(time.Date(2026, time.November, 2, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6)
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
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Off-peak: Weekday (Monday Jan 12, 2026 at 8 PM) -> 0.10279
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 20, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("RG2 Sunday-Only Holiday Shift", func(t *testing.T) {
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

		// Christmas 2026 is Friday Dec 25 -> Holiday Off-Peak at 2 PM ($0.10279)
		p, err := u.priceForTime(time.Date(2026, time.December, 25, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", p.PeriodName)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Independence Day 2026 is Saturday July 4 (stays on Saturday per Sheet 221).
		// Preceding Friday July 3, 2026 at 2 PM must remain weekday On-Peak ($0.30335).
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Independence Day 2027 is Sunday July 4 -> shifts to Monday July 5, 2027 Off-Peak ($0.10279).
		p, err = u.priceForTime(time.Date(2027, time.July, 5, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Christmas 2027 is Saturday Dec 25 and New Year's Day 2028 is Saturday Jan 1.
		// Preceding Fridays 2027-12-24 and 2027-12-31 at 2 PM remain weekday On-Peak ($0.30335).
		p, err = u.priceForTime(time.Date(2027, time.December, 24, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		p, err = u.priceForTime(time.Date(2027, time.December, 31, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.Equal(t, "On-Peak", p.PeriodName)
		}
	})

	t.Run("RG2 with EV-R Credit in 2026 and 2027", func(t *testing.T) {
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

		// 2026 Monday Jan 12 at 3 AM: Off-peak ($0.10279) - EV credit ($0.01000) = $0.09279
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 3, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09279, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2026 Monday Jan 12 at 7 AM: On-peak ($0.30335) - EV credit ($0.01000) = $0.29335
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.29335, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2027 Monday Jan 11 at 6 AM: Off-peak ($0.10279) - EV credit ($0.01000) = $0.09279
		p, err = u.priceForTime(time.Date(2027, time.January, 11, 6, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09279, p.DollarsPerKWH, 1e-6)

		// 2027 Monday Jan 11 at 7 AM: outside 00:00-07:00 EV-R window -> full On-peak $0.30335
		p, err = u.priceForTime(time.Date(2027, time.January, 11, 7, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6)
	})

	t.Run("RG2 with CGS-CU Capacity Transitions and CGS-NP", func(t *testing.T) {
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

		// Jan-May 2026 Non-Summer On-Peak: Monday Jan 12, 2026 at 2 PM -> CGS-CU credit $0.08093
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.08093, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Jan-May 2026 Non-Summer Off-Peak: Monday Jan 12, 2026 at 8 AM -> CGS credit $0.03289
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03289, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer On-Peak (effective June 1, 2026): Monday June 15, 2026 at 2 PM -> CGS-CU credit $0.09346
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.09346, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Off-Peak: Monday June 15, 2026 at 8 AM -> CGS credit $0.03290
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 8, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03290, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Oct-Dec 2026 Non-Summer On-Peak: Thursday Oct 15, 2026 at 2 PM -> Oct refund import $0.28270 + CGS-CU credit $0.08303
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.28270, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.08303, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2027 Non-Summer On-Peak (Jan-May & Oct-Dec): Monday Jan 11, 2027 at 2 PM -> CGS-CU credit $0.08303
		p, err = u.priceForTime(time.Date(2027, time.January, 11, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.08303, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2027 Summer Holiday (Monday July 5, 2027 at 2 PM): Off-Peak CGS credit $0.03290
		p, err = u.priceForTime(time.Date(2027, time.July, 5, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.10279, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.03290, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// RG2 CGS-NP non-purchase scheme: $0.00/kWh separate generation credit
		cgsNPSettings := types.Settings{
			UtilityProvider: "we",
			UtilityRate:     "we_rg2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "cgs_np",
				PeakPeriodOption:  "peak_9_9",
			},
		}
		err = u.ApplySettings(context.Background(), cgsNPSettings)
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, ctLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.30335, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0, p.ExportRateDollars(cgsNPSettings, []types.Price{p}), 1e-6)
		}
	})

	t.Run("Export Option Choices and 2026-2027 Continuity", func(t *testing.T) {
		info := weUtilityInfo()
		require.Len(t, info.Rates, 2)

		var exportOpt types.UtilityRateOption
		for _, opt := range info.Rates[1].Options {
			if opt.Field == "netMeteringScheme" {
				exportOpt = opt
				break
			}
		}
		if assert.Len(t, exportOpt.Choices, 3) {
			assert.Equal(t, "net", exportOpt.Choices[0].Value)
			assert.Equal(t, "cgs", exportOpt.Choices[1].Value)
			assert.Equal(t, "cgs_np", exportOpt.Choices[2].Value)
		}

		assertHourlyContinuity := func(t *testing.T, periods []types.UtilityFeesPeriod) {
			t.Helper()
			start := time.Date(2026, time.January, 1, 0, 0, 0, 0, ctLocation)
			end := time.Date(2028, time.January, 1, 0, 0, 0, 0, ctLocation)
			for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
				baseMatches := 0
				solarMatches := 0
				for i := range periods {
					ok, _, err := periods[i].Contains(ts)
					if err != nil {
						require.NoError(t, err)
					}
					if ok {
						if periods[i].SeparateGenerationCredit {
							solarMatches++
						} else {
							baseMatches++
						}
					}
				}
				require.Equal(t, 1, baseMatches, "expected 1 base period at %v", ts)
				require.Equal(t, 1, solarMatches, "expected 1 solar period at %v", ts)
			}
		}

		t.Run("RG1 CGS Continuity", func(t *testing.T) {
			periods, err := info.Rates[0].GetFees(types.UtilityRateOptions{NetMeteringScheme: "cgs"})
			require.NoError(t, err)
			assertHourlyContinuity(t, periods)
		})

		t.Run("RG1 CGS-NP Continuity", func(t *testing.T) {
			periods, err := info.Rates[0].GetFees(types.UtilityRateOptions{NetMeteringScheme: "cgs_np"})
			require.NoError(t, err)
			assertHourlyContinuity(t, periods)
		})

		t.Run("RG2 CGS Continuity", func(t *testing.T) {
			periods, err := info.Rates[1].GetFees(types.UtilityRateOptions{NetMeteringScheme: "cgs", PeakPeriodOption: "peak_9_9"})
			require.NoError(t, err)
			assertHourlyContinuity(t, periods)
		})

		t.Run("RG2 CGS-NP Continuity", func(t *testing.T) {
			periods, err := info.Rates[1].GetFees(types.UtilityRateOptions{NetMeteringScheme: "cgs_np", PeakPeriodOption: "peak_9_9"})
			require.NoError(t, err)
			assertHourlyContinuity(t, periods)
		})
	})
}
