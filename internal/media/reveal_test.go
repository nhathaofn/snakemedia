package media

import (
	"os"
	"path/filepath"
	"testing"

	"SnakeMedia/internal/models"
)

// Reveal opens only files the app stored or saved in its own folders.
func TestRevealRefusesFilesOutsideAppFolders(t *testing.T) {
	store := NewStore(t.TempDir())
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := store.Reveal(outside); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("reveal outside = %+v", res)
	}
	root := t.TempDir()
	store.AllowRoot(root)
	if res := store.Reveal(filepath.Join(root, "gone.png")); res.Success || res.Code != models.CodeNotFound {
		t.Fatalf("reveal missing file = %+v", res)
	}
}
