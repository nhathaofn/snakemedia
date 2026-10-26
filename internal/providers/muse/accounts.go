package muse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"SnakeMedia/internal/secure"
	"SnakeMedia/pkg/utils"
)

// MuseAccountStatus is what the app last learned about an account's session.
type MuseAccountStatus string

const (
	// MuseAccountUnverified: saved but not checked yet (new, or cookies just imported).
	MuseAccountUnverified MuseAccountStatus = "unverified"
	// MuseAccountSigningIn: a sign-in window is open and waiting for the user.
	MuseAccountSigningIn MuseAccountStatus = "signing_in"
	MuseAccountSignedIn  MuseAccountStatus = "signed_in"
	MuseAccountSignedOut MuseAccountStatus = "signed_out"
	MuseAccountError     MuseAccountStatus = "error"
)

var AllMuseAccountStatuses = []struct {
	Value  MuseAccountStatus
	TSName string
}{
	{MuseAccountUnverified, "Unverified"}, {MuseAccountSigningIn, "SigningIn"}, {MuseAccountSignedIn, "SignedIn"},
	{MuseAccountSignedOut, "SignedOut"}, {MuseAccountError, "Error"},
}

// MuseAccountSource is how the account's session was obtained.
type MuseAccountSource string

const (
	MuseSourceBrowser MuseAccountSource = "browser"
	MuseSourceCookies MuseAccountSource = "cookies"
)

var AllMuseAccountSources = []struct {
	Value  MuseAccountSource
	TSName string
}{{MuseSourceBrowser, "Browser"}, {MuseSourceCookies, "Cookies"}}

// MuseAccount is the public view of an account; it never carries cookies.
type MuseAccount struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Source      MuseAccountSource `json:"source"`
	ProfileDir  string            `json:"profileDir"`
	Status      MuseAccountStatus `json:"status"`
	Enabled     bool              `json:"enabled"`
	Quota       *MuseQuota        `json:"quota,omitempty"`
	Proxy       string            `json:"proxy,omitempty"`
	ProxyAuto   string            `json:"proxyAuto,omitempty"`
	ProxyStatus string            `json:"proxyStatus,omitempty"`
	ProxyIP     string            `json:"proxyIp,omitempty"`
	// PausedUntil rests the account after Muse said it hit a limit (RFC 3339).
	PausedUntil   string `json:"pausedUntil,omitempty"`
	Message       string `json:"message,omitempty"`
	LastCheckedAt string `json:"lastCheckedAt,omitempty"`
	LastSentAt    string `json:"lastSentAt,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type storedAccount struct {
	MuseAccount
	Cookies []Cookie `json:"cookies,omitempty"`
	// CookieVersion changes on every import, so a profile is re-seeded once.
	CookieVersion string `json:"cookieVersion,omitempty"`
}

const accountStoreHeader = "SMMA\x01"

// accountStore persists accounts DPAPI-encrypted. A corrupt file is moved
// aside and blocks writes for the run instead of being read as empty.
type accountStore struct {
	mu       sync.Mutex
	path     string
	accounts []storedAccount
	loaded   bool
	err      error
}

func newAccountStore(path string) *accountStore { return &accountStore{path: path} }

func (s *accountStore) loadLocked() error {
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
	var accounts []storedAccount
	if !bytes.HasPrefix(data, []byte(accountStoreHeader)) {
		err = errors.New("missing header")
	} else if data, err = secure.Unprotect(data[len(accountStoreHeader):]); err == nil {
		err = json.Unmarshal(data, &accounts)
	}
	if err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405"))
		if moveErr := os.Rename(s.path, backup); moveErr != nil {
			s.err = fmt.Errorf("Dữ liệu tài khoản Muse hỏng và không cách ly được: %w", moveErr)
		} else {
			s.err = fmt.Errorf("Dữ liệu tài khoản Muse hỏng đã được giữ tại %s. Khôi phục dữ liệu trước khi tiếp tục.", backup)
		}
		return s.err
	}
	s.accounts, s.loaded = accounts, true
	return nil
}

func (s *accountStore) saveLocked() error {
	data, err := json.Marshal(s.accounts)
	if err != nil {
		return err
	}
	if data, err = secure.Protect(data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(s.path, append([]byte(accountStoreHeader), data...), 0o600)
}

// list returns copies of all accounts in saved order.
func (s *accountStore) list() ([]storedAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return append([]storedAccount(nil), s.accounts...), nil
}

func (s *accountStore) get(id string) (storedAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return storedAccount{}, err
	}
	for _, acc := range s.accounts {
		if acc.ID == id {
			return acc, nil
		}
	}
	return storedAccount{}, errAccountNotFound
}

var errAccountNotFound = errors.New("Tài khoản Muse không tồn tại.")

func (s *accountStore) add(acc storedAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	acc.CreatedAt, acc.UpdatedAt = now, now
	s.accounts = append(s.accounts, acc)
	if err := s.saveLocked(); err != nil {
		s.accounts = s.accounts[:len(s.accounts)-1]
		return err
	}
	return nil
}

// update applies change to the stored account and persists it.
func (s *accountStore) update(id string, change func(*storedAccount)) (storedAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return storedAccount{}, err
	}
	for i := range s.accounts {
		if s.accounts[i].ID != id {
			continue
		}
		previous := s.accounts[i]
		change(&s.accounts[i])
		s.accounts[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveLocked(); err != nil {
			s.accounts[i] = previous
			return storedAccount{}, err
		}
		return s.accounts[i], nil
	}
	return storedAccount{}, errAccountNotFound
}

func (s *accountStore) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			previous := s.accounts
			s.accounts = append(append([]storedAccount(nil), s.accounts[:i]...), s.accounts[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.accounts = previous
				return err
			}
			return nil
		}
	}
	return errAccountNotFound
}
