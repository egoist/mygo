//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// A native view in a window of native UI (platform.NativeViewSurface) is
// the app's own NSView, which the window's content shows in the box of the
// element of the content that holds it: it sits in a clip view of this
// package's, a subview of the window's content view under the surface,
// like the clip view of a web view (webview.go). The surface's frames are
// transparent where the content shows it, so what the content paints after
// it shows over it, and its hitTest: lets the pointer through to it there,
// except where the content painted over it (NativeViewPlacement.Covers).
//
// Unlike the clip view of a web view, the clip view of a native view does
// not hide its contents from assistive technology: the app's controls are
// the window's, and are found where they show.
//
// The clip view is flipped, and the app's view sits in it at its frame's
// offset from the clip: the clip changing size as the content scrolls
// moves the view without resizing it, which would re-lay-out what it
// draws (a text view reflowing its lines, for one), and the clip's own
// origin keeps the view's internal offset from the content's.

// A surface that shows native views: the core reaches these three through
// the provider this file exports (NativeViewSurface), which a window of
// native UI pays for only when it shows one (nativeview.go).
var _ platform.NativeViewSurface = (*nativeViewSurface)(nil)

// nativeViewSurface is a surface as platform.NativeViewSurface: the
// opt-in provider of this backend's native-view hosting. The surface
// itself does not implement the interface, so that an app that makes no
// native view carries neither these methods nor the code they name
// (AGENTS.md: apps pay only for what they use).
type nativeViewSurface struct {
	s *surface
}

// NativeViewSurface returns s as this backend's native-view hosting
// surface, when s is one of this backend's surfaces: the provider the
// core's native-view constructor hosts through. A surface of another
// backend, or none, reports that it cannot show native views, and the
// code of this file is not reachable for the apps that make none.
func NativeViewSurface(s platform.Surface) (platform.NativeViewSurface, bool) {
	ss, ok := s.(*surface)
	if !ok || ss == nil {
		return nil, false
	}
	return &nativeViewSurface{s: ss}, true
}

// errWindowGone is returned for a native view the window cannot host
// because it closed: before the factory ran, or while it did.
var errWindowGone = errors.New("mygo: darwin: the window of the native view is gone")

// nativeViewClass registers the class of the clip view a native view goes
// in, once, when the first one is made: a window of native UI that shows
// no native view never registers it, and pays nothing for it.
//
// The clip view is a sibling of the surface view in the window's content
// view (NewNativeView adds it to that view, below the surface), and unlike
// MyGoClipView it deliberately overrides no accessibility method: a plain
// NSView exposes its subviews as its accessibility children, so assistive
// technology reaches the app's controls through the window's own tree. The
// surface view's override of accessibilityChildren returns the content's
// elements and applies to the surface view alone, so it hides nothing
// beside it. A clip view the surface view held would hide them, which is
// why it is not added there.
var nativeViewClass sync.Once

func registerNativeViewClass() {
	nativeViewClass.Do(func() {
		classDef("MyGoNativeClipView", "NSView", nil, []objc.MethodDef{
			method("isFlipped", func(self id, _ objc.SEL) bool { return true }),
		})
	})
}

// placedNative is a native view of the window, as this file's type
// implements it: the clip view the window stacks under the surface, where
// the view goes in it, and the state placement asks of it. The placement
// and hit-test code of the extension (nativePlacement) uses this interface,
// never the concrete type, and an app that makes no native view links none
// of it (AGENTS.md: apps pay only for what they use).
type placedNative interface {
	platform.NativeView // NativeHandle, Focus, Destroy
	// owner is the window the view belongs to.
	owner() *window
	// clipID is the view's clip view, a subview of the window's content
	// view under the surface.
	clipID() id
	// place shows the view where the content's last frame puts it: the
	// clip where the frame clips it, and the app's view in the clip at
	// its frame's offset.
	place(c, f platform.RectF, height float64)
	// hasKeyboard reports whether the view, or a control in it, is the
	// first responder of its window.
	hasKeyboard() bool
	// isHidden reports whether the view's clip view is hidden.
	isHidden() bool
	// hide hides the view's clip view, which keeps the view's state.
	hide()
	// alive reports whether the view is not destroyed.
	alive() bool
}

