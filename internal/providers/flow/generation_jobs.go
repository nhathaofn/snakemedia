package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

const (
	generationJobImage   = "image"
	generationJobVideo   = "video"
	generationPreparing  = "preparing"
	generationSubmitting = "submitting"
	generationAccepted   = "accepted"
	generationComplete   = "complete"
	generationRejected   = "rejected"
)

// generationJob is a durable journal for one billable Flow mutation. The RPC
// payload and recaptcha token are deliberately omitted; ClientIDs are enough to
// find accepted work in the owning project after an interrupted response.
type generationJob struct {
	ID            string                  `json:"id"`
	Kind          string                  `json:"kind"`
	Fingerprint   string                  `json:"fingerprint"`
	Phase         string                  `json:"phase"`
	AccountID     string                  `json:"accountId"`
	ProjectID     string                  `json:"projectId,omitempty"`
	ClientIDs     []string                `json:"clientIds,omitempty"`
	CreatedAt     string                  `json:"createdAt"`
	Message       string                  `json:"message,omitempty"`
	Images        []GeneratedImageItem    `json:"images,omitempty"`
	Videos        []trackedVideo          `json:"videos,omitempty"`
	ImageResult   []GeneratedImageItem    `json:"imageResults,omitempty"`
	VideoResult   []GeneratedVideoItem    `json:"videoResults,omitempty"`
	VideoTemplate GeneratedVideoItem      `json:"videoTemplate,omitempty"`
	ImageTemplate imageGenerationTemplate `json:"imageTemplate,omitempty"`
}

type imageGenerationTemplate struct {
	Prompt      string       `json:"prompt"`
	ModelID     string       `json:"modelId"`
	ModelName   string       `json:"modelName,omitempty"`
	AspectRatio string       `json:"aspectRatio"`
	Quality     ImageQuality `json:"quality"`
	// ProjectMode tells which studio (Single or All) sent the job; journals
	// written before it existed came from Single.
	ProjectMode ImageProjectMode `json:"projectMode,omitempty"`
}

func requestFingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Provider) loadGenerationJob(id string) (generationJob, error) {
	var job generationJob
	if err := s.readState("generation-jobs", id, &job); err != nil {
		return generationJob{}, err
	}
	if job.ID != id || (job.Kind != generationJobImage && job.Kind != generationJobVideo) || job.Fingerprint == "" {
		return generationJob{}, errors.New("Thông tin job tạo media đã lưu không hợp lệ.")
	}
	return job, nil
}

func (s *Provider) saveGenerationJob(job generationJob) error {
	if job.ID == "" {
		return errors.New("Thiếu mã job tạo media.")
	}
	return s.saveState("generation-jobs", job.ID, job)
}

func prepareRequestID(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return uuid.NewString(), nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("Mã yêu cầu không hợp lệ.")
	}
	return strings.ToLower(id), nil
}

func newGenerationClientIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = strings.ToUpper(uuid.NewString())
	}
	return ids
}

func definiteMutationRejection(err error) bool {
	var marked *mutationRejection
	if errors.As(err, &marked) {
		return true
	}
	var remote *chrome.HTTPError
	if errors.As(err, &remote) {
		return remote.Status >= 400 && remote.Status < 500 && remote.Status != 408 && remote.Status != 429
	}
	var rpcErr *chrome.RPCError
	return errors.As(err, &rpcErr)
}

type mutationRejection struct{ cause error }

func (e *mutationRejection) Error() string { return e.cause.Error() }
func (e *mutationRejection) Unwrap() error { return e.cause }

func (s *Provider) checkGenerationJob(id, kind, fingerprint string) (generationJob, bool, error) {
	job, err := s.loadGenerationJob(id)
	if errors.Is(err, os.ErrNotExist) {
		return generationJob{}, false, nil
	}
	if err != nil {
		return generationJob{}, false, err
	}
	if job.Kind != kind || job.Fingerprint != fingerprint {
		return generationJob{}, true, errors.New("Mã yêu cầu đã được dùng cho nội dung khác.")
	}
	return job, true, nil
}

func pendingImageItems(job generationJob, req GenerateImageRequest) []GeneratedImageItem {
	ids := job.ClientIDs
	if len(ids) == 0 {
		ids = []string{job.ID}
	}
	items := make([]GeneratedImageItem, len(ids))
	for i, id := range ids {
		items[i] = GeneratedImageItem{
			ID: id, JobID: job.ID, Pending: true, Prompt: req.Prompt, Model: req.ModelID,
			AspectRatio: req.AspectRatio, CreatedAt: job.CreatedAt,
			Error: "Flow chưa xác nhận kết quả. Gọi lại cùng yêu cầu để kiểm tra job, không tạo job mới.",
		}
	}
	return items
}

func pendingVideoItems(job generationJob, req GenerateVideoRequest) []GeneratedVideoItem {
	ids := job.ClientIDs
	if len(ids) == 0 {
		ids = []string{job.ID}
	}
	items := make([]GeneratedVideoItem, len(ids))
	for i, id := range ids {
		template := job.VideoTemplate
		if template.Prompt == "" {
			template = GeneratedVideoItem{Prompt: req.Prompt, Model: req.ModelID, AspectRatio: req.AspectRatio, Duration: req.Duration, Resolution: req.Resolution}
		}
		items[i] = GeneratedVideoItem{
			JobID: job.ID, Pending: true, Prompt: template.Prompt, Model: template.Model,
			AspectRatio: template.AspectRatio, Duration: template.Duration, Resolution: template.Resolution,
			ModelName: template.ModelName,
			CreatedAt: job.CreatedAt,
			Error:     "Flow chưa xác nhận kết quả. Gọi lại cùng yêu cầu để kiểm tra job, không tạo job mới.",
		}
		items[i].ID = id
	}
	return items
}

