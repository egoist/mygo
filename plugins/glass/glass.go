// Package glass draws Liquid Glass in native UI, as macOS 26 and later
// draw it (AppKit's NSGlassEffectView), on every platform: what the
// elements under it painted shows through, blurred, bent near its edges as
// through the rim of a lens, and lit along its rim, over a soft shadow.
// MyGo's renderers draw it with shaders of this package, on the GPU or the
// CPU, so it looks the same everywhere.
//
//	ui.Row(c).Padding(8, 16).Radius(22).Material(glass.Glass{})
package glass

//go:generate go run ./internal/gen

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Glass is Liquid Glass, a ui.Material: Element.Material fills an element
// with it, in place of a background, shaped by its Radius. What was
// painted under the element shows through it, as content that scrolls
// under a bar placed over it with Absolute, and other glass.
type Glass struct {
	// Style is the glass's material.
	Style Style
	// Tint colors the glass, by its alpha, as for a prominent button: an
	// opaque tint lets a little of the glass show, as AppKit's does.
	Tint ui.Color
	// Interactive makes the glass react as the element is pressed, with
	// a glow under the pointer, as AppKit's interactive glass does.
	Interactive bool
}

// Style is the material of Glass.
type Style uint8

const (
	// Regular frosts what shows through and lightens it, or darkens it in
	// dark mode, so that what is on the glass reads over anything: for
	// bars, buttons and panels. Larger panes are frostier.
	Regular Style = iota
	// Clear barely blurs nor tones what shows through, for glass over
	// photos and video, where what is on it brings its own contrast.
	Clear
)

// pressKey is the key of an interactive glass's state and animation.
type pressKey struct{}

// pressed is an interactive glass as its element is built: how far it
// glows, from 0 to 1, and where, relative to the element.
type pressed struct {
	Glass
	glow float32
	at   [2]float32
}

// BuildMaterial follows the press of an interactive glass's element: the
// glow follows the pointer while it is pressed, and fades where it was
// let go.
func (g Glass) BuildMaterial(e *ui.Element) ui.Material {
	if !g.Interactive {
		return g
	}
	x, y, _ := e.PointerPosition()
	down := e.Pressed()
	at := ui.Local(e, pressKey{}, func() [2]float32 { return [2]float32{} })
	if down {
		*at = [2]float32{x, y}
	}
	target := float32(0)
	if down {
		target = 1
	}
	return pressed{g, e.Animate(pressKey{}, target, 250*time.Millisecond), *at}
}

// PaintMaterial paints the glass over box, rounded by radii, with its
// shadow.
func (g Glass) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	paint(p, box, radii, g, 0, [2]float32{})
}

func (g pressed) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	paint(p, box, radii, g.Glass, g.glow, g.at)
}

// String describes the glass, as the inspector shows it.
func (g Glass) String() string {
	s := "glass"
	if g.Style == Clear {
		s += " clear"
	}
	if g.Tint.A > 0 {
		s += fmt.Sprintf(" tint(#%02x%02x%02x%02x)", g.Tint.R, g.Tint.G, g.Tint.B, g.Tint.A)
	}
	if g.Interactive {
		s += " interactive"
	}
	return s
}

// Paint paints a pane of glass shaped as r rounded by radius, in a
// drawing (Element.Draw), over what the painter painted before it, with
// its shadow.
func Paint(p *ui.Painter, r ui.Rect, radius float32, g Glass) {
	paint(p, r, [4]float32{radius, radius, radius, radius}, g, 0, [2]float32{})
}

// paint paints a pane of glass over box, rounded by radii, with its
// shadow; an interactive one glows by glow (0 to 1) at at, relative to
// box.
//
// Its material follows macOS 27's, measured from NSGlassEffectView: the
// regular one maps black to 54% and white to 100% in light mode, and to
// 15% and 51% in dark mode, the clear one adds an eighth; larger panes
// blur more, up to 10 DIPs; the bezel curves the last 36 DIPs of the
// pane, at most half of it, and bends what shows through by up to 1.6
// times that, mirroring what is inside it, as AppKit's do; light comes
// from above and below.
func paint(p *ui.Painter, box ui.Rect, radii [4]float32, g Glass, glow float32, at [2]float32) {
	if box.W <= 0 || box.H <= 0 {
		return
	}
	s := p.Scale()
	m := min(box.W, box.H)
	bezel := min(36, m/2)
	mat := material{
		bezel:      bezel * s,
		refraction: 1.6 * bezel * s,
		rim:        0.35,
		rimWidth:   s,
		light:      math.Pi / 2,
	}
	shade := ui.RGBA(0, 0, 0, 0.13)
	switch {
	case g.Style == Clear:
		mat.blur = s
		mat.low, mat.high, mat.curve, mat.saturation = 0.125, 1.082, 1, 1
		shade = ui.RGBA(0, 0, 0, 0.07)
	case p.Theme().Dark:
		mat.blur = min(max(m*0.035, 1), 10) * s
		mat.low, mat.high, mat.curve, mat.saturation = 0.15, 0.51, 2, 2.2
		shade = ui.RGBA(0, 0, 0, 0.3)
	default:
		mat.blur = min(max(m*0.035, 1), 10) * s
		mat.low, mat.high, mat.curve, mat.saturation = 0.541, 1, 1.2, 1
	}
	if g.Tint.A > 0 {
		mat.tint = [4]float32{float32(g.Tint.R) / 255, float32(g.Tint.G) / 255, float32(g.Tint.B) / 255, float32(g.Tint.A) / 255 * 0.88}
	}
	if glow > 0 {
		mat.glow = 0.3 * glow
		mat.glowX, mat.glowY = (box.X+at[0])*s, (box.Y+at[1])*s
		mat.glowRadius = max(box.W, box.H) * 0.6 * s
	}
	// The shadow grows with the pane, softly, and below it.
	sh := min(20, m/4)
	p.Shadow(box, max(radii[0], radii[1], radii[2], radii[3]), 0, 0.3*sh, 2*sh, 0, shade)
	p.Effect(Effect, box, radii, mat.blur, mat.params())
}
