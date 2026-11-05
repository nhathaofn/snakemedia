package watermark

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestVideoSyntheticMatrix compares every output frame with a known clean
// background, including changing opacity and edges passing behind the mark.
// The opt-in encoder test uses the same lossy input and output settings for
// both implementations; it is not evidence of perfect recovery of real media.
func TestVideoSyntheticMatrix(t *testing.T) {
	root, output := os.Getenv("SNAKEMEDIA_SYNTHETIC_INPUTS"), os.Getenv("SNAKEMEDIA_WATERMARK_OUTPUT")
	if root == "" || output == "" {
		t.Skip("set SNAKEMEDIA_SYNTHETIC_INPUTS and SNAKEMEDIA_WATERMARK_OUTPUT")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, output} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initTemplates()
	type metric struct {
		Name            string    `json:"name"`
		Seconds         float64   `json:"seconds"`
		Processed       bool      `json:"processed"`
		MeanError       float64   `json:"meanError"`
		WorstFrameError float64   `json:"worstFrameError"`
		FrameErrors     []float64 `json:"frameErrors"`
	}
	var results []metric
	for _, name := range []string{"flat-dark", "flat-bright", "gradient", "texture-pan", "edge-crossing", "opacity-pulse", "unmarked", "text-dark", "text-opacity-pulse", "both"} {
		w, h, frames := 640, 360, 48
		text := name == "text-dark" || name == "text-opacity-pulse" || name == "both"
		if text {
			w, h, frames = 1280, 720, 24
		}
		star := name != "unmarked" && name != "text-dark" && name != "text-opacity-pulse"
		src := filepath.Join(root, name+".mp4")
		reference := filepath.Join(root, name+".rgb")
		const extent = 160
		if _, err := os.Stat(src); os.IsNotExist(err) {
			cmd := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", w, h), "-framerate", "24", "-i", "-", "-c:v", "libx264", "-preset", "fast", "-crf", "20", "-pix_fmt", "yuv420p", src)
			pipe, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			ref, err := os.Create(reference)
			if err != nil {
				t.Fatal(err)
			}
			for f := 0; f < frames; f++ {
				img := syntheticVideoBackground(w, h, f, name)
				corner := make([]byte, extent*extent*3)
				for y := 0; y < extent; y++ {
					for x := 0; x < extent; x++ {
						c := img.RGBAAt(w-extent+x, h-extent+y)
						i := (y*extent + x) * 3
						corner[i], corner[i+1], corner[i+2] = c.R, c.G, c.B
					}
				}
				if _, err := ref.Write(corner); err != nil {
					t.Fatal(err)
				}
				if star {
					tmpl := videoTemplates[0]
					margin := 96
					if h == 360 {
						tmpl = videoTemplates[1]
						margin = 48
					}
					gain := 1.0
					if name == "opacity-pulse" {
						gain = .55 + .7*math.Pow(math.Sin(float64(f)*math.Pi/24), 2)
					}
					stampVideoTemplate(img, w-margin-tmpl.size, h-margin-tmpl.size, tmpl, gain)
				}
				if text {
					m := veoMark()
					gain := 1.0
					if name == "text-opacity-pulse" {
						gain = .55 + .7*math.Pow(math.Sin(float64(f)*math.Pi/24), 2)
					}
					stampVideoAlpha(img, w-veoRight-m.w, h-veoBottom-m.h, m.w, m.h, m.alpha, gain, white)
				}
				if _, err := pipe.Write(img.Pix); err != nil {
					t.Fatal(err)
				}
			}
			ref.Close()
			pipe.Close()
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		}
		dst := filepath.Join(output, name+".mp4")
		start := time.Now()
		processed, err := (Video{FFmpeg: ffmpeg, FFprobe: ffprobe}).RemoveVideo(context.Background(), src, dst)
		elapsed := time.Since(start).Seconds()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !processed {
			dst = src
		}
		decoded, err := exec.Command(ffmpeg, "-v", "error", "-i", dst, "-vf", fmt.Sprintf("crop=%d:%d:iw-%d:ih-%d", extent, extent, extent, extent), "-pix_fmt", "rgb24", "-f", "rawvideo", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		clean, err := os.ReadFile(reference)
		if err != nil {
			t.Fatal(err)
		}
		if len(clean) != len(decoded) {
			t.Fatalf("%s: frame count changed", name)
		}
		var total, worst float64
		perFrame := make([]float64, frames)
		for f := 0; f < frames; f++ {
			var sum float64
			count := 0
			for y := 0; y < extent; y++ {
				for x := 0; x < extent; x++ {
					inside := false
					if star {
						size, margin := 48, 96
						if h == 360 {
							size, margin = 24, 48
						}
						inside = x >= extent-margin-size-3 && x < extent-margin+3 && y >= extent-margin-size-3 && y < extent-margin+3
					}
					if text {
						m := veoMark()
						inside = inside || (x >= extent-veoRight-m.w-3 && x < extent-veoRight+3 && y >= extent-veoBottom-m.h-3 && y < extent-veoBottom+3)
					}
					if name == "unmarked" {
						inside = true
					}
					if inside {
						for c := 0; c < 3; c++ {
							i := (f*extent*extent+y*extent+x)*3 + c
							sum += math.Abs(float64(decoded[i]) - float64(clean[i]))
							count++
						}
					}
				}
			}
			perFrame[f] = sum / float64(count)
			total += perFrame[f]
			worst = math.Max(worst, perFrame[f])
		}
		m := metric{name, elapsed, processed, total / float64(frames), worst, perFrame}
		results = append(results, m)
		t.Logf("%s processed=%v seconds=%.3f MAE=%.3f worstFrame=%.3f", name, processed, elapsed, m.MeanError, worst)
		// Bounds include the lossy input/output codec floor. The old fixed-gain
		// implementation exceeds both the texture and changing-opacity budgets.
		budget := map[string][2]float64{
			"flat-dark": {3, 4}, "flat-bright": {4.5, 5}, "gradient": {3.5, 4.5},
			"texture-pan": {5.5, 7}, "edge-crossing": {4.5, 5.5}, "opacity-pulse": {3.5, 4},
			"unmarked": {3, 3}, "text-dark": {4, 4}, "text-opacity-pulse": {4, 5}, "both": {3.5, 4},
		}[name]
		if m.MeanError > budget[0] || worst > budget[1] {
			t.Errorf("%s: quality regression: mean %.3f worst %.3f", name, m.MeanError, worst)
		}
		if name == "unmarked" && processed {
			t.Error("unmarked video was modified")
		}
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "synthetic.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func syntheticVideoBackground(w, h, f int, name string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 50.0
			switch name {
			case "flat-bright":
				v = 210
			case "gradient":
				v = 30 + 100*float64(x)/float64(w) + 20*math.Sin(float64(y+f*2)/50)
			case "texture-pan", "unmarked":
				v = 100 + 35*math.Sin(float64(x+f*3)/3) + 30*math.Sin(float64(y)/5)
			case "edge-crossing":
				v = 40
				if (x+y+f*5)%100 < 50 {
					v = 170
				}
			}
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = clamp8(v*.7), clamp8(v), clamp8(v*.6), 255
		}
	}
	return img
}

func stampVideoTemplate(img *image.RGBA, x, y int, tmpl template, gain float64) {
	stampVideoAlpha(img, x, y, tmpl.size, tmpl.size, tmpl.alpha, gain, tmpl.colour)
}

func stampVideoAlpha(img *image.RGBA, x, y, w, h int, alpha []float32, gain float64, col [3]float64) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			a := math.Min(.95, float64(alpha[dy*w+dx])*gain)
			c := img.RGBAAt(x+dx, y+dy)
			img.SetRGBA(x+dx, y+dy, color.RGBA{clamp8(float64(c.R)*(1-a) + a*col[0]), clamp8(float64(c.G)*(1-a) + a*col[1]), clamp8(float64(c.B)*(1-a) + a*col[2]), 255})
		}
	}
}
