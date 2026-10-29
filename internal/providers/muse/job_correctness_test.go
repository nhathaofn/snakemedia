package muse

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
)

// Muse's own sequence for a finished job (read from muse.ai): the session is
// running, then completed; the chat holds a short reply, the file as its own
// presentation event, and often a follow-up question. The question is not a
// failure: the file is there.
func TestCompletedWithFileAndQuestionSucceeds(t *testing.T) {
	_, files := videoResult("/out/a.mp4", []byte("video"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "running", "completed"},
		events: []AssistantEvent{
			{Text: "Đây là video của bạn:"},
			{Text: "video", Videos: []ResultMedia{{Path: "/out/a.mp4", Mime: "video/mp4", ByteLen: 5}}},
			{Text: "Muốn mình làm thêm bản chuyển động chậm hơn không?"},
		}, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(got.Files) != 1 {
		t.Fatalf("job = %+v", got)
	}
}

// Muse may send a video and then a corrected cut of it (seen on muse.ai:
// "here's the exact-cut version, verified at 5.000 seconds, replacing the
// 5.08s cut I just sent"). The job asked for one video: only the last one is
// kept.
func TestReplacedVideoKeepsOnlyTheLastCut(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{
			{Text: "Done — 1 clip."},
			{Text: "5 seconds", Videos: []ResultMedia{{Path: "/out/new.mp4", Mime: "video/mp4", ByteLen: 5}}},
			{Text: "Quick correction — here's the exact-cut version, replacing the 5.08s cut I just sent."},
			{Text: "exactly 5.000 seconds", Videos: []ResultMedia{{Path: "/out/final.mp4", Mime: "video/mp4", ByteLen: 5}}},
		}, files: map[string][]byte{"/out/new.mp4": []byte("first"), "/out/final.mp4": []byte("final")}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || len(got.Files) != 1 {
		t.Fatalf("job = %+v", got)
	}
	path, err := p.media.Path(got.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "final" {
		t.Fatalf("kept %q %v, want the corrected cut", data, err)
	}
}

// A session not listed yet, or in a state Muse was never seen to use, keeps
// the job waiting: only "completed" or "interrupted" end it.
func TestOnlyMuseEndStatesEndTheJob(t *testing.T) {
	result, files := videoResult("/out/a.mp4", []byte("video"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"},
		statuses: []string{"", "queued", "running", "completed"}, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	page.onSend = func() {
		page.mu.Lock()
		page.events = nil // nothing in the chat until the run has finished
		page.mu.Unlock()
	}
	reads := 0
	p.sleep = func(ctx context.Context, d time.Duration) error {
		reads++
		if reads == 4 { // the poll that reads "completed"
			page.mu.Lock()
			page.events = result
			page.mu.Unlock()
		}
		return ctx.Err()
	}

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(page.sends) != 1 {
		t.Fatalf("job = %+v, sends = %d", got, len(page.sends))
	}
}

// Muse declining a prompt (read from muse.ai) completes the run with words and
// suggestions but no file: with usage left on the account, that is a prompt
// to change, never sent again.
func TestCompletedWithoutFileIsPolicy(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Text: "Mình không tạo được ảnh chân dung chân thực giống hệt người thật đó. Bạn muốn thử hướng nào?"}},
		quota:  &MuseQuota{PercentUsed: 9}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobImage, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || got.SessionID != "s1" || !strings.HasPrefix(got.Error, "CẦN ĐỔI PROMPT") || !strings.Contains(got.Error, "Bạn muốn thử hướng nào") {
		t.Fatalf("job = %+v", got)
	}
	if len(page.sends) != 1 {
		t.Fatalf("sends = %d", len(page.sends))
	}
	if a, _ := p.accounts.Record(acc.ID); !usable(museView(a), p.cfg.Get().Muse, p.now()) {
		t.Fatal("a declined prompt rested the account")
	}
}

func TestQuotaWithoutResetRestsOnlyForAWhile(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	pause := time.Duration(cfg.UsagePauseMinutes) * time.Minute
	acc := MuseAccount{Quota: &MuseQuota{PercentUsed: 100, CheckedAt: now.Format(time.RFC3339)}}
	if got := pausedUntil(acc, cfg, now); !got.Equal(now.Add(pause)) {
		t.Fatalf("paused until %v, want %v", got, now.Add(pause))
	}
	if got := pausedUntil(acc, cfg, now.Add(pause+time.Minute)); !got.IsZero() {
		t.Fatalf("stale quota still rests the account until %v", got)
	}
}

// A used-up quota whose rest ran out is read again before a send: with room
// again the job runs, still used up the job goes back without an attempt.
func TestStaleQuotaIsReadBeforeSend(t *testing.T) {
	stale := &MuseQuota{PercentUsed: 100, CheckedAt: time.Now().Add(-24 * time.Hour).Format(time.RFC3339)}
	for _, c := range []struct {
		name  string
		fresh float64
		sent  int
	}{{"room again", 40, 1}, {"still out", 100, 0}} {
		t.Run(c.name, func(t *testing.T) {
			result, files := videoResult("/out/a.mp4", []byte("video"))
			page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
				events: result, files: files, quota: &MuseQuota{PercentUsed: c.fresh}}
			p := testProvider(t, page)
			acc := addSignedIn(t, p, "A")
			p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.Quota = stale })
			if a, _ := p.accounts.Record(acc.ID); !usable(museView(a), p.cfg.Get().Muse, p.now()) {
				t.Fatal("a stale quota keeps the account from the queue")
			}
			job := submit(t, p, MuseJobVideo, "x")[0]

			p.runOne(context.Background(), job.ID, acc.ID)
			p.wg.Wait()

			if len(page.sends) != c.sent {
				t.Fatalf("sends = %d", len(page.sends))
			}
			got := mustJob(t, p, job.ID)
			if c.sent == 0 && (got.Status != MuseJobQueued || got.Attempts != 0) {
				t.Fatalf("job = %+v", got)
			}
			if c.sent == 1 && got.Status != MuseJobSucceeded {
				t.Fatalf("job = %+v", got)
			}
		})
	}
}

