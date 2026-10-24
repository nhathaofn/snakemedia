package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
	"github.com/google/uuid"
)

// Video editing: a prompt rewrites an uploaded video. Verified against the Flow
// web bundle (chunk_studio_1.js) and a live capture of jIps6:
//   - the source goes through the Scotty uploader (chrome.UploadVideo);
//   - like Flow's editor ("generate_video_into_workflow"), one video is made and
//     ratio and quality come from the source, not from settings: the ratio is
//     the nearest of Flow's ratios to the video's size (_.TO, then SO) and an
//     uploaded video is 720p (_.CP resolution 1);
//   - the request item is Y7a: field 1 source range [null, mediaID, startFrame,
//     endFrame], 2 prompt, 3 variant key, 4 aspect, 5 client IDs, 13 resolution
//     (only when not 720p);
//   - the reply has the same layout as text-to-video, so it is followed by the
//     same polling (runVideoJob).
const (
	// RPCEditVideo is /VideoFxService.BatchAsyncGenerateVideoEditVideo.
	RPCEditVideo = "jIps6"
	// capabilityVideoInput marks usages that take a source video (EDIT_VIDEO).
	capabilityVideoInput = 20
	// uploadedVideoFPS is the frame rate Flow assumes for uploaded videos (_.CP: Rd 30).
	uploadedVideoFPS = 30
	// editResolutionIndex is field 13 of an edit request item.
	editResolutionIndex = 12
	// maxSourceVideoBytes is Flow's upload limit for videos (1 GB).
	maxSourceVideoBytes = 1 << 30
	// videoAspectSquare is SO's code for 1:1, which no edit usage offers.
	videoAspectSquare = 0
)

// flowRatios are Flow's ratios (_.KL) with the video aspect SO maps them to.
var flowRatios = []struct {
	name   string
	value  float64
	aspect int
}{
	{"16:9", 16.0 / 9, 2},
	{"4:3", 4.0 / 3, 2},
	{"1:1", 1, videoAspectSquare},
	{"3:4", 0.75, 1},
	{"9:16", 0.5625, 1},
}

// FlowEditModel is a video family that can edit an uploaded video on the
// account's tier.
type FlowEditModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
	// Duration is the length of the edited video in seconds.
	Duration int `json:"duration"`
	// MaxInputSeconds is the longest part of the source video Flow reads.
	MaxInputSeconds int `json:"maxInputSeconds"`
}

// UploadedSourceVideo describes a video successfully uploaded to Google Flow.
type UploadedSourceVideo struct {
	MediaID     string `json:"mediaId"`
	WorkflowID  string `json:"workflowId"`
	DisplayName string `json:"displayName"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// EditVideoRequest rewrites a video imported into the media store.
type EditVideoRequest struct {
	Prompt    string `json:"prompt"`
	ModelID   string `json:"modelId"`
	ProjectID string `json:"projectId,omitempty"`
	// SourceURL is the local URL of the imported source video.
	SourceURL string `json:"sourceUrl"`
	// MediaID is the Flow media ID if already uploaded.
	MediaID string `json:"mediaId,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	// TrimStartMS and TrimEndMS select part of the source; both 0 keeps it whole.
	TrimStartMS int `json:"trimStartMs"`
	TrimEndMS   int `json:"trimEndMs"`
}

// editVideo reports whether the usage edits a source video on the tier.
func (u videoUsage) editVideo(tier int) bool {
	return u.offered(tier) && slices.ContainsFunc(u.caps, func(set []int) bool { return slices.Contains(set, capabilityVideoInput) })
}

// editModels lists families that can edit a video on the tier.
func editModels(families []videoFamily, defaultID string, tier int) []FlowEditModel {
	var list []FlowEditModel
	for _, f := range families {
		m := FlowEditModel{ID: f.id, Name: displayName(f.name), Default: f.id == defaultID}
		for _, u := range f.usages {
			if u.editVideo(tier) {
				m.Duration = max(m.Duration, u.duration)
				m.MaxInputSeconds = max(m.MaxInputSeconds, u.maxInputSeconds)
			}
		}
		if m.Duration > 0 {
			list = append(list, m)
		}
	}
	if len(list) > 0 && !slices.ContainsFunc(list, func(m FlowEditModel) bool { return m.Default }) {
		list[0].Default = true
	}
	return list
}

