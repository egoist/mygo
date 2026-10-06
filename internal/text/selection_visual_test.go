package text

import (
	"slices"
	"testing"
)

func TestSelectionVisualBidiGapAndLigature(t *testing.T) {
	// Logical abc + three RTL glyphs; the first RTL character is visually
	// farthest right. Selecting "c" and that character leaves a gap.
	l := Layout{Params: Params{Style: Style{Size: 15}}, Runes: []rune("abcאבג"), Lines: []Line{{Start: 0, End: 6, Width: 60, Height: 20, Glyphs: []Glyph{
		{Cluster: 0, Runes: 1, X: 0, Advance: 10},
		{Cluster: 1, Runes: 1, X: 10, Advance: 10},
		{Cluster: 2, Runes: 1, X: 20, Advance: 10},
		{Cluster: 5, Runes: 1, X: 30, Advance: 10, RTL: true},
		{Cluster: 4, Runes: 1, X: 40, Advance: 10, RTL: true},
		{Cluster: 3, Runes: 1, X: 50, Advance: 10, RTL: true},
	}}}}
	if got := l.SelectionVisual(2, 4, false); !slices.Equal(got, []Rect{{20, 0, 10, 20}, {50, 0, 10, 20}}) {
		t.Fatal("selection covered unselected RTL text", got)
	}
	l.Runes = []rune("ffi")
	l.Lines[0].End, l.Lines[0].Width = 3, 30
	l.Lines[0].Glyphs = []Glyph{{Cluster: 0, Runes: 3, X: 0, Advance: 30}}
	if got := l.SelectionVisual(1, 2, false); !slices.Equal(got, []Rect{{10, 0, 10, 20}}) {
		t.Fatal("ligature selection", got)
	}
}
