package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

const kana = "あいうえおかきく" // eight kana, each an em down a column

// TestWritingMode checks CSS's writing-mode: vertical-rl on a text: its
// kana stand an em apart down a column as long as they take, without a
// height; with one, the columns wrap at it, stacked from the right, and
// the text is as wide as they are.
func TestWritingMode(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreRow(c).Children(func() {
			coreText(c, kana).FontSize(10).WritingMode(VerticalRL).Label("one")
			coreText(c, kana).FontSize(10).Height(30).WritingMode(VerticalRL).Label("three").Selectable()
			coreText(c, kana).FontSize(10).Label("across")
		})
		coreColumn(c).Children(func() {
			coreText(c, kana).FontSize(10).WritingMode(VerticalRL).Label("tall")
		})
	}, 400, 300)
	rect := func(l string) Rect {
		r, ok := tt.Find(l)
		if !ok {
			t.Fatalf("no element %q", l)
		}
		return r
	}
	one, three, across := rect("one"), rect("three"), rect("across")
	if one.W <= 10 || one.W > 20 {
		t.Errorf("a column of 10 DIP kana is %v wide", one.W)
	}
	if three.W < 3*one.W-0.5 || three.W > 3*one.W+0.5 || three.H != 30 {
		t.Errorf("three columns are %v×%v, one %v wide", three.W, three.H, one.W)
	}
	// Across, the kana keep the font's own advances, which differ between
	// the fonts each system falls back to, but set eight in a row.
	if across.W < 4*one.W {
		t.Errorf("the kana across are %v wide, a column %v", across.W, one.W)
	}
	if h := rect("tall").H; h != 80 {
		t.Errorf("a column of eight kana is %v long, want 80", h)
	}
	// Its columns from the right: the first kana at the top right.
	var l *text.Layout
	for _, s := range tt.rt.texts {
		l = s.editor.layout
	}
	if l == nil || len(l.Lines) != 3 {
		t.Fatal("no layout of three columns")
	}
	first := l.PhysicalRect(text.Rect{X: 0, Y: l.Lines[0].Y, W: 30, H: l.Lines[0].Height})
	if w, _ := l.Size(); first.X+first.W != w || first.Y != 0 {
		t.Errorf("the first column is at %+v of a text %v wide", first, w)
	}
}

// TestVerticalGlyphs checks how vertical text paints: each column turned
// a quarter clockwise, what stands up in it (UAX #50's U, as kana, han and
// ©) turned back, its Latin left turned with it, as text-orientation:
// mixed sets them. It sets ©, which every system's Latin fonts have, as
// not every test machine has a font for han.
func TestVerticalGlyphs(t *testing.T) {
	black := RGB(0, 0, 0)
	tt := coreNewTester(func(c *context) {
		coreText(c, "©A").FontSize(20).TextColor(black).WritingMode(VerticalRL)
	}, 200, 200)
	var up, side int
	for _, op := range tt.h.last.Ops {
		if op.Kind != scene.OpGlyphs {
			continue
		}
		if m := op.Transform; !m.Set || m.A != 0 || m.B != 1 || m.C != -1 || m.D != 0 {
			t.Errorf("a column's glyphs paint with %+v, not turned a quarter clockwise", m)
		}
		for _, g := range tt.h.last.Glyphs[op.Start:op.End] {
			if g.Color != black.scene() {
				continue
			}
			switch m := g.Transform; {
			case m.Set && m.A == 0 && m.B == -1 && m.C == 1 && m.D == 0:
				up++
			case !m.Set:
				side++
			}
		}
	}
	if up != 1 || side != 1 {
		t.Errorf("%d glyphs standing up, %d turned, want one of each", up, side)
	}
}

