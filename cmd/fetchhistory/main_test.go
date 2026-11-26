package main

import (
	"testing"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundCoordinate(t *testing.T) {
	t.Run("PositivePrecision", func(t *testing.T) {
		assert.Equal(t, 33.12, roundCoordinate(33.115866499999996))
		assert.Equal(t, 41.7, roundCoordinate(41.69864))
		assert.Equal(t, 37.52, roundCoordinate(37.5245965))
	})

	t.Run("NegativePrecision", func(t *testing.T) {
		assert.Equal(t, -117.19, roundCoordinate(-117.185294))
		assert.Equal(t, -88.07, roundCoordinate(-88.0684))
		assert.Equal(t, -122.34, roundCoordinate(-122.33909360000001))
	})

	t.Run("AlreadyRounded", func(t *testing.T) {
		assert.Equal(t, 42.01, roundCoordinate(42.01))
		assert.Equal(t, -87.84, roundCoordinate(-87.84))
		assert.Equal(t, 0.0, roundCoordinate(0.0))
	})
}

func TestAnonymizeWeatherCoordinates(t *testing.T) {
	t.Run("RoundsAllWeatherDaysInPlace", func(t *testing.T) {
		weather := []types.Weather{
			{
				TimeLocation: "America/Chicago",
				Latitude:     41.69864,
				Longitude:    -88.0684,
			},
			{
				TimeLocation: "America/Los_Angeles",
				Latitude:     33.115866499999996,
				Longitude:    -117.185294,
			},
		}

		anonymizeWeatherCoordinates(weather)

		require.Len(t, weather, 2)
		assert.Equal(t, 41.7, weather[0].Latitude)
		assert.Equal(t, -88.07, weather[0].Longitude)
		assert.Equal(t, 33.12, weather[1].Latitude)
		assert.Equal(t, -117.19, weather[1].Longitude)
	})
}
