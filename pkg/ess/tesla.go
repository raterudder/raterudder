package ess

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/time/rate"
)

// from: https://developer.tesla.com/docs/fleet-api/authentication/overview#scopes
const teslaScopes = "openid offline_access energy_cmds energy_device_data"

const (
	teslaExportRuleBatteryOk = "battery_ok"
	teslaExportRulePvOnly    = "pv_only"
	teslaExportRuleNever     = "never"
)

type baseTesla struct {
	clientID     string
	clientSecret string
	keyPEM       string
	pubPEM       string
	baseURLs     map[string]string
	tokenURL     string
	authURL      string
	client       *http.Client
}

func configuredBaseTesla() *baseTesla {
	teslaClientID := lflag.String("tesla-client-id", "", "Tesla Fleet API Client ID")
	teslaClientSecret := lflag.String("tesla-client-secret", "", "Tesla Fleet API Client Secret")
	teslaKeyPEM := lflag.String("tesla-key-pem", "", "Tesla private key in PEM format")
	teslaBaseURLs := map[string]string{
		// from: https://developer.tesla.com/docs/fleet-api/getting-started/regions-countries#base-urls-by-region
		"NA": "https://fleet-api.prd.na.vn.cloud.tesla.com",
		"EU": "https://fleet-api.prd.eu.vn.cloud.tesla.com",
	}
	lflag.JSON(&teslaBaseURLs, "tesla-base-urls", teslaBaseURLs, "JSON map of Region to Base URL for the Tesla API")

	b := baseTesla{
		tokenURL: "https://fleet-auth.prd.vn.cloud.tesla.com/oauth2/v3/token",
		authURL:  "https://auth.tesla.com/oauth2/v3/authorize",
		client:   common.HTTPClient(time.Minute),
	}
	lflag.Do(func() {
		b.clientID = *teslaClientID
		b.clientSecret = *teslaClientSecret
		b.keyPEM = *teslaKeyPEM

		b.baseURLs = teslaBaseURLs

		if b.clientID != "" && b.clientSecret != "" {
			if b.keyPEM == "" {
				log.Ctx(context.Background()).Error("missing tesla-key-pem")
				os.Exit(1)
			}
			var err error
			b.pubPEM, err = publicKeyPEMFromPrivate(b.keyPEM)
			if err != nil {
				log.Ctx(context.Background()).Error("failed to parse tesla-key-pem", slog.Any("error", err))
				os.Exit(1)
			}
		}
	})
	return &b
}

func publicKeyPEMFromPrivate(pk string) (string, error) {
	block, _ := pem.Decode([]byte(pk))
	if block == nil {
		return "", errors.New("failed to find PEM-encoded block")
	}

	var s crypto.Signer
	switch block.Type {
	case "RSA PRIVATE KEY":
		var err error
		s, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("failed to parse rsa private key: %w", err)
		}
	case "EC PRIVATE KEY":
		var err error
		s, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("failed to parse ec private key: %w", err)
		}
	case "PRIVATE KEY":
		anything, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("failed to parse private key: %w", err)
		}
		s = anything.(crypto.Signer)
	default:
		return "", fmt.Errorf("unknown PEM header: %v", block.Type)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(s.Public())
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
	return string(pubPEM), nil
}

func (b *baseTesla) info(ctx context.Context) types.ESSProviderInfo {
	var baseOAuthURL string
	parsed, err := url.Parse(b.authURL)
	if err != nil {
		log.Ctx(ctx).Error("failed to parse tesla auth URL", slog.Any("error", err), slog.Any("url", b.authURL))
	} else {
		q := parsed.Query()
		q.Set("response_type", "code")
		q.Set("client_id", b.clientID)
		q.Set("scope", teslaScopes)
		q.Set("redirect_uri", b.redirectURI(ctx))
		q.Set("require_requested_scopes", "true")
		parsed.RawQuery = q.Encode()
		baseOAuthURL = parsed.String()
	}

	return types.ESSProviderInfo{
		ID:   "tesla",
		Name: "Tesla",
		OAuthURLs: map[string]string{
			"default": baseOAuthURL,
		},
		Credentials: []types.ESSCredentialField{
			{
				Field:    "authCode",
				Name:     "Authorization Code",
				Type:     types.ESSCredentialFieldTypePassword,
				Required: true,
				Hidden:   true,
				OneTime:  true,
			},
			{
				Field:       "serialNumber",
				Name:        "Serial Number (Optional)",
				Type:        types.ESSCredentialFieldTypeString,
				Required:    false,
				Description: "If left empty, RateRudder will choose the first system available.",
			},
		},
	}
}

func (b *baseTesla) redirectURI(ctx context.Context) string {
	scheme := "https"
	host := common.CtxHost(ctx)
	if host == "localhost" || strings.HasPrefix(host, "localhost:") {
		scheme = "http"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/callback/tesla",
	}
	return u.String()
}

func (b *baseTesla) doTokenRequest(ctx context.Context, baseURL string, data url.Values) (teslaTokenResponse, error) {
	// always set client_id and audience
	data.Set("client_id", b.clientID)
	data.Set("audience", baseURL)

	switch data.Get("grant_type") {
	case "client_credentials", "authorization_code":
		data.Set("client_secret", b.clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return teslaTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var res teslaTokenResponse
	if err := b.doRequest(req, &res); err != nil {
		return teslaTokenResponse{}, err
	}
	if res.AccessToken == "" {
		return teslaTokenResponse{}, fmt.Errorf("missing access token in token response: %s", data.Get("grant_type"))
	}
	return res, nil
}

func (b *baseTesla) newGETRequest(ctx context.Context, method, path, token, baseURL string) (*http.Request, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req, nil
}

func (b *baseTesla) newPOSTRequest(ctx context.Context, method, path, token, baseURL string, payload any) (*http.Request, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, path)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if payload != nil {
		pb, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(pb)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req, nil
}

// teslaHTTPError wraps an error from the Tesla API with its HTTP status code.
type teslaHTTPError struct {
	StatusCode int
	Wrapped    error
}

func (e *teslaHTTPError) Error() string {
	return e.Wrapped.Error()
}

func (e *teslaHTTPError) Unwrap() error {
	return e.Wrapped
}

func (b *baseTesla) doRequest(req *http.Request, dest any) error {
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var retErr error
		if len(body) == 0 {
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				retErr = fmt.Errorf("%w: unexpected tesla response status %d", ErrUnauthorized, resp.StatusCode)
			} else {
				retErr = fmt.Errorf("unexpected tesla response status %d", resp.StatusCode)
			}
		} else {
			var errBody teslaErrorResponse
			if err := json.Unmarshal(body, &errBody); err == nil && (errBody.Error != "" || errBody.ErrorDescription != "") {
				errStr := errBody.ErrorDescription
				if errStr == "" {
					errStr = errBody.Error
				}
				originalErr := fmt.Errorf("unexpected tesla response status %d: %s", resp.StatusCode, errStr)
				errStrLower := strings.ToLower(errStr)
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
					strings.Contains(errStrLower, "unauthorized") || strings.Contains(errStrLower, "missing scopes") || strings.Contains(errStrLower, "invalid_token") {
					retErr = fmt.Errorf("%w: %w", ErrUnauthorized, originalErr)
				} else {
					retErr = originalErr
				}
			} else {
				if len(body) > 256 {
					body = body[:256]
				}
				originalErr := fmt.Errorf("unexpected tesla response status %d: %s", resp.StatusCode, string(body))
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					retErr = fmt.Errorf("%w: %w", ErrUnauthorized, originalErr)
				} else {
					retErr = originalErr
				}
			}
		}
		return &teslaHTTPError{
			StatusCode: resp.StatusCode,
			Wrapped:    retErr,
		}
	}

	var logResponse any
	var envelope struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Response) > 0 {
		var responseMap map[string]any
		if err := json.Unmarshal(envelope.Response, &responseMap); err == nil {
			for _, k := range []string{"tariff_content", "tariff_content_v2"} {
				if v, ok := responseMap[k]; ok {
					b, err := json.Marshal(v)
					if err == nil && len(b) > 4096 {
						responseMap[k] = string(b[:4096]) + fmt.Sprintf("... [truncated %d bytes]", len(b)-4096)
					}
				}
			}
			logResponse = responseMap
		} else {
			var respAny any
			if err := json.Unmarshal(envelope.Response, &respAny); err == nil {
				logResponse = respAny
			} else {
				logResponse = string(envelope.Response)
			}
		}
	} else {
		var bodyAny any
		if err := json.Unmarshal(body, &bodyAny); err == nil {
			logResponse = bodyAny
		} else {
			logResponse = string(body)
		}
	}

	if !strings.Contains(req.URL.Path, "/oauth") && !strings.Contains(req.URL.Path, "token") {
		log.Ctx(req.Context()).DebugContext(req.Context(),
			"tesla result",
			slog.String("url", req.URL.String()),
			slog.String("method", req.Method),
			slog.Any("response", logResponse),
		)
	}

	if dest != nil {
		if err := json.Unmarshal(body, dest); err != nil {
			if len(body) > 256 {
				body = body[:256]
			}
			log.Ctx(req.Context()).ErrorContext(req.Context(), "failed to decode tesla response", slog.Any("error", err), slog.String("body", string(body)))
			return fmt.Errorf("failed to decode tesla response: %w", err)
		}
	}
	return nil
}

func (b *baseTesla) enabled() bool {
	return b.clientID != "" && b.clientSecret != ""
}

func (b *baseTesla) getRegionBaseURLFromCode(code string) (string, string, error) {
	// the code starts with the region
	for region, baseURL := range b.baseURLs {
		if strings.HasPrefix(code, region) {
			return region, baseURL, nil
		}
	}
	// don't print out the whole code, the first 8 characters are usually enough
	if len(code) > 8 {
		code = code[:8]
	}
	return "", "", fmt.Errorf("unknown region for code %s", code)
}

func (b *baseTesla) getBaseURL(region string) (string, error) {
	if baseURL := b.baseURLs[region]; baseURL != "" {
		return baseURL, nil
	}
	return "", fmt.Errorf("unknown region %s", region)
}

// Tesla implements the System interface for Tesla Fleet API.
type Tesla struct {
	base             *baseTesla
	settings         types.Settings
	mu               sync.Mutex
	token            string
	baseURL          string
	energySiteID     int64
	siteInfoCache    teslaSiteInfoResponse
	siteInfoExpiry   time.Time
	liveStatusCache  teslaLiveStatusResponse
	liveStatusExpiry time.Time

	// retry delays for live_status 424 failures
	retryDelay1 time.Duration
	retryDelay2 time.Duration
	verifyDelay time.Duration
}

func newTesla(b *baseTesla) *Tesla {
	return &Tesla{
		base:        b,
		retryDelay1: 3 * time.Second,
		retryDelay2: 5 * time.Second,
		verifyDelay: 2 * time.Minute,
	}
}

func (b *Tesla) Name() string {
	return "tesla"
}

