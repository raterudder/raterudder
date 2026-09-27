package ess

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/time/rate"
)

const franklinLoginPath = "hes-gateway/terminal/initialize/appUserOrInstallerLogin"

// Franklin implements the System interface for FranklinWH.
// It interacts with the FranklinWH API to monitor and control the energy storage system.
type Franklin struct {
	client               *http.Client
	baseURL              string
	username             string
	md5Password          string
	gatewayID            string
	tokenStr             string
	mu                   sync.Mutex
	settings             types.Settings
	deviceInfoCache      franklinDeviceInfoV2Result
	deviceInfoExpiry     time.Time
	powerCapConfigCache  []franklinPowerCapConfigResult
	powerCapConfigExpiry time.Time
	runtimeDataCache     franklinDeviceCompositeInfoResult
	runtimeDataExpiry    time.Time

	// retry delays for getDeviceCompositeInfo valid: false failures
	retryDelay1 time.Duration
	retryDelay2 time.Duration
}

type franklinWorkMode int

const (
	franklinWorkModeTimeOfUse           franklinWorkMode = 1 // Time-of-Use mode (TOU)
	franklinWorkModeSelfConsumption     franklinWorkMode = 2 // Self-Consumption mode
	franklinWorkModeEmergencyBackup     franklinWorkMode = 3 // Emergency Backup mode (reserve SOC locked to 100%)
	franklinWorkModeSmartEnergyDispatch franklinWorkMode = 7 // Smart Energy Dispatch mode (cloud AI-driven optimization)

	// Special runtime overlay mode IDs reported in RuntimeData.TOUID (JSON "mode"):
	franklinOverlayModeStormHedge franklinWorkMode = 6 // Storm Hedge active override (grid charge, 100% reserve SOC)
	franklinOverlayModeVPP        franklinWorkMode = 9 // Virtual Power Plant (VPP) demand response event active

	// WorkMode aliases:
	franklinWorkModeStormHedge    = franklinOverlayModeStormHedge
	franklinWorkModeVPP           = franklinOverlayModeVPP
	franklinWorkModeSmartDispatch = franklinWorkModeSmartEnergyDispatch
)

func (m franklinWorkMode) String() string {
	switch m {
	case franklinWorkModeTimeOfUse:
		return "Time-of-Use"
	case franklinWorkModeSelfConsumption:
		return "Self-Consumption"
	case franklinWorkModeEmergencyBackup:
		return "Emergency Backup"
	case franklinWorkModeSmartEnergyDispatch:
		return "Smart Energy Dispatch"
	case franklinOverlayModeStormHedge:
		return "Storm Hedge"
	case franklinOverlayModeVPP:
		return "VPP"
	default:
		return fmt.Sprintf("WorkMode(%d)", int(m))
	}
}

type franklinMode struct {
	ID                int
	Name              string
	WorkMode          franklinWorkMode
	OldIndex          int
	ElectricityType   int
	ReserveSOC        float64
	CanEditReserveSOC bool
}

func newFranklin() *Franklin {
	return &Franklin{
		client:      common.HTTPClient(time.Minute),
		baseURL:     "https://energy.franklinwh.com",
		retryDelay1: 5 * time.Second,
		retryDelay2: 10 * time.Second,
	}
}

func franklinInfo() types.ESSProviderInfo {
	return types.ESSProviderInfo{
		ID:   "franklin",
		Name: "FranklinWH",
		Credentials: []types.ESSCredentialField{
			{
				Field:    "username",
				Name:     "Email",
				Type:     types.ESSCredentialFieldTypeString,
				Required: true,
			},
			{
				Field:    "password",
				Name:     "Password",
				Type:     types.ESSCredentialFieldTypePassword,
				Required: true,
			},
			{
				Field:       "gatewayID",
				Name:        "Gateway ID (Optional)",
				Type:        types.ESSCredentialFieldTypeString,
				Required:    false,
				Description: "If left empty, RateRudder will attempt to auto-discover the gateway ID.",
			},
		},
	}
}

func (f *Franklin) Name() string {
	return "franklin"
}

// ApplySettings applies the given settings to the Franklin struct.
func (f *Franklin) ApplySettings(ctx context.Context, settings types.Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = settings
	return nil
}

// Authenticate logs into franklin and fetches the default gateway if its not
// filled in. If a valid token is already stored in creds, it is restored to
// avoid an unnecessary login round-trip. A fresh login is only performed when
// the username/password has changed or no stored token is available. After a
// successful login the new token is written back into creds so the caller can
// persist it.
func (f *Franklin) Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error) {
	if creds.Franklin == nil || creds.Franklin.Username == "" || (creds.Franklin.Password == "" && creds.Franklin.MD5Password == "") {
		return creds, false, ErrCredentialsMissing
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var changed bool
	var currentMD5 string
	if creds.Franklin.Password != "" {
		// MD5 is mandated by the FranklinWH API protocol for authentication.
		// It is not used for secure password storage in RateRudder.
		// codeql[go/insecure-hashing]
		// nolint:gosec
		hash := md5.Sum([]byte(creds.Franklin.Password))
		currentMD5 = hex.EncodeToString(hash[:])
		if creds.Franklin.MD5Password != "" {
			// Clear deprecated MD5Password if we are migrating to raw password
			creds.Franklin.MD5Password = ""
			changed = true
		}
	} else if creds.Franklin.MD5Password != "" {
		currentMD5 = creds.Franklin.MD5Password
	}

	// Determine if we need a fresh login. We need one when:
	// - There is no cached token in the supplied credentials (first time), OR
	// - The username/password in the incoming credentials differ from what we
	//   already have verified (detected by comparing against stored struct state
	//   only when we have previously authenticated with those credentials).
	needLogin := creds.Franklin.Token == ""
	if !needLogin && f.username != "" {
		// We've previously authenticated; check if credentials have changed.
		needLogin = f.username != creds.Franklin.Username || f.md5Password != currentMD5
	}

	if needLogin {
		log.Ctx(ctx).DebugContext(ctx, "logging in to franklin")
		// Credentials changed or no cached token — must login fresh.
		token, err := f.login(ctx, creds.Franklin.Username, currentMD5)
		if err != nil {
			return creds, false, err
		}
		f.username = creds.Franklin.Username
		f.md5Password = currentMD5
		f.tokenStr = token
		// Persist the new token so we can skip login next time.
		creds.Franklin.Token = token

		// fill in the default gatewayID if it wasn't sent
		if creds.Franklin.GatewayID == "" {
			id, err := f.getDefaultGatewayID(ctx)
			if err != nil {
				return creds, false, err
			}
			log.Ctx(ctx).InfoContext(ctx, "automatically selected gateway", slog.String("gatewayID", id))
			creds.Franklin.GatewayID = id
		}
		changed = true
	} else {
		log.Ctx(ctx).DebugContext(ctx, "restored franklin credentials from cache")
		// Restore the token from credentials so we can skip login.
		f.username = creds.Franklin.Username
		f.md5Password = currentMD5
		f.tokenStr = creds.Franklin.Token
	}

	if creds.Franklin.GatewayID == "" {
		return creds, false, fmt.Errorf("missing gateway ID: %w", ErrCredentialsMissing)
	}

	f.gatewayID = creds.Franklin.GatewayID

	// Validate the credentials by fetching device info. This confirms the token
	// and gateway ID are working. The result is cached so the subsequent
	// GetStatus call will reuse it for free.
	if _, err := f.getDeviceInfoWithCache(ctx, true); err != nil {
		// check if we got an invalid token error and try to refresh the token
		if strings.Contains(err.Error(), "invalid token") {
			log.Ctx(ctx).DebugContext(ctx, "franklin token expired", slog.Any("error", err))
			// Credentials changed or no cached token — must login fresh.
			var token string
			token, err = f.login(ctx, f.username, f.md5Password)
			if err != nil {
				return creds, false, err
			}
			f.tokenStr = token
			// Persist the new token so we can skip login next time.
			creds.Franklin.Token = token
			changed = true
			_, err = f.getDeviceInfoWithCache(ctx, true)
		}
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "franklin credential validation failed", slog.Any("error", err))
			return creds, false, fmt.Errorf("credential validation failed: %w", err)
		}
	}

	return creds, changed, nil
}

type loginResult struct {
	UserID  int    `json:"userId"`
	Token   string `json:"token"`
	Version string `json:"version"`
}

func (f *Franklin) login(ctx context.Context, username, md5Password string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("missing username: %w", ErrCredentialsMissing)
	}
	if md5Password == "" {
		return "", fmt.Errorf("missing password: %w", ErrCredentialsMissing)
	}

	data := url.Values{}
	data.Set("account", username)
	data.Set("password", md5Password)
	data.Set("type", "0")

	req, err := f.newPostFormRequest(ctx, franklinLoginPath, data)
	if err != nil {
		return "", err
	}

	var res loginResult
	if err := f.doRequest(req, &res); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "franklin login failed", slog.Any("error", err))
		return "", fmt.Errorf("login failed: %w", err)
	}
	log.Ctx(ctx).DebugContext(ctx, "franklin login success", slog.String("username", username))

	return res.Token, nil
}

func (f *Franklin) newPostFormRequest(ctx context.Context, endpoint string, data url.Values) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	body := strings.NewReader(data.Encode())
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func (f *Franklin) newGetRequest(ctx context.Context, endpoint string, params url.Values) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	u.RawQuery = params.Encode()
	return http.NewRequestWithContext(ctx, "GET", u.String(), nil)
}

func (f *Franklin) newPostQueryRequest(ctx context.Context, endpoint string, params url.Values) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	u.RawQuery = params.Encode()
	return http.NewRequestWithContext(ctx, "POST", u.String(), nil)
}

