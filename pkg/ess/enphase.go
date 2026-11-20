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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

var ffEnphaseExportSchedules = feats.Bool("enphaseExportSchedules", false)

// EnphaseExportSchedulesEnabled reports whether Enphase TOU and DTG export schedule management is enabled.
func EnphaseExportSchedulesEnabled(ctx context.Context) bool {
	return ffEnphaseExportSchedules.Enabled(ctx)
}

var errEnphaseUnauthorized = errors.New("unauthorized")

type enphaseCSRFToken struct {
	Token     string
	CreatedAt time.Time
}

const enphasePELCacheTTL = 24 * time.Hour

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
	pelCache         enphasePELDetailsResponse
	pelExpiry        time.Time
	lastCSRFToken    map[string]enphaseCSRFToken
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
			Transport: common.HTTPClient(10 * time.Second).Transport,
			Timeout:   10 * time.Second,
			Jar:       jar,
		},
		baseURL:       u,
		lastCSRFToken: make(map[string]enphaseCSRFToken),
	}
}

func enphaseCSRFKey(path string) string {
	if idx := strings.Index(path, "/schedules"); idx != -1 {
		return path[:idx+len("/schedules")]
	}
	return path
}

func (e *Enphase) getCSRFToken(ctx context.Context, path string, fetchFunc func() error) (string, error) {
	key := enphaseCSRFKey(path)
	tokenObj, ok := e.lastCSRFToken[key]
	if !ok || tokenObj.Token == "" || time.Since(tokenObj.CreatedAt) > 60*time.Second {
		if err := fetchFunc(); err != nil {
			return "", err
		}
		tokenObj = e.lastCSRFToken[key]
		if tokenObj.Token == "" {
			return "", fmt.Errorf("missing CSRF token for path: %s", path)
		}
	}
	return tokenObj.Token, nil
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

	enphaseCreds := *creds.Enphase
	creds.Enphase = &enphaseCreds

	changed, err := e.loginOrRestoreSession(ctx, creds.Enphase)
	if err != nil {
		return creds, changed, err
	}

	// Ensure cookies are in the jar
	e.syncCookies()

	// Validate by fetching data.json
	res, err := e.getDataWithCache(ctx, true)
	if err != nil {
		// If we didn't just login and we got a 401, try to login and retry
		if !changed && errors.Is(err, errEnphaseUnauthorized) {
			retryChanged, retryErr := e.retryExpiredSession(ctx, creds.Enphase)
			if retryChanged {
				changed = true
			}
			return creds, changed, retryErr
		}
		log.Ctx(ctx).WarnContext(ctx, "enphase credential validation failed", slog.Any("error", err))
		return creds, changed, fmt.Errorf("credential validation failed: %w", err)
	}

	if res.App.UserID != 0 {
		e.userID = res.App.UserID
		if creds.Enphase.UserID != res.App.UserID {
			creds.Enphase.UserID = res.App.UserID
			changed = true
		}
	}

	return creds, changed, nil
}

func (e *Enphase) needsLogin(c types.EnphaseCredentials) bool {
	if c.SessionID == "" || c.ManagerToken == "" || c.SystemID == 0 {
		return true
	}
	if e.username == "" {
		return c.Code != ""
	}
	if e.username != c.Username {
		return true
	}
	if c.Password != "" && e.password != c.Password {
		return true
	}
	return c.Code != ""
}

func (e *Enphase) loginOrRestoreSession(ctx context.Context, c *types.EnphaseCredentials) (bool, error) {
	if !e.needsLogin(*c) {
		log.Ctx(ctx).DebugContext(ctx, "restoring enphase credentials from cache")
		if e.systemID != c.SystemID {
			e.pelExpiry = time.Time{}
		}
		e.username = c.Username
		e.password = c.Password
		e.sessionID = c.SessionID
		e.managerToken = c.ManagerToken
		e.systemID = c.SystemID
		e.userID = c.UserID
		return false, nil
	}

	var res enphaseLoginResponse
	var err error
	if c.Code != "" {
		log.Ctx(ctx).DebugContext(ctx, "logging in to enphase with otp code")
		res, err = e.loginWithOTP(ctx, c.Username, c.Code)
		if err != nil {
			return false, err
		}
		// Clear the temporary single-use Code field immediately once redeemed.
		c.Code = ""
	} else if c.Password != "" {
		log.Ctx(ctx).DebugContext(ctx, "logging in to enphase with password")
		res, err = e.login(ctx, c.Username, c.Password)
		if err != nil {
			return false, err
		}
	} else {
		log.Ctx(ctx).DebugContext(ctx, "triggering enphase otp email code generation")
		if err := e.generateOTP(ctx, c.Username); err != nil {
			return false, err
		}
		return false, ErrNeedsNextStage
	}

	if res.SystemID == 0 {
		// TODO: should we instead search for a system?
		return false, errors.New("no enphase default system found")
	}

	if e.systemID != res.SystemID {
		e.pelExpiry = time.Time{}
	}
	e.username = c.Username
	e.password = c.Password
	e.sessionID = res.SessionID
	e.managerToken = res.ManagerToken
	e.systemID = res.SystemID

	c.SessionID = res.SessionID
	c.ManagerToken = res.ManagerToken
	c.SystemID = res.SystemID

	return true, nil
}

func (e *Enphase) retryExpiredSession(ctx context.Context, c *types.EnphaseCredentials) (bool, error) {
	if e.password == "" {
		log.Ctx(ctx).WarnContext(ctx, "enphase session expired, cannot retry login without password")
		return false, errEnphaseUnauthorized
	}

	log.Ctx(ctx).DebugContext(ctx, "enphase session expired, retrying login")
	res, err := e.login(ctx, e.username, e.password)
	if err != nil {
		return false, err
	}
	if res.SystemID == 0 {
		return false, errors.New("no enphase default system found")
	}

	e.sessionID = res.SessionID
	e.managerToken = res.ManagerToken
	e.systemID = res.SystemID
	e.syncCookies()

	c.SessionID = res.SessionID
	c.ManagerToken = res.ManagerToken
	c.SystemID = res.SystemID

	if _, err := e.getDataWithCache(ctx, true); err != nil {
		return true, fmt.Errorf("credential validation failed after retry: %w", err)
	}
	return true, nil
}

// GridSettings returns the grid-related capabilities reported by Enphase.
func (e *Enphase) GridSettings(ctx context.Context) (types.GridSettings, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := e.getDataWithCache(ctx, false)
	if err != nil {
		return types.GridSettings{}, err
	}

	chargeFromGrid := data.State.BatteryConfig.ChargeFromGrid ||
		data.State.BatteryGridMode == "ImportOnly" ||
		data.State.BatteryGridMode == "ImportAndExport"

	exportBatteries := data.State.BatteryGridMode == "ImportAndExport" ||
		data.State.BatteryGridMode == "ExportOnly"

	exportSolar := data.State.BatteryGridMode != "NoImportOrExport"

	// Newer Enphase IQ8 + IQ Meter Collar setups leave batteryGridMode empty/null
	// and expose grid charge and DTG capabilities via cfgControl / dtgControl in batterySettings.
	if data.State.BatteryGridMode == "" {
		if settingsData, err := e.getBatterySettings(ctx); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get enphase battery settings for grid settings", slog.Any("error", err))
		} else {
			if settingsData.ChargeFromGrid || (settingsData.CFGControl != nil && settingsData.CFGControl.Show) {
				chargeFromGrid = true
			}
			// dtgControl.locked is temporarily true during backup_only, ai_optimisation, or StormGuard;
			// only treat locked as a grid restriction outside those temporary profile/storm states.
			tempLocked := settingsData.Profile == "backup_only" ||
				settingsData.Profile == "ai_optimisation" ||
				data.State.BatteryConfig.SevereWeatherWatch == "active"
			if settingsData.DTGControl != nil && settingsData.DTGControl.Show && (!settingsData.DTGControl.Locked || tempLocked) {
				exportBatteries = true
			}
		}
	}

	return types.GridSettings{
		GridChargeBatteries: chargeFromGrid,
		GridExportSolar:     exportSolar,
		GridExportBatteries: exportBatteries,
	}, nil
}

func (e *Enphase) GetStatus(ctx context.Context) (types.SystemStatus, error) {
	log.Ctx(ctx).DebugContext(ctx, "getting enphase status")
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := e.getDataWithCache(ctx, false)
	if err != nil {
		return types.SystemStatus{}, err
	}

	if data.LastReportDate > 0 {
		lastReportTime := time.Unix(data.LastReportDate, 0)
		if time.Since(lastReportTime) > 15*time.Minute {
			log.Ctx(ctx).WarnContext(ctx, "enphase: last report is more than 15 minutes old, power estimation may be inaccurate",
				slog.Time("lastReportTime", lastReportTime),
				slog.Duration("age", time.Since(lastReportTime)),
			)
		}
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

	nowLocal := time.Now().In(loc)

	// Fetch today's stats to estimate live powers and get actual state of charge
	todayStats, err := e.getToday(ctx, nowLocal, true)
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
			for latestIndex >= 0 && hasCorruptEnphaseInterval(ctx, stat, latestIndex, loc) {
				latestIndex--
			}

			if latestIndex >= 0 {
				var latestIntervalDuration float64 = float64(intervalSecs)
				intervalStartTime := stat.StartTime + int64(latestIndex*intervalSecs)
				if data.LastReportDate > intervalStartTime && data.LastReportDate < intervalStartTime+int64(intervalSecs) {
					latestIntervalDuration = float64(data.LastReportDate - intervalStartTime)
					if latestIntervalDuration < 5 {
						latestIntervalDuration = 5
					}
				}

				whToKW := func(val float64) float64 {
					return (val / 1000.0) / (latestIntervalDuration / 3600.0)
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

	usage := data.State.BatteryConfig.Usage
	var dtgEnabled bool
	if ffEnphaseExportSchedules.Enabled(ctx) && !emergencyMode {
		if settingsData, err := e.getBatterySettings(ctx); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to get enphase battery settings for status", slog.Any("error", err))
		} else {
			if settingsData.RequestedConfig.Profile != "" {
				usage = settingsData.RequestedConfig.Profile
			}
			if settingsData.RequestedConfig.DTGControl != nil {
				dtgEnabled = settingsData.RequestedConfig.DTGControl.Enabled
			} else if settingsData.DTGControl != nil {
				dtgEnabled = settingsData.DTGControl.Enabled
			}
		}
	}
	managedTOUMode := !emergencyMode && usage != "ai_optimisation" && (usage == "cost_savings" || dtgEnabled)

	var gridUnavailable bool
	if gridCheck, err := e.getGridControlCheck(ctx); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get enphase grid control check for status", slog.Any("error", err))
	} else {
		log.Ctx(ctx).DebugContext(ctx, "enphase grid control check", slog.Any("gridControlCheck", gridCheck))
		gridUnavailable = gridCheck.GridOutageCheck
	}

	// Populate MaxGridExportKW from Power Export Limiting (get_pel_details), cached for 24h.
	// We intentionally skip fetching MaxGridImportKW for Enphase to avoid extra API calls:
	// US sites omit gridInterconnectionDetails in siteSettings, and pcs-setting only reports
	// main-panel breaker_rating for EVSE dynamic load balancing, where residential panels
	// (even 100A-150A) have far more continuous import headroom than Enphase batteries can
	// draw alongside baseline home load.
	var maxGridExportKW float64
	if pelDetails, err := e.getPELDetailsWithCache(ctx); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get enphase pel details for status", slog.Any("error", err))
	} else {
		maxGridExportKW = resolveEnphasePELExportKW(ctx, pelDetails)
	}

	status := types.SystemStatus{
		Timestamp:               nowLocal,
		TimeLocation:            loc.String(),
		BatterySOC:              currentSOC,
		BatteryKW:               batteryKW,
		BatteryCapacityKWH:      capacityKWH,
		MaxBatteryDischargeKW:   maxPower,
		MaxBatteryChargeKW:      maxPower,
		MaxGridExportKW:         maxGridExportKW,
		SolarKW:                 solarKW,
		GridKW:                  gridKW,
		HomeKW:                  homeKW,
		ElevatedMinBatterySOC:   backupReserveSOC > 0 && backupReserveSOC > e.settings.MinBatterySOC,
		BatteryAboveMinSOC:      currentSOC >= backupReserveSOC,
		EmergencyMode:           emergencyMode,
		ManagedTOUMode:          managedTOUMode,
		GridUnavailable:         gridUnavailable,
		BatteryChargingDisabled: batteryChargingDisabled,
		Alarms:                  alarms,
		Storms:                  storms,
		VPPActive:               data.State.BatteryConfig.DrEventActive,
	}

	log.Ctx(ctx).DebugContext(ctx, "enphase system status", slog.Any("status", status))
	return status, nil
}

type enphaseEffectiveConfig struct {
	chargeFromGrid                bool
	chargeFromGridScheduleEnabled bool
	reserveSOC                    float64
	profile                       string
	dtgEnabled                    bool
	severeWeatherWatchActive      bool
	isBackup                      bool
}

