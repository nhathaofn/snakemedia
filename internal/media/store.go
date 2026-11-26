// Package media keeps generated images and videos on disk and serves them to the
// webview. It knows nothing about providers: any node that downloads a result
// puts it here and hands the returned local URL to the UI or the next node.
package media

import (
	"errors"
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
	// VaultRoute is the URL prefix the asset server uses for the managed Muse Asset Vault.
	VaultRoute = "/muse-vault/"
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
	// roots are the folders files may be saved into or revealed from without
	// a dialog (see AllowRoot).
	roots        []string
	dynamicRoots []func() []string
	// formats are the saved image formats by stored-name prefix (ConvertImages).
	formats map[string]func() string
	// vaultDir resolves the active Muse Asset Vault directory; vaultThumbDir keeps
	// thumbnails in AppData instead of polluting the user-visible vault folder.
	vaultDir       func() string
	vaultThumbDir  string
	importPreviews importPreviewSet
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

// SetVaultDir configures the directory resolver and thumbnail cache folder for
// serving managed Muse Asset Vault files under VaultRoute.
func (s *Store) SetVaultDir(dirFn func() string, thumbCacheDir string) {
	s.mu.Lock()
	s.vaultDir = dirFn
	s.vaultThumbDir = thumbCacheDir
	s.mu.Unlock()
}

// VaultURL returns the asset-server URL for a relative path inside the Vault.
func VaultURL(relPath string) string {
	slash := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	return VaultRoute + slash
}

// VaultPath resolves a VaultRoute URL to a file inside the active Asset Vault,
// rejecting path traversal, symlinks outside the vault, and non-regular paths.
func (s *Store) VaultPath(localURL string) (string, error) {
	rel := strings.TrimPrefix(localURL, VaultRoute)
	if rel == localURL || rel == "" || strings.ContainsRune(rel, 0) || strings.Contains(rel, `\`) || strings.Contains(rel, ":") {
		return "", fmt.Errorf("Tệp %q không thuộc kho tư liệu.", localURL)
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", fmt.Errorf("Tệp %q không thuộc kho tư liệu.", localURL)
		}
	}
	s.mu.Lock()
	dirFn := s.vaultDir
	s.mu.Unlock()
	if dirFn == nil {
		return "", fmt.Errorf("Kho tư liệu chưa được khởi tạo.")
	}
	root := strings.TrimSpace(dirFn())
	cleanRoot, err := cleanDir(root)
	if err != nil {
		return "", fmt.Errorf("Kho tư liệu chưa hợp lệ: %w", err)
	}
	candidate := filepath.Join(append([]string{cleanRoot}, parts...)...)
	cleanTarget, err := cleanDir(candidate)
	if err != nil || !inside(cleanRoot, cleanTarget) || cleanTarget == cleanRoot {
		return "", fmt.Errorf("Tệp %q không thuộc kho tư liệu.", localURL)
	}
	return cleanTarget, nil
}

// OpenPath resolves either a stored /flow-media/ URL or a /muse-vault/ URL to a disk path.
func (s *Store) OpenPath(localURL string) (string, error) {
	if strings.HasPrefix(localURL, VaultRoute) {
		return s.VaultPath(localURL)
	}
	return s.Path(localURL)
}

// Path returns the stored file behind a local URL, or an error for anything
// outside the media folder.
func (s *Store) Path(localURL string) (string, error) {
	name := strings.TrimPrefix(localURL, Route)
	if name == localURL || name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return "", fmt.Errorf("Tệp %q không thuộc thư mục media.", localURL)
	}
	return filepath.Join(s.dir, name), nil
}

// Remove deletes the stored file behind localURL and any cached thumbnails for
// it; missing files are ignored.
func (s *Store) Remove(localURL string) error {
	path, err := s.Path(localURL)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.removeThumbs(filepath.Base(path))
	return nil
}

// Save copies a stored file into a folder chooseDir returns (starting at the
// last folder used) under the next number of that folder (see OutputName),
// never replacing an existing file. An empty folder means the user canceled.
func (s *Store) Save(localURL string, chooseDir func(defaultDir string) (string, error)) models.Response[SaveResult] {
	path, err := s.Path(localURL)
	if err != nil {
		return models.ErrorResponse[SaveResult]("Tệp không hợp lệ.", models.CodeInvalidArgument)
	}
	if _, err := os.Stat(path); err != nil {
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
	saved, err := s.saveFile(dir, OutputName(NextNumber(dir), ProviderOf(localURL)), path)
	if err != nil {
		return models.ErrorResponse[SaveResult](err.Error(), models.CodeInternal)
	}
	s.AllowRoot(dir)
	s.mu.Lock()
	s.saveDir = dir
	s.mu.Unlock()
	return models.SuccessResponse(SaveResult{Path: saved})
}

// SaveTo copies a stored file into dir as name plus the file's extension (or
// the format ConvertImages set for it), numbering the name when it is taken
// (name-2, name-3, …). dir must lie inside
// an allowed root and is created when missing. The copy is streamed, so a long
// video is never read into memory.
func (s *Store) SaveTo(localURL, dir, name string) (string, error) {
	src, err := s.Path(localURL)
	if err != nil {
		return "", err
	}
	if !validSaveFileName(name) {
		return "", fmt.Errorf("Tên tệp %q không hợp lệ.", name)
	}
	if dir, err = s.CheckDir(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("Không tạo được thư mục %q: %w", dir, err)
	}
	return s.saveFile(dir, name, src)
}

func validSaveFileName(name string) bool {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, "<>:\"/\\|?*") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ImportVideo copies the video file chooseFile returns into the media folder,
// refusing extensions outside accepted (the provider's upload whitelist). An
// empty path means the user canceled.
func (s *Store) ImportVideo(accepted []string, chooseFile func() (string, error)) models.Response[ImportResult] {
	src, err := chooseFile()
	if err != nil {
		return models.ErrorResponse[ImportResult]("Không mở được hộp thoại chọn tệp: "+err.Error(), models.CodeInternal)
	}
	if src == "" {
		return models.SuccessResponse(ImportResult{Canceled: true})
	}
	ext := strings.ToLower(filepath.Ext(src))
	if !slices.Contains(accepted, ext) {
		return models.ErrorResponse[ImportResult](fmt.Sprintf("Định dạng %q không được hỗ trợ (chỉ %s).", ext, strings.Join(accepted, ", ")), models.CodeInvalidArgument)
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
	return models.SuccessResponse(ImportResult{LocalURL: Route + name, FileName: filepath.Base(src), Size: size, ContentType: contentTypes[ext]})
}

// Middleware serves stored media under Route ahead of the asset server. It must
// be middleware, not a Handler: during wails dev every GET goes to the Vite dev
// server first, which answers unknown paths with index.html. http.FileServer
// also answers Range requests, which video seeking relies on.
func (s *Store) Middleware(next http.Handler) http.Handler {
	files := http.StripPrefix(Route, http.FileServer(http.Dir(s.dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, importPreviewRoute) {
			s.serveImportPreview(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, VaultRoute) {
			s.serveVault(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, Route) {
			next.ServeHTTP(w, r)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, Route)
		if rel == "" || strings.HasSuffix(r.URL.Path, "/") || hasHiddenSegment(rel) {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Has(ThumbQuery) && s.serveThumb(w, r, rel) {
			return
		}
		if contentType, ok := contentTypes[strings.ToLower(filepath.Ext(r.URL.Path))]; ok {
			w.Header().Set("Content-Type", contentType)
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Store) serveVault(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r)
		return
	}
	fullPath, err := s.VaultPath(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !imageExts[strings.ToLower(filepath.Ext(fullPath))] {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Has(ThumbQuery) && s.serveVaultThumb(w, r, strings.TrimPrefix(r.URL.Path, VaultRoute), fullPath, info) {
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, fullPath)
}

func hasHiddenSegment(rel string) bool {
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func fileBase(id string) string {
	base := strings.Trim(unsafeFileChars.ReplaceAllString(id, "_"), "_")
	if base == "" {
		return "flow-media"
	}
	return base
}

// copyUniqueFile streams src into dir as name+ext, or name-2+ext and so on
// when that file exists. The name is taken with an exclusive create, so two
// savers never write the same file and an existing file is never replaced; a
// failed copy leaves no partial file behind.
func copyUniqueFile(dir, name, ext, src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("Không đọc được tệp đã tải: %w", err)
	}
	defer in.Close()
	return writeUniqueFile(dir, name, ext, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}

// writeUniqueFile creates name+ext in dir (name-2+ext and so on when taken)
// and fills it with write, leaving no partial file behind on failure.
func writeUniqueFile(dir, name, ext string, write func(io.Writer) error) (string, error) {
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
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("Không lưu được tệp vào %q: %w", dir, err)
		}
		err = write(out)
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("Không lưu được tệp vào %q: %w", dir, err)
		}
		return path, nil
	}
	return "", fmt.Errorf("Thư mục %q đã có quá nhiều tệp trùng tên.", dir)
}
