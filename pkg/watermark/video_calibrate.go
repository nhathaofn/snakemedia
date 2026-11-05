package watermark

import (
	"image"
	"image/color"
	"math"
	"slices"
)

type videoPlane [3]float64

func (p videoPlane) at(x, y int) float64 { return p[0] + p[1]*float64(x) + p[2]*float64(y) }

func frameLuma(img image.Image, x, y int) float64 {
	c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	return .299*float64(c.R) + .587*float64(c.G) + .114*float64(c.B)
}

const videoPlaneResidual = 14.0 // H.264 compression noise on smooth gradients exceeds flatSpread (8)

// fitVideoPlane uses only pixels outside the known mark. Busy backgrounds
// fail the residual check and never supply a calibration estimate.
func fitVideoPlane(img image.Image, m matchResult) (videoPlane, bool) {
	var n, sx, sy, sxx, syy, sxy, sv, sxv, syv float64
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			if m.template.alpha[y*m.size+x] >= .01 {
				continue
			}
			xx, yy, v := float64(x), float64(y), frameLuma(img, m.x+x, m.y+y)
			n++
			sx += xx
			sy += yy
			sxx += xx * xx
			syy += yy * yy
			sxy += xx * yy
			sv += v
			sxv += xx * v
			syv += yy * v
		}
	}
	if n < 3 {
		return videoPlane{}, false
	}
	xx, yy, xy := sxx-sx*sx/n, syy-sy*sy/n, sxy-sx*sy/n
	det := xx*yy - xy*xy
	if math.Abs(det) < 1e-9 {
		return videoPlane{}, false
	}
	xv, yv := sxv-sx*sv/n, syv-sy*sv/n
	bx, by := (xv*yy-yv*xy)/det, (yv*xx-xv*xy)/det
	p := videoPlane{sv/n - bx*sx/n - by*sy/n, bx, by}
	var residual float64
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			if m.template.alpha[y*m.size+x] < .01 {
				d := frameLuma(img, m.x+x, m.y+y) - p.at(x, y)
				residual += d * d
			}
		}
	}
	return p, math.Sqrt(residual/n) <= videoPlaneResidual
}

// calibrateVideoStar measures small alpha-map differences on several smooth
// frames. A single scene edge cannot become part of the mask: estimates must
// be near the detected template and agree over time. No image pixels are filled
// from the fitted background; the map is used only for reverse blending.
func calibrateVideoStar(samples []sampledCorner, m matchResult) matchResult {
	if !m.found {
		return m
	}
	type sample struct {
		image sampledCorner
		plane videoPlane
	}
	var good []sample
	for _, img := range samples {
		if p, ok := fitVideoPlane(img, m); ok {
			good = append(good, sample{img, p})
		}
	}
	if len(good) < minVotes {
		return m
	}
	colour := .299*m.template.colour[0] + .587*m.template.colour[1] + .114*m.template.colour[2]
	// A changing-opacity overlay must retain the original mask: mixing pixel
	// estimates from different strengths would distort its shape.
	var peak float32
	for _, a := range m.template.alpha {
		peak = max(peak, a)
	}
	var strengths []float64
	for _, s := range good {
		var measured, expected float64
		for y := 0; y < m.size; y++ {
			for x := 0; x < m.size; x++ {
				a := m.template.alpha[y*m.size+x]
				if a < peak*.8 {
					continue
				}
				bg := s.plane.at(x, y)
				den := colour - bg
				if den < 32 {
					continue
				}
				measured += (frameLuma(s.image, m.x+x, m.y+y) - bg) / den
				expected += float64(a)
			}
		}
		if expected > 0 {
			strengths = append(strengths, measured/expected)
		}
	}
	if len(strengths) < minVotes {
		return m
	}
	slices.Sort(strengths)
	if strengths[3*len(strengths)/4]-strengths[len(strengths)/4] > .12*strengths[len(strengths)/2] {
		return m
	}
	rows := make([][]float32, m.size)
	for y := 0; y < m.size; y++ {
		rows[y] = make([]float32, m.size)
		for x := 0; x < m.size; x++ {
			prior := float64(m.template.alpha[y*m.size+x]) * m.gain
			rows[y][x] = float32(prior)
			if prior < .002 {
				continue
			}
			var estimates []float64
			for _, s := range good {
				bg := s.plane.at(x, y)
				den := colour - bg
				if den < 32 {
					continue
				}
				estimates = append(estimates, math.Max(0, math.Min(.9, (frameLuma(s.image, m.x+x, m.y+y)-bg)/den)))
			}
			if len(estimates) < minVotes {
				continue
			}
			slices.Sort(estimates)
			median := estimates[len(estimates)/2]
			var deviations []float64
			for _, v := range estimates {
				deviations = append(deviations, math.Abs(v-median))
			}
			slices.Sort(deviations)
			if math.Abs(median-prior) <= .12 && deviations[len(deviations)/2] <= .025 {
				rows[y][x] = float32(median)
			}
		}
	}
	t := m.template
	m.template = newTemplate(rows, t.minMargin, t.maxMargin, t.minGain, t.maxGain, t.edgeBand, t.denoise, t.colour)
	m.gain = 1
	return m
}
