package muse

// Regression tests for the [A] findings of
// docs/superpowers/plans/2026-10-26-muse-review.md (sections 2 and 3).

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers/flow/chrome"
)

// 2.1: a scene sent into the shared storyboard chat takes only its own reply,
// not the files and text earlier turns left in that chat.
func TestThreadJobIgnoresEarlierRepliesInChat(t *testing.T) {
	oldScene, _ := videoResult("/out/scene1.mp4", []byte("scene-1"))
	oldScene[0].Text = "You have reached your weekly limit." // an old turn's text must not classify this job
	newScene, files := videoResult("/out/scene2.mp4", []byte("scene-2"))
	files["/out/scene1.mp4"] = []byte("scene-1")
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "board"}, statuses: []string{"running", "idle"},
		history: oldScene, events: newScene, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "cảnh 2")[0]
	p.jobs.update(job.ID, func(j *MuseJob) { j.Thread, j.PinAccount = "board", acc.ID })

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || len(got.Files) != 1 {
		t.Fatalf("job = %+v", got)
	}
	path, _ := p.media.Path(got.Files[0])
	if data, _ := os.ReadFile(path); string(data) != "scene-2" {
		t.Fatalf("took %q, want this scene's video", data)
	}
}

// 2.1: when the capped history no longer lines up, files already in the chat
// are still excluded by path.
func TestThreadJobExcludesSeenFilesWhenHistoryIsCapped(t *testing.T) {
	p := testProvider(t, &fakePage{})
	job := MuseJob{Kind: MuseJobVideo, HistoryBase: 99, SeenPaths: []string{"/old.mp4"}}
	page := &fakePage{events: []AssistantEvent{{Videos: []ResultMedia{{Path: "/old.mp4", Mime: "video/mp4"}}}}, sends: []string{"x"}}
	err := p.collect(context.Background(), job, "acc", page, p.cfg.Get().Muse)
	var muse *museError
	if !errors.As(err, &muse) {
		t.Fatalf("an old file was taken as the result: err = %v", err)
	}
}

// 2.2: reading the dead-proxy set while job goroutines mark proxies must not
// race (the map was read without the lock).
func TestDeadProxySetIsSafeUnderConcurrency(t *testing.T) {
	p := testProvider(t, &fakePage{})
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for k := 0; k < 500; k++ {
				p.markProxyDead("1.2.3.4:80", now)
			}
		}()
		go func() {
			defer wg.Done()
			for k := 0; k < 500; k++ {
				if dead := p.deadProxySet(now); len(dead) > 1 {
					t.Error("unexpected entries")
				}
			}
		}()
	}
	wg.Wait()
	dead := p.deadProxySet(now)
	dead["other"] = now.Add(time.Hour)
	if _, leaked := p.deadProxySet(now)["other"]; leaked {
		t.Fatal("deadProxySet must return a copy")
	}
	if len(p.deadProxySet(now.Add(11*time.Minute))) != 0 {
		t.Fatal("expired marks must be dropped")
	}
}

// 2.3: cancelling while the send is in flight still cancels the generation
// Muse accepted, instead of leaving it running unseen.
func TestCancelDuringSendCancelsOnMuse(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "paid"}, statuses: []string{"running"}}
	page.onSend = func() {
		close(entered)
		<-release
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
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
	if got.Status != MuseJobCancelled || got.SessionID != "paid" {
		t.Fatalf("job = %+v", got)
	}
	if len(page.cancelled) != 1 || page.cancelled[0] != "paid" {
		t.Fatalf("Muse generation not cancelled: %v", page.cancelled)
	}
}

