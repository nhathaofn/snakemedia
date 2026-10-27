package config

import "fmt"

// AccountConfig owns the adjustable policy shared by every provider's accounts
// and their Chrome browsers. Protocol identifiers belong to the provider
// adapter; session values come from Chrome.
type AccountConfig struct {
	// Headless runs account browsers without a window; sign-in always opens one.
	Headless bool `json:"headless"`
	// PageReadySeconds bounds waiting for a freshly opened page to show its session.
	PageReadySeconds             int   `json:"pageReadySeconds"`
	PollIntervalMS               int   `json:"pollIntervalMs"`
	LoginTimeoutSeconds          int   `json:"loginTimeoutSeconds"`
	RequestTimeoutSeconds        int   `json:"requestTimeoutSeconds"`
	BrowserStartupTimeoutSeconds int   `json:"browserStartupTimeoutSeconds"`
	MaxResponseBytes             int64 `json:"maxResponseBytes"`
	// MaxDownloadBytes bounds one generated file (image or video) downloaded
	// from any provider.
	MaxDownloadBytes int64 `json:"maxDownloadBytes"`
	// MaxDiscoveryBytes, MaxDiscoveryScripts and RPCCacheSeconds are Flow's RPC
	// discovery policy, saved in FlowConfig. They are not persisted or
	// validated here: Flow's page layer (including the frozen recaptcha.go)
	// takes this struct, so the Flow provider fills them from FlowConfig
	// before handing it down.
	MaxDiscoveryBytes   int64 `json:"-"`
	MaxDiscoveryScripts int   `json:"-"`
	RPCCacheSeconds     int   `json:"-"`
	// IdleBrowserMinutes closes an account's headless browser no work has used
	// for that long, so rotating over many accounts does not keep a Chrome per
	// account; 0 keeps them until the app exits.
	IdleBrowserMinutes int `json:"idleBrowserMinutes"`
}

func DefaultAccountConfig() AccountConfig {
	return AccountConfig{
		Headless: true, PageReadySeconds: 60,
		PollIntervalMS:      1500,
		LoginTimeoutSeconds: 900, RequestTimeoutSeconds: 15,
		BrowserStartupTimeoutSeconds: 30,
		MaxResponseBytes:             4 << 20,
		MaxDownloadBytes:             512 << 20,
		IdleBrowserMinutes:           10,
	}
}

func (c AccountConfig) Validate() error {
	if c.PageReadySeconds < 10 || c.PageReadySeconds > 600 {
		return fmt.Errorf("accounts: page readiness must be between 10 and 600 seconds")
	}
	if c.PollIntervalMS < 500 || c.PollIntervalMS > 60000 {
		return fmt.Errorf("accounts: polling must be between 500 and 60000 ms")
	}
	if c.RequestTimeoutSeconds < 1 || c.RequestTimeoutSeconds > 120 || c.BrowserStartupTimeoutSeconds < c.RequestTimeoutSeconds || c.BrowserStartupTimeoutSeconds > 120 {
		return fmt.Errorf("accounts: request/startup timeouts must be between 1 and 120 seconds, with startup >= request")
	}
	if c.LoginTimeoutSeconds < c.BrowserStartupTimeoutSeconds || c.LoginTimeoutSeconds > 3600 {
		return fmt.Errorf("accounts: login timeout must be between startup timeout and 3600 seconds")
	}
	if c.MaxResponseBytes < 1024 || c.MaxResponseBytes > 32<<20 {
		return fmt.Errorf("accounts: invalid response size limit")
	}
	if c.MaxDownloadBytes < 1<<20 || c.MaxDownloadBytes > 8<<30 {
		return fmt.Errorf("accounts: download size limit must be between 1 MB and 8 GB")
	}
	if c.IdleBrowserMinutes < 0 || c.IdleBrowserMinutes > 1440 {
		return fmt.Errorf("accounts: idle browser timeout must be between 0 and 1440 minutes")
	}
	return nil
}
