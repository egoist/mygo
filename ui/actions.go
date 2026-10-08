package ui

type actionKind uint8

const (
	actionClick actionKind = iota
	actionChange
	actionSubmit
	actionShortcut
	actionWindowShortcut
)

type action struct {
	element Element
	frame   Frame
	kind    actionKind
	mods    Modifiers
	key     Key
	fn      func()
}

func (e Element) on(kind actionKind, mods Modifiers, key Key, fn func()) Element {
	n := e.unbuilt()
	if n == nil || fn == nil {
		return e
	}
	if kind == actionClick {
		n.flags |= flagClickable
		if n.pending != 0 {
			p := &n.c.rt.pending[n.pending-1]
			p.flags |= flagClickable
		}
	}
	n.c.rt.actions = append(n.c.rt.actions, action{element: e, kind: kind, mods: mods, key: key, fn: fn})
	return e
}

// OnClick registers an action that runs on the UI thread after the view's
// configuration is built, once for the handled input.
func (e Element) OnClick(fn func()) Element { return e.on(actionClick, 0, 0, fn) }

// OnChange registers an action for a user change of the bound value.
func (e Element) OnChange(fn func()) Element { return e.on(actionChange, 0, 0, fn) }

// OnSubmit registers an action for a widget's submission gesture.
func (e Element) OnSubmit(fn func()) Element { return e.on(actionSubmit, 0, 0, fn) }

// OnShortcut handles a key while focus is in the element or its children.
func (e Element) OnShortcut(mods Modifiers, key Key, fn func()) Element {
	return e.on(actionShortcut, mods, key, fn)
}

// OnShortcut registers an action scoped to this frame's parent. Root
// shortcuts handle keys left by the focused control and active overlays.
func (f Frame) OnShortcut(mods Modifiers, key Key, fn func()) {
	rt := f.runtime()
	if rt == nil || fn == nil {
		return
	}
	if f.parent != 0 {
		Element{owner: f.owner, epoch: f.epoch, slot: f.parent}.OnShortcut(mods, key, fn)
		return
	}
	rt.actions = append(rt.actions, action{frame: f, kind: actionWindowShortcut, mods: mods, key: key, fn: fn})
}

func (rt *engine) runActions() {
	for _, a := range rt.actions {
		if rt.closed {
			return
		}
		handled := false
		if a.kind == actionWindowShortcut {
			s := a.frame.enter()
			if s.c != nil {
				handled = s.c.Shortcut(a.mods, a.key)
			}
			s.leave()
		} else if n := a.element.nodeFor(rt); n != nil && !n.disabled() {
			switch a.kind {
			case actionClick:
				handled = n.Clicked()
			case actionChange:
				handled = n.Changed()
			case actionSubmit:
				handled = n.Submitted()
			case actionShortcut:
				handled = n.Shortcut(a.mods, a.key)
			}
		}
		if handled {
			rt.consumed = true
			a.fn()
		}
	}
}
