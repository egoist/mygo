//go:build darwin && !ios

package gpusurface

import (
	"errors"
	"testing"
)

// TestHDR10Layer: an HDR10 surface's layer wants extended dynamic range
// in a colour space of its own, its BT.2020 and PQ overlay pipelines
// compile, and it presents with an overlay; SetHDR10(false) gives the
// layer back the colour space and range it had, and on again takes them.
// An 8-bit surface refuses HDR10.
func TestHDR10Layer(t *testing.T) {
	win, _, _ := testWindow(t, 400, 300)
	frame := testIOSurface(t, 64, 64)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win, Composite: true, HDR10: true}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(s.Close) })
	if s.yuv2020Pipe == 0 || s.overlayPQPipe == 0 || s.pixelFormat != pixelFormatBGR10A2Unorm {
		t.Fatalf("pipelines %#x/%#x, format %d", s.yuv2020Pipe, s.overlayPQPipe, s.pixelFormat)
	}
	edr, space := s.layerHDR()
	if !s.HDR10() || !edr || space == 0 || space == s.sdrSpace {
		t.Fatalf("HDR10 %v: EDR %v, colour space %#x (SDR %#x)", s.HDR10(), edr, space, s.sdrSpace)
	}
	s.SetOverlay([]OverlayRect{{X: 8, Y: 8, W: 100, H: 40, Radius: 6, Color: [4]float32{1, 1, 1, 0.9}}})
	for range 3 {
		if err := s.Present(frame); err != nil && err != errNoDrawable {
			t.Fatal(err)
		}
	}

	if err := s.SetHDR10(false); err != nil {
		t.Fatal(err)
	}
	if edr, space := s.layerHDR(); s.HDR10() || edr || space != s.sdrSpace {
		t.Errorf("after SetHDR10(false): HDR10 %v, EDR %v, colour space %#x, want %#x", s.HDR10(), edr, space, s.sdrSpace)
	}
	if err := s.Present(frame); err != nil && err != errNoDrawable {
		t.Fatal(err)
	}
	if err := s.SetHDR10(true); err != nil {
		t.Fatal(err)
	}
	if edr, space := s.layerHDR(); !edr || space == s.sdrSpace {
		t.Errorf("HDR10 again: EDR %v, colour space %#x", edr, space)
	}

	win2, _, _ := testWindow(t, 200, 200)
	var sdr *Surface
	onMain(func() { sdr, err = New(Options{Window: win2, Composite: true}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(sdr.Close) })
	if err := sdr.SetHDR10(true); !errors.Is(err, ErrHDRNeeds10Bit) {
		t.Errorf("8-bit surface SetHDR10: %v, want ErrHDRNeeds10Bit", err)
	}
}
