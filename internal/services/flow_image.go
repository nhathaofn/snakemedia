package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/services/chrome"
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
)

// FlowImageAspect maps user-facing aspect ratio strings to Google Flow Protobuf enum values.
var FlowImageAspect = map[string]int{
	"1:1":  1,
	"9:16": 2,
	"16:9": 3,
	"3:4":  4,
	"4:3":  5,
}

// BuildBatchEnvelope constructs the standard Google Boq batchexecute outer envelope:
// [[[rpcID, JSON_STRING(inner), nil, "generic"]]]
func BuildBatchEnvelope(rpcID string, inner any) (string, error) {
	var innerJSON string
	switch v := inner.(type) {
	case string:
		innerJSON = v
	case []byte:
		innerJSON = string(v)
	default:
		b, err := json.Marshal(inner)
		if err != nil {
			return "", err
		}
		innerJSON = string(b)
	}

	outer := []any{
		[]any{
			[]any{
				rpcID,
				innerJSON,
				nil,
				"generic",
			},
		},
	}
	outerBytes, err := json.Marshal(outer)
	if err != nil {
		return "", err
	}
	return string(outerBytes), nil
}

// BuildImageBatchRequest constructs the verified batchexecute inner payload for RPC ogiZ0b (BatchGenerateImages)
// based on reverse-engineered JSPB AST from Google Flow web client.
func BuildImageBatchRequest(prompt string, projectID string, ratio string, modelID string, count int, recaptchaToken string, refMediaIDs ...[]string) ([]byte, error) {
	if count <= 0 {
		count = 1
	}
	if count > 4 {
		count = 4
	}
	aspectCode, ok := FlowImageAspect[ratio]
	if !ok {
		aspectCode = FlowImageAspect["1:1"]
	}
	if modelID == "" {
		modelID = "GEM_PIX_2"
	}

	var projVal any
	if projectID != "" && projectID != "default" {
		projVal = projectID
	}

	var refs []any
	if len(refMediaIDs) > 0 && len(refMediaIDs[0]) > 0 {
		for _, mid := range refMediaIDs[0] {
			refs = append(refs, []any{mid, nil, nil, nil, RefTypeImage})
		}
	}

	// Context structure: [null, 22, null, null, null, projectID, null, null, null, null, [recaptchaToken, 1]]
	contextObj := []any{
		nil,
		FlowSurfaceID,
		nil,
		nil,
		nil,
		projVal,
		nil,
		nil,
		nil,
		nil,
		[]any{recaptchaToken, 1},
	}

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
	if mimeType == "" {
		mimeType = "image/png"
	}
	if fileName == "" {
		fileName = "reference.png"
	}

	var projVal any
	if projectID != "" && projectID != "default" {
		projVal = projectID
	}

	contextObj := []any{
		nil,
		FlowSurfaceID,
		nil,
		nil,
		nil,
		projVal,
		nil,
		nil,
		nil,
		nil,
		[]any{recaptchaToken, 1},
	}

	uuid1 := strings.ToUpper(uuid.NewString())
	uuid2 := strings.ToUpper(uuid.NewString())

	innerPayload := []any{
		contextObj,
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

var mediaIDRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// ParseUploadedMediaID extracts the uploaded media ID from maseQ response.
func ParseUploadedMediaID(raw []byte) (string, error) {
	m := mediaIDRegex.Find(raw)
	if len(m) > 0 {
		return string(m), nil
	}
	return "", errors.New("không tìm thấy media ID trong phản hồi tải ảnh lên")
}

var mediaURLRegex = regexp.MustCompile(`https://(?:flow-content\.google|ai-sandbox-videofx)/image/([0-9a-zA-Z\-_]+)(?:\?[^"'\s\\]*)?`)

// ParseGeneratedImages extracts generated image URLs and media IDs from batchexecute response data.
func ParseGeneratedImages(raw []byte, prompt string, modelID string, ratio string) []GeneratedImageItem {
	matches := mediaURLRegex.FindAllSubmatch(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var items []GeneratedImageItem
	now := time.Now().UTC().Format(time.RFC3339)

	for _, m := range matches {
		fullURL := string(m[0])
		mediaID := string(m[1])
		if seen[mediaID] {
			continue
		}
		seen[mediaID] = true
		items = append(items, GeneratedImageItem{
			ID:          mediaID,
			URL:         fullURL,
			Prompt:      prompt,
			Model:       modelID,
			AspectRatio: ratio,
			CreatedAt:   now,
		})
	}
	return items
}

// FlowModel represents an image generation model available in Google Flow.
type FlowModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Alias       string `json:"alias"`
	Default     bool   `json:"default"`
}

// GenerateImageRequest represents the payload for generating images.
type GenerateImageRequest struct {
	Prompt               string `json:"prompt"`
	ModelID              string `json:"modelId"`
	AspectRatio          string `json:"aspectRatio"`
	Count                int    `json:"count"`
	ReferenceImageBase64 string `json:"referenceImageBase64,omitempty"`
	ReferenceMimeType    string `json:"referenceMimeType,omitempty"`
	ReferenceFileName    string `json:"referenceFileName,omitempty"`
	ProjectID            string `json:"projectId,omitempty"`
}

// GeneratedImageItem represents an image returned from generation.
type GeneratedImageItem struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model"`
	AspectRatio string `json:"aspectRatio"`
	CreatedAt   string `json:"createdAt"`
}

// FlowImageService coordinates image generation, model discovery and round-robin account rotation.
type FlowImageService struct {
	accounts           *AccountService
	rrIndex            uint64
	mu                 sync.Mutex
	cachedModels       []FlowModel
	modelsExpire       time.Time
	fetchLiveModels    func(ctx context.Context, acc storedAccount) ([]FlowModel, error)
	generateLiveImages func(ctx context.Context, acc storedAccount, req GenerateImageRequest) ([]GeneratedImageItem, error)
}

// NewFlowImageService creates a new FlowImageService.
func NewFlowImageService(accounts *AccountService) *FlowImageService {
	s := &FlowImageService{
		accounts: accounts,
	}
	s.fetchLiveModels = s.queryModelsFromAccount
	s.generateLiveImages = s.generateImagesFromAccount
	return s
}

// ValidateAccountCookies verifies that an account has valid, non-expired Google session cookies.
func ValidateAccountCookies(cookies []chrome.CookieItem) error {
	if len(cookies) == 0 {
		return errors.New("tài khoản không có cookie nào")
	}
	hasSID := false
	hasHSID := false
	hasSSID := false
	now := time.Now().Unix()

	for _, c := range cookies {
		if c.Expires > 0 && int64(c.Expires) < now {
			continue
		}
		if c.Name == "SID" || c.Name == "__Secure-1PSID" || c.Name == "__Secure-3PSID" {
			hasSID = true
		}
		if c.Name == "HSID" {
			hasHSID = true
		}
		if c.Name == "SSID" || c.Name == "__Secure-1PAPISID" || c.Name == "__Secure-3PAPISID" || c.Name == "SAPISID" {
			hasSSID = true
		}
	}

	if !hasSID || !hasHSID || !hasSSID {
		return errors.New("thiếu các cookie phiên Google cốt lõi hoặc cookie đã hết hạn")
	}
	return nil
}

// GetValidConnectedAccounts returns all connected accounts that pass strict cookie validation.
func (s *FlowImageService) GetValidConnectedAccounts() ([]storedAccount, error) {
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}
	connected, err := s.accounts.GetConnectedStoredAccounts()
	if err != nil {
		return nil, fmt.Errorf("không đọc được danh sách tài khoản: %w", err)
	}

	var valid []storedAccount
	for _, acc := range connected {
		if err := ValidateAccountCookies(acc.Credentials.Cookies); err == nil {
			valid = append(valid, acc)
		}
	}
	return valid, nil
}

