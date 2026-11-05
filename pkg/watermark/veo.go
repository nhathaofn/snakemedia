package watermark

import (
	_ "embed"
	"image"
	"image/color"
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
	gradient []float64
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
		veo.gradient = alphaGradient(veo.alpha, veo.w, veo.h)
	})
	return veo
}

type veoMatch struct {
	x, y  int
	score float64
	gain  float64
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
				best = veoMatch{x: x0, y: y0, score: c, gain: 1}
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
	return veoMatch{x: best.X, y: best.Y, score: median, gain: 1}, median >= minVeoScore
}

// removeVeo seamlessly inpaints the small (24x12) "Veo" text mark at m by
// interpolating from the boundary pixels around the bounding box. This avoids
// leaving dark letter residues or smudges on compressed video backgrounds.
func removeVeo(rgba *image.RGBA, m veoMatch) {
	t := veoMark()
	bounds := rgba.Bounds()
	x0 := max(bounds.Min.X+1, m.x-2)
	y0 := max(bounds.Min.Y+1, m.y-2)
	x1 := min(bounds.Max.X-2, m.x+t.w+2)
	y1 := min(bounds.Max.Y-2, m.y+t.h+2)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	bw := float64(x1 - x0)
	bh := float64(y1 - y0)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			cTop := rgba.RGBAAt(x, y0-1)
			cBottom := rgba.RGBAAt(x, y1+1)
			cLeft := rgba.RGBAAt(x0-1, y)
			cRight := rgba.RGBAAt(x1+1, y)

			wxTop := (bh - float64(y-y0)) / bh
			wxBottom := float64(y-y0) / bh
			wxLeft := (bw - float64(x-x0)) / bw
			wxRight := float64(x-x0) / bw

			rHoriz := wxLeft*float64(cLeft.R) + wxRight*float64(cRight.R)
			gHoriz := wxLeft*float64(cLeft.G) + wxRight*float64(cRight.G)
			bHoriz := wxLeft*float64(cLeft.B) + wxRight*float64(cRight.B)

			rVert := wxTop*float64(cTop.R) + wxBottom*float64(cBottom.R)
			gVert := wxTop*float64(cTop.G) + wxBottom*float64(cBottom.G)
			bVert := wxTop*float64(cTop.B) + wxBottom*float64(cBottom.B)

			rgba.SetRGBA(x, y, color.RGBA{
				R: clamp8((rHoriz + rVert) / 2),
				G: clamp8((gHoriz + gVert) / 2),
				B: clamp8((bHoriz + bVert) / 2),
				A: 255,
			})
		}
	}
}
