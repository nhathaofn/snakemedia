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
	// GenerationTimeoutSeconds bounds one media upload or generation RPC, which
	// runs far longer than session and metadata calls.
	GenerationTimeoutSeconds int `json:"generationTimeoutSeconds"`
	// VideoGenerationTimeoutSeconds bounds waiting for Flow to finish rendering videos.
	VideoGenerationTimeoutSeconds int `json:"videoGenerationTimeoutSeconds"`
	// VideoPollIntervalMS is the delay between video status checks.
	VideoPollIntervalMS int `json:"videoPollIntervalMs"`
}

func DefaultAccountConfig() AccountConfig {
	return AccountConfig{
		PollIntervalMS: 1500, PollMaxIntervalMS: 15000,
		LoginTimeoutSeconds: 300, RequestTimeoutSeconds: 15,
		BrowserStartupTimeoutSeconds: 30, RefreshCooldownSeconds: 15,
		MaxResponseBytes:              4 << 20,
		MaxDiscoveryScripts:           16,
		GenerationTimeoutSeconds:      120,
		VideoGenerationTimeoutSeconds: 600,
		VideoPollIntervalMS:           5000,
	}
}

func (c AccountConfig) Validate() error {
	if c.PollIntervalMS < 500 || c.PollMaxIntervalMS < c.PollIntervalMS || c.PollMaxIntervalMS > 60000 {
		return fmt.Errorf("accounts: polling must be between 500 and 60000 ms, with max >= interval")
	}
	if c.RequestTimeoutSeconds < 1 || c.RequestTimeoutSeconds > 120 || c.BrowserStartupTimeoutSeconds < c.RequestTimeoutSeconds || c.BrowserStartupTimeoutSeconds > 120 {
		return fmt.Errorf("accounts: request/startup timeouts must be between 1 and 120 seconds, with startup >= request")
	}
	if c.GenerationTimeoutSeconds < c.RequestTimeoutSeconds || c.GenerationTimeoutSeconds > 600 {
		return fmt.Errorf("accounts: generation timeout must be between request timeout and 600 seconds")
	}
	if c.VideoGenerationTimeoutSeconds < 60 || c.VideoGenerationTimeoutSeconds > 3600 || c.VideoPollIntervalMS < 1000 || c.VideoPollIntervalMS > 60000 {
		return fmt.Errorf("accounts: video timeout must be 60-3600 seconds and video polling 1000-60000 ms")
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
