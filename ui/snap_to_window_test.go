package ui_test

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSnapToWindowUsesOverflowOnlyWithoutFlipping(t *testing.T) {
	for _, test := range []struct {
		name             string
		windowW, windowH int
		x, y, w, h       float32
		at, self         ui.Anchor
		margin           float32
		want             ui.Rect
	}{
		{name: "right and bottom overflow snap instead of flip", windowW: 300, windowH: 200, x: 280, y: 170, w: 100, h: 80, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, margin: 8, want: ui.Rect{X: 192, Y: 112, W: 100, H: 80}},
		{name: "small positive origin is not inset", windowW: 300, windowH: 200, x: 3, y: 2, w: 40, h: 20, at: ui.AnchorTopLeft, self: ui.AnchorTopLeft, margin: 8, want: ui.Rect{X: 3, Y: 2, W: 40, H: 20}},
		{name: "zero origin is not inset", windowW: 300, windowH: 200, w: 40, h: 20, at: ui.AnchorTopLeft, self: ui.AnchorTopLeft, margin: 8, want: ui.Rect{W: 40, H: 20}},
		{name: "exact right and bottom edges are not overflow", windowW: 300, windowH: 200, x: 260, y: 180, w: 40, h: 20, at: ui.AnchorTopLeft, self: ui.AnchorTopLeft, margin: 8, want: ui.Rect{X: 260, Y: 180, W: 40, H: 20}},
		{name: "one DIP overflow activates full inset", windowW: 300, windowH: 200, x: 261, y: 181, w: 40, h: 20, at: ui.AnchorTopLeft, self: ui.AnchorTopLeft, margin: 8, want: ui.Rect{X: 252, Y: 172, W: 40, H: 20}},
		{name: "negative left and top snap without changing corner", windowW: 300, windowH: 200, x: 2, y: 3, w: 40, h: 20, at: ui.AnchorTopLeft, self: ui.AnchorBottomRight, margin: 8, want: ui.Rect{X: 8, Y: 8, W: 40, H: 20}},
		{name: "narrow window keeps oversized width", windowW: 240, windowH: 200, x: 200, y: 100, w: 256, h: 40, at: ui.AnchorTopRight, self: ui.AnchorTopRight, margin: 8, want: ui.Rect{X: 8, Y: 100, W: 256, H: 40}},
		{name: "oversized box gives left and top priority", windowW: 100, windowH: 80, x: 80, y: 60, w: 150, h: 120, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, margin: 8, want: ui.Rect{X: 8, Y: 8, W: 150, H: 120}},
		{name: "zero is an explicit opt in", windowW: 300, windowH: 200, x: 280, y: 170, w: 100, h: 80, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, want: ui.Rect{X: 200, Y: 120, W: 100, H: 80}},
		{name: "custom overflow margin", windowW: 300, windowH: 200, x: 280, y: 170, w: 100, h: 80, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, margin: 12, want: ui.Rect{X: 188, Y: 108, W: 100, H: 80}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var panel ui.Handle
			var raw ui.Rect
			tt := ui.NewTester(func(c *ui.Context) {
				anchor := ui.Box(c).Size(20, 20).Absolute().Left(test.x).Top(test.y)
				ui.Overlay(c, func() {
					ui.Box(c).Size(test.w, test.h).AttachTo(anchor, test.at, test.self).SnapToWindow(test.margin).Label("snap panel").Bind(&panel)
				})
				raw = panel.Bounds(c)
			}, test.windowW, test.windowH)
			snapWantCommittedRect(t, tt, "snap panel", &raw, test.want)
			tt.Frame()
			snapWantCommittedRect(t, tt, "snap panel", &raw, test.want)
		})
	}
}