func (f *Franklin) newPostJSONRequest(ctx context.Context, endpoint string, data any) (*http.Request, error) {
	u, err := url.Parse(f.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, endpoint)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

type franklinResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
	Success bool            `json:"success"`
}

func (f *Franklin) doRequest(req *http.Request, dest any) error {
	var isLogin bool
	if strings.HasSuffix(req.URL.Path, franklinLoginPath) {
		isLogin = true
	} else {
		req.Header.Set("logintoken", f.tokenStr)
	}

	// TODO: should we set softwareversion, optsystemversion, opttime, optdevicename, optsource, optdevice
	// we don't know what to set them as for now but at some point we should consider setting them
	// once we better understand how Franklin expects them
	req.Header.Set("lang", "EN_US")

	resp, err := f.client.Do(req)
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
			return fmt.Errorf("unexpected franklin status code %d", resp.StatusCode)
		}
		if len(body) > 256 {
			body = body[:256]
		}
		return fmt.Errorf("unexpected franklin status code %d: %s", resp.StatusCode, string(body))
	}

	var fr franklinResponse
	if err := json.Unmarshal(body, &fr); err != nil {
		if len(body) > 256 {
			body = body[:256]
		}
		log.Ctx(req.Context()).ErrorContext(req.Context(), "failed to decode franklin response", slog.Any("error", err), slog.String("body", string(body)))
		return err
	}

	if !fr.Success && fr.Code != 200 {
		if dest == nil && strings.Contains(fr.Message, "Saved successfully") && strings.Contains(fr.Message, "synchronized later") {
			log.Ctx(req.Context()).WarnContext(req.Context(), "franklin api warning: Saved successfully. The data will be synchronized later", slog.String("url", req.URL.String()))
			return nil
		}
		if fr.Message == "" {
			if len(body) > 256 {
				body = body[:256]
			}
			log.Ctx(req.Context()).ErrorContext(req.Context(), "franklin api unknown error", slog.String("body", string(body)))
			return fmt.Errorf("franklin unknown error")
		}
		return fmt.Errorf("franklin api error: %s", fr.Message)
	}

	// debug log the whole response which will aid in debugging but not if its a login response
	if !isLogin {
		log.Ctx(req.Context()).DebugContext(
			req.Context(),
			"franklin result",
			slog.String("url", req.URL.String()),
			slog.String("method", req.Method),
			slog.Any("response", fr.Result),
		)
	}

	if dest != nil {
		if err := json.Unmarshal(fr.Result, dest); err != nil {
			log.Ctx(req.Context()).ErrorContext(req.Context(), "failed to decode franklin result", slog.Any("error", err))
			return fmt.Errorf("failed to decode franklin result: %w", err)
		}
	}
	return nil
}

func (f *Franklin) getRuntimeData(ctx context.Context) (franklinDeviceCompositeInfoResult, error) {
	var res franklinDeviceCompositeInfoResult
	for attempt := 1; attempt <= 3; attempt++ {
		refreshFlag := "1"
		if attempt == 2 {
			refreshFlag = "0"
		}

		params := url.Values{}
		params.Set("gatewayId", f.gatewayID)
		params.Set("refreshFlag", refreshFlag)

		req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getDeviceCompositeInfo", params)
		if err != nil {
			return franklinDeviceCompositeInfoResult{}, err
		}

		res = franklinDeviceCompositeInfoResult{}
		if err := f.doRequest(req, &res); err != nil {
			return franklinDeviceCompositeInfoResult{}, fmt.Errorf("getDeviceCompositeInfo failed: %w", err)
		}

		var dataTime time.Time
		var age time.Duration
		isStale := false

		// 0 seems to be returned if refreshFlag=0
		if res.Valid && res.RuntimeData.Timestamp > 0 {
			dataTime = time.Unix(res.RuntimeData.Timestamp, 0)
			age = time.Since(dataTime)
			if age > 5*time.Minute {
				isStale = true
				res.Valid = false
			}
		}

		if res.Valid {
			break
		}

		if attempt < 3 {
			delay := f.retryDelay1
			if attempt == 2 {
				delay = f.retryDelay2
			}
			if delay > 0 {
				delay = delay + rand.N(delay)
			}
			attrs := []any{
				slog.Int("attempt", attempt),
				slog.String("refreshFlag", refreshFlag),
				slog.Duration("delay", delay),
			}
			if isStale {
				attrs = append(attrs,
					slog.Bool("stale", true),
					slog.Duration("age", age),
					slog.Time("timestamp", dataTime),
				)
			}
			log.Ctx(ctx).WarnContext(
				ctx,
				"getDeviceCompositeInfo returned invalid or stale status, retrying",
				attrs...,
			)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return franklinDeviceCompositeInfoResult{}, ctx.Err()
			}
		}
	}

	if !res.Valid {
		return franklinDeviceCompositeInfoResult{}, fmt.Errorf("getDeviceCompositeInfo returned invalid status")
	}

	// solar inverters/combiners use power so it can be negative, just set it to 0
	// sometimes the PowerSolar field is stuck (particularly when report_type: 2),
	// we should decide if we want to rely on the solarPower field which is ~10x the p_sun field
	// but for now we're changing the refreshFlag to 1 and seeing if that helps
	if res.RuntimeData.PowerSolar < 0 {
		res.RuntimeData.PowerSolar = 0
	}

	log.Ctx(ctx).DebugContext(ctx, "franklin runtime data",
		slog.Float64("soc", res.RuntimeData.SOC),
		slog.Float64("solarKW", res.RuntimeData.PowerSolar),
		slog.Float64("gridKW", res.RuntimeData.PowerGrid),
		slog.Float64("loadKW", res.RuntimeData.PowerLoad),
		slog.Float64("batteryKW", res.RuntimeData.PowerBattery),
		slog.Int("alarms", len(res.CurrentAlarmList)),
		slog.Int("mode", int(res.RuntimeData.TOUID)),
		slog.Int64("timestamp", res.RuntimeData.Timestamp),
	)

	return res, nil
}

func (f *Franklin) getRuntimeDataWithCache(ctx context.Context, refresh bool) (franklinDeviceCompositeInfoResult, error) {
	if !refresh && time.Now().Before(f.runtimeDataExpiry) {
		return f.runtimeDataCache, nil
	}
	rd, err := f.getRuntimeData(ctx)
	if err != nil {
		return franklinDeviceCompositeInfoResult{}, err
	}
	f.runtimeDataCache = rd
	// we are really only trying to keep this around for the update call which
	// calls SetMode and GetStatus
	f.runtimeDataExpiry = time.Now().Add(time.Minute)
	return rd, nil
}

func (f *Franklin) getDefaultGatewayID(ctx context.Context) (string, error) {
	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getHomeGatewayList", nil)
	if err != nil {
		return "", err
	}

	var list []franklinHomeGateway
	if err := f.doRequest(req, &list); err != nil {
		return "", err
	}

	if len(list) == 1 {
		return list[0].ID, nil
	}
	return "", fmt.Errorf("found %d gateways, expected 1", len(list))
}

func (f *Franklin) getDeviceInfo(ctx context.Context) (franklinDeviceInfoV2Result, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("lang", "en_US")

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getDeviceInfoV2", params)
	if err != nil {
		return franklinDeviceInfoV2Result{}, err
	}

	var res franklinDeviceInfoV2Result
	if err := f.doRequest(req, &res); err != nil {
		return franklinDeviceInfoV2Result{}, err
	}

	loc, err := time.LoadLocation(res.TimeZone)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to load location, defaulting to UTC", slog.String("tz", res.TimeZone), slog.Any("error", err))
		loc = time.UTC
	}
	res.location = loc

	return res, nil
}

// getDeviceInfoWithCache returns cached device info if still fresh, otherwise
// fetches it from the API and updates the cache. Must be called with f.mu held.
func (f *Franklin) getDeviceInfoWithCache(ctx context.Context, refresh bool) (franklinDeviceInfoV2Result, error) {
	if !refresh && time.Now().Before(f.deviceInfoExpiry) {
		return f.deviceInfoCache, nil
	}
	di, err := f.getDeviceInfo(ctx)
	if err != nil {
		return franklinDeviceInfoV2Result{}, err
	}
	f.deviceInfoCache = di
	f.deviceInfoExpiry = time.Now().Add(time.Minute)
	return di, nil
}

