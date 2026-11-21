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

	t.Run("E-16 Demand Plan 2026 Discounted vs 2027 Normal", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e16",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// On-Peak: 5 p.m. to 10 p.m. weekdays, except holidays
		// Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
		// Summer base (May, Jun, Sep, Oct): On-Peak $0.1257, Off-Peak $0.0995, Super Off-Peak $0.0393
		// Summer Peak base (Jul, Aug): On-Peak $0.1654, Off-Peak $0.0996, Super Off-Peak $0.0622
		// Winter base (Nov–Apr): On-Peak $0.1119, Off-Peak $0.0994, Super Off-Peak $0.0438

		// 1. 2026 Summer (discounted -$0.0038/kWh): Mon Jun 15, Sat Jun 20, Memorial Day Mon May 25, Wed Sep 16, Labor Day Mon Sep 7
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1219, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 16, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0957, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0355, p.DollarsPerKWH, 1e-6)

		// Weekend shifts On-Peak window (6 p.m.) to Off-Peak while keeping Super Off-Peak (10 a.m.)
		p, err = u.priceForTime(time.Date(2026, time.June, 20, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0957, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 20, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0355, p.DollarsPerKWH, 1e-6)

		// Holiday (Memorial Day Mon May 25, 2026 & Labor Day Mon Sep 7, 2026) shifts On-Peak to Off-Peak and keeps Super Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.May, 25, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0957, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.May, 25, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0355, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 16, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1219, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 7, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0957, p.DollarsPerKWH, 1e-6)

		// 2. 2026 Summer Peak (discounted -$0.0038/kWh): Wed Jul 15, Sat Jul 18, Observed Independence Day Fri Jul 3
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1616, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 20, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1616, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 22, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0958, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0584, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 18, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0958, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 3, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0958, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 3, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0584, p.DollarsPerKWH, 1e-6)

		// 3. 2027 Summer (un-discounted base rates): Tue Jun 15, Sat Jun 19, Memorial Day Mon May 31, Wed Sep 15, Labor Day Mon Sep 6
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1257, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 15, 16, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0995, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0393, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 19, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0995, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.May, 31, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0995, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.May, 31, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0393, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.September, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1257, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.September, 6, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0995, p.DollarsPerKWH, 1e-6)

		// 4. 2027 Summer Peak (un-discounted base rates): Thu Jul 15, Sat Jul 17, Observed Independence Day Mon Jul 5
		p, err = u.priceForTime(time.Date(2027, time.July, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1654, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 15, 22, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0996, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0622, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 17, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0996, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0996, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0622, p.DollarsPerKWH, 1e-6)

		// 5. Winter (not discounted in 2026 or 2027): Tue Dec 15, 2026, Sat Dec 19, 2026, Thanksgiving Thu Nov 26, 2026
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1119, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 15, 22, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0994, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0438, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 19, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0994, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 26, 18, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0994, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 26, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0438, p.DollarsPerKWH, 1e-6)
	})

	t.Run("E-28 Conserve Plan 2026 Discounted vs 2027 Normal", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e28",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net_billing",
			},
		})
		require.NoError(t, err)

		// On-Peak: 6 p.m. to 9 p.m. weekdays, except holidays
		// Super Off-Peak: 8 a.m. to 3 p.m. daily, including holidays
		// Summer base (May, Jun, Sep, Oct): On-Peak $0.1885, Off-Peak $0.1506, Super Off-Peak $0.0395
		// Summer Peak base (Jul, Aug): On-Peak $0.4020, Off-Peak $0.1276, Super Off-Peak $0.0661
		// Winter base (Nov–Apr): On-Peak $0.1508, Off-Peak $0.1355, Super Off-Peak $0.0432

		// 1. 2026 Summer (discounted -$0.0038/kWh): Mon Jun 15, Sat Jun 20, Memorial Day Mon May 25, Wed Sep 16, Labor Day Mon Sep 7
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1847, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1468, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0357, p.DollarsPerKWH, 1e-6)

		// Weekend shifts On-Peak window (7 p.m.) to Off-Peak while keeping Super Off-Peak (10 a.m.)
		p, err = u.priceForTime(time.Date(2026, time.June, 20, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1468, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 20, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0357, p.DollarsPerKWH, 1e-6)

		// Holiday (Memorial Day Mon May 25, 2026 & Labor Day Mon Sep 7, 2026) shifts On-Peak to Off-Peak and keeps Super Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.May, 25, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1468, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.May, 25, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0357, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 16, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1847, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 7, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1468, p.DollarsPerKWH, 1e-6)

		// 2. 2026 Summer Peak (discounted -$0.0038/kWh): Wed Jul 15, Sat Jul 18, Observed Independence Day Fri Jul 3
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.3982, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 21, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1238, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0623, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 18, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1238, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 3, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1238, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 3, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0623, p.DollarsPerKWH, 1e-6)

		// 3. 2027 Summer (un-discounted base rates): Tue Jun 15, Sat Jun 19, Memorial Day Mon May 31, Wed Sep 15, Labor Day Mon Sep 6
		p, err = u.priceForTime(time.Date(2027, time.June, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1885, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 15, 17, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1506, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0395, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 19, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1506, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.May, 31, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1506, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.May, 31, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0395, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.September, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1885, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.September, 6, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1506, p.DollarsPerKWH, 1e-6)

		// 4. 2027 Summer Peak (un-discounted base rates): Thu Jul 15, Sat Jul 17, Observed Independence Day Mon Jul 5
		p, err = u.priceForTime(time.Date(2027, time.July, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.4020, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 15, 21, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1276, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0661, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 17, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1276, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1276, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0661, p.DollarsPerKWH, 1e-6)

		// 5. Winter (not discounted in 2026 or 2027): Tue Dec 15, 2026, Sat Dec 19, 2026, Thanksgiving Thu Nov 26, 2026
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1508, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 15, 21, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1355, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 15, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0432, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 19, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1355, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 26, 19, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.1355, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 26, 10, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0432, p.DollarsPerKWH, 1e-6)
	})

	t.Run("E-15 Customer Gen Avg Demand Plan 2026 Discounted vs 2027 Normal", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e15",
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
		// Base: $0.0674
		t2026WinterPeak, err := u.priceForTime(time.Date(2026, time.December, 15, 6, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.InDelta(t, 0.0674, t2026WinterPeak.DollarsPerKWH, 1e-6)
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

	t.Run("E-28 Net Billing vs Net Metering Export Credits", func(t *testing.T) {
		// Net Billing
		uBilling := &genericTOU{}
		err := uBilling.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e28",
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
			UtilityRate:     "srp_e28",
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

	t.Run("E-15 Net Metering Export Credits", func(t *testing.T) {
		uNet := &genericTOU{}
		err := uNet.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e15",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		pNet, err := uNet.priceForTime(time.Date(2027, time.July, 15, 15, 0, 0, 0, phoenix))
		require.NoError(t, err)
		assert.False(t, pNet.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pNet.GenerationCreditDollarsPerKWH, 1e-6)

		// Even if options request net_billing, E-15 should override to net metering
		uNetBilling := &genericTOU{}
		err = uNetBilling.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "srp",
			UtilityRate:     "srp_e15",
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
