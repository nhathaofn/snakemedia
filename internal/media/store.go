// Package media keeps generated images and videos on disk and serves them to the
// webview. It knows nothing about providers: any node that downloads a result
// puts it here and hands the returned local URL to the UI or the next node.
package media

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"SnakeMedia/internal/models"
	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

const (
	// Route is the URL prefix the asset server uses for stored media.
	Route = "/flow-media/"
	// maxSaveAttempts bounds the numbered names tried before giving up on a folder.
	maxSaveAttempts = 1000
)

// extensions maps the content types providers serve to file extensions.
var extensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
	"video/mp4":  ".mp4",
	"video/webm": ".webm",
}

// contentTypes are sent for stored files so playback never depends on the OS registry.
var contentTypes = map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime"}

// VideoTypes are the video files a user may import, with the type sent when
// uploading them (Flow's upload whitelist: mp4, m4v, webm, quicktime).
var VideoTypes = map[string]string{".mp4": "video/mp4", ".m4v": "video/m4v", ".webm": "video/webm", ".mov": "video/quicktime"}

func SupportedVideoExtensions() []string {
	list := make([]string, 0, len(VideoTypes))
	for extension := range VideoTypes {
		list = append(list, extension)
	}
	slices.Sort(list)
	return list
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// SaveResult reports where a file was saved, or that the user closed the folder
// dialog without choosing one.
type SaveResult struct {
	Path     string `json:"path"`
	Canceled bool   `json:"canceled"`
}

// DirectoryResult reports the folder selected by the user, or that the
// directory dialog was canceled.
type DirectoryResult struct {
	Path     string `json:"path"`
	Canceled bool   `json:"canceled"`
}

// ImportResult describes a video the user picked, copied into the media folder
// so the webview can preview it and providers can upload it.
type ImportResult struct {
	LocalURL    string `json:"localUrl"`
	FileName    string `json:"fileName"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	Canceled    bool   `json:"canceled"`
}

// Store is the folder of downloaded media.
type Store struct {
	dir     string
	mu      sync.Mutex
	saveDir string // last folder the user saved a file to
}

// NewStore keeps media in dir, created on first use.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir is the folder served under Route.
func (s *Store) Dir() string {
	return s.dir
}

// Put writes downloaded bytes and returns the URL the webview loads them from.
// fallbackExt is used for untyped content.
func (s *Store) Put(id, contentType, fallbackExt string, data []byte) (string, error) {
	ext, ok := extensions[strings.TrimSpace(strings.Split(contentType, ";")[0])]
	if !ok {
		ext = fallbackExt
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", err
	}
	name := fileBase(id) + ext
	if err := utils.WriteFileAtomic(filepath.Join(s.dir, name), data, 0o600); err != nil {
		return "", err
	}
	return Route + name, nil
}

// Path returns the stored file behind a local URL, or an error for anything
// outside the media folder.
func (s *Store) Path(localURL string) (string, error) {
	name := strings.TrimPrefix(localURL, Route)
	if name == localURL || name == "" || name != filepath.Base(name) {
		return "", fmt.Errorf("Tệp %q không thuộc thư mục media.", localURL)
	}
	return filepath.Join(s.dir, name), nil
}

// Save copies a stored file into a folder chooseDir returns (starting at the
// last folder used), never replacing an existing file. An empty folder means
// the user canceled.
func (s *Store) Save(localURL string, chooseDir func(defaultDir string) (string, error)) models.Response[SaveResult] {
	path, err := s.Path(localURL)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Tệp không hợp lệ.", models.CodeInvalidArgument)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Không đọc được tệp đã tải: "+err.Error(), models.CodeNotFound)
	}
	s.mu.Lock()
	lastDir := s.saveDir
	s.mu.Unlock()
	dir, err := chooseDir(lastDir)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Không mở được hộp thoại chọn thư mục: "+err.Error(), models.CodeInternal)
	}
	if dir == "" {
		return models.SuccessResponse(SaveResult{Canceled: true})
	}
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	s.mu.Lock()
	saved, err := writeUniqueFile(dir, strings.TrimSuffix(name, ext), ext, data)
	s.mu.Unlock()
	if err != nil {
		return models.ErrorResponse[SaveResult](err.Error(), models.CodeInternal)
	}
	s.mu.Lock()
	s.saveDir = dir
	s.mu.Unlock()
	return models.SuccessResponse(SaveResult{Path: saved})
}

// SaveImageToTaskFolder copies a generated image into a stable folder for one
// prompt batch. taskID must be a UUID so it cannot escape outputDir.
func (s *Store) SaveImageToTaskFolder(localURL, outputDir, taskID string) models.Response[SaveResult] {
	mediaPath, err := s.Path(localURL)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Tệp không hợp lệ.", models.CodeInvalidArgument)
	}
	ext := strings.ToLower(filepath.Ext(mediaPath))
	switch ext {
	case ".png", ".jpg", ".webp", ".gif":
	default:
		return models.ErrorResponse[SaveResult]("Chỉ có thể lưu ảnh được tạo.", models.CodeInvalidArgument)
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Mã tác vụ không hợp lệ.", models.CodeInvalidArgument)
	}
	if strings.TrimSpace(outputDir) == "" {
		return models.ErrorResponse[SaveResult]("Chưa chọn thư mục lưu.", models.CodeInvalidArgument)
	}
	rootInfo, err := os.Stat(outputDir)
	if err != nil || !rootInfo.IsDir() {
		return models.ErrorResponse[SaveResult](fmt.Sprintf("Thư mục lưu %q không tồn tại.", outputDir), models.CodeInvalidArgument)
	}
	data, err := os.ReadFile(mediaPath)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Không đọc được ảnh đã tạo: "+err.Error(), models.CodeNotFound)
	}
	taskDir := filepath.Join(outputDir, "task-"+id.String())
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return models.ErrorResponse[SaveResult]("Không tạo được thư mục tác vụ: "+err.Error(), models.CodeInternal)
	}

	// Multiple image workers can save into the same task folder at once.
	s.mu.Lock()
	saved, err := writeUniqueFile(taskDir, strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath)), ext, data)
	s.mu.Unlock()
	if err != nil {
		return models.ErrorResponse[SaveResult](err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(SaveResult{Path: saved})
}

// ImportVideo copies the video file chooseFile returns into the media folder. An
// empty path means the user canceled.
func (s *Store) ImportVideo(chooseFile func() (string, error)) models.Response[ImportResult] {
	src, err := chooseFile()
	if err != nil {
		return models.ErrorResponse[ImportResult]("Không mở được hộp thoại chọn tệp: "+err.Error(), models.CodeInternal)
	}
	if src == "" {
		return models.SuccessResponse(ImportResult{Canceled: true})
	}
	ext := strings.ToLower(filepath.Ext(src))
	contentType, ok := VideoTypes[ext]
	if !ok {
		return models.ErrorResponse[ImportResult](fmt.Sprintf("Định dạng %q không được hỗ trợ (chỉ mp4, m4v, webm, mov).", ext), models.CodeInvalidArgument)
	}
	in, err := os.Open(src)
	if err != nil {
		return models.ErrorResponse[ImportResult]("Không đọc được tệp video: "+err.Error(), models.CodeNotFound)
	}
	defer in.Close()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return models.ErrorResponse[ImportResult](err.Error(), models.CodeInternal)
	}
	name := "source-" + uuid.NewString() + ext
	dst := filepath.Join(s.dir, name)
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return models.ErrorResponse[ImportResult](err.Error(), models.CodeInternal)
	}
	size, copyErr := io.Copy(out, in)
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(dst)
		return models.ErrorResponse[ImportResult]("Không sao chép được tệp video: "+copyErr.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(ImportResult{LocalURL: Route + name, FileName: filepath.Base(src), Size: size, ContentType: contentType})
}

// Middleware serves stored media under Route ahead of the asset server. It must
// be middleware, not a Handler: during wails dev every GET goes to the Vite dev
// server first, which answers unknown paths with index.html. http.FileServer
// also answers Range requests, which video seeking relies on.
func (s *Store) Middleware(next http.Handler) http.Handler {
	files := http.StripPrefix(Route, http.FileServer(http.Dir(s.dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, Route) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if contentType, ok := contentTypes[strings.ToLower(filepath.Ext(r.URL.Path))]; ok {
			w.Header().Set("Content-Type", contentType)
		}
		files.ServeHTTP(w, r)
	})
}

func fileBase(id string) string {
	base := strings.Trim(unsafeFileChars.ReplaceAllString(id, "_"), "_")
	if base == "" {
		return "flow-media"
	}
	return base
}

// writeUniqueFile writes name+ext, or name-2+ext and so on when that file exists.
func writeUniqueFile(dir, name, ext string, data []byte) (string, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("Thư mục %q không tồn tại.", dir)
	}
	for i := 1; i <= maxSaveAttempts; i++ {
		candidate := name + ext
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d%s", name, i, ext)
		}
		path := filepath.Join(dir, candidate)
		if utils.FileExists(path) {
			continue
		}
		if err := utils.WriteFileAtomic(path, data, 0o644); err != nil {
			return "", fmt.Errorf("Không lưu được tệp vào %q: %w", dir, err)
		}
		return path, nil
	}
	return "", fmt.Errorf("Thư mục %q đã có quá nhiều tệp trùng tên.", dir)
}
