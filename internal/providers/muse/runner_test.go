package muse

import (
	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunJobSendsPollsAndDownloads(t *testing.T) {
	events, files := videoResult("/out/a.mp4", []byte("video-bytes"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con mèo")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s1" || got.Attempts != 1 || len(got.Files) != 1 {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != 1 || page.sends[0] != "Task: Create a new video.\nPrompt:\nmột con mèo" {
		t.Fatalf("sent %q", page.sends)
	}
	path, err := p.media.Path(got.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "video-bytes" {
		t.Fatalf("downloaded %q", data)
	}
	if a, _ := p.accounts.Record(acc.ID); museView(a).LastSentAt == "" {
		t.Fatal("last send time not recorded")
	}
}

func TestResumedJobIsNeverSentAgain(t *testing.T) {
	events, files := videoResult("/out/b.mp4", []byte("v"))
	page := &fakePage{loggedIn: true, statuses: []string{"completed"}, history: events, files: files} // Muse finished while the app was closed
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.SessionID, j.Attempts = MuseJobProcessing, acc.ID, "old-session", 1
	})

	p.runOne(context.Background(), job.ID, acc.ID)

	if len(page.sends) != 0 {
		t.Fatalf("resumed job was sent again: %q", page.sends)
	}
	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || got.SessionID != "old-session" {
		t.Fatalf("job = %+v", got)
	}
}

func TestTransportErrorAfterSendKeepsSession(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running"}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	page.statusErr = errors.New("socket closed")

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	// A sent job is re-checked (Rechecks), never counted as a new attempt.
	if got.Status != MuseJobProcessing || got.SessionID != "s1" || got.Attempts != 1 || got.Rechecks != 1 {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != 1 {
		t.Fatalf("sends = %d", len(page.sends))
	}
}

func TestUnknownSendOutcomeFailsWithoutRetry(t *testing.T) {
	page := &fakePage{loggedIn: true, sendErr: errors.New("websocket: close 1006")}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || got.Error != errSendUnknown.Error() || got.Sending {
		t.Fatalf("job = %+v", got)
	}
}

// A run Muse completed without the file, on an account with usage left, is
// Muse declining the prompt, whatever its reply says.
func TestPolicyReplyFailsAtOnce(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Text: "Mình không thể hỗ trợ yêu cầu này vì vi phạm chính sách."}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobFailed || !strings.HasPrefix(got.Error, "CẦN ĐỔI PROMPT") {
		t.Fatalf("job = %+v", got)
	}
}

func TestUsageReplyRequeuesAndRestsAccount(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Text: "Bạn đã dùng hết lượt."}}, quota: &MuseQuota{PercentUsed: 100}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || got.SessionID != "" {
		t.Fatalf("job = %+v", got)
	}
	a, _ := p.accounts.Record(acc.ID)
	if a.PausedUntil == "" || usable(museView(a), p.cfg.Get().Muse, p.now()) {
		t.Fatalf("account not rested: %+v", a.Account)
	}
}

func TestInterruptedRunRetriesInNewChatUntilAttemptsRunOut(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "interrupted"},
		events: []AssistantEvent{{Text: "Đã có lỗi kỹ thuật."}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	if got := mustJob(t, p, job.ID); got.Status != MuseJobQueued || got.SessionID != "" || got.Attempts != 1 {
		t.Fatalf("after first try: %+v", got)
	}
	page.statuses = []string{"running", "interrupted"}
	p.runOne(context.Background(), job.ID, acc.ID)
	if got := mustJob(t, p, job.ID); got.Status != MuseJobFailed || got.Attempts != 2 {
		t.Fatalf("after second try: %+v", got)
	}
	if len(page.sends) != 2 {
		t.Fatalf("sends = %d", len(page.sends))
	}
}

func TestSignedOutAccountReturnsJobWithoutSpendingAttempt(t *testing.T) {
	page := &fakePage{loggedIn: false}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobQueued || got.Attempts != 0 || len(page.sends) != 0 {
		t.Fatalf("job = %+v sends=%d", got, len(page.sends))
	}
	if a, _ := p.accounts.Record(acc.ID); a.Status != accounts.StatusSignedOut {
		t.Fatalf("account status = %s", a.Status)
	}
}

func TestImageJobCollectsEveryImage(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Images: []ResultMedia{
			{Path: "/1.png", Mime: "image/png", ByteLen: 1}, {Path: "/2.png", Mime: "image/png", ByteLen: 1},
		}, Videos: []ResultMedia{{Path: "/x.mp4", Mime: "video/mp4", ByteLen: 1}}}},
		files: map[string][]byte{"/1.png": {1}, "/2.png": {2}, "/x.mp4": {3}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobImage, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(got.Files) != 2 {
		t.Fatalf("job = %+v", got)
	}
	if page.sends[0] != "Task: Create a new image.\nPrompt:\nx" {
		t.Fatalf("sent %q", page.sends[0])
	}
}

func TestDownloadRejectsWrongSize(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Videos: []ResultMedia{{Path: "/v.mp4", Mime: "video/mp4", ByteLen: 10}}}},
		files:  map[string][]byte{"/v.mp4": []byte("short")}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status == MuseJobSucceeded {
		t.Fatalf("a truncated file was accepted: %+v", got)
	}
}

