package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateSettings(t *testing.T) {
	t.Run("v1: initial defaults", func(t *testing.T) {
		s, changed, err := MigrateSettings(Settings{}, 0)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 2.0, s.IgnoreHourUsageOverMultiple)
		assert.Equal(t, 0.5, s.IgnoreHourUsageFloorKWH)
		assert.Equal(t, 0.03, s.MinArbitrageDifferenceDollarsPerKWH)
		assert.Equal(t, 20.0, s.MinBatterySOC)
	})

	t.Run("v5 to v6: release production", func(t *testing.T) {
		s, changed, err := MigrateSettings(Settings{Release: ""}, 5)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "production", s.Release)
	})

	t.Run("v5 to v7: comed_hourly to comed/comed_besh", func(t *testing.T) {
		old := Settings{
			UtilityProvider: "comed_hourly",
		}
		s, changed, err := MigrateSettings(old, 4)
		require.NoError(t, err)
		assert.True(t, changed)
		// v5 change: comed_hourly -> comed_besh
		// v7 change: comed_besh -> (comed, comed_besh)
		assert.Equal(t, "comed", s.UtilityProvider)
		assert.Equal(t, "comed_besh", s.UtilityRate)
	})

	t.Run("v6 to v7: comed_besh to comed/comed_besh", func(t *testing.T) {
		old := Settings{
			UtilityProvider: "comed_besh",
			UtilityRateOptions: UtilityRateOptions{
				RateClass: "singleFamilyWithoutElectricHeat",
			},
		}
		s, changed, err := MigrateSettings(old, 6)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "comed", s.UtilityProvider)
		assert.Equal(t, "comed_besh", s.UtilityRate)
		assert.Equal(t, "singleFamilyWithoutElectricHeat", s.UtilityRateOptions.RateClass)
	})

	t.Run("v8 to v9: set UpdateGroup", func(t *testing.T) {
		// Both ESS and Utility configured, UpdateGroup unset (0)
		old := Settings{
			ESS:                       "franklin",
			UtilityProvider:           "comed",
			MinStartChargeMinutes:     5,
			PeakSurvivalBufferMinutes: 30,
		}
		s, changed, err := MigrateSettings(old, 8)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, s.UpdateGroup >= 1 && s.UpdateGroup <= 16, "UpdateGroup should be between 1 and 16, got %d", s.UpdateGroup)

		// ESS configured but Utility is not
		oldNoUtility := Settings{
			ESS:                       "franklin",
			MinStartChargeMinutes:     5,
			PeakSurvivalBufferMinutes: 30,
			IgnoreHourUsageFloorKWH:   0.5,
		}
		s, changed, err = MigrateSettings(oldNoUtility, 8)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, 0, s.UpdateGroup)

		// Utility configured but ESS is not
		oldNoESS := Settings{
			UtilityProvider:           "comed",
			MinStartChargeMinutes:     5,
			PeakSurvivalBufferMinutes: 30,
			IgnoreHourUsageFloorKWH:   0.5,
		}
		s, changed, err = MigrateSettings(oldNoESS, 8)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, 0, s.UpdateGroup)

		// UpdateGroup already set
		oldSet := Settings{
			ESS:                       "franklin",
			UtilityProvider:           "comed",
			UpdateGroup:               5,
			MinStartChargeMinutes:     5,
			PeakSurvivalBufferMinutes: 30,
			IgnoreHourUsageFloorKWH:   0.5,
		}
		s, changed, err = MigrateSettings(oldSet, 8)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, 5, s.UpdateGroup)
	})

	t.Run("v9 to v10: default timing values", func(t *testing.T) {
		old := Settings{
			MinStartChargeMinutes:     0,
			PeakSurvivalBufferMinutes: 0,
		}
		s, changed, err := MigrateSettings(old, 9)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 5, s.MinStartChargeMinutes)
		assert.Equal(t, 30, s.PeakSurvivalBufferMinutes)
	})

	t.Run("no change: current version", func(t *testing.T) {
		current := Settings{
			UtilityProvider:           "comed",
			UtilityRate:               "comed_besh",
			Release:                   "production",
			UpdateGroup:               7,
			MinStartChargeMinutes:     5,
			PeakSurvivalBufferMinutes: 30,
		}
		s, changed, err := MigrateSettings(current, CurrentSettingsVersion)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, current, s)
	})
}

func TestCredentialsHas(t *testing.T) {
	t.Run("All Nil", func(t *testing.T) {
		creds := &Credentials{}
		hasMap := creds.Has()
		// Do not use assert.Empty or assert.False on the entire map per memory guidelines
		// The map is pre-populated with keys for each credential strategy
		for _, v := range hasMap {
			assert.False(t, v, "Expected all credential values to be false")
		}
		// Also verify the specific keys
		assert.Contains(t, hasMap, "franklin")
		assert.Contains(t, hasMap, "mock")
		assert.Contains(t, hasMap, "tesla")
		assert.Contains(t, hasMap, "enphase")
	})

	t.Run("Only Franklin", func(t *testing.T) {
		creds := &Credentials{
			Franklin: &FranklinCredentials{},
		}
		hasMap := creds.Has()
		assert.True(t, hasMap["franklin"], "Expected franklin to be true")
		assert.False(t, hasMap["mock"], "Expected mock to be false")
		assert.False(t, hasMap["tesla"], "Expected tesla to be false")
	})

	t.Run("Only Mock", func(t *testing.T) {
		creds := &Credentials{
			Mock: &MockCredentials{},
		}
		hasMap := creds.Has()
		assert.False(t, hasMap["franklin"], "Expected franklin to be false")
		assert.True(t, hasMap["mock"], "Expected mock to be true")
		assert.False(t, hasMap["tesla"], "Expected tesla to be false")
	})

	t.Run("Only Tesla", func(t *testing.T) {
		creds := &Credentials{
			Tesla: &TeslaCredentials{},
		}
		hasMap := creds.Has()
		assert.False(t, hasMap["franklin"], "Expected franklin to be false")
		assert.False(t, hasMap["mock"], "Expected mock to be false")
		assert.True(t, hasMap["tesla"], "Expected tesla to be true")
	})

	t.Run("All Set", func(t *testing.T) {
		creds := &Credentials{
			Franklin: &FranklinCredentials{},
			Mock:     &MockCredentials{},
			Tesla:    &TeslaCredentials{},
			Enphase:  &EnphaseCredentials{},
		}
		hasMap := creds.Has()
		for key, v := range hasMap {
			assert.True(t, v, "Expected %s to be true", key)
		}
	})
}
