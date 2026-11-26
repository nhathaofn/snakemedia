package muse

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"SnakeMedia/internal/media"
)

func TestParseAndBuildAssetConventionNames(t *testing.T) {
	parsed := ParseAssetFileName("OLIVA_CHAR_jimena_full_work_soot_v2_rej.png")
	if parsed == nil {
		t.Fatal("expected parsed asset")
	}
	if parsed.Project != "OLIVA" || parsed.Type != MuseTypeChar || parsed.Role != MuseRoleCharacter ||
		parsed.Alias != "jimena" || parsed.Angle != "full" || parsed.Variant != "work" ||
		parsed.State != "soot" || parsed.Version != 2 || !parsed.Rejected {
		t.Fatalf("unexpected parse: %+v", parsed)
	}

	built := BuildAssetFileName("oliva", MuseTypeChar, "jimena", "full", "work", "soot", 2, true, ".png")
	if built != "OLIVA_CHAR_jimena_full_work_soot_v2_rej.png" {
		t.Fatalf("built = %q", built)
	}

	fold := BuildAssetFileName("", MuseTypeProp, "Áo Dài Đỏ", "", "", "", 1, false, ".webp")
	if fold != "PROP_ao_dai_do.webp" {
		t.Fatalf("fold = %q", fold)
	}

	ff := ParseAssetFileName("OLIVA_FF_sc01.png")
	if ff == nil || ff.Role != MuseRoleStoryboard || ff.Hint == "" {
		t.Fatalf("FF hint parse = %+v", ff)
	}
}

func TestVaultMultiAngleProjectResolutionAndSection13Priority(t *testing.T) {
	p := testProvider(t, &fakePage{})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 4)

	// Upload 2 angles of @jimena (front Primary + close_soot secondary) and a STYLE + LOC + VEH in OLIVA
	saveAngle := func(proj string, typ MuseAssetType, role MuseRefRole, tag, angle, state string, primary bool) MuseAsset {
		u, err := p.media.Put(assetPrefix+tag+angle+state, "image/png", ".png", testPNGBytes(t, 24, 24))
		if err != nil {
			t.Fatal(err)
		}
		res := p.SaveAsset(MuseAsset{
			Project: proj, Type: typ, Role: role, Tag: tag, Name: tag,
			Items: []MuseAssetItem{{URL: u, Angle: angle, State: state, Primary: primary}},
		})
		if !res.Success {
			t.Fatalf("save %s: %s", tag, res.Error)
		}
		return res.Data
	}

	uFront, _ := p.media.Put(assetPrefix+"jf", "image/png", ".png", testPNGBytes(t, 24, 24))
	uClose, _ := p.media.Put(assetPrefix+"jc", "image/png", ".png", testPNGBytes(t, 24, 24))
	uRej, _ := p.media.Put(assetPrefix+"jr", "image/png", ".png", testPNGBytes(t, 24, 24))
	jimRes := p.SaveAsset(MuseAsset{
		Project: "OLIVA", Type: MuseTypeChar, Role: MuseRoleCharacter, Tag: "jimena", Name: "Jimena",
		Items: []MuseAssetItem{
			{URL: uFront, Angle: "front", Primary: true},
			{URL: uClose, Angle: "close", State: "soot"},
			{URL: uRej, Angle: "back", Rejected: true},
		},
	})
	if !jimRes.Success {
		t.Fatalf("save jimena: %s", jimRes.Error)
	}

	saveAngle("OLIVA", MuseTypeStyle, MuseRoleStyle, "noir", "", "", true)
	saveAngle("OLIVA", MuseTypeLoc, MuseRoleSetting, "bep", "wide", "", true)
	saveAngle("OLIVA", MuseTypeVeh, MuseRoleVehicle, "xe_tho", "", "", true)
	saveAngle("OTHER", MuseTypeChar, MuseRoleCharacter, "jimena", "front", "", true)

	// Mentioning style before char and loc should sort Section 13 order: CHAR -> LOC -> VEH -> STYLE
	res := p.SubmitJobs(MuseJobRequest{
		Kind:    MuseJobVideo,
		Project: "OLIVA",
		Prompts: []string{"@noir trong @bep cạnh @xe_tho với @jimena và @jimena_close_soot"},
	})
	if !res.Success || len(res.Data) != 1 {
		t.Fatalf("submit failed: %+v", res)
	}
	job := res.Data[0]
	if len(job.RefInfo) != 5 {
		t.Fatalf("expected 5 attached refs (jimena front, jimena close_soot, bep, xe_tho, noir), got %+v", job.RefInfo)
	}
	gotOrder := []string{job.RefInfo[0].Tag + ":" + job.RefInfo[0].Type, job.RefInfo[1].Tag, job.RefInfo[2].Tag, job.RefInfo[3].Tag, job.RefInfo[4].Tag}
	wantOrder := []string{"jimena:CHAR", "jimena", "bep", "xe_tho", "noir"}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("Section 13 priority order = %v, want %v", gotOrder, wantOrder)
	}

	// Vault files remain untouched original PNG on disk while job References hold immutable /flow-media/ snapshots
	if !strings.HasPrefix(jimRes.Data.Items[0].URL, media.VaultRoute) ||
		!strings.HasSuffix(jimRes.Data.Items[0].FileName, ".png") ||
		!strings.Contains(jimRes.Data.Items[2].VaultRelPath, "rejected/") {
		t.Fatalf("vault paths unexpected: %+v", jimRes.Data.Items)
	}
	if !strings.HasPrefix(job.References[0], media.Route+referencePrefix) {
		t.Fatalf("expected snapshot in MediaDir, got %q", job.References[0])
	}
}