// 2.4: a job Muse already accepted is re-checked past MaxAttempts, and only
// handed back after MaxRechecks with a warning not to pay twice.
func TestSentJobIsRecheckedNotFailedAtAttemptCap(t *testing.T) {
	page := &fakePage{loggedIn: true, statusErr: errors.New("socket closed")}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	cfg := p.cfg.Get().Muse
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.SessionID, j.Attempts = MuseJobProcessing, acc.ID, "s1", cfg.MaxAttempts
	})

	p.runOne(context.Background(), job.ID, acc.ID)
	if got := mustJob(t, p, job.ID); got.Status != MuseJobProcessing || got.Rechecks != 1 {
		t.Fatalf("after one failed read: %+v", got)
	}
	for i := 1; i < cfg.MaxRechecks; i++ {
		p.runOne(context.Background(), job.ID, acc.ID)
	}
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || !strings.Contains(got.Error, "tốn thêm 1 lượt") {
		t.Fatalf("after %d reads: %+v", cfg.MaxRechecks, got)
	}
	if len(page.sends) != 0 {
		t.Fatalf("a sent job was sent again: %d", len(page.sends))
	}
}

func addProduction(t *testing.T, p *Provider, prod Production) Production {
	t.Helper()
	prod.ID = "prod-1"
	if prod.Title == "" {
		prod.Title = "Video"
	}
	if prod.Scenes == nil {
		prod.Scenes = []SceneItem{{Text: "cảnh 1"}, {Text: "cảnh 2"}}
	}
	if prod.Config.Ratio == "" {
		prod.Config.Ratio = "9:16"
	}
	if err := p.productions.add(prod); err != nil {
		t.Fatal(err)
	}
	return prod
}

func mustProduction(t *testing.T, p *Provider, id string) Production {
	t.Helper()
	prod, err := p.productions.get(id)
	if err != nil {
		t.Fatal(err)
	}
	return prod
}

// 2.5: retry is refused while a child job still runs, and a production whose
// storyboard is missing restarts from the storyboard.
func TestRetryProductionWaitsForChildAndRestartsBoard(t *testing.T) {
	p := testProvider(t, &fakePage{})
	child := submit(t, p, MuseJobVideo, "cảnh 1")[0]
	prod := addProduction(t, p, Production{Status: ProductionStatusError, CurrentJobID: child.ID, Config: ScriptConfig{Board: "review"}})

	if res := p.RetryProduction(prod.ID); res.Success {
		t.Fatal("retry accepted while the child job is still queued")
	}
	p.jobs.update(child.ID, func(j *MuseJob) { j.Status = MuseJobFailed })
	if res := p.RetryProduction(prod.ID); !res.Success {
		t.Fatal(res.Error)
	}
	if got := mustProduction(t, p, prod.ID); got.Status != ProductionStatusQueued || got.CurrentJobID != "" {
		t.Fatalf("production = %+v", got)
	}
	if res := p.RetryProduction(prod.ID); res.Success {
		t.Fatal("retry accepted for a production that is not in error")
	}
}

// 2.6: a production that crashed after queuing a child but before recording
// it adopts that child instead of queuing (and paying for) a second one.
func TestProductionAdoptsChildAfterCrash(t *testing.T) {
	p := testProvider(t, &fakePage{})
	prod := addProduction(t, p, Production{Status: ProductionStatusRunning, Config: ScriptConfig{Duration: "each"}})
	orphan := submit(t, p, MuseJobVideo, "cảnh 1")[0]
	p.jobs.update(orphan.ID, func(j *MuseJob) { j.ProductionID, j.ProductionPart = prod.ID, scenePart(0) })

	p.pumpProductions(context.Background())

	if got := mustProduction(t, p, prod.ID); got.CurrentJobID != orphan.ID {
		t.Fatalf("production did not adopt the existing child: %+v", got)
	}
	if n := len(p.jobs.list()); n != 1 {
		t.Fatalf("a second child was queued: %d jobs", n)
	}
}

