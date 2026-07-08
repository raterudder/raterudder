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

	t.Run("Residential Flat 16.01 under 300 kWh", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// Flat rate should be $0.1800/kWh at any time
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			// Legacy NEM (net) should offset consumption at 1:1 retail rate
			assert.False(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Test with Solar Net Billing (net_billing)
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_residential",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1800, p.DollarsPerKWH, 1e-6)
			// Solar Net Billing should credit at the avoided cost of $0.0455
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0455, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Residential TOU EV R-TOU-EV Pricing and Seasons", func(t *testing.T) {
		// --- Summer: June 15 ---
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// 1. Summer On-Peak (4:00 PM - 7:00 PM, weekday)
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ptLocation)) // Monday
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3902, p.DollarsPerKWH, 1e-6)
			assert.False(t, p.SeparateGenerationCredit)
		}

		// 2. Summer Mid-Peak (8:00 AM - 4:00 PM, weekday)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ptLocation))
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

		// --- Winter: December 15 ---
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
	})

	t.Run("Residential TOU EV Holiday Sunday Shifting", func(t *testing.T) {
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// In 2027, July 4th (Independence Day) falls on a Sunday.
		// Therefore, the following Monday (July 5th, 2027) is recognized as a holiday off-peak period.
		p, err := u.priceForTime(time.Date(2027, time.July, 5, 12, 0, 0, 0, ptLocation)) // Monday 12 PM (usually Mid-Peak)
		if assert.NoError(t, err) {
			// Billed at Summer Off-Peak rate ($0.1555) due to holiday shifting
			assert.InDelta(t, 0.1555, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Residential TOU EV Export Schemes", func(t *testing.T) {
		// Test with Solar Net Billing
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bwp",
			UtilityRate:     "bwp_res_tou_ev",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// On-peak hour (should consume at $0.3902 but export credit should be avoided cost $0.0455)
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ptLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3902, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0455, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})
}
