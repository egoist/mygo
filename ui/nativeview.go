package ui

import (
	"log"
	"slices"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// HostedView is a window-owned platform view, implemented by
// *mygo.NativeView. Its native hooks are connected only in a real window
// (or the fake backend); a Tester lays out an empty box without native calls.
type HostedView = surface.HostedView

// HostView places a mygo.NativeView inside a layout. Give it an explicit
// size or Grow: platform controls have no intrinsic MyGo size. Key the
// element when siblings can move. It is one tab stop by default.
//
//	ui.Row(c).Fill().Children(func() {
//		ui.Button(c, "Refresh")
//		ui.HostView(c, app.control).Key("control").Grow(1).Height(40)
//	})
//
// MyGo clips the native view to rectangular ancestor clips. The native
// view draws above the scene; if a later element overlaps it, MyGo hides
// the whole view until the overlap goes away, so dialogs and popovers can
// receive input. Rounded masks, opacity, effects and transforms do not
// apply to native views. Omitting the element hides, but does not dispose,
// the window-owned control. Window captures contain only MyGo's scene.
func HostView(c *Context, v HostedView) *Element {
	e := Box(c).AlignSelf(Stretch).Role(RoleGroup)
	e.hostView = v
	if v != nil {
		e.Focusable()
	}
	return e
}

func (rt *engine) nativeConn() *surface.Conn {
	if h, ok := rt.host.(*windowHost); ok {
		return h.conn
	}
	return nil
}

// placeNativeViews follows layout, clipping, visibility and paint order.
func (rt *engine) placeNativeViews() {
	conn := rt.nativeConn()
	if conn == nil {
		return
	}
	if len(rt.hosted) == 0 && len(rt.shownHosts) == 0 {
		return
	}
	if rt.shownHosts == nil {
		rt.shownHosts = map[HostedView]uint64{}
	}
	seen := make(map[HostedView]bool, len(rt.hosted))
	for _, e := range rt.hosted {
		v, s := e.hostView, e.st
		if seen[v] {
			if rt.strict {
				panic("ui: a native view may be placed only once per frame")
			}
			log.Print("ui: a native view may be placed only once per frame")
			e.nativeHandle = 0
			continue
		}
		seen[v] = true
		clip := Rect{s.vx, s.vy, s.vw, s.vh}
		visible := clip.W > 0 && clip.H > 0 && (rt.modal == 0 || s.scope == rt.modal)
		// Native views have their own drawing plane. A later MyGo element
		// intersecting it must keep both its pixels and its input.
		for i := e.hostHit + 1; visible && i < len(rt.hits); i++ {
			h := rt.hits[i]
			if r := intersect(clip, h.r); r.W > 0 && r.H > 0 {
				visible = false
			}
		}
		p := platform.NativeViewPlacement{
			ID: e.id, Bounds: platform.RectF{X: float64(e.x), Y: float64(e.y), W: float64(e.w), H: float64(e.h)},
			Clip:    platform.RectF{X: float64(clip.X), Y: float64(clip.Y), W: float64(clip.W), H: float64(clip.H)},
			Visible: visible, Enabled: !e.IsDisabled(),
		}
		e.nativeHandle = v.PlaceNativeView(conn, p)
		rt.shownHosts[v] = e.id
		if e.nativeHandle == 0 && clip.W > 0 && clip.H > 0 {
			// A hidden, destroyed or misplaced view receives no focus.
			if i := slices.Index(rt.focusOrder, e.id); i >= 0 {
				rt.focusOrder = slices.Delete(rt.focusOrder, i, i+1)
				rt.focusScopes = slices.Delete(rt.focusScopes, i, i+1)
			}
			if rt.focused == e.id {
				rt.focused = 0
			}
		}
	}
	for v := range rt.shownHosts {
		if !seen[v] {
			v.PlaceNativeView(conn, platform.NativeViewPlacement{})
			delete(rt.shownHosts, v)
		}
	}
}

func (rt *engine) syncNativeFocus() {
	if rt.inFrame {
		return
	}
	conn := rt.nativeConn()
	if conn == nil {
		return
	}
	var target HostedView
	for v, id := range rt.shownHosts {
		if id == rt.focused {
			target = v
			break
		}
	}
	if target == rt.nativeFocused {
		return
	}
	old := rt.nativeFocused
	rt.nativeFocused = target
	if old != nil {
		old.FocusNativeView(conn, false, false)
	}
	if target != nil {
		target.FocusNativeView(conn, true, rt.nativeBackward)
	}
	rt.nativeBackward = false
}
