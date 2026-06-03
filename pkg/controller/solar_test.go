package controller

import (
	"context"
	"math"
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

	t.Run("clipping cap learned with 3 occurrences", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0},
			{TSHourStart: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0},
			{TSHourStart: time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 4, 12, 0, 0, 0, time.UTC), GTI: 1500, TemperatureC: 25},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val12 := results[time.Date(2024, 1, 4, 12, 0, 0, 0, time.UTC).Unix()]
		// Should learn clipping cap of 10.0, so the forecast for GTI=1500 is capped at 10.0.
		assert.LessOrEqual(t, val12.ImprovedSolar, 10.0001)
	})

	t.Run("clipping cap not learned with 2 occurrences", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0},
			{TSHourStart: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), GTI: 1000, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 4, 12, 0, 0, 0, time.UTC), GTI: 1500, TemperatureC: 25},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val12 := results[time.Date(2024, 1, 4, 12, 0, 0, 0, time.UTC).Unix()]
		// With only 2 occurrences, no clipping cap is learned, so ImprovedSolar should be unclipped (~15.0).
		assert.Greater(t, val12.ImprovedSolar, 14.0)
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

func TestSolarCalculations(t *testing.T) {
	t.Run("sun position Chicago", func(t *testing.T) {
		tm := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
		el, az := calculateSunPosition(tm, 41.8781, -87.6298)
		assert.Greater(t, el, 0.0)
		assert.GreaterOrEqual(t, az, 0.0)
		assert.Less(t, az, 360.0)
	})

	t.Run("angle of incidence horizontal", func(t *testing.T) {
		// If array is horizontal (tilt = 0), AOI should be the zenith angle (90 - elevation)
		aoi := calculateAngleOfIncidence(60.0, 180.0, 0.0, 180.0)
		assert.InDelta(t, 30.0*math.Pi/180.0, aoi, 0.001)
	})

	t.Run("GTI horizontal", func(t *testing.T) {
		// At elevation = 90, tilt = 0, DNI = 800, DHI = 100, GTI should be DNI * cos(0) + DHI = 900
		gti := calculateGTI(800.0, 100.0, 90.0, 180.0, 0.0, 180.0)
		assert.InDelta(t, 900.0, gti, 0.1)
	})

	t.Run("weather solar 1h and 15m basic", func(t *testing.T) {
		ctx := context.Background()
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				TSDayStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Latitude:   41.8781,
				Longitude:  -87.6298,
				ForecastHours: []types.HourlyWeather{
					{
						TSHourStart:  time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC),
						DNI:          800,
						DHI:          100,
						TemperatureC: 25,
						Forecast15m: []types.Weather15m{
							{MinuteStart: 0, DNI: 800, DHI: 100},
							{MinuteStart: 15, DNI: 800, DHI: 100},
							{MinuteStart: 30, DNI: 800, DHI: 100},
							{MinuteStart: 45, DNI: 800, DHI: 100},
						},
					},
					{TSHourStart: time.Date(2024, 1, 1, 19, 0, 0, 0, time.UTC), DNI: 400, DHI: 50, TemperatureC: 25},
				},
			},
		}
		loc := types.SiteLocation{
			Latitude:  41.8781,
			Longitude: -87.6298,
			TimeZone:  "America/Chicago",
		}

		res1h := CalculateWeatherSolar1h(ctx, time.Time{}, history, weather, loc)
		assert.NotEmpty(t, res1h)

		res15m := CalculateWeatherSolar15m(ctx, time.Time{}, history, weather, loc)
		assert.NotEmpty(t, res15m)
	})
}

