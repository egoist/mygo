package mygo

import (
	"errors"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// NativeView is a platform view inside a window's native UI, which the
// window's view places with ui.NativeView: a control the app makes with the
// platform's own toolkit, beside MyGo's own controls and its web views.
//
//	v, err := mygo.NewNativeView(win, mygo.NativeViewOptions{Create: func(h *mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
//		return newView(h.Parent, h.Scale)
//	}})
//
//	// In the view:
//	ui.NativeView(c, v).Grow(1)
//
// What a backend hosts is a widget of the toolkit it runs on, put in the
// same process as the window's other controls: an NSView on macOS, a child
// HWND on Windows, a GtkWidget on Linux. What the host takes is a widget the
// toolkit can host that way; a special control the toolkit does not — a
// terminal, a video player, a map, or anything else that brings a GPU or
// video surface of its own — is not guaranteed to work, however it is
// wrapped.
//
// The view shows where the view places it, inside the window's own layout,
// cut by the scroll containers and clips around it, under what the view
// paints after it, as popovers and menus. It hides while the view does not
// build it, keeping its state, until Destroy or until its window closes.
// MyGo owns no part of the view: the app makes it with Create and destroys
// it with Destroy, which MyGo calls on the main thread, once.
//
// Its methods are safe from any goroutine.
type NativeView struct {
	w *Window
	// native is the platform side of the view, as SurfaceNativeView gives
	// it to the content, provider the platform surface it was made
	// through, as SurfaceNativeViewHost gives it to the content, and
	// adapter the view the app made, which mygo destroys.
	native   platform.NativeView
	provider platform.NativeViewSurface
	adapter  NativeViewAdapter
	handle   uintptr
	// host is what the factory heard of the window, and where the view
	// reports its own events.
	host *NativeViewHost
	// bridge is the window's native view state, the
	// *surface.NativeViewBridge holding the views its cleanup destroys,
	// which the view joined when the platform attached it. The view keeps
	// it, not the window: a window clears its connection before its
	// cleanup runs (Window.detachContent), so what outlives the detach and
	// takes the view out of it is the view's own (invalidate). Nil for a
	// view that never came to be. Guarded by the lock of windows, as the
	// Views of the bridge are.
	bridge *surface.NativeViewBridge
	// misplaced is set once the content of another window placed the view,
	// which is logged once.
	misplaced bool
	destroyed atomic.Bool
}

// NativeViewAdapter is the platform view behind a NativeView: what
// NativeViewOptions.Create returns. It is the app's own object, and the Go
// value stays the app's after MyGo destroys the view; the platform object
// behind it does not. What NativeHandle reported is what MyGo shows, and it
// is dead once Destroy ran: never use the control through that handle after
// MyGo destroyed the view.
type NativeViewAdapter interface {
	// NativeHandle returns the platform handle of the view: an NSView* on
	// macOS, an HWND on Windows, a GtkWidget* on Linux. Zero when the view
	// has none, which fails the creation.
	//
	// MyGo calls it once, during creation and after Create returned, and
	// shows the view at that handle until it destroys the view: the handle
	// must stay the view's own for as long as the view lives. MyGo never
	// asks the app for another one; Focus and Destroy are what it calls
	// after that.
	NativeHandle() uintptr
	// Focus gives the view the keyboard as Tab does, at its first control
	// taking the focus, or its last when back, as Shift+Tab does. Tab past
	// the other end comes back as NativeViewHost.TabOut.
	Focus(back bool)
	// Destroy destroys the view. MyGo calls it once, on the main thread,
	// when the app destroys the native view (NativeView.Destroy) or its
	// window closes.
	Destroy()
}

// nativeViewHandleAdapter is the app's view as the platform hears it: it
// answers the handle the factory read from the app once, forwards Focus to
// the app's own view, and destroys that view once, whether the app destroys
// the native view or the platform destroys its own children first. The
// platform is handed this, so the app's getter runs exactly once, while the
// factory runs, and the handle the view was registered at is the one mygo
// read then: a getter that closes the window, or replaces its content,
// cannot make mygo attach the view of a window that moved on.
type nativeViewHandleAdapter struct {
	NativeViewAdapter
	// v is the core's view of the app's view, which this invalidates
	// before the app's own Destroy runs (Destroy).
	v *NativeView
	// handle is what the factory read from the app's view, once, and what
	// the platform is told for as long as the app's view lives
	// (NativeViewAdapter.NativeHandle).
	handle uintptr
	// cleaned is set by the one call that reaches the app's own Destroy,
	// whether it came from the app (NativeView.Destroy) or from the
	// platform destroying its children first.
	cleaned atomic.Bool
}

func (a *nativeViewHandleAdapter) NativeHandle() uintptr { return a.handle }

// Destroy destroys the app's own view, once. MyGo calls NativeView.Destroy
// itself when the app destroys the native view, but a backend that closes
// its native children as its window goes destroys this view before the
// window's handler hears of the close: the core's view is still live then,
// and the app's own Destroy — which may report a focus, a Tab out or a
// press — would reach a content that is on its way out. The core's view is
// invalidated first, here, so those callbacks stop at it, and it leaves the
// window's cleanup before the app's view goes, so neither the window's
// close nor a later NativeView.Destroy destroys the app's view again.
func (a *nativeViewHandleAdapter) Destroy() {
	a.v.invalidate()
	if a.cleaned.Swap(true) {
		return
	}
	a.NativeViewAdapter.Destroy()
}

// NativeViewHost is what a NativeViewOptions.Create factory hears of the
// window it makes the view for, and where the view reports its own events.
// It is filled before the factory runs: read it there, or keep it.
//
// Its callbacks are set before the factory runs and are safe from any
// goroutine, so the view may call them while it is made: what they report
// then, before mygo knows the view, is ignored — not kept and replayed once
// the view is registered — and what they report once the view was destroyed,
// or its window closed, does nothing either.
type NativeViewHost struct {
	// Platform is the platform the view runs on: "darwin", "windows" or
	// "linux".
	Platform string
	// Parent is the platform handle of the container MyGo made for this
	// view, which the app borrows and never owns: an NSView* on macOS, a
	// child HWND on Windows, a GtkWidget* on Linux. It is always a
	// container of MyGo's inside the window, never the window itself or a
	// view of the app's, and the factory must neither destroy it nor take
	// its control out of MyGo's hierarchy: MyGo never reparents it.
	Parent uintptr
	// Scale is the window's scale when the factory runs: how many device
	// pixels a DIP is there, which the backend read before it made the
	// view. It is a snapshot of that moment: a window that moves to a
	// screen of another scale afterwards keeps the value here, and a view
	// that must stay right asks its own toolkit for the scale when the
	// scale changes.
	Scale float64
	// Focused reports that the view took the keyboard, as a click on one
	// of its controls does: the content moves its focus to the view's
	// element, and nothing more. It closes nothing: a press outside them
	// is what closes popovers, and the view reports that as PointerDown.
	Focused func()
	// TabOut reports that Tab moved the keyboard out of the view, past its
	// last control, or Shift+Tab past its first (back): the content moves
	// the focus on from the view's element.
	TabOut func(back bool)
	// PointerDown reports that the pointer went down on the view at (x, y),
	// in DIPs relative to the view itself: (0, 0) is the view's own top
	// left corner, not the window's, and the point is the same wherever the
	// view is placed. A backend reports the press in its toolkit's own
	// coordinates, and the view converts what that is not DIPs already:
	// Windows reports device pixels, which are divided by the scale the
	// window has at the press, while macOS and GTK report points that are
	// DIPs. The content moves it to its own space with where the view
	// shows: it notes the press, as popovers close for a press outside
	// them, but handles none of it.
	PointerDown func(x, y float64)
}

// NativeViewOptions are how NewNativeView makes a native view.
type NativeViewOptions struct {
	// Create makes the view, once, on the main thread, with the host of
	// the view it makes. It returns the view's adapter, whose handle the
	// window's content shows: that handle is read once, right after Create
	// returns, and must stay the view's own until mygo destroys it
	// (NativeViewAdapter.NativeHandle). An error fails the creation; the
	// adapter it returns with an error is destroyed, so a factory that
	// fails after making its view must return it.
	Create func(host *NativeViewHost) (NativeViewAdapter, error)
}

// The errors NewNativeView returns besides the factory's own.
var (
	// errNativeViewUnsupported is returned on platforms whose backend
	// shows no native views, as the browser and unsupported platforms: the
	// apps that make one were told that macOS, Windows and Linux have
	// them. errors.Is(err, errors.ErrUnsupported) holds for it.
	//
	// It is a value of a zero-sized type, as platform.ErrUnsupported, so
	// that packages that make no native view run no code for this: apps
	// pay only for what they use.
	errNativeViewUnsupported error = nativeViewUnsupportedError{}
	// errNativeViewNoCreate is returned for NativeViewOptions without a
	// factory.
	errNativeViewNoCreate = errors.New("mygo: NativeViewOptions.Create is required")
	// errNativeViewNoAdapter is returned for a factory that returned no
	// view, or one the platform would not take.
	errNativeViewNoAdapter = errors.New("mygo: the native view's factory made no view")
	// errNativeViewNoHandle is returned for a view without a platform
	// handle, which the content cannot show.
	errNativeViewNoHandle = errors.New("mygo: the native view has no platform handle")
	// errNativeViewStale is returned for a view whose factory ran while the
	// window's content went away: no native view joins content that is no
	// longer there.
	errNativeViewStale = errors.New("mygo: the window's content changed while the native view was made")
)

// nativeViewUnsupportedError says that this platform's backend shows no
// native view. It is errors.ErrUnsupported, and platform.ErrUnsupported, to
// apps checking either (errors.Is).
type nativeViewUnsupportedError struct{}

func (nativeViewUnsupportedError) Error() string {
	return "mygo: native views: not supported on this platform"
}

func (nativeViewUnsupportedError) Is(target error) bool {
	return target == errors.ErrUnsupported || target == platform.ErrUnsupported
}

// nativeViewSurfaceFallback returns s as a surface that hosts native views
// through the optional interface (platform.NativeViewSurface), for the
// surfaces whose backend reaches the interface that way rather than through
// an always-linked provider: the fake surface the core's own tests show
// native views with, and the surfaces of the platforms whose provider is the
// interface itself (nativeview_backend_js.go, nativeview_backend_other.go).
// It reports nil for a surface that hosts no native view.
func nativeViewSurfaceFallback(s platform.Surface) platform.NativeViewSurface {
	if s == nil {
		return nil
	}
	ns, _ := s.(platform.NativeViewSurface)
	return ns
}

// NewNativeView creates a native view for the window's native UI, which
// shows it where its view places it with ui.NativeView. The window must
// show Content, and the platform must support native views: the browser and
// unsupported platforms report an error for which
// errors.Is(err, errors.ErrUnsupported) holds. A window that is nothing, and
// one that is gone, report an error and no view: nothing of the app's is
// made for either.
//
// It is the constructor of the feature, and the apps that show no native
// view call none of it: the native-view hosting of a platform is reachable
// only from here, so an app pays nothing for it unless it makes a view
// (AGENTS.md: apps pay only for what they use). It is a function rather
// than a method of Window because every window puts its *Window in the
// connection's Window, an any, which marks the type as used in an interface
// for the linker, and that keeps every exported method of *Window — and the
// native-view hosting in the body of one — in every binary, the apps that
// show no native view at all included.
func NewNativeView(w *Window, opts NativeViewOptions) (*NativeView, error) {
	if opts.Create == nil {
		return nil, errNativeViewNoCreate
	}
	if w == nil {
		return nil, errDestroyed
	}
	var v *NativeView
	err := errDestroyed
	// What the window is — its content, its native window, the connection
	// its content draws through — is the main thread's, which is where
	// closing the window writes it: read it there, where the two cannot
	// interleave, not here where a window closing under the caller would
	// race the read.
	onMain(func() {
		switch {
		case w.content == nil:
			err = errNoContent
		case w.native == nil || w.conn == nil:
			err = errDestroyed
		default:
			v, err = newNativeView(w, opts)
		}
	})
	return v, err
}

// newNativeView creates a native view of the window. Main thread only. It is
// a function of the window rather than a method on purpose: a method of
// *Window would be in every binary, as the exported methods of a type held
// in an interface are (NewNativeView), and the code of the native views must
// not be.
func newNativeView(w *Window, opts NativeViewOptions) (*NativeView, error) {
	conn := w.conn
	if conn == nil {
		return nil, errDestroyed
	}
	ns := nativeViewSurfaceFor(conn.Surface)
	if ns == nil {
		return nil, errNativeViewUnsupported
	}
	v := &NativeView{w: w}
	host := &NativeViewHost{}
	// The view may report its own events while the factory runs, before
	// mygo knows it: the callbacks are the view's already, and reach the
	// window only once the view is registered and placed.
	host.Focused, host.TabOut, host.PointerDown = v.tookFocus, v.tabOut, v.pointerDown
	// The platform calls make with the host of the view: the window's
	// parent handle and scale, which it knows and the core does not.
	var (
		// adapter is the view the app made, which mygo destroys, and view
		// the same view as the platform hears it: it answers the handle
		// read below, and forwards Focus and Destroy to the app's own
		// view (nativeViewHandleAdapter).
		adapter NativeViewAdapter
		view    platform.NativeView
		// handle is the platform handle of the app's view, read from the
		// app's own getter exactly once, while the factory runs.
		handle uintptr
	)
	make := func(ph platform.NativeViewHost) (platform.NativeView, error) {
		host.Platform, host.Parent, host.Scale = ph.Platform, ph.Parent, ph.Scale
		a, err := opts.Create(host)
		if err != nil {
			// The view never was: nothing it reports reaches a window, and
			// the adapter it returned with the error is destroyed once.
			v.fail()
			if a != nil {
				a.Destroy()
			}
			return nil, err
		}
		if a == nil {
			v.fail()
			return nil, errNativeViewNoAdapter
		}
		// The factory runs the app, which may close the window, or replace
		// or detach its content: the view joins nothing of that.
		if err := nativeViewOwner(w, conn); err != nil {
			v.fail()
			a.Destroy()
			return nil, err
		}
		// The app's getter runs the app as well, and what it returns is its
		// view's handle for as long as the view lives: mygo reads it once,
		// here, and hands the platform a view that answers it from what was
		// read, so the app is never asked for a handle again. The owner is
		// checked once more after the getter ran, because the getter is the
		// app's code: it may have closed the window, and a view of a window
		// that moved on must not be registered.
		handle = a.NativeHandle()
		if err := nativeViewOwner(w, conn); err != nil {
			v.fail()
			a.Destroy()
			return nil, err
		}
		if handle == 0 {
			v.fail()
			a.Destroy()
			return nil, errNativeViewNoHandle
		}
		adapter = a
		view = &nativeViewHandleAdapter{NativeViewAdapter: a, handle: handle, v: v}
		return view, nil
	}
	n, err := ns.NewNativeView(make)
	if err != nil {
		v.fail()
		if view != nil {
			// The platform would not attach the view the factory made.
			view.Destroy()
		}
		return nil, fmt.Errorf("mygo: cannot create a native view: %w", err)
	}
	if n == nil || adapter == nil {
		// The platform's surface returns the view its factory made, or an
		// error; it holds no app code of its own after the factory ran, so a
		// view it did not make is a backend bug, not a state an app reaches.
		panic("mygo: the " + backend().Name() + " backend attached no view for the native view's factory")
	}
	v.native, v.provider, v.adapter, v.handle, v.host = n, ns, adapter, handle, host

	// The window's native view state, made here with the cleanup of the
	// views it owns: the windows that make no native view never reach it,
	// and their close path keeps no branch of the feature. The view keeps
	// the bridge, so that taking itself out of the window's cleanup needs
	// no field of the window's, which the detach clears first.
	b := surface.NativeViewState(conn, destroyNativeViews)
	v.bridge = b
	windows.Lock()
	b.Views = append(b.Views, v)
	windows.Unlock()
	return v, nil
}

// nativeViewOwner reports whether the window that started a native view is
// still the one it joins: the same content, connection and native window as
// when its factory started. It returns the error creation reports when the
// window moved on while the factory ran. Main thread only.
func nativeViewOwner(w *Window, conn *surface.Conn) error {
	if w.native == nil || w.destroyed.Load() {
		return errDestroyed
	}
	if w.conn != conn || w.content == nil {
		return errNativeViewStale
	}
	return nil
}

// fail marks a view that never came to be: the callbacks of its adapter
// reach no window, and its cleanup destroys nothing twice. Main thread only.
func (v *NativeView) fail() {
	v.destroyed.Store(true)
}

// invalidate marks the view destroyed and takes it out of the window's
// cleanup, reporting whether this call was the one that did it: the second
// caller and every one after it get false. The window's next frame drops the
// place the view held, which is the caller's to ask for where it owns the
// redraw (NativeView.Destroy): a platform closing its children asks for
// nothing of a window that is going away. Main thread only.
func (v *NativeView) invalidate() bool {
	if v == nil || !v.destroyed.CompareAndSwap(false, true) {
		return false
	}
	windows.Lock()
	// The bridge is the view's own (NativeView.bridge), not the window's:
	// the window clears its connection before its cleanup runs
	// (Window.detachContent), and the view must leave the cleanup even
	// then, as the detach that runs it is what closes the views.
	if b := v.bridge; b != nil {
		for i, x := range b.Views {
			if x == v {
				b.Views = append(b.Views[:i:i], b.Views[i+1:]...)
				break
			}
		}
	}
	windows.Unlock()
	return true
}

// NativeHandle returns the platform handle of the view: an NSView* on
// macOS, an HWND on Windows, a GtkWidget* on Linux. Zero once destroyed.
func (v *NativeView) NativeHandle() uintptr {
	if v == nil || v.destroyed.Load() {
		return 0
	}
	return v.handle
}

// Destroy destroys the view, once: its window no longer shows it where the
// view still places it. The window redraws its content without it, so the
// place it held goes away, and the content stops reaching it. It does
// nothing the second time, and nothing after the window closed, which
// destroys the views of its content itself.
func (v *NativeView) Destroy() {
	if v == nil {
		return
	}
	onMain(func() {
		if !v.invalidate() {
			// Destroyed already: by the app, or by the platform that
			// destroyed its native children first (nativeViewHandleAdapter).
			return
		}
		if v.native != nil {
			v.native.Destroy()
		} else if v.adapter != nil {
			v.adapter.Destroy()
		}
		if w := v.w; w != nil {
			// The content may still place the view, and its next frame
			// takes the hole away: the app asks for nothing.
			w.Invalidate()
		}
	})
}

// Focus moves the keyboard to the view, as a press on it would: the content
// shows the view focused, and the view's own toolkit focuses it. A view the
// content does not show, one that was destroyed, and one of a window that
// closed take the keyboard from nothing: Focus does nothing for them. It is
// safe from any goroutine.
func (v *NativeView) Focus(back bool) {
	if v == nil {
		return
	}
	onMain(func() {
		c := v.live()
		// The content installs the hook as it builds its first native view,
		// and the state goes away as the window lets it go: nil is a
		// content that shows no native view, where nothing shows the view
		// and there is no keyboard for it to take, and a window that
		// closed.
		b := nativeViewBridge(c)
		if b == nil || b.RequestFocus == nil {
			return
		}
		b.RequestFocus(v, back)
	})
}

// IsDestroyed reports whether the view was destroyed, or its window
// closed.
func (v *NativeView) IsDestroyed() bool {
	return v == nil || v.destroyed.Load()
}

// SurfaceNativeView returns the platform view for package ui to place, nil
// once destroyed, for a connection that shows no window, and for the content
// of another window, which is logged once. Main thread only.
func (v *NativeView) SurfaceNativeView(conn *surface.Conn) platform.NativeView {
	if v == nil || v.w == nil || v.destroyed.Load() || v.native == nil {
		return nil
	}
	if !v.ownsConn(conn) {
		return nil
	}
	return v.native
}

// SurfaceNativeViewHost returns the platform surface that hosts the view, for
// the content of the view's own window to place it through, and nil for every
// other connection, for a view that was destroyed, and for a window that
// closed. Content reaches native-view hosting through it rather than through
// its own surface, so that hosting a native view stays an opt-in of the app
// that shows one: the platform's hosting code is reachable from a native view
// alone (AGENTS.md: apps pay only for what they use). Main thread only.
func (v *NativeView) SurfaceNativeViewHost(conn *surface.Conn) platform.NativeViewSurface {
	if v == nil || v.w == nil || v.destroyed.Load() || v.w.destroyed.Load() || v.provider == nil {
		return nil
	}
	if !v.ownsConn(conn) {
		return nil
	}
	return v.provider
}

// ownsConn reports whether conn is the connection of the view's own window.
// The content of another window showing the view is logged once, and joins
// nothing. Main thread only.
func (v *NativeView) ownsConn(conn *surface.Conn) bool {
	if conn == nil {
		return false
	}
	if host, _ := conn.Window.(*Window); host != v.w {
		if !v.misplaced {
			v.misplaced = true
			id := 0
			if host != nil {
				id = host.ID()
			}
			log.Printf("mygo: the content of window %d shows a native view of window %d", id, v.w.ID())
		}
		return false
	}
	return true
}

// live returns the conn of the view's window, nil once the view or its
// window is gone, or the view never was registered with it: the callbacks
// the view's adapter calls stop there.
func (v *NativeView) live() *surface.Conn {
	if v == nil || v.destroyed.Load() || v.native == nil || v.w == nil || v.w.destroyed.Load() {
		return nil
	}
	return v.w.conn
}

// nativeViewBridge returns the native view state of the connection, nil
// while the window's app made no native view and the content shows none.
// Main thread only.
func nativeViewBridge(c *surface.Conn) *surface.NativeViewBridge {
	if c == nil {
		return nil
	}
	b, _ := c.NativeViews.(*surface.NativeViewBridge)
	return b
}

// tookFocus reports that the view took the keyboard. Safe from any
// goroutine.
func (v *NativeView) tookFocus() {
	onMain(func() {
		if b := nativeViewBridge(v.live()); b != nil && b.ReportFocus != nil {
			b.ReportFocus(v)
		}
	})
}

// tabOut reports that Tab moved the keyboard out of the view. Safe from any
// goroutine.
func (v *NativeView) tabOut(back bool) {
	onMain(func() {
		if b := nativeViewBridge(v.live()); b != nil && b.ReportTabOut != nil {
			b.ReportTabOut(v, back)
		}
	})
}

// pointerDown reports that the pointer went down on the view. Safe from any
// goroutine.
func (v *NativeView) pointerDown(x, y float64) {
	onMain(func() {
		if b := nativeViewBridge(v.live()); b != nil && b.ReportPress != nil {
			b.ReportPress(v, x, y)
		}
	})
}

// destroyNativeViews destroys the native views a closing window's content
// still owns, which the state's Detach hook calls before the content hears
// that the window closed. Main thread only.
func destroyNativeViews(b *surface.NativeViewBridge) {
	windows.Lock()
	views := append([]any(nil), b.Views...)
	windows.Unlock()
	for _, x := range views {
		if v, ok := x.(*NativeView); ok {
			v.Destroy()
		}
	}
}
