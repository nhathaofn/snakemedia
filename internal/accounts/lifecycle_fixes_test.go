package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/proxy"
	"github.com/gorilla/websocket"
)

type renewTestSite struct {
	*fakeSite
	fn func(ctx context.Context, client *http.Client, rec Record) (RefreshResult, error)
}

func (s *renewTestSite) RefreshSession(ctx context.Context, client *http.Client, rec Record) (RefreshResult, error) {
	return s.fn(ctx, client, rec)
}

func TestSyncCookiesPersistsForBrowserSourceAndUpgradesSessionCookies(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true, Identity: "a@x"})
	rec := seed(t, s, Record{
		Account:    Account{ID: "b1", Provider: ProviderMuse, Source: SourceBrowser, Name: "Muse 1"},
		ProfileDir: filepath.Join(pool.root, "b1"),
	})
	if err := os.MkdirAll(rec.ProfileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	var injected [][]browser.Cookie
	s.inject = func(_ context.Context, _ int, _ config.AccountConfig, cookies []browser.Cookie) error {
		injected = append(injected, cookies)
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]browser.Target{{
			ID:                   "t1",
			Type:                 "page",
			URL:                  "https://muse.ai/",
			WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/1", port),
		}})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if conn.ReadJSON(&msg) != nil {
				return
			}
			res := map[string]any{}
			if msg.Method == "Network.getAllCookies" {
				res = map[string]any{"cookies": []map[string]any{
					{"name": "hatch_sess", "value": "ext-token", "domain": ".example.test", "path": "/", "expires": -1, "secure": true, "httpOnly": true},
					{"name": "other", "value": "skip", "domain": ".google.com", "path": "/", "expires": -1},
				}}
			}
			_ = conn.WriteJSON(map[string]any{"id": msg.ID, "result": res})
		}
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	defer srv.Close()

	s.SyncCookies(context.Background(), rec.ID, port)

	got, err := s.Record(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cookies) != 1 || got.Cookies[0].Name != "hatch_sess" || got.Cookies[0].Expires <= 0 {
		t.Fatalf("SourceBrowser SyncCookies did not persist promoted cookie: %+v", got.Cookies)
	}
	if got.CookieVersion == "" {
		t.Fatal("expected CookieVersion to be recorded")
	}
	marker, err := os.ReadFile(filepath.Join(rec.ProfileDir, cookieMarker))
	if err != nil || string(marker) != got.CookieVersion {
		t.Fatalf("cookieMarker = %q (err=%v), want %q", string(marker), err, got.CookieVersion)
	}
	if len(injected) != 1 || len(injected[0]) != 1 || injected[0][0].Name != "hatch_sess" || injected[0][0].Expires <= 0 {
		t.Fatalf("expected live Chrome session cookie promotion, got %+v", injected)
	}
}

func TestWaitSignedInStopsImmediatelyOnSignedOut(t *testing.T) {
	s, pool, base := testService(t, Inspection{SignedOut: true})
	cfg := s.cfg.Get()
	cfg.Accounts.PageReadySeconds = 60
	cfg.Accounts.PollIntervalMS = 5000
	if _, err := s.cfg.Update(cfg); err != nil {
		t.Fatal(err)
	}
	seed(t, s, Record{Account: Account{ID: "a", Source: SourceBrowser}})

	start := time.Now()
	res := s.Check(context.Background(), "a")
	elapsed := time.Since(start)
	if !res.Success || res.Data.Status != StatusSignedOut || base.calls != 1 {
		t.Fatalf("Check = %+v (inspect calls=%d), want single-poll SignedOut", res, base.calls)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("waitSignedIn waited too long (%v) on SignedOut", elapsed)
	}
	if len(pool.closed) != 1 {
		t.Fatalf("failed/signed-out headless browser should be closed immediately, closed=%v", pool.closed)
	}
}

