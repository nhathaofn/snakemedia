// Package watermark provides dynamic multi-scale watermark detection and
// reconstruction for Google Flow / Imagen / Gemini generated images.
package watermark

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"slices"
	"sync"

	_ "golang.org/x/image/webp"
)

//go:embed assets/bg_48.png
var bg48Data []byte

//go:embed assets/bg_96.png
var bg96Data []byte

// video_48.png is the mark of Flow's 720p videos, measured on a real video
// (the mark stays still while the picture moves). It has the image mark's
// shape but a flat, stronger fill and a brighter rim, so the image alpha maps
// over-correct videos.
//
//go:embed assets/video_48.png
var video48Data []byte

// Info holds details about the detected watermark location and size.
type Info struct {
	Size     int
	Position image.Rectangle
}

type template struct {
	size     int
	alpha    []float32
	sumAlpha float64
	denAlpha float64
	// minMargin and maxMargin bound the gap between the mark and the right and
	// bottom edges. Flow places the 48 px image mark 73-78 px in and the video
	// mark 94-96 px in (24 px, 46-48 px in at 360p).
	minMargin int
	maxMargin int
	// minGain and maxGain bound the strength the template is blended at
	// (see blendGain).
	minGain float64
	maxGain float64
	// edgeBand widens the smoothed contour: video stores colour at half
	// resolution, so its mark leaves a colour fringe a pixel past the edge.
	edgeBand int
	// colour is the overlay's colour. Images are marked in white; the video
	// mark is a warm white, and unblending it as white leaves an orange tint.
	colour [3]float64
	// denoise scales how strongly amplified noise inside the star is evened
	// out (see denoiseInside); video noise is mostly fine detail worth keeping.
	denoise float64
}

var (
	templatesOnce  sync.Once
	imageTemplates []template
	videoTemplates []template
)

// initTemplates reads the alpha maps captured from the real marks. They are
// used at the sizes Flow renders: small stand-ins correlate with ordinary
// picture detail. Flow blends the image maps at 0.57-0.65 of their alpha; the
// video map is the measured alpha itself.
func initTemplates() {
	templatesOnce.Do(func() {
		img48, img96, vid48 := decodeAlpha(bg48Data), decodeAlpha(bg96Data), decodeAlpha(video48Data)
		imageTemplates = []template{
			newTemplate(img48, 56, 104, 0.4, 0.9, 0, 4, white),
			newTemplate(img96, 112, 208, 0.4, 0.9, 0, 4, white),
		}
		videoTemplates = []template{
			newTemplate(vid48, 80, 112, 0.5, 1.4, 1, 1.5, videoColour),
			newTemplate(halve(vid48), 36, 60, 0.5, 1.4, 1, 1.5, videoColour),
			// Some videos carry the soft-edged image mark at the video's place.
			newTemplate(img48, 80, 112, 0.4, 0.9, 1, 1.5, white),
			newTemplate(halve(img48), 36, 60, 0.4, 0.9, 1, 1.5, white),
		}
	})
}

var (
	white = [3]float64{255, 255, 255}
	// videoColour is the video mark's colour, measured on the luma (235, the
	// top of video range) and colour planes of a real video.
	videoColour = [3]float64{255, 253, 243}
)

// decodeAlpha reads a square alpha map stored as max(R, G, B) / 255.
func decodeAlpha(data []byte) [][]float32 {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("watermark: decode embedded template: %v", err))
	}
	b := img.Bounds()
	rows := make([][]float32, b.Dy())
	for y := range rows {
		rows[y] = make([]float32, b.Dx())
		for x := range rows[y] {
			c := color.RGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.RGBA)
			rows[y][x] = float32(max(c.R, c.G, c.B)) / 255
		}
	}
	return rows
}

