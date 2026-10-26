package muse

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFindAssetsAndUntag(t *testing.T) {
	assets := []StudioAsset{
		{ID: "1", Tag: "mai", Name: "Nhân vật Mai", Role: RoleCharacterFace, Images: []string{"mai.png"}},
		{ID: "2", Tag: "ao_polo", Name: "Áo Polo Xanh", Role: RoleProduct, Images: []string{"polo.png"}},
	}

	prompt := "Video người mẫu @mai mặc chiếc áo @ao_polo đi dạo"
	matched, missing := findAssets(prompt, assets)
	if len(missing) > 0 {
		t.Fatalf("unexpected missing tags: %v", missing)
	}
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched assets, got %d", len(matched))
	}
	if matched[0].Tag != "mai" || matched[1].Tag != "ao_polo" {
		t.Errorf("matched order wrong: got %s, %s", matched[0].Tag, matched[1].Tag)
	}

	untagged := untag(prompt, assets)
	expected := "Video người mẫu mai mặc chiếc áo ao_polo đi dạo"
	if untagged != expected {
		t.Errorf("untag = %q, want %q", untagged, expected)
	}

	// Missing tag
	_, missing2 := findAssets("Mặc đồ @khong_co_tag", assets)
	if len(missing2) != 1 || missing2[0] != "khong_co_tag" {
		t.Errorf("expected missing tag 'khong_co_tag', got %v", missing2)
	}
}

func TestComposeImages(t *testing.T) {
	studio := StudioData{
		ActiveProject: "default",
		ModelConfig: StudioModelConfig{
			On:     true,
			Images: []string{"model1.png"},
		},
		RotateConfig: StudioRotateConfig{
			On:     true,
			Images: []string{"rotateA.png", "rotateB.png"},
			Index:  0,
		},
		Assets: []StudioAsset{
			{ID: "1", Tag: "mai", Role: RoleCharacterFace, Images: []string{"mai.png"}, LockNotes: "mắt nâu"},
			{ID: "2", Tag: "ao", Role: RoleProduct, Images: []string{"ao.png"}},
		},
	}

	prompt := "Chụp ảnh @mai mặc @ao"
	batchRefs := []string{"batch1.png", "batch2.png"}
	res, err := composeImages(prompt, 0, studio, batchRefs, false, 8)
	if err != nil {
		t.Fatalf("composeImages error: %v", err)
	}

	// Order: Model (model1.png) -> @mai (mai.png) -> @ao (ao.png) -> rotate (rotateA.png) -> batchRefs (batch1, batch2)
	wantImages := []string{"model1.png", "mai.png", "ao.png", "rotateA.png", "batch1.png", "batch2.png"}
	if len(res.Images) != len(wantImages) {
		t.Fatalf("res.Images length = %d, want %d: %v", len(res.Images), len(wantImages), res.Images)
	}
	for i, img := range wantImages {
		if res.Images[i] != img {
			t.Errorf("res.Images[%d] = %q, want %q", i, res.Images[i], img)
		}
	}

	if !strings.Contains(res.Manifest, "=== REFERENCE ROLE & PRIORITY PROTOCOL") {
		t.Errorf("manifest missing header: %s", res.Manifest)
	}
	if !strings.Contains(res.Manifest, "- @mai lock: mắt nâu") {
		t.Errorf("manifest missing lock notes: %s", res.Manifest)
	}
	if !strings.Contains(res.Manifest, "PRODUCT: @ao > storyboard > prompt text") {
		t.Errorf("manifest missing product priority: %s", res.Manifest)
	}
}

func TestStudioStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "studio.json")
	s := newStudioStore(path)

	initial := s.get()
	if initial.ActiveProject != "default" {
		t.Fatalf("expected default project, got %s", initial.ActiveProject)
	}

	asset, err := s.addAsset(StudioAsset{
		Tag:    "test_tag",
		Name:   "Test Asset",
		Role:   RoleProduct,
		Images: []string{"test.jpg"},
	})
	if err != nil {
		t.Fatalf("addAsset error: %v", err)
	}
	if asset.ID == "" {
		t.Fatalf("expected non-empty asset ID")
	}

	cur := s.get()
	if len(cur.Assets) != 1 || cur.Assets[0].Tag != "test_tag" {
		t.Fatalf("asset not stored: %v", cur.Assets)
	}

	if err := s.removeAsset(asset.ID); err != nil {
		t.Fatalf("removeAsset error: %v", err)
	}
	cur2 := s.get()
	if len(cur2.Assets) != 0 {
		t.Fatalf("asset not removed")
	}
}
