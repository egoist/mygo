package ui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// box builds a w×h box named label, which Tester.Find finds.
func box(c *ui.Context, label string, w, h float32) ui.Element {
	return ui.Box(c).Size(w, h).Shrink(0).Label(label)
}

// at returns where the element named label is, failing the test without
// one.
func at(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no element named %q", label)
	}
	return r
}

// wantX checks the left edges of the elements named, in order.
func wantX(t *testing.T, tt *ui.Tester, xs map[string]float32) {
	t.Helper()
	for label, x := range xs {
		if r := at(t, tt, label); r.X != x {
			t.Errorf("%s at x %v, want %v", label, r.X, x)
		}
	}
}

// TestDirectionRow lays a row out from the right right to left, with its
// gap, its padding, Justify and automatic margins, and from the left again
// in reverse, as CSS's direction: rtl does.
func TestDirectionRow(t *testing.T) {
	justify := ui.Start
	reverse := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			row := ui.Row(c).Height(20).Gap(10).Padding(0, 4, 0, 8).Justify(justify)
			if reverse {
				row.Reverse()
			}
			row.Children(func() {
				box(c, "a", 30, 10)
				box(c, "b", 40, 10)
			})
			ui.Row(c).Height(20).Children(func() {
				box(c, "first", 10, 10)
				box(c, "pushed", 10, 10).MarginStart(ui.Auto)
			})
		})
	}, 300, 200)
	wantX(t, tt, map[string]float32{"a": 8, "b": 48, "first": 0, "pushed": 290})
	tt.SetDirection(ui.RightToLeft)
	// The padding stays physical: 4 on the right.
	wantX(t, tt, map[string]float32{"a": 300 - 4 - 30, "b": 300 - 4 - 30 - 10 - 40, "first": 290, "pushed": 0})
	justify = ui.End
	tt.Frame()
	wantX(t, tt, map[string]float32{"a": 8 + 40 + 10, "b": 8})
	justify, reverse = ui.Start, true
	tt.Frame()
	wantX(t, tt, map[string]float32{"a": 8, "b": 48})
}

// TestDirectionColumn aligns the children of a column across it from the
// right right to left, Start and End swapped, Center and Stretch alike.
func TestDirectionColumn(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Start).Children(func() {
			box(c, "start", 30, 10)
			box(c, "end", 30, 10).AlignSelf(ui.End)
			box(c, "center", 30, 10).AlignSelf(ui.Center)
			ui.Box(c).Height(10).AlignSelf(ui.Stretch).MarginStart(20).Label("stretch")
		})
	}, 300, 200)
	wantX(t, tt, map[string]float32{"start": 0, "end": 270, "center": 135, "stretch": 20})
	tt.SetDirection(ui.RightToLeft)
	wantX(t, tt, map[string]float32{"start": 270, "end": 0, "center": 135, "stretch": 0})
	if r := at(t, tt, "stretch"); r.W != 280 {
		t.Errorf("stretched over %v DIPs with a start margin of 20, want 280", r.W)
	}
}

// TestDirectionEdges puts the logical edges on the left or the right by
// the element's direction, and keeps the physical ones where they are.
func TestDirectionEdges(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).PaddingStart(10).PaddingEnd(30).Children(func() { box(c, "padded", 10, 10) })
			ui.Row(c).Padding(0, 0, 0, 7).Children(func() { box(c, "left", 10, 10) })
			ui.Row(c).BorderStartWidth(5).BorderColor(ui.RGB(0, 0, 0)).Children(func() { box(c, "bordered", 10, 10) })
			ui.Box(c).Height(20).Children(func() {
				box(c, "abs-start", 10, 10).Absolute().Start(6)
				box(c, "abs-end", 10, 10).Absolute().End(6)
				box(c, "abs-left", 10, 10).Absolute().Left(6)
				box(c, "abs-static", 10, 10).Absolute()
				box(c, "attached", 10, 10).Attach(ui.AnchorTopEnd, ui.AnchorTopEnd)
			})
		})
	}, 300, 200)
	wantX(t, tt, map[string]float32{"padded": 10, "left": 7, "bordered": 5, "abs-start": 6, "abs-end": 284, "abs-left": 6, "abs-static": 0, "attached": 290})
	tt.SetDirection(ui.RightToLeft)
	wantX(t, tt, map[string]float32{"padded": 300 - 10 - 10, "left": 300 - 10, "bordered": 300 - 5 - 10, "abs-start": 284, "abs-end": 6, "abs-left": 6, "abs-static": 290, "attached": 0})
}