// ApplySettings applies the given settings.
func (b *Tesla) ApplySettings(ctx context.Context, settings types.Settings) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.settings = settings
	return nil
}

type teslaTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Authenticate validates the credentials and performs OAuth exchange or token refresh.
func (b *Tesla) Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error) {
	if creds.Tesla == nil || (creds.Tesla.AuthCode == "" && creds.Tesla.AccessToken == "") {
		return creds, false, ErrCredentialsMissing
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	changed := false

	// If we have an AuthCode, we need to exchange it for a token
	if creds.Tesla.AuthCode != "" {
		// determine base URL so we can set the region
		region, baseURL, err := b.base.getRegionBaseURLFromCode(creds.Tesla.AuthCode)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to determine region from auth code", slog.String("authCode", creds.Tesla.AuthCode))
			return creds, false, fmt.Errorf("failed to determine region from auth code")
		}
		log.Ctx(ctx).InfoContext(ctx, "submitting auth code to tesla to get access token", slog.String("region", region))
		res, err := b.exchangeCodeForToken(ctx, baseURL, creds.Tesla.AuthCode)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to exchange auth code", slog.String("authCode", creds.Tesla.AuthCode), slog.Any("error", err))
			return creds, false, fmt.Errorf("failed to exchange auth code: %w", err)
		}
		creds.Tesla.AccessToken = res.AccessToken
		creds.Tesla.RefreshToken = res.RefreshToken
		creds.Tesla.Expiry = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
		creds.Tesla.AuthCode = "" // clear it so we don't try again
		creds.Tesla.Region = region
		b.baseURL = baseURL
		b.token = res.AccessToken

		// fill in the default energy site ID if it wasn't sent
		if creds.Tesla.EnergySiteID == 0 {
			id, err := b.getDefaultSiteID(ctx, creds.Tesla.SerialNumber)
			if err != nil {
				return creds, false, fmt.Errorf("failed to get default site id: %w", err)
			}
			log.Ctx(ctx).InfoContext(ctx, "automatically selected site", slog.Int64("energySiteID", id), slog.String("serialNumber", creds.Tesla.SerialNumber))
			creds.Tesla.EnergySiteID = id
		}
		changed = true
	} else {
		baseURL, err := b.base.getBaseURL(creds.Tesla.Region)
		if err != nil {
			return creds, false, fmt.Errorf("failed to get base URL: %w", err)
		}
		b.baseURL = baseURL

		// If token is expired or about to expire, refresh it
		if creds.Tesla.RefreshToken != "" && time.Now().Add(15*time.Minute).After(creds.Tesla.Expiry) {
			// the refresh token can only be used once so we need to ensure we return it
			// and it gets updated along with the new one and the updated token
			// we can't do ensureLogin like we do in franklin because we might use the
			// refresh token and not be able to ensure it gets stored in the database
			log.Ctx(ctx).DebugContext(ctx, "refreshing tesla access token", slog.Time("expiry", creds.Tesla.Expiry))
			res, err := b.refreshToken(ctx, creds.Tesla.Region, creds.Tesla.RefreshToken)
			if err != nil {
				return creds, false, fmt.Errorf("failed to refresh token: %w", err)
			}
			creds.Tesla.AccessToken = res.AccessToken
			creds.Tesla.RefreshToken = res.RefreshToken
			creds.Tesla.Expiry = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
			b.token = res.AccessToken
			changed = true
		} else {
			// use the existing token
			b.token = creds.Tesla.AccessToken
		}
	}

	b.energySiteID = creds.Tesla.EnergySiteID

	// Verify token works by fetching site info and live status
	if _, err := b.getSiteInfoWithCache(ctx, true); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "tesla credential validation failed", slog.Any("error", err))
		return creds, false, fmt.Errorf("credential validation failed: %w", err)
	}

	if _, err := b.getLiveStatusWithCache(ctx, true); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "tesla credential validation failed", slog.Any("error", err))
		return creds, false, fmt.Errorf("credential validation failed: %w", err)
	}

	return creds, changed, nil
}

func (b *Tesla) exchangeCodeForToken(ctx context.Context, baseURL, code string) (teslaTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", b.base.redirectURI(ctx))
	return b.base.doTokenRequest(ctx, baseURL, data)
}

func (b *Tesla) refreshToken(ctx context.Context, baseURL, refreshToken string) (teslaTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	return b.base.doTokenRequest(ctx, baseURL, data)
}

func (b *Tesla) getCalendarHistory(ctx context.Context, kind string, start, end time.Time, loc *time.Location, tz string, dest any) error {
	path := fmt.Sprintf("api/1/energy_sites/%d/calendar_history", b.energySiteID)
	params := url.Values{}
	params.Set("kind", kind)
	params.Set("period", "day")
	params.Set("time_zone", tz)
	params.Set("start_date", start.In(loc).Format(time.RFC3339))
	params.Set("end_date", end.In(loc).Format(time.RFC3339))
	return b.doGETRequest(ctx, path, params, dest)
}

func (b *Tesla) doGETRequest(ctx context.Context, path string, params url.Values, dest any) error {
	req, err := b.base.newGETRequest(ctx, "GET", path, b.token, b.baseURL)
	if err != nil {
		return err
	}
	if len(params) > 0 {
		q := req.URL.Query()
		for k, vs := range params {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		req.URL.RawQuery = q.Encode()
	}

	var raw json.RawMessage
	if err := b.base.doRequest(req, &raw); err != nil {
		return err
	}

	var response struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		body := raw
		if len(body) > 256 {
			body = body[:256]
		}
		log.Ctx(ctx).ErrorContext(ctx, "failed to decode tesla envelope", slog.Any("error", err), slog.String("body", string(body)))
		return fmt.Errorf("failed to decode tesla envelope: %w", err)
	}

	if dest != nil {
		if err := json.Unmarshal(response.Response, dest); err != nil {
			// We encountered cases where the response field is a string like ""
			// rather than the expected struct object. Log up to 512 characters of the raw envelope
			// to help debug why the API returned this shape instead of the expected struct/slice.
			body := raw
			if len(body) > 512 {
				body = body[:512]
			}
			log.Ctx(ctx).ErrorContext(ctx, "failed to decode tesla response", slog.Any("error", err), slog.String("body", string(body)))
			return fmt.Errorf("failed to decode tesla response: %w", err)
		}
	}
	return nil
}

func (b *Tesla) getDefaultSiteID(ctx context.Context, serialNumber string) (int64, error) {
	var res teslaProductsResponse
	if err := b.doGETRequest(ctx, "api/1/products", nil, &res); err != nil {
		return 0, err
	}
	for _, p := range res {
		if p.DeviceType == "energy" && p.ResourceType == "battery" && p.EnergySiteID != 0 {
			if serialNumber != "" && p.GatewayID != "" {
				gwLower := strings.ToLower(p.GatewayID)
				snLower := strings.ToLower(serialNumber)
				if strings.EqualFold(p.GatewayID, serialNumber) || strings.Contains(gwLower, snLower) {
					return p.EnergySiteID, nil
				}
			} else {
				return p.EnergySiteID, nil
			}
		}
	}
	if serialNumber != "" {
		return 0, fmt.Errorf("no energy site found matching serial number: %s", serialNumber)
	}
	return 0, fmt.Errorf("no energy site found")
}
func (b *Tesla) getSiteInfo(ctx context.Context) (teslaSiteInfoResponse, error) {
	siteInfoPath := fmt.Sprintf("api/1/energy_sites/%d/site_info", b.energySiteID)
	var siteInfo teslaSiteInfoResponse
	for attempt := 1; attempt <= 3; attempt++ {
		err := b.doGETRequest(ctx, siteInfoPath, nil, &siteInfo)
		if err == nil {
			break
		}

		var httpErr *teslaHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 500 {
			delay := b.retryDelay1
			if attempt == 2 {
				delay = b.retryDelay2
			}
			if attempt < 3 {
				log.Ctx(ctx).WarnContext(ctx, "tesla site_info failed with 500, retrying", slog.Any("error", err), slog.Duration("delay", delay))
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return teslaSiteInfoResponse{}, ctx.Err()
				}
				continue
			}
		}
		return teslaSiteInfoResponse{}, err
	}
	return siteInfo, nil
}

func (b *Tesla) getSiteInfoWithCache(ctx context.Context, refresh bool) (teslaSiteInfoResponse, error) {
	if !refresh && time.Now().Before(b.siteInfoExpiry) {
		return b.siteInfoCache, nil
	}
	si, err := b.getSiteInfo(ctx)
	if err != nil {
		return teslaSiteInfoResponse{}, err
	}
	b.siteInfoCache = si
	b.siteInfoExpiry = time.Now().Add(time.Minute)
	return si, nil
}

func (b *Tesla) getLiveStatus(ctx context.Context) (teslaLiveStatusResponse, error) {
	liveStatusPath := fmt.Sprintf("api/1/energy_sites/%d/live_status", b.energySiteID)
	var liveStatus teslaLiveStatusResponse
	for attempt := 1; attempt <= 3; attempt++ {
		err := b.doGETRequest(ctx, liveStatusPath, nil, &liveStatus)
		if err == nil {
			break
		}

		var httpErr *teslaHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 424 {
			delay := b.retryDelay1
			if attempt == 2 {
				delay = b.retryDelay2
			}
			if attempt < 3 {
				log.Ctx(ctx).WarnContext(ctx, "tesla live_status failed with 424, retrying", slog.Any("error", err), slog.Duration("delay", delay))
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return teslaLiveStatusResponse{}, ctx.Err()
				}
				continue
			}
		}
		return teslaLiveStatusResponse{}, err
	}
	return liveStatus, nil
}

func (b *Tesla) getLiveStatusWithCache(ctx context.Context, refresh bool) (teslaLiveStatusResponse, error) {
	if !refresh && time.Now().Before(b.liveStatusExpiry) {
		return b.liveStatusCache, nil
	}
	ls, err := b.getLiveStatus(ctx)
	if err != nil {
		return teslaLiveStatusResponse{}, err
	}
	b.liveStatusCache = ls
	b.liveStatusExpiry = time.Now().Add(time.Minute)
	return ls, nil
}

