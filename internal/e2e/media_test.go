//go:build !js

package e2e

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

// TestVideoInAWindow plays the media package's own fixture in a real window:
// the platform reader hands out frames, the video pulls them off the main
// thread and updates the bitmap on the main thread, and the window draws it.
// The unit tests fake that last step, which is what this covers.
func TestVideoInAWindow(t *testing.T) {
	med, err := media.Open("../../media/testdata/clip.mp4", media.WithAudio(false))
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skipf("no media on this platform yet: %v", err)
		}
		t.Fatal(err)
	}
	defer med.Close()
	// The view runs on the main thread and the test on another, so the
	// video it draws is handed over rather than shared.
	var video atomic.Pointer[mediaui.Video]
	var draws atomic.Int32
	w := newWindow(t, mygo.WindowOptions{Title: "Video", Width: 320, Height: 240, Content: ui.View(func(c *ui.Context) {
		v := video.Load()
		if v == nil {
			return
		}
		v.Element(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { draws.Add(1) })
	})})
	v := mediaui.NewVideo(w, med)
	video.Store(v)
	v.Start()
	eventually(t, "the video's frames", func() bool { return draws.Load() > 3 })
	eventually(t, "the end of the file", func() bool { return errors.Is(v.Err(), media.ErrEnd) })
	v.Stop()
}
