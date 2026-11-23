//go:build windows && (amd64 || arm64)

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
	"github.com/egoist/mygo/ui"
)

// TestCaptionlessResizeEdges presses, by hit test, the left, right and bottom
// edges of a captionless window whose client area covers the whole window.
// The webview's window covers that client area too, so the window itself
// never gets WM_NCHITTEST there: without the resize edge windows, every one
// of these points would answer HTCLIENT and only move the window, or reach
// the page. Native UI's surface has the same problem.
//
// Windows 11 keeps its native borders outside the client area, so the test
// only covers Windows 10, where frameCalcSize gives the client the whole
// window.
func TestCaptionlessResizeEdges(t *testing.T) {
	if win.TestWindowsBuild() >= 22000 {
		t.Skip("the client area covers the whole window on Windows 10 only")
	}
	for _, test := range []struct {
		name string
		opts mygo.WindowOptions
		page bool
	}{
		{"webview", mygo.WindowOptions{Frameless: true, Width: 400, Height: 300}, true},
		{"webview hidden title bar", mygo.WindowOptions{TitleBarStyle: mygo.TitleBarHidden, Width: 400, Height: 300}, true},
		{"native ui", mygo.WindowOptions{Frameless: true, Width: 400, Height: 300, Content: ui.View(func(c *ui.Context) {
			c.Root().Background(ui.RGB(0x20, 0x20, 0x20))
		})}, false},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			w := newWindow(t, test.opts)
			if test.page {
				w.Page().LoadHTML(`<body style="margin:0;height:100vh"></body>`, "")
				waitFor(t, w, "document.readyState === 'complete'")
			}
			contentWidth, contentHeight := w.ContentSize()
			width, height := int32(contentWidth), int32(contentHeight)
			midX, midY := width/2, height/2
			for _, point := range []struct {
				name string
				x, y int32
				want string
			}{
				{"left", 3, midY, "left"},
				{"right", width - 3, midY, "right"},
				{"bottom", midX, height - 3, "bottom"},
				{"top", midX, 3, "top"},
				{"bottom-right corner", width - 3, height - 3, "bottom-right"},
				{"middle", midX, midY, "client"},
			} {
				hit, class := hitTest(w, point.x, point.y)
				if hit != point.want {
					t.Errorf("%s edge at (%d,%d) hit %q in %q, want %q", point.name, point.x, point.y, hit, class, point.want)
				}
			}
			// The window controls of a hidden title bar fill the top-right
			// corner: the right edge must start below them, not resize the
			// window when the close button is pressed.
			if test.name == "webview hidden title bar" {
				if hit, class := hitTest(w, width-3, 20); hit == "right" {
					t.Errorf("the right edge covers the window controls: hit %q in %q", hit, class)
				}
			}
			// A window that cannot resize leaves its edges to the content.
			w.SetResizable(false)
			if hit, class := hitTest(w, 3, midY); hit != "client" {
				t.Errorf("not resizable: left edge hit %q in %q, want client", hit, class)
			}
			w.SetResizable(true)
		})
	}
}

// hitTest sends WM_NCHITTEST to the window at (x, y) DIPs in w's client area,
// as the pointer would, and returns the code and the window class there.
func hitTest(w *mygo.Window, x, y int32) (hit, class string) {
	mygo.RunOnMain(func() {
		hit, class = win.TestHitAt(w.NativeHandle(), float64(x), float64(y))
	})
	return hit, class
}
