//go:build windows && (amd64 || arm64)

package windows

import "unsafe"

// A window without a caption, frameless or with a hidden title bar, has no
// border at the top: frameCalcSize gives the top to the client area, where
// the webview's or the surface's window answers HTCLIENT. On Windows 10 the
// client area covers the whole window, so its left, right and bottom borders
// are gone too. The edges are invisible windows (WS_EX_NOREDIRECTIONBITMAP)
// above the webview or the surface, as thick as the resize border, along the
// sides: across the whole width of a frameless window, and up to the window
// controls of a hidden title bar. They answer HTLEFT, HTRIGHT or HTBOTTOM,
// and the corners HTTOPLEFT and so on, and hand presses to the window, which
// then resizes from its edges as one with a caption does. Below them, a drag
// region of the page or of native UI still moves the window.
//
// The webview's window covers the whole client area, so the window itself
// never gets WM_NCHITTEST at its edges: without these windows, a captionless
// webview window could not be resized from its left, right or bottom.
// Windows 11 keeps its native left, right and bottom borders outside the
// client area, and only needs the top edge.

// edgeKind numbers the edges of a window, in the order they are laid out.
type edgeKind uint8

const (
	edgeTop edgeKind = iota
	edgeBottom
	edgeLeft
	edgeRight
	edgeCount
)

// resizeEdge is the window of one edge, and the window it resizes.
type resizeEdge struct {
	w    *window
	kind edgeKind
}

// resizeCornerSize is how far from a corner an edge resizes the corner, as
// Chromium's frames do.
const resizeCornerSize = 16

// newResizeEdges creates the edges of a window without a caption. Flyouts,
// which never resize, have none. Windows 11 needs the top edge only: its
// native left, right and bottom borders remain.
func (w *window) newResizeEdges() {
	if !w.captionless() || w.opts.Flyout != nil {
		return
	}
	kinds := []edgeKind{edgeTop}
	if !windows11() {
		kinds = append(kinds, edgeBottom, edgeLeft, edgeRight)
	}
	for _, kind := range kinds {
		e := createWindow(wsExLayered|wsExNoRedirect, captionClass, "", wsChild|wsClipSiblings, 0, 0, 0, 0, w.hwnd)
		if e == 0 {
			continue
		}
		// A layered window shows, and takes the mouse, once it has attributes;
		// these have no pixels to show.
		procSetLayeredWindowAttributes.Call(e, 0, 255, lwaAlpha)
		w.edges[kind] = e
		w.b.resizeEdges[e] = resizeEdge{w, kind}
	}
}

// layoutResizeEdges puts the edges along the window, above the webview or the
// surface, and hides them while nothing resizes the window. It runs when the
// window changes size, state or DPI, when the webview appears, and when the
// window controls change.
func (w *window) layoutResizeEdges() {
	if w.IsMinimized() {
		return
	}
	dpi := dpiOf(w.hwnd)
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	band := resizeBorder(dpi)
	show := uintptr(swpShowWindow)
	if w.fullScreen || w.IsMaximized() || !w.hasStyle(wsThickFrame) {
		show = swpHideWindow
	}
	// A hidden title bar's controls fill the top-right corner: the right edge
	// starts below them, or it would resize the window instead of pressing
	// the close button.
	top := band
	if w.caption != nil {
		top = max(top, toPx(w.titleBarHeight(), dpi))
	}
	width := r.Right
	if w.caption != nil {
		width -= toPx(captionButtonWidth, dpi) * int32(len(w.shownButtons()))
	}
	for kind, e := range w.edges {
		if e == 0 {
			continue
		}
		var x, y, edgeWidth, edgeHeight int32
		switch edgeKind(kind) {
		case edgeTop:
			x, y, edgeWidth, edgeHeight = 0, 0, width, band
		case edgeBottom:
			x, y, edgeWidth, edgeHeight = 0, r.Bottom-band, r.Right, band
		case edgeLeft:
			x, y, edgeWidth, edgeHeight = 0, band, band, r.Bottom-band
		case edgeRight:
			x, y, edgeWidth, edgeHeight = r.Right-band, top, band, r.Bottom-top
		}
		// HWND_TOP (0): above the webview's window and the surface's.
		procSetWindowPos.Call(e, 0, uintptr(x), uintptr(y), uintptr(max(edgeWidth, 0)), uintptr(max(edgeHeight, 0)), swpNoActivate|show)
	}
}

