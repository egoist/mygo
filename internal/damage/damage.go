// Package damage finds the areas of a window whose drawing changed since
// the last frame, so that a renderer can draw only those: a blinking caret,
// a ticking clock or a button under the pointer redraws only itself.
//
// It compares the operations of two scenes from both ends, so a change in
// the middle of the display list draws the boxes of the operations that
// changed, clipped as they draw. Glyphs whose pixels changed in an atlas,
// and images whose pixels changed, count as changed. An effect that reads
// its backdrop widens the damage to what it reads, and the damage settles
// into rectangles apart from each other.
package damage

import (
	"image"
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// Tracker compares successive scenes and keeps the last one, reusing its
// memory from frame to frame. Its zero value compares the first scene it
// sees with nothing, so that one is drawn whole.
type Tracker struct {
	// valid tells that the last scene, of w×h pixels, is remembered to
	// compare the next with.
	valid   bool
	w, h    int
	clear   scene.Color
	ops     []scene.Op
	glyphs  []scene.Glyph
	effects []scene.EffectOp
	// bounds is where each operation of the last scene drew, and next
	// those of the scene compared last; versions has the versions of its
	// images.
	bounds, next []image.Rectangle
	versions     []uint64
	mask, color  atlasMark
	damage       []image.Rectangle
	clips        []image.Rectangle
}

// atlasMark is the state of an atlas a scene drew from.
type atlasMark struct {
	atlas        *scene.Atlas
	gen, version uint64
}

// Whole starts comparing s with the last scene remembered, and reports
// whether s must be drawn whole: there is none, s changes too much of it,
// or its size or clear color did. Rects then returns the area to draw: one
// rectangle over the whole frame, or what changed.
func (t *Tracker) Whole(s *scene.Scene) bool {
	t.next = t.opBounds(s, t.next[:0])
	if !t.valid || s.Width != t.w || s.Height != t.h || s.Clear != t.clear || !t.diff(s) {
		t.damage = append(t.damage[:0], image.Rect(0, 0, s.Width, s.Height))
		return true
	}
	return false
}

// Rects returns the rectangles Whole found for the scene it saw, apart
// from each other, and empty when nothing changed.
func (t *Tracker) Rects() []image.Rectangle { return t.damage }

// Bounds returns where each operation of the scene Whole saw draws,
// within the clips around it, one per operation.
func (t *Tracker) Bounds() []image.Rectangle { return t.next }

// Invalidate makes the next Whole report that its scene must be drawn
// whole, as after the pixels of the last frame are gone.
func (t *Tracker) Invalidate() { t.valid = false }

// Remember keeps what Whole needs of s to compare the next scene with. It
// is called once the frame is drawn.
func (t *Tracker) Remember(s *scene.Scene) {
	t.valid = true
	t.w, t.h = s.Width, s.Height
	t.clear = s.Clear
	if len(t.ops) > len(s.Ops) {
		clear(t.ops[len(s.Ops):])
	}
	t.ops = append(t.ops[:0], s.Ops...)
	t.glyphs = append(t.glyphs[:0], s.Glyphs...)
	if len(t.effects) > len(s.Effects) {
		clear(t.effects[len(s.Effects):])
	}
	t.effects = append(t.effects[:0], s.Effects...)
	t.bounds, t.next = t.next, t.bounds
	t.versions = t.versions[:0]
	for i := range s.Ops {
		v := uint64(0)
		if img := s.Ops[i].Image; img != nil {
			v = img.Version()
		}
		t.versions = append(t.versions, v)
	}
	t.mask.set(s.MaskAtlas)
	t.color.set(s.ColorAtlas)
}

// changes returns the rectangles of a that changed since the mark, or
// false when everything may have.
func (m *atlasMark) changes(a *scene.Atlas) ([]image.Rectangle, bool) {
	if a == nil || m.atlas == nil {
		return nil, a == m.atlas
	}
	if a != m.atlas {
		return nil, false
	}
	rects, full := a.Changes(m.gen, m.version)
	return rects, !full
}

func (m *atlasMark) set(a *scene.Atlas) {
	m.atlas = a
	if a != nil {
		m.gen, m.version = a.Generation(), a.Version()
	}
}

// diff sets the damage between the last scene and s, and reports false
// when s must be drawn whole.
func (t *Tracker) diff(s *scene.Scene) bool {
	maskRects, ok := t.mask.changes(s.MaskAtlas)
	if !ok {
		return false
	}
	colorRects, ok := t.color.changes(s.ColorAtlas)
	if !ok {
		return false
	}
	same := func(i, j int) bool { return t.same(i, s, j, maskRects, colorRects) }
	n, m := len(t.ops), len(s.Ops)
	pre := 0
	for pre < n && pre < m && same(pre, pre) {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && same(n-1-suf, m-1-suf) {
		suf++
	}
	t.damage = t.damage[:0]
	if n == m {
		for k := pre; k < n-suf; k++ {
			if !same(k, k) {
				t.damage = addRect(t.damage, t.bounds[k])
				t.damage = addRect(t.damage, t.next[k])
			}
		}
	} else {
		for i := pre; i < n-suf; i++ {
			t.damage = addRect(t.damage, t.bounds[i])
		}
		for j := pre; j < m-suf; j++ {
			t.damage = addRect(t.damage, t.next[j])
		}
	}
	t.addBackdrops(s)
	// Past half the window, drawing it whole costs about the same.
	return area(t.damage)*2 < s.Width*s.Height
}

// addBackdrops adds to the damage the effects of s that read their
// backdrops it meets, with those backdrops: draw reads a backdrop after
// drawing the operations before its effect within the area drawn, which
// must hold all of it, and then no other area may draw the effect. So the
// damage becomes rectangles apart from each other.
func (t *Tracker) addBackdrops(s *scene.Scene) {
	if len(s.Effects) == 0 || len(t.damage) == 0 {
		return
	}
	for changed := true; changed; {
		changed = false
		for i := range s.Ops {
			op := &s.Ops[i]
			if op.Kind != scene.OpEffect || int(op.Start) >= len(s.Effects) || t.next[i].Empty() {
				continue
			}
			fx := &s.Effects[op.Start]
			if fx.Effect == nil || !fx.Effect.Backdrop {
				continue
			}
			need := scene.BackdropOf(op.Rect, fx.Blur, s.Width, s.Height).Area.Union(t.next[i])
			for _, d := range t.damage {
				if d.Overlaps(need) && !need.In(d) {
					t.damage = addRect(t.damage, need)
					changed = true
					break
				}
			}
		}
		// Rectangles that overlap become one.
		for i := 0; i < len(t.damage); i++ {
			for j := i + 1; j < len(t.damage); j++ {
				if t.damage[i].Overlaps(t.damage[j]) {
					t.damage[i] = t.damage[i].Union(t.damage[j])
					t.damage = append(t.damage[:j], t.damage[j+1:]...)
					changed = true
					j = i
				}
			}
		}
	}
}

// same reports whether operation i of the last scene draws what operation
// j of s does.
func (t *Tracker) same(i int, s *scene.Scene, j int, maskRects, colorRects []image.Rectangle) bool {
	a, b := t.ops[i], s.Ops[j]
	// Glyphs and effects are compared by what Start and End point at.
	sa, sb := a.Start, b.Start
	ga, gb := t.glyphs[:0], s.Glyphs[:0]
	if a.Kind == scene.OpGlyphs && b.Kind == scene.OpGlyphs {
		ga, gb = t.glyphs[a.Start:a.End], s.Glyphs[b.Start:b.End]
	}
	a.Start, a.End, b.Start, b.End = 0, 0, 0, 0
	// The CPU draws the sRGB colors, not those outside its gamut, which
	// Wide points at in each scene's own table.
	a.Wide, b.Wide = 0, 0
	if a != b {
		return false
	}
	switch b.Kind {
	case scene.OpGlyphs:
		if len(ga) != len(gb) {
			return false
		}
		for k := range gb {
			g := &gb[k]
			if ga[k] != *g {
				x := ga[k]
				x.Wide = g.Wide
				if x != *g {
					return false
				}
			}
			rects := maskRects
			if g.Colored {
				rects = colorRects
			}
			at := image.Rect(int(g.U), int(g.V), int(g.U)+int(g.UW), int(g.V)+int(g.VH))
			for _, c := range rects {
				if c.Overlaps(at) {
					return false
				}
			}
		}
	case scene.OpImage:
		if b.Image != nil && b.Image.Version() != t.versions[i] {
			return false
		}
	case scene.OpEffect:
		if int(sa) >= len(t.effects) || int(sb) >= len(s.Effects) || t.effects[sa] != s.Effects[sb] {
			return false
		}
	}
	return true
}

// addRect adds a rectangle to damage, merged with one it touches.
func addRect(damage []image.Rectangle, b image.Rectangle) []image.Rectangle {
	if b.Empty() {
		return damage
	}
	for i, d := range damage {
		if d.Inset(-8).Overlaps(b) {
			damage[i] = d.Union(b)
			return damage
		}
	}
	damage = append(damage, b)
	if len(damage) > 8 {
		u := damage[0]
		for _, d := range damage[1:] {
			u = u.Union(d)
		}
		damage = append(damage[:0], u)
	}
	return damage
}

// Bounds appends where each operation of s draws, within the clips around
// it, one per operation: for a renderer drawing s whole.
func Bounds(s *scene.Scene, out []image.Rectangle) []image.Rectangle {
	var t Tracker
	return t.opBounds(s, out)
}

// opBounds appends where each operation of s draws, within the clips
// around it.
func (t *Tracker) opBounds(s *scene.Scene, out []image.Rectangle) []image.Rectangle {
	clip := image.Rect(0, 0, s.Width, s.Height)
	t.clips = t.clips[:0]
	for i := range s.Ops {
		op := &s.Ops[i]
		var b image.Rectangle
		switch op.Kind {
		case scene.OpFill, scene.OpImage, scene.OpEffect:
			b = outset(op.Rect, 1)
		case scene.OpShadow:
			b = outset(op.Rect, 1.5*op.Blur+1)
		case scene.OpGlyphs:
			for _, g := range s.Glyphs[op.Start:op.End] {
				b = b.Union(outset(scene.Rect{X: g.X, Y: g.Y, W: g.W, H: g.H}, 1))
			}
		case scene.OpPushClip:
			// A clip that changes changes everything it cuts.
			b = outset(op.Rect, 1)
			t.clips = append(t.clips, clip)
			clip = clip.Intersect(b)
		case scene.OpPopClip:
			if len(t.clips) > 0 {
				clip = t.clips[len(t.clips)-1]
				t.clips = t.clips[:len(t.clips)-1]
			}
		}
		out = append(out, b.Intersect(clip))
	}
	return out
}

func outset(rc scene.Rect, d float32) image.Rectangle {
	return image.Rect(int(math.Floor(float64(rc.X-d))), int(math.Floor(float64(rc.Y-d))),
		int(math.Ceil(float64(rc.X+rc.W+d))), int(math.Ceil(float64(rc.Y+rc.H+d))))
}

func area(rects []image.Rectangle) int {
	n := 0
	for _, d := range rects {
		n += d.Dx() * d.Dy()
	}
	return n
}