// nativeView is the platform side of a native view: the app's own view
// (adapter), which the window shows in the clip view clip.
type nativeView struct {
	host    *window
	surface *surface
	adapter platform.NativeView
	clip    id
	handle  uintptr
	closed  bool
}

// The extension's native view is what the placement code of this file
// places: the extension holds it through this interface, never through this
// type. The assertion keeps the two in step: a missing method would
// otherwise fail the runtime assertions silently, and the view would not
// show.
var _ placedNative = (*nativeView)(nil)

// NativeHandle returns the app's view, zero once it is destroyed.
func (v *nativeView) NativeHandle() uintptr {
	if v.closed {
		return 0
	}
	return v.handle
}

// Focus gives the app's view the keyboard, as Tab does: the app's own view
// takes it, at its first control, or its last when back.
func (v *nativeView) Focus(back bool) {
	if v.closed {
		return
	}
	v.adapter.Focus(back)
}

// The extension reaches and holds a native view through the placedNative
// interface, never through this type.

// owner is the window the view belongs to.
func (v *nativeView) owner() *window { return v.host }

// clipID is the view's clip view, a subview of the window's content view
// under the surface.
func (v *nativeView) clipID() id { return v.clip }

// isHidden reports whether the view's clip view is hidden.
func (v *nativeView) isHidden() bool { return v.clip != 0 && sendBool(v.clip, "isHidden") }

// hide hides the view's clip view, which keeps the view's state.
func (v *nativeView) hide() {
	if v.clip != 0 {
		send(v.clip, "setHidden:", 1)
	}
}

// alive reports whether the view is not destroyed.
func (v *nativeView) alive() bool { return !v.closed }

// Destroy takes the app's view out of the window and destroys it: the
// platform call of mygo.NativeView.Destroy. The view is detached before
// the app destroys it, so that nothing in the window sends it messages
// once it is gone, and the app's Destroy runs exactly once.
func (v *nativeView) Destroy() {
	if v.closed {
		return
	}
	host, s := v.host, v.surface
	// Whether the app's view holds the keyboard is asked before the view
	// is destroyed (hasKeyboard of a destroyed view is false).
	keyboard := v.hasKeyboard()
	v.closed = true
	// The content no longer shows it, and the extension no longer holds
	// it.
	if p := s.extOf(); p != nil {
		p.placed = slices.DeleteFunc(p.placed, func(x platform.NativeViewPlacement) bool {
			return x.View == platform.NativeView(v)
		})
		p.views = slices.DeleteFunc(p.views, func(x placedNative) bool { return x == v })
	}
	withPool(func() {
		if keyboard && host != nil && !host.closed && s != nil && s.view != 0 {
			// A view that goes keeps no keyboard: it goes back to the
			// content.
			send(host.win, "makeFirstResponder:", uintptr(s.view))
		}
		send(id(v.handle), "removeFromSuperview")
		if v.adapter != nil {
			v.adapter.Destroy()
		}
		send(v.clip, "removeFromSuperview")
		release(v.clip)
		v.clip, v.handle = 0, 0
	})
}

// place shows the app's view where the content's last frame puts it: the
// clip view where the frame clips it, and the view in the clip at its
// frame's offset.
func (v *nativeView) place(c, f platform.RectF, height float64) {
	msgSetRect(v.clip, sel("setFrame:"), clipFrame(c, height))
	msgSetRect(id(v.handle), sel("setFrame:"), childFrame(f, c))
	if sendBool(v.clip, "isHidden") {
		send(v.clip, "setHidden:", 0)
	}
}

// hasKeyboard reports whether the app's view, or a control in it, is the
// first responder of its window: a view that is hidden or destroyed gives
// the keyboard back to the content, as one that is not the first responder
// itself holds it through a control of its own.
func (v *nativeView) hasKeyboard() bool {
	if v.closed || v.clip == 0 || v.host == nil || v.host.win == 0 {
		return false
	}
	return firstResponderIn(v.clip, v.host.win)
}

