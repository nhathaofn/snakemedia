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
	if got := unrevised(m.Get()); got != DefaultConfig() {
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
	if unrevised(got) != DefaultConfig() || unrevised(m.Get()) != DefaultConfig() {
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
	if unrevised(m.Get()) != DefaultConfig() {
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
	if got := unrevised(m.Get()); got != want {
		t.Errorf("Get() = %+v; want %+v", got, want)
	}
}

// unrevised drops the run-time save counter so a loaded config compares with defaults.
func unrevised(cfg AppConfig) AppConfig {
	cfg.Revision = 0
	return cfg
}

func TestManager_UpdateRefusesStaleRevision(t *testing.T) {
	m, _ := NewManager(filepath.Join(t.TempDir(), "config.json"))
	screenA, screenB := m.Get(), m.Get()

	screenA.Theme = ThemeLight
	saved, err := m.Update(screenA)
	if err != nil || saved.Revision != screenA.Revision+1 {
		t.Fatalf("first save failed: %+v %v", saved, err)
	}
	screenB.LogLevel = LogLevelDebug
	if _, err := m.Update(screenB); !errors.Is(err, ErrConflict) {
		t.Fatalf("a save from a stale copy must be refused, got %v", err)
	}
	if got := m.Get(); got.Theme != ThemeLight || got.LogLevel != DefaultConfig().LogLevel {
		t.Fatalf("stale save changed the config: %+v", got)
	}
	// A client without revisions (0) is still accepted.
	legacy := m.Get()
	legacy.Revision, legacy.LogLevel = 0, LogLevelWarn
	if _, err := m.Update(legacy); err != nil {
		t.Fatalf("legacy save refused: %v", err)
	}
}

func TestManager_DownloadCapMovesFromFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"flow":{"maxDownloadBytes":104857600}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Accounts.MaxDownloadBytes; got != 100<<20 {
		t.Fatalf("old Flow download cap not carried over: %d", got)
	}
}

func TestManager_RPCDiscoveryMovesToFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"accounts":{"maxDiscoveryBytes":2097152,"maxDiscoveryScripts":8,"rpcCacheSeconds":60}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Get()
	if cfg.Flow.MaxDiscoveryBytes != 2<<20 || cfg.Flow.MaxDiscoveryScripts != 8 || cfg.Flow.RPCCacheSeconds != 60 {
		t.Fatalf("old discovery policy not carried over: %+v", cfg.Flow)
	}
	policy := cfg.FlowPagePolicy()
	if policy.MaxDiscoveryBytes != 2<<20 || policy.MaxDiscoveryScripts != 8 || policy.RPCCacheSeconds != 60 {
		t.Fatalf("page policy not filled from Flow: %+v", policy)
	}
	if _, err := m.Update(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	accounts := string(data)[strings.Index(string(data), `"accounts"`):strings.Index(string(data), `"flow"`)]
	if strings.Contains(accounts, "Discovery") || strings.Contains(accounts, "rpcCache") {
		t.Fatalf("discovery policy still saved under accounts: %s", accounts)
	}
	if !strings.Contains(string(data), `"rpcCacheSeconds": 60`) {
		t.Fatal("discovery policy not saved under flow")
	}
}

func TestDefaultFlowPagePolicyHasDiscoveryDefaults(t *testing.T) {
	p, f := DefaultFlowPagePolicy(), DefaultFlowConfig()
	if p.MaxDiscoveryBytes != f.MaxDiscoveryBytes || p.MaxDiscoveryScripts != f.MaxDiscoveryScripts || p.RPCCacheSeconds != f.RPCCacheSeconds || p.RPCCacheSeconds == 0 {
		t.Fatalf("defaults not filled: %+v", p)
	}
	bad := DefaultFlowConfig()
	bad.RPCCacheSeconds = 0
	if bad.Validate() == nil {
		t.Fatal("zero RPC cache accepted")
	}
}

func TestManager_SavesSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m, _ := NewManager(path)
	if _, err := m.Update(m.Get()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"schemaVersion": 1`) || !strings.Contains(string(data), `"theme"`) {
		t.Fatalf("saved config = %s", data)
	}
}

func TestManager_NewerConfigIsReadOnlyAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	newer := []byte(`{"schemaVersion": 99, "theme": "light", "someNewSetting": true}`)
	if err := os.WriteFile(path, newer, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(path)
	if !errors.Is(err, ErrNewerConfig) {
		t.Fatalf("NewManager error = %v, want ErrNewerConfig", err)
	}
	if m.Get().Theme != ThemeLight {
		t.Errorf("values the build accepts were not used: theme %q", m.Get().Theme)
	}
	if _, err := m.Update(m.Get()); !errors.Is(err, ErrNewerConfig) {
		t.Fatalf("Update = %v, want the save refused", err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(newer) {
		t.Fatalf("file changed to %s", data)
	}
	if matches, _ := filepath.Glob(path + ".corrupt-*"); len(matches) > 0 {
		t.Fatal("a newer config was moved aside as corrupt")
	}
}
