package muse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// findFFmpeg locates the ffmpeg executable according to configured path,
// the application executable directory, or system PATH.
func findFFmpeg(configuredPath string) (string, error) {
	if configuredPath != "" {
		if _, err := os.Stat(configuredPath); err == nil {
			return configuredPath, nil
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		names := []string{"ffmpeg"}
		if runtime.GOOS == "windows" {
			names = []string{"ffmpeg.exe", "ffmpeg"}
		}
		for _, name := range names {
			candidate := filepath.Join(exeDir, "bin", name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			candidate = filepath.Join(exeDir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}
	return "", errors.New("Không tìm thấy ffmpeg; cài FFmpeg vào PATH hoặc cấu hình muse.ffmpegPath.")
}

// findFFprobe locates the ffprobe executable next to ffmpeg or on PATH.
func findFFprobe(ffmpegPath string) (string, error) {
	if ffmpegPath != "" {
		dir := filepath.Dir(ffmpegPath)
		ext := filepath.Ext(ffmpegPath)
		names := []string{"ffprobe" + ext, "ffprobe"}
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	if path, err := exec.LookPath("ffprobe"); err == nil {
		return path, nil
	}
	return "", errors.New("Không tìm thấy ffprobe; cài FFmpeg vào PATH hoặc cấu hình muse.ffmpegPath.")
}

// extractAudio extracts audio track from video to outAudio. It tries stream copy first,
// and falls back to AAC transcoding if stream copy fails.
func extractAudio(ctx context.Context, ffmpeg, video, outAudio string) error {
	cmd := exec.CommandContext(ctx, ffmpeg, "-y", "-i", video, "-vn", "-acodec", "copy", outAudio)
	hideCmdWindow(cmd)
	if err := cmd.Run(); err == nil {
		return nil
	}
	cmdTranscode := exec.CommandContext(ctx, ffmpeg, "-y", "-i", video, "-vn", "-c:a", "aac", "-b:a", "192k", outAudio)
	hideCmdWindow(cmdTranscode)
	if out, err := cmdTranscode.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg extractAudio error: %v (%s)", err, string(out))
	}
	return nil
}

// replaceAudio muxes video stream from video and audio stream from audio into out.
func replaceAudio(ctx context.Context, ffmpeg, video, audio, out string) error {
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-y", "-i", video, "-i", audio,
		"-map", "0:v:0", "-map", "1:a:0",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "192k",
		"-shortest", out,
	)
	hideCmdWindow(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg replaceAudio error: %v (%s)", err, string(output))
	}
	return nil
}

// mergeVideos concatenates multiple video files into out using ffmpeg concat demuxer.
// It tries stream copy first, and re-encodes if stream copy fails.
func mergeVideos(ctx context.Context, ffmpeg string, files []string, out string) error {
	if len(files) == 0 {
		return errors.New("Không có file video để ghép")
	}
	if len(files) == 1 {
		return copyFile(files[0], out)
	}

	tmpDir := os.TempDir()
	listFile := filepath.Join(tmpDir, fmt.Sprintf("muse_concat_%d.txt", time.Now().UnixNano()))
	defer os.Remove(listFile)

	var sb strings.Builder
	for _, f := range files {
		clean := filepath.Clean(f)
		clean = strings.ReplaceAll(clean, "'", "'\\''")
		sb.WriteString(fmt.Sprintf("file '%s'\n", clean))
	}
	if err := os.WriteFile(listFile, []byte(sb.String()), 0o600); err != nil {
		return err
	}

	// First try -c copy
	cmdCopy := exec.CommandContext(ctx, ffmpeg, "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c", "copy", out)
	hideCmdWindow(cmdCopy)
	if err := cmdCopy.Run(); err == nil {
		return nil
	}

	// Fallback re-encode
	cmdReencode := exec.CommandContext(ctx, ffmpeg, "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c:v", "libx264", "-preset", "fast", "-crf", "22", "-c:a", "aac", "-b:a", "192k", out)
	hideCmdWindow(cmdReencode)
	if outBytes, err := cmdReencode.CombinedOutput(); err != nil {
		return fmt.Errorf("ghép video lỗi: %v (%s)", err, string(outBytes))
	}
	return nil
}

// addMusic adds background music to video. If video has audio, music is mixed at 15% volume;
// otherwise music is played at 80% volume with 1s fade-in and 2s fade-out.
func addMusic(ctx context.Context, ffmpeg, ffprobe, video, music, out string) error {
	dur, err := getVideoDuration(ctx, ffprobe, video)
	if err != nil || dur <= 0 {
		return fmt.Errorf("không đọc được thời lượng video: %v", err)
	}
	hasAudio := checkHasAudio(ctx, ffprobe, video)

	vol := 0.8
	if hasAudio {
		vol = 0.15
	}
	fadeOutStart := dur - 2
	if fadeOutStart < 0 {
		fadeOutStart = 0
	}

	mFilter := fmt.Sprintf("[1:a]volume=%.2f,afade=t=in:d=1,afade=t=out:st=%.2f:d=2", vol, fadeOutStart)
	var filter string
	if hasAudio {
		filter = fmt.Sprintf("%s[m];[0:a][m]amix=inputs=2:duration=first:normalize=0[a]", mFilter)
	} else {
		filter = fmt.Sprintf("%s[a]", mFilter)
	}

	args := []string{
		"-y", "-i", video, "-stream_loop", "-1", "-i", music,
		"-filter_complex", filter,
		"-map", "0:v", "-map", "[a]",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "192k",
		"-t", fmt.Sprintf("%.2f", dur),
		out,
	}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	hideCmdWindow(cmd)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("thêm nhạc lỗi: %v (%s)", err, string(outBytes))
	}
	return nil
}

func getVideoDuration(ctx context.Context, ffprobe, file string) (float64, error) {
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", file)
	hideCmdWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var dur float64
	_, err = fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	return dur, err
}

func checkHasAudio(ctx context.Context, ffprobe, file string) bool {
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "a", "-show_entries", "stream=index", "-of", "csv=p=0", file)
	hideCmdWindow(cmd)
	out, err := cmd.Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
