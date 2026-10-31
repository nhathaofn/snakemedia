package flow

import (
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/providers/flow/session"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/pkg/watermark"
	"github.com/google/uuid"
)

const (
	// fallbackGenerateImagesRPC is the last verified ID of BatchGenerateImages,
	// used only when the running Flow build does not expose its descriptor.
	fallbackGenerateImagesRPC = "ogiZ0b"
	// FlowSurfaceID represents Google Flow Studio's internal surface identifier (22).
	FlowSurfaceID = 22
	// RefTypeImage is the reference type code for images (1).
	RefTypeImage = 1
	// projectTitleLayout matches the date titles Flow gives new projects ("Oct 17 - 13:11").
	projectTitleLayout = "Jan 02 - 15:04"
)

// GetModels payload layout, decoded from captured HTrJv responses:
// [[null, null, tierDefaults, labels, videoFamilies, imageFamilies, null, ttsFamilies]].
// A tier default is [tierCode, [null, null, imageDisplayAlias, videoModelKey, ...]];
// an image family is [displayName, [[modelKey, ..., capabilities(8), ...]], flag, alias, ...].
const (
	modelsTierDefaultsIndex  = 2
	modelsImageFamiliesIndex = 5
	modelCapabilitiesIndex   = 8
	// modelMaxReferencesIndex holds how many reference images a model accepts. Only
	// models that take references carry it (image models 10, r2v 7, edit 5).
	modelMaxReferencesIndex = 9
	// capabilityTextToImage marks a model that generates from a prompt; upsamplers
	// only list their upscale capability and are not generation models.
	capabilityTextToImage = 1
)

// ImageQuality is the requested output resolution in Flow's image editor.
type ImageQuality string

const (
	ImageQuality1K ImageQuality = "1K"
	ImageQuality2K ImageQuality = "2K"
	ImageQuality4K ImageQuality = "4K"
)

// DefaultImageQuality matches the default resolution Flow uses for generated images.
const DefaultImageQuality = ImageQuality1K

// AllImageQualities is bound to TypeScript as an enum.
var AllImageQualities = []struct {
	Value  ImageQuality
	TSName string
}{
	{ImageQuality1K, "OneK"},
	{ImageQuality2K, "TwoK"},
	{ImageQuality4K, "FourK"},
}

// FlowImageAspect maps user-facing aspect ratio strings to Google Flow Protobuf enum values.
var FlowImageAspect = map[string]int{
	"1:1":  1,
	"9:16": 2,
	"16:9": 3,
	"3:4":  4,
	"4:3":  5,
}

// ReferenceImage is one local image uploaded to guide generation.
type ReferenceImage struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mimeType"`
	FileName string `json:"fileName"`
}

// GenerateImageRequest represents the payload for generating images.
type GenerateImageRequest struct {
	RequestID   string           `json:"requestId,omitempty"`
	Prompt      string           `json:"prompt"`
	ModelID     string           `json:"modelId"`
	AspectRatio string           `json:"aspectRatio"`
	Quality     ImageQuality     `json:"quality,omitempty"`
	ProjectMode ImageProjectMode `json:"projectMode,omitempty"`
	Count       int              `json:"count"`
	References  []ReferenceImage `json:"references,omitempty"`
	ProjectID   string           `json:"projectId,omitempty"`
	fingerprint string
	onSubmit    func(projectID string, clientIDs []string) error
	onAccepted  func(images []GeneratedImageItem) error
}

// GeneratedImageItem represents an image returned from generation.
type GeneratedImageItem struct {
	JobID       string `json:"jobId,omitempty"`
	Pending     bool   `json:"pending,omitempty"`
	ID          string `json:"id"`
	URL         string `json:"url"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model"`
	AspectRatio string `json:"aspectRatio"`
	CreatedAt   string `json:"createdAt"`
	// ModelName is the model's display name.
	ModelName string `json:"modelName"`
	// LocalURL serves the downloaded file through the app's asset server; the
	// webview cannot load Google's content URLs itself.
	LocalURL string `json:"localUrl,omitempty"`
	// Error explains why the image could not be downloaded for display.
	Error string `json:"error,omitempty"`
}

