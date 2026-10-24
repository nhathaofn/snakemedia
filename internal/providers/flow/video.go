package flow

import (
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/providers/flow/session"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/logger"
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
	videoURLMarker = "flow-content.google/video/"
	// MaxVideosPerRequest is the largest output count offered for one request.
	MaxVideosPerRequest     = 4
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

// FlowVideoAspect maps user-facing ratios to Flow's video aspect enum.
var FlowVideoAspect = map[string]int{"9:16": 1, "16:9": 2}

// FlowVideoResolution maps user-facing qualities to Flow's resolution enum (_.eO).
// Only 720p and 360p have generation variants; 1080p and 4K are upsamplers.
var FlowVideoResolution = map[string]int{"720p": 1, "1080p": 2, "4K": 3, "360p": 4}

// videoResolutionOrder lists qualities best first for display.
var videoResolutionOrder = []string{"4K", "1080p", "720p", "360p"}

// Media status codes of a generation (Flow's _.wO mapping).
const videoStatusSuccess = 3

// videoFailedStatuses end a generation without a video: failed, canceled, policy.
var videoFailedStatuses = []int{4, 5, 7}

// GenerateVideoRequest is a text-to-video request.
type GenerateVideoRequest struct {
	Prompt      string `json:"prompt"`
	ModelID     string `json:"modelId"`
	AspectRatio string `json:"aspectRatio"`
	Duration    int    `json:"duration"`
	Resolution  string `json:"resolution"`
	Count       int    `json:"count"`
	ProjectID   string `json:"projectId,omitempty"`
	// References are images the video must follow (reference-to-video).
	References []ReferenceImage `json:"references,omitempty"`
	onAccepted func()
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
	if _, ok := FlowVideoAspect[req.AspectRatio]; !ok {
		return fmt.Errorf("Tỉ lệ video %q không được hỗ trợ.", req.AspectRatio)
	}
	if req.Duration <= 0 {
		return errors.New("Chưa chọn thời lượng video.")
	}
	if _, ok := FlowVideoResolution[req.Resolution]; !ok {
		return fmt.Errorf("Chất lượng video %q không được hỗ trợ.", req.Resolution)
	}
	if req.Count < 1 || req.Count > MaxVideosPerRequest {
		return fmt.Errorf("Số lượng video phải từ 1 đến %d.", MaxVideosPerRequest)
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
		return nil, models.CodeUnavailable, errors.New("không có tài khoản Google Flow nào có cookie hợp lệ. Vui lòng đăng nhập hoặc làm mới tài khoản.")
	}
	choices := make([]videoGenerationChoice, 0, len(accounts))
	var firstCatalogErr, firstSelectionErr error
	for _, acc := range accounts {
		families, _, err := s.videoFamiliesFor(ctx, acc)
		if err != nil {
			if firstCatalogErr == nil {
				firstCatalogErr = err
			}
			continue
		}
		idx := slices.IndexFunc(families, func(f videoFamily) bool { return f.id == req.ModelID })
		if idx < 0 {
			if firstSelectionErr == nil {
				firstSelectionErr = fmt.Errorf("Model %q không có trong danh sách model của tài khoản %s.", req.ModelID, acc.Email)
			}
			continue
		}
		usage, err := selectVideoVariant(families[idx], accountTier(acc), FlowVideoAspect[req.AspectRatio], req.Duration, FlowVideoResolution[req.Resolution], len(req.References) > 0)
		if err != nil {
			if firstSelectionErr == nil {
				firstSelectionErr = err
			}
			continue
		}
		if len(req.References) > usage.maxRefs && len(req.References) > 0 {
			if firstSelectionErr == nil {
				firstSelectionErr = fmt.Errorf("Model %s nhận tối đa %d ảnh tham chiếu (đang chọn %d).", displayName(families[idx].name), usage.maxRefs, len(req.References))
			}
			continue
		}
		cost, known := usage.creditCost(accountTier(acc))
		if known && req.Count > 0 {
			maxInt := int(^uint(0) >> 1)
			if cost > maxInt/req.Count {
				return nil, models.CodeInternal, errors.New("chi phí video vượt quá giới hạn số nguyên")
			}
			cost *= req.Count
		}
		choices = append(choices, videoGenerationChoice{
			rotation: videoAccountCandidate{account: acc, cost: cost},
			family:   families[idx],
			usage:    usage,
		})
	}
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
	choices, code, err := s.videoGenerationChoices(ctx, req)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không thể tạo video: "+err.Error(), code)
	}
	rotationCandidates := make([]videoAccountCandidate, 0, len(choices))
	remaining := append([]videoGenerationChoice(nil), choices...)
	var chosen videoGenerationChoice
	var account session.StoredAccount
	var lease *accountLease
	var releaseProfile func()
	var skipped []string
	for len(remaining) > 0 {
		rotationCandidates = rotationCandidates[:0]
		for _, choice := range remaining {
			rotationCandidates = append(rotationCandidates, choice.rotation)
		}
		idx, nextLease, reserveErr := s.acquireVideoCandidate(ctx, rotationCandidates)
		if reserveErr != nil {
			return models.ErrorResponse[[]GeneratedVideoItem]("Không thể tạo video: "+reserveErr.Error(), models.CodeUnavailable)
		}
		next := remaining[idx]
		nextReleaseProfile := s.accounts.AcquireProfile(next.rotation.account.ProfileDir)
		synced, syncErr := s.syncSession(ctx, next.rotation.account)
		if syncErr == nil {
			chosen, account, lease, releaseProfile = next, synced, nextLease, nextReleaseProfile
			break
		}
		nextReleaseProfile()
		nextLease.release()
		if !errors.Is(syncErr, session.ErrSessionSignedOut) {
			return models.ErrorResponse[[]GeneratedVideoItem](fmt.Sprintf("Không đồng bộ được phiên Flow của %s: %v", next.rotation.account.Email, syncErr), models.CodeUnavailable)
		}
		skipped = append(skipped, next.rotation.account.Email)
		remaining = slices.Delete(remaining, idx, idx+1)
	}
	if lease == nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Các tài khoản đã đăng xuất khỏi Google ("+strings.Join(skipped, ", ")+"); hãy đăng nhập lại ở mục Account.", models.CodeUnavailable)
	}
	defer func() {
		releaseProfile()
		lease.release()
	}()

	req.onAccepted = lease.commitVideoSpend
	videos, err := s.generateLiveVideos(ctx, account, req, chosen.usage.key)
	if s.refreshLiveCredits != nil {
		s.refreshLiveCredits(ctx, account.ID)
	}
	if err != nil {
		s.forgetProject(account.ID)
		return models.ErrorResponse[[]GeneratedVideoItem](
			fmt.Sprintf("Lỗi khi tạo video từ Google Flow (%s): %s", account.Email, err.Error()),
			models.CodeUnavailable,
		)
	}
	lease.commitVideoSpend()
	for i := range videos {
		videos[i].ModelName = displayName(chosen.family.name)
	}
	return models.SuccessResponse(videos)
}

