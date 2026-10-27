package flow

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/jobs"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
)

// ImageTaskStatus is a queued image task's place in its lifecycle.
type ImageTaskStatus string

const (
	ImageTaskQueued    ImageTaskStatus = "queued"
	ImageTaskRunning   ImageTaskStatus = "running"
	ImageTaskSucceeded ImageTaskStatus = "succeeded"
	ImageTaskFailed    ImageTaskStatus = "failed"
	ImageTaskCancelled ImageTaskStatus = "cancelled"
)

// AllImageTaskStatuses is bound to the frontend as a TypeScript enum.
var AllImageTaskStatuses = []struct {
	Value  ImageTaskStatus
	TSName string
}{
	{ImageTaskQueued, "Queued"}, {ImageTaskRunning, "Running"}, {ImageTaskSucceeded, "Succeeded"},
	{ImageTaskFailed, "Failed"}, {ImageTaskCancelled, "Cancelled"},
}

// ImageTaskReference is a reference image kept in the media store for a task;
// its bytes are read only when the request is sent, never kept in the journal.
type ImageTaskReference struct {
	LocalURL string `json:"localUrl"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// ImageTask is one prompt of the image batch queue (Ảnh → All). RequestID is
// the Flow request ID, written to the journal before the request leaves the
// app and kept while Flow may hold the job: such a task is only ever checked
// again under that ID, never sent with a new one.
type ImageTask struct {
	ID          string               `json:"id"`
	BatchID     string               `json:"batchId"`
	Prompt      string               `json:"prompt"`
	ModelID     string               `json:"modelId"`
	AspectRatio string               `json:"aspectRatio"`
	Quality     ImageQuality         `json:"quality"`
	Count       int                  `json:"count"`
	References  []ImageTaskReference `json:"references"`
	// OutputDir is the batch's own folder under the one the user chose
	// (imageQueue.batchFolder).
	OutputDir string `json:"outputDir"`
	// Index is the prompt's place in its batch (from 1); saved files are
	// numbered with it (media.OutputName).
	Index int `json:"index,omitempty"`
	// Held tasks wait in the queue until the user starts them.
	Held      bool            `json:"held,omitempty"`
	Status    ImageTaskStatus `json:"status"`
	Step      string          `json:"step,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	// Rechecks counts reads of a request Flow has not settled yet; it sets the
	// wait before the next one.
	Rechecks    int                  `json:"rechecks,omitempty"`
	NextCheckAt string               `json:"nextCheckAt,omitempty"`
	Results     []GeneratedImageItem `json:"results"`
	// SavedFiles are the results (local URLs) already copied to OutputDir, and
	// SavedPaths where those copies are on disk.
	SavedFiles []string `json:"savedFiles"`
	SavedPaths []string `json:"savedPaths,omitempty"`
	Error      string   `json:"error,omitempty"`
	CreatedAt  string   `json:"createdAt"`
	StartedAt  string   `json:"startedAt,omitempty"`
	FinishedAt string   `json:"finishedAt,omitempty"`
}

// JobID implements jobs.Record.
func (t ImageTask) JobID() string { return t.ID }

// Finished implements jobs.Record.
func (t ImageTask) Finished() bool {
	return t.Status == ImageTaskSucceeded || t.Status == ImageTaskFailed || t.Status == ImageTaskCancelled
}

// batchImagesPerPrompt is how many images one queue task asks Flow for: a
// batch makes one result per prompt; variants exist only in the single studio.
const batchImagesPerPrompt = 1

// ImageTaskRequest queues one task per non-empty prompt with shared settings.
type ImageTaskRequest struct {
	Prompts     []string             `json:"prompts"`
	ModelID     string               `json:"modelId"`
	AspectRatio string               `json:"aspectRatio"`
	Quality     ImageQuality         `json:"quality"`
	References  []ImageTaskReference `json:"references"`
	OutputDir   string               `json:"outputDir"`
	// Hold adds the tasks without starting them.
	Hold bool `json:"hold"`
}

// ImageQueueState is what the queue screen loads: the tasks and a warning
// when the journal was found corrupt at start.
type ImageQueueState struct {
	Tasks   []ImageTask `json:"tasks"`
	Warning string      `json:"warning,omitempty"`
}

const (
	// queuePumpInterval is how often the queue looks for work besides wake-ups.
	queuePumpInterval = time.Second
	// recheckFirst and recheckMax bound the wait before reading back a request
	// Flow has not settled.
	recheckFirst = 15 * time.Second
	recheckMax   = 5 * time.Minute
)

