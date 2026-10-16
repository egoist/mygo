//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/egoist/mygo/internal/platform"
)

// A native view in a window of native UI (platform.NativeViewSurface) is
// the app's own GtkWidget, made by the host, shown where the content
// draws it. Two layouts of mygo's hold it, in the layout under the
// surface's area that the first view of the window puts where the area
// was (makeLayers): wrap is the part of the view the content shows (its
// Clip), and parent is the view's own box (its Frame), inside wrap, which
// the app's control is added to. The wrap's window is thus the rectangle
// the view shows in, and a frame the content slides under its clip moves
// the control without resizing it: an app-owned control keeps what it
// draws and where it draws it inside its own frame. The windowed layouts
// also give a control that has no GdkWindow of its own one, which is what
// orders the view among the web views and the other native views
// (NativeViewPlacement.Order, orderViews) and what the area's input shape
// lets the pointer through to (shapeInput).
//
// The app's control is the app's. MyGo adds it to the parent it made, and
// takes it out again before the adapter destroys it (Destroy), so GTK
// never destroys a widget the app still owns, and MyGo destroys only its
// own layouts. A factory that fails, or a handle MyGo does not host,
// takes MyGo's layouts down and leaves the adapter to the core, which
// destroys it.
//
// Every reference MyGo and GTK hold on each layout, and who drops it
// (g_object_ref_sink/unref of GTK's "floating" references):
//
//	gtk_layout_new			floating: one reference, no owner
//	ownLayout(wrap), (parent)	MyGo sinks it and holds that reference
//	gtk_layout_put(wrap, parent)	wrap's container adds its own
//	gtk_layout_put(web, wrap)	web's container adds its own
//
// While the view is up, each layout has two references: the container's
// and MyGo's. drop takes the view down: gtk_widget_destroy unparents the
// layout, which drops the container's reference, and g_object_unref drops
// MyGo's, which frees the widget. MyGo unrefs only what it owns, so a
// layout it made but never put into a container (an earlier failure, or a
// host that closed while the factory ran) has only MyGo's to drop. GTK
// owns what gtk_layout_put added, and MyGo never sinks a layout a
// container already owns: that would be a reference nobody drops.

var (
	gtkWidgetGetType       func() uintptr
	gtkWindowGetType       func() uintptr
	gtkWindowGetFocus      func(w ptr) ptr
	gtkWidgetInDestruction func(w ptr) bool
	// gtkSocketGetType and gtkPlugGetType are optional (refusedWidget):
	// a GTK without them has no such widget to refuse. gtkGLAreaGetType
	// is the surface's own binding (surface_access.go).
	gtkSocketGetType  func() uintptr
	gtkPlugGetType    func() uintptr
	gTypeName         func(t uintptr) ptr
	nativeViewsLoaded bool
)

// ownLayout takes MyGo's own reference to a layout gtk_layout_new just
// made, which is floating: after this MyGo owns one reference to w, and
// every path out of NewNativeView drops it exactly once, put into a
// container or not (drop).
func ownLayout(w ptr) ptr {
	if w != 0 {
		gObjectRefSink(w)
	}
	return w
}

// nativeView is the platform side of one native view: the adapter the
// app's factory made, and the layouts MyGo holds its control in.
type nativeView struct {
	host    *window
	surface *surface
	adapter platform.NativeView
	// wrap holds the part of the view the content shows, and clips it;
	// parent is the view's box inside wrap, which the app's control is
	// added to; widget is that control.
	wrap, parent, widget ptr
	closed               bool
}

// nativePlacement is the native-view extension of a surface: everything
// the surface holds of the feature. The surface reaches it only through
// its ext field, as a surfacePlacement (surface.go, webview.go), so that
// the placement code of a surface, which every app links, names no native
// type. Only a native view makes one (newNativeView, through placement),
// and only the provider of this backend reaches that.
type nativePlacement struct {
	s      *surface
	placed []platform.NativeViewPlacement
	views  []placedNative
}

// extOf returns the native-view extension of the surface, nil until it
// makes one.
func (s *surface) extOf() *nativePlacement {
	p, _ := s.ext.(*nativePlacement)
	return p
}

