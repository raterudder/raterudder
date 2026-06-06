package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPSEGLIUtility(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	t.Run("Rate 194 TOU Prices", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "psegli",
			UtilityRate:     "psegli_194",
		})
		require.NoError(t, err)

		// Summer: Mon, Jun 15, 2026
		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, ny)

		// Peak (3 PM - 7 PM weekdays): 4:00 PM -> $0.2217
		p, err := u.priceForTime(summerMon.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2217, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.1093
		p, err = u.priceForTime(summerMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1093, p.DollarsPerKWH, 1e-6)
		}

		// Winter: Tue, Jan 20, 2026
		winterMon := time.Date(2026, time.January, 20, 0, 0, 0, 0, ny)

		// Peak (3 PM - 7 PM weekdays): 4:00 PM -> $0.1885
		p, err = u.priceForTime(winterMon.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1885, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.0929
		p, err = u.priceForTime(winterMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0929, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Rate 195 TOU Prices", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "psegli",
			UtilityRate:     "psegli_195",
		})
		require.NoError(t, err)

		summerMon := time.Date(2026, time.June, 15, 0, 0, 0, 0, ny)

		// Super Off-Peak (10 PM - 6 AM daily): 12:00 AM -> $0.0452
		p, err := u.priceForTime(summerMon)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0452, p.DollarsPerKWH, 1e-6)
		}

		// On-Peak (3 PM - 7 PM weekdays): 4:00 PM -> $0.2979
		p, err = u.priceForTime(summerMon.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2979, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak (all other times): 10:00 AM -> $0.1388
		p, err = u.priceForTime(summerMon.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1388, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Rate 190 (Short Peak)", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "psegli",
			UtilityRate:     "psegli_190",
		})
		require.NoError(t, err)

		// Summer Peak: Mon, Jun 15, 2026 at 5:00 PM -> $0.2697
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 17, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2697, p.DollarsPerKWH, 1e-6)
		}

		// Shoulder Peak: Mon, Apr 15, 2026 at 5:00 PM -> $0.1698
		p, err = u.priceForTime(time.Date(2026, time.April, 15, 17, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1698, p.DollarsPerKWH, 1e-6)
		}

		// Winter Peak: Mon, Jan 15, 2026 at 5:00 PM -> $0.2222
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 17, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2222, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Rate 193 (Overnight)", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "psegli",
			UtilityRate:     "psegli_193",
		})
		require.NoError(t, err)

		// Summer Night (12 AM): $0.0694
		p, err := u.priceForTime(time.Date(2026, time.June, 15, 0, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0694, p.DollarsPerKWH, 1e-6)
		}

		// Summer Day (12 PM): $0.1438
		p, err = u.priceForTime(time.Date(2026, time.June, 15, 12, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1438, p.DollarsPerKWH, 1e-6)
		}

		// Winter Day (12 PM): $0.1173
		p, err = u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1173, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Federal Holidays Exclusions", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "psegli",
			UtilityRate:     "psegli_194",
		})
		require.NoError(t, err)

		// Memorial Day (last Monday in May): Mon, May 25, 2026 at 4:00 PM -> should be Off-Peak ($0.0929)
		p, err := u.priceForTime(time.Date(2026, time.May, 25, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0929, p.DollarsPerKWH, 1e-6)
		}

		// Christmas Day: Fri, Dec 25, 2026 at 4:00 PM -> should be Off-Peak ($0.0929)
		p, err = u.priceForTime(time.Date(2026, time.December, 25, 16, 0, 0, 0, ny))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0929, p.DollarsPerKWH, 1e-6)
		}
	})
}
