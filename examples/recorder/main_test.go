package main

import (
	"context"
	"errors"
	"path/filepath"
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

// framesSource makes a few pictures of moving colour, as a camera pointed at a
// screen does, and then ends. It needs no camera and no screen.
type framesSource struct {
	width, height int
	frames        int
	done          int
	pts           time.Duration
}

func (s *framesSource) NextFrame(ctx context.Context) (*media.Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.done == s.frames {
		return nil, media.ErrEnd
	}
	pix := make([]byte, s.width*s.height*4)
	for i := 0; i < s.width*s.height; i++ {
		pix[i*4] = byte(i * s.done) // red
		pix[i*4+1] = byte(i / 2)    // green
		pix[i*4+2] = byte(200)      // blue
		pix[i*4+3] = 255            // opaque
	}
	f := media.NewFrame(media.PixelRGBA, s.width, s.height, []media.Plane{{Pix: pix, Stride: s.width * 4}}, s.pts, time.Second/30, nil)
	s.pts += time.Second / 30
	s.done++
	return f, nil
}

func (s *framesSource) Close() error { return nil }

func TestTheRecorderSaysWhatItRecords(t *testing.T) {
	r := &recorder{name: "Built-in screen", out: "screen.mp4"}
	tt := ui.NewTester(r.view, 480, 300)
	if !tt.HasText("recording Built-in screen to screen.mp4") {
		t.Errorf("the window shows %q, without saying what is being recorded", tt.Texts())
	}
}

func TestTheRecorderSaysWhereTheFileWent(t *testing.T) {
	r := &recorder{name: "Built-in screen", out: "screen.mp4"}
	r.done = true
	tt := ui.NewTester(r.view, 480, 300)
	if !tt.HasText("saved screen.mp4") {
		t.Errorf("the window shows %q, without saying what was saved", tt.Texts())
	}
}

func TestTheRecorderSaysWhenItFailed(t *testing.T) {
	r := &recorder{name: "Built-in screen", out: "screen.mp4"}
	r.done = true
	r.err = errors.New("media: this machine has no H.264 encoder")
	tt := ui.NewTester(r.view, 480, 300)
	if !tt.HasText("media: this machine has no H.264 encoder") {
		t.Errorf("the window shows %q, without saying why the recording failed", tt.Texts())
	}
}

func TestTheRecorderShowsTheFileItRecorded(t *testing.T) {
	r := &recorder{name: "Built-in screen", out: "screen.mp4"}
	r.done = true
	r.video = mediaui.NewVideo(testWindow{}, &framesSource{width: 32, height: 32, frames: 2})
	tt := ui.NewTester(r.view, 480, 300)
	if !tt.HasText("saved screen.mp4") {
		t.Errorf("the window shows %q, without saying what was saved", tt.Texts())
	}
}

func TestAHandMadeSourceRecordsIntoAFileAPlayerCanOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.mp4")
	src := &framesSource{width: 64, height: 64, frames: 10}
	if err := media.Record(context.Background(), path, src, media.WithRecordFrameRate(30)); err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skipf("this machine has no encoder wired up: %v", err)
		}
		t.Fatalf("recording: %v", err)
	}
	player, err := media.Open(path)
	if err != nil {
		t.Fatalf("opening what was recorded: %v", err)
	}
	defer player.Close()
	var video media.Track
	for _, track := range player.Tracks() {
		if track.Kind == media.TrackVideo {
			video = track
		}
	}
	if video.Codec != media.CodecH264 {
		t.Fatalf("the recording has %q pictures, want H.264", video.Codec)
	}
	if video.Width != 64 || video.Height != 64 {
		t.Errorf("the recording says %dx%d, want 64x64", video.Width, video.Height)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frame, err := player.NextFrame(ctx)
	if err != nil {
		t.Fatalf("pulling a picture of what was recorded: %v", err)
	}
	defer frame.Release()
	if frame.Width != 64 || frame.Height != 64 {
		t.Errorf("the picture is %dx%d, want 64x64", frame.Width, frame.Height)
	}
}

func TestTheScreenIsChosenByIDOrName(t *testing.T) {
	screens := []media.ScreenInfo{
		{ID: "1", Name: "Built-in display"},
		{ID: "2", Name: "Studio display"},
	}
	for _, c := range []struct {
		id   string
		want string
	}{
		{"", "1"},
		{"2", "2"},
		{"Studio display", "2"},
	} {
		got, err := chooseScreen(screens, c.id)
		if err != nil {
			t.Errorf("chooseScreen(%q): %v", c.id, err)
			continue
		}
		if got.ID != c.want {
			t.Errorf("chooseScreen(%q) chose %q, want %q", c.id, got.ID, c.want)
		}
	}
	if _, err := chooseScreen(screens, "a screen this machine does not have"); err == nil {
		t.Error("choosing a screen that is not there worked")
	}
	if _, err := chooseScreen(nil, ""); !errors.Is(err, media.ErrNoScreen) {
		t.Errorf("choosing from no screens failed with %v, want ErrNoScreen", err)
	}
}
