package media

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"SnakeMedia/pkg/utils"
)

// SaveToOnce publishes a finished copy under its planned name. A retry may
// reuse an identical copy left behind after a crash or a journal write error.
func (s *Store) SaveToOnce(localURL, dir, name string) (string, error) {
	if !validSaveFileName(name) {
		return "", fmt.Errorf("Tên tệp %q không hợp lệ.", name)
	}
	src, err := s.Path(localURL)
	if err != nil {
		return "", err
	}
	dir, err = s.CheckDir(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stageDir, err := os.MkdirTemp(dir, ".snakemedia-save-")
	if err != nil {
		return "", err
	}
	defer os.Remove(stageDir)
	staged, err := s.saveFile(stageDir, name, src)
	if err != nil {
		return "", err
	}
	defer os.Remove(staged)
	final := filepath.Join(dir, filepath.Base(staged))
	if err := utils.PublishFile(staged, final); err == nil {
		return final, nil
	} else if !errors.Is(err, os.ErrExist) {
		return "", err
	}
	want, err := utils.FileDigest(staged)
	if err != nil {
		return "", err
	}
	have, err := utils.FileDigest(final)
	if err != nil {
		return "", err
	}
	if have != want {
		return "", fmt.Errorf("Tệp %q đã có nội dung khác. Hãy đổi tên tệp đó rồi thử lưu lại.", final)
	}
	return final, nil
}
