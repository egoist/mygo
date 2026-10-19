package vt

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"sync"
	"unsafe"
)

// Images of the Kitty graphics protocol, which the library keeps once a
// terminal allows them a storage limit: decoded, with their placements on
// the screen.

const (
	dataKittyGraphics = 30 // GHOSTTY_TERMINAL_DATA_KITTY_GRAPHICS
	sysOptDecodePNG   = 1  // GHOSTTY_SYS_OPT_DECODE_PNG

	kittyDataPlacementIterator = 1
	kittyDataGeneration        = 2

	placementDataImageID      = 1
	placementDataPlacementID  = 2
	placementDataIsVirtual    = 3
	placementDataXOffset      = 4
	placementDataYOffset      = 5
	placementDataSourceX      = 6
	placementDataSourceY      = 7
	placementDataSourceWidth  = 8
	placementDataSourceHeight = 9
	placementDataColumns      = 10
	placementDataRows         = 11
	placementDataZ            = 12

	pointTagActive = 0 // GHOSTTY_POINT_TAG_ACTIVE
	pointTagScreen = 2 // GHOSTTY_POINT_TAG_SCREEN

	imageDataWidth      = 3
	imageDataHeight     = 4
	imageDataFormat     = 5
	imageDataDataPtr    = 7
	imageDataDataLen    = 8
	imageDataGeneration = 9
)

// ImageFormat is how an image's pixels are laid out: 8 bits a channel,
// alpha not premultiplied.
type ImageFormat int32

const (
	ImageRGB       ImageFormat = 0
	ImageRGBA      ImageFormat = 1
	ImageGrayAlpha ImageFormat = 3
	ImageGray      ImageFormat = 4
)

// Image is an image a program sent. Pix is the library's: valid until the
// terminal next changes.
type Image struct {
	// Generation changes whenever the image's pixels may have, as when it
	// is sent again or an animation moves on to its next frame.
	Generation    uint64
	Width, Height int
	Format        ImageFormat
	Pix           []byte
}

// Placement is where an image shows on the screen.
type Placement struct {
	Image uint32
	// Z orders placements: below the cells' backgrounds when less than
	// math.MinInt32/2, between them and the text when negative, over the
	// text otherwise.
	Z int32
	// Col and Row are the cell of its top left corner in the viewport,
	// negative when it scrolled partly out of it, and X and Y its offset
	// in that cell; Width and Height are its size on the screen. All are
	// in the pixels of the cell size Resize gave.
	Col, Row      int
	X, Y          int
	Width, Height int
	// Source is the part of the image it shows, in the image's pixels.
	Source image.Rectangle
}

// placementRenderInfo is a GhosttyKittyGraphicsPlacementRenderInfo.
type placementRenderInfo struct {
	size                      uintptr
	pixelWidth, pixelHeight   uint32
	gridCols, gridRows        uint32
	viewportCol, viewportRow  int32
	viewportVisible           bool
	sourceX, sourceY          uint32
	sourceWidth, sourceHeight uint32
}

// sysImage is a GhosttySysImage: a decoded image the library takes.
type sysImage struct {
	width, height uint32
	data          uintptr
	dataLen       uintptr
}

// maxDecodedPNG bounds the pixels a PNG may decode to, so that a small
// file claiming a huge size allocates nothing.
const maxDecodedPNG = 256 << 20

var decoder struct {
	once sync.Once
	cb   uintptr
}

// installPNGDecoder gives the library a PNG decoder, once for the
// process: the Kitty graphics protocol sends PNGs, which the library
// decodes as an image arrives.
func installPNGDecoder() {
	decoder.once.Do(func() {
		decoder.cb = newCallback(func(_, alloc, data, n, out uintptr) uintptr {
			if decodePNG(alloc, unsafe.Slice((*byte)(mem(data)), n), (*sysImage)(mem(out))) {
				return 1
			}
			return 0
		})
		call(fnSysSet, sysOptDecodePNG, decoder.cb)
	})
}

// decodePNG decodes a PNG to straight RGBA in memory of the library's
// allocator, which the library frees.
func decodePNG(alloc uintptr, data []byte, out *sysImage) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxDecodedPNG/4 {
		return false
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	b := img.Bounds()
	n := b.Dx() * b.Dy() * 4
	p := call(fnAlloc, alloc, uintptr(n))
	if p == 0 {
		return false
	}
	dst := &image.NRGBA{Pix: unsafe.Slice((*byte)(mem(p)), n), Stride: b.Dx() * 4, Rect: image.Rect(0, 0, b.Dx(), b.Dy())}
	draw.Draw(dst, dst.Rect, img, b.Min, draw.Src)
	*out = sysImage{width: uint32(b.Dx()), height: uint32(b.Dy()), data: p, dataLen: uintptr(n)}
	return true
}

// SetImageStorageLimit lets the terminal keep up to that many bytes of
// the decoded pixels of images of the Kitty graphics protocol, apart from
// the scrollback; zero, the default, keeps none, so that images are
// refused and programs fall back to something else.
func (t *Terminal) SetImageStorageLimit(bytes uint64) error {
	if bytes > 0 {
		installPNGDecoder()
	}
	return t.set(optKittyImageLimit, unsafe.Pointer(&bytes))
}

// graphics returns the image storage of the active screen, or 0.
func (t *Terminal) graphics() uintptr {
	var g uintptr
	if t.get(dataKittyGraphics, unsafe.Pointer(&g)) != nil {
		return 0
	}
	return g
}