// imageQueue runs ImageTasks through the provider's own generation path, so
// rotation, the generation journal and resume work exactly as for one image.
type imageQueue struct {
	s       *Provider
	journal *jobs.Journal[ImageTask]
	cfg     func() config.FlowConfig

	mu         sync.Mutex
	ctx        context.Context
	stop       context.CancelFunc
	wg         sync.WaitGroup
	emit       func(events.Event, any)
	running    map[string]context.CancelFunc
	nextLaunch time.Time
	warning    string
	wake       chan struct{}

	// Seams replaced in tests.
	generate     func(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem]
	resume       func(ctx context.Context, id string) models.Response[[]GeneratedImageItem]
	requestPhase func(id string) (phase string, found bool)
	pendingJobs  func() ([]generationJob, error)
	saveImage    func(localURL string, t ImageTask) (string, error)
	now          func() time.Time
	gap          func(min, max int) time.Duration
}

func newImageQueue(s *Provider) *imageQueue {
	q := &imageQueue{
		s:       s,
		cfg:     func() config.FlowConfig { return s.cfg.Get().Flow },
		running: map[string]context.CancelFunc{},
		wake:    make(chan struct{}, 1),
		emit:    func(events.Event, any) {},
		now:     time.Now,
	}
	// A provider without settings (some unit tests) never starts its queue.
	path := ""
	if s.cfg != nil {
		path = filepath.Join(s.stateDir(""), "image-queue.json")
	}
	q.journal = jobs.New(path, jobs.Options[ImageTask]{
		Limit: func() int { return q.cfg().QueueMaxTasks },
		Normalize: func(t *ImageTask) {
			if t.References == nil {
				t.References = []ImageTaskReference{}
			}
			if t.Results == nil {
				t.Results = []GeneratedImageItem{}
			}
			if t.SavedFiles == nil {
				t.SavedFiles = []string{}
			}
		},
	})
	q.generate = s.GenerateImages
	q.resume = s.ResumeImageGeneration
	q.requestPhase = func(id string) (string, bool) {
		job, err := s.loadGenerationJob(id)
		if err != nil {
			return "", false
		}
		return job.Phase, true
	}
	q.pendingJobs = func() ([]generationJob, error) { return s.pendingGenerationJobs(generationJobImage) }
	q.saveImage = func(localURL string, t ImageTask) (string, error) {
		return s.media.SaveTo(localURL, t.OutputDir, media.OutputName(t.Index, "flow"))
	}
	// A batch keeps saving into the folder it was queued with, also after the
	// user picks another one or the app restarts.
	if s.media != nil {
		s.media.AllowRootsFrom(func() []string {
			var dirs []string
			for _, t := range q.journal.List() {
				if !t.Finished() && t.OutputDir != "" {
					dirs = append(dirs, t.OutputDir)
				}
			}
			return dirs
		})
	}
	q.gap = func(min, max int) time.Duration {
		if max <= min {
			return time.Duration(min) * time.Second
		}
		return time.Duration(min+rand.IntN(max-min+1)) * time.Second
	}
	return q
}

// Start loads the image queue, returns tasks interrupted by the last exit to
// the queue (their request is checked again under the same ID) and runs it
// until Close.
func (s *Provider) Start(ctx context.Context, emit func(events.Event, any)) {
	q := s.queue
	q.mu.Lock()
	q.ctx, q.stop = context.WithCancel(ctx)
	if emit != nil {
		q.emit = emit
	}
	q.mu.Unlock()
	if err := q.journal.Load(); err != nil {
		logger.Get().Warn("flow image queue reset", "error", err)
		if errors.Is(err, jobs.ErrCorrupt) {
			q.mu.Lock()
			q.warning = strings.TrimSuffix(err.Error(), ": "+jobs.ErrCorrupt.Error())
			q.mu.Unlock()
		}
	}
	for _, t := range q.journal.List() {
		if t.Status == ImageTaskRunning {
			_, _ = q.journal.Update(t.ID, func(t *ImageTask) { t.Status, t.Step = ImageTaskQueued, "" })
		}
	}
	q.adoptPending()
	q.wg.Add(1)
	go q.loop()
}