// A page that never found Muse's chat client sent nothing: no attempt, no
// send time, the job waits for the page.
func TestPageNotReadyIsNotARejection(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: false, Reason: "not_ready (/settings)"}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.Attempts != 0 || got.Sending || got.NextCheckAt == "" {
		t.Fatalf("job = %+v", got)
	}
	if a, _ := p.accounts.Record(acc.ID); museView(a).LastSentAt != "" {
		t.Fatal("a send that never left counts as sent")
	}
}

// A job cancelled while its send is in flight cannot be queued again until
// the send has answered; the answer lands on the cancelled job and its chat
// is stopped on Muse.
func TestRetryWaitsForCancelledSend(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.running[job.ID], p.cancels[job.ID] = acc.ID, cancel
	p.mu.Unlock()
	var retry error
	page.onSend = func() {
		if res := p.CancelJob(job.ID); !res.Success {
			t.Errorf("cancel: %s", res.Error)
		}
		if res := p.RetryJob(job.ID); !res.Success {
			retry = errors.New(res.Error)
		}
	}

	p.runOne(ctx, job.ID, acc.ID)

	if retry == nil || retry.Error() != errRunStopping.Error() {
		t.Fatalf("retry during the send: %v", retry)
	}
	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobCancelled || got.SessionID != "s1" || got.Sending {
		t.Fatalf("job = %+v", got)
	}
	if len(page.cancelled) != 1 || page.cancelled[0] != "s1" {
		t.Fatalf("cancelled on Muse: %v", page.cancelled)
	}
	if res := p.RetryJob(job.ID); !res.Success || res.Data.SessionID != "" {
		t.Fatalf("retry after the run: %+v", res)
	}
}

// The success event is what makes the library screen reload: the files must
// already be in the library by then.
func TestLibraryHasFilesWhenSuccessIsAnnounced(t *testing.T) {
	result, files := videoResult("/out/a.mp4", []byte("video"))
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: result, files: files}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	announced := false
	p.emit = func(e events.Event, payload any) {
		if j, ok := payload.(MuseJob); ok && e == events.EventMuseJobUpdated && j.Status == MuseJobSucceeded {
			announced = true
			if n := len(p.library.List()); n != 1 {
				t.Errorf("library has %d items when the success is announced", n)
			}
		}
	}

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	if !announced {
		t.Fatal("success not announced")
	}
}

// A lost send answer marks the job, so a retry knows it may pay again.
func TestUnknownSendIsMarked(t *testing.T) {
	page := &fakePage{loggedIn: true, sendErr: errors.New("websocket: close 1006")}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)
	p.wg.Wait()

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || !got.SendUnknown || !got.mayBePaid() {
		t.Fatalf("job = %+v", got)
	}
	resetForRetry(&got)
	if got.SendUnknown {
		t.Fatal("a retry keeps the unknown mark")
	}
}

// A job that ends while CancelJob is on its way keeps its answer: a made
// file is never turned into "cancelled".
func TestCancelKeepsAJobThatJustEnded(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	job := submit(t, p, MuseJobImage, "x")[0]
	if _, err := p.jobs.update(job.ID, func(j *MuseJob) { j.Status = MuseJobProcessing }); err != nil {
		t.Fatal(err)
	}
	// The run settles between CancelJob's check and its save.
	p.mu.Lock()
	p.cancels[job.ID] = func() {
		if _, err := p.jobs.update(job.ID, func(j *MuseJob) { j.Status = MuseJobSucceeded }); err != nil {
			t.Error(err)
		}
	}
	p.mu.Unlock()

	if res := p.CancelJob(job.ID); res.Success || res.Error != errJobEnded.Error() {
		t.Fatalf("cancel of an ended job: %+v", res)
	}
	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded {
		t.Fatalf("job = %+v", got)
	}
}

// EditAndRetry never resets a job the scheduler has started, whether the
// run is registered or has already moved the job to Processing.
func TestEditAndRetryLeavesAStartedJob(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	job := submit(t, p, MuseJobImage, "x")[0]
	p.mu.Lock()
	p.running[job.ID] = "A"
	p.mu.Unlock()
	if res := p.EditAndRetry(job.ID, "y"); res.Success || res.Error != errRunStopping.Error() {
		t.Fatalf("edit of a started job: %+v", res)
	}

	p.mu.Lock()
	delete(p.running, job.ID)
	p.mu.Unlock()
	if _, err := p.jobs.update(job.ID, func(j *MuseJob) { j.Status, j.SessionID = MuseJobProcessing, "s1" }); err != nil {
		t.Fatal(err)
	}
	if _, err := p.updateIdle(job.ID, func(j MuseJob) bool { return j.Status == MuseJobQueued || j.finished() }, resetForRetry); !errors.Is(err, errJobChanged) {
		t.Fatalf("updateIdle on a processing job: %v", err)
	}
	if got := mustJob(t, p, job.ID); got.Status != MuseJobProcessing || got.SessionID != "s1" || got.Prompt != "x" {
		t.Fatalf("job = %+v", got)
	}
}
