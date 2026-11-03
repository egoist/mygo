//go:build darwin

package sfsymbols

import (
	"image"
	"math"
	"runtime"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/ui"
)

var (
	selSymbolImage = objc.RegisterName("imageWithSystemSymbolName:accessibilityDescription:")
	selSymbolConfg = objc.RegisterName("configurationWithPointSize:weight:")
	selSymbolApply = objc.RegisterName("imageWithSymbolConfiguration:")
	selCGImage     = objc.RegisterName("CGImageForProposedRect:context:hints:")
	selAlloc       = objc.RegisterName("alloc")
	selInit        = objc.RegisterName("init")
	selDrain       = objc.RegisterName("drain")
	selInitBytes   = objc.RegisterName("initWithBytes:length:encoding:")
	selRelease     = objc.RegisterName("release")
)

// cgRect is CGRect, which CGContextDrawImage takes by value.
type cgRect struct {
	_          structs.HostLayout
	X, Y, W, H float64
}

// cg holds the CoreGraphics functions the rasterizer calls, once resolved.
var cg struct {
	once         sync.Once
	ok           bool
	space        func() uintptr
	releaseSpace func(space uintptr)
	bitmap       func(data unsafe.Pointer, width, height, bitsPerComponent, bytesPerRow, space uintptr, info uint32) uintptr
	releaseCtx   func(ctx uintptr)
	draw         func(ctx uintptr, rect cgRect, img uintptr)
	width        func(img uintptr) uintptr
	height       func(img uintptr) uintptr
}

// cgPremultipliedLast and cgByteOrder32Big name the pixel format of the
// canvases: 8 bits of red, green, blue and alpha each, in that order.
const (
	cgPremultipliedLast = 1
	cgByteOrder32Big    = 4 << 12
)

const nsUTF8StringEncoding = 4

func loadCG() bool {
	cg.once.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return
		}
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&cg.space, lib, "CGColorSpaceCreateDeviceRGB")
		purego.RegisterLibFunc(&cg.releaseSpace, lib, "CGColorSpaceRelease")
		purego.RegisterLibFunc(&cg.bitmap, lib, "CGBitmapContextCreate")
		purego.RegisterLibFunc(&cg.releaseCtx, lib, "CGContextRelease")
		purego.RegisterLibFunc(&cg.draw, lib, "CGContextDrawImage")
		purego.RegisterLibFunc(&cg.width, lib, "CGImageGetWidth")
		purego.RegisterLibFunc(&cg.height, lib, "CGImageGetHeight")
		cg.ok = true
	})
	return cg.ok
}

// Available reports whether this system draws SF Symbols at all: whether
// AppKit and CoreGraphics load here. A particular symbol may still be
// missing from an older macOS, and Image says so for it.
func Available() bool { return loadCG() }

// key names a drawn symbol: its name, its pixels square, its color and its
// options.
type key struct {
	name       string
	px         int
	r, g, b, a uint8
	natural    bool
}

var (
	mu    sync.Mutex
	cache = map[key]*ui.Bitmap{}
)

// Image returns the SF Symbol named name (as "gearshape"), drawn px pixels
// square in col, or false where the system has no such symbol: on another
// OS, or a macOS without it. px is limited to 8 through 256. The result is
// cached, and Image is safe to call from any goroutine.
func Image(name string, px int, col ui.Color, o Options) (ui.ImageSource, bool) {
	if !loadCG() {
		return nil, false
	}
	px = min(max(px, 8), 256)
	k := key{name: name, px: px, r: col.R, g: col.G, b: col.B, a: col.A, natural: o.Natural}
	mu.Lock()
	b, ok := cache[k]
	mu.Unlock()
	if ok {
		return b, true
	}
	coverage, ok := rasterize(name, px, o.Natural)
	if !ok {
		return nil, false
	}
	b = ui.NewBitmap(tint(coverage, col))
	mu.Lock()
	cache[k] = b
	mu.Unlock()
	return b, true
}

// canvas makes a bitmap context over pix, w by h pixels, and returns it
// with the function that releases it.
func canvas(pix []byte, w, h int) (uintptr, func()) {
	space := cg.space()
	ctx := cg.bitmap(unsafe.Pointer(&pix[0]), uintptr(w), uintptr(h), 8, uintptr(w*4), space, cgPremultipliedLast|cgByteOrder32Big)
	cg.releaseSpace(space)
	if ctx == 0 {
		return 0, func() {}
	}
	return ctx, func() {
		cg.releaseCtx(ctx)
		runtime.KeepAlive(pix)
	}
}

