package scene

import "testing"

// TestPremul checks the color premultiplied by its alpha and its opacity.
func TestPremul(t *testing.T) {
	p := Color{255, 128, 0, 128}.Premul(1)
	for i, w := range [4]float32{0.50196, 0.25196, 0, 0.50196} {
		if p[i] < w-1e-4 || p[i] > w+1e-4 {
			t.Errorf("Premul channel %d = %v, want %v", i, p[i], w)
		}
	}
	// Half opaque halves every channel, the alpha with them.
	p = Color{255, 128, 0, 255}.Premul(0.5)
	for i, w := range [4]float32{0.5, 0.25098, 0, 0.5} {
		if p[i] < w-1e-4 || p[i] > w+1e-4 {
			t.Errorf("half Premul channel %d = %v, want %v", i, p[i], w)
		}
	}
}

// TestRectContains checks the rectangle a point is in: its right and
// bottom edges are not.
func TestRectContains(t *testing.T) {
	r := Rect{X: 10, Y: 10, W: 10, H: 10}
	for _, tc := range []struct {
		x, y float32
		want bool
	}{
		{10, 10, true},
		{19.9, 19.9, true},
		{20, 10, false},
		{10, 20, false},
		{9.9, 10, false},
		{10, 9.9, false},
	} {
		if got := r.Contains(tc.x, tc.y); got != tc.want {
			t.Errorf("Contains(%v, %v) = %v", tc.x, tc.y, got)
		}
	}
}

// TestHasStopsAndBorder checks the questions a paint and a border ask.
func TestHasStopsAndBorder(t *testing.T) {
	if PaintSolid.HasStops() {
		t.Error("a solid paint takes no stops")
	}
	if !PaintLinearStops.HasStops() || !PaintRadialOklab.HasStops() {
		t.Error("a gradient paint takes stops")
	}
	if Paint(PaintRadialOklab + 1).HasStops() {
		t.Error("a paint past the gradients takes no stops")
	}
	if HasBorder([4]float32{}) {
		t.Error("no widths, no border")
	}
	if !HasBorder(Uniform(2)) {
		t.Error("a width everywhere is a border")
	}
	if !HasBorder([4]float32{0, 0, 0, 1}) {
		t.Error("a width on one side is a border")
	}
	if HasBorder([4]float32{-1, 0, 0, 0}) {
		t.Error("a negative width is no border")
	}
}

// TestInnerRadii checks the radii of a border's inner edge: each corner's
// less the wider of its two sides, fitted to the inner rectangle.
func TestInnerRadii(t *testing.T) {
	inner, radii := InnerRadii(Rect{X: 0, Y: 0, W: 100, H: 100},
		[4]float32{10, 20, 30, 40}, [4]float32{5, 6, 7, 8})
	if inner != (Rect{X: 8, Y: 5, W: 86, H: 88}) {
		t.Errorf("inner = %v, want (8, 5, 86, 88)", inner)
	}
	// Top left loses the wider of top (5) and left (8); the others the
	// wider of theirs.
	for i, w := range [4]float32{2, 14, 23, 32} {
		if radii[i] != w {
			t.Errorf("inner radius %d = %v, want %v", i, radii[i], w)
		}
	}

	// Radii too big for the inner rectangle are scaled down to fit: the
	// corners lose their sides' widths first, 60 to 52 and 54 here, then
	// the fit shrinks them.
	_, radii = InnerRadii(Rect{X: 0, Y: 0, W: 100, H: 100},
		[4]float32{60, 60, 0, 0}, [4]float32{5, 6, 7, 8})
	if radii[0] < 42.18 || radii[0] > 42.20 || radii[1] < 43.80 || radii[1] > 43.82 ||
		radii[2] != 0 || radii[3] != 0 {
		t.Errorf("fitted radii = %v, want (42.19, 43.81, 0, 0)", radii)
	}

	// Continuous corners keep their sign, negative.
	_, radii = InnerRadii(Rect{X: 0, Y: 0, W: 100, H: 100},
		[4]float32{-10, 20, 30, 40}, [4]float32{5, 6, 7, 8})
	for i, w := range [4]float32{-2, -14, -23, -32} {
		if radii[i] != w {
			t.Errorf("continuous inner radius %d = %v, want %v", i, radii[i], w)
		}
	}
}

// TestCorners checks what the renderers take: fitted, and negative for
// continuous corners.
func TestCorners(t *testing.T) {
	if radii := Corners(Rect{X: 0, Y: 0, W: 40, H: 40}, [4]float32{100, 0, 0, 0}, false); radii[0] != 40 {
		t.Errorf("Corners of an oversized radius = %v, want (40, 0, 0, 0)", radii)
	}
	if radii := Corners(Rect{X: 0, Y: 0, W: 40, H: 40}, [4]float32{4, 4, 4, 4}, true); radii[0] != -4 || radii[3] != -4 {
		t.Errorf("continuous Corners = %v, want them negative", radii)
	}
}
