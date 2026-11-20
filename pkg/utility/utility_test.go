package utility

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestListUtilities(t *testing.T) {
	m := NewMap(nil)
	utilities := m.ListUtilities()

	require.NotEmpty(t, utilities, "expected at least one utility")

	ids := make(map[string]bool)
	for _, u := range utilities {
		assert.NotEmpty(t, u.ID, "utility must have an ID")
		assert.NotEmpty(t, u.Name, "utility must have a name")
		assert.False(t, ids[u.ID], "utility IDs must be unique, duplicate: %s", u.ID)
		ids[u.ID] = true

		for _, rate := range u.Rates {
			assert.NotEmpty(t, rate.ID, "rate must have an ID")
			assert.NotEmpty(t, rate.Name, "rate must have a name")

			for _, opt := range rate.Options {
				assert.NotEmpty(t, opt.Field, "option must have a Field")
				assert.NotEmpty(t, opt.Name, "option must have a name")
				assert.True(t, opt.Type == types.UtilityOptionTypeSelect || opt.Type == types.UtilityOptionTypeSwitch,
					"option type must be 'select' or 'switch', got %q", opt.Type)

				if opt.Type == types.UtilityOptionTypeSelect {
					assert.NotEmpty(t, opt.Choices, "select option %q must have choices", opt.Field)
					for _, c := range opt.Choices {
						assert.NotEmpty(t, c.Value, "choice must have a Value in option %q", opt.Field)
						assert.NotEmpty(t, c.Name, "choice must have a name in option %q", opt.Field)
					}
					assert.NotNil(t, opt.Default, "select option %q must have a default", opt.Field)
				}
			}
		}
	}

	t.Run("TariffAuditRegistryCoversAllUtilities", func(t *testing.T) {
		raw, err := os.ReadFile("TARIFF_AUDIT.md")
		require.NoError(t, err, "expected pkg/utility/TARIFF_AUDIT.md to exist")

		const expectedHeader = "| Provider ID | Utility Name | Source File | Rate Plans | Revision Cadence | Repo Effective Horizon | Next Audit Due | Last Verified | Official Source URL(s) | Notes |"
		lines := strings.Split(string(raw), "\n")

		headerIdx := -1
		for i, line := range lines {
			if strings.TrimSpace(line) == expectedHeader {
				headerIdx = i
				break
			}
		}
		require.NotEqual(t, -1, headerIdx, "TARIFF_AUDIT.md must contain exact registry table header %q", expectedHeader)
		require.Less(t, headerIdx+2, len(lines), "TARIFF_AUDIT.md registry table must have separator and data rows")

		documentedRows := make(map[string][]string)
		for _, line := range lines[headerIdx+2:] {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "|") {
				break
			}
			parts := strings.Split(strings.Trim(trimmed, "|"), "|")
			require.Len(t, parts, 10, "TARIFF_AUDIT.md table row must have 10 columns: %s", trimmed)

			providerID := strings.Trim(strings.TrimSpace(parts[0]), "`")
			require.NotEmpty(t, providerID, "Provider ID must not be empty in row: %s", trimmed)
			assert.NotContains(t, documentedRows, providerID, "duplicate Provider ID %q in TARIFF_AUDIT.md", providerID)
			documentedRows[providerID] = parts

			for colIdx, colName := range []string{
				"Provider ID",
				"Utility Name",
				"Source File",
				"Rate Plans",
				"Revision Cadence",
				"Repo Effective Horizon",
				"Next Audit Due",
				"Last Verified",
				"Official Source URL(s)",
				"Notes",
			} {
				assert.NotEmpty(t, strings.TrimSpace(parts[colIdx]), "column %q must not be empty for provider %q", colName, providerID)
			}

			for _, srcFile := range strings.Split(parts[2], ",") {
				cleanFile := strings.Trim(strings.TrimSpace(srcFile), "`")
				assert.Equal(t, filepath.Base(cleanFile), cleanFile, "Source File %q for provider %q must be a base filename in pkg/utility", cleanFile, providerID)
				assert.True(t, strings.HasSuffix(cleanFile, ".go") && !strings.HasSuffix(cleanFile, "_test.go"), "Source File %q for provider %q must be a non-test .go file", cleanFile, providerID)
				info, err := os.Stat(cleanFile)
				if assert.NoError(t, err, "Source File %q for provider %q must exist in pkg/utility", cleanFile, providerID) {
					assert.False(t, info.IsDir(), "Source File %q for provider %q must be a regular file, not a directory", cleanFile, providerID)
				}
			}

			nextAuditDue := strings.Trim(strings.TrimSpace(parts[6]), "`")
			_, err := time.Parse("2006-01-02", nextAuditDue)
			assert.NoError(t, err, "Next Audit Due for %q must be YYYY-MM-DD, got %q", providerID, nextAuditDue)

			lastVerified := strings.Trim(strings.TrimSpace(parts[7]), "`")
			if lastVerified != "unverified" {
				_, err := time.Parse("2006-01-02", lastVerified)
				assert.NoError(t, err, "Last Verified for %q must be 'unverified' or YYYY-MM-DD, got %q", providerID, lastVerified)
			}

			assert.Contains(t, parts[8], "https://", "Official Source URL(s) for %q must contain an https:// URL", providerID)
		}

		visibleUtilities := make(map[string]bool)
		for _, u := range utilities {
			if !u.Hidden && u.ID != "tou_example" {
				visibleUtilities[u.ID] = true
				row, ok := documentedRows[u.ID]
				if assert.True(t, ok, "visible utility %q (%s) must have a row in pkg/utility/TARIFF_AUDIT.md", u.ID, u.Name) {
					documentedPlans := make(map[string]bool)
					for _, rawPlan := range strings.Split(row[3], ",") {
						planID := strings.Trim(strings.TrimSpace(rawPlan), "`")
						if assert.NotEmpty(t, planID, "Rate Plans column for %q in TARIFF_AUDIT.md must not contain an empty plan ID", u.ID) {
							assert.False(t, documentedPlans[planID], "Rate Plans column for %q in TARIFF_AUDIT.md has duplicate plan ID %q", u.ID, planID)
							documentedPlans[planID] = true
						}
					}

					expectedPlans := make(map[string]bool, len(u.Rates))
					for _, r := range u.Rates {
						expectedPlans[r.ID] = true
						assert.True(t, documentedPlans[r.ID], "Rate Plans column for %q in TARIFF_AUDIT.md must list rate %q", u.ID, r.ID)
					}

					for planID := range documentedPlans {
						assert.True(t, expectedPlans[planID], "Rate Plans column for %q in TARIFF_AUDIT.md lists unknown rate %q", u.ID, planID)
					}
				}
			}
		}

		for docID := range documentedRows {
			assert.True(t, visibleUtilities[docID], "TARIFF_AUDIT.md documents unknown or hidden provider ID %q", docID)
		}
	})
}

