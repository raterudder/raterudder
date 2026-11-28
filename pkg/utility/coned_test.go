package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConEdPeriods(t *testing.T) {
	ctx := context.Background()

	t.Run("SC 1 Rate I Flat Tier 1 Effective Date Transitions Zonal Pricing and Rider R 1:1 Export", func(t *testing.T) {
		// Default empty location resolves to NYC (NYISO Zone J)
		settingsNYC := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate1",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsNYC))

		nycR25_2026 := 0.005155 + 0.005041 - 0.003745 // $0.006451/kWh
		nycR25_2027 := 0.004672 + 0.005041 - 0.003745 // $0.005968/kWh
		nycR26 := 0.024207

		// January 2026 baseline ($0.16107/kWh delivery) right before Feb 1, 2026 RY1 cutover
		pJan, err := u.priceForTime(time.Date(2026, time.January, 31, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.False(t, pJan.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pJan.GridUseDollarsPerKWH, 1e-6)
		expectedJanNYC := 0.16107 + 0.115526 + nycR25_2026 + nycR26
		assert.InDelta(t, expectedJanNYC, pJan.DollarsPerKWH, 1e-6)
		assert.InDelta(t, expectedJanNYC, pJan.ImportRateDollars(), 1e-6)
		assert.InDelta(t, expectedJanNYC, pJan.ExportRateDollars(settingsNYC, nil), 1e-6)
		assert.InDelta(t, expectedJanNYC, pJan.BatteryExportRateDollars(settingsNYC, nil), 1e-6)

		// Rate Year 1 effective February 1, 2026 at 00:00 ET ($0.16402/kWh delivery)
		pFeb, err := u.priceForTime(time.Date(2026, time.February, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		expectedRY1NYC := 0.16402 + 0.115526 + nycR25_2026 + nycR26
		assert.InDelta(t, expectedRY1NYC, pFeb.DollarsPerKWH, 1e-6)
		assert.InDelta(t, expectedRY1NYC, pFeb.ExportRateDollars(settingsNYC, nil), 1e-6)

		// Rate Year 2 effective January 1, 2027 at 00:00 ET ($0.17246/kWh delivery & $0.004672 MFC)
		p2027, err := u.priceForTime(time.Date(2027, time.January, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		expectedRY2NYC := 0.17246 + 0.115526 + nycR25_2027 + nycR26
		assert.InDelta(t, expectedRY2NYC, p2027.DollarsPerKWH, 1e-6)
		assert.InDelta(t, expectedRY2NYC, p2027.ExportRateDollars(settingsNYC, nil), 1e-6)

		// Westchester (NYISO Zone I)
		settingsWestchester := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate1",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "westchester",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsWestchester))
		westR25_2026 := 0.005155 + 0.005041 + 0.014722 // $0.024918/kWh
		westR26 := 0.024307
		pWest, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.16402+0.094260+westR25_2026+westR26, pWest.DollarsPerKWH, 1e-6)

		// Upper Westchester (NYISO Zone H)
		settingsUpperWestchester := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate1",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "upper_westchester",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsUpperWestchester))
		pUpperWest, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.16402+0.089572+westR25_2026+westR26, pUpperWest.DollarsPerKWH, 1e-6)
	})

	t.Run("SC 1 Rate III Voluntary TOU Daily Peaks Summer Super-Peak Capacity Holidays and Transitions", func(t *testing.T) {
		settingsNYC := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate3",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "nyc",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsNYC))

		nycRiders2026 := (0.005155 + 0.005041 - 0.003745) + 0.024207 // $0.030658/kWh
		nycRiders2027 := (0.004672 + 0.005041 - 0.003745) + 0.024207 // $0.030175/kWh

		// January 2026 Winter On-Peak ($0.1305 delivery) & Off-Peak ($0.0249 delivery)
		// New Year's Day (Thursday Jan 1, 2026) still bills On-Peak from 08:00-24:00 per Leaf 389.1 ("including holidays")
		pJanHolPeak, err := u.priceForTime(time.Date(2026, time.January, 1, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pJanHolPeak.PeriodName)
		assert.InDelta(t, 0.1305+0.074331+nycRiders2026, pJanHolPeak.DollarsPerKWH, 1e-6)

		pJanOffPeak, err := u.priceForTime(time.Date(2026, time.January, 15, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pJanOffPeak.PeriodName)
		assert.InDelta(t, 0.0249+0.068771+nycRiders2026, pJanOffPeak.DollarsPerKWH, 1e-6)

		// RY1 Winter after Feb 1, 2026 ($0.1711 On-Peak, $0.0522 Off-Peak)
		pFebOnPeak, err := u.priceForTime(time.Date(2026, time.February, 1, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pFebOnPeak.PeriodName)
		assert.InDelta(t, 0.1711+0.074331+nycRiders2026, pFebOnPeak.DollarsPerKWH, 1e-6)

		// Summer Weekday (Wednesday July 15, 2026):
		// 00:00-08:00 Off-Peak, 08:00-14:00 On-Peak, 14:00-18:00 Super-Peak (+$0.79957 MSC-Capacity), 18:00-24:00 On-Peak
		pSumOff, err := u.priceForTime(time.Date(2026, time.July, 15, 3, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pSumOff.PeriodName)
		assert.InDelta(t, 0.0522+0.068771+nycRiders2026, pSumOff.DollarsPerKWH, 1e-6)

		pSumMorningOn, err := u.priceForTime(time.Date(2026, time.July, 15, 13, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pSumMorningOn.PeriodName)
		assert.InDelta(t, 0.2786+0.074331+nycRiders2026, pSumMorningOn.DollarsPerKWH, 1e-6)

		pSumSuperPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super-Peak", pSumSuperPeak.PeriodName)
		expectedNYCSuperPeak := 0.2786 + 0.074331 + 0.79957 + nycRiders2026
		assert.InDelta(t, expectedNYCSuperPeak, pSumSuperPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, expectedNYCSuperPeak, pSumSuperPeak.ExportRateDollars(settingsNYC, nil), 1e-6)

		pSumEveningOn, err := u.priceForTime(time.Date(2026, time.July, 15, 18, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pSumEveningOn.PeriodName)
		assert.InDelta(t, 0.2786+0.074331+nycRiders2026, pSumEveningOn.DollarsPerKWH, 1e-6)

		// Summer Weekend (Saturday July 18, 2026 at 15:00 ET): On-Peak without weekday Super-Peak MSC-Capacity
		pSumWeekend, err := u.priceForTime(time.Date(2026, time.July, 18, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pSumWeekend.PeriodName)
		assert.InDelta(t, 0.2786+0.074331+nycRiders2026, pSumWeekend.DollarsPerKWH, 1e-6)

		// Labor Day (Monday Sep 7, 2026 at 15:00 ET): Rate III includes holidays in weekday Super-Peak / On-Peak
		pLaborDaySuper, err := u.priceForTime(time.Date(2026, time.September, 7, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super-Peak", pLaborDaySuper.PeriodName)
		assert.InDelta(t, expectedNYCSuperPeak, pLaborDaySuper.DollarsPerKWH, 1e-6)

		// RY2 Summer 2027 in Upper Westchester (Zone H): $0.2930 delivery + $0.072184 MSC + $0.32504 Super-Peak Cap + Westchester riders
		settingsUpperWest := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate3",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "upper_westchester",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsUpperWest))
		westRiders2027 := (0.004672 + 0.005041 + 0.014722) + 0.024307 // $0.048742/kWh
		p2027SuperWest, err := u.priceForTime(time.Date(2027, time.July, 14, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super-Peak", p2027SuperWest.PeriodName)
		assert.InDelta(t, 0.2930+0.072184+0.32504+westRiders2027, p2027SuperWest.DollarsPerKWH, 1e-6)

		// Re-apply NYC for RY2 Winter 2027 check
		require.NoError(t, u.ApplySettings(ctx, settingsNYC))
		p2027WinOn, err := u.priceForTime(time.Date(2027, time.November, 10, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p2027WinOn.PeriodName)
		assert.InDelta(t, 0.1800+0.074331+nycRiders2027, p2027WinOn.DollarsPerKWH, 1e-6)
	})

	t.Run("SC 1 Rate IV Optional Demand-Based 001IPP TOU Supply Surcharges and Unshifted Six Calendar Holidays", func(t *testing.T) {
		settingsNYC := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate4",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "nyc",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsNYC))

		nycR25_2026 := 0.005155 + 0.005041 - 0.003745 // $0.006451/kWh
		nycR25_2027 := 0.004672 + 0.005041 - 0.003745 // $0.005968/kWh
		nycR26Rate4 := 0.01841
		expectedNYCRate4Off2026 := 0.070885 + nycR25_2026 + nycR26Rate4
		expectedNYCRate4Peak2026 := 0.248837 + nycR25_2026 + nycR26Rate4
		expectedNYCRate4Peak2027 := 0.248837 + nycR25_2027 + nycR26Rate4

		// Weekday 12:00-20:00 On-Peak vs 11:00 / 20:00 Off-Peak
		pPrePeak, err := u.priceForTime(time.Date(2026, time.August, 12, 11, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pPrePeak.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pPrePeak.DollarsPerKWH, 1e-6)

		pPeak, err := u.priceForTime(time.Date(2026, time.August, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pPeak.PeriodName)
		assert.InDelta(t, expectedNYCRate4Peak2026, pPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, expectedNYCRate4Peak2026, pPeak.ExportRateDollars(settingsNYC, nil), 1e-6)

		pPostPeak, err := u.priceForTime(time.Date(2026, time.August, 12, 20, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pPostPeak.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pPostPeak.DollarsPerKWH, 1e-6)

		// Six exact calendar holidays (New Year's Day, Memorial Day, Independence Day, Labor Day,
		// Thanksgiving, Christmas) are Off-Peak all day:
		pJanHol, err := u.priceForTime(time.Date(2026, time.January, 1, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pJanHol.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pJanHol.DollarsPerKWH, 1e-6)

		pMemDay, err := u.priceForTime(time.Date(2026, time.May, 25, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pMemDay.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pMemDay.DollarsPerKWH, 1e-6)

		pLaborDay, err := u.priceForTime(time.Date(2026, time.September, 7, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pLaborDay.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pLaborDay.DollarsPerKWH, 1e-6)

		pThanksgiving, err := u.priceForTime(time.Date(2026, time.November, 26, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pThanksgiving.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pThanksgiving.DollarsPerKWH, 1e-6)

		pXmas, err := u.priceForTime(time.Date(2026, time.December, 25, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pXmas.PeriodName)
		assert.InDelta(t, expectedNYCRate4Off2026, pXmas.DollarsPerKWH, 1e-6)

		// Unshifted calendar holiday rule: July 4, 2026 falls on Saturday, so Friday July 3, 2026 remains On-Peak!
		pJul3, err := u.priceForTime(time.Date(2026, time.July, 3, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pJul3.PeriodName)
		assert.InDelta(t, expectedNYCRate4Peak2026, pJul3.DollarsPerKWH, 1e-6)

		// Similarly, July 4, 2027 falls on Sunday, so Monday July 5, 2027 remains 2027 On-Peak!
		pJul5_2027, err := u.priceForTime(time.Date(2027, time.July, 5, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pJul5_2027.PeriodName)
		assert.InDelta(t, expectedNYCRate4Peak2027, pJul5_2027.DollarsPerKWH, 1e-6)

		// MLK Day (Monday Jan 19, 2026) is NOT one of the 6 Con Edison holidays -> On-Peak
		pMLK, err := u.priceForTime(time.Date(2026, time.January, 19, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pMLK.PeriodName)
		assert.InDelta(t, expectedNYCRate4Peak2026, pMLK.DollarsPerKWH, 1e-6)

		// NYC 2027 Rate IV On-Peak ($0.004672 MFC)
		pNYC2027, err := u.priceForTime(time.Date(2027, time.April, 14, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pNYC2027.PeriodName)
		assert.InDelta(t, expectedNYCRate4Peak2027, pNYC2027.DollarsPerKWH, 1e-6)

		// Westchester (Zone I) in 2027: $0.158744 On-Peak MSC + $0.024435 Rule 25 + $0.01851 Rule 26
		settingsWest := types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate4",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "westchester",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsWest))
		westR25_2027 := 0.004672 + 0.005041 + 0.014722
		pWest2027, err := u.priceForTime(time.Date(2027, time.April, 14, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pWest2027.PeriodName)
		assert.InDelta(t, 0.158744+westR25_2027+0.01851, pWest2027.DollarsPerKWH, 1e-6)
	})

	t.Run("DST Transitions 24-Hour Period Coverage and Invalid Option Errors", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate3",
		}))

		// Spring-forward Sunday March 8, 2026 (23 hours)
		springStart := time.Date(2026, time.March, 8, 0, 0, 0, 0, etLocation)
		springEnd := time.Date(2026, time.March, 9, 0, 0, 0, 0, etLocation)
		springPrices, err := u.GetConfirmedPrices(ctx, springStart, springEnd)
		require.NoError(t, err)
		assert.Len(t, springPrices, 23)

		// Fall-back Sunday November 1, 2026 (25 hours)
		fallStart := time.Date(2026, time.November, 1, 0, 0, 0, 0, etLocation)
		fallEnd := time.Date(2026, time.November, 2, 0, 0, 0, 0, etLocation)
		fallPrices, err := u.GetConfirmedPrices(ctx, fallStart, fallEnd)
		require.NoError(t, err)
		assert.Len(t, fallPrices, 25)

		// Verify named TOU periods cover all 24 hours across Rate III and Rate IV
		t.Run("SC 1 Rate III Named Period Coverage", func(t *testing.T) {
			require.NoError(t, u.ApplySettings(ctx, types.Settings{
				UtilityProvider: "coned",
				UtilityRate:     "coned_sc1_rate3",
			}))
			periods, err := u.GetPeriods(ctx)
			require.NoError(t, err)
			assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.July, 15, 0, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.September, 7, 0, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(periods, time.Date(2027, time.January, 1, 0, 0, 0, 0, etLocation)))
			seen := make(map[string]bool)
			for _, p := range periods {
				seen[p.Name] = true
			}
			assert.True(t, seen["Off-Peak"])
			assert.True(t, seen["On-Peak"])
			assert.True(t, seen["Super-Peak"])
		})

		t.Run("SC 1 Rate IV Named Period Coverage", func(t *testing.T) {
			require.NoError(t, u.ApplySettings(ctx, types.Settings{
				UtilityProvider: "coned",
				UtilityRate:     "coned_sc1_rate4",
			}))
			periods, err := u.GetPeriods(ctx)
			require.NoError(t, err)
			assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.July, 15, 0, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(periods, time.Date(2026, time.September, 7, 0, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(periods, time.Date(2027, time.January, 1, 0, 0, 0, 0, etLocation)))
			seen := make(map[string]bool)
			for _, p := range periods {
				seen[p.Name] = true
			}
			assert.True(t, seen["Off-Peak"])
			assert.True(t, seen["On-Peak"])
		})

		// Unknown location error
		err = u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "coned",
			UtilityRate:     "coned_sc1_rate1",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "long_island",
			},
		})
		assert.ErrorContains(t, err, "unknown Con Edison location")

		// Unknown plan error (including ignored legacy Rate II)
		_, err = conedPeriods("coned_sc1_rate2", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.ErrorContains(t, err, "unknown Con Edison rate plan")
		_, err = conedPeriods("coned_sc2", types.UtilityRateOptions{}, []int{2026, 2027})
		assert.ErrorContains(t, err, "unknown Con Edison rate plan")
	})
}
