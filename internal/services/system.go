package services

import (
	"context"
	"runtime"
	"time"

	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/models"
)

// SystemInfo represents basic host runtime metadata.
type SystemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
	Uptime    string `json:"uptime"`
}

// SystemService provides system-level operations for the desktop app.
type SystemService struct {
	info      buildinfo.Info
	startTime time.Time
}

// NewSystemService creates a new system service instance.
func NewSystemService(info buildinfo.Info) *SystemService {
	return &SystemService{
		info:      info,
		startTime: time.Now(),
	}
}

// GetAppInfo returns the application identity and build metadata.
func (s *SystemService) GetAppInfo(_ context.Context) models.Response[buildinfo.Info] {
	return models.SuccessResponse(s.info)
}

// GetSystemInfo retrieves runtime information about the host and environment.
func (s *SystemService) GetSystemInfo(_ context.Context) models.Response[SystemInfo] {
	return models.SuccessResponse(SystemInfo{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		Uptime:    time.Since(s.startTime).Truncate(time.Second).String(),
	})
}
