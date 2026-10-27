package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
	"github.com/google/uuid"
)

type trackedVideo struct {
	MediaID        string `json:"mediaId"`
	OperationID    string `json:"operationId,omitempty"`
	Status         int    `json:"status"`
	StatusObserved bool   `json:"statusObserved,omitempty"`
	ClientID       string `json:"clientId,omitempty"`
}

type editJob struct {
	ID        string               `json:"id"`
	AccountID string               `json:"accountId"`
	ProjectID string               `json:"projectId"`
	Request   EditVideoRequest     `json:"request"`
	Template  GeneratedVideoItem   `json:"template"`
	Videos    []trackedVideo       `json:"videos"`
	Results   []GeneratedVideoItem `json:"results"`
	Complete  bool                 `json:"complete"`
	Message   string               `json:"message,omitempty"`
}

// saveEditJob writes an edit journal; the listing cache reads it again next time.
func (s *Provider) saveEditJob(job editJob) error {
	s.journals.forget("jobs", job.ID)
	return s.saveState("jobs", job.ID, job)
}

// checkEditJob rejects a decoded edit journal that does not belong to its file.
func checkEditJob(id string, job editJob) error {
	if job.ID != id {
		return errors.New("Thông tin job chỉnh sửa đã lưu không hợp lệ.")
	}
	return nil
}

// tryJobLock claims a request ID for one call: a second call with the same ID
// is refused until the first releases it, so a job is never followed or sent
// twice at once. Released IDs leave nothing behind.
func (s *Provider) tryJobLock(id string) (release func(), ok bool) {
	s.jobLocksMu.Lock()
	defer s.jobLocksMu.Unlock()
	if _, held := s.jobLocks[id]; held {
		return nil, false
	}
	if s.jobLocks == nil {
		s.jobLocks = make(map[string]struct{})
	}
	s.jobLocks[id] = struct{}{}
	return func() {
		s.jobLocksMu.Lock()
		delete(s.jobLocks, id)
		s.jobLocksMu.Unlock()
	}, true
}

func (j editJob) items() []GeneratedVideoItem {
	if j.Complete {
		items := append([]GeneratedVideoItem(nil), j.Results...)
		for i := range items {
			if strings.Contains(items[i].Error, publicErrorDangerFilter) {
				items[i].Error = videoStatusText(4) + ": " + editVideoFailureDetail(items[i].Error)
			}
		}
		return items
	}
	item := j.Template
	item.ID, item.JobID, item.Pending = j.ID, j.ID, true
	item.Error = j.Message
	if item.Error == "" {
		item.Error = "Flow chưa trả kết quả. Có thể kiểm tra tiếp mà không gửi lại lượt chỉnh sửa."
	}
	return []GeneratedVideoItem{item}
}

