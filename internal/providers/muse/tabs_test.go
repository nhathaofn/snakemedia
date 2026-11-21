package muse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/proxy"
)

type fakeChromeTabs struct {
	mu          sync.Mutex
	seq         int
	targets     []browser.Target
	openCalls   int
	closeCalls  []string
	failNavOnID string
}

func newFakeChromeTabs(port int, initial int) *fakeChromeTabs {
	fc := &fakeChromeTabs{}
	for i := range initial {
		fc.seq++
		id := fmt.Sprintf("t%d", fc.seq)
		_ = i
		fc.targets = append(fc.targets, browser.Target{
			ID:                   id,
			Type:                 "page",
			URL:                  MuseURL,
			WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", port, id),
		})
	}
	return fc
}

func (fc *fakeChromeTabs) pool() *tabPool {
	tp := newTabPool()
	tp.listTargets = func(_ context.Context, _ int, _ config.AccountConfig) ([]browser.Target, error) {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		return append([]browser.Target(nil), fc.targets...), nil
	}
	tp.openTab = func(_ context.Context, port int, _ config.AccountConfig) (browser.Target, error) {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		fc.seq++
		fc.openCalls++
		id := fmt.Sprintf("t%d", fc.seq)
		t := browser.Target{
			ID:                   id,
			Type:                 "page",
			URL:                  "about:blank",
			WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", port, id),
		}
		fc.targets = append(fc.targets, t)
		return t, nil
	}
	tp.navigate = func(_ context.Context, t browser.Target, _ int, site browser.Site) error {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		if fc.failNavOnID == t.ID {
			return errors.New("proxy connection failed")
		}
		for i := range fc.targets {
			if fc.targets[i].ID == t.ID {
				fc.targets[i].URL = site.Home
			}
		}
		return nil
	}
	tp.closeTab = func(_ context.Context, _ int, _ config.AccountConfig, id string) error {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		fc.closeCalls = append(fc.closeCalls, id)
		out := fc.targets[:0]
		for _, t := range fc.targets {
			if t.ID != id {
				out = append(out, t)
			}
		}
		fc.targets = out
		return nil
	}
	return tp
}

func (fc *fakeChromeTabs) countMuseTabs() int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	n := 0
	for _, t := range fc.targets {
		if u, err := url.Parse(t.URL); err == nil && museURL(u) {
			n++
		}
	}
	return n
}

func TestTabPoolLazyOpenReuseAndDynamicScale(t *testing.T) {
	const port = 60111
	fc := newFakeChromeTabs(port, 1) // Chrome launched with 1 initial muse.ai tab
	tp := fc.pool()
	policy := config.DefaultAccountConfig()
	ctx := context.Background()

	wantThreads := 2
	wantFn := func() int { return wantThreads }

	// Job 1 adopts the initial tab t1 without opening another tab.
	t1, err := tp.acquire(ctx, "accA", "j1", port, wantThreads, policy)
	if err != nil || t1 != "t1" {
		t.Fatalf("acquire j1 = (%q, %v), want t1", t1, err)
	}
	if fc.openCalls != 0 || fc.countMuseTabs() != 1 {
		t.Fatalf("openCalls=%d tabs=%d, want 0 and 1", fc.openCalls, fc.countMuseTabs())
	}

	// Job 2 shares the single tab t1 instead of opening a 2nd Chrome renderer tab.
	t2, err := tp.acquire(ctx, "accA", "j2", port, wantThreads, policy)
	if err != nil || t2 != "t1" || fc.openCalls != 0 || fc.countMuseTabs() != 1 {
		t.Fatalf("acquire j2 = (%q, %v) open=%d tabs=%d, want shared t1 with 1 tab", t2, err, fc.openCalls, fc.countMuseTabs())
	}
	if !tp.hasOtherBusy("accA", port, t1, "j1") || !tp.hasOtherBusy("accA", port, t1, "j2") {
		t.Fatal("expected hasOtherBusy to report sibling job sharing t1")
	}

	// A 3rd job above wantThreads=2 gets errTabCapReached.
	if extra, err := tp.acquire(ctx, "accA", "j_over", port, wantThreads, policy); !errors.Is(err, errTabCapReached) {
		t.Fatalf("expected errTabCapReached when at cap, got (%q, %v)", extra, err)
	}

	// Job 1 finishes -> t1 stays open cleanly with j2 still active.
	tp.release(ctx, "accA", port, t1, "j1", false, wantFn, policy)
	if len(fc.closeCalls) != 0 || fc.countMuseTabs() != 1 {
		t.Fatalf("release kept-warm violated: closed=%v tabs=%d", fc.closeCalls, fc.countMuseTabs())
	}
	if tp.hasOtherBusy("accA", port, t1, "j2") {
		t.Fatal("expected hasOtherBusy=false once only j2 remains")
	}

	// Job 3 reuses t1 up to wantThreads=4 concurrent slots, still keeping 1 shared tab.
	wantThreads = 4
	id3, err := tp.acquire(ctx, "accA", "j3", port, wantThreads, policy)
	if err != nil || id3 != "t1" {
		t.Fatalf("acquire j3 = (%q, %v), want t1", id3, err)
	}
	id4, err := tp.acquire(ctx, "accA", "j4", port, wantThreads, policy)
	if err != nil || id4 != "t1" {
		t.Fatalf("acquire j4 = (%q, %v), want t1", id4, err)
	}
	id5, err := tp.acquire(ctx, "accA", "j5", port, wantThreads, policy)
	if err != nil || id5 != "t1" || fc.openCalls != 0 || fc.countMuseTabs() != 1 {
		t.Fatalf("scale up 2->4 shared tab: id5=%q err=%v open=%d tabs=%d", id5, err, fc.openCalls, fc.countMuseTabs())
	}

	// Scale down from 4 -> 2 while 4 jobs share t1: new job j6 hits errTabCapReached.
	wantThreads = 2
	if extra, err := tp.acquire(ctx, "accA", "j6", port, wantThreads, policy); !errors.Is(err, errTabCapReached) {
		t.Fatalf("expected errTabCapReached when shared refs exceed reduced cap, got (%q, %v)", extra, err)
	}

	// Release jobs down below cap and verify acquire works cleanly again.
	tp.release(ctx, "accA", port, t1, "j2", false, wantFn, policy)
	tp.release(ctx, "accA", port, id3, "j3", false, wantFn, policy)
	tp.release(ctx, "accA", port, id4, "j4", false, wantFn, policy)
	id6, err := tp.acquire(ctx, "accA", "j6", port, wantThreads, policy)
	if err != nil || id6 != "t1" {
		t.Fatalf("expected j6 to reuse shared tab after releasing excess refs, got (%q, %v)", id6, err)
	}
}

