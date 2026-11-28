// Package gpusurface presents frames that are already on the GPU, as a
// hardware video decoder leaves them, into a window: no CPU copy of their
// pixels, and no wait for the display.
//
// A frame is an IOSurface on macOS and an ID3D11Texture2D on Windows, in
// BGRA, NV12 or P010. The surface wraps it as a texture once (a decoder
// cycles through a small pool of frames), samples it in one pass into the
// window's drawable, converting YUV to RGB in the shader, and presents at
// once: on macOS a CAMetalLayer of its own with two drawables and display
// sync off, on Windows a flip-discard swap chain presenting with sync
// interval 0 and tearing where the system allows it.
//
// Present is the hot path: it allocates nothing on the Go heap once each
// frame of the pool was seen. Timings, when asked for, record each frame's
// way from Present to the screen.
//
// One thread presents: Present, PresentTagged, PresentSlice, Resize and
// SetHDR10 are called from it, one at a time. SetOverlay and
// SetOverlayAtlas may be called from any goroutine. On macOS New, Close,
// SetDirect and ScreenSize may be called from any goroutine too: they do
// their AppKit work on the main thread, at once when called there and
// otherwise through the main dispatch queue, which the main thread's run
// loop must then be draining, as it does while a MyGo app runs. Close
// comes last, once no other call on the surface is under way.
//
// Every platform has the whole API, with the same signatures: where a
// feature is missing, its methods return ErrUnsupported or do nothing,
// and New returns ErrUnsupported on platforms other than macOS and
// Windows.
package gpusurface

import (
	"errors"
	"sync/atomic"
)

// Format is the pixel layout of a frame.
type Format uint8

const (
	FormatBGRA8 Format = iota + 1
	// FormatNV12 is 8-bit 4:2:0, luma plane then interleaved chroma,
	// BT.709 video range.
	FormatNV12
	// FormatP010 is NV12's layout with 10 bits a sample in the high bits
	// of 16, BT.709 video range.
	FormatP010
	// FormatBGR10A2 is 10-bit RGB in 32 bits, as an IOSurface 'l10r' holds
	// it.
	FormatBGR10A2
)

func (f Format) String() string {
	switch f {
	case FormatBGRA8:
		return "BGRA8"
	case FormatNV12:
		return "NV12"
	case FormatP010:
		return "P010"
	case FormatBGR10A2:
		return "BGR10A2"
	}
	return "unknown"
}

// Options configure a surface.
type Options struct {
	// Window is the window the surface fills: an NSWindow* on macOS, an
	// HWND on Windows (as mygo.Window.NativeHandle returns). On Windows the
	// surface makes the window's swap chain, so the window must have none.
	Window uintptr
	// Adapter picks the GPU on Windows: the first DXGI adapter whose
	// description contains it, ignoring case (as "4080" or "Radeon"). Empty
	// takes the system's default. On macOS the surface draws on the default
	// Metal device.
	Adapter string
	// Timings is how many frames, from the first, record a Timing.
	Timings int
	// Composite, on Windows, shows the frames through a DirectComposition
	// target on Window that is not topmost, under the window's own content
	// and the topmost target a MyGo window with Transparent draws through,
	// which shows the frames where it paints nothing. The window may then
	// have a swap chain of its own.
	//
	// On macOS the surface's layer goes in a view of its own at the back of
	// the window's content view, under MyGo's surface view, which shows the
	// frames where the UI paints nothing; the view follows the window's
	// size, and Resize sets only the drawables' size. Close removes the
	// view. Without Composite the layer is added to the content view's own
	// layer, which suits a window holding nothing else.
	Composite bool
	// Drawable10Bit makes the drawables BGR10A2 instead of BGRA8, on macOS.
	Drawable10Bit bool
	// HDR10 presents the frames as BT.2020 PQ on macOS: BGR10A2
	// drawables, the layer's extended dynamic range on and its colour space
	// kCGColorSpaceITUR_2100_PQ, YUV converted with the BT.2020 matrix and
	// the overlay mapped to PQ at SDR reference white. SetHDR10 turns it
	// off and on again. Windows ignores it.
	HDR10 bool
}

// Timing is the way of one frame from Present to the screen, in
// nanoseconds of the clock Now reads.
type Timing struct {
	// Submitted is when Present was called, Encoded when it returned with
	// the frame handed to the GPU and the compositor.
	Submitted, Encoded int64
	// Presented is when the frame reached the screen, as the platform
	// reports it, or 0 when it never did: a newer frame replaced it before
	// the next refresh, or the platform could not tell.
	Presented int64
}

// ErrUnsupported is the error of a feature the platform's surface lacks.
var ErrUnsupported = errors.New("gpusurface: not supported on this platform")

// ErrFormat is Present's error for a frame in a format the surface does
// not take.
var ErrFormat = errors.New("gpusurface: unsupported frame format")

// ErrHDRNeeds10Bit is SetHDR10's error on a macOS surface whose drawables
// are 8-bit: make it with Options.HDR10 or Drawable10Bit.
var ErrHDRNeeds10Bit = errors.New("gpusurface: HDR10 needs BGR10A2 drawables")

// timings records the frames' Timing, from the first, up to its capacity.
type timings struct {
	recs []Timing
	// next is the index of the next frame Present records; resolved counts
	// the frames whose Presented is final.
	next, resolved atomic.Int64
}

func (t *timings) start(submitted int64) int {
	i := int(t.next.Load())
	if i >= len(t.recs) {
		return -1
	}
	t.next.Store(int64(i + 1))
	t.recs[i].Submitted = submitted
	return i
}

func (t *timings) resolve(i int, presented int64) {
	if i < 0 || i >= len(t.recs) {
		return
	}
	atomic.StoreInt64(&t.recs[i].Presented, presented)
	t.resolved.Add(1)
}

// Timings returns the timing of each frame recorded, once Close returned.
func (s *Surface) Timings() []Timing {
	return s.timings.recs[:s.timings.next.Load()]
}
