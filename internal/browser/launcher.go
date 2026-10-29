package browser

import (
	"fmt"
	"os"
	"os/exec"

	"SnakeMedia/internal/logger"
)

// LaunchOptions holds dynamic options for starting Chrome.
type LaunchOptions struct {
	ChromePath string
	ProfileDir string
	Port       int
	TargetURL  string
	Headless   bool
	ExtraArgs  []string
}

// BuildArgs constructs realistic command-line flags.
// Avoids automation banners (--enable-automation) while enabling remote debugging.
func BuildArgs(opts LaunchOptions) []string {
	args := []string{
		fmt.Sprintf("--user-data-dir=%s", opts.ProfileDir),
		fmt.Sprintf("--remote-debugging-port=%d", opts.Port),
		"--remote-debugging-address=127.0.0.1",
		"--no-first-run",
		"--no-default-browser-check",
		// Google background features the app never uses: they cost memory and
		// proxy traffic. Safe Browsing and other background networking stay on,
		// so the profile still looks like an ordinary browser to reCAPTCHA.
		"--disable-sync",
		"--disable-default-apps",
		"--disable-component-update",
		// The only --disable-features flag: Chrome keeps just the last one given.
		"--disable-features=Translate,OptimizationHints,MediaRouter",
	}
	if opts.Headless {
		args = append(args, "--headless=new", "--hide-scrollbars", "--mute-audio")
	}
	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}
	if opts.TargetURL != "" {
		args = append(args, opts.TargetURL)
	}
	return args
}

// Launch starts a detached Chrome process with the given options.
func Launch(opts LaunchOptions) (*exec.Cmd, error) {
	if err := os.MkdirAll(opts.ProfileDir, 0o700); err != nil {
		return nil, fmt.Errorf("chrome: create profile dir: %w", err)
	}

	args := BuildArgs(opts)
	cmd := exec.Command(opts.ChromePath, args...)
	configureCmd(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("chrome: start process: %w", err)
	}
	if err := bindToApp(cmd); err != nil {
		logger.Get().Warn("cannot tie Chrome to the app lifetime", "error", err)
	}

	return cmd, nil
}
