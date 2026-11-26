package media

import (
	"image"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	importPreviewRoute     = "/import-preview/"
	importPreviewSide      = 120
	importPreviewQuality   = 72
	importPreviewMaxPixels = 32 << 20
)

type importPreviewSet struct {
	token        string
	paths        []string
	maxFileBytes int64
}

// ImportPreviewPrefix exposes thumbnails for a single active import scan.
// The webview only receives URLs indexed into a list supplied by the native picker.
func (s *Store) ImportPreviewPrefix(paths []string, maxFileBytes int64) string {
	token := uuid.NewString()
	s.mu.Lock()
	s.importPreviews = importPreviewSet{token: token, paths: paths, maxFileBytes: maxFileBytes}
	s.mu.Unlock()
	return importPreviewRoute + token + "/"
}

func (s *Store) serveImportPreview(w http.ResponseWriter, r *http.Request) {
	token, rawIndex, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, importPreviewRoute), "/")
	index, err := strconv.Atoi(rawIndex)
	if r.Method != http.MethodGet || !ok || err != nil || index < 0 {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	previews := s.importPreviews
	s.mu.Unlock()
	if previews.token != token || index >= len(previews.paths) || previews.paths[index] == "" {
		http.NotFound(w, r)
		return
	}
	select {
	case thumbSlots <- struct{}{}:
		defer func() { <-thumbSlots }()
	case <-r.Context().Done():
		return
	}
	data, err := readImportPreview(previews.paths[index], previews.maxFileBytes)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(data)
}

func readImportPreview(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, os.ErrInvalid
	}
	cfg, _, err := image.DecodeConfig(file)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > importPreviewMaxPixels/cfg.Height {
		return nil, os.ErrInvalid
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	return encodeScaledJPEG(img, importPreviewSide, importPreviewQuality)
}