// adoptPending takes over unfinished All-mode generation jobs that no task
// holds: jobs the earlier in-page queue sent before the queue moved to Go. Each
// becomes a task carrying the job's request ID, so it is only checked again
// under that ID and never sent anew. Their images stay in the media store; the
// old queue's folder is unknown, so they are not copied anywhere.
func (q *imageQueue) adoptPending() {
	pending, err := q.pendingJobs()
	if err != nil {
		logger.Get().Warn("cannot list pending image jobs for the queue", "error", err)
		return
	}
	held := map[string]bool{}
	for _, t := range q.journal.List() {
		if t.RequestID != "" {
			held[strings.ToLower(t.RequestID)] = true
		}
	}
	var adopted []ImageTask
	batch, now := uuid.NewString(), q.stamp()
	for _, job := range pending {
		if mode, _ := normalizeImageProjectMode(job.ImageTemplate.ProjectMode); mode != ImageProjectModeAll || held[strings.ToLower(job.ID)] {
			continue
		}
		tpl := job.ImageTemplate
		adopted = append(adopted, ImageTask{
			ID: uuid.NewString(), BatchID: batch, Prompt: tpl.Prompt, ModelID: tpl.ModelID, AspectRatio: tpl.AspectRatio,
			Quality: tpl.Quality, Count: max(len(job.ClientIDs), 1), References: []ImageTaskReference{},
			Status: ImageTaskQueued, RequestID: job.ID, Step: "Job gửi trước đây, đang chờ kiểm tra lại; sẽ không gửi lại.",
			Results: []GeneratedImageItem{}, SavedFiles: []string{}, CreatedAt: now,
		})
	}
	if len(adopted) == 0 {
		return
	}
	if err := q.journal.Add(adopted...); err != nil {
		logger.Get().Warn("cannot adopt pending image jobs", "error", err)
		return
	}
	logger.Get().Info("adopted pending image jobs into the queue", "count", len(adopted))
}

// Close stops the image queue. Running requests stay in the journal and are
// checked again on the next start.
func (s *Provider) Close() {
	q := s.queue
	q.mu.Lock()
	stop := q.stop
	q.mu.Unlock()
	if stop != nil {
		stop()
	}
	q.wg.Wait()
}

func (q *imageQueue) lifetime() context.Context {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ctx == nil {
		return context.Background()
	}
	return q.ctx
}

func (q *imageQueue) loop() {
	defer q.wg.Done()
	ticker := time.NewTicker(queuePumpInterval)
	defer ticker.Stop()
	ctx := q.lifetime()
	for {
		q.safePump()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-q.wake:
		}
	}
}

// safePump runs one pump; a panic in it is logged and the loop carries on
// at the next tick instead of the queue stopping for the rest of the run.
func (q *imageQueue) safePump() {
	defer logger.Recover("hàng đợi ảnh Flow")
	q.pump()
}

func (q *imageQueue) trigger() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// pump starts due tasks while threads are free. A new send waits for the
// spacing reserved by the previous one; checking an earlier request does not.
func (q *imageQueue) pump() {
	ctx := q.lifetime()
	if ctx.Err() != nil {
		return
	}
	cfg := q.cfg()
	for {
		now := q.now()
		q.mu.Lock()
		free := cfg.QueueThreads - len(q.running)
		q.mu.Unlock()
		if free <= 0 {
			return
		}
		task, ok := q.nextDue(now)
		if !ok {
			return
		}
		fresh := q.isFreshSend(task)
		q.mu.Lock()
		if fresh && now.Before(q.nextLaunch) {
			q.mu.Unlock()
			return
		}
		if fresh {
			q.nextLaunch = now.Add(q.gap(cfg.QueueGapMinSeconds, cfg.QueueGapMaxSeconds))
		}
		taskCtx, cancel := context.WithCancel(ctx)
		q.running[task.ID] = cancel
		q.mu.Unlock()
		started, err := q.journal.Update(task.ID, func(t *ImageTask) {
			t.Status, t.Error, t.NextCheckAt = ImageTaskRunning, "", ""
			t.StartedAt = q.stamp()
			t.Step = "Đang gửi yêu cầu tới Flow…"
			if !fresh {
				t.Step = "Đang kiểm tra lại job trên Flow…"
			}
		})
		if err != nil {
			q.release(task.ID)
			return
		}
		q.publish(started)
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			defer q.release(task.ID)
			// The request ID is kept: a request that may have left the app is
			// checked again on retry, never sent anew.
			defer logger.Recover("tác vụ ảnh Flow", func(err error) { q.fail(task.ID, err.Error(), false) })
			q.execute(taskCtx, started)
		}()
	}
}

func (q *imageQueue) release(id string) {
	q.mu.Lock()
	if cancel, ok := q.running[id]; ok {
		cancel()
		delete(q.running, id)
	}
	q.mu.Unlock()
	q.trigger()
}

