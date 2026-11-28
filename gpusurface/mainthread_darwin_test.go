//go:build darwin && !ios

package gpusurface

import (
	"sync"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// TestOnMain: a call from another goroutine runs on the main thread, and
// one made on the main thread runs at once rather than wait for the main
// queue it is draining.
func TestOnMain(t *testing.T) {
	if isMainThread() {
		t.Fatal("the test runs on the main thread")
	}
	var outer, inner bool
	onMain(func() {
		outer = isMainThread()
		onMain(func() { inner = isMainThread() })
	})
	if !outer || !inner {
		t.Fatalf("on the main thread: the call %v, the call it made %v", outer, inner)
	}
}

// TestSurfaceOffMainThread makes, uses and closes a surface from
// goroutines other than the main thread: New and Close put the surface's
// view in the window and take it out on the main thread, while frames
// present and the overlay's atlas changes on goroutines of their own.
func TestSurfaceOffMainThread(t *testing.T) {
	frame := testIOSurface(t, 64, 64)
	win, content, ui := testWindow(t, 400, 300)
	s, err := New(Options{Window: win, Composite: true})
	if err != nil {
		t.Fatal(err)
	}
	if w, h := s.ScreenSize(); w <= 0 || h <= 0 {
		t.Fatalf("ScreenSize %dx%d", w, h)
	}
	if err := s.SetDirect(true); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var presenting, atlases sync.WaitGroup
	presenting.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := s.Present(frame); err != nil && err != errNoDrawable {
				t.Error(err)
				return
			}
		}
	})
	glyph := []OverlayRect{{X: 8, Y: 8, W: 16, H: 16, Color: [4]float32{1, 1, 1, 1}, UV: [4]float32{0, 0, 1, 1}}}
	for g := range 4 {
		atlases.Go(func() {
			pix := make([]byte, 8*8)
			for i := range 50 {
				pix[(g+i)%len(pix)] = 255
				if err := s.SetOverlayAtlas(pix, 8, 8); err != nil {
					t.Error(err)
					return
				}
				s.SetOverlay(glyph)
			}
		})
	}
	atlases.Wait()
	close(stop)
	presenting.Wait()

	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	wait.For(t, "Close on another goroutine", closed)
	onMain(func() {
		if views := subviews(content); len(views) != 1 || views[0] != ui {
			t.Errorf("content subviews after Close %#x, want only the UI view %#x", views, ui)
		}
		if l := send(ui, "layer"); l != 0 && layerHidden(l) {
			t.Error("the UI view's layer is still hidden after Close ended direct mode")
		}
	})
}
