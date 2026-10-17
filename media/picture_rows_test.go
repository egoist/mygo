package media

import (
	"testing"
	"unsafe"
)

// A picture's pixels as they are in memory are blue, green, red and unused,
// and as they are handed out they are red, green, blue and opaque.
func rwRow(row []byte, w, displayRow int) {
	for x := range w {
		p := row[x*4:]
		p[0] = byte(10*displayRow + x)      // blue
		p[1] = byte(100 + 2*displayRow + x) // green
		p[2] = byte(200 - 3*displayRow - x) // red
		p[3] = 0xcc                         // unused
	}
}

func rwPicture(w, h int) []byte {
	out := make([]byte, w*h*4)
	for y := range h {
		for x := range w {
			p := out[(y*w+x)*4:]
			p[0] = byte(200 - 3*y - x)
			p[1] = byte(100 + 2*y + x)
			p[2] = byte(10*y + x)
			p[3] = 0xff
		}
	}
	return out
}

// TestCopyRGB32RowsTopDown covers what Lock2D returns for a picture stored from
// its top row down: the first displayed row is the first in memory, and its
// rows are further apart than its pixels.
func TestCopyRGB32RowsTopDown(t *testing.T) {
	const w, h = 3, 4
	const span = w*4 + 4
	alloc := make([]byte, span*h)
	for y := range h {
		rwRow(alloc[y*span:], w, y)
	}
	dst := make([]byte, w*h*4)
	if err := copyRGB32Rows(dst, unsafe.Pointer(&alloc[0]), w, h, span); err != nil {
		t.Fatalf("copyRGB32Rows: %v", err)
	}
	if got, want := dst, rwPicture(w, h); !equalBytes(got, want) {
		t.Errorf("a top-down picture copied to %x, want %x", got, want)
	}
}

// TestCopyRGB32RowsBottomUp covers what Lock2D returns for a picture stored
// from its bottom row up: the pointer is the first displayed row, at the end of
// the memory the rows are in, and a negative stride walks back towards the
// previous one.
func TestCopyRGB32RowsBottomUp(t *testing.T) {
	const w, h = 3, 4
	const span = w*4 + 4
	alloc := make([]byte, span*h)
	for y := range h {
		// Memory row m is display row h-1-m of a bottom-up picture.
		rwRow(alloc[(h-1-y)*span:], w, y)
	}
	first := unsafe.Pointer(&alloc[(h-1)*span])
	dst := make([]byte, w*h*4)
	if err := copyRGB32Rows(dst, first, w, h, -span); err != nil {
		t.Fatalf("copyRGB32Rows: %v", err)
	}
	if got, want := dst, rwPicture(w, h); !equalBytes(got, want) {
		t.Errorf("a bottom-up picture copied to %x, want %x", got, want)
	}
}

func TestCopyRGB32RowsRejectsNarrowStride(t *testing.T) {
	const w, h = 3, 2
	alloc := make([]byte, w*h*4)
	if err := copyRGB32Rows(make([]byte, w*h*4), unsafe.Pointer(&alloc[0]), w, h, w*4-4); err == nil {
		t.Fatal("a picture whose rows overlap was copied")
	}
}

func TestCopyRGB32RowsRejectsNoMemory(t *testing.T) {
	if err := copyRGB32Rows(make([]byte, 3*2*4), nil, 3, 2, 3*4); err == nil {
		t.Fatal("a picture with no memory was copied")
	}
}

func TestCopyRGB32RowsRejectsSmallOutput(t *testing.T) {
	const w, h = 3, 2
	alloc := make([]byte, w*h*4)
	if err := copyRGB32Rows(make([]byte, w*h*4-1), unsafe.Pointer(&alloc[0]), w, h, w*4); err == nil {
		t.Fatal("a picture copied into a buffer too small for it")
	}
}

// TestCopyRGB32FlatPacked covers the case the platform leaving a buffer's
// stride out means: rows packed as the picture's width makes them, top down.
// A buffer of exactly the picture's size is a whole picture, not one too
// short.
func TestCopyRGB32FlatPacked(t *testing.T) {
	const w, h = 3, 4
	alloc := make([]byte, w*h*4)
	for y := range h {
		rwRow(alloc[y*w*4:], w, y)
	}
	dst := make([]byte, w*h*4)
	if err := copyRGB32Flat(dst, unsafe.Pointer(&alloc[0]), len(alloc), w, h, 0); err != nil {
		t.Fatalf("copyRGB32Flat: %v", err)
	}
	if got, want := dst, rwPicture(w, h); !equalBytes(got, want) {
		t.Errorf("a packed picture copied to %x, want %x", got, want)
	}
}

func TestCopyRGB32FlatPackedTooShort(t *testing.T) {
	const w, h = 3, 4
	alloc := make([]byte, w*h*4-1)
	if err := copyRGB32Flat(make([]byte, w*h*4), unsafe.Pointer(&alloc[0]), len(alloc), w, h, 0); err == nil {
		t.Fatal("a buffer too short to be the picture was copied")
	}
}

// TestCopyRGB32FlatBottomUp covers a flat buffer stored from its bottom row up:
// the memory it is in starts at the last displayed row, and a negative stride
// says so.
func TestCopyRGB32FlatBottomUp(t *testing.T) {
	const w, h = 3, 4
	const span = w*4 + 8
	alloc := make([]byte, span*h)
	for y := range h {
		rwRow(alloc[(h-1-y)*span:], w, y)
	}
	dst := make([]byte, w*h*4)
	if err := copyRGB32Flat(dst, unsafe.Pointer(&alloc[0]), len(alloc), w, h, -span); err != nil {
		t.Fatalf("copyRGB32Flat: %v", err)
	}
	if got, want := dst, rwPicture(w, h); !equalBytes(got, want) {
		t.Errorf("a bottom-up flat picture copied to %x, want %x", got, want)
	}
}

func TestCopyRGB32FlatTopDown(t *testing.T) {
	const w, h = 3, 4
	const span = w*4 + 8
	alloc := make([]byte, span*h)
	for y := range h {
		rwRow(alloc[y*span:], w, y)
	}
	dst := make([]byte, w*h*4)
	if err := copyRGB32Flat(dst, unsafe.Pointer(&alloc[0]), len(alloc), w, h, span); err != nil {
		t.Fatalf("copyRGB32Flat: %v", err)
	}
	if got, want := dst, rwPicture(w, h); !equalBytes(got, want) {
		t.Errorf("a flat picture copied to %x, want %x", got, want)
	}
}

func TestCopyRGB32FlatTooShort(t *testing.T) {
	const w, h = 3, 4
	const span = w*4 + 8
	alloc := make([]byte, span*h-1)
	if err := copyRGB32Flat(make([]byte, w*h*4), unsafe.Pointer(&alloc[0]), len(alloc), w, h, span); err == nil {
		t.Fatal("a flat buffer too short to be the picture was copied")
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
