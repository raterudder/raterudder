package types

import "time"

const (
	CurrentEnergyStatsVersion  = 3
	CurrentPriceHistoryVersion = 2
	CurrentWeatherVersion      = 4 // Note: Version 5 was burned in staging/development and should not be reused

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
	ID            string             `json:"id"`
	Email         string             `json:"email"`
	Sites         []UserSite         `json:"sites"`
	Admin         bool               `json:"-"`
	SessionSecret string             `json:"sessionSecret,omitempty"`
	Subscriptions []PushSubscription `json:"subscriptions,omitempty"`
}

// ActionReason represents the type of action taken by the system.
type ActionReason string

const (
	ActionReasonAlwaysChargeBelowThreshold  ActionReason = "alwaysChargeBelowThreshold"
	ActionReasonMissingBattery              ActionReason = "missingBattery"
	ActionReasonDeficitChargeNow            ActionReason = "deficitCharge"
	ActionReasonArbitrageChargeExport       ActionReason = "arbitrageChargeExport"
	ActionReasonDeficitSaveForPeak          ActionReason = "deficitSaveForPeak"
	ActionReasonDischargeAtPeak             ActionReason = "dischargeAtPeak"
	ActionReasonArbitrageHoldExport         ActionReason = "arbitrageHoldExport"
	ActionReasonSufficientBattery           ActionReason = "sufficientBattery"
	ActionReasonSufficientBatteryTillCharge ActionReason = "sufficientBatteryTillCharge"
	ActionReasonEmergencyMode               ActionReason = "emergencyMode"
	ActionReasonHasAlarms                   ActionReason = "hasAlarms"
	ActionReasonGridUnavailable             ActionReason = "gridUnavailable"
	ActionReasonWaitingToCharge             ActionReason = "waitingToCharge"
	ActionReasonPreventSolarCurtailment     ActionReason = "preventSolarCurtailment"
	ActionReasonHoldSimilarPrice            ActionReason = "holdSimilarPrice"
	ActionReasonBatteryAtReserve            ActionReason = "batteryAtReserve"
	ActionReasonVPPActive                   ActionReason = "vppActive"
	ActionReasonVPPPrep                     ActionReason = "vppPrep"
	ActionReasonEVChargingStandby           ActionReason = "evChargingStandby"
	ActionReasonDirectExport                ActionReason = "directExport"

	// Deprecated - we don't use these anymore but we don't delete them so we know they were used
	ActionReasonDischargeBeforeCapacityNow ActionReason = "dischargeBeforeCapacity"
	ActionReasonArbitrageHoldSave          ActionReason = "arbitrageHoldSave"
	ActionReasonArbitrageChargeSave        ActionReason = "arbitrageChargeSave"
	ActionReasonArbitrageChargeNow         ActionReason = "arbitrageCharge"
	ActionReasonArbitrageHold              ActionReason = "arbitrageHold"
	ActionReasonDeficitSave                ActionReason = "deficitSave"
)

// ModesOptions contains options for setting the operating modes.
type ModesOptions struct {
	ChargeToSOC         int       `json:"chargeToSoc,omitempty"`
	MinimumSOC          int       `json:"minimumSoc,omitempty"`
	TSScheduleModeUntil time.Time `json:"tsScheduleModeUntil,omitempty"`
	CurrentPrice        Price     `json:"currentPrice,omitempty"`
}

