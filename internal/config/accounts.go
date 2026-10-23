package config

import "fmt"

// AccountConfig owns the adjustable account timing and resource policy. Protocol
// identifiers belong to the provider adapter; session values come from Chrome.
type AccountConfig struct {
	PollIntervalMS               int   `json:"pollIntervalMs"`
	PollMaxIntervalMS            int   `json:"pollMaxIntervalMs"`
	LoginTimeoutSeconds          int   `json:"loginTimeoutSeconds"`
	RequestTimeoutSeconds        int   `json:"requestTimeoutSeconds"`
	BrowserStartupTimeoutSeconds int   `json:"browserStartupTimeoutSeconds"`
	RefreshCooldownSeconds       int   `json:"refreshCooldownSeconds"`
	MaxResponseBytes             int64 `json:"maxResponseBytes"`
	MaxDiscoveryScripts          int   `json:"maxDiscoveryScripts"`
}

func DefaultAccountConfig() AccountConfig {
	return AccountConfig{
		PollIntervalMS: 1500, PollMaxIntervalMS: 15000,
		LoginTimeoutSeconds: 300, RequestTimeoutSeconds: 15,
		BrowserStartupTimeoutSeconds: 30, RefreshCooldownSeconds: 15,
		MaxResponseBytes:    4 << 20,
		MaxDiscoveryScripts: 16,
	}
}

func (c AccountConfig) Validate() error {
	if c.PollIntervalMS < 500 || c.PollMaxIntervalMS < c.PollIntervalMS || c.PollMaxIntervalMS > 60000 {
		return fmt.Errorf("accounts: polling must be between 500 and 60000 ms, with max >= interval")
	}
	if c.RequestTimeoutSeconds < 1 || c.RequestTimeoutSeconds > 120 || c.BrowserStartupTimeoutSeconds < c.RequestTimeoutSeconds || c.BrowserStartupTimeoutSeconds > 120 {
		return fmt.Errorf("accounts: request/startup timeouts must be between 1 and 120 seconds, with startup >= request")
	}
	if c.LoginTimeoutSeconds < c.BrowserStartupTimeoutSeconds || c.LoginTimeoutSeconds > 3600 {
		return fmt.Errorf("accounts: login timeout must be between startup timeout and 3600 seconds")
	}
	if c.RefreshCooldownSeconds < 1 || c.RefreshCooldownSeconds > 3600 || c.MaxResponseBytes < 1024 || c.MaxResponseBytes > 32<<20 {
		return fmt.Errorf("accounts: invalid refresh cooldown or response size limit")
	}
	if c.MaxDiscoveryScripts < 1 || c.MaxDiscoveryScripts > 128 {
		return fmt.Errorf("accounts: discovery script limit must be between 1 and 128")
	}
	return nil
}
