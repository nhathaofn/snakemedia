package muse

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/proxy"
)

type quotaHookPage struct {
	*fakePage
	quotaCalls atomic.Int32
	onQuota    func()
}

func (q *quotaHookPage) Quota(ctx context.Context) (*MuseQuota, error) {
	q.quotaCalls.Add(1)
	if q.onQuota != nil {
		q.onQuota()
	}
	return q.fakePage.Quota(ctx)
}

func TestCheckStaleQuotaTopupAndConcurrentDedupe(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	t.Run("skips refresh while topup quota check is fresh", func(t *testing.T) {
		page := &quotaHookPage{fakePage: &fakePage{loggedIn: true, quota: &MuseQuota{PercentUsed: 100, TopupBalance: 10}}}
		p := testProvider(t, page)
		p.now = func() time.Time { return now }
		cfg := p.cfg.Get()
		cfg.Muse.UseTopup = true
		if _, err := p.cfg.Update(cfg); err != nil {
			t.Fatal(err)
		}
		acc := addSignedIn(t, p, "A")
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) {
			d.Quota = &MuseQuota{
				PercentUsed:  100,
				TopupBalance: 10,
				CheckedAt:    now.Add(-5 * time.Minute).Format(time.RFC3339),
			}
		})

		if err := p.checkStaleQuota(context.Background(), acc.ID, page, p.cfg.Get().Muse); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := page.quotaCalls.Load(); got != 0 {
			t.Fatalf("quota calls = %d, want 0", got)
		}
	})

	t.Run("refreshes topup quota asynchronously after TTL without blocking send", func(t *testing.T) {
		gate := make(chan struct{})
		page := &quotaHookPage{
			fakePage: &fakePage{loggedIn: true, quota: &MuseQuota{PercentUsed: 100, TopupBalance: 8}},
			onQuota:  func() { <-gate },
		}
		p := testProvider(t, page)
		p.now = func() time.Time { return now }
		cfg := p.cfg.Get()
		cfg.Muse.UseTopup = true
		if _, err := p.cfg.Update(cfg); err != nil {
			t.Fatal(err)
		}
		acc := addSignedIn(t, p, "A")
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) {
			d.Quota = &MuseQuota{
				PercentUsed:  100,
				TopupBalance: 10,
				CheckedAt:    now.Add(-20 * time.Minute).Format(time.RFC3339),
			}
		})

		// Second call within cooldown must not start a duplicate background refresh.
		if err := p.checkStaleQuota(context.Background(), acc.ID, page, p.cfg.Get().Muse); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := p.checkStaleQuota(context.Background(), acc.ID, page, p.cfg.Get().Muse); err != nil {
			t.Fatalf("unexpected error on second check: %v", err)
		}
		close(gate)
		p.wg.Wait()
		if got := page.quotaCalls.Load(); got != 1 {
			t.Fatalf("quota calls = %d, want 1", got)
		}
	})

	t.Run("skips repeat sync quota refresh during cooldown after failed attempt", func(t *testing.T) {
		page := &quotaHookPage{fakePage: &fakePage{loggedIn: true, quota: nil}}
		p := testProvider(t, page)
		p.now = func() time.Time { return now }
		acc := addSignedIn(t, p, "A")
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) {
			d.Quota = &MuseQuota{
				PercentUsed: 100,
				CheckedAt:   now.Add(-24 * time.Hour).Format(time.RFC3339),
			}
		})

		// First attempt tries Quota once (empty -> fails to update CheckedAt), second skips within cooldown.
		_ = p.checkStaleQuota(context.Background(), acc.ID, page, p.cfg.Get().Muse)
		_ = p.checkStaleQuota(context.Background(), acc.ID, page, p.cfg.Get().Muse)
		if got := page.quotaCalls.Load(); got != 1 {
			t.Fatalf("quota calls after empty/failed attempt = %d, want 1", got)
		}
	})

	t.Run("serializes and deduplicates concurrent stale checks on one account", func(t *testing.T) {
		page := &quotaHookPage{
			fakePage: &fakePage{loggedIn: true, quota: &MuseQuota{PercentUsed: 100}},
			onQuota:  func() { time.Sleep(15 * time.Millisecond) },
		}
		p := testProvider(t, page)
		p.now = func() time.Time { return now }
		acc := addSignedIn(t, p, "A")
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) {
			d.Quota = &MuseQuota{
				PercentUsed: 100,
				CheckedAt:   now.Add(-24 * time.Hour).Format(time.RFC3339),
			}
		})

		const workers = 3
		errs := make([]error, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := range workers {
			go func() {
				defer wg.Done()
				errs[i] = p.checkStaleQuota(context.Background(), acc.ID, page, p.cfg.Get().Muse)
			}()
		}
		wg.Wait()

		if got := page.quotaCalls.Load(); got != 1 {
			t.Fatalf("concurrent quota calls = %d, want 1", got)
		}
		for i, err := range errs {
			if !errors.Is(err, errAccountResting) {
				t.Fatalf("worker %d err = %v, want errAccountResting", i, err)
			}
		}
	})
}