type imageGenerationChoice struct {
	account session.StoredAccount
	model   FlowModel
}

func (s *Provider) imageGenerationChoices(ctx context.Context, req GenerateImageRequest) ([]imageGenerationChoice, models.Code, error) {
	accounts, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, models.CodeUnavailable, err
	}
	if len(accounts) == 0 {
		return nil, models.CodeUnavailable, errNoFlowAccount
	}
	choices, firstCatalogErr, firstSelectionErr := accountChoices(ctx, accounts, func(ctx context.Context, acc session.StoredAccount) accountChoice[imageGenerationChoice] {
		available, err := s.modelsFor(ctx, acc)
		if err != nil {
			return accountChoice[imageGenerationChoice]{catalogErr: err}
		}
		idx := slices.IndexFunc(available, func(model FlowModel) bool { return model.ID == req.ModelID })
		if idx < 0 {
			return accountChoice[imageGenerationChoice]{selectErr: fmt.Errorf("Model %q không có trong danh sách model của tài khoản %s.", req.ModelID, acc.Email)}
		}
		model := available[idx]
		if req.Quality != ImageQuality1K && !slices.Contains(model.AvailableQualities, req.Quality) {
			return accountChoice[imageGenerationChoice]{selectErr: fmt.Errorf("Model %s không có chất lượng ảnh %s trên tài khoản %s.", model.Name, req.Quality, acc.Email)}
		}
		if err := checkReferenceLimit(model, len(req.References)); err != nil {
			return accountChoice[imageGenerationChoice]{selectErr: err}
		}
		return accountChoice[imageGenerationChoice]{choice: imageGenerationChoice{account: acc, model: model}, ok: true}
	})
	if len(choices) > 0 {
		return choices, models.CodeOK, nil
	}
	if firstCatalogErr != nil {
		return nil, models.CodeUnavailable, fmt.Errorf("không lấy được danh sách model ảnh từ tài khoản nào: %w", firstCatalogErr)
	}
	if firstSelectionErr != nil {
		return nil, models.CodeInvalidArgument, firstSelectionErr
	}
	return nil, models.CodeInvalidArgument, fmt.Errorf("Model %q không được tài khoản Flow nào hỗ trợ.", req.ModelID)
}

