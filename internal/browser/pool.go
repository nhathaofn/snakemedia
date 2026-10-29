package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"github.com/google/uuid"
)

// ErrProfileBusy refuses to restart a browser that running work depends on.
var ErrProfileBusy = errors.New("Tài khoản đang chạy tác vụ. Hãy chờ xong rồi thử lại.")

// errPoolClosed refuses new browsers once the app is shutting down.
var errPoolClosed = errors.New("Ứng dụng đang thoát.")

// reapInterval is how often idle browsers are looked for.
const reapInterval = time.Minute

// Want describes the browser a caller needs for a profile.
type Want struct {
	// Visible asks for a window (sign-in, inspection by the user). Otherwise any
	// running browser will do, and a new one is headless when the settings say so.
	Visible bool
	// Route names the network route (proxy) the browser must use. A browser
	// running on another route is restarted, so a changed proxy can never leave
	// Chrome on the old network or on the real IP.
	Route string
	// Args are extra Chrome flags for a new launch (proxy, privacy).
	Args []string
}

// Pool owns at most one Chrome per profile directory under its root.
type Pool struct {
	mu       sync.Mutex
	cfg      *config.Manager
	root     string
	browsers map[string]*Browser
	routes   map[string]string
	inUse    map[string]int
	launches map[string]*launchLock
	// lastUsed is when work last got or held each browser; idle headless
	// browsers are closed after AccountConfig.IdleBrowserMinutes.
	lastUsed map[string]time.Time
	closed   bool
	onClosed func(profileDir string)
	// Seams for tests.
	now        func() time.Time
	start      func(context.Context, LaunchOptions, config.AccountConfig) (*Browser, error)
	findChrome func() (string, error)
	validate   func(context.Context, *Browser, config.AccountConfig) error
	shutdown   func(*Browser, config.AccountConfig) error
}

// NewPool manages profiles under root.
func NewPool(cfg *config.Manager, root string) *Pool {
	return &Pool{
		cfg: cfg, root: root,
		browsers: make(map[string]*Browser), routes: make(map[string]string),
		inUse: make(map[string]int), launches: make(map[string]*launchLock),
		lastUsed: make(map[string]time.Time), now: time.Now,
		start: StartBrowser, findChrome: FindChrome,
		validate: func(ctx context.Context, b *Browser, p config.AccountConfig) error { return b.Validate(ctx, p) },
		shutdown: (*Browser).Close,
	}
}

// NewProfile returns a fresh profile directory under the root. Chrome creates it.
func (p *Pool) NewProfile() string { return filepath.Join(p.root, uuid.NewString()) }

// launchLock serialises launches and removal of one profile. refs counts the
// callers holding or waiting for it, so a removed profile's entry is dropped
// only once nobody can still be using it.
type launchLock struct {
	mu   sync.Mutex
	refs int
	drop bool // the profile was removed: forget the entry when unused
}

func (p *Pool) lockLaunch(path string) func() {
	p.mu.Lock()
	l := p.launches[path]
	if l == nil {
		l = &launchLock{}
		p.launches[path] = l
	}
	l.refs++
	p.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		p.mu.Lock()
		defer p.mu.Unlock()
		l.refs--
		if l.refs == 0 && l.drop && p.browsers[path] == nil && p.launches[path] == l {
			delete(p.launches, path)
		}
	}
}

