package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExists(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	if !FileExists(tmpFile.Name()) {
		t.Errorf("Expected file %s to exist, but got false", tmpFile.Name())
	}

	nonExistentPath := tmpFile.Name() + ".nonexistent"
	if FileExists(nonExistentPath) {
		t.Errorf("Expected path %s to not exist, but got true", nonExistentPath)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "data.json")

	for _, content := range []string{"first", "second"} {
		if err := WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFileAtomic(%q) failed: %v", content, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != content {
			t.Fatalf("ReadFile = %q, %v; want %q", got, err, content)
		}
	}

	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("expected only the target file, found %d entries (temp file leaked?)", len(entries))
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		got := FormatBytes(tt.bytes)
		if got != tt.expected {
			t.Errorf("FormatBytes(%d) = %s; want %s", tt.bytes, got, tt.expected)
		}
	}
}