// TestVerticalSelection checks that selectable vertical text selects what
// a drag down its column crosses, and that the arrows move along the
// column up and down.
func TestVerticalSelection(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreText(c, kana).FontSize(20).WritingMode(VerticalRL).Selectable()
	}, 200, 300)
	at := func(i int) (float32, float32) {
		for _, s := range tt.rt.texts {
			if s.editor.source == kana {
				l := s.editor.layout
				x, y, h := l.Caret(i)
				r := l.PhysicalRect(text.Rect{X: x, Y: y, W: 0, H: h})
				return s.x + s.editor.originX + r.X + r.W/2, s.y + s.editor.originY + r.Y
			}
		}
		t.Fatal("no selectable text")
		return 0, 0
	}
	x0, y0 := at(1)
	x1, y1 := at(3)
	if x0 != x1 || y1 <= y0 {
		t.Fatalf("carets 1 and 3 at (%v, %v) and (%v, %v), not down one column", x0, y0, x1, y1)
	}
	tt.Press(x0, y0+1)
	tt.Move(x1, y1+1)
	tt.Release(x1, y1+1)
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); got != "いう" {
		t.Errorf("a drag down the column copied %q", got)
	}
	tt.Key(Shift, KeyDown)
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); got != "いうえ" {
		t.Errorf("Shift-Down extended the selection to %q", got)
	}
}

// TestVerticalInline checks that an element inside a vertical text takes
// its words' box down the column, where assistive technology and the
// pointer find it.
func TestVerticalInline(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreText(c, "あい").FontSize(20).WritingMode(VerticalRL).Label("para").Children(func() {
			coreText(c, "うえ").Label("inline")
		})
	}, 200, 200)
	para, _ := tt.Find("para")
	in, ok := tt.Find("inline")
	if !ok {
		t.Fatal("no inline element")
	}
	if in.Y < para.Y+39 || in.H < 39 || in.H > 41 || in.W > para.W+0.5 || in.X < para.X-0.5 {
		t.Errorf("the inline element's box is %+v in its paragraph's %+v", in, para)
	}
}

// TestTextShapeVertical checks a TextLayout set in columns: its size,
// carets and selection where it shows.
func TestTextShapeVertical(t *testing.T) {
	l := ShapeRichTextWith([]Span{{Text: kana}}, Font{Size: 10}, TextShape{Width: 40, WritingMode: VerticalRL})
	w, h := l.Size()
	lines := l.Lines()
	if len(lines) != 2 || h != 40 || w < 2*lines[0].Rect.W-0.5 {
		t.Fatalf("%d columns, %v×%v", len(lines), w, h)
	}
	if r := lines[0].Rect; r.X+r.W != w || r.H != 40 {
		t.Errorf("the first column is at %+v of a layout %v wide", r, w)
	}
	c := l.Caret(1)
	if c.H != 1 || c.Y != 10 || c.W != lines[0].Rect.W {
		t.Errorf("the caret after the first kana is %+v", c)
	}
	if i := l.IndexAt(Point{lines[1].Rect.X + 2, 15}); i != 5 && i != 6 {
		t.Errorf("a point in the second column, by the second kana, is at %d", i)
	}
	if rs := l.SelectionRects(TextInputRange{Start: 4, End: 6}); len(rs) != 1 || rs[0].X != lines[1].Rect.X || rs[0].H != 20 {
		t.Errorf("selection %+v", rs)
	}
}

// TestVerticalColumnKeys checks that left and right move across the
// columns of vertical-rl text: left to the next column, beside where the
// caret was.
func TestVerticalColumnKeys(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreText(c, kana).FontSize(20).Height(60).WritingMode(VerticalRL).Selectable()
	}, 200, 200)
	var x, y float32
	for _, s := range tt.rt.texts {
		l := s.editor.layout
		cx, cy, h := l.Caret(1)
		r := l.PhysicalRect(text.Rect{X: cx, Y: cy, W: 0, H: h})
		x, y = s.x+s.editor.originX+r.X+r.W/2, s.y+s.editor.originY+r.Y+1
	}
	tt.Press(x, y)
	tt.Release(x, y)
	tt.Key(Shift, KeyLeft)
	tt.Key(Cmd, KeyC)
	if got := tt.Clipboard(); got != "いうえ" {
		t.Errorf("Shift-Left selected %q, want the runes to the next column", got)
	}
}
