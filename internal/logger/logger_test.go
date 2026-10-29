package logger

import (
	"bytes"
	"errors"
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

func TestLogRotatesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(Options{Level: "info", Dir: dir, MaxSizeBytes: 512, MaxBackups: 2}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
	for i := 0; i < 40; i++ {
		Get().Info("filler line to grow the log past its cap", "i", i)
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "app.log"))
	if err != nil || info.Size() > 512 {
		t.Fatalf("app.log not kept under its cap: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.log.1")); err != nil {
		t.Fatalf("no rotated backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.log.3")); err == nil {
		t.Fatal("more backups kept than configured")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// A failing log file does not stop the stdout copy.
func TestFanOutKeepsWritingAfterAFailure(t *testing.T) {
	var out bytes.Buffer
	n, err := fanOut{failingWriter{}, &out}.Write([]byte("line"))
	if err == nil || n != 0 || out.String() != "line" {
		t.Fatalf("n=%d err=%v out=%q", n, err, out.String())
	}
	var a, b bytes.Buffer
	if n, err := (fanOut{&a, &b}).Write([]byte("ok")); err != nil || n != 2 || a.String() != "ok" || b.String() != "ok" {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
