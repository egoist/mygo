//go:build windows && (amd64 || arm64)

package windows

import (
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Pens and fingers come as WM_POINTER messages. The surface leaves a
// pen's to DefWindowProc to make mouse messages of, as it does for windows
// that handle none: the surface reads its pressure from them, and the
// mouse messages that follow tell what device they come of. A finger's it
// takes (touchMessage), so that DefWindowProc makes no mouse of the first
// finger, and gives them to a platform.TouchGesture: a finger dragging
// scrolls, two pinch, a tap clicks and press and hold right-clicks, as on
// Linux. A finger landing on a web view embedded in the content is left
// to DefWindowProc, whose mouse messages the web view gets
// (forwardMouse).
var (
	procGetMessageExtraInfo = user32.NewProc("GetMessageExtraInfo")
	procGetPointerType      = user32.NewProc("GetPointerType")
	procGetPointerPenInfo   = user32.NewProc("GetPointerPenInfo")
	procGetPointerTouchInfo = user32.NewProc("GetPointerTouchInfo")
)

const (
	wmPointerUpdate         = 0x0245
	wmPointerDown           = 0x0246
	wmPointerUp             = 0x0247
	wmPointerCaptureChanged = 0x024C

	// pointerMessageFlagPrimary and pointerMessageFlagCanceled are
	// POINTER_MESSAGE_FLAG_PRIMARY and _CANCELED, in the high word of a
	// WM_POINTER message's wParam.
	pointerMessageFlagPrimary  = 0x2000
	pointerMessageFlagCanceled = 0x8000

	ptTouch = 2
	ptPen   = 3

	penMaskPressure   = 0x1
	touchMaskPressure = 0x4

	// miWpSignature marks the extra information of mouse messages that
	// Windows makes of a pen's or a finger's (MI_WP_SIGNATURE), with
	// 0x80 set for a finger's.
	miWpSignature = 0xFF515700
	signatureMask = 0xFFFFFF00
)

// pointerInfo is POINTER_INFO.
type pointerInfo struct {
	Type, ID, FrameID, Flags uint32
	SourceDevice, Target     uintptr
	Pixel, Himetric          point
	PixelRaw, HimetricRaw    point
	Time, HistoryCount       uint32
	InputData                int32
	KeyStates                uint32
	PerformanceCount         uint64
	ButtonChangeType         uint32
}

// pointerPenInfo is POINTER_PEN_INFO.
type pointerPenInfo struct {
	Info                  pointerInfo
	Flags, Mask, Pressure uint32
	Rotation              uint32
	TiltX, TiltY          int32
}

// pointerTouchInfo is POINTER_TOUCH_INFO.
type pointerTouchInfo struct {
	Info                  pointerInfo
	Flags, Mask           uint32
	Contact, ContactRaw   rect
	Orientation, Pressure uint32
}

// The pointer messages' information, read on the main thread into these
// rather than into values of each message, which would go to the heap.
var (
	pointerType uint32
	penInfo     pointerPenInfo
	touchInfo   pointerTouchInfo
)

// pointerPressure returns how hard the pen or the finger of WM_POINTER's
// pointer id presses, from 0 to 1, or 0 where it does not tell, as a
// mouse's, with its device and where it is on the screen, in pixels. ok is
// false where Windows tells none of it.
func pointerPressure(id uint32) (pressure float64, kind platform.PointerKind, at point, ok bool) {
	if !has(procGetPointerType) {
		return 0, 0, point{}, false
	}
	if r, _, _ := procGetPointerType.Call(uintptr(id), uintptr(unsafe.Pointer(&pointerType))); r == 0 {
		return 0, 0, point{}, false
	}
	switch pointerType {
	case ptPen:
		if r, _, _ := procGetPointerPenInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&penInfo))); r != 0 {
			if penInfo.Mask&penMaskPressure != 0 {
				pressure = float64(penInfo.Pressure) / 1024
			}
			return pressure, platform.PointerPen, penInfo.Info.Pixel, true
		}
	case ptTouch:
		if r, _, _ := procGetPointerTouchInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&touchInfo))); r != 0 {
			if touchInfo.Mask&touchMaskPressure != 0 {
				pressure = float64(touchInfo.Pressure) / 1024
			}
			return pressure, platform.PointerTouch, touchInfo.Info.Pixel, true
		}
	}
	return 0, 0, point{}, false
}

