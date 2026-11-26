package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockForecastService struct {
	Service
	CalledForecast bool
	Loc            types.SiteLocation
}

func (m *mockForecastService) Forecast(ctx context.Context, loc types.SiteLocation, startDate, endDate time.Time) ([]types.Weather, error) {
	m.CalledForecast = true
	m.Loc = loc
	return []types.Weather{{Latitude: loc.Latitude, Longitude: loc.Longitude}}, nil
}

func TestGoogleGeocodingWrapper(t *testing.T) {
	t.Run("MissingParams", func(t *testing.T) {
		w := &GoogleGeocodingWrapper{}
		_, err := w.Location(context.Background(), "", "90210")
		assert.ErrorContains(t, err, "country code and zip code are required")

		_, err = w.Location(context.Background(), "US", "")
		assert.ErrorContains(t, err, "country code and zip code are required")
	})

	t.Run("InvalidLocationInputs", func(t *testing.T) {
		var upstreamHits atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			upstreamHits.Add(1)
			rw.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY_MARKER_9911",
			GeocodingURL: geoURL,
			HTTPClient:   ts.Client(),
		}

		_, err = w.Location(context.Background(), "US", strings.Repeat("%", 400000))
		require.ErrorIs(t, err, ErrInvalidLocation)
		assert.NotContains(t, err.Error(), "FAKE_GMAPS_KEY_MARKER_9911")

		_, err = w.Location(context.Background(), "US", strings.Repeat("9", MaxPostalCodeLength+1))
		require.ErrorIs(t, err, ErrInvalidLocation)

		_, err = w.Location(context.Background(), "US", "90210\n")
		require.ErrorIs(t, err, ErrInvalidLocation)

		_, err = w.Location(context.Background(), "U\x00", "90210")
		require.ErrorIs(t, err, ErrInvalidLocation)

		_, err = w.Location(context.Background(), "TOOLONG", "90210")
		require.ErrorIs(t, err, ErrInvalidLocation)

		_, err = w.Location(context.Background(), "US", "%%%")
		require.ErrorIs(t, err, ErrInvalidLocation)

		assert.Equal(t, int32(0), upstreamHits.Load())
	})

	t.Run("GeocodingTransportErrorRedactsAPIKey", func(t *testing.T) {
		closedTS := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
		closedURL := closedTS.URL
		closedClient := closedTS.Client()
		closedTS.Close()

		geoURL, err := url.Parse(closedURL + "/geocode")
		require.NoError(t, err)

		const apiKey = "FAKE_GMAPS_KEY_MARKER_9911+secret/val=1"
		w := &GoogleGeocodingWrapper{
			APIKey:       apiKey,
			GeocodingURL: geoURL,
			HTTPClient:   closedClient,
		}

		_, err = w.Location(context.Background(), "US", "60601")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "FAKE_GMAPS_KEY_MARKER_9911")
		assert.NotContains(t, err.Error(), apiKey)
		assert.NotContains(t, fmt.Sprintf("%v", llog.ErrKV(err)), "FAKE_GMAPS_KEY_MARKER_9911")
		assert.Contains(t, err.Error(), "key=REDACTED")

		var netErr net.Error
		assert.ErrorAs(t, err, &netErr)
		var urlErr *url.Error
		if assert.ErrorAs(t, err, &urlErr) {
			assert.NotContains(t, urlErr.URL, "FAKE_GMAPS_KEY_MARKER_9911")
			assert.Contains(t, urlErr.URL, "key=REDACTED")
		}
	})

	t.Run("TimezoneTransportErrorRedactsAPIKey", func(t *testing.T) {
		geoTS := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			resp := googleGeocodingResponse{
				Results: []googleGeocodingResult{
					{
						Location: googleGeocodingLocation{Latitude: 41.8818, Longitude: -87.6231},
					},
				},
			}
			_ = json.NewEncoder(rw).Encode(resp)
		}))
		defer geoTS.Close()

		closedTZ := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
		closedTZURL := closedTZ.URL
		closedTZ.Close()

		geoURL, err := url.Parse(geoTS.URL + "/geocode")
		require.NoError(t, err)
		tzURL, err := url.Parse(closedTZURL + "/timezone")
		require.NoError(t, err)

		const apiKey = "FAKE_GMAPS_KEY_MARKER_9911"
		w := &GoogleGeocodingWrapper{
			APIKey:       apiKey,
			GeocodingURL: geoURL,
			TimezoneURL:  tzURL,
			HTTPClient:   geoTS.Client(),
		}

		_, err = w.Location(context.Background(), "US", "60601")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), apiKey)
		assert.NotContains(t, fmt.Sprintf("%v", llog.ErrKV(err)), apiKey)
		assert.Contains(t, err.Error(), "key=REDACTED")

		var netErr net.Error
		assert.ErrorAs(t, err, &netErr)

		// Also verify deadline exceeded unwrapping on timezone timeout
		slowTZ := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer slowTZ.Close()

		slowTZURL, err := url.Parse(slowTZ.URL + "/timezone")
		require.NoError(t, err)
		w.TimezoneURL = slowTZURL

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err = w.Location(ctx, "US", "60601")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), apiKey)
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
	})

	t.Run("GeocodingAPIError", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			HTTPClient:   ts.Client(),
		}

		_, err = w.Location(context.Background(), "US", "90210")
		assert.ErrorContains(t, err, "geocoding api returned status 500")
	})

	t.Run("GeocodingZeroResults", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			resp := googleGeocodingResponse{
				Results: nil,
			}
			json.NewEncoder(rw).Encode(resp)
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			HTTPClient:   ts.Client(),
		}

		_, err = w.Location(context.Background(), "US", "90210")
		assert.ErrorContains(t, err, "no location found for zip code 90210 and country code US")
	})

	t.Run("GeocodingStatusNotOK", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusBadRequest)
			rw.Write([]byte(`{
				"error": {
					"code": 400,
					"message": "API key not valid. Please pass a valid API key.",
					"status": "INVALID_ARGUMENT"
				}
			}`))
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			HTTPClient:   ts.Client(),
		}

		_, err = w.Location(context.Background(), "US", "90210")
		assert.ErrorContains(t, err, "google geocoding api returned error: API key not valid. Please pass a valid API key. (INVALID_ARGUMENT)")
	})

	t.Run("TimezoneAPIError", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/geocode" {
				// return v4 response
				resp := googleGeocodingResponse{
					Results: []googleGeocodingResult{
						{
							Location: struct {
								Latitude  float64 `json:"latitude"`
								Longitude float64 `json:"longitude"`
							}{Latitude: 34.0736, Longitude: -118.4004},
						},
					},
				}
				json.NewEncoder(rw).Encode(resp)
				return
			}
			if r.URL.Path == "/timezone" {
				rw.WriteHeader(http.StatusInternalServerError)
				return
			}
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)
		tzURL, err := url.Parse(ts.URL + "/timezone")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			TimezoneURL:  tzURL,
			HTTPClient:   ts.Client(),
		}

		_, err = w.Location(context.Background(), "US", "90210")
		assert.ErrorContains(t, err, "timezone api returned status 500")
	})

	t.Run("TimezoneStatusNotOK", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/geocode" {
				resp := googleGeocodingResponse{
					Results: []googleGeocodingResult{
						{
							Location: struct {
								Latitude  float64 `json:"latitude"`
								Longitude float64 `json:"longitude"`
							}{Latitude: 34.0736, Longitude: -118.4004},
						},
					},
				}
				json.NewEncoder(rw).Encode(resp)
				return
			}
			if r.URL.Path == "/timezone" {
				resp := googleTimezoneResponse{
					Status: "OVER_QUERY_LIMIT",
				}
				json.NewEncoder(rw).Encode(resp)
				return
			}
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)
		tzURL, err := url.Parse(ts.URL + "/timezone")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			TimezoneURL:  tzURL,
			HTTPClient:   ts.Client(),
		}

		_, err = w.Location(context.Background(), "US", "90210")
		assert.ErrorContains(t, err, "google timezone api returned status OVER_QUERY_LIMIT")
	})

	t.Run("Success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/geocode" {
				// Assert query params for v4: address.postalCode, address.regionCode, fields, key
				assert.Equal(t, "90210", r.URL.Query().Get("address.postalCode"))
				assert.Equal(t, "US", r.URL.Query().Get("address.regionCode"))
				assert.Equal(t, "results.location,results.postalAddress", r.URL.Query().Get("fields"))
				assert.Equal(t, "FAKE_GMAPS_KEY", r.URL.Query().Get("key"))

				resp := googleGeocodingResponse{
					Results: []googleGeocodingResult{
						{
							PostalAddress: googleGeocodingPostalAddress{
								Locality: "Beverly Hills",
							},
							Location: googleGeocodingLocation{
								Latitude:  34.0736,
								Longitude: -118.4004,
							},
						},
					},
				}
				json.NewEncoder(rw).Encode(resp)
				return
			}
			if r.URL.Path == "/timezone" {
				// Assert query params
				assert.Equal(t, "34.073600,-118.400400", r.URL.Query().Get("location"))
				assert.Equal(t, "FAKE_GMAPS_KEY", r.URL.Query().Get("key"))
				assert.NotEmpty(t, r.URL.Query().Get("timestamp"))

				resp := googleTimezoneResponse{
					Status:     "OK",
					TimeZoneID: "America/Los_Angeles",
				}
				json.NewEncoder(rw).Encode(resp)
				return
			}
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)
		tzURL, err := url.Parse(ts.URL + "/timezone")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			TimezoneURL:  tzURL,
			HTTPClient:   ts.Client(),
		}

		loc, err := w.Location(context.Background(), "US", "90210")
		require.NoError(t, err)

		assert.Equal(t, "90210", loc.PostalCode)
		assert.Equal(t, "US", loc.CountryCode)
		assert.Equal(t, 34.0736, loc.Latitude)
		assert.Equal(t, -118.4004, loc.Longitude)
		assert.Equal(t, "Beverly Hills", loc.City)
		assert.Equal(t, "America/Los_Angeles", loc.TimeZone)
		assert.Equal(t, float64(0), loc.Elevation)
	})

	t.Run("InternationalAlphanumericPostalCode", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/geocode" {
				assert.Equal(t, "SW1A 1AA", r.URL.Query().Get("address.postalCode"))
				assert.Equal(t, "GB", r.URL.Query().Get("address.regionCode"))
				_ = json.NewEncoder(rw).Encode(googleGeocodingResponse{
					Results: []googleGeocodingResult{
						{
							PostalAddress: googleGeocodingPostalAddress{Locality: "London"},
							Location:      googleGeocodingLocation{Latitude: 51.5014, Longitude: -0.1419},
						},
					},
				})
				return
			}
			if r.URL.Path == "/timezone" {
				_ = json.NewEncoder(rw).Encode(googleTimezoneResponse{
					Status:     "OK",
					TimeZoneID: "Europe/London",
				})
				return
			}
		}))
		defer ts.Close()

		geoURL, err := url.Parse(ts.URL + "/geocode")
		require.NoError(t, err)
		tzURL, err := url.Parse(ts.URL + "/timezone")
		require.NoError(t, err)

		w := &GoogleGeocodingWrapper{
			APIKey:       "FAKE_GMAPS_KEY",
			GeocodingURL: geoURL,
			TimezoneURL:  tzURL,
			HTTPClient:   ts.Client(),
		}

		loc, err := w.Location(context.Background(), "GB", "SW1A 1AA")
		require.NoError(t, err)
		assert.Equal(t, "SW1A 1AA", loc.PostalCode)
		assert.Equal(t, "GB", loc.CountryCode)
		assert.Equal(t, "Europe/London", loc.TimeZone)
	})

	t.Run("ForecastForwarding", func(t *testing.T) {
		mockOM := &mockForecastService{}
		w := &GoogleGeocodingWrapper{
			OpenMeteo: mockOM,
		}

		testLoc := types.SiteLocation{
			Latitude:  34.0736,
			Longitude: -118.4004,
		}
		res, err := w.Forecast(context.Background(), testLoc, time.Now(), time.Now().Add(24*time.Hour))
		require.NoError(t, err)

		assert.True(t, mockOM.CalledForecast)
		if assert.Len(t, res, 1) {
			assert.Equal(t, 34.0736, res[0].Latitude)
			assert.Equal(t, -118.4004, res[0].Longitude)
		}
	})
}
