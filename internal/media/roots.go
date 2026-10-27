package media

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errOutsideRoots refuses a folder or file the frontend names that the user
// never chose: a compromised page must not be able to write or reveal files
// anywhere on disk.
var errOutsideRoots = errors.New("Thư mục này chưa được chọn trong app. Hãy chọn thư mục lưu bằng nút chọn thư mục.")

// AllowRoot lets files be saved under dir and revealed from it: the app's
// default output folders, and folders the user picks in a native dialog.
func (s *Store) AllowRoot(dir string) {
	clean, err := cleanDir(dir)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, root := range s.roots {
		if strings.EqualFold(root, clean) {
			return
		}
	}
	s.roots = append(s.roots, clean)
}

// AllowRootsFrom adds roots read at the time of each check, for folders kept
// in settings that the user can change (a provider's output folder).
func (s *Store) AllowRootsFrom(roots func() []string) {
	s.mu.Lock()
	s.dynamicRoots = append(s.dynamicRoots, roots)
	s.mu.Unlock()
}

// CheckDir returns dir cleaned when it lies inside an allowed root.
func (s *Store) CheckDir(dir string) (string, error) {
	clean, err := cleanDir(dir)
	if err != nil {
		return "", errOutsideRoots
	}
	if s.within(clean, false) {
		return clean, nil
	}
	return "", errOutsideRoots
}

// CheckFile returns path cleaned when it is a stored media file or lies inside
// an allowed root.
func (s *Store) CheckFile(path string) (string, error) {
	clean, err := cleanDir(path)
	if err != nil {
		return "", errOutsideRoots
	}
	if s.within(clean, true) {
		return clean, nil
	}
	return "", fmt.Errorf("Tệp %q không nằm trong thư mục của app.", filepath.Base(clean))
}

func (s *Store) within(path string, mediaDir bool) bool {
	s.mu.Lock()
	roots := append([]string{}, s.roots...)
	dynamic := append([]func() []string{}, s.dynamicRoots...)
	s.mu.Unlock()
	if mediaDir {
		roots = append(roots, s.dir)
	}
	for _, fn := range dynamic {
		roots = append(roots, fn()...)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		clean, err := cleanDir(root)
		if err == nil && inside(clean, path) {
			return true
		}
	}
	return false
}

// cleanDir makes path absolute and resolves links of the part that exists, so
// a link inside a root cannot lead outside it.
func cleanDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return abs, nil
}

// inside reports whether path is root or below it (filepath.Rel ignores case
// on Windows, like its file systems).
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
