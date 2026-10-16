package main

import "testing"

// PointerDown asks for DIPs from the top-left of the view MyGo hosts, whatever
// the platform's own coordinates are: the bounds origin (a scroll view's scroll
// offset) comes off, and a view that is not flipped counts Y upward from the
// bottom of its bounds.
func TestLocalDIPs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		p       point
		b       bounds
		flipped bool
		want    point
	}{
		{"flipped", point{X: 12, Y: 34}, bounds{Width: 320, Height: 160}, true, point{X: 12, Y: 34}},
		{"unflipped", point{X: 12, Y: 34}, bounds{Width: 320, Height: 160}, false, point{X: 12, Y: 126}},
		{"unflipped top left", point{X: 0, Y: 160}, bounds{Width: 320, Height: 160}, false, point{X: 0, Y: 0}},
		{"flipped, scrolled", point{X: 12, Y: 234}, bounds{Y: 200, Width: 320, Height: 160}, true, point{X: 12, Y: 34}},
		{"unflipped, scrolled", point{X: 14, Y: 190}, bounds{X: 4, Y: 50, Width: 320, Height: 160}, false, point{X: 10, Y: 20}},
		{"unflipped, scrolled to its top left", point{X: 4, Y: 210}, bounds{X: 4, Y: 50, Width: 320, Height: 160}, false, point{X: 0, Y: 0}},
	} {
		if got := localDIPs(tc.p, tc.b, tc.flipped); got != tc.want {
			t.Errorf("%s: localDIPs(%+v, %+v, %v) = %+v, want %+v", tc.name, tc.p, tc.b, tc.flipped, got, tc.want)
		}
	}
}

// The point a press reports is where it is in the part of the view the user
// sees, so scrolling the content must not move it: without this, a press
// drifts by the scroll offset once the document is scrolled, which is exactly
// what converting into a scrolled view's content coordinates does.
func TestLocalDIPsIgnoresScrolling(t *testing.T) {
	for _, flipped := range []bool{true, false} {
		// The visible part's top-left in the view's own coordinates, and a
		// point 20 DIPs below it.
		inView := func(scroll, down float64) point {
			y := scroll + down
			if !flipped {
				y = scroll + 160 - down
			}
			return point{X: 3, Y: y}
		}
		for _, scroll := range []float64{0, 40, 200, 1000} {
			b := bounds{X: 2, Y: scroll, Width: 320, Height: 160}
			for _, down := range []float64{0, 20, 159} {
				if got, want := localDIPs(inView(scroll, down), b, flipped), (point{X: 1, Y: down}); got != want {
					t.Errorf("flipped=%v scroll=%v down=%v: got %+v, want %+v", flipped, scroll, down, got, want)
				}
			}
		}
	}
}
