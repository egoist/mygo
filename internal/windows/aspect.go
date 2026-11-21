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
	dpi := dpiOf(w.hwnd)
	frameW := (wr.Right - wr.Left) - (cr.Right - cr.Left)
	frameH := (wr.Bottom - wr.Top) - (cr.Bottom - cr.Top)
	// MinimumSize and MaximumSize are window sizes (WM_GETMINMAXINFO
	// tracks them with the frame in), and the ratio's limits are content
	// sizes: take the frame out, so a drag stops with the ratio holding
	// at the size the app set. 0 is no limit.
	lim := func(v int, frame int32) int32 {
		if v == 0 {
			return 0
		}
		return max(toPx(v, dpi)-frame, 1)
	}
	aspectRect(r, edge, w.aspect, frameW, frameH,
		lim(w.minW, frameW), lim(w.minH, frameH), lim(w.maxW, frameW), lim(w.maxH, frameH))
}

// aspectRect adjusts r, the window rectangle the user is dragging by the
// edge or corner edge, so its content keeps ratio, within minW, minH, maxW
// and maxH (0 for no limit). frameW and frameH are the room the frame
// takes around the content. The side or corner being dragged stays under
// the pointer; a side drag keeps the top or left edge put and grows the
// window from there, as Electron and Chromium do on Windows, and a corner
// drag keeps the opposite corner put. A corner's proposal keeps the ratio
// by growing the dimension that lags behind it, never shrinking either
// below the proposal. The limits are applied inside the ratio's math, so a
// drag that reaches one stops there with the ratio still holding.
func aspectRect(r *rect, edge uint32, ratio float64, frameW, frameH, minW, minH, maxW, maxH int32) {
	pw := r.Right - r.Left - frameW
	ph := r.Bottom - r.Top - frameH
	if pw <= 0 || ph <= 0 || ratio <= 0 {
		return
	}
	// A side drag drives its own axis; a corner grows the dimension that
	// lags behind the ratio.
	widthDriven := edge == wmszLeft || edge == wmszRight
	if !widthDriven && edge != wmszTop && edge != wmszBottom {
		widthDriven = float64(ph)*ratio <= float64(pw)
	}
	cw, ch := pw, int32(math.Round(float64(pw)/ratio))
	if !widthDriven {
		cw, ch = int32(math.Round(float64(ph)*ratio)), ph
	}
	// Clamp the driver at a limit, then derive the other dimension from it
	// again, so the ratio holds where the drag stops.
	if widthDriven {
		cw = clampDim(cw, minW, maxW)
		ch = int32(math.Round(float64(cw) / ratio))
		if dh := clampDim(ch, minH, maxH); dh != ch {
			ch = dh
			cw = clampDim(int32(math.Round(float64(ch)*ratio)), minW, maxW)
		}
	} else {
		ch = clampDim(ch, minH, maxH)
		cw = int32(math.Round(float64(ch) * ratio))
		if dw := clampDim(cw, minW, maxW); dw != cw {
			cw = dw
			ch = clampDim(int32(math.Round(float64(cw)/ratio)), minH, maxH)
		}
	}
	totalW, totalH := cw+frameW, ch+frameH
	switch edge {
	case wmszLeft:
		r.Left = r.Right - totalW
		r.Bottom = r.Top + totalH
	case wmszRight:
		r.Right = r.Left + totalW
		r.Bottom = r.Top + totalH
	case wmszTop:
		r.Top = r.Bottom - totalH
		r.Right = r.Left + totalW
	case wmszBottom:
		r.Bottom = r.Top + totalH
		r.Right = r.Left + totalW
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

// clampDim limits v to a minimum and a maximum, where 0 means no limit.
func clampDim(v, min, max int32) int32 {
	if min > 0 && v < min {
		v = min
	}
	if max > 0 && v > max {
		v = max
	}
	return v
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
