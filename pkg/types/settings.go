package types

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
)

// CurrentSettingsVersion is the current version of the settings struct.
// Increment this value only if you need to set a default value other than the Go default for that value.
const CurrentSettingsVersion = 15

// Settings represents the configuration stored in the database.
// These are dynamic settings that can be changed without redeploying.
type Settings struct {
	DryRun bool `json:"dryRun"`
	// Pause updates
	Pause bool `json:"pause"`

	// What environment to opt into
	Release string `json:"release"`

	// Power History Settings
	// What multiple over previous days to ignore when calculating power usage
	IgnoreHourUsageOverMultiple float64 `json:"ignoreHourUsageOverMultiple"`
	// The minimum hourly energy usage (in kWh) below which we do not filter outliers.
	IgnoreHourUsageFloorKWH float64 `json:"ignoreHourUsageFloorKWH"`

	// Utility Provider
	UtilityProvider    string              `json:"utilityProvider"`
	UtilityRate        string              `json:"utilityRate"`
	UtilityRateOptions UtilityRateOptions  `json:"utilityRateOptions"`
	UtilityFeesPeriods []UtilityFeesPeriod `json:"utilityFeesPeriods,omitempty"`

	// ESS Provider
	ESS string `json:"ess"`

	// Price Settings
	// Always charge when the price is under this amount (in $/kWh)
	AlwaysChargeUnderDollarsPerKWH         float64 `json:"alwaysChargeUnderDollarsPerKWH"`
	MinArbitrageDifferenceDollarsPerKWH    float64 `json:"minArbitrageDifferenceDollarsPerKWH"`
	MinDeficitPriceDifferenceDollarsPerKWH float64 `json:"minDeficitPriceDifferenceDollarsPerKWH"`
	MinExportHoldDifferenceDollarsPerKWH   float64 `json:"minExportHoldDifferenceDollarsPerKWH"`

	// How to value solar exports when net metering credits are active. Valid values: "", "lowest", "highest", "none". Default is "lowest".
	SolarNetMeteringCreditsValue string `json:"solarNetMeteringCreditsValue"`

	// The minimum battery SOC should be charged to at all times.
	MinBatterySOC float64 `json:"minBatterySOC"`

	// Optional variable minimum battery SOC periods (time-based or TOU period-name based).
	MinBatterySOCPeriods []MinBatterySOCPeriod `json:"minBatterySOCPeriods,omitempty"`

	// Optional EV charging periods where battery discharge is avoided.
	// If empty/nil, feature is disabled.
	EVChargingPeriods []TimePeriod `json:"evChargingPeriods,omitempty"`

	// Grid Settings
	// Maximum Grid Use (in kW) (not supported yet since we don't change limits)
	// MaxGridUseKW float64 `json:"maxGridUseKW"`
	// Can charge batteries from grid
	GridChargeBatteries bool `json:"gridChargeBatteries"`
	// Maximum Grid Export (in kW) (not supported yet since we don't change limits)
	//MaxGridExportKW float64 `json:"maxGridExportKW"`
	// Can export solar to grid
	GridExportSolar bool `json:"gridExportSolar"`
	// Can export batteries to grid
	GridExportBatteries bool `json:"gridExportBatteries"`

	// Location settings
	CountryCode  string  `json:"countryCode"`
	PostalCode   string  `json:"postalCode"`
	SolarAzimuth float64 `json:"solarAzimuth"`
	SolarTilt    float64 `json:"solarTilt"`
	// Location is set by the weather package and the solar azimuth/tilt are copied
	Location *SiteLocation `json:"location,omitempty"`

	// Solar Settings
	// Maximum ratio for solar trend adjustment (caps recentSolar/modelSolar).
	// Higher values allow more aggressive upward solar predictions.
	SolarTrendRatioMax float64 `json:"solarTrendRatioMax"`
	// Multiplier for bell curve solar smoothing weight.
	// 0 disables bell curve smoothing entirely. 1.0 = full weight.
	SolarBellCurveMultiplier float64 `json:"solarBellCurveMultiplier"`

	// Headroom for solar fully charging when export is disabled (in battery SOC %).
	// A value of 5 means we ensure we have 95% capacity.
	// A value of -5 means we hit capacity during the solar charging period.
	// Setting it to something like -100 will effectively disable the feature.
	SolarFullyChargeHeadroomBatterySOC float64 `json:"solarFullyChargeHeadroomBatterySOC"`

	// Credentials for external systems (encrypted)
	EncryptedCredentials []byte `json:"encryptedCredentials,omitempty"`

	// ESS Authentication Status
	ESSAuthStatus ESSAuthStatus `json:"essAuthStatus,omitempty"`

	// UpdateGroup controls which group the site gets updated in to spread out updates.
	UpdateGroup int `json:"updateGroup"`

	// Hysteresis & timing thresholds
	MinStartChargeMinutes      int     `json:"minStartChargeMinutes"`
	PeakSurvivalBufferMinutes  int     `json:"peakSurvivalBufferMinutes"`
	SOCBufferPercent           float64 `json:"socBufferPercent"`
	SolarCapacityBufferMinutes int     `json:"solarCapacityBufferMinutes"`
	VPPChargingBufferMinutes   int     `json:"vppChargingBufferMinutes"`

	// Home load prediction strategy ("default", "conservative")
	HomeLoadPredictionStrategy string `json:"homeLoadPredictionStrategy"`
}