func TestPollDelayBackoffAndPoll(t *testing.T) {
	base := 4 * time.Second
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		minWant time.Duration
		maxWant time.Duration
	}{
		{"initial", 0, 3 * time.Second, 5 * time.Second},
		{"after 1m", time.Minute, 6 * time.Second, 10 * time.Second},
		{"after 3m", 3 * time.Minute, 9 * time.Second, 15 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 50 {
				got := pollDelay(base, tc.elapsed)
				if got < tc.minWant || got > tc.maxWant {
					t.Fatalf("pollDelay(%v, %v) = %v, want [%v, %v]", base, tc.elapsed, got, tc.minWant, tc.maxWant)
				}
			}
		})
	}
	if got := pollDelay(10*time.Millisecond, 0); got < time.Second {
		t.Fatalf("pollDelay small base = %v, want >= 1s floor", got)
	}

	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	status, err := p.poll(context.Background(), MuseJob{Kind: MuseJobVideo, SessionID: "s1"}, page, p.cfg.Get().Muse)
	if err != nil || status != sessionCompleted {
		t.Fatalf("poll = (%q, %v), want (completed, nil)", status, err)
	}
}

func makeLargeNoisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8((x*37 + y*91 + (x^y)*13) & 0xff)
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v ^ 0x55, B: uint8(x + y), A: 255})
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAttachCacheReusesAndInvalidatesOnChange(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	rawPNG := makeLargeNoisyPNG(t, 200, 200)
	ref, err := p.media.Put("ref", "image/png", ".png", rawPNG)
	if err != nil {
		t.Fatal(err)
	}
	caps := &MuseCapabilities{
		MaxFileBytes: 40 << 10,
		MaxFiles:     4,
		ImageTypes: []MuseFileType{
			{Mime: "image/png", Extensions: []string{".png"}},
			{Mime: "image/jpeg", Extensions: []string{".jpg", ".jpeg"}},
		},
	}

	job1 := MuseJob{Kind: MuseJobVideo, Prompt: "first prompt", References: []string{ref}}
	atts1, err := p.attach(job1, caps)
	if err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if len(atts1) != 1 || atts1[0].MimeType != "image/jpeg" {
		t.Fatalf("unexpected first attachments: %+v", atts1)
	}
	decoded, err := base64.StdEncoding.DecodeString(atts1[0].DataBase64)
	if err != nil || int64(len(decoded)) > caps.MaxFileBytes {
		t.Fatalf("compressed size = %d (max %d, err=%v)", len(decoded), caps.MaxFileBytes, err)
	}
	p.attCache.mu.Lock()
	cachedCount := len(p.attCache.items)
	p.attCache.mu.Unlock()
	if cachedCount != 1 {
		t.Fatalf("cache items = %d, want 1", cachedCount)
	}

	// Second job reusing the same reference should reuse the cached JPEG data
	// without growing cache cardinality.
	job2 := MuseJob{Kind: MuseJobVideo, Prompt: "second prompt", References: []string{ref}}
	atts2, err := p.attach(job2, caps)
	if err != nil {
		t.Fatalf("second attach: %v", err)
	}
	if atts2[0].DataBase64 != atts1[0].DataBase64 || atts2[0].Filename != "image_1.jpg" {
		t.Fatalf("cache miss on identical image: got filename=%s sameData=%v", atts2[0].Filename, atts2[0].DataBase64 == atts1[0].DataBase64)
	}

	// Changing the underlying file + timestamp invalidates the cache entry.
	path, err := p.media.Path(ref)
	if err != nil {
		t.Fatal(err)
	}
	otherPNG := makeLargeNoisyPNG(t, 220, 220)
	if err := os.WriteFile(path, otherPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	atts3, err := p.attach(job2, caps)
	if err != nil {
		t.Fatalf("third attach: %v", err)
	}
	if atts3[0].DataBase64 == atts1[0].DataBase64 {
		t.Fatal("stale cache entry returned after file modification")
	}

	// Cache evicts oldest items once maxCachedAttachments is exceeded.
	p.attCache.clear()
	for i := range maxCachedAttachments + 5 {
		p.attCache.put(attachmentCacheKey{path: string(rune('a' + i))}, cachedAttachment{dataBase64: "x"})
	}
	p.attCache.mu.Lock()
	defer p.attCache.mu.Unlock()
	if len(p.attCache.items) != maxCachedAttachments {
		t.Fatalf("bounded cache size = %d, want %d", len(p.attCache.items), maxCachedAttachments)
	}
}

type cleanConcurrencyPage struct {
	*fakePage
	mu        sync.Mutex
	active    int
	maxActive int
}

func (c *cleanConcurrencyPage) Send(ctx context.Context, text string, atts []Attachment) (SendResult, error) {
	c.mu.Lock()
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	c.mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	res, err := c.fakePage.Send(ctx, text, atts)
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	return res, err
}

func TestCleanChatsCapsConcurrencyAndClosesIdle(t *testing.T) {
	page := &cleanConcurrencyPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "clean"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "ok"}},
		},
	}
	p := testProvider(t, page)
	for _, label := range []string{"A", "B", "C", "D"} {
		addSignedIn(t, p, label)
	}

	res := p.CleanChats(context.Background())
	if !res.Success || len(res.Data) != 4 {
		t.Fatalf("result = %+v", res)
	}
	for _, item := range res.Data {
		if item.Error != "" || item.Reply != "ok" {
			t.Fatalf("item = %+v", item)
		}
		if p.cleaning[item.AccountID] {
			t.Fatalf("account %s still marked cleaning", item.AccountID)
		}
	}
	if page.maxActive > cleanConcurrency {
		t.Fatalf("max concurrent CleanChats sends = %d, want <= %d", page.maxActive, cleanConcurrency)
	}
}

