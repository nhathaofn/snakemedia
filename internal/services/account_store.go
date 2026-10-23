package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/services/chrome"
	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

const accountStoreHeader = "SMAC\x01"

type storedAccount struct {
	AccountProfile
	Credentials chrome.Credentials `json:"credentials"`
}

func (s *AccountService) accountsFilePath() string {
	return filepath.Join(s.paths.DataDir, "accounts.json")
}

// Caller holds mu. Malformed data is preserved and blocks writes for this run,
// rather than being silently interpreted as an empty account collection.
func (s *AccountService) loadAccountsUnsafe() ([]storedAccount, error) {
	if s.storeErr != nil {
		return nil, s.storeErr
	}
	data, err := os.ReadFile(s.accountsFilePath())
	if errors.Is(err, os.ErrNotExist) {
		return []storedAccount{}, nil
	}
	if err != nil {
		return nil, err
	}
	legacy := !bytes.HasPrefix(data, []byte(accountStoreHeader))
	if !legacy {
		data, err = unprotectAccounts(data[len(accountStoreHeader):])
		if err != nil {
			return nil, errors.New("Không giải mã được dữ liệu tài khoản cho người dùng hệ điều hành hiện tại.")
		}
	}
	var accounts []storedAccount
	err = json.Unmarshal(data, &accounts)
	if err == nil && accounts == nil {
		err = errors.New("account store must be an array")
	}
	seen := make(map[string]bool)
	if err == nil {
		for i := range accounts {
			a := &accounts[i]
			if a.ID == "" || a.Email == "" || seen[strings.ToLower(a.Email)] {
				err = errors.New("invalid or duplicate account identity")
				break
			}
			seen[strings.ToLower(a.Email)] = true
			if legacy {
				// Old refresh could copy another account's credentials. Never trust
				// those credentials until the matching profile is inspected again.
				a.Credentials = chrome.Credentials{}
				a.Status = AccountUnverified
				a.LastVerifiedAt = ""
				a.Balance = nil
				a.CookieCount = 0
			}
		}
	}
	if err != nil {
		backup := s.accountsFilePath() + ".corrupt-" + uuid.NewString()
		if moveErr := os.Rename(s.accountsFilePath(), backup); moveErr != nil {
			s.storeErr = fmt.Errorf("Dữ liệu tài khoản hỏng; không thể cách ly: %w", moveErr)
		} else {
			s.storeErr = fmt.Errorf("Dữ liệu tài khoản hỏng đã được giữ tại %s. Khôi phục dữ liệu trước khi tiếp tục.", backup)
		}
		return nil, s.storeErr
	}
	if legacy {
		if err := s.saveAccountsUnsafe(accounts); err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

func (s *AccountService) saveAccountsUnsafe(accounts []storedAccount) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	data, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	data, err = protectAccounts(data)
	if err != nil {
		return err
	}
	return utils.WriteFileAtomic(s.accountsFilePath(), append([]byte(accountStoreHeader), data...), 0o600)
}

func (s *AccountService) GetAccounts(_ context.Context) models.Response[[]AccountProfile] {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.loadAccountsUnsafe()
	if err != nil {
		return models.ErrorResponse[[]AccountProfile](err.Error(), models.CodeInternal)
	}
	views := make([]AccountProfile, 0, len(accounts))
	for _, acc := range accounts {
		views = append(views, acc.AccountProfile)
	}
	return models.SuccessResponse(views)
}

func (s *AccountService) findAccount(id string) (storedAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.loadAccountsUnsafe()
	if err != nil {
		return storedAccount{}, err
	}
	for _, acc := range accounts {
		if acc.ID == id {
			return acc, nil
		}
	}
	return storedAccount{}, errors.New("Tài khoản không tồn tại.")
}

// No frontend-writable account/credential endpoint is exposed. Only verified
// service operations may create/update a stored account.
func (s *AccountService) saveAccount(acc storedAccount, mustExist bool) (AccountProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.loadAccountsUnsafe()
	if err != nil {
		return AccountProfile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	acc.UpdatedAt = now
	found := false
	for i, existing := range accounts {
		if existing.ID == acc.ID || strings.EqualFold(existing.Email, acc.Email) {
			acc.ID = existing.ID
			acc.CreatedAt = existing.CreatedAt
			if acc.Balance == nil {
				acc.Balance = existing.Balance
				acc.LastVerifiedAt = existing.LastVerifiedAt
			}
			accounts[i] = acc
			found = true
			break
		}
	}
	if !found {
		if mustExist {
			return AccountProfile{}, errors.New("Tài khoản đã bị xóa trong lúc xử lý.")
		}
		acc.CreatedAt = now
		accounts = append(accounts, acc)
	}
	if err := s.saveAccountsUnsafe(accounts); err != nil {
		return AccountProfile{}, err
	}
	return acc.AccountProfile, nil
}

func (s *AccountService) RemoveAccount(ctx context.Context, id string) models.Response[bool] {
	if !s.begin() {
		return busy[bool]()
	}
	defer s.operation.Unlock()
	s.mu.Lock()
	accounts, err := s.loadAccountsUnsafe()
	if err != nil {
		s.mu.Unlock()
		return models.ErrorResponse[bool](err.Error(), models.CodeInternal)
	}
	filtered := make([]storedAccount, 0, len(accounts))
	var selected *storedAccount
	for _, acc := range accounts {
		if acc.ID == id {
			copy := acc
			selected = &copy
		} else {
			filtered = append(filtered, acc)
		}
	}
	if selected == nil {
		s.mu.Unlock()
		return models.ErrorResponse[bool]("Tài khoản không tồn tại.", models.CodeNotFound)
	}
	profilePath, err := s.deletionPath(selected.ProfileDir)
	if err != nil {
		s.mu.Unlock()
		return models.ErrorResponse[bool](err.Error(), models.CodeInvalidArgument)
	}
	for _, other := range filtered {
		if overlappingProfiles(profilePath, other.ProfileDir) {
			s.mu.Unlock()
			return models.ErrorResponse[bool]("Profile đang được tài khoản khác sử dụng; chưa xóa dữ liệu dùng chung.", models.CodeInvalidArgument)
		}
	}
	if s.login != nil && s.login.browser != nil && strings.EqualFold(s.login.browser.ProfileDir, profilePath) {
		if s.login.completed == nil || s.login.completed.Profile.ID != id {
			s.mu.Unlock()
			return busy[bool]()
		}
		s.login.cancel()
		s.login = nil
	}
	s.mu.Unlock()
	if err := s.deleteProfile(ctx, profilePath); err != nil {
		return models.ErrorResponse[bool](err.Error(), models.CodeInternal)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveAccountsUnsafe(filtered); err != nil {
		return models.ErrorResponse[bool]("Profile đã xóa nhưng chưa lưu được danh sách tài khoản; hãy thử xóa lại: "+err.Error(), models.CodeInternal)
	}
	delete(s.nextRefresh, id)
	if s.login != nil && s.login.completed != nil && s.login.completed.Profile.ID == id {
		s.login.cancel()
		s.login = nil
	}
	return models.SuccessResponse(true)
}
