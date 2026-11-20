package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// systemBack hands the content the system's back as a phone's surface
// does (internal/mobile's surface.back): Escape, then the Back key, each
// pressed and let go. It reports whether the content took either, which
// keeps Android from leaving the app.
func systemBack(tt *Tester) bool {
	for _, k := range []Key{KeyEscape, KeyBack} {
		taken := tt.rt.event(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(k)})
		tt.settle()
		tt.KeyUp(k)
		if taken {
			return true
		}
	}
	return false
}

// An overlay closing on the Back key takes the system's back: the app stays.
func TestSystemBackOverlayShortcut(t *testing.T) {
	open := true
	tt := NewTester(func(c *Context) {
		Text(c, "page")
		if open {
			Overlay(c, func() {
				if Box(c).Size(100, 100).OverlayShortcut(0, KeyBack) {
					open = false
				}
			})
		}
	}, 300, 200)
	if !systemBack(tt) {
		t.Error("the overlay's Back went unreported: the app would leave")
	}
	if open {
		t.Error("the overlay stayed open")
	}
	if systemBack(tt) {
		t.Error("with the overlay closed, nothing takes back, yet it was taken")
	}
}

// A dialog takes the system's back through its Escape.
func TestSystemBackDialog(t *testing.T) {
	open := true
	tt := NewTester(func(c *Context) {
		Text(c, "page")
		DialogBase(c, &open, func(backdrop, panel Element) {
			Text(c, "sheet")
		})
	}, 300, 200)
	if !systemBack(tt) {
		t.Error("the dialog's Escape went unreported: the app would leave")
	}
	if open {
		t.Error("the dialog stayed open")
	}
	if systemBack(tt) {
		t.Error("with the dialog closed, nothing takes back, yet it was taken")
	}
}

// The window's shortcut for Back takes it; nothing at all leaves it to the
// system.
func TestSystemBackWindow(t *testing.T) {
	window, backs := true, 0
	tt := NewTester(func(c *Context) {
		if window {
			c.OnWindowShortcut(0, KeyBack, func() { backs++ })
		}
		Text(c, "page")
	}, 300, 200)
	if !systemBack(tt) || backs != 1 {
		t.Errorf("the window's Back: taken by %d", backs)
	}
	window = false
	tt.Frame()
	if systemBack(tt) {
		t.Error("nothing takes back, yet it was taken")
	}
}
