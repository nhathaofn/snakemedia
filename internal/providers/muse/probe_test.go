package muse

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
)

func testSite(t *testing.T, page Page) (*Site, *config.Manager) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.NewManager(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewSite(cfg)
	s.newPage = func(*browser.Browser) Page { return page }
	s.sleep = func(context.Context, time.Duration) error { return nil }
	return s, cfg
}

type scriptLoginPage struct {
	*fakePage
	loginState LoginState
	loginErr   error
}

func (p *scriptLoginPage) Login(context.Context) (LoginState, error) {
	return p.loginState, p.loginErr
}

func TestSiteInspectDetectsDeadRedirectsAndBlockedSession(t *testing.T) {
	t.Run("redirect to dead/external path returns Fatal and disables", func(t *testing.T) {
		page := &scriptLoginPage{fakePage: &fakePage{}, loginErr: errTabMissing}
		s, _ := testSite(t, page)
		s.awayCheck = func(_ context.Context, _ int, _ config.AccountConfig, navigate bool) (awayTabInfo, error) {
			return awayTabInfo{
				URL:   "https://auth.example.com/suspended-account?token=secret",
				Ready: "complete",
				Title: "Account Suspended",
				Text:  "Your account has been suspended",
			}, nil
		}
		insp, err := s.Inspect(context.Background(), &browser.Browser{Port: 1234}, accounts.Record{})
		if err != nil {
			t.Fatalf("Inspect error = %v", err)
		}
		if insp.Fatal == "" || !insp.DisableAccount {
			t.Fatalf("expected Fatal + DisableAccount, got %+v", insp)
		}
		if strings.Contains(insp.Fatal, "token=secret") {
			t.Fatalf("URL query leaked in Fatal: %q", insp.Fatal)
		}
	})

	t.Run("redirect to ordinary login page returns signed out", func(t *testing.T) {
		page := &scriptLoginPage{fakePage: &fakePage{}, loginErr: errTabMissing}
		s, _ := testSite(t, page)
		s.awayCheck = func(context.Context, int, config.AccountConfig, bool) (awayTabInfo, error) {
			return awayTabInfo{
				URL:   "https://auth.example.com/login",
				Ready: "complete",
				Title: "Sign in",
				Text:  "Sign in to continue",
			}, nil
		}
		insp, err := s.Inspect(context.Background(), &browser.Browser{Port: 1234}, accounts.Record{})
		if err != nil || insp.SignedIn || insp.Fatal != "" {
			t.Fatalf("unexpected inspect on login redirect: insp=%+v err=%v", insp, err)
		}
	})

	t.Run("session API 403 or banned status returns Fatal immediately", func(t *testing.T) {
		page := &scriptLoginPage{
			fakePage:   &fakePage{},
			loginState: LoginState{HTTP: 403, Status: "banned", Error: "account_banned"},
		}
		s, _ := testSite(t, page)
		insp, err := s.Inspect(context.Background(), &browser.Browser{}, accounts.Record{})
		if err != nil || insp.Fatal == "" || !insp.DisableAccount {
			t.Fatalf("expected Fatal + DisableAccount on banned session, got insp=%+v err=%v", insp, err)
		}
	})
}

