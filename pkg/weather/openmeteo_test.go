package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenMeteoService(t *testing.T) {
	t.Run("GetLocationData", func(t *testing.T) {
		tests := []struct {
			name        string
			countryCode string
			postalCode  string
			mockStatus  int
			mockBody    any
			wantErr     bool
			wantLoc     *types.SiteLocation
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
				wantLoc: &types.SiteLocation{
					PostalCode:  "90210",
					CountryCode: "US",
					Lat:         34.0736,
					Long:        -118.4004,
					City:        "Beverly Hills",
					TimeZone:    "America/Los_Angeles",
					Elevation:   79,
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var loc *types.SiteLocation
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
					s := &OpenMeteo{GeocodingURL: ts.URL + "/v1/search", HTTPClient: ts.Client()}
					loc, err = s.GetLocationData(context.Background(), tc.countryCode, tc.postalCode)
				} else {
					s := &OpenMeteo{}
					loc, err = s.GetLocationData(context.Background(), tc.countryCode, tc.postalCode)
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

	t.Run("FetchWeatherForecast", func(t *testing.T) {
		loc, _ := time.LoadLocation("America/Los_Angeles")
		targetDay := time.Now().In(loc)
		startDay := targetDay.AddDate(0, 0, -1)
		endDay := targetDay.AddDate(0, 0, 1)

		tests := []struct {
			name       string
			timezone   string
			mockStatus int
			mockBody   any
			wantErr    bool
			wantCount  int
		}{
			{
				name:     "invalid timezone",
				timezone: "Invalid/Zone",
				wantErr:  true,
			},
			{
				name:       "api error",
				timezone:   "America/Los_Angeles",
				mockStatus: http.StatusInternalServerError,
				wantErr:    true,
			},
			{
				name:       "success",
				timezone:   "America/Los_Angeles",
				mockStatus: http.StatusOK,
				mockBody: weatherForecastResponse{
					Daily: struct {
						Time    []string `json:"time"`
						Sunrise []string `json:"sunrise"`
						Sunset  []string `json:"sunset"`
					}{
						Time:    []string{startDay.Format("2006-01-02"), endDay.Format("2006-01-02")},
						Sunrise: []string{startDay.Format("2006-01-02") + "T06:00", endDay.Format("2006-01-02") + "T06:00"},
						Sunset:  []string{startDay.Format("2006-01-02") + "T18:00", endDay.Format("2006-01-02") + "T18:00"},
					},
					Hourly: struct {
						Time               []string  `json:"time"`
						ShortwaveRadiation []float64 `json:"shortwave_radiation"`
					}{
						Time:               []string{startDay.Format("2006-01-02") + "T10:00", endDay.Format("2006-01-02") + "T12:00"},
						ShortwaveRadiation: []float64{100.5, 200.5},
					},
				},
				wantErr:   false,
				wantCount: 3,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var res []types.Weather
				var err error
				if tc.mockBody != nil || tc.mockStatus != 0 {
					ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.WriteHeader(tc.mockStatus)
						json.NewEncoder(w).Encode(tc.mockBody)
					}))
					defer ts.Close()
					s := &OpenMeteo{ForecastURL: ts.URL + "/v1/forecast", HTTPClient: ts.Client()}
					res, err = s.FetchWeatherForecast(context.Background(), 34.0, -118.0, tc.timezone, startDay, endDay)
				} else {
					s := &OpenMeteo{}
					res, err = s.FetchWeatherForecast(context.Background(), 34.0, -118.0, tc.timezone, startDay, endDay)
				}

				if tc.wantErr {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Len(t, res, tc.wantCount)

					// Assert the correct splitting into Actuals vs Forecasts based on time.Now() rule logic
					// startDay is past so it must be actual
					assert.Len(t, res[0].ActualHours, 1)
					assert.Equal(t, 100.5, res[0].ActualHours[0].GHI)
					assert.Len(t, res[0].ForecastHours, 0)

					// endDay is future so it must be forecast
					assert.Len(t, res[2].ForecastHours, 1)
					assert.Equal(t, 200.5, res[2].ForecastHours[0].GHI)
					assert.Len(t, res[2].ActualHours, 0)
				}
			})
		}
	})

	t.Run("Integration_RealAPI_GetLocationData", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		s := &OpenMeteo{
			GeocodingURL: "https://geocoding-api.open-meteo.com/v1/search",
			HTTPClient:   &http.Client{Timeout: 10 * time.Second},
		}
		loc, err := s.GetLocationData(context.Background(), "US", "90210")
		require.NoError(t, err)
		require.NotNil(t, loc)
		assert.Equal(t, "Beverly Hills", loc.City)
		assert.Equal(t, "US", loc.CountryCode)
		assert.Equal(t, "90210", loc.PostalCode)
		assert.InDelta(t, 34.07, loc.Lat, 0.1)
		assert.InDelta(t, -118.40, loc.Long, 0.1)
		assert.Equal(t, "America/Los_Angeles", loc.TimeZone)
	})

	t.Run("Integration_RealAPI_FetchWeatherForecast", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		loc, _ := time.LoadLocation("America/Los_Angeles")
		targetDay := time.Now().In(loc)
		startDay := targetDay.AddDate(0, 0, -1)
		endDay := targetDay.AddDate(0, 0, 1)

		s := &OpenMeteo{
			ForecastURL: "https://api.open-meteo.com/v1/forecast",
			HTTPClient:  &http.Client{Timeout: 10 * time.Second},
		}
		weathers, err := s.FetchWeatherForecast(context.Background(), 34.07, -118.40, "America/Los_Angeles", startDay, endDay)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(weathers), 2)
	})
}
