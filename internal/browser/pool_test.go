package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/config"
)

func testPool(t *testing.T) (*Pool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, _ := config.NewManager(filepath.Join(dir, "config.json"))
	pool := NewPool(cfg, filepath.Join(dir, "profiles"))
	pool.findChrome = func() (string, error) { return "chrome", nil }
	pool.validate = func(context.Context, *Browser, config.AccountConfig) error { return nil }
	pool.shutdown = func(*Browser, config.AccountConfig) error { return nil }
	return pool, filepath.Join(dir, "profiles", "acc")
}

// A running browser is reused only with the same proxy route; a new route
// restarts it so Chrome never keeps the old network.
func TestPoolRestartsWhenRouteChanges(t *testing.T) {
	pool, profile := testPool(t)
	var launches [][]string
	closed := 0
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		launches = append(launches, opts.ExtraArgs)
		return &Browser{ProfileDir: opts.ProfileDir, Headless: opts.Headless, done: make(chan struct{}), cmd: launchedCmd()}, nil
	}
	pool.shutdown = func(*Browser, config.AccountConfig) error { closed++; return nil }
	get := func(want Want) {
		t.Helper()
		if _, err := pool.Get(context.Background(), profile, want); err != nil {
			t.Fatal(err)
		}
	}
	get(Want{Route: "direct"})
	get(Want{Route: "direct"})
	if len(launches) != 1 {
		t.Fatalf("same route relaunched: %d launches", len(launches))
	}
	get(Want{Route: "1.2.3.4:80", Args: []string{"--proxy-server=http://127.0.0.1:5000"}})
	if len(launches) != 2 || closed != 1 || launches[1][0] != "--proxy-server=http://127.0.0.1:5000" {
		t.Fatalf("launches = %v, closed = %d", launches, closed)
	}
}

// A browser adopted from an earlier run has unknown flags and is restarted.
func TestPoolRestartsAdoptedBrowser(t *testing.T) {
	pool, profile := testPool(t)
	starts := 0
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		starts++
		b := &Browser{ProfileDir: opts.ProfileDir}
		if starts > 1 {
			b.done = make(chan struct{})
			b.cmd = launchedCmd()
		}
		return b, nil
	}
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct"}); err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("adopted browser kept: %d starts", starts)
	}
}

// Work that holds a browser keeps it: a sign-in window or a new proxy waits.
func TestPoolKeepsBrowserInUse(t *testing.T) {
	pool, profile := testPool(t)
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		return &Browser{ProfileDir: opts.ProfileDir, Headless: opts.Headless, done: make(chan struct{}), cmd: launchedCmd()}, nil
	}
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct"}); err != nil {
		t.Fatal(err)
	}
	release := pool.Acquire(profile)
	if _, err := pool.Get(context.Background(), profile, Want{Route: "other"}); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("busy browser restarted for a new route: %v", err)
	}
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct", Visible: true}); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("busy headless browser replaced by a window: %v", err)
	}
	release()
	if _, err := pool.Get(context.Background(), profile, Want{Route: "other"}); err != nil {
		t.Fatal(err)
	}
}

// Closing or removing a profile that work holds is refused, so a job that
// acquired the browser just before is never killed or has its profile deleted.
func TestPoolCloseAndRemoveRefuseBusyProfile(t *testing.T) {
	pool, profile := testPool(t)
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		return &Browser{ProfileDir: opts.ProfileDir, done: make(chan struct{}), cmd: launchedCmd()}, nil
	}
	closed := 0
	pool.shutdown = func(*Browser, config.AccountConfig) error { closed++; return nil }
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	release := pool.Acquire(profile)
	if err := pool.CloseIfIdle(profile); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("busy browser closed: %v", err)
	}
	if err := pool.RemoveIfIdle(profile); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("busy profile removed: %v", err)
	}
	if closed != 0 || pool.Running(profile) == nil {
		t.Fatalf("busy browser shut down (%d) or forgotten", closed)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("busy profile deleted: %v", err)
	}
	release()
	if err := pool.RemoveIfIdle(profile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) || closed != 1 {
		t.Fatalf("idle profile not removed (closed %d): %v", closed, err)
	}
}

// A page that fails to load through the proxy is told apart from other load
// failures.
func TestProxyNavigationErrors(t *testing.T) {
	if !IsProxyError(&NavigateError{Text: "net::ERR_PROXY_CONNECTION_FAILED"}) || !IsProxyError(&NavigateError{Text: "net::ERR_TUNNEL_CONNECTION_FAILED"}) {
		t.Fatal("proxy failure not recognised")
	}
	if IsProxyError(&NavigateError{Text: "net::ERR_NAME_NOT_RESOLVED"}) || IsProxyError(errors.New("Không tìm thấy Google Chrome")) {
		t.Fatal("non-proxy failure blamed on the proxy")
	}
}

// Profiles outside the managed root are refused.
func TestPoolRefusesForeignProfile(t *testing.T) {
	pool, _ := testPool(t)
	if _, err := pool.Get(context.Background(), t.TempDir(), Want{}); err == nil {
		t.Fatal("profile outside the root accepted")
	}
}

// launchedCmd marks a test browser as started by this run rather than adopted.
func launchedCmd() *exec.Cmd { return &exec.Cmd{} }
