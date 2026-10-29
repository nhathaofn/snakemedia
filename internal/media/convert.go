package media

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // decoders for images converted on save
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"

	"SnakeMedia/internal/logger"
)

// jpegQuality is the quality images converted to JPG are written at.
const jpegQuality = 95

// imageExts are the stored files a conversion applies to; anything else
// (videos) is copied as it is.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true}

// ConvertImages makes images whose stored name starts with prefix be saved as
// format ("png" or "jpg"; anything else keeps the file) by Save and SaveTo.
// format is read on every save, so a changed setting applies at once.
func (s *Store) ConvertImages(prefix string, format func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.formats == nil {
		s.formats = map[string]func() string{}
	}
	s.formats[prefix] = format
}

// saveFormat is the extension a stored file is saved with (".png", ".jpg"),
// or "" to copy it as it is.
func (s *Store) saveFormat(src string) string {
	ext := strings.ToLower(filepath.Ext(src))
	if !imageExts[ext] {
		return ""
	}
	base := filepath.Base(src)
	s.mu.Lock()
	// The longest matching prefix wins, so overlapping prefixes pick the same
	// format on every save.
	var format func() string
	best := -1
	for prefix, fn := range s.formats {
		if strings.HasPrefix(base, prefix) && len(prefix) > best {
			format, best = fn, len(prefix)
		}
	}
	s.mu.Unlock()
	if format == nil {
		return ""
	}
	switch want := "." + format(); {
	case want != ".png" && want != ".jpg":
		return ""
	case want == ext || (want == ".jpg" && ext == ".jpeg"):
		return ""
	default:
		return want
	}
}

// saveFile writes src into dir as name (numbered when taken), converted to
// the saved format when one applies. An image that cannot be decoded is
// copied as it is rather than lost.
func (s *Store) saveFile(dir, name, src string) (string, error) {
	ext := s.saveFormat(src)
	if ext == "" {
		return copyUniqueFile(dir, name, filepath.Ext(src), src)
	}
	img, err := decodeImage(src)
	if err != nil {
		logger.Get().Warn("image kept in its own format", "file", filepath.Base(src), "error", err)
		return copyUniqueFile(dir, name, filepath.Ext(src), src)
	}
	return writeUniqueFile(dir, name, ext, func(w io.Writer) error {
		if ext == ".jpg" {
			return jpeg.Encode(w, opaque(img), &jpeg.Options{Quality: jpegQuality})
		}
		return png.Encode(w, img)
	})
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return img, nil
}

// opaque lays an image on white, since JPG has no transparency.
func opaque(img image.Image) image.Image {
	// An RGBA or NRGBA image with no transparent pixel already encodes to the
	// same JPG; other types (YCbCr, Gray) are kept on the RGBA path so the
	// written file does not change.
	switch img.(type) {
	case *image.RGBA, *image.NRGBA:
		if o, ok := img.(interface{ Opaque() bool }); ok && o.Opaque() {
			return img
		}
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}
