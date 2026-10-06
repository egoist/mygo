package ui

import (
	"math"
	"slices"

	"github.com/egoist/mygo/internal/platform"
)

// GestureKind is a set of transformations that share a gesture owner.
type GestureKind = platform.GestureKind

const (
	GesturePan      = platform.GesturePan
	GesturePinch    = platform.GesturePinch
	GestureRotation = platform.GestureRotation
)

// GesturePhase describes the start, updates, end or cancellation of a gesture.
type GesturePhase = platform.GesturePhase

const (
	GestureBegin  = platform.GestureBegin
	GestureUpdate = platform.GestureUpdate
	GestureEnd    = platform.GestureEnd
	GestureCancel = platform.GestureCancel
)

// GestureEvent reports transformations since the previous event. X/Y are
// the focal point relative to the element. DX/DY follow the fingers in
// DIPs, Scale is a multiplier (1 means unchanged), and Rotation is radians
// clockwise in the top-left coordinate system. TotalX/Y, TotalScale and
// TotalRotation accumulate updates since Begin. Begin and terminal events
// have neutral deltas. Contacts is zero when the OS does not report it.
type GestureEvent struct {
	Kind                                      GestureKind
	Phase                                     GesturePhase
	Device                                    PointerDevice
	Contacts                                  int
	Mods                                      Modifiers
	X, Y, DX, DY                              float32
	Scale, Rotation                           float32
	TotalX, TotalY, TotalScale, TotalRotation float32
}

// Gestures offers pan, pinch and rotation to fn before scrolling. The
// innermost eligible element gets Begin first; returning true claims the
// requested transformations together until End/Cancel, even outside its
// bounds. Returning false offers them to ancestors, then scroll containers
// for pan. Return values after Begin do not change ownership. Explicit
// pointer capture prevents touch recognition; a claimed touch gesture
// cancels ordinary presses and implicit captures so they produce no click
// or drag. Contact-count changes end the old gesture and reset its baseline.
// Mouse wheel input stays scrolling; precise phased trackpad scrolling can
// be claimed as pan. Callbacks run on the UI thread, before the next frame.
func (e *Element) Gestures(kinds GestureKind, fn func(GestureEvent) bool) *Element {
	e.gestureKinds, e.gestureFn = kinds, fn
	return e
}

type gestureSession struct {
	owner                      uint64
	chain                      []uint64
	fn                         func(GestureEvent) bool
	ev                         GestureEvent
	ox, oy                     float32
	claimed, scroll, cancelled bool
}

type touchSample struct{ x, y, distance, angle float64 }
type touchGesture struct {
	count      int
	base, last touchSample
	session    *gestureSession
	decided    bool
	blocked    bool
}

func sampleTouches(contacts []*pointerContact) touchSample {
	var v touchSample
	for _, p := range contacts {
		v.x += p.ev.X
		v.y += p.ev.Y
	}
	v.x /= float64(len(contacts))
	v.y /= float64(len(contacts))
	if len(contacts) > 1 {
		a, b := contacts[0].ev, contacts[1].ev
		v.distance, v.angle = math.Hypot(b.X-a.X, b.Y-a.Y), math.Atan2(b.Y-a.Y, b.X-a.X)
	}
	return v
}

func angleDelta(a, b float64) float64 { return math.Atan2(math.Sin(a-b), math.Cos(a-b)) }

func (rt *engine) gestureEvent(g *gestureSession, phase GesturePhase, dx, dy, scale, rotation float32) GestureEvent {
	ev := g.ev
	ev.Phase, ev.DX, ev.DY, ev.Scale, ev.Rotation = phase, dx, dy, scale, rotation
	if ev.Kind&GesturePan == 0 {
		ev.DX, ev.DY = 0, 0
	}
	if ev.Kind&GesturePinch == 0 {
		ev.Scale = 1
	}
	if ev.Kind&GestureRotation == 0 {
		ev.Rotation = 0
	}
	if s := rt.states[g.owner]; s != nil {
		g.ox, g.oy = s.x, s.y
	}
	ev.X, ev.Y = ev.X-g.ox, ev.Y-g.oy
	return ev
}

