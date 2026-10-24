package flow

import (
	"SnakeMedia/internal/providers/flow/session"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

const (
	// RPCBatchGenerateImages is the verified synchronous image generation RPC identifier on Google Flow.
	RPCBatchGenerateImages = "ogiZ0b"
	// RPCBatchAsyncGenerateImages is the asynchronous image generation RPC identifier on Google Flow.
	RPCBatchAsyncGenerateImages = "Z9cAtf"
	// RPCUploadImage is the image upload registration RPC identifier on Google Flow.
	RPCUploadImage = "maseQ"
	// FlowSurfaceID represents Google Flow Studio's internal surface identifier (22).
	FlowSurfaceID = 22
	// RefTypeImage is the reference type code for images (1).
	RefTypeImage = 1
	// MaxImagesPerRequest is the largest item count one ogiZ0b request carries.
	MaxImagesPerRequest = 4
	// MaxReferenceImageBytes is the largest reference image accepted for upload.
	MaxReferenceImageBytes = 10 << 20
	// maxGeneratedImageBytes bounds one downloaded generated image.
	maxGeneratedImageBytes = 32 << 20
	// modelCacheTTL bounds how long an account's model catalogue is reused.
	modelCacheTTL = 5 * time.Minute
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

// ImageAspectRatios lists the image ratios in display order (landscape to portrait).
var ImageAspectRatios = []string{"16:9", "4:3", "1:1", "3:4", "9:16"}

// DefaultImageAspectRatio is the ratio a new image request starts with.
const DefaultImageAspectRatio = "1:1"

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

// ReferenceMimeTypes are the reference image formats Flow accepts for upload.
var ReferenceMimeTypes = []string{"image/png", "image/jpeg", "image/webp"}

// FlowImageAspect maps user-facing aspect ratio strings to Google Flow Protobuf enum values.
var FlowImageAspect = map[string]int{
	"1:1":  1,
	"9:16": 2,
	"16:9": 3,
	"3:4":  4,
	"4:3":  5,
}

// BuildImageBatchRequest constructs the verified batchexecute inner payload for RPC ogiZ0b (BatchGenerateImages)
// based on reverse-engineered JSPB AST from Google Flow web client. Every argument is
// required; nothing is replaced by a default.
func BuildImageBatchRequest(prompt string, projectID string, ratio string, modelID string, count int, recaptchaToken string, refMediaIDs ...[]string) ([]byte, error) {
	if count < 1 || count > MaxImagesPerRequest {
		return nil, fmt.Errorf("số lượng ảnh phải từ 1 đến %d", MaxImagesPerRequest)
	}
	aspectCode, ok := FlowImageAspect[ratio]
	if !ok {
		return nil, fmt.Errorf("tỉ lệ ảnh %q không được hỗ trợ", ratio)
	}
	if modelID == "" {
		return nil, errors.New("thiếu model tạo ảnh")
	}
	if projectID == "" {
		return nil, errors.New("thiếu project ID")
	}

	var refs []any
	if len(refMediaIDs) > 0 && len(refMediaIDs[0]) > 0 {
		for _, mid := range refMediaIDs[0] {
			refs = append(refs, []any{mid, nil, nil, nil, RefTypeImage})
		}
	}

	contextObj := flowContext(projectID, recaptchaToken)

	items := make([]any, 0, count)
	for i := 0; i < count; i++ {
		seed := rand.Intn(999999999) + 1
		clientUUID := strings.ToUpper(uuid.NewString())
		// Item structure (14 elements):
		item := []any{
			nil,
			nil,
			refs, // reference media items
			seed,
			aspectCode,
			modelID,
			nil,
			contextObj,
			[][][]string{{{prompt}}},
			nil,
			nil,
			nil,
			nil,
			clientUUID,
		}
		items = append(items, item)
	}

	outerUUID := strings.ToUpper(uuid.NewString())
	innerPayload := []any{
		nil,
		items,
		1,
		contextObj,
		[]string{outerUUID},
	}

	return json.Marshal(innerPayload)
}

// BuildImageUploadRequest constructs the batchexecute inner payload for RPC maseQ (UploadImage).
func BuildImageUploadRequest(imageBase64 string, mimeType string, fileName string, projectID string, recaptchaToken string) ([]byte, error) {
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, fmt.Errorf("kiểu tệp %q không phải hình ảnh", mimeType)
	}
	if fileName == "" {
		return nil, errors.New("thiếu tên tệp ảnh tham chiếu")
	}
	if projectID == "" {
		return nil, errors.New("thiếu project ID")
	}

	uuid1 := strings.ToUpper(uuid.NewString())
	uuid2 := strings.ToUpper(uuid.NewString())

	innerPayload := []any{
		flowContext(projectID, recaptchaToken),
		imageBase64,
		mimeType,
		1,
		nil,
		nil,
		nil,
		nil,
		fileName,
		nil,
		uuid1,
		uuid2,
	}

	return json.Marshal(innerPayload)
}

// BuildImageUpsampleRequest constructs the verified UpsampleImage RPC request.
// Flow's client uses target-resolution enum 1 for 2K and 2 for 4K; 1K is the
// original generation and does not make this RPC.
func BuildImageUpsampleRequest(mediaID string, quality ImageQuality, projectID string, recaptchaToken string) ([]byte, error) {
	if strings.TrimSpace(mediaID) == "" {
		return nil, errors.New("thiếu media ID ảnh cần nâng độ phân giải")
	}
	if projectID == "" {
		return nil, errors.New("thiếu project ID")
	}
	var resolution int
	switch quality {
	case ImageQuality2K:
		resolution = 1
	case ImageQuality4K:
		resolution = 2
	default:
		return nil, fmt.Errorf("mức chất lượng %q không hỗ trợ nâng độ phân giải", quality)
	}
	return json.Marshal([]any{mediaID, resolution, flowContext(projectID, recaptchaToken)})
}

// ParseUpsampledImageBytes extracts the image bytes returned in the response's
// protobuf bytes field. The batchexecute JSON envelope can contain that field at
// different nesting depths, so candidates are accepted only when their decoded
// bytes have a supported raster image content type.
func ParseUpsampledImageBytes(payload string) ([]byte, string, error) {
	var root any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil, "", fmt.Errorf("phản hồi nâng độ phân giải không phải JSON hợp lệ: %w", err)
	}
	var imageData []byte
	var contentType string
	var visit func(any)
	visit = func(value any) {
		if imageData != nil {
			return
		}
		switch item := value.(type) {
		case []any:
			if data, ok := numericImageBytes(item); ok {
				imageData, contentType = data, detectedImageType(data)
				return
			}
			for _, child := range item {
				visit(child)
				if imageData != nil {
					return
				}
			}
		case map[string]any:
			for _, child := range item {
				visit(child)
				if imageData != nil {
					return
				}
			}
		case string:
			candidate := strings.TrimSpace(item)
			if strings.HasPrefix(candidate, "[") || strings.HasPrefix(candidate, "{") {
				var nested any
				if json.Unmarshal([]byte(candidate), &nested) == nil {
					visit(nested)
					return
				}
			}
			if strings.HasPrefix(candidate, "data:") {
				if comma := strings.IndexByte(candidate, ','); comma >= 0 {
					candidate = candidate[comma+1:]
				}
			}
			for _, decoder := range []*base64.Encoding{
				base64.StdEncoding,
				base64.RawStdEncoding,
				base64.URLEncoding,
				base64.RawURLEncoding,
			} {
				data, err := decoder.DecodeString(candidate)
				if err != nil || len(data) == 0 || len(data) > maxGeneratedImageBytes {
					continue
				}
				if detected := detectedImageType(data); detected != "" {
					imageData, contentType = data, detected
					return
				}
			}
		}
	}
	visit(root)
	if imageData == nil {
		return nil, "", errors.New("Google Flow không trả về dữ liệu ảnh hợp lệ sau khi nâng độ phân giải")
	}
	return imageData, contentType, nil
}

