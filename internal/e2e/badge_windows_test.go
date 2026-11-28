//go:build windows && (amd64 || arm64)

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
)

// TestBadge puts a count on a real window's taskbar button and takes it
// off again: the shell takes the overlay once the button exists.
func TestBadge(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Badge", Width: 300, Height: 200})
	t.Cleanup(func() { mygo.App.SetBadgeCount(0) })
	shown := func() bool {
		var ok bool
		mygo.RunOnMain(func() { ok = win.TestBadgeShown(w.NativeHandle()) })
		return ok
	}
	// Set before the button exists, the badge waits for it.
	mygo.App.SetBadgeCount(7)
	w.Show()
	eventually(t, "the window shown", w.IsVisible)
	eventually(t, "the shell has the badge", shown)
	if got := mygo.App.Dock.Badge(); got != "7" {
		t.Errorf("Badge() = %q, want 7", got)
	}
	mygo.App.SetBadgeCount(120)
	if !shown() {
		t.Error("a new count took the badge off")
	}
	mygo.App.SetBadgeCount(0)
	eventually(t, "the badge taken off", func() bool { return !shown() })
}