// GenerateImages selects the least-busy account that supports the requested model.
func (s *Provider) GenerateImages(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return models.ErrorResponse[[]GeneratedImageItem](
			"Vui lòng nhập nội dung mô tả (prompt).",
			models.CodeInvalidArgument,
		)
	}
	quality, err := normalizeImageQuality(req.Quality)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInvalidArgument)
	}
	req.Quality = quality
	projectMode, err := normalizeImageProjectMode(req.ProjectMode)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInvalidArgument)
	}
	req.ProjectMode = projectMode
	if err := validateGenerateRequest(req); err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInvalidArgument)
	}
	requestID, err := prepareRequestID(req.RequestID)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInvalidArgument)
	}
	req.RequestID = requestID
	fingerprintReq := req
	fingerprintReq.RequestID = ""
	fingerprintReq.fingerprint = ""
	fingerprintReq.onSubmit = nil
	fingerprintReq.onAccepted = nil
	fingerprint, err := requestFingerprint(fingerprintReq)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInternal)
	}
	unlock, settled, proceed := openBillable(s, s.imageBillable(ctx, req), req.RequestID, fingerprint)
	if !proceed {
		return settled
	}
	defer unlock()

	choices, code, err := s.imageGenerationChoices(ctx, req)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem]("Không thể tạo ảnh: "+err.Error(), code)
	}
	compatible := make([]session.StoredAccount, len(choices))
	for i := range choices {
		compatible[i] = choices[i].account
	}
	// The account's profile stays held (its headless browser must outlive this
	// request) and its session is read fresh from that browser.
	acc, release, err := s.readyAccountFrom(ctx, compatible)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem]("Không thể tạo ảnh: "+err.Error(), models.CodeUnavailable)
	}
	defer func() { release() }()

	var model FlowModel
	for _, choice := range choices {
		if choice.account.ID == acc.ID {
			model = choice.model
			break
		}
	}
	if model.ID == "" {
		return models.ErrorResponse[[]GeneratedImageItem]("Không tìm lại được model ảnh tương thích cho tài khoản đã chọn.", models.CodeInternal)
	}
	req.fingerprint = fingerprint
	req.onSubmit = func(projectID string, clientIDs []string) error {
		job := newGenerationJob(req.RequestID, generationJobImage, fingerprint, acc.ID)
		job.Phase, job.ProjectID, job.ClientIDs = generationSubmitting, projectID, append([]string(nil), clientIDs...)
		job.ImageTemplate = imageGenerationTemplate{Prompt: req.Prompt, ModelID: req.ModelID, ModelName: model.Name, AspectRatio: req.AspectRatio, Quality: req.Quality, ProjectMode: req.ProjectMode}
		return s.saveGenerationJob(job)
	}
	req.onAccepted = func(images []GeneratedImageItem) error {
		job, err := s.loadGenerationJob(req.RequestID)
		if err != nil {
			return err
		}
		job.Phase, job.Images = generationAccepted, append([]GeneratedImageItem(nil), images...)
		return s.saveGenerationJob(job)
	}

	images, err := s.generateLiveImages(ctx, *acc, req)
	if s.refreshLiveCredits != nil {
		s.refreshLiveCredits(ctx, acc.ID)
	}
	if err != nil {
		job, readErr := s.loadGenerationJob(req.RequestID)
		if readErr == nil && job.Phase == generationSubmitting {
			if definiteMutationRejection(err) {
				job.Phase, job.Message = generationRejected, err.Error()
				if saveErr := s.saveGenerationJob(job); saveErr != nil {
					return models.ErrorResponse[[]GeneratedImageItem](saveErr.Error(), models.CodeInternal)
				}
				return models.ErrorResponse[[]GeneratedImageItem](fmt.Sprintf("Lỗi khi tạo ảnh từ Google Flow (%s): %s", acc.Email, err.Error()), models.CodeUnavailable)
			}
			release()
			release = func() {}
			return s.resumeImageGeneration(ctx, job, req)
		}
		// A removed or inaccessible project must be resolved again next time.
		s.forgetProject(acc.ID)
		s.forgetDailyImageProjects(acc.ID)
		return models.ErrorResponse[[]GeneratedImageItem](
			fmt.Sprintf("Lỗi khi tạo ảnh từ Google Flow (%s): %s", acc.Email, err.Error()),
			models.CodeUnavailable,
		)
	}

	for i := range images {
		images[i].ModelName = model.Name
		images[i].JobID = req.RequestID
		images[i].Pending = false
	}
	if job, readErr := s.loadGenerationJob(req.RequestID); readErr == nil {
		job.Phase, job.ImageResult = imageJobPhase(images), append([]GeneratedImageItem(nil), images...)
		if err := s.saveGenerationJob(job); err != nil {
			return models.ErrorResponse[[]GeneratedImageItem](err.Error(), models.CodeInternal)
		}
	}
	return models.SuccessResponse(images)
}

// imageJobPhase keeps a job open while a generated image could not be
// downloaded, so resuming it retries the download instead of freezing the error.
func imageJobPhase(images []GeneratedImageItem) string {
	for _, image := range images {
		if image.LocalURL == "" && image.URL != "" {
			return generationAccepted
		}
	}
	return generationComplete
}

