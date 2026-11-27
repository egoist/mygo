//go:build !js

package mygo

import (
	"path/filepath"
	"testing"
)

// TestWindowBooleans checks what a window says of the switches it was
// made with: what was switched off reads back off, what was left alone
// reads back on, and setting reaches the backend, which keeps what it
// keeps.
func TestWindowBooleans(t *testing.T) {
	off, _ := testWindow(t, WindowOptions{
		DisableResize:   true,
		DisableMove:     true,
		DisableMinimize: true,
		DisableMaximize: true,
		DisableClose:    true,
		AlwaysOnTop:     true,
	})
	for _, c := range []struct {
		name string
		got  func() bool
	}{
		{"resizable", off.IsResizable},
		{"movable", off.IsMovable},
		{"minimizable", off.IsMinimizable},
		{"maximizable", off.IsMaximizable},
		{"closable", off.IsClosable},
	} {
		if c.got() {
			t.Errorf("a window switched off is %s", c.name)
		}
	}
	if !off.IsAlwaysOnTop() {
		t.Error("a window made to stay above others does not")
	}

	on, _ := testWindow(t, WindowOptions{})
	for _, c := range []struct {
		name string
		got  func() bool
	}{
		{"resizable", on.IsResizable},
		{"movable", on.IsMovable},
		{"minimizable", on.IsMinimizable},
		{"maximizable", on.IsMaximizable},
		{"closable", on.IsClosable},
	} {
		if !c.got() {
			t.Errorf("a window left alone is not %s", c.name)
		}
	}
	if on.IsAlwaysOnTop() {
		t.Error("a window left alone is always on top")
	}

	// The setters reach the backend without trouble; what a window then
	// reports is the system's, which the fake does not model.
	on.SetResizable(false)
	on.SetMovable(false)
	on.SetMinimizable(false)
	on.SetMaximizable(false)
	on.SetClosable(false)
	on.SetAlwaysOnTop(true)
	on.SetMinimumSize(200, 120)
	on.SetMaximumSize(800, 600)
	on.SetHasShadow(false)
}

// TestWindowBounds checks that the window is where it was set: the bounds
// it was made with, the ones SetBounds set, the ones SetContentBounds set
// for the page area, and the corner SetPosition moves it to.
func TestWindowBounds(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{Width: 300, Height: 200})
	if got := w.Bounds(); got.Width != 300 || got.Height != 200 {
		t.Errorf("a window of 300×200 is %v", got)
	}

	r := Rectangle{X: 10, Y: 20, Width: 300, Height: 200}
	w.SetBounds(r)
	if got := w.Bounds(); got != r {
		t.Errorf("Bounds = %v, want %v", got, r)
	}

	c := Rectangle{X: 40, Y: 50, Width: 120, Height: 80}
	w.SetContentBounds(c)
	if got := w.ContentBounds(); got != c {
		t.Errorf("ContentBounds = %v, want %v", got, c)
	}

	w.SetPosition(50, 60)
	x, y := w.Position()
	if x != 50 || y != 60 {
		t.Errorf("Position = (%d, %d), want (50, 60)", x, y)
	}
	if got := w.Bounds(); got.Width != 120 || got.Height != 80 {
		t.Errorf("moving the window changed its size to %v", got)
	}

	// Centering is the display's to do.
	w.Center()
}

// TestWindowLook checks what the window says of how it looks: the handle
// it was given, its opacity kept between 0 and 1, and the material, the tint and the window
// buttons it was set, whose colors are checked as colors are.
func TestWindowLook(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{})
	if w.NativeHandle() == 0 {
		t.Error("a live window has no handle")
	}

	if got := w.Opacity(); got != 1 {
		t.Errorf("a window made without an opacity has %v, want 1", got)
	}
	for _, c := range []struct{ set, want float64 }{{0.5, 0.5}, {2, 1}, {-1, 0}} {
		w.SetOpacity(c.set)
		if got := w.Opacity(); got != c.want {
			t.Errorf("after SetOpacity(%v), Opacity = %v, want %v", c.set, got, c.want)
		}
	}

	w.SetVibrancy(VibrancyNone)
	if err := w.SetVibrancyTint("#204060"); err != nil {
		t.Errorf("SetVibrancyTint of a color: %v", err)
	}
	if err := w.SetVibrancyTint(""); err != nil {
		t.Errorf("SetVibrancyTint of no tint: %v", err)
	}
	if err := w.SetVibrancyTint("not a color"); err == nil {
		t.Error("SetVibrancyTint of something that is not a color said nothing")
	}

	w.SetWindowButtonsTheme(ThemeDark)
	w.SetWindowButtonsTheme("")
	w.SetContentProtection(true)

	// An absolute path on every platform: SetRepresentedFile makes
	// paths absolute, and "/tmp/report.md" is not on Windows.
	report := filepath.Join(t.TempDir(), "report.md")
	w.SetRepresentedFile(report)
	if got := w.RepresentedFile(); got != report {
		t.Errorf("RepresentedFile = %q, want %q, the one SetRepresentedFile set", got, report)
	}
	if !w.HasShadow() {
		t.Error("a window that was not asked to go without has no shadow")
	}
}
