package ui

import "testing"

// A style's zero fields follow the theme: the thumb's color and width,
// a bar six DIPs wider than the thumb, half its width as the corner, and
// 24 DIPs as the shortest thumb. A thumb wider than its bar is cut to it.
func TestScrollbarStyleResolve(t *testing.T) {
	th := *DarkTheme()
	th.Scrollbar, th.ScrollbarWidth = RGB(10, 20, 30), 8
	s := ScrollbarStyle{}.Resolve(&th)
	if s.Width != 14 || s.ThumbWidth != 8 || s.Radius != 4 || s.MinLength != 24 {
		t.Errorf("the defaults are %+v, want a 14 DIP bar with an 8 DIP thumb, radius 4, at least 24 long", s)
	}
	if s.Thumb != th.Scrollbar || s.Hover != th.Scrollbar || s.Active != th.Scrollbar || s.Track != Transparent {
		t.Errorf("the default colors are %+v, want the theme's thumb and no track", s)
	}
	track, hover := RGB(1, 2, 3), RGB(4, 5, 6)
	s = ScrollbarStyle{Width: 8, ThumbWidth: 20, Radius: 1, MinLength: 5, Track: track, Hover: hover}.Resolve(&th)
	if s.ThumbWidth != 8 {
		t.Errorf("a thumb %v wide in an 8 DIP bar stays %v wide", 20, s.ThumbWidth)
	}
	if s.Radius != 1 || s.MinLength != 5 || s.Track != track || s.Hover != hover || s.Active != th.Scrollbar {
		t.Errorf("the style is %+v, want its own radius, length, track and hover, and the theme's active", s)
	}
}

// A Scrollbar paints the thumb a scroll container shows for the same
// view, content and offset, pixel for pixel, dragged or not; and OffsetAt
// undoes Thumb.
func TestScrollbarLikeContainer(t *testing.T) {
	const content = 600
	var offset float64
	var dragging bool
	tt := coreNewTester(func(c *context) {
		coreRow(c).Size(200, 200).Children(func() {
			sc := coreScroll(c).Size(100, 200).Children(func() { coreBox(c).Size(100, content) })
			sc.st.scrollY = offset
			coreBox(c).Size(100, 200).Draw(func(p *Painter, r Rect) {
				p.Scrollbar(Scrollbar{Box: r, View: r.H, Content: content, Offset: float32(offset)}, ScrollbarState{Dragging: dragging})
			})
		})
	}, 200, 200)
	same := func(what string) {
		t.Helper()
		img := tt.Image()
		s := img.Bounds().Dx() / 200
		painted := false
		for y := 0; y < 200*s; y++ {
			for x := 80 * s; x < 100*s; x++ {
				a, b := img.RGBAAt(x, y), img.RGBAAt(x+100*s, y)
				if a != b {
					t.Fatalf("%s: at (%d, %d) the container's bar is %v, the Scrollbar's %v", what, x, y, a, b)
				}
				painted = painted || a != img.RGBAAt(10*s, 10*s)
			}
		}
		if !painted {
			t.Fatalf("%s: no thumb painted", what)
		}
	}
	tt.Move(50, 100)
	tt.Frame()
	same("at the top")
	offset = 250
	tt.Frame()
	same("scrolled")
	// The container's thumb held: both widen.
	g := Scrollbar{Box: Rect{0, 0, 100, 200}, View: 200, Content: content, Offset: 250}
	start, n := g.Thumb()
	dragging = true
	tt.Press(95, start+n/2)
	tt.Frame()
	same("dragged")
	tt.Release(95, start+n/2)
	for _, off := range []float32{0, 100, 250, 400} {
		g.Offset = off
		start, _ := g.Thumb()
		if got := g.OffsetAt(start); got < off-0.01 || got > off+0.01 {
			t.Errorf("OffsetAt(Thumb()) at %v = %v", off, got)
		}
	}
	if got := g.OffsetAt(-100); got != 0 {
		t.Errorf("OffsetAt above the track = %v, want 0", got)
	}
	if got := g.OffsetAt(500); got != content-200 {
		t.Errorf("OffsetAt below the track = %v, want %v", got, content-200)
	}
}
