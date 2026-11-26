package weather

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isTransientOpenMeteoErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "returned status 429") || strings.Contains(msg, "returned status 5")
}

func retryOpenMeteoIntegration[T any](t *testing.T, fn func() (T, error)) (T, error) {
	t.Helper()
	const maxAttempts = 5
	backoff := time.Second

	var zero T
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		res, err := fn()
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !isTransientOpenMeteoErr(err) {
			return zero, err
		}
		t.Logf("open-meteo integration attempt %d/%d failed with transient error: %v", attempt+1, maxAttempts, err)
		if attempt < maxAttempts-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	t.Skipf("open-meteo API transiently unavailable after %d attempts: %v", maxAttempts, lastErr)
	return zero, lastErr
}

func TestOpenMeteoService(t *testing.T) {
	t.Run("Location", func(t *testing.T) {
		tests := []struct {
			name        string
			countryCode string
			postalCode  string
			mockStatus  int
			mockBody    any
			wantErr     bool
			wantLoc     types.SiteLocation
		}{
			{
				name:        "missing params",
				countryCode: "",
				postalCode:  "",
				wantErr:     true,
			},
			{
				name:        "api error 500",
				countryCode: "US",
				postalCode:  "90210",
				mockStatus:  http.StatusInternalServerError,
				wantErr:     true,
			},
			{
				name:        "malformed response",
				countryCode: "US",
				postalCode:  "90210",
				mockStatus:  http.StatusOK,
				mockBody:    "not json",
				wantErr:     true,
			},
			{
				name:        "no results",
				countryCode: "US",
				postalCode:  "90210",
				mockStatus:  http.StatusOK,
				mockBody:    geocodingResponse{Results: nil},
				wantErr:     true,
			},
			{
				name:        "successful match",
				countryCode: "US",
				postalCode:  "90210",
				mockStatus:  http.StatusOK,
				mockBody: geocodingResponse{
					Results: []struct {
						Name        string  `json:"name"`
						Latitude    float64 `json:"latitude"`
						Longitude   float64 `json:"longitude"`
						CountryCode string  `json:"country_code"`
						Timezone    string  `json:"timezone"`
						Elevation   float64 `json:"elevation"`
						Population  int     `json:"population"`
					}{
						{
							Name:        "Wrong Country",
							CountryCode: "CA",
							Population:  10000,
						},
						{
							Name:        "Beverly Hills",
							CountryCode: "US",
							Latitude:    34.0736,
							Longitude:   -118.4004,
							Timezone:    "America/Los_Angeles",
							Elevation:   79,
							Population:  34000,
						},
					},
				},
				wantErr: false,
				wantLoc: types.SiteLocation{
					PostalCode:  "90210",
					CountryCode: "US",
					Latitude:    34.0736,
					Longitude:   -118.4004,
					City:        "Beverly Hills",
					TimeZone:    "America/Los_Angeles",
					Elevation:   79,
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var loc types.SiteLocation
				var err error
				if tc.mockBody != nil || tc.mockStatus != 0 {
					ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						assert.Equal(t, "/v1/search", r.URL.Path)
						assert.Equal(t, tc.postalCode, r.URL.Query().Get("name"))

						w.WriteHeader(tc.mockStatus)
						if s, ok := tc.mockBody.(string); ok {
							w.Write([]byte(s))
						} else {
							json.NewEncoder(w).Encode(tc.mockBody)
						}
					}))
					defer ts.Close()
					geoURL, parseErr := url.Parse(ts.URL + "/v1/search")
					require.NoError(t, parseErr)
					s := &OpenMeteo{GeocodingURL: geoURL, HTTPClient: ts.Client()}
					loc, err = s.Location(context.Background(), tc.countryCode, tc.postalCode)
				} else {
					s := &OpenMeteo{}
					loc, err = s.Location(context.Background(), tc.countryCode, tc.postalCode)
				}

				if tc.wantErr {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tc.wantLoc, loc)
				}
			})
		}
	})

	t.Run("Forecast", func(t *testing.T) {
		timezone := "America/Los_Angeles"
		loc, err := time.LoadLocation(timezone)
		require.NoError(t, err)
		targetDay := time.Now().In(loc)
		startDay := targetDay.AddDate(0, 0, -1)
		endDay := targetDay.AddDate(0, 0, 1)

		t.Run("invalid timezone", func(t *testing.T) {
			s := &OpenMeteo{}
			_, err := s.Forecast(context.Background(), types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: "Invalid/Zone"}, startDay, endDay)
			assert.Error(t, err)
		})

		t.Run("api error", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer ts.Close()

			forecastURL, parseErr := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, parseErr)
			s := &OpenMeteo{ForecastURL: forecastURL, HTTPClient: ts.Client()}
			_, err = s.Forecast(context.Background(), types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: timezone}, startDay, endDay)
			assert.Error(t, err)
		})

		t.Run("success", func(t *testing.T) {
			mockBody := weatherForecastResponse{
				Daily: struct {
					Time    []string `json:"time"`
					Sunrise []string `json:"sunrise"`
					Sunset  []string `json:"sunset"`
				}{
					Time:    []string{startDay.Format("2006-01-02"), targetDay.Format("2006-01-02"), endDay.Format("2006-01-02")},
					Sunrise: []string{startDay.Format("2006-01-02") + "T06:00", targetDay.Format("2006-01-02") + "T06:00", endDay.Format("2006-01-02") + "T06:00"},
					Sunset:  []string{startDay.Format("2006-01-02") + "T18:00", targetDay.Format("2006-01-02") + "T18:00", endDay.Format("2006-01-02") + "T18:00"},
				},
				Hourly: struct {
					Time                   []string  `json:"time"`
					ShortwaveRadiation     []float64 `json:"shortwave_radiation"`
					DiffuseRadiation       []float64 `json:"diffuse_radiation"`
					DirectNormalIrradiance []float64 `json:"direct_normal_irradiance"`
					TiltedRadiation        []float64 `json:"global_tilted_irradiance"`
					Temperature            []float64 `json:"temperature_2m"`
					Snowfall               []float64 `json:"snowfall"`
					SnowDepth              []float64 `json:"snow_depth"`
					CloudCover             []float64 `json:"cloud_cover"`
				}{
					Time:                   []string{startDay.Format("2006-01-02") + "T10:00", startDay.Format("2006-01-02") + "T11:00", endDay.Format("2006-01-02") + "T12:00", endDay.Format("2006-01-02") + "T13:00"},
					ShortwaveRadiation:     []float64{100.5, 150.5, 200.5, 250.5},
					DiffuseRadiation:       []float64{50.0, 60.0, 70.0, 80.0},
					DirectNormalIrradiance: []float64{200.0, 300.0, 400.0, 500.0},
					TiltedRadiation:        []float64{110.0, 160.0, 210.0, 260.0},
					Temperature:            []float64{20.0, 22.0, 25.0, 27.0},
					Snowfall:               []float64{0.0, 0.0, 0.0, 0.0},
					SnowDepth:              []float64{0.1, 0.3, 0.5, 0.7},
					CloudCover:             []float64{10.0, 20.0, 30.0, 40.0},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				hourly := query.Get("hourly")
				assert.NotContains(t, hourly, "global_tilted_irradiance")
				assert.Empty(t, query.Get("tilt"))
				assert.Empty(t, query.Get("azimuth"))
				assert.Equal(t, "ncep_nbm_conus,ecmwf_ifs025,icon_seamless", query.Get("models"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockBody)
			}))
			defer ts.Close()

			forecastURL, parseErr := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, parseErr)
			s := &OpenMeteo{ForecastURL: forecastURL, HTTPClient: ts.Client()}
			res, err := s.Forecast(context.Background(), types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: timezone}, startDay, endDay)

			require.NoError(t, err)
			assert.Len(t, res, 3)

			if assert.Len(t, res[0].ForecastHours, 2) {
				assert.Equal(t, []string{"ncep_nbm_conus", "ecmwf_ifs025", "icon_seamless"}, res[0].Models)
				assert.Nil(t, res[0].ModelForecasts)
				assert.Equal(t, 150.5, res[0].ForecastHours[0].GHI)
				assert.Equal(t, 160.0, res[0].ForecastHours[0].GTI)
				assert.Equal(t, 21.0, res[0].ForecastHours[0].TemperatureC)      // (20+22)/2
				assert.Equal(t, 15.0, res[0].ForecastHours[0].CloudCoverPercent) // (10+20)/2
				assert.Equal(t, 20.0, res[0].ForecastHours[0].SnowDepthCM)       // (0.1+0.3)/2 * 100
				assert.Equal(t, 200.5, res[0].ForecastHours[1].GHI)              // takes next index 2
				assert.Equal(t, 210.0, res[0].ForecastHours[1].GTI)
				assert.Equal(t, 23.5, res[0].ForecastHours[1].TemperatureC)      // average of index 1 and 2
				assert.Equal(t, 25.0, res[0].ForecastHours[1].CloudCoverPercent) // (20+30)/2
				assert.Equal(t, 40.0, res[0].ForecastHours[1].SnowDepthCM)       // (0.3+0.5)/2 * 100
			}

			assert.Len(t, res[1].ForecastHours, 0)
			if assert.Len(t, res[2].ForecastHours, 2) {
				assert.Equal(t, 250.5, res[2].ForecastHours[0].GHI)
				assert.Equal(t, 260.0, res[2].ForecastHours[0].GTI)
				assert.Equal(t, 26.0, res[2].ForecastHours[0].TemperatureC)      // (25+27)/2
				assert.Equal(t, 35.0, res[2].ForecastHours[0].CloudCoverPercent) // (30+40)/2
				assert.Equal(t, 60.0, res[2].ForecastHours[0].SnowDepthCM)       // (0.5+0.7)/2 * 100
			}
		})

		t.Run("multi-model blend CONUS and per-model forecasts", func(t *testing.T) {
			mockBody := map[string]any{
				"daily": map[string]any{
					"time":                   []string{startDay.Format("2006-01-02"), targetDay.Format("2006-01-02")},
					"sunrise_ncep_nbm_conus": []string{startDay.Format("2006-01-02") + "T06:15", targetDay.Format("2006-01-02") + "T06:16"},
					"sunset_ncep_nbm_conus":  []string{startDay.Format("2006-01-02") + "T17:45", targetDay.Format("2006-01-02") + "T17:44"},
				},
				"hourly": map[string]any{
					"time": []string{startDay.Format("2006-01-02") + "T10:00", startDay.Format("2006-01-02") + "T11:00", startDay.Format("2006-01-02") + "T12:00"},
					// Model 1: ncep_nbm_conus
					"shortwave_radiation_ncep_nbm_conus":      []any{100.0, 300.0, 600.0},
					"diffuse_radiation_ncep_nbm_conus":        []any{20.0, 60.0, 120.0},
					"direct_normal_irradiance_ncep_nbm_conus": []any{200.0, 400.0, 700.0},
					"temperature_2m_ncep_nbm_conus":           []any{18.0, 20.0, 22.0},
					"snowfall_ncep_nbm_conus":                 []any{0.0, 0.0, 0.0},
					"snow_depth_ncep_nbm_conus":               []any{0.0, 0.0, 0.0},
					"cloud_cover_ncep_nbm_conus":              []any{10.0, 20.0, 30.0},
					// Model 2: ecmwf_ifs025
					"shortwave_radiation_ecmwf_ifs025":      []any{100.0, 360.0, 630.0},
					"diffuse_radiation_ecmwf_ifs025":        []any{20.0, 90.0, 150.0},
					"direct_normal_irradiance_ecmwf_ifs025": []any{200.0, 500.0, 760.0},
					"temperature_2m_ecmwf_ifs025":           []any{20.0, 22.0, 24.0},
					"snowfall_ecmwf_ifs025":                 []any{0.0, 0.0, 0.0},
					"snow_depth_ecmwf_ifs025":               []any{0.0, 0.0, 0.0},
					"cloud_cover_ecmwf_ifs025":              []any{20.0, 40.0, 60.0},
					// Model 3: icon_seamless (with null at index 2 to verify null handling)
					"shortwave_radiation_icon_seamless":      []any{100.0, 330.0, nil},
					"diffuse_radiation_icon_seamless":        []any{20.0, 75.0, nil},
					"direct_normal_irradiance_icon_seamless": []any{200.0, 450.0, nil},
					"temperature_2m_icon_seamless":           []any{22.0, 24.0, 26.0},
					"snowfall_icon_seamless":                 []any{0.0, 0.0, 0.0},
					"snow_depth_icon_seamless":               []any{0.0, 0.0, 0.0},
					"cloud_cover_icon_seamless":              []any{30.0, 60.0, 90.0},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "ncep_nbm_conus,ecmwf_ifs025,icon_seamless", r.URL.Query().Get("models"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockBody)
			}))
			defer ts.Close()

			forecastURL, parseErr := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, parseErr)
			s := &OpenMeteo{ForecastURL: forecastURL, HTTPClient: ts.Client()}
			res, err := s.Forecast(context.Background(), types.SiteLocation{
				CountryCode: "US",
				Latitude:    35.26,
				Longitude:   -81.18,
				TimeZone:    timezone,
			}, startDay, targetDay)

			require.NoError(t, err)
			if assert.Len(t, res, 2) {
				assert.Equal(t, []string{"ncep_nbm_conus", "ecmwf_ifs025", "icon_seamless"}, res[0].Models)
				assert.Equal(t, 6, res[0].TSSunrise.Hour())
				assert.Equal(t, 15, res[0].TSSunrise.Minute())
				if assert.Len(t, res[0].ForecastHours, 3) {
					// Hour 0 (10:00) uses index 1 for irradiance: mean(300, 360, 330) = 330
					assert.Equal(t, 330.0, res[0].ForecastHours[0].GHI)
					assert.Equal(t, 75.0, res[0].ForecastHours[0].DHI)
					assert.Equal(t, 450.0, res[0].ForecastHours[0].DNI)
					// Temperature averages index 0 and 1 per model: nbm=19, ecmwf=21, icon=23 -> mean=21
					assert.Equal(t, 21.0, res[0].ForecastHours[0].TemperatureC)
					// Cloud cover averages index 0 and 1 per model: nbm=15, ecmwf=30, icon=45 -> mean=30
					assert.Equal(t, 30.0, res[0].ForecastHours[0].CloudCoverPercent)

					// Hour 1 (11:00) uses index 2 for irradiance, where icon_seamless is nil: mean(600, 630) = 615
					assert.Equal(t, 615.0, res[0].ForecastHours[1].GHI)
					assert.Equal(t, 135.0, res[0].ForecastHours[1].DHI)
					assert.Equal(t, 730.0, res[0].ForecastHours[1].DNI)
				}
				if assert.Len(t, res[0].ModelForecasts, 3) {
					require.Len(t, res[0].ModelForecasts["ncep_nbm_conus"], 3)
					assert.Equal(t, 300.0, res[0].ModelForecasts["ncep_nbm_conus"][0].GHI)
					assert.Equal(t, 600.0, res[0].ModelForecasts["ncep_nbm_conus"][1].GHI)
					require.Len(t, res[0].ModelForecasts["ecmwf_ifs025"], 3)
					assert.Equal(t, 360.0, res[0].ModelForecasts["ecmwf_ifs025"][0].GHI)
					assert.Equal(t, 630.0, res[0].ModelForecasts["ecmwf_ifs025"][1].GHI)
					require.Len(t, res[0].ModelForecasts["icon_seamless"], 3)
					assert.Equal(t, 330.0, res[0].ModelForecasts["icon_seamless"][0].GHI)
					assert.Equal(t, 0.0, res[0].ModelForecasts["icon_seamless"][1].GHI)
				}
			}
		})

		t.Run("non-CONUS site uses global models and UTC offset DST conversion", func(t *testing.T) {
			nyTZ := "America/New_York"
			nyLoc, locErr := time.LoadLocation(nyTZ)
			require.NoError(t, locErr)
			// Oct 31, 2026 is in EDT (-04:00 = -14400s), while Nov 3 request time is in EST (-05:00 = -18000s).
			oct31 := time.Date(2026, 10, 31, 0, 0, 0, 0, nyLoc)
			nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, nyLoc)

			// 2026-10-31T11:00 in fixed -05:00 (EST) is 16:00 UTC, which is 2026-10-31T12:00 EDT (-04:00).
			mockBody := map[string]any{
				"utc_offset_seconds": -18000,
				"daily": map[string]any{
					"time":               []string{"2026-10-31"},
					"sunrise_best_match": []string{"2026-10-31T06:45"},
					"sunset_best_match":  []string{"2026-10-31T17:30"},
				},
				"hourly": map[string]any{
					"time":                                   []string{"2026-10-31T11:00", "2026-10-31T12:00"},
					"shortwave_radiation_best_match":         []any{400.0, 500.0},
					"diffuse_radiation_best_match":           []any{100.0, 120.0},
					"direct_normal_irradiance_best_match":    []any{500.0, 600.0},
					"temperature_2m_best_match":              []any{20.0, 22.0},
					"snowfall_best_match":                    []any{0.0, 0.0},
					"snow_depth_best_match":                  []any{0.0, 0.0},
					"cloud_cover_best_match":                 []any{10.0, 20.0},
					"shortwave_radiation_ecmwf_ifs025":       []any{400.0, 500.0},
					"diffuse_radiation_ecmwf_ifs025":         []any{100.0, 120.0},
					"direct_normal_irradiance_ecmwf_ifs025":  []any{500.0, 600.0},
					"temperature_2m_ecmwf_ifs025":            []any{20.0, 22.0},
					"snowfall_ecmwf_ifs025":                  []any{0.0, 0.0},
					"snow_depth_ecmwf_ifs025":                []any{0.0, 0.0},
					"cloud_cover_ecmwf_ifs025":               []any{10.0, 20.0},
					"shortwave_radiation_icon_seamless":      []any{400.0, 500.0},
					"diffuse_radiation_icon_seamless":        []any{100.0, 120.0},
					"direct_normal_irradiance_icon_seamless": []any{500.0, 600.0},
					"temperature_2m_icon_seamless":           []any{20.0, 22.0},
					"snowfall_icon_seamless":                 []any{0.0, 0.0},
					"snow_depth_icon_seamless":               []any{0.0, 0.0},
					"cloud_cover_icon_seamless":              []any{10.0, 20.0},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "best_match,ecmwf_ifs025,icon_seamless", r.URL.Query().Get("models"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockBody)
			}))
			defer ts.Close()

			forecastURL, parseErr := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, parseErr)
			s := &OpenMeteo{ForecastURL: forecastURL, HTTPClient: ts.Client()}
			res, err := s.Forecast(context.Background(), types.SiteLocation{
				CountryCode: "AU",
				Latitude:    -27.47,
				Longitude:   153.02,
				TimeZone:    nyTZ,
			}, oct31, nov1)

			require.NoError(t, err)
			if assert.Len(t, res, 1) {
				assert.Equal(t, []string{"best_match", "ecmwf_ifs025", "icon_seamless"}, res[0].Models)
				// 06:45 in -05:00 becomes 07:45 in -04:00 (EDT)
				assert.Equal(t, 7, res[0].TSSunrise.Hour())
				assert.Equal(t, 45, res[0].TSSunrise.Minute())
				if assert.Len(t, res[0].ForecastHours, 2) {
					// 11:00 in -05:00 becomes 12:00 in -04:00 (EDT)
					assert.Equal(t, 12, res[0].ForecastHours[0].TSHourStart.Hour())
					assert.Equal(t, 13, res[0].ForecastHours[1].TSHourStart.Hour())
				}
			}
		})

		t.Run("converts UTC startDate and endDate to negative-offset site timezone", func(t *testing.T) {
			laTZ := "America/Los_Angeles"
			laLoc, locErr := time.LoadLocation(laTZ)
			require.NoError(t, locErr)

			// 2026-06-02T03:00:00Z is 2026-06-01T20:00:00-07:00 in America/Los_Angeles.
			// 2026-06-03T03:00:00Z is 2026-06-02T20:00:00-07:00 in America/Los_Angeles.
			startUTC := time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC)
			endUTC := time.Date(2026, 6, 3, 3, 0, 0, 0, time.UTC)

			mockBody := weatherForecastResponse{
				Daily: struct {
					Time    []string `json:"time"`
					Sunrise []string `json:"sunrise"`
					Sunset  []string `json:"sunset"`
				}{
					Time:    []string{"2026-06-01", "2026-06-02"},
					Sunrise: []string{"2026-06-01T05:42", "2026-06-02T05:42"},
					Sunset:  []string{"2026-06-01T20:00", "2026-06-02T20:01"},
				},
				Hourly: struct {
					Time                   []string  `json:"time"`
					ShortwaveRadiation     []float64 `json:"shortwave_radiation"`
					DiffuseRadiation       []float64 `json:"diffuse_radiation"`
					DirectNormalIrradiance []float64 `json:"direct_normal_irradiance"`
					TiltedRadiation        []float64 `json:"global_tilted_irradiance"`
					Temperature            []float64 `json:"temperature_2m"`
					Snowfall               []float64 `json:"snowfall"`
					SnowDepth              []float64 `json:"snow_depth"`
					CloudCover             []float64 `json:"cloud_cover"`
				}{
					Time:                   []string{"2026-06-01T12:00", "2026-06-02T12:00"},
					ShortwaveRadiation:     []float64{500.0, 600.0},
					DiffuseRadiation:       []float64{100.0, 120.0},
					DirectNormalIrradiance: []float64{700.0, 800.0},
					Temperature:            []float64{22.0, 24.0},
					Snowfall:               []float64{0.0, 0.0},
					SnowDepth:              []float64{0.0, 0.0},
					CloudCover:             []float64{10.0, 20.0},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				assert.Equal(t, "2026-06-01", query.Get("start_date"))
				assert.Equal(t, "2026-06-02", query.Get("end_date"))
				assert.Equal(t, laTZ, query.Get("timezone"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockBody)
			}))
			defer ts.Close()

			forecastURL, parseErr := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, parseErr)
			s := &OpenMeteo{ForecastURL: forecastURL, HTTPClient: ts.Client()}
			res, err := s.Forecast(context.Background(), types.SiteLocation{
				Latitude:  34.0,
				Longitude: -118.0,
				TimeZone:  laTZ,
			}, startUTC, endUTC)

			require.NoError(t, err)
			if assert.Len(t, res, 2) {
				assert.True(t, res[0].TSDayStart.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, laLoc)))
				assert.True(t, res[1].TSDayStart.Equal(time.Date(2026, 6, 2, 0, 0, 0, 0, laLoc)))
				assert.Len(t, res[0].ForecastHours, 1)
				assert.Len(t, res[1].ForecastHours, 1)
			}
		})

		t.Run("converts UTC startDate and endDate to positive-offset site timezone", func(t *testing.T) {
			brisTZ := "Australia/Brisbane"
			brisLoc, locErr := time.LoadLocation(brisTZ)
			require.NoError(t, locErr)

			// 2026-06-01T14:00:00Z is 2026-06-02T00:00:00+10:00 in Australia/Brisbane.
			// 2026-06-02T14:00:00Z is 2026-06-03T00:00:00+10:00 in Australia/Brisbane.
			startUTC := time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC)
			endUTC := time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC)

			mockBody := weatherForecastResponse{
				Daily: struct {
					Time    []string `json:"time"`
					Sunrise []string `json:"sunrise"`
					Sunset  []string `json:"sunset"`
				}{
					Time:    []string{"2026-06-02", "2026-06-03"},
					Sunrise: []string{"2026-06-02T06:30", "2026-06-03T06:31"},
					Sunset:  []string{"2026-06-02T17:01", "2026-06-03T17:01"},
				},
				Hourly: struct {
					Time                   []string  `json:"time"`
					ShortwaveRadiation     []float64 `json:"shortwave_radiation"`
					DiffuseRadiation       []float64 `json:"diffuse_radiation"`
					DirectNormalIrradiance []float64 `json:"direct_normal_irradiance"`
					TiltedRadiation        []float64 `json:"global_tilted_irradiance"`
					Temperature            []float64 `json:"temperature_2m"`
					Snowfall               []float64 `json:"snowfall"`
					SnowDepth              []float64 `json:"snow_depth"`
					CloudCover             []float64 `json:"cloud_cover"`
				}{
					Time:                   []string{"2026-06-02T12:00", "2026-06-03T00:00"},
					ShortwaveRadiation:     []float64{450.0, 0.0},
					DiffuseRadiation:       []float64{90.0, 0.0},
					DirectNormalIrradiance: []float64{600.0, 0.0},
					Temperature:            []float64{21.0, 15.0},
					Snowfall:               []float64{0.0, 0.0},
					SnowDepth:              []float64{0.0, 0.0},
					CloudCover:             []float64{15.0, 5.0},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				assert.Equal(t, "2026-06-02", query.Get("start_date"))
				assert.Equal(t, "2026-06-03", query.Get("end_date"))
				assert.Equal(t, brisTZ, query.Get("timezone"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockBody)
			}))
			defer ts.Close()

			forecastURL, parseErr := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, parseErr)
			s := &OpenMeteo{ForecastURL: forecastURL, HTTPClient: ts.Client()}
			res, err := s.Forecast(context.Background(), types.SiteLocation{
				CountryCode: "AU",
				Latitude:    -27.47,
				Longitude:   153.02,
				TimeZone:    brisTZ,
			}, startUTC, endUTC)

			require.NoError(t, err)
			if assert.Len(t, res, 1) {
				assert.True(t, res[0].TSDayStart.Equal(time.Date(2026, 6, 2, 0, 0, 0, 0, brisLoc)))
				assert.Len(t, res[0].ForecastHours, 1)
			}
		})
	})

	t.Run("APIKey", func(t *testing.T) {
		apiKey := "test-api-key"

		t.Run("Location with APIKey", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, apiKey, r.URL.Query().Get("apikey"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(geocodingResponse{
					Results: []struct {
						Name        string  `json:"name"`
						Latitude    float64 `json:"latitude"`
						Longitude   float64 `json:"longitude"`
						CountryCode string  `json:"country_code"`
						Timezone    string  `json:"timezone"`
						Elevation   float64 `json:"elevation"`
						Population  int     `json:"population"`
					}{
						{
							Name:        "Beverly Hills",
							CountryCode: "US",
							Latitude:    34.0736,
							Longitude:   -118.4004,
							Timezone:    "America/Los_Angeles",
							Elevation:   79,
							Population:  34000,
						},
					},
				})
			}))
			defer ts.Close()

			geoURL, err := url.Parse(ts.URL + "/v1/search")
			require.NoError(t, err)

			s := &OpenMeteo{
				GeocodingURL: geoURL,
				HTTPClient:   ts.Client(),
				APIKey:       apiKey,
			}

			_, err = s.Location(context.Background(), "US", "90210")
			require.NoError(t, err)
		})

		t.Run("Forecast with APIKey", func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, apiKey, r.URL.Query().Get("apikey"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(weatherForecastResponse{})
			}))
			defer ts.Close()

			forecastURL, err := url.Parse(ts.URL + "/v1/forecast")
			require.NoError(t, err)

			s := &OpenMeteo{
				ForecastURL: forecastURL,
				HTTPClient:  ts.Client(),
				APIKey:      apiKey,
			}

			timezone := "America/Los_Angeles"
			loc, err := time.LoadLocation(timezone)
			require.NoError(t, err)
			targetDay := time.Now().In(loc)
			startDay := targetDay.AddDate(0, 0, -1)
			endDay := targetDay.AddDate(0, 0, 1)

			_, err = s.Forecast(context.Background(), types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: timezone}, startDay, endDay)
			require.NoError(t, err)
		})

		t.Run("Location transport error redacts APIKey and preserves Unwrap", func(t *testing.T) {
			secretKey := "FAKE_OPENMETEO_KEY_MARKER_8822"
			closedTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			closedURL := closedTS.URL
			closedClient := closedTS.Client()
			closedTS.Close()

			geoURL, err := url.Parse(closedURL + "/v1/search")
			require.NoError(t, err)

			s := &OpenMeteo{
				GeocodingURL: geoURL,
				HTTPClient:   closedClient,
				APIKey:       secretKey,
			}

			_, err = s.Location(context.Background(), "US", "60601")
			require.Error(t, err)
			assert.NotContains(t, err.Error(), secretKey)
			assert.Contains(t, err.Error(), "apikey=REDACTED")
			assert.True(t, isTransientOpenMeteoErr(err))

			_, err = s.Location(context.Background(), "US", strings.Repeat("%", 400000))
			require.ErrorIs(t, err, ErrInvalidLocation)
			assert.NotContains(t, err.Error(), secretKey)
		})

		t.Run("Forecast transport error redacts APIKey and preserves Unwrap", func(t *testing.T) {
			secretKey := "FAKE_OPENMETEO_KEY_MARKER_8822"
			slowTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			}))
			defer slowTS.Close()

			forecastURL, err := url.Parse(slowTS.URL + "/v1/forecast")
			require.NoError(t, err)

			s := &OpenMeteo{
				ForecastURL: forecastURL,
				HTTPClient:  slowTS.Client(),
				APIKey:      secretKey,
			}

			timezone := "America/Los_Angeles"
			loc, err := time.LoadLocation(timezone)
			require.NoError(t, err)
			targetDay := time.Now().In(loc)
			startDay := targetDay.AddDate(0, 0, -1)
			endDay := targetDay.AddDate(0, 0, 1)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			_, err = s.Forecast(ctx, types.SiteLocation{Latitude: 34.0, Longitude: -118.0, TimeZone: timezone}, startDay, endDay)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), secretKey)
			assert.Contains(t, err.Error(), "apikey=REDACTED")
			assert.True(t, errors.Is(err, context.DeadlineExceeded))
			assert.True(t, isTransientOpenMeteoErr(err))
		})
	})

	t.Run("Integration_RealAPI_GetLocationData", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}
		if os.Getenv("RATERUDDER_LIVE_API_TESTS") != "1" {
			t.Skip("Skipping integration test: set RATERUDDER_LIVE_API_TESTS=1 to run")
		}

		geoURL, parseErr := url.Parse("https://geocoding-api.open-meteo.com/v1/search")
		require.NoError(t, parseErr)
		s := &OpenMeteo{
			GeocodingURL: geoURL,
			HTTPClient:   common.HTTPClient(25 * time.Second),
		}
		loc, err := retryOpenMeteoIntegration(t, func() (types.SiteLocation, error) {
			return s.Location(context.Background(), "US", "90210")
		})
		require.NoError(t, err)
		require.NotNil(t, loc)
		assert.Equal(t, "Beverly Hills", loc.City)
		assert.Equal(t, "US", loc.CountryCode)
		assert.Equal(t, "90210", loc.PostalCode)
		assert.InDelta(t, 34.07, loc.Latitude, 0.1)
		assert.InDelta(t, -118.40, loc.Longitude, 0.1)
		assert.Equal(t, "America/Los_Angeles", loc.TimeZone)
	})

	t.Run("Integration_RealAPI_FetchWeatherForecast", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}
		if os.Getenv("RATERUDDER_LIVE_API_TESTS") != "1" {
			t.Skip("Skipping integration test: set RATERUDDER_LIVE_API_TESTS=1 to run")
		}

		loc, err := time.LoadLocation("America/Los_Angeles")
		require.NoError(t, err)
		targetDay := time.Now().In(loc)
		startDay := targetDay.AddDate(0, 0, -1)
		endDay := targetDay.AddDate(0, 0, 1)

		forecastURL, parseErr := url.Parse("https://api.open-meteo.com/v1/forecast")
		require.NoError(t, parseErr)
		s := &OpenMeteo{
			ForecastURL: forecastURL,
			HTTPClient:  common.HTTPClient(25 * time.Second),
		}

		weathers, err := retryOpenMeteoIntegration(t, func() ([]types.Weather, error) {
			return s.Forecast(context.Background(), types.SiteLocation{Latitude: 34.07, Longitude: -118.40, TimeZone: "America/Los_Angeles"}, startDay, endDay)
		})

		require.NoError(t, err)
		require.GreaterOrEqual(t, len(weathers), 2)
		assert.Equal(t, []string{"ncep_nbm_conus", "ecmwf_ifs025", "icon_seamless"}, weathers[0].Models)
		if assert.Len(t, weathers[0].ModelForecasts, 3) {
			assert.Len(t, weathers[0].ModelForecasts["ncep_nbm_conus"], len(weathers[0].ForecastHours))
			assert.Len(t, weathers[0].ModelForecasts["ecmwf_ifs025"], len(weathers[0].ForecastHours))
			assert.Len(t, weathers[0].ModelForecasts["icon_seamless"], len(weathers[0].ForecastHours))
		}
	})
}

