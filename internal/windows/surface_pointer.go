//go:build windows && (amd64 || arm64)

package windows

import (
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

const (
	wmPointerUpdate         = 0x0245
	wmPointerDown           = 0x0246
	wmPointerUp             = 0x0247
	wmPointerEnter          = 0x0249
	wmPointerLeave          = 0x024A
	wmPointerCaptureChanged = 0x024C
	wmCancelMode            = 0x001F
	pointerInContact        = 0x0004
	pointerPrimary          = 0x2000
	pointerCanceled         = 0x8000
)

var (
	procGetPointerInfo      = user32.NewProc("GetPointerInfo")
	procGetPointerPenInfo   = user32.NewProc("GetPointerPenInfo")
	procGetPointerTouchInfo = user32.NewProc("GetPointerTouchInfo")
	procGetMessageExtraInfo = user32.NewProc("GetMessageExtraInfo")
)

// POINTER_INFO is 96 bytes on both 64-bit Windows targets. Using the full
// structure avoids reading the packed signed 16-bit message coordinates,
// which truncate locations on large desktops and at high DPI.
type pointerInfo struct {
	Type, ID, Frame, Flags                 uint32
	Device, Target                         uintptr
	Pixel, Himetric, RawPixel, RawHimetric point
	Time, History                          uint32
	Input                                  int32
	Keys                                   uint32
	Performance                            uint64
	ButtonChange                           uint32
}

type pointerPenInfo struct {
	Info                            pointerInfo
	Flags, Mask, Pressure, Rotation uint32
	TiltX, TiltY                    int32
}

type pointerTouchInfo struct {
	Info                  pointerInfo
	Flags, Mask           uint32
	Contact, RawContact   rect
	Orientation, Pressure uint32
}

func promotedPointerMouse() bool {
	if procGetPointerInfo.Find() != nil {
		return false
	}
	extra, _, _ := procGetMessageExtraInfo.Call()
	return extra&0xFFFFFF00 == 0xFF515700
}

func (s *surface) pointerMessage(m uint32, wp uintptr) bool {
	id := uint64(loword(wp))
	if m == wmPointerCaptureChanged {
		if ev, ok := s.pointerContacts[id]; ok {
			ev.Kind, ev.Pointer.Contact = platform.PointerCaptureLost, false
			delete(s.pointerContacts, id)
			s.send(ev)
		}
		return true
	}
	var pi pointerInfo
	if r, _, _ := procGetPointerInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&pi))); r == 0 {
		if ev, ok := s.pointerContacts[id]; ok {
			ev.Kind, ev.Pointer.Contact = platform.PointerCancel, false
			delete(s.pointerContacts, id)
			s.send(ev)
		}
		return true
	}
	if pi.Type != 2 && pi.Type != 3 {
		return false
	} // touch/pen; keep ordinary mouse messages
	p := platform.PointerInfo{ID: id, Device: platform.PointerTouch, Primary: pi.Flags&pointerPrimary != 0, Contact: pi.Flags&pointerInContact != 0}
	if pi.Type == 3 {
		p.Device = platform.PointerPen
		var pen pointerPenInfo
		if r, _, _ := procGetPointerPenInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&pen))); r != 0 {
			p.HasPressure, p.Pressure = pen.Mask&1 != 0, float32(pen.Pressure)/1024
			p.HasTilt = pen.Mask&12 == 12
			p.TiltX, p.TiltY = float32(pen.TiltX), float32(pen.TiltY)
			p.Eraser = pen.Flags&6 != 0 // inverted or eraser
		}
	} else {
		var touch pointerTouchInfo
		if r, _, _ := procGetPointerTouchInfo.Call(uintptr(id), uintptr(unsafe.Pointer(&touch))); r != 0 {
			p.HasPressure, p.Pressure = touch.Mask&4 != 0, float32(touch.Pressure)/1024
		}
	}
	pt := pi.Pixel
	procScreenToClient.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
	ev := platform.SurfaceEvent{Pointer: p, X: s.toDIP(pt.X), Y: s.toDIP(pt.Y), Mods: mods()}
	if pi.Flags&0x20 != 0 || pi.ButtonChange == 3 || pi.ButtonChange == 4 {
		ev.Button = 1
	}
	switch m {
	case wmPointerDown:
		ev.Kind = platform.PointerDown
		procSetFocus.Call(s.hwnd)
	case wmPointerUpdate:
		ev.Kind = platform.PointerMove
	case wmPointerUp:
		ev.Kind = platform.PointerUp
	case wmPointerEnter:
		ev.Kind = platform.PointerEnter
	case wmPointerLeave:
		ev.Kind = platform.PointerLeave
	}
	if pi.Flags&pointerCanceled != 0 {
		ev.Kind = platform.PointerCancel
	}
	if s.pointerContacts == nil {
		s.pointerContacts = make(map[uint64]platform.SurfaceEvent)
	}
	if ev.Kind == platform.PointerDown || ev.Kind == platform.PointerMove && p.Contact {
		s.pointerContacts[id] = ev
	}
	if ev.Kind == platform.PointerCancel || ev.Kind == platform.PointerUp {
		delete(s.pointerContacts, id)
	}
	s.send(ev)
	return true
}

func (s *surface) cancelNativePointers() {
	for id, ev := range s.pointerContacts {
		delete(s.pointerContacts, id)
		ev.Kind, ev.Pointer.Contact = platform.PointerCancel, false
		s.send(ev)
	}
	if s.buttons != 0 {
		s.buttons = 0
		ev := s.lastMouse
		ev.Kind, ev.Pointer.Contact = platform.PointerCaptureLost, false
		s.send(ev)
		procReleaseCapture.Call()
	}
}
