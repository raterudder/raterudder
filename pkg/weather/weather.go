package weather

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	lflag "github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// MaxCountryCodeLength is the maximum length of an ISO country code (alpha-2 or alpha-3).
	MaxCountryCodeLength = 3
	// MaxPostalCodeLength is the maximum length of an international postal code.
	MaxPostalCodeLength = 20
)

var (
	// ErrInvalidLocation indicates that a country code or postal code failed input validation.
	ErrInvalidLocation = errors.New("invalid location")
	// ErrNoLocationFound indicates that geocoding returned no matching location.
	ErrNoLocationFound = errors.New("no location found")

	secretQueryParamPattern = regexp.MustCompile(`(?i)([?&;](?:key|apikey)=)[^&#\s"]*`)
)

// Service provides weather data mapping and API handling.
type Service interface {
	Location(ctx context.Context, countryCode, postalCode string) (types.SiteLocation, error)
	Forecast(ctx context.Context, loc types.SiteLocation, startDate, endDate time.Time) ([]types.Weather, error)
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// ValidateLocationInput validates non-empty countryCode and postalCode values,
// rejecting oversized inputs, control characters, and unsupported punctuation
// while allowing international alphanumeric postal codes with spaces or hyphens.
func ValidateLocationInput(countryCode, postalCode string) error {
	if countryCode != "" {
		if len(countryCode) > MaxCountryCodeLength {
			return fmt.Errorf("%w: country code must be %d characters or fewer", ErrInvalidLocation, MaxCountryCodeLength)
		}
		for _, r := range countryCode {
			if unicode.IsControl(r) {
				return fmt.Errorf("%w: country code cannot contain control characters", ErrInvalidLocation)
			}
			if !isASCIILetter(r) {
				return fmt.Errorf("%w: country code must contain only letters", ErrInvalidLocation)
			}
		}
		if len(countryCode) < 2 {
			return fmt.Errorf("%w: country code must be at least 2 characters", ErrInvalidLocation)
		}
	}

	if postalCode != "" {
		if len(postalCode) > MaxPostalCodeLength {
			return fmt.Errorf("%w: postal code must be %d characters or fewer", ErrInvalidLocation, MaxPostalCodeLength)
		}
		hasAlnum := false
		for _, r := range postalCode {
			if unicode.IsControl(r) {
				return fmt.Errorf("%w: postal code cannot contain control characters", ErrInvalidLocation)
			}
			if isASCIILetter(r) || isASCIIDigit(r) {
				hasAlnum = true
				continue
			}
			if r == ' ' || r == '-' {
				continue
			}
			return fmt.Errorf("%w: postal code contains invalid characters", ErrInvalidLocation)
		}
		if !hasAlnum {
			return fmt.Errorf("%w: postal code must contain letters or numbers", ErrInvalidLocation)
		}
	}

	return nil
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string {
	return e.msg
}

func (e *redactedError) Unwrap() error {
	return e.err
}

type redactedMultiError struct {
	msg  string
	errs []error
}

func (e *redactedMultiError) Error() string {
	return e.msg
}

func (e *redactedMultiError) Unwrap() []error {
	return e.errs
}

func redactQuerySecrets(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		subParts := strings.Split(part, ";")
		for j, sub := range subParts {
			name, _, hasEq := strings.Cut(sub, "=")
			checkName := name
			if unescaped, err := url.QueryUnescape(name); err == nil {
				checkName = unescaped
			}
			if strings.EqualFold(checkName, "key") || strings.EqualFold(checkName, "apikey") {
				if hasEq {
					subParts[j] = name + "=REDACTED"
				} else {
					subParts[j] = "REDACTED"
				}
			}
		}
		parts[i] = strings.Join(subParts, ";")
	}
	return strings.Join(parts, "&")
}

func redactAPIKeyLiterals(s, apiKey string) string {
	if s == "" || apiKey == "" {
		return s
	}
	if qEsc := url.QueryEscape(apiKey); qEsc != "" && qEsc != apiKey {
		s = strings.ReplaceAll(s, qEsc, "REDACTED")
	}
	if pEsc := url.PathEscape(apiKey); pEsc != "" && pEsc != apiKey {
		s = strings.ReplaceAll(s, pEsc, "REDACTED")
	}
	return strings.ReplaceAll(s, apiKey, "REDACTED")
}

func redactSecretURL(rawURL, apiKey string) string {
	if rawURL == "" {
		return ""
	}
	out := rawURL
	if parsed, err := url.Parse(rawURL); err == nil && parsed != nil {
		if parsed.User != nil {
			if _, hasPwd := parsed.User.Password(); hasPwd {
				parsed.User = url.UserPassword(parsed.User.Username(), "REDACTED")
			}
		}
		if parsed.RawQuery != "" {
			parsed.RawQuery = redactQuerySecrets(parsed.RawQuery)
		}
		out = parsed.String()
	}
	out = secretQueryParamPattern.ReplaceAllString(out, "${1}REDACTED")
	return redactAPIKeyLiterals(out, apiKey)
}

func redactSecretText(s, apiKey string) string {
	if s == "" {
		return ""
	}
	out := secretQueryParamPattern.ReplaceAllString(s, "${1}REDACTED")
	return redactAPIKeyLiterals(out, apiKey)
}

func redactRequestError(err error, apiKey string) error {
	if err == nil {
		return nil
	}
	if uErr, ok := err.(*url.Error); ok {
		return &url.Error{
			Op:  uErr.Op,
			URL: redactSecretURL(uErr.URL, apiKey),
			Err: redactRequestError(uErr.Err, apiKey),
		}
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		origErrs := multi.Unwrap()
		sanitizedErrs := make([]error, len(origErrs))
		changed := false
		for i, sub := range origErrs {
			sanitizedErrs[i] = redactRequestError(sub, apiKey)
			if sanitizedErrs[i] != sub {
				changed = true
			}
		}
		redactedMsg := redactSecretText(err.Error(), apiKey)
		if changed || redactedMsg != err.Error() {
			return &redactedMultiError{
				msg:  redactedMsg,
				errs: sanitizedErrs,
			}
		}
		return err
	}
	inner := errors.Unwrap(err)
	sanitizedInner := redactRequestError(inner, apiKey)
	redactedMsg := redactSecretText(err.Error(), apiKey)
	if redactedMsg != err.Error() || sanitizedInner != inner {
		unwrapTarget := err
		if inner != nil {
			unwrapTarget = sanitizedInner
		}
		return &redactedError{
			msg: redactedMsg,
			err: unwrapTarget,
		}
	}
	return err
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
