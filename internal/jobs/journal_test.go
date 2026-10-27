package jobs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type rec struct {
	ID   string   `json:"id"`
	Done bool     `json:"done"`
	Step string   `json:"step,omitempty"`
	Tags []string `json:"tags"`
}

func (r rec) JobID() string  { return r.ID }
func (r rec) Finished() bool { return r.Done }

func open(t *testing.T, limit int) (*Journal[rec], string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "q", "jobs.json")
	j := New(path, Options[rec]{
		Limit: func() int { return limit },
		Normalize: func(r *rec) {
			if r.Tags == nil {
				r.Tags = []string{}
			}
		},
	})
	if err := j.Load(); err != nil {
		t.Fatal(err)
	}
	return j, path
}

func TestAddUpdateRemoveSurviveReload(t *testing.T) {
	j, path := open(t, 0)
	if err := j.Add(rec{ID: "a"}, rec{ID: "b"}, rec{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Add(rec{ID: "a"}); err == nil {
		t.Fatal("a duplicate ID must be refused")
	}
	got, err := j.Update("b", func(r *rec) { r.Done = true })
	if err != nil || !got.Done || got.ID != "b" {
		t.Fatalf("update returned %+v %v", got, err)
	}
	if err := j.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Update("a", func(*rec) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed record still updatable: %v", err)
	}
	if n, err := j.RemoveWhere(func(r rec) bool { return r.ID == "zzz" }); n != 0 || err != nil {
		t.Fatalf("RemoveWhere with no match: %d %v", n, err)
	}

	again := New(path, Options[rec]{Normalize: func(r *rec) {
		if r.Tags == nil {
			r.Tags = []string{}
		}
	}})
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	list := again.List()
	if len(list) != 2 || list[0].ID != "b" || !list[0].Done || list[1].ID != "c" || list[1].Tags == nil {
		t.Fatalf("reloaded %+v", list)
	}
	if c, ok := again.Get("c"); !ok || c.ID != "c" {
		t.Fatal("index not rebuilt after load")
	}
	snap := again.Snapshot([]string{"b", "gone"})
	if len(snap) != 1 || snap["b"].ID != "b" {
		t.Fatalf("snapshot %+v", snap)
	}
}

func TestTrimDropsOnlyFinishedAndKeepsIndex(t *testing.T) {
	j, _ := open(t, 2)
	if err := j.Add(rec{ID: "u1"}, rec{ID: "u2"}, rec{ID: "u3"}); err != nil {
		t.Fatal(err)
	}
	if len(j.List()) != 3 {
		t.Fatal("unfinished records must never be trimmed")
	}
	// Updating over the cap neither trims nor shifts the record returned.
	for _, id := range []string{"u1", "u2", "u3"} {
		got, err := j.Update(id, func(r *rec) { r.Done = true })
		if err != nil || got.ID != id {
			t.Fatalf("update %s returned %+v %v", id, got, err)
		}
	}
	if len(j.List()) != 3 {
		t.Fatal("Update must not trim")
	}
	if err := j.Add(rec{ID: "n"}); err != nil {
		t.Fatal(err)
	}
	list := j.List()
	if len(list) != 2 || list[0].ID != "u3" || list[1].ID != "n" {
		t.Fatalf("expected the oldest finished trimmed, got %+v", list)
	}
	if _, ok := j.Get("u1"); ok {
		t.Fatal("trimmed record still indexed")
	}
	if r, ok := j.Get("n"); !ok || r.ID != "n" {
		t.Fatal("index wrong after trim")
	}
}

func TestTransientUpdateIsNotWritten(t *testing.T) {
	j, path := open(t, 0)
	if err := j.Add(rec{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	got, err := j.UpdateTransient("a", func(r *rec) { r.Step = "đang chạy" })
	if err != nil || got.Step != "đang chạy" {
		t.Fatalf("transient update %+v %v", got, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("a transient update must not touch the file")
	}
	if r, _ := j.Get("a"); r.Step != "đang chạy" {
		t.Fatal("transient value lost in memory")
	}
	if _, err := j.Update("a", func(r *rec) { r.Done = true }); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	if string(written) == string(after) {
		t.Fatal("a real update must be written")
	}
}

func TestCorruptJournalIsMovedAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := New(path, Options[rec]{})
	err := j.Load()
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
	if len(j.List()) != 0 {
		t.Fatal("a corrupt journal starts empty")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("the corrupt file must be moved aside")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("expected one backup, got %v", matches)
	}
	if err := j.Add(rec{ID: "a"}); err != nil {
		t.Fatal("the journal must work after a reset:", err)
	}
}

// An Update the Transient option calls progress only stays in memory; the
// next real change writes it too.
func TestTransientOptionSkipsProgressWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	j := New(path, Options[rec]{Transient: func(a, b rec) bool {
		a.Step, b.Step = "", ""
		return a.ID == b.ID && a.Done == b.Done
	}})
	if err := j.Load(); err != nil {
		t.Fatal(err)
	}
	if err := j.Add(rec{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := j.Update("a", func(r *rec) { r.Step = "đang chạy" }); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("a progress-only update was written")
	}
	if got, _ := j.Get("a"); got.Step != "đang chạy" {
		t.Fatalf("progress lost in memory: %+v", got)
	}
	if _, err := j.Update("a", func(r *rec) { r.Done = true }); err != nil {
		t.Fatal(err)
	}
	reloaded := New[rec](path, Options[rec]{})
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.Get("a"); !got.Done || got.Step != "đang chạy" {
		t.Fatalf("after reload = %+v", got)
	}
}