type enphaseModePlan struct {
	newProfile            string
	newReserveSOC         float64
	newChargeFromGrid     bool
	wantDTG               bool
	dtgWindow             *enphaseDTGWindow
	updatedSOC            bool
	updatedProfile        bool
	updatedChargeFromGrid bool
	disableSchedule       bool
}

func resolveEnphaseEffectiveConfig(data enphaseDataResult, settingsData enphaseBatterySettingsResponse) enphaseEffectiveConfig {
	eff := enphaseEffectiveConfig{
		chargeFromGrid:                settingsData.ChargeFromGrid,
		chargeFromGridScheduleEnabled: settingsData.ChargeFromGridScheduleEnabled,
		reserveSOC:                    float64(settingsData.BatteryBackupPercentage),
		profile:                       settingsData.Profile,
		severeWeatherWatchActive:      data.State.BatteryConfig.SevereWeatherWatch == "active",
		isBackup:                      settingsData.Profile == "backup_only" || settingsData.RequestedConfig.Profile == "backup_only",
	}
	if settingsData.DTGControl != nil {
		eff.dtgEnabled = settingsData.DTGControl.Enabled
	}
	if settingsData.RequestedConfig.ChargeFromGrid != nil {
		eff.chargeFromGrid = *settingsData.RequestedConfig.ChargeFromGrid
	}
	if settingsData.RequestedConfig.ChargeFromGridScheduleEnabled != nil {
		eff.chargeFromGridScheduleEnabled = *settingsData.RequestedConfig.ChargeFromGridScheduleEnabled
	}
	if settingsData.RequestedConfig.BatteryBackupPercentage != nil {
		eff.reserveSOC = float64(*settingsData.RequestedConfig.BatteryBackupPercentage)
	}
	if settingsData.RequestedConfig.Profile != "" {
		eff.profile = settingsData.RequestedConfig.Profile
	}
	if settingsData.RequestedConfig.DTGControl != nil {
		eff.dtgEnabled = settingsData.RequestedConfig.DTGControl.Enabled
	}
	return eff
}

func resolveEnphaseTimezone(tz string) (string, *time.Location) {
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return tz, loc
}

func (e *Enphase) SetModes(ctx context.Context, bat types.BatteryMode, sol types.SolarMode, opts types.ModesOptions) (bool, error) {
	log.Ctx(ctx).DebugContext(ctx, "SetModes called", slog.Any("batteryMode", bat), slog.Any("solarMode", sol), slog.Any("opts", opts))
	e.mu.Lock()
	defer e.mu.Unlock()

	if bat == types.BatteryModeNoChange && sol == types.SolarModeNoChange {
		return false, nil
	}

	data, err := e.getDataWithCache(ctx, false)
	if err != nil {
		return false, err
	}

	tz, loc := resolveEnphaseTimezone(data.App.Timezone)
	now := time.Now().In(loc)

	todayStatsRaw, err := e.getToday(ctx, now, true)
	if err != nil {
		return false, fmt.Errorf("failed to get enphase today stats in SetModes: %w", err)
	}
	if todayStatsRaw.BatteryDetails == nil {
		return false, errors.New("enphase today stats missing battery details in SetModes")
	}

	// Fetch current battery settings (chargeFromGrid, schedule, etc.)
	settingsData, err := e.getBatterySettings(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get battery settings: %w", err)
	}

	eff := resolveEnphaseEffectiveConfig(data, settingsData)
	if eff.severeWeatherWatchActive || eff.isBackup {
		return e.handleEnphaseStormOrBackup(ctx, bat, eff)
	}

	enphaseSchedules := ffEnphaseExportSchedules.Enabled(ctx)
	manageSchedules := enphaseSchedules && e.settings.ManageTOUSchedules
	changed, tariffHasPeakNow := e.syncEnphaseExportTariffIfNeeded(ctx, sol, opts, now, settingsData.IsBatteryChangePending, manageSchedules)

	plan, err := e.computeEnphaseModePlan(ctx, bat, sol, opts, now, todayStatsRaw.BatteryDetails.AggregateSOC, settingsData, eff, enphaseSchedules, manageSchedules, tariffHasPeakNow)
	if err != nil {
		return false, err
	}

	return e.applyEnphaseModeUpdates(ctx, tz, loc, eff, plan, enphaseSchedules, changed)
}

func (e *Enphase) handleEnphaseStormOrBackup(ctx context.Context, bat types.BatteryMode, eff enphaseEffectiveConfig) (bool, error) {
	if bat == types.BatteryModeNoChange {
		return false, nil
	}
	var targetChargeFromGrid bool
	switch bat {
	case types.BatteryModeChargeAny, types.BatteryModeLoad, types.BatteryModeStandby, types.BatteryModeExport:
		targetChargeFromGrid = e.settings.GridChargeBatteries
	default:
		return false, fmt.Errorf("unknown battery mode: %v", bat)
	}

	updatedChargeFromGrid := targetChargeFromGrid != eff.chargeFromGrid
	disableSchedule := eff.chargeFromGridScheduleEnabled
	if !updatedChargeFromGrid && !disableSchedule {
		log.Ctx(ctx).DebugContext(ctx, "grid charge mode already set correctly in storm/backup mode",
			slog.Bool("chargeFromGrid", targetChargeFromGrid),
		)
		return false, nil
	}

	if e.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated enphase battery settings in storm/backup mode",
			slog.Bool("chargeFromGrid", targetChargeFromGrid),
			slog.Bool("chargeFromGridScheduleEnabled", false),
			slog.Bool("severeWeatherWatchActive", eff.severeWeatherWatchActive),
			slog.Bool("isBackup", eff.isBackup),
		)
		return false, nil
	}

	log.Ctx(ctx).InfoContext(ctx, "updating enphase battery settings in storm/backup mode",
		slog.Bool("chargeFromGrid", targetChargeFromGrid),
		slog.Bool("chargeFromGridScheduleEnabled", false),
		slog.Bool("severeWeatherWatchActive", eff.severeWeatherWatchActive),
		slog.Bool("isBackup", eff.isBackup),
	)
	cfgScheduleDisabled := false
	payload := enphaseBatterySettingsPayload{
		ChargeFromGrid:                &targetChargeFromGrid,
		ChargeFromGridScheduleEnabled: &cfgScheduleDisabled,
	}
	if err := e.updateBatterySettings(ctx, payload); err != nil {
		return false, err
	}
	return true, nil
}

func (e *Enphase) syncEnphaseExportTariffIfNeeded(
	ctx context.Context,
	sol types.SolarMode,
	opts types.ModesOptions,
	now time.Time,
	isBatteryChangePending bool,
	manageSchedules bool,
) (bool, bool) {
	// Sync TOU tariff before modifying battery profile or settings so tariff-ms
	// does not reject edits with error 934 ("active profile request pending").
	if !manageSchedules || (opts.Schedule == nil && sol != types.SolarModeExport) {
		return false, false
	}
	tariffChanged, hasPeakNow, err := e.ensureTOUTariffSynced(ctx, opts.Schedule, now, sol == types.SolarModeExport, isBatteryChangePending)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to check/sync enphase TOU tariff", slog.Any("error", err))
		return false, false
	}
	return tariffChanged, hasPeakNow
}

func (e *Enphase) computeEnphaseModePlan(
	ctx context.Context,
	bat types.BatteryMode,
	sol types.SolarMode,
	opts types.ModesOptions,
	now time.Time,
	currentSOC float64,
	settingsData enphaseBatterySettingsResponse,
	eff enphaseEffectiveConfig,
	enphaseSchedules bool,
	manageSchedules bool,
	tariffHasPeakNow bool,
) (enphaseModePlan, error) {
	minAllowedSOC := max(float64(settingsData.BatteryBackupPercentageMin), float64(settingsData.VeryLowSOC))
	if minAllowedSOC <= 0 {
		minAllowedSOC = 10
	}

	newChargeFromGrid := e.computeEnphaseChargeFromGrid(ctx, bat, eff.chargeFromGrid, enphaseSchedules)
	newProfile := computeEnphaseTargetProfile(ctx, sol, manageSchedules, tariffHasPeakNow)

	newReserveSOC, err := e.computeEnphaseTargetReserveSOC(bat, opts, currentSOC, eff.reserveSOC, minAllowedSOC)
	if err != nil {
		return enphaseModePlan{}, err
	}

	wantDTG, dtgWindow := computeEnphaseDTGTarget(ctx, bat, opts, now, newReserveSOC, minAllowedSOC, settingsData.DTGControl, eff.dtgEnabled, enphaseSchedules, manageSchedules)

	plan := enphaseModePlan{
		newProfile:            newProfile,
		newReserveSOC:         newReserveSOC,
		newChargeFromGrid:     newChargeFromGrid,
		wantDTG:               wantDTG,
		dtgWindow:             dtgWindow,
		updatedSOC:            math.Round(newReserveSOC) != math.Round(eff.reserveSOC),
		updatedProfile:        (bat != types.BatteryModeNoChange || (manageSchedules && sol != types.SolarModeNoChange)) && eff.profile != "" && eff.profile != newProfile,
		updatedChargeFromGrid: newChargeFromGrid != eff.chargeFromGrid,
		disableSchedule:       eff.chargeFromGridScheduleEnabled,
	}

	log.Ctx(ctx).DebugContext(ctx, "computed enphase SetModes target",
		slog.Bool("enphaseSchedules", enphaseSchedules),
		slog.Bool("manageSchedules", manageSchedules),
		slog.String("currentProfile", eff.profile),
		slog.String("newProfile", plan.newProfile),
		slog.Float64("currentReserveSOC", eff.reserveSOC),
		slog.Float64("newReserveSOC", plan.newReserveSOC),
		slog.Bool("currentChargeFromGrid", eff.chargeFromGrid),
		slog.Bool("newChargeFromGrid", plan.newChargeFromGrid),
		slog.Bool("currentDTGEnabled", eff.dtgEnabled),
		slog.Bool("wantDTG", plan.wantDTG),
		slog.Bool("tariffHasPeakNow", tariffHasPeakNow),
	)
	return plan, nil
}

func (e *Enphase) computeEnphaseChargeFromGrid(ctx context.Context, bat types.BatteryMode, currentChargeFromGrid, enphaseSchedules bool) bool {
	if enphaseSchedules {
		if bat != types.BatteryModeNoChange {
			return bat == types.BatteryModeChargeAny && e.settings.GridChargeBatteries
		}
		return currentChargeFromGrid
	}
	if e.settings.GridChargeBatteries && !currentChargeFromGrid {
		log.Ctx(ctx).InfoContext(ctx, "grid charging is disabled on enphase, enabling grid charge to match site settings")
		return true
	}
	return currentChargeFromGrid
}

func computeEnphaseTargetProfile(ctx context.Context, sol types.SolarMode, manageSchedules, tariffHasPeakNow bool) string {
	if manageSchedules && sol == types.SolarModeExport {
		if tariffHasPeakNow {
			return "cost_savings"
		}
		log.Ctx(ctx).WarnContext(ctx, "skipping enphase cost_savings profile because active tariff does not cover current hour as peak")
	}
	return "self-consumption"
}

func (e *Enphase) computeEnphaseTargetReserveSOC(
	bat types.BatteryMode,
	opts types.ModesOptions,
	currentSOC float64,
	currentReserveSOC float64,
	minAllowedSOC float64,
) (float64, error) {
	minSOC := e.settings.MinBatterySOC
	if opts.MinimumSOC != 0 {
		minSOC = float64(opts.MinimumSOC)
	}

	var newReserveSOC float64
	switch bat {
	case types.BatteryModeChargeAny:
		// If they want to charge the battery, set the backup reserve SOC to 100% to
		// force it to charge.
		targetSOC := 100
		if opts.ChargeToSOC != 0 {
			targetSOC = opts.ChargeToSOC
		}
		newReserveSOC = float64(targetSOC)
	case types.BatteryModeLoad, types.BatteryModeExport:
		// Set the reserve SOC to the configured minimum battery SOC to begin discharging
		// and covering home loads (or exporting during DTG window).
		newReserveSOC = minSOC
	case types.BatteryModeStandby:
		// Set reserve SOC to the floored current SOC to prevent discharging further,
		// without setting it higher which would force a charge.
		newReserveSOC = max(math.Floor(currentSOC), minSOC)
	case types.BatteryModeNoChange:
		// Keep existing values
		return currentReserveSOC, nil
	default:
		return 0, fmt.Errorf("unknown battery mode: %v", bat)
	}

	newReserveSOC = min(max(newReserveSOC, minAllowedSOC), 100)
	// if enphase overshot our reserve SOC by less than 1 percent, ignore it
	if math.Abs(newReserveSOC-currentReserveSOC) <= 1.0 {
		newReserveSOC = currentReserveSOC
	}
	return newReserveSOC, nil
}

