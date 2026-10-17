package media

import (
	"unsafe"
)

// A picture the platform hands out is 32 bits a pixel: blue, green, red and
// unused, in memory, which the UI draws as red, green, blue and opaque.
//
// The two shapes a platform buffer takes are a picture with rows of its own,
// whose stride is the distance between two of them as the platform reports it
// (a Media Foundation 2D buffer says so when it is locked), and a flat stretch
// of pixels, whose stride the caller knows from what it asked for. Both are
// copied top row first here.

// copyRGB32Rows copies a picture of w×h 32-bit pixels into dst as 8-bit RGBA,
// its first displayed row first. first is the address of that row, which is
// what a 2D buffer's lock returns, and stride is the distance to the row below
// it: negative when that row is at a lower address, as a picture stored from
// its bottom row up has it.
func copyRGB32Rows(dst []byte, first unsafe.Pointer, w, h, stride int) error {
	if w <= 0 || h <= 0 {
		return unsupported("a picture of %d×%d", w, h)
	}
	if first == nil {
		return unsupported("a picture with no memory")
	}
	span := stride
	if span < 0 {
		span = -span
	}
	if span < w*4 {
		return unsupported("a picture with a stride of %d", stride)
	}
	if len(dst) < w*h*4 {
		return unsupported("a picture in %d bytes", len(dst))
	}
	out := dst[:w*h*4]
	for y := range h {
		row := unsafe.Slice((*byte)(unsafe.Add(first, y*stride)), w*4)
		for x := range w {
			out[(y*w+x)*4+0] = row[x*4+2]
			out[(y*w+x)*4+1] = row[x*4+1]
			out[(y*w+x)*4+2] = row[x*4+0]
			out[(y*w+x)*4+3] = 0xff
		}
	}
	return nil
}

// copyRGB32Flat copies a flat buffer of length bytes holding a picture of w×h
// 32-bit pixels into dst as 8-bit RGBA, its first displayed row first. base is
// the start of the memory the rows are in — what a flat buffer's lock returns
// — and stride is the distance between two rows, zero when the platform left
// it out and the rows are packed as the picture's width makes them. A negative
// stride says the picture is stored from its bottom row up.
func copyRGB32Flat(dst []byte, base unsafe.Pointer, length, w, h, stride int) error {
	if w <= 0 || h <= 0 {
		return unsupported("a picture of %d×%d", w, h)
	}
	if base == nil {
		return unsupported("a picture with no memory")
	}
	// A buffer that says nothing of its stride holds rows as wide as its
	// pixels: the default is what its length is measured against too, so
	// that a picture the UI asks for is not turned away for being exactly
	// as long as it is.
	if stride == 0 {
		stride = w * 4
	}
	span := stride
	if span < 0 {
		span = -span
	}
	if span < w*4 || length < span*h {
		return unsupported("a picture of %d bytes", length)
	}
	if stride < 0 {
		// The memory starts at the last displayed row of a bottom-up
		// picture.
		base = unsafe.Add(base, (h-1)*span)
	}
	return copyRGB32Rows(dst, base, w, h, stride)
}
