package muse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
)

func TestCheckAPI_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready": true,
				"driver": map[string]any{
					"driver":    "browser",
					"ok":        true,
					"tabs":      3,
					"busy_tabs": 0,
				},
				"accounts": map[string]any{
					"total":     1,
					"available": 1,
				},
			})
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer test-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := &Provider{}
	res := p.CheckAPI(context.Background(), ts.URL+"/v1", "test-key")
	if !res.Success || !res.Data.OK {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.Data.Accounts != 1 || res.Data.Tabs != 3 {
		t.Errorf("unexpected data: %+v", res.Data)
	}
}

func TestCheckAPI_InvalidKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true})
		case "/v1/models":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := &Provider{}
	res := p.CheckAPI(context.Background(), ts.URL+"/v1", "bad-key")
	if !res.Success || res.Data.OK {
		t.Fatalf("expected failed key, got %+v", res)
	}
}

func TestCheckAPI_Live(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_LIVE_MUSE_API") != "1" {
		t.Skip("set SNAKEMEDIA_LIVE_MUSE_API=1 to run live muse2api check")
	}
	endpoint := os.Getenv("SNAKEMEDIA_LIVE_MUSE_API_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:18610/v1"
	}
	key := os.Getenv("SNAKEMEDIA_LIVE_MUSE_API_KEY")
	if key == "" {
		t.Skip("set SNAKEMEDIA_LIVE_MUSE_API_KEY to authenticate against live muse2api")
	}
	p := &Provider{}
	res := p.CheckAPI(context.Background(), endpoint, key)
	if !res.Success || !res.Data.OK {
		t.Fatalf("live check failed: %+v", res)
	}
	t.Logf("Live test success: %+v", res.Data)
}

func TestPlanWithCapacity_UseAPISkipsChromeProcessingJobs(t *testing.T) {
	now := time.Now()
	jobs := []MuseJob{
		{ID: "chrome-session", Status: MuseJobProcessing, AccountID: "chrome-acc-1", SessionID: "chat-123"},
		{ID: "api-session", Status: MuseJobProcessing, AccountID: "api", SessionID: "task-456"},
		{ID: "queued-job", Status: MuseJobQueued},
	}
	cfg := config.MuseConfig{UseAPI: true, APIEndpoint: "http://127.0.0.1:18610/v1"}
	got := planWithCapacity(jobs, nil, cfg, map[string]string{}, now, apiCapacity{driver: "chrome", accounts: 2, tabs: 1})
	var ids []string
	for _, a := range got {
		if a.accountID != "api" {
			t.Fatalf("unexpected non-api assignment: %+v", a)
		}
		ids = append(ids, a.jobID)
	}
	if len(ids) != 2 || ids[0] != "api-session" || ids[1] != "queued-job" {
		t.Fatalf("assigned jobs = %v, want [api-session queued-job]", ids)
	}
}

func TestExecuteAPIVideoFailedStatusDoesNotRecheckDeadSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			fmt.Fprint(w, `{"id":"task-fail-1","task_id":"task-fail-1","status":"queued"}`)
		case http.MethodGet:
			fmt.Fprint(w, `{"id":"task-fail-1","status":"failed","error":"render crashed"}`)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	p := testProvider(t, &fakePage{})
	cfg := p.cfg.Get()
	cfg.Muse.UseAPI = true
	cfg.Muse.APIEndpoint = srv.URL + "/v1"
	if _, err := p.cfg.Update(cfg); err != nil {
		t.Fatal(err)
	}

	job := submit(t, p, MuseJobVideo, "render a cat")[0]
	p.runOne(context.Background(), job.ID, "api")

	got := mustJob(t, p, job.ID)
	if got.Status != MuseJobQueued || got.SessionID != "" || got.Attempts != 1 || got.Rechecks != 0 {
		t.Fatalf("after 1st attempt: %+v, want Queued with empty SessionID Attempts=1 Rechecks=0", got)
	}
	if !strings.Contains(got.Error, "render crashed") {
		t.Fatalf("error = %q, want render crashed", got.Error)
	}

	p.runOne(context.Background(), job.ID, "api")
	got = mustJob(t, p, job.ID)
	if got.Status != MuseJobFailed || got.Attempts != 2 || got.Rechecks != 0 {
		t.Fatalf("after 2nd attempt: %+v, want Failed with Attempts=2 Rechecks=0", got)
	}
}