// placement returns the native-view extension of the surface, making it
// on first use: it is where the feature keeps the surface's native views
// and their placements, and installing it is what makes the surface
// place, shape, order and expose the app's own controls (webview.go).
func (s *surface) placement() *nativePlacement {
	if p := s.extOf(); p != nil {
		return p
	}
	p := &nativePlacement{s: s}
	s.ext = p
	return p
}

// placedNative is what the placing, shaping and ordering code needs of a
// native view: the view's clip and window, its order and its accessible.
// That code names this interface, never the concrete view, and reaches it
// with an interface-to-interface assertion on a placement's View, which
// names no implementation type: an app that makes no native view links
// none of it (AGENTS.md: apps pay only for what they use).
type placedNative interface {
	platform.NativeView
	// owner is the window the view belongs to.
	owner() *window
	// alive tells that the view was not destroyed.
	alive() bool
	// shows tells that the view's clip is up and visible.
	shows() bool
	// viewWindow is the GdkWindow of the view's clip, zero until it is
	// realized.
	viewWindow() ptr
	// control is the app's control, zero once the view is gone.
	control() ptr
	// place puts the view at a placement's clip and frame.
	place(c, f platform.RectF)
	// hasKeyboard tells that the view's control holds the keyboard.
	hasKeyboard() bool
	// hide takes the view's clip out of the content's sight, keeping the
	// view's state.
	hide()
}

func loadNativeViews() {
	if nativeViewsLoaded {
		return
	}
	nativeViewsLoaded = true
	mustBind(libGTK, &gtkWidgetGetType, "gtk_widget_get_type")
	mustBind(libGTK, &gtkWindowGetType, "gtk_window_get_type")
	mustBind(libGTK, &gtkWindowGetFocus, "gtk_window_get_focus")
	mustBind(libGTK, &gtkWidgetInDestruction, "gtk_widget_in_destruction")
	mustBind(libGObject, &gTypeName, "g_type_name")
	// The types of the controls MyGo refuses (refusedWidget), where the
	// GTK has them: GtkGLArea came in GTK 3.16, and the surface's own
	// binding of it may already have run.
	bind(libGTK, &gtkGLAreaGetType, "gtk_gl_area_get_type")
	bind(libGTK, &gtkSocketGetType, "gtk_socket_get_type")
	bind(libGTK, &gtkPlugGetType, "gtk_plug_get_type")
}

// nativeViewSurface is the platform.NativeViewSurface of one of this
// backend's surfaces: the provider the core makes and shows the app's
// native views through. It is a provider of its own, not the surface: a
// window's content is held as an interface (mygo.Conn.Surface, and the
// core's own assertion of it), and Go keeps every exported method of a
// type used that way, so a surface that implemented the native-view
// platform interface would keep this whole implementation in every
// linked webview-only app (AGENTS.md: apps pay only for what they use).
// Everything below is reached only from here through its unexported
// flow, so NativeViewSurface is the one link the core's native-view
// feature has to it.
type nativeViewSurface struct {
	s *surface
}

// NativeViewSurface returns the native-view provider of s, when s is one
// of this backend's surfaces, and false for any other surface. Calling
// it is the only way the core's native-view feature reaches the
// implementation in this file: the core's build-tagged free constructor
// uses it, and an app that makes no native view never calls it, so the
// linker drops the provider and the GTK bindings, widgets and types under
// it. Preserving the surface's own errors is deliberate: a surface whose
// window is gone still yields a provider, and making a view in it reports
// the window gone, as it did before the provider existed.
//
// The wrapper implements platform.NativeViewSurface by being returned as
// one. A "var _ platform.NativeViewSurface = (*nativeViewSurface)(nil)"
// assertion would keep the interface and the provider linked in every
// app, including the webview-only path this exists to keep them out of.
func NativeViewSurface(s platform.Surface) (platform.NativeViewSurface, bool) {
	ls, ok := s.(*surface)
	if !ok || ls == nil {
		return nil, false
	}
	return &nativeViewSurface{s: ls}, true
}

// NewNativeView makes the platform side of a native view: the parent the
// app's factory adds its control to, under the surface's area. Main
// thread only.
func (n *nativeViewSurface) NewNativeView(make func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	return newNativeView(n.s, make)
}

// PlaceNativeViews shows the window's native views where the content
// shows them, in its paint order under the surface among the web views,
// and hides the others, which keep their state. Main thread only.
func (n *nativeViewSurface) PlaceNativeViews(views []platform.NativeViewPlacement) {
	s := n.s
	if host := s.w; host == nil || host.closed || s.layers == 0 {
		return
	}
	p := s.placement()
	p.placed = append(p.placed[:0], views...)
	p.placeViews()
}

