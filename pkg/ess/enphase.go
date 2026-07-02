package ess

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
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
	client           *http.Client
	baseURL          *url.URL
	mu               sync.Mutex
	settings         types.Settings
	username         string
	password         string
	sessionID        string
	managerToken     string
	systemID         int
	userID           int
	dataCache        enphaseDataResult
	dataExpiry       time.Time
	lastCSRFToken    map[string]string
	todayCache       enphaseTodayResponse
	todayCacheDate   string
	todayCacheExpiry time.Time
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
		baseURL:       u,
		lastCSRFToken: make(map[string]string),
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
				Stage:    0,
			},
			{
				Field:       "code",
				Name:        "Email Code",
				Type:        types.ESSCredentialFieldTypeString,
				Required:    false,
				Stage:       1,
				Description: "Enter the code sent to your email/phone.",
				OneTime:     true,
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
	if creds.Enphase == nil || creds.Enphase.Username == "" {
		return creds, false, ErrCredentialsMissing
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	needLogin := creds.Enphase.SessionID == "" || creds.Enphase.ManagerToken == "" || creds.Enphase.SystemID == 0
	if !needLogin && e.username != "" {
		// Check if credentials changed
		if e.username != creds.Enphase.Username {
			needLogin = true
		} else if creds.Enphase.Password != "" && e.password != creds.Enphase.Password {
			needLogin = true
		} else if creds.Enphase.Code != "" {
			needLogin = true
		}
	}

	var changed bool
	if needLogin {
		var res enphaseLoginResponse
		var err error

		if creds.Enphase.Code != "" {
			log.Ctx(ctx).DebugContext(ctx, "logging in to enphase with otp code")
			res, err = e.loginWithOTP(ctx, creds.Enphase.Username, creds.Enphase.Code)
			if err != nil {
				return creds, false, err
			}
			// Clear the temporary Code field so we don't store it
			creds.Enphase.Code = ""
		} else if creds.Enphase.Password != "" {
			log.Ctx(ctx).DebugContext(ctx, "logging in to enphase with password")
			res, err = e.login(ctx, creds.Enphase.Username, creds.Enphase.Password)
			if err != nil {
				return creds, false, err
			}
		} else {
			// Trigger OTP generation
			log.Ctx(ctx).DebugContext(ctx, "triggering enphase otp email code generation")
			err = e.generateOTP(ctx, creds.Enphase.Username)
			if err != nil {
				return creds, false, err
			}
			return creds, false, ErrNeedsNextStage
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
		e.userID = creds.Enphase.UserID
	}

	// Ensure cookies are in the jar
	e.syncCookies()

	// Validate by fetching data.json
	if res, err := e.getDataWithCache(ctx, true); err != nil {
		// If we didn't just login and we got a 401, try to login and retry
		if !needLogin && errors.Is(err, errEnphaseUnauthorized) {
			if e.password != "" {
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
				log.Ctx(ctx).WarnContext(ctx, "enphase session expired, cannot retry login without password")
				return creds, false, err
			}
		} else {
			log.Ctx(ctx).WarnContext(ctx, "enphase credential validation failed", slog.Any("error", err))
			return creds, false, fmt.Errorf("credential validation failed: %w", err)
		}
	} else if res.App.UserID != 0 && creds.Enphase.UserID != res.App.UserID {
		creds.Enphase.UserID = res.App.UserID
		changed = true
	}

	return creds, changed, nil
}

func (e *Enphase) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting enphase status")
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := e.getDataWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	batteryStatus, err := e.getBatteryStatus(ctx)
	if err != nil {
		return types.SystemStatus{}, fmt.Errorf("failed to get enphase battery status: %w", err)
	}

	tz := data.App.Timezone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to load location, defaulting to UTC", slog.String("tz", tz), slog.Any("error", err))
		loc = time.UTC
	}

	// Fetch today's stats to estimate live powers and get actual state of charge
	todayStats, err := e.getToday(ctx, time.Now().In(loc))
	if err != nil {
		return types.SystemStatus{}, fmt.Errorf("failed to get enphase today stats: %w", err)
	}

	if todayStats.BatteryDetails == nil {
		return types.SystemStatus{}, errors.New("enphase today stats missing battery details")
	}

	currentSOC := todayStats.BatteryDetails.AggregateSOC

	var solarKW, gridKW, homeKW, batteryKW float64
	if len(todayStats.Stats) > 0 {
		stat := todayStats.Stats[0]
		intervalSecs := stat.IntervalLength
		if intervalSecs == 0 {
			intervalSecs = 900 // 15 minutes
		}

		maxLen := 0
		if len(stat.Production) > maxLen {
			maxLen = len(stat.Production)
		}
		if len(stat.Consumption) > maxLen {
			maxLen = len(stat.Consumption)
		}
		if len(stat.GridImport) > maxLen {
			maxLen = len(stat.GridImport)
		}

		if maxLen > 0 {
			latestIndex := maxLen - 1
			whToKW := func(val float64) float64 {
				return (val / 1000.0) / (float64(intervalSecs) / 3600.0)
			}

			getPowerVal := func(arr []float64) float64 {
				if latestIndex < len(arr) {
					return whToKW(arr[latestIndex])
				}
				return 0
			}

			prod := getPowerVal(stat.Production)
			solarHome := getPowerVal(stat.SolarHome)
			solarBat := getPowerVal(stat.SolarBattery)
			solarGrid := getPowerVal(stat.SolarGrid)

			gridBat := getPowerVal(stat.GridBattery)
			gridHome := getPowerVal(stat.GridHome)

			batHome := getPowerVal(stat.BatteryHome)
			batGrid := getPowerVal(stat.BatteryGrid)

			solarKW = prod
			if solarKW == 0 {
				solarKW = solarHome + solarBat + solarGrid
			}
			batteryKW = batHome + batGrid - gridBat - solarBat
			gridKW = gridHome + gridBat - solarGrid - batGrid
			homeKW = solarHome + batHome + gridHome
			if homeKW == 0 && latestIndex < len(stat.Consumption) {
				homeKW = whToKW(stat.Consumption[latestIndex])
			}
		}
	}

	capacityKWH := batteryStatus.MaxCapacity
	maxPower := batteryStatus.AvailablePower
	emergencyMode := data.State.BatteryConfig.Usage == "backup_only"
	backupReserveSOC := float64(data.State.BatteryConfig.BatteryBackupPercentage)

	var storms []types.Storm
	// If a severe weather alert is active, populate the storms list with a generic description
	// and log the detailed StormAlertMessage object to verify its structure.
	if data.State.BatteryConfig.ShowSevereWeatherAlert {
		log.Ctx(ctx).InfoContext(ctx, "severe weather alert active", slog.Any("stormAlertMessage", data.State.BatteryConfig.StormAlertMessage))
		storms = append(storms, types.Storm{
			Description: "Severe weather alert active",
		})
	}
	if data.State.BatteryConfig.DrEventActive {
		log.Ctx(ctx).InfoContext(ctx, "active demand response (VPP) event", slog.String("drEventMode", data.State.BatteryConfig.DrEventMode))
	}
	var alarms []types.SystemAlarm
	var batteryChargingDisabled bool
	offlineBatteries := 0
	totalBatteries := 0

	// Scan through devices to check connection statuses and reporting states
	for _, dev := range data.State.Devices {
		isBattery := strings.Contains(strings.ToLower(dev.Name), "battery") || strings.Contains(strings.ToLower(dev.Name), "encharge")
		if isBattery {
			totalBatteries++
			if !dev.Connected {
				offlineBatteries++
			}
		}

		if !dev.Connected {
			log.Ctx(ctx).DebugContext(ctx, "device disconnected", slog.Any("device", dev))
		}

		if !dev.Connected || (dev.Status != "" && !strings.EqualFold(dev.Status, "normal") && !strings.EqualFold(dev.Status, "reporting")) {
			code := "DEVICE_ALERT"
			if !dev.Connected {
				code = "DEVICE_OFFLINE"
			}
			alarms = append(alarms, types.SystemAlarm{
				Name:        dev.Name,
				Description: fmt.Sprintf("Device %s is offline or in status: %s", dev.SerialNumber, dev.Status),
				Timestamp:   time.Now().In(loc),
				Code:        code,
			})
		}
	}

	// Disable battery charging if all battery units are reported offline
	// TODO: is there a better way to determine this?
	if totalBatteries > 0 && offlineBatteries == totalBatteries {
		batteryChargingDisabled = true
	}

	status := types.SystemStatus{
		Timestamp:             time.Now().In(loc),
		BatterySOC:            currentSOC,
		BatteryKW:             batteryKW,
		BatteryCapacityKWH:    capacityKWH,
		MaxBatteryDischargeKW: maxPower,
		MaxBatteryChargeKW:    maxPower,
		SolarKW:               solarKW,
		GridKW:                gridKW,
		HomeKW:                homeKW,
		ElevatedMinBatterySOC: backupReserveSOC > 0 && backupReserveSOC > e.settings.MinBatterySOC,
		BatteryAboveMinSOC:    currentSOC >= backupReserveSOC,
		EmergencyMode:         emergencyMode,
		// TODO: determine GridUnavailable
		BatteryChargingDisabled: batteryChargingDisabled,
		Alarms:                  alarms,
		Storms:                  storms,
		VPPActive:               data.State.BatteryConfig.DrEventActive,
	}

	log.Ctx(ctx).DebugContext(ctx, "enphase system status", slog.Any("status", status))
	return status, nil
}

