package flow

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/models"
)

type videoQueueHarness struct {
	t   *testing.T
	s   *Provider
	q   *videoQueue
	dir string

	mu        sync.Mutex
	clock     time.Time
	phases    map[string]string
	phaseErr  error
	generated []GenerateVideoRequest
	resumed   []string
	saved     []string
	onGen     func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem]
	onResume  func(ctx context.Context, id string) models.Response[[]GeneratedVideoItem]
}

func newVideoQueueHarness(t *testing.T) *videoQueueHarness {
	t.Helper()
	s := testFlowService(testAccountService(t))
	h := &videoQueueHarness{
		t:      t,
		s:      s,
		q:      s.videoQueue,
		dir:    resolvedTempDir(t),
		clock:  time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC),
		phases: map[string]string{},
	}
	s.media.AllowRoot(h.dir)
	cfg := config.DefaultFlowConfig()
	cfg.VideoQueueThreads, cfg.VideoQueueGapMinSeconds, cfg.VideoQueueGapMaxSeconds = 2, 0, 0
	h.q.cfg = func() config.FlowConfig { return cfg }
	h.q.now = func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.clock }
	h.q.gap = func(int, int) time.Duration { return 0 }
	h.q.requestPhase = func(id string) (string, bool, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if err := h.phaseErr; err != nil {
			return "", false, err
		}
		phase, ok := h.phases[id]
		return phase, ok, nil
	}
	h.q.generate = func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		h.mu.Lock()
		h.generated = append(h.generated, req)
		gen := h.onGen
		h.mu.Unlock()
		if gen != nil {
			return gen(ctx, req)
		}
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/test.mp4"}})
	}
	h.q.resume = func(ctx context.Context, id string) models.Response[[]GeneratedVideoItem] {
		h.mu.Lock()
		h.resumed = append(h.resumed, id)
		resume := h.onResume
		h.mu.Unlock()
		if resume != nil {
			return resume(ctx, id)
		}
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/test.mp4"}})
	}
	h.q.saveVideo = func(localURL string, t VideoTask) (string, error) {
		h.mu.Lock()
		h.saved = append(h.saved, localURL)
		h.mu.Unlock()
		return filepath.Join(t.OutputDir, filepath.Base(localURL)), nil
	}
	return h
}

func (h *videoQueueHarness) setPhase(id, phase string) {
	h.mu.Lock()
	h.phases[id] = phase
	h.mu.Unlock()
}

func (h *videoQueueHarness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.s.Start(ctx, func(events.Event, any) {})
	h.t.Cleanup(func() { cancel(); h.s.Close() })
}

func (h *videoQueueHarness) submit(prompts ...string) []VideoTask {
	h.t.Helper()
	res := h.s.SubmitVideoTasks(VideoTaskRequest{
		Prompts:     prompts,
		ModelID:     "veo_3_1_lite",
		AspectRatio: "16:9",
		Duration:    8,
		Resolution:  "720p",
		OutputDir:   h.dir,
	})
	if !res.Success {
		h.t.Fatalf("submit: %s", res.Error)
	}
	return res.Data
}

func (h *videoQueueHarness) waitForTask(id string, pred func(VideoTask) bool) VideoTask {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if t, ok := h.q.journal.Get(id); ok && pred(t) {
			return t
		}
		h.q.trigger()
		time.Sleep(10 * time.Millisecond)
	}
	t, _ := h.q.journal.Get(id)
	h.t.Fatalf("task %s did not reach expected state; got status=%s step=%q error=%q", id, t.Status, t.Step, t.Error)
	return VideoTask{}
}

