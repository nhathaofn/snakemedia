package chrome

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// LaunchOptions holds dynamic options for starting Chrome.
type LaunchOptions struct {
	ChromePath string
	ProfileDir string
	Port       int
	TargetURL  string
	Headless   bool
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
	}
	if opts.Headless {
		args = append(args, "--headless=new", "--hide-scrollbars", "--mute-audio")
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

	return cmd, nil
}

// IsPortOpen checks if the Chrome CDP endpoint responds on the given port.
func IsPortOpen(ctx context.Context, port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
