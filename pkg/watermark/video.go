package watermark

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// Video holds the programs RemoveVideo runs. Both come from the machine (PATH
// or configuration); nothing is bundled.
type Video struct {
	FFmpeg  string
	FFprobe string
	// Prepare, when set, adjusts every command before it starts (for example
	// to hide its console window).
	Prepare func(*exec.Cmd)
}

const (
	// sampleFrames frames spread over the video are searched for the mark.
	sampleFrames = 12
	// minVotes of them must place it at the same spot.
	minVotes = 3
	// minVideoScore is the lowest median correlation of the voting frames.
	// Real marks measured 0.55-0.99; a still sticker 0.24.
	minVideoScore = 0.4
	// videoCRF keeps the re-encoded video visually lossless.
	videoCRF = "16"
)

// RemoveVideo writes src to dst without Flow's marks (the star and the "Veo"
// text). A mark does not move in a video, so its place and strength are
// measured once on frames spread over the video, then every frame is restored
// the same way as an image. Audio is
// copied unchanged; size and frame rate stay those of src. removed is false
// (and dst is not written) when neither mark is found.
func (v Video) RemoveVideo(ctx context.Context, src, dst string) (removed bool, err error) {
	fps, err := v.frameRate(ctx, src)
	if err != nil {
		return false, err
	}
	marks, w, h, err := v.locate(ctx, src)
	if err != nil || (!marks.star.found && !marks.hasVeo) {
		return false, err
	}
	if err := v.rewrite(ctx, src, dst, marks, w, h, fps); err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	return true, nil
}

func (v Video) command(ctx context.Context, program string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	if v.Prepare != nil {
		v.Prepare(cmd)
	}
	return cmd
}

