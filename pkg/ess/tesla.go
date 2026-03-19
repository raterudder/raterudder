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
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
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
		parsed.RawQuery = q.Encode()
		baseOAuthURL = parsed.String()
	}

	return types.ESSProviderInfo{
		ID:   "tesla",
		Name: "Tesla (Beta)",
		OAuthURLs: map[string]string{
			"default": baseOAuthURL,
		},
		Credentials: []types.ESSCredentialField{
			{
				Field:    "authCode",
				Name:     "Authorization Code",
				Type:     types.ESSCredentialFieldTypePassword,
				Required: true,
			},
			{
				Field:       "energySiteID",
				Name:        "Energy Site ID (Optional)",
				Type:        types.ESSCredentialFieldTypeString,
				Required:    false,
				Description: "If left empty, RateRudder will attempt to auto-discover the site ID.",
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
		if len(body) == 0 {
			return fmt.Errorf("unexpected tesla response status %d", resp.StatusCode)
		}
		var errBody teslaErrorResponse
		if err := json.Unmarshal(body, &errBody); err == nil && errBody.ErrorDescription != "" {
			return fmt.Errorf("unexpected tesla response status %d: %s", resp.StatusCode, errBody.ErrorDescription)
		}
		if len(body) > 256 {
			body = body[:256]
		}
		return fmt.Errorf("unexpected tesla response status %d: %s", resp.StatusCode, string(body))
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
	base           *baseTesla
	settings       types.Settings
	mu             sync.Mutex
	token          string
	baseURL        string
	energySiteID   int64
	siteInfoCache  teslaSiteInfoResponse
	siteInfoExpiry time.Time
}

func newTesla(b *baseTesla) *Tesla {
	return &Tesla{
		base: b,
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
			id, err := b.getDefaultSiteID(ctx)
			if err != nil {
				return creds, false, fmt.Errorf("failed to get default site id: %w", err)
			}
			log.Ctx(ctx).InfoContext(ctx, "automatically selected site", slog.Int64("energySiteID", id))
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

	if creds.Tesla.EnergySiteID == 0 {
		return creds, false, fmt.Errorf("missing energy site ID: %w", ErrCredentialsMissing)
	}

	b.energySiteID = creds.Tesla.EnergySiteID

	// Verify token works by fetching site info
	if _, err := b.getSiteInfoWithCache(ctx, true); err != nil {
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

	var response struct {
		Response json.RawMessage `json:"response"`
	}
	if err := b.base.doRequest(req, &response); err != nil {
		return err
	}

	// debug log the whole response which will aid in debugging
	log.Ctx(ctx).DebugContext(ctx,
		"tesla result",
		slog.String("url", req.URL.String()),
		slog.String("method", req.Method),
		slog.Any("response", response.Response),
	)
	if err := json.Unmarshal(response.Response, dest); err != nil {
		return err
	}
	return nil
}

func (b *Tesla) getDefaultSiteID(ctx context.Context) (int64, error) {
	var res teslaProductsResponse
	if err := b.doGETRequest(ctx, "api/1/products", nil, &res); err != nil {
		return 0, err
	}
	for _, p := range res {
		if p.DeviceType == "energy" && p.ResourceType == "battery" && p.EnergySiteID != 0 {
			return p.EnergySiteID, nil
		}
	}
	return 0, fmt.Errorf("no energy site found")
}

func (b *Tesla) getSiteInfo(ctx context.Context) (teslaSiteInfoResponse, error) {
	siteInfoPath := fmt.Sprintf("api/1/energy_sites/%d/site_info", b.energySiteID)
	var siteInfo teslaSiteInfoResponse
	if err := b.doGETRequest(ctx, siteInfoPath, nil, &siteInfo); err != nil {
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

// GetStatus returns the current status of the system.
func (b *Tesla) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting tesla system status")
	b.mu.Lock()
	defer b.mu.Unlock()

	siteInfo, err := b.getSiteInfoWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	liveStatusPath := fmt.Sprintf("api/1/energy_sites/%d/live_status", b.energySiteID)
	var liveStatus teslaLiveStatusResponse
	if err := b.doGETRequest(ctx, liveStatusPath, nil, &liveStatus); err != nil {
		return types.SystemStatus{}, err
	}

	var totalChargeKW float64
	var totalDischargeKW float64
	for _, b := range siteInfo.Components.Batteries {
		totalChargeKW += b.NameplateMaxChargePowerW / 1000.0
		totalDischargeKW += b.NameplateMaxDischargePowerW / 1000.0
	}

	status := types.SystemStatus{
		Timestamp:             time.Now(),
		BatterySOC:            liveStatus.PercentageCharged,
		BatteryKW:             liveStatus.BatteryPowerW / 1000.0,
		BatteryCapacityKWH:    siteInfo.NameplateEnergyWH / 1000.0,
		MaxBatteryDischargeKW: totalDischargeKW,
		MaxBatteryChargeKW:    totalChargeKW,
		SolarKW:               liveStatus.SolarPowerW / 1000.0,
		GridKW:                liveStatus.GridPowerW / 1000.0,
		HomeKW:                liveStatus.LoadPowerW / 1000.0,
		ElevatedMinBatterySOC: siteInfo.BackupReservePercent > 0 && siteInfo.BackupReservePercent > b.settings.MinBatterySOC,
		BatteryAboveMinSOC:    liveStatus.PercentageCharged >= siteInfo.BackupReservePercent,
		EmergencyMode:         liveStatus.StormModeActive,
		// TODO: how do we know when battery charging is disabled
		// TODO: what about alarms?
	}

	return status, nil
}

// SetModes sets the operating modes of the system.
func (b *Tesla) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	log.Ctx(ctx).DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol))
	b.mu.Lock()
	defer b.mu.Unlock()

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return nil
	}

	siteInfo, err := b.getSiteInfoWithCache(ctx, false)
	if err != nil {
		return err
	}

	liveStatusPath := fmt.Sprintf("api/1/energy_sites/%d/live_status", b.energySiteID)
	var liveStatus teslaLiveStatusResponse
	if err := b.doGETRequest(ctx, liveStatusPath, nil, &liveStatus); err != nil {
		return err
	}

	if liveStatus.StormModeActive {
		log.Ctx(ctx).InfoContext(ctx, "device is in storm mode, skipping set modes")
		return errors.New("device is in storm mode")
	}

	reserveSOC := siteInfo.BackupReservePercent
	newReserveSOC := reserveSOC
	allowGridCharge := !siteInfo.Components.DisallowChargeFromGridWithSolarInstalled
	var updatedGrid bool
	var updatedMode bool

	switch bat {
	case types.BatteryModeChargeAny:
		// if they want to charge the battery then set the SOC to 100 to force it to
		// charge if its not charging already
		newReserveSOC = 100
		if b.settings.GridChargeBatteries {
			if !allowGridCharge {
				allowGridCharge = true
				updatedGrid = true
			}
		} else {
			if allowGridCharge {
				allowGridCharge = false
				updatedGrid = true
			}
		}
	case types.BatteryModeChargeSolar:
		// we disallow charging from the grid if they only want to charge via solar
		// and otherwise set the SOC to 100
		newReserveSOC = 100
		if allowGridCharge {
			allowGridCharge = false
			updatedGrid = true
		}
	case types.BatteryModeLoad:
		// we set the SOC to the minimum battery SOC to ensure we start discharging
		// if we're somehow less than this soc, we'll charge from the solar, unless
		// solar is unavailable then it'll charge from the grid
		newReserveSOC = b.settings.MinBatterySOC
		if b.settings.GridChargeBatteries {
			if !allowGridCharge {
				allowGridCharge = true
				updatedGrid = true
			}
		} else {
			if allowGridCharge {
				allowGridCharge = false
				updatedGrid = true
			}
		}
	case types.BatteryModeStandby:
		// we floor the SOC to ensure we don't set it to a value that would cause the
		// battery to charge
		// make sure we don't set it to less than the minimum battery SOC
		newReserveSOC = math.Max(math.Floor(liveStatus.PercentageCharged), b.settings.MinBatterySOC)
		if allowGridCharge {
			allowGridCharge = false
			updatedGrid = true
		}
	case types.BatteryModeNoChange:
		// Do not change battery settings
	default:
		return fmt.Errorf("unknown battery mode: %v", bat)
	}

	if newReserveSOC < 5 {
		newReserveSOC = 5
	}
	updatedSOC := math.Round(newReserveSOC) != math.Round(reserveSOC)

	exportRule := siteInfo.Components.CustomerPreferredExportRule
	switch sol {
	case types.SolarModeAny:
		if b.settings.GridExportSolar && b.settings.GridExportBatteries {
			if exportRule != teslaExportRuleBatteryOk {
				exportRule = teslaExportRuleBatteryOk
				updatedGrid = true
			}
		} else if b.settings.GridExportSolar {
			if exportRule != teslaExportRulePvOnly {
				exportRule = teslaExportRulePvOnly
				updatedGrid = true
			}
		} else {
			if exportRule != teslaExportRuleNever {
				exportRule = teslaExportRuleNever
				updatedGrid = true
			}
		}
	case types.SolarModeNoExport:
		if exportRule != teslaExportRuleNever {
			exportRule = teslaExportRuleNever
			updatedGrid = true
		}
	case types.SolarModeNoChange:
		// Do nothing
	default:
		return fmt.Errorf("unknown solar mode: %v", sol)
	}

	if siteInfo.DefaultRealMode != "self_consumption" {
		updatedMode = true
	}

	if b.settings.DryRun {
		if updatedMode {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated operation mode", slog.String("mode", "self_consumption"))
		}
		if updatedSOC {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated backup reserve", slog.Float64("soc", newReserveSOC))
		}
		if updatedGrid {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated grid import export", slog.Bool("allowGridCharge", allowGridCharge), slog.String("exportRule", exportRule))
		}
		return nil
	}

	if updatedMode {
		log.Ctx(ctx).InfoContext(ctx, "updating tesla operation mode", slog.String("mode", "self_consumption"))
		path := fmt.Sprintf("api/1/energy_sites/%d/operation", b.energySiteID)
		payload := map[string]string{"default_real_mode": "self_consumption"}
		req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
		if err != nil {
			return err
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla operation mode", slog.Any("error", err))
			return err
		}
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
			return err
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla backup reserve", slog.Any("error", err))
			return err
		}
	}

	if updatedGrid {
		log.Ctx(ctx).InfoContext(
			ctx,
			"updating tesla grid import export",
			slog.Bool("allowGridCharge", allowGridCharge),
			slog.String("exportRule", exportRule),
		)
		path := fmt.Sprintf("api/1/energy_sites/%d/grid_import_export", b.energySiteID)
		payload := map[string]any{
			"disallow_charge_from_grid_with_solar_installed": !allowGridCharge,
			"customer_preferred_export_rule":                 exportRule,
		}
		req, err := b.base.newPOSTRequest(ctx, "POST", path, b.token, b.baseURL, payload)
		if err != nil {
			return err
		}
		if err := b.base.doRequest(req, nil); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to update tesla grid import export", slog.Any("error", err))
			return err
		}
	}

	return nil
}