type mockUtility struct {
	settingsErr error
}

func (m *mockUtility) Name() string {
	return "mock"
}

func (m *mockUtility) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return types.Price{}, nil
}

func (m *mockUtility) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	return nil, nil
}

func (m *mockUtility) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	return nil, nil
}

func (m *mockUtility) ApplySettings(ctx context.Context, settings types.Settings) error {
	return m.settingsErr
}

func (m *mockUtility) GetVPPInfo(context.Context) (types.UtilityVPPInfo, error) {
	return types.UtilityVPPInfo{}, nil
}

func (m *mockUtility) GetPeriods(context.Context) ([]types.TimePeriod, error) {
	return nil, nil
}

func TestMap(t *testing.T) {
	ctx := context.Background()

	t.Run("NewMap", func(t *testing.T) {
		m := NewMap(nil)
		assert.NotNil(t, m)
		assert.NotNil(t, m.utilities)
		assert.Nil(t, m.baseComEdHourly)
		assert.Nil(t, m.baseAmerenSmart)
	})

	t.Run("Configured", func(t *testing.T) {
		m := Configured(nil)
		assert.NotNil(t, m)
		assert.NotNil(t, m.baseComEdHourly)
		assert.NotNil(t, m.baseAmerenSmart)
	})

	t.Run("SetProvider", func(t *testing.T) {
		m := NewMap(nil)
		provider := &mockUtility{}
		m.SetProvider("site1", provider)
		assert.Equal(t, provider, m.utilities["site1"])
	})

	t.Run("Site with custom provider", func(t *testing.T) {
		m := NewMap(nil)
		provider := &mockUtility{}
		m.SetProvider("site1", provider)

		settings := types.Settings{UtilityRate: "mock"}
		u, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.Equal(t, provider, u)
	})

	t.Run("Site with unknown provider", func(t *testing.T) {
		m := NewMap(nil)
		settings := types.Settings{UtilityProvider: "unknown_provider"}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "unknown utility provider: unknown_provider")
	})

	t.Run("Site with ComEd provider", func(t *testing.T) {
		m := Configured(nil)
		settings := types.Settings{UtilityProvider: "comed", UtilityRate: "comed_besh"}
		u, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.NotNil(t, u)

		// Second call should return the cached instance
		u2, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.Equal(t, u, u2)
	})

	t.Run("Site with ComEd missing base", func(t *testing.T) {
		m := NewMap(nil) // ComEd base not configured
		settings := types.Settings{UtilityProvider: "comed", UtilityRate: "comed_besh"}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "comed provider not configured")
	})

	t.Run("Site with ComEd unsupported rate", func(t *testing.T) {
		m := Configured(nil)
		settings := types.Settings{UtilityProvider: "comed", UtilityRate: "unsupported"}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "unknown utility rate")
	})

	t.Run("Site with Ameren provider", func(t *testing.T) {
		m := Configured(nil)
		settings := types.Settings{UtilityProvider: "ameren", UtilityRate: "ameren_psp"}
		u, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.NotNil(t, u)

		// Second call should return the cached instance
		u2, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.Equal(t, u, u2)
	})

	t.Run("Site with Ameren missing base", func(t *testing.T) {
		m := NewMap(nil) // Ameren base not configured
		settings := types.Settings{UtilityProvider: "ameren", UtilityRate: "ameren_psp"}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "ameren provider not configured")
	})

	t.Run("Site with Ameren unsupported rate", func(t *testing.T) {
		m := Configured(nil)
		settings := types.Settings{UtilityProvider: "ameren", UtilityRate: "unsupported"}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "unknown utility rate")
	})

	t.Run("Site with TOU provider", func(t *testing.T) {
		m := NewMap(nil)
		settings := types.Settings{UtilityProvider: "tou_example", UtilityRate: "tou_example_1"}
		u, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.NotNil(t, u)

		// Second call should return the cached instance
		u2, err := m.Site(ctx, "site1", settings)
		require.NoError(t, err)
		assert.Equal(t, u, u2)
	})

	t.Run("Site caches custom provider but checks ApplySettings error", func(t *testing.T) {
		m := NewMap(nil)
		provider := &mockUtility{settingsErr: assert.AnError}
		m.SetProvider("site1", provider)

		settings := types.Settings{UtilityRate: "mock"}
		u, err := m.Site(ctx, "site1", settings)
		require.ErrorIs(t, err, assert.AnError)
		assert.Nil(t, u)
	})

	t.Run("Site with ComEd provider ApplySettings error", func(t *testing.T) {
		m := Configured(nil)
		settings := types.Settings{UtilityProvider: "comed", UtilityRate: "comed_besh", UtilityRateOptions: types.UtilityRateOptions{RateClass: "invalid"}}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "unknown ComEd rate class")
	})

	t.Run("Site with TOU provider ApplySettings error", func(t *testing.T) {
		m := NewMap(nil)
		settings := types.Settings{UtilityProvider: "tou_example", UtilityRate: "unknown"}
		u, err := m.Site(ctx, "site1", settings)
		require.Error(t, err)
		assert.Nil(t, u)
		assert.ErrorContains(t, err, "unknown utility rate: unknown")
	})

	t.Run("Site with tesla_dynamic updates ERCOT load zone on TDU location change", func(t *testing.T) {
		m := Configured(nil)
		fixedNow := time.Date(2026, time.September, 15, 10, 0, 0, 0, ctLocation)
		dateKey := fixedNow.Format("20060102")
		m.baseERCOT = &baseERCOT{
			nowFunc: func() time.Time { return fixedNow },
			cachedPrices: map[string][]types.Price{
				"LZ_HOUSTON_" + dateKey: {
					{
						Provider:                      "ercot",
						TSStart:                       fixedNow,
						TSEnd:                         fixedNow.Add(time.Hour),
						DollarsPerKWH:                 0.020,
						SeparateGenerationCredit:      true,
						GenerationCreditDollarsPerKWH: 0.020,
					},
				},
				"LZ_NORTH_" + dateKey: {
					{
						Provider:                      "ercot",
						TSStart:                       fixedNow,
						TSEnd:                         fixedNow.Add(time.Hour),
						DollarsPerKWH:                 0.080,
						SeparateGenerationCredit:      true,
						GenerationCreditDollarsPerKWH: 0.080,
					},
				},
			},
		}

		// 1. Start on CenterPoint (LZ_HOUSTON)
		u1, err := m.Site(ctx, "site-tx", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "centerpoint",
			},
		})
		require.NoError(t, err)
		sf1, ok := u1.(*SiteFees)
		require.True(t, ok)
		z1, ok := sf1.base.(*baseERCOTZone)
		require.True(t, ok)
		assert.Equal(t, "LZ_HOUSTON", z1.loadZone)

		p1, err := u1.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, 0.020*0.90, p1.GenerationCreditDollarsPerKWH, 1e-6)

		// 2. Change TDU location to Oncor (LZ_NORTH) -> must rebuild with LZ_NORTH
		u2, err := m.Site(ctx, "site-tx", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "oncor",
			},
		})
		require.NoError(t, err)
		assert.NotSame(t, u1, u2)
		sf2, ok := u2.(*SiteFees)
		require.True(t, ok)
		z2, ok := sf2.base.(*baseERCOTZone)
		require.True(t, ok)
		assert.Equal(t, "LZ_NORTH", z2.loadZone)

		p2, err := u2.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, 0.080*0.90, p2.GenerationCreditDollarsPerKWH, 1e-6)

		// 3. Change TDU location to TNMP (also LZ_NORTH) -> reuses cached provider and updates delivery fees
		u3, err := m.Site(ctx, "site-tx", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "tnmp",
			},
		})
		require.NoError(t, err)
		assert.Same(t, u2, u3)
		p3, err := u3.GetCurrentPrice(ctx)
		require.NoError(t, err)
		assert.InDelta(t, texasTDUs["tnmp"].DeliveryDollarsPerKWH, p3.GridUseDollarsPerKWH, 1e-6)

		// 4. Invalid TDU location returns error instead of reusing old zone
		u4, err := m.Site(ctx, "site-tx", types.Settings{
			UtilityProvider: "tesla_electric_tx",
			UtilityRate:     "tesla_dynamic",
			UtilityRateOptions: types.UtilityRateOptions{
				Location: "invalid_tdu",
			},
		})
		require.Error(t, err)
		assert.Nil(t, u4)
		assert.ErrorContains(t, err, "unknown TDU delivery utility")
	})

	t.Run("Site with eversource_ct_vpp returns VPP periods", func(t *testing.T) {
		m := Configured(nil)
		u, err := m.Site(ctx, "site-ct", types.Settings{
			UtilityProvider: "eversource",
			UtilityRate:     "eversource_ct_vpp",
			UtilityRateOptions: types.UtilityRateOptions{
				VPPProgram: "ess-passive",
			},
		})
		require.NoError(t, err)
		vppInfo, err := u.GetVPPInfo(ctx)
		require.NoError(t, err)
		assert.Len(t, vppInfo.Mandatory, 6)
	})
}

