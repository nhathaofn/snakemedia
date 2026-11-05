package watermark

import (
	"image"
	"image/draw"
	"math"
	"testing"
)

func TestFrameStarTracksChangingOpacity(t *testing.T) {
	initTemplates()
	tmpl := videoTemplates[1]
	base := matchResult{found: true, x: 568, y: 288, size: tmpl.size, gain: 1, template: tmpl}
	for _, gain := range []float64{.55, .8, 1.25} {
		clean := syntheticVideoBackground(640, 360, 0, "flat-dark")
		marked := image.NewRGBA(clean.Bounds())
		draw.Draw(marked, marked.Bounds(), clean, image.Point{}, draw.Src)
		stampVideoTemplate(marked, base.x, base.y, tmpl, gain)
		current := frameStar(marked, base)
		if math.Abs(current.gain-gain) > .06 {
			t.Fatalf("gain %.2f fitted as %.3f", gain, current.gain)
		}
		reconstructWatermark(marked, current)
		var diff float64
		for y := base.y; y < base.y+base.size; y++ {
			for x := base.x; x < base.x+base.size; x++ {
				a, b := clean.RGBAAt(x, y), marked.RGBAAt(x, y)
				diff += math.Abs(float64(a.R)-float64(b.R)) + math.Abs(float64(a.G)-float64(b.G)) + math.Abs(float64(a.B)-float64(b.B))
			}
		}
		if mean := diff / float64(3*base.size*base.size); mean > 2 {
			t.Fatalf("gain %.2f residual %.3f", gain, mean)
		}
	}
}

func TestFrameTextTracksChangingOpacity(t *testing.T) {
	m := veoMark()
	base := veoMatch{x: 1280 - veoRight - m.w, y: 720 - veoBottom - m.h, gain: 1}
	for _, gain := range []float64{.55, .8, 1.2} {
		img := syntheticVideoBackground(1280, 720, 0, "flat-dark")
		stampVideoAlpha(img, base.x, base.y, m.w, m.h, m.alpha, gain, white)
		got := frameText(img, base)
		if math.Abs(got.gain-gain) > .07 {
			t.Fatalf("text gain %.2f fitted as %.3f", gain, got.gain)
		}
	}
}

func TestFrameStarDoesNotFitAcrossHighContrastSceneEdges(t *testing.T) {
	initTemplates()
	tmpl := videoTemplates[1]
	base := matchResult{found: true, x: 568, y: 288, size: tmpl.size, gain: .9, template: tmpl}
	img := syntheticVideoBackground(640, 360, 10, "edge-crossing")
	stampVideoTemplate(img, base.x, base.y, tmpl, 1)
	if got := frameStar(img, base); got.gain != base.gain {
		t.Fatalf("scene edge changed the stable gain: %.3f", got.gain)
	}
}

func TestVoteDoesNotCombineDifferentTemplates(t *testing.T) {
	initTemplates()
	a := matchResult{found: true, x: 1136, y: 576, size: 48, gain: 1, score: .8, template: videoTemplates[0]}
	b := a
	b.template = videoTemplates[2]
	if vote([]matchResult{a, b, a, b}).found {
		t.Fatal("different alpha maps incorrectly reached consensus")
	}
	got := vote([]matchResult{a, b, a, a})
	if !got.found || &got.template.alpha[0] != &a.template.alpha[0] {
		t.Fatal("majority alpha map not retained")
	}
}

func TestSampledCornerMatchesFullFrameDetection(t *testing.T) {
	initTemplates()
	img := syntheticVideoBackground(640, 360, 0, "gradient")
	tmpl := videoTemplates[1]
	stampVideoTemplate(img, 568, 288, tmpl, 1)
	full := findWith(img, videoTemplates)
	patch := image.NewRGBA(image.Rect(480, 200, 640, 360))
	draw.Draw(patch, patch.Bounds(), img, patch.Bounds().Min, draw.Src)
	corner := findWith(sampledCorner{RGBA: patch, full: img.Bounds()}, videoTemplates)
	if !full.found || !corner.found || full.x != corner.x || full.y != corner.y || full.size != corner.size || math.Abs(full.gain-corner.gain) > 1e-8 {
		t.Fatalf("full=%+v corner=%+v", full, corner)
	}
}

func TestFastBlendGainMatchesGradientDefinition(t *testing.T) {
	initTemplates()
	for _, name := range []string{"flat-dark", "gradient", "texture-pan", "edge-crossing"} {
		img := syntheticVideoBackground(640, 360, 5, name)
		tmpl := videoTemplates[1]
		stampVideoTemplate(img, 568, 288, tmpl, .9)
		m := matchResult{x: 568, y: 288, size: tmpl.size, template: tmpl}
		fast, okFast := blendGain(img, m)
		residual := func(g float64) float64 {
			v := make([]float64, m.size*m.size)
			for y := 0; y < m.size; y++ {
				for x := 0; x < m.size; x++ {
					c := img.RGBAAt(m.x+x, m.y+y)
					a := math.Min(.95, float64(tmpl.alpha[y*m.size+x])*g)
					v[y*m.size+x] = ((float64(c.R)+float64(c.G)+float64(c.B))/3 - a*255) / (1 - a)
				}
			}
			var sum float64
			for y := 0; y < m.size-1; y++ {
				for x := 0; x < m.size-1; x++ {
					i := y*m.size + x
					sum += (v[i+1]-v[i])*float64(tmpl.alpha[i+1]-tmpl.alpha[i]) + (v[i+m.size]-v[i])*float64(tmpl.alpha[i+m.size]-tmpl.alpha[i])
				}
			}
			return sum
		}
		lo, hi := tmpl.minGain, tmpl.maxGain
		okSlow := residual(lo) > 0 && residual(hi) < 0
		for range 24 {
			mid := (lo + hi) / 2
			if residual(mid) > 0 {
				lo = mid
			} else {
				hi = mid
			}
		}
		if okFast != okSlow || okFast && math.Abs(fast-(lo+hi)/2) > 1e-6 {
			t.Fatalf("%s: fast %.8f/%v reference %.8f/%v", name, fast, okFast, (lo+hi)/2, okSlow)
		}
	}
}
