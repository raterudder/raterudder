package weather

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"time"

	lflag "github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// Service provides weather data mapping and API handling.
type Service interface {
	Location(ctx context.Context, countryCode, postalCode string) (types.SiteLocation, error)
	Forecast(ctx context.Context, loc types.SiteLocation, startDate, endDate time.Time) ([]types.Weather, error)
}

// Configured creates a weather service using lflag.
func Configured() Service {
	geocodingBaseURL := lflag.String("open-meteo-geocoding-url", "https://geocoding-api.open-meteo.com/v1/search", "Open-Meteo geocoding API URL")
	forecastBaseURL := lflag.String("open-meteo-forecast-url", "https://api.open-meteo.com/v1/forecast", "Open-Meteo forecast API URL")
	openMeteoAPIKey := lflag.String("open-meteo-api-key", "", "Open-Meteo API key")
	googleGeocodingV4BaseURL := lflag.String("google-geocoding-url", "https://geocode.googleapis.com/v4/geocode/address", "Google Geocoding v4 API URL")
	googleTimezoneURL := lflag.String("google-timezone-url", "https://maps.googleapis.com/maps/api/timezone/json", "Google Timezone API URL")
	googleMapsAPIKey := lflag.String("google-maps-api-key", "", "Google API key for Geocoding and Time Zone APIs")

	om := &OpenMeteo{
		HTTPClient: common.HTTPClient(10 * time.Second),
	}

	var s struct{ Service }

	lflag.Do(func() {
		geoOMURL, err := url.Parse(*geocodingBaseURL)
		if err != nil {
			log.Ctx(context.Background()).Error("failed to parse open-meteo-geocoding-url",
				slog.String("url", *geocodingBaseURL),
				slog.Any("error", err),
			)
			os.Exit(1)
		}
		om.GeocodingURL = geoOMURL
		forecastOMURL, err := url.Parse(*forecastBaseURL)
		if err != nil {
			log.Ctx(context.Background()).Error("failed to parse open-meteo-forecast-url",
				slog.String("url", *forecastBaseURL),
				slog.Any("error", err),
			)
			os.Exit(1)
		}
		om.ForecastURL = forecastOMURL
		om.APIKey = *openMeteoAPIKey

		if *googleMapsAPIKey != "" {
			geoURL, err := url.Parse(*googleGeocodingV4BaseURL)
			if err != nil {
				log.Ctx(context.Background()).Error("failed to parse google-geocoding-url",
					slog.String("url", *googleGeocodingV4BaseURL),
					slog.Any("error", err),
				)
				os.Exit(1)
			}
			tzURL, err := url.Parse(*googleTimezoneURL)
			if err != nil {
				log.Ctx(context.Background()).Error("failed to parse google-timezone-url",
					slog.String("url", *googleTimezoneURL),
					slog.Any("error", err),
				)
				os.Exit(1)
			}
			s.Service = &GoogleGeocodingWrapper{
				OpenMeteo:    om,
				APIKey:       *googleMapsAPIKey,
				GeocodingURL: geoURL,
				TimezoneURL:  tzURL,
				HTTPClient:   common.HTTPClient(10 * time.Second),
			}
		} else {
			s.Service = om
		}
	})

	return &s
}
