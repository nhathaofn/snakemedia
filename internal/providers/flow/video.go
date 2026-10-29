package flow

import (
	"SnakeMedia/internal/providers/flow/session"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

// Video generation protocol: model selection follows the Flow web bundle
// (chunk_studio_1.js: HTrJv usage parser iab and variant picker iO); the request
// shape and polling follow SolwStudio's live-captured codec
// (references/SolwStudio/plugins/flow/flow_rpc.py). Raw submit replies are logged
// at debug level so the layout can be checked against real traffic.
const (
	// videoURLMarker identifies the finished video file among a media's URLs.
	videoURLMarker          = "flow-content.google/video/"
	publicErrorDangerFilter = "PUBLIC_ERROR_DANGER_FILTER"
)

// Video usage fields inside a GetModels video family (0-based JSON indices).
const (
	modelsVideoFamiliesIndex = 4
	videoTierCostsIndex      = 4  // [[tier, [[null, credits]] | [null, []]], ...]
	videoCapabilitiesIndex   = 7  // [[[[capability, ...]], ...]]
	videoAspectsIndex        = 12 // [[aspect, ...]]: 1 portrait, 2 landscape
	videoDurationIndex       = 16 // seconds; Flow assumes 8 when absent
	videoAnyDurationIndex    = 22 // true when the usage accepts any duration
	videoResolutionsIndex    = 23 // [[resolution, ...]]: 1 = 720p, 4 = 360p
	videoHiddenIndex         = 24
	// videoInputLimitsIndex holds input limits (bundle rab: field 22); slot 3 is
	// the longest source video in seconds, 10 when absent.
	videoInputLimitsIndex    = 21
	videoMaxInputSecondsSlot = 3
	videoDefaultInputSeconds = 10
	videoDefaultDuration     = 8
	videoResolution720p      = 1 // Flow assumes 720p when a usage lists none
	// tierDefaultVideoIndex is the video family preselected in a tier default.
	tierDefaultVideoIndex = 3
)

// FlowVideoAspect maps user-facing ratios to Flow's video aspect enum. Which
// ratios a model offers comes from the catalogue; this is only the wire mapping.
var FlowVideoAspect = map[string]int{"9:16": 1, "16:9": 2}

// FlowVideoResolution maps user-facing qualities to Flow's resolution enum (_.eO).
// Which qualities a model offers comes from the catalogue; this is only the
// wire mapping (1080p and 4K are upsamplers, never listed by generation usages).
var FlowVideoResolution = map[string]int{"720p": 1, "1080p": 2, "4K": 3, "360p": 4}

// videoResolutionOrder lists qualities best first for display.
var videoResolutionOrder = []string{"4K", "1080p", "720p", "360p"}

// Media status codes of a generation (Flow's _.wO mapping).
const videoStatusSuccess = 3

// videoFailedStatuses end a generation without a video: failed, canceled, policy.
var videoFailedStatuses = []int{4, 5, 7}

// GenerateVideoRequest is a text-to-video request.
type GenerateVideoRequest struct {
	RequestID   string `json:"requestId,omitempty"`
	Prompt      string `json:"prompt"`
	ModelID     string `json:"modelId"`
	AspectRatio string `json:"aspectRatio"`
	Duration    int    `json:"duration"`
	Resolution  string `json:"resolution"`
	Count       int    `json:"count"`
	ProjectID   string `json:"projectId,omitempty"`
	// References are images the video must follow (reference-to-video).
	References  []ReferenceImage `json:"references,omitempty"`
	fingerprint string
	onAccepted  func()
}

// GeneratedVideoItem is one requested output video.
type GeneratedVideoItem struct {
	JobID       string `json:"jobId,omitempty"`
	Pending     bool   `json:"pending,omitempty"`
	ID          string `json:"id"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model"`
	AspectRatio string `json:"aspectRatio"`
	Duration    int    `json:"duration"`
	Resolution  string `json:"resolution"`
	CreatedAt   string `json:"createdAt"`
	// ModelName is the model family's display name.
	ModelName string `json:"modelName"`
	// LocalURL serves the downloaded file through the app's asset server.
	LocalURL string `json:"localUrl,omitempty"`
	// Error explains why this output failed or could not be downloaded.
	Error string `json:"error,omitempty"`
}

func validateVideoRequest(req GenerateVideoRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return errors.New("Vui lòng nhập nội dung mô tả (prompt).")
	}
	if strings.TrimSpace(req.ModelID) == "" {
		return errors.New("Chưa chọn model video.")
	}
	// Ratio, duration and quality are checked against the account's catalogue
	// when the variant is chosen; here only that they name a wire value.
	if _, ok := FlowVideoAspect[req.AspectRatio]; !ok {
		return fmt.Errorf("Chưa chọn tỉ lệ video Flow liệt kê (đang chọn %q).", req.AspectRatio)
	}
	if req.Duration <= 0 {
		return errors.New("Chưa chọn thời lượng video.")
	}
	if _, ok := FlowVideoResolution[req.Resolution]; !ok {
		return fmt.Errorf("Chưa chọn chất lượng video Flow liệt kê (đang chọn %q).", req.Resolution)
	}
	if limit := maxVideosPerRequest(); req.Count < 1 || req.Count > limit {
		return fmt.Errorf("Số lượng video phải từ 1 đến %d.", limit)
	}
	if req.ProjectID != "" {
		if _, err := uuid.Parse(req.ProjectID); err != nil {
			return fmt.Errorf("Project ID %q không hợp lệ.", req.ProjectID)
		}
	}
	return validateReferences(req.References)
}

