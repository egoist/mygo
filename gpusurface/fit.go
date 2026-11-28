package gpusurface

// FitRect is where a fw x fh frame shows whole in a dw x dh drawable,
// centred on whole pixels: its top-left x, y and its size w, h in the
// drawable's pixels. A frame of the drawable's shape, or an empty size on
// either side, fills it.
func FitRect(fw, fh, dw, dh int) (x, y, w, h int) {
	if fw <= 0 || fh <= 0 || dw <= 0 || dh <= 0 || fw*dh == fh*dw {
		return 0, 0, max(dw, 0), max(dh, 0)
	}
	w, h = dw, dh
	if fw*dh > fh*dw {
		h = (dw*fh + fw/2) / fw
	} else {
		w = (dh*fw + fh/2) / fh
	}
	return (dw - w) / 2, (dh - h) / 2, w, h
}

// fitRect is FitRect as the scale x, y and offset x, y in clip space that
// take the drawable's quad to it, and whether it leaves bars.
func fitRect(fw, fh, dw, dh uintptr) (fit [4]float32, bars bool) {
	x0, y0, vw, vh := FitRect(int(fw), int(fh), int(dw), int(dh))
	if vw == int(dw) && vh == int(dh) {
		return [4]float32{1, 1, 0, 0}, false
	}
	w, h := float32(dw), float32(dh)
	return [4]float32{float32(vw) / w, float32(vh) / h, float32(2*x0+vw)/w - 1, 1 - float32(2*y0+vh)/h}, true
}