func TestTabPoolNavigateFailureClosesNewTab(t *testing.T) {
	const port = 60112
	fc := newFakeChromeTabs(port, 0)
	fc.failNavOnID = "t1"
	tp := fc.pool()
	policy := config.DefaultAccountConfig()
	ctx := context.Background()

	if _, err := tp.acquire(ctx, "accA", "j1", port, 2, policy); err == nil {
		t.Fatal("expected navigate error on t1")
	}
	if len(fc.closeCalls) != 1 || fc.closeCalls[0] != "t1" || fc.countMuseTabs() != 0 {
		t.Fatalf("failed new tab should be closed cleanly: closed=%v tabs=%d", fc.closeCalls, fc.countMuseTabs())
	}
}

func TestConcurrentEnsureSignedInChecksOncePerChrome(t *testing.T) {
	f := newFakeDevTools(t, MuseURL)
	f.answers["login"] = func([]json.RawMessage) any {
		time.Sleep(35 * time.Millisecond)
		return LoginState{LoggedIn: true, Chat: true, HTTP: 200}
	}
	p := testProvider(t, &fakePage{loggedIn: true})
	p.mu.Lock()
	p.ctx = context.Background()
	p.mu.Unlock()
	acc := addSignedIn(t, p, "A")
	p.updateAccount(acc.ID, func(r *accounts.Record, _ *museData) { r.Proxy = proxy.None })
	jobs := submit(t, p, MuseJobVideo, "one", "two")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			page := newTestPage(f)
			defer page.Close()
			errs[idx] = p.ensureSignedIn(context.Background(), jobs[idx].ID, acc, page)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d err = %v", i, err)
		}
	}
	if got := f.ran("login"); got != 1 {
		t.Fatalf("login checks across concurrent threads = %d, want 1", got)
	}
	if !p.sessionVerified(acc.ID, f.port) {
		t.Fatal("expected verified port to be recorded")
	}
}

func TestBrowserClosedDropsVerifiedAndTabs(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	acc := addSignedIn(t, p, "A")
	p.markSessionVerified(acc.ID, 60259)
	p.tabs.mu.Lock()
	p.tabs.accounts[acc.ID] = &accountTabState{port: 60259, slots: []tabSlot{{targetID: "t1"}}}
	p.tabs.mu.Unlock()

	p.accountBrowserClosed(acc)
	if p.sessionVerified(acc.ID, 60259) {
		t.Fatal("expected accountBrowserClosed to clear verified session")
	}
	p.tabs.mu.Lock()
	_, kept := p.tabs.accounts[acc.ID]
	p.tabs.mu.Unlock()
	if kept {
		t.Fatal("expected accountBrowserClosed to drop tab pool state")
	}
}

