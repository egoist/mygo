package vec

import (
	"bytes"
	"testing"
)

// maskOf rasterizes what draw draws into a w×h mask of one byte per pixel.
func maskOf(w, h int, draw func(*Rasterizer)) []byte {
	var z Rasterizer
	z.Reset(w, h)
	draw(&z)
	dst := make([]byte, w*h)
	z.Mask(dst, w)
	return dst
}

// checkMask compares a mask with the rows wanted, each row a row of pixels.
func checkMask(t *testing.T, got []byte, w int, want [][]byte) {
	t.Helper()
	for y, row := range want {
		for x, wv := range row {
			if got[y*w+x] != wv {
				t.Errorf("pixel (%d,%d) = %d, want %d", x, y, got[y*w+x], wv)
				return
			}
		}
	}
}

// TestRasterizerRectangle checks a shape with straight edges: the pixels
// inside are covered whole, and the ones outside not at all.
func TestRasterizerRectangle(t *testing.T) {
	got := maskOf(8, 6, func(z *Rasterizer) {
		z.MoveTo(2, 2)
		z.LineTo(6, 2)
		z.LineTo(6, 5)
		z.LineTo(2, 5)
		z.ClosePath()
	})
	want := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 255, 255, 255, 255, 0, 0},
		{0, 0, 255, 255, 255, 255, 0, 0},
		{0, 0, 255, 255, 255, 255, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	checkMask(t, got, 8, want)

	// A path left open is closed by the mask itself.
	open := maskOf(8, 6, func(z *Rasterizer) {
		z.MoveTo(2, 2)
		z.LineTo(6, 2)
		z.LineTo(6, 5)
		z.LineTo(2, 5)
	})
	if !bytes.Equal(open, got) {
		t.Errorf("an open path covers %v, want %v", open, got)
	}
}

// TestRasterizerTriangle checks a slanted edge: the pixels it crosses are
// covered in part, by how much of them the edge covers.
func TestRasterizerTriangle(t *testing.T) {
	got := maskOf(4, 3, func(z *Rasterizer) {
		z.MoveTo(0, 0)
		z.LineTo(2, 0)
		z.LineTo(0, 2)
		z.ClosePath()
	})
	want := [][]byte{
		{255, 128, 0, 0},
		{128, 0, 0, 0},
		{0, 0, 0, 0},
	}
	checkMask(t, got, 4, want)
}

