package muse

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	// ClipSeconds is one Muse clip; a longer duration is made as several clips
	// joined in one chat.
	ClipSeconds int `json:"clipSeconds"`
	// MaxBoardFrames caps a storyboard; MaxAssetImages is 0 (unlimited in Vault).
	MaxBoardFrames    int   `json:"maxBoardFrames"`
	MaxAssetImages    int   `json:"maxAssetImages"`
	InlineBudgetBytes int64 `json:"inlineBudgetBytes"`
}

// MuseJobRequest queues Repeat jobs per non-empty prompt with shared settings.
type MuseJobRequest struct {
	// SubmissionID identifies one user action; repeating it returns its saved jobs.
	SubmissionID string      `json:"submissionId,omitempty"`
	Kind         MuseJobKind `json:"kind"`
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
	// Project narrows @tag matching in MuseAsset Vault (plus global _chung assets).
	Project string `json:"project,omitempty"`
	// RefMode says how References go to the prompts; empty shares them all.
	RefMode MuseRefMode `json:"refMode,omitempty"`
	// Assets are asset IDs attached to every prompt (a fixed model or
	// product); assets a prompt names (@tag) are attached to that prompt.
	Assets []string `json:"assets,omitempty"`
	// Storyboard makes every video prompt as a storyboard image job and a
	// video job that waits for it (storyboard.go).
	Storyboard *MuseStoryboard `json:"storyboard,omitempty"`
}

// MuseStoryboard asks for Frames scene images before each video; with
// Review the video waits on hold until the user runs it.
type MuseStoryboard struct {
	Frames int  `json:"frames"`
	Review bool `json:"review"`
}

// MuseJobsFormatRequest updates Ratio and/or Duration on unstarted jobs of
// kind and mode (or the named IDs). Nil fields leave the existing value intact;
// empty ratio or 0 duration switches back to prompt-defined format.
type MuseJobsFormatRequest struct {
	Kind     MuseJobKind `json:"kind"`
	Mode     MuseJobMode `json:"mode"`
	IDs      []string    `json:"ids,omitempty"`
	Ratio    *string     `json:"ratio,omitempty"`
	Duration *int        `json:"duration,omitempty"`
}

// MuseJobsConfigRequest updates output folder and/or reference attachment settings
// on unstarted queued jobs of kind and mode so post-queue edits take effect.
type MuseJobsConfigRequest struct {
	Kind       MuseJobKind `json:"kind"`
	Mode       MuseJobMode `json:"mode"`
	OutputDir  *string     `json:"outputDir,omitempty"`
	SyncRefs   bool        `json:"syncRefs,omitempty"`
	References []string    `json:"references,omitempty"`
	RefMode    MuseRefMode `json:"refMode,omitempty"`
	Project    string      `json:"project,omitempty"`
	Assets     []string    `json:"assets,omitempty"`
}

// MusePromptCheck is what a prompt will attach and the hints about it.
type MusePromptCheck struct {
	Tags           []string `json:"tags"`
	Images         int      `json:"images"`
	Warnings       []string `json:"warnings"`
	EstimatedBytes int64    `json:"estimatedBytes,omitempty"`
	BudgetBytes    int64    `json:"budgetBytes,omitempty"`
}

