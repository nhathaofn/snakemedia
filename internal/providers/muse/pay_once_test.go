package muse

// Regression tests for paths that could send a job Muse already accepted.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Updating a job while the journal is over its cap returns that same job and
// never drops or shifts unfinished ones.
func TestUpdateOverCapKeepsUnfinishedJobs(t *testing.T) {
	s := newJobStore(filepath.Join(t.TempDir(), "jobs.json"), func() int { return 2 })
	ids := []string{"done1", "q1", "q2", "q3"}
	for _, id := range ids {
		status := MuseJobQueued
		if id == "done1" {
			status = MuseJobSucceeded
		}
		if _, err := s.add(MuseJob{ID: id, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.update("q3", func(j *MuseJob) { j.Status = MuseJobSucceeded })
	if err != nil || got.ID != "q3" {
		t.Fatalf("update returned %+v, %v", got, err)
	}
	for _, id := range []string{"q1", "q2", "q3"} {
		if _, ok := s.get(id); !ok {
			t.Fatalf("job %s dropped", id)
		}
	}
}

// Muse answering ok without naming its chat may be generating: the job fails
// as unknown instead of being sent again.
func TestAcceptedSendWithoutSessionIsUnknown(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobFailed || got.Error != errSendUnknown.Error() || len(page.sends) != 1 {
		t.Fatalf("job = %+v sends=%d", got, len(page.sends))
	}
}

// A session Muse does not list is read from its chat rather than treated as
// finished with no file and sent again.
func TestUnlistedSessionIsReadFromChat(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{""}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(page.sends) != 1 {
		t.Fatalf("job = %+v sends=%d", got, len(page.sends))
	}
}

// An unknown state keeps the job waiting; the chat is never read empty and
// sent again.
func TestUnknownStatusKeepsWaiting(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"starting"}, cancelErr: errors.New("gone")}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	// It waited until the time limit, could not stop Muse, and is checked again.
	if got := mustJob(t, p, job.ID); got.Status != MuseJobProcessing || got.SessionID != "s1" || got.Rechecks != 1 || len(page.sends) != 1 {
		t.Fatalf("job = %+v sends=%d", got, len(page.sends))
	}
}

// A job resumed past its time limit whose file is ready is collected, not
// cancelled and sent again.
func TestOverdueResumedJobIsCollected(t *testing.T) {
	page := &fakePage{loggedIn: true, statuses: []string{"running"}}
	page.history, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	sent := p.now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.SessionID, j.Attempts, j.SentAt = MuseJobProcessing, acc.ID, "s1", 1, sent
	})

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(page.cancelled) != 0 || len(page.sends) != 0 {
		t.Fatalf("job = %+v cancelled=%v sends=%d", got, page.cancelled, len(page.sends))
	}
}

// The time limit counts from the send, not from when the job was picked.
func TestTimeLimitCountsFromSend(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"}}
	page.events, page.files = videoResult("/v.mp4", []byte("v"))
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]
	// Picked long ago (a long send gap or VM boot), sent just now.
	p.jobs.update(job.ID, func(j *MuseJob) {
		j.Status, j.AccountID, j.Attempts = MuseJobProcessing, acc.ID, 1
		j.StartedAt = p.now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	})

	p.runOne(context.Background(), job.ID, acc.ID)

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobSucceeded || got.SentAt == "" || len(page.cancelled) != 0 {
		t.Fatalf("job = %+v cancelled=%v", got, page.cancelled)
	}
}

// A file Muse made but cannot hand over is read again later, not paid again.
func TestMissingResultIsRechecked(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Videos: []ResultMedia{{Path: "/v.mp4", Mime: "video/mp4", Missing: true}}}}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobProcessing || got.SessionID != "s1" || got.Rechecks != 1 || len(page.sends) != 1 {
		t.Fatalf("job = %+v sends=%d", got, len(page.sends))
	}
}

// A result whose size Muse left out is still downloaded.
func TestResultWithoutSizeIsDownloaded(t *testing.T) {
	page := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "s1"}, statuses: []string{"running", "completed"},
		events: []AssistantEvent{{Videos: []ResultMedia{{Path: "/v.mp4", Mime: "video/mp4"}}}},
		files:  map[string][]byte{"/v.mp4": []byte("video")}}
	p := testProvider(t, page)
	acc := addSignedIn(t, p, "A")
	job := submit(t, p, MuseJobVideo, "x")[0]

	p.runOne(context.Background(), job.ID, acc.ID)

	if got := mustJob(t, p, job.ID); got.Status != MuseJobSucceeded || len(got.Files) != 1 || len(page.sends) != 1 {
		t.Fatalf("job = %+v sends=%d", got, len(page.sends))
	}
}

// "Retry failed" leaves jobs Muse accepted, or may have accepted, alone.
func TestRetryFailedSkipsAcceptedJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobVideo, "plain", "accepted", "unknown")
	p.jobs.update(jobs[0].ID, func(j *MuseJob) { j.Status = MuseJobFailed })
	p.jobs.update(jobs[1].ID, func(j *MuseJob) { j.Status, j.SessionID = MuseJobFailed, "s1" })
	p.jobs.update(jobs[2].ID, func(j *MuseJob) { j.Status, j.Error = MuseJobFailed, errSendUnknown.Error() })

	if res := p.RetryFailed("", ""); !res.Success || res.Data != 1 {
		t.Fatalf("retried %+v", res)
	}
	if got := mustJob(t, p, jobs[0].ID); got.Status != MuseJobQueued {
		t.Fatalf("plain job = %+v", got)
	}
	for _, j := range jobs[1:] {
		if got := mustJob(t, p, j.ID); got.Status != MuseJobFailed {
			t.Fatalf("job %s re-queued: %+v", j.Prompt, got)
		}
	}
}

// Bulk actions of one screen leave the other screen's jobs alone; a job from a
// journal written before modes existed belongs to Hàng loạt.
func TestBulkActionsStayInTheirMode(t *testing.T) {
	p := testProvider(t, &fakePage{})
	single := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Mode: MuseJobSingle, Prompts: []string{"đơn"}}).Data[0]
	batch := submit(t, p, MuseJobImage, "hàng loạt", "cũ")
	if single.Mode != MuseJobSingle || batch[0].Mode != MuseJobBatch {
		t.Fatalf("modes = %q %q", single.Mode, batch[0].Mode)
	}
	p.jobs.update(batch[1].ID, func(j *MuseJob) { j.Mode = "" })

	if res := p.HoldQueued(MuseJobImage, MuseJobBatch); !res.Success || res.Data != 2 {
		t.Fatalf("hold = %+v", res)
	}
	if got := mustJob(t, p, single.ID); got.Held {
		t.Fatalf("single job held by the batch hold: %+v", got)
	}
	if res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Mode: "x", Prompts: []string{"a"}}); res.Success {
		t.Fatalf("unknown mode accepted: %+v", res)
	}
	if res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Mode: MuseJobBatch, Prompts: []string{"a"}, Repeat: 2}); res.Success {
		t.Fatalf("batch variants accepted: %+v", res)
	}
	if res := p.SubmitJobs(MuseJobRequest{Kind: MuseJobImage, Mode: MuseJobSingle, Prompts: []string{"a"}, Repeat: 2}); !res.Success || len(res.Data) != 2 {
		t.Fatalf("single variants = %+v", res)
	}
}