func (e *Enphase) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) error {
	log.Ctx(ctx).DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol), slog.Any("opts", opts))
	e.mu.Lock()
	defer e.mu.Unlock()

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return nil
	}

	data, err := e.getDataWithCache(ctx, false)
	if err != nil {
		return err
	}

	severeWeatherWatchActive := data.State.BatteryConfig.SevereWeatherWatch == "active"
	if severeWeatherWatchActive {
		log.Ctx(ctx).InfoContext(ctx, "device is in storm mode, skipping set modes",
			slog.String("severeWeatherWatch", data.State.BatteryConfig.SevereWeatherWatch),
		)
		return errors.New("device is in storm mode")
	}

	var currentSOC float64
	for _, settings := range data.State.BatteryConfig.EnvStorageSettings {
		currentSOC = settings.SOC
		break
	}

	// Fetch current battery settings (chargeFromGrid, schedule, etc.)
	settingsData, err := e.getBatterySettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get battery settings: %w", err)
	}

	currentChargeFromGrid := settingsData.ChargeFromGrid
	currentChargeFromGridScheduleEnabled := settingsData.ChargeFromGridScheduleEnabled
	currentReserveSOC := float64(settingsData.BatteryBackupPercentage)
	currentProfile := settingsData.Profile

	if settingsData.RequestedConfig != (enphaseRequestedConfig{}) {
		currentChargeFromGrid = settingsData.RequestedConfig.ChargeFromGrid
		currentChargeFromGridScheduleEnabled = settingsData.RequestedConfig.ChargeFromGridScheduleEnabled
		currentReserveSOC = float64(settingsData.RequestedConfig.BatteryBackupPercentage)
		currentProfile = settingsData.RequestedConfig.Profile
	}

	isBackupCurrent := settingsData.Profile == "backup_only"
	isBackupPending := settingsData.RequestedConfig.Profile == "backup_only"
	if isBackupCurrent || isBackupPending {
		log.Ctx(ctx).InfoContext(ctx, "device is in backup mode, skipping set modes")
		return errors.New("device is in backup mode")
	}

	newReserveSOC := currentReserveSOC
	newChargeFromGrid := currentChargeFromGrid
	newProfile := "self-consumption"
	switch bat {
	case types.BatteryModeChargeAny:
		// If they want to charge the battery, set the backup reserve SOC to 100% to
		// force it to charge.
		targetSOC := 100
		if opts.ChargeToSOC != 0 {
			targetSOC = opts.ChargeToSOC
		}
		newReserveSOC = float64(targetSOC)
		newChargeFromGrid = e.settings.GridChargeBatteries
	case types.BatteryModeChargeSolar:
		// Force charging by setting reserve SOC to 100%, but disallow charging
		// from the grid so it only charges using solar power.
		targetSOC := 100
		if opts.ChargeToSOC != 0 {
			targetSOC = opts.ChargeToSOC
		}
		newReserveSOC = float64(targetSOC)
		newChargeFromGrid = false
	case types.BatteryModeLoad:
		// Set the reserve SOC to the configured minimum battery SOC to begin discharging
		// and covering home loads. Allow grid charging if configured.
		newReserveSOC = e.settings.MinBatterySOC
		newChargeFromGrid = e.settings.GridChargeBatteries
	case types.BatteryModeStandby:
		// Set reserve SOC to the floored current SOC to prevent discharging further,
		// without setting it higher which would force a charge. Disallow grid charging.
		newReserveSOC = math.Max(math.Floor(currentSOC), e.settings.MinBatterySOC)
		newChargeFromGrid = false
	case types.BatteryModeNoChange:
		// Keep existing values
	default:
		return fmt.Errorf("unknown battery mode: %v", bat)
	}

	if bat != types.BatteryModeNoChange {
		if newReserveSOC < 10 {
			newReserveSOC = 10
		}
		if newReserveSOC > 100 {
			newReserveSOC = 100
		}
		// if enphase overshot our reserve SOC by less than 1 percent, ignore it
		if math.Abs(newReserveSOC-currentReserveSOC) <= 1.0 {
			newReserveSOC = currentReserveSOC
		}
	}

	updatedSOC := math.Round(newReserveSOC) != math.Round(currentReserveSOC)
	updatedChargeFromGrid := newChargeFromGrid != currentChargeFromGrid
	disableSchedule := currentChargeFromGridScheduleEnabled

	if !updatedSOC && !updatedChargeFromGrid && !disableSchedule {
		log.Ctx(ctx).DebugContext(ctx, "no enphase reserve SOC, charge settings, or schedule updates required")
		return nil
	}

	// Update batterySettings if chargeFromGrid or schedules need to change/disable
	if updatedChargeFromGrid || disableSchedule {
		if e.settings.DryRun {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated enphase battery settings",
				slog.Bool("chargeFromGrid", newChargeFromGrid),
				slog.Bool("chargeFromGridScheduleEnabled", false),
			)
		} else {
			log.Ctx(ctx).InfoContext(ctx, "updating enphase battery settings",
				slog.Bool("chargeFromGrid", newChargeFromGrid),
				slog.Bool("chargeFromGridScheduleEnabled", false),
			)
			payload := enphaseBatterySettingsPayload{
				ChargeFromGrid:                newChargeFromGrid,
				ChargeFromGridScheduleEnabled: false,
			}
			if err := e.updateBatterySettings(ctx, payload); err != nil {
				return err
			}
		}
	}

	// Update batteryProfile if backup reserve SOC needs to change
	if updatedSOC {
		if e.settings.DryRun {
			log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated enphase battery profile",
				slog.String("currentProfile", currentProfile),
				slog.String("newProfile", newProfile),
				slog.Int("batteryBackupPercentage", int(math.Round(newReserveSOC))),
			)
		} else {
			log.Ctx(ctx).InfoContext(ctx, "updating enphase battery profile",
				slog.String("currentProfile", currentProfile),
				slog.String("newProfile", newProfile),
				slog.Int("batteryBackupPercentage", int(math.Round(newReserveSOC))),
			)
			payload := enphaseBatteryProfilePayload{
				Profile:                 newProfile,
				BatteryBackupPercentage: int(math.Round(newReserveSOC)),
			}
			if err := e.updateBatteryProfile(ctx, payload); err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to update enphase battery profile", slog.Any("error", err))
				return err
			}
		}
	}

	e.dataExpiry = time.Time{}
	e.todayCacheExpiry = time.Time{}
	return nil
}