func numericImageBytes(values []any) ([]byte, bool) {
	if len(values) == 0 || len(values) > maxGeneratedImageBytes {
		return nil, false
	}
	data := make([]byte, len(values))
	for i, value := range values {
		n, ok := value.(float64)
		if !ok || n < 0 || n > 255 || n != float64(int(n)) {
			return nil, false
		}
		data[i] = byte(n)
	}
	return data, detectedImageType(data) != ""
}

func detectedImageType(data []byte) string {
	typeName := http.DetectContentType(data)
	switch typeName {
	case "image/png", "image/jpeg", "image/webp":
		return typeName
	default:
		return ""
	}
}

// ParseUploadedMediaID extracts the uploaded media ID from the maseQ payload. IDs the
// request itself carried (project ID, client UUIDs) are never mistaken for it.
func ParseUploadedMediaID(raw []byte, sent ...string) (string, error) {
	return parseUploadedMediaID(raw, RPCUploadImage, sent...)
}

func parseUploadedMediaID(raw []byte, rpcID string, sent ...string) (string, error) {
	payload, err := chrome.ParseRPCPayload(string(raw), rpcID)
	if err != nil {
		return "", fmt.Errorf("phản hồi tải ảnh lên không hợp lệ: %w", err)
	}
	skip := make(map[string]bool, len(sent))
	for _, id := range sent {
		skip[strings.ToLower(id)] = true
	}
	for _, m := range uuidRegex.FindAllString(payload, -1) {
		if !skip[strings.ToLower(m)] {
			return m, nil
		}
	}
	return "", errors.New("không tìm thấy media ID trong phản hồi tải ảnh lên")
}