type resultCountingPage struct {
	*fakePage
	resultCalls atomic.Int32
}

func (r *resultCountingPage) Result(ctx context.Context, sid string) ([]AssistantEvent, error) {
	r.resultCalls.Add(1)
	return r.fakePage.Result(ctx, sid)
}

func TestPollReusesResultAndFasterEarlyImagePoll(t *testing.T) {
	base := 4 * time.Second
	for range 50 {
		early := pollDelayForKind(MuseJobImage, base, 10*time.Second)
		if early < 1500*time.Millisecond || early > 2500*time.Millisecond {
			t.Fatalf("early image pollDelay = %v, want [1.5s, 2.5s]", early)
		}
		later := pollDelayForKind(MuseJobImage, base, 30*time.Second)
		if later < 3*time.Second || later > 5*time.Second {
			t.Fatalf("after-30s image pollDelay = %v, want [3s, 5s]", later)
		}
	}

	page := &resultCountingPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"unknown_intermediate_state"},
			events: []AssistantEvent{{
				Text:   "done",
				Images: []ResultMedia{{Path: "/a.webp", Mime: "image/webp", ByteLen: 3}},
			}},
			files: map[string][]byte{"/a.webp": []byte("img")},
		},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	jobs := submit(t, p, MuseJobImage, "fast poll")
	p.runOne(context.Background(), jobs[0].ID, acc.ID)
	if got := mustJob(t, p, jobs[0].ID); got.Status != MuseJobSucceeded {
		t.Fatalf("job = %+v", got)
	}
	if got := page.resultCalls.Load(); got != 1 {
		t.Fatalf("Result calls across poll+collect = %d, want 1", got)
	}
}

type parallelImageDownloadPage struct {
	*fakePage
	mu        sync.Mutex
	active    int
	maxActive int
}

func (d *parallelImageDownloadPage) Download(ctx context.Context, path string, sink io.Writer) (int64, error) {
	d.mu.Lock()
	d.active++
	if d.active > d.maxActive {
		d.maxActive = d.active
	}
	d.mu.Unlock()
	time.Sleep(25 * time.Millisecond)
	n, err := d.fakePage.Download(ctx, path, sink)
	d.mu.Lock()
	d.active--
	d.mu.Unlock()
	return n, err
}

