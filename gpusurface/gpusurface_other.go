//go:build !(darwin && !ios) && !windows

package gpusurface

import "time"

// Surface presents frames into a window, on macOS and Windows only.
type Surface struct{ timings timings }

// New returns ErrUnsupported: there is no GPU surface on this platform yet.
func New(Options) (*Surface, error) { return nil, ErrUnsupported }

var epoch = time.Now()

// Now returns the time in nanoseconds of the clock Timing uses: here the
// monotonic time since the program started.
func Now() int64 { return int64(time.Since(epoch)) }

// Present returns ErrUnsupported.
func (s *Surface) Present(uintptr) error { return ErrUnsupported }

// PresentTagged returns ErrUnsupported.
func (s *Surface) PresentTagged(uintptr, int64) error { return ErrUnsupported }

// PresentSlice returns ErrUnsupported.
func (s *Surface) PresentSlice(uintptr, int, int, int) error { return ErrUnsupported }

// OnPresented does nothing: no frame is ever presented.
func (s *Surface) OnPresented(func(tag, submitted, presented int64)) {}

// Resize returns ErrUnsupported.
func (s *Surface) Resize(w, h int) error { return ErrUnsupported }

// Size returns 0, 0.
func (s *Surface) Size() (w, h int) { return 0, 0 }

// Device, Queue, Context and SwapChain return 0: there is no GPU object.
func (s *Surface) Device() uintptr    { return 0 }
func (s *Surface) Queue() uintptr     { return 0 }
func (s *Surface) Context() uintptr   { return 0 }
func (s *Surface) SwapChain() uintptr { return 0 }

// Tearing reports false.
func (s *Surface) Tearing() bool { return false }

// AdapterName returns "".
func (s *Surface) AdapterName() string { return "" }

// Close does nothing.
func (s *Surface) Close() {}