func TestParseModelHourlySeries(t *testing.T) {
	ctx := context.Background()
	model := "ecmwf_ifs025"
	suffix := "_" + model

	newValidHourlyMap := func() map[string]json.RawMessage {
		return map[string]json.RawMessage{
			"shortwave_radiation" + suffix:      json.RawMessage(`[100.0, 200.0]`),
			"diffuse_radiation" + suffix:        json.RawMessage(`[20.0, 40.0]`),
			"direct_normal_irradiance" + suffix: json.RawMessage(`[300.0, 400.0]`),
			"global_tilted_irradiance" + suffix: json.RawMessage(`[150.0, 250.0]`),
			"temperature_2m" + suffix:           json.RawMessage(`[18.5, 20.5]`),
			"snowfall" + suffix:                 json.RawMessage(`[0.0, 0.5]`),
			"snow_depth" + suffix:               json.RawMessage(`[0.1, 0.2]`),
			"cloud_cover" + suffix:              json.RawMessage(`[10.0, 50.0]`),
		}
	}

	t.Run("valid", func(t *testing.T) {
		ms, err := parseModelHourlySeries(ctx, newValidHourlyMap(), model, suffix, 2)
		require.NoError(t, err)
		assert.Equal(t, model, ms.model)
		assert.Len(t, ms.shortwaveRadiation, 2)
		assert.Len(t, ms.diffuseRadiation, 2)
		assert.Len(t, ms.directNormalIrradiance, 2)
		assert.Len(t, ms.tiltedRadiation, 2)
		assert.Len(t, ms.temperature, 2)
		assert.Len(t, ms.snowfall, 2)
		assert.Len(t, ms.snowDepth, 2)
		assert.Len(t, ms.cloudCover, 2)
	})

	t.Run("decode shortwave_radiation error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["shortwave_radiation"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode shortwave_radiation"+suffix)
	})

	t.Run("shortwave_radiation length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["shortwave_radiation"+suffix] = json.RawMessage(`[100.0]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 shortwave radiation")
	})

	t.Run("decode diffuse_radiation error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["diffuse_radiation"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode diffuse_radiation"+suffix)
	})

	t.Run("diffuse_radiation length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["diffuse_radiation"+suffix] = json.RawMessage(`[20.0]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 diffuse radiation")
	})

	t.Run("decode direct_normal_irradiance error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["direct_normal_irradiance"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode direct_normal_irradiance"+suffix)
	})

	t.Run("direct_normal_irradiance length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["direct_normal_irradiance"+suffix] = json.RawMessage(`[300.0]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 direct normal irradiance")
	})

	t.Run("decode global_tilted_irradiance error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["global_tilted_irradiance"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode global_tilted_irradiance"+suffix)
	})

	t.Run("decode temperature_2m error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["temperature_2m"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode temperature_2m"+suffix)
	})

	t.Run("temperature_2m length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["temperature_2m"+suffix] = json.RawMessage(`[18.5]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 temperature")
	})

	t.Run("decode snowfall error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["snowfall"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode snowfall"+suffix)
	})

	t.Run("snowfall length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["snowfall"+suffix] = json.RawMessage(`[0.0]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 snowfall")
	})

	t.Run("decode snow_depth error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["snow_depth"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode snow_depth"+suffix)
	})

	t.Run("snow_depth length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["snow_depth"+suffix] = json.RawMessage(`[0.1]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 snow depth")
	})

	t.Run("decode cloud_cover error", func(t *testing.T) {
		m := newValidHourlyMap()
		m["cloud_cover"+suffix] = json.RawMessage(`"bad"`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "failed to decode cloud_cover"+suffix)
	})

	t.Run("cloud_cover length mismatch", func(t *testing.T) {
		m := newValidHourlyMap()
		m["cloud_cover"+suffix] = json.RawMessage(`[10.0]`)
		_, err := parseModelHourlySeries(ctx, m, model, suffix, 2)
		assert.ErrorContains(t, err, "hourly data mismatch: 2 times, 1 cloud cover")
	})
}
