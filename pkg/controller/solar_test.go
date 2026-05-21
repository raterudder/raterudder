package controller

import (
	"context"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestCalculateWeatherSolar(t *testing.T) {
	ctx := context.Background()
	loc := types.SiteLocation{TimeZone: "America/Chicago"}

	t.Run("basic projection", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 400, TemperatureC: 25},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		assert.NotEmpty(t, results)

		// 12:00 has history, so it should calibrate efficiency
		// Irradiance 800, Solar 10 -> Efficiency = 10 / (800 * TempFactor(1) * SnowFactor(1)) = 0.0125
		// 13:00 projection: 400 * 0.0125 = 5.0
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]
		assert.InDelta(t, 5.24, val13.ImprovedSolar, 0.1)
	})

	t.Run("clipping cap", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC), SolarKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC), SolarKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 1000},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val12 := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()]
		// Should learn clipping cap of 5.0
		assert.LessOrEqual(t, val12.ImprovedSolar, 5.0001)
	})

	t.Run("curtailed history ignored", func(t *testing.T) {
		history := []types.EnergyStats{
			// Curtailed hour: export=0, battery=100. Should be ignored.
			{TSHourStart: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC), SolarKWH: 2.0, GridExportKWH: 0.0, MaxBatterySOC: 100},
			// Valid hour:
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0, MaxBatterySOC: 100},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
				},
			},
		}
		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		// Efficiency should be learned ONLY from 12:00 (10.0 / 800 * ...)
		// Not from 11:00 (which would drag efficiency down)
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]
		assert.InDelta(t, 10.0, val13.ImprovedSolar, 0.1)
	})

	t.Run("snow effects", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					// Valid history hour
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 0, SnowDepthCM: 0},
					// Heavy snow prediction
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 0, SnowDepthCM: 6.0},
					// Partial snow prediction
					{TSHourStart: time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 0, SnowDepthCM: 1.0},
					// Dusting
					{TSHourStart: time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 0, SnowDepthCM: 0.1},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		assert.Equal(t, 0.0, results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()].SnowFactor)
		assert.Equal(t, 0.1, results[time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC).Unix()].SnowFactor)
		assert.Equal(t, 0.7, results[time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC).Unix()].SnowFactor)

		// Ensure output generation scales down accordingly
		baseSolar := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()].ImprovedSolar
		assert.Equal(t, 0.0, results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()].ImprovedSolar)
		assert.InDelta(t, baseSolar*0.1, results[time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC).Unix()].ImprovedSolar, 0.01)
		assert.InDelta(t, baseSolar*0.7, results[time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC).Unix()].ImprovedSolar, 0.01)
	})

	t.Run("fallback to GHI", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					// GTI is missing (0)
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GHI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GHI: 400, TemperatureC: 25},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		// Should use GHI for calculation
		val12 := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()]
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]

		assert.Equal(t, 800.0, val12.Irradiance)
		assert.Equal(t, 400.0, val13.Irradiance)
		assert.InDelta(t, 5.24, val13.ImprovedSolar, 0.1) // proportional to 10
	})

	t.Run("ignore current hour", func(t *testing.T) {
		currentHour := time.Now().Truncate(time.Hour)
		yesterdayHour := currentHour.Add(-24 * time.Hour)
		futureHour := currentHour.Add(time.Hour)

		history := []types.EnergyStats{
			// Yesterday's hour: should be used for calibration (yields efficiency = 10.0 / theoretical)
			{TSHourStart: yesterdayHour, SolarKWH: 10.0, GridExportKWH: 5.0},
			// Current hour: has high actual solar (e.g. 50.0) but should be IGNORED for calibration
			{TSHourStart: currentHour, SolarKWH: 50.0, GridExportKWH: 25.0},
		}

		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: yesterdayHour, GTI: 800, TemperatureC: 25},
					{TSHourStart: currentHour, GTI: 800, TemperatureC: 25},
					{TSHourStart: futureHour, GTI: 800, TemperatureC: 25},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, currentHour, history, weather, loc)
		assert.NotEmpty(t, results)

		valYesterday := results[yesterdayHour.Unix()]
		valCurrent := results[currentHour.Unix()]
		valFuture := results[futureHour.Unix()]

		// If current hour was ignored for calibration:
		// 1. Learned efficiency is based ONLY on yesterday (which yields 10.0 ImprovedSolar).
		// 2. Both currentHour and futureHour (both having GTI 800) should project exactly around 10.0 ImprovedSolar.
		// 3. Neither should be 50.0 (the actual value of current hour), and neither should be 0.0.
		assert.InDelta(t, 10.0, valYesterday.ImprovedSolar, 0.1)
		assert.InDelta(t, 10.0, valCurrent.ImprovedSolar, 0.1)
		assert.InDelta(t, 10.0, valFuture.ImprovedSolar, 0.1)
	})
}

func TestCalculateSmoothedSolar(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	settings := types.Settings{SolarBellCurveMultiplier: 1.0}

	t.Run("averaging and smoothing", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC), SolarKWH: 2.0, GridExportKWH: 2.0},
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 5.0, GridExportKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 10.0},
			{TSHourStart: time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC), SolarKWH: 5.0, GridExportKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC), SolarKWH: 2.0, GridExportKWH: 2.0},
		}

		results := CalculateSmoothedSolar(ctx, now, history, settings)
		// Should have daylight from 11 to 15. mu = 11 + 5/2 = 13.5.
		assert.InDelta(t, 10.0, results[13], 0.1)
		// 12 and 15 should be roughly similar (symmetric around 13.5)
		assert.InDelta(t, results[12], results[15], 0.5)
	})

	t.Run("no solar data", func(t *testing.T) {
		history := []types.EnergyStats{}
		results := CalculateSmoothedSolar(ctx, now, history, settings)
		assert.Empty(t, results)
	})

	t.Run("bell curve disabled", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 5.0, GridExportKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), SolarKWH: 15.0, GridExportKWH: 15.0},
		}
		// Multiplier 0 means no adjustment, just simple averages
		s := types.Settings{SolarBellCurveMultiplier: 0}
		results := CalculateSmoothedSolar(ctx, now, history, s)
		assert.Equal(t, 10.0, results[12])
		assert.Len(t, results, 1) // only hour 12
	})

	t.Run("curtailed history ignored for curve fitting", func(t *testing.T) {
		history := []types.EnergyStats{
			// Hour 12: normally good, but battery full and no export -> curtailed.
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 2.0, GridExportKWH: 0.0, MaxBatterySOC: 100},
			// Hour 13: good data
			{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0, MaxBatterySOC: 100},
			// Adding more hours to establish a range
			{TSHourStart: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC), SolarKWH: 5.0, GridExportKWH: 5.0},
			{TSHourStart: time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC), SolarKWH: 5.0, GridExportKWH: 5.0},
		}
		// The average for 12 is 2.0, but for fitting the peak, it should ignore 12 and use 13 as the best hour.
		results := CalculateSmoothedSolar(ctx, now, history, settings)

		// It should boost hour 12 because the curve is anchored around hour 13 with peak ~10.0
		assert.Greater(t, results[12], 2.0, "Should have boosted curtailed hour")
	})
}
