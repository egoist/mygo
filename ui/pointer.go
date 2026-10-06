package ui

import (
	"slices"

	"github.com/egoist/mygo/internal/platform"
)

// PointerDevice identifies the device producing pointer input.
type PointerDevice = platform.PointerDevice

const (
	PointerMouse    = platform.PointerMouse
	PointerTouch    = platform.PointerTouch
	PointerPen      = platform.PointerPen
	PointerTouchpad = platform.PointerTouchpad
)

// PointerInfo describes a contact or hovering pointer. ID is stable for
// its lifetime within a window (zero for the mouse). Contact distinguishes
// a press from hover. Pressure is 0..1, TiltX/Y are -90..90 degrees, and
// HasPressure/HasTilt report availability. Touchpad contacts are indirect:
// NormalizedX/Y are top-left coordinates on the device, while InputEvent's
// X/Y are the cursor anchor, not the finger's position on the screen.
type PointerInfo = platform.PointerInfo

// TrackContacts extends HandleInput to every contact, including additional
// fingers and indirect touchpad contacts. Without it, HandleInput retains
// the compatibility pointer (mouse or first direct touch/pen), so existing
// widgets do not treat fingers resting on a trackpad as mouse presses.
func (e *Element) TrackContacts() *Element { e.allContacts = true; return e }

func (rt *engine) contactHandler(chain []uint64) *state {
	for _, id := range chain {
		if s := rt.states[id]; s != nil && s.input != nil && s.allContacts && s.flags&flagDisabled == 0 {
			return s
		}
	}
	return nil
}

type pointerContact struct {
	ev                           platform.SurfaceEvent
	chain                        []uint64
	owner                        uint64
	compat, explicit, suppressed bool
	buttons                      uint32
	fn                           func(InputEvent) bool
	ox, oy                       float32
}

// CapturePointer routes an active contact's moves and end to this element
// until release or cancellation, including outside its bounds. It returns
// false for an inactive contact or an element without HandleInput. Taking
// a down event captures implicitly; explicit capture also prevents touch
// gestures and scrolling from claiming that contact. Call while building
// the view or in an input callback, on the UI thread.
func (e *Element) CapturePointer(id uint64) bool {
	if e == nil || e.c == nil {
		return false
	}
	return e.c.rt.capturePointer(id, e.id, true)
}

// ReleasePointer releases this element's capture and sends CaptureLost.
// It does not produce a click. Call on the UI thread.
func (e *Element) ReleasePointer(id uint64) {
	if e == nil || e.c == nil {
		return
	}
	rt := e.c.rt
	if p := rt.contacts[id]; p != nil && p.owner == e.id {
		rt.loseCapture(p)
		if p.compat {
			rt.clearPointerPress()
		}
	}
}

func (rt *engine) contactInput(p *pointerContact, s *state, kind InputKind) bool {
	fn := p.fn
	x, y := p.ox, p.oy
	if s != nil && s.input != nil && (p.compat || s.allContacts || p.explicit) {
		fn, x, y = s.input, s.x, s.y
		if p.owner == s.id {
			p.fn, p.ox, p.oy = fn, x, y
		}
	} else if kind != InputPointerCancel && kind != InputPointerCaptureLost && kind != InputPointerUp {
		return false
	}
	if fn == nil {
		return false
	}
	ev := p.ev
	taken := fn(InputEvent{Kind: kind, Pointer: ev.Pointer,
		X: float32(ev.X) - x, Y: float32(ev.Y) - y, Button: ev.Button,
		Clicks: max(ev.Clicks, 1), Cancelled: kind == InputPointerUp && ev.Kind == platform.PointerCancel, Mods: Modifiers(ev.Mods)})
	if taken {
		rt.requestFrame()
	}
	return taken
}

func (rt *engine) capturePointer(id, owner uint64, explicit bool) bool {
	p, s := rt.contacts[id], rt.states[owner]
	if p == nil || p.suppressed || !p.ev.Pointer.Contact || s == nil || s.input == nil || s.flags&flagDisabled != 0 {
		return false
	}
	if p.owner != owner {
		if p.compat && rt.pressed != nil && rt.pressed.id != owner {
			rt.clearPointerPress()
		}
		rt.loseCapture(p)
		p.owner = owner
		rt.contactInput(p, s, InputPointerCapture)
	}
	p.explicit = p.owner == owner && (p.explicit || explicit)
	return p.owner == owner
}

func (rt *engine) loseCapture(p *pointerContact) {
	owner := p.owner
	p.owner, p.explicit = 0, false
	if owner != 0 {
		rt.contactInput(p, rt.states[owner], InputPointerCaptureLost)
	}
	if p.owner == 0 {
		p.fn = nil
	}
}

func (rt *engine) clearPointerPress() {
	if p := rt.pressed; p != nil {
		p.pressed = false
		if p.editor != nil {
			p.editor.release()
		}
	}
	rt.pressed, rt.drag, rt.scrollDrag.st = nil, nil, nil
	rt.setHover(rt.hitChain(rt.pointerX, rt.pointerY))
	rt.requestFrame()
}

