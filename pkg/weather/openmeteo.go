package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
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
				Latitude:    result.Latitude,
				Longitude:   result.Longitude,
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
		TiltedRadiation    []float64 `json:"global_tilted_irradiance"`
		Temperature        []float64 `json:"temperature_2m"`
		Snowfall           []float64 `json:"snowfall"`
	} `json:"hourly"`
}

// FetchWeatherForecast fetches the weather forecast data for the specified date range.
// startDate is inclusive and endDate is exclusive, similar to storage boundaries.
// Returns a slice of types.Weather structs for each day in the requested range.
func (s *OpenMeteo) FetchWeatherForecast(
	ctx context.Context,
	loc types.SiteLocation,
	startDate, endDate time.Time,
) ([]types.Weather, error) {
	if loc.Latitude == 0 && loc.Longitude == 0 {
		return nil, fmt.Errorf("latitude and longitude are required")
	}

	timezone := loc.TimeZone
	tLoc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	// Open-Meteo expects an inclusive end date for its query.
	// we used to subtract one day if the endDate sent was exactly midnight, but
	// some of the data points from the API are for th	e preceding hour so we need
	// to always fetch the next hour.
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	u, err := url.Parse(s.ForecastURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse forecast base URL: %w", err)
	}

	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%f", loc.Latitude))
	q.Set("longitude", fmt.Sprintf("%f", loc.Longitude))
	hourly := []string{"shortwave_radiation", "temperature_2m", "snowfall"}
	var hasTiltedRadiation bool
	if loc.SolarTilt > 0 {
		hasTiltedRadiation = true
		hourly = append(hourly, "global_tilted_irradiance")
		q.Set("tilt", fmt.Sprintf("%f", loc.SolarTilt))
		if loc.SolarAzimuth < 0 || loc.SolarAzimuth > 360 {
			log.Ctx(ctx).WarnContext(ctx, "open-meteo: invalid azimuth", slog.Float64("azimuth", loc.SolarAzimuth))
		} else {
			// correct for values > 180 which should be negative when passed to Open-Meteo
			if loc.SolarAzimuth > 180 {
				loc.SolarAzimuth = loc.SolarAzimuth - 360
			}
			q.Set("azimuth", fmt.Sprintf("%f", loc.SolarAzimuth))
		}
	}
	q.Set("hourly", strings.Join(hourly, ","))
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
		err := json.NewDecoder(resp.Body).Decode(&errData)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "open-meteo: failed to parse weather error response", slog.Any("error", err), slog.Int("status", resp.StatusCode))
			return nil, fmt.Errorf("weather api returned status %d", resp.StatusCode)
		}
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: weather api returned status", slog.Any("response", errData), slog.Int("status", resp.StatusCode))
		return nil, fmt.Errorf("weather api returned status %d: %v", resp.StatusCode, errData)
	}

	var data weatherForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: failed to decode weather response", slog.Any("error", err))
		return nil, fmt.Errorf("failed to decode weather response: %w", err)
	}

	if len(data.Daily.Time) != len(data.Daily.Sunrise) || len(data.Daily.Time) != len(data.Daily.Sunset) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: daily data mismatch", slog.Int("timeCount", len(data.Daily.Time)), slog.Int("sunriseCount", len(data.Daily.Sunrise)), slog.Int("sunsetCount", len(data.Daily.Sunset)))
		return nil, fmt.Errorf("daily data mismatch: %d times, %d sunrises, %d sunsets", len(data.Daily.Time), len(data.Daily.Sunrise), len(data.Daily.Sunset))
	}

	if len(data.Hourly.Time) != len(data.Hourly.ShortwaveRadiation) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("ghiCount", len(data.Hourly.ShortwaveRadiation)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d shortwave radiation", len(data.Hourly.Time), len(data.Hourly.ShortwaveRadiation))
	}

	if hasTiltedRadiation && len(data.Hourly.Time) != len(data.Hourly.TiltedRadiation) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("gtiCount", len(data.Hourly.TiltedRadiation)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d tilted radiation", len(data.Hourly.Time), len(data.Hourly.TiltedRadiation))
	}

	// Parse the response into daily types.Weather structs
	var weathers []types.Weather
	now := time.Now()
	startMidnight := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, tLoc)
	for day := startMidnight; day.Before(endDate); day = day.AddDate(0, 0, 1) {
		targetDateStr := day.Format("2006-01-02")
		w := types.Weather{
			TSDayStart:   day,
			TimeLocation: timezone,
			Latitude:     loc.Latitude,
			Longitude:    loc.Longitude,
			TSUpdated:    now,
		}

		// Find daily sunrise/sunset
		for i, tStr := range data.Daily.Time {
			if tStr == targetDateStr {
				t, err := time.ParseInLocation("2006-01-02T15:04", data.Daily.Sunrise[i], tLoc)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "open-meteo: failed to parse sunrise time", slog.Any("error", err), slog.String("time", data.Daily.Sunrise[i]))
				} else {
					w.TSSunrise = t
				}
				t, err = time.ParseInLocation("2006-01-02T15:04", data.Daily.Sunset[i], tLoc)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "open-meteo: failed to parse sunset time", slog.Any("error", err), slog.String("time", data.Daily.Sunset[i]))
				} else {
					w.TSSunset = t
				}
				break
			}
		}

		// Find hourly data
		for i, tStr := range data.Hourly.Time {
			if strings.HasPrefix(tStr, targetDateStr) {
				t, err := time.ParseInLocation("2006-01-02T15:04", tStr, tLoc)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "open-meteo: failed to parse hourly time", slog.Any("error", err), slog.String("time", tStr))
					continue
				}
				hw := types.HourlyWeather{
					TSHourStart: t,
					SnowfallCM:  data.Hourly.Snowfall[i],
				}

				// the temperature values are instantaneous values so we average the
				// temperature at this start of the hour to the next start of the hour
				// to get the "average" temperature over the hour
				if i+1 < len(data.Hourly.Temperature) {
					hw.TemperatureC = (data.Hourly.Temperature[i] + data.Hourly.Temperature[i+1]) / 2.0
				} else {
					// realistically we should never get here because we'll be before the end date
					hw.TemperatureC = data.Hourly.Temperature[i]
				}

				// irradiance values are for the hour preceding the timestamp
				// so we take the value for the next hour
				if i+1 < len(data.Hourly.ShortwaveRadiation) {
					hw.GHI = data.Hourly.ShortwaveRadiation[i+1]
				} else {
					// realistically we should never get here because we'll be before the end date
					hw.GHI = data.Hourly.ShortwaveRadiation[i]
				}

				// irradiance values are for the hour preceding the timestamp
				// so we take the value for the next hour
				if hasTiltedRadiation {
					if i+1 < len(data.Hourly.TiltedRadiation) {
						hw.GTI = data.Hourly.TiltedRadiation[i+1]
					} else {
						// realistically we should never get here because we'll be before the end date
						hw.GTI = data.Hourly.TiltedRadiation[i]
					}
				}

				w.ForecastHours = append(w.ForecastHours, hw)
			}
		}
		weathers = append(weathers, w)
	}

	return weathers, nil
}