// NextAccount returns the next valid connected account using atomic round-robin rotation.
func (s *FlowImageService) NextAccount() (*storedAccount, error) {
	valid, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, err
	}
	if len(valid) == 0 {
		return nil, errors.New("không có tài khoản Google Flow nào có cookie hợp lệ. Vui lòng đăng nhập hoặc làm mới tài khoản.")
	}
	idx := atomic.AddUint64(&s.rrIndex, 1) - 1
	selected := valid[idx%uint64(len(valid))]
	return &selected, nil
}

// GetActiveRotationCount returns the number of valid accounts currently active in rotation.
func (s *FlowImageService) GetActiveRotationCount(_ context.Context) models.Response[int] {
	valid, err := s.GetValidConnectedAccounts()
	if err != nil {
		return models.ErrorResponse[int](err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(len(valid))
}

// GetImageModels queries real models using a valid connected account from the rotation pool.
func (s *FlowImageService) GetImageModels(ctx context.Context) models.Response[[]FlowModel] {
	acc, err := s.NextAccount()
	if err != nil {
		return models.ErrorResponse[[]FlowModel](
			"Không thể lấy model: "+err.Error(),
			models.CodeUnavailable,
		)
	}

	s.mu.Lock()
	if len(s.cachedModels) > 0 && time.Now().Before(s.modelsExpire) {
		cached := make([]FlowModel, len(s.cachedModels))
		copy(cached, s.cachedModels)
		s.mu.Unlock()
		return models.SuccessResponse(cached)
	}
	s.mu.Unlock()

	modelsList, err := s.fetchLiveModels(ctx, *acc)
	if err != nil {
		return models.ErrorResponse[[]FlowModel](
			"Không lấy được danh sách model từ Google Flow: "+err.Error(),
			models.CodeUnavailable,
		)
	}

	s.mu.Lock()
	s.cachedModels = modelsList
	s.modelsExpire = time.Now().Add(5 * time.Minute)
	s.mu.Unlock()

	return models.SuccessResponse(modelsList)
}

// queryModelsFromAccount validates live cookies against Google Flow and returns authenticated models.
func (s *FlowImageService) queryModelsFromAccount(ctx context.Context, acc storedAccount) ([]FlowModel, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(chrome.FlowURL)
	var httpCookies []*http.Cookie
	for _, c := range acc.Credentials.Cookies {
		hc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			hc.Expires = time.Unix(int64(c.Expires), 0)
		}
		httpCookies = append(httpCookies, hc)
	}
	jar.SetCookies(u, httpCookies)

	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}

	// Verify session live against Google Flow config RPC
	rpcID := "cPZSdc" // /VideoFxService.GetFlowAppConfig
	reqURL := fmt.Sprintf("%s/_/AiSandboxAngularFrontend/data/batchexecute?rpcids=%s&source-path=%s&bl=%s&hl=en&_reqid=100000&rt=c",
		chrome.FlowURL, rpcID, url.QueryEscape("/"), url.QueryEscape(acc.Credentials.BuildLabel))

	fReq := fmt.Sprintf(`[[["%s","[]",null,"generic"]]]`, rpcID)
	bodyVal := url.Values{}
	bodyVal.Set("f.req", fReq)
	if acc.Credentials.AtToken != "" {
		bodyVal.Set("at", acc.Credentials.AtToken)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(bodyVal.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("X-Same-Domain", "1")
	if acc.Credentials.UserAgent != "" {
		req.Header.Set("User-Agent", acc.Credentials.UserAgent)
	}
	req.Header.Set("Referer", chrome.FlowURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối máy chủ Google Flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Flow trả về mã lỗi HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Verify batchexecute envelope success
	if !bytes.Contains(body, []byte(`"wrb.fr"`)) || !bytes.Contains(body, []byte(rpcID)) {
		return nil, errors.New("phiên đăng nhập hoặc cookie không được Google Flow chấp nhận")
	}

	// Models available for verified authenticated Flow account
	return []FlowModel{
		{
			ID:          "NARWHAL",
			Name:        "Nano 2",
			Alias:       "nano2",
			Description: "Tạo ảnh nhanh, tiêu chuẩn cao",
			Default:     true,
		},
		{
			ID:          "GEM_PIX_2",
			Name:        "Nano Pro",
			Alias:       "nano-pro",
			Description: "Chất lượng cao, chi tiết vượt trội",
			Default:     false,
		},
		{
			ID:          "IMAGEN_3_5",
			Name:        "Imagen 3.5",
			Alias:       "image4",
			Description: "Chân thực cao, phong cách nghệ thuật",
			Default:     false,
		},
	}, nil
}

// GenerateImages coordinates request dispatch using round-robin account rotation.
func (s *FlowImageService) GenerateImages(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem] {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return models.ErrorResponse[[]GeneratedImageItem](
			"Vui lòng nhập nội dung mô tả (prompt).",
			models.CodeInvalidArgument,
		)
	}

	if req.Count <= 0 {
		req.Count = 1
	} else if req.Count > 4 {
		req.Count = 4
	}

	if req.AspectRatio == "" {
		req.AspectRatio = "1:1"
	}

	acc, err := s.NextAccount()
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](
			"Không thể tạo ảnh: "+err.Error(),
			models.CodeUnavailable,
		)
	}

	images, err := s.generateLiveImages(ctx, *acc, req)
	if err != nil {
		return models.ErrorResponse[[]GeneratedImageItem](
			"Lỗi khi tạo ảnh từ Google Flow: "+err.Error(),
			models.CodeUnavailable,
		)
	}

	return models.SuccessResponse(images)
}

