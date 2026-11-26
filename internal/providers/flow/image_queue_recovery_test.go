package flow

import (
	"context"
	"os"
	"slices"
	"testing"

	"SnakeMedia/internal/models"
)

const recoveryRequestID = "44444444-4444-4444-8444-444444444444"

func obstructImageJournal(t *testing.T, q *imageQueue) func() {
	t.Helper()
	path := q.journal.Path()
	backup := path + ".test-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(backup, path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeletedFlowRequestIsNotAdoptedAfterRestart(t *testing.T) {
	h := newQueueHarness(t)
	task := h.submit("removed")[0]
	if _, err := h.q.journal.Update(task.ID, func(t *ImageTask) {
		t.Status, t.RequestID = ImageTaskCancelled, recoveryRequestID
	}); err != nil {
		t.Fatal(err)
	}
	if res := h.s.RemoveImageTasks([]string{task.ID}); !res.Success || res.Data != 1 {
		t.Fatalf("remove: %+v", res)
	}

	reopened := newImageQueue(h.s)
	if err := reopened.journal.Load(); err != nil {
		t.Fatal(err)
	}
	reopened.pendingJobs = func() ([]generationJob, error) {
		return []generationJob{{
			ID: recoveryRequestID, Kind: generationJobImage, Phase: generationAccepted,
			ImageTemplate: imageGenerationTemplate{ProjectMode: ImageProjectModeAll},
		}}, nil
	}
	reopened.adoptPending()
	if got := reopened.journal.List(); len(got) != 0 {
		t.Fatalf("deleted job was restored: %+v", got)
	}
}

func TestFlowRecoversRunningTaskAfterJournalWriteFails(t *testing.T) {
	h := newQueueHarness(t)
	task := h.submit("recover")[0]
	task, err := h.q.journal.Update(task.ID, func(t *ImageTask) {
		t.Status, t.RequestID = ImageTaskRunning, recoveryRequestID
	})
	if err != nil {
		t.Fatal(err)
	}
	h.setPhase(recoveryRequestID, generationComplete)
	h.onResume = func(_ context.Context, id string) models.Response[[]GeneratedImageItem] {
		return models.SuccessResponse(made(id))
	}

	restore := obstructImageJournal(t, h.q)
	h.q.settle(context.Background(), task, models.SuccessResponse(made(recoveryRequestID)))
	h.q.release(task.ID)
	if _, ok := h.q.nextWake(); !ok {
		t.Fatal("scheduler went to sleep with a running job missing its worker")
	}
	restore()

	h.q.pump()
	h.q.wg.Wait()
	got, _ := h.q.journal.Get(task.ID)
	if got.Status != ImageTaskSucceeded || len(got.SavedFiles) != 1 {
		t.Fatalf("task did not recover and save: %+v", got)
	}
	if len(h.generated) != 0 || !slices.Equal(h.resumed, []string{recoveryRequestID}) {
		t.Fatalf("expected only reconciliation: sends=%v resumes=%v", h.generated, h.resumed)
	}
}

func TestFlowCancelDuringSaveStopsBeforeNextFile(t *testing.T) {
	h := newQueueHarness(t)
	task := h.submit("cancel while saving")[0]
	results := append(made("first"), made("second")...)
	task, err := h.q.journal.Update(task.ID, func(t *ImageTask) {
		t.Status, t.Results = ImageTaskRunning, results
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var saved []string
	h.q.saveImage = func(url string, _ ImageTask) (string, error) {
		saved = append(saved, url)
		cancel()
		return h.dir, nil
	}
	h.q.finishWithResults(ctx, task, results)
	got, _ := h.q.journal.Get(task.ID)
	if got.Status != ImageTaskCancelled || len(got.SavedFiles) != 1 || len(saved) != 1 {
		t.Fatalf("expected cancellation after the in-flight copy: %+v, saved=%v", got, saved)
	}
}

func TestAddFlowReferencesReportsWriteFailureAndCountsOnlyChanges(t *testing.T) {
	h := newQueueHarness(t)
	task := h.submit("reference")[0]
	url, err := h.s.media.Put("reference", "image/png", ".png", []byte("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{task.ID}
	refs := []ImageTaskReference{{LocalURL: url, FileName: "reference.png"}}
	restore := obstructImageJournal(t, h.q)
	res := h.s.AddImageTaskReferences(ids, refs)
	restore()
	if res.Success {
		t.Fatalf("reported success after a journal write failure: %+v", res)
	}
	if res := h.s.AddImageTaskReferences(ids, refs); !res.Success || res.Data != 1 {
		t.Fatalf("first add should change exactly one task: %+v", res)
	}
	if res := h.s.AddImageTaskReferences(ids, refs); !res.Success || res.Data != 0 {
		t.Fatalf("duplicate reference should leave the task unchanged: %+v", res)
	}
}