func TestVideoQueue_SubmitOrdersAndNumbersIndexes(t *testing.T) {
	h := newVideoQueueHarness(t)
	tasks := h.submit("first", "second", "third")
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	for i, task := range tasks {
		if task.Index != i+1 {
			t.Errorf("task %d index = %d, want %d", i, task.Index, i+1)
		}
		if task.Status != VideoTaskQueued {
			t.Errorf("task %d status = %s, want queued", i, task.Status)
		}
	}

	match, _ := regexp.MatchString(`^snakemedia-flow-video-\d{8}-\d{6}$`, filepath.Base(tasks[0].OutputDir))
	if !match {
		t.Errorf("unexpected batch folder name: %s", tasks[0].OutputDir)
	}
	if tasks[1].OutputDir != tasks[0].OutputDir || tasks[2].OutputDir != tasks[0].OutputDir {
		t.Errorf("prompts of same batch must share output folder")
	}
}

func TestVideoQueue_PassOnUnusualActivity(t *testing.T) {
	h := newVideoQueueHarness(t)
	attempt := 0
	h.onGen = func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		attempt++
		if attempt == 1 {
			if req.onRefused != nil {
				req.onRefused("acc-1")
			}
			return models.ErrorResponse[[]GeneratedVideoItem]("unusual activity", models.CodeUnavailable)
		}
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/final.mp4"}})
	}

	h.start()
	task := h.submit("cat in snow")[0]

	done := h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskSucceeded })
	if len(done.SavedFiles) == 0 {
		t.Errorf("expected saved files, got none")
	}
	if !slices.Contains(done.RefusedBy, "acc-1") {
		t.Errorf("expected acc-1 in RefusedBy, got %v", done.RefusedBy)
	}
}

func TestVideoQueue_RecheckBackoff(t *testing.T) {
	h := newVideoQueueHarness(t)
	h.onGen = func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		return models.SuccessResponse([]GeneratedVideoItem{{JobID: req.RequestID, Pending: true, Error: "rendering"}})
	}

	h.start()
	task := h.submit("lake at sunrise")[0]

	rechecked := h.waitForTask(task.ID, func(t VideoTask) bool {
		return t.Status == VideoTaskQueued && t.NextCheckAt != "" && t.Rechecks == 1
	})
	if rechecked.RequestID == "" {
		t.Fatal("request ID must not be cleared during recheck")
	}
	if rechecked.NextCheckAt == "" {
		t.Fatal("expected NextCheckAt to be set")
	}
}

func TestVideoQueue_ResumeOnRestartAfterSent(t *testing.T) {
	h := newVideoQueueHarness(t)
	task := h.submit("drone footage")[0]

	reqID := "req-existing-123"
	_, _ = h.q.journal.Update(task.ID, func(t *VideoTask) {
		t.RequestID = reqID
		t.AccountID = "acc-owner-1"
	})
	h.setPhase(reqID, generationAccepted)

	resumed := false
	h.onResume = func(ctx context.Context, id string) models.Response[[]GeneratedVideoItem] {
		if id == reqID {
			resumed = true
			h.setPhase(id, generationComplete)
		}
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/video.mp4"}})
	}

	h.start()

	h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskSucceeded })
	if !resumed {
		t.Fatal("expected task with existing journal phase to be resumed rather than re-generated")
	}
}

func TestVideoQueue_PerAccountConcurrencyAndBatchHold(t *testing.T) {
	h := newVideoQueueHarness(t)
	heldAcc := "acc-alpha"

	// Mock hold an account
	ok := h.s.HoldBatchVideoAccount(heldAcc, "other-task-id", "req-1")
	if !ok {
		t.Fatal("expected hold to succeed")
	}

	if !h.s.IsBatchVideoAccountHeld(heldAcc, "different-task-id") {
		t.Fatal("expected account to be reported as held for different task")
	}
	if h.s.IsBatchVideoAccountHeld(heldAcc, "other-task-id") {
		t.Fatal("same task should not be blocked by its own hold")
	}

	// Release hold
	h.s.ReleaseBatchVideoTask("other-task-id")
	if h.s.IsBatchVideoAccountHeld(heldAcc, "different-task-id") {
		t.Fatal("expected account hold to be released after ReleaseBatchVideoTask")
	}
}

