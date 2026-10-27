package services

import (
	"context"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/models"
)

func newSettings(t *testing.T) *SettingsService {
	t.Helper()
	m, err := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewSettingsService(m)
}

func TestSettingsService_UpdateConfig(t *testing.T) {
	s := newSettings(t)
	cfg := s.GetConfig(context.Background()).Data
	cfg.Theme = config.ThemeLight

	res := s.UpdateConfig(context.Background(), cfg)
	if !res.Success || res.Code != models.CodeOK || res.Data.Theme != config.ThemeLight {
		t.Fatalf("unexpected response: %+v", res)
	}
}

func TestSettingsService_UpdateConfigInvalid(t *testing.T) {
	s := newSettings(t)
	cfg := s.GetConfig(context.Background()).Data
	cfg.LogLevel = "verbose"

	res := s.UpdateConfig(context.Background(), cfg)
	if res.Success || res.Code != models.CodeInvalidArgument || res.Error == "" {
		t.Fatalf("expected invalid-argument response, got %+v", res)
	}
	got := s.GetConfig(context.Background()).Data
	got.Revision = 0
	if got != config.DefaultConfig() {
		t.Errorf("state changed after rejected update: %+v", got)
	}
}

func TestSettingsService_UpdateConfigConflict(t *testing.T) {
	s := newSettings(t)
	stale := s.GetConfig(context.Background()).Data
	fresh := stale
	fresh.Theme = config.ThemeLight
	if res := s.UpdateConfig(context.Background(), fresh); !res.Success {
		t.Fatalf("first save failed: %+v", res)
	}
	stale.LogLevel = config.LogLevelDebug
	res := s.UpdateConfig(context.Background(), stale)
	if res.Success || res.Code != models.CodeConflict || res.Data.Theme != config.ThemeLight {
		t.Fatalf("expected a conflict carrying the current config, got %+v", res)
	}
}
