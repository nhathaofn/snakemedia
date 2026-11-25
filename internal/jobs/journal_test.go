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

func TestUpdateManySavesOnceAndReturnsChanged(t *testing.T) {
	j, path := open(t, 0)
	if err := j.Add(rec{ID: "a"}, rec{ID: "b"}, rec{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	got, err := j.UpdateMany([]string{"c", "missing", "a", "b"}, func(r *rec) bool {
		if r.ID == "b" {
			return false
		}
		r.Done = true
		return true
	})
	if err != nil || len(got) != 2 || got[0].ID != "c" || got[1].ID != "a" {
		t.Fatalf("UpdateMany returned %+v %v", got, err)
	}
	again := New(path, Options[rec]{})
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	for id, done := range map[string]bool{"a": true, "b": false, "c": true} {
		if r, _ := again.Get(id); r.Done != done {
			t.Fatalf("%s after reload: %+v", id, r)
		}
	}
}

func TestSaveBatchAddsAndUpdatesAtomically(t *testing.T) {
	j, path := open(t, 0)
	if err := j.Add(rec{ID: "a", Step: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := j.SaveBatch([]rec{{ID: "b", Step: "new"}}, []rec{{ID: "a", Done: true, Step: "updated"}}); err != nil {
		t.Fatal(err)
	}
	if err := j.SaveBatch([]rec{{ID: "c"}}, []rec{{ID: "missing"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update should fail with ErrNotFound, got %v", err)
	}
	if _, ok := j.Get("c"); ok {
		t.Fatal("failed SaveBatch mutated in-memory state")
	}
	again := New(path, Options[rec]{})
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if a, ok := again.Get("a"); !ok || !a.Done || a.Step != "updated" {
		t.Fatalf("a after reload: %+v", a)
	}
	if b, ok := again.Get("b"); !ok || b.Step != "new" {
		t.Fatalf("b after reload: %+v", b)
	}
}

func TestListActiveTracksTransitionsAndOrder(t *testing.T) {
	j, _ := open(t, 10)
	if len(j.ListActive()) != 0 {
		t.Fatal("empty journal should have no active records")
	}
	if err := j.Add(rec{ID: "a"}, rec{ID: "b", Done: true}, rec{ID: "c"}, rec{ID: "d"}); err != nil {
		t.Fatal(err)
	}
	got := j.ListActive()
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "d" {
		t.Fatalf("initial active = %+v, want [a c d]", got)
	}
	if _, err := j.Update("c", func(r *rec) { r.Done = true }); err != nil {
		t.Fatal(err)
	}
	if _, err := j.UpdateMany([]string{"a", "b"}, func(r *rec) bool {
		r.Done = !r.Done
		return true
	}); err != nil {
		t.Fatal(err)
	}
	// b became active again, d stayed active, a and c are now finished.
	got = j.ListActive()
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "d" {
		t.Fatalf("after transitions = %+v, want [b d]", got)
	}
	if err := j.Remove("b"); err != nil {
		t.Fatal(err)
	}
	got = j.ListActive()
	if len(got) != 1 || got[0].ID != "d" {
		t.Fatalf("after remove = %+v, want [d]", got)
	}
}

func TestJournalNewerFormatAndDiskErrorNeverPolluteRAM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")
	j := New(path, Options[rec]{})
	if err := j.Load(); err != nil {
		t.Fatal(err)
	}
	if err := j.Add(rec{ID: "a", Step: "v1"}); err != nil {
		t.Fatal(err)
	}
	// Simulate disk failure via directory blocker on another journal path.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blockedPath := filepath.Join(blocker, "jobs.json")
	jb := New(blockedPath, Options[rec]{})
	if _, err := jb.AddTrimming(rec{ID: "ghost"}); err == nil {
		t.Fatal("expected AddTrimming to fail on blocked path")
	}
	if got := jb.List(); len(got) != 0 {
		t.Fatalf("RAM polluted after failed AddTrimming: %+v", got)
	}
	if got := jb.ListActive(); len(got) != 0 {
		t.Fatalf("active polluted after failed AddTrimming: %+v", got)
	}

	// Simulate write failure on existing loaded journal by swapping j.path behind blocker.
	j.mu.Lock()
	j.path = blockedPath
	j.mu.Unlock()
	if _, err := j.Update("a", func(r *rec) { r.Done = true; r.Step = "v2-dirty" }); err == nil {
		t.Fatal("expected Update to fail")
	}
	gotA, ok := j.Get("a")
	if !ok || gotA.Done || gotA.Step != "v1" || len(j.ListActive()) != 1 {
		t.Fatalf("Update failed but mutated RAM: %+v active=%v", gotA, j.ListActive())
	}
	if err := j.Remove("a"); err == nil {
		t.Fatal("expected Remove to fail")
	}
	if gotA, ok := j.Get("a"); !ok || len(j.List()) != 1 || len(j.ListActive()) != 1 {
		t.Fatalf("Remove failed but mutated RAM: ok=%v got=%+v", ok, gotA)
	}
	if _, err := j.UpdateMany([]string{"a"}, func(r *rec) bool { r.Step = "dirty"; return true }); err == nil {
		t.Fatal("expected UpdateMany to fail")
	}
	if gotA, _ := j.Get("a"); gotA.Step != "v1" {
		t.Fatalf("UpdateMany failed but mutated RAM: %+v", gotA)
	}
	if _, err := j.RemoveWhereIDs(func(r rec) bool { return r.ID == "a" }); err == nil {
		t.Fatal("expected RemoveWhereIDs to fail")
	}
	if _, ok := j.Get("a"); !ok || len(j.ListActive()) != 1 {
		t.Fatalf("RemoveWhereIDs failed but dropped RAM record")
	}

	// Newer-format blocked journal must reject every mutation without altering RAM.
	j.mu.Lock()
	j.blocked = ErrNewerFormat
	j.mu.Unlock()
	for name, err := range map[string]error{
		"Add": func() error { return j.Add(rec{ID: "ghost-2"}) }(),
		"Update": func() error {
			_, e := j.Update("a", func(r *rec) { r.Step = "blocked" })
			return e
		}(),
		"UpdateMany": func() error {
			_, e := j.UpdateMany([]string{"a"}, func(r *rec) bool { r.Step = "blocked"; return true })
			return e
		}(),
		"Remove": j.Remove("a"),
		"RemoveWhereIDs": func() error {
			_, e := j.RemoveWhereIDs(func(rec) bool { return true })
			return e
		}(),
	} {
		if !errors.Is(err, ErrNewerFormat) {
			t.Fatalf("%s = %v, want ErrNewerFormat", name, err)
		}
	}
	if got := j.List(); len(got) != 1 || got[0].Step != "v1" {
		t.Fatalf("newer-format mutations touched RAM: %+v", got)
	}
}
