package flow

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
)

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
	q.submitMu.Lock()
	dir, first, batch := q.batchFolder(dir)
	now := q.stamp()
	tasks := make([]ImageTask, len(prompts))
	for i, p := range prompts {
		tasks[i] = ImageTask{
			ID: uuid.NewString(), BatchID: batch, Prompt: p, ModelID: req.ModelID, AspectRatio: req.AspectRatio,
			Quality: quality, Count: batchImagesPerPrompt, References: slices.Clone(refs), OutputDir: dir, Index: first + i, Held: req.Hold,
			Status: ImageTaskQueued, Results: []GeneratedImageItem{}, SavedFiles: []string{}, CreatedAt: now,
		}
	}
	trimmed, err := q.journal.AddTrimming(tasks...)
	q.submitMu.Unlock()
	if err != nil {
		return queueFail[[]ImageTask](err, models.CodeInternal)
	}
	q.changed(tasks, trimmed)
	return models.SuccessResponse(tasks)
}

// batchFolder picks the batch's folder under parent and the first number for
// the prompts being added (media.BatchFolder): an unfinished batch is joined,
// otherwise a new "snakemedia-flow-image-<date-time>" is opened.
func (q *imageQueue) batchFolder(parent string) (string, int, string) {
	var items []media.BatchItem
	tasks := q.journal.List()
	for _, t := range tasks {
		if t.OutputDir != "" {
			items = append(items, media.BatchItem{Dir: t.OutputDir, Index: t.Index, CreatedAt: t.CreatedAt, Open: !t.Finished()})
		}
	}
	dir, first := media.BatchFolder(parent, "snakemedia-flow-image", q.now(), items)
	for _, t := range tasks {
		if t.OutputDir == dir && !t.Finished() && t.BatchID != "" {
			return dir, first, t.BatchID
		}
	}
	return dir, first, uuid.NewString()
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
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]ImageTask, 0, len(ids))
	for _, t := range all {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// updateSelected applies change to the tasks with ids (every task when ids is
// empty, each once, in queue order) in one journal write and publishes each task change took. It returns
// how many changed; a failed write publishes none.
func (q *imageQueue) updateSelected(ids []string, change func(*ImageTask) bool) (int, error) {
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

// onlyImageStepChanged reports whether a and b differ in nothing but Step:
// progress text stays in memory and reaches disk with the next real change.
func onlyImageStepChanged(a, b ImageTask) bool {
	a.Step, b.Step = "", ""
	return reflect.DeepEqual(a, b)
}

func (q *imageQueue) isRunning(id string) bool {
	q.mu.Lock()
	_, ok := q.running[id]
	q.mu.Unlock()
	return ok
}

// UpdateImageTasksSettings applies model, ratio and/or quality changes to
// tasks that have not sent a request yet (queued tasks when IDs is empty).
func (s *Provider) UpdateImageTasksSettings(req ImageTaskSettingsRequest) models.Response[int] {
	modelID := strings.TrimSpace(req.ModelID)
	ratio := strings.TrimSpace(req.AspectRatio)
	var quality ImageQuality
	if req.Quality != "" {
		var err error
		quality, err = normalizeImageQuality(req.Quality)
		if err != nil {
			return queueFail[int](err, models.CodeInvalidArgument)
		}
	}
	if modelID == "" && ratio == "" && quality == "" {
		return models.SuccessResponse(0)
	}
	q := s.queue
	byID := len(req.IDs) > 0
	n, err := q.updateSelected(req.IDs, func(t *ImageTask) bool {
		if q.isRunning(t.ID) || t.RequestID != "" {
			return false
		}
		if byID {
			if t.Status != ImageTaskQueued && t.Status != ImageTaskFailed && t.Status != ImageTaskCancelled {
				return false
			}
		} else if t.Status != ImageTaskQueued {
			return false
		}
		changed := false
		if modelID != "" && t.ModelID != modelID {
			t.ModelID = modelID
			changed = true
		}
		if ratio != "" && t.AspectRatio != ratio {
			t.AspectRatio = ratio
			changed = true
		}
		if quality != "" && t.Quality != quality {
			t.Quality = quality
			changed = true
		}
		return changed
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	return models.SuccessResponse(n)
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
	updated, err := q.updateSelected(ids, func(t *ImageTask) bool {
		if t.Status != ImageTaskQueued {
			return false
		}
		if held {
			// Only active waiting tasks (not already held) transition to Tạm dừng.
			if t.Held {
				return false
			}
			t.Held, t.Paused = true, true
			return true
		}
		if !t.Held && !t.Paused {
			return false
		}
		t.Held, t.Paused = false, false
		return true
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.trigger()
	return models.SuccessResponse(updated)
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
	// Close TOCTOU window: if pump() claimed the task between our first check
	// and journal.Update (so Status was already Running), cancel it now.
	if updated.Status == ImageTaskRunning {
		q.mu.Lock()
		cancel, running = q.running[id]
		q.mu.Unlock()
		if running {
			cancel()
		}
		t, _ := q.journal.Get(id)
		return models.SuccessResponse(t)
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
	canEdit := true
	updated, err := q.journal.Update(id, func(t *ImageTask) {
		if t.Status == ImageTaskRunning || (t.Status == ImageTaskQueued && t.RequestID != "") {
			canEdit = false
			return
		}
		t.Prompt = prompt
		if t.Status == ImageTaskQueued {
			return
		}
		t.Status, t.Error, t.Step, t.NextCheckAt, t.FinishedAt = ImageTaskQueued, "", "", "", ""
		t.RequestID, t.Rechecks, t.Held, t.Paused, t.RefusedBy = "", 0, false, false, nil
		t.Results, t.SavedFiles, t.SavedPaths = []GeneratedImageItem{}, []string{}, nil
	})
	if err != nil {
		return queueFail[ImageTask](err, models.CodeInternal)
	}
	if !canEdit {
		return queueFail[ImageTask](errors.New("Tác vụ đang chạy hoặc đang đối soát, hãy hủy trước khi sửa."), models.CodeInvalidArgument)
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
	n, err := q.updateSelected(ids, func(t *ImageTask) bool {
		if t.Status != ImageTaskFailed && t.Status != ImageTaskCancelled {
			return false
		}
		t.Status, t.Error, t.Step, t.NextCheckAt, t.FinishedAt = ImageTaskQueued, "", "", "", ""
		t.Rechecks, t.Held, t.Paused, t.RefusedBy = 0, false, false, nil
		if t.RequestID == "" && len(unsaved(*t)) == 0 {
			t.Results, t.SavedFiles, t.SavedPaths = []GeneratedImageItem{}, []string{}, nil
		}
		return true
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
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
	removed, err := q.journal.RemoveWhereIDs(func(t ImageTask) bool {
		return slices.Contains(ids, t.ID) && !running[t.ID] && t.Status != ImageTaskRunning
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.changed(nil, removed)
	return models.SuccessResponse(len(removed))
}

// ClearFinishedImageTasks deletes the succeeded tasks. Their images stay in
// their folders.
func (s *Provider) ClearFinishedImageTasks() models.Response[int] {
	q := s.queue
	removed, err := q.journal.RemoveWhereIDs(func(t ImageTask) bool { return t.Status == ImageTaskSucceeded })
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
	}
	q.changed(nil, removed)
	return models.SuccessResponse(len(removed))
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
	n, err := q.updateSelected(ids, func(t *ImageTask) bool {
		if t.Status == ImageTaskRunning || t.Status == ImageTaskSucceeded || t.RequestID != "" {
			return false
		}
		refs := slices.Clone(t.References)
		for _, ref := range checked {
			if !slices.ContainsFunc(refs, func(r ImageTaskReference) bool { return r.LocalURL == ref.LocalURL }) {
				refs = append(refs, ref)
			}
		}
		if len(refs) == len(t.References) {
			return false
		}
		t.References = refs
		return true
	})
	if err != nil {
		return queueFail[int](err, models.CodeInternal)
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