func validateGenerateRequest(req GenerateImageRequest) error {
	if strings.TrimSpace(req.ModelID) == "" {
		return errors.New("Chưa chọn model tạo ảnh.")
	}
	if _, ok := FlowImageAspect[req.AspectRatio]; !ok {
		return fmt.Errorf("Tỉ lệ ảnh %q không được hỗ trợ.", req.AspectRatio)
	}
	if _, err := normalizeImageQuality(req.Quality); err != nil {
		return err
	}
	if _, err := normalizeImageProjectMode(req.ProjectMode); err != nil {
		return err
	}
	if limit := maxImagesPerRequest(); req.Count < 1 || req.Count > limit {
		return fmt.Errorf("Số lượng ảnh phải từ 1 đến %d.", limit)
	}
	if req.ProjectID != "" {
		if _, err := uuid.Parse(req.ProjectID); err != nil {
			return fmt.Errorf("Project ID %q không hợp lệ.", req.ProjectID)
		}
	}
	return validateReferences(req.References)
}

func normalizeImageQuality(quality ImageQuality) (ImageQuality, error) {
	if quality == "" {
		return DefaultImageQuality, nil
	}
	switch quality {
	case ImageQuality1K, ImageQuality2K, ImageQuality4K:
		return quality, nil
	default:
		return "", fmt.Errorf("Chất lượng ảnh %q không được hỗ trợ.", quality)
	}
}

// UploadReferenceImage registers a local image to Google Flow using the RPC ID
// discovered from the account's active Flow build.
func (s *Provider) UploadReferenceImage(ctx context.Context, client *chrome.FlowClient, _ session.StoredAccount, imageBase64 string, mimeType string, fileName string, token string, projectID ...string) (string, error) {
	proj := ""
	if len(projectID) > 0 {
		proj = projectID[0]
	}
	inner, err := BuildImageUploadRequest(imageBase64, mimeType, fileName, proj, token)
	if err != nil {
		return "", fmt.Errorf("không đóng gói được payload maseQ: %w", err)
	}

	rpcID := client.Credentials().RPCs.ID(chrome.MethodUploadImage)
	if rpcID == "" {
		return "", errors.New("Flow RPC tải ảnh tham chiếu chưa được khám phá từ build hiện tại")
	}
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return "", fmt.Errorf("không đóng gói được envelope tải ảnh tham chiếu: %w", err)
	}
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return "", unusualActivity("tải ảnh lên")
	}
	if err != nil {
		return "", fmt.Errorf("lỗi khi gọi Flow UploadImage: %w", err)
	}

	// The request echoes its own IDs; only an ID Flow created can be the media ID.
	return parseUploadedMediaID(body, rpcID, uuidRegex.FindAllString(string(inner), -1)...)
}

