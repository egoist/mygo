package ui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// TestGlyphShadow paints two shadows of glyphs a Painter draws, under
// them, each offset and blurred as asked.
func TestGlyphShadow(t *testing.T) {
	black, red, blue := RGB(0, 0, 0), RGB(255, 0, 0), RGB(0, 0, 255)
	font := Font{Size: 32}
	glyphs := Shape("HH", font)
	tt := coreNewTester(func(c *context) {
		coreBox(c).Size(300, 100).Draw(func(p *Painter, r Rect) {
			x, y := r.X+20, r.Y+20+font.Metrics().Ascent
			// CSS's text-shadow: 5px 7px 4px red, -3px 2px blue.
			p.GlyphShadow(glyphs, x, y, NewTextShadow(-3, 2, 0, blue))
			p.GlyphShadow(glyphs, x, y, NewTextShadow(5, 7, 4, red))
			p.Glyphs(glyphs, x, y, black)
		})
	}, 300, 100)
	reds, fills, under := strokesAndFills(tt, red, black)
	blues, _, bluesUnder := strokesAndFills(tt, blue, black)
	if len(reds) != 2 || len(blues) != 2 || len(fills) != 2 || !under || !bluesUnder {
		t.Fatalf("%d red shadows, %d blue, %d fills, under them: %v, %v; want 2, 2, 2, true, true", len(reds), len(blues), len(fills), under, bluesUnder)
	}
	if !before(tt, blue, red) {
		t.Error("the red shadow paints under the blue one")
	}
	for i, f := range fills {
		s := reds[i]
		dx, dy := s.X+s.W/2-(f.X+f.W/2), s.Y+s.H/2-(f.Y+f.H/2)
		// The blur widens it by three sigmas, of half the blur, each side.
		if dx < 4 || dx > 6 || dy < 6 || dy > 8 || s.W < f.W+10 || s.H < f.H+10 {
			t.Errorf("red shadow %d at %v,%v %v×%v, its glyph at %v,%v %v×%v", i, s.X, s.Y, s.W, s.H, f.X, f.Y, f.W, f.H)
		}
		s = blues[i]
		dx, dy = s.X+s.W/2-(f.X+f.W/2), s.Y+s.H/2-(f.Y+f.H/2)
		if dx < -4 || dx > -2 || dy < 1 || dy > 3 || math.Abs(float64(s.W-f.W)) > 2 || math.Abs(float64(s.H-f.H)) > 2 {
			t.Errorf("blue shadow %d at %v,%v %v×%v, its glyph at %v,%v %v×%v", i, s.X, s.Y, s.W, s.H, f.X, f.Y, f.W, f.H)
		}
	}
}

// before tells whether the first glyph of color a paints before the
// first of color b.
func before(tt *Tester, a, b Color) bool {
	for _, op := range tt.h.last.Ops {
		if op.Kind != scene.OpGlyphs {
			continue
		}
		for _, g := range tt.h.last.Glyphs[op.Start:op.End] {
			switch g.Color {
			case a.scene():
				return true
			case b.scene():
				return false
			}
		}
	}
	return false
}

// TestGlyphShadowAsText casts the shadow of glyphs where an element's
// text casts it from its own: the same mask, as far from the glyph, dark
// and light.
func TestGlyphShadowAsText(t *testing.T) {
	// Colors of the same shade, so that the glyphs the shadows blur are
	// the same.
	for _, k := range []struct{ fill, shadow Color }{
		{RGB(0, 0, 0), RGB(0, 0, 64)},
		{RGB(255, 255, 255), RGB(250, 250, 255)},
	} {
		font := Font{Size: 32}
		element := coreNewTester(func(c *context) {
			coreText(c, "Hg").FontSize(32).TextColor(k.fill).TextShadow(5, 7, 4, k.shadow)
		}, 300, 100)
		glyphs := Shape("Hg", font)
		painter := coreNewTester(func(c *context) {
			coreBox(c).Size(300, 100).Draw(func(p *Painter, r Rect) {
				x, y := r.X+20, r.Y+20+font.Metrics().Ascent
				p.GlyphShadow(glyphs, x, y, NewTextShadow(5, 7, 4, k.shadow))
				p.Glyphs(glyphs, x, y, k.fill)
			})
		}, 300, 100)
		want, wantFills, _ := strokesAndFills(element, k.shadow, k.fill)
		got, gotFills, _ := strokesAndFills(painter, k.shadow, k.fill)
		if len(want) != 2 || len(got) != 2 || len(wantFills) != 2 || len(gotFills) != 2 {
			t.Fatalf("%v: %d and %d shadows, %d and %d fills; want 2 of each", k.shadow, len(want), len(got), len(wantFills), len(gotFills))
		}
		for i := range want {
			w, g := want[i], got[i]
			wf, gf := wantFills[i], gotFills[i]
			if w.X-wf.X != g.X-gf.X || w.Y-wf.Y != g.Y-gf.Y || w.W != g.W || w.H != g.H {
				t.Errorf("%v: shadow %d: %v,%v %v×%v from its glyph, want the element's %v,%v %v×%v",
					k.shadow, i, g.X-gf.X, g.Y-gf.Y, g.W, g.H, w.X-wf.X, w.Y-wf.Y, w.W, w.H)
			}
			if a, b := maskCoverage(element, w), maskCoverage(painter, g); a != b {
				t.Errorf("%v: shadow %d covers %v, want the element's %v", k.shadow, i, b, a)
			}
		}
	}
}