func TestInterpolateHourlyEfficiencies(t *testing.T) {
	t.Run("empty validHours", func(t *testing.T) {
		valid := make(map[int]float64)
		res := interpolateHourlyEfficiencies(valid)
		var expected [24]float64
		assert.Equal(t, expected, res)
	})

	t.Run("single valid hour circular behavior", func(t *testing.T) {
		valid := map[int]float64{12: 0.15}
		res := interpolateHourlyEfficiencies(valid)
		for h := 0; h < 24; h++ {
			assert.InDelta(t, 0.15, res[h], 0.001)
		}
	})

	t.Run("linear interpolation circular boundary", func(t *testing.T) {
		// Valid at 22 and 2
		valid := map[int]float64{22: 0.10, 2: 0.20}
		res := interpolateHourlyEfficiencies(valid)
		// Distance from 22 to 2 is 4 hours (22 -> 23 -> 0 -> 1 -> 2)
		// Hour 0 is exactly in the middle, so it should be 0.15
		assert.InDelta(t, 0.15, res[0], 0.001)
		// Hour 23 is closer to 22: should be 0.125
		assert.InDelta(t, 0.125, res[23], 0.001)
		// Hour 1 is closer to 2: should be 0.175
		assert.InDelta(t, 0.175, res[1], 0.001)
	})
}

