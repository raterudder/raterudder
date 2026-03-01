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
)

const franklinLoginPath = "hes-gateway/terminal/initialize/appUserOrInstallerLogin"

// Franklin implements the System interface for FranklinWH.
// It interacts with the FranklinWH API to monitor and control the energy storage system.
type Franklin struct {
	client           *http.Client
	baseURL          string
	username         string
	md5Password      string
	gatewayID        string
	tokenStr         string
	mu               sync.Mutex
	settings         types.Settings
	deviceInfoCache  deviceInfoV2Result
	deviceInfoExpiry time.Time
}

type franklinMode struct {
	ID                int
	Name              string
	WorkMode          int
	OldIndex          int
	ElectricityType   int
	ReserveSOC        float64
	CanEditReserveSOC bool
}

func newFranklin() *Franklin {
	return &Franklin{
		client:  common.HTTPClient(time.Minute),
		baseURL: "https://energy.franklinwh.com",
	}
}

func franklinInfo() types.ESSProviderInfo {
	return types.ESSProviderInfo{
		ID:   "franklin",
		Name: "FranklinWH",
		Credentials: []types.ESSCredential{
			{
				Field:    "username",
				Name:     "Email",
				Type:     "string",
				Required: true,
			},
			{
				Field:    "password",
				Name:     "Password",
				Type:     "password",
				Required: true,
			},
			{
				Field:       "gatewayID",
				Name:        "Gateway ID (Optional)",
				Type:        "string",
				Required:    false,
				Description: "If left empty, RateRudder will attempt to auto-discover the gateway ID.",
			},
		},
	}
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
	if creds.Franklin == nil {
		return creds, false, ErrCredentialsMissing
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var changed bool

	// If raw password is provided, hash it inside backend to handle it securely
	// and avoid frontend dependencies.
	if creds.Franklin.Password != "" {
		hash := md5.Sum([]byte(creds.Franklin.Password))
		creds.Franklin.MD5Password = hex.EncodeToString(hash[:])
		creds.Franklin.Password = ""
		changed = true
	}

	// Determine if we need a fresh login. We need one when:
	// - There is no cached token in the supplied credentials (first time), OR
	// - The username/password in the incoming credentials differ from what we
	//   already have verified (detected by comparing against stored struct state
	//   only when we have previously authenticated with those credentials).
	needLogin := creds.Franklin.Token == ""
	if !needLogin && f.username != "" {
		// We've previously authenticated; check if credentials have changed.
		needLogin = f.username != creds.Franklin.Username || f.md5Password != creds.Franklin.MD5Password
	}

	if needLogin {
		log.Ctx(ctx).DebugContext(ctx, "logging in to franklin")
		// Credentials changed or no cached token — must login fresh.
		token, err := f.login(ctx, creds.Franklin.Username, creds.Franklin.MD5Password)
		if err != nil {
			return creds, false, err
		}
		f.username = creds.Franklin.Username
		f.md5Password = creds.Franklin.MD5Password
		f.tokenStr = token
		// Persist the new token so we can skip login next time.
		creds.Franklin.Token = token
		changed = true
	} else {
		log.Ctx(ctx).DebugContext(ctx, "restored franklin credentials from cache")
		// Restore the token from credentials so we can skip login.
		f.username = creds.Franklin.Username
		f.md5Password = creds.Franklin.MD5Password
		f.tokenStr = creds.Franklin.Token
	}

	if creds.Franklin.GatewayID == "" {
		id, err := f.getDefaultGatewayID(ctx)
		if err != nil {
			return creds, false, err
		}
		log.Ctx(ctx).InfoContext(ctx, "automatically selected gateway", slog.String("gatewayID", id))
		creds.Franklin.GatewayID = id
		changed = true
	}
	f.gatewayID = creds.Franklin.GatewayID

	// Validate the credentials by fetching device info. This confirms the token
	// and gateway ID are working. The result is cached so the subsequent
	// GetStatus call will reuse it for free.
	if _, err := f.getDeviceInfoWithCache(ctx); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "franklin credential validation failed", slog.Any("error", err))
		return creds, false, fmt.Errorf("credential validation failed: %w", err)
	}

	return creds, changed, nil
}

