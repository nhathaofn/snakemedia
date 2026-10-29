package muse

import (
	"context"
	"strings"
	"testing"

	"SnakeMedia/internal/events"
)

// countingPage counts chat reads.
type countingPage struct {
	fakePage
	reads int
}

func (c *countingPage) Result(ctx context.Context, sid string) ([]AssistantEvent, error) {
	c.reads++
	return c.fakePage.Result(ctx, sid)
}

func TestReadChatOnceAsksThePageOnce(t *testing.T) {
	page := &countingPage{fakePage: fakePage{history: []AssistantEvent{{}, {}}}}
	chat := readChatOnce(context.Background(), MuseJob{SessionID: "s"}, page)
	job := MuseJob{RepliesSeen: 1}
	if !newReply(job, chat) || !newReply(job, chat) {
		t.Fatal("a chat with two replies has one newer than the first")
	}
	if chatHasResult(MuseJob{Kind: MuseJobImage}, chat) {
		t.Fatal("a chat without files has no result")
	}
	if page.reads != 1 {
		t.Fatalf("chat read %d times, want 1", page.reads)
	}
}

func TestBulkActionsEmitEachChangedJob(t *testing.T) {
	p := testProvider(t, &fakePage{})
	var updated []string
	p.emit = func(e events.Event, payload any) {
		if j, ok := payload.(MuseJob); ok && e == events.EventMuseJobUpdated {
			updated = append(updated, j.ID)
		}
	}
	jobs := submit(t, p, MuseJobImage, "a", "b")
	updated = nil
	for _, j := range jobs {
		if j.Held {
			t.Fatal("submitted jobs start unheld")
		}
	}
	if res := p.HoldQueued("", ""); res.Data != 2 || len(updated) != 2 {
		t.Fatalf("hold: count %d, events %d", res.Data, len(updated))
	}
	if res := p.HoldQueued("", ""); res.Data != 0 || len(updated) != 2 {
		t.Fatalf("second hold changed %d jobs", res.Data)
	}
	ids := []string{jobs[0].ID, jobs[1].ID, "missing"}
	if res := p.RunJobs(ids); res.Data != 2 || len(updated) != 4 {
		t.Fatalf("run: count %d, events %d", res.Data, len(updated))
	}
	for _, j := range jobs {
		if mustJob(t, p, j.ID).Held {
			t.Fatal("run left a job held")
		}
	}
}

func TestReleaseWaitingAttachesEachBoardOnce(t *testing.T) {
	p := testProvider(t, &fakePage{})
	board := MuseJob{ID: "board", Kind: MuseJobImage, Status: MuseJobSucceeded, Files: []string{"a", "b"}, BoardFrames: 2}
	v1 := MuseJob{ID: "v1", Kind: MuseJobVideo, Status: MuseJobQueued, After: "board", BoardFrames: 1}
	v2 := MuseJob{ID: "v2", Kind: MuseJobVideo, Status: MuseJobQueued, After: "board"}
	gone := MuseJob{ID: "v3", Kind: MuseJobVideo, Status: MuseJobQueued, After: "nothing"}
	if _, err := p.jobs.add(board, v1, v2, gone); err != nil {
		t.Fatal(err)
	}
	updated := map[string]int{}
	p.emit = func(e events.Event, payload any) {
		if j, ok := payload.(MuseJob); ok && e == events.EventMuseJobUpdated {
			updated[j.ID]++
		}
	}
	p.releaseWaiting()
	if got := mustJob(t, p, "v1").Frames; len(got) != 1 || got[0] != "a" {
		t.Fatalf("v1 frames %v", got)
	}
	if got := mustJob(t, p, "v2").Frames; len(got) != 2 {
		t.Fatalf("v2 frames %v", got)
	}
	if j := mustJob(t, p, "v3"); j.Status != MuseJobFailed || !strings.Contains(j.Error, "xoá") {
		t.Fatalf("orphan: %s %q", j.Status, j.Error)
	}
	if updated["v1"] != 1 || updated["v2"] != 1 || updated["v3"] != 1 {
		t.Fatalf("events %v", updated)
	}
}

func TestCallExpressionsBuildFullOnlyOnDemand(t *testing.T) {
	short, full, err := callExpressions("status", []any{"s"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(short, pageSource) {
		t.Fatal("short form carries the page script")
	}
	f := full()
	if !strings.Contains(f, pageSource) || !strings.Contains(f, `o["status"](...["s"])`) {
		t.Fatal("full form lacks the page script or the call")
	}
	if strings.Replace(f, pageObjectsFull(), "", 1) != strings.Replace(short, pageObjectsShort(), "", 1) {
		t.Fatal("short and full differ beyond the page object")
	}
}

func pageObjectsShort() string { s, _ := pageObjects(); return s }
func pageObjectsFull() string  { _, f := pageObjects(); return f }

func TestCapabilityTypeHelpers(t *testing.T) {
	var none *MuseCapabilities
	if none.extensions() != nil || none.videoExtensions() != nil {
		t.Fatal("nil capabilities list no extensions")
	}
	if _, ok := none.imageMime(".png"); ok {
		t.Fatal("nil capabilities know no MIME type")
	}
	c := testCaps()
	if got := strings.Join(c.extensions(), ","); got != ".jpeg,.jpg,.png" {
		t.Fatalf("extensions %q", got)
	}
	if m, ok := c.imageMime(".JPG"); !ok || m != "image/jpeg" {
		t.Fatalf("mime %q %v", m, ok)
	}
	if _, ok := c.videoMime(".png"); ok {
		t.Fatal("an image extension is no video")
	}
}
