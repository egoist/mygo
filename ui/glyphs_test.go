package ui

import (
	"testing"
	"unicode"
)

// TestShapeVisualOrder checks that Shape gives glyphs from the left on
// every platform, those of a right-to-left run marked RTL, their clusters
// descending.
func TestShapeVisualOrder(t *testing.T) {
	s := []rune("中文 hello שלום，مرحبا world。")
	glyphs := Shape(string(s), Font{Size: 17})
	rtl := 0
	for i, g := range glyphs {
		if r := s[g.Cluster]; unicode.In(r, unicode.Hebrew, unicode.Arabic) != g.RTL && unicode.IsLetter(r) {
			t.Errorf("glyph %d of %q has RTL %v", i, r, g.RTL)
		}
		if g.RTL {
			rtl++
		}
		if i == 0 {
			continue
		}
		prev := glyphs[i-1]
		if g.X < prev.X-0.01 {
			t.Errorf("glyph %d (cluster %d) at %v, left of cluster %d at %v", i, g.Cluster, g.X, prev.Cluster, prev.X)
		}
		if g.RTL && prev.RTL && g.Cluster > prev.Cluster {
			t.Errorf("right to left, cluster %d follows cluster %d", g.Cluster, prev.Cluster)
		}
	}
	if rtl == 0 {
		t.Error("no glyph is right to left")
	}
}