func TestTabPoolDirtyReleaseAndPeriodicHeapReload(t *testing.T) {
	const port = 60113
	fc := newFakeChromeTabs(port, 2) // start with an extra pre-existing secondary tab
	tp := fc.pool()
	tp.reloadAfter = 3
	navMu := &sync.RWMutex{}
	tp.navLock = func(string, string) *sync.RWMutex { return navMu }
	navs := 0
	origNav := tp.navigate
	tp.navigate = func(ctx context.Context, target browser.Target, port int, site browser.Site) error {
		navs++
		return origNav(ctx, target, port, site)
	}
	policy := config.DefaultAccountConfig()
	ctx := context.Background()
	wantFn := func() int { return 3 }

	// Acquiring j1 picks t1 and prunes excess idle t2 down to 1 shared tab.
	t1, err := tp.acquire(ctx, "accA", "j1", port, 3, policy)
	if err != nil || t1 != "t1" || fc.countMuseTabs() != 1 || len(fc.closeCalls) != 1 || fc.closeCalls[0] != "t2" {
		t.Fatalf("acquire j1 should prune excess t2: t=%q err=%v closed=%v tabs=%d", t1, err, fc.closeCalls, fc.countMuseTabs())
	}
	// Dirty release on sole tab never closes Chrome's last tab; marks needsReload.
	tp.release(ctx, "accA", port, t1, "j1", true, wantFn, policy)
	if len(fc.closeCalls) != 1 || fc.countMuseTabs() != 1 {
		t.Fatalf("must never close sole Chrome tab: closed=%v tabs=%d", fc.closeCalls, fc.countMuseTabs())
	}
	if _, err := tp.acquire(ctx, "accA", "j2", port, 3, policy); err != nil {
		t.Fatal(err)
	}
	if navs != 1 {
		t.Fatalf("expected dirty tab reload on next acquire, navs=%d", navs)
	}
	// Complete 3 uses after dirty reset to hit reloadAfter=3 while a sibling is running.
	tp.release(ctx, "accA", port, t1, "j2", false, wantFn, policy) // uses=1
	_, _ = tp.acquire(ctx, "accA", "j3", port, 3, policy)
	tp.release(ctx, "accA", port, t1, "j3", false, wantFn, policy) // uses=2
	_, _ = tp.acquire(ctx, "accA", "j4", port, 3, policy)
	tp.release(ctx, "accA", port, t1, "j4", false, wantFn, policy) // uses=3 -> needsReload=true

	// Hold nav.RLock() on the shared tab to simulate an active Download/Send on a sibling thread.
	// acquire(j5) must wait for nav.RUnlock() before reloading t1 to Home.
	navMu.RLock()
	type acqRes struct {
		id  string
		err error
	}
	acquired := make(chan acqRes, 1)
	go func() {
		id, err := tp.acquire(ctx, "accA", "j5", port, 3, policy)
		acquired <- acqRes{id, err}
	}()
	time.Sleep(30 * time.Millisecond)
	if navs != 1 {
		navMu.RUnlock()
		t.Fatalf("periodic reload started while nav.RLock was held! navs=%d", navs)
	}
	navMu.RUnlock()
	select {
	case res := <-acquired:
		if res.err != nil || res.id != t1 || navs != 2 {
			t.Fatalf("res=%+v navs=%d, want t1 and navs=2", res, navs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for j4 reload after nav.RUnlock")
	}
}

type fakeBrowserChecker map[string]bool

func (f fakeBrowserChecker) HasRunningBrowser(id string) bool { return f[id] }

func TestColdChromeStaggerPlanner(t *testing.T) {
	planner := newChromeStaggerPlanner(fakeBrowserChecker{"warm": true})
	// Warm account + sibling thread launch quickly without full cold stagger.
	if d := planner("warm"); d < warmThreadStaggerMin || d > warmThreadStaggerMin+time.Duration(warmThreadStaggerSpan)*time.Millisecond {
		t.Fatalf("warm stagger = %v", d)
	}
	// Cold accounts spread beyond the old 5s cap across 20 accounts, capped at 20s.
	var prev time.Duration
	for i := range 20 {
		id := fmt.Sprintf("cold-%d", i)
		d := planner(id)
		if i == 0 && d != 0 {
			t.Fatalf("first cold account stagger = %v, want 0", d)
		}
		if i > 0 && d < prev {
			t.Fatalf("cold stagger regressed at %d: %v < %v", i, d, prev)
		}
		if d > maxColdChromeStagger {
			t.Fatalf("cold stagger exceeded cap: %v > %v", d, maxColdChromeStagger)
		}
		sib := planner(id)
		if sib <= d {
			t.Fatalf("sibling thread stagger (%v) must follow account cold delay (%v)", sib, d)
		}
		prev = d
	}
	if prev <= 5*time.Second {
		t.Fatalf("expected 20 cold accounts to spread past 5s, got max=%v", prev)
	}
}
