package media

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PutReader streams into a private temporary file, then publishes atomically.
// Partial and oversized downloads never replace an existing playable file.
func (s *Store) PutReader(id, contentType, fallbackExt string, reader io.Reader, limit int64) (string, error) {
	if limit <= 0 {
		return "", errors.New("Giới hạn tải media không hợp lệ.")
	}
	ext, ok := extensions[strings.TrimSpace(strings.Split(contentType, ";")[0])]
	if !ok {
		ext = fallbackExt
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(s.dir, ".download-*")
	if err != nil {
		return "", err
	}
	temp := file.Name()
	defer os.Remove(temp)
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return "", err
	}
	n, err := io.Copy(file, io.LimitReader(reader, limit+1))
	if err != nil {
		return "", err
	}
	if n == 0 || n > limit {
		return "", errors.New("Tệp media rỗng hoặc vượt giới hạn tải.")
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	name := fileBase(id) + ext
	if err := os.Rename(temp, filepath.Join(s.dir, name)); err != nil {
		return "", err
	}
	return Route + name, nil
}