func computeEnphaseDTGTarget(
	ctx context.Context,
	bat types.BatteryMode,
	opts types.ModesOptions,
	now time.Time,
	newReserveSOC float64,
	minAllowedSOC float64,
	dtgControl *enphaseControlStatus,
	currentDTGEnabled bool,
	enphaseSchedules bool,
	manageSchedules bool,
) (bool, *enphaseDTGWindow) {
	if !enphaseSchedules {
		return false, nil
	}
	if manageSchedules && bat == types.BatteryModeExport {
		dtgSupported := dtgControl != nil && dtgControl.Show && !dtgControl.Locked && dtgControl.ScheduleSupported
		if !dtgSupported {
			log.Ctx(ctx).WarnContext(ctx, "enphase DTG battery export requested but dtgControl is unavailable, locked, or schedule unsupported",
				slog.Any("dtgControl", dtgControl),
			)
			return false, nil
		}
		dtgLimit := max(int(math.Round(newReserveSOC)), int(math.Round(minAllowedSOC)))
		w := computeEnphaseDTGWindow(opts.Schedule, now, dtgLimit)
		return true, &w
	}
	if bat == types.BatteryModeNoChange {
		return currentDTGEnabled, nil
	}
	return false, nil
}

func (e *Enphase) applyEnphaseModeUpdates(
	ctx context.Context,
	tz string,
	loc *time.Location,
	eff enphaseEffectiveConfig,
	plan enphaseModePlan,
	enphaseSchedules bool,
	changed bool,
) (bool, error) {
	// Update batteryProfile first so lowering batteryBackupPercentage takes effect before DTG schedule validation.
	if plan.updatedSOC || plan.updatedProfile {
		profileChanged, err := e.applyEnphaseProfileUpdate(ctx, eff.profile, plan.newProfile, int(math.Round(plan.newReserveSOC)))
		if err != nil {
			return changed, err
		}
		if profileChanged {
			changed = true
		}
	}

	var dtgScheduleChanged bool
	if plan.wantDTG && plan.dtgWindow != nil {
		dtgChanged, err := e.ensureDTGSchedule(ctx, tz, loc, *plan.dtgWindow)
		if err != nil {
			return changed, fmt.Errorf("failed to configure enphase DTG schedule: %w", err)
		}
		dtgScheduleChanged = dtgChanged
		if dtgScheduleChanged {
			changed = true
		}
	}

	updatedDTG := enphaseSchedules && (plan.wantDTG != eff.dtgEnabled || (plan.wantDTG && dtgScheduleChanged))
	if plan.updatedChargeFromGrid || plan.disableSchedule || updatedDTG {
		settingsChanged, err := e.applyEnphaseSettingsUpdate(ctx, eff, plan, enphaseSchedules, updatedDTG)
		if err != nil {
			return changed, err
		}
		if settingsChanged {
			changed = true
		}
	}

	e.dataExpiry = time.Time{}
	e.todayCacheExpiry = time.Time{}
	return changed, nil
}

func (e *Enphase) applyEnphaseProfileUpdate(ctx context.Context, currentProfile, newProfile string, backupPercent int) (bool, error) {
	if e.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated enphase battery profile",
			slog.String("currentProfile", currentProfile),
			slog.String("newProfile", newProfile),
			slog.Int("batteryBackupPercentage", backupPercent),
		)
		return false, nil
	}

	log.Ctx(ctx).InfoContext(ctx, "updating enphase battery profile",
		slog.String("currentProfile", currentProfile),
		slog.String("newProfile", newProfile),
		slog.Int("batteryBackupPercentage", backupPercent),
	)
	payload := enphaseBatteryProfilePayload{
		Profile:                 newProfile,
		BatteryBackupPercentage: backupPercent,
	}
	if err := e.updateBatteryProfile(ctx, payload); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update enphase battery profile", slog.Any("error", err))
		return false, err
	}
	return true, nil
}

func (e *Enphase) applyEnphaseSettingsUpdate(
	ctx context.Context,
	eff enphaseEffectiveConfig,
	plan enphaseModePlan,
	enphaseSchedules bool,
	updatedDTG bool,
) (bool, error) {
	var payload enphaseBatterySettingsPayload
	if plan.updatedChargeFromGrid || plan.disableSchedule {
		cfgVal := plan.newChargeFromGrid
		schedDisabled := false
		payload.ChargeFromGrid = &cfgVal
		payload.ChargeFromGridScheduleEnabled = &schedDisabled
	}
	if enphaseSchedules && (plan.wantDTG || eff.dtgEnabled) && updatedDTG {
		payload.DTGControl = &enphaseControlToggle{Enabled: plan.wantDTG}
	}

	if e.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated enphase battery settings",
			slog.Bool("chargeFromGrid", plan.newChargeFromGrid),
			slog.Bool("chargeFromGridScheduleEnabled", false),
			slog.Bool("dtgEnabled", plan.wantDTG),
		)
		return false, nil
	}

	if payload.ChargeFromGrid == nil && payload.ChargeFromGridScheduleEnabled == nil && payload.DTGControl == nil {
		return false, nil
	}

	log.Ctx(ctx).InfoContext(ctx, "updating enphase battery settings",
		slog.Bool("chargeFromGrid", plan.newChargeFromGrid),
		slog.Bool("chargeFromGridScheduleEnabled", false),
		slog.Bool("dtgEnabled", plan.wantDTG),
	)
	if err := e.updateBatterySettings(ctx, payload); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to set enphase battery settings", slog.Any("error", err))
		return false, err
	}
	return true, nil
}

type enphaseControlStatus struct {
	Show                   bool   `json:"show"`
	Enabled                bool   `json:"enabled"`
	Disabled               bool   `json:"disabled,omitempty"`
	Locked                 bool   `json:"locked"`
	ScheduleSupported      bool   `json:"scheduleSupported,omitempty"`
	ShowDaySchedule        bool   `json:"showDaySchedule,omitempty"`
	StartTime              int    `json:"startTime,omitempty"`
	EndTime                int    `json:"endTime,omitempty"`
	DischargeUpTo          int    `json:"dischargeUpTo,omitempty"`
	ShowDayScheduleMessage string `json:"showDayScheduleMessage,omitempty"`
}

type enphaseControlToggle struct {
	Enabled bool `json:"enabled"`
}

type enphaseRequestedConfig struct {
	ChargeFromGrid                *bool                 `json:"chargeFromGrid,omitempty"`
	ChargeFromGridScheduleEnabled *bool                 `json:"chargeFromGridScheduleEnabled,omitempty"`
	Profile                       string                `json:"profile,omitempty"`
	BatteryBackupPercentage       *int                  `json:"batteryBackupPercentage,omitempty"`
	DTGControl                    *enphaseControlToggle `json:"dtgControl,omitempty"`
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

type enphaseGridControlCheckResponse struct {
	GridOutageCheck           bool `json:"gridOutageCheck"`
	UserInitiatedGridToggle   bool `json:"userInitiatedGridToggle"`
	DisableGridControl        bool `json:"disableGridControl"`
	SunlightBackupSystemCheck bool `json:"sunlightBackupSystemCheck"`
}

func (e *Enphase) getGridControlCheck(ctx context.Context) (enphaseGridControlCheckResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("app-api/%d/grid_control_check.json", e.systemID))
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseGridControlCheckResponse{}, err
	}

	var res enphaseGridControlCheckResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseGridControlCheckResponse{}, err
	}
	return res, nil
}

type enphasePELGatewayLimit struct {
	SerialNumber string `json:"serial_number"`
	ExportValue  any    `json:"export_value"`
}

type enphasePELDetailsResponse struct {
	PELType     string          `json:"pel_type"`
	ExportLimit json.RawMessage `json:"export_limit"`
}

func (e *Enphase) getPELDetailsWithCache(ctx context.Context) (enphasePELDetailsResponse, error) {
	if time.Now().Before(e.pelExpiry) {
		return e.pelCache, nil
	}
	res, err := e.getPELDetails(ctx)
	if err != nil {
		return enphasePELDetailsResponse{}, err
	}
	e.pelCache = res
	e.pelExpiry = time.Now().Add(enphasePELCacheTTL)
	return res, nil
}

func (e *Enphase) getPELDetails(ctx context.Context) (enphasePELDetailsResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("app-api/%d/get_pel_details", e.systemID))
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphasePELDetailsResponse{}, err
	}
	if e.managerToken != "" {
		req.Header.Set("Authorization", e.managerToken)
	}

	var res enphasePELDetailsResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphasePELDetailsResponse{}, err
	}
	return res, nil
}

func resolveEnphasePELExportKW(ctx context.Context, res enphasePELDetailsResponse) float64 {
	pelType := strings.TrimSpace(res.PELType)
	raw := bytes.TrimSpace(res.ExportLimit)
	if strings.EqualFold(pelType, "Not Configured") || len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0
	}
	if strings.EqualFold(pelType, "24-Hour Schedule") {
		log.Ctx(ctx).WarnContext(ctx,
			"enphase 24-hour scheduled pel is not supported, ignoring export limit",
			slog.String("pelType", pelType),
			slog.String("exportLimit", string(raw)),
		)
		return 0
	}

	parseOrWarn := func(val any) float64 {
		if val == nil {
			return 0
		}
		if s, ok := val.(string); ok {
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "", "n/a", "not available", "not configured":
				return 0
			}
		}
		kw, ok := parseEnphasePELPowerKW(val)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx,
				"unexpected enphase pel export_limit format",
				slog.String("pelType", pelType),
				slog.Any("exportValue", val),
				slog.String("raw", string(raw)),
			)
			return 0
		}
		return kw
	}

	switch raw[0] {
	case '[':
		var gateways []enphasePELGatewayLimit
		if err := json.Unmarshal(raw, &gateways); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to decode enphase pel gateway array", slog.Any("error", err), slog.String("raw", string(raw)))
			return 0
		}
		var totalKW float64
		for _, gw := range gateways {
			totalKW += parseOrWarn(gw.ExportValue)
		}
		return totalKW

	case '{':
		var gw enphasePELGatewayLimit
		if err := json.Unmarshal(raw, &gw); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to decode enphase pel gateway object", slog.Any("error", err), slog.String("raw", string(raw)))
			return 0
		}
		return parseOrWarn(gw.ExportValue)

	default:
		var scalar any
		if err := json.Unmarshal(raw, &scalar); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to decode enphase pel scalar", slog.Any("error", err), slog.String("raw", string(raw)))
			return 0
		}
		return parseOrWarn(scalar)
	}
}

func parseEnphasePELPowerKW(v any) (float64, bool) {
	val, ok := v.(string)
	if !ok {
		return 0, false
	}
	lower := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(val, ",", "")))
	var multiplier float64
	switch {
	case strings.HasSuffix(lower, "kw"):
		multiplier = 1.0
		lower = strings.TrimSpace(strings.TrimSuffix(lower, "kw"))
	case strings.HasSuffix(lower, "w"):
		multiplier = 0.001
		lower = strings.TrimSpace(strings.TrimSuffix(lower, "w"))
	default:
		return 0, false
	}
	num, err := strconv.ParseFloat(lower, 64)
	if err != nil || math.IsNaN(num) || math.IsInf(num, 0) || num <= 0 {
		return 0, false
	}
	return num * multiplier, true
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
	ChargeFromGrid                *bool                 `json:"chargeFromGrid,omitempty"`
	ChargeFromGridScheduleEnabled *bool                 `json:"chargeFromGridScheduleEnabled,omitempty"`
	DTGControl                    *enphaseControlToggle `json:"dtgControl,omitempty"`
}

func (e *Enphase) updateBatterySettings(ctx context.Context, payload enphaseBatterySettingsPayload) error {
	path := fmt.Sprintf("/service/batteryConfig/api/v1/batterySettings/%d", e.systemID)
	csrfToken, err := e.getCSRFToken(ctx, path, func() error {
		_, err := e.getBatterySettings(ctx)
		return err
	})
	if err != nil {
		return err
	}

	u := e.baseURL.JoinPath(path)
	q := u.Query()
	if e.userID != 0 {
		q.Set("userId", fmt.Sprintf("%d", e.userID))
	}
	q.Set("source", "enho")
	u.RawQuery = q.Encode()
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
	if res.Message != "success" && res.Message != "" && !strings.EqualFold(res.Message, "changes already activated!") {
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
	csrfToken, err := e.getCSRFToken(ctx, path, func() error {
		_, err := e.getBatteryProfile(ctx)
		return err
	})
	if err != nil {
		return err
	}

	u := e.baseURL.JoinPath(path)
	if e.userID != 0 {
		q := u.Query()
		q.Set("userId", fmt.Sprintf("%d", e.userID))
		u.RawQuery = q.Encode()
	}
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
	if res.Message != "success" && res.Message != "" && !strings.EqualFold(res.Message, "changes already activated!") {
		return fmt.Errorf("battery profile update failed: %s", res.Message)
	}
	return nil
}

const (
	enphaseMinScheduleMinutes = 15
	enphaseMaxPeakPeriods     = 5
)

func (e *Enphase) getSchedulesCSRFToken(ctx context.Context) (string, error) {
	path := fmt.Sprintf("/service/batteryConfig/api/v1/battery/sites/%d/schedules", e.systemID)
	return e.getCSRFToken(ctx, path, func() error {
		_, err := e.getBatterySchedules(ctx)
		return err
	})
}

func (e *Enphase) getBatterySchedules(ctx context.Context) (enphaseBatterySchedulesResponse, error) {
	path := fmt.Sprintf("/service/batteryConfig/api/v1/battery/sites/%d/schedules", e.systemID)
	u := e.baseURL.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseBatterySchedulesResponse{}, err
	}
	var res enphaseBatterySchedulesResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseBatterySchedulesResponse{}, err
	}
	return res, nil
}