type enphaseRequestedConfig struct {
	ChargeFromGrid                bool   `json:"chargeFromGrid"`
	ChargeFromGridScheduleEnabled bool   `json:"chargeFromGridScheduleEnabled"`
	Profile                       string `json:"profile"`
	BatteryBackupPercentage       int    `json:"batteryBackupPercentage"`
}

func (e *Enphase) getBatteryStatus(ctx context.Context) (enphaseBatteryStatusResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("pv/settings/%d/battery_status.json", e.systemID))
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseBatteryStatusResponse{}, err
	}

	var res enphaseBatteryStatusResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseBatteryStatusResponse{}, err
	}
	return res, nil
}

func (e *Enphase) getBatterySettings(ctx context.Context) (enphaseBatterySettingsResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("service/batteryConfig/api/v1/batterySettings/%d", e.systemID))
	if e.userID != 0 {
		q := u.Query()
		q.Set("userId", fmt.Sprintf("%d", e.userID))
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseBatterySettingsResponse{}, err
	}

	var res struct {
		Data enphaseBatterySettingsResponse `json:"data"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return enphaseBatterySettingsResponse{}, err
	}
	return res.Data, nil
}

type enphaseBatterySettingsPayload struct {
	ChargeFromGrid                bool `json:"chargeFromGrid"`
	ChargeFromGridScheduleEnabled bool `json:"chargeFromGridScheduleEnabled"`
}

func (e *Enphase) updateBatterySettings(ctx context.Context, payload enphaseBatterySettingsPayload) error {
	path := fmt.Sprintf("/service/batteryConfig/api/v1/batterySettings/%d", e.systemID)
	csrfToken := e.lastCSRFToken[path]
	if csrfToken == "" {
		if _, err := e.getBatterySettings(ctx); err != nil {
			return err
		}
		csrfToken = e.lastCSRFToken[path]
		if csrfToken == "" {
			return errors.New("missing CSRF token for battery settings")
		}
	}
	delete(e.lastCSRFToken, path)

	u := e.baseURL.JoinPath(path)
	if e.userID != 0 {
		q := u.Query()
		q.Set("userId", fmt.Sprintf("%d", e.userID))
		u.RawQuery = q.Encode()
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal battery settings payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", u.String(), bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xsrf-Token", csrfToken)

	var res struct {
		Message string `json:"message"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return fmt.Errorf("failed to update battery settings: %w", err)
	}
	if res.Message != "success" && res.Message != "" {
		return fmt.Errorf("battery settings update failed: %s", res.Message)
	}
	return nil
}

