// Package config owns the user-editable runtime settings and their persistence.
//
// Application identity (name, version) is NOT configuration; see internal/buildinfo.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"SnakeMedia/pkg/utils"
)

// Theme is the UI colour scheme preference.
type Theme string

const (
	ThemeDark   Theme = "dark"
	ThemeLight  Theme = "light"
	ThemeSystem Theme = "system"
)

// AllThemes is bound to the frontend as a TypeScript enum (config.Theme).
var AllThemes = []struct {
	Value  Theme
	TSName string
}{
	{ThemeDark, "Dark"},
	{ThemeLight, "Light"},
	{ThemeSystem, "System"},
}

// LogLevel is the minimum severity written by the logger.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// AllLogLevels is bound to the frontend as a TypeScript enum (config.LogLevel).
var AllLogLevels = []struct {
	Value  LogLevel
	TSName string
}{
	{LogLevelDebug, "Debug"},
	{LogLevelInfo, "Info"},
	{LogLevelWarn, "Warn"},
	{LogLevelError, "Error"},
}

// Window geometry bounds. Sizes are in device-independent pixels; the scale is the
// fraction of the primary screen's width and height the window opens at.
const (
	MinWindowWidth  = 800
	MinWindowHeight = 600
	MinWindowScale  = 0.3
	MaxWindowScale  = 1.0

	// MinPort..MaxPort bounds a Chrome debugging port read back from a profile.
	MinPort = 1024
	MaxPort = 65535
)

// ErrInvalidConfig marks configuration that fails validation or cannot be decoded.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// ErrConflict refuses an update made from an older copy of the configuration:
// saving it would undo changes another screen saved in between.
var ErrConflict = errors.New("Cài đặt vừa được lưu ở màn hình khác; hãy thử lưu lại.")

// AppConfig defines user-editable runtime settings.
type AppConfig struct {
	Theme    Theme    `json:"theme"`
	LogLevel LogLevel `json:"logLevel"`
	// WindowScale is the fraction (MinWindowScale..MaxWindowScale) of the primary
	// screen's width and height used for the window at startup.
	WindowScale float64       `json:"windowScale"`
	Accounts    AccountConfig `json:"accounts"`
	Flow        FlowConfig    `json:"flow"`
	Muse        MuseConfig    `json:"muse"`
	// Revision counts saves in this run. An update must carry the revision it
	// was made from (0 from a client that predates it is accepted), so a stale
	// screen cannot overwrite a newer save. It is not kept across restarts.
	Revision int `json:"revision"`
}

// DefaultConfig provides the fallback settings used on first run or when the file is unusable.
func DefaultConfig() AppConfig {
	return AppConfig{
		Theme:       ThemeDark,
		LogLevel:    LogLevelInfo,
		WindowScale: 0.7,
		Accounts:    DefaultAccountConfig(),
		Flow:        DefaultFlowConfig(),
		Muse:        DefaultMuseConfig(),
	}
}

// Validate reports whether every field holds an allowed value.
func (c AppConfig) Validate() error {
	var errs []error
	if !isOneOf(c.Theme, ThemeDark, ThemeLight, ThemeSystem) {
		errs = append(errs, fmt.Errorf("theme %q is not supported", c.Theme))
	}
	if !isOneOf(c.LogLevel, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError) {
		errs = append(errs, fmt.Errorf("logLevel %q is not supported", c.LogLevel))
	}
	// Written as a positive range check so NaN is rejected too.
	if !(c.WindowScale >= MinWindowScale && c.WindowScale <= MaxWindowScale) {
		errs = append(errs, fmt.Errorf("windowScale %v must be between %v and %v", c.WindowScale, MinWindowScale, MaxWindowScale))
	}
	if err := c.Accounts.Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := c.Flow.Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := c.Muse.Validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errors.Join(errs...))
	}
	return nil
}

// Manager loads, validates and persists AppConfig. It is safe for concurrent use.
type Manager struct {
	mu     sync.RWMutex
	config AppConfig
	path   string
}

// NewManager loads configuration from path. It always returns a usable manager:
// on a missing file it uses defaults silently; on an unreadable, corrupt or invalid
// file it uses defaults, moves the bad file aside, and returns the error for logging.
func NewManager(path string) (*Manager, error) {
	m := &Manager{config: DefaultConfig(), path: path}
	err := m.load()
	m.config.Revision = 1
	return m, err
}

