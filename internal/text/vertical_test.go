package text

import (
	"slices"
	"testing"
)

// verticalSystem returns a system laying text out with the stub engine,
// every rune half an em across, that sets lines in columns.
func verticalSystem() *System {
	s := newSystem()
	s.eng = &stubEngine{}
	s.UseVertical()
	return s
}

// TestVerticalColumns checks CSS's writing-mode: vertical-rl with the
// default text-orientation: mixed (CSS Writing Modes 4 §2 and §5.1): Han
// and kana stand up in cells of an em down the column, a column wraps at
// the width, which is its height, and the layout takes as much across as
// its columns do.
func TestVerticalColumns(t *testing.T) {
	s := verticalSystem()
	l := s.Layout(Params{Text: "あいうえおかきく", Style: Style{Size: 10}, Width: 30, Mode: VerticalRL})
	if !l.Vertical() || len(l.Lines) != 3 {
		t.Fatalf("vertical %v, %d columns", l.Vertical(), len(l.Lines))
	}
	for i, want := range [][2]int{{0, 3}, {3, 6}, {6, 8}} {
		if line := l.Lines[i]; line.Start != want[0] || line.End != want[1] {
			t.Errorf("column %d holds runes %d to %d, want %v", i, line.Start, line.End, want)
		}
	}
	for _, g := range l.Lines[0].Glyphs {
		if !g.Upright || g.Advance != 10 {
			t.Errorf("a kana %+v, want it upright, an em down the column", g)
		}
	}
	if g := l.Lines[0].Glyphs[1]; g.X != 10 {
		t.Errorf("the second kana's cell starts at %v, want 10", g.X)
	}
	if w, h := l.Size(); w != l.Height || h != 30 || l.Width != 30 {
		t.Errorf("the layout shows %v×%v, laid out %v×%v", w, h, l.Width, l.Height)
	}
	// The first column at the right.
	first := l.PhysicalRect(Rect{0, l.Lines[0].Y, 30, l.Lines[0].Height})
	if want := l.Height - l.Lines[0].Height; first.X != want || first.Y != 0 || first.W != l.Lines[0].Height || first.H != 30 {
		t.Errorf("the first column shows at %+v, want its left at %v", first, want)
	}
	// What shows at a point of the third kana maps to it.
	x, y := l.Logical(first.X+first.W/2, 25)
	if i := l.IndexAt(x, y); i != 2 && i != 3 {
		t.Errorf("a point on the third kana is at rune %d", i)
	}
	// Without UseVertical, the text is set across.
	plain := newSystem()
	plain.eng = &stubEngine{}
	if l := plain.Layout(Params{Text: "あい", Mode: VerticalRL}); l.Vertical() {
		t.Error("a system that never used vertical text set it in columns")
	}
}

// TestVerticalLR checks vertical-lr: the columns stack from the left,
// each turned as in vertical-rl, its top at the right.
func TestVerticalLR(t *testing.T) {
	s := verticalSystem()
	l := s.Layout(Params{Text: "あいうえ", Style: Style{Size: 10}, Width: 20, Mode: VerticalLR})
	if len(l.Lines) != 2 {
		t.Fatalf("%d columns", len(l.Lines))
	}
	for i, line := range l.Lines {
		r := l.PhysicalRect(Rect{0, line.Y, 20, line.Height})
		if r.X != line.Y || r.W != line.Height {
			t.Errorf("column %d shows at %+v, want from %v", i, r, line.Y)
		}
		// Within it, the line's top is at the right.
		top := l.PhysicalRect(Rect{0, line.Y, 20, 1})
		if top.X != line.Y+line.Height-1 {
			t.Errorf("the top of column %d shows at %v", i, top.X)
		}
		if x, y := l.Logical(top.X+0.5, 5); y < line.Y || y > line.Y+1 || x != 5 {
			t.Errorf("the top of column %d maps back to (%v, %v)", i, x, y)
		}
	}
}

// TestTextOrientation checks text-orientation: mixed turns Latin and
// digits sideways, their advances down the column, upright stands them
// up in cells of an em, sideways turns Han too, and a span's orientation
// overrides the text's.
func TestTextOrientation(t *testing.T) {
	s := verticalSystem()
	advances := func(p Params) (out []float32, up []bool) {
		l := s.Layout(p)
		for _, g := range l.Lines[0].Glyphs {
			out, up = append(out, g.Advance), append(up, g.Upright)
		}
		return out, up
	}
	p := Params{Text: "漢AB字", Style: Style{Size: 10}, Mode: VerticalRL}
	if a, up := advances(p); !slices.Equal(a, []float32{10, 5, 5, 10}) || !slices.Equal(up, []bool{true, false, false, true}) {
		t.Errorf("mixed: advances %v, upright %v", a, up)
	}
	p.Orientation = Upright
	if a, _ := advances(p); !slices.Equal(a, []float32{10, 10, 10, 10}) {
		t.Errorf("upright: advances %v", a)
	}
	p.Orientation = Sideways
	if a, _ := advances(p); !slices.Equal(a, []float32{5, 5, 5, 5}) {
		t.Errorf("sideways: advances %v", a)
	}
	p.Orientation = Mixed
	p.Spans = EncodeSpans([]Span{{End: 1}, {End: 3, Orientation: Upright}})
	if a, _ := advances(p); !slices.Equal(a, []float32{10, 10, 10, 10}) {
		t.Errorf("a span standing up Latin: advances %v", a)
	}
	if got := DecodeSpans(p.Spans); got[1].Orientation != Upright || got[0].Orientation != 0 {
		t.Errorf("spans decode as %+v", got)
	}
}

