//go:build windows && (amd64 || arm64)

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestCaptionlessWindowSize pins the sizes of a window without a caption,
// now that its client area covers the whole window (frameCalcSize). The
// frame around it must not grow it: a captionless window is the size its
// options ask for, and its page is the size UseContentSize asks for.
//
// Before this, outerSize still added the invisible borders of a resizable
// window, which frameCalcSize no longer takes out of the client area: a
// 400x300 window with UseContentSize came out 416x308 on Windows 10 and 11,
// and SetContentSize/SetContentBounds grew it by the same borders.
//
// A hidden title bar keeps one pixel at the top on Windows 11 for the line
// DWM draws there, so its page may be one pixel shorter than its window, or
// its window one pixel taller than its page. Nothing else may differ.
func TestCaptionlessWindowSize(t *testing.T) {
	for _, test := range []struct {
		name        string
		opts        mygo.WindowOptions
		hiddenTitle bool
	}{
		{"frameless", mygo.WindowOptions{Frameless: true}, false},
		{"fixed frameless", mygo.WindowOptions{Frameless: true, DisableResize: true}, false},
		{"hidden title bar", mygo.WindowOptions{TitleBarStyle: mygo.TitleBarHidden}, true},
		{"fixed hidden title bar", mygo.WindowOptions{TitleBarStyle: mygo.TitleBarHidden, DisableResize: true}, true},
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
			if width != 400 || height != 300 && !(test.hiddenTitle && height == 299) {
				t.Errorf("content size = %dx%d, want 400x300 (a hidden title bar may keep one pixel on Windows 11)", width, height)
			}
			// A resize after the window exists takes the size it asks for too:
			// WM_NCCALCSIZE gives the page the whole window, so the window
			// around it must not add the borders back.
			resized := mygo.Rectangle{X: 200, Y: 180, Width: 420, Height: 280}
			w.SetBounds(resized)
			if b := w.Bounds(); b.Width != 420 || b.Height != 280 {
				t.Errorf("window bounds after SetBounds = %dx%d, want 420x280", b.Width, b.Height)
			}
			width, height = w.ContentSize()
			if width != 420 || height != 280 && !(test.hiddenTitle && height == 279) {
				t.Errorf("content size after SetBounds = %dx%d, want 420x280 (a hidden title bar may keep one pixel on Windows 11)", width, height)
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
			if b.Width != 400 || b.Height != 300 && !(test.hiddenTitle && b.Height == 301) {
				t.Errorf("window bounds = %dx%d, want 400x300 (a hidden title bar may need one more pixel on Windows 11)", b.Width, b.Height)
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
			if b.Width != 420 || b.Height != 280 && !(test.hiddenTitle && b.Height == 281) {
				t.Errorf("window bounds after SetContentSize = %dx%d, want 420x280 (a hidden title bar may need one more pixel on Windows 11)", b.Width, b.Height)
			}
		})

		t.Run(test.name+"/SetContentBounds", func(t *testing.T) {
			opts := test.opts
			opts.Width, opts.Height, opts.UseContentSize = 400, 300, true
			w := newWindow(t, opts)
			want := mygo.Rectangle{X: 200, Y: 180, Width: 360, Height: 240}
			w.SetContentBounds(want)
			eventually(t, "the content where it was put", func() bool { return w.ContentBounds() == want })
			if b := w.Bounds(); b.Width != want.Width || b.Height != want.Height && !(test.hiddenTitle && b.Height == want.Height+1) {
				t.Errorf("window bounds after SetContentBounds = %dx%d, want %dx%d (a hidden title bar may need one more pixel on Windows 11)",
					b.Width, b.Height, want.Width, want.Height)
			}
		})
	}
}
