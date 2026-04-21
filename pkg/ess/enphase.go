package ess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

var errEnphaseUnauthorized = errors.New("unauthorized")

type Enphase struct {
	client       *http.Client
	baseURL      *url.URL
	mu           sync.Mutex
	settings     types.Settings
	username     string
	password     string
	sessionID    string
	managerToken string
	systemID     int
	dataCache    enphaseDataResult
	dataExpiry   time.Time
}

func newEnphase() *Enphase {
	u, err := url.Parse("https://enlighten.enphaseenergy.com")
	if err != nil {
		panic(fmt.Errorf("failed to parse enphase base url: %w", err))
	}
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		panic(fmt.Errorf("failed to create enphase cookie jar: %w", err))
	}
	return &Enphase{
		client: &http.Client{
			Transport: common.HTTPClient(time.Minute).Transport,
			Timeout:   time.Minute,
			Jar:       jar,
		},
		baseURL: u,
	}
}

func enphaseInfo() types.ESSProviderInfo {
	return types.ESSProviderInfo{
		ID:     "enphase",
		Name:   "Enphase",
		Hidden: true,
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
		},
	}
}

func (e *Enphase) Name() string {
	return "enphase"
}

func (e *Enphase) ApplySettings(ctx context.Context, settings types.Settings) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.settings = settings
	return nil
}

func (e *Enphase) Authenticate(ctx context.Context, creds types.Credentials) (types.Credentials, bool, error) {
	if creds.Enphase == nil || creds.Enphase.Username == "" || creds.Enphase.Password == "" {
		// If we have session info but no password, we might be able to restore
		if creds.Enphase != nil && creds.Enphase.SessionID != "" && creds.Enphase.Username != "" {
			// Restore from cache
		} else {
			return creds, false, ErrCredentialsMissing
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	needLogin := creds.Enphase.SessionID == "" || creds.Enphase.ManagerToken == "" || creds.Enphase.SystemID == 0
	if !needLogin && e.username != "" {
		// Check if credentials changed
		needLogin = e.username != creds.Enphase.Username || e.password != creds.Enphase.Password
	}

	var changed bool
	if needLogin {
		log.Ctx(ctx).DebugContext(ctx, "logging in to enphase")
		res, err := e.login(ctx, creds.Enphase.Username, creds.Enphase.Password)
		if err != nil {
			return creds, false, err
		}

		e.username = creds.Enphase.Username
		e.password = creds.Enphase.Password
		e.sessionID = res.SessionID
		e.managerToken = res.ManagerToken
		e.systemID = res.SystemID

		if res.SystemID == 0 {
			// TODO: should we instead search for a system?
			return creds, false, errors.New("no enphase default system found")
		}

		creds.Enphase.SessionID = res.SessionID
		creds.Enphase.ManagerToken = res.ManagerToken
		creds.Enphase.SystemID = res.SystemID
		changed = true
	} else {
		log.Ctx(ctx).DebugContext(ctx, "restoring enphase credentials from cache")
		e.username = creds.Enphase.Username
		e.password = creds.Enphase.Password
		e.sessionID = creds.Enphase.SessionID
		e.managerToken = creds.Enphase.ManagerToken
		e.systemID = creds.Enphase.SystemID
	}

	// Ensure cookies are in the jar
	e.syncCookies()

	// Validate by fetching data.json
	if _, err := e.getDataWithCache(ctx, true); err != nil {
		// If we didn't just login and we got a 401, try to login and retry
		if !needLogin && errors.Is(err, errEnphaseUnauthorized) {
			log.Ctx(ctx).DebugContext(ctx, "enphase session expired, retrying login")
			res, err := e.login(ctx, e.username, e.password)
			if err != nil {
				return creds, false, err
			}

			e.sessionID = res.SessionID
			e.managerToken = res.ManagerToken
			e.systemID = res.SystemID
			e.syncCookies()

			creds.Enphase.SessionID = res.SessionID
			creds.Enphase.ManagerToken = res.ManagerToken
			creds.Enphase.SystemID = res.SystemID
			changed = true

			// Retry fetching data
			_, err = e.getDataWithCache(ctx, true)
			if err != nil {
				return creds, false, fmt.Errorf("credential validation failed after retry: %w", err)
			}
		} else {
			log.Ctx(ctx).WarnContext(ctx, "enphase credential validation failed", slog.Any("error", err))
			return creds, false, fmt.Errorf("credential validation failed: %w", err)
		}
	}

	return creds, changed, nil
}

func (e *Enphase) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	// TODO: implement
	return types.SystemStatus{}, fmt.Errorf("not implemented")
}

func (e *Enphase) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode) error {
	// TODO: implement
	return fmt.Errorf("not implemented")
}

