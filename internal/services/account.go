package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/services/chrome"
	"github.com/google/uuid"
)

type AccountStatus string

const (
	AccountUnverified AccountStatus = "unverified"
	AccountSignedIn   AccountStatus = "signed_in"
	AccountConnected  AccountStatus = "connected"
	AccountExpired    AccountStatus = "expired"
	AccountError      AccountStatus = "error"
)

var AllAccountStatuses = []struct {
	Value  AccountStatus
	TSName string
}{
	{AccountUnverified, "Unverified"}, {AccountSignedIn, "SignedIn"}, {AccountConnected, "Connected"}, {AccountExpired, "Expired"}, {AccountError, "Error"},
}

// AccountProfile is a public view, never a credential container.
type AccountProfile struct {
	ID             string              `json:"id"`
	Email          string              `json:"email"`
	GaiaID         string              `json:"gaiaId"`
	Name           string              `json:"name"`
	ProfileDir     string              `json:"profileDir"`
	Status         AccountStatus       `json:"status"`
	Balance        *chrome.FlowCredits `json:"balance"`
	BalanceError   string              `json:"balanceError,omitempty"`
	CookieCount    int                 `json:"cookieCount"`
	LastVerifiedAt string              `json:"lastVerifiedAt,omitempty"`
	CreatedAt      string              `json:"createdAt"`
	UpdatedAt      string              `json:"updatedAt"`
}

type LoginProcessInfo struct {
	SessionID  string               `json:"sessionId"`
	Port       int                  `json:"port"`
	ProfileDir string               `json:"profileDir"`
	ExpiresAt  string               `json:"expiresAt"`
	Policy     config.AccountConfig `json:"policy"`
}

type LoginStatusInfo struct {
	IsConnected    bool            `json:"isConnected"`
	IsLoggedIn     bool            `json:"isLoggedIn"`
	Profile        *AccountProfile `json:"profile,omitempty"`
	BrowserVersion string          `json:"browserVersion,omitempty"`
	ActivePage     string          `json:"activePage,omitempty"`
	Message        string          `json:"message"`
}

type loginSession struct {
	id         string
	browser    *chrome.Browser
	ctx        context.Context
	cancel     context.CancelFunc
	expires    time.Time
	completed  *LoginStatusInfo
	nextCheck  time.Time
	lastStatus LoginStatusInfo
}

type AccountService struct {
	mu             sync.Mutex // storage, login lifecycle and cooldowns
	operation      sync.Mutex // no unbounded queue of remote/browser work
	paths          paths.Paths
	cfgManager     *config.Manager
	login          *loginSession
	browsers       map[string]*chrome.Browser
	nextRefresh    map[string]time.Time
	canceledLogins map[string]time.Time
	storeErr       error
	closed         bool
	// Adapter seams keep regression tests independent of real Google accounts.
	inspect         func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error)
	inspectIdentity func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error)
	query           func(context.Context, chrome.Credentials, config.AccountConfig) (chrome.FlowCredits, chrome.Credentials, error)
	start           func(context.Context, chrome.LaunchOptions, config.AccountConfig) (*chrome.Browser, error)
	validate        func(context.Context, *chrome.Browser, config.AccountConfig) error
}

func NewAccountService(p paths.Paths, cfg *config.Manager) *AccountService {
	return &AccountService{paths: p, cfgManager: cfg, browsers: make(map[string]*chrome.Browser), nextRefresh: make(map[string]time.Time), canceledLogins: make(map[string]time.Time), inspect: chrome.InspectFlowSession, inspectIdentity: chrome.InspectFlowIdentity, query: chrome.QueryCreditsDirectHTTP, start: chrome.StartBrowser, validate: func(ctx context.Context, b *chrome.Browser, p config.AccountConfig) error { return b.Validate(ctx, p) }}
}

func accountFailure[T any](err error) models.Response[T] {
	return models.ErrorResponse[T](err.Error(), models.CodeUnavailable)
}
func busy[T any]() models.Response[T] {
	return accountFailure[T](errors.New("Đang xử lý tài khoản. Vui lòng chờ thao tác hiện tại hoàn tất."))
}

