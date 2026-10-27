package browser

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// ErrChromeNotFound is returned when no valid Google Chrome executable is found.
var ErrChromeNotFound = errors.New("chrome: executable not found")

// FindChrome locates the Google Chrome executable on the host system without hardcoding paths.
func FindChrome() (string, error) {
	// 1. Environment variable overrides
	for _, env := range []string{"CHROME_PATH", "CHROME_BIN", "FLOW_CHROME_PATH"} {
		if path := os.Getenv(env); path != "" {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// 2. OS-specific lookup
	if runtime.GOOS == "windows" {
		if path := findWindowsChrome(); path != "" {
			return path, nil
		}
	} else if runtime.GOOS == "darwin" {
		const macPath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(macPath); err == nil {
			return macPath, nil
		}
	}

	// 3. Fallback to standard PATH lookup
	for _, bin := range []string{"chrome", "google-chrome", "google-chrome-stable", "chromium-browser", "chromium"} {
		if path, err := exec.LookPath(bin); err == nil {
			return path, nil
		}
	}

	return "", ErrChromeNotFound
}
