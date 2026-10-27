package flow

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
)

// queueHarness drives the image queue with fake Flow calls: generate records
// the request and, like the real path, writes a generation journal phase
// before answering when told to.
type queueHarness struct {
	t   *testing.T
	s   *Provider
	q   *imageQueue
	dir string

	mu        sync.Mutex
	clock     time.Time
	phases    map[string]string
	generated []GenerateImageRequest
	resumed   []string
	saved     []string
	onGen     func(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem]
	onResume  func(ctx context.Context, id string) models.Response[[]GeneratedImageItem]
}

func newQueueHarness(t *testing.T) *queueHarness {
	t.Helper()
	s := testFlowService(testAccountService(t))
	h := &queueHarness{t: t, s: s, q: s.queue, dir: t.TempDir(), clock: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), phases: map[string]string{}}
	s.media.AllowRoot(h.dir)
	cfg := config.DefaultFlowConfig()
	cfg.QueueThreads, cfg.QueueGapMinSeconds, cfg.QueueGapMaxSeconds = 2, 0, 0
	h.q.cfg = func() config.FlowConfig { return cfg }
	h.q.now = func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.clock }
	h.q.gap = func(int, int) time.Duration { return 0 }
	h.q.requestPhase = func(id string) (string, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		phase, ok := h.phases[id]
		return phase, ok
	}
	h.q.generate = func(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		h.mu.Lock()
		h.generated = append(h.generated, req)
		gen := h.onGen
		h.mu.Unlock()
		return gen(ctx, req)
	}
	h.q.resume = func(ctx context.Context, id string) models.Response[[]GeneratedImageItem] {
		h.mu.Lock()
		h.resumed = append(h.resumed, id)
		resume := h.onResume
		h.mu.Unlock()
		return resume(ctx, id)
	}
	h.q.pendingJobs = func() ([]generationJob, error) { return nil, nil }
	h.q.saveImage = func(localURL, outputDir, batchID string) models.Response[media.SaveResult] {
		h.mu.Lock()
		h.saved = append(h.saved, localURL)
		h.mu.Unlock()
		return models.SuccessResponse(media.SaveResult{Path: filepath.Join(outputDir, filepath.Base(localURL))})
	}
	return h
}

func (h *queueHarness) setPhase(id, phase string) {
	h.mu.Lock()
	h.phases[id] = phase
	h.mu.Unlock()
}

func (h *queueHarness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.s.Start(ctx, func(events.Event, any) {})
	h.t.Cleanup(func() { cancel(); h.s.Close() })
}

func (h *queueHarness) submit(prompts ...string) []ImageTask {
	h.t.Helper()
	res := h.s.SubmitImageTasks(ImageTaskRequest{Prompts: prompts, ModelID: "m", AspectRatio: "IMAGE_ASPECT_RATIO_SQUARE", Count: 1, OutputDir: h.dir})
	if !res.Success {
		h.t.Fatalf("submit: %s", res.Error)
	}
	return res.Data
}

func (h *queueHarness) waitFor(id string, ok func(ImageTask) bool) ImageTask {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if task, found := h.q.journal.Get(id); found && ok(task) {
			return task
		}
		h.q.trigger()
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := h.q.journal.Get(id)
	h.t.Fatalf("task never reached the expected state: %+v", task)
	return ImageTask{}
}

func made(id string) []GeneratedImageItem {
	return []GeneratedImageItem{{JobID: id, ID: "img-" + id, LocalURL: "/flow-media/" + id + ".png"}}
}