func TestExecuteAPIImage_CancelledMidDownloadAndMimeDetect(t *testing.T) {
	minimalPNG := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}

	t.Run("detects png from octet-stream", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"created":1,"data":[{"url":"%s/dl/img.bin"}]}`, srv.URL)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(minimalPNG)
		}))
		defer srv.Close()

		p := testProvider(t, &fakePage{})
		cfg := p.cfg.Get()
		cfg.Muse.UseAPI = true
		cfg.Muse.APIEndpoint = srv.URL + "/v1"
		if _, err := p.cfg.Update(cfg); err != nil {
			t.Fatal(err)
		}

		job := submit(t, p, MuseJobImage, "detect png")[0]
		p.runOne(context.Background(), job.ID, "api")

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobSucceeded || len(got.Files) != 1 {
			t.Fatalf("job = %+v", got)
		}
		if !strings.HasSuffix(got.Files[0], ".png") {
			t.Fatalf("stored file URL = %q, want .png extension from MIME detection", got.Files[0])
		}
	})

	t.Run("cancelled mid-download does not mark succeeded", func(t *testing.T) {
		p := testProvider(t, &fakePage{})
		var jobID string
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"created":1,"data":[{"url":"%s/dl/img.bin"}]}`, srv.URL)
				return
			}
			_, _ = p.jobs.update(jobID, func(j *MuseJob) {
				j.Status = MuseJobCancelled
				j.Step = "Đã huỷ"
			})
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(minimalPNG)
		}))
		defer srv.Close()

		cfg := p.cfg.Get()
		cfg.Muse.UseAPI = true
		cfg.Muse.APIEndpoint = srv.URL + "/v1"
		if _, err := p.cfg.Update(cfg); err != nil {
			t.Fatal(err)
		}

		job := submit(t, p, MuseJobImage, "cancel mid download")[0]
		jobID = job.ID
		p.runOne(context.Background(), job.ID, "api")

		got := mustJob(t, p, job.ID)
		if got.Status != MuseJobCancelled || got.Error != "" {
			t.Fatalf("job = %+v, want Cancelled with empty error", got)
		}
	})

	t.Run("removed job does not fail finishAPIJob", func(t *testing.T) {
		p := testProvider(t, &fakePage{})
		job := submit(t, p, MuseJobImage, "removed mid download")[0]
		if _, err := p.jobs.update(job.ID, func(j *MuseJob) {
			j.Status = MuseJobProcessing
			j.AccountID = "api"
		}); err != nil {
			t.Fatal(err)
		}
		res := p.RemoveJobs([]string{job.ID})
		if !res.Success {
			t.Fatalf("RemoveJobs failed: %+v", res)
		}
		if err := p.finishAPIJob(context.Background(), job, []string{"/flow-media/fake.png"}, "done"); err != nil {
			t.Fatalf("finishAPIJob returned unexpected error for removed job: %v", err)
		}
	})
}

func TestPlanWithAPICapacity(t *testing.T) {
	cfg := config.DefaultMuseConfig()
	cfg.UseAPI = true
	cfg.ThreadsPerAccount = 3
	now := time.Now()

	var jobs []MuseJob
	for i := range 20 {
		jobs = append(jobs, MuseJob{
			ID:        fmt.Sprintf("q%02d", i),
			Kind:      MuseJobVideo,
			Status:    MuseJobQueued,
			CreatedAt: fmt.Sprintf("%02d", i),
		})
	}
	// Held and storyboard-waiting jobs must stay skipped even in API mode.
	jobs = append(jobs,
		MuseJob{ID: "held", Status: MuseJobQueued, Held: true, CreatedAt: "00"},
		MuseJob{ID: "wait-board", Status: MuseJobQueued, After: "board-1", CreatedAt: "00"},
	)

	// Unknown capacity falls back to max(ThreadsPerAccount, 2).
	if got := planWithCapacity(jobs, nil, cfg, map[string]string{}, now, apiCapacity{}); len(got) != 3 {
		t.Fatalf("unknown cap got %d assignments, want 3", len(got))
	}

	// 4 available accounts * 3 threads = 12 (within 13 browser tabs).
	got := planWithCapacity(jobs, nil, cfg, map[string]string{}, now, apiCapacity{driver: "browser", accounts: 4, tabs: 13})
	if len(got) != 12 {
		t.Fatalf("4 accounts * 3 threads got %d assignments, want 12", len(got))
	}
	for _, a := range got {
		if a.jobID == "held" || a.jobID == "wait-board" {
			t.Fatalf("scheduled blocked job %s", a.jobID)
		}
	}

	// ThreadsPerAccount=5 with 4 accounts (20) is bounded by 13 browser tabs.
	cfg5 := cfg
	cfg5.ThreadsPerAccount = 5
	if got := planWithCapacity(jobs, nil, cfg5, map[string]string{}, now, apiCapacity{driver: "browser", accounts: 4, tabs: 13}); len(got) != 13 {
		t.Fatalf("browser tabs cap got %d assignments, want 13", len(got))
	}

	// Non-browser drivers are not bounded by browser tabs.
	if got := planWithCapacity(jobs, nil, cfg5, map[string]string{}, now, apiCapacity{driver: "http", accounts: 4, tabs: 1}); len(got) != 20 {
		t.Fatalf("http driver got %d assignments, want 20", len(got))
	}

	// Cold-start browser tabs < accounts still dispatches one per account.
	if got := planWithCapacity(jobs, nil, cfg, map[string]string{}, now, apiCapacity{driver: "browser", accounts: 4, tabs: 2}); len(got) != 4 {
		t.Fatalf("cold-start tabs got %d assignments, want 4", len(got))
	}
}

