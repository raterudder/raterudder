package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBGE(t *testing.T) {
	t.Run("Schedule R Residential Service", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_r",
		})
		require.NoError(t, err)

		// Pre-Oct 2026 Non-Summer (January 15, 2026 at 12:00 PM)
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19103, p.DollarsPerKWH, 1e-6)

		// Summer (July 15, 2026 at 12:00 PM) -> June - Sept
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19700, p.DollarsPerKWH, 1e-6)

		// Pre-Oct 2026 boundary end (September 30, 2026 at 11:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.September, 30, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.19700, p.DollarsPerKWH, 1e-6)

		// Oct 1, 2026 boundary start -> Supp. 755 SOS + delivery + EmPOWER + Rider 8 credit ($0.20309/kWh)
		p, err = u.priceForTime(time.Date(2026, time.October, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.20309, p.DollarsPerKWH, 1e-6)

		// Non-Summer (November 15, 2026 at 12:00 PM) -> Oct 2026 - Jan 2027
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.20309, p.DollarsPerKWH, 1e-6)

		// January 2027 (Rider 8 credit still active through Jan 31, 2027)
		p, err = u.priceForTime(time.Date(2027, time.January, 31, 23, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.20309, p.DollarsPerKWH, 1e-6)

		// Feb 1, 2027 -> Rider 8 expired ($0.21359/kWh)
		p, err = u.priceForTime(time.Date(2027, time.February, 1, 0, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21359, p.DollarsPerKWH, 1e-6)

		// Summer 2027 SOS (July 15, 2027 at 12:00 PM) -> $0.21671/kWh
		p, err = u.priceForTime(time.Date(2027, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21671, p.DollarsPerKWH, 1e-6)

		// Non-Summer 2027-2028 carry-forward (November 15, 2027 & March 15, 2028) -> $0.21359/kWh
		p, err = u.priceForTime(time.Date(2027, time.November, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21359, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2028, time.March, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21359, p.DollarsPerKWH, 1e-6)

		// Summer 2028 carry-forward (July 15, 2028 at 12:00 PM) -> $0.21671/kWh
		p, err = u.priceForTime(time.Date(2028, time.July, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21671, p.DollarsPerKWH, 1e-6)

		// Late 2028 Non-Summer carry-forward (December 15, 2028 at 12:00 PM) -> $0.21359/kWh
		p, err = u.priceForTime(time.Date(2028, time.December, 15, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21359, p.DollarsPerKWH, 1e-6)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Schedule RL Residential Optional TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_rl",
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, p := range periods {
			names[p.Name] = true
		}
		assert.True(t, names["On-Peak"])
		assert.True(t, names["Inter-Peak"])
		assert.True(t, names["Off-Peak"])

		// Summer Peak: Weekday (Monday July 13, 2026 at 12:00 PM) -> 10 AM to 8 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32152, p.DollarsPerKWH, 1e-6)

		// Summer Intermediate: Weekday (Monday July 13, 2026 at 8:00 AM) -> 7 AM to 10 AM
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.22152, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: Weekday (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15152, p.DollarsPerKWH, 1e-6)

		// Summer Weekend Off-Peak (Saturday July 18, 2026 at 12:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.July, 18, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15152, p.DollarsPerKWH, 1e-6)

		// Non-Summer Peak: Weekday (Monday January 12, 2026 at 8:00 AM) -> 7 AM to 11 AM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29152, p.DollarsPerKWH, 1e-6)

		// Non-Summer Intermediate: Weekday (Monday January 12, 2026 at 12:00 PM) -> 11 AM to 5 PM
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.21152, p.DollarsPerKWH, 1e-6)

		// Non-Summer Off-Peak: Weekday (Monday January 12, 2026 at 10:00 PM)
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 22, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15652, p.DollarsPerKWH, 1e-6)

		// Holiday (President's Day: Monday February 16, 2026 at 12:00 PM) -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.February, 16, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15652, p.DollarsPerKWH, 1e-6)

		t.Run("2026-2028 active rate windows", func(t *testing.T) {
			// Oct 2026 - Jan 2027 (Supp. 755 + EmPOWER + Rider 8): $0.30367 / $0.17359 / $0.16671
			// Monday November 16, 2026 (unshifted Non-Summer)
			pPeak, err := u.priceForTime(time.Date(2026, time.November, 16, 8, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.30367, pPeak.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", pPeak.PeriodName)

			pInter, err := u.priceForTime(time.Date(2026, time.November, 16, 12, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.17359, pInter.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Inter-Peak", pInter.PeriodName)

			pOff, err := u.priceForTime(time.Date(2026, time.November, 16, 22, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.16671, pOff.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pOff.PeriodName)

			// Feb 2027 - May 2027 (Rider 8 expired): $0.31417 / $0.18409 / $0.17721
			// Tuesday February 2, 2027 (unshifted Non-Summer)
			pPeakFeb, err := u.priceForTime(time.Date(2027, time.February, 2, 8, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.31417, pPeakFeb.DollarsPerKWH, 1e-6)

			pInterFeb, err := u.priceForTime(time.Date(2027, time.February, 2, 12, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.18409, pInterFeb.DollarsPerKWH, 1e-6)

			pOffFeb, err := u.priceForTime(time.Date(2027, time.February, 2, 22, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.17721, pOffFeb.DollarsPerKWH, 1e-6)

			// Summer 2027 SOS (Monday July 12, 2027): $0.30025 / $0.18100 / $0.17960
			pPeakSum27, err := u.priceForTime(time.Date(2027, time.July, 12, 12, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.30025, pPeakSum27.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", pPeakSum27.PeriodName)

			pInterSum27, err := u.priceForTime(time.Date(2027, time.July, 12, 8, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.18100, pInterSum27.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Inter-Peak", pInterSum27.PeriodName)

			pOffSum27, err := u.priceForTime(time.Date(2027, time.July, 12, 2, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.17960, pOffSum27.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pOffSum27.PeriodName)

			// Summer 2028 carry-forward (Monday July 10, 2028) & Non-Summer 2028 (Monday Nov 13, 2028)
			pPeakSum28, err := u.priceForTime(time.Date(2028, time.July, 10, 14, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.30025, pPeakSum28.DollarsPerKWH, 1e-6)

			pPeakNov28, err := u.priceForTime(time.Date(2028, time.November, 13, 8, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.InDelta(t, 0.31417, pPeakNov28.DollarsPerKWH, 1e-6)
		})

		t.Run("Spring and Fall DST +1h weekday shifts", func(t *testing.T) {
			// Spring 2027 DST transition: Sun Mar 14, 2027 00:00 through Sun Apr 4, 2027 00:00.
			// Weekday Monday Mar 15, 2027 -> On-Peak 08-12 & 18-22, Inter-Peak 12-18, Off-Peak 00-08 & 22-24.
			p7am, err := u.priceForTime(time.Date(2027, time.March, 15, 7, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Off-Peak", p7am.PeriodName)
			assert.InDelta(t, 0.17721, p7am.DollarsPerKWH, 1e-6)

			p8am, err := u.priceForTime(time.Date(2027, time.March, 15, 8, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", p8am.PeriodName)
			assert.InDelta(t, 0.31417, p8am.DollarsPerKWH, 1e-6)

			p11am, err := u.priceForTime(time.Date(2027, time.March, 15, 11, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", p11am.PeriodName)

			p12pm, err := u.priceForTime(time.Date(2027, time.March, 15, 12, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Inter-Peak", p12pm.PeriodName)
			assert.InDelta(t, 0.18409, p12pm.DollarsPerKWH, 1e-6)

			p5pm, err := u.priceForTime(time.Date(2027, time.March, 15, 17, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Inter-Peak", p5pm.PeriodName)

			p6pm, err := u.priceForTime(time.Date(2027, time.March, 15, 18, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", p6pm.PeriodName)

			p9pm, err := u.priceForTime(time.Date(2027, time.March, 15, 21, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", p9pm.PeriodName)

			p10pm, err := u.priceForTime(time.Date(2027, time.March, 15, 22, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Off-Peak", p10pm.PeriodName)

			// Monday Apr 5, 2027 (after 1st Sunday in April) -> back to unshifted 07-11 & 17-21 On-Peak
			pAfterSpring7am, err := u.priceForTime(time.Date(2027, time.April, 5, 7, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", pAfterSpring7am.PeriodName)

			pAfterSpring9pm, err := u.priceForTime(time.Date(2027, time.April, 5, 21, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Off-Peak", pAfterSpring9pm.PeriodName)

			// Fall 2027 cross-month DST transition: Sun Oct 31, 2027 00:00 through Sun Nov 7, 2027 00:00.
			// Wednesday Nov 3, 2027 is shifted (+1h); Monday Nov 8, 2027 is unshifted.
			pFall7am, err := u.priceForTime(time.Date(2027, time.November, 3, 7, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Off-Peak", pFall7am.PeriodName)

			pFall9pm, err := u.priceForTime(time.Date(2027, time.November, 3, 21, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", pFall9pm.PeriodName)

			pPostFall7am, err := u.priceForTime(time.Date(2027, time.November, 8, 7, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", pPostFall7am.PeriodName)

			// Fall 2028 cross-month DST transition: Sun Oct 29, 2028 00:00 through Sun Nov 5, 2028 00:00.
			// Tuesday Oct 31, 2028 and Wednesday Nov 1, 2028 are both shifted (+1h).
			pOct31_28, err := u.priceForTime(time.Date(2028, time.October, 31, 7, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "Off-Peak", pOct31_28.PeriodName)

			pNov1_28, err := u.priceForTime(time.Date(2028, time.November, 1, 21, 0, 0, 0, etLocation))
			require.NoError(t, err)
			assert.Equal(t, "On-Peak", pNov1_28.PeriodName)
		})
	})

	t.Run("Schedule EV Residential EV TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_ev",
		})
		require.NoError(t, err)

		// Summer Peak: Weekday (Monday July 13, 2026 at 4:00 PM) -> 10 AM to 8 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.32181, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: Weekday (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15181, p.DollarsPerKWH, 1e-6)

		// Pre-Oct 2026 Non-Summer (Monday Jan 12, 2026) -> $0.29181 / $0.15681
		p, err = u.priceForTime(time.Date(2026, time.January, 12, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.29181, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.January, 12, 12, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.15681, p.DollarsPerKWH, 1e-6)

		// Oct 2026 - Jan 2027 (with Rider 8): $0.30569 / $0.16817
		// Fall 2026 DST transition week: Monday Oct 26, 2026 -> On-Peak 08-12 & 18-22
		p, err = u.priceForTime(time.Date(2026, time.October, 26, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.16817, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 26, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.30569, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.October, 26, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.30569, p.DollarsPerKWH, 1e-6)

		// Post-Rider 8 Non-Summer (Feb 2027+) & Summer 2027/2028 carry-forward: $0.31619 / $0.17867
		p, err = u.priceForTime(time.Date(2027, time.February, 10, 8, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.31619, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 12, 15, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.31619, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 12, 22, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.17867, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Schedule RD Residential Delivery & Energy TOU", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "bge",
			UtilityRate:     "bge_rd",
		})
		require.NoError(t, err)

		// Summer Peak: Weekday (Monday July 13, 2026 at 4:00 PM) -> 3 PM to 8 PM
		p, err := u.priceForTime(time.Date(2026, time.July, 13, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.38353, p.DollarsPerKWH, 1e-6)

		// Summer Off-Peak: Weekday (Monday July 13, 2026 at 2:00 AM)
		p, err = u.priceForTime(time.Date(2026, time.July, 13, 2, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.13664, p.DollarsPerKWH, 1e-6)

		// Oct 2026 - Jan 2027 (Supp. 749 + Supp. 755 + EmPOWER + Rider 8): $0.36868 / $0.15392
		// Unshifted Non-Summer weekday (Monday Nov 16, 2026) -> On-Peak 06-09 & 17-21
		p, err = u.priceForTime(time.Date(2026, time.November, 16, 6, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.36868, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2026, time.November, 16, 9, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.15392, p.DollarsPerKWH, 1e-6)

		// Spring 2028 DST transition (Monday Mar 13, 2028) -> On-Peak shifted to 07-10 & 18-22 ($0.37918 / $0.16442)
		p, err = u.priceForTime(time.Date(2028, time.March, 13, 6, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.16442, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2028, time.March, 13, 7, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.37918, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2028, time.March, 13, 9, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.37918, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2028, time.March, 13, 10, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.16442, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2028, time.March, 13, 17, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)

		p, err = u.priceForTime(time.Date(2028, time.March, 13, 21, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)

		// Summer 2027 carry-forward (Monday July 12, 2027) -> On-Peak 15-20 ($0.37918 / $0.16442)
		p, err = u.priceForTime(time.Date(2027, time.July, 12, 16, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "On-Peak", p.PeriodName)
		assert.InDelta(t, 0.37918, p.DollarsPerKWH, 1e-6)

		p, err = u.priceForTime(time.Date(2027, time.July, 12, 14, 0, 0, 0, etLocation))
		require.NoError(t, err)
		assert.Equal(t, "Off-Peak", p.PeriodName)
		assert.InDelta(t, 0.16442, p.DollarsPerKWH, 1e-6)
	})

	t.Run("Holidays, DST boundary Sundays, and 24-hour namedPeriodsCoverDay", func(t *testing.T) {
		assertSingleOffPeakDay := func(t *testing.T, plan string, day time.Time, expectedHours int) {
			t.Helper()
			fees := bgePeriods(plan, types.UtilityRateOptions{}, []int{2026, 2027, 2028})
			var named []types.TimePeriod
			for _, fp := range fees {
				if fp.Name != "" {
					named = append(named, fp.TimePeriod)
				}
			}
			assert.True(t, namedPeriodsCoverDay(named, day), "%s should cover full day %s", plan, day.Format("2006-01-02"))

			dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, etLocation)
			nextDay := dayStart.AddDate(0, 0, 1)
			elapsedHours := 0
			for ts := dayStart; ts.Before(nextDay); ts = ts.Add(time.Hour) {
				elapsedHours++
				matchCount := 0
				var matchedName string
				for i := range fees {
					ok, _, err := fees[i].Contains(ts)
					require.NoError(t, err)
					if ok {
						matchCount++
						matchedName = fees[i].Name
					}
				}
				assert.Equal(t, 1, matchCount, "%s at %v should match exactly 1 period", plan, ts)
				assert.Equal(t, "Off-Peak", matchedName, "%s at %v should match Off-Peak", plan, ts)
			}
			assert.Equal(t, expectedHours, elapsedHours)
		}

		verifyTOUPlanEdgeDays := func(t *testing.T, plan string) {
			t.Helper()
			// 23-hour spring-forward Sundays (2nd Sunday in March)
			assertSingleOffPeakDay(t, plan, time.Date(2026, time.March, 8, 12, 0, 0, 0, etLocation), 23)
			assertSingleOffPeakDay(t, plan, time.Date(2027, time.March, 14, 12, 0, 0, 0, etLocation), 23)
			assertSingleOffPeakDay(t, plan, time.Date(2028, time.March, 12, 12, 0, 0, 0, etLocation), 23)

			// 25-hour fall-back Sundays (1st Sunday in November)
			assertSingleOffPeakDay(t, plan, time.Date(2026, time.November, 1, 12, 0, 0, 0, etLocation), 25)
			assertSingleOffPeakDay(t, plan, time.Date(2027, time.November, 7, 12, 0, 0, 0, etLocation), 25)
			assertSingleOffPeakDay(t, plan, time.Date(2028, time.November, 5, 12, 0, 0, 0, etLocation), 25)

			// Good Friday falling inside spring DST transition weeks (2026-04-03 and 2027-03-26)
			assertSingleOffPeakDay(t, plan, time.Date(2026, time.April, 3, 12, 0, 0, 0, etLocation), 24)
			assertSingleOffPeakDay(t, plan, time.Date(2027, time.March, 26, 12, 0, 0, 0, etLocation), 24)

			// Sunday holiday shifted to Monday: July 4, 2027 (Sunday) -> observed Monday July 5, 2027
			assertSingleOffPeakDay(t, plan, time.Date(2027, time.July, 5, 12, 0, 0, 0, etLocation), 24)

			// Named periods cover transition weekdays and cross-month fall DST weeks
			fees := bgePeriods(plan, types.UtilityRateOptions{}, []int{2026, 2027, 2028})
			var named []types.TimePeriod
			for _, fp := range fees {
				if fp.Name != "" {
					named = append(named, fp.TimePeriod)
				}
			}
			assert.True(t, namedPeriodsCoverDay(named, time.Date(2026, time.October, 28, 12, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(named, time.Date(2027, time.October, 31, 12, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(named, time.Date(2027, time.November, 3, 12, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(named, time.Date(2028, time.October, 31, 12, 0, 0, 0, etLocation)))
			assert.True(t, namedPeriodsCoverDay(named, time.Date(2028, time.November, 2, 12, 0, 0, 0, etLocation)))
		}

		t.Run("bge_rl", func(t *testing.T) {
			verifyTOUPlanEdgeDays(t, "bge_rl")
		})
		t.Run("bge_ev", func(t *testing.T) {
			verifyTOUPlanEdgeDays(t, "bge_ev")
		})
		t.Run("bge_rd", func(t *testing.T) {
			verifyTOUPlanEdgeDays(t, "bge_rd")
		})
	})

	t.Run("Exhaustive 2026-2028 hourly continuity and Rider 18 1:1 net metering", func(t *testing.T) {
		verifyPlanContinuity := func(t *testing.T, plan string, expectNamed bool) {
			t.Helper()
			fees := bgePeriods(plan, types.UtilityRateOptions{NetMeteringCredits: true}, []int{2026, 2027, 2028})
			start := time.Date(2026, time.January, 1, 0, 0, 0, 0, etLocation)
			end := time.Date(2029, time.January, 1, 0, 0, 0, 0, etLocation)

			for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
				matches := 0
				var matched types.UtilityFeesPeriod
				for i := range fees {
					ok, _, err := fees[i].Contains(ts)
					if err != nil {
						require.NoError(t, err)
					}
					if ok {
						matches++
						matched = fees[i]
					}
				}
				if matches != 1 {
					require.Equal(t, 1, matches, "%s expected 1 period at %v", plan, ts)
				}
				if expectNamed && matched.Name == "" {
					require.NotEmpty(t, matched.Name, "%s expected named period at %v", plan, ts)
				}
				if !expectNamed && matched.Name != "" {
					require.Empty(t, matched.Name, "%s expected unnamed period at %v", plan, ts)
				}
				if matched.SeparateGenerationCredit || matched.GridAdditional {
					require.False(t, matched.SeparateGenerationCredit || matched.GridAdditional, "%s should maintain 1:1 net metering under Rider 18", plan)
				}
			}
		}

		t.Run("bge_r", func(t *testing.T) {
			verifyPlanContinuity(t, "bge_r", false)
		})
		t.Run("bge_rl", func(t *testing.T) {
			verifyPlanContinuity(t, "bge_rl", true)
		})
		t.Run("bge_ev", func(t *testing.T) {
			verifyPlanContinuity(t, "bge_ev", true)
		})
		t.Run("bge_rd", func(t *testing.T) {
			verifyPlanContinuity(t, "bge_rd", true)
		})
	})
}