// GetStatus returns the current status of the system.
func (b *Tesla) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting tesla system status")
	b.mu.Lock()
	defer b.mu.Unlock()

	siteInfo, err := b.getSiteInfoWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	liveStatus, err := b.getLiveStatusWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	var validBatteries []teslaSiteComponentsBattery
	for _, b := range siteInfo.Components.Batteries {
		if strings.ToLower(b.PartName) == "unknown" || b.NameplateEnergyWH == 0 || b.NameplateMaxChargePowerW == 0 || b.NameplateMaxDischargePowerW == 0 {
			continue
		}
		validBatteries = append(validBatteries, b)
	}

	capacityKWH := siteInfo.NameplateEnergyWH / 1000.0
	var totalChargeKW float64
	var totalDischargeKW float64
	if len(validBatteries) > 0 {
		var totalBatteryEnergyWH float64
		for _, b := range validBatteries {
			totalChargeKW += 3.3
			totalDischargeKW += b.NameplateMaxDischargePowerW / 1000.0
			totalBatteryEnergyWH += b.NameplateEnergyWH
		}
		if capacityKWH == 0 {
			capacityKWH = totalBatteryEnergyWH / 1000.0
		}
	} else if len(siteInfo.Components.Gateways) > 0 {
		var totalGatewayEnergyWH float64
		for _, gw := range siteInfo.Components.Gateways {
			totalChargeKW += 3.3
			// sometimes a gateway has the total nameplate for the whole system
			totalDischargeKW += gw.NameplatePowerWatts / 1000.0
			totalGatewayEnergyWH += gw.NameplateEnergyWatts
		}
		if capacityKWH == 0 {
			capacityKWH = totalGatewayEnergyWH / 1000.0
		}
	} else {
		totalChargeKW = 3.3
	}

	tz := siteInfo.InstallationTimeZone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to load installation time zone", slog.String("tz", tz), slog.Any("error", err))
		loc = time.UTC
	}

	batteryCount := getBatteryCount(siteInfo)
	isMaxBackup := isMaxBackupCharging(siteInfo, liveStatus, batteryCount)

	status := types.SystemStatus{
		Timestamp:             time.Now().In(loc),
		TimeLocation:          loc.String(),
		BatterySOC:            liveStatus.PercentageCharged,
		BatteryKW:             liveStatus.BatteryPowerW / 1000.0,
		BatteryCapacityKWH:    capacityKWH,
		MaxBatteryDischargeKW: totalDischargeKW,
		MaxBatteryChargeKW:    totalChargeKW,
		SolarKW:               liveStatus.SolarPowerW / 1000.0,
		GridKW:                liveStatus.GridPowerW / 1000.0,
		HomeKW:                liveStatus.LoadPowerW / 1000.0,
		ElevatedMinBatterySOC: siteInfo.BackupReservePercent > 0 && siteInfo.BackupReservePercent > b.settings.MinBatterySOC,
		BatteryAboveMinSOC:    liveStatus.PercentageCharged >= siteInfo.BackupReservePercent,
		EmergencyMode:         liveStatus.StormModeActive || isMaxBackup,
		GridUnavailable:       liveStatus.GridStatus != "Active",
		VPPActive:             liveStatus.GridServicesActive,
		VPPKW:                 liveStatus.GridServicesPowerW / 1000.0,
		TOUMode:               siteInfo.DefaultRealMode == "autonomous",
		// TODO: how do we know when battery charging is disabled
		// TODO: what about alarms?
	}
	log.Ctx(ctx).DebugContext(ctx, "tesla system status", slog.Any("status", status))

	return status, nil
}

// GridSettings returns the grid-related capabilities reported by Tesla.
func (b *Tesla) GridSettings(ctx context.Context) (types.GridSettings, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	siteInfo, err := b.getSiteInfoWithCache(ctx, false)
	if err != nil {
		return types.GridSettings{}, err
	}

	canCharge := siteInfo.Components.EditSettingGridCharging || !siteInfo.Components.DisallowChargeFromGridWithSolarInstalled
	canExportSolar := siteInfo.Components.EditSettingPermissionToExport || siteInfo.Components.Solar
	// this can be changed so it might not represent the actual permissions
	canExportBatteries := siteInfo.Components.EditSettingEnergyExports || siteInfo.Components.CustomerPreferredExportRule == teslaExportRuleBatteryOk

	return types.GridSettings{
		GridChargeBatteries: canCharge,
		GridExportSolar:     canExportSolar,
		GridExportBatteries: canExportBatteries,
	}, nil
}

func getBatteryCount(siteInfo teslaSiteInfoResponse) float64 {
	if siteInfo.BatteryCount > 0 {
		return float64(siteInfo.BatteryCount)
	}
	return 1.0
}

func isMaxBackupCharging(siteInfo teslaSiteInfoResponse, liveStatus teslaLiveStatusResponse, batteryCount float64) bool {
	if batteryCount <= 0 {
		batteryCount = 1.0
	}
	// In self_consumption mode, grid charge rate is capped at ~1.67 kW (1670 W) per battery.
	// If the grid-specific charge rate per battery exceeds 2.0 kW (2000 W), this indicates Max Backup or high-rate backup charge mode.
	solarExcessW := math.Max(0, liveStatus.SolarPowerW-liveStatus.LoadPowerW)
	totalChargePowerW := math.Max(0, -liveStatus.BatteryPowerW)
	gridChargePowerW := math.Max(0, totalChargePowerW-solarExcessW)
	gridChargeRatePerBatteryW := gridChargePowerW / batteryCount

	return siteInfo.DefaultRealMode == "self_consumption" &&
		liveStatus.BatteryPowerW < -100 &&
		gridChargeRatePerBatteryW > 2000
}

// roundTOUPeriodStart rounds a start timestamp for ESS TOU periods down to the nearest 00 or 30 minute boundary.
// It must always round down (never up) so that the peak period is immediately active and we don't delay solar export.
func roundTOUPeriodStart(t time.Time) time.Time {
	truncated := t.Truncate(time.Hour)
	if t.Minute() < 30 {
		return truncated
	}
	return truncated.Add(30 * time.Minute)
}

// roundTOUPeriodEnd rounds an ending timestamp for ESS TOU periods to 00 or 30 minutes.
// Realistically the periods will be either 30 or 60 minute periods.
// We round up to the next hour if it's 31 minutes or higher, otherwise round down to 30 minutes.
// If it's less than 15 mins from the start of an hour, round down to 00, otherwise round up to 30.
// When we run again on the next update, we can stop, but we want to generally round up to make sure we don't miss peak pricing.
func roundTOUPeriodEnd(t time.Time) time.Time {
	minute := t.Minute()
	truncated := t.Truncate(time.Hour)
	switch {
	case minute < 15:
		return truncated
	case minute <= 30:
		return truncated.Add(30 * time.Minute)
	default: // minute >= 31
		return truncated.Add(time.Hour)
	}
}

