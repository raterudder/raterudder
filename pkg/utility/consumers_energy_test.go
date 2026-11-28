package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumersEnergyPeriods(t *testing.T) {
	ctx := context.Background()

	t.Run("Rate RSP Default Summer Peak Seasonal Prices May 1 Transition and Export Schemes", func(t *testing.T) {
		settingsDG := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsp",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsDG))

		// Pre-2026-05-01 Case U-21585 Oct-May Off-Peak (Wednesday Feb 18, 2026 at 15:00 ET -> Off-Peak all day)
		p, err := u.priceForTime(time.Date(2026, time.February, 18, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.False(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.085252+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.078955+0.014397, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.085252+0.01456+0.078955+0.014397, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.085252+0.01456, p.ExportRateDollars(settingsDG, nil), 1e-6)
		assert.InDelta(t, 0.085252+0.01456, p.BatteryExportRateDollars(settingsDG, nil), 1e-6)

		// Exact 2026-05-01T00:00:00 ET transition boundary (Thu Apr 30 23:00 ET -> Fri May 1 00:00 ET)
		pBeforeMay, err := u.priceForTime(time.Date(2026, time.April, 30, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pBeforeMay.PeriodName)
		assert.InDelta(t, 0.085252+0.01456, pBeforeMay.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.078955+0.014397, pBeforeMay.GridUseDollarsPerKWH, 1e-6)

		pAfterMay, err := u.priceForTime(time.Date(2026, time.May, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pAfterMay.PeriodName)
		assert.InDelta(t, 0.082996+0.01456, pAfterMay.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, pAfterMay.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.082996+0.01456+0.093333+0.014397, pAfterMay.ImportRateDollars(), 1e-6)

		// Memorial Day Monday May 25, 2026 at 15:00 ET -> still Oct-May season so Off-Peak ($0.082996 supply)
		p, err = u.priceForTime(time.Date(2026, time.May, 25, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.082996+0.01456, p.DollarsPerKWH, 1e-6)

		// Summer Weekday (Monday June 15, 2026): 13:00 Off-Peak, 14:00 On-Peak, 18:00 On-Peak, 19:00 Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 13, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.103970+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.151904+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.151904+0.01456+0.093333+0.014397, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.151904+0.01456, p.ExportRateDollars(settingsDG, nil), 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.June, 15, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.103970+0.01456, p.DollarsPerKWH, 1e-6)

		// Summer Weekend (Sunday June 14, 2026 at 15:00 ET) -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.June, 14, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.103970+0.01456, p.DollarsPerKWH, 1e-6)

		// Weekday holidays remain On-Peak on residential schedules (Labor Day Monday Sep 7, 2026 & Monday Jul 5, 2027)
		p, err = u.priceForTime(time.Date(2026, time.September, 7, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.151904+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 5, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.151904+0.01456, p.DollarsPerKWH, 1e-6)

		// Oct-May 2027 Off-Peak (Wednesday January 20, 2027 at 16:00 ET)
		p, err = u.priceForTime(time.Date(2027, time.January, 20, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.082996+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, p.GridUseDollarsPerKWH, 1e-6)

		// Rule C11.2 legacy_nem moves Distribution into base DollarsPerKWH / ExportRateDollars across May 1 boundary
		settingsLegacyNEM := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsp",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "legacy_nem",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsLegacyNEM))

		pPreLegacy, err := u.priceForTime(time.Date(2026, time.April, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", pPreLegacy.PeriodName)
		assert.False(t, pPreLegacy.SeparateGenerationCredit)
		assert.InDelta(t, 0.085252+0.01456+0.078955, pPreLegacy.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.014397, pPreLegacy.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.085252+0.01456+0.078955+0.014397, pPreLegacy.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.085252+0.01456+0.078955, pPreLegacy.ExportRateDollars(settingsLegacyNEM, nil), 1e-6)

		pPostLegacy, err := u.priceForTime(time.Date(2027, time.August, 11, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pPostLegacy.PeriodName)
		assert.InDelta(t, 0.151904+0.01456+0.093333, pPostLegacy.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.014397, pPostLegacy.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.151904+0.01456+0.093333+0.014397, pPostLegacy.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.151904+0.01456+0.093333, pPostLegacy.ExportRateDollars(settingsLegacyNEM, nil), 1e-6)

		// Rule C11.4 simple_renewable sets SeparateGenerationCredit = true at $0.077/kWh while preserving ImportRateDollars
		settingsSimpleRenewable := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsp",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "simple_renewable",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsSimpleRenewable))

		pSimple, err := u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", pSimple.PeriodName)
		assert.True(t, pSimple.SeparateGenerationCredit)
		assert.InDelta(t, 0.151904+0.01456, pSimple.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, pSimple.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.151904+0.01456+0.093333+0.014397, pSimple.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.077, pSimple.ExportRateDollars(settingsSimpleRenewable, nil), 1e-6)
		assert.InDelta(t, 0.077, pSimple.BatteryExportRateDollars(settingsSimpleRenewable, nil), 1e-6)
	})

	t.Run("Rate RSH Smart Hours Year-Round TOU Holidays and Export Schemes", func(t *testing.T) {
		settingsDG := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsh",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "dg",
			},
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsDG))

		// Pre-2026-05-01 Winter Weekday On-Peak (14:00-19:00 -> $0.095326) and Off-Peak ($0.082971)
		p, err := u.priceForTime(time.Date(2026, time.March, 18, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.095326+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.078955+0.014397, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.095326+0.01456, p.ExportRateDollars(settingsDG, nil), 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.March, 18, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.082971+0.01456, p.DollarsPerKWH, 1e-6)

		// Post-2026-05-01 May Weekday On-Peak ($0.096396) and Off-Peak ($0.080437), including Memorial Day Mon May 25, 2026
		p, err = u.priceForTime(time.Date(2026, time.May, 25, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.096396+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, p.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.May, 25, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.080437+0.01456, p.DollarsPerKWH, 1e-6)

		// Summer 2026 Weekday On-Peak ($0.151904) and Off-Peak ($0.103970)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.151904+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 15, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.103970+0.01456, p.DollarsPerKWH, 1e-6)

		// Thanksgiving Thursday Nov 26, 2026 at 16:00 ET remains On-Peak ($0.096396)
		p, err = u.priceForTime(time.Date(2026, time.November, 26, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.096396+0.01456, p.DollarsPerKWH, 1e-6)

		// RSH legacy_nem and simple_renewable export credits
		settingsLegacyNEM := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsh",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "legacy_nem",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsLegacyNEM))
		p, err = u.priceForTime(time.Date(2027, time.December, 24, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.096396+0.01456+0.093333, p.ExportRateDollars(settingsLegacyNEM, nil), 1e-6)

		settingsSimple := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsh",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "simple_renewable",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsSimple))
		p, err = u.priceForTime(time.Date(2027, time.December, 24, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.077, p.ExportRateDollars(settingsSimple, nil), 1e-6)
	})

	t.Run("Rate RPM Nighttime Savers Three-Period TOU Export Schemes and DST Transitions", func(t *testing.T) {
		settingsDG := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rpm",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsDG))

		// Pre-2026-05-01 Winter Weekday (Wednesday Jan 14, 2026)
		// 02:00 -> Super Off-Peak ($0.071387), 08:00 -> Off-Peak ($0.092809), 15:00 -> On-Peak ($0.095326),
		// 20:00 -> Off-Peak ($0.092809), 23:00 -> Super Off-Peak ($0.071387)
		p, err := u.priceForTime(time.Date(2026, time.January, 14, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.071387+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.078955+0.014397, p.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.January, 14, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.092809+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.January, 14, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.095326+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.January, 14, 20, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.092809+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.January, 14, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.071387+0.01456, p.DollarsPerKWH, 1e-6)

		// Summer Weekday (Monday July 13, 2026)
		// 05:00 -> Super Off-Peak ($0.093996), 06:00 -> Off-Peak ($0.116161), 14:00 -> On-Peak ($0.151904),
		// 19:00 -> Off-Peak ($0.116161), 23:00 -> Super Off-Peak ($0.093996)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 5, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.093996+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, p.GridUseDollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 13, 6, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.116161+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 13, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.151904+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 13, 19, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.116161+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.July, 13, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.093996+0.01456, p.DollarsPerKWH, 1e-6)

		// Summer Weekend (Saturday July 11, 2026 at 15:00 ET) -> Super Off-Peak all day
		p, err = u.priceForTime(time.Date(2026, time.July, 11, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.093996+0.01456, p.DollarsPerKWH, 1e-6)

		// Post-2026-05-01 Winter Weekday & Holiday (Christmas Friday Dec 25, 2026)
		p, err = u.priceForTime(time.Date(2026, time.December, 25, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.096396+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 25, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.090423+0.01456, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.December, 25, 1, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Super Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.072645+0.01456, p.DollarsPerKWH, 1e-6)

		// RPM legacy_nem and simple_renewable export credits
		settingsLegacyNEM := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rpm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "legacy_nem",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsLegacyNEM))
		p, err = u.priceForTime(time.Date(2027, time.June, 16, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.151904+0.01456+0.093333, p.ExportRateDollars(settingsLegacyNEM, nil), 1e-6)

		settingsSimple := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rpm",
			UtilityRateOptions: types.UtilityRateOptions{
				NetMeteringScheme: "simple_renewable",
			},
		}
		require.NoError(t, u.ApplySettings(ctx, settingsSimple))
		p, err = u.priceForTime(time.Date(2027, time.June, 16, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.077, p.ExportRateDollars(settingsSimple, nil), 1e-6)

		// Verify GetConfirmedPrices across 2026-05-01 transition, spring-forward DST (2026-03-08 & 2027-03-14),
		// and fall-back DST (2026-11-01 & 2027-11-07) has contiguous non-overlapping hourly prices.
		require.NoError(t, u.ApplySettings(ctx, settingsDG))
		for _, windowStart := range []time.Time{
			time.Date(2026, time.April, 30, 0, 0, 0, 0, etLocation),
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

	t.Run("Rate RSM Non-Transmitting Meter Tier 1 Flat and Zero Export Credit", func(t *testing.T) {
		settingsRSM := types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rsm",
		}
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(ctx, settingsRSM))

		// Pre-2026-05-01 ($0.085252 supply + $0.01456 PSCR + $0.078955 dist + $0.014397 surcharges, $0.00 export)
		p, err := u.priceForTime(time.Date(2026, time.February, 10, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Empty(t, p.PeriodName)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.085252+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.078955+0.014397, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.085252+0.01456+0.078955+0.014397, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.0, p.ExportRateDollars(settingsRSM, nil), 1e-6)
		assert.InDelta(t, 0.0, p.BatteryExportRateDollars(settingsRSM, nil), 1e-6)

		// Post-2026-05-01 & 2027 ($0.082996 supply + $0.01456 PSCR + $0.093333 dist + $0.014397 surcharges, $0.00 export)
		p, err = u.priceForTime(time.Date(2027, time.July, 20, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Empty(t, p.PeriodName)
		assert.True(t, p.SeparateGenerationCredit)
		assert.InDelta(t, 0.082996+0.01456, p.DollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.093333+0.014397, p.GridUseDollarsPerKWH, 1e-6)
		assert.InDelta(t, 0.082996+0.01456+0.093333+0.014397, p.ImportRateDollars(), 1e-6)
		assert.InDelta(t, 0.0, p.ExportRateDollars(settingsRSM, nil), 1e-6)
	})

	t.Run("Hourly Continuity NamedPeriodsCoverDay Map Integration and Validation", func(t *testing.T) {
		start2026 := time.Date(2026, time.January, 1, 0, 0, 0, 0, etLocation)
		end2028 := time.Date(2028, time.January, 1, 0, 0, 0, 0, etLocation)

		type planScheme struct {
			plan        string
			scheme      string
			expectSolar bool
		}
		combos := []planScheme{
			{plan: "consumers_energy_rsp", scheme: "dg", expectSolar: false},
			{plan: "consumers_energy_rsp", scheme: "legacy_nem", expectSolar: false},
			{plan: "consumers_energy_rsp", scheme: "simple_renewable", expectSolar: true},
			{plan: "consumers_energy_rsh", scheme: "dg", expectSolar: false},
			{plan: "consumers_energy_rsh", scheme: "legacy_nem", expectSolar: false},
			{plan: "consumers_energy_rsh", scheme: "simple_renewable", expectSolar: true},
			{plan: "consumers_energy_rpm", scheme: "dg", expectSolar: false},
			{plan: "consumers_energy_rpm", scheme: "legacy_nem", expectSolar: false},
			{plan: "consumers_energy_rpm", scheme: "simple_renewable", expectSolar: true},
			{plan: "consumers_energy_rsm", scheme: "", expectSolar: true},
		}

		for _, c := range combos {
			fees, err := consumersEnergyPeriods(c.plan, types.UtilityRateOptions{NetMeteringScheme: c.scheme}, []int{2026, 2027})
			require.NoError(t, err)
			require.NotEmpty(t, fees)

			var namedImportPeriods []types.TimePeriod
			for _, p := range fees {
				if !p.SeparateGenerationCredit && !p.SeparateBatteryGenerationCredit && !p.GridAdditional && p.Name != "" {
					namedImportPeriods = append(namedImportPeriods, p.TimePeriod)
				}
			}
			if c.plan == "consumers_energy_rsm" {
				assert.Empty(t, namedImportPeriods)
			} else {
				require.NotEmpty(t, namedImportPeriods)
				for day := start2026; day.Before(end2028); day = day.AddDate(0, 0, 1) {
					if !namedPeriodsCoverDay(namedImportPeriods, day) {
						require.Failf(t, "missing 24-hour named coverage", "plan %s (%s) failed namedPeriodsCoverDay on %s", c.plan, c.scheme, day.Format("2006-01-02"))
					}
				}
			}

			for ts := start2026; ts.Before(end2028); ts = ts.Add(time.Hour) {
				baseCount := 0
				additionalCount := 0
				solarCount := 0
				for i := range fees {
					ok, _, cErr := fees[i].Contains(ts)
					if cErr != nil {
						require.NoError(t, cErr)
					}
					if ok {
						if fees[i].SeparateGenerationCredit {
							solarCount++
						} else if fees[i].GridAdditional {
							additionalCount++
						} else {
							baseCount++
						}
					}
				}
				expectedSolar := 0
				if c.expectSolar {
					expectedSolar = 1
				}
				if baseCount != 1 || additionalCount != 1 || solarCount != expectedSolar {
					require.Equal(t, 1, baseCount, "plan %s (%s) at %v must match 1 base period", c.plan, c.scheme, ts)
					require.Equal(t, 1, additionalCount, "plan %s (%s) at %v must match 1 GridAdditional period", c.plan, c.scheme, ts)
					require.Equal(t, expectedSolar, solarCount, "plan %s (%s) at %v must match %d SeparateGenerationCredit period", c.plan, c.scheme, ts, expectedSolar)
				}
			}
		}

		// Verify Configured(nil).ListUtilities() and Map.Site integration
		m := Configured(nil)
		var foundProvider *types.UtilityProviderInfo
		for _, u := range m.ListUtilities() {
			if u.ID == "consumers_energy" {
				uCopy := u
				foundProvider = &uCopy
				break
			}
		}
		if assert.NotNil(t, foundProvider, "expected consumers_energy in Configured(nil).ListUtilities()") {
			assert.False(t, foundProvider.Hidden)
			assert.Equal(t, "Consumers Energy", foundProvider.Name)
			if assert.Len(t, foundProvider.Rates, 4) {
				assert.Empty(t, foundProvider.Rates[3].Options, "Rate RSM must not expose netMeteringScheme options")
			}
		}

		provider, err := m.Site(ctx, "site-ce", types.Settings{
			UtilityProvider: "consumers_energy",
			UtilityRate:     "consumers_energy_rpm",
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
		_, err = consumersEnergyPeriods("consumers_energy_unknown", types.UtilityRateOptions{}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown Consumers Energy rate plan")

		_, err = consumersEnergyPeriods("consumers_energy_rsp", types.UtilityRateOptions{NetMeteringScheme: "invalid"}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown Consumers Energy net metering scheme")

		_, err = consumersEnergyPeriods("consumers_energy_rsm", types.UtilityRateOptions{NetMeteringScheme: "dg"}, []int{2026, 2027})
		require.Error(t, err)
		assert.ErrorContains(t, err, "not supported on non-transmitting Consumers Energy Rate RSM")
	})
}