// TestUprightGlyphsCentered checks that an upright glyph's horizontal
// origin is half its advance across the column's middle, so that it is
// centered across the column, and that a sideways one's em box is
// centered on the middle.
func TestUprightGlyphsCentered(t *testing.T) {
	s := verticalSystem()
	l := s.Layout(Params{Text: "漢A", Style: Style{Size: 10}, Mode: VerticalRL})
	line := l.Lines[0]
	if mid := line.Y + line.Height/2; line.Baseline != mid {
		t.Errorf("baseline %v, the column's middle %v", line.Baseline, mid)
	}
	up, side := line.Glyphs[0], line.Glyphs[1]
	// The stub engine's glyphs are 5 across.
	if up.Y != line.Baseline+2.5 {
		t.Errorf("upright glyph's origin at %v, baseline %v", up.Y, line.Baseline)
	}
	if want := line.Baseline + (8-2)/2.0; side.Y != want {
		t.Errorf("sideways glyph's baseline at %v, want %v", side.Y, want)
	}
	if up.VerticalOrigin() != 10*emTop {
		t.Errorf("vertical origin %v", up.VerticalOrigin())
	}
}

// TestVerticalTruncation checks that MaxLines cuts the columns, ending
// the last with an ellipsis that fits its height.
func TestVerticalTruncation(t *testing.T) {
	s := verticalSystem()
	l := s.Layout(Params{Text: "あいうえおかきくけこ", Style: Style{Size: 10}, Width: 30, MaxLines: 2, Mode: VerticalRL})
	if !l.Truncated || len(l.Lines) != 2 {
		t.Fatalf("truncated %v, %d columns", l.Truncated, len(l.Lines))
	}
	last := l.Lines[1]
	if last.Width > 30 {
		t.Errorf("the last column is %v long, past 30", last.Width)
	}
	if g := last.Glyphs[len(last.Glyphs)-1]; g.Runes != 0 {
		t.Errorf("the last column ends with %+v, not the ellipsis", g)
	}
}

// TestVerticalDecorations checks that an underline of vertical text runs
// along its column, past the em boxes' line-under edge, and a
// strikethrough through the column's middle.
func TestVerticalDecorations(t *testing.T) {
	s := verticalSystem()
	l := s.Layout(Params{Text: "漢字", Style: Style{Size: 10}, Mode: VerticalRL})
	n := len(l.Lines[0].Glyphs)
	under := s.Decorate(l, 0, 0, n, 0, 0, 1, Underline, LineStyle{})
	strike := s.Decorate(l, 0, 0, n, 0, 0, 1, Strikethrough, LineStyle{})
	if len(under) != 1 || len(strike) != 1 {
		t.Fatalf("%d underlines, %d strikethroughs", len(under), len(strike))
	}
	base := l.Lines[0].Baseline
	if u := under[0]; u.X0 != 0 || u.X1 != 20 || u.Top < base+5 {
		t.Errorf("underline %+v, baseline %v", u, base)
	}
	if st := strike[0]; st.Top > base || st.Bottom < base {
		t.Errorf("strikethrough %+v, baseline %v", st, base)
	}
}

// TestUprightSpans checks that the runs standing up take the font's
// vertical forms (vert), with the features they have.
func TestUprightSpans(t *testing.T) {
	got := uprightSpans([]Span{{End: 2, Features: "tnum"}}, Style{Features: "liga=0"}, []bool{true, false, true, true})
	want := []Span{{End: 1, Features: "tnum,vert"}, {End: 2, Features: "tnum"}, {End: 4, Features: "liga=0,vert"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestStandsUp checks UAX #50's orientations: U and Tu stand up, R turns,
// and Tr stands up with the font's vertical forms or with a vertical
// presentation form of Unicode's, and turns otherwise.
func TestStandsUp(t *testing.T) {
	for _, c := range []struct {
		r           rune
		up, upForms bool
	}{
		{'漢', true, true}, {'あ', true, true}, {'、', true, true}, {'A', false, false},
		{'1', false, false}, {'ー', true, false}, {'「', true, true}, {'한', true, true},
	} {
		if got := standsUp(c.r, Mixed, false); got != c.up {
			t.Errorf("%q stands up %v with vert, want %v", c.r, got, c.up)
		}
		if got := standsUp(c.r, Mixed, true); got != c.upForms {
			t.Errorf("%q stands up %v without features, want %v", c.r, got, c.upForms)
		}
	}
}
