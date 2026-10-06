//go:build darwin

package darwin

import (
	"math"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

type surfaceTouch struct {
	id      uint64
	primary bool
}

func cocoaGesturePhase(ev id) platform.GesturePhase {
	p := send(ev, "phase")
	switch {
	case p&16 != 0:
		return platform.GestureCancel
	case p&8 != 0:
		return platform.GestureEnd
	case p&1 != 0:
		return platform.GestureBegin
	default:
		return platform.GestureUpdate
	}
}

func (s *surface) gesture(ev id, kind platform.GestureKind) {
	x, y := s.location(ev)
	e := platform.SurfaceEvent{Kind: platform.SurfaceGesture, Gesture: kind,
		Phase: cocoaGesturePhase(ev), X: x, Y: y, Scale: 1, Mods: eventMods(ev),
		Pointer: platform.PointerInfo{Device: platform.PointerTouchpad}}
	if kind == platform.GesturePinch {
		e.Scale = 1 + msgFloat(ev, sel("magnification"))
	}
	if kind == platform.GestureRotation {
		e.Rotation = -msgFloat(ev, sel("rotation")) * math.Pi / 180
	}
	s.send(e)
}

func (s *surface) touches(ev id, phase uint, kind platform.SurfaceEventKind) {
	if s.touchesByID == nil {
		s.touchesByID = make(map[id]surfaceTouch)
	}
	touches := send(send(ev, "touchesMatchingPhase:inView:", uintptr(phase), uintptr(s.view)), "allObjects")
	for i, n := 0, sendInt(touches, "count"); i < n; i++ {
		touch := send(touches, "objectAtIndex:", uintptr(i))
		identity := send(touch, "identity")
		c, exists := s.touchesByID[identity]
		if kind == platform.PointerDown {
			if len(s.touchesByID) == 0 {
				x, y := s.location(ev)
				s.touchAnchor = NSPoint{X: x, Y: y}
			}
			s.nextTouch++
			c = surfaceTouch{id: s.nextTouch, primary: len(s.touchesByID) == 0}
			s.touchesByID[identity] = c
		} else if !exists {
			continue
		}
		p := msgPoint(touch, sel("normalizedPosition"))
		s.send(platform.SurfaceEvent{Kind: kind, X: s.touchAnchor.X, Y: s.touchAnchor.Y,
			Pointer: platform.PointerInfo{ID: c.id, Device: platform.PointerTouchpad,
				Primary: c.primary, Contact: kind == platform.PointerDown || kind == platform.PointerMove,
				NormalizedX: float32(p.X), NormalizedY: float32(1 - p.Y)}, Mods: eventMods(ev)})
		if kind == platform.PointerUp || kind == platform.PointerCancel {
			delete(s.touchesByID, identity)
		}
	}
}

func surfacePointerMethods() []objc.MethodDef {
	touch := func(phase uint, kind platform.SurfaceEventKind) func(id, objc.SEL, id) {
		return func(self id, _ objc.SEL, ev id) {
			if s := theBackend.surfaceOf(self); s != nil {
				s.touches(ev, phase, kind)
			}
		}
	}
	gesture := func(kind platform.GestureKind) func(id, objc.SEL, id) {
		return func(self id, _ objc.SEL, ev id) {
			if s := theBackend.surfaceOf(self); s != nil {
				s.gesture(ev, kind)
			}
		}
	}
	return []objc.MethodDef{
		method("touchesBeganWithEvent:", touch(1, platform.PointerDown)),
		method("touchesMovedWithEvent:", touch(2, platform.PointerMove)),
		method("touchesEndedWithEvent:", touch(8, platform.PointerUp)),
		method("touchesCancelledWithEvent:", touch(16, platform.PointerCancel)),
		method("magnifyWithEvent:", gesture(platform.GesturePinch)),
		method("rotateWithEvent:", gesture(platform.GestureRotation)),
		// Tablet points embedded in mouse events use the ordinary path.
		// Standalone tablet points update the same pen identity.
		method("tabletPoint:", func(self id, _ objc.SEL, ev id) {
			if s := theBackend.surfaceOf(self); s != nil {
				s.mouse(platform.PointerMove, ev, 0)
			}
		}),
		method("tabletProximity:", func(self id, _ objc.SEL, ev id) {
			if s := theBackend.surfaceOf(self); s != nil {
				s.penEraser = sendInt(ev, "pointingDeviceType") == 3
				kind := platform.PointerEnter
				if !sendBool(ev, "isEnteringProximity") {
					kind = platform.PointerCancel
					delete(s.penContacts, uint64(send(ev, "deviceID"))|1<<63)
				}
				x, y := s.location(ev)
				s.send(platform.SurfaceEvent{Kind: kind, X: x, Y: y, Pointer: platform.PointerInfo{ID: uint64(send(ev, "deviceID")) | 1<<63, Device: platform.PointerPen, Primary: true, Eraser: s.penEraser}})
			}
		}),
	}
}
