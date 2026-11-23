package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSMUDUtility(t *testing.T) {
	ctx := context.Background()

	t.Run("Schedule R-TOD Default RT02 2026 and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
		})
		require.NoError(t, err)

		// 2026 Summer Weekday (Monday June 15, 2026)
		// Off-Peak 00:00-12:00 ($0.1550)
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 8, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1550, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0960, p.GenerationCreditDollarsPerKWH, 1e-6)

		// Mid-Peak 12:00-17:00 ($0.2139)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2139, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)

		// Peak 17:00-20:00 ($0.3765)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.3765, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		// Evening Mid-Peak 20:00-24:00 ($0.2139)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 20, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2139, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)

		// 2026 Summer Weekend (Sunday June 14, 2026 at 18:00 -> Off-Peak $0.1550)
		p, err = u.priceForTime(time.Date(2026, time.June, 14, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1550, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// 2026 Non-Summer Weekday (Wednesday March 18, 2026 & Tuesday November 17, 2026)
		p, err = u.priceForTime(time.Date(2026, time.March, 18, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1285, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.0740, p.GenerationCreditDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.March, 18, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1776, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.November, 17, 20, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1285, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.0960, p.GenerationCreditDollarsPerKWH, 1e-6)

		// 2027 Summer Weekday (Monday June 14, 2027)
		p, err = u.priceForTime(time.Date(2027, time.June, 14, 9, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1596, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2027, time.June, 14, 15, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2203, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2027, time.June, 14, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.3878, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)
		assert.InDelta(t, 0.0960, p.GenerationCreditDollarsPerKWH, 1e-6)

		// 2027 Non-Summer Weekday (Tuesday October 12, 2027)
		p, err = u.priceForTime(time.Date(2027, time.October, 12, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1829, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2027, time.October, 12, 11, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1324, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)
	})

	t.Run("Schedule R-TOD RTL1 2026 and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rtl1",
				NetMeteringScheme: "ssr",
			},
		})
		require.NoError(t, err)

		// 2026 Summer (Monday July 13, 2026)
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.4154, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.July, 13, 13, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2514, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.July, 13, 7, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1920, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// 2026 Non-Summer (Wednesday February 11, 2026)
		p, err = u.priceForTime(time.Date(2026, time.February, 11, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2148, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.February, 11, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1654, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// 2027 Summer (Wednesday July 14, 2027)
		p, err = u.priceForTime(time.Date(2027, time.July, 14, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.4314, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 14, 21, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2610, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 14, 8, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1993, p.DollarsPerKWH, 1e-6)

		// 2027 Non-Summer (Wednesday December 15, 2027)
		p, err = u.priceForTime(time.Date(2027, time.December, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2231, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.December, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1718, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule R-TOD EV Credit and GetPeriods", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rt02",
				EVCredit:          false,
				NetMeteringScheme: "ssr",
			},
		})
		require.NoError(t, err)

		periodsWithoutEV, err := u.GetPeriods(ctx)
		require.NoError(t, err)
		namesWithoutEV := make(map[string]bool)
		for _, p := range periodsWithoutEV {
			namesWithoutEV[p.Name] = true
		}
		assert.True(t, namesWithoutEV["Peak"])
		assert.True(t, namesWithoutEV["Mid-Peak"])
		assert.True(t, namesWithoutEV["Off-Peak"])
		assert.False(t, namesWithoutEV["Super Off-Peak"])

		// Enable EVCredit on RT02
		err = u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rt02",
				EVCredit:          true,
				NetMeteringScheme: "ssr",
			},
		})
		require.NoError(t, err)

		periodsWithEV, err := u.GetPeriods(ctx)
		require.NoError(t, err)
		namesWithEV := make(map[string]bool)
		for _, p := range periodsWithEV {
			namesWithEV[p.Name] = true
		}
		assert.True(t, namesWithEV["Peak"])
		assert.True(t, namesWithEV["Mid-Peak"])
		assert.True(t, namesWithEV["Off-Peak"])
		assert.True(t, namesWithEV["Super Off-Peak"])

		// 2026 Summer 00:00-06:00 daily: $0.1550 - $0.0150 = $0.1400
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 2, 0, 0, 0, ptLocation)) // Weekday
		require.NoError(t, err)
		assert.InDelta(t, 0.1400, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.June, 14, 5, 0, 0, 0, ptLocation)) // Weekend
		require.NoError(t, err)
		assert.InDelta(t, 0.1400, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.June, 19, 3, 0, 0, 0, ptLocation)) // Juneteenth Holiday
		require.NoError(t, err)
		assert.InDelta(t, 0.1400, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)

		// At 06:00 reverts to Off-Peak ($0.1550)
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 6, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1550, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// 2026 Non-Summer 00:00-06:00: $0.1285 - $0.0150 = $0.1135
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 4, 0, 0, 0, ptLocation)) // Thanksgiving Holiday
		require.NoError(t, err)
		assert.InDelta(t, 0.1135, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)

		// 2027 Summer & Non-Summer RTL1 with EVCredit
		err = u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rtl1",
				EVCredit:          true,
				NetMeteringScheme: "nem1",
			},
		})
		require.NoError(t, err)

		// 2027 Summer RTL1: $0.1993 - $0.0150 = $0.1843
		p, err = u.priceForTime(time.Date(2027, time.August, 10, 1, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1843, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.False(t, p.SeparateGenerationCredit)

		// 2027 Non-Summer RTL1: $0.1718 - $0.0150 = $0.1568
		p, err = u.priceForTime(time.Date(2027, time.January, 12, 4, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1568, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)

		// Verify strictly 1 base period per hour across a full summer and winter week with EVCredit=true
		fees, err := smudPeriods("smud_r_tod", types.UtilityRateOptions{
			RateClass:         "rtl1",
			EVCredit:          true,
			NetMeteringScheme: "ssr",
		}, []int{2026, 2027})
		require.NoError(t, err)
		for _, startDay := range []time.Time{
			time.Date(2026, time.June, 14, 0, 0, 0, 0, ptLocation),
			time.Date(2026, time.November, 22, 0, 0, 0, 0, ptLocation),
		} {
			for h := 0; h < 7*24; h++ {
				ts := startDay.Add(time.Duration(h) * time.Hour)
				baseMatches := 0
				solarMatches := 0
				for i := range fees {
					ok, _, cErr := fees[i].Contains(ts)
					require.NoError(t, cErr)
					if ok {
						if fees[i].SeparateGenerationCredit {
							solarMatches++
						} else {
							baseMatches++
						}
					}
				}
				assert.Equal(t, 1, baseMatches, "expected 1 base period at %v", ts)
				assert.Equal(t, 1, solarMatches, "expected 1 solar period at %v", ts)
			}
		}
	})

	t.Run("Schedule R Flat Seasonal 2026 and 2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r",
		})
		require.NoError(t, err)

		// 2026 Non-Summer ($0.1371) & Summer ($0.2189)
		p, err := u.priceForTime(time.Date(2026, time.May, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1371, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2189, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)

		p, err = u.priceForTime(time.Date(2026, time.October, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1371, p.DollarsPerKWH, 1e-6)

		// 2027 Non-Summer ($0.1412) & Summer ($0.2255)
		p, err = u.priceForTime(time.Date(2027, time.February, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1412, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.August, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2255, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("Schedule SSR June 1 2026 Step and NEM1 Exports", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rt02",
				NetMeteringScheme: "ssr",
			},
		})
		require.NoError(t, err)

		// 2026-05-31 23:00 PDT -> $0.0740/kWh SSR export credit
		beforeStep, err := u.priceForTime(time.Date(2026, time.May, 31, 23, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.True(t, beforeStep.SeparateGenerationCredit)
		assert.InDelta(t, 0.0740, beforeStep.GenerationCreditDollarsPerKWH, 1e-6)

		// 2026-06-01 00:00 PDT -> $0.0960/kWh SSR export credit (Resolution 26-04-04)
		atStep, err := u.priceForTime(time.Date(2026, time.June, 1, 0, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.True(t, atStep.SeparateGenerationCredit)
		assert.InDelta(t, 0.0960, atStep.GenerationCreditDollarsPerKWH, 1e-6)

		// Continuous hourly prices across the 2026-06-01T00:00:00 PT transition
		confirmed, err := u.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.May, 31, 22, 0, 0, 0, ptLocation),
			time.Date(2026, time.June, 1, 2, 0, 0, 0, ptLocation),
		)
		require.NoError(t, err)
		if assert.Len(t, confirmed, 4) {
			assert.InDelta(t, 0.0740, confirmed[0].GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0740, confirmed[1].GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0960, confirmed[2].GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0960, confirmed[3].GenerationCreditDollarsPerKWH, 1e-6)
			for i := 1; i < len(confirmed); i++ {
				assert.True(t, confirmed[i].TSStart.Equal(confirmed[i-1].TSEnd), "expected contiguous transition at index %d", i)
			}
		}

		// 2027-12-31 23:00 PST -> $0.0960/kWh SSR export credit
		end2027, err := u.priceForTime(time.Date(2027, time.December, 31, 23, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.True(t, end2027.SeparateGenerationCredit)
		assert.InDelta(t, 0.0960, end2027.GenerationCreditDollarsPerKWH, 1e-6)

		// NEM1 omits separate export periods and uses IsFlatNetMetering 1:1 credits
		nem1Settings := types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rt02",
				NetMeteringScheme: "nem1",
			},
			SolarNetMeteringCreditsValue: "highest",
		}
		assert.True(t, nem1Settings.UtilityRateOptions.IsFlatNetMetering())
		err = u.ApplySettings(ctx, nem1Settings)
		require.NoError(t, err)

		pNEM1, err := u.priceForTime(time.Date(2026, time.June, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.False(t, pNEM1.SeparateGenerationCredit)
		assert.InDelta(t, 0.0, pNEM1.GenerationCreditDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.3765, pNEM1.ExportRateDollars(nem1Settings, []types.Price{pNEM1}), 1e-6)
	})

	t.Run("Holidays and Non-Shifting Weekend Holidays", func(t *testing.T) {
		hols2026 := getSMUDHolidays(2026)
		assert.Equal(t, []string{
			"2026-01-01", // New Year's Day
			"2026-01-19", // MLK Day
			"2026-02-16", // Presidents Day
			"2026-05-25", // Memorial Day
			"2026-06-19", // Juneteenth
			"2026-07-04", // Independence Day (Saturday - not shifted)
			"2026-09-07", // Labor Day
			"2026-10-12", // Columbus Day
			"2026-11-11", // Veterans Day
			"2026-11-26", // Thanksgiving Day
			"2026-12-25", // Christmas Day
		}, hols2026)

		u := &genericTOU{}
		err := u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass:         "rt02",
				NetMeteringScheme: "ssr",
			},
		})
		require.NoError(t, err)

		// Weekday holidays are Off-Peak all day (e.g. Juneteenth Friday 2026-06-19 & Columbus Day Monday 2026-10-12)
		p, err := u.priceForTime(time.Date(2026, time.June, 19, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1550, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.October, 12, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1285, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2026, time.November, 11, 18, 0, 0, 0, ptLocation)) // Veterans Day Wednesday
		require.NoError(t, err)
		assert.InDelta(t, 0.1285, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		// Non-shifting weekend holidays:
		// 2026-07-04 is Saturday -> Friday 2026-07-03 at 18:00 remains Summer Peak ($0.3765)
		p, err = u.priceForTime(time.Date(2026, time.July, 3, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.3765, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		// 2027-07-04 is Sunday -> Monday 2027-07-05 at 18:00 remains Summer Peak ($0.3878) and 14:00 remains Mid-Peak ($0.2203)
		p, err = u.priceForTime(time.Date(2027, time.July, 5, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.3878, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 14, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2203, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Mid-Peak", p.PeriodName)

		// 2027-12-25 is Saturday -> Friday 2027-12-24 at 18:00 remains Non-Summer Peak ($0.1829)
		p, err = u.priceForTime(time.Date(2027, time.December, 24, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.1829, p.DollarsPerKWH, 1e-6)
		assert.Equal(t, "Peak", p.PeriodName)
	})

	t.Run("Invalid Options Rejected", func(t *testing.T) {
		m := Configured(nil)

		_, err := m.Site(ctx, "site-smud", types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "unknown_class",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown SMUD rate class")

		_, err = m.Site(ctx, "site-smud", types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r_tod",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown SMUD net metering scheme")

		_, err = m.Site(ctx, "site-smud", types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "ssr",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown SMUD net metering scheme")

		_, err = m.Site(ctx, "site-smud", types.Settings{
			UtilityProvider: "smud",
			UtilityRate:     "smud_r",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "rtl1",
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown SMUD rate class")
	})
}
