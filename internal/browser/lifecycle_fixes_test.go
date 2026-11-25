package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

func startTestDevTools(t *testing.T, ua string) (port int, captured func() (uaOverrides []string, cookieBatches [][]map[string]any, navs []string)) {
	t.Helper()
	var (
		mu      sync.Mutex
		uaList  []string
		cookies [][]map[string]any
		navList []string
	)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Target{{
			ID:                   "tab-1",
			Type:                 "page",
			URL:                  "about:blank",
			WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/1", port),
		}})
	})
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(browserVersion{
			Browser:   "Chrome/999.0.0.0",
			UserAgent: ua,
			WebSocket: fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/1", port),
		})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if conn.ReadJSON(&msg) != nil {
				return
			}
			mu.Lock()
			switch msg.Method {
			case "Emulation.setUserAgentOverride":
				var p struct {
					UserAgent string `json:"userAgent"`
				}
				_ = json.Unmarshal(msg.Params, &p)
				uaList = append(uaList, p.UserAgent)
			case "Network.setCookies":
				var p struct {
					Cookies []map[string]any `json:"cookies"`
				}
				_ = json.Unmarshal(msg.Params, &p)
				cookies = append(cookies, p.Cookies)
			case "Page.navigate":
				var p struct {
					URL string `json:"url"`
				}
				_ = json.Unmarshal(msg.Params, &p)
				navList = append(navList, p.URL)
			}
			mu.Unlock()
			result := map[string]any{}
			if msg.Method == "Page.navigate" {
				result = map[string]any{"frameId": "f1"}
			}
			_ = conn.WriteJSON(map[string]any{"id": msg.ID, "result": result})
		}
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	return port, func() ([]string, [][]map[string]any, []string) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, uaList...), append([][]map[string]any{}, cookies...), append([]string{}, navList...)
	}
}

func TestPersistAndSessionOnlyCookies(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	fixedExp := float64(now.Add(48 * time.Hour).Unix())
	in := []Cookie{
		{Name: "hatch_sess", Value: "abc", Domain: "muse.ai", Expires: -1},
		{Name: "__Host-csrf", Value: "xyz", Domain: "muse.ai", Expires: 0},
		{Name: "kept", Value: "123", Domain: "muse.ai", Expires: fixedExp},
	}

	sessOnly := SessionOnlyCookies(in)
	if len(sessOnly) != 2 || sessOnly[0].Name != "hatch_sess" || sessOnly[1].Name != "__Host-csrf" {
		t.Fatalf("SessionOnlyCookies = %+v", sessOnly)
	}

	persisted := PersistSessionCookies(in, now, DefaultSessionCookieTTL)
	wantExp := float64(now.Add(DefaultSessionCookieTTL).Unix())
	if persisted[0].Expires != wantExp || persisted[1].Expires != wantExp || persisted[2].Expires != fixedExp {
		t.Fatalf("PersistSessionCookies = %+v, want session cookies promoted to %v", persisted, wantExp)
	}
	if in[0].Expires != -1 {
		t.Fatalf("PersistSessionCookies mutated caller slice: %+v", in)
	}
}

func TestInjectCookiesHandlesHostAndSecurePrefixes(t *testing.T) {
	port, captured := startTestDevTools(t, "Mozilla/5.0 Chrome/999.0.0.0")
	policy := config.DefaultAccountConfig()
	err := InjectCookies(context.Background(), port, policy, []Cookie{
		{Name: "__Host-next-auth.csrf-token", Value: "tok", Domain: ".muse.ai", Path: "/api", Secure: false, HTTPOnly: true},
		{Name: "__Secure-next-auth.session-token", Value: "sess", Domain: ".muse.ai", Path: "", Secure: false, HTTPOnly: true},
		{Name: "hatch_sess", Value: "val", Domain: "muse.ai", Path: "/", Secure: true, HTTPOnly: true, Expires: 1800000000},
	})
	if err != nil {
		t.Fatalf("InjectCookies failed: %v", err)
	}
	_, batches, _ := captured()
	if len(batches) != 1 || len(batches[0]) != 3 {
		t.Fatalf("captured cookie batches = %+v", batches)
	}
	hostCookie := batches[0][0]
	if _, hasDomain := hostCookie["domain"]; hasDomain {
		t.Fatalf("__Host- cookie must omit domain: %+v", hostCookie)
	}
	if hostCookie["url"] != "https://muse.ai/" || hostCookie["path"] != "/" || hostCookie["secure"] != true {
		t.Fatalf("bad __Host- attributes: %+v", hostCookie)
	}
	secCookie := batches[0][1]
	if secCookie["domain"] != ".muse.ai" || secCookie["path"] != "/" || secCookie["secure"] != true {
		t.Fatalf("bad __Secure- attributes: %+v", secCookie)
	}
	normal := batches[0][2]
	if normal["domain"] != "muse.ai" || normal["expires"] != float64(1800000000) {
		t.Fatalf("bad regular cookie attributes: %+v", normal)
	}
}

