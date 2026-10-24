package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// zoomSurface is a test surface that keeps what the window gives it.
type zoomSurface struct {
	testSurface
	w, h   int
	ime    platform.TextInputState
	access *platform.AccessTree
}

func (s *zoomSurface) PresentPixels(_ []byte, _, w, h int)        { s.w, s.h = w, h }
func (s *zoomSurface) SetTextInput(t platform.TextInputState)     { s.ime = t }
func (s *zoomSurface) UpdateAccessibility(t *platform.AccessTree) { s.access = t }

// TestZoom checks that a window's zoom lays its content out in a surface
// that much smaller, paints it at the surface's pixels, and turns what goes
// between them: the pointer, the caret and assistive technology's boxes.
func TestWindowZoom(t *testing.T) {
	t.Setenv("MYGO_GPU", "0")
	zoom := 2.0
	s := &zoomSurface{}
	h := &windowHost{conn: &surface.Conn{Surface: s, Zoom: func() float64 { return zoom }}}
	clicks, value := 0, ""
	h.rt = newRuntime(func(c *context) {
		coreColumn(c).Children(func() {
			coreBox(c).Size(10, 20)
			if coreBox(c).Size(40, 20).Clicked() {
				clicks++
			}
			coreTextInput(c, &value).Width(80)
		})
	}, h)
	h.conn.ZoomChanged = h.zoomChanged
	ev := func(e platform.SurfaceEvent) { h.event(e) }
	frame := func() { ev(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) }
	click := func(x, y float64) {
		ev(platform.SurfaceEvent{Kind: platform.PointerDown, X: x, Y: y, Clicks: 1})
		ev(platform.SurfaceEvent{Kind: platform.PointerUp, X: x, Y: y, Clicks: 1})
		frame()
	}
	frame()
	if w, ht, scale := h.size(); w != 100 || ht != 50 || scale != 2 {
		t.Fatalf("size at 2×: %v×%v at %v, want 100×50 at 2", w, ht, scale)
	}
	if s.w != 200 || s.h != 100 {
		t.Fatalf("frame of %d×%d pixels, want the surface's 200×100", s.w, s.h)
	}
	// The box is at 20 to 40 in the frame, 40 to 80 on the surface.
	click(10, 50)
	if clicks != 1 {
		t.Fatalf("a click on the zoomed box made %d clicks", clicks)
	}
	click(10, 30)
	if clicks != 1 {
		t.Fatal("a click above the zoomed box clicked it")
	}
	h.rt.accessibilityOn()
	frame()
	// The text input is 80 wide at 40 in the frame.
	var box platform.RectF
	for _, n := range s.access.Nodes {
		if n.Bounds.W == 160 {
			box = n.Bounds
		}
	}
	if box.Y != 80 {
		t.Fatalf("assistive technology's box of the text input: %+v, want 160 wide at y 80", box)
	}
	// The text input takes the keyboard: its caret is on the surface's
	// scale, and follows a change of the zoom.
	click(20, 90)
	if !s.ime.Active {
		t.Fatal("the text input took no keyboard")
	}
	caret := s.ime.Caret
	zoom = 1
	h.zoomChanged()
	frame()
	if s.ime.Caret.H*2 != caret.H || s.ime.Caret.Y*2 != caret.Y {
		t.Fatalf("caret at 1× %+v, at 2× %+v", s.ime.Caret, caret)
	}
	if w, _, scale := h.size(); w != 200 || scale != 1 {
		t.Fatalf("size at 1×: %v wide at %v", w, scale)
	}
}
