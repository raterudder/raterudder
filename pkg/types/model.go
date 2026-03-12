package types

import "time"

const (
	CurrentEnergyStatsVersion  = 1
	CurrentPriceHistoryVersion = 1
	CurrentWeatherVersion      = 1

	SiteIDNone = "none"
)

// Site represents a household or location that has a battery and solar panels.
type Site struct {
	ID          string            `json:"id"`
	InviteCode  string            `json:"inviteCode"`
	Permissions []SitePermissions `json:"permissions"`
}

// SitePermissions represents the permissions for a user on a site.
type SitePermissions struct {
	UserID string `json:"userID"`
}

// UserSite represents a site on a user
type UserSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User represents a user of the system.
type User struct {
	ID    string     `json:"id"`
	Email string     `json:"email"`
	Sites []UserSite `json:"sites"`
	Admin bool       `json:"-"`
}

// ActionReason represents the type of action taken by the system.
type ActionReason string

const (
	ActionReasonAlwaysChargeBelowThreshold ActionReason = "alwaysChargeBelowThreshold"
	ActionReasonMissingBattery             ActionReason = "missingBattery"
	ActionReasonDeficitChargeNow           ActionReason = "deficitCharge"
	ActionReasonArbitrageChargeNow         ActionReason = "arbitrageCharge"
	ActionReasonDischargeBeforeCapacityNow ActionReason = "dischargeBeforeCapacity"
	ActionReasonDeficitSaveForPeak         ActionReason = "deficitSaveForPeak"
	ActionReasonArbitrageSave              ActionReason = "dischargeAtPeak"
	ActionReasonSufficientBattery          ActionReason = "sufficientBattery"
	ActionReasonEmergencyMode              ActionReason = "emergencyMode"
	ActionReasonHasAlarms                  ActionReason = "hasAlarms"
	ActionReasonWaitingToCharge            ActionReason = "waitingToCharge"
	ActionReasonChargeSurvivePeak          ActionReason = "chargeSurvivePeak"
	ActionReasonPreventSolarCurtailment    ActionReason = "preventSolarCurtailment"
)

// Action represents a control decision made by the system.
type Action struct {
	Timestamp         time.Time    `json:"timestamp"`
	BatteryMode       BatteryMode  `json:"batteryMode"`
	SolarMode         SolarMode    `json:"solarMode"`
	TargetBatteryMode BatteryMode  `json:"targetBatteryMode"`
	TargetSolarMode   SolarMode    `json:"targetSolarMode"`
	Reason            ActionReason `json:"reason"`
	Description       string       `json:"description"`
	CurrentPrice      *Price       `json:"currentPrice,omitempty"`
	FuturePrice       *Price       `json:"futurePrice,omitempty"`
	SystemStatus      SystemStatus `json:"systemStatus"`
	HitDeficitAt      time.Time    `json:"deficitAt"`
	HitCapacityAt     time.Time    `json:"capacityAt"`
	DryRun            bool         `json:"dryRun,omitempty"`
	Fault             bool         `json:"fault,omitempty"`
	Failed            bool         `json:"failed,omitempty"`
	Paused            bool         `json:"paused,omitempty"`
	Error             string       `json:"error,omitempty"`
}

// EnergyStats represents aggregated energy statistics for an hourly period.
type EnergyStats struct {
	TSHourStart time.Time `json:"tsHourStart"`

	// Battery Stats
	MinBatterySOC float64 `json:"minBatterySOC"`
	MaxBatterySOC float64 `json:"maxBatterySOC"`

	// Totals
	BatteryChargedKWH float64 `json:"batteryChargedKWH"`
	BatteryUsedKWH    float64 `json:"batteryUsedKWH"`
	SolarKWH          float64 `json:"solarKWH"`
	HomeKWH           float64 `json:"homeKWH"`
	GridExportKWH     float64 `json:"gridExportKWH"`
	GridImportKWH     float64 `json:"gridImportKWH"`

	// Source to destination
	BatteryToHomeKWH  float64 `json:"batteryToHomeKWH"`
	SolarToHomeKWH    float64 `json:"solarToHomeKWH"`
	SolarToBatteryKWH float64 `json:"solarToBatteryKWH"`
	SolarToGridKWH    float64 `json:"solarToGridKWH"`
	BatteryToGridKWH  float64 `json:"batteryToGridKWH"`

	// Miscellaneous
	Alarms []SystemAlarm `json:"alarms,omitempty"`
}

// SystemAlarm represents a single alarm condition.
type SystemAlarm struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	Code        string    `json:"code"`
}

