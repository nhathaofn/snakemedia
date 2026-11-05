package flow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/tools"
)

// TestFlowWatermarkProcessing exercises the production tool lookup, processor,
// archive and atomic replacement on an existing local download. No provider
// account is opened and no billable request is made.
func TestFlowWatermarkProcessing(t *testing.T) {
	src, output := os.Getenv("SNAKEMEDIA_FLOW_WATERMARK_INPUT"), os.Getenv("SNAKEMEDIA_FLOW_WATERMARK_OUTPUT")
	if src == "" || output == "" {
		t.Skip("set SNAKEMEDIA_FLOW_WATERMARK_INPUT and SNAKEMEDIA_FLOW_WATERMARK_OUTPUT")
	}
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	s := testFlowService(testAccountService(t))
	s.media = media.NewStore(output)
	s.tools, err = tools.New(filepath.Join(t.TempDir(), "tools"), "watermark-test", func() map[string]string { return nil })
	if err != nil {
		t.Fatal(err)
	}
	url, err := s.media.Put("fixture", "video/mp4", ".mp4", original)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s.removeVideoWatermark(ctx, "fixture", url)
	archive, err := os.ReadFile(filepath.Join(output, "originals", "fixture.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive, original) {
		t.Fatal("archived download differs from input")
	}
	file, err := s.media.Path(url)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(processed, original) {
		t.Fatal("fixture did not reach watermark processing")
	}
	info, err := media.ProbeVideo(ctx, file, func() (string, error) { return s.program(featureVideoWatermark, "ffprobe") })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("processed=%s original=%x output=%x size=%dx%d duration=%.3f", file, sha256.Sum256(original), sha256.Sum256(processed), info.Width, info.Height, info.DurationSeconds)
}
