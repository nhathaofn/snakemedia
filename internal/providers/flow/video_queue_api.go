package flow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
)

// SubmitVideoTasks queues one task per non-empty prompt.
func (s *Provider) SubmitVideoTasks(req VideoTaskRequest) models.Response[[]VideoTask] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	prompts := make([]string, 0, len(req.Prompts))
	for _, p := range req.Prompts {
		if p = strings.TrimSpace(p); p != "" {
			prompts = append(prompts, p)
		}
	}
	if len(prompts) == 0 {
		return queueFail[[]VideoTask](errors.New("Chưa có prompt nào."), models.CodeInvalidArgument)
	}
	if strings.TrimSpace(req.ModelID) == "" {
		return queueFail[[]VideoTask](errors.New("Chưa chọn model video."), models.CodeInvalidArgument)
	}
	if _, ok := FlowVideoAspect[req.AspectRatio]; !ok {
		return queueFail[[]VideoTask](fmt.Errorf("Tỉ lệ video %q không hợp lệ.", req.AspectRatio), models.CodeInvalidArgument)
	}
	if req.Duration <= 0 {
		return queueFail[[]VideoTask](errors.New("Chưa chọn thời lượng video."), models.CodeInvalidArgument)
	}
	if _, ok := FlowVideoResolution[req.Resolution]; !ok {
		return queueFail[[]VideoTask](fmt.Errorf("Chất lượng video %q không hợp lệ.", req.Resolution), models.CodeInvalidArgument)
	}
	if strings.TrimSpace(req.OutputDir) == "" {
		return queueFail[[]VideoTask](errors.New("Chưa chọn thư mục lưu."), models.CodeInvalidArgument)
	}
	dir, err := s.media.CheckDir(req.OutputDir)
	if err != nil {
		return queueFail[[]VideoTask](err, models.CodeInvalidArgument)
	}
	refs, err := s.checkTaskReferences(req.References)
	if err != nil {
		return queueFail[[]VideoTask](err, models.CodeInvalidArgument)
	}
	dir, first := q.batchFolder(dir)
	batch, now := uuid.NewString(), q.stamp()
	tasks := make([]VideoTask, len(prompts))
	for i, p := range prompts {
		tasks[i] = VideoTask{
			ID:          uuid.NewString(),
			BatchID:     batch,
			Prompt:      p,
			ModelID:     req.ModelID,
			AspectRatio: req.AspectRatio,
			Duration:    req.Duration,
			Resolution:  req.Resolution,
			Count:       batchVideosPerPrompt,
			References:  slices.Clone(refs),
			OutputDir:   dir,
			Index:       first + i,
			Held:        req.Hold,
			Status:      VideoTaskQueued,
			Results:     []GeneratedVideoItem{},
			SavedFiles:  []string{},
			CreatedAt:   now,
		}
	}
	trimmed, err := q.journal.AddTrimming(tasks...)
	if err != nil {
		return queueFail[[]VideoTask](err, models.CodeInternal)
	}
	q.changed(tasks, trimmed)
	return models.SuccessResponse(tasks)
}

func (q *videoQueue) batchFolder(parent string) (string, int) {
	var items []media.BatchItem
	for _, t := range q.journal.List() {
		if t.OutputDir != "" {
			items = append(items, media.BatchItem{Dir: t.OutputDir, Index: t.Index, CreatedAt: t.CreatedAt, Open: !t.Finished()})
		}
	}
	return media.BatchFolder(parent, "snakemedia-flow-video", q.now(), items)
}

// ListVideoTasks returns the video queue and any warning about its journal.
func (s *Provider) ListVideoTasks() models.Response[VideoQueueState] {
	q := s.videoQueue
	q.mu.Lock()
	warning := q.warning
	q.mu.Unlock()
	return models.SuccessResponse(VideoQueueState{Tasks: q.journal.List(), Warning: warning})
}

