//go:build windows

package windows

import (
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// A flyout (WindowOptions.Flyout) is a WS_POPUP window owned by its
// parent, which keeps it above, out of the taskbar (WS_EX_TOOLWINDOW), and
// transparent where its content is: without a redirection bitmap, the
// content shows through DirectComposition with its alpha. One that is not
// focusable is never activated (WS_EX_NOACTIVATE, MA_NOACTIVATE), and one
// that is closes when another window is activated. Owned windows do not
// follow their owner: the core places flyouts again as their parent moves.

const (
	wmMouseActivate = 0x0021
	maNoActivate    = 3
	// wmAppDismiss asks a flyout whether it closes, after the activation
	// that dismissed it.
	wmAppDismiss = wmApp + 6

	// flyoutShadowClass is the window class of flyouts with a shadow: the
	// drop shadow of menus and tooltips (CS_DROPSHADOW).
	flyoutShadowClass = "MyGoFlyoutShadow"
)

var procMonitorFromRect = user32.NewProc("MonitorFromRect")

// flyoutStyles returns the styles of a flyout's window.
func (w *window) flyoutStyles() (style, ex uint32) {
	style = wsPopup | wsClipChildren
	ex = wsExToolWindow
	if !w.opts.Flyout.Focusable {
		ex |= wsExNoActivate
	}
	if w.noRedirect {
		ex |= wsExNoRedirect
	}
	return style, ex
}

// PlaceFlyout places a flyout next to its anchor, in the parent's client
// area, as Flyout.Place resolves it in the work area of the monitor that
// has most of the anchor, in physical pixels at the parent's DPI.
func (w *window) PlaceFlyout(f platform.Flyout, size platform.Size) {
	p := w.parent
	if w.opts.Flyout == nil || w.closed || p == nil || p.closed {
		return
	}
	w.flyout, w.flyoutSize = f, size
	dpi := dpiOf(p.hwnd)
	origin := point{}
	procClientToScreen.Call(p.hwnd, uintptr(unsafe.Pointer(&origin)))
	px := f
	px.Gap = int(toPx(f.Gap, dpi))
	px.Anchor = platform.Rect{
		X:      int(origin.X + toPx(f.Anchor.X, dpi)),
		Y:      int(origin.Y + toPx(f.Anchor.Y, dpi)),
		Width:  int(toPx(f.Anchor.Width, dpi)),
		Height: int(toPx(f.Anchor.Height, dpi)),
	}
	a := rect{int32(px.Anchor.X), int32(px.Anchor.Y), int32(px.Anchor.X + max(px.Anchor.Width, 1)), int32(px.Anchor.Y + max(px.Anchor.Height, 1))}
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&a)), monitorDefaultToNearest)
	work := monitorWorkArea(mon)
	r, _, _ := px.Place(px.Anchor, platform.Size{Width: int(toPx(size.Width, dpi)), Height: int(toPx(size.Height, dpi))},
		platform.Rect{X: int(work.Left), Y: int(work.Top), Width: int(work.Right - work.Left), Height: int(work.Bottom - work.Top)})
	procSetWindowPos.Call(w.hwnd, 0, uintptr(r.X), uintptr(r.Y), uintptr(r.Width), uintptr(r.Height), swpNoZOrder|swpNoActivate)
}

// flyoutMessage handles the messages of a flyout's window that differ
// from other windows'.
func (w *window) flyoutMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmMouseActivate:
		if !w.flyout.Focusable {
			return maNoActivate, true
		}
	case wmActivate:
		if loword(wp) == waInactive && w.flyout.Focusable {
			// Unless the window activated is a flyout of its own.
			for x := w.b.windows[lp]; x != nil; x = x.parent {
				if x == w {
					return 0, false
				}
			}
			postMessage(w.hwnd, wmAppDismiss, 0, 0)
		}
	case wmAppDismiss:
		w.dismissFlyout()
		return 0, true
	case wmNCCalcSize:
		if wp != 0 {
			return 0, true // all of the window is client area
		}
	}
	return 0, false
}

// dismissFlyout closes a focusable flyout another window was activated
// over, as the user closing it.
func (w *window) dismissFlyout() {
	if !w.closed && w.IsVisible() && w.h.ShouldClose() {
		w.Close()
	}
}
