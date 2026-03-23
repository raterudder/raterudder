package types

import "time"

// InterestSubmission represents interest expressed by a user for unsupported
// configurations.
type InterestSubmission struct {
	Email               string    `json:"email"`
	Utility             string    `json:"utility"`
	Battery             string    `json:"battery"`
	UtilityProviderName string    `json:"utilityProviderName"`
	State               string    `json:"state"`
	PlanName            string    `json:"planName"`
	BatteryName         string    `json:"batteryName"`
	Comments            string    `json:"comments"`
	Timestamp           time.Time `json:"timestamp"`
}
