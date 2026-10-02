package controller

// PlanActionName represents the diagnostic name assigned to an action candidate in the optimal plan.
type PlanActionName = string

const (
	// VPP Operational Actions
	PlanActionVPPTargetSOCReached           PlanActionName = "VPP target SOC reached"
	PlanActionVPPActiveEventDischarging     PlanActionName = "VPP active event discharging"
	PlanActionVPP2HourPrepEmergencyTopUp    PlanActionName = "VPP 2-hour prep emergency top-up"
	PlanActionVPP2HourPrepStandbyLock       PlanActionName = "VPP 2-hour prep standby lock"
	PlanActionVPP2HourPrepDirectSolarExport PlanActionName = "VPP 2-hour prep direct solar export"
	PlanActionVPPPreChargeBeforeDeadline    PlanActionName = "VPP pre-charge before deadline"

	// EV Charging Actions
	PlanActionActiveEVChargingStandby PlanActionName = "active EV charging detected, battery locked in standby"

	// Negative / Force-Charge Threshold Actions
	PlanActionNegativeOrForceChargeThresholdCharging PlanActionName = "negative or force-charge threshold charging"
	PlanActionNegativeOrForceChargeThresholdStandby  PlanActionName = "negative or force-charge threshold standby"

	// Self-Consumption & Reserve Actions
	PlanActionDischargingBattery      PlanActionName = "discharging battery"
	PlanActionBatteryAtReserveStandby PlanActionName = "battery at reserve, standby"
	PlanActionBatteryStandby          PlanActionName = "battery standby"

	// Grid Charging & Pre-Charge Actions
	PlanActionReserveTargetCharge    PlanActionName = "reserve target charge"
	PlanActionGridArbitragePreCharge PlanActionName = "grid arbitrage pre-charge"

	// Export Actions
	PlanActionDirectSolarExport     PlanActionName = "direct solar export"
	PlanActionBatteryGridExportDump PlanActionName = "battery grid export dump"
)

// AllPlanActionNames contains all recognized action candidate names.
var AllPlanActionNames = []PlanActionName{
	PlanActionVPPTargetSOCReached,
	PlanActionVPPActiveEventDischarging,
	PlanActionVPP2HourPrepEmergencyTopUp,
	PlanActionVPP2HourPrepStandbyLock,
	PlanActionVPP2HourPrepDirectSolarExport,
	PlanActionVPPPreChargeBeforeDeadline,
	PlanActionActiveEVChargingStandby,
	PlanActionNegativeOrForceChargeThresholdCharging,
	PlanActionNegativeOrForceChargeThresholdStandby,
	PlanActionDischargingBattery,
	PlanActionBatteryAtReserveStandby,
	PlanActionBatteryStandby,
	PlanActionReserveTargetCharge,
	PlanActionGridArbitragePreCharge,
	PlanActionDirectSolarExport,
	PlanActionBatteryGridExportDump,
}