func (rt *engine) chooseGesture(chain []uint64, ev GestureEvent, dx, dy float32) *gestureSession {
	g := &gestureSession{chain: slices.Clone(chain), ev: ev}
	g.ev.TotalScale = 1
	for _, id := range chain {
		s := rt.states[id]
		if s == nil || s.flags&flagDisabled != 0 || s.gesture == nil {
			continue
		}
		if kinds := ev.Kind & s.gestureKinds; kinds != 0 {
			g.owner, g.fn, g.ev.Kind = id, s.gesture, kinds
			if g.fn(rt.gestureEvent(g, GestureBegin, 0, 0, 1, 0)) {
				g.claimed = true
				rt.requestFrame()
				return g
			}
		}
	}
	g.owner, g.fn, g.ev.Kind = 0, nil, ev.Kind
	if ev.Kind&GesturePan != 0 {
		for _, id := range chain {
			s := rt.states[id]
			if s != nil && s.flags&flagDisabled == 0 && canPanScroll(s, -dx, -dy) {
				g.owner, g.scroll, g.ev.Kind = id, true, GesturePan
				break
			}
		}
	}
	return g
}

func canPanScroll(s *state, dx, dy float32) bool {
	return s.flags&flagScrollX != 0 && (dx < 0 && s.scrollX > 0 || dx > 0 && s.scrollX < s.contentW-float64(s.w)) ||
		s.flags&flagScrollY != 0 && (dy < 0 && s.scrollY > 0 || dy > 0 && s.scrollY < s.contentH-float64(s.h))
}

func (rt *engine) updateGesture(g *gestureSession, dx, dy, scale, rotation float32) {
	if g.ev.Kind&GesturePan != 0 {
		g.ev.TotalX += dx
		g.ev.TotalY += dy
	}
	if g.ev.Kind&GesturePinch != 0 {
		g.ev.TotalScale *= scale
	}
	if g.ev.Kind&GestureRotation != 0 {
		g.ev.TotalRotation += rotation
	}
	if g.claimed {
		g.fn(rt.gestureEvent(g, GestureUpdate, dx, dy, scale, rotation))
		rt.requestFrame()
	} else if g.scroll {
		if s := rt.states[g.owner]; s != nil && scrollBy(s, -dx, -dy) {
			rt.requestFrame()
		}
	}
}

func (rt *engine) finishGesture(g *gestureSession, phase GesturePhase) {
	if g != nil && g.claimed && !g.cancelled {
		g.fn(rt.gestureEvent(g, phase, 0, 0, 1, 0))
		g.fn = nil
		rt.requestFrame()
	}
}

func (rt *engine) endTouchGesture(phase GesturePhase) {
	rt.finishGesture(rt.touchGesture.session, phase)
	rt.touchGesture = touchGesture{}
}

func (rt *engine) touchChanged(kind platform.SurfaceEventKind) {
	contacts := rt.touchContacts()
	t := &rt.touchGesture
	if t.blocked {
		if len(contacts) == 0 {
			rt.touchGesture = touchGesture{}
		}
		return
	}
	if len(contacts) != t.count {
		phase := GestureEnd
		if kind == platform.PointerCancel || kind == platform.PointerCaptureLost {
			phase = GestureCancel
		}
		rt.endTouchGesture(phase)
		if len(contacts) > 0 {
			t.count = len(contacts)
			t.base = sampleTouches(contacts)
			t.last = t.base
		}
		return
	}
	if len(contacts) == 0 {
		return
	}
	if len(contacts) == 1 && rt.scrollDrag.st != nil {
		return
	}
	for _, p := range contacts {
		if p.explicit {
			return
		}
		// Sliders, selection and typed drags keep a one-finger interaction.
		if len(contacts) == 1 && p.compat && rt.pressed != nil && rt.pressed.flags&(flagDraggable|flagEditable|flagSelectable|flagDragWindow) != 0 {
			return
		}
	}
	v := sampleTouches(contacts)
	if !t.decided {
		moved := math.Hypot(v.x-t.base.x, v.y-t.base.y) >= 6
		if len(contacts) > 1 {
			moved = moved || math.Abs(v.distance-t.base.distance) >= 4 || math.Abs(angleDelta(v.angle, t.base.angle)) >= .04
		}
		if !moved {
			return
		}
		t.decided = true
		chain := slices.Clone(contacts[0].chain)
		for _, p := range contacts[1:] {
			chain = slices.DeleteFunc(chain, func(id uint64) bool { return !slices.Contains(p.chain, id) })
		}
		kinds := GesturePan
		if len(contacts) > 1 {
			kinds |= GesturePinch | GestureRotation
		}
		t.session = rt.chooseGesture(chain, GestureEvent{Kind: kinds, Device: PointerTouch, Contacts: len(contacts), X: float32(t.base.x), Y: float32(t.base.y), Mods: Modifiers(contacts[0].ev.Mods)}, float32(v.x-t.base.x), float32(v.y-t.base.y))
		if t.session.claimed || t.session.scroll {
			for _, p := range contacts {
				rt.cancelContact(p)
			}
		}
	}
	g := t.session
	if g == nil {
		return
	}
	g.ev.X, g.ev.Y, g.ev.Mods = float32(v.x), float32(v.y), Modifiers(contacts[0].ev.Mods)
	scale, rotation := float32(1), float32(0)
	if len(contacts) > 1 {
		if t.last.distance > 1e-6 && v.distance > 1e-6 {
			scale = float32(v.distance / t.last.distance)
		}
		if t.last.distance > 1e-6 && v.distance > 1e-6 {
			rotation = float32(angleDelta(v.angle, t.last.angle))
		}
	}
	rt.updateGesture(g, float32(v.x-t.last.x), float32(v.y-t.last.y), scale, rotation)
	t.last = v
}

