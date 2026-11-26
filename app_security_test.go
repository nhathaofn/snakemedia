package main

import (
	"os"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/muse"
	"SnakeMedia/internal/services"
	"SnakeMedia/internal/tools"
)

func testSecurityApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	cfgManager, err := config.NewManager(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	store := media.NewStore(filepath.Join(dir, "media"))
	toolSet, err := tools.New(filepath.Join(dir, "tools"), "test/1.0", func() map[string]string {
		return cfgManager.Get().Tools.Paths
	})
	if err != nil {
		t.Fatal(err)
	}
	return &App{
		info:     buildinfo.Info{Name: "SnakeMedia", ProductName: "SnakeMedia", Version: "1.4.3"},
		cfg:      cfgManager,
		settings: services.NewSettingsService(cfgManager),
		media:    store,
		tools:    toolSet,
	}
}

func TestUpdateConfigCannotModifyServerManagedDirsOrUnapprovedTools(t *testing.T) {
	a := testSecurityApp(t)

	// Seed server-managed directories via config manager (as native dialogs do).
	seed := a.cfg.Get()
	seed.Flow.BatchOutputDir = filepath.Join(t.TempDir(), "flow-out")
	seed.Muse.OutputDir = filepath.Join(t.TempDir(), "muse-out")
	seed.Muse.AssetVaultDir = filepath.Join(t.TempDir(), "muse-vault")
	if _, err := a.cfg.Update(seed); err != nil {
		t.Fatal(err)
	}

	// Attempt to hijack directory roots via UpdateConfig (compromised webview scenario).
	attempt := a.cfg.Get()
	attempt.Theme = config.ThemeLight
	attempt.Flow.BatchOutputDir = filepath.Join(t.TempDir(), "hijack-flow")
	attempt.Muse.OutputDir = filepath.Join(t.TempDir(), "hijack-muse")
	attempt.Muse.AssetVaultDir = filepath.Join(t.TempDir(), "hijack-vault")
	res := a.UpdateConfig(attempt)
	if !res.Success {
		t.Fatalf("UpdateConfig failed unexpectedly: %+v", res)
	}
	if res.Data.Theme != config.ThemeLight {
		t.Fatalf("Theme not saved: %q", res.Data.Theme)
	}
	if res.Data.Flow.BatchOutputDir != seed.Flow.BatchOutputDir ||
		res.Data.Muse.OutputDir != seed.Muse.OutputDir ||
		res.Data.Muse.AssetVaultDir != seed.Muse.AssetVaultDir {
		t.Fatalf("UpdateConfig allowed modifying server-managed directories: got (%q, %q, %q)",
			res.Data.Flow.BatchOutputDir, res.Data.Muse.OutputDir, res.Data.Muse.AssetVaultDir)
	}

	// Unapproved tool executable path must be rejected.
	fakeExe := filepath.Join(t.TempDir(), "evil.exe")
	if err := os.WriteFile(fakeExe, []byte("exe"), 0o600); err != nil {
		t.Fatal(err)
	}
	badTool := a.cfg.Get()
	badTool.Tools.Paths = map[string]string{"ffmpeg": fakeExe}
	badRes := a.UpdateConfig(badTool)
	if badRes.Success || badRes.Code != models.CodeInvalidArgument {
		t.Fatalf("expected UpdateConfig to reject unapproved tool path, got %+v", badRes)
	}

	// Unknown program name must be rejected even if approved somehow.
	unknownProg := a.cfg.Get()
	unknownProg.Tools.Paths = map[string]string{"powershell": fakeExe}
	unkRes := a.UpdateConfig(unknownProg)
	if unkRes.Success || unkRes.Code != models.CodeInvalidArgument {
		t.Fatalf("expected UpdateConfig to reject unknown program, got %+v", unkRes)
	}

	// Approved tool path (simulating PickToolPath) is accepted, and clearing to "" is allowed.
	a.approveToolPath("ffmpeg", fakeExe)
	okTool := a.cfg.Get()
	okTool.Tools.Paths = map[string]string{"ffmpeg": fakeExe}
	okRes := a.UpdateConfig(okTool)
	if !okRes.Success || okRes.Data.Tools.Paths["ffmpeg"] != fakeExe {
		t.Fatalf("expected approved tool path to succeed, got %+v", okRes)
	}
	clearTool := a.cfg.Get()
	clearTool.Tools.Paths = map[string]string{}
	clearRes := a.UpdateConfig(clearTool)
	if !clearRes.Success || len(clearRes.Data.Tools.Paths) != 0 {
		t.Fatalf("expected clearing tool path to succeed, got %+v", clearRes)
	}
}

func TestVaultAndBatchImportRequireNativePickerApproval(t *testing.T) {
	a := testSecurityApp(t)

	// Unapproved Vault directory is rejected before reaching Muse provider.
	vaultRes := a.SetMuseAssetVaultDirectory(filepath.Join(t.TempDir(), "unapproved"), false)
	if vaultRes.Success || vaultRes.Code != models.CodeInvalidArgument {
		t.Fatalf("expected unapproved Vault dir to be rejected, got %+v", vaultRes)
	}

	// Unapproved raw OS path in ScanMuseAssetPaths and ImportMuseAssetBatch is rejected.
	rawImg := filepath.Join(t.TempDir(), "secret.png")
	scanRes := a.ScanMuseAssetPaths([]string{rawImg})
	if scanRes.Success || scanRes.Code != models.CodeInvalidArgument {
		t.Fatalf("expected ScanMuseAssetPaths to reject unapproved path, got %+v", scanRes)
	}
	impRes := a.ImportMuseAssetBatch(muse.MuseAssetBatchRequest{
		Items: []muse.MuseAssetBatchItem{{SourcePath: rawImg, Tag: "x", Name: "X"}},
	})
	if impRes.Success || impRes.Code != models.CodeInvalidArgument {
		t.Fatalf("expected ImportMuseAssetBatch to reject unapproved SourcePath, got %+v", impRes)
	}
}
