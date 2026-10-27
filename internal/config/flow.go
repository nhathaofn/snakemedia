package config

import "fmt"

// FlowConfig contains operational policy, never account or captured build data,
// nor Flow's own limits (source size, trim length): those are Flow's, read or
// kept as logged fallbacks in the provider. Keys of removed fields in a saved
// file are ignored on load.
type FlowConfig struct {
	ProbePath          string `json:"probePath"`
	UploadAttempts     int    `json:"uploadAttempts"`
	UploadRetryDelayMS int    `json:"uploadRetryDelayMs"`
	MaxPollFailures    int    `json:"maxPollFailures"`
	PollMaxIntervalMS  int    `json:"pollMaxIntervalMs"`
	// MaxImageBytes bounds one downloaded generated or referenced image.
	MaxImageBytes int64 `json:"maxImageBytes"`
	// ModelCacheSeconds is how long an account's model catalogue is reused.
	ModelCacheSeconds int `json:"modelCacheSeconds"`
	// JobRetentionHours is how long finished job journals are kept, and how
	// long an unconfirmed job may stay pending before it is given up.
	JobRetentionHours int `json:"jobRetentionHours"`
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
	// QueueThreads is how many image queue tasks (Ảnh → All) run at once.
	QueueThreads int `json:"queueThreads"`
	// QueueGapMinSeconds and QueueGapMaxSeconds bound the random wait between
	// two new requests the image queue sends.
	QueueGapMinSeconds int `json:"queueGapMinSeconds"`
	QueueGapMaxSeconds int `json:"queueGapMaxSeconds"`
	// QueueMaxTasks caps the image queue journal; the oldest finished tasks go first.
	QueueMaxTasks int `json:"queueMaxTasks"`
	// MaxDiscoveryBytes and MaxDiscoveryScripts bound how much of Flow's
	// scripts RPC discovery reads; RPCCacheSeconds is how long a discovery
	// result (or a failed one) is reused before the scripts are scanned again.
	MaxDiscoveryBytes   int64 `json:"maxDiscoveryBytes"`
	MaxDiscoveryScripts int   `json:"maxDiscoveryScripts"`
	RPCCacheSeconds     int   `json:"rpcCacheSeconds"`
}

func DefaultFlowConfig() FlowConfig {
	return FlowConfig{UploadAttempts: 2, UploadRetryDelayMS: 500, MaxPollFailures: 5, PollMaxIntervalMS: 30000, MaxImageBytes: 32 << 20, ModelCacheSeconds: 300, JobRetentionHours: 168,
		GenerationTimeoutSeconds: 120, VideoGenerationTimeoutSeconds: 600, VideoPollIntervalMS: 5000,
		MaxConcurrentRequestsPerAccount: 2, SignedOutReads: 3,
		QueueThreads: 1, QueueGapMinSeconds: 5, QueueGapMaxSeconds: 10, QueueMaxTasks: 2000,
		MaxDiscoveryScripts: 16, MaxDiscoveryBytes: 16 << 20, RPCCacheSeconds: 15}
}

func (c FlowConfig) Validate() error {
	if c.UploadAttempts < 1 || c.UploadAttempts > 10 || c.UploadRetryDelayMS < 0 || c.UploadRetryDelayMS > 60000 || c.MaxPollFailures < 1 || c.MaxPollFailures > 100 || c.PollMaxIntervalMS < 1000 || c.PollMaxIntervalMS > 60000 {
		return fmt.Errorf("flow: invalid upload or polling policy")
	}
	if c.MaxImageBytes < 1<<20 || c.MaxImageBytes > 256<<20 || c.ModelCacheSeconds < 0 || c.ModelCacheSeconds > 86400 || c.JobRetentionHours < 1 || c.JobRetentionHours > 8760 {
		return fmt.Errorf("flow: invalid image size, model cache or job retention policy")
	}
	if c.GenerationTimeoutSeconds < 10 || c.GenerationTimeoutSeconds > 600 {
		return fmt.Errorf("flow: generation timeout must be between 10 and 600 seconds")
	}
	if c.VideoGenerationTimeoutSeconds < 60 || c.VideoGenerationTimeoutSeconds > 3600 || c.VideoPollIntervalMS < 1000 || c.VideoPollIntervalMS > 60000 {
		return fmt.Errorf("flow: video timeout must be 60-3600 seconds and video polling 1000-60000 ms")
	}
	if c.MaxConcurrentRequestsPerAccount < 1 || c.MaxConcurrentRequestsPerAccount > 10 || c.SignedOutReads < 1 || c.SignedOutReads > 20 {
		return fmt.Errorf("flow: concurrent requests per account must be 1-10 and signed-out reads 1-20")
	}
	if c.QueueThreads < 1 || c.QueueThreads > 20 || c.QueueMaxTasks < 50 || c.QueueMaxTasks > 20000 {
		return fmt.Errorf("flow: image queue threads must be 1-20 and its size 50-20000 tasks")
	}
	if c.QueueGapMinSeconds < 0 || c.QueueGapMaxSeconds > 600 || c.QueueGapMinSeconds > c.QueueGapMaxSeconds {
		return fmt.Errorf("flow: image queue wait must be 0-600 seconds, minimum not above maximum")
	}
	if c.MaxDiscoveryBytes < 1024 || c.MaxDiscoveryBytes > 64<<20 || c.MaxDiscoveryScripts < 1 || c.MaxDiscoveryScripts > 128 {
		return fmt.Errorf("flow: invalid RPC discovery budget")
	}
	if c.RPCCacheSeconds < 1 || c.RPCCacheSeconds > 3600 {
		return fmt.Errorf("flow: invalid RPC cache duration")
	}
	return nil
}

// FlowPagePolicy is the account policy handed to Flow's page layer
// (flow/chrome, including the frozen recaptcha.go), with Flow's RPC discovery
// fields filled in from FlowConfig.
func (c AppConfig) FlowPagePolicy() AccountConfig {
	policy := c.Accounts
	policy.MaxDiscoveryBytes = c.Flow.MaxDiscoveryBytes
	policy.MaxDiscoveryScripts = c.Flow.MaxDiscoveryScripts
	policy.RPCCacheSeconds = c.Flow.RPCCacheSeconds
	return policy
}

// DefaultFlowPagePolicy is FlowPagePolicy over the default configuration.
func DefaultFlowPagePolicy() AccountConfig {
	return AppConfig{Accounts: DefaultAccountConfig(), Flow: DefaultFlowConfig()}.FlowPagePolicy()
}
