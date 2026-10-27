//go:build darwin

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pageFrame is the frame of the window a page reads: its outerWidth,
// outerHeight, screenX and screenY, and screenLeft and screenTop, which
// say the same.
func pageFrame(t *testing.T, p *mygo.Page) mygo.Rectangle {
	t.Helper()
	n, err := mygo.EvalAs[[]int](p, "[outerWidth, outerHeight, screenX, screenY, screenLeft, screenTop]")
	if err != nil || len(n) != 6 {
		t.Fatalf("reading the window frame: %v, %v", n, err)
	}
	if n[2] != n[4] || n[3] != n[5] {
		t.Errorf("screenX, screenY = %d, %d, but screenLeft, screenTop = %d, %d", n[2], n[3], n[4], n[5])
	}
	return mygo.Rectangle{X: n[2], Y: n[3], Width: n[0], Height: n[1]}
}

// movesAndResizes puts the window at several frames, one move, one resize
// and both, and waits for the page to read each as the window's Bounds:
// the page asks WebKit each time it reads one, so nothing is pushed.
func movesAndResizes(t *testing.T, w *mygo.Window, p *mygo.Page) {
	t.Helper()
	for _, frame := range []mygo.Rectangle{
		{X: 150, Y: 160, Width: 420, Height: 320},
		{X: 260, Y: 160, Width: 420, Height: 320},
		{X: 260, Y: 160, Width: 520, Height: 380},
		{X: 90, Y: 70, Width: 460, Height: 300},
	} {
		w.SetBounds(frame)
		eventually(t, "the window at its new frame", func() bool { return w.Bounds() == frame })
		if got := pageFrame(t, p); got != frame {
			t.Errorf("after SetBounds(%+v) the page reads the frame %+v", frame, got)
		}
	}
}

// TestPageWindowFrame checks that a page sees the frame of its window, as
// it does in Safari: outerWidth and outerHeight are its size and screenX
// and screenY its top-left, in points, from the top-left of the primary
// screen. A window that moves or resizes shows its new frame, a hidden one
// its frame.
func TestPageWindowFrame(t *testing.T) {
	t.Run("window", func(t *testing.T) {
		w := newWindow(t, mygo.WindowOptions{Title: "Frame", Width: 420, Height: 320, X: 150, Y: 160})
		w.Page().LoadHTML(`<body style="margin:0"><script>window.ready = true</script>`, "")
		waitFor(t, w, "window.ready")
		if got, want := pageFrame(t, w.Page()), w.Bounds(); got != want {
			t.Errorf("the page reads the frame %+v, want %+v", got, want)
		}
		movesAndResizes(t, w, w.Page())

		w.Hide()
		if got, want := pageFrame(t, w.Page()), w.Bounds(); got != want {
			t.Errorf("hidden: the page reads the frame %+v, want %+v", got, want)
		}
	})

	// A web view in native UI, which fills part of its window, sees the
	// frame of the window.
	t.Run("web view", func(t *testing.T) {
		var view *mygo.WebView
		var frames atomic.Int32
		w := newWindow(t, mygo.WindowOptions{Title: "Frame web view", Width: 420, Height: 320, X: 150, Y: 160, Content: ui.View(func(c *ui.Context) {
			frames.Add(1)
			ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
				ui.Text(c, "Above the page")
				ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
					ui.Box(c).Size(100, 10)
					if view != nil {
						ui.WebView(c, view).Grow(1)
					}
				})
			})
		})})
		eventually(t, "a frame", func() bool { return frames.Load() > 0 })
		v, err := w.NewWebView(mygo.WebViewOptions{})
		if err != nil {
			t.Fatal(err)
		}
		w.Update(func() { view = v })
		v.Page().LoadHTML(`<body style="margin:0"><script>window.ready = true</script>`, "")
		waitForPage(t, v.Page(), "window.ready")
		if n, err := mygo.EvalAs[int](v.Page(), "innerWidth"); err != nil || n >= 420 {
			t.Fatalf("the web view is %d wide (%v), not a part of the window", n, err)
		}
		if got, want := pageFrame(t, v.Page()), w.Bounds(); got != want {
			t.Errorf("the page reads the frame %+v, want %+v", got, want)
		}
		movesAndResizes(t, w, v.Page())
	})
}