func TestRenewDueSessionsCookieRotationAndProxyFailClosed(t *testing.T) {
	t.Run("rotates cookies and bumps CookieVersion without overwriting marker while Chrome is closed", func(t *testing.T) {
		s, pool, base := testService(t)
		now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
		s.now = func() time.Time { return now }

		called := 0
		s.sites[ProviderMuse] = &renewTestSite{
			fakeSite: base,
			fn: func(_ context.Context, client *http.Client, rec Record) (RefreshResult, error) {
				called++
				if client == nil {
					t.Fatal("expected HTTP client")
				}
				return RefreshResult{
					Cookies: []browser.Cookie{{Name: "hatch_sess", Value: "rotated", Domain: "muse.ai", Path: "/", Expires: float64(now.Add(72 * time.Hour).Unix())}},
				}, nil
			},
		}
		profile := filepath.Join(pool.root, "p1")
		_ = os.MkdirAll(profile, 0o700)
		_ = os.WriteFile(filepath.Join(profile, cookieMarker), []byte("v1"), 0o600)
		seed(t, s, Record{
			Account:       Account{ID: "a1", Provider: ProviderMuse, Source: SourceBrowser, Status: StatusSignedIn, Enabled: true, Proxy: proxy.None, LastCheckedAt: now.Add(-8 * time.Hour).UTC().Format(time.RFC3339)},
			ProfileDir:    profile,
			CookieVersion: "v1",
			Cookies:       []browser.Cookie{{Name: "hatch_sess", Value: "old", Domain: "muse.ai", Path: "/", Expires: float64(now.Add(48 * time.Hour).Unix())}},
		})

		s.RenewDueSessions(context.Background())
		if called != 1 {
			t.Fatalf("RefreshSession called %d times, want 1", called)
		}
		got, _ := s.Record("a1")
		if got.Cookies[0].Value != "rotated" || got.CookieVersion == "v1" || got.Status != StatusSignedIn {
			t.Fatalf("after rotate: %+v", got)
		}
		if marker, _ := os.ReadFile(filepath.Join(profile, cookieMarker)); string(marker) != "v1" {
			t.Fatalf("cookieMarker on disk changed to %q; want old v1 so next Chrome open re-injects rotated cookie", string(marker))
		}

		// Calling again immediately must respect cooldown (no spam).
		now = now.Add(5 * time.Minute)
		s.RenewDueSessions(context.Background())
		if called != 1 {
			t.Fatalf("expected cooldown to skip immediate second renew; calls=%d", called)
		}
	})

	t.Run("fails closed when automatic account has empty proxy list", func(t *testing.T) {
		s, pool, base := testService(t)
		now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
		s.now = func() time.Time { return now }

		called := 0
		s.sites[ProviderMuse] = &renewTestSite{
			fakeSite: base,
			fn: func(context.Context, *http.Client, Record) (RefreshResult, error) {
				called++
				return RefreshResult{}, nil
			},
		}
		if err := s.Seed(Record{
			Account:    Account{ID: "a-noproxy", Provider: ProviderMuse, Source: SourceBrowser, Status: StatusSignedIn, Enabled: true, Proxy: ""},
			ProfileDir: filepath.Join(pool.root, "np"),
			Cookies:    []browser.Cookie{{Name: "hatch_sess", Value: "old", Domain: "muse.ai"}},
		}); err != nil {
			t.Fatal(err)
		}

		s.RenewDueSessions(context.Background())
		if called != 0 {
			t.Fatalf("account with no proxy route must fail closed and never hit RefreshSession, called=%d", called)
		}
	})

	t.Run("marks SignedOut on explicit RefreshSession rejection and skips running browsers", func(t *testing.T) {
		s, pool, base := testService(t)
		now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
		s.now = func() time.Time { return now }

		var seen []string
		var mu sync.Mutex
		s.sites[ProviderMuse] = &renewTestSite{
			fakeSite: base,
			fn: func(_ context.Context, _ *http.Client, rec Record) (RefreshResult, error) {
				mu.Lock()
				seen = append(seen, rec.ID)
				mu.Unlock()
				return RefreshResult{SignedOut: true, Message: "Phiên Muse đã hết hạn."}, nil
			},
		}
		seed(t, s, Record{
			Account:    Account{ID: "closed", Provider: ProviderMuse, Source: SourceBrowser, Status: StatusSignedIn, Enabled: true, Proxy: proxy.None},
			ProfileDir: filepath.Join(pool.root, "closed"),
			Cookies:    []browser.Cookie{{Name: "hatch_sess", Value: "dead", Domain: "muse.ai"}},
		})
		runningProfile := filepath.Join(pool.root, "running")
		seed(t, s, Record{
			Account:    Account{ID: "running", Provider: ProviderMuse, Source: SourceBrowser, Status: StatusSignedIn, Enabled: true, Proxy: proxy.None},
			ProfileDir: runningProfile,
			Cookies:    []browser.Cookie{{Name: "hatch_sess", Value: "live", Domain: "muse.ai"}},
		})
		if _, err := pool.Get(context.Background(), runningProfile, browser.Want{Route: proxy.DirectRoute}); err != nil {
			t.Fatal(err)
		}

		s.RenewDueSessions(context.Background())
		if len(seen) != 1 || seen[0] != "closed" {
			t.Fatalf("seen = %v, want [closed] only", seen)
		}
		gotClosed, _ := s.Record("closed")
		if gotClosed.Status != StatusSignedOut || !strings.Contains(gotClosed.Message, "hết hạn") {
			t.Fatalf("expected SignedOut after explicit rejection, got %+v", gotClosed.Account)
		}
		gotRunning, _ := s.Record("running")
		if gotRunning.Status != StatusSignedIn {
			t.Fatalf("running browser account should stay SignedIn untouched, got %+v", gotRunning.Account)
		}
	})
}
