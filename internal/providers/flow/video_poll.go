package flow

import (
	"SnakeMedia/internal/media"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
)

// Following accepted video jobs: resume, polling, collecting and downloading.

func (s *Provider) resumeVideoGeneration(ctx context.Context, job generationJob, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
	pending := pendingVideoItems(job, req)
	if job.AccountID == "" || job.ProjectID == "" || len(job.ClientIDs) == 0 {
		return models.SuccessResponse(pending)
	}
	acc, release, err := s.readyOwnedAccount(ctx, job.AccountID)
	if err != nil {
		logger.Get().Warn("video job stays pending: account not ready", "job", job.ID, "error", err)
		return models.SuccessResponse(pending)
	}
	defer release()
	client, err := s.projectPageClient(ctx, *acc, chrome.MethodProjectContents, chrome.MethodMedia, chrome.MethodVideoStatus)
	if err != nil {
		logger.Get().Warn("video job stays pending: Flow client not ready", "job", job.ID, "error", err)
		return models.SuccessResponse(pending)
	}
	defer s.keepCredentials(*acc, client)
	template := job.VideoTemplate
	if template.Prompt == "" {
		template = GeneratedVideoItem{JobID: job.ID, Prompt: req.Prompt, Model: req.ModelID, AspectRatio: req.AspectRatio, Duration: req.Duration, Resolution: req.Resolution}
	}
	items, err := s.reconcileVideoGenerationWithClient(ctx, client, job, template)
	if errors.Is(err, errLostSubmission) {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeUnavailable)
	}
	if err != nil {
		logger.Get().Warn("video job stays pending: cannot check it", "job", job.ID, "error", err)
		return models.SuccessResponse(pending)
	}
	return models.SuccessResponse(items)
}

// ResumeVideoGeneration reconciles a saved video request without resubmitting it.
func (s *Provider) ResumeVideoGeneration(ctx context.Context, id string) models.Response[[]GeneratedVideoItem] {
	busy := []GeneratedVideoItem{{JobID: id, ID: id, Pending: true, Error: "Job đang được đối soát."}}
	return resumeBillable(s, s.videoBillable(ctx, GenerateVideoRequest{}), id, busy)
}

// videoBillable describes video generation to openBillable/resumeBillable.
// A resume rebuilds the request from the journal when req carries no prompt.
func (s *Provider) videoBillable(ctx context.Context, req GenerateVideoRequest) billableJob[GeneratedVideoItem] {
	return billableJob[GeneratedVideoItem]{
		kind:      generationJobVideo,
		wrongKind: "Mã này không phải job tạo video.",
		pending:   func(job generationJob) []GeneratedVideoItem { return pendingVideoItems(job, req) },
		result:    func(job generationJob) []GeneratedVideoItem { return job.VideoResult },
		resume: func(job generationJob) models.Response[[]GeneratedVideoItem] {
			r := req
			if r.Prompt == "" {
				t := job.VideoTemplate
				r = GenerateVideoRequest{RequestID: job.ID, Prompt: t.Prompt, ModelID: t.Model, AspectRatio: t.AspectRatio, Duration: t.Duration, Resolution: t.Resolution, Count: len(job.ClientIDs)}
			}
			return s.resumeVideoGeneration(ctx, job, r)
		},
	}
}

// adoptBatchVideos gives the client IDs no workflow names the videos carrying
// the request's batch ID, the first client ID (buildVideoRequest), in listing
// order. They all come from the same request, so the order does not matter.
func adoptBatchVideos(listing string, clientIDs []string, started []pendingVideo, known map[string]bool) []pendingVideo {
	if len(clientIDs) == 0 {
		return started
	}
	taken := make(map[string]bool, len(started))
	for _, v := range started {
		taken[v.name+"|"+v.operation] = true
	}
	batch := mediaForSeed(listing, clientIDs[0])
	for _, clientID := range clientIDs {
		if known[strings.ToLower(clientID)] {
			continue
		}
		for len(batch) > 0 && taken[batch[0].name+"|"+batch[0].operation] {
			batch = batch[1:]
		}
		if len(batch) == 0 {
			break
		}
		match := batch[0]
		batch = batch[1:]
		match.clientID = clientID
		taken[match.name+"|"+match.operation] = true
		started = append(started, match)
		known[strings.ToLower(clientID)] = true
	}
	return started
}

