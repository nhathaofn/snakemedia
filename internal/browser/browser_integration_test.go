package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
)

// Opt-in lifecycle smoke test. It uses about:blank and a temporary profile, never
// a saved Google session, user cookies or a live authentication RPC.
func TestBrowserLifecycle(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_TEST_CHROME") == "" {
		t.Skip("set SNAKEMEDIA_TEST_CHROME to run local Chrome smoke test")
	}
	path, err := FindChrome()
	if err != nil {
		t.Fatal(err)
	}
	policy := config.DefaultAccountConfig()
	options := LaunchOptions{ChromePath: path, ProfileDir: t.TempDir(), Headless: true, TargetURL: "about:blank"}
	for i := 0; i < 2; i++ {
		b, err := StartBrowser(context.Background(), options, policy)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Close(policy) })
		if b.Port == 0 || b.Version == "" {
			t.Fatal("dynamic browser endpoint not reported")
		}
		if err := b.Validate(context.Background(), policy); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(b.cmd.Args, " "), "--remote-debugging-port=0") {
			t.Fatal("Chrome received automation-triggering port zero")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(policy.RequestTimeoutSeconds)*time.Second)
		targets, err := Targets(ctx, b.Port, policy)
		cancel()
		if err != nil || len(targets) == 0 {
			t.Fatalf("tab listing failed: %v", err)
		}
		attached, err := StartBrowser(context.Background(), options, policy)
		if err != nil || attached.Port != b.Port || attached.cmd != nil {
			t.Fatalf("profile endpoint reconnection failed: %v", err)
		}
		if err := attached.Close(policy); err != nil {
			t.Fatal(err)
		}
		if err := b.Close(policy); err != nil {
			t.Fatal(err)
		}
	}
}
