package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageSizeReadsWebPHeaders(t *testing.T) {
	// VP8X canvas 1152×1792 (stored as size - 1, 24-bit little endian).
	vp8x := []byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00\x7f\x04\x00\xff\x06\x00")
	file := filepath.Join(t.TempDir(), "a.webp")
	if err := os.WriteFile(file, vp8x, 0o600); err != nil {
		t.Fatal(err)
	}
	if w, h, err := ImageSize(file); err != nil || w != 1152 || h != 1792 {
		t.Fatalf("size %dx%d, %v", w, h, err)
	}
	if _, _, err := ImageSize(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Fatal("a missing file has a size")
	}
}