// pointerMessage notes the pressure of a WM_POINTER message, which the
// mouse messages Windows makes of it carry (mouseDevice). A pen pressing
// harder or softer where it stands makes no mouse message: the surface
// moves the pointer where it is, with the new pressure, as Pointer Events
// fire pointermove.
func (s *surface) pointerMessage(m uint32, wp uintptr) {
	pressure, kind, at, ok := pointerPressure(uint32(loword(wp)))
	if !ok {
		s.pressure = 0
		return
	}
	if m == wmPointerUpdate && s.buttons != 0 && at == s.pointerAt && pressure != s.pressure {
		pt := at
		procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
		s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: s.toDIP(pt.X), Y: s.toDIP(pt.Y), Mods: mods(), Pointer: kind, Pressure: pressure})
	}
	s.pressure, s.pointerAt = pressure, at
}

// touchMessage gives a finger's WM_POINTERDOWN, UPDATE or UP to the
// surface's gesture and sends what it does, and reports whether it took
// the message: a pen's, and a finger's landing on an embedded web view or
// that one did, it leaves to DefWindowProc.
func (s *surface) touchMessage(m uint32, wp uintptr) bool {
	id := uintptr(loword(wp))
	if m != wmPointerDown && !s.touch.Holds(id) {
		return false
	}
	pressure, kind, at, ok := pointerPressure(uint32(id))
	if !ok || kind != platform.PointerTouch {
		return false
	}
	procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&at)))
	x, y := s.toDIP(at.X), s.toDIP(at.Y)
	t, _, _ := procGetMessageTime.Call()
	var evs []platform.SurfaceEvent
	switch m {
	case wmPointerDown:
		if hiword(wp)&pointerMessageFlagPrimary != 0 {
			// The first finger of new touches: no other is down,
			// whatever ended unseen.
			s.touch.Reset()
		}
		if s.layer != nil && !s.touch.Holds(id) && s.webViewAt(x, y) != nil {
			return false
		}
		evs = s.touch.Begin(id, x, y, uint32(t))
	case wmPointerUpdate:
		evs = s.touch.Move(id, x, y)
	default:
		evs = s.touch.End(id, x, y, uint32(t), pointerCanceled(wp))
	}
	s.sendTouch(evs, pressure, uint32(t))
	return true
}

// touchCaptureLost ends with no tap the finger of a
// WM_POINTERCAPTURECHANGED, whose window took it, and reports whether it
// was the gesture's.
func (s *surface) touchCaptureLost(wp uintptr) bool {
	id := uintptr(loword(wp))
	if !s.touch.Holds(id) {
		return false
	}
	s.sendTouch(s.touch.End(id, 0, 0, 0, true), 0, 0)
	return true
}

// sendTouch sends the events of the gesture, by a finger pressing with
// pressure at time t: a tap focuses the surface, as a click does, and
// counts its presses as a mouse's, though taps land further apart.
func (s *surface) sendTouch(evs []platform.SurfaceEvent, pressure float64, t uint32) {
	for _, ev := range evs {
		ev.Mods = mods()
		ev.Pointer, ev.Pressure = platform.PointerTouch, pressure
		if ev.Kind == platform.PointerDown {
			procSetFocus.Call(s.hwnd)
			// The count is in the window's pixels, as the mouse's.
			scale := float64(s.dpi()) / 96
			ms, _, _ := procGetDoubleClickTime.Call()
			w, h := systemMetric(smCxDoubleClk, s.hwnd), systemMetric(smCyDoubleClk, s.hwnd)
			dc := platform.DoubleClick{Time: time.Duration(ms) * time.Millisecond, DX: max(float64(w)/2, platform.TouchSlop*scale), DY: max(float64(h)/2, platform.TouchSlop*scale)}
			ev.Clicks = s.clicks.Press(ev.Button, ev.X*scale, ev.Y*scale, t, dc)
		}
		s.send(ev)
	}
}

// pointerCanceled reports whether a WM_POINTER message's pointer was
// canceled (IS_POINTER_CANCELED_WPARAM): the system took it, and it ends
// with no release.
func pointerCanceled(wp uintptr) bool { return hiword(wp)&pointerMessageFlagCanceled != 0 }

// cancelPress ends the press of the buttons held with no release
// (platform.PointerCancel).
func (s *surface) cancelPress() {
	s.buttons = 0
	s.send(platform.SurfaceEvent{Kind: platform.PointerCancel, Mods: mods()})
}

// mouseDevice returns the device of the mouse message being handled, as
// Windows marks those it makes of a pen's or a finger's, with the pressure
// that the pointer message before it told.
func (s *surface) mouseDevice() (platform.PointerKind, float64) {
	extra, _, _ := procGetMessageExtraInfo.Call()
	switch {
	case uint32(extra)&signatureMask != miWpSignature:
		return platform.PointerMouse, 0
	case extra&0x80 != 0:
		return platform.PointerTouch, s.pressure
	}
	return platform.PointerPen, s.pressure
}