// nextDue is the oldest queued task that is not held and not waiting for a
// later check.
func (q *imageQueue) nextDue(now time.Time) (ImageTask, bool) {
	q.mu.Lock()
	running := make(map[string]bool, len(q.running))
	for id := range q.running {
		running[id] = true
	}
	q.mu.Unlock()
	for _, t := range q.journal.List() {
		if t.Status != ImageTaskQueued || t.Held || running[t.ID] {
			continue
		}
		if t.NextCheckAt != "" {
			if at, err := time.Parse(time.RFC3339, t.NextCheckAt); err == nil && now.Before(at) {
				continue
			}
		}
		return t, true
	}
	return ImageTask{}, false
}

// isFreshSend reports whether running t may send a new request to Flow, as
// opposed to reading back one Flow may already have, or only saving results.
func (q *imageQueue) isFreshSend(t ImageTask) bool {
	if q.onlySaving(t) {
		return false
	}
	if t.RequestID == "" {
		return true
	}
	_, found := q.requestPhase(t.RequestID)
	return !found
}

// onlySaving reports a task whose images were made but not all saved.
func (q *imageQueue) onlySaving(t ImageTask) bool {
	return t.RequestID == "" && t.OutputDir != "" && len(t.Results) > 0 && len(unsaved(t)) > 0
}

func (q *imageQueue) execute(ctx context.Context, t ImageTask) {
	if q.onlySaving(t) {
		q.finishWithResults(t, t.Results)
		return
	}
	if t.RequestID == "" {
		id := uuid.NewString()
		var err error
		// The ID is on disk before the request can leave the app.
		if t, err = q.journal.Update(t.ID, func(t *ImageTask) { t.RequestID = id }); err != nil {
			q.fail(t.ID, "Không ghi được hàng đợi: "+err.Error(), false)
			return
		}
	}
	var res models.Response[[]GeneratedImageItem]
	if _, found := q.requestPhase(t.RequestID); found {
		res = q.resume(ctx, t.RequestID)
	} else {
		req, err := q.request(t)
		if err != nil {
			// Nothing was sent: the next attempt may use a new ID.
			q.fail(t.ID, err.Error(), true)
			return
		}
		res = q.generate(ctx, req)
	}
	if ctx.Err() != nil {
		q.interrupted(t.ID)
		return
	}
	q.settle(t, res)
}

// request builds the generation request, reading the reference images from
// the media store.
func (q *imageQueue) request(t ImageTask) (GenerateImageRequest, error) {
	refs := make([]ReferenceImage, 0, len(t.References))
	for _, ref := range t.References {
		path, err := q.s.media.Path(ref.LocalURL)
		if err != nil {
			return GenerateImageRequest{}, fmt.Errorf("Ảnh tham chiếu %s không còn trong thư mục media.", ref.FileName)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return GenerateImageRequest{}, fmt.Errorf("Không đọc được ảnh tham chiếu %s: %v", ref.FileName, err)
		}
		refs = append(refs, ReferenceImage{Base64: base64.StdEncoding.EncodeToString(data), MimeType: ref.MimeType, FileName: ref.FileName})
	}
	return GenerateImageRequest{
		RequestID: t.RequestID, Prompt: t.Prompt, ModelID: t.ModelID, AspectRatio: t.AspectRatio,
		Quality: t.Quality, ProjectMode: ImageProjectModeAll, Count: t.Count, References: refs,
	}, nil
}

// settle records Flow's answer. A request Flow may still hold keeps its ID
// and is checked again later; only a definite answer frees the ID.
func (q *imageQueue) settle(t ImageTask, res models.Response[[]GeneratedImageItem]) {
	phase, found := q.requestPhase(t.RequestID)
	open := found && phase != generationComplete && phase != generationRejected
	if !res.Success {
		if open {
			q.recheck(t.ID, nil, res.Error)
			return
		}
		q.fail(t.ID, res.Error, true)
		return
	}
	items := res.Data
	if pending := slices.IndexFunc(items, func(i GeneratedImageItem) bool { return i.Pending }); pending >= 0 || open {
		message := "Flow đang xử lý job đã nhận."
		if pending >= 0 && items[pending].Error != "" {
			message = items[pending].Error
		}
		q.recheck(t.ID, items, message)
		return
	}
	made := slices.ContainsFunc(items, func(i GeneratedImageItem) bool { return i.LocalURL != "" })
	if !made {
		message := "Không tạo được ảnh."
		if i := slices.IndexFunc(items, func(i GeneratedImageItem) bool { return i.Error != "" }); i >= 0 {
			message = items[i].Error
		}
		updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
			t.Results, t.RequestID = items, ""
		})
		if err == nil {
			t = updated
		}
		q.fail(t.ID, message, true)
		return
	}
	updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
		t.Results, t.RequestID, t.Rechecks = items, "", 0
	})
	if err != nil {
		q.fail(t.ID, "Không ghi được hàng đợi: "+err.Error(), false)
		return
	}
	q.finishWithResults(updated, items)
}

