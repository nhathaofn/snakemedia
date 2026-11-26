package flow

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
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
	// Held tasks wait in the queue until the user starts them. Paused marks
	// tasks paused after running (shown under Tạm dừng, vs Chờ chạy).
	Held      bool            `json:"held,omitempty"`
	Paused    bool            `json:"paused,omitempty"`
	Status    ImageTaskStatus `json:"status"`
	Step      string          `json:"step,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	// Rechecks counts reads of a request Flow has not settled yet; it sets the
	// wait before the next one.
	Rechecks int `json:"rechecks,omitempty"`
	// RefusedBy are the accounts Google refused this prompt for unusual
	// activity; the task goes back to the queue and the rotation picks among
	// the other accounts, so refused prompts spread over them.
	RefusedBy   []string             `json:"refusedBy,omitempty"`
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

// ImageTaskSettingsRequest updates model, aspect ratio and/or quality on
// queued tasks (or the named IDs) that have not sent a request to Flow yet.
// Empty fields leave the task's existing value intact.
type ImageTaskSettingsRequest struct {
	IDs         []string     `json:"ids,omitempty"`
	ModelID     string       `json:"modelId,omitempty"`
	AspectRatio string       `json:"aspectRatio,omitempty"`
	Quality     ImageQuality `json:"quality,omitempty"`
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
	submitMu   sync.Mutex
	ctx        context.Context
	stop       context.CancelFunc
	wg         sync.WaitGroup
	emit       func(events.Event, any)
	running    map[string]context.CancelFunc
	nextLaunch time.Time
	warning    string
	wake       chan struct{}

	// Seams replaced in tests.
	generate func(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem]
	resume   func(ctx context.Context, id string) models.Response[[]GeneratedImageItem]
	// requestPhase reads a request's journal. err is any failure other than
	// "no journal": the request may have been sent, so it is never fresh.
	requestPhase func(id string) (phase string, found bool, err error)
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
		Transient: onlyImageStepChanged,
		RetireKey: func(t ImageTask) string { return imageRequestKey(t.RequestID) },
	})
	q.generate = s.GenerateImages
	q.resume = s.ResumeImageGeneration
	q.requestPhase = func(id string) (string, bool, error) {
		job, err := s.loadGenerationJob(id)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return job.Phase, true, nil
	}
	q.pendingJobs = func() ([]generationJob, error) { return s.pendingGenerationJobs(generationJobImage) }
	q.saveImage = func(localURL string, t ImageTask) (string, error) {
		return s.media.SaveToOnce(localURL, t.OutputDir, media.PlannedOutputName(t.Index, "flow", t.ID, localURL))
	}
	// A batch keeps saving into the folder it was queued with. Keep completed
	// batch paths revealable after a folder change or an app restart.
	if s.media != nil {
		s.media.AllowRootsFrom(func() []string {
			var dirs []string
			for _, t := range q.journal.List() {
				if t.OutputDir != "" {
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
	if s.cfg != nil {
		s.cfg.OnChange(q.trigger)
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
		if errors.Is(err, jobs.ErrCorrupt) || errors.Is(err, jobs.ErrNewerFormat) {
			q.mu.Lock()
			q.warning = strings.TrimSuffix(err.Error(), ": "+jobs.ErrCorrupt.Error())
			q.mu.Unlock()
		}
	}
	q.adoptPending()
	q.wg.Add(1)
	go q.loop()
}

func imageRequestKey(id string) string {
	if id == "" {
		return ""
	}
	return "request:" + strings.ToLower(id)
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
		if mode, _ := normalizeImageProjectMode(job.ImageTemplate.ProjectMode); mode != ImageProjectModeAll || job.ImageTemplate.QueueOwned ||
			held[strings.ToLower(job.ID)] || q.journal.Retired(imageRequestKey(job.ID)) {
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
	s.creditRefreshMu.Lock()
	s.closingCredits = true
	s.creditRefreshMu.Unlock()
	q.mu.Lock()
	stop := q.stop
	q.mu.Unlock()
	if stop != nil {
		stop()
	}
	q.wg.Wait()
	s.creditRefreshWG.Wait()
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
	timer := time.NewTimer(queuePumpInterval)
	defer timer.Stop()
	ctx := q.lifetime()
	for {
		q.safePump()
		timer.Stop()
		var alarm <-chan time.Time
		if delay, ok := q.nextWake(); ok {
			timer.Reset(delay)
			alarm = timer.C
		}
		select {
		case <-ctx.Done():
			return
		case <-alarm:
		case <-q.wake:
		}
	}
}

// A queue with nothing eligible sleeps until a submission, worker exit or
// config change. Delayed jobs set the next alarm themselves.
func (q *imageQueue) nextWake() (time.Duration, bool) {
	cfg := q.cfg()
	q.mu.Lock()
	free, nextLaunch := cfg.QueueThreads-len(q.running), q.nextLaunch
	q.mu.Unlock()
	if free <= 0 {
		return 0, false
	}
	now := q.now()
	var earliest time.Time
	for _, task := range q.journal.ListActive() {
		if task.Status == ImageTaskRunning && !q.isRunning(task.ID) {
			return queuePumpInterval, true
		}
		if task.Held || task.Status != ImageTaskQueued {
			continue
		}
		at, _ := time.Parse(time.RFC3339, task.NextCheckAt)
		if !at.After(now) && nextLaunch.After(now) && q.isFreshSend(task) {
			at = nextLaunch
		}
		if !at.After(now) {
			return queuePumpInterval, true
		}
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	}
	return earliest.Sub(now), !earliest.IsZero()
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
	active := q.journal.ListActive()
	if q.recoverOrphanedTasks(active) {
		active = q.journal.ListActive()
	}
	cfg := q.cfg()
	for _, task := range active {
		if ctx.Err() != nil {
			return
		}
		now := q.now()
		q.mu.Lock()
		free := cfg.QueueThreads - len(q.running)
		allowFresh := !now.Before(q.nextLaunch)
		q.mu.Unlock()
		if free <= 0 {
			return
		}
		if !imageTaskDue(task, now) || q.isRunning(task.ID) {
			continue
		}
		fresh := q.isFreshSend(task)
		if fresh && !allowFresh {
			continue
		}
		q.mu.Lock()
		taskCtx, cancel := context.WithCancel(ctx)
		q.running[task.ID] = cancel
		q.mu.Unlock()
		claimed, err := q.journal.UpdateMany([]string{task.ID}, func(t *ImageTask) bool {
			if !imageTaskDue(*t, now) {
				return false
			}
			t.Status, t.Error, t.NextCheckAt = ImageTaskRunning, "", ""
			t.StartedAt = q.stamp()
			t.Step = "Đang gửi yêu cầu tới Flow…"
			if !fresh {
				t.Step = "Đang kiểm tra lại job trên Flow…"
			}
			return true
		})
		if err != nil {
			q.release(task.ID)
			return
		}
		if len(claimed) == 0 {
			q.release(task.ID)
			continue
		}
		started := claimed[0]
		if fresh {
			q.mu.Lock()
			q.nextLaunch = now.Add(q.gap(cfg.QueueGapMinSeconds, cfg.QueueGapMaxSeconds))
			q.mu.Unlock()
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

// recoverOrphanedTasks requeues interrupted work at startup and after a
// worker could not persist its final transition. Request IDs and saved
// results stay intact so reconciliation and saving can continue safely.
func (q *imageQueue) recoverOrphanedTasks(active []ImageTask) bool {
	var orphaned []string
	for _, task := range active {
		if task.Status == ImageTaskRunning && !q.isRunning(task.ID) {
			orphaned = append(orphaned, task.ID)
		}
	}
	if len(orphaned) == 0 {
		return false
	}
	recovered, err := q.journal.UpdateMany(orphaned, func(t *ImageTask) bool {
		if t.Status != ImageTaskRunning {
			return false
		}
		t.Status, t.Step = ImageTaskQueued, ""
		return true
	})
	if err != nil {
		logger.Get().Warn("cannot recover interrupted Flow image tasks", "error", err)
		return false
	}
	for _, task := range recovered {
		q.publish(task)
	}
	return len(recovered) > 0
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

func imageTaskDue(t ImageTask, now time.Time) bool {
	if t.Status != ImageTaskQueued || t.Held {
		return false
	}
	at, err := time.Parse(time.RFC3339, t.NextCheckAt)
	return err != nil || !now.Before(at)
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
	_, found, err := q.requestPhase(t.RequestID)
	return !found && err == nil
}

// onlySaving reports a task with committed results to finalize. Even an
// adopted task without an output folder must never send again from this state.
func (q *imageQueue) onlySaving(t ImageTask) bool {
	return t.RequestID == "" && slices.ContainsFunc(t.Results, func(item GeneratedImageItem) bool {
		return item.LocalURL != ""
	})
}

func (q *imageQueue) execute(ctx context.Context, t ImageTask) {
	if ctx.Err() != nil {
		q.interrupted(t.ID)
		return
	}
	if q.onlySaving(t) {
		q.finishWithResults(ctx, t, t.Results)
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
	_, found, err := q.requestPhase(t.RequestID)
	if err != nil {
		q.unknown(t.ID, err)
		return
	}
	if found {
		res = q.resume(ctx, t.RequestID)
	} else {
		req, err := q.request(t)
		if err != nil {
			// Nothing was sent: the next attempt may use a new ID.
			q.fail(t.ID, err.Error(), true)
			return
		}
		refused := ""
		req.avoid, req.onRefused = t.RefusedBy, func(accountID string) { refused = accountID }
		res = q.generate(ctx, req)
		if refused != "" && ctx.Err() == nil {
			q.passOn(t.ID, refused)
			return
		}
	}
	if ctx.Err() != nil {
		q.interrupted(t.ID)
		return
	}
	q.settle(ctx, t, res)
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
		Quality: t.Quality, ProjectMode: ImageProjectModeAll, Count: t.Count, References: refs, queueOwned: true,
	}, nil
}

// settle records Flow's answer. A request Flow may still hold keeps its ID
// and is checked again later; only a definite answer frees the ID.
func (q *imageQueue) settle(ctx context.Context, t ImageTask, res models.Response[[]GeneratedImageItem]) {
	phase, found, phaseErr := q.requestPhase(t.RequestID)
	open := found && phase != generationComplete && phase != generationRejected
	if !res.Success {
		if phaseErr != nil {
			q.unknown(t.ID, phaseErr)
			return
		}
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
		q.finishFailure(t.ID, message, true, items)
		return
	}
	updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
		t.Results, t.RequestID, t.Rechecks = items, "", 0
	})
	if err != nil {
		q.fail(t.ID, "Không ghi được hàng đợi: "+err.Error(), false)
		return
	}
	q.finishWithResults(ctx, updated, items)
}

// finishWithResults copies every made image not yet saved into the task's
// folder and finishes the task.
func (q *imageQueue) finishWithResults(ctx context.Context, t ImageTask, items []GeneratedImageItem) {
	saved := slices.Clone(t.SavedFiles)
	paths := slices.Clone(t.SavedPaths)
	var saveErr string
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
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
	closing := q.lifetime().Err() != nil
	updated, err := q.journal.Update(t.ID, func(t *ImageTask) {
		t.SavedFiles, t.SavedPaths, t.Step, t.NextCheckAt = saved, paths, "", ""
		if ctx.Err() != nil {
			q.markInterrupted(t, closing)
			return
		}
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

// passOn puts a task Google refused on one account for unusual activity back
// in the queue for the other accounts. The refusal is definite and unpaid, so
// the next attempt is a new request (new ID); it waits for the queue's gap
// like any new send.
func (q *imageQueue) passOn(id, accountID string) {
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status != ImageTaskRunning {
			return
		}
		t.Status, t.Error, t.NextCheckAt = ImageTaskQueued, "", ""
		t.Step = "Tài khoản vừa bị Google tạm chặn; chuyển sang tài khoản khác."
		t.RequestID, t.Rechecks = "", 0
		if !slices.Contains(t.RefusedBy, accountID) {
			t.RefusedBy = append(t.RefusedBy, accountID)
		}
	})
	if err == nil {
		q.publish(updated)
	}
}

// unknown settles a task whose request journal cannot be read: the request
// may have reached Flow, so its ID is kept and nothing is sent again. A
// quarantined journal will not come back, so the task fails; any other read
// error is checked again later.
func (q *imageQueue) unknown(id string, err error) {
	if errors.Is(err, errJournalQuarantined) {
		q.fail(id, err.Error(), false)
		return
	}
	q.recheck(id, nil, "Không đọc được nhật ký yêu cầu: "+err.Error())
}

// fail ends a task. definite means Flow answered (or nothing was sent), so
// the request ID is freed and a retry sends a new request.
func (q *imageQueue) fail(id, message string, definite bool) {
	q.finishFailure(id, message, definite, nil)
}

func (q *imageQueue) finishFailure(id, message string, definite bool, results []GeneratedImageItem) {
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status != ImageTaskRunning {
			return
		}
		if results != nil {
			t.Results = results
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
		q.markInterrupted(t, closing)
	})
	if err == nil {
		q.publish(updated)
	}
}

func (q *imageQueue) markInterrupted(t *ImageTask, closing bool) {
	t.Step = ""
	if closing {
		t.Status = ImageTaskQueued
		return
	}
	t.Status, t.FinishedAt = ImageTaskCancelled, q.stamp()
	if t.RequestID != "" {
		if phase, found, err := q.requestPhase(t.RequestID); err == nil && (!found || phase == generationRejected) {
			t.RequestID = ""
		}
	}
}

// notify sends an event through the emitter the provider was started with.
func (q *imageQueue) notify(event events.Event, payload any) {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	emit(event, payload)
}

func (q *imageQueue) stamp() string { return q.now().UTC().Format(time.RFC3339) }

func (q *imageQueue) publish(t ImageTask) {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	emit(events.EventFlowImageTaskUpdated, t)
}

// changed reports tasks added and removed at once: listeners apply them to
// their copy instead of reloading the whole queue.
func (q *imageQueue) changed(added []ImageTask, removed []string) {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	if len(removed) > 0 {
		emit(events.EventFlowImageTasksRemoved, removed)
	}
	if len(added) > 0 {
		emit(events.EventFlowImageTasksAdded, added)
	}
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