// 2.6: a child continuing the storyboard chat is written complete in one go.
func TestProductionChildIsWrittenWithThread(t *testing.T) {
	p := testProvider(t, &fakePage{})
	prod := addProduction(t, p, Production{Status: ProductionStatusRunning, ThreadSessionID: "board", ThreadAccountID: "acc",
		BoardFiles: []string{"/flow-media/b.png"}, Config: ScriptConfig{Duration: "each", Board: "auto"}})

	p.pumpProductions(context.Background())

	got := mustProduction(t, p, prod.ID)
	job, ok := p.jobs.get(got.CurrentJobID)
	if !ok || job.Thread != "board" || job.PinAccount != "acc" || job.ProductionID != prod.ID || job.ProductionPart != scenePart(0) {
		t.Fatalf("child = %+v", job)
	}
}

// 2.6/2.8: a cancelled or removed child ends the production with an error
// instead of leaving it waiting forever.
func TestProductionFailsWhenChildIsCancelledOrRemoved(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(p *Provider, id string)
	}{
		{"cancelled", func(p *Provider, id string) { p.CancelJob(id) }},
		{"removed", func(p *Provider, id string) { p.RemoveJob(id) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvider(t, &fakePage{})
			child := submit(t, p, MuseJobVideo, "cảnh 1")[0]
			prod := addProduction(t, p, Production{Status: ProductionStatusRunning, CurrentJobID: child.ID, Config: ScriptConfig{Duration: "each"}})
			tc.change(p, child.ID)

			p.pumpProductions(context.Background())

			if got := mustProduction(t, p, prod.ID); got.Status != ProductionStatusError {
				t.Fatalf("production = %+v", got)
			}
		})
	}
}

// 2.x: a storyboard larger than the per-job image limit users get is allowed.
func TestProductionBoardMayExceedUserImageLimit(t *testing.T) {
	p := testProvider(t, &fakePage{})
	scenes := make([]SceneItem, BoardMax)
	for i := range scenes {
		scenes[i] = SceneItem{Text: "cảnh"}
	}
	prod := addProduction(t, p, Production{Status: ProductionStatusQueued, Scenes: scenes, Config: ScriptConfig{Board: "auto"}})

	p.pumpProductions(context.Background())

	got := mustProduction(t, p, prod.ID)
	job, ok := p.jobs.get(got.BoardJobID)
	if got.Status != ProductionStatusRunning || !ok || job.Count != BoardMax {
		t.Fatalf("production = %+v, board job = %+v", got, job)
	}
}

