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

// Location queries the Open-Meteo Geocoding API to find a location based on zip code and country code.
// It returns the location with the largest population if multiple results are found.
func (s *OpenMeteo) Location(ctx context.Context, countryCode, postalCode string) (types.SiteLocation, error) {
	if countryCode == "" || postalCode == "" {
		return types.SiteLocation{}, fmt.Errorf("country code and zip code are required")
	}

	u, err := url.Parse(s.GeocodingURL)
	if err != nil {
		return types.SiteLocation{}, fmt.Errorf("failed to parse geocoding base URL: %w", err)
	}

	q := u.Query()
	q.Set("name", postalCode)
	q.Set("count", "10")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return types.SiteLocation{}, fmt.Errorf("failed to create geocoding request: %w", err)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return types.SiteLocation{}, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.SiteLocation{}, fmt.Errorf("geocoding api returned status %d", resp.StatusCode)
	}

	var data geocodingResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return types.SiteLocation{}, fmt.Errorf("failed to decode geocoding response: %w", err)
	}

	if len(data.Results) == 0 {
		return types.SiteLocation{}, fmt.Errorf("no location found for zip code %s", postalCode)
	}

	// Find the result with the largest population that matches the country code
	var bestLoc types.SiteLocation
	maxPop := -1

	for _, result := range data.Results {
		if result.CountryCode == countryCode && result.Population > maxPop {
			maxPop = result.Population
			bestLoc = types.SiteLocation{
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

	if bestLoc == (types.SiteLocation{}) {
		return types.SiteLocation{}, fmt.Errorf("no location found for zip code %s and country code %s", postalCode, countryCode)
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
		Time                   []string  `json:"time"`
		ShortwaveRadiation     []float64 `json:"shortwave_radiation"`
		DiffuseRadiation       []float64 `json:"diffuse_radiation"`
		DirectNormalIrradiance []float64 `json:"direct_normal_irradiance"`
		TiltedRadiation        []float64 `json:"global_tilted_irradiance"`
		Temperature            []float64 `json:"temperature_2m"`
		Snowfall               []float64 `json:"snowfall"`
		SnowDepth              []float64 `json:"snow_depth"`
		CloudCover             []float64 `json:"cloud_cover"`
	} `json:"hourly"`
}

// Forecast fetches the weather forecast data for the specified date range.
// startDate is inclusive and endDate is exclusive, similar to storage boundaries.
// Returns a slice of types.Weather structs for each day in the requested range.
func (s *OpenMeteo) Forecast(
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
	hourly := []string{"shortwave_radiation", "diffuse_radiation", "direct_normal_irradiance", "snow_depth", "snowfall", "cloud_cover", "temperature_2m"}
	if loc.SolarTilt > 0 {
		hourly = append(hourly, "global_tilted_irradiance")
		q.Set("tilt", fmt.Sprintf("%f", loc.SolarTilt))
		// Open-Meteo expects Azimuth 0 south, -90 = east, +90 = west.
		// Our system uses compass degrees where 0 = North, 90 = East, 180 = South, 270 = West.
		omAzimuth := loc.SolarAzimuth - 180
		q.Set("azimuth", fmt.Sprintf("%f", omAzimuth))
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
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("shortwaveRadiationCount", len(data.Hourly.ShortwaveRadiation)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d shortwave radiation", len(data.Hourly.Time), len(data.Hourly.ShortwaveRadiation))
	}
	if len(data.Hourly.Time) != len(data.Hourly.DiffuseRadiation) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("diffuseRadiationCount", len(data.Hourly.DiffuseRadiation)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d diffuse radiation", len(data.Hourly.Time), len(data.Hourly.DiffuseRadiation))
	}
	if len(data.Hourly.Time) != len(data.Hourly.DirectNormalIrradiance) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("directNormalIrradianceCount", len(data.Hourly.DirectNormalIrradiance)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d direct normal irradiance", len(data.Hourly.Time), len(data.Hourly.DirectNormalIrradiance))
	}
	if len(data.Hourly.Time) != len(data.Hourly.Temperature) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("temperatureCount", len(data.Hourly.Temperature)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d temperature", len(data.Hourly.Time), len(data.Hourly.Temperature))
	}
	if len(data.Hourly.Time) != len(data.Hourly.Snowfall) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("snowfallCount", len(data.Hourly.Snowfall)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d snowfall", len(data.Hourly.Time), len(data.Hourly.Snowfall))
	}
	if len(data.Hourly.Time) != len(data.Hourly.SnowDepth) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("snowDepthCount", len(data.Hourly.SnowDepth)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d snow depth", len(data.Hourly.Time), len(data.Hourly.SnowDepth))
	}
	if len(data.Hourly.Time) != len(data.Hourly.CloudCover) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("cloudCoverCount", len(data.Hourly.CloudCover)))
		return nil, fmt.Errorf("hourly data mismatch: %d times, %d cloud cover", len(data.Hourly.Time), len(data.Hourly.CloudCover))
	}
	if loc.SolarTilt > 0 && len(data.Hourly.Time) != len(data.Hourly.TiltedRadiation) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.Int("timeCount", len(data.Hourly.Time)), slog.Int("tiltedRadiationCount", len(data.Hourly.TiltedRadiation)))
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
				// instance values
				hw := types.HourlyWeather{
					TSHourStart: t,
				}

				// the temperature, snow depth, and cloud cover values are instantaneous values so we average
				// the value at this start of the hour to the next start of the hour
				// to get the "average" over the hour
				if i+1 < len(data.Hourly.Temperature) {
					hw.TemperatureC = (data.Hourly.Temperature[i] + data.Hourly.Temperature[i+1]) / 2.0
					hw.SnowDepthCM = (data.Hourly.SnowDepth[i] + data.Hourly.SnowDepth[i+1]) / 2.0 * 100.0
					hw.CloudCoverPercent = (data.Hourly.CloudCover[i] + data.Hourly.CloudCover[i+1]) / 2.0
				} else {
					// realistically we should never get here because we'll be before the end date
					hw.TemperatureC = data.Hourly.Temperature[i]
					hw.SnowDepthCM = data.Hourly.SnowDepth[i] * 100.0
					hw.CloudCoverPercent = data.Hourly.CloudCover[i]
				}

				// irradiance values and snowfall are for the hour preceding the timestamp
				// so we take the value for the next hour
				if i+1 < len(data.Hourly.ShortwaveRadiation) {
					hw.GHI = data.Hourly.ShortwaveRadiation[i+1]
					hw.DHI = data.Hourly.DiffuseRadiation[i+1]
					hw.DNI = data.Hourly.DirectNormalIrradiance[i+1]
					hw.SnowfallCM = data.Hourly.Snowfall[i+1]
					if len(data.Hourly.TiltedRadiation) > i+1 {
						hw.GTI = data.Hourly.TiltedRadiation[i+1]
					}
				} else {
					// realistically we should never get here because we'll be before the end date
					hw.GHI = data.Hourly.ShortwaveRadiation[i]
					hw.DHI = data.Hourly.DiffuseRadiation[i]
					hw.DNI = data.Hourly.DirectNormalIrradiance[i]
					hw.SnowfallCM = data.Hourly.Snowfall[i]
					if len(data.Hourly.TiltedRadiation) > i {
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
