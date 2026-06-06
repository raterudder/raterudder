package types

import "time"

// ESSProviderInfo provides metadata about an Energy Storage System (ESS) provider.
type ESSProviderInfo struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Credentials []ESSCredentialField `json:"credentials"`
	OAuthURLs   map[string]string    `json:"oAuthURLs,omitempty"`
	OAuthKey    *ESSCredentialField  `json:"oAuthKey,omitempty"`
	Hidden      bool                 `json:"hidden,omitempty"`
}

// ESSCredentialFieldType defines the type of input field for an ESSCredentialField.
type ESSCredentialFieldType string

const (
	ESSCredentialFieldTypeSelect   ESSCredentialFieldType = "select"
	ESSCredentialFieldTypeString   ESSCredentialFieldType = "string"
	ESSCredentialFieldTypePassword ESSCredentialFieldType = "password"
)

// ESSCredentialField defines a single configuration/credential option for an ESS.
type ESSCredentialField struct {
	Field       string                     `json:"field"`
	Name        string                     `json:"name"`
	Type        ESSCredentialFieldType     `json:"type"` // e.g. "string" or "password" or "select"
	Choices     []ESSCredentialFieldChoice `json:"choices,omitempty"`
	Default     any                        `json:"default,omitempty"`
	Required    bool                       `json:"required"`
	Description string                     `json:"description,omitempty"`
	Stage       int                        `json:"stage"`
	Hidden      bool                       `json:"hidden,omitempty"`
	OneTime     bool                       `json:"oneTime,omitempty"`
}

// ESSCredentialFieldChoice defines a single choice for an ESSCredentialField.
type ESSCredentialFieldChoice struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// ESSMockState represents the internal state of a mock ESS provider.
type ESSMockState struct {
	Timestamp    time.Time              `json:"timestamp"`
	BatterySOC   float64                `json:"batterySOC"`
	BatteryMode  BatteryMode            `json:"batteryMode"`
	SolarMode    SolarMode              `json:"solarMode"`
	DailyHistory map[string]EnergyStats `json:"dailyHistory"`
}
