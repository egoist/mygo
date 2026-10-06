package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/internal/scene"
)

// Color is a color with straight (not premultiplied) alpha. R, G, B and A are
// its sRGB value. A Color from [Oklch] may lie outside the sRGB gamut and
// then also holds the wide color, which a renderer that draws a wide gamut
// (the GPU one on a Mac) shows in place of R, G and B; every other one shows
// them, the nearest sRGB color, found as CSS Color 4 does. Set R, G or B of
// such a color and the wide color is stale: make a new one instead.
type Color struct {
	R, G, B, A uint8
	wide       wideRGB
}

// wideRGB is the sRGB-encoded components of a color, extended beyond 0 to 1
// (a green only Display P3 has reads above 1 in G), when ok.
type wideRGB struct {
	r, g, b float32
	ok      bool
}

// Transparent is the color of nothing.
var Transparent = Color{}

// RGB returns an opaque color.
func RGB(r, g, b uint8) Color { return Color{R: r, G: g, B: b, A: 255} }

// RGBA returns a color with alpha between 0 and 1.
func RGBA(r, g, b uint8, alpha float32) Color { return Color{R: r, G: g, B: b, A: alphaByte(alpha)} }

// Hex parses "#rgb", "#rgba", "#rrggbb" or "#rrggbbaa". It panics on other
// input, which is a mistake in the program.
func Hex(s string) Color {
	c, err := parseHex(s)
	if err != nil {
		panic(err)
	}
	return c
}

func parseHex(s string) (Color, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	var v [8]uint8
	for i := 0; i < len(h) && i < len(v); i++ {
		d, ok := hexDigit(h[i])
		if !ok {
			return Color{}, fmt.Errorf("ui: invalid color %q", s)
		}
		v[i] = d
	}
	switch len(h) {
	case 3, 4:
		// #rgb and #rgba double each digit.
		a := uint8(15)
		if len(h) == 4 {
			a = v[3]
		}
		return Color{R: v[0] * 17, G: v[1] * 17, B: v[2] * 17, A: a * 17}, nil
	case 6, 8:
		a := uint8(255)
		if len(h) == 8 {
			a = v[6]<<4 | v[7]
		}
		return Color{R: v[0]<<4 | v[1], G: v[2]<<4 | v[3], B: v[4]<<4 | v[5], A: a}, nil
	}
	return Color{}, fmt.Errorf("ui: invalid color %q", s)
}

// hexDigit returns the value of a hexadecimal digit.
func hexDigit(c byte) (uint8, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func alphaByte(a float32) uint8 {
	if a <= 0 {
		return 0
	}
	if a >= 1 {
		return 255
	}
	return uint8(a*255 + 0.5)
}

// Alpha returns the color with its alpha multiplied by a.
func (c Color) Alpha(a float32) Color {
	c.A = alphaByte(float32(c.A) / 255 * a)
	return c
}

// SRGB returns c without its wide color: the nearest sRGB color, which a
// window that cannot draw a wide gamut shows.
func (c Color) SRGB() Color {
	c.wide = wideRGB{}
	return c
}

// wideRGBA returns the wide color of c, with its alpha, if it has one.
func (c Color) wideRGBA() ([4]float32, bool) {
	if !c.wide.ok {
		return [4]float32{}, false
	}
	return [4]float32{c.wide.r, c.wide.g, c.wide.b, float32(c.A) / 255}, true
}

// wideColors returns the wide color of c as the Color of an op.
func (c Color) wideColors() (w scene.WideColors) {
	if a, ok := c.wideRGBA(); ok {
		w = scene.WideColors{Color: a, Set: scene.WideColor}
	}
	return w
}

// components returns the red, green and blue of c, as floats from 0 to 1
// and beyond for a wide color.
func (c Color) components() (r, g, b float32) {
	if c.wide.ok {
		return c.wide.r, c.wide.g, c.wide.b
	}
	return float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255
}

// gray returns the color in shades of gray, of the same luminance.
func (c Color) gray() Color {
	l := uint8(0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B) + 0.5)
	return Color{R: l, G: l, B: l, A: c.A}
}

// Mix returns the color t of the way from c to o.
func (c Color) Mix(o Color, t float32) Color {
	t = max(0, min(t, 1))
	m := func(a, b uint8) uint8 { return uint8(float32(a)*(1-t) + float32(b)*t + 0.5) }
	r := Color{R: m(c.R, o.R), G: m(c.G, o.G), B: m(c.B, o.B), A: m(c.A, o.A)}
	if c.wide.ok || o.wide.ok {
		cr, cg, cb := c.components()
		or, og, ob := o.components()
		mf := func(a, b float32) float32 { return a*(1-t) + b*t }
		r.wide = wideRGB{mf(cr, or), mf(cg, og), mf(cb, ob), true}
	}
	return r
}

// Over returns c composited over the opaque color base.
func (c Color) Over(base Color) Color {
	a := float32(c.A) / 255
	o := c.Mix(base, 1-a)
	o.A = base.A
	return o
}

func (c Color) scene() scene.Color { return scene.Color{R: c.R, G: c.G, B: c.B, A: c.A} }

// wideSet returns the wide colors of an op with the colors c, c2 and
// border, those that have one.
func wideSet(c, c2, border Color) (w scene.WideColors) {
	if a, ok := c.wideRGBA(); ok {
		w.Color, w.Set = a, w.Set|scene.WideColor
	}
	if a, ok := c2.wideRGBA(); ok {
		w.Color2, w.Set = a, w.Set|scene.WideColor2
	}
	if a, ok := border.wideRGBA(); ok {
		w.Border, w.Set = a, w.Set|scene.WideBorder
	}
	return w
}

// setGlyphColor sets the colors of g to c, times opacity.
func setGlyphColor(g *scene.Glyph, c Color, opacity float32) {
	c = c.Alpha(opacity)
	g.Color = c.scene()
	g.Wide, g.HasWide = c.wideRGBA()
}