// TakeFocus gives the content the keyboard back from a native view, as
// Tab leaving its last control does. A view that has it keeps it: taking
// the keyboard of a page, or of another window, would be mygo's own
// doing, not the content's.
func (n *nativeViewSurface) TakeFocus() {
	if p := n.s.extOf(); p != nil {
		p.takeFocus()
	}
}

// newNativeView makes the platform side of a native view: the parent the
// app's factory adds its control to, under the surface's area. It is the
// constructor of the feature: it installs the surface's native-view
// extension, which the surface places, shapes, orders and exposes the
// app's own controls through from then on. Main thread only.
func newNativeView(s *surface, make func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	loadLayers()
	loadNativeViews()
	host := s.w
	if host == nil || host.closed || host.win == 0 || s.area == 0 {
		return nil, errors.New("mygo: linux: the window of the native view is gone")
	}
	if s.layers == 0 {
		// The view goes under the surface's area, in the layout of the
		// views under it (webview.go).
		s.makeLayers()
	}
	if s.web == 0 {
		return nil, errors.New("mygo: linux: the window of the native view has no layout")
	}
	p := s.placement()
	v := &nativeView{host: host, surface: s}
	// Both layouts are MyGo's own references from here on (ownLayout), so
	// every path out of this function that does not hand the view to the
	// window's list drops them, put into a container or not, and nothing
	// else unrefs them.
	v.wrap, v.parent = ownLayout(gtkLayoutNew(0, 0)), ownLayout(gtkLayoutNew(0, 0))
	if v.wrap == 0 || v.parent == 0 {
		v.drop()
		return nil, errors.New("mygo: linux: gtk_layout_new made no layout: is there a display?")
	}
	// Hidden until the content shows them, and no size of their own: the
	// placements size them.
	gtkWidgetSetNoShowAll(v.wrap, true)
	gtkWidgetSetNoShowAll(v.parent, true)
	gtkWidgetSetSizeRequest(v.wrap, 1, 1)
	gtkWidgetSetSizeRequest(v.parent, 1, 1)
	gtkLayoutPut(v.wrap, v.parent, 0, 0)
	gtkLayoutPut(s.web, v.wrap, 0, 0)
	a, err := make(platform.NativeViewHost{
		Platform: "linux",
		Parent:   uintptr(v.parent),
		Scale:    float64(max(gtkWidgetGetScaleFactor(s.area), 1)),
	})
	if err != nil {
		// The core destroys the adapter a factory that failed after
		// making one returned (nativeview.go): MyGo takes only its own
		// layouts down.
		v.drop()
		return nil, err
	}
	if a == nil {
		v.drop()
		return nil, errors.New("mygo: linux: the native view's factory made none")
	}
	if host.closed || host.win == 0 || s.area == 0 || s.web == 0 {
		// The factory runs the app's code, and that may close the window
		// it was made for (or destroy its surface) before it returns:
		// the window's teardown took MyGo's layouts with it, and MyGo
		// adds no view of a window that is gone. It drops only its own
		// layouts: the core destroys the adapter of the failed creation,
		// and the app's control, if the window did not already destroy
		// it, is the app's adapter to destroy.
		v.drop()
		return nil, errors.New("mygo: linux: the window of the native view closed while its factory ran")
	}
	v.adapter = a
	h := a.NativeHandle()
	if host.closed || host.win == 0 || s.area == 0 || s.web == 0 {
		// NativeHandle is the app's code as much as the factory is, and
		// reading it may close the window the view was made for (or
		// destroy its surface) before it returns: MyGo attaches its
		// control to nothing then. It drops only its own layouts, and the
		// core destroys the adapter of the failed creation.
		v.drop()
		return nil, errors.New("mygo: linux: the window of the native view closed while its handle was read")
	}
	switch {
	case h == 0:
		v.drop()
		return nil, errors.New("mygo: linux: the native view's factory made no GtkWidget")
	default:
		if err := v.attach(h); err != nil {
			v.drop()
			return nil, err
		}
	}
	if host.closeNativeViews == nil {
		// The window's teardown closes its native views through this hook
		// (cleanup, window.go): a window that hosts none never calls any
		// part of the feature.
		host.closeNativeViews = p.closeViews
	}
	p.views = append(p.views, v)
	return v, nil
}