// finishWithResults copies every made image not yet saved into the task's
// folder and finishes the task.
func (q *imageQueue) finishWithResults(t ImageTask, items []GeneratedImageItem) {
	saved := slices.Clone(t.SavedFiles)
	paths := slices.Clone(t.SavedPaths)
	var saveErr string
	for _, item := range items {
		// An adopted task has no folder: its images stay in the media store.
		if t.OutputDir == "" || item.LocalURL == "" || slices.Contains(saved, item.LocalURL) {
			continue
		}
		path, err := q.saveImage(item.LocalURL, t)
		if err != nil {
			if saveErr == "" {
				saveErr = err.Error()
			}
			continue
		}
		saved, paths = append(saved, item.LocalURL), append(paths, path)
	}
	updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
		t.SavedFiles, t.SavedPaths, t.Step, t.NextCheckAt = saved, paths, "", ""
		t.FinishedAt = q.stamp()
		if saveErr != "" {
			t.Status, t.Error = ImageTaskFailed, "Lỗi lưu ảnh: "+saveErr
			return
		}
		t.Status, t.Error = ImageTaskSucceeded, ""
		if i := slices.IndexFunc(items, func(i GeneratedImageItem) bool { return i.Error != "" }); i >= 0 {
			t.Error = items[i].Error
		}
	})
	if err == nil {
		q.publish(updated)
	}
}

// recheck returns a task whose request Flow has not settled to the queue, to
// be read back under the same ID after a growing wait.
func (q *imageQueue) recheck(id string, items []GeneratedImageItem, message string) {
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status != ImageTaskRunning {
			return
		}
		wait := recheckFirst << min(t.Rechecks, 6)
		if wait > recheckMax {
			wait = recheckMax
		}
		at := q.now().Add(wait)
		t.Rechecks++
		t.Status, t.NextCheckAt = ImageTaskQueued, at.UTC().Format(time.RFC3339)
		t.Step = fmt.Sprintf("%s Kiểm tra lại lúc %s; yêu cầu sẽ không được gửi lại.", strings.TrimSpace(message), at.Local().Format("15:04:05"))
		if items != nil {
			t.Results = items
		}
	})
	if err == nil {
		q.publish(updated)
	}
}

// fail ends a task. definite means Flow answered (or nothing was sent), so
// the request ID is freed and a retry sends a new request.
func (q *imageQueue) fail(id, message string, definite bool) {
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status != ImageTaskRunning {
			return
		}
		t.Status, t.Error, t.Step, t.NextCheckAt = ImageTaskFailed, message, "", ""
		t.FinishedAt = q.stamp()
		if definite {
			t.RequestID, t.Rechecks = "", 0
		}
	})
	if err == nil {
		q.publish(updated)
	}
}

// interrupted settles a task whose wait was stopped: by the user (cancelled)
// or by the app closing (left for the next start). The request ID stays, so
// an accepted job is checked again rather than paid for twice.
func (q *imageQueue) interrupted(id string) {
	closing := q.lifetime().Err() != nil
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status != ImageTaskRunning {
			return
		}
		if closing {
			t.Status, t.Step = ImageTaskQueued, ""
			return
		}
		t.Status, t.Step, t.FinishedAt = ImageTaskCancelled, "", q.stamp()
		if phase, found := q.requestPhase(t.RequestID); !found || phase == generationRejected {
			t.RequestID = ""
		}
	})
	if err == nil {
		q.publish(updated)
	}
}

func (q *imageQueue) stamp() string { return q.now().UTC().Format(time.RFC3339) }

func (q *imageQueue) publish(t ImageTask) {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	emit(events.EventFlowImageTaskUpdated, t)
}

func (q *imageQueue) changed() {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	emit(events.EventFlowImageTasksChanged, nil)
	q.trigger()
}

func unsaved(t ImageTask) []string {
	var out []string
	for _, item := range t.Results {
		if item.LocalURL != "" && !slices.Contains(t.SavedFiles, item.LocalURL) {
			out = append(out, item.LocalURL)
		}
	}
	return out
}

func queueFail[T any](err error, code models.Code) models.Response[T] {
	return models.ErrorResponse[T](err.Error(), code)
}