// firstResponderIn reports whether view, or a control in it, holds the
// keyboard of the window: a native view's controls are its subviews, so
// the first responder is rarely the view itself.
//
// A control the window edits text in does not hold the keyboard itself:
// the window edits it in the field editor, one NSTextView it shares among
// its text fields. AppKit usually moves the field editor into the control
// it edits, a descendant of view, which the ancestry finds, but it is not
// a subview of it while it is not editing there, or when the control is
// not a view of the window. The field editor's delegate is the control it
// edits, which is what says whose it is then; a field editor without a
// delegate, or with one that is not a view, is not claimed.
func firstResponderIn(view, win id) bool {
	r := send(win, "firstResponder")
	if r == 0 {
		return false
	}
	descendant := sendBool(r, "isKindOfClass:", uintptr(class("NSView"))) &&
		(r == view || sendBool(r, "isDescendantOf:", uintptr(view)))
	return responderBelongs(descendant, fieldEditorOf(r, view))
}

// fieldEditorOf reports whether r is the window's field editor editing a
// control of view: r is an NSTextView whose delegate, the control it
// edits, is view or a view in it.
func fieldEditorOf(r, view id) bool {
	if !sendBool(r, "isKindOfClass:", uintptr(class("NSTextView"))) {
		return false
	}
	d := send(r, "delegate")
	return d != 0 && sendBool(d, "isKindOfClass:", uintptr(class("NSView"))) &&
		(d == view || sendBool(d, "isDescendantOf:", uintptr(view)))
}

// responderBelongs is the decision behind firstResponderIn, from what it
// learned of the first responder: whether it is view or a view in it, and
// whether it is the window's field editor editing a control in view.
// Ancestry alone is not proof for a field editor, which the window shares:
// only its delegate says whose text it holds.
func responderBelongs(inView, fieldEditorOfView bool) bool {
	return inView || fieldEditorOfView
}

// clipFrame is where a clip view goes in the window's content view, which
// is not flipped: the content's placements, and its height, are in
// top-left DIPs.
func clipFrame(c platform.RectF, height float64) NSRect {
	return NSRect{
		Origin: NSPoint{X: c.X, Y: height - c.Y - c.H},
		Size:   NSSize{Width: c.W, Height: c.H},
	}
}

// childFrame is the app's view in its clip view, which is flipped: its
// frame offset from the clip, whose size is the clip's, so that a clip
// that shrinks with the content scrolling moves the view without resizing
// it.
func childFrame(f, c platform.RectF) NSRect {
	return NSRect{
		Origin: NSPoint{X: f.X - c.X, Y: f.Y - c.Y},
		Size:   NSSize{Width: f.W, Height: f.H},
	}
}

// nativePlacement is the native-view extension of a surface: the native
// views the window shows, the placements of the content's last frame, and
// the placement, hit-testing and stack code they share with the web views
// of the window. The surface holds it as surfacePlacement, from the first
// native view it makes on (placement), so that its own type, the window and
// the placement code of webview.go name no native type, and an app that
// makes no native view links none of this (AGENTS.md: apps pay only for
// what they use).
type nativePlacement struct {
	s *surface
	// placed are where the content's last frame shows the native views.
	placed []platform.NativeViewPlacement
	// views are the native views of the window, which they are destroyed
	// with when it closes and which TakeFocus looks through.
	views []placedNative
}

// extOf returns the native-view extension of the surface, or nil where it
// made no native view.
func (s *surface) extOf() *nativePlacement {
	if s == nil {
		return nil
	}
	p, _ := s.ext.(*nativePlacement)
	return p
}

// placement returns the native-view extension of the surface, making it on
// the first native view the surface shows: the surface holds it from then
// on, it installs the two hooks the default path reaches it through, and
// the placement code of this file runs only for a window that shows one.
func (s *surface) placement() *nativePlacement {
	if p := s.extOf(); p != nil {
		return p
	}
	p := &nativePlacement{s: s}
	s.ext = p
	s.placeNative = p.placeViews
	s.hitNative = p.pointer
	return p
}