func (f *Franklin) getPowerCapacityConfig(ctx context.Context) ([]franklinPowerCapConfigResult, error) {
	req, err := f.newGetRequest(ctx, "hes-gateway/common/getPowerCapConfigList", nil)
	if err != nil {
		return nil, err
	}

	var res []franklinPowerCapConfigResult
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (f *Franklin) getPowerCapacityConfigWithCache(ctx context.Context) ([]franklinPowerCapConfigResult, error) {
	if time.Now().Before(f.powerCapConfigExpiry) {
		return f.powerCapConfigCache, nil
	}
	pc, err := f.getPowerCapacityConfig(ctx)
	if err != nil {
		return nil, err
	}
	f.powerCapConfigCache = pc
	f.powerCapConfigExpiry = time.Now().Add(24 * time.Hour)
	return pc, nil
}

// GetStatus returns the status of the franklin system
func (f *Franklin) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting franklin system status")
	f.mu.Lock()
	defer f.mu.Unlock()

	rd, err := f.getRuntimeDataWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	di, err := f.getDeviceInfoWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	modes, err := f.getAvailableModes(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	pcaps, err := f.getPowerCapacityConfigWithCache(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	var alarms []types.SystemAlarm
	for _, alarm := range rd.CurrentAlarmList {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", alarm.Time, di.location)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse alarmtime", slog.String("time", alarm.Time), slog.Any("error", err))
		}
		log.Ctx(ctx).DebugContext(
			ctx,
			"franklin alarm in status",
			slog.String("name", alarm.Name),
			slog.String("description", alarm.Explanation),
			slog.Time("time", t),
			slog.String("code", alarm.AlarmCode),
		)
		if alarm.Name == "SIM card not inserted" || alarm.Name == "4G module not connected" {
			continue
		}
		if alarm.Name == "No PV Current Detected" {
			log.Ctx(ctx).InfoContext(ctx, "ignoring alarm: No PV Current Detected", slog.String("alarmName", alarm.Name), slog.String("alarmCode", alarm.AlarmCode))
			continue
		}

		alarms = append(alarms, types.SystemAlarm{
			Name:        alarm.Name,
			Description: alarm.Explanation,
			Timestamp:   t,
			Code:        alarm.AlarmCode,
		})
	}

	var batteryChargingDisabled bool
	if len(alarms) == 1 && strings.Contains(alarms[0].Name, "BMS Charge Under Temperature") {
		log.Ctx(ctx).InfoContext(ctx, "bms charge under temperature is the only alarm, ignoring and setting battery charging disabled")
		alarms = nil
		batteryChargingDisabled = true
	}

	stormHedge := rd.RuntimeData.TOUID == franklinOverlayModeStormHedge
	vppActive := rd.RuntimeData.TOUID == franklinOverlayModeVPP

	var storms []types.Storm
	if stormHedge {
		sres, err := f.getStormList(ctx)
		if err != nil {
			return types.SystemStatus{}, err
		}
		for _, storm := range sres {
			t, err := time.ParseInLocation("2006-01-02 15:04:05", storm.Onset, di.location)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to parse storm onset time", slog.String("time", storm.Onset), slog.Any("error", err))
			}
			log.Ctx(ctx).DebugContext(
				ctx,
				"franklin reporting storm",
				slog.String("severity", storm.Severity),
				slog.Time("onset", t),
				slog.Int("durationMins", storm.DurationMins),
			)
			storms = append(storms, types.Storm{
				Description: storm.Severity,
				TSStart:     t,
				TSEnd:       t.Add(time.Duration(storm.DurationMins) * time.Minute),
			})
		}
		log.Ctx(ctx).DebugContext(
			ctx,
			"franklin in storm hedge mode",
			slog.Int("count", len(storms)),
		)
	}

	maxBatteryChargeKW := 0.0
	maxBatteryDischargeKW := 0.0

	if len(di.BatteryPEHWVersions) > 0 {
		for _, hwVer := range di.BatteryPEHWVersions {
			found := false
			for _, pc := range pcaps {
				if pc.PEHWVersion == hwVer {
					maxBatteryChargeKW += float64(pc.ChargePower) / 1000.0
					maxBatteryDischargeKW += float64(pc.DischargePower) / 1000.0
					found = true
					break
				}
			}
			if !found {
				log.Ctx(ctx).WarnContext(ctx, "could not find power capacity config for battery hardware version", slog.Int("hwVer", hwVer))
				// assume aPower 2 for each battery
				maxBatteryChargeKW += 8.0
				maxBatteryDischargeKW += 10.0
			}
		}
	} else if len(rd.RuntimeData.EachSOC) > 0 {
		// assume aPower 2 for each battery
		log.Ctx(ctx).WarnContext(ctx, "no battery hardware versions reported, assuming aPower 2 for each battery", slog.Int("count", len(rd.RuntimeData.EachSOC)))
		maxBatteryChargeKW = 8.0 * float64(len(rd.RuntimeData.EachSOC))
		maxBatteryDischargeKW = 10.0 * float64(len(rd.RuntimeData.EachSOC))
	}

	var vppEvents []types.VPPEvent
	if modes.VPPApplicable {
		// TODO: instead could we look at todayVppVo? but we don't know what the
		// structure is in the wild so we'll have to just use queryProgramDetails for now
		pd, err := f.queryProgramDetails(ctx)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to query program details", slog.Any("error", err))
		} else if pd.LatestEventStartTime != "" && pd.LatestEventEndTime != "" {
			// check to see if the latest event is in the future and if it is then
			// we need to know about it for forecasting
			startTime, err1 := time.ParseInLocation("2006-01-02 15:04:05", pd.LatestEventStartTime, di.location)
			endTime, err2 := time.ParseInLocation("2006-01-02 15:04:05", pd.LatestEventEndTime, di.location)
			if err1 != nil || err2 != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to parse VPP event times",
					slog.String("startTime", pd.LatestEventStartTime),
					slog.String("endTime", pd.LatestEventEndTime),
					slog.Any("err1", err1),
					slog.Any("err2", err2),
				)
			} else if endTime.After(time.Now()) {
				ehEvents, ehErr := f.queryEHEvents(ctx)
				if ehErr != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to query EH events", slog.Any("error", ehErr))
				}

				seenEvents := map[string]bool{}
				// 1. Process all upcoming and ongoing events from queryEHEvents
				for _, ev := range ehEvents {
					if ev.EventID == "" {
						continue
					}
					st, err1 := time.ParseInLocation("2006-01-02 15:04:05", ev.StartTime, di.location)
					et, err2 := time.ParseInLocation("2006-01-02 15:04:05", ev.EndTime, di.location)
					if err1 != nil || err2 != nil {
						log.Ctx(ctx).WarnContext(ctx, "failed to parse VPP event times from EH events",
							slog.String("startTime", ev.StartTime),
							slog.String("endTime", ev.EndTime),
							slog.Any("err1", err1),
							slog.Any("err2", err2),
						)
						continue
					}
					// Keep future and ongoing events
					if et.After(time.Now()) {
						vppEvents = append(vppEvents, types.VPPEvent{
							Description: pd.ProgramName,
							TSStart:     st,
							TSEnd:       et,
							VPPSoc:      ev.VPPSoc,
							OptOut:      ev.EventStatus == 3 || ev.EventStatus == 4,
						})
						seenEvents[ev.EventID] = true
					}
				}

				// 2. Process pd.LatestEvent if not already added
				if pd.LatestEventID != "" && !seenEvents[pd.LatestEventID] {
					optOut := pd.LatestEventStatus == 3 || pd.LatestEventStatus == 4
					vppEvents = append(vppEvents, types.VPPEvent{
						Description: pd.ProgramName,
						TSStart:     startTime,
						TSEnd:       endTime,
						VPPSoc:      pd.VPPSoc,
						OptOut:      optOut,
					})
				}

				// 3. Sort the events by TSStart
				sort.Slice(vppEvents, func(i, j int) bool {
					return vppEvents[i].TSStart.Before(vppEvents[j].TSStart)
				})
			}
		}
	}

	status := types.SystemStatus{
		Timestamp:               time.Now().In(di.location),
		TimeLocation:            di.location.String(),
		BatterySOC:              rd.RuntimeData.SOC,
		BatteryKW:               rd.RuntimeData.PowerBattery,
		SolarKW:                 rd.RuntimeData.PowerSolar,
		GridKW:                  rd.RuntimeData.PowerGrid,
		HomeKW:                  rd.RuntimeData.PowerLoad,
		BatteryCapacityKWH:      di.TotalBatteryCapacityKWH,
		EmergencyMode:           stormHedge || modes.currentMode.WorkMode == franklinWorkModeEmergencyBackup,
		GridUnavailable:         rd.RuntimeData.OffGridFlag != franklinOffGridFlagOnGrid,
		ElevatedMinBatterySOC:   modes.currentMode.ReserveSOC > 0 && modes.currentMode.ReserveSOC > f.settings.MinBatterySOC,
		BatteryAboveMinSOC:      rd.RuntimeData.SOC >= modes.currentMode.ReserveSOC,
		BatteryChargingDisabled: batteryChargingDisabled,
		MaxBatteryChargeKW:      maxBatteryChargeKW,
		MaxBatteryDischargeKW:   maxBatteryDischargeKW,
		Alarms:                  alarms,
		Storms:                  storms,
		VPPActive:               vppActive,
		VPPSOC:                  modes.VPPSOC,
		VPPEvents:               vppEvents,
		TOUMode:                 modes.currentMode.WorkMode == franklinWorkModeTimeOfUse || rd.CurrentWorkMode == franklinWorkModeTimeOfUse,
	}

	log.Ctx(ctx).DebugContext(ctx, "franklin system status", slog.Any("status", status))
	return status, nil
}

func (f *Franklin) queryProgramDetails(ctx context.Context) (franklinProgramDetailsResult, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/queryProgramDetails", params)
	if err != nil {
		return franklinProgramDetailsResult{}, err
	}

	var res franklinProgramDetailsResult
	if err := f.doRequest(req, &res); err != nil {
		return franklinProgramDetailsResult{}, err
	}

	return res, nil
}