func (b *Tesla) buildTOUTariffPayload(start time.Time, until time.Time, currentPrice types.Price) map[string]any {
	roundedStart := roundTOUPeriodStart(start)
	roundedUntil := roundTOUPeriodEnd(until)
	if !roundedUntil.After(roundedStart) {
		roundedUntil = roundedStart.Add(30 * time.Minute)
	}

	startHour := roundedStart.Hour()
	startMinute := roundedStart.Minute()
	endHour := roundedUntil.Hour()
	endMinute := roundedUntil.Minute()

	startDay := time.Date(roundedStart.Year(), roundedStart.Month(), roundedStart.Day(), 0, 0, 0, 0, roundedStart.Location())
	untilDay := time.Date(roundedUntil.Year(), roundedUntil.Month(), roundedUntil.Day(), 0, 0, 0, 0, roundedUntil.Location())
	crossesMidnight := !untilDay.Equal(startDay) && !(roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0)

	day1EndHour := endHour
	day1EndMinute := endMinute
	if crossesMidnight || (endHour == 0 && endMinute == 0) || !untilDay.Equal(startDay) {
		day1EndHour = 24
		day1EndMinute = 0
	}
	if day1EndHour > 24 {
		day1EndHour = 24
		day1EndMinute = 0
	}

	const minRateSpread = 0.15

	onPeakBuy := currentPrice.DollarsPerKWH + currentPrice.GridUseDollarsPerKWH
	if onPeakBuy < 0 {
		onPeakBuy = 0.0
	}

	onPeakSell := currentPrice.DollarsPerKWH + currentPrice.GenerationAdjustmentDollarsPerKWH
	if currentPrice.SeparateGenerationCredit {
		onPeakSell = currentPrice.GenerationCreditDollarsPerKWH
	}
	if onPeakSell < 0 {
		onPeakSell = 0.0
	}

	// The Tesla Fleet API requires that buy_rate >= sell_rate at any given time;
	// if sell_rate > buy_rate, Tesla snaps buy_rate equal to sell_rate.
	// To ensure the onboard optimizer sees maximum financial incentive to export solar immediately,
	// we set onPeakBuy and onPeakSell equal to the peak rate (with at least minRateSpread above $0.00 off-peak).
	peakRate := max(onPeakBuy, onPeakSell, minRateSpread)
	onPeakBuy = peakRate
	onPeakSell = peakRate

	offPeakBuy := 0.0
	offPeakSell := 0.0

	var offPeakPeriods []map[string]any
	if startHour > 0 || startMinute > 0 {
		offPeakPeriods = append(offPeakPeriods, map[string]any{
			"fromDayOfWeek": 0,
			"toDayOfWeek":   6,
			"fromHour":      0,
			"fromMinute":    0,
			"toHour":        startHour,
			"toMinute":      startMinute,
		})
	}
	if day1EndHour < 24 {
		offPeakPeriods = append(offPeakPeriods, map[string]any{
			"fromDayOfWeek": 0,
			"toDayOfWeek":   6,
			"fromHour":      day1EndHour,
			"fromMinute":    day1EndMinute,
			"toHour":        0,
			"toMinute":      0,
		})
	}

	onPeakPeriods := []map[string]any{
		{
			"fromDayOfWeek": 0,
			"toDayOfWeek":   6,
			"fromHour":      startHour,
			"fromMinute":    startMinute,
			"toHour":        day1EndHour % 24,
			"toMinute":      day1EndMinute,
		},
	}

	todayTouPeriods := map[string]any{
		"ON_PEAK": map[string]any{
			"periods": onPeakPeriods,
		},
	}
	if len(offPeakPeriods) > 0 {
		todayTouPeriods["OFF_PEAK"] = map[string]any{
			"periods": offPeakPeriods,
		}
	}

	todayBuyRates := map[string]float64{
		"ON_PEAK": onPeakBuy,
	}
	todaySellRates := map[string]float64{
		"ON_PEAK": onPeakSell,
	}
	if len(offPeakPeriods) > 0 {
		todayBuyRates["OFF_PEAK"] = offPeakBuy
		todaySellRates["OFF_PEAK"] = offPeakSell
	}

	nonTodayTouPeriods := map[string]any{
		"OFF_PEAK": map[string]any{
			"periods": []map[string]any{
				{
					"fromDayOfWeek": 0,
					"toDayOfWeek":   6,
					"fromHour":      0,
					"fromMinute":    0,
					"toHour":        0,
					"toMinute":      0,
				},
			},
		},
	}
	nonTodayBuyRates := map[string]float64{
		"OFF_PEAK": 0.0,
	}
	nonTodaySellRates := map[string]float64{
		"OFF_PEAK": 0.0,
	}

	seasons := map[string]any{}
	energyCharges := map[string]any{}
	sellSeasons := map[string]any{}
	sellEnergyCharges := map[string]any{}

	day1 := roundedStart
	day1Month := int(day1.Month())
	day1Day := day1.Day()

	if crossesMidnight {
		day2 := roundedStart.AddDate(0, 0, 1)
		day2Month := int(day2.Month())
		day2Day := day2.Day()

		tomorrowOnPeak := []map[string]any{
			{
				"fromDayOfWeek": 0,
				"toDayOfWeek":   6,
				"fromHour":      0,
				"fromMinute":    0,
				"toHour":        endHour % 24,
				"toMinute":      endMinute,
			},
		}
		var tomorrowOffPeak []map[string]any
		if (endHour%24) != 0 || endMinute != 0 {
			tomorrowOffPeak = append(tomorrowOffPeak, map[string]any{
				"fromDayOfWeek": 0,
				"toDayOfWeek":   6,
				"fromHour":      endHour % 24,
				"fromMinute":    endMinute,
				"toHour":        0,
				"toMinute":      0,
			})
		}

		tomorrowTouPeriods := map[string]any{
			"ON_PEAK": map[string]any{
				"periods": tomorrowOnPeak,
			},
		}
		if len(tomorrowOffPeak) > 0 {
			tomorrowTouPeriods["OFF_PEAK"] = map[string]any{
				"periods": tomorrowOffPeak,
			}
		}

		tomorrowBuyRates := map[string]float64{
			"ON_PEAK": onPeakBuy,
		}
		tomorrowSellRates := map[string]float64{
			"ON_PEAK": onPeakSell,
		}
		if len(tomorrowOffPeak) > 0 {
			tomorrowBuyRates["OFF_PEAK"] = offPeakBuy
			tomorrowSellRates["OFF_PEAK"] = offPeakSell
		}

		if day1.Year() != day2.Year() {
			// Year rollover: Day 1 is Dec 31, Day 2 is Jan 1
			seasons["TOMORROW"] = map[string]any{
				"fromMonth":   1,
				"fromDay":     1,
				"toMonth":     1,
				"toDay":       1,
				"tou_periods": tomorrowTouPeriods,
			}
			energyCharges["TOMORROW"] = map[string]any{
				"rates": tomorrowBuyRates,
			}
			sellSeasons["TOMORROW"] = map[string]any{
				"fromMonth":   1,
				"fromDay":     1,
				"toMonth":     1,
				"toDay":       1,
				"tou_periods": tomorrowTouPeriods,
			}
			sellEnergyCharges["TOMORROW"] = map[string]any{
				"rates": tomorrowSellRates,
			}

			seasons["AFTER"] = map[string]any{
				"fromMonth":   1,
				"fromDay":     2,
				"toMonth":     12,
				"toDay":       30,
				"tou_periods": nonTodayTouPeriods,
			}
			energyCharges["AFTER"] = map[string]any{
				"rates": nonTodayBuyRates,
			}
			sellSeasons["AFTER"] = map[string]any{
				"fromMonth":   1,
				"fromDay":     2,
				"toMonth":     12,
				"toDay":       30,
				"tou_periods": nonTodayTouPeriods,
			}
			sellEnergyCharges["AFTER"] = map[string]any{
				"rates": nonTodaySellRates,
			}

			seasons["TODAY"] = map[string]any{
				"fromMonth":   12,
				"fromDay":     31,
				"toMonth":     12,
				"toDay":       31,
				"tou_periods": todayTouPeriods,
			}
			energyCharges["TODAY"] = map[string]any{
				"rates": todayBuyRates,
			}
			sellSeasons["TODAY"] = map[string]any{
				"fromMonth":   12,
				"fromDay":     31,
				"toMonth":     12,
				"toDay":       31,
				"tou_periods": todayTouPeriods,
			}
			sellEnergyCharges["TODAY"] = map[string]any{
				"rates": todaySellRates,
			}
		} else {
			if !(day1Month == 1 && day1Day == 1) {
				prevDay := day1.AddDate(0, 0, -1)
				seasons["BEFORE"] = map[string]any{
					"fromMonth":   1,
					"fromDay":     1,
					"toMonth":     int(prevDay.Month()),
					"toDay":       prevDay.Day(),
					"tou_periods": nonTodayTouPeriods,
				}
				energyCharges["BEFORE"] = map[string]any{
					"rates": nonTodayBuyRates,
				}
				sellSeasons["BEFORE"] = map[string]any{
					"fromMonth":   1,
					"fromDay":     1,
					"toMonth":     int(prevDay.Month()),
					"toDay":       prevDay.Day(),
					"tou_periods": nonTodayTouPeriods,
				}
				sellEnergyCharges["BEFORE"] = map[string]any{
					"rates": nonTodaySellRates,
				}
			}

			seasons["TODAY"] = map[string]any{
				"fromMonth":   day1Month,
				"fromDay":     day1Day,
				"toMonth":     day1Month,
				"toDay":       day1Day,
				"tou_periods": todayTouPeriods,
			}
			energyCharges["TODAY"] = map[string]any{
				"rates": todayBuyRates,
			}
			sellSeasons["TODAY"] = map[string]any{
				"fromMonth":   day1Month,
				"fromDay":     day1Day,
				"toMonth":     day1Month,
				"toDay":       day1Day,
				"tou_periods": todayTouPeriods,
			}
			sellEnergyCharges["TODAY"] = map[string]any{
				"rates": todaySellRates,
			}

			seasons["TOMORROW"] = map[string]any{
				"fromMonth":   day2Month,
				"fromDay":     day2Day,
				"toMonth":     day2Month,
				"toDay":       day2Day,
				"tou_periods": tomorrowTouPeriods,
			}
			energyCharges["TOMORROW"] = map[string]any{
				"rates": tomorrowBuyRates,
			}
			sellSeasons["TOMORROW"] = map[string]any{
				"fromMonth":   day2Month,
				"fromDay":     day2Day,
				"toMonth":     day2Month,
				"toDay":       day2Day,
				"tou_periods": tomorrowTouPeriods,
			}
			sellEnergyCharges["TOMORROW"] = map[string]any{
				"rates": tomorrowSellRates,
			}

			if !(day2Month == 12 && day2Day == 31) {
				nextDay := day2.AddDate(0, 0, 1)
				seasons["AFTER"] = map[string]any{
					"fromMonth":   int(nextDay.Month()),
					"fromDay":     nextDay.Day(),
					"toMonth":     12,
					"toDay":       31,
					"tou_periods": nonTodayTouPeriods,
				}
				energyCharges["AFTER"] = map[string]any{
					"rates": nonTodayBuyRates,
				}
				sellSeasons["AFTER"] = map[string]any{
					"fromMonth":   int(nextDay.Month()),
					"fromDay":     nextDay.Day(),
					"toMonth":     12,
					"toDay":       31,
					"tou_periods": nonTodayTouPeriods,
				}
				sellEnergyCharges["AFTER"] = map[string]any{
					"rates": nonTodaySellRates,
				}
			}
		}
	} else {
		if !(day1Month == 1 && day1Day == 1) {
			prevDay := day1.AddDate(0, 0, -1)
			seasons["BEFORE"] = map[string]any{
				"fromMonth":   1,
				"fromDay":     1,
				"toMonth":     int(prevDay.Month()),
				"toDay":       prevDay.Day(),
				"tou_periods": nonTodayTouPeriods,
			}
			energyCharges["BEFORE"] = map[string]any{
				"rates": nonTodayBuyRates,
			}
			sellSeasons["BEFORE"] = map[string]any{
				"fromMonth":   1,
				"fromDay":     1,
				"toMonth":     int(prevDay.Month()),
				"toDay":       prevDay.Day(),
				"tou_periods": nonTodayTouPeriods,
			}
			sellEnergyCharges["BEFORE"] = map[string]any{
				"rates": nonTodaySellRates,
			}
		}

		seasons["TODAY"] = map[string]any{
			"fromMonth":   day1Month,
			"fromDay":     day1Day,
			"toMonth":     day1Month,
			"toDay":       day1Day,
			"tou_periods": todayTouPeriods,
		}
		energyCharges["TODAY"] = map[string]any{
			"rates": todayBuyRates,
		}
		sellSeasons["TODAY"] = map[string]any{
			"fromMonth":   day1Month,
			"fromDay":     day1Day,
			"toMonth":     day1Month,
			"toDay":       day1Day,
			"tou_periods": todayTouPeriods,
		}
		sellEnergyCharges["TODAY"] = map[string]any{
			"rates": todaySellRates,
		}

		if !(day1Month == 12 && day1Day == 31) {
			nextDay := day1.AddDate(0, 0, 1)
			seasons["AFTER"] = map[string]any{
				"fromMonth":   int(nextDay.Month()),
				"fromDay":     nextDay.Day(),
				"toMonth":     12,
				"toDay":       31,
				"tou_periods": nonTodayTouPeriods,
			}
			energyCharges["AFTER"] = map[string]any{
				"rates": nonTodayBuyRates,
			}
			sellSeasons["AFTER"] = map[string]any{
				"fromMonth":   int(nextDay.Month()),
				"fromDay":     nextDay.Day(),
				"toMonth":     12,
				"toDay":       31,
				"tou_periods": nonTodayTouPeriods,
			}
			sellEnergyCharges["AFTER"] = map[string]any{
				"rates": nonTodaySellRates,
			}
		}
	}

	tariffContent := map[string]any{
		"name":           "RateRudder Dynamic Solar Export TOU",
		"currency":       "USD",
		"utility":        "RateRudder",
		"daily_charges":  []map[string]any{{"name": "Charge", "amount": 0}},
		"energy_charges": energyCharges,
		"sell_tariff": map[string]any{
			"name":           "RateRudder Dynamic Solar Export TOU",
			"currency":       "USD",
			"utility":        "RateRudder",
			"energy_charges": sellEnergyCharges,
			"seasons":        sellSeasons,
		},
		"seasons": seasons,
	}

	return map[string]any{
		"tou_settings": map[string]any{
			"tariff_content_v2":     tariffContent,
			"optimization_strategy": "economics",
		},
	}
}

func (b *Tesla) updateTOUSettings(ctx context.Context, start time.Time, until time.Time, currentPrice types.Price) error {
	payload := b.buildTOUTariffPayload(start, until, currentPrice)
	if b.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated tesla time_of_use_settings",
			slog.Time("start", start),
			slog.Time("until", until),
		)
		return nil
	}
	path := fmt.Sprintf("api/1/energy_sites/%d/time_of_use_settings", b.energySiteID)
	req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
	if err != nil {
		return err
	}
	if err := b.base.doRequest(req, nil); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla time_of_use_settings", slog.Any("error", err))
		return err
	}
	if touSettings, ok := payload["tou_settings"].(map[string]any); ok {
		if tc, ok := touSettings["tariff_content_v2"].(map[string]any); ok {
			b.siteInfoCache.TariffContentV2 = tc
		}
	}
	return nil
}

func getTeslaTariffInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

type teslaTOUPeriod struct {
	fromDayOfWeek int
	toDayOfWeek   int
	fromHour      int
	fromMinute    int
	toHour        int
	toMinute      int
}