type videoGenerationChoice struct {
	rotation videoAccountCandidate
	family   videoFamily
	usage    videoUsage
}

func (s *Provider) videoGenerationChoices(ctx context.Context, req GenerateVideoRequest) ([]videoGenerationChoice, models.Code, error) {
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, models.CodeUnavailable, err
	}
	if len(accounts) == 0 {
		return nil, models.CodeUnavailable, errNoFlowAccount
	}
	choices, firstCatalogErr, firstSelectionErr := accountChoices(ctx, accounts, func(ctx context.Context, acc session.StoredAccount) accountChoice[videoGenerationChoice] {
		families, _, err := s.videoFamiliesFor(ctx, acc)
		if err != nil {
			return accountChoice[videoGenerationChoice]{catalogErr: err}
		}
		idx := slices.IndexFunc(families, func(f videoFamily) bool { return f.id == req.ModelID })
		if idx < 0 {
			return accountChoice[videoGenerationChoice]{selectErr: fmt.Errorf("Model %q không có trong danh sách model của tài khoản %s.", req.ModelID, acc.Email)}
		}
		usage, err := selectVideoVariant(families[idx], accountTier(acc), FlowVideoAspect[req.AspectRatio], req.Duration, FlowVideoResolution[req.Resolution], len(req.References) > 0)
		if err != nil {
			return accountChoice[videoGenerationChoice]{selectErr: err}
		}
		if len(req.References) > usage.maxRefs && len(req.References) > 0 {
			return accountChoice[videoGenerationChoice]{selectErr: fmt.Errorf("Model %s nhận tối đa %d ảnh tham chiếu (đang chọn %d).", displayName(families[idx].name), usage.maxRefs, len(req.References))}
		}
		cost, known := usage.creditCost(accountTier(acc))
		if known && req.Count > 0 {
			if cost > math.MaxInt/req.Count {
				return accountChoice[videoGenerationChoice]{selectErr: errors.New("chi phí video vượt quá giới hạn số nguyên")}
			}
			cost *= req.Count
		}
		return accountChoice[videoGenerationChoice]{ok: true, choice: videoGenerationChoice{
			rotation: videoAccountCandidate{account: acc, cost: cost},
			family:   families[idx],
			usage:    usage,
		}}
	})
	if len(choices) > 0 {
		return choices, models.CodeOK, nil
	}
	if firstCatalogErr != nil {
		return nil, models.CodeUnavailable, fmt.Errorf("không lấy được danh sách model video từ tài khoản nào: %w", firstCatalogErr)
	}
	if firstSelectionErr != nil {
		return nil, models.CodeInvalidArgument, firstSelectionErr
	}
	return nil, models.CodeUnavailable, errors.New("không có tài khoản nào hỗ trợ yêu cầu video này")
}