func (f *Franklin) queryEHEvents(ctx context.Context) ([]franklinEHEvent, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("pageNum", "1")
	params.Set("pageSize", "10")

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/queryEHEvents", params)
	if err != nil {
		return nil, err
	}

	var res []franklinEHEvent
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (f *Franklin) getPowerControl(ctx context.Context) (franklinGetPowerControlSettingResult, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/tou/getPowerControlSetting", params)
	if err != nil {
		return franklinGetPowerControlSettingResult{}, err
	}

	var res franklinGetPowerControlSettingResult
	if err := f.doRequest(req, &res); err != nil {
		return franklinGetPowerControlSettingResult{}, err
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"franklin power control",
		slog.Int("gridMaxFlag", int(res.GridMaxFlag)),
		slog.Int("gridFeedMaxFlag", int(res.GridFeedMaxFlag)),
		slog.Float64("gridMax", res.GridMax),
		slog.Float64("gridFeedMax", res.GridFeedMax),
	)

	return res, nil
}

// GridSettings returns the grid-related capabilities and restrictions configured for the FranklinWH gateway.
func (f *Franklin) GridSettings(ctx context.Context) (types.GridSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	pc, err := f.getPowerControl(ctx)
	if err != nil {
		return types.GridSettings{}, err
	}

	return types.GridSettings{
		GridChargeBatteries: pc.GridMaxFlag == franklinGridMaxFlagChargeFromGrid,
		GridExportSolar:     pc.GridFeedMaxFlag == franklinGridFeedMaxFlagSolarOnly || pc.GridFeedMaxFlag == franklinGridFeedMaxFlagBatteryAndSolar,
		GridExportBatteries: pc.GridFeedMaxFlag == franklinGridFeedMaxFlagBatteryAndSolar,
	}, nil
}

func (f *Franklin) setPowerControl(ctx context.Context, pc franklinGetPowerControlSettingResult) error {
	data := map[string]any{
		"gatewayId": f.gatewayID,
		// TODO: what does a gridMax value of -1 mean? It's not clear yet
		"gridMax":     pc.GridMax,
		"gridMaxFlag": pc.GridMaxFlag,
	}
	// if we have no export, we don't set the gridFeedMax
	if pc.GridFeedMaxFlag != 3 {
		if pc.GridFeedMax < 0 {
			pc.GridFeedMax = -1.0
		}
		data["gridFeedMax"] = pc.GridFeedMax
	}
	data["gridFeedMaxFlag"] = pc.GridFeedMaxFlag

	log.Ctx(ctx).InfoContext(
		ctx,
		"setting franklin power control",
		slog.Float64("gridMax", pc.GridMax),
		slog.Int("gridMaxFlag", int(pc.GridMaxFlag)),
		slog.Float64("gridFeedMax", pc.GridFeedMax),
		slog.Int("gridFeedMaxFlag", int(pc.GridFeedMaxFlag)),
	)

	req, err := f.newPostJSONRequest(ctx, "hes-gateway/terminal/tou/setPowerControlV2", data)
	if err != nil {
		return err
	}

	// TODO: should we be doing something with the powerControlTipMsg response? What does it mean?
	return f.doRequest(req, nil)
}

type availableModes struct {
	list                []franklinMode
	touMode             franklinMode
	selfConsumption     franklinMode
	backup              franklinMode
	smartEnergyDispatch franklinMode
	currentMode         franklinMode
	stormHedgeEnabled   int
	VPPSOC              float64
	VPPApplicable       bool
}

func (f *Franklin) getAvailableModes(ctx context.Context) (availableModes, error) {
	params := url.Values{}
	params.Set("showType", "1")
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/getGatewayTouListV2", params)
	if err != nil {
		return availableModes{}, err
	}

	var res franklinGatewayTouListV2Result
	if err := f.doRequest(req, &res); err != nil {
		return availableModes{}, err
	}

	var tou franklinMode
	var sc franklinMode
	var backup franklinMode
	var smartDispatch franklinMode
	var current franklinMode
	var first franklinMode
	foundIDs := make([]string, len(res.List))
	modes := make([]franklinMode, len(res.List))
	for i, item := range res.List {
		m := franklinMode{
			ID:                item.ID,
			Name:              item.Name,
			WorkMode:          item.WorkMode,
			OldIndex:          item.OldIndex,
			ElectricityType:   item.ElectricityType,
			ReserveSOC:        item.ReserveSOC,
			CanEditReserveSOC: item.CanEditReserveSOC,
		}
		switch item.WorkMode {
		case franklinWorkModeTimeOfUse:
			modes[i] = m
			tou = modes[i]
		case franklinWorkModeSelfConsumption:
			modes[i] = m
			sc = modes[i]
		case franklinWorkModeEmergencyBackup:
			modes[i] = m
			backup = modes[i]
		case franklinWorkModeSmartEnergyDispatch:
			modes[i] = m
			smartDispatch = modes[i]
		default:
			log.Ctx(ctx).WarnContext(
				ctx,
				"unknown work mode",
				slog.Int("id", item.ID),
				slog.String("name", item.Name),
				slog.Int("workMode", int(item.WorkMode)),
				slog.Int("oldIndex", item.OldIndex),
			)
		}
		if item.ID == res.CurrentID {
			current = m
		}
		if i == 0 {
			first = m
		}
		foundIDs[i] = fmt.Sprintf("%d", item.ID)
	}

	if current.ID == 0 {
		current = first
		log.Ctx(ctx).WarnContext(
			ctx,
			"franklin current tou id not found",
			slog.Int("currentTouID", res.CurrentID),
			slog.String("foundIDs", strings.Join(foundIDs, ",")),
		)
	}

	return availableModes{
		list:                modes,
		touMode:             tou,
		selfConsumption:     sc,
		backup:              backup,
		smartEnergyDispatch: smartDispatch,
		stormHedgeEnabled:   res.StormHedgeEnabled,
		currentMode:         current,
		VPPSOC:              res.VPPSOC.VPPSoc,
		VPPApplicable:       res.VPPSOC.VPPApplicable,
	}, nil
}

func (f *Franklin) getTOUTemplate(ctx context.Context) (franklinTOUTemplateResponse, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/tou/getTouDispatchDetail", params)
	if err != nil {
		return franklinTOUTemplateResponse{}, err
	}

	var res franklinTOUTemplateResponse
	if err := f.doRequest(req, &res); err != nil {
		return franklinTOUTemplateResponse{}, err
	}

	return res, nil
}

func (f *Franklin) saveExportTouDispatch(ctx context.Context, start time.Time, until time.Time, dispatchID franklinDispatchID) error {
	roundedStart := roundTOUPeriodStart(start)
	roundedUntil := roundTOUPeriodEnd(until)
	if !roundedUntil.After(roundedStart) {
		roundedUntil = roundedStart.Add(30 * time.Minute)
	}

	tplRes, err := f.getTOUTemplate(ctx)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get franklin tou dispatch detail, using defaults", slog.Any("error", err))
	}
	countryID := tplRes.Template.CountryID
	if countryID == 0 {
		countryID = 2 // default to United States
	}
	provinceID := tplRes.Template.ProvinceID
	if provinceID == 0 {
		provinceID = 39 // default to California
	}

	if f.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated franklin saveTouDispatch",
			slog.Time("start", roundedStart),
			slog.Time("until", roundedUntil),
			slog.Int("dispatchID", int(dispatchID)),
			slog.Int("countryID", countryID),
			slog.Int("provinceID", provinceID),
		)
		return nil
	}

	startDay := time.Date(roundedStart.Year(), roundedStart.Month(), roundedStart.Day(), 0, 0, 0, 0, roundedStart.Location())
	untilDay := time.Date(roundedUntil.Year(), roundedUntil.Month(), roundedUntil.Day(), 0, 0, 0, 0, roundedUntil.Location())
	crossesMidnight := !untilDay.Equal(startDay) && !(roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0)
	startStr := roundedStart.Format("15:04")

	var details []map[string]any
	if crossesMidnight {
		nextDayEndStr := roundedUntil.Format("15:04")
		if nextDayEndStr >= startStr {
			details = append(details, map[string]any{
				"startHourTime": "00:00",
				"endHourTime":   "24:00",
				"waveType":      2,
				"name":          "On-peak",
				"dispatchId":    int(dispatchID),
			})
		} else {
			details = append(details, map[string]any{
				"startHourTime": "00:00",
				"endHourTime":   nextDayEndStr,
				"waveType":      2,
				"name":          "On-peak",
				"dispatchId":    int(dispatchID),
			})
			details = append(details, map[string]any{
				"startHourTime": nextDayEndStr,
				"endHourTime":   startStr,
				"waveType":      0,
				"name":          "Off-peak",
				"dispatchId":    int(franklinDispatchSelfConsumption),
			})
			details = append(details, map[string]any{
				"startHourTime": startStr,
				"endHourTime":   "24:00",
				"waveType":      2,
				"name":          "On-peak",
				"dispatchId":    int(dispatchID),
			})
		}
	} else {
		endStr := roundedUntil.Format("15:04")
		if (roundedUntil.Hour() == 0 && roundedUntil.Minute() == 0) || roundedUntil.Day() != roundedStart.Day() {
			endStr = "24:00"
		}
		if startStr != "00:00" {
			details = append(details, map[string]any{
				"startHourTime": "00:00",
				"endHourTime":   startStr,
				"waveType":      0,
				"name":          "Off-peak",
				"dispatchId":    int(franklinDispatchSelfConsumption),
			})
		}
		details = append(details, map[string]any{
			"startHourTime": startStr,
			"endHourTime":   endStr,
			"waveType":      2,
			"name":          "On-peak",
			"dispatchId":    int(dispatchID),
		})
		if endStr != "24:00" {
			details = append(details, map[string]any{
				"startHourTime": endStr,
				"endHourTime":   "24:00",
				"waveType":      0,
				"name":          "Off-peak",
				"dispatchId":    int(franklinDispatchSelfConsumption),
			})
		}
	}

	payload := map[string]any{
		"template": map[string]any{
			"gatewayId":          f.gatewayID,
			"electricCompany":    "RateRudder",
			"eletricCompanyId":   -1,
			"name":               "Direct Solar Export",
			"electricityType":    1,
			"workMode":           int(franklinWorkModeTimeOfUse),
			"countryId":          countryID,
			"provinceId":         provinceID,
			"eleCompanyFullName": "RateRudder Dynamic Solar Export",
			"tariffName":         "Direct Solar Export",
		},
		"strategyList": []map[string]any{
			{
				"seasonName": "All Year",
				"month":      "1,2,3,4,5,6,7,8,9,10,11,12",
				"dayTypeVoList": []map[string]any{
					{
						"dayName":      "everyDay",
						"dayType":      3,
						"detailVoList": details,
					},
				},
			},
		},
		"coverContentFlag": false,
	}

	req, err := f.newPostJSONRequest(ctx, "hes-gateway/terminal/tou/saveTouDispatch", payload)
	if err != nil {
		return err
	}
	if err := f.doRequest(req, nil); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save tou dispatch", slog.Any("error", err))
		return err
	}
	return nil
}

