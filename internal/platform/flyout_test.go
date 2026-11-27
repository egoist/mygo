package platform

import "testing"

func TestFlyoutPlace(t *testing.T) {
	work := Rect{0, 25, 1000, 700} // under a menu bar
	button := Rect{100, 100, 80, 20}
	size := Size{200, 150}
	tests := []struct {
		name   string
		f      Flyout
		anchor Rect
		size   Size
		want   Rect
		fx, fy bool
	}{
		{"below, start", Flyout{Gap: 4}, button, size, Rect{100, 124, 200, 150}, false, false},
		{"below, centered", Flyout{Align: AlignCenter}, button, size, Rect{40, 120, 200, 150}, false, false},
		{"below, end", Flyout{Align: AlignEnd}, Rect{400, 100, 80, 20}, size, Rect{280, 120, 200, 150}, false, false},
		{"flips to the start", Flyout{Align: AlignEnd}, button, size, Rect{100, 120, 200, 150}, true, false},
		{"above", Flyout{Side: SideTop, Gap: 4}, Rect{100, 400, 80, 20}, size, Rect{100, 246, 200, 150}, false, false},
		{"right", Flyout{Side: SideRight, Gap: 2}, button, size, Rect{182, 100, 200, 150}, false, false},
		{"left, end", Flyout{Side: SideLeft, Align: AlignEnd}, Rect{500, 300, 80, 20}, size, Rect{300, 170, 200, 150}, false, false},
		// Not enough room below: above instead, the gap on that side.
		{"flips up", Flyout{Gap: 4}, Rect{100, 600, 80, 20}, size, Rect{100, 446, 200, 150}, false, true},
		// Not enough room right of the start: lines up with the end.
		{"flips to the end", Flyout{}, Rect{900, 100, 80, 20}, size, Rect{780, 120, 200, 150}, true, false},
		// Not enough room on either side: stays, slides back in.
		{"slides", Flyout{}, Rect{100, 300, 80, 20}, Size{200, 600}, Rect{100, 125, 200, 600}, false, false},
		{"slides from the left", Flyout{Side: SideLeft}, Rect{50, 300, 80, 20}, Size{200, 100}, Rect{130, 300, 200, 100}, true, false},
		// Larger than the work area: its start shows, the rest is cut.
		{"shrinks", Flyout{}, button, Size{1200, 900}, Rect{0, 25, 1000, 700}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fx, fy := tt.f.Place(tt.anchor, tt.size, work)
			if got != tt.want || fx != tt.fx || fy != tt.fy {
				t.Errorf("Place = %+v, flipped %v %v; want %+v, %v %v", got, fx, fy, tt.want, tt.fx, tt.fy)
			}
		})
	}
}

func TestWorkAreaFor(t *testing.T) {
	displays := []Display{
		{Bounds: Rect{0, 0, 1920, 1080}, WorkArea: Rect{0, 25, 1920, 1055}},
		{Bounds: Rect{1920, -77, 1470, 956}, WorkArea: Rect{1920, -77, 1470, 900}},
	}
	for _, tt := range []struct {
		r    Rect
		want int
	}{
		{Rect{100, 100, 50, 20}, 0},
		{Rect{1900, 100, 50, 20}, 1}, // more of it on the second
		{Rect{1880, 100, 50, 20}, 0},
		{Rect{2000, 100, 0, 0}, 1},   // a point: the nearest display
		{Rect{5000, 100, 10, 10}, 1}, // off every display
		{Rect{-300, 5, 10, 10}, 0},
	} {
		if got := WorkAreaFor(tt.r, displays); got != displays[tt.want].WorkArea {
			t.Errorf("WorkAreaFor(%+v) = %+v, want display %d", tt.r, got, tt.want)
		}
	}
}

// TestFlyoutCentered checks where a flyout goes without an anchor: in the
// middle of the work area, as large as it fits.
func TestFlyoutCentered(t *testing.T) {
	work := Rect{X: 0, Y: 0, Width: 200, Height: 100}
	if r := Centered(Size{Width: 100, Height: 50}, work); r != (Rect{X: 50, Y: 25, Width: 100, Height: 50}) {
		t.Errorf("Centered = %v, want the middle", r)
	}
	// One too big for the work area is the work area.
	work = Rect{X: 10, Y: 20, Width: 200, Height: 100}
	if r := Centered(Size{Width: 500, Height: 500}, work); r != work {
		t.Errorf("Centered of a flyout too big = %v, want the work area", r)
	}
	// A work area away from the origin centers in it, not on the screen.
	work = Rect{X: 1000, Y: 500, Width: 200, Height: 100}
	if r := Centered(Size{Width: 100, Height: 50}, work); r != (Rect{X: 1050, Y: 525, Width: 100, Height: 50}) {
		t.Errorf("Centered in a far work area = %v", r)
	}
}

// TestPrimaryWorkArea checks whose work area a flyout without an anchor
// goes to: the primary display's, or the first one's when none is.
func TestPrimaryWorkArea(t *testing.T) {
	a := Rect{X: 0, Y: 0, Width: 100, Height: 100}
	b := Rect{X: 200, Y: 0, Width: 100, Height: 100}
	displays := []Display{{WorkArea: a}, {Primary: true, WorkArea: b}}
	if w := PrimaryWorkArea(displays); w != b {
		t.Errorf("PrimaryWorkArea = %v, want the primary's", w)
	}
	if w := PrimaryWorkArea([]Display{{WorkArea: a}, {WorkArea: b}}); w != a {
		t.Errorf("PrimaryWorkArea without a primary = %v, want the first's", w)
	}
	if w := PrimaryWorkArea(nil); w != (Rect{}) {
		t.Errorf("PrimaryWorkArea of no displays = %v", w)
	}
}
