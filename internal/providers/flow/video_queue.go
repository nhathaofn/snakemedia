package flow

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
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

// VideoTaskStatus is a queued video task's place in its lifecycle.
type VideoTaskStatus string

const (
	VideoTaskQueued    VideoTaskStatus = "queued"
	VideoTaskRunning   VideoTaskStatus = "running"
	VideoTaskSucceeded VideoTaskStatus = "succeeded"
	VideoTaskFailed    VideoTaskStatus = "failed"
	VideoTaskCancelled VideoTaskStatus = "cancelled"
)

// AllVideoTaskStatuses is bound to the frontend as a TypeScript enum.
var AllVideoTaskStatuses = []struct {
	Value  VideoTaskStatus
	TSName string
}{
	{VideoTaskQueued, "Queued"}, {VideoTaskRunning, "Running"}, {VideoTaskSucceeded, "Succeeded"},
	{VideoTaskFailed, "Failed"}, {VideoTaskCancelled, "Cancelled"},
}

// VideoTask is one prompt of the video batch queue (Video → All).
type VideoTask struct {
	ID          string               `json:"id"`
	BatchID     string               `json:"batchId"`
	Prompt      string               `json:"prompt"`
	ModelID     string               `json:"modelId"`
	AspectRatio string               `json:"aspectRatio"`
	Duration    int                  `json:"duration"`
	Resolution  string               `json:"resolution"`
	Count       int                  `json:"count"`
	References  []ImageTaskReference `json:"references"`
	// OutputDir is the batch's own folder under the one the user chose.
	OutputDir string `json:"outputDir"`
	// Index is the prompt's place in its batch (from 1); saved files are
	// numbered with it (media.OutputName).
	Index int `json:"index,omitempty"`
	// Held tasks wait in the queue until the user starts them.
	Held        bool                 `json:"held,omitempty"`
	Status      VideoTaskStatus      `json:"status"`
	Step        string               `json:"step,omitempty"`
	RequestID   string               `json:"requestId,omitempty"`
	AccountID   string               `json:"accountId,omitempty"`
	Rechecks    int                  `json:"rechecks,omitempty"`
	RefusedBy   []string             `json:"refusedBy,omitempty"`
	NextCheckAt string               `json:"nextCheckAt,omitempty"`
	Results     []GeneratedVideoItem `json:"results"`
	SavedFiles  []string             `json:"savedFiles"`
	SavedPaths  []string             `json:"savedPaths,omitempty"`
	Error       string               `json:"error,omitempty"`
	CreatedAt   string               `json:"createdAt"`
	StartedAt   string               `json:"startedAt,omitempty"`
	FinishedAt  string               `json:"finishedAt,omitempty"`
}

// JobID implements jobs.Record.
func (t VideoTask) JobID() string { return t.ID }

// Finished implements jobs.Record.
func (t VideoTask) Finished() bool {
	return t.Status == VideoTaskSucceeded || t.Status == VideoTaskFailed || t.Status == VideoTaskCancelled
}

// batchVideosPerPrompt is how many videos one queue task asks Flow for.
const batchVideosPerPrompt = 1

// VideoTaskRequest queues one task per non-empty prompt with shared settings.
type VideoTaskRequest struct {
	Prompts     []string             `json:"prompts"`
	ModelID     string               `json:"modelId"`
	AspectRatio string               `json:"aspectRatio"`
	Duration    int                  `json:"duration"`
	Resolution  string               `json:"resolution"`
	References  []ImageTaskReference `json:"references"`
	OutputDir   string               `json:"outputDir"`
	Hold        bool                 `json:"hold"`
}

// VideoQueueState is what the queue screen loads: the tasks and a warning
// when the journal was found corrupt at start.
type VideoQueueState struct {
	Tasks   []VideoTask `json:"tasks"`
	Warning string      `json:"warning,omitempty"`
}

type videoQueueTimingData struct {
	FormatVersion   int               `json:"formatVersion"`
	NextLaunch      string            `json:"nextLaunch,omitempty"`
	AccountCooldown map[string]string `json:"accountCooldown,omitempty"`
}

type videoQueue struct {
	s       *Provider
	journal *jobs.Journal[VideoTask]
	cfg     func() config.FlowConfig

	mu sync.Mutex
	// controlMu serializes user actions with scheduler admission.
	controlMu sync.Mutex
	ctx       context.Context
	stop      context.CancelFunc
	wg        sync.WaitGroup
	emit      func(events.Event, any)
	running   map[string]context.CancelFunc
	warning   string
	wake      chan struct{}

	timingMu        sync.Mutex
	nextLaunch      time.Time
	accountCooldown map[string]time.Time
	timingPath      string
	timingErr       error

	// Seams replaced in tests.
	generate     func(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem]
	resume       func(ctx context.Context, id string) models.Response[[]GeneratedVideoItem]
	requestPhase func(id string) (phase string, found bool, err error)
	saveVideo    func(localURL string, t VideoTask) (string, error)
	now          func() time.Time
	gap          func(min, max int) time.Duration
}

