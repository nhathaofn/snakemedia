package muse

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
)

func TestP0_M1_CancelWhileSendingPreservesReconcileAndCrashSettlesUnknown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	page := &fakePage{
		loggedIn: true,
		sendRes:  SendResult{OK: true, SessionID: "chat-late"},
		onSend: func() {
			close(started)
			<-release
		},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobImage, "sunset beach")[0]

	p.mu.Lock()
	p.running[job.ID] = acc.ID
	p.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			p.mu.Lock()
			delete(p.running, job.ID)
			p.mu.Unlock()
		}()
		p.runOne(context.Background(), job.ID, acc.ID)
	}()

	<-started
	res := p.CancelJob(job.ID)
	if !res.Success {
		t.Fatalf("CancelJob failed: %+v", res)
	}
	if !res.Data.Sending || res.Data.Status != MuseJobCancelled {
		t.Fatalf("CancelJob cleared Sending mid-flight: %+v", res.Data)
	}
	close(release)
	<-done

	// Simulate app crash / restart while job was cancelled mid-send with Sending still true.
	p2 := testProvider(t, page)
	p2.jobs = newJobStore(p.jobs.path, func() int { return 500 })
	if err := p2.jobs.load(); err != nil {
		t.Fatal(err)
	}
	_, _ = p2.jobs.update(job.ID, func(j *MuseJob) {
		j.Status = MuseJobCancelled
		j.Sending = true
		j.SessionID = ""
		j.SendUnknown = false
	})
	p2.Start(context.Background(), func(events.Event, any) {})
	defer p2.Close()

	got := mustJob(t, p2, job.ID)
	if !got.SendUnknown || got.Sending {
		t.Fatalf("expected interrupted cancelled-while-sending job to settle as SendUnknown=true, Sending=false: %+v", got)
	}
	if !got.mayBePaid() {
		t.Fatalf("mayBePaid() = false after SendUnknown settled: %+v", got)
	}
}

func TestP0_M2_RemoveRefusesQueuedJobAlreadyInRunningStagger(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	job := submit(t, p, MuseJobImage, "staggered job")[0]

	p.mu.Lock()
	p.running[job.ID] = "acc-1"
	p.mu.Unlock()

	if res := p.RemoveJob(job.ID); res.Success {
		t.Fatalf("RemoveJob removed job already in p.running: %+v", res)
	}
	if res := p.RemoveJobs([]string{job.ID}); !res.Success || res.Data != 0 {
		t.Fatalf("RemoveJobs should skip running staggered job (want removed=0): %+v", res)
	}
	if _, ok := p.jobs.get(job.ID); !ok {
		t.Fatal("queued running job disappeared from journal")
	}
}

func TestP0_M3_RefreshRecycleFailureNeverMarksAccountSignedOut(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	acc := addSignedIn(t, p, "HealthyAccount")

	lease := &accountLease{
		accounts: p.accounts,
		id:       acc.ID,
		release:  func() {},
		recycle:  func() error { return browser.ErrProfileBusy },
	}
	var curPage Page = &chromePage{
		port:   1, // non-zero port triggers chrome recycle path; with reason ensure_signed_in avoids network wait
		policy: config.DefaultAccountConfig(),
	}

	_, ok, err := p.refreshAccountForRun(
		context.Background(),
		acc.ID,
		MuseJobImage,
		&curPage,
		lease,
		"ensure_signed_in",
	)
	if ok || err == nil || errors.Is(err, errNotSignedIn) {
		t.Fatalf("expected non-signed-out transient error on RecycleHeadless failure, got ok=%v err=%v", ok, err)
	}
	var ns *notSentError
	if !errors.As(err, &ns) {
		t.Fatalf("expected notSentError on RecycleHeadless failure, got %T: %v", err, err)
	}
	fresh, recErr := p.accounts.Record(acc.ID)
	if recErr != nil {
		t.Fatal(recErr)
	}
	if fresh.Status != accounts.StatusSignedIn {
		t.Fatalf("account falsely marked %s after failed recycle: %+v", fresh.Status, fresh.Account)
	}
}

