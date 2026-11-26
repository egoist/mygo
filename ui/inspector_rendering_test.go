//go:build !mygo_noinspector

package ui

import (
	"math"
	"testing"
	"time"
)

func TestContrastRatio(t *testing.T) {
	for _, tt := range []struct {
		a, b Color
		want float64
	}{
		{RGB(0, 0, 0), RGB(255, 255, 255), 21},
		{RGB(255, 255, 255), RGB(255, 255, 255), 1},
		{RGB(119, 119, 119), RGB(255, 255, 255), 4.48}, // the well-known #777 on white, just below AA
		{RGB(118, 118, 118), RGB(255, 255, 255), 4.54},
	} {
		if got := contrastRatio(tt.a, tt.b); math.Abs(got-tt.want) > 0.01 {
			t.Errorf("contrastRatio(%v, %v) = %.3f, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestContrastLevel(t *testing.T) {
	for _, tt := range []struct {
		ratio  float64
		size   float32
		weight int
		want   int
	}{
		{7, 14, 400, 2},
		{6.9, 14, 400, 1},
		{4.5, 14, 400, 1},
		{4.4, 14, 400, 0},
		{3, 24, 400, 1},
		{4.5, 24, 400, 2},
		{3, 19, 700, 1},
		{3, 19, 400, 0},
		{2.9, 24, 400, 0},
	} {
		if got := contrastLevel(tt.ratio, tt.size, tt.weight); got != tt.want {
			t.Errorf("contrastLevel(%v, %v, %v) = %d, want %d", tt.ratio, tt.size, tt.weight, got, tt.want)
		}
	}
}

func TestTextContrast(t *testing.T) {
	type view func(*context) *node
	black, white := RGB(0, 0, 0), RGB(255, 255, 255)
	for name, tt := range map[string]struct {
		view view
		want float64
		ok   bool
	}{
		"own background": {func(c *context) *node {
			return coreText(c, "A").TextColor(black).Background(white)
		}, 21, true},
		"an ancestor's": {func(c *context) *node {
			var e *node
			coreColumn(c).Background(black).Children(func() {
				coreColumn(c).Children(func() { e = coreText(c, "A").TextColor(white) })
			})
			return e
		}, 21, true},
		"translucent over opaque": {func(c *context) *node {
			var e *node
			// Alpha 0.5 is the byte 128, so #7f7f7f on white.
			coreColumn(c).Background(white).Children(func() {
				e = coreText(c, "A").TextColor(RGBA(0, 0, 0, 0.5))
			})
			return e
		}, 4.00, true},
		"a faded group": {func(c *context) *node {
			var e *node
			coreColumn(c).Background(white).Children(func() {
				coreColumn(c).Opacity(0.5).Children(func() { e = coreText(c, "A").TextColor(black) })
			})
			return e
		}, 3.95, true},
		"a gradient": {func(c *context) *node {
			var e *node
			coreColumn(c).Gradient(black, white, 0).Children(func() { e = coreText(c, "A").TextColor(black) })
			return e
		}, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			var e *node
			tt2 := coreNewTester(func(c *context) { e = tt.view(c) }, 200, 200)
			tt2.Frame()
			got, ok := textContrast(tt2.rt, e)
			if ok != tt.ok || math.Abs(got-tt.want) > 0.02 {
				t.Errorf("got %.3f, %v, want %v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestContrastRow(t *testing.T) {
	var pass, fail, big *node
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Background(RGB(255, 255, 255)).Children(func() {
			pass = coreText(c, "A").TextColor(RGB(0, 0, 0))
			fail = coreText(c, "B").TextColor(RGB(190, 190, 190))
			big = coreText(c, "C").TextColor(RGB(128, 128, 128)).FontSize(30)
		})
	}, 200, 200)
	tt.Frame()
	for _, c := range []struct {
		e    *node
		want string
	}{
		{pass, "Aa 21.00 ✓✓"},
		{fail, "Aa 1.86 ⊘"},
		{big, "Aa 3.95 ✓"}, // large text passes AA from 3
	} {
		r, ok := contrastRow(tt.rt, c.e)
		if !ok || r.value != c.want {
			t.Errorf("got %q, %v, want %q", r.value, ok, c.want)
		}
	}
}

func TestInspectorPaintFlashing(t *testing.T) {
	x := float32(10)
	tt, _ := inspected(func(c *context) {
		coreBox(c).Absolute().Left(x).Top(10).Size(20, 20).Background(RGB(255, 0, 0))
	})
	openInspector(tt)
	if err := tt.Click("Paint flashing"); err != nil {
		t.Fatal(err)
	}
	in := &tt.rt.insp
	tt.Frame()
	tt.Frame()
	if len(in.flashes) != 0 {
		t.Fatalf("flashes without a change: %v", in.flashes)
	}

	x = 100
	tt.rt.requestFrame()
	tt.Frame()
	if len(in.flashes) == 0 {
		t.Fatal("no flash for a box that moved")
	}
	// Where it was and where it is, without more than that around them.
	var left, right float32 = 1e9, 0
	for _, f := range in.flashes {
		left, right = min(left, f.r.X), max(right, f.r.X+f.r.W)
	}
	if left > 10 || right < 120 || left < 5 || right > 125 {
		t.Errorf("the flashes go from %v to %v", left, right)
	}
	shown := tt.Image().RGBAAt(115, 15)
	if shown.R == 255 && shown.G == 0 {
		t.Errorf("the box shows no flash: %v", shown)
	}

	// They fade by the clock, and nothing else changing leaves none.
	// (A refresh later, for a frame to come after they have.)
	tt.Advance(inspFlashLife + 50*time.Millisecond)
	if len(in.flashes) != 0 {
		t.Errorf("flashes after fading: %v", in.flashes)
	}
	if got := tt.Image().RGBAAt(115, 15); got.R != 255 || got.G != 0 {
		t.Errorf("the box after fading: %v", got)
	}
	if tt.Advance(time.Second); len(in.flashes) != 0 {
		t.Errorf("flashes with nothing changing: %v", in.flashes)
	}
}

// The panel changing isn't the content changing, and a flash is never over it.
func TestInspectorPaintFlashingLeavesThePanel(t *testing.T) {
	tt, _ := inspected(func(c *context) { coreText(c, "Content") })
	openInspector(tt)
	if err := tt.Click("Paint flashing"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	in := &tt.rt.insp
	if err := tt.Click("Performance"); err != nil {
		t.Fatal(err)
	}
	for _, f := range in.flashes {
		if f.r.X+f.r.W > in.appW {
			t.Errorf("a flash over the panel: %v, the content is %v wide", f.r, in.appW)
		}
	}
}

func TestInspectorPaintFlashingTurnsOff(t *testing.T) {
	x := float32(10)
	tt, _ := inspected(func(c *context) {
		coreBox(c).Absolute().Left(x).Top(10).Size(20, 20).Background(RGB(255, 0, 0))
	})
	openInspector(tt)
	tt.Click("Paint flashing")
	tt.Frame()
	tt.Click("Paint flashing")
	x = 100
	tt.rt.requestFrame()
	tt.Frame()
	if in := &tt.rt.insp; len(in.flashes) != 0 || in.differ != nil {
		t.Errorf("after turning it off: %v, %v", in.flashes, in.differ)
	}
	tt.Click("Paint flashing")
	tt.rt.toggleInspector()
	tt.Frame()
	if in := &tt.rt.insp; in.flashing || in.differ != nil {
		t.Errorf("after closing: %v, %v", in.flashing, in.differ)
	}
}

func TestInspectorDarkToggle(t *testing.T) {
	var bg Color
	tt, _ := inspected(func(c *context) {
		bg = c.Theme().Background
		coreText(c, "Content")
	})
	light := LightTheme().Background
	dark := DarkTheme().Background
	openInspector(tt)
	if bg != light {
		t.Fatalf("started with %v", bg)
	}
	if err := tt.Click("Dark appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if bg != dark || !tt.rt.dark {
		t.Fatalf("the toggle left %v", bg)
	}
	// The system changing doesn't undo it while it is on.
	tt.SetDark(false)
	tt.Frame()
	if bg != dark {
		t.Errorf("the system's change left %v", bg)
	}
	if err := tt.Click("Dark appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if bg != light {
		t.Errorf("toggled back to %v", bg)
	}
	// Closing goes back to the system's.
	tt.Click("Dark appearance")
	tt.Frame()
	tt.rt.toggleInspector()
	tt.Frame()
	if bg != light {
		t.Errorf("after closing: %v", bg)
	}
}
