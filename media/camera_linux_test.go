//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"errors"

	"testing"
	"time"
)

// cameraBuiltIn opens a camera whose pictures come from a source a machine
// always has, which is how a machine with no camera tests the capture path
// itself.
func cameraBuiltIn(t *testing.T, source string) *Camera {
	t.Helper()
	was := cameraTestSource
	cameraTestSource = source
	t.Cleanup(func() { cameraTestSource = was })
	q := newLiveFrames(cameraFrames)
	h, err := openCamera(CameraInfo{ID: "test", Name: "test camera"}, q)
	if err != nil {
		t.Fatalf("opening a camera: %v", err)
	}
	cam := &Camera{q: q, h: h}
	t.Cleanup(func() { cam.Close() })
	return cam
}

// TestACameraHandsOutPictures reads the pictures of a camera that needs no
// hardware, from the pipeline a real one is read through.
func TestACameraHandsOutPictures(t *testing.T) {
	cam := cameraBuiltIn(t, "videotestsrc is-live=true")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := cam.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the camera's first picture: %v", err)
	}
	if first.Format != PixelRGBA {
		t.Fatalf("a camera's picture is %v, want RGBA", first.Format)
	}
	if first.Width <= 0 || first.Height <= 0 {
		t.Fatalf("a camera's picture is %dx%d", first.Width, first.Height)
	}
	if len(first.Planes) != 1 || len(first.Planes[0].Pix) != first.Width*first.Height*4 {
		t.Fatalf("a camera's picture is in %d planes of %d bytes", len(first.Planes), len(first.Planes[0].Pix))
	}
	if first.Planes[0].Stride < first.Width*4 {
		t.Fatalf("a camera's picture has a stride of %d for a width of %d", first.Planes[0].Stride, first.Width)
	}
	if first.Image() == nil {
		t.Fatal("a camera's picture is not an image")
	}
	second, err := cam.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the camera's second picture: %v", err)
	}
	defer second.Release()
	if second.PTS <= first.PTS {
		t.Fatalf("the second picture is due at %v, the first at %v", second.PTS, first.PTS)
	}
	first.Release()
}

// TestACameraSaysWhenItStops is a camera whose device went away: the pictures
// end with what happened rather than with the end of nothing.
func TestACameraSaysWhenItStops(t *testing.T) {
	cam := cameraBuiltIn(t, "videotestsrc is-live=true num-buffers=3")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	for range 3 {
		var f *Frame
		if f, err = cam.NextFrame(ctx); err != nil {
			break
		}
		f.Release()
	}
	if err == nil {
		if _, err = cam.NextFrame(ctx); err == nil {
			t.Fatal("a camera that stopped handed out another picture")
		}
	}
	if errors.Is(err, ErrEnd) {
		t.Fatalf("a camera that stopped is an error, got %v", err)
	}
}

// TestACameraTheMachineCannotMakeNamesTheMissingPart is a machine with no
// camera plugin: it reports the component it does not have, which is a feature
// this machine lacks.
func TestACameraTheMachineCannotMakeNamesTheMissingPart(t *testing.T) {
	was := cameraTestSource
	cameraTestSource = "nosuchelement"
	t.Cleanup(func() { cameraTestSource = was })
	_, err := openCamera(CameraInfo{ID: "test", Name: "test camera"}, newLiveFrames(cameraFrames))
	if err == nil {
		t.Fatal("a camera built on an element the machine does not have opened")
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("a missing plugin is a feature this machine lacks, got %v", err)
	}
	var missing *MissingComponentError
	if !errors.As(err, &missing) || missing.Component != "nosuchelement" {
		t.Fatalf("a missing plugin names the part, got %v", err)
	}
}
