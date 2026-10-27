package media

import (
	"context"
	"encoding/json"
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
	if res, ok := out.(SaveNodeResult); err != nil || !ok || len(res.Paths) != 1 || res.Paths[0] != filepath.Join(dir, "img.png") {
		t.Fatalf("unexpected result %+v %v", out, err)
	}
	if _, err := store.SaveNode().Run(context.Background(), json.RawMessage(`{"media":[{"localUrl":"`+local+`","kind":"image"}]}`)); err == nil {
		t.Fatal("a save without a folder must fail")
	}
}
