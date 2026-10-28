package services

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/updater"
	"SnakeMedia/pkg/utils"
)

// progressEvery spaces download progress events, so a fast link does not
// flood the frontend.
const progressEvery = 200 * time.Millisecond

// quitDelay lets InstallUpdate's reply reach the window before the app closes.
const quitDelay = 500 * time.Millisecond

// UpdateService checks for, downloads and installs app updates, and opens the
// log folder for support.
type UpdateService struct {
	updater *updater.Updater
	logDir  string

	mu         sync.Mutex
	installing bool
	progress   updater.Progress
	emit       func(events.Event, any)
	quit       func()
}

// NewUpdateService wraps u; logDir is where app.log lives.
func NewUpdateService(u *updater.Updater, logDir string) *UpdateService {
	return &UpdateService{updater: u, logDir: logDir}
}

// Start connects the service to the running app: emit sends events, quit
// closes the app (its shutdown keeps every queue's jobs for the next start).
// Downloads an earlier update left behind are removed.
func (s *UpdateService) Start(emit func(events.Event, any), quit func()) {
	s.mu.Lock()
	s.emit, s.quit = emit, quit
	s.mu.Unlock()
	s.updater.Cleanup()
}

// CheckInBackground looks for a newer release and announces it; failures
// only reach the log (the user did not ask).
func (s *UpdateService) CheckInBackground(ctx context.Context) {
	go func() {
		defer logger.Recover("kiểm tra cập nhật")
		rel, err := s.updater.Check(ctx)
		if err != nil {
			if !errors.Is(err, updater.ErrDisabled) {
				logger.Get().Info("update check failed", "error", err)
			}
			return
		}
		logger.Get().Info("update check", "current", rel.Current, "latest", rel.Latest, "available", rel.Available)
		if rel.Available {
			s.send(events.EventUpdateAvailable, rel)
		}
	}()
}

// Check looks for a newer release.
func (s *UpdateService) Check(ctx context.Context) models.Response[updater.Release] {
	rel, err := s.updater.Check(ctx)
	if err != nil {
		return models.ErrorResponse[updater.Release](err.Error(), models.CodeUnavailable)
	}
	return models.SuccessResponse(rel)
}

// Install downloads the release the last Check found, verifies it, starts the
// silent install and closes the app; the installer starts it again.
func (s *UpdateService) Install(ctx context.Context) models.Response[bool] {
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return models.ErrorResponse[bool]("Đang tải bản cập nhật.", models.CodeConflict)
	}
	s.installing, s.progress = true, updater.Progress{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}()

	var last time.Time
	path, err := s.updater.Download(ctx, func(p updater.Progress) {
		s.mu.Lock()
		s.progress = p
		s.mu.Unlock()
		if now := time.Now(); now.Sub(last) >= progressEvery || p.Done == p.Total {
			last = now
			s.send(events.EventUpdateProgress, p)
		}
	})
	if err != nil {
		return models.ErrorResponse[bool](err.Error(), models.CodeUnavailable)
	}
	if err := s.updater.Install(path); err != nil {
		return models.ErrorResponse[bool](err.Error(), models.CodeInternal)
	}
	logger.Get().Info("update installer started, closing the app", "installer", path)
	s.mu.Lock()
	quit := s.quit
	s.mu.Unlock()
	if quit != nil {
		time.AfterFunc(quitDelay, quit)
	}
	return models.SuccessResponse(true)
}

// Progress is the running download's progress (zero when none runs).
func (s *UpdateService) Progress() models.Response[updater.Progress] {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.installing {
		return models.SuccessResponse(updater.Progress{})
	}
	return models.SuccessResponse(s.progress)
}

// OpenLogFolder shows the folder holding app.log.
func (s *UpdateService) OpenLogFolder() models.Response[bool] {
	if err := os.MkdirAll(s.logDir, 0o755); err != nil {
		return models.ErrorResponse[bool](err.Error(), models.CodeInternal)
	}
	if err := utils.OpenFolder(s.logDir); err != nil {
		return models.ErrorResponse[bool]("Không mở được thư mục log: "+err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

func (s *UpdateService) send(event events.Event, data any) {
	s.mu.Lock()
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(event, data)
	}
}
