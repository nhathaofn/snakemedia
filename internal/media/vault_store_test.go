package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func vaultTestPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 640))
	img.Set(0, 0, color.NRGBA{R: 200, G: 40, B: 40, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestVaultRouteServesImagesAndRejectsTraversal(t *testing.T) {
	store := NewStore(t.TempDir())
	vault := t.TempDir()
	thumbs := filepath.Join(t.TempDir(), "vault-thumbs")
	store.SetVaultDir(func() string { return vault }, thumbs)

	imgDir := filepath.Join(vault, "OLIVA")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(imgDir, "OLIVA_CHAR_jimena_front.png")
	if err := os.WriteFile(imgPath, vaultTestPNG(), 0o644); err != nil {
		t.Fatal(err)
	}

	relURL := VaultURL("OLIVA/OLIVA_CHAR_jimena_front.png")
	gotPath, err := store.OpenPath(relURL)
	if err != nil || !strings.EqualFold(filepath.Clean(gotPath), filepath.Clean(imgPath)) {
		t.Fatalf("OpenPath(%q) = %q, err=%v", relURL, gotPath, err)
	}

	// Rejects parent traversal outside VaultDir
	if _, err := store.VaultPath(VaultRoute + "../secret.png"); err == nil {
		t.Fatal("expected traversal error")
	}

	// Middleware serves vault files and caches thumb inside thumbs cache (not inside vault folder)
	mw := store.Middleware(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, relURL+"?"+ThumbQuery+"=1", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("vault thumb response = %d len=%d", rec.Code, rec.Body.Len())
	}
	entries, err := os.ReadDir(imgDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("vault folder polluted by thumbs: %+v (err=%v)", entries, err)
	}
	if _, err := os.Stat(thumbs); err != nil {
		t.Fatalf("expected thumb cache inside AppData dir: %v", err)
	}
}