// Storm represents a storm warning.
type Storm struct {
	Description string    `json:"description"`
	TSStart     time.Time `json:"tsStart"`
	TSEnd       time.Time `json:"tsEnd"`
}

// SystemStatus represents the current system status.
type SystemStatus struct {
	Timestamp               time.Time     `json:"timestamp"`
	BatterySOC              float64       `json:"batterySOC"`            // 0-100
	BatteryKW               float64       `json:"batteryKW"`             // Positive for discharge, negative for charge
	BatteryCapacityKWH      float64       `json:"batteryCapacityKWH"`    // Total capacity of the battery (kWh)
	MaxBatteryChargeKW      float64       `json:"maxBatteryChargeKW"`    // Maximum charge rate of the battery (kW)
	MaxBatteryDischargeKW   float64       `json:"maxBatteryDischargeKW"` // Maximum discharge rate of the battery (kW)
	SolarKW                 float64       `json:"solarKW"`               // Solar generation (kW)
	GridKW                  float64       `json:"gridKW"`                // Grid import/export (kW, + import, - export)
	HomeKW                  float64       `json:"homeKW"`                // Home consumption (kW)
	CanExportSolar          bool          `json:"canExportSolar"`        // True if solar exporting is enabled
	CanExportBattery        bool          `json:"canExportBattery"`      // True if battery exporting is enabled
	CanImportBattery        bool          `json:"canImportBattery"`      // True if battery importing is enabled
	ElevatedMinBatterySOC   bool          `json:"elevatedMinBatterySOC"` // True if the minimum SOC is elevated to force standby
	BatteryAboveMinSOC      bool          `json:"batteryAboveMinSOC"`    // True if the battery SOC is above the minimum SOC
	EmergencyMode           bool          `json:"emergencyMode"`
	BatteryChargingDisabled bool          `json:"batteryChargingDisabled"` // True if battery charging is disabled due to alarms
	Alarms                  []SystemAlarm `json:"alarms"`
	Storms                  []Storm       `json:"storms"`
}

// BatteryMode represents the mode of the battery.
type BatteryMode int

const (
	BatteryModeNoChange    BatteryMode = 0
	BatteryModeStandby     BatteryMode = 1
	BatteryModeChargeAny   BatteryMode = 2
	BatteryModeChargeSolar BatteryMode = 3
	BatteryModeLoad        BatteryMode = -1
)

// SolarMode represents the mode of the solar panels.
type SolarMode int

const (
	SolarModeNoChange SolarMode = 0
	SolarModeNoExport SolarMode = 1
	SolarModeAny      SolarMode = 2
	// TODO: we could add SolarModeExportOnly SolarMode = 2 but right now no ESS system supports this mode
)

// Feedback represents feedback submitted by a user.
type Feedback struct {
	ID        string            `json:"id"`
	Sentiment string            `json:"sentiment"`
	Comment   string            `json:"comment"`
	SiteID    string            `json:"siteID"`
	UserID    string            `json:"userID"`
	Extra     map[string]string `json:"extra"`
	Timestamp time.Time         `json:"timestamp"`
}

// SiteLocation represents the geographical location of a site.
type SiteLocation struct {
	PostalCode  string  `json:"postalCode"`
	Lat         float64 `json:"lat"`
	Long        float64 `json:"long"`
	City        string  `json:"city"`
	CountryCode string  `json:"countryCode"`
	TimeZone    string  `json:"timeZone"`
	Elevation   float64 `json:"elevation"`
}

// HourlyWeather represents the solar generation data for a specific hour.
type HourlyWeather struct {
	TSHourStart time.Time `json:"tsHourStart"`

	GHI      float64 `json:"ghi"`      // Shortwave radiation in W/m²
	SolarKWH float64 `json:"solarKWH"` // Actual solar generation in kWh
}

// Weather represents the daily weather and solar data.
type Weather struct {
	TSDayStart time.Time `json:"tsDayStart"`
	// ActualHours contains the actual solar generation and GHI data for hours that have occurred.
	// The slice is unsorted. Use TSHourStart to determine the hour instead of index.
	ActualHours []HourlyWeather `json:"actualHours"`
	// ForecastHours contains the forecasted GHI data for hours that have not yet occurred or
	// for which actuals are not yet available. The slice is unsorted. Use TSHourStart to determine the hour.
	ForecastHours []HourlyWeather `json:"forecastHours"`
	TimeLocation  string          `json:"timeLocation"`
	Lat           float64         `json:"lat"`
	Long          float64         `json:"long"`
	TSSunrise     time.Time       `json:"tsSunrise"`
	TSSunset      time.Time       `json:"tsSunset"`
	TSUpdated     time.Time       `json:"tsUpdated"`
}