func TestNavigateNormalizesHeadlessUserAgent(t *testing.T) {
	headlessUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/999.0.0.0 Safari/537.36"
	wantUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/999.0.0.0 Safari/537.36"
	port, captured := startTestDevTools(t, headlessUA)
	err := Navigate(context.Background(), Target{
		ID:                   "tab-1",
		Type:                 "page",
		WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/1", port),
	}, port, Site{Home: "https://muse.ai/"})
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}
	uas, _, navs := captured()
	if len(uas) != 1 || uas[0] != wantUA {
		t.Fatalf("Emulation.setUserAgentOverride = %v, want [%q]", uas, wantUA)
	}
	if len(navs) != 1 || navs[0] != "https://muse.ai/" {
		t.Fatalf("Page.navigate = %v", navs)
	}
	if got := LastUserAgent(); got != wantUA {
		t.Fatalf("LastUserAgent() = %q, want %q", got, wantUA)
	}
}

func TestPoolWatchExitCleansUpClosedBrowser(t *testing.T) {
	pool, profile := testPool(t)
	doneCh := make(chan struct{})
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		return &Browser{ProfileDir: opts.ProfileDir, Headless: opts.Headless, done: doneCh, cmd: launchedCmd()}, nil
	}
	closedCh := make(chan string, 1)
	pool.OnClosed(func(dir string) { closedCh <- dir })

	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct", Visible: true}); err != nil {
		t.Fatal(err)
	}
	if pool.Running(profile) == nil {
		t.Fatal("expected running browser before exit")
	}

	close(doneCh)
	select {
	case dir := <-closedCh:
		if dir != profile {
			t.Fatalf("OnClosed dir = %q, want %q", dir, profile)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watchExit OnClosed notification")
	}
	if pool.Running(profile) != nil {
		t.Fatal("expected exited browser to be removed from Running map")
	}
}

func TestPoolReapIdleInvokesOnClosingBeforeShutdown(t *testing.T) {
	pool, profile := testPool(t)
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		return &Browser{Port: 45555, ProfileDir: opts.ProfileDir, Headless: opts.Headless, done: make(chan struct{}), cmd: launchedCmd()}, nil
	}
	now := time.Now()
	pool.now = func() time.Time { return now }

	var order []string
	var closedPort atomic.Int32
	pool.OnClosing(func(dir string, b *Browser) {
		if dir == profile && b != nil {
			closedPort.Store(int32(b.Port))
			order = append(order, "closing")
		}
	})
	pool.shutdown = func(*Browser, config.AccountConfig) error {
		order = append(order, "shutdown")
		return nil
	}
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct"}); err != nil {
		t.Fatal(err)
	}
	idle := time.Duration(config.DefaultAccountConfig().IdleBrowserMinutes) * time.Minute
	pool.now = func() time.Time { return now.Add(idle + time.Second) }
	if n := pool.reapIdle(); n != 1 {
		t.Fatalf("reapIdle = %d, want 1", n)
	}
	if closedPort.Load() != 45555 || len(order) != 2 || order[0] != "closing" || order[1] != "shutdown" {
		t.Fatalf("order = %v port = %d, want [closing shutdown] on port 45555", order, closedPort.Load())
	}
}

func TestNavigateWithPolicyHonorsConfiguredTimeout(t *testing.T) {
	// DevTools /json/version stalls longer than RequestTimeoutSeconds while Page.navigate succeeds.
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			_, _ = w.Write([]byte(`{"User-Agent":"Chrome/1.0"}`))
		case <-r.Context().Done():
		}
	})
	up := websocket.Upgrader{}
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				ID int `json:"id"`
			}
			_ = json.Unmarshal(raw, &msg)
			resp, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{}})
			if err := c.WriteMessage(websocket.TextMessage, resp); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	policy := config.DefaultAccountConfig()
	policy.RequestTimeoutSeconds = 1
	policy.BrowserStartupTimeoutSeconds = 3

	start := time.Now()
	err := NavigateWithPolicy(context.Background(), Target{
		ID:                   "t1",
		Type:                 "page",
		WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/1", port),
	}, port, Site{Home: "https://muse.ai/"}, policy)
	if err != nil {
		t.Fatalf("NavigateWithPolicy failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Fatalf("expected custom RequestTimeoutSeconds=1 to abort /json/version quickly, took %v", elapsed)
	}
}