// frameRate reads the first video stream's frame rate ("24/1").
func (v Video) frameRate(ctx context.Context, src string) (string, error) {
	var out, stderr bytes.Buffer
	cmd := v.command(ctx, v.FFprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=r_frame_rate", "-of", "default=noprint_wrappers=1:nokey=1", src)
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("watermark: ffprobe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	rate := strings.TrimSpace(out.String())
	num, den, ok := strings.Cut(rate, "/")
	n, errN := strconv.Atoi(num)
	d, errD := strconv.Atoi(den)
	if !ok || errN != nil || errD != nil || n <= 0 || d <= 0 {
		return "", fmt.Errorf("watermark: unreadable frame rate %q", rate)
	}
	return rate, nil
}

// videoMarks are the marks found on a video: the star, the "Veo" text, or both.
type videoMarks struct {
	star   matchResult
	veo    veoMatch
	hasVeo bool
}

// locate decodes sampleFrames frames spread over the video and returns the
// marks found at the same place on most of them (the star with its median
// strength).
func (v Video) locate(ctx context.Context, src string) (videoMarks, int, int, error) {
	frames, err := v.countFrames(ctx, src)
	if err != nil {
		return videoMarks{}, 0, 0, err
	}
	step := max(1, frames/sampleFrames)
	var stderr bytes.Buffer
	cmd := v.command(ctx, v.FFmpeg, "-v", "error", "-i", src, "-map", "0:v:0",
		"-vf", fmt.Sprintf("select=not(mod(n\\,%d))", step), "-fps_mode", "passthrough",
		"-frames:v", strconv.Itoa(sampleFrames), "-c:v", "png", "-f", "image2pipe", "-")
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return videoMarks{}, 0, 0, err
	}
	if err := cmd.Start(); err != nil {
		return videoMarks{}, 0, 0, fmt.Errorf("watermark: start ffmpeg: %w", err)
	}
	initTemplates()
	reader := bufio.NewReader(out)
	var found []matchResult
	var texts []veoMatch
	w, h := 0, 0
	for {
		img, err := png.Decode(reader)
		if err != nil {
			break
		}
		w, h = img.Bounds().Dx(), img.Bounds().Dy()
		if m := findWith(img, videoTemplates); m.found {
			found = append(found, m)
		}
		texts = append(texts, findVeo(img))
	}
	_, _ = io.Copy(io.Discard, reader)
	if err := cmd.Wait(); err != nil {
		return videoMarks{}, 0, 0, fmt.Errorf("watermark: sample frames: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if w == 0 {
		return videoMarks{}, 0, 0, errors.New("watermark: the video has no decodable frame")
	}
	marks := videoMarks{star: vote(found)}
	marks.veo, marks.hasVeo = voteVeo(texts)
	return marks, w, h, nil
}

// vote keeps the place most frames agree on, and the median of their gains.
func vote(found []matchResult) matchResult {
	type spot struct{ x, y, size int }
	votes := map[spot][]matchResult{}
	var best []matchResult
	for _, m := range found {
		k := spot{m.x, m.y, m.size}
		votes[k] = append(votes[k], m)
		if len(votes[k]) > len(best) {
			best = votes[k]
		}
	}
	if len(best) < minVotes {
		return matchResult{}
	}
	gains := make([]float64, len(best))
	scores := make([]float64, len(best))
	for i, m := range best {
		gains[i], scores[i] = m.gain, m.score
	}
	slices.Sort(gains)
	slices.Sort(scores)
	// A still overlay that is not the star (a sticker, a logo) can pass the
	// blend check on some frames but never correlates with the star well.
	if scores[len(scores)/2] < minVideoScore {
		return matchResult{}
	}
	m := best[0]
	m.gain = gains[len(gains)/2]
	return m
}

func (v Video) countFrames(ctx context.Context, src string) (int, error) {
	var out bytes.Buffer
	cmd := v.command(ctx, v.FFprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=nb_frames", "-of", "default=noprint_wrappers=1:nokey=1", src)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("watermark: ffprobe: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil || n <= 0 {
		// Containers without a frame count: sample from the start.
		return sampleFrames, nil
	}
	return n, nil
}

// rewrite pipes every decoded frame through reconstructWatermark into an
// encoder that also copies src's audio.
func (v Video) rewrite(ctx context.Context, src, dst string, marks videoMarks, w, h int, fps string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var decErr, encErr bytes.Buffer
	dec := v.command(ctx, v.FFmpeg, "-v", "error", "-i", src, "-map", "0:v:0", "-f", "rawvideo", "-pix_fmt", "rgba", "-")
	dec.Stderr = &decErr
	frames, err := dec.StdoutPipe()
	if err != nil {
		return err
	}
	enc := v.command(ctx, v.FFmpeg, "-v", "error", "-y",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", w, h), "-framerate", fps, "-i", "-",
		"-i", src, "-map", "0:v:0", "-map", "1:a?", "-map_metadata", "1",
		"-c:v", "libx264", "-preset", "medium", "-crf", videoCRF, "-pix_fmt", "yuv420p",
		"-c:a", "copy", "-movflags", "+faststart", dst)
	enc.Stderr = &encErr
	sink, err := enc.StdinPipe()
	if err != nil {
		return err
	}
	if err := dec.Start(); err != nil {
		return fmt.Errorf("watermark: start ffmpeg: %w", err)
	}
	if err := enc.Start(); err != nil {
		cancel()
		_ = dec.Wait()
		return fmt.Errorf("watermark: start ffmpeg: %w", err)
	}

	frame := image.NewRGBA(image.Rect(0, 0, w, h))
	var pipeErr error
	for {
		if _, err := io.ReadFull(frames, frame.Pix); err != nil {
			if !errors.Is(err, io.EOF) {
				pipeErr = fmt.Errorf("watermark: read frame: %w", err)
			}
			break
		}
		if marks.star.found {
			reconstructWatermark(frame, marks.star)
		}
		if marks.hasVeo {
			removeVeo(frame, marks.veo)
		}
		if _, err := sink.Write(frame.Pix); err != nil {
			pipeErr = fmt.Errorf("watermark: write frame: %w", err)
			break
		}
	}
	_ = sink.Close()
	if pipeErr != nil {
		cancel()
	}
	errDec, errEnc := dec.Wait(), enc.Wait()
	switch {
	case pipeErr != nil:
		return fmt.Errorf("%w: %s %s", pipeErr, strings.TrimSpace(decErr.String()), strings.TrimSpace(encErr.String()))
	case errDec != nil:
		return fmt.Errorf("watermark: decode video: %w: %s", errDec, strings.TrimSpace(decErr.String()))
	case errEnc != nil:
		return fmt.Errorf("watermark: encode video: %w: %s", errEnc, strings.TrimSpace(encErr.String()))
	}
	return nil
}
