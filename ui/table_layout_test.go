package ui

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestTablePinnedColumnsScrollAndHit(t *testing.T) {
	f := newCellFixture(30)
	f.cols[0].Pin = PinLeft
	f.cols[1].Width = 240
	f.cols[2].Pin, f.cols[2].Width = PinRight, 100
	tt := f.tester(360, 220)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	left, right := header(t, tt, "Name"), header(t, tt, "Note")
	tt.Scroll(180, 100, 200, 0)
	if got := header(t, tt, "Name"); got != left {
		t.Fatalf("left header moved: %+v -> %+v", left, got)
	}
	if got := header(t, tt, "Note"); got != right || got.X != 260 {
		t.Fatalf("right header moved: %+v -> %+v", right, got)
	}
	for _, title := range []string{"Name: name-0", "Count: 0", "Note: note-0"} {
		n := node(t, tt.h.access, platform.RoleCell, title)
		if n.Bounds.X < 0 || n.Bounds.X+n.Bounds.W > 360 {
			t.Fatalf("access bounds escape viewport: %+v", n)
		}
	}
	middle := node(t, tt.h.access, platform.RoleCell, "Count: 0")
	if middle.Bounds.X != 120 || middle.Bounds.W != 140 {
		t.Fatalf("scrolling cell's visible bounds: %+v", middle.Bounds)
	}
	// Hit exactly the portions seen, including where the scrolling cell's
	// full box sits behind a frozen one.
	y := float32(middle.Bounds.Y + middle.Bounds.H/2)
	for _, hit := range []struct {
		x   float32
		col int
	}{{20, 0}, {150, 1}, {290, 2}} {
		tt.ClickAt(hit.x, y)
		expectCellRange(t, f, 0, hit.col, 0, hit.col)
	}
	// Both frozen cells follow vertical scrolling.
	tt.Scroll(180, 150, 0, 250)
	if f.state.first == 0 || header(t, tt, "Name").Y != left.Y {
		t.Fatal("frozen columns did not scroll vertically with the rows")
	}
}

func TestTablePinnedCellPaintClips(t *testing.T) {
	cols := []TableColumn{{Title: "Left", Width: 100, Pin: PinLeft}, {Title: "Middle", Width: 300}, {Title: "Right", Width: 100, Pin: PinRight}}
	s := ListState{}
	colors := []Color{RGB(200, 20, 20), RGB(20, 200, 20), RGB(20, 20, 200)}
	tt := NewTester(func(c *Context) {
		Table(c, &s, cols, 1, func(row, col int) {
			Box(c).Grow(1).Height(40).Background(colors[col])
		}).Fill()
	}, 320, 140)
	tt.Scroll(180, 90, 500, 0)
	img := tt.Image()
	for _, sample := range []struct {
		x, col int
	}{{50, 0}, {150, 1}, {260, 2}} {
		got := img.RGBAAt(sample.x, 60)
		want := colors[sample.col]
		if got.R != want.R || got.G != want.G || got.B != want.B {
			t.Fatalf("at x=%d scrolling content covered pin: %+v, want %+v", sample.x, got, want)
		}
	}
}

func TestTablePinnedNavigationReveal(t *testing.T) {
	cols := []TableColumn{{Title: "Left", Width: 100, Pin: PinLeft}, {Title: "One", Width: 100}, {Title: "Two", Width: 100}, {Title: "Three", Width: 100}, {Title: "Right", Width: 100, Pin: PinRight}}
	cells := TableCellState{Value: func(row, col int) string { return cols[col].Title + " value" }}
	s := ListState{Cells: &cells}
	tt := NewTester(func(c *Context) {
		Table(c, &s, cols, 10, func(row, col int) { Text(c, cells.Value(row, col)).SingleLine() }).Fill()
	}, 350, 200)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tt.Click("Left value")
	tt.Key(0, KeyRight)
	tt.Key(0, KeyRight)
	tt.Key(0, KeyRight)
	n := node(t, tt.h.access, platform.RoleColumnHeader, "Three")
	if n.Bounds.X < 100 || n.Bounds.X+n.Bounds.W > 250 || n.Bounds.W != 100 {
		t.Fatalf("navigation revealed behind pinned columns: %+v", n.Bounds)
	}
	x := s.frame.e.st.scrollX
	tt.Key(0, KeyRight)
	if s.frame.e.st.scrollX != x || cells.Selection.Cursor.Column != "Right" {
		t.Fatal("revealing right pin changed horizontal scrolling")
	}
	tt.Key(0, KeyHome)
	if s.frame.e.st.scrollX != x || cells.Selection.Cursor.Column != "Left" {
		t.Fatal("revealing left pin changed horizontal scrolling")
	}
}

