//go:build darwin

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/ui"
)

// onScreen reports whether some of the window shows on screen.
func onScreen(w *mygo.Window) (on bool) {
	mygo.RunOnMain(func() { on = darwin.TestOnScreen(w.NativeHandle()) })
	return on
}

// TestShowBuildsFrameOutOfSight hides a window of native UI whose web view
// has the keyboard, changes what it shows, and shows it again behind
// another window, which keeps the system reporting that nothing of it is on
// screen, as macOS does for a window shown again soon after it was hidden:
// the window draws the change all the same, rather than wait for the
// system to say it shows, which it may never do. With the keyboard on the
// native UI the window would draw a frame as its focus came back.
func TestShowBuildsFrameOutOfSight(t *testing.T) {
	var view *mygo.WebView
	var frames atomic.Int32
	w := newWindow(t, mygo.WindowOptions{Title: "Shown out of sight", X: 100, Y: 100, Width: 500, Height: 300, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Text(c, "Native").Height(40)
			if view != nil {
				ui.WebView(c, view).Grow(1)
			}
		})
	})})
	v, err := w.NewWebView(mygo.WebViewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w.Update(func() { view = v })
	v.Page().LoadHTML(webViewPage, "")
	waitForPage(t, v.Page(), "window.ready")
	waitOnScreen(w)
	v.Focus()
	// The keyboard leaving the native UI is input, which draws a frame
	// out of sight: let it draw before the window is hidden.
	built := frames.Load()
	w.Invalidate()
	eventually(t, "a frame after the web view took the keyboard", func() bool { return frames.Load() > built })

	// Another window covers it; one above its level stays above it as it
	// comes to the front.
	cover := newWindow(t, mygo.WindowOptions{Title: "Cover", X: 60, Y: 60, Width: 600, Height: 400, AlwaysOnTop: true, Content: ui.View(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(30, 30, 30))
	})})
	cover.Show()
	eventually(t, "the window covered", func() bool { return !onScreen(w) })

	w.Hide()
	built = frames.Load()
	w.Invalidate()
	w.Show()
	eventually(t, "a frame after the window was shown", func() bool { return frames.Load() > built })
	if onScreen(w) {
		t.Error("the window showed on screen: nothing kept it out of sight")
	}
}