// runEditVideoJob journals the client media seed BEFORE sending the billable
// mutation. Neither the payload nor its short-lived token is persisted.
func (s *Provider) runEditVideoJob(ctx context.Context, client *chrome.FlowClient, acc session.StoredAccount, req EditVideoRequest, projectID, rpcID string, inner []byte, template GeneratedVideoItem) ([]GeneratedVideoItem, error) {
	var payload []any
	if err := json.Unmarshal(inner, &payload); err != nil {
		return nil, err
	}
	// The payload is built locally, but guard its shape so a builder change
	// fails as an error instead of a panic.
	var item, metadata []any
	if len(payload) > 0 {
		if items := asList(payload[0]); len(items) > 0 {
			item = asList(items[0])
		}
	}
	if len(item) > 4 {
		metadata = asList(item[4])
	}
	if len(metadata) < 5 {
		return nil, errors.New("payload chỉnh sửa video không đúng cấu trúc")
	}
	metadata[4] = strings.ToUpper(req.RequestID)
	// Flow's video editor writes a new version into the uploaded workflow.
	// Field 2 selects that workflow; field 6 is only for a NEW workflow seed.
	if req.source != nil && req.source.WorkflowID != "" {
		metadata[1] = req.source.WorkflowID
		item[4] = metadata[:5]
	}
	inner, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	template.JobID = req.RequestID
	template.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return nil, err
	}
	// The browser is ready before the journal says "sent": a request that
	// never left must not be polled as if Flow had it.
	browser, err := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
	if err != nil || browser == nil {
		if err == nil {
			err = chrome.ErrPageNotReady
		}
		return nil, err
	}
	job := editJob{ID: req.RequestID, AccountID: acc.ID, ProjectID: projectID, Request: req, Template: template,
		Videos: []trackedVideo{{MediaID: req.RequestID, ClientID: req.RequestID}}, Message: "Đã bắt đầu gửi yêu cầu. Đang chờ Flow xác nhận."}
	if err := s.saveEditJob(job); err != nil {
		return nil, err
	}
	page := s.pageLock(browser.Port)
	page.RLock()
	body, err := client.DoFromBrowser(ctx, browser.Port, rpcID, fReq, time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds)*time.Second)
	page.RUnlock()
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		err = &mutationRejection{cause: errors.New("Google Flow yêu cầu xác thực reCAPTCHA Enterprise token trước khi chỉnh sửa video.")}
	}
	if err == nil {
		var reply string
		reply, err = chrome.ParseRPCPayload(string(body), rpcID)
		if err == nil {
			started, parseErr := parseVideoMedia(reply, 3)
			if parseErr != nil {
				err = parseErr
			} else {
				if len(started) == 0 {
					started = parseVideoOperations(reply)
				}
				if len(started) > 0 {
					if req.onAccepted != nil {
						req.onAccepted()
					}
					for i := range started {
						started[i].clientID = req.RequestID
					}
					job.Videos = trackVideos(started)
					job.Message = ""
				}
			}
		}
	}
	if err != nil {
		// An uncertain response must remain recoverable, never trigger resubmission.
		job.Message = "Chưa xác nhận được kết quả gửi: " + err.Error() + " Có thể kiểm tra lại job này."
		if definiteMutationRejection(err) {
			job.Complete = true
			failed := template
			failed.ID = job.ID
			failed.Error = err.Error()
			job.Results = []GeneratedVideoItem{failed}
		}
		if saveErr := s.saveEditJob(job); saveErr != nil {
			return nil, saveErr
		}
		return job.items(), nil
	}
	if err := s.saveEditJob(job); err != nil {
		return nil, err
	}
	return s.followEditJob(ctx, client, &job)
}

func trackVideos(videos []pendingVideo) []trackedVideo {
	out := make([]trackedVideo, 0, len(videos))
	for _, v := range videos {
		out = append(out, trackedVideo{MediaID: v.name, OperationID: v.operation, Status: v.status, StatusObserved: v.statusObserved, ClientID: v.clientID})
	}
	return out
}

func (s *Provider) followEditJob(ctx context.Context, client *chrome.FlowClient, job *editJob) ([]GeneratedVideoItem, error) {
	var pending []pendingVideo
	for _, v := range job.Videos {
		pending = append(pending, pendingVideo{name: v.MediaID, operation: v.OperationID, status: v.Status, statusObserved: v.StatusObserved, clientID: v.ClientID})
	}
	final, err := s.waitForVideos(ctx, client, job.ProjectID, pending, s.cfg.Get().Flow.VideoPollIntervalMS, s.cfg.Get().Flow.VideoGenerationTimeoutSeconds, func(videos []pendingVideo) error {
		current := trackVideos(videos)
		if slices.Equal(current, job.Videos) {
			return nil
		}
		job.Videos = current
		return s.saveEditJob(*job)
	})
	if len(final) > 0 {
		job.Videos = trackVideos(final)
	}
	if err != nil {
		job.Message = "Chưa hoàn tất theo dõi: " + strings.TrimSuffix(err.Error(), ".") + ". Kiểm tra tiếp để lấy kết quả, không gửi lại yêu cầu."
		if errors.Is(err, context.DeadlineExceeded) {
			job.Message = "Flow chưa hoàn tất trong thời gian chờ. Có thể kiểm tra tiếp mà không gửi lại lượt chỉnh sửa."
		}
	} else {
		job.Results = s.collectVideoItems(ctx, client, final, job.Template, true)
		job.Complete = !videosAwaitingDownload(final, job.Results)
		if !job.Complete {
			job.Message = videoDownloadPendingMessage
		}
	}
	if err := s.saveEditJob(*job); err != nil {
		return nil, err
	}
	return job.items(), nil
}