func TestSiteCheckVideoProbe(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	t.Run("success only reads chat.history once session leaves running", func(t *testing.T) {
		ev, _ := videoResult("/cat.mp4", []byte("mp4"))
		ev[0].Text = "DONE"
		inner := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "check-1"},
			statuses: []string{"running", "running", "running", "completed"},
			events:   ev,
			quota:    &MuseQuota{PercentUsed: 15, Tier: "Pro"},
		}
		resultCalls := 0
		page := &countingResultPage{fakePage: inner, onResult: func() { resultCalls++ }}
		s, _ := testSite(t, page)
		s.now = func() time.Time { return now }

		insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
		if err != nil || insp.Fatal != "" || !insp.Resume || !insp.VideoVerified || len(inner.sends) != 1 {
			t.Fatalf("successful video probe failed: insp=%+v err=%v sends=%d", insp, err, len(inner.sends))
		}
		if resultCalls != 1 {
			t.Fatalf("Result() called %d times while running/completed, want 1", resultCalls)
		}
		if len(inner.sends) != 1 || inner.sends[0] != "Task: Create a new video.\n"+requestRules+"\nDuration: 2 seconds\nprompt:\nA cute cat sitting still by a sunny window." {
			t.Fatalf("unexpected video check prompt wording: %q", inner.sends)
		}
	})

	t.Run("DONE text without a video file disables video check while staying SignedIn", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "check-2"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "DONE"}},
			quota:    &MuseQuota{PercentUsed: 15},
		}
		s, _ := testSite(t, page)
		s.now = func() time.Time { return now }

		insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
		if err != nil || insp.Fatal != "" || insp.DisableAccount || !insp.SignedIn || !insp.VideoCheckDisabled {
			t.Fatalf("text-only reply should disable video check without Fatal/error, got insp=%+v err=%v", insp, err)
		}
	})

	t.Run("SkipVideoCheck skips video probe and only verifies account is alive", func(t *testing.T) {
		page := &fakePage{loggedIn: true}
		s, _ := testSite(t, page)
		insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{Account: accounts.Account{SkipVideoCheck: true}}, accounts.Inspection{SignedIn: true})
		if err != nil || len(page.sends) != 0 || insp.Fatal != "" || !insp.SignedIn {
			t.Fatalf("expected skipped video send when SkipVideoCheck=true: sends=%d insp=%+v err=%v", len(page.sends), insp, err)
		}
	})

	t.Run("daily video limit during check disables account for 24h with AutoResume", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes: SendResult{
				OK:          false,
				Reason:      "send_failed",
				Diagnostics: &SendDiagnostics{Message: "Daily video generation limit reached for today"},
			},
			quota: &MuseQuota{PercentUsed: 20},
		}
		s, _ := testSite(t, page)
		s.now = func() time.Time { return now }

		insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
		if err != nil || insp.Fatal != "" {
			t.Fatalf("expected pause inspection without Fatal, got insp=%+v err=%v", insp, err)
		}
		wantUntil := now.Add(24 * time.Hour)
		if !insp.DisableAccount || !insp.AutoResume || !insp.PausedUntil.Equal(wantUntil) {
			t.Fatalf("24h pause fields mismatch: %+v, want until %v", insp, wantUntil)
		}
	})

	t.Run("weekly exhausted without topup skips video send", func(t *testing.T) {
		page := &fakePage{loggedIn: true}
		s, cfg := testSite(t, page)
		s.now = func() time.Time { return now }
		c := cfg.Get()
		c.Muse.UseTopup = false
		if _, err := cfg.Update(c); err != nil {
			t.Fatal(err)
		}
		initial := accounts.Inspection{
			SignedIn: true,
			Data:     museData{Quota: &MuseQuota{PercentUsed: 100, TopupBalance: 0}},
		}
		insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, initial)
		if err != nil || len(page.sends) != 0 || insp.PausedUntil.IsZero() {
			t.Fatalf("weekly out probe: sends=%d insp=%+v err=%v", len(page.sends), insp, err)
		}
	})

	t.Run("not_ready retries and transient timeouts do not disable account or video check", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: false, Reason: "not_ready (/settings)"},
		}
		s, _ := testSite(t, page)
		s.now = func() time.Time { return now }

		insp, err := s.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
		if err == nil || len(page.sends) != 3 {
			t.Fatalf("expected 3 send attempts and error on persistent not_ready, got sends=%d err=%v", len(page.sends), err)
		}
		if insp.DisableAccount || insp.VideoCheckDisabled || insp.Fatal != "" {
			t.Fatalf("transient not_ready must not disable account or video check: %+v", insp)
		}

		pageTimeout := &fakePage{
			loggedIn: true,
			sendRes: SendResult{
				OK:          false,
				Reason:      "send_failed",
				Diagnostics: &SendDiagnostics{Message: "gateway timeout", RequestTimedOut: true},
			},
		}
		sTimeout, _ := testSite(t, pageTimeout)
		sTimeout.now = func() time.Time { return now }
		inspTO, errTO := sTimeout.CheckVideo(context.Background(), &browser.Browser{}, accounts.Record{}, accounts.Inspection{SignedIn: true})
		if errTO == nil || inspTO.DisableAccount || inspTO.VideoCheckDisabled {
			t.Fatalf("transient RequestTimedOut must not mark account/video disabled: insp=%+v err=%v", inspTO, errTO)
		}
	})

	t.Run("generic 429 rate limit is not treated as 24h daily video limit", func(t *testing.T) {
		kind, _ := classifyRestriction("rate_limit", &SendDiagnostics{Message: "HTTP 429 too many requests"}, "", nil)
		if kind == restrictDailyVideo24h {
			t.Fatalf("generic 429 should not lock account for 24h, got %v", kind)
		}
	})
}

type countingResultPage struct {
	*fakePage
	onResult func()
}

func (c *countingResultPage) Result(ctx context.Context, sid string) ([]AssistantEvent, error) {
	if c.onResult != nil {
		c.onResult()
	}
	return c.fakePage.Result(ctx, sid)
}

