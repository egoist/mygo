//go:build darwin

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/internal/wait"
	"github.com/egoist/mygo/ui"
)

// TestPopoverFlyoutsReuseNSPopovers opens a popover flyout, closes it and
// opens another, as a menu bar app does at each click of its tray icon:
// the second shows in the first one's NSPopover. On macOS 26 a released
// NSPopover leaves its material's backdrop behind, a few MB each.
func TestPopoverFlyoutsReuseNSPopovers(t *testing.T) {
	parent := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200})
	eventually(t, "anchor window visible", parent.IsVisible)
	open := func() (*mygo.Window, uintptr) {
		t.Helper()
		fl := mygo.NewFlyout(mygo.FlyoutOptions{Parent: parent, Anchor: mygo.Rectangle{X: 100, Y: 20, Width: 80, Height: 24}, Popover: true, Width: 220, Height: 120, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Popover reuse test") })})
		eventually(t, "popover window adopted", func() bool { return fl.NativeHandle() != 0 })
		var popover uintptr
		mygo.RunOnMain(func() { popover = darwin.TestPopover(fl.NativeHandle()) })
		if popover == 0 {
			t.Fatal("the flyout shows in no NSPopover")
		}
		return fl, popover
	}
	first, before := open()
	closed := make(chan struct{}, 1)
	first.OnClosed(func() { closed <- struct{}{} })
	first.Close()
	wait.For(t, "first flyout closed", closed)
	second, after := open()
	defer second.Destroy()
	if after != before {
		t.Fatalf("the second popover flyout shows in a new NSPopover (%#x), not the first one's (%#x)", after, before)
	}
	if !second.IsVisible() {
		t.Fatal("the second popover flyout does not show")
	}
}