type loginResult struct {
	UserID  int    `json:"userId"`
	Token   string `json:"token"`
	Version string `json:"version"`
}

// ensureLogin will not login again if the token we have cached is still valid
func (f *Franklin) ensureLogin(ctx context.Context) error {
	if f.tokenStr == "" {
		token, err := f.login(ctx, f.username, f.md5Password)
		if err != nil {
			return fmt.Errorf("failed to login: %w", err)
		}
		f.tokenStr = token
	}

	if f.gatewayID == "" {
		id, err := f.getDefaultGatewayID(ctx)
		if err != nil {
			return fmt.Errorf("failed to get default gateway id: %w", err)
		}
		f.gatewayID = id
		log.Ctx(ctx).InfoContext(ctx, "automatically selected gateway", slog.String("gatewayID", f.gatewayID))
	}
	return nil
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

	// TODO: what is the actual expiry of the token?
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

func (f *Franklin) newPostJSONRequest(ctx context.Context, endpoint string, data interface{}) (*http.Request, error) {
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

func (f *Franklin) doRequest(req *http.Request, dest interface{}) error {
	isLogin := strings.HasSuffix(req.URL.Path, franklinLoginPath)

	// we try up to 2 times because we might have an expired token
	for i := 0; i < 2; i++ {
		if !isLogin {
			req.Header.Set("logintoken", f.tokenStr)
		}

		// TODO: should we set softwareversion, lang, optsystemversion, opttime, optdevicename, optsource, optdevice

		resp, err := f.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusUnauthorized && !isLogin && f.tokenStr != "" {
				log.Ctx(req.Context()).DebugContext(req.Context(), "franklin token expired")
				f.tokenStr = ""
				if err := f.ensureLogin(req.Context()); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("status %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		var fr franklinResponse
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&fr); err != nil {
			log.Ctx(req.Context()).ErrorContext(req.Context(), "failed to decode franklin response", slog.Any("error", err), slog.String("body", string(body)))
			return err
		}

		if !fr.Success && fr.Code != 200 {
			// if we got a 401 error, it wasn't a login, and we sent a token then we
			// need to get another token
			if fr.Code == 401 && !isLogin && f.tokenStr != "" {
				log.Ctx(req.Context()).DebugContext(req.Context(), "franklin token expired", slog.String("message", fr.Message))
				f.tokenStr = ""
				if err := f.ensureLogin(req.Context()); err != nil {
					return err
				}
				continue
			}
			if fr.Message == "" {
				log.Ctx(req.Context()).ErrorContext(req.Context(), "franklin api unknown error", slog.String("body", string(body)))
				return fmt.Errorf("franklin unknown error")
			}
			log.Ctx(req.Context()).ErrorContext(req.Context(), "franklin api error", slog.String("message", fr.Message))
			return fmt.Errorf("franklin api error: %s", fr.Message)
		}

		if dest != nil {
			if err := json.Unmarshal(fr.Result, dest); err != nil {
				log.Ctx(req.Context()).ErrorContext(req.Context(), "failed to decode franklin result", slog.Any("error", err))
				return fmt.Errorf("failed to decode franklin result: %w", err)
			}
		} else {
			log.Ctx(req.Context()).DebugContext(req.Context(), "franklin request success (no destination)", slog.String("url", req.URL.String()))
		}
		return nil
	}
	return nil
}

func (f *Franklin) getRuntimeData(ctx context.Context) (deviceCompositeInfoResult, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	// 0 is set on the first call and subsequent calls should set to 1
	params.Set("refreshFlag", "0")

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getDeviceCompositeInfo", params)
	if err != nil {
		return deviceCompositeInfoResult{}, err
	}

	var res deviceCompositeInfoResult
	if err := f.doRequest(req, &res); err != nil {
		return deviceCompositeInfoResult{}, fmt.Errorf("getDeviceCompositeInfo failed: %w", err)
	}

	// solar inverters/combiners use power so it can be negative, just set it to 0
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
		slog.Int("mode", res.RuntimeData.TOUID),
	)

	return res, nil
}

func (f *Franklin) getDefaultGatewayID(ctx context.Context) (string, error) {
	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getHomeGatewayList", nil)
	if err != nil {
		return "", err
	}

	var list []homeGateway
	if err := f.doRequest(req, &list); err != nil {
		return "", err
	}

	if len(list) == 1 {
		return list[0].ID, nil
	}
	return "", fmt.Errorf("found %d gateways, expected 1", len(list))
}

func (f *Franklin) getDeviceInfo(ctx context.Context) (deviceInfoV2Result, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("lang", "en_US")

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/getDeviceInfoV2", params)
	if err != nil {
		return deviceInfoV2Result{}, err
	}

	var res deviceInfoV2Result
	if err := f.doRequest(req, &res); err != nil {
		return deviceInfoV2Result{}, err
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
func (f *Franklin) getDeviceInfoWithCache(ctx context.Context) (deviceInfoV2Result, error) {
	if time.Now().Before(f.deviceInfoExpiry) {
		return f.deviceInfoCache, nil
	}
	di, err := f.getDeviceInfo(ctx)
	if err != nil {
		return deviceInfoV2Result{}, err
	}
	f.deviceInfoCache = di
	f.deviceInfoExpiry = time.Now().Add(time.Minute)
	return di, nil
}

// GetStatus returns the status of the franklin system
func (f *Franklin) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting franklin system status")
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.ensureLogin(ctx); err != nil {
		return types.SystemStatus{}, err
	}

	rd, err := f.getRuntimeData(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	di, err := f.getDeviceInfoWithCache(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	modes, err := f.getAvailableModes(ctx)
	if err != nil {
		return types.SystemStatus{}, err
	}

	pc, err := f.getPowerControl(ctx)
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
		if alarm.Name == "SIM card not inserted" {
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

	stormHedge := rd.RuntimeData.TOUID == 6

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

	return types.SystemStatus{
		Timestamp:               time.Now().In(di.location),
		BatterySOC:              rd.RuntimeData.SOC,
		EachBatterySOC:          rd.RuntimeData.EachSOC,
		BatteryKW:               rd.RuntimeData.PowerBattery,
		EachBatteryKW:           rd.RuntimeData.PowerEachBattery,
		SolarKW:                 rd.RuntimeData.PowerSolar,
		GridKW:                  rd.RuntimeData.PowerGrid,
		HomeKW:                  rd.RuntimeData.PowerLoad,
		BatteryCapacityKWH:      di.TotalBatteryCapacityKWH,
		EmergencyMode:           stormHedge || modes.currentMode.WorkMode == 3,
		CanExportSolar:          pc.GridFeedMaxFlag == GridFeedMaxFlagSolarOnly || pc.GridFeedMaxFlag == GridFeedMaxFlagBatteryAndSolar,
		CanExportBattery:        pc.GridFeedMaxFlag == GridFeedMaxFlagBatteryAndSolar,
		CanImportBattery:        pc.GridMaxFlag == GridMaxFlagChargeFromGrid,
		ElevatedMinBatterySOC:   modes.currentMode.ReserveSOC > 0 && modes.currentMode.ReserveSOC > f.settings.MinBatterySOC,
		BatteryAboveMinSOC:      rd.RuntimeData.SOC >= modes.currentMode.ReserveSOC,
		BatteryChargingDisabled: batteryChargingDisabled,

		// TODO: get this from hes-gateway/common/getPowerCapConfigList
		MaxBatteryChargeKW:    8 * float64(len(rd.RuntimeData.EachSOC)),
		MaxBatteryDischargeKW: 10 * float64(len(rd.RuntimeData.EachSOC)),

		Alarms: alarms,
		Storms: storms,
	}, nil
}

func (f *Franklin) getPowerControl(ctx context.Context) (getPowerControlSettingResult, error) {
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/tou/getPowerControlSetting", params)
	if err != nil {
		return getPowerControlSettingResult{}, err
	}

	var res getPowerControlSettingResult
	if err := f.doRequest(req, &res); err != nil {
		return getPowerControlSettingResult{}, err
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

func (f *Franklin) setPowerControl(ctx context.Context, pc getPowerControlSettingResult) error {
	data := map[string]interface{}{
		"gatewayId": f.gatewayID,
		// TODO: what is -1 here?
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

	// TODO: powerControlTipMsg
	return f.doRequest(req, &struct{}{})
}

type availableModes struct {
	list              []franklinMode
	selfConsumption   franklinMode
	currentMode       franklinMode
	stormHedgeEnabled int
}

func (f *Franklin) getAvailableModes(ctx context.Context) (availableModes, error) {
	params := url.Values{}
	params.Set("showType", "1")
	params.Set("gatewayId", f.gatewayID)

	req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/getGatewayTouListV2", params)
	if err != nil {
		return availableModes{}, err
	}

	var res gatewayTouListV2Result
	if err := f.doRequest(req, &res); err != nil {
		return availableModes{}, err
	}

	var sc franklinMode
	var current franklinMode
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
		case 1: // time-of-use
			modes[i] = m
		case 2: // self consumption
			modes[i] = m
			sc = modes[i]
		case 3: // backup
			modes[i] = m
		default:
			log.Ctx(ctx).WarnContext(
				ctx,
				"unknown work mode",
				slog.Int("id", item.ID),
				slog.String("name", item.Name),
				slog.Int("workMode", item.WorkMode),
				slog.Int("oldIndex", item.OldIndex),
			)
		}
		if item.ID == res.CurrentID {
			current = franklinMode{
				ID:              item.ID,
				Name:            item.Name,
				WorkMode:        item.WorkMode,
				OldIndex:        item.OldIndex,
				ElectricityType: item.ElectricityType,
				ReserveSOC:      item.ReserveSOC,
			}
		}
		foundIDs[i] = fmt.Sprintf("%d", item.ID)
	}

	if current.ID == 0 {
		log.Ctx(ctx).WarnContext(
			ctx,
			"franklin current tou id not found",
			slog.Int("currentTouID", res.CurrentID),
			slog.String("foundIDs", strings.Join(foundIDs, ",")),
		)
	}

	return availableModes{
		list:              modes,
		selfConsumption:   sc,
		stormHedgeEnabled: res.StormHedgeEnabled,
		currentMode:       current,
	}, nil
}

// SetModes sets the battery and solar modes for the franklin system
func (f *Franklin) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	log.Ctx(ctx).DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol))
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.ensureLogin(ctx); err != nil {
		return err
	}

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return nil
	}

	rd, err := f.getRuntimeData(ctx)
	if err != nil {
		return err
	}

	// 6 means storm hedge
	if rd.RuntimeData.TOUID == 6 {
		log.Ctx(ctx).InfoContext(ctx, "device is in storm hedge mode, skipping set modes")
		return errors.New("device is in storm hedge mode")
	}

	modes, err := f.getAvailableModes(ctx)
	if err != nil {
		return err
	}

	if modes.currentMode.WorkMode == 3 {
		log.Ctx(ctx).InfoContext(ctx, "device is in backup mode, skipping set modes")
		return errors.New("device is in backup mode")
	}

	if modes.selfConsumption == (franklinMode{}) {
		log.Ctx(ctx).ErrorContext(ctx, "self consumption mode not available", slog.Any("modes", modes))
		return errors.New("self consumption mode not available")
	}
	sc := modes.selfConsumption
	alreadySC := sc.ID == modes.currentMode.ID

	data := url.Values{}
	data.Set("gatewayId", f.gatewayID)
	data.Set("currendId", fmt.Sprint(sc.ID)) // yes, this is misspelled
	data.Set("workMode", fmt.Sprint(sc.WorkMode))
	data.Set("electricityType", fmt.Sprint(sc.ElectricityType))
	data.Set("oldIndex", fmt.Sprint(sc.OldIndex))
	data.Set("stromEn", fmt.Sprint(modes.stormHedgeEnabled))

	minBatterySOC := f.settings.MinBatterySOC
	if minBatterySOC < 5 {
		minBatterySOC = 5
	}

	soc := sc.ReserveSOC

	log.Ctx(ctx).DebugContext(ctx, "existing reserve SOC", slog.Float64("reserveSOC", sc.ReserveSOC))

	pc, err := f.getPowerControl(ctx)
	if err != nil {
		return err
	}
	var updatedPC bool
	var updatedModeOrSOC bool
	switch bat {
	case types.BatteryModeChargeAny:
		// if they want to charge the battery then set the SOC to 100 to force it to
		// charge if its not charging already
		// note: since we're not setting emergency backup mode solar will still be
		// used to power the home first then spill over into the battery
		if !sc.CanEditReserveSOC {
			log.Ctx(ctx).WarnContext(ctx, "cannot edit reserve SOC")
			return errors.New("cannot edit reserve SOC")
		}
		soc = 100
		updatedModeOrSOC = true
		if f.settings.GridChargeBatteries {
			if pc.GridMaxFlag != GridMaxFlagChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagChargeFromGrid
				updatedPC = true
			}
		} else {
			if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
				updatedPC = true
			}
		}
	case types.BatteryModeChargeSolar:
		// we disallow charging from the grid if they only want to charge via solar
		// and otherwise set the SOC to 100
		// note: since we're not setting emergency backup mode solar will still be
		// used to power the home first then spill over into the battery
		if !sc.CanEditReserveSOC {
			log.Ctx(ctx).WarnContext(ctx, "cannot edit reserve SOC")
			return errors.New("cannot edit reserve SOC")
		}
		soc = 100
		updatedModeOrSOC = true
		if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
			pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
			updatedPC = true
		}
	case types.BatteryModeLoad:
		// we set the SOC to the minimum battery SOC to ensure we start discharging
		// if we're somehow less than this soc, we'll charge from the solar, unless
		// solar is unavailable then it'll charge from the grid
		// it seems like this accepts an int value
		soc = minBatterySOC
		updatedModeOrSOC = true
		if f.settings.GridChargeBatteries {
			if pc.GridMaxFlag != GridMaxFlagChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagChargeFromGrid
				updatedPC = true
			}
		} else {
			if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
				pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
				updatedPC = true
			}
		}
	case types.BatteryModeStandby:
		rd, err := f.getRuntimeData(ctx)
		if err != nil {
			return err
		}
		// we floor the SOC to ensure we don't set it to a value that would cause the
		// battery to charge
		// make sure we don't set it to less than the minimum battery SOC
		if !sc.CanEditReserveSOC {
			log.Ctx(ctx).WarnContext(ctx, "cannot edit reserve SOC")
			return errors.New("cannot edit reserve SOC")
		}
		soc = math.Max(math.Floor(rd.RuntimeData.SOC), minBatterySOC)
		updatedModeOrSOC = true
		if pc.GridMaxFlag != GridMaxFlagNoChargeFromGrid {
			pc.GridMaxFlag = GridMaxFlagNoChargeFromGrid
			updatedPC = true
		}
	case types.BatteryModeNoChange:
		// Do not change battery settings
	default:
		return fmt.Errorf("unknown battery mode: %v", bat)
	}

	// round to the nearest integer to minimize the chance of the battery charging
	// or discharging when we don't want it to
	data.Set("soc", strconv.Itoa(int(math.Round(soc))))

	switch sol {
	case types.SolarModeAny:
		// if settings allow us to export solar then we start exporting solar
		// this will prefer home, then charge, then export
		if f.settings.GridExportSolar && f.settings.GridExportBatteries {
			if pc.GridFeedMaxFlag != GridFeedMaxFlagBatteryAndSolar {
				pc.GridFeedMaxFlag = GridFeedMaxFlagBatteryAndSolar
				updatedPC = true
			}
		} else if f.settings.GridExportSolar {
			if pc.GridFeedMaxFlag != GridFeedMaxFlagSolarOnly {
				pc.GridFeedMaxFlag = GridFeedMaxFlagSolarOnly
				updatedPC = true
			}
		} else {
			if pc.GridFeedMaxFlag != GridFeedMaxFlagNoExport {
				pc.GridFeedMaxFlag = GridFeedMaxFlagNoExport
				updatedPC = true
			}
		}
	case types.SolarModeNoExport:
		// set the flag to not export solar
		if pc.GridFeedMaxFlag != GridFeedMaxFlagNoExport {
			pc.GridFeedMaxFlag = GridFeedMaxFlagNoExport
			updatedPC = true
		}
	case types.SolarModeNoChange:
		// Do nothing for solar
	default:
		return fmt.Errorf("unknown solar mode: %v", sol)
	}

	if updatedModeOrSOC {
		if f.settings.DryRun {
			if alreadySC {
				log.Ctx(ctx).DebugContext(
					ctx,
					"dry run: would've updated just soc",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
			} else {
				log.Ctx(ctx).DebugContext(
					ctx,
					"dry run: would've tou mode",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
			}
		} else {
			if alreadySC {
				log.Ctx(ctx).InfoContext(
					ctx,
					"updating franklin soc",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
				params := url.Values{}
				params.Set("gatewayId", f.gatewayID)
				params.Set("workMode", strconv.Itoa(sc.WorkMode))
				params.Set("electricityType", strconv.Itoa(sc.ElectricityType))
				params.Set("soc", data.Get("soc"))

				req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/updateSocV2", params)
				if err != nil {
					return err
				}
				if err := f.doRequest(req, &struct{}{}); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update soc", slog.Any("error", err))
					return err
				}
			} else {
				log.Ctx(ctx).InfoContext(
					ctx,
					"updating franklin tou mode",
					slog.String("soc", data.Get("soc")),
					slog.String("workMode", data.Get("workMode")),
				)
				req, err := f.newPostQueryRequest(ctx, "hes-gateway/terminal/tou/updateTouModeV2", data)
				if err != nil {
					return err
				}
				if err := f.doRequest(req, &struct{}{}); err != nil {
					log.Ctx(ctx).ErrorContext(ctx, "failed to update tou mode", slog.Any("error", err))
					return err
				}
			}
		}
	}

	if updatedPC {
		if f.settings.DryRun {
			log.Ctx(ctx).DebugContext(
				ctx,
				"dry run: would've set power control",
				slog.Float64("gridMax", pc.GridMax),
				slog.Int("gridMaxFlag", int(pc.GridMaxFlag)),
				slog.Float64("gridFeedMax", pc.GridFeedMax),
				slog.Int("gridFeedMaxFlag", int(pc.GridFeedMaxFlag)),
			)
		} else {
			err := f.setPowerControl(ctx, pc)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to set power control", slog.Any("error", err))
				return err
			}
		}
	}

	return nil
}

