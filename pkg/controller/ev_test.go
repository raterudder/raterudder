package controller

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectEVCharging(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 23, 15, 0, 0, time.UTC)

	t.Run("HighPower_48A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 0.9},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 1.1},
		}
		isEV, step := detectEVCharging(ctx, 12.0, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 11.0, step, 0.1)
		}
	})

	t.Run("HighPower_48A_WithHeavyAC", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 4.5},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 4.2},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 4.6},
		}
		isEV, step := detectEVCharging(ctx, 16.0, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 11.5, step, 0.1)
		}
	})

	t.Run("MidPower_32A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 0.8},
		}
		isEV, step := detectEVCharging(ctx, 8.2, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 7.2, step, 0.1)
		}
	})

	t.Run("MidPower_24A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.0},
		}
		isEV, step := detectEVCharging(ctx, 6.5, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 5.5, step, 0.1)
		}
	})

	t.Run("PHEV_16A_WithStep", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.2},
		}
		isEV, step := detectEVCharging(ctx, 5.0, history)
		if assert.True(t, isEV) {
			assert.InDelta(t, 3.8, step, 0.1)
		}
	})

	t.Run("CoincidingAC_And_Dryer_NoStep_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 8.0},
			{TSHourStart: now.Add(-2 * time.Hour), HomeKWH: 8.2},
			{TSHourStart: now.Add(-3 * time.Hour), HomeKWH: 8.1},
		}
		isEV, step := detectEVCharging(ctx, 8.0, history)
		assert.False(t, isEV)
		assert.InDelta(t, 0.0, step, 0.1)
	})

	t.Run("OvenOrDryer_BelowStepThreshold_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 1.5},
		}
		isEV, _ := detectEVCharging(ctx, 4.2, history)
		assert.False(t, isEV)
	})

	t.Run("NormalSleeping_Baseline_Rejected", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: now.Add(-1 * time.Hour), HomeKWH: 0.8},
		}
		isEV, _ := detectEVCharging(ctx, 0.8, history)
		assert.False(t, isEV)
	})

	t.Run("EmptyHistory_UsesDefaultBaseline", func(t *testing.T) {
		isEV, step := detectEVCharging(ctx, 8.0, nil)
		if assert.True(t, isEV) {
			assert.InDelta(t, 7.0, step, 0.1)
		}
	})
}

