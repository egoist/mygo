package ui

import "testing"

// FixedLineHeight is a line's exact height: a font taller than it, a
// larger span's as a fallback's, doesn't grow the line, while LineHeight
// stays a minimum (#2016).
func TestFixedLineHeightIsExact(t *testing.T) {
	b := boxes(t, func(c *context) {
		coreColumn(c).AlignItems(Start).Children(func() {
			coreText(c, "one\ntwo").FontSize(15).FixedLineHeight(12).Label("small")
			coreRichText(c, Span{Text: "a"}, Span{Text: "B", Size: 40}).FontSize(15).FixedLineHeight(20).Label("tall")
			coreRichText(c, Span{Text: "a"}, Span{Text: "B", Size: 40}).FontSize(15).LineHeight(20.0 / 15).Label("minimum")
		})
	}, 300, 300, "small", "tall", "minimum")
	if b["small"].H != 24 {
		t.Errorf("two lines 12 high in a 15 DIP font take %v", b["small"].H)
	}
	if b["tall"].H != 20 {
		t.Errorf("a line 20 high with a 40 DIP span takes %v", b["tall"].H)
	}
	if b["minimum"].H <= 40 {
		t.Errorf("LineHeight is a minimum, but a line with a 40 DIP span takes %v", b["minimum"].H)
	}
}