// GetEnergyHistory retrieves energy history for the specified period.
// It aggregates 5-minute intervals into hourly EnergyStats.
func (f *Franklin) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting franklin energy history", slog.String("start", start.String()), slog.String("end", end.String()))
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.ensureLogin(ctx); err != nil {
		return nil, err
	}

	var di deviceInfoV2Result
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

	var allStats []types.EnergyStats

	startInLoc := start.In(di.location)
	endInLoc := end.In(di.location)

	// Iterate through days
	// Start from the beginning of the start day
	current := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 0, 0, 0, 0, di.location)
	for current.Before(endInLoc) || current.Equal(endInLoc) {
		if current.After(time.Now()) {
			break
		}

		stats, err := f.getEnergyStatsForDay(ctx, current, di.location)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get energy stats for day", slog.String("day", current.Format("2006-01-02")), slog.Any("error", err))
			// Continue or return error? Let's return error to be safe.
			return nil, err
		}

		// Filter stats that are within the requested range
		for _, s := range stats {
			hourEnd := s.TSHourStart.Add(time.Hour)
			if !s.TSHourStart.Before(start) && !hourEnd.After(end) {
				allStats = append(allStats, s)
			}
		}

		current = current.AddDate(0, 0, 1)
	}

	return allStats, nil
}