func TestSnapToWindowFitsFullPositiveMarginBox(t *testing.T) {
	for _, test := range []struct {
		name             string
		windowW, windowH int
		x, y, w, h       float32
		at, self         ui.Anchor
		margins          [4]float32
		want             ui.Rect
	}{
		{name: "asymmetric margins participate in both edges", windowW: 300, windowH: 200, x: 260, y: 150, w: 60, h: 40, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, margins: [4]float32{5, 11, 7, 13}, want: ui.Rect{X: 221, Y: 145, W: 60, H: 40}},
		{name: "margin box at zero remains there", windowW: 300, windowH: 200, x: 5, y: 5, w: 60, h: 40, at: ui.AnchorTopLeft, self: ui.AnchorTopLeft, margins: [4]float32{3, 4, 5, 6}, want: ui.Rect{X: 7, Y: 3, W: 60, H: 40}},
		{name: "negative margin box origin restores border inset", windowW: 300, windowH: 200, w: 60, h: 40, at: ui.AnchorTopLeft, self: ui.AnchorTopLeft, margins: [4]float32{3, 4, 5, 6}, want: ui.Rect{X: 14, Y: 11, W: 60, H: 40}},
		{name: "oversized full box retains border size", windowW: 100, windowH: 80, x: 80, y: 60, w: 90, h: 70, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, margins: [4]float32{5, 11, 7, 13}, want: ui.Rect{X: 21, Y: 13, W: 90, H: 70}},
		{name: "six DIP gap above target", windowW: 300, windowH: 200, x: 100, y: 100, w: 100, h: 80, at: ui.AnchorTopLeft, self: ui.AnchorBottomLeft, margins: [4]float32{0, 0, 6, 0}, want: ui.Rect{X: 100, Y: 14, W: 100, H: 80}},
		{name: "six DIP bottom margin counts in fit", windowW: 300, windowH: 200, x: 100, y: 170, w: 100, h: 80, at: ui.AnchorBottomLeft, self: ui.AnchorTopLeft, margins: [4]float32{0, 0, 6, 0}, want: ui.Rect{X: 100, Y: 106, W: 100, H: 80}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var panel ui.Handle
			var raw ui.Rect
			tt := ui.NewTester(func(c *ui.Context) {
				anchor := ui.Box(c).Size(20, 20).Absolute().Left(test.x).Top(test.y)
				ui.Overlay(c, func() {
					ui.Box(c).Size(test.w, test.h).BorderWidth(2).Padding(4).Margin(test.margins[:]...).AttachTo(anchor, test.at, test.self).SnapToWindow(8).Label("margin panel").Bind(&panel)
				})
				raw = panel.Bounds(c)
			}, test.windowW, test.windowH)
			snapWantCommittedRect(t, tt, "margin panel", &raw, test.want)
		})
	}
}

func TestSnapToWindowUsesCurrentAutoHeight(t *testing.T) {
	rows := 2
	var panel ui.Handle
	var raw ui.Rect
	tt := ui.NewTester(func(c *ui.Context) {
		anchor := ui.Box(c).Size(20, 20).Absolute().Left(120).Top(110)
		ui.Overlay(c, func() {
			ui.Column(c).Width(100).AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).SnapToWindow(8).Label("auto panel").Bind(&panel).Children(func() {
				for range rows {
					ui.Box(c).Size(100, 20).Shrink(0)
				}
			})
		})
		raw = panel.Bounds(c)
	}, 300, 200)
	snapWantCommittedRect(t, tt, "auto panel", &raw, ui.Rect{X: 120, Y: 130, W: 100, H: 40})
	rows = 5
	tt.Frame()
	snapWantCommittedRect(t, tt, "auto panel", &raw, ui.Rect{X: 120, Y: 92, W: 100, H: 100})
	rows = 1
	tt.Frame()
	snapWantCommittedRect(t, tt, "auto panel", &raw, ui.Rect{X: 120, Y: 130, W: 100, H: 20})
	rows = 5
	tt.Frame()
	tt.SetSize(300, 80)
	snapWantCommittedRect(t, tt, "auto panel", &raw, ui.Rect{X: 120, Y: 8, W: 100, H: 100})
}

func TestSnapToWindowScrollHeightIsOptIn(t *testing.T) {
	for _, test := range []struct {
		name string
		snap bool
		want ui.Rect
	}{
		{name: "legacy scroll still fits room beside target", want: ui.Rect{X: 120, Y: 110, W: 100, H: 86}},
		{name: "snap scroll retains requested height", snap: true, want: ui.Rect{X: 120, Y: 22, W: 100, H: 170}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				anchor := ui.Box(c).Size(20, 20).Absolute().Left(120).Top(90)
				ui.Overlay(c, func() {
					panel := ui.Scroll(c).Size(100, 170).AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).Label("scroll panel")
					if test.snap {
						panel.SnapToWindow(8)
					}
					panel.Children(func() { ui.Box(c).Size(100, 400).Shrink(0) })
				})
			}, 300, 200)
			snapWantRect(t, tt, "scroll panel", test.want)
		})
	}
}

