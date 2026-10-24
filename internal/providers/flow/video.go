package flow

import (
	"SnakeMedia/internal/providers/flow/session"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// RPCGenerateVideoText is /VideoFxService.BatchAsyncGenerateVideoText.
	RPCGenerateVideoText = "YhhmEf"
	// RPCGenerateVideoReferences is /VideoFxService.BatchAsyncGenerateVideoReferenceImages.
	RPCGenerateVideoReferences = "MZZa6b"
	// RPCGetProjectContents is /Provider.GetProjectContents (project listing).
	RPCGetProjectContents = "Zzl0ze"
	// RPCGetMedia is /Provider.GetMedia; a video is ready once it carries a
	// flow-content.google/video/ URL.
	RPCGetMedia = "as29s"
	// videoURLMarker identifies the finished video file among a media's URLs.
	videoURLMarker = "flow-content.google/video/"
	// maxPollFailures stops polling after this many consecutive failed rounds.
	maxPollFailures = 5
	// MaxVideosPerRequest is the largest output count offered for one request.
	MaxVideosPerRequest = 4
	// maxGeneratedVideoBytes bounds one downloaded video.
	maxGeneratedVideoBytes = 512 << 20
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
}

// GeneratedVideoItem is one requested output video.
type GeneratedVideoItem struct {
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

// GenerateVideos dispatches text-to-video with round-robin account rotation and
// waits until Flow finishes rendering or the configured timeout passes.
func (s *Provider) GenerateVideos(ctx context.Context, req GenerateVideoRequest) models.Response[[]GeneratedVideoItem] {
	if err := validateVideoRequest(req); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	acc, release, err := s.readyAccount(ctx)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không thể tạo video: "+err.Error(), models.CodeUnavailable)
	}
	defer release()

	families, _, err := s.videoFamiliesFor(ctx, *acc)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không lấy được danh sách model video: "+err.Error(), models.CodeUnavailable)
	}
	idx := slices.IndexFunc(families, func(f videoFamily) bool { return f.id == req.ModelID })
	if idx < 0 {
		return models.ErrorResponse[[]GeneratedVideoItem](fmt.Sprintf("Model %q không có trong danh sách model của tài khoản.", req.ModelID), models.CodeInvalidArgument)
	}
	usage, err := selectVideoVariant(families[idx], accountTier(*acc), FlowVideoAspect[req.AspectRatio], req.Duration, FlowVideoResolution[req.Resolution], len(req.References) > 0)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	if len(req.References) > usage.maxRefs && len(req.References) > 0 {
		return models.ErrorResponse[[]GeneratedVideoItem](
			fmt.Sprintf("Model %s nhận tối đa %d ảnh tham chiếu (đang chọn %d).", displayName(families[idx].name), usage.maxRefs, len(req.References)),
			models.CodeInvalidArgument,
		)
	}

	videos, err := s.generateLiveVideos(ctx, *acc, req, usage.key)
	if err != nil {
		s.forgetProject(acc.ID)
		return models.ErrorResponse[[]GeneratedVideoItem](
			fmt.Sprintf("Lỗi khi tạo video từ Google Flow (%s): %s", acc.Email, err.Error()),
			models.CodeUnavailable,
		)
	}
	for i := range videos {
		videos[i].ModelName = displayName(families[idx].name)
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
		if len(media) > 5 {
			if holder := asList(media[5]); len(holder) > 8 {
				state := asList(holder[8])
				if len(state) > 0 {
					if code, ok := state[0].(float64); ok {
						pv.status = int(code)
					}
				}
				if len(state) > 1 && state[1] != nil {
					detail, _ := json.Marshal(state[1])
					pv.errMsg = string(detail)
				}
			}
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
	limits := s.cfg.Get().Accounts
	client, err := chrome.NewFlowClient(acc.Credentials, limits)
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

	// 3. Dispatch text-to-video (YhhmEf) or reference-to-video (MZZa6b)
	rpcID := RPCGenerateVideoText
	inner, err := BuildVideoTextRequest(req.Prompt, projectID, variant, FlowVideoAspect[req.AspectRatio], FlowVideoResolution[req.Resolution], req.Count, recaptchaToken)
	if len(refMediaIDs) > 0 {
		rpcID = RPCGenerateVideoReferences
		inner, err = BuildVideoReferenceRequest(req.Prompt, projectID, variant, FlowVideoAspect[req.AspectRatio], FlowVideoResolution[req.Resolution], req.Count, refMediaIDs, recaptchaToken)
	}
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload tạo video: %w", err)
	}
	return s.runVideoJob(ctx, client, projectID, rpcID, inner, GeneratedVideoItem{Prompt: req.Prompt, Model: variant, AspectRatio: req.AspectRatio, Duration: req.Duration, Resolution: req.Resolution})
}