func (s *Provider) reconcileVideoGenerationWithClient(ctx context.Context, client *chrome.FlowClient, job generationJob, template GeneratedVideoItem) ([]GeneratedVideoItem, error) {
	started := make([]pendingVideo, 0, len(job.ClientIDs))
	known := make(map[string]bool, len(job.ClientIDs))
	for _, record := range job.Videos {
		if record.MediaID == "" && record.OperationID == "" {
			continue
		}
		started = append(started, pendingVideo{name: record.MediaID, operation: record.OperationID, clientID: record.ClientID, status: record.Status, statusObserved: record.StatusObserved})
		if record.ClientID != "" {
			known[strings.ToLower(record.ClientID)] = true
		}
	}
	listed := true
	if len(known) < len(job.ClientIDs) {
		listing, err := s.projectContentsPayload(ctx, client, job.ProjectID)
		if err != nil {
			// Without the listing nothing is known, so the job stays pending
			// instead of being called lost (and re-sent).
			listed = false
			logger.Get().Warn("cannot list project to reconcile video job", "job", job.ID, "error", err)
		} else {
			workflows := parseWorkflows(listing)
			for _, clientID := range job.ClientIDs {
				if known[strings.ToLower(clientID)] {
					continue
				}
				match := workflows.forClient(clientID)
				if match.name == "" && match.operation == "" {
					continue
				}
				match.clientID = clientID
				started = append(started, match)
				known[strings.ToLower(clientID)] = true
			}
			started = adoptBatchVideos(listing, job.ClientIDs, started, known)
		}
	}
	if len(started) == 0 && listed && s.submissionLost(job) {
		return nil, s.rejectLostSubmission(job)
	}
	if len(started) == 0 {
		job.Message = "Flow chưa tìm thấy job mang mã đã lưu. Hãy kiểm tra lại sau; yêu cầu này sẽ không được gửi lại."
		_ = s.saveGenerationJob(job)
		return pendingVideoItems(job, GenerateVideoRequest{Prompt: template.Prompt, ModelID: template.Model, AspectRatio: template.AspectRatio, Duration: template.Duration, Resolution: template.Resolution}), nil
	}
	job.Phase, job.Videos = generationAccepted, trackVideos(started)
	if err := s.saveGenerationJob(job); err != nil {
		return nil, err
	}
	return s.followGenerationVideos(ctx, client, job, started, template)
}

func (s *Provider) followGenerationVideos(ctx context.Context, client *chrome.FlowClient, job generationJob, started []pendingVideo, template GeneratedVideoItem) ([]GeneratedVideoItem, error) {
	final, err := s.waitForVideos(ctx, client, job.ProjectID, started, s.cfg.Get().Flow.VideoPollIntervalMS, s.cfg.Get().Flow.VideoGenerationTimeoutSeconds, func(videos []pendingVideo) error {
		// A round that changed nothing tracked leaves the saved journal as is.
		tracked := trackVideos(videos)
		if slices.Equal(tracked, job.Videos) {
			return nil
		}
		job.Videos = tracked
		return s.saveGenerationJob(job)
	})
	if len(final) > 0 {
		job.Videos = trackVideos(final)
	}
	if err != nil {
		job.Phase = generationAccepted
		job.Message = "Flow đã nhận yêu cầu. Job đang được theo dõi; gọi lại cùng mã request để kiểm tra, không gửi job mới."
		if saveErr := s.saveGenerationJob(job); saveErr != nil {
			return nil, saveErr
		}
		return pendingVideoItems(job, GenerateVideoRequest{Prompt: template.Prompt, ModelID: template.Model, AspectRatio: template.AspectRatio, Duration: template.Duration, Resolution: template.Resolution}), nil
	}
	results := s.collectVideoItems(ctx, client, final, template, false)
	for i := range results {
		results[i].JobID = job.ID
		results[i].Pending = false
	}
	known := make(map[string]bool, len(started))
	for _, video := range started {
		if video.clientID != "" {
			known[strings.ToLower(video.clientID)] = true
		}
	}
	if len(known) < len(job.ClientIDs) {
		allPending := pendingVideoItems(job, GenerateVideoRequest{Prompt: template.Prompt, ModelID: template.Model, AspectRatio: template.AspectRatio, Duration: template.Duration, Resolution: template.Resolution})
		for i, id := range job.ClientIDs {
			if known[strings.ToLower(id)] || i >= len(allPending) {
				continue
			}
			results = append(results, allPending[i])
		}
	}
	job.VideoResult = append([]GeneratedVideoItem(nil), results...)
	switch {
	case len(known) < len(job.ClientIDs):
		job.Phase = generationAccepted
	case videosAwaitingDownload(final, results):
		job.Phase, job.Message = generationAccepted, videoDownloadPendingMessage
	default:
		job.Phase, job.Message = generationComplete, ""
	}
	if err := s.saveGenerationJob(job); err != nil {
		return nil, err
	}
	return results, nil
}