func TestProviderAPIScalesConcurrencyAndResumesOnStart(t *testing.T) {
	var (
		mu      sync.Mutex
		active  = map[string]bool{}
		maxSeen int
		all12   = make(chan struct{})
		closed  bool
		seq     int
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/readyz":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready": true,
				"driver": map[string]any{
					"driver":    "browser",
					"ok":        true,
					"tabs":      13,
					"busy_tabs": 0,
				},
				"accounts": map[string]any{
					"total":     4,
					"available": 4,
				},
			})
		case r.URL.Path == "/v1/videos" && r.Method == http.MethodPost:
			mu.Lock()
			seq++
			id := fmt.Sprintf("task-%d", seq)
			active[id] = true
			if len(active) > maxSeen {
				maxSeen = len(active)
			}
			if len(active) >= 12 && !closed {
				closed = true
				close(all12)
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "queued"})
		case strings.HasPrefix(r.URL.Path, "/v1/videos/") && strings.HasSuffix(r.URL.Path, "/content"):
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake-mp4"))
		case strings.HasPrefix(r.URL.Path, "/v1/videos/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/videos/")
			mu.Lock()
			active[id] = true
			if len(active) > maxSeen {
				maxSeen = len(active)
			}
			if len(active) >= 12 && !closed {
				closed = true
				close(all12)
			}
			ready := closed
			mu.Unlock()
			if !ready {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "in_progress", "progress": 25})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "completed", "progress": 100})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := testProvider(t, nil)
	p.sleep = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	}
	cfgVal := p.cfg.Get()
	cfgVal.Muse.UseAPI = true
	cfgVal.Muse.APIEndpoint = ts.URL + "/v1"
	cfgVal.Muse.ThreadsPerAccount = 3
	cfgVal.Muse.PollIntervalMS = 2000
	if _, err := p.cfg.Update(cfgVal); err != nil {
		t.Fatal(err)
	}

	// Pre-seed a processing API job with SessionID to verify Start() resumes it.
	resumed := MuseJob{
		ID:        "resumed-api-job",
		Kind:      MuseJobVideo,
		Mode:      MuseJobSingle,
		Prompt:    "resumed prompt",
		Status:    MuseJobProcessing,
		AccountID: "api",
		SessionID: "task-resume",
		CreatedAt: "2026-11-18T00:00:00Z",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := p.jobs.add(resumed); err != nil {
		t.Fatal(err)
	}

	prompts := make([]string, 11)
	for i := range prompts {
		prompts[i] = fmt.Sprintf("prompt-%d", i+1)
	}
	res := p.SubmitJobs(MuseJobRequest{
		Kind:    MuseJobVideo,
		Mode:    MuseJobSingle,
		Prompts: prompts,
	})
	if !res.Success {
		t.Fatalf("submit failed: %s", res.Error)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		p.Close()
	}()
	p.Start(ctx, nil)

	select {
	case <-all12:
	case <-time.After(5 * time.Second):
		mu.Lock()
		got := maxSeen
		mu.Unlock()
		t.Fatalf("timed out waiting for 12 concurrent API tasks, maxSeen=%d, resumed=%+v", got, mustJob(t, p, "resumed-api-job"))
	}

	// Wait for all 12 jobs (1 resumed + 11 submitted) to finish successfully.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		done := 0
		for _, j := range p.jobs.list() {
			if j.Status == MuseJobSucceeded {
				done++
			} else if j.Status == MuseJobFailed {
				t.Fatalf("job %s failed: %s", j.ID, j.Error)
			}
		}
		if done == 12 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("jobs did not all complete in time: %+v", p.jobs.list())
}
