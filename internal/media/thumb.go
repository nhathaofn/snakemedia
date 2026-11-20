package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"

	"SnakeMedia/internal/logger"
	"SnakeMedia/pkg/utils"
)

// ThumbQuery asks Middleware for a small JPEG of a stored image instead of the
// image itself: lists show many images at once, and decoding full-size files
// for a thumbnail costs the WebView memory and frame time.
const ThumbQuery = "thumb"

// thumbSize is the longest side of a thumbnail, enough for list cells at 2× scale.
const thumbSize = 320

const thumbQuality = 82

// thumbDir holds thumbnails inside the media folder (not shown anywhere else).
const thumbDir = ".thumbs"

// thumbSlots caps thumbnails made at once, so a list opening does not take every core.
var thumbSlots = make(chan struct{}, 2)

// serveThumb answers a thumbnail request for the stored image at name; it
// returns false when the original should be served instead (not an image,
// small already, or the thumbnail could not be made).
func (s *Store) serveThumb(w http.ResponseWriter, r *http.Request, name string) bool {
	// Only files directly in the media folder: no separators, no "..".
	if !imageExts[strings.ToLower(filepath.Ext(name))] || strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") {
		return false
	}
	src := filepath.Join(s.dir, name)
	info, err := os.Stat(src)
	if err != nil {
		return false
	}
	// Keyed by size and time: a file replaced under the same name gets a new thumbnail.
	thumb := filepath.Join(s.dir, thumbDir, fmt.Sprintf("%s-%d-%d.jpg", strings.TrimSuffix(name, filepath.Ext(name)), info.Size(), info.ModTime().UnixNano()))
	if _, err := os.Stat(thumb); err != nil {
		if made, err := makeThumb(src, thumb); err != nil {
			logger.Get().Debug("thumbnail not made, serving the original", "file", name, "error", err)
			return false
		} else if !made {
			return false
		}
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, thumb)
	return true
}

// makeThumb writes a thumbnail of src to dst; false (no error) when src is
// already small enough to be its own thumbnail.
func makeThumb(src, dst string) (bool, error) {
	thumbSlots <- struct{}{}
	defer func() { <-thumbSlots }()
	if _, err := os.Stat(dst); err == nil {
		return true, nil // made by a request that waited ahead of this one
	}
	img, err := decodeImage(src)
	if err != nil {
		return false, err
	}
	b := img.Bounds()
	if max(b.Dx(), b.Dy()) <= thumbSize {
		return false, nil
	}
	data, err := encodeScaledJPEG(img, thumbSize, thumbQuality)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return false, err
	}
	return true, utils.WriteFileAtomic(dst, data, 0o600)
}

// PreviewDataURL decodes src, scales its longest side down to maxSide when
// larger, and returns a JPEG data URL for lightweight previews without storing
// a copy in the media folder. Returns "" when the file cannot be decoded.
func PreviewDataURL(src string, maxSide, quality int) string {
	img, err := decodeImage(src)
	if err != nil {
		return ""
	}
	data, err := encodeScaledJPEG(img, maxSide, quality)
	if err != nil {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
}

func encodeScaledJPEG(img image.Image, maxSide, quality int) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxSide > 0 && max(w, h) > maxSide {
		if w >= h {
			w, h = maxSide, max(1, h*maxSide/w)
		} else {
			w, h = max(1, w*maxSide/h), maxSide
		}
	}
	var src image.Image
	if w == b.Dx() && h == b.Dy() {
		src = opaque(img)
	} else {
		small := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), opaque(img), b, xdraw.Src, nil)
		src = small
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// removeThumbs deletes cached thumbnails generated for name.
func (s *Store) removeThumbs(name string) {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" {
		return
	}
	dir := filepath.Join(s.dir, thumbDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := stem + "-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
