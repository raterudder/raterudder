package types

import "time"

// HourlySavingsStatsDebugging is a helper struct for debugging the savings calculations
type HourlySavingsStatsDebugging struct {
	ExportPrice     float64 `json:"exportPrice"`
	ImportPrice     float64 `json:"importPrice"`
	BatteryToHome   float64 `json:"batteryToHome"`
	Avoided         float64 `json:"avoided"`
	GridToBattery   float64 `json:"gridToBattery"`
	ChargingCost    float64 `json:"chargingCost"`
	SolarToHome     float64 `json:"solarToHome"`
	SolarSavings    float64 `json:"solarSavings"`
	IgnoredFraction float64 `json:"ignoredFraction"`
}

// SavingsStats is the response type for the savings endpoint
type SavingsStats struct {
	Timestamp       time.Time                     `json:"timestamp"`
	Cost            float64                       `json:"cost"`
	Credit          float64                       `json:"credit"`
	BatterySavings  float64                       `json:"batterySavings"` // Estimated Battery Savings = Avoided - Charging
	SolarSavings    float64                       `json:"solarSavings"`   // Estimated Solar Savings = SolarToHome * Price
	AvoidedCost     float64                       `json:"avoidedCost"`    // Cost we would have paid w/o battery (BatteryToHome * Price)
	ChargingCost    float64                       `json:"chargingCost"`   // Cost to charge the battery from grid
	SolarGenerated  float64                       `json:"solarGenerated"` // Total solar generated
	GridImported    float64                       `json:"gridImported"`   // Total grid imported
	GridExported    float64                       `json:"gridExported"`   // Total grid exported
	HomeUsed        float64                       `json:"homeUsed"`       // Total home usage
	BatteryUsed     float64                       `json:"batteryUsed"`    // Total battery discharged
	HourlyDebugging []HourlySavingsStatsDebugging `json:"hourlyDebugging"`
}