func TestQueueSendsSavesAndFreesTheRequestID(t *testing.T) {
	h := newQueueHarness(t)
	ref, err := h.s.media.Put("ref", "image/png", ".png", []byte("png-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	h.onGen = func(_ context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		h.setPhase(req.RequestID, generationComplete)
		return models.SuccessResponse(made(req.RequestID))
	}
	h.start()
	res := h.s.SubmitImageTasks(ImageTaskRequest{
		Prompts: []string{"a cat", " "}, ModelID: "m", Count: 1, OutputDir: h.dir,
		References: []ImageTaskReference{{LocalURL: ref, FileName: "ref.png", MimeType: "image/png"}},
	})
	if !res.Success || len(res.Data) != 1 {
		t.Fatalf("submit %+v", res)
	}
	done := h.waitFor(res.Data[0].ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	if done.RequestID != "" || len(done.SavedFiles) != 1 || len(done.Results) != 1 {
		t.Fatalf("finished task %+v", done)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.generated) != 1 || len(h.generated[0].References) != 1 || h.generated[0].References[0].Base64 == "" {
		t.Fatalf("request did not carry the reference bytes: %+v", h.generated)
	}
	if h.generated[0].ProjectMode != ImageProjectModeAll {
		t.Fatal("queue requests belong to the All project")
	}
	if len(h.saved) != 1 {
		t.Fatalf("saved %v", h.saved)
	}
}

func TestQueueResumesAnInterruptedRequestUnderTheSameID(t *testing.T) {
	h := newQueueHarness(t)
	if err := h.q.journal.Load(); err != nil {
		t.Fatal(err)
	}
	const rid = "11111111-1111-4111-8111-111111111111"
	task := ImageTask{ID: "22222222-2222-4222-8222-222222222222", BatchID: "33333333-3333-4333-8333-333333333333", Prompt: "p", ModelID: "m", Count: 1, OutputDir: h.dir,
		Status: ImageTaskRunning, RequestID: rid, References: []ImageTaskReference{}, Results: []GeneratedImageItem{}, SavedFiles: []string{}}
	if err := h.q.journal.Add(task); err != nil {
		t.Fatal(err)
	}
	h.setPhase(rid, generationSubmitting)
	h.onGen = func(context.Context, GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		t.Error("an interrupted request must never be sent again")
		return models.ErrorResponse[[]GeneratedImageItem]("no", models.CodeInternal)
	}
	h.onResume = func(_ context.Context, id string) models.Response[[]GeneratedImageItem] {
		h.setPhase(id, generationComplete)
		return models.SuccessResponse(made(id))
	}
	h.start()
	done := h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.resumed) != 1 || h.resumed[0] != rid || done.Results[0].JobID != rid {
		t.Fatalf("resumed %v, task %+v", h.resumed, done)
	}
}

func TestQueueDefiniteFailureSendsANewRequestOnRetry(t *testing.T) {
	h := newQueueHarness(t)
	h.onGen = func(context.Context, GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		return models.ErrorResponse[[]GeneratedImageItem]("Flow từ chối prompt", models.CodeUnavailable)
	}
	h.start()
	task := h.submit("a dog")[0]
	failed := h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskFailed })
	if failed.RequestID != "" || failed.Error != "Flow từ chối prompt" {
		t.Fatalf("definite failure must free the request ID: %+v", failed)
	}
	h.onGen = func(_ context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		h.setPhase(req.RequestID, generationComplete)
		return models.SuccessResponse(made(req.RequestID))
	}
	if n := h.s.RetryImageTasks([]string{task.ID}); n.Data != 1 {
		t.Fatalf("retry %+v", n)
	}
	h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.generated) != 2 || h.generated[0].RequestID == h.generated[1].RequestID {
		t.Fatalf("retry after a definite answer needs a new request ID: %+v", h.generated)
	}
}

func TestQueueUnknownOutcomeIsCheckedNotResent(t *testing.T) {
	h := newQueueHarness(t)
	h.onGen = func(_ context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		// The request reached Flow (journal written) but the answer was lost.
		h.setPhase(req.RequestID, generationSubmitting)
		return models.ErrorResponse[[]GeneratedImageItem]("bridge lost", models.CodeUnavailable)
	}
	h.onResume = func(_ context.Context, id string) models.Response[[]GeneratedImageItem] {
		h.setPhase(id, generationComplete)
		return models.SuccessResponse(made(id))
	}
	h.start()
	task := h.submit("a bird")[0]
	waiting := h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskQueued && t.NextCheckAt != "" })
	if waiting.RequestID == "" || waiting.Rechecks != 1 {
		t.Fatalf("unknown outcome must keep the request ID: %+v", waiting)
	}
	h.mu.Lock()
	h.clock = h.clock.Add(time.Minute)
	h.mu.Unlock()
	done := h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.generated) != 1 || len(h.resumed) != 1 || h.resumed[0] != waiting.RequestID || done.RequestID != "" {
		t.Fatalf("generated %d, resumed %v, task %+v", len(h.generated), h.resumed, done)
	}
}

func TestQueueCancelKeepsAnAcceptedRequestForRetry(t *testing.T) {
	h := newQueueHarness(t)
	sent := make(chan string, 1)
	h.onGen = func(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		h.setPhase(req.RequestID, generationSubmitting)
		sent <- req.RequestID
		<-ctx.Done()
		return models.ErrorResponse[[]GeneratedImageItem](ctx.Err().Error(), models.CodeUnavailable)
	}
	h.onResume = func(_ context.Context, id string) models.Response[[]GeneratedImageItem] {
		h.setPhase(id, generationComplete)
		return models.SuccessResponse(made(id))
	}
	h.start()
	held := h.s.SubmitImageTasks(ImageTaskRequest{Prompts: []string{"held"}, ModelID: "m", Count: 1, OutputDir: h.dir, Hold: true}).Data[0]
	if res := h.s.CancelImageTask(held.ID); res.Data.Status != ImageTaskCancelled {
		t.Fatalf("queued task not cancelled: %+v", res)
	}
	task := h.submit("slow")[0]
	rid := <-sent
	h.s.CancelImageTask(task.ID)
	cancelled := h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskCancelled })
	if cancelled.RequestID != rid {
		t.Fatalf("a request Flow may hold must keep its ID: %+v", cancelled)
	}
	h.s.RetryImageTasks([]string{task.ID})
	h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.generated) != 1 || len(h.resumed) != 1 || h.resumed[0] != rid {
		t.Fatalf("retry must check the same job: generated %d resumed %v", len(h.generated), h.resumed)
	}
}

