package terminal

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestGridRowAsView: a Grid given the cells a Terminal holds makes the
// row View makes of them: backgrounds, runs, lines, drawn characters and
// wide characters. A row of the same cells keeps what it made.
func TestGridRowAsView(t *testing.T) {
	theme := DarkTheme()
	term, err := New(Options{Conn: newPipe(), Theme: theme})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 400, 200)
	term.Feed([]byte("\x1b[1mab\x1b[0m 中─\x1b[4mu\x1b[0m\x1b[48;2;200;0;0m x\x1b[0m\x1b[9ms\x1b[0mé👍█"))
	tt.Frame()
	term.mu.Lock()
	want := term.v.lines[0]
	term.mu.Unlock()

	fg, red := theme.Foreground, ui.RGB(200, 0, 0)
	cells := []Cell{
		{Text: "a", FG: fg, Bold: true}, {Text: "b", FG: fg, Bold: true}, {Text: " ", FG: fg},
		{Text: "中", Wide: true, FG: fg}, {}, {Text: "─", FG: fg}, {Text: "u", FG: fg, Underline: true},
		{Text: " ", FG: fg, BG: red, HasBG: true}, {Text: "x", FG: fg, BG: red, HasBG: true},
		{Text: "s", FG: fg, Strikethrough: true}, {Text: "é", FG: fg}, {Text: "👍", Wide: true, FG: fg}, {}, {Text: "█", FG: fg},
	}
	g := NewGrid(theme)
	g.SetFont(Font{}, 1)
	g.Resize(1)
	g.SetRow(0, cells, 0, 0)
	got := g.v.lines[0]
	type row struct {
		bgs   []bgSpan
		runs  []glyphRun
		decos []deco
		boxes []boxCell
		wide  []int16
	}
	pick := func(r rowCache) row { return row{r.bgs, r.runs, r.decos, r.boxes, r.wide} }
	if len(want.runs) < 2 || len(want.boxes) != 2 || len(want.wide) != 2 || len(want.decos) != 2 || len(want.bgs) != 1 {
		t.Fatalf("the view's row is not the one this test means: %+v", pick(want))
	}
	if w, gr := pick(want), pick(got); !reflect.DeepEqual(w, gr) {
		t.Fatalf("grid row\n%+v\nview row\n%+v", gr, w)
	}
	runs := &g.v.lines[0].runs[0]
	g.SetRow(0, cells, 1, 3)
	if line := g.v.lines[0]; &line.runs[0] != runs || !line.sel || line.selA != 1 || line.selB != 2 {
		t.Fatalf("same cells: rebuilt %v, selection %v %d..%d", &line.runs[0] != runs, line.sel, line.selA, line.selB)
	}
	if w, h := CellSize(Font{}, 1); w != g.v.cellW || h != g.v.cellH || w != term.v.cellW || h != term.v.cellH {
		t.Fatalf("CellSize %dx%d, grid %dx%d, view %dx%d", w, h, g.v.cellW, g.v.cellH, term.v.cellW, term.v.cellH)
	}
}

// TestGridCursorApart: Draw leaves the cursor out, and DrawCursor draws
// it alone, the character under a block in the cursor's text color.
func TestGridCursorApart(t *testing.T) {
	theme := DarkTheme()
	g := NewGrid(theme)
	g.Focused = true
	g.Cursor = GridCursor{X: 1, Visible: true}
	withCursor := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
			g.SetFont(Font{}, p.Scale())
			g.Resize(1)
			g.SetRow(0, []Cell{{Text: "a", FG: theme.Foreground}, {Text: "M", FG: theme.Foreground}}, 0, 0)
			g.Draw(p, r)
			if withCursor {
				g.DrawCursor(p, r)
			}
		})
	}, 200, 100)
	tt.Frame()
	cw, ch := float32(g.v.cellW), float32(g.v.cellH)
	at := func(x, y float32) ui.Color {
		c := tt.Image().RGBAAt(int(x), int(y))
		return ui.RGB(c.R, c.G, c.B)
	}
	if c := at(cw+1, 1); c != theme.Background {
		t.Fatalf("Draw drew %v at the cursor's corner, want the background %v", c, theme.Background)
	}
	withCursor = true
	tt.Frame()
	cur := theme.Cursor
	if cur.A == 0 {
		cur = theme.Foreground
	}
	if c := at(cw+1, 1); c != cur {
		t.Fatalf("DrawCursor drew %v at the cursor's corner, want %v", c, cur)
	}
	ink := false
	for x := cw; x < 2*cw; x++ {
		for y := float32(0); y < ch; y++ {
			ink = ink || at(x, y) != cur
		}
	}
	if !ink {
		t.Fatal("the block cursor hides the character under it")
	}
}