func TestTablePinnedResizeAndReorder(t *testing.T) {
	cols := []TableColumn{{Title: "A", Width: 100, Pin: PinLeft}, {Title: "B", Width: 100, Pin: PinLeft}, {Title: "C", Width: 200}, {Title: "D", Width: 90, Pin: PinRight, Fixed: true}}
	s := ListState{}
	tt := NewTester(func(c *Context) {
		Table(c, &s, cols, 2, func(row, col int) { Text(c, cols[col].Title+" value").SingleLine() }).Fill()
	}, 400, 200)
	b := header(t, tt, "B")
	x, y := float32(b.X+20), float32(b.Y+b.H/2)
	tt.Press(x, y)
	tt.Move(x-60, y)
	tt.Release(x-60, y)
	if order := s.Columns.arrange(cols); !reflect.DeepEqual(order, []int{1, 0, 2, 3}) {
		t.Fatalf("pinned columns did not reorder: %v", order)
	}
	// A scrolling column cannot cross into the pinned group.
	c := header(t, tt, "C")
	x = float32(c.X + 20)
	tt.Press(x, y)
	tt.Move(x-160, y)
	tt.Release(x-160, y)
	if order := s.Columns.arrange(cols); !reflect.DeepEqual(order, []int{1, 0, 2, 3}) {
		t.Fatalf("column crossed pin boundary: %v", order)
	}
	b = header(t, tt, "B")
	x = float32(b.X + b.W - 2)
	tt.Press(x, y)
	tt.Move(x+30, y)
	tt.Release(x+30, y)
	if w := header(t, tt, "B").W; w != 130 {
		t.Fatalf("pin was treated as fixed width: %v", w)
	}
	if header(t, tt, "D").X != 310 || header(t, tt, "D").W != 90 {
		t.Fatal("right pin moved when another pin resized")
	}
}

func TestTablePinsFillViewport(t *testing.T) {
	cols := []TableColumn{{Title: "Left", Width: 200, Pin: PinLeft}, {Title: "Middle", Width: 100}, {Title: "Right", Width: 200, Pin: PinRight}}
	s := ListState{}
	tt := NewTester(func(c *Context) {
		Table(c, &s, cols, 1, func(row, col int) { Text(c, cols[col].Title+" value") }).Fill()
	}, 300, 200)
	left, middle, right := header(t, tt, "Left"), header(t, tt, "Middle"), header(t, tt, "Right")
	if left.W != 200 || middle.W != 0 || right.X != 200 || right.W != 100 {
		t.Fatalf("overlapping pins: left %+v middle %+v right %+v", left, middle, right)
	}
}

func TestTableFixedOrderWithinPinGroup(t *testing.T) {
	cols := []TableColumn{{Title: "Scrolling"}, {Title: "Fixed", Pin: PinLeft, Fixed: true}, {Title: "Movable", Pin: PinLeft}, {Title: "Right", Pin: PinRight}}
	l := TableLayout{Order: []string{"Movable", "Scrolling", "Right", "Fixed"}}
	if order := l.arrange(cols); !reflect.DeepEqual(order, []int{1, 2, 0, 3}) {
		t.Fatalf("fixed column moved within its pin group: %v", order)
	}
}

func TestTableLegacyDuplicateColumnIDs(t *testing.T) {
	cols := []TableColumn{{Width: 100}, {Width: 100}, {Title: "Same", Width: 100}, {Title: "Same", Width: 100}}
	tt := NewTester(func(c *Context) {
		Table(c, nil, cols, 1, func(row, col int) { Text(c, []string{"a", "b", "c", "d"}[col]) }).Fill()
	}, 500, 200)
	for _, value := range []string{"a", "b", "c", "d"} {
		if !tt.HasText(value) {
			t.Fatal("legacy table lost its column", value)
		}
	}
}

func TestTableResizeBeforeFrame(t *testing.T) {
	f := newCellFixture(2)
	f.cols[0].Pin = PinLeft
	tt := f.tester(400, 200)
	h := header(t, tt, "Name")
	x, y := h.X+h.W-2, h.Y+h.H/2
	for _, ev := range []platform.SurfaceEvent{
		{Kind: platform.PointerDown, X: x, Y: y},
		{Kind: platform.PointerMove, X: x + 5, Y: y},
		{Kind: platform.PointerMove, X: x + 20, Y: y},
		{Kind: platform.PointerUp, X: x + 30, Y: y},
	} {
		tt.rt.event(ev)
	}
	tt.settle()
	if header(t, tt, "Name").W != 150 || f.state.Columns.Widths["name"] != 150 {
		t.Fatal("a resize completed between frames was lost or applied twice")
	}
}
