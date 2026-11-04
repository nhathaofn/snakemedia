package flow

import (
	sharedaccounts "SnakeMedia/internal/accounts"
	"context"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/providers/flow/session"
	"github.com/google/uuid"
)

// Explicit opt-in integration tests use existing real accounts and files.
// Upload/edit tests may consume Flow credits. No live call runs by default.

type liveAccountSelection struct {
	Accounts
	id string
}

func TestLiveReplayUploadedSource(t *testing.T) {
	sourceID, prompt := os.Getenv("SNAKEMEDIA_LIVE_SOURCE"), os.Getenv("SNAKEMEDIA_LIVE_PROMPT")
	if os.Getenv("SNAKEMEDIA_LIVE_REPLAY") != "1" || sourceID == "" || prompt == "" {
		t.Skip("explicit real source/prompt replay required")
	}
	realPaths := paths.ForRoot(os.Getenv("SNAKEMEDIA_LIVE_DATA"))
	realConfig, err := config.NewManager(realPaths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("SNAKEMEDIA_LIVE_OUTPUT")
	cfg, err := config.NewManager(filepath.Join(output, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	accounts := liveAccounts(t, realPaths, realConfig)
	store := media.NewStore(filepath.Join(output, "media"))
	s := New(accounts, cfg, store)
	var source sourceRecord
	if err := s.readState("sources", sourceID, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Video.Models) == 0 {
		t.Fatal("no model metadata")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	id := uuid.NewString()
	t.Logf("real replay request=%s source=%s", id, sourceID)
	end := min(source.Video.DurationSeconds, float64(source.Video.Models[0].MaxInputSeconds))
	startMS, endMS := 0, int(math.Round(end*1000))
	if value := os.Getenv("SNAKEMEDIA_LIVE_TRIM_START_MS"); value != "" {
		startMS, err = strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	if value := os.Getenv("SNAKEMEDIA_LIVE_TRIM_END_MS"); value != "" {
		endMS, err = strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real selected range=%d-%dms", startMS, endMS)
	result := s.EditVideos(ctx, EditVideoRequest{SourceID: sourceID, RequestID: id, ModelID: source.Video.Models[0].ID, Prompt: prompt, TrimStartMS: startMS, TrimEndMS: endMS})
	if !result.Success {
		t.Fatal(result.Error)
	}
	for _, item := range result.Data {
		t.Logf("replay job=%s pending=%v message=%s", item.JobID, item.Pending, item.Error)
		if item.Pending || item.Error != "" || item.LocalURL == "" {
			t.Fatal("real replay did not produce a finished video")
		}
		file, err := store.Path(item.LocalURL)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := media.ProbeVideo(ctx, file, func() (string, error) { return exec.LookPath("ffprobe") })
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("real replay output=%s dimensions=%dx%d duration=%.3f", file, meta.Width, meta.Height, meta.DurationSeconds)
	}
}

type liveRequestLog struct {
	base    http.RoundTripper
	mu      sync.Mutex
	ids     []string
	uploads int
}

func (l *liveRequestLog) RoundTrip(r *http.Request) (*http.Response, error) {
	l.mu.Lock()
	if ids := r.URL.Query().Get("rpcids"); ids != "" {
		l.ids = append(l.ids, strings.Split(ids, ",")...)
	}
	if strings.HasPrefix(r.URL.Path, "/upload/") {
		l.uploads++
	}
	l.mu.Unlock()
	return l.base.RoundTrip(r)
}

func TestLiveResumeVideoEdit(t *testing.T) {
	id := os.Getenv("SNAKEMEDIA_LIVE_JOB")
	if os.Getenv("SNAKEMEDIA_LIVE_RESUME") != "1" || id == "" {
		t.Skip("explicit read-only resume opt-in required")
	}
	realPaths := paths.ForRoot(os.Getenv("SNAKEMEDIA_LIVE_DATA"))
	realConfig, err := config.NewManager(realPaths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("SNAKEMEDIA_LIVE_OUTPUT")
	cfg, err := config.NewManager(filepath.Join(output, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	accounts := liveAccounts(t, realPaths, realConfig)
	s := New(accounts, cfg, media.NewStore(filepath.Join(output, "media")))
	trace := &liveRequestLog{base: http.DefaultTransport}
	http.DefaultTransport = trace
	t.Cleanup(func() { http.DefaultTransport = trace.base })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	res := s.ResumeVideoEdit(ctx, id)
	if !res.Success {
		t.Fatal(res.Error)
	}
	var job editJob
	if err := s.readState("jobs", id, &job); err != nil {
		t.Fatal(err)
	}
	all, err := accounts.GetConnectedStoredAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range all {
		if account.ID == job.AccountID {
			for _, rpc := range trace.ids {
				if rpc != "" && rpc == account.Credentials.RPCs.EditVideo {
					t.Fatal("resume resubmitted an edit")
				}
			}
		}
	}
	if trace.uploads != 0 {
		t.Fatal("resume uploaded another source")
	}
	for _, item := range res.Data {
		t.Logf("resumed existing job pending=%v output=%s message=%s", item.Pending, item.LocalURL, item.Error)
	}
	t.Logf("resume used %d read RPCs, zero uploads and zero edit submissions", len(trace.ids))
}

func (a liveAccountSelection) GetConnectedStoredAccounts() ([]session.StoredAccount, error) {
	all, err := a.Accounts.GetConnectedStoredAccounts()
	if err != nil {
		return nil, err
	}
	for _, account := range all {
		if account.ID == a.id {
			return []session.StoredAccount{account}, nil
		}
	}
	return nil, nil
}

func TestLiveVideoEdit(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_LIVE_EDIT") != "1" {
		t.Skip("requires explicit live-edit opt-in")
	}
	root, input, output := os.Getenv("SNAKEMEDIA_LIVE_DATA"), os.Getenv("SNAKEMEDIA_LIVE_VIDEO"), os.Getenv("SNAKEMEDIA_LIVE_OUTPUT")
	prompt := os.Getenv("SNAKEMEDIA_LIVE_PROMPT")
	if root == "" || input == "" || prompt == "" {
		t.Fatal("set SNAKEMEDIA_LIVE_DATA, SNAKEMEDIA_LIVE_VIDEO and SNAKEMEDIA_LIVE_PROMPT")
	}
	if output == "" {
		output = t.TempDir()
	}
	realPaths := paths.ForRoot(root)
	realConfig, err := config.NewManager(realPaths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(filepath.Join(output, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Update(realConfig.Get()); err != nil {
		t.Fatal(err)
	}
	accounts := liveAccounts(t, realPaths, realConfig)
	var selected Accounts = accounts
	if value := os.Getenv("SNAKEMEDIA_LIVE_ACCOUNT_INDEX"); value != "" {
		all, err := accounts.GetConnectedStoredAccounts()
		if err != nil {
			t.Fatal(err)
		}
		index, err := strconv.Atoi(value)
		if err != nil || index < 0 || index >= len(all) {
			t.Fatal("invalid real account index")
		}
		selected = liveAccountSelection{Accounts: accounts, id: all[index].ID}
		t.Logf("real account index=%d", index)
	}
	store := media.NewStore(filepath.Join(output, "media"))
	imported := store.ImportVideo((&Provider{}).SourceVideoExtensions(), func() (string, error) { return input, nil })
	if !imported.Success {
		t.Fatal(imported.Error)
	}
	s := New(selected, cfg, store)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	up := s.UploadSourceVideo(ctx, imported.Data.LocalURL)
	if !up.Success {
		t.Fatalf("real upload failed: %s", up.Error)
	}
	t.Logf("real source: %dx%d duration=%.3f models=%d", up.Data.Width, up.Data.Height, up.Data.DurationSeconds, len(up.Data.Models))
	if len(up.Data.Models) == 0 {
		t.Fatal("no live edit models")
	}
	end := min(float64(up.Data.Models[0].MaxInputSeconds), up.Data.DurationSeconds)
	requestID := uuid.NewString()
	t.Logf("real edit request=%s", requestID)
	res := s.EditVideos(ctx, EditVideoRequest{SourceID: up.Data.SourceID, RequestID: requestID, ModelID: up.Data.Models[0].ID, Prompt: prompt, TrimEndMS: int(math.Round(end * 1000))})
	if !res.Success {
		t.Fatalf("real edit failed: %s", res.Error)
	}
	for _, item := range res.Data {
		t.Logf("job=%s pending=%v message=%s", item.JobID, item.Pending, item.Error)
		if item.Pending {
			t.Fatalf("job remains recoverable but no finished video yet: %s", item.JobID)
		}
		if item.Error != "" {
			t.Fatal(item.Error)
		}
		file, err := store.Path(item.LocalURL)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := media.ProbeVideo(ctx, file, func() (string, error) { return exec.LookPath("ffprobe") })
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("real output: %dx%d duration=%.3f file=%s", meta.Width, meta.Height, meta.DurationSeconds, file)
		// New provider instance restores completed jobs and does not submit again.
		restarted := New(accounts, cfg, store)
		recovered := restarted.ResumeVideoEdit(ctx, item.JobID)
		if !recovered.Success || len(recovered.Data) == 0 || recovered.Data[0].LocalURL != item.LocalURL {
			t.Fatal("restart recovery failed")
		}
	}
}

// liveAccounts serves the Flow accounts saved in a real data directory.
func liveAccounts(t *testing.T, realPaths paths.Paths, cfg *config.Manager) *session.Service {
	t.Helper()
	shared := sharedaccounts.New(filepath.Join(realPaths.DataDir, "accounts"), cfg, session.NewSite(cfg))
	t.Cleanup(shared.Close)
	return session.NewService(shared, cfg)
}
