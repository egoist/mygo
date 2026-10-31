package ui

import (
	"math"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// A finger on a touch screen presses what it touches, as a mouse does,
// until it moves: then it pans what scrolls under it, as UIKit's scroll
// views and Android's do, and the press ends without a click. Lifted
// while it moves, it flings the content on, which slows down as UIKit's
// decelerates (UIScrollView.DecelerationRate.normal), from frame to
// frame of the window's clock.

// Desktops send no touches: what handles them is reached only where the
// platform has touch screens (touch_mobile.go), or a Tester touches, for
// apps of desktops not to link it.
var (
	touchInput func(rt *engine, ev platform.SurfaceEvent, x, y float32) bool
	touchFrame func(rt *engine, now time.Time)
	// insetRoot keeps the content out from under what covers the
	// window's edges, which only phones' do.
	insetRoot     func(rt *engine, root *node)
	extendScrolls func(rt *engine, root *node, h float32)
)

// enableTouch has the engines handle touches.
func enableTouch() {
	touchInput = (*engine).touchEvent
	touchFrame = (*engine).flingStep
	insetRoot = (*engine).padInsets
	extendScrolls = (*engine).extendUnderBottom
}

// touchSlop is how far a finger moves, in DIPs, before it pans rather
// than presses: UIKit's and Android's touch slop, about a millimeter.
const touchSlop = 8

// flingDecay is how much of its speed a fling keeps each millisecond,
// UIKit's normal deceleration.
const flingDecay = 0.998

// flingStop is the speed, in DIPs a second, under which a fling stops.
const flingStop = 20

// touchPan is the finger on the screen.
type touchPan struct {
	down    bool
	panning bool
	// swallow drops what the finger does: it went down to stop a fling,
	// which takes no tap.
	swallow bool
	x0, y0  float32 // where it went down
	lx, ly  float32 // where it was last
	// samples are its last positions, for its speed as it lifts.
	samples [6]touchSample
	n       int
}

type touchSample struct {
	t    time.Time
	x, y float32
}

// fling is content going on after a finger lifted as it panned.
type fling struct {
	on     bool
	x, y   float32 // where, for the hit chain
	vx, vy float32 // DIPs a second
	at     time.Time
}

// touchEvent handles the pointer events of a finger, and reports whether
// it took the event, which the mouse's handling then skips.
func (rt *engine) touchEvent(ev platform.SurfaceEvent, x, y float32) bool {
	t := &rt.touch
	switch ev.Kind {
	case platform.PointerDown:
		*t = touchPan{down: true, x0: x, y0: y, lx: x, ly: y}
		t.sample(rt.now(), x, y)
		if rt.flinging.on {
			// A finger stops content flinging, and taps nothing.
			rt.flinging.on = false
			t.swallow = true
			return true
		}
		return false
	case platform.PointerMove:
		if !t.down {
			return false
		}
		if t.swallow {
			return true
		}
		if !t.panning {
			if math.Hypot(float64(x-t.x0), float64(y-t.y0)) < touchSlop || rt.dragsPress() {
				return false
			}
			t.panning = true
			rt.cancelPress()
			// From where it went down, past the slop: the content does
			// not jump.
			t.lx, t.ly = x, y
		}
		dx, dy := t.lx-x, t.ly-y
		t.lx, t.ly = x, y
		t.sample(rt.now(), x, y)
		rt.pan(t.x0, t.y0, dx, dy, false)
		return true
	case platform.PointerUp:
		if !t.down {
			return false
		}
		was := *t
		t.down = false
		if was.swallow {
			return true
		}
		if !was.panning {
			return false
		}
		t.sample(rt.now(), x, y)
		vx, vy := t.velocity(rt.now())
		if math.Hypot(float64(vx), float64(vy)) > flingStop && !rt.preferences().ReduceMotion {
			rt.flinging = fling{on: true, x: was.x0, y: was.y0, vx: vx, vy: vy, at: rt.now()}
			rt.requestFrame()
		}
		return true
	case platform.PointerLeave:
		return t.panning || t.swallow
	}
	return false
}

func (t *touchPan) sample(now time.Time, x, y float32) {
	copy(t.samples[1:], t.samples[:len(t.samples)-1])
	t.samples[0] = touchSample{now, x, y}
	t.n = min(t.n+1, len(t.samples))
}

// velocity is the finger's speed over its last tenth of a second, in DIPs
// a second the content moves: against the finger.
func (t *touchPan) velocity(now time.Time) (float32, float32) {
	last := t.samples[0]
	first := last
	for i := 1; i < t.n; i++ {
		if now.Sub(t.samples[i].t) > 100*time.Millisecond {
			break
		}
		first = t.samples[i]
	}
	dt := float32(last.t.Sub(first.t).Seconds())
	if dt <= 0 || now.Sub(last.t) > 100*time.Millisecond {
		return 0, 0
	}
	return (first.x - last.x) / dt, (first.y - last.y) / dt
}

// dragsPress reports whether the element pressed takes the finger's moves
// itself, as a slider or an element one drags, rather than panning.
func (rt *engine) dragsPress() bool {
	p := rt.pressed
	return p != nil && p.flags&(flagDraggable|flagTrackPointer) != 0
}

// cancelPress ends the press of a finger that pans, without a click.
func (rt *engine) cancelPress() {
	p := rt.pressed
	if p == nil {
		return
	}
	if p.input != nil {
		rt.deliver(p, InputEvent{Kind: InputPointerUp, Button: rt.pressButton, Mods: rt.mods, Clicks: rt.pressClicks})
	}
	if p.editor != nil {
		p.editor.release()
	}
	rt.selection.dragging = false
	p.pressed = false
	rt.pressed = nil
	rt.setHover(nil)
	rt.requestFrame()
}

// pan scrolls what is under x, y by dx, dy: the innermost element
// handling its input takes it, as it takes a touchpad's scrolling, or
// else the innermost scroll container that can move that way. It reports
// whether something moved.
func (rt *engine) pan(x, y, dx, dy float32, momentum bool) bool {
	chain := rt.appendHitChain(rt.chain[:0], x, y)
	rt.chain = chain[:0]
	ev := InputEvent{Kind: InputScroll, DX: dx, DY: dy, Mods: rt.mods, Precise: true, Momentum: momentum}
	for _, id := range chain {
		if s := rt.states[id]; s != nil && s.input != nil && s.flags&flagDisabled == 0 && rt.deliver(s, ev) {
			return true
		}
	}
	for _, id := range chain {
		s := rt.states[id]
		if s == nil {
			continue
		}
		sx, sy := s.vector(x, y, dx, dy)
		s.wheel = nil
		if scrollBy(s, sx, sy) {
			rt.requestFrame()
			return true
		}
	}
	return false
}

// flingStep moves flinging content on to now, slowing it down, and stops
// it once it is slow or can go no further.
func (rt *engine) flingStep(now time.Time) {
	f := &rt.flinging
	if !f.on {
		return
	}
	ms := float32(now.Sub(f.at).Seconds() * 1000)
	f.at = now
	if ms <= 0 {
		rt.animating = true
		return
	}
	// The distance of a speed decaying by flingDecay each millisecond.
	k := float32(math.Pow(flingDecay, float64(ms)))
	scale := (1 - k) / -float32(math.Log(flingDecay)) / 1000
	dx, dy := f.vx*scale, f.vy*scale
	f.vx, f.vy = f.vx*k, f.vy*k
	if !rt.pan(f.x, f.y, dx, dy, true) || math.Hypot(float64(f.vx), float64(f.vy)) < flingStop {
		f.on = false
		return
	}
	rt.animating = true
}
