package muse

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/proxy"
)

type homeRecoverLoginPage struct {
	*fakePage
	mu         sync.Mutex
	loginCalls int
	homeCalls  int
	recoverOn  int
}

func (p *homeRecoverLoginPage) Login(context.Context) (LoginState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loginCalls++
	if p.homeCalls >= p.recoverOn {
		return LoginState{LoggedIn: true, Chat: true, HTTP: 200}, nil
	}
	return LoginState{LoggedIn: false, Chat: false, HTTP: 401}, nil
}

func (p *homeRecoverLoginPage) Home(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.homeCalls++
	return nil
}

func TestWaitSignedInAndSiteInspectRecover401ViaHomeReload(t *testing.T) {
	t.Run("waitSignedIn navigates Home on 401 and recovers on next poll", func(t *testing.T) {
		page := &homeRecoverLoginPage{fakePage: &fakePage{}, recoverOn: 1}
		p := testProvider(t, page)

		if err := p.waitSignedIn(context.Background(), page, 10*time.Second); err != nil {
			t.Fatalf("expected waitSignedIn to recover after Home reload, got err: %v", err)
		}
		if page.homeCalls != 1 {
			t.Fatalf("homeCalls = %d, want 1", page.homeCalls)
		}
	})

	t.Run("Site.Inspect navigates Home on 401 and keeps account SignedIn", func(t *testing.T) {
		page := &homeRecoverLoginPage{fakePage: &fakePage{quota: &MuseQuota{PercentUsed: 12}}, recoverOn: 1}
		s, _ := testSite(t, page)

		insp, err := s.Inspect(context.Background(), nil, accounts.Record{})
		if err != nil || !insp.SignedIn || insp.Fatal != "" {
			t.Fatalf("expected Site.Inspect to recover SignedIn after Home, got insp=%+v err=%v", insp, err)
		}
		if page.homeCalls != 1 {
			t.Fatalf("homeCalls = %d, want 1", page.homeCalls)
		}
	})
}

type scriptRecoveryPage struct {
	*fakePage
	mu           sync.Mutex
	failSendOnce bool
	onHome       func()
}

func (s *scriptRecoveryPage) Home(ctx context.Context) error {
	s.mu.Lock()
	hook := s.onHome
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return s.fakePage.Home(ctx)
}

func (s *scriptRecoveryPage) Send(ctx context.Context, text string, atts []Attachment) (SendResult, error) {
	s.mu.Lock()
	failOnce := s.failSendOnce
	if failOnce {
		s.failSendOnce = false
	}
	s.mu.Unlock()
	if failOnce {
		return SendResult{OK: false, Reason: "send_failed", Diagnostics: &SendDiagnostics{Message: "unauthorized gateway"}}, nil
	}
	return s.fakePage.Send(ctx, text, atts)
}

func TestPreSendRefreshRecoversLiveAccountAndRetriesSendOnce(t *testing.T) {
	events, files := videoResult("/out/healed.mp4", []byte("vid"))
	page := &scriptRecoveryPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-ok"},
			statuses: []string{"completed"},
			events:   events,
			files:    files,
			quota:    &MuseQuota{PercentUsed: 15},
		},
		failSendOnce: true,
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một chú mèo")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s-ok" || len(page.sends) != 1 || page.homes < 1 {
		t.Fatalf("expected refresh+retry to succeed cleanly: job=%+v sends=%d homes=%d", got, len(page.sends), page.homes)
	}
	freshAcc, _ := p.accounts.Record(acc.ID)
	if freshAcc.Status != accounts.StatusSignedIn || !freshAcc.Enabled {
		t.Fatalf("account should remain SignedIn and Enabled: %+v", freshAcc.Account)
	}
}

