package muse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

// MuseOptions is what the create screens may offer, read from Muse on the
// accounts: Known is false until an account's limits have been read.
type MuseOptions struct {
	Known bool `json:"known"`
	// MaxFileBytes caps one reference image; MaxFiles the images of one job.
	MaxFileBytes    int64    `json:"maxFileBytes"`
	MaxFiles        int      `json:"maxFiles"`
	ImageExtensions []string `json:"imageExtensions"`
	// Models are the chat models Muse offers the accounts.
	Models    []MuseModel `json:"models"`
	CheckedAt string      `json:"checkedAt"`
	// JournalWarning is set when the job journal was unreadable on start and
	// moved aside: jobs it held, sent ones included, are no longer listed.
	JournalWarning string `json:"journalWarning"`
	// Ratios and Durations (seconds, video) are what a job may ask Muse for.
	Ratios    []MuseRatio `json:"ratios"`
	Durations []int       `json:"durations"`
}

// MuseJobRequest queues Repeat jobs per non-empty prompt with shared settings.
type MuseJobRequest struct {
	Kind MuseJobKind `json:"kind"`
	// Mode is the screen the jobs come from; empty means MuseJobBatch.
	Mode       MuseJobMode `json:"mode"`
	Prompts    []string    `json:"prompts"`
	Repeat     int         `json:"repeat"`
	References []string    `json:"references"`
	Label      string      `json:"label"`
	Held       bool        `json:"held"`
	// OutputDir is the folder the Hàng loạt screen saves the batch's files
	// into (required there). Đơn and graph nodes save nothing.
	OutputDir string `json:"outputDir"`
	// Ratio and Duration (seconds, video) are asked of Muse for every job;
	// empty or 0 leaves them to the prompt. MuseOptions lists the values.
	Ratio    string `json:"ratio"`
	Duration int    `json:"duration"`
}

