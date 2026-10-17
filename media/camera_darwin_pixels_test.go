//go:build darwin

package media

import (
	"testing"
	"unsafe"
)

// A packed picture says one chroma sample for each pair of pixels: the two
// pixels of the pair take their colour from it, and their own brightness.
func TestPackedPicturesBecomeRGBA(t *testing.T) {
	// Four pixels in one row: U Y0 V Y1 U Y2 V Y3. The chroma says neither
	// red nor blue, so the first pair comes out white and the second black,
	// as the first pair's brightness is full and the second pair's none.
	src := []byte{128, 235, 128, 235, 128, 16, 128, 16}
	dst := make([]byte, 4*4)
	uyvyToRGBA(dst, unsafe.Pointer(&src[0]), 4, 1, len(src), false)
	want := []byte{255, 255, 255, 255, 255, 255, 255, 255, 0, 0, 0, 255, 0, 0, 0, 255}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("pixel %d is %v, want %v", i/4, dst[i*4:i*4+4], want[i*4:i*4+4])
		}
	}
}

// The colours of the two colour space standards differ: the same chroma in a
// high definition picture is further from grey than in a standard definition
// one.
func TestHighDefinitionPicturesUseTheirOwnColour(t *testing.T) {
	// One chroma sample of V 200 with a brightness of 100: red, from the
	// standard definition table of the 601 standard.
	sd := []byte{128, 100, 200, 100}
	dst := make([]byte, 2*4)
	uyvyToRGBA(dst, unsafe.Pointer(&sd[0]), 2, 1, len(sd), false)
	if got := dst[:4]; got[0] != 213 || got[1] != 39 || got[2] != 98 || got[3] != 255 {
		t.Fatalf("a standard definition pixel is %v, want [213 39 98 255]", got)
	}
	// The same chroma and brightness in a picture high enough to carry the
	// 709 standard: a redder red.
	hd := make([]byte, 4*hdHeight)
	copy(hd, []byte{128, 100, 200, 100})
	high := make([]byte, 2*hdHeight*4)
	uyvyToRGBA(high, unsafe.Pointer(&hd[0]), 2, hdHeight, 4, true)
	if got := high[:4]; got[0] != 227 || got[1] != 60 || got[2] != 98 || got[3] != 255 {
		t.Fatalf("a high definition pixel is %v, want [227 60 98 255]", got)
	}
}

// A planar picture keeps its brightness apart from its colour, the colour
// shared by a block of four pixels.
func TestPlanarPicturesBecomeRGBA(t *testing.T) {
	// Four pixels in two rows, and one chroma pair for all of them: the
	// brightness is full on the left and none on the right.
	luma := []byte{235, 235, 16, 16, 235, 235, 16, 16}
	chroma := []byte{128, 128, 128, 128} // two pairs of no colour, for both rows
	dst := make([]byte, 4*2*4)
	planarToRGBA(dst, unsafe.Pointer(&luma[0]), 4, unsafe.Pointer(&chroma[0]), 4, 4, 2, false, false)
	want := [][3]byte{{255, 255, 255}, {255, 255, 255}, {0, 0, 0}, {0, 0, 0}}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			at := y*16 + x*4
			got := [3]byte{dst[at], dst[at+1], dst[at+2]}
			if got != want[x] {
				t.Fatalf("pixel %d,%d is %v, want %v", x, y, got, want[x])
			}
			if dst[at+3] != 0xff {
				t.Fatalf("pixel %d,%d has no alpha", x, y)
			}
		}
	}
}

// A full range picture keeps its brightness as it stands, rather than scaling
// it up from the limited range of a video picture.
func TestFullRangePicturesKeepTheirBrightness(t *testing.T) {
	luma := []byte{128, 128, 128, 128, 128, 128, 128, 128}
	chroma := []byte{128, 128, 128, 128}
	dst := make([]byte, 4*2*4)
	planarToRGBA(dst, unsafe.Pointer(&luma[0]), 4, unsafe.Pointer(&chroma[0]), 4, 4, 2, true, false)
	if got := dst[:3]; got[0] != 128 || got[1] != 128 || got[2] != 128 {
		t.Fatalf("a full range pixel is %v, want a grey of 128", got)
	}
	// The limited range would have scaled the same 128 up to a brighter grey.
	limited := make([]byte, 4*2*4)
	planarToRGBA(limited, unsafe.Pointer(&luma[0]), 4, unsafe.Pointer(&chroma[0]), 4, 4, 2, false, false)
	if got := limited[:3]; got[0] != 130 {
		t.Fatalf("a limited range pixel is %v, want a grey of 130", got)
	}
}
