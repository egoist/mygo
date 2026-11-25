//go:build !(darwin && !ios)

package gpusurface

// HDR10 reports false: only macOS presents HDR10 yet.
func (s *Surface) HDR10() bool { return false }

// SetHDR10 returns ErrUnsupported for on, and nil for off, which the
// surface already is: only macOS presents HDR10 yet.
func (s *Surface) SetHDR10(on bool) error {
	if on {
		return ErrUnsupported
	}
	return nil
}