// BuildVideoTextRequest builds the YhhmEf payload as captured by SolwStudio:
// [[item...], context, [batchID, 2]] where each item is
// [[null, null, [[[prompt]]]], variantKey, aspect, null, [null, null, null, null, clientID, clientID]].
// Flow's own client sends one item per requested output and, for any resolution
// other than 720p, adds field 8 = [resolution] (bundle builder u(); not in the capture).
func BuildVideoTextRequest(prompt, projectID, variantKey string, aspect, resolution, count int, recaptchaToken string) ([]byte, error) {
	return buildVideoRequest(prompt, projectID, variantKey, aspect, resolution, count, nil, recaptchaToken)
}

// BuildVideoReferenceRequest builds the MZZa6b payload as captured by SolwStudio:
// each item is [[null, null, [[[prompt]]]], [[null, mediaID]...], variantKey, aspect, null, tailIDs];
// a resolution other than 720p goes in field 12 (bundle builder for t8a).
func BuildVideoReferenceRequest(prompt, projectID, variantKey string, aspect, resolution, count int, mediaIDs []string, recaptchaToken string) ([]byte, error) {
	if len(mediaIDs) == 0 {
		return nil, errors.New("thiếu ảnh tham chiếu")
	}
	return buildVideoRequest(prompt, projectID, variantKey, aspect, resolution, count, mediaIDs, recaptchaToken)
}