// ImagesGeneration changes whenever an image is sent, placed or deleted,
// and is 0 while the terminal has none.
func (t *Terminal) ImagesGeneration() uint64 {
	g := t.graphics()
	if g == 0 {
		return 0
	}
	var gen uint64
	if !ok(call(fnKittyGraphicsGet, g, kittyDataGeneration, uintptr(unsafe.Pointer(&gen)))) {
		return 0
	}
	return gen
}

// Placements appends to dst the placements that show in the viewport, in
// the order the library keeps them. Placements of Unicode placeholders,
// which cells draw, are left out: VirtualPlacements reads those.
func (t *Terminal) Placements(dst []Placement) []Placement {
	g, it := t.placementIterator()
	if it == 0 {
		return dst
	}
	for cbool(call(fnKittyPlacementNext, it)) {
		var id uint32
		if !ok(call(fnKittyPlacementGet, it, placementDataImageID, uintptr(unsafe.Pointer(&id)))) {
			continue
		}
		img := call(fnKittyGraphicsImage, g, uintptr(id))
		if img == 0 {
			continue
		}
		info := placementRenderInfo{size: unsafe.Sizeof(placementRenderInfo{})}
		if !ok(call(fnKittyPlacementRenderInfo, it, img, t.h, uintptr(unsafe.Pointer(&info)))) || !info.viewportVisible {
			continue
		}
		var z int32
		var x, y uint32
		call(fnKittyPlacementGet, it, placementDataZ, uintptr(unsafe.Pointer(&z)))
		call(fnKittyPlacementGet, it, placementDataXOffset, uintptr(unsafe.Pointer(&x)))
		call(fnKittyPlacementGet, it, placementDataYOffset, uintptr(unsafe.Pointer(&y)))
		dst = append(dst, Placement{
			Image: id, Z: z,
			Col: int(info.viewportCol), Row: int(info.viewportRow), X: int(x), Y: int(y),
			Width: int(info.pixelWidth), Height: int(info.pixelHeight),
			Source: image.Rect(int(info.sourceX), int(info.sourceY), int(info.sourceX+info.sourceWidth), int(info.sourceY+info.sourceHeight)),
		})
	}
	return dst
}

// VirtualPlacement is a placement of an image that Unicode placeholders
// show: cells of U+10EEEE, which say the image and the tile of the
// placement's grid each shows.
type VirtualPlacement struct {
	Image, ID uint32
	// Cols and Rows are the grid the image fits in, 0 for the cells the
	// image covers at its size.
	Cols, Rows int
}

// VirtualPlacements appends to dst the placements of Unicode
// placeholders, of every image.
func (t *Terminal) VirtualPlacements(dst []VirtualPlacement) []VirtualPlacement {
	_, it := t.placementIterator()
	if it == 0 {
		return dst
	}
	for cbool(call(fnKittyPlacementNext, it)) {
		var virtual bool
		if !ok(call(fnKittyPlacementGet, it, placementDataIsVirtual, uintptr(unsafe.Pointer(&virtual)))) || !virtual {
			continue
		}
		var img, id, cols, rows uint32
		if !ok(call(fnKittyPlacementGet, it, placementDataImageID, uintptr(unsafe.Pointer(&img)))) ||
			!ok(call(fnKittyPlacementGet, it, placementDataPlacementID, uintptr(unsafe.Pointer(&id)))) {
			continue
		}
		call(fnKittyPlacementGet, it, placementDataColumns, uintptr(unsafe.Pointer(&cols)))
		call(fnKittyPlacementGet, it, placementDataRows, uintptr(unsafe.Pointer(&rows)))
		dst = append(dst, VirtualPlacement{Image: img, ID: id, Cols: int(cols), Rows: int(rows)})
	}
	return dst
}

// placementIterator returns the image storage of the active screen and
// the terminal's placement iterator over it, or 0s.
func (t *Terminal) placementIterator() (g, it uintptr) {
	g = t.graphics()
	if g == 0 {
		return 0, 0
	}
	if t.placements == 0 && !ok(call(fnKittyPlacementIteratorNew, 0, uintptr(unsafe.Pointer(&t.placements)))) {
		t.placements = 0
		return 0, 0
	}
	it = t.placements
	if !ok(call(fnKittyGraphicsGet, g, kittyDataPlacementIterator, uintptr(unsafe.Pointer(&it)))) {
		return 0, 0
	}
	return g, it
}

// Image returns the image of an ID, and false when there is none or its
// pixels are still coming.
func (t *Terminal) Image(id uint32) (Image, bool) {
	g := t.graphics()
	if g == 0 {
		return Image{}, false
	}
	img := call(fnKittyGraphicsImage, g, uintptr(id))
	if img == 0 {
		return Image{}, false
	}
	var (
		w, h   uint32
		format int32
		gen    uint64
		p, n   uintptr
	)
	get := func(data int, out unsafe.Pointer) bool {
		return ok(call(fnKittyImageGet, img, uintptr(data), uintptr(out)))
	}
	if !get(imageDataWidth, unsafe.Pointer(&w)) || !get(imageDataHeight, unsafe.Pointer(&h)) ||
		!get(imageDataFormat, unsafe.Pointer(&format)) || !get(imageDataGeneration, unsafe.Pointer(&gen)) ||
		!get(imageDataDataPtr, unsafe.Pointer(&p)) || !get(imageDataDataLen, unsafe.Pointer(&n)) || p == 0 {
		return Image{}, false
	}
	return Image{Generation: gen, Width: int(w), Height: int(h), Format: ImageFormat(format), Pix: unsafe.Slice((*byte)(mem(p)), n)}, true
}
