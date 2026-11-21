package muse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
)

func TestMuseSiteRefreshSession(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

	t.Run("rotates cookies via Set-Cookie on HTTP 200 JSON", func(t *testing.T) {
		var gotUA, gotCookie, gotReferer string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			gotCookie = r.Header.Get("Cookie")
			gotReferer = r.Header.Get("Referer")
			http.SetCookie(w, &http.Cookie{
				Name:     "hatch_sess",
				Value:    "fresh-sess",
				Domain:   ".muse.ai",
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"active"}`))
		}))
		defer srv.Close()

		cfg, _ := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
		site := NewSite(cfg)
		site.now = func() time.Time { return now }
		site.sessionURL = srv.URL + "/api/session"
		site.userAgent = func() string { return "TestChrome/999" }

		rec := accounts.Record{
			Cookies: []browser.Cookie{
				{Name: "hatch_sess", Value: "old-sess", Domain: "127.0.0.1", Path: "/", Expires: float64(now.Add(24 * time.Hour).Unix())},
				{Name: "other", Value: "ignored", Domain: "google.com", Path: "/", Expires: float64(now.Add(24 * time.Hour).Unix())},
			},
		}
		res, err := site.RefreshSession(context.Background(), srv.Client(), rec)
		if err != nil {
			t.Fatalf("RefreshSession error = %v", err)
		}
		if res.SignedOut || gotUA != "TestChrome/999" || gotReferer != MuseURL {
			t.Fatalf("unexpected res=%+v ua=%q ref=%q", res, gotUA, gotReferer)
		}
		if !strings.Contains(gotCookie, "hatch_sess=old-sess") || strings.Contains(gotCookie, "other=") {
			t.Fatalf("unexpected Cookie header sent: %q", gotCookie)
		}
		// Ensure merged cookies include rotated hatch_sess with persistent expiry (TTL >= now).
		var found bool
		for _, c := range res.Cookies {
			if c.Name == "hatch_sess" && c.Value == "fresh-sess" {
				found = true
				if c.Expires <= float64(now.Unix()) || !c.HTTPOnly || !c.Secure {
					t.Fatalf("merged hatch_sess = %+v", c)
				}
			}
		}
		if !found {
			t.Fatalf("rotated hatch_sess missing in merged cookies: %+v", res.Cookies)
		}
	})

	t.Run("marks SignedOut on HTTP 401 JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		}))
		defer srv.Close()

		cfg, _ := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
		site := NewSite(cfg)
		site.now = func() time.Time { return now }
		site.sessionURL = srv.URL + "/api/session"

		rec := accounts.Record{Cookies: []browser.Cookie{{Name: "hatch_sess", Value: "expired-on-server", Domain: "127.0.0.1", Path: "/"}}}
		res, err := site.RefreshSession(context.Background(), srv.Client(), rec)
		if err != nil || !res.SignedOut {
			t.Fatalf("expected SignedOut on 401 JSON, got res=%+v err=%v", res, err)
		}
	})

	t.Run("treats Cloudflare challenge HTML as transient error not SignedOut", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<!doctype html><html><body>Just a moment...</body></html>`))
		}))
		defer srv.Close()

		cfg, _ := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
		site := NewSite(cfg)
		site.now = func() time.Time { return now }
		site.sessionURL = srv.URL + "/api/session"

		rec := accounts.Record{Cookies: []browser.Cookie{{Name: "hatch_sess", Value: "still-valid", Domain: "127.0.0.1", Path: "/"}}}
		res, err := site.RefreshSession(context.Background(), srv.Client(), rec)
		if err == nil || res.SignedOut {
			t.Fatalf("Cloudflare HTML challenge must return error and NOT SignedOut: res=%+v err=%v", res, err)
		}
	})
}

