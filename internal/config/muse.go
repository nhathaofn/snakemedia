package config

import (
	"fmt"
	"unicode/utf8"
)

// MuseMode is how queued jobs are spread over accounts.
type MuseMode string

const (
	// MuseModeRotate prefers the least busy account, then shares jobs out in
	// step with the quota each account has left.
	MuseModeRotate MuseMode = "rotate"
	// MuseModeSequential uses accounts in their saved order.
	MuseModeSequential MuseMode = "sequential"
)

// AllMuseModes is bound to the frontend as a TypeScript enum (config.MuseMode).
var AllMuseModes = []struct {
	Value  MuseMode
	TSName string
}{{MuseModeRotate, "Rotate"}, {MuseModeSequential, "Sequential"}}

// ImageFormat is the file type images are saved as for the user.
type ImageFormat string

const (
	ImageFormatPNG ImageFormat = "png"
	ImageFormatJPG ImageFormat = "jpg"
	// ImageFormatOriginal keeps the file as the provider made it.
	ImageFormatOriginal ImageFormat = "original"
)

// AllImageFormats is bound to the frontend as a TypeScript enum (config.ImageFormat).
var AllImageFormats = []struct {
	Value  ImageFormat
	TSName string
}{{ImageFormatPNG, "PNG"}, {ImageFormatJPG, "JPG"}, {ImageFormatOriginal, "Original"}}

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
	// ThreadsPerAccount caps the jobs one account runs at once. Every account
	// that is signed in and can work takes jobs; there is no account count to
	// set (an account added, signed out or resting joins or leaves on its own).
	ThreadsPerAccount int      `json:"threadsPerAccount"`
	Mode              MuseMode `json:"mode"`
	// UseTopup lets an account go on with its additional tokens once its
	// weekly usage is gone; off rests it until the weekly reset.
	UseTopup bool `json:"useTopup"`
	// UsagePauseMinutes rests an account after Muse replies that it hit a limit
	// while its weekly reset time is unknown.
	UsagePauseMinutes int `json:"usagePauseMinutes"`
	// OutputDir is the folder the last batch was saved into (chosen in the
	// batch form); empty uses the app's default folder (paths.MuseOutputDir).
	// Single jobs are not saved; their files are downloaded by hand.
	OutputDir string `json:"outputDir"`
	// SetupMessage is sent in a chat of its own on every Muse account once it
	// is signed in; the account counts as signed in only once Muse has replied.
	// A changed message is sent again; empty sends nothing.
	SetupMessage string `json:"setupMessage"`
	// ImageFormat is what Muse images (made as WebP) are saved as, in a batch
	// folder or by the download button.
	ImageFormat ImageFormat `json:"imageFormat"`
}

// MaxMuseSetupMessage caps the setup message, in characters.
const MaxMuseSetupMessage = 8000

// defaultMuseSetupMessage asks Muse to treat every generation request on its
// own, so nothing from earlier outputs leaks into a new one.
const defaultMuseSetupMessage = `Please save the following rule to this account's long-term / permanent memory and apply it across the whole account, in every conversation and every future generation, until I cancel or change it myself:

GLOBAL RULE: Every request I make to create an image, a video or any other new output is a completely independent request. Never use, reference or infer anything from any output created earlier on your own, including subject, characters, style, aspect ratio, duration, resolution and old files, even when my generation prompt does not mention this rule. Use only the parameters in the current request. Reuse an earlier output only when I explicitly say in that request that I want to continue, edit or reuse it.

After saving it, please confirm: where you saved it, what its scope is, and whether I need to repeat this rule in later generations.`

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
		ThreadsPerAccount:   1,
		Mode:                MuseModeRotate,
		UseTopup:            true,
		UsagePauseMinutes:   60,
		SetupMessage:        defaultMuseSetupMessage,
		ImageFormat:         ImageFormatPNG,
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
	if c.ThreadsPerAccount < 1 || c.ThreadsPerAccount > 5 {
		return fmt.Errorf("muse: threadsPerAccount must be between 1 and 5")
	}
	if !isOneOf(c.Mode, MuseModeRotate, MuseModeSequential) {
		return fmt.Errorf("muse: mode %q is not supported", c.Mode)
	}
	if c.UsagePauseMinutes < 1 || c.UsagePauseMinutes > 10080 {
		return fmt.Errorf("muse: usage pause must be between 1 and 10080 minutes")
	}
	if !isOneOf(c.ImageFormat, ImageFormatPNG, ImageFormatJPG, ImageFormatOriginal) {
		return fmt.Errorf("muse: image format %q is not supported", c.ImageFormat)
	}
	if utf8.RuneCountInString(c.SetupMessage) > MaxMuseSetupMessage {
		return fmt.Errorf("muse: setup message must be at most %d characters", MaxMuseSetupMessage)
	}
	return nil
}
