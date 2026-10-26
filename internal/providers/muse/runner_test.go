package muse

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunJobSendsPollsAndDownloads(t *testing.T) {
	events, files := videoResult("/out/a.mp4", []byte("video-bytes"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "một con mèo")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SessionID != "s1" || got.Attempts != 1 || len(got.Files) != 1 {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != 1 || !strings.HasPrefix(page.sends[0], "Tạo video · tỷ lệ 9:16 dọc\n") || !strings.HasSuffix(page.sends[0], "một con mèo") {
		t.Fatalf("sent %q", page.sends)
	}
	path, err := p.media.Path(got.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "video-bytes" {
		t.Fatalf("downloaded %q", data)
	}
	if a, _ := p.accounts.get(acc.ID); a.LastSentAt == "" {
		t.Fatal("last send time not recorded")
	}
}

func TestResumedJobIsNeverSentAgain(t *testing.T) {
	events, files := videoResult("/out/b.mp4", []byte("v"))
	page := &fakePage{loggedIn: true, statuses: []string{"idle"}, history: events, files: files} // Muse finished while the app was closed
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

func TestPolicyReplyFailsAtOnce(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"},
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
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"},
		events: []AssistantEvent{{Text: "You have reached your weekly limit."}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || got.SessionID != "" {
		t.Fatalf("job = %+v", got)
	}
	a, _ := p.accounts.get(acc.ID)
	if a.PausedUntil == "" || usable(a.MuseAccount, p.cfg.Get().Muse, p.now()) {
		t.Fatalf("account not rested: %+v", a.MuseAccount)
	}
}

func TestNoFileRetriesInNewChatUntilAttemptsRunOut(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"},
		events: []AssistantEvent{{Text: "Đã có lỗi kỹ thuật."}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	if got := mustJob(t, p, job.ID); got.Status != MuseJobQueued || got.SessionID != "" || got.Attempts != 1 {
		t.Fatalf("after first try: %+v", got)
	}
	page.statuses = []string{"running", "idle"}
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
	if a, _ := p.accounts.get(acc.ID); a.Status != MuseAccountSignedOut {
		t.Fatalf("account status = %s", a.Status)
	}
}

func TestImageJobKeepsRequestedCount(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"},
		events: []AssistantEvent{{Images: []ResultMedia{
			{Path: "/1.png", Mime: "image/png", ByteLen: 1}, {Path: "/2.png", Mime: "image/png", ByteLen: 1}, {Path: "/3.png", Mime: "image/png", ByteLen: 1},
		}}},
		files: map[string][]byte{"/1.png": {1}, "/2.png": {2}, "/3.png": {3}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"x"}, Ratio: "1:1", Count: 2})
	if !res.Success {
		t.Fatal(res.Error)
	}

	p.runOne(context.Background(), res.Data[0].ID, acc.ID)

	if got := mustJob(t, p, res.Data[0].ID); got.Status != MuseJobSucceeded || len(got.Files) != 2 {
		t.Fatalf("job = %+v", got)
	}
	if !strings.HasPrefix(page.sends[0], "Tạo ảnh · 2 ảnh · tỷ lệ 1:1 vuông\n") {
		t.Fatalf("sent %q", page.sends[0])
	}
}

func TestStartSettlesInterruptedJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobVideo, "sending", "sent", "picked")
	p.jobs.update(jobs[0].ID, func(j *MuseJob) { j.Status, j.Sending, j.Attempts = MuseJobProcessing, true, 1 })
	p.jobs.update(jobs[1].ID, func(j *MuseJob) { j.Status, j.SessionID, j.Attempts = MuseJobProcessing, "s", 1 })
	p.jobs.update(jobs[2].ID, func(j *MuseJob) { j.Status, j.Attempts = MuseJobProcessing, 1 })

	// A fresh provider over the same directory reads the journal like a restart.
	restarted := New(filepath.Dir(filepath.Dir(p.accounts.path)), p.cfg, p.media)
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
}

func TestSubmitValidates(t *testing.T) {
	p := testProvider(t, &fakePage{})
	cases := []MuseJobRequest{
		{Kind: "gif", Prompts: []string{"x"}, Ratio: "9:16"},
		{Kind: MuseJobVideo, Prompts: []string{"x"}, Ratio: "2:1"},
		{Kind: MuseJobVideo, Prompts: []string{"  ", ""}, Ratio: "9:16"},
		{Kind: MuseJobImage, Prompts: []string{"x"}, Ratio: "9:16", Count: 9},
		{Kind: MuseJobVideo, Prompts: []string{"x"}, Ratio: "9:16", References: []string{"C:/secret.png"}},
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
}

func TestRunJobSendsIntoThread(t *testing.T) {
	events, files := videoResult("/out/thread.mp4", []byte("thread-video"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "idle"}, events: events, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "cảnh storyboard")[0]
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Thread = "storyboard-sess-1"
		j.PinAccount = acc.ID
	})

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded {
		t.Fatalf("job status = %s, err = %s", got.Status, got.Error)
	}
	if len(page.threads) != 1 || page.threads[0] != "storyboard-sess-1" {
		t.Fatalf("expected thread storyboard-sess-1, got %v", page.threads)
	}
}

func TestThreadRefusedSwitchesToNewChat(t *testing.T) {
	events, files := videoResult("/out/new.mp4", []byte("new-chat-video"))
	page := &fakePage{
		loggedIn:    true,
		threadFails: true,
		sendRes:     SendResult{OK: true, SessionID: "s2"},
		statuses:    []string{"running", "idle"},
		events:      events,
		files:       files,
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "cảnh thử lại")[0]
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Thread = "thread-die"
	})

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded {
		t.Fatalf("job status = %s, err = %s", got.Status, got.Error)
	}
	if len(page.threads) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(page.threads))
	}
	if page.threads[0] != "thread-die" || page.threads[1] != "" {
		t.Fatalf("unexpected threads: %v", page.threads)
	}
	if !strings.Contains(got.ThreadError, "làm ở chat mới") {
		t.Fatalf("threadError = %q", got.ThreadError)
	}
}

