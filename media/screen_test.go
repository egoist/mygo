//go:build darwin || (windows && (amd64 || arm64)) || (linux && (amd64 || arm64))

package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScreensAreNamedAndDistinct(t *testing.T) {
	screens, err := Screens()
	if err != nil {
		t.Skipf("this machine offers no screen to capture: %v", err)
	}
	if len(screens) == 0 {
		t.Skip("this machine offers no screen")
	}
	seen := make(map[string]bool, len(screens))
	for _, screen := range screens {
		if screen.ID == "" || screen.Name == "" {
			t.Errorf("the screen %+v has no id or no name", screen)
		}
		if seen[screen.ID] {
			t.Errorf("two screens have the id %q", screen.ID)
		}
		seen[screen.ID] = true
	}
}

func TestAScreenThatIsNotThereIsAnError(t *testing.T) {
	screen, err := OpenScreen("a screen this machine does not have")
	if err == nil {
		screen.Close()
		t.Fatal("opening a screen that is not there worked")
	}
}

// openTestScreen opens the system's screen, skipping the test where there is
// none and where the system has not let this app capture it: a test never asks
// the person to answer a permission question.
func openTestScreen(t *testing.T) *Screen {
	t.Helper()
	screens, err := Screens()
	if err != nil {
		t.Skipf("no screen to list: %v", err)
	}
	if len(screens) == 0 {
		t.Skip("this machine offers no screen")
	}
	switch status := ScreenPermission(); status {
	case PermissionDenied, PermissionRestricted:
		t.Skipf("this app may not capture the screen: %s", status)
	}
	screen, err := OpenScreen(screens[0].ID)
	if err != nil {
		if errors.Is(err, ErrPermissionDenied) {
			t.Skipf("this app may not capture the screen: %v", err)
		}
		t.Skipf("no screen to open: %v", err)
	}
	t.Cleanup(func() { screen.Close() })
	return screen
}

// TestPicturesOfTheScreen checks that a screen hands pictures over in the
// format the UI draws, and that closing it ends them rather than failing.
func TestPicturesOfTheScreen(t *testing.T) {
	screen := openTestScreen(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f, err := screen.NextFrame(ctx)
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skipf("this session cannot capture the screen: %v", err)
		}
		t.Fatalf("the screen's first picture: %v", err)
	}
	if f.Format != PixelRGBA {
		t.Fatalf("a screen's picture is %v, want RGBA", f.Format)
	}
	if f.Width <= 0 || f.Height <= 0 {
		t.Fatalf("a screen's picture is %dx%d", f.Width, f.Height)
	}
	if len(f.Planes) != 1 || len(f.Planes[0].Pix) != f.Width*f.Height*4 {
		t.Fatalf("a screen's picture is in %d planes of %d bytes", len(f.Planes), len(f.Planes[0].Pix))
	}
	if f.Planes[0].Stride < f.Width*4 {
		t.Fatalf("a screen's picture has a stride of %d for a width of %d", f.Planes[0].Stride, f.Width)
	}
	if f.Image() == nil {
		t.Fatal("a screen's picture is not an image")
	}
	if info := screen.Info(); info.Width > 0 && info.Height > 0 {
		if f.Width != info.Width || f.Height != info.Height {
			t.Fatalf("the screen says it is %dx%d, its picture is %dx%d", info.Width, info.Height, f.Width, f.Height)
		}
	}
	f.Release()
}

// TestCloseEndsTheScreen is a screen closed while it is being read: the reader
// is told there is no more, rather than left waiting.
func TestCloseEndsTheScreen(t *testing.T) {
	screen := openTestScreen(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if f, err := screen.NextFrame(ctx); err == nil {
		f.Release()
	} else if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("this session cannot capture the screen: %v", err)
	}
	if err := screen.Close(); err != nil {
		t.Fatalf("closing the screen: %v", err)
	}
	if f, err := screen.NextFrame(ctx); !errors.Is(err, ErrEnd) {
		if f != nil {
			f.Release()
		}
		t.Fatalf("reading a closed screen gave %v, want %v", err, ErrEnd)
	}
	if err := screen.Close(); err != nil {
		t.Fatalf("closing the screen twice: %v", err)
	}
}
