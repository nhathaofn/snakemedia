package flow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

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

func (s *Provider) jobLock(id string) *sync.Mutex {
	v, _ := s.jobLocks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
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
	item := asList(asList(payload[0])[0])
	metadata := asList(item[4])
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
	job := editJob{ID: req.RequestID, AccountID: acc.ID, ProjectID: projectID, Request: req, Template: template,
		Videos: []trackedVideo{{MediaID: req.RequestID, ClientID: req.RequestID}}, Message: "Đã bắt đầu gửi yêu cầu. Đang chờ Flow xác nhận."}
	if err := s.saveState("jobs", job.ID, job); err != nil {
		return nil, err
	}
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return nil, err
	}
	browser, err := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
	if err != nil || browser == nil {
		if err == nil {
			err = chrome.ErrPageNotReady
		}
		job.Message = err.Error()
		if saveErr := s.saveState("jobs", job.ID, job); saveErr != nil {
			return nil, saveErr
		}
		return job.items(), nil
	}
	body, err := client.DoFromBrowser(ctx, browser.Port, rpcID, fReq, time.Duration(s.cfg.Get().Accounts.GenerationTimeoutSeconds)*time.Second)
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
		var remote *chrome.HTTPError
		var rejection *chrome.RPCError
		if (errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 && remote.Status != 408 && remote.Status != 429) || errors.As(err, &rejection) {
			job.Complete = true
			failed := template
			failed.ID = job.ID
			failed.Error = err.Error()
			job.Results = []GeneratedVideoItem{failed}
		}
		if saveErr := s.saveState("jobs", job.ID, job); saveErr != nil {
			return nil, saveErr
		}
		return job.items(), nil
	}
	if err := s.saveState("jobs", job.ID, job); err != nil {
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
	policy := s.cfg.Get().Accounts
	final, err := s.waitForVideos(ctx, client, job.ProjectID, pending, policy.VideoPollIntervalMS, policy.VideoGenerationTimeoutSeconds, func(videos []pendingVideo) error {
		current := trackVideos(videos)
		if slices.Equal(current, job.Videos) {
			return nil
		}
		job.Videos = current
		return s.saveState("jobs", job.ID, job)
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
		job.Results = s.collectVideoItems(ctx, client, final, job.Template)
		job.Complete = !slices.ContainsFunc(job.Results, func(item GeneratedVideoItem) bool {
			return item.LocalURL == "" && !slices.ContainsFunc(final, func(v pendingVideo) bool { return v.name == item.ID && videoFailed(v) })
		})
		if !job.Complete {
			job.Message = "Flow đã xử lý, nhưng chưa tải được video. Có thể kiểm tra và tải lại."
		}
	}
	if err := s.saveState("jobs", job.ID, job); err != nil {
		return nil, err
	}
	return job.items(), nil
}

func (s *Provider) ResumeVideoEdit(ctx context.Context, id string) models.Response[[]GeneratedVideoItem] {
	if _, err := uuid.Parse(id); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	lock := s.jobLock(id)
	if !lock.TryLock() {
		return models.ErrorResponse[[]GeneratedVideoItem]("Job đang được theo dõi.", models.CodeUnavailable)
	}
	defer lock.Unlock()
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

func (s *Provider) ListVideoEditJobs(context.Context) models.Response[[]GeneratedVideoItem] {
	dir := filepath.Join(filepath.Dir(s.cfg.Path()), "flow", "jobs")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return models.SuccessResponse([]GeneratedVideoItem{})
	}
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
	}
	items := []GeneratedVideoItem{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		var job editJob
		if err := s.readState("jobs", id, &job); err != nil {
			return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
		}
		if job.ID != id {
			return models.ErrorResponse[[]GeneratedVideoItem]("Mã job đã lưu không khớp.", models.CodeInternal)
		}
		items = append(items, job.items()...)
	}
	slices.SortFunc(items, func(a, b GeneratedVideoItem) int { return strings.Compare(b.CreatedAt, a.CreatedAt) })
	return models.SuccessResponse(items)
}