func (e *Enphase) createBatterySchedule(ctx context.Context, payload enphaseBatteryScheduleDetail) (string, error) {
	csrfToken, err := e.getSchedulesCSRFToken(ctx)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/service/batteryConfig/api/v1/battery/sites/%d/schedules", e.systemID)
	u := e.baseURL.JoinPath(path)
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal schedule payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(jsonBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xsrf-Token", csrfToken)
	var res struct {
		ScheduleID string `json:"scheduleId"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return "", fmt.Errorf("failed to create enphase schedule: %w", err)
	}
	return res.ScheduleID, nil
}

func (e *Enphase) updateBatterySchedule(ctx context.Context, scheduleID string, payload enphaseBatteryScheduleDetail) error {
	csrfToken, err := e.getSchedulesCSRFToken(ctx)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/service/batteryConfig/api/v1/battery/sites/%d/schedules/%s", e.systemID, scheduleID)
	u := e.baseURL.JoinPath(path)
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal schedule payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", u.String(), bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xsrf-Token", csrfToken)
	var res enphaseBatteryScheduleDetail
	if err := e.doRequest(req, &res); err != nil {
		return fmt.Errorf("failed to update enphase schedule: %w", err)
	}
	return nil
}

func (e *Enphase) deleteBatterySchedule(ctx context.Context, scheduleID string) error {
	csrfToken, err := e.getSchedulesCSRFToken(ctx)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/service/batteryConfig/api/v1/battery/sites/%d/schedules/%s/delete", e.systemID, scheduleID)
	u := e.baseURL.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xsrf-Token", csrfToken)
	if err := e.doRequest(req, nil); err != nil {
		return fmt.Errorf("failed to delete enphase schedule: %w", err)
	}
	return nil
}

func (e *Enphase) validateBatterySchedule(ctx context.Context, scheduleType string) (bool, error) {
	csrfToken, err := e.getSchedulesCSRFToken(ctx)
	if err != nil {
		return false, err
	}
	path := fmt.Sprintf("/service/batteryConfig/api/v1/battery/sites/%d/schedules/isValid", e.systemID)
	u := e.baseURL.JoinPath(path)
	jsonBytes, err := json.Marshal(map[string]string{"scheduleType": scheduleType})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(jsonBytes))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xsrf-Token", csrfToken)
	var res struct {
		IsValid bool `json:"isValid"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return false, fmt.Errorf("failed to validate enphase schedule: %w", err)
	}
	return res.IsValid, nil
}

type enphaseDTGWindow struct {
	StartMin int
	EndMin   int
	Limit    int
}

func computeEnphaseDTGWindow(sched *types.TOUSchedule, now time.Time, limit int) enphaseDTGWindow {
	nowMin := now.Hour()*60 + now.Minute()
	startMin := -1
	endMin := -1

	if sched != nil {
		var exportWindows []enphaseMinuteRange
		for _, p := range sched.Periods {
			if p.BatteryMode != types.BatteryModeExport {
				continue
			}
			sMin := p.StartHour*60 + p.StartMinute
			eMin := p.EndHour*60 + p.EndMinute
			if eMin > sMin {
				exportWindows = append(exportWindows, enphaseMinuteRange{StartMin: sMin, EndMin: min(eMin, 1440)})
			}
		}
		merged := mergeEnphaseMinuteRanges(exportWindows)
		for _, w := range merged {
			if w.StartMin <= nowMin && nowMin < w.EndMin {
				startMin = w.StartMin
				endMin = w.EndMin
				break
			}
		}
	}

	if startMin < 0 || endMin <= startMin {
		startMin = now.Hour() * 60
		endMin = min(startMin+60, 1439)
	}
	if endMin >= 1440 {
		endMin = 1439
	}
	if endMin-startMin < enphaseMinScheduleMinutes {
		startMin = max(0, endMin-enphaseMinScheduleMinutes)
	}

	return enphaseDTGWindow{
		StartMin: startMin,
		EndMin:   endMin,
		Limit:    limit,
	}
}

func isDTGScheduleMatch(existing enphaseBatteryScheduleDetail, target enphaseBatteryScheduleDetail) bool {
	if existing.IsDeleted || !existing.IsEnabled {
		return false
	}
	if existing.Timezone != target.Timezone ||
		existing.StartTime != target.StartTime ||
		existing.EndTime != target.EndTime ||
		existing.Limit != target.Limit {
		return false
	}
	if len(existing.Days) != len(target.Days) {
		return false
	}
	daySet := make(map[int]bool, len(existing.Days))
	for _, d := range existing.Days {
		daySet[d] = true
	}
	for _, d := range target.Days {
		if !daySet[d] {
			return false
		}
	}
	return true
}

func (e *Enphase) ensureDTGSchedule(ctx context.Context, tz string, loc *time.Location, win enphaseDTGWindow) (bool, error) {
	schedules, err := e.getBatterySchedules(ctx)
	if err != nil {
		return false, err
	}

	var activeDTG []enphaseBatteryScheduleDetail
	for _, d := range schedules.DTG.Details {
		if !d.IsDeleted {
			activeDTG = append(activeDTG, d)
		}
	}

	targetTZ := tz
	if len(activeDTG) > 0 && activeDTG[0].Timezone != "" {
		if loadedLoc, err := time.LoadLocation(activeDTG[0].Timezone); err == nil && loadedLoc.String() == loc.String() {
			targetTZ = activeDTG[0].Timezone
		}
	}
	target := enphaseBatteryScheduleDetail{
		Timezone:     targetTZ,
		StartTime:    fmt.Sprintf("%02d:%02d", win.StartMin/60, win.StartMin%60),
		EndTime:      fmt.Sprintf("%02d:%02d", win.EndMin/60, win.EndMin%60),
		Limit:        win.Limit,
		ScheduleType: "DTG",
		Days:         []int{1, 2, 3, 4, 5, 6, 7},
	}

	if len(activeDTG) == 1 && isDTGScheduleMatch(activeDTG[0], target) {
		log.Ctx(ctx).DebugContext(ctx, "enphase DTG schedule already matches target",
			slog.String("scheduleID", activeDTG[0].ScheduleID),
			slog.String("startTime", target.StartTime),
			slog.String("endTime", target.EndTime),
			slog.Int("limit", target.Limit),
		)
		return false, nil
	}

	if e.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've configured enphase DTG schedule",
			slog.String("startTime", target.StartTime),
			slog.String("endTime", target.EndTime),
			slog.Int("limit", target.Limit),
			slog.String("timezone", target.Timezone),
		)
		return false, nil
	}

	// delete any other active schedules
	for i := 1; i < len(activeDTG); i++ {
		log.Ctx(ctx).DebugContext(ctx, "deleting extra enphase DTG schedule",
			slog.String("scheduleID", activeDTG[i].ScheduleID),
		)
		if err := e.deleteBatterySchedule(ctx, activeDTG[i].ScheduleID); err != nil {
			return false, err
		}
	}

	if len(activeDTG) >= 1 {
		log.Ctx(ctx).DebugContext(ctx, "updating existing enphase DTG schedule",
			slog.String("scheduleID", activeDTG[0].ScheduleID),
			slog.String("startTime", target.StartTime),
			slog.String("endTime", target.EndTime),
			slog.Int("limit", target.Limit),
		)
		if err := e.updateBatterySchedule(ctx, activeDTG[0].ScheduleID, target); err != nil {
			return false, err
		}
	} else {
		log.Ctx(ctx).DebugContext(ctx, "creating new enphase DTG schedule",
			slog.String("startTime", target.StartTime),
			slog.String("endTime", target.EndTime),
			slog.Int("limit", target.Limit),
		)
		if _, err := e.createBatterySchedule(ctx, target); err != nil {
			return false, err
		}
	}

	valid, err := e.validateBatterySchedule(ctx, "dtg")
	if err != nil {
		return false, err
	}
	if !valid {
		return false, errors.New("enphase DTG schedule rejected by isValid check")
	}

	log.Ctx(ctx).InfoContext(ctx, "configured enphase DTG export schedule",
		slog.String("startTime", target.StartTime),
		slog.String("endTime", target.EndTime),
		slog.Int("limit", target.Limit),
	)
	return true, nil
}

type enphaseMinuteRange struct {
	StartMin int
	EndMin   int
}