func TestHourlyScaleFactorCalibration(t *testing.T) {
	ctx := context.Background()
	loc := types.SiteLocation{TimeZone: "America/Chicago"}

	t.Run("hourly shading learned and interpolated", func(t *testing.T) {
		// We provide 5 days of history for hours 10, 11, 12, 13, 14, 15
		// Hour 12 has shading on all days: actual solar is 40% of normal
		history := []types.EnergyStats{}
		weatherHours := []types.HourlyWeather{}

		baseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		// Create 5 days of data
		for d := 0; d < 5; d++ {
			day := baseDate.AddDate(0, 0, d)
			for h := 10; h <= 16; h++ { // Include 16 in forecast
				ts := day.Add(time.Duration(h) * time.Hour)
				weatherHours = append(weatherHours, types.HourlyWeather{
					TSHourStart: ts,
					GTI:         1000.0,
				})

				if h <= 15 { // Only put 10..15 in history
					var actualSolar float64
					switch h {
					case 10:
						actualSolar = 10.0
					case 11:
						actualSolar = 12.0
					case 12:
						actualSolar = 6.0 // shaded
					case 13:
						actualSolar = 14.0
					case 14:
						actualSolar = 13.0
					case 15:
						actualSolar = 11.0
					}
					history = append(history, types.EnergyStats{
						TSHourStart:   ts,
						SolarKWH:      actualSolar,
						GridExportKWH: 1.0,
					})
				}
			}
		}

		weather := []types.Weather{
			{
				ForecastHours: weatherHours,
			},
		}

		res := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		t.Logf("res map size: %d", len(res))
		val11 := res[baseDate.Add(11*time.Hour).Unix()]
		val12 := res[baseDate.Add(12*time.Hour).Unix()]
		val16 := res[baseDate.Add(16*time.Hour).Unix()]

		assert.InDelta(t, 12.0, val11.ImprovedSolar, 0.5)
		assert.InDelta(t, 6.0, val12.ImprovedSolar, 0.5)
		assert.InDelta(t, 11.0, val16.ImprovedSolar, 0.5)
	})

	t.Run("fallback to staticEff when < 4 valid hours", func(t *testing.T) {
		// We provide 5 days of history for ONLY 3 hours: 11, 12, 13
		history := []types.EnergyStats{}
		weatherHours := []types.HourlyWeather{}

		baseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 5; d++ {
			day := baseDate.AddDate(0, 0, d)
			for h := 11; h <= 13; h++ {
				ts := day.Add(time.Duration(h) * time.Hour)
				weatherHours = append(weatherHours, types.HourlyWeather{
					TSHourStart: ts,
					GTI:         1000.0,
				})

				var actualSolar float64
				switch h {
				case 11:
					actualSolar = 12.0
				case 12:
					actualSolar = 13.0
				case 13:
					actualSolar = 14.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:   ts,
					SolarKWH:      actualSolar,
					GridExportKWH: 1.0,
				})
			}
			// Add a forecast-only hour 15 (no history)
			weatherHours = append(weatherHours, types.HourlyWeather{
				TSHourStart: day.Add(15 * time.Hour),
				GTI:         1000.0,
			})
		}

		weather := []types.Weather{
			{
				ForecastHours: weatherHours,
			},
		}

		res := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		// Since we only had 3 valid hours, Stage 3 falls back to staticEff for all hours.
		// staticEff is based on daily total:
		// Daily actual = 12 + 13 + 14 = 39.0
		// Daily theoretical = 1000 * 0.978125 * 3 = 2934.375
		// staticEff = 39.0 / 2934.375 = 0.013291
		// For hour 15, expected solar = 1000 * 0.013291 * 0.978125 = 13.0
		val15 := res[baseDate.Add(15*time.Hour).Unix()]
		assert.InDelta(t, 13.0, val15.ImprovedSolar, 0.5)
	})

	t.Run("outlier hour (noisy) thrown away and interpolated", func(t *testing.T) {
		// We have 5 days of history for hours 10, 11, 12, 13, 14, 15.
		// Hour 12 has extremely noisy data: [15.0, 1.0, 30.0, 0.5, 20.0],
		// which means stdDev / mean > 0.6. It should be thrown away and interpolated.
		history := []types.EnergyStats{}
		weatherHours := []types.HourlyWeather{}

		baseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		noisyValues := []float64{15.0, 1.0, 30.0, 0.5, 20.0}

		for d := 0; d < 5; d++ {
			day := baseDate.AddDate(0, 0, d)
			for h := 10; h <= 15; h++ {
				ts := day.Add(time.Duration(h) * time.Hour)
				weatherHours = append(weatherHours, types.HourlyWeather{
					TSHourStart: ts,
					GTI:         1000.0,
				})

				var actualSolar float64
				switch h {
				case 10:
					actualSolar = 10.0
				case 11:
					actualSolar = 12.0
				case 12:
					actualSolar = noisyValues[d]
				case 13:
					actualSolar = 14.0
				case 14:
					actualSolar = 13.0
				case 15:
					actualSolar = 11.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:   ts,
					SolarKWH:      actualSolar,
					GridExportKWH: 1.0,
				})
			}
		}

		weather := []types.Weather{
			{
				ForecastHours: weatherHours,
			},
		}

		res := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		// Hour 12 is invalid due to high noise.
		// The valid hours are 10, 11, 13, 14, 15 (5 valid hours >= 4).
		// So Hour 12 should be interpolated between 11 (12.0) and 13 (14.0) -> ~13.0.
		val12 := res[baseDate.Add(12*time.Hour).Unix()]
		assert.InDelta(t, 13.0, val12.ImprovedSolar, 0.5)
	})

	t.Run("unphysically high hour thrown away and interpolated", func(t *testing.T) {
		// We have 5 days of history for hours 10, 11, 12, 13, 14, 15.
		// Hour 12 has unphysically high data: 30.0 kWh (more than double normal).
		history := []types.EnergyStats{}
		weatherHours := []types.HourlyWeather{}

		baseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		for d := 0; d < 5; d++ {
			day := baseDate.AddDate(0, 0, d)
			for h := 10; h <= 15; h++ {
				ts := day.Add(time.Duration(h) * time.Hour)
				weatherHours = append(weatherHours, types.HourlyWeather{
					TSHourStart: ts,
					GTI:         1000.0,
				})

				var actualSolar float64
				switch h {
				case 10:
					actualSolar = 10.0
				case 11:
					actualSolar = 12.0
				case 12:
					actualSolar = 30.0 // unphysically high outlier
				case 13:
					actualSolar = 14.0
				case 14:
					actualSolar = 13.0
				case 15:
					actualSolar = 11.0
				}
				history = append(history, types.EnergyStats{
					TSHourStart:   ts,
					SolarKWH:      actualSolar,
					GridExportKWH: 1.0,
				})
			}
		}

		weather := []types.Weather{
			{
				ForecastHours: weatherHours,
			},
		}

		res := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		// Hour 12 is invalid due to being unphysically high.
		// The valid hours are 10, 11, 13, 14, 15 (5 valid hours >= 4).
		// So Hour 12 should be interpolated between 11 (12.0) and 13 (14.0) -> ~13.0.
		val12 := res[baseDate.Add(12*time.Hour).Unix()]
		assert.InDelta(t, 13.0, val12.ImprovedSolar, 0.5)
	})
}

