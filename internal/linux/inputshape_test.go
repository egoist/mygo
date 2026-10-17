//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// The tests here are the ledger of the area's input shape over the views
// under it (shapeInput): the pixels MyGo tells GDK are the surface's own,
// and the pixels it leaves to the web views and the app's controls below.
// They replace the cairo region entry points with real regions over the
// pixels of an allocation (cairoStub), so what a test reads back is the
// set the subtracts and the unions left, in the order they came: the
// grouping of the placements is what the shape is. They assert MyGo's
// calls and what they come to; only the real toolkit, in internal/e2e,
// says where a click then lands.

// testOverlayWin is the window GTK gives the overlay the area is in, whose
// input shape shapeInput sets.
const testOverlayWin = ptr(0x9000)

// The allocation the tests shape.
const (
	shapeW, shapeH = 40, 30
)

// cairoStub is the cairo region API of a test: a region is the set of the
// whole pixels in it, as cairo's regions are, and every operation works on
// that set, so the pixels a test reads back are the ones the operations
// left and not merely the calls MyGo made.
type cairoStub struct {
	regions map[ptr]map[[2]int]bool
	next    ptr

	// shaped is whether MyGo gave GDK a region, the window it gave it to,
	// and the pixels of that region.
	shaped   bool
	shapeWin ptr
	shape    map[[2]int]bool

	// ops is the ledger of the operations, for a failure that names them.
	ops []string
}

// newCairoStub replaces the cairo region entry points and the GTK calls
// shapeInput makes with stubs over a w x h allocation, and puts them back
// when the test ends.
func newCairoStub(t *testing.T, w, h int) *cairoStub {
	t.Helper()
	c := &cairoStub{regions: map[ptr]map[[2]int]bool{}}
	stubGTKFunc(t, &cairoRegionCreateRectangle, c.create)
	stubGTKFunc(t, &cairoRegionSubtractRectangle, c.subtract)
	stubGTKFunc(t, &cairoRegionUnion, c.union)
	stubGTKFunc(t, &cairoRegionDestroy, c.destroy)
	stubGTKFunc(t, &gdkWindowInputShapeCombineRegion, c.combine)
	// The area is an overlay's child, not the window's own widget (the
	// shape is only set in the first case), and its allocation is the
	// window's.
	stubGTKFunc(t, &gtkWidgetGetParentWindow, func(ptr) ptr { return testOverlayWin })
	stubGTKFunc(t, &gtkWidgetGetWindow, func(ptr) ptr { return testWin })
	stubGTKFunc(t, &gtkWidgetGetAllocation, func(_ ptr, a *gdkRectangle) {
		*a = gdkRectangle{Width: int32(w), Height: int32(h)}
	})
	return c
}

// pixels is the whole pixels of a rectangle: cairo's rectangles are whole
// pixels, and gdkRect makes them so.
func pixels(r gdkRectangle) map[[2]int]bool {
	p := map[[2]int]bool{}
	for x := int(r.X); x < int(r.X+r.Width); x++ {
		for y := int(r.Y); y < int(r.Y+r.Height); y++ {
			p[[2]int{x, y}] = true
		}
	}
	return p
}

// create stands in for cairo_region_create_rectangle.
func (c *cairoStub) create(r *gdkRectangle) ptr {
	c.next++
	c.regions[c.next] = pixels(*r)
	c.ops = append(c.ops, fmt.Sprintf("create %+v", *r))
	return c.next
}

// subtract stands in for cairo_region_subtract_rectangle.
func (c *cairoStub) subtract(region ptr, r *gdkRectangle) int32 {
	for p := range pixels(*r) {
		delete(c.regions[region], p)
	}
	c.ops = append(c.ops, fmt.Sprintf("subtract %+v", *r))
	return 0
}

// union stands in for cairo_region_union.
func (c *cairoStub) union(region, other ptr) int32 {
	for p := range c.regions[other] {
		c.regions[region][p] = true
	}
	c.ops = append(c.ops, "union")
	return 0
}

// destroy stands in for cairo_region_destroy.
func (c *cairoStub) destroy(region ptr) {
	delete(c.regions, region)
	c.ops = append(c.ops, "destroy")
}

// combine stands in for gdk_window_input_shape_combine_region: it keeps
// the pixels of the region, which the code destroys right after.
func (c *cairoStub) combine(w, region ptr, x, y int32) {
	c.shaped, c.shapeWin = true, w
	c.shape = map[[2]int]bool{}
	for p := range c.regions[region] {
		c.shape[[2]int{p[0] + int(x), p[1] + int(y)}] = true
	}
	c.ops = append(c.ops, "combine")
}