// MuseReference is an image imported for use as a reference.
type MuseReference struct {
	LocalURL string `json:"localUrl"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
}

func fail[T any](err error, code models.Code) models.Response[T] {
	return models.ErrorResponse[T](err.Error(), code)
}

// errLimitsUnknown means no account has had Muse's limits read yet.
var errLimitsUnknown = errors.New("Chưa đọc được giới hạn đính kèm của Muse. Hãy kiểm tra một tài khoản Muse đã đăng nhập (Trang chủ → Tài khoản).")

// knownCapabilities returns the most recently read capabilities of any Muse
// account (Muse's limits belong to its build, not to one account), with the
// models any account offers.
func (p *Provider) knownCapabilities() (*MuseCapabilities, []MuseModel) {
	records, err := p.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		return nil, nil
	}
	var latest *MuseCapabilities
	var offered []MuseModel
	for _, rec := range records {
		var data museData
		if accounts.DecodeData(rec, &data) != nil || data.Capabilities == nil {
			continue
		}
		if latest == nil || data.Capabilities.CheckedAt > latest.CheckedAt {
			latest = data.Capabilities
		}
		for _, m := range data.Capabilities.Models {
			if !slices.ContainsFunc(offered, func(x MuseModel) bool { return x.ID == m.ID }) {
				offered = append(offered, m)
			}
		}
	}
	return latest, offered
}

// GetOptions returns what the create screens may offer.
func (p *Provider) GetOptions() models.Response[MuseOptions] {
	caps, offered := p.knownCapabilities()
	if offered == nil {
		offered = []MuseModel{}
	}
	p.mu.Lock()
	warning := p.journalWarning
	p.mu.Unlock()
	if caps == nil {
		return models.SuccessResponse(MuseOptions{ImageExtensions: []string{}, Models: offered, JournalWarning: warning,
			Ratios: museRatios, Durations: museDurations})
	}
	return models.SuccessResponse(MuseOptions{
		Known: true, MaxFileBytes: caps.MaxFileBytes, MaxFiles: caps.MaxFiles,
		ImageExtensions: caps.extensions(), Models: offered, CheckedAt: caps.CheckedAt,
		JournalWarning: warning, Ratios: museRatios, Durations: museDurations,
	})
}

// ImageFilePatterns lists the image types Muse accepts as file-picker patterns
// ("*.png"); empty until Muse's limits are known.
func (p *Provider) ImageFilePatterns() []string {
	caps, _ := p.knownCapabilities()
	out := []string{}
	for _, ext := range caps.extensions() {
		out = append(out, "*"+ext)
	}
	return out
}

// ---------- Jobs ----------

// ImportReferences copies picked image files into the media folder so they can
// be previewed and attached. Muse's own limits decide what is accepted.
func (p *Provider) ImportReferences(paths []string) models.Response[[]MuseReference] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[[]MuseReference](errLimitsUnknown, models.CodeUnavailable)
	}
	out := make([]MuseReference, 0, len(paths))
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		mime, ok := caps.imageMime(ext)
		if !ok {
			return fail[[]MuseReference](fmt.Errorf("Muse không nhận định dạng của %s (nhận %s).", filepath.Base(path), strings.Join(caps.extensions(), ", ")), models.CodeInvalidArgument)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fail[[]MuseReference](fmt.Errorf("Không đọc được %s.", filepath.Base(path)), models.CodeInvalidArgument)
		}
		if info.Size() > caps.MaxFileBytes {
			return fail[[]MuseReference](fmt.Errorf("%s lớn hơn %d MB, giới hạn một file của Muse.", filepath.Base(path), caps.MaxFileBytes>>20), models.CodeInvalidArgument)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fail[[]MuseReference](err, models.CodeInternal)
		}
		url, err := p.media.Put(referencePrefix+uuid.NewString(), mime, ext, data)
		if err != nil {
			return fail[[]MuseReference](err, models.CodeInternal)
		}
		out = append(out, MuseReference{LocalURL: url, FileName: filepath.Base(path), Size: info.Size()})
	}
	return models.SuccessResponse(out)
}

// SubmitJobs queues Repeat jobs (at least one) for every non-empty prompt.
func (p *Provider) SubmitJobs(req MuseJobRequest) models.Response[[]MuseJob] {
	return p.submitJobs(req, "")
}

// maxNodeAttempts bounds how many never-sent failures one prompt of a graph
// run may queue again under new IDs.
const maxNodeAttempts = 50

// runJobID gives one job of a graph run a stable ID derived from the run key,
// so running the same graph again finds the job it queued before instead of
// queueing (and paying for) it again. A job that ended without Muse ever
// taking it (no SessionID) moves on to the next attempt; one Muse took is
// returned as it is, its failure retried only from the queue.
func (p *Provider) runJobID(runKey string, parts ...any) (string, *MuseJob) {
	for attempt := range maxNodeAttempts {
		id := engine.RequestIDFor(runKey, attempt, parts...)
		job, ok := p.jobs.get(id)
		if !ok {
			return id, nil
		}
		if (job.Status == MuseJobFailed || job.Status == MuseJobCancelled) && job.SessionID == "" {
			continue
		}
		return id, &job
	}
	return uuid.NewString(), nil
}

// submitJobs queues one job per prompt and repeat. With a graph run key the
// job IDs are derived from it (runJobID); without one every job is new.
func (p *Provider) submitJobs(req MuseJobRequest, runKey string) models.Response[[]MuseJob] {
	if !req.Kind.valid() {
		return fail[[]MuseJob](errors.New("Loại tác vụ không hợp lệ."), models.CodeInvalidArgument)
	}
	if len(req.References) > 0 {
		caps, _ := p.knownCapabilities()
		if caps == nil {
			return fail[[]MuseJob](errLimitsUnknown, models.CodeUnavailable)
		}
		if len(req.References) > caps.MaxFiles {
			return fail[[]MuseJob](fmt.Errorf("Muse nhận tối đa %d ảnh mỗi tin nhắn.", caps.MaxFiles), models.CodeInvalidArgument)
		}
	}
	var missing []string
	for _, ref := range req.References {
		path, err := p.media.Path(ref)
		if err != nil {
			return fail[[]MuseJob](err, models.CodeInvalidArgument)
		}
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, filepath.Base(path))
		}
	}
	if len(missing) > 0 {
		return fail[[]MuseJob](fmt.Errorf("Ảnh tham chiếu không còn trong thư mục media (có thể đã được dọn vì lâu không dùng): %s. Hãy bỏ và chọn lại ảnh.", strings.Join(missing, ", ")), models.CodeInvalidArgument)
	}
	var prompts []string
	for _, prompt := range req.Prompts {
		if prompt = strings.TrimSpace(prompt); prompt != "" {
			prompts = append(prompts, prompt)
		}
	}
	if len(prompts) == 0 {
		return fail[[]MuseJob](errors.New("Hãy nhập prompt."), models.CodeInvalidArgument)
	}
	mode := req.Mode
	if mode == "" {
		mode = MuseJobBatch
	}
	if !mode.valid() {
		return fail[[]MuseJob](errors.New("Chế độ tác vụ không hợp lệ."), models.CodeInvalidArgument)
	}
	repeat := max(req.Repeat, 1)
	// The Hàng loạt screen makes one result per prompt; variants exist only in
	// Đơn. Graph nodes send no mode and keep their repeat.
	if req.Mode == MuseJobBatch && repeat > 1 {
		return fail[[]MuseJob](errors.New("Hàng loạt tạo một kết quả cho mỗi prompt; biến thể chỉ có ở Đơn."), models.CodeInvalidArgument)
	}
	if err := checkFormat(req.Kind, req.Ratio, req.Duration); err != nil {
		return fail[[]MuseJob](err, models.CodeInvalidArgument)
	}
	references := append([]string{}, req.References...)
	outputDir, first := "", 1
	if req.Mode == MuseJobBatch {
		if strings.TrimSpace(req.OutputDir) == "" {
			return fail[[]MuseJob](errors.New("Chưa chọn thư mục lưu."), models.CodeInvalidArgument)
		}
		dir, err := p.media.CheckDir(req.OutputDir)
		if err != nil {
			return fail[[]MuseJob](err, models.CodeInvalidArgument)
		}
		outputDir, first = p.batchFolder(req.Kind, dir)
	}

	jobs := make([]MuseJob, 0, len(prompts)*repeat)
	base := p.now().UTC()
	for pi, prompt := range prompts {
		for r := range repeat {
			id := uuid.NewString()
			if runKey != "" {
				var existing *MuseJob
				id, existing = p.runJobID(runKey, req.Kind, pi, r, prompt, references)
				if existing != nil {
					jobs = append(jobs, *existing)
					continue
				}
			}
			job := MuseJob{
				ID: id, Kind: req.Kind, Mode: mode, Prompt: prompt, Label: strings.TrimSpace(req.Label),
				References: references, Held: req.Held, Status: MuseJobQueued,
				OutputDir: outputDir, Index: first + pi, Ratio: req.Ratio, Duration: req.Duration,
				Files: []string{}, SavedFiles: []string{},
				// Distinct, ordered timestamps keep the batch in prompt order.
				CreatedAt: base.Add(time.Duration(len(jobs)) * time.Millisecond).Format("2006-01-02T15:04:05.000Z07:00"),
			}
			if err := p.jobs.add(job); err != nil {
				return fail[[]MuseJob](err, models.CodeInternal)
			}
			p.emit(events.EventMuseJobUpdated, job)
			jobs = append(jobs, job)
		}
	}
	p.trigger()
	return models.SuccessResponse(jobs)
}

// batchFolder picks the Hàng loạt folder under parent and the first number
// for prompts of kind (media.BatchFolder): an unfinished batch of this kind
// is joined, otherwise a new "snakemedia-muse-<kind>-<date-time>" is opened.
func (p *Provider) batchFolder(kind MuseJobKind, parent string) (string, int) {
	var items []media.BatchItem
	for _, j := range p.jobs.list() {
		if j.OutputDir != "" {
			items = append(items, media.BatchItem{Dir: j.OutputDir, Index: j.Index, CreatedAt: j.CreatedAt,
				Open: j.Kind == kind && !j.finished()})
		}
	}
	return media.BatchFolder(parent, "snakemedia-muse-"+string(kind), p.now(), items)
}

// ListJobs returns every job, newest first.
func (p *Provider) ListJobs() models.Response[[]MuseJob] {
	return models.SuccessResponse(sortedJobs(p.jobs.list()))
}

// CancelJob stops a queued or running job. A job Muse is already working on
// is cancelled there too, so it stops using the account's quota.
func (p *Provider) CancelJob(id string) models.Response[MuseJob] {
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if job.Status != MuseJobQueued && job.Status != MuseJobProcessing {
		return fail[MuseJob](errors.New("Tác vụ đã kết thúc."), models.CodeInvalidArgument)
	}
	p.mu.Lock()
	cancel := p.cancels[id]
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// A send in flight is cancelled on Muse by the runner once it returns.
	if job.SessionID != "" && !job.Sending {
		if acc, err := p.accounts.Record(job.AccountID); err == nil {
			// Tracked so Close waits for it; stopping the app cuts it short.
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				defer logger.Recover("huỷ chat Muse")
				ctx, done := context.WithTimeout(p.lifetime(), time.Minute)
				defer done()
				if page, err := p.pageFor(ctx, acc); err == nil {
					_ = page.Cancel(ctx, job.SessionID)
				}
			}()
		}
	}
	updated, err := p.updateJob(id, func(j *MuseJob) {
		j.Status, j.Step, j.Error, j.Sending = MuseJobCancelled, "", "Đã huỷ", false
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	return models.SuccessResponse(updated)
}

// RetryJob sends a failed or cancelled job again, in a new chat.
func (p *Provider) RetryJob(id string) models.Response[MuseJob] {
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if job.Status != MuseJobFailed && job.Status != MuseJobCancelled {
		return fail[MuseJob](errors.New("Chỉ chạy lại được tác vụ lỗi hoặc đã huỷ."), models.CodeInvalidArgument)
	}
	if p.runActive(id) {
		// The cancelled run may still be sending: its answer must land on
		// this job before the job starts over.
		return fail[MuseJob](errRunStopping, models.CodeInvalidArgument)
	}
	updated, err := p.updateJob(id, func(j *MuseJob) {
		resetForRetry(j)
		j.Attempts = 0
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(updated)
}

// RemoveJob deletes a job that is not running. Downloaded files stay in the
// media folder.
func (p *Provider) RemoveJob(id string) models.Response[bool] {
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[bool](errJobNotFound, models.CodeNotFound)
	}
	if job.Status == MuseJobProcessing {
		return fail[bool](errors.New("Tác vụ đang chạy; hãy huỷ trước khi xoá."), models.CodeInvalidArgument)
	}
	if err := p.jobs.remove(id); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	p.sweepReferences()
	return models.SuccessResponse(true)
}

// RunJobs removes the Held flag from queued jobs so the scheduler picks them up.
func (p *Provider) RunJobs(ids []string) models.Response[int] {
	count := 0
	for _, id := range ids {
		if j, ok := p.jobs.get(id); ok && j.Held {
			_, err := p.updateJob(id, func(job *MuseJob) {
				job.Held = false
			})
			if err == nil {
				count++
			}
		}
	}
	if count > 0 {
		p.trigger()
	}
	return models.SuccessResponse(count)
}

// inScope reports whether job is of kind and mode; an empty kind or mode
// matches every job, and a job without a mode counts as MuseJobBatch.
func inScope(job MuseJob, kind MuseJobKind, mode MuseJobMode) bool {
	jobMode := job.Mode
	if jobMode == "" {
		jobMode = MuseJobBatch
	}
	return (kind == "" || job.Kind == kind) && (mode == "" || jobMode == mode)
}

// HoldQueued puts every unstarted queued job of kind and mode (all when empty) on hold.
func (p *Provider) HoldQueued(kind MuseJobKind, mode MuseJobMode) models.Response[int] {
	count := 0
	for _, j := range p.jobs.list() {
		if j.Status == MuseJobQueued && !j.Held && inScope(j, kind, mode) {
			_, err := p.updateJob(j.ID, func(job *MuseJob) {
				job.Held = true
			})
			if err == nil {
				count++
			}
		}
	}
	return models.SuccessResponse(count)
}

// RetryFailed queues every failed or cancelled job of kind and mode (all when empty)
// again and returns how many it queued. Jobs Muse already accepted (they have a
// chat) or may have accepted (the send's answer was lost) are skipped: sending
// them again could pay a second time, so only an explicit RetryJob on one of
// them sends it again.
func (p *Provider) RetryFailed(kind MuseJobKind, mode MuseJobMode) models.Response[int] {
	count := 0
	for _, j := range p.jobs.list() {
		if (j.Status == MuseJobFailed || j.Status == MuseJobCancelled) && !j.mayBePaid() && inScope(j, kind, mode) && !p.runActive(j.ID) {
			_, err := p.updateJob(j.ID, func(job *MuseJob) {
				resetForRetry(job)
				job.Attempts, job.Held = 0, false
			})
			if err == nil {
				count++
			}
		}
	}
	if count > 0 {
		p.trigger()
	}
	return models.SuccessResponse(count)
}

// ClearFinished deletes the succeeded jobs of kind and mode (all when empty) from the
// journal. Their files stay in the media and output folders.
func (p *Provider) ClearFinished(kind MuseJobKind, mode MuseJobMode) models.Response[int] {
	n, err := p.jobs.removeWhere(func(j MuseJob) bool {
		return j.Status == MuseJobSucceeded && inScope(j, kind, mode)
	})
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.sweepReferences()
	p.emit(events.EventMuseJobsChanged, nil)
	return models.SuccessResponse(n)
}

// RemoveJobs deletes the specified jobs that are not actively processing.
func (p *Provider) RemoveJobs(ids []string) models.Response[int] {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	n, err := p.jobs.removeWhere(func(j MuseJob) bool {
		return idSet[j.ID] && j.Status != MuseJobProcessing
	})
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.sweepReferences()
	p.emit(events.EventMuseJobsChanged, nil)
	return models.SuccessResponse(n)
}

// EditAndRetry updates the prompt of an unstarted or ended job and queues it.
func (p *Provider) EditAndRetry(id, prompt string) models.Response[MuseJob] {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fail[MuseJob](errors.New("Prompt không được để trống."), models.CodeInvalidArgument)
	}
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if job.Status == MuseJobProcessing {
		return fail[MuseJob](errors.New("Tác vụ đang chạy, hãy huỷ trước khi sửa."), models.CodeInvalidArgument)
	}
	if p.runActive(id) {
		return fail[MuseJob](errRunStopping, models.CodeInvalidArgument)
	}
	// A job still waiting keeps its hold; an ended one runs again.
	held := job.Status == MuseJobQueued && job.Held
	updated, err := p.updateJob(id, func(j *MuseJob) {
		resetForRetry(j)
		j.Prompt, j.Attempts, j.Held = prompt, 0, held
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(updated)
}

// OpenOutputFolder opens the folder the batch form saves into.
func (p *Provider) OpenOutputFolder() models.Response[bool] {
	dir := p.cfg.Get().Muse.OutputDir
	if dir == "" {
		dir = p.defaultOutputDir
	}
	_ = os.MkdirAll(dir, 0o755)
	if err := utils.OpenFolder(dir); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}
