package config

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManager_DefaultsWhenFileMissing(t *testing.T) {
	m, err := NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewManager returned error for missing file: %v", err)
	}
	if got := m.Get(); got != DefaultConfig() {
		t.Errorf("Get() = %+v; want defaults %+v", got, DefaultConfig())
	}
}

func TestManager_UpdatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	m, _ := NewManager(path)

	cfg := m.Get()
	cfg.Theme = ThemeLight
	if _, err := m.Update(cfg); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	m2, err := NewManager(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if got := m2.Get().Theme; got != ThemeLight {
		t.Errorf("persisted Theme = %q; want %q", got, ThemeLight)
	}
}

func TestManager_UpdateRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m, _ := NewManager(path)

	cfg := m.Get()
	cfg.Theme = "neon"
	cfg.WindowScale = 5
	got, err := m.Update(cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if got != DefaultConfig() || m.Get() != DefaultConfig() {
		t.Errorf("invalid update must not change state; got %+v", m.Get())
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("invalid update must not write the file")
	}
}

func TestValidate_WindowScaleBounds(t *testing.T) {
	cases := map[float64]bool{
		MinWindowScale: true,
		0.7:            true,
		MaxWindowScale: true,
		0.1:            false,
		1.5:            false,
		0:              false,
		math.NaN():     false,
	}
	for scale, valid := range cases {
		cfg := DefaultConfig()
		cfg.WindowScale = scale
		if err := cfg.Validate(); (err == nil) != valid {
			t.Errorf("WindowScale %v: Validate() = %v; want valid=%v", scale, err, valid)
		}
	}
}

func TestManager_CorruptFileIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(path)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if m.Get() != DefaultConfig() {
		t.Errorf("expected defaults after corrupt file, got %+v", m.Get())
	}

	entries, _ := os.ReadDir(dir)
	var quarantined bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.json.corrupt-") {
			quarantined = true
		}
	}
	if !quarantined {
		t.Errorf("expected corrupt file to be moved aside, dir has %v", entries)
	}
}

func TestManager_PartialFileKeepsDefaultsAndIgnoresUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"theme":"light","appName":"legacy"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	want := DefaultConfig()
	want.Theme = ThemeLight
	if got := m.Get(); got != want {
		t.Errorf("Get() = %+v; want %+v", got, want)
	}
}