func TestEstimateEVCharging(t *testing.T) {
	loc := time.UTC

	t.Run("EmptyHistory_ReturnsUndetected", func(t *testing.T) {
		res := EstimateEVCharging(nil, loc)
		assert.False(t, res.Detected)
		assert.Equal(t, "No energy history available to analyze.", res.Message)
	})

	t.Run("ConsistentMidnightCharges", func(t *testing.T) {
		var dailyStats []types.DailyEnergyStats
		startDate := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)

		for d := 0; d < 14; d++ {
			dayStart := startDate.AddDate(0, 0, d)
			var hourly []types.EnergyStats
			for h := 0; h < 24; h++ {
				hStart := dayStart.Add(time.Duration(h) * time.Hour)
				load := 0.8
				// Charge from 01:00 to 04:00 at 11.5 kW
				if h == 1 || h == 2 || h == 3 {
					load = 11.5
				}
				hourly = append(hourly, types.EnergyStats{
					TSHourStart: hStart,
					HomeKWH:     load,
				})
			}
			dailyStats = append(dailyStats, types.DailyEnergyStats{
				TSDayStart: dayStart,
				Hourly:     hourly,
			})
		}

		res := EstimateEVCharging(dailyStats, loc)
		if assert.True(t, res.Detected) {
			assert.Equal(t, 14, res.SessionsCount)
			assert.InDelta(t, 11.5, res.EstimatedRateKW, 0.1)
			require.Len(t, res.RecommendedPeriod.Hours, 1)
			assert.Equal(t, 1, res.RecommendedPeriod.Hours[0].HourStart)
			assert.True(t, res.RecommendedPeriod.Hours[0].HourEnd >= 4)
			if assert.NotEmpty(t, res.Sessions) {
				assert.Equal(t, time.Date(2026, 8, 1, 1, 0, 0, 0, loc), res.Sessions[0].TSStartHour)
				assert.Equal(t, time.Date(2026, 8, 1, 4, 0, 0, 0, loc), res.Sessions[0].TSEndHour)
			}
		}
	})

	t.Run("DaytimeSolarOnly_ReturnsUndetected", func(t *testing.T) {
		var dailyStats []types.DailyEnergyStats
		startDate := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)

		for d := 0; d < 7; d++ {
			dayStart := startDate.AddDate(0, 0, d)
			var hourly []types.EnergyStats
			for h := 0; h < 24; h++ {
				hStart := dayStart.Add(time.Duration(h) * time.Hour)
				load := 0.8
				// Charge only from 11:00 to 14:00 (daytime solar)
				if h >= 11 && h <= 14 {
					load = 11.5
				}
				hourly = append(hourly, types.EnergyStats{
					TSHourStart: hStart,
					HomeKWH:     load,
				})
			}
			dailyStats = append(dailyStats, types.DailyEnergyStats{
				TSDayStart: dayStart,
				Hourly:     hourly,
			})
		}

		res := EstimateEVCharging(dailyStats, loc)
		assert.False(t, res.Detected)
	})

	t.Run("MultiHour_RoundedHourBoundaries", func(t *testing.T) {
		var dailyStats []types.DailyEnergyStats
		startDate := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)

		for d := 0; d < 5; d++ {
			dayStart := startDate.AddDate(0, 0, d)
			var hourly []types.EnergyStats
			for h := 0; h < 24; h++ {
				hStart := dayStart.Add(time.Duration(h) * time.Hour)
				load := 1.0
				if h == 0 || h == 1 || h == 2 || h == 3 {
					load = 7.5
				}
				hourly = append(hourly, types.EnergyStats{
					TSHourStart: hStart,
					HomeKWH:     load,
				})
			}
			dailyStats = append(dailyStats, types.DailyEnergyStats{
				TSDayStart: dayStart,
				Hourly:     hourly,
			})
		}

		res := EstimateEVCharging(dailyStats, loc)
		if assert.True(t, res.Detected) {
			require.Len(t, res.RecommendedPeriod.Hours, 1)
			assert.Equal(t, 0, res.RecommendedPeriod.Hours[0].HourStart)
			assert.Equal(t, 4, res.RecommendedPeriod.Hours[0].HourEnd)
			assert.InDelta(t, 7.5, res.EstimatedRateKW, 0.1)
		}
	})

	t.Run("CoincidingACAndLaundry_NoStep_Rejected", func(t *testing.T) {
		var dailyStats []types.DailyEnergyStats
		startDate := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)

		// 7 days where AC is 4.0 kW baseline, and laundry brings it to 5.2 kW (only +1.2 kW step)
		for d := 0; d < 7; d++ {
			dayStart := startDate.AddDate(0, 0, d)
			var hourly []types.EnergyStats
			for h := 0; h < 24; h++ {
				hStart := dayStart.Add(time.Duration(h) * time.Hour)
				load := 4.0 // Heavy baseline (AC running overnight)
				if h >= 1 && h <= 3 {
					load = 5.2 // Laundry running with AC for 3 hours
				}
				hourly = append(hourly, types.EnergyStats{
					TSHourStart: hStart,
					HomeKWH:     load,
				})
			}
			dailyStats = append(dailyStats, types.DailyEnergyStats{
				TSDayStart: dayStart,
				Hourly:     hourly,
			})
		}

		res := EstimateEVCharging(dailyStats, loc)
		assert.False(t, res.Detected, "Should reject coinciding AC and laundry because step is only 1.2 kW")
	})
}
