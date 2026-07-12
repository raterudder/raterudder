package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSRPHolidays(t *testing.T) {
	t.Run("Arizona Holidays 2026", func(t *testing.T) {
		holidays := getSRPHolidays(2026)
		// 2026:
		// New Year's: 2026-01-01
		// Memorial Day: 2026-05-25
		// Independence Day (observed on Fri, July 3 since July 4 is Saturday): 2026-07-03
		// Labor Day: 2026-09-07
		// Thanksgiving: 2026-11-26
		// Christmas: 2026-12-25
		expected := []string{
			"2026-01-01",
			"2026-05-25",
			"2026-07-03",
			"2026-09-07",
			"2026-11-26",
			"2026-12-25",
		}
		assert.Equal(t, expected, holidays)
	})

	t.Run("Arizona Holidays 2027", func(t *testing.T) {
		holidays := getSRPHolidays(2027)
		// 2027:
		// New Year's: 2027-01-01
		// Memorial Day: 2027-05-31
		// Independence Day (observed on Mon, July 5 since July 4 is Sunday): 2027-07-05
		// Labor Day: 2027-09-06
		// Thanksgiving: 2027-11-25
		// Christmas (observed on Fri, Dec 24 since Dec 25 is Saturday): 2027-12-24
		// New Year's 2028 (observed on Dec 31, 2027 since Jan 1, 2028 is Saturday): 2027-12-31
		expected := []string{
			"2027-01-01",
			"2027-05-31",
			"2027-07-05",
			"2027-09-06",
			"2027-11-25",
			"2027-12-24",
			"2027-12-31",
		}
		assert.Equal(t, expected, holidays)
	})
}

func TestSRPRates(t *testing.T) {
	phoenix := mstLocation

	t.Run("E-13 TOU Export Plan 2026 Discounted vs 2027 Normal", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e13",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// 1. Summer On-Peak 2026 (July 15, 2026 3:00 PM MST) - Should have 2026 discount of $0.0038
		// Base: $0.2338 -> Discounted: $0.2300
		t2026Peak, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.2300, t2026Peak.DollarsPerKWH, 1e-6)

		// 2. Summer On-Peak 2027 (July 15, 2027 3:00 PM MST) - No discount
		// Base: $0.2338
		t2027Peak, err := u.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.2338, t2027Peak.DollarsPerKWH, 1e-6)

		// 3. Winter On-Peak 2026 (December 15, 2026 6:00 AM MST) - Winter is not discounted
		// Base: $0.1425
		t2026WinterPeak, err := u.priceForTime(time.Date(2026, time.December, 15, 6, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1425, t2026WinterPeak.DollarsPerKWH, 1e-6)
	})

	t.Run("E-14 EV Export Plan 2026 Discounted vs 2027 Normal", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e14",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// 1. Summer Super Off-Peak 2026 (June 15, 2026 2:00 AM MST) - Discounted
		// Base: $0.0793 -> Discounted: $0.0755
		t2026SOP, err := u.priceForTime(time.Date(2026, time.June, 15, 2, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0755, t2026SOP.DollarsPerKWH, 1e-6)

		// 2. Summer Super Off-Peak 2027 (June 15, 2027 2:00 AM MST) - No discount
		// Base: $0.0793
		t2027SOP, err := u.priceForTime(time.Date(2027, time.June, 15, 2, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0793, t2027SOP.DollarsPerKWH, 1e-6)
	})

	t.Run("E-16 Demand Plan TOU Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e16",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// On-peak: 5 p.m. to 10 p.m. weekdays
		// Super off-peak: 8 a.m. to 3 p.m. daily
		// Summer Peak On-Peak 2027: $0.1616
		t2027Peak, err := u.priceForTime(time.Date(2027, time.July, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1616, t2027Peak.DollarsPerKWH, 1e-6)

		// Summer Peak Super Off-Peak 2027: $0.0584
		t2027SOP, err := u.priceForTime(time.Date(2027, time.July, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0584, t2027SOP.DollarsPerKWH, 1e-6)
	})

	t.Run("E-28 Conserve Plan TOU Rates", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e28",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// On-peak: 6 p.m. to 9 p.m. weekdays
		// Super off-peak: 8 a.m. to 3 p.m. daily
		// Summer Peak On-Peak 2027: $0.3982
		t2027Peak, err := u.priceForTime(time.Date(2027, time.July, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.3982, t2027Peak.DollarsPerKWH, 1e-6)

		// Summer Peak Super Off-Peak 2027: $0.0623
		t2027SOP, err := u.priceForTime(time.Date(2027, time.July, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0623, t2027SOP.DollarsPerKWH, 1e-6)
	})

	t.Run("E-27 Customer Gen Plan 2026 Discounted vs 2027 Normal", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e27",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		// 1. Summer On-Peak 2026 (July 15, 2026 3:00 PM MST) - Should have 2026 discount of $0.0038
		// Base: $0.0823 -> Discounted: $0.0785
		t2026Peak, err := u.priceForTime(time.Date(2026, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0785, t2026Peak.DollarsPerKWH, 1e-6)

		// 2. Summer On-Peak 2027 (July 15, 2027 3:00 PM MST) - No discount
		// Base: $0.0823
		t2027Peak, err := u.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0823, t2027Peak.DollarsPerKWH, 1e-6)

		// 3. Winter On-Peak 2026 (December 15, 2026 6:00 AM MST) - Winter is not discounted
		// Base: $0.0673
		t2026WinterPeak, err := u.priceForTime(time.Date(2026, time.December, 15, 6, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0673, t2026WinterPeak.DollarsPerKWH, 1e-6)
	})
}

func TestSRPExportCredits(t *testing.T) {
	phoenix := mstLocation

	t.Run("E-13 Net Billing vs Net Metering Export Credits", func(t *testing.T) {
		// Net Billing (default/net_billing)
		uBilling := &genericTOU{}
		err := uBilling.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e13",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		pBilling, err := uBilling.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.True(t, pBilling.SeparateGenerationCredit)
		assert.InDelta(t, 0.0345, pBilling.GenerationCreditDollarsPerKWH, 1e-6)

		// Net Metering (1:1 retail credit)
		uNet := &genericTOU{}
		err = uNet.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e13",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNet, err := uNet.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.False(t, pNet.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pNet.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("E-16 Net Billing vs Net Metering Export Credits", func(t *testing.T) {
		// Net Billing
		uBilling := &genericTOU{}
		err := uBilling.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e16",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		pBilling, err := uBilling.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.True(t, pBilling.SeparateGenerationCredit)
		assert.InDelta(t, 0.0187, pBilling.GenerationCreditDollarsPerKWH, 1e-6)

		// Net Metering (1:1 retail credit)
		uNet := &genericTOU{}
		err = uNet.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e16",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNet, err := uNet.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.False(t, pNet.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pNet.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("E-27 Net Metering Export Credits", func(t *testing.T) {
		uNet := &genericTOU{}
		err := uNet.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e27",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNet, err := uNet.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.False(t, pNet.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pNet.GenerationCreditDollarsPerKWH, 1e-6)

		// Even if options request net_billing, E-27 should override to net metering
		uNetBilling := &genericTOU{}
		err = uNetBilling.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e27",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		pNetBilling, err := uNetBilling.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.False(t, pNetBilling.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pNetBilling.GenerationCreditDollarsPerKWH, 1e-6)
	})
}
