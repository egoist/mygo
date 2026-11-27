package scene

import "testing"

// TestYUVToRGB checks the conversion a frame's samples take, at the
// extremes each standard and range puts its black and its white at.
func TestYUVToRGB(t *testing.T) {
	// The limited range keeps a luma of 16 for black and 235 for white,
	// and the chroma's mid 128 for no color.
	if r, g, b := BT601.YUVToRGB(16.0/255, 128.0/255, 128.0/255); r != 0 || g != 0 || b != 0 {
		t.Errorf("BT601 black = (%v, %v, %v)", r, g, b)
	}
	if r, g, b := BT601.YUVToRGB(235.0/255, 128.0/255, 128.0/255); r != 1 || g != 1 || b != 1 {
		t.Errorf("BT601 white = (%v, %v, %v)", r, g, b)
	}
	// The full range puts them at 0 and 1.
	if r, g, b := (BT601 + FullRange).YUVToRGB(0, 128.0/255, 128.0/255); r != 0 || g != 0 || b != 0 {
		t.Errorf("full-range black = (%v, %v, %v)", r, g, b)
	}
	if r, g, b := (BT601 + FullRange).YUVToRGB(1, 128.0/255, 128.0/255); r != 1 || g != 1 || b != 1 {
		t.Errorf("full-range white = (%v, %v, %v)", r, g, b)
	}

	// Full chroma pulls red out to the clamp.
	r, g, b := (BT601 + FullRange).YUVToRGB(0.5, 128.0/255, 1)
	if r != 1 {
		t.Errorf("full chroma red = %v, want the clamp", r)
	}
	if b < 0.49 || b > 0.51 {
		t.Errorf("full chroma blue = %v, want about 0.5", b)
	}
	if g < 0.13 || g > 0.16 {
		t.Errorf("full chroma green = %v, want about 0.14", g)
	}

	// A gray holds no color in every standard, and a matrix past the
	// standards is BT709.
	for _, m := range []YUVMatrix{BT601 + FullRange, BT709 + FullRange, BT2020 + FullRange, YUVMatrix(4)} {
		if r, g, b := m.YUVToRGB(0.5, 128.0/255, 128.0/255); r < 0.499 || r > 0.501 || g != r || b != r {
			t.Errorf("gray of %d = (%v, %v, %v), want half gray", m, r, g, b)
		}
	}
	// BT709 draws red more than BT601 does of the same samples: its
	// red weight is smaller, so the chroma counts for more.
	r601, _, _ := (BT601 + FullRange).YUVToRGB(0.5, 128.0/255, 200.0/255)
	r709, _, _ := (BT709 + FullRange).YUVToRGB(0.5, 128.0/255, 200.0/255)
	if r709 <= r601 {
		t.Errorf("BT709 red %v is not more than BT601's %v", r709, r601)
	}
}

