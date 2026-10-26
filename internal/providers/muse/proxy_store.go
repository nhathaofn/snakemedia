package muse

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"SnakeMedia/internal/secure"
	"SnakeMedia/pkg/utils"
)

const proxyStoreHeader = "SMPX\x01"

type proxyStore struct {
	mu     sync.Mutex
	path   string
	text   string
	loaded bool
	err    error
}

func newProxyStore(path string) *proxyStore {
	return &proxyStore{path: path}
}

func (s *proxyStore) loadLocked() error {
	if s.err != nil || s.loaded {
		return s.err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte(proxyStoreHeader)) {
		err = errors.New("missing header")
	} else if data, err = secure.Unprotect(data[len(proxyStoreHeader):]); err == nil {
		s.text = string(data)
	}
	if err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405"))
		if moveErr := os.Rename(s.path, backup); moveErr != nil {
			s.err = fmt.Errorf("Dữ liệu proxy Muse hỏng và không cách ly được: %w", moveErr)
		} else {
			s.err = fmt.Errorf("Dữ liệu proxy Muse hỏng đã được giữ tại %s. Khôi phục dữ liệu trước khi tiếp tục.", backup)
		}
		return s.err
	}
	s.loaded = true
	return nil
}

// GetText returns the saved proxy list. An unreadable store is an error, not
// an empty list: treating it as empty would send every account out on the
// machine's real IP.
func (s *proxyStore) GetText() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	return s.text, nil
}

func (s *proxyStore) SaveText(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	raw, err := secure.Protect([]byte(text))
	if err != nil {
		return fmt.Errorf("không mã hóa được danh sách proxy: %w", err)
	}
	buf := append([]byte(proxyStoreHeader), raw...)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := utils.WriteFileAtomic(s.path, buf, 0o600); err != nil {
		return err
	}
	s.text = text
	return nil
}