func TestSiteInspectDoesNotReloadOnGatewayDuringConnectAndReportsSignedOutOn401(t *testing.T) {
	t.Run("logged-in with connecting gateway does not trigger Home reload", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{quota: &MuseQuota{PercentUsed: 12}},
			states: []LoginState{
				{HTTP: 200, Chat: true, LoggedIn: true, Gateway: "gateway_unreachable", Path: "/"},
			},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())

		insp, err := s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m1"}})
		if err != nil || !insp.SignedIn || page.homes != 0 {
			t.Fatalf("want SignedIn with 0 Home reloads when already logged in at /, got insp=%+v err=%v homes=%d", insp, err, page.homes)
		}
	})

	t.Run("headless 401 sets SignedOut immediately after Home retry", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{},
			states: []LoginState{
				{HTTP: 401, Chat: false, Path: "/"},
			},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())

		insp, err := s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m2"}})
		if err != nil || insp.SignedIn || !insp.SignedOut || page.homes != 1 {
			t.Fatalf("want SignedOut=true after Home retry, got insp=%+v err=%v homes=%d", insp, err, page.homes)
		}
	})

	t.Run("interactive sign-in keeps SignedOut false on 401", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{},
			states:   []LoginState{{HTTP: 401, Chat: false, Path: "/"}},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())

		insp, err := s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: false}, accounts.Record{Account: accounts.Account{ID: "m3", Status: accounts.StatusSigningIn}})
		if err != nil || insp.SignedIn || insp.SignedOut {
			t.Fatalf("interactive sign-in must keep SignedOut=false, got insp=%+v err=%v", insp, err)
		}
	})
}

func TestPageScriptChunkScanCoalescesConcurrentFetches(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
const store = new Map();
global.localStorage = {
  getItem: (k) => store.has(k) ? store.get(k) : null,
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: (k) => store.delete(k),
  get length() { return store.size; },
  key: (i) => Array.from(store.keys())[i] ?? null,
};
global.location = { pathname: '/', origin: 'https://muse.ai', href: 'https://muse.ai/' };
const scriptEls = [
  { src: 'https://muse.ai/_next/static/chunks/app.js' },
  { src: 'https://muse.ai/_next/static/chunks/other.js' },
];
global.performance = {
  getEntriesByType: (t) => (t === 'resource' ? scriptEls.map((e) => ({ name: e.src })) : []),
};
global.document = {
  body: {},
  scripts: scriptEls,
  querySelectorAll: () => scriptEls,
};
global.window = {
  __NEXT_DATA__: { buildId: 'b123' },
  __hatchEarlyGatewayRuntimeState: { activeRuntime: { getSnapshot: () => ({ isConnected: true }), sendRequest: async () => ({}) } },
};
const chunkFetches = new Map();
const chunks = {
  'https://muse.ai/_next/static/chunks/app.js': '"static/chunks/lazy1.js";const x={maxFileSize:5242880,maxFilesPerUpload:4,supportedMimeTypes:["image/png"]};',
  'https://muse.ai/_next/static/chunks/other.js': 'console.log("other");',
  'https://muse.ai/_next/static/chunks/lazy1.js': '(0,t.createServerReference)("0123456789abcdef0123456789abcdef",a,"fetchSubscriptionAction")',
};
global.fetch = async (url, opts = {}) => {
  if (chunks[url]) {
    chunkFetches.set(url, (chunkFetches.get(url) || 0) + 1);
    return { ok: true, status: 200, text: async () => chunks[url] };
  }
  return {
    ok: true,
    status: 200,
    headers: { get: () => 'text/x-component' },
    text: async () => '1:{"success":true,"subscription":{"tier":"free","percentUsed":7}}\n',
  };
};
const page = (` + pageSource + `)(1024);
(async () => {
  const [q, caps] = await Promise.all([page.quota(), page.capabilities()]);
  assert.equal(q.tier, 'free');
  assert.equal(caps.upload.maxFilesPerUpload, 4);
  assert.equal(chunkFetches.size, 3);
  for (const [u, n] of chunkFetches.entries()) {
    assert.equal(n, 1, 'chunk fetched more than once: ' + u + ' (' + n + ')');
  }
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page scan coalesce test failed: %v\n%s", err, out)
	}
}
