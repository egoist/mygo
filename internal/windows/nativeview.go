//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"slices"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// A native view in a window of native UI (platform.NativeViewSurface) is a
// control the app makes with its own toolkit, which the surface hosts in
// windows of its own:
//
//   - the clip window, a child of the surface's window, is only as big as the
//     part of the view the content shows (NativeViewPlacement.Clip). The app's
//     control is its child, at the control's offset from the clip and sized to
//     the view's frame, so that a clip that moves as the content scrolls moves
//     the control without resizing it: what it draws keeps its own layout (an
//     edit control its text, caret and selection). The region of the clip
//     window leaves out the boxes where a web view the content painted after
//     the view shows, so the page shows there and takes the pointer there
//     (cutNativeClip).
//   - the cover window, above every clip window, takes the pointer where the
//     content painted over a view (NativeViewPlacement.Covers). It paints
//     nothing (WS_EX_TRANSPARENT, and its window procedure draws nothing) and
//     its window region is the covers, so what the control shows under a
//     translucent popup stays, and the pointer over those boxes is the
//     content's: the cover sends the mouse messages on to the surface, which
//     routes them as the pointer over the content (its web views included).
//     Without it the app's control would take every press on a popup over it.
//
// What the content paints over a native view is painted by its frames, which
// are a DirectComposition target on top of the surface's children
// (compositor.go). The surface therefore composes while it hosts a native view
// (viaComposition), or a popup would show under the control.
//
// The app's control takes the pointer where it shows, and with it the
// keyboard; the app reports both through mygo.NativeViewHost (Focused, TabOut,
// PointerDown), which the window's content hears as the view's own events.
// Nothing here subclasses the app's control, and its IME stays its own.
//
// One thing this cannot do: show a web view over a native view the content
// painted before it. Web views are a non-topmost DirectComposition target,
// under every child window of the surface, so a child window cannot be covered
// by one. What the view shows is its window region instead: the boxes of the
// clip where a web view the content painted after the view shows are left out
// of the region (clipHoles), and the page, which the surface's frames leave
// transparent there, shows through them. The pixels and the pointer agree over
// the whole clip: where the region leaves the app's control, the page shows and
// takes the pointer, as the content painted it. The boxes are recomputed
// whenever the native views are laid out, and whenever the web views are placed
// (PlaceWebViews), since the boxes and the paint order both decide.
//
// What that cannot do is blend. The boxes a page shows through are the surface,
// not the pixels of the app's control under them, so a page that is not opaque
// (a translucent or blurring page over a native view) shows what is behind the
// view rather than the control's own pixels: native transparency and effects
// behind a hosted page are not composited, and are outside what mygo guarantees
// here. This is not general compositing; opaque web content over a native view
// is exact in both pixels and input. Interleaving the two by composition needs
// a composition target per native view, or one surface per view.

// A surface that shows native views: the core reaches this file through
// NativeViewSurface, the free function below, and a window of native UI pays
// for it only when it shows one (nativeview.go, in the root package). The
// surface itself does not implement platform.NativeViewSurface: an app that
// shows no native view carries no method of the interface, and none of the
// code behind those methods (AGENTS.md: apps pay only for what they use).
var _ platform.NativeViewSurface = (*nativeViewSurface)(nil)

// nativeViewSurface is the native-view hosting of one surface: its clip
// windows and its cover window, and the placement the content last gave its
// views. It is this backend's opt-in provider (NativeViewSurface), and the
// always-linked surface names none of it: a window that shows no native view
// has no provider, and calls none of this file through the hooks the provider
// installs (surface.go, newNativeViewSurface).
type nativeViewSurface struct {
	// s is the surface the hosting belongs to: its window, its DPI and its
	// web views are what a placement is laid out against.
	s *surface

	// native holds the native views the surface hosts, and placedNative the
	// placement the content last gave them; stackedNative are those shown, in
	// the order the content painted them. cover is the window that takes the
	// pointer where the content painted over a native view, with coverKey the
	// boxes its region was built from, to tell whether it must be built again,
	// and coverRegion whether it has a region at all (SetWindowRgn with none
	// gives a window its whole area back).
	native        []*nativeView
	placedNative  []platform.NativeViewPlacement
	stackedNative []*nativeView
	cover         uintptr
	coverKey      []pixelRect
	coverRegion   bool
}

// NativeViewSurface returns the native-view hosting of a surface of this
// backend, and whether the surface is one of this backend's own: the free
// function the core's native-view constructor reaches this backend through,
// so that an app that shows no native view does not carry the code behind it.
// A surface of another backend, or none, reports that it cannot show native
// views. The surface itself is not a platform.NativeViewSurface: hosting is
// opt-in, and every native view of the surface goes through the value this
// returns, which is the surface's own and is made once.
func NativeViewSurface(s platform.Surface) (platform.NativeViewSurface, bool) {
	surf, ok := s.(*surface)
	if !ok || surf == nil {
		return nil, false
	}
	if h, ok := surf.nativeHost.(*nativeViewSurface); ok {
		return h, true
	}
	h := newNativeViewSurface(surf)
	surf.nativeHost = h
	return h, true
}