func TestRunJobV2VTwoTurns(t *testing.T) {
	events, files := videoResult("/out/v2v.mp4", []byte("video-v2v-rendered"))
	page := &fakePage{
		loggedIn:   true,
		sendRes:    SendResult{OK: true, SessionID: "v2v-sid"},
		statuses:   []string{"running", "idle", "running", "idle"},
		events:     events,
		files:      files,
		replyAfter: 2, // the remake's video appears after the second turn
	}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")

	srcURL, err := p.media.Put("test-src", "video/mp4", ".mp4", []byte("source-video-data"))
	if err != nil {
		t.Fatal(err)
	}
	refURL, err := p.media.Put("test-ref", "image/png", ".png", []byte("ref-image-data"))
	if err != nil {
		t.Fatal(err)
	}

	p.muxAudio = func(ctx context.Context, ffmpegPath, videoPath, sourcePath, outPath string) error {
		return os.WriteFile(outPath, []byte("muxed-video-with-source-audio"), 0o644)
	}

	res := p.SubmitJobs(MuseJobRequest{
		Kind:        MuseJobV2V,
		Prompts:     []string{"zoom t-shirt"},
		Ratio:       "9:16",
		SourceVideo: srcURL,
		KeepAudio:   true,
		References:  []string{refURL},
	})
	if !res.Success || len(res.Data) != 1 {
		t.Fatalf("submit failed: %v", res.Error)
	}
	jobID := res.Data[0].ID

	p.runOne(context.Background(), jobID, acc.ID)

	got := mustJob(t, p, jobID)
	if got.Status != MuseJobSucceeded {
		t.Fatalf("job status = %s, err = %s", got.Status, got.Error)
	}
	if got.V2VTurn != 2 {
		t.Fatalf("v2vTurn = %d, expected 2", got.V2VTurn)
	}
	if len(page.sends) != 2 {
		t.Fatalf("expected 2 sends for V2V, got %d", len(page.sends))
	}
	if page.sends[0] != v2vAnalysisPrompt {
		t.Fatalf("turn 1 prompt = %q", page.sends[0])
	}
	if len(page.attachments[0]) != 1 || page.attachments[0][0].Category != "video" {
		t.Fatalf("turn 1 attachments = %+v", page.attachments[0])
	}
	if !strings.Contains(page.sends[1], v2vRemakePrefix) {
		t.Fatalf("turn 2 prompt = %q", page.sends[1])
	}
	if page.threads[1] != "v2v-sid" {
		t.Fatalf("turn 2 thread = %q, expected v2v-sid", page.threads[1])
	}
	if len(got.Files) != 1 {
		t.Fatalf("files = %v", got.Files)
	}
	finalPath, err := p.media.Path(got.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(finalPath)
	if string(data) != "muxed-video-with-source-audio" {
		t.Fatalf("expected muxed audio, got %q", string(data))
	}
}

func TestRestartDuringV2VSecondTurnSendingFails(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobVideo, "v2v-job")
	p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Kind = MuseJobV2V
		j.Status = MuseJobProcessing
		j.SessionID = "v2v-turn1-session"
		j.Sending = true
		j.V2VTurn = 1
		j.Attempts = 1
	})

	restarted := New(filepath.Dir(filepath.Dir(p.accounts.path)), p.cfg, p.media)
	restarted.pageFor = p.pageFor
	ctx, cancel := context.WithCancel(context.Background())
	restarted.Start(ctx, nil)
	cancel()
	restarted.Close()

	got := mustJob(t, restarted, jobs[0].ID)
	if got.Status != MuseJobFailed || got.Error != errSendUnknown.Error() {
		t.Fatalf("expected job with Sending=true to fail even with SessionID, got: %+v", got)
	}
}