func TestThreeMainRestrictionClassesAfterRefresh(t *testing.T) {
	t.Run("verification or waitlist marks account dead/disabled and requeues unsent media job", func(t *testing.T) {
		for _, reply := range []string{
			"You're currently on the waitlist. Early access will open soon.",
			"Please complete verification required for your account before generating.",
			"Tài khoản cần xác minh tài khoản để tiếp tục.",
		} {
			page := &fakePage{
				loggedIn: true,
				sendRes:  SendResult{OK: true, SessionID: "s1"},
				statuses: []string{"completed"},
				events:   []AssistantEvent{{Text: reply}},
				quota:    &MuseQuota{PercentUsed: 5},
			}
			p := testProvider(t, page)
			acc := addSignedIn(t, p, "A")
			job := submit(t, p, MuseJobVideo, "cat")[0]

			p.runOne(context.Background(), job.ID, acc.ID)

			gotJob := mustJob(t, p, job.ID)
			if gotJob.Status != MuseJobQueued || gotJob.Attempts != 0 {
				t.Fatalf("reply %q: job = %+v, want Queued with 0 attempts", reply, gotJob)
			}
			gotAcc, _ := p.accounts.Record(acc.ID)
			if gotAcc.Enabled || gotAcc.Status != accounts.StatusError {
				t.Fatalf("reply %q: dead/waitlist account should be disabled & StatusError, got %+v", reply, gotAcc.Account)
			}
		}
	})

	t.Run("region video restriction sets image-only mode (SkipVideoCheck) while keeping account active", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "This feature is not available in your region right now."}},
			quota:    &MuseQuota{PercentUsed: 5},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "cat")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		gotJob := mustJob(t, p, job.ID)
		if gotJob.Status != MuseJobQueued || gotJob.Attempts != 0 {
			t.Fatalf("video job should requeue to another account, got %+v", gotJob)
		}
		gotAcc, _ := p.accounts.Record(acc.ID)
		if !gotAcc.Enabled || gotAcc.Status != accounts.StatusSignedIn || !gotAcc.SkipVideoCheck {
			t.Fatalf("region-restricted video account should stay SignedIn & Enabled with SkipVideoCheck=true: %+v", gotAcc.Account)
		}
	})

	t.Run("24h video limit pauses account with AutoResume and notifies Muse on expiry", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "Daily video cap reached. Try again in 24 hours."}},
			quota:    &MuseQuota{PercentUsed: 10},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "cat")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		gotAcc, _ := p.accounts.Record(acc.ID)
		if gotAcc.Enabled || !gotAcc.AutoResume || gotAcc.PausedUntil == "" {
			t.Fatalf("expected 24h AutoResume pause, got %+v", gotAcc.Account)
		}
		p.markSessionVerified(acc.ID, 55555)
		_, _ = p.accounts.Update(acc.ID, func(r *accounts.Record) {
			r.PausedUntil = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
		})
		p.accounts.ResumeExpired()
		if p.sessionVerified(acc.ID, 55555) {
			t.Fatal("expected AutoResume hook to clear stale sessionVerified for fresh check")
		}
		gotAcc, _ = p.accounts.Record(acc.ID)
		if !gotAcc.Enabled || gotAcc.AutoResume {
			t.Fatalf("expected account re-enabled after 24h expiry, got %+v", gotAcc.Account)
		}
	})
}

type flakeyStatusPage struct {
	*fakePage
	mu          sync.Mutex
	statusCalls int
}

func (f *flakeyStatusPage) Status(ctx context.Context, sid string) (string, error) {
	f.mu.Lock()
	f.statusCalls++
	n := f.statusCalls
	f.mu.Unlock()
	if n <= 2 {
		return "", errors.New("gateway disconnected")
	}
	return f.fakePage.Status(ctx, sid)
}

func TestPollRecoversFromStatusDisconnectWithoutResending(t *testing.T) {
	events, files := videoResult("/out/polled.mp4", []byte("vid"))
	page := &flakeyStatusPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-poll"},
			statuses: []string{"completed"},
			events:   events,
			files:    files,
			quota:    &MuseQuota{PercentUsed: 20},
		},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "polled cat")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s-poll" || len(page.sends) != 1 || page.homes < 1 {
		t.Fatalf("expected mid-poll refresh to recover without resend: job=%+v sends=%d homes=%d", got, len(page.sends), page.homes)
	}
}

func TestReconcileRecoversHistoryViaRefreshPreventingSendUnknown(t *testing.T) {
	events, files := videoResult("/out/after-refresh.mp4", []byte("vid"))
	base := &fakePage{
		loggedIn: true,
		sendErr:  errors.New("websocket: close 1006"),
		statuses: []string{"completed"},
		events:   events,
		files:    files,
		quota:    &MuseQuota{PercentUsed: 15},
	}
	page := &trackedFakePage{
		fakePage:    base,
		sessions:    []SessionInfo{{ID: "s0", Status: "completed"}},
		userMsgs:    map[string][]UserEvent{},
		sessionsErr: nil,
	}
	var sent atomic.Bool
	base.onSend = func() {
		sent.Store(true)
		page.mu.Lock()
		// Gateway disconnects right after accepting send, then recovers on Home refresh.
		page.sessionsErr = errors.New("gateway disconnected")
		page.sessions = append(page.sessions, SessionInfo{ID: "s-created", Status: "running"})
		page.userMsgs["s-created"] = []UserEvent{{Text: "cảnh bình minh trên biển", HasText: true}}
		page.mu.Unlock()
	}
	page.onHome = func() {
		if sent.Load() {
			page.mu.Lock()
			page.sessionsErr = nil
			page.mu.Unlock()
		}
	}

	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "cảnh bình minh trên biển")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s-created" || got.SendUnknown {
		t.Fatalf("expected reconcile refresh to recover session instead of errSendUnknown: job=%+v", got)
	}
}

