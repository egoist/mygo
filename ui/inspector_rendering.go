//go:build !mygo_noinspector

package ui

import (
	"fmt"
	"image"
	"math"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

// The toolbar's paint flashing flashes over the content what each frame changed, as
// Chrome's paint flashing does, to find what redraws when nothing should.
// What it shows is what a GPU renderer redraws: the damage of scene.Differ,
// compared before the inspector paints over the scene. Painting whole is
// what the inspector does, which says nothing of the frames without it.

const (
	// inspFlashLife is how long a flash takes to fade.
	inspFlashLife = 400 * time.Millisecond
	// inspFlashes is how many flashes show at once, the latest.
	inspFlashes = 64
)

// inspFlash is what a frame changed, in DIPs of the window.
type inspFlash struct {
	r  Rect
	at time.Time
}

// togglePaintFlashing turns paint flashing on or off.
func (in *inspector) togglePaintFlashing() {
	in.flashing = !in.flashing
	in.flashes, in.differ, in.differKnown = nil, nil, false
	if in.flashing {
		in.differ = &scene.Differ{Reach: 1}
	}
}

// noteRepaints compares the scene as the elements painted it with the last.
func (in *inspector) noteRepaints(rt *engine, p *Painter) {
	if !in.flashing {
		return
	}
	d, s := in.differ, p.s
	first := !in.differKnown
	damage, known := d.Diff(s)
	d.Remember(s)
	in.differKnown = true
	if first {
		// Turning it on changed nothing of the content.
		return
	}
	now := rt.c.now
	if !known {
		damage = []image.Rectangle{image.Rect(0, 0, s.Width, s.Height)}
	}
	for _, r := range damage {
		x0, y0 := float32(r.Min.X)/p.scale, float32(r.Min.Y)/p.scale
		x1, y1 := min(float32(r.Max.X)/p.scale, in.appW), float32(r.Max.Y)/p.scale
		// The panel changes with what it shows, which isn't the content.
		if x1 > x0 && y1 > y0 {
			in.flashes = append(in.flashes, inspFlash{Rect{x0, y0, x1 - x0, y1 - y0}, now})
		}
	}
	if n := len(in.flashes) - inspFlashes; n > 0 {
		in.flashes = append(in.flashes[:0], in.flashes[n:]...)
	}
}

// paintFlashes paints the flashes that haven't faded, and asks for the frames
// that fade them.
func (in *inspector) paintFlashes(rt *engine, p *Painter) {
	now := rt.c.now
	live := in.flashes[:0]
	for _, f := range in.flashes {
		if age := now.Sub(f.at); age < inspFlashLife {
			live = append(live, f)
			fade := 1 - float32(age)/float32(inspFlashLife)
			p.Fill(f.r, RGBA(0, 200, 83, 0.45*fade), 0)
			p.Stroke(f.r, RGBA(0, 150, 60, 0.9*fade), 0, 1)
		}
	}
	in.flashes = live
	if len(live) > 0 {
		p.AnimationFrame()
	}
}

// inspectorDark returns the appearance the window shows given the system's:
// the toolbar's dark appearance overrides it until the inspector closes.
func inspectorDark(rt *engine, dark bool) bool {
	if rt.insp.appearance != 0 {
		return rt.insp.appearance == inspForceDark
	}
	return dark
}

const (
	inspForceLight = iota + 1
	inspForceDark
)

// toggleDark shows the window in the other appearance.
func (in *inspector) toggleDark(rt *engine) {
	in.appearance = inspForceDark
	if rt.dark {
		in.appearance = inspForceLight
	}
	rt.themeChanged()
}

// luminance returns a color's relative luminance, as WCAG defines it.
func luminance(c Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// contrastRatio returns WCAG's contrast ratio of two opaque colors, from 1 to
// 21.
func contrastRatio(a, b Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// blendOver returns c at the opacity given over the opaque color under.
func blendOver(under, c Color, opacity float32) Color {
	a := float32(c.A) / 255 * opacity
	mix := func(u, v uint8) uint8 { return uint8(float32(u)*(1-a) + float32(v)*a + 0.5) }
	return Color{R: mix(under.R, c.R), G: mix(under.G, c.G), B: mix(under.B, c.B), A: 255}
}

// textContrast returns the contrast of an element's text with what is behind
// it: its own background and those around it, over the window's. It doesn't
// know a gradient or an image, for which it reports false, nor what other
// elements overlap it.
func textContrast(rt *engine, e *node) (ratio float64, ok bool) {
	var chain []*node
	for n := e; n != nil; n = n.parent {
		chain = append(chain, n)
	}
	back := rt.theme.Background
	back.A = 255
	opacity := float32(1)
	for i := len(chain) - 1; i >= 0; i-- {
		n := chain[i]
		if n.kind == kindImage || n.fill != fillColor {
			return 0, false
		}
		if n.opacitySet {
			opacity *= n.opacity
		}
		if n.bg.A > 0 {
			back = blendOver(back, n.bg, opacity)
		}
	}
	return contrastRatio(blendOver(back, e.resolvedText().color, opacity), back), true
}

// contrastLevel returns what the ratio passes of WCAG for text of a size and
// weight: 2 for AAA, 1 for AA, 0 for neither. Large text asks for less: 24
// DIPs, or 18.66 in bold.
func contrastLevel(ratio float64, size float32, weight int) int {
	aa, aaa := 4.5, 7.0
	if size >= 24 || size >= 18.66 && weight >= 700 {
		aa, aaa = 3, 4.5
	}
	switch {
	case ratio >= aaa:
		return 2
	case ratio >= aa:
		return 1
	}
	return 0
}

// contrastRow returns the tooltip's row of a text's contrast, as Chrome's:
// a check for AA, two for AAA, and a warning color below them.
func contrastRow(rt *engine, e *node) (tipRow, bool) {
	ratio, ok := textContrast(rt, e)
	if !ok {
		return tipRow{}, false
	}
	ts := e.resolvedText()
	row := tipRow{label: "Contrast"}
	switch contrastLevel(ratio, ts.size, ts.weight) {
	case 2:
		row.value, row.tone = fmt.Sprintf("Aa %.2f ✓✓", ratio), Hex("#188038")
	case 1:
		row.value, row.tone = fmt.Sprintf("Aa %.2f ✓", ratio), Hex("#188038")
	default:
		row.value, row.tone = fmt.Sprintf("Aa %.2f ⊘", ratio), Hex("#c5221f")
	}
	return row, true
}