// halve averages 2x2 blocks: the 360p video mark is the 720p one at half size.
func halve(rows [][]float32) [][]float32 {
	out := make([][]float32, len(rows)/2)
	for y := range out {
		out[y] = make([]float32, len(rows[0])/2)
		for x := range out[y] {
			out[y][x] = (rows[2*y][2*x] + rows[2*y][2*x+1] + rows[2*y+1][2*x] + rows[2*y+1][2*x+1]) / 4
		}
	}
	return out
}

func newTemplate(rows [][]float32, minMargin, maxMargin int, minGain, maxGain float64, edgeBand int, denoise float64, colour [3]float64) template {
	sz := len(rows)
	alpha := make([]float32, 0, sz*sz)
	var sum float64
	for _, row := range rows {
		for _, a := range row {
			alpha = append(alpha, a)
			sum += float64(a)
		}
	}
	mean := sum / float64(len(alpha))
	var sumSq float64
	for _, a := range alpha {
		d := float64(a) - mean
		sumSq += d * d
	}
	return template{size: sz, alpha: alpha, sumAlpha: sum, denAlpha: math.Sqrt(sumSq),
		minMargin: minMargin, maxMargin: maxMargin, minGain: minGain, maxGain: maxGain, edgeBand: edgeBand, denoise: denoise, colour: colour}
}

// Detect checks if the given image data contains a visible Gemini/Flow watermark.
// It dynamically scans the corner region across multi-scale templates
// using normalized cross-correlation (NCC) to find the exact watermark position.
func Detect(data []byte) (present bool, score float64, info Info, err error) {
	if len(data) == 0 {
		return false, 0, Info{}, fmt.Errorf("watermark: empty image data")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false, 0, Info{}, fmt.Errorf("watermark: decode image: %w", err)
	}

	match := findWatermark(img)
	if !match.found {
		return false, match.score, Info{}, nil
	}
	return true, match.score, Info{
		Size:     match.size,
		Position: image.Rect(match.x, match.y, match.x+match.size, match.y+match.size),
	}, nil
}

// Remove dynamically locates and removes the Gemini/Flow watermark.
// It detects the accurate multi-scale coordinates in the corner and reconstructs
// the underlying pixels without clipping, ghost silhouettes, or halo artifacts.
func Remove(data []byte) (output []byte, removed bool, err error) {
	if len(data) == 0 {
		return nil, false, fmt.Errorf("watermark: empty image data")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false, fmt.Errorf("watermark: decode image: %w", err)
	}

	cleaned, ok := RemoveImage(img)
	if !ok {
		return data, false, nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, cleaned); err != nil {
		return nil, false, fmt.Errorf("watermark: encode cleaned image: %w", err)
	}
	return buf.Bytes(), true, nil
}

// RemoveImage removes the watermark from an in-memory image.Image at its dynamically detected location.
func RemoveImage(img image.Image) (*image.RGBA, bool) {
	match := findWatermark(img)
	if !match.found {
		return nil, false
	}

	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)

	reconstructWatermark(rgba, match)

	return rgba, true
}

type matchResult struct {
	found    bool
	size     int
	x        int
	y        int
	score    float64
	gain     float64
	template template
}

// minScore is the lowest correlation for a position to be tried. Over busy
// texture (grass, snow) a real mark correlates as low as 0.2, so correlation
// only orders the candidates; blendGain decides.
const (
	minScore      = 0.2
	maxCandidates = 256
)

// findWatermark correlates each template with the band of the bottom-right
// corner where Flow places it and accepts the best-correlated position whose
// pixels are the template blended over the picture at Flow's strength
// (blendGain). Picture detail that merely resembles the star has no such
// blend, so it is left alone.
func findWatermark(img image.Image) matchResult {
	initTemplates()
	return findWith(img, imageTemplates)
}