func Test15mInverterClipping(t *testing.T) {
	ctx := context.Background()
	loc := types.SiteLocation{
		TimeZone:     "America/Chicago",
		Latitude:     41.8781,
		Longitude:    -87.6298,
		SolarTilt:    0,
		SolarAzimuth: 180,
	}

	t.Run("15m chunk-level clipping using hourly clipping cap", func(t *testing.T) {
		// We have 7 days of history.
		// On each day, the peak hour (12:00) has exactly 10.0 kWh.
		// Since it occurs 7 times (>= 6), Stage 1 learns an hourly clipping cap of 10.0 kWh.
		history := []types.EnergyStats{}
		var weatherHours []types.HourlyWeather

		baseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for d := 0; d < 7; d++ {
			day := baseDate.AddDate(0, 0, d)

			// High solar hour: establishes hourly clipping cap = 10.0 (18:00 UTC = 12:00 PM CST)
			tsHigh := day.Add(18 * time.Hour)
			weatherHours = append(weatherHours, types.HourlyWeather{
				TSHourStart:  tsHigh,
				DHI:          800.0,
				TemperatureC: -100.0, // Clamps tCell to -40 for tempFactor = 1.2275
			})
			history = append(history, types.EnergyStats{
				TSHourStart:   tsHigh,
				SolarKWH:      10.0,
				GridExportKWH: 1.0,
			})
		}

		// Now add the forecast hour (on baseDate + 7 days) at 18:00 UTC
		// Unclipped chunks: 3.0, 3.0, 1.0, 1.0 kWh (total 8.0 kWh unclipped)
		// With clipping cap of 10.0 kWh -> chunk cap of 2.5 kWh.
		// Clipped chunks: 2.5, 2.5, 1.0, 1.0 kWh (total 7.0 kWh clipped)
		forecastDay := baseDate.AddDate(0, 0, 7)
		tsForecast := forecastDay.Add(18 * time.Hour)

		// We need to make sure staticEff is calibrated such that we get the desired chunkGen.
		// tsHigh has stats.SolarKWH = 10.0, DHI = 800.0.
		// irradiance = 800, tempFactor = 1.2275.
		// staticEff = 10.0 / (800 * 1.2275) = 0.0101833.
		// To get chunkGen = 3.0: GTI * staticEff * tempFactor * 0.25 = 3.0
		// GTI * 0.0101833 * 1.2275 * 0.25 = 3.0
		// GTI * 0.003125 = 3.0 -> GTI = 960.
		// To get chunkGen = 1.0: GTI = 320.
		// So we set chunk DHIs to 960 and 320!
		weatherHours = append(weatherHours, types.HourlyWeather{
			TSHourStart:  tsForecast,
			DHI:          640.0, // average DHI
			TemperatureC: -100.0,
			Forecast15m: []types.Weather15m{
				{MinuteStart: 0, DHI: 960.0},
				{MinuteStart: 15, DHI: 960.0},
				{MinuteStart: 30, DHI: 320.0},
				{MinuteStart: 45, DHI: 320.0},
			},
		})

		weather := []types.Weather{
			{
				TSDayStart:    baseDate,
				Latitude:      loc.Latitude,
				Longitude:     loc.Longitude,
				ForecastHours: weatherHours,
			},
		}

		res := CalculateWeatherSolar15m(ctx, time.Time{}, history, weather, loc)
		assert.NotEmpty(t, res)

		valForecast := res[tsForecast.Unix()]
		// Unclipped should be ~8.0, Improved should be ~7.0 (clipped at 2.5 per chunk)
		assert.InDelta(t, 8.0, valForecast.UnclippedSolar, 0.1)
		assert.InDelta(t, 7.0, valForecast.ImprovedSolar, 0.1)
	})
}

