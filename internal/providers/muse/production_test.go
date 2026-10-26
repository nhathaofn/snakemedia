package muse

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptAndBoard(t *testing.T) {
	scenes := []SceneItem{
		{Text: "Cô gái mỉm cười", Voiceover: "Chào các bạn"},
		{Text: "Cận cảnh bàn tay đang cầm điện thoại", Voiceover: "Hôm nay mình giới thiệu siêu phẩm"},
	}
	cfg := ScriptConfig{
		Prefix:   "Tạo video chân thực",
		Ratio:    "9:16",
		Duration: "15s",
		Voice:    "Chipper Tambourine",
		Host:     "nam",
	}

	p := BuildPrompt(scenes, cfg, nil, "Người dẫn: Nam")
	if !strings.Contains(p, "Cảnh 1") || !strings.Contains(p, "Cảnh 2") {
		t.Fatalf("BuildPrompt missing scenes: %s", p)
	}
	if !strings.Contains(p, "Lời: \"Chào các bạn\"") {
		t.Fatalf("BuildPrompt missing voiceover: %s", p)
	}
	if !strings.Contains(p, "Chipper Tambourine") {
		t.Fatalf("BuildPrompt missing voice name: %s", p)
	}
	if !strings.Contains(p, Pace) {
		t.Fatalf("BuildPrompt missing PACE instruction: %s", p)
	}

	// Test self voice
	cfgSelf := cfg
	cfgSelf.Voice = SelfVoice
	pSelf := BuildPrompt(scenes, cfgSelf, nil, "")
	if !strings.Contains(pSelf, "Nhân vật nói vào máy: \"Chào các bạn\"") {
		t.Fatalf("SelfVoice prompt mismatch: %s", pSelf)
	}
	if !strings.Contains(pSelf, "khẩu hình khớp từng lời (lip-sync)") {
		t.Fatalf("SelfVoice missing lip-sync instruction: %s", pSelf)
	}

	// Test Board prompt
	boardP := BuildBoardPrompt(scenes, cfg, nil, "Người dẫn: Nam")
	if !strings.Contains(boardP, "Tạo 2 ảnh storyboard tỉ lệ 9:16") {
		t.Fatalf("Board prompt header mismatch: %s", boardP)
	}
	if !strings.Contains(boardP, "Ảnh 1: Cô gái mỉm cười") {
		t.Fatalf("Board prompt missing scene 1: %s", boardP)
	}

	// Test Board video prompt
	bVideoP := BoardVideoPrompt(scenes, cfg, 2, true, "")
	if !strings.Contains(bVideoP, "Dùng đúng 2 ảnh storyboard vừa tạo (gửi kèm lại)") {
		t.Fatalf("BoardVideoPrompt prefix mismatch: %s", bVideoP)
	}
}

func TestProductionStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "productions.json")
	store := newProductionStore(path)

	items := store.list()
	if len(items) != 0 {
		t.Fatalf("expected empty initially, got %d", len(items))
	}

	prod := Production{
		Title: "Test Video",
		Scenes: []SceneItem{
			{Text: "Cảnh 1"},
		},
		Status: ProductionStatusQueued,
	}

	if err := store.add(prod); err != nil {
		t.Fatalf("add production failed: %v", err)
	}

	list := store.list()
	if len(list) != 1 || list[0].Title != "Test Video" {
		t.Fatalf("production not stored correctly: %v", list)
	}

	id := list[0].ID
	updated, err := store.update(id, func(p *Production) {
		p.Status = ProductionStatusRunning
		p.Step = "Đang vẽ board"
	})
	if err != nil || updated.Status != ProductionStatusRunning {
		t.Fatalf("update failed: %v", err)
	}

	if err := store.remove(id); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if len(store.list()) != 0 {
		t.Fatalf("production not removed")
	}
}
