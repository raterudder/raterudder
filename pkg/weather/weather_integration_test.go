package weather

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_RealAPI_GetLocationData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	s := NewService(Config{})
	loc, err := s.GetLocationData(context.Background(), "US", "90210")
	require.NoError(t, err)
	require.NotNil(t, loc)
	assert.Equal(t, "Beverly Hills", loc.City)
	assert.Equal(t, "US", loc.CountryCode)
	assert.Equal(t, "90210", loc.PostalCode)
	assert.InDelta(t, 34.07, loc.Lat, 0.1)
	assert.InDelta(t, -118.40, loc.Long, 0.1)
	assert.Equal(t, "America/Los_Angeles", loc.TimeZone)
}

func TestIntegration_RealAPI_FetchWeatherForecast(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	loc, _ := time.LoadLocation("America/Los_Angeles")
	targetDay := time.Now().In(loc)
	startDay := targetDay.AddDate(0, 0, -1)
	endDay := targetDay.AddDate(0, 0, 1)

	s := NewService(Config{})
	weathers, err := s.FetchWeatherForecast(context.Background(), 34.07, -118.40, "America/Los_Angeles", startDay, endDay)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(weathers), 2)
}