// runVideoJob submits a built video request (text, reference or edit), follows
// its videos until Flow finishes them and downloads the results. template
// carries the request's settings into every returned item.
func (s *Provider) runVideoJob(ctx context.Context, client *chrome.FlowClient, projectID, rpcID string, inner []byte, template GeneratedVideoItem) ([]GeneratedVideoItem, error) {
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

	// 4. Follow each video until it has a video URL or Flow reports a failure.
	final, err := s.waitForVideos(ctx, client, projectID, started, limits.VideoPollIntervalMS, limits.VideoGenerationTimeoutSeconds)
	if err != nil {
		return nil, err
	}

	// 5. Download finished videos into the app's media folder.
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
				item.Error += ": " + v.errMsg
			}
		default:
			local, dlErr := s.downloadVideo(ctx, client, v, limits.GenerationTimeoutSeconds)
			if dlErr != nil {
				item.Error = "Không tải được video: " + dlErr.Error()
				logger.Get().Warn("cannot download generated video", "media", v.name, "error", dlErr)
			} else {
				item.LocalURL = local
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// waitForVideos polls until every started video has a video URL or failed. As in
// SolwStudio, GetMedia is the authority for the URL because the project listing
// can lag behind; the listing supplies failure states and the media IDs of
// generations known only by operation.
func (s *Provider) waitForVideos(ctx context.Context, client *chrome.FlowClient, projectID string, started []pendingVideo, intervalMS, timeoutSeconds int) ([]pendingVideo, error) {
	timeout := time.Duration(s.cfg.Get().Accounts.RequestTimeoutSeconds) * time.Second
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	videos := append([]pendingVideo(nil), started...)
	settled := func(v pendingVideo) bool { return v.url != "" || slices.Contains(videoFailedStatuses, v.status) }
	listing, err := BuildBatchEnvelope(RPCGetProjectContents, []any{"projects/" + projectID, nil, nil, nil, []any{1}})
	if err != nil {
		return nil, err
	}
	call := func(rpcID, fReq string) (string, error) {
		body, err := client.Do(ctx, rpcID, fReq, timeout)
		if err != nil {
			return "", err
		}
		return chrome.ParseRPCPayload(string(body), rpcID)
	}

	failures := 0
	var lastErr error
	for slices.ContainsFunc(videos, func(v pendingVideo) bool { return !settled(v) }) {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("quá %d giây mà Flow chưa tạo xong video", timeoutSeconds)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(intervalMS) * time.Millisecond):
		}

		roundFailed := false
		if payload, err := call(RPCGetProjectContents, listing); err == nil {
			// GetProjectContents: field 3 (index 2) lists the project's media.
			media, _ := parseVideoMedia(payload, 2)
			for i, v := range videos {
				if settled(v) {
					continue
				}
				if v.name == "" {
					videos[i].name = mediaForOperation(payload, v.operation)
				}
				for _, m := range media {
					if m.name == videos[i].name {
						videos[i].status, videos[i].errMsg = m.status, m.errMsg
					}
				}
			}
		} else {
			roundFailed, lastErr = true, err
		}
		for i, v := range videos {
			if settled(v) || v.name == "" {
				continue
			}
			fReq, err := BuildBatchEnvelope(RPCGetMedia, []any{v.name})
			if err != nil {
				return nil, err
			}
			payload, err := call(RPCGetMedia, fReq)
			if err != nil {
				// A media still rendering may not be served yet ("Media not found");
				// only transport failures count against the poll.
				var remote *chrome.HTTPError
				if errors.As(err, &remote) || ctx.Err() != nil {
					roundFailed, lastErr = true, err
				}
				continue
			}
			var decoded any
			if json.Unmarshal([]byte(payload), &decoded) == nil {
				if u := videoURL(decoded); u != "" {
					videos[i].url, videos[i].status = u, videoStatusSuccess
				}
			}
		}
		if roundFailed {
			failures++
			if failures >= maxPollFailures {
				return nil, fmt.Errorf("lỗi khi theo dõi tiến độ video: %w", lastErr)
			}
		} else {
			failures = 0
		}
	}
	return videos, nil
}

func (s *Provider) downloadVideo(ctx context.Context, client *chrome.FlowClient, v pendingVideo, timeoutSeconds int) (string, error) {
	data, contentType, err := client.FetchMedia(ctx, v.url, []string{"video/", "application/octet-stream"}, maxGeneratedVideoBytes, time.Duration(timeoutSeconds)*time.Second)
	if err != nil {
		return "", err
	}
	return s.media.Put(v.name, contentType, ".mp4", data)
}