func (rt *engine) nativeGesture(ev platform.SurfaceEvent) bool {
	rt.pointerEvent = ev
	rt.pointerX, rt.pointerY = float32(ev.X), float32(ev.Y)
	if rt.nativeGestures == nil {
		rt.nativeGestures = make(map[GestureKind]*gestureSession)
	}
	g := rt.nativeGestures[ev.Gesture]
	if g != nil && g.cancelled && ev.Phase != platform.GestureBegin {
		if ev.Phase == platform.GestureEnd || ev.Phase == platform.GestureCancel {
			delete(rt.nativeGestures, ev.Gesture)
		}
		return g.claimed
	}
	if ev.Phase == platform.GestureBegin || g == nil && ev.Phase == platform.GestureUpdate {
		rt.finishGesture(g, GestureCancel)
		// Native pan begins with zero movement. A scroll fallback is
		// decided by its updates; handlers still get Begin exactly once.
		g = rt.chooseGesture(rt.hitChain(float32(ev.X), float32(ev.Y)), GestureEvent{Kind: ev.Gesture, Device: ev.Pointer.Device, Contacts: ev.Contacts, X: float32(ev.X), Y: float32(ev.Y), Mods: Modifiers(ev.Mods)}, float32(ev.DX), float32(ev.DY))
		if !g.claimed {
			g.scroll, g.owner = false, 0
		}
		rt.nativeGestures[ev.Gesture] = g
	}
	if g == nil {
		return false
	}
	g.ev.X, g.ev.Y, g.ev.Mods = float32(ev.X), float32(ev.Y), Modifiers(ev.Mods)
	if ev.DX != 0 || ev.DY != 0 || ev.Scale != 0 && ev.Scale != 1 || ev.Rotation != 0 {
		scale := float32(ev.Scale)
		if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
			scale = 1
		}
		rt.updateGesture(g, float32(ev.DX), float32(ev.DY), scale, float32(ev.Rotation))
		if !g.claimed && !g.scroll && ev.Gesture&GesturePan != 0 {
			// An unclaimed trackpad pan stays ordinary scrolling, including
			// HandleInput, along the chain under its initial focal point.
			h := rt.handler(g.chain)
			taken := h != nil && rt.deliver(h, InputEvent{Kind: InputScroll, DX: -float32(ev.DX), DY: -float32(ev.DY), Precise: true, Mods: Modifiers(ev.Mods)})
			if !taken {
				for _, id := range g.chain {
					if s := rt.states[id]; s != nil && scrollBy(s, -float32(ev.DX), -float32(ev.DY)) {
						rt.requestFrame()
						break
					}
				}
			}
		}
	}
	if ev.Phase == platform.GestureEnd || ev.Phase == platform.GestureCancel {
		rt.finishGesture(g, GesturePhase(ev.Phase))
		delete(rt.nativeGestures, ev.Gesture)
		return g.claimed
	}
	return g.claimed
}