func (p teslaTOUPeriod) coversDayOfWeek(dayOfWeek int) bool {
	if p.fromDayOfWeek <= p.toDayOfWeek {
		return dayOfWeek >= p.fromDayOfWeek && dayOfWeek <= p.toDayOfWeek
	}
	return dayOfWeek >= p.fromDayOfWeek || dayOfWeek <= p.toDayOfWeek
}

func seasonCoversDate(seasonMap map[string]any, d time.Time, loc *time.Location) bool {
	fromMonth, ok1 := getTeslaTariffInt(seasonMap["fromMonth"])
	fromDay, ok2 := getTeslaTariffInt(seasonMap["fromDay"])
	toMonth, ok3 := getTeslaTariffInt(seasonMap["toMonth"])
	toDay, ok4 := getTeslaTariffInt(seasonMap["toDay"])
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	dInLoc := d.In(loc)
	if fromMonth < toMonth || (fromMonth == toMonth && fromDay <= toDay) {
		seasonStart := time.Date(dInLoc.Year(), time.Month(fromMonth), fromDay, 0, 0, 0, 0, loc)
		seasonEnd := time.Date(dInLoc.Year(), time.Month(toMonth), toDay, 23, 59, 59, 999999999, loc)
		return !dInLoc.Before(seasonStart) && !dInLoc.After(seasonEnd)
	}
	// Wraps around new year
	seasonStart := time.Date(dInLoc.Year(), time.Month(fromMonth), fromDay, 0, 0, 0, 0, loc)
	seasonEnd := time.Date(dInLoc.Year(), time.Month(toMonth), toDay, 23, 59, 59, 999999999, loc)
	return !dInLoc.Before(seasonStart) || !dInLoc.After(seasonEnd)
}

func getTeslaOnPeakPeriods(seasonMap map[string]any) []teslaTOUPeriod {
	touPeriods, ok := seasonMap["tou_periods"].(map[string]any)
	if !ok {
		return nil
	}
	onPeak, ok := touPeriods["ON_PEAK"].(map[string]any)
	if !ok {
		return nil
	}
	periodsRaw, ok := onPeak["periods"]
	if !ok {
		return nil
	}

	var periodList []map[string]any
	switch pl := periodsRaw.(type) {
	case []any:
		for _, p := range pl {
			if pm, ok := p.(map[string]any); ok {
				periodList = append(periodList, pm)
			}
		}
	case []map[string]any:
		periodList = pl
	}

	var res []teslaTOUPeriod
	for _, p := range periodList {
		fromDayOfWeek, ok1 := getTeslaTariffInt(p["fromDayOfWeek"])
		toDayOfWeek, ok2 := getTeslaTariffInt(p["toDayOfWeek"])
		fromHour, ok3 := getTeslaTariffInt(p["fromHour"])
		fromMinute, ok4 := getTeslaTariffInt(p["fromMinute"])
		toHour, ok5 := getTeslaTariffInt(p["toHour"])
		toMinute, ok6 := getTeslaTariffInt(p["toMinute"])
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
			continue
		}
		res = append(res, teslaTOUPeriod{
			fromDayOfWeek: fromDayOfWeek,
			toDayOfWeek:   toDayOfWeek,
			fromHour:      fromHour,
			fromMinute:    fromMinute,
			toHour:        toHour,
			toMinute:      toMinute,
		})
	}
	return res
}

func isTeslaScheduleMatch(currentTariff map[string]any, targetUntil time.Time, now time.Time, loc *time.Location) bool {
	if len(currentTariff) == 0 {
		return false
	}
	seasons, ok := currentTariff["seasons"].(map[string]any)
	if !ok || len(seasons) == 0 {
		if sellTariff, ok := currentTariff["sell_tariff"].(map[string]any); ok {
			seasons, _ = sellTariff["seasons"].(map[string]any)
		}
	}
	if len(seasons) == 0 {
		return false
	}

	nowInLoc := now.In(loc)
	targetUntilInLoc := targetUntil.In(loc)
	roundedTargetUntil := roundTOUPeriodEnd(targetUntilInLoc)

	startDay := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), 0, 0, 0, 0, loc)
	targetDay := time.Date(roundedTargetUntil.Year(), roundedTargetUntil.Month(), roundedTargetUntil.Day(), 0, 0, 0, 0, loc)
	crossesMidnight := !targetDay.Equal(startDay) && !(roundedTargetUntil.Hour() == 0 && roundedTargetUntil.Minute() == 0)

	if crossesMidnight {
		hasEvening := false
		hasMorning := false

		// Check season covering nowInLoc (evening segment ending at midnight)
		for _, sVal := range seasons {
			seasonMap, ok := sVal.(map[string]any)
			if !ok || !seasonCoversDate(seasonMap, nowInLoc, loc) {
				continue
			}
			nowDayOfWeek := int(nowInLoc.Weekday())
			for _, p := range getTeslaOnPeakPeriods(seasonMap) {
				if !p.coversDayOfWeek(nowDayOfWeek) {
					continue
				}
				schedStart := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), p.fromHour, p.fromMinute, 0, 0, loc)
				if schedStart.After(nowInLoc) {
					continue
				}
				if p.toHour == 0 && p.toMinute == 0 {
					hasEvening = true
					break
				}
			}
			if hasEvening {
				break
			}
		}

		// Check season covering targetUntilInLoc (morning segment starting at midnight)
		for _, sVal := range seasons {
			seasonMap, ok := sVal.(map[string]any)
			if !ok || !seasonCoversDate(seasonMap, targetUntilInLoc, loc) {
				continue
			}
			targetDayOfWeek := int(targetUntilInLoc.Weekday())
			for _, p := range getTeslaOnPeakPeriods(seasonMap) {
				if !p.coversDayOfWeek(targetDayOfWeek) {
					continue
				}
				if p.fromHour != 0 || p.fromMinute != 0 {
					continue
				}
				var schedEnd time.Time
				if p.toHour == 0 && p.toMinute == 0 {
					schedEnd = targetDay.Add(24 * time.Hour)
				} else {
					schedEnd = time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day(), p.toHour, p.toMinute, 0, 0, loc)
				}
				if math.Abs(schedEnd.Sub(targetUntilInLoc).Minutes()) <= 10 || math.Abs(schedEnd.Sub(roundedTargetUntil).Minutes()) <= 10 {
					hasMorning = true
					break
				}
			}
			if hasMorning {
				break
			}
		}

		return hasEvening && hasMorning
	}

	// Non-cross-midnight (same day or targetUntil is midnight at end of today)
	for _, sVal := range seasons {
		seasonMap, ok := sVal.(map[string]any)
		if !ok || !seasonCoversDate(seasonMap, nowInLoc, loc) {
			continue
		}
		nowDayOfWeek := int(nowInLoc.Weekday())
		for _, p := range getTeslaOnPeakPeriods(seasonMap) {
			if !p.coversDayOfWeek(nowDayOfWeek) {
				continue
			}
			schedStart := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), p.fromHour, p.fromMinute, 0, 0, loc)
			var schedEnd time.Time
			if p.toHour == 0 && p.toMinute == 0 {
				schedEnd = time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), 0, 0, 0, 0, loc).Add(24 * time.Hour)
			} else {
				schedEnd = time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), p.toHour, p.toMinute, 0, 0, loc)
				if schedEnd.Before(schedStart) {
					schedEnd = schedEnd.Add(24 * time.Hour)
				}
			}
			if schedStart.After(nowInLoc) || !nowInLoc.Before(schedEnd) {
				continue
			}
			if math.Abs(schedEnd.Sub(targetUntilInLoc).Minutes()) <= 10 || math.Abs(schedEnd.Sub(roundedTargetUntil).Minutes()) <= 10 {
				return true
			}
		}
	}
	return false
}

