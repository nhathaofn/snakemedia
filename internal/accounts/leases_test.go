package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
)

func TestManualCheckAndSharedWorkCannotOverlapOnProfile(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.NewManager(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(filepath.Join(root, "accounts"), cfg)
	profile, other := service.pool.NewProfile(), service.pool.NewProfile()
	release, err := service.AcquireProfile(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.tryCheckProfile(profile); !errors.Is(err, browser.ErrProfileBusy) {
		t.Fatalf("manual check admitted during worker lease: %v", err)
	}
	release()

	finishCheck, err := service.tryCheckProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.tryCheckProfile(profile); !errors.Is(err, browser.ErrProfileBusy) {
		t.Fatalf("second manual check admitted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := service.AcquireProfile(ctx, profile); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker admitted during a manual check: %v", err)
	}
	otherRelease, err := service.AcquireProfile(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	otherRelease()
	finishCheck()

	first, err := service.AcquireProfile(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := service.AcquireProfile(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	second()
}
