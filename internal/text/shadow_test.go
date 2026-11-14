package text

import "testing"

// TestBlurBitmap blurs a square of coverage: it keeps its sum, spreads
// three standard deviations past it, symmetric, and shades a sharp
// shadow of a color glyph by its alpha.
func TestBlurBitmap(t *testing.T) {
	b := bitmap{left: 2, top: -8, w: 4, h: 4, pix: make([]byte, 16)}
	for i := range b.pix {
		b.pix[i] = 255
	}
	sharp := blurBitmap(b, 0)
	if sharp.w != 4 || sharp.h != 4 || sharp.left != 2 || sharp.top != -8 || sharp.pix[5] != 255 {
		t.Fatalf("unblurred: %dx%d at (%d, %d)", sharp.w, sharp.h, sharp.left, sharp.top)
	}
	soft := blurBitmap(b, 2)
	pad := (soft.w - 4) / 2
	if pad != 7 || soft.h != soft.w || soft.left != 2-pad || soft.top != -8-pad {
		t.Fatalf("blurred: %dx%d at (%d, %d)", soft.w, soft.h, soft.left, soft.top)
	}
	sum := 0
	for _, v := range soft.pix {
		sum += int(v)
	}
	if want := 16 * 255; sum < want*98/100 || sum > want*102/100 {
		t.Errorf("the blur sums %d, the square %d", sum, want)
	}
	center := soft.pix[(soft.h/2)*soft.w+soft.w/2]
	edge := soft.pix[(soft.h/2)*soft.w]
	if center <= edge || center == 255 || soft.pix[(soft.h/2)*soft.w+1] != soft.pix[(soft.h/2)*soft.w+soft.w-2] {
		t.Errorf("the blur shades the center %d, the edge %d, unevenly", center, edge)
	}
	emoji := bitmap{w: 1, h: 1, color: true, pix: []byte{200, 10, 10, 128}}
	if s := blurBitmap(emoji, 0); s.color || s.pix[0] != 128 {
		t.Errorf("a color glyph's shadow is %v", s.pix)
	}
}

// TestBlurredColors blurs a color glyph in its colors, premultiplied and
// as wide as its shadow, and a mask glyph as its shadow.
func TestBlurredColors(t *testing.T) {
	emoji := bitmap{left: 1, top: -3, w: 3, h: 3, color: true, pix: make([]byte, 36)}
	for i := 0; i < len(emoji.pix); i += 4 {
		copy(emoji.pix[i:], []byte{200, 10, 0, 255})
	}
	soft := blurred(emoji, 1.5, true)
	shadow := blurBitmap(emoji, 1.5)
	if !soft.color || soft.w != shadow.w || soft.h != shadow.h || soft.left != shadow.left || soft.top != shadow.top || len(soft.pix) != 4*soft.w*soft.h {
		t.Fatalf("blurred %dx%d at (%d, %d), color %v; its shadow %dx%d at (%d, %d)", soft.w, soft.h, soft.left, soft.top, soft.color, shadow.w, shadow.h, shadow.left, shadow.top)
	}
	for i := 0; i < len(soft.pix); i += 4 {
		r, g, b, a := soft.pix[i], soft.pix[i+1], soft.pix[i+2], soft.pix[i+3]
		if r > a || g > a || b > a || a != shadow.pix[i/4] {
			t.Fatalf("pixel %d is %v, its shadow %d", i/4, soft.pix[i:i+4], shadow.pix[i/4])
		}
		if a > 32 && (r < 4*g || b != 0) {
			t.Fatalf("pixel %d is no longer red: %v", i/4, soft.pix[i:i+4])
		}
	}
	mask := bitmap{w: 2, h: 2, pix: []byte{255, 255, 255, 255}}
	m, s := blurred(mask, 2, true), blurBitmap(mask, 2)
	if m.color || string(m.pix) != string(s.pix) {
		t.Error("a mask glyph blurs otherwise than its shadow")
	}
}