func (f *Franklin) getStormList(ctx context.Context) ([]stormListResult, error) {
	params := url.Values{}
	params.Set("equipNo", f.gatewayID)

	req, err := f.newGetRequest(ctx, "hes-gateway/terminal/weather/getProgressingStormList", params)
	if err != nil {
		return nil, err
	}

	var res []stormListResult
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (f *Franklin) getEnergyStatsForDay(ctx context.Context, day time.Time, loc *time.Location) ([]types.EnergyStats, error) {
	day = day.In(loc)
	params := url.Values{}
	params.Set("gatewayId", f.gatewayID)
	params.Set("dayTime", day.Format("2006-01-02"))

	req, err := f.newGetRequest(ctx, "api-energy/power/getFhpPowerByDay", params)
	if err != nil {
		return nil, err
	}

	var res fhpPowerByDayResult
	if err := f.doRequest(req, &res); err != nil {
		return nil, err
	}

	// Aggregate 5-min data into hourly buckets
	hourlyStats := make(map[string]*types.EnergyStats)
	var sortedKeys []string

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

	for i, timeStr := range res.DeviceTimeArray {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, loc)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse time", slog.String("time", timeStr), slog.Any("error", err))
			return nil, err
		}
		// figure out the duration of this "bucket"
		var duration time.Duration
		if len(res.DeviceTimeArray) > i+1 {
			nextT, err := time.ParseInLocation("2006-01-02 15:04:05", res.DeviceTimeArray[i+1], loc)
			if err != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to parse time", slog.String("time", timeStr), slog.Any("error", err))
				return nil, err
			}
			duration = nextT.Sub(t)
		} else {
			nextDay := day.AddDate(0, 0, 1)
			nextDay = time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, loc)
			// duration until the next day?
			duration = nextDay.Sub(t)
		}

		// Determine hour bucket
		hourKey := t.Format("2006-01-02 15:00:00")
		if _, exists := hourlyStats[hourKey]; !exists {
			hourlyStats[hourKey] = &types.EnergyStats{
				TSHourStart:   time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()),
				MinBatterySOC: res.SOCArray[i],
				MaxBatterySOC: res.SOCArray[i],
			}
			sortedKeys = append(sortedKeys, hourKey)
		}
		s := hourlyStats[hourKey]

		// collect all relevant stats for the time
		// and convert them all to KWH from the rate of kwh in that duration
		solarToHome := res.SolarToHomeKWHRates[i] * (duration.Hours())
		solarToGrid := res.SolarToGridKWHRates[i] * (duration.Hours())
		solarToBat := res.SolarToBatteryKWHRates[i] * (duration.Hours())
		gridToBat := res.GridToBatteryKWHRates[i] * (duration.Hours())
		gridToHome := res.GridToHomeKWHRates[i] * (duration.Hours())
		batToGrid := res.BatteryToGridKWHRates[i] * (duration.Hours())
		batToHome := res.BatteryToHomeKWHRates[i] * (duration.Hours())

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

		if res.SOCArray[i] < s.MinBatterySOC {
			s.MinBatterySOC = res.SOCArray[i]
		}
		if res.SOCArray[i] > s.MaxBatterySOC {
			s.MaxBatterySOC = res.SOCArray[i]
		}
	}

	// Sort the keys in chronological order
	sort.Strings(sortedKeys)

	var result []types.EnergyStats
	for _, key := range sortedKeys {
		result = append(result, *hourlyStats[key])
	}

	return result, nil
}

