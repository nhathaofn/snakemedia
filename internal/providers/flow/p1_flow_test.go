package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
)

func TestEditVideosRejectsQuarantinedJournalAndNormalizesUUID(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "edit-q@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }

	src := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(src, []byte("MP4"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported := service.media.ImportVideo(service.SourceVideoExtensions(), func() (string, error) { return src, nil })
	if !imported.Success {
		t.Fatal(imported.Error)
	}

	// Uppercase RequestID gets normalized to lowercase before calling editLiveVideos.
	upperID := "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE"
	var gotReqID string
	service.editLiveVideos = func(_ context.Context, _ session.StoredAccount, req EditVideoRequest, _ localVideo, _ videoFamily) ([]GeneratedVideoItem, error) {
		gotReqID = req.RequestID
		return []GeneratedVideoItem{{ID: "ok-1"}}, nil
	}
	res := service.EditVideos(context.Background(), EditVideoRequest{
		RequestID: upperID,
		Prompt:    "warm light",
		ModelID:   "abra",
		SourceURL: imported.Data.LocalURL,
	})
	if !res.Success || gotReqID != strings.ToLower(upperID) {
		t.Fatalf("EditVideos = %+v gotReqID=%q, want lowercase %q", res, gotReqID, strings.ToLower(upperID))
	}

	// Quarantined edit journal must block re-sending the same RequestID.
	corruptID := "11111111-2222-4333-8444-555555555555"
	basePath, err := service.statePath("jobs", corruptID)
	if err != nil {
		t.Fatal(err)
	}
	corruptPath := basePath + ".corrupt-20260101"
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corruptPath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	service.editLiveVideos = func(context.Context, session.StoredAccount, EditVideoRequest, localVideo, videoFamily) ([]GeneratedVideoItem, error) {
		called = true
		return nil, nil
	}
	qRes := service.EditVideos(context.Background(), EditVideoRequest{
		RequestID: strings.ToUpper(corruptID),
		Prompt:    "warm light",
		ModelID:   "abra",
		SourceURL: imported.Data.LocalURL,
	})
	if qRes.Success || qRes.Code != models.CodeInternal || called {
		t.Fatalf("quarantined edit journal was not blocked: res=%+v called=%v", qRes, called)
	}
}

func TestEditVideosCommitsCreditOnceAndRefreshesAfterRelease(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "edit-credit@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }

	src := filepath.Join(t.TempDir(), "clip.mp4")
	_ = os.WriteFile(src, []byte("MP4"), 0o600)
	imported := service.media.ImportVideo(service.SourceVideoExtensions(), func() (string, error) { return src, nil })
	if !imported.Success {
		t.Fatal(imported.Error)
	}

	var refreshSawActive int
	var refreshCount int
	service.refreshLiveCredits = func(_ context.Context, id string) {
		refreshCount++
		service.rotationMu.Lock()
		if st := service.rotationAccounts[id]; st != nil {
			refreshSawActive = st.active
		}
		service.rotationMu.Unlock()
	}
	service.editLiveVideos = func(_ context.Context, _ session.StoredAccount, req EditVideoRequest, _ localVideo, _ videoFamily) ([]GeneratedVideoItem, error) {
		if req.onAccepted == nil {
			t.Fatal("expected EditVideos to wire req.onAccepted to lease.commitVideoSpend")
		}
		req.onAccepted()
		req.onAccepted() // idempotent
		return []GeneratedVideoItem{{ID: "ed-1"}}, nil
	}

	res := service.EditVideos(context.Background(), EditVideoRequest{
		Prompt:    "sunset",
		ModelID:   "abra",
		SourceURL: imported.Data.LocalURL,
	})
	if !res.Success {
		t.Fatalf("EditVideos failed: %+v", res)
	}
	if refreshCount != 1 || refreshSawActive != 0 {
		t.Fatalf("refreshCount=%d refreshSawActive=%d, want 1 and 0 (after lease release)", refreshCount, refreshSawActive)
	}
	service.rotationMu.Lock()
	st := service.rotationAccounts[acc.ID]
	reserved, spent := 0, 0
	if st != nil {
		reserved, spent = st.videoReserved, st.videoSpent
	}
	service.rotationMu.Unlock()
	if reserved != 0 || spent != 20 {
		t.Fatalf("rotation state after commit+release: reserved=%d spent=%d, want 0 and 20", reserved, spent)
	}
}

func TestGenerateVideosDoesNotOverwriteAcceptedJobAsRejected(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "vid-acc@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return editModelsPayload, nil }

	reqID := "22222222-3333-4444-8555-666666666666"
	service.generateLiveVideos = func(_ context.Context, a session.StoredAccount, req GenerateVideoRequest, _ string) ([]GeneratedVideoItem, error) {
		// Simulate server already accepted the job and persisted generationAccepted,
		// then polling/download returned a later error.
		job := generationJob{
			ID:          req.RequestID,
			Kind:        generationJobVideo,
			Fingerprint: "test-fp",
			Phase:       generationAccepted,
			AccountID:   a.ID,
			ProjectID:   "proj-1",
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		if err := service.saveGenerationJob(job); err != nil {
			t.Fatal(err)
		}
		// Even a definite RPC rejection returned after the job was already
		// accepted must never overwrite generationAccepted to generationRejected.
		return nil, &chrome.RPCError{ID: "test", Code: 3, Detail: "[3,\"INVALID_ARGUMENT\"]"}
	}

	res := service.GenerateVideos(context.Background(), GenerateVideoRequest{
		RequestID:   reqID,
		Prompt:      "ocean waves",
		ModelID:     "veo_3_1_fast",
		AspectRatio: "16:9",
		Duration:    8,
		Resolution:  "720p",
		Count:       1,
	})
	if !res.Success || len(res.Data) != 1 || !res.Data[0].Pending {
		t.Fatalf("expected resumable Pending response for already-accepted job, got %+v", res)
	}
	saved, err := service.loadGenerationJob(reqID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Phase != generationAccepted {
		t.Fatalf("already-accepted video job overwritten to phase=%q, want %q (acc=%s)", saved.Phase, generationAccepted, acc.ID)
	}
}

func TestFlightGroupDoesNotDeadlockOnPanic(t *testing.T) {
	var fg flightGroup
	started := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { _ = recover() }()
		_, _ = fg.do("k", func() (any, error) {
			close(started)
			<-release
			panic("boom")
		})
	}()

	<-started
	waiterDone := make(chan any, 1)
	go func() {
		v, _ := fg.do("k", func() (any, error) { return "second", nil })
		waiterDone <- v
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	select {
	case <-waiterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter deadlocked after leader panicked inside flightGroup.do")
	}

	// Next call on the same key runs fresh without sticking to the panicked flight.
	got, err := fg.do("k", func() (any, error) { return "recovered", nil })
	if err != nil || got != "recovered" {
		t.Fatalf("after panic flightGroup.do = (%v, %v), want recovered", got, err)
	}
}

func TestCancelImageTaskCancelsRunningContextEvenAcrossUpdateRace(t *testing.T) {
	h := newQueueHarness(t)
	task := h.submit("cancel race")[0]
	var canceled atomic.Bool
	h.q.mu.Lock()
	h.q.running[task.ID] = func() { canceled.Store(true) }
	h.q.mu.Unlock()
	if _, err := h.q.journal.Update(task.ID, func(it *ImageTask) {
		it.Status = ImageTaskRunning
	}); err != nil {
		t.Fatal(err)
	}

	res := h.s.CancelImageTask(task.ID)
	if !res.Success {
		t.Fatalf("CancelImageTask = %+v", res)
	}
	if !canceled.Load() {
		t.Fatal("expected running cancel func to be invoked")
	}
}