func TestP0_M4_AttachmentsNilCapabilitiesNeverPanics(t *testing.T) {
	img := Attachment{
		Category:   "image",
		Filename:   "big.png",
		MimeType:   "image/png",
		DataBase64: strings.Repeat("A", museInlineRequestBudget+1024),
	}
	if needsOutgoingJPEGNorm([]byte("small"), ".png", 1024, nil) {
		t.Fatal("needsOutgoingJPEGNorm(small, nil caps) should return false without panicking")
	}
	if !needsOutgoingJPEGNorm(make([]byte, 2000), ".png", 1024, nil) {
		t.Fatal("needsOutgoingJPEGNorm(oversized, nil caps) should return true without panicking")
	}
	if _, err := prepareAttachments(MuseJob{Kind: MuseJobImage, Prompt: "test"}, []Attachment{img}, nil); err == nil {
		t.Fatal("expected error for oversized invalid-image payload when caps is nil, not panic or success")
	}
	if _, err := reduceAttachment(img, museInlineRequestBudget, nil); !errors.Is(err, errLimitsUnknown) {
		t.Fatalf("reduceAttachment(nil caps) = %v, want errLimitsUnknown", err)
	}
}

func TestP0_M14_RestrictionsIgnoreTurnstileChromeErrorAndAmbiguousReplyPhrases(t *testing.T) {
	for _, rawURL := range []string{
		"https://challenges.cloudflare.com/turnstile/v0/api",
		"https://auth.muse.ai/cdn-cgi/challenge-platform/h/b",
		"https://example.com/login?redirect=turnstile",
		"chrome-error://chromewebdata/",
	} {
		kind, detail := classifyAwayTab(awayTabInfo{URL: rawURL, Title: "Just a moment..."})
		if kind != restrictNone {
			t.Fatalf("classifyAwayTab(%q) = (%v, %q), want restrictNone", rawURL, kind, detail)
		}
	}

	// Unknown external domain without explicit account ban marker should not permanently disable account.
	if kind, _ := classifyAwayTab(awayTabInfo{URL: "https://external-status.example.org/", Title: "Service maintenance"}); kind != restrictNone {
		t.Fatalf("generic external URL falsely returned %v", kind)
	}

	// Ambiguous bare phrases inside normal assistant reply text must not lock or disable healthy accounts.
	for _, reply := range []string{
		"I cannot fulfill a request about a fictional daily limit or wait 24 hours scenario.",
		"This prompt mentions joining a waitlist and verify your identity in a story.",
	} {
		kind, _ := classifyText("", "", reply, nil)
		if kind != restrictNone {
			t.Fatalf("ambiguous assistant reply %q falsely classified as %v", reply, kind)
		}
	}

	// Same phrase in trusted system / quota metadata still classifies accurately.
	if kind, _ := classifyText("", "daily limit reached — try again in 24 hours", "", nil); kind != restrictDailyVideo24h {
		t.Fatalf("trusted quota metadata classified as %v, want restrictDailyVideo24h", kind)
	}
	if kind, _ := classifyText("account waitlist", "", "", nil); kind != restrictDeadAccount {
		t.Fatalf("trusted sysMsg waitlist classified as %v, want restrictDeadAccount", kind)
	}
}

func TestP0_M15_CheckVideoTransientPollErrorsNeverDisableAccount(t *testing.T) {
	var mu sync.Mutex
	statusCalls := 0
	page := &transientVideoPollPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "probe-chat"},
		},
		onStatus: func() (string, error) {
			mu.Lock()
			defer mu.Unlock()
			statusCalls++
			return "", errors.New("temporary cdp timeout")
		},
	}
	site, _ := testSite(t, page)
	site.now = func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) }

	// Bounded context stops waitVideoCheck once transient errors exceed short timeout without disabling account.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	insp, err := site.CheckVideo(ctx, &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
	if err == nil {
		t.Fatal("expected error when poll repeatedly fails")
	}
	if insp.DisableAccount || insp.VideoCheckDisabled {
		t.Fatalf("transient poll errors must NOT disable account or video check: %+v", insp)
	}
	if statusCalls == 0 {
		t.Fatal("expected at least one Status poll call")
	}

	// Send returns OK:true without SessionID on timeout must also return transient error, never DisableAccount.
	pageNoSID := &fakePage{
		loggedIn: true,
		sendRes:  SendResult{OK: true, SessionID: ""},
	}
	s2, _ := testSite(t, pageNoSID)
	insp2, err2 := s2.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
	if err2 == nil || insp2.DisableAccount || insp2.VideoCheckDisabled {
		t.Fatalf("missing SessionID after OK:true must not disable account: insp=%+v err=%v", insp2, err2)
	}
}

type transientVideoPollPage struct {
	*fakePage
	onStatus func() (string, error)
}

func (p *transientVideoPollPage) Status(ctx context.Context, sid string) (string, error) {
	if p.onStatus != nil {
		return p.onStatus()
	}
	return p.fakePage.Status(ctx, sid)
}
