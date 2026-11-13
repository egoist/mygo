package text

import (
	"reflect"
	"strings"
	"testing"
)

// A text laid out at width after width from its last layout, as a box being
// resized does, has the lines it would have laid out anew, those it copied
// from the last included.
func TestLayoutFromReusesLines(t *testing.T) {
	words := strings.Fields("window button layout render scroll update shadow cursor memory thread signal bitmap stroke column editor kernel")
	var b strings.Builder
	for j := range 160 {
		if j > 0 {
			b.WriteByte(' ')
			if j%53 == 0 {
				b.WriteByte('\n')
			}
		}
		b.WriteString(words[(j*j*3+j)%len(words)])
	}
	text := b.String()
	for _, p := range []Params{
		{Text: text, Style: Style{Size: 13}},
		{Text: text, Style: Style{Size: 15, Weight: 700}, Align: Left},
		{Text: text, Style: Style{Size: 13}, Align: Center},
		{Text: text, Style: Style{Size: 13}, Indent: 20},
		{Text: text, Style: Style{Size: 13, LineHeight: 1.6}, Balance: true},
	} {
		s, fresh := newSystem(), newSystem()
		var last *Layout
		copied := 0
		for w := float32(420); w > 180; w -= 3 {
			p.Width = w
			got := s.LayoutFrom(last, p)
			want := fresh.Shape(p)
			if len(got.Lines) != len(want.Lines) || got.Width != want.Width || got.Height != want.Height {
				t.Fatalf("%+v at %v: %d lines %vx%v, laid out anew %d lines %vx%v", p.Style, w, len(got.Lines), got.Width, got.Height, len(want.Lines), want.Width, want.Height)
			}
			for i := range got.Lines {
				g, h := got.Lines[i], want.Lines[i]
				g.carets, g.stops, g.upstream, h.carets, h.stops, h.upstream = nil, nil, nil, nil, nil, nil
				if !sameGlyphs(g.Glyphs, h.Glyphs) {
					t.Fatalf("%+v at %v: line %d's glyphs differ from those laid out anew", p.Style, w, i)
				}
				g.Glyphs, h.Glyphs = nil, nil
				if !reflect.DeepEqual(g, h) {
					t.Fatalf("%+v at %v: line %d is %+v, laid out anew %+v", p.Style, w, i, g, h)
				}
				if last != nil && i < len(last.Lines) && len(got.Lines[i].Glyphs) > 0 && last.Lines[i].Start == got.Lines[i].Start && last.Lines[i].End == got.Lines[i].End {
					copied++
				}
			}
			last = got
			s.EndFrame()
			fresh.EndFrame()
		}
		if copied == 0 {
			t.Errorf("%+v: no line was as in the layout before", p)
		}
	}
}

// sameGlyphs reports whether glyphs of two layouts of one system are alike,
// their fonts compared by their native fonts and sizes.
func sameGlyphs(a, b []Glyph) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		g, h := a[i], b[i]
		if (g.Font == nil) != (h.Font == nil) || g.Font != nil && (g.Font.native != h.Font.native || g.Font.Size != h.Font.Size) {
			return false
		}
		g.Font, h.Font = nil, nil
		if g != h {
			return false
		}
	}
	return true
}