func TestRunnerDailyVideo24hAutoDisableAndAutoResumeAndVideoBan(t *testing.T) {
	t.Run("24h daily video limit auto-disables, requeues job, and re-enables after 24h", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "You have reached your daily video generation limit. Try again in 24 hours."}},
			quota:    &MuseQuota{PercentUsed: 25},
		}
		p := testProvider(t, page)
		start := p.now()
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "cat")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		gotJob := mustJob(t, p, job.ID)
		if gotJob.Status != MuseJobQueued || gotJob.Attempts != 0 {
			t.Fatalf("job should requeue cleanly for another account, got %+v", gotJob)
		}
		gotAcc, _ := p.accounts.Record(acc.ID)
		if gotAcc.Enabled || !gotAcc.AutoResume || gotAcc.PausedUntil == "" {
			t.Fatalf("account should be auto-disabled for 24h, got %+v", gotAcc.Account)
		}
		until, err := time.Parse(time.RFC3339, gotAcc.PausedUntil)
		if err != nil || until.Sub(start) < 23*time.Hour || until.Sub(start) > 25*time.Hour {
			t.Fatalf("pausedUntil = %q (start=%v, err=%v)", gotAcc.PausedUntil, start, err)
		}
		var data museData
		_ = accounts.DecodeData(gotAcc, &data)
		if quotaResumes(data, &MuseQuota{PercentUsed: 25}) {
			t.Fatal("weekly quota check must not clear an active 24h daily_video rest")
		}

		// Calling ResumeExpired before 24h keeps it off; after 24h turns it back on.
		p.accounts.ResumeExpired()
		if a, _ := p.accounts.Record(acc.ID); a.Enabled {
			t.Fatal("account re-enabled before 24h passed")
		}
		// Seed a past PausedUntil to verify ResumeExpired re-enables immediately.
		_, _ = p.accounts.Update(acc.ID, func(r *accounts.Record) {
			r.PausedUntil = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
		})
		p.accounts.ResumeExpired()
		gotAcc, _ = p.accounts.Record(acc.ID)
		if !gotAcc.Enabled || gotAcc.AutoResume || gotAcc.PausedUntil != "" {
			t.Fatalf("account was not auto-re-enabled after 24h: %+v", gotAcc.Account)
		}
	})

	t.Run("video ban reply disables video check, stays SignedIn for images, and requeues video", func(t *testing.T) {
		page := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "Video generation is disabled for your account."}},
			quota:    &MuseQuota{PercentUsed: 10},
		}
		p := testProvider(t, page)
		acc := addSignedIn(t, p, "A")
		job := submit(t, p, MuseJobVideo, "cat")[0]

		p.runOne(context.Background(), job.ID, acc.ID)

		gotJob := mustJob(t, p, job.ID)
		if gotJob.Status != MuseJobQueued {
			t.Fatalf("job should move to another account, got %+v", gotJob)
		}
		gotAcc, _ := p.accounts.Record(acc.ID)
		if gotAcc.Status != accounts.StatusSignedIn || !gotAcc.Enabled || !gotAcc.SkipVideoCheck {
			t.Fatalf("video-disabled account should remain SignedIn & Enabled with SkipVideoCheck=true: %+v", gotAcc.Account)
		}

		// Scheduler must skip the video job but assign the image job to this account.
		view := museView(gotAcc)
		jobs := []MuseJob{
			{ID: "vid", Kind: MuseJobVideo, Status: MuseJobQueued, CreatedAt: "2026-11-19T10:00:00Z"},
			{ID: "img", Kind: MuseJobImage, Status: MuseJobQueued, CreatedAt: "2026-11-19T10:00:01Z"},
		}
		planned := plan(jobs, []MuseAccount{view}, p.cfg.Get().Muse, nil, p.now())
		if len(planned) != 1 || planned[0].jobID != "img" || planned[0].accountID != acc.ID {
			t.Fatalf("planned = %+v, want only image job on SkipVideoCheck account", planned)
		}
	})
}

func TestPageLoginCapturesBlockSignalsInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
global.fetch = async () => ({ status: 403, async json() { return { status: 'banned', error: 'account_banned' }; } });
global.document = { body: { innerText: 'Your account has been banned' }, querySelectorAll: () => [] };
const page = (` + pageSource + `)(1024);
(async () => {
  const st = await page.login();
  assert.equal(st.loggedIn, false);
  assert.equal(st.http, 403);
  assert.equal(st.status, 'banned');
  assert.equal(st.error, 'account_banned');
  assert.equal(st.notice, 'Your account has been banned');
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page login script: %v\n%s", err, out)
	}
}

type inspectSeqPage struct {
	*fakePage
	states   []LoginState
	idx      int
	homes    int
	loginErr error
}

