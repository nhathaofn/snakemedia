package muse

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
)

type trackedFakePage struct {
	*fakePage
	sessions    []SessionInfo
	sessionsErr error
	userMsgs    map[string][]UserEvent
	userMsgsErr error
	homeErr     error
	onHome      func()
	onSessions  func(call int)
	sessionCall int
}

func (f *trackedFakePage) Home(ctx context.Context) error {
	if err := f.fakePage.Home(ctx); err != nil {
		return err
	}
	f.mu.Lock()
	hook := f.onHome
	err := f.homeErr
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}

func (f *trackedFakePage) Sessions(context.Context, bool) ([]SessionInfo, error) {
	f.mu.Lock()
	f.sessionCall++
	call := f.sessionCall
	hook := f.onSessions
	f.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionsErr != nil {
		return nil, f.sessionsErr
	}
	out := make([]SessionInfo, len(f.sessions))
	copy(out, f.sessions)
	return out, nil
}

func (f *trackedFakePage) UserMessages(_ context.Context, sid string) ([]UserEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userMsgsErr != nil {
		return nil, f.userMsgsErr
	}
	msgs := f.userMsgs[sid]
	out := make([]UserEvent, len(msgs))
	copy(out, msgs)
	return out, nil
}

func TestPreSendCDPErrorRequeuesWithoutUnknown(t *testing.T) {
	page := &trackedFakePage{
		fakePage: &fakePage{
			loggedIn: true,
			sendErr:  &preSendError{err: errors.New("Chrome CDP chưa sẵn sàng.")},
		},
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con mèo")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || got.Sending || got.SendUnknown || got.PreSendTracked {
		t.Fatalf("job = %+v", got)
	}
	if a, _ := p.accounts.Record(acc.ID); museView(a).LastSentAt != "" {
		t.Fatal("pre-send failure recorded LastSentAt")
	}
}

func TestSendTimeoutRecoversSessionFromHistory(t *testing.T) {
	events, files := videoResult("/out/recovered.mp4", []byte("video-bytes"))
	base := &fakePage{
		loggedIn: true,
		sendErr:  context.DeadlineExceeded,
		statuses: []string{"running", "completed"},
		events:   events,
		files:    files,
	}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		userMsgs: map[string][]UserEvent{},
	}
	base.onSend = func() {
		page.mu.Lock()
		page.sessions = append(page.sessions, SessionInfo{ID: "s1", Status: "running"})
		page.userMsgs["s1"] = []UserEvent{{Text: "một con mèo chạy trên bãi biển", HasText: true}}
		page.mu.Unlock()
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con mèo chạy trên bãi biển")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s1" || got.Sending || got.SendUnknown || got.PreSendTracked || len(got.Files) != 1 {
		t.Fatalf("job = %+v", got)
	}
	if len(base.sends) != 1 || base.homes != 1 {
		t.Fatalf("sends=%d homes=%d, want 1 and 1", len(base.sends), base.homes)
	}
}

func TestSendTimeoutConfirmedNotSentRequeuesSafely(t *testing.T) {
	base := &fakePage{
		loggedIn: true,
		sendErr:  context.DeadlineExceeded,
	}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		userMsgs: map[string][]UserEvent{},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "prompt chưa gửi được")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || got.Sending || got.SendUnknown || got.PreSendTracked || got.SessionID != "" {
		t.Fatalf("job = %+v", got)
	}
	if base.homes != 1 {
		t.Fatalf("homes=%d, want 1 so hung send JS is killed before retry", base.homes)
	}
}

func TestSendTimeoutIgnoresUnrelatedManualChat(t *testing.T) {
	base := &fakePage{
		loggedIn: true,
		sendErr:  context.DeadlineExceeded,
	}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		userMsgs: map[string][]UserEvent{},
	}
	base.onSend = func() {
		page.mu.Lock()
		page.sessions = append(page.sessions, SessionInfo{ID: "s-manual", Status: "completed"})
		page.userMsgs["s-manual"] = []UserEvent{{Text: "draw a completely different castle", HasText: true}}
		page.mu.Unlock()
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "a small red paper boat on a calm lake")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || got.SessionID != "" || got.SendUnknown {
		t.Fatalf("job = %+v", got)
	}
}