func TestVaultSupportsManyImagesCompressesOutgoingJPEGAndKeepsOriginal(t *testing.T) {
	page := &fakePage{loggedIn: true}
	p := testProvider(t, page)
	caps := compressionCaps()
	caps.MaxFiles = 4 // Must NOT block >4 images anymore

	// Create 10 wide PNG images (>2048px edge) in the Vault and attach all 10
	bigPNG := func() []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 2400, 1200))
		img.Set(10, 10, color.NRGBA{R: 220, G: 80, B: 40, A: 180}) // semi-transparent PNG
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}()

	var refs []string
	for idx := range 10 {
		u, err := p.media.Put(fmt.Sprintf("flow-img-big-%02d", idx), "image/png", ".png", bigPNG)
		if err != nil {
			t.Fatal(err)
		}
		imp := p.ImportAssetFromMedia(MuseMediaAssetImportRequest{
			SourceURL: u, Project: "P1", Type: MuseTypeProp, Tag: fmt.Sprintf("prop_%02d", idx),
			Role: MuseRoleProp, Primary: true,
		})
		if !imp.Success {
			t.Fatalf("import media: %s", imp.Error)
		}
		// Ensure original vault PNG stayed 2400x1200 PNG on disk
		vaultAbs, err := p.media.VaultPath(imp.Data.Items[0].URL)
		if err != nil {
			t.Fatal(err)
		}
		cfg, fmtName, err := func() (image.Config, string, error) {
			f, err := os.Open(vaultAbs)
			if err != nil {
				return image.Config{}, "", err
			}
			defer f.Close()
			return image.DecodeConfig(f)
		}()
		if err != nil || fmtName != "png" || cfg.Width != 2400 {
			t.Fatalf("vault original modified unexpectedly: fmt=%s w=%d err=%v", fmtName, cfg.Width, err)
		}
		refs = append(refs, imp.Data.Items[0].URL)
	}

	// Verify 8 MB inline ceiling is advertised and attachments convert to <=2048px JPEG
	if p.GetOptions().Data.InlineBudgetBytes != 8<<20 {
		t.Fatalf("budget = %d, want 8MB", p.GetOptions().Data.InlineBudgetBytes)
	}
	atts, err := p.attach(MuseJob{Kind: MuseJobVideo, Prompt: "test 10 images", References: refs}, caps)
	if err != nil {
		t.Fatalf("attach 10 images failed despite 8MB budget: %v", err)
	}
	if len(atts) != 10 {
		t.Fatalf("atts len = %d, want 10", len(atts))
	}
	for i, att := range atts {
		if att.MimeType != "image/jpeg" || !strings.HasSuffix(att.Filename, ".jpg") {
			t.Fatalf("att[%d] not normalized to jpg: %+v", i, att)
		}
		raw, err := base64.StdEncoding.DecodeString(att.DataBase64)
		if err != nil {
			t.Fatal(err)
		}
		cfg, fmtName, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || fmtName != "jpeg" || max(cfg.Width, cfg.Height) > 2048 {
			t.Fatalf("att[%d] decoded = fmt:%s %dx%d err:%v", i, fmtName, cfg.Width, cfg.Height, err)
		}
	}
}

