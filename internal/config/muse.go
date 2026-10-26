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

// MuseConfig owns the adjustable muse.ai timing and retry policy. Protocol
// details (page script, RPC names) belong to the provider adapter.
type MuseConfig struct {
	// Headless runs account browsers without a window; sign-in always opens one.
	Headless bool `json:"headless"`
	// MaxAttempts bounds how many times one job may be sent or re-checked.
	MaxAttempts int `json:"maxAttempts"`
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
	// SendTimeoutSeconds bounds one send, which waits for Muse to prepare its machine.
	SendTimeoutSeconds int `json:"sendTimeoutSeconds"`
	// PageReadySeconds bounds waiting for a freshly opened muse.ai tab to sign in.
	PageReadySeconds int `json:"pageReadySeconds"`
	// LoginTimeoutMinutes bounds how long a sign-in window waits for the user.
	LoginTimeoutMinutes int `json:"loginTimeoutMinutes"`
	// MaxAttachmentBytes is Muse's limit on the total size of attached images.
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes"`
	// MaxDownloadBytes caps one downloaded result file.
	MaxDownloadBytes int64 `json:"maxDownloadBytes"`
	// MaxJobs caps the journal; the oldest finished jobs are dropped first.
	MaxJobs int `json:"maxJobs"`
	// MaxAccounts caps how many accounts work at once; TotalThreads caps jobs
	// across all accounts and ThreadsPerAccount jobs on one account.
	MaxAccounts       int      `json:"maxAccounts"`
	TotalThreads      int      `json:"totalThreads"`
	ThreadsPerAccount int      `json:"threadsPerAccount"`
	Mode              MuseMode `json:"mode"`
	// UseTopup keeps an account working on top-up tokens after its weekly quota.
	UseTopup bool `json:"useTopup"`
	// KeepGoing ignores the quota entirely and lets Muse decide.
	KeepGoing bool `json:"keepGoing"`
	// UsagePauseMinutes rests an account after Muse replies that it hit a limit
	// while its quota could not be read.
	UsagePauseMinutes int `json:"usagePauseMinutes"`
	// AutoSave copies every finished file into OutputDir (a dated sub-folder).
	AutoSave bool `json:"autoSave"`
	// OutputDir is where finished files are copied; empty uses the user's
	// Videos folder.
	OutputDir string `json:"outputDir"`
	// FFmpegPath points at ffmpeg for merging scenes and music; empty looks it
	// up next to the app and on PATH. ffprobe is expected beside it.
	FFmpegPath string `json:"ffmpegPath"`
	// MaxSourceVideoBytes caps a source video sent for video-to-video.
	MaxSourceVideoBytes int64 `json:"maxSourceVideoBytes"`
	// StopAfterFailures pauses new productions after this many fail in a row,
	// which usually means a shared problem (signed out, out of quota).
	StopAfterFailures int `json:"stopAfterFailures"`
	// MaxRechecks bounds how often a job Muse already accepted is read back
	// after transport errors before it is handed to the user. It is separate
	// from MaxAttempts because sending again would pay twice.
	MaxRechecks int `json:"maxRechecks"`
}

func DefaultMuseConfig() MuseConfig {
	return MuseConfig{
		Headless:            true,
		MaxAttempts:         2,
		ImageTimeoutMinutes: 5,
		VideoTimeoutMinutes: 30,
		PollIntervalMS:      4000,
		PollErrorLimit:      5,
		SendGapMinSeconds:   15,
		SendGapMaxSeconds:   20,
		SendTimeoutSeconds:  180,
		PageReadySeconds:    60,
		LoginTimeoutMinutes: 15,
		MaxAttachmentBytes:  8 << 20,
		MaxDownloadBytes:    1 << 30,
		MaxJobs:             500,
		MaxAccounts:         2,
		TotalThreads:        2,
		ThreadsPerAccount:   1,
		Mode:                MuseModeRotate,
		UseTopup:            true,
		UsagePauseMinutes:   60,
		AutoSave:            true,
		MaxSourceVideoBytes: 100 << 20,
		StopAfterFailures:   2,
		MaxRechecks:         10,
	}
}

func (c MuseConfig) Validate() error {
	if c.MaxAttempts < 1 || c.MaxAttempts > 5 {
		return fmt.Errorf("muse: maxAttempts must be between 1 and 5")
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
	if c.SendTimeoutSeconds < 30 || c.SendTimeoutSeconds > 600 || c.PageReadySeconds < 10 || c.PageReadySeconds > 600 {
		return fmt.Errorf("muse: send timeout must be 30-600 seconds and page readiness 10-600 seconds")
	}
	if c.LoginTimeoutMinutes < 1 || c.LoginTimeoutMinutes > 120 {
		return fmt.Errorf("muse: login timeout must be between 1 and 120 minutes")
	}
	if c.MaxAttachmentBytes < 1<<20 || c.MaxAttachmentBytes > 64<<20 || c.MaxDownloadBytes < 1<<20 || c.MaxDownloadBytes > 8<<30 {
		return fmt.Errorf("muse: invalid attachment or download size limit")
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
	if c.MaxSourceVideoBytes < 1<<20 || c.MaxSourceVideoBytes > 2<<30 {
		return fmt.Errorf("muse: source video limit must be between 1 MB and 2 GB")
	}
	if c.MaxRechecks < 1 || c.MaxRechecks > 100 {
		return fmt.Errorf("muse: maxRechecks must be between 1 and 100")
	}
	if c.StopAfterFailures < 0 || c.StopAfterFailures > 100 {
		return fmt.Errorf("muse: stopAfterFailures must be between 0 and 100")
	}
	return nil
}