func (p *inspectSeqPage) Login(context.Context) (LoginState, error) {
	if p.loginErr != nil {
		return LoginState{}, p.loginErr
	}
	if len(p.states) == 0 {
		return p.fakePage.Login(context.Background())
	}
	i := min(p.idx, len(p.states)-1)
	p.idx++
	return p.states[i], nil
}

func (p *inspectSeqPage) Home(context.Context) error {
	p.homes++
	return nil
}

func TestSiteInspectAvoidsReloadLoopsAndOAuthYanking(t *testing.T) {
	t.Run("HTTP 200 without chat at home path hydrates without reloading and reports errPageNotReady until chat appears", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{quota: &MuseQuota{PercentUsed: 10}},
			states: []LoginState{
				{HTTP: 200, Chat: false, Path: "/"},
				{HTTP: 200, Chat: false, Path: "/"},
				{HTTP: 200, Chat: true, LoggedIn: true, Path: "/"},
			},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())

		insp, err := s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m1"}})
		if !errors.Is(err, errPageNotReady) || insp.SignedIn {
			t.Fatalf("poll 1: want errPageNotReady, got insp=%+v err=%v", insp, err)
		}
		insp, err = s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m1"}})
		if !errors.Is(err, errPageNotReady) || insp.SignedIn {
			t.Fatalf("poll 2: want errPageNotReady, got insp=%+v err=%v", insp, err)
		}
		insp, err = s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m1"}})
		if err != nil || !insp.SignedIn {
			t.Fatalf("poll 3: expected SignedIn once chat hydrated, got insp=%+v err=%v", insp, err)
		}
		if page.homes != 0 {
			t.Fatalf("expected 0 Home reloads while hydrating at /, got %d", page.homes)
		}
	})

	t.Run("repeated 401 polls inside one Check scope reload Home at most once", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{},
			states: []LoginState{
				{HTTP: 401, Chat: false, Path: "/"},
			},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())

		for range 5 {
			insp, err := s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m2"}})
			if err != nil || insp.SignedIn {
				t.Fatalf("expected signed out inspection without error on 401, got insp=%+v err=%v", insp, err)
			}
		}
		if page.homes != 1 {
			t.Fatalf("expected Home reload once per Check scope, got %d", page.homes)
		}
	})

	t.Run("off-home route without chat navigates Home once per Check scope", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{quota: &MuseQuota{PercentUsed: 5}},
			states: []LoginState{
				{HTTP: 200, Chat: false, Path: "/settings"},
				{HTTP: 200, Chat: false, Path: "/settings"},
			},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())

		for range 3 {
			_, _ = s.Inspect(ctx, &browser.Browser{Port: 1234, Headless: true}, accounts.Record{Account: accounts.Account{ID: "m3"}})
		}
		if page.homes != 1 {
			t.Fatalf("expected Home reload once for /settings route, got %d", page.homes)
		}
	})

	t.Run("interactive visible sign-in never reloads Home or pulls away from Google OAuth", func(t *testing.T) {
		page := &inspectSeqPage{
			fakePage: &fakePage{},
			states:   []LoginState{{HTTP: 401, Chat: false, Path: "/"}},
		}
		s, _ := testSite(t, page)
		ctx := accounts.WithInspectScope(context.Background())
		visible := &browser.Browser{Port: 1234, Headless: false}
		rec := accounts.Record{Account: accounts.Account{ID: "m4", Status: accounts.StatusSigningIn}}

		for range 4 {
			insp, err := s.Inspect(ctx, visible, rec)
			if err != nil || insp.SignedIn {
				t.Fatalf("unexpected result during interactive sign-in: insp=%+v err=%v", insp, err)
			}
		}
		if page.homes != 0 {
			t.Fatalf("interactive sign-in must not reload Home on /, got %d reloads", page.homes)
		}

		navs := 0
		pageOAuth := &inspectSeqPage{fakePage: &fakePage{}, loginErr: errTabMissing}
		sOAuth, _ := testSite(t, pageOAuth)
		sOAuth.awayCheck = func(_ context.Context, _ int, _ config.AccountConfig, navigate bool) (awayTabInfo, error) {
			if navigate {
				navs++
			}
			return awayTabInfo{
				URL:   "https://accounts.google.com/v3/signin/identifier",
				Ready: "complete",
				Title: "Sign in - Google Accounts",
			}, nil
		}
		insp, err := sOAuth.Inspect(ctx, visible, rec)
		if err != nil || insp.SignedIn || insp.Fatal != "" || navs != 0 {
			t.Fatalf("interactive Google OAuth must not navigate away: insp=%+v err=%v navs=%d", insp, err, navs)
		}
	})
}