// TestSurfaceRGBA checks what a frame's planes become in memory: BGRA is
// swizzled, NV12's chroma is bilinear between the samples around it.
func TestSurfaceRGBA(t *testing.T) {
	// No planes, no pixels; and no pixels for no picture.
	var empty Surface
	if p := empty.RGBA(nil, 4, 4); p != nil {
		t.Errorf("RGBA of a surface without planes = %v", p)
	}
	bg := Surface{Format: SurfaceBGRA, Planes: [2]Plane{{Pix: []byte{1, 2, 3, 4}, Stride: 4}}}
	if p := bg.RGBA(nil, 0, 1); p != nil {
		t.Errorf("RGBA of nothing = %v", p)
	}

	// BGRA's one plane swizzles to RGBA, over the padding of its rows.
	bg = Surface{Format: SurfaceBGRA, Planes: [2]Plane{
		{Pix: []byte{10, 20, 30, 40, 50, 60, 70, 80, 0xFF, 0xFF, 0xFF, 0xFF,
			10, 20, 30, 40, 50, 60, 70, 80, 0xFF, 0xFF, 0xFF, 0xFF}, Stride: 12},
	}}
	p := bg.RGBA(nil, 2, 2)
	if len(p) != 2*2*4 {
		t.Fatalf("RGBA of BGRA = %d bytes", len(p))
	}
	want := []byte{30, 20, 10, 40, 70, 60, 50, 80, 30, 20, 10, 40, 70, 60, 50, 80}
	for i, w := range want {
		if p[i] != w {
			t.Errorf("pixel byte %d = %d, want %d", i, p[i], w)
		}
	}

	// NV12 of a neutral frame is its luma, gray as it came.
	nv := Surface{Format: SurfaceNV12, Matrix: BT601 + FullRange, Planes: [2]Plane{
		{Pix: []byte{200, 100, 200, 100}, Stride: 2},
		{Pix: []byte{128, 128}, Stride: 2},
	}}
	p = nv.RGBA(nil, 2, 2)
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			luma := byte(200)
			if x == 1 {
				luma = 100
			}
			c := p[4*(2*y+x):]
			if c[0] != luma || c[1] != luma || c[2] != luma || c[3] != 255 {
				t.Errorf("pixel (%d,%d) = %v, want gray %d", x, y, c, luma)
			}
		}
	}

	// A colored frame: every pixel's chroma bilinear between the chroma
	// samples around it, which sit between luma's.
	nv = Surface{Format: SurfaceNV12, Matrix: BT601 + FullRange, Planes: [2]Plane{
		{Pix: make([]byte, 16), Stride: 4},
		{Pix: []byte{0, 255, 255, 0, 0, 255, 255, 0}, Stride: 4},
	}}
	for i := range nv.Planes[0].Pix {
		nv.Planes[0].Pix[i] = 128
	}
	p = nv.RGBA(nil, 4, 4)
	// The pixel at (1,1) sits a quarter of the way from the first chroma
	// sample to the second.
	checkNV12(t, p, 1, 1, 217, 105, 14)
	// The pixel at (0,0) lies outside the chroma samples, which clamp to
	// their edges.
	checkNV12(t, p, 0, 0, 255, 81, 0)
}

// checkNV12 checks the pixel at (x, y) of an NV12 conversion, within one
// of a byte.
func checkNV12(t *testing.T, p []byte, x, y int, r, g, b byte) {
	t.Helper()
	c := p[4*(4*y+x):]
	for i, w := range []byte{r, g, b, 255} {
		if int(c[i]) < int(w)-1 || int(c[i]) > int(w)+1 {
			t.Errorf("pixel (%d,%d) channel %d = %d, want about %d", x, y, i, c[i], w)
		}
	}
}

// TestSurfaceHelpers checks the small conversions the frames read through.
func TestSurfaceHelpers(t *testing.T) {
	if v := floor(2.9); v != 2 {
		t.Errorf("floor(2.9) = %d", v)
	}
	if v := floor(3); v != 3 {
		t.Errorf("floor(3) = %d", v)
	}
	if v := floor(-0.1); v != -1 {
		t.Errorf("floor(-0.1) = %d", v)
	}
	if v := floor(-2); v != -2 {
		t.Errorf("floor(-2) = %d", v)
	}
	if v := to8(0); v != 0 {
		t.Errorf("to8(0) = %d", v)
	}
	if v := to8(0.5); v != 128 {
		t.Errorf("to8(0.5) = %d", v)
	}
	if v := to8(1); v != 255 {
		t.Errorf("to8(1) = %d", v)
	}
	if v := clamp01(-1); v != 0 {
		t.Errorf("clamp01(-1) = %v", v)
	}
	if v := clamp01(2); v != 1 {
		t.Errorf("clamp01(2) = %v", v)
	}
	if v := clamp01(0.5); v != 0.5 {
		t.Errorf("clamp01(0.5) = %v", v)
	}
}
