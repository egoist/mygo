package ui

import "weak"

// Invalidate requests a frame from any goroutine. It reaches only the
// original window and touches no build data. Prefer Services when retaining
// a redraw function outside a view.
func (f Frame) Invalidate() { Services{owner: f.owner}.Invalidate() }

//go:generate go run ../internal/uigen

// Frame describes a window's interface during one build pass. It is a
// checked value: copies keep their original window, parent and generation.
// Children and widget builders receive a Frame scoped to their parent.
// Use it on the UI thread. Publish background results with Window.Update.
type Frame struct {
	owner  weak.Pointer[engine]
	epoch  uint64
	parent uint32
}

// Element is a checked value handle to a private, pooled node. Its zero
// value is absent. Old copies expire before the next build pass; they
// cannot become references to a different node when storage is reused.
// Keep a Ref in app state for a control's persistent identity.
type Element struct {
	owner weak.Pointer[engine]
	epoch uint64
	slot  uint32
}

type partsBox[T any] struct{ value *T }

func makeFrame(c *context) Frame {
	if c == nil || c.rt == nil || c.parent == nil {
		return Frame{}
	}
	return Frame{owner: c.rt.owner, epoch: c.rt.epoch, parent: uint32(c.parent.serial - 1)}
}

func wrapElement(n *node) Element {
	if n == nil || n.c == nil || n.c.rt == nil || n.serial <= 0 {
		return Element{}
	}
	return Element{owner: n.c.rt.owner, epoch: n.epoch, slot: uint32(n.serial - 1)}
}

func (f Frame) runtime() *engine {
	rt := f.owner.Value()
	if rt == nil || rt.closed || rt.epoch != f.epoch || !rt.inFrame {
		return nil
	}
	return rt
}

type frameScope struct {
	c      *context
	parent *node
}

func (f Frame) enter() frameScope {
	rt := f.runtime()
	if rt == nil {
		return frameScope{}
	}
	n := rt.nodeAt(f.parent)
	if n == nil {
		return frameScope{}
	}
	s := frameScope{c: &rt.c, parent: rt.c.parent}
	rt.c.parent = n
	return s
}

func (s frameScope) leave() {
	if s.c != nil {
		s.c.parent = s.parent
	}
}

func (rt *engine) nodeAt(slot uint32) *node {
	if uint64(slot) >= uint64(rt.c.used) {
		return nil
	}
	return &rt.c.chunks[int(slot)/chunkSize][int(slot)%chunkSize]
}

func (e Element) unbuilt() *node {
	rt := e.owner.Value()
	if rt == nil || rt.closed || rt.epoch != e.epoch || !rt.inFrame {
		return nil
	}
	n := rt.nodeAt(e.slot)
	if n == nil || n.epoch != e.epoch {
		return nil
	}
	return n
}

func (e Element) node() *node {
	n := e.unbuilt()
	if n == nil {
		return nil
	}
	n.c.rt.realize(n)
	if n.redirect != nil {
		return n.redirect
	}
	return n
}

func (e Element) nodeFor(rt *engine) *node {
	if e.owner.Value() != rt {
		return nil
	}
	return e.node()
}

// Valid reports whether the handle belongs to the active build pass.
func (e Element) Valid() bool { return e.unbuilt() != nil }

// Valid reports whether the frame belongs to the active build pass.
func (f Frame) Valid() bool { return f.runtime() != nil }

// Children builds the element's children with an explicitly scoped Frame.
// An absent or expired element does not run fn.
func (e Element) Children(fn func(Frame)) Element {
	n := e.node()
	if n != nil && fn != nil {
		n.Children(func() { fn(makeFrame(n.c)) })
	}
	return e
}

// Key identifies a control among its siblings before its input is handled.
// Put it before Children, interaction queries or custom local state.
func (e Element) Key(key any) Element {
	n := e.unbuilt()
	if n == nil {
		return e
	}
	if n.pending != 0 {
		n.key = key
		n.id = keyedID(n.parent.id, key)
	} else {
		n.Key(key)
	}
	return e
}

// MaterialBuilder creates a material using the control's checked handle.
type MaterialBuilder interface {
	Material
	BuildMaterial(Element) Material
}

func (e Element) Material(m Material) Element {
	n := e.node()
	if n != nil {
		if b, ok := m.(MaterialBuilder); ok {
			m = b.BuildMaterial(e)
		}
		n.Material(m)
	}
	return e
}

func (rt *engine) keepPart(p any) { rt.parts = append(rt.parts, p) }

func (r *Router) rtContext() *context {
	if r == nil || r.rt == nil || r.rt.closed || !r.rt.inFrame {
		return nil
	}
	return &r.rt.c
}