// Reference images are checked against the limits Muse's composer has for
// the account, read from the page when the account has none stored.
func TestAttachmentsFollowMuseLimits(t *testing.T) {
	page := &fakePage{loggedIn: true, caps: testCaps(), sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	png, err := p.media.Put("ref", "image/png", ".png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	job := submit(t, p, MuseJobVideo, "x")[0]
	p.jobs.update(job.ID, func(j *MuseJob) { j.References = []string{png} })

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded {
		t.Fatalf("job = %+v", got)
	}
	if atts := page.attachments[0]; len(atts) != 1 || atts[0].MimeType != "image/png" || atts[0].Filename != "image_1.png" {
		t.Fatalf("attachments = %+v", atts)
	}
	if a, _ := p.accounts.Record(acc.ID); func() bool {
		var data museData
		_ = accounts.DecodeData(a, &data)
		return data.Capabilities == nil
	}() {
		t.Fatal("capabilities read from the page were not kept")
	}

	big, _ := p.media.Put("big", "image/png", ".png", make([]byte, 2<<20))
	webp, _ := p.media.Put("w", "image/webp", ".webp", []byte("w"))
	for _, refs := range [][]string{{big}, {webp}, {png, png, png}} {
		job := submit(t, p, MuseJobVideo, "x")[0]
		p.jobs.update(job.ID, func(j *MuseJob) { j.References = refs })
		p.runOne(context.Background(), job.ID, acc.ID)
		if got := mustJob(t, p, job.ID); got.Status != MuseJobFailed {
			t.Fatalf("refs %v: job = %+v", refs, got)
		}
	}
}

func TestStartSettlesInterruptedJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	jobs := submit(t, p, MuseJobVideo, "sending", "sent", "picked", "orphan")
	p.jobs.update(jobs[0].ID, func(j *MuseJob) { j.Status, j.Sending, j.Attempts = MuseJobProcessing, true, 1 })
	p.jobs.update(jobs[1].ID, func(j *MuseJob) {
		j.Status, j.SessionID, j.AccountID, j.Attempts = MuseJobProcessing, "s", acc.ID, 1
	})
	p.jobs.update(jobs[2].ID, func(j *MuseJob) { j.Status, j.Attempts = MuseJobProcessing, 1 })
	p.jobs.update(jobs[3].ID, func(j *MuseJob) {
		j.Status, j.SessionID, j.AccountID, j.Attempts = MuseJobProcessing, "s2", "gone", 1
	})

	// A fresh provider over the same directory reads the journal like a restart.
	restarted := New(filepath.Dir(p.root), p.defaultOutputDir, p.cfg, p.media, p.accounts)
	restarted.pageFor = p.pageFor
	ctx, cancel := context.WithCancel(context.Background())
	restarted.Start(ctx, nil)
	cancel()
	restarted.Close()

	if got := mustJob(t, restarted, jobs[0].ID); got.Status != MuseJobFailed || got.Error != errSendUnknown.Error() {
		t.Fatalf("sending job = %+v", got)
	}
	if got := mustJob(t, restarted, jobs[1].ID); got.Status != MuseJobProcessing || got.SessionID != "s" {
		t.Fatalf("sent job = %+v", got)
	}
	if got := mustJob(t, restarted, jobs[2].ID); got.Status != MuseJobQueued || got.Attempts != 0 {
		t.Fatalf("picked job = %+v", got)
	}
	// Only the sending account can read a chat: a sent job whose account is
	// gone fails instead of being sent again.
	if got := mustJob(t, restarted, jobs[3].ID); got.Status != MuseJobFailed || got.Error != errAccountGone {
		t.Fatalf("orphaned job = %+v", got)
	}
}

func TestSubmitValidates(t *testing.T) {
	p := testProvider(t, &fakePage{})
	cases := []MuseJobRequest{
		{Kind: "gif", Prompts: []string{"x"}},
		{Kind: "v2v", Prompts: []string{"x"}},
		{Kind: MuseJobVideo, Prompts: []string{"  ", ""}},
		// References need Muse's limits, which no account has read yet.
		{Kind: MuseJobVideo, Prompts: []string{"x"}, References: []string{"/flow-media/a.png"}},
	}
	for i, req := range cases {
		if res := p.SubmitJobs(req); res.Success {
			t.Errorf("case %d accepted", i)
		}
	}
	jobs := submit(t, p, MuseJobVideo, "a", " ", "b")
	if len(jobs) != 2 || jobs[0].CreatedAt >= jobs[1].CreatedAt {
		t.Fatalf("batch = %+v", jobs)
	}
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"a", "b"}, Repeat: 3})
	if !res.Success || len(res.Data) != 6 || res.Data[2].Prompt != "a" || res.Data[3].Prompt != "b" {
		t.Fatalf("repeat batch = %+v", res)
	}
	for i := 1; i < len(res.Data); i++ {
		if res.Data[i-1].CreatedAt >= res.Data[i].CreatedAt {
			t.Fatalf("repeat batch out of order: %+v", res.Data)
		}
	}
}

// An account without a proxy route takes no new job and checks no sent one.
func TestNoRouteAccountTakesNoWork(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	now := time.Now()
	acc := MuseAccount{ID: "a", Enabled: true, Status: accounts.StatusSignedIn, NoRoute: "no proxy"}
	if usable(acc, cfg, now) {
		t.Fatal("account without a route is usable")
	}
	jobs := []MuseJob{
		{ID: "new", Status: MuseJobQueued},
		{ID: "sent", Status: MuseJobProcessing, AccountID: "a", SessionID: "s"},
	}
	if got := plan(jobs, []MuseAccount{acc}, cfg, map[string]string{}, now); len(got) != 0 {
		t.Fatalf("planned %+v", got)
	}
	if step := waitingStep(jobs[1], []MuseAccount{acc}); !strings.Contains(step, "proxy") {
		t.Fatalf("waiting step %q", step)
	}
}
