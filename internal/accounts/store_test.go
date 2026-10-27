package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
	s, _, _ := testService(t)
	s.notifyDelay = 250 * time.Millisecond
	var mu sync.Mutex
	count := 0
	done := make(chan struct{}, 4)
	s.Start(t.Context(), func(e events.Event, _ any) {
		if e == events.EventAccountsUpdated {
			mu.Lock()
			count++
			mu.Unlock()
			done <- struct{}{}
		}
	})
	seed(t, s, Record{Account: Account{ID: "a"}})
	for range 5 {
		if _, err := s.Update("a", func(r *Record) { r.Message = "x" }); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("no accounts:updated event")
	}
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("want one coalesced event, got %d", count)
	}
}
