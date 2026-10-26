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
	MaxDiscoveryBytes            int64 `json:"maxDiscoveryBytes"`
	MaxDiscoveryScripts          int   `json:"maxDiscoveryScripts"`
	// GenerationTimeoutSeconds bounds one media upload or generation RPC, which
	// runs far longer than session and metadata calls.
	GenerationTimeoutSeconds int `json:"generationTimeoutSeconds"`
	// VideoGenerationTimeoutSeconds bounds waiting for Flow to finish rendering videos.
	VideoGenerationTimeoutSeconds int `json:"videoGenerationTimeoutSeconds"`
	// VideoPollIntervalMS is the delay between video status checks.
	VideoPollIntervalMS int `json:"videoPollIntervalMs"`
	// MaxConcurrentRequestsPerAccount caps generations one account runs at once.
	MaxConcurrentRequestsPerAccount int `json:"maxConcurrentRequestsPerAccount"`
	// SignedOutReads is how many consecutive anonymous Flow page reads count as
	// signed out, so a page still filling in its session is not misread.
	SignedOutReads int `json:"signedOutReads"`
	// RPCCacheSeconds is how long a failed RPC discovery result is reused
	// before the scripts are scanned again.
	RPCCacheSeconds int `json:"rpcCacheSeconds"`
}

func DefaultAccountConfig() AccountConfig {
	return AccountConfig{
		PollIntervalMS: 1500, PollMaxIntervalMS: 15000,
		LoginTimeoutSeconds: 300, RequestTimeoutSeconds: 15,
		BrowserStartupTimeoutSeconds: 30, RefreshCooldownSeconds: 15,
		MaxResponseBytes:                4 << 20,
		MaxDiscoveryScripts:             16,
		MaxDiscoveryBytes:               16 << 20,
		GenerationTimeoutSeconds:        120,
		VideoGenerationTimeoutSeconds:   600,
		VideoPollIntervalMS:             5000,
		MaxConcurrentRequestsPerAccount: 2,
		SignedOutReads:                  3,
		RPCCacheSeconds:                 15,
	}
}

func (c AccountConfig) Validate() error {
	if c.MaxDiscoveryBytes < 1024 || c.MaxDiscoveryBytes > 64<<20 {
		return fmt.Errorf("accounts: invalid script discovery budget")
	}
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
	if c.MaxConcurrentRequestsPerAccount < 1 || c.MaxConcurrentRequestsPerAccount > 10 {
		return fmt.Errorf("accounts: concurrent requests per account must be between 1 and 10")
	}
	if c.SignedOutReads < 1 || c.SignedOutReads > 20 || c.RPCCacheSeconds < 1 || c.RPCCacheSeconds > 3600 {
		return fmt.Errorf("accounts: invalid signed-out read count or RPC cache duration")
	}
	return nil
}
