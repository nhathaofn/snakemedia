package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/providers/flow/chrome"
)

// TestLiveDownloadWatermarkOriginals reads media of existing jobs only. It
// never generates, edits, resubmits a job or replaces the app's downloaded file.
// Close the app first so the account's Chrome profile has a single owner.
func TestLiveDownloadWatermarkOriginals(t *testing.T) {
	ids, data, output := os.Getenv("SNAKEMEDIA_WATERMARK_JOBS"), os.Getenv("SNAKEMEDIA_LIVE_DATA"), os.Getenv("SNAKEMEDIA_LIVE_OUTPUT")
	if ids == "" || data == "" || output == "" {
		t.Skip("set SNAKEMEDIA_WATERMARK_JOBS, SNAKEMEDIA_LIVE_DATA and SNAKEMEDIA_LIVE_OUTPUT to download existing originals")
	}
	p := paths.ForRoot(data)
	cfg, err := config.NewManager(p.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	store := media.NewStore(output)
	s := New(liveAccounts(t, p, cfg), cfg, store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, id := range strings.Split(ids, ",") {
		job, err := s.loadGenerationJob(strings.TrimSpace(id))
		if err != nil {
			t.Fatal(err)
		}
		if job.Kind != generationJobVideo || job.Phase != generationComplete {
			t.Fatal("only completed video jobs may be downloaded")
		}
		acc, release, err := s.readyOwnedAccount(ctx, job.AccountID)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer release()
			client, err := s.projectPageClient(ctx, *acc, chrome.MethodMedia, chrome.MethodProjectContents, chrome.MethodVideoStatus)
			if err != nil {
				t.Fatal(err)
			}
			defer s.keepCredentials(*acc, client)
			for _, item := range job.Videos {
				videos, err := s.waitForVideos(ctx, client, job.ProjectID, []pendingVideo{{name: item.MediaID}}, cfg.Get().Flow.VideoPollIntervalMS, cfg.Get().Flow.VideoGenerationTimeoutSeconds)
				if err != nil {
					t.Fatal(err)
				}
				for _, video := range videos {
					if video.url == "" {
						t.Fatal("Flow returned no download URL")
					}
					local, err := s.downloadVideo(ctx, client, video, cfg.Get().Flow.VideoGenerationTimeoutSeconds)
					if err != nil {
						t.Fatal(err)
					}
					file, err := store.Path(local)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("original job=%s model=%s file=%s", job.ID, job.VideoTemplate.Model, filepath.Clean(file))
				}
			}
		}()
	}
}