// attach adds the app's control to the parent MyGo made for it, or checks
// the one the factory added itself, and refuses a control MyGo does not
// host. A widget of another parent's, or of another window's, stays
// there: MyGo does not take a control from the app's own tree.
func (v *nativeView) attach(h ptr) error {
	switch {
	case !gTypeCheckInstanceIsA(h, gtkWidgetGetType()):
		return fmt.Errorf("mygo: linux: the handle %#x of the native view is a %s, not a GtkWidget", h, typeName(h))
	case gTypeCheckInstanceIsA(h, gtkWindowGetType()):
		return fmt.Errorf("mygo: linux: the native view %#x is a GtkWindow: the content shows a control, not a window", h)
	case h == v.surface.area || h == v.surface.web || h == v.surface.layers || h == v.host.win:
		return fmt.Errorf("mygo: linux: the native view %#x is a widget of mygo's own", h)
	}
	if why := refusedWidget(h); why != "" {
		return fmt.Errorf("mygo: linux: the native view %#x is a %s: %s", h, typeName(h), why)
	}
	switch p := gtkWidgetGetParent(h); {
	case p == 0:
		// The factory left the control to MyGo to add where it made the
		// room for it.
		gtkLayoutPut(v.parent, h, 0, 0)
	case inside(v.parent, h):
		// The factory added its control to the parent MyGo gave it, on
		// its own or in a container of its own: it stays where it is.
	default:
		return fmt.Errorf("mygo: linux: the native view %#x is already a child of another widget: mygo does not take a control from another parent", h)
	}
	v.widget = h
	return nil
}

// refusedWidget is why MyGo does not show a control of the type of h in a
// native view, "" for one it hosts. MyGo puts the control in its own
// layouts under the surface's area, where the content draws and clips it,
// and that only holds for a control that draws in the widget hierarchy:
// one that draws into a surface of its own, or shows a window that is not
// this window's, is refused rather than hosted with clip guarantees that
// would not hold.
//
//   - A GtkGLArea draws with OpenGL in a GL surface of its own, under the
//     drawing of the window.
//   - A GtkSocket shows another process's window, which the window's own
//     drawing and input shape do not reach.
//   - A GtkPlug is another window's own widget, so its GdkWindow is not in
//     this window's hierarchy.
//
// g_type_check_instance_is_a is the check, so a subclass of a refused type
// is refused too, whatever its own type name says. That list is all MyGo
// claims to know: a control that leaves the widget hierarchy by other
// means (a foreign GdkWindow the app set on it, or a toolkit with a
// surface of its own) is not caught here, is hosted, and gets no clipping
// guarantee from the content.
func refusedWidget(h ptr) string {
	for _, r := range []struct {
		getType func() uintptr
		why     string
	}{
		{gtkGLAreaGetType, "mygo does not host a control that draws with OpenGL in a surface of its own"},
		{gtkSocketGetType, "mygo does not host a control that shows another process's window"},
		{gtkPlugGetType, "mygo does not host a control that is another window's own widget"},
	} {
		if r.getType == nil {
			continue
		}
		if t := r.getType(); t != 0 && gTypeCheckInstanceIsA(h, t) {
			return r.why
		}
	}
	return ""
}

// inside reports whether w is somewhere inside parent.
func inside(parent, w ptr) bool {
	for p := gtkWidgetGetParent(w); p != 0; p = gtkWidgetGetParent(p) {
		if p == parent {
			return true
		}
	}
	return false
}

// typeName returns the name of the GType of an instance, for errors.
func typeName(o ptr) string {
	return goStr(ptr(gTypeName(instanceType(o))))
}

// instanceType returns the GType of an instance: GTypeInstance.g_class,
// then GTypeClass.g_type.
func instanceType(o ptr) uintptr {
	return field[uintptr](field[ptr](o, 0), 0)
}

// The placedNative interface: what the always-linked placement code and
// the surface ask of a view, without naming its type.

// owner is the window the view belongs to.
func (v *nativeView) owner() *window { return v.host }

// alive tells that the view was not destroyed.
func (v *nativeView) alive() bool { return !v.closed }

