package ui

import (
	"bytes"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// TestCaptureAfterAnotherWindowDrew checks that capturing a window shows
// what it shows though another window drew a frame since: windows share
// the atlases of glyphs and masks, where the masks a frame draws for the
// first time, as an animation's, last that frame only.
func TestCaptureAfterAnotherWindowDrew(t *testing.T) {
	t.Setenv("MYGO_GPU", "0")
	// Each window shows a wedge, of a new angle in each of its frames
	// as a gauge moving.
	window := func(angle *float32) *windowHost {
		h := &windowHost{conn: &surface.Conn{Surface: &testSurface{}, Post: func(f func()) { f() }}}
		h.rt = newRuntime(func(c *context) {
			coreBox(c).Fill().Draw(func(p *Painter, r Rect) {
				var path Path
				path.MoveTo(40, 80)
				path.LineTo(120, 80)
				path.LineTo(40, 80-*angle)
				path.Close()
				p.FillPath(&path, RGB(20, 60, 230))
			})
		}, h)
		return h
	}
	frame := func(h *windowHost) { h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) }
	angleA, angleB := float32(50), float32(70)
	a, b := window(&angleA), window(&angleB)
	frame(a)
	_, _, want := a.capture()
	frame(b)
	if _, _, got := a.capture(); !bytes.Equal(got, want) {
		t.Fatal("the capture shows the masks another window drew since")
	}
}
