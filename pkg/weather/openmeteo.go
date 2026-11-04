package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// OpenMeteo implements the weather Service interface using Open-Meteo APIs.
type OpenMeteo struct {
	GeocodingURL *url.URL
	ForecastURL  *url.URL
	HTTPClient   *http.Client
	APIKey       string
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

	u := *s.GeocodingURL

	q := u.Query()
	if s.APIKey != "" {
		q.Set("apikey", s.APIKey)
	}
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

var (
	conusWeatherModels  = []string{"ncep_nbm_conus", "ecmwf_ifs025", "icon_seamless"}
	globalWeatherModels = []string{"best_match", "ecmwf_ifs025", "icon_seamless"}
)

func isCONUSSite(loc types.SiteLocation) bool {
	if loc.CountryCode != "" && !strings.EqualFold(loc.CountryCode, "US") && !strings.EqualFold(loc.CountryCode, "USA") {
		return false
	}
	if loc.TimeZone != "" && !strings.HasPrefix(loc.TimeZone, "America/") && !strings.HasPrefix(loc.TimeZone, "US/") {
		return false
	}
	return loc.Latitude >= 24.0 && loc.Latitude <= 50.0 && loc.Longitude >= -125.0 && loc.Longitude <= -66.0
}

func forecastModels(loc types.SiteLocation) []string {
	if isCONUSSite(loc) {
		return conusWeatherModels
	}
	return globalWeatherModels
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

type rawWeatherForecastResponse struct {
	UTCOffsetSeconds *int                       `json:"utc_offset_seconds"`
	Daily            map[string]json.RawMessage `json:"daily"`
	Hourly           map[string]json.RawMessage `json:"hourly"`
}

// nullableFloat decodes JSON numbers as float64 and JSON null as math.NaN() so missing
// model values can be distinguished from 0.0 without allocating pointers per element.
type nullableFloat float64

func (f *nullableFloat) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = nullableFloat(math.NaN())
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = nullableFloat(v)
	return nil
}

type modelHourlySeries struct {
	model                  string
	shortwaveRadiation     []nullableFloat
	diffuseRadiation       []nullableFloat
	directNormalIrradiance []nullableFloat
	tiltedRadiation        []nullableFloat
	temperature            []nullableFloat
	snowfall               []nullableFloat
	snowDepth              []nullableFloat
	cloudCover             []nullableFloat
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func decodeStringSlice(m map[string]json.RawMessage, key string) ([]string, bool, error) {
	raw, ok := m[key]
	if !ok {
		return nil, false, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

func decodeNullableFloatSlice(m map[string]json.RawMessage, key string) ([]nullableFloat, bool, error) {
	raw, ok := m[key]
	if !ok {
		return nil, false, nil
	}
	var out []nullableFloat
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

func parseModelHourlySeries(ctx context.Context, hourlyMap map[string]json.RawMessage, model string, suffix string, expectedLen int) (modelHourlySeries, error) {
	var ms modelHourlySeries
	ms.model = model
	var err error

	if ms.shortwaveRadiation, _, err = decodeNullableFloatSlice(hourlyMap, "shortwave_radiation"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode shortwave_radiation%s: %w", suffix, err)
	}
	if len(ms.shortwaveRadiation) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("shortwaveRadiationCount", len(ms.shortwaveRadiation)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d shortwave radiation", expectedLen, len(ms.shortwaveRadiation))
	}

	if ms.diffuseRadiation, _, err = decodeNullableFloatSlice(hourlyMap, "diffuse_radiation"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode diffuse_radiation%s: %w", suffix, err)
	}
	if len(ms.diffuseRadiation) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("diffuseRadiationCount", len(ms.diffuseRadiation)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d diffuse radiation", expectedLen, len(ms.diffuseRadiation))
	}

	if ms.directNormalIrradiance, _, err = decodeNullableFloatSlice(hourlyMap, "direct_normal_irradiance"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode direct_normal_irradiance%s: %w", suffix, err)
	}
	if len(ms.directNormalIrradiance) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("directNormalIrradianceCount", len(ms.directNormalIrradiance)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d direct normal irradiance", expectedLen, len(ms.directNormalIrradiance))
	}

	if ms.tiltedRadiation, _, err = decodeNullableFloatSlice(hourlyMap, "global_tilted_irradiance"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode global_tilted_irradiance%s: %w", suffix, err)
	}

	if ms.temperature, _, err = decodeNullableFloatSlice(hourlyMap, "temperature_2m"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode temperature_2m%s: %w", suffix, err)
	}
	if len(ms.temperature) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("temperatureCount", len(ms.temperature)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d temperature", expectedLen, len(ms.temperature))
	}

	if ms.snowfall, _, err = decodeNullableFloatSlice(hourlyMap, "snowfall"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode snowfall%s: %w", suffix, err)
	}
	if len(ms.snowfall) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("snowfallCount", len(ms.snowfall)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d snowfall", expectedLen, len(ms.snowfall))
	}

	if ms.snowDepth, _, err = decodeNullableFloatSlice(hourlyMap, "snow_depth"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode snow_depth%s: %w", suffix, err)
	}
	if len(ms.snowDepth) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("snowDepthCount", len(ms.snowDepth)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d snow depth", expectedLen, len(ms.snowDepth))
	}

	if ms.cloudCover, _, err = decodeNullableFloatSlice(hourlyMap, "cloud_cover"+suffix); err != nil {
		return ms, fmt.Errorf("failed to decode cloud_cover%s: %w", suffix, err)
	}
	if len(ms.cloudCover) != expectedLen {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: hourly data mismatch", slog.String("model", model), slog.Int("timeCount", expectedLen), slog.Int("cloudCoverCount", len(ms.cloudCover)))
		return ms, fmt.Errorf("hourly data mismatch: %d times, %d cloud cover", expectedLen, len(ms.cloudCover))
	}

	return ms, nil
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
	// some of the data points from the API are for the preceding hour so we need
	// to always fetch the next hour.
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")
	models := forecastModels(loc)

	u := *s.ForecastURL

	q := u.Query()
	if s.APIKey != "" {
		q.Set("apikey", s.APIKey)
	}
	q.Set("latitude", fmt.Sprintf("%f", loc.Latitude))
	q.Set("longitude", fmt.Sprintf("%f", loc.Longitude))
	hourly := []string{"shortwave_radiation", "diffuse_radiation", "direct_normal_irradiance", "snow_depth", "snowfall", "cloud_cover", "temperature_2m"}
	q.Set("hourly", strings.Join(hourly, ","))
	q.Set("daily", "sunrise,sunset")
	q.Set("models", strings.Join(models, ","))
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

	var data rawWeatherForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: failed to decode weather response", slog.Any("error", err))
		return nil, fmt.Errorf("failed to decode weather response: %w", err)
	}

	dailyTime, _, err := decodeStringSlice(data.Daily, "time")
	if err != nil {
		return nil, fmt.Errorf("failed to decode daily time: %w", err)
	}
	// Single-model Open-Meteo responses (and unit test mocks) return unsuffixed "sunrise" and
	// "sunset" arrays in daily, whereas multi-model requests ("models=a,b,c") suffix every
	// daily variable with the model name ("sunrise_<model>" and "sunset_<model>"). Because
	// sunrise and sunset are astronomical calculations based on latitude/longitude and are
	// identical across models, check the unsuffixed keys first and fall back to the first
	// model-suffixed key present.
	dailySunrise, ok, err := decodeStringSlice(data.Daily, "sunrise")
	if err != nil {
		return nil, fmt.Errorf("failed to decode daily sunrise: %w", err)
	}
	if !ok {
		for _, m := range models {
			if dailySunrise, ok, err = decodeStringSlice(data.Daily, "sunrise_"+m); err != nil {
				return nil, fmt.Errorf("failed to decode daily sunrise_%s: %w", m, err)
			} else if ok {
				break
			}
		}
	}
	dailySunset, ok, err := decodeStringSlice(data.Daily, "sunset")
	if err != nil {
		return nil, fmt.Errorf("failed to decode daily sunset: %w", err)
	}
	if !ok {
		for _, m := range models {
			if dailySunset, ok, err = decodeStringSlice(data.Daily, "sunset_"+m); err != nil {
				return nil, fmt.Errorf("failed to decode daily sunset_%s: %w", m, err)
			} else if ok {
				break
			}
		}
	}

	if len(dailyTime) != len(dailySunrise) || len(dailyTime) != len(dailySunset) {
		log.Ctx(ctx).ErrorContext(ctx, "open-meteo: daily data mismatch", slog.Int("timeCount", len(dailyTime)), slog.Int("sunriseCount", len(dailySunrise)), slog.Int("sunsetCount", len(dailySunset)))
		return nil, fmt.Errorf("daily data mismatch: %d times, %d sunrises, %d sunsets", len(dailyTime), len(dailySunrise), len(dailySunset))
	}

	hourlyTime, _, err := decodeStringSlice(data.Hourly, "time")
	if err != nil {
		return nil, fmt.Errorf("failed to decode hourly time: %w", err)
	}

	var modelSeries []modelHourlySeries
	isMultiModel := false
	for _, m := range models {
		if _, exists := data.Hourly["shortwave_radiation_"+m]; exists {
			isMultiModel = true
			break
		}
	}

	if isMultiModel {
		for _, m := range models {
			if _, exists := data.Hourly["shortwave_radiation_"+m]; !exists {
				continue
			}
			ms, err := parseModelHourlySeries(ctx, data.Hourly, m, "_"+m, len(hourlyTime))
			if err != nil {
				return nil, err
			}
			modelSeries = append(modelSeries, ms)
		}
	} else {
		ms, err := parseModelHourlySeries(ctx, data.Hourly, "", "", len(hourlyTime))
		if err != nil {
			return nil, err
		}
		modelSeries = append(modelSeries, ms)
	}

	// Open-Meteo returns all timestamps in a single fixed UTC offset (utc_offset_seconds) based on the
	// request time rather than applying historical DST rules per day. Parse in omZone and convert to tLoc.
	omZone := tLoc
	if data.UTCOffsetSeconds != nil {
		omZone = time.FixedZone("openmeteo", *data.UTCOffsetSeconds)
	}

	parsedHourlyTimes := make([]time.Time, len(hourlyTime))
	parsedHourlyValid := make([]bool, len(hourlyTime))
	for i, tStr := range hourlyTime {
		rawT, err := time.ParseInLocation("2006-01-02T15:04", tStr, omZone)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "open-meteo: failed to parse hourly time", slog.Any("error", err), slog.String("time", tStr))
			continue
		}
		parsedHourlyTimes[i] = rawT.In(tLoc)
		parsedHourlyValid[i] = true
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
			Models:       models,
		}
		if isMultiModel {
			w.ModelForecasts = make(map[string][]types.HourlyWeatherMetrics, len(modelSeries))
		}

		// Find daily sunrise/sunset
		for i, tStr := range dailyTime {
			if tStr == targetDateStr {
				t, err := time.ParseInLocation("2006-01-02T15:04", dailySunrise[i], omZone)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "open-meteo: failed to parse sunrise time", slog.Any("error", err), slog.String("time", dailySunrise[i]))
				} else {
					w.TSSunrise = t.In(tLoc)
				}
				t, err = time.ParseInLocation("2006-01-02T15:04", dailySunset[i], omZone)
				if err != nil {
					log.Ctx(ctx).WarnContext(ctx, "open-meteo: failed to parse sunset time", slog.Any("error", err), slog.String("time", dailySunset[i]))
				} else {
					w.TSSunset = t.In(tLoc)
				}
				break
			}
		}

		// Find hourly data
		for i, valid := range parsedHourlyValid {
			if !valid {
				continue
			}
			t := parsedHourlyTimes[i]
			if t.Format("2006-01-02") == targetDateStr {
				hw := types.HourlyWeather{
					TSHourStart: t,
				}

				var sumGHI, sumDHI, sumDNI, sumGTI float64
				var sumTemp, sumSnowfall, sumSnowDepth, sumCloud float64
				var countIrradiance, countGTI, countTemp, countSnowfall, countSnowDepth, countCloud int

				for _, ms := range modelSeries {
					var mMetrics types.HourlyWeatherMetrics

					// the temperature, snow depth, and cloud cover values are instantaneous values so we average
					// the value at this start of the hour to the next start of the hour
					// to get the "average" over the hour
					var temp, snowDepth, cloud float64
					if i+1 < len(ms.temperature) {
						temp = float64(ms.temperature[i]+ms.temperature[i+1]) / 2.0
						snowDepth = float64(ms.snowDepth[i]+ms.snowDepth[i+1]) / 2.0 * 100.0
						cloud = float64(ms.cloudCover[i]+ms.cloudCover[i+1]) / 2.0
					} else {
						// realistically we should never get here because we'll be before the end date
						temp = float64(ms.temperature[i])
						snowDepth = float64(ms.snowDepth[i]) * 100.0
						cloud = float64(ms.cloudCover[i])
					}
					if !math.IsNaN(temp) {
						mMetrics.TemperatureC = round2(temp)
						sumTemp += temp
						countTemp++
					}
					if !math.IsNaN(snowDepth) {
						mMetrics.SnowDepthCM = round2(snowDepth)
						sumSnowDepth += snowDepth
						countSnowDepth++
					}
					if !math.IsNaN(cloud) {
						mMetrics.CloudCoverPercent = round2(cloud)
						sumCloud += cloud
						countCloud++
					}

					// irradiance values and snowfall are for the hour preceding the timestamp
					// so we take the value for the next hour
					idx := i
					if i+1 < len(ms.shortwaveRadiation) {
						idx = i + 1
					}
					ghi := float64(ms.shortwaveRadiation[idx])
					dhi := float64(ms.diffuseRadiation[idx])
					dni := float64(ms.directNormalIrradiance[idx])
					if !math.IsNaN(ghi) && !math.IsNaN(dhi) && !math.IsNaN(dni) {
						mMetrics.GHI = round2(ghi)
						mMetrics.DHI = round2(dhi)
						mMetrics.DNI = round2(dni)
						sumGHI += ghi
						sumDHI += dhi
						sumDNI += dni
						countIrradiance++
					}
					if len(ms.tiltedRadiation) > idx {
						if gti := float64(ms.tiltedRadiation[idx]); !math.IsNaN(gti) {
							mMetrics.GTI = round2(gti)
							sumGTI += gti
							countGTI++
						}
					}
					if snowfall := float64(ms.snowfall[idx]); !math.IsNaN(snowfall) {
						mMetrics.SnowfallCM = round2(snowfall)
						sumSnowfall += snowfall
						countSnowfall++
					}

					if isMultiModel {
						w.ModelForecasts[ms.model] = append(w.ModelForecasts[ms.model], mMetrics)
					}
				}

				if countIrradiance > 0 {
					hw.GHI = round2(sumGHI / float64(countIrradiance))
					hw.DHI = round2(sumDHI / float64(countIrradiance))
					hw.DNI = round2(sumDNI / float64(countIrradiance))
				}
				if countGTI > 0 {
					hw.GTI = round2(sumGTI / float64(countGTI))
				}
				if countTemp > 0 {
					hw.TemperatureC = round2(sumTemp / float64(countTemp))
				}
				if countSnowfall > 0 {
					hw.SnowfallCM = round2(sumSnowfall / float64(countSnowfall))
				}
				if countSnowDepth > 0 {
					hw.SnowDepthCM = round2(sumSnowDepth / float64(countSnowDepth))
				}
				if countCloud > 0 {
					hw.CloudCoverPercent = round2(sumCloud / float64(countCloud))
				}

				w.ForecastHours = append(w.ForecastHours, hw)
			}
		}

		if len(w.ForecastHours) == 0 {
			w.ModelForecasts = nil
		}

		weathers = append(weathers, w)
	}

	return weathers, nil
}
