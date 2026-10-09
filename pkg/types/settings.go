package types

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
)

// CurrentSettingsVersion is the current version of the settings struct.
// Increment this value only if you need to set a default value other than the Go default for that value.
const CurrentSettingsVersion = 19

// Settings represents the configuration stored in the database.
// These are dynamic settings that can be changed without redeploying.
type Settings struct {
	DryRun bool `json:"dryRun"`
	// Pause updates
	Pause bool `json:"pause"`

	// PlanMode enables the 24-hour forward planning engine for this site.
	// TODO: Remove PlanMode once plan mode is rolled out to 100% of sites.
	PlanMode bool `json:"planMode,omitempty"`

	// What environment to opt into
	Release string `json:"release"`

	// Deprecated: IgnoreHourUsageOverMultiple is deprecated in favor of automatic zero-config EV and load spike filtering.
	IgnoreHourUsageOverMultiple float64 `json:"ignoreHourUsageOverMultiple"`
	// Deprecated: IgnoreHourUsageFloorKWH is deprecated in favor of automatic zero-config EV and load spike filtering.
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
	AlwaysChargeUnderDollarsPerKWH float64 `json:"alwaysChargeUnderDollarsPerKWH"`

	// How to value solar exports when net metering credits are active. Valid values: "", "lowest", "highest", "none". Default is "lowest".
	SolarNetMeteringCreditsValue string `json:"solarNetMeteringCreditsValue"`

	// The minimum battery SOC should be charged to at all times.
	MinBatterySOC float64 `json:"minBatterySOC"`

	// Optional variable minimum battery SOC periods (time-based or TOU period-name based).
	MinBatterySOCPeriods []MinBatterySOCPeriod `json:"minBatterySOCPeriods,omitempty"`

	// EVChargingStandby places the battery into standby when active Level 2 EV charging is
	// detected at night (20:00-07:00 local time) so the home battery does not discharge into the EV.
	// Real-time EV charging standby only operates at night (20:00-07:00) because EV charging can
	// only be reliably isolated from normal household/HVAC loads overnight, and during the day
	// using solar to charge the EV is assumed to be desired.
	EVChargingStandby bool `json:"evChargingStandby,omitempty"`

	// Deprecated: EVChargingPeriods is deprecated in favor of EVChargingStandby and automatic
	// nighttime EV charging detection.
	EVChargingPeriods []TimePeriod `json:"evChargingPeriods,omitempty"`

	// Grid Settings
	// CustomGridSettings indicates if the grid settings were manually configured.
	// When false, grid settings are automatically synchronized from the ESS.
	CustomGridSettings bool `json:"customGridSettings,omitempty"`
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
	// When enabled, RateRudder manages the ESS's Time-Of-Use (TOU) schedules and modes to enable direct solar export without pre-charging to 100%.
	ManageTOUSchedules bool `json:"manageTOUSchedules"`

	// Location settings
	CountryCode string `json:"countryCode"`
	PostalCode  string `json:"postalCode"`
	// Location is set by the weather package
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
	MinStartChargeMinutes int `json:"minStartChargeMinutes"`

	// Home load prediction strategy ("default", "conservative")
	HomeLoadPredictionStrategy string `json:"homeLoadPredictionStrategy"`

	// OptimizationProfile controls the risk profile, reserve safety buffers, and round-trip efficiency assumptions.
	// Valid values: "conservative" (higher reserve buffer), "balanced" (default), "aggressive".
	OptimizationProfile string `json:"optimizationProfile,omitempty"`

	// Notifications maps userID to notification preferences for this site.
	Notifications map[string]UserNotificationSettings `json:"notifications,omitempty"`

	// Deprecated
	SolarCapacityBufferMinutes int `json:"solarCapacityBufferMinutes"`

	// Deprecated: MinExportHoldDifferenceDollarsPerKWH is deprecated in favor of internal cycling hurdle and RTE. Retained for backwards compatibility in decide path.
	MinExportHoldDifferenceDollarsPerKWH float64 `json:"minExportHoldDifferenceDollarsPerKWH"`

	// Deprecated: VPPChargingBufferMinutes is deprecated in favor of OptimizationParams.VPPChargingBufferMinutes.
	VPPChargingBufferMinutes int `json:"vppChargingBufferMinutes"`

	// Deprecated: MinArbitrageDifferenceDollarsPerKWH is deprecated in favor of OptimizationParams.SolarExportDegradationDollarsPerKWH. Retained for backwards compatibility in decide path.
	MinArbitrageDifferenceDollarsPerKWH float64 `json:"minArbitrageDifferenceDollarsPerKWH"`

	// Deprecated: MinDeficitPriceDifferenceDollarsPerKWH is deprecated in favor of OptimizationParams.GridChargeDegradationDollarsPerKWH. Retained for backwards compatibility in decide path.
	MinDeficitPriceDifferenceDollarsPerKWH float64 `json:"minDeficitPriceDifferenceDollarsPerKWH"`

	// Deprecated: MinBatteryExportDifferenceDollarsPerKWH is deprecated in favor of OptimizationParams.BatteryExportDegradationDollarsPerKWH. Retained for backwards compatibility in decide path.
	MinBatteryExportDifferenceDollarsPerKWH float64 `json:"minBatteryExportDifferenceDollarsPerKWH"`
}

