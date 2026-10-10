package e2e

import (
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

const flyoutPage = `<body style="margin:0;background:transparent">` +
	`<button id="b" style="width:100%;height:100px;background:#fc0" onclick="window.clicked=(window.clicked||0)+1">x</button></body>`

// TestFlyout places a flyout below an anchor in its parent, beyond the
// parent's bottom edge, flips it above where the work area ends, and
// keeps it next to the anchor as the parent moves.
func TestFlyout(t *testing.T) {
	work := mygo.Screen.PrimaryDisplay().WorkArea
	parent := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200, X: work.X + 100, Y: work.Y + 100})
	anchor := mygo.Rectangle{X: 20, Y: 150, Width: 80, Height: 24}
	fl := mygo.NewFlyout(mygo.FlyoutOptions{Parent: parent, Anchor: anchor, Gap: 4, Width: 220, Height: 160})
	defer fl.Destroy()
	if !fl.IsFlyout() || fl.Parent() != parent || !fl.IsVisible() {
		t.Fatalf("flyout: IsFlyout %v, parent %v, visible %v", fl.IsFlyout(), fl.Parent() == parent, fl.IsVisible())
	}
	// Linux reports where the window manager's frame is, not the
	// content, which a frame's title bar and borders push in: measure it.
	content := func() mygo.Rectangle { return parent.ContentBounds() }
	eventually(t, "the flyout placed", func() bool { return fl.Bounds().Width == 220 })
	if f, c := fl.Bounds(), content(); f.X-c.X != anchor.X || f.Y-c.Y != anchor.Y+anchor.Height+4 {
		inset := mygo.Point{X: f.X - c.X - anchor.X, Y: f.Y - c.Y - anchor.Y - anchor.Height - 4}
		if runtime.GOOS != "linux" || inset.X < 0 || inset.Y < 0 || inset.X > 10 || inset.Y > 50 {
			t.Fatalf("flyout at %+v, not below its anchor %+v in the content at %+v", f, anchor, c)
		}
		t.Logf("the content is %+v from where the parent reports it", inset)
		content = func() mygo.Rectangle {
			c := parent.ContentBounds()
			return mygo.Rectangle{X: c.X + inset.X, Y: c.Y + inset.Y, Width: c.Width, Height: c.Height}
		}
	}
	below := func() mygo.Rectangle {
		c := content()
		return mygo.Rectangle{X: c.X + anchor.X, Y: c.Y + anchor.Y + anchor.Height + 4, Width: 220, Height: 160}
	}
	eventuallyAt(t, "the flyout below its anchor", fl, below)
	if p, f := parent.Bounds(), fl.Bounds(); f.Y+f.Height <= p.Y+p.Height {
		t.Errorf("flyout %+v does not extend beyond its parent %+v", f, p)
	}

	// A click in a flyout that is not focusable reaches its page and
	// leaves the keyboard in the parent.
	fl.Page().LoadHTML(flyoutPage, "")
	waitFor(t, fl, "document.getElementById('b')")
	parent.Focus()
	eventually(t, "the parent focused", parent.IsFocused)
	if click(fl, 50, 50) {
		waitFor(t, fl, "window.clicked === 1")
		if fl.IsFocused() || !parent.IsFocused() {
			t.Errorf("after a click in the flyout: flyout focused %v, parent focused %v", fl.IsFocused(), parent.IsFocused())
		}
	}

	// It follows the parent.
	parent.SetPosition(work.X+160, work.Y+140)
	eventuallyAt(t, "the flyout following its parent", fl, below)

	// Where the work area has no room below, it goes above.
	parent.SetPosition(work.X+100, work.Y+work.Height-210)
	eventuallyAt(t, "the flyout above its anchor", fl, func() mygo.Rectangle {
		c := content()
		return mygo.Rectangle{X: c.X + anchor.X, Y: c.Y + anchor.Y - 4 - 160, Width: 220, Height: 160}
	})

	// SetAnchor and SetSize place it again.
	parent.SetPosition(work.X+100, work.Y+100)
	anchor = mygo.Rectangle{X: 200, Y: 10, Width: 40, Height: 20}
	fl.SetAnchor(anchor)
	fl.SetSize(120, 90)
	eventuallyAt(t, "the flyout at its new anchor and size", fl, func() mygo.Rectangle {
		c := content()
		return mygo.Rectangle{X: c.X + anchor.X, Y: c.Y + anchor.Y + anchor.Height + 4, Width: 120, Height: 90}
	})

	// It closes with its parent.
	parent.Destroy()
	eventually(t, "the flyout closed with its parent", fl.IsDestroyed)
}

