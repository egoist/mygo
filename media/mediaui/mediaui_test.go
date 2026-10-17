//go:build darwin

package mediaui

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
	"github.com/egoist/mygo/media"
)

// testClip is the media package's own fixture: two seconds of a 160×120
// H.264 file with AAC sound.
const testClip = "../testdata/clip.mp4"

// A window that runs an update at once, as the main thread would when it is
// not busy, and counts them.
type testWindow struct {
	mu      sync.Mutex
	updates int
}

func (w *testWindow) Update(fn func()) {
	fn()
	w.mu.Lock()
	w.updates++
	w.mu.Unlock()
}

func (w *testWindow) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.updates
}

// openTest opens the fixture silently: the tests would sound on the machine
// running them, and a source with no sound is timed by the wall.
func openTest(t *testing.T) *media.Player {
	t.Helper()
	med, err := media.Open(testClip, media.WithAudio(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { med.Close() })
	return med
}

// TestOnlyTheLastFrameIsStillShown checks the rule the bitmap's lifetime
// rests on: the frame whose pixels the bitmap has just stopped showing is
// given up on the main thread as the next one is shown, and the frame being
// shown now is not given up until its own turn comes.
func TestOnlyTheLastFrameIsStillShown(t *testing.T) {
	var mu sync.Mutex
	var released []*media.Frame
	release := releaseFrame
	releaseFrame = func(f *media.Frame) {
		mu.Lock()
		released = append(released, f)
		mu.Unlock()
		release(f)
	}
	t.Cleanup(func() { releaseFrame = release })

	w := &testWindow{}
	v := NewVideo(w, openTest(t))
	v.Start()
	wait.Until(t, "the file to play to its end", func() bool { return v.Err() != nil })
	if err := v.Err(); !errors.Is(err, media.ErrEnd) {
		t.Fatalf("the video stopped with %v, want the end of the file", err)
	}

	mu.Lock()
	defer mu.Unlock()
	updates := w.count()
	if updates < 15 {
		t.Fatalf("%d frames were shown, want the file's own", updates)
	}
	if len(released) != updates-1 {
		t.Errorf("%d frames were shown and %d given up: the last one should still be shown",
			updates, len(released))
	}
	seen := make(map[*media.Frame]bool, len(released))
	var last time.Duration
	for i, f := range released {
		if seen[f] {
			t.Errorf("frame %d was given up twice", i)
		}
		seen[f] = true
		if i > 0 && f.PTS <= last {
			t.Errorf("frame %d was given up at %v, after %v", i, f.PTS, last)
		}
		last = f.PTS
	}
}

// TestStopWaitsAndStartsAgain checks that stopping holds until the frame in
// flight has been dealt with, that a stopped video is not an error, and that
// a stopped video can be started again.
func TestStopWaitsAndStartsAgain(t *testing.T) {
	w := &testWindow{}
	v := NewVideo(w, openTest(t))
	v.Start()
	wait.Until(t, "a first frame", func() bool { return w.count() > 0 })
	v.Stop()
	if err := v.Err(); err != nil {
		t.Errorf("a stopped video reports %v, want nothing", err)
	}
	before := w.count()
	v.Start()
	wait.Until(t, "another frame", func() bool { return w.count() > before })
	v.Stop()
	if err := v.Err(); err != nil {
		t.Errorf("after stopping again: %v", err)
	}
}

// TestStopBeforeAnyFrame checks that a video stopped at once still stops.
func TestStopBeforeAnyFrame(t *testing.T) {
	v := NewVideo(&testWindow{}, openTest(t))
	v.Start()
	v.Stop()
	if err := v.Err(); err != nil {
		t.Errorf("a video stopped at once reports %v", err)
	}
	// Stopping twice is nothing rather than a mistake.
	v.Stop()
}

// TestTheBitmapsSizeFollowsThePicture checks that the element shows a picture
// of the video's own size once frames arrive, the placeholder before that.
func TestTheBitmapsSizeFollowsThePicture(t *testing.T) {
	w := &testWindow{}
	v := NewVideo(w, openTest(t))
	if width, height := v.bitmap.Size(); width != 16 || height != 9 {
		t.Errorf("the placeholder is %dx%d, want 16x9", width, height)
	}
	v.Start()
	wait.Until(t, "a first frame", func() bool { return w.count() > 0 })
	if width, height := v.bitmap.Size(); width != 160 || height != 120 {
		t.Errorf("the bitmap is %dx%d, want the picture's 160x120", width, height)
	}
	v.Stop()
}