func findWith(img image.Image, templates []template) matchResult {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	best := matchResult{}
	for _, tmpl := range templates {
		sz := tmpl.size
		if sz+tmpl.maxMargin > w/2 || sz+tmpl.maxMargin > h/2 {
			continue
		}
		minX, maxX := w-tmpl.maxMargin-sz, w-tmpl.minMargin-sz
		minY, maxY := h-tmpl.maxMargin-sz, h-tmpl.minMargin-sz

		patchW, patchH := w-minX, h-minY
		luma := make([]float64, patchW*patchH)
		for y := 0; y < patchH; y++ {
			for x := 0; x < patchW; x++ {
				c := color.RGBAModel.Convert(img.At(bounds.Min.X+minX+x, bounds.Min.Y+minY+y)).(color.RGBA)
				luma[y*patchW+x] = 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
			}
		}

		n := float64(sz * sz)
		var candidates []matchResult
		for y := minY; y <= maxY; y++ {
			for x := minX; x <= maxX; x++ {
				var sum, sumSq, prod float64
				for dy := 0; dy < sz; dy++ {
					row := luma[(y-minY+dy)*patchW+x-minX:]
					ta := tmpl.alpha[dy*sz:]
					for dx := 0; dx < sz; dx++ {
						v := row[dx]
						sum += v
						sumSq += v * v
						prod += v * float64(ta[dx])
					}
				}
				den := sumSq - sum*sum/n
				if den <= 0 {
					continue
				}
				if c := (prod - sum/n*tmpl.sumAlpha) / (math.Sqrt(den) * tmpl.denAlpha); c >= minScore {
					candidates = append(candidates, matchResult{size: sz, x: bounds.Min.X + x, y: bounds.Min.Y + y, score: c, template: tmpl})
				}
			}
		}
		slices.SortFunc(candidates, func(a, b matchResult) int { return cmp.Compare(b.score, a.score) })
		for _, c := range candidates[:min(len(candidates), maxCandidates)] {
			if c.score <= best.score {
				break
			}
			if gain, ok := blendGain(img, c); ok && fitsSurroundings(img, c, gain) {
				c.gain, c.found = refineGainOnFlat(img, c, gain), true
				best = c
				break
			}
		}
	}
	return best
}

// reconstructWatermark removes the watermark by reverse alpha blending with the
// template's own alpha map: original = (marked - a*colour) / (1 - a). The overlay's
// strength varies between images, so it is measured on the image (blendGain)
// instead of fixed. Only the contour, where JPEG ringing does not follow the
// alpha map, is smoothed afterwards; the rest keeps its real texture.
func reconstructWatermark(rgba *image.RGBA, match matchResult) {
	bounds := rgba.Bounds()
	sz := match.size
	wmX, wmY := match.x, match.y
	if wmX < bounds.Min.X || wmY < bounds.Min.Y || wmX+sz > bounds.Max.X || wmY+sz > bounds.Max.Y {
		return
	}
	alpha := match.template.alpha
	gain := match.gain
	col := match.template.colour

	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			a := float64(alpha[y*sz+x]) * gain
			if a < 0.002 {
				continue
			}
			if a > 0.95 {
				a = 0.95
			}
			c := rgba.RGBAAt(wmX+x, wmY+y)
			rgba.SetRGBA(wmX+x, wmY+y, color.RGBA{R: unblend(c.R, a, col[0]), G: unblend(c.G, a, col[1]), B: unblend(c.B, a, col[2]), A: 255})
		}
	}
	smoothContour(rgba, match)
	denoiseInside(rgba, match, gain)
}

func unblend(c uint8, a, colour float64) uint8 {
	return clamp8((float64(c) - a*colour) / (1 - a))
}

func clamp8(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(255, v))))
}

