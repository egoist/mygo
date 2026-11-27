package editor

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/text"
	"github.com/egoist/mygo/ui"
)

// recordShapes records the texts the editors shape until the test ends.
func recordShapes(t *testing.T) *[]string {
	var shaped []string
	useShaper(t, func(sys *text.System, p text.Params) *text.Layout {
		shaped = append(shaped, p.Text)
		return sys.Shape(p)
	})
	return &shaped
}

// numberedLines returns n lines, each its number followed by tail.
func numberedLines(n int, tail string) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d %s", i, tail)
	}
	return strings.Join(lines, "\n")
}

// An edit lays the lines in view out anew, which shaped every one of them
// again, as the frames that painted them since did not ask for their
// shapes: a key typed in a tall editor shaped dozens of lines. What the
// edit left as it was keeps its shapes.
func TestAnEditShapesOnlyTheLinesItChanges(t *testing.T) {
	lines := numberedLines(60, "of the text")
	for _, tc := range []struct {
		name string
		lang string
		text string
		wrap bool
		edit func(tt *ui.Tester)
	}{
		{"typed", "", "|" + lines, false, func(tt *ui.Tester) { tt.Type("x") }},
		{"far down", "", strings.Replace(numberedLines(5000, "of the text"), "line 2500 ", "line 2500 |", 1), false, func(tt *ui.Tester) { tt.Type("x") }},
		{"new line", "", strings.Replace(lines, "line 1 of", "line 1 of|", 1), false, func(tt *ui.Tester) { tt.Key(0, ui.KeyEnter) }},
		{"joined lines", "", strings.Replace(lines, "line 1", "|line 1", 1), false, func(tt *ui.Tester) { tt.Key(0, ui.KeyBackspace) }},
		{"wrapped", "", "|" + numberedLines(30, strings.Repeat("a long line wrapped in rows ", 6)), true, func(tt *ui.Tester) { tt.Type("x") }},
		{"highlighted", "go", "package p\n\nfunc f() {\n\t|" + strings.Repeat("x := g(1, \"s\") // a call\n\t", 50) + "\n}", false, func(tt *ui.Tester) { tt.Type("y") }},
		{"composed", "", "|" + lines, false, func(tt *ui.Tester) { tt.Compose("に", 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shaped := recordShapes(t)
			ct := newEditorTest(t, tc.lang, tc.text)
			ct.st.SoftWrap = tc.wrap
			ct.tt.SetSize(600, 1400)
			ct.st.GoTo(ct.st.CursorPos())
			// Frames enough for the shapes of the first to have gone from
			// the shape cache, as a while after the lines came into view.
			for range 4 {
				ct.tt.Frame()
			}
			before := map[string]bool{}
			for _, s := range *shaped {
				before[s] = true
			}
			*shaped = nil
			tc.edit(ct.tt)
			ct.tt.Frame()
			ct.tt.Frame()
			for _, s := range *shaped {
				if before[s] {
					t.Fatalf("shaped %q again, which the edit left as it was (%d texts shaped: %q)", s, len(*shaped), *shaped)
				}
			}
			if len(*shaped) == 0 {
				t.Error("the edited lines were not shaped")
			}
		})
	}
}

// shownGlyphs returns the glyphs of the pieces of the lines in view.
func shownGlyphs(ct *editorTest) []string {
	v := &ct.st.ed.view
	first, last := ct.st.VisibleLines()
	var out []string
	for n := first; n <= last && n < v.text.LineCount(); n++ {
		l := v.line(n)
		for ri := range l.rows {
			r := &l.rows[ri]
			for pi := range r.pieces {
				p := &r.pieces[pi]
				lay := v.piece(l, p)
				for _, ln := range lay.Lines {
					out = append(out, fmt.Sprintf("%d.%d.%d %q %v %v %+v", n, ri, pi, l.disp[p.from:p.to], p.x, p.width, ln.Glyphs))
				}
			}
		}
	}
	return out
}

// The lines that keep their shapes through edits, and through what else
// changes how lines show, look as the same lines shaped anew.
func TestKeptShapesAreAsShapedAnew(t *testing.T) {
	theme := DefaultTheme(ui.LightTheme())
	theme.Tokens[TokenComment].Italic = true
	theme.Tokens[TokenKeyword].Bold = true
	ct := newEditorTest(t, "go", "package p\n\nfunc f() {\n|"+strings.Repeat("\tx := g(1, \"s\") // a call of g, long enough to be wrapped when the editor is narrow\n", 60)+"}")
	ct.st.Theme, ct.st.NoAutoClose = theme, true
	ct.tt.SetSize(600, 1400)
	steps := []func(){
		func() { ct.tt.Type("var y = 2") },
		func() { ct.tt.Key(0, ui.KeyEnter) },
		func() { ct.st.Insert("one\ntwo\nthree\n") },
		func() { ct.tt.Key(0, ui.KeyBackspace) },
		func() { ct.tt.Compose("に", 1) },
		func() { ct.tt.Compose("", 0) },
		func() { ct.tt.Type("/*") },
		func() { ct.tt.Type("*/") },
		func() { ct.st.SoftWrap = true },
		func() { ct.tt.SetSize(400, 1400) },
		func() { ct.st.SetInlays([]Inlay{{Offset: ct.st.Buffer.Offset(Pos{Line: 8, Col: 3}), Text: ": int"}}) },
		func() { ct.st.Fold(2) },
		func() { ct.st.Unfold(2) },
		func() { ct.st.SetLineStyles([]LineStyle{{Line: 6, Font: ui.Font{Size: 20}}}) },
		func() { ct.tt.Type("w") },
		func() { ct.st.Font.Size = 15 },
		func() { ct.st.Font.Features = "-liga" },
		func() { ct.st.Font.Family = "serif" },
		func() { theme.Tokens[TokenComment].Italic = false },
		func() { ct.st.SoftWrap = false },
		func() { ct.tt.Type("z") },
	}
	for i, step := range steps {
		for range 3 {
			ct.tt.Frame()
		}
		step()
		ct.tt.Frame()
		kept := shownGlyphs(ct)
		v := &ct.st.ed.view
		v.lines.m, v.shaped.prev, v.shaped.cur = nil, nil, nil
		for _, child := range v.styleViews {
			child.lines.m, child.shaped.prev, child.shaped.cur = nil, nil, nil
		}
		anew := shownGlyphs(ct)
		if !reflect.DeepEqual(kept, anew) {
			t.Fatalf("after step %d the lines shown differ from those shaped anew:\n%s\nwant\n%s", i, strings.Join(kept, "\n"), strings.Join(anew, "\n"))
		}
	}
}

// A frame keeps the shapes of the rows it shows, not those of every row
// of the lines it shows: a line soft wrapped in thousands of rows, scrolled
// through, keeps a frame's worth.
func TestAFrameKeepsTheShapesOfTheRowsItShows(t *testing.T) {
	var long strings.Builder
	for i := range 30000 {
		fmt.Fprintf(&long, "word%d ", i)
	}
	ct := newEditorTest(t, "", "|"+long.String())
	ct.st.SoftWrap = true
	ct.tt.Frame()
	v := &ct.st.ed.view
	for y := 0.0; y < ct.st.LineTop(1); y += ct.st.ViewportHeight() / 2 {
		ct.st.SetScrollY(y)
		ct.tt.Frame()
	}
	ct.tt.Frame()
	rows := len(v.line(0).rows)
	shown := int(ct.st.ViewportHeight()/float64(v.lineHeight)) + 1
	// prev holds what the last frame kept: its rows, and the gutter's number.
	if kept := len(v.shaped.prev); kept > 2*shown || rows < 50*shown {
		t.Errorf("a frame showing %d rows of %d kept %d shapes", shown, rows, kept)
	}
}
