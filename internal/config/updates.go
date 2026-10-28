package config

// UpdateConfig is the app updater's policy (Cài đặt → Chung → Cập nhật).
type UpdateConfig struct {
	// CheckOnStartup looks for a newer release once the window is up and
	// tells the user; installing always waits for the user.
	CheckOnStartup bool `json:"checkOnStartup"`
}

// DefaultUpdateConfig checks on every start.
func DefaultUpdateConfig() UpdateConfig {
	return UpdateConfig{CheckOnStartup: true}
}