// SetModes sets the operating modes of the system.
func (b *Tesla) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) (bool, error) {
	log.Ctx(ctx).DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol), slog.Any("opts", opts))
	b.mu.Lock()
	defer b.mu.Unlock()

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return false, nil
	}

	siteInfo, err := b.getSiteInfoWithCache(ctx, false)
	if err != nil {
		return false, err
	}

	liveStatus, err := b.getLiveStatusWithCache(ctx, false)
	if err != nil {
		return false, err
	}

	if liveStatus.StormModeActive {
		if bat == types.BatteryModeNoChange {
			return false, nil
		}
		var targetAllowGridCharge bool
		switch bat {
		case types.BatteryModeChargeAny, types.BatteryModeLoad, types.BatteryModeStandby, types.BatteryModeExport:
			targetAllowGridCharge = b.settings.GridChargeBatteries
		default:
			return false, fmt.Errorf("unknown battery mode: %v", bat)
		}

		allowGridCharge := !siteInfo.Components.DisallowChargeFromGridWithSolarInstalled
		if allowGridCharge != targetAllowGridCharge {
			log.Ctx(ctx).InfoContext(ctx, "fixing grid charge mode in storm mode",
				slog.Bool("oldAllowGridCharge", allowGridCharge),
				slog.Bool("newAllowGridCharge", targetAllowGridCharge),
			)
			allowGridCharge = targetAllowGridCharge
			if b.settings.DryRun {
				log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated grid import export in storm mode", slog.Bool("allowGridCharge", allowGridCharge))
			} else {
				path := fmt.Sprintf("api/1/energy_sites/%d/grid_import_export", b.energySiteID)
				payload := map[string]any{
					"disallow_charge_from_grid_with_solar_installed": !allowGridCharge,
					"customer_preferred_export_rule":                 siteInfo.Components.CustomerPreferredExportRule,
				}
				req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
				if err != nil {
					return false, err
				}
				if err := b.base.doRequest(req, nil); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla grid import export in storm mode", slog.Any("error", err))
					return false, err
				}
				return true, nil
			}
		} else {
			log.Ctx(ctx).DebugContext(ctx, "grid charge mode already set correctly in storm mode", slog.Bool("allowGridCharge", allowGridCharge))
		}
		return false, nil
	}

	reserveSOC := siteInfo.BackupReservePercent
	newReserveSOC := reserveSOC
	allowGridCharge := !siteInfo.Components.DisallowChargeFromGridWithSolarInstalled
	targetAllowGridCharge := allowGridCharge
	if b.settings.GridChargeBatteries && !allowGridCharge {
		targetAllowGridCharge = true
	}
	var updatedGrid bool
	var updatedMode bool

	minSOC := b.settings.MinBatterySOC
	if opts.MinimumSOC != 0 {
		minSOC = float64(opts.MinimumSOC)
	}

	targetMode := siteInfo.DefaultRealMode

	switch bat {
	case types.BatteryModeChargeAny:
		// if they want to charge the battery then set the SOC to 100 to force it to
		// charge if its not charging already
		targetSOC := 100
		if opts.ChargeToSOC != 0 {
			targetSOC = opts.ChargeToSOC
		}
		socDelta := float64(targetSOC) - liveStatus.PercentageCharged
		if socDelta > 10.0 {
			log.Ctx(ctx).DebugContext(ctx, "using backup mode to reach target SOC",
				slog.Int("targetSOC", targetSOC),
				slog.Float64("liveSOC", liveStatus.PercentageCharged),
				slog.Float64("socDelta", socDelta),
			)
			targetMode = "backup"
			newReserveSOC = 100.0
		} else {
			targetMode = "self_consumption"
			newReserveSOC = float64(targetSOC)

			// handle Tesla limitation around 81-99 reserve
			if newReserveSOC >= 85.0 && newReserveSOC < 100.0 {
				newReserveSOC = 100.0
			} else if newReserveSOC > 80.0 && newReserveSOC < 85.0 {
				newReserveSOC = 80.0
			}
		}
	case types.BatteryModeExport:
		if b.settings.ManageTOUSchedules {
			targetMode = "autonomous"
			newReserveSOC = minSOC
		} else {
			targetMode = "self_consumption"
			newReserveSOC = minSOC
		}
	case types.BatteryModeLoad:
		if b.settings.ManageTOUSchedules && sol == types.SolarModeExport {
			targetMode = "autonomous"
			newReserveSOC = minSOC
		} else {
			targetMode = "self_consumption"
			// we set the SOC to the minimum battery SOC to ensure we start discharging
			// if we're somehow less than this soc, we'll charge from the solar, unless
			// solar is unavailable then it'll charge from the grid
			// it seems like this accepts an int value
			newReserveSOC = minSOC
		}
	case types.BatteryModeStandby:
		// we floor the SOC to ensure we don't set it to a value that would cause the
		// battery to charge
		// make sure we don't set it to less than the minimum battery SOC
		newReserveSOC = max(math.Floor(liveStatus.PercentageCharged), minSOC)
		if b.settings.ManageTOUSchedules && sol == types.SolarModeExport {
			targetMode = "autonomous"
		} else if newReserveSOC >= 99 {
			newReserveSOC = 100.0
			targetMode = "self_consumption"
		} else if newReserveSOC > 80 {
			log.Ctx(ctx).DebugContext(ctx, "using backup mode with grid charging disabled for standby between 81% and 98% SOC",
				slog.Float64("soc", newReserveSOC),
				slog.Float64("liveSOC", liveStatus.PercentageCharged),
			)
			targetMode = "backup"
			newReserveSOC = 100.0
			targetAllowGridCharge = false
		} else {
			targetMode = "self_consumption"
		}
	case types.BatteryModeNoChange:
		targetAllowGridCharge = allowGridCharge
	default:
		return false, fmt.Errorf("unknown battery mode: %v", bat)
	}

	if bat != types.BatteryModeNoChange && allowGridCharge != targetAllowGridCharge {
		log.Ctx(ctx).DebugContext(ctx, "updating tesla grid charge mode",
			slog.Bool("oldAllowGridCharge", allowGridCharge),
			slog.Bool("newAllowGridCharge", targetAllowGridCharge),
		)
		allowGridCharge = targetAllowGridCharge
		updatedGrid = true
	}

	if newReserveSOC < 5 {
		newReserveSOC = 5
	}

	// if tesla overshot our reserve SOC by less than 1 percent, ignore it
	if math.Abs(newReserveSOC-reserveSOC) <= 1.0 {
		newReserveSOC = reserveSOC
	}

	updatedSOC := math.Round(newReserveSOC) != math.Round(reserveSOC)

	batteryCount := getBatteryCount(siteInfo)
	isMaxBackup := isMaxBackupCharging(siteInfo, liveStatus, batteryCount)

	// Detect unexpected grid charging when in load or standby mode.
	// If the hardware was already configured in self_consumption, SOC is above the configured reserve,
	// and the battery is unexpectedly charging from the grid, log a warning and force re-posting reserve/mode settings.
	if (bat == types.BatteryModeLoad || bat == types.BatteryModeStandby) &&
		siteInfo.DefaultRealMode == "self_consumption" &&
		liveStatus.BatteryPowerW < -100 &&
		(-liveStatus.BatteryPowerW)-liveStatus.SolarPowerW > 100 &&
		liveStatus.PercentageCharged > reserveSOC+1.0 {
		if isMaxBackup {
			log.Ctx(ctx).DebugContext(ctx, "tesla max backup or high-rate charge mode detected, suppressing reserve override",
				slog.Float64("batteryPowerW", liveStatus.BatteryPowerW),
				slog.Float64("batteryCount", batteryCount),
				slog.Float64("batterySOC", liveStatus.PercentageCharged),
				slog.Float64("configuredReserve", reserveSOC),
			)
		} else {
			log.Ctx(ctx).WarnContext(ctx, "unexpected battery grid charging detected, force updating backup reserve setting",
				slog.Float64("batteryPowerW", liveStatus.BatteryPowerW),
				slog.Float64("solarPowerW", liveStatus.SolarPowerW),
				slog.Float64("gridPowerW", liveStatus.GridPowerW),
				slog.Float64("batterySOC", liveStatus.PercentageCharged),
				slog.Float64("configuredReserve", reserveSOC),
				slog.Float64("targetReserve", newReserveSOC),
				slog.Any("batteryMode", bat),
				slog.String("defaultRealMode", siteInfo.DefaultRealMode),
			)
			updatedSOC = true
			if targetMode != "" {
				updatedMode = true
			}
		}
	}

	// Detect unexpected non-discharging when in load mode.
	// If in BatteryModeLoad, hardware is configured in self_consumption, SOC is above configured reserve (+1%),
	// home load is pulling from grid, but the battery is not discharging (> 100W),
	// log a warning and force re-posting reserve/mode settings.
	netUnservedLoadW := liveStatus.LoadPowerW - liveStatus.SolarPowerW
	if bat == types.BatteryModeLoad &&
		siteInfo.DefaultRealMode == "self_consumption" &&
		liveStatus.PercentageCharged > reserveSOC+1.0 &&
		liveStatus.BatteryPowerW <= 100 &&
		liveStatus.GridPowerW > 200 &&
		netUnservedLoadW > 200 {
		if isMaxBackup {
			log.Ctx(ctx).DebugContext(ctx, "tesla max backup or high-rate charge mode detected, suppressing non-discharge override",
				slog.Float64("batteryPowerW", liveStatus.BatteryPowerW),
				slog.Float64("batteryCount", batteryCount),
				slog.Float64("batterySOC", liveStatus.PercentageCharged),
				slog.Float64("configuredReserve", reserveSOC),
			)
		} else {
			log.Ctx(ctx).WarnContext(ctx, "unexpected battery non-discharge detected when battery should be powering load",
				slog.Float64("batteryPowerW", liveStatus.BatteryPowerW),
				slog.Float64("solarPowerW", liveStatus.SolarPowerW),
				slog.Float64("gridPowerW", liveStatus.GridPowerW),
				slog.Float64("loadPowerW", liveStatus.LoadPowerW),
				slog.Float64("batterySOC", liveStatus.PercentageCharged),
				slog.Float64("configuredReserve", reserveSOC),
				slog.Float64("targetReserve", newReserveSOC),
				slog.Any("batteryMode", bat),
				slog.String("defaultRealMode", siteInfo.DefaultRealMode),
			)
			updatedSOC = true
			if targetMode != "" {
				updatedMode = true
			}
		}
	}

	exportRule := siteInfo.Components.CustomerPreferredExportRule
	if bat == types.BatteryModeExport {
		if exportRule != teslaExportRuleBatteryOk {
			exportRule = teslaExportRuleBatteryOk
			updatedGrid = true
		}
	} else {
		switch sol {
		case types.SolarModeExport:
			if exportRule != teslaExportRulePvOnly {
				exportRule = teslaExportRulePvOnly
				updatedGrid = true
			}
		case types.SolarModeAny:
			targetRule := teslaExportRulePvOnly
			if b.settings.GridExportBatteries {
				targetRule = teslaExportRuleBatteryOk
			}
			if exportRule != "" && exportRule != targetRule {
				exportRule = targetRule
				updatedGrid = true
			}
		case types.SolarModeNoExport:
			if exportRule != "" && exportRule != teslaExportRuleNever {
				exportRule = teslaExportRuleNever
				updatedGrid = true
			}
		case types.SolarModeNoChange:
			// Do nothing
		default:
			return false, fmt.Errorf("unknown solar mode: %v", sol)
		}
	}

	if bat != types.BatteryModeNoChange && targetMode != "" && siteInfo.DefaultRealMode != targetMode {
		updatedMode = true
	}

	tz := siteInfo.InstallationTimeZone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	nowInLoc := time.Now().In(loc)

	updatedTOU := false
	if b.settings.ManageTOUSchedules && (sol == types.SolarModeExport || bat == types.BatteryModeExport) && !opts.TSScheduleModeUntil.IsZero() {
		untilInLoc := opts.TSScheduleModeUntil.In(loc)
		// Only upload schedule if it changed or if the operating mode was changed (e.g. restoring control after user intervention)
		if updatedMode || !isTeslaScheduleMatch(siteInfo.TariffContentV2, untilInLoc, nowInLoc, loc) {
			updatedTOU = true
		}
	}

	if b.settings.DryRun {
		if updatedMode {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated operation mode", slog.String("mode", targetMode))
		}
		if updatedSOC {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated backup reserve", slog.Float64("soc", newReserveSOC))
		}
		if updatedGrid {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated grid import export", slog.Bool("allowGridCharge", allowGridCharge), slog.String("exportRule", exportRule))
		}
		if updatedTOU {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated tesla time_of_use_settings",
				slog.Time("start", nowInLoc),
				slog.Time("until", opts.TSScheduleModeUntil.In(loc)),
			)
		}
		return false, nil
	}

	if updatedGrid {
		log.Ctx(ctx).InfoContext(ctx, "updating tesla grid import export", slog.Bool("allowGridCharge", allowGridCharge), slog.String("exportRule", exportRule))
		path := fmt.Sprintf("api/1/energy_sites/%d/grid_import_export", b.energySiteID)
		payload := map[string]any{
			"disallow_charge_from_grid_with_solar_installed": !allowGridCharge,
			"customer_preferred_export_rule":                 exportRule,
		}
		req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
		if err != nil {
			return false, err
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla grid import export", slog.Any("error", err))
			return false, err
		}
		b.siteInfoCache.Components.CustomerPreferredExportRule = exportRule
		b.siteInfoCache.Components.DisallowChargeFromGridWithSolarInstalled = !allowGridCharge
		b.siteInfoExpiry = time.Time{}
	}

	if updatedTOU {
		if err := b.updateTOUSettings(ctx, nowInLoc, opts.TSScheduleModeUntil.In(loc), opts.CurrentPrice); err != nil {
			return false, err
		}
	}

	if updatedMode {
		log.Ctx(ctx).InfoContext(ctx, "updating tesla operation mode", slog.String("mode", targetMode))
		path := fmt.Sprintf("api/1/energy_sites/%d/operation", b.energySiteID)
		payload := map[string]string{"default_real_mode": targetMode}
		req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
		if err != nil {
			return false, err
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla operation mode", slog.Any("error", err))
			return false, err
		}
		b.siteInfoCache.DefaultRealMode = targetMode
		b.siteInfoExpiry = time.Time{}
	}

	if updatedSOC {
		log.Ctx(ctx).InfoContext(ctx,
			"updating tesla backup reserve",
			slog.Float64("soc", newReserveSOC),
			slog.Float64("previous", reserveSOC),
		)
		path := fmt.Sprintf("api/1/energy_sites/%d/backup", b.energySiteID)
		payload := map[string]float64{"backup_reserve_percent": newReserveSOC}
		req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
		if err != nil {
			return false, err
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla backup reserve", slog.Any("error", err))
			return false, err
		}
		b.siteInfoCache.BackupReservePercent = newReserveSOC
		b.siteInfoExpiry = time.Time{}
	}

	changed := updatedMode || updatedSOC || updatedGrid || updatedTOU

	if updatedSOC {
		if wg := common.CtxWaitGroup(ctx); wg != nil {
			wg.Add(1)
			go func(ctx context.Context) {
				defer wg.Done()
				asyncCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				b.verifyBackupReserve(asyncCtx, bat, newReserveSOC, liveStatus)
			}(ctx)
		}
	}

	return changed, nil
}

