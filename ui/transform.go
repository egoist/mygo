package ui

import (
	"math"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// Transform is an affine visual transform. Its zero value is identity.
// It changes drawing and interaction, leaving the space reserved by layout
// unchanged. Build one with Translation, Scaling and Rotation and compose
// them with Then. Elements inherit their ancestors' transforms.
type Transform struct{ m scene.Affine }

// Translation moves content by x and y DIPs.
func Translation(x, y float32) Transform {
	finiteTransform(x, y)
	return Transform{scene.Translation(x, y)}
}

// Scaling scales each axis; zero collapses it and negative values reflect it.
func Scaling(x, y float32) Transform {
	finiteTransform(x, y)
	return Transform{scene.Scaling(x, y)}
}

// Rotation rotates clockwise by degrees in the window's coordinate system.
func Rotation(degrees float32) Transform {
	finiteTransform(degrees)
	return Transform{scene.Rotation(degrees)}
}

// Then applies t first, then next.
func (t Transform) Then(next Transform) Transform {
	m := next.m.Mul(t.m)
	finiteTransform(m.A, m.B, m.C, m.D, m.X, m.Y)
	return Transform{m}
}

// Transform sets the element's visual transform. Its origin is the center
// of its border box unless TransformOrigin changes it.
func (e *Element) Transform(t Transform) *Element { e.transform = t; return e }

// Translate moves the element after its existing transform.
func (e *Element) Translate(x, y float32) *Element {
	e.transform = e.transform.Then(Translation(x, y))
	return e
}

// Scale scales the element after its existing transform, around its origin.
func (e *Element) Scale(x, y float32) *Element {
	e.transform = e.transform.Then(Scaling(x, y))
	return e
}

// TransformOrigin sets the origin as fractions of the border box:
// (0,0) is the top-left and (0.5,0.5) the center. Values outside the box
// are allowed. Translation does not depend on the origin.
func (e *Element) TransformOrigin(x, y float32) *Element {
	finiteTransform(x, y)
	e.originX, e.originY, e.originSet = x, y, true
	return e
}

func finiteTransform(v ...float32) {
	for _, n := range v {
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			panic("ui: transform values must be finite")
		}
	}
}

func (e *Element) transformAt(x, y float32) scene.Affine {
	if !e.transform.m.Set {
		return scene.Affine{}
	}
	ox, oy := float32(0.5), float32(0.5)
	if e.originSet {
		ox, oy = e.originX, e.originY
	}
	x, y = x+ox*e.w, y+oy*e.h
	return scene.Translation(x, y).Mul(e.transform.m).Mul(scene.Translation(-x, -y))
}

func (e *Element) visualRect(r Rect) Rect { return transformRect(e.world, r) }
func transformRect(m scene.Affine, r Rect) Rect {
	b := m.Bounds(scene.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	return Rect{b.X, b.Y, b.W, b.H}
}

// LayoutBounds returns the previous frame's border box before transforms,
// in the layout's window coordinates. Draw callbacks use these coordinates.
func (e *Element) LayoutBounds() Rect { s := e.st; return Rect{s.x, s.y, s.w, s.h} }

// LocalToWindow maps a point in the previous frame's border box to the window.
func (e *Element) LocalToWindow(x, y float32) (float32, float32) {
	return e.st.world.Point(e.st.x+x, e.st.y+y)
}

// WindowToLocal maps a window point into the previous frame's border box.
// ok is false for singular transforms, which take no pointer input.
func (e *Element) WindowToLocal(x, y float32) (lx, ly float32, ok bool) {
	m, ok := e.st.world.Inverse()
	if !ok {
		return 0, 0, false
	}
	x, y = m.Point(x, y)
	return x - e.st.x, y - e.st.y, true
}

func (s *state) local(x, y float32) (float32, float32) {
	m, ok := s.world.Inverse()
	if !ok {
		return 0, 0
	}
	x, y = m.Point(x, y)
	return x - s.x, y - s.y
}

func (s *state) vector(x, y float32) (float32, float32) {
	m, ok := s.world.Inverse()
	if !ok {
		return 0, 0
	}
	return m.Vector(x, y)
}

// contains tests the actual transformed box and every ancestor's clip,
// rather than the conservative rectangles used for accessibility and culling.
func (rt *engine) contains(s *state, r Rect, x, y float32) bool {
	m, ok := s.world.Inverse()
	if !ok {
		return false
	}
	lx, ly := m.Point(x, y)
	if !r.Contains(lx, ly) {
		return false
	}
	for p := rt.states[s.parent]; p != nil; p = rt.states[p.parent] {
		if p.clips {
			m, ok := p.world.Inverse()
			if !ok {
				return false
			}
			px, py := m.Point(x, y)
			if !roundedContains(p.clipRect, p.clipRadii, px, py) {
				return false
			}
		}
		if p.parent == 0 {
			break
		}
	}
	w, h, _ := rt.host.size()
	return x >= 0 && y >= 0 && x < w && y < h
}

func roundedContains(r Rect, radii [4]float32, x, y float32) bool {
	if !r.Contains(x, y) {
		return false
	}
	return raster.Contains(scene.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, radii, continuousCorners, x, y)
}

func accessBounds(e *Element) platform.RectF {
	r := e.visualRect(Rect{e.x, e.y, e.w, e.h})
	return platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)}
}

func laidOutVisualBox(e *Element) Rect {
	b := laidOutBox(e)
	if e.st.seen != e.c.rt.frame {
		return e.Bounds()
	}
	var world func(e *Element) scene.Affine
	world = func(e *Element) scene.Affine {
		x, y := laidOutOrigin(e)
		if e.isInline() {
			r := laidOutBox(e)
			x, y = r.X, r.Y
		}
		m := e.transformAt(x, y)
		if e.parent != nil && e != e.c.overlay {
			m = world(e.parent).Mul(m)
		}
		return m
	}
	return transformRect(world(e), b)
}

func (p *Painter) add(op scene.Op) {
	op.Transform = p.transform.Pixels(p.scale).Mul(op.Transform)
	p.s.Ops = append(p.s.Ops, op)
}

func (p *Painter) inlineTransforms() bool {
	if p.inline == nil {
		return false
	}
	var walk func(*Element) bool
	walk = func(e *Element) bool {
		for ch := e.first; ch != nil; ch = ch.next {
			if ch.transform.m.Set || walk(ch) {
				return true
			}
		}
		return false
	}
	return walk(p.inline)
}

func (p *Painter) inlineTransform(runeIndex int) scene.Affine {
	if p.inline == nil {
		return scene.Affine{}
	}
	var find func(e *Element, offset int) *Element
	find = func(e *Element, offset int) *Element {
		for ch := e.first; ch != nil; ch = ch.next {
			if runeIndex >= offset+ch.runes[0] && runeIndex < offset+ch.runes[1] {
				return find(ch, offset+ch.runes[0])
			}
		}
		return e
	}
	e := find(p.inline, 0)
	inv, ok := p.inline.world.Inverse()
	if !ok {
		return scene.Scaling(0, 0)
	}
	return inv.Mul(e.world)
}

// Inline glyphs can move into view even when their paragraph's layout box
// is out of view. Their ancestors' clips still apply when they paint.
func (p *Painter) inlineVisible(e *Element) bool {
	for ch := e.first; ch != nil; ch = ch.next {
		for _, f := range ch.frags {
			if v := intersect(ch.visualRect(f), p.clip); v.W > 0 && v.H > 0 {
				return true
			}
		}
		if p.inlineVisible(ch) {
			return true
		}
	}
	return false
}
