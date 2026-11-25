package utility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockHTTPClient(handler http.HandlerFunc) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler(rec, req)
			return rec.Result(), nil
		}),
	}
}

func isTransientOctopusAPIError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return strings.Contains(err.Error(), "octopus api error status 5") ||
		strings.Contains(err.Error(), "octopus api error status 429") ||
		errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout())
}

func TestOctopusUtility(t *testing.T) {
	t.Run("Provider Metadata and RatesNotice", func(t *testing.T) {
		info := octopusUtilityInfo()
		assert.Equal(t, "octopus", info.ID)
		assert.Equal(t, "Octopus Energy (UK)", info.Name)
		assert.Empty(t, info.RatesNotice)
		require.Len(t, info.Rates, 5)
		assert.Equal(t, "octopus_flux", info.Rates[0].ID)
		assert.Equal(t, "Octopus Flux", info.Rates[0].Name)
		assert.Len(t, info.Rates[0].Options, 1)
		assert.Equal(t, "octopus_intelligent_flux", info.Rates[1].ID)
		assert.Equal(t, "Intelligent Octopus Flux", info.Rates[1].Name)
		assert.Len(t, info.Rates[1].Options, 1)
		assert.Equal(t, "octopus_go", info.Rates[2].ID)
		assert.Equal(t, "Octopus Go", info.Rates[2].Name)
		if assert.Len(t, info.Rates[2].Options, 3) {
			assert.Equal(t, "rateClass", info.Rates[2].Options[0].Field)
			assert.Equal(t, "flexible", info.Rates[2].Options[0].Default)
			assert.Equal(t, "location", info.Rates[2].Options[1].Field)
			assert.Equal(t, "generationRate", info.Rates[2].Options[2].Field)
			assert.Equal(t, "outgoing_12p", info.Rates[2].Options[2].Default)
		}
		assert.Equal(t, "octopus_intelligent_go", info.Rates[3].ID)
		assert.Equal(t, "Intelligent Octopus Go", info.Rates[3].Name)
		if assert.Len(t, info.Rates[3].Options, 3) {
			assert.Equal(t, "rateClass", info.Rates[3].Options[0].Field)
			assert.Equal(t, "flexible", info.Rates[3].Options[0].Default)
			assert.Len(t, info.Rates[3].Options[0].Choices, 4)
			assert.Equal(t, "location", info.Rates[3].Options[1].Field)
			assert.Equal(t, "generationRate", info.Rates[3].Options[2].Field)
			assert.Equal(t, "outgoing_12p", info.Rates[3].Options[2].Default)
		}
		assert.Equal(t, "octopus_cosy", info.Rates[4].ID)
		assert.Equal(t, "Cosy Octopus", info.Rates[4].Name)
		if assert.Len(t, info.Rates[4].Options, 3) {
			assert.Equal(t, "rateClass", info.Rates[4].Options[0].Field)
			assert.Equal(t, "flexible", info.Rates[4].Options[0].Default)
			assert.Equal(t, "location", info.Rates[4].Options[1].Field)
			assert.Equal(t, "generationRate", info.Rates[4].Options[2].Field)
			assert.Equal(t, "outgoing_12p", info.Rates[4].Options[2].Default)
		}
	})

	t.Run("Octopus Flux - London", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.Len(t, periods, 6) {
			assert.Equal(t, "Off-Peak", periods[0].Name)
			assert.Equal(t, "Peak", periods[1].Name)
			assert.Equal(t, "Standard", periods[2].Name)
			assert.Equal(t, "Off-Peak", periods[3].Name)
			assert.Equal(t, "Peak", periods[4].Name)
			assert.Equal(t, "Standard", periods[5].Name)
		}

		// Pre-2026-10-19 historical rates:
		// Off-Peak (02:00 – 05:00): 03:00 -> Import £0.1456, Export £0.0524
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 3, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1456, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0524, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Peak (16:00 – 19:00): 17:00 -> Import £0.3396, Export £0.2860
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3396, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.2860, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Standard (05:00 – 16:00, 19:00 – 02:00):
		// 10:00 -> Import £0.2425, Export £0.0980
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 10, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// 21:00 -> Import £0.2425, Export £0.0980
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 21, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// 01:00 -> Import £0.2425, Export £0.0980
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 1, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, p.GenerationCreditDollarsPerKWH, 1e-4)
		}
	})

	t.Run("Octopus Flux - 2026-10-19 Transition and DST Continuity", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		// Last hour before 2026-10-19T00:00:00 BST: 2026-10-18 23:00 BST -> pre-revision Standard (£0.2425 / £0.0980)
		pBefore, err := u.priceForTime(time.Date(2026, time.October, 18, 23, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2425, pBefore.DollarsPerKWH, 1e-6)
			assert.True(t, pBefore.SeparateGenerationCredit)
			assert.InDelta(t, 0.0980, pBefore.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Standard", pBefore.PeriodName)
		}

		// Exact 2026-10-19T00:00:00 BST boundary: Q4 2026 Standard (£0.267752 / £0.111143)
		pAfterStd, err := u.priceForTime(time.Date(2026, time.October, 19, 0, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.267752, pAfterStd.DollarsPerKWH, 1e-6)
			assert.True(t, pAfterStd.SeparateGenerationCredit)
			assert.InDelta(t, 0.111143, pAfterStd.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Standard", pAfterStd.PeriodName)
		}

		// Q4 2026 Off-Peak (03:00) and Peak (17:00) in London
		pAfterOff, err := u.priceForTime(time.Date(2026, time.October, 19, 3, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.161662, pAfterOff.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.047185, pAfterOff.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pAfterOff.PeriodName)
		}
		pAfterPeak, err := u.priceForTime(time.Date(2026, time.October, 19, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.373843, pAfterPeak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.303116, pAfterPeak.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Peak", pAfterPeak.PeriodName)
		}

		// Verify 48-hour GetConfirmedPrices across the 2026-10-19 cutoff has no gaps or overlaps
		winStart := time.Date(2026, time.October, 18, 0, 0, 0, 0, lonLocation)
		winEnd := time.Date(2026, time.October, 20, 0, 0, 0, 0, lonLocation)
		confirmed, err := u.GetConfirmedPrices(context.Background(), winStart, winEnd)
		require.NoError(t, err)
		if assert.Len(t, confirmed, 48) {
			assert.InDelta(t, 0.2425, confirmed[23].DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.267752, confirmed[24].DollarsPerKWH, 1e-6)
			for i := 1; i < len(confirmed); i++ {
				assert.Equal(t, confirmed[i-1].TSEnd, confirmed[i].TSStart)
			}
		}

		// Verify 25-hour UK BST->GMT fall-back day (Sunday Oct 25, 2026) has continuous coverage
		dstStart := time.Date(2026, time.October, 25, 0, 0, 0, 0, lonLocation)
		dstEnd := dstStart.AddDate(0, 0, 1)
		dstConfirmed, err := u.GetConfirmedPrices(context.Background(), dstStart, dstEnd)
		require.NoError(t, err)
		if assert.Len(t, dstConfirmed, 25) {
			for i := 1; i < len(dstConfirmed); i++ {
				assert.Equal(t, dstConfirmed[i-1].TSEnd, dstConfirmed[i].TSStart)
			}
		}
	})

	t.Run("Octopus Flux - Eastern", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "eastern",
			},
		})
		require.NoError(t, err)

		// Q4 2026 (post-2026-10-19):
		// Off-Peak 03:00 -> Import £0.161769, Export £0.047216
		p, err := u.priceForTime(time.Date(2026, time.November, 10, 3, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.161769, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.047216, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Peak 17:00 -> Import £0.374090, Export £0.303317
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.374090, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.303317, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Standard 12:00 -> Import £0.267930, Export £0.111216
		p, err = u.priceForTime(time.Date(2026, time.November, 10, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.267930, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.111216, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Intelligent Octopus Flux - Parity and Midnight Split", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.Len(t, periods, 2) {
			assert.Equal(t, "Peak", periods[0].Name)
			assert.Equal(t, "Standard", periods[1].Name)
		}

		// Morning Standard (00:00 – 16:00): 03:00 -> 1:1 parity £0.242493
		pStdMorning, err := u.priceForTime(time.Date(2026, time.November, 15, 3, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.242493, pStdMorning.DollarsPerKWH, 1e-6)
			assert.True(t, pStdMorning.SeparateGenerationCredit)
			assert.InDelta(t, 0.242493, pStdMorning.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Standard", pStdMorning.PeriodName)
		}

		// Peak (16:00 – 19:00): 17:00 -> 1:1 parity £0.303116
		pPeak, err := u.priceForTime(time.Date(2026, time.November, 15, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.303116, pPeak.DollarsPerKWH, 1e-6)
			assert.True(t, pPeak.SeparateGenerationCredit)
			assert.InDelta(t, 0.303116, pPeak.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Peak", pPeak.PeriodName)
		}

		// Evening Standard (19:00 – 24:00): 23:00 -> 1:1 parity £0.242493
		pStdEvening, err := u.priceForTime(time.Date(2026, time.November, 15, 23, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.242493, pStdEvening.DollarsPerKWH, 1e-6)
			assert.True(t, pStdEvening.SeparateGenerationCredit)
			assert.InDelta(t, 0.242493, pStdEvening.GenerationCreditDollarsPerKWH, 1e-6)
			assert.Equal(t, "Standard", pStdEvening.PeriodName)
		}

		// Verify GetFuturePrices across midnight (hStart < hEnd preserved by 00:00-16:00 / 19:00-24:00 split)
		u.nowFunc = func() time.Time { return time.Date(2026, time.November, 15, 22, 30, 0, 0, lonLocation) }
		future, err := u.GetFuturePrices(context.Background())
		require.NoError(t, err)
		assert.Len(t, future, 48)
	})

	t.Run("Octopus Go - London", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		// 00:15 -> Day Peak (£0.3453), default export outgoing_12p (£0.1200)
		p, err := u.priceForTime(time.Date(2026, time.July, 15, 0, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3453, p.DollarsPerKWH, 1e-4)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1200, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Overnight Off-Peak (00:30 – 05:30):
		// 00:45 -> Off-Peak (£0.0940)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 0, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0940, p.DollarsPerKWH, 1e-4)
		}

		// 05:15 -> Off-Peak (£0.0940)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 5, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0940, p.DollarsPerKWH, 1e-4)
		}

		// Day Peak (05:30 – 00:30):
		// 05:45 -> Peak (£0.3453)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 5, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3453, p.DollarsPerKWH, 1e-4)
		}

		// 23:45 -> Peak (£0.3453)
		p, err = u.priceForTime(time.Date(2026, time.July, 15, 23, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3453, p.DollarsPerKWH, 1e-4)
		}

		// Octopus Go 12M Fixed (GO-FIX-12M-26-10-30) in London: £0.0940 off-peak, £0.383123 peak
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_go",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "fixed",
				Location:  "london",
			},
		})
		require.NoError(t, err)
		pFixedOff, err := u.priceForTime(time.Date(2026, time.November, 15, 2, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0940, pFixedOff.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1200, pFixedOff.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pFixedPeak, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.383123, pFixedPeak.DollarsPerKWH, 1e-6)
		}

		// Stale RateClass from another plan falls back to flexible
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_go",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "legacy_24_10_29",
				Location:  "london",
			},
		})
		require.NoError(t, err)
		pFallback, err := u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3453, pFallback.DollarsPerKWH, 1e-4)
		}
	})

	t.Run("Intelligent Octopus Go - London", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		// Default flexible (IOG-SMB-VAR-26-11-04):
		// Overnight Off-Peak (23:30 – 05:30):
		// 02:00 -> Import £0.0990, Export £0.1200 (default outgoing_12p)
		p, err := u.priceForTime(time.Date(2026, time.November, 15, 2, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0990, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 23:45 -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 23, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0990, p.DollarsPerKWH, 1e-6)
		}

		// 05:15 -> Off-Peak
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 5, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0990, p.DollarsPerKWH, 1e-6)
		}

		// Day Peak (05:30 – 23:30):
		// 05:45 -> Peak (£0.341553)
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 5, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.341553, p.DollarsPerKWH, 1e-6)
		}

		// 12:00 -> Peak (£0.341553)
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.341553, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1200, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// 23:15 -> Peak (£0.341553)
		p, err = u.priceForTime(time.Date(2026, time.November, 15, 23, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.341553, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Intelligent Octopus Go - 2026-11-04 Transition", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.Len(t, periods, 4) {
			assert.Equal(t, "Off-Peak", periods[0].Name)
			assert.Equal(t, "Peak", periods[1].Name)
			assert.Equal(t, "Off-Peak", periods[2].Name)
			assert.Equal(t, "Peak", periods[3].Name)
		}

		// Pre-2026-11-04 (INTELLI-VAR-22-10-14 in London): Peak £0.2802, Off-Peak £0.0700
		pPrePeak, err := u.priceForTime(time.Date(2026, time.November, 3, 23, 15, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2802, pPrePeak.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Peak", pPrePeak.PeriodName)
			assert.True(t, pPrePeak.SeparateGenerationCredit)
			assert.InDelta(t, 0.1200, pPrePeak.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Pre-midnight half of overnight window [23:30, 24:00) on 2026-11-03 -> £0.0700
		pPreOff, err := u.priceForTime(time.Date(2026, time.November, 3, 23, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0700, pPreOff.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pPreOff.PeriodName)
		}

		// Exact 2026-11-04T00:00:00 GMT boundary (post-midnight half of overnight window [00:00, 05:30)) -> IOG-SMB-VAR-26-11-04 £0.0990
		pPostOff, err := u.priceForTime(time.Date(2026, time.November, 4, 0, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.0990, pPostOff.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", pPostOff.PeriodName)
			assert.True(t, pPostOff.SeparateGenerationCredit)
			assert.InDelta(t, 0.1200, pPostOff.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Morning Peak starting at 05:30 on 2026-11-04 -> £0.341553
		pPostPeak, err := u.priceForTime(time.Date(2026, time.November, 4, 5, 45, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.341553, pPostPeak.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Peak", pPostPeak.PeriodName)
		}

		// Verify 48-hour GetConfirmedPrices continuity across 2026-11-03 00:00 to 2026-11-05 00:00 GMT
		// (2 days * 26 segments/day = 52 segments, covering 23:30-05:30 across midnight 2026-11-04T00:00:00)
		winStart := time.Date(2026, time.November, 3, 0, 0, 0, 0, lonLocation)
		winEnd := time.Date(2026, time.November, 5, 0, 0, 0, 0, lonLocation)
		confirmed, err := u.GetConfirmedPrices(context.Background(), winStart, winEnd)
		require.NoError(t, err)
		if assert.Len(t, confirmed, 52) {
			assert.Equal(t, winStart, confirmed[0].TSStart)
			assert.Equal(t, winEnd, confirmed[len(confirmed)-1].TSEnd)
			for i := 1; i < len(confirmed); i++ {
				assert.Equal(t, confirmed[i-1].TSEnd, confirmed[i].TSStart)
			}

			// 2026-11-03 23:00-23:30 (index 24): pre-revision Peak (£0.2802)
			assert.Equal(t, time.Date(2026, time.November, 3, 23, 0, 0, 0, lonLocation), confirmed[24].TSStart)
			assert.Equal(t, time.Date(2026, time.November, 3, 23, 30, 0, 0, lonLocation), confirmed[24].TSEnd)
			assert.InDelta(t, 0.2802, confirmed[24].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Peak", confirmed[24].PeriodName)

			// 2026-11-03 23:30-24:00 (index 25): pre-revision Off-Peak (£0.0700)
			assert.Equal(t, time.Date(2026, time.November, 3, 23, 30, 0, 0, lonLocation), confirmed[25].TSStart)
			assert.Equal(t, time.Date(2026, time.November, 4, 0, 0, 0, 0, lonLocation), confirmed[25].TSEnd)
			assert.InDelta(t, 0.0700, confirmed[25].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", confirmed[25].PeriodName)

			// 2026-11-04 00:00-01:00 (index 26): Q4 2026 Off-Peak (£0.0990)
			assert.Equal(t, time.Date(2026, time.November, 4, 0, 0, 0, 0, lonLocation), confirmed[26].TSStart)
			assert.Equal(t, time.Date(2026, time.November, 4, 1, 0, 0, 0, lonLocation), confirmed[26].TSEnd)
			assert.InDelta(t, 0.0990, confirmed[26].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", confirmed[26].PeriodName)

			// 2026-11-04 05:00-05:30 (index 31): Q4 2026 Off-Peak (£0.0990)
			assert.Equal(t, time.Date(2026, time.November, 4, 5, 0, 0, 0, lonLocation), confirmed[31].TSStart)
			assert.Equal(t, time.Date(2026, time.November, 4, 5, 30, 0, 0, lonLocation), confirmed[31].TSEnd)
			assert.InDelta(t, 0.0990, confirmed[31].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", confirmed[31].PeriodName)

			// 2026-11-04 05:30-06:00 (index 32): Q4 2026 Peak (£0.341553)
			assert.Equal(t, time.Date(2026, time.November, 4, 5, 30, 0, 0, lonLocation), confirmed[32].TSStart)
			assert.Equal(t, time.Date(2026, time.November, 4, 6, 0, 0, 0, lonLocation), confirmed[32].TSEnd)
			assert.InDelta(t, 0.341553, confirmed[32].DollarsPerKWH, 1e-6)
			assert.Equal(t, "Peak", confirmed[32].PeriodName)
		}
	})

	t.Run("Intelligent Octopus Go - Rate Classes and Fallback", func(t *testing.T) {
		u := &genericTOU{}
		offPeakTS := time.Date(2026, time.November, 15, 2, 0, 0, 0, lonLocation)
		peakTS := time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation)

		// 1. max (IOG-SMB-VAR-26-10-29): £0.0650 off-peak, £0.382729 peak in London
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "max",
				Location:  "london",
			},
		}))
		pOff, err := u.priceForTime(offPeakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.0650, pOff.DollarsPerKWH, 1e-6)
		pPeak, err := u.priceForTime(peakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.382729, pPeak.DollarsPerKWH, 1e-6)

		// 2. fixed (IOG-SMB-FIX-12M-26-11-11): £0.0790 off-peak, £0.404354 peak in London
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "fixed",
				Location:  "london",
			},
		}))
		pOff, err = u.priceForTime(offPeakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.0790, pOff.DollarsPerKWH, 1e-6)
		pPeak, err = u.priceForTime(peakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.404354, pPeak.DollarsPerKWH, 1e-6)

		// 3. legacy_24_10_29 (INTELLI-VAR-24-10-29): £0.0660 off-peak, £0.361200 peak in London
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "legacy_24_10_29",
				Location:  "london",
			},
		}))
		pOff, err = u.priceForTime(offPeakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.0660, pOff.DollarsPerKWH, 1e-6)
		pPeak, err = u.priceForTime(peakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.361200, pPeak.DollarsPerKWH, 1e-6)

		// 4. Stale RateClass falls back to date-bounded flexible (£0.0700 / £0.2802 pre-2026-11-04; £0.0990 / £0.341553 on/after 2026-11-04)
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "stale_class",
				Location:  "london",
			},
		}))
		pPreOff, err := u.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, lonLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.0700, pPreOff.DollarsPerKWH, 1e-6)
		pPrePeak, err := u.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
		require.NoError(t, err)
		assert.InDelta(t, 0.2802, pPrePeak.DollarsPerKWH, 1e-6)
		pOff, err = u.priceForTime(offPeakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.0990, pOff.DollarsPerKWH, 1e-6)
		pPeak, err = u.priceForTime(peakTS)
		require.NoError(t, err)
		assert.InDelta(t, 0.341553, pPeak.DollarsPerKWH, 1e-6)
	})

	t.Run("Cosy Octopus - London", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_cosy",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "london",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.Len(t, periods, 3) {
			assert.Equal(t, "Off-Peak", periods[0].Name)
			assert.Equal(t, "Peak", periods[1].Name)
			assert.Equal(t, "Standard", periods[2].Name)
		}

		// Standard window 1 (00:00 – 04:00): 02:00 -> Import £0.2880, Export £0.1200 (default outgoing_12p)
		p, err := u.priceForTime(time.Date(2026, time.November, 9, 2, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2880, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Standard", p.PeriodName)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.1200, p.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Off-Peak dip 1 (04:00 – 07:00): 05:00 -> Import £0.1412
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 5, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1412, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Standard window 2 (07:00 – 13:00): 10:00 -> Import £0.2880
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 10, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2880, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Standard", p.PeriodName)
		}

		// Off-Peak dip 2 (13:00 – 16:00): 14:00 -> Import £0.1412
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 14, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1412, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Peak (16:00 – 19:00): 17:00 -> Import £0.4319
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4319, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Peak", p.PeriodName)
		}

		// Standard window 3 (19:00 – 22:00): 20:00 -> Import £0.2880
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 20, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2880, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Standard", p.PeriodName)
		}

		// Off-Peak dip 3 (22:00 – 24:00): 23:00 -> Import £0.1412
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 23, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1412, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Test 12M Fixed variant (RateClass: "fixed") in London
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_cosy",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "fixed",
				Location:  "london",
			},
		})
		require.NoError(t, err)

		// Off-Peak (05:00) -> £0.1715
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 5, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1715, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// Standard (10:00) -> £0.3495
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 10, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3495, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Standard", p.PeriodName)
		}

		// Peak (17:00) -> £0.5242
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5242, p.DollarsPerKWH, 1e-4)
			assert.Equal(t, "Peak", p.PeriodName)
		}

		// Stale RateClass from another plan falls back to flexible
		err = u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_cosy",
			UtilityRateOptions: types.UtilityRateOptions{
				RateClass: "max",
				Location:  "london",
			},
		})
		require.NoError(t, err)
		p, err = u.priceForTime(time.Date(2026, time.November, 9, 10, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2880, p.DollarsPerKWH, 1e-4)
		}
	})

	t.Run("Intelligent Octopus Go - Export Options", func(t *testing.T) {
		// Test 12p export (explicit)
		u12 := &genericTOU{}
		err := u12.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_12p",
			},
		})
		require.NoError(t, err)
		p12, err := u12.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1200, p12.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Test 15p fixed export (outgoing_15p)
		u15 := &genericTOU{}
		err = u15.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_15p",
			},
		})
		require.NoError(t, err)
		p15, err := u15.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.1500, p15.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Test Prime Outgoing export (16p/kWh 16:00-19:00, 9p/kWh all other hours)
		uPrime := &genericTOU{}
		err = uPrime.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_prime",
			},
		})
		require.NoError(t, err)

		primePeriods, err := uPrime.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.Len(t, primePeriods, 4) {
			assert.Equal(t, "Off-Peak", primePeriods[0].Name)
			assert.Equal(t, "Peak", primePeriods[1].Name)
			assert.Equal(t, "Off-Peak", primePeriods[2].Name)
			assert.Equal(t, "Peak", primePeriods[3].Name)
		}

		pPrimeOffPeak, err := uPrime.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2802, pPrimeOffPeak.DollarsPerKWH, 1e-4)
			assert.True(t, pPrimeOffPeak.SeparateGenerationCredit)
			assert.InDelta(t, 0.0900, pPrimeOffPeak.GenerationCreditDollarsPerKWH, 1e-4)
		}
		pPrimePeak, err := uPrime.priceForTime(time.Date(2026, time.July, 15, 17, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2802, pPrimePeak.DollarsPerKWH, 1e-4)
			assert.True(t, pPrimePeak.SeparateGenerationCredit)
			assert.InDelta(t, 0.1600, pPrimePeak.GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Verify 24h GetConfirmedPrices for Prime Outgoing across split 05:30/23:30 import and 16:00/19:00 export boundaries
		dayStart := time.Date(2026, time.July, 15, 0, 0, 0, 0, lonLocation)
		confirmedPrime, err := uPrime.GetConfirmedPrices(context.Background(), dayStart, dayStart.Add(24*time.Hour))
		require.NoError(t, err)
		// 24 hours with 2 split hours (05:00-05:30/05:30-06:00 and 23:00-23:30/23:30-24:00) = 26 intervals
		if assert.Len(t, confirmedPrime, 26) {
			// 05:00-05:30: Import £0.0700 (Off-Peak), Export £0.0900
			assert.Equal(t, dayStart.Add(5*time.Hour), confirmedPrime[5].TSStart)
			assert.Equal(t, dayStart.Add(5*time.Hour+30*time.Minute), confirmedPrime[5].TSEnd)
			assert.InDelta(t, 0.0700, confirmedPrime[5].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, confirmedPrime[5].GenerationCreditDollarsPerKWH, 1e-4)

			// 05:30-06:00: Import £0.2802 (Peak), Export £0.0900
			assert.Equal(t, dayStart.Add(5*time.Hour+30*time.Minute), confirmedPrime[6].TSStart)
			assert.Equal(t, dayStart.Add(6*time.Hour), confirmedPrime[6].TSEnd)
			assert.InDelta(t, 0.2802, confirmedPrime[6].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, confirmedPrime[6].GenerationCreditDollarsPerKWH, 1e-4)

			// 16:00-17:00 (index 17): Import £0.2802 (Peak), Export £0.1600 (Prime Export)
			assert.Equal(t, dayStart.Add(16*time.Hour), confirmedPrime[17].TSStart)
			assert.InDelta(t, 0.2802, confirmedPrime[17].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.1600, confirmedPrime[17].GenerationCreditDollarsPerKWH, 1e-4)

			// 19:00-20:00 (index 20): Import £0.2802 (Peak), Export £0.0900 (Off-Peak Export)
			assert.Equal(t, dayStart.Add(19*time.Hour), confirmedPrime[20].TSStart)
			assert.InDelta(t, 0.2802, confirmedPrime[20].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, confirmedPrime[20].GenerationCreditDollarsPerKWH, 1e-4)

			// 23:00-23:30 (index 24): Import £0.2802 (Peak), Export £0.0900
			assert.Equal(t, dayStart.Add(23*time.Hour), confirmedPrime[24].TSStart)
			assert.Equal(t, dayStart.Add(23*time.Hour+30*time.Minute), confirmedPrime[24].TSEnd)
			assert.InDelta(t, 0.2802, confirmedPrime[24].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, confirmedPrime[24].GenerationCreditDollarsPerKWH, 1e-4)

			// 23:30-24:00 (index 25): Import £0.0700 (Off-Peak), Export £0.0900
			assert.Equal(t, dayStart.Add(23*time.Hour+30*time.Minute), confirmedPrime[25].TSStart)
			assert.Equal(t, dayStart.Add(24*time.Hour), confirmedPrime[25].TSEnd)
			assert.InDelta(t, 0.0700, confirmedPrime[25].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, confirmedPrime[25].GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Verify GetFuturePrices at 05:15 includes the remaining [05:30, 06:00) half-hour segment
		uPrime.nowFunc = func() time.Time { return time.Date(2026, time.July, 15, 5, 15, 0, 0, lonLocation) }
		futureAt0515, err := uPrime.GetFuturePrices(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, futureAt0515) {
			assert.Equal(t, dayStart.Add(5*time.Hour+30*time.Minute), futureAt0515[0].TSStart)
			assert.Equal(t, dayStart.Add(6*time.Hour), futureAt0515[0].TSEnd)
			assert.InDelta(t, 0.2802, futureAt0515[0].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, futureAt0515[0].GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Verify GetFuturePrices at 23:15 includes the remaining [23:30, 24:00) half-hour segment
		uPrime.nowFunc = func() time.Time { return time.Date(2026, time.July, 15, 23, 15, 0, 0, lonLocation) }
		futureAt2315, err := uPrime.GetFuturePrices(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, futureAt2315) {
			assert.Equal(t, dayStart.Add(23*time.Hour+30*time.Minute), futureAt2315[0].TSStart)
			assert.Equal(t, dayStart.Add(24*time.Hour), futureAt2315[0].TSEnd)
			assert.InDelta(t, 0.0700, futureAt2315[0].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.0900, futureAt2315[0].GenerationCreditDollarsPerKWH, 1e-4)
		}

		// Test none export
		uNone := &genericTOU{}
		err = uNone.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "none",
			},
		})
		require.NoError(t, err)
		pNone, err := uNone.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
		if assert.NoError(t, err) {
			assert.False(t, pNone.SeparateGenerationCredit)
			assert.InDelta(t, 0.0, pNone.GenerationCreditDollarsPerKWH, 1e-4)
		}
	})

	t.Run("Agile Outgoing API, Memory Caching, and DB Persistence", func(t *testing.T) {
		fixedNow := time.Date(2026, time.October, 12, 10, 15, 0, 0, lonLocation)
		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)

		var httpHits atomic.Int32
		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			httpHits.Add(1)
			assert.Equal(t, "/v1/products/AGILE-OUTGOING-19-05-13/electricity-tariffs/E-1R-AGILE-OUTGOING-19-05-13-C/standard-unit-rates/", r.URL.Path)

			// Return 48 half-hour slots for dayStart in reverse chronological order (matching Octopus API)
			var results []octopusUnitRateEntry
			for slot := 47; slot >= 0; slot-- {
				st := dayStart.Add(time.Duration(slot) * 30 * time.Minute)
				pencePerKWH := 10.0 + float64(slot)*0.5 // e.g. slot 20 (10:00) = 20.0p (£0.20)
				results = append(results, octopusUnitRateEntry{
					ValueExcVAT: pencePerKWH,
					ValueIncVAT: pencePerKWH,
					ValidFrom:   st.UTC(),
					ValidTo:     st.Add(30 * time.Minute).UTC(),
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count:   len(results),
				Results: results,
			})
		})

		mockDB := &storagemock.MockDatabase{}
		var persistedStates []types.PriceState

		// 1. Cold cache + empty DB -> fetches from HTTP API and upserts to DB
		mockDB.On("GetUtilityPrices", mock.Anything, "octopus_london", dayStart, dayStart.AddDate(0, 0, 2)).
			Return([]types.PriceState{}, nil).Once()
		mockDB.On("UpsertUtilityPrices", mock.Anything, "octopus_london", mock.MatchedBy(func(states []types.PriceState) bool {
			if len(states) != 48 {
				return false
			}
			for _, s := range states {
				if !s.Confirmed || !s.Price.SeparateGenerationCredit || s.Price.DollarsPerKWH != 0 {
					return false
				}
			}
			persistedStates = append([]types.PriceState(nil), states...)
			return true
		}), 0).Return(nil).Once()

		base := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			db:              mockDB,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return fixedNow },
		}

		ctx := context.Background()
		confirmed, err := base.GetConfirmedPrices(ctx, "london", dayStart, dayStart.Add(24*time.Hour))
		require.NoError(t, err)
		if assert.Len(t, confirmed, 48) {
			// Verify sorted ascending by TSStart and converted from pence to £/kWh
			assert.Equal(t, dayStart, confirmed[0].TSStart)
			assert.InDelta(t, 0.10, confirmed[0].GenerationCreditDollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.20, confirmed[20].GenerationCreditDollarsPerKWH, 1e-6)
		}
		assert.Equal(t, int32(1), httpHits.Load())

		// 2. Subsequent GetConfirmedPrices and GetCurrentPrice hit in-memory cache without DB or HTTP calls
		confirmedCached, err := base.GetConfirmedPrices(ctx, "london", dayStart, dayStart.Add(24*time.Hour))
		require.NoError(t, err)
		assert.Len(t, confirmedCached, 48)

		currPrice, err := base.GetCurrentPrice(ctx, "london")
		require.NoError(t, err)
		// fixedNow is 10:15 -> slot 20 (10:00-10:30), rate = 20.0p = £0.20/kWh
		assert.Equal(t, dayStart.Add(10*time.Hour), currPrice.TSStart)
		assert.Equal(t, dayStart.Add(10*time.Hour+30*time.Minute), currPrice.TSEnd)
		assert.InDelta(t, 0.20, currPrice.GenerationCreditDollarsPerKWH, 1e-6)
		assert.True(t, currPrice.SeparateGenerationCredit)
		assert.Equal(t, int32(1), httpHits.Load())

		// 3. Cold memory cache + warm DB cache -> loads from DB without hitting HTTP API
		baseFromDB := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			db:              mockDB,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return fixedNow },
		}
		mockDB.On("GetUtilityPrices", mock.Anything, "octopus_london", dayStart, dayStart.AddDate(0, 0, 2)).
			Return(persistedStates, nil).Once()

		currFromDB, err := baseFromDB.GetCurrentPrice(ctx, "london")
		require.NoError(t, err)
		assert.InDelta(t, 0.20, currFromDB.GenerationCreditDollarsPerKWH, 1e-6)
		assert.Equal(t, int32(1), httpHits.Load())
		mockDB.AssertExpectations(t)
	})

	t.Run("GetFuturePrices Timeline and Cooldown", func(t *testing.T) {
		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)
		currentNow := time.Date(2026, time.October, 12, 10, 15, 0, 0, lonLocation)
		publishTomorrow := false

		var httpHits atomic.Int32
		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			httpHits.Add(1)
			// Before 16:00 UK (or when delayed), Octopus has published slots up to 23:00 today (slots 0..45).
			// Once tomorrow is published, Octopus returns slots through 23:00 tomorrow (slots 0..93).
			maxSlot := 46
			if publishTomorrow {
				maxSlot = 94
			}
			var results []octopusUnitRateEntry
			for slot := maxSlot - 1; slot >= 0; slot-- {
				st := dayStart.Add(time.Duration(slot) * 30 * time.Minute)
				results = append(results, octopusUnitRateEntry{
					ValueExcVAT: 15.0,
					ValueIncVAT: 15.0,
					ValidFrom:   st.UTC(),
					ValidTo:     st.Add(30 * time.Minute).UTC(),
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count:   len(results),
				Results: results,
			})
		})

		base := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return currentNow },
		}

		ctx := context.Background()

		// 1. At 10:15 UK time (before 16:00 publication): returns only published slots from 10:30 to 23:00 today (25 slots)
		future, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		if assert.Len(t, future, 25) {
			assert.Equal(t, dayStart.Add(10*time.Hour+30*time.Minute), future[0].TSStart)
			assert.Equal(t, dayStart.Add(23*time.Hour), future[len(future)-1].TSEnd)
		}
		assert.Equal(t, int32(1), httpHits.Load())

		// 2. At 10:20 UK time: all expected slots up to 23:00 today are already in memory -> no HTTP call
		currentNow = time.Date(2026, time.October, 12, 10, 20, 0, 0, lonLocation)
		future2, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		assert.Len(t, future2, 25)
		assert.Equal(t, int32(1), httpHits.Load())

		// 2b. Verify a cold fetch at 15:55 UK time (just before 16:00) does NOT set lastFutureFetch cooldown across the 16:00 boundary
		delete(base.cachedPrices, "london")
		currentNow = time.Date(2026, time.October, 12, 15, 55, 0, 0, lonLocation)
		futureAt1555, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		assert.Len(t, futureAt1555, 14) // 16:00..23:00 = 14 slots
		assert.Equal(t, int32(2), httpHits.Load())

		// 3. At 16:05 UK time (after 16:00, but tomorrow's rates are delayed): queries API once (not blocked by 15:55 fetch), gets today's remaining slots (16:30..23:00 = 13 slots)
		currentNow = time.Date(2026, time.October, 12, 16, 5, 0, 0, lonLocation)
		futureDelayed, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		assert.Len(t, futureDelayed, 13)
		assert.Equal(t, int32(3), httpHits.Load())

		// 4. At 16:10 UK time (within 15-minute cooldown): returns cached slots without hitting API
		currentNow = time.Date(2026, time.October, 12, 16, 10, 0, 0, lonLocation)
		futureCooldown, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		assert.Len(t, futureCooldown, 13)
		assert.Equal(t, int32(3), httpHits.Load())

		// 5. At 16:25 UK time (after 15-minute cooldown, tomorrow published through 23:00 tomorrow):
		// Fetches from API and returns 16:30 today through 23:00 tomorrow (13 + 48 = 61 slots)
		publishTomorrow = true
		currentNow = time.Date(2026, time.October, 12, 16, 25, 0, 0, lonLocation)
		futureTomorrow, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		if assert.Len(t, futureTomorrow, 61) {
			assert.Equal(t, dayStart.Add(16*time.Hour+30*time.Minute), futureTomorrow[0].TSStart)
			assert.Equal(t, dayStart.AddDate(0, 0, 1).Add(23*time.Hour), futureTomorrow[len(futureTomorrow)-1].TSEnd)
		}
		assert.Equal(t, int32(4), httpHits.Load())

		// 6. At 16:28 UK time: all slots through 23:00 tomorrow are now in memory -> no HTTP call
		currentNow = time.Date(2026, time.October, 12, 16, 28, 0, 0, lonLocation)
		futureCachedTomorrow, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		assert.Len(t, futureCachedTomorrow, 61)
		assert.Equal(t, int32(4), httpHits.Load())
	})

	t.Run("Intelligent Octopus Go with Agile Outgoing via Map.Site", func(t *testing.T) {
		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)
		fixedNow := time.Date(2026, time.October, 12, 23, 15, 0, 0, lonLocation)

		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			// Verify Eastern England maps to tariff code E-1R-AGILE-OUTGOING-19-05-13-A
			assert.Equal(t, "/v1/products/AGILE-OUTGOING-19-05-13/electricity-tariffs/E-1R-AGILE-OUTGOING-19-05-13-A/standard-unit-rates/", r.URL.Path)

			var results []octopusUnitRateEntry
			// Slots 46 (23:00-23:30) and 47 (23:30-24:00)
			st46 := dayStart.Add(23 * time.Hour)
			st47 := dayStart.Add(23*time.Hour + 30*time.Minute)
			results = append(results,
				octopusUnitRateEntry{
					ValueExcVAT: 22.5,
					ValueIncVAT: 22.5, // £0.225/kWh export during 23:30-24:00
					ValidFrom:   st47.UTC(),
					ValidTo:     st47.Add(30 * time.Minute).UTC(),
				},
				octopusUnitRateEntry{
					ValueExcVAT: 18.0,
					ValueIncVAT: 18.0, // £0.18/kWh export during 23:00-23:30
					ValidFrom:   st46.UTC(),
					ValidTo:     st46.Add(30 * time.Minute).UTC(),
				},
			)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count:   len(results),
				Results: results,
			})
		})

		m := NewMap(nil)
		m.baseOctopus = &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return fixedNow },
		}

		ctx := context.Background()
		siteUtil, err := m.Site(ctx, "site-uk", types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "eastern",
				GenerationRate: "outgoing_agile",
			},
		})
		require.NoError(t, err)

		// Current price at 23:15 on 2026-10-12 (pre-2026-11-04): Intelligent Go Peak (£0.2896 in Eastern) + Agile Outgoing (£0.18)
		curr, err := siteUtil.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, 0.2896, curr.DollarsPerKWH, 1e-4)
		assert.InDelta(t, 0.18, curr.GenerationCreditDollarsPerKWH, 1e-4)
		assert.True(t, curr.SeparateGenerationCredit)

		// Future price at 23:30-24:00 on 2026-10-12 (pre-2026-11-04): Intelligent Go Off-Peak (£0.0700) + Agile Outgoing (£0.225)
		future, err := siteUtil.GetFuturePrices(ctx)
		require.NoError(t, err)
		if assert.Len(t, future, 1) {
			assert.Equal(t, dayStart.Add(23*time.Hour+30*time.Minute), future[0].TSStart)
			assert.InDelta(t, 0.0700, future[0].DollarsPerKWH, 1e-4)
			assert.InDelta(t, 0.225, future[0].GenerationCreditDollarsPerKWH, 1e-4)
			assert.True(t, future[0].SeparateGenerationCredit)
		}

		// Verify SiteFees.GetPeriods returns named Off-Peak and Peak TOU periods for Rate-Based Minimum SOC
		periods, err := siteUtil.GetPeriods(ctx)
		require.NoError(t, err)
		assert.Len(t, periods, 4)
		periodNames := make(map[string]bool)
		for _, p := range periods {
			periodNames[p.Name] = true
		}
		assert.True(t, periodNames["Off-Peak"])
		assert.True(t, periodNames["Peak"])
	})

	t.Run("All 14 DNO Regions and Tariff Codes", func(t *testing.T) {
		expectedCodes := map[string]string{
			"eastern":           "A",
			"east_midlands":     "B",
			"london":            "C",
			"merseyside_wales":  "D",
			"west_midlands":     "E",
			"north_east":        "F",
			"north_west":        "G",
			"southern":          "H",
			"south_east":        "J",
			"south_wales":       "K",
			"south_west":        "L",
			"yorkshire":         "M",
			"southern_scotland": "N",
			"northern_scotland": "P",
		}
		require.Len(t, octopusLocationChoices, 14)

		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)
		var lastPath string
		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			lastPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count: 1,
				Results: []octopusUnitRateEntry{
					{
						ValueExcVAT: 14.0,
						ValueIncVAT: 14.0,
						ValidFrom:   dayStart.UTC(),
						ValidTo:     dayStart.Add(30 * time.Minute).UTC(),
					},
				},
			})
		})

		base := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return dayStart },
		}

		for _, choice := range octopusLocationChoices {
			loc := choice.Value
			expectedLetter, ok := expectedCodes[loc]
			require.True(t, ok, "missing expected region code for %s", loc)
			cfg := octopusRates[loc]

			fluxFees, err := octopusFluxFees(types.UtilityRateOptions{Location: loc})
			require.NoError(t, err)
			assert.Len(t, fluxFees, 12)

			uFlux := &genericTOU{periods: fluxFees, name: "octopus_flux"}
			pFluxQ4Peak, err := uFlux.priceForTime(time.Date(2026, time.November, 15, 17, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.FluxImportPeakDollarsPerKWH, pFluxQ4Peak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, cfg.FluxExportPeakDollarsPerKWH, pFluxQ4Peak.GenerationCreditDollarsPerKWH, 1e-6)

			intelliFluxFees, err := octopusIntelligentFluxFees(types.UtilityRateOptions{Location: loc})
			require.NoError(t, err)
			assert.Len(t, intelliFluxFees, 4)

			uIntelliFlux := &genericTOU{periods: intelliFluxFees, name: "octopus_intelligent_flux"}
			pIFluxPeak, err := uIntelliFlux.priceForTime(time.Date(2026, time.November, 15, 17, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.IntelliFluxPeakDollarsPerKWH, pIFluxPeak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, cfg.IntelliFluxPeakDollarsPerKWH, pIFluxPeak.GenerationCreditDollarsPerKWH, 1e-6)
			pIFluxStd, err := uIntelliFlux.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.IntelliFluxStandardDollarsPerKWH, pIFluxStd.DollarsPerKWH, 1e-6)
			assert.InDelta(t, cfg.IntelliFluxStandardDollarsPerKWH, pIFluxStd.GenerationCreditDollarsPerKWH, 1e-6)

			goFees, err := octopusGoFees(types.UtilityRateOptions{Location: loc, GenerationRate: "outgoing_agile"})
			require.NoError(t, err)
			assert.NotEmpty(t, goFees)

			goFixedFees, err := octopusGoFees(types.UtilityRateOptions{RateClass: "fixed", Location: loc})
			require.NoError(t, err)
			uGoFixed := &genericTOU{periods: goFixedFees, name: "octopus_go"}
			pGoFixedPeak, err := uGoFixed.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.GoFixedPeakDollarsPerKWH, pGoFixedPeak.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.1200, pGoFixedPeak.GenerationCreditDollarsPerKWH, 1e-6)

			iogFlexFees, err := octopusIntelligentGoFees(types.UtilityRateOptions{RateClass: "flexible", Location: loc, GenerationRate: "outgoing_agile"})
			require.NoError(t, err)
			assert.Len(t, iogFlexFees, 4)
			uIOGFlex := &genericTOU{periods: iogFlexFees, name: "octopus_intelligent_go"}
			pIOGPrePeak, err := uIOGFlex.priceForTime(time.Date(2026, time.July, 15, 12, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.IOGPeakRate, pIOGPrePeak.DollarsPerKWH, 1e-6)
			pIOGPreOff, err := uIOGFlex.priceForTime(time.Date(2026, time.July, 15, 2, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.IOGOffPeakRate, pIOGPreOff.DollarsPerKWH, 1e-6)
			pIOGPostPeak, err := uIOGFlex.priceForTime(time.Date(2026, time.November, 15, 12, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, cfg.IOGFlexiblePeakDollarsPerKWH, pIOGPostPeak.DollarsPerKWH, 1e-6)
			pIOGPostOff, err := uIOGFlex.priceForTime(time.Date(2026, time.November, 15, 2, 0, 0, 0, lonLocation))
			require.NoError(t, err)
			assert.InDelta(t, octopusIOGFlexibleOffPeakDollarsPerKWH, pIOGPostOff.DollarsPerKWH, 1e-6)

			for _, rc := range []string{"max", "fixed", "legacy_24_10_29"} {
				iogFees, err := octopusIntelligentGoFees(types.UtilityRateOptions{RateClass: rc, Location: loc, GenerationRate: "outgoing_agile"})
				require.NoError(t, err)
				assert.Len(t, iogFees, 2)
			}

			cosyFees, err := octopusCosyFees(types.UtilityRateOptions{Location: loc, GenerationRate: "outgoing_agile"})
			require.NoError(t, err)
			assert.NotEmpty(t, cosyFees)

			cosyFixedFees, err := octopusCosyFees(types.UtilityRateOptions{RateClass: "fixed", Location: loc, GenerationRate: "outgoing_agile"})
			require.NoError(t, err)
			assert.NotEmpty(t, cosyFixedFees)

			reg, err := base.Region(loc)
			require.NoError(t, err)
			p, err := reg.GetCurrentPrice(context.Background())
			require.NoError(t, err)
			assert.InDelta(t, 0.14, p.GenerationCreditDollarsPerKWH, 1e-6)
			expectedPath := fmt.Sprintf("/v1/products/AGILE-OUTGOING-19-05-13/electricity-tariffs/E-1R-AGILE-OUTGOING-19-05-13-%s/standard-unit-rates/", expectedLetter)
			assert.Equal(t, expectedPath, lastPath)
		}
	})

	t.Run("Map.Site Dynamic Transitions and Flux Isolation", func(t *testing.T) {
		fixedNow := time.Date(2026, time.October, 12, 12, 0, 0, 0, lonLocation)
		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)

		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count: 1,
				Results: []octopusUnitRateEntry{
					{
						ValueExcVAT: 21.0,
						ValueIncVAT: 21.0,
						ValidFrom:   fixedNow.UTC(),
						ValidTo:     fixedNow.Add(30 * time.Minute).UTC(),
					},
				},
			})
		})

		m := NewMap(nil)
		m.baseOctopus = &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return fixedNow },
		}
		_ = dayStart

		ctx := context.Background()
		// 1. Start on Intelligent Go with fixed 15p export -> genericTOU
		u1, err := m.Site(ctx, "site-trans", types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_15p",
			},
		})
		require.NoError(t, err)
		_, isTOU := u1.(*genericTOU)
		assert.True(t, isTOU)

		// 2. Switch same site to outgoing_agile -> SiteFees wrapping london
		u2, err := m.Site(ctx, "site-trans", types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_agile",
			},
		})
		require.NoError(t, err)
		sf2, isSF := u2.(*SiteFees)
		require.True(t, isSF)
		assert.Equal(t, "london", sf2.base.(*baseOctopusRegion).location)

		// 3. Switch location to eastern and rate to octopus_cosy -> SiteFees wrapping eastern
		u3, err := m.Site(ctx, "site-trans", types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_cosy",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "eastern",
				GenerationRate: "outgoing_agile",
			},
		})
		require.NoError(t, err)
		sf3, isSF := u3.(*SiteFees)
		require.True(t, isSF)
		assert.Equal(t, "eastern", sf3.base.(*baseOctopusRegion).location)
		currCosy, err := u3.GetCurrentPrice(ctx)
		require.NoError(t, err)
		// 12:00 London time in Eastern is Cosy Standard (£0.2882/kWh) + Agile Outgoing (£0.21/kWh)
		assert.InDelta(t, 0.2882, currCosy.DollarsPerKWH, 1e-4)
		assert.Equal(t, "Standard", currCosy.PeriodName)
		assert.InDelta(t, 0.21, currCosy.GenerationCreditDollarsPerKWH, 1e-4)

		// 4. Switch rate to octopus_flux (even with stale outgoing_agile option) -> must use genericTOU, not SiteFees!
		fluxSettings := types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_agile",
			},
		}
		u4, err := m.Site(ctx, "site-trans", fluxSettings)
		require.NoError(t, err)
		_, isTOU4 := u4.(*genericTOU)
		assert.True(t, isTOU4, "octopus_flux must use genericTOU even when stale outgoing_agile is present")
		u4Preview, err := m.PreviewSite(ctx, fluxSettings)
		require.NoError(t, err)
		_, isTOU4Preview := u4Preview.(*genericTOU)
		assert.True(t, isTOU4Preview, "octopus_flux preview must use genericTOU even when stale outgoing_agile is present")

		// 5. Switch rate to octopus_intelligent_flux (even with stale outgoing_agile option) -> must use genericTOU!
		intelliFluxSettings := types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_flux",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_agile",
			},
		}
		u5, err := m.Site(ctx, "site-trans", intelliFluxSettings)
		require.NoError(t, err)
		_, isTOU5 := u5.(*genericTOU)
		assert.True(t, isTOU5, "octopus_intelligent_flux must use genericTOU even when stale outgoing_agile is present")
		u5Preview, err := m.PreviewSite(ctx, intelliFluxSettings)
		require.NoError(t, err)
		_, isTOU5Preview := u5Preview.(*genericTOU)
		assert.True(t, isTOU5Preview, "octopus_intelligent_flux preview must use genericTOU even when stale outgoing_agile is present")
	})

	t.Run("GetConfirmedPrices Pre-1600 Horizon Capping and Active Slot Inclusion", func(t *testing.T) {
		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)
		// 12:15 UK time (before 16:00 publication): syncEnd = now + 12h = 00:15 tomorrow (past 23:00 today)
		fixedNow := time.Date(2026, time.October, 12, 12, 15, 0, 0, lonLocation)

		var httpHits atomic.Int32
		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			httpHits.Add(1)
			// Octopus has published slots 0..45 (00:00 to 23:00 today)
			var results []octopusUnitRateEntry
			for slot := 45; slot >= 0; slot-- {
				st := dayStart.Add(time.Duration(slot) * 30 * time.Minute)
				results = append(results, octopusUnitRateEntry{
					ValueExcVAT: 16.0,
					ValueIncVAT: 16.0,
					ValidFrom:   st.UTC(),
					ValidTo:     st.Add(30 * time.Minute).UTC(),
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count:   len(results),
				Results: results,
			})
		})

		base := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return fixedNow },
		}

		ctx := context.Background()
		syncStart := fixedNow.Add(-1 * time.Hour) // 11:15 (mid-slot inside 11:00-11:30)
		syncEnd := fixedNow.Add(12 * time.Hour)   // 00:15 tomorrow

		// First call fetches from API and includes active 11:00-11:30 slot through 22:30-23:00 today (24 slots)
		confirmed1, err := base.GetConfirmedPrices(ctx, "london", syncStart, syncEnd)
		require.NoError(t, err)
		if assert.Len(t, confirmed1, 24) {
			assert.Equal(t, dayStart.Add(11*time.Hour), confirmed1[0].TSStart)
			assert.Equal(t, dayStart.Add(23*time.Hour), confirmed1[len(confirmed1)-1].TSEnd)
		}
		assert.Equal(t, int32(1), httpHits.Load())

		// Second call with end > 23:00 before 16:00 UK must hit memory cache without calling the API again
		confirmed2, err := base.GetConfirmedPrices(ctx, "london", syncStart, syncEnd)
		require.NoError(t, err)
		assert.Len(t, confirmed2, 24)
		assert.Equal(t, int32(1), httpHits.Load())

		// Calling GetFuturePrices on the same poll must also be satisfied from memory without calling the API
		future, err := base.GetFuturePrices(ctx, "london")
		require.NoError(t, err)
		assert.Len(t, future, 21) // 12:30 to 23:00 = 21 slots
		assert.Equal(t, int32(1), httpHits.Load())
	})

	t.Run("UK BST/GMT Daylight Saving Time Transitions", func(t *testing.T) {
		// UK Fall-back day: Sunday Oct 25, 2026 has 25 hours (50 half-hour slots)
		fallDayStart := time.Date(2026, time.October, 25, 0, 0, 0, 0, lonLocation)
		fallDayEnd := fallDayStart.AddDate(0, 0, 1)
		assert.Equal(t, 25*time.Hour, fallDayEnd.Sub(fallDayStart))

		var httpHits atomic.Int32
		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			httpHits.Add(1)
			var results []octopusUnitRateEntry
			for slot := 49; slot >= 0; slot-- {
				st := fallDayStart.Add(time.Duration(slot) * 30 * time.Minute)
				results = append(results, octopusUnitRateEntry{
					ValueExcVAT: float64(slot),
					ValueIncVAT: float64(slot),
					ValidFrom:   st.UTC(),
					ValidTo:     st.Add(30 * time.Minute).UTC(),
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(octopusUnitRatesResponse{
				Count:   len(results),
				Results: results,
			})
		})

		base := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          mockClient,
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return fallDayStart.Add(26 * time.Hour) },
		}

		confirmed, err := base.GetConfirmedPrices(context.Background(), "london", fallDayStart, fallDayEnd)
		require.NoError(t, err)
		assert.Len(t, confirmed, 50)
		assert.Equal(t, int32(1), httpHits.Load())

		// Second call verifies all 50 half-hour slots across the 25-hour DST day are cached in memory without collision
		confirmedCached, err := base.GetConfirmedPrices(context.Background(), "london", fallDayStart, fallDayEnd)
		require.NoError(t, err)
		assert.Len(t, confirmedCached, 50)
		assert.Equal(t, int32(1), httpHits.Load())
	})

	t.Run("Transient API Error Detection", func(t *testing.T) {
		dayStart := time.Date(2026, time.October, 12, 0, 0, 0, 0, lonLocation)
		dayEnd := dayStart.Add(24 * time.Hour)

		var statusCode int
		mockClient := newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(`{"detail":"upstream error"}`))
		})

		newTestBase := func() *baseOctopus {
			return &baseOctopus{
				apiURL:          defaultOctopusAPIURL,
				client:          mockClient,
				cachedPrices:    make(map[string]map[int64]types.Price),
				lastFutureFetch: make(map[string]time.Time),
				nowFunc:         func() time.Time { return dayEnd },
			}
		}

		statusCode = http.StatusServiceUnavailable
		_, err503 := newTestBase().GetConfirmedPrices(context.Background(), "london", dayStart, dayEnd)
		assert.ErrorContains(t, err503, "octopus api error status 503")
		assert.True(t, isTransientOctopusAPIError(err503))

		statusCode = http.StatusTooManyRequests
		_, err429 := newTestBase().GetConfirmedPrices(context.Background(), "london", dayStart, dayEnd)
		assert.ErrorContains(t, err429, "octopus api error status 429")
		assert.True(t, isTransientOctopusAPIError(err429))

		statusCode = http.StatusNotFound
		_, err404 := newTestBase().GetConfirmedPrices(context.Background(), "london", dayStart, dayEnd)
		assert.ErrorContains(t, err404, "octopus api error status 404")
		assert.False(t, isTransientOctopusAPIError(err404))

		assert.True(t, isTransientOctopusAPIError(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)))
		assert.False(t, isTransientOctopusAPIError(nil))
	})

	t.Run("Integration Real API", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping live Octopus API integration test in short mode")
		}
		targetNow := time.Date(2026, time.April, 10, 12, 15, 0, 0, lonLocation)
		dayStart := time.Date(2026, time.April, 10, 0, 0, 0, 0, lonLocation)
		dayEnd := dayStart.AddDate(0, 0, 1)

		base := &baseOctopus{
			apiURL:          defaultOctopusAPIURL,
			client:          common.HTTPClient(15 * time.Second),
			cachedPrices:    make(map[string]map[int64]types.Price),
			lastFutureFetch: make(map[string]time.Time),
			nowFunc:         func() time.Time { return targetNow },
		}

		var confirmed []types.Price
		var err error
		for attempt := range 3 {
			attemptCtx, attemptCancel := context.WithTimeout(context.Background(), 15*time.Second)
			confirmed, err = base.GetConfirmedPrices(attemptCtx, "london", dayStart, dayEnd)
			attemptCancel()
			if err == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(1 * time.Second)
			}
		}
		if isTransientOctopusAPIError(err) {
			t.Skipf("octopus API transiently unavailable: %v", err)
		}
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if assert.Len(t, confirmed, 48) {
			assert.Equal(t, dayStart, confirmed[0].TSStart)
			assert.Equal(t, dayEnd, confirmed[len(confirmed)-1].TSEnd)
			hasNonZero := false
			for i, p := range confirmed {
				expectedStart := dayStart.Add(time.Duration(i) * 30 * time.Minute)
				assert.Equal(t, expectedStart, p.TSStart)
				assert.Equal(t, expectedStart.Add(30*time.Minute), p.TSEnd)
				assert.Equal(t, "octopus", p.Provider)
				assert.True(t, p.SeparateGenerationCredit)
				if p.GenerationCreditDollarsPerKWH != 0 {
					hasNonZero = true
				}
				// Sanity check unit conversion: Agile Outgoing rates in £/kWh are typically between -£0.20 and £1.00/kWh (not 10-50 pence raw)
				assert.Greater(t, p.GenerationCreditDollarsPerKWH, -0.50)
				assert.Less(t, p.GenerationCreditDollarsPerKWH, 2.00)
			}
			assert.True(t, hasNonZero, "expected non-zero export credits across day")
		}

		// Verify end-to-end SiteFees layering with Intelligent Octopus Go + Agile Outgoing against the real API data
		m := NewMap(nil)
		m.baseOctopus = base
		siteUtil, err := m.Site(ctx, "site-live-uk", types.Settings{
			UtilityProvider: "octopus",
			UtilityRate:     "octopus_intelligent_go",
			UtilityRateOptions: types.UtilityRateOptions{
				Location:       "london",
				GenerationRate: "outgoing_agile",
			},
		})
		require.NoError(t, err)

		curr, err := siteUtil.GetCurrentPrice(ctx)
		require.NoError(t, err)
		// 12:15 London time on 2026-04-10 (pre-2026-11-04) is Intelligent Octopus Go Peak (£0.2802/kWh)
		assert.InDelta(t, 0.2802, curr.DollarsPerKWH, 1e-4)
		assert.Equal(t, "Peak", curr.PeriodName)
		assert.True(t, curr.SeparateGenerationCredit)
		assert.NotZero(t, curr.GenerationCreditDollarsPerKWH)
	})
}
