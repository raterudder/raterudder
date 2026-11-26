package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDTEElectric(t *testing.T) {
	ctx := context.Background()

	t.Run("Rate D1.11 Standard TOU Seasonal Prices PSCR Transition and Rider 18 vs Rider 16", func(t *testing.T) {
		settingsRider18 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_11",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsRider18))

		// Jan–Feb 2026 Oct–May On-Peak (Wednesday Feb 18, 2026 at 16:00 ET, PSCR $0.00250)
		p, err := u.priceForTime(time.Date(2026, time.February, 18, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.False(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.10319+0.00250, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.09726+0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.10319+0.00250+0.09726+0.012270, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.10319+0.00250, p.ExportRateDollars(settingsRider18, nil), 1e-6)

		// Exact PSCR transition boundary on Sunday March 1, 2026 at 00:00 ET (PSCR $0.01877)
		pBeforeMar, err := u.priceForTime(time.Date(2026, time.February, 28, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pBeforeMar.PeriodName)
		assert.InDelta(t, 0.08709+0.00250, pBeforeMar.DollarsPerKWH, 1e-6)

		pAfterMar, err := u.priceForTime(time.Date(2026, time.March, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pAfterMar.PeriodName)
		assert.InDelta(t, 0.08709+0.01877, pAfterMar.DollarsPerKWH, 1e-6)

		// Mar–May 2026 Oct–May On-Peak (Wednesday March 18, 2026 at 15:00 ET, PSCR $0.01877)
		p, err = u.priceForTime(time.Date(2026, time.March, 18, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.10319+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.10319+0.01877+0.09726+0.012270, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.10319+0.01877, p.ExportRateDollars(settingsRider18, nil), 1e-6)

		// Jun–Sep 2026 Summer Weekday On-Peak (Monday June 15, 2026 at 17:00 ET) & Off-Peak (19:00 ET)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.14407+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.14407+0.01877+0.09726+0.012270, p.ImportRateDollars(), 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.08709+0.01877, p.DollarsPerKWH, 1e-6)

		// Summer Weekend at 16:00 ET -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.June, 14, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.08709+0.01877, p.DollarsPerKWH, 1e-6)

		// Weekday peak stays active on holidays for D1.11 (Labor Day Monday Sep 7, 2026 & Monday Jul 5, 2027)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.14407+0.01877, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.14407+0.01877, p.DollarsPerKWH, 1e-6)

		// Rider 16 includes distribution in DollarsPerKWH / ExportRateDollars and keeps surcharges GridAdditional
		settingsRider16 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_11",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rider_16",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsRider16))

		p, err = u.priceForTime(time.Date(2027, time.August, 11, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.14407+0.01877+0.09726, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.14407+0.01877+0.09726+0.012270, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.14407+0.01877+0.09726, p.ExportRateDollars(settingsRider16, nil), 1e-6)
		assert.InDelta(t, 0.14407+0.01877+0.09726, p.BatteryExportRateDollars(settingsRider16, nil), 1e-6)
	})

	t.Run("Rate D1.2 Enhanced TOU Jun-Oct vs Nov-May and Rider 18 vs Rider 16", func(t *testing.T) {
		settingsRider18 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rider_18",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsRider18))

		// Jan 2026 Winter On-Peak (11:00–19:00 Mon–Fri, PSCR $0.00250)
		p, err := u.priceForTime(time.Date(2026, time.January, 14, 11, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.13687+0.00250, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.09726+0.011374, p.GridUseDollarsPerKWH, 1e-6)

		// October is Summer on D1.2 (Tuesday Oct 20, 2026 at 12:00 ET -> $0.16203 supply; 20:00 ET -> $0.05456 supply)
		p, err = u.priceForTime(time.Date(2026, time.October, 20, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.16203+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.16203+0.01877, p.ExportRateDollars(settingsRider18, nil), 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 20, 20, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.05456+0.01877, p.DollarsPerKWH, 1e-6)

		// November transitions to Winter on D1.2 (Monday Nov 2, 2026 at 14:00 ET -> $0.13687; 10:00 ET -> $0.05241)
		p, err = u.priceForTime(time.Date(2026, time.November, 2, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.13687+0.01877, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 2, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.05241+0.01877, p.DollarsPerKWH, 1e-6)

		// Holiday on weekday remains On-Peak for D1.2 (Thanksgiving Thursday Nov 25, 2027 at 13:00 ET)
		p, err = u.priceForTime(time.Date(2027, time.November, 25, 13, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.13687+0.01877, p.DollarsPerKWH, 1e-6)

		// Rider 16 on D1.2 credits distribution ($0.09726) alongside TOU supply + PSCR
		settingsRider16 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_2",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rider_16",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsRider16))

		p, err = u.priceForTime(time.Date(2027, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.16203+0.01877+0.09726, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.011374, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.16203+0.01877+0.09726, p.ExportRateDollars(settingsRider16, nil), 1e-6)
	})

	t.Run("Rate D1.13 Overnight Savers TOU Distribution Rider 18 vs Rider 16 and DST Transitions", func(t *testing.T) {
		settingsRider18 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_13",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rider_18",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsRider18))

		// Summer Weekday (Monday July 13, 2026, PSCR $0.01877)
		// 00:00–01:00 -> Off-Peak ($0.10784 supply, $0.14676 dist)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10784+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.14676+0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.10784+0.01877+0.14676+0.012270, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.10784+0.01877, p.ExportRateDollars(settingsRider18, nil), 1e-6)

		// 01:00–07:00 -> Super Off-Peak ($0.06852 supply, $0.04892 dist)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 3, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.06852+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04892+0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06852+0.01877+0.04892+0.012270, p.ImportRateDollars(), 1e-6)

		// 15:00–19:00 -> On-Peak ($0.15991 supply, $0.19568 dist)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.15991+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.19568+0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.15991+0.01877+0.19568+0.012270, p.ImportRateDollars(), 1e-6)

		// Summer Weekend (Sunday July 12, 2026): 02:00 Super Off-Peak, 16:00 Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.July, 12, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.06852+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04892+0.012270, p.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 12, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10784+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.14676+0.012270, p.GridUseDollarsPerKWH, 1e-6)

		// Oct–May Weekday (Thursday Dec 10, 2026) & Holiday Weekday (Christmas Friday Dec 25, 2026)
		p, err = u.priceForTime(time.Date(2026, time.December, 10, 4, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.06852+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.04892+0.012270, p.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 10, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.08214+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.07338+0.012270, p.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 25, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.09274+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.09784+0.012270, p.GridUseDollarsPerKWH, 1e-6)

		// Rider 16 on D1.13 credits TOU distribution in DollarsPerKWH / ExportRateDollars
		settingsRider16 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_13",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rider_16",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsRider16))

		p, err = u.priceForTime(time.Date(2027, time.June, 16, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.15991+0.01877+0.19568, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.15991+0.01877+0.19568+0.012270, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.15991+0.01877+0.19568, p.ExportRateDollars(settingsRider16, nil), 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.June, 16, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.06852+0.01877+0.04892, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.012270, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.06852+0.01877+0.04892, p.ExportRateDollars(settingsRider16, nil), 1e-6)

		// Verify GetConfirmedPrices across 2026-03-01 PSCR transition, spring-forward DST (2026-03-08 & 2027-03-14),
		// and fall-back DST (2026-11-01 & 2027-11-07) has contiguous non-overlapping hourly prices.
		require.NoError(t, u.ApplySettings(ctx, settingsRider18))
		for _, windowStart := range []time.Time{
			time.Date(2026, time.February, 28, 0, 0, 0, 0, etLocation),
			time.Date(2026, time.March, 7, 12, 0, 0, 0, etLocation),
			time.Date(2026, time.October, 31, 12, 0, 0, 0, etLocation),
			time.Date(2027, time.March, 13, 12, 0, 0, 0, etLocation),
			time.Date(2027, time.November, 6, 12, 0, 0, 0, etLocation),
		} {
			windowEnd := windowStart.Add(48 * time.Hour)
			prices, cErr := u.GetConfirmedPrices(ctx, windowStart, windowEnd)
			require.NoError(t, cErr)
			require.NotEmpty(t, prices)
			assert.True(t, prices[0].TSStart.Equal(windowStart))
			assert.True(t, prices[len(prices)-1].TSEnd.Equal(windowEnd))
			for i := 1; i < len(prices); i++ {
				assert.True(t, prices[i].TSStart.Equal(prices[i-1].TSEnd), "expected contiguous price segments at %v", prices[i].TSStart)
			}
		}
	})

	t.Run("Rate D1.8 Dynamic Peak Pricing and Section C11 Holidays", func(t *testing.T) {
		holidays2026 := getDTESectionC11Holidays(2026)
		assert.ElementsMatch(t, []string{
			"2026-01-01", // New Year's Day (Thu)
			"2026-04-03", // Good Friday (Fri)
			"2026-05-25", // Memorial Day (Mon)
			"2026-07-04", // Independence Day (Sat, not shifted to Fri)
			"2026-09-07", // Labor Day (Mon)
			"2026-11-26", // Thanksgiving Day (Thu)
			"2026-12-25", // Christmas Day (Fri)
		}, holidays2026)

		holidays2027 := getDTESectionC11Holidays(2027)
		assert.ElementsMatch(t, []string{
			"2027-01-01", // New Year's Day (Fri)
			"2027-03-26", // Good Friday (Fri)
			"2027-05-31", // Memorial Day (Mon)
			"2027-07-05", // Independence Day observed (Sun Jul 4 -> Mon Jul 5)
			"2027-09-06", // Labor Day (Mon)
			"2027-11-25", // Thanksgiving Day (Thu)
			"2027-12-25", // Christmas Day (Sat, not shifted to Fri)
		}, holidays2027)

		settingsRider18 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_8",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsRider18))

		// Regular non-holiday weekday (Wednesday July 15, 2026, PSCR $0.01877)
		// 02:00 -> Off-Peak ($0.04583 supply)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04583+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.09726+0.011851, p.GridUseDollarsPerKWH, 1e-6)

		// 10:00 -> Mid-Peak ($0.08884 supply)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", p.PeriodName)
		assert.InDelta(t, 0.08884+0.01877, p.DollarsPerKWH, 1e-6)

		// 16:00 -> On-Peak ($0.17198 supply)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17198+0.01877, p.DollarsPerKWH, 1e-6)

		// 21:00 -> Mid-Peak ($0.08884 supply)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", p.PeriodName)
		assert.InDelta(t, 0.08884+0.01877, p.DollarsPerKWH, 1e-6)

		// 23:00 -> Off-Peak ($0.04583 supply)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04583+0.01877, p.DollarsPerKWH, 1e-6)

		// Good Friday 2026-04-03 at 16:00 ET -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.April, 3, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04583+0.01877, p.DollarsPerKWH, 1e-6)

		// Friday July 3, 2026 (day before Saturday July 4) is NOT a Section C11 Monday holiday -> On-Peak at 16:00
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17198+0.01877, p.DollarsPerKWH, 1e-6)

		// Non-C11 holidays (MLK Day Jan 19, 2026 & Juneteenth Jun 19, 2026) remain On-Peak at 16:00
		p, err = u.priceForTime(time.Date(2026, time.January, 19, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17198+0.00250, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 19, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17198+0.01877, p.DollarsPerKWH, 1e-6)

		// Sunday July 4, 2027 shifts to Monday July 5, 2027 -> Off-Peak all day on 2027-07-05
		p, err = u.priceForTime(time.Date(2027, time.July, 5, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04583+0.01877, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 11, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04583+0.01877, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Rate D1.9 Electric Vehicle TOU and Rate D1 Tier 1 Flat", func(t *testing.T) {
		u := &genericTOU{}
		settingsD19 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_9",
		}
		require.NoError(t, u.ApplySettings(ctx, settingsD19))

		// Weekday 09:00–23:00 On-Peak (Memorial Day Monday May 25, 2026 at 09:00 & 22:00 -> On-Peak)
		p, err := u.priceForTime(time.Date(2026, time.May, 25, 9, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17359+0.01877, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.09726+0.011439, p.GridUseDollarsPerKWH, 1e-6)

		// Weekday 23:00–09:00 Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.May, 25, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04340+0.01877, p.DollarsPerKWH, 1e-6)

		// Weekend at 14:00 -> Off-Peak
		p, err = u.priceForTime(time.Date(2027, time.April, 10, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.04340+0.01877, p.DollarsPerKWH, 1e-6)

		// Rate D1 Tier 1 Flat under Rider 18 & Rider 16
		settingsD1Rider18 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1",
		}
		require.NoError(t, u.ApplySettings(ctx, settingsD1Rider18))

		// Jan 2026 ($0.08782 + $0.00250 PSCR)
		p, err = u.priceForTime(time.Date(2026, time.January, 10, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Empty(t, p.PeriodName)
		assert.InDelta(t, 0.08782+0.00250, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.09726+0.011406, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.08782+0.00250, p.ExportRateDollars(settingsD1Rider18, nil), 1e-6)

		// Mar 2026+ & 2027 under Rider 16 ($0.08782 + $0.01877 PSCR + $0.09726 dist)
		settingsD1Rider16 := types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "rider_16",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsD1Rider16))

		p, err = u.priceForTime(time.Date(2027, time.September, 22, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.08782+0.01877+0.09726, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.011406, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.08782+0.01877+0.09726+0.011406, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.08782+0.01877+0.09726, p.ExportRateDollars(settingsD1Rider16, nil), 1e-6)
	})

	t.Run("Hourly Continuity Across 2026-2027 GetPeriods Map Integration and Validation", func(t *testing.T) {
		start2026 := time.Date(2026, time.January, 1, 0, 0, 0, 0, etLocation)
		end2028 := time.Date(2028, time.January, 1, 0, 0, 0, 0, etLocation)

		for _, plan := range []string{"dte_d1_11", "dte_d1_2", "dte_d1_13", "dte_d1_8", "dte_d1_9", "dte_d1"} {
			for _, scheme := range []string{"rider_18", "rider_16"} {
				fees, err := dtePeriods(plan, types.UtilityRateOptions{NetMeteringScheme: scheme}, []int{2026, 2027})
				require.NoError(t, err)
				require.NotEmpty(t, fees)

				for ts := start2026; ts.Before(end2028); ts = ts.Add(time.Hour) {
					baseCount := 0
					additionalCount := 0
					for i := range fees {
						ok, _, cErr := fees[i].Contains(ts)
						if cErr != nil {
							require.NoError(t, cErr)
						}
						if ok {
							if fees[i].GridAdditional {
								additionalCount++
							} else {
								baseCount++
							}
						}
					}
					if baseCount != 1 || additionalCount != 1 {
						require.Equal(t, 1, baseCount, "plan %s (%s) at %v must match 1 base period", plan, scheme, ts)
						require.Equal(t, 1, additionalCount, "plan %s (%s) at %v must match 1 GridAdditional period", plan, scheme, ts)
					}
				}
			}
		}

		// Verify Map.Site integration and GetPeriods
		m := Configured(nil)
		provider, err := m.Site(ctx, "site-dte", types.Settings{
			UtilityProvider: "dte",
			UtilityRate:     "dte_d1_13",
		})
		require.NoError(t, err)
		require.NotNil(t, provider)

		periods, err := provider.GetPeriods(ctx)
		require.NoError(t, err)
		periodNames := make(map[string]bool)
		for _, p := range periods {
			periodNames[p.Name] = true
		}
		assert.True(t, periodNames["On-Peak"])
		assert.True(t, periodNames["Off-Peak"])
		assert.True(t, periodNames["Super Off-Peak"])

		// Validation errors
		_, err = dtePeriods("dte_unknown", types.UtilityRateOptions{}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown DTE rate plan")

		_, err = dtePeriods("dte_d1_11", types.UtilityRateOptions{NetMeteringScheme: "invalid"}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown DTE net metering scheme")
	})
}
