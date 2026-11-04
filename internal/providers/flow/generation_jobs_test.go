package flow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/providers/flow/chrome"
)

// ageState backdates a saved record so it looks idle for d.
func ageState(t *testing.T, s *Provider, kind, id string, d time.Duration) {
	t.Helper()
	path, err := s.statePath(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestPendingGenerationJobsSurviveCorruptJournalsAndExpire(t *testing.T) {
	s := testFlowService(testAccountService(t))
	retention := s.jobRetention()

	pending := newGenerationJob("11111111-1111-4111-8111-111111111111", generationJobImage, "fp", "acc")
	pending.Phase = generationAccepted
	stale := newGenerationJob("22222222-2222-4222-8222-222222222222", generationJobImage, "fp", "acc")
	stale.Phase = generationSubmitting
	done := newGenerationJob("33333333-3333-4333-8333-333333333333", generationJobImage, "fp", "acc")
	done.Phase = generationComplete
	for _, job := range []generationJob{pending, stale, done} {
		if err := s.saveGenerationJob(job); err != nil {
			t.Fatal(err)
		}
	}
	ageState(t, s, "generation-jobs", stale.ID, retention+time.Hour)
	ageState(t, s, "generation-jobs", done.ID, retention+time.Hour)
	corrupt := filepath.Join(s.stateDir("generation-jobs"), "44444444-4444-4444-8444-444444444444.json")
	if err := os.WriteFile(corrupt, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := s.ListPendingImageGenerations(ImageProjectModeSingle)
	if !res.Success {
		t.Fatalf("one corrupt journal must not break recovery: %s", res.Error)
	}
	if len(res.Data) != 1 || res.Data[0].JobID != pending.ID {
		t.Fatalf("want only the live pending job, got %+v", res.Data)
	}
	if _, err := s.loadGenerationJob(done.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("finished journal past retention was not deleted: %v", err)
	}
	closed, err := s.loadGenerationJob(stale.ID)
	if err != nil || closed.Phase != generationRejected {
		t.Fatalf("idle unconfirmed job was not closed: %+v %v", closed, err)
	}
}

func TestListVideoEditJobsSkipsCorruptAndClosesAbandoned(t *testing.T) {
	s := testFlowService(testAccountService(t))
	retention := s.jobRetention()
	abandoned := editJob{ID: "55555555-5555-4555-8555-555555555555", Template: GeneratedVideoItem{Prompt: "a fox"}}
	finished := editJob{ID: "66666666-6666-4666-8666-666666666666", Complete: true}
	for _, job := range []editJob{abandoned, finished} {
		if err := s.saveState("jobs", job.ID, job); err != nil {
			t.Fatal(err)
		}
		ageState(t, s, "jobs", job.ID, retention+time.Hour)
	}
	if err := os.WriteFile(filepath.Join(s.stateDir("jobs"), "77777777-7777-4777-8777-777777777777.json"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := s.ListVideoEditJobs(context.Background())
	if !res.Success || len(res.Data) != 1 {
		t.Fatalf("want the abandoned job only, got %+v", res)
	}
	if res.Data[0].Pending || res.Data[0].Error == "" {
		t.Fatalf("abandoned edit must be closed with a reason: %+v", res.Data[0])
	}
	if _, err := os.Stat(filepath.Join(s.stateDir("jobs"), finished.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("finished edit journal past retention was not deleted: %v", err)
	}
}

func TestImageJobStaysOpenUntilEveryImageIsDownloaded(t *testing.T) {
	downloaded := GeneratedImageItem{URL: "https://flow/1", LocalURL: "/flow-media/1.png"}
	failed := GeneratedImageItem{URL: "https://flow/2", Error: "Không tải được ảnh để hiển thị"}
	rejected := GeneratedImageItem{Error: "bộ lọc an toàn"}
	if got := imageJobPhase([]GeneratedImageItem{downloaded, failed}); got != generationAccepted {
		t.Fatalf("a failed download must keep the job open, got %s", got)
	}
	if got := imageJobPhase([]GeneratedImageItem{downloaded, rejected}); got != generationComplete {
		t.Fatalf("Flow-side failures are final, got %s", got)
	}
}

func TestListPendingImageGenerationsFiltersByStudio(t *testing.T) {
	s := testFlowService(testAccountService(t))
	single := newGenerationJob("88888888-8888-4888-8888-888888888888", generationJobImage, "fp", "acc")
	single.Phase = generationAccepted // no ProjectMode: journals from before the field are Single
	all := newGenerationJob("99999999-9999-4999-8999-999999999999", generationJobImage, "fp", "acc")
	all.Phase, all.ImageTemplate.ProjectMode = generationAccepted, ImageProjectModeAll
	for _, job := range []generationJob{single, all} {
		if err := s.saveGenerationJob(job); err != nil {
			t.Fatal(err)
		}
	}
	for mode, want := range map[ImageProjectMode]string{ImageProjectModeSingle: single.ID, ImageProjectModeAll: all.ID} {
		res := s.ListPendingImageGenerations(mode)
		if !res.Success || len(res.Data) != 1 || res.Data[0].JobID != want {
			t.Fatalf("%s studio got %+v", mode, res)
		}
	}
}

func TestSubmissionLostOnlyAfterTheGenerationTimeout(t *testing.T) {
	s := testFlowService(testAccountService(t))
	job := newGenerationJob("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", generationJobImage, "fp", "acc")
	job.Phase = generationSubmitting
	if s.submissionLost(job) {
		t.Fatal("a fresh submission was treated as lost")
	}
	limits := s.cfg.Get().FlowPagePolicy()
	job.CreatedAt = time.Now().Add(-time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds+limits.RequestTimeoutSeconds+1) * time.Second).UTC().Format(time.RFC3339)
	if !s.submissionLost(job) {
		t.Fatal("an old submission with no result was kept pending")
	}
	job.Phase = generationAccepted
	if s.submissionLost(job) {
		t.Fatal("an accepted job must never be treated as lost")
	}
	if err := s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	job.Phase = generationSubmitting
	if err := s.rejectLostSubmission(job); !errors.Is(err, errLostSubmission) {
		t.Fatalf("want errLostSubmission, got %v", err)
	}
	if saved, _ := s.loadGenerationJob(job.ID); saved.Phase != generationRejected {
		t.Fatalf("lost job not closed: %s", saved.Phase)
	}
}

func TestOnlyNotExecutedRPCStatusesAreDefiniteRejections(t *testing.T) {
	for code, want := range map[int]bool{3: true, 7: true, 8: true, 9: true, 16: false, 0: false, 2: false, 4: false, 10: false, 13: false, 14: false} {
		if got := definiteMutationRejection(&chrome.RPCError{ID: "x", Code: code}); got != want {
			t.Errorf("RPC status %d: definite=%v, want %v", code, got, want)
		}
	}
	authDetail := `[16,null,[["type.googleapis.com/google.rpc.ErrorInfo",["ACCESS_TOKEN_EXPIRED","googleapis.com"]]]]`
	if !definiteMutationRejection(&chrome.RPCError{ID: "x", Code: 16, Detail: authDetail}) {
		t.Error("UNAUTHENTICATED with an auth reason must be definite")
	}
	otherDetail := `[16,null,[["type.googleapis.com/google.rpc.ErrorInfo",["SERVICE_DISABLED","googleapis.com"]]]]`
	if definiteMutationRejection(&chrome.RPCError{ID: "x", Code: 16, Detail: otherDetail}) {
		t.Error("UNAUTHENTICATED without an auth reason must be reconciled")
	}
	if got := rpcErrorReason(authDetail); got != "ACCESS_TOKEN_EXPIRED" {
		t.Errorf("reason = %q", got)
	}
	if !definiteMutationRejection(&chrome.HTTPError{Status: 400}) || definiteMutationRejection(&chrome.HTTPError{Status: 503}) {
		t.Error("HTTP 4xx must be definite and 5xx unknown")
	}
	if !definiteMutationRejection(&mutationRejection{cause: errors.New("captcha")}) {
		t.Error("a marked rejection must be definite")
	}
}

func TestVideoJobStaysOpenUntilEveryFinishedVideoIsDownloaded(t *testing.T) {
	final := []pendingVideo{
		{name: "ok", url: "https://flow/ok", status: videoStatusSuccess},
		{name: "dl", url: "https://flow/dl", status: videoStatusSuccess},
		{name: "blocked", status: 7, statusObserved: true},
	}
	downloaded := GeneratedVideoItem{ID: "ok", LocalURL: "/flow-media/ok.mp4"}
	notDownloaded := GeneratedVideoItem{ID: "dl", Error: "Không tải được video"}
	blocked := GeneratedVideoItem{ID: "blocked", Error: "bộ lọc an toàn"}
	if !videosAwaitingDownload(final, []GeneratedVideoItem{downloaded, notDownloaded, blocked}) {
		t.Fatal("a failed download must keep the job open")
	}
	if videosAwaitingDownload(final, []GeneratedVideoItem{downloaded, blocked, {ID: "later", Pending: true}}) {
		t.Fatal("Flow-side failures and unknown videos do not wait for a download")
	}
}

// reconcileTransport answers the project listing and GetMedia calls of a
// resumed job; an empty reply sends Flow's UNAVAILABLE error envelope.
func reconcileTransport(t *testing.T, listing, media string) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	quote := func(payload string) string {
		quoted, _ := json.Marshal(payload)
		return string(quoted)
	}
	http.DefaultTransport = flowRoundTrip(func(r *http.Request) (*http.Response, error) {
		rpc := r.URL.Query().Get("rpcids")
		reply := map[string]string{RPCGetProjectContents: listing, RPCGetMedia: media}[rpc]
		if reply == "" {
			envelope := `)]}'` + "\n\n1\n" + `[["wrb.fr","` + rpc + `",null,null,null,[14],"generic"]]`
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(envelope)), Request: r}, nil
		}
		return flowReply(r, rpc, quote(reply)), nil
	})
}

func oldSubmission(s *Provider, job generationJob) generationJob {
	limits := s.cfg.Get().FlowPagePolicy()
	job.Phase, job.ProjectID, job.ClientIDs = generationSubmitting, "proj", []string{"CID-1"}
	job.CreatedAt = time.Now().Add(-time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds+limits.RequestTimeoutSeconds+60) * time.Second).UTC().Format(time.RFC3339)
	return job
}

func TestVideoResumeKeepsJobPendingWhenTheListingFails(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	reconcileTransport(t, "", "")
	s := testFlowService(accounts)
	job := oldSubmission(s, newGenerationJob("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", generationJobVideo, "fp", acc.ID))
	if err := s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	client, err := chrome.NewFlowClient(stored.Credentials, accounts.cfgManager.Get().Accounts)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.reconcileVideoGenerationWithClient(context.Background(), client, job, GeneratedVideoItem{Prompt: "a fox"})
	if err != nil || len(items) != 1 || !items[0].Pending {
		t.Fatalf("a failed listing must keep the job pending, got %+v %v", items, err)
	}
	if saved, _ := s.loadGenerationJob(job.ID); saved.Phase != generationSubmitting {
		t.Fatalf("a failed listing closed the job as %s", saved.Phase)
	}
}

func TestImageResumeNeverCallsAListedJobLost(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	listing, _ := json.Marshal([]any{nil, []any{[]any{"workflow-1", nil, nil, []any{nil, nil, nil, nil, "media-1", "CID-1"}}}})
	reconcileTransport(t, string(listing), "") // GetMedia fails
	s := testFlowService(accounts)
	job := oldSubmission(s, newGenerationJob("cccccccc-cccc-4ccc-8ccc-cccccccccccc", generationJobImage, "fp", acc.ID))
	job.ImageTemplate = imageGenerationTemplate{Prompt: "a fox", ModelID: "m", AspectRatio: "16:9", Quality: ImageQuality1K}
	if err := s.saveGenerationJob(job); err != nil {
		t.Fatal(err)
	}
	res := s.ResumeImageGeneration(context.Background(), job.ID)
	if !res.Success || len(res.Data) != 1 || !res.Data[0].Pending {
		t.Fatalf("a listed job whose media is not readable yet must stay pending, got %+v", res)
	}
	if saved, _ := s.loadGenerationJob(job.ID); saved.Phase != generationAccepted {
		t.Fatalf("Flow listed the client ID, so the job is accepted; got %s", saved.Phase)
	}
}

func TestEditJobIsNotJournaledWhenTheBrowserIsNotReady(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := testFlowService(accounts)
	inner, err := BuildEditVideoRequest("a fox", "proj", "variant", "source", 0, 24, 1, 1, "token")
	if err != nil {
		t.Fatal(err)
	}
	client, err := chrome.NewFlowClient(stored.Credentials, accounts.cfgManager.Get().Accounts)
	if err != nil {
		t.Fatal(err)
	}
	req := EditVideoRequest{RequestID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", Prompt: "a fox"}
	if _, err := s.runEditVideoJob(context.Background(), client, stored, req, "proj", RPCEditVideo, inner, GeneratedVideoItem{}); err == nil {
		t.Fatal("an edit that could not be sent must fail, not wait as pending")
	}
	var job editJob
	if err := s.readState("jobs", req.RequestID, &job); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unsent edit was journaled as sent: %+v %v", job, err)
	}
}

func TestUnusualActivityNamesTheActionAndTheRealCause(t *testing.T) {
	msg := unusualActivity("tạo ảnh").Error()
	for _, want := range []string{"tạo ảnh", "hoạt động bất thường", "chưa bị trừ credit"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}
