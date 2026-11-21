package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// TestRoundFrame: a window whose system keeps square corners has its frames cut
// to the corner radius: a rounded clip around everything the frame draws, with
// the background filled inside it, so that the raster renderer and the GPU's
// shader antialias the same arc.
func TestRoundFrame(t *testing.T) {
	s := &scene.Scene{Background: scene.Color{A: 40}}
	if !roundFrame(s, 12, 100, 100) {
		t.Fatal("not rounded")
	}
	if s.Background.A != 0 {
		t.Fatalf("background beyond the corners: %v", s.Background)
	}
	if len(s.Ops) != 2 {
		t.Fatalf("ops: %d", len(s.Ops))
	}
	if s.Ops[0].Kind != scene.OpFill || s.Ops[0].Color.A != 40 {
		t.Fatalf("background fill: %v %v", s.Ops[0].Kind, s.Ops[0].Color)
	}
	if s.Ops[1].Kind != scene.OpPushClip || s.Ops[1].Radii != scene.Uniform(12) {
		t.Fatalf("clip: %v %v", s.Ops[1].Kind, s.Ops[1].Radii)
	}
	if roundFrame(&scene.Scene{}, 60, 100, 100) {
		t.Fatal("rounded a frame too small for the radius")
	}
	if roundFrame(&scene.Scene{}, 0, 100, 100) {
		t.Fatal("rounded a square frame")
	}
}