func mergeEnphaseMinuteRanges(ranges []enphaseMinuteRange) []enphaseMinuteRange {
	if len(ranges) == 0 {
		return nil
	}
	sorted := make([]enphaseMinuteRange, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartMin != sorted[j].StartMin {
			return sorted[i].StartMin < sorted[j].StartMin
		}
		return sorted[i].EndMin < sorted[j].EndMin
	})
	var merged []enphaseMinuteRange
	for _, r := range sorted {
		if r.EndMin <= r.StartMin {
			continue
		}
		if len(merged) > 0 && merged[len(merged)-1].EndMin >= r.StartMin {
			if r.EndMin > merged[len(merged)-1].EndMin {
				merged[len(merged)-1].EndMin = r.EndMin
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func complementEnphaseMinuteRanges(covered []enphaseMinuteRange) []enphaseMinuteRange {
	merged := mergeEnphaseMinuteRanges(covered)
	var gaps []enphaseMinuteRange
	cursor := 0
	for _, r := range merged {
		if r.StartMin > cursor {
			gaps = append(gaps, enphaseMinuteRange{StartMin: cursor, EndMin: r.StartMin})
		}
		if r.EndMin > cursor {
			cursor = r.EndMin
		}
	}
	if cursor < 1440 {
		gaps = append(gaps, enphaseMinuteRange{StartMin: cursor, EndMin: 1440})
	}
	return gaps
}

type enphaseTariffRateGroup struct {
	RateDollars float64
	Ranges      []enphaseMinuteRange
}

type enphaseTariffPlan struct {
	ImportPeakDollars    float64
	ImportPeakRanges     []enphaseMinuteRange
	ImportMidPeakGroups  []enphaseTariffRateGroup
	ImportOffPeakDollars float64
	ImportOffPeakRanges  []enphaseMinuteRange
	ExportType           string // "net_metering" or "tou"
	ExportPeakDollars    float64
	ExportPeakRanges     []enphaseMinuteRange
	ExportOffPeakDollars float64
	ExportOffPeakRanges  []enphaseMinuteRange
}

func (p *enphaseTariffPlan) hasPeakAt(now time.Time) bool {
	if p == nil {
		return false
	}
	nowMin := now.Hour()*60 + now.Minute()
	for _, r := range p.ImportPeakRanges {
		if r.StartMin <= nowMin && nowMin < r.EndMin {
			return true
		}
	}
	return false
}

func (p *enphaseTariffPlan) toTOUSchedule() *types.TOUSchedule {
	if p == nil {
		return nil
	}
	exportPeakFor := func(startMin, endMin int) float64 {
		if p.ExportType == "net_metering" {
			return p.ImportPeakDollars
		}
		for _, er := range p.ExportPeakRanges {
			if er.StartMin < endMin && startMin < er.EndMin {
				return p.ExportPeakDollars
			}
		}
		return p.ExportOffPeakDollars
	}
	exportNonPeakFor := func(importRate float64, startMin, endMin int) float64 {
		if p.ExportType == "net_metering" {
			return importRate
		}
		for _, er := range p.ExportPeakRanges {
			if er.StartMin < endMin && startMin < er.EndMin {
				return p.ExportPeakDollars
			}
		}
		return p.ExportOffPeakDollars
	}

	var periods []types.TOUPeriod
	for _, r := range p.ImportPeakRanges {
		periods = append(periods, types.TOUPeriod{
			StartHour:     r.StartMin / 60,
			StartMinute:   r.StartMin % 60,
			EndHour:       r.EndMin / 60,
			EndMinute:     r.EndMin % 60,
			ImportDollars: p.ImportPeakDollars,
			ExportDollars: exportPeakFor(r.StartMin, r.EndMin),
			BatteryMode:   types.BatteryModeLoad,
			SolarMode:     types.SolarModeExport,
			Peak:          true,
		})
	}
	for _, g := range p.ImportMidPeakGroups {
		for _, r := range g.Ranges {
			periods = append(periods, types.TOUPeriod{
				StartHour:     r.StartMin / 60,
				StartMinute:   r.StartMin % 60,
				EndHour:       r.EndMin / 60,
				EndMinute:     r.EndMin % 60,
				ImportDollars: g.RateDollars,
				ExportDollars: exportNonPeakFor(g.RateDollars, r.StartMin, r.EndMin),
				BatteryMode:   types.BatteryModeLoad,
				SolarMode:     types.SolarModeAny,
				Peak:          false,
			})
		}
	}
	for _, r := range p.ImportOffPeakRanges {
		periods = append(periods, types.TOUPeriod{
			StartHour:     r.StartMin / 60,
			StartMinute:   r.StartMin % 60,
			EndHour:       r.EndMin / 60,
			EndMinute:     r.EndMin % 60,
			ImportDollars: p.ImportOffPeakDollars,
			ExportDollars: exportNonPeakFor(p.ImportOffPeakDollars, r.StartMin, r.EndMin),
			BatteryMode:   types.BatteryModeLoad,
			SolarMode:     types.SolarModeAny,
			Peak:          false,
		})
	}

	sort.Slice(periods, func(i, j int) bool {
		return (periods[i].StartHour*60 + periods[i].StartMinute) < (periods[j].StartHour*60 + periods[j].StartMinute)
	})
	return &types.TOUSchedule{Periods: periods}
}

func enphaseTariffNeedsSync(sched *types.TOUSchedule) bool {
	if sched == nil {
		return false
	}
	for _, p := range sched.Periods {
		if p.Peak || p.BatteryMode == types.BatteryModeExport || p.SolarMode == types.SolarModeExport {
			return true
		}
	}
	return false
}

func formatEnphaseRate(dollars float64) string {
	//  enforces maxDigitsNumericFields=3 and strictly positive TOU rates (> 0).
	if dollars < 0.001 {
		dollars = 0.001
	}
	s := strconv.FormatFloat(dollars, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "00"
	} else if idx := strings.IndexByte(s, '.'); idx >= 0 && len(s)-idx-1 == 1 {
		s += "0"
	}
	return s
}

func subtractEnphaseMinuteRanges(ranges, exclude []enphaseMinuteRange) []enphaseMinuteRange {
	var out []enphaseMinuteRange
	ex := mergeEnphaseMinuteRanges(exclude)
	for _, r := range mergeEnphaseMinuteRanges(ranges) {
		curStart := r.StartMin
		for _, e := range ex {
			if e.EndMin <= curStart {
				continue
			}
			if e.StartMin >= r.EndMin {
				break
			}
			if e.StartMin > curStart {
				out = append(out, enphaseMinuteRange{StartMin: curStart, EndMin: e.StartMin})
			}
			if e.EndMin > curStart {
				curStart = e.EndMin
			}
		}
		if curStart < r.EndMin {
			out = append(out, enphaseMinuteRange{StartMin: curStart, EndMin: r.EndMin})
		}
	}
	return out
}

func ensureMinEnphasePeakDuration(ranges []enphaseMinuteRange) []enphaseMinuteRange {
	var expanded []enphaseMinuteRange
	for _, r := range ranges {
		if r.EndMin-r.StartMin < enphaseMinScheduleMinutes {
			if r.StartMin+enphaseMinScheduleMinutes <= 1440 {
				r.EndMin = r.StartMin + enphaseMinScheduleMinutes
			} else {
				r.StartMin = max(0, 1440-enphaseMinScheduleMinutes)
				r.EndMin = 1440
			}
		}
		expanded = append(expanded, r)
	}
	return mergeEnphaseMinuteRanges(expanded)
}

func buildEnphaseTariffPlan(ctx context.Context, sched *types.TOUSchedule, now time.Time) *enphaseTariffPlan {
	if sched == nil || len(sched.Periods) == 0 {
		return nil
	}

	_, tierPeriods := classifyTOUScheduleTiers(sched.Periods)
	onPeakPeriods := tierPeriods[touTierOnPeak]
	if len(onPeakPeriods) == 0 {
		return nil
	}

	var peakRanges []enphaseMinuteRange
	for _, p := range onPeakPeriods {
		peakRanges = append(peakRanges, enphaseMinuteRange{
			StartMin: p.StartHour*60 + p.StartMinute,
			EndMin:   min(p.EndHour*60+p.EndMinute, 1440),
		})
	}
	mergedPeakRanges := ensureMinEnphasePeakDuration(mergeEnphaseMinuteRanges(peakRanges))

	// Enphase limits manual TOU peak schedules to enphaseMaxPeakPeriods (5).
	var overflowPeakRanges []enphaseMinuteRange
	if len(mergedPeakRanges) > enphaseMaxPeakPeriods {
		rawPeakRanges := mergedPeakRanges
		nowMin := now.Hour()*60 + now.Minute()
		type rankedRange struct {
			r      enphaseMinuteRange
			active bool
		}
		ranked := make([]rankedRange, len(mergedPeakRanges))
		for i, r := range mergedPeakRanges {
			ranked[i] = rankedRange{
				r:      r,
				active: r.StartMin <= nowMin && nowMin < r.EndMin,
			}
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			if ranked[i].active != ranked[j].active {
				return ranked[i].active
			}
			return (ranked[i].r.EndMin - ranked[i].r.StartMin) > (ranked[j].r.EndMin - ranked[j].r.StartMin)
		})
		kept := make([]enphaseMinuteRange, enphaseMaxPeakPeriods)
		for i := 0; i < enphaseMaxPeakPeriods; i++ {
			kept[i] = ranked[i].r
		}
		for i := enphaseMaxPeakPeriods; i < len(ranked); i++ {
			overflowPeakRanges = append(overflowPeakRanges, ranked[i].r)
		}
		mergedPeakRanges = mergeEnphaseMinuteRanges(kept)
		overflowPeakRanges = subtractEnphaseMinuteRanges(overflowPeakRanges, mergedPeakRanges)
		log.Ctx(ctx).DebugContext(ctx, "enphase TOU schedule exceeded max peak periods; prioritizing active and longest windows",
			slog.Int("totalPeakPeriods", len(rawPeakRanges)),
			slog.Int("maxPeakPeriods", enphaseMaxPeakPeriods),
			slog.Any("allPeakRanges", rawPeakRanges),
			slog.Any("keptPeakRanges", mergedPeakRanges),
			slog.Any("overflowPeakRanges", overflowPeakRanges),
		)
	}

	peakBuy, peakSell := calculateDurationWeightedRates(onPeakPeriods)
	peakBuy = max(0.001, peakBuy)
	peakSell = max(0.001, peakSell)

	// Determine base off-peak tier and up to two mid-peak groups while preserving real utility rates.
	var nonPeakTiers []touTier
	for _, t := range []touTier{touTierSuperOffPeak, touTierOffPeak, touTierPartialPeak} {
		if len(tierPeriods[t]) > 0 {
			nonPeakTiers = append(nonPeakTiers, t)
		}
	}

	var baseOffPeakBuy, baseOffPeakSell float64
	var midPeakGroups []enphaseTariffRateGroup
	coveredByPeakAndMid := make([]enphaseMinuteRange, len(mergedPeakRanges))
	copy(coveredByPeakAndMid, mergedPeakRanges)

	if len(nonPeakTiers) > 0 {
		baseTier := nonPeakTiers[0]
		bRate, sRate := calculateDurationWeightedRates(tierPeriods[baseTier])
		baseOffPeakBuy = max(0.001, bRate)
		baseOffPeakSell = max(0.001, sRate)

		prevMaxBuy := baseOffPeakBuy
		for idx, midTier := range nonPeakTiers[1:] {
			mBuy, _ := calculateDurationWeightedRates(tierPeriods[midTier])
			mBuy = max(0.001, mBuy)
			if mBuy <= prevMaxBuy {
				mBuy = prevMaxBuy + 0.001
			}
			prevMaxBuy = mBuy

			var mRanges []enphaseMinuteRange
			for _, p := range tierPeriods[midTier] {
				mRanges = append(mRanges, enphaseMinuteRange{
					StartMin: p.StartHour*60 + p.StartMinute,
					EndMin:   min(p.EndHour*60+p.EndMinute, 1440),
				})
			}
			if idx == len(nonPeakTiers[1:])-1 && len(overflowPeakRanges) > 0 {
				mRanges = append(mRanges, overflowPeakRanges...)
				overflowPeakRanges = nil
			}
			// Trim against already-claimed peak/mid-peak intervals and enforce Enphase 15m minimum.
			mTrimmed := subtractEnphaseMinuteRanges(mRanges, coveredByPeakAndMid)
			var mValid []enphaseMinuteRange
			for _, r := range mTrimmed {
				if r.EndMin-r.StartMin >= enphaseMinScheduleMinutes {
					mValid = append(mValid, r)
				}
			}
			if len(mValid) > 0 {
				midPeakGroups = append(midPeakGroups, enphaseTariffRateGroup{
					RateDollars: mBuy,
					Ranges:      mValid,
				})
				coveredByPeakAndMid = append(coveredByPeakAndMid, mValid...)
			}
		}
		if len(overflowPeakRanges) > 0 {
			mTrimmed := subtractEnphaseMinuteRanges(overflowPeakRanges, coveredByPeakAndMid)
			var mValid []enphaseMinuteRange
			for _, r := range mTrimmed {
				if r.EndMin-r.StartMin >= enphaseMinScheduleMinutes {
					mValid = append(mValid, r)
				}
			}
			if len(mValid) > 0 {
				mBuy := max(prevMaxBuy+0.001, peakBuy-0.001)
				midPeakGroups = append(midPeakGroups, enphaseTariffRateGroup{
					RateDollars: mBuy,
					Ranges:      mValid,
				})
				coveredByPeakAndMid = append(coveredByPeakAndMid, mValid...)
			}
		}
	} else {
		baseOffPeakBuy = max(0.001, peakBuy-0.01)
		baseOffPeakSell = max(0.001, peakSell-0.01)
	}

	highestNonPeakBuy := baseOffPeakBuy
	if len(midPeakGroups) > 0 {
		highestNonPeakBuy = midPeakGroups[len(midPeakGroups)-1].RateDollars
	}
	if peakBuy <= highestNonPeakBuy {
		peakBuy = highestNonPeakBuy + 0.001
	}

	offPeakRanges := complementEnphaseMinuteRanges(coveredByPeakAndMid)

	isNetMetering := true
	var nonPeakExportPeriods []types.TOUPeriod
	for _, p := range sched.Periods {
		if math.Abs(p.ImportDollars-p.ExportDollars) > 0.005 {
			isNetMetering = false
		}
		if !p.Peak && p.BatteryMode != types.BatteryModeExport && p.SolarMode != types.SolarModeExport {
			nonPeakExportPeriods = append(nonPeakExportPeriods, p)
		}
	}

	plan := &enphaseTariffPlan{
		ImportPeakDollars:    peakBuy,
		ImportPeakRanges:     mergedPeakRanges,
		ImportMidPeakGroups:  midPeakGroups,
		ImportOffPeakDollars: baseOffPeakBuy,
		ImportOffPeakRanges:  offPeakRanges,
		ExportType:           "net_metering",
	}

	if !isNetMetering {
		exportOffPeakDollars := baseOffPeakSell
		if len(nonPeakExportPeriods) > 0 {
			_, expSell := calculateDurationWeightedRates(nonPeakExportPeriods)
			exportOffPeakDollars = max(0.001, expSell)
		}
		exportPeakDollars := max(exportOffPeakDollars+0.001, peakSell)
		plan.ExportType = "tou"
		plan.ExportPeakDollars = exportPeakDollars
		plan.ExportPeakRanges = mergedPeakRanges
		plan.ExportOffPeakDollars = exportOffPeakDollars
		plan.ExportOffPeakRanges = complementEnphaseMinuteRanges(mergedPeakRanges)
	}

	return plan
}

func (e *Enphase) getTariff(ctx context.Context) (enphaseTariffJSONResponse, error) {
	u := e.baseURL.JoinPath(fmt.Sprintf("/app-api/%d/tariff.json", e.systemID))
	q := u.Query()
	q.Set("country", "us")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return enphaseTariffJSONResponse{}, err
	}
	var res enphaseTariffJSONResponse
	if err := e.doRequest(req, &res); err != nil {
		return enphaseTariffJSONResponse{}, err
	}
	return res, nil
}

func parseEnphaseJSONInt(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(math.Round(val)), true
	case int:
		return val, true
	case string:
		if val == "" {
			return 0, false
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func parseEnphaseJSONFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f
		}
	}
	return 0
}

func normalizeEnphaseEndMinute(endMin int) int {
	// Autofill tariffs store endTime on :59 boundaries (e.g., 1199 for 20:00, 1439 for 24:00).
	if endMin%60 == 59 {
		return min(endMin+1, 1440)
	}
	return min(endMin, 1440)
}

