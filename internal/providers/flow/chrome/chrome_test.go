package chrome

import (
	"strings"
	"testing"
)

func TestBuildArgs_IncludesRealisticFlags(t *testing.T) {
	opts := LaunchOptions{
		ChromePath: "C:\\mock\\chrome.exe",
		ProfileDir: "C:\\mock\\profile",
		Port:       9999,
		TargetURL:  "https://flow.google.com",
	}

	args := BuildArgs(opts)
	argStr := strings.Join(args, " ")

	expected := []string{
		"--user-data-dir=C:\\mock\\profile",
		"--remote-debugging-port=9999",
		"--remote-debugging-address=127.0.0.1",
		"--no-first-run",
		"--no-default-browser-check",
		"https://flow.google.com",
	}

	for _, exp := range expected {
		if !strings.Contains(argStr, exp) {
			t.Errorf("BuildArgs missing expected flag: %s", exp)
		}
	}

	if strings.Contains(argStr, "--enable-automation") {
		t.Errorf("BuildArgs should NOT contain --enable-automation")
	}
	if strings.Contains(argStr, "--remote-allow-origins") || strings.Contains(argStr, "--disable-background") {
		t.Fatal("unsafe or resource-heavy browser flags")
	}
}
