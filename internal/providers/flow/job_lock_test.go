package flow

import (
	"sync"
	"sync/atomic"
	"testing"
)

// A request ID is followed by one call at a time, and a released ID leaves no
// entry behind however many requests ran.
func TestJobLockRefusesSecondCallAndForgetsReleasedIDs(t *testing.T) {
	s := &Provider{}
	release, ok := s.tryJobLock("a")
	if !ok {
		t.Fatal("first claim refused")
	}
	if _, ok := s.tryJobLock("a"); ok {
		t.Fatal("second call with the same request ID was let through")
	}
	if other, ok := s.tryJobLock("b"); !ok {
		t.Fatal("another request ID was refused")
	} else {
		other()
	}
	release()

	var wg sync.WaitGroup
	var inside, overlap atomic.Int32
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if release, ok := s.tryJobLock("a"); ok {
				if inside.Add(1) > 1 {
					overlap.Add(1)
				}
				inside.Add(-1)
				release()
			}
		}()
	}
	wg.Wait()
	if overlap.Load() != 0 {
		t.Fatal("two calls held the same request ID at once")
	}
	if n := len(s.jobLocks); n != 0 {
		t.Fatalf("%d released IDs left behind", n)
	}
}