func liveStatusChanged(prev, current teslaLiveStatusResponse) bool {
	if math.Abs(current.PercentageCharged-prev.PercentageCharged) >= 0.01 {
		return true
	}
	if math.Abs(current.BatteryPowerW-prev.BatteryPowerW) >= 1.0 {
		return true
	}
	if math.Abs(current.SolarPowerW-prev.SolarPowerW) >= 1.0 {
		return true
	}
	if math.Abs(current.GridPowerW-prev.GridPowerW) >= 1.0 {
		return true
	}
	if math.Abs(current.LoadPowerW-prev.LoadPowerW) >= 1.0 {
		return true
	}
	if current.GridStatus != prev.GridStatus || current.IslandStatus != prev.IslandStatus {
		return true
	}
	return false
}

func (b *Tesla) verifyBackupReserve(
	ctx context.Context,
	targetBat types.BatteryMode,
	expectedReserve float64,
	prevLiveStatus teslaLiveStatusResponse,
) {
	stepDelay := 1 * time.Minute
	if b.verifyDelay > 0 {
		stepDelay = b.verifyDelay
	}

	for attempt := 1; attempt <= 2; attempt++ {
		select {
		case <-time.After(stepDelay):
		case <-ctx.Done():
			return
		}

		b.mu.Lock()
		siteInfo, err := b.getSiteInfoWithCache(ctx, true)
		if err != nil {
			b.mu.Unlock()
			log.Ctx(ctx).WarnContext(ctx, "tesla backup reserve verification: failed to fetch site_info", slog.Any("error", err))
			return
		}

		liveStatus, err := b.getLiveStatusWithCache(ctx, true)
		b.mu.Unlock()
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "tesla backup reserve verification: failed to fetch live_status", slog.Any("error", err))
			return
		}

		currentReserve := siteInfo.BackupReservePercent
		reserveMatches := math.Abs(currentReserve-expectedReserve) <= 1.0
		refreshed := liveStatusChanged(prevLiveStatus, liveStatus) || reserveMatches

		if !refreshed {
			if attempt == 1 {
				log.Ctx(ctx).DebugContext(ctx, "tesla backup reserve verification: live_status unchanged after 1 minute, waiting another minute")
				continue
			}
			log.Ctx(ctx).WarnContext(ctx, "tesla backup reserve verification: live_status did not update after 2 minutes, bailing out",
				slog.Float64("expectedReserve", expectedReserve),
				slog.Float64("actualReserve", currentReserve),
				slog.Any("prevLiveStatus", prevLiveStatus),
				slog.Any("liveStatus", liveStatus),
			)
			return
		}

		var behaviorOk bool
		switch targetBat {
		case types.BatteryModeLoad:
			// In Load mode: if battery SOC is above expected reserve and home has net unserved load (>200W),
			// but battery is unexpectedly not discharging (<=100W) and grid is importing (>200W),
			// the battery is failing to power the load.
			netUnservedLoadW := liveStatus.LoadPowerW - liveStatus.SolarPowerW
			if liveStatus.PercentageCharged > expectedReserve+1.0 && netUnservedLoadW > 200 && liveStatus.BatteryPowerW <= 100 && liveStatus.GridPowerW > 200 {
				behaviorOk = false
			} else {
				behaviorOk = true
			}
		case types.BatteryModeChargeAny:
			// In ChargeAny mode: if the battery is fully charged (>=99%), no further charging power flow is expected.
			// If battery is under 80%, it should be actively charging at more than 1 kW (BatteryPowerW < -1000).
			// Otherwise (80% to 99%), behavior is valid if reserve matches expected or battery is actively charging (<-100W).
			if liveStatus.PercentageCharged >= 99.0 {
				behaviorOk = true
			} else if liveStatus.PercentageCharged < 80.0 {
				behaviorOk = liveStatus.BatteryPowerW < -1000
			} else {
				behaviorOk = liveStatus.BatteryPowerW < -100
			}
		case types.BatteryModeStandby:
			// In Standby mode:
			// 1. If SOC is above reserve, battery should not be unexpectedly charging from grid (<-100W).
			// 2. If SOC is at or below expected reserve (-1%), battery should not still be discharging (>100W).
			if liveStatus.PercentageCharged > expectedReserve+1.0 && liveStatus.BatteryPowerW < -100 && (-liveStatus.BatteryPowerW)-liveStatus.SolarPowerW > 100 {
				behaviorOk = false
			} else if liveStatus.PercentageCharged <= expectedReserve-1.0 && liveStatus.BatteryPowerW > 100 {
				behaviorOk = false
			} else {
				behaviorOk = true
			}
		default:
			behaviorOk = true
		}

		if reserveMatches && behaviorOk {
			log.Ctx(ctx).InfoContext(
				ctx,
				"tesla backup reserve verification succeeded",
				slog.Float64("expectedReserve", expectedReserve),
				slog.Float64("actualReserve", currentReserve),
				slog.Any("prevLiveStatus", prevLiveStatus),
				slog.Any("liveStatus", liveStatus),
			)
			return
		}

		log.Ctx(ctx).WarnContext(
			ctx,
			"tesla backup reserve not applied, retrying backup reserve setting",
			slog.Float64("expectedReserve", expectedReserve),
			slog.Float64("actualReserve", currentReserve),
			slog.Any("prevLiveStatus", prevLiveStatus),
			slog.Any("liveStatus", liveStatus),
		)

		path := fmt.Sprintf("api/1/energy_sites/%d/backup", b.energySiteID)
		payload := map[string]float64{"backup_reserve_percent": expectedReserve}
		req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "tesla backup reserve verification retry: failed to create request", slog.Any("error", err))
			return
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "tesla backup reserve verification retry: failed to update tesla backup reserve", slog.Any("error", err))
		} else {
			log.Ctx(ctx).DebugContext(ctx, "tesla backup reserve verification set reserve again", slog.Float64("soc", expectedReserve))
		}
		return
	}
}

// GetEnergyHistory returns the energy history for the specified period.
func (b *Tesla) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting tesla energy history", slog.Time("start", start), slog.Time("end", end))
	b.mu.Lock()
	defer b.mu.Unlock()

	siteInfo, err := b.getSiteInfoWithCache(ctx, false)
	if err != nil {
		return nil, err
	}

	tz := siteInfo.InstallationTimeZone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to load timezone, defaulting to UTC", slog.String("tz", tz), slog.Any("error", err))
		loc = time.UTC
	}

	hourlyStats := make(map[string]*types.EnergyStats)

	startInLoc := start.In(loc)
	endInLoc := end.In(loc)
	startDay := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(endInLoc.Year(), endInLoc.Month(), endInLoc.Day(), 0, 0, 0, 0, loc)
	lastDayToFetch := endDay
	// since the points look backward, we need to include the next day to get the
	// last period of the last day but if its in the future there's no point
	// in fetching it
	if nextDay := lastDayToFetch.AddDate(0, 0, 1); !nextDay.After(time.Now()) {
		lastDayToFetch = nextDay
	}
	current := startDay

	limiter := rate.NewLimiter(rate.Limit(4), 4)
	for !current.After(lastDayToFetch) {
		if current.After(time.Now()) {
			break
		}

		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}

		dayEnd := current.AddDate(0, 0, 1).Add(-time.Second)

		// Fetch energy history for this day
		var energyRes teslaCalendarHistoryResponse
		if err := b.getCalendarHistory(ctx, "energy", current, dayEnd, loc, tz, &energyRes); err != nil {
			return nil, err
		}

		now := time.Now().In(loc)
		// Aggregate energy data into hourly buckets
		for _, ts := range energyRes.TimeSeries {
			t, err := time.Parse(time.RFC3339, ts.Timestamp)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to parse timestamp", slog.String("timestamp", ts.Timestamp), slog.Any("error", err))
				continue
			}
			tInLoc := t.In(loc)
			if tInLoc.After(now) {
				continue
			}
			// The data is for the preceding period, so we subtract 1 minute to shift the
			// "end of hour" timestamp (e.g. 10:00:00) into the correct bucket (09:00:00).
			bucketT := tInLoc.Add(-time.Minute)
			hourKey := bucketT.Truncate(time.Hour).Format(time.RFC3339)
			if _, exists := hourlyStats[hourKey]; !exists {
				hourlyStats[hourKey] = &types.EnergyStats{
					TSHourStart:  bucketT.Truncate(time.Hour),
					TimeLocation: loc.String(),
				}
			}
			s := hourlyStats[hourKey]

			s.SolarKWH += ts.SolarEnergyExportedWH / 1000.0
			s.BatteryChargedKWH += (ts.BatteryEnergyImportedFromGridWH + ts.BatteryEnergyImportedFromSolarWH) / 1000.0
			s.BatteryUsedKWH += ts.BatteryEnergyExportedWH / 1000.0
			s.GridImportKWH += ts.GridEnergyImportedWH / 1000.0
			s.GridExportKWH += (ts.GridEnergyExportedFromSolarWH + ts.GridEnergyExportedFromBatteryWH) / 1000.0
			s.VPPExportKWH += ts.GridServicesEnergyExportedWH / 1000.0
			s.HomeKWH += (ts.ConsumerEnergyImportedFromGridWH + ts.ConsumerEnergyImportedFromSolarWH + ts.ConsumerEnergyImportedFromBatteryWH) / 1000.0
			s.SolarToHomeKWH += ts.ConsumerEnergyImportedFromSolarWH / 1000.0
			s.SolarToBatteryKWH += ts.BatteryEnergyImportedFromSolarWH / 1000.0
			s.SolarToGridKWH += ts.GridEnergyExportedFromSolarWH / 1000.0
			s.BatteryToHomeKWH += ts.ConsumerEnergyImportedFromBatteryWH / 1000.0
			s.BatteryToGridKWH += ts.GridEnergyExportedFromBatteryWH / 1000.0
		}

		// Fetch SOE history for this day but SOE is believed to be exact SOE values
		// for the timestamp so we don't need to fetch ahead
		if !current.After(endDay) {
			var soeRes teslaSOEResponse
			if err := b.getCalendarHistory(ctx, "soe", current, dayEnd, loc, tz, &soeRes); err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to get SOE history, continuing without SOC data", slog.Any("error", err))
			} else {
				// Merge SOE data into hourly buckets
				for _, soeEntry := range soeRes.TimeSeries {
					t, err := time.Parse(time.RFC3339, soeEntry.Timestamp)
					if err != nil {
						continue
					}
					tInLoc := t.In(loc)
					if tInLoc.After(now) {
						continue
					}
					hourKey := tInLoc.Truncate(time.Hour).Format(time.RFC3339)

					s, exists := hourlyStats[hourKey]
					if !exists {
						// SOE entry for hour without energy data; create a bucket
						s = &types.EnergyStats{
							TSHourStart:   tInLoc.Truncate(time.Hour),
							TimeLocation:  loc.String(),
							MinBatterySOC: soeEntry.SOE,
							MaxBatterySOC: soeEntry.SOE,
						}
						hourlyStats[hourKey] = s
						continue
					}

					if s.MinBatterySOC == 0 && s.MaxBatterySOC == 0 {
						// First SOE for this bucket
						s.MinBatterySOC = soeEntry.SOE
						s.MaxBatterySOC = soeEntry.SOE
					} else {
						if soeEntry.SOE < s.MinBatterySOC {
							s.MinBatterySOC = soeEntry.SOE
						}
						if soeEntry.SOE > s.MaxBatterySOC {
							s.MaxBatterySOC = soeEntry.SOE
						}
					}
				}
			}
		}

		current = current.AddDate(0, 0, 1)
	}

	dailyMap := make(map[string][]types.EnergyStats)
	var sortedDayKeys []string

	for _, s := range hourlyStats {
		dayStart := time.Date(s.TSHourStart.Year(), s.TSHourStart.Month(), s.TSHourStart.Day(), 0, 0, 0, 0, loc)
		dayKey := dayStart.Format("2006-01-02")
		if _, exists := dailyMap[dayKey]; !exists {
			sortedDayKeys = append(sortedDayKeys, dayKey)
		}
		dailyMap[dayKey] = append(dailyMap[dayKey], *s)
	}

	for _, list := range dailyMap {
		sort.Slice(list, func(i, j int) bool {
			return list[i].TSHourStart.Before(list[j].TSHourStart)
		})
	}

	sort.Strings(sortedDayKeys)

	var result []types.DailyEnergyStats
	for _, key := range sortedDayKeys {
		dayStart, err := time.ParseInLocation("2006-01-02", key, loc)
		if err != nil {
			return nil, fmt.Errorf("failed to parse day key %s: %w", key, err)
		}

		// Filter based on the requested start and end bounds per the overall intent,
		// but since we keep whole days, we just check if the day intersects the requested range reasonably
		// actually, let's just return the full days that we fetched!
		if dayStart.After(endDay) || dayStart.Before(startDay) {
			continue
		}

		result = append(result, types.DailyEnergyStats{
			TSDayStart:   dayStart,
			TimeLocation: loc.String(),
			Hourly:       dailyMap[key],
		})
	}

	return result, nil
}

