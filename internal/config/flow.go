package config

import "fmt"

// FlowConfig contains operational policy, never account or captured build data.
type FlowConfig struct {
	ProbePath          string  `json:"probePath"`
	MaxSourceBytes     int64   `json:"maxSourceBytes"`
	MaxDownloadBytes   int64   `json:"maxDownloadBytes"`
	UploadAttempts     int     `json:"uploadAttempts"`
	UploadRetryDelayMS int     `json:"uploadRetryDelayMs"`
	MaxPollFailures    int     `json:"maxPollFailures"`
	PollMaxIntervalMS  int     `json:"pollMaxIntervalMs"`
	MinTrimSeconds     float64 `json:"minTrimSeconds"`
	TrimStepSeconds    float64 `json:"trimStepSeconds"`
}

func DefaultFlowConfig() FlowConfig {
	return FlowConfig{MaxSourceBytes: 1 << 30, MaxDownloadBytes: 512 << 20, UploadAttempts: 2, UploadRetryDelayMS: 500, MaxPollFailures: 5, PollMaxIntervalMS: 30000, MinTrimSeconds: 0.5, TrimStepSeconds: 0.1}
}

func (c FlowConfig) Validate() error {
	if !(c.MinTrimSeconds > 0 && c.MinTrimSeconds <= 60 && c.TrimStepSeconds > 0 && c.TrimStepSeconds <= c.MinTrimSeconds) {
		return fmt.Errorf("flow: invalid trim policy")
	}
	if c.MaxSourceBytes <= 0 || c.MaxDownloadBytes <= 0 || c.UploadAttempts < 1 || c.UploadAttempts > 10 || c.UploadRetryDelayMS < 0 || c.UploadRetryDelayMS > 60000 || c.MaxPollFailures < 1 || c.MaxPollFailures > 100 || c.PollMaxIntervalMS < 1000 || c.PollMaxIntervalMS > 60000 {
		return fmt.Errorf("flow: invalid upload or polling policy")
	}
	return nil
}