// TestRasterizerEvenOdd checks the even-odd rule: what two contours cover
// twice is outside the shape.
func TestRasterizerEvenOdd(t *testing.T) {
	draw := func(z *Rasterizer) {
		z.MoveTo(1, 1)
		z.LineTo(5, 1)
		z.LineTo(5, 4)
		z.LineTo(1, 4)
		z.ClosePath()
		z.MoveTo(3, 2)
		z.LineTo(7, 2)
		z.LineTo(7, 5)
		z.LineTo(3, 5)
		z.ClosePath()
	}

	var z Rasterizer
	z.Reset(8, 6)
	draw(&z)
	nonZero := make([]byte, 8*6)
	z.Mask(nonZero, 8)
	want := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 255, 255, 255, 255, 0, 0, 0},
		{0, 255, 255, 255, 255, 255, 255, 0},
		{0, 255, 255, 255, 255, 255, 255, 0},
		{0, 0, 0, 255, 255, 255, 255, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	checkMask(t, nonZero, 8, want)

	z.Reset(8, 6)
	draw(&z)
	evenOdd := make([]byte, 8*6)
	z.MaskEvenOdd(evenOdd, 8)
	want = [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 255, 255, 255, 255, 0, 0, 0},
		{0, 255, 255, 0, 0, 255, 255, 0},
		{0, 255, 255, 0, 0, 255, 255, 0},
		{0, 0, 0, 255, 255, 255, 255, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	checkMask(t, evenOdd, 8, want)
}

// TestRasterizerCurves checks what a curve draws: a curve whose control
// points lie on a straight line draws that line, and a curve that bows
// away from it covers more than the line does.
func TestRasterizerCurves(t *testing.T) {
	straight := maskOf(8, 6, func(z *Rasterizer) {
		z.MoveTo(2, 2)
		z.LineTo(6, 2)
		z.LineTo(6, 5)
		z.LineTo(2, 5)
		z.ClosePath()
	})

	// A quadratic curve with its control point on the line draws the line.
	quad := maskOf(8, 6, func(z *Rasterizer) {
		z.MoveTo(2, 2)
		z.QuadTo(4, 2, 6, 2)
		z.LineTo(6, 5)
		z.QuadTo(4, 5, 2, 5)
		z.ClosePath()
	})
	if !bytes.Equal(quad, straight) {
		t.Errorf("a quadratic curve on a line covers %v, want %v", quad, straight)
	}

	// A cubic one does too.
	cube := maskOf(8, 6, func(z *Rasterizer) {
		z.MoveTo(2, 2)
		z.CubeTo(4, 2, 4, 2, 6, 2)
		z.LineTo(6, 5)
		z.CubeTo(4, 5, 4, 5, 2, 5)
		z.ClosePath()
	})
	if !bytes.Equal(cube, straight) {
		t.Errorf("a cubic curve on a line covers %v, want %v", cube, straight)
	}

	// A curve that bows above its chord covers more than the chord does,
	// and the pixels of the bow are covered in part.
	bulge := maskOf(4, 4, func(z *Rasterizer) {
		z.MoveTo(0, 2)
		z.QuadTo(2, 0, 4, 2)
		z.LineTo(4, 4)
		z.LineTo(0, 4)
		z.ClosePath()
	})
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if bulge[y*4+x] != 255 {
				t.Errorf("pixel (%d,%d) below the bow = %d, want 255", x, y, bulge[y*4+x])
			}
		}
	}
	if coverage(bulge[:4]) != 0 {
		t.Errorf("the first row of the bow is covered: %v", bulge[:4])
	}
	if coverage(bulge[4:8]) == 0 || coverage(bulge[4:8]) >= 4*255 {
		t.Errorf("the row of the bow is covered %d, want in part", coverage(bulge[4:8]))
	}
	// The shape is the rectangle below the chord and the bow above it: the
	// bow's area is between two thirds of its width times its height and
	// that height times its width.
	if area := float64(coverage(bulge)) / 255; area <= 8 || area >= 13.4 {
		t.Errorf("the area of the bowed shape = %v, want between 8 and 13.4", area)
	}

	// A cubic curve bows above its chord as well.
	cubic := maskOf(4, 4, func(z *Rasterizer) {
		z.MoveTo(0, 2)
		z.CubeTo(2, 0, 2, 0, 4, 2)
		z.LineTo(4, 4)
		z.LineTo(0, 4)
		z.ClosePath()
	})
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if cubic[y*4+x] != 255 {
				t.Errorf("pixel (%d,%d) below the cubic bow = %d, want 255", x, y, cubic[y*4+x])
			}
		}
	}
	if coverage(cubic[:8]) == 0 {
		t.Errorf("the cubic curve bows nowhere: %v", cubic[:8])
	}
	if area := float64(coverage(cubic)) / 255; area <= 8 || area >= 13.4 {
		t.Errorf("the area of the cubic shape = %v, want between 8 and 13.4", area)
	}
}

// TestRasterizerWideEdge checks a slanted edge that crosses many pixels
// of a row: each of them is covered by how much of the shape falls in it.
func TestRasterizerWideEdge(t *testing.T) {
	got := maskOf(10, 2, func(z *Rasterizer) {
		z.MoveTo(0, 0)
		z.LineTo(9, 1)
		z.LineTo(0, 1)
		z.ClosePath()
	})
	// The shape is a triangle a pixel high: its height over a column x is
	// 1 - x/9, so the pixels of the row are covered by that much of them.
	want := []float64{
		1 - 1.0/18, 1 - 3.0/18, 1 - 5.0/18, 1 - 7.0/18, 0.5,
		1 - 11.0/18, 1 - 13.0/18, 1 - 15.0/18, 1 - 17.0/18, 0,
	}
	for x, w := range want {
		got := float64(got[x]) / 255
		if got < w-0.01 || got > w+0.01 {
			t.Errorf("pixel (%d,0) = %v, want %v", x, got, w)
		}
	}
	if coverage(got[10:]) != 0 {
		t.Errorf("the row below the shape is covered: %v", got[10:])
	}
}

// TestRasterizerEvenOddPartial checks the even-odd rule where a contour
// covers a pixel in part: what is covered twice is outside, and what is
// covered once and a half is half covered.
func TestRasterizerEvenOddPartial(t *testing.T) {
	draw := func(z *Rasterizer) {
		z.MoveTo(0, 0)
		z.LineTo(2, 0)
		z.LineTo(2, 2)
		z.LineTo(0, 2)
		z.ClosePath()
		z.MoveTo(0, 0)
		z.LineTo(2, 0)
		z.LineTo(0, 2)
		z.ClosePath()
	}

	var z Rasterizer
	z.Reset(3, 3)
	draw(&z)
	nonZero := make([]byte, 3*3)
	z.Mask(nonZero, 3)
	checkMask(t, nonZero, 3, [][]byte{
		{255, 255, 0},
		{255, 255, 0},
		{0, 0, 0},
	})

	z.Reset(3, 3)
	draw(&z)
	evenOdd := make([]byte, 3*3)
	z.MaskEvenOdd(evenOdd, 3)
	checkMask(t, evenOdd, 3, [][]byte{
		{0, 128, 0},
		{128, 255, 0},
		{0, 0, 0},
	})
}