// shows tells that the view's clip is up and visible.
func (v *nativeView) shows() bool {
	return !v.closed && v.wrap != 0 && gtkWidgetGetVisible(v.wrap)
}

// viewWindow is the GdkWindow of the view's clip, zero until it is
// realized.
func (v *nativeView) viewWindow() ptr {
	if v.wrap == 0 {
		return 0
	}
	return gtkWidgetGetWindow(v.wrap)
}

// control is the app's control, zero once the view is gone.
func (v *nativeView) control() ptr {
	if v.closed {
		return 0
	}
	return v.widget
}

// NativeHandle returns the app's control, or zero once the view is gone.
func (v *nativeView) NativeHandle() uintptr {
	if v.closed {
		return 0
	}
	return uintptr(v.widget)
}

// Focus gives the view the keyboard, as the adapter does: putting it at
// its control's first entry, or its last going back, is the control's own
// to know.
func (v *nativeView) Focus(back bool) {
	if v.closed || v.adapter == nil {
		return
	}
	v.adapter.Focus(back)
}

// Destroy takes a native view down: the app's control leaves MyGo's
// parents, the adapter destroys it, and MyGo's own layouts go. Main
// thread only, and once.
func (v *nativeView) Destroy() {
	if v.closed {
		return
	}
	keyboard := v.hasKeyboard()
	v.closed = true
	host, s := v.host, v.surface
	var p *nativePlacement
	if s != nil {
		p = s.extOf()
	}
	if p != nil {
		p.placed = slices.DeleteFunc(p.placed, func(pl platform.NativeViewPlacement) bool {
			return pl.View == platform.NativeView(v)
		})
		p.views = slices.DeleteFunc(p.views, func(x placedNative) bool { return x == placedNative(v) })
	}
	if keyboard && host != nil && !host.closed && s != nil && s.area != 0 {
		// A view that goes keeps no keyboard: it goes back to the
		// content.
		gtkWidgetGrabFocus(s.area)
	}
	v.drop()
	if v.adapter != nil {
		v.adapter.Destroy()
	}
	// The adapter is the app's code, and destroying it may close the
	// window, whose teardown takes the surface down with it (cleanup sets
	// closed before it calls here): only a window still up has views to
	// order and an input shape to set.
	if p != nil && host != nil && !host.closed && s.area != 0 && s.web != 0 {
		p.orderViews()
		p.shapeInput()
	}
}

// drop takes MyGo's own layouts down, taking the app's control out of
// them first, so that GTK cannot destroy a widget the app owns: its
// adapter destroys it. It drops exactly the references ownLayout took,
// put into a container or not, and nothing else.
func (v *nativeView) drop() {
	if v.parent != 0 {
		// A box that the window is already destroying destroyed its
		// children itself (gtk_widget_in_destruction): its child list is
		// not MyGo's to walk any more, and whatever was in it is gone
		// with the window.
		if !gtkWidgetInDestruction(v.parent) {
			if list := gtkContainerGetChildren(v.parent); list != 0 {
				for node := list; node != 0; node = field[ptr](node, 8) {
					gtkContainerRemove(v.parent, field[ptr](node, 0))
				}
				gListFree(list)
			}
		}
		gtkWidgetDestroy(v.parent) // unparents the box: its container drops its reference
		gObjectUnref(v.parent)     // MyGo's own reference, which frees the box
		v.parent = 0
	}
	v.widget = 0
	if v.wrap != 0 {
		gtkWidgetDestroy(v.wrap)
		gObjectUnref(v.wrap)
		v.wrap = 0
	}
}

// hosted returns the widget under the view's parent that MyGo sizes to
// the frame: the app's control, or the container the factory put it in.
func (v *nativeView) hosted() ptr {
	for w := v.widget; w != 0; {
		switch p := gtkWidgetGetParent(w); {
		case p == v.parent:
			return w
		case p == 0:
			return 0
		default:
			w = p
		}
	}
	return 0
}

// hasKeyboard reports whether the view's control has the keyboard: the
// window's focus widget is MyGo's layout of the view, or inside it. A
// window on its way out answers no: its widgets go with it.
func (v *nativeView) hasKeyboard() bool {
	if v.closed || v.host == nil || v.host.closed || v.host.win == 0 || v.wrap == 0 {
		return false
	}
	f := gtkWindowGetFocus(v.host.win)
	return f != 0 && (f == v.wrap || inside(v.wrap, f))
}