// editAspect maps a video's size to the aspect Flow sends for it. A video of
// unknown size is treated as landscape, Flow's default for edits (MYb).
func editAspect(width, height int) (name string, aspect int, err error) {
	if width <= 0 || height <= 0 {
		return flowRatios[0].name, flowRatios[0].aspect, nil
	}
	ratio := float64(width) / float64(height)
	best := flowRatios[0]
	for _, r := range flowRatios[1:] {
		if math.Abs(ratio-r.value) < math.Abs(ratio-best.value) {
			best = r
		}
	}
	if best.aspect == videoAspectSquare {
		return "", 0, errors.New("Flow chỉ chỉnh sửa video ngang hoặc dọc, không nhận video vuông (1:1).")
	}
	if best.aspect == 1 {
		return "9:16", best.aspect, nil
	}
	return "16:9", best.aspect, nil
}

// selectEditVariant picks the family's edit usage for the aspect, preferring
// 720p as Flow does for uploads and falling back to the tier's other qualities.
func selectEditVariant(f videoFamily, tier, aspect int) (videoUsage, int, error) {
	for _, name := range []string{"720p", "360p"} {
		resolution := FlowVideoResolution[name]
		for _, u := range f.usages {
			if u.editVideo(tier) && slices.Contains(u.aspects, aspect) && slices.Contains(u.resolutionCodes(), resolution) {
				return u, resolution, nil
			}
		}
	}
	return videoUsage{}, 0, fmt.Errorf("Model %s không chỉnh sửa được video có tỉ lệ này ở gói tài khoản hiện tại.", displayName(f.name))
}

// GetEditModels returns the families that can edit a video for the account
// whose turn is next.
func (s *Provider) GetEditModels(ctx context.Context) models.Response[[]FlowEditModel] {
	acc, err := s.pickAccount(false)
	if err != nil {
		return models.ErrorResponse[[]FlowEditModel]("Không thể lấy model: "+err.Error(), models.CodeUnavailable)
	}
	families, defaultID, err := s.videoFamiliesFor(ctx, *acc)
	if err != nil {
		return models.ErrorResponse[[]FlowEditModel]("Không lấy được danh sách model video từ Google Flow: "+err.Error(), models.CodeUnavailable)
	}
	list := editModels(families, defaultID, accountTier(*acc))
	if len(list) == 0 {
		return models.ErrorResponse[[]FlowEditModel]("Gói tài khoản này không có model chỉnh sửa video.", models.CodeUnavailable)
	}
	return models.SuccessResponse(list)
}

func validateEditRequest(req EditVideoRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return errors.New("Vui lòng nhập nội dung chỉnh sửa (prompt).")
	}
	if strings.TrimSpace(req.ModelID) == "" {
		return errors.New("Chưa chọn model.")
	}
	if strings.TrimSpace(req.SourceURL) == "" {
		return errors.New("Chưa chọn video gốc.")
	}
	if req.TrimStartMS != 0 || req.TrimEndMS != 0 {
		if req.TrimStartMS < 0 || req.TrimEndMS <= req.TrimStartMS {
			return errors.New("Đoạn cắt video không hợp lệ.")
		}
	}
	if req.ProjectID != "" {
		if _, err := uuid.Parse(req.ProjectID); err != nil {
			return fmt.Errorf("Project ID %q không hợp lệ.", req.ProjectID)
		}
	}
	return nil
}

// UploadSourceVideo uploads a local video to Google Flow using Scotty resumable upload.
func (s *Provider) UploadSourceVideo(ctx context.Context, localURL string) models.Response[UploadedSourceVideo] {
	source, err := s.sourceVideo(localURL)
	if err != nil {
		return models.ErrorResponse[UploadedSourceVideo](err.Error(), models.CodeInvalidArgument)
	}
	acc, release, err := s.readyAccount(ctx)
	if err != nil {
		return models.ErrorResponse[UploadedSourceVideo]("Không thể tải video lên Flow: "+err.Error(), models.CodeUnavailable)
	}
	defer release()

	uploaded, err := s.uploadLiveVideo(ctx, *acc, source)
	if err != nil {
		return models.ErrorResponse[UploadedSourceVideo](
			fmt.Sprintf("Lỗi khi tải video lên Google Flow (%s): %s", acc.Email, err.Error()),
			models.CodeUnavailable,
		)
	}
	return models.SuccessResponse(uploaded)
}

