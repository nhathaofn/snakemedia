package watermark

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestVideoMatrix is an explicit, repeatable real-file benchmark. Inputs are
// immutable; outputs and timings are kept in a separate directory. It never
// contacts a provider or consumes credits. The JSON cases are {name,input}.
func TestVideoMatrix(t *testing.T) {
	file, output := os.Getenv("SNAKEMEDIA_WATERMARK_CASES"), os.Getenv("SNAKEMEDIA_WATERMARK_OUTPUT")
	if file == "" || output == "" {
		t.Skip("set SNAKEMEDIA_WATERMARK_CASES and SNAKEMEDIA_WATERMARK_OUTPUT")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	repeats, _ := strconv.Atoi(os.Getenv("SNAKEMEDIA_WATERMARK_REPEATS"))
	if repeats < 1 {
		repeats = 1
	}
	type result struct {
		Name      string  `json:"name"`
		Run       int     `json:"run"`
		Seconds   float64 `json:"seconds"`
		Processed bool    `json:"processed"`
		Output    string  `json:"output"`
	}
	var results []result
	for _, c := range cases {
		for run := 1; run <= repeats; run++ {
			dst := filepath.Join(output, c.Name+"-"+strconv.Itoa(run)+".mp4")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			start := time.Now()
			processed, err := (Video{FFmpeg: ffmpeg, FFprobe: ffprobe}).RemoveVideo(ctx, c.Input, dst)
			elapsed := time.Since(start).Seconds()
			cancel()
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			results = append(results, result{c.Name, run, elapsed, processed, dst})
			t.Logf("%s run=%d processed=%v seconds=%.3f output=%s", c.Name, run, processed, elapsed, dst)
		}
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "timings.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}