// hide takes the view's clip out of the content's sight, keeping the
// view's state: a hidden view keeps no keyboard, which goes back to the
// content.
func (v *nativeView) hide() {
	if v.hasKeyboard() {
		gtkWidgetGrabFocus(v.surface.area)
	}
	gtkWidgetHide(v.wrap)
}

// place shows the view's clip where the content shows it in the surface,
// and its control at the frame's place in that clip.
func (v *nativeView) place(c, f platform.RectF) {
	dx, dy := int32(math.Round(c.X)), int32(math.Round(c.Y))
	gtkLayoutMove(v.surface.web, v.wrap, dx, dy)
	gtkWidgetSetSizeRequest(v.wrap, int32(math.Round(c.X+c.W))-dx, int32(math.Round(c.Y+c.H))-dy)
	fx, fy := int32(math.Round(f.X)), int32(math.Round(f.Y))
	gtkLayoutMove(v.wrap, v.parent, fx-dx, fy-dy)
	gtkWidgetSetSizeRequest(v.parent, int32(math.Round(f.X+f.W))-fx, int32(math.Round(f.Y+f.H))-fy)
	if w := v.hosted(); w != 0 {
		// What the factory put in the parent fills the frame: MyGo sizes
		// the view, the app sizes what it built inside it.
		gtkWidgetSetSizeRequest(w, int32(math.Round(f.X+f.W))-fx, int32(math.Round(f.Y+f.H))-fy)
	}
	if !gtkWidgetGetVisible(v.parent) {
		// MyGo shows its own box, and its clip: whether the app's control
		// itself shows is the app's, as the app's view's is on darwin.
		// GTK maps the control with the box it is in.
		gtkWidgetShow(v.parent)
	}
	if !gtkWidgetGetVisible(v.wrap) {
		// Showing the clip maps its window over the ones already there,
		// whatever order the content painted them in: what the surface
		// remembers of the view windows no longer describes them (a view
		// that goes away and comes back is the same window, raised), and
		// ordering forgets it so that it puts the windows back
		// (orderViews). Showing a view that was never hidden is not this:
		// its window is already where ordering left it.
		v.surface.ordered = v.surface.ordered[:0]
		gtkWidgetShow(v.wrap)
	}
}

// placeViews shows the window's native views where the content shows
// them, in its paint order under the surface among the web views, and
// hides the others, which keep their state; the area's input shape lets
// the pointer through to them, and the windows of both kinds are ordered
// together. It is the placement of a surface that has a native view in
// it: the web views are placed first. Main thread only.
func (p *nativePlacement) placeViews() {
	s, host := p.s, p.s.w
	if host.closed || s.layers == 0 {
		return
	}
	s.placeWeb()
	shown := make([]placedNative, 0, len(p.placed))
	for _, pl := range p.placed {
		v, ok := pl.View.(placedNative)
		if !ok || v.owner() != host || !v.alive() {
			continue
		}
		if pl.Clip.W <= 0 || pl.Clip.H <= 0 {
			// No pixel of the view shows: the content shows nothing of
			// it, and the loop below hides it, keeping its state, as the
			// windows backend does (hideNative). Placing a clip of no
			// size would show a window of no size over the others.
			continue
		}
		v.place(pl.Clip, pl.Frame)
		shown = append(shown, v)
	}
	for _, v := range p.views {
		if !slices.Contains(shown, v) && v.shows() {
			// A hidden view keeps no keyboard: hide returns it to the
			// content.
			v.hide()
		}
	}
	p.orderViews()
	p.shapeInput()
}

// takeFocus gives the content the keyboard back from a native view, as
// Tab leaving its last control does. A view that has it keeps it: taking
// the keyboard of a page, or of another window, would be mygo's own
// doing, not the content's.
func (p *nativePlacement) takeFocus() {
	s, host := p.s, p.s.w
	if host.closed || host.win == 0 || s.area == 0 {
		return
	}
	for _, v := range p.views {
		if v.hasKeyboard() {
			gtkWidgetGrabFocus(s.area)
			return
		}
	}
}

