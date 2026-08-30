package types

import (
	"context"
	"testing"
	"time"

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
			ESS:                        "franklin",
			MinStartChargeMinutes:      5,
			PeakSurvivalBufferMinutes:  30,
			IgnoreHourUsageFloorKWH:    0.5,
			HomeLoadPredictionStrategy: "default",
		}
		s, changed, err = MigrateSettings(oldNoUtility, 8)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 0, s.UpdateGroup)
		assert.Equal(t, 4.0, s.SOCBufferPercent)
		assert.Equal(t, 20, s.PeakSurvivalBufferMinutes)

		// Utility configured but ESS is not
		oldNoESS := Settings{
			UtilityProvider:            "comed",
			MinStartChargeMinutes:      5,
			PeakSurvivalBufferMinutes:  30,
			IgnoreHourUsageFloorKWH:    0.5,
			HomeLoadPredictionStrategy: "default",
		}
		s, changed, err = MigrateSettings(oldNoESS, 8)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 0, s.UpdateGroup)
		assert.Equal(t, 4.0, s.SOCBufferPercent)
		assert.Equal(t, 20, s.PeakSurvivalBufferMinutes)

		// UpdateGroup already set
		oldSet := Settings{
			ESS:                        "franklin",
			UtilityProvider:            "comed",
			UpdateGroup:                5,
			MinStartChargeMinutes:      5,
			PeakSurvivalBufferMinutes:  30,
			IgnoreHourUsageFloorKWH:    0.5,
			HomeLoadPredictionStrategy: "default",
		}
		s, changed, err = MigrateSettings(oldSet, 8)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 5, s.UpdateGroup)
		assert.Equal(t, 4.0, s.SOCBufferPercent)
		assert.Equal(t, 20, s.PeakSurvivalBufferMinutes)
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
		assert.Equal(t, 20, s.PeakSurvivalBufferMinutes)
		assert.Equal(t, 4.0, s.SOCBufferPercent)
	})

	t.Run("v11 to v12: default home load strategy", func(t *testing.T) {
		old := Settings{
			IgnoreHourUsageFloorKWH: 0.5,
		}
		s, changed, err := MigrateSettings(old, 11)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "default", s.HomeLoadPredictionStrategy)
	})

	t.Run("v12 to v13: split buffer settings", func(t *testing.T) {
		// Case 1: PeakSurvivalBufferMinutes = 30 -> Balanced
		oldBalanced := Settings{
			PeakSurvivalBufferMinutes: 30,
		}
		s, changed, err := MigrateSettings(oldBalanced, 12)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 4.0, s.SOCBufferPercent)
		assert.Equal(t, 20, s.PeakSurvivalBufferMinutes)
		assert.Equal(t, 10, s.SolarCapacityBufferMinutes)
		assert.Equal(t, 20, s.VPPChargingBufferMinutes)

		// Case 2: PeakSurvivalBufferMinutes > 30 -> Conservative
		oldConservative := Settings{
			PeakSurvivalBufferMinutes: 45,
		}
		s2, changed2, err2 := MigrateSettings(oldConservative, 12)
		require.NoError(t, err2)
		assert.True(t, changed2)
		assert.Equal(t, 8.0, s2.SOCBufferPercent)
		assert.Equal(t, 40, s2.PeakSurvivalBufferMinutes)
		assert.Equal(t, 30, s2.SolarCapacityBufferMinutes)
		assert.Equal(t, 40, s2.VPPChargingBufferMinutes)

		// Case 3: PeakSurvivalBufferMinutes < 30 -> Aggressive
		oldAggressive := Settings{
			PeakSurvivalBufferMinutes: 15,
		}
		s3, changed3, err3 := MigrateSettings(oldAggressive, 12)
		require.NoError(t, err3)
		assert.True(t, changed3)
		assert.Equal(t, 2.0, s3.SOCBufferPercent)
		assert.Equal(t, 10, s3.PeakSurvivalBufferMinutes)
		assert.Equal(t, 0, s3.SolarCapacityBufferMinutes)
		assert.Equal(t, 10, s3.VPPChargingBufferMinutes)
	})

	t.Run("v13 to v14: bump version for firestore release field", func(t *testing.T) {
		old := Settings{
			Release: "production",
		}
		s, changed, err := MigrateSettings(old, 13)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "production", s.Release)
	})

	t.Run("v14 to v15: add default MinExportHoldDifferenceDollarsPerKWH", func(t *testing.T) {
		old := Settings{
			Release: "production",
		}
		s, changed, err := MigrateSettings(old, 14)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, 0.02, s.MinExportHoldDifferenceDollarsPerKWH)
	})

	t.Run("v15 to v16: set CustomGridSettings true for configured ESS", func(t *testing.T) {
		// Case 1: Configured ESS site
		configuredSite := Settings{
			ESS: "tesla",
		}
		s1, changed1, err1 := MigrateSettings(configuredSite, 15)
		require.NoError(t, err1)
		assert.True(t, changed1)
		assert.True(t, s1.CustomGridSettings)

		// Case 2: Unconfigured ESS site
		unconfiguredSite := Settings{
			ESS: "",
		}
		s2, changed2, err2 := MigrateSettings(unconfiguredSite, 15)
		require.NoError(t, err2)
		assert.False(t, changed2)
		assert.False(t, s2.CustomGridSettings)
	})

	t.Run("no change: current version", func(t *testing.T) {
		current := Settings{
			UtilityProvider:                      "comed",
			UtilityRate:                          "comed_besh",
			Release:                              "production",
			UpdateGroup:                          7,
			MinStartChargeMinutes:                5,
			PeakSurvivalBufferMinutes:            20,
			SOCBufferPercent:                     4.0,
			SolarCapacityBufferMinutes:           10,
			VPPChargingBufferMinutes:             20,
			HomeLoadPredictionStrategy:           "default",
			MinExportHoldDifferenceDollarsPerKWH: 0.02,
			CustomGridSettings:                   true,
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

func TestGetMinBatterySOC(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)

	t.Run("EmptyPeriods_ReturnsDefaultMinBatterySOC", func(t *testing.T) {
		s := Settings{MinBatterySOC: 20.0}
		soc := s.GetMinBatterySOC(ctx, now, nil, Price{})
		assert.Equal(t, 20.0, soc)
	})

	t.Run("PeriodNameMatch_ReturnsMatchingSOC", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
				{UtilityPeriodName: "Off-Peak", MinBatterySOC: 15.0},
			},
		}
		p := Price{PeriodName: "Peak"}
		soc := s.GetMinBatterySOC(ctx, now, nil, p)
		assert.Equal(t, 50.0, soc)
	})

	t.Run("PeriodNameMatch_FallbackOnUnknownName", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		p := Price{PeriodName: "Super Off-Peak"}
		soc := s.GetMinBatterySOC(ctx, now, nil, p)
		assert.Equal(t, 20.0, soc)
	})

	t.Run("PeriodNameMatch_EmptyPricePeriodNameLogsErrorAndFallsBack", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{UtilityPeriodName: "Peak", MinBatterySOC: 50.0},
			},
		}
		soc := s.GetMinBatterySOC(ctx, now, nil, Price{})
		assert.Equal(t, 20.0, soc)
	})

	t.Run("CustomScheduleMatch_ReturnsTimeSOC", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 12}}},
					MinBatterySOC: 10.0,
				},
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 12, HourEnd: 24}}},
					MinBatterySOC: 40.0,
				},
			},
		}
		tMorning := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
		socM := s.GetMinBatterySOC(ctx, tMorning, nil, Price{})
		assert.Equal(t, 10.0, socM)

		tAfternoon := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
		socA := s.GetMinBatterySOC(ctx, tAfternoon, nil, Price{})
		assert.Equal(t, 40.0, socA)
	})

	t.Run("CustomScheduleMatch_ConvertsToLocation", func(t *testing.T) {
		loc := time.FixedZone("EDT", -4*3600) // UTC-4
		s := Settings{
			MinBatterySOC: 30.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 1, HourEnd: 6}}},
					MinBatterySOC: 50.0,
				},
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 6, HourEnd: 1}}},
					MinBatterySOC: 30.0,
				},
			},
		}
		// UTC time 05:30 is 01:30 EDT (which is in 1:00-6:00 EDT) -> should evaluate to 50.0
		tUTC := time.Date(2026, 8, 7, 5, 30, 0, 0, time.UTC)
		soc1 := s.GetMinBatterySOC(ctx, tUTC, loc, Price{})
		assert.Equal(t, 50.0, soc1)

		// UTC time 04:30 is 00:30 EDT (which is in 6:00-1:00 EDT) -> should evaluate to 30.0
		tUTC2 := time.Date(2026, 8, 7, 4, 30, 0, 0, time.UTC)
		soc2 := s.GetMinBatterySOC(ctx, tUTC2, loc, Price{})
		assert.Equal(t, 30.0, soc2)
	})

	t.Run("CustomScheduleMatch_OvernightHours", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 22, HourEnd: 6}}},
					MinBatterySOC: 30.0,
				},
			},
		}
		tNight := time.Date(2026, 5, 20, 23, 0, 0, 0, time.UTC)
		socN := s.GetMinBatterySOC(ctx, tNight, nil, Price{})
		assert.Equal(t, 30.0, socN)

		tEarly := time.Date(2026, 5, 20, 4, 0, 0, 0, time.UTC)
		socE := s.GetMinBatterySOC(ctx, tEarly, nil, Price{})
		assert.Equal(t, 30.0, socE)
	})

	t.Run("CustomScheduleMatch_UnmatchedTimeLogsErrorAndFallsBack", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 6}}},
					MinBatterySOC: 30.0,
				},
			},
		}
		tNoon := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
		soc := s.GetMinBatterySOC(ctx, tNoon, nil, Price{})
		assert.Equal(t, 20.0, soc)
	})

	t.Run("Precedence_PeriodNameOverTime", func(t *testing.T) {
		s := Settings{
			MinBatterySOC: 20.0,
			MinBatterySOCPeriods: []MinBatterySOCPeriod{
				{
					TimePeriod:    TimePeriod{Hours: []UtilityHourPeriod{{HourStart: 0, HourEnd: 24}}},
					MinBatterySOC: 15.0,
				},
				{
					UtilityPeriodName: "Peak",
					MinBatterySOC:     60.0,
				},
			},
		}
		p := Price{PeriodName: "Peak"}
		soc := s.GetMinBatterySOC(ctx, now, nil, p)
		assert.Equal(t, 60.0, soc)
	})
}
