package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	"SnakeMedia/pkg/utils"
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
	// capabilityVideoInput marks usages that take a source video (EDIT_VIDEO).
	capabilityVideoInput = 20
	// uploadedVideoFPS is the frame rate Flow assumes for uploaded videos (_.CP: Rd 30).
	uploadedVideoFPS = 30
	// editResolutionIndex is field 13 of an edit request item.
	editResolutionIndex = 12

	// videoAspectSquare is SO's code for 1:1, which no edit usage offers.
	videoAspectSquare = 0
)

// flowRatios is Flow's ratio table (_.KL) with the video aspect SO maps each
// to: the wire mapping from a source video's size, not a list of what Flow
// accepts (the catalogue's edit usages say which aspects can be edited).
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
	// MaxCredits is the most one edit costs on the account's tier; the exact
	// price depends on the variant the source video selects.
	MaxCredits int `json:"maxCredits"`
}

// UploadedSourceVideo describes a video successfully uploaded to Google Flow.
type UploadedSourceVideo struct {
	MinTrimSeconds  float64         `json:"minTrimSeconds"`
	TrimStepSeconds float64         `json:"trimStepSeconds"`
	SourceID        string          `json:"sourceId"`
	DurationSeconds float64         `json:"durationSeconds"`
	Models          []FlowEditModel `json:"models"`
	DisplayName     string          `json:"displayName"`
	Width           int             `json:"width"`
	Height          int             `json:"height"`
}