// mediaPathMarkers identify generated image URLs inside the ogiZ0b payload.
var mediaPathMarkers = []string{"flow-content.google/image/", "ai-sandbox-videofx/image/"}

// ParseGeneratedImages walks every string of the decoded ogiZ0b payload (strings
// holding nested JSON included) and returns one item per media ID, in order.
// Decoding first keeps escaped query characters such as = and & intact.
func ParseGeneratedImages(payload string, prompt string, modelID string, ratio string) []GeneratedImageItem {
	var root any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var items []GeneratedImageItem
	now := time.Now().UTC().Format(time.RFC3339)
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			for _, item := range x {
				walk(item)
			}
		case string:
			trimmed := strings.TrimSpace(x)
			if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
				var nested any
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					walk(nested)
					return
				}
			}
			mediaID := generatedMediaID(trimmed)
			if mediaID == "" || seen[mediaID] {
				return
			}
			seen[mediaID] = true
			items = append(items, GeneratedImageItem{
				ID:          mediaID,
				URL:         trimmed,
				Prompt:      prompt,
				Model:       modelID,
				AspectRatio: ratio,
				CreatedAt:   now,
			})
		}
	}
	walk(root)
	return items
}

// generatedMediaID returns the segment between "/image/" and the query of a
// generated image URL, or "" when s is not one.
func generatedMediaID(s string) string {
	if !strings.HasPrefix(s, "https://") {
		return ""
	}
	for _, marker := range mediaPathMarkers {
		if i := strings.Index(s, marker); i >= 0 {
			id := s[i+len(marker):]
			if j := strings.IndexAny(id, "?#/"); j >= 0 {
				id = id[:j]
			}
			return id
		}
	}
	return ""
}

// ReferenceImage is one local image uploaded to guide generation.
type ReferenceImage struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mimeType"`
	FileName string `json:"fileName"`
}

// GenerateImageRequest represents the payload for generating images.
type GenerateImageRequest struct {
	Prompt      string           `json:"prompt"`
	ModelID     string           `json:"modelId"`
	AspectRatio string           `json:"aspectRatio"`
	Quality     ImageQuality     `json:"quality,omitempty"`
	ProjectMode ImageProjectMode `json:"projectMode,omitempty"`
	Count       int              `json:"count"`
	References  []ReferenceImage `json:"references,omitempty"`
	ProjectID   string           `json:"projectId,omitempty"`
}