func buildVideoRequest(prompt, projectID, variantKey string, aspect, resolution, count int, mediaIDs []string, recaptchaToken string) ([]byte, error) {
	if strings.TrimSpace(prompt) == "" || projectID == "" || variantKey == "" {
		return nil, errors.New("thiếu prompt, project ID hoặc model video")
	}
	if count < 1 || count > MaxVideosPerRequest {
		return nil, fmt.Errorf("số lượng video phải từ 1 đến %d", MaxVideosPerRequest)
	}
	if aspect != 1 && aspect != 2 {
		return nil, fmt.Errorf("tỉ lệ video %d không được hỗ trợ", aspect)
	}
	clientID := func() string { return strings.ToUpper(uuid.NewString()) }
	refs := make([]any, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		refs = append(refs, []any{nil, id})
	}
	items := make([]any, 0, count)
	for i := 0; i < count; i++ {
		item := []any{[]any{nil, nil, [][][]string{{{prompt}}}}}
		resolutionIndex := 7 // field 8 of a text request
		if len(refs) > 0 {
			item = append(item, refs)
			resolutionIndex = 11 // field 12 of a reference request
		}
		item = append(item, variantKey, aspect, nil, []any{nil, nil, nil, nil, clientID(), clientID()})
		if resolution != videoResolution720p {
			for len(item) < resolutionIndex {
				item = append(item, nil)
			}
			item = append(item, []any{resolution})
		}
		items = append(items, item)
	}
	return json.Marshal([]any{items, flowContext(projectID, recaptchaToken), []any{clientID(), 2}})
}

type pendingVideo struct {
	statusObserved bool
	clientID       string
	// operation identifies a generation whose media ID is not known yet
	// (reference-to-video replies name the workflow, not the media).
	operation string
	name      string
	status    int
	errMsg    string
	url       string
}

// parseVideoMedia reads media entries ([name, ..., status at [5][8][0]]) from a
// payload whose media list sits at listIndex.
func parseVideoMedia(payload string, listIndex int) ([]pendingVideo, error) {
	var root []any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil, errors.New("phản hồi video không đúng cấu trúc")
	}
	if len(root) <= listIndex {
		return nil, nil
	}
	var out []pendingVideo
	for _, m := range asList(root[listIndex]) {
		media := asList(m)
		if len(media) == 0 {
			continue
		}
		name, _ := media[0].(string)
		if name == "" {
			continue
		}
		pv := pendingVideo{name: name}
		if len(media) > 2 {
			pv.operation, _ = media[2].(string)
		}
		if len(media) > 5 {
			if holder := asList(media[5]); len(holder) > 8 {
				state := asList(holder[8])
				if len(state) > 0 {
					if code, ok := state[0].(float64); ok {
						pv.status = int(code)
						pv.statusObserved = true
					}
				}
				if len(state) > 1 && state[1] != nil {
					detail, _ := json.Marshal(state[1])
					pv.errMsg = string(detail)
				}
			}
		}
		if pv.statusObserved && pv.status == videoStatusSuccess {
			pv.url = videoOutputURL(media)
		}
		out = append(out, pv)
	}
	return out, nil
}

// parseVideoOperations reads the workflows of a submit reply,
// [null, n, [[operationID, projectID, sceneID, status, ...], ...]] (SolwStudio read_operation).
func parseVideoOperations(payload string) []pendingVideo {
	var root []any
	if json.Unmarshal([]byte(payload), &root) != nil || len(root) < 3 {
		return nil
	}
	var out []pendingVideo
	for _, r := range asList(root[2]) {
		if record := asList(r); len(record) > 0 {
			if id, _ := record[0].(string); id != "" {
				out = append(out, pendingVideo{operation: id})
			}
		}
	}
	return out
}

