package media

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// PreserveOriginal archives downloaded bytes before a destructive post-process.
// The archive is not served by the single-file /flow-media/ route. Repeated
// calls retain the first original rather than archiving an already processed file.
func (s *Store) PreserveOriginal(localURL string) (string, error) {
	src, err := s.Path(localURL)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.dir, "originals")
	dst := filepath.Join(dir, filepath.Base(src))
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(dir, "original-*.tmp")
	if err != nil {
		return "", err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}
