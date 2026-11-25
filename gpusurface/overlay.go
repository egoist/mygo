package gpusurface

import (
	"sync"
	"sync/atomic"
)

// OverlayRect is one rectangle the overlay draws over the frames, in
// drawable pixels from the top left. Color is straight (not premultiplied)
// sRGB. A rect with UV all zero is solid, its corners rounded to Radius;
// any other UV is a glyph, its coverage read from the overlay atlas
// between u0,v0 and u1,v1.
type OverlayRect struct {
	X, Y, W, H float32
	Radius     float32
	Color      [4]float32
	UV         [4]float32
}

// MaxOverlayRects bounds the rects of one overlay; SetOverlay drops the
// rest.
const MaxOverlayRects = 512

// overlayRects passes overlays from SetOverlay, on any goroutine, to the
// presenting thread, through three slots: the writer fills its back slot
// and swaps it with the middle one, the reader swaps the middle one with
// its front slot when it is newer. Neither waits on the other and neither
// touches the slot the other holds.
type overlayRects struct {
	mu     sync.Mutex // writers
	slots  [3][MaxOverlayRects]OverlayRect
	counts [3]int
	back   uint32 // the writer's, under mu
	front  uint32 // the reader's
	// middle is the middle slot's index, with fresh set when the writer
	// left it and the reader has not taken it yet.
	middle atomic.Uint32
}

const overlayFresh = 1 << 8

func (o *overlayRects) init() {
	o.back, o.front = 0, 2
	o.middle.Store(1)
}

func (o *overlayRects) set(rects []OverlayRect) {
	o.mu.Lock()
	n := copy(o.slots[o.back][:], rects)
	o.counts[o.back] = n
	o.back = o.middle.Swap(o.back|overlayFresh) &^ overlayFresh
	o.mu.Unlock()
}

// latest returns the newest overlay set. Only the presenting thread calls
// it; the slice stays valid until its next call.
func (o *overlayRects) latest() []OverlayRect {
	if o.middle.Load()&overlayFresh != 0 {
		o.front = o.middle.Swap(o.front) &^ overlayFresh
	}
	return o.slots[o.front][:o.counts[o.front]]
}
