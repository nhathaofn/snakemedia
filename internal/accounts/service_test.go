package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/proxy"
)

// fakePool hands out browsers without Chrome and records what was asked.
type fakePool struct {
	mu      sync.Mutex
	root    string
	running map[string]*browser.Browser
	inUse   map[string]int
	wants   []browser.Want
	closed  []string
	removed []string
	// onClosed is what the service registered for browsers the pool reaps.
	onClosed func(string)
}

func (f *fakePool) Get(_ context.Context, profile string, want browser.Want) (*browser.Browser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wants = append(f.wants, want)
	b := &browser.Browser{ProfileDir: profile, Headless: !want.Visible}
	f.running[profile] = b
	return b, nil
}

func (f *fakePool) Acquire(profile string) func() {
	f.mu.Lock()
	f.inUse[profile]++
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.inUse[profile]--
		f.mu.Unlock()
	}
}

func (f *fakePool) InUse(profile string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inUse[profile] > 0
}

func (f *fakePool) Running(profile string) *browser.Browser {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[profile]
}

func (f *fakePool) CloseIfIdle(profile string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inUse[profile] > 0 {
		return browser.ErrProfileBusy
	}
	delete(f.running, profile)
	f.closed = append(f.closed, profile)
	return nil
}

func (f *fakePool) CloseAll() {}

func (f *fakePool) RemoveIfIdle(profile string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inUse[profile] > 0 {
		return browser.ErrProfileBusy
	}
	f.removed = append(f.removed, profile)
	return nil
}

func (f *fakePool) NewProfile() string { return filepath.Join(f.root, "p") }

func (f *fakePool) OnClosed(fn func(string)) {
	f.mu.Lock()
	f.onClosed = fn
	f.mu.Unlock()
}

func (f *fakePool) Reap(context.Context) {}

// fakeSite answers inspections from a script; the last answer repeats.
type fakeSite struct {
	mu      sync.Mutex
	answers []Inspection
	calls   int
}

func (f *fakeSite) Provider() Provider { return ProviderMuse }
func (f *fakeSite) Page() browser.Site {
	return browser.Site{Home: "https://example.test/"}
}
func (f *fakeSite) CookieDomain() string { return ".example.test" }
func (f *fakeSite) AcceptsCookie(domain string) bool {
	return strings.HasSuffix(domain, "example.test")
}
func (f *fakeSite) Inspect(context.Context, *browser.Browser, Record) (Inspection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := f.answers[min(f.calls, len(f.answers)-1)]
	f.calls++
	return answer, nil
}

func testService(t *testing.T, answers ...Inspection) (*Service, *fakePool, *fakeSite) {
	t.Helper()
	dir := t.TempDir()
	cfg, _ := config.NewManager(filepath.Join(dir, "config.json"))
	site := &fakeSite{answers: answers}
	s := New(filepath.Join(dir, "accounts"), cfg, site)
	s.notifyDelay = 0
	pool := &fakePool{root: filepath.Join(dir, "accounts", "profiles"), running: map[string]*browser.Browser{}, inUse: map[string]int{}}
	s.pool = pool
	// Registered after TempDir, so it runs first: background checks finish
	// before the folder is removed.
	t.Cleanup(s.Close)
	s.ensureTab = func(context.Context, int, config.AccountConfig, browser.Site) error { return nil }
	s.inject = func(context.Context, int, config.AccountConfig, []browser.Cookie) error { return nil }
	clock := time.Now()
	var clockMu sync.Mutex
	s.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	s.sleep = func(ctx context.Context, d time.Duration) error {
		clockMu.Lock()
		clock = clock.Add(d)
		clockMu.Unlock()
		return ctx.Err()
	}
	return s, pool, site
}

