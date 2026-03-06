package weather

import (
	"context"
	"time"

	lflag "github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/types"
)

// Service provides weather data mapping and API handling.
type Service interface {
	GetLocationData(ctx context.Context, countryCode, postalCode string) (*types.SiteLocation, error)
	FetchWeatherForecast(ctx context.Context, lat, long float64, timezone string, startDate, endDate time.Time) ([]types.Weather, error)
}

// Configured creates a weather service using lflag.
func Configured() Service {
	geocodingBaseURL := lflag.String("weather-geocoding-url", "https://geocoding-api.open-meteo.com/v1/search", "Open-Meteo geocoding API URL")
	forecastBaseURL := lflag.String("weather-forecast-url", "https://api.open-meteo.com/v1/forecast", "Open-Meteo forecast API URL")

	s := &OpenMeteo{
		GeocodingURL: "https://geocoding-api.open-meteo.com/v1/search",
		ForecastURL:  "https://api.open-meteo.com/v1/forecast",
		HTTPClient:   common.HTTPClient(10 * time.Second),
	}

	lflag.Do(func() {
		s.GeocodingURL = *geocodingBaseURL
		s.ForecastURL = *forecastBaseURL
	})

	return s
}

