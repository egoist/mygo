package ui_test

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestPainterShadows checks that Painter.ShadowRadii, InsetShadow and
// InsetShadowRadii paint the pixels an element's Shadow and InsetShadow
// paint for the same box, radii and shadows, at whole and fractional
// scales.
func TestPainterShadows(t *testing.T) {
	white, red, blue := ui.RGB(255, 255, 255), ui.RGBA(255, 0, 0, 0.8), ui.RGBA(0, 0, 255, 0.6)
	radii := [4]float32{4, 12, 24, 36}
	render := func(scale float32, box func(b ui.Element)) *image.RGBA {
		return ui.Render(func(c *ui.Context) {
			box(ui.Box(c).Margin(40).Size(120, 90))
		}, 200, 170, scale)
	}
	same := func(name string, a, b *image.RGBA) {
		t.Helper()
		for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
			for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
				if ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y); ca != cb {
					t.Errorf("%s: (%d, %d) = %v, want the element's %v", name, x, y, cb, ca)
					return
				}
			}
		}
	}
	for _, scale := range []float32{1, 1.5, 2} {
		// Corners of their own, an outer shadow under the box and an inner
		// one over it.
		element := render(scale, func(b ui.Element) {
			b.Radius(radii[0], radii[1], radii[2], radii[3]).Background(white).
				Shadow(6, 8, 10, 2, red).InsetShadow(-3, 4, 6, 3, blue)
		})
		painter := render(scale, func(b ui.Element) {
			b.Draw(func(p *ui.Painter, r ui.Rect) {
				p.ShadowRadii(r, radii, 6, 8, 10, 2, red)
				p.FillRadii(r, radii, white)
				p.InsetShadowRadii(r, radii, -3, 4, 6, 3, blue)
			})
		})
		same("ShadowRadii and InsetShadowRadii", element, painter)

		// One radius, an inner shadow alone, spread.
		element = render(scale, func(b ui.Element) {
			b.Radius(16).Background(white).InsetShadow(0, 2, 8, 3, red)
		})
		painter = render(scale, func(b ui.Element) {
			b.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, white, 16)
				p.InsetShadow(r, 16, 0, 2, 8, 3, red)
			})
		})
		same("InsetShadow", element, painter)
	}

	// The corners are each its own: the shadow of the largest radius
	// everywhere, which Shadow alone could paint, is another.
	one := render(1, func(b ui.Element) {
		b.Draw(func(p *ui.Painter, r ui.Rect) { p.Shadow(r, radii[3], 6, 8, 10, 2, red) })
	})
	four := render(1, func(b ui.Element) {
		b.Draw(func(p *ui.Painter, r ui.Rect) { p.ShadowRadii(r, radii, 6, 8, 10, 2, red) })
	})
	// The small top left corner of the box hides what the round one
	// leaves of the shadow.
	if c, d := one.RGBAAt(60, 40), four.RGBAAt(60, 40); c == d {
		t.Errorf("by the top left corner: %v with a radius of %v and with %v alike", c, radii[3], radii)
	}
}
