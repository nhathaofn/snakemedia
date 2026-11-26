package accounts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/secure"
	"SnakeMedia/pkg/utils"
)

func TestStoreMigratesLegacyFileOnFirstChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.dat")
	legacy := []Record{
		{Account: Account{ID: "a", Name: "A"}, Cookies: []browser.Cookie{{Name: "sid", Value: "1"}}},
		{Account: Account{ID: "b", Name: "B"}},
	}
	data, _ := json.Marshal(legacy)
	protected, err := secure.Protect(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := utils.WriteFileAtomic(path, append([]byte(legacyStoreHeader), protected...), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newStore(path)
	if rec, err := s.get("a"); err != nil || len(rec.Cookies) != 1 {
		t.Fatalf("legacy record not read: %+v %v", rec, err)
	}
	if _, err := s.update("b", time.Now(), func(r *Record) { r.Name = "B2" }); err != nil {
		t.Fatal(err)
	}

	reloaded := newStore(path)
	records, err := reloaded.list()
	if err != nil || len(records) != 2 || records[0].ID != "a" || records[1].Name != "B2" || len(records[0].Cookies) != 1 {
		t.Fatalf("migrated store = %+v %v", records, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw[:len(storeHeader)]) != storeHeader {
		t.Fatal("the index was not rewritten in the split format")
	}
	for _, id := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "records", id+".dat")); err != nil {
			t.Fatalf("record file for %s missing: %v", id, err)
		}
	}
}

func TestStoreUpdateRewritesOnlyThatAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.dat")
	s := newStore(path)
	for _, id := range []string{"a", "b"} {
		if err := s.add(Record{Account: Account{ID: id}, Cookies: []browser.Cookie{{Name: "sid", Value: id}}}); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	other := filepath.Join(filepath.Dir(path), "records", "b.dat")
	for _, p := range []string{path, other} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(other)

	if _, err := s.update("a", time.Now(), func(r *Record) { r.Message = "checked" }); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, other} {
		if info, _ := os.Stat(p); !info.ModTime().Equal(old) {
			t.Fatalf("%s was rewritten by another account's update", filepath.Base(p))
		}
	}
	if after, _ := os.ReadFile(other); string(after) != string(before) {
		t.Fatal("another account's record changed")
	}

	if err := s.remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "records", "a.dat")); !os.IsNotExist(err) {
		t.Fatal("a removed account's record file must be deleted")
	}
	records, err := newStore(path).list()
	if err != nil || len(records) != 1 || records[0].ID != "b" || records[0].Cookies[0].Value != "b" {
		t.Fatalf("reloaded = %+v %v", records, err)
	}
}

func TestStoreMissingRecordFileBlocksInsteadOfDroppingTheAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.dat")
	s := newStore(path)
	if err := s.add(Record{Account: Account{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(path), "records", "a.dat")); err != nil {
		t.Fatal(err)
	}
	if _, err := newStore(path).list(); err == nil {
		t.Fatal("a listed account without its record must be reported, not dropped")
	}
}

func TestNotifyCoalescesChanges(t *testing.T) {
	// Real encrypted writes can span multiple notification windows on a busy
	// runner. Advance the timer explicitly while still exercising Update and
	// checking that the delivered event contains the latest persisted state.
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := testService(t)
		seed(t, s, Record{Account: Account{ID: "a"}})
		s.notifyDelay = 250 * time.Millisecond
		var mu sync.Mutex
		count, changes := 0, 0
		var latest []Account
		snap := func() (int, int, []Account) {
			mu.Lock()
			defer mu.Unlock()
			return count, changes, latest
		}
		s.Start(t.Context(), func(e events.Event, data any) {
			if e == events.EventAccountsUpdated {
				mu.Lock()
				count++
				latest, _ = data.([]Account)
				mu.Unlock()
			}
		})
		s.OnChange(func() {
			mu.Lock()
			changes++
			mu.Unlock()
		})
		for _, message := range []string{"one", "two", "three", "four", "latest"} {
			if _, err := s.Update("a", func(r *Record) { r.Message = message }); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if gotCount, gotChanges, _ := snap(); gotCount != 0 || gotChanges != 0 {
			t.Fatal("notification fired before its coalescing window ended")
		}
		time.Sleep(s.notifyDelay)
		synctest.Wait()
		if gotCount, gotChanges, gotLatest := snap(); gotCount != 1 || gotChanges != 1 || len(gotLatest) != 1 || gotLatest[0].Message != "latest" {
			t.Fatalf("coalesced events=%d listeners=%d latest=%+v", gotCount, gotChanges, gotLatest)
		}
		time.Sleep(2 * s.notifyDelay)
		synctest.Wait()
		if gotCount, gotChanges, _ := snap(); gotCount != 1 || gotChanges != 1 {
			t.Fatal("a completed burst emitted extra notifications")
		}
		if _, err := s.Update("a", func(r *Record) { r.Message = "next" }); err != nil {
			t.Fatal(err)
		}
		time.Sleep(s.notifyDelay)
		synctest.Wait()
		if gotCount, gotChanges, gotLatest := snap(); gotCount != 2 || gotChanges != 2 || len(gotLatest) != 1 || gotLatest[0].Message != "next" {
			t.Fatalf("next burst events=%d listeners=%d latest=%+v", gotCount, gotChanges, gotLatest)
		}
	})
}

func TestStoreFromAnotherWindowsUserSaysSoInsteadOfCorrupt(t *testing.T) {
	if !secureEncrypts() {
		t.Skip("only Windows encrypts the store")
	}
	path := filepath.Join(t.TempDir(), "accounts.dat")
	// A blob DPAPI cannot open for this user, as after copying the folder.
	if err := os.WriteFile(path, []byte(storeHeader+"sealed for someone else"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newStore(path).list()
	if !errors.Is(err, secure.ErrOtherUser) || !strings.Contains(err.Error(), "người dùng Windows khác") {
		t.Fatalf("list error = %v, want it to explain the data belongs to another Windows user", err)
	}
}

func secureEncrypts() bool {
	sealed, err := secure.Protect([]byte("x"))
	return err == nil && string(sealed) != "x"
}

func TestStoreCorruptSingleRecordQuarantinesOnlyBadRecordNotIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.dat")
	s := newStore(path)
	for _, r := range []Record{
		{Account: Account{ID: "acc-1", Name: "One"}},
		{Account: Account{ID: "acc-2", Name: "Two"}},
	} {
		if err := s.add(r); err != nil {
			t.Fatal(err)
		}
	}
	badPath := filepath.Join(dir, "records", "acc-2.dat")
	if err := os.WriteFile(badPath, []byte("not encrypted json"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded := newStore(path)
	if _, err := reloaded.list(); err == nil {
		t.Fatal("expected error listing store with corrupt record")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("healthy index accounts.dat was quarantined or removed: %v", err)
	}
	if indexQuarantines, _ := filepath.Glob(path + ".corrupt-*"); len(indexQuarantines) != 0 {
		t.Fatalf("accounts.dat should not be quarantined: %v", indexQuarantines)
	}
	if badQuarantines, _ := filepath.Glob(badPath + ".corrupt-*"); len(badQuarantines) != 1 {
		t.Fatalf("expected only acc-2 record to be quarantined, got: %v", badQuarantines)
	}
	if _, err := os.Stat(filepath.Join(dir, "records", "acc-1.dat")); err != nil {
		t.Fatalf("healthy record acc-1 missing: %v", err)
	}
}
