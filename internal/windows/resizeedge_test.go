//go:build windows && (amd64 || arm64)

package windows

import "testing"

// TestTopEdgeHit checks the codes the top edge of a window without a
// caption answers: the corners near the ends it reaches, the top between,
// and no top-right corner under the window controls of a hidden title bar,
// which end the edge before the window's right.
func TestTopEdgeHit(t *testing.T) {
	const corner = 32 // 16 DIPs at 200%
	for _, c := range []struct {
		name            string
		x, edge, client int32
		want            uintptr
	}{
		{"frameless, left end", 0, 800, 800, htTopLeft},
		{"frameless, left corner", corner - 1, 800, 800, htTopLeft},
		{"frameless, middle", corner, 800, 800, htTop},
		{"frameless, before the right corner", 800 - corner - 1, 800, 800, htTop},
		{"frameless, right corner", 800 - corner, 800, 800, htTopRight},
		{"frameless, right end", 799, 800, 800, htTopRight},
		{"hidden title bar, left corner", 3, 524, 800, htTopLeft},
		{"hidden title bar, before the buttons", 523, 524, 800, htTop},
		{"hidden title bar without buttons, right corner", 790, 800, 800, htTopRight},
		{"narrower than two corners", 20, 30, 30, htTopLeft},
	} {
		if got := topEdgeHit(c.x, c.edge, c.client, corner); got != c.want {
			t.Errorf("%s: topEdgeHit(%d, %d, %d) = %d, want %d", c.name, c.x, c.edge, c.client, got, c.want)
		}
	}
}

// TestBottomEdgeHit checks the codes the bottom edge answers along the whole
// width of a window whose client area covers it: corners at both ends and
// the bottom between.
func TestBottomEdgeHit(t *testing.T) {
	const corner = 32 // 16 DIPs at 200%
	for _, c := range []struct {
		name   string
		x      int32
		client int32
		want   uintptr
	}{
		{"left end", 0, 800, htBottomLeft},
		{"left corner", corner - 1, 800, htBottomLeft},
		{"middle", corner, 800, htBottom},
		{"before the right corner", 800 - corner - 1, 800, htBottom},
		{"right corner", 800 - corner, 800, htBottomRight},
		{"right end", 799, 800, htBottomRight},
		{"narrower than two corners", 20, 30, htBottomLeft},
	} {
		if got := bottomEdgeHit(c.x, c.client, corner); got != c.want {
			t.Errorf("%s: bottomEdgeHit(%d, %d) = %d, want %d", c.name, c.x, c.client, got, c.want)
		}
	}
}

// TestSideEdgeHit checks the codes the left and right edges answer along
// the height of a window: corners at both ends and the side between.
func TestSideEdgeHit(t *testing.T) {
	const corner = 32 // 16 DIPs at 200%
	for _, c := range []struct {
		name   string
		y      int32
		client int32
		left   uintptr
		right  uintptr
	}{
		{"top end", 0, 600, htTopLeft, htTopRight},
		{"top corner", corner - 1, 600, htTopLeft, htTopRight},
		{"middle", corner, 600, htLeft, htRight},
		{"before the bottom corner", 600 - corner - 1, 600, htLeft, htRight},
		{"bottom corner", 600 - corner, 600, htBottomLeft, htBottomRight},
		{"bottom end", 599, 600, htBottomLeft, htBottomRight},
		{"shorter than two corners", 20, 30, htTopLeft, htTopRight},
	} {
		if got := leftEdgeHit(c.y, c.client, corner); got != c.left {
			t.Errorf("%s: leftEdgeHit(%d, %d) = %d, want %d", c.name, c.y, c.client, got, c.left)
		}
		if got := rightEdgeHit(c.y, c.client, corner); got != c.right {
			t.Errorf("%s: rightEdgeHit(%d, %d) = %d, want %d", c.name, c.y, c.client, got, c.right)
		}
	}
}