// blendGain finds how strongly the template was blended into this image: the
// gain at which the unblended patch keeps no trace of the template's edges
// (its gradient no longer correlates with the template's gradient). Scene
// edges do not follow the star's contour, so they barely move the result.
// ok is false when no gain in the template's range does that.
func blendGain(img image.Image, match matchResult) (gain float64, ok bool) {
	sz := match.size
	alpha := match.template.alpha
	residual := func(gain float64) float64 {
		luma := make([]float64, sz*sz)
		for y := 0; y < sz; y++ {
			for x := 0; x < sz; x++ {
				a := math.Min(float64(alpha[y*sz+x])*gain, 0.95)
				c := color.RGBAModel.Convert(img.At(match.x+x, match.y+y)).(color.RGBA)
				v := (float64(c.R) + float64(c.G) + float64(c.B)) / 3
				luma[y*sz+x] = (v - a*255) / (1 - a)
			}
		}
		var sum float64
		for y := 0; y < sz-1; y++ {
			for x := 0; x < sz-1; x++ {
				i := y*sz + x
				sum += (luma[i+1]-luma[i])*float64(alpha[i+1]-alpha[i]) + (luma[i+sz]-luma[i])*float64(alpha[i+sz]-alpha[i])
			}
		}
		return sum
	}
	lo, hi := match.template.minGain, match.template.maxGain
	if residual(lo) <= 0 || residual(hi) >= 0 {
		return 0, false
	}
	for range 24 {
		mid := (lo + hi) / 2
		if residual(mid) > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2, true
}

// maxCoreGap is the largest luma gap allowed between the unblended centre of
// the star and the picture around it inside the template box. Real marks
// measured 0-35 over busy pictures.
const maxCoreGap = 45

// fitsSurroundings checks the centre of the star once unblended at gain: under
// a real mark it continues the picture around it, while a bright star-shaped
// object in the picture stays far brighter than its surroundings.
func fitsSurroundings(img image.Image, match matchResult, gain float64) bool {
	gap, _, ok := coreGap(img, match, gain)
	return ok && math.Abs(gap) <= maxCoreGap
}

// coreGap is the luma of the star's unblended centre minus the mean luma
// around the star inside the template box, and spread the standard deviation
// of that surrounding luma.
func coreGap(img image.Image, match matchResult, gain float64) (gap, spread float64, ok bool) {
	sz := match.size
	var peak float32
	for _, a := range match.template.alpha {
		peak = max(peak, a)
	}
	var core, around, aroundSq, nCore, nAround float64
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			ta := match.template.alpha[y*sz+x]
			c := color.RGBAModel.Convert(img.At(match.x+x, match.y+y)).(color.RGBA)
			v := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
			switch {
			case ta >= 0.8*peak:
				a := math.Min(float64(ta)*gain, 0.95)
				core += (v - a*255) / (1 - a)
				nCore++
			case ta < 0.01:
				around += v
				aroundSq += v * v
				nAround++
			}
		}
	}
	if nCore == 0 || nAround == 0 {
		return 0, 0, false
	}
	mean := around / nAround
	return core/nCore - mean, math.Sqrt(math.Max(0, aroundSq/nAround-mean*mean)), true
}

// flatSpread is the surrounding luma spread below which the picture under the
// star is taken as flat. There the gradient test in blendGain has too little
// to work with and settles low, leaving a pale star; the centre matching its
// surroundings is the sharper measure.
const flatSpread = 8

