package media

import (
	"path/filepath"
	"slices"
	"testing"

	"SnakeMedia/internal/config"
)

func TestCheckVideoToolsReportsAMissingConfiguredPath(t *testing.T) {
	policy := config.DefaultFlowConfig()
	policy.FFmpegPath = filepath.Join(t.TempDir(), "ffmpeg.exe")
	policy.ProbePath = filepath.Join(t.TempDir(), "ffprobe.exe")
	tools := CheckVideoTools(policy)
	if !slices.Equal(tools.Missing, []string{"ffmpeg", "ffprobe"}) {
		t.Fatalf("missing = %v, want both: a configured path that does not exist is not a tool", tools.Missing)
	}
}
