package paths

import (
	"path/filepath"
	"testing"
)

// A test binary must never resolve the installed release's data folder.
func TestResolveUsesDevFolderInTests(t *testing.T) {
	p, err := Resolve("SnakeMedia")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(p.DataDir), "SnakeMedia-dev"; got != want {
		t.Fatalf("data folder = %q, want %q", got, want)
	}
	if filepath.Base(filepath.Dir(p.ToolsDir)) != "SnakeMedia" {
		t.Fatalf("tools folder should stay shared, got %q", p.ToolsDir)
	}
}