// refineGainOnFlat returns the gain that makes the unblended centre match a
// flat surrounding, or gain unchanged when the surrounding is not flat or no
// gain in range does that.
func refineGainOnFlat(img image.Image, match matchResult, gain float64) float64 {
	gap := func(g float64) float64 {
		v, _, _ := coreGap(img, match, g)
		return v
	}
	if _, spread, ok := coreGap(img, match, gain); !ok || spread > flatSpread {
		return gain
	}
	lo, hi := match.template.minGain, match.template.maxGain
	if gap(lo) <= 0 || gap(hi) >= 0 {
		return gain
	}
	for range 24 {
		mid := (lo + hi) / 2
		if gap(mid) > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// smoothContour replaces pixels on the template's edge, where the alpha changes
// fast and JPEG ringing leaves a thin outline, by the median of their 5x5
// neighbourhood (wide enough to drop a line two pixels thick).
func smoothContour(rgba *image.RGBA, match matchResult) {
	sz := match.size
	alpha := match.template.alpha
	at := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= sz || y >= sz {
			return 0
		}
		return float64(alpha[y*sz+x])
	}
	edge := func(x, y int) bool {
		gx, gy := at(x+1, y)-at(x-1, y), at(x, y+1)-at(x, y-1)
		return gx*gx+gy*gy >= 0.004
	}
	band := match.template.edgeBand
	nearEdge := func(x, y int) bool {
		for dy := -band; dy <= band; dy++ {
			for dx := -band; dx <= band; dx++ {
				if edge(x+dx, y+dy) {
					return true
				}
			}
		}
		return false
	}
	src := copyAround(rgba, match)
	var r, g, b [25]uint8
	for y := -band; y < sz+band; y++ {
		for x := -band; x < sz+band; x++ {
			if !nearEdge(x, y) {
				continue
			}
			n := 0
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c := src.RGBAAt(match.x+x+dx, match.y+y+dy)
					r[n], g[n], b[n] = c.R, c.G, c.B
					n++
				}
			}
			rgba.SetRGBA(match.x+x, match.y+y, color.RGBA{R: median(r), G: median(g), B: median(b), A: 255})
		}
	}
}

// Unblending divides by (1 - a), so the JPEG noise under the star comes back
// louder than around it: a speckled star on dark or flat pictures. A bilateral
// filter evens that out without blurring edges stronger than rangeSigma, and
// is applied in proportion to how much each pixel was amplified.
const (
	denoiseRadius = 3
	spaceSigma    = 1.5
	rangeSigma    = 14.0
)

func denoiseInside(rgba *image.RGBA, match matchResult, gain float64) {
	sz := match.size
	src := copyAround(rgba, match)
	b := src.Bounds()
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			a := math.Min(float64(match.template.alpha[y*sz+x])*gain, 0.95)
			w := math.Min(1, a/(1-a)*match.template.denoise)
			if w < 0.02 {
				continue
			}
			px, py := match.x+x, match.y+y
			c := src.RGBAAt(px, py)
			var sr, sg, sb, sw float64
			for dy := -denoiseRadius; dy <= denoiseRadius; dy++ {
				for dx := -denoiseRadius; dx <= denoiseRadius; dx++ {
					q := image.Pt(px+dx, py+dy)
					if !q.In(b) {
						continue
					}
					n := src.RGBAAt(q.X, q.Y)
					dr, dg, db := float64(n.R)-float64(c.R), float64(n.G)-float64(c.G), float64(n.B)-float64(c.B)
					k := math.Exp(-float64(dx*dx+dy*dy)/(2*spaceSigma*spaceSigma) - (dr*dr+dg*dg+db*db)/(3*2*rangeSigma*rangeSigma))
					sr += k * float64(n.R)
					sg += k * float64(n.G)
					sb += k * float64(n.B)
					sw += k
				}
			}
			mix := func(v uint8, f float64) uint8 { return clamp8((1-w)*float64(v) + w*f/sw) }
			rgba.SetRGBA(px, py, color.RGBA{R: mix(c.R, sr), G: mix(c.G, sg), B: mix(c.B, sb), A: 255})
		}
	}
}

// copyAround copies the template box and a margin wide enough for the
// filters, so a video frame is not copied whole for every frame.
func copyAround(rgba *image.RGBA, match matchResult) *image.RGBA {
	const margin = 4
	r := image.Rect(match.x-margin, match.y-margin, match.x+match.size+margin, match.y+match.size+margin).Intersect(rgba.Bounds())
	src := image.NewRGBA(r)
	draw.Draw(src, r, rgba, r.Min, draw.Src)
	return src
}

func median(v [25]uint8) uint8 {
	slices.Sort(v[:])
	return v[12]
}
