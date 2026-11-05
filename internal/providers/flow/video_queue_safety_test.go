package flow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"SnakeMedia/internal/jobs"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/session"
)

func TestVideoQueueUnresolvedRequestCannotBeEditedOrRemoved(t *testing.T) {
	for _, status := range []VideoTaskStatus{VideoTaskQueued, VideoTaskFailed, VideoTaskCancelled} {
		t.Run(string(status), func(t *testing.T) {
			h := newVideoQueueHarness(t)
			task := h.submit("original prompt")[0]
			_, err := h.q.journal.Update(task.ID, func(t *VideoTask) {
				t.Status, t.RequestID, t.AccountID = status, "unknown-request", "owner"
			})
			if err != nil {
				t.Fatal(err)
			}
			h.setPhase("unknown-request", generationAccepted)
			if res := h.s.EditVideoTask(task.ID, "changed prompt"); res.Success {
				t.Fatal("edited an unresolved request")
			}
			if res := h.s.RemoveVideoTasks([]string{task.ID}); !res.Success || res.Data != 0 {
				t.Fatalf("removed unresolved request: %+v", res)
			}
			got, ok := h.q.journal.Get(task.ID)
			if !ok || got.RequestID != "unknown-request" || got.Prompt != "original prompt" {
				t.Fatalf("lost reconciliation identity: %+v", got)
			}
		})
	}
}

func TestVideoQueuePreSendFailureReleasesUnrecordedHold(t *testing.T) {
	h := newVideoQueueHarness(t)
	h.onGen = func(_ context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		h.s.HoldBatchVideoAccount("owner", req.batchTaskID, req.RequestID)
		return models.ErrorResponse[[]GeneratedVideoItem]("session sync failed before submission", models.CodeUnavailable)
	}
	h.start()
	task := h.submit("prompt")[0]
	h.waitForTask(task.ID, func(t VideoTask) bool {
		return t.Status == VideoTaskFailed && !h.s.IsBatchVideoAccountHeld("owner", "next-task")
	})
}

func TestBatchVideoSessionSyncFailureReleasesActualReservation(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "review@example.invalid", true)
	s := testFlowService(accounts)
	s.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return videoModelsPayload, nil }
	s.syncSession = func(context.Context, session.StoredAccount) (session.StoredAccount, error) {
		return session.StoredAccount{}, errors.New("browser unavailable")
	}
	reserved := ""
	res := s.GenerateVideos(context.Background(), GenerateVideoRequest{
		Prompt: "a fox", ModelID: "veo_3_1_fast", AspectRatio: "16:9", Duration: 8, Resolution: "720p", Count: 1,
		batchTaskID: "task", onReserved: func(id string) error { reserved = id; return nil },
	})
	if res.Success || reserved != acc.ID {
		t.Fatalf("did not reach session sync: %+v reserved=%s", res, reserved)
	}
	if s.IsBatchVideoAccountHeld(acc.ID, "next-task") {
		t.Fatal("pre-send failure leaked reservation")
	}
}

func TestVideoQueueAllRefusedSettlesInsteadOfWaitingForever(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "review@example.invalid", true)
	s := testFlowService(accounts)
	s.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return videoModelsPayload, nil }
	q := s.videoQueue
	q.s.media.AllowRoot(accounts.mediaDir)
	res := s.SubmitVideoTasks(VideoTaskRequest{Prompts: []string{"a fox"}, ModelID: "veo_3_1_fast", AspectRatio: "16:9", Duration: 8, Resolution: "720p", OutputDir: accounts.mediaDir})
	if !res.Success {
		t.Fatal(res.Error)
	}
	task := res.Data[0]
	_, err := q.journal.Update(task.ID, func(t *VideoTask) { t.RefusedBy = []string{acc.ID} })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := q.nextDue(time.Now(), true); !ok {
		t.Fatal("exhausted prompt skipped forever")
	}
	_, err = q.journal.Update(task.ID, func(t *VideoTask) { t.Status = VideoTaskRunning })
	if err != nil {
		t.Fatal(err)
	}
	task, _ = q.journal.Get(task.ID)
	q.execute(context.Background(), task)
	task, _ = q.journal.Get(task.ID)
	if task.Status != VideoTaskFailed || task.Error == "" {
		t.Fatalf("exhaustion did not settle: %+v", task)
	}
}

func TestVideoQueueCooldownAppliesToChosenCompatibleAccount(t *testing.T) {
	h := newVideoQueueHarness(t)
	cooling := session.StoredAccount{AccountProfile: session.AccountProfile{ID: "cooling"}}
	ready := session.StoredAccount{AccountProfile: session.AccountProfile{ID: "ready"}}
	h.q.accountCooldown[cooling.ID] = h.clock.Add(time.Hour)
	h.s.rotationMu.Lock()
	idx, lease, err := h.s.reserveVideoCandidateWithBatchLocked([]videoAccountCandidate{{account: cooling}, {account: ready}}, "task", "request")
	h.s.rotationMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	if idx != 1 {
		t.Fatal("selected account still in cooldown")
	}
	h.s.rotationMu.Lock()
	_, _, err = h.s.reserveVideoCandidateWithBatchLocked([]videoAccountCandidate{{account: cooling}}, "other-task", "other-request")
	h.s.rotationMu.Unlock()
	var cooldown *rotationCooldown
	if !errors.As(err, &cooldown) || cooldown.wait != time.Hour {
		t.Fatalf("expected cooldown wait, got %v", err)
	}
}