// newNativeViewSurface makes the hosting of a surface, and installs the hooks
// the always-linked surface calls it through (surface.go): a surface that
// never asks for its native views has none of them, and calls none of this
// file's code.
func newNativeViewSurface(s *surface) *nativeViewSurface {
	h := &nativeViewSurface{s: s}
	s.nativeComposing = h.composing
	s.nativeEvent = h.event
	return h
}

// composing reports whether the surface hosts a native view right now: the
// frames of the content must then show over it, through DirectComposition
// (surface.viaComposition).
func (h *nativeViewSurface) composing() bool { return len(h.native) > 0 }

// event handles what the always-linked surface sends the hosting whose hooks
// it installed (surface.go, nativeEvent): the events of the surface's own
// window its native views take part in. One method for all of them, so that
// the surface carries one function value and not one per event.
func (h *nativeViewSurface) event(ev uint8) {
	switch ev {
	case nativeEvFit:
		h.fitNatives()
	case nativeEvPlaced:
		h.cutNativeClips()
	case nativeEvClose:
		h.closeNativeViews()
	case nativeEvReset:
		h.reset()
	}
}

// reset forgets the hosting of a surface whose window is gone (wmDestroy): the
// views themselves were given up before Windows destroyed the window
// (closeNativeViews, window.go, destroy), and the clip windows and the cover
// window went with it.
func (h *nativeViewSurface) reset() {
	h.native, h.placedNative, h.stackedNative = nil, nil, nil
	h.cover, h.coverKey, h.coverRegion = 0, nil, false
}

// surfaceHWND is the window of the surface the hosting belongs to: where the
// pointer over a cover is sent on, and the parent of the clip windows.
func (h *nativeViewSurface) surfaceHWND() uintptr { return h.s.hwnd }

const (
	// gwChild is GW_CHILD: the first child of a window, which tells a clip
	// window the app's control went in from one it did not.
	gwChild = 5
	// rgnOr and rgnDiff are the CombineRgn modes of the cover window's region.
	rgnOr   = 2
	rgnDiff = 4
)

// nativeHostClass is the class of the windows a native view goes in: the clip
// window of each view, and the surface's cover window. It is registered once,
// when the first native view is made: a window of native UI that shows no
// native view never registers it, and pays nothing for it.
//
// The windows of this class have messages of their own -- the paint and the
// pointer of a cover, the teardown of a clip window -- so the class has a
// window procedure of its own (nativeHostWndProc). Registering the app's
// procedure for them instead would put the lookup of the hosting in the path
// of every message of every window of every app (app.go, wndProc).
const nativeHostClass = "MyGoNativeHost"

var nativeHostClassRegistered bool

// nativeHostProc is the window procedure of the hosting's class, made once on
// the first native view (initNativeHostProc) and then registered for the class
// and kept for the rest of the process: syscall.NewCallback makes a procedure
// Windows may call for as long as the process lives, so one is made and never
// given up, whatever number of views a process shows over time (AGENTS.md:
// purego callbacks are created once, at startup, and routed by their own data).
var nativeHostProc uintptr

// initNativeHostProc returns the procedure of the hosting's class, making it on
// the first call. Main thread only.
func initNativeHostProc() uintptr {
	if nativeHostProc == 0 {
		nativeHostProc = syscall.NewCallback(nativeHostWndProc)
	}
	return nativeHostProc
}

