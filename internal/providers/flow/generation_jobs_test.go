package flow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ageState backdates a saved record so it looks idle for d.
func ageState(t *testing.T, s *Provider, kind, id string, d time.Duration) {
	t.Helper()
	path, err := s.statePath(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestPendingGenerationJobsSurviveCorruptJournalsAndExpire(t *testing.T) {
	s := testFlowService(testAccountService(t))
	retention := s.jobRetention()

	pending := newGenerationJob("11111111-1111-4111-8111-111111111111", generationJobImage, "fp", "acc")
	pending.Phase = generationAccepted
	stale := newGenerationJob("22222222-2222-4222-8222-222222222222", generationJobImage, "fp", "acc")
	stale.Phase = generationSubmitting
	done := newGenerationJob("33333333-3333-4333-8333-333333333333", generationJobImage, "fp", "acc")
	done.Phase = generationComplete
	for _, job := range []generationJob{pending, stale, done} {
		if err := s.saveGenerationJob(job); err != nil {
			t.Fatal(err)
		}
	}
	ageState(t, s, "generation-jobs", stale.ID, retention+time.Hour)
	ageState(t, s, "generation-jobs", done.ID, retention+time.Hour)
	corrupt := filepath.Join(s.stateDir("generation-jobs"), "44444444-4444-4444-8444-444444444444.json")
	if err := os.WriteFile(corrupt, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := s.ListPendingImageGenerations(ImageProjectModeSingle)
	if !res.Success {
		t.Fatalf("one corrupt journal must not break recovery: %s", res.Error)
	}
	if len(res.Data) != 1 || res.Data[0].JobID != pending.ID {
		t.Fatalf("want only the live pending job, got %+v", res.Data)
	}
	if _, err := s.loadGenerationJob(done.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("finished journal past retention was not deleted: %v", err)
	}
	closed, err := s.loadGenerationJob(stale.ID)
	if err != nil || closed.Phase != generationRejected {
		t.Fatalf("idle unconfirmed job was not closed: %+v %v", closed, err)
	}
}

func TestListVideoEditJobsSkipsCorruptAndClosesAbandoned(t *testing.T) {
	s := testFlowService(testAccountService(t))
	retention := s.jobRetention()
	abandoned := editJob{ID: "55555555-5555-4555-8555-555555555555", Template: GeneratedVideoItem{Prompt: "a fox"}}
	finished := editJob{ID: "66666666-6666-4666-8666-666666666666", Complete: true}
	for _, job := range []editJob{abandoned, finished} {
		if err := s.saveState("jobs", job.ID, job); err != nil {
			t.Fatal(err)
		}
		ageState(t, s, "jobs", job.ID, retention+time.Hour)
	}
	if err := os.WriteFile(filepath.Join(s.stateDir("jobs"), "77777777-7777-4777-8777-777777777777.json"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := s.ListVideoEditJobs(context.Background())
	if !res.Success || len(res.Data) != 1 {
		t.Fatalf("want the abandoned job only, got %+v", res)
	}
	if res.Data[0].Pending || res.Data[0].Error == "" {
		t.Fatalf("abandoned edit must be closed with a reason: %+v", res.Data[0])
	}
	if _, err := os.Stat(filepath.Join(s.stateDir("jobs"), finished.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("finished edit journal past retention was not deleted: %v", err)
	}
}

func TestImageJobStaysOpenUntilEveryImageIsDownloaded(t *testing.T) {
	downloaded := GeneratedImageItem{URL: "https://flow/1", LocalURL: "/flow-media/1.png"}
	failed := GeneratedImageItem{URL: "https://flow/2", Error: "Không tải được ảnh để hiển thị"}
	rejected := GeneratedImageItem{Error: "bộ lọc an toàn"}
	if got := imageJobPhase([]GeneratedImageItem{downloaded, failed}); got != generationAccepted {
		t.Fatalf("a failed download must keep the job open, got %s", got)
	}
	if got := imageJobPhase([]GeneratedImageItem{downloaded, rejected}); got != generationComplete {
		t.Fatalf("Flow-side failures are final, got %s", got)
	}
}

func TestListPendingImageGenerationsFiltersByStudio(t *testing.T) {
	s := testFlowService(testAccountService(t))
	single := newGenerationJob("88888888-8888-4888-8888-888888888888", generationJobImage, "fp", "acc")
	single.Phase = generationAccepted // no ProjectMode: journals from before the field are Single
	all := newGenerationJob("99999999-9999-4999-8999-999999999999", generationJobImage, "fp", "acc")
	all.Phase, all.ImageTemplate.ProjectMode = generationAccepted, ImageProjectModeAll
	for _, job := range []generationJob{single, all} {
		if err := s.saveGenerationJob(job); err != nil {
			t.Fatal(err)
		}
	}
	for mode, want := range map[ImageProjectMode]string{ImageProjectModeSingle: single.ID, ImageProjectModeAll: all.ID} {
		res := s.ListPendingImageGenerations(mode)
		if !res.Success || len(res.Data) != 1 || res.Data[0].JobID != want {
			t.Fatalf("%s studio got %+v", mode, res)
		}
	}
}

func TestSubmissionLostOnlyAfterTheGenerationTimeout(t *testing.T) {
	s := testFlowService(testAccountService(t))
	job := newGenerationJob("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", generationJobImage, "fp", "acc")
	job.Phase = generationSubmitting
	if s.submissionLost(job) {
		t.Fatal("a fresh submission was treated as lost")
	}
	limits := s.cfg.Get().Accounts
	job.CreatedAt = time.Now().Add(-time.Duration(limits.GenerationTimeoutSeconds+limits.RequestTimeoutSeconds+1) * time.Second).UTC().Format(time.RFC3339)
	if !s.submissionLost(job) {
		t.Fatal("an old submission with no result was kept pending")
	}
	job.Phase = generationAccepted
	if s.submissionLost(job) {
		t.Fatal("an accepted job must never be treated as lost")
	}
	if err := s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	job.Phase = generationSubmitting
	if err := s.rejectLostSubmission(job); !errors.Is(err, errLostSubmission) {
		t.Fatalf("want errLostSubmission, got %v", err)
	}
	if saved, _ := s.loadGenerationJob(job.ID); saved.Phase != generationRejected {
		t.Fatalf("lost job not closed: %s", saved.Phase)
	}
}