// GridSettings represents the ESS grid configuration capabilities.
type GridSettings struct {
	GridChargeBatteries bool `json:"gridChargeBatteries"`
	GridExportSolar     bool `json:"gridExportSolar"`
	GridExportBatteries bool `json:"gridExportBatteries"`
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
func MigrateSettings(s Settings, currentVersion int, release string) (Settings, bool, error) {
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
				s.Release = release
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
			// version 10: set default MinStartChargeMinutes
			if s.MinStartChargeMinutes == 0 {
				s.MinStartChargeMinutes = 5
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
			// version 13: split buffer settings
			if s.SolarCapacityBufferMinutes == 0 {
				s.SolarCapacityBufferMinutes = 10
			}
			if s.VPPChargingBufferMinutes == 0 {
				s.VPPChargingBufferMinutes = 20
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
		case 16:
			// version 16: Lock in existing grid settings as custom for already configured ESS sites
			if s.ESS != "" {
				s.CustomGridSettings = true
				migrated = true
			}
		case 17:
			// version 17: add default MinBatteryExportDifferenceDollarsPerKWH
			if s.MinBatteryExportDifferenceDollarsPerKWH == 0 {
				// Default is $0.07/kWh (~$1 profit per cycle on a 15 kWh battery).
				s.MinBatteryExportDifferenceDollarsPerKWH = 0.07
				migrated = true
			}
		case 18:
			// version 18: partition updateGroup into TOU (1..12) and ComEd (13..16) pools,
			// and ensure ineligible sites have updateGroup reset to 0.
			if !isEligibleForUpdate(s) {
				if s.UpdateGroup != 0 {
					s.UpdateGroup = 0
					migrated = true
				}
			} else {
				if isComEdHourly(s.UtilityProvider, s.UtilityRate) {
					if s.UpdateGroup < 13 || s.UpdateGroup > 16 {
						s.UpdateGroup = rand.IntN(4) + 13
						migrated = true
					}
				} else {
					if s.UpdateGroup < 1 || s.UpdateGroup > 12 {
						s.UpdateGroup = rand.IntN(12) + 1
						migrated = true
					}
				}
			}
		case 19:
			// version 19: migrate legacy buffer settings to OptimizationProfile if unset
			if s.OptimizationProfile == "" {
				if s.VPPChargingBufferMinutes >= 40 || s.SolarCapacityBufferMinutes >= 30 {
					s.OptimizationProfile = "conservative"
				} else if s.VPPChargingBufferMinutes > 0 && s.VPPChargingBufferMinutes <= 10 {
					s.OptimizationProfile = "aggressive"
				} else {
					s.OptimizationProfile = "balanced"
				}
				migrated = true
			}
		default:
			return s, false, fmt.Errorf("unknown settings version: %d", version)
		}
	}

	return s, migrated, nil
}

func isComEdHourly(provider, rate string) bool {
	return provider == "comed" && rate != "comed_bes" && rate != "comed_best"
}

func isEligibleForUpdate(s Settings) bool {
	return s.ESS != "" && s.UtilityProvider != "" && len(s.EncryptedCredentials) > 0
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

// OptimizationParams holds physical, risk, weather adjustment, and battery degradation parameters associated with an OptimizationProfile.
type OptimizationParams struct {
	RoundTripEfficiency                   float64 `json:"roundTripEfficiency"`
	ReserveBufferPercent                  float64 `json:"reserveBufferPercent"`
	PeakSurvivalBufferMinutes             int     `json:"peakSurvivalBufferMinutes"`
	SolarCapacityBufferMinutes            int     `json:"solarCapacityBufferMinutes"`
	VPPChargingBufferMinutes              int     `json:"vppChargingBufferMinutes"`
	CloudCoverDeratePercent               float64 `json:"cloudCoverDeratePercent"`
	GridChargeDegradationDollarsPerKWH    float64 `json:"gridChargeDegradationDollarsPerKWH"`
	SolarExportDegradationDollarsPerKWH   float64 `json:"solarExportDegradationDollarsPerKWH"`
	BatteryExportDegradationDollarsPerKWH float64 `json:"batteryExportDegradationDollarsPerKWH"`
}

// GetOptimizationParams returns the physical round-trip efficiency (η), active reserve buffer (%),
// cloud cover derating (%), and half-cycle battery degradation costs ($/kWh) associated with the configured OptimizationProfile.
// Note that GridChargeDegradationDollarsPerKWH ($0.02) and BatteryExportDegradationDollarsPerKWH ($0.05) are additive across
// a full grid-charge-to-battery-export cycle ($0.02 + $0.05 = $0.07/kWh total cycle hurdle in balanced/conservative,
// and $0.01 + $0.04 = $0.05/kWh in aggressive).
func (s Settings) GetOptimizationParams() OptimizationParams {
	switch strings.ToLower(s.OptimizationProfile) {
	case "conservative":
		return OptimizationParams{
			RoundTripEfficiency:                   0.85,
			ReserveBufferPercent:                  10.0,
			PeakSurvivalBufferMinutes:             30,
			SolarCapacityBufferMinutes:            20,
			VPPChargingBufferMinutes:              40,
			CloudCoverDeratePercent:               10.0,
			GridChargeDegradationDollarsPerKWH:    0.02,
			SolarExportDegradationDollarsPerKWH:   0.02,
			BatteryExportDegradationDollarsPerKWH: 0.05,
		}
	case "aggressive":
		return OptimizationParams{
			RoundTripEfficiency:                   0.92,
			ReserveBufferPercent:                  0.0,
			PeakSurvivalBufferMinutes:             0,
			SolarCapacityBufferMinutes:            0,
			VPPChargingBufferMinutes:              10,
			CloudCoverDeratePercent:               0.0,
			GridChargeDegradationDollarsPerKWH:    0.01,
			SolarExportDegradationDollarsPerKWH:   0.01,
			BatteryExportDegradationDollarsPerKWH: 0.04,
		}
	default:
		return OptimizationParams{
			RoundTripEfficiency:                   0.90,
			ReserveBufferPercent:                  0.0,
			PeakSurvivalBufferMinutes:             15,
			SolarCapacityBufferMinutes:            0,
			VPPChargingBufferMinutes:              20,
			CloudCoverDeratePercent:               5.0,
			GridChargeDegradationDollarsPerKWH:    0.02,
			SolarExportDegradationDollarsPerKWH:   0.02,
			BatteryExportDegradationDollarsPerKWH: 0.05,
		}
	}
}
