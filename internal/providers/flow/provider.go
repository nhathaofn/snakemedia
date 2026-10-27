package flow

import (
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
)

// Accounts is what the provider needs from the Google account sessions
// (session.Service in the app, an in-memory fake in tests).
type Accounts interface {
	GetConnectedStoredAccounts() ([]session.StoredAccount, error)
	AcquireProfile(profileDir string) (release func())
	EnsureAccountHeadlessBrowser(ctx context.Context, acc session.StoredAccount) (*browser.Browser, error)
	ProxyURL(ctx context.Context, acc session.StoredAccount) (*url.URL, error)
	SyncSession(ctx context.Context, acc session.StoredAccount) (session.StoredAccount, error)
	UpdateCredentials(id string, c chrome.Credentials) error
	UpdateBalance(id string, balance chrome.FlowCredits, c chrome.Credentials) error
}

// Provider is the Google Flow provider: account rotation, per-account caches,
// credential upkeep and the image/video capabilities (image.go, video.go) that
// engine nodes expose.
type Provider struct {
	accounts Accounts
	cfg      *config.Manager
	media    *media.Store
	// jobLocks holds the request IDs a call is working on (tryJobLock).
	jobLocksMu sync.Mutex
	jobLocks   map[string]struct{}
	journals   journalCache
	// queue runs the image batch queue (Ảnh → All); see image_queue.go.
	queue               *imageQueue
	balanceRefreshLocks sync.Map
	projectResolveLocks sync.Map // account ID -> serialises resolveProject
	pageLocks           sync.Map // browser port -> *sync.RWMutex (pageLock)
	// undiscoverableBuilds remembers Flow builds whose project page lacked
	// the BatchGenerateImages descriptor.
	undiscoverableBuilds map[string]time.Time
	mu                   sync.Mutex
	rotationMu           sync.Mutex
	rotationAccounts     map[string]*rotationAccountState
	videoCurrent         map[string]float64
	imageCursor          uint64
	rotationChanged      chan struct{}
	// Both caches are keyed by account ID: catalogue defaults depend on the
	// account tier, and projects belong to one account.
	models               map[string]modelCache
	catalogFlights       flightGroup
	projects             map[string]string
	dailyImageProjects   map[dailyImageProjectKey]string
	generatedImageCounts map[string]int
	reservedImageCounts  map[string]int
	imageProjectLocks    map[dailyImageProjectKey]*sync.Mutex
	productName          string
	videoModels          map[string]videoModelCache
	fetchModelsPayload   func(ctx context.Context, acc session.StoredAccount) (string, error)
	refreshLiveCredits   func(ctx context.Context, id string)
	// syncSession refreshes an account's session from its Chrome profile.
	syncSession        func(ctx context.Context, acc session.StoredAccount) (session.StoredAccount, error)
	generateLiveVideos func(ctx context.Context, acc session.StoredAccount, req GenerateVideoRequest, variant string) ([]GeneratedVideoItem, error)
	uploadLiveVideo    func(ctx context.Context, acc session.StoredAccount, source localVideo) (sourceRecord, error)
	editLiveVideos     func(ctx context.Context, acc session.StoredAccount, req EditVideoRequest, source localVideo, family videoFamily) ([]GeneratedVideoItem, error)
	fetchLiveModels    func(ctx context.Context, acc session.StoredAccount) ([]FlowModel, error)
	generateLiveImages func(ctx context.Context, acc session.StoredAccount, req GenerateImageRequest) ([]GeneratedImageItem, error)
}

// New wires the provider to the account sessions, settings and media store.
func New(accounts Accounts, cfg *config.Manager, store *media.Store, productNames ...string) *Provider {
	productName := ""
	if len(productNames) > 0 {
		productName = strings.TrimSpace(productNames[0])
	}
	s := &Provider{
		accounts:             accounts,
		cfg:                  cfg,
		media:                store,
		productName:          productName,
		models:               make(map[string]modelCache),
		projects:             make(map[string]string),
		dailyImageProjects:   make(map[dailyImageProjectKey]string),
		generatedImageCounts: make(map[string]int),
		reservedImageCounts:  make(map[string]int),
		imageProjectLocks:    make(map[dailyImageProjectKey]*sync.Mutex),
		videoModels:          make(map[string]videoModelCache),
	}
	s.queue = newImageQueue(s)
	s.fetchLiveModels = s.queryModelsFromAccount
	s.generateLiveImages = s.generateImagesFromAccount
	s.fetchModelsPayload = s.fetchModelsPayloadFromAccount
	// The balance query must not delay the generation reply; its activity epoch
	// check already drops a result made stale by a newer request.
	s.refreshLiveCredits = func(ctx context.Context, id string) {
		go s.refreshAccountCredits(context.WithoutCancel(ctx), id)
	}
	s.generateLiveVideos = s.generateVideosFromAccount
	s.uploadLiveVideo = s.uploadVideoFromAccount
	s.editLiveVideos = s.editVideosFromAccount
	if accounts != nil {
		s.syncSession = accounts.SyncSession
	}
	return s
}

