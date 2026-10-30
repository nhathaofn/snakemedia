package watermark

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"sync"
)

// veo.png is the alpha of the "Veo" text some Flow videos carry in the
// bottom-right corner, measured on five real videos (the text stays still
// while the picture moves). Like the star it is white blended over the
// picture, so it is unblended the same way.
//
//go:embed assets/veo.png
var veoData []byte

const (
	// veoRight and veoBottom are the gaps between the text box and the right
	// and bottom edges of a 720p video (landscape and portrait alike).
	veoRight  = 14
	veoBottom = 14
	// veoSearch is how far from that place the text is looked for.
	veoSearch = 2
	// minVeoScore is the lowest median correlation of the sampled frames.
	minVeoScore = 0.5
)

type textMark struct {
	w, h     int
	alpha    []float32
	sumAlpha float64
	denAlpha float64
}

var (
	veoOnce sync.Once
	veo     textMark
)

func veoMark() textMark {
	veoOnce.Do(func() {
		rows := decodeAlpha(veoData)
		veo.h, veo.w = len(rows), len(rows[0])
		for _, row := range rows {
			for _, a := range row {
				veo.alpha = append(veo.alpha, a)
				veo.sumAlpha += float64(a)
			}
		}
		mean := veo.sumAlpha / float64(len(veo.alpha))
		var sumSq float64
		for _, a := range veo.alpha {
			sumSq += (float64(a) - mean) * (float64(a) - mean)
		}
		veo.denAlpha = math.Sqrt(sumSq)
	})
	return veo
}

type veoMatch struct {
	x, y  int
	score float64
}

// findVeo correlates the text's alpha with the picture around its place.
func findVeo(img image.Image) veoMatch {
	t := veoMark()
	b := img.Bounds()
	best := veoMatch{score: -1}
	baseX, baseY := b.Max.X-veoRight-t.w, b.Max.Y-veoBottom-t.h
	n := float64(t.w * t.h)
	for dy := -veoSearch; dy <= veoSearch; dy++ {
		for dx := -veoSearch; dx <= veoSearch; dx++ {
			x0, y0 := baseX+dx, baseY+dy
			if !image.Rect(x0, y0, x0+t.w, y0+t.h).In(b) {
				continue
			}
			var sum, sumSq, prod float64
			for y := 0; y < t.h; y++ {
				for x := 0; x < t.w; x++ {
					c := color.RGBAModel.Convert(img.At(x0+x, y0+y)).(color.RGBA)
					v := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
					sum += v
					sumSq += v * v
					prod += v * float64(t.alpha[y*t.w+x])
				}
			}
			den := sumSq - sum*sum/n
			if den <= 0 {
				continue
			}
			if c := (prod - sum/n*t.sumAlpha) / (math.Sqrt(den) * t.denAlpha); c > best.score {
				best = veoMatch{x: x0, y: y0, score: c}
			}
		}
	}
	return best
}

// voteVeo keeps the place most sampled frames agree on, when its median
// correlation says the text is there.
func voteVeo(found []veoMatch) (veoMatch, bool) {
	votes := map[image.Point][]float64{}
	var best image.Point
	for _, m := range found {
		p := image.Pt(m.x, m.y)
		votes[p] = append(votes[p], m.score)
		if len(votes[p]) > len(votes[best]) {
			best = p
		}
	}
	scores := votes[best]
	if len(scores) < minVotes {
		return veoMatch{}, false
	}
	slices.Sort(scores)
	median := scores[len(scores)/2]
	return veoMatch{x: best.X, y: best.Y, score: median}, median >= minVeoScore
}

// removeVeo unblends the white text at m. The strokes are a pixel or two
// wide and video keeps colour at half resolution, so the text's colour is
// spread wider and fainter than its brightness: brightness is unblended with
// the sharp alpha and colour with the alpha averaged over 3x3, otherwise the
// strokes come back as dark letters. What is left is faint letter-shaped
// noise, so stroke pixels then take the median of their 5x5 neighbourhood,
// which is mostly picture since the strokes are thin.
func removeVeo(rgba *image.RGBA, m veoMatch) {
	t := veoMark()
	at := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= t.w || y >= t.h {
			return 0
		}
		return float64(t.alpha[y*t.w+x])
	}
	for y := -1; y <= t.h; y++ {
		for x := -1; x <= t.w; x++ {
			var soft float64
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					soft += at(x+dx, y+dy)
				}
			}
			soft /= 9
			a := at(x, y)
			if a < 0.002 && soft < 0.002 {
				continue
			}
			px, py := m.x+x, m.y+y
			if !image.Pt(px, py).In(rgba.Bounds()) {
				continue
			}
			c := rgba.RGBAAt(px, py)
			yy, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
			yy = unblend(yy, a, 255)
			cb = unblend(cb, soft, 128)
			cr = unblend(cr, soft, 128)
			r, g, b := color.YCbCrToRGB(yy, cb, cr)
			rgba.SetRGBA(px, py, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	box := image.Rect(m.x-2, m.y-2, m.x+t.w+2, m.y+t.h+2).Intersect(rgba.Bounds())
	src := image.NewRGBA(box)
	draw.Draw(src, box, rgba, box.Min, draw.Src)
	var r, g, b [25]uint8
	for y := 0; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			if at(x, y) < 0.05 {
				continue
			}
			n := 0
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c := src.RGBAAt(m.x+x+dx, m.y+y+dy)
					r[n], g[n], b[n] = c.R, c.G, c.B
					n++
				}
			}
			rgba.SetRGBA(m.x+x, m.y+y, color.RGBA{R: median(r), G: median(g), B: median(b), A: 255})
		}
	}
}