func (b *baseTesla) RegisterTesla(ctx context.Context, domain string) error {
	log.Ctx(ctx).InfoContext(ctx, "registering tesla partner")
	for region, baseURL := range b.baseURLs {
		// 1. Get Partner Token
		// see https://developer.tesla.com/docs/fleet-api/authentication/partner-tokens
		data := url.Values{}
		data.Set("grant_type", "client_credentials")
		data.Set("scope", teslaScopes)
		res, err := b.doTokenRequest(ctx, baseURL, data)
		if err != nil {
			return fmt.Errorf("partner token request failed for region %s: %w", region, err)
		}

		// 2. Call Register Endpoint
		req, err := b.newPOSTRequest(ctx, "POST", "api/1/partner_accounts", res.AccessToken, baseURL, teslaRegisterRequest{
			Domain: domain,
		})
		if err != nil {
			return fmt.Errorf("partner register request failed for region %s: %w", region, err)
		}
		if err := b.doRequest(req, nil); err != nil {
			return fmt.Errorf("partner register request failed for region %s: %w", region, err)
		}
	}

	log.Ctx(ctx).InfoContext(ctx, "tesla partner register success")
	return nil
}

// TeslaPublicKeyPEM returns the public key PEM for the configured private key.
func (m *Map) TeslaPublicKeyPEM() string {
	if m.baseTesla == nil {
		return ""
	}
	return m.baseTesla.pubPEM
}

type teslaProduct struct {
	EnergySiteID int64  `json:"energy_site_id"`
	DeviceType   string `json:"device_type"`
	ResourceType string `json:"resource_type"`
	GatewayID    string `json:"gateway_id"`
	// this is sometimes a string and sometimes a number
	ID interface{} `json:"id"`
}

type teslaProductsResponse []teslaProduct

type teslaSiteInfoResponse struct {
	BackupReservePercent float64 `json:"backup_reserve_percent"`
	// DefaultRealMode can be autonomous or self_consumption
	DefaultRealMode      string              `json:"default_real_mode"`
	NameplateEnergyWH    float64             `json:"nameplate_energy"`
	NameplatePowerW      float64             `json:"nameplate_power"`
	BatteryCount         int                 `json:"battery_count"`
	InstallationTimeZone string              `json:"installation_time_zone"`
	Components           teslaSiteComponents `json:"components"`
	Version              string              `json:"version"`
	UserSettings         struct {
		StormModeEnabled bool `json:"storm_mode_enabled"`
	} `json:"user_settings"`
	TariffContentV2 map[string]any `json:"tariff_content_v2"`

	// there's also max_site_meter_power_ac and min_site_meter_power_ac
}

type teslaSiteComponents struct {
	Solar bool `json:"solar"`

	Battery     bool                         `json:"battery"`
	Batteries   []teslaSiteComponentsBattery `json:"batteries"`
	Gateways    []teslaSiteComponentsGateway `json:"gateways"`
	BatteryType string                       `json:"battery_type"`

	Grid             bool `json:"grid"`
	Backup           bool `json:"backup"`
	LoadMeter        bool `json:"load_meter"`
	StormModeCapable bool `json:"storm_mode_capable"`
	Configurable     bool `json:"configurable"`
	// can be pv_only or battery_ok
	CustomerPreferredExportRule              string `json:"customer_preferred_export_rule"`
	DisallowChargeFromGridWithSolarInstalled bool   `json:"disallow_charge_from_grid_with_solar_installed"`
	// can be pv_only or battery_ok
	NetMeterMode                  string `json:"net_meter_mode"`
	EditSettingEnergyExports      bool   `json:"edit_setting_energy_exports"`
	EditSettingGridCharging       bool   `json:"edit_setting_grid_charging"`
	EditSettingPermissionToExport bool   `json:"edit_setting_permission_to_export"`
}

type teslaSiteComponentsGateway struct {
	DeviceID             string  `json:"device_id"`
	SerialNumber         string  `json:"serial_number"`
	PartNumber           string  `json:"part_number"`
	PartName             string  `json:"part_name"`
	IsActive             bool    `json:"is_active"`
	SiteID               string  `json:"site_id"`
	FirmwareVersion      string  `json:"firmware_version"`
	UpdatedDatetime      string  `json:"updated_datetime"`
	NameplatePowerWatts  float64 `json:"nameplate_power_watts"`
	NameplateEnergyWatts float64 `json:"nameplate_energy_watts"`
}

type teslaSiteComponentsBattery struct {
	DeviceID                    string  `json:"device_id"`
	Active                      bool    `json:"is_active"`
	NameplateEnergyWH           float64 `json:"nameplate_energy"`
	NameplateMaxChargePowerW    float64 `json:"nameplate_max_charge_power"`
	NameplateMaxDischargePowerW float64 `json:"nameplate_max_discharge_power"`
	PartName                    string  `json:"part_name"`
	PartNumber                  string  `json:"part_number"`
	SerialNumber                string  `json:"serial_number"`
}

type teslaLiveStatusResponse struct {
	SolarPowerW        float64 `json:"solar_power"`
	BatteryPowerW      float64 `json:"battery_power"`
	GridPowerW         float64 `json:"grid_power"`
	LoadPowerW         float64 `json:"load_power"`
	PercentageCharged  float64 `json:"percentage_charged"`
	StormModeActive    bool    `json:"storm_mode_active"`
	GridServicesActive bool    `json:"grid_services_active"`
	GridServicesPowerW float64 `json:"grid_services_power"`
	// GridStatus can be "Active", not sure what else
	GridStatus string `json:"grid_status"`
	// IslandStatus can be "on_grid", not sure what else
	IslandStatus string `json:"island_status"`
}

type teslaRegisterRequest struct {
	Domain string `json:"domain"`
}

type teslaErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type teslaCalendarHistoryTimeSeries struct {
	Timestamp                           string  `json:"timestamp"`
	SolarEnergyExportedWH               float64 `json:"solar_energy_exported"`
	BatteryEnergyExportedWH             float64 `json:"battery_energy_exported"`
	BatteryEnergyImportedFromGridWH     float64 `json:"battery_energy_imported_from_grid"`
	BatteryEnergyImportedFromSolarWH    float64 `json:"battery_energy_imported_from_solar"`
	GridEnergyImportedWH                float64 `json:"grid_energy_imported"`
	GridEnergyExportedFromSolarWH       float64 `json:"grid_energy_exported_from_solar"`
	GridEnergyExportedFromBatteryWH     float64 `json:"grid_energy_exported_from_battery"`
	ConsumerEnergyImportedFromGridWH    float64 `json:"consumer_energy_imported_from_grid"`
	ConsumerEnergyImportedFromSolarWH   float64 `json:"consumer_energy_imported_from_solar"`
	ConsumerEnergyImportedFromBatteryWH float64 `json:"consumer_energy_imported_from_battery"`
	TotalHomeUsageWH                    float64 `json:"total_home_usage"`
	TotalSolarGenerationWH              float64 `json:"total_solar_generation"`
	TotalBatteryChargeWH                float64 `json:"total_battery_charge"`
	TotalGridEnergyExportedWH           float64 `json:"total_grid_energy_exported"`
	GridServicesEnergyExportedWH        float64 `json:"grid_services_energy_exported"`
}

type teslaCalendarHistoryResponse struct {
	SerialNumber string                           `json:"serial_number"`
	Period       string                           `json:"period"`
	TimeSeries   []teslaCalendarHistoryTimeSeries `json:"time_series"`
}

type teslaSOETimeSeries struct {
	Timestamp string  `json:"timestamp"`
	SOE       float64 `json:"soe"`
}

type teslaSOEResponse struct {
	SerialNumber string               `json:"serial_number"`
	Period       string               `json:"period"`
	TimeSeries   []teslaSOETimeSeries `json:"time_series"`
}
