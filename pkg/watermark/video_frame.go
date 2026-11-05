package watermark

import (
	"image"
	"image/color"
	"math"
	"slices"
)

const (
	// Above these contrasts, background edges dominate the local gain fit.
	maxVideoBackgroundSpread = 2.5 * rangeSigma
	maxVideoBackgroundEdge   = 3 * rangeSigma
)

// frameStar keeps the position fixed (avoiding a dancing repair) while fitting
// opacity to this frame. A sharp scene edge outside the overlay makes that fit
// unreliable, so the voted estimate remains the safer choice there.
func frameStar(img *image.RGBA, base matchResult) matchResult {
	if _, spread, ok := coreGap(img, base, base.gain); ok && spread > maxVideoBackgroundSpread {
		return base
	}
	sz, alpha := base.size, base.template.alpha
	luma := func(x, y int) float64 {
		c := img.RGBAAt(base.x+x, base.y+y)
		return .299*float64(c.R) + .587*float64(c.G) + .114*float64(c.B)
	}
	for y := 0; y < sz-1; y++ {
		for x := 0; x < sz-1; x++ {
			i := y*sz + x
			if alpha[i] > .01 {
				continue
			}
			for _, d := range []image.Point{{X: 1}, {Y: 1}} {
				if alpha[(y+d.Y)*sz+x+d.X] < .01 && math.Abs(luma(x+d.X, y+d.Y)-luma(x, y)) > maxVideoBackgroundEdge {
					return base
				}
			}
		}
	}
	if gain, ok := blendGain(img, base); ok && fitsSurroundings(img, base, gain) {
		base.gain = refineGainOnFlat(img, base, gain)
	}
	return base
}

func frameText(img *image.RGBA, base veoMatch) veoMatch {
	t := veoMark()
	values := make([]float64, len(t.alpha))
	var sum, sumSq, count float64
	for y := 0; y < t.h; y++ {
		for x := 0; x < t.w; x++ {
			c := img.RGBAAt(base.x+x, base.y+y)
			v := .299*float64(c.R) + .587*float64(c.G) + .114*float64(c.B)
			i := y*t.w + x
			values[i] = (v - 255) * t.gradient[i]
			if t.alpha[i] < .01 {
				sum += v
				sumSq += v * v
				count++
			}
		}
	}
	if count > 0 && math.Sqrt(math.Max(0, sumSq/count-sum*sum/(count*count))) > maxVideoBackgroundSpread {
		return base
	}
	if gain, ok := solveGain(t.alpha, values, .2, 1.4); ok {
		base.gain = gain
	}
	return base
}

func reconstructVideoStar(rgba *image.RGBA, match matchResult) {
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
			rgba.SetRGBA(wmX+x, wmY+y, color.RGBA{
				R: unblend(c.R, a, col[0]),
				G: unblend(c.G, a, col[1]),
				B: unblend(c.B, a, col[2]),
				A: 255,
			})
		}
	}
	smoothVideoContour(rgba, match)
	denoiseInside(rgba, match, gain)
}

func smoothVideoContour(rgba *image.RGBA, match matchResult) {
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
	var r, g, b [9]uint8
	for y := -band; y < sz+band; y++ {
		for x := -band; x < sz+band; x++ {
			if !nearEdge(x, y) {
				continue
			}
			orig := src.RGBAAt(match.x+x, match.y+y)
			n := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					c := src.RGBAAt(match.x+x+dx, match.y+y+dy)
					r[n], g[n], b[n] = c.R, c.G, c.B
					n++
				}
			}
			mr, mg, mb := median9(r), median9(g), median9(b)
			diffR := math.Abs(float64(orig.R) - float64(mr))
			diffG := math.Abs(float64(orig.G) - float64(mg))
			diffB := math.Abs(float64(orig.B) - float64(mb))
			if diffR < 45 && diffG < 45 && diffB < 45 {
				rgba.SetRGBA(match.x+x, match.y+y, color.RGBA{R: mr, G: mg, B: mb, A: 255})
			}
		}
	}
}

func median9(v [9]uint8) uint8 {
	slices.Sort(v[:])
	return v[4]
}