func TestSnapToWindowTracksTargetResizeAndReopen(t *testing.T) {
	x, y, open := float32(200), float32(120), true
	tt := ui.NewTester(func(c *ui.Context) {
		c.Key("moving anchor")
		anchor := ui.ButtonBase(c).Size(40, 20).Absolute().Left(x).Top(y).Label("moving anchor")
		ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
			panel.Size(80, 30).Margin(0, 0, 6, 0).AttachTo(anchor, ui.AnchorTopRight, ui.AnchorBottomRight).SnapToWindow(8).Label("moving panel")
		})
	}, 300, 200)
	snapWantRect(t, tt, "moving panel", ui.Rect{X: 160, Y: 84, W: 80, H: 30})
	x, y = 4, 10
	tt.Frame()
	snapWantRect(t, tt, "moving panel", ui.Rect{X: 8, Y: 8, W: 80, H: 30})
	open = false
	tt.Frame()
	tt.Frame()
	if _, found := tt.Find("moving panel"); found {
		t.Fatal("closed panel retained its measured element")
	}
	x, y = 250, 180
	tt.Frame()
	open = true
	tt.Frame()
	snapWantRect(t, tt, "moving panel", ui.Rect{X: 210, Y: 144, W: 80, H: 30})
	tt.SetSize(260, 150)
	snapWantRect(t, tt, "moving panel", ui.Rect{X: 172, Y: 106, W: 80, H: 30})
	tt.SetSize(300, 200)
	snapWantRect(t, tt, "moving panel", ui.Rect{X: 210, Y: 144, W: 80, H: 30})
}

func TestSnapToWindowKeepsNestedAttachmentsOnFinalPanelGeometry(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		anchor := ui.Box(c).Size(20, 20).Absolute().Left(280).Top(170)
		ui.Overlay(c, func() {
			ui.Box(c).Size(100, 80).AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).SnapToWindow(8).Label("outer snap panel").Children(func() {
				innerAnchor := ui.Box(c).Size(20, 10).Label("inner anchor")
				ui.Box(c).Size(30, 20).AttachTo(innerAnchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).SnapToWindow(8).Label("inner snap panel")
			})
		})
	}, 300, 200)
	snapWantRect(t, tt, "outer snap panel", ui.Rect{X: 192, Y: 112, W: 100, H: 80})
	snapWantRect(t, tt, "inner anchor", ui.Rect{X: 192, Y: 112, W: 20, H: 10})
	snapWantRect(t, tt, "inner snap panel", ui.Rect{X: 192, Y: 122, W: 30, H: 20})
}

func TestSnapToWindowPreservesPopoverFocusDismissalAndTabIdentity(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "legacy AttachTo"
		if enabled {
			name = "snap AttachTo"
		}
		t.Run(name, func(t *testing.T) {
			open, draft, outside, chosen := false, "", 0, 0
			var input ui.Handle
			tt := ui.NewTester(func(c *ui.Context) {
				c.Key("before")
				ui.ButtonBase(c).Size(60, 20).Absolute().Left(20).Top(20).Label("before")
				c.Key("trigger")
				anchor := ui.ButtonBase(c).Size(40, 20).Absolute().Left(240).Top(170).Label("trigger").OnClick(func() { open = !open })
				c.Key("after")
				ui.ButtonBase(c).Size(60, 20).Absolute().Left(20).Top(60).Label("after").OnClick(func() { outside++ })
				ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
					panel.Size(120, 60).Padding(6).AttachTo(anchor, ui.AnchorTopLeft, ui.AnchorBottomLeft).Label("interactive panel")
					if enabled {
						panel.SnapToWindow(8)
					}
					c.Key("popup input")
					ui.TextInputBase(c, &draft).Size(100, 20).Bind(&input).Label("popup input")
					c.Key("popup action")
					ui.ButtonBase(c).Size(100, 20).Label("popup action").OnClick(func() { chosen++ })
				})
			}, 300, 200)
			snapClick(t, tt, "trigger")
			if !open || !tt.Focused("trigger") {
				t.Fatal("opening popover lost its anchor identity")
			}
			tt.Key(0, ui.KeyTab)
			if !tt.Focused("popup input") {
				t.Fatal("Tab did not insert popover controls immediately after their anchor")
			}
			tt.Type("typed")
			tt.SetSize(280, 200)
			tt.Frame()
			if !tt.Focused("popup input") || draft != "typed" {
				t.Fatal("geometry update lost input state or focus")
			}
			tt.Key(0, ui.KeyTab)
			if !tt.Focused("popup action") {
				t.Fatal("Tab skipped a popover control")
			}
			tt.Key(0, ui.KeyEnter)
			if chosen != 1 || !open {
				t.Fatal("keyboard action failed or was treated as outside")
			}
			tt.Key(0, ui.KeyTab)
			if !tt.Focused("after") {
				t.Fatal("Tab did not return to the anchor's next sibling")
			}
			snapClick(t, tt, "popup input")
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if open || !tt.Focused("trigger") {
				t.Fatal("Escape did not close and restore anchor focus")
			}
			snapClick(t, tt, "trigger")
			snapClick(t, tt, "trigger")
			if open {
				t.Fatal("anchor press was treated as outside before its toggle")
			}
			snapClick(t, tt, "trigger")
			snapClick(t, tt, "popup input")
			if draft != "typed" {
				t.Fatal("reopening discarded the bound input value")
			}
			snapClick(t, tt, "after")
			if open || outside != 1 || !tt.Focused("after") {
				t.Fatal("outside press did not dismiss and reach the underlying control")
			}
			tt.Frame()
			tt.Frame()
			if _, found := tt.Find("interactive panel"); found {
				t.Fatal("dismissed panel remained mounted")
			}
		})
	}
}