func TestRatesCoverAllTime(t *testing.T) {
	for _, provider := range allUtilities {
		for _, rate := range provider.Rates {
			t.Run(rate.ID, func(t *testing.T) {
				t.Parallel()
				periods, err := rate.GetFees(types.UtilityRateOptions{})
				require.NoError(t, err)
				if !assert.Greater(t, len(periods), 0, "should have at least one period") {
					return
				}

				minStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				maxEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

				// Find earliest Start and latest End from periods
				hasBound := false
				for _, p := range periods {
					if !p.Start.IsZero() {
						if !hasBound || p.Start.Before(minStart) {
							minStart = p.Start
						}
					}
					if !p.End.IsZero() {
						if !hasBound || p.End.After(maxEnd) {
							maxEnd = p.End
						}
					}
					hasBound = true
				}

				// If all periods started/ended before our default UTC year then
				// move the loop bounds to match. We check this because minStart
				// and maxEnd were initialized to 2026 UTC.
				for _, p := range periods {
					if !p.Start.IsZero() && p.Start.Before(minStart) {
						minStart = p.Start
					}
					if !p.End.IsZero() && p.End.After(maxEnd) {
						maxEnd = p.End
					}
				}

				// Use a location for the loop if all periods have one
				loopLoc := time.UTC
				var candidateLoc *time.Location
				first := true
				var hasSolar bool
				var hasBattery bool
				var hasAdditional bool
				var hasSubHourly bool
				for _, p := range periods {
					if p.LocationPtr != nil {
						if first {
							candidateLoc = p.LocationPtr
							first = false
						} else if p.LocationPtr.String() != candidateLoc.String() {
							candidateLoc = nil
						}
					}
					if p.SeparateGenerationCredit {
						hasSolar = true
					}
					if p.SeparateBatteryGenerationCredit {
						hasBattery = true
					}
					if p.GridAdditional {
						hasAdditional = true
					}
					for _, hr := range p.Hours {
						if hr.MinuteStart != 0 || hr.MinuteEnd != 0 {
							hasSubHourly = true
						}
					}
				}
				if candidateLoc != nil {
					loopLoc = candidateLoc
				}

				// Calendar Sampling Optimization:
				// Brute-force checking every single hour of a multi-year window (e.g. for dynamic schedules
				// like SCE or PG&E which have thousands of periods) is extremely slow and resource intensive.
				// Since utility schedules are highly periodic, we sample a subset of days per month to balance
				// coverage and execution speed.
				//
				// To ensure eventually complete test coverage over multiple test runs, we randomly select
				// 8 weekdays and 8 weekend days per month. All defined holidays/specific dates are always tested.
				holidaysMap := make(map[string]bool)
				for _, p := range periods {
					for _, d := range p.SpecificDates {
						holidaysMap[d] = true
					}
				}

				type ymM struct {
					year  int
					month time.Month
				}
				monthDays := make(map[ymM][]time.Time)
				startDay := minStart.In(loopLoc)
				startDay = time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, loopLoc)
				for d := startDay; d.Before(maxEnd); d = d.AddDate(0, 0, 1) {
					ym := ymM{d.Year(), d.Month()}
					monthDays[ym] = append(monthDays[ym], d)
				}

				testedDates := make(map[string]bool)
				for _, days := range monthDays {
					var weekdays []time.Time
					var weekends []time.Time
					for _, d := range days {
						dateStr := d.Format("2006-01-02")
						if holidaysMap[dateStr] {
							testedDates[dateStr] = true
							continue
						}
						if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
							weekends = append(weekends, d)
						} else {
							weekdays = append(weekdays, d)
						}
					}

					// Shuffle days randomly to pick a different subset of days on each test run
					rand.Shuffle(len(weekdays), func(i, j int) {
						weekdays[i], weekdays[j] = weekdays[j], weekdays[i]
					})
					rand.Shuffle(len(weekends), func(i, j int) {
						weekends[i], weekends[j] = weekends[j], weekends[i]
					})

					// Select up to 8 weekdays and 8 weekend days
					for i := 0; i < len(weekdays) && i < 8; i++ {
						testedDates[weekdays[i].Format("2006-01-02")] = true
					}
					for i := 0; i < len(weekends) && i < 8; i++ {
						testedDates[weekends[i].Format("2006-01-02")] = true
					}
				}

				var failures int
				for d := startDay; d.Before(maxEnd); d = d.AddDate(0, 0, 1) {
					dateStr := d.Format("2006-01-02")
					if !testedDates[dateStr] {
						continue
					}

					for hour := 0; hour < 24; hour++ {
						minutes := []int{0}
						if hasSubHourly {
							minutes = []int{0, 15, 30, 45}
						}
						for _, minute := range minutes {
							ts := d.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
							if ts.Before(minStart) || !ts.Before(maxEnd) {
								continue
							}

							var applicableNames []string
							var applicableSolarNames []string
							var applicableBatteryNames []string
							var additionalNames []string
							for i := range periods {
								ok, _, err := periods[i].Contains(ts)
								// require.NoError is slow in tight loops so we only check inside
								// of the if
								if err != nil {
									require.NoError(t, err)
								}
								if ok {
									if periods[i].SeparateGenerationCredit {
										applicableSolarNames = append(applicableSolarNames, periods[i].Description)
									} else if periods[i].SeparateBatteryGenerationCredit {
										applicableBatteryNames = append(applicableBatteryNames, periods[i].Description)
									} else if periods[i].GridAdditional {
										additionalNames = append(additionalNames, periods[i].Description)
									} else {
										applicableNames = append(applicableNames, periods[i].Description)
									}
								}
							}
							expectBase := 1
							if rate.ID == "comed_besh" || rate.ID == "ameren_psp" {
								expectBase = 0
							}
							isMatch := len(applicableNames) == expectBase
							if rate.ID == "comed_bes" || rate.ID == "comed_besh" {
								// PSC, PEC/MPCC, and PEA/HPEA are GridAdditional: false.
								// PEA/HPEA fallback ends Jan 1, 2027, so it has 3 before Jan 1 2027 and 2 after.
								isMatch = len(applicableNames) == 2 || len(applicableNames) == 3
							} else if rate.ID == "comed_best" {
								// PJM Component, TPEA, and BEST TOU charge are GridAdditional: false
								isMatch = len(applicableNames) == 3
							}
							if !isMatch {
								failures++
								if failures > 10 {
									assert.Fail(t, "too many failures")
									return
								}
								assert.Fail(t, "missing or overlapping base periods", "Hour %v should have %v period (found %v)", ts, expectBase, applicableNames)
							}
							if hasSolar && len(applicableSolarNames) != 1 {
								failures++
								if failures > 10 {
									assert.Fail(t, "too many failures")
									return
								}
								assert.Fail(t, "missing or overlapping solar periods", "Hour %v should have 1 period (found %v)", ts, applicableSolarNames)
							}
							if hasBattery && len(applicableBatteryNames) != 1 {
								failures++
								if failures > 10 {
									assert.Fail(t, "too many failures")
									return
								}
								assert.Fail(t, "missing or overlapping battery export periods", "Hour %v should have 1 period (found %v)", ts, applicableBatteryNames)
							}
							// we allow multiple additional periods
							if hasAdditional && len(additionalNames) < 1 {
								failures++
								if failures > 10 {
									assert.Fail(t, "too many failures")
									return
								}
								assert.Fail(t, "missing or overlapping additional periods", "Hour %v should have at least 1 period (found %v)", ts, additionalNames)
							}
						}
					}
				}
			})
		}
	}
}