func TestCollectDownloadsImagesInParallelPreservingOrder(t *testing.T) {
	files := make(map[string][]byte)
	var medias []ResultMedia
	for i := 1; i <= 4; i++ {
		path := fmt.Sprintf("/frame_%d.webp", i)
		payload := []byte(fmt.Sprintf("payload-%d", i))
		files[path] = payload
		medias = append(medias, ResultMedia{Path: path, Mime: "image/webp", ByteLen: int64(len(payload))})
	}
	page := &parallelImageDownloadPage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "ok", Images: medias}},
			files:    files,
		},
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	jobs := submit(t, p, MuseJobImage, "storyboard frames")
	p.runOne(context.Background(), jobs[0].ID, acc.ID)

	got := mustJob(t, p, jobs[0].ID)
	if got.Status != MuseJobSucceeded || len(got.Files) != 4 {
		t.Fatalf("job = %+v", got)
	}
	if page.maxActive < 2 {
		t.Fatalf("max concurrent image downloads = %d, want >= 2", page.maxActive)
	}
	for i, localURL := range got.Files {
		path, err := p.media.Path(localURL)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("payload-%d", i+1)
		if string(data) != want {
			t.Fatalf("file[%d] = %q, want %q (ordering mismatch)", i, data, want)
		}
	}
}

func TestNotSentRequeuesImmediatelyWhenOtherAccountExists(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	t.Run("hands off immediately to another healthy account without 1m hold", func(t *testing.T) {
		failPage := &fakePage{loggedIn: true, sendRes: SendResult{OK: false, Reason: "not_ready (/home)"}}
		okPage := &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s-ok"},
			statuses: []string{"completed"},
			events:   []AssistantEvent{{Text: "ok", Videos: []ResultMedia{{Path: "/v.mp4", Mime: "video/mp4", ByteLen: 3}}}},
			files:    map[string][]byte{"/v.mp4": []byte("vid")},
		}
		p := testProvider(t, failPage)
		p.now = func() time.Time { return now }
		accA := addSignedIn(t, p, "A")
		accB := addSignedIn(t, p, "B")
		for _, id := range []string{accA.ID, accB.ID} {
			p.updateAccount(id, func(r *accounts.Record, _ *museData) { r.Proxy = proxy.None })
		}
		p.pageFor = func(_ context.Context, rec accounts.Record) (Page, error) {
			if rec.ID == accA.ID {
				return failPage, nil
			}
			return okPage, nil
		}
		jobs := submit(t, p, MuseJobVideo, "prompt 3")
		p.runOne(context.Background(), jobs[0].ID, accA.ID)

		got := mustJob(t, p, jobs[0].ID)
		if got.Status != MuseJobQueued || got.NextCheckAt != "" || got.Attempts != 0 {
			t.Fatalf("job should requeue immediately without NextCheckAt hold, got %+v", got)
		}
		_, backedOff := p.backedOffAccountsSnapshot(now)
		if !backedOff[accA.ID] || backedOff[accB.ID] {
			t.Fatalf("backedOff = %v, want only %s backed off", backedOff, accA.ID)
		}
		// Immediately after, account B can take and finish the job without waiting 1 minute.
		p.runOne(context.Background(), jobs[0].ID, accB.ID)
		got = mustJob(t, p, jobs[0].ID)
		if got.Status != MuseJobSucceeded || got.AccountID != accB.ID {
			t.Fatalf("expected account B to finish immediately, got %+v", got)
		}
	})

	t.Run("sequential notSent across both accounts still avoids 1m job lock", func(t *testing.T) {
		failPage := &fakePage{loggedIn: true, sendRes: SendResult{OK: false, Reason: "not_ready (/home)"}}
		p := testProvider(t, failPage)
		p.now = func() time.Time { return now }
		accA := addSignedIn(t, p, "A")
		accB := addSignedIn(t, p, "B")
		for _, id := range []string{accA.ID, accB.ID} {
			p.updateAccount(id, func(r *accounts.Record, _ *museData) { r.Proxy = proxy.None })
		}
		jobs := submit(t, p, MuseJobVideo, "prompt multi")
		p.runOne(context.Background(), jobs[0].ID, accA.ID)
		p.runOne(context.Background(), jobs[0].ID, accB.ID)

		got := mustJob(t, p, jobs[0].ID)
		if got.Status != MuseJobQueued || got.NextCheckAt != "" {
			t.Fatalf("2-account queue should never freeze job with NextCheckAt, got %+v", got)
		}
	})

	t.Run("errAccountResting requeues immediately without 1m hold", func(t *testing.T) {
		page := &fakePage{loggedIn: true, quota: &MuseQuota{PercentUsed: 100}}
		p := testProvider(t, page)
		p.now = func() time.Time { return now }
		acc := addSignedIn(t, p, "A")
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) {
			d.Quota = &MuseQuota{PercentUsed: 100}
		})
		jobs := submit(t, p, MuseJobVideo, "resting prompt")
		p.runOne(context.Background(), jobs[0].ID, acc.ID)

		got := mustJob(t, p, jobs[0].ID)
		if got.Status != MuseJobQueued || got.NextCheckAt != "" {
			t.Fatalf("resting error must not put 1m NextCheckAt hold on job, got %+v", got)
		}
	})
}