func TestSnapToWindowLeavesLegacyFlipAndFourDIPFitUnchanged(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		anchor := ui.Box(c).Size(20, 20).Absolute().Left(280).Top(170)
		ui.Overlay(c, func() {
			ui.Box(c).Size(100, 80).AttachTo(anchor, ui.AnchorBottomLeft, ui.AnchorTopLeft).Label("legacy flipped panel")
		})
	}, 300, 200)
	snapWantRect(t, tt, "legacy flipped panel", ui.Rect{X: 196, Y: 90, W: 100, H: 80})
	tt = ui.NewTester(func(c *ui.Context) {
		anchor := ui.Box(c).Size(20, 20).Absolute()
		ui.Overlay(c, func() {
			ui.Box(c).Size(100, 80).AttachTo(anchor, ui.AnchorCenter, ui.AnchorCenter).Label("legacy clamped panel")
		})
	}, 300, 200)
	snapWantRect(t, tt, "legacy clamped panel", ui.Rect{X: 4, Y: 4, W: 100, H: 80})
}

func TestSnapToWindowResolvesLogicalAnchorsInRTL(t *testing.T) {
	for _, test := range []struct {
		name     string
		at, self ui.Anchor
		ltr, rtl float32
	}{
		{name: "end", at: ui.AnchorBottomEnd, self: ui.AnchorTopEnd, ltr: 200, rtl: 192},
		{name: "start", at: ui.AnchorBottomStart, self: ui.AnchorTopStart, ltr: 192, rtl: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				anchor := ui.Box(c).Size(20, 20).Absolute().Left(280).Top(170)
				ui.Overlay(c, func() {
					ui.Box(c).Size(100, 80).AttachTo(anchor, test.at, test.self).SnapToWindow(8).Label("direction panel")
				})
			}, 300, 200)
			snapWantRect(t, tt, "direction panel", ui.Rect{X: test.ltr, Y: 112, W: 100, H: 80})
			tt.SetDirection(ui.RightToLeft)
			snapWantRect(t, tt, "direction panel", ui.Rect{X: test.rtl, Y: 112, W: 100, H: 80})
		})
	}
}

func snapWantCommittedRect(t *testing.T, tt *ui.Tester, label string, raw *ui.Rect, want ui.Rect) {
	t.Helper()
	// Handle.Bounds exposes the committed border box, while Tester.Find
	// exposes only its visible intersection. Read the preceding committed
	// frame into plain data without retaining a Context or Element.
	tt.Frame()
	snapCheckRect(t, label, *raw, want)
}

func snapWantRect(t *testing.T, tt *ui.Tester, label string, want ui.Rect) {
	t.Helper()
	got, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q", label)
	}
	snapCheckRect(t, label, got, want)
}

func snapCheckRect(t *testing.T, label string, got, want ui.Rect) {
	t.Helper()
	if math.Abs(float64(got.X-want.X)) > .01 || math.Abs(float64(got.Y-want.Y)) > .01 || math.Abs(float64(got.W-want.W)) > .01 || math.Abs(float64(got.H-want.H)) > .01 {
		t.Fatalf("%s = %+v, want %+v", label, got, want)
	}
}

func snapClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}
