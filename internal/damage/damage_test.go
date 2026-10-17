package damage

import (
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// These cover what a scene draws with besides its operations: the text
// parameters every glyph's coverage is corrected with, the color atlas
// subpixel glyphs hold their coverage in, and the colors outside the sRGB
// gamut a renderer drawing them takes. A change in any of those shows in
// the pixels, so a frame that draws part of the window must not miss it.

// pixels returns n bytes that differ from those of an earlier call with
// another seed, for a bitmap's worth of coverage.
func pixels(n int, seed byte) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i) ^ seed*37
	}
	return p
}

func TestTrackerRedrawsTextParameterChange(t *testing.T) {
	mask := scene.NewAtlas(1, 16, 16)
	mask.Put(0, 0, 4, 4, make([]byte, 16), 4)
	s := &scene.Scene{Width: 40, Height: 20, Clear: scene.Color{A: 255}, MaskAtlas: mask}
	s.Glyphs = []scene.Glyph{{X: 2, Y: 2, W: 4, H: 4, UW: 4, VH: 4, Color: scene.Color{R: 255, A: 255}}}
	s.Ops = []scene.Op{{Kind: scene.OpGlyphs, Start: 0, End: 1}}
	var tr Tracker
	if !tr.Whole(s) {
		t.Fatal("the first scene drew whole")
	}
	tr.Remember(s)
	s.Text.Contrast = 1
	if !tr.Whole(s) {
		t.Error("a change of the text parameters drew only part of the window")
	}
}

func TestTrackerRedrawsSubpixelGlyphAtlasChange(t *testing.T) {
	mask := scene.NewAtlas(1, 16, 16)
	color := scene.NewAtlas(4, 16, 16)
	x, y, ok := color.Alloc(4, 4)
	if !ok {
		t.Fatal("cannot allocate in the color atlas")
	}
	color.Put(x, y, 4, 4, make([]byte, 4*4*4), 16)
	s := &scene.Scene{Width: 40, Height: 20, Clear: scene.Color{A: 255}, MaskAtlas: mask, ColorAtlas: color}
	s.Glyphs = []scene.Glyph{{X: 2, Y: 2, W: 4, H: 4, U: uint16(x), V: uint16(y), UW: 4, VH: 4,
		Color: scene.Color{R: 255, A: 255}, Subpixel: true}}
	s.Ops = []scene.Op{{Kind: scene.OpGlyphs, Start: 0, End: 1}}
	var tr Tracker
	if !tr.Whole(s) {
		t.Fatal("the first scene drew whole")
	}
	tr.Remember(s)
	// New coverage where the glyph draws, as the color atlas reuses the
	// place of a subpixel glyph of the last frame.
	color.Put(x, y, 4, 4, pixels(64, 3), 16)
	if !tr.Whole(s) && len(tr.Rects()) == 0 {
		t.Error("a change of the color atlas under a subpixel glyph drew nothing")
	}
}

func TestTrackerRedrawsWideColors(t *testing.T) {
	sceneWithWide := func() *scene.Scene {
		s := &scene.Scene{Width: 40, Height: 20, Clear: scene.Color{A: 255}}
		s.Wide = []scene.WideColors{{Color: [4]float32{1.2, 0, 0, 1}, Set: scene.WideColor}}
		s.Ops = []scene.Op{{Kind: scene.OpFill, Rect: scene.Rect{W: 8, H: 8}, Color: scene.Color{R: 255, A: 255}, Wide: 1}}
		return s
	}
	// A renderer drawing the colors outside the sRGB gamut redraws when the
	// table it takes them from changes.
	s := sceneWithWide()
	tr := Tracker{Wide: true}
	if !tr.Whole(s) {
		t.Fatal("the first scene drew whole")
	}
	tr.Remember(s)
	s.Wide[0].Color = [4]float32{0, 1.3, 0, 1}
	if !tr.Whole(s) {
		t.Error("a change of the wide colors drew only part of the window")
	}
	// And when an operation's place in the table changes.
	s = sceneWithWide()
	tr = Tracker{Wide: true}
	tr.Whole(s)
	tr.Remember(s)
	s.Ops[0].Wide = 0
	if !tr.Whole(s) && len(tr.Rects()) == 0 {
		t.Error("a change of an operation's wide colors drew nothing")
	}
	// A renderer drawing the sRGB colors ignores them: the pixels it draws
	// are the operation's Color, which did not change.
	s = sceneWithWide()
	tr = Tracker{}
	if !tr.Whole(s) {
		t.Fatal("the first scene drew whole")
	}
	tr.Remember(s)
	s.Wide[0].Color = [4]float32{1.5, 1.5, 0, 1}
	if tr.Whole(s) {
		t.Error("a renderer drawing the sRGB colors drew whole for a change of the wide colors alone")
	}
}
