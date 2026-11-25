//go:build !(darwin && !ios) && !windows

package gpusurface

import "errors"

// Surface presents frames into a window, on macOS and Windows only.
type Surface struct{ timings timings }

// New returns an error: there is no GPU surface on this platform yet.
func New(Options) (*Surface, error) {
	return nil, errors.New("gpusurface: not supported on this platform")
}

func (s *Surface) Present(uintptr) error {
	return errors.New("gpusurface: not supported on this platform")
}
func (s *Surface) Resize(w, h int) error {
	return errors.New("gpusurface: not supported on this platform")
}
func (s *Surface) Size() (w, h int) { return 0, 0 }
func (s *Surface) Close()           {}