func TestVaultDirectoryMigrationMovesExistingAssetsSafely(t *testing.T) {
	p := testProvider(t, &fakePage{})
	u, _ := p.media.Put(assetPrefix+"m", "image/png", ".png", testPNGBytes(t, 16, 16))
	saved := p.SaveAsset(MuseAsset{
		Project: "OLIVA", Type: MuseTypeChar, Tag: "jimena", Role: MuseRoleCharacter,
		Items: []MuseAssetItem{{URL: u, Angle: "front", Primary: true}},
	})
	if !saved.Success {
		t.Fatal(saved.Error)
	}
	oldAbs, err := p.media.OpenPath(saved.Data.Items[0].URL)
	if err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(resolvedTempDir(t), "MuseAssetsCustom")
	res := p.SetAssetVaultDirectory(newDir, true)
	if !res.Success {
		t.Fatalf("SetAssetVaultDirectory failed: %s", res.Error)
	}
	if _, err := os.Stat(oldAbs); !os.IsNotExist(err) {
		t.Fatalf("expected old vault file moved out, stat err = %v", err)
	}
	movedAbs, err := p.media.OpenPath(saved.Data.Items[0].URL)
	if err != nil || !strings.HasPrefix(strings.ToLower(movedAbs), strings.ToLower(newDir)) {
		t.Fatalf("movedAbs = %q, want under %q (err=%v)", movedAbs, newDir, err)
	}
	if _, err := os.Stat(movedAbs); err != nil {
		t.Fatalf("moved file missing at new vault dir: %v", err)
	}
}

func TestVaultMigrationRefusesExistingDestination(t *testing.T) {
	p := testProvider(t, &fakePage{})
	url, err := p.media.Put(assetPrefix+"migration", "image/png", ".png", testPNGBytes(t, 16, 16))
	if err != nil {
		t.Fatal(err)
	}
	saved := p.SaveAsset(MuseAsset{
		Project: "OLIVA", Type: MuseTypeChar, Tag: "jimena", Role: MuseRoleCharacter,
		Items: []MuseAssetItem{{URL: url, Angle: "front", Primary: true}},
	})
	if !saved.Success {
		t.Fatal(saved.Error)
	}
	oldRoot := p.vaultDir()
	rel := saved.Data.Items[0].VaultRelPath
	dest := t.TempDir()
	collision := filepath.Join(dest, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(collision), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "existing destination file"
	if err := os.WriteFile(collision, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := p.SetAssetVaultDirectory(dest, true); res.Success {
		t.Fatal("migration accepted a collision")
	}
	if got, err := os.ReadFile(collision); err != nil || string(got) != original {
		t.Fatalf("destination changed: %q, %v", got, err)
	}
	if p.vaultDir() != oldRoot {
		t.Fatal("failed migration changed the vault root")
	}
	if _, err := os.Stat(filepath.Join(oldRoot, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

func TestVaultMigrationRejectsNestedAndTraversalAndPreservesSameFile(t *testing.T) {
	p := testProvider(t, &fakePage{})
	u, err := p.media.Put(assetPrefix+"safe", "image/png", ".png", testPNGBytes(t, 16, 16))
	if err != nil {
		t.Fatal(err)
	}
	saved := p.SaveAsset(MuseAsset{
		Project: "OLIVA", Type: MuseTypeChar, Tag: "hero", Role: MuseRoleCharacter,
		Items: []MuseAssetItem{{URL: u, Angle: "front", Primary: true}},
	})
	if !saved.Success {
		t.Fatal(saved.Error)
	}
	oldRoot := p.vaultDir()
	rel := saved.Data.Items[0].VaultRelPath
	oldAbs := filepath.Join(oldRoot, filepath.FromSlash(rel))

	// 1. Nested destination inside current vault must be refused.
	nested := filepath.Join(oldRoot, "sub-vault")
	if res := p.SetAssetVaultDirectory(nested, true); res.Success {
		t.Fatal("migration allowed nested target inside old vault directory")
	}

	// 2. Hardlinked destination pointing to the exact same physical file (os.SameFile == true)
	// must NOT delete the source file.
	hardlinkDest := filepath.Join(resolvedTempDir(t), "HardlinkDest")
	dstFile := filepath.Join(hardlinkDest, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dstFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(oldAbs, dstFile); err == nil {
		res := p.SetAssetVaultDirectory(hardlinkDest, true)
		if !res.Success {
			t.Fatalf("migration with same-file destination failed: %s", res.Error)
		}
		if _, statErr := os.Stat(dstFile); statErr != nil {
			t.Fatalf("destination file missing after same-file migration: %v", statErr)
		}
		if _, statErr := os.Stat(oldAbs); statErr != nil {
			t.Fatalf("same-file source was deleted during migration: %v", statErr)
		}
	}

	// 3. Corrupted VaultRelPath with traversal ("../") must be refused and never touch outside files.
	outsideFile := filepath.Join(resolvedTempDir(t), "outside.png")
	if err := os.WriteFile(outsideFile, testPNGBytes(t, 8, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = p.assets.Update(saved.Data.ID, func(a *MuseAsset) {
		a.Items[0].VaultRelPath = "../outside.png"
	})
	otherDest := filepath.Join(resolvedTempDir(t), "OtherDest")
	if res := p.SetAssetVaultDirectory(otherDest, true); res.Success {
		t.Fatal("migration accepted traversal VaultRelPath")
	}
	if _, statErr := os.Stat(outsideFile); statErr != nil {
		t.Fatalf("outside file was modified/removed by traversal migration: %v", statErr)
	}
}
