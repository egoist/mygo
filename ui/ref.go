package ui

import "weak"

// Ref is a persistent identity for a control in one window. Bind its
// address with Element.Ref. It owns no pooled node and does not keep a
// closed window alive. Its zero value is ready for use.
// Use it on the UI thread; background work uses Window.Update.
type Ref struct{ state *refState }

type refState struct {
	owner             weak.Pointer[engine]
	element           Element
	requested, closed bool
}

func (r *Ref) init() *refState {
	if r == nil {
		return nil
	}
	if r.state == nil {
		r.state = &refState{}
	}
	return r.state
}

// RequestFocus focuses the control when it is next built. Repeated
// requests coalesce. A hidden control retains the request until it returns;
// closing its window cancels it.
func (r *Ref) RequestFocus() {
	s := r.init()
	if s == nil || s.closed {
		return
	}
	s.requested = true
	if rt := s.owner.Value(); rt != nil && !rt.closed {
		rt.requestFrame()
	}
}

// CancelFocus cancels an outstanding focus request.
func (r *Ref) CancelFocus() {
	if r != nil && r.state != nil {
		r.state.requested = false
	}
}

// Ref binds a control to a persistent reference. Each Ref can name one
// control per build and belongs to one window.
func (e Element) Ref(r *Ref) Element {
	n := e.unbuilt()
	if n == nil || r == nil {
		return e
	}
	s := r.init()
	rt := n.c.rt
	if s.closed {
		return e
	}
	if owner := s.owner.Value(); owner != nil && owner != rt {
		panic("ui: Ref belongs to another window")
	}
	if s.element.Valid() && s.element != e {
		panic("ui: Ref bound to two controls in one build")
	}
	if s.owner.Value() == nil {
		s.owner = rt.owner
		rt.refs = append(rt.refs, s)
	}
	s.element = e
	return e
}

// Resolve returns this build's element for ref, or an absent element.
func (f Frame) Resolve(ref Ref) Element {
	rt := f.runtime()
	s := ref.state
	if rt == nil || s == nil || s.closed || s.owner.Value() != rt || !s.element.Valid() {
		return Element{}
	}
	return s.element
}

func (f Frame) FocusWithin(ref Ref) bool { return f.Resolve(ref).FocusWithin() }
func (f Frame) Focused(ref Ref) bool     { return f.Resolve(ref).Focused() }

func (rt *engine) applyFocusRequests() {
	for _, s := range rt.refs {
		if !s.requested || s.closed {
			continue
		}
		n := s.element.nodeFor(rt)
		if n == nil || n.disabled() {
			continue
		}
		s.requested = false
		if rt.focused != n.id {
			n.Focus()
			rt.consumed = true
		}
	}
}