// videoDownloadPendingMessage explains a job kept open because a finished video
// could not be downloaded yet.
const videoDownloadPendingMessage = "Flow đã xử lý, nhưng chưa tải được video. Có thể kiểm tra và tải lại."

// videosAwaitingDownload reports whether a video Flow finished has no local
// file, so the job stays open and resuming it downloads the file again instead
// of freezing the error.
func videosAwaitingDownload(final []pendingVideo, items []GeneratedVideoItem) bool {
	return slices.ContainsFunc(items, func(item GeneratedVideoItem) bool {
		return !item.Pending && item.LocalURL == "" && !slices.ContainsFunc(final, func(v pendingVideo) bool {
			return (v.name == item.ID || v.operation == item.ID) && videoFailed(v)
		})
	})
}

// collectVideoItems downloads finished videos into items built from template.
// edit selects the edit wording for failures and probes the downloaded file so
// the trimmed source duration is reported; generated videos keep their request
// duration. JobID is set for every journaled job and must not select this.
func (s *Provider) collectVideoItems(ctx context.Context, client *chrome.FlowClient, final []pendingVideo, template GeneratedVideoItem, edit bool) []GeneratedVideoItem {
	limits := s.cfg.Get().FlowPagePolicy()
	now := time.Now().UTC().Format(time.RFC3339)
	items := make([]GeneratedVideoItem, 0, len(final))
	for _, v := range final {
		id := v.name
		if id == "" {
			id = v.operation
		}
		item := template
		item.ID, item.CreatedAt = id, now
		switch {
		case v.url == "":
			item.Error = videoStatusText(v.status)
			if v.errMsg != "" {
				detail := videoFailureDetail(v.errMsg)
				if edit {
					detail = editVideoFailureDetail(v.errMsg)
				}
				item.Error += ": " + detail
			}
		default:
			local, dlErr := s.downloadVideo(ctx, client, v, s.cfg.Get().Flow.GenerationTimeoutSeconds)
			if dlErr != nil {
				item.Error = "Không tải được video: " + dlErr.Error()
				logger.Get().Warn("cannot download generated video", "media", v.name, "error", dlErr)
			} else {
				item.LocalURL = local
				if edit {
					file, pathErr := s.media.Path(local)
					probeCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
					metadata, probeErr := media.ProbeVideo(probeCtx, file, s.cfg.Get().Flow)
					cancel()
					if pathErr != nil || probeErr != nil {
						item.LocalURL = ""
						item.Error = "Không kiểm tra được tệp video đã tải."
					} else {
						item.Duration = int(math.Round(metadata.DurationSeconds))
					}
				}
			}
		}
		items = append(items, item)
	}
	return items
}

