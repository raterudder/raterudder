package types

import (
	"fmt"
	"time"
)

// CurrentSettingsVersion is the current version of the settings struct.
// Increment this value only if you need to set a default value other than the Go default for that value.
const CurrentSettingsVersion = 8

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

	// Utility Provider
	UtilityProvider    string             `json:"utilityProvider"`
	UtilityRate        string             `json:"utilityRate"`
	UtilityRateOptions UtilityRateOptions `json:"utilityRateOptions"`

	// ESS Provider
	ESS string `json:"ess"`

	// Price Settings
	// Always charge when the price is under this amount (in $/kWh)
	AlwaysChargeUnderDollarsPerKWH         float64                       `json:"alwaysChargeUnderDollarsPerKWH"`
	MinArbitrageDifferenceDollarsPerKWH    float64                       `json:"minArbitrageDifferenceDollarsPerKWH"`
	MinDeficitPriceDifferenceDollarsPerKWH float64                       `json:"minDeficitPriceDifferenceDollarsPerKWH"`
	AdditionalFeesPeriods                  []UtilityAdditionalFeesPeriod `json:"additionalFeesPeriods"`

	// How to value solar exports when net metering credits are active. Valid values: "", "lowest", "highest", "none". Default is "lowest".
	SolarNetMeteringCreditsValue string `json:"solarNetMeteringCreditsValue"`

	// The minimum battery SOC should be charged to at all times.
	MinBatterySOC float64 `json:"minBatterySOC"`

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

	CountryCode string        `json:"countryCode"`
	PostalCode  string        `json:"postalCode"`
	Location    *SiteLocation `json:"location,omitempty"`

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
}

// ESSAuthStatus represents the status of ESS authentication for the site.
type ESSAuthStatus struct {
	ConsecutiveFailures int       `json:"consecutiveFailures,omitempty"`
	LastAttempt         time.Time `json:"lastAttempt,omitempty"`
}

// Credentials for external systems
type Credentials struct {
	Franklin *FranklinCredentials `json:"franklin,omitempty"`
	Mock     *MockCredentials     `json:"mock,omitempty"`
	Tesla    *TeslaCredentials    `json:"tesla,omitempty"`
	// when a new field is added we need to make sure that handleGetSettings and
	// handleUpdateSettings are updated to handle the new field
}

// Has returns a map of credentials that are set
func (c *Credentials) Has() map[string]bool {
	return map[string]bool{
		"franklin": c.Franklin != nil,
		"mock":     c.Mock != nil,
		"tesla":    c.Tesla != nil,
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
		default:
			return s, false, fmt.Errorf("unknown settings version: %d", version)
		}
	}

	return s, migrated, nil
}