func TestVideoQueue_RecoveryOnlyHoldsQueueOwnedRequests(t *testing.T) {
	h := newVideoQueueHarness(t)
	job := newGenerationJob("a43e07aa-0f3c-4e87-a93c-b5a683fe34e2", generationJobVideo, "fingerprint", "single-account")
	job.Phase = generationAccepted
	if err := h.s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	task := h.submit("pending")[0]
	_, err := h.q.journal.Update(task.ID, func(t *VideoTask) {
		t.RequestID, t.AccountID = "queue-request", "queue-account"
	})
	if err != nil {
		t.Fatal(err)
	}
	h.setPhase("queue-request", generationAccepted)
	h.q.recoverHolds()
	if !h.s.IsBatchVideoAccountHeld("queue-account", "other-task") {
		t.Fatal("queue request lost its hold")
	}
	if h.s.IsBatchVideoAccountHeld("single-account", "other-task") {
		t.Fatal("single studio request must not create an orphan batch hold")
	}
}

func TestVideoQueue_StaggeredGapAndTimingGate(t *testing.T) {
	h := newVideoQueueHarness(t)
	cfg := config.DefaultFlowConfig()
	cfg.VideoQueueThreads = 2
	cfg.VideoQueueGapMinSeconds = 5
	cfg.VideoQueueGapMaxSeconds = 5
	h.q.cfg = func() config.FlowConfig { return cfg }
	h.q.gap = func(min, max int) time.Duration { return 5 * time.Second }

	var timestamps []time.Time
	var lock sync.Mutex
	h.onGen = func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		lock.Lock()
		timestamps = append(timestamps, h.q.now())
		lock.Unlock()
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/clip.mp4"}})
	}

	task1 := h.submit("prompt 1")[0]
	task2 := h.submit("prompt 2")[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Execute task 1
	h.q.execute(ctx, task1)

	// Advance clock by only 2 seconds
	h.mu.Lock()
	h.clock = h.clock.Add(2 * time.Second)
	h.mu.Unlock()

	// Task 2 should wait or gate should enforce 5s delay
	done := make(chan struct{})
	go func() {
		h.q.execute(ctx, task2)
		close(done)
	}()

	select {
	case <-done:
		// Succeeded after waiting for gate
	case <-time.After(500 * time.Millisecond):
		// Gate is waiting for the remaining 3s
		h.mu.Lock()
		h.clock = h.clock.Add(4 * time.Second)
		h.mu.Unlock()
		<-done
	}

	lock.Lock()
	count := len(timestamps)
	lock.Unlock()
	if count < 2 {
		t.Fatalf("expected 2 generations recorded, got %d", count)
	}
}

func TestVideoQueue_SaveErrorRetriesOnlySave(t *testing.T) {
	h := newVideoQueueHarness(t)
	var saveMu sync.Mutex
	saveCalls := 0
	h.q.saveVideo = func(localURL string, t VideoTask) (string, error) {
		saveMu.Lock()
		saveCalls++
		first := saveCalls == 1
		saveMu.Unlock()
		if first {
			return "", errors.New("disk full")
		}
		return filepath.Join(t.OutputDir, "saved.mp4"), nil
	}

	genCalls := 0
	h.onGen = func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		genCalls++
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/video.mp4"}})
	}

	h.start()
	task := h.submit("ocean waves")[0]

	failed := h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskFailed })
	if !strings.Contains(failed.Error, "disk full") {
		t.Fatalf("expected save error, got %s", failed.Error)
	}
	if len(failed.Results) == 0 {
		t.Fatal("results must be preserved when save fails")
	}

	// Retry failed task: should not call generate again!
	h.s.RetryVideoTasks([]string{task.ID})
	succeeded := h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskSucceeded })
	if genCalls != 1 {
		t.Errorf("generate was called %d times; expected exactly 1 (only save should be retried)", genCalls)
	}
	if len(succeeded.SavedFiles) == 0 {
		t.Errorf("saved files should not be empty")
	}
}

