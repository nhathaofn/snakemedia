package chrome

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
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
		// Exercise discovery against Chrome's actual compiled scripts, including
		// a synthetic lazy script absent from document.scripts; no Google network.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(policy.RequestTimeoutSeconds)*time.Second)
		var pages []pageEntry
		if err := localJSON(ctx, b.Port, "/json", policy, &pages); err != nil {
			cancel()
			t.Fatal(err)
		}
		var pageWS string
		for _, page := range pages {
			if page.Type == "page" {
				pageWS = page.WebSocketDebuggerURL
				break
			}
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, pageWS, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if err := cdpCall(ctx, conn, 1, "Runtime.evaluate", map[string]any{"expression": `globalThis.testRegistry=[{hb:'/VideoFxService.GetCredits',Wa:'liveTestCredits'},{Wa:'liveTestAvailability',hb:'/VideoFxService.CheckFlowAvailability'}];
//# sourceURL=https://www.gstatic.com/runtime-account-test.js`}, nil); err != nil {
			t.Fatal(err)
		}
		registry, err := discoverRPCs(ctx, conn, "test-build-"+time.Now().String(), policy)
		_ = conn.Close()
		cancel()
		if err != nil || registry.Credits != "liveTestCredits" || registry.Availability != "liveTestAvailability" {
			t.Fatalf("compiled script discovery failed: %+v %v", registry, err)
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
