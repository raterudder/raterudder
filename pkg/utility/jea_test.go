package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJEA(t *testing.T) {
	t.Run("Rate R Residential Service Flat", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Jan 2026: Tier 1 ($0.07237) + Fuel ($0.04224) = $0.11461
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11461, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// June 2026: Tier 1 ($0.07237) + Fuel ($0.04494) = $0.11731
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11731, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// August 2026: Tier 1 ($0.07237) + Fuel ($0.04282) = $0.11519
		p, err = u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11519, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// September 2026: Tier 1 ($0.07237) + Fuel ($0.04282) = $0.11519
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11519, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// October 2026: Tier 1 ($0.07815) + Fuel ($0.04326) = $0.12141
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12141, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// November 2026: Tier 1 ($0.07815) + Fuel ($0.04975) = $0.12790
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12790, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// December 2026: Tier 1 ($0.07815) + Fuel ($0.04861) = $0.12676
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12676, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// January 2027 fallback: Tier 1 ($0.07815) + Dec 2026 fallback Fuel ($0.04861) = $0.12676
		p, err = u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12676, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Rate R Residential Service DG", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dg",
			},
		})
		require.NoError(t, err)

		// Jan 2026: Consumption = $0.11461, Export Credit = $0.04224 (fuel only)
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.11461, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04224, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Oct 2026: Consumption = $0.12141, Export Credit = $0.04326 (fuel only)
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12141, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04326, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Nov 2026: Consumption = $0.12790, Export Credit = $0.04975 (fuel only)
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12790, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04975, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Dec 2026: Consumption = $0.12676, Export Credit = $0.04861 (fuel only)
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12676, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04861, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Jan 2027 fallback: Consumption = $0.12676, Export Credit = $0.04861 (Dec 2026 fuel fallback)
		p, err = u.priceForTime(time.Date(2027, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.12676, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04861, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Rate GST Time of Day Winter Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
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
		assert.True(t, names["Off-Peak"])

		// Winter Peak (Jan 12, 2026 is Monday)
		// On-peak 6-10 AM, 6-10 PM. On-peak base: $0.13776 + Jan fuel $0.04224 = $0.18000
		p, err := u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18000, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		p, err = u.priceForTime(time.Date(2026, time.January, 12, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18000, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Winter Off-peak (Jan 12, 2026 at 12 PM)
		// Off-peak base: $0.04535 + Jan fuel $0.04224 = $0.08759
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08759, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Winter Weekend Off-peak (Jan 17, 2026 is Saturday)
		p, err = u.priceForTime(time.Date(2026, time.January, 17, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08759, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Pre-Oct 2026 evening peak extends to 10 PM (Jan 12, 2026 at 9 PM is On-Peak)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.18000, p.DollarsPerKWH, 1e-6)

		// Nov 2026 Winter Peak & TOU Fuel (Nov 16, 2026 is Monday):
		// Sheet 5.1 peaks: 06:00–10:00 and 18:00–21:00.
		// On-Peak: $0.14195 base + $0.05310 TOU fuel = $0.19505
		// Off-Peak: $0.04732 base + $0.04828 TOU fuel = $0.09560
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 5, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 6, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 9, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 17, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 18, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 20, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		// 21:00 (9 PM) is now Off-Peak under Fourth Revised Sheet No. 5.1
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)

		// Dec 2026 & Jan 2027 carry forward Nov 2026 TOU fuel ($0.19505 On-Peak / $0.09560 Off-Peak)
		p, err = u.priceForTime(time.Date(2026, time.December, 14, 18, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 14, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.January, 11, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.January, 11, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Rate GST Time of Day Summer Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Summer Peak (June 15, 2026 is Monday)
		// On-peak 12 PM - 9 PM. On-peak base: $0.13776 + June fuel $0.04494 = $0.18270
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18270, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Summer Off-peak (June 15, 2026 at 8 AM)
		// Off-peak base: $0.04535 + June fuel $0.04494 = $0.09029
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09029, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// August On-Peak (August 17, 2026 is Monday)
		// On-peak base: $0.13776 + Aug fuel $0.04282 = $0.18058
		p, err = u.priceForTime(time.Date(2026, time.August, 17, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18058, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// September Off-Peak (September 14, 2026 is Monday at 8 AM)
		// Off-peak base: $0.04535 + Sept fuel $0.04282 = $0.08817
		p, err = u.priceForTime(time.Date(2026, time.September, 14, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08817, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// October On-Peak (October 12, 2026 is Monday at 2 PM)
		// On-peak base: $0.14195 + Oct fuel $0.04326 = $0.18521
		p, err = u.priceForTime(time.Date(2026, time.October, 12, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18521, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// October Off-Peak (October 12, 2026 is Monday at 8 AM)
		// Off-peak base: $0.04732 + Oct fuel $0.04326 = $0.09058
		p, err = u.priceForTime(time.Date(2026, time.October, 12, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09058, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// October Sheet 5.1 Apr–Oct peak boundary hours (13:00–20:00 on-peak; 12:00 and 20:00 off-peak)
		p, err = u.priceForTime(time.Date(2026, time.October, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09058, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 12, 13, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.18521, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 12, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.18521, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 12, 20, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09058, p.DollarsPerKWH, 1e-6)

		// Summer 2027 (June 14, 2027 is Monday): 13:00–20:00 On-Peak with TOU fuel carried forward
		p, err = u.priceForTime(time.Date(2027, time.June, 14, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 14, 13, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 14, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 14, 20, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Rate GST Time of Day Holiday Shift", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Independence Day: Saturday July 4th, 2026 (observed Friday July 3rd, 2026)
		// Friday July 3rd at 2:00 PM (normally summer peak, but should be holiday off-peak)
		p, err := u.priceForTime(time.Date(2026, time.July, 3, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.08921, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// Thanksgiving Day: Thursday November 26th, 2026 at 8:00 AM (holiday off-peak $0.09560)
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}

		// New Year's Day 2028 (Saturday Jan 1, 2028) observed Friday Dec 31, 2027 at 8:00 AM
		p, err = u.priceForTime(time.Date(2027, time.December, 31, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6) {
			assert.False(t, p.SeparateGenerationCredit)
		}
	})

	t.Run("Rate GST Time of Day DG Credit", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "jea",
			UtilityRate:     "jea_gst",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dg",
			},
		})
		require.NoError(t, err)

		// June On-Peak: consumption = $0.18270, export credit = $0.04494 (fuel only)
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.18270, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04494, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Nov 2026 On-Peak & Off-Peak: TOU consumption ($0.19505 / $0.09560), levelized monthly fuel credit ($0.04975)
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04975, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.09560, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04975, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Dec 2026 On-Peak: TOU consumption ($0.19505), levelized monthly fuel credit ($0.04861)
		p, err = u.priceForTime(time.Date(2026, time.December, 14, 18, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04861, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Jan 2027 On-Peak: TOU consumption ($0.19505), Dec 2026 fallback fuel credit ($0.04861)
		p, err = u.priceForTime(time.Date(2027, time.January, 11, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		if assert.InDelta(t, 0.19505, p.DollarsPerKWH, 1e-6) {
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.04861, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})
}
