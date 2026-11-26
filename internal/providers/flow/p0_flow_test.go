package flow

import (
	"context"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/models"
)

func TestUpsample4KFailureKeepsJobOpenWithout1KFallback(t *testing.T) {
	h := newQueueHarness(t)
	s := h.s
	items := []GeneratedImageItem{{
		ID:     "media-4k-1",
		URL:    "https://storage.googleapis.com/flow/1k.png",
		Prompt: "high res",
	}}
	req := GenerateImageRequest{
		Prompt:      "high res",
		ModelID:     "model-1",
		AspectRatio: "1:1",
		Quality:     ImageQuality4K,
		Count:       1,
	}

	// Browser is nil so 4K upsample fails; materializeGeneratedImages must NOT
	// populate LocalURL with a 1K fallback, keeping imageJobPhase open (generationAccepted).
	out := s.materializeGeneratedImages(context.Background(), nil, nil, config.DefaultFlowPagePolicy(), "proj-1", req, items)
	if len(out) != 1 || out[0].LocalURL != "" || out[0].Error == "" {
		t.Fatalf("4K upsample failure must leave LocalURL empty with error: %+v", out)
	}
	if phase := imageJobPhase(out); phase != generationAccepted {
		t.Fatalf("imageJobPhase = %q, want %q so resume retries upsampling", phase, generationAccepted)
	}

	// In the batch queue, settling an open generationAccepted task with failed upsample
	// must schedule a recheck rather than marking the task succeeded with 1K output.
	task := h.submit("4k batch")[0]
	task, err := h.q.journal.Update(task.ID, func(it *ImageTask) {
		it.Status, it.RequestID, it.Quality = ImageTaskRunning, recoveryRequestID, ImageQuality4K
	})
	if err != nil {
		t.Fatal(err)
	}
	h.setPhase(recoveryRequestID, generationAccepted)
	h.q.settle(context.Background(), task, models.SuccessResponse(out))
	got, _ := h.q.journal.Get(task.ID)
	if got.Status != ImageTaskQueued || got.RequestID != recoveryRequestID || len(got.SavedFiles) != 0 {
		t.Fatalf("expected failed 4K upsample to recheck under same RequestID with no 1K saved, got %+v", got)
	}
}

func TestUpsampleResponseLimitExceeds4KBase64Envelope(t *testing.T) {
	const maxImage = int64(32 << 20)
	limit := upsampleResponseLimit(4<<20, maxImage)
	minExpected := int64(base64.StdEncoding.EncodedLen(int(maxImage)))
	if limit <= minExpected {
		t.Fatalf("upsampleResponseLimit = %d, must exceed base64 size %d", limit, minExpected)
	}
}

func TestMintLockSerializesConcurrentMintsPerBrowserPort(t *testing.T) {
	s := New(nil, nil, nil)
	const port = 9222
	var active atomic.Int32
	var maxConcurrent atomic.Int32

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mint := s.mintLock(port)
			mint.Lock()
			page := s.pageLock(port)
			page.RLock()
			cur := active.Add(1)
			for {
				prev := maxConcurrent.Load()
				if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(15 * time.Millisecond)
			active.Add(-1)
			page.RUnlock()
			mint.Unlock()
		}()
	}
	wg.Wait()
	if maxConcurrent.Load() != 1 {
		t.Fatalf("concurrent mints overlapped on same port: maxConcurrent=%d, want 1", maxConcurrent.Load())
	}
}
