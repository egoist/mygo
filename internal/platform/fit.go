package platform

// FitWindow returns a restored window's rectangle r fitted to the work
// area of its display, as AppKit constrains a window's frame to its
// screen. Its top edge is inside the work area, so that the title bar is
// not under the menu bar or above the screen, and at least minW × minH of
// it shows there; a window left partly off a side of the display stays
// where it was. One wider or taller than the work area takes all of it
// that way, so that none of its edges is out of reach.
func FitWindow(r, work Rect, minW, minH int) Rect {
	if r.Width > work.Width {
		r.X, r.Width = work.X, work.Width
	}
	if r.Height > work.Height {
		r.Y, r.Height = work.Y, work.Height
	}
	minW, minH = min(minW, r.Width), min(minH, r.Height)
	r.X = max(work.X+minW-r.Width, min(r.X, work.X+work.Width-minW))
	r.Y = max(work.Y, min(r.Y, work.Y+work.Height-minH))
	return r
}