// placeViews shows the window's web views and native views where the
// content's last frame shows them, in its paint order under the surface,
// and hides the others, which keep their state (webview.go, this file). It
// is the mixed path PlaceWebViews delegates to while the surface shows a
// native view.
func (p *nativePlacement) placeViews() {
	s, host := p.s, p.s.w
	height := msgRect(host.view, sel("bounds")).Size.Height
	shownNative := make([]placedNative, 0, len(p.placed))
	withPool(func() {
		shownWeb := p.placeWebViews()
		for i := range p.placed {
			v, ok := p.placed[i].View.(placedNative)
			if !ok || v.owner() != host || !v.alive() {
				continue
			}
			v.place(p.placed[i].Clip, p.placed[i].Frame, height)
			if !slices.Contains(shownNative, v) {
				shownNative = append(shownNative, v)
			}
		}
		for _, v := range p.views {
			if !slices.Contains(shownNative, v) && !v.isHidden() {
				if v.hasKeyboard() {
					// A hidden native view keeps no keyboard either: it
					// goes back to the content.
					send(host.win, "makeFirstResponder:", uintptr(s.view))
				}
				v.hide()
			}
		}
		p.stackViews(shownClips(s.placed, p.placed, shownWeb, shownNative))
	})
}

// placeWebViews is the web half of placeViews: it shows the window's web
// views of the content's last frame where it shows them, hides the ones it
// does not show, which keep their state, and returns the views it shows. It
// repeats the web-only placement of PlaceWebViews (webview.go) rather than
// sharing it: the one helper both paths would call is linked into every app
// that shows a web view, while this copy is reached from a native view
// alone (AGENTS.md: apps pay only for what they use). It runs in a pool.
func (p *nativePlacement) placeWebViews() []*window {
	s, host := p.s, p.s.w
	height := msgRect(host.view, sel("bounds")).Size.Height
	shown := make([]*window, 0, len(s.placed))
	for i := range s.placed {
		q := &s.placed[i]
		v, ok := q.WebView.(*window)
		if !ok || v.host != host || v.closed {
			continue
		}
		msgSetRect(v.clip, sel("setFrame:"), clipFrame(q.Clip, height))
		// The clip view is flipped.
		msgSetRect(v.web, sel("setFrame:"), childFrame(q.Frame, q.Clip))
		if sendBool(v.clip, "isHidden") {
			send(v.clip, "setHidden:", 0)
		}
		shown = append(shown, v)
	}
	for _, v := range host.webViews {
		if !slices.Contains(shown, v) && !sendBool(v.clip, "isHidden") {
			if v.hasKeyboard() {
				// A hidden view keeps no keyboard: it goes back to the
				// content.
				send(host.win, "makeFirstResponder:", uintptr(s.view))
			}
			send(v.clip, "setHidden:", 1)
		}
	}
	return shown
}

// pointer returns the web view that takes the pointer at x, y, and whether
// a view of the surface takes it there. It is the hit test the surface
// leaves the pointer to (pointerTaken, webview.go).
func (p *nativePlacement) pointer(x, y float64) (*window, bool) {
	web, native := p.top(x, y)
	return web, web != nil || native != nil
}

// top returns what takes the pointer at x, y in the surface: the web view
// or the native view the content painted last among those whose clip holds
// the point, or neither where none does or what it painted over the last
// one takes the pointer there (Placement.Covers), which is the surface's to
// route.
func (p *nativePlacement) top(x, y float64) (*window, placedNative) {
	s := p.s
	order, web, native := -1, (*window)(nil), placedNative(nil)
	for i := range s.placed {
		q := &s.placed[i]
		if q.Order <= order || !q.Clip.Contains(x, y) {
			continue
		}
		v, ok := q.WebView.(*window)
		if !ok || v.closed {
			continue
		}
		order = q.Order
		if q.At(x, y) {
			web, native = v, nil
		} else {
			web, native = nil, nil
		}
	}
	for i := range p.placed {
		q := &p.placed[i]
		if q.Order <= order || !q.Clip.Contains(x, y) {
			continue
		}
		v, ok := q.View.(placedNative)
		if !ok || !v.alive() {
			continue
		}
		order = q.Order
		if q.At(x, y) {
			web, native = nil, v
		} else {
			web, native = nil, nil
		}
	}
	return web, native
}

// stackViews puts the clip views of the views shown, web and native, in the
// order the content painted them, under the surface, unless they are.
func (p *nativePlacement) stackViews(clips []id) {
	if len(clips) < 2 {
		return
	}
	host := p.s.w
	subviews := send(host.view, "subviews")
	n := sendInt(subviews, "count")
	at := 0
	for i := range n {
		if at < len(clips) && send(subviews, "objectAtIndex:", uintptr(i)) == clips[at] {
			at++
		}
	}
	if at == len(clips) {
		return
	}
	// Moving a view out of its superview takes the keyboard from it: the
	// first responder goes back to it.
	first := send(host.win, "firstResponder")
	for _, clip := range clips {
		retain(clip)
		send(clip, "removeFromSuperview")
		send(host.view, "addSubview:positioned:relativeTo:", uintptr(clip), nsWindowBelow, uintptr(p.s.view))
		release(clip)
	}
	if first != 0 && first != send(host.win, "firstResponder") {
		send(host.win, "makeFirstResponder:", uintptr(first))
	}
}