// SubmitImageTasks queues one task per non-empty prompt.
func (s *Provider) SubmitImageTasks(req ImageTaskRequest) models.Response[[]ImageTask] {
	q := s.queue
	prompts := make([]string, 0, len(req.Prompts))
	for _, p := range req.Prompts {
		if p = strings.TrimSpace(p); p != "" {
			prompts = append(prompts, p)
		}
	}
	if len(prompts) == 0 {
		return queueFail[[]ImageTask](errors.New("Chưa có prompt nào."), models.CodeInvalidArgument)
	}
	if strings.TrimSpace(req.ModelID) == "" {
		return queueFail[[]ImageTask](errors.New("Chưa chọn model ảnh."), models.CodeInvalidArgument)
	}
	quality, err := normalizeImageQuality(req.Quality)
	if err != nil {
		return queueFail[[]ImageTask](err, models.CodeInvalidArgument)
	}
	if strings.TrimSpace(req.OutputDir) == "" {
		return queueFail[[]ImageTask](errors.New("Chưa chọn thư mục lưu."), models.CodeInvalidArgument)
	}
	dir, err := s.media.CheckDir(req.OutputDir)
	if err != nil {
		return queueFail[[]ImageTask](err, models.CodeInvalidArgument)
	}
	refs, err := s.checkTaskReferences(req.References)
	if err != nil {
		return queueFail[[]ImageTask](err, models.CodeInvalidArgument)
	}
	dir, first := q.batchFolder(dir)
	batch, now := uuid.NewString(), q.stamp()
	tasks := make([]ImageTask, len(prompts))
	for i, p := range prompts {
		tasks[i] = ImageTask{
			ID: uuid.NewString(), BatchID: batch, Prompt: p, ModelID: req.ModelID, AspectRatio: req.AspectRatio,
			Quality: quality, Count: batchImagesPerPrompt, References: slices.Clone(refs), OutputDir: dir, Index: first + i, Held: req.Hold,
			Status: ImageTaskQueued, Results: []GeneratedImageItem{}, SavedFiles: []string{}, CreatedAt: now,
		}
	}
	if err := q.journal.Add(tasks...); err != nil {
		return queueFail[[]ImageTask](err, models.CodeInternal)
	}
	q.changed()
	return models.SuccessResponse(tasks)
}

// batchFolder picks the batch's folder under parent and the first number for
// the prompts being added (media.BatchFolder): an unfinished batch is joined,
// otherwise a new "snakemedia-flow-image-<date-time>" is opened.
func (q *imageQueue) batchFolder(parent string) (string, int) {
	var items []media.BatchItem
	for _, t := range q.journal.List() {
		if t.OutputDir != "" {
			items = append(items, media.BatchItem{Dir: t.OutputDir, Index: t.Index, CreatedAt: t.CreatedAt, Open: !t.Finished()})
		}
	}
	return media.BatchFolder(parent, "snakemedia-flow-image", q.now(), items)
}

// checkTaskReferences accepts only references already in the media store.
func (s *Provider) checkTaskReferences(refs []ImageTaskReference) ([]ImageTaskReference, error) {
	out := make([]ImageTaskReference, 0, len(refs))
	for _, ref := range refs {
		if _, err := s.media.Path(ref.LocalURL); err != nil {
			return nil, fmt.Errorf("Ảnh tham chiếu %s không hợp lệ.", ref.FileName)
		}
		out = append(out, ref)
	}
	return out, nil
}

// ListImageTasks returns the queue and any warning about its journal.
func (s *Provider) ListImageTasks() models.Response[ImageQueueState] {
	q := s.queue
	q.mu.Lock()
	warning := q.warning
	q.mu.Unlock()
	return models.SuccessResponse(ImageQueueState{Tasks: q.journal.List(), Warning: warning})
}

// selected returns the tasks with ids, or every task when ids is empty.
func (q *imageQueue) selected(ids []string) []ImageTask {
	all := q.journal.List()
	if len(ids) == 0 {
		return all
	}
	out := make([]ImageTask, 0, len(ids))
	for _, t := range all {
		if slices.Contains(ids, t.ID) {
			out = append(out, t)
		}
	}
	return out
}

// RunImageTasks releases held queued tasks (all when ids is empty).
func (s *Provider) RunImageTasks(ids []string) models.Response[int] {
	return s.setHeld(ids, false)
}

// HoldImageTasks keeps queued tasks from starting (all when ids is empty).
// Running tasks finish.
func (s *Provider) HoldImageTasks(ids []string) models.Response[int] {
	return s.setHeld(ids, true)
}