func TestSendTimeoutFailsClosedOnAmbiguousOrUnreachableHistory(t *testing.T) {
	t.Run("ambiguous empty new session", func(t *testing.T) {
		base := &fakePage{loggedIn: true, sendErr: context.DeadlineExceeded}
		page := &trackedFakePage{
			fakePage: base,
			sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
			userMsgs: map[string][]UserEvent{},
		}
		base.onSend = func() {
			page.mu.Lock()
			page.sessions = append(page.sessions, SessionInfo{ID: "s-ambig", Status: "running"})
			page.userMsgs["s-ambig"] = []UserEvent{{HasText: false}}
			page.mu.Unlock()
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "x")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobFailed || !got.SendUnknown || got.Sending || got.PreSendTracked {
			t.Fatalf("job = %+v", got)
		}
	})

	t.Run("sessions list fails during reconcile", func(t *testing.T) {
		base := &fakePage{loggedIn: true, sendErr: errors.New("websocket: close 1006")}
		page := &trackedFakePage{
			fakePage: base,
			sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		}
		base.onSend = func() {
			page.mu.Lock()
			page.sessionsErr = errors.New("gateway disconnected")
			page.mu.Unlock()
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "x")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobFailed || !got.SendUnknown || got.Sending || got.PreSendTracked {
			t.Fatalf("job = %+v", got)
		}
	})
}

func TestAcceptedSendWithoutSessionRecoversAndFailsClosedOtherwise(t *testing.T) {
	t.Run("recovers matching session without reloading tab", func(t *testing.T) {
		events, files := videoResult("/out/a.mp4", []byte("v"))
		base := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true},
			statuses: []string{"running", "completed"},
			events:   events,
			files:    files,
		}
		page := &trackedFakePage{
			fakePage: base,
			sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
			userMsgs: map[string][]UserEvent{},
		}
		base.onSend = func() {
			page.mu.Lock()
			page.sessions = append(page.sessions, SessionInfo{ID: "s-new", Status: "running"})
			page.userMsgs["s-new"] = []UserEvent{{Text: "một cảnh quay điện ảnh buổi sáng", HasText: true}}
			page.mu.Unlock()
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "một cảnh quay điện ảnh buổi sáng")[0]

		p.runOne(context.Background(), job.ID, acc.ID)
		p.wg.Wait()

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobSucceeded || got.SessionID != "s-new" || base.homes != 0 {
			t.Fatalf("job = %+v, homes=%d", got, base.homes)
		}
	})

	t.Run("ok without session never becomes notSent", func(t *testing.T) {
		base := &fakePage{loggedIn: true, sendRes: SendResult{OK: true}}
		page := &trackedFakePage{
			fakePage: base,
			sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
			userMsgs: map[string][]UserEvent{},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "x")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobFailed || !got.SendUnknown || got.Error != errSendUnknown.Error() {
			t.Fatalf("job = %+v", got)
		}
	})
}

func TestCancelDuringTimedOutSendCancelsRecoveredSession(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	base := &fakePage{loggedIn: true, sendErr: context.DeadlineExceeded}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		userMsgs: map[string][]UserEvent{},
	}
	base.onSend = func() {
		close(entered)
		<-release
		page.mu.Lock()
		page.sessions = append(page.sessions, SessionInfo{ID: "paid-timeout", Status: "running"})
		page.userMsgs["paid-timeout"] = []UserEvent{{Text: "cảnh thành phố ban đêm", HasText: true}}
		page.mu.Unlock()
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "cảnh thành phố ban đêm")[0]
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancels[job.ID] = cancel
	p.mu.Unlock()
	done := make(chan struct{})
	go func() {
		p.runOne(ctx, job.ID, acc.ID)
		close(done)
	}()

	<-entered
	if res := p.CancelJob(job.ID); !res.Success {
		t.Fatal(res.Error)
	}
	close(release)
	<-done

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobCancelled || got.SessionID != "paid-timeout" || got.Sending || got.PreSendTracked {
		t.Fatalf("job = %+v", got)
	}
	if len(base.cancelled) != 1 || base.cancelled[0] != "paid-timeout" {
		t.Fatalf("cancelled = %v, want [paid-timeout]", base.cancelled)
	}
}

