package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBWPUtility(t *testing.T) {
	u := &genericTOU{}

	t.Run("Residential Flat 2026 and 2027 Rates and Export Schemes", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// 2026 Tier 1 flat rate: $0.1460 base + $0.0340 ECAC = $0.1800/kWh
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			// Legacy NEM (net) offsets consumption at 1:1 retail rate
			assert.False(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Verify 2026 -> 2027 boundary under Legacy NEM (net)
		p, err = u.priceForTime(time.Date(2026, time.December, 31, 23, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
		}
		p, err = u.priceForTime(time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			// 2027 Tier 1 flat rate: $0.1744 base + $0.0340 ECAC = $0.2084/kWh
			assert.InDelta(t, 0.2084, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Test with Solar Net Billing (net_billing) across Section 11(C) TOU export windows
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// Summer On-Peak export (weekday 16:00 - 19:00): $0.1186/kWh
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ptLocation)) // Monday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1186, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Mid-Peak export (weekday 08:00 - 16:00 & 19:00 - 23:00): $0.0825/kWh
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0825, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Off-Peak export (weekday 23:00 - 08:00 & weekends/holidays): $0.0702/kWh
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 23, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0702, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Winter export (Nov - May all hours) in 2027: $0.2084/kWh import, $0.0696/kWh export
		p, err = u.priceForTime(time.Date(2027, time.February, 10, 14, 0, 0, 0, ptLocation)) // Wednesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2084, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0696, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Default empty NetMeteringScheme defaults to "net_billing"
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_residential",
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2027, time.June, 15, 17, 0, 0, 0, ptLocation)) // Tuesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2084, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1186, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Residential TOU EV R-TOU-EV 2026 and 2027 Pricing and Seasons", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
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
		assert.True(t, names["Mid-Peak"])
		assert.True(t, names["Off-Peak"])

		// --- 2026 Summer: June 15 ---
		// 1. Summer On-Peak (4:00 PM - 7:00 PM, weekday)
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ptLocation)) // Monday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3902, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2. Summer Mid-Peak (8:00 AM - 4:00 PM & 7:00 PM - 11:00 PM, weekday)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2617, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 20, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2617, p.DollarsPerKWH, 1e-6)
		}

		// 3. Summer Off-Peak (11:00 PM - 8:00 AM, weekday)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 4, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
		}

		// 4. Summer Weekend (Sunday is entirely Off-Peak)
		p, err = u.priceForTime(time.Date(2026, time.June, 14, 12, 0, 0, 0, ptLocation)) // Sunday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
		}

		// --- 2026 Winter: December 15 ---
		// 5. Winter Mid-Peak (8:00 AM - 11:00 PM, weekday)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ptLocation)) // Tuesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2617, p.DollarsPerKWH, 1e-6)
		}

		// 6. Winter Off-Peak (11:00 PM - 8:00 AM, weekday)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 4, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
		}

		// 7. Winter Weekend (entirely Off-Peak)
		p, err = u.priceForTime(time.Date(2026, time.December, 13, 12, 0, 0, 0, ptLocation)) // Sunday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
		}

		// --- 2026 -> 2027 Nov-May Split & 2027 Rates ---
		// Dec 31, 2026 23:00 PT (Thursday Off-Peak): 2026 Off-Peak $0.1555/kWh
		p, err = u.priceForTime(time.Date(2026, time.December, 31, 23, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
		}
		// Jan 1, 2027 00:00 PT (New Year's Day Holiday Off-Peak): 2027 Off-Peak $0.1735/kWh
		p, err = u.priceForTime(time.Date(2027, time.January, 1, 0, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
		}

		// 2027 Summer On-Peak ($0.4014 + $0.0340 = $0.4354/kWh)
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 17, 0, 0, 0, ptLocation)) // Tuesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4354, p.DollarsPerKWH, 1e-6)
		}

		// 2027 Summer Mid-Peak ($0.2580 + $0.0340 = $0.2920/kWh)
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2920, p.DollarsPerKWH, 1e-6)
		}

		// 2027 Summer Off-Peak ($0.1395 + $0.0340 = $0.1735/kWh)
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 4, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
		}

		// 2027 Winter Mid-Peak ($0.2580 + $0.0340 = $0.2920/kWh) & Off-Peak ($0.1735/kWh)
		p, err = u.priceForTime(time.Date(2027, time.February, 10, 12, 0, 0, 0, ptLocation)) // Wednesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2920, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.November, 16, 4, 0, 0, 0, ptLocation)) // Tuesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Residential TOU EV Holiday Sunday Shifting", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// In 2027, July 4th (Independence Day) falls on a Sunday.
		// Therefore, the following Monday (July 5th, 2027) is recognized as a holiday off-peak period
		// for both retail consumption ($0.1735/kWh) and Section 11(C) export credits ($0.0702/kWh).
		p, err := u.priceForTime(time.Date(2027, time.July, 5, 17, 0, 0, 0, ptLocation)) // Monday 5 PM (usually On-Peak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0702, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Verify Legacy NEM (net) parity on shifted Monday holiday
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 12, 0, 0, 0, ptLocation)) // Monday 12 PM (usually Mid-Peak)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Residential TOU EV Export Schemes", func(t *testing.T) {
		// Default empty NetMeteringScheme defaults to "net_billing"
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
		})
		require.NoError(t, err)

		// 1. Summer On-Peak weekday (16:00 - 19:00): $0.3902 import, $0.1186 export in 2026
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ptLocation)) // Monday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3902, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1186, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 2. Explicit "net_billing" across all Section 11(C) TOU export windows
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// Summer Mid-Peak morning (08:00 - 16:00) and evening (19:00 - 23:00): $0.0825/kWh
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 10, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2617, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0825, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.August, 18, 20, 0, 0, 0, ptLocation)) // Wednesday in 2027
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2920, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0825, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Summer Off-Peak weekday overnight (23:00 - 08:00) and weekend: $0.0702/kWh
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 3, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0702, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.June, 13, 17, 0, 0, 0, ptLocation)) // Sunday in 2027
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0702, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Winter (Nov - May) weekday Mid-Peak, weekday Off-Peak, and weekend: $0.0696/kWh all day
		p, err = u.priceForTime(time.Date(2026, time.November, 17, 17, 0, 0, 0, ptLocation)) // Tuesday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2617, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0696, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.March, 14, 2, 0, 0, 0, ptLocation)) // Sunday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1735, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0696, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Continuous 2026-2027 Base and Solar Export Coverage", func(t *testing.T) {
		info := bwpUtilityInfo()
		require.Len(t, info.Rates, 2)

		start := time.Date(2026, time.January, 1, 0, 0, 0, 0, ptLocation)
		end := time.Date(2028, time.January, 1, 0, 0, 0, 0, ptLocation)

		for _, rate := range info.Rates {
			periods, err := rate.GetFees(types.UtilityRateOptions{})
			require.NoError(t, err)

			var namedImport []types.TimePeriod
			for _, p := range periods {
				assert.NotEmpty(t, p.Description)
				if p.SeparateGenerationCredit {
					assert.Empty(t, p.Name, "export-only periods should omit Name")
				} else if p.Name != "" {
					namedImport = append(namedImport, p.TimePeriod)
				}
			}

			if rate.ID == "bwp_res_tou_ev" {
				require.NotEmpty(t, namedImport)
				assert.True(t, namedPeriodsCoverDay(namedImport, time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation)))
				assert.True(t, namedPeriodsCoverDay(namedImport, time.Date(2027, time.January, 1, 12, 0, 0, 0, ptLocation)))
				assert.True(t, namedPeriodsCoverDay(namedImport, time.Date(2027, time.July, 5, 12, 0, 0, 0, ptLocation)))
			} else {
				assert.Empty(t, namedImport)
			}

			for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
				baseMatches := 0
				solarMatches := 0
				for i := range periods {
					ok, _, err := periods[i].Contains(ts)
					require.NoError(t, err)
					if !ok {
						continue
					}
					if periods[i].SeparateGenerationCredit {
						solarMatches++
					} else {
						baseMatches++
						if rate.ID == "bwp_res_tou_ev" {
							assert.NotEmpty(t, periods[i].Name, "expected named import period at %v", ts)
						}
					}
				}
				require.Equal(t, 1, baseMatches, "rate %s expected 1 base period at %v", rate.ID, ts)
				require.Equal(t, 1, solarMatches, "rate %s expected 1 solar export period at %v", rate.ID, ts)
			}
		}
	})
}
