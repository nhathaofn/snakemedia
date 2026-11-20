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

	// Job 2 needs thread 2 -> opens t2, leaving total tabs = 2.
	t2, err := tp.acquire(ctx, "accA", "j2", port, wantThreads, policy)
	if err != nil || t2 != "t2" {
		t.Fatalf("acquire j2 = (%q, %v), want t2", t2, err)
	}
	if fc.openCalls != 1 || fc.countMuseTabs() != 2 {
		t.Fatalf("openCalls=%d tabs=%d, want 1 and 2", fc.openCalls, fc.countMuseTabs())
	}

	// Job 1 finishes -> t1 stays open for reuse.
	tp.release(ctx, "accA", port, t1, false, wantFn, policy)
	if len(fc.closeCalls) != 0 || fc.countMuseTabs() != 2 {
		t.Fatalf("release kept-warm violated: closed=%v tabs=%d", fc.closeCalls, fc.countMuseTabs())
	}

	// Job 3 reuses idle t1 instead of opening a 3rd tab.
	t3, err := tp.acquire(ctx, "accA", "j3", port, wantThreads, policy)
	if err != nil || t3 != "t1" || fc.openCalls != 1 || fc.countMuseTabs() != 2 {
		t.Fatalf("acquire j3 = (%q, %v) open=%d tabs=%d", t3, err, fc.openCalls, fc.countMuseTabs())
	}

	// Scale up from 2 -> 4 threads: new jobs open t3 and t4 on the same port.
	wantThreads = 4
	id3, err := tp.acquire(ctx, "accA", "j4", port, wantThreads, policy)
	if err != nil {
		t.Fatalf("acquire j4: %v", err)
	}
	id4, err := tp.acquire(ctx, "accA", "j5", port, wantThreads, policy)
	if err != nil {
		t.Fatalf("acquire j5: %v", err)
	}
	if fc.countMuseTabs() != 4 || id3 == id4 {
		t.Fatalf("scale up 2->4: tabs=%d id3=%q id4=%q", fc.countMuseTabs(), id3, id4)
	}

	// Scale down from 4 -> 2 while 1 tab is idle and 3 are busy:
	// first release one tab so we have an idle tab to test reconcileIdle too.
	tp.release(ctx, "accA", port, id4, false, func() int { return 4 }, policy)
	wantThreads = 2
	tp.reconcileIdle(ctx, wantThreads, policy)
	if fc.countMuseTabs() != 3 {
		t.Fatalf("expected reconcileIdle to close the 1 idle extra tab down to 3, got %d", fc.countMuseTabs())
	}

	// When a still-busy extra tab finishes, release immediately shrinks from 3 -> 2.
	tp.release(ctx, "accA", port, id3, false, wantFn, policy)
	if fc.countMuseTabs() != 2 {
		t.Fatalf("expected release on scale down to close excess tab down to 2, got %d", fc.countMuseTabs())
	}

	// A pending acquire hitting reduced cap gets errTabCapReached rather than opening a 3rd tab.
	if extra, err := tp.acquire(ctx, "accA", "j6", port, wantThreads, policy); !errors.Is(err, errTabCapReached) {
		t.Fatalf("expected errTabCapReached when full, got (%q, %v)", extra, err)
	}
}

func TestTabPoolNavigateFailureClosesNewTab(t *testing.T) {
	const port = 60112
	fc := newFakeChromeTabs(port, 1)
	fc.failNavOnID = "t2"
	tp := fc.pool()
	policy := config.DefaultAccountConfig()
	ctx := context.Background()

	if _, err := tp.acquire(ctx, "accA", "j1", port, 2, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := tp.acquire(ctx, "accA", "j2", port, 2, policy); err == nil {
		t.Fatal("expected navigate error on t2")
	}
	if len(fc.closeCalls) != 1 || fc.closeCalls[0] != "t2" || fc.countMuseTabs() != 1 {
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

func TestTabPoolDirtyReleaseClosesBrokenExtraTabButKeepsLastTab(t *testing.T) {
	const port = 60113
	fc := newFakeChromeTabs(port, 1)
	tp := fc.pool()
	policy := config.DefaultAccountConfig()
	ctx := context.Background()
	wantFn := func() int { return 2 }

	t1, err := tp.acquire(ctx, "accA", "j1", port, 2, policy)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := tp.acquire(ctx, "accA", "j2", port, 2, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Dirty release on extra tab t2 closes t2 even when wantThreads is still 2.
	tp.release(ctx, "accA", port, t2, true, wantFn, policy)
	if len(fc.closeCalls) != 1 || fc.closeCalls[0] != t2 || fc.countMuseTabs() != 1 {
		t.Fatalf("dirty extra tab should close cleanly: closed=%v tabs=%d", fc.closeCalls, fc.countMuseTabs())
	}
	// Dirty release on the sole remaining tab never closes the last tab in Chrome.
	tp.release(ctx, "accA", port, t1, true, wantFn, policy)
	if len(fc.closeCalls) != 1 || fc.countMuseTabs() != 1 {
		t.Fatalf("must never close sole Chrome tab: closed=%v tabs=%d", fc.closeCalls, fc.countMuseTabs())
	}
}
