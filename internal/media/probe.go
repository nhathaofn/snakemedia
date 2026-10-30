package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"SnakeMedia/internal/config"
)

// VideoMetadata is read from the file, independently of webview codec support.
type VideoMetadata struct {
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	DurationSeconds float64 `json:"durationSeconds"`
}

// HideWindow keeps the console window of a helper program (ffmpeg, ffprobe)
// from showing.
func HideWindow(cmd *exec.Cmd) { hideProbeWindow(cmd) }

// FFprobe is ffprobe from configuration, next to a configured ffmpeg, or PATH.
func FFprobe(policy config.FlowConfig) (string, error) {
	program, err := findTool("ffprobe", policy.ProbePath, policy.FFmpegPath)
	if err != nil {
		return "", errors.New("Không tìm thấy ffprobe; cài FFmpeg vào PATH hoặc cấu hình flow.probePath.")
	}
	return program, nil
}

// FFmpeg is ffmpeg from configuration, next to a configured ffprobe, or PATH.
func FFmpeg(policy config.FlowConfig) (string, error) {
	program, err := findTool("ffmpeg", policy.FFmpegPath, policy.ProbePath)
	if err != nil {
		return "", errors.New("Không tìm thấy ffmpeg; cài FFmpeg vào PATH hoặc cấu hình flow.ffmpegPath.")
	}
	return program, nil
}

// findTool returns the configured path (when it exists), else the program in
// the folder of its configured sibling (FFmpeg ships both in one bin
// folder), else PATH.
func findTool(name, configured, sibling string) (string, error) {
	if configured != "" {
		return exec.LookPath(configured)
	}
	if sibling != "" {
		if path, err := exec.LookPath(filepath.Join(filepath.Dir(sibling), name)); err == nil {
			return path, nil
		}
	}
	return exec.LookPath(name)
}

// VideoTools is what the video watermark removal finds on this machine.
// Missing lists the programs it could not find ("ffmpeg", "ffprobe").
type VideoTools struct {
	FFmpeg  string   `json:"ffmpeg"`
	FFprobe string   `json:"ffprobe"`
	Missing []string `json:"missing"`
}

// CheckVideoTools looks for ffmpeg and ffprobe the way the removal does.
func CheckVideoTools(policy config.FlowConfig) VideoTools {
	tools := VideoTools{Missing: []string{}}
	var err error
	if tools.FFmpeg, err = FFmpeg(policy); err != nil {
		tools.Missing = append(tools.Missing, "ffmpeg")
	}
	if tools.FFprobe, err = FFprobe(policy); err != nil {
		tools.Missing = append(tools.Missing, "ffprobe")
	}
	return tools
}

// ProbeVideo reads MP4, MOV and M4V headers directly, so the common formats
// work without FFmpeg; other files (WebM) and unreadable headers fall back to
// ffprobe from configuration or PATH. There are no machine paths or assumed
// durations. The caller supplies a bounded context.
func ProbeVideo(ctx context.Context, file string, policy config.FlowConfig) (VideoMetadata, error) {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".mp4", ".mov", ".m4v":
		if meta, err := probeMP4(file); err == nil {
			return meta, nil
		}
	}
	program, err := FFprobe(policy)
	if err != nil {
		return VideoMetadata{}, err
	}
	cmd := exec.CommandContext(ctx, program, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,duration:stream_side_data=rotation:format=duration", "-of", "json", file)
	hideProbeWindow(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return VideoMetadata{}, ctx.Err()
		}
		return VideoMetadata{}, errors.New("Không đọc được thông số video bằng ffprobe.")
	}
	var result struct {
		Streams []struct {
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration string `json:"duration"`
			SideData []struct {
				Rotation int `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || len(result.Streams) != 1 {
		return VideoMetadata{}, errors.New("Tệp không chứa luồng video hợp lệ.")
	}
	s := result.Streams[0]
	duration, _ := strconv.ParseFloat(s.Duration, 64)
	if duration <= 0 {
		duration, _ = strconv.ParseFloat(result.Format.Duration, 64)
	}
	if s.Width <= 0 || s.Height <= 0 || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return VideoMetadata{}, errors.New("Kích thước hoặc thời lượng video không hợp lệ.")
	}
	for _, data := range s.SideData {
		if data.Rotation%180 != 0 {
			s.Width, s.Height = s.Height, s.Width
			break
		}
	}
	return VideoMetadata{Width: s.Width, Height: s.Height, DurationSeconds: duration}, nil
}
