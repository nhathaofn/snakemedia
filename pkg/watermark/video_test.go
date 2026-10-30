package watermark_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/pkg/watermark"
)

// TestVideoFile runs RemoveVideo on SNAKEMEDIA_TEST_VIDEO with the machine's
// ffmpeg and ffprobe and writes <name>.clean.mp4 next to it.
func TestVideoFile(t *testing.T) {
	src := os.Getenv("SNAKEMEDIA_TEST_VIDEO")
	if src == "" {
		t.Skip("set SNAKEMEDIA_TEST_VIDEO to a Flow video (needs ffmpeg and ffprobe on PATH)")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not on PATH")
	}
	dst := strings.TrimSuffix(src, filepath.Ext(src)) + ".clean.mp4"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	start := time.Now()
	removed, err := watermark.Video{FFmpeg: ffmpeg, FFprobe: ffprobe}.RemoveVideo(ctx, src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("no mark found")
	}
	t.Logf("wrote %s in %s", dst, time.Since(start))
}