// 3.1: an unreadable proxy store stops the account instead of sending it out
// on the real IP.
func TestCorruptProxyStoreBlocksAccount(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	if err := os.MkdirAll(filepath.Dir(p.proxies.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.proxies.path, []byte("not a proxy store"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := p.setupProxy(context.Background(), &acc); err == nil {
		t.Fatal("setupProxy went direct with an unreadable proxy store")
	}
	if res := p.GetProxies(); res.Success {
		t.Fatal("GetProxies hid the store error")
	}
}

// 3.2: a running browser is reused only with the same proxy route; a new
// route restarts it so Chrome never keeps the old network.
func TestBrowserRestartsWhenProxyRouteChanges(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.NewManager(filepath.Join(dir, "config.json"))
	pool := newBrowserPool(cfg, filepath.Join(dir, "profiles"))
	var launches [][]string
	closed := 0
	pool.findChrome = func() (string, error) { return "chrome", nil }
	pool.start = func(_ context.Context, opts chrome.LaunchOptions, _ config.AccountConfig) (*chrome.Browser, error) {
		launches = append(launches, opts.ExtraArgs)
		return &chrome.Browser{ProfileDir: opts.ProfileDir}, nil
	}
	pool.validate = func(context.Context, *chrome.Browser, config.AccountConfig) error { return nil }
	pool.adopted = func(*chrome.Browser) bool { return false }
	pool.shutdown = func(*chrome.Browser, config.AccountConfig) error { closed++; return nil }
	profile := filepath.Join(dir, "profiles", "acc")

	mustGet := func(route string, args ...string) {
		t.Helper()
		if _, err := pool.get(context.Background(), profile, false, route, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGet(directRoute)
	mustGet(directRoute)
	if len(launches) != 1 {
		t.Fatalf("same route relaunched: %d launches", len(launches))
	}
	mustGet("1.2.3.4:80", "--proxy-server=http://127.0.0.1:5000")
	if len(launches) != 2 || closed != 1 || launches[1][0] != "--proxy-server=http://127.0.0.1:5000" {
		t.Fatalf("launches = %v, closed = %d", launches, closed)
	}
}

// 3.2: a browser adopted from an earlier run has unknown flags and is restarted.
func TestAdoptedBrowserIsRestarted(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.NewManager(filepath.Join(dir, "config.json"))
	pool := newBrowserPool(cfg, filepath.Join(dir, "profiles"))
	starts := 0
	pool.findChrome = func() (string, error) { return "chrome", nil }
	pool.start = func(context.Context, chrome.LaunchOptions, config.AccountConfig) (*chrome.Browser, error) {
		starts++
		return &chrome.Browser{}, nil
	}
	pool.adopted = func(*chrome.Browser) bool { return starts == 1 }
	pool.shutdown = func(*chrome.Browser, config.AccountConfig) error { return nil }
	if _, err := pool.get(context.Background(), filepath.Join(dir, "profiles", "acc"), false, directRoute); err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("adopted browser kept: %d starts", starts)
	}
}

// 3.3: every account browser blocks WebRTC from bypassing the proxy.
func TestDirectRouteBlocksWebRTCLeak(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	route, err := p.setupProxy(context.Background(), &acc)
	if err != nil {
		t.Fatal(err)
	}
	if route.key != directRoute || !strings.Contains(strings.Join(route.args, " "), "disable_non_proxied_udp") {
		t.Fatalf("route = %+v", route)
	}
}

// 3.4: an account that used the proxy list is not sent out directly once the
// list is emptied; only "none" means direct.
func TestEmptiedProxyListDoesNotFallBackToRealIP(t *testing.T) {
	acc := storedAccount{MuseAccount: MuseAccount{ID: "a", Name: "A", ProxyAuto: "1.2.3.4:80"}}
	if p, _, err := pickProxy(acc, nil, "", nil, time.Now()); err == nil || p != nil {
		t.Fatalf("got proxy %v, err %v; want an error", p, err)
	}
	acc.ProxyAuto, acc.Proxy = "", "none"
	if p, _, err := pickProxy(acc, nil, "", nil, time.Now()); err != nil || p != nil {
		t.Fatalf("\"none\" must go direct: %v %v", p, err)
	}
}

// 3.5: provider errors and bodies never carry the key, and a bad explicit
// proxy line is reported without its password.
func TestProxySecretsAreRedacted(t *testing.T) {
	msg := transportMessage(&url.Error{Op: "Get", URL: "https://proxyxoay.shop/api/get.php?key=SECRET", Err: errors.New("i/o timeout")})
	if strings.Contains(msg, "SECRET") {
		t.Fatalf("key leaked: %s", msg)
	}
	if got := redactSecret("bad key SECRET "+strings.Repeat("x", 500), "SECRET"); strings.Contains(got, "SECRET") || len([]rune(got)) > maxProviderMessage+1 {
		t.Fatalf("redactSecret = %q", got)
	}
	if got := maskProxyLine("1.2.3.4:80:user:hunter2"); strings.Contains(got, "hunter2") || strings.Contains(got, "user") {
		t.Fatalf("maskProxyLine = %q", got)
	}
	acc := storedAccount{MuseAccount: MuseAccount{Name: "A", Proxy: "1.2.3.4:notaport:user:hunter2"}}
	if _, _, err := pickProxy(acc, nil, "", nil, time.Now()); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v", err)
	}
}
