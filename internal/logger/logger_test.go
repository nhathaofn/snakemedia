package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
		"":        slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestInit_WritesFileAndHonoursRuntimeLevel(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(Options{Level: "info", Dir: dir}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	Get().Debug("hidden-debug")
	SetLevel("debug")
	Get().Debug("visible-debug")
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	out := string(data)
	if strings.Contains(out, "hidden-debug") {
		t.Errorf("debug record written while level was info")
	}
	if !strings.Contains(out, "visible-debug") {
		t.Errorf("debug record missing after SetLevel(debug); log:\n%s", out)
	}
}

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "current-log")
	write(path+".1", "older")

	if err := rotate(path, 4, 3); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	for p, want := range map[string]string{path + ".1": "current-log", path + ".2": "older"} {
		got, err := os.ReadFile(p)
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", filepath.Base(p), got, err, want)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be rotated away", fileName)
	}
}

func TestRotate_BelowThresholdIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	if err := os.WriteFile(path, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rotate(path, 1<<20, 3); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("rotation should not happen below threshold")
	}
}
