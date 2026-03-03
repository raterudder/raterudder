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

func TestGetLocationData(t *testing.T) {
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
				s := NewService(Config{GeocodingURL: ts.URL + "/v1/search", HTTPClient: ts.Client()})
				loc, err = s.GetLocationData(context.Background(), tc.countryCode, tc.postalCode)
			} else {
				s := NewService(Config{})
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
}

func TestFetchWeatherForecast(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	targetDay := time.Date(2023, 10, 15, 12, 0, 0, 0, loc)

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
					Time:    []string{"2023-10-14", "2023-10-16"},
					Sunrise: []string{"2023-10-14T06:00", "2023-10-16T06:00"},
					Sunset:  []string{"2023-10-14T18:00", "2023-10-16T18:00"},
				},
				Hourly: struct {
					Time               []string  `json:"time"`
					ShortwaveRadiation []float64 `json:"shortwave_radiation"`
				}{
					Time:               []string{"2023-10-14T10:00", "2023-10-16T12:00"},
					ShortwaveRadiation: []float64{100.5, 200.5},
				},
			},
			wantErr:   false,
			wantCount: 2,
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
				s := NewService(Config{ForecastURL: ts.URL + "/v1/forecast", HTTPClient: ts.Client()})
				res, err = s.FetchWeatherForecast(context.Background(), 34.0, -118.0, tc.timezone, targetDay)
			} else {
				s := NewService(Config{})
				res, err = s.FetchWeatherForecast(context.Background(), 34.0, -118.0, tc.timezone, targetDay)
			}

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, res, tc.wantCount)
			}
		})
	}
}