func TestCalculateSnowFactor(t *testing.T) {
	t.Run("no snow", func(t *testing.T) {
		assert.Equal(t, 1.0, calculateSnowFactor(0.0))
	})

	t.Run("negative snow depth treated as no snow", func(t *testing.T) {
		assert.Equal(t, 1.0, calculateSnowFactor(-1.0))
	})

	t.Run("light dusting just above zero", func(t *testing.T) {
		assert.Equal(t, 0.70, calculateSnowFactor(0.05))
	})

	t.Run("light dusting at boundary 0.2", func(t *testing.T) {
		// 0.2 is NOT > 0.2, so it falls into the > 0.0 case
		assert.Equal(t, 0.70, calculateSnowFactor(0.2))
	})

	t.Run("moderate snow just above 0.2", func(t *testing.T) {
		assert.Equal(t, 0.1, calculateSnowFactor(0.3))
	})

	t.Run("moderate snow at 1cm", func(t *testing.T) {
		assert.Equal(t, 0.1, calculateSnowFactor(1.0))
	})

	t.Run("moderate snow at boundary 5.0", func(t *testing.T) {
		// 5.0 is NOT > 5.0, so it falls into the > 0.2 case
		assert.Equal(t, 0.1, calculateSnowFactor(5.0))
	})

	t.Run("heavy snow just above 5.0", func(t *testing.T) {
		assert.Equal(t, 0.0, calculateSnowFactor(5.1))
	})

	t.Run("very heavy snow", func(t *testing.T) {
		assert.Equal(t, 0.0, calculateSnowFactor(30.0))
	})
}

func TestIsSolarCurtailed(t *testing.T) {
	t.Run("curtailed: no export and battery full", func(t *testing.T) {
		stats := types.EnergyStats{GridExportKWH: 0.0, MaxBatterySOC: 100.0}
		assert.True(t, isSolarCurtailed(stats))
	})

	t.Run("curtailed: minimal export and battery at 98", func(t *testing.T) {
		stats := types.EnergyStats{GridExportKWH: 0.1, MaxBatterySOC: 98.0}
		assert.True(t, isSolarCurtailed(stats))
	})

	t.Run("not curtailed: export above threshold", func(t *testing.T) {
		stats := types.EnergyStats{GridExportKWH: 0.2, MaxBatterySOC: 100.0}
		assert.False(t, isSolarCurtailed(stats))
	})

	t.Run("not curtailed: battery below 98", func(t *testing.T) {
		stats := types.EnergyStats{GridExportKWH: 0.0, MaxBatterySOC: 97.9}
		assert.False(t, isSolarCurtailed(stats))
	})

	t.Run("not curtailed: both conditions fail", func(t *testing.T) {
		stats := types.EnergyStats{GridExportKWH: 5.0, MaxBatterySOC: 50.0}
		assert.False(t, isSolarCurtailed(stats))
	})

	t.Run("not curtailed: zero solar no battery", func(t *testing.T) {
		stats := types.EnergyStats{GridExportKWH: 0.0, MaxBatterySOC: 0.0}
		assert.False(t, isSolarCurtailed(stats))
	})
}