func (s *Provider) ResumeVideoEdit(ctx context.Context, id string) models.Response[[]GeneratedVideoItem] {
	if _, err := uuid.Parse(id); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	unlock, ok := s.tryJobLock(id)
	if !ok {
		return models.ErrorResponse[[]GeneratedVideoItem]("Job đang được theo dõi.", models.CodeUnavailable)
	}
	defer unlock()
	var job editJob
	if err := s.readState("jobs", id, &job); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeNotFound)
	}
	if job.ID != id || job.AccountID == "" || job.ProjectID == "" || len(job.Videos) == 0 {
		return models.ErrorResponse[[]GeneratedVideoItem]("Thông tin job không hợp lệ.", models.CodeInvalidArgument)
	}
	if job.Complete {
		return models.SuccessResponse(job.items())
	}
	acc, release, err := s.readyOwnedAccount(ctx, job.AccountID)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeUnavailable)
	}
	defer release()
	client, err := s.editClient(ctx, *acc, job.ProjectID)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeUnavailable)
	}
	defer s.keepCredentials(*acc, client)
	items, err := s.followEditJob(ctx, client, &job)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(items)
}

// ListVideoEditJobs lists saved edit jobs. Broken journals are moved aside,
// finished ones past retention are deleted, and unfinished ones idle past
// retention are closed with an explanation instead of staying pending.
func (s *Provider) ListVideoEditJobs(context.Context) models.Response[[]GeneratedVideoItem] {
	entries, err := listJournals(s, "jobs", checkEditJob)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
	}
	retention := s.jobRetention()
	items := []GeneratedVideoItem{}
	for _, entry := range entries {
		job := entry.value
		if time.Since(entry.modified) > retention {
			if job.Complete {
				s.expireEditJob(job)
				continue
			}
			job = s.abandonEditJob(job)
		}
		items = append(items, job.items()...)
	}
	slices.SortFunc(items, func(a, b GeneratedVideoItem) int { return strings.Compare(b.CreatedAt, a.CreatedAt) })
	return models.SuccessResponse(items)
}

func (s *Provider) expireEditJob(job editJob) {
	unlock, ok := s.tryJobLock(job.ID)
	if !ok {
		return
	}
	defer unlock()
	if err := s.removeState("jobs", job.ID); err != nil {
		logger.Get().Warn("cannot delete expired edit job", "job", job.ID, "error", err)
	}
	s.journals.forget("jobs", job.ID)
}

// abandonEditJob closes an edit Flow never finished within retention.
func (s *Provider) abandonEditJob(job editJob) editJob {
	unlock, ok := s.tryJobLock(job.ID)
	if !ok {
		return job
	}
	defer unlock()
	item := job.Template
	item.ID, item.JobID = job.ID, job.ID
	item.Error = "Flow không trả kết quả trong thời gian lưu giữ; đã ngừng theo dõi."
	job.Complete, job.Results, job.Message = true, []GeneratedVideoItem{item}, item.Error
	if err := s.saveEditJob(job); err != nil {
		logger.Get().Warn("cannot close abandoned edit job", "job", job.ID, "error", err)
	}
	return job
}