func parseTOUTimeOfDay(timeStr string, baseDate time.Time) (time.Time, bool) {
	if timeStr == "24:00" {
		return baseDate.Add(24 * time.Hour), true
	}
	parts := strings.Split(timeStr, ":")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return time.Time{}, false
	}
	return time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), h, m, 0, 0, baseDate.Location()), true
}

func isFranklinScheduleMatch(strategies []franklinTOUStrategy, targetUntil time.Time, dispatchID franklinDispatchID, now time.Time) bool {
	loc := targetUntil.Location()
	nowInLoc := now.In(loc)
	today := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), 0, 0, 0, 0, loc)
	roundedTargetUntil := roundTOUPeriodEnd(targetUntil.In(loc))
	targetDay := time.Date(roundedTargetUntil.Year(), roundedTargetUntil.Month(), roundedTargetUntil.Day(), 0, 0, 0, 0, loc)

	crossesMidnight := !targetDay.Equal(today) && !(roundedTargetUntil.Hour() == 0 && roundedTargetUntil.Minute() == 0)

	for _, s := range strategies {
		for _, dt := range s.DayTypeVoList {
			if crossesMidnight {
				hasEvening := false
				hasMorning := false
				tomorrow := today.AddDate(0, 0, 1)

				for _, d := range dt.DetailVoList {
					if d.WaveType == 2 && d.DispatchID == int(dispatchID) {
						if d.StartHourTime == "00:00" && d.EndHourTime == "24:00" {
							hasEvening = true
							hasMorning = true
							break
						}
						// Check evening segment
						startTime, ok1 := parseTOUTimeOfDay(d.StartHourTime, today)
						if ok1 && d.EndHourTime == "24:00" && !startTime.After(nowInLoc) {
							hasEvening = true
						}
						// Check morning segment on tomorrow
						endTime, ok2 := parseTOUTimeOfDay(d.EndHourTime, tomorrow)
						if ok2 && d.StartHourTime == "00:00" {
							if math.Abs(endTime.Sub(targetUntil.In(loc)).Minutes()) <= 10 || math.Abs(endTime.Sub(roundedTargetUntil).Minutes()) <= 10 {
								hasMorning = true
							}
						}
					}
				}
				if hasEvening && hasMorning {
					return true
				}
			} else {
				for _, d := range dt.DetailVoList {
					if d.WaveType == 2 && d.DispatchID == int(dispatchID) {
						startTime, ok1 := parseTOUTimeOfDay(d.StartHourTime, today)
						endTime, ok2 := parseTOUTimeOfDay(d.EndHourTime, today)
						if !ok1 || !ok2 {
							continue
						}
						if endTime.Before(startTime) {
							endTime = endTime.Add(24 * time.Hour)
						}
						// Segment must be currently active (start <= now < end)
						if startTime.After(nowInLoc) || !nowInLoc.Before(endTime) {
							continue
						}
						// End time is within 10 minutes of targetUntil or roundedTargetUntil
						if math.Abs(endTime.Sub(targetUntil.In(loc)).Minutes()) <= 10 || math.Abs(endTime.Sub(roundedTargetUntil).Minutes()) <= 10 {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// SetModes sets the operating modes of the system.
func (f *Franklin) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) (bool, error) {
	log.Ctx(ctx).DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol), slog.Any("opts", opts))
	f.mu.Lock()
	defer f.mu.Unlock()

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return false, nil
	}

	rd, err := f.getRuntimeDataWithCache(ctx, false)
	if err != nil {
		return false, err
	}

	modes, err := f.getAvailableModes(ctx)
	if err != nil {
		return false, err
	}

	isStormHedge := rd.RuntimeData.TOUID == franklinOverlayModeStormHedge
	isBackup := modes.currentMode.WorkMode == franklinWorkModeEmergencyBackup
	// TODO: restore checking isBackup when we are no longer using backup as a workaround
	_ = isBackup

	if isStormHedge {
		if bat == types.BatteryModeNoChange {
			return false, nil
		}
		log.Ctx(ctx).DebugContext(ctx, "storm hedge active, skipping mode change", slog.Any("batteryMode", bat))
		return false, nil
	}

	if modes.selfConsumption == (franklinMode{}) {
		log.Ctx(ctx).ErrorContext(ctx, "self consumption mode not available", slog.Any("modes", modes))
		return false, errors.New("self consumption mode not available")
	}
	sc := modes.selfConsumption

	pc, err := f.getPowerControl(ctx)
	if err != nil {
		return false, err
	}

	targetMode := sc
	newReserveSOC := sc.ReserveSOC

	minSOC := f.settings.MinBatterySOC
	if opts.MinimumSOC != 0 {
		minSOC = float64(opts.MinimumSOC)
	}

	if f.settings.ManageTOUSchedules && (sol == types.SolarModeExport || bat == types.BatteryModeExport) {
		dispatchID := franklinDispatchAPowerToHome
		if bat == types.BatteryModeStandby {
			dispatchID = franklinDispatchAPowerOnStandby
			newReserveSOC = max(math.Floor(rd.RuntimeData.SOC), minSOC)
		} else if bat == types.BatteryModeExport {
			dispatchID = franklinDispatchAPowerToHomeAndGrid
			newReserveSOC = minSOC
		} else {
			// franklinDispatchAPowerToHome (Code F) has the battery supply home loads while solar exports to the grid.
			newReserveSOC = minSOC
		}

		if opts.TSScheduleModeUntil.IsZero() {
			log.Ctx(ctx).ErrorContext(ctx, "no schedule duration provided", slog.String("gatewayID", f.gatewayID))
			return false, errors.New("missing schedule duration to provision tou mode")
		}

		loc := opts.TSScheduleModeUntil.Location()
		startTime := time.Now().In(loc)
		if rd.RuntimeData.Timestamp > 0 {
			startTime = time.Unix(rd.RuntimeData.Timestamp, 0).In(loc)
		}
		scheduleMatched := false
		if modes.touMode != (franklinMode{}) && modes.currentMode.WorkMode == franklinWorkModeTimeOfUse {
			tplRes, err := f.getTOUTemplate(ctx)
			if err == nil && isFranklinScheduleMatch(tplRes.StrategyList, opts.TSScheduleModeUntil, dispatchID, startTime) {
				scheduleMatched = true
				log.Ctx(ctx).DebugContext(ctx, "franklin tou schedule already matches, skipping saveTouDispatch",
					slog.Time("until", opts.TSScheduleModeUntil),
					slog.Int("dispatchID", int(dispatchID)),
				)
			}
		}

		if !scheduleMatched {
			if err := f.saveExportTouDispatch(ctx, startTime, opts.TSScheduleModeUntil, dispatchID); err != nil {
				return false, err
			}
		}

		if modes.touMode == (franklinMode{}) {
			if f.settings.DryRun {
				log.Ctx(ctx).InfoContext(ctx, "dry run: franklin tou mode not configured on gateway, would've provisioned via saveTouDispatch",
					slog.String("gatewayID", f.gatewayID),
				)
				modes.touMode = franklinMode{
					WorkMode: franklinWorkModeTimeOfUse,
					Name:     "Direct Solar Export",
				}
			} else {
				log.Ctx(ctx).WarnContext(ctx, "franklin tou mode not configured on gateway, re-fetching modes after saving dispatch",
					slog.String("gatewayID", f.gatewayID),
				)
				refreshedModes, err := f.getAvailableModes(ctx)
				if err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to re-fetch available modes after saving tou dispatch", slog.Any("error", err))
					return false, err
				}
				modes = refreshedModes
			}
		}

		if modes.touMode == (franklinMode{}) {
			log.Ctx(ctx).ErrorContext(ctx, "franklin tou mode not available on gateway even after saving tou dispatch",
				slog.String("gatewayID", f.gatewayID),
				slog.Any("modes", modes.list),
			)
			return false, errors.New("franklin tou mode not available")
		}

		targetMode = modes.touMode
	} else {
		switch bat {
		case types.BatteryModeChargeAny:
			if f.settings.GridChargeBatteries && pc.GridMaxFlag != franklinGridMaxFlagChargeFromGrid {
				log.Ctx(ctx).WarnContext(ctx, "grid charging is disabled in power control, setting emergency backup mode to charge", slog.Any("opts", opts))
				if modes.backup == (franklinMode{}) {
					log.Ctx(ctx).ErrorContext(ctx, "backup mode not available", slog.Any("modes", modes))
					return false, errors.New("backup mode not available")
				}
				targetMode = modes.backup
			} else {
				// note: since we're not setting emergency backup mode solar will still be
				// used to power the home first then spill over into the battery
				if !sc.CanEditReserveSOC {
					log.Ctx(ctx).WarnContext(ctx, "cannot edit reserve SOC")
					return false, errors.New("cannot edit reserve SOC")
				}
				targetSOC := 100
				if opts.ChargeToSOC != 0 {
					targetSOC = opts.ChargeToSOC
				}
				newReserveSOC = float64(targetSOC)
			}
		case types.BatteryModeLoad, types.BatteryModeExport:
			// we set the SOC to the minimum battery SOC to ensure we start discharging
			// if we're somehow less than this soc, we'll charge from the solar, unless
			// solar is unavailable then it'll charge from the grid
			// it seems like this accepts an int value
			newReserveSOC = minSOC
		case types.BatteryModeStandby:
			// we floor the SOC to ensure we don't set it to a value that would cause the
			// battery to charge
			if !sc.CanEditReserveSOC {
				log.Ctx(ctx).WarnContext(ctx, "cannot edit reserve SOC")
				return false, errors.New("cannot edit reserve SOC")
			}
			// make sure we don't set it to less than the minimum battery SOC
			newReserveSOC = max(math.Floor(rd.RuntimeData.SOC), minSOC)
		case types.BatteryModeNoChange:
			targetMode = modes.currentMode
		default:
			return false, fmt.Errorf("unknown battery mode: %v", bat)
		}
	}

	if targetMode.WorkMode == franklinWorkModeSelfConsumption || targetMode.WorkMode == franklinWorkModeTimeOfUse {
		// we can't set it below 5
		if newReserveSOC < 5 {
			newReserveSOC = 5
		}

		// if franklin overshot our reserve SOC by less than 1 percent, ignore it
		if math.Abs(newReserveSOC-targetMode.ReserveSOC) <= 1.0 {
			newReserveSOC = targetMode.ReserveSOC
		}
	}

	switch sol {
	case types.SolarModeExport, types.SolarModeAny, types.SolarModeNoExport, types.SolarModeNoChange:
		// In FranklinWH, PCS power control (setPowerControl/setPowerControlV2) only configures static interconnection
		// compliance caps (PCS limits, which require installer privileges) and does not command runtime export dispatch.
		// Runtime export is controlled exclusively via TOU dispatch schedules (saveTouDispatch).
	default:
		return false, fmt.Errorf("unknown solar mode: %v", sol)
	}

	modeChanged := modes.currentMode.WorkMode != targetMode.WorkMode
	socChanged := (targetMode.WorkMode == franklinWorkModeSelfConsumption || targetMode.WorkMode == franklinWorkModeTimeOfUse) && math.Round(newReserveSOC) != math.Round(modes.currentMode.ReserveSOC)

	if modeChanged || socChanged {
		if f.settings.DryRun {
			if !modeChanged {
				log.Ctx(ctx).DebugContext(
					ctx,
					"dry run: would've updated just soc",
					slog.Int("soc", int(math.Round(newReserveSOC))),
					slog.Int("workMode", int(targetMode.WorkMode)),
				)
			} else {
				log.Ctx(ctx).DebugContext(
					ctx,
					"dry run: would've tou mode",
					slog.Int("soc", int(math.Round(newReserveSOC))),
					slog.Int("workMode", int(targetMode.WorkMode)),
				)
			}
		} else {
			if !modeChanged && targetMode.WorkMode == franklinWorkModeSelfConsumption {
				log.Ctx(ctx).InfoContext(
					ctx,
					"updating franklin soc",
					slog.Int("soc", int(math.Round(newReserveSOC))),
					slog.Int("workMode", int(targetMode.WorkMode)),
				)
				params := url.Values{}
				params.Set("gatewayId", f.gatewayID)
				params.Set("workMode", strconv.Itoa(int(targetMode.WorkMode)))
				params.Set("electricityType", strconv.Itoa(targetMode.ElectricityType))
				params.Set("soc", strconv.Itoa(int(math.Round(newReserveSOC))))

				req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/updateSocV2", params)
				if err != nil {
					return false, err
				}
				if err := f.doRequest(req, nil); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update soc", slog.Any("error", err))
					return false, err
				}
			} else {
				log.Ctx(ctx).InfoContext(
					ctx,
					"updating franklin tou mode",
					slog.Float64("soc", newReserveSOC),
					slog.Int("workMode", int(targetMode.WorkMode)),
				)

				params := url.Values{}
				params.Set("gatewayId", f.gatewayID)
				params.Set("currendId", fmt.Sprint(targetMode.ID))
				params.Set("workMode", strconv.Itoa(int(targetMode.WorkMode)))
				params.Set("electricityType", fmt.Sprint(targetMode.ElectricityType))
				params.Set("oldIndex", fmt.Sprint(targetMode.OldIndex))
				params.Set("stromEn", fmt.Sprint(modes.stormHedgeEnabled))
				// round to the nearest integer to minimize the chance of the battery charging
				// or discharging when we don't want it to
				params.Set("soc", strconv.Itoa(int(math.Round(newReserveSOC))))

				req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/updateTouModeV2", params)
				if err != nil {
					return false, err
				}
				if err := f.doRequest(req, nil); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update tou mode", slog.Any("error", err))
					return false, err
				}
			}
			return true, nil
		}
	}

	return false, nil
}