// shapeOf shapes the area of a w x h surface showing the placements, and
// returns the pixels MyGo left to the surface itself, which takes the
// pointer there: the pixels it does not hold go through the area to the
// views below.
func shapeOf(t *testing.T, w, h int, web []platform.WebViewPlacement, native []platform.NativeViewPlacement) (*cairoStub, map[[2]int]bool) {
	t.Helper()
	c := newCairoStub(t, w, h)
	s := &surface{w: &window{win: testWin}, area: testArea, web: testWeb}
	s.placed = web
	if len(native) > 0 {
		// The native placements are the extension's, which the surface
		// gets when it makes its first native view (nativeview.go): a
		// surface with one shapes through it, as the app's surface does.
		s.placement().placed = native
	}
	s.shapeInput()
	if !c.shaped {
		t.Fatalf("the shape was never given to GDK: ops %v", c.ops)
	}
	if c.shapeWin != testOverlayWin {
		t.Errorf("the shape went to window %#x, want the overlay's %#x", c.shapeWin, testOverlayWin)
	}
	return c, c.shape
}

// checkPixels checks pixels of the shape against what the diagram of a
// test says they are: true where the surface takes the pointer there,
// false where the pixel is a view's.
func checkPixels(t *testing.T, c *cairoStub, shape map[[2]int]bool, want map[[2]int]bool) {
	t.Helper()
	for _, p := range sortedPixels(want) {
		if got := shape[p]; got != want[p] {
			side := "a view's"
			if want[p] {
				side = "the surface's"
			}
			gotSide := "a view's"
			if got {
				gotSide = "the surface's"
			}
			t.Errorf("pixel (%d, %d) is %s, want %s\nops: %v", p[0], p[1], gotSide, side, c.ops)
		}
	}
}