// EditVideos uploads the source video and asks Flow to rewrite it with the
// prompt, using the next ready account, then waits for the result.
func (s *Provider) EditVideos(ctx context.Context, req EditVideoRequest) models.Response[[]GeneratedVideoItem] {
	if err := validateEditRequest(req); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	source, err := s.sourceVideo(req.SourceURL)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	acc, release, err := s.readyAccount(ctx)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không thể chỉnh sửa video: "+err.Error(), models.CodeUnavailable)
	}
	defer release()

	families, defaultID, err := s.videoFamiliesFor(ctx, *acc)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Không lấy được danh sách model video: "+err.Error(), models.CodeUnavailable)
	}
	tier := accountTier(*acc)
	editable := editModels(families, defaultID, tier)
	idx := slices.IndexFunc(editable, func(m FlowEditModel) bool { return m.ID == req.ModelID })
	if idx < 0 {
		return models.ErrorResponse[[]GeneratedVideoItem](fmt.Sprintf("Model %q không chỉnh sửa được video ở gói tài khoản hiện tại.", req.ModelID), models.CodeInvalidArgument)
	}
	model := editable[idx]
	if req.TrimEndMS-req.TrimStartMS > model.MaxInputSeconds*1000 {
		return models.ErrorResponse[[]GeneratedVideoItem](
			fmt.Sprintf("Model %s chỉ đọc tối đa %d giây video gốc; hãy cắt ngắn đoạn đã chọn.", model.Name, model.MaxInputSeconds),
			models.CodeInvalidArgument,
		)
	}
	family := families[slices.IndexFunc(families, func(f videoFamily) bool { return f.id == req.ModelID })]

	videos, err := s.editLiveVideos(ctx, *acc, req, source, family)
	if err != nil {
		s.forgetProject(acc.ID)
		return models.ErrorResponse[[]GeneratedVideoItem](
			fmt.Sprintf("Lỗi khi chỉnh sửa video với Google Flow (%s): %s", acc.Email, err.Error()),
			models.CodeUnavailable,
		)
	}
	for i := range videos {
		videos[i].ModelName = model.Name
	}
	return models.SuccessResponse(videos)
}

// localVideo is an imported source video on disk.
type localVideo struct {
	path        string
	fileName    string
	contentType string
}

// sourceVideo resolves an imported video and checks Flow's type and size limits.
func (s *Provider) sourceVideo(localURL string) (localVideo, error) {
	if s.media == nil {
		return localVideo{}, errors.New("kho media chưa được khởi tạo")
	}
	path, err := s.media.Path(localURL)
	if err != nil {
		return localVideo{}, err
	}
	contentType, ok := media.VideoTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return localVideo{}, errors.New("Video gốc phải là mp4, m4v, webm hoặc mov.")
	}
	info, err := os.Stat(path)
	if err != nil {
		return localVideo{}, errors.New("Không tìm thấy video gốc, hãy chọn lại.")
	}
	if info.Size() > maxSourceVideoBytes {
		return localVideo{}, errors.New("Video gốc vượt quá giới hạn 1 GB của Flow.")
	}
	return localVideo{path: path, fileName: filepath.Base(path), contentType: contentType}, nil
}

// BuildEditVideoRequest builds the jIps6 payload for one edited video:
// [[item], context, [batchID, 2]] where item is
// [[null, mediaID, startFrame, endFrame], [null, null, [[[prompt]]]], variantKey,
// aspect, [null, null, null, null, clientID, clientID]] plus field 13 =
// [resolution] for any resolution other than 720p.
func BuildEditVideoRequest(prompt, projectID, variantKey, mediaID string, startFrame, endFrame, aspect, resolution int, recaptchaToken string) ([]byte, error) {
	if strings.TrimSpace(prompt) == "" || projectID == "" || variantKey == "" || mediaID == "" {
		return nil, errors.New("thiếu prompt, project ID, model hoặc video gốc")
	}
	if startFrame < 0 || endFrame <= startFrame {
		return nil, errors.New("đoạn video gốc không hợp lệ")
	}
	if aspect != 1 && aspect != 2 {
		return nil, fmt.Errorf("tỉ lệ video %d không được hỗ trợ", aspect)
	}
	clientID := func() string { return strings.ToUpper(uuid.NewString()) }
	item := []any{
		[]any{nil, mediaID, startFrame, endFrame},
		[]any{nil, nil, [][][]string{{{prompt}}}},
		variantKey,
		aspect,
		[]any{nil, nil, nil, nil, clientID(), clientID()},
	}
	if resolution != videoResolution720p {
		for len(item) < editResolutionIndex {
			item = append(item, nil)
		}
		item = append(item, []any{resolution})
	}
	return json.Marshal([]any{[]any{item}, flowContext(projectID, recaptchaToken), []any{clientID(), 2}})
}

