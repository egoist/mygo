package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// occludedSurface is a testSurface that can be out of sight.
type occludedSurface struct {
	testSurface
	occluded bool
}

func (s *occludedSurface) Occluded() bool { return s.occluded }

// TestCaptureBuildsHeldFrame checks that capturing a window out of sight
// shows what changed since its last frame, which waited for it to show.
func TestCaptureBuildsHeldFrame(t *testing.T) {
	t.Setenv("MYGO_GPU", "0")
	s := &occludedSurface{}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	count, builds := 0, 0
	h.rt = newRuntime(func(c *context) {
		builds++
		coreTextf(c, "%d", count)
	}, h)
	frame := func() { h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) }
	frame()
	s.occluded = true
	count = 1
	h.rt.changed()
	frame()
	if builds != 1 || !h.rt.held {
		t.Fatalf("a change out of sight: %d builds, held %v", builds, h.rt.held)
	}
	if w, ht, pix := h.capture(); w == 0 || ht == 0 || len(pix) == 0 || builds != 2 {
		t.Fatalf("captured %dx%d after %d builds: the change not built", w, ht, builds)
	}
	if n := len(h.rt.labels); n == 0 || h.rt.labels[0].text != "1" {
		t.Errorf("captured the frame showing %v", h.rt.labels)
	}
}