func (s *Provider) setHeld(ids []string, held bool) models.Response[int] {
	q := s.queue
	n := 0
	for _, t := range q.selected(ids) {
		if t.Status != ImageTaskQueued || t.Held == held {
			continue
		}
		if updated, err := q.journal.Update(t.ID, func(t *ImageTask) { t.Held = held }); err == nil {
			q.publish(updated)
			n++
		}
	}
	q.trigger()
	return models.SuccessResponse(n)
}

// CancelImageTask stops a queued or running task. A request Flow may already
// hold keeps its ID, so retrying the task checks that job instead of paying
// for a new one.
func (s *Provider) CancelImageTask(id string) models.Response[ImageTask] {
	q := s.queue
	q.mu.Lock()
	cancel, running := q.running[id]
	q.mu.Unlock()
	if running {
		cancel()
		t, _ := q.journal.Get(id)
		return models.SuccessResponse(t)
	}
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status == ImageTaskQueued {
			t.Status, t.Step, t.NextCheckAt, t.FinishedAt = ImageTaskCancelled, "", "", q.stamp()
		}
	})
	if err != nil {
		return queueFail[ImageTask](err, models.CodeNotFound)
	}
	q.publish(updated)
	return models.SuccessResponse(updated)
}

// EditImageTask changes the prompt of a task that is not running. A queued
// task keeps its place and hold; an ended one starts over with the new prompt
// as a new request (the user asked for a different image, so nothing Flow
// made for the old prompt is read back).
func (s *Provider) EditImageTask(id, prompt string) models.Response[ImageTask] {
	q := s.queue
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return queueFail[ImageTask](errors.New("Prompt không được để trống."), models.CodeInvalidArgument)
	}
	q.mu.Lock()
	_, running := q.running[id]
	q.mu.Unlock()
	t, ok := q.journal.Get(id)
	if !ok {
		return queueFail[ImageTask](errors.New("Tác vụ không tồn tại."), models.CodeNotFound)
	}
	if running || t.Status == ImageTaskRunning {
		return queueFail[ImageTask](errors.New("Tác vụ đang chạy, hãy hủy trước khi sửa."), models.CodeInvalidArgument)
	}
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		t.Prompt = prompt
		if t.Status == ImageTaskQueued {
			return
		}
		t.Status, t.Error, t.Step, t.NextCheckAt, t.FinishedAt = ImageTaskQueued, "", "", "", ""
		t.RequestID, t.Rechecks, t.Held = "", 0, false
		t.Results, t.SavedFiles, t.SavedPaths = []GeneratedImageItem{}, []string{}, nil
	})
	if err != nil {
		return queueFail[ImageTask](err, models.CodeInternal)
	}
	q.publish(updated)
	q.trigger()
	return models.SuccessResponse(updated)
}

// RetryImageTasks queues failed or cancelled tasks again. A task whose request
// Flow may still hold is read back under the same ID; a task whose images were
// made but not saved is only saved again; any other starts a new request.
// Retrying never sends a request Flow may already have accepted.
func (s *Provider) RetryImageTasks(ids []string) models.Response[int] {
	q := s.queue
	n := 0
	for _, t := range q.selected(ids) {
		if t.Status != ImageTaskFailed && t.Status != ImageTaskCancelled {
			continue
		}
		updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
			t.Status, t.Error, t.Step, t.NextCheckAt, t.FinishedAt = ImageTaskQueued, "", "", "", ""
			t.Rechecks, t.Held = 0, false
			if t.RequestID == "" && len(unsaved(*t)) == 0 {
				t.Results, t.SavedFiles, t.SavedPaths = []GeneratedImageItem{}, []string{}, nil
			}
		})
		if err == nil {
			q.publish(updated)
			n++
		}
	}
	q.trigger()
	return models.SuccessResponse(n)
}

