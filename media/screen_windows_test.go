//go:build windows && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

// screenBuiltIn opens a screen whose pictures come from a function a session
// with no desktop can still call, which is how a bare remote shell tests the
// capture path itself.
func screenBuiltIn(t *testing.T, capture func(w, h int, dst []byte) error) *Screen {
	t.Helper()
	was := screenTestCapture
	screenTestCapture = capture
	t.Cleanup(func() { screenTestCapture = was })
	q := newLiveFrames(screenFrames)
	h, err := openScreen(ScreenInfo{ID: "test", Name: "test screen"}, q)
	if err != nil {
		t.Fatalf("opening a screen: %v", err)
	}
	screen := &Screen{q: q, h: h}
	t.Cleanup(func() { screen.Close() })
	return screen
}

// TestAScreenHandsOutPictures reads the pictures of a screen that needs no
// desktop, through the loop a real one is read with. GDI's pixels are blue,
// green, red and alpha, and a frame's are red, green, blue and alpha.
func TestAScreenHandsOutPictures(t *testing.T) {
	screen := screenBuiltIn(t, func(w, h int, dst []byte) error {
		for i := 0; i+3 < len(dst); i += 4 {
			dst[i] = 10   // blue
			dst[i+1] = 20 // green
			dst[i+2] = 30 // red
			dst[i+3] = 255
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := screen.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the screen's first picture: %v", err)
	}
	if first.Format != PixelRGBA {
		t.Fatalf("a screen's picture is %v, want RGBA", first.Format)
	}
	if first.Width != screenTestSize[0] || first.Height != screenTestSize[1] {
		t.Fatalf("a screen's picture is %dx%d, want %dx%d", first.Width, first.Height, screenTestSize[0], screenTestSize[1])
	}
	if got := first.Planes[0].Pix[:4]; got[0] != 30 || got[1] != 20 || got[2] != 10 || got[3] != 255 {
		t.Fatalf("a screen's first pixel is %v, want [30 20 10 255]", got)
	}
	second, err := screen.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the screen's second picture: %v", err)
	}
	defer second.Release()
	if second.PTS <= first.PTS {
		t.Fatalf("the second picture is due at %v, the first at %v", second.PTS, first.PTS)
	}
	first.Release()
}

// TestAScreenSaysWhenItBreaks is a capture that stopped working: the pictures
// end with what happened rather than with the end of nothing.
func TestAScreenSaysWhenItBreaks(t *testing.T) {
	calls := 0
	screen := screenBuiltIn(t, func(w, h int, dst []byte) error {
		calls++
		if calls > 2 {
			return errors.New("the desktop went away")
		}
		clear(dst)
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	for range 5 {
		var f *Frame
		if f, err = screen.NextFrame(ctx); err != nil {
			break
		}
		f.Release()
	}
	if err == nil {
		t.Fatal("a capture that broke handed out another picture")
	}
	if errors.Is(err, ErrEnd) {
		t.Fatalf("a capture that broke is an error, got %v", err)
	}
}