// EditVideoRequest rewrites a video imported into the media store.
type EditVideoRequest struct {
	SourceID   string `json:"sourceId,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	source     *sourceRecord
	onAccepted func()
	Prompt     string `json:"prompt"`
	ModelID    string `json:"modelId"`
	ProjectID  string `json:"projectId,omitempty"`
	// SourceURL is the local URL of the imported source video.
	SourceURL string `json:"sourceUrl"`
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
				if credits, ok := u.creditCost(tier); ok {
					m.MaxCredits = max(m.MaxCredits, credits)
				}
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

// editCandidates pairs each account offering video edits with the most one
// edit can cost on its tier, the amount reserved while an edit is prepared.
func (s *Provider) editCandidates(ctx context.Context, accounts []session.StoredAccount) []videoAccountCandidate {
	candidates, _, _ := accountChoices(ctx, accounts, func(ctx context.Context, acc session.StoredAccount) accountChoice[videoAccountCandidate] {
		families, defaultID, err := s.videoFamiliesFor(ctx, acc)
		if err != nil {
			return accountChoice[videoAccountCandidate]{catalogErr: err}
		}
		models := editModels(families, defaultID, accountTier(acc))
		if len(models) == 0 {
			return accountChoice[videoAccountCandidate]{}
		}
		cost := 0
		for _, m := range models {
			cost = max(cost, m.MaxCredits)
		}
		return accountChoice[videoAccountCandidate]{ok: true, choice: videoAccountCandidate{account: acc, cost: cost}}
	})
	return candidates
}

// readyEditAccount reserves an edit's credits on one of the candidates and
// syncs its session, skipping accounts that signed out of Google.
func (s *Provider) readyEditAccount(ctx context.Context, candidates []videoAccountCandidate) (*session.StoredAccount, func(), error) {
	if len(candidates) == 0 {
		return nil, nil, errors.New("không có tài khoản nào dùng được model chỉnh sửa video")
	}
	_, synced, _, release, err := readyCandidate(ctx, s, candidates, candidateAccount, s.acquireVideoCandidate)
	if err != nil {
		return nil, nil, err
	}
	return &synced, release, nil
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
// 720p as Flow does for uploads (_.CP) and otherwise taking the first quality an
// edit usage lists for the tier.
func selectEditVariant(f videoFamily, tier, aspect int) (videoUsage, int, error) {
	var fallback *videoUsage
	for i, u := range f.usages {
		if !u.editVideo(tier) || !slices.Contains(u.aspects, aspect) {
			continue
		}
		if slices.Contains(u.resolutionCodes(), videoResolution720p) {
			return u, videoResolution720p, nil
		}
		if fallback == nil {
			fallback = &f.usages[i]
		}
	}
	if fallback != nil {
		return *fallback, fallback.resolutionCodes()[0], nil
	}
	return videoUsage{}, 0, fmt.Errorf("Model %s không chỉnh sửa được video có tỉ lệ này ở gói tài khoản hiện tại.", displayName(f.name))
}

// GetEditModels returns the families that can edit a video for the account
// whose turn is next.
func (s *Provider) GetEditModels(ctx context.Context) models.Response[[]FlowEditModel] {
	acc, err := s.catalogAccount()
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
	if strings.TrimSpace(req.SourceURL) == "" && req.SourceID == "" {
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
	// The edit runs on the account that holds the upload, so pick one that can
	// afford an edit, with the same credit-weighted rotation as video generation.
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return models.ErrorResponse[UploadedSourceVideo]("Không thể tải video lên Flow: "+err.Error(), models.CodeUnavailable)
	}
	acc, release, err := s.readyEditAccount(ctx, s.editCandidates(ctx, accounts))
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

	if uploaded.Video.DurationSeconds <= 0 {
		return models.ErrorResponse[UploadedSourceVideo]("Không đọc được thời lượng nguồn.", models.CodeInvalidArgument)
	}
	if len(uploaded.Video.Models) == 0 {
		families, defaultID, err := s.videoFamiliesFor(ctx, *acc)
		if err != nil {
			return models.ErrorResponse[UploadedSourceVideo](err.Error(), models.CodeUnavailable)
		}
		uploaded.Video.Models = editModels(families, defaultID, accountTier(*acc))
	}
	if len(uploaded.Video.Models) == 0 {
		return models.ErrorResponse[UploadedSourceVideo]("Tài khoản upload không có model Edit video.", models.CodeUnavailable)
	}
	uploaded.ID = uuid.NewString()
	uploaded.AccountID = acc.ID
	uploaded.LocalURL = localURL
	uploaded.Video.SourceID = uploaded.ID
	uploaded.Video.MinTrimSeconds = minTrimSeconds()
	uploaded.Video.TrimStepSeconds = trimStepSeconds()
	if err := s.saveState("sources", uploaded.ID, uploaded); err != nil {
		return models.ErrorResponse[UploadedSourceVideo](err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(uploaded.Video)
}

// EditVideos uploads the source video and asks Flow to rewrite it with the
// prompt, using the next ready account, then waits for the result.
func (s *Provider) EditVideos(ctx context.Context, req EditVideoRequest) models.Response[[]GeneratedVideoItem] {
	if err := validateEditRequest(req); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}

	if req.RequestID == "" {
		req.RequestID = uuid.NewString()
	}
	if _, err := uuid.Parse(req.RequestID); err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem]("Mã yêu cầu không hợp lệ.", models.CodeInvalidArgument)
	}
	jobPath, _ := s.statePath("jobs", req.RequestID)
	if _, err := os.Stat(jobPath); err == nil {
		var saved editJob
		if err := s.readState("jobs", req.RequestID, &saved); err != nil {
			return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
		}
		if !sameEditRequest(saved.Request, req) {
			return models.ErrorResponse[[]GeneratedVideoItem]("Mã yêu cầu đã được dùng cho nội dung khác.", models.CodeInvalidArgument)
		}
		return s.ResumeVideoEdit(ctx, req.RequestID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
	}
	unlock, ok := s.tryJobLock(req.RequestID)
	if !ok {
		return models.ErrorResponse[[]GeneratedVideoItem]("Yêu cầu đang được xử lý.", models.CodeUnavailable)
	}
	defer unlock()
	// Recheck after locking: a completed concurrent submission must not be repeated.
	if _, err := os.Stat(jobPath); err == nil {
		var job editJob
		if err := s.readState("jobs", req.RequestID, &job); err != nil {
			return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInternal)
		}
		if !sameEditRequest(job.Request, req) {
			return models.ErrorResponse[[]GeneratedVideoItem]("Mã yêu cầu đã được dùng cho nội dung khác.", models.CodeInvalidArgument)
		}
		return models.SuccessResponse(job.items())
	}
	record, err := s.loadSource(req)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	if record != nil {
		req.source = record
		req.SourceURL = record.LocalURL
		req.ProjectID = record.ProjectID
	}
	source, err := s.sourceVideo(req.SourceURL)
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeInvalidArgument)
	}
	// Reserve the edit's credits up front, so parallel video work cannot spend
	// them first; the exact cost is recorded once Flow accepts the edit.
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeUnavailable)
	}
	if record != nil {
		accounts = slices.DeleteFunc(accounts, func(a session.StoredAccount) bool { return a.ID != record.AccountID })
		if len(accounts) == 0 {
			return models.ErrorResponse[[]GeneratedVideoItem]("Tài khoản sở hữu nguồn/job không còn kết nối. Hãy kết nối lại tài khoản đó.", models.CodeUnavailable)
		}
	}
	acc, release, err := s.readyEditAccount(ctx, s.editCandidates(ctx, accounts))
	if err != nil {
		return models.ErrorResponse[[]GeneratedVideoItem](err.Error(), models.CodeUnavailable)
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
	if s.refreshLiveCredits != nil {
		s.refreshLiveCredits(ctx, acc.ID)
	}
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
	types := s.SourceVideoTypes()
	contentType, ok := types[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return localVideo{}, fmt.Errorf("Flow chỉ nhận video gốc dạng %s.", strings.Join(slices.Sorted(maps.Keys(types)), ", "))
	}
	info, err := os.Stat(path)
	if err != nil {
		return localVideo{}, errors.New("Không tìm thấy video gốc, hãy chọn lại.")
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return localVideo{}, errors.New("Tệp video rỗng hoặc không hợp lệ.")
	}
	if limit := maxSourceVideoBytes(); info.Size() > limit {
		return localVideo{}, fmt.Errorf("Video gốc vượt giới hạn %s của Flow.", utils.FormatBytes(uint64(limit)))
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

// editSourceFrames uses the actual duration and selected usage limit.
// Flow normalizes uploaded sources to a 30 fps timeline, independently of file fps.
func editSourceFrames(duration float64, startMS, endMS, maxSeconds int) (int, int, error) {
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) || maxSeconds <= 0 {
		return 0, 0, errors.New("Thiếu thời lượng nguồn hoặc giới hạn model.")
	}
	start, end := float64(startMS)/1000, float64(endMS)/1000
	if startMS == 0 && endMS == 0 {
		end = math.Min(duration, float64(maxSeconds))
	}
	if start < 0 || end <= start || end > duration+1/float64(uploadedVideoFPS) || (endMS > 0 && endMS-startMS > maxSeconds*1000) {
		return 0, 0, errors.New("Đoạn cắt vượt quá video gốc hoặc giới hạn model.")
	}
	first, last := int(math.Round(start*uploadedVideoFPS)), int(math.Round(math.Min(end, duration)*uploadedVideoFPS))
	last = min(last, first+maxSeconds*uploadedVideoFPS)
	if last <= first {
		return 0, 0, errors.New("Đoạn cắt ngắn hơn một frame.")
	}
	return first, last, nil
}

// uploadVideoFromAccount uploads the source video to Flow via Scotty and returns its info.
func (s *Provider) uploadVideoFromAccount(ctx context.Context, acc session.StoredAccount, source localVideo) (sourceRecord, error) {
	return s.prepareSourceVideo(ctx, acc, source, "")
}

func (s *Provider) prepareSourceVideo(ctx context.Context, acc session.StoredAccount, source localVideo, requestedProject string) (sourceRecord, error) {
	ctx, cancelUpload := context.WithTimeout(ctx, time.Duration(s.cfg.Get().Flow.VideoGenerationTimeoutSeconds)*time.Second)
	defer cancelUpload()
	if s.accounts == nil {
		return sourceRecord{}, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}

	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Get().FlowPagePolicy().RequestTimeoutSeconds)*time.Second)
	metadata, err := media.ProbeVideo(probeCtx, source.path, s.prober(featureVideoEdit))
	cancel()
	if err != nil {
		return sourceRecord{}, err
	}
	if _, _, err := editAspect(metadata.Width, metadata.Height); err != nil {
		return sourceRecord{}, err
	}
	client, err := s.flowClient(ctx, acc, chrome.MethodProjects, chrome.MethodCreateProject)
	if err != nil {
		return sourceRecord{}, err
	}
	defer func() {
		if client != nil {
			s.keepCredentials(acc, client)
		}
	}()

	projectID, err := s.resolveProject(ctx, client, acc, requestedProject)
	if err != nil {
		return sourceRecord{}, err
	}

	client, err = s.editClient(ctx, acc, projectID)
	if err != nil {
		return sourceRecord{}, err
	}
	acc.Credentials = client.Credentials()
	families, defaultID, err := s.videoFamiliesFor(ctx, acc)
	if err != nil {
		return sourceRecord{}, err
	}
	editable := editModels(families, defaultID, accountTier(acc))
	if len(editable) == 0 {
		return sourceRecord{}, errors.New("Tài khoản không có model Edit video.")
	}
	uploaded, err := client.UploadVideo(ctx, projectID, source.path, source.fileName, source.contentType, nil, time.Duration(s.cfg.Get().Flow.VideoGenerationTimeoutSeconds)*time.Second)
	if err != nil {
		s.forgetProject(acc.ID)
		return sourceRecord{}, fmt.Errorf("không tải video gốc lên Flow được: %w", err)
	}

	prepared, err := s.waitForUploadedSource(ctx, client, uploaded.MediaID)
	if err != nil {
		return sourceRecord{}, err
	}
	return sourceRecord{Original: metadata, ProjectID: projectID, MediaID: uploaded.MediaID, WorkflowID: uploaded.WorkflowID, Video: UploadedSourceVideo{
		Models: editable, DurationSeconds: prepared.DurationSeconds, DisplayName: uploaded.DisplayName, Width: prepared.Width, Height: prepared.Height,
	}}, nil
}

// editVideosFromAccount uploads the source, picks the variant for its size,
// mints a token and runs the edit.
func (s *Provider) editVideosFromAccount(ctx context.Context, acc session.StoredAccount, req EditVideoRequest, source localVideo, family videoFamily) ([]GeneratedVideoItem, error) {
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}

	if req.source == nil {
		uploaded, err := s.prepareSourceVideo(ctx, acc, source, req.ProjectID)
		if err != nil {
			return nil, err
		}
		uploaded.AccountID = acc.ID
		uploaded.LocalURL = req.SourceURL
		req.source = &uploaded
	}

	client, err := s.editClient(ctx, acc, req.source.ProjectID)
	if err != nil {
		return nil, err
	}
	defer s.keepCredentials(acc, client)
	projectID := req.source.ProjectID
	mediaID := req.source.MediaID
	// Revalidate a cached source before any billable mutation, including sources
	// restored after a restart or uploaded before readiness tracking existed.
	readyCtx, cancelReady := context.WithTimeout(ctx, time.Duration(s.cfg.Get().Flow.VideoGenerationTimeoutSeconds)*time.Second)
	prepared, err := s.waitForUploadedSource(readyCtx, client, mediaID)
	cancelReady()
	if err != nil {
		return nil, err
	}
	req.source.Video.Width, req.source.Video.Height, req.source.Video.DurationSeconds = prepared.Width, prepared.Height, prepared.DurationSeconds
	if req.source.ID != "" {
		if err := s.saveState("sources", req.source.ID, req.source); err != nil {
			return nil, err
		}
	}
	width, height := req.source.Video.Width, req.source.Video.Height
	ratio, aspect, err := editAspect(width, height)
	if err != nil {
		return nil, err
	}
	usage, resolution, err := selectEditVariant(family, accountTier(acc), aspect)
	if err != nil {
		return nil, err
	}

	start, end, err := editSourceFrames(req.source.Video.DurationSeconds, req.TrimStartMS, req.TrimEndMS, usage.maxInputSeconds)
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

	tok, tErr := s.mintGenerationToken(ctx, browser, s.cfg.Get().FlowPagePolicy(), chrome.ActionVideoGeneration)
	if tErr != nil {
		return nil, tErr
	}
	recaptchaToken = tok

	// 3. Dispatch the edit (jIps6) and follow it like any other video.
	inner, err := BuildEditVideoRequest(req.Prompt, projectID, usage.key, mediaID, start, end, aspect, resolution, recaptchaToken)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload chỉnh sửa video: %w", err)
	}
	if cost, ok := usage.creditCost(accountTier(acc)); ok {
		req.onAccepted = func() { s.recordVideoSpend(acc, cost) }
	}
	quality := ""
	for name, code := range FlowVideoResolution {
		if code == resolution {
			quality = name
		}
	}
	return s.runEditVideoJob(ctx, client, acc, req, projectID, client.Credentials().RPCs.ID(chrome.MethodEditVideo), inner, GeneratedVideoItem{ModelName: displayName(family.name), Prompt: req.Prompt, Model: usage.key, AspectRatio: ratio, Duration: int(math.Round(float64(end-start) / uploadedVideoFPS)), Resolution: quality})
}