func TestRestartDuringTrackedSendReconcilesOnSameAccount(t *testing.T) {
	t.Run("recovers session after restart", func(t *testing.T) {
		events, files := videoResult("/out/resumed.mp4", []byte("v"))
		base := &fakePage{loggedIn: true, statuses: []string{"completed"}, history: events, files: files}
		page := &trackedFakePage{
			fakePage: base,
			sessions: []SessionInfo{{ID: "s0", Status: "completed"}, {ID: "s-after-crash", Status: "completed"}},
			userMsgs: map[string][]UserEvent{
				"s-after-crash": {{Text: "một dòng sông yên bình", HasText: true}},
			},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		other := addSignedIn(t, p, "B")
		job := submit(t, p, MuseJobVideo, "một dòng sông yên bình")[0]
		p.jobs.update(job.ID, func(j *MuseJob) {
			j.Status, j.AccountID, j.Sending = MuseJobProcessing, acc.ID, true
			j.PreSendTracked, j.PreSendSessions, j.Attempts = true, []string{"s0"}, 1
		})

		restarted := New(filepath.Dir(p.root), p.defaultOutputDir, p.cfg, p.media, p.accounts)
		restarted.pageFor = func(context.Context, accounts.Record) (Page, error) { return page, nil }
		restarted.now = p.now
		restarted.sleep = p.sleep
		ctx, cancel := context.WithCancel(context.Background())
		restarted.Start(ctx, nil)

		got := mustJob(t, restarted, job.ID)
		if got.Status != MuseJobProcessing || !got.Sending || !got.PreSendTracked {
			t.Fatalf("after start = %+v", got)
		}
		assigned := plan([]MuseJob{got}, []MuseAccount{
			{ID: other.ID, Enabled: true, Status: accounts.StatusSignedIn},
			{ID: acc.ID, Enabled: true, Status: accounts.StatusSignedIn},
		}, restarted.cfg.Get().Muse, map[string]string{}, restarted.now())
		if len(assigned) != 1 || assigned[0].accountID != acc.ID {
			t.Fatalf("planned = %+v, want only original account %s", assigned, acc.ID)
		}

		restarted.runOne(ctx, job.ID, acc.ID)
		cancel()
		restarted.Close()

		got = mustJob(t, restarted, job.ID)
		if got.Status != MuseJobSucceeded || got.SessionID != "s-after-crash" || len(base.sends) != 0 {
			t.Fatalf("after reconcile = %+v, sends=%d", got, len(base.sends))
		}
	})

	t.Run("not created before restart requeues safely", func(t *testing.T) {
		base := &fakePage{loggedIn: true}
		page := &trackedFakePage{
			fakePage: base,
			sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "chưa tới server")[0]
		p.jobs.update(job.ID, func(j *MuseJob) {
			j.Status, j.AccountID, j.Sending = MuseJobProcessing, acc.ID, true
			j.PreSendTracked, j.PreSendSessions, j.Attempts = true, []string{"s0"}, 1
		})

		restarted := New(filepath.Dir(p.root), p.defaultOutputDir, p.cfg, p.media, p.accounts)
		restarted.pageFor = func(context.Context, accounts.Record) (Page, error) { return page, nil }
		restarted.now = p.now
		restarted.sleep = p.sleep
		ctx, cancel := context.WithCancel(context.Background())
		restarted.Start(ctx, nil)
		restarted.runOne(ctx, job.ID, acc.ID)
		cancel()
		restarted.Close()

		got := mustJob(t, restarted, job.ID)
		if got.Status != MuseJobQueued || got.Attempts != 0 || got.Sending || got.SendUnknown || got.PreSendTracked {
			t.Fatalf("requeued = %+v", got)
		}
	})

	t.Run("signed out account stays pinned instead of handing off", func(t *testing.T) {
		base := &fakePage{loggedIn: false}
		page := &trackedFakePage{fakePage: base}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "chờ đăng nhập lại")[0]
		p.jobs.update(job.ID, func(j *MuseJob) {
			j.Status, j.AccountID, j.Sending = MuseJobProcessing, acc.ID, true
			j.PreSendTracked, j.PreSendSessions, j.Attempts = true, []string{"s0"}, 1
		})

		p.runOne(context.Background(), job.ID, acc.ID)

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobProcessing || got.AccountID != acc.ID || !got.Sending || !got.PreSendTracked {
			t.Fatalf("signed out tracked send moved off account: %+v", got)
		}
	})
}

