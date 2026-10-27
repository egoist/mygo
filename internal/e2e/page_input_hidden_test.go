//go:build !js

package e2e

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// shownView is a window of native UI whose view builds a web view when told
// to, and paints a box over it when told to.
type shownView struct {
	w       *mygo.Window
	v       *mygo.WebView
	shown   atomic.Bool
	covered atomic.Bool
	frames  atomic.Int32
}

func newShownView(t *testing.T) *shownView {
	s := &shownView{}
	var view *mygo.WebView
	s.w = newWindow(t, mygo.WindowOptions{Title: "Page input, web view", Width: 760, Height: 560, Content: ui.View(func(c *ui.Context) {
		s.frames.Add(1)
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if view != nil && s.shown.Load() {
				ui.WebView(c, view).Grow(1).Children(func() {
					if s.covered.Load() {
						ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(ui.RGB(255, 0, 0))
					}
				})
			}
		})
	})})
	eventually(t, "a frame", func() bool { return s.frames.Load() > 0 })
	v, err := s.w.NewWebView(mygo.WebViewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s.v = v
	s.w.Update(func() { view = v })
	return s
}

// show builds the web view, or not, with a box over it, or not, and waits
// for the frames that place it.
func (s *shownView) show(t *testing.T, shown, covered bool) {
	t.Helper()
	s.shown.Store(shown)
	s.covered.Store(covered)
	n := s.frames.Load()
	eventually(t, "the frames that place the web view", func() bool {
		s.w.Invalidate()
		return s.frames.Load() >= n+2
	})
}

// Page.SendMouse, SendKey and InsertText need no shown web view, as the
// documentation of SendMouse says, wherever the platform lets them: a web
// view the window's view does not build, one a box covers, and, on Windows,
// a minimized window answer them as a shown one does (a hidden page does not
// draw frames, and so does not answer a mouse move until another event
// comes, which SendMouse does not wait for), and the page hears them.
func TestPageInputNotShown(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("WebKitGTK drops the presses of a web view that is not shown (see TestPageInput)")
	}
	s := newShownView(t)
	ip := newInputPageOf(t, s.v.Page())
	hidden := func() { ip.wait("document.visibilityState === 'hidden'") }
	visible := func() { ip.wait("document.visibilityState === 'visible'") }

	t.Run("never shown", func(t *testing.T) {
		ip.t = t
		// The page runs, hidden, and answers.
		hidden()
		if runtime.GOOS == "darwin" {
			// A web view that never had a size has none: WebKit hears no
			// mouse event there. Every call still returns, and the keys
			// and the text go in.
			for _, typ := range []mygo.MouseEventType{mygo.MouseMove, mygo.MouseDown, mygo.MouseUp} {
				if err := ip.p.SendMouse(mygo.MouseEvent{Type: typ, X: 30, Y: 30}); err != nil {
					t.Fatal(err)
				}
			}
			ip.eval("R.reset(); document.getElementById('ta').focus()")
			ip.key("h", 0)
			if err := ip.p.InsertText("i"); err != nil {
				t.Fatal(err)
			}
			ip.wait("R.state().model.ta === 'hi'")
			return
		}
		ip.reply("ce", "never shown")
	})

	t.Run("shown", func(t *testing.T) {
		ip.t = t
		s.show(t, true, false)
		visible()
		ip.reply("ce", "shown")
	})

	t.Run("not built", func(t *testing.T) {
		ip.t = t
		s.show(t, false, false)
		hidden()
		for _, who := range []string{"ce", "be", "ta"} {
			ip.reply(who, "not built")
		}
	})

	t.Run("covered", func(t *testing.T) {
		ip.t = t
		s.show(t, true, true)
		visible()
		ip.reply("be", "covered")
	})

	// A window that was created hidden and never shown.
	t.Run("hidden window", func(t *testing.T) {
		w := newWindow(t, mygo.WindowOptions{Title: "Page input, hidden", Width: 520, Height: 400, Hidden: true})
		wip := newInputPage(t, w)
		wip.reply("ta", "hidden window")
	})

	if runtime.GOOS != "windows" {
		return
	}

	// A minimized window leaves the page of a web view of native UI
	// visible, and hides the page of a window's own web view.
	t.Run("minimized", func(t *testing.T) {
		ip.t = t
		minimize := func() {
			s.w.Minimize()
			eventually(t, "the minimized window", s.w.IsMinimized)
		}
		restore := func() {
			s.w.Restore()
			eventually(t, "the restored window", func() bool { return !s.w.IsMinimized() })
		}
		t.Cleanup(restore)
		s.show(t, true, false)
		minimize()
		ip.reply("ce", "minimized")
		// A minimized window builds no frames: the view is hidden first.
		restore()
		s.show(t, false, false)
		hidden()
		minimize()
		ip.reply("ta", "not built and minimized")
	})

	t.Run("window", func(t *testing.T) {
		w := newWindow(t, mygo.WindowOptions{Title: "Page input, minimized", Width: 520, Height: 400})
		wip := newInputPage(t, w)
		w.Minimize()
		eventually(t, "the minimized window", w.IsMinimized)
		wip.wait("document.visibilityState === 'hidden'")
		wip.reply("be", "minimized window")
		w.Restore()
	})
}
