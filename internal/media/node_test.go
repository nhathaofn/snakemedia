package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/engine"
)

func TestSaveNodeCopiesIntoConfiguredFolder(t *testing.T) {
	store := NewStore(t.TempDir())
	local, err := store.Put("img", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input, _ := json.Marshal(SaveRequest{Media: []engine.MediaRef{engine.RefFor(local, engine.MediaImage)}, Dir: dir})
	if _, err := store.SaveNode().Run(context.Background(), input); err == nil {
		t.Fatal("a node saved into a folder nobody chose")
	}
	store.AllowRoot(dir)
	out, err := store.SaveNode().Run(context.Background(), input)
	// Saved paths are in cleaned form (links and 8.3 names resolved).
	want, _ := cleanDir(dir)
	if res, ok := out.(SaveNodeResult); err != nil || !ok || len(res.Paths) != 1 || filepath.Dir(res.Paths[0]) != want || !savedFile.MatchString(filepath.Base(res.Paths[0])) {
		t.Fatalf("unexpected result %+v %v", out, err)
	}
	if _, err := store.SaveNode().Run(context.Background(), json.RawMessage(`{"media":[{"localUrl":"`+local+`","kind":"image"}]}`)); err == nil {
		t.Fatal("a save without a folder must fail")
	}
}

func TestGraphSaveReusesAnIdenticalPublishedCopy(t *testing.T) {
	store := NewStore(t.TempDir())
	local, err := store.Put("img", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store.AllowRoot(dir)
	input, err := json.Marshal(SaveRequest{Media: []engine.MediaRef{engine.RefFor(local, engine.MediaImage)}, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	node := store.SaveNode()
	ctx := engine.WithRunKey(context.Background(), "run-123/save")
	first, err := node.Run(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := node.Run(ctx, input)
	if err != nil || second.(SaveNodeResult).Paths[0] != first.(SaveNodeResult).Paths[0] {
		t.Fatalf("repeated graph save returned a different path: %v / %v (%v)", first, second, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("save produced extra files: %v / %v", entries, err)
	}
	path := first.(SaveNodeResult).Paths[0]
	if err := os.WriteFile(path, []byte("different user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := node.Run(ctx, input); err == nil {
		t.Fatal("existing file with different contents was accepted")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "different user file" {
		t.Fatalf("existing file changed: %q / %v", data, err)
	}
}