func (e *Enphase) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.DailyEnergyStats, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting enphase energy history", slog.Time("start", start), slog.Time("end", end))
	e.mu.Lock()
	defer e.mu.Unlock()

	// get timezone from data
	data, err := e.getDataWithCache(ctx, false)
	if err != nil {
		return nil, err
	}
	tz := data.App.Timezone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to load enphase timezone, defaulting to UTC", slog.String("tz", tz), slog.Any("error", err))
		loc = time.UTC
	}

	startInLoc := start.In(loc)
	endInLoc := end.In(loc)
	startDay := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(endInLoc.Year(), endInLoc.Month(), endInLoc.Day(), 0, 0, 0, 0, loc)

	lastDayToFetch := endDay
	if nextDay := lastDayToFetch.AddDate(0, 0, 1); !nextDay.After(time.Now()) {
		lastDayToFetch = nextDay
	}

	var result []types.DailyEnergyStats

	for current := startDay; !current.After(lastDayToFetch); current = current.AddDate(0, 0, 1) {
		if current.After(time.Now()) {
			break
		}

		res, err := e.getToday(ctx, current)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get enphase today data", slog.Time("date", current), slog.Any("error", err))
			return nil, err
		}

		if len(res.Stats) == 0 {
			log.Ctx(ctx).WarnContext(ctx, "no stats found for date", slog.Time("date", current))
			continue
		}

		stat := res.Stats[0]

		// let's make sure the time is midnight
		startTime := time.Unix(stat.StartTime, 0).In(loc)
		if !startTime.Equal(current) {
			log.Ctx(ctx).WarnContext(
				ctx,
				"start time is not midnight",
				slog.Time("start_time", startTime),
				slog.Time("current", current),
			)
		}

		hourlyStatsMap := make(map[time.Time]*types.EnergyStats)
		maxLen := len(stat.Consumption)
		if len(stat.Consumption) > maxLen {
			maxLen = len(stat.Consumption)
		}
		if len(stat.GridImport) > maxLen {
			maxLen = len(stat.GridImport)
		}
		if len(stat.Production) > maxLen {
			maxLen = len(stat.Production)
		}

		intervalSecs := stat.IntervalLength
		if intervalSecs == 0 {
			intervalSecs = 900 // default to 15m
		}

		for i := 0; i < maxLen; i++ {
			intervalTime := startTime.Add(time.Duration(i*intervalSecs) * time.Second)
			hourStart := intervalTime.Truncate(time.Hour)

			s, ok := hourlyStatsMap[hourStart]
			if !ok {
				s = &types.EnergyStats{
					TSHourStart: hourStart,
				}
				hourlyStatsMap[hourStart] = s
			}

			getVal := func(arr []int) float64 {
				if i < len(arr) {
					return float64(arr[i]) / 1000.0
				}
				return 0
			}

			prod := getVal(stat.Production)
			solarHome := getVal(stat.SolarHome)
			solarBat := getVal(stat.SolarBattery)
			solarGrid := getVal(stat.SolarGrid)

			gridBat := getVal(stat.GridBattery)
			gridHome := getVal(stat.GridHome)

			batHome := getVal(stat.BatteryHome)
			batGrid := getVal(stat.BatteryGrid)

			s.SolarKWH += solarHome + solarBat + solarGrid
			if s.SolarKWH < prod {
				log.Ctx(ctx).WarnContext(
					ctx,
					"solar kWh is less than production",
					slog.Float64("solarKWH", s.SolarKWH),
					slog.Float64("production", prod),
					slog.Float64("solarHome", solarHome),
					slog.Float64("solarBat", solarBat),
					slog.Float64("solarGrid", solarGrid),
				)
			}

			s.BatteryChargedKWH += solarBat + gridBat
			s.BatteryUsedKWH += batHome + batGrid
			// TODO: should we use import or is that in dollars?
			s.GridImportKWH += gridHome + gridBat
			// TODO: should we use export or is that in dollars?
			s.GridExportKWH += solarGrid + batGrid
			// TODO: should we use consumption?
			s.HomeKWH += solarHome + batHome + gridHome

			s.BatteryToHomeKWH += batHome
			s.BatteryToGridKWH += batGrid
			s.SolarToHomeKWH += solarHome
			s.SolarToBatteryKWH += solarBat
			s.SolarToGridKWH += solarGrid
		}

		var hourly []types.EnergyStats
		for _, s := range hourlyStatsMap {
			hourly = append(hourly, *s)
		}

		result = append(result, types.DailyEnergyStats{
			TSDayStart: current,
			Hourly:     hourly,
		})
	}

	for i := range result {
		sort.Slice(result[i].Hourly, func(j, k int) bool {
			return result[i].Hourly[j].TSHourStart.Before(result[i].Hourly[k].TSHourStart)
		})
	}

	// Filter based on requested start and end bounds
	var filtered []types.DailyEnergyStats
	for _, r := range result {
		if !r.TSDayStart.Before(startDay) && !r.TSDayStart.After(endDay) {
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
}

type enphaseLoginResponse struct {
	Message      string `json:"message"`
	SessionID    string `json:"session_id"`
	ManagerToken string `json:"manager_token"`
	SystemID     int    `json:"system_id"`
}

func (e *Enphase) login(ctx context.Context, username, password string) (enphaseLoginResponse, error) {
	data := url.Values{}
	data.Set("user[email]", username)
	data.Set("user[password]", password)
	data.Set("locale", "en")

	u := e.baseURL.JoinPath("login/login.json")
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return enphaseLoginResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	var res enphaseLoginResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseLoginResponse{}, err
	}

	if res.Message != "success" {
		return enphaseLoginResponse{}, fmt.Errorf("enphase login failed: %s", res.Message)
	}

	return res, nil
}