// GetEnergyHistory retrieves energy history for the specified period.
// It aggregates 5-minute intervals into hourly EnergyStats.
func (f *Franklin) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting franklin energy history", slog.String("start", start.String()), slog.String("end", end.String()))
	f.mu.Lock()
	defer f.mu.Unlock()

	var di franklinDeviceInfoV2Result
	if time.Now().Before(f.deviceInfoExpiry) {
		di = f.deviceInfoCache
	} else {
		var err error
		di, err = f.getDeviceInfo(ctx)
		if err != nil {
			return nil, err
		}
		f.deviceInfoCache = di
		f.deviceInfoExpiry = time.Now().Add(time.Minute)
	}

	startInLoc := start.In(di.location)
	endInLoc := end.In(di.location)

	// Fetch full days based on start and end
	startDay := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 0, 0, 0, 0, di.location)
	lastDayToFetch := time.Date(endInLoc.Year(), endInLoc.Month(), endInLoc.Day(), 0, 0, 0, 0, di.location)
	// since the points look backward, we need to include the next day to get the
	// last 5 minutes of the last day but if its in the future there's no point
	// in fetching it
	if nextDay := lastDayToFetch.AddDate(0, 0, 1); !nextDay.After(time.Now()) {
		lastDayToFetch = nextDay
	}

	var allPoints []franklinEnergyPoint

	// Iterate through days
	current := startDay
	limiter := rate.NewLimiter(rate.Limit(4), 4)
	for !current.After(lastDayToFetch) {
		if current.After(time.Now()) {
			break
		}

		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}

		points, err := f.getEnergyPointsForDay(ctx, current, di.location)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get energy points for day", slog.String("day", current.Format("2006-01-02")), slog.Any("error", err))
			return nil, err
		}
		allPoints = append(allPoints, points...)
		current = current.AddDate(0, 0, 1)
	}

	// Aggregate all points into hourly buckets
	allStats := f.aggregatePointsIntoHours(allPoints, di.location)

	// Group into daily buckets
	dailyMap := make(map[string][]types.EnergyStats)
	var sortedDayKeys []string

	for _, s := range allStats {
		dayStart := time.Date(s.TSHourStart.Year(), s.TSHourStart.Month(), s.TSHourStart.Day(), 0, 0, 0, 0, di.location)
		dayKey := dayStart.Format("2006-01-02")
		if _, exists := dailyMap[dayKey]; !exists {
			sortedDayKeys = append(sortedDayKeys, dayKey)
		}
		dailyMap[dayKey] = append(dailyMap[dayKey], s)
	}

	sort.Strings(sortedDayKeys)

	var result []types.DailyEnergyStats
	for _, key := range sortedDayKeys {
		dayStart, err := time.ParseInLocation("2006-01-02", key, di.location)
		if err != nil {
			return nil, fmt.Errorf("failed to parse day key %s: %w", key, err)
		}

		// Filter based on the requested start and end bounds per the overall intent,
		// but since we keep whole days, we just check if the day intersects the requested range reasonably
		// actually, let's just return the full days that we fetched!
		if dayStart.After(lastDayToFetch) || dayStart.Before(startDay) {
			continue
		}

		result = append(result, types.DailyEnergyStats{
			TSDayStart:   dayStart,
			TimeLocation: di.location.String(),
			Hourly:       dailyMap[key],
		})
	}

	return result, nil
}

func (f *Franklin) getStormList(ctx context.Context) ([]franklinStormListResult, error) {
	params := url.Values{}
	params.Set("equipNo", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/weather/getProgressingStormList", params)
	if err != nil {
		return nil, err
	}

	var res []franklinStormListResult
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (f *Franklin) getEnergyPointsForDay(ctx context.Context, day time.Time, loc *time.Location) ([]franklinEnergyPoint, error) {
	day = day.In(loc)
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("dayTime", day.Format("2006-01-02"))

	req, err := f.newGetRequest(ctx, "api-energy/power/getFhpPowerByDay", params)
	if err != nil {
		return nil, err
	}

	var res franklinFHPPowerByDayResult
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	// no energy data for this day
	if len(res.SolarToHomeKWHRates) == 0 &&
		len(res.SolarToGridKWHRates) == 0 &&
		len(res.SolarToBatteryKWHRates) == 0 &&
		len(res.GridToBatteryKWHRates) == 0 &&
		len(res.GridToHomeKWHRates) == 0 &&
		len(res.BatteryToGridKWHRates) == 0 &&
		len(res.BatteryToHomeKWHRates) == 0 &&
		len(res.SOCArray) == 0 {
		return nil, nil
	}

	expLen := len(res.DeviceTimeArray)
	if len(res.SolarToHomeKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerSolarHomeArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SolarToHomeKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.SolarToGridKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerSolarGirdArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SolarToGridKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.SolarToBatteryKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerSolarFhpArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SolarToBatteryKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.GridToBatteryKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerGirdFhpArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.GridToBatteryKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.GridToHomeKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerGirdHomeArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.GridToHomeKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.BatteryToGridKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerFhpGirdArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.BatteryToGridKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.BatteryToHomeKWHRates) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "powerFhpHomeArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.BatteryToHomeKWHRates)))
		return nil, errors.New("unexpected array length in response")
	}
	if len(res.SOCArray) != expLen {
		log.Ctx(ctx).WarnContext(ctx, "socArray length unexpected", slog.Int("expected", expLen), slog.Int("length", len(res.SOCArray)))
		return nil, errors.New("unexpected array length in response")
	}

	now := time.Now().In(loc)
	points := make([]franklinEnergyPoint, 0, len(res.DeviceTimeArray))
	for i, timeStr := range res.DeviceTimeArray {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, loc)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse time", slog.String("time", timeStr), slog.Any("error", err))
			return nil, err
		}
		if t.After(now) {
			continue
		}
		points = append(points, franklinEnergyPoint{
			Timestamp:             t,
			SolarToHomeKWHRate:    res.SolarToHomeKWHRates[i],
			SolarToGridKWHRate:    res.SolarToGridKWHRates[i],
			SolarToBatteryKWHRate: res.SolarToBatteryKWHRates[i],
			GridToBatteryKWHRate:  res.GridToBatteryKWHRates[i],
			GridToHomeKWHRate:     res.GridToHomeKWHRates[i],
			BatteryToGridKWHRate:  res.BatteryToGridKWHRates[i],
			BatteryToHomeKWHRate:  res.BatteryToHomeKWHRates[i],
			BatterySOC:            res.SOCArray[i],
		})
	}

	return points, nil
}

