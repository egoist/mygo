package ui

import (
	"testing"
	"time"
)

// TestKeptGlyphsOutliveFonts draws glyphs Shape made, kept across frames
// as its documentation says, after the text system let go of its fonts,
// which text of ever new sizes, as a pinch zooms, piles up.
func TestKeptGlyphsOutliveFonts(t *testing.T) {
	glyphs := Shape("Hello 你好", Font{Size: 13})
	size := float32(10)
	tt := NewTester(func(c *Context) {
		Box(c).Fill().Draw(func(p *Painter, r Rect) {
			p.Glyphs(glyphs, 0, 20, RGB(0, 0, 0))
			for range 64 {
				size += 0.01
				Shape("x", Font{Size: size})
			}
		})
		c.Invalidate()
	}, 200, 100)
	gen := textSystem().Generation()
	for i := 0; textSystem().Generation() == gen; i++ {
		if i == 100 {
			t.Fatal("the text system kept its fonts")
		}
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	img := tt.Image()
	ink := 0
	for y := range 30 {
		for x := range 120 {
			if c := img.RGBAAt(x, y); c.R < 128 && c.G < 128 && c.B < 128 {
				ink++
			}
		}
	}
	if ink == 0 {
		t.Error("the kept glyphs drew nothing once the text system let go of their fonts")
	}
}