// ESSAuthStatus represents the status of ESS authentication for the site.
type ESSAuthStatus struct {
	ConsecutiveFailures    int       `json:"consecutiveFailures,omitempty"`
	ConsecutiveSetFailures int       `json:"consecutiveSetFailures,omitempty"`
	LastAttempt            time.Time `json:"lastAttempt,omitempty"`
}

// Credentials for external systems
type Credentials struct {
	Franklin *FranklinCredentials `json:"franklin,omitempty"`
	Mock     *MockCredentials     `json:"mock,omitempty"`
	Tesla    *TeslaCredentials    `json:"tesla,omitempty"`
	Enphase  *EnphaseCredentials  `json:"enphase,omitempty"`
	// when a new field is added we need to make sure that handleGetSettings and
	// handleUpdateSettings are updated to handle the new field
}

// Has returns a map of credentials that are set
func (c *Credentials) Has() map[string]bool {
	return map[string]bool{
		"franklin": c.Franklin != nil,
		"mock":     c.Mock != nil,
		"tesla":    c.Tesla != nil,
		"enphase":  c.Enphase != nil,
	}
}

// Credentials for Tesla
type TeslaCredentials struct {
	AuthCode     string    `json:"authCode,omitempty"`
	Region       string    `json:"region,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	EnergySiteID int64     `json:"energySiteID,omitempty"`
	SerialNumber string    `json:"serialNumber,omitempty"`
}

// MockCredentials for simulated ESS
type MockCredentials struct {
	Strategy string `json:"strategy"`
	Location string `json:"location"`
}

// Credentials for Franklin
type FranklinCredentials struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	// Deprecated: MD5Password is kept for backward compatibility with existing credentials.
	MD5Password string `json:"md5Password,omitempty"`
	GatewayID   string `json:"gatewayID,omitempty"`
	// Token is the cached Franklin API session token. It is stored alongside
	// the other credentials so we can skip login on every update cycle and only
	// re-login when the token has expired (backend returns 401).
	Token string `json:"token,omitempty"`
}

// Credentials for Enphase
type EnphaseCredentials struct {
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`
	Code         string `json:"code,omitempty"`
	SessionID    string `json:"sessionID,omitempty"`
	ManagerToken string `json:"managerToken,omitempty"`
	SystemID     int    `json:"systemID,omitempty"`
	UserID       int    `json:"userID,omitempty"`
}

