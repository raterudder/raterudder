package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEPB(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	ct, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	t.Run("Utility Registry Metadata", func(t *testing.T) {
		utilities := Configured(nil).ListUtilities()
		var epb *types.UtilityProviderInfo
		for i := range utilities {
			if utilities[i].ID == "epb" {
				epb = &utilities[i]
				break
			}
		}
		require.NotNil(t, epb, "expected epb in Configured(nil).ListUtilities()")
		assert.False(t, epb.Hidden)
		assert.Equal(t, "EPB Chattanooga", epb.Name)
		if assert.Len(t, epb.Rates, 3) {
			assert.Equal(t, "epb_base", epb.Rates[0].ID)
			assert.Equal(t, "epb_time_shift", epb.Rates[1].ID)
			assert.Equal(t, "epb_night_shift", epb.Rates[2].ID)
		}
	})

	t.Run("Base Rate Plan", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
		})
		require.NoError(t, err)

		// July 15, 2026: Schedule RS ($0.09464) + July FCA ($0.02825) = $0.12289
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.09464, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02825, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.12289, p.ImportRateDollars(), 1e-6)

		// August 15, 2026: Schedule RS ($0.09464) + August FCA ($0.02631) = $0.12095
		p, err = u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12095, p.ImportRateDollars(), 1e-6)

		// September 15, 2026: Schedule RS ($0.09464) + September FCA ($0.02535) = $0.11999
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.11999, p.ImportRateDollars(), 1e-6)

		// October 15, 2026: Schedule RS ($0.09464) + October FCA ($0.02164) = $0.11628
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.11628, p.ImportRateDollars(), 1e-6)

		// October 31, 2026 at 11:00 PM EDT -> still October FCA ($0.02164)
		p, err = u.priceForTime(time.Date(2026, time.October, 31, 23, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.02164, p.GridUseDollarsPerKWH, 1e-6)

		// November 1, 2026 at 12:00 AM EDT -> November FCA ($0.02754): $0.09464 + $0.02754 = $0.12218
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 0, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.02754, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.12218, p.ImportRateDollars(), 1e-6)

		// December 15, 2026 & 2027-2028 fallback -> November FCA ($0.02754): $0.12218
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12218, p.ImportRateDollars(), 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.March, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12218, p.ImportRateDollars(), 1e-6)

		// January 15, 2026: Schedule RS ($0.09464) + Jan FCA ($0.03021) = $0.12485
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12485, p.ImportRateDollars(), 1e-6)

		// Historical 2025: Schedule RS ($0.09464) + 2025 FCA ($0.02900) = $0.12364
		p, err = u.priceForTime(time.Date(2025, time.June, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.12364, p.ImportRateDollars(), 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Time Shift Plan", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_time_shift",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.Equal(t, "On-Peak", periods[0].Name)
		}

		// Summer Weekday On-Peak (Monday July 13, 2026 at 4:00 PM) -> $0.17744 + July FCA ($0.02825) = $0.20569
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.20569, p.ImportRateDollars(), 1e-6)

		// Summer Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM) -> $0.08075 + July FCA ($0.02825) = $0.10900
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10900, p.ImportRateDollars(), 1e-6)

		// Summer Weekday On-Peak October (Wednesday October 14, 2026 at 4:00 PM) -> $0.17744 + Oct FCA ($0.02164) = $0.19908
		p, err = u.priceForTime(time.Date(2026, time.October, 14, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.19908, p.ImportRateDollars(), 1e-6)

		// Summer Weekday Off-Peak October (Wednesday October 14, 2026 at 10:00 AM) -> $0.08075 + Oct FCA ($0.02164) = $0.10239
		p, err = u.priceForTime(time.Date(2026, time.October, 14, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10239, p.ImportRateDollars(), 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 4:00 PM) -> $0.08075 + July FCA ($0.02825) = $0.10900
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10900, p.ImportRateDollars(), 1e-6)

		// Summer Holiday (Independence Day observed: Friday July 3, 2026 at 4:00 PM) -> Off-Peak $0.08075 + July FCA ($0.02825) = $0.10900
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.10900, p.ImportRateDollars(), 1e-6)

		// Non-Summer Weekday On-Peak Morning (Monday January 12, 2026 at 7:00 AM) -> $0.17744 + Jan FCA ($0.03021) = $0.20765
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.20765, p.ImportRateDollars(), 1e-6)

		// Non-Summer Weekday Off-Peak (Monday January 12, 2026 at 2:00 PM) -> $0.08075 + Jan FCA ($0.03021) = $0.11096
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.11096, p.ImportRateDollars(), 1e-6)

		// MLK Day (Monday January 19, 2026 at 7:00 AM) is NOT one of Schedule TRS's 6 holidays -> On-Peak $0.17744 + Jan FCA ($0.03021) = $0.20765
		p, err = u.priceForTime(time.Date(2026, time.January, 19, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.20765, p.ImportRateDollars(), 1e-6)

		// Juneteenth (Friday June 19, 2026 at 4:00 PM) & Veterans Day (Wednesday November 11, 2026 at 7:00 AM) -> regular weekday On-Peak
		p, err = u.priceForTime(time.Date(2026, time.June, 19, 16, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17744+0.02793, p.ImportRateDollars(), 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 11, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.17744+0.02754, p.ImportRateDollars(), 1e-6)

		// Thanksgiving Day (Thursday November 26, 2026 at 7:00 AM) -> exempted holiday Off-Peak $0.08075 + Nov FCA ($0.02754) = $0.10829
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.10829, p.ImportRateDollars(), 1e-6)
	})

	t.Run("Night Shift Plan", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_night_shift",
		})
		require.NoError(t, err)

		// Night Hour (Monday July 13, 2026 at 2:00 AM) -> $0.06306 + July FCA ($0.02825) = $0.09131
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.09131, p.ImportRateDollars(), 1e-6)

		// Night Hour (Monday July 13, 2026 at 11:00 PM) -> $0.06306 + July FCA ($0.02825) = $0.09131
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 23, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.09131, p.ImportRateDollars(), 1e-6)

		// Day Hour (Monday July 13, 2026 at 10:00 AM) -> $0.10516 + July FCA ($0.02825) = $0.13341
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.13341, p.ImportRateDollars(), 1e-6)

		// Day Hour (Monday January 12, 2026 at 10:00 AM) -> $0.10516 + Jan FCA ($0.03021) = $0.13537
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.13537, p.ImportRateDollars(), 1e-6)

		// Day Hour (Monday November 16, 2026 at 10:00 AM) -> $0.10516 + Nov FCA ($0.02754) = $0.13270
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.13270, p.ImportRateDollars(), 1e-6)
	})

	t.Run("DPP Part A Export Credits", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dpp_a",
			},
		})
		require.NoError(t, err)

		// January 15, 2026: fallback to May 2026 flat rate of $0.02931
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.02931, p.GenerationCreditDollarsPerKWH, 1e-6)

		// June 15, 2026: flat Part A rate of $0.02930
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.02930, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July 15, 2026: flat Part A rate of $0.03534
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.03534, p.GenerationCreditDollarsPerKWH, 1e-6)

		// August 15, 2026: flat Part A rate of $0.03316
		p, err = u.priceForTime(time.Date(2026, time.August, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.03316, p.GenerationCreditDollarsPerKWH, 1e-6)

		// September 15, 2026: flat Part A rate of $0.02677
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.02677, p.GenerationCreditDollarsPerKWH, 1e-6)

		// October 15, 2026: flat Part A rate of $0.03112
		p, err = u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.03112, p.GenerationCreditDollarsPerKWH, 1e-6)

		// November 1, 2026 at 00:00 EDT (= Oct 31, 23:00 CDT) -> still October DPP Part A ($0.03112)
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 0, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03112, p.GenerationCreditDollarsPerKWH, 1e-6)

		// November 1, 2026 at 01:00 EDT (= Nov 1, 00:00 CDT) -> November DPP Part A ($0.03373)
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 1, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03373, p.GenerationCreditDollarsPerKWH, 1e-6)

		// December 15, 2026 & 2027-2028 fallback -> $0.03373
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03373, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03373, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2028, time.December, 31, 23, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03373, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("DPP Part B Export Credits", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dpp_b",
			},
		})
		require.NoError(t, err)

		// July Weekday Super Peak (Monday July 13, 2026 at 2:00 PM EDT = 1:00 PM CDT) -> $0.04049
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.04049, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekday On-Peak (Monday July 13, 2026 at 1:00 PM EDT = 12:00 PM CDT) -> $0.03962
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 13, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03962, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekday Off-Peak (Monday July 13, 2026 at 10:00 AM EDT = 9:00 AM CDT) -> $0.03424
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03424, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekday Super Off-Peak (Monday July 13, 2026 at 2:00 AM EDT = 1:00 AM CDT) -> $0.02947
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.02947, p.GenerationCreditDollarsPerKWH, 1e-6)

		// July Weekend Off-Peak (Saturday July 18, 2026 at 12:00 PM EDT = 11:00 AM CDT) -> $0.03424
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03424, p.GenerationCreditDollarsPerKWH, 1e-6)

		// June Weekday Super Peak (Monday June 15, 2026 at 2:00 PM EDT = 1:00 PM CDT) -> $0.03754
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.03754, p.GenerationCreditDollarsPerKWH, 1e-6)

		// May Weekday Super Peak (Friday May 15, 2026 at 3:00 PM EDT = 2:00 PM CDT) -> $0.02978
		p, err = u.priceForTime(time.Date(2026, time.May, 15, 15, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.InDelta(t, 0.02978, p.GenerationCreditDollarsPerKWH, 1e-6)

		// August 2026 (Monday Aug 17, 2026 & Saturday Aug 15, 2026 in CT):
		// Super Peak 14-19 CT ($0.04096), On-Peak 09-14/19-21 CT ($0.03790),
		// Off-Peak 04-09/21-23 CT ($0.03159), Super Off-Peak 00-04/23-24 CT ($0.02795), Weekend Off-Peak 08-22 CT
		p, err = u.priceForTime(time.Date(2026, time.August, 17, 15, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.04096, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.August, 17, 10, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03790, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.August, 17, 5, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03159, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.August, 17, 3, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02795, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.August, 15, 8, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03159, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.August, 15, 7, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02795, p.GenerationCreditDollarsPerKWH, 1e-6)

		// September 2026 (Tuesday Sep 15, 2026 & Labor Day Monday Sep 7, 2026 in CT):
		// Super Peak 13-18 CT ($0.03211), On-Peak 11-13/18-21 CT ($0.02996),
		// Off-Peak 08-11/21-23 CT ($0.02647), Super Off-Peak 00-08/23-24 CT ($0.02290), Weekend/Holiday Off-Peak 10-22 CT
		p, err = u.priceForTime(time.Date(2026, time.September, 15, 14, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03211, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 15, 12, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02996, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 15, 8, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02647, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 15, 7, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02290, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.September, 7, 14, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02647, p.GenerationCreditDollarsPerKWH, 1e-6)

		// October 2026 (Wednesday Oct 14, 2026 & Saturday Oct 17, 2026 in CT):
		// Super Peak 11-21 CT ($0.03221), On-Peak 07-11/21-22 CT ($0.03133),
		// Off-Peak 05-07/22-24 CT ($0.03096), Super Off-Peak 00-05 CT ($0.02913), Weekend Off-Peak 08-22 CT
		p, err = u.priceForTime(time.Date(2026, time.October, 14, 11, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03221, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 14, 21, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03133, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 14, 23, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03096, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 14, 4, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.02913, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 17, 9, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03096, p.GenerationCreditDollarsPerKWH, 1e-6)

		// November-December 2026 (Monday Nov 16, 2026, Saturday Nov 14, 2026, & Thanksgiving Nov 26, 2026):
		// Super Peak 06-09/17-21 CT ($0.03571), On-Peak 09-12/16-17/21-22 CT ($0.03400),
		// Off-Peak 05-06/12-16/22-23 CT ($0.03364), Super Off-Peak 00-05/23-24 CT ($0.03202), Weekend/Holiday Off-Peak 05-12/16-22 CT
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 6, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03571, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 18, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03571, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 16, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03400, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 13, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03364, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 23, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03202, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Weekend & Thanksgiving midday 12-16 CT is Super Off-Peak ($0.03202), while 05-12 & 16-22 CT are Off-Peak ($0.03364)
		p, err = u.priceForTime(time.Date(2026, time.November, 14, 13, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03202, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 26, 7, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03364, p.GenerationCreditDollarsPerKWH, 1e-6)

		// 2027-2028 seasonal fallback:
		// Winter (Monday Feb 15, 2027 at 07:00 CT -> Super Peak $0.03571; 13:00 CT -> Off-Peak $0.03364)
		p, err = u.priceForTime(time.Date(2027, time.February, 15, 7, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03571, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.February, 15, 13, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03364, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Summer (Wednesday July 14, 2027 at 13:00 CT -> Super Peak $0.03221; 08:00 CT -> On-Peak $0.03133)
		p, err = u.priceForTime(time.Date(2027, time.July, 14, 13, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03221, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 14, 8, 0, 0, 0, ct))
		require.NoError(t, err)
		assert.InDelta(t, 0.03133, p.GenerationCreditDollarsPerKWH, 1e-6)
	})

	t.Run("Central to Eastern Alignment DST and Horizon Boundaries", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_time_shift",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dpp_b",
			},
		})
		require.NoError(t, err)

		// Weekday morning boundary (Monday Nov 16, 2026):
		// Schedule TRS Non-Summer On-Peak is 06:00-10:00 ET ($0.17744 + $0.02754 = $0.20498).
		// DPP Part B Super Peak is 06:00-09:00 CT = 07:00-10:00 ET ($0.03571).
		// At 06:00 ET (= 05:00 CT): import is On-Peak ($0.20498), DPP Part B is Off-Peak ($0.03364).
		p06ET, err := u.priceForTime(time.Date(2026, time.November, 16, 6, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p06ET.PeriodName)
		assert.InDelta(t, 0.20498, p06ET.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.03364, p06ET.GenerationCreditDollarsPerKWH, 1e-6)

		// At 07:00 ET (= 06:00 CT): import is On-Peak ($0.20498), DPP Part B shifts to Super Peak ($0.03571).
		p07ET, err := u.priceForTime(time.Date(2026, time.November, 16, 7, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p07ET.PeriodName)
		assert.InDelta(t, 0.20498, p07ET.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.03571, p07ET.GenerationCreditDollarsPerKWH, 1e-6)

		// At 10:00 ET (= 09:00 CT): import shifts to Off-Peak ($0.08075 + $0.02754 = $0.10829), DPP Part B shifts to On-Peak ($0.03400).
		p10ET, err := u.priceForTime(time.Date(2026, time.November, 16, 10, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p10ET.PeriodName)
		assert.InDelta(t, 0.10829, p10ET.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.03400, p10ET.GenerationCreditDollarsPerKWH, 1e-6)

		// November 1, 2026 DST transition (Sunday):
		// 04:00 UTC = 00:00 EDT (Nov 1 ET -> Nov FCA $0.02754) and 23:00 CDT (Oct 31 CT -> Oct Weekend Super Off-Peak $0.02913)
		p04UTC, err := u.priceForTime(time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.InDelta(t, 0.02754, p04UTC.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.02913, p04UTC.GenerationCreditDollarsPerKWH, 1e-6)

		// 05:00 UTC = 01:00 EDT & 00:00 CDT (Nov 1 CT -> Nov Weekend Super Off-Peak $0.03202)
		p05UTC, err := u.priceForTime(time.Date(2026, time.November, 1, 5, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.InDelta(t, 0.03202, p05UTC.GenerationCreditDollarsPerKWH, 1e-6)

		// 06:00 UTC = 01:00 EST (ET fell back) & 01:00 CDT -> Nov Weekend Super Off-Peak $0.03202
		p06UTC, err := u.priceForTime(time.Date(2026, time.November, 1, 6, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.InDelta(t, 0.03202, p06UTC.GenerationCreditDollarsPerKWH, 1e-6)

		// 07:00 UTC = 02:00 EST & 01:00 CST (CT fell back) -> Nov Weekend Super Off-Peak $0.03202
		p07UTC, err := u.priceForTime(time.Date(2026, time.November, 1, 7, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.InDelta(t, 0.03202, p07UTC.GenerationCreditDollarsPerKWH, 1e-6)

		// 11:00 UTC = 06:00 EST & 05:00 CST -> Nov Weekend Off-Peak $0.03364
		p11UTC, err := u.priceForTime(time.Date(2026, time.November, 1, 11, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.InDelta(t, 0.03364, p11UTC.GenerationCreditDollarsPerKWH, 1e-6)

		// Verify GetConfirmedPrices and GetFuturePrices across the November 1, 2026 DST transition day
		dstPrices, err := u.GetConfirmedPrices(
			context.Background(),
			time.Date(2026, time.November, 1, 0, 0, 0, 0, ny),
			time.Date(2026, time.November, 2, 0, 0, 0, 0, ny),
		)
		require.NoError(t, err)
		assert.Len(t, dstPrices, 25)

		u.nowFunc = func() time.Time {
			return time.Date(2026, time.November, 1, 3, 30, 0, 0, time.UTC)
		}
		futurePrices, err := u.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Len(t, futurePrices, 48)

		// Verify every hour across [2025-01-01 00:00 ET, 2029-01-01 00:00 ET) for both dpp_a and dpp_b
		horizonStartET := time.Date(2025, time.January, 1, 0, 0, 0, 0, ny)
		horizonEndET := time.Date(2029, time.January, 1, 0, 0, 0, 0, ny)

		for _, scheme := range []string{"dpp_a", "dpp_b"} {
			periods := epbPeriods("epb_time_shift", types.UtilityRateOptions{NetMeteringScheme: scheme}, []int{2025, 2026, 2027, 2028})
			for _, per := range periods {
				assert.False(t, per.Start.Before(horizonStartET), "scheme %s period started before ET horizon: %v", scheme, per.Start)
				assert.False(t, per.End.After(horizonEndET), "scheme %s period ended after ET horizon: %v", scheme, per.End)
			}

			for ts := horizonStartET; ts.Before(horizonEndET); ts = ts.Add(time.Hour) {
				var baseCount, fcaCount, solarCount int
				for i := range periods {
					ok, _, err := periods[i].Contains(ts)
					if err != nil {
						require.NoError(t, err)
					}
					if ok {
						if periods[i].SeparateGenerationCredit {
							solarCount++
						} else if periods[i].GridAdditional {
							fcaCount++
						} else {
							baseCount++
						}
					}
				}
				if baseCount != 1 || fcaCount != 1 || solarCount != 1 {
					require.Failf(t, "continuity gap or overlap", "scheme %s at %v (%v): base=%d fca=%d solar=%d", scheme, ts, ts.In(ct), baseCount, fcaCount, solarCount)
				}
			}
		}
	})

	t.Run("Standard Net Metering 1:1", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "epb",
			UtilityRate:     "epb_base",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, ny))
		require.NoError(t, err)
		assert.False(t, p.SeparateGenerationCredit)
	})
}