func (f *Franklin) aggregatePointsIntoHours(points []franklinEnergyPoint, loc *time.Location) []types.EnergyStats {
	if len(points) == 0 {
		return nil
	}

	// make sure the points are sorted by timestamp
	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp.Before(points[j].Timestamp)
	})

	// Aggregate points into hourly buckets
	hourlyStats := make(map[string]*types.EnergyStats)
	var sortedKeys []string

	for i, p := range points {
		t := p.Timestamp

		// if its the first point and its for a previous hour (minute 0), skip it
		if i == 0 && t.Minute() == 0 {
			continue
		}

		// The data is for the preceding period, so we look backwards.
		var duration time.Duration
		if i > 0 {
			// Cap duration to 1 hour to avoid massive errors during data gaps
			duration = min(t.Sub(points[i-1].Timestamp), time.Hour)
		} else {
			// for the first point, we assume it's a 5 minute interval leading up to t.
			duration = 5 * time.Minute
		}

		// The data is for the preceding period, so the bucket should be based on
		// the start of that period.
		// We'll subtract 1 minute to shift the "end of hour" timestamp (e.g. 12:00:00)
		// into the correct bucket (11:00:00).
		bucketT := t.Add(-time.Minute).In(loc)

		// Determine hour bucket
		hourKey := bucketT.Format("2006-01-02 15:00:00")
		if _, exists := hourlyStats[hourKey]; !exists {
			hourlyStats[hourKey] = &types.EnergyStats{
				TSHourStart:   time.Date(bucketT.Year(), bucketT.Month(), bucketT.Day(), bucketT.Hour(), 0, 0, 0, loc),
				TimeLocation:  loc.String(),
				MinBatterySOC: p.BatterySOC,
				MaxBatterySOC: p.BatterySOC,
			}
			sortedKeys = append(sortedKeys, hourKey)
		}
		s := hourlyStats[hourKey]

		// collect all relevant stats for the time
		// and convert them all to KWH from the rate of kwh in that duration
		hours := duration.Hours()
		solarToHome := max(0, p.SolarToHomeKWHRate) * hours
		solarToGrid := max(0, p.SolarToGridKWHRate) * hours
		solarToBat := max(0, p.SolarToBatteryKWHRate) * hours
		gridToBat := p.GridToBatteryKWHRate * hours
		gridToHome := p.GridToHomeKWHRate * hours
		batToGrid := p.BatteryToGridKWHRate * hours
		batToHome := p.BatteryToHomeKWHRate * hours

		s.SolarKWH += (solarToHome + solarToGrid + solarToBat)
		s.BatteryChargedKWH += (solarToBat + gridToBat)
		s.BatteryUsedKWH += (batToHome + batToGrid)
		s.GridExportKWH += (solarToGrid + batToGrid)
		s.GridImportKWH += (gridToHome + gridToBat)
		s.HomeKWH += (solarToHome + gridToHome + batToHome)
		s.BatteryToHomeKWH += batToHome
		s.BatteryToGridKWH += batToGrid
		s.SolarToHomeKWH += solarToHome
		s.SolarToBatteryKWH += solarToBat
		s.SolarToGridKWH += solarToGrid

		if p.BatterySOC < s.MinBatterySOC {
			s.MinBatterySOC = p.BatterySOC
		}
		if p.BatterySOC > s.MaxBatterySOC {
			s.MaxBatterySOC = p.BatterySOC
		}
	}

	// Sort the keys in chronological order
	sort.Strings(sortedKeys)

	result := make([]types.EnergyStats, 0, len(sortedKeys))
	for _, key := range sortedKeys {
		result = append(result, *hourlyStats[key])
	}

	return result
}

// Internal Structs

type franklinCurrentAlarmVO struct {
	ID                   int    `json:"id"`
	GatewayID            string `json:"gatewayId"`
	AlarmForSerialNumber string `json:"alarmEqSn"`
	AlarmCode            string `json:"alarmCode"`
	Time                 string `json:"time"`
	Name                 string `json:"logName"`
	Explanation          string `json:"alarmExplanation"`
	Plan                 string `json:"plan"`
	// TODO: what does level mean? Should we only stop updating based on certain levels?
}

type franklinDeviceCompositeInfoResult struct {
	CurrentWorkMode  franklinWorkMode         `json:"currentWorkMode"`
	DeviceStatus     int                      `json:"deviceStatus"`
	RuntimeData      franklinRuntimeData      `json:"runtimeData"`
	Valid            bool                     `json:"valid"`
	CurrentAlarmList []franklinCurrentAlarmVO `json:"currentAlarmVOList"`

	// there's also "solarHaveVo": {...}
}

type franklinRuntimeData struct {
	// Mode ID matching the active entry in getGatewayTouListV2 (e.g. 2236 for TOU, 2774 for Self-Consumption, 2775 for Backup).
	// When special system overlay states are active, the gateway reports:
	// franklinOverlayModeStormHedge (6) = Storm Hedge active (forces 100% reserve SOC and grid charge)
	// franklinOverlayModeVPP (9) = VPP active (utility demand response event actively dispatching battery)
	TOUID    franklinWorkMode `json:"mode"`
	ModeName string           `json:"name"`

	RunStatus   franklinRunStatus   `json:"run_status"`
	OffGridFlag franklinOffGridFlag `json:"offGirdFlag"` // misspelled in API

	SOC       float64   `json:"soc"`
	Timestamp int64     `json:"timestamp"`
	EachSOC   []float64 `json:"fhpSoc"`

	PowerBattery     float64   `json:"p_fhp"`
	PowerSolar       float64   `json:"p_sun"`
	PowerGrid        float64   `json:"p_uti"`
	PowerLoad        float64   `json:"p_load"`
	PowerGenerator   float64   `json:"p_gen"`
	PowerEachBattery []float64 `json:"fhpPower"`

	TotalBatteryCharge    float64 `json:"kwh_fhp_chg"`
	TotalBatteryDischarge float64 `json:"kwh_fhp_di"`
	TotalGridImport       float64 `json:"kwh_uti_in"`
	TotalGridExport       float64 `json:"kwh_uti_out"`
	TotalSolar            float64 `json:"kwh_sun"`
	TotalGenerator        float64 `json:"kwh_gen"`
	TotalLoad             float64 `json:"kwh_load"`

	GridChargedBattery  float64 `json:"gridChBat"`
	BatteryOutGrid      float64 `json:"batOutGrid"`
	SolarOutGrid        float64 `json:"soOutGrid"`
	SolarChargedBattery float64 `json:"soChBat"`

	// TODO: does t_amb mean the ambient temperature of the outside or the batteries?
	// TODO: what do kwhSolarLoad, kwhGridLoad, kwhFhpLoad, kwhGenLoad mean?
	// TODO: what does solarPower (seems to be 10x p_sun?) mean?
}

type franklinPowerCapConfigResult struct {
	ID             int    `json:"id"`
	ModelName      string `json:"modelName"`
	PEHWVersion    int    `json:"peHwVersion"`
	RatedCap       int    `json:"ratedCap"`
	ChargePower    int    `json:"chargePower"`
	DischargePower int    `json:"dischargePower"`
	DerateFlag     int    `json:"derateFlag"`
}

type franklinDeviceInfoV2Result struct {
	GatewayID               string                `json:"gatewayId"`
	DeviceTime              string                `json:"deviceTime"`
	TimeZone                string                `json:"zoneInfo"`
	SystemHardwareVersion   int                   `json:"sysHdVersionInt"`
	TotalBatteryCapacityKWH float64               `json:"totalCap"`
	TotalBatteryPowerKW     float64               `json:"fixedPowerTotal"`
	Batteries               []franklinBatteryInfo `json:"apowerList"`
	BatteryPEHWVersions     []int                 `json:"peHwVerList"`
	ProtocolVersion         string                `json:"protocolVer"`

	location *time.Location

	// TODO: what do solarFlag, solarTipMsg mean?
	// TODO: what does activeStatus mean?
	// TODO: what do sleepStatus, blackSleepFlag mean?
}

type franklinBatteryInfo struct {
	Serial     string `json:"id"`
	CapacityWH int    `json:"rateBatCap"`
	PowerW     int    `json:"ratedPwr"`
}

type franklinOffGridFlag int

const (
	franklinOffGridFlagOnGrid  franklinOffGridFlag = 0
	franklinOffGridFlagOffGrid franklinOffGridFlag = 1
)

type franklinRunStatus int

