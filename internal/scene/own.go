package scene

import (
	"cmp"
	"image"
	"slices"
)

// Owned are the atlases of a scene that keeps the bitmaps of its glyphs
// (Scene.Own), and what making them again reuses.
type Owned struct {
	// Mask and Color are the scene's atlases, each nil until the scene
	// draws a glyph from one.
	Mask, Color *Atlas

	at    map[ownedRect]image.Point
	rects [2][]ownedRect
}

// ownedRect is a rectangle of an atlas a glyph shows: of the color atlas,
// or of the mask atlas.
type ownedRect struct {
	color      bool
	u, v, w, h uint16
}

// Own has s draw its glyphs from atlases of its own, o's, holding the
// bitmaps they show and no other, copied from the atlases it was painted
// with. Those are every window's: what a frame does not draw moves or
// goes as one makes room in them (text.System.MakeRoom), and the masks a
// frame drew for itself alone go as the next begins. A scene drawn after
// the frame that painted it, as a canvas's by a renderer that has not its
// texture, then draws what it was painted with.
//
// An atlas none of the glyphs shows stays the scene's as it is. It reports
// false, leaving s as it is, when the bitmaps of an atlas do not fit one
// of the largest size, or when a glyph shows what its atlas does not hold.
func (s *Scene) Own(o *Owned) bool {
	if o.at == nil {
		o.at = map[ownedRect]image.Point{}
	}
	clear(o.at)
	src := [2]*Atlas{s.MaskAtlas, s.ColorAtlas}
	rects := [2][]ownedRect{o.rects[0][:0], o.rects[1][:0]}
	for i := range s.Glyphs {
		k, n := ownedOf(&s.Glyphs[i])
		if k.w == 0 || k.h == 0 {
			continue
		}
		if _, ok := o.at[k]; ok {
			continue
		}
		if a := src[n]; a == nil || int(k.u)+int(k.w) > a.W || int(k.v)+int(k.h) > a.H {
			return false
		}
		o.at[k] = image.Point{}
		rects[n] = append(rects[n], k)
	}
	o.rects = rects
	var sizes [2]image.Point
	for n, rs := range rects {
		// Tallest first, as Repack: shelves waste the least.
		slices.SortFunc(rs, func(a, b ownedRect) int {
			return cmp.Or(cmp.Compare(b.h, a.h), cmp.Compare(b.w, a.w), cmp.Compare(a.v, b.v), cmp.Compare(a.u, b.u))
		})
		var ok bool
		if sizes[n], ok = ownedSize(rs); !ok {
			return false
		}
	}
	own := [2]**Atlas{&o.Mask, &o.Color}
	for n, rs := range rects {
		if len(rs) == 0 {
			continue
		}
		from, a := src[n], *own[n]
		if a == nil || a.BPP != from.BPP || a.W != sizes[n].X || a.H != sizes[n].Y {
			a = NewAtlas(from.BPP, sizes[n].X, sizes[n].Y)
			*own[n] = a
		} else {
			a.Reset()
		}
		for _, k := range rs {
			x, y, _ := a.Alloc(int(k.w), int(k.h))
			a.Put(x, y, int(k.w), int(k.h), from.Pix[(int(k.v)*from.W+int(k.u))*from.BPP:], from.W*from.BPP)
			o.at[k] = image.Pt(x, y)
		}
	}
	for i := range s.Glyphs {
		g := &s.Glyphs[i]
		k, _ := ownedOf(g)
		if at, ok := o.at[k]; ok {
			g.U, g.V = uint16(at.X), uint16(at.Y)
		}
	}
	if len(rects[0]) > 0 {
		s.MaskAtlas = o.Mask
	}
	if len(rects[1]) > 0 {
		s.ColorAtlas = o.Color
	}
	return true
}

// ownedOf returns the rectangle a glyph shows, and the index of its atlas:
// 0 for the mask atlas, 1 for the color atlas.
func ownedOf(g *Glyph) (ownedRect, int) {
	k := ownedRect{color: g.Colored || g.Subpixel, u: g.U, v: g.V, w: g.UW, h: g.VH}
	if k.color {
		return k, 1
	}
	return k, 0
}

// ownedSize returns the size of the smallest atlas, of a width that is a
// power of two, about as wide as high, that rects fit allocated in their
// order, or false when none does.
func ownedSize(rects []ownedRect) (image.Point, bool) {
	if len(rects) == 0 {
		return image.Point{}, true
	}
	area, widest := 0, 0
	for _, k := range rects {
		// As Alloc pads them.
		area += (int(k.w) + 1) * (int(k.h) + 1)
		widest = max(widest, int(k.w)+1)
	}
	w := 16
	for w < widest || w*w < area {
		w *= 2
	}
	for ; w <= MaxAtlasSize; w *= 2 {
		plan := Atlas{W: w, H: MaxAtlasSize, top: MaxAtlasSize}
		fits := true
		for _, k := range rects {
			if _, _, ok := plan.Alloc(int(k.w), int(k.h)); !ok {
				fits = false
				break
			}
		}
		if fits {
			// Only as high as its shelves: a wide mask alone takes its
			// own size, not a square of its width.
			return image.Pt(w, plan.bottom), true
		}
	}
	return image.Point{}, false
}