func (e *Enphase) getDataWithCache(ctx context.Context, force bool) (enphaseDataResult, error) {
	if !force && time.Now().Before(e.dataExpiry) {
		return e.dataCache, nil
	}

	u := e.baseURL.JoinPath(fmt.Sprintf("app-api/%d/data.json", e.systemID))
	q := u.Query()
	q.Set("app", "1")
	q.Set("device_status", "non_retired")
	q.Set("is_mobile", "0")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseDataResult{}, err
	}

	var res enphaseDataResult
	if err := e.doRequest(req, &res); err != nil {
		return enphaseDataResult{}, err
	}

	e.dataCache = res
	e.dataExpiry = time.Now().Add(5 * time.Minute)
	return res, nil
}

type enphaseTodayResponse struct {
	StartDate string `json:"start_date"`
	Stats     []struct {
		Production       []int `json:"production"`
		Consumption      []int `json:"consumption"`
		Import           []int `json:"import"`
		Export           []int `json:"export"`
		GridImport       []int `json:"grid_import"`
		SolarHome        []int `json:"solar_home"`
		SolarBattery     []int `json:"solar_battery"`
		SolarGrid        []int `json:"solar_grid"`
		GeneratorHome    []int `json:"generator_home"`
		GeneratorBattery []int `json:"generator_battery"`
		GeneratorGrid    []int `json:"generator_grid"`
		BatteryHome      []int `json:"battery_home"`
		BatteryGrid      []int `json:"battery_grid"`
		GridBattery      []int `json:"grid_battery"`
		GridHome         []int `json:"grid_home"`
		StartTime        int64 `json:"start_time"`
		IntervalLength   int   `json:"interval_length"`
	} `json:"stats"`
}

func (e *Enphase) getToday(ctx context.Context, date time.Time) (enphaseTodayResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("pv/systems/%d/today", e.systemID))
	q := u.Query()
	q.Set("date", date.Format("2006-01-02"))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseTodayResponse{}, err
	}

	var res enphaseTodayResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseTodayResponse{}, err
	}
	return res, nil
}

