package muse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/proxy"
)

func TestSessionRenewDeletesExpiredCookiesAndPreservesSameSite(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:   "dead_cookie",
			Value:  "",
			Domain: ".muse.ai",
			Path:   "/",
			MaxAge: -1,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "fresh_cookie",
			Value:    "v2",
			Domain:   ".muse.ai",
			Path:     "/",
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"active"}`))
	}))
	defer srv.Close()

	cfg, _ := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	site := NewSite(cfg)
	site.now = func() time.Time { return now }
	site.sessionURL = srv.URL + "/api/session"

	rec := accounts.Record{
		Cookies: []browser.Cookie{
			{Name: "local_trigger", Value: "1", Domain: "127.0.0.1", Path: "/", Expires: float64(now.Add(24 * time.Hour).Unix())},
			{Name: "dead_cookie", Value: "old", Domain: ".muse.ai", Path: "/", Expires: float64(now.Add(24 * time.Hour).Unix())},
			{Name: "fresh_cookie", Value: "v1", Domain: ".muse.ai", Path: "/", Expires: float64(now.Add(24 * time.Hour).Unix())},
		},
	}
	res, err := site.RefreshSession(context.Background(), srv.Client(), rec)
	if err != nil || res.SignedOut {
		t.Fatalf("RefreshSession = (%+v, %v)", res, err)
	}
	var sawFresh bool
	for _, c := range res.Cookies {
		if c.Name == "dead_cookie" {
			t.Fatalf("explicitly deleted cookie (Max-Age=-1) was resurrected: %+v", c)
		}
		if c.Name == "fresh_cookie" {
			sawFresh = true
			if c.Value != "v2" || c.SameSite != "None" {
				t.Fatalf("fresh_cookie = %+v, want Value=v2 SameSite=None", c)
			}
		}
	}
	if !sawFresh {
		t.Fatalf("fresh_cookie missing from merged cookies: %+v", res.Cookies)
	}
}

func TestRemoveJobFromSlotAndDeferredSharedTabReload(t *testing.T) {
	// Missing jobID must leave unrelated active jobs intact.
	kept := removeJobFromSlot([]string{"j1", "j2"}, "missing")
	if len(kept) != 2 || kept[0] != "j1" || kept[1] != "j2" {
		t.Fatalf("removeJobFromSlot evicted unrelated job: got %v, want [j1 j2]", kept)
	}

	const port = 60222
	fc := newFakeChromeTabs(port, 1)
	tp := fc.pool()
	navs := 0
	origNav := tp.navigate
	tp.navigate = func(ctx context.Context, target browser.Target, port int, site browser.Site, policy config.AccountConfig) error {
		navs++
		return origNav(ctx, target, port, site, policy)
	}
	policy := config.DefaultAccountConfig()
	ctx := context.Background()
	wantFn := func() int { return 3 }

	// j1 and j2 share t1; j2 finishes dirty -> needsReload=true.
	if _, err := tp.acquire(ctx, "accA", "j1", port, 3, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := tp.acquire(ctx, "accA", "j2", port, 3, policy); err != nil {
		t.Fatal(err)
	}
	tp.release(ctx, "accA", port, "t1", "j2", true, wantFn, policy)

	// j3 acquires t1 while j1 is still active: must NOT reload under j1.
	if _, err := tp.acquire(ctx, "accA", "j3", port, 3, policy); err != nil {
		t.Fatal(err)
	}
	if navs != 0 {
		t.Fatalf("shared tab reloaded while sibling j1 was active! navs=%d", navs)
	}

	// Once both j1 and j3 release, j4 acquires as sole holder and reloads cleanly.
	tp.release(ctx, "accA", port, "t1", "j1", false, wantFn, policy)
	tp.release(ctx, "accA", port, "t1", "j3", false, wantFn, policy)
	if _, err := tp.acquire(ctx, "accA", "j4", port, 3, policy); err != nil {
		t.Fatal(err)
	}
	if navs != 1 {
		t.Fatalf("expected deferred reload once sole holder acquired tab, navs=%d", navs)
	}
}

func TestStateRefreshConcurrencyIsBounded(t *testing.T) {
	var active, peak atomic.Int32
	base := &fakePage{loggedIn: false}
	p := testProvider(t, base)
	p.mu.Lock()
	p.ctx = context.Background()
	p.mu.Unlock()
	p.pageFor = func(context.Context, accounts.Record) (Page, error) {
		cur := active.Add(1)
		for {
			prev := peak.Load()
			if cur <= prev || peak.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		active.Add(-1)
		return base, nil
	}

	for i := range 6 {
		rec := accounts.Record{Account: accounts.Account{
			ID:            "ref-" + string(rune('a'+i)),
			Provider:      accounts.ProviderMuse,
			Name:          "Acc",
			Source:        accounts.SourceBrowser,
			Status:        accounts.StatusSignedOut,
			Enabled:       true,
			Proxy:         proxy.None,
			LastCheckedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		}}
		if err := p.accounts.Seed(rec); err != nil {
			t.Fatal(err)
		}
		p.scheduleStateAccountRefresh(rec, "queue_needs_account")
	}
	p.wg.Wait()

	if got := peak.Load(); got > maxConcurrentStateRefreshes {
		t.Fatalf("peak concurrent state refreshes = %d, want <= %d", got, maxConcurrentStateRefreshes)
	}
}

func TestRestrictionsAvoidFalsePositivesOnLLMProseAwayTextAndTransientProbes(t *testing.T) {
	// Free-form LLM content policy refusal mentioning "video" + "not allowed" must NOT ban video.
	kind, _ := classifyRestrictionForKind(
		MuseJobVideo,
		"",
		nil,
		"I'm sorry, generating a video of this copyrighted character is not allowed under our safety policy.",
		nil,
	)
	if kind != restrictNone {
		t.Fatalf("LLM policy refusal misclassified as %v, want restrictNone", kind)
	}

	// Away URL with "403" inside ID and benign "cookies are disabled" text must NOT kill account.
	awayKind, _ := classifyAwayTab(awayTabInfo{
		URL:   "https://accounts.google.com/o/oauth2/v2/auth?state=140399",
		Title: "Sign in",
		Text:  "Cookies are disabled in your browser. Please enable cookies to sign in.",
	})
	if awayKind != restrictNone {
		t.Fatalf("benign OAuth away tab misclassified as %v, want restrictNone", awayKind)
	}

	// CheckVideo on sessionInterrupted with no restriction text must return transient error, NOT disable video.
	pageInterrupted := &fakePage{
		loggedIn: true,
		sendRes:  SendResult{OK: true, SessionID: "chk-int"},
		statuses: []string{sessionInterrupted},
		events:   nil,
		quota:    &MuseQuota{PercentUsed: 10},
	}
	sInt, _ := testSite(t, pageInterrupted)
	inspInt, errInt := sInt.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
	if errInt == nil || inspInt.VideoCheckDisabled || inspInt.DisableAccount {
		t.Fatalf("sessionInterrupted should return transient error without disabling video: insp=%+v err=%v", inspInt, errInt)
	}

	// CheckVideo on bare send_failed without restriction diagnostics must return transient error, NOT disable video.
	pageBareFail := &fakePage{
		loggedIn: true,
		sendRes:  SendResult{OK: false, Reason: "send_failed"},
		quota:    &MuseQuota{PercentUsed: 10},
	}
	sBare, _ := testSite(t, pageBareFail)
	inspBare, errBare := sBare.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
	if errBare == nil || inspBare.VideoCheckDisabled || inspBare.DisableAccount {
		t.Fatalf("bare send_failed should return transient error without disabling video: insp=%+v err=%v", inspBare, errBare)
	}
}

func TestReleaseAfterFailsPartialAndEmptyStoryboards(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})

	partialBoard := MuseJob{ID: "b-part", Kind: MuseJobImage, Status: MuseJobSucceeded, Files: []string{"/flow-media/f1.png"}, BoardFrames: 3}
	waitPartial := MuseJob{ID: "v-part", Kind: MuseJobVideo, Status: MuseJobQueued, After: "b-part", BoardFrames: 3}
	emptyBoard := MuseJob{ID: "b-empty", Kind: MuseJobImage, Status: MuseJobSucceeded, Files: nil, BoardFrames: 2}
	waitEmpty := MuseJob{ID: "v-empty", Kind: MuseJobVideo, Status: MuseJobQueued, After: "b-empty", BoardFrames: 2}
	if _, err := p.jobs.add(partialBoard, waitPartial, emptyBoard, waitEmpty); err != nil {
		t.Fatal(err)
	}

	p.releaseAfter(partialBoard)
	p.releaseAfter(emptyBoard)

	gotPart := mustJob(t, p, "v-part")
	if gotPart.Status != MuseJobFailed || !strings.Contains(gotPart.Error, "1/3") {
		t.Fatalf("partial storyboard releaseAfter = %+v, want Failed with 1/3", gotPart)
	}
	gotEmpty := mustJob(t, p, "v-empty")
	if gotEmpty.Status != MuseJobFailed || !strings.Contains(gotEmpty.Error, "không có ảnh") {
		t.Fatalf("empty storyboard releaseAfter = %+v, want Failed", gotEmpty)
	}
}

func TestReconcileInterruptedSendSerializesAndIgnoresOwnedSessions(t *testing.T) {
	base := &fakePage{loggedIn: true}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{
			{ID: "s0", Status: "completed"},
			{ID: "s-owned-by-sibling", Status: "running"},
		},
		userMsgs: map[string][]UserEvent{
			"s-owned-by-sibling": {{Text: "same prompt text for both jobs", HasText: true}},
		},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	jobs := submit(t, p, MuseJobVideo, "same prompt text for both jobs", "same prompt text for both jobs")
	// Sibling job already owns s-owned-by-sibling.
	_, _ = p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.SessionID = MuseJobProcessing, acc.ID, "s-owned-by-sibling"
	})
	_, _ = p.jobs.update(jobs[1].ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.Sending = MuseJobProcessing, acc.ID, true
		j.PreSendTracked, j.PreSendSessions = true, []string{"s0"}
	})

	// Hold sendSlot briefly to prove reconcileInterruptedSend waits for it.
	release, err := p.sendSlot(context.Background(), acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reconciled atomic.Bool
	var recErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		job1 := mustJob(t, p, jobs[1].ID)
		_, _, recErr = p.reconcileInterruptedSend(context.Background(), job1, acc, nil, page)
		reconciled.Store(true)
	}()
	time.Sleep(30 * time.Millisecond)
	if reconciled.Load() {
		release()
		t.Fatal("reconcileInterruptedSend ran while sendSlot was held")
	}
	release()
	wg.Wait()

	// Because s-owned-by-sibling belongs to jobs[0], jobs[1] must NOT steal it and must resolve as notSent.
	var notSent *notSentError
	if !errors.As(recErr, &notSent) {
		t.Fatalf("expected reconcileInterruptedSend to ignore sibling-owned session and return notSentError, got %v", recErr)
	}
	if got := mustJob(t, p, jobs[1].ID); got.SessionID != "" {
		t.Fatalf("jobs[1] stole sibling SessionID: %+v", got)
	}
}

func TestPageScriptP1HistoryLimitRafCleanupAndEvictionInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
let rafTimersActive = 0;
const realSetTimeout = setTimeout;
const realClearTimeout = clearTimeout;
const tracked = new Set();
global.setTimeout = (fn, ms) => {
  const id = realSetTimeout(() => { tracked.delete(id); fn(); }, ms);
  if (ms === 32) tracked.add(id);
  return id;
};
global.clearTimeout = (id) => { tracked.delete(id); return realClearTimeout(id); };
global.performance = { now: () => 1, getEntriesByType: () => [] };
const historyLimits = [];
let openSeq = 0;
global.window = {
  requestAnimationFrame: (cb) => { queueMicrotask(() => cb(16)); return 1; },
  cancelAnimationFrame: () => {},
  __hatchEarlyGatewayRuntimeState: {
    activeRuntime: {
      getSnapshot: () => ({
        isConnected: true,
        async sendRequest(method, params) {
          if (method === 'chat.history') {
            historyLimits.push(params.limit);
            return { chat_events: [{ role: 'user', text: 'hi' }, { role: 'assistant', display_text: 'ok' }] };
          }
          if (method === 'fs.raw') {
            openSeq++;
            return { data: new Blob([new Uint8Array(2048).fill(openSeq)]) };
          }
          return {};
        }
      })
    }
  }
};
global.document = { body: {} };
global.Document = function() {};
// Run visibleShim twice (simulating addScriptToEvaluateOnNewDocument + Runtime.evaluate) then pageMain.
` + visibleShim + `
` + visibleShim + `
const page = (` + pageSource + `)(1024);
(async () => {
  // 1. rAF fires before 32ms timer -> timer must be cleared immediately.
  await new Promise((resolve) => window.requestAnimationFrame(() => resolve()));
  assert.equal(tracked.size, 0, 'rAF fallback timer was not cleared when rAF ran first');

  // 2. chat.history uses limit >= 500 for both userMessages and result.
  await page.userMessages('s1');
  await page.result('s1');
  assert.deepEqual(historyLimits, [500, 500]);

  // 3. Open >16 files without closing -> oldest handle evicted cleanly.
  const first = await page.open('/f0.mp4', 10000);
  for (let i = 1; i <= 16; i++) {
    await page.open('/f' + i + '.mp4', 10000);
  }
  await assert.rejects(() => page.slice(first.id, 0), /expired|closed/i);
})().catch(e => { console.error(e); process.exitCode = 1; });`

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page.js P1 stability test failed: %v\n%s", err, out)
	}
}