func (s *AccountService) profilePath(path string) (string, error) {
	root := filepath.Join(s.paths.DataDir, "profiles")
	if path == "" {
		return "", errors.New("Tài khoản chưa có profile. Hãy thêm lại tài khoản để đăng nhập.")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("Profile nằm ngoài thư mục dữ liệu tài khoản.")
	}
	// Existing links/junctions must not redirect native browser writes outside
	// the managed profile root. Missing directories are created by Chrome.
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		resolvedRoot, e := filepath.EvalSymlinks(root)
		if e != nil {
			return "", e
		}
		r, e := filepath.Rel(resolvedRoot, resolved)
		if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
			return "", errors.New("Profile liên kết ra ngoài thư mục dữ liệu.")
		}
	}
	return abs, nil
}

func (s *AccountService) begin() bool {
	if !s.operation.TryLock() {
		return false
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		s.operation.Unlock()
		return false
	}
	return true
}

func (s *AccountService) browser(ctx context.Context, path string, headless bool) (*chrome.Browser, error) {
	path, err := s.profilePath(path)
	if err != nil {
		return nil, err
	}
	cfg := s.cfgManager.Get()
	s.mu.Lock()
	b := s.browsers[path]
	s.mu.Unlock()
	if b != nil {
		if s.validate(ctx, b, cfg.Accounts) == nil {
			if !headless && b.Headless {
				return nil, errors.New("Profile đang được làm mới. Hãy chờ thao tác hoàn tất.")
			}
			return b, nil
		}
		// Never launch a second browser for an owned profile before closing the
		// failed handle. This also prevents headless/visible profile lock races.
		b.Close(cfg.Accounts)
	}
	chromePath, err := chrome.FindChrome()
	if err != nil {
		return nil, err
	}
	port := cfg.CdpPort
	s.mu.Lock()
	for _, other := range s.browsers {
		if other.Port == port {
			port = 0
			break
		}
	}
	s.mu.Unlock()
	b, err = s.start(ctx, chrome.LaunchOptions{ChromePath: chromePath, ProfileDir: path, Port: port, TargetURL: chrome.FlowURL, Headless: headless}, cfg.Accounts)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.browsers[path] = b
	s.mu.Unlock()
	return b, nil
}

// LaunchLoginBrowser is idempotent for the caller's session ID (including React
// StrictMode). A different session cannot silently replace an active login.
func (s *AccountService) LaunchLoginBrowser(ctx context.Context, id string) models.Response[LoginProcessInfo] {
	if _, err := uuid.Parse(id); err != nil {
		return models.ErrorResponse[LoginProcessInfo]("Phiên đăng nhập không hợp lệ.", models.CodeInvalidArgument)
	}
	if !s.begin() {
		return busy[LoginProcessInfo]()
	}
	defer s.operation.Unlock()
	cfg := s.cfgManager.Get()
	s.mu.Lock()
	for token, until := range s.canceledLogins {
		if time.Now().After(until) {
			delete(s.canceledLogins, token)
		}
	}
	if _, canceled := s.canceledLogins[id]; canceled {
		s.mu.Unlock()
		return accountFailure[LoginProcessInfo](errors.New("Phiên đăng nhập đã đóng."))
	}
	if s.login != nil && s.login.id == id && s.login.browser != nil && s.login.ctx.Err() == nil {
		info := loginInfo(s.login, cfg.Accounts)
		s.mu.Unlock()
		return models.SuccessResponse(info)
	}
	if s.login != nil && s.login.ctx.Err() == nil {
		s.mu.Unlock()
		return busy[LoginProcessInfo]()
	}
	loginCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Accounts.LoginTimeoutSeconds)*time.Second)
	l := &loginSession{id: id, ctx: loginCtx, cancel: cancel, expires: time.Now().Add(time.Duration(cfg.Accounts.LoginTimeoutSeconds) * time.Second)}
	s.login = l
	s.mu.Unlock()
	context.AfterFunc(loginCtx, func() { s.CancelLogin(id) })
	profileDir := filepath.Join(s.paths.DataDir, "profiles", uuid.NewString())
	b, err := s.browser(loginCtx, profileDir, false)
	if err != nil {
		cancel()
		return accountFailure[LoginProcessInfo](err)
	}
	s.mu.Lock()
	l.browser = b
	s.mu.Unlock()
	if loginCtx.Err() != nil {
		b.Close(cfg.Accounts)
		return accountFailure[LoginProcessInfo](loginCtx.Err())
	}
	return models.SuccessResponse(loginInfo(l, cfg.Accounts))
}