func seed(t *testing.T, s *Service, rec Record) Record {
	t.Helper()
	if rec.Provider == "" {
		rec.Provider = ProviderMuse
	}
	if rec.ProfileDir == "" {
		rec.ProfileDir = filepath.Join(t.TempDir(), rec.ID)
	}
	if rec.Proxy == "" {
		// Tests run direct unless they set a proxy: with an empty list an
		// account on the list has no route (TestEmptyListHasNoRoute).
		rec.Proxy = proxy.None
	}
	rec.Enabled = true
	if err := s.Seed(rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestCheckRecordsSessionUsageAndData(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true, Identity: "a@x", Usage: []UsageItem{{Label: "Credits", Value: "5"}}, Data: map[string]int{"n": 1}})
	seed(t, s, Record{Account: Account{ID: "a"}})
	res := s.Check(context.Background(), "a")
	if !res.Success || res.Data.Status != StatusSignedIn || res.Data.Identity != "a@x" || len(res.Data.Usage) != 1 {
		t.Fatalf("check = %+v", res)
	}
	rec, _ := s.Record("a")
	var data map[string]int
	if err := DecodeData(rec, &data); err != nil || data["n"] != 1 {
		t.Fatalf("provider data = %v, %v", data, err)
	}
}

func TestCheckSignedOutAfterPageReadyTimeout(t *testing.T) {
	s, _, site := testService(t, Inspection{})
	seed(t, s, Record{Account: Account{ID: "a", Source: SourceCookies}, Cookies: []browser.Cookie{{Name: "sid"}}})
	res := s.Check(context.Background(), "a")
	if !res.Success || res.Data.Status != StatusSignedOut || !strings.Contains(res.Data.Message, "cookie") {
		t.Fatalf("check = %+v", res)
	}
	if site.calls < 2 {
		t.Fatal("signed-out page was not polled until the deadline")
	}
}

// A session for another identity never overwrites the account.
func TestCheckRefusesAnotherIdentity(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true, Identity: "b@x"})
	seed(t, s, Record{Account: Account{ID: "a", Identity: "a@x", Status: StatusSignedIn}})
	if res := s.Check(context.Background(), "a"); res.Success {
		t.Fatal("another identity accepted")
	}
	rec, _ := s.Record("a")
	if rec.Identity != "a@x" || rec.Status != StatusError {
		t.Fatalf("account = %+v", rec.Account)
	}
}

// The same identity cannot be saved twice for one provider.
func TestCheckRefusesDuplicateIdentity(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true, Identity: "A@x"})
	seed(t, s, Record{Account: Account{ID: "first", Identity: "a@x", Status: StatusSignedIn}})
	seed(t, s, Record{Account: Account{ID: "second"}})
	if res := s.Check(context.Background(), "second"); res.Success {
		t.Fatal("duplicate identity accepted")
	}
}

func TestImportCookiesKeepsOnlySiteCookies(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx, nil)
	res := s.ImportCookies(ProviderMuse, "", `[{"name":"sid","value":"1","domain":".example.test"},{"name":"x","value":"2","domain":".evil.test"}]`)
	if !res.Success {
		t.Fatal(res.Error)
	}
	rec, _ := s.Record(res.Data.ID)
	if len(rec.Cookies) != 1 || rec.Cookies[0].Name != "sid" || rec.Name != "Muse 1" {
		t.Fatalf("record = %+v", rec)
	}
	if res := s.ImportCookies(ProviderMuse, "", `[{"name":"x","value":"2","domain":".evil.test"}]`); res.Success {
		t.Fatal("import without site cookies accepted")
	}
}

// Work holding a browser blocks a proxy change and removal.
func TestBusyAccountKeepsProxyAndProfile(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	rec := seed(t, s, Record{Account: Account{ID: "a"}})
	release := s.Acquire("a")
	if res := s.SetProxy("a", proxy.None); res.Success {
		t.Fatal("proxy changed under running work")
	}
	if res := s.Remove("a"); res.Success {
		t.Fatal("account removed under running work")
	}
	release()
	if res := s.SetProxy("a", proxy.None); !res.Success || res.Data.Proxy != proxy.None {
		t.Fatalf("set proxy = %+v", res)
	}
	if len(pool.closed) == 0 || pool.closed[0] != rec.ProfileDir {
		t.Fatal("browser kept running on the old route")
	}
}

