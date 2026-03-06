package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// OpenMeteo implements the weather Service interface using Open-Meteo APIs.
type OpenMeteo struct {
	GeocodingURL string
	ForecastURL  string
	HTTPClient   *http.Client
}

type geocodingResponse struct {
	Results []struct {
		Name        string  `json:"name"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		CountryCode string  `json:"country_code"`
		Timezone    string  `json:"timezone"`
		Elevation   float64 `json:"elevation"`
		Population  int     `json:"population"`
	} `json:"results"`
}

// GetLocationData queries the Open-Meteo Geocoding API to find a location based on zip code and country code.
// It returns the location with the largest population if multiple results are found.
func (s *OpenMeteo) GetLocationData(ctx context.Context, countryCode, postalCode string) (*types.SiteLocation, error) {
	if countryCode == "" || postalCode == "" {
		return nil, fmt.Errorf("country code and zip code are required")
	}

	u, err := url.Parse(s.GeocodingURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse geocoding base URL: %w", err)
	}

	q := u.Query()
	q.Set("name", postalCode)
	q.Set("count", "10")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create geocoding request: %w", err)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoding api returned status %d", resp.StatusCode)
	}

	var data geocodingResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode geocoding response: %w", err)
	}

	if len(data.Results) == 0 {
		return nil, fmt.Errorf("no location found for zip code %s", postalCode)
	}

	// Find the result with the largest population that matches the country code
	var bestLoc *types.SiteLocation
	maxPop := -1

	for _, result := range data.Results {
		if result.CountryCode == countryCode && result.Population > maxPop {
			maxPop = result.Population
			bestLoc = &types.SiteLocation{
				PostalCode:  postalCode,
				CountryCode: countryCode,
				Lat:         result.Latitude,
				Long:        result.Longitude,
				City:        result.Name,
				TimeZone:    result.Timezone,
				Elevation:   result.Elevation,
			}
		}
	}

	if bestLoc == nil {
		return nil, fmt.Errorf("no location found for zip code %s and country code %s", postalCode, countryCode)
	}

	return bestLoc, nil
}

type weatherForecastResponse struct {
	Daily struct {
		Time    []string `json:"time"`
		Sunrise []string `json:"sunrise"`
		Sunset  []string `json:"sunset"`
	} `json:"daily"`
	Hourly struct {
		Time               []string  `json:"time"`
		ShortwaveRadiation []float64 `json:"shortwave_radiation"`
	} `json:"hourly"`
}

// FetchWeatherForecast fetches the shortwave radiation for the specified date range.
// startDate is inclusive and endDate is exclusive, similar to storage boundaries.
// Returns a slice of types.Weather structs for each day in the requested range.
func (s *OpenMeteo) FetchWeatherForecast(ctx context.Context, lat, long float64, timezone string, startDate, endDate time.Time) ([]types.Weather, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	startMidnight := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, loc)
	// Open-Meteo expects an inclusive end date for its query.
	// Since our endDate is exclusive, we subtract one day for the API request
	// if the endDate is exactly midnight.
	apiEndDate := endDate
	if endDate.Hour() == 0 && endDate.Minute() == 0 && endDate.Second() == 0 {
		apiEndDate = endDate.AddDate(0, 0, -1)
	}

	startDateStr := startMidnight.Format("2006-01-02")
	endDateStr := apiEndDate.Format("2006-01-02")

	u, err := url.Parse(s.ForecastURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse forecast base URL: %w", err)
	}

	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%f", lat))
	q.Set("longitude", fmt.Sprintf("%f", long))
	q.Set("hourly", "shortwave_radiation")
	q.Set("daily", "sunrise,sunset")
	q.Set("timezone", timezone)
	q.Set("start_date", startDateStr)
	q.Set("end_date", endDateStr)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create weather request: %w", err)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errData)
		return nil, fmt.Errorf("weather api returned status %d: %v", resp.StatusCode, errData)
	}

	var data weatherForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode weather response: %w", err)
	}

	// Parse the response into daily types.Weather structs
	var weathers []types.Weather
	now := time.Now()

	// Build a slice of target days based on the requested range
	var targetDays []time.Time
	for t := startMidnight; t.Before(endDate); t = t.AddDate(0, 0, 1) {
		targetDays = append(targetDays, t)
	}

	for _, targetTime := range targetDays {
		targetDateStr := targetTime.Format("2006-01-02")

		w := types.Weather{
			TSDayStart:   targetTime,
			TimeLocation: timezone,
			Lat:          lat,
			Long:         long,
			TSUpdated:    now,
		}

		// Find daily sunrise/sunset
		for i, tStr := range data.Daily.Time {
			if tStr == targetDateStr {
				if i < len(data.Daily.Sunrise) {
					t, err := time.ParseInLocation("2006-01-02T15:04", data.Daily.Sunrise[i], loc)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to parse sunrise time", slog.Any("error", err), slog.String("time", data.Daily.Sunrise[i]))
					} else {
						w.TSSunrise = t
					}
				}
				if i < len(data.Daily.Sunset) {
					t, err := time.ParseInLocation("2006-01-02T15:04", data.Daily.Sunset[i], loc)
					if err != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to parse sunset time", slog.Any("error", err), slog.String("time", data.Daily.Sunset[i]))
					} else {
						w.TSSunset = t
					}
				}
				break
			}
		}

		// Find hourly data
		for i, tStr := range data.Hourly.Time {
			t, err := time.ParseInLocation("2006-01-02T15:04", tStr, loc)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to parse hourly time", slog.Any("error", err), slog.String("time", tStr))
				continue
			}
			if t.Year() == targetTime.Year() && t.Month() == targetTime.Month() && t.Day() == targetTime.Day() {
				if i < len(data.Hourly.ShortwaveRadiation) {
					hw := types.HourlyWeather{
						TSHourStart: t,
						GHI:         data.Hourly.ShortwaveRadiation[i],
					}

					// Determine if this is an actual or forecast hour.
					// Use the rule: if the current time is more than 2 hours past sunset, the whole day is "actual" (up to current time).
					// But more generally, any hour in the past is actual, future is forecast.
					// However, the rule specified was about updating once a day, 2 hours after sunset.
					// A simple and robust way is: if t < now, it's Actual, else Forecast.
					if t.Before(now) {
						w.ActualHours = append(w.ActualHours, hw)
					} else {
						w.ForecastHours = append(w.ForecastHours, hw)
					}
				}
			}
		}

		weathers = append(weathers, w)
	}

	return weathers, nil
}