func TestKeepWarmChromeAndSkipVerifiedLogin(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	p := testProvider(t, &fakePage{loggedIn: true})
	p.now = func() time.Time { return now }
	acc := addSignedIn(t, p, "A")
	p.updateAccount(acc.ID, func(r *accounts.Record, _ *museData) { r.Proxy = proxy.None })
	p.mu.Lock()
	p.ctx = context.Background()
	p.mu.Unlock()
	jobs := submit(t, p, MuseJobImage, "warm chrome")

	// Unreachable port 59999 proves ensureSignedIn skips CDP Login calls when port is already verified.
	p.markSessionVerified(acc.ID, 59999)
	reused := &chromePage{port: 59999}
	if err := p.ensureSignedIn(context.Background(), jobs[0].ID, acc, reused); err != nil {
		t.Fatalf("expected verified Chrome port to skip login check, got err: %v", err)
	}

	// Keep warm when usable -> sessionVerified stays intact after closeIdleBrowserIfDone.
	p.closeIdleBrowserIfDone(acc.ID)
	if !p.sessionVerified(acc.ID, 59999) {
		t.Fatal("expected healthy usable account to keep warm verified session")
	}

	// New Chrome port (e.g. restarted browser) must re-check login and clear old port cache on failure.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	restarted := &chromePage{port: 59998}
	if err := p.ensureSignedIn(ctx, jobs[0].ID, acc, restarted); err == nil {
		t.Fatal("expected restarted Chrome port to verify login via CDP instead of skipping")
	}
	if p.sessionVerified(acc.ID, 59999) {
		t.Fatal("expected failed ensureSignedIn to clear sessionVerified")
	}

	// Signed-out account clears sessionVerified in closeIdleBrowserIfDone (closing headless browser).
	p.markSessionVerified(acc.ID, 59999)
	p.updateAccount(acc.ID, func(r *accounts.Record, _ *museData) {
		r.Status = accounts.StatusSignedOut
	})
	p.closeIdleBrowserIfDone(acc.ID)
	if p.sessionVerified(acc.ID, 59999) {
		t.Fatal("expected signed-out account to clear verified session and close browser")
	}
}

func TestQueuedWaitingStepShowsClearReason(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	p := testProvider(t, &fakePage{loggedIn: true})
	p.now = func() time.Time { return now }
	cfg := p.cfg.Get().Muse

	job := MuseJob{ID: "j1", Kind: MuseJobVideo, Status: MuseJobQueued}
	accs := []MuseAccount{{ID: "a1", Enabled: true, Status: accounts.StatusSignedIn}}

	if step := queuedWaitingStep(job, accs, map[string]bool{"other": true}, nil, nil, cfg, now); step != "Chờ tài khoản chạy xong" {
		t.Fatalf("step while other running = %q", step)
	}
	if step := queuedWaitingStep(job, accs, nil, nil, map[string]bool{"a1": true}, cfg, now); step != "Chờ tài khoản sẵn sàng lại" {
		t.Fatalf("step while backed off = %q", step)
	}
	if step := queuedWaitingStep(job, nil, nil, nil, nil, cfg, now); step != "Chờ tài khoản Muse khả dụng" {
		t.Fatalf("step with no accounts = %q", step)
	}
	heldJob := job
	heldJob.NextCheckAt = now.Add(20 * time.Second).UTC().Format(time.RFC3339Nano)
	if step := queuedWaitingStep(heldJob, accs, nil, nil, nil, cfg, now); step == "" {
		t.Fatal("expected retry timestamp hint when NextCheckAt is set")
	}
}

