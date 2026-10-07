//go:build linux && (amd64 || arm64)

package linux

import (
	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

var (
	gdkEventGetSourceDevice func(event ptr) ptr
	gdkDeviceGetSource      func(device ptr) int32
	gdkEventGetAxis         func(event ptr, axis int32, value *float64) bool
	gdkEventGetSequence     func(event ptr) ptr
	gdkEventGetCoords       func(event ptr, x, y *float64) bool
	gdkEventGetState        func(event ptr, state *uint32) bool
	gdkEventIsScrollStop    func(event ptr) bool
	cbSurfaceEvent          ptr
)

type touchSequence struct {
	id      uint64
	primary bool
}

func loadPointer() {
	bind(libGDK, &gdkEventIsScrollStop, "gdk_event_is_scroll_stop_event")
	mustBind(libGDK, &gdkEventGetSourceDevice, "gdk_event_get_source_device")
	mustBind(libGDK, &gdkDeviceGetSource, "gdk_device_get_source")
	mustBind(libGDK, &gdkEventGetAxis, "gdk_event_get_axis")
	mustBind(libGDK, &gdkEventGetSequence, "gdk_event_get_event_sequence")
	mustBind(libGDK, &gdkEventGetCoords, "gdk_event_get_coords")
	mustBind(libGDK, &gdkEventGetState, "gdk_event_get_state")
}

func eventSource(event ptr) int32 {
	if device := gdkEventGetSourceDevice(event); device != 0 {
		return gdkDeviceGetSource(device)
	}
	return 0
}

func eventButtons(event ptr) uint32 {
	var state uint32
	gdkEventGetState(event, &state)
	return state & (1<<8 | 1<<9 | 1<<10)
}

func gdkPointer(event ptr, kind platform.SurfaceEventKind) platform.PointerInfo {
	p := platform.PointerInfo{Primary: true}
	source := eventSource(event)
	if source == 1 || source == 2 {
		p.ID, p.Device, p.Eraser = uint64(gdkEventGetSourceDevice(event))|1<<63, platform.PointerPen, source == 2
		var pressure, x, y float64
		p.HasPressure = gdkEventGetAxis(event, 3, &pressure)
		p.Pressure = float32(pressure)
		xok, yok := gdkEventGetAxis(event, 4, &x), gdkEventGetAxis(event, 5, &y)
		p.HasTilt, p.TiltX, p.TiltY = xok && yok, float32(x*90), float32(y*90)
	}
	p.Contact = kind == platform.PointerDown || kind == platform.PointerMove && eventButtons(event) != 0
	return p
}

func (s *surface) pointerEvent(event ptr) bool {
	typ := field[int32](event, 0)
	if (typ == 3 || typ >= 4 && typ <= 7) && eventSource(event) == 5 {
		// Consuming raw touch sequences disables GDK's promoted mouse
		// path: a touch must not also click/drag as a second contact.
		return true
	}
	if typ == 35 { // GDK_GRAB_BROKEN
		// A keyboard grab breaking does not cancel pointer input.
		if field[int32](event, 20) == 0 {
			s.send(platform.SurfaceEvent{Kind: platform.PointerCaptureLost})
			if s.penPointer.ID != 0 {
				s.send(platform.SurfaceEvent{Kind: platform.PointerCancel, Pointer: s.penPointer, X: s.penPosition[0], Y: s.penPosition[1]})
			}
			for _, c := range s.touchSequences {
				s.send(platform.SurfaceEvent{Kind: platform.PointerCancel, Pointer: platform.PointerInfo{ID: c.id, Device: platform.PointerTouch}})
			}
			clear(s.touchSequences)
		}
		return false
	}
	if typ == 10 { // GDK_ENTER_NOTIFY
		var x, y float64
		gdkEventGetCoords(event, &x, &y)
		s.send(platform.SurfaceEvent{Kind: platform.PointerEnter, Pointer: gdkPointer(event, platform.PointerEnter), X: x, Y: y})
		return false
	}
	if typ == 20 || typ == 21 {
		p := gdkPointer(event, platform.PointerEnter)
		if p.Device == platform.PointerPen {
			kind := platform.PointerEnter
			if typ == 21 {
				kind = platform.PointerCancel
			}
			s.send(platform.SurfaceEvent{Kind: kind, Pointer: p, X: s.penPosition[0], Y: s.penPosition[1]})
		}
		return false
	}
	if typ >= 37 && typ <= 40 {
		if s.touchSequences == nil {
			s.touchSequences = make(map[ptr]touchSequence)
		}
		sequence := gdkEventGetSequence(event)
		c, exists := s.touchSequences[sequence]
		kind := platform.PointerMove
		switch typ {
		case 37:
			s.nextTouch++
			c = touchSequence{id: s.nextTouch, primary: len(s.touchSequences) == 0}
			s.touchSequences[sequence] = c
			kind = platform.PointerDown
			if !gtkWidgetHasFocus(s.area) {
				gtkWidgetGrabFocus(s.area)
			}
		case 39:
			kind = platform.PointerUp
		case 40:
			kind = platform.PointerCancel
		}
		if typ != 37 && !exists {
			return true
		}
		var x, y float64
		var state uint32
		gdkEventGetCoords(event, &x, &y)
		gdkEventGetState(event, &state)
		s.send(platform.SurfaceEvent{Kind: kind, X: x, Y: y, Mods: gdkMods(state), Pointer: platform.PointerInfo{ID: c.id, Device: platform.PointerTouch, Primary: c.primary, Contact: typ == 37 || typ == 38}})
		if typ == 39 || typ == 40 {
			delete(s.touchSequences, sequence)
		}
		return true
	}
	if typ == 41 || typ == 42 { // GDK_TOUCHPAD_SWIPE/PINCH, GTK 3.18+
		e := platform.SurfaceEvent{Kind: platform.SurfaceGesture, Gesture: platform.GesturePan,
			Pointer: platform.PointerInfo{Device: platform.PointerTouchpad}, Contacts: int(field[int8](event, 18)),
			X: field[float64](event, 24), Y: field[float64](event, 32), DX: field[float64](event, 40), DY: field[float64](event, 48), Scale: 1}
		switch field[int8](event, 17) {
		case 0:
			e.Phase = platform.GestureBegin
			s.pinchScale = 1
		case 1:
			e.Phase = platform.GestureUpdate
		case 2:
			e.Phase = platform.GestureEnd
		case 3:
			e.Phase = platform.GestureCancel
		}
		stateOffset := uintptr(72)
		if typ == 42 {
			e.Gesture |= platform.GesturePinch | platform.GestureRotation
			e.Rotation = field[float64](event, 56)
			total := field[float64](event, 64)
			if total > 0 && s.pinchScale > 0 {
				e.Scale = total / s.pinchScale
			}
			s.pinchScale = total
			stateOffset = 88
		}
		e.Mods = gdkMods(field[uint32](event, stateOffset))
		s.send(e)
		return true
	}
	return false
}

func initPointerCallback() {
	cbSurfaceEvent = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := theBackend.surfaceOf(data); s != nil {
			return s.pointerEvent(event)
		}
		return false
	})
}
