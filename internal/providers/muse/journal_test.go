package muse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalIndexFollowsAddTrimAndRemove(t *testing.T) {
	s := newJobStore(filepath.Join(t.TempDir(), "jobs.json"), func() int { return 3 })
	for _, id := range []string{"a", "b", "c", "d"} {
		status := MuseJobQueued
		if id == "a" || id == "b" {
			status = MuseJobSucceeded
		}
		if _, err := s.add(MuseJob{ID: id, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.get("a"); ok {
		t.Fatal("the oldest finished job should be trimmed over the cap")
	}
	for _, id := range []string{"b", "c", "d"} {
		if j, ok := s.get(id); !ok || j.ID != id {
			t.Fatalf("get(%s) = %+v, %v", id, j, ok)
		}
	}
	if err := s.remove("c"); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.removeWhere(func(j MuseJob) bool { return j.ID == "b" }); len(ids) != 1 || err != nil {
		t.Fatalf("removeWhere = %d, %v", len(ids), err)
	}
	if j, ok := s.get("d"); !ok || j.ID != "d" {
		t.Fatalf("index not rebuilt after removal: %+v, %v", j, ok)
	}
	if _, ok := s.get("c"); ok {
		t.Fatal("removed job still found")
	}
	got := s.snapshot([]string{"d", "c", "missing"})
	if len(got) != 1 || got["d"].ID != "d" {
		t.Fatalf("snapshot = %+v", got)
	}

	reloaded := newJobStore(s.path, func() int { return 3 })
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if j, ok := reloaded.get("d"); !ok || j.ID != "d" {
		t.Fatalf("index not built on load: %+v, %v", j, ok)
	}
}

func TestStepOnlyUpdateIsNotWritten(t *testing.T) {
	s := newJobStore(filepath.Join(t.TempDir(), "jobs.json"), func() int { return 100 })
	if _, err := s.add(MuseJob{ID: "a", Status: MuseJobProcessing}); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour)
	if err := os.Chtimes(s.path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.path)

	job, err := s.update("a", func(j *MuseJob) { j.Step = "Tải file" })
	if err != nil || job.Step != "Tải file" {
		t.Fatalf("update = %+v, %v", job, err)
	}
	after, _ := os.ReadFile(s.path)
	info, _ := os.Stat(s.path)
	if string(before) != string(after) || !info.ModTime().Equal(stamp) {
		t.Fatal("a Step-only change must not rewrite the journal")
	}
	if j, _ := s.get("a"); j.Step != "Tải file" {
		t.Fatal("the Step change must stay in memory")
	}

	if _, err := s.update("a", func(j *MuseJob) { j.SessionID = "s1" }); err != nil {
		t.Fatal(err)
	}
	reloaded := newJobStore(s.path, func() int { return 100 })
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if j, _ := reloaded.get("a"); j.SessionID != "s1" || j.Step != "Tải file" {
		t.Fatalf("a state change must be written with the pending Step: %+v", j)
	}
}

func TestCorruptJournalIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newJobStore(path, func() int { return 100 })
	err := s.load()
	if !errors.Is(err, errJournalCorrupt) {
		t.Fatalf("load = %v, want errJournalCorrupt", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("the corrupt journal should be moved aside")
	}
	if len(s.list()) != 0 {
		t.Fatal("a corrupt journal starts an empty queue")
	}
	if _, err := s.add(MuseJob{ID: "a", Status: MuseJobQueued}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.get("a"); !ok {
		t.Fatal("the queue must work after a reset")
	}
}
