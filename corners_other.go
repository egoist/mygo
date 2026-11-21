//go:build !windows

package mygo

// roundsContentCorners reports whether the window's content rounds its own
// corners: the system does on every platform but Windows 10 (corners_windows.go).
func (w *Window) roundsContentCorners() bool { return false }