// Action represents a control decision made by the system.
type Action struct {
	Timestamp               time.Time        `json:"timestamp"`
	SystemTimestamp         time.Time        `json:"systemTimestamp,omitempty"`
	BatteryMode             BatteryMode      `json:"batteryMode"`
	SolarMode               SolarMode        `json:"solarMode"`
	ChargeToSOC             int              `json:"chargeToSoc,omitempty"`
	Reason                  ActionReason     `json:"reason"`
	Description             string           `json:"description"`
	CurrentPrice            *Price           `json:"currentPrice,omitempty"`
	FuturePrice             *Price           `json:"futurePrice,omitempty"`
	SystemStatus            SystemStatus     `json:"systemStatus"`
	HitDeficitAt            time.Time        `json:"deficitAt"`
	HitCapacityAt           time.Time        `json:"capacityAt"`
	TSScheduleModeUntil     time.Time        `json:"tsScheduleModeUntil,omitempty"`
	StrategyBenefitDollars  float64          `json:"strategyBenefitDollars,omitempty"`
	DryRun                  bool             `json:"dryRun,omitempty"`
	Fault                   bool             `json:"fault,omitempty"`
	Failed                  bool             `json:"failed,omitempty"`
	Paused                  bool             `json:"paused,omitempty"`
	Error                   string           `json:"error,omitempty"`
	SimulationParams        SimulationParams `json:"simulationParams,omitempty"`
	Plan                    *Plan            `json:"plan,omitempty"`
	RecentHomeUsageKWH      float64          `json:"recentHomeUsageKWH,omitempty"`
	Q3HomeUsageKWH          float64          `json:"q3HomeUsageKWH,omitempty"`
	RecentHomeUsageAbnormal bool             `json:"recentHomeUsageAbnormal,omitempty"`

	// Deprecated: use HitDeficitAt
	HitBufferedDeficitAt time.Time `json:"hitBufferedDeficitAt,omitempty"`
	// Deprecated: use HitDeficitAt
	HitThresholdDeficitAt time.Time `json:"hitThresholdDeficitAt,omitempty"`
	// Deprecated: use BatteryMode
	TargetBatteryMode BatteryMode `json:"targetBatteryMode,omitempty"`
	// Deprecated: use SolarMode
	TargetSolarMode SolarMode `json:"targetSolarMode,omitempty"`
}

// SimulationParams holds calculated or calibrated solar parameters from
// the weather and history simulation run.
type SimulationParams struct {
	// ClippingCapKWH is the learned solar generation limit in kWh.
	ClippingCapKWH float64 `json:"clippingCapKWH"`
	// PanelAzimuth is the detected/calibrated best panel azimuth direction.
	PanelAzimuth float64 `json:"panelAzimuth"`
	// PanelTilt is the detected/calibrated best panel tilt angle.
	PanelTilt float64 `json:"panelTilt"`
	// AverageSolarEfficiency is the average efficiency calculated across all non-zero generation hours.
	AverageSolarEfficiency float64 `json:"averageSolarEfficiency"`
	// DetectedShift represents any detected structural load shift ("none", "down", "up").
	DetectedShift string `json:"detectedShift,omitempty"`
}

// EnergyStats represents aggregated energy statistics for an hourly period.
type EnergyStats struct {
	TSHourStart  time.Time `json:"tsHourStart"`
	TimeLocation string    `json:"timeLocation,omitempty"`

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
	VPPExportKWH      float64 `json:"vppExportKWH,omitempty"`

	// Source to destination
	BatteryToHomeKWH  float64 `json:"batteryToHomeKWH"`
	SolarToHomeKWH    float64 `json:"solarToHomeKWH"`
	SolarToBatteryKWH float64 `json:"solarToBatteryKWH"`
	SolarToGridKWH    float64 `json:"solarToGridKWH"`
	BatteryToGridKWH  float64 `json:"batteryToGridKWH"`

	// Miscellaneous
	Alarms []SystemAlarm `json:"alarms,omitempty"`
}