func TestSendContinuePreSendErrorRevertsContinueWithoutUnknown(t *testing.T) {
	page := &fakePage{
		loggedIn:  true,
		sendToErr: &preSendError{err: errTabMissing},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submitLong(t, p, 30)
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.SessionID, j.Attempts = MuseJobProcessing, acc.ID, "s1", 1
		j.SentAt = "2026-11-19T00:00:00Z"
	})
	job = mustJob(t, p, job.ID)

	got, err := p.sendContinue(context.Background(), job, acc, page, 2)
	var notSent *notSentError
	if !errors.As(err, &notSent) || !errors.Is(err, errTabMissing) {
		t.Fatalf("sendContinue err = %v, want notSentError wrapping errTabMissing", err)
	}
	if got.Continues != 0 || got.RepliesSeen != 0 || got.SentAt != "2026-11-19T00:00:00Z" {
		t.Fatalf("sendContinue did not revert state: %+v", got)
	}
}

func TestPromptMatchesEdgeCases(t *testing.T) {
	longPrompt := "One continuous 10-second cinematic shot set on the opposite side of the lobby"
	sent := "Task: Create a new video.\n\nprompt:\n" + longPrompt
	if !promptMatches(sent, longPrompt, sent) {
		t.Fatal("exact sent text should match")
	}
	if !promptMatches("Task: Create a new video.\nprompt:\nx", "x", "x") {
		t.Fatal("exact short prompt should match")
	}
	if !promptMatches(sent, longPrompt, "Image 1\n"+longPrompt+" extra note") {
		t.Fatal("long prompt substring should match")
	}
	if !promptMatches(sent, longPrompt, sent[:55]+"...") {
		t.Fatal("truncated prefix should match")
	}
	if promptMatches(sent, longPrompt, "a completely unrelated chat") {
		t.Fatal("unrelated text matched")
	}
	if promptMatches(sent, "x", "x extra words") {
		t.Fatal("short prompt substring should not match unrelated text")
	}
}

func TestReopenSignedInWhenHomeFailsDuringReconcile(t *testing.T) {
	events, files := videoResult("/out/reopened.mp4", []byte("v"))
	brokenBase := &fakePage{loggedIn: true, sendErr: errors.New("tab crashed")}
	broken := &trackedFakePage{
		fakePage: brokenBase,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		homeErr:  errors.New("tab closed"),
	}
	freshBase := &fakePage{loggedIn: true, statuses: []string{"completed"}, history: events, files: files}
	fresh := &trackedFakePage{
		fakePage: freshBase,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}, {ID: "s-reopened", Status: "completed"}},
		userMsgs: map[string][]UserEvent{"s-reopened": {{Text: "một con chim bay qua", HasText: true}}},
	}
	p := testProvider(t, broken)
	calls := 0
	p.pageFor = func(context.Context, accounts.Record) (Page, error) {
		calls++
		if calls == 1 {
			return broken, nil
		}
		return fresh, nil
	}
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con chim bay qua")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s-reopened" || calls != 2 {
		t.Fatalf("job = %+v, calls = %d", got, calls)
	}
}