func TestRemoveDeletesProfileAndTellsProviders(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	rec := seed(t, s, Record{Account: Account{ID: "a"}})
	var removed []string
	s.OnRemove(func(r Record) { removed = append(removed, r.ID) })
	var emitted []events.Event
	s.Start(context.Background(), func(e events.Event, _ any) { emitted = append(emitted, e) })
	if res := s.Remove("a"); !res.Success {
		t.Fatal(res.Error)
	}
	if len(pool.removed) != 1 || pool.removed[0] != rec.ProfileDir || len(removed) != 1 || len(emitted) == 0 {
		t.Fatalf("removed=%v hooks=%v events=%v", pool.removed, removed, emitted)
	}
	if _, err := s.Record("a"); !errors.Is(err, errNotFound) {
		t.Fatal("account still stored")
	}
}

// Every account browser starts on a route; direct still blocks WebRTC.
func TestBrowserUsesProxyRoute(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	seed(t, s, Record{Account: Account{ID: "a", Proxy: "1.2.3.4:8080"}})
	seed(t, s, Record{Account: Account{ID: "b"}})
	t.Cleanup(s.proxies.Close)
	if _, err := s.Browser(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Browser(context.Background(), "b"); err != nil {
		t.Fatal(err)
	}
	if u, err := s.ProxyURL("a"); pool.wants[0].Route != "1.2.3.4:8080" || !strings.HasPrefix(pool.wants[0].Args[0], "--proxy-server=http://127.0.0.1:") || u == nil || err != nil {
		t.Fatalf("proxied want = %+v, url %v, err %v", pool.wants[0], u, err)
	}
	if u, err := s.ProxyURL("b"); pool.wants[1].Route != proxy.DirectRoute || u != nil || err != nil {
		t.Fatalf("direct want = %+v, url %v, err %v", pool.wants[1], u, err)
	}
	rec, _ := s.Record("a")
	if rec.ProxyStatus != string(proxy.StatusLive) || rec.ProxyIP != "1.2.3.4" || rec.ProxyInUse == "" {
		t.Fatalf("proxy fields = %+v", rec.Account)
	}
}

// A provider's HTTP client never goes direct for an account whose browser has
// no recorded route: not started yet, or closed for a new proxy meanwhile.
func TestProxyURLWithoutRouteFails(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true})
	seed(t, s, Record{Account: Account{ID: "a", Proxy: "1.2.3.4:8080"}})
	t.Cleanup(s.proxies.Close)
	if u, err := s.ProxyURL("a"); err == nil || u != nil {
		t.Fatalf("no browser yet: url %v, err %v", u, err)
	}
	if _, err := s.Browser(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if res := s.SaveProxies(""); !res.Success {
		t.Fatal(res.Error)
	}
	if u, err := s.ProxyURL("a"); err == nil || u != nil {
		t.Fatalf("route dropped by a proxy change: url %v, err %v", u, err)
	}
}