func (e *Enphase) getBatteryProfile(ctx context.Context) (enphaseBatteryProfileResponse, error) {
	path := fmt.Sprintf("/service/batteryConfig/api/v1/profile/%d", e.systemID)
	u := e.baseURL.JoinPath(path)
	q := u.Query()
	q.Set("source", "enho")
	q.Set("locale", "en")
	if e.userID != 0 {
		q.Set("userId", fmt.Sprintf("%d", e.userID))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseBatteryProfileResponse{}, err
	}
	var res struct {
		Data enphaseBatteryProfileResponse `json:"data"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return enphaseBatteryProfileResponse{}, err
	}
	return res.Data, nil
}

type enphaseBatteryProfilePayload struct {
	Profile                 string `json:"profile"`
	BatteryBackupPercentage int    `json:"batteryBackupPercentage"`
}

func (e *Enphase) updateBatteryProfile(ctx context.Context, payload enphaseBatteryProfilePayload) error {
	path := fmt.Sprintf("/service/batteryConfig/api/v1/profile/%d", e.systemID)
	csrfToken := e.lastCSRFToken[path]
	if csrfToken == "" {
		if _, err := e.getBatteryProfile(ctx); err != nil {
			return err
		}
		csrfToken = e.lastCSRFToken[path]
		if csrfToken == "" {
			return errors.New("missing CSRF token for battery profile")
		}
	}
	delete(e.lastCSRFToken, path)

	u := e.baseURL.JoinPath(path)
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal battery profile payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", u.String(), bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xsrf-Token", csrfToken)

	var res struct {
		Message string `json:"message"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return fmt.Errorf("failed to update battery profile: %w", err)
	}
	if res.Message != "success" && res.Message != "" {
		return fmt.Errorf("battery profile update failed: %s", res.Message)
	}
	return nil
}

