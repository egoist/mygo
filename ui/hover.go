package ui

// OnHover calls fn when the pointer enters or leaves this element, including
// when it disappears, is disabled, or a layout change moves it away. It follows
// Hovered's press behavior. Calls run on the main thread after input or a
// committed frame, never while the view is being constructed. A stable Key
// keeps hover identity when rows reorder. The callback must not retain Element
// or Context values from a build.
func (e *node) OnHover(fn func(bool)) *node {
	if e.hasState() {
		e.flags |= flagHover
		e.hoverFn = fn
	}
	return e
}

type hoverChange struct {
	state *state
	id    uint64
	fn    func(bool)
	over  bool
}

func (rt *engine) changeHover(s *state, over bool) {
	if s.onHover == nil || s.hovered == over {
		return
	}
	s.hovered = over
	rt.hoverChanges = append(rt.hoverChanges, hoverChange{state: s, id: s.id, fn: s.onHover, over: over})
}

func (rt *engine) syncHoverCallbacks() {
	for _, id := range rt.hover {
		s := rt.states[id]
		if s == nil {
			continue
		}
		over := (rt.pointerIn || rt.pressed == s) && s.flags&flagDisabled == 0 && s.vw > 0 && s.vh > 0
		if rt.pressed != nil && rt.pressed != s {
			over = over && rt.contains(s, Rect{s.x, s.y, s.w, s.h}, rt.pointerX, rt.pointerY)
		}
		rt.changeHover(s, over)
	}
}

func (rt *engine) publishHoverChanges() {
	if rt.inFrame || len(rt.hoverChanges) == 0 {
		return
	}
	changes := rt.hoverChanges
	rt.hoverChanges = nil
	for _, change := range changes {
		if rt.closed {
			break
		}
		// A pruned/reused state cannot deliver a stale enter. Its old leave
		// still reaches the old callback so it can clear external highlighting.
		if change.over && (rt.states[change.id] != change.state || !change.state.hovered) {
			continue
		}
		change.fn(change.over)
		rt.requestFrame()
	}
}
