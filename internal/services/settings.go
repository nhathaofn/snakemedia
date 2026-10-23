package services

import (
	"context"
	"errors"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
)

// SettingsService reads and updates the user configuration and applies side effects.
type SettingsService struct {
	cfg *config.Manager
}

// NewSettingsService creates a settings service backed by the given config manager.
func NewSettingsService(cfg *config.Manager) *SettingsService {
	return &SettingsService{cfg: cfg}
}

// GetConfig returns the current configuration.
func (s *SettingsService) GetConfig(_ context.Context) models.Response[config.AppConfig] {
	return models.SuccessResponse(s.cfg.Get())
}

// UpdateConfig validates and persists newCfg, then applies runtime side effects.
func (s *SettingsService) UpdateConfig(_ context.Context, newCfg config.AppConfig) models.Response[config.AppConfig] {
	updated, err := s.cfg.Update(newCfg)
	if err != nil {
		code := models.CodeInternal
		if errors.Is(err, config.ErrInvalidConfig) {
			code = models.CodeInvalidArgument
		}
		logger.Get().Warn("config update rejected", "error", err)
		return models.ErrorResponse[config.AppConfig](err.Error(), code)
	}

	logger.SetLevel(string(updated.LogLevel))
	logger.Get().Info("config updated", "theme", updated.Theme, "logLevel", updated.LogLevel)
	return models.SuccessResponse(updated)
}
