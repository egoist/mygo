//go:build darwin

package darwin

import (
	"errors"
	"runtime"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

// Each control lives in a flipped, layer-backed NSView that clips its
// drawing and hit testing. Containers are children of the drawing surface
// so both Metal and CPU frames compose below them.
type nativeView struct {
	s          *surface
	h          platform.NativeViewHandler
	clip, view id
	p          platform.NativeViewPlacement
	axParent   id
	closed     bool
	disabled   map[id]bool // retained controls and their original enabled state
}

var hostedViews = map[id]*nativeView{}
var msgSuperHitView func(sup uintptr, sel objc.SEL, p NSPoint) id
var msgAccessHitView func(obj id, sel objc.SEL, p NSPoint) id
var unignoredChildren uintptr

func registerNativeViewClass() {
	purego.RegisterFunc(&msgSuperHitView, msgSendSuperAddr)
	purego.RegisterFunc(&msgAccessHitView, msgSendAddr)
	unignoredChildren = mustDlsym(libAppKit, "NSAccessibilityUnignoredChildren")
	classDef("MyGoHostedView", "NSView", nil, []objc.MethodDef{
		method("isFlipped", func(id, objc.SEL) bool { return true }),
		method("hitTest:", func(self id, cmd objc.SEL, p NSPoint) id {
			n := hostedViews[self]
			if n == nil || n.closed || !n.p.Visible {
				return 0
			}
			var pin runtime.Pinner
			defer pin.Unpin()
			target := msgSuperHitView(superOf(self, "MyGoHostedView", &pin), cmd, p)
			if target != 0 && !n.p.Enabled {
				return self
			}
			return target
		}),
		method("isAccessibilityElement", func(id, objc.SEL) bool { return true }),
		method("accessibilityRole", func(id, objc.SEL) id { return nsString("AXGroup") }),
		method("isAccessibilityEnabled", func(self id, _ objc.SEL) bool {
			n := hostedViews[self]
			return n != nil && n.p.Enabled
		}),
		method("accessibilityParent", func(self id, _ objc.SEL) id {
			if n := hostedViews[self]; n != nil {
				return n.axParent
			}
			return 0
		}),
		method("accessibilityChildren", func(self id, _ objc.SEL) id {
			n := hostedViews[self]
			if n == nil || n.closed || !n.p.Visible || n.view == 0 {
				return nsArray()
			}
			r, _, _ := purego.SyscallN(unignoredChildren, uintptr(nsArray(n.view)))
			return id(r)
		}),
	})
}

func (b *Backend) NewNativeView(s platform.Surface, h platform.NativeViewHandler) (platform.NativeViewHost, error) {
	area, ok := s.(*surface)
	if !ok || area.w.closed {
		return nil, platform.ErrUnsupported
	}
	n := &nativeView{s: area, h: h}
	withPool(func() {
		n.clip = msgInitRect(send(class("MyGoHostedView"), "alloc"), sel("initWithFrame:"), NSRect{})
		hostedViews[n.clip] = n
		send(n.clip, "setWantsLayer:", 1)
		// Metal adds its drawing layer after controls may already exist.
		// Keep native subviews above that layer regardless of insertion order.
		msgSetFloat(send(n.clip, "layer"), sel("setZPosition:"), 1)
		send(send(n.clip, "layer"), "setMasksToBounds:", 1)
		send(n.clip, "setHidden:", 1)
		send(area.view, "addSubview:", uintptr(n.clip))
	})
	area.nativeViews = append(area.nativeViews, n)
	return n, nil
}

func (n *nativeView) Parent() uintptr { return uintptr(n.clip) }

func (n *nativeView) Attach(v uintptr) error {
	view := id(v)
	if n.closed || n.view != 0 || v == 0 || !sendBool(view, "isKindOfClass:", uintptr(class("NSView"))) || send(view, "superview") != 0 {
		return errors.New("an owned, unattached NSView is required")
	}
	n.view = view
	send(view, "setAutoresizingMask:", 0)
	send(n.clip, "addSubview:", v)
	return nil
}

func (n *nativeView) Place(p platform.NativeViewPlacement) {
	if n.closed {
		return
	}
	n.p = p
	if !p.Visible {
		n.axParent = 0
	}
	withPool(func() {
		if !p.Visible || !p.Enabled {
			n.Blur()
		}
		if p.Visible {
			msgSetRect(n.clip, sel("setFrame:"), NSRect{Origin: NSPoint{p.Clip.X, p.Clip.Y}, Size: NSSize{p.Clip.W, p.Clip.H}})
			msgSetRect(n.view, sel("setFrame:"), NSRect{Origin: NSPoint{p.Bounds.X - p.Clip.X, p.Bounds.Y - p.Clip.Y}, Size: NSSize{p.Bounds.W, p.Bounds.H}})
		}
		n.setEnabled(p.Enabled)
		send(n.clip, "setHidden:", boolArg(!p.Visible))
	})
}

func (n *nativeView) setEnabled(enabled bool) {
	if enabled {
		for v, was := range n.disabled {
			send(v, "setEnabled:", boolArg(was))
			release(v)
			delete(n.disabled, v)
		}
		return
	}
	if n.disabled == nil {
		n.disabled = map[id]bool{}
	}
	var walk func(id)
	walk = func(v id) {
		if respondsTo(v, "setEnabled:") && respondsTo(v, "isEnabled") {
			if _, kept := n.disabled[v]; !kept {
				n.disabled[retain(v)] = sendBool(v, "isEnabled")
			}
			send(v, "setEnabled:", 0)
		}
		kids := send(v, "subviews")
		for i, count := 0, sendInt(kids, "count"); i < count; i++ {
			walk(send(kids, "objectAtIndex:", uintptr(i)))
		}
	}
	if n.view != 0 {
		walk(n.view)
	}
}

func (n *nativeView) hasFocus() bool {
	r := send(n.s.w.win, "firstResponder")
	return r != 0 && sendBool(r, "isKindOfClass:", uintptr(class("NSView"))) && sendBool(r, "isDescendantOf:", uintptr(n.clip))
}

func (n *nativeView) Focus(backward bool) {
	if n.closed || !n.p.Visible || !n.p.Enabled || n.hasFocus() {
		return
	}
	send(n.s.w.win, "makeFirstResponder:", uintptr(n.view))
}

func (n *nativeView) Blur() {
	if !n.s.w.closed && n.hasFocus() {
		send(n.s.w.win, "makeFirstResponder:", uintptr(n.s.view))
	}
}

func (n *nativeView) Close() {
	if n.closed {
		return
	}
	n.closed = true
	n.h.Closing()
	withPool(func() {
		n.Blur()
		for v := range n.disabled {
			release(v)
		}
		n.disabled = nil
		send(n.view, "removeFromSuperview")
		release(n.view)
		send(n.clip, "removeFromSuperview")
		delete(hostedViews, n.clip)
		release(n.clip)
	})
	for i, v := range n.s.nativeViews {
		if v == n {
			n.s.nativeViews = append(n.s.nativeViews[:i:i], n.s.nativeViews[i+1:]...)
			break
		}
	}
}

func (n *nativeView) Command(command string) bool {
	if n.closed || !n.p.Enabled || !n.hasFocus() {
		return false
	}
	selector := map[string]string{"copy": "copy:", "cut": "cut:", "paste": "paste:", "selectAll": "selectAll:", "delete": "delete:", "undo": "undo:", "redo": "redo:"}[command]
	if selector == "" {
		return false
	}
	return sendBool(n.s.w.b.app, "sendAction:to:from:", uintptr(sel(selector)), 0, uintptr(n.clip))
}

func (s *surface) closeNativeViews() {
	for len(s.nativeViews) > 0 {
		i := len(s.nativeViews) - 1
		n := s.nativeViews[i]
		s.nativeViews = s.nativeViews[:i]
		n.Close()
	}
}

func (s *surface) focusedNativeView() *nativeView {
	for _, n := range s.nativeViews {
		if !n.closed && n.p.Visible && n.hasFocus() {
			return n
		}
	}
	return nil
}

// nativeWindowMethods route standard Tab and native first-responder
// changes without allocating callbacks for individual views.
func nativeWindowMethods() []objc.MethodDef {
	return []objc.MethodDef{
		method("makeFirstResponder:", func(self id, cmd objc.SEL, responder id) bool {
			ok := byte(sendSuper(self, "MyGoWindow", cmd, uintptr(responder))) != 0
			if w := theBackend.byNSWindow[self]; ok && w != nil && w.surface != nil {
				if n := w.surface.focusedNativeView(); n != nil {
					n.h.Focused()
				}
			}
			return ok
		}),
		method("sendEvent:", func(self id, cmd objc.SEL, ev id) {
			if w := theBackend.byNSWindow[self]; w != nil && w.surface != nil {
				if n := w.surface.focusedNativeView(); n != nil && !n.p.ManagesTab && sendInt(ev, "type") == 10 && sendInt(ev, "keyCode") == 48 {
					mods := eventMods(ev)
					if mods&^platform.ModShift == 0 {
						n.h.Traverse(mods&platform.ModShift != 0)
						return
					}
				}
			}
			sendSuper(self, "MyGoWindow", cmd, uintptr(ev))
		}),
	}
}
