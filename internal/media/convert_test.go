package media

import (
	"encoding/base64"
	"image"
	"image/color"
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

// Overlapping prefixes always pick the longest one.
func TestSaveFormatPicksLongestPrefix(t *testing.T) {
	store := NewStore(t.TempDir())
	store.ConvertImages("muse-", func() string { return "png" })
	store.ConvertImages("muse-video-", func() string { return "jpg" })
	store.ConvertImages("m", func() string { return "original" })
	for range 20 {
		if got := store.saveFormat("muse-video-a.webp"); got != ".jpg" {
			t.Fatalf("saveFormat = %q", got)
		}
		if got := store.saveFormat("muse-a.webp"); got != ".png" {
			t.Fatalf("saveFormat = %q", got)
		}
	}
}

// An opaque RGBA image is used as it is; a transparent one is laid on white.
func TestOpaqueSkipsCopyOnlyWhenOpaque(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for x := range 2 {
		for y := range 2 {
			img.Set(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	if opaque(img) != image.Image(img) {
		t.Fatal("opaque image copied")
	}
	img.Set(0, 0, color.NRGBA{A: 0})
	out := opaque(img)
	if out == image.Image(img) {
		t.Fatal("transparent image not flattened")
	}
	if r, g, b, _ := out.At(0, 0).RGBA(); r != 0xffff || g != 0xffff || b != 0xffff {
		t.Fatal("transparent pixel not white")
	}
}
