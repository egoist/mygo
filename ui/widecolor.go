package ui

import (
	"math"

	"github.com/egoist/mygo/internal/gamut"
)

// Oklch returns the color of CSS's oklch(l c h): lightness l from 0 to 1,
// chroma c (about 0 to 0.4) and hue h in degrees. Use Alpha for oklch(l c h
// / a).
//
// A color Display P3 cannot show loses chroma, keeping its lightness and
// hue, as CSS does. A color outside the sRGB gamut is shown as it is by a
// window that draws a wide gamut, and as the nearest sRGB color by every
// other (see [Color]).
func Oklch(l, c, h float32) Color {
	r, g, b := gamut.Map(gamut.DisplayP3, float64(l), float64(c), float64(h))
	pr, pg, pb := gamut.LinearP3ToSRGB(r, g, b)
	w := wideRGB{float32(gamut.Encode(pr)), float32(gamut.Encode(pg)), float32(gamut.Encode(pb)), true}
	return fromWide(w)
}

// fromWide returns the color of w and, when it lies outside the sRGB
// gamut, w.
func fromWide(w wideRGB) Color {
	const eps = 1.0 / 512
	in := func(v float32) bool { return v >= -eps && v <= 1+eps }
	finite := func(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
	if !finite(w.r) || !finite(w.g) || !finite(w.b) {
		return Color{A: 255}
	}
	r, g, b := gamut.Decode(float64(w.r)), gamut.Decode(float64(w.g)), gamut.Decode(float64(w.b))
	l, c, h := gamut.Oklch(gamut.Oklab(r, g, b))
	r, g, b = gamut.Map(gamut.SRGB, l, c, h)
	byteOf := func(v float64) uint8 { return uint8(gamut.Encode(v)*255 + 0.5) }
	out := Color{R: byteOf(r), G: byteOf(g), B: byteOf(b), A: 255}
	if !in(w.r) || !in(w.g) || !in(w.b) {
		out.wide = w
	}
	return out
}