// readyAccount reserves the least-busy compatible account, holds its profile
// and brings its Flow session up to date from Chrome. Signed-out accounts are
// skipped so generation can continue while another account remains connected.
func (s *Provider) readyAccount(ctx context.Context) (*session.StoredAccount, func(), error) {
	valid, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, nil, err
	}
	return s.readyAccountFrom(ctx, valid)
}

func (s *Provider) readyAccountFrom(ctx context.Context, valid []session.StoredAccount) (*session.StoredAccount, func(), error) {
	_, synced, _, release, err := readyCandidate(ctx, s, valid, sameAccount, s.leastBusy)
	if err != nil {
		return nil, nil, err
	}
	return &synced, release, nil
}

func removeAccount(accounts []session.StoredAccount, id string) []session.StoredAccount {
	return slices.DeleteFunc(accounts, func(acc session.StoredAccount) bool { return acc.ID == id })
}

// isUnauthorized reports a Flow HTTP 401, i.e. a stale stored session.
func isUnauthorized(err error) bool {
	var remote *chrome.HTTPError
	return errors.As(err, &remote) && remote.Status == http.StatusUnauthorized
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
func (s *Provider) GetValidConnectedAccounts() ([]session.StoredAccount, error) {
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được khởi tạo")
	}
	connected, err := s.accounts.GetConnectedStoredAccounts()
	if err != nil {
		return nil, fmt.Errorf("không đọc được danh sách tài khoản: %w", err)
	}

	var valid []session.StoredAccount
	for _, acc := range connected {
		if err := ValidateAccountCookies(acc.Credentials.Cookies); err == nil {
			valid = append(valid, acc)
		}
	}
	return valid, nil
}

// catalogAccount picks the account whose model lists the screens show: the
// first valid one. Reading metadata never reserves or rotates accounts.
func (s *Provider) catalogAccount() (*session.StoredAccount, error) {
	valid, err := s.GetValidConnectedAccounts()
	if err != nil {
		return nil, err
	}
	if len(valid) == 0 {
		return nil, errNoFlowAccount
	}
	return &valid[0], nil
}

// GetActiveRotationCount returns the number of valid accounts currently active in rotation.
func (s *Provider) GetActiveRotationCount(_ context.Context) models.Response[int] {
	valid, err := s.GetValidConnectedAccounts()
	if err != nil {
		return models.ErrorResponse[int](err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(len(valid))
}

// keepCredentials persists cookies Flow rotated during this account's requests.
func (s *Provider) keepCredentials(acc session.StoredAccount, client *chrome.FlowClient) {
	if err := s.accounts.UpdateCredentials(acc.ID, client.Credentials()); err != nil {
		logger.Get().Warn("cannot persist rotated Flow cookies", "account", acc.ID, "error", err)
	}
}

func accountTier(acc session.StoredAccount) int {
	if acc.Balance != nil {
		return acc.Balance.TierCode
	}
	return -1
}

// StudioConfig holds the studio choices and limits that GetModels does not
// carry (see limits.go), so the frontend renders them instead of keeping its
// own copies. Per-model choices come with the model lists.
type StudioConfig struct {
	VideoExtensions    []string `json:"videoExtensions"`
	ImageAspectRatios  []string `json:"imageAspectRatios"`
	DefaultImageRatio  string   `json:"defaultImageRatio"`
	DefaultBatchRatio  string   `json:"defaultBatchRatio"`
	MaxImages          int      `json:"maxImages"`
	MaxVideos          int      `json:"maxVideos"`
	MaxReferenceBytes  int      `json:"maxReferenceBytes"`
	ReferenceMimeTypes []string `json:"referenceMimeTypes"`
}

// GetStudioConfig returns the studio choices and limits enforced by the backend.
func (s *Provider) GetStudioConfig(_ context.Context) models.Response[StudioConfig] {
	return models.SuccessResponse(StudioConfig{
		VideoExtensions:    s.SourceVideoExtensions(),
		ImageAspectRatios:  imageAspectRatios(),
		DefaultImageRatio:  defaultImageRatio(),
		DefaultBatchRatio:  defaultBatchRatio(),
		MaxImages:          maxImagesPerRequest(),
		MaxVideos:          maxVideosPerRequest(),
		MaxReferenceBytes:  maxReferenceImageBytes(),
		ReferenceMimeTypes: referenceMimeTypes(),
	})
}