// sortedPixels returns the pixels of a want-map in a fixed order, so that
// a failure reads the same every run.
func sortedPixels(m map[[2]int]bool) [][2]int {
	out := make([][2]int, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j][0] < out[j-1][0] || out[j][0] == out[j-1][0] && out[j][1] < out[j-1][1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// The boxes of the mixed test, on a shapeW x shapeH surface:
//
//	(5,5) +-------------+ A (the native view)
//	      |    +-------------+ B (the web view)
//	      |    |  overlap    |
//	      +----|------------+
//	           |  [Q]   [P]  |
//	           +-------------+
//
// A is 20x15 at (5, 5): x 5..25, y 5..20. B is 20x15 at (15, 12): x
// 15..35, y 12..27. They overlap on x 15..25, y 12..20. Q is 3x3 at
// (17, 13), a popup painted over both, inside the overlap; P is 5x5 at
// (28, 15), a popup painted over B alone.
var (
	shapeA      = platform.RectF{X: 5, Y: 5, W: 20, H: 15}
	shapeB      = platform.RectF{X: 15, Y: 12, W: 20, H: 15}
	shapeOver   = platform.RectF{X: 15, Y: 12, W: 10, H: 8}
	shapePopup  = platform.RectF{X: 28, Y: 15, W: 5, H: 5}
	shapePopup2 = platform.RectF{X: 17, Y: 13, W: 3, H: 3}
)

// TestInputShapeNativeOrder checks what the tests of a native view and a
// web view cannot: two native views over each other, the later one taking
// the overlap, and each keeping its own part.
func TestInputShapeNativeOrder(t *testing.T) {
	c, shape := shapeOf(t, shapeW, shapeH, nil, []platform.NativeViewPlacement{
		{Clip: shapeA, Covers: []platform.RectF{shapeOver}, Order: 0},
		{Clip: shapeB, Order: 1},
	})
	checkPixels(t, c, shape, map[[2]int]bool{
		{8, 8}:   false, // A alone
		{32, 24}: false, // B alone
		{20, 16}: false, // the overlap: the later native view's
		{2, 2}:   true,
	})
}

// TestInputShapeIdenticalRectsOrder checks two views showing in the very
// same box, reordered with nothing remade: every pixel of the box is the
// later view's, whichever it is, and the content painted over them after
// both keeps the surface's.
func TestInputShapeIdenticalRectsOrder(t *testing.T) {
	box := platform.RectF{X: 10, Y: 10, W: 20, H: 20}
	popup := platform.RectF{X: 15, Y: 15, W: 4, H: 4}
	// A second popup over the same box: the content's popups of the later
	// view are all put back, not just the first of them.
	popup2 := platform.RectF{X: 18, Y: 24, W: 4, H: 4}
	want := map[[2]int]bool{
		{10, 10}: false, // the box's first pixel
		{12, 12}: false,
		{27, 27}: false,
		{29, 29}: false, // its last
		{16, 16}: true,  // the popup over them both
		{20, 26}: true,  // and the second one
		{9, 9}:   true,  // the content around the box
		{35, 25}: true,
	}
	for _, tc := range []struct {
		name   string
		web    []platform.WebViewPlacement
		native []platform.NativeViewPlacement
	}{
		{
			name:   "the web view painted last",
			web:    []platform.WebViewPlacement{{Clip: box, Covers: []platform.RectF{popup, popup2}, Order: 1}},
			native: []platform.NativeViewPlacement{{Clip: box, Covers: []platform.RectF{box}, Order: 0}},
		},
		{
			name:   "the native view painted last",
			web:    []platform.WebViewPlacement{{Clip: box, Covers: []platform.RectF{box}, Order: 0}},
			native: []platform.NativeViewPlacement{{Clip: box, Covers: []platform.RectF{popup, popup2}, Order: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, shape := shapeOf(t, shapeW, shapeH, tc.web, tc.native)
			checkPixels(t, c, shape, want)
		})
	}
}

// TestInputShapeFractionalBounds checks fractional bounds through the
// region operations: a view whose clip is fractional gives up exactly the
// whole pixels gdkRect makes of it, and the content painted over it takes
// back exactly those of the cover.
func TestInputShapeFractionalBounds(t *testing.T) {
	// A clip x 1.5..12, y 2.5..7: the pixels x 2..11, y 3..6.
	clip := platform.RectF{X: 1.5, Y: 2.5, W: 10.5, H: 4.5}
	// A popup over it, x 2.5..5.5, y 3.5..5.5: the pixels x 3..4, y 4.
	cover := platform.RectF{X: 2.5, Y: 3.5, W: 3, H: 2}
	c, shape := shapeOf(t, shapeW, shapeH, []platform.WebViewPlacement{{Clip: clip, Covers: []platform.RectF{cover}}}, nil)
	checkPixels(t, c, shape, map[[2]int]bool{
		{2, 3}: false, {11, 6}: false, // the clip's whole pixels
		{5, 4}: false, {4, 3}: false, // in the clip, beside the popup
		{3, 4}: true, {4, 4}: true, // the popup's, taken back
		{1, 3}: true, {12, 6}: true, // the fractions around the clip
		{2, 2}: true, {2, 7}: true, // and around it the other way
	})
}

// TestGDKRectFractionalBounds is what gdkRect makes of fractional
// rectangles: the pixels they cover wholly, from the ceil of the origin to
// the floor of the far edge, never less than none.
func TestGDKRectFractionalBounds(t *testing.T) {
	for _, tc := range []struct {
		in   platform.RectF
		want gdkRectangle
	}{
		{platform.RectF{X: 0, Y: 0, W: 10, H: 10}, gdkRectangle{X: 0, Y: 0, Width: 10, Height: 10}},
		{platform.RectF{X: 1.5, Y: 2.5, W: 10.5, H: 4.5}, gdkRectangle{X: 2, Y: 3, Width: 10, Height: 4}},
		{platform.RectF{X: 10.5, Y: 20.5, W: 3, H: 3}, gdkRectangle{X: 11, Y: 21, Width: 2, Height: 2}},
		{platform.RectF{X: -2.5, Y: -3.5, W: 10, H: 10}, gdkRectangle{X: -2, Y: -3, Width: 9, Height: 9}},
		{platform.RectF{X: 5, Y: 5, W: 0, H: 10}, gdkRectangle{X: 5, Y: 5, Width: 0, Height: 10}},
		{platform.RectF{X: 5, Y: 5, W: -3, H: 10}, gdkRectangle{X: 5, Y: 5, Width: 0, Height: 10}},
		{platform.RectF{X: 1.2, Y: 1.8, W: 0.5, H: 0.5}, gdkRectangle{X: 2, Y: 2, Width: 0, Height: 0}},
	} {
		if got := *gdkRect(tc.in); got != tc.want {
			t.Errorf("gdkRect(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// TestInputShapeEmptyIsTheWholeSurface checks the window showing no view
// under its area: the content takes the pointer everywhere, and the shape
// is the allocation, nothing outside it.
func TestInputShapeEmptyIsTheWholeSurface(t *testing.T) {
	c, shape := shapeOf(t, shapeW, shapeH, nil, nil)
	if len(shape) != shapeW*shapeH {
		t.Errorf("the shape has %d pixels, want the whole %dx%d allocation: %v", len(shape), shapeW, shapeH, c.ops)
	}
	for _, p := range [][2]int{{0, 0}, {10, 10}, {shapeW - 1, shapeH - 1}} {
		if !shape[p] {
			t.Errorf("pixel %v is not the surface's, want all of an empty surface: %v", p, c.ops)
		}
	}
	for _, p := range [][2]int{{-1, 0}, {shapeW, 0}, {0, shapeH}} {
		if shape[p] {
			t.Errorf("pixel %v is the surface's, outside the allocation: %v", p, c.ops)
		}
	}
}

// TestInputShapeOfTheWindowItself checks the early way out: the area of a
// window whose widget the area is (not an overlay's child) sets no shape,
// so GTK keeps whatever shape the window has.
func TestInputShapeOfTheWindowItself(t *testing.T) {
	c := newCairoStub(t, shapeW, shapeH)
	stubGTKFunc(t, &gtkWidgetGetParentWindow, func(ptr) ptr { return testWin })
	s := &surface{w: &window{win: testWin}, area: testArea, web: testWeb}
	s.placed = []platform.WebViewPlacement{{Clip: shapeA}}
	s.shapeInput()
	if c.shaped {
		t.Errorf("the shape of the area's own window was set: %v", c.ops)
	}
}