func findEnphaseTariffDaySchedule(section *enphaseTariffSection, now time.Time) *enphaseTariffDaySchedule {
	if section == nil || len(section.Seasons) == 0 {
		return nil
	}
	curMonth := int(now.Month())
	selectedSeason := &section.Seasons[0]
	for i := range section.Seasons {
		s := &section.Seasons[i]
		sm, ok1 := parseEnphaseJSONInt(s.StartMonth)
		em, ok2 := parseEnphaseJSONInt(s.EndMonth)
		if !ok1 || !ok2 || sm <= 0 || em <= 0 {
			continue
		}
		if sm <= em {
			if curMonth >= sm && curMonth <= em {
				selectedSeason = s
				break
			}
		} else if curMonth >= sm || curMonth <= em {
			selectedSeason = s
			break
		}
	}
	if len(selectedSeason.Days) == 0 {
		return nil
	}
	// Enphase days use 1=Mon..7=Sun.
	enphaseDay := int(now.Weekday())
	if enphaseDay == 0 {
		enphaseDay = 7
	}
	for i := range selectedSeason.Days {
		for _, d := range selectedSeason.Days[i].Days {
			if d == enphaseDay {
				return &selectedSeason.Days[i]
			}
		}
	}
	return &selectedSeason.Days[0]
}

type enphaseResolvedTariffSlot struct {
	StartMin int
	EndMin   int
	Rate     float64
	Type     string
}

func resolveEnphaseTariffSlots(section *enphaseTariffSection, now time.Time) []enphaseResolvedTariffSlot {
	daySched := findEnphaseTariffDaySchedule(section, now)
	if daySched == nil {
		return nil
	}
	var timed []enphaseResolvedTariffSlot
	var defaultRate float64
	defaultType := "off-peak"
	for _, p := range daySched.Periods {
		rate := parseEnphaseJSONFloat(p.Rate)
		sMin, ok1 := parseEnphaseJSONInt(p.StartTime)
		eMin, ok2 := parseEnphaseJSONInt(p.EndTime)
		if !ok1 || !ok2 {
			defaultRate = rate
			if p.Type != "" {
				defaultType = p.Type
			}
			continue
		}
		eMin = normalizeEnphaseEndMinute(eMin)
		if eMin > sMin {
			timed = append(timed, enphaseResolvedTariffSlot{
				StartMin: max(0, sMin),
				EndMin:   eMin,
				Rate:     rate,
				Type:     p.Type,
			})
		}
	}
	sort.Slice(timed, func(i, j int) bool {
		return timed[i].StartMin < timed[j].StartMin
	})

	var all []enphaseResolvedTariffSlot
	cursor := 0
	for _, sl := range timed {
		if sl.StartMin > cursor {
			all = append(all, enphaseResolvedTariffSlot{
				StartMin: cursor,
				EndMin:   sl.StartMin,
				Rate:     defaultRate,
				Type:     defaultType,
			})
		}
		all = append(all, sl)
		if sl.EndMin > cursor {
			cursor = sl.EndMin
		}
	}
	if cursor < 1440 {
		all = append(all, enphaseResolvedTariffSlot{
			StartMin: cursor,
			EndMin:   1440,
			Rate:     defaultRate,
			Type:     defaultType,
		})
	}
	return all
}

func enphaseTariffIsPeakAt(res enphaseTariffJSONResponse, now time.Time) bool {
	nowMin := now.Hour()*60 + now.Minute()
	for _, sl := range resolveEnphaseTariffSlots(res.Purchase, now) {
		if sl.StartMin <= nowMin && nowMin < sl.EndMin {
			return sl.Type == "peak"
		}
	}
	return false
}

func findEnphaseSlotAt(slots []enphaseResolvedTariffSlot, mid int) (enphaseResolvedTariffSlot, bool) {
	for _, s := range slots {
		if s.StartMin <= mid && mid < s.EndMin {
			return s, true
		}
	}
	return enphaseResolvedTariffSlot{}, false
}

func parseEnphaseTariffTOUSchedule(res enphaseTariffJSONResponse, now time.Time) *types.TOUSchedule {
	if res.Purchase == nil || (res.Purchase.TypeID != "" && res.Purchase.TypeID != "tou") {
		return nil
	}
	buySlots := resolveEnphaseTariffSlots(res.Purchase, now)
	if len(buySlots) == 0 {
		return nil
	}
	sellSlots := resolveEnphaseTariffSlots(res.Buyback, now)

	boundarySet := map[int]bool{0: true, 1440: true}
	for _, s := range buySlots {
		boundarySet[s.StartMin] = true
		boundarySet[s.EndMin] = true
	}
	for _, s := range sellSlots {
		boundarySet[s.StartMin] = true
		boundarySet[s.EndMin] = true
	}
	boundaries := make([]int, 0, len(boundarySet))
	for b := range boundarySet {
		boundaries = append(boundaries, b)
	}
	sort.Ints(boundaries)

	var periods []types.TOUPeriod
	for i := 0; i < len(boundaries)-1; i++ {
		startMin, endMin := boundaries[i], boundaries[i+1]
		if endMin <= startMin {
			continue
		}
		mid := (startMin + endMin) / 2
		buySlot, hasBuy := findEnphaseSlotAt(buySlots, mid)
		if !hasBuy {
			continue
		}
		sellRate := buySlot.Rate
		isSellPeak := false
		if sellSlot, hasSell := findEnphaseSlotAt(sellSlots, mid); hasSell {
			sellRate = sellSlot.Rate
			isSellPeak = sellSlot.Type == "peak"
		}
		isPeak := buySlot.Type == "peak" || isSellPeak
		solarMode := types.SolarModeAny
		if isPeak {
			solarMode = types.SolarModeExport
		}
		periods = append(periods, types.TOUPeriod{
			StartHour:     startMin / 60,
			StartMinute:   startMin % 60,
			EndHour:       endMin / 60,
			EndMinute:     endMin % 60,
			ImportDollars: buySlot.Rate,
			ExportDollars: sellRate,
			BatteryMode:   types.BatteryModeLoad,
			SolarMode:     solarMode,
			Peak:          isPeak,
		})
	}
	return &types.TOUSchedule{Periods: periods}
}

func enphaseTariffScheduleMatch(res enphaseTariffJSONResponse, plan *enphaseTariffPlan, now time.Time) bool {
	if plan == nil {
		return true
	}
	existingSched := parseEnphaseTariffTOUSchedule(res, now)
	if existingSched == nil {
		return false
	}
	targetSched := plan.toTOUSchedule()
	if targetSched == nil {
		return false
	}
	return !existingSched.IsSignificantlyDifferent(targetSched, now)
}

