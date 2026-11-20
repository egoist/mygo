package text

import "testing"

// An exact line keeps its height when a run is in a taller font, as a
// fallback's, and keeps its baseline where the style's own font puts it;
// a minimum line grows to the taller font (#2016).
func TestExactLineHeight(t *testing.T) {
	ui := &Font{Size: 15, Ascent: 16.19, Descent: 3.76}
	tall := &Font{Size: 15, Ascent: 28.56, Descent: 8.94} // Noto Nastaliq Urdu
	text := []rune("ab")
	sl := shapedLine{end: 2, runs: []shapedRun{
		{font: ui, end: 1, glyphs: []Glyph{{Font: ui, Advance: 8, Cluster: 0}}},
		{font: tall, start: 1, end: 2, glyphs: []Glyph{{Font: tall, X: 8, Advance: 8, Cluster: 1}}},
	}}
	lay := func(style Style, spans []Span) Line {
		var y float32
		var buf []int
		return line(Params{Style: style}, text, sl, 0, false, metricsOf(ui, style), &y, nil, &buf, spans)
	}
	near := func(a, b float32) bool { return a-b < 1e-3 && b-a < 1e-3 }

	exact := Style{Size: 15, LineHeight: 20.0 / 15, ExactLineHeight: true}
	l := lay(exact, nil)
	if !near(l.Height, 20) {
		t.Errorf("an exact line 20 high with a taller fallback is %v high", l.Height)
	}
	if want := (20-16.19-3.76)/2 + 16.19; !near(l.Baseline, float32(want)) {
		t.Errorf("exact line's baseline at %v, want %v from the style's font", l.Baseline, want)
	}

	// Smaller than the font's own ascent and descent, as CSS's
	// line-height: 15px: the line is still that high.
	small := Style{Size: 15, LineHeight: 1, ExactLineHeight: true}
	if l := lay(small, nil); !near(l.Height, 15) {
		t.Errorf("an exact line 15 high is %v high", l.Height)
	}

	// An inline object still grows an exact line, by what it adds.
	obj := []Span{{End: 1}, {End: 2, ObjectWidth: 8, ObjectHeight: 26.19}}
	if l := lay(exact, obj); !near(l.Height, 30) {
		t.Errorf("an exact line 20 high with an object 10 above the ascent is %v high, want 30", l.Height)
	}

	minimum := Style{Size: 15, LineHeight: 20.0 / 15}
	if l := lay(minimum, nil); !near(l.Height, 37.5) {
		t.Errorf("a minimum line 20 high with a 37.5 fallback is %v high", l.Height)
	}
}