// maskCoverage returns the sum of the mask atlas's pixels under glyph g
// of the last frame of tt.
func maskCoverage(tt *Tester, g scene.Glyph) int {
	at := tt.rt.text.MaskAtlas
	sum := 0
	for y := int(g.V); y < int(g.V)+int(g.VH); y++ {
		for x := int(g.U); x < int(g.U)+int(g.UW); x++ {
			sum += int(at.Pix[y*at.W+x])
		}
	}
	return sum
}

// TestGlyphShadowLikeText renders the shadows of glyphs as an element
// renders its text's, pixel for pixel: scaled, faded, with marks off the
// baseline, at whole and fractional places and scales.
func TestGlyphShadowLikeText(t *testing.T) {
	black, shadow := RGB(0, 0, 0), RGB(0, 0, 64)
	wraps := []struct {
		name string
		fn   func(c *context, inner func())
	}{
		{"plain", func(c *context, inner func()) { inner() }},
		{"scaled", func(c *context, inner func()) { coreBox(c).Fill().Scale(1.7, 1.7).Children(inner) }},
		// One child and no background: the painter fades what it paints.
		{"faded", func(c *context, inner func()) { coreBox(c).Fill().Opacity(0.5).Children(inner) }},
	}
	for _, s := range []string{"Hg", "x̃ q̣̃ ǫ́"} {
		for _, scale := range []float32{1, 1.5, 2} {
			for _, w := range wraps {
				var n *node
				tt := coreNewTester(func(c *context) {
					w.fn(c, func() {
						n = coreText(c, s).Margin(20.3).FontSize(32).TextColor(black).TextShadow(2.5, 3.25, 4, shadow)
					})
				}, 300, 150)
				tt.SetScale(scale)
				element := tt.Image()
				line := &n.tl.Lines[0]
				x, y := n.x+n.contentX()+line.X, n.y+n.contentY()+line.Baseline
				glyphs := Shape(s, Font{Size: 32})
				painter := coreRender(func(c *context) {
					w.fn(c, func() {
						coreBox(c).Fill().Draw(func(p *Painter, r Rect) {
							p.GlyphShadow(glyphs, x, y, NewTextShadow(2.5, 3.25, 4, shadow))
							p.Glyphs(glyphs, x, y, black)
						})
					})
				}, 300, 150, scale)
				if d, at := differ(element, painter); d > 0 {
					t.Errorf("%q %s at scale %v: %d pixels differ from the element's, the first at %v", s, w.name, scale, d, at)
				}
			}
		}
	}
}

