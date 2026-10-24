package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"SnakeMedia/internal/providers/flow/chrome"
)

// Check the final absolute path and every existing component before removal.
// Links/junctions cannot redirect recursive deletion outside managed profiles.
func (s *Service) deletionPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("Profile không có đường dẫn để xóa.")
	}
	root, err := filepath.Abs(filepath.Join(s.paths.DataDir, "profiles"))
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("Từ chối xóa profile nằm ngoài thư mục quản lý.")
	}
	current := root
	parts := append([]string{""}, strings.Split(rel, string(os.PathSeparator))...)
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return target, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("Từ chối xóa profile qua liên kết hoặc đường dẫn không phải thư mục.")
		}
	}
	return target, nil
}

// Caller holds operation, but not mu while waiting for Chrome to release files.
func (s *Service) deleteProfile(ctx context.Context, path string) error {
	target, err := s.deletionPath(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	browser := s.browsers[target]
	s.mu.Unlock()
	if browser == nil {
		browser, err = chrome.ExistingBrowser(ctx, target, s.cfgManager.Get().Accounts)
		if err != nil {
			return err
		}
	}
	if browser != nil {
		if err := browser.Close(s.cfgManager.Get().Accounts); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Recheck after waiting, immediately before recursive deletion.
	target, err = s.deletionPath(target)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("không xóa được thư mục profile; tài khoản vẫn được giữ để thử lại: %w", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("Thư mục profile vẫn tồn tại sau khi xóa.")
	}
	s.mu.Lock()
	delete(s.browsers, target)
	s.mu.Unlock()
	return nil
}

func overlappingProfiles(first, second string) bool {
	if second == "" {
		return false
	}
	other, err := filepath.Abs(second)
	if err != nil {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(other); err == nil {
		other = resolved
	}
	if resolved, err := filepath.EvalSymlinks(first); err == nil {
		first = resolved
	}
	for _, pair := range [][2]string{{first, other}, {other, first}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return strings.EqualFold(first, other)
}
