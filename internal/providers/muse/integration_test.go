package muse

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"SnakeMedia/internal/models"
)

// A restart keeps how often Muse answered "limit" and a job's hold, so the
// cap is not reset and the job is not retried early.
func TestRestartKeepsLimitRepliesAndHold(t *testing.T) {
	p := testProvider(t, &fakePage{})
	job := submit(t, p, MuseJobImage, "x")[0]
	until := p.now().Add(time.Hour)
	p.updateJob(job.ID, func(j *MuseJob) { j.LimitReplies = 2 })
	p.holdUntil(job.ID, until)

	reloaded := newJobStore(p.jobs.path, func() int { return 100 })
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.get(job.ID)
	if !ok || got.LimitReplies != 2 {
		t.Fatalf("job after restart = %+v", got)
	}
	if left := due([]MuseJob{got}, p.now()); len(left) != 0 {
		t.Fatal("a held job is planned right after a restart")
	}
	if left := due([]MuseJob{got}, until.Add(time.Second)); len(left) != 1 {
		t.Fatal("the hold never ends")
	}
}

// Only a retry the user asks for starts the limit count and hold over.
func TestRetryResetsLimitRepliesAndHold(t *testing.T) {
	p := testProvider(t, &fakePage{})
	job := submit(t, p, MuseJobImage, "x")[0]
	p.updateJob(job.ID, func(j *MuseJob) {
		j.Status, j.LimitReplies, j.NextCheckAt = MuseJobFailed, maxLimitReplies, p.now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	})
	if res := p.RetryJob(job.ID); !res.Success {
		t.Fatal(res.Error)
	}
	if got := mustJob(t, p, job.ID); got.LimitReplies != 0 || got.NextCheckAt != "" {
		t.Fatalf("job after retry = %+v", got)
	}
}

// A journal moved aside on start is reported through the options the UI loads.
func TestCorruptJournalReachesOptions(t *testing.T) {
	p := testProvider(t, &fakePage{})
	if err := os.MkdirAll(filepath.Dir(p.jobs.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.jobs.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.Start(context.Background(), nil)
	defer p.Close()

	res := p.GetOptions()
	if !res.Success || res.Data.JournalWarning == "" {
		t.Fatalf("options = %+v", res)
	}
}

// Reveal opens only files the app stored or saved in its own folders.
func TestRevealRefusesFilesOutsideAppFolders(t *testing.T) {
	p := testProvider(t, &fakePage{})
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := p.RevealFile(outside); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("reveal outside = %+v", res)
	}
	missing := filepath.Join(p.defaultOutputDir, "gone.png")
	if res := p.RevealFile(missing); res.Success || res.Code != models.CodeNotFound {
		t.Fatalf("reveal missing file in the output folder = %+v", res)
	}
}