// lostSubmissionMessage explains a job closed because Flow never received it.
const lostSubmissionMessage = "Flow không nhận được yêu cầu này: sau thời gian chờ vẫn không thấy kết quả trong project. Có thể gửi lại."

var errLostSubmission = errors.New(lostSubmissionMessage)

// submissionLost reports whether a job still marked submitting can be treated
// as never received. Flow lists accepted work in the project within one
// generation timeout, so finding nothing after that means the request was lost
// (for example the app was stopped mid-send) and sending it again is safe.
func (s *Provider) submissionLost(job generationJob) bool {
	if job.Phase != generationSubmitting {
		return false
	}
	created, err := time.Parse(time.RFC3339, job.CreatedAt)
	if err != nil {
		return false
	}
	limits := s.cfg.Get().Accounts
	grace := time.Duration(limits.GenerationTimeoutSeconds+limits.RequestTimeoutSeconds) * time.Second
	return time.Since(created) > grace
}

// rejectLostSubmission closes a lost job so its request ID can be replaced.
func (s *Provider) rejectLostSubmission(job generationJob) error {
	job.Phase, job.Message = generationRejected, lostSubmissionMessage
	if err := s.saveGenerationJob(job); err != nil {
		return err
	}
	return errLostSubmission
}

func newGenerationJob(id, kind, fingerprint, accountID string) generationJob {
	return generationJob{
		ID: id, Kind: kind, Fingerprint: fingerprint, Phase: generationPreparing,
		AccountID: accountID, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// pendingGenerationJobs lists unfinished jobs of kind. Along the way it skips
// unreadable journals instead of failing every recovery, deletes finished
// journals past retention, and gives up unconfirmed jobs that made no progress
// within retention so they are not listed as pending forever.
func (s *Provider) pendingGenerationJobs(kind string) ([]generationJob, error) {
	entries, err := s.stateEntries("generation-jobs")
	if err != nil {
		return nil, err
	}
	retention := s.jobRetention()
	jobs := make([]generationJob, 0, len(entries))
	for _, entry := range entries {
		job, err := s.loadGenerationJob(entry.id)
		if err != nil {
			logger.Get().Warn("skipping unreadable generation job", "job", entry.id, "error", err)
			continue
		}
		terminal := job.Phase == generationComplete || job.Phase == generationRejected
		if time.Since(entry.modified) > retention {
			s.expireGenerationJob(job, terminal)
			continue
		}
		if job.Kind == kind && !terminal {
			jobs = append(jobs, job)
		}
	}
	slices.SortFunc(jobs, func(a, b generationJob) int { return strings.Compare(b.CreatedAt, a.CreatedAt) })
	return jobs, nil
}

// expireGenerationJob deletes a finished journal or closes an abandoned one.
// A job being followed right now is left for the next listing.
func (s *Provider) expireGenerationJob(job generationJob, terminal bool) {
	lock := s.jobLock(job.ID)
	if !lock.TryLock() {
		return
	}
	defer lock.Unlock()
	var err error
	if terminal {
		err = s.removeState("generation-jobs", job.ID)
		s.jobLocks.Delete(job.ID)
	} else {
		job.Phase, job.Message = generationRejected, "Flow không xác nhận job trong thời gian lưu giữ; đã ngừng theo dõi."
		err = s.saveGenerationJob(job)
	}
	if err != nil {
		logger.Get().Warn("cannot expire generation job", "job", job.ID, "error", err)
	}
}

// ListPendingImageGenerations lists unfinished image jobs sent from mode's studio.
func (s *Provider) ListPendingImageGenerations(requestedMode ImageProjectMode) models.Response[[]GeneratedImageItem] {
	mode, err := normalizeImageProjectMode(requestedMode)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInvalidArgument)
	}
	jobs, err := s.pendingGenerationJobs(generationJobImage)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInternal)
	}
	items := make([]GeneratedImageItem, 0)
	for _, job := range jobs {
		if jobMode, _ := normalizeImageProjectMode(job.ImageTemplate.ProjectMode); jobMode != mode {
			continue
		}
		req := GenerateImageRequest{Prompt: job.ImageTemplate.Prompt, ModelID: job.ImageTemplate.ModelID, AspectRatio: job.ImageTemplate.AspectRatio, Quality: job.ImageTemplate.Quality, Count: len(job.ClientIDs)}
		items = append(items, pendingImageItems(job, req)...)
	}
	return models.SuccessResponse(items)
}

func (s *Provider) ListPendingVideoGenerations() models.Response[[]GeneratedVideoItem] {
	jobs, err := s.pendingGenerationJobs(generationJobVideo)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
	}
	items := make([]GeneratedVideoItem, 0)
	for _, job := range jobs {
		template := job.VideoTemplate
		req := GenerateVideoRequest{Prompt: template.Prompt, ModelID: template.Model, AspectRatio: template.AspectRatio, Duration: template.Duration, Resolution: template.Resolution, Count: len(job.ClientIDs)}
		items = append(items, pendingVideoItems(job, req)...)
	}
	return models.SuccessResponse(items)
}
