package mygo

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// NativeView is a platform view owned by a window showing native UI.
// ui.HostView places it in a layout. Its methods are safe from any
// goroutine; native hooks always run on the main thread.
//
// A view stays alive while its element is omitted (for example in an
// inactive tab), and closes with Destroy or its window. It can be placed
// only once per frame, in its owning window.
type NativeView struct {
	w    *Window
	opts NativeViewOptions
	// The following fields are main-thread only.
	host      platform.NativeViewHost
	view      uintptr
	place     platform.NativeViewPlacement
	destroyed atomic.Bool
}

// NativeViewOptions describes an adapter for a compatible platform control.
type NativeViewOptions struct {
	// Create runs once with a hidden Parent container, on the main thread.
	// Return an unattached, owned (+1) NSView on macOS, a new parentless
	// GTK 3 widget (floating or owned) on Linux, or a child HWND created
	// with Parent on Windows. A successful Attach transfers ownership to
	// MyGo, which releases/destroys the view. On any creation/attachment
	// error the adapter owns cleanup; Dispose is not called.
	// Never return a shared view, reparent it, or destroy it yourself.
	Create func(NativeViewContext) (uintptr, error)
	// Update runs after each placement of the element, and once when it is
	// omitted and hidden. It can synchronize app state and appearance.
	Update func(NativeViewContext)
	// Dispose runs once, before MyGo destroys the view, even when the
	// window closes. Disconnect callbacks and release auxiliary resources
	// here; the returned view and Parent are still valid and owned by MyGo.
	Dispose func(NativeViewContext)
	// Focus optionally selects the entry control of a compound view, on
	// the main thread. The default focuses the view (or a GTK descendant).
	Focus func(NativeViewContext, bool)
	// ManagesTab leaves Tab to the native control. By default each hosted
	// subtree is one MyGo tab stop. An adapter managing internal traversal
	// calls NativeView.MoveFocus at its boundary.
	ManagesTab bool
	// Message handles Windows messages sent to Parent, including
	// WM_COMMAND/WM_NOTIFY from child controls. Return handled=false to
	// use the host's default processing. Runs on the main thread.
	Message func(NativeViewContext, NativeViewMessage) (result uintptr, handled bool)
}

// NativeViewMessage is a Win32 message sent to a native view's Parent.
// Pointer-valued parameters are valid only during the Message hook.
type NativeViewMessage struct {
	Message        uint32
	WParam, LParam uintptr
}

// NativeViewRect is a rectangle in DIPs, relative to the MyGo surface,
// with its origin at the top left. Native controls use their toolkit's
// local coordinates; MyGo translates their geometry itself.
type NativeViewRect struct{ X, Y, Width, Height float64 }

// NativeViewContext is a snapshot for a native hook. Handles may be used
// only during a hook; do not retain the context for native calls later.
// Use NativeView.Update to make later calls safely on the main thread.
type NativeViewContext struct {
	// Platform is "darwin", "linux", "windows", or "fake" in tests.
	Platform string
	// Parent is MyGo's clipping NSView, GTK 3 GtkLayout, or child HWND.
	// View is the adopted control's handle (zero during Create).
	Parent, View          uintptr
	Bounds, VisibleBounds NativeViewRect
	Scale                 float64
	Visible, Enabled      bool
}

// NewNativeView creates a platform control for ui.HostView. The window
// must show WindowOptions.Content. Creation happens on the main thread;
// no native handle is exposed through a goroutine-unsafe getter.
func (w *Window) NewNativeView(opts NativeViewOptions) (*NativeView, error) {
	if opts.Create == nil {
		return nil, errors.New("mygo: NativeViewOptions.Create is required")
	}
	if w.content == nil {
		return nil, errors.New("mygo: native views require WindowOptions.Content")
	}
	var v *NativeView
	err := errDestroyed
	onMain(func() {
		if w.native == nil || w.conn == nil {
			return
		}
		v = &NativeView{w: w, opts: opts}
		v.place.Enabled = true
		v.host, err = backend().NewNativeView(w.conn.Surface, (*nativeViewHandler)(v))
		if err != nil {
			v = nil
			return
		}
		w.nativeViews = append(w.nativeViews, v)
		view, createErr := opts.Create(v.context())
		if v.destroyed.Load() || w.native == nil {
			v, err = nil, errDestroyed
			return
		}
		if createErr == nil && view == 0 {
			createErr = errors.New("Create returned a zero native view")
		}
		if createErr == nil {
			createErr = v.host.Attach(view)
		}
		if createErr != nil {
			v.host.Close()
			v, err = nil, fmt.Errorf("mygo: native view: %w", createErr)
			return
		}
		v.view = view
		w.contentChanged()
	})
	return v, err
}

func nativeRect(r platform.RectF) NativeViewRect { return NativeViewRect{r.X, r.Y, r.W, r.H} }

func (v *NativeView) context() NativeViewContext {
	_, _, scale := v.w.conn.Surface.Size()
	return NativeViewContext{
		Platform: backend().Name(), Parent: v.host.Parent(), View: v.view,
		Bounds: nativeRect(v.place.Bounds), VisibleBounds: nativeRect(v.place.Clip),
		Scale: scale, Visible: v.place.Visible, Enabled: v.place.Enabled,
	}
}

// Window returns the owning window.
func (v *NativeView) Window() *Window { return v.w }

