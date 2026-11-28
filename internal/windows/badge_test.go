//go:build windows && (amd64 || arm64)

package windows

import (
	"image"
	"testing"
)

// badgePixels draws a badge, its pixels 0xAARRGGBB.
func badgePixels(t *testing.T, text string, size int) []uint32 {
	t.Helper()
	bmp := newBitmap(0, int32(size), int32(size))
	if bmp == nil {
		t.Fatal("no bitmap")
	}
	defer bmp.free()
	drawBadge(bmp, text, int32(size))
	return append([]uint32(nil), bmp.px...)
}

func TestBadgeIcon(t *testing.T) {
	icon := badgeIcon("42", 32)
	if icon == 0 {
		t.Fatal("no icon")
	}
	procDestroyIcon.Call(icon)
}

func TestBadgeText(t *testing.T) {
	for label, want := range map[string]string{"7": "7", "42": "42", "99": "99", "100": "99+", "12345": "99+", "New": "New", "1.5k": "1.5k"} {
		if got := badgeText(label); got != want {
			t.Errorf("badgeText(%q) = %q, want %q", label, got, want)
		}
	}
}

// TestBadge checks that a badge is a disc, its text white on it and
// centered, at the sizes of small icons from 100% to 200%.
func TestBadge(t *testing.T) {
	for _, size := range []int{16, 20, 24, 32} {
		for _, text := range []string{"7", "42", "99+"} {
			px := badgePixels(t, text, size)
			at := func(x, y int) (a, r, g, b uint32) {
				p := px[y*size+x]
				return p >> 24, p >> 16 & 0xFF, p >> 8 & 0xFF, p & 0xFF
			}
			if a, _, _, _ := at(0, 0); a != 0 {
				t.Errorf("%q at %d: corner alpha %d", text, size, a)
			}
			if a, r, g, _ := at(1, size/2); a < 200 || r < 150 || g > 80 {
				t.Errorf("%q at %d: edge %#08x, not the red disc", text, size, px[size/2*size+1])
			}
			ink := image.Rectangle{}
			for y := range size {
				for x := range size {
					if a, _, g, b := at(x, y); a == 255 && g > 200 && b > 200 {
						ink = ink.Union(image.Rect(x, y, x+1, y+1))
					}
				}
			}
			if ink.Empty() {
				t.Errorf("%q at %d: no text", text, size)
				continue
			}
			// Twice the centers, to compare whole pixels.
			if dx, dy := ink.Min.X+ink.Max.X-size, ink.Min.Y+ink.Max.Y-size; dx < -2 || dx > 2 || dy < -2 || dy > 2 {
				t.Errorf("%q at %d: text at %v, off center by (%d, %d) half pixels", text, size, ink, dx, dy)
			}
		}
	}
}
