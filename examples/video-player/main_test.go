package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

// A window that runs an update at once, so that a test can make a video
// without a window or the goroutine that pulls frames.
type testWindow struct{}

func (testWindow) Update(fn func()) { fn() }

// The tests play the fixture the media package tests with, without sound:
// they would sound on the machine running them.
const testClip = "../../media/testdata/clip.mp4"

func newTestVideo(t *testing.T) (*videoPlayer, *ui.Tester) {
	t.Helper()
	med, err := media.Open(testClip, media.WithAudio(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { med.Close() })
	a := &videoPlayer{path: testClip, med: med, fit: ui.Contain}
	a.video = mediaui.NewVideo(testWindow{}, med)
	return a, ui.NewTester(a.view, 480, 300)
}

func TestView(t *testing.T) {
	a, tt := newTestVideo(t)
	if !tt.HasText("0:00") || !tt.HasText("0:02") {
		t.Errorf("the bar shows %q, want the position and the length", tt.Texts())
	}
	if a.fit != ui.Contain {
		t.Errorf("the video fills as %v, want Contain", a.fit)
	}
	tt.Key(0, ui.KeyF)
	if a.fit != ui.Cover {
		t.Errorf("F left the video filling as %v, want Cover", a.fit)
	}
}

func TestSeeking(t *testing.T) {
	a, tt := newTestVideo(t)
	// The file is two seconds long: the arrows move five, so either way
	// they land against one of its ends.
	tt.Key(0, ui.KeyRight)
	if got := a.med.Position(); got < 1900*time.Millisecond || got > 2100*time.Millisecond {
		t.Errorf("the right arrow left playback at %v, want the end of the file", got)
	}
	tt.Key(0, ui.KeyLeft)
	// Playback has started again by the time it is read, so this is the
	// start of the file and not much more than it.
	if got := a.med.Position(); got > 100*time.Millisecond {
		t.Errorf("the left arrow left playback at %v, want the start", got)
	}
}

func TestSpacePauses(t *testing.T) {
	a, tt := newTestVideo(t)
	if !a.med.Playing() {
		t.Fatal("the player is not playing to start with")
	}
	tt.Key(0, ui.KeySpace)
	if a.med.Playing() {
		t.Error("Space left the player playing")
	}
	if !tt.HasText("paused") {
		t.Errorf("the bar shows %q, without saying it is paused", tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	if !a.med.Playing() {
		t.Error("Space did not start the player again")
	}
}

func TestClock(t *testing.T) {
	for _, c := range []struct {
		in   time.Duration
		want string
	}{
		{0, "0:00"},
		{9 * time.Second, "0:09"},
		{65 * time.Second, "1:05"},
		{-time.Second, "0:00"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2:03:04"},
	} {
		if got := clock(c.in); got != c.want {
			t.Errorf("clock(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
