package proxy

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

// Store persists the proxy list encrypted. A corrupt file is moved aside and
// blocks writes for the run instead of being read as an empty list.
type Store struct {
	mu     sync.Mutex
	path   string
	text   string
	loaded bool
	err    error
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) loadLocked() error {
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
	} else if len(data) == len(proxyStoreHeader) {
		// An emptied list is saved as the header alone: DPAPI refuses no data.
		s.text = ""
	} else if data, err = secure.Unprotect(data[len(proxyStoreHeader):]); err == nil {
		s.text = string(data)
	}
	if err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405"))
		if moveErr := os.Rename(s.path, backup); moveErr != nil {
			s.err = fmt.Errorf("Dữ liệu proxy hỏng và không cách ly được: %w", moveErr)
		} else {
			s.err = fmt.Errorf("Dữ liệu proxy hỏng đã được giữ tại %s. Khôi phục dữ liệu trước khi tiếp tục.", backup)
		}
		return s.err
	}
	s.loaded = true
	return nil
}

// GetText returns the saved proxy list. An unreadable store is an error, not
// an empty list: treating it as empty would send every account out on the
// machine's real IP.
func (s *Store) GetText() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	return s.text, nil
}

func (s *Store) SaveText(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	buf := []byte(proxyStoreHeader)
	if text != "" {
		raw, err := secure.Protect([]byte(text))
		if err != nil {
			return fmt.Errorf("không mã hóa được danh sách proxy: %w", err)
		}
		buf = append(buf, raw...)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := utils.WriteFileAtomic(s.path, buf, 0o600); err != nil {
		return err
	}
	s.text = text
	return nil
}
