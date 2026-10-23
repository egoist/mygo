package terminal

import (
	"image"
	"slices"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// Layers of images (Kitty graphics), as Ghostty draws them, by their z.
const (
	belowBackground = iota // z below -2^30
	belowText              // negative z
	aboveText
)

// shownImage is a placement of an image as the view draws it: the part of
// its bitmap, where it goes from the grid's origin in device pixels, and
// its layer.
type shownImage struct {
	bmp        *ui.Bitmap
	src        ui.Rect
	x, y, w, h int
	z          int32
	layer      int
}

// cachedImage is an image's bitmap, as of its generation, and its size.
type cachedImage struct {
	gen  uint64
	bmp  *ui.Bitmap
	w, h int
	seen bool
}

// readImages reads the placements that show, the virtual placements of
// Unicode placeholders, and the bitmaps of the images they show, which it
// keeps while placements show them: for placeholders, the images those of
// the last frame showed. t.mu is held.
func (v *view) readImages(term *vt.Terminal) {
	v.images, v.virtual = v.images[:0], v.virtual[:0]
	for _, c := range v.bitmaps {
		c.seen = false
	}
	term.Placements(func(pl vt.Placement) {
		if pl.Virtual {
			v.virtual = append(v.virtual, virtualPlacement{image: pl.ImageID, id: pl.ID, cols: pl.ParamCols, rows: pl.ParamRows})
			return
		}
		if !pl.Visible || pl.Width <= 0 || pl.Height <= 0 || pl.SrcW <= 0 || pl.SrcH <= 0 {
			return
		}
		c := v.bitmap(term, pl.ImageID)
		if c == nil {
			return // its pixels have yet to arrive
		}
		layer := aboveText
		switch {
		case pl.Z < -(1 << 30):
			layer = belowBackground
		case pl.Z < 0:
			layer = belowText
		}
		v.images = append(v.images, shownImage{
			bmp: c.bmp,
			src: ui.Rect{X: float32(pl.SrcX), Y: float32(pl.SrcY), W: float32(pl.SrcW), H: float32(pl.SrcH)},
			x:   pl.Col*v.cellW + pl.XOffset, y: pl.Row*v.cellH + pl.YOffset, w: pl.Width, h: pl.Height,
			z: pl.Z, layer: layer,
		})
	})
	for _, id := range v.shown {
		v.bitmap(term, id)
	}
	for id, c := range v.bitmaps {
		if !c.seen {
			delete(v.bitmaps, id)
		}
	}
	slices.SortStableFunc(v.images, func(a, b shownImage) int { return int(a.z) - int(b.z) })
}

// bitmap returns the bitmap of an image, made anew when its pixels
// changed, and keeps it; nil when its pixels have yet to arrive. t.mu is
// held.
func (v *view) bitmap(term *vt.Terminal, id uint32) *cachedImage {
	gen := term.ImageGeneration(id)
	if gen == 0 {
		return nil
	}
	c := v.bitmaps[id]
	if c == nil || c.gen != gen {
		img, ok := term.Image(id)
		if !ok {
			return nil
		}
		if c == nil {
			if v.bitmaps == nil {
				v.bitmaps = map[uint32]*cachedImage{}
			}
			c = &cachedImage{}
			v.bitmaps[id] = c
		}
		c.gen, c.bmp, c.w, c.h = gen, ui.NewBitmap(decoded(img)), img.Width, img.Height
	}
	c.seen = true
	return c
}

// placeholderImages adds the tiles the rows' Unicode placeholders show to
// the images drawn, over the cells' backgrounds and below the text, as
// Ghostty draws them, and reports whether an image they show was not
// read with the frame: the next frame reads it and draws it.
func (v *view) placeholderImages() (more bool) {
	read := v.shown
	v.shown = v.spare[:0]
	added := false
	for y := range v.lines {
		for _, r := range v.lines[y].holders {
			vp, ok := target(v.virtual, r.image, r.id)
			if !ok {
				continue
			}
			if !slices.Contains(v.shown, r.image) {
				v.shown = append(v.shown, r.image)
			}
			if !slices.Contains(read, r.image) {
				// Not read with the frame: its pixels may be new.
				more = true
			}
			c := v.bitmaps[r.image]
			if c == nil {
				continue // its pixels have yet to arrive, or to be read
			}
			tile, ok := placeholderPlacement(r, y, vp.cols, vp.rows, c.w, c.h, v.cellW, v.cellH)
			if !ok {
				continue
			}
			v.images = append(v.images, shownImage{bmp: c.bmp, src: tile.src, x: tile.x, y: tile.y, w: tile.w, h: tile.h, z: -1, layer: belowText})
			added = true
		}
	}
	v.spare = read
	if added {
		slices.SortStableFunc(v.images, func(a, b shownImage) int { return int(a.z) - int(b.z) })
	}
	return more
}

// drawImages draws the images of a layer, within the grid.
func (v *view) drawImages(p *ui.Painter, layer int, ox, oy float32) {
	s := v.scale
	grid := ui.Rect{X: ox, Y: oy, W: float32(v.cols*v.cellW) / s, H: float32(v.rows*v.cellH) / s}
	for _, im := range v.images {
		if im.layer != layer {
			continue
		}
		dst := ui.Rect{X: ox + float32(im.x)/s, Y: oy + float32(im.y)/s, W: float32(im.w) / s, H: float32(im.h) / s}
		p.Clip(grid, 0, func() { p.ImageRegion(im.bmp, im.src, dst, ui.ImageOptions{}) })
	}
}

// decoded returns an image's pixels as an image.
func decoded(img vt.Image) *image.NRGBA {
	w, h := img.Width, img.Height
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	px := img.Pixels
	switch img.Format {
	case vt.ImageRGBA:
		copy(out.Pix, px)
	case vt.ImageRGB:
		for i := 0; i < w*h && 3*i+2 < len(px); i++ {
			copy(out.Pix[4*i:], px[3*i:3*i+3])
			out.Pix[4*i+3] = 0xff
		}
	case vt.ImageGrayAlpha:
		for i := 0; i < w*h && 2*i+1 < len(px); i++ {
			g := px[2*i]
			out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = g, g, g, px[2*i+1]
		}
	case vt.ImageGray:
		for i := 0; i < w*h && i < len(px); i++ {
			g := px[i]
			out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = g, g, g, 0xff
		}
	}
	return out
}