// Get returns the profile's running browser when it matches want, otherwise
// (re)launches it. A browser that work holds (Acquire) is never restarted.
func (p *Pool) Get(ctx context.Context, profileDir string, want Want) (*Browser, error) {
	path, err := p.dir(profileDir)
	if err != nil {
		return nil, err
	}
	unlock := p.lockLaunch(path)
	defer unlock()
	cfg := p.cfg.Get()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errPoolClosed
	}
	b := p.browsers[path]
	sameRoute := p.routes[path] == want.Route
	busy := p.inUse[path] > 0
	p.lastUsed[path] = p.now()
	p.mu.Unlock()
	profile := filepath.Base(path)
	if b != nil {
		valid := p.validate(ctx, b, cfg.Accounts) == nil
		if valid && sameRoute && (!want.Visible || !b.Headless) {
			return b, nil
		}
		if valid && busy {
			return nil, ErrProfileBusy
		}
		logger.Get().Info("restarting Chrome", "profile", profile, "alive", valid, "routeChanged", !sameRoute, "windowWanted", want.Visible && b.Headless)
		p.forget(path, b)
		_ = p.shutdown(b, cfg.Accounts)
	}
	chromePath, err := p.findChrome()
	if err != nil {
		return nil, errors.New("Không tìm thấy Google Chrome. Hãy cài Chrome rồi mở lại app.")
	}
	opts := LaunchOptions{
		ChromePath: chromePath,
		ProfileDir: path,
		TargetURL:  "about:blank",
		Headless:   !want.Visible && cfg.Accounts.Headless,
		ExtraArgs:  want.Args,
	}
	b, err = p.start(ctx, opts, cfg.Accounts)
	if err != nil {
		logger.Get().Warn("Chrome did not start", "profile", profile, "error", err)
		return nil, err
	}
	if b.Adopted() || (want.Visible && b.Headless) {
		// StartBrowser attached to a browser an earlier run left behind: its
		// flags (proxy, headless) are unknown, so it is restarted as wanted.
		logger.Get().Info("restarting Chrome left by an earlier run", "profile", profile)
		_ = p.shutdown(b, cfg.Accounts)
		if b, err = p.start(ctx, opts, cfg.Accounts); err != nil {
			return nil, err
		}
		if b.Adopted() || (want.Visible && b.Headless) {
			// The old Chrome did not exit: its proxy flags are unknown, so it
			// must not be used (it may go out on the real IP).
			_ = p.shutdown(b, cfg.Accounts)
			return nil, errors.New("Chrome cũ của tài khoản chưa tắt được; hãy đóng cửa sổ Chrome của tài khoản rồi thử lại.")
		}
	}
	logger.Get().Info("Chrome started", "profile", profile, "headless", b.Headless, "port", b.Port)
	p.mu.Lock()
	p.browsers[path] = b
	p.routes[path] = want.Route
	p.lastUsed[path] = p.now()
	p.mu.Unlock()
	return b, nil
}

// Acquire marks the profile's browser as needed by running work until the
// returned release is called; Get then refuses to restart it.
func (p *Pool) Acquire(profileDir string) (release func()) {
	path, err := p.dir(profileDir)
	if err != nil {
		return func() {}
	}
	p.mu.Lock()
	p.inUse[path]++
	p.lastUsed[path] = p.now()
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			if p.inUse[path]--; p.inUse[path] <= 0 {
				delete(p.inUse, path)
			}
			p.lastUsed[path] = p.now()
			p.mu.Unlock()
		})
	}
}

// OnClosed registers fn to run after the pool closed an idle browser on its
// own, so the owner can drop what it keeps for that browser (its route).
func (p *Pool) OnClosed(fn func(profileDir string)) {
	p.mu.Lock()
	p.onClosed = fn
	p.mu.Unlock()
}

// Reap closes idle headless browsers every reapInterval until ctx ends.
// Rotating over many accounts would otherwise keep one Chrome per account.
func (p *Pool) Reap(ctx context.Context) {
	go func() {
		t := time.NewTicker(reapInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.safeReap()
			}
		}
	}()
}

// safeReap runs one reap; a panic is logged and the next tick tries again.
func (p *Pool) safeReap() {
	defer logger.Recover("đóng Chrome rảnh")
	p.reapIdle()
}

// reapIdle closes every headless browser that no work holds and nothing has
// used for IdleBrowserMinutes. A window the user opened is never closed.
func (p *Pool) reapIdle() int {
	idle := time.Duration(p.cfg.Get().Accounts.IdleBrowserMinutes) * time.Minute
	if idle <= 0 {
		return 0
	}
	p.mu.Lock()
	var candidates []string
	for path, b := range p.browsers {
		if b.Headless && p.inUse[path] == 0 && p.now().Sub(p.lastUsed[path]) >= idle {
			candidates = append(candidates, path)
		}
	}
	p.mu.Unlock()
	closed := 0
	for _, path := range candidates {
		unlock := p.lockLaunch(path)
		p.mu.Lock()
		b := p.browsers[path]
		still := b != nil && b.Headless && p.inUse[path] == 0 && p.now().Sub(p.lastUsed[path]) >= idle
		onClosed := p.onClosed
		p.mu.Unlock()
		if still && p.closeIdleLocked(path) == nil {
			closed++
			logger.Get().Info("closed idle Chrome", "profile", filepath.Base(path), "idle", idle)
			if onClosed != nil {
				onClosed(path)
			}
		}
		unlock()
	}
	return closed
}

