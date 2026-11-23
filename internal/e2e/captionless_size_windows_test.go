//go:build windows && (amd64 || arm64)

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
)

// TestCaptionlessWindowSize pins the sizes of a window without a caption on
// both sides of the frameCalcSize change.
//
// On Windows 10 the client area covers the whole window: a captionless
// window is the size its options ask for, and its page is the size
// UseContentSize asks for. Before this, outerSize still added the invisible
// borders of a resizable window, which frameCalcSize no longer takes out of
// the client area: a 400x300 window with UseContentSize came out 413x306 here,
// and SetContentSize/SetContentBounds grew it by the same borders.
//
// Windows 11 keeps its native frame: options still size the window, and its
// page is smaller than it by the native borders. UseContentSize still makes
// the page 400x300, and the window grows around it.
func TestCaptionlessWindowSize(t *testing.T) {
	fullClient := win.TestWindowsBuild() < 22000 // frameCalcSize covers the whole window on Windows 10
	for _, test := range []struct {
		name      string
		opts      mygo.WindowOptions
		resizable bool
	}{
		{"frameless", mygo.WindowOptions{Frameless: true}, true},
		{"fixed frameless", mygo.WindowOptions{Frameless: true, DisableResize: true}, false},
		{"hidden title bar", mygo.WindowOptions{TitleBarStyle: mygo.TitleBarHidden}, true},
		{"fixed hidden title bar", mygo.WindowOptions{TitleBarStyle: mygo.TitleBarHidden, DisableResize: true}, false},
	} {
		test := test
		t.Run(test.name+"/window size", func(t *testing.T) {
			opts := test.opts
			opts.Width, opts.Height = 400, 300
			w := newWindow(t, opts)
			if b := w.Bounds(); b.Width != 400 || b.Height != 300 {
				t.Errorf("window bounds = %dx%d, want 400x300", b.Width, b.Height)
			}
			width, height := w.ContentSize()
			if fullClient || !test.resizable {
				if width != 400 || height != 300 {
					t.Errorf("content size = %dx%d, want 400x300", width, height)
				}
			} else if width >= 400 || height >= 300 {
				t.Errorf("content size = %dx%d, want less than 400x300 inside the native frame", width, height)
			}
			// A resize after the window exists takes the size it asks for too.
			resized := mygo.Rectangle{X: 200, Y: 180, Width: 420, Height: 280}
			w.SetBounds(resized)
			if b := w.Bounds(); b.Width != 420 || b.Height != 280 {
				t.Errorf("window bounds after SetBounds = %dx%d, want 420x280", b.Width, b.Height)
			}
			width, height = w.ContentSize()
			if fullClient || !test.resizable {
				if width != 420 || height != 280 {
					t.Errorf("content size after SetBounds = %dx%d, want 420x280", width, height)
				}
			} else if width >= 420 || height >= 280 {
				t.Errorf("content size after SetBounds = %dx%d, want less than 420x280 inside the native frame", width, height)
			}
		})

		t.Run(test.name+"/content size", func(t *testing.T) {
			opts := test.opts
			opts.Width, opts.Height, opts.UseContentSize = 400, 300, true
			w := newWindow(t, opts)
			width, height := w.ContentSize()
			if width != 400 || height != 300 {
				t.Fatalf("content size = %dx%d, want 400x300", width, height)
			}
			b := w.Bounds()
			if fullClient || !test.resizable {
				if b.Width != 400 || b.Height != 300 {
					t.Errorf("window bounds = %dx%d, want 400x300", b.Width, b.Height)
				}
			} else if b.Width <= 400 || b.Height <= 300 {
				t.Errorf("window bounds = %dx%d, want more than 400x300 around the native frame", b.Width, b.Height)
			}
		})

		t.Run(test.name+"/SetContentSize", func(t *testing.T) {
			opts := test.opts
			opts.Width, opts.Height, opts.UseContentSize = 400, 300, true
			w := newWindow(t, opts)
			w.SetContentSize(420, 280)
			width, height := w.ContentSize()
			if width != 420 || height != 280 {
				t.Fatalf("content size after SetContentSize = %dx%d, want 420x280", width, height)
			}
			b := w.Bounds()
			if fullClient || !test.resizable {
				if b.Width != 420 || b.Height != 280 {
					t.Errorf("window bounds after SetContentSize = %dx%d, want 420x280", b.Width, b.Height)
				}
			} else if b.Width <= 420 || b.Height <= 280 {
				t.Errorf("window bounds after SetContentSize = %dx%d, want more than 420x280 around the native frame", b.Width, b.Height)
			}
		})

		t.Run(test.name+"/SetContentBounds", func(t *testing.T) {
			opts := test.opts
			opts.Width, opts.Height, opts.UseContentSize = 400, 300, true
			w := newWindow(t, opts)
			want := mygo.Rectangle{X: 200, Y: 180, Width: 360, Height: 240}
			w.SetContentBounds(want)
			eventually(t, "the content where it was put", func() bool { return w.ContentBounds() == want })
			b := w.Bounds()
			if fullClient || !test.resizable {
				if b.Width != want.Width || b.Height != want.Height {
					t.Errorf("window bounds after SetContentBounds = %dx%d, want %dx%d", b.Width, b.Height, want.Width, want.Height)
				}
			} else if b.Width <= want.Width || b.Height <= want.Height {
				t.Errorf("window bounds after SetContentBounds = %dx%d, want more than %dx%d around the native frame", b.Width, b.Height, want.Width, want.Height)
			}
		})
	}
}