func (rt *engine) cancelContact(p *pointerContact) {
	if !p.suppressed {
		p.ev.Pointer.Contact = false
		p.ev.Kind = platform.PointerCancel
		if !rt.contactInput(p, rt.states[p.owner], InputPointerCancel) && p.compat {
			button := p.ev.Button
			for b := 0; b < 3; b++ {
				if p.buttons&(1<<b) != 0 {
					p.ev.Button = b
					rt.contactInput(p, rt.states[p.owner], InputPointerUp)
				}
			}
			p.ev.Button = button
		}
		rt.loseCapture(p)
		p.suppressed = true
		if p.compat {
			rt.clearPointerPress()
		}
	}
}

func (rt *engine) cancelPointers() {
	rt.endTouchGesture(GestureCancel)
	for _, g := range rt.nativeGestures {
		rt.finishGesture(g, GestureCancel)
		g.cancelled = true
	}
	for id, p := range rt.contacts {
		rt.cancelContact(p)
		delete(rt.contacts, id)
	}
	rt.legacyActive = false
	rt.clearPointerPress()
}

func (rt *engine) prunePointers() {
	alive := func(id uint64) bool {
		s := rt.states[id]
		return s != nil && s.seen == rt.frame && s.pass == rt.pass && s.flags&(flagDisabled|flagInvisible|flagInert) == 0
	}
	blocked := false
	for _, p := range rt.contacts {
		if p.owner == 0 {
			continue
		}
		s := rt.states[p.owner]
		if !alive(p.owner) || s.input == nil || !p.compat && !s.allContacts && !p.explicit {
			rt.cancelContact(p)
			blocked = blocked || p.ev.Pointer.Device == PointerTouch
		} else {
			p.fn, p.ox, p.oy = s.input, s.x, s.y
		}
	}
	if g := rt.touchGesture.session; g != nil && g.owner != 0 {
		s := rt.states[g.owner]
		if !alive(g.owner) || g.claimed && (s.gesture == nil || s.gestureKinds&g.ev.Kind != g.ev.Kind) {
			blocked = true
		} else if g.claimed {
			g.fn = s.gesture
		}
	}
	if blocked {
		rt.endTouchGesture(GestureCancel)
		rt.touchGesture.blocked = true
	}
	for _, g := range rt.nativeGestures {
		if g.cancelled || g.owner == 0 {
			continue
		}
		s := rt.states[g.owner]
		if !alive(g.owner) || g.claimed && (s.gesture == nil || s.gestureKinds&g.ev.Kind != g.ev.Kind) {
			rt.finishGesture(g, GestureCancel)
			g.cancelled = true
		} else if g.claimed {
			g.fn = s.gesture
		}
	}
}