// GenerateVideos chooses a compatible account using Flow's current per-model
// cost and a smooth credit-weighted rotation, then waits for Flow's job result.
func (s *Provider) GenerateVideos(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
	if err := validateVideoRequest(req); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	requestID, err := prepareRequestID(req.RequestID)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	req.RequestID = requestID
	fingerprintReq := req
	fingerprintReq.RequestID = ""
	fingerprintReq.fingerprint = ""
	fingerprintReq.onAccepted = nil
	fingerprint, err := requestFingerprint(fingerprintReq)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
	}
	req.fingerprint = fingerprint
	unlock, settled, proceed := openBillable(s, s.videoBillable(ctx, req), req.RequestID, fingerprint)
	if !proceed {
		return settled
	}
	defer unlock()
	choices, code, err := s.videoGenerationChoices(ctx, req)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không thể tạo video: "+err.Error(), code)
	}
	reserve := func(ctx context.Context, remaining []videoGenerationChoice) (int, *accountLease, error) {
		candidates := make([]videoAccountCandidate, len(remaining))
		for i, choice := range remaining {
			candidates[i] = choice.rotation
		}
		return s.acquireVideoCandidate(ctx, candidates)
	}
	chosen, account, lease, release, err := readyCandidate(ctx, s, choices, func(c videoGenerationChoice) session.StoredAccount { return c.rotation.account }, reserve)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không thể tạo video: "+err.Error(), models.CodeUnavailable)
	}
	defer release()

	req.onAccepted = lease.commitVideoSpend
	videos, err := s.generateLiveVideos(ctx, account, req, chosen.usage.key)
	if s.refreshLiveCredits != nil {
		s.refreshLiveCredits(ctx, account.ID)
	}
	if err != nil {
		job, readErr := s.loadGenerationJob(req.RequestID)
		if readErr == nil && (job.Phase == generationSubmitting || job.Phase == generationAccepted) {
			if definiteMutationRejection(err) {
				job.Phase, job.Message = generationRejected, err.Error()
				if saveErr := s.saveGenerationJob(job); saveErr != nil {
					return models.ErrorResponse[[]GeneratedVideoItem](saveErr.Error(), models.CodeInternal)
				}
				return models.ErrorResponse[[]GeneratedVideoItem](fmt.Sprintf("Lỗi khi tạo video từ Google Flow (%s): %s", account.Email, err.Error()), models.CodeUnavailable)
			}
			lease.commitVideoSpend()
			pending := pendingVideoItems(job, req)
			for i := range pending {
				pending[i].ModelName = displayName(chosen.family.name)
			}
			job.Phase, job.VideoResult = generationAccepted, append([]GeneratedVideoItem(nil), pending...)
			_ = s.saveGenerationJob(job)
			return models.SuccessResponse(pending)
		}
		s.forgetProject(account.ID)
		return models.ErrorResponse[[]GeneratedVideoItem](
			fmt.Sprintf("Lỗi khi tạo video từ Google Flow (%s): %s", account.Email, err.Error()),
			models.CodeUnavailable,
		)
	}
	lease.commitVideoSpend()
	for i := range videos {
		videos[i].ModelName = displayName(chosen.family.name)
		videos[i].JobID = req.RequestID
	}
	if job, readErr := s.loadGenerationJob(req.RequestID); readErr == nil {
		job.VideoTemplate.ModelName = displayName(chosen.family.name)
		job.VideoResult = append([]GeneratedVideoItem(nil), videos...)
		// followGenerationVideos keeps the job accepted while a finished video
		// still has to be downloaded; that must not be overwritten here.
		if job.Phase == generationAccepted || slices.ContainsFunc(videos, func(item GeneratedVideoItem) bool { return item.Pending }) {
			job.Phase = generationAccepted
		} else {
			job.Phase, job.Message = generationComplete, ""
		}
		if err := s.saveGenerationJob(job); err != nil {
			return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
		}
	}
	return models.SuccessResponse(videos)
}