func (q *videoQueue) selected(ids []string) []VideoTask {
	all := q.journal.List()
	if len(ids) == 0 {
		return all
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]VideoTask, 0, len(ids))
	for _, t := range all {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

func (q *videoQueue) updateSelected(ids []string, change func(*VideoTask) bool) (int, error) {
	tasks := q.selected(ids)
	ordered := make([]string, len(tasks))
	for i, t := range tasks {
		ordered[i] = t.ID
	}
	updated, err := q.journal.UpdateMany(ordered, change)
	if err != nil {
		return 0, err
	}
	for _, t := range updated {
		q.publish(t)
	}
	return len(updated), nil
}

// RunVideoTasks releases held queued tasks (all when ids is empty).
func (s *Provider) RunVideoTasks(ids []string) models.Response[int] {
	return s.setVideoHeld(ids, false)
}

// HoldVideoTasks keeps queued tasks from starting (all when ids is empty).
func (s *Provider) HoldVideoTasks(ids []string) models.Response[int] {
	return s.setVideoHeld(ids, true)
}

func (s *Provider) setVideoHeld(ids []string, held bool) models.Response[int] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	updated, err := q.updateSelected(ids, func(t *VideoTask) bool {
		if t.Status != VideoTaskQueued || t.Held == held {
			return false
		}
		t.Held = held
		return true
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.trigger()
	return models.SuccessResponse(updated)
}

// CancelVideoTask stops a queued or running task.
func (s *Provider) CancelVideoTask(id string) models.Response[VideoTask] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	q.mu.Lock()
	cancel, running := q.running[id]
	q.mu.Unlock()
	if running {
		cancel()
		t, _ := q.journal.Get(id)
		return models.SuccessResponse(t)
	}
	updated, err := q.journal.Update(id, func(t *VideoTask) {
		if t.Status == VideoTaskQueued {
			t.Status, t.Step, t.NextCheckAt, t.FinishedAt = VideoTaskCancelled, "", "", q.stamp()
		}
	})
	if err != nil {
		return queueFail[VideoTask](err, models.CodeNotFound)
	}
	q.publish(updated)
	q.releaseSettledHolds(id)
	return models.SuccessResponse(updated)
}

// EditVideoTask changes the prompt of a task that is not running.
func (s *Provider) EditVideoTask(id, prompt string) models.Response[VideoTask] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return queueFail[VideoTask](errors.New("Prompt không được để trống."), models.CodeInvalidArgument)
	}
	q.mu.Lock()
	_, running := q.running[id]
	q.mu.Unlock()
	t, ok := q.journal.Get(id)
	if !ok {
		return queueFail[VideoTask](errors.New("Tác vụ không tồn tại."), models.CodeNotFound)
	}
	if running || t.Status == VideoTaskRunning {
		return queueFail[VideoTask](errors.New("Tác vụ đang chạy, hãy hủy trước khi sửa."), models.CodeInvalidArgument)
	}
	if t.RequestID != "" {
		return queueFail[VideoTask](errors.New("Tác vụ đang có yêu cầu gửi dở dang, không thể sửa."), models.CodeInvalidArgument)
	}
	updated, err := q.journal.Update(id, func(t *VideoTask) {
		t.Prompt = prompt
		if t.Status == VideoTaskQueued {
			return
		}
		t.Status, t.Error, t.Step, t.NextCheckAt, t.FinishedAt = VideoTaskQueued, "", "", "", ""
		t.RequestID, t.AccountID, t.Rechecks, t.Held, t.RefusedBy = "", "", 0, false, nil
		t.Results, t.SavedFiles, t.SavedPaths = []GeneratedVideoItem{}, []string{}, nil
	})
	if err != nil {
		return queueFail[VideoTask](err, models.CodeInternal)
	}
	q.publish(updated)
	q.trigger()
	return models.SuccessResponse(updated)
}

// RetryVideoTasks queues failed or cancelled tasks again.
func (s *Provider) RetryVideoTasks(ids []string) models.Response[int] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	n, err := q.updateSelected(ids, func(t *VideoTask) bool {
		if t.Status != VideoTaskFailed && t.Status != VideoTaskCancelled {
			return false
		}
		t.Status, t.Error, t.Step, t.NextCheckAt, t.FinishedAt = VideoTaskQueued, "", "", "", ""
		t.Rechecks, t.Held, t.RefusedBy = 0, false, nil
		if t.RequestID == "" && len(unsavedVideos(*t)) == 0 {
			t.Results, t.SavedFiles, t.SavedPaths = []GeneratedVideoItem{}, []string{}, nil
			t.AccountID = ""
		}
		return true
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.trigger()
	return models.SuccessResponse(n)
}

// RemoveVideoTasks deletes tasks that are not running.
func (s *Provider) RemoveVideoTasks(ids []string) models.Response[int] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	q.mu.Lock()
	running := make(map[string]bool, len(q.running))
	for id := range q.running {
		running[id] = true
	}
	q.mu.Unlock()
	removed, err := q.journal.RemoveWhereIDs(func(t VideoTask) bool {
		return slices.Contains(ids, t.ID) && !running[t.ID] && t.Status != VideoTaskRunning && t.RequestID == ""
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	for _, id := range removed {
		q.s.ReleaseBatchVideoTask(id)
	}
	q.changed(nil, removed)
	return models.SuccessResponse(len(removed))
}

// ClearFinishedVideoTasks deletes the succeeded tasks.
func (s *Provider) ClearFinishedVideoTasks() models.Response[int] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	removed, err := q.journal.RemoveWhereIDs(func(t VideoTask) bool { return t.Status == VideoTaskSucceeded })
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	for _, id := range removed {
		q.s.ReleaseBatchVideoTask(id)
	}
	q.changed(nil, removed)
	return models.SuccessResponse(len(removed))
}

// AddVideoTaskReferences appends references to tasks that have not started a request.
func (s *Provider) AddVideoTaskReferences(ids []string, refs []ImageTaskReference) models.Response[int] {
	q := s.videoQueue
	q.controlMu.Lock()
	defer q.controlMu.Unlock()
	checked, err := s.checkTaskReferences(refs)
	if err != nil {
		return queueFail[int](err, models.CodeInvalidArgument)
	}
	n, err := q.updateSelected(ids, func(t *VideoTask) bool {
		if t.Status == VideoTaskRunning || t.Status == VideoTaskSucceeded || t.RequestID != "" {
			return false
		}
		for _, ref := range checked {
			if !slices.ContainsFunc(t.References, func(r ImageTaskReference) bool { return r.LocalURL == ref.LocalURL }) {
				t.References = append(t.References, ref)
			}
		}
		return true
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	return models.SuccessResponse(n)
}