// Internal Structs

type currentAlarmVO struct {
	ID                   int    `json:"id"`
	GatewayID            string `json:"gatewayId"`
	AlarmForSerialNumber string `json:"alarmEqSn"`
	AlarmCode            string `json:"alarmCode"`
	Time                 string `json:"time"`
	Name                 string `json:"logName"`
	Explanation          string `json:"alarmExplanation"`
	Plan                 string `json:"plan"`
	// TODO: level?
}

type deviceCompositeInfoResult struct {
	CurrentWorkMode  int              `json:"currentWorkMode"`
	DeviceStatus     int              `json:"deviceStatus"`
	RuntimeData      runtimeData      `json:"runtimeData"`
	Valid            bool             `json:"valid"`
	CurrentAlarmList []currentAlarmVO `json:"currentAlarmVOList"`

	// there's also "solarHaveVo": {...}
}

type runtimeData struct {
	TOUID    int    `json:"mode"`
	ModeName string `json:"name"`

	// 0 is standby
	// 1 is charging
	// 2 is discharging
	// 3 is fault?
	// 5 is off-grid standby
	// 6 is off-grid charging
	// 7 is off-grid discharging
	RunStatus int `json:"run_status"`

	SOC     float64   `json:"soc"`
	EachSOC []float64 `json:"fhpSoc"`

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

	// TODO: t_amb? temperature?
	// TODO: kwhSolarLoad, kwhGridLoad, kwhFhpLoad, kwhGenLoad
	// TODO: solarPower (seems to be 10x p_sun?)
}