// shownClips returns the clip views of the web views web and the native
// views native, which the content's last frame shows, in the order it
// painted them: the web views and the native views of a frame are numbered
// by one counter (WebViewPlacement.Order, NativeViewPlacement.Order), so
// each of the two lists is ordered by itself, and one pass here merges
// them, which is what sorting the two joined would give: of two views the
// content painted alike, the web view comes first, as it does in the list
// the content hands over. Entries the frame does not show are skipped and
// take no room, and the only slice made is the result.
func shownClips(webPlaced []platform.WebViewPlacement, nativePlaced []platform.NativeViewPlacement, web []*window, native []placedNative) []id {
	ordered := make([]id, 0, len(webPlaced)+len(nativePlaced))
	i, j := 0, 0
	for i < len(webPlaced) || j < len(nativePlaced) {
		for i < len(webPlaced) {
			if v, ok := webPlaced[i].WebView.(*window); ok && slices.Contains(web, v) {
				break
			}
			i++
		}
		for j < len(nativePlaced) {
			if v, ok := nativePlaced[j].View.(placedNative); ok && slices.Contains(native, v) {
				break
			}
			j++
		}
		if i == len(webPlaced) && j == len(nativePlaced) {
			break
		}
		// j == len means a web view is left, since both cannot run out
		// here; the web view wins a tie.
		if j == len(nativePlaced) || (i < len(webPlaced) && webPlaced[i].Order <= nativePlaced[j].Order) {
			ordered = append(ordered, webPlaced[i].WebView.(*window).clip)
			i++
			continue
		}
		ordered = append(ordered, nativePlaced[j].View.(placedNative).clipID())
		j++
	}
	return ordered
}

// closeViews destroys the native views of a window that closes, as
// closeWebViews closes its web views. It is the hook the window holds and
// reaches without naming this file's code, installed when the first native
// view was made (NewNativeView, window.go).
func (p *nativePlacement) closeViews() {
	for len(p.views) > 0 {
		p.views[len(p.views)-1].Destroy()
	}
}

// NewNativeView makes the platform side of a native view: it calls make
// with the host of the view, whose Parent is the clip view the app's view
// goes in, and returns the view it made, attached to the window. It is the
// platform call of platform.NativeViewSurface, on the provider the core
// hosts through (NativeViewSurface).
func (p *nativeViewSurface) NewNativeView(make func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	s := p.s
	host := s.w
	if host.closed || host.view == 0 || s.view == 0 {
		return nil, errWindowGone
	}
	// This is the first native view of the surface, or it is not: the
	// extension of the surface holds the view, the placements the content
	// hands over, and the code that places and stacks them
	// (nativePlacement). The window's teardown destroys its native views
	// through the hook the extension installs, which the window reaches
	// without naming this file's code, so a window that shows none pays a
	// nil check (window.go).
	ext := s.placement()
	if host.closeNativeViews == nil {
		host.closeNativeViews = ext.closeViews
	}
	registerNativeViewClass()
	var (
		clip    id
		adapter platform.NativeView
		v       *nativeView
		err     error
	)
	withPool(func() {
		clip = msgInitRect(send(class("MyGoNativeClipView"), "alloc"), sel("initWithFrame:"), NSRect{})
		send(clip, "setWantsLayer:", 1)
		// The clip view clips the view to its own box, square: the radius
		// and the transform of the element are the app's view's.
		send(send(clip, "layer"), "setMasksToBounds:", 1)
		send(clip, "setAutoresizingMask:", nsViewMinYMargin)
		send(clip, "setHidden:", 1)
		send(host.view, "addSubview:positioned:relativeTo:", uintptr(clip), nsWindowBelow, uintptr(s.view))
		// The app's view goes in the clip view, and is under the surface
		// from here on: a factory that fails after showing something is
		// given the parent it made it in, and mygo destroys what it
		// returned, never what it did not.
		adapter, err = make(platform.NativeViewHost{Platform: "darwin", Parent: uintptr(clip), Scale: s.scale()})
		if err != nil {
			closeClip(clip)
			return
		}
		// The factory is the app's code: it may have closed the window,
		// or the app may have destroyed the view it makes for, while it
		// ran, and the window's teardown ran then, before this view was
		// registered with it. Nothing below touches the clip view or the
		// view the factory made from there on: the clip is taken down
		// here, and mygo destroys what the factory returned, exactly
		// once.
		if host.closed {
			closeClip(clip)
			err = errWindowGone
			return
		}
		if adapter == nil {
			closeClip(clip)
			err = errors.New("mygo: darwin: the native view's factory made none")
			return
		}
		handle := adapter.NativeHandle()
		if handle == 0 {
			// mygo destroys it: a view without an NSView shows nothing.
			closeClip(clip)
			err = errors.New("mygo: darwin: the native view's factory made no NSView")
			return
		}
		// NativeHandle is the app's code as well.
		if host.closed {
			closeClip(clip)
			err = errWindowGone
			return
		}
		if err = attachNativeView(handle, clip); err != nil {
			closeClip(clip)
			return
		}
		v = &nativeView{host: host, surface: s, adapter: adapter, clip: clip, handle: handle}
	})
	if err != nil {
		return nil, err
	}
	ext.views = append(ext.views, v)
	return platform.NativeView(v), nil
}

