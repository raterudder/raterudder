package ess

import (
	"context"
	"log/slog"
	"testing"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStorage = storagemock.MockDatabase

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}

func TestListSystems(t *testing.T) {
	m := NewMap()
	systems := m.ListSystems(context.Background())

	require.NotEmpty(t, systems, "expected at least one system")

	ids := make(map[string]bool)
	for _, s := range systems {
		assert.NotEmpty(t, s.ID, "system must have an ID")
		assert.NotEmpty(t, s.Name, "system must have a name")
		assert.False(t, ids[s.ID], "system IDs must be unique, duplicate: %s", s.ID)
		ids[s.ID] = true

		for _, opt := range s.Credentials {
			assert.NotEmpty(t, opt.Field, "credential must have a Field")
			assert.NotEmpty(t, opt.Name, "credential must have a name")
			assert.True(t, opt.Type == types.ESSCredentialFieldTypeSelect || opt.Type == types.ESSCredentialFieldTypeString || opt.Type == types.ESSCredentialFieldTypePassword,
				"credential type must be 'select' or 'string' or 'password', got %q", opt.Type)

			if opt.Type == types.ESSCredentialFieldTypeSelect {
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

func TestCalculateDurationWeightedRates(t *testing.T) {
	t.Run("Empty Slice Returns Zero", func(t *testing.T) {
		b, s := calculateDurationWeightedRates(nil)
		assert.Equal(t, 0.0, b)
		assert.Equal(t, 0.0, s)

		b, s = calculateDurationWeightedRates([]types.TOUPeriod{})
		assert.Equal(t, 0.0, b)
		assert.Equal(t, 0.0, s)
	})

	t.Run("Single Period Preserves Rates Rounded", func(t *testing.T) {
		periods := []types.TOUPeriod{
			{StartHour: 10, StartMinute: 0, EndHour: 11, EndMinute: 0, ImportDollars: 0.1234567, ExportDollars: 0.0543219},
		}
		b, s := calculateDurationWeightedRates(periods)
		assert.InDelta(t, 0.12346, b, 0.000001)
		assert.InDelta(t, 0.05432, s, 0.000001)
	})

	t.Run("Multi Period Weighted Average Duration And Enforces Buy Ge Sell", func(t *testing.T) {
		// Period 1: 1 hour (60 min) at $0.10 import, $0.05 export
		// Period 2: 3 hours (180 min) at $0.30 import, $0.15 export
		// Total dur = 240 min
		// Weighted buy = (0.10*60 + 0.30*180) / 240 = (6 + 54) / 240 = 60 / 240 = 0.25
		// Weighted sell = (0.05*60 + 0.15*180) / 240 = (3 + 27) / 240 = 30 / 240 = 0.125
		periods := []types.TOUPeriod{
			{StartHour: 0, StartMinute: 0, EndHour: 1, EndMinute: 0, ImportDollars: 0.10, ExportDollars: 0.05},
			{StartHour: 1, StartMinute: 0, EndHour: 4, EndMinute: 0, ImportDollars: 0.30, ExportDollars: 0.15},
		}
		b, s := calculateDurationWeightedRates(periods)
		assert.InDelta(t, 0.25, b, 0.00001)
		assert.InDelta(t, 0.125, s, 0.00001)

		// Sell rate exceeding buy rate caps buy rate to sell rate
		periodsInverted := []types.TOUPeriod{
			{StartHour: 0, StartMinute: 0, EndHour: 1, EndMinute: 0, ImportDollars: 0.05, ExportDollars: 0.20},
		}
		bInv, sInv := calculateDurationWeightedRates(periodsInverted)
		assert.InDelta(t, 0.20, bInv, 0.00001)
		assert.InDelta(t, 0.20, sInv, 0.00001)
	})
}

func TestClassifyTOUScheduleTiers(t *testing.T) {
	t.Run("All Peak Periods", func(t *testing.T) {
		periods := []types.TOUPeriod{
			{StartHour: 17, EndHour: 20, Peak: true, ImportDollars: 0.40},
			{StartHour: 20, EndHour: 22, BatteryMode: types.BatteryModeExport, ImportDollars: 0.35},
		}
		tiers, tierPeriods := classifyTOUScheduleTiers(periods)
		assert.Equal(t, []touTier{touTierOnPeak, touTierOnPeak}, tiers)
		assert.Len(t, tierPeriods[touTierOnPeak], 2)
	})

	t.Run("Single Non Peak Cluster Defaults To OffPeak", func(t *testing.T) {
		periods := []types.TOUPeriod{
			{StartHour: 0, EndHour: 8, ImportDollars: 0.10},
			{StartHour: 8, EndHour: 16, ImportDollars: 0.11},
			{StartHour: 16, EndHour: 21, Peak: true, ImportDollars: 0.40},
		}
		tiers, tierPeriods := classifyTOUScheduleTiers(periods)
		assert.Equal(t, []touTier{touTierOffPeak, touTierOffPeak, touTierOnPeak}, tiers)
		assert.Len(t, tierPeriods[touTierOffPeak], 2)
		assert.Len(t, tierPeriods[touTierOnPeak], 1)
	})

	t.Run("Two Non Peak Clusters Under Spread Uses SuperOffPeak And OffPeak", func(t *testing.T) {
		// Overnight: 0.05, Daytime: 0.08 (spread 0.03 <= 0.05)
		periods := []types.TOUPeriod{
			{StartHour: 0, EndHour: 6, ImportDollars: 0.05},
			{StartHour: 6, EndHour: 16, ImportDollars: 0.08},
			{StartHour: 16, EndHour: 21, Peak: true, ImportDollars: 0.35},
		}
		tiers, tierPeriods := classifyTOUScheduleTiers(periods)
		assert.Equal(t, []touTier{touTierSuperOffPeak, touTierOffPeak, touTierOnPeak}, tiers)
		assert.Len(t, tierPeriods[touTierSuperOffPeak], 1)
		assert.Len(t, tierPeriods[touTierOffPeak], 1)
		assert.Len(t, tierPeriods[touTierOnPeak], 1)
	})

	t.Run("Wide Spread Expands To 4 Tiers", func(t *testing.T) {
		// Overnight: 0.03, Midday: 0.12, Evening Shoulder: 0.20, Peak: 0.45 (spread 0.17 > 0.05)
		periods := []types.TOUPeriod{
			{StartHour: 0, EndHour: 6, ImportDollars: 0.03},
			{StartHour: 6, EndHour: 12, ImportDollars: 0.12},
			{StartHour: 12, EndHour: 16, ImportDollars: 0.20},
			{StartHour: 16, EndHour: 21, Peak: true, ImportDollars: 0.45},
		}
		tiers, tierPeriods := classifyTOUScheduleTiers(periods)
		assert.Equal(t, []touTier{touTierSuperOffPeak, touTierOffPeak, touTierPartialPeak, touTierOnPeak}, tiers)
		assert.Len(t, tierPeriods[touTierSuperOffPeak], 1)
		assert.Len(t, tierPeriods[touTierOffPeak], 1)
		assert.Len(t, tierPeriods[touTierPartialPeak], 1)
		assert.Len(t, tierPeriods[touTierOnPeak], 1)
	})
}