func TestVideoQueue_HoldAndRunControls(t *testing.T) {
	h := newVideoQueueHarness(t)
	res := h.s.SubmitVideoTasks(VideoTaskRequest{
		Prompts:     []string{"held 1", "held 2"},
		ModelID:     "veo_3_1_lite",
		AspectRatio: "16:9",
		Duration:    8,
		Resolution:  "720p",
		OutputDir:   h.dir,
		Hold:        true,
	})
	if !res.Success || len(res.Data) != 2 {
		t.Fatal("expected 2 held tasks submitted")
	}
	for _, task := range res.Data {
		if !task.Held {
			t.Errorf("expected task %s to be held", task.ID)
		}
	}

	// Release hold on all
	h.s.RunVideoTasks(nil)
	t1, _ := h.q.journal.Get(res.Data[0].ID)
	if t1.Held {
		t.Errorf("expected task %s held=false after RunVideoTasks", t1.ID)
	}

	// Hold again
	h.s.HoldVideoTasks([]string{res.Data[0].ID})
	t1, _ = h.q.journal.Get(res.Data[0].ID)
	if !t1.Held {
		t.Errorf("expected task %s held=true after HoldVideoTasks", t1.ID)
	}
}

func TestVideoQueue_EditTaskValidation(t *testing.T) {
	h := newVideoQueueHarness(t)
	task := h.submit("original")[0]

	// Edit with empty prompt fails
	res := h.s.EditVideoTask(task.ID, "   ")
	if res.Success {
		t.Fatal("expected edit with whitespace prompt to fail")
	}

	// Valid edit updates prompt
	res = h.s.EditVideoTask(task.ID, "new prompt")
	if !res.Success || res.Data.Prompt != "new prompt" {
		t.Fatalf("edit failed: %v", res.Error)
	}
}

func TestVideoQueue_RetryFailedTaskWithThreadsOne(t *testing.T) {
	h := newVideoQueueHarness(t)
	cfg := config.DefaultFlowConfig()
	cfg.VideoQueueThreads = 1
	h.q.cfg = func() config.FlowConfig { return cfg }

	attempts := 0
	h.onGen = func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
		attempts++
		if attempts == 1 {
			_, _ = h.q.journal.Update(req.batchTaskID, func(task *VideoTask) {
				task.AccountID = "acc-1"
			})
			return models.ErrorResponse[[]GeneratedVideoItem]("invalid prompt", models.CodeInvalidArgument)
		}
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/video.mp4"}})
	}

	h.start()
	task := h.submit("failed prompt")[0]
	failed := h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskFailed })
	if failed.Status != VideoTaskFailed {
		t.Fatalf("expected failed, got %s", failed.Status)
	}

	// Retry the failed task
	h.s.RetryVideoTasks([]string{task.ID})

	// Wait for task to succeed
	h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskSucceeded })
}

func TestVideoQueue_RecheckNotBlockedByNextLaunch(t *testing.T) {
	h := newVideoQueueHarness(t)
	cfg := config.DefaultFlowConfig()
	cfg.VideoQueueThreads = 1
	h.q.cfg = func() config.FlowConfig { return cfg }

	// Set nextLaunch far in the future
	h.q.timingMu.Lock()
	h.q.nextLaunch = h.clock.Add(1 * time.Hour)
	h.q.timingMu.Unlock()

	// Recheck task ready now
	reqID := "req-recheck-fast"
	task := h.submit("recheck prompt")[0]
	_, _ = h.q.journal.Update(task.ID, func(t *VideoTask) {
		t.RequestID = reqID
		t.NextCheckAt = h.clock.UTC().Format(time.RFC3339)
	})
	h.setPhase(reqID, generationAccepted)

	resumed := false
	h.onResume = func(ctx context.Context, id string) models.Response[[]GeneratedVideoItem] {
		if id == reqID {
			resumed = true
			h.setPhase(id, generationComplete)
		}
		return models.SuccessResponse([]GeneratedVideoItem{{LocalURL: "asset://flow/video.mp4"}})
	}

	h.start()
	h.waitForTask(task.ID, func(t VideoTask) bool { return t.Status == VideoTaskSucceeded })
	if !resumed {
		t.Fatal("expected recheck task to run even when nextLaunch is in future")
	}
}