func TestTemperatureEffects(t *testing.T) {
	ctx := context.Background()
	loc := types.SiteLocation{TimeZone: "America/Chicago"}

	t.Run("hot temperature reduces output", func(t *testing.T) {
		// Create calibration history at 25°C
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}

		// Forecast at two temps: 25°C (STC) and 45°C (hot)
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 45},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val12 := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()]
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]

		// Hot temperature should reduce output
		assert.Greater(t, val12.ImprovedSolar, val13.ImprovedSolar, "hot temp should produce less")
		assert.Greater(t, val12.TempFactor, val13.TempFactor, "hot temp should have lower tempFactor")
	})

	t.Run("cold temperature increases output", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}

		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 0},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val12 := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()]
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]

		// Cold temperature should increase output (higher tempFactor)
		assert.Less(t, val12.ImprovedSolar, val13.ImprovedSolar, "cold temp should produce more")
		assert.Less(t, val12.TempFactor, val13.TempFactor, "cold temp should have higher tempFactor")
	})

	t.Run("temp factor at STC is 1.0", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}

		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					// At 25°C ambient with 0 irradiance, Tcell = 25, so TempFactor = 1.0
					// But we need irradiance > 0 for projection. With GTI=800 and Tamb=25:
					// Tcell = 25 + (800/800)*(45-20) = 25 + 25 = 50°C
					// TempFactor = 1.0 - (50-25)*0.0035 = 0.9125
					// To get TempFactor=1.0, we need Tcell=25, i.e. Tamb + irr/800*25 = 25
					// With GTI=800: Tamb = 25 - 25 = 0°C
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 0},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val12 := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()]

		// At Tamb=0°C and GTI=800: Tcell = 0 + (800/800)*(45-20) = 25°C → TempFactor = 1.0
		assert.InDelta(t, 1.0, val12.TempFactor, 0.001)
	})

	t.Run("cell temperature clamped at extremes", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}

		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					// Extremely cold: Tamb=-100, Tcell = -100 + 25 = -75, clamped to -40
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: -100},
					// Extremely hot: Tamb=100, Tcell = 100 + 25 = 125, clamped to 80
					{TSHourStart: time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 100},
				},
			},
		}

		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]
		val14 := results[time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC).Unix()]

		// Clamped to -40: TempFactor = 1.0 - (-40 - 25) * 0.0035 = 1.2275
		assert.InDelta(t, -40.0, val13.TCell, 0.001)
		assert.InDelta(t, 1.2275, val13.TempFactor, 0.001)

		// Clamped to 80: TempFactor = 1.0 - (80 - 25) * 0.0035 = 0.8075
		assert.InDelta(t, 80.0, val14.TCell, 0.001)
		assert.InDelta(t, 0.8075, val14.TempFactor, 0.001)
	})
}

func TestCalculateGTI(t *testing.T) {
	t.Run("below horizon returns zero", func(t *testing.T) {
		gti := calculateGTI(800.0, 100.0, 0.0, 180.0, 30.0, 180.0)
		assert.Equal(t, 0.0, gti)
	})

	t.Run("negative elevation returns zero", func(t *testing.T) {
		gti := calculateGTI(800.0, 100.0, -5.0, 180.0, 30.0, 180.0)
		assert.Equal(t, 0.0, gti)
	})

	t.Run("tilted south-facing array at solar noon", func(t *testing.T) {
		// Sun at 60° elevation, due south (180°), array tilted 30° facing south (180°)
		// AOI: cos(AOI) = sin(60°)*cos(30°) + cos(60°)*sin(30°)*cos(0°)
		//              = 0.866*0.866 + 0.5*0.5*1.0
		//              = 0.75 + 0.25 = 1.0 → AOI = 0° (perfect alignment)
		// Direct = 800 * 1.0 = 800
		// Diffuse = 100 * (1 + cos(30°)) / 2 = 100 * (1 + 0.866) / 2 = 93.3
		gti := calculateGTI(800.0, 100.0, 60.0, 180.0, 30.0, 180.0)
		assert.InDelta(t, 893.3, gti, 1.0)
	})

	t.Run("sun behind array", func(t *testing.T) {
		// Sun in the north (0°), array facing south (180°) at 45° tilt, sun at 30° elevation
		// The sun is behind the array, so direct component should be 0 (cos(AOI) < 0 → clamped to 0)
		// Only diffuse remains: 100 * (1 + cos(45°)) / 2 = 100 * (1 + 0.707) / 2 = 85.35
		gti := calculateGTI(800.0, 100.0, 30.0, 0.0, 45.0, 180.0)
		assert.InDelta(t, 85.35, gti, 1.0)
	})

	t.Run("horizontal array gets full beam", func(t *testing.T) {
		// Already tested in TestSolarCalculations, but verify at lower elevation
		// Sun at 45° elevation, tilt=0°, AOI = 90° - 45° = 45°
		// Direct = 800 * cos(45°) = 800 * 0.707 = 565.7
		// Diffuse = 100 * (1 + cos(0°)) / 2 = 100
		gti := calculateGTI(800.0, 100.0, 45.0, 180.0, 0.0, 180.0)
		assert.InDelta(t, 665.7, gti, 1.0)
	})

	t.Run("zero DNI only diffuse", func(t *testing.T) {
		// Overcast day: no direct beam, only diffuse
		// Diffuse on horizontal: 200 * (1 + cos(0°)) / 2 = 200
		gti := calculateGTI(0.0, 200.0, 45.0, 180.0, 0.0, 180.0)
		assert.InDelta(t, 200.0, gti, 0.1)
	})
}

