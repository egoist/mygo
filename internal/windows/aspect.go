//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"unsafe"
)

// The edges and corners of WM_SIZING's wParam (WinUser.h).
const (
	wmszLeft        = 1
	wmszRight       = 2
	wmszTop         = 3
	wmszTopLeft     = 4
	wmszTopRight    = 5
	wmszBottom      = 6
	wmszBottomLeft  = 7
	wmszBottomRight = 8
)

// sizingAspect constrains the drag rectangle of WM_SIZING to the window's
// aspect ratio, which applies to the content: the frame around it keeps
// its size.
func (w *window) sizingAspect(r *rect, edge uint32) {
	var wr, cr rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&wr)))
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	aspectRect(r, edge, w.aspect,
		(wr.Right-wr.Left)-(cr.Right-cr.Left),
		(wr.Bottom-wr.Top)-(cr.Bottom-cr.Top))
}

// aspectRect adjusts r, the window rectangle the user is dragging by the
// edge or corner edge, so its content keeps ratio. frameW and frameH are
// the room the frame takes around the content. The side or corner being
// dragged stays under the pointer; a side drag grows the other axis around
// its center, as AppKit's content aspect ratio does, and a corner drag
// keeps the opposite corner put. A corner's proposal keeps the ratio by
// growing the dimension that lags behind it, never shrinking either below
// the proposal.
func aspectRect(r *rect, edge uint32, ratio float64, frameW, frameH int32) {
	pw := r.Right - r.Left - frameW
	ph := r.Bottom - r.Top - frameH
	if pw <= 0 || ph <= 0 || ratio <= 0 {
		return
	}
	cw, ch := pw, int32(math.Round(float64(pw)/ratio))
	// A side drag drives its own axis; a corner grows the dimension that
	// lags behind the ratio.
	growWidth := edge == wmszTop || edge == wmszBottom
	if edge != wmszLeft && edge != wmszRight && !growWidth {
		growWidth = float64(ph)*ratio > float64(pw)
	}
	if growWidth {
		cw, ch = int32(math.Round(float64(ph)*ratio)), ph
	}
	totalW, totalH := cw+frameW, ch+frameH
	switch edge {
	case wmszLeft:
		mid := (r.Top + r.Bottom) / 2
		r.Left = r.Right - totalW
		r.Top, r.Bottom = mid-totalH/2, mid-totalH/2+totalH
	case wmszRight:
		mid := (r.Top + r.Bottom) / 2
		r.Right = r.Left + totalW
		r.Top, r.Bottom = mid-totalH/2, mid-totalH/2+totalH
	case wmszTop:
		mid := (r.Left + r.Right) / 2
		r.Top = r.Bottom - totalH
		r.Left, r.Right = mid-totalW/2, mid-totalW/2+totalW
	case wmszBottom:
		mid := (r.Left + r.Right) / 2
		r.Bottom = r.Top + totalH
		r.Left, r.Right = mid-totalW/2, mid-totalW/2+totalW
	case wmszTopLeft:
		r.Left, r.Top = r.Right-totalW, r.Bottom-totalH
	case wmszTopRight:
		r.Right, r.Top = r.Left+totalW, r.Bottom-totalH
	case wmszBottomLeft:
		r.Left, r.Bottom = r.Right-totalW, r.Top+totalH
	default: // wmszBottomRight
		r.Right, r.Bottom = r.Left+totalW, r.Top+totalH
	}
}

// TestSizingAspect grows the window's rectangle by dw, dh pixels, sends
// WM_SIZING as the modal resize loop does for the edge being dragged, and
// returns the content size the window's aspect ratio leaves.
func TestSizingAspect(hwnd uintptr, edge uint32, dw, dh int32) (int32, int32) {
	var wr, cr rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	frameW := (wr.Right - wr.Left) - (cr.Right - cr.Left)
	frameH := (wr.Bottom - wr.Top) - (cr.Bottom - cr.Top)
	r := rect{wr.Left, wr.Top, wr.Right + dw, wr.Bottom + dh}
	procSendMessageW.Call(hwnd, wmSizing, uintptr(edge), uintptr(unsafe.Pointer(&r)))
	return (r.Right - r.Left) - frameW, (r.Bottom - r.Top) - frameH
}