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
	Location(ctx context.Context, countryCode, postalCode string) (types.SiteLocation, error)
	Forecast(ctx context.Context, loc types.SiteLocation, startDate, endDate time.Time) ([]types.Weather, error)
}

// Configured creates a weather service using lflag.
func Configured() Service {
	return configuredOpenMeteo()
}

func configuredOpenMeteo() Service {
	geocodingBaseURL := lflag.String("open-meteo-geocoding-url", "https://geocoding-api.open-meteo.com/v1/search", "Open-Meteo geocoding API URL")
	forecastBaseURL := lflag.String("open-meteo-forecast-url", "https://api.open-meteo.com/v1/forecast", "Open-Meteo forecast API URL")

	s := &OpenMeteo{
		HTTPClient: common.HTTPClient(10 * time.Second),
	}

	lflag.Do(func() {
		s.GeocodingURL = *geocodingBaseURL
		s.ForecastURL = *forecastBaseURL
	})

	return s
}
