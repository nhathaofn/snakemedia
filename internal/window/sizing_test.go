package window

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func screen(w, h int, primary, current bool) runtime.Screen {
	s := runtime.Screen{IsPrimary: primary, IsCurrent: current}
	s.Size.Width, s.Size.Height = w, h
	return s
}

func TestFit(t *testing.T) {
	minSize := Size{Width: 800, Height: 600}
	cases := []struct {
		name   string
		screen Size
		scale  float64
		want   Size
	}{
		{"full HD at 70%", Size{1920, 1080}, 0.7, Size{1344, 756}},
		{"QHD at 70%", Size{2560, 1440}, 0.7, Size{1792, 1008}},
		{"small laptop clamps to minimum", Size{1366, 768}, 0.7, Size{956, 600}},
		{"never larger than the screen", Size{1024, 576}, 0.7, Size{800, 576}},
		{"full scale", Size{1920, 1080}, 1.0, Size{1920, 1080}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Fit(tc.screen, tc.scale, minSize); got != tc.want {
				t.Errorf("Fit(%v, %v) = %v; want %v", tc.screen, tc.scale, got, tc.want)
			}
		})
	}
}

func TestPrimaryScreen(t *testing.T) {
	cases := []struct {
		name    string
		screens []runtime.Screen
		want    Size
		ok      bool
	}{
		{"prefers primary", []runtime.Screen{screen(1280, 720, false, true), screen(2560, 1440, true, false)}, Size{2560, 1440}, true},
		{"falls back to current", []runtime.Screen{screen(1280, 720, false, false), screen(1920, 1080, false, true)}, Size{1920, 1080}, true},
		{"falls back to first usable", []runtime.Screen{screen(0, 0, true, false), screen(1600, 900, false, false)}, Size{1600, 900}, true},
		{"none", nil, Size{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PrimaryScreen(tc.screens)
			if got != tc.want || ok != tc.ok {
				t.Errorf("PrimaryScreen() = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