func TestBatchVideoCooldownWakesWithoutAnotherLeaseRelease(t *testing.T) {
	h := newVideoQueueHarness(t)
	h.q.now = time.Now
	acc := session.StoredAccount{AccountProfile: session.AccountProfile{ID: "cooling"}}
	until := time.Now().Add(30 * time.Millisecond)
	h.q.accountCooldown[acc.ID] = until
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, lease, err := h.s.acquireVideoCandidateWithBatch(ctx, []videoAccountCandidate{{account: acc}}, "task", "request")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	if time.Now().Before(until) {
		t.Fatal("reserved before cooldown expired")
	}
}

func TestVideoQueueCancelKeepsOpenRequestHoldAndRetryResumes(t *testing.T) {
	h := newVideoQueueHarness(t)
	task := h.submit("prompt")[0]
	h.setPhase("open-request", generationAccepted)
	_, err := h.q.journal.Update(task.ID, func(t *VideoTask) { t.RequestID, t.AccountID = "open-request", "owner" })
	if err != nil {
		t.Fatal(err)
	}
	h.s.HoldBatchVideoAccount("owner", task.ID, "open-request")
	if res := h.s.CancelVideoTask(task.ID); !res.Success {
		t.Fatal(res.Error)
	}
	if !h.s.IsBatchVideoAccountHeld("owner", "next-task") {
		t.Fatal("cancel released unresolved billable request")
	}
	if res := h.s.RetryVideoTasks([]string{task.ID}); !res.Success {
		t.Fatal(res.Error)
	}
	_, err = h.q.journal.Update(task.ID, func(t *VideoTask) { t.Status = VideoTaskRunning })
	if err != nil {
		t.Fatal(err)
	}
	h.onResume = func(context.Context, string) models.Response[[]GeneratedVideoItem] {
		h.setPhase("open-request", generationComplete)
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/finished.mp4"}})
	}
	task, _ = h.q.journal.Get(task.ID)
	h.q.execute(context.Background(), task)
	if len(h.generated) != 0 || len(h.resumed) != 1 {
		t.Fatalf("retry sent again: generated=%d resumed=%d", len(h.generated), len(h.resumed))
	}
}

func TestVideoQueueActionsReportPersistenceFailure(t *testing.T) {
	for _, action := range []string{"run", "hold", "retry", "references"} {
		t.Run(action, func(t *testing.T) {
			h := newVideoQueueHarness(t)
			task := h.submit("prompt")[0]
			task.Held = action == "run"
			if action == "retry" {
				task.Status = VideoTaskFailed
			}
			blocker := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			h.q.journal = jobs.New[VideoTask](filepath.Join(blocker, "queue.json"), jobs.Options[VideoTask]{})
			// Journal retains the in-memory record when its write fails.
			if err := h.q.journal.Add(task); err == nil {
				t.Fatal("expected setup write to fail")
			}
			var res models.Response[int]
			switch action {
			case "run":
				res = h.s.RunVideoTasks([]string{task.ID})
			case "hold":
				res = h.s.HoldVideoTasks([]string{task.ID})
			case "retry":
				res = h.s.RetryVideoTasks([]string{task.ID})
			case "references":
				res = h.s.AddVideoTaskReferences([]string{task.ID}, nil)
			}
			if res.Success || res.Error == "" {
				t.Fatalf("write failure reported as success: %+v", res)
			}
		})
	}
}

func TestVideoQueueInterruptedOpenRequestKeepsHold(t *testing.T) {
	h := newVideoQueueHarness(t)
	task := h.submit("prompt")[0]
	_, err := h.q.journal.Update(task.ID, func(t *VideoTask) {
		t.Status, t.RequestID, t.AccountID = VideoTaskRunning, "open-request", "owner"
	})
	if err != nil {
		t.Fatal(err)
	}
	h.setPhase("open-request", generationAccepted)
	h.s.HoldBatchVideoAccount("owner", task.ID, "open-request")
	h.q.interrupted(task.ID)
	h.q.release(task.ID)
	got, _ := h.q.journal.Get(task.ID)
	if got.Status != VideoTaskCancelled || got.RequestID != "open-request" || !h.s.IsBatchVideoAccountHeld("owner", "next-task") {
		t.Fatalf("interruption lost unresolved ownership: %+v", got)
	}
}

func TestVideoQueueOldTaskCannotReleaseAnotherTasksHold(t *testing.T) {
	h := newVideoQueueHarness(t)
	h.s.HoldBatchVideoAccount("owner", "new-task", "new-request")
	h.q.settleAccountCooldown("owner", "old-task")
	if !h.s.IsBatchVideoAccountHeld("owner", "other-task") {
		t.Fatal("old worker released another task's hold")
	}
	if h.q.accountWait("owner", h.clock) != 0 {
		t.Fatal("old worker changed another task's cooldown")
	}
}

func TestVideoQueueNewerTimingIsNotOverwrittenOrUsedToSend(t *testing.T) {
	h := newVideoQueueHarness(t)
	h.q.timingPath = filepath.Join(t.TempDir(), "timing.json")
	before := []byte(fmt.Sprintf(`{"formatVersion":%d,"nextLaunch":"2099-01-01T00:00:00Z"}`, jobs.FormatVersion+1))
	if err := os.WriteFile(h.q.timingPath, before, 0o600); err != nil {
		t.Fatal(err)
	}
	h.q.loadTiming()
	if err := h.q.waitForNextLaunch(context.Background()); !errors.Is(err, jobs.ErrNewerFormat) {
		t.Fatalf("sent using newer timing: %v", err)
	}
	after, err := os.ReadFile(h.q.timingPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("newer timing overwritten")
	}
}
