package printdoc

import (
	"image"
	"math"
)

// BGRA is opaque, top-down, tightly packed, native-endian ARGB32 on the
// supported little-endian platforms. Flattening matches PDF export.
func BGRA(img *image.RGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	b := make([]byte, 4*w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := img.RGBAAt(x+img.Rect.Min.X, y+img.Rect.Min.Y)
			i, white := 4*(y*w+x), uint16(255-p.A)
			b[i], b[i+1], b[i+2], b[i+3] = uint8(min(255, uint16(p.B)+white)), uint8(min(255, uint16(p.G)+white)), uint8(min(255, uint16(p.R)+white)), 255
		}
	}
	return b
}

// Fit centers one fixed page in the printer's available area. Physical
// printers differ in their imageable bounds; no page is cropped or split.
func Fit(pageW, pageH, areaW, areaH float64) (x, y, w, h float64) {
	s := math.Min(areaW/pageW, areaH/pageH)
	w, h = pageW*s, pageH*s
	return (areaW - w) / 2, (areaH - h) / 2, w, h
}
