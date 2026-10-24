package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SnakeMedia/internal/models"
	"github.com/google/uuid"
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
	if first.Data.Path != filepath.Join(dir, "clip.mp4") || second.Data.Path != filepath.Join(dir, "clip-2.mp4") {
		t.Fatalf("existing files must not be replaced: %q %q", first.Data.Path, second.Data.Path)
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

func TestSaveImageToTaskFolderKeepsBatchImagesTogether(t *testing.T) {
	store := NewStore(t.TempDir())
	firstImage, err := store.Put("image-one", "image/png", ".png", []byte("PNG-1"))
	if err != nil {
		t.Fatal(err)
	}
	secondImage, err := store.Put("image-two", "image/png", ".png", []byte("PNG-2"))
	if err != nil {
		t.Fatal(err)
	}
	outputDir := t.TempDir()
	taskID := uuid.NewString()

	first := store.SaveImageToTaskFolder(firstImage, outputDir, taskID)
	second := store.SaveImageToTaskFolder(secondImage, outputDir, taskID)
	if !first.Success || !second.Success {
		t.Fatalf("could not save task images: %+v %+v", first, second)
	}
	firstDir := filepath.Dir(first.Data.Path)
	if firstDir != filepath.Dir(second.Data.Path) || filepath.Base(firstDir) != "task-"+taskID {
		t.Fatalf("images from one batch must share its task folder: %q %q", first.Data.Path, second.Data.Path)
	}
	for path, expected := range map[string]string{first.Data.Path: "PNG-1", second.Data.Path: "PNG-2"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != expected {
			t.Errorf("saved file %q = %q, %v; want %q", path, data, err, expected)
		}
	}

	otherTask := store.SaveImageToTaskFolder(firstImage, outputDir, uuid.NewString())
	if !otherTask.Success || filepath.Dir(otherTask.Data.Path) == firstDir {
		t.Fatalf("a new prompt batch must use a new task folder: %+v", otherTask)
	}
}

func TestSaveImageToTaskFolderRejectsInvalidInputs(t *testing.T) {
	store := NewStore(t.TempDir())
	image, err := store.Put("image", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	video, err := store.Put("video", "video/mp4", ".mp4", []byte("MP4"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, localURL, outputDir, taskID string
	}{
		{"invalid task id", image, t.TempDir(), "../outside"},
		{"missing folder", image, filepath.Join(t.TempDir(), "missing"), uuid.NewString()},
		{"video result", video, t.TempDir(), uuid.NewString()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if response := store.SaveImageToTaskFolder(tc.localURL, tc.outputDir, tc.taskID); response.Success {
				t.Fatalf("invalid save input was accepted: %+v", response)
			}
		})
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

func TestSaveNodeCopiesIntoConfiguredFolder(t *testing.T) {
	store := NewStore(t.TempDir())
	local, err := store.Put("img", "image/png", ".png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input, _ := json.Marshal(SaveRequest{LocalURL: local, Dir: dir})
	out, err := store.SaveNode().Run(context.Background(), input)
	if err != nil || out.(SaveResult).Path != filepath.Join(dir, "img.png") {
		t.Fatalf("unexpected result %+v %v", out, err)
	}
	if _, err := store.SaveNode().Run(context.Background(), json.RawMessage(`{"localUrl":"`+local+`"}`)); err == nil {
		t.Fatal("a save without a folder must fail")
	}
}

func TestImportVideoCopiesIntoStore(t *testing.T) {
	store := NewStore(t.TempDir())
	src := filepath.Join(t.TempDir(), "My Clip.MOV")
	if err := os.WriteFile(src, []byte("MOVDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := store.ImportVideo(func() (string, error) { return src, nil })
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
	if res := store.ImportVideo(func() (string, error) { return "", nil }); !res.Success || !res.Data.Canceled {
		t.Fatalf("closing the dialog must not be an error: %+v", res)
	}
	bad := filepath.Join(t.TempDir(), "notes.txt")
	_ = os.WriteFile(bad, []byte("x"), 0o600)
	if res := store.ImportVideo(func() (string, error) { return bad, nil }); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("a non-video file must be refused: %+v", res)
	}
}