// RemoveImageTasks deletes tasks that are not running. Saved images stay in
// their folders.
func (s *Provider) RemoveImageTasks(ids []string) models.Response[int] {
	q := s.queue
	q.mu.Lock()
	running := make(map[string]bool, len(q.running))
	for id := range q.running {
		running[id] = true
	}
	q.mu.Unlock()
	n, err := q.journal.RemoveWhere(func(t ImageTask) bool {
		return slices.Contains(ids, t.ID) && !running[t.ID] && t.Status != ImageTaskRunning
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.changed()
	return models.SuccessResponse(n)
}

// ClearFinishedImageTasks deletes the succeeded tasks. Their images stay in
// their folders.
func (s *Provider) ClearFinishedImageTasks() models.Response[int] {
	q := s.queue
	n, err := q.journal.RemoveWhere(func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.changed()
	return models.SuccessResponse(n)
}

// AddImageTaskReferences appends references to tasks that have not started a
// request (queued, failed or cancelled without a request ID). Changing a task
// whose request Flow may hold would make it a different, second request.
func (s *Provider) AddImageTaskReferences(ids []string, refs []ImageTaskReference) models.Response[int] {
	q := s.queue
	checked, err := s.checkTaskReferences(refs)
	if err != nil {
		return queueFail[int](err, models.CodeInvalidArgument)
	}
	n := 0
	for _, t := range q.selected(ids) {
		if t.Status == ImageTaskRunning || t.Status == ImageTaskSucceeded || t.RequestID != "" {
			continue
		}
		updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
			for _, ref := range checked {
				if !slices.ContainsFunc(t.References, func(r ImageTaskReference) bool { return r.LocalURL == ref.LocalURL }) {
					t.References = append(t.References, ref)
				}
			}
		})
		if err == nil {
			q.publish(updated)
			n++
		}
	}
	return models.SuccessResponse(n)
}

// ImportImageReferences copies image files chosen by the user into the media
// store, refusing types and sizes Flow does not take.
func (s *Provider) ImportImageReferences(paths []string) models.Response[[]ImageTaskReference] {
	accepted := referenceMimeTypes()
	limit := int64(maxReferenceImageBytes())
	out := make([]ImageTaskReference, 0, len(paths))
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		mimeType := mime.TypeByExtension(ext)
		if i := strings.IndexByte(mimeType, ';'); i >= 0 {
			mimeType = mimeType[:i]
		}
		if ext == ".jpg" || ext == ".jpeg" {
			mimeType = "image/jpeg"
		}
		if !slices.Contains(accepted, mimeType) {
			return queueFail[[]ImageTaskReference](fmt.Errorf("Flow không nhận định dạng của %s.", filepath.Base(path)), models.CodeInvalidArgument)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return queueFail[[]ImageTaskReference](fmt.Errorf("Không đọc được %s.", filepath.Base(path)), models.CodeInvalidArgument)
		}
		if info.Size() > limit {
			return queueFail[[]ImageTaskReference](fmt.Errorf("%s lớn hơn %d MB.", filepath.Base(path), limit>>20), models.CodeInvalidArgument)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return queueFail[[]ImageTaskReference](err, models.CodeInternal)
		}
		url, err := s.media.Put("flow-ref-"+uuid.NewString(), mimeType, ext, data)
		if err != nil {
			return queueFail[[]ImageTaskReference](err, models.CodeInternal)
		}
		out = append(out, ImageTaskReference{LocalURL: url, FileName: filepath.Base(path), MimeType: mimeType, Size: info.Size()})
	}
	return models.SuccessResponse(out)
}

// ImportProjectImageReference copies one image of a Flow project into the
// media store for use as a queue reference.
func (s *Provider) ImportProjectImageReference(ctx context.Context, accountID, projectID string, mode ImageProjectMode, mediaID string) models.Response[ImageTaskReference] {
	res := s.GetProjectImageReference(ctx, accountID, projectID, mode, mediaID)
	if !res.Success {
		return models.ErrorResponse[ImageTaskReference](res.Error, res.Code)
	}
	data, err := base64.StdEncoding.DecodeString(res.Data.Base64)
	if err != nil {
		return queueFail[ImageTaskReference](err, models.CodeInternal)
	}
	exts, _ := mime.ExtensionsByType(res.Data.MimeType)
	ext := ".png"
	if len(exts) > 0 {
		ext = exts[0]
	}
	url, err := s.media.Put("flow-ref-"+uuid.NewString(), res.Data.MimeType, ext, data)
	if err != nil {
		return queueFail[ImageTaskReference](err, models.CodeInternal)
	}
	return models.SuccessResponse(ImageTaskReference{LocalURL: url, FileName: res.Data.FileName, MimeType: res.Data.MimeType, Size: int64(len(data))})
}

// ReferenceFilePatterns are the file-dialog patterns of the image types Flow
// takes as references.
func (s *Provider) ReferenceFilePatterns() []string {
	var out []string
	for _, mimeType := range referenceMimeTypes() {
		exts, _ := mime.ExtensionsByType(mimeType)
		if mimeType == "image/jpeg" {
			exts = []string{".jpg", ".jpeg"}
		}
		for _, ext := range exts {
			if pattern := "*" + ext; !slices.Contains(out, pattern) {
				out = append(out, pattern)
			}
		}
	}
	return out
}