// listingMediaSlot finds the media ID that follows an operation in the project
// listing ([title, [sec, nanos], isVideo, null, mediaID, operationID, done]).
// Ported from SolwStudio's find_media_id_in_text; isVideo may be null or a bool.
var listingMediaSlot = regexp.MustCompile(`(?:null|true|false),null,\\?"([0-9a-fA-F-]{36})\\?"`)

// mediaForOperation returns the media ID the listing records for an operation.
func mediaForOperation(listing, operation string) string {
	start := strings.Index(strings.ToLower(listing), strings.ToLower(operation))
	if start < 0 {
		return ""
	}
	window := listing[start:min(len(listing), start+800)]
	if m := listingMediaSlot.FindStringSubmatch(window); m != nil {
		return m[1]
	}
	return ""
}

// videoURL returns the finished video URL inside a GetMedia payload, if any.
func videoURL(v any) string {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "https://") && strings.Contains(x, videoURLMarker) {
			return x
		}
	case []any:
		for _, item := range x {
			if u := videoURL(item); u != "" {
				return u
			}
		}
	}
	return ""
}

func videoStatusText(code int) string {
	switch code {
	case 4:
		return "Flow báo tạo video thất bại"
	case 5:
		return "Flow đã huỷ video"
	case 7:
		return "Prompt bị bộ lọc an toàn của Flow chặn"
	case 0:
		return "Flow không còn theo dõi video này"
	}
	return fmt.Sprintf("Flow trả về trạng thái %d", code)
}

func videoFailureDetail(raw string) string {
	if strings.Contains(raw, publicErrorDangerFilter) {
		return "Flow đã từ chối yêu cầu này qua bộ lọc an toàn nội dung. Đây là phản hồi từ Flow, không phải lỗi kết nối. Hãy thử nội dung khác phù hợp với chính sách của Flow."
	}
	return raw
}

func editVideoFailureDetail(raw string) string {
	if strings.Contains(raw, publicErrorDangerFilter) {
		return "Flow đã từ chối video nguồn hoặc yêu cầu chỉnh sửa này qua bộ lọc an toàn nội dung. Đây là phản hồi từ Flow, không phải lỗi kết nối. Hãy thử video nguồn khác hoặc yêu cầu chỉnh sửa khác phù hợp với chính sách của Flow."
	}
	return videoFailureDetail(raw)
}

func logRawVideoReply(rpcID, payload string) {
	limit := len(payload)
	if limit > 4000 {
		limit = 4000
	}
	logger.Get().Debug("flow video reply", "rpc", rpcID, "payload", payload[:limit])
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
	client, err := s.flowClient(ctx, acc, methods...)
	if err != nil {
		return nil, err
	}
	defer s.keepCredentials(acc, client)

	projectID, err := s.resolveProject(ctx, client, acc, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// 1. Ensure headless browser and mint reCAPTCHA Enterprise token
	var recaptchaToken string
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được liên kết để cấp token reCAPTCHA")
	}

	browser, bErr := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
	if bErr != nil {
		return nil, fmt.Errorf("không khởi động được trình duyệt Chrome để cấp bảo mật: %w", bErr)
	}
	if browser == nil {
		return nil, errors.New("trình duyệt Chrome chưa được khởi tạo")
	}

	policy := s.cfg.Get().Accounts
	tok, tErr := chrome.MintRecaptchaToken(ctx, browser.Port, policy, chrome.ActionVideoGeneration)
	if tErr != nil {
		return nil, fmt.Errorf("lỗi cấp token reCAPTCHA Enterprise từ trình duyệt: %w", tErr)
	}
	if tok == "" {
		return nil, errors.New("token reCAPTCHA Enterprise trả về rỗng")
	}
	recaptchaToken = tok

	// 2. If reference images are supplied, upload each one via RPC maseQ
	var refMediaIDs []string
	for _, ref := range req.References {
		uploadToken, uErrToken := chrome.MintRecaptchaToken(ctx, browser.Port, policy, chrome.ActionImageGeneration)
		if uErrToken != nil || uploadToken == "" {
			uploadToken = recaptchaToken
		}
		mediaID, uErr := s.UploadReferenceImage(ctx, client, acc, ref.Base64, ref.MimeType, ref.FileName, uploadToken, projectID)
		if uErr != nil {
			return nil, fmt.Errorf("không tải ảnh tham chiếu %q lên được: %w", ref.FileName, uErr)
		}
		refMediaIDs = append(refMediaIDs, mediaID)
	}

	// 3. Dispatch text-to-video or reference-to-video using the active Flow build.
	rpcID := client.Credentials().RPCs.ID(chrome.MethodGenerateVideoText)
	inner, err := BuildVideoTextRequest(req.Prompt, projectID, variant, FlowVideoAspect[req.AspectRatio], FlowVideoResolution[req.Resolution], req.Count, recaptchaToken)
	if len(refMediaIDs) > 0 {
		rpcID = client.Credentials().RPCs.ID(chrome.MethodGenerateVideoReferences)
		inner, err = BuildVideoReferenceRequest(req.Prompt, projectID, variant, FlowVideoAspect[req.AspectRatio], FlowVideoResolution[req.Resolution], req.Count, refMediaIDs, recaptchaToken)
	}
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload tạo video: %w", err)
	}
	return s.runVideoJob(ctx, client, projectID, rpcID, inner, GeneratedVideoItem{Prompt: req.Prompt, Model: variant, AspectRatio: req.AspectRatio, Duration: req.Duration, Resolution: req.Resolution}, req.onAccepted)
}

