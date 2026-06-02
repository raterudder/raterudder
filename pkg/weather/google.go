package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

// GoogleGeocodingWrapper wraps a Service and uses Google APIs for location geocoding.
type GoogleGeocodingWrapper struct {
	OpenMeteo    Service
	APIKey       string
	GeocodingURL *url.URL
	TimezoneURL  *url.URL
	HTTPClient   *http.Client
}

type googleGeocodingResponse struct {
	Results []googleGeocodingResult `json:"results"`
}

type googleGeocodingResult struct {
	PostalAddress googleGeocodingPostalAddress `json:"postalAddress"`
	Location      googleGeocodingLocation      `json:"location"`
}

type googleGeocodingPostalAddress struct {
	Locality           string `json:"locality"`
	Sublocality        string `json:"sublocality"`
	AdministrativeArea string `json:"administrativeArea"`
}

type googleGeocodingLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type googleTimezoneResponse struct {
	Status     string `json:"status"`
	TimeZoneID string `json:"timeZoneId"`
}

// Location queries the Google Geocoding and Time Zone APIs to find a location based on zip code and country code.
func (w *GoogleGeocodingWrapper) Location(ctx context.Context, countryCode, postalCode string) (types.SiteLocation, error) {
	if countryCode == "" || postalCode == "" {
		return types.SiteLocation{}, fmt.Errorf("country code and zip code are required")
	}

	lat, lng, city, err := w.geocode(ctx, countryCode, postalCode)
	if err != nil {
		return types.SiteLocation{}, llog.ErrWithKV(err, llog.KV{
			"postalCode":  postalCode,
			"countryCode": countryCode,
		})
	}

	timezone, err := w.fetchTimezone(ctx, lat, lng)
	if err != nil {
		return types.SiteLocation{}, llog.ErrWithKV(err, llog.KV{
			"postalCode":  postalCode,
			"countryCode": countryCode,
			"latitude":    lat,
			"longitude":   lng,
			"city":        city,
		})
	}

	log.Ctx(ctx).Debug("found location",
		slog.String("postalCode", postalCode),
		slog.String("countryCode", countryCode),
		slog.Float64("latitude", lat),
		slog.Float64("longitude", lng),
		slog.String("city", city),
		slog.String("timezone", timezone),
	)

	return types.SiteLocation{
		PostalCode:  postalCode,
		CountryCode: countryCode,
		Latitude:    lat,
		Longitude:   lng,
		City:        city,
		TimeZone:    timezone,
		// we don't support getting the elevation
	}, nil
}

func (w *GoogleGeocodingWrapper) geocode(ctx context.Context, countryCode, postalCode string) (float64, float64, string, error) {
	u := *w.GeocodingURL

	q := u.Query()
	q.Set("address.postalCode", postalCode)
	q.Set("address.regionCode", countryCode)
	q.Set("fields", "results.location,results.postalAddress")
	q.Set("key", w.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to create geocoding request: %w", err)
	}

	resp, err := w.HTTPClient.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData struct {
			Error struct {
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errData); err == nil && errData.Error.Message != "" {
			log.Ctx(ctx).Warn("google geocoding api returned error",
				slog.String("message", errData.Error.Message),
				slog.String("status", errData.Error.Status),
				slog.String("postalCode", postalCode),
				slog.String("countryCode", countryCode),
			)
			return 0, 0, "", fmt.Errorf("google geocoding api returned error: %s (%s)", errData.Error.Message, errData.Error.Status)
		} else {
			log.Ctx(ctx).Error("geocoding api returned non-ok status",
				slog.Int("statusCode", resp.StatusCode),
				slog.String("postalCode", postalCode),
				slog.String("countryCode", countryCode),
			)
		}
		return 0, 0, "", fmt.Errorf("geocoding api returned status %d", resp.StatusCode)
	}

	var data googleGeocodingResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Ctx(ctx).Error("failed to decode geocoding response",
			slog.Any("error", err),
			slog.String("postalCode", postalCode),
			slog.String("countryCode", countryCode),
		)
		return 0, 0, "", fmt.Errorf("failed to decode geocoding response: %w", err)
	}

	if len(data.Results) == 0 {
		log.Ctx(ctx).Warn("no location found for zip code and country code",
			slog.String("postalCode", postalCode),
			slog.String("countryCode", countryCode),
		)
		return 0, 0, "", fmt.Errorf("no location found for zip code %s and country code %s", postalCode, countryCode)
	}

	log.Ctx(ctx).Debug("found geocoding results",
		slog.String("postalCode", postalCode),
		slog.String("countryCode", countryCode),
		slog.Any("results", data.Results),
	)

	result := data.Results[0]
	lat := result.Location.Latitude
	lng := result.Location.Longitude
	city := result.PostalAddress.Locality

	return lat, lng, city, nil
}

func (w *GoogleGeocodingWrapper) fetchTimezone(ctx context.Context, lat, lng float64) (string, error) {
	u := *w.TimezoneURL

	tzQ := u.Query()
	tzQ.Set("location", fmt.Sprintf("%f,%f", lat, lng))
	tzQ.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	tzQ.Set("key", w.APIKey)
	u.RawQuery = tzQ.Encode()

	tzReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create timezone request: %w", err)
	}

	tzResp, err := w.HTTPClient.Do(tzReq)
	if err != nil {
		return "", fmt.Errorf("timezone request failed: %w", err)
	}
	defer tzResp.Body.Close()

	if tzResp.StatusCode != http.StatusOK {
		log.Ctx(ctx).Error("timezone api returned non-ok status",
			slog.Int("statusCode", tzResp.StatusCode),
			slog.Float64("latitude", lat),
			slog.Float64("longitude", lng),
		)
		return "", fmt.Errorf("timezone api returned status %d", tzResp.StatusCode)
	}

	var tzData googleTimezoneResponse
	if err := json.NewDecoder(tzResp.Body).Decode(&tzData); err != nil {
		log.Ctx(ctx).Error("failed to decode timezone response",
			slog.Any("error", err),
			slog.Float64("latitude", lat),
			slog.Float64("longitude", lng),
		)
		return "", fmt.Errorf("failed to decode timezone response: %w", err)
	}

	if tzData.Status != "OK" {
		log.Ctx(ctx).Error("timezone api returned non-ok status",
			slog.String("status", tzData.Status),
			slog.Float64("latitude", lat),
			slog.Float64("longitude", lng),
		)
		return "", fmt.Errorf("google timezone api returned status %s", tzData.Status)
	}

	return tzData.TimeZoneID, nil
}

// Forecast forwards the call directly to OpenMeteo forecast API.
func (w *GoogleGeocodingWrapper) Forecast(ctx context.Context, loc types.SiteLocation, startDate, endDate time.Time) ([]types.Weather, error) {
	return w.OpenMeteo.Forecast(ctx, loc, startDate, endDate)
}