// Update runs fn with the live view on the main thread and requests a
// layout frame. It returns after fn; a destroyed view returns an error.
// fn must not block waiting for work that needs the main thread.
func (v *NativeView) Update(fn func(NativeViewContext)) error {
	err := errDestroyed
	onMain(func() {
		if !v.destroyed.Load() && v.view != 0 {
			if fn != nil {
				fn(v.context())
			}
			v.w.contentChanged()
			err = nil
		}
	})
	return err
}

// Focus gives a visible, enabled hosted element the keyboard. Focus on
// an unplaced, clipped out, disabled, or destroyed view does nothing.
func (v *NativeView) Focus() {
	onMain(func() {
		if !v.destroyed.Load() && v.place.Visible && v.place.Enabled {
			v.send(platform.NativeViewFocus, false)
		}
	})
}

// MoveFocus transfers focus out of a control that ManagesTab, to the
// next MyGo tab stop (the previous one when backward is true).
func (v *NativeView) MoveFocus(backward bool) {
	onMain(func() {
		if !v.destroyed.Load() && v.place.Visible && v.place.Enabled {
			v.send(platform.NativeViewTraverse, backward)
		}
	})
}

// Destroy disposes the native view once and removes it from its window.
// A layout still placing it shows an empty box.
func (v *NativeView) Destroy() {
	onMain(func() {
		if v.host != nil && !v.destroyed.Load() {
			v.host.Close()
		}
	})
}

// IsDestroyed reports whether Destroy ran or the owning window closed.
func (v *NativeView) IsDestroyed() bool { return v.destroyed.Load() }

// PlaceNativeView is the main-thread connection used by ui.HostView.
func (v *NativeView) PlaceNativeView(conn *surface.Conn, p platform.NativeViewPlacement) uintptr {
	var handle uintptr
	onMain(func() { handle = v.placeNativeView(conn, p) })
	return handle
}

func (v *NativeView) placeNativeView(conn *surface.Conn, p platform.NativeViewPlacement) uintptr {
	if v == nil || v.destroyed.Load() || conn == nil || conn.Window != v.w {
		return 0
	}
	p.ManagesTab = v.opts.ManagesTab
	v.place = p
	v.host.Place(p)
	if v.opts.Update != nil {
		v.opts.Update(v.context())
	}
	if !p.Visible || v.destroyed.Load() {
		return 0
	}
	return v.host.Parent()
}

// FocusNativeView is the main-thread connection used by ui.HostView.
func (v *NativeView) FocusNativeView(conn *surface.Conn, focused, backward bool) {
	onMain(func() { v.focusNativeView(conn, focused, backward) })
}

func (v *NativeView) focusNativeView(conn *surface.Conn, focused, backward bool) {
	if v == nil || v.destroyed.Load() || conn == nil || conn.Window != v.w {
		return
	}
	if !focused {
		v.host.Blur()
		return
	}
	if v.place.Visible && v.place.Enabled {
		if v.opts.Focus != nil {
			v.opts.Focus(v.context(), backward)
		} else {
			v.host.Focus(backward)
		}
	}
}

func (v *NativeView) send(kind platform.SurfaceEventKind, backward bool) {
	if v.w.conn == nil || v.w.conn.Event == nil || v.place.ID == 0 {
		return
	}
	var mods platform.Modifiers
	if backward {
		mods = platform.ModShift
	}
	v.w.conn.Event(platform.SurfaceEvent{Kind: kind, ID: v.place.ID, Mods: mods})
}

type nativeViewHandler NativeView

// CommandNativeView is the main-thread Edit menu connection used by ui.
func (v *NativeView) CommandNativeView(conn *surface.Conn, command string) bool {
	var taken bool
	onMain(func() {
		taken = v != nil && !v.destroyed.Load() && conn != nil && conn.Window == v.w && v.host.Command(command)
	})
	return taken
}

func (h *nativeViewHandler) Message(message uint32, wp, lp uintptr) (uintptr, bool) {
	v := (*NativeView)(h)
	if !v.destroyed.Load() && v.view != 0 && v.opts.Message != nil {
		return v.opts.Message(v.context(), NativeViewMessage{message, wp, lp})
	}
	return 0, false
}

func (h *nativeViewHandler) Focused() {
	v := (*NativeView)(h)
	if !v.destroyed.Load() && v.place.Visible && v.place.Enabled {
		v.send(platform.NativeViewFocus, false)
	}
}
func (h *nativeViewHandler) Traverse(backward bool) { (*NativeView)(h).MoveFocus(backward) }
func (h *nativeViewHandler) Closing() {
	v := (*NativeView)(h)
	if v.destroyed.Swap(true) {
		return
	}
	if v.view != 0 && v.opts.Dispose != nil {
		v.opts.Dispose(v.context())
	}
	v.view, v.host = 0, nil
	v.opts = NativeViewOptions{}
	for i, x := range v.w.nativeViews {
		if x == v {
			v.w.nativeViews = append(v.w.nativeViews[:i:i], v.w.nativeViews[i+1:]...)
			break
		}
	}
	v.w.contentChanged()
}

func (w *Window) destroyNativeViews() {
	for len(w.nativeViews) > 0 {
		i := len(w.nativeViews) - 1
		v := w.nativeViews[i]
		w.nativeViews = w.nativeViews[:i]
		v.Destroy()
	}
}
