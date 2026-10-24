//go:build darwin || (windows && (amd64 || arm64)) || (linux && (amd64 || arm64))

package media

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestCamerasAreNamedAndDistinct(t *testing.T) {
	cameras, err := Cameras()
	if err != nil {
		t.Fatalf("Cameras: %v", err)
	}
	if len(cameras) == 0 {
		t.Skip("this machine offers no camera")
	}
	seen := make(map[string]bool, len(cameras))
	for _, camera := range cameras {
		if camera.ID == "" || camera.Name == "" {
			t.Errorf("the camera %+v has no id or no name", camera)
		}
		if seen[camera.ID] {
			t.Errorf("two cameras have the id %q", camera.ID)
		}
		seen[camera.ID] = true
	}
}

func TestACameraThatIsNotThereIsAnError(t *testing.T) {
	camera, err := OpenCamera("a camera this machine does not have")
	if err == nil {
		camera.Close()
		t.Fatal("opening a camera that is not there worked")
	}
}

// openTestCamera opens the system's camera, skipping the test where there is
// none and where the system has not let this app use it: a test never asks the
// person to answer a permission question.
//
// Windows has no answer until an app opens a device, so opening the camera is
// what says whether the app may use it. macOS has an answer before that, and
// opening a camera it has not answered for is what asks the question, which a
// test never does.
func openTestCamera(t *testing.T) *Camera {
	t.Helper()
	cameras, err := Cameras()
	if err != nil {
		t.Skipf("no camera to list: %v", err)
	}
	if len(cameras) == 0 {
		t.Skip("this machine offers no camera")
	}
	switch status := CameraPermission(); status {
	case PermissionDenied, PermissionRestricted:
		t.Skipf("this app may not use the camera: %s", status)
	case PermissionNotDetermined:
		if runtime.GOOS == "darwin" {
			t.Skipf("this app may not use the camera: %s", status)
		}
	}
	camera, err := OpenCamera(cameras[0].ID)
	if err != nil {
		if errors.Is(err, ErrPermissionDenied) {
			t.Skipf("this app may not use the camera: %v", err)
		}
		t.Skipf("no camera to open: %v", err)
	}
	t.Cleanup(func() { camera.Close() })
	return camera
}

func TestPicturesComeOutAsFrames(t *testing.T) {
	camera := openTestCamera(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	first, err := camera.NextFrame(ctx)
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}
	defer first.Release()
	if first.Format != PixelRGBA {
		t.Errorf("the first picture is %s, want RGBA", first.Format)
	}
	if first.Width <= 0 || first.Height <= 0 {
		t.Fatalf("the first picture is %dx%d", first.Width, first.Height)
	}
	if len(first.Planes) != 1 || len(first.Planes[0].Pix) != first.Width*first.Height*4 {
		t.Fatalf("the first picture has %d planes of the wrong size", len(first.Planes))
	}
	if stride := first.Planes[0].Stride; stride < first.Width*4 {
		t.Errorf("the first picture has a stride of %d for a width of %d", stride, first.Width)
	}
	if img := first.Image(); img == nil {
		t.Error("the picture makes no image")
	}

	second, err := camera.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the second NextFrame: %v", err)
	}
	defer second.Release()
	if second.PTS <= first.PTS {
		t.Errorf("the second picture is at %v, the first at %v", second.PTS, first.PTS)
	}
}

func TestCloseEndsThePictures(t *testing.T) {
	camera := openTestCamera(t)
	if err := camera.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := camera.NextFrame(ctx); !errors.Is(err, ErrEnd) {
		t.Fatalf("after Close, NextFrame is %v, want ErrEnd", err)
	}
	if err := camera.Close(); err != nil {
		t.Errorf("closing a closed camera: %v", err)
	}
}

// The queue under a camera is what makes a slow reader lose pictures rather
// than slow the camera down, and it is the same on every platform.

func TestAReaderThatFallsBehindLosesTheOldestPicture(t *testing.T) {
	q := newLiveFrames(2)
	for _, pts := range []time.Duration{1, 2, 3} {
		q.push(NewFrame(PixelRGBA, 1, 1, []Plane{{Pix: make([]byte, 4), Stride: 4}}, pts, 0, nil))
	}
	frame, err := q.next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if frame.PTS != 2 {
		t.Errorf("the next picture is at %v, want the second's at 2s", frame.PTS)
	}
	frame.Release()
}

func TestClosingTheQueueGivesBackThePicturesHeld(t *testing.T) {
	q := newLiveFrames(4)
	released := 0
	for i := 0; i < 2; i++ {
		q.push(NewFrame(PixelRGBA, 1, 1, []Plane{{Pix: make([]byte, 4), Stride: 4}}, 0, 0, func() { released++ }))
	}
	q.close()
	if released != 2 {
		t.Errorf("closing the queue released %d pictures, want 2", released)
	}
	if _, err := q.next(context.Background()); !errors.Is(err, ErrEnd) {
		t.Errorf("next after close is %v, want ErrEnd", err)
	}
	q.push(NewFrame(PixelRGBA, 1, 1, []Plane{{Pix: make([]byte, 4), Stride: 4}}, 0, 0, func() { released++ }))
	if released != 3 {
		t.Errorf("a picture pushed after close was not given back (released %d)", released)
	}
}

func TestNextWaitsForAPicture(t *testing.T) {
	q := newLiveFrames(4)
	go func() {
		q.push(NewFrame(PixelRGBA, 1, 1, []Plane{{Pix: make([]byte, 4), Stride: 4}}, 7, 0, nil))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	frame, err := q.next(ctx)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	defer frame.Release()
	if frame.PTS != 7 {
		t.Errorf("the picture that arrived is at %v, want 7s", frame.PTS)
	}
}

func TestNextStopsWhenTheContextIsDone(t *testing.T) {
	q := newLiveFrames(4)
	defer q.close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.next(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("next with a done context is %v, want context.Canceled", err)
	}
}