func parseDailyStats(stat enphaseTodayStats, dayStart time.Time, loc *time.Location) types.DailyEnergyStats {
	startTime := time.Unix(stat.StartTime, 0).In(loc)

	hourlyStatsMap := make(map[time.Time]*types.EnergyStats)
	maxLen := len(stat.Consumption)
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

	getVal := func(arr []float64, i int) float64 {
		if i < len(arr) {
			return arr[i] / 1000.0
		}
		return 0
	}

	hourInitializedSOC := make(map[time.Time]bool)
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

		if i < len(stat.SOC) && stat.SOC[i] != nil {
			socVal := *stat.SOC[i]
			if !hourInitializedSOC[hourStart] {
				s.MinBatterySOC = socVal
				s.MaxBatterySOC = socVal
				hourInitializedSOC[hourStart] = true
			} else {
				if socVal < s.MinBatterySOC {
					s.MinBatterySOC = socVal
				}
				if socVal > s.MaxBatterySOC {
					s.MaxBatterySOC = socVal
				}
			}
		}

		solarHome := getVal(stat.SolarHome, i)
		solarBat := getVal(stat.SolarBattery, i)
		solarGrid := getVal(stat.SolarGrid, i)

		gridBat := getVal(stat.GridBattery, i)
		gridHome := getVal(stat.GridHome, i)

		batHome := getVal(stat.BatteryHome, i)
		batGrid := getVal(stat.BatteryGrid, i)

		s.SolarKWH += solarHome + solarBat + solarGrid
		s.BatteryChargedKWH += solarBat + gridBat
		s.BatteryUsedKWH += batHome + batGrid
		s.GridImportKWH += gridHome + gridBat
		s.GridExportKWH += solarGrid + batGrid
		s.HomeKWH += solarHome + batHome + gridHome

		s.BatteryToHomeKWH += batHome
		s.BatteryToGridKWH += batGrid
		s.SolarToHomeKWH += solarHome
		s.SolarToBatteryKWH += solarBat
		s.SolarToGridKWH += solarGrid
	}

	var hourly []types.EnergyStats
	for _, hs := range hourlyStatsMap {
		hourly = append(hourly, *hs)
	}
	// Sort hourly stats
	sort.Slice(hourly, func(i, j int) bool {
		return hourly[i].TSHourStart.Before(hourly[j].TSHourStart)
	})

	return types.DailyEnergyStats{
		TSDayStart: dayStart,
		Hourly:     hourly,
	}
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

	var dailyStats []types.DailyEnergyStats

	todayLocal := time.Now().In(loc)
	todayMidnight := time.Date(todayLocal.Year(), todayLocal.Month(), todayLocal.Day(), 0, 0, 0, 0, loc)
	yesterday := todayMidnight.AddDate(0, 0, -1)

	// 1. Fetch historical range (from startDay up to yesterday, if startDay is not after yesterday)
	if !startDay.After(yesterday) {
		histEndDay := lastDayToFetch
		if histEndDay.After(yesterday) {
			histEndDay = yesterday
		}

		res, err := e.getDailyEnergy(ctx, startDay, histEndDay)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get enphase daily energy data",
				slog.Time("start", startDay),
				slog.Time("end", histEndDay),
				slog.Any("error", err),
			)
			return nil, err
		}

		for _, stat := range res.Stats {
			statStartTime := time.Unix(stat.StartTime, 0).In(loc)
			statDay := time.Date(statStartTime.Year(), statStartTime.Month(), statStartTime.Day(), 0, 0, 0, 0, loc)
			dailyStats = append(dailyStats, parseDailyStats(stat, statDay, loc))
		}
	}

	// 2. Fetch today's data (if lastDayToFetch includes today)
	if !lastDayToFetch.Before(todayMidnight) {
		res, err := e.getToday(ctx, todayMidnight)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get enphase today data",
				slog.Time("date", todayMidnight),
				slog.Any("error", err),
			)
			return nil, err
		}

		if len(res.Stats) > 0 {
			dailyStats = append(dailyStats, parseDailyStats(res.Stats[0], todayMidnight, loc))
		}
	}

	// Sort final dailyStats chronologically by TSDayStart to ensure consistent ordering
	sort.Slice(dailyStats, func(i, j int) bool {
		return dailyStats[i].TSDayStart.Before(dailyStats[j].TSDayStart)
	})

	// Filter based on requested start and end bounds
	var filtered []types.DailyEnergyStats
	for _, r := range dailyStats {
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

func (e *Enphase) generateOTP(ctx context.Context, email string) error {
	data := url.Values{}
	b64Email := base64.StdEncoding.EncodeToString([]byte(email))
	data.Set("email", b64Email)
	data.Set("locale", "en")
	data.Set("source", "ENHO")

	u := e.baseURL.JoinPath("app-api/generate_login_otp.json")
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	var res struct {
		Success   bool `json:"success"`
		IsBlocked bool `json:"isBlocked"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return err
	}

	if !res.Success {
		return fmt.Errorf("enphase otp generation failed (blocked: %v)", res.IsBlocked)
	}

	return nil
}

func (e *Enphase) loginWithOTP(ctx context.Context, email, otp string) (enphaseLoginResponse, error) {
	data := url.Values{}
	b64Email := base64.StdEncoding.EncodeToString([]byte(email))
	b64OTP := base64.StdEncoding.EncodeToString([]byte(otp))
	data.Set("email", b64Email)
	data.Set("otp", b64OTP)
	data.Set("xhrFields[withCredentials]", "true")

	u := e.baseURL.JoinPath("app-api/validate_login_otp.json")
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
		return enphaseLoginResponse{}, fmt.Errorf("enphase otp validation failed: %s", res.Message)
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

func (e *Enphase) getToday(ctx context.Context, date time.Time) (enphaseTodayResponse, error) {
	dateStr := date.Format("2006-01-02")
	if dateStr == e.todayCacheDate && time.Now().Before(e.todayCacheExpiry) {
		return e.todayCache, nil
	}

	u := e.baseURL.JoinPath(fmt.Sprintf("pv/systems/%d/today", e.systemID))
	q := u.Query()
	q.Set("date", dateStr)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseTodayResponse{}, err
	}

	var res enphaseTodayResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseTodayResponse{}, err
	}

	filterFutureIntervals(ctx, &res, time.Now())

	e.todayCache = res
	e.todayCacheDate = dateStr
	e.todayCacheExpiry = time.Now().Add(time.Minute)

	return res, nil
}

func (e *Enphase) getDailyEnergy(ctx context.Context, start, end time.Time) (enphaseTodayResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("pv/systems/%d/daily_energy", e.systemID))
	q := u.Query()
	q.Set("start_date", start.Format("2006-01-02"))
	q.Set("end_date", end.Format("2006-01-02"))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseTodayResponse{}, err
	}

	var res enphaseTodayResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseTodayResponse{}, err
	}

	filterFutureIntervals(ctx, &res, time.Now())

	return res, nil
}

func filterFutureIntervals(ctx context.Context, res *enphaseTodayResponse, now time.Time) {
	for i := range res.Stats {
		stat := &res.Stats[i]
		intervalSecs := stat.IntervalLength
		if intervalSecs == 0 {
			intervalSecs = 900
		}

		lastValidOtherIdx := 0
		lastValidSOCIdx := 0
		maxLen := max(len(stat.Consumption), len(stat.Consumption), len(stat.Production), len(stat.SOC))

		for idx := 0; idx < maxLen; idx++ {
			intervalStartTime := stat.StartTime + int64(idx*intervalSecs)
			if intervalStartTime > now.Unix() {
				break
			}
			if idx < len(stat.SOC) && stat.SOC[idx] != nil {
				lastValidSOCIdx = idx
			} else {
				// check if we have any other data because maybe the SOC is null for some
				// other reason
				if (idx < len(stat.GridImport) && stat.GridImport[idx] > 0) ||
					(idx < len(stat.BatteryHome) && stat.BatteryHome[idx] > 0) ||
					(idx < len(stat.GridHome) && stat.GridHome[idx] > 0) ||
					(idx < len(stat.GridBattery) && stat.GridBattery[idx] > 0) ||
					(idx < len(stat.SolarHome) && stat.SolarHome[idx] > 0) ||
					(idx < len(stat.Production) && stat.Production[idx] > 0) {
					lastValidOtherIdx = idx
				}
			}
		}
		if lastValidOtherIdx > lastValidSOCIdx {
			log.Ctx(ctx).WarnContext(ctx, "enphase: missing soc data for past/present hours",
				slog.Int("lastValidOtherIdx", lastValidOtherIdx),
				slog.Int("lastValidSOCIdx", lastValidSOCIdx),
			)
		}
		validLen := max(lastValidOtherIdx, lastValidSOCIdx) + 1
		truncateSlice := func(slice []float64) []float64 {
			if len(slice) > validLen {
				return slice[:validLen]
			}
			return slice
		}

		stat.Production = truncateSlice(stat.Production)
		stat.Consumption = truncateSlice(stat.Consumption)
		stat.Import = truncateSlice(stat.Import)
		stat.Export = truncateSlice(stat.Export)
		stat.GridImport = truncateSlice(stat.GridImport)
		stat.SolarHome = truncateSlice(stat.SolarHome)
		stat.SolarBattery = truncateSlice(stat.SolarBattery)
		stat.SolarGrid = truncateSlice(stat.SolarGrid)
		stat.GeneratorHome = truncateSlice(stat.GeneratorHome)
		stat.GeneratorBattery = truncateSlice(stat.GeneratorBattery)
		stat.GeneratorGrid = truncateSlice(stat.GeneratorGrid)
		stat.BatteryHome = truncateSlice(stat.BatteryHome)
		stat.BatteryGrid = truncateSlice(stat.BatteryGrid)
		stat.GridBattery = truncateSlice(stat.GridBattery)
		stat.GridHome = truncateSlice(stat.GridHome)

		if len(stat.SOC) > validLen {
			stat.SOC = stat.SOC[:validLen]
		}
	}
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

	csrfToken := resp.Header.Get("X-Csrf-Token")
	if csrfToken != "" {
		if e.lastCSRFToken == nil {
			e.lastCSRFToken = make(map[string]string)
		}
		e.lastCSRFToken[req.URL.Path] = csrfToken
	}

	// If we get redirected to a login page, treat it as unauthorized
	if resp.Request != nil && !strings.Contains(req.URL.Path, "/login") && strings.Contains(resp.Request.URL.Path, "/login") {
		return errEnphaseUnauthorized
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
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

	var result json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if !strings.HasSuffix(req.URL.Path, "/login.json") && !strings.HasSuffix(req.URL.Path, "/generate_login_otp.json") {
		log.Ctx(req.Context()).DebugContext(
			req.Context(),
			"enphase result",
			slog.String("url", req.URL.String()),
			slog.String("method", req.Method),
			slog.Any("body", result),
		)
	}

	if dest != nil {
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
	UserID   int           `json:"userId"`
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
	SiteID                     int                  `json:"siteId"`
	BatteryConfig              enphaseBatteryConfig `json:"batteryConfig"`
	Devices                    []enphaseDevice      `json:"devices"`
	BatteryInfo                enphaseBatteryInfo   `json:"battery_info"`
	HasBatteries               bool                 `json:"hasBatteries"`
	BatteryGridMode            string               `json:"batteryGridMode"`
	IsEncharge5P               bool                 `json:"isEncharge5P"`
	ReducedEnchargeCapacityKWH float64              `json:"reducedEnchargeCapacity"`
}

type enphaseEnvStorageSettings struct {
	SOC  float64 `json:"soc"`
	Mode string  `json:"mode"`
}

type enphaseBatteryConfig struct {
	ID                               string                               `json:"_id"`
	ActiveAlertCount                 int                                  `json:"active_alert_count"`
	BatteryBackupPercentage          int                                  `json:"battery_backup_percentage"`
	ChargeFromGrid                   bool                                 `json:"charge_from_grid"`
	ChargeFromGridOnlyScheduleChange bool                                 `json:"charge_from_grid_only_schedule_change"`
	ChargeFromGridPending            bool                                 `json:"charge_from_grid_pending"`
	ChargeFromGridScheduleEnabled    bool                                 `json:"charge_from_grid_schedule_enabled"`
	DrEventActive                    bool                                 `json:"dr_event_active"`
	DrEventMode                      string                               `json:"dr_event_mode"`
	GridModeSettings                 map[string]any                       `json:"grid_mode_settings"`
	HideChargeFromGrid               bool                                 `json:"hide_charge_from_grid"`
	IsTOU                            bool                                 `json:"is_tou"`
	OperationModePvType              string                               `json:"operation_mode_pv_type"`
	OperationModeSubType             string                               `json:"operation_mode_sub_type"`
	PrevBatteryBackupPercentage      enphaseBatteryBackupPercentage       `json:"prev_battery_backup_percentage"`
	SevereWeatherWatch               string                               `json:"severe_weather_watch"`
	ShowSevereWeatherAlert           bool                                 `json:"show_severe_weather_alert"`
	StormAlertMessage                *enphaseStormAlertMessage            `json:"storm_alert_message"`
	Usage                            string                               `json:"usage"`
	VeryLowSoc                       int                                  `json:"very_low_soc"`
	EnvStorageSettings               map[string]enphaseEnvStorageSettings `json:"env_storage_settings"`
}

type enphaseBatteryBackupPercentage struct {
	SelfConsumption int `json:"self-consumption"`
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
	TotalCapacityWH   int `json:"total_capacity"`
}

type enphaseBatterySchedulesPayload struct {
	Usage                   string `json:"usage"`
	BatteryBackupPercentage int    `json:"battery_backup_percentage"`
	ChargeFromGrid          bool   `json:"charge_from_grid"`
	BatteryGridMode         string `json:"battery_grid_mode,omitempty"`
}

type enphaseStormAlertMessage struct {
	Critical  bool   `json:"critical"`
	AlertName string `json:"alert_name"`
	StartTime any    `json:"start_time"`
	EndTime   any    `json:"end_time"`
}

type enphaseBatterySettingsResponse struct {
	ChargeFromGrid                bool                   `json:"chargeFromGrid"`
	ChargeFromGridScheduleEnabled bool                   `json:"chargeFromGridScheduleEnabled"`
	Profile                       string                 `json:"profile"`
	BatteryBackupPercentage       int                    `json:"batteryBackupPercentage"`
	ChargeBeginTime               int                    `json:"chargeBeginTime"`
	ChargeEndTime                 int                    `json:"chargeEndTime"`
	RequestedConfig               enphaseRequestedConfig `json:"requestedConfig"`
	BatteryBackupPercentageMin    int                    `json:"batteryBackupPercentageMin"`
	BatteryBackupPercentageMax    int                    `json:"batteryBackupPercentageMax"`
	VeryLowSOC                    int                    `json:"veryLowSoc"`
}

type enphaseBatteryProfileResponse struct {
	Profile                    string                 `json:"profile"`
	RequestedConfig            enphaseRequestedConfig `json:"requestedConfig"`
	BatteryBackupPercentage    int                    `json:"batteryBackupPercentage"`
	BatteryBackupPercentageMin int                    `json:"batteryBackupPercentageMin"`
	BatteryBackupPercentageMax int                    `json:"batteryBackupPercentageMax"`
	VeryLowSOC                 int                    `json:"veryLowSoc"`
}

type enphaseTodayStats struct {
	Production       []float64  `json:"production"`
	Consumption      []float64  `json:"consumption"`
	Import           []float64  `json:"import"`
	Export           []float64  `json:"export"`
	GridImport       []float64  `json:"grid_import"`
	SolarHome        []float64  `json:"solar_home"`
	SolarBattery     []float64  `json:"solar_battery"`
	SolarGrid        []float64  `json:"solar_grid"`
	GeneratorHome    []float64  `json:"generator_home"`
	GeneratorBattery []float64  `json:"generator_battery"`
	GeneratorGrid    []float64  `json:"generator_grid"`
	BatteryHome      []float64  `json:"battery_home"`
	BatteryGrid      []float64  `json:"battery_grid"`
	GridBattery      []float64  `json:"grid_battery"`
	GridHome         []float64  `json:"grid_home"`
	SOC              []*float64 `json:"soc"`
	StartTime        int64      `json:"start_time"`
	IntervalLength   int        `json:"interval_length"`
}

type enphaseTodayResponse struct {
	StartDate      string              `json:"start_date"`
	Stats          []enphaseTodayStats `json:"stats"`
	BatteryDetails *struct {
		AggregateSOC float64 `json:"aggregate_soc"`
	} `json:"battery_details"`
}

type enphaseBatteryStatusResponse struct {
	MaxCapacity    float64 `json:"max_capacity"`
	AvailablePower float64 `json:"available_power"`
}