// MigrateSettings migrates the settings to the current version.
// It returns the migrated settings, a boolean indicating if changes were made, and an error if migration failed.
func MigrateSettings(s Settings, currentVersion int) (Settings, bool, error) {
	if currentVersion >= CurrentSettingsVersion {
		return s, false, nil
	}

	migrated := false
	// Loop through versions to apply migrations sequentially
	for version := currentVersion + 1; version <= CurrentSettingsVersion; version++ {
		switch version {
		case 1:
			// version 1: initial
			if s.IgnoreHourUsageOverMultiple == 0 {
				s.IgnoreHourUsageOverMultiple = 2
				migrated = true
			}
			if s.MinArbitrageDifferenceDollarsPerKWH == 0 {
				s.MinArbitrageDifferenceDollarsPerKWH = 0.03
				migrated = true
			}
			if s.MinBatterySOC == 0 {
				s.MinBatterySOC = 20.0
				migrated = true
			}
			// we don't want to assume they can charge from grid or export to grid
		case 2:
			// version 2: add MinDeficitPriceDifferenceDollarsPerKWH
			if s.MinDeficitPriceDifferenceDollarsPerKWH == 0 {
				s.MinDeficitPriceDifferenceDollarsPerKWH = 0.02
				migrated = true
			}
		case 3:
			// version 3: add solar trend ratio max and bell curve multiplier
			if s.SolarTrendRatioMax == 0 {
				s.SolarTrendRatioMax = 3.0
				migrated = true
			}
			if s.SolarBellCurveMultiplier == 0 {
				s.SolarBellCurveMultiplier = 1.0
				migrated = true
			}
		case 4:
			// version 4: add utility provider
			// we no longer default this
		case 5:
			// version 5: add additional fees schedule
			if s.UtilityProvider == "comed_hourly" {
				s.UtilityProvider = "comed_besh"
				migrated = true
			}
		case 6:
			if s.Release == "" {
				s.Release = "production"
				migrated = true
			}
		case 7:
			if s.UtilityProvider == "comed_besh" {
				s.UtilityProvider = "comed"
				s.UtilityRate = "comed_besh"
				migrated = true
			}
		case 8:
			// version 8: default ESS to "franklin" if we have credentials for franklin
			// Actually we don't have decrypted creds here, but we can check if EncryptedCredentials exist
			// because until now, Franklin was the only ESS supported
			if len(s.EncryptedCredentials) > 0 && s.ESS == "" {
				s.ESS = "franklin"
				migrated = true
			}
		case 9:
			// version 9: set UpdateGroup if it is unset (i.e. 0) if and only if both the ess is configured and a utility is configured.
			if s.UpdateGroup == 0 && s.ESS != "" && s.UtilityProvider != "" {
				s.UpdateGroup = rand.IntN(16) + 1
				migrated = true
			}
		case 10:
			// version 10: set default MinStartChargeMinutes and PeakSurvivalBufferMinutes
			if s.MinStartChargeMinutes == 0 {
				s.MinStartChargeMinutes = 5
				migrated = true
			}
			if s.PeakSurvivalBufferMinutes == 0 {
				s.PeakSurvivalBufferMinutes = 30
				migrated = true
			}
		case 11:
			// version 11: add IgnoreHourUsageFloorKWH default
			if s.IgnoreHourUsageFloorKWH == 0 {
				s.IgnoreHourUsageFloorKWH = 0.5
				migrated = true
			}
		case 12:
			// version 12: add default HomeLoadPredictionStrategy
			if s.HomeLoadPredictionStrategy == "" {
				s.HomeLoadPredictionStrategy = "default"
				migrated = true
			}
		case 13:
			// version 13: split buffer settings based on existing PeakSurvivalBufferMinutes
			existingBuffer := s.PeakSurvivalBufferMinutes
			if existingBuffer == 30 || existingBuffer == 0 {
				s.SOCBufferPercent = 4.0
				s.PeakSurvivalBufferMinutes = 20
				s.SolarCapacityBufferMinutes = 10
				s.VPPChargingBufferMinutes = 20
			} else if existingBuffer > 30 {
				s.SOCBufferPercent = 8.0
				s.PeakSurvivalBufferMinutes = 40
				s.SolarCapacityBufferMinutes = 30
				s.VPPChargingBufferMinutes = 40
			} else {
				// existingBuffer < 30
				s.SOCBufferPercent = 2.0
				s.PeakSurvivalBufferMinutes = 10
				s.SolarCapacityBufferMinutes = 0
				s.VPPChargingBufferMinutes = 10
			}
			migrated = true
		case 14:
			// version 14: bump version to write top-level release field to firestore settings doc
			migrated = true
		case 15:
			// version 15: add default MinExportHoldDifferenceDollarsPerKWH
			if s.MinExportHoldDifferenceDollarsPerKWH == 0 {
				s.MinExportHoldDifferenceDollarsPerKWH = 0.02
				migrated = true
			}
		default:
			return s, false, fmt.Errorf("unknown settings version: %d", version)
		}
	}

	return s, migrated, nil
}

// GetMinBatterySOC calculates the active minimum battery reserve SOC (%) for a given time, location, and price.
// It prioritizes period-name matching (if price has a PeriodName and a matching MinBatterySOCPeriod exists),
// then custom time schedule matching, falling back to settings.MinBatterySOC.
// If matching fails or mismatched periods are configured, an error is logged to ctx.
func (s Settings) GetMinBatterySOC(ctx context.Context, t time.Time, loc *time.Location, price Price) float64 {
	if len(s.MinBatterySOCPeriods) == 0 {
		return s.MinBatterySOC
	}

	if loc != nil {
		t = t.In(loc)
	} else if !price.TSStart.IsZero() && price.TSStart.Location() != nil {
		t = t.In(price.TSStart.Location())
	}

	hasNamedPeriods := false
	for _, p := range s.MinBatterySOCPeriods {
		if p.UtilityPeriodName != "" {
			hasNamedPeriods = true
			break
		}
	}

	if hasNamedPeriods {
		if price.PeriodName == "" {
			log.Ctx(ctx).ErrorContext(
				ctx,
				"min battery SOC schedule is name-based but given price has no period name",
				slog.Time("time", t),
				slog.Any("price", price),
				slog.Any("periods", s.MinBatterySOCPeriods),
			)
			return s.MinBatterySOC
		}
		for _, p := range s.MinBatterySOCPeriods {
			if p.UtilityPeriodName != "" && p.UtilityPeriodName == price.PeriodName {
				return p.MinBatterySOC
			}
		}
		log.Ctx(ctx).ErrorContext(
			ctx,
			"no min battery SOC period found matching utility period name",
			slog.Any("price", price),
			slog.Time("time", t),
			slog.Any("periods", s.MinBatterySOCPeriods),
		)
		return s.MinBatterySOC
	}

	// Time-based schedule matching
	for _, p := range s.MinBatterySOCPeriods {
		if p.UtilityPeriodName == "" {
			if ok, _, _ := p.Contains(t); ok {
				return p.MinBatterySOC
			}
		}
	}

	log.Ctx(ctx).ErrorContext(
		ctx,
		"no min battery SOC period found matching time",
		slog.Time("time", t),
		slog.Any("periods", s.MinBatterySOCPeriods),
	)
	return s.MinBatterySOC
}