// sourceFrames is the frame range Flow sends right after an upload: from the
// first frame for its assumed clip length (8 s), capped by the usage's input limit.
func sourceFrames(usage videoUsage) (int, int) {
	return 0, min(videoDefaultDuration, usage.maxInputSeconds) * uploadedVideoFPS
}

// uploadVideoFromAccount uploads the source video to Flow via Scotty and returns its info.
func (s *Provider) uploadVideoFromAccount(ctx context.Context, acc session.StoredAccount, source localVideo) (UploadedSourceVideo, error) {
	if s.accounts == nil {
		return UploadedSourceVideo{}, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}
	limits := s.cfg.Get().Accounts
	client, err := chrome.NewFlowClient(acc.Credentials, limits)
	if err != nil {
		return UploadedSourceVideo{}, err
	}
	defer s.keepCredentials(acc, client)

	projectID, err := s.resolveProject(ctx, client, acc, "")
	if err != nil {
		return UploadedSourceVideo{}, err
	}

	uploaded, err := client.UploadVideo(ctx, projectID, source.path, source.fileName, source.contentType, nil, time.Duration(limits.VideoGenerationTimeoutSeconds)*time.Second)
	if err != nil {
		s.forgetProject(acc.ID)
		return UploadedSourceVideo{}, fmt.Errorf("không tải video gốc lên Flow được: %w", err)
	}

	return UploadedSourceVideo{
		MediaID:     uploaded.MediaID,
		WorkflowID:  uploaded.WorkflowID,
		DisplayName: uploaded.DisplayName,
		Width:       uploaded.Width,
		Height:      uploaded.Height,
	}, nil
}

// editVideosFromAccount uploads the source, picks the variant for its size,
// mints a token and runs the edit.
func (s *Provider) editVideosFromAccount(ctx context.Context, acc session.StoredAccount, req EditVideoRequest, source localVideo, family videoFamily) ([]GeneratedVideoItem, error) {
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

	// 1. Resolve mediaID and dimensions: reuse if already uploaded, otherwise upload now.
	mediaID := req.MediaID
	width, height := req.Width, req.Height
	if mediaID == "" {
		var trim *chrome.VideoTrim
		if req.TrimEndMS > 0 {
			trim = &chrome.VideoTrim{StartMS: req.TrimStartMS, EndMS: req.TrimEndMS}
		}
		uploaded, err := client.UploadVideo(ctx, projectID, source.path, source.fileName, source.contentType, trim, time.Duration(limits.VideoGenerationTimeoutSeconds)*time.Second)
		if err != nil {
			return nil, fmt.Errorf("không tải video gốc lên Flow được: %w", err)
		}
		mediaID = uploaded.MediaID
		width, height = uploaded.Width, uploaded.Height
	}
	ratio, aspect, err := editAspect(width, height)
	if err != nil {
		return nil, err
	}
	usage, resolution, err := selectEditVariant(family, accountTier(acc), aspect)
	if err != nil {
		return nil, err
	}

	// 2. Ensure headless browser and mint reCAPTCHA Enterprise token
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

	// 3. Dispatch the edit (jIps6) and follow it like any other video.
	start, end := sourceFrames(usage)
	if req.TrimEndMS > req.TrimStartMS {
		start = int(math.Round(float64(req.TrimStartMS) * float64(uploadedVideoFPS) / 1000.0))
		end = int(math.Round(float64(req.TrimEndMS) * float64(uploadedVideoFPS) / 1000.0))
	}
	inner, err := BuildEditVideoRequest(req.Prompt, projectID, usage.key, mediaID, start, end, aspect, resolution, recaptchaToken)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload chỉnh sửa video: %w", err)
	}
	quality := ""
	for name, code := range FlowVideoResolution {
		if code == resolution {
			quality = name
		}
	}
	return s.runVideoJob(ctx, client, projectID, RPCEditVideo, inner, GeneratedVideoItem{Prompt: req.Prompt, Model: usage.key, AspectRatio: ratio, Duration: usage.duration, Resolution: quality})
}