// edgeMessage handles a message of an edge's window; ok false lets
// DefWindowProc run.
func (w *window) edgeMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	kind, ok := w.edgeKind(hwnd)
	if !ok {
		return 0, false
	}
	switch m {
	case wmNCHitTest:
		pt := point{getXParam(lp), getYParam(lp)}
		procScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
		var client rect
		procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&client)))
		corner := toPx(resizeCornerSize, dpiOf(w.hwnd))
		switch kind {
		case edgeTop:
			var edge rect
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&edge)))
			return topEdgeHit(pt.X, edge.Right, client.Right, corner), true
		case edgeBottom:
			return bottomEdgeHit(pt.X, client.Right, corner), true
		case edgeLeft:
			return leftEdgeHit(pt.Y, client.Bottom, corner), true
		case edgeRight:
			return rightEdgeHit(pt.Y, client.Bottom, corner), true
		}
	case wmNCLButtonDown, wmNCLButtonDblClk:
		// The window resizes from its edge, or a double click stretches it to
		// the height of the screen, as from a caption.
		r, _, _ := procSendMessageW.Call(w.hwnd, uintptr(m), wp, lp)
		return r, true
	}
	return 0, false
}

// edgeKind returns which edge hwnd is, if it is one of w's.
func (w *window) edgeKind(hwnd uintptr) (edgeKind, bool) {
	for kind, e := range w.edges {
		if e != 0 && e == hwnd {
			return edgeKind(kind), true
		}
	}
	return 0, false
}

// topEdgeHit returns the hit-test code at x, in pixels from the left of the
// window's client area, on a top edge edgeWidth pixels wide in a client area
// clientWidth wide: the corner within corner pixels of the left, or of the
// right where the edge reaches it (no window controls there), and the top
// elsewhere.
func topEdgeHit(x, edgeWidth, clientWidth, corner int32) uintptr {
	switch {
	case x < corner:
		return htTopLeft
	case edgeWidth >= clientWidth && x >= clientWidth-corner:
		return htTopRight
	}
	return htTop
}

// bottomEdgeHit returns the hit-test code at x for a bottom edge across the
// client area clientWidth wide.
func bottomEdgeHit(x, clientWidth, corner int32) uintptr {
	switch {
	case x < corner:
		return htBottomLeft
	case x >= clientWidth-corner:
		return htBottomRight
	}
	return htBottom
}

// leftEdgeHit returns the hit-test code at y for a left edge along the client
// area clientHeight tall.
func leftEdgeHit(y, clientHeight, corner int32) uintptr {
	switch {
	case y < corner:
		return htTopLeft
	case y >= clientHeight-corner:
		return htBottomLeft
	}
	return htLeft
}

// rightEdgeHit returns the hit-test code at y for a right edge along the
// client area clientHeight tall.
func rightEdgeHit(y, clientHeight, corner int32) uintptr {
	switch {
	case y < corner:
		return htTopRight
	case y >= clientHeight-corner:
		return htBottomRight
	}
	return htRight
}

// resizeBorder is the thickness of the band along an edge that resizes the
// window: the thickness of its sizing frame, as above a caption.
func resizeBorder(dpi int) int32 {
	if has(procGetSystemMetricsForDpi) {
		frame, _, _ := procGetSystemMetricsForDpi.Call(smCyFrame, uintptr(dpi))
		padding, _, _ := procGetSystemMetricsForDpi.Call(smCxPaddedBorder, uintptr(dpi))
		if frame+padding > 0 {
			return int32(frame + padding)
		}
	}
	return toPx(8, dpi)
}
