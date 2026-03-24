package utility

import (
	"log/slog"
	"testing"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"context"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
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
}

func TestComEdUtilityInfo(t *testing.T) {
	info := comEdUtilityInfo()

	assert.Equal(t, "comed", info.ID)
	assert.Equal(t, "ComEd", info.Name)
	require.Len(t, info.Rates, 1, "comed should have exactly 1 rate")

	rate := info.Rates[0]
	assert.Equal(t, "comed_besh", rate.ID)
	assert.NotEmpty(t, rate.Name)
	require.Len(t, rate.Options, 3, "comed_besh should have exactly 3 options")

	t.Run("RateClass option", func(t *testing.T) {
		opt := rate.Options[0]
		assert.Equal(t, "rateClass", opt.Field)
		assert.Equal(t, types.UtilityOptionTypeSelect, opt.Type)
		require.Len(t, opt.Choices, 4, "rateClass should have 4 choices")

		choiceValues := make([]string, len(opt.Choices))
		for i, c := range opt.Choices {
			choiceValues[i] = c.Value
		}
		assert.Contains(t, choiceValues, ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat)
		assert.Contains(t, choiceValues, ComEdRateClassMultiFamilyResidenceWithoutElectricSpaceHeat)
		assert.Contains(t, choiceValues, ComEdRateClassSingleFamilyResidenceWithElectricSpaceHeat)
		assert.Contains(t, choiceValues, ComEdRateClassMultiFamilyResidenceWithElectricSpaceHeat)

		assert.Equal(t, ComEdRateClassSingleFamilyResidenceWithoutElectricSpaceHeat, opt.Default)
	})

	t.Run("VariableDeliveryRate option", func(t *testing.T) {
		opt := rate.Options[1]
		assert.Equal(t, "variableDeliveryRate", opt.Field)
		assert.Equal(t, types.UtilityOptionTypeSwitch, opt.Type)
		assert.NotEmpty(t, opt.Description)
		assert.Equal(t, false, opt.Default)
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
}

func TestRatesCoverAllTime(t *testing.T) {
	for _, provider := range allUtilities {
		for _, rate := range provider.Rates {
			t.Run(rate.ID, func(t *testing.T) {
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

				// If all period started/ended before our default UTC year then
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
				var hasAdditional bool
				for _, p := range periods {
					if p.LocationPtr != nil {
						if first {
							candidateLoc = p.LocationPtr
							first = false
						} else if p.LocationPtr.String() != candidateLoc.String() {
							candidateLoc = nil
							break
						}
					}
					if p.SeparateGenerationCredit {
						hasSolar = true
					}
					if p.GridAdditional {
						hasAdditional = true
					}
				}
				if candidateLoc != nil {
					loopLoc = candidateLoc
				}

				var failures int
				for ts := minStart.In(loopLoc); ts.Before(maxEnd); ts = ts.Add(time.Hour) {
					var applicableNames []string
					var applicableSolarNames []string
					var additionalNames []string
					for _, p := range periods {
						ok, err := p.Contains(ts)
						require.NoError(t, err)
						if ok {
							if p.SeparateGenerationCredit {
								applicableSolarNames = append(applicableSolarNames, p.Description)
							} else if p.GridAdditional {
								additionalNames = append(additionalNames, p.Description)
							} else {
								applicableNames = append(applicableNames, p.Description)
							}
						}
					}
					expectBase := 1
					if provider.ID == "comed" || provider.ID == "ameren" {
						expectBase = 0
					}
					if len(applicableNames) != expectBase {
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
			})
		}
	}
}