const (
	franklinRunStatusStandby            franklinRunStatus = 0
	franklinRunStatusCharging           franklinRunStatus = 1
	franklinRunStatusDischarging        franklinRunStatus = 2
	franklinRunStatusFault              franklinRunStatus = 3
	franklinRunStatusOffGridStandby     franklinRunStatus = 5
	franklinRunStatusOffGridCharging    franklinRunStatus = 6
	franklinRunStatusOffGridDischarging franklinRunStatus = 7
	franklinRunStatusDebug              franklinRunStatus = 8
)

type franklinGridMaxFlag int

const (
	franklinGridMaxFlagNoChargeFromGrid franklinGridMaxFlag = 1
	franklinGridMaxFlagChargeFromGrid   franklinGridMaxFlag = 2
)

type franklinGridFeedMaxFlag int

const (
	franklinGridFeedMaxFlagNoExport        franklinGridFeedMaxFlag = 3
	franklinGridFeedMaxFlagSolarOnly       franklinGridFeedMaxFlag = 1
	franklinGridFeedMaxFlagBatteryAndSolar franklinGridFeedMaxFlag = 2
)

type franklinDispatchID int

const (
	franklinDispatchAPowerToHome           franklinDispatchID = 1 // aPower supplies home, solar exported to grid (code F)
	franklinDispatchAPowerOnStandby        franklinDispatchID = 2 // aPower on standby, excess solar exported to grid (code B)
	franklinDispatchAPowerChargesFromSolar franklinDispatchID = 3 // Solar charges aPower, grid supplies home (code E)
	franklinDispatchSelfConsumption        franklinDispatchID = 6 // Home load priority: solar, aPower, grid (code D)
	franklinDispatchAPowerToHomeAndGrid    franklinDispatchID = 7 // aPower supplies home & battery exports to grid, solar exported to grid (code H)
	franklinDispatchAPowerChargesFromGrid  franklinDispatchID = 8 // Solar and grid charge aPower, grid supplies home (code G)
	franklinDispatchZeroImport             franklinDispatchID = 9 // Zero import: home runs on battery (code I)
)

type franklinGetPowerControlSettingResult struct {
	GridFeedMax     float64                 `json:"gridFeedMax"`
	GridFeedMaxFlag franklinGridFeedMaxFlag `json:"gridFeedMaxFlag"`
	GridMax         float64                 `json:"gridMax"`
	GridMaxFlag     franklinGridMaxFlag     `json:"gridMaxFlag"`

	// Global limits (globalGridDischargeMax, globalGridChargeMax, globalSettingStatus)
	// represent aggregate power caps across all installed aPower units combined.
	// PeakDemandGridMax is the grid import power ceiling for peak demand management tariffs.
	// IsNem3 / IsCalifornia flags indicate California Net Billing Tariff (NEM 3.0) recommendation profiles.
}

type franklinGatewayTouListV2Result struct {
	CurrentID   int               `json:"currendId"` // yes, it's misspelled in the Franklin API
	List        []franklinTouItem `json:"list"`
	VPPSOC      franklinVPPSOC    `json:"vppSocVo"`
	TodayVPPSOC franklinTodayVPP  `json:"todayVppVo"`

	// StormHedgeEnabled is 1 when Storm Hedge is active, 0 otherwise (misspelled in API).
	StormHedgeEnabled int `json:"stromEn"`

	// StopMode is 0 for normal operation, 1 when emergency stop mode is active ("Disconnect the FranklinWH System").
	StopMode int `json:"stopMode"`

	// GridChargeEnabled is 1 when grid charging is enabled in TOU settings, 0 otherwise.
	GridChargeEnabled *int `json:"gridChargeEn"`
}

type franklinTOUTemplateResponse struct {
	Template     franklinTOUTemplate   `json:"template"`
	StrategyList []franklinTOUStrategy `json:"strategyList"`
}

type franklinTOUStrategy struct {
	ID            int                 `json:"id,omitempty"`
	SeasonName    string              `json:"seasonName"`
	Month         string              `json:"month"`
	TemplateID    int                 `json:"templateId,omitempty"`
	DayTypeVoList []franklinDayTypeVo `json:"dayTypeVoList"`
}

type franklinDayTypeVo struct {
	ID           int                    `json:"id,omitempty"`
	DayName      string                 `json:"dayName"`
	DayType      int                    `json:"dayType"`
	DetailVoList []franklinDetailVoItem `json:"detailVoList"`
}

type franklinDetailVoItem struct {
	ID            int    `json:"id,omitempty"`
	Name          string `json:"name"`
	StartHourTime string `json:"startHourTime"`
	EndHourTime   string `json:"endHourTime"`
	WaveType      int    `json:"waveType"`
	DispatchID    int    `json:"dispatchId"`
}

type franklinTOUTemplate struct {
	ID                 int    `json:"id"`
	GatewayID          string `json:"gatewayId"`
	CountryID          int    `json:"countryId"`
	ProvinceID         int    `json:"provinceId"`
	CountryEn          string `json:"countryEn"`
	ProvinceEn         string `json:"provinceEn"`
	ElectricCompany    string `json:"electricCompany"`
	EleCompanyFullName string `json:"eleCompanyFullName"`
	TariffName         string `json:"tariffName"`
	ElectricityType    int    `json:"electricityType"`
}

type franklinVPPSOC struct {
	VPPMaxSoc float64 `json:"vppMaxSoc"`
	VPPMinSoc float64 `json:"vppMinSoc"`
	VPPSoc    float64 `json:"vppSoc"`
	// it's not clear if this means they're actively enrolled or just that VPP
	// is available
	VPPApplicable bool `json:"vppSocDisplayFlag"`
}

type franklinTodayVPP struct {
	StartTime  *string `json:"startTime"`
	EndTime    *string `json:"endTime"`
	ShowVPPTip bool    `json:"showVppTipFlag"`

	// TODO: what about vppStatus, vppId, vppFlag, programFlag
}

type franklinTouItem struct {
	ID                 int              `json:"id"`
	OldIndex           int              `json:"oldIndex"`
	Name               string           `json:"name"`
	ReserveSOC         float64          `json:"soc"`
	MinSOC             float64          `json:"minSoc"`
	MaxSOC             float64          `json:"maxSoc"`
	CanEditReserveSOC  bool             `json:"editSocFlag"`
	WorkMode           franklinWorkMode `json:"workMode"`
	ElectricityType    int              `json:"electricityType"`
	BackupForeverFlag  int              `json:"backupForeverFlag"`
	TimerStartTimeUnix string           `json:"timerStartTimeZero"`

	// MultiSOCFlag indicates whether per-unit or tiered SOC limits are configured across multiple aPower units.
	MultiSOCFlag bool `json:"multiSOCFlag"`
}

type franklinFHPPowerByDayResult struct {
	SOCArray        []float64 `json:"socArray"`
	KwhTotalArray   []float64 `json:"kwhTotalArray"` // Unused for now
	RunStatusArray  []int     `json:"runStatusArray"`
	DeviceTimeArray []string  `json:"deviceTimeArray"`

	SolarToHomeKWHRates    []float64 `json:"powerSolarHomeArray"`
	SolarToGridKWHRates    []float64 `json:"powerSolarGirdArray"` // misspelled
	SolarToBatteryKWHRates []float64 `json:"powerSolarFhpArray"`

	GridToBatteryKWHRates []float64 `json:"powerGirdFhpArray"` // misspelled
	GridToHomeKWHRates    []float64 `json:"powerGirdHomeArray"`

	BatteryToGridKWHRates []float64 `json:"powerFhpGirdArray"` // misspelled
	BatteryToHomeKWHRates []float64 `json:"powerFhpHomeArray"`

	// Generators and V2L ignored for now
}

type franklinHomeGateway struct {
	ID       string `json:"id"`
	Status   int    `json:"status"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	ZoneInfo string `json:"zoneInfo"`
}

type franklinStormListResult struct {
	ID           int    `json:"id"`
	Onset        string `json:"onset"`
	Severity     string `json:"severity"`
	DurationMins int    `json:"durationTime"`
}

type franklinEnergyPoint struct {
	Timestamp             time.Time
	SolarToHomeKWHRate    float64
	SolarToGridKWHRate    float64
	SolarToBatteryKWHRate float64
	GridToBatteryKWHRate  float64
	GridToHomeKWHRate     float64
	BatteryToGridKWHRate  float64
	BatteryToHomeKWHRate  float64
	BatterySOC            float64
}

type franklinProgramDetailsResult struct {
	ProgramAttendingStatus int    `json:"programAttendingStatus"`
	UpdateTime             string `json:"updateTime"`
	LatestEventID          string `json:"latestEventId"`
	// 2 means completed
	LatestEventStatus    int     `json:"latestEventStatus"`
	LatestEventStartTime string  `json:"latestEventStartTime"`
	LatestEventEndTime   string  `json:"latestEventEndTime"`
	ProgramID            int     `json:"programId"`
	ProgramName          string  `json:"programName"`
	PartnerID            int     `json:"partnerId"`
	PartnerName          string  `json:"partnerName"`
	VPPSoc               float64 `json:"vppSoc"`
	VPPMinSoc            float64 `json:"vppMinSoc"`
	VPPMaxSoc            float64 `json:"vppMaxSoc"`
}

type franklinEHEvent struct {
	ID        int     `json:"id"`
	EventID   string  `json:"eventId"`
	VPPSoc    float64 `json:"vppSoc"`
	StartTime string  `json:"startTime"`
	EndTime   string  `json:"endTime"`
	// EventStatus 0 means pending, 2 means completed, 3 means cancelled, 4 means opt-out
	// TODO: determine the rest of the statuses
	EventStatus int `json:"eventStatus"`
}
