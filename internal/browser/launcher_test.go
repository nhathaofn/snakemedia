package browser

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
		ExtraArgs:  []string{"--proxy-server=http://127.0.0.1:8888"},
	}

	args := BuildArgs(opts)
	argStr := strings.Join(args, " ")

	expected := []string{
		"--user-data-dir=C:\\mock\\profile",
		"--remote-debugging-port=9999",
		"--remote-debugging-address=127.0.0.1",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-default-apps",
		"--disable-component-update",
		"--disable-features=Translate,OptimizationHints,MediaRouter",
		"--proxy-server=http://127.0.0.1:8888",
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
	if strings.Contains(argStr, "--disable-blink-features") {
		t.Errorf("BuildArgs should NOT contain --disable-blink-features (triggers Chrome unsupported-flag infobar)")
	}
	if strings.Contains(argStr, "--remote-allow-origins") || strings.Contains(argStr, "--disable-background") ||
		strings.Contains(argStr, "--max-old-space-size") || strings.Count(argStr, "--disable-features") > 1 {
		t.Fatal("unsafe or resource-heavy browser flags")
	}

	headlessArgs := strings.Join(BuildArgs(LaunchOptions{ChromePath: opts.ChromePath, ProfileDir: opts.ProfileDir, Port: opts.Port, Headless: true}), " ")
	if !strings.Contains(headlessArgs, "--headless=new") || !strings.Contains(headlessArgs, "--window-size=1366,768") {
		t.Fatalf("headless args missing expected viewport/headless flags: %s", headlessArgs)
	}
}