func TestStateDrivenPumpRefreshesSignedOutEnabledAccountOnceWhenQueueHasWork(t *testing.T) {
	events, files := videoResult("/out/auto-recovered.mp4", []byte("vid"))
	page := &homeRecoverLoginPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-auto"},
			statuses: []string{"completed"},
			events:   events,
			files:    files,
			quota:    &MuseQuota{PercentUsed: 10},
		},
		recoverOn: 1,
	}
	p := testProvider(t, page)
	signedOut := accounts.Record{Account: accounts.Account{
		ID: "acc-signedout", Provider: accounts.ProviderMuse, Name: "Recovered",
		Source: accounts.SourceBrowser, Status: accounts.StatusSignedOut, Enabled: true, Proxy: proxy.None,
	}}
	deadDisabled := accounts.Record{Account: accounts.Account{
		ID: "acc-dead", Provider: accounts.ProviderMuse, Name: "Dead",
		Source: accounts.SourceBrowser, Status: accounts.StatusError, Enabled: false, Proxy: proxy.None,
	}}
	if err := p.accounts.Seed(signedOut); err != nil {
		t.Fatal(err)
	}
	if err := p.accounts.Seed(deadDisabled); err != nil {
		t.Fatal(err)
	}
	job := submit(t, p, MuseJobVideo, "chó chạy trên cỏ")[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx, nil)
	defer p.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := mustJob(t, p, job.ID)
		if got.Status == MuseJobSucceeded {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded {
		t.Fatalf("expected state-driven refresh to revive SignedOut enabled account and complete job: job=%+v", got)
	}
	if acc, _ := p.accounts.Record(deadDisabled.ID); acc.Enabled || acc.Status != accounts.StatusError {
		t.Fatalf("permanently disabled dead account should not be re-enabled: %+v", acc.Account)
	}
}

func TestTabPoolInvalidateAccountClosesExcessAndMarksRemainingReload(t *testing.T) {
	const port = 60120
	fc := newFakeChromeTabs(port, 1)
	tp := fc.pool()
	policy := config.DefaultAccountConfig()
	ctx := context.Background()

	t1, err := tp.acquire(ctx, "accA", "j1", port, 2, policy)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := tp.acquire(ctx, "accA", "j2", port, 2, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Release t2 so it becomes an idle secondary tab, while t1 remains kept.
	tp.release(ctx, "accA", port, t2, false, func() int { return 2 }, policy)
	tp.release(ctx, "accA", port, t1, false, func() int { return 2 }, policy)

	tp.invalidateAccount(ctx, "accA", port, t1, policy)

	if len(fc.closeCalls) != 1 || fc.closeCalls[0] != t2 {
		t.Fatalf("expected excess idle tab %s closed, closeCalls=%v", t2, fc.closeCalls)
	}
	fc.mu.Lock()
	for i := range fc.targets {
		if fc.targets[i].ID == t1 {
			fc.targets[i].URL = "https://muse.ai/chat/stale"
		}
	}
	fc.mu.Unlock()

	reused, err := tp.acquire(ctx, "accA", "j3", port, 2, policy)
	if err != nil || reused != t1 {
		t.Fatalf("reused = (%q, %v), want %s", reused, err, t1)
	}
	fc.mu.Lock()
	finalURL := fc.targets[0].URL
	fc.mu.Unlock()
	if finalURL != MuseURL {
		t.Fatalf("expected invalidated slot %s to reload to %s on acquire, got %q", t1, MuseURL, finalURL)
	}
}

func TestSiblingVideoThreadsAbortBeforeSendingOnceVideoDisabled(t *testing.T) {
	t.Run("subsequent runOne aborts immediately when SkipVideoCheck is set", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "Video generation is not available in your country."}},
			quota:    &MuseQuota{PercentUsed: 10},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		jobs := submit(t, p, MuseJobVideo, "video 1", "video 2")

		p.runOne(context.Background(), jobs[0].ID, acc.ID)
		p.runOne(context.Background(), jobs[1].ID, acc.ID)

		if got := len(page.sends); got != 1 {
			t.Fatalf("expected thread 2 to abort before sending once SkipVideoCheck turned on; sends = %d, want 1", got)
		}
		got2 := mustJob(t, p, jobs[1].ID)
		if got2.Status != MuseJobQueued || got2.Attempts != 0 {
			t.Fatalf("second video job should return untouched to queue for another account, got %+v", got2)
		}
		gotAcc, _ := p.accounts.Record(acc.ID)
		if !gotAcc.SkipVideoCheck || !gotAcc.Enabled || gotAcc.Status != accounts.StatusSignedIn {
			t.Fatalf("expected account to keep detailed SkipVideoCheck image-only state: %+v", gotAcc.Account)
		}
		if !usableForJob(museView(gotAcc), MuseJobImage, p.cfg.Get().Muse, p.now()) {
			t.Fatal("account should still run image jobs")
		}
		if usableForJob(museView(gotAcc), MuseJobVideo, p.cfg.Get().Muse, p.now()) {
			t.Fatal("account must not take video jobs once SkipVideoCheck is set")
		}
	})

	t.Run("concurrent sibling waiting on sendSlot aborts without sending once video disabled", func(t *testing.T) {
		enteredSend := make(chan struct{})
		allowFinish := make(chan struct{})
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "Video generation is not available in your region."}},
			quota:    &MuseQuota{PercentUsed: 10},
			onSend: func() {
				select {
				case <-enteredSend:
				default:
					close(enteredSend)
				}
				<-allowFinish
			},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		jobs := submit(t, p, MuseJobVideo, "video 1", "video 2")

		// Start thread 2 in send() waiting on sendSlot while thread 1 holds it and sets SkipVideoCheck=true.
		releaseSlot, err := p.sendSlot(context.Background(), acc.ID)
		if err != nil {
			t.Fatal(err)
		}
		done2 := make(chan struct{})
		go func() {
			defer close(done2)
			p.runOne(context.Background(), jobs[1].ID, acc.ID)
		}()
		// Give goroutine 2 time to pass execute's initial check and block on sendSlot.
		time.Sleep(40 * time.Millisecond)
		p.disableVideoForAccount(acc.ID, "region locked")
		close(allowFinish)
		releaseSlot()
		<-done2
		_ = enteredSend

		if got := len(page.sends); got != 0 {
			t.Fatalf("sibling waiting on sendSlot sent anyway: sends = %d, want 0", got)
		}
		got2 := mustJob(t, p, jobs[1].ID)
		if got2.Status != MuseJobQueued || got2.Attempts != 0 {
			t.Fatalf("second video job should return to queue untouched: %+v", got2)
		}
	})
}

