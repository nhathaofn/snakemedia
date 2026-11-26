package media

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, store *Store, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	store.Middleware(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestThumbIsSmallJPEG(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	writePNG(t, filepath.Join(dir, "big.png"), 1200, 600)

	rec := get(t, store, Route+"big.png?"+ThumbQuery)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-cache" {
		t.Fatalf("Cache-Control = %q, want private, no-cache", cc)
	}
	img, err := jpeg.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != thumbSize || b.Dy() != thumbSize/2 {
		t.Fatalf("thumbnail is %v", b)
	}
	// Served from the cache the second time.
	if again := get(t, store, Route+"big.png?"+ThumbQuery); again.Code != http.StatusOK {
		t.Fatalf("cached thumbnail status %d", again.Code)
	}
}

func TestThumbFallsBackToOriginal(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	writePNG(t, filepath.Join(dir, "small.png"), 100, 100)

	rec := get(t, store, Route+"small.png?"+ThumbQuery)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("small image: status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(t, store, Route+"..%5Csecret.png?"+ThumbQuery); rec.Header().Get("Content-Type") == "image/jpeg" {
		t.Fatal("a path outside the media folder made a thumbnail")
	}
}

func TestImportPreviewLoadsOnlySelectedImageOnDemand(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	wanted := filepath.Join(dir, "wide.png")
	writePNG(t, wanted, 800, 400)
	prefix := store.ImportPreviewPrefix([]string{wanted, ""}, 1<<20)
	rec := get(t, store, prefix+"0")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status %d", rec.Code)
	}
	img, err := jpeg.Decode(rec.Body)
	if err != nil || img.Bounds().Dx() != 120 || img.Bounds().Dy() != 60 {
		t.Fatalf("expected 120x60 JPEG, got %v (%v)", img, err)
	}
	for _, url := range []string{prefix + "1", prefix + "2", prefix + "-1"} {
		if rec := get(t, store, url); rec.Code != http.StatusNotFound {
			t.Fatalf("invalid selection %q returned %d", url, rec.Code)
		}
	}
	store.ImportPreviewPrefix([]string{wanted}, 1)
	if rec := get(t, store, prefix+"0"); rec.Code != http.StatusNotFound {
		t.Fatal("old scan should be invalidated")
	}
}

func TestWithinDeduplicatesRepeatedDynamicRoots(t *testing.T) {
	store := NewStore(t.TempDir())
	allowed := t.TempDir()
	other := t.TempDir()
	store.AllowRootsFrom(func() []string {
		// Simulate 2,000 queued jobs sharing the same batch output folder.
		dup := make([]string, 0, 2001)
		for range 2000 {
			dup = append(dup, other)
		}
		dup = append(dup, allowed)
		return dup
	})
	if _, err := store.CheckDir(filepath.Join(allowed, "sub")); err != nil {
		t.Fatalf("CheckDir inside allowed root failed: %v", err)
	}
	if _, err := store.CheckDir(filepath.Join(t.TempDir(), "outside")); err == nil {
		t.Fatal("CheckDir outside allowed roots should fail")
	}
}