// eventuallyAt waits for a flyout to be at the bounds want returns.
func eventuallyAt(t *testing.T, what string, fl *mygo.Window, want func() mygo.Rectangle) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for fl.Bounds() != want() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: at %+v, not %+v", what, fl.Bounds(), want())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestFocusableFlyout gives a focusable flyout the keyboard and closes it
// when the parent takes the keyboard back, unless OnClose prevents it.
func TestFocusableFlyout(t *testing.T) {
	parent := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200})
	parent.Focus()
	eventually(t, "the parent focused", parent.IsFocused)
	fl := mygo.NewFlyout(mygo.FlyoutOptions{Parent: parent, Anchor: mygo.Rectangle{X: 10, Y: 10}, Width: 200, Height: 120, Focusable: true})
	defer fl.Destroy()
	eventually(t, "the flyout focused", fl.IsFocused)

	keep := true
	closes := 0
	fl.OnClose(func(e *mygo.CloseEvent) {
		closes++
		if keep {
			e.PreventDefault()
		}
	})
	parent.Focus()
	eventually(t, "the flyout asked to close", func() bool { return closes == 1 })
	if fl.IsDestroyed() {
		t.Fatal("the flyout closed though OnClose prevented it")
	}

	keep = false
	fl.Focus()
	eventually(t, "the flyout focused again", fl.IsFocused)
	parent.Focus()
	eventually(t, "the flyout closed", fl.IsDestroyed)
	eventually(t, "the parent focused", parent.IsFocused)
}

// TestPopoverFlyout shows a flyout in a popover of the system where there
// is one (macOS), which closes as the user clicks elsewhere when it is
// focusable, and is a plain flyout elsewhere.
func TestPopoverFlyout(t *testing.T) {
	parent := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200})
	parent.Focus()
	eventually(t, "the parent focused", parent.IsFocused)
	fl := mygo.NewFlyout(mygo.FlyoutOptions{Parent: parent, Anchor: mygo.Rectangle{X: 100, Y: 20, Width: 80, Height: 24}, Width: 200, Height: 100, Popover: true})
	defer fl.Destroy()
	fl.Page().LoadHTML(flyoutPage, "")
	waitFor(t, fl, "document.getElementById('b')")
	if !fl.IsVisible() {
		t.Fatal("the popover does not show")
	}
	b, c := fl.Bounds(), parent.ContentBounds()
	if b.Width < 200 || b.Height < 100 || b.Y < c.Y+20+24 {
		t.Errorf("popover at %+v, below the anchor at %+v in %+v", b, mygo.Rectangle{X: 100, Y: 20, Width: 80, Height: 24}, c)
	}
	fl.Hide()
	eventually(t, "the popover hidden", func() bool { return !fl.IsVisible() })
	fl.ShowInactive()
	eventually(t, "the popover shown again", fl.IsVisible)
	if fl.IsDestroyed() {
		t.Fatal("hiding the popover closed it")
	}
	if appClick(fl, 50, 50) {
		waitFor(t, fl, "window.clicked === 1")
	}
	if fl.IsDestroyed() {
		t.Fatal("a click in the popover closed it")
	}

	focusable := mygo.NewFlyout(mygo.FlyoutOptions{Parent: parent, Anchor: mygo.Rectangle{X: 10, Y: 150}, Width: 150, Height: 80, Popover: true, Focusable: true})
	defer focusable.Destroy()
	eventually(t, "the focusable popover focused", focusable.IsFocused)
	closes := 0
	focusable.OnClose(func(*mygo.CloseEvent) { closes++ })
	if appClick(parent, 280, 10) {
		eventually(t, "the focusable popover closed", focusable.IsDestroyed)
		if closes != 1 {
			t.Errorf("OnClose heard %d closes", closes)
		}
	}
}