// Path returns the file the manager persists to.
func (m *Manager) Path() string {
	return m.path
}

// Get returns a copy of the current configuration.
func (m *Manager) Get() AppConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Update validates cfg, persists it atomically, and only then makes it current.
// cfg must be based on the current revision (see AppConfig.Revision).
func (m *Manager) Update(cfg AppConfig) (AppConfig, error) {
	if err := cfg.Validate(); err != nil {
		return m.Get(), err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.Revision != 0 && cfg.Revision != m.config.Revision {
		return m.config, ErrConflict
	}
	cfg.Revision = m.config.Revision + 1
	if err := m.write(cfg); err != nil {
		return m.config, err
	}
	m.config = cfg
	return m.config, nil
}

func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: read %s: %w", m.path, err)
	}

	// Decode over defaults so fields added in newer versions keep sensible values.
	cfg := DefaultConfig()
	decodeErr := json.Unmarshal(data, &cfg)
	if decodeErr == nil {
		migrate(data, &cfg)
		cfg.Revision = 0
		decodeErr = cfg.Validate()
	} else {
		decodeErr = fmt.Errorf("%w: %w", ErrInvalidConfig, decodeErr)
	}
	if decodeErr != nil {
		return m.quarantine(decodeErr)
	}

	m.config = cfg
	return nil
}

// migrate carries values saved under an older key over to where they live now.
func migrate(data []byte, cfg *AppConfig) {
	var old struct {
		Accounts struct {
			MaxDownloadBytes    *int64 `json:"maxDownloadBytes"`
			MaxDiscoveryBytes   *int64 `json:"maxDiscoveryBytes"`
			MaxDiscoveryScripts *int   `json:"maxDiscoveryScripts"`
			RPCCacheSeconds     *int   `json:"rpcCacheSeconds"`
		} `json:"accounts"`
		Flow struct {
			MaxDownloadBytes    *int64 `json:"maxDownloadBytes"`
			MaxDiscoveryBytes   *int64 `json:"maxDiscoveryBytes"`
			MaxDiscoveryScripts *int   `json:"maxDiscoveryScripts"`
			RPCCacheSeconds     *int   `json:"rpcCacheSeconds"`
		} `json:"flow"`
	}
	if json.Unmarshal(data, &old) != nil {
		return
	}
	// The download cap used to be Flow's own; Muse downloads results too.
	if old.Accounts.MaxDownloadBytes == nil && old.Flow.MaxDownloadBytes != nil {
		cfg.Accounts.MaxDownloadBytes = *old.Flow.MaxDownloadBytes
	}
	// Flow's RPC discovery policy used to sit with the shared account policy.
	if old.Flow.MaxDiscoveryBytes == nil && old.Accounts.MaxDiscoveryBytes != nil {
		cfg.Flow.MaxDiscoveryBytes = *old.Accounts.MaxDiscoveryBytes
	}
	if old.Flow.MaxDiscoveryScripts == nil && old.Accounts.MaxDiscoveryScripts != nil {
		cfg.Flow.MaxDiscoveryScripts = *old.Accounts.MaxDiscoveryScripts
	}
	if old.Flow.RPCCacheSeconds == nil && old.Accounts.RPCCacheSeconds != nil {
		cfg.Flow.RPCCacheSeconds = *old.Accounts.RPCCacheSeconds
	}
}

// quarantine moves an unusable config file aside so the user's data is kept for inspection.
func (m *Manager) quarantine(cause error) error {
	backup := fmt.Sprintf("%s.corrupt-%s", m.path, time.Now().Format("20060102-150405"))
	if err := os.Rename(m.path, backup); err != nil {
		return fmt.Errorf("config: %w (moving it aside also failed: %v)", cause, err)
	}
	return fmt.Errorf("config: %w (moved to %s, using defaults)", cause, backup)
}

func (m *Manager) write(cfg AppConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := utils.WriteFileAtomic(m.path, data, 0o644); err != nil {
		return fmt.Errorf("config: save %s: %w", m.path, err)
	}
	return nil
}

func isOneOf[T comparable](v T, allowed ...T) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