func TestStateDrivenRefreshRunsOnceOnSignedOutAndSkipsFreshAccount(t *testing.T) {
	var loginCalls atomic.Int32
	var openCalls atomic.Int32
	page := &countingLoginFailPage{fakePage: &fakePage{loggedIn: false}, calls: &loginCalls}
	p := testProvider(t, page)
	origPageFor := p.pageFor
	p.pageFor = func(ctx context.Context, rec accounts.Record) (Page, error) {
		openCalls.Add(1)
		return origPageFor(ctx, rec)
	}

	fresh := accounts.Record{Account: accounts.Account{
		ID: "acc-fresh", Provider: accounts.ProviderMuse, Name: "Muse 5",
		Source: accounts.SourceBrowser, Status: accounts.StatusUnverified, Enabled: true, Proxy: proxy.None,
	}}
	signedOut := accounts.Record{Account: accounts.Account{
		ID: "acc-signedout", Provider: accounts.ProviderMuse, Name: "SignedOut",
		Source: accounts.SourceBrowser, Status: accounts.StatusSignedOut, Enabled: true, Proxy: proxy.None,
		LastCheckedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}}
	if err := p.accounts.Seed(fresh); err != nil {
		t.Fatal(err)
	}
	if err := p.accounts.Seed(signedOut); err != nil {
		t.Fatal(err)
	}
	_ = submit(t, p, MuseJobVideo, "một con mèo")[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx, nil)
	defer p.Close()

	// Wait for the one-shot refresh of signedOut to complete and settle.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		busy := p.stateRefreshBusy[signedOut.ID]
		_, seen := p.stateRefreshSeen[signedOut.ID]
		p.mu.Unlock()
		if seen && !busy && openCalls.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Let several pump/trigger cycles run; signedOut must NOT refresh again.
	time.Sleep(250 * time.Millisecond)

	if got := openCalls.Load(); got != 1 {
		t.Fatalf("expected signed-out background refresh to open page once, got %d (loginCalls=%d)", got, loginCalls.Load())
	}
	gotFresh, _ := p.accounts.Record(fresh.ID)
	if gotFresh.Status != accounts.StatusUnverified || gotFresh.LastCheckedAt != "" {
		t.Fatalf("fresh browser account should stay unverified untouched, got %+v", gotFresh.Account)
	}
}

type countingLoginFailPage struct {
	*fakePage
	calls *atomic.Int32
}

func (c *countingLoginFailPage) Login(context.Context) (LoginState, error) {
	c.calls.Add(1)
	return LoginState{LoggedIn: false, Chat: false, HTTP: 401}, nil
}
