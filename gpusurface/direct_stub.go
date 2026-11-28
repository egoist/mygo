//go:build !(darwin && !ios)

package gpusurface

// SetDirect returns ErrUnsupported: only macOS has a direct mode.
func (s *Surface) SetDirect(bool) error { return ErrUnsupported }

// Direct reports false: only macOS has a direct mode.
func (s *Surface) Direct() bool { return false }

// SetOverlay does nothing: only macOS draws an overlay yet.
func (s *Surface) SetOverlay([]OverlayRect) {}

// SetOverlayAtlas returns ErrUnsupported: only macOS draws an overlay yet.
func (s *Surface) SetOverlayAtlas([]byte, int, int) error { return ErrUnsupported }

// ScreenSize returns 0, 0: only macOS reports it yet.
func (s *Surface) ScreenSize() (w, h int) { return 0, 0 }

// Scale returns 1.
func (s *Surface) Scale() float64 { return 1 }
