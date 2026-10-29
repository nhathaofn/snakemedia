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
