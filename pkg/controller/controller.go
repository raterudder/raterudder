package controller

import (
	"fmt"

	"github.com/raterudder/raterudder/pkg/types"
)

const priceEpsilonForEquality = 1e-3

// Decision represents the result of the decision logic.
type Decision struct {
	Action           types.Action
	SimulationParams types.SimulationParams
}

// Controller handles the decision-making logic for the ESS.
type Controller struct{}

// NewController creates a new Controller.
func NewController() *Controller {
	return &Controller{}
}

func drModeString(mode types.BatteryMode) string {
	switch mode {
	case types.BatteryModeNoChange:
		return "NoChange"
	case types.BatteryModeStandby:
		return "Standby"
	case types.BatteryModeChargeAny:
		return "ChargeAny"
	case types.BatteryModeLoad:
		return "Load"
	case types.BatteryModeExport:
		return "Export"
	default:
		return fmt.Sprintf("Unknown(%d)", mode)
	}
}
