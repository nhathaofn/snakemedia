package watermark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
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
	// videoCRF is a quality target; H.264 output remains lossy.
	videoCRF = "16"
	// fast keeps the same CRF target with less CPU, at the cost of larger files.
	videoPreset = "fast"
)

// VideoReport describes work performed, not a guarantee of perfect recovery.
// A covered pixel cannot always be recovered exactly after lossy compression.
type VideoReport struct {
	Processed   bool
	Frames      int
	StarFrames  int
	TextFrames  int
	ProbeTime   time.Duration
	DetectTime  time.Duration
	RewriteTime time.Duration
}

// RemoveVideo processes detected marks and leaves src untouched. Callers must
// retain the original if they replace it with dst. false means no mark found.
func (v Video) RemoveVideo(ctx context.Context, src, dst string) (bool, error) {
	report, err := v.ProcessVideo(ctx, src, dst)
	return report.Processed, err
}

// ProcessVideo additionally reports stage times and the number of frames touched.
func (v Video) ProcessVideo(ctx context.Context, src, dst string) (VideoReport, error) {
	var report VideoReport
	start := time.Now()
	info, err := v.probe(ctx, src)
	report.ProbeTime = time.Since(start)
	if err != nil {
		return report, err
	}
	start = time.Now()
	marks, err := v.locate(ctx, src, info)
	report.DetectTime = time.Since(start)
	if err != nil || (!marks.star.found && !marks.hasVeo) {
		return report, err
	}
	start = time.Now()
	err = v.rewrite(ctx, src, dst, marks, info, &report)
	report.RewriteTime = time.Since(start)
	if err != nil {
		_ = os.Remove(dst)
		return report, err
	}
	report.Processed = true
	return report, nil
}

func (v Video) command(ctx context.Context, program string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	if v.Prepare != nil {
		v.Prepare(cmd)
	}
	return cmd
}

type videoInfo struct {
	Width  int
	Height int
	Frames int
	FPS    string
}

func (v Video) probe(ctx context.Context, src string) (videoInfo, error) {
	var out, stderr bytes.Buffer
	cmd := v.command(ctx, v.FFprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,r_frame_rate,nb_frames,duration", "-of", "json", src)
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return videoInfo{}, fmt.Errorf("watermark: ffprobe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var data struct {
		Streams []struct {
			Width    int
			Height   int
			Rate     string `json:"r_frame_rate"`
			Frames   string `json:"nb_frames"`
			Duration string `json:"duration"`
		}
	}
	if err := json.Unmarshal(out.Bytes(), &data); err != nil || len(data.Streams) != 1 {
		return videoInfo{}, errors.New("watermark: unreadable video stream")
	}
	stream := data.Streams[0]
	num, den, ok := strings.Cut(stream.Rate, "/")
	n, errN := strconv.Atoi(num)
	d, errD := strconv.Atoi(den)
	if !ok || errN != nil || errD != nil || n <= 0 || d <= 0 || stream.Width <= 0 || stream.Height <= 0 {
		return videoInfo{}, errors.New("watermark: invalid video geometry or frame rate")
	}
	count, _ := strconv.Atoi(stream.Frames)
	if count <= 0 {
		duration, _ := strconv.ParseFloat(stream.Duration, 64)
		count = int(duration * float64(n) / float64(d))
	}
	return videoInfo{Width: stream.Width, Height: stream.Height, Frames: count, FPS: stream.Rate}, nil
}

// videoMarks are the marks found on a video: the star, the "Veo" text, or both.
type videoMarks struct {
	star   matchResult
	veo    veoMatch
	hasVeo bool
}

// sampledCorner exposes the full frame's coordinates without allocating or
// transferring pixels outside the detection region.
type sampledCorner struct {
	*image.RGBA
	full image.Rectangle
}

func (c sampledCorner) Bounds() image.Rectangle { return c.full }

func (v Video) locate(ctx context.Context, src string, info videoInfo) (videoMarks, error) {
	initTemplates()
	extent := max(veoRight+veoMark().w+veoSearch, veoBottom+veoMark().h+veoSearch)
	for _, tmpl := range videoTemplates {
		extent = max(extent, tmpl.maxMargin+tmpl.size)
	}
	cw, ch := min(extent, info.Width), min(extent, info.Height)
	step := max(1, info.Frames/sampleFrames)
	var stderr bytes.Buffer
	cmd := v.command(ctx, v.FFmpeg, "-v", "error", "-i", src, "-map", "0:v:0",
		"-vf", fmt.Sprintf("select=not(mod(n\\,%d)),crop=%d:%d:iw-%d:ih-%d", step, cw, ch, cw, ch),
		"-fps_mode", "passthrough", "-frames:v", strconv.Itoa(sampleFrames), "-pix_fmt", "rgba", "-f", "rawvideo", "-")
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return videoMarks{}, err
	}
	if err := cmd.Start(); err != nil {
		return videoMarks{}, fmt.Errorf("watermark: start ffmpeg: %w", err)
	}
	corner := sampledCorner{RGBA: image.NewRGBA(image.Rect(info.Width-cw, info.Height-ch, info.Width, info.Height)), full: image.Rect(0, 0, info.Width, info.Height)}
	var found []matchResult
	var texts []veoMatch
	var samples []sampledCorner
	var decodeErr error
	for {
		_, err := io.ReadFull(out, corner.Pix)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			decodeErr = err
			break
		}
		if m := findWith(corner, videoTemplates); m.found {
			found = append(found, m)
		}
		texts = append(texts, findVeo(corner))
		copyOf := image.NewRGBA(corner.RGBA.Bounds())
		copy(copyOf.Pix, corner.Pix)
		samples = append(samples, sampledCorner{RGBA: copyOf, full: corner.full})
	}
	_, _ = io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil {
		return videoMarks{}, fmt.Errorf("watermark: sample frames: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if decodeErr != nil {
		return videoMarks{}, fmt.Errorf("watermark: incomplete sampled frame: %w", decodeErr)
	}
	if len(texts) == 0 {
		return videoMarks{}, errors.New("watermark: the video has no decodable frame")
	}
	marks := videoMarks{star: calibrateVideoStar(samples, vote(found))}
	marks.veo, marks.hasVeo = voteVeo(texts)
	return marks, nil
}

// vote keeps the place most frames agree on, and the median of their gains.
func vote(found []matchResult) matchResult {
	type spot struct {
		x, y, size int
		alpha      *float32
	}
	votes := map[spot][]matchResult{}
	var best []matchResult
	for _, m := range found {
		if len(m.template.alpha) == 0 {
			continue
		}
		k := spot{m.x, m.y, m.size, &m.template.alpha[0]}
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

// rewrite pipes every decoded frame through reconstructWatermark into an
// encoder that also copies src's audio.
func (v Video) rewrite(ctx context.Context, src, dst string, marks videoMarks, info videoInfo, report *VideoReport) error {
	w, h, fps := info.Width, info.Height, info.FPS
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
		"-c:v", "libx264", "-preset", videoPreset, "-crf", videoCRF, "-pix_fmt", "yuv420p",
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
		report.Frames++
		if marks.star.found {
			reconstructVideoStar(frame, frameStar(frame, marks.star))
			report.StarFrames++
		}
		if marks.hasVeo {
			removeVeo(frame, frameText(frame, marks.veo))
			report.TextFrames++
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
