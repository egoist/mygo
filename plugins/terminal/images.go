package terminal

import (
	"bytes"
	"image"
	"math"
	"slices"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// Images of the Kitty graphics protocol, which a view draws when
// Options.ImageStorageBytes lets the terminal keep them: each frame reads
// where they are placed, under the terminal's lock, with the pixels of the
// images that changed, then makes bitmaps of those, which the GPU keeps as
// textures until an image changes again.

// The layers placements draw in, by their z: below the cells'
// backgrounds, between them and the text, and over the text.
const (
	imagesBelowBackground = iota
	imagesBelowText
	imagesAboveText
)

func imageLayer(z int32) int {
	switch {
	case z < math.MinInt32/2:
		return imagesBelowBackground
	case z < 0:
		return imagesBelowText
	}
	return imagesAboveText
}

// viewImages is what a view keeps of the images it draws.
type viewImages struct {
	// places are the placements in the viewport, by z: the images' own,
	// then, after placeholders, those of Unicode placeholders.
	places []vt.Placement
	// virtual are the placements of Unicode placeholders, and known the
	// images they place, for placeholders.
	virtual []vt.VirtualPlacement
	known   []knownImage
	// wanted are the images the placeholders of the last frame show,
	// whose pixels the next snapshot copies.
	wanted []uint32
	// bitmaps are the images places show, by ID.
	bitmaps map[uint32]*viewImage
	// copied are images whose pixels changed, copied under the
	// terminal's lock for update to make bitmaps of.
	copied []vt.Image
	ids    []uint32
}

type viewImage struct {
	gen uint64
	bm  *ui.Bitmap
}

type knownImage struct {
	id            uint32
	gen           uint64
	width, height int
}

func newViewImages() *viewImages {
	return &viewImages{bitmaps: map[uint32]*viewImage{}}
}

// snapshot reads the placements of the viewport and the virtual ones,
// and copies the pixels of the images that changed, which are the
// library's only until the terminal changes. t.mu is held.
func (vi *viewImages) snapshot(term *vt.Terminal) {
	vi.places, vi.copied, vi.ids = vi.places[:0], vi.copied[:0], vi.ids[:0]
	vi.virtual, vi.known = vi.virtual[:0], vi.known[:0]
	if term.ImagesGeneration() == 0 {
		return
	}
	vi.places = term.Placements(vi.places)
	for _, p := range vi.places {
		if img, ok := term.Image(p.Image); ok { // else its pixels are still coming
			vi.copy(p.Image, img)
		}
	}
	vi.virtual = term.VirtualPlacements(vi.virtual)
	for _, p := range vi.virtual {
		if slices.ContainsFunc(vi.known, func(k knownImage) bool { return k.id == p.Image }) {
			continue
		}
		img, ok := term.Image(p.Image)
		if !ok {
			continue
		}
		vi.known = append(vi.known, knownImage{p.Image, img.Generation, img.Width, img.Height})
		if slices.Contains(vi.wanted, p.Image) {
			vi.copy(p.Image, img)
		}
	}
}

// copy copies the pixels of an image, unless they are copied or its
// bitmap has them.
func (vi *viewImages) copy(id uint32, img vt.Image) {
	if slices.Contains(vi.ids, id) {
		return
	}
	if have := vi.bitmaps[id]; have != nil && have.gen == img.Generation {
		return
	}
	img.Pix = bytes.Clone(img.Pix)
	vi.copied = append(vi.copied, img)
	vi.ids = append(vi.ids, id)
}

// placeholders adds the placements of the rows' Unicode placeholders to
// places, for cells of cellW×cellH pixels, and reports whether an image
// they show has no bitmap of its pixels yet, which the next snapshot
// copies.
func (vi *viewImages) placeholders(lines []rowCache, cellW, cellH int) (more bool) {
	vi.wanted = vi.wanted[:0]
	for y := range lines {
		for _, r := range lines[y].holders {
			vp, ok := vi.target(r.image, r.id)
			if !ok {
				continue
			}
			i := slices.IndexFunc(vi.known, func(k knownImage) bool { return k.id == r.image })
			if i < 0 {
				continue
			}
			k := vi.known[i]
			p, ok := placeholderPlacement(r, y, vp.Cols, vp.Rows, k.width, k.height, cellW, cellH)
			if !ok {
				continue
			}
			vi.places = append(vi.places, p)
			if !slices.Contains(vi.wanted, r.image) {
				vi.wanted = append(vi.wanted, r.image)
				if e := vi.bitmaps[r.image]; (e == nil || e.gen != k.gen) && !slices.Contains(vi.ids, r.image) {
					more = true
				}
			}
		}
	}
	return more
}

// target is the virtual placement a placeholder of an image and a
// placement ID shows: the one of that ID, or with none, the image's of
// the lowest ID. (Ghostty prefers a placement given an ID to one the
// library numbered, which the library does not tell apart.)
func (vi *viewImages) target(img, id uint32) (vt.VirtualPlacement, bool) {
	var best vt.VirtualPlacement
	found := false
	for _, p := range vi.virtual {
		if p.Image != img || (id != 0 && p.ID != id) {
			continue
		}
		if !found || p.ID < best.ID {
			best, found = p, true
		}
	}
	return best, found
}

// update sorts places by z, makes bitmaps of the images copied, in place
// of the ones of the same size, and drops those no placement shows. Main
// thread.
func (vi *viewImages) update() {
	slices.SortStableFunc(vi.places, func(a, b vt.Placement) int {
		switch {
		case a.Z < b.Z:
			return -1
		case a.Z > b.Z:
			return 1
		}
		return 0
	})
	for i, img := range vi.copied {
		rgba := premultiplied(img)
		e := vi.bitmaps[vi.ids[i]]
		if e == nil {
			e = &viewImage{}
			vi.bitmaps[vi.ids[i]] = e
		}
		if w, h := e.size(); e.bm != nil && w == img.Width && h == img.Height {
			e.bm.Update(rgba)
		} else {
			e.bm = ui.NewBitmap(rgba)
		}
		e.gen = img.Generation
		vi.copied[i].Pix = nil
	}
	for id := range vi.bitmaps {
		if !slices.ContainsFunc(vi.places, func(p vt.Placement) bool { return p.Image == id }) {
			delete(vi.bitmaps, id)
		}
	}
}

func (e *viewImage) size() (int, int) {
	if e.bm == nil {
		return 0, 0
	}
	return e.bm.Size()
}

// premultiplied converts an image's pixels to the premultiplied RGBA of a
// bitmap.
func premultiplied(img vt.Image) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, img.Width, img.Height))
	src, dst := img.Pix, out.Pix
	n := img.Width * img.Height
	switch img.Format {
	case vt.ImageRGBA:
		for i := range min(n, len(src)/4) {
			r, g, b, a := uint32(src[i*4]), uint32(src[i*4+1]), uint32(src[i*4+2]), uint32(src[i*4+3])
			dst[i*4], dst[i*4+1], dst[i*4+2], dst[i*4+3] = byte(r*a/255), byte(g*a/255), byte(b*a/255), byte(a)
		}
	case vt.ImageRGB:
		for i := range min(n, len(src)/3) {
			dst[i*4], dst[i*4+1], dst[i*4+2], dst[i*4+3] = src[i*3], src[i*3+1], src[i*3+2], 255
		}
	case vt.ImageGrayAlpha:
		for i := range min(n, len(src)/2) {
			v, a := uint32(src[i*2]), uint32(src[i*2+1])
			p := byte(v * a / 255)
			dst[i*4], dst[i*4+1], dst[i*4+2], dst[i*4+3] = p, p, p, byte(a)
		}
	case vt.ImageGray:
		for i := range min(n, len(src)) {
			dst[i*4], dst[i*4+1], dst[i*4+2], dst[i*4+3] = src[i], src[i], src[i], 255
		}
	}
	return out
}

// drawImages draws the placements of a layer, over the grid at ox, oy
// (DIPs), clipped to it.
func (v *view) drawImages(p *ui.Painter, layer int, ox, oy float32) {
	vi := v.images
	if vi == nil || len(vi.places) == 0 {
		return
	}
	s := v.scale
	grid := ui.Rect{X: ox, Y: oy, W: float32(v.cols*v.cellW) / s, H: float32(v.rows*v.cellH) / s}
	p.Clip(grid, 0, func() {
		for _, pl := range vi.places {
			e := vi.bitmaps[pl.Image]
			if imageLayer(pl.Z) != layer || e == nil || e.bm == nil {
				continue
			}
			src := ui.Rect{X: float32(pl.Source.Min.X), Y: float32(pl.Source.Min.Y), W: float32(pl.Source.Dx()), H: float32(pl.Source.Dy())}
			dst := ui.Rect{
				X: ox + float32(pl.Col*v.cellW+pl.X)/s, Y: oy + float32(pl.Row*v.cellH+pl.Y)/s,
				W: float32(pl.Width) / s, H: float32(pl.Height) / s,
			}
			p.ImageRegion(e.bm, src, dst, ui.ImageOptions{})
		}
	})
}
