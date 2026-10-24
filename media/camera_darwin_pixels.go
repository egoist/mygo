//go:build darwin

package media

import (
	"unsafe"

	"github.com/egoist/mygo/media/internal/darwin"
)

// hdHeight is the height from which a picture is taken to carry the high
// definition colour of its camera rather than the standard definition one.
const hdHeight = 720

// copyCameraPicture turns one picture a capture session delivered into the RGBA
// rows this package hands out. A capture output converts nothing for the app:
// it hands over what the camera makes, which here is packed or planar YCbCr.
func copyCameraPicture(dst []byte, picture uintptr, w, h int) error {
	if w <= 0 || h <= 0 {
		return unsupported("a camera picture of %d by %d", w, h)
	}
	hd := h >= hdHeight
	switch format := darwin.PixelFormat(picture); format {
	case darwin.PixelFormat32BGRA, darwin.PixelFormat32RGBA:
		base := darwin.PixelBase(picture)
		if base == nil {
			return unsupported("a camera picture with no pixels")
		}
		return copyPicture(dst, base, w, h, darwin.PixelStride(picture), format)
	case darwin.PixelFormat2vuy:
		base := darwin.PixelBase(picture)
		stride := darwin.PixelStride(picture)
		if base == nil || stride < w*2 {
			return unsupported("a camera picture of %d by %d with a stride of %d", w, h, stride)
		}
		uyvyToRGBA(dst, base, w, h, stride, hd)
		return nil
	case darwin.PixelFormat420v, darwin.PixelFormat420f:
		planes := darwin.PixelPlanes(picture)
		if planes < 2 {
			return unsupported("a camera picture of %d planes", planes)
		}
		luma := darwin.PixelPlaneBase(picture, 0)
		chroma := darwin.PixelPlaneBase(picture, 1)
		lumaStride := darwin.PixelPlaneStride(picture, 0)
		chromaStride := darwin.PixelPlaneStride(picture, 1)
		if luma == nil || chroma == nil || lumaStride < w || chromaStride < (w/2)*2 {
			return unsupported("a planar camera picture of %d by %d", w, h)
		}
		planarToRGBA(dst, luma, lumaStride, chroma, chromaStride, w, h, format == darwin.PixelFormat420f, hd)
		return nil
	default:
		return unsupported("a camera picture of format %s", fourCC(format))
	}
}

// uyvyToRGBA converts a picture whose chroma is shared by pairs of pixels,
// stored as U Y0 V Y1 (4:2:2 packed), to RGBA.
func uyvyToRGBA(dst []byte, base unsafe.Pointer, w, h, stride int, hd bool) {
	src := unsafe.Slice((*byte)(base), stride*h)
	for y := 0; y < h; y++ {
		in := src[y*stride : y*stride+w*2]
		out := dst[y*w*4 : (y+1)*w*4]
		for x := 0; x < w; x += 2 {
			u, v := int32(in[x*2]), int32(in[x*2+2])
			dr, dg, db := chromaOffsets(u, v, hd)
			putPixel(out, x*4, lumaTerm(int32(in[x*2+1]), false), dr, dg, db)
			if x+1 < w {
				putPixel(out, x*4+4, lumaTerm(int32(in[x*2+3]), false), dr, dg, db)
			}
		}
	}
}

// planarToRGBA converts a picture of a full luma plane and a chroma plane whose
// blue and red differences are interleaved for pairs of pixels (4:2:0
// bi-planar), to RGBA.
func planarToRGBA(dst []byte, luma unsafe.Pointer, lumaStride int, chroma unsafe.Pointer, chromaStride, w, h int, full, hd bool) {
	lumaRows := unsafe.Slice((*byte)(luma), lumaStride*h)
	chromaRows := unsafe.Slice((*byte)(chroma), chromaStride*((h+1)/2))
	for y := 0; y < h; y++ {
		in := lumaRows[y*lumaStride : y*lumaStride+w]
		cs := chromaRows[(y/2)*chromaStride:]
		out := dst[y*w*4 : (y+1)*w*4]
		for x := 0; x < w; x += 2 {
			dr, dg, db := chromaOffsets(int32(cs[x]), int32(cs[x+1]), hd)
			putPixel(out, x*4, lumaTerm(int32(in[x]), full), dr, dg, db)
			if x+1 < w {
				putPixel(out, x*4+4, lumaTerm(int32(in[x+1]), full), dr, dg, db)
			}
		}
	}
}

// lumaTerm scales one luma sample the way the conversion tables do, in
// 1/256ths: from the limited range of a video picture, or as it stands when the
// camera made a full range one.
func lumaTerm(y int32, full bool) int32 {
	if full {
		return y << 8
	}
	return 298 * (y - 16)
}

// chromaOffsets returns what one chroma sample adds to each of the three
// channels, in 1/256ths, in the colour of a high definition picture or of a
// standard definition one.
func chromaOffsets(u, v int32, hd bool) (dr, dg, db int32) {
	d := u - 128
	e := v - 128
	if hd {
		return 459 * e, -55*d - 136*e, 541 * d
	}
	return 409 * e, -100*d - 208*e, 516 * d
}

// putPixel writes one converted pixel to a row of RGBA.
func putPixel(dst []byte, at int, luma, dr, dg, db int32) {
	dst[at+0] = clampByte((luma + dr + 128) >> 8)
	dst[at+1] = clampByte((luma + dg + 128) >> 8)
	dst[at+2] = clampByte((luma + db + 128) >> 8)
	dst[at+3] = 0xff
}

// clampByte keeps a converted channel between none and full.
func clampByte(v int32) byte {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return byte(v)
}