// GetEnergyHistory returns the energy history for the specified period.
func (b *Tesla) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting tesla energy history", slog.String("start", start.String()), slog.String("end", end.String()))
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

	historyPath := fmt.Sprintf("api/1/energy_sites/%d/calendar_history", b.energySiteID)
	baseParams := url.Values{}
	baseParams.Set("period", "day")
	baseParams.Set("time_zone", tz)
	baseParams.Set("start_date", start.In(loc).Format(time.RFC3339))
	baseParams.Set("end_date", end.In(loc).Format(time.RFC3339))

	// Fetch energy history
	energyParams := url.Values{}
	for k, v := range baseParams {
		energyParams[k] = v
	}
	energyParams.Set("kind", "energy")

	var energyRes teslaCalendarHistoryResponse
	if err := b.doGETRequest(ctx, historyPath, energyParams, &energyRes); err != nil {
		return nil, err
	}

	// Aggregate energy data into hourly buckets
	hourlyStats := make(map[string]*types.EnergyStats)
	for _, ts := range energyRes.TimeSeries {
		t, err := time.Parse(time.RFC3339, ts.Timestamp)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse timestamp", slog.String("timestamp", ts.Timestamp), slog.Any("error", err))
			continue
		}
		tInLoc := t.In(loc)

		hourKey := tInLoc.Truncate(time.Hour).Format(time.RFC3339)
		if _, exists := hourlyStats[hourKey]; !exists {
			hourlyStats[hourKey] = &types.EnergyStats{
				TSHourStart: tInLoc.Truncate(time.Hour),
			}
		}
		s := hourlyStats[hourKey]

		s.SolarKWH += ts.SolarEnergyExportedWH / 1000.0
		s.BatteryChargedKWH += (ts.BatteryEnergyImportedFromGridWH + ts.BatteryEnergyImportedFromSolarWH) / 1000.0
		s.BatteryUsedKWH += ts.BatteryEnergyExportedWH / 1000.0
		s.GridImportKWH += ts.GridEnergyImportedWH / 1000.0
		s.GridExportKWH += (ts.GridEnergyExportedFromSolarWH + ts.GridEnergyExportedFromBatteryWH) / 1000.0
		s.HomeKWH += (ts.ConsumerEnergyImportedFromGridWH + ts.ConsumerEnergyImportedFromSolarWH + ts.ConsumerEnergyImportedFromBatteryWH) / 1000.0
		s.SolarToHomeKWH += ts.ConsumerEnergyImportedFromSolarWH / 1000.0
		s.SolarToBatteryKWH += ts.BatteryEnergyImportedFromSolarWH / 1000.0
		s.SolarToGridKWH += ts.GridEnergyExportedFromSolarWH / 1000.0
		s.BatteryToHomeKWH += ts.ConsumerEnergyImportedFromBatteryWH / 1000.0
		s.BatteryToGridKWH += ts.GridEnergyExportedFromBatteryWH / 1000.0
	}

	// Fetch SOE (state of energy) history for min/max battery SOC
	soeParams := url.Values{}
	for k, v := range baseParams {
		soeParams[k] = v
	}
	soeParams.Set("kind", "soe")

	var soeRes teslaSOEResponse
	if err := b.doGETRequest(ctx, historyPath, soeParams, &soeRes); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get SOE history, continuing without SOC data", slog.Any("error", err))
	}

	// Merge SOE data into hourly buckets for min/max battery SOC
	for _, soeEntry := range soeRes.TimeSeries {
		t, err := time.Parse(time.RFC3339, soeEntry.Timestamp)
		if err != nil {
			continue
		}
		tInLoc := t.In(loc)
		hourKey := tInLoc.Truncate(time.Hour).Format(time.RFC3339)

		s, exists := hourlyStats[hourKey]
		if !exists {
			// SOE entry for hour without energy data; create a bucket
			s = &types.EnergyStats{
				TSHourStart:   tInLoc.Truncate(time.Hour),
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

	var allStats []types.EnergyStats
	for _, s := range hourlyStats {
		if !s.TSHourStart.Before(start) && s.TSHourStart.Before(end) {
			allStats = append(allStats, *s)
		}
	}

	return allStats, nil
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

	// there's also max_site_meter_power_ac and min_site_meter_power_ac
}

type teslaSiteComponents struct {
	Solar bool `json:"solar"`

	Battery     bool                         `json:"battery"`
	Batteries   []teslaSiteComponentsBattery `json:"batteries"`
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
	NetMeterMode string `json:"net_meter_mode"`
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
	SolarPowerW       float64 `json:"solar_power"`
	BatteryPowerW     float64 `json:"battery_power"`
	GridPowerW        float64 `json:"grid_power"`
	LoadPowerW        float64 `json:"load_power"`
	PercentageCharged float64 `json:"percentage_charged"`
	StormModeActive   bool    `json:"storm_mode_active"`
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