// differ returns how many pixels of a and b differ, and where the first
// does.
func differ(a, b *image.RGBA) (n int, at image.Point) {
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				if n == 0 {
					at = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, at
}

// TestGlyphShadowFaded fades the shadow of glyphs inside an element
// faded, alone, which the painter paints flat.
func TestGlyphShadowFaded(t *testing.T) {
	font := Font{Size: 60, Weight: 700}
	glyphs := Shape("I", font)
	render := func(opacity float32) color.RGBA {
		img := coreRender(func(c *context) {
			coreBox(c).Fill().Background(RGB(255, 255, 255)).Children(func() {
				coreBox(c).Fill().Opacity(opacity).Draw(func(p *Painter, r Rect) {
					p.GlyphShadow(glyphs, 10, 10+font.Metrics().Ascent, NewTextShadow(0, 0, 0, RGB(255, 0, 0)))
				})
			})
		}, 80, 90, 1)
		// The middle of the stem.
		var mid color.RGBA
		for x := 0; x < 80; x++ {
			if c := img.RGBAAt(x, 50); c.G < mid.G || mid.A == 0 {
				mid = c
			}
		}
		return mid
	}
	if c, half := render(1), render(0.5); c.G > 10 || half.G < 110 || half.G > 145 {
		t.Errorf("the middle of the shadow: %v, and %v at half opacity", c, half)
	}
}

// TestGlyphShadowClipped paints the shadows that reach into the clip:
// of a glyph outside it, and blurred into it from beyond its edge.
func TestGlyphShadowClipped(t *testing.T) {
	glyphs := Shape("H", Font{Size: 40})
	reds := func(view func(p *Painter)) int {
		img := coreRender(func(c *context) {
			coreBox(c).Fill().Background(RGB(255, 255, 255)).Draw(func(p *Painter, r Rect) {
				p.Clip(Rect{100, 0, 100, 100}, 0, func() { view(p) })
			})
		}, 300, 100, 1)
		n := 0
		for x := 100; x < 200; x++ {
			for y := 0; y < 100; y++ {
				if c := img.RGBAAt(x, y); c.G < 250 {
					n++
				}
			}
		}
		return n
	}
	red := RGB(255, 0, 0)
	// The glyph left of the clip, its shadow in it.
	if n := reds(func(p *Painter) { p.GlyphShadow(glyphs, 10, 60, NewTextShadow(110, 0, 0, red)) }); n == 0 {
		t.Error("the shadow of a glyph left of the clip does not show in it")
	}
	// The shadow's glyph right of the clip, its blur reaching into it.
	if n := reds(func(p *Painter) { p.GlyphShadow(glyphs, 0, 60, NewTextShadow(204, 0, 8, red)) }); n == 0 {
		t.Error("the blur of a shadow right of the clip does not reach into it")
	}
}

// TestGlyphShadowFontShade blurs the glyphs of Font.Thicken and
// Font.Antialiased as they are drawn, whatever the shadow's color.
func TestGlyphShadowFontShade(t *testing.T) {
	for _, font := range []Font{{Size: 15, Antialiased: true}, {Size: 15, Thicken: true}} {
		glyphs := Shape("Hamburg", font)
		dark, light := RGB(0, 0, 64), RGB(250, 250, 255)
		tt := coreNewTester(func(c *context) {
			coreBox(c).Fill().Draw(func(p *Painter, r Rect) {
				p.GlyphShadow(glyphs, 10, 20, NewTextShadow(0, 0, 0, dark))
				p.GlyphShadow(glyphs, 10, 50, NewTextShadow(0, 0, 0, light))
			})
		}, 200, 80)
		darks, lights, _ := strokesAndFills(tt, dark, light)
		if len(darks) == 0 || len(darks) != len(lights) {
			t.Fatalf("%+v: %d dark shadows, %d light", font, len(darks), len(lights))
		}
		for i := range darks {
			if a, b := maskCoverage(tt, darks[i]), maskCoverage(tt, lights[i]); a != b {
				t.Errorf("%+v: glyph %d's shadow covers %v in a dark color, %v in a light one", font, i, a, b)
			}
		}
	}
}

// TestGlyphShadowAlone paints the shadow of glyphs that are not drawn,
// as of transparent text: red beside where they would be, nothing there.
func TestGlyphShadowAlone(t *testing.T) {
	font := Font{Size: 40}
	glyphs := Shape("H", font)
	img := coreRender(func(c *context) {
		coreBox(c).Fill().Background(RGB(255, 255, 255)).Draw(func(p *Painter, r Rect) {
			p.GlyphShadow(glyphs, r.X+10, r.Y+10+font.Metrics().Ascent, NewTextShadow(50, 0, 0, RGB(255, 0, 0)))
		})
	}, 140, 70, 1)
	var glyph, shadow int
	for y := 10; y < 60; y++ {
		for x := 0; x < 140; x++ {
			if c := img.RGBAAt(x, y); c.G < 128 {
				if x < 55 {
					glyph++
				} else {
					shadow++
				}
			}
		}
	}
	if glyph != 0 || shadow == 0 {
		t.Errorf("red pixels: %d where the glyph is, %d to its right; want none and some", glyph, shadow)
	}
}