// TestDirectionOverride lays a subtree out in its own direction, which
// the elements inside inherit, the window's direction around it.
func TestDirectionOverride(t *testing.T) {
	var dirs [3]ui.Direction
	tt := ui.NewTester(func(c *ui.Context) {
		dirs[0] = c.Direction()
		ui.Column(c).Fill().Children(func() {
			ui.Row(c).Children(func() { box(c, "window", 10, 10) })
			ui.Column(c).Direction(ui.LeftToRight).Children(func() {
				dirs[1] = c.Direction()
				ui.Row(c).Children(func() { box(c, "ltr", 10, 10) })
			})
			ui.Row(c).Children(func() {
				ui.Row(c).Direction(ui.RightToLeft).Width(100).Children(func() {
					dirs[2] = c.Direction()
					box(c, "rtl", 10, 10)
				})
			})
		})
	}, 300, 200)
	if dirs != [3]ui.Direction{ui.LeftToRight, ui.LeftToRight, ui.RightToLeft} {
		t.Errorf("left to right, the directions read %v", dirs)
	}
	wantX(t, tt, map[string]float32{"window": 0, "ltr": 0, "rtl": 90})
	tt.SetDirection(ui.RightToLeft)
	if dirs != [3]ui.Direction{ui.RightToLeft, ui.LeftToRight, ui.RightToLeft} {
		t.Errorf("right to left, the directions read %v", dirs)
	}
	wantX(t, tt, map[string]float32{"window": 290, "ltr": 0, "rtl": 290})
}

// TestDirectionLate gives an element its direction after its children
// and edges: they take it, and its logical edges move.
func TestDirectionLate(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).PaddingStart(20).Children(func() {
				box(c, "late", 10, 10)
			}).Direction(ui.RightToLeft)
		})
	}, 300, 200)
	wantX(t, tt, map[string]float32{"late": 270})
}

// TestDirectionGrid places a grid's columns from the right right to left,
// and items at the start of their cells.
func TestDirectionGrid(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Grid(c).Width(300).ColumnTracks(ui.Fixed(100), ui.Fixed(100), ui.Fixed(100)).JustifyItems(ui.Start).Children(func() {
			box(c, "one", 20, 10)
			box(c, "two", 20, 10)
			box(c, "wide", 20, 10).ColumnSpan(2).JustifySelf(ui.End)
		})
	}, 300, 200)
	wantX(t, tt, map[string]float32{"one": 0, "two": 100, "wide": 180})
	tt.SetDirection(ui.RightToLeft)
	wantX(t, tt, map[string]float32{"one": 280, "two": 180, "wide": 100})
}

// TestDirectionScroll scrolls sideways from the right right to left: the
// content starts at the right, the offset counts from there, and the wheel
// moves the content as it does left to right.
func TestDirectionScroll(t *testing.T) {
	var s ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.ScrollHorizontal(c).Size(200, 50).TrackScroll(&s).Children(func() {
			ui.Row(c).Children(func() {
				box(c, "first", 150, 10)
				box(c, "last", 150, 10)
			})
		})
		ui.ScrollHorizontal(c).Size(200, 50).Children(func() {
			box(c, "narrow", 50, 10)
		})
	}, 200, 200)
	// Find gives what shows of an element, within the scroll container.
	want := func(label string, x, w float32) {
		t.Helper()
		if r := at(t, tt, label); r.X != x || r.W != w {
			t.Errorf("%s shows over %v+%v, want %v+%v", label, r.X, r.W, x, w)
		}
	}
	want("first", 0, 150)
	want("last", 150, 50)
	want("narrow", 0, 50)
	tt.SetDirection(ui.RightToLeft)
	want("first", 50, 150)
	want("last", 0, 50)
	want("narrow", 150, 50)
	if s.X != 0 || s.MaxX != 100 {
		t.Errorf("at the start, the offset is %v of %v, want 0 of 100", s.X, s.MaxX)
	}
	// The wheel going left shows what is further left: the end.
	tt.Scroll(100, 25, -50, 0)
	if s.X != 50 {
		t.Errorf("scrolled 50 to the left, the offset is %v, want 50", s.X)
	}
	want("first", 100, 100)
	s.X = 100
	tt.Frame()
	want("last", 0, 150)
}
