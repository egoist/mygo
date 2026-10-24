//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

// screenBuiltIn opens a screen whose pictures come from a source a machine
// always has, which is how a machine with no display server tests the capture
// path itself.
func screenBuiltIn(t *testing.T, source string) *Screen {
	t.Helper()
	was := screenTestSource
	screenTestSource = source
	t.Cleanup(func() { screenTestSource = was })
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
// display, from the pipeline a real one is read through.
func TestAScreenHandsOutPictures(t *testing.T) {
	screen := screenBuiltIn(t, "videotestsrc is-live=true")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := screen.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the screen's first picture: %v", err)
	}
	if first.Format != PixelRGBA {
		t.Fatalf("a screen's picture is %v, want RGBA", first.Format)
	}
	if first.Width <= 0 || first.Height <= 0 {
		t.Fatalf("a screen's picture is %dx%d", first.Width, first.Height)
	}
	if len(first.Planes) != 1 || len(first.Planes[0].Pix) != first.Width*first.Height*4 {
		t.Fatalf("a screen's picture is in %d planes of %d bytes", len(first.Planes), len(first.Planes[0].Pix))
	}
	if first.Image() == nil {
		t.Fatal("a screen's picture is not an image")
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

// TestAScreenTheMachineCannotMakeNamesTheMissingPart is a machine with no X11
// plugin: it reports the component it does not have, which is a feature this
// machine lacks.
func TestAScreenTheMachineCannotMakeNamesTheMissingPart(t *testing.T) {
	was := screenTestSource
	screenTestSource = "nosuchelement"
	t.Cleanup(func() { screenTestSource = was })
	_, err := openScreen(ScreenInfo{ID: "test", Name: "test screen"}, newLiveFrames(screenFrames))
	if err == nil {
		t.Fatal("a screen built on an element the machine does not have opened")
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("a missing plugin is a feature this machine lacks, got %v", err)
	}
	var missing *MissingComponentError
	if !errors.As(err, &missing) || missing.Component != "nosuchelement" {
		t.Fatalf("a missing plugin names the part, got %v", err)
	}
}