// runVideoJob submits a built video request (text, reference or edit), follows
// its videos until Flow finishes them and downloads the results. template
// carries the request's settings into every returned item.
func (s *Provider) runVideoJob(ctx context.Context, client *chrome.FlowClient, projectID, rpcID string, inner []byte, template GeneratedVideoItem, accepted ...func()) ([]GeneratedVideoItem, error) {
	limits := s.cfg.Get().Accounts
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return nil, err
	}
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return nil, errors.New("Google Flow yêu cầu xác thực reCAPTCHA Enterprise token (action: VIDEO_GENERATION). Phiên cần được cấp token qua trình duyệt trước khi tạo video.")
	}
	if err != nil {
		return nil, fmt.Errorf("lỗi khi gọi %s: %w", rpcID, err)
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		return nil, fmt.Errorf("Google Flow từ chối yêu cầu tạo video: %w", err)
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
		return nil, fmt.Errorf("Google Flow không trả về video nào đang tạo (phản hồi: %.200s)", payload)
	}
	if len(accepted) > 0 && accepted[0] != nil {
		accepted[0]()
	}

	final, err := s.waitForVideos(ctx, client, projectID, started, limits.VideoPollIntervalMS, limits.VideoGenerationTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	return s.collectVideoItems(ctx, client, final, template), nil
}

func (s *Provider) collectVideoItems(ctx context.Context, client *chrome.FlowClient, final []pendingVideo, template GeneratedVideoItem) []GeneratedVideoItem {
	limits := s.cfg.Get().Accounts
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
				if template.JobID != "" {
					detail = editVideoFailureDetail(v.errMsg)
				}
				item.Error += ": " + detail
			}
		default:
			local, dlErr := s.downloadVideo(ctx, client, v, limits.GenerationTimeoutSeconds)
			if dlErr != nil {
				item.Error = "Không tải được video: " + dlErr.Error()
				logger.Get().Warn("cannot download generated video", "media", v.name, "error", dlErr)
			} else {
				item.LocalURL = local
				if template.JobID != "" {
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
	timeout := time.Duration(s.cfg.Get().Accounts.RequestTimeoutSeconds) * time.Second
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
				for i, v := range videos {
					if settled(v) {
						continue
					}
					if v.clientID != "" && (v.name == "" || strings.EqualFold(v.name, v.clientID)) {
						if found := mediaForClient(payload, v.clientID); found.name != "" {
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
		local, err = s.media.PutReader(v.name, contentType, ".mp4", reader, s.cfg.Get().Flow.MaxDownloadBytes)
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