// generateImagesFromAccount executes generation against Google Flow using the selected account.
func (s *Provider) generateImagesFromAccount(ctx context.Context, acc session.StoredAccount, req GenerateImageRequest) ([]GeneratedImageItem, error) {
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}
	methods := []string{chrome.MethodProjects, chrome.MethodCreateProject, chrome.MethodProjectContents, chrome.MethodMedia}
	if req.Quality != ImageQuality1K {
		methods = append(methods, chrome.MethodUpsampleImage)
	}
	client, err := s.flowClient(ctx, acc, methods...)
	if err != nil {
		return nil, err
	}
	client = s.withImageGenerationRPC(ctx, acc, client, methods)
	defer s.keepCredentials(acc, client)

	// Resolve the account's real project; the GetProjects call also proves Flow
	// still accepts this session before a browser is involved.
	projectID, finishProjectReservation, err := s.resolveImageProjectForRequest(ctx, client, acc, req.ProjectID, req.ProjectMode, req.Count)
	if err != nil {
		return nil, err
	}
	requestSubmitted := false
	generatedMediaCount := 0
	defer func() {
		if requestSubmitted && generatedMediaCount == 0 {
			generatedMediaCount = req.Count
		}
		finishProjectReservation(generatedMediaCount)
	}()

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
	recaptchaToken, err := s.mintGenerationToken(ctx, browser, policy, chrome.ActionImageGeneration)
	if err != nil {
		return nil, err
	}

	// 4. Dispatch verified image generation RPC ogiZ0b (BatchGenerateImages)
	clientIDs := newGenerationClientIDs(req.Count)
	innerPayload, err := BuildImageBatchRequestWithClientIDs(req.Prompt, projectID, req.AspectRatio, req.ModelID, req.Count, recaptchaToken, refMediaIDs, clientIDs)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload tạo ảnh: %w", err)
	}

	rpcID := client.Credentials().RPCs.ID(chrome.MethodGenerateImages)
	if rpcID == "" {
		rpcID = fallbackGenerateImagesRPC
	}
	genFReq, err := BuildBatchEnvelope(rpcID, innerPayload)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được envelope tạo ảnh: %w", err)
	}
	if req.onSubmit != nil {
		if err := req.onSubmit(projectID, clientIDs); err != nil {
			return nil, fmt.Errorf("không lưu được mã request trước khi gửi Flow: %w", err)
		}
	}

	requestSubmitted = true
	genBody, err := client.Do(ctx, rpcID, genFReq, time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(genBody, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		requestSubmitted = false
		return nil, &mutationRejection{cause: unusualActivity("tạo ảnh")}
	}
	if err != nil {
		if definiteMutationRejection(err) {
			requestSubmitted = false
		}
		return nil, fmt.Errorf("lỗi khi gọi %s: %w", rpcID, err)
	}

	payload, err := chrome.ParseRPCPayload(string(genBody), rpcID)
	if err != nil {
		if definiteMutationRejection(err) {
			requestSubmitted = false
		}
		return nil, fmt.Errorf("Google Flow từ chối yêu cầu tạo ảnh: %w", err)
	}
	images := ParseGeneratedImages(payload, req.Prompt, req.ModelID, req.AspectRatio)
	if len(images) > 0 {
		generatedMediaCount = len(images)
		if req.onAccepted != nil {
			if err := req.onAccepted(images); err != nil {
				return nil, fmt.Errorf("không lưu được kết quả Flow đã nhận: %w", err)
			}
		}
		requestSubmitted = false
		return s.materializeGeneratedImages(ctx, client, browser, policy, projectID, req, images), nil
	}

	limit := len(payload)
	if limit > 200 {
		limit = 200
	}
	return nil, fmt.Errorf("Google Flow chưa trả về ảnh hợp lệ (phản hồi: %s)", payload[:limit])
}

func (s *Provider) materializeGeneratedImages(ctx context.Context, client *chrome.FlowClient, browser *browser.Browser, policy config.AccountConfig, projectID string, req GenerateImageRequest, images []GeneratedImageItem) []GeneratedImageItem {
	for i := range images {
		data, contentType, err := client.FetchMedia(ctx, images[i].URL, []string{"image/"}, s.cfg.Get().Flow.MaxImageBytes, time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds)*time.Second)
		if err == nil {
			data, contentType = s.removeWatermark(images[i].ID, data, contentType)
			images[i].LocalURL, err = s.media.Put(images[i].ID, contentType, ".png", data)
		}
		if err != nil {
			images[i].Error = "Không tải được ảnh để hiển thị: " + err.Error()
			logger.Get().Warn("cannot download generated image", "media", images[i].ID, "error", err)
			continue
		}
		if req.Quality == ImageQuality1K {
			continue
		}
		if browser == nil {
			images[i].Error = fmt.Sprintf("Không nâng được ảnh lên %s: Chrome chưa sẵn sàng.", req.Quality)
			continue
		}
		upscaleToken, tokenErr := s.mintGenerationToken(ctx, browser, policy, chrome.ActionImageGeneration)
		if tokenErr != nil {
			images[i].Error = fmt.Sprintf("Không nâng được ảnh lên %s: %v", req.Quality, tokenErr)
			logger.Get().Warn("cannot mint image upsample token", "media", images[i].ID, "error", tokenErr)
			continue
		}
		upscaledURL, err := s.upsampleGeneratedImage(ctx, client, projectID, images[i].ID, req.Quality, upscaleToken)
		if err != nil {
			images[i].Error = fmt.Sprintf("Không nâng được ảnh lên %s: %v", req.Quality, err)
			logger.Get().Warn("cannot upsample generated image", "media", images[i].ID, "quality", req.Quality, "error", err)
			continue
		}
		images[i].LocalURL = upscaledURL
	}
	return images
}

