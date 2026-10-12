package imageview

import (
	"encoding/binary"
	"image"
	"math"
)

func powZoom(delta float32) float64           { return math.Exp(-float64(delta) / 200) }
func turn(src *image.RGBA, n int) *image.RGBA { o := []int{1, 6, 3, 8}[n]; return orient(src, o) }
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch o {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			copy(dst.Pix[dst.PixOffset(x, y):][:4], src.Pix[src.PixOffset(sx, sy):][:4])
		}
	}
	return dst
}
func exifOrientation(jpg []byte) int {
	if len(jpg) < 4 || jpg[0] != 255 || jpg[1] != 216 {
		return 1
	}
	for i := 2; i+4 <= len(jpg); {
		if jpg[i] != 255 {
			return 1
		}
		size := int(binary.BigEndian.Uint16(jpg[i+2:]))
		if jpg[i+1] == 218 || size < 2 || i+2+size > len(jpg) {
			return 1
		}
		seg := jpg[i+4 : i+2+size]
		if jpg[i+1] == 225 && len(seg) >= 14 && string(seg[:6]) == "Exif\x00\x00" {
			t := seg[6:]
			var bo binary.ByteOrder
			switch string(t[:2]) {
			case "II":
				bo = binary.LittleEndian
			case "MM":
				bo = binary.BigEndian
			default:
				return 1
			}
			offset := uint64(bo.Uint32(t[4:]))
			if offset+2 > uint64(len(t)) {
				return 1
			}
			n := int(bo.Uint16(t[offset:]))
			for e := int(offset) + 2; e+12 <= len(t) && n > 0; e, n = e+12, n-1 {
				if bo.Uint16(t[e:]) == 0x112 && bo.Uint16(t[e+2:]) == 3 {
					return int(bo.Uint16(t[e+8:]))
				}
			}
			return 1
		}
		i += 2 + size
	}
	return 1
}