func (rt *engine) routePointer(ev platform.SurfaceEvent) bool {
	previous := rt.pointerEvent
	id := ev.Pointer.ID
	x, y := float32(ev.X), float32(ev.Y)
	rt.pointerEvent = ev
	p := rt.contacts[id]
	alreadyCaptured := p != nil && p.owner != 0
	if p == nil && (ev.Pointer.Device == PointerTouch || ev.Pointer.Device == PointerTouchpad) && (ev.Kind == platform.PointerMove || ev.Kind == platform.PointerUp) {
		return false
	}
	if ev.Pointer.Device == PointerMouse {
		ev.Pointer.Primary = true
		if ev.Kind == platform.PointerDown {
			ev.Pointer.Contact = true
		}
		if ev.Kind == platform.PointerMove && p != nil {
			ev.Pointer.Contact = true
		}
		rt.pointerEvent = ev
	}
	if ev.Kind == platform.PointerCancel || ev.Kind == platform.PointerCaptureLost {
		if p != nil {
			rt.cancelContact(p)
			delete(rt.contacts, id)
			if p.compat {
				rt.legacyActive = false
			}
			rt.touchChanged(ev.Kind)
		}
		return true
	}
	if ev.Kind == platform.PointerDown {
		if rt.contacts == nil {
			rt.contacts = make(map[uint64]*pointerContact)
		}
		if p != nil && ev.Pointer.Device != PointerMouse {
			rt.cancelContact(p)
			if p.compat {
				rt.legacyActive = false
			}
			delete(rt.contacts, id)
			rt.touchChanged(platform.PointerCancel)
		}
		if p == nil || ev.Pointer.Device != PointerMouse {
			p = &pointerContact{ev: ev, chain: rt.hitChain(x, y)}
			rt.contacts[id] = p
			p.compat = ev.Pointer.Device != PointerTouchpad && !rt.legacyActive
			if p.compat {
				rt.legacyID, rt.legacyActive = id, true
			}
		}
		p.ev = ev
		p.buttons |= 1 << max(ev.Button, 0)
		if p.compat && alreadyCaptured && ev.Pointer.Device == PointerMouse {
			rt.pointerMove(x, y)
			rt.contactInput(p, rt.states[p.owner], InputPointerDown)
		} else if p.compat {
			rt.pointerMove(x, y)
			rt.pointerDown(x, y, ev.Button, Modifiers(ev.Mods), ev.Clicks)
			if rt.pressed != nil && rt.pressed.input != nil && p.owner == 0 {
				rt.capturePointer(id, rt.pressed.id, false)
			}
		} else if h := rt.contactHandler(p.chain); h != nil {
			if rt.contactInput(p, h, InputPointerDown) && p.owner == 0 {
				rt.capturePointer(id, h.id, false)
			}
		}
		if p.compat && p.owner != 0 && rt.pressed != nil && rt.pressed.id != p.owner {
			rt.clearPointerPress()
		}
		rt.touchChanged(ev.Kind)
		return p.owner != 0
	}
	if p != nil {
		p.ev = ev
	}
	if ev.Kind == platform.PointerMove && p != nil && ev.Pointer.Device == PointerTouch {
		rt.touchChanged(ev.Kind)
	}
	if p != nil && p.suppressed {
		if ev.Kind == platform.PointerUp {
			delete(rt.contacts, id)
			if p.compat {
				rt.legacyActive = false
			}
			rt.touchChanged(ev.Kind)
		}
		return true
	}
	compat := p != nil && p.compat || p == nil && ev.Pointer.Device != PointerTouchpad && !rt.legacyActive
	if p == nil && ev.Pointer.Device == PointerMouse && (!rt.legacyActive || rt.legacyID == 0) {
		compat = true
	}
	if compat && p != nil && p.owner != 0 && (rt.pressed == nil || rt.pressed.id != p.owner) && (ev.Kind == platform.PointerMove || ev.Kind == platform.PointerUp) {
		kind := InputPointerMove
		if ev.Kind == platform.PointerUp {
			kind = InputPointerUp
		}
		rt.pointerX, rt.pointerY, rt.pointerIn = x, y, true
		rt.contactInput(p, rt.states[p.owner], kind)
		if ev.Kind == platform.PointerUp {
			rt.clearPointerPress()
		}
	} else if compat {
		switch ev.Kind {
		case platform.PointerMove:
			stationary := x == rt.pointerX && y == rt.pointerY
			rt.pointerMove(x, y)
			if stationary && previous.Pointer != ev.Pointer {
				if p != nil && p.owner != 0 {
					rt.contactInput(p, rt.states[p.owner], InputPointerMove)
				} else if h := rt.handler(rt.hitChain(x, y)); h != nil {
					rt.contactInput(&pointerContact{ev: ev, compat: compat}, h, InputPointerMove)
				}
			}
		case platform.PointerUp:
			rt.pointerMove(x, y)
			rt.pointerUp(ev.Button, ev.Clicks)
		case platform.PointerEnter:
			rt.pointerIn = true
			rt.pointerMove(x, y)
		case platform.PointerLeave:
			rt.pointerIn = false
			if rt.pressed == nil {
				rt.setHover(nil)
			} else {
				rt.requestFrame()
			}
		}
	} else if p == nil && ev.Kind == platform.PointerMove {
		ev.Button = -1
		rt.contactInput(&pointerContact{ev: ev}, rt.contactHandler(rt.hitChain(x, y)), InputPointerMove)
	} else if p != nil && (ev.Kind == platform.PointerMove || ev.Kind == platform.PointerUp) {
		h := rt.states[p.owner]
		if h == nil {
			h = rt.contactHandler(rt.hitChain(x, y))
		}
		kind := InputPointerMove
		if ev.Kind == platform.PointerUp {
			kind = InputPointerUp
		}
		rt.contactInput(p, h, kind)
	}
	if ev.Kind == platform.PointerEnter || ev.Kind == platform.PointerLeave {
		h := rt.handler(rt.hitChain(x, y))
		if !compat {
			h = rt.contactHandler(rt.hitChain(x, y))
		}
		if p != nil && p.owner != 0 {
			h = rt.states[p.owner]
		}
		if h != nil {
			kind := InputPointerEnter
			if ev.Kind == platform.PointerLeave {
				kind = InputPointerLeave
			}
			rt.contactInput(&pointerContact{ev: ev, compat: compat}, h, kind)
		}
	}
	if ev.Kind == platform.PointerUp && p != nil {
		p.buttons &^= 1 << max(ev.Button, 0)
		if p.buttons == 0 {
			p.ev.Pointer.Contact = false
			rt.loseCapture(p)
			delete(rt.contacts, id)
			if p.compat {
				rt.legacyActive = false
			}
			rt.touchChanged(ev.Kind)
		}
	}
	return p != nil && p.owner != 0
}

// touchContacts excludes indirect trackpad contacts: the OS recognizes
// those gestures. Sorting makes the pair used for rotation deterministic.
func (rt *engine) touchContacts() []*pointerContact {
	var contacts []*pointerContact
	for _, p := range rt.contacts {
		if p.ev.Pointer.Device == PointerTouch {
			contacts = append(contacts, p)
		}
	}
	slices.SortFunc(contacts, func(a, b *pointerContact) int {
		if a.ev.Pointer.ID < b.ev.Pointer.ID {
			return -1
		}
		if a.ev.Pointer.ID > b.ev.Pointer.ID {
			return 1
		}
		return 0
	})
	return contacts
}