func (e *Enphase) getTariffJourney(ctx context.Context) (map[string]any, error) {
	path := fmt.Sprintf("/service/tariff/tariff-ms/systems/%d/tariff-journey", e.systemID)
	u := e.baseURL.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data map[string]any `json:"data"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (e *Enphase) postTariffJourney(ctx context.Context, payload map[string]any) error {
	path := fmt.Sprintf("/service/tariff/tariff-ms/systems/%d/tariff-journey", e.systemID)
	u := e.baseURL.JoinPath(path)
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal tariff journey payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok, ok := e.lastCSRFToken[path]; ok && tok.Token != "" {
		req.Header.Set("X-Xsrf-Token", tok.Token)
		delete(e.lastCSRFToken, path)
	}
	var res struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := e.doRequest(req, &res); err != nil {
		return fmt.Errorf("failed to post enphase tariff journey: %w", err)
	}
	return nil
}

func buildEnphaseJourneyPurchaseRate(plan *enphaseTariffPlan) map[string]any {
	peakPeriods := make([]map[string]any, 0, len(plan.ImportPeakRanges))
	peakStr := formatEnphaseRate(plan.ImportPeakDollars)
	for _, r := range plan.ImportPeakRanges {
		peakPeriods = append(peakPeriods, map[string]any{
			"startTime": r.StartMin,
			"endTime":   r.EndMin,
			"rate":      peakStr,
			"type":      "peak",
		})
	}
	offPeakTimings := make([]map[string]any, 0, len(plan.ImportOffPeakRanges))
	for _, r := range plan.ImportOffPeakRanges {
		offPeakTimings = append(offPeakTimings, map[string]any{
			"startTime": strconv.Itoa(r.StartMin),
			"endTime":   strconv.Itoa(r.EndMin),
		})
	}

	entry := map[string]any{
		"baseline":       "",
		"days":           []int{1, 2, 3, 4, 5, 6, 7},
		"isFlatRate":     false,
		"isSameRate":     false,
		"offPeak":        formatEnphaseRate(plan.ImportOffPeakDollars),
		"offPeakTimings": offPeakTimings,
		"peakRate":       peakStr,
		"periods":        peakPeriods,
		"rate":           "",
		"tiers":          []any{},
	}

	if len(plan.ImportMidPeakGroups) > 0 {
		var midPeakList []map[string]any
		for _, g := range plan.ImportMidPeakGroups {
			var gPeriods []map[string]any
			for _, r := range g.Ranges {
				gPeriods = append(gPeriods, map[string]any{
					"startTime": strconv.Itoa(r.StartMin),
					"endTime":   strconv.Itoa(r.EndMin),
				})
			}
			midPeakList = append(midPeakList, map[string]any{
				"periods": gPeriods,
				"rate":    formatEnphaseRate(g.RateDollars),
				"type":    "mid-peak",
			})
		}
		entry["midPeakPeriods"] = midPeakList
	}

	return entry
}

func buildEnphaseJourneyExportRate(plan *enphaseTariffPlan) map[string]any {
	peakPeriods := make([]map[string]any, 0, len(plan.ExportPeakRanges))
	peakStr := formatEnphaseRate(plan.ExportPeakDollars)
	for _, r := range plan.ExportPeakRanges {
		peakPeriods = append(peakPeriods, map[string]any{
			"startTime": r.StartMin,
			"endTime":   r.EndMin,
			"rate":      peakStr,
			"type":      "peak",
		})
	}
	offPeakTimings := make([]map[string]any, 0, len(plan.ExportOffPeakRanges))
	for _, r := range plan.ExportOffPeakRanges {
		offPeakTimings = append(offPeakTimings, map[string]any{
			"startTime": strconv.Itoa(r.StartMin),
			"endTime":   strconv.Itoa(r.EndMin),
		})
	}

	return map[string]any{
		"baseline":       "",
		"days":           []int{1, 2, 3, 4, 5, 6, 7},
		"offPeak":        formatEnphaseRate(plan.ExportOffPeakDollars),
		"offPeakTimings": offPeakTimings,
		"peakRate":       peakStr,
		"periods":        peakPeriods,
		"rate":           "0.0",
		"rateIndex":      0,
		"rateType":       "tou",
		"seasonId":       "default",
		"tiers":          []any{},
	}
}

func setJourneyComponentDefault(components []any, id string, val any) {
	for _, c := range components {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cm["id"] == id {
			cm["defaultValue"] = val
		}
		if children, ok := cm["children"].([]any); ok {
			setJourneyComponentDefault(children, id, val)
		}
	}
}

func buildEnphaseImportRateComponent(importRateEntry map[string]any, plan *enphaseTariffPlan) map[string]any {
	const prefix = "energy-rate-entry_default_rate_1"
	peakStr := formatEnphaseRate(plan.ImportPeakDollars)
	offPeakStr := formatEnphaseRate(plan.ImportOffPeakDollars)

	touChildren := []any{
		map[string]any{
			"id":           prefix + "_touRateSection_offPeakRate",
			"type":         "NUMBER_INPUT",
			"defaultValue": offPeakStr,
			"required":     true,
		},
		map[string]any{
			"id":           prefix + "_touRateSection_peakRate",
			"type":         "NUMBER_INPUT",
			"defaultValue": peakStr,
			"required":     true,
		},
	}
	for i, r := range plan.ImportPeakRanges {
		pid := fmt.Sprintf("%s_touRateSection_peakPeriods_p%d", prefix, i)
		touChildren = append(touChildren, map[string]any{
			"id":       pid,
			"type":     "TOU_PERIOD_SELECTOR",
			"required": true,
			"children": []any{
				map[string]any{"id": pid + "_startTime", "type": "TIME_PICKER", "defaultValue": r.StartMin, "required": true},
				map[string]any{"id": pid + "_endTime", "type": "TIME_PICKER", "defaultValue": r.EndMin, "required": true},
			},
		})
	}
	for gIdx, g := range plan.ImportMidPeakGroups {
		gid := fmt.Sprintf("%s_touRateSection_midPeakRateGroup_g%d", prefix, gIdx)
		gChildren := []any{
			map[string]any{
				"id":           gid + "_midPeakRate",
				"type":         "NUMBER_INPUT",
				"defaultValue": formatEnphaseRate(g.RateDollars),
				"required":     true,
			},
		}
		for pIdx, r := range g.Ranges {
			mpID := fmt.Sprintf("%s_midPeakPeriods_p%d", gid, pIdx)
			gChildren = append(gChildren, map[string]any{
				"id":       mpID,
				"type":     "TOU_PERIOD_SELECTOR",
				"required": false,
				"children": []any{
					map[string]any{"id": mpID + "_startTime", "type": "TIME_PICKER", "defaultValue": strconv.Itoa(r.StartMin), "required": true},
					map[string]any{"id": mpID + "_endTime", "type": "TIME_PICKER", "defaultValue": strconv.Itoa(r.EndMin), "required": true},
				},
			})
		}
		touChildren = append(touChildren, map[string]any{
			"id":       gid,
			"type":     "RATE_GROUP_REPEATER",
			"required": false,
			"children": gChildren,
		})
	}

	return map[string]any{
		"id":           prefix,
		"type":         "SEASON_RATE_REPEATER",
		"dependsOn":    "seasons",
		"properties":   map[string]any{"seasonId": "default"},
		"defaultValue": importRateEntry,
		"children": []any{
			map[string]any{
				"id":           prefix + "_isSameRate",
				"type":         "CHECKBOX",
				"defaultValue": false,
				"dependsOn":    "tariffType",
				"showWhen":     []string{"tou"},
			},
			map[string]any{
				"id":           prefix + "_daySelection",
				"type":         "DAY_SELECTOR",
				"defaultValue": []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
				"required":     true,
			},
			map[string]any{
				"id":        prefix + "_touRateSection",
				"type":      "SECTION",
				"dependsOn": "tariffType",
				"showWhen":  []string{"tou"},
				"children":  touChildren,
			},
		},
	}
}

func buildEnphaseExportRateComponent(exportRateEntry map[string]any, plan *enphaseTariffPlan) map[string]any {
	const prefix = "export-energy-rate-entry_default_rate_1"
	peakStr := formatEnphaseRate(plan.ExportPeakDollars)
	offPeakStr := formatEnphaseRate(plan.ExportOffPeakDollars)

	touChildren := []any{
		map[string]any{
			"id":           "offPeakRate",
			"type":         "NUMBER_INPUT",
			"defaultValue": offPeakStr,
			"required":     true,
		},
		map[string]any{
			"id":           "peakRate",
			"type":         "NUMBER_INPUT",
			"defaultValue": peakStr,
			"required":     true,
		},
	}
	for i, r := range plan.ExportPeakRanges {
		pid := fmt.Sprintf("%s_peakPeriods_p%d", prefix, i)
		touChildren = append(touChildren, map[string]any{
			"id":       pid,
			"type":     "TOU_PERIOD_SELECTOR",
			"required": true,
			"children": []any{
				map[string]any{"id": pid + "_startTime", "type": "TIME_PICKER", "defaultValue": r.StartMin, "required": true},
				map[string]any{"id": pid + "_endTime", "type": "TIME_PICKER", "defaultValue": r.EndMin, "required": true},
			},
		})
	}

	return map[string]any{
		"id":           prefix,
		"type":         "SEASON_RATE_REPEATER",
		"dependsOn":    "exportSeasons",
		"properties":   map[string]any{"seasonId": "default"},
		"defaultValue": exportRateEntry,
		"children": []any{
			map[string]any{
				"id":           prefix + "_daySelection",
				"type":         "DAY_SELECTOR",
				"defaultValue": []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
				"required":     true,
			},
			map[string]any{
				"id":        prefix + "_touRateSection",
				"type":      "SECTION",
				"dependsOn": "exportType&exportTodPayout",
				"showWhen":  "exportType=tou|exportType=net_fit&exportTodPayout=yes",
				"children":  touChildren,
			},
		},
	}
}

func replaceJourneyComponentsByPrefix(components []any, prefix string, replacement map[string]any) []any {
	var out []any
	inserted := false
	for _, c := range components {
		cm, ok := c.(map[string]any)
		if ok {
			if id, _ := cm["id"].(string); strings.HasPrefix(id, prefix) {
				if replacement != nil && !inserted {
					out = append(out, replacement)
					inserted = true
				}
				continue
			}
		}
		out = append(out, c)
	}
	if replacement != nil && !inserted {
		return append([]any{replacement}, out...)
	}
	return out
}

func applyEnphaseTariffPlanToJourney(journey map[string]any, plan *enphaseTariffPlan, now time.Time) {
	if journey == nil || plan == nil {
		return
	}

	defaultValues, ok := journey["defaultValues"].(map[string]any)
	if !ok {
		defaultValues = make(map[string]any)
		journey["defaultValues"] = defaultValues
	}

	importRateEntry := buildEnphaseJourneyPurchaseRate(plan)

	defaultValues["tariffMode"] = "manual"
	defaultValues["tariffType"] = "tou"
	defaultValues["hasSeasonalRates"] = "no"
	defaultValues["hasDemandCharge"] = "no"
	defaultValues["hasSeasonalDemandCharge"] = "no"
	defaultValues["hasTimeBased"] = "no"
	defaultValues["hasTieredDemand"] = "no"
	defaultValues["hasMinBillableDemand"] = false
	defaultValues["demandCharges"] = map[string]any{}
	defaultValues["flatRates"] = map[string]any{}
	defaultValues["tieredRates"] = map[string]any{}
	defaultValues["rates"] = map[string]any{
		"default": []any{importRateEntry},
	}

	defaultValues["exportSeasonalVariation"] = "no"
	defaultValues["exportType"] = plan.ExportType
	var exportRateEntry map[string]any
	if plan.ExportType == "net_metering" {
		defaultValues["exportTodPayout"] = "no"
		defaultValues["exportRates"] = map[string]any{}
	} else {
		exportRateEntry = buildEnphaseJourneyExportRate(plan)
		defaultValues["exportRates"] = map[string]any{
			"default": []any{exportRateEntry},
		}
	}

	todayStr := now.Format("2006-01-02")
	bs, _ := defaultValues["billingStartDay"].(string)
	if bs == "" || bs > todayStr {
		bs = todayStr
	}
	if installDate, _ := defaultValues["installDate"].(string); installDate != "" && bs < installDate {
		bs = installDate
	}
	defaultValues["billingStartDay"] = bs
	if defaultValues["billingCycle"] == nil {
		defaultValues["billingCycle"] = map[string]any{"type": "months", "value": 1}
	}
	defaultValues["_billingSet"] = "set"

	screens, ok := journey["screens"].([]any)
	if !ok {
		return
	}
	for _, s := range screens {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		comps, _ := sm["components"].([]any)
		switch sm["id"] {
		case "tariff-type-selection":
			setJourneyComponentDefault(comps, "tariffMode", "manual")
			setJourneyComponentDefault(comps, "tariffType", "tou")
			setJourneyComponentDefault(comps, "hasSeasonalRates", "no")
			setJourneyComponentDefault(comps, "hasDemandCharge", "no")
		case "season-rate-entry":
			sm["components"] = replaceJourneyComponentsByPrefix(comps, "energy-rate-entry_", buildEnphaseImportRateComponent(importRateEntry, plan))
		case "export-type-selection":
			setJourneyComponentDefault(comps, "exportType", plan.ExportType)
			setJourneyComponentDefault(comps, "exportSeasonalVariation", "no")
			if plan.ExportType == "net_metering" {
				setJourneyComponentDefault(comps, "exportTodPayout", "no")
			}
		case "export-season-entry":
			if exportRateEntry != nil {
				sm["components"] = replaceJourneyComponentsByPrefix(comps, "export-energy-rate-entry_", buildEnphaseExportRateComponent(exportRateEntry, plan))
			} else {
				sm["components"] = replaceJourneyComponentsByPrefix(comps, "export-energy-rate-entry_", nil)
			}
		case "billing-details":
			setJourneyComponentDefault(comps, "billingStartDay", defaultValues["billingStartDay"])
			setJourneyComponentDefault(comps, "billingCycle", defaultValues["billingCycle"])
		}
	}
}

func (e *Enphase) ensureTOUTariffSynced(ctx context.Context, sched *types.TOUSchedule, now time.Time, requirePeakNow bool, batteryChangePending bool) (bool, bool, error) {
	existingTariff, err := e.getTariff(ctx)
	if err != nil {
		return false, false, err
	}
	hasPeakNow := enphaseTariffIsPeakAt(existingTariff, now)

	if !enphaseTariffNeedsSync(sched) {
		log.Ctx(ctx).DebugContext(ctx, "skipping enphase TOU tariff sync: schedule has no peak or export periods",
			slog.Bool("hasPeakNow", hasPeakNow),
		)
		return false, hasPeakNow, nil
	}

	plan := buildEnphaseTariffPlan(ctx, sched, now)
	if plan == nil {
		log.Ctx(ctx).DebugContext(ctx, "skipping enphase TOU tariff sync: no on-peak periods in schedule",
			slog.Bool("hasPeakNow", hasPeakNow),
		)
		return false, hasPeakNow, nil
	}

	peakMismatch := requirePeakNow && plan.hasPeakAt(now) && !hasPeakNow
	if !peakMismatch && enphaseTariffScheduleMatch(existingTariff, plan, now) {
		log.Ctx(ctx).DebugContext(ctx, "enphase TOU tariff already matches schedule",
			slog.Bool("hasPeakNow", hasPeakNow),
			slog.Bool("requirePeakNow", requirePeakNow),
		)
		return false, hasPeakNow, nil
	}

	log.Ctx(ctx).DebugContext(ctx, "syncing enphase TOU tariff plan",
		slog.Bool("peakMismatch", peakMismatch),
		slog.Bool("hasPeakNow", hasPeakNow),
		slog.Any("plan", plan),
	)

	if batteryChangePending {
		log.Ctx(ctx).InfoContext(ctx, "enphase battery change pending, deferring tariff-journey update")
		return false, hasPeakNow, nil
	}

	if e.settings.DryRun {
		log.Ctx(ctx).InfoContext(ctx, "dry run: would've updated enphase TOU tariff",
			slog.Float64("importPeakDollars", plan.ImportPeakDollars),
			slog.Float64("importOffPeakDollars", plan.ImportOffPeakDollars),
			slog.Int("peakPeriods", len(plan.ImportPeakRanges)),
			slog.String("exportType", plan.ExportType),
		)
		return false, hasPeakNow, nil
	}

	journey, err := e.getTariffJourney(ctx)
	if err != nil {
		return false, hasPeakNow, err
	}
	applyEnphaseTariffPlanToJourney(journey, plan, now)
	if err := e.postTariffJourney(ctx, journey); err != nil {
		return false, hasPeakNow, err
	}

	log.Ctx(ctx).InfoContext(ctx, "updated enphase TOU tariff",
		slog.Float64("importPeakDollars", plan.ImportPeakDollars),
		slog.Float64("importOffPeakDollars", plan.ImportOffPeakDollars),
		slog.Int("peakPeriods", len(plan.ImportPeakRanges)),
		slog.String("exportType", plan.ExportType),
	)
	return true, plan.hasPeakAt(now), nil
}

func hasCorruptEnphaseInterval(ctx context.Context, stat enphaseTodayStats, idx int, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	intervalSecs := stat.IntervalLength
	if intervalSecs == 0 {
		intervalSecs = 900
	}
	intervalStart := time.Unix(stat.StartTime, 0).In(loc).Add(time.Duration(idx*intervalSecs) * time.Second)
	intervalEnd := intervalStart.Add(time.Duration(intervalSecs) * time.Second)

	val := func(arr []float64) float64 {
		if idx < len(arr) {
			return arr[idx]
		}
		return 0
	}

	prod := val(stat.Production)
	cons := val(stat.Consumption)
	gridImp := val(stat.GridImport)
	solarHome := val(stat.SolarHome)
	solarBat := val(stat.SolarBattery)
	solarGrid := val(stat.SolarGrid)
	gridBat := val(stat.GridBattery)
	gridHome := val(stat.GridHome)
	batHome := val(stat.BatteryHome)
	batGrid := val(stat.BatteryGrid)

	warnCorrupt := func(reason string, extraAttrs ...any) {
		attrs := []any{
			slog.String("reason", reason),
			slog.Time("intervalStart", intervalStart),
			slog.Time("intervalEnd", intervalEnd),
			slog.Int("intervalIndex", idx),
			slog.Float64("productionWH", prod),
			slog.Float64("consumptionWH", cons),
			slog.Float64("solarHomeWH", solarHome),
			slog.Float64("solarBatteryWH", solarBat),
			slog.Float64("solarGridWH", solarGrid),
			slog.Float64("gridHomeWH", gridHome),
			slog.Float64("gridBatteryWH", gridBat),
			slog.Float64("batteryHomeWH", batHome),
			slog.Float64("batteryGridWH", batGrid),
		}
		if idx < len(stat.GridImport) && gridImp != 0 {
			attrs = append(attrs, slog.Float64("gridImport", gridImp))
		}
		attrs = append(attrs, extraAttrs...)
		log.Ctx(ctx).WarnContext(ctx, "skipping corrupt enphase interval", attrs...)
	}

	checkNeg := func(arr []float64) bool {
		return idx < len(arr) && arr[idx] < 0
	}
	if checkNeg(stat.Production) ||
		checkNeg(stat.Consumption) ||
		checkNeg(stat.GridImport) ||
		checkNeg(stat.SolarHome) ||
		checkNeg(stat.SolarBattery) ||
		checkNeg(stat.SolarGrid) ||
		checkNeg(stat.GridBattery) ||
		checkNeg(stat.GridHome) ||
		checkNeg(stat.BatteryHome) ||
		checkNeg(stat.BatteryGrid) {
		warnCorrupt("negative_value")
		return true
	}

	// Solar production and home consumption should match their directional component sums when both are reported.
	solarSum := solarHome + solarBat + solarGrid
	if prod > 0 && solarSum > 0 && math.Abs(prod-solarSum) > 50 {
		warnCorrupt("production_sum_mismatch", slog.Float64("solarSumWH", solarSum))
		return true
	}
	homeSum := solarHome + batHome + gridHome
	if cons > 0 && homeSum > 0 && math.Abs(cons-homeSum) > 50 {
		warnCorrupt("consumption_sum_mismatch", slog.Float64("homeSumWH", homeSum))
		return true
	}

	// Large net battery flow (> 1.5 kWh in 15m) with flat SOC (<= 2% change) or opposite SOC change (> 1 kWh with > 1% opposite SOC change).
	if idx > 0 && idx < len(stat.SOC) && stat.SOC[idx] != nil && stat.SOC[idx-1] != nil {
		prevSOC := *stat.SOC[idx-1]
		currSOC := *stat.SOC[idx]
		socDelta := currSOC - prevSOC
		batIn := solarBat + gridBat
		batOut := batHome + batGrid
		netBatWH := batIn - batOut
		if math.Abs(netBatWH) > 1500 && math.Abs(socDelta) <= 2 {
			warnCorrupt("net_battery_vs_flat_soc",
				slog.Float64("netBatteryWH", netBatWH),
				slog.Float64("prevSOC", prevSOC),
				slog.Float64("currSOC", currSOC),
				slog.Float64("socDelta", socDelta),
			)
			return true
		}
		if (netBatWH > 1000 && socDelta < -1) || (netBatWH < -1000 && socDelta > 1) {
			warnCorrupt("net_battery_opposite_soc",
				slog.Float64("netBatteryWH", netBatWH),
				slog.Float64("prevSOC", prevSOC),
				slog.Float64("currSOC", currSOC),
				slog.Float64("socDelta", socDelta),
			)
			return true
		}
	}

	return false
}

func parseDailyStats(ctx context.Context, stat enphaseTodayStats, dayStart time.Time, loc *time.Location) types.DailyEnergyStats {
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
				TSHourStart:  hourStart,
				TimeLocation: loc.String(),
			}
			hourlyStatsMap[hourStart] = s
		}

		if i < len(stat.SOC) && stat.SOC[i] != nil {
			socVal := *stat.SOC[i]
			// An exact hour-boundary reading (e.g. 01:00:00) marks both the start of the
			// new hour and the end of the previous hour. Include it in the previous hour's
			// bucket as well so MinBatterySOC/MaxBatterySOC cover the full hour.
			if intervalTime.Equal(hourStart) {
				prevHourStart := hourStart.Add(-time.Hour)
				if prevS, prevOK := hourlyStatsMap[prevHourStart]; prevOK {
					if !hourInitializedSOC[prevHourStart] {
						prevS.MinBatterySOC = socVal
						prevS.MaxBatterySOC = socVal
						hourInitializedSOC[prevHourStart] = true
					} else {
						if socVal < prevS.MinBatterySOC {
							prevS.MinBatterySOC = socVal
						}
						if socVal > prevS.MaxBatterySOC {
							prevS.MaxBatterySOC = socVal
						}
					}
				}
			}
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

		// Skip accumulating directional energy flows when an Enphase 15-minute bucket
		// contains corrupt values from a gateway/battery lifetime counter rollover.
		if hasCorruptEnphaseInterval(ctx, stat, i, loc) {
			continue
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
		TSDayStart:   dayStart,
		TimeLocation: loc.String(),
		Hourly:       hourly,
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
			dailyStats = append(dailyStats, parseDailyStats(ctx, stat, statDay, loc))
		}
	}

	// 2. Fetch today's data (if lastDayToFetch includes today)
	if !lastDayToFetch.Before(todayMidnight) {
		res, err := e.getToday(ctx, todayMidnight, false)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get enphase today data",
				slog.Time("date", todayMidnight),
				slog.Any("error", err),
			)
			return nil, err
		}

		if len(res.Stats) > 0 {
			dailyStats = append(dailyStats, parseDailyStats(ctx, res.Stats[0], todayMidnight, loc))
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

func (e *Enphase) getToday(ctx context.Context, date time.Time, includeNow bool) (enphaseTodayResponse, error) {
	dateStr := date.Format("2006-01-02")
	var rawRes enphaseTodayResponse
	if dateStr == e.todayCacheDate && time.Now().Before(e.todayCacheExpiry) {
		rawRes = e.todayCache
	} else {
		u := e.baseURL.JoinPath(fmt.Sprintf("pv/systems/%d/today", e.systemID))
		q := u.Query()
		q.Set("date", dateStr)
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return enphaseTodayResponse{}, err
		}

		if err := e.doRequest(req, &rawRes); err != nil {
			return enphaseTodayResponse{}, err
		}

		e.todayCache = rawRes
		e.todayCacheDate = dateStr
		e.todayCacheExpiry = time.Now().Add(time.Minute)
	}

	res := copyTodayResponse(rawRes)
	var lastReportTime time.Time
	if e.dataCache.LastReportDate > 0 {
		lastReportTime = time.Unix(e.dataCache.LastReportDate, 0)
	} else {
		lastReportTime = time.Now()
	}
	filterFutureIntervals(ctx, &res, lastReportTime, includeNow)

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

	var lastReportTime time.Time
	if e.dataCache.LastReportDate > 0 {
		lastReportTime = time.Unix(e.dataCache.LastReportDate, 0)
	} else {
		lastReportTime = time.Now()
	}
	filterFutureIntervals(ctx, &res, lastReportTime, false)

	return res, nil
}

func copyTodayResponse(res enphaseTodayResponse) enphaseTodayResponse {
	copied := res
	if res.Stats != nil {
		copied.Stats = make([]enphaseTodayStats, len(res.Stats))
		copy(copied.Stats, res.Stats)
	}
	return copied
}

func filterFutureIntervals(ctx context.Context, res *enphaseTodayResponse, lastReportTime time.Time, includeNow bool) {
	for i := range res.Stats {
		stat := &res.Stats[i]
		intervalSecs := stat.IntervalLength
		if intervalSecs == 0 {
			intervalSecs = 900
		}

		lastValidOtherIdx := -1
		lastValidSOCIdx := -1
		maxLen := max(len(stat.Consumption), len(stat.GridImport), len(stat.Production), len(stat.SOC))

		for idx := 0; idx < maxLen; idx++ {
			intervalStartTime := stat.StartTime + int64(idx*intervalSecs)
			if intervalStartTime > lastReportTime.Unix() {
				break
			}
			isCompletedInterval := intervalStartTime+int64(intervalSecs) <= lastReportTime.Unix()
			if idx < len(stat.SOC) && stat.SOC[idx] != nil {
				lastValidSOCIdx = idx
			} else if isCompletedInterval {
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

		if includeNow {
			for idx := validLen; idx < maxLen; idx++ {
				intervalStartTime := stat.StartTime + int64(idx*intervalSecs)
				if intervalStartTime > lastReportTime.Unix() {
					break
				}
				hasAnyData := (idx < len(stat.GridImport) && stat.GridImport[idx] > 0) ||
					(idx < len(stat.BatteryHome) && stat.BatteryHome[idx] > 0) ||
					(idx < len(stat.GridHome) && stat.GridHome[idx] > 0) ||
					(idx < len(stat.GridBattery) && stat.GridBattery[idx] > 0) ||
					(idx < len(stat.SolarHome) && stat.SolarHome[idx] > 0) ||
					(idx < len(stat.Production) && stat.Production[idx] > 0) ||
					(idx < len(stat.SOC) && stat.SOC[idx] != nil)
				if hasAnyData {
					validLen = idx + 1
				}
			}
		}

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
	if e.userID != 0 && (strings.Contains(req.URL.Path, "/service/batteryConfig/") || strings.Contains(req.URL.Path, "/service/tariff/")) {
		req.Header.Set("Username", fmt.Sprintf("%d", e.userID))
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	csrfToken := resp.Header.Get("X-Csrf-Token")
	if csrfToken != "" {
		if e.lastCSRFToken == nil {
			e.lastCSRFToken = make(map[string]enphaseCSRFToken)
		}
		e.lastCSRFToken[enphaseCSRFKey(req.URL.Path)] = enphaseCSRFToken{
			Token:     csrfToken,
			CreatedAt: time.Now(),
		}
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
	App            enphaseApp   `json:"app"`
	State          enphaseState `json:"state"`
	LastReportDate int64        `json:"last_report_date"`
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
	DTGControl                    *enphaseControlStatus  `json:"dtgControl,omitempty"`
	CFGControl                    *enphaseControlStatus  `json:"cfgControl,omitempty"`
	RBDControl                    *enphaseControlStatus  `json:"rbdControl,omitempty"`
	IsBatteryChangePending        bool                   `json:"isBatteryChangePending"`
	IsTariffTOU                   bool                   `json:"isTariffTOU"`
	BuybackExportPlan             string                 `json:"buybackExportPlan,omitempty"`
	OperationModeSubType          string                 `json:"operationModeSubType,omitempty"`
}

type enphaseBatteryProfileResponse struct {
	Profile                    string                 `json:"profile"`
	RequestedConfig            enphaseRequestedConfig `json:"requestedConfig"`
	BatteryBackupPercentage    int                    `json:"batteryBackupPercentage"`
	BatteryBackupPercentageMin int                    `json:"batteryBackupPercentageMin"`
	BatteryBackupPercentageMax int                    `json:"batteryBackupPercentageMax"`
	VeryLowSOC                 int                    `json:"veryLowSoc"`
	IsBatteryChangePending     bool                   `json:"isBatteryChangePending"`
	OperationModeSubType       string                 `json:"operationModeSubType,omitempty"`
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

type enphaseBatteryScheduleDetail struct {
	ScheduleID     string `json:"scheduleId,omitempty"`
	Timezone       string `json:"timezone"`
	StartTime      string `json:"startTime"`
	EndTime        string `json:"endTime"`
	Limit          int    `json:"limit"`
	ScheduleType   string `json:"scheduleType"`
	ScheduleStatus string `json:"scheduleStatus,omitempty"`
	Days           []int  `json:"days"`
	IsDeleted      bool   `json:"isDeleted,omitempty"`
	IsEnabled      bool   `json:"isEnabled,omitempty"`
}

type enphaseBatteryScheduleGroup struct {
	ScheduleStatus string                         `json:"scheduleStatus"`
	Count          int                            `json:"count"`
	Details        []enphaseBatteryScheduleDetail `json:"details"`
}

type enphaseBatterySchedulesResponse struct {
	Type               string                      `json:"type"`
	CFG                enphaseBatteryScheduleGroup `json:"cfg"`
	DTG                enphaseBatteryScheduleGroup `json:"dtg"`
	RBD                enphaseBatteryScheduleGroup `json:"rbd"`
	AnySchedulePending bool                        `json:"anySchedulePending"`
}

type enphaseTariffPeriod struct {
	ID          string `json:"id,omitempty"`
	StartTime   any    `json:"startTime"`
	EndTime     any    `json:"endTime"`
	Rate        any    `json:"rate"`
	Type        string `json:"type"`
	GenRateType string `json:"genRateType,omitempty"`
}

type enphaseTariffDaySchedule struct {
	ID      string                `json:"id,omitempty"`
	Days    []int                 `json:"days"`
	Periods []enphaseTariffPeriod `json:"periods"`
}

type enphaseTariffSeason struct {
	ID         string                     `json:"id,omitempty"`
	StartMonth string                     `json:"startMonth,omitempty"`
	EndMonth   string                     `json:"endMonth,omitempty"`
	Days       []enphaseTariffDaySchedule `json:"days"`
}

type enphaseTariffSection struct {
	TypeKind   string                `json:"typeKind,omitempty"`
	TypeID     string                `json:"typeId,omitempty"`
	Source     string                `json:"source,omitempty"`
	ExportPlan string                `json:"exportPlan,omitempty"`
	Seasons    []enphaseTariffSeason `json:"seasons"`
}

type enphaseTariffJSONResponse struct {
	SiteID   int                   `json:"site_id"`
	Currency string                `json:"currency"`
	Purchase *enphaseTariffSection `json:"purchase"`
	Buyback  *enphaseTariffSection `json:"buyback"`
}
