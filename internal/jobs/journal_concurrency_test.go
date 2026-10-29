package jobs

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentUpdatesAllReachDisk(t *testing.T) {
	j, path := open(t, 0)
	const n = 64
	for i := range n {
		if err := j.Add(rec{ID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			if _, err := j.Update(fmt.Sprint(i), func(r *rec) { r.Done = true }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	reloaded := New(path, Options[rec]{})
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	for _, r := range reloaded.List() {
		if !r.Done {
			t.Fatalf("record %s lost its update", r.ID)
		}
	}
}

// BenchmarkConcurrentUpdates measures a full queue (2000 records, the Flow
// image queue's default cap) updated from 8 workers at once.
func BenchmarkConcurrentUpdates(b *testing.B) {
	j := New(filepath.Join(b.TempDir(), "jobs.json"), Options[rec]{})
	if err := j.Load(); err != nil {
		b.Fatal(err)
	}
	for i := range 2000 {
		if err := j.Add(rec{ID: fmt.Sprint(i), Step: "waiting for the provider to answer", Tags: []string{"a", "b"}}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetParallelism(8)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			if _, err := j.Update(fmt.Sprint(i%2000), func(r *rec) { r.Done = !r.Done }); err != nil {
				b.Error(err)
			}
		}
	})
}
