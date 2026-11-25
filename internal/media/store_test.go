package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/models"
)

func TestPutUsesContentTypeExtension(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, tc := range []struct{ id, contentType, fallback, want string }{
		{"img-1", "image/jpeg", ".png", Route + "img-1.jpg"},
		{"vid/../1", "application/octet-stream", ".mp4", Route + "vid_1.mp4"},
		{"vid-2", "video/mp4; codecs=avc1", ".webm", Route + "vid-2.mp4"},
	} {
		got, err := store.Put(tc.id, tc.contentType, tc.fallback, []byte("DATA"))
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q %v, want %q", tc.id, got, err, tc.want)
		}
	}
	data, err := os.ReadFile(filepath.Join(store.Dir(), "img-1.jpg"))
	if err != nil || string(data) != "DATA" {
		t.Fatalf("stored bytes differ: %q %v", data, err)
	}
}

func TestSaveCopiesIntoChosenFolder(t *testing.T) {
	store := NewStore(t.TempDir())
	local, err := store.Put("clip", "video/mp4", ".mp4", []byte("MP4"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var defaults []string
	choose := func(defaultDir string) (string, error) {
		defaults = append(defaults, defaultDir)
		return dir, nil
	}
	first := store.Save(local, choose)
	second := store.Save(local, choose)
	if !first.Success || !second.Success {
		t.Fatalf("save failed: %+v %+v", first, second)
	}
	firstName, secondName := filepath.Base(first.Data.Path), filepath.Base(second.Data.Path)
	if !savedFile.MatchString(firstName) || !strings.HasPrefix(firstName, "1-snakemedia-flow-") || !strings.HasPrefix(secondName, "2-snakemedia-flow-") {
		t.Fatalf("saved as %q and %q", firstName, secondName)
	}
	if len(defaults) != 2 || defaults[0] != "" || defaults[1] != dir {
		t.Fatalf("dialog did not start at the last folder: %v", defaults)
	}
	canceled := store.Save(local, func(string) (string, error) { return "", nil })
	if !canceled.Success || !canceled.Data.Canceled {
		t.Fatalf("closing the dialog must not be an error: %+v", canceled)
	}
	for _, bad := range []string{"clip.mp4", Route + "../accounts.json", Route, Route + "missing.mp4"} {
		res := store.Save(bad, choose)
		if res.Success || (res.Code != models.CodeInvalidArgument && res.Code != models.CodeNotFound) {
			t.Errorf("%q: saved from outside the media folder: %+v", bad, res)
		}
		if strings.Contains(bad, "..") && res.Code != models.CodeInvalidArgument {
			t.Errorf("%q: path traversal must be rejected as invalid", bad)
		}
	}
}

func TestMiddlewareServesVideosBeforeAssets(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Put("clip", "video/mp4", ".mp4", []byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	assetsCalled := 0
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assetsCalled++
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, Route+"clip.mp4", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("video not served for seeking: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	for _, path := range []string{Route, Route + "../config.json"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s must not be served", path)
		}
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if assetsCalled != 1 || rec.Code != http.StatusTeapot {
		t.Fatal("other paths must reach the asset server")
	}
}

// videoExtensions stands for a provider's upload whitelist.
var videoExtensions = []string{".mov", ".mp4"}

func TestImportVideoCopiesIntoStore(t *testing.T) {
	store := NewStore(t.TempDir())
	src := filepath.Join(t.TempDir(), "My Clip.MOV")
	if err := os.WriteFile(src, []byte("MOVDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := store.ImportVideo(videoExtensions, func() (string, error) { return src, nil })
	if !res.Success || res.Data.FileName != "My Clip.MOV" || res.Data.Size != 7 || res.Data.ContentType != "video/quicktime" {
		t.Fatalf("unexpected result %+v", res)
	}
	path, err := store.Path(res.Data.LocalURL)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "MOVDATA" {
		t.Fatalf("copied bytes differ: %q %v", data, err)
	}
	if res := store.ImportVideo(videoExtensions, func() (string, error) { return "", nil }); !res.Success || !res.Data.Canceled {
		t.Fatalf("closing the dialog must not be an error: %+v", res)
	}
	bad := filepath.Join(t.TempDir(), "notes.txt")
	_ = os.WriteFile(bad, []byte("x"), 0o600)
	if res := store.ImportVideo(videoExtensions, func() (string, error) { return bad, nil }); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("a non-video file must be refused: %+v", res)
	}
}

// Only folders the user chose (or the app's own) take files or reveal them;
// a link inside a root cannot lead out of it.
func TestRootsLimitFoldersAndFiles(t *testing.T) {
	store := NewStore(t.TempDir())
	root, outside := t.TempDir(), t.TempDir()
	store.AllowRoot(root)
	if _, err := store.CheckDir(filepath.Join(root, "batch", "a")); err != nil {
		t.Fatalf("a folder below a root was refused: %v", err)
	}
	if _, err := store.CheckDir(filepath.Join(root, "..")); err == nil {
		t.Fatal("the parent of a root was accepted")
	}
	if _, err := store.CheckDir(outside); err == nil {
		t.Fatal("a folder nobody chose was accepted")
	}
	local, err := store.Put("img", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Path(local)
	if _, err := store.CheckFile(stored); err != nil {
		t.Fatalf("a stored media file was refused: %v", err)
	}
	if _, err := store.CheckFile(filepath.Join(outside, "x.png")); err == nil {
		t.Fatal("a file outside every root was accepted")
	}
	settings := outside
	store.AllowRootsFrom(func() []string { return []string{settings} })
	if _, err := store.CheckDir(outside); err != nil {
		t.Fatalf("a folder from settings was refused: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(t.TempDir(), link); err == nil {
		if _, err := store.CheckDir(link); err == nil {
			t.Fatal("a link inside a root led outside it")
		}
	}
}

// SaveTo streams a stored file into an allowed folder it creates, numbers a
// taken name instead of replacing it, and refuses folders outside the roots
// and names that are paths.
func TestSaveToNumbersNamesAndKeepsRoots(t *testing.T) {
	store := NewStore(t.TempDir())
	local, err := store.Put("clip", "video/mp4", ".mp4", []byte("MP4"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store.AllowRoot(root)
	dir := filepath.Join(root, "2026-10-27")

	first, err := store.SaveTo(local, dir, "mèo-1")
	if err != nil || filepath.Base(first) != "mèo-1.mp4" {
		t.Fatalf("first save = %q %v", first, err)
	}
	second, err := store.SaveTo(local, dir, "mèo-1")
	if err != nil || filepath.Base(second) != "mèo-1-2.mp4" {
		t.Fatalf("second save = %q %v", second, err)
	}
	if data, err := os.ReadFile(second); err != nil || string(data) != "MP4" {
		t.Fatalf("copied bytes = %q %v", data, err)
	}
	if _, err := store.SaveTo(local, t.TempDir(), "x"); err == nil {
		t.Fatal("saved outside the allowed folders")
	}
	if _, err := store.SaveTo(local, dir, "../x"); err == nil {
		t.Fatal("a name with a path was accepted")
	}
}

func TestRemoveDeletesStoredFileAndCachedThumbs(t *testing.T) {
	store := NewStore(t.TempDir())
	local, err := store.Put("muse-asset-1", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	thumb := filepath.Join(store.Dir(), thumbDir, "muse-asset-1-3-123.jpg")
	if err := os.MkdirAll(filepath.Dir(thumb), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumb, []byte("JPG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(local); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	path, _ := store.Path(local)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stored file still exists: %v", err)
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Fatalf("cached thumb still exists: %v", err)
	}
	if err := store.Remove(local); err != nil {
		t.Fatalf("Remove missing must succeed: %v", err)
	}
	if err := store.Remove(Route + "../evil.png"); err == nil {
		t.Fatal("Remove accepted path traversal")
	}
}

func TestP0SecurityAndBatchFolderFixes(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	srcURL, err := store.Put("img", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	// I-3: Save registers user-chosen folder via AllowRoot so CheckFile succeeds.
	outDir := filepath.Join(t.TempDir(), "user-out")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	res := store.Save(srcURL, func(string) (string, error) { return outDir, nil })
	if !res.Success || res.Data.Path == "" {
		t.Fatalf("Save failed: %+v", res)
	}
	if _, err := store.CheckFile(res.Data.Path); err != nil {
		t.Fatalf("CheckFile failed on just-saved path %q: %v", res.Data.Path, err)
	}

	// I-5a: Middleware blocks hidden .thumbs directory under /flow-media/.
	leakThumb := filepath.Join(dir, thumbDir, "leak.txt")
	if err := os.MkdirAll(filepath.Dir(leakThumb), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leakThumb, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, Route+".thumbs/leak.txt", nil)
	store.Middleware(http.NotFoundHandler()).ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("Middleware served hidden thumb file with status %d", rr.Code)
	}

	// I-5b: Remove("img.png") must not delete thumbnail belonging to "img-2-100-1.jpg".
	neighborThumb := filepath.Join(dir, thumbDir, "img-2-100-1.jpg")
	if err := os.WriteFile(neighborThumb, []byte("thumb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(srcURL); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(neighborThumb); err != nil {
		t.Fatalf("Remove deleted neighbor thumb: %v", err)
	}

	// I-5c: SaveTo rejects Windows alternate data stream colon and illegal chars.
	src2, err := store.Put("img2", "image/png", ".png", []byte("PNG2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTo(src2, outDir, "foo:bar"); err == nil {
		t.Fatal("SaveTo allowed colon/ADS in filename")
	}
	if _, err := store.SaveTo(src2, outDir, "bad\x01name"); err == nil {
		t.Fatal("SaveTo allowed control character in filename")
	}

	// I-6: BatchFolder joins open batch despite trailing separator or Windows case differences.
	parent := filepath.Join(t.TempDir(), "Outputs")
	openDir := filepath.Join(parent, "snakemedia-muse-image-20261123-100000")
	items := []BatchItem{{Dir: openDir, Index: 1, CreatedAt: "2026-11-23T10:00:00Z", Open: true}}
	altParent := strings.ToUpper(parent) + string(filepath.Separator)
	gotDir, gotIndex := BatchFolder(altParent, "snakemedia-muse-image", time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC), items)
	if !strings.EqualFold(filepath.Clean(gotDir), filepath.Clean(openDir)) || gotIndex != 2 {
		t.Fatalf("BatchFolder = (%q, %d), want (%q, 2)", gotDir, gotIndex, openDir)
	}
}