func loginInfo(l *loginSession, p config.AccountConfig) LoginProcessInfo {
	return LoginProcessInfo{SessionID: l.id, Port: l.browser.Port, ProfileDir: l.browser.ProfileDir, ExpiresAt: l.expires.UTC().Format(time.RFC3339Nano), Policy: p}
}

func (s *AccountService) CancelLogin(id string) models.Response[bool] {
	if _, err := uuid.Parse(id); err != nil {
		return models.ErrorResponse[bool]("Phiên đăng nhập không hợp lệ.", models.CodeInvalidArgument)
	}
	s.mu.Lock()
	s.canceledLogins[id] = time.Now().Add(time.Duration(s.cfgManager.Get().Accounts.LoginTimeoutSeconds) * time.Second)
	l := s.login
	if l == nil || l.id != id {
		s.mu.Unlock()
		return models.SuccessResponse(true)
	}
	l.cancel()
	s.mu.Unlock()
	// Interrupt CDP before waiting for the operation, then release an unfinished
	// browser so repeatedly opening/closing the popup cannot leak Chrome processes.
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	b := l.browser
	unfinished := l.completed == nil
	s.mu.Unlock()
	if unfinished && b != nil {
		if err := s.deleteProfile(context.Background(), b.ProfileDir); err != nil {
			return models.ErrorResponse[bool](err.Error(), models.CodeInternal)
		}
	}
	return models.SuccessResponse(true)
}

func (s *AccountService) CheckLoginStatus(_ context.Context, id string) models.Response[LoginStatusInfo] {
	if !s.begin() {
		return busy[LoginStatusInfo]()
	}
	defer s.operation.Unlock()
	s.mu.Lock()
	l := s.login
	if l == nil || l.id != id {
		s.mu.Unlock()
		return accountFailure[LoginStatusInfo](errors.New("Không tìm thấy phiên đăng nhập."))
	}
	if l.completed != nil {
		status := *l.completed
		s.mu.Unlock()
		return models.SuccessResponse(status)
	}
	if err := l.ctx.Err(); err != nil {
		s.mu.Unlock()
		return accountFailure[LoginStatusInfo](errors.New("Phiên đăng nhập đã đóng hoặc hết thời gian chờ."))
	}
	if time.Now().Before(l.nextCheck) {
		status := l.lastStatus
		s.mu.Unlock()
		return models.SuccessResponse(status)
	}
	s.mu.Unlock()
	p := s.cfgManager.Get().Accounts
	if l.browser == nil {
		return busy[LoginStatusInfo]()
	}
	status := LoginStatusInfo{Message: "Đang chờ trình duyệt tải trang Flow."}
	if err := s.validate(l.ctx, l.browser, p); err != nil {
		status.Message = err.Error()
	} else {
		status.IsConnected = true
		status.BrowserVersion = l.browser.Version
		details, err := s.inspectIdentity(l.ctx, l.browser.Port, p)
		if err != nil {
			status.Message = err.Error()
			if s.rateLimited(err, "", l) {
				return accountFailure[LoginStatusInfo](err)
			}
		} else if details != nil {
			status.ActivePage = details.Title
			if details.IsLoggedIn {
				if err := l.ctx.Err(); err != nil {
					return accountFailure[LoginStatusInfo](err)
				}
				acc, err := s.persistSession(l.ctx, l.browser.ProfileDir, details, nil)
				if err != nil {
					return accountFailure[LoginStatusInfo](err)
				}
				status.IsLoggedIn = true
				status.Profile = &acc
				status.Message = "Đã nhận diện và lưu tài khoản Flow. Số dư được cập nhật riêng."
			} else {
				status.Message = "Vui lòng đăng nhập tài khoản trên trang Flow."
			}
		}
	}
	s.mu.Lock()
	l.lastStatus = status
	l.nextCheck = time.Now().Add(time.Duration(p.PollIntervalMS) * time.Millisecond)
	if status.IsLoggedIn {
		l.completed = &status
	}
	s.mu.Unlock()
	return models.SuccessResponse(status)
}