type deviceInfoV2Result struct {
	GatewayID               string        `json:"gatewayId"`
	DeviceTime              string        `json:"deviceTime"`
	TimeZone                string        `json:"zoneInfo"`
	SystemHardwareVersion   int           `json:"sysHdVersionInt"`
	TotalBatteryCapacityKWH float64       `json:"totalCap"`
	TotalBatteryPowerKW     float64       `json:"fixedPowerTotal"`
	BatteryList             []batteryInfo `json:"batteryList"`

	location *time.Location

	// TODO: solarFlag, solarTipMsg
	// TODO: activeStatus
	// TODO: sleepStatus, blackSleepFlag
}

type batteryInfo struct {
	Serial     int `json:"id"`
	CapacityWH int `json:"rateBatCap"`
	PowerW     int `json:"ratedPwr"`
}

type gridMaxFlag int

const (
	GridMaxFlagNoChargeFromGrid gridMaxFlag = 1
	GridMaxFlagChargeFromGrid   gridMaxFlag = 2
)

type gridFeedMaxFlag int

const (
	GridFeedMaxFlagNoExport        gridFeedMaxFlag = 3
	GridFeedMaxFlagSolarOnly       gridFeedMaxFlag = 1
	GridFeedMaxFlagBatteryAndSolar gridFeedMaxFlag = 2
)

