package gpusurface

import (
	"sync"
	"testing"
	"unsafe"
)

// The Metal shader reads the rects as packed floats, 52 bytes each.
func TestOverlayRectLayout(t *testing.T) {
	if n := unsafe.Sizeof(OverlayRect{}); n != 52 {
		t.Fatalf("OverlayRect is %d bytes, the shader reads 52", n)
	}
}

// TestOverlayRectsNeverTorn: a reader on its own thread only ever sees one
// writer's whole overlay, never a mix of two, and in the end the newest.
func TestOverlayRectsNeverTorn(t *testing.T) {
	var o overlayRects
	o.init()
	const writers, rounds = 3, 4000
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			var rects [MaxOverlayRects]OverlayRect
			for i := range rounds {
				k := float32(w*rounds + i + 1)
				n := (w*rounds+i)%37 + 1
				for j := range n {
					rects[j] = OverlayRect{X: k, Y: float32(n)}
				}
				o.set(rects[:n])
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	check := func(rs []OverlayRect) {
		if len(rs) == 0 {
			return
		}
		for _, r := range rs {
			if r.X != rs[0].X || int(r.Y) != len(rs) {
				t.Fatalf("torn overlay: %d rects, first %+v, then %+v", len(rs), rs[0], r)
			}
		}
	}
	for {
		select {
		case <-done:
			o.set([]OverlayRect{{X: -1, Y: 1}})
			if rs := o.latest(); len(rs) != 1 || rs[0].X != -1 {
				t.Fatalf("latest after the last set: %+v", rs)
			}
			return
		default:
			check(o.latest())
		}
	}
}

func TestOverlaySetDropsBeyondMax(t *testing.T) {
	var o overlayRects
	o.init()
	o.set(make([]OverlayRect, MaxOverlayRects+10))
	if n := len(o.latest()); n != MaxOverlayRects {
		t.Fatalf("%d rects kept, want %d", n, MaxOverlayRects)
	}
	o.set(nil)
	if n := len(o.latest()); n != 0 {
		t.Fatalf("%d rects after an empty set", n)
	}
}

func TestFitRect(t *testing.T) {
	for _, c := range []struct {
		fw, fh, dw, dh uintptr
		fit            [4]float32
		bars           bool
	}{
		{3840, 2160, 3840, 2160, [4]float32{1, 1, 0, 0}, false},
		{1920, 1080, 3840, 2160, [4]float32{1, 1, 0, 0}, false},
		// 16:9 on a 32:9 screen: pillarboxed, 2560 of 5120 wide.
		{1920, 1080, 5120, 1440, [4]float32{0.5, 1, 0, 0}, true},
		// 16:9 on 16:10: letterboxed, 1890 of 2100 high, from row 105.
		{1920, 1080, 3360, 2100, [4]float32{1, 0.9, 0, 0}, true},
		// An odd margin starts on a whole pixel: 3 of 6 wide from 1.
		{1, 2, 6, 6, [4]float32{0.5, 1, -1.0 / 6, 0}, true},
	} {
		fit, bars := fitRect(c.fw, c.fh, c.dw, c.dh)
		for i := range fit {
			if d := fit[i] - c.fit[i]; d > 1e-6 || d < -1e-6 {
				t.Errorf("fitRect(%d, %d, %d, %d) = %v, want %v", c.fw, c.fh, c.dw, c.dh, fit, c.fit)
				break
			}
		}
		if bars != c.bars {
			t.Errorf("fitRect(%d, %d, %d, %d) bars %v, want %v", c.fw, c.fh, c.dw, c.dh, bars, c.bars)
		}
	}
}

func TestFitRectPixels(t *testing.T) {
	for _, c := range []struct{ fw, fh, dw, dh, x, y, w, h int }{
		{1920, 1080, 1920, 1080, 0, 0, 1920, 1080},
		{1920, 1080, 5120, 1440, 1280, 0, 2560, 1440},
		{5120, 1440, 1920, 1080, 0, 270, 1920, 540},
		{1920, 1080, 3024, 1964, 0, 131, 3024, 1701},
		{0, 0, 800, 600, 0, 0, 800, 600},
	} {
		if x, y, w, h := FitRect(c.fw, c.fh, c.dw, c.dh); x != c.x || y != c.y || w != c.w || h != c.h {
			t.Errorf("FitRect(%d, %d, %d, %d) = %d, %d, %d, %d; want %d, %d, %d, %d", c.fw, c.fh, c.dw, c.dh, x, y, w, h, c.x, c.y, c.w, c.h)
		}
	}
}