// MusePromptPreview is the exact composed message that would be sent to Muse
// for one prompt, along with resolved asset tags, attachment count, and hints.
type MusePromptPreview struct {
	Prompt            string   `json:"prompt"`
	Message           string   `json:"message"`
	StoryboardMessage string   `json:"storyboardMessage,omitempty"`
	Tags              []string `json:"tags"`
	Images            int      `json:"images"`
	Warnings          []string `json:"warnings"`
	EstimatedBytes    int64    `json:"estimatedBytes,omitempty"`
	BudgetBytes       int64    `json:"budgetBytes,omitempty"`
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
	if p.cfg.Get().Muse.UseAPI {
		latest := &MuseCapabilities{
			MaxFileBytes: 100 * 1024 * 1024,
			MaxFiles:     4,
			ImageTypes: []MuseFileType{
				{Mime: "image/png", Extensions: []string{".png"}},
				{Mime: "image/jpeg", Extensions: []string{".jpg", ".jpeg"}},
				{Mime: "image/webp", Extensions: []string{".webp"}},
			},
			Models: []MuseModel{
				{ID: "sora-2", Name: "Sora 2 (Video)"},
				{ID: "muse-image", Name: "Muse Image"},
				{ID: "muse-chat", Name: "Muse Chat"},
			},
			CheckedAt: p.now().UTC().Format(time.RFC3339),
		}
		return latest, latest.Models
	}
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
			Ratios: museRatios, Durations: museDurations, ClipSeconds: museClipSeconds,
			MaxBoardFrames: maxBoardFrames, MaxAssetImages: maxAssetImages, InlineBudgetBytes: museInlineRequestBudget})
	}
	return models.SuccessResponse(MuseOptions{
		Known: true, MaxFileBytes: caps.MaxFileBytes, MaxFiles: caps.MaxFiles,
		ImageExtensions: caps.extensions(), Models: offered, CheckedAt: caps.CheckedAt,
		JournalWarning: warning, Ratios: museRatios, Durations: museDurations, ClipSeconds: museClipSeconds,
		MaxBoardFrames: maxBoardFrames, MaxAssetImages: maxAssetImages, InlineBudgetBytes: museInlineRequestBudget,
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
	return p.importImages(paths, referencePrefix)
}

// importImages copies image files into the media folder under prefix,
// checked against Muse's limits.
func (p *Provider) importImages(paths []string, prefix string) models.Response[[]MuseReference] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[[]MuseReference](errLimitsUnknown, models.CodeUnavailable)
	}
	out := make([]MuseReference, 0, len(paths))
	for _, path := range paths {
		ref, err := p.importOneImage(caps, path, prefix)
		if err != nil {
			code := models.CodeInvalidArgument
			if errors.Is(err, errImageWrite) {
				code = models.CodeInternal
			}
			return fail[[]MuseReference](err, code)
		}
		out = append(out, ref)
	}
	return models.SuccessResponse(out)
}

var errImageWrite = errors.New("image write failed")

// checkImageFile validates that path is a regular image file Muse accepts.
func checkImageFile(caps *MuseCapabilities, path string) (mime, ext string, size int64, err error) {
	ext = strings.ToLower(filepath.Ext(path))
	mime, ok := caps.imageMime(ext)
	if !ok {
		return "", "", 0, fmt.Errorf("Muse không nhận định dạng của %s (nhận %s).", filepath.Base(path), strings.Join(caps.extensions(), ", "))
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", 0, fmt.Errorf("Không đọc được %s.", filepath.Base(path))
	}
	if info.Size() > caps.MaxFileBytes {
		return "", "", 0, fmt.Errorf("%s lớn hơn %d MB, giới hạn một file của Muse.", filepath.Base(path), caps.MaxFileBytes>>20)
	}
	return mime, ext, info.Size(), nil
}

