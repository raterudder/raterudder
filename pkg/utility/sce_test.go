package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSouthernCaliforniaEdison(t *testing.T) {
	t.Run("sceUtilityInfo exposes applicationYear option dependent on SBP", func(t *testing.T) {
		info := sceUtilityInfo()
		assert.Equal(t, "sce", info.ID)
		require.Len(t, info.Rates, 3)

		for _, rate := range info.Rates {
			if assert.Len(t, rate.Options, 2, "rate %s should have netMeteringScheme and applicationYear options", rate.ID) {
				assert.Equal(t, "netMeteringScheme", rate.Options[0].Field)
				assert.Equal(t, "sbp", rate.Options[0].Default)

				appYearOpt := rate.Options[1]
				assert.Equal(t, "applicationYear", appYearOpt.Field)
				assert.Equal(t, types.UtilityOptionTypeSelect, appYearOpt.Type)
				assert.Equal(t, "2026", appYearOpt.Default)
				assert.Equal(t, map[string][]any{"netMeteringScheme": {"sbp"}}, appYearOpt.DependsOn)
				if assert.Len(t, appYearOpt.Choices, 6) {
					assert.Equal(t, "2023", appYearOpt.Choices[0].Value)
					assert.Equal(t, "2024", appYearOpt.Choices[1].Value)
					assert.Equal(t, "2025", appYearOpt.Choices[2].Value)
					assert.Equal(t, "2026", appYearOpt.Choices[3].Value)
					assert.Equal(t, "2027", appYearOpt.Choices[4].Value)
					assert.Equal(t, "non_locked", appYearOpt.Choices[5].Value)
				}
			}
		}
	})

	t.Run("TOU-D-PRIME across 2026 and 2027 windows with SBP default cohort (2026)", func(t *testing.T) {
		settings := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), settings))

		// 1. Jan-May 2026 (Advice 5725-E): NBC = $0.01135
		// Winter Mid-Peak (4-9 PM): $0.57199 retail -> $0.56064 base + $0.01135 NBC
		janMidPeak, err := u.priceForTime(time.Date(2026, time.March, 11, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", janMidPeak.PeriodName)
		assert.InDelta(t, 0.57199-0.01135, janMidPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01135, janMidPeak.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.57199, janMidPeak.ImportRateDollars(), 1e-6)

		// Winter Super Off-Peak (8 AM - 4 PM): $0.25362 retail; Winter Off-Peak: $0.25362 retail
		janSuperOffPeak, err := u.priceForTime(time.Date(2026, time.March, 11, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", janSuperOffPeak.PeriodName)
		assert.InDelta(t, 0.25362, janSuperOffPeak.ImportRateDollars(), 1e-6)

		janOffPeak, err := u.priceForTime(time.Date(2026, time.March, 11, 22, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", janOffPeak.PeriodName)
		assert.InDelta(t, 0.25362, janOffPeak.ImportRateDollars(), 1e-6)

		// 2. Jun-Sep 2026 (Advice 5829-E): NBC = $0.00779
		// Summer Weekday On-Peak (Wednesday July 15, 2026 5:00 PM): $0.59910 retail, NBT26 0.28366 + 0.01600 ACC Plus = 0.29966
		julOnPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", julOnPeak.PeriodName)
		assert.InDelta(t, 0.59910-0.00779, julOnPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.00779, julOnPeak.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.59910, julOnPeak.ImportRateDollars(), 1e-6)
		if assert.True(t, julOnPeak.SeparateGenerationCredit) {
			assert.InDelta(t, 0.28366+0.01600, julOnPeak.GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.29966, julOnPeak.ExportRateDollars(settings, nil), 1e-6)
		}

		// Summer Weekend Mid-Peak (Saturday July 18, 2026 5:00 PM): $0.40801 retail
		julWeekendMid, err := u.priceForTime(time.Date(2026, time.July, 18, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", julWeekendMid.PeriodName)
		assert.InDelta(t, 0.40801, julWeekendMid.ImportRateDollars(), 1e-6)

		// Summer Off-Peak (Wednesday July 15, 2026 11:00 AM): $0.27317 retail
		julOffPeak, err := u.priceForTime(time.Date(2026, time.July, 15, 11, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", julOffPeak.PeriodName)
		assert.InDelta(t, 0.27317, julOffPeak.ImportRateDollars(), 1e-6)

		// 3. Oct-Dec 2026 (Advice 5897-E): NBC = $0.01164
		// Winter Mid-Peak $0.46311, Off-Peak $0.31470, Super Off-Peak $0.26191
		octMidPeak, err := u.priceForTime(time.Date(2026, time.October, 15, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", octMidPeak.PeriodName)
		assert.InDelta(t, 0.46311-0.01164, octMidPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01164, octMidPeak.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.46311, octMidPeak.ImportRateDollars(), 1e-6)

		octOffPeak, err := u.priceForTime(time.Date(2026, time.October, 15, 22, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", octOffPeak.PeriodName)
		assert.InDelta(t, 0.31470, octOffPeak.ImportRateDollars(), 1e-6)

		octSuperOffPeak, err := u.priceForTime(time.Date(2026, time.October, 15, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", octSuperOffPeak.PeriodName)
		assert.InDelta(t, 0.26191, octSuperOffPeak.ImportRateDollars(), 1e-6)

		// 4. Summer 2027 (Advice 5897-E carried forward): NBC = $0.01164
		// On-Peak $0.62377, Mid-Peak $0.40853, Off-Peak $0.27798; 2027 NBT26 Jul_Weekday_17 0.28827 + 0.01600 = 0.30427
		jul2027OnPeak, err := u.priceForTime(time.Date(2027, time.July, 14, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", jul2027OnPeak.PeriodName)
		assert.InDelta(t, 0.62377-0.01164, jul2027OnPeak.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01164, jul2027OnPeak.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.62377, jul2027OnPeak.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.28827+0.01600, jul2027OnPeak.GenerationCreditDollarsPerKWH, 1e-6)

		jul2027MidPeak, err := u.priceForTime(time.Date(2027, time.July, 17, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", jul2027MidPeak.PeriodName)
		assert.InDelta(t, 0.40853, jul2027MidPeak.ImportRateDollars(), 1e-6)

		jul2027OffPeak, err := u.priceForTime(time.Date(2027, time.July, 14, 11, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", jul2027OffPeak.PeriodName)
		assert.InDelta(t, 0.27798, jul2027OffPeak.ImportRateDollars(), 1e-6)
	})

	t.Run("TOU-D-4-9PM Tier 1 across 2026 and 2027 windows with NEM 1.0 and NEM 2.0", func(t *testing.T) {
		nem2Settings := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_4_9pm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), nem2Settings))

		// 1. Jan-May 2026 (Advice 5725-E, NBC $0.01135): Mid-Peak $0.41670, Off-Peak $0.28061, Super Off-Peak $0.24147
		febMid, err := u.priceForTime(time.Date(2026, time.February, 10, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.41670-0.01135, febMid.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01135, febMid.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.41670, febMid.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.41670-0.01135, febMid.ExportRateDollars(nem2Settings, nil), 1e-6)
		assert.False(t, febMid.SeparateGenerationCredit)

		febOff, err := u.priceForTime(time.Date(2026, time.February, 10, 22, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.28061, febOff.ImportRateDollars(), 1e-6)

		febSuper, err := u.priceForTime(time.Date(2026, time.February, 10, 15, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", febSuper.PeriodName)
		assert.InDelta(t, 0.24147, febSuper.ImportRateDollars(), 1e-6)

		// 4:00 PM transitions to Mid-Peak on TOU-D-4-9PM
		feb4pm, err := u.priceForTime(time.Date(2026, time.February, 10, 16, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", feb4pm.PeriodName)

		// 2. Jun-Sep 2026 (Advice 5829-E, NBC $0.00779): On-Peak $0.48997, Mid-Peak $0.37002, Off-Peak $0.25186
		julOn, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.48997-0.00779, julOn.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.00779, julOn.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.48997, julOn.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.48997-0.00779, julOn.ExportRateDollars(nem2Settings, nil), 1e-6)

		julMid, err := u.priceForTime(time.Date(2026, time.July, 18, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.37002, julMid.ImportRateDollars(), 1e-6)

		julOff, err := u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.25186, julOff.ImportRateDollars(), 1e-6)

		// 3. Oct-Dec 2026 & Jan-May 2027 (Advice 5897-E, NBC $0.01164): Mid-Peak $0.42839, Off-Peak $0.28513, Super Off-Peak $0.24259
		novMid, err := u.priceForTime(time.Date(2026, time.November, 10, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.42839-0.01164, novMid.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.01164, novMid.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.42839, novMid.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.42839-0.01164, novMid.ExportRateDollars(nem2Settings, nil), 1e-6)

		novOff, err := u.priceForTime(time.Date(2026, time.November, 10, 22, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.28513, novOff.ImportRateDollars(), 1e-6)

		novSuper, err := u.priceForTime(time.Date(2026, time.November, 10, 12, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.24259, novSuper.ImportRateDollars(), 1e-6)

		// 4. Summer 2027 (Advice 5897-E, NBC $0.01164): On-Peak $0.51328, Mid-Peak $0.36965, Off-Peak $0.25274
		aug2027On, err := u.priceForTime(time.Date(2027, time.August, 11, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.51328, aug2027On.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.51328-0.01164, aug2027On.ExportRateDollars(nem2Settings, nil), 1e-6)

		aug2027Mid, err := u.priceForTime(time.Date(2027, time.August, 14, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.36965, aug2027Mid.ImportRateDollars(), 1e-6)

		aug2027Off, err := u.priceForTime(time.Date(2027, time.August, 11, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.25274, aug2027Off.ImportRateDollars(), 1e-6)

		// 5. NEM 1.0 ("net") preserves 1:1 full retail + FRC netting
		nem1Settings := types.Settings{
			UtilityProvider:              "sce",
			UtilityRate:                  "sce_tou_d_4_9pm",
			SolarNetMeteringCreditsValue: "highest",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "net",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), nem1Settings))
		nem1Peak, err := u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.False(t, nem1Peak.SeparateGenerationCredit)
		assert.InDelta(t, 0.48997, nem1Peak.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.48997, nem1Peak.ExportRateDollars(nem1Settings, []types.Price{nem1Peak}), 1e-6)
	})

	t.Run("TOU-D-5-8PM Tier 1 rates and 08:00-17:00 winter Super Off-Peak window", func(t *testing.T) {
		settings := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_5_8pm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "nem2",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), settings))

		// 1. Jan-May 2026: 08:00-17:00 Super Off-Peak ($0.23086), 17:00-20:00 Mid-Peak ($0.51371), Off-Peak ($0.28743)
		feb16h, err := u.priceForTime(time.Date(2026, time.February, 11, 16, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", feb16h.PeriodName)
		assert.InDelta(t, 0.23086, feb16h.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.01135, feb16h.GridUseDollarsPerKWH, 1e-6)

		feb17h, err := u.priceForTime(time.Date(2026, time.February, 11, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", feb17h.PeriodName)
		assert.InDelta(t, 0.51371, feb17h.ImportRateDollars(), 1e-6)

		feb20h, err := u.priceForTime(time.Date(2026, time.February, 11, 20, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", feb20h.PeriodName)
		assert.InDelta(t, 0.28743, feb20h.ImportRateDollars(), 1e-6)

		// 2. Jun-Sep 2026: On-Peak $0.64899, Mid-Peak $0.44882, Off-Peak $0.25049
		julOn, err := u.priceForTime(time.Date(2026, time.July, 15, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", julOn.PeriodName)
		assert.InDelta(t, 0.64899, julOn.ImportRateDollars(), 1e-6)

		julMid, err := u.priceForTime(time.Date(2026, time.July, 19, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", julMid.PeriodName)
		assert.InDelta(t, 0.44882, julMid.ImportRateDollars(), 1e-6)

		julOff, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", julOff.PeriodName)
		assert.InDelta(t, 0.25049, julOff.ImportRateDollars(), 1e-6)

		// 3. Oct-Dec 2026 & 2027 Winter: 08:00-17:00 Super Off-Peak ($0.24320), Mid-Peak ($0.47996), Off-Peak ($0.29702)
		dec16h, err := u.priceForTime(time.Date(2026, time.December, 9, 16, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", dec16h.PeriodName)
		assert.InDelta(t, 0.24320, dec16h.ImportRateDollars(), 1e-6)

		jan2027_16h, err := u.priceForTime(time.Date(2027, time.January, 13, 16, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", jan2027_16h.PeriodName)
		assert.InDelta(t, 0.24320, jan2027_16h.ImportRateDollars(), 1e-6)

		jan2027_17h, err := u.priceForTime(time.Date(2027, time.January, 13, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", jan2027_17h.PeriodName)
		assert.InDelta(t, 0.47996, jan2027_17h.ImportRateDollars(), 1e-6)

		jan2027_21h, err := u.priceForTime(time.Date(2027, time.January, 13, 21, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", jan2027_21h.PeriodName)
		assert.InDelta(t, 0.29702, jan2027_21h.ImportRateDollars(), 1e-6)

		// 4. Summer 2027: On-Peak $0.64614, Mid-Peak $0.43939, Off-Peak $0.26315
		jul2027On, err := u.priceForTime(time.Date(2027, time.July, 14, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", jul2027On.PeriodName)
		assert.InDelta(t, 0.64614, jul2027On.ImportRateDollars(), 1e-6)

		jul2027Mid, err := u.priceForTime(time.Date(2027, time.July, 18, 18, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", jul2027Mid.PeriodName)
		assert.InDelta(t, 0.43939, jul2027Mid.ImportRateDollars(), 1e-6)

		jul2027Off, err := u.priceForTime(time.Date(2027, time.July, 14, 16, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", jul2027Off.PeriodName)
		assert.InDelta(t, 0.26315, jul2027Off.ImportRateDollars(), 1e-6)
	})

	t.Run("Summer holidays charge Mid-Peak during peak hours and Off-Peak otherwise including Saturday holidays", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2026",
			},
		}))

		// Weekday holiday: Monday September 7, 2026 (Labor Day) -> Mid-Peak $0.40801 at 5 PM, Off-Peak $0.27317 at 10 AM
		laborDayPeak, err := u.priceForTime(time.Date(2026, time.September, 7, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", laborDayPeak.PeriodName)
		assert.InDelta(t, 0.40801, laborDayPeak.ImportRateDollars(), 1e-6)
		// Holiday SBP export uses Sep_Weekend_17 (0.20073) + 0.01600 = 0.21673
		assert.InDelta(t, sceNBTData[2026]["Sep_Weekend_17"]+0.01600, laborDayPeak.GenerationCreditDollarsPerKWH, 1e-6)

		laborDayOff, err := u.priceForTime(time.Date(2026, time.September, 7, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", laborDayOff.PeriodName)
		assert.InDelta(t, 0.27317, laborDayOff.ImportRateDollars(), 1e-6)

		// Saturday holiday: Saturday July 4, 2026 (Independence Day) -> Mid-Peak $0.40801 at 5 PM, Off-Peak $0.27317 at 10 AM
		july4Peak, err := u.priceForTime(time.Date(2026, time.July, 4, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", july4Peak.PeriodName)
		assert.InDelta(t, 0.40801, july4Peak.ImportRateDollars(), 1e-6)

		july4Off, err := u.priceForTime(time.Date(2026, time.July, 4, 10, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", july4Off.PeriodName)
		assert.InDelta(t, 0.27317, july4Off.ImportRateDollars(), 1e-6)

		// Observed Sunday->Monday holiday: Monday July 5, 2027 -> Mid-Peak $0.40853 at 5 PM, Off-Peak $0.27798 at 10 AM
		july5ObservedPeak, err := u.priceForTime(time.Date(2027, time.July, 5, 17, 0, 0, 0, ptLocation))
		require.NoError(t, err)
		assert.Equal(t, "Mid-Peak", july5ObservedPeak.PeriodName)
		assert.InDelta(t, 0.40853, july5ObservedPeak.ImportRateDollars(), 1e-6)
	})

	t.Run("SBP applicationYear cohorts apply verified 2026 and 2027 EEC tables and ACC Plus adders", func(t *testing.T) {
		jul2026Weekday17 := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		sep2026Weekday19 := time.Date(2026, time.September, 16, 19, 0, 0, 0, ptLocation)
		jul2027Weekday17 := time.Date(2027, time.July, 14, 17, 0, 0, 0, ptLocation)
		aug2027Weekend20 := time.Date(2027, time.August, 14, 20, 0, 0, 0, ptLocation)
		sep2027Weekday19 := time.Date(2027, time.September, 15, 19, 0, 0, 0, ptLocation)
		mar2027Weekend15 := time.Date(2027, time.March, 13, 15, 0, 0, 0, ptLocation)

		u := &genericTOU{}

		// 1. "2023": PTO Grp 1 + $0.04000 ACC Plus
		s2023 := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2023",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), s2023))
		p, err := u.priceForTime(jul2026Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.15887+0.04000, p.ExportRateDollars(s2023, nil), 1e-6)
		p, err = u.priceForTime(sep2026Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 2.42118+0.04000, p.ExportRateDollars(s2023, nil), 1e-6)
		p, err = u.priceForTime(jul2027Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.22284+0.04000, p.ExportRateDollars(s2023, nil), 1e-6)
		p, err = u.priceForTime(aug2027Weekend20)
		require.NoError(t, err)
		assert.InDelta(t, 0.69475+0.04000, p.ExportRateDollars(s2023, nil), 1e-6)
		p, err = u.priceForTime(sep2027Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 1.90837+0.04000, p.ExportRateDollars(s2023, nil), 1e-6)

		// 2. "2024": PTO Grp 2 (same base as 2023) + $0.03200 ACC Plus
		s2024 := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2024",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), s2024))
		p, err = u.priceForTime(jul2026Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.15887+0.03200, p.ExportRateDollars(s2024, nil), 1e-6)
		p, err = u.priceForTime(sep2027Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 1.90837+0.03200, p.ExportRateDollars(s2024, nil), 1e-6)

		// 3. "2025": PTO Grp 3 (NBT26 base) + $0.02400 ACC Plus
		s2025 := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2025",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), s2025))
		p, err = u.priceForTime(jul2026Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.28366+0.02400, p.ExportRateDollars(s2025, nil), 1e-6)
		p, err = u.priceForTime(sep2026Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 0.58417+0.02400, p.ExportRateDollars(s2025, nil), 1e-6)
		p, err = u.priceForTime(jul2027Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.28827+0.02400, p.ExportRateDollars(s2025, nil), 1e-6)
		p, err = u.priceForTime(aug2027Weekend20)
		require.NoError(t, err)
		assert.InDelta(t, 1.18181+0.02400, p.ExportRateDollars(s2025, nil), 1e-6)
		p, err = u.priceForTime(sep2027Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 0.69415+0.02400, p.ExportRateDollars(s2025, nil), 1e-6)

		// 4. "2027": PTO Grp 5 (NBT26 fallback in 2026, NBT27 in 2027) + $0.00800 ACC Plus
		s2027 := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2027",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), s2027))
		p, err = u.priceForTime(jul2026Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.28366+0.00800, p.ExportRateDollars(s2027, nil), 1e-6)
		p, err = u.priceForTime(sep2026Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 0.58417+0.00800, p.ExportRateDollars(s2027, nil), 1e-6)
		p, err = u.priceForTime(jul2027Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.27051+0.00800, p.ExportRateDollars(s2027, nil), 1e-6)
		p, err = u.priceForTime(aug2027Weekend20)
		require.NoError(t, err)
		assert.InDelta(t, 0.25555+0.00800, p.ExportRateDollars(s2027, nil), 1e-6)
		p, err = u.priceForTime(sep2027Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 0.54531+0.00800, p.ExportRateDollars(s2027, nil), 1e-6)
		// Zero-base NBT27 slot Mar_Weekend_15 preserves 0.00000 + 0.00800 = 0.00800
		p, err = u.priceForTime(mar2027Weekend15)
		require.NoError(t, err)
		assert.InDelta(t, 0.00800, p.ExportRateDollars(s2027, nil), 1e-6)

		// 5. "non_locked": NBT00 (NBT26 in 2026, NBT27 in 2027) + $0.00000 ACC Plus
		sNonLocked := types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_prime",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "non_locked",
			},
		}
		require.NoError(t, u.ApplySettings(context.Background(), sNonLocked))
		p, err = u.priceForTime(jul2026Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.28366, p.ExportRateDollars(sNonLocked, nil), 1e-6)
		p, err = u.priceForTime(sep2026Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 0.58417, p.ExportRateDollars(sNonLocked, nil), 1e-6)
		p, err = u.priceForTime(jul2027Weekday17)
		require.NoError(t, err)
		assert.InDelta(t, 0.27051, p.ExportRateDollars(sNonLocked, nil), 1e-6)
		p, err = u.priceForTime(aug2027Weekend20)
		require.NoError(t, err)
		assert.InDelta(t, 0.25555, p.ExportRateDollars(sNonLocked, nil), 1e-6)
		p, err = u.priceForTime(sep2027Weekday19)
		require.NoError(t, err)
		assert.InDelta(t, 0.54531, p.ExportRateDollars(sNonLocked, nil), 1e-6)
		// Zero-base NBT00 slot Mar_Weekend_15 preserves exact $0.00000 SeparateGenerationCredit
		p, err = u.priceForTime(mar2027Weekend15)
		require.NoError(t, err)
		if assert.True(t, p.SeparateGenerationCredit) {
			assert.InDelta(t, 0.0, p.ExportRateDollars(sNonLocked, nil), 1e-6)
		}
	})

	t.Run("Continuous hourly pricing across effective date and DST transitions", func(t *testing.T) {
		ctx := context.Background()
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, types.Settings{
			UtilityProvider: "sce",
			UtilityRate:     "sce_tou_d_5_8pm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "sbp",
				ApplicationYear:   "2026",
			},
		}))

		// May 31 -> June 1, 2026 transition (Advice 5725-E $0.01135 NBC -> Advice 5829-E $0.00779 NBC)
		mayJunPrices, err := u.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.May, 31, 22, 0, 0, 0, ptLocation),
			time.Date(2026, time.June, 1, 2, 0, 0, 0, ptLocation),
		)
		require.NoError(t, err)
		if assert.Len(t, mayJunPrices, 4) {
			assert.InDelta(t, 0.01135, mayJunPrices[1].GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.28743, mayJunPrices[1].ImportRateDollars(), 1e-6)
			assert.InDelta(t, 0.00779, mayJunPrices[2].GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.25049, mayJunPrices[2].ImportRateDollars(), 1e-6)
		}

		// Sep 30 -> Oct 1, 2026 transition (Advice 5829-E $0.00779 NBC -> Advice 5897-E $0.01164 NBC)
		sepOctPrices, err := u.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.September, 30, 22, 0, 0, 0, ptLocation),
			time.Date(2026, time.October, 1, 2, 0, 0, 0, ptLocation),
		)
		require.NoError(t, err)
		if assert.Len(t, sepOctPrices, 4) {
			assert.InDelta(t, 0.00779, sepOctPrices[1].GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.25049, sepOctPrices[1].ImportRateDollars(), 1e-6)
			assert.InDelta(t, 0.01164, sepOctPrices[2].GridUseDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.29702, sepOctPrices[2].ImportRateDollars(), 1e-6)
		}

		// DST Fall-Back Sunday November 1, 2026 (25-hour day in America/Los_Angeles)
		dstFallPrices, err := u.GetConfirmedPrices(
			ctx,
			time.Date(2026, time.November, 1, 0, 0, 0, 0, ptLocation),
			time.Date(2026, time.November, 2, 0, 0, 0, 0, ptLocation),
		)
		require.NoError(t, err)
		assert.Len(t, dstFallPrices, 25)
	})
}

func TestSCEHolidays(t *testing.T) {
	t.Run("Check designated holidays for 2026", func(t *testing.T) {
		holidays := getSCEHolidays(2026)
		assert.Contains(t, holidays, "2026-01-01")
		assert.Contains(t, holidays, "2026-02-16")
		assert.Contains(t, holidays, "2026-05-25")
		assert.Contains(t, holidays, "2026-07-04")
		assert.Contains(t, holidays, "2026-09-07")
		assert.Contains(t, holidays, "2026-11-11")
		assert.Contains(t, holidays, "2026-11-26")
		assert.Contains(t, holidays, "2026-12-25")
	})

	t.Run("Sunday holiday shifts to Monday while Saturday holiday stays on Saturday", func(t *testing.T) {
		holidays := getSCEHolidays(2027)
		if assert.Contains(t, holidays, "2027-07-05") {
			assert.NotContains(t, holidays, "2027-07-04")
		}
		// Christmas 2027 falls on Saturday December 25 and does not shift to Friday
		assert.Contains(t, holidays, "2027-12-25")
	})
}

func TestGetSCENBTExportRate(t *testing.T) {
	t.Run("Default 2026 cohort includes $0.01600 ACC Plus adder on weekday, weekend, and holiday", func(t *testing.T) {
		weekday := time.Date(2026, time.July, 15, 17, 0, 0, 0, ptLocation)
		assert.InDelta(t, 0.28366+0.01600, getSCENBTExportRate(weekday, ""), 1e-6)
		assert.InDelta(t, 0.28366+0.01600, getSCENBTExportRate(weekday, "2026"), 1e-6)

		weekend := time.Date(2026, time.July, 18, 17, 0, 0, 0, ptLocation)
		assert.InDelta(t, 0.13600+0.01600, getSCENBTExportRate(weekend, "2026"), 1e-6)

		holiday := time.Date(2026, time.January, 1, 17, 0, 0, 0, ptLocation)
		assert.InDelta(t, 0.10548+0.01600, getSCENBTExportRate(holiday, "2026"), 1e-6)
	})

	t.Run("Cohort-specific 2026 and 2027 tables and non_locked zero-adder rates", func(t *testing.T) {
		sep2026Weekday19 := time.Date(2026, time.September, 16, 19, 0, 0, 0, ptLocation)
		sep2027Weekday19 := time.Date(2027, time.September, 15, 19, 0, 0, 0, ptLocation)

		assert.InDelta(t, 2.42118+0.04000, getSCENBTExportRate(sep2026Weekday19, "2023"), 1e-6)
		assert.InDelta(t, 2.42118+0.03200, getSCENBTExportRate(sep2026Weekday19, "2024"), 1e-6)
		assert.InDelta(t, 0.58417+0.02400, getSCENBTExportRate(sep2026Weekday19, "2025"), 1e-6)
		assert.InDelta(t, 0.58417+0.00800, getSCENBTExportRate(sep2026Weekday19, "2027"), 1e-6)
		assert.InDelta(t, 0.58417, getSCENBTExportRate(sep2026Weekday19, "non_locked"), 1e-6)

		assert.InDelta(t, 1.90837+0.04000, getSCENBTExportRate(sep2027Weekday19, "2023"), 1e-6)
		assert.InDelta(t, 1.90837+0.03200, getSCENBTExportRate(sep2027Weekday19, "2024"), 1e-6)
		assert.InDelta(t, 0.69415+0.02400, getSCENBTExportRate(sep2027Weekday19, "2025"), 1e-6)
		assert.InDelta(t, 0.69415+0.01600, getSCENBTExportRate(sep2027Weekday19, "2026"), 1e-6)
		assert.InDelta(t, 0.54531+0.00800, getSCENBTExportRate(sep2027Weekday19, "2027"), 1e-6)
		assert.InDelta(t, 0.54531, getSCENBTExportRate(sep2027Weekday19, "non_locked"), 1e-6)
	})
}