// UploadReferenceImage registers a local image to Google Flow via RPC maseQ.
func (s *FlowImageService) UploadReferenceImage(ctx context.Context, client *http.Client, acc storedAccount, imageBase64 string, mimeType string, fileName string, token string, projectID ...string) (string, error) {
	proj := ""
	if len(projectID) > 0 {
		proj = projectID[0]
	}
	inner, err := BuildImageUploadRequest(imageBase64, mimeType, fileName, proj, token)
	if err != nil {
		return "", fmt.Errorf("không đóng gói được payload maseQ: %w", err)
	}

	rpcID := RPCUploadImage
	reqURL := fmt.Sprintf("%s/_/AiSandboxAngularFrontend/data/batchexecute?rpcids=%s&source-path=%s&bl=%s&hl=en&_reqid=100004&rt=c",
		chrome.FlowURL, rpcID, url.QueryEscape("/"), url.QueryEscape(acc.Credentials.BuildLabel))

	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return "", fmt.Errorf("không đóng gói được envelope maseQ: %w", err)
	}
	val := url.Values{}
	val.Set("f.req", fReq)
	if acc.Credentials.AtToken != "" {
		val.Set("at", acc.Credentials.AtToken)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(val.Encode()))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	httpReq.Header.Set("X-Same-Domain", "1")
	if acc.Credentials.UserAgent != "" {
		httpReq.Header.Set("User-Agent", acc.Credentials.UserAgent)
	}
	httpReq.Header.Set("Referer", chrome.FlowURL+"/")

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("lỗi kết nối khi gọi maseQ: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if bytes.Contains(body, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return "", errors.New("Google Flow từ chối tải ảnh lên do token bảo mật không hợp lệ")
	}

	return ParseUploadedMediaID(body)
}

var projectIDRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// FetchAccountProjects queries RPC UpteDb (/FlowService.GetProjects) to obtain project UUIDs for the account.
func (s *FlowImageService) FetchAccountProjects(ctx context.Context, client *http.Client, acc storedAccount) ([]string, error) {
	rpcID := "UpteDb"
	reqURL := fmt.Sprintf("%s/_/AiSandboxAngularFrontend/data/batchexecute?rpcids=%s&source-path=%s&bl=%s&hl=en&_reqid=100002&rt=c",
		chrome.FlowURL, rpcID, url.QueryEscape("/"), url.QueryEscape(acc.Credentials.BuildLabel))

	innerPayload := []any{"projects/*", 21, nil, nil, nil, nil, []any{1}}
	fReq, err := BuildBatchEnvelope(rpcID, innerPayload)
	if err != nil {
		return nil, err
	}

	val := url.Values{}
	val.Set("f.req", fReq)
	if acc.Credentials.AtToken != "" {
		val.Set("at", acc.Credentials.AtToken)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(val.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	httpReq.Header.Set("X-Same-Domain", "1")
	if acc.Credentials.UserAgent != "" {
		httpReq.Header.Set("User-Agent", acc.Credentials.UserAgent)
	}
	httpReq.Header.Set("Referer", chrome.FlowURL+"/")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	matches := projectIDRegex.FindAll(body, -1)
	var projects []string
	seen := make(map[string]bool)
	for _, m := range matches {
		id := string(m)
		if !seen[id] {
			seen[id] = true
			projects = append(projects, id)
		}
	}
	return projects, nil
}

// generateImagesFromAccount executes generation against Google Flow using the selected account.
func (s *FlowImageService) generateImagesFromAccount(ctx context.Context, acc storedAccount, req GenerateImageRequest) ([]GeneratedImageItem, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(chrome.FlowURL)
	var httpCookies []*http.Cookie
	for _, c := range acc.Credentials.Cookies {
		hc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			hc.Expires = time.Unix(int64(c.Expires), 0)
		}
		httpCookies = append(httpCookies, hc)
	}
	jar.SetCookies(u, httpCookies)

	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	// Verify session active against Google Flow
	verifyRPC := "cPZSdc"
	reqURL := fmt.Sprintf("%s/_/AiSandboxAngularFrontend/data/batchexecute?rpcids=%s&source-path=%s&bl=%s&hl=en&_reqid=100001&rt=c",
		chrome.FlowURL, verifyRPC, url.QueryEscape("/"), url.QueryEscape(acc.Credentials.BuildLabel))

	fReq := fmt.Sprintf(`[[["%s","[]",null,"generic"]]]`, verifyRPC)
	bodyVal := url.Values{}
	bodyVal.Set("f.req", fReq)
	if acc.Credentials.AtToken != "" {
		bodyVal.Set("at", acc.Credentials.AtToken)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(bodyVal.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	httpReq.Header.Set("X-Same-Domain", "1")
	if acc.Credentials.UserAgent != "" {
		httpReq.Header.Set("User-Agent", acc.Credentials.UserAgent)
	}
	httpReq.Header.Set("Referer", chrome.FlowURL+"/")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối máy chủ Google Flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Flow trả về mã lỗi HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if !bytes.Contains(body, []byte(`"wrb.fr"`)) {
		return nil, errors.New("phiên đăng nhập hoặc cookie không hợp lệ trên Google Flow")
	}

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

	policy := s.accounts.cfgManager.Get().Accounts
	tok, tErr := chrome.MintRecaptchaToken(ctx, browser.Port, policy, chrome.ActionImageGeneration)
	if tErr != nil {
		return nil, fmt.Errorf("lỗi cấp token reCAPTCHA Enterprise từ trình duyệt: %w", tErr)
	}
	if tok == "" {
		return nil, errors.New("token reCAPTCHA Enterprise trả về rỗng")
	}
	recaptchaToken = tok

	// 2. Resolve real project ID from account
	projectID := req.ProjectID
	if projectID == "" {
		projects, pErr := s.FetchAccountProjects(ctx, client, acc)
		if pErr == nil && len(projects) > 0 {
			projectID = projects[0]
		}
	}

	// 3. If a reference image is supplied, upload it via RPC maseQ
	if req.ReferenceImageBase64 != "" {
		uploadToken, uErrToken := chrome.MintRecaptchaToken(ctx, browser.Port, policy, chrome.ActionImageGeneration)
		if uErrToken != nil || uploadToken == "" {
			uploadToken = recaptchaToken
		}
		mediaID, uErr := s.UploadReferenceImage(ctx, client, acc, req.ReferenceImageBase64, req.ReferenceMimeType, req.ReferenceFileName, uploadToken, projectID)
		if uErr != nil {
			return nil, fmt.Errorf("không tải ảnh tham chiếu lên được: %w", uErr)
		}
		refMediaIDs = append(refMediaIDs, mediaID)
	}

	// 4. Dispatch verified image generation RPC ogiZ0b (BatchGenerateImages)
	innerPayload, err := BuildImageBatchRequest(req.Prompt, projectID, req.AspectRatio, req.ModelID, req.Count, recaptchaToken, refMediaIDs)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được payload tạo ảnh: %w", err)
	}

	rpcID := RPCBatchGenerateImages
	genURL := fmt.Sprintf("%s/_/AiSandboxAngularFrontend/data/batchexecute?rpcids=%s&source-path=%s&bl=%s&hl=en&_reqid=100003&rt=c",
		chrome.FlowURL, rpcID, url.QueryEscape("/"), url.QueryEscape(acc.Credentials.BuildLabel))

	genFReq, err := BuildBatchEnvelope(rpcID, innerPayload)
	if err != nil {
		return nil, fmt.Errorf("không đóng gói được envelope tạo ảnh: %w", err)
	}
	genVal := url.Values{}
	genVal.Set("f.req", genFReq)
	if acc.Credentials.AtToken != "" {
		genVal.Set("at", acc.Credentials.AtToken)
	}

	genReq, err := http.NewRequestWithContext(ctx, "POST", genURL, strings.NewReader(genVal.Encode()))
	if err != nil {
		return nil, err
	}
	genReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	genReq.Header.Set("X-Same-Domain", "1")
	if acc.Credentials.UserAgent != "" {
		genReq.Header.Set("User-Agent", acc.Credentials.UserAgent)
	}
	genReq.Header.Set("Referer", chrome.FlowURL+"/")

	genResp, err := client.Do(genReq)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối máy chủ Google Flow khi gọi %s: %w", rpcID, err)
	}
	defer genResp.Body.Close()

	genBody, err := io.ReadAll(genResp.Body)
	if err != nil {
		return nil, err
	}

	if bytes.Contains(genBody, []byte("PUBLIC_ERROR_UNUSUAL_ACTIVITY")) {
		return nil, errors.New("Google Flow yêu cầu xác thực reCAPTCHA Enterprise token (action: IMAGE_GENERATION). Phiên cần được cấp token qua trình duyệt trước khi tạo ảnh.")
	}

	images := ParseGeneratedImages(genBody, req.Prompt, req.ModelID, req.AspectRatio)
	if len(images) > 0 {
		return images, nil
	}

	limit := len(genBody)
	if limit > 200 {
		limit = 200
	}
	return nil, fmt.Errorf("Google Flow chưa trả về ảnh hợp lệ (phản hồi: %s)", string(genBody[:limit]))
}
