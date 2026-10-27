package config

import "fmt"

// MuseMode is how queued jobs are spread over accounts.
type MuseMode string

const (
	// MuseModeRotate prefers the least busy account, then the lowest quota use.
	MuseModeRotate MuseMode = "rotate"
	// MuseModeSequential uses accounts in their saved order.
	MuseModeSequential MuseMode = "sequential"
)

// AllMuseModes is bound to the frontend as a TypeScript enum (config.MuseMode).
var AllMuseModes = []struct {
	Value  MuseMode
	TSName string
}{{MuseModeRotate, "Rotate"}, {MuseModeSequential, "Sequential"}}

// MuseConfig is how the app runs its Muse queue. What Muse itself allows
// (attachment types and sizes, quota, models) is read from muse.ai on each
// account, never set here. MaxRechecks, PollIntervalMS and PollErrorLimit are
// not shown in the settings screen: their defaults suit every user, and a
// saved value still loads.
type MuseConfig struct {
	// MaxAttempts bounds how many times one job may be sent.
	MaxAttempts int `json:"maxAttempts"`
	// MaxRechecks bounds how often a job Muse already accepted is read back
	// after transport errors before it is handed to the user. It is separate
	// from MaxAttempts because sending again would pay twice.
	MaxRechecks int `json:"maxRechecks"`
	// ImageTimeoutMinutes and VideoTimeoutMinutes bound waiting for Muse to finish.
	ImageTimeoutMinutes int `json:"imageTimeoutMinutes"`
	VideoTimeoutMinutes int `json:"videoTimeoutMinutes"`
	// PollIntervalMS is the delay between session status checks.
	PollIntervalMS int `json:"pollIntervalMs"`
	// PollErrorLimit is how many consecutive failed status checks end a job.
	PollErrorLimit int `json:"pollErrorLimit"`
	// SendGapMinSeconds..SendGapMaxSeconds is the random pause between two sends
	// on one account; bursts of sends look automated to Muse.
	SendGapMinSeconds int `json:"sendGapMinSeconds"`
	SendGapMaxSeconds int `json:"sendGapMaxSeconds"`
	// SendTimeoutSeconds bounds one send, which waits for Muse to prepare its
	// machine, and reading Muse's limits, which may scan its scripts once.
	SendTimeoutSeconds int `json:"sendTimeoutSeconds"`
	// MaxJobs caps the journal; the oldest finished jobs are dropped first.
	MaxJobs int `json:"maxJobs"`
	// MaxAccounts caps how many accounts work at once; TotalThreads caps jobs
	// across all accounts and ThreadsPerAccount jobs on one account.
	MaxAccounts       int      `json:"maxAccounts"`
	TotalThreads      int      `json:"totalThreads"`
	ThreadsPerAccount int      `json:"threadsPerAccount"`
	Mode              MuseMode `json:"mode"`
	// UseTopup lets an account go on with its additional tokens once its
	// weekly usage is gone; off rests it until the weekly reset.
	UseTopup bool `json:"useTopup"`
	// UsagePauseMinutes rests an account after Muse replies that it hit a limit
	// while its weekly reset time is unknown.
	UsagePauseMinutes int `json:"usagePauseMinutes"`
	// AutoSave copies every finished file into OutputDir (a dated sub-folder).
	AutoSave bool `json:"autoSave"`
	// OutputDir is where finished files are copied; empty uses the app's
	// default folder (paths.MuseOutputDir).
	OutputDir string `json:"outputDir"`
}

func DefaultMuseConfig() MuseConfig {
	return MuseConfig{
		MaxAttempts:         2,
		MaxRechecks:         10,
		ImageTimeoutMinutes: 5,
		VideoTimeoutMinutes: 30,
		PollIntervalMS:      4000,
		PollErrorLimit:      5,
		SendGapMinSeconds:   15,
		SendGapMaxSeconds:   20,
		SendTimeoutSeconds:  180,
		MaxJobs:             500,
		MaxAccounts:         2,
		TotalThreads:        2,
		ThreadsPerAccount:   1,
		Mode:                MuseModeRotate,
		UseTopup:            true,
		UsagePauseMinutes:   60,
		AutoSave:            true,
	}
}

func (c MuseConfig) Validate() error {
	if c.MaxAttempts < 1 || c.MaxAttempts > 5 {
		return fmt.Errorf("muse: maxAttempts must be between 1 and 5")
	}
	if c.MaxRechecks < 1 || c.MaxRechecks > 100 {
		return fmt.Errorf("muse: maxRechecks must be between 1 and 100")
	}
	if c.ImageTimeoutMinutes < 1 || c.ImageTimeoutMinutes > 60 || c.VideoTimeoutMinutes < 5 || c.VideoTimeoutMinutes > 120 {
		return fmt.Errorf("muse: image timeout must be 1-60 minutes and video timeout 5-120 minutes")
	}
	if c.PollIntervalMS < 1000 || c.PollIntervalMS > 60000 || c.PollErrorLimit < 1 || c.PollErrorLimit > 50 {
		return fmt.Errorf("muse: polling must be 1000-60000 ms with an error limit of 1-50")
	}
	if c.SendGapMinSeconds < 0 || c.SendGapMaxSeconds < c.SendGapMinSeconds || c.SendGapMaxSeconds > 600 {
		return fmt.Errorf("muse: send gap must be 0-600 seconds, with max >= min")
	}
	if c.SendTimeoutSeconds < 30 || c.SendTimeoutSeconds > 600 {
		return fmt.Errorf("muse: send timeout must be 30-600 seconds")
	}
	if c.MaxJobs < 10 || c.MaxJobs > 10000 {
		return fmt.Errorf("muse: maxJobs must be between 10 and 10000")
	}
	if c.MaxAccounts < 1 || c.MaxAccounts > 50 || c.TotalThreads < 1 || c.TotalThreads > 100 || c.ThreadsPerAccount < 1 || c.ThreadsPerAccount > 5 {
		return fmt.Errorf("muse: maxAccounts must be 1-50, totalThreads 1-100 and threadsPerAccount 1-5")
	}
	if !isOneOf(c.Mode, MuseModeRotate, MuseModeSequential) {
		return fmt.Errorf("muse: mode %q is not supported", c.Mode)
	}
	if c.UsagePauseMinutes < 1 || c.UsagePauseMinutes > 10080 {
		return fmt.Errorf("muse: usage pause must be between 1 and 10080 minutes")
	}
	return nil
}
