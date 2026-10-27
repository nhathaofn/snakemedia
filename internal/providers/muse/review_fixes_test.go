package muse

// Regression tests for sends Muse may already have billed.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Cancelling while the send is in flight still cancels the generation
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

// A job Muse already accepted is re-checked past MaxAttempts, and only
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
