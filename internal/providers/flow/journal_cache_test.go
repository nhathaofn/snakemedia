package flow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalListingReusesDecodedJobsAndQuarantinesBrokenOnes(t *testing.T) {
	s := testFlowService(testAccountService(t))
	job := newGenerationJob("11111111-1111-4111-8111-111111111111", generationJobVideo, "fp", "acc")
	job.Phase = generationAccepted
	if err := s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	dir := s.stateDir("generation-jobs")
	broken := filepath.Join(dir, "44444444-4444-4444-8444-444444444444.json")
	if err := os.WriteFile(broken, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	jobs, err := s.pendingGenerationJobs(generationJobVideo)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("pending = %+v, %v", jobs, err)
	}
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatal("a broken journal must be moved out of the listing")
	}
	if _, err := os.Stat(broken + ".corrupt"); err != nil {
		t.Fatalf("the broken journal must be kept aside: %v", err)
	}

	// Garbage of the same size and time stamp: only a cached listing still
	// sees the job, which proves the file was not decoded again.
	path, _ := s.statePath("generation-jobs", job.ID)
	info, _ := os.Stat(path)
	garbage := make([]byte, info.Size())
	for i := range garbage {
		garbage[i] = 'x'
	}
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if jobs, err := s.pendingGenerationJobs(generationJobVideo); err != nil || len(jobs) != 1 {
		t.Fatalf("an unchanged journal should come from the cache: %+v, %v", jobs, err)
	}

	// Saving through the app always refreshes the entry.
	job.Phase = generationComplete
	if err := s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if jobs, err := s.pendingGenerationJobs(generationJobVideo); err != nil || len(jobs) != 0 {
		t.Fatalf("a saved journal must be read again: %+v, %v", jobs, err)
	}
}

func TestEditJobListingDropsDeletedJournals(t *testing.T) {
	s := testFlowService(testAccountService(t))
	job := editJob{ID: "55555555-5555-4555-8555-555555555555", Template: GeneratedVideoItem{Prompt: "a fox", CreatedAt: time.Now().UTC().Format(time.RFC3339)}}
	if err := s.saveEditJob(job); err != nil {
		t.Fatal(err)
	}
	if res := s.ListVideoEditJobs(context.Background()); !res.Success || len(res.Data) != 1 {
		t.Fatalf("list = %+v", res)
	}
	if err := s.removeState("jobs", job.ID); err != nil {
		t.Fatal(err)
	}
	if res := s.ListVideoEditJobs(context.Background()); !res.Success || len(res.Data) != 0 {
		t.Fatalf("a deleted journal must leave the listing: %+v", res)
	}
	if _, ok := s.journals.byKind["jobs"][job.ID]; ok {
		t.Fatal("the cache must drop journals whose files are gone")
	}
}