// generateVideosFromAccount runs one text-to-video request and follows it to completion.
func (s *Provider) generateVideosFromAccount(ctx context.Context, acc session.StoredAccount, req GenerateVideoRequest, variant string) ([]GeneratedVideoItem, error) {
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}
	generateMethod := chrome.MethodGenerateVideoText
	if len(req.References) > 0 {
		generateMethod = chrome.MethodGenerateVideoReferences
	}
	methods := []string{chrome.MethodProjects, chrome.MethodCreateProject, chrome.MethodProjectContents, chrome.MethodMedia, chrome.MethodVideoStatus, generateMethod}
	if len(req.References) > 0 {
		methods = append(methods, chrome.MethodUploadImage)
	}
	// Video descriptors are only in the project page's lazy bundle.
	client, err := s.projectPageClient(ctx, acc, methods...)
	if err != nil {
		return nil, err
	}
	defer s.keepCredentials(acc, client)

	projectID, err := s.resolveProject(ctx, client, acc, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// 1. Upload references, then mint the generation token last so it is fresh.
	browser, err := s.generationBrowser(ctx, acc)
	if err != nil {
		return nil, err
	}
	policy := s.cfg.Get().FlowPagePolicy()
	refMediaIDs, err := s.uploadReferences(ctx, client, acc, browser, policy, req.References, projectID)
	if err != nil {
		return nil, err
	}
	recaptchaToken, err := s.mintGenerationToken(ctx, browser, policy, chrome.ActionVideoGeneration)
	if err != nil {
		return nil, err
	}

	// 3. Dispatch text-to-video or reference-to-video using the active Flow build.
	clientIDs := newGenerationClientIDs(req.Count)
	rpcID := client.Credentials().RPCs.ID(chrome.MethodGenerateVideoText)
	inner, err := BuildVideoTextRequestWithClientIDs(req.Prompt, projectID, variant, FlowVideoAspect[req.AspectRatio], FlowVideoResolution[req.Resolution], req.Count, recaptchaToken, clientIDs)
	if len(refMediaIDs) > 0 {
		rpcID = client.Credentials().RPCs.ID(chrome.MethodGenerateVideoReferences)
		inner, err = BuildVideoReferenceRequestWithClientIDs(req.Prompt, projectID, variant, FlowVideoAspect[req.AspectRatio], FlowVideoResolution[req.Resolution], req.Count, refMediaIDs, recaptchaToken, clientIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload tạo video: %w", err)
	}
	template := GeneratedVideoItem{JobID: req.RequestID, Prompt: req.Prompt, Model: variant, AspectRatio: req.AspectRatio, Duration: req.Duration, Resolution: req.Resolution}
	return s.runVideoJob(ctx, client, acc.ID, req.RequestID, req.fingerprint, projectID, rpcID, inner, clientIDs, template, req.onAccepted)
}

// runVideoJob submits a built video request (text, reference or edit), follows
// its videos until Flow finishes them and downloads the results. template
// carries the request's settings into every returned item.
func (s *Provider) runVideoJob(ctx context.Context, client *chrome.FlowClient, accountID, requestID, fingerprint, projectID, rpcID string, inner []byte, clientIDs []string, template GeneratedVideoItem, accepted ...func()) ([]GeneratedVideoItem, error) {
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return nil, err
	}
	job := newGenerationJob(requestID, generationJobVideo, fingerprint, accountID)
	job.Phase, job.ProjectID, job.ClientIDs, job.VideoTemplate = generationSubmitting, projectID, append([]string(nil), clientIDs...), template
	if err := s.saveGenerationJob(job); err != nil {
		return nil, fmt.Errorf("không lưu được mã request trước khi gửi Flow: %w", err)
	}
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		job.Phase, job.Message = generationRejected, "Flow yêu cầu xác thực lại reCAPTCHA trước khi tạo video."
		_ = s.saveGenerationJob(job)
		return nil, errors.New("Google Flow yêu cầu xác thực reCAPTCHA Enterprise token (action: VIDEO_GENERATION). Phiên cần được cấp token qua trình duyệt trước khi tạo video.")
	}
	if err != nil {
		if definiteMutationRejection(err) {
			job.Phase, job.Message = generationRejected, err.Error()
			_ = s.saveGenerationJob(job)
			return nil, fmt.Errorf("lỗi khi gọi %s: %w", rpcID, err)
		}
		if len(accepted) > 0 && accepted[0] != nil {
			accepted[0]()
		}
		return s.reconcileVideoGenerationWithClient(ctx, client, job, template)
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		if definiteMutationRejection(err) {
			job.Phase, job.Message = generationRejected, err.Error()
			_ = s.saveGenerationJob(job)
			return nil, fmt.Errorf("Google Flow từ chối yêu cầu tạo video: %w", err)
		}
		if len(accepted) > 0 && accepted[0] != nil {
			accepted[0]()
		}
		return s.reconcileVideoGenerationWithClient(ctx, client, job, template)
	}
	logRawVideoReply(rpcID, payload)
	// Submit replies list the new media at field 4 (index 3); reference-to-video
	// replies may only name the workflows (index 2), resolved through the listing.
	started, err := parseVideoMedia(payload, 3)
	if err != nil {
		return nil, err
	}
	if len(started) == 0 {
		started = parseVideoOperations(payload)
	}
	if len(started) == 0 {
		if len(accepted) > 0 && accepted[0] != nil {
			accepted[0]()
		}
		return s.reconcileVideoGenerationWithClient(ctx, client, job, template)
	}
	if len(accepted) > 0 && accepted[0] != nil {
		accepted[0]()
	}
	for i := range started {
		if i < len(clientIDs) {
			started[i].clientID = clientIDs[i]
		}
	}
	job.Phase, job.Videos = generationAccepted, trackVideos(started)
	if err := s.saveGenerationJob(job); err != nil {
		return nil, err
	}
	return s.followGenerationVideos(ctx, client, job, started, template)
}