func TestQueueHoldRunAndSaveOnlyRetry(t *testing.T) {
	h := newQueueHarness(t)
	saveFails := true
	h.q.saveImage = func(localURL, outputDir, batchID string) models.Response[media.SaveResult] {
		h.mu.Lock()
		defer h.mu.Unlock()
		if saveFails {
			return models.ErrorResponse[media.SaveResult]("ổ đầy", models.CodeInternal)
		}
		h.saved = append(h.saved, localURL)
		return models.SuccessResponse(media.SaveResult{Path: localURL})
	}
	h.onGen = func(_ context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		h.setPhase(req.RequestID, generationComplete)
		return models.SuccessResponse(made(req.RequestID))
	}
	h.start()
	task := h.s.SubmitImageTasks(ImageTaskRequest{Prompts: []string{"x"}, ModelID: "m", Count: 1, OutputDir: h.dir, Hold: true}).Data[0]
	time.Sleep(50 * time.Millisecond)
	if got, _ := h.q.journal.Get(task.ID); got.Status != ImageTaskQueued {
		t.Fatalf("a held task must not start: %+v", got)
	}
	if n := h.s.RunImageTasks(nil); n.Data != 1 {
		t.Fatalf("run %+v", n)
	}
	failed := h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskFailed })
	if len(failed.Results) != 1 || failed.RequestID != "" {
		t.Fatalf("save failure keeps the made images: %+v", failed)
	}
	h.mu.Lock()
	saveFails = false
	h.mu.Unlock()
	h.s.RetryImageTasks(nil)
	h.waitFor(task.ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.generated) != 1 || len(h.saved) != 1 {
		t.Fatalf("a save-only retry must not generate again: generated %d saved %v", len(h.generated), h.saved)
	}
}

func TestQueueRejectsOutputOutsideAllowedFolders(t *testing.T) {
	h := newQueueHarness(t)
	outside := filepath.Join(os.TempDir(), "not-allowed-"+t.Name())
	res := h.s.SubmitImageTasks(ImageTaskRequest{Prompts: []string{"x"}, ModelID: "m", Count: 1, OutputDir: outside})
	if res.Success {
		t.Fatal("an output folder the user never chose must be refused")
	}
}

func TestQueueAdoptsPendingAllModeJobsWithoutResending(t *testing.T) {
	h := newQueueHarness(t)
	const all, single, held = "44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666"
	if err := h.q.journal.Load(); err != nil {
		t.Fatal(err)
	}
	owner := ImageTask{ID: "77777777-7777-4777-8777-777777777777", BatchID: "88888888-8888-4888-8888-888888888888", Prompt: "p", ModelID: "m", Count: 1, OutputDir: h.dir,
		Status: ImageTaskQueued, Held: true, RequestID: held, References: []ImageTaskReference{}, Results: []GeneratedImageItem{}, SavedFiles: []string{}}
	if err := h.q.journal.Add(owner); err != nil {
		t.Fatal(err)
	}
	h.q.pendingJobs = func() ([]generationJob, error) {
		return []generationJob{
			{ID: all, Kind: generationJobImage, Phase: generationAccepted, ClientIDs: []string{"A", "B"},
				ImageTemplate: imageGenerationTemplate{Prompt: "old prompt", ModelID: "m", ProjectMode: ImageProjectModeAll}},
			{ID: single, Kind: generationJobImage, Phase: generationAccepted, ImageTemplate: imageGenerationTemplate{Prompt: "single"}},
			{ID: held, Kind: generationJobImage, Phase: generationAccepted, ImageTemplate: imageGenerationTemplate{ProjectMode: ImageProjectModeAll}},
		}, nil
	}
	h.setPhase(all, generationAccepted)
	h.onGen = func(context.Context, GenerateImageRequest) models.Response[[]GeneratedImageItem] {
		t.Error("an adopted job must never be sent again")
		return models.ErrorResponse[[]GeneratedImageItem]("no", models.CodeInternal)
	}
	h.onResume = func(_ context.Context, id string) models.Response[[]GeneratedImageItem] {
		h.setPhase(id, generationComplete)
		return models.SuccessResponse(made(id))
	}
	h.start()
	var adopted ImageTask
	for _, task := range h.q.journal.List() {
		if task.RequestID == all || (task.Prompt == "old prompt") {
			adopted = task
		}
		if task.ID != owner.ID && task.RequestID != all && task.Prompt != "old prompt" {
			t.Fatalf("only the unowned All-mode job is adopted, got %+v", task)
		}
	}
	if adopted.ID == "" || adopted.Count != 2 {
		t.Fatalf("adopted task %+v", adopted)
	}
	done := h.waitFor(adopted.ID, func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.resumed) != 1 || h.resumed[0] != all || len(done.Results) != 1 || len(h.saved) != 0 {
		t.Fatalf("resumed %v saved %v task %+v", h.resumed, h.saved, done)
	}
}
