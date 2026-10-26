package muse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers/flow/chrome"
)

// browserPool owns at most one Chrome per account profile.
type browserPool struct {
	mu       sync.Mutex
	cfg      *config.Manager
	root     string // managed profiles root
	browsers map[string]*chrome.Browser
	// routes records which proxy each running browser was launched with.
	routes   map[string]string
	launches map[string]*sync.Mutex
	// Seams for tests.
	start      func(context.Context, chrome.LaunchOptions, config.AccountConfig) (*chrome.Browser, error)
	findChrome func() (string, error)
	validate   func(context.Context, *chrome.Browser, config.AccountConfig) error
	adopted    func(*chrome.Browser) bool
	shutdown   func(*chrome.Browser, config.AccountConfig) error
}

func newBrowserPool(cfg *config.Manager, root string) *browserPool {
	return &browserPool{
		cfg: cfg, root: root,
		browsers: make(map[string]*chrome.Browser), routes: make(map[string]string), launches: make(map[string]*sync.Mutex),
		start: chrome.StartBrowser, findChrome: chrome.FindChrome,
		validate: func(ctx context.Context, b *chrome.Browser, p config.AccountConfig) error { return b.Validate(ctx, p) },
		adopted:  (*chrome.Browser).Adopted,
		shutdown: (*chrome.Browser).Close,
	}
}

func (p *browserPool) lockLaunch(path string) func() {
	p.mu.Lock()
	m := p.launches[path]
	if m == nil {
		m = &sync.Mutex{}
		p.launches[path] = m
	}
	p.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// get returns the account's running browser or launches one. visible asks for
// a window (sign-in); otherwise any running browser will do and a new one is
// headless when the settings say so. A headless browser is replaced by a
// visible one, so callers must not ask for a window while jobs use it.
// route names the proxy the browser must use; a browser running with another
// route is restarted, so a changed proxy can never leave Chrome on the old
// network (or on the real IP).
func (p *browserPool) get(ctx context.Context, profileDir string, visible bool, route string, extraArgs ...string) (*chrome.Browser, error) {
	path, err := managedDir(p.root, profileDir)
	if err != nil {
		return nil, err
	}
	unlock := p.lockLaunch(path)
	defer unlock()
	cfg := p.cfg.Get()
	p.mu.Lock()
	b := p.browsers[path]
	sameRoute := p.routes[path] == route
	p.mu.Unlock()
	if b != nil {
		if sameRoute && p.validate(ctx, b, cfg.Accounts) == nil && (!visible || !b.Headless) {
			return b, nil
		}
		p.forget(path, b)
		_ = p.shutdown(b, cfg.Accounts)
	}
	chromePath, err := p.findChrome()
	if err != nil {
		return nil, errors.New("Không tìm thấy Google Chrome. Hãy cài Chrome rồi mở lại app.")
	}
	opts := chrome.LaunchOptions{
		ChromePath: chromePath,
		ProfileDir: path,
		TargetURL:  "about:blank",
		Headless:   !visible && cfg.Muse.Headless,
		ExtraArgs:  extraArgs,
	}
	if visible {
		opts.TargetURL = MuseURL
	}
	b, err = p.start(ctx, opts, cfg.Accounts)
	if err != nil {
		return nil, err
	}
	if p.adopted(b) || (visible && b.Headless) {
		// StartBrowser adopted a browser left by an earlier run: its proxy
		// flags are unknown, so it is restarted with this route.
		_ = p.shutdown(b, cfg.Accounts)
		if b, err = p.start(ctx, opts, cfg.Accounts); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	p.browsers[path] = b
	p.routes[path] = route
	p.mu.Unlock()
	return b, nil
}

// running returns the account's browser without launching one.
func (p *browserPool) running(profileDir string) *chrome.Browser {
	path, err := managedDir(p.root, profileDir)
	if err != nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.browsers[path]
}

func (p *browserPool) forget(path string, b *chrome.Browser) {
	p.mu.Lock()
	if p.browsers[path] == b {
		delete(p.browsers, path)
		delete(p.routes, path)
	}
	p.mu.Unlock()
}

// close shuts the account's browser down gracefully so Chrome flushes cookies.
func (p *browserPool) close(profileDir string) error {
	path, err := managedDir(p.root, profileDir)
	if err != nil {
		return err
	}
	unlock := p.lockLaunch(path)
	defer unlock()
	p.mu.Lock()
	b := p.browsers[path]
	delete(p.browsers, path)
	p.mu.Unlock()
	policy := p.cfg.Get().Accounts
	if b == nil {
		// A browser left running by an earlier run still holds the profile.
		if b, err = chrome.ExistingBrowser(context.Background(), path, policy); err != nil || b == nil {
			return err
		}
	}
	return b.Close(policy)
}

func (p *browserPool) closeAll() {
	p.mu.Lock()
	browsers := p.browsers
	p.browsers = make(map[string]*chrome.Browser)
	p.mu.Unlock()
	for _, b := range browsers {
		_ = b.Close(p.cfg.Get().Accounts)
	}
}

// removeProfile deletes an account's profile directory (cookies, cache, login
// data) after its browser has closed. It refuses anything outside the root or
// reached through a link.
func (p *browserPool) removeProfile(profileDir string) error {
	if err := p.close(profileDir); err != nil {
		return err
	}
	path, err := managedDir(p.root, profileDir)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("Không xóa được thư mục profile: %w", err)
	}
	return nil
}

// managedDir returns the absolute path of dir when it lies strictly inside
// root and no existing component is a link or a file.
func managedDir(root, dir string) (string, error) {
	if dir == "" {
		return "", errors.New("Tài khoản chưa có profile.")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(dir)
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
