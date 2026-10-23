// Package logger provides the process-wide structured logger built on log/slog.
//
// Logs are written to <Dir>/app.log (rotated by size at startup) and mirrored to
// stdout. Release GUI builds on Windows have no console, so the file is the primary
// sink; it is written first so a failing stdout never drops a record.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	fileName = "app.log"

	// DefaultMaxSizeBytes is the size at which app.log is rotated on startup.
	DefaultMaxSizeBytes int64 = 5 << 20
	// DefaultMaxBackups is how many rotated files (app.log.1 … app.log.N) are kept.
	DefaultMaxBackups = 3
)

// Options configures Init.
type Options struct {
	Level        string // debug | info | warn | error (anything else means info)
	Dir          string // directory for app.log; empty means stdout only
	MaxSizeBytes int64  // <= 0 uses DefaultMaxSizeBytes
	MaxBackups   int    // <= 0 uses DefaultMaxBackups
}

var (
	mu      sync.Mutex
	level   = new(slog.LevelVar)
	current = slog.Default()
	logFile *os.File
)

// Init (re)configures the global logger and sets it as the slog default.
// It always installs a working logger; a non-nil error means the file sink
// could not be opened and only stdout is in use.
func Init(opts Options) (*slog.Logger, error) {
	mu.Lock()
	defer mu.Unlock()

	closeFileLocked()
	level.Set(ParseLevel(opts.Level))

	var (
		writers []io.Writer
		fileErr error
	)
	if opts.Dir != "" {
		f, err := openLogFile(opts)
		if err != nil {
			fileErr = fmt.Errorf("logger: file sink disabled: %w", err)
		} else {
			logFile = f
			writers = append(writers, f)
		}
	}
	writers = append(writers, os.Stdout)

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: level})
	current = slog.New(handler)
	slog.SetDefault(current)
	return current, fileErr
}

// Get returns the configured logger (slog.Default before Init).
func Get() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	return current
}

// SetLevel changes the minimum level at runtime without rebuilding the logger.
func SetLevel(levelStr string) {
	level.Set(ParseLevel(levelStr))
}

// Level returns the currently active minimum level.
func Level() slog.Level {
	return level.Level()
}

// ParseLevel maps a level name to slog.Level, defaulting to Info.
func ParseLevel(levelStr string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Close flushes and closes the log file, falling back to a stdout-only logger.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	err := closeFileLocked()
	current = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(current)
	return err
}

func closeFileLocked() error {
	if logFile == nil {
		return nil
	}
	err := logFile.Close()
	logFile = nil
	return err
}

func openLogFile(opts Options) (*os.File, error) {
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, err
	}
	maxSize := opts.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = DefaultMaxSizeBytes
	}
	maxBackups := opts.MaxBackups
	if maxBackups <= 0 {
		maxBackups = DefaultMaxBackups
	}

	path := filepath.Join(opts.Dir, fileName)
	if err := rotate(path, maxSize, maxBackups); err != nil {
		return nil, fmt.Errorf("rotate %s: %w", path, err)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// rotate shifts app.log -> app.log.1 -> … -> app.log.N when app.log exceeds maxSize.
func rotate(path string, maxSize int64, maxBackups int) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxSize {
		return nil
	}
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, fmt.Sprintf("%s.%d", path, i+1)); err != nil {
				return err
			}
		}
	}
	return os.Rename(path, path+".1")
}
