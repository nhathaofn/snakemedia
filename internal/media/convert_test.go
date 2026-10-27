package media

import (
	"encoding/base64"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// tinyWebP is a 4×3 half-transparent lossless WebP, the format Muse makes.
const tinyWebP = "UklGRh4AAABXRUJQVlA4TBEAAAAvA4AAEAdQjyLXo4CBiOh/AAA="

func decodeFile(t *testing.T, path string) (image.Image, string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, kind, err := image.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return img, kind
}

// A provider's images are saved in the format it asks for; other files, and
// the "original" choice, are copied as they are.
func TestSaveConvertsImagesByPrefix(t *testing.T) {
	store := NewStore(t.TempDir())
	dir := t.TempDir()
	store.AllowRoot(dir)
	format := "png"
	store.ConvertImages("muse-", func() string { return format })
	data, _ := base64.StdEncoding.DecodeString(tinyWebP)
	muse, err := store.Put("muse-a", "image/webp", ".webp", data)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := store.Put("flow-a", "image/webp", ".webp", data)

	for _, tc := range []struct{ format, ext, kind string }{{"png", ".png", "png"}, {"jpg", ".jpg", "jpeg"}, {"original", ".webp", "webp"}} {
		format = tc.format
		saved, err := store.SaveTo(muse, dir, "1-snakemedia-muse-"+tc.format)
		if err != nil {
			t.Fatal(err)
		}
		img, kind := decodeFile(t, saved)
		if filepath.Ext(saved) != tc.ext || kind != tc.kind || img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
			t.Fatalf("%s: saved %s as %s %v", tc.format, saved, kind, img.Bounds())
		}
	}
	format = "jpg"
	if saved, err := store.SaveTo(other, dir, "2-snakemedia-flow"); err != nil || filepath.Ext(saved) != ".webp" {
		t.Fatalf("other provider: %s %v", saved, err)
	}
}

// A file that only looks like an image is copied rather than lost.
func TestSaveKeepsUndecodableImages(t *testing.T) {
	store := NewStore(t.TempDir())
	dir := t.TempDir()
	store.AllowRoot(dir)
	store.ConvertImages("muse-", func() string { return "png" })
	broken, _ := store.Put("muse-b", "image/webp", ".webp", []byte("not an image"))
	saved, err := store.SaveTo(broken, dir, "1-snakemedia-muse")
	if err != nil || filepath.Ext(saved) != ".webp" {
		t.Fatalf("saved %s %v", saved, err)
	}
	if got, _ := os.ReadFile(saved); string(got) != "not an image" {
		t.Fatalf("content %q", got)
	}
}