func (e *Enphase) doRequest(req *http.Request, dest any) error {
	// set token header if we have it but it will also be in the cookies
	if e.sessionID != "" {
		req.Header.Set("e-auth-token", e.sessionID)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return errEnphaseUnauthorized
		}
		// if this errors there's nothing we can do
		body, err := io.ReadAll(resp.Body)
		if err == nil && len(body) > 256 {
			body = body[:256]
		}
		log.Ctx(req.Context()).WarnContext(
			req.Context(),
			"unexpected enphase response",
			slog.Int("status", resp.StatusCode),
			slog.String("body", string(body)),
		)
		return fmt.Errorf("enphase request failed with status %d", resp.StatusCode)
	}

	if dest != nil {
		var result json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return err
		}

		if !strings.HasSuffix(req.URL.Path, "/login/login.json") {
			log.Ctx(req.Context()).DebugContext(
				req.Context(),
				"enphase result",
				slog.String("url", req.URL.String()),
				slog.String("method", req.Method),
				slog.Any("body", result),
			)
		}

		if err := json.Unmarshal(result, dest); err != nil {
			return fmt.Errorf("failed to decode enphase response: %w", err)
		}
	}

	return nil
}

func (e *Enphase) syncCookies() {
	if e.sessionID == "" || e.client.Jar == nil {
		return
	}
	e.client.Jar.SetCookies(e.baseURL, []*http.Cookie{
		{
			Name:  "_enlighten_4_session",
			Value: e.sessionID,
		},
		{
			Name:  "enlighten_manager_token_production",
			Value: e.managerToken,
		},
	})
}

// Internal structs for data.json
type enphaseDataResult struct {
	App   enphaseApp   `json:"app"`
	State enphaseState `json:"state"`
}

type enphaseApp struct {
	Timezone string        `json:"timezone"`
	Tariff   enphaseTariff `json:"tariff"`
}

type enphaseTariff struct {
	StorageSettings enphaseStorageSettings `json:"storage_settings"`
}

type enphaseStorageSettings struct {
	Mode           string `json:"mode"`
	ReservedSOC    string `json:"reserved_soc"`
	VeryLowSOC     string `json:"very_low_soc"`
	ChargeFromGrid string `json:"charge_from_grid"`
	OptSchedules   string `json:"opt_schedules"`
}

type enphaseState struct {
	SiteID        int                  `json:"siteId"`
	BatteryConfig enphaseBatteryConfig `json:"batteryConfig"`
	Devices       []enphaseDevice      `json:"devices"`
	BatteryInfo   enphaseBatteryInfo   `json:"battery_info"`
	HasBatteries  bool                 `json:"hasBatteries"`
	// TODO: batteryGridMode
}

type enphaseBatteryConfig struct {
	ID                               string                         `json:"_id"`
	ActiveAlertCount                 int                            `json:"active_alert_count"`
	BatteryBackupPercentage          int                            `json:"battery_backup_percentage"`
	ChargeFromGrid                   bool                           `json:"charge_from_grid"`
	ChargeFromGridOnlyScheduleChange bool                           `json:"charge_from_grid_only_schedule_change"`
	ChargeFromGridPending            bool                           `json:"charge_from_grid_pending"`
	ChargeFromGridScheduleEnabled    bool                           `json:"charge_from_grid_schedule_enabled"`
	DrEventActive                    bool                           `json:"dr_event_active"`
	DrEventMode                      string                         `json:"dr_event_mode"`
	GridModeSettings                 map[string]any                 `json:"grid_mode_settings"`
	HideChargeFromGrid               bool                           `json:"hide_charge_from_grid"`
	IsTOU                            bool                           `json:"is_tou"`
	OperationModePvType              string                         `json:"operation_mode_pv_type"`
	OperationModeSubType             string                         `json:"operation_mode_sub_type"`
	PrevBatteryBackupPercentage      enphaseBatteryBackupPercentage `json:"prev_battery_backup_percentage"`
	SevereWeatherWatch               string                         `json:"severe_weather_watch"`
	ShowSevereWeatherAlert           bool                           `json:"show_severe_weather_alert"`
	StormAlertMessage                map[string]any                 `json:"storm_alert_message"`
	Usage                            string                         `json:"usage"`
	VeryLowSoc                       int                            `json:"very_low_soc"`
}

type enphaseBatteryBackupPercentage struct {
	SelfConsumption int `json:"self_consumption"`
	CostSavings     int `json:"cost_savings"`
	BackupOnly      int `json:"backup_only"`
	Expert          int `json:"expert"`
}

type enphaseDevice struct {
	Name         string `json:"name"`
	SerialNumber string `json:"serialNumber"`
	Connected    bool   `json:"connected"`
	Status       string `json:"status"`
}

type enphaseBatteryInfo struct {
	NumberOfBatteries int `json:"no_of_batteries"`
	// TODO: kwh?
	TotalCapacity int `json:"total_capacity"`
}