func TestCalculateSunPosition(t *testing.T) {
	t.Run("summer solstice solar noon Chicago", func(t *testing.T) {
		// Chicago: 41.8781°N, -87.6298°W
		// Solar noon on June 21 is approximately 12:53 CDT (17:53 UTC)
		// At solar noon, azimuth should be ~180° (due south)
		// Max elevation at summer solstice for 41.88°N ≈ 90 - 41.88 + 23.44 = 71.56°
		tm := time.Date(2024, 6, 21, 17, 53, 0, 0, time.UTC)
		el, az := calculateSunPosition(tm, 41.8781, -87.6298)

		assert.InDelta(t, 71.5, el, 1.5, "summer solstice max elevation for Chicago")
		assert.InDelta(t, 180.0, az, 5.0, "solar noon azimuth should be ~due south")
	})

	t.Run("winter solstice solar noon Chicago", func(t *testing.T) {
		// Max elevation at winter solstice for 41.88°N ≈ 90 - 41.88 - 23.44 = 24.68°
		tm := time.Date(2024, 12, 21, 17, 53, 0, 0, time.UTC)
		el, az := calculateSunPosition(tm, 41.8781, -87.6298)

		assert.InDelta(t, 24.7, el, 1.5, "winter solstice max elevation for Chicago")
		assert.InDelta(t, 180.0, az, 5.0, "solar noon azimuth should be ~due south")
	})

	t.Run("night time sun below horizon", func(t *testing.T) {
		// Midnight in Chicago (06:00 UTC in summer)
		tm := time.Date(2024, 6, 21, 6, 0, 0, 0, time.UTC)
		el, _ := calculateSunPosition(tm, 41.8781, -87.6298)

		assert.Less(t, el, 0.0, "sun should be below horizon at midnight")
	})

	t.Run("equator equinox", func(t *testing.T) {
		// At the equator on the equinox, the sun at solar noon should be ~90° elevation
		// March 20 2024 at ~12:00 UTC (solar noon at 0° longitude)
		tm := time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC)
		el, _ := calculateSunPosition(tm, 0.0, 0.0)

		assert.InDelta(t, 90.0, el, 2.0, "equator equinox noon should be near zenith")
	})
}

