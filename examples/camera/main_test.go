package main

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/wait"
	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

// A window that runs an update at once, so that a test can make a video
// without a window or the goroutine that pulls frames.
type testWindow struct{}

func (testWindow) Update(fn func()) { fn() }

// A source that has no picture to show, as a camera nobody has pointed at
// anything: it waits for the test to end rather than ending.
type stillSource struct{}

func (stillSource) NextFrame(ctx context.Context) (*media.Frame, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stillSource) Close() error { return nil }

// A source that says it stopped, as a camera taken away does.
type stoppedSource struct{}

func (stoppedSource) NextFrame(context.Context) (*media.Frame, error) { return nil, media.ErrEnd }
func (stoppedSource) Close() error                                    { return nil }

func TestTheCameraIsChosenByIDOrName(t *testing.T) {
	cameras := []media.CameraInfo{
		{ID: "built-in", Name: "Built-in camera"},
		{ID: "usb", Name: "ASUS FHD webcam"},
	}
	for _, c := range []struct {
		id   string
		want string
	}{
		{"", "built-in"},
		{"usb", "usb"},
		{"ASUS FHD webcam", "usb"},
	} {
		got, err := chooseCamera(cameras, c.id)
		if err != nil {
			t.Errorf("chooseCamera(%q): %v", c.id, err)
			continue
		}
		if got.ID != c.want {
			t.Errorf("chooseCamera(%q) chose %q, want %q", c.id, got.ID, c.want)
		}
	}
	if _, err := chooseCamera(cameras, "a camera this machine does not have"); err == nil {
		t.Error("choosing a camera that is not there worked")
	}
	if _, err := chooseCamera(nil, ""); !errors.Is(err, media.ErrNoCamera) {
		t.Errorf("choosing from no cameras failed with %v, want ErrNoCamera", err)
	}
}

func TestTheWindowShowsTheCameraAndThatItIsLive(t *testing.T) {
	v := &viewer{name: "ASUS FHD webcam", video: mediaui.NewVideo(testWindow{}, stillSource{})}
	tt := ui.NewTester(v.view, 480, 300)
	if !tt.HasText("ASUS FHD webcam") {
		t.Errorf("the window shows %q, without the camera's name", tt.Texts())
	}
	if !tt.HasText("live") {
		t.Errorf("the window shows %q, without saying the camera is live", tt.Texts())
	}
}

func TestTheWindowSaysWhenTheCameraStopped(t *testing.T) {
	v := &viewer{name: "ASUS FHD webcam", video: mediaui.NewVideo(testWindow{}, stoppedSource{})}
	v.video.Start()
	wait.Until(t, "the camera to stop", func() bool { return v.video.Err() != nil })
	tt := ui.NewTester(v.view, 480, 300)
	if !tt.HasText("the camera stopped") {
		t.Errorf("the window shows %q, without saying the camera stopped", tt.Texts())
	}
}
