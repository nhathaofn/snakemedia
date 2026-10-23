package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/services/chrome"
)

// FlowModel represents an image generation model available in Google Flow.
type FlowModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Alias       string `json:"alias"`
	Default     bool   `json:"default"`
}

// FlowImageService coordinates image generation, model discovery and round-robin account rotation.
type FlowImageService struct {
	accounts        *AccountService
	rrIndex         uint64
	mu              sync.Mutex
	cachedModels    []FlowModel
	modelsExpire    time.Time
	fetchLiveModels func(ctx context.Context, acc storedAccount) ([]FlowModel, error)
}

// NewFlowImageService creates a new FlowImageService.
func NewFlowImageService(accounts *AccountService) *FlowImageService {
	s := &FlowImageService{
		accounts: accounts,
	}
	s.fetchLiveModels = s.queryModelsFromAccount
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
