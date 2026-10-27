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
		switch {
		case errors.Is(err, config.ErrInvalidConfig):
			code = models.CodeInvalidArgument
		case errors.Is(err, config.ErrConflict):
			code = models.CodeConflict
		}
		logger.Get().Warn("config update rejected", "error", err)
		// The current config travels with the refusal so the screen can rebase.
		res := models.ErrorResponse[config.AppConfig](err.Error(), code)
		res.Data = updated
		return res
	}

	logger.SetLevel(string(updated.LogLevel))
	logger.Get().Info("config updated", "theme", updated.Theme, "logLevel", updated.LogLevel)
	return models.SuccessResponse(updated)
}