func sameIdentity(acc AccountProfile, session *chrome.FlowSessionDetails) bool {
	return strings.EqualFold(acc.Email, session.Email) && (acc.GaiaID == "" || acc.GaiaID == session.GaiaID)
}

func (s *AccountService) persistSession(ctx context.Context, path string, session *chrome.FlowSessionDetails, expected *AccountProfile) (AccountProfile, error) {
	if err := ctx.Err(); err != nil {
		return AccountProfile{}, err
	}
	address, err := mail.ParseAddress(session.Email)
	if err != nil || address.Address != session.Email || session.GaiaID == "" || !session.IsLoggedIn {
		return AccountProfile{}, errors.New("Không xác minh được danh tính phiên Flow.")
	}
	if expected != nil && !sameIdentity(*expected, session) {
		return AccountProfile{}, errors.New("Chrome đang đăng nhập tài khoản khác. Dữ liệu tài khoản được giữ nguyên.")
	}
	acc := AccountProfile{ID: strings.ToLower(session.Email), Email: session.Email, GaiaID: session.GaiaID, Name: session.Email, ProfileDir: path, Status: AccountSignedIn, CookieCount: len(session.Credentials.Cookies)}
	if session.CreditsVerified {
		acc.Balance = &session.Credits
		acc.Status = AccountConnected
		acc.LastVerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if session.CreditsErr != nil {
		acc.BalanceError = session.CreditsErr.Error()
	}
	if expected != nil {
		acc.ID = expected.ID
		acc.CreatedAt = expected.CreatedAt
	}
	return s.saveAccount(storedAccount{AccountProfile: acc, Credentials: session.Credentials}, expected != nil)
}

func (s *AccountService) OpenAccountBrowser(ctx context.Context, id string) models.Response[LoginProcessInfo] {
	if !s.begin() {
		return busy[LoginProcessInfo]()
	}
	defer s.operation.Unlock()
	acc, err := s.findAccount(id)
	if err != nil {
		return accountFailure[LoginProcessInfo](err)
	}
	b, err := s.browser(ctx, acc.ProfileDir, false)
	if err != nil {
		return accountFailure[LoginProcessInfo](err)
	}
	return models.SuccessResponse(LoginProcessInfo{Port: b.Port, ProfileDir: b.ProfileDir, Policy: s.cfgManager.Get().Accounts})
}

func (s *AccountService) RefreshAccount(ctx context.Context, id string) models.Response[AccountProfile] {
	if !s.begin() {
		return busy[AccountProfile]()
	}
	defer s.operation.Unlock()
	acc, err := s.findAccount(id)
	if err != nil {
		return accountFailure[AccountProfile](err)
	}
	p := s.cfgManager.Get().Accounts
	s.mu.Lock()
	next := s.nextRefresh[acc.ID]
	if time.Now().Before(next) {
		s.mu.Unlock()
		return accountFailure[AccountProfile](fmt.Errorf("Vui lòng chờ %s trước khi làm mới lại.", time.Until(next).Round(time.Second)))
	}
	s.nextRefresh[acc.ID] = time.Now().Add(time.Duration(p.RefreshCooldownSeconds) * time.Second)
	s.mu.Unlock()
	// One budget covers HTTP, browser startup and page readiness; nested attempts
	// cannot each restart the full timeout window.
	ctx, cancelOperation := context.WithTimeout(ctx, time.Duration(p.RequestTimeoutSeconds+p.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancelOperation()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		next := time.Now().Add(time.Duration(p.RefreshCooldownSeconds) * time.Second)
		if next.After(s.nextRefresh[acc.ID]) {
			s.nextRefresh[acc.ID] = next
		}
	}()
	// Persisted credentials are accepted only after migration/identity validation.
	if acc.LastVerifiedAt != "" {
		credits, credentials, queryErr := s.query(ctx, acc.Credentials, p)
		if queryErr == nil {
			acc.Balance = &credits
			acc.Credentials = credentials
			acc.CookieCount = len(credentials.Cookies)
			acc.Status = AccountConnected
			acc.BalanceError = ""
			acc.LastVerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
			result, saveErr := s.saveAccount(acc, true)
			if saveErr != nil {
				return accountFailure[AccountProfile](saveErr)
			}
			return models.SuccessResponse(result)
		}
		if s.rateLimited(queryErr, acc.ID, nil) {
			return accountFailure[AccountProfile](queryErr)
		}
	}
	b, err := s.browser(ctx, acc.ProfileDir, true)
	if err != nil {
		return accountFailure[AccountProfile](err)
	}
	if b.Headless {
		defer func() { b.Close(p); s.mu.Lock(); delete(s.browsers, b.ProfileDir); s.mu.Unlock() }()
	}
	loadCtx, cancel := context.WithTimeout(ctx, time.Duration(p.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Duration(p.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		if err = s.validate(loadCtx, b, p); err == nil {
			var session *chrome.FlowSessionDetails
			session, err = s.inspect(loadCtx, b.Port, p)
			if err == nil && session != nil && session.IsLoggedIn {
				result, saveErr := s.persistSession(loadCtx, acc.ProfileDir, session, &acc.AccountProfile)
				if saveErr != nil {
					return accountFailure[AccountProfile](saveErr)
				}
				if session.CreditsErr != nil {
					s.rateLimited(session.CreditsErr, acc.ID, nil)
				}
				if !session.CreditsVerified {
					err := session.CreditsErr
					if err == nil {
						err = errors.New("Đã đăng nhập nhưng chưa lấy được số dư Flow.")
					}
					return accountFailure[AccountProfile](err)
				}
				return models.SuccessResponse(result)
			}
			if s.rateLimited(err, acc.ID, nil) {
				return accountFailure[AccountProfile](err)
			}
			if err == nil {
				err = chrome.ErrLoginRequired
			}
		}
		// Only page bootstrap/login waiting is retried. A failed RPC gets one
		// attempt per refresh; repeated server failures must not launch a storm.
		if err != nil && !errors.Is(err, chrome.ErrPageNotReady) && !errors.Is(err, chrome.ErrLoginRequired) {
			break
		}
		// An already loaded visible browser needs only one inspection; a newly
		// launched headless profile gets a bounded, cancellation-aware load wait.
		if !b.Headless {
			break
		}
		select {
		case <-loadCtx.Done():
			if err == nil {
				err = loadCtx.Err()
			}
		case <-ticker.C:
			continue
		}
		break
	}
	acc.Status = AccountError
	if err != nil {
		acc.BalanceError = err.Error()
	}
	var remote *chrome.HTTPError
	if errors.Is(err, chrome.ErrLoginRequired) || (errors.As(err, &remote) && remote.Status == http.StatusUnauthorized) {
		acc.Status = AccountExpired
	}
	if _, saveErr := s.saveAccount(acc, true); saveErr != nil {
		return accountFailure[AccountProfile](saveErr)
	}
	return accountFailure[AccountProfile](fmt.Errorf("Không làm mới được phiên Flow. Hãy mở Chrome để kiểm tra đăng nhập: %w", err))
}

func (s *AccountService) rateLimited(err error, id string, l *loginSession) bool {
	var remote *chrome.HTTPError
	if !errors.As(err, &remote) || (remote.Status != http.StatusTooManyRequests && remote.Status != http.StatusServiceUnavailable) {
		return false
	}
	p := s.cfgManager.Get().Accounts
	delay := max(remote.RetryAfter, time.Duration(p.RefreshCooldownSeconds)*time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if l != nil {
		l.nextCheck = time.Now().Add(delay)
		l.lastStatus = LoginStatusInfo{Message: err.Error()}
	}
	if id != "" {
		s.nextRefresh[id] = time.Now().Add(delay)
	}
	return true
}

// EnsureAccountHeadlessBrowser returns an active browser instance for the stored account,
// starting a background headless Chrome instance if not already running.
func (s *AccountService) EnsureAccountHeadlessBrowser(ctx context.Context, acc storedAccount) (*chrome.Browser, error) {
	return s.browser(ctx, acc.ProfileDir, true)
}

// Close cancels in-flight login work before waiting for the bounded operation.
func (s *AccountService) Close() {
	s.mu.Lock()
	s.closed = true
	if s.login != nil {
		s.login.cancel()
	}
	s.mu.Unlock()
	s.operation.Lock()
	defer s.operation.Unlock()
	s.mu.Lock()
	browsers := s.browsers
	s.browsers = make(map[string]*chrome.Browser)
	s.mu.Unlock()
	for _, b := range browsers {
		b.Close(s.cfgManager.Get().Accounts)
	}
}
