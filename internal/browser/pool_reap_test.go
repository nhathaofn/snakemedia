package browser

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"SnakeMedia/internal/config"
)

func startFake(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
	return &Browser{ProfileDir: opts.ProfileDir, Headless: opts.Headless, done: make(chan struct{}), cmd: launchedCmd()}, nil
}

// Idle headless browsers are closed; a window, a busy browser and a recently
// used one stay.
func TestPoolReapsIdleHeadlessBrowsers(t *testing.T) {
	pool, profile := testPool(t)
	pool.start = startFake
	closed := 0
	pool.shutdown = func(*Browser, config.AccountConfig) error { closed++; return nil }
	now := time.Now()
	pool.now = func() time.Time { return now }
	var reported []string
	pool.OnClosed(func(dir string) { reported = append(reported, dir) })

	root := filepath.Dir(profile)
	busy, window := filepath.Join(root, "busy"), filepath.Join(root, "window")
	for _, p := range []struct {
		dir  string
		want Want
	}{{profile, Want{Route: "direct"}}, {busy, Want{Route: "direct"}}, {window, Want{Route: "direct", Visible: true}}} {
		if _, err := pool.Get(context.Background(), p.dir, p.want); err != nil {
			t.Fatal(err)
		}
	}
	release := pool.Acquire(busy)
	defer release()

	if n := pool.reapIdle(); n != 0 {
		t.Fatalf("fresh browsers reaped: %d", n)
	}
	idle := time.Duration(config.DefaultAccountConfig().IdleBrowserMinutes) * time.Minute
	pool.now = func() time.Time { return now.Add(idle + time.Second) }
	if n := pool.reapIdle(); n != 1 || closed != 1 {
		t.Fatalf("reaped %d (closed %d), want only the idle headless one", n, closed)
	}
	if pool.Running(profile) != nil || pool.Running(busy) == nil || pool.Running(window) == nil {
		t.Fatal("wrong browsers closed")
	}
	if len(reported) != 1 || filepath.Base(reported[0]) != filepath.Base(profile) {
		t.Fatalf("OnClosed got %v", reported)
	}
}

// After CloseAll no browser starts; every running one was closed.
func TestPoolCloseAllRefusesNewBrowsers(t *testing.T) {
	pool, profile := testPool(t)
	pool.start = startFake
	closed := 0
	pool.shutdown = func(*Browser, config.AccountConfig) error { closed++; return nil }
	other := filepath.Join(filepath.Dir(profile), "other")
	for _, dir := range []string{profile, other} {
		if _, err := pool.Get(context.Background(), dir, Want{Route: "direct"}); err != nil {
			t.Fatal(err)
		}
	}
	pool.CloseAll()
	if closed != 2 {
		t.Fatalf("closed %d browsers, want 2", closed)
	}
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct"}); !errors.Is(err, errPoolClosed) {
		t.Fatalf("browser started after CloseAll: %v", err)
	}
}

// A Chrome from an earlier run that will not exit is never used: its proxy
// flags are unknown.
func TestPoolRefusesStubbornAdoptedBrowser(t *testing.T) {
	pool, profile := testPool(t)
	pool.start = func(_ context.Context, opts LaunchOptions, _ config.AccountConfig) (*Browser, error) {
		return &Browser{ProfileDir: opts.ProfileDir}, nil // always adopted
	}
	if _, err := pool.Get(context.Background(), profile, Want{Route: "direct"}); err == nil {
		t.Fatal("an adopted browser with unknown flags was handed out")
	}
	if pool.Running(profile) != nil {
		t.Fatal("adopted browser recorded")
	}
}