// nativeHostWndProc handles the messages of the hosting's own windows: the clip
// window of a native view and the surface's cover window, both looked up by
// their handle (nativeHostWindows). It is registered for nativeHostClass and no
// other class, so no window of an app that shows no native view reaches this
// file. Main thread only.
func nativeHostWndProc(hwnd, m, wp, lp uintptr) uintptr {
	if h := nativeHostWindowOf(hwnd); h != nil {
		if r, ok := h.hostMessage(hwnd, uint32(m), wp, lp); ok {
			return r
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

func registerNativeHostClass() {
	if nativeHostClassRegistered {
		return
	}
	nativeHostClassRegistered = true
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		WndProc:   initNativeHostProc(),
		Instance:  instance(),
		Cursor:    cursor,
		ClassName: u16(nativeHostClass),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

// nativeHostWindows is where a window of this file that routes its own
// messages is looked up: the clip window of a native view, or the surface's
// cover window. It is nil until the first native view is made, and only the
// class's own procedure looks in it (nativeHostWndProc).
var nativeHostWindows map[uintptr]nativeHostWindow

// nativeHostWindow is a window of this file that handles its own messages: the
// clip window of a native view, or the surface's cover window.
type nativeHostWindow interface {
	hostMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool)
}

// nativeHostWindowOf returns the handler of a window of this file, nil for any
// other window. Main thread only.
func nativeHostWindowOf(hwnd uintptr) nativeHostWindow {
	if nativeHostWindows == nil {
		return nil
	}
	return nativeHostWindows[hwnd]
}

func registerNativeHostWindow(hwnd uintptr, h nativeHostWindow) {
	if nativeHostWindows == nil {
		nativeHostWindows = make(map[uintptr]nativeHostWindow)
	}
	nativeHostWindows[hwnd] = h
}

// wmNativeViewReap tells the clip window of a native view to destroy itself,
// once the call that gave it up returned: the app's own Destroy runs first,
// and only then does the window the app made its control in go.
const wmNativeViewReap = wmApp + 0x10

// nativeView is the platform side of a native view: the clip window the app's
// control goes in, and the placement the content last gave it.
type nativeView struct {
	s      *nativeViewSurface
	clip   uintptr
	handle uintptr
	closed bool
	// adapter is the view the app's factory made: the control, which is the
	// app's own and which mygo destroys once.
	adapter platform.NativeView
	// placed is whether the content shows the view where the last placement
	// put it, and shown whether the clip window is shown.
	placed bool
	shown  bool
	// frame, clipRect and order are the placement's: where the view goes, the
	// part of it that shows, and where the content painted it among the other
	// views of the frame. The covers of a placement are not kept here: the
	// cover window's region is built from the placements of the surface
	// (coverNative), which is where the content's paint order is known.
	frame    platform.RectF
	clipRect platform.RectF
	order    int
	// clipAt and childAt are the pixel rectangles the clip window and the
	// app's control were last put at, so that neither is moved or resized
	// twice, and a control is never resized where the content only moved it.
	clipAt, childAt pixelRect
	hasChild        bool
	// cut is the region the clip window last took, in its own pixels: the
	// boxes of the clip without the boxes a later web view shows through
	// (clipRegion, cutNativeClip). cutRegion tells whether the window has a
	// region at all, so that a window that never took one is not compared to
	// one that had its region taken away.
	cut       []pixelRect
	cutRegion bool
}

// NativeHandle returns the app's control, zero once it is destroyed.
func (v *nativeView) NativeHandle() uintptr {
	if v.closed {
		return 0
	}
	return v.handle
}

// Focus gives the app's control the keyboard, as Tab does: the app's own view
// takes it, at its first control, or its last when back.
func (v *nativeView) Focus(back bool) {
	if v.closed || v.adapter == nil {
		return
	}
	v.adapter.Focus(back)
}

// Destroy is the platform call of mygo.NativeView.Destroy: the app's control
// is detached from the window, the app destroys it (adapter.Destroy, exactly
// once, and before anything of the window destroys a control the app left),
// and the clip window it was in goes with it.
func (v *nativeView) Destroy() {
	if v.closed {
		return
	}
	s := v.s
	// Whether the app's control holds the keyboard is asked before it is
	// gone: hasKeyboard of a closed view is false.
	keyboard := s != nil && v.hasKeyboard()
	v.closed = true
	if s != nil {
		s.forgetNative(v)
	}
	if keyboard {
		s.giveBackKeyboard()
	}
	if v.adapter != nil {
		v.adapter.Destroy()
	}
	v.destroyClip()
}

// destroyClip gives up the clip window of a native view. The destroy is
// posted, not called: the message runs once the current call returned, after
// the app's Destroy above, so that a control the app's Destroy is about to
// destroy is not destroyed before it, with the clip window it is in.
func (v *nativeView) destroyClip() {
	clip := v.clip
	v.clip = 0
	if clip == 0 || !isWindow(clip) {
		return
	}
	postMessage(clip, wmNativeViewReap, 0, 0)
}

// hostMessage handles the messages of the clip window of a native view: it
// draws nothing (the app's control covers it, and the frames of the content
// cover them both), and destroys itself when it is given up.
func (v *nativeView) hostMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmEraseBkgnd:
		return 1, true
	case wmPaint:
		paintNothing(hwnd)
		return 0, true
	case wmNativeViewReap:
		procDestroyWindow.Call(hwnd)
		return 0, true
	case wmDestroy:
		v.clip = 0
		delete(nativeHostWindows, hwnd)
		return 0, true
	}
	return 0, false
}

// hasKeyboard reports whether the app's control, or a control in it, holds the
// keyboard of the window: a native view's controls are its children, so what
// takes the keyboard is rarely the clip window itself.
func (v *nativeView) hasKeyboard() bool {
	if v.closed || v.clip == 0 || !isWindow(v.clip) {
		return false
	}
	focus := getFocus()
	return focus != 0 && (focus == v.clip || isChild(v.clip, focus))
}

// nativeCover is the cover window of a surface: transparent (it paints
// nothing) and with a window region of the boxes the content painted over its
// native views, above the clip windows, so that the pointer over those boxes is
// the content's and not the app's control's.
type nativeCover struct {
	s *nativeViewSurface
	// tracking tells that the pointer over a cover is tracked, so that the
	// content hears it leave as it hears its own mouse leave.
	tracking bool
}

func (c *nativeCover) hostMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmEraseBkgnd:
		return 1, true
	case wmPaint:
		paintNothing(hwnd)
		return 0, true
	case wmSetCursor:
		// The cursor of the content under the pointer.
		r, _, _ := procSendMessageW.Call(c.s.surfaceHWND(), wmSetCursor, hwnd, lp)
		return r, true
	case wmMouseMove, wmLButtonDown, wmLButtonDblClk, wmLButtonUp,
		wmRButtonDown, wmRButtonDblClk, wmRButtonUp,
		wmMButtonDown, wmMButtonDblClk, wmMButtonUp,
		wmXButtonDown, wmXButtonDblClk, wmXButtonUp,
		wmMouseWheel, wmMouseHWheel:
		if m == wmMouseMove && !c.tracking {
			tme := trackMouseEvent{Flags: tmeLeaveFlag, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			c.tracking = true
		}
		// The pointer is the content's here, as where the content painted over
		// a native view: the mouse messages go to the surface, which routes
		// them (its own web views included). The cover window is at the
		// surface's origin and as big as it, so the pointer coordinates are
		// already the surface's; a wheel message carries screen coordinates,
		// as it does at the surface.
		procSendMessageW.Call(c.s.surfaceHWND(), uintptr(m), wp, lp)
		return 0, true
	case wmMouseLeave:
		// The pointer left the boxes the content takes: it hears it as its
		// own mouse leaving, so the frame it shows keeps no hover.
		c.tracking = false
		procSendMessageW.Call(c.s.surfaceHWND(), wmMouseLeave, 0, 0)
		return 0, true
	case wmDestroy:
		if c.s != nil && c.s.cover == hwnd {
			c.s.cover = 0
		}
		delete(nativeHostWindows, hwnd)
		return 0, true
	}
	return 0, false
}

