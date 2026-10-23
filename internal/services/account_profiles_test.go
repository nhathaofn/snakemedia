package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func populateProfile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "Default", "Cache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Default", "Cache", "test-data"), []byte("test profile data"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveAccountDeletesProfileAndKeepsOtherProfiles(t *testing.T) {
	s := testAccountService(t)
	first := seedAccount(t, s, "first@example.invalid")
	second := seedAccount(t, s, "second@example.invalid")
	populateProfile(t, first.ProfileDir)
	populateProfile(t, second.ProfileDir)
	if res := s.RemoveAccount(context.Background(), first.ID); !res.Success {
		t.Fatal(res.Error)
	}
	if _, err := os.Stat(first.ProfileDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted profile directory still exists")
	}
	if _, err := os.Stat(second.ProfileDir); err != nil {
		t.Fatal("another profile was removed")
	}
	res := s.GetAccounts(context.Background())
	if !res.Success || len(res.Data) != 1 || res.Data[0].ID != second.ID {
		t.Fatal("incorrect saved account list")
	}
}

func TestRemoveAccountRejectsOutsideAndOverlappingProfiles(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside", true: "shared descendant"}[shared], func(t *testing.T) {
			s := testAccountService(t)
			first := seedAccount(t, s, "first@example.invalid")
			stored, err := s.findAccount(first.ID)
			if err != nil {
				t.Fatal(err)
			}
			if shared {
				second := seedAccount(t, s, "second@example.invalid")
				other, err := s.findAccount(second.ID)
				if err != nil {
					t.Fatal(err)
				}
				other.ProfileDir = filepath.Join(first.ProfileDir, "nested")
				if _, err := s.saveAccount(other, true); err != nil {
					t.Fatal(err)
				}
			} else {
				stored.ProfileDir = t.TempDir()
				if _, err := s.saveAccount(stored, true); err != nil {
					t.Fatal(err)
				}
			}
			populateProfile(t, stored.ProfileDir)
			if res := s.RemoveAccount(context.Background(), first.ID); res.Success {
				t.Fatal("unsafe deletion accepted")
			}
			if _, err := os.Stat(stored.ProfileDir); err != nil {
				t.Fatal("unsafe target modified")
			}
			if _, err := s.findAccount(first.ID); err != nil {
				t.Fatal("account lost after rejected deletion")
			}
		})
	}
}

func TestDeletionRejectsLinksAndRoot(t *testing.T) {
	s := testAccountService(t)
	root := filepath.Join(s.paths.DataDir, "profiles")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.deletionPath(root); err == nil {
		t.Fatal("profiles root accepted")
	}
	outside := t.TempDir()
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Skip("OS does not permit test symlink")
	}
	if _, err := s.deletionPath(linked); err == nil {
		t.Fatal("linked profile accepted")
	}
}