type getPowerControlSettingResult struct {
	GridFeedMax     float64         `json:"gridFeedMax"`
	GridFeedMaxFlag gridFeedMaxFlag `json:"gridFeedMaxFlag"`
	GridMax         float64         `json:"gridMax"`
	GridMaxFlag     gridMaxFlag     `json:"gridMaxFlag"`

	// TODO: difference between global and non-global?
	// TODO: globalGridDischargeMax, globalGridChargeMax, globalSettingStatus (does this being 1 mean we use global instead?)
	// TODO: peakDemandGridMax
	// TODO: isNem3, isCalifornia
}

type gatewayTouListV2Result struct {
	CurrentID int       `json:"currendId"` // yes, it's misspelled
	List      []touItem `json:"list"`

	// TODO: validate this
	StormHedgeEnabled int `json:"stromEn"`
}

type touItem struct {
	ID                 int     `json:"id"`
	OldIndex           int     `json:"oldIndex"`
	Name               string  `json:"name"`
	ReserveSOC         float64 `json:"soc"`
	MinSOC             float64 `json:"minSoc"`
	MaxSOC             float64 `json:"maxSoc"`
	CanEditReserveSOC  bool    `json:"editSocFlag"`
	WorkMode           int     `json:"workMode"`
	ElectricityType    int     `json:"electricityType"`
	BackupForeverFlag  int     `json:"backupForeverFlag"`
	TimerStartTimeUnix string  `json:"timerStartTimeZero"`

	// TODO: multiSOCFlag
	// TODO: stopMode
	// TODO: gridChargeEn
	// TODO: vppSocVo, todayVppVo
}

type fhpPowerByDayResult struct {
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

type homeGateway struct {
	ID       string `json:"id"`
	Status   int    `json:"status"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	ZoneInfo string `json:"zoneInfo"`
}

type stormListResult struct {
	ID           int    `json:"id"`
	Onset        string `json:"onset"`
	Severity     string `json:"severity"`
	DurationMins int    `json:"durationTime"`
}