// orderViews puts the windows of the views the content shows in the order
// it painted them, the web views and the native views merged: the first
// painted first, the last over the others. stackWindows does the ordering
// itself (webview.go).
func (p *nativePlacement) orderViews() {
	s := p.s
	entries := make([]windowOrder, 0, len(s.placed)+len(p.placed))
	shown := 0
	add := func(order int, w ptr) {
		shown++
		if w == 0 {
			// Not realized yet: the window it gets is mapped over the
			// ones there are, and the frame that realizes it orders it
			// then.
			return
		}
		entries = append(entries, windowOrder{order, w})
	}
	for _, pl := range s.placed {
		v, ok := pl.WebView.(*window)
		if !ok || v.host != s.w || v.closed || !gtkWidgetGetVisible(v.web) {
			continue
		}
		add(pl.Order, gtkWidgetGetWindow(v.web))
	}
	for _, pl := range p.placed {
		v, ok := pl.View.(placedNative)
		if !ok || v.owner() != s.w || !v.shows() {
			continue
		}
		add(pl.Order, v.viewWindow())
	}
	s.stackWindows(entries, shown)
}

// shapeThrough takes the pixels of clip out of region, and puts back the
// ones the content painted over it (covers): cairo has no union of a
// difference. Only the extension of a surface that made a native view
// shapes two kinds of view together, so only it links this (AGENTS.md: apps
// pay only for what they use).
func shapeThrough(region ptr, clip platform.RectF, covers []platform.RectF) {
	cairoRegionSubtractRectangle(region, gdkRect(clip))
	for _, c := range covers {
		r := gdkRect(c)
		cover := cairoRegionCreateRectangle(r)
		cairoRegionUnion(region, cover)
		cairoRegionDestroy(cover)
	}
}

// shapeInput lets the pointer through the area where the views under it
// take it: in their clips but where the content painted over them. One
// region covers the web views and the native views: a second would
// replace the first.
//
// The two lists are one of the content's: it numbers every view of a frame
// with one counter (WebViewPlacement.Order, NativeViewPlacement.Order) and
// gives each of them in that order (ui's webPlacements and frameViews), so
// they are merged here, and the views are shaped in the order the content
// painted them. That order is what the shape is: a view's covers put back
// what the content painted over it, and only a view painted after it may
// take those pixels away again. Walking one list after the other would let
// a view painted earlier put its covers back over a view painted later, and
// the surface would take a click on the overlap that is no view's: neither
// view hears it.
func (p *nativePlacement) shapeInput() {
	s := p.s
	win := gtkWidgetGetParentWindow(s.area) // the overlay's window of the area
	if win == 0 || win == gtkWidgetGetWindow(s.w.win) {
		return
	}
	var all gdkRectangle
	gtkWidgetGetAllocation(s.area, &all)
	region := cairoRegionCreateRectangle(&gdkRectangle{Width: all.Width, Height: all.Height})
	// The merge of the two lists, in the content's paint order: the web
	// view first where one and a native view share an order (the content
	// numbers them apart, so only a list of its own could).
	for wi, ni := 0, 0; wi < len(s.placed) || ni < len(p.placed); {
		if ni == len(p.placed) || wi < len(s.placed) && s.placed[wi].Order <= p.placed[ni].Order {
			shapeThrough(region, s.placed[wi].Clip, s.placed[wi].Covers)
			wi++
			continue
		}
		shapeThrough(region, p.placed[ni].Clip, p.placed[ni].Covers)
		ni++
	}
	gdkWindowInputShapeCombineRegion(win, region, 0, 0)
	cairoRegionDestroy(region)
}

// accessibles returns the accessibles of the native views the content
// shows, which the overlay of the surface holds under its area
// (surface_access.go).
func (p *nativePlacement) accessibles() []ptr {
	accessibles := make([]ptr, 0, len(p.placed))
	for _, pl := range p.placed {
		v, ok := pl.View.(placedNative)
		if !ok || !v.shows() {
			continue
		}
		c := v.control()
		if c == 0 {
			continue
		}
		if a := gtkWidgetGetAccessible(c); a != 0 {
			accessibles = append(accessibles, a)
		}
	}
	return accessibles
}

// closeViews destroys the native views of a window that closes. The
// provider that makes the first view of a window installs it in the
// window's closeNativeViews hook (window.go), so that the teardown of a
// window that hosts none calls nothing of the feature.
func (p *nativePlacement) closeViews() {
	for len(p.views) > 0 {
		p.views[len(p.views)-1].Destroy()
	}
}
