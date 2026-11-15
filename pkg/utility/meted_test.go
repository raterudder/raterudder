package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetedPeriods(t *testing.T) {
	t.Run("Rate RS Flat Rate", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_rs",
		})
		require.NoError(t, err)

		// Winter 2025-2026 (Feb 10, 2026): $0.073418 + $0.12968 = $0.203098/kWh
		p, err := u.priceForTime(time.Date(2026, time.February, 10, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.203098, p.DollarsPerKWH, 1e-6)
		assert.False(t, p.SeparateGenerationCredit)

		// Summer/Fall 2026 (Jul 15, 2026): $0.073418 + $0.13954 = $0.212958/kWh
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.212958, p.DollarsPerKWH, 1e-6)

		// Winter 2026-2027 (Dec 15, 2026): $0.073418 + $0.144249 = $0.217667/kWh
		p, err = u.priceForTime(time.Date(2026, time.December, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217667, p.DollarsPerKWH, 1e-6)

		// Winter 2026-2027 rollover (Feb 10, 2027): $0.217667/kWh
		p, err = u.priceForTime(time.Date(2027, time.February, 10, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.217667, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, periods)
	})

	t.Run("TOU Summer Fall 2026 Weekday", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Verify period names exist in GetPeriods
		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, period := range periods {
			names[period.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Off-Peak"])
		assert.True(t, names["Super Off-Peak"])

		// Monday, June 15, 2026
		mon := time.Date(2026, time.June, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak: 02:00 AM and 11:30 PM ($0.160142/kWh)
		p, err := u.priceForTime(mon.Add(2 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.160142, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(23*time.Hour + 30*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.160142, p.DollarsPerKWH, 1e-6)

		// Off-Peak: 10:00 AM, 1:59 PM, and 9:00 PM ($0.190590/kWh)
		p, err = u.priceForTime(mon.Add(10 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.190590, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(13*time.Hour + 59*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.190590, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(21 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.190590, p.DollarsPerKWH, 1e-6)

		// On-Peak: 2:00 PM - 9:00 PM Mon-Fri ($0.311473/kWh)
		p, err = u.priceForTime(mon.Add(14 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.311473, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(mon.Add(20*time.Hour + 59*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.311473, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU Summer Fall 2026 Weekend", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Saturday, June 20, 2026
		sat := time.Date(2026, time.June, 20, 0, 0, 0, 0, etLocation)

		// 4:00 PM on weekend is Off-Peak ($0.190590/kWh)
		p, err := u.priceForTime(sat.Add(16 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.190590, p.DollarsPerKWH, 1e-6)

		// 11:30 PM on weekend is Super Off-Peak ($0.160142/kWh)
		p, err = u.priceForTime(sat.Add(23*time.Hour + 30*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.160142, p.DollarsPerKWH, 1e-6)
	})

	t.Run("TOU Winter 2026-2027", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "meted",
			UtilityRate:     "meted_tou",
		})
		require.NoError(t, err)

		// Tuesday, Dec 15, 2026
		decTue := time.Date(2026, time.December, 15, 0, 0, 0, 0, etLocation)

		// Super Off-Peak: 05:30 AM ($0.163069/kWh)
		p, err := u.priceForTime(decTue.Add(5*time.Hour + 30*time.Minute))
		require.NoError(t, err)
		assert.InDelta(t, 0.163069, p.DollarsPerKWH, 1e-6)

		// Off-Peak: 10:00 PM ($0.194544/kWh)
		p, err = u.priceForTime(decTue.Add(22 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.194544, p.DollarsPerKWH, 1e-6)

		// On-Peak: 6:00 PM ($0.319507/kWh)
		p, err = u.priceForTime(decTue.Add(18 * time.Hour))
		require.NoError(t, err)
		assert.InDelta(t, 0.319507, p.DollarsPerKWH, 1e-6)

		// Verify Jan 2027 uses the same Dec 2026-May 2027 winter PTC
		janMon := time.Date(2027, time.January, 11, 18, 0, 0, 0, etLocation)
		p, err = u.priceForTime(janMon)
		require.NoError(t, err)
		assert.InDelta(t, 0.319507, p.DollarsPerKWH, 1e-6)
	})
}

func TestMetedUtilityInfo(t *testing.T) {
	info := metedUtilityInfo()
	assert.Equal(t, "meted", info.ID)
	assert.Equal(t, "Metropolitan Edison (Met-Ed)", info.Name)

	t.Run("Rates and Net Metering Option", func(t *testing.T) {
		if assert.Len(t, info.Rates, 2) {
			assert.Equal(t, "meted_rs", info.Rates[0].ID)
			assert.Equal(t, "Residential Service (Rate RS)", info.Rates[0].Name)
			if assert.Len(t, info.Rates[0].Options, 1) {
				assert.Equal(t, "netMeteringCredits", info.Rates[0].Options[0].Field)
				assert.Equal(t, true, info.Rates[0].Options[0].Default)
				assert.True(t, info.Rates[0].Options[0].Hidden)
			}

			assert.Equal(t, "meted_tou", info.Rates[1].ID)
			assert.Equal(t, "Residential Time-of-Use (TOU)", info.Rates[1].Name)
			if assert.Len(t, info.Rates[1].Options, 1) {
				assert.Equal(t, "netMeteringCredits", info.Rates[1].Options[0].Field)
				assert.Equal(t, true, info.Rates[1].Options[0].Default)
				assert.True(t, info.Rates[1].Options[0].Hidden)
			}
		}
	})
}