// Verify reconcile timeout doesn't block tests when context is normal.
func TestReconcileUsesFakeSleepWithoutRealDelay(t *testing.T) {
	base := &fakePage{loggedIn: true, sendErr: context.DeadlineExceeded}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "fast")[0]
	start := time.Now()
	p.runOne(context.Background(), job.ID, acc.ID)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("reconcile slept in real time: %v", time.Since(start))
	}
}

func TestSendEvalErrorConfirmedNotSentFailsAfterMaxAttempts(t *testing.T) {
	base := &fakePage{
		loggedIn: true,
		sendErr:  &browser.EvalError{Detail: "TypeError: e[i] is not a function"},
	}
	page := &trackedFakePage{
		fakePage: base,
		sessions: []SessionInfo{{ID: "s0", Status: "completed"}},
		userMsgs: map[string][]UserEvent{},
	}
	p := testProvider(t, page)
	accA := addSignedIn(t, p, "A")
	accB := addSignedIn(t, p, "B")
	job := submit(t, p, MuseJobVideo, "một con mèo")[0]

	p.runOne(context.Background(), job.ID, accA.ID)
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 1 || got.SendUnknown {
		t.Fatalf("after 1st eval error: job = %+v", got)
	}

	p.runOne(context.Background(), job.ID, accB.ID)
	got = mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || got.Attempts != 2 || !strings.Contains(got.Error, "TypeError: e[i] is not a function") {
		t.Fatalf("after 2nd eval error: job = %+v", got)
	}
}

func TestSendContinueRestrictionClassifiesAndKeepsLongChat(t *testing.T) {
	t.Run("24h daily limit on continue #1 pauses account and holds job on same SessionID", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-long-1"},
			quota:    &MuseQuota{PercentUsed: 15},
			events:   []AssistantEvent{{Text: "Clips 1-3 ready.\nSTATUS: INCOMPLETE clips 4-6"}},
			sendToRes: SendResult{
				OK:          false,
				Reason:      "limit_reached",
				Diagnostics: &SendDiagnostics{Message: "You have reached your daily video generation limit. Try again in 24 hours."},
			},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		otherAcc := addSignedIn(t, p, "B")
		job := submitLong(t, p, 60)

		p.runOne(context.Background(), job.ID, acc.ID)

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobProcessing || got.SessionID != "s-long-1" || got.AccountID != acc.ID || got.NextCheckAt == "" {
			t.Fatalf("expected long video to hold and stay on s-long-1/%s, got %+v (otherAcc=%s)", acc.ID, got, otherAcc.ID)
		}
		gotAcc, _ := p.accounts.Record(acc.ID)
		if gotAcc.Enabled || !gotAcc.AutoResume || gotAcc.PausedUntil == "" {
			t.Fatalf("expected account auto-disabled for 24h limit, got %+v", gotAcc.Account)
		}
	})

	t.Run("video ban on continue #1 fails on same SessionID without requeuing to another account", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-long-2"},
			quota:    &MuseQuota{PercentUsed: 15},
			events:   []AssistantEvent{{Text: "Clips 1-3 ready.\nSTATUS: INCOMPLETE clips 4-6"}},
			sendToRes: SendResult{
				OK:          false,
				Reason:      "restricted",
				Diagnostics: &SendDiagnostics{Message: "Video generation is disabled for your account."},
			},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		_ = addSignedIn(t, p, "B")
		job := submitLong(t, p, 60)

		p.runOne(context.Background(), job.ID, acc.ID)

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobFailed || got.SessionID != "s-long-2" || got.AccountID != acc.ID {
			t.Fatalf("expected banned long video to fail on s-long-2 without leaving account, got %+v", got)
		}
		gotAcc, _ := p.accounts.Record(acc.ID)
		if !gotAcc.SkipVideoCheck {
			t.Fatalf("expected SkipVideoCheck disabled after video ban on continue, got %+v", gotAcc.Account)
		}
	})
}
