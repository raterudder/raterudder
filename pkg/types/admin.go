package types

// AdminSettings represents global system configuration for admin functions.
type AdminSettings struct {
	Aliases map[string]string `json:"aliases"`
}
