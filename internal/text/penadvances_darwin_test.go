package text

import (
	"math"
	"testing"
)

// TestPenAdvancesRightToLeft keeps a right-to-left line as wide as its
// glyphs where Core Text places some past the pen: the glyphs of مَّرَضٌ
// in Amiri Quran at 100, as Core Text lays them out, left to right. Its
// tanween starts the line at 57, its advance taking the pen back to 0,
// and its shadda sits 18 past the pen; HarfBuzz gives the word 203.1.
func TestPenAdvancesRightToLeft(t *testing.T) {
	glyphs := []Glyph{
		{X: 57.2, Advance: -57.2, RTL: true},
		{X: 0, Advance: 119.7, RTL: true},
		{X: 119.7, Advance: 1.5, RTL: true},
		{X: 121.2, Advance: 47.3, RTL: true},
		{X: 186.5, Advance: -20.2, RTL: true},
		{X: 166.3, Advance: 2.2, RTL: true},
		{X: 168.5, Advance: 34.6, RTL: true},
	}
	runs := []shapedRun{{glyphs: glyphs}}
	penAdvances(runs)
	var w float32
	for _, g := range glyphs {
		w += g.Advance
	}
	if math.Abs(float64(w)-203.1) > 0.05 {
		t.Errorf("the line is %.1f wide, want 203.1", w)
	}

	// Left to right, and right to left where the pen meets every glyph,
	// the advances are Core Text's.
	for _, rtl := range []bool{false, true} {
		plain := []Glyph{{X: 0, Advance: 10, RTL: rtl}, {X: 10, Advance: 12, RTL: rtl}, {X: 22, Advance: 8, RTL: rtl}}
		penAdvances([]shapedRun{{glyphs: plain}})
		for i, want := range []float32{10, 12, 8} {
			if plain[i].Advance != want {
				t.Errorf("rtl=%v: glyph %d advances %v, want %v", rtl, i, plain[i].Advance, want)
			}
		}
	}
}