// NewNativeView makes the platform side of a native view: it makes the clip
// window the app's control goes in, calls make with its host, and takes what
// it made. It is the platform call of platform.NativeViewSurface: the view the
// factory returned is destroyed by the core when this returns an error, so
// nothing here destroys it.
func (h *nativeViewSurface) NewNativeView(make func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	s := h.s
	w := s.w
	if w == nil || w.closed || w.destroying || s.hwnd == 0 || !isWindow(s.hwnd) {
		return nil, errors.New("mygo: windows: the window of the native view is gone")
	}
	if !s.viaComposition() && w.b != nil && w.b.composition() == nil {
		// The frames of the content must show over the app's control, which
		// they do as a topmost composition target. Without DirectComposition
		// the frame would be drawn into the window itself, over every child
		// window of it: mygo cannot host a native view there.
		return nil, errors.New("mygo: windows: no DirectComposition to show the frames of the content over a native view")
	}
	registerNativeHostClass()
	// The clip window is a child of the surface's window: under the frames the
	// content draws (a topmost DirectComposition target of that window) and
	// over its web views (a non-topmost one).
	//
	// No WS_CLIPSIBLINGS: what the app's control draws must keep its pixels
	// where the content painted over the clip window (the boxes of the cover
	// window), or a translucent popup there would blend with nothing. A clip
	// window that clips its siblings draws nothing under the cover window,
	// which is the same as cutting the covers out of the control's region.
	clip := createWindow(0, nativeHostClass, "", wsChild, 0, 0, 0, 0, s.hwnd)
	if clip == 0 {
		return nil, errors.New("mygo: windows: cannot create the window of the native view")
	}
	v := &nativeView{s: h, clip: clip}
	registerNativeHostWindow(clip, v)
	composing := s.viaComposition()
	// The app's control goes in the clip window, and is under the surface from
	// here on: a factory that fails is given the parent it made its control
	// in, and mygo destroys what it returned, never what it did not.
	view, err := make(platform.NativeViewHost{Platform: "windows", Parent: clip, Scale: float64(s.dpi()) / 96})
	if err != nil {
		h.dropNativeView(v)
		return nil, err
	}
	if view == nil {
		h.dropNativeView(v)
		return nil, errors.New("mygo: windows: the native view's factory made none")
	}
	if w.closed || w.destroying || s.hwnd == 0 || !isWindow(s.hwnd) {
		// The window closed while the app made its view (its factory may
		// close it): the view is hosted nowhere, and the core destroys what
		// the factory returned.
		h.dropNativeView(v)
		return nil, errors.New("mygo: windows: the window of the native view closed while it was made")
	}
	// The handle of what the factory returned is only looked at once the
	// window that is to host it is known to be live: the app's control may
	// already be gone with a window its factory closed.
	handle := view.NativeHandle()
	if w.closed || w.destroying || s.hwnd == 0 || !isWindow(s.hwnd) {
		// NativeHandle is the app's own getter, which runs the app and may
		// close the window while it does: the view is hosted nowhere, and the
		// core destroys what the factory returned.
		h.dropNativeView(v)
		return nil, errors.New("mygo: windows: the window of the native view closed while it was made")
	}
	if err := checkNativeChild(s, clip, handle); err != nil {
		h.dropNativeView(v)
		return nil, err
	}
	v.adapter, v.handle = view, handle
	h.native = append(h.native, v)
	if !composing {
		// The frames of the content must show over the control: the content
		// makes its renderer again, which then composes the frames of the
		// surface's window, over its children.
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceRenew})
	}
	return platform.NativeView(v), nil
}

