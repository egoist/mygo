package scene

import (
	"bytes"
	"testing"
)

// ownedBitmap returns the w×h bitmap a glyph of s shows, from its atlas.
func ownedBitmap(s *Scene, g Glyph) []byte {
	a := s.MaskAtlas
	if g.Colored || g.Subpixel {
		a = s.ColorAtlas
	}
	var pix []byte
	for y := range int(g.VH) {
		i := ((int(g.V)+y)*a.W + int(g.U)) * a.BPP
		pix = append(pix, a.Pix[i:][:int(g.UW)*a.BPP]...)
	}
	return pix
}

// TestOwn checks that a scene given atlases of its own draws the bitmaps
// its glyphs showed, though the atlases it was painted with then hold
// others in their places, from atlases holding nothing else.
func TestOwn(t *testing.T) {
	mask, color := NewAtlas(1, 64, 64), NewAtlas(4, 64, 64)
	put := func(a *Atlas, w, h int, fill byte) (uint16, uint16) {
		x, y, ok := a.Alloc(w, h)
		if !ok {
			t.Fatal("no room")
		}
		a.Put(x, y, w, h, bytes.Repeat([]byte{fill}, w*h*a.BPP), w*a.BPP)
		return uint16(x), uint16(y)
	}
	// Others first, so that the scene's are not at the atlases' corners.
	put(mask, 20, 20, 1)
	put(color, 20, 20, 2)
	au, av := put(mask, 9, 11, 30)
	bu, bv := put(mask, 30, 5, 60)
	cu, cv := put(color, 7, 7, 90)
	s := &Scene{MaskAtlas: mask, ColorAtlas: color}
	s.Glyphs = []Glyph{
		{U: au, V: av, UW: 9, VH: 11},
		{U: bu, V: bv, UW: 30, VH: 5},
		{U: cu, V: cv, UW: 7, VH: 7, Colored: true},
		{U: au, V: av, UW: 9, VH: 11}, // the first again
		{},                            // a glyph without pixels
	}
	var want [][]byte
	for _, g := range s.Glyphs {
		want = append(want, ownedBitmap(s, g))
	}
	var o Owned
	if !s.Own(&o) {
		t.Fatal("the bitmaps did not fit atlases of their own")
	}
	if s.MaskAtlas != o.Mask || s.ColorAtlas != o.Color || o.Mask == mask || o.Color == color {
		t.Fatal("the scene draws from the atlases it was painted with")
	}
	// As another frame making room in them leaves them.
	mask.Reset()
	color.Reset()
	for i, g := range s.Glyphs {
		if got := ownedBitmap(s, g); !bytes.Equal(got, want[i]) {
			t.Errorf("glyph %d shows another bitmap from the scene's own atlas", i)
		}
	}
	if s.Glyphs[0].U != s.Glyphs[3].U || s.Glyphs[0].V != s.Glyphs[3].V {
		t.Error("a bitmap two glyphs show was copied twice")
	}
	// No larger than they need: 31×6 and 10×12 padded, in 32 columns.
	if o.Mask.W != 32 || o.Mask.H > 24 || o.Color.W != 16 || o.Color.H > 16 {
		t.Errorf("atlases of %d×%d and %d×%d", o.Mask.W, o.Mask.H, o.Color.W, o.Color.H)
	}

	// Painted again with other glyphs of those sizes, the same atlas holds
	// those, as another generation of it.
	gen := o.Mask.Generation()
	s.MaskAtlas, s.ColorAtlas = mask, color
	du, dv := put(mask, 9, 11, 120)
	eu, ev := put(mask, 30, 5, 150)
	s.Glyphs = []Glyph{{U: du, V: dv, UW: 9, VH: 11}, {U: eu, V: ev, UW: 30, VH: 5}}
	kept := o.Mask
	if !s.Own(&o) || s.MaskAtlas != kept || kept.Generation() == gen {
		t.Fatalf("drawn again: the atlas reused %v, at generation %d after %d", s.MaskAtlas == kept, kept.Generation(), gen)
	}
	if s.ColorAtlas != color {
		t.Error("a scene without color glyphs took a color atlas of its own")
	}
	if got := ownedBitmap(s, s.Glyphs[1]); !bytes.Equal(got, bytes.Repeat([]byte{150}, 150)) {
		t.Error("drawn again, it shows another bitmap")
	}
}

// TestOwnTooLarge checks that a scene whose bitmaps fit no atlas keeps the
// atlases it was painted with, its glyphs where they were.
func TestOwnTooLarge(t *testing.T) {
	mask := &Atlas{BPP: 1, W: 2 * MaxAtlasSize, H: 4, Pix: make([]byte, 2*MaxAtlasSize*4)}
	s := &Scene{MaskAtlas: mask, Glyphs: []Glyph{{U: 3, V: 1, UW: MaxAtlasSize, VH: 2}}}
	var o Owned
	if s.Own(&o) || s.MaskAtlas != mask || s.Glyphs[0].U != 3 || s.Glyphs[0].V != 1 {
		t.Errorf("a bitmap wider than the largest atlas: owned, glyph at %d,%d", s.Glyphs[0].U, s.Glyphs[0].V)
	}
	// One beyond the atlas it names is left alone too.
	s = &Scene{MaskAtlas: NewAtlas(1, 16, 16), Glyphs: []Glyph{{U: 10, V: 10, UW: 8, VH: 8}}}
	if s.Own(&o) {
		t.Error("a glyph beyond its atlas was copied")
	}
}