// rasterize draws the symbol's outline into a px×px canvas, centered and
// inset a little. With natural, every symbol takes one scale, as AppKit
// draws them at one point size: a cross stays smaller than a wide
// rectangle, rather than each filling the box.
func rasterize(name string, px int, natural bool) (out *image.RGBA, ok bool) {
	// AppKit's temporary objects are autoreleased: drain them here, on the
	// thread that made them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.Send[objc.ID](objc.Send[objc.ID](objc.ID(objc.GetClass("NSAutoreleasePool")), selAlloc), selInit)
	defer objc.Send[objc.ID](pool, selDrain)

	str := nsString(name)
	defer objc.Send[objc.ID](str, selRelease)
	img := objc.Send[objc.ID](objc.ID(objc.GetClass("NSImage")), selSymbolImage, str, objc.ID(0))
	if img == 0 {
		return nil, false
	}
	// A point size of px pixels renders at least 2× that on a Retina
	// screen: the canvas below scales it down, never up.
	cfg := objc.Send[objc.ID](objc.ID(objc.GetClass("NSImageSymbolConfiguration")), selSymbolConfg, float64(px), 0.0)
	img = objc.Send[objc.ID](img, selSymbolApply, cfg)
	if img == 0 {
		return nil, false
	}
	cgImage := objc.Send[objc.ID](img, selCGImage, uintptr(0), uintptr(0), uintptr(0))
	if cgImage == 0 {
		return nil, false
	}
	w, h := int(cg.width(uintptr(cgImage))), int(cg.height(uintptr(cgImage)))
	if w <= 0 || h <= 0 {
		return nil, false
	}

	// The glyph inside its alignment margins, in a canvas of its own size.
	src := make([]byte, w*h*4)
	ctx, done := canvas(src, w, h)
	if ctx == 0 {
		return nil, false
	}
	cg.draw(ctx, cgRect{W: float64(w), H: float64(h)}, uintptr(cgImage))
	done()
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := range h {
		for x := range w {
			if src[y*w*4+x*4+3] != 0 {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return nil, false
	}
	gw, gh := maxX-minX+1, maxY-minY+1

	// The glyph, scaled into the box and centered.
	dst := make([]byte, px*px*4)
	dctx, ddone := canvas(dst, px, px)
	if dctx == 0 {
		return nil, false
	}
	const inset = 0.85
	scale := math.Min(inset*float64(px)/float64(gw), inset*float64(px)/float64(gh))
	if natural {
		// rectangle.on.rectangle, of the widest, spans 2.43 times the point
		// size in the image AppKit draws: it, and so every other, fits.
		scale = math.Min(scale, 0.92/2.43)
	}
	dw, dh := float64(gw)*scale, float64(gh)*scale
	// The image holds the glyph inside alignment margins: scale the whole
	// image so that the glyph, and not the margins, fills the box.
	cg.draw(dctx, cgRect{
		X: (float64(px)-dw)/2 - float64(minX)*scale,
		Y: (float64(px)-dh)/2 - float64(minY)*scale,
		W: float64(w) * scale,
		H: float64(h) * scale,
	}, uintptr(cgImage))
	ddone()

	out = image.NewRGBA(image.Rect(0, 0, px, px))
	for i := range px * px {
		a := dst[i*4+3]
		out.Pix[i*4+0], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = a, a, a, a
	}
	return out, true
}

// tint colors coverage, whose alpha holds the shape, in col.
func tint(coverage *image.RGBA, col ui.Color) *image.RGBA {
	out := image.NewRGBA(coverage.Rect)
	for i := range len(coverage.Pix) / 4 {
		a := uint16(coverage.Pix[i*4+3]) * uint16(col.A) / 255
		out.Pix[i*4+0] = uint8(uint16(col.R) * a / 255)
		out.Pix[i*4+1] = uint8(uint16(col.G) * a / 255)
		out.Pix[i*4+2] = uint8(uint16(col.B) * a / 255)
		out.Pix[i*4+3] = uint8(a)
	}
	return out
}

// nsString makes an owned NSString for s, which the caller releases. It
// copies the bytes, which stay alive until the message returns.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	str := objc.Send[objc.ID](objc.Send[objc.ID](objc.ID(objc.GetClass("NSString")), selAlloc), selInitBytes,
		unsafe.Pointer(&b[0]), uintptr(len(s)), uintptr(nsUTF8StringEncoding))
	runtime.KeepAlive(b)
	return str
}