// InUse reports whether running work holds the profile's browser.
func (p *Pool) InUse(profileDir string) bool {
	path, err := p.dir(profileDir)
	if err != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inUse[path] > 0
}

// Running returns the profile's browser without launching one.
func (p *Pool) Running(profileDir string) *Browser {
	path, err := p.dir(profileDir)
	if err != nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.browsers[path]
}

func (p *Pool) forget(path string, b *Browser) {
	p.mu.Lock()
	if p.browsers[path] == b {
		delete(p.browsers, path)
		delete(p.routes, path)
	}
	p.mu.Unlock()
}

// CloseIfIdle shuts the profile's browser down gracefully so Chrome flushes
// cookies. It refuses with ErrProfileBusy while work holds the browser: the
// check and the close happen under the launch lock, so work that acquired the
// browser a moment earlier is never killed.
func (p *Pool) CloseIfIdle(profileDir string) error {
	path, err := p.dir(profileDir)
	if err != nil {
		return err
	}
	unlock := p.lockLaunch(path)
	defer unlock()
	return p.closeIdleLocked(path)
}

// closeIdleLocked is CloseIfIdle for a caller holding the profile's launch lock.
func (p *Pool) closeIdleLocked(path string) error {
	p.mu.Lock()
	if p.inUse[path] > 0 {
		p.mu.Unlock()
		return ErrProfileBusy
	}
	b := p.browsers[path]
	delete(p.browsers, path)
	delete(p.routes, path)
	p.mu.Unlock()
	policy := p.cfg.Get().Accounts
	if b == nil {
		// A browser left running by an earlier run still holds the profile.
		var err error
		if b, err = ExistingBrowser(context.Background(), path, policy); err != nil || b == nil {
			return err
		}
	}
	return p.shutdown(b, policy)
}

// CloseAll shuts every browser down in parallel; used on application exit.
// No browser starts afterwards, and one still launching is waited for (its
// launch lock) and closed too.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	p.closed = true
	paths := make([]string, 0, len(p.launches))
	for path := range p.launches {
		paths = append(paths, path)
	}
	p.mu.Unlock()
	policy := p.cfg.Get().Accounts
	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logger.Recover("đóng Chrome")
			unlock := p.lockLaunch(path)
			defer unlock()
			p.mu.Lock()
			b := p.browsers[path]
			delete(p.browsers, path)
			delete(p.routes, path)
			p.mu.Unlock()
			if b != nil {
				_ = p.shutdown(b, policy)
			}
		}()
	}
	wg.Wait()
}

// RemoveIfIdle deletes a profile directory (cookies, cache, login data) after
// its browser has closed, refusing with ErrProfileBusy while work holds it.
// The launch lock is held throughout so no browser starts on the profile
// while it is deleted. Anything outside the root or reached through a link is
// refused.
func (p *Pool) RemoveIfIdle(profileDir string) error {
	path, err := p.dir(profileDir)
	if err != nil {
		return err
	}
	unlock := p.lockLaunch(path)
	defer unlock()
	if err := p.closeIdleLocked(path); err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("Không xóa được thư mục profile: %w", err)
	}
	p.mu.Lock()
	if l := p.launches[path]; l != nil {
		l.drop = true
	}
	p.mu.Unlock()
	return nil
}

// dir returns the absolute path of profileDir when it lies strictly inside the
// root and no existing component is a link or a file, so native browser writes
// can never be redirected outside the managed root.
func (p *Pool) dir(profileDir string) (string, error) {
	if profileDir == "" {
		return "", errors.New("Tài khoản chưa có profile.")
	}
	rootAbs, err := filepath.Abs(p.root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(profileDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("Profile nằm ngoài thư mục quản lý.")
	}
	current := rootAbs
	for _, part := range append([]string{""}, strings.Split(rel, string(os.PathSeparator))...) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("Profile đi qua liên kết hoặc không phải thư mục.")
		}
	}
	return target, nil
}
