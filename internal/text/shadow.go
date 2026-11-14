package text

import "math"

// blurBitmap returns the coverage of b blurred by a Gaussian of standard
// deviation sigma pixels, as a mask reaching three of them past b on each
// side: a glyph's shadow. Color glyphs blur their alpha, subpixel ones
// their most covered channel; a sigma of 0 leaves the coverage sharp.
func blurBitmap(b bitmap, sigma float32) bitmap { return blurred(b, sigma, false) }

// blurred returns b blurred as blurBitmap does, but for a color glyph
// with colors, which blurs its four premultiplied channels into a color
// bitmap: a glyph blurred where it shows (BlurGlyph).
func blurred(b bitmap, sigma float32, colors bool) bitmap {
	pad := 0
	if sigma > 0 {
		pad = int(math.Ceil(float64(3*sigma))) + 1
	}
	n := 1 // channels
	if colors && b.color {
		n = 4
	}
	w, h := b.w+2*pad, b.h+2*pad
	planes := make([][]float32, n)
	for c := range planes {
		planes[c] = make([]float32, w*h)
	}
	for y := range b.h {
		for x := range b.w {
			i := y*b.w + x
			at := (y+pad)*w + x + pad
			var c byte
			switch {
			case n == 4:
				for ch, plane := range planes {
					plane[at] = float32(b.pix[4*i+ch]) / 255
				}
				continue
			case b.color:
				c = b.pix[4*i+3]
			case b.subpixel:
				p := b.pix[4*i:]
				c = max(p[0], p[1], p[2])
			default:
				c = b.pix[i]
			}
			planes[0][at] = float32(c) / 255
		}
	}
	if pad > 0 {
		// The kernel, normalized, and the blur in rows, then in columns.
		kernel := make([]float32, 2*pad+1)
		var sum float32
		for i := range kernel {
			d := float64(i - pad)
			kernel[i] = float32(math.Exp(-d * d / (2 * float64(sigma) * float64(sigma))))
			sum += kernel[i]
		}
		for i := range kernel {
			kernel[i] /= sum
		}
		tmp := make([]float32, w*h)
		blur := func(dst, src []float32, n, step, lines, across int) {
			for l := range lines {
				for i := range n {
					var v float32
					for k, kv := range kernel {
						if j := i + k - pad; j >= 0 && j < n {
							v += src[l*across+j*step] * kv
						}
					}
					dst[l*across+i*step] = v
				}
			}
		}
		for _, cov := range planes {
			blur(tmp, cov, w, 1, h, w) // rows
			blur(cov, tmp, h, w, w, 1) // columns
		}
	}
	pix := make([]byte, n*w*h)
	for c, cov := range planes {
		for i, v := range cov {
			pix[n*i+c] = uint8(min(max(v, 0), 1)*255 + 0.5)
		}
	}
	if n == 4 {
		// Premultiplied still, which rounding could undo.
		for i := 0; i < len(pix); i += 4 {
			a := pix[i+3]
			pix[i], pix[i+1], pix[i+2] = min(pix[i], a), min(pix[i+1], a), min(pix[i+2], a)
		}
	}
	return bitmap{left: b.left - pad, top: b.top - pad, w: w, h: h, pix: pix, color: n == 4}
}