func TestCalculateWeatherSolarEdgeCases(t *testing.T) {
	ctx := context.Background()
	loc := types.SiteLocation{TimeZone: "America/Chicago"}

	t.Run("empty history and weather", func(t *testing.T) {
		results := CalculateWeatherSolar(ctx, time.Time{}, nil, nil, loc)
		assert.Empty(t, results)
	})

	t.Run("empty history with weather", func(t *testing.T) {
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
				},
			},
		}
		results := CalculateWeatherSolar(ctx, time.Time{}, nil, weather, loc)
		// Should have entries but with zero solar (no calibration data)
		assert.Len(t, results, 1)
		val := results[time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()]
		assert.Equal(t, 0.0, val.ImprovedSolar, "no history means no efficiency, so zero projection")
	})

	t.Run("history with no matching weather", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					// Different timestamp than history
					{TSHourStart: time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
				},
			},
		}
		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		// Weather hour exists but no matching history, so no calibrated efficiency
		assert.Len(t, results, 1)
		val := results[time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC).Unix()]
		assert.Equal(t, 0.0, val.ImprovedSolar)
	})

	t.Run("zero irradiance produces zero solar", func(t *testing.T) {
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
					{TSHourStart: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC), GTI: 0, TemperatureC: 25},
				},
			},
		}
		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, loc)
		val13 := results[time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC).Unix()]
		assert.Equal(t, 0.0, val13.ImprovedSolar, "zero irradiance should produce zero solar")
	})

	t.Run("invalid timezone falls back to UTC", func(t *testing.T) {
		badLoc := types.SiteLocation{TimeZone: "Invalid/Timezone"}
		history := []types.EnergyStats{
			{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), SolarKWH: 10.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), GTI: 800, TemperatureC: 25},
				},
			},
		}
		// Should not panic, just fall back to UTC
		results := CalculateWeatherSolar(ctx, time.Time{}, history, weather, badLoc)
		assert.NotEmpty(t, results)
	})
}

func TestCalculateWeatherSolar1h(t *testing.T) {
	ctx := context.Background()
	loc := types.SiteLocation{
		Latitude:     41.8781,
		Longitude:    -87.6298,
		TimeZone:     "America/Chicago",
		SolarTilt:    0,
		SolarAzimuth: 180,
	}

	t.Run("1h projection with DNI and DHI", func(t *testing.T) {
		// Use a summer afternoon hour where sun is well above the horizon
		// June 21 at 18:00 UTC = ~1PM CDT (good solar production)
		calibHour := time.Date(2024, 6, 21, 18, 0, 0, 0, time.UTC)
		forecastHour := time.Date(2024, 6, 21, 19, 0, 0, 0, time.UTC)

		history := []types.EnergyStats{
			{TSHourStart: calibHour, SolarKWH: 8.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: calibHour, DNI: 700, DHI: 150, TemperatureC: 30},
					{TSHourStart: forecastHour, DNI: 700, DHI: 150, TemperatureC: 30},
				},
			},
		}

		results := CalculateWeatherSolar1h(ctx, time.Time{}, history, weather, loc)
		assert.NotEmpty(t, results)

		// Both hours have same DNI/DHI/temp but different sun positions, so
		// GTI will differ (sun angle changes). The calibration hour and forecast hour
		// may have slightly different projected values due to the sun position change.
		valCalib := results[calibHour.Unix()]
		valForecast := results[forecastHour.Unix()]

		assert.Greater(t, valCalib.ImprovedSolar, 0.0, "calibration hour should have positive solar")
		assert.Greater(t, valForecast.ImprovedSolar, 0.0, "forecast hour should have positive solar")
		assert.Greater(t, valCalib.Irradiance, 0.0, "GTI should be positive during daytime")
		assert.Greater(t, valForecast.Irradiance, 0.0, "GTI should be positive during daytime")
	})

	t.Run("1h night hours produce zero", func(t *testing.T) {
		// Calibrate with a daytime hour, forecast a night hour
		calibHour := time.Date(2024, 6, 21, 18, 0, 0, 0, time.UTC)
		nightHour := time.Date(2024, 6, 22, 6, 0, 0, 0, time.UTC) // midnight CDT

		history := []types.EnergyStats{
			{TSHourStart: calibHour, SolarKWH: 8.0, GridExportKWH: 5.0},
		}
		weather := []types.Weather{
			{
				ForecastHours: []types.HourlyWeather{
					{TSHourStart: calibHour, DNI: 700, DHI: 150, TemperatureC: 30},
					{TSHourStart: nightHour, DNI: 0, DHI: 0, TemperatureC: 20},
				},
			},
		}

		results := CalculateWeatherSolar1h(ctx, time.Time{}, history, weather, loc)
		valNight := results[nightHour.Unix()]
		assert.Equal(t, 0.0, valNight.ImprovedSolar, "night should produce zero solar")
	})
}