// With an empty proxy list an account on the list gets no route at all, not
// the real IP; only "Không dùng proxy" goes direct.
func TestEmptyListHasNoRoute(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	if err := s.Seed(Record{Account: Account{ID: "a", Provider: ProviderMuse, Enabled: true}, ProfileDir: filepath.Join(t.TempDir(), "a")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Browser(context.Background(), "a"); err == nil || len(pool.wants) != 0 {
		t.Fatalf("browser started without a proxy: %v, wants %+v", err, pool.wants)
	}
}

// An account with no route is listed with the reason, and nothing changes:
// no browser, no saved status. "No proxy" or an explicit line routes.
func TestNoRouteIsListedWithoutSideEffects(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	if err := s.Seed(Record{Account: Account{ID: "auto", Provider: ProviderMuse}, ProfileDir: filepath.Join(t.TempDir(), "auto")}); err != nil {
		t.Fatal(err)
	}
	seed(t, s, Record{Account: Account{ID: "direct", Provider: ProviderMuse, Proxy: proxy.None}})
	seed(t, s, Record{Account: Account{ID: "own", Provider: ProviderMuse, Proxy: "1.2.3.4:8080"}})
	list, err := s.public(ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, a := range list {
		reasons[a.ID] = a.NoRoute
	}
	if reasons["auto"] == "" || reasons["direct"] != "" || reasons["own"] != "" {
		t.Fatalf("route reasons: %+v", reasons)
	}
	if rec, _ := s.Record("auto"); rec.NoRoute != "" || rec.ProxyStatus != "" || len(pool.wants) != 0 {
		t.Fatalf("listing changed the account: %+v, wants %+v", rec.Account, pool.wants)
	}
}

// Only a page that fails to load through the proxy marks it dead; the
// account shows "live" once a page has loaded through it.
func TestProxyMarkedDeadOnlyForProxyErrors(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true})
	seed(t, s, Record{Account: Account{ID: "a", Proxy: "1.2.3.4:8080"}})
	t.Cleanup(s.proxies.Close)
	s.ensureTab = func(context.Context, int, config.AccountConfig, browser.Site) error {
		return &browser.NavigateError{URL: "https://example.test/", Text: "net::ERR_NAME_NOT_RESOLVED"}
	}
	if _, err := s.Browser(context.Background(), "a"); err == nil {
		t.Fatal("failed page accepted")
	}
	if rec, _ := s.Record("a"); rec.ProxyStatus == string(proxy.StatusDead) || rec.ProxyStatus == string(proxy.StatusLive) {
		t.Fatalf("site failure changed proxy status to %q", rec.ProxyStatus)
	}
	s.ensureTab = func(context.Context, int, config.AccountConfig, browser.Site) error {
		return &browser.NavigateError{URL: "https://example.test/", Text: "net::ERR_PROXY_CONNECTION_FAILED"}
	}
	if _, err := s.Browser(context.Background(), "a"); err == nil {
		t.Fatal("failed proxy accepted")
	}
	if rec, _ := s.Record("a"); rec.ProxyStatus != string(proxy.StatusDead) {
		t.Fatalf("proxy failure not recorded: %q", rec.ProxyStatus)
	}
	s.ensureTab = func(context.Context, int, config.AccountConfig, browser.Site) error { return nil }
	if _, err := s.Browser(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.Record("a"); rec.ProxyStatus != string(proxy.StatusLive) {
		t.Fatalf("loaded page did not mark the proxy live: %q", rec.ProxyStatus)
	}
}

// A browser the pool closed while idle loses its route, so a provider's HTTP
// client can no longer use the proxy bridge it ran on.
func TestReapedBrowserForgetsRoute(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	rec := seed(t, s, Record{Account: Account{ID: "a", Proxy: "1.2.3.4:8080"}})
	t.Cleanup(s.proxies.Close)
	s.Start(context.Background(), nil)
	if _, err := s.Browser(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if u, err := s.ProxyURL("a"); u == nil || err != nil {
		t.Fatalf("route missing: %v %v", u, err)
	}
	pool.mu.Lock()
	onClosed := pool.onClosed
	pool.mu.Unlock()
	if onClosed == nil {
		t.Fatal("the service did not register for reaped browsers")
	}
	onClosed(rec.ProfileDir)
	if u, err := s.ProxyURL("a"); err == nil || u != nil {
		t.Fatalf("reaped browser kept its route: %v %v", u, err)
	}
}

// followSite adds a step after a sign-in in the window.
type followSite struct {
	*fakeSite
	followed int
}

func (f *followSite) AfterSignIn(_ context.Context, _ *browser.Browser, _ Record, insp Inspection) Inspection {
	f.followed++
	insp.Note = "setup not answered"
	return insp
}

// A sign-in in the window runs the site's follow-up step, and the account is
// signed in with its note; a plain check never runs it.
func TestSignInRunsFollowUpOnlyInTheWindow(t *testing.T) {
	s, _, base := testService(t, Inspection{SignedIn: true, Identity: "a@x"})
	site := &followSite{fakeSite: base}
	seed(t, s, Record{Account: Account{ID: "a", Status: StatusSigningIn}})
	s.watch(&browser.Browser{}, "a", site)
	rec, _ := s.Record("a")
	if site.followed != 1 || rec.Status != StatusSignedIn || rec.Message != "setup not answered" {
		t.Fatalf("after sign-in: followed %d, %+v", site.followed, rec.Account)
	}
	if res := s.Check(context.Background(), "a"); !res.Success || site.followed != 1 || res.Data.Message != "" {
		t.Fatalf("check ran the follow-up: %d %+v", site.followed, res)
	}
}
