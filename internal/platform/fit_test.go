package platform

import "testing"

func TestFitWindow(t *testing.T) {
	work := Rect{X: 0, Y: 38, Width: 1800, Height: 1087}
	for _, tt := range []struct {
		name string
		r    Rect
		want Rect
	}{
		{"inside", Rect{100, 100, 800, 600}, Rect{100, 100, 800, 600}},
		{"taller", Rect{100, 266, 800, 1410}, Rect{100, 38, 800, 1087}},
		{"wider", Rect{-500, 100, 2000, 600}, Rect{0, 100, 1800, 600}},
		{"above the top", Rect{100, -400, 800, 600}, Rect{100, 38, 800, 600}},
		{"below the bottom", Rect{100, 1120, 800, 600}, Rect{100, 1075, 800, 600}},
		{"off the left", Rect{-790, 100, 800, 600}, Rect{-700, 100, 800, 600}},
		{"off the right", Rect{1750, 100, 800, 600}, Rect{1700, 100, 800, 600}},
		{"smaller than the minimum", Rect{-100, 100, 60, 40}, Rect{0, 100, 60, 40}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := FitWindow(tt.r, work, 100, 50); got != tt.want {
				t.Errorf("FitWindow(%+v) = %+v, want %+v", tt.r, got, tt.want)
			}
		})
	}
}
