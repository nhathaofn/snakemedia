package flow

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/events"
)

func TestBulkHoldWritesOnceAndPublishesEachTask(t *testing.T) {
	h := newQueueHarness(t)
	var published atomic.Int32
	h.q.emit = func(events.Event, any) { published.Add(1) }
	h.s.SubmitImageTasks(ImageTaskRequest{Prompts: []string{"a", "b", "c"}, ModelID: "m", OutputDir: h.dir, Hold: true})
	before, _ := os.Stat(h.q.journal.Path())
	published.Store(0)
	time.Sleep(20 * time.Millisecond)
	ids := []string{}
	for _, task := range h.q.journal.List() {
		ids = append(ids, task.ID, task.ID) // duplicates count once
	}
	if n := h.s.RunImageTasks(ids); n.Data != 3 {
		t.Fatalf("run %+v", n)
	}
	if got := published.Load(); got != 3 {
		t.Fatalf("each changed task is published once, got %d", got)
	}
	after, _ := os.Stat(h.q.journal.Path())
	if before != nil && !after.ModTime().After(before.ModTime()) {
		t.Fatalf("the hold change must reach disk")
	}
	if n := h.s.RunImageTasks(nil); n.Data != 0 {
		t.Fatalf("running tasks already released changes nothing: %+v", n)
	}
}

func TestStepOnlyChangeStaysInMemory(t *testing.T) {
	a := ImageTask{ID: "x", Status: ImageTaskRunning, Results: []GeneratedImageItem{}}
	b := a
	b.Step = "đang chạy"
	if !onlyImageStepChanged(a, b) {
		t.Fatal("a Step-only change is transient")
	}
	b.Status = ImageTaskFailed
	if onlyImageStepChanged(a, b) {
		t.Fatal("a status change must be saved")
	}
}

func TestWorkflowListingMatchesPerClientParse(t *testing.T) {
	payload := `[null,[["wf-1",null,null,["t",1,null,null,"media-1","Seed-A"]],["wf-2",null,null,["t",1,null,null,"media-2","seed-a"]],["wf-3",null,null,["t",1,null,null,"media-3","seed-b"]]]]`
	l := parseWorkflows(payload)
	if got := l.forSeed("SEED-A"); len(got) != 2 || got[0].name != "media-1" || got[1].operation != "wf-2" || got[0].clientID != "SEED-A" {
		t.Fatalf("seed matches %+v", got)
	}
	if got := mediaForClient(payload, "seed-b"); got.name != "media-3" {
		t.Fatalf("client match %+v", got)
	}
	if got := mediaForSeed(payload, ""); got != nil || l.forClient("") != (pendingVideo{}) || parseWorkflows("bad") != nil {
		t.Fatal("empty seed or bad payload matches nothing")
	}
}

func TestCreditRefreshCoalesces(t *testing.T) {
	s := &Provider{}
	// A run in progress: later requests only mark one trailing rerun.
	s.creditRefreshes = map[string]*creditRefresh{"a": {}}
	for range 5 {
		s.coalesceCreditRefresh(context.Background(), "a")
	}
	if s.creditRefreshes["a"].pending == nil || len(s.creditRefreshes) != 1 {
		t.Fatalf("requests during a run fold into one pending rerun: %+v", s.creditRefreshes)
	}
	// Without accounts a refresh returns at once; the account is then freed.
	s.coalesceCreditRefresh(context.Background(), "b")
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.creditRefreshMu.Lock()
		_, running := s.creditRefreshes["b"]
		s.creditRefreshMu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a finished refresh frees the account")
		}
		time.Sleep(time.Millisecond)
	}
}
