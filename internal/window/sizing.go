// Package window computes the main window geometry from the user's screens.
package window

import (
	"math"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Size is a width/height pair in logical (DPI-independent) pixels, the unit
// expected by runtime.WindowSetSize.
type Size struct {
	Width  int
	Height int
}

// PrimaryScreen returns the logical size of the primary screen, falling back to
// the screen hosting the window, then to the first screen with a usable size.
func PrimaryScreen(screens []runtime.Screen) (Size, bool) {
	pick := func(match func(runtime.Screen) bool) (Size, bool) {
		for _, s := range screens {
			if match(s) && s.Size.Width > 0 && s.Size.Height > 0 {
				return Size{Width: s.Size.Width, Height: s.Size.Height}, true
			}
		}
		return Size{}, false
	}
	if size, ok := pick(func(s runtime.Screen) bool { return s.IsPrimary }); ok {
		return size, true
	}
	if size, ok := pick(func(s runtime.Screen) bool { return s.IsCurrent }); ok {
		return size, true
	}
	return pick(func(runtime.Screen) bool { return true })
}

// Fit scales each screen dimension by scale, then clamps the result so it is
// never smaller than minSize nor larger than the screen itself.
func Fit(screen Size, scale float64, minSize Size) Size {
	return Size{
		Width:  fitDimension(screen.Width, scale, minSize.Width),
		Height: fitDimension(screen.Height, scale, minSize.Height),
	}
}

func fitDimension(screen int, scale float64, minimum int) int {
	v := int(math.Round(float64(screen) * scale))
	if v < minimum {
		v = minimum
	}
	if v > screen {
		v = screen
	}
	return v
}
