//go:build !js

package mygo

import (
	"testing"

	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/internal/platform"
)

// setDisplays gives the app the displays d until the test is over.
func setDisplays(t *testing.T, d ...platform.Display) {
	t.Helper()
	fake.Displays = d
	t.Cleanup(func() { fake.Displays = nil })
}

func testDisplay(id int64, label string, x, y, w, h int, primary bool) platform.Display {
	return platform.Display{
		ID: id, Label: label, Primary: primary, ScaleFactor: 2,
		Bounds:   platform.Rect{X: x, Y: y, Width: w, Height: h},
		WorkArea: platform.Rect{X: x, Y: y + 25, Width: w, Height: h - 25},
	}
}

// Screen tells of the displays as the backend lists them and of the mouse
// where it is.
func TestScreenDisplays(t *testing.T) {
	setDisplays(t,
		testDisplay(1, "Left", 0, 0, 1440, 900, true),
		testDisplay(2, "Right", 1440, 0, 1280, 800, false),
	)
	ds := Screen.Displays()
	if len(ds) != 2 || ds[0].ID != 1 || ds[1].ID != 2 {
		t.Fatalf("displays = %+v", ds)
	}
	if got := Screen.PrimaryDisplay(); got.ID != 1 || got.Label != "Left" {
		t.Fatalf("primary = %+v", got)
	}
	if got := ds[0]; got.Bounds != (Rectangle{Width: 1440, Height: 900}) ||
		got.WorkArea != (Rectangle{Y: 25, Width: 1440, Height: 875}) ||
		got.ScaleFactor != 2 || !got.Primary {
		t.Fatalf("the primary display = %+v", got)
	}

	// Without a primary one the first is it.
	setDisplays(t, testDisplay(7, "Solo", 0, 0, 800, 600, false))
	if got := Screen.PrimaryDisplay(); got.ID != 7 {
		t.Fatalf("primary without one marked = %+v", got)
	}

	if got := Screen.CursorScreenPoint(); got != (Point{X: 10, Y: 20}) {
		t.Fatalf("the mouse is at %+v", got)
	}
}

// DisplayNearestPoint is the display closest to a point: the one it is
// on, and otherwise the one whose edge it is nearest.
func TestScreenDisplayNearestPoint(t *testing.T) {
	setDisplays(t,
		testDisplay(1, "Left", 0, 0, 1000, 800, true),
		testDisplay(2, "Right", 1000, 0, 1000, 800, false),
	)
	for _, tc := range []struct {
		p    Point
		want int64
	}{
		{Point{X: -50, Y: 400}, 1},  // left of the left one
		{Point{X: 1500, Y: 400}, 2}, // right of the right one
		{Point{X: 999, Y: 400}, 1},  // on the left one
		{Point{X: 1001, Y: 400}, 2}, // on the right one
		{Point{X: 1100, Y: -10}, 2}, // above the right one
		{Point{X: 1000, Y: 400}, 1}, // on both edges: the first of them
	} {
		if got := Screen.DisplayNearestPoint(tc.p); got.ID != tc.want {
			t.Errorf("the display nearest %+v is %d, want %d", tc.p, got.ID, tc.want)
		}
	}
	// A point in the gap between two stacked displays is nearest the one
	// it is closer to, by the distance to its edge.
	setDisplays(t,
		testDisplay(1, "Top", 0, 0, 1000, 500, true),
		testDisplay(2, "Bottom", 0, 900, 1000, 500, false),
	)
	if got := Screen.DisplayNearestPoint(Point{X: 500, Y: 750}); got.ID != 2 {
		t.Errorf("a point 150 above the bottom display is nearest %d", got.ID)
	}
	if got := Screen.DisplayNearestPoint(Point{X: 500, Y: 300}); got.ID != 1 {
		t.Errorf("a point on the top display is nearest %d", got.ID)
	}
}

// DisplayMatching is the display a rectangle overlaps the most, and the
// first when it overlaps none of them.
func TestScreenDisplayMatching(t *testing.T) {
	setDisplays(t,
		testDisplay(1, "Left", 0, 0, 1000, 800, true),
		testDisplay(2, "Right", 1000, 0, 1000, 800, false),
	)
	for _, tc := range []struct {
		r    Rectangle
		want int64
	}{
		{Rectangle{X: 900, Width: 200, Height: 100}, 1},  // equal overlap: the first of them
		{Rectangle{X: 1100, Width: 200, Height: 100}, 2}, // on the right
		{Rectangle{X: 600, Width: 1000, Height: 800}, 2}, // overlaps both, more of the right
		{Rectangle{X: -900, Width: 100, Height: 100}, 1}, // overlaps neither: the first of them
	} {
		if got := Screen.DisplayMatching(tc.r); got.ID != tc.want {
			t.Errorf("the display matching %+v is %d, want %d", tc.r, got.ID, tc.want)
		}
	}
}

// OnDisplaysChanged tells the listeners as displays change, until they
// are turned off.
func TestScreenDisplaysChanged(t *testing.T) {
	changed := 0
	off := Screen.OnDisplaysChanged(func() { changed++ })
	onMain(func() { fb.DisplaysChanged() })
	if changed != 1 {
		t.Fatalf("told of a change %d times", changed)
	}
	off()
	onMain(func() { fb.DisplaysChanged() })
	if changed != 1 {
		t.Fatalf("told of a change after off: %d times", changed)
	}
}
