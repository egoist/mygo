//go:build windows

package mygo

// roundsContentCorners reports whether the window's content rounds its own
// corners: a transparent frameless window with a CornerRadius, where Windows
// keeps the system's square ones (Windows 10), as it rounds them itself
// (Windows 11, at 8 DIPs) and every maximized or full-screen window is square.
func (w *Window) roundsContentCorners() bool {
	return w.transparent && w.cornerRadius > 0 && w.native != nil && w.native.CornerRadius() == 0 &&
		!w.native.IsMaximized() && !w.native.IsFullScreen()
}