// attachNativeView puts the app's view in the clip view its factory was
// given, and reports a view the window cannot host: one whose handle is
// not an NSView, or one that is already another view's, or in a window of
// its own, which mygo never takes from it.
func attachNativeView(handle uintptr, clip id) error {
	view := id(handle)
	if !sendBool(view, "isKindOfClass:", uintptr(class("NSView"))) {
		return fmt.Errorf("mygo: darwin: the handle %#x of the native view is not an NSView", handle)
	}
	switch parent := send(view, "superview"); {
	case parent == clip:
		// The factory put the view in the parent it was given.
	case parent != 0:
		return fmt.Errorf("mygo: darwin: the native view %#x is already a subview of another view; mygo does not take a view from another parent", handle)
	case send(view, "window") != 0:
		return fmt.Errorf("mygo: darwin: the native view %#x is already in the view hierarchy of a window; mygo does not take a view from another window", handle)
	default:
		send(clip, "addSubview:", handle)
	}
	return nil
}

// closeClip takes the clip view of a native view that failed to be made
// out of the window and releases it, detaching whatever the factory put in
// it first: mygo is about to destroy the view the factory returned, and
// the clip view it made it in is gone by then.
func closeClip(clip id) {
	subviews := send(clip, "subviews")
	for i := sendInt(subviews, "count") - 1; i >= 0; i-- {
		send(send(subviews, "objectAtIndex:", uintptr(i)), "removeFromSuperview")
	}
	send(clip, "removeFromSuperview")
	release(clip)
}

// PlaceNativeViews shows the window's native views where the content shows
// them, in its paint order among the web views, under the surface, and
// hides the others, which keep their state. It is the platform call of
// platform.NativeViewSurface, on the provider the core hosts through
// (NativeViewSurface).
func (p *nativeViewSurface) PlaceNativeViews(views []platform.NativeViewPlacement) {
	s := p.s
	if s.w.closed {
		return
	}
	ext := s.placement()
	ext.placed = append(ext.placed[:0], views...)
	ext.placeViews()
}

// TakeFocus gives the content the keyboard back from a native view, as Tab
// leaving its last control does. It is the platform call of
// platform.NativeViewSurface, on the provider the core hosts through
// (NativeViewSurface): a view that holds the keyboard keeps it, so that
// nothing here takes it from a page, or from another window.
func (p *nativeViewSurface) TakeFocus() {
	s := p.s
	host := s.w
	if host.closed || host.win == 0 || s.view == 0 {
		return
	}
	withPool(func() {
		ext := s.extOf()
		if ext == nil {
			return
		}
		for _, v := range ext.views {
			if v.hasKeyboard() {
				send(host.win, "makeFirstResponder:", uintptr(s.view))
				return
			}
		}
	})
}