// importOneImage validates path against caps and stores a copy in p.media.
func (p *Provider) importOneImage(caps *MuseCapabilities, path, prefix string) (MuseReference, error) {
	mime, ext, size, err := checkImageFile(caps, path)
	if err != nil {
		return MuseReference{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return MuseReference{}, fmt.Errorf("%w: %w", errImageWrite, err)
	}
	url, err := p.media.Put(prefix+uuid.NewString(), mime, ext, data)
	if err != nil {
		return MuseReference{}, fmt.Errorf("%w: %w", errImageWrite, err)
	}
	return MuseReference{LocalURL: url, FileName: filepath.Base(path), Size: size}, nil
}

// SubmitJobs queues Repeat jobs (at least one) for every non-empty prompt.
func (p *Provider) SubmitJobs(req MuseJobRequest) models.Response[[]MuseJob] {
	p.submitMu.Lock()
	defer p.submitMu.Unlock()
	if req.SubmissionID != "" {
		hash := submissionHash(req)
		var existing []MuseJob
		for _, job := range p.jobs.list() {
			if job.SubmissionID == req.SubmissionID {
				if job.SubmissionHash != hash {
					return fail[[]MuseJob](errors.New("Lần gửi này đã có nội dung khác. Hãy gửi lại bằng một lần tạo mới."), models.CodeInvalidArgument)
				}
				existing = append(existing, job)
			}
		}
		if len(existing) > 0 {
			p.trigger()
			return models.SuccessResponse(existing)
		}
		if pending, ok := p.failedSubmissions[req.SubmissionID]; ok {
			if pending.hash != hash {
				return fail[[]MuseJob](errors.New("Lần gửi này đã có nội dung khác. Hãy gửi lại bằng một lần tạo mới."), models.CodeInvalidArgument)
			}
			trimmed, err := p.jobs.add(pending.fresh...)
			if err != nil {
				return fail[[]MuseJob](err, models.CodeInternal)
			}
			delete(p.failedSubmissions, req.SubmissionID)
			p.publishSubmittedJobs(pending.fresh, trimmed)
			return models.SuccessResponse(slices.Clone(pending.out))
		}
	}
	return p.submitJobs(req, "")
}

func submissionHash(req MuseJobRequest) string {
	data, _ := json.Marshal(req)
	return fmt.Sprintf("%x", sha256.Sum256(data))
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
	if !req.RefMode.valid() {
		return fail[[]MuseJob](errors.New("Cách gắn ảnh không hợp lệ."), models.CodeInvalidArgument)
	}
	fixed, err := p.assetsByID(req.Assets)
	if err != nil {
		return fail[[]MuseJob](err, models.CodeInvalidArgument)
	}
	board := req.Storyboard
	if board != nil {
		if req.Kind != MuseJobVideo {
			return fail[[]MuseJob](errors.New("Storyboard chỉ dùng cho video."), models.CodeInvalidArgument)
		}
		if board.Frames < 1 || board.Frames > maxBoardFrames {
			return fail[[]MuseJob](fmt.Errorf("Storyboard có 1 đến %d khung.", maxBoardFrames), models.CodeInvalidArgument)
		}
	}
	if len(req.References) > 0 {
		caps, _ := p.knownCapabilities()
		if caps == nil {
			return fail[[]MuseJob](errLimitsUnknown, models.CodeUnavailable)
		}
	}
	if err := p.checkReferenceFiles(req.References); err != nil {
		return fail[[]MuseJob](err, models.CodeInvalidArgument)
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

	plans, err := p.planPrompts(req, prompts, fixed, runKey)
	if err != nil {
		return fail[[]MuseJob](err, models.CodeInvalidArgument)
	}

	jobs := make([]MuseJob, 0, len(prompts)*repeat)
	base := p.now().UTC()
	// Distinct, ordered timestamps keep the batch in prompt order.
	stamp := func() string {
		return base.Add(time.Duration(len(jobs)) * time.Millisecond).Format("2006-01-02T15:04:05.000Z07:00")
	}
	// New jobs are written together once the batch is built: one journal
	// write for the whole batch, and none of it if that write fails.
	var fresh []MuseJob
	hash := submissionHash(req)
	add := func(job MuseJob) {
		if req.SubmissionID != "" {
			job.SubmissionID, job.SubmissionHash = req.SubmissionID, hash
		}
		// A job without references goes to the UI as [] (never null).
		normalizeJob(&job)
		fresh = append(fresh, job)
		jobs = append(jobs, job)
	}
	label := strings.TrimSpace(req.Label)
	boardFrames, boardReview := 0, false
	if board != nil {
		boardFrames, boardReview = board.Frames, board.Review
	}
	for pi, prompt := range prompts {
		refs, info := plans[pi].refs, plans[pi].info
		for r := range repeat {
			id := uuid.NewString()
			if runKey != "" {
				var existing *MuseJob
				id, existing = p.runJobID(runKey, req.Kind, pi, r, prompt, refs, info, req.Ratio, req.Duration, boardFrames, boardReview)
				if existing != nil {
					if existing.After != "" && len(existing.Frames) == 0 {
						if bj, ok := p.jobs.get(existing.After); ok {
							jobs = append(jobs, bj)
						}
					}
					jobs = append(jobs, *existing)
					continue
				}
			}
			job := MuseJob{
				ID: id, Kind: req.Kind, Mode: mode, Prompt: prompt, Label: label,
				References: slices.Clone(refs), RefInfo: info,
				Held: req.Held, Status: MuseJobQueued,
				OutputDir: outputDir, Index: first + pi, Ratio: req.Ratio, Duration: req.Duration,
				Files: []string{}, SavedFiles: []string{},
			}
			if board != nil {
				var boardJob MuseJob
				var existingBoard *MuseJob
				boardID := uuid.NewString()
				if runKey != "" {
					boardID, existingBoard = p.runJobID(runKey, "storyboard", pi, r, prompt, refs, info, req.Ratio, board.Frames)
				}
				job.After, job.BoardFrames, job.Review = boardID, board.Frames, board.Review
				if existingBoard != nil && existingBoard.Status == MuseJobSucceeded && len(existingBoard.Files) > 0 {
					frames := slices.Clone(existingBoard.Files)
					if len(frames) > board.Frames {
						frames = frames[:board.Frames]
					}
					job.Frames = frames
					job.Held = req.Held || board.Review
					job.Step = "Đã có storyboard"
					if board.Review {
						job.Paused = true
					}
				} else {
					if existingBoard != nil {
						jobs = append(jobs, *existingBoard)
					} else {
						boardJob = storyboardJob(job, board.Frames)
						boardJob.ID = boardID
						boardJob.CreatedAt = stamp()
						add(boardJob)
					}
					job.Held = req.Held || board.Review
					job.Step = "Chờ storyboard"
				}
			}
			job.CreatedAt = stamp()
			add(job)
		}
	}
	if len(fresh) > 0 {
		trimmed, err := p.jobs.add(fresh...)
		if err != nil {
			if req.SubmissionID != "" {
				if p.failedSubmissions == nil {
					p.failedSubmissions = make(map[string]pendingSubmission)
				} else if len(p.failedSubmissions) >= 64 {
					clear(p.failedSubmissions)
				}
				p.failedSubmissions[req.SubmissionID] = pendingSubmission{
					hash:  hash,
					fresh: slices.Clone(fresh),
					out:   slices.Clone(jobs),
				}
			}
			return fail[[]MuseJob](err, models.CodeInternal)
		}
		p.publishSubmittedJobs(fresh, trimmed)
	} else {
		p.trigger()
	}
	return models.SuccessResponse(jobs)
}

func (p *Provider) publishSubmittedJobs(fresh []MuseJob, trimmed []string) {
	if len(trimmed) > 0 {
		p.emit(events.EventMuseJobsRemoved, trimmed)
		p.sweepUnused()
	}
	for _, job := range fresh {
		p.emit(events.EventMuseJobUpdated, job)
	}
	p.trigger()
}

// batchFolder picks the Hàng loạt folder under parent and the first number
// for prompts of kind (media.BatchFolder): an unfinished batch of this kind
// is joined, otherwise a new "snakemedia-muse-<kind>-<date-time>" is opened.
func (p *Provider) batchFolder(kind MuseJobKind, parent string) (string, int) {
	var items []media.BatchItem
	for _, j := range p.jobs.list() {
		if j.OutputDir != "" {
			items = append(items, media.BatchItem{Dir: j.OutputDir, Index: j.Index, CreatedAt: j.CreatedAt,
				Open: j.screenKind() == kind && !j.finished()})
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
		return fail[MuseJob](errJobEnded, models.CodeInvalidArgument)
	}
	p.mu.Lock()
	cancel := p.cancels[id]
	_, activeRun := p.running[id]
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
					closePage(page)
				}
			}()
		}
	}
	ended := false
	updated, err := p.updateJob(id, func(j *MuseJob) {
		// The run may have finished since the check above: its answer stands.
		if ended = j.finished(); ended {
			return
		}
		j.Status, j.Step, j.Error, j.Held, j.Paused = MuseJobCancelled, "", "Đã huỷ", false, false
		// Preserve Sending/PreSendTracked while the active run finishes in-flight
		// send(), so a crash mid-send still triggers SendUnknown reconciliation on restart.
		if !activeRun || !j.Sending {
			j.Sending, j.PreSendTracked, j.PreSendSessions = false, false, nil
		}
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	if ended {
		return fail[MuseJob](errJobEnded, models.CodeInvalidArgument)
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
	// A cancelled run may still be sending: its answer must land on this job
	// before the job starts over.
	updated, err := p.updateIdle(id, func(j MuseJob) bool {
		return j.Status == MuseJobFailed || j.Status == MuseJobCancelled
	}, func(j *MuseJob) {
		resetForRetry(j)
		j.Attempts, j.Held, j.Paused = 0, false, false
	})
	if err != nil {
		return fail[MuseJob](err, idleCode(err))
	}
	p.trigger()
	return models.SuccessResponse(updated)
}

// RemoveJob deletes a job that is not running. Downloaded files stay in the
// media folder.
func (p *Provider) RemoveJob(id string) models.Response[bool] {
	p.mu.Lock()
	_, active := p.running[id]
	if active {
		p.mu.Unlock()
		return fail[bool](errors.New("Tác vụ đang chạy; hãy huỷ trước khi xoá."), models.CodeInvalidArgument)
	}
	job, ok := p.jobs.get(id)
	if !ok {
		p.mu.Unlock()
		return fail[bool](errJobNotFound, models.CodeNotFound)
	}
	if job.Status == MuseJobProcessing {
		p.mu.Unlock()
		return fail[bool](errors.New("Tác vụ đang chạy; hãy huỷ trước khi xoá."), models.CodeInvalidArgument)
	}
	err := p.jobs.remove(id)
	p.mu.Unlock()
	if err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	p.sweepUnused()
	p.releaseWaiting()
	return models.SuccessResponse(true)
}

// RunJobs removes the Held/Paused flags from queued jobs so the scheduler picks them up.
func (p *Provider) RunJobs(ids []string) models.Response[int] {
	changed := p.updateJobs(ids, func(job *MuseJob) bool {
		if !job.Held && !job.Paused {
			return false
		}
		job.Held, job.Paused = false, false
		return true
	})
	if len(changed) > 0 {
		p.trigger()
	}
	return models.SuccessResponse(len(changed))
}

// RetryJobs queues ended jobs (including jobs Muse may already have accepted)
// again in a new chat in one atomic write, moving them into Chờ chạy.
func (p *Provider) RetryJobs(ids []string) models.Response[int] {
	if len(ids) == 0 {
		return models.SuccessResponse(0)
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	p.mu.Lock()
	var eligible []string
	for _, j := range p.jobs.list() {
		if _, active := p.running[j.ID]; active || !want[j.ID] {
			continue
		}
		if j.Status == MuseJobFailed || j.Status == MuseJobCancelled {
			eligible = append(eligible, j.ID)
		}
	}
	var changed []MuseJob
	var err error
	if len(eligible) > 0 {
		changed, err = p.jobs.updateMany(eligible, func(j *MuseJob) bool {
			if _, active := p.running[j.ID]; active {
				return false
			}
			if j.Status != MuseJobFailed && j.Status != MuseJobCancelled {
				return false
			}
			resetForRetry(j)
			j.Attempts, j.Held, j.Paused = 0, false, false
			return true
		})
	}
	p.mu.Unlock()
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	for _, job := range changed {
		p.emit(events.EventMuseJobUpdated, job)
	}
	if len(changed) > 0 {
		p.trigger()
	}
	return models.SuccessResponse(len(changed))
}

// UpdateJobsFormat updates Ratio and/or Duration on unstarted jobs that Muse
// has not accepted yet, so format changes after queueing prompts take effect.
func (p *Provider) UpdateJobsFormat(req MuseJobsFormatRequest) models.Response[int] {
	if req.Ratio == nil && req.Duration == nil {
		return models.SuccessResponse(0)
	}
	byID := len(req.IDs) > 0
	if !byID {
		if !req.Kind.valid() {
			return fail[int](errors.New("Loại tác vụ không hợp lệ."), models.CodeInvalidArgument)
		}
		if !req.Mode.valid() {
			return fail[int](errors.New("Chế độ tác vụ không hợp lệ."), models.CodeInvalidArgument)
		}
	}
	if req.Kind != "" {
		ratio := ""
		if req.Ratio != nil {
			ratio = *req.Ratio
		}
		duration := 0
		if req.Duration != nil {
			duration = *req.Duration
		}
		if err := checkFormat(req.Kind, ratio, duration); err != nil {
			return fail[int](err, models.CodeInvalidArgument)
		}
	}

	p.mu.Lock()
	var candidates []MuseJob
	if byID {
		snap := p.jobs.snapshot(req.IDs)
		candidates = make([]MuseJob, 0, len(snap))
		for _, id := range req.IDs {
			if j, ok := snap[id]; ok {
				candidates = append(candidates, j)
			}
		}
	} else {
		candidates = p.jobs.listActive()
	}
	allow := func(j MuseJob) bool {
		if byID {
			return j.Status == MuseJobQueued || j.Status == MuseJobFailed || j.Status == MuseJobCancelled
		}
		return j.Status == MuseJobQueued && inScope(j, req.Kind, req.Mode)
	}
	return p.commitUnstartedLocked(candidates, allow, func(j *MuseJob) bool {
		updated := false
		if req.Ratio != nil {
			wantRatio := *req.Ratio
			if j.Kind == MuseJobImage && j.BoardFrames > 0 {
				// Keep storyboard image ratios within what Muse offers for images.
				if wantRatio != "" {
					if _, ok := ratioFor(wantRatio, MuseJobImage); !ok {
						wantRatio = ""
					}
				}
			} else if wantRatio != "" {
				if _, ok := ratioFor(wantRatio, j.Kind); !ok {
					wantRatio = j.Ratio
				}
			}
			if j.Ratio != wantRatio {
				j.Ratio = wantRatio
				updated = true
			}
		}
		if req.Duration != nil && j.Kind == MuseJobVideo {
			wantDur := *req.Duration
			if wantDur == 0 || slices.Contains(museDurations, wantDur) {
				if j.Duration != wantDur {
					j.Duration = wantDur
					updated = true
				}
			}
		}
		return updated
	})
}

// UpdateJobsConfig updates OutputDir and/or planned references on unstarted
// queued jobs of kind and mode when the user picks them after queueing prompts.
func (p *Provider) UpdateJobsConfig(req MuseJobsConfigRequest) models.Response[int] {
	if req.OutputDir == nil && !req.SyncRefs {
		return models.SuccessResponse(0)
	}
	if !req.Kind.valid() {
		return fail[int](errors.New("Loại tác vụ không hợp lệ."), models.CodeInvalidArgument)
	}
	if !req.Mode.valid() {
		return fail[int](errors.New("Chế độ tác vụ không hợp lệ."), models.CodeInvalidArgument)
	}
	var checkedDir string
	if req.OutputDir != nil {
		if req.Mode != MuseJobBatch {
			return fail[int](errors.New("Chỉ Hàng loạt mới có thư mục lưu."), models.CodeInvalidArgument)
		}
		trimmed := strings.TrimSpace(*req.OutputDir)
		if trimmed == "" {
			return fail[int](errors.New("Chưa chọn thư mục lưu."), models.CodeInvalidArgument)
		}
		dir, err := p.media.CheckDir(trimmed)
		if err != nil {
			return fail[int](err, models.CodeInvalidArgument)
		}
		checkedDir = dir
	}
	var fixed []MuseAsset
	if req.SyncRefs {
		if !req.RefMode.valid() {
			return fail[int](errors.New("Cách gắn ảnh không hợp lệ."), models.CodeInvalidArgument)
		}
		var err error
		if fixed, err = p.assetsByID(req.Assets); err != nil {
			return fail[int](err, models.CodeInvalidArgument)
		}
		if err := p.checkReferenceFiles(req.References); err != nil {
			return fail[int](err, models.CodeInvalidArgument)
		}
		if len(req.References) > 0 {
			caps, _ := p.knownCapabilities()
			if caps == nil {
				return fail[int](errLimitsUnknown, models.CodeUnavailable)
			}
		}
	}

	p.mu.Lock()
	var eligible []MuseJob
	byID := make(map[string]MuseJob)
	for _, j := range p.jobs.listActive() {
		if _, active := p.running[j.ID]; active || j.mayBePaid() {
			continue
		}
		if j.Status == MuseJobQueued && inScope(j, req.Kind, req.Mode) {
			eligible = append(eligible, j)
			byID[j.ID] = j
		}
	}
	p.mu.Unlock()
	if len(eligible) == 0 {
		return models.SuccessResponse(0)
	}

	// Storyboard image jobs (referenced via video.After) inherit their paired
	// video job's prompt ordinal and file index instead of advancing counts twice.
	isLinkedBoard := make(map[string]bool)
	for _, j := range eligible {
		if j.After != "" {
			if _, ok := byID[j.After]; ok {
				isLinkedBoard[j.After] = true
			}
		}
	}

	var prompts []string
	var extraFrames []int
	ordinalByID := make(map[string]int, len(eligible))
	for _, j := range eligible {
		if isLinkedBoard[j.ID] {
			continue
		}
		pi := len(prompts)
		prompts = append(prompts, j.Prompt)
		extraFrames = append(extraFrames, j.BoardFrames)
		ordinalByID[j.ID] = pi
		if j.After != "" && isLinkedBoard[j.After] {
			ordinalByID[j.After] = pi
		}
	}

	var planByID map[string]promptPlan
	if req.SyncRefs {
		plans, err := p.planPromptSpecs(prompts, req.Project, req.References, req.RefMode, fixed, p.assets.List(), extraFrames)
		if err != nil {
			return fail[int](err, models.CodeInvalidArgument)
		}
		planByID = make(map[string]promptPlan, len(eligible))
		for _, j := range eligible {
			pi := ordinalByID[j.ID]
			planByID[j.ID] = plans[pi]
		}
	}

	type targetLocation struct {
		dir   string
		index int
	}
	dirByID := make(map[string]targetLocation)
	if req.OutputDir != nil {
		needsMove := make(map[string]bool, len(eligible))
		for _, j := range eligible {
			if j.OutputDir == "" || !sameDirPath(filepath.Dir(j.OutputDir), checkedDir) {
				needsMove[j.ID] = true
			}
		}
		if len(needsMove) > 0 {
			batchDir, firstIdx := p.batchFolder(req.Kind, checkedDir)
			nextOffset := 0
			for _, j := range eligible {
				if isLinkedBoard[j.ID] {
					continue
				}
				linkedMove := j.After != "" && isLinkedBoard[j.After] && needsMove[j.After]
				if !needsMove[j.ID] && !linkedMove {
					continue
				}
				loc := targetLocation{dir: batchDir, index: firstIdx + nextOffset}
				nextOffset++
				if needsMove[j.ID] {
					dirByID[j.ID] = loc
				}
				if linkedMove {
					dirByID[j.After] = loc
				}
			}
		}
	}
	if len(dirByID) == 0 && !req.SyncRefs {
		return models.SuccessResponse(0)
	}

	p.mu.Lock()
	candidates := make([]MuseJob, 0, len(eligible))
	for _, j := range eligible {
		candidates = append(candidates, j)
	}
	allow := func(j MuseJob) bool {
		return j.Status == MuseJobQueued && inScope(j, req.Kind, req.Mode)
	}
	return p.commitUnstartedLocked(candidates, allow, func(j *MuseJob) bool {
		updated := false
		if loc, ok := dirByID[j.ID]; ok {
			if j.OutputDir != loc.dir || j.Index != loc.index {
				j.OutputDir, j.Index = loc.dir, loc.index
				updated = true
			}
		}
		if plan, ok := planByID[j.ID]; ok {
			if !slices.Equal(j.References, plan.refs) || !slices.Equal(j.RefInfo, plan.info) {
				j.References = slices.Clone(plan.refs)
				if j.References == nil {
					j.References = []string{}
				}
				j.RefInfo = slices.Clone(plan.info)
				updated = true
			}
		}
		return updated
	})
}

// commitUnstartedLocked updates each non-running, unpaid candidate passing allow
// and releases p.mu before emitting JobUpdated events.
func (p *Provider) commitUnstartedLocked(candidates []MuseJob, allow func(MuseJob) bool, apply func(*MuseJob) bool) models.Response[int] {
	var ids []string
	for _, j := range candidates {
		if _, active := p.running[j.ID]; active || j.mayBePaid() {
			continue
		}
		if allow(j) {
			ids = append(ids, j.ID)
		}
	}
	var changed []MuseJob
	var updateErr error
	if len(ids) > 0 {
		changed, updateErr = p.jobs.updateMany(ids, func(j *MuseJob) bool {
			if _, active := p.running[j.ID]; active || j.mayBePaid() || !allow(*j) {
				return false
			}
			return apply(j)
		})
	}
	p.mu.Unlock()
	if updateErr != nil {
		return fail[int](updateErr, models.CodeInternal)
	}
	for _, job := range changed {
		p.emit(events.EventMuseJobUpdated, job)
	}
	return models.SuccessResponse(len(changed))
}

func (p *Provider) checkReferenceFiles(refs []string) error {
	var missing []string
	for _, ref := range refs {
		path, err := p.media.OpenPath(ref)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, filepath.Base(path))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("Ảnh tham chiếu không còn trong thư mục media (có thể đã được dọn vì lâu không dùng): %s. Hãy bỏ và chọn lại ảnh.", strings.Join(missing, ", "))
	}
	return nil
}

func sameDirPath(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// inScope reports whether job is of kind and mode; an empty kind or mode
// matches every job, and a job without a mode counts as MuseJobBatch.
func inScope(job MuseJob, kind MuseJobKind, mode MuseJobMode) bool {
	jobMode := job.Mode
	if jobMode == "" {
		jobMode = MuseJobBatch
	}
	return (kind == "" || job.screenKind() == kind) && (mode == "" || jobMode == mode)
}

// HoldQueued puts every unstarted queued job of kind and mode (all when empty) on hold.
func (p *Provider) HoldQueued(kind MuseJobKind, mode MuseJobMode) models.Response[int] {
	holdable := func(j MuseJob) bool {
		return j.Status == MuseJobQueued && !j.Held && inScope(j, kind, mode)
	}
	var ids []string
	for _, j := range p.jobs.listActive() {
		if holdable(j) {
			ids = append(ids, j.ID)
		}
	}
	changed := p.updateJobs(ids, func(job *MuseJob) bool {
		if !holdable(*job) {
			return false
		}
		job.Held, job.Paused = true, true
		return true
	})
	return models.SuccessResponse(len(changed))
}

// RetryFailed queues every failed or cancelled job of kind and mode (all when empty)
// again and returns how many it queued. Jobs Muse already accepted (they have a
// chat) or may have accepted (the send's answer was lost) are skipped: sending
// them again could pay a second time, so only an explicit RetryJob on one of
// them sends it again.
func (p *Provider) RetryFailed(kind MuseJobKind, mode MuseJobMode) models.Response[int] {
	retryable := func(j MuseJob) bool {
		return (j.Status == MuseJobFailed || j.Status == MuseJobCancelled) && !j.mayBePaid() && inScope(j, kind, mode)
	}
	var ids []string
	for _, j := range p.jobs.list() {
		if retryable(j) && !p.runActive(j.ID) {
			ids = append(ids, j.ID)
		}
	}
	changed := p.updateJobs(ids, func(job *MuseJob) bool {
		if !retryable(*job) {
			return false
		}
		resetForRetry(job)
		job.Attempts, job.Held, job.Paused = 0, false, false
		return true
	})
	if len(changed) > 0 {
		p.trigger()
	}
	return models.SuccessResponse(len(changed))
}

// ClearFinished deletes the succeeded jobs of kind and mode (all when empty) from the
// journal. Their files stay in the media and output folders.
func (p *Provider) ClearFinished(kind MuseJobKind, mode MuseJobMode) models.Response[int] {
	removed, err := p.jobs.removeWhere(func(j MuseJob) bool {
		return j.Status == MuseJobSucceeded && inScope(j, kind, mode)
	})
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.sweepUnused()
	p.releaseWaiting()
	p.emit(events.EventMuseJobsRemoved, removed)
	return models.SuccessResponse(len(removed))
}

// RemoveJobs deletes the specified jobs that are not actively processing or staggered in flight.
func (p *Provider) RemoveJobs(ids []string) models.Response[int] {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	p.mu.Lock()
	removed, err := p.jobs.removeWhere(func(j MuseJob) bool {
		_, active := p.running[j.ID]
		return idSet[j.ID] && !active && j.Status != MuseJobProcessing
	})
	p.mu.Unlock()
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.sweepUnused()
	p.releaseWaiting()
	p.emit(events.EventMuseJobsRemoved, removed)
	return models.SuccessResponse(len(removed))
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
	// A job still waiting keeps its hold; an ended one runs again. A job the
	// scheduler started since the check above is left to its run.
	updated, err := p.updateIdle(id, func(j MuseJob) bool {
		return j.Status == MuseJobQueued || j.finished()
	}, func(j *MuseJob) {
		held := j.Status == MuseJobQueued && j.Held
		paused := j.Status == MuseJobQueued && j.Paused
		resetForRetry(j)
		j.Prompt, j.Attempts, j.Held, j.Paused = prompt, 0, held, paused
	})
	if err != nil {
		return fail[MuseJob](err, idleCode(err))
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