// checkNativeChild reports a handle the surface can host: a control the app
// made in the clip window its factory was given, on this thread, in this
// process. What the handle's *parent* must be is decided by nativeChildError
// (nativegeom.go), which is tested without a Windows runtime: IsChild alone
// says yes to any descendant, and a control nested under an intermediate
// window would be placed at that window's offset instead of where the content
// shows the view. A window of another parent's, another thread's, another
// process's, or one of mygo's own, is never taken from it.
func checkNativeChild(s *surface, clip, handle uintptr) error {
	var pid uint32
	thread, _, _ := procGetWindowThreadProcessId.Call(handle, uintptr(unsafe.Pointer(&pid)))
	cur, _, _ := procGetCurrentProcessId.Call()
	// GetParent, not the GWLP_HWNDPARENT word: that word is a top-level
	// window's owner, and can be set to another window, while what is judged
	// here is the window the handle really is in. A top-level window whose
	// owner is the clip window is still no child of it, and the inClip fact
	// (IsChild) is what refuses it.
	parent, _, _ := procGetParent.Call(handle)
	return nativeChildError(nativeChild{
		handle:         handle,
		clip:           clip,
		isWindow:       isWindow(handle),
		mine:           handle == s.hwnd || (s.w != nil && handle == s.w.hwnd),
		inClip:         isChild(clip, handle),
		parent:         parent,
		process:        pid,
		currentProcess: uint32(cur),
		thread:         thread,
		currentThread:  uintptr(currentThreadID()),
	})
}

// dropNativeView gives up the clip window of a native view the surface could
// not take. The app's view is not destroyed here: the core destroys the view
// the factory returned once this returns. A clip window that holds a control
// waits for that: it is given up with the reaping message, so nothing here
// destroys a control of the app's before the app's own Destroy ran.
func (h *nativeViewSurface) dropNativeView(v *nativeView) {
	h.forgetNative(v)
	v.closed = true
	clip := v.clip
	v.clip = 0
	if clip == 0 || !isWindow(clip) {
		return
	}
	if firstChild(clip) != 0 {
		postMessage(clip, wmNativeViewReap, 0, 0)
		return
	}
	procDestroyWindow.Call(clip)
}

// PlaceNativeViews shows the window's native views where the content shows
// them, in its paint order among the web views, and hides the others, which
// keep their state. It is the platform call of platform.NativeViewSurface.
func (h *nativeViewSurface) PlaceNativeViews(views []platform.NativeViewPlacement) {
	s := h.s
	if s.w == nil || s.w.closed || s.w.destroying || s.hwnd == 0 || !isWindow(s.hwnd) {
		return
	}
	h.placedNative = append(h.placedNative[:0], views...)
	for _, v := range h.native {
		v.placed = false
	}
	for _, p := range views {
		v, ok := p.View.(*nativeView)
		if !ok || v.closed || v.s != h {
			continue
		}
		v.frame, v.clipRect, v.order = p.Frame, p.Clip, p.Order
		v.placed = true
	}
	h.layoutNatives()
}

// TakeFocus gives the content the keyboard back from a native view, as Tab
// leaving its last control does. A view that does not hold the keyboard keeps
// it: nothing here takes the keyboard from a page, or from another window.
func (h *nativeViewSurface) TakeFocus() {
	s := h.s
	if s.w == nil || s.w.closed || s.w.destroying || s.hwnd == 0 || !isWindow(s.hwnd) {
		return
	}
	for _, v := range h.native {
		if v.hasKeyboard() {
			procSetFocus.Call(s.hwnd)
			return
		}
	}
}

// layoutNatives shows the native views of the surface where the content last
// put them, in the pixels of the surface's DPI, stacks them in the order it
// painted them, cuts the boxes a later web view shows out of them, and gives
// the cover window the boxes it takes.
func (h *nativeViewSurface) layoutNatives() {
	for _, v := range h.native {
		if v.closed {
			continue
		}
		if v.placed {
			h.placeNative(v)
		} else {
			h.hideNative(v)
		}
	}
	h.stackNative()
	h.cutNativeClips()
	h.coverNative()
}

