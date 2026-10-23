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
	if got := s.GetConfig(context.Background()).Data; got != config.DefaultConfig() {
		t.Errorf("state changed after rejected update: %+v", got)
	}
}