type loginCountingPage struct {
	*fakePage
	logins atomic.Int32
	state  LoginState
}

func (l *loginCountingPage) Login(ctx context.Context) (LoginState, error) {
	l.logins.Add(1)
	if l.state.HTTP != 0 || l.state.Path != "" {
		return l.state, nil
	}
	return l.fakePage.Login(ctx)
}

func TestWaitSignedInExitsFastOnSignedOutOrLoginPath(t *testing.T) {
	t.Run("HTTP 401 exits after brief retry instead of looping full timeout", func(t *testing.T) {
		page := &loginCountingPage{fakePage: &fakePage{loggedIn: false}}
		p := testProvider(t, page)
		err := p.waitSignedIn(context.Background(), page, 60*time.Second)
		if !errors.Is(err, errNotSignedIn) {
			t.Fatalf("err = %v, want errNotSignedIn", err)
		}
		if got := page.logins.Load(); got != 2 {
			t.Fatalf("Login polls = %d, want 2 (no 60s polling loop)", got)
		}
	})

	t.Run("login path with HTTP 200 and no chat exits quickly as signed out", func(t *testing.T) {
		page := &loginCountingPage{
			fakePage: &fakePage{loggedIn: true},
			state:    LoginState{LoggedIn: false, Chat: false, HTTP: 200, Path: "/login"},
		}
		p := testProvider(t, page)
		err := p.waitSignedIn(context.Background(), page, 60*time.Second)
		if !errors.Is(err, errNotSignedIn) {
			t.Fatalf("err = %v, want errNotSignedIn", err)
		}
		if got := page.logins.Load(); got > 3 {
			t.Fatalf("Login polls = %d, want <= 3", got)
		}
	})
}

func TestSnapshotSessionsDegradesDirectlyToSend(t *testing.T) {
	events, files := videoResult("/out/ok.mp4", []byte("vid"))
	broken := &trackedFakePage{
		fakePage: &fakePage{
			loggedIn: true,
			sendRes:  SendResult{OK: true, SessionID: "s1"},
			statuses: []string{"completed"},
			events:   events,
			files:    files,
		},
		sessionsErr: errors.New("gateway timeout"),
	}
	p := testProvider(t, broken)
	acc := addSignedIn(t, p, "A")
	p.updateAccount(acc.ID, func(r *accounts.Record, _ *museData) { r.Proxy = proxy.None })
	jobs := submit(t, p, MuseJobVideo, "direct send even when sessions.list fails")

	p.runOne(context.Background(), jobs[0].ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, jobs[0].ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s1" || len(broken.sends) != 1 {
		t.Fatalf("expected direct send to succeed when pre-send snapshot degrades, got %+v sends=%d", got, len(broken.sends))
	}
}

func TestRecycleBrowserClearsVerifiedOnPreSendFailure(t *testing.T) {
	failPage := &fakePage{loggedIn: true, sendRes: SendResult{OK: false, Reason: "not_ready (/home)"}}
	p := testProvider(t, failPage)
	p.mu.Lock()
	p.ctx = context.Background()
	p.mu.Unlock()
	acc := addSignedIn(t, p, "A")
	p.updateAccount(acc.ID, func(r *accounts.Record, _ *museData) { r.Proxy = proxy.None })
	p.markSessionVerified(acc.ID, 60259)
	jobs := submit(t, p, MuseJobVideo, "stuck chrome")

	p.mu.Lock()
	p.running[jobs[0].ID] = acc.ID
	p.mu.Unlock()
	p.runOne(context.Background(), jobs[0].ID, acc.ID)
	if p.sessionVerified(acc.ID, 60259) {
		t.Fatal("expected finishRun to clear verified session on broken Chrome recycle")
	}
	if p.takeRecycleBrowser(acc.ID) {
		t.Fatal("expected finishRun to consume recycle marker")
	}

	p.markSessionVerified(acc.ID, 60259)
	p.markRecycleBrowser(acc.ID)
	p.closeIdleBrowserIfDone(acc.ID)
	if p.sessionVerified(acc.ID, 60259) {
		t.Fatal("expected closeIdleBrowserIfDone to clear verified session when marked for recycle")
	}
}