func (s *Provider) upsampleGeneratedImage(ctx context.Context, client *chrome.FlowClient, projectID, mediaID string, quality ImageQuality, recaptchaToken string) (string, error) {
	rpcID := client.Credentials().RPCs.ID(chrome.MethodUpsampleImage)
	if rpcID == "" {
		return "", errors.New("Flow RPC nâng độ phân giải chưa được khám phá từ build hiện tại")
	}
	inner, err := BuildImageUpsampleRequest(mediaID, quality, projectID, recaptchaToken)
	if err != nil {
		return "", err
	}
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return "", fmt.Errorf("không đóng gói được envelope nâng độ phân giải: %w", err)
	}
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return "", unusualActivity("nâng độ phân giải")
	}
	if err != nil {
		return "", fmt.Errorf("lỗi khi gọi Flow UpsampleImage: %w", err)
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		return "", fmt.Errorf("Google Flow từ chối yêu cầu nâng độ phân giải: %w", err)
	}
	data, contentType, err := ParseUpsampledImageBytes(payload, s.cfg.Get().Flow.MaxImageBytes)
	if err != nil {
		return "", err
	}
	data, contentType = s.removeWatermark(mediaID, data, contentType)
	localURL, err := s.media.Put(mediaID, contentType, ".png", data)
	if err != nil {
		return "", fmt.Errorf("không lưu được ảnh đã nâng độ phân giải: %w", err)
	}
	return localURL, nil
}

// removeWatermark strips Flow's visible mark when the setting is on. An image
// without the mark, or one that cannot be read, is kept as Flow sent it.
func (s *Provider) removeWatermark(mediaID string, data []byte, contentType string) ([]byte, string) {
	if !s.cfg.Get().Flow.AutoRemoveWatermark {
		return data, contentType
	}
	cleaned, removed, err := watermark.Remove(data)
	if err != nil {
		logger.Get().Warn("cannot remove Flow watermark; keeping the original", "media", mediaID, "error", err)
		return data, contentType
	}
	if !removed {
		return data, contentType
	}
	logger.Get().Info("removed Flow watermark", "media", mediaID)
	return cleaned, "image/png"
}

// validateReferences checks reference images before anything is uploaded.
func validateReferences(refs []ReferenceImage) error {
	if len(refs) == 0 {
		return nil
	}
	mimeTypes, maxBytes := referenceMimeTypes(), maxReferenceImageBytes()
	for i, ref := range refs {
		if ref.Base64 == "" {
			return fmt.Errorf("Ảnh tham chiếu %d không có dữ liệu.", i+1)
		}
		if !slices.Contains(mimeTypes, ref.MimeType) {
			return fmt.Errorf("Ảnh tham chiếu %q phải thuộc định dạng Flow nhận (%s).", ref.FileName, strings.Join(mimeTypes, ", "))
		}
		if strings.TrimSpace(ref.FileName) == "" {
			return fmt.Errorf("Ảnh tham chiếu %d thiếu tên tệp.", i+1)
		}
		if base64.StdEncoding.DecodedLen(len(ref.Base64)) > maxBytes {
			return fmt.Errorf("Ảnh tham chiếu %q vượt quá %d MB.", ref.FileName, maxBytes>>20)
		}
	}
	return nil
}
