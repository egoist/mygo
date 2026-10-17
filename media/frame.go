package media

import (
	"image"
	"time"
)

// A PixelFormat is how a picture's pixels are laid out.
type PixelFormat uint8

const (
	PixelUnknown PixelFormat = iota

	// PixelRGBA is 8-bit red, green, blue and alpha, packed in one plane,
	// in that order.
	PixelRGBA

	// PixelBGRA is the same, blue first, as the pixel buffers of macOS
	// and Windows hold.
	PixelBGRA

	// PixelNV12 is 8-bit luma, then the two chroma channels interleaved
	// two pixels at a time, in two planes, the second half as tall.
	PixelNV12

	// PixelI420 is 8-bit luma, then blue chroma, then red chroma, in
	// three planes of a quarter of the pixels each.
	PixelI420

	// PixelP010 is 10-bit luma and chroma in the top bits of 16-bit
	// samples, in the two planes of PixelNV12.
	PixelP010

	// PixelUYVY is 8-bit luma and chroma packed two pixels at a time, in
	// the order chroma, luma, chroma, luma.
	PixelUYVY

	// PixelYUYV is the same, in the order luma, chroma, luma, chroma.
	PixelYUYV
)

// String returns the format's name, or "unknown" for one this version does
// not know.
func (f PixelFormat) String() string {
	switch f {
	case PixelRGBA:
		return "RGBA"
	case PixelBGRA:
		return "BGRA"
	case PixelNV12:
		return "NV12"
	case PixelI420:
		return "I420"
	case PixelP010:
		return "P010"
	case PixelUYVY:
		return "UYVY"
	case PixelYUYV:
		return "YUYV"
	}
	return "unknown"
}

// A Plane is one plane of a picture: its rows of pixels, Stride bytes
// apart.
type Plane struct {
	// Pix holds the plane. Rows start every Stride bytes; for a planar
	// format the chroma planes are subsampled, as their format says.
	Pix []byte
	// Stride is the distance from one row to the next, in bytes, which
	// may be more than the row's pixels need: a decoder lines its rows up
	// as its hardware likes.
	Stride int
}

// A Frame is one picture: its format, its size, and the memory holding it,
// with the time it belongs at.
//
// A frame owns its memory until [Frame.Release], after which the source may
// write there again. Read what is wanted out of it first, or hand it to
// something that copies or keeps it alive, as the UI's bitmap does not:
// give the bitmap the frame's image, release the frame once the next one
// has been drawn.
type Frame struct {
	Format PixelFormat

	// Width and Height are the picture's size in pixels.
	Width, Height int

	// Planes holds the picture, in the format's own order.
	Planes []Plane

	// PTS is when the picture belongs, from the start of the media, and
	// Duration how long it stays, or 0 when that is not known.
	PTS, Duration time.Duration

	release func()
}

// NewFrame makes a frame of the given picture, which the caller owns and
// releases; release, when not nil, is called by [Frame.Release].
//
// It is for a source inside or outside this package that has pixels of its
// own to hand out.
func NewFrame(format PixelFormat, width, height int, planes []Plane, pts, duration time.Duration, release func()) *Frame {
	return &Frame{
		Format:   format,
		Width:    width,
		Height:   height,
		Planes:   planes,
		PTS:      pts,
		Duration: duration,
		release:  release,
	}
}

// Release gives the frame's memory back to its source, which may then write
// to it. Releasing a frame twice, or using it after, is a mistake: the
// pixels may be replaced by another picture at any time.
//
// A frame that is never released is never reused: its source allocates
// another buffer for the next picture, which costs memory and work a frame.
func (f *Frame) Release() {
	if f == nil || f.release == nil {
		return
	}
	f.release()
	f.release = nil
}

// Image returns the frame's picture as an *image.RGBA, sharing the frame's
// memory rather than copying it: it is good until the frame is released.
//
// It returns nil for a format that is not one packed plane of RGBA.
func (f *Frame) Image() *image.RGBA {
	if f == nil || f.Format != PixelRGBA || len(f.Planes) == 0 {
		return nil
	}
	p := f.Planes[0]
	if p.Stride < 4*f.Width || len(p.Pix) < p.Stride*f.Height {
		return nil
	}
	return &image.RGBA{
		Pix:    p.Pix,
		Stride: p.Stride,
		Rect:   image.Rect(0, 0, f.Width, f.Height),
	}
}