// waitForVideos polls until every started video has a video URL or failed. As in
// SolwStudio, GetMedia is the authority for the URL because the project listing
// can lag behind; the listing supplies failure states and the media IDs of
// generations known only by operation.
func (s *Provider) waitForVideos(ctx context.Context, client *chrome.FlowClient, projectID string, started []pendingVideo, intervalMS, timeoutSeconds int, checkpoint ...func([]pendingVideo) error) ([]pendingVideo, error) {
	timeout := time.Duration(s.cfg.Get().FlowPagePolicy().RequestTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	videos := append([]pendingVideo(nil), started...)
	settled := func(v pendingVideo) bool { return v.url != "" || videoFailed(v) }
	listingRPC := client.Credentials().RPCs.ID(chrome.MethodProjectContents)
	mediaRPC := client.Credentials().RPCs.ID(chrome.MethodMedia)
	statusRPC := client.Credentials().RPCs.ID(chrome.MethodVideoStatus)
	listing, err := BuildBatchEnvelope(listingRPC, []any{"projects/" + projectID, nil, nil, nil, []any{1}})
	if err != nil {
		return videos, err
	}
	call := func(rpcID, fReq string) (string, error) {
		body, err := client.Do(ctx, rpcID, fReq, timeout)
		if err != nil {
			return "", err
		}
		return chrome.ParseRPCPayload(string(body), rpcID)
	}

	delay := time.Duration(intervalMS) * time.Millisecond
	failures := 0
	notFound := make(map[string]int)
	var lastErr error
	for slices.ContainsFunc(videos, func(v pendingVideo) bool { return !settled(v) }) {
		select {
		case <-ctx.Done():
			return videos, ctx.Err()
		case <-time.After(delay):
		}

		roundFailed := false
		active := make(map[string]bool)
		if statusRPC != "" {
			ids := []any{}
			for _, v := range videos {
				if !settled(v) && v.name != "" {
					ids = append(ids, []any{v.name})
				}
			}
			if len(ids) > 0 {
				fReq, err := BuildBatchEnvelope(statusRPC, []any{nil, nil, ids})
				if err != nil {
					return videos, err
				}
				payload, err := call(statusRPC, fReq)
				if err != nil {
					roundFailed, lastErr = true, err
				} else {
					records, parseErr := parseVideoMedia(payload, 2)
					if parseErr != nil {
						roundFailed, lastErr = true, parseErr
					} else {
						for i, v := range videos {
							for _, record := range records {
								if record.name == v.name {
									videos[i].status, videos[i].errMsg, videos[i].statusObserved = record.status, record.errMsg, record.statusObserved
									videos[i].url = record.url
									active[v.name] = record.statusObserved && slices.Contains([]int{1, 2, 6}, record.status)
								}
							}
						}
					}
				}
			}
		}
		listed := make(map[string]bool)
		listingOK := false
		needsListing := statusRPC == "" || slices.ContainsFunc(videos, func(v pendingVideo) bool {
			return !settled(v) && (v.name == "" || (v.clientID != "" && strings.EqualFold(v.name, v.clientID)))
		})
		if needsListing {
			if payload, err := call(listingRPC, listing); err == nil {
				listingOK = true
				// GetProjectContents: field 3 (index 2) lists the project's media.
				media, _ := parseVideoMedia(payload, 2)
				workflows := parseWorkflows(payload)
				for i, v := range videos {
					if settled(v) {
						continue
					}
					if v.clientID != "" && (v.name == "" || strings.EqualFold(v.name, v.clientID)) {
						if found := workflows.forClient(v.clientID); found.name != "" {
							videos[i].name = found.name
							videos[i].operation = found.operation
						}
					}
					if v.name == "" {
						videos[i].name = mediaForOperation(payload, v.operation)
					}
					for _, m := range media {
						if m.name == videos[i].name || (v.name == "" && v.operation != "" && m.operation == v.operation) {
							videos[i].name = m.name
							listed[m.name] = true
							if !active[videos[i].name] && !settled(videos[i]) {
								videos[i].status, videos[i].errMsg, videos[i].statusObserved = m.status, m.errMsg, m.statusObserved
							}
						}
					}
				}
			} else {
				roundFailed, lastErr = true, err
			}
		}
		for i, v := range videos {
			if settled(v) || v.name == "" || active[v.name] {
				continue
			}
			fReq, err := BuildBatchEnvelope(mediaRPC, []any{v.name})
			if err != nil {
				return videos, err
			}
			payload, err := call(mediaRPC, fReq)
			if err != nil {
				// A media still rendering may not be served yet ("Media not found");
				// only transport failures count against the poll.
				var rpcError *chrome.RPCError
				if errors.As(err, &rpcError) && rpcError.Code == 5 && listingOK && !listed[v.name] {
					notFound[v.name]++
					if notFound[v.name] >= s.cfg.Get().Flow.MaxPollFailures {
						return videos, errors.New("Flow chưa tìm thấy media hoặc workflow của job đã nhận")
					}
				} else {
					notFound[v.name] = 0
				}
				var remote *chrome.HTTPError
				if errors.As(err, &remote) || ctx.Err() != nil {
					roundFailed, lastErr = true, err
				}
				continue
			}
			notFound[v.name] = 0
			var decoded any
			if json.Unmarshal([]byte(payload), &decoded) == nil {
				records, _ := parseVideoMedia("[["+payload+"]]", 0)
				for _, record := range records {
					if record.name == v.name && videoFailed(record) {
						videos[i].status = record.status
						videos[i].statusObserved = record.statusObserved
						videos[i].errMsg = record.errMsg
					}
				}
				if u := videoOutputURL(asList(decoded)); u != "" && !videoFailed(videos[i]) {
					videos[i].url, videos[i].status = u, videoStatusSuccess
				}
			}
		}
		for _, save := range checkpoint {
			if save != nil {
				if err := save(videos); err != nil {
					return videos, err
				}
			}
		}
		if roundFailed {
			failures++
			delay = min(delay*2, time.Duration(s.cfg.Get().Flow.PollMaxIntervalMS)*time.Millisecond)
			var remote *chrome.HTTPError
			if errors.As(lastErr, &remote) {
				delay = max(delay, remote.RetryAfter)
			}
			if failures >= s.cfg.Get().Flow.MaxPollFailures {
				return videos, fmt.Errorf("lỗi khi theo dõi tiến độ video: %w", lastErr)
			}
		} else {
			failures = 0
			delay = time.Duration(intervalMS) * time.Millisecond
		}
	}
	return videos, nil
}

func (s *Provider) downloadVideo(ctx context.Context, client *chrome.FlowClient, v pendingVideo, timeoutSeconds int) (string, error) {
	var local string
	err := client.DownloadMedia(ctx, v.url, []string{"video/", "application/octet-stream"}, time.Duration(timeoutSeconds)*time.Second, func(reader io.Reader, contentType string) error {
		var err error
		local, err = s.media.PutReader(v.name, contentType, ".mp4", reader, s.cfg.Get().FlowPagePolicy().MaxDownloadBytes)
		return err
	})
	return local, err
}

func videoFailed(v pendingVideo) bool {
	return slices.Contains(videoFailedStatuses, v.status) || (v.statusObserved && v.status == 0)
}

// GetMedia materializes the current video's serving URL in video field 1/9.
// Other Flow variants use field 5/3. Both belong to this media, not its inputs.
func videoOutputURL(record []any) string {
	if len(record) <= 7 {
		return ""
	}
	video := asList(record[7])
	if len(video) > 0 {
		materialized := asList(video[0])
		if len(materialized) > 8 {
			value, _ := materialized[8].(string)
			if strings.HasPrefix(value, "https://") && strings.Contains(value, videoURLMarker) {
				return value
			}
		}
	}
	if len(video) <= 4 {
		return ""
	}
	generated := asList(video[4])
	if len(generated) <= 2 {
		return ""
	}
	value, _ := generated[2].(string)
	if !strings.HasPrefix(value, "https://") {
		return ""
	}
	return value
}