// DailyEnergyStats represents aggregated energy statistics for a day.
type DailyEnergyStats struct {
	TSDayStart   time.Time     `json:"tsDayStart"`
	TimeLocation string        `json:"timeLocation,omitempty"`
	Hourly       []EnergyStats `json:"hourly"`
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

// VPPEvent represents a virtual power plant event.
type VPPEvent struct {
	Description   string    `json:"description"`
	TSStart       time.Time `json:"tsStart"`
	TSEnd         time.Time `json:"tsEnd"`
	VPPSoc        float64   `json:"vppSoc"`
	OptOut        bool      `json:"optOut"`
	Mandatory     bool      `json:"mandatory"`
	DollarsPerKWH float64   `json:"dollarsPerKWH,omitempty"`
}

// SystemStatus represents the current system status.
type SystemStatus struct {
	Timestamp               time.Time     `json:"timestamp"`
	TimeLocation            string        `json:"timeLocation,omitempty"`
	BatterySOC              float64       `json:"batterySOC"`            // 0-100
	BatteryKW               float64       `json:"batteryKW"`             // Positive for discharge, negative for charge
	BatteryCapacityKWH      float64       `json:"batteryCapacityKWH"`    // Total capacity of the battery (kWh)
	MaxBatteryChargeKW      float64       `json:"maxBatteryChargeKW"`    // Maximum charge rate of the battery (kW)
	MaxBatteryDischargeKW   float64       `json:"maxBatteryDischargeKW"` // Maximum discharge rate of the battery (kW)
	SolarKW                 float64       `json:"solarKW"`               // Solar generation (kW)
	GridKW                  float64       `json:"gridKW"`                // Grid import/export (kW, + import, - export)
	HomeKW                  float64       `json:"homeKW"`                // Home consumption (kW)
	ElevatedMinBatterySOC   bool          `json:"elevatedMinBatterySOC"` // True if the minimum SOC is elevated to force standby
	BatteryAboveMinSOC      bool          `json:"batteryAboveMinSOC"`    // True if the battery SOC is above the minimum SOC
	EmergencyMode           bool          `json:"emergencyMode,omitempty"`
	GridUnavailable         bool          `json:"gridUnavailable,omitempty"`         // True if the grid is unavailable
	BatteryChargingDisabled bool          `json:"batteryChargingDisabled,omitempty"` // True if battery charging is disabled due to alarms
	Alarms                  []SystemAlarm `json:"alarms,omitempty"`
	Storms                  []Storm       `json:"storms,omitempty"`
	VPPActive               bool          `json:"vppActive,omitempty"` // True if VPP is currently controlling
	VPPKW                   float64       `json:"vppKW,omitempty"`     // Power going to/from the VPP
	VPPSOC                  float64       `json:"vppSOC,omitempty"`    // VPP target SOC
	VPPEvents               []VPPEvent    `json:"vppEvents,omitempty"`
	ManagedTOUMode          bool          `json:"managedTouMode,omitempty"` // True if battery is operating in RateRudder-managed Time-of-Use mode
}

// BatteryMode represents the mode of the battery.
type BatteryMode int

const (
	BatteryModeNoChange  BatteryMode = 0
	BatteryModeStandby   BatteryMode = 1
	BatteryModeChargeAny BatteryMode = 2
	BatteryModeLoad      BatteryMode = -1
	BatteryModeExport    BatteryMode = -2
)

// SolarMode represents the mode of the solar panels.
type SolarMode int

const (
	SolarModeNoChange SolarMode = 0
	SolarModeNoExport SolarMode = 1
	SolarModeAny      SolarMode = 2
	SolarModeExport   SolarMode = 3
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
	PostalCode   string  `json:"postalCode"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	City         string  `json:"city"`
	CountryCode  string  `json:"countryCode"`
	TimeZone     string  `json:"timeZone"`
	Elevation    float64 `json:"elevation"`
	SolarAzimuth float64 `json:"solarAzimuth"`
	SolarTilt    float64 `json:"solarTilt"`
}

// HourlyWeather represents the solar forecast data for a specific hour.
type HourlyWeather struct {
	TSHourStart       time.Time `json:"tsHourStart"`
	GHI               float64   `json:"ghi,omitempty"`               // Shortwave radiation in W/m²
	GTI               float64   `json:"gti,omitempty"`               // Global tilted irradiance in W/m²
	DHI               float64   `json:"dhi,omitempty"`               // Diffuse radiation in W/m²
	DNI               float64   `json:"dni,omitempty"`               // Direct normal irradiance in W/m²
	TemperatureC      float64   `json:"temperatureC,omitempty"`      // Temperature in °C
	SnowfallCM        float64   `json:"snowfallCM,omitempty"`        // Snowfall in cm
	SnowDepthCM       float64   `json:"snowDepthCM,omitempty"`       // Snow depth in cm
	CloudCoverPercent float64   `json:"cloudCoverPercent,omitempty"` // Cloud cover
}

// Weather represents the daily weather forecast data.
type Weather struct {
	TSDayStart    time.Time       `json:"tsDayStart"`
	TimeLocation  string          `json:"timeLocation"`
	Latitude      float64         `json:"latitude"`
	Longitude     float64         `json:"longitude"`
	TSSunrise     time.Time       `json:"tsSunrise"`
	TSSunset      time.Time       `json:"tsSunset"`
	TSUpdated     time.Time       `json:"tsUpdated"`
	ForecastHours []HourlyWeather `json:"forecastHours"`
}

// HistorySummary represents the monthly history data stored in the history_summary Firestore collection.
type HistorySummary struct {
	TSMonthStart time.Time          `json:"tsMonthStart"`
	Energy       []DailyEnergyStats `json:"energy"`
	Weather      []Weather          `json:"weather"`
}

// EVSession represents a detected EV charging session.
type EVSession struct {
	TSStartHour time.Time `json:"tsStartHour"`
	TSEndHour   time.Time `json:"tsEndHour"`
	DurationHr  int       `json:"durationHr"`
	PeakKW      float64   `json:"peakKW"`
	AvgKW       float64   `json:"avgKW"`
	TotalKWH    float64   `json:"totalKWH"`
	NetStepKW   float64   `json:"netStepKW"`
}

// EVDetectionResult contains the output of EV charging estimation across history.
type EVDetectionResult struct {
	Detected           bool         `json:"detected"`
	RecommendedPeriod  TimePeriod   `json:"recommendedPeriod"`
	AllDetectedPeriods []TimePeriod `json:"allDetectedPeriods,omitempty"`
	EstimatedRateKW    float64      `json:"estimatedRateKW"`
	SessionsCount      int          `json:"sessionsCount"`
	Sessions           []EVSession  `json:"sessions,omitempty"`
	Message            string       `json:"message,omitempty"`
}

// PushSubscriptionKeys holds the client P256DH and Auth secrets from PushSubscription.toJSON().
type PushSubscriptionKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// PushSubscription represents a single subscribed browser/device target.
type PushSubscription struct {
	ID        string               `json:"id"`
	Endpoint  string               `json:"endpoint"`
	Keys      PushSubscriptionKeys `json:"keys"`
	UserAgent string               `json:"userAgent,omitempty"`
	TSCreated time.Time            `json:"tsCreated"`
}

// Notification types sent by the push notification system.
const (
	NotificationTypeMorningSummary       = "morning_summary"
	NotificationTypeEveningSummary       = "evening_summary"
	NotificationTypeGridOutage           = "grid_outage"
	NotificationTypeGridRestored         = "grid_restored"
	NotificationTypePriceSpike           = "price_spike"
	NotificationTypeSolarUnderproduction = "solar_underproduction"
	NotificationTypeVPPDispatch          = "vpp_dispatch"
	NotificationTypeHighHomeLoad         = "high_home_load"
)

// UserNotificationSettings holds notification preferences for a specific user on a specific site.
type UserNotificationSettings struct {
	AllAlertSensitivity string `json:"allAlertSensitivity,omitempty"` // "low", "medium", "high", "disabled", or "" for custom/individual alert settings

	MorningSummaryEnabled bool   `json:"morningSummaryEnabled"`
	MorningSummaryHour    int    `json:"morningSummaryHour"`
	MorningSummaryFlavor  string `json:"morningSummaryFlavor"`

	EveningSummaryEnabled bool   `json:"eveningSummaryEnabled"`
	EveningSummaryHour    int    `json:"eveningSummaryHour"`
	EveningSummaryFlavor  string `json:"eveningSummaryFlavor"`

	GridOutageAlert           bool         `json:"gridOutageAlert"`
	PriceSpikeAlert           string       `json:"priceSpikeAlert,omitempty"`           // "", "low", "medium", "high"
	SolarUnderproductionAlert string       `json:"solarUnderproductionAlert,omitempty"` // "", "low", "medium", "high"
	HighHomeLoadAlert         string       `json:"highHomeLoadAlert,omitempty"`         // "", "low", "medium", "high"
	VPPDispatchAlert          bool         `json:"vppDispatchAlert"`
	QuietPeriods              []TimePeriod `json:"quietPeriods,omitempty"`
}

// RealTimeAlertSensitivity returns the effective sensitivity ("low", "medium", "high", or "" if disabled)
// for a sensitivity-based alert type. When AllAlertSensitivity is set, it overrides customVal.
func (s UserNotificationSettings) RealTimeAlertSensitivity(customVal string) string {
	if s.AllAlertSensitivity == "disabled" {
		return ""
	}
	if s.AllAlertSensitivity != "" {
		return s.AllAlertSensitivity
	}
	return customVal
}

// RealTimeAlertEnabled returns whether a boolean alert type is enabled. When AllAlertSensitivity is set,
// all real-time alerts are enabled (or disabled if AllAlertSensitivity == "disabled").
func (s UserNotificationSettings) RealTimeAlertEnabled(customVal bool) bool {
	if s.AllAlertSensitivity == "disabled" {
		return false
	}
	if s.AllAlertSensitivity != "" {
		return true
	}
	return customVal
}

// IsInQuietPeriod returns true if any period in QuietPeriods contains the given time t.
func (s UserNotificationSettings) IsInQuietPeriod(t time.Time) bool {
	for i := range s.QuietPeriods {
		if contains, _, _ := s.QuietPeriods[i].Contains(t); contains {
			return true
		}
	}
	return false
}

// NotificationLog records a sent push notification for debugging and click analysis.
type NotificationLog struct {
	ID         string            `json:"id"`
	TSCreated  time.Time         `json:"tsCreated"`
	UserID     string            `json:"userID"`
	Type       string            `json:"type"`
	Flavor     string            `json:"flavor"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	Success    bool              `json:"success"`
	Muted      bool              `json:"muted,omitempty"`
	StatusCode int               `json:"statusCode"`
	Error      string            `json:"error,omitempty"`
	Clicked    bool              `json:"clicked,omitempty"`
	TSClicked  time.Time         `json:"tsClicked,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// MonthlyNotificationLogs groups notifications by month in UTC.
type MonthlyNotificationLogs struct {
	TSMonthStart time.Time         `json:"tsMonthStart"`
	Logs         []NotificationLog `json:"logs"`
}

// Plan represents a forward-looking optimal schedule over the planning horizon.
type Plan struct {
	TSCreated          time.Time    `json:"tsCreated"`
	HorizonHours       int          `json:"horizonHours"`
	TotalProjectedCost float64      `json:"totalProjectedCost,omitempty"`
	TotalExportCredits float64      `json:"totalExportCredits,omitempty"`
	NetEconomicBenefit float64      `json:"netEconomicBenefit,omitempty"`
	Periods            []PlanPeriod `json:"periods"`
}

// PlanPeriod represents a single discrete scheduling period in a Plan.
type PlanPeriod struct {
	TSStart       time.Time    `json:"tsStart"`
	TSEnd         time.Time    `json:"tsEnd"`
	DurationHours float64      `json:"durationHours"`
	ImportDollars float64      `json:"importDollars"`
	ExportDollars float64      `json:"exportDollars,omitempty"`
	BatteryMode   BatteryMode  `json:"batteryMode"`
	SolarMode     SolarMode    `json:"solarMode"`
	Reason        ActionReason `json:"reason"`
	StartSOC      float64      `json:"startSoc"`
	EndSOC        float64      `json:"endSoc"`
	ReserveSOC    float64      `json:"reserveSOC,omitempty"`
	LoadKWH       float64      `json:"loadKWH,omitempty"`
	SolarKWH      float64      `json:"solarKWH,omitempty"`
	GridImportKWH float64      `json:"gridImportKWH,omitempty"`
	GridExportKWH float64      `json:"gridExportKWH,omitempty"`
	CostDollars   float64      `json:"costDollars,omitempty"`
}

// StoredPlanPeriod represents a single discrete scheduling period in a StoredPlan,
// stored with compact 1-2 character JSON tags in Firestore to reduce storage footprint.
type StoredPlanPeriod struct {
	TSStart       time.Time    `json:"ts"`
	TSEnd         time.Time    `json:"te"`
	DurationHours float64      `json:"d"`
	ImportDollars float64      `json:"i"`
	ExportDollars float64      `json:"e,omitempty"`
	BatteryMode   BatteryMode  `json:"bm"`
	SolarMode     SolarMode    `json:"sm"`
	Reason        ActionReason `json:"r"`
	StartSOC      float64      `json:"ss"`
	EndSOC        float64      `json:"es"`
	ReserveSOC    float64      `json:"rs,omitempty"`
	LoadKWH       float64      `json:"l,omitempty"`
	SolarKWH      float64      `json:"s,omitempty"`
	GridImportKWH float64      `json:"gi,omitempty"`
	GridExportKWH float64      `json:"ge,omitempty"`
	CostDollars   float64      `json:"c,omitempty"`
}

// StoredPlan represents a forward-looking optimal schedule over the planning horizon,
// stored with compact 1-2 character JSON tags in Firestore.
type StoredPlan struct {
	TSCreated          time.Time          `json:"ts"`
	HorizonHours       int                `json:"h"`
	TotalProjectedCost float64            `json:"tc,omitempty"`
	TotalExportCredits float64            `json:"te,omitempty"`
	NetEconomicBenefit float64            `json:"nb,omitempty"`
	Periods            []StoredPlanPeriod `json:"p"`
}

// StoredAction wraps Action for Firestore persistence, storing Plan under a compact
// StoredPlan schema using the "shortPlan" JSON key to optimize storage footprint.
type StoredAction struct {
	Action
	Plan *StoredPlan `json:"shortPlan,omitempty"`
}

// ToStored converts a PlanPeriod to its compact StoredPlanPeriod representation.
func (p PlanPeriod) ToStored() StoredPlanPeriod {
	return StoredPlanPeriod{
		TSStart:       p.TSStart,
		TSEnd:         p.TSEnd,
		DurationHours: p.DurationHours,
		ImportDollars: p.ImportDollars,
		ExportDollars: p.ExportDollars,
		BatteryMode:   p.BatteryMode,
		SolarMode:     p.SolarMode,
		Reason:        p.Reason,
		StartSOC:      p.StartSOC,
		EndSOC:        p.EndSOC,
		ReserveSOC:    p.ReserveSOC,
		LoadKWH:       p.LoadKWH,
		SolarKWH:      p.SolarKWH,
		GridImportKWH: p.GridImportKWH,
		GridExportKWH: p.GridExportKWH,
		CostDollars:   p.CostDollars,
	}
}

// ToPlanPeriod converts a StoredPlanPeriod back to a domain PlanPeriod.
func (sp StoredPlanPeriod) ToPlanPeriod() PlanPeriod {
	return PlanPeriod{
		TSStart:       sp.TSStart,
		TSEnd:         sp.TSEnd,
		DurationHours: sp.DurationHours,
		ImportDollars: sp.ImportDollars,
		ExportDollars: sp.ExportDollars,
		BatteryMode:   sp.BatteryMode,
		SolarMode:     sp.SolarMode,
		Reason:        sp.Reason,
		StartSOC:      sp.StartSOC,
		EndSOC:        sp.EndSOC,
		ReserveSOC:    sp.ReserveSOC,
		LoadKWH:       sp.LoadKWH,
		SolarKWH:      sp.SolarKWH,
		GridImportKWH: sp.GridImportKWH,
		GridExportKWH: sp.GridExportKWH,
		CostDollars:   sp.CostDollars,
	}
}

// ToStored converts a Plan to its compact StoredPlan representation. Returns nil if p is nil.
func (p *Plan) ToStored() *StoredPlan {
	if p == nil {
		return nil
	}
	sp := &StoredPlan{
		TSCreated:          p.TSCreated,
		HorizonHours:       p.HorizonHours,
		TotalProjectedCost: p.TotalProjectedCost,
		TotalExportCredits: p.TotalExportCredits,
		NetEconomicBenefit: p.NetEconomicBenefit,
		Periods:            make([]StoredPlanPeriod, len(p.Periods)),
	}
	for i, per := range p.Periods {
		sp.Periods[i] = per.ToStored()
	}
	return sp
}

// ToPlan converts a StoredPlan back to a domain Plan. Returns nil if sp is nil.
func (sp *StoredPlan) ToPlan() *Plan {
	if sp == nil {
		return nil
	}
	p := &Plan{
		TSCreated:          sp.TSCreated,
		HorizonHours:       sp.HorizonHours,
		TotalProjectedCost: sp.TotalProjectedCost,
		TotalExportCredits: sp.TotalExportCredits,
		NetEconomicBenefit: sp.NetEconomicBenefit,
		Periods:            make([]PlanPeriod, len(sp.Periods)),
	}
	for i, per := range sp.Periods {
		p.Periods[i] = per.ToPlanPeriod()
	}
	return p
}

// ToStored wraps an Action into a StoredAction, clearing the embedded Action.Plan
// so that json.Marshal only serializes Plan under "shortPlan".
func (a Action) ToStored() StoredAction {
	sa := StoredAction{
		Action: a,
	}
	sa.Action.Plan = nil
	if a.Plan != nil {
		sa.Plan = a.Plan.ToStored()
	}
	return sa
}

// ToAction extracts the Action from a StoredAction, restoring Plan from
// shortPlan.
func (s StoredAction) ToAction() Action {
	act := s.Action
	if s.Plan != nil {
		act.Plan = s.Plan.ToPlan()
	}
	return act
}