// TestRasterizerOutside checks what a shape that reaches out of the mask
// covers: the part of it inside, and nothing else.
func TestRasterizerOutside(t *testing.T) {
	// Above the mask: only the part of the shape that falls in the first
	// row is covered.
	above := maskOf(4, 3, func(z *Rasterizer) {
		z.MoveTo(0, -1)
		z.LineTo(2, -1)
		z.LineTo(2, 0.5)
		z.LineTo(0, 0.5)
		z.ClosePath()
	})
	checkMask(t, above, 4, [][]byte{
		{128, 128, 0, 0},
		{0, 0, 0, 0},
		{0, 0, 0, 0},
	})

	// Below the mask, the same.
	below := maskOf(4, 3, func(z *Rasterizer) {
		z.MoveTo(0, 2.5)
		z.LineTo(2, 2.5)
		z.LineTo(2, 4)
		z.LineTo(0, 4)
		z.ClosePath()
	})
	checkMask(t, below, 4, [][]byte{
		{0, 0, 0, 0},
		{0, 0, 0, 0},
		{128, 128, 0, 0},
	})

	// To the left of the mask: the part of the shape that is inside it.
	left := maskOf(4, 3, func(z *Rasterizer) {
		z.MoveTo(-1, 0)
		z.LineTo(1, 0)
		z.LineTo(1, 2)
		z.LineTo(-1, 2)
		z.ClosePath()
	})
	checkMask(t, left, 4, [][]byte{
		{255, 0, 0, 0},
		{255, 0, 0, 0},
		{0, 0, 0, 0},
	})

	// A shape that is nowhere near the mask covers nothing.
	away := maskOf(4, 3, func(z *Rasterizer) {
		z.MoveTo(10, 10)
		z.LineTo(12, 10)
		z.LineTo(12, 12)
		z.LineTo(10, 12)
		z.ClosePath()
	})
	checkMask(t, away, 4, [][]byte{
		{0, 0, 0, 0},
		{0, 0, 0, 0},
		{0, 0, 0, 0},
	})
}

// TestRasterizerReset checks that a rasterizer used again draws only what
// it is given, in the mask it is given.
func TestRasterizerReset(t *testing.T) {
	var z Rasterizer
	z.Reset(8, 6)
	z.MoveTo(2, 2)
	z.LineTo(6, 2)
	z.LineTo(6, 5)
	z.LineTo(2, 5)
	z.ClosePath()
	dst := make([]byte, 8*6)
	z.Mask(dst, 8)
	if coverage(dst) == 0 {
		t.Fatal("the rectangle covers nothing")
	}

	// A smaller mask, and only what is drawn in it.
	z.Reset(4, 3)
	z.MoveTo(0, 0)
	z.LineTo(2, 0)
	z.LineTo(2, 2)
	z.LineTo(0, 2)
	z.ClosePath()
	small := make([]byte, 4*3)
	z.Mask(small, 4)
	checkMask(t, small, 4, [][]byte{
		{255, 255, 0, 0},
		{255, 255, 0, 0},
		{0, 0, 0, 0},
	})

	// A bigger mask again: what was drawn before is gone.
	z.Reset(8, 6)
	z.MoveTo(0, 0)
	z.LineTo(1, 0)
	z.LineTo(1, 1)
	z.LineTo(0, 1)
	z.ClosePath()
	big := make([]byte, 8*6)
	z.Mask(big, 8)
	checkMask(t, big, 8, [][]byte{
		{255, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
	})
}

// TestRasterizerStride checks a mask whose rows are wider than the shape
// in it: the rest of the row is left as it was.
func TestRasterizerStride(t *testing.T) {
	var z Rasterizer
	z.Reset(4, 2)
	z.MoveTo(0, 0)
	z.LineTo(2, 0)
	z.LineTo(2, 2)
	z.LineTo(0, 2)
	z.ClosePath()
	dst := bytes.Repeat([]byte{0xAB}, 6*2)
	z.Mask(dst, 6)
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			want := byte(0)
			if x < 2 {
				want = 255
			}
			if dst[y*6+x] != want {
				t.Errorf("pixel (%d,%d) = %d, want %d", x, y, dst[y*6+x], want)
			}
		}
		if dst[y*6+4] != 0xAB || dst[y*6+5] != 0xAB {
			t.Errorf("row %d past the shape = %v, want untouched", y, dst[y*6+4:y*6+6])
		}
	}
}

// coverage adds the coverage of a mask.
func coverage(b []byte) int {
	n := 0
	for _, v := range b {
		n += int(v)
	}
	return n
}
