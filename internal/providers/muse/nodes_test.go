package muse

import (
	"context"
	"errors"
	"testing"

	"SnakeMedia/internal/engine"
)

func TestNodeHandsOnMediaRefs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobVideo, "a cat")
	if _, err := p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.Files = MuseJobSucceeded, []string{"/flow-media/out-1"}
	}); err != nil {
		t.Fatal(err)
	}
	res, err := p.awaitJobs(context.Background(), jobs, false)
	if err != nil || len(res.Files) != 1 || res.Files[0].Kind != engine.MediaVideo || res.Files[0].LocalURL != "/flow-media/out-1" {
		t.Fatalf("files handed on as media references: %+v %v", res, err)
	}
}

func TestStoppedNodeCancelsOnlyUnsentJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobImage, "sent", "waiting")
	if _, err := p.jobs.update(jobs[0].ID, func(j *MuseJob) {
		j.Status, j.SessionID = MuseJobProcessing, "chat-1"
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.awaitJobs(ctx, jobs, true)
	var pending *engine.PendingError
	if !errors.As(err, &pending) || len(pending.JobIDs) != 1 || pending.JobIDs[0] != jobs[0].ID {
		t.Fatalf("the sent job must keep the node pending: %v", err)
	}
	if mustJob(t, p, jobs[0].ID).Status != MuseJobProcessing {
		t.Fatal("a job Muse already took must keep running")
	}
	if mustJob(t, p, jobs[1].ID).Status != MuseJobCancelled {
		t.Fatal("an unsent job must be cancelled when asked")
	}
}

func TestStoppedNodeKeepsQueueWithoutCancelOnStop(t *testing.T) {
	p := testProvider(t, &fakePage{})
	jobs := submit(t, p, MuseJobImage, "waiting")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.awaitJobs(ctx, jobs, false)
	var pending *engine.PendingError
	if !errors.As(err, &pending) || mustJob(t, p, jobs[0].ID).Status != MuseJobQueued {
		t.Fatalf("without cancelOnStop the job stays queued: %v", err)
	}
}

// TestGraphRerunFindsItsJobs submits the same graph run twice: the second
// submit must return the jobs of the first instead of queueing new ones, and
// only a job Muse never took moves on to a new attempt.
func TestGraphRerunFindsItsJobs(t *testing.T) {
	p := testProvider(t, &fakePage{})
	req := MuseJobRequest{Kind: MuseJobImage, Prompts: []string{"a", "b"}}
	first := mustSubmit(t, p, req, "run-1/m")
	again := mustSubmit(t, p, req, "run-1/m")
	if len(again) != 2 || again[0].ID != first[0].ID || again[1].ID != first[1].ID || len(p.jobs.list()) != 2 {
		t.Fatalf("a re-run must find its jobs: %+v vs %+v", again, first)
	}
	if _, err := p.jobs.update(first[0].ID, func(j *MuseJob) { j.Status = MuseJobFailed }); err != nil {
		t.Fatal(err)
	}
	if _, err := p.jobs.update(first[1].ID, func(j *MuseJob) { j.Status, j.SessionID = MuseJobFailed, "chat-1" }); err != nil {
		t.Fatal(err)
	}
	retry := mustSubmit(t, p, req, "run-1/m")
	if retry[0].ID == first[0].ID || retry[1].ID != first[1].ID {
		t.Fatalf("only the never-sent failure is queued again: %+v", retry)
	}
	if other := mustSubmit(t, p, req, ""); other[0].ID == first[0].ID {
		t.Fatal("without a run key every job is new")
	}
}

func mustSubmit(t *testing.T, p *Provider, req MuseJobRequest, runKey string) []MuseJob {
	t.Helper()
	res := p.submitJobs(req, runKey)
	if !res.Success {
		t.Fatalf("submit failed: %s", res.Error)
	}
	return res.Data
}