// GeneratedImageItem represents an image returned from generation.
type GeneratedImageItem struct {
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
		return nil, models.CodeUnavailable, errors.New("không có tài khoản Google Flow nào có cookie hợp lệ. Vui lòng đăng nhập hoặc làm mới tài khoản.")
	}
	choices := make([]imageGenerationChoice, 0, len(accounts))
	var firstCatalogErr, firstSelectionErr error
	for _, acc := range accounts {
		available, err := s.modelsFor(ctx, acc)
		if err != nil {
			if firstCatalogErr == nil {
				firstCatalogErr = err
			}
			continue
		}
		idx := slices.IndexFunc(available, func(model FlowModel) bool { return model.ID == req.ModelID })
		if idx < 0 {
			if firstSelectionErr == nil {
				firstSelectionErr = fmt.Errorf("Model %q không có trong danh sách model của tài khoản %s.", req.ModelID, acc.Email)
			}
			continue
		}
		model := available[idx]
		if req.Quality != ImageQuality1K && !slices.Contains(model.AvailableQualities, req.Quality) {
			if firstSelectionErr == nil {
				firstSelectionErr = fmt.Errorf("Model %s không có chất lượng ảnh %s trên tài khoản %s.", model.Name, req.Quality, acc.Email)
			}
			continue
		}
		if err := checkReferenceLimit(model, len(req.References)); err != nil {
			if firstSelectionErr == nil {
				firstSelectionErr = err
			}
			continue
		}
		choices = append(choices, imageGenerationChoice{account: acc, model: model})
	}
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
	defer release()

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

	images, err := s.generateLiveImages(ctx, *acc, req)
	if s.refreshLiveCredits != nil {
		s.refreshLiveCredits(ctx, acc.ID)
	}
	if err != nil {
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
	}
	return models.SuccessResponse(images)
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
	if req.Count < 1 || req.Count > MaxImagesPerRequest {
		return fmt.Errorf("Số lượng ảnh phải từ 1 đến %d.", MaxImagesPerRequest)
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
	limits := s.cfg.Get().Accounts
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return "", errors.New("Google Flow từ chối tải ảnh lên do token bảo mật không hợp lệ")
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
	limits := s.cfg.Get().Accounts
	methods := []string{chrome.MethodProjects, chrome.MethodCreateProject, chrome.MethodProjectContents, chrome.MethodMedia}
	if req.Quality != ImageQuality1K {
		methods = append(methods, chrome.MethodUpsampleImage)
	}
	client, err := s.flowClient(ctx, acc, methods...)
	if err != nil {
		return nil, err
	}
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

	// 1. Ensure headless browser and mint reCAPTCHA Enterprise token
	var recaptchaToken string
	var refMediaIDs []string
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
	tok, tErr := chrome.MintRecaptchaToken(ctx, browser.Port, policy, chrome.ActionImageGeneration)
	if tErr != nil {
		return nil, fmt.Errorf("lỗi cấp token reCAPTCHA Enterprise từ trình duyệt: %w", tErr)
	}
	if tok == "" {
		return nil, errors.New("token reCAPTCHA Enterprise trả về rỗng")
	}
	recaptchaToken = tok

	// 3. If reference images are supplied, upload each one via RPC maseQ
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

	// 4. Dispatch verified image generation RPC ogiZ0b (BatchGenerateImages)
	innerPayload, err := BuildImageBatchRequest(req.Prompt, projectID, req.AspectRatio, req.ModelID, req.Count, recaptchaToken, refMediaIDs)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload tạo ảnh: %w", err)
	}

	rpcID := RPCBatchGenerateImages
	genFReq, err := BuildBatchEnvelope(rpcID, innerPayload)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được envelope tạo ảnh: %w", err)
	}

	genBody, err := client.Do(ctx, rpcID, genFReq, time.Duration(limits.GenerationTimeoutSeconds)*time.Second)
	if err != nil {
		var responseError *chrome.HTTPError
		requestSubmitted = !errors.As(err, &responseError)
	}
	if bytes.Contains(genBody, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		requestSubmitted = false
		return nil, errors.New("Google Flow yêu cầu xác thực reCAPTCHA Enterprise token (action: IMAGE_GENERATION). Phiên cần được cấp token qua trình duyệt trước khi tạo ảnh.")
	}
	if err != nil {
		return nil, fmt.Errorf("lỗi khi gọi %s: %w", rpcID, err)
	}

	payload, err := chrome.ParseRPCPayload(string(genBody), rpcID)
	if err != nil {
		return nil, fmt.Errorf("Google Flow từ chối yêu cầu tạo ảnh: %w", err)
	}
	images := ParseGeneratedImages(payload, req.Prompt, req.ModelID, req.AspectRatio)
	requestSubmitted = false
	if len(images) > 0 {
		generatedMediaCount = len(images)
		for i := range images {
			data, contentType, fetchErr := client.FetchMedia(ctx, images[i].URL, []string{"image/"}, maxGeneratedImageBytes, time.Duration(limits.GenerationTimeoutSeconds)*time.Second)
			if fetchErr == nil {
				images[i].LocalURL, fetchErr = s.media.Put(images[i].ID, contentType, ".png", data)
			}
			if fetchErr != nil {
				images[i].Error = "Không tải được ảnh để hiển thị: " + fetchErr.Error()
				logger.Get().Warn("cannot download generated image", "media", images[i].ID, "error", fetchErr)
				continue
			}

			if req.Quality == ImageQuality1K {
				continue
			}
			upscaleToken, tokenErr := chrome.MintRecaptchaToken(ctx, browser.Port, policy, chrome.ActionImageGeneration)
			if tokenErr != nil || upscaleToken == "" {
				if tokenErr == nil {
					tokenErr = errors.New("token reCAPTCHA Enterprise trả về rỗng")
				}
				images[i].Error = fmt.Sprintf("Không nâng được ảnh lên %s: %v", req.Quality, tokenErr)
				logger.Get().Warn("cannot mint image upsample token", "media", images[i].ID, "error", tokenErr)
				continue
			}
			upscaledURL, upsampleErr := s.upsampleGeneratedImage(ctx, client, projectID, images[i].ID, req.Quality, upscaleToken)
			if upsampleErr != nil {
				images[i].Error = fmt.Sprintf("Không nâng được ảnh lên %s: %v", req.Quality, upsampleErr)
				logger.Get().Warn("cannot upsample generated image", "media", images[i].ID, "quality", req.Quality, "error", upsampleErr)
				continue
			}
			images[i].LocalURL = upscaledURL
		}
		return images, nil
	}

	limit := len(payload)
	if limit > 200 {
		limit = 200
	}
	return nil, fmt.Errorf("Google Flow chưa trả về ảnh hợp lệ (phản hồi: %s)", payload[:limit])
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
	limits := s.cfg.Get().Accounts
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.GenerationTimeoutSeconds)*time.Second)
	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return "", errors.New("Google Flow từ chối yêu cầu do token bảo mật không hợp lệ")
	}
	if err != nil {
		return "", fmt.Errorf("lỗi khi gọi Flow UpsampleImage: %w", err)
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		return "", fmt.Errorf("Google Flow từ chối yêu cầu nâng độ phân giải: %w", err)
	}
	data, contentType, err := ParseUpsampledImageBytes(payload)
	if err != nil {
		return "", err
	}
	localURL, err := s.media.Put(mediaID, contentType, ".png", data)
	if err != nil {
		return "", fmt.Errorf("không lưu được ảnh đã nâng độ phân giải: %w", err)
	}
	return localURL, nil
}

// validateReferences checks reference images before anything is uploaded.
func validateReferences(refs []ReferenceImage) error {
	for i, ref := range refs {
		if ref.Base64 == "" {
			return fmt.Errorf("Ảnh tham chiếu %d không có dữ liệu.", i+1)
		}
		if !slices.Contains(ReferenceMimeTypes, ref.MimeType) {
			return fmt.Errorf("Ảnh tham chiếu %q phải là PNG, JPEG hoặc WebP.", ref.FileName)
		}
		if strings.TrimSpace(ref.FileName) == "" {
			return fmt.Errorf("Ảnh tham chiếu %d thiếu tên tệp.", i+1)
		}
		if base64.StdEncoding.DecodedLen(len(ref.Base64)) > MaxReferenceImageBytes {
			return fmt.Errorf("Ảnh tham chiếu %q vượt quá %d MB.", ref.FileName, MaxReferenceImageBytes>>20)
		}
	}
	return nil
}
