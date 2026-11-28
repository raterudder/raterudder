package utility

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngieUtility(t *testing.T) {
	t.Run("Solar Elec Single Rate - Ausgrid", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Flat $0.4011 import rate and $0.03 export rate year-round
		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4011, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		assert.Empty(t, periods)
	})

	t.Run("Solar Elec Single Rate - Endeavour", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4048, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3711, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - Powercor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "powercor",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3009, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - United Energy", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2883, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec Single Rate - Citipower Fallback", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "citipower",
			},
		})
		require.NoError(t, err)

		p, err := u.priceForTime(time.Date(2026, time.January, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2883, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Ausgrid Summer", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		periods, err := u.GetPeriods(context.Background())
		require.NoError(t, err)
		if assert.NotEmpty(t, periods) {
			assert.Equal(t, "On-Peak", periods[0].Name)
		}

		// Summer Peak Season (Jan 15, 2026)
		summerDay := time.Date(2026, time.January, 15, 0, 0, 0, 0, sydLocation)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM -> $0.4288, export $0.0300
		p, err := u.priceForTime(summerDay.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4288, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.3927
		p, err = u.priceForTime(summerDay.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3927, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Ausgrid Non-Summer Non-Winter", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Non-Summer Non-Winter (April 15, 2026)
		autumnDay := time.Date(2026, time.April, 15, 0, 0, 0, 0, sydLocation)

		// Should be flat $0.3927 at all times
		p, err := u.priceForTime(autumnDay.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3927, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Ausgrid Winter Peak", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "ausgrid",
			},
		})
		require.NoError(t, err)

		// Winter Peak Season (July 15, 2026)
		winterDay := time.Date(2026, time.July, 15, 0, 0, 0, 0, sydLocation)

		// Peak (3:00 PM - 8:59 PM): 4:00 PM -> $0.4288, export $0.0300
		p, err := u.priceForTime(winterDay.Add(16 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4288, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Endeavour Summer", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, Jan 14, 2026
		weekday := time.Date(2026, time.January, 14, 0, 0, 0, 0, sydLocation)

		// Peak weekdays 4 PM - 8 PM: 5:00 PM -> $0.6564, export $0.0300
		p, err := u.priceForTime(weekday.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.6564, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak weekday: 10:00 AM -> $0.3838
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}

		// Weekend: Saturday, Jan 17, 2026 - Off-Peak all day
		weekend := time.Date(2026, time.January, 17, 17, 0, 0, 0, sydLocation)
		p, err = u.priceForTime(weekend)
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Endeavour Winter", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "endeavour",
			},
		})
		require.NoError(t, err)

		// Weekday: Wednesday, July 15, 2026
		weekday := time.Date(2026, time.July, 15, 0, 0, 0, 0, sydLocation)

		// Peak weekdays 4 PM - 8 PM: 5:00 PM -> $0.4204, export $0.0300
		p, err := u.priceForTime(weekday.Add(17 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4204, p.DollarsPerKWH, 1e-6)
		}

		// Off-Peak weekday: 10:00 AM -> $0.3838
		p, err = u.priceForTime(weekday.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Energex", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "energex",
			},
		})
		require.NoError(t, err)

		// Everyday: Jan 15, 2026
		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, bneLocation)

		// Peak 4 PM - 9 PM: 6:00 PM -> $0.5613, export $0.0100
		p, err := u.priceForTime(day.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.5613, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak 11 AM - 4 PM: 12:00 PM -> $0.2621
		p, err = u.priceForTime(day.Add(12 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2621, p.DollarsPerKWH, 1e-6)
		}

		// Shoulder all other times: 8:00 AM -> $0.3059
		p, err = u.priceForTime(day.Add(8 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3059, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - Powercor", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "powercor",
			},
		})
		require.NoError(t, err)

		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, melLocation)

		// Peak 3 PM - 9 PM: 6:00 PM -> $0.4039, export $0.0100
		p, err := u.priceForTime(day.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4039, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.2365
		p, err = u.priceForTime(day.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2365, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("Solar Elec TOU Rate - United Energy", func(t *testing.T) {
		u := &genericTOU{}
		err := u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider: "engie",
			UtilityRate:     "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "united_energy",
			},
		})
		require.NoError(t, err)

		day := time.Date(2026, time.January, 15, 0, 0, 0, 0, melLocation)

		// Peak 3 PM - 9 PM: 6:00 PM -> $0.3837, export $0.0100
		p, err := u.priceForTime(day.Add(18 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3837, p.DollarsPerKWH, 1e-6)
			assert.True(t, p.SeparateGenerationCredit)
			assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Off-Peak: 10:00 AM -> $0.2299
		p, err = u.priceForTime(day.Add(10 * time.Hour))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2299, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("2026-2027 Single Rate - NSW and QLD October 27 Transition", func(t *testing.T) {
		// Ausgrid: $0.4011 before 2026-10-27 00:00, $0.33132 from 2026-10-27 through 2027
		uAusgrid := &genericTOU{}
		require.NoError(t, uAusgrid.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausgrid"},
		}))

		pBefore, err := uAusgrid.priceForTime(time.Date(2026, time.October, 26, 23, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4011, pBefore.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pBefore.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pAfter, err := uAusgrid.priceForTime(time.Date(2026, time.October, 27, 0, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.33132, pAfter.DollarsPerKWH, 1e-6)
			assert.True(t, pAfter.SeparateGenerationCredit)
			assert.InDelta(t, 0.0300, pAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p2027, err := uAusgrid.priceForTime(time.Date(2027, time.June, 15, 12, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.33132, p2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, p2027.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Endeavour: $0.4048 before 2026-10-27 00:00, $0.33726 from 2026-10-27 through 2027
		uEndeavour := &genericTOU{}
		require.NoError(t, uEndeavour.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "endeavour"},
		}))
		pEndBefore, err := uEndeavour.priceForTime(time.Date(2026, time.October, 26, 23, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.4048, pEndBefore.DollarsPerKWH, 1e-6)
		}
		pEndAfter, err := uEndeavour.priceForTime(time.Date(2026, time.October, 27, 0, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.33726, pEndAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pEndAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pEnd2027, err := uEndeavour.priceForTime(time.Date(2027, time.November, 10, 14, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.33726, pEnd2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0300, pEnd2027.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Energex: $0.3711 before 2026-10-27 00:00, $0.27973 from 2026-10-27 through 2027
		uEnergex := &genericTOU{}
		require.NoError(t, uEnergex.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "energex"},
		}))
		pEnBefore, err := uEnergex.priceForTime(time.Date(2026, time.October, 26, 23, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3711, pEnBefore.DollarsPerKWH, 1e-6)
		}
		pEnAfter, err := uEnergex.priceForTime(time.Date(2026, time.October, 27, 0, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.27973, pEnAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0100, pEnAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pEn2027, err := uEnergex.priceForTime(time.Date(2027, time.August, 20, 9, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.27973, pEn2027.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0100, pEn2027.GenerationCreditDollarsPerKWH, 1e-6)
		}
	})

	t.Run("2026-2027 Single Rate - Victoria July 1 Transition and Distinct Distributors", func(t *testing.T) {
		// Powercor: $0.3009 before 2026-07-01 00:00, $0.28215 from 2026-07-01 through 2027
		uPowercor := &genericTOU{}
		require.NoError(t, uPowercor.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "powercor"},
		}))
		pBefore, err := uPowercor.priceForTime(time.Date(2026, time.June, 30, 23, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3009, pBefore.DollarsPerKWH, 1e-6)
		}
		pAfter, err := uPowercor.priceForTime(time.Date(2026, time.July, 1, 0, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.28215, pAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0100, pAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		p2027, err := uPowercor.priceForTime(time.Date(2027, time.March, 15, 12, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.28215, p2027.DollarsPerKWH, 1e-6)
		}

		// United Energy: $0.2883 before 2026-07-01 00:00, $0.27346 from 2026-07-01 through 2027
		uUnited := &genericTOU{}
		require.NoError(t, uUnited.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "united_energy"},
		}))
		pUEAfter, err := uUnited.priceForTime(time.Date(2026, time.July, 1, 0, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.27346, pUEAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0100, pUEAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pUE2027, err := uUnited.priceForTime(time.Date(2027, time.September, 10, 18, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.27346, pUE2027.DollarsPerKWH, 1e-6)
		}

		// CitiPower: $0.2883 fallback before 2026-07-01 00:00, distinct $0.25960 from 2026-07-01 through 2027
		uCitipower := &genericTOU{}
		require.NoError(t, uCitipower.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "citipower"},
		}))
		pCPBefore, err := uCitipower.priceForTime(time.Date(2026, time.June, 30, 23, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2883, pCPBefore.DollarsPerKWH, 1e-6)
		}
		pCPAfter, err := uCitipower.priceForTime(time.Date(2026, time.July, 1, 0, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.25960, pCPAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0100, pCPAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pCP2027, err := uCitipower.priceForTime(time.Date(2027, time.December, 1, 15, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.25960, pCP2027.DollarsPerKWH, 1e-6)
		}

		// Jemena: $0.2883 fallback before 2026-07-01 00:00, distinct $0.27467 from 2026-07-01 through 2027
		uJemena := &genericTOU{}
		require.NoError(t, uJemena.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_single",
			UtilityRateOptions: types.UtilityRateOptions{Location: "jemena"},
		}))
		pJMBefore, err := uJemena.priceForTime(time.Date(2026, time.June, 30, 23, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2883, pJMBefore.DollarsPerKWH, 1e-6)
		}
		pJMAfter, err := uJemena.priceForTime(time.Date(2026, time.July, 1, 0, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.27467, pJMAfter.DollarsPerKWH, 1e-6)
			assert.InDelta(t, 0.0100, pJMAfter.GenerationCreditDollarsPerKWH, 1e-6)
		}
		pJM2027, err := uJemena.priceForTime(time.Date(2027, time.May, 20, 8, 0, 0, 0, melLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.27467, pJM2027.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("2026-2027 TOU Rate - Ausgrid EA025 Season Boundaries", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{Location: "ausgrid"},
		}))

		// Pre-cutoff Oct 26, 2026 at 16:00 -> legacy Spring Off-Peak $0.3927
		p, err := u.priceForTime(time.Date(2026, time.October, 26, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3927, p.DollarsPerKWH, 1e-6)
		}

		// Oct 27-31, 2026 maps to Spring Off-Peak ($0.24035 all hours, including 16:00 on Oct 31)
		for _, day := range []int{27, 31} {
			p, err = u.priceForTime(time.Date(2026, time.October, day, 16, 0, 0, 0, sydLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.24035, p.DollarsPerKWH, 1e-6)
				assert.Equal(t, "Off-Peak", p.PeriodName)
				assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
		}

		// Nov 1, 2026 starts Summer: 15:00-21:00 Peak $0.60192, other hours Off-Peak $0.24035
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 15, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.60192, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 21, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24035, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// 2027 season boundary checks at 16:00:
		// Mar 31, 2027 (last day of Summer) -> Peak $0.60192; Apr 1, 2027 (start of Autumn) -> Off-Peak $0.24035
		p, err = u.priceForTime(time.Date(2027, time.March, 31, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.60192, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.April, 1, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24035, p.DollarsPerKWH, 1e-6)
		}

		// May 31, 2027 (last day of Autumn) -> Off-Peak $0.24035; Jun 1, 2027 (start of Winter) -> Peak $0.60192
		p, err = u.priceForTime(time.Date(2027, time.May, 31, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24035, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.June, 1, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.60192, p.DollarsPerKWH, 1e-6)
		}

		// Aug 31, 2027 (last day of Winter) -> Peak $0.60192; Sep 1, 2027 (start of Spring) -> Off-Peak $0.24035
		p, err = u.priceForTime(time.Date(2027, time.August, 31, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.60192, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.September, 1, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24035, p.DollarsPerKWH, 1e-6)
		}

		// Oct 31, 2027 (last day of Spring) -> Off-Peak $0.24035; Nov 1, 2027 (start of Summer) -> Peak $0.60192
		p, err = u.priceForTime(time.Date(2027, time.October, 31, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.24035, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.November, 1, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.60192, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("2026-2027 TOU Rate - Endeavour N71 Sponge and Season Boundaries", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{Location: "endeavour"},
		}))

		// Pre-cutoff Monday Oct 26, 2026: 11:00 -> legacy Off-Peak $0.3838 (no sponge before Oct 27)
		p, err := u.priceForTime(time.Date(2026, time.October, 26, 11, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.3838, p.DollarsPerKWH, 1e-6)
		}

		// Oct 27-30, 2026 (Tue-Fri) maps to Winter Weekday Peak ($0.46563) at 16:00-20:00 before Nov 1 Summer
		p, err = u.priceForTime(time.Date(2026, time.October, 30, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.46563, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
			assert.InDelta(t, 0.0300, p.GenerationCreditDollarsPerKWH, 1e-6)
		}

		// Daily Sponge 10:00-14:00 on Weekday (Fri Oct 30, 2026) and Weekend (Sat Oct 31, 2026) -> $0.12342
		p, err = u.priceForTime(time.Date(2026, time.October, 30, 10, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12342, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
		}
		p, err = u.priceForTime(time.Date(2026, time.October, 31, 13, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.12342, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Super Off-Peak", p.PeriodName)
		}

		// Split Off-Peak boundaries around Sponge and Peak on Weekday (Mon Nov 2, 2026 - Summer):
		// 09:00 -> $0.35684, 14:00 -> $0.35684, 16:00 -> Summer Peak $0.47135, 20:00 -> $0.35684
		for _, hr := range []int{9, 14, 20} {
			p, err = u.priceForTime(time.Date(2026, time.November, 2, hr, 0, 0, 0, sydLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.35684, p.DollarsPerKWH, 1e-6)
				assert.Equal(t, "Off-Peak", p.PeriodName)
			}
		}
		p, err = u.priceForTime(time.Date(2026, time.November, 2, 16, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.47135, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "On-Peak", p.PeriodName)
		}

		// Weekend Off-Peak outside Sponge (Sun Nov 1, 2026 at 17:00) -> $0.35684
		p, err = u.priceForTime(time.Date(2026, time.November, 1, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.35684, p.DollarsPerKWH, 1e-6)
			assert.Equal(t, "Off-Peak", p.PeriodName)
		}

		// 2027 Summer -> Winter transition: Wed Mar 31, 2027 ($0.47135) vs Thu Apr 1, 2027 ($0.46563)
		p, err = u.priceForTime(time.Date(2027, time.March, 31, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.47135, p.DollarsPerKWH, 1e-6)
		}
		p, err = u.priceForTime(time.Date(2027, time.April, 1, 17, 0, 0, 0, sydLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.46563, p.DollarsPerKWH, 1e-6)
		}
	})

	t.Run("2026-2027 TOU Rate - Energex 6900", func(t *testing.T) {
		u := &genericTOU{}
		require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
			UtilityProvider:    "engie",
			UtilityRate:        "engie_solar_elec_tou",
			UtilityRateOptions: types.UtilityRateOptions{Location: "energex"},
		}))

		// Pre-cutoff Oct 26, 2026 at 12:00 -> $0.2621
		p, err := u.priceForTime(time.Date(2026, time.October, 26, 12, 0, 0, 0, bneLocation))
		if assert.NoError(t, err) {
			assert.InDelta(t, 0.2621, p.DollarsPerKWH, 1e-6)
		}

		// Post-cutoff in late 2026 and 2027: Peak 16:00-21:00 $0.47784, Off-Peak 11:00-16:00 $0.06974, Shoulder 21:00-11:00 $0.25289
		for _, day := range []time.Time{
			time.Date(2026, time.October, 27, 0, 0, 0, 0, bneLocation),
			time.Date(2027, time.July, 10, 0, 0, 0, 0, bneLocation),
		} {
			p, err = u.priceForTime(day.Add(18 * time.Hour))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.47784, p.DollarsPerKWH, 1e-6)
				assert.Equal(t, "On-Peak", p.PeriodName)
				assert.InDelta(t, 0.0100, p.GenerationCreditDollarsPerKWH, 1e-6)
			}
			p, err = u.priceForTime(day.Add(12 * time.Hour))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.06974, p.DollarsPerKWH, 1e-6)
				assert.Equal(t, "Off-Peak", p.PeriodName)
			}
			p, err = u.priceForTime(day.Add(22 * time.Hour))
			if assert.NoError(t, err) {
				assert.InDelta(t, 0.25289, p.DollarsPerKWH, 1e-6)
				assert.Equal(t, "Mid-Peak", p.PeriodName)
			}
		}
	})

	t.Run("2026-2027 TOU Rate - Victoria Three-Period Saver Schedule", func(t *testing.T) {
		assertVicTOU := func(t *testing.T, loc string, prePeakDollarsPerKWH, peakDollarsPerKWH, saverDollarsPerKWH, offPeakDollarsPerKWH float64) {
			t.Helper()
			u := &genericTOU{}
			require.NoError(t, u.ApplySettings(context.Background(), types.Settings{
				UtilityProvider:    "engie",
				UtilityRate:        "engie_solar_elec_tou",
				UtilityRateOptions: types.UtilityRateOptions{Location: loc},
			}))

			// Pre-cutoff June 30, 2026 at 15:00 -> legacy 15:00-21:00 Peak
			pPre, err := u.priceForTime(time.Date(2026, time.June, 30, 15, 0, 0, 0, melLocation))
			if assert.NoError(t, err) {
				assert.InDelta(t, prePeakDollarsPerKWH, pPre.DollarsPerKWH, 1e-6)
			}

			// From July 1, 2026 through 2027: 11:00-16:00 Saver (including 15:00!), 16:00-21:00 Peak, 21:00-11:00 Off-Peak
			for _, day := range []time.Time{
				time.Date(2026, time.July, 1, 0, 0, 0, 0, melLocation),
				time.Date(2027, time.February, 15, 0, 0, 0, 0, melLocation),
			} {
				pSaver, err := u.priceForTime(day.Add(15 * time.Hour))
				if assert.NoError(t, err) {
					assert.InDelta(t, saverDollarsPerKWH, pSaver.DollarsPerKWH, 1e-6)
					assert.Equal(t, "Super Off-Peak", pSaver.PeriodName)
					assert.True(t, pSaver.SeparateGenerationCredit)
					assert.InDelta(t, 0.0100, pSaver.GenerationCreditDollarsPerKWH, 1e-6)
				}
				pPeak, err := u.priceForTime(day.Add(16 * time.Hour))
				if assert.NoError(t, err) {
					assert.InDelta(t, peakDollarsPerKWH, pPeak.DollarsPerKWH, 1e-6)
					assert.Equal(t, "On-Peak", pPeak.PeriodName)
				}
				pOffPeakMorning, err := u.priceForTime(day.Add(10 * time.Hour))
				if assert.NoError(t, err) {
					assert.InDelta(t, offPeakDollarsPerKWH, pOffPeakMorning.DollarsPerKWH, 1e-6)
					assert.Equal(t, "Off-Peak", pOffPeakMorning.PeriodName)
				}
				pOffPeakNight, err := u.priceForTime(day.Add(21 * time.Hour))
				if assert.NoError(t, err) {
					assert.InDelta(t, offPeakDollarsPerKWH, pOffPeakNight.DollarsPerKWH, 1e-6)
					assert.Equal(t, "Off-Peak", pOffPeakNight.PeriodName)
				}
			}
		}

		t.Run("powercor", func(t *testing.T) {
			assertVicTOU(t, "powercor", 0.4039, 0.41668, 0.17259, 0.22484)
		})
		t.Run("united_energy", func(t *testing.T) {
			assertVicTOU(t, "united_energy", 0.3837, 0.40315, 0.17259, 0.22132)
		})
		t.Run("citipower", func(t *testing.T) {
			assertVicTOU(t, "citipower", 0.3837, 0.38302, 0.16588, 0.21164)
		})
		t.Run("jemena", func(t *testing.T) {
			assertVicTOU(t, "jemena", 0.3837, 0.37565, 0.17644, 0.21758)
		})
	})

	t.Run("All Distributors Cover Every Hour of 2026-2027 Without Gaps or Overlaps", func(t *testing.T) {
		info := engieUtilityInfo()
		locations := []string{"ausgrid", "endeavour", "energex", "powercor", "united_energy", "citipower", "jemena"}

		for _, rate := range info.Rates {
			for _, loc := range locations {
				t.Run(rate.ID+"_"+loc, func(t *testing.T) {
					periods, err := rate.GetFees(types.UtilityRateOptions{Location: loc})
					require.NoError(t, err)
					require.NotEmpty(t, periods)

					tz := periods[0].LocationPtr
					require.NotNil(t, tz)
					start := time.Date(2026, time.January, 1, 0, 0, 0, 0, tz)
					end := time.Date(2028, time.January, 1, 0, 0, 0, 0, tz)

					for ts := start; ts.Before(end); ts = ts.Add(time.Hour) {
						baseMatches := 0
						solarMatches := 0
						namedMatches := 0
						for i := range periods {
							ok, _, err := periods[i].Contains(ts)
							require.NoError(t, err)
							if !ok {
								continue
							}
							if periods[i].SeparateGenerationCredit {
								solarMatches++
							} else {
								baseMatches++
								if periods[i].Name != "" {
									namedMatches++
								}
							}
						}
						require.Equal(t, 1, baseMatches, "expected 1 base period at %v for %s/%s", ts, rate.ID, loc)
						require.Equal(t, 1, solarMatches, "expected 1 solar period at %v for %s/%s", ts, rate.ID, loc)
						if rate.ID == "engie_solar_elec_tou" {
							require.Equal(t, 1, namedMatches, "expected 1 named TOU period at %v for %s/%s", ts, rate.ID, loc)
						}
					}
				})
			}
		}
	})
}