// fitNatives lays the native views out again for a surface that was resized or
// whose DPI changed: the cover window is resized with it, and its region is
// built again, as the same DIPs are other pixels.
func (h *nativeViewSurface) fitNatives() {
	if len(h.native) == 0 {
		return
	}
	if h.cover != 0 {
		r := clientRect(h.s.hwnd)
		procSetWindowPos.Call(h.cover, 0, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
	}
	h.coverKey = h.coverKey[:0]
	h.layoutNatives()
}

// placeNative shows a native view: the clip window where the content clips it,
// and the app's control in it, at its frame's offset from the clip and its
// size. A clip that moves as the content scrolls moves the control without
// resizing it, so what it draws keeps its own layout.
func (h *nativeViewSurface) placeNative(v *nativeView) {
	dpi := h.s.dpi()
	clip := pxRect(v.clipRect, dpi)
	if clip.empty() {
		h.hideNative(v)
		return
	}
	if clip != v.clipAt {
		// Not HWND_TOP: the clip windows are stacked in the order the content
		// painted them (stackNative), which moving one must not change.
		procSetWindowPos.Call(v.clip, 0, uintptr(clip.Left), uintptr(clip.Top),
			uintptr(clip.Right-clip.Left), uintptr(clip.Bottom-clip.Top), swpNoZOrder|swpNoActivate)
		v.clipAt = clip
	}
	if !v.shown {
		v.shown = true
		procShowWindow.Call(v.clip, swShowNoActivate)
		// Showing a window may raise it over its siblings, which the cached
		// stack no longer describes: stackNative puts it back in the content's
		// paint order, and only when one of these calls did raise something.
		h.stackedNative = nil
	}
	if v.handle == 0 || !isWindow(v.handle) {
		return
	}
	frame := pxRect(v.frame, dpi)
	at := childAt(clip, frame)
	if v.hasChild && at == v.childAt {
		return
	}
	flags := uintptr(swpNoZOrder | swpNoActivate)
	if v.hasChild && sameSize(at, v.childAt) {
		// The size did not change: moving the control must not re-lay it out.
		flags |= swpNoSize
	}
	if r, _, _ := procSetWindowPos.Call(v.handle, 0, uintptr(at.Left), uintptr(at.Top),
		uintptr(at.Right-at.Left), uintptr(at.Bottom-at.Top), flags); r != 0 {
		v.childAt, v.hasChild = at, true
	}
}

// hideNative hides the view of one the content no longer shows, keeping its
// state: the keyboard goes back to the content if a control of it has it, and
// the clip window stays, hidden, until the app destroys the view.
func (h *nativeViewSurface) hideNative(v *nativeView) {
	if !v.shown || v.clip == 0 {
		return
	}
	keyboard := v.hasKeyboard()
	v.shown = false
	procShowWindow.Call(v.clip, swHide)
	if keyboard {
		h.giveBackKeyboard()
	}
}

// stackNative puts the clip windows in the order the content painted them,
// last painted on top: they are siblings of one another, and their boxes may
// overlap, where a later view shows over an earlier one.
//
// hWndInsertAfter puts a window *below* the window it names, so the plan walks
// the paint order downwards (stackNativePlan, where the sequence is tested):
// the last painted window first, at the top of the stack, and each next one
// directly below the one before it. A stack that is already the content's is
// left alone, and nothing is moved when no view was placed or shown.
func (h *nativeViewSurface) stackNative() {
	views := make([]*nativeView, 0, len(h.native))
	for _, v := range h.native {
		if !v.closed && v.placed && v.shown {
			views = append(views, v)
		}
	}
	orders := make([]int, len(views))
	for i, v := range views {
		orders[i] = v.order
	}
	plan := stackNativePlan(orders)
	// The stack the plan gives, top first: it is what the surface remembers, so
	// that a stack that did not change -- no view added, removed or reordered
	// -- moves nothing at all.
	stacked := make([]*nativeView, len(plan))
	for i, step := range plan {
		stacked[i] = views[step.View]
	}
	if slices.Equal(stacked, h.stackedNative) {
		return
	}
	for _, step := range plan {
		var below uintptr
		if step.Below != topOfStack {
			below = views[step.Below].clip
		}
		procSetWindowPos.Call(views[step.View].clip, below, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
	h.stackedNative = append(h.stackedNative[:0], stacked...)
}

// webClips are the clips of the web views the surface shows, with the order the
// content painted them: the boxes the region of a native view's clip window
// leaves room for (clipHoles). A web view of another window of mygo's, or one
// the surface no longer shows, is no box of this content's.
func (h *nativeViewSurface) webClips() []platform.WebViewPlacement {
	s := h.s
	var webs []platform.WebViewPlacement
	for i := range s.placed {
		p := &s.placed[i]
		v, ok := p.WebView.(*window)
		if !ok || v.closed || v.host != s.w || !v.shown {
			continue
		}
		webs = append(webs, platform.WebViewPlacement{Clip: p.Clip, Order: p.Order})
	}
	return webs
}

// cutNativeClips gives each shown clip window the region of the boxes of the
// app's control it shows, leaving out the boxes a web view the content painted
// after the view shows through (clipHoles): web views are a non-topmost
// composition target, under every child window of the surface, so a child
// window covers one unless its own region leaves room, and its pixels must
// agree with what takes the pointer.
//
// It runs whenever the native views are laid out, and whenever the web views
// are placed (PlaceWebViews): the boxes and the paint order of the web views
// both decide, and either call may be the one that changed them.
func (h *nativeViewSurface) cutNativeClips() {
	if len(h.native) == 0 {
		return
	}
	dpi := h.s.dpi()
	webs := h.webClips()
	for _, v := range h.native {
		if v.closed || !v.placed || !v.shown || v.clip == 0 {
			continue
		}
		h.cutNativeClip(v, dpi, webs)
	}
}

// cutNativeClip gives one clip window its region, or gives its whole area back
// when no web view shows over it. The region is the window's area without the
// boxes of the later web views (clipRegion): the app's control shows nothing
// there, the page shows through, and the pointer over those boxes is the
// surface's, which routes it to the web view, as the content painted it.
//
// The boxes the content painted over the view (NativeViewPlacement.Covers) are
// not cut: they are the content's pixels, and the transparent cover window
// above the clip window takes the pointer there, so the pixels the app's
// control drew under a translucent popup stay (newCover).
func (h *nativeViewSurface) cutNativeClip(v *nativeView, dpi int, webs []platform.WebViewPlacement) {
	clip := pxRect(v.clipRect, dpi)
	if clip.empty() {
		return
	}
	holes := clipHoles(clip, v.order, webs, dpi)
	region := clipRegion(clip, holes)
	if len(holes) == 0 {
		// The whole clip: a window no web view shows over any more gives its
		// area back, and the system, which owned the region the window took,
		// keeps none of it (SetWindowRgn with no region).
		if v.cutRegion {
			procSetWindowRgn.Call(v.clip, 0, 1)
			v.cutRegion = false
		}
		v.cut = append(v.cut[:0], region...)
		return
	}
	if v.cutRegion && slices.Equal(region, v.cut) {
		// The window already shows exactly these boxes.
		return
	}
	rgn := createRectRgn(0, 0, 0, 0)
	if rgn == 0 {
		return
	}
	for _, r := range region {
		unionRegion(rgn, r)
	}
	// The window owns the region once it took it; a region it would not take
	// is still mygo's, and is given back here (coverNative does the same for
	// the cover window).
	if ok, _, _ := procSetWindowRgn.Call(v.clip, rgn, 1); ok == 0 {
		procDeleteObject.Call(rgn)
		return
	}
	v.cutRegion = true
	v.cut = append(v.cut[:0], region...)
}

// closeNativeViews gives up every native view of the surface, and with each the
// app's own control, before Windows destroys the child windows of the window:
// DestroyWindow destroys the children first and sends WM_DESTROY to the parent
// after, and the core destroys the native views of a window on that message,
// when the clip windows and the controls in them would already be gone. The
// window's own teardown calls this (window.go, destroy), which is the only
// place early enough for the app's Destroy to run while its control lives.
func (h *nativeViewSurface) closeNativeViews() {
	for _, v := range slices.Clone(h.native) {
		v.Destroy()
	}
}

// coverNative gives the cover window the boxes the content painted over the
// native views of the surface, in the pixels of the surface, and keeps it on
// top of the clip windows, which stackNative raises.
func (h *nativeViewSurface) coverNative() {
	dpi := h.s.dpi()
	// Only the placements of the views the surface still holds: a placement
	// the content dropped with its view keeps no box in the region.
	placed := make([]platform.NativeViewPlacement, 0, len(h.placedNative))
	key := make([]pixelRect, 0, len(h.placedNative)*2)
	covers := 0
	for _, p := range h.placedNative {
		if v, ok := p.View.(*nativeView); !ok || v.closed || v.s != h {
			continue
		}
		placed = append(placed, p)
		key = append(key, pxRect(p.Clip, dpi))
		for _, c := range p.Covers {
			key = append(key, pxRect(c, dpi))
			covers++
		}
	}
	changed := !slices.Equal(key, h.coverKey)
	if changed {
		h.coverKey = append(h.coverKey[:0], key...)
	}
	if covers == 0 {
		if h.cover != 0 {
			procShowWindow.Call(h.cover, swHide)
			if h.coverRegion {
				// Nothing of the content is painted over a native view any
				// more: the window takes its whole area back, and the system,
				// which owned the region the window took, keeps none of it
				// (SetWindowRgn with no region). The key stays what the region
				// was last built from; coverRegion makes it build again when
				// the covers come back.
				procSetWindowRgn.Call(h.cover, 0, 1)
				h.coverRegion = false
			}
		}
		return
	}
	if h.cover == 0 {
		h.cover = h.newCover()
		if h.cover == 0 {
			return
		}
		changed = true
	}
	if changed || !h.coverRegion {
		// The region is what each view takes, in the order the content painted
		// the views (coverSteps): the app's control of a view takes its own
		// boxes, and the content the boxes painted over it; a later view takes
		// from an earlier one what its own place holds.
		region := createRectRgn(0, 0, 0, 0)
		if region == 0 {
			return
		}
		for _, step := range coverSteps(placed, dpi) {
			for _, c := range step.Control {
				if own := rectRegion(c); own != 0 {
					procCombineRgn.Call(region, region, own, rgnDiff)
					procDeleteObject.Call(own)
				}
			}
			for _, c := range step.Covers {
				unionRegion(region, c)
			}
		}
		// The window owns the region from here on; a region it would not take
		// is still mygo's, and is given back here.
		if ok, _, _ := procSetWindowRgn.Call(h.cover, region, 1); ok == 0 {
			procDeleteObject.Call(region)
			return
		}
		h.coverRegion = true
		procShowWindow.Call(h.cover, swShowNoActivate)
	}
	// The clip windows were stacked, which may have raised one over the cover
	// window: without this the pointer over a cover would go to the control
	// under it.
	procSetWindowPos.Call(h.cover, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

// newCover makes the cover window of the surface, as big as its client area:
// its boxes are a region of it, where the pointer is the content's. It draws
// nothing, which is what makes it transparent: its class has no background
// brush, and it neither erases nor paints, so the pixels the clip windows drew
// under it stay (a translucent popup over a native view blends with them).
//
// No WS_EX_TRANSPARENT: with WS_EX_LAYERED that style is documented to pass
// the mouse to the window under a layered window, which is the one thing the
// cover must not do; and the cover needs no paint ordering, as it draws
// nothing. Its region, and nothing else, decides where the pointer is its own.
func (h *nativeViewSurface) newCover() uintptr {
	r := clientRect(h.s.hwnd)
	hwnd := createWindow(0, nativeHostClass, "", wsChild,
		0, 0, r.Right-r.Left, r.Bottom-r.Top, h.s.hwnd)
	if hwnd == 0 {
		return 0
	}
	registerNativeHostWindow(hwnd, &nativeCover{s: h})
	return hwnd
}

// forgetNative takes a view out of what the surface holds, once it is gone.
func (h *nativeViewSurface) forgetNative(v *nativeView) {
	h.native = slices.DeleteFunc(h.native, func(x *nativeView) bool { return x == v })
	h.stackedNative = slices.DeleteFunc(h.stackedNative, func(x *nativeView) bool { return x == v })
	h.placedNative = slices.DeleteFunc(h.placedNative, func(p platform.NativeViewPlacement) bool {
		return p.View == platform.NativeView(v)
	})
	s := h.s
	if s.w != nil && !s.w.closed && !s.w.destroying && s.hwnd != 0 && isWindow(s.hwnd) {
		// A view that is gone takes nothing with it: a box it left in the
		// cover window's region goes back to the content. Not while the window
		// is being destroyed: the whole surface goes, region and all.
		h.coverNative()
	}
}

// giveBackKeyboard gives the keyboard to the content, for a view that hides or
// is destroyed while a control of it has it.
func (h *nativeViewSurface) giveBackKeyboard() {
	s := h.s
	if s.w == nil || s.w.closed || s.hwnd == 0 || !isWindow(s.hwnd) {
		return
	}
	procSetFocus.Call(s.hwnd)
}

// The procs this file calls itself. They are lazily found, like the rest of
// the package's, so that a window of native UI that shows no native view pays
// nothing for them.
var (
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsChild                  = user32.NewProc("IsChild")
	procGetParent                = user32.NewProc("GetParent")
	procGetFocus                 = user32.NewProc("GetFocus")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetCurrentProcessId      = kernel32.NewProc("GetCurrentProcessId")
	procSetWindowRgn             = user32.NewProc("SetWindowRgn")
	procCreateRectRgn            = gdi32.NewProc("CreateRectRgn")
	procCombineRgn               = gdi32.NewProc("CombineRgn")
)

func isWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

func isChild(parent, child uintptr) bool {
	r, _, _ := procIsChild.Call(parent, child)
	return r != 0
}

func getFocus() uintptr {
	r, _, _ := procGetFocus.Call()
	return r
}

func firstChild(hwnd uintptr) uintptr {
	r, _, _ := procGetNextWindow.Call(hwnd, gwChild)
	return r
}

func clientRect(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// paintNothing validates the update region of a window that draws nothing,
// which leaves what is under it (a control, the frames of the content) alone.
func paintNothing(hwnd uintptr) {
	var ps paintStruct
	procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
}

func createRectRgn(left, top, right, bottom int32) uintptr {
	r, _, _ := procCreateRectRgn.Call(uintptr(left), uintptr(top), uintptr(right), uintptr(bottom))
	return r
}

// rectRegion is a window region of one rectangle, or zero.
func rectRegion(r pixelRect) uintptr {
	if r.empty() {
		return 0
	}
	return createRectRgn(r.Left, r.Top, r.Right, r.Bottom)
}

// unionRegion adds one rectangle to a window region, which then owns it.
func unionRegion(region uintptr, r pixelRect) {
	if one := rectRegion(r); one != 0 {
		procCombineRgn.Call(region, region, one, rgnOr)
		procDeleteObject.Call(one)
	}
}