func newVideoQueue(s *Provider) *videoQueue {
	q := &videoQueue{
		s:               s,
		cfg:             func() config.FlowConfig { return s.cfg.Get().Flow },
		running:         map[string]context.CancelFunc{},
		wake:            make(chan struct{}, 1),
		emit:            func(events.Event, any) {},
		now:             time.Now,
		accountCooldown: map[string]time.Time{},
	}
	path := ""
	if s.cfg != nil {
		path = filepath.Join(s.stateDir(""), "video-queue.json")
		q.timingPath = filepath.Join(s.stateDir(""), "video-queue-timing.json")
	}
	q.journal = jobs.New(path, jobs.Options[VideoTask]{
		Limit: func() int { return q.cfg().VideoQueueMaxTasks },
		Normalize: func(t *VideoTask) {
			if t.References == nil {
				t.References = []ImageTaskReference{}
			}
			if t.Results == nil {
				t.Results = []GeneratedVideoItem{}
			}
			if t.SavedFiles == nil {
				t.SavedFiles = []string{}
			}
		},
		Transient: onlyVideoStepChanged,
	})
	q.generate = s.GenerateVideos
	q.resume = s.ResumeVideoGeneration
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
	q.saveVideo = func(localURL string, t VideoTask) (string, error) {
		return s.media.SaveTo(localURL, t.OutputDir, media.OutputName(t.Index, "flow"))
	}
	if s.media != nil {
		s.media.AllowRootsFrom(func() []string {
			var dirs []string
			for _, t := range q.journal.List() {
				if (!t.Finished() || len(unsavedVideos(t)) > 0) && t.OutputDir != "" {
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

func (q *videoQueue) start(ctx context.Context, emit func(events.Event, any)) {
	q.mu.Lock()
	q.ctx, q.stop = context.WithCancel(ctx)
	if emit != nil {
		q.emit = emit
	}
	q.mu.Unlock()

	q.loadTiming()

	if err := q.journal.Load(); err != nil {
		logger.Get().Warn("flow video queue reset", "error", err)
		if errors.Is(err, jobs.ErrCorrupt) || errors.Is(err, jobs.ErrNewerFormat) {
			q.mu.Lock()
			q.warning = strings.TrimSuffix(err.Error(), ": "+jobs.ErrCorrupt.Error())
			q.mu.Unlock()
		}
	}
	for _, t := range q.journal.List() {
		if t.Status == VideoTaskRunning {
			_, _ = q.journal.Update(t.ID, func(t *VideoTask) { t.Status, t.Step = VideoTaskQueued, "" })
		}
	}
	q.recoverHolds()
	q.wg.Add(1)
	go q.loop()
}

func (q *videoQueue) close() {
	q.mu.Lock()
	stop := q.stop
	q.mu.Unlock()
	if stop != nil {
		stop()
	}
	q.wg.Wait()
}

func (q *videoQueue) lifetime() context.Context {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ctx == nil {
		return context.Background()
	}
	return q.ctx
}

func (q *videoQueue) loop() {
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

func (q *videoQueue) safePump() {
	defer logger.Recover("hàng đợi video Flow")
	q.pump()
}

func (q *videoQueue) trigger() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *videoQueue) activeBatchJobCount() int {
	q.mu.Lock()
	running := make(map[string]bool, len(q.running))
	for id := range q.running {
		running[id] = true
	}
	q.mu.Unlock()

	count := 0
	for _, t := range q.journal.List() {
		if t.Finished() {
			continue
		}
		if running[t.ID] || !q.isFreshSend(t) {
			count++
		}
	}
	return count
}

func (q *videoQueue) pump() {
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	ctx := q.lifetime()
	if ctx.Err() != nil {
		return
	}
	cfg := q.cfg()
	for {
		now := q.now()
		q.mu.Lock()
		freeWorkers := cfg.VideoQueueThreads - len(q.running)
		q.mu.Unlock()
		if freeWorkers <= 0 {
			return
		}

		activeCount := q.activeBatchJobCount()
		canAdmitFresh := activeCount < cfg.VideoQueueThreads

		task, ok := q.nextDue(now, canAdmitFresh)
		if !ok {
			return
		}
		fresh := q.isFreshSend(task)

		taskCtx, cancel := context.WithCancel(ctx)
		q.mu.Lock()
		q.running[task.ID] = cancel
		q.mu.Unlock()

		started, err := q.journal.Update(task.ID, func(t *VideoTask) {
			t.Status, t.Error, t.NextCheckAt = VideoTaskRunning, "", ""
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
			defer logger.Recover("tác vụ video Flow", func(err error) { q.fail(task.ID, err.Error(), false) })
			q.execute(taskCtx, started)
		}()
	}
}

func (q *videoQueue) release(id string) {
	q.releaseSettledHolds(id)
	q.mu.Lock()
	if cancel, ok := q.running[id]; ok {
		cancel()
		delete(q.running, id)
	}
	q.mu.Unlock()
	q.trigger()
}

func (q *videoQueue) nextDue(now time.Time, canAdmitFresh bool) (VideoTask, bool) {
	q.mu.Lock()
	running := make(map[string]bool, len(q.running))
	for id := range q.running {
		running[id] = true
	}
	q.mu.Unlock()

	all := q.journal.List()
	for _, t := range all {
		if t.Status != VideoTaskQueued || t.Held || running[t.ID] {
			continue
		}
		if t.NextCheckAt != "" {
			if at, err := time.Parse(time.RFC3339, t.NextCheckAt); err == nil && now.Before(at) {
				continue
			}
		}
		fresh := q.isFreshSend(t)
		if fresh {
			if !canAdmitFresh {
				continue
			}
			q.timingMu.Lock()
			launchGate := q.nextLaunch
			q.timingMu.Unlock()
			if now.Before(launchGate) {
				continue
			}
			if !q.hasAccountReadyForFreshSend(t, now) {
				continue
			}
		}
		return t, true
	}
	return VideoTask{}, false
}

func (q *videoQueue) hasAccountReadyForFreshSend(t VideoTask, now time.Time) bool {
	if q.s.accounts == nil {
		return true
	}
	valid, err := q.s.GetValidConnectedAccounts()
	if err != nil || len(valid) == 0 {
		return true
	}
	unrefused := 0
	for _, acc := range valid {
		if slices.Contains(t.RefusedBy, acc.ID) {
			continue
		}
		unrefused++
		if q.s.IsBatchVideoAccountHeld(acc.ID, t.ID) {
			continue
		}
		if q.accountWait(acc.ID, now) > 0 {
			continue
		}
		return true
	}
	// Let GenerateVideos report exhaustion; otherwise this prompt never settles.
	return unrefused == 0
}

func (q *videoQueue) accountWait(accountID string, now time.Time) time.Duration {
	q.timingMu.Lock()
	defer q.timingMu.Unlock()
	return max(0, q.accountCooldown[accountID].Sub(now))
}

func (q *videoQueue) isFreshSend(t VideoTask) bool {
	if q.onlySaving(t) {
		return false
	}
	if t.RequestID == "" {
		return true
	}
	_, found, err := q.requestPhase(t.RequestID)
	return !found && err == nil
}

func (q *videoQueue) onlySaving(t VideoTask) bool {
	return t.RequestID == "" && t.OutputDir != "" && len(t.Results) > 0 && len(unsavedVideos(t)) > 0
}

func (q *videoQueue) waitForNextLaunch(ctx context.Context) error {
	for {
		q.timingMu.Lock()
		now := q.now()
		wait := q.nextLaunch.Sub(now)
		if wait <= 0 {
			cfg := q.cfg()
			q.nextLaunch = now.Add(q.gap(cfg.VideoQueueGapMinSeconds, cfg.VideoQueueGapMaxSeconds))
			err := q.saveTimingLocked()
			q.timingMu.Unlock()
			return err
		}
		timer := time.NewTimer(wait)
		q.timingMu.Unlock()

		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (q *videoQueue) execute(ctx context.Context, t VideoTask) {
	if q.onlySaving(t) {
		q.finishWithResults(t, t.Results)
		return
	}
	if t.RequestID == "" {
		id := uuid.NewString()
		var err error
		if t, err = q.journal.Update(t.ID, func(t *VideoTask) { t.RequestID = id }); err != nil {
			q.fail(t.ID, "Không ghi được hàng đợi: "+err.Error(), false)
			return
		}
	}
	var res models.Response[[]GeneratedVideoItem]
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
			q.fail(t.ID, err.Error(), true)
			return
		}
		if err := q.waitForNextLaunch(ctx); err != nil {
			if ctx.Err() != nil {
				q.interrupted(t.ID)
			} else {
				q.fail(t.ID, "Không ghi được khoảng chờ hàng đợi: "+err.Error(), true)
			}
			return
		}
		refused := ""
		req.avoid = t.RefusedBy
		req.batchTaskID = t.ID
		req.onReserved = func(accountID string) error {
			updated, err := q.journal.Update(t.ID, func(task *VideoTask) { task.AccountID = accountID })
			if err == nil {
				q.publish(updated)
			}
			return err
		}
		req.onRefused = func(accountID string) { refused = accountID }
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
	q.settle(t, res)
}

func (q *videoQueue) request(t VideoTask) (GenerateVideoRequest, error) {
	refs := make([]ReferenceImage, 0, len(t.References))
	for _, ref := range t.References {
		path, err := q.s.media.Path(ref.LocalURL)
		if err != nil {
			return GenerateVideoRequest{}, fmt.Errorf("Ảnh tham chiếu %s không còn trong thư mục media.", ref.FileName)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return GenerateVideoRequest{}, fmt.Errorf("Không đọc được ảnh tham chiếu %s: %v", ref.FileName, err)
		}
		refs = append(refs, ReferenceImage{
			Base64:   base64.StdEncoding.EncodeToString(data),
			MimeType: ref.MimeType,
			FileName: ref.FileName,
		})
	}
	return GenerateVideoRequest{
		RequestID:   t.RequestID,
		Prompt:      t.Prompt,
		ModelID:     t.ModelID,
		AspectRatio: t.AspectRatio,
		Duration:    t.Duration,
		Resolution:  t.Resolution,
		Count:       batchVideosPerPrompt,
		References:  refs,
	}, nil
}

func (q *videoQueue) settle(t VideoTask, res models.Response[[]GeneratedVideoItem]) {
	phase, found, phaseErr := q.requestPhase(t.RequestID)
	open := found && phase != generationComplete && phase != generationRejected

	accountID := t.AccountID
	if accountID == "" && found {
		if job, err := q.s.loadGenerationJob(t.RequestID); err == nil && job.AccountID != "" {
			accountID = job.AccountID
			updated, err := q.journal.Update(t.ID, func(task *VideoTask) { task.AccountID = accountID })
			if err == nil {
				t = updated
			}
		}
	}

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
	if pending := slices.IndexFunc(items, func(i GeneratedVideoItem) bool { return i.Pending }); pending >= 0 || open {
		message := "Flow đang xử lý job đã nhận."
		if pending >= 0 && items[pending].Error != "" {
			message = items[pending].Error
		}
		q.recheck(t.ID, items, message)
		return
	}

	made := slices.ContainsFunc(items, func(i GeneratedVideoItem) bool { return i.LocalURL != "" })
	if !made {
		message := "Không tạo được video."
		if i := slices.IndexFunc(items, func(i GeneratedVideoItem) bool { return i.Error != "" }); i >= 0 {
			message = items[i].Error
		}
		updated, err := q.journal.Update(t.ID, func(t *VideoTask) {
			t.Results, t.RequestID = items, ""
		})
		if err == nil {
			t = updated
		}
		q.fail(t.ID, message, true)
		return
	}

	updated, err := q.journal.Update(t.ID, func(t *VideoTask) {
		t.Results, t.RequestID, t.Rechecks = items, "", 0
	})
	if err != nil {
		q.fail(t.ID, "Không ghi được hàng đợi: "+err.Error(), false)
		return
	}
	q.finishWithResults(updated, items)
}

func (q *videoQueue) finishWithResults(t VideoTask, items []GeneratedVideoItem) {
	saved := slices.Clone(t.SavedFiles)
	paths := slices.Clone(t.SavedPaths)
	var saveErr string
	for _, item := range items {
		if t.OutputDir == "" || item.LocalURL == "" || slices.Contains(saved, item.LocalURL) {
			continue
		}
		path, err := q.saveVideo(item.LocalURL, t)
		if err != nil {
			if saveErr == "" {
				saveErr = err.Error()
			}
			continue
		}
		saved, paths = append(saved, item.LocalURL), append(paths, path)
	}
	updated, err := q.journal.Update(t.ID, func(t *VideoTask) {
		t.SavedFiles, t.SavedPaths, t.Step, t.NextCheckAt = saved, paths, "", ""
		t.FinishedAt = q.stamp()
		if saveErr != "" {
			t.Status, t.Error = VideoTaskFailed, "Lỗi lưu video: "+saveErr
			return
		}
		t.Status, t.Error = VideoTaskSucceeded, ""
		if i := slices.IndexFunc(items, func(i GeneratedVideoItem) bool { return i.Error != "" }); i >= 0 {
			t.Error = items[i].Error
		}
	})
	if err == nil {
		q.publish(updated)
	}
	if updated.AccountID != "" {
		q.settleAccountCooldown(updated.AccountID, t.ID)
	}
}

func (q *videoQueue) recheck(id string, items []GeneratedVideoItem, message string) {
	updated, err := q.journal.Update(id, func(t *VideoTask) {
		if t.Status != VideoTaskRunning {
			return
		}
		wait := recheckFirst << min(t.Rechecks, 6)
		if wait > recheckMax {
			wait = recheckMax
		}
		at := q.now().Add(wait)
		t.Rechecks++
		t.Status, t.NextCheckAt = VideoTaskQueued, at.UTC().Format(time.RFC3339)
		t.Step = fmt.Sprintf("%s Kiểm tra lại lúc %s; yêu cầu sẽ không được gửi lại.", strings.TrimSpace(message), at.Local().Format("15:04:05"))
		if items != nil {
			t.Results = items
		}
	})
	if err == nil {
		q.publish(updated)
	}
}

func (q *videoQueue) passOn(id, accountID string) {
	updated, err := q.journal.Update(id, func(t *VideoTask) {
		if t.Status != VideoTaskRunning {
			return
		}
		t.Status, t.Error, t.NextCheckAt = VideoTaskQueued, "", ""
		t.Step = "Tài khoản vừa bị Google tạm chặn; chuyển sang tài khoản khác."
		t.RequestID, t.Rechecks, t.AccountID = "", 0, ""
		if !slices.Contains(t.RefusedBy, accountID) {
			t.RefusedBy = append(t.RefusedBy, accountID)
		}
	})
	if err == nil {
		q.publish(updated)
	}
	q.settleAccountCooldown(accountID, id)
}

func (q *videoQueue) unknown(id string, err error) {
	if errors.Is(err, errJournalQuarantined) {
		q.fail(id, err.Error(), false)
		return
	}
	q.recheck(id, nil, "Không đọc được nhật ký yêu cầu: "+err.Error())
}

func (q *videoQueue) fail(id, message string, definite bool) {
	accountID := ""
	updated, err := q.journal.Update(id, func(t *VideoTask) {
		if t.Status != VideoTaskRunning {
			return
		}
		t.Status, t.Error, t.Step, t.NextCheckAt = VideoTaskFailed, message, "", ""
		t.FinishedAt = q.stamp()
		accountID = t.AccountID
		if definite {
			t.RequestID, t.AccountID, t.Rechecks = "", "", 0
		}
	})
	if err == nil {
		q.publish(updated)
	}
	if definite && accountID != "" {
		q.settleAccountCooldown(accountID, id)
	}
}

func (q *videoQueue) interrupted(id string) {
	closing := q.lifetime().Err() != nil
	accountID := ""
	updated, err := q.journal.Update(id, func(t *VideoTask) {
		if t.Status != VideoTaskRunning {
			return
		}
		if closing {
			t.Status, t.Step = VideoTaskQueued, ""
			return
		}
		t.Status, t.Step, t.FinishedAt = VideoTaskCancelled, "", q.stamp()
		if phase, found, err := q.requestPhase(t.RequestID); err == nil && (!found || phase == generationRejected) {
			t.RequestID = ""
		}
		accountID = t.AccountID
	})
	if err == nil {
		q.publish(updated)
	}
	if !closing && accountID != "" && updated.RequestID == "" {
		q.settleAccountCooldown(accountID, id)
	}
}

func (q *videoQueue) stamp() string { return q.now().UTC().Format(time.RFC3339) }

func (q *videoQueue) publish(t VideoTask) {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	emit(events.EventFlowVideoTaskUpdated, t)
}

func (q *videoQueue) changed(added []VideoTask, removed []string) {
	q.mu.Lock()
	emit := q.emit
	q.mu.Unlock()
	if len(removed) > 0 {
		emit(events.EventFlowVideoTasksRemoved, removed)
	}
	if len(added) > 0 {
		emit(events.EventFlowVideoTasksAdded, added)
	}
	q.trigger()
}

func unsavedVideos(t VideoTask) []string {
	var out []string
	for _, item := range t.Results {
		if item.LocalURL != "" && !slices.Contains(t.SavedFiles, item.LocalURL) {
			out = append(out, item.LocalURL)
		}
	}
	return out
}

func onlyVideoStepChanged(a, b VideoTask) bool {
	a.Step, b.Step = "", ""
	return reflect.DeepEqual(a, b)
}
