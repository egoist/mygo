//go:build linux && (amd64 || arm64)

package linux

import (
	"math"
	"slices"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

// A web view in a window of native UI (platform.Surface.NewWebView) is a
// window of this package without a GtkWindow of its own: win is its
// host's, and its WebKitWebView is a child of a GtkLayout under the
// surface's area, in a GtkOverlay that the first web view puts where the
// area was (the area is then an overlay of it, which GTK realizes again,
// with a new GL context). The area keeps an alpha channel from then on:
// its frames are transparent where the content shows a web view (holes),
// so that what the content paints after it shows over the page, and an
// input shape lets the pointer through to the page there. Its signals
// find it by its id in Backend.webViews, apart from the windows.

var (
	gtkLayoutNew                     func(h, v ptr) ptr
	gtkLayoutPut                     func(l, child ptr, x, y int32)
	gtkLayoutMove                    func(l, child ptr, x, y int32)
	gtkWidgetGetParent               func(w ptr) ptr
	gtkWidgetGetParentWindow         func(w ptr) ptr
	gdkWindowInputShapeCombineRegion func(w, region ptr, x, y int32)
	gdkWindowRestack                 func(w, sibling ptr, above bool)
	cairoRegionCreateRectangle       func(r *gdkRectangle) ptr
	cairoRegionSubtractRectangle     func(region ptr, r *gdkRectangle) int32
	cairoRegionUnion                 func(region, other ptr) int32
	cairoRegionDestroy               func(region ptr)
	gtkWidgetEvent                   func(w, event ptr) bool
	gtkContainerGetFocusChild        func(c ptr) ptr
	// layersLoaded is the native views' own binding flag (loadLayers):
	// no app that makes none links it, or the bindings under it.
	layersLoaded, webViewsLoaded bool

	// cbWebViewFocus handles a web view's focus signal.
	cbWebViewFocus ptr
)

// loadLayers binds what the native views of the surface's area are laid
// out with: the layout they go in, the area's input shape over them, and
// the restacking of their windows (nativeview.go). The web views bind the
// same calls for themselves (loadWebViews), so that an app that makes no
// native view links none of this, and the feature's own path keeps its
// binding without the web views' callback.
func loadLayers() {
	if layersLoaded {
		return
	}
	layersLoaded = true
	mustBind(libGTK, &gtkLayoutNew, "gtk_layout_new")
	mustBind(libGTK, &gtkLayoutPut, "gtk_layout_put")
	mustBind(libGTK, &gtkLayoutMove, "gtk_layout_move")
	mustBind(libGTK, &gtkWidgetGetParent, "gtk_widget_get_parent")
	mustBind(libGTK, &gtkWidgetGetParentWindow, "gtk_widget_get_parent_window")
	mustBind(libGDK, &gdkWindowInputShapeCombineRegion, "gdk_window_input_shape_combine_region")
	// The views are z-ordered among themselves with the windows their
	// layouts give them (stackWindows).
	mustBind(libGDK, &gdkWindowRestack, "gdk_window_restack")
	mustBind(libCairo, &cairoRegionCreateRectangle, "cairo_region_create_rectangle")
	mustBind(libCairo, &cairoRegionSubtractRectangle, "cairo_region_subtract_rectangle")
	mustBind(libCairo, &cairoRegionUnion, "cairo_region_union")
	mustBind(libCairo, &cairoRegionDestroy, "cairo_region_destroy")
}

func loadWebViews() {
	if webViewsLoaded {
		return
	}
	webViewsLoaded = true
	mustBind(libGTK, &gtkLayoutNew, "gtk_layout_new")
	mustBind(libGTK, &gtkLayoutPut, "gtk_layout_put")
	mustBind(libGTK, &gtkLayoutMove, "gtk_layout_move")
	mustBind(libGTK, &gtkWidgetGetParent, "gtk_widget_get_parent")
	mustBind(libGTK, &gtkWidgetGetParentWindow, "gtk_widget_get_parent_window")
	mustBind(libGDK, &gdkWindowInputShapeCombineRegion, "gdk_window_input_shape_combine_region")
	// The web views alone are not re-ordered among themselves: only the
	// extension of a surface that made a native view orders view windows
	// (stackWindows), and it binds gdk_window_restack itself (loadLayers).
	mustBind(libCairo, &cairoRegionCreateRectangle, "cairo_region_create_rectangle")
	mustBind(libCairo, &cairoRegionSubtractRectangle, "cairo_region_subtract_rectangle")
	mustBind(libCairo, &cairoRegionUnion, "cairo_region_union")
	mustBind(libCairo, &cairoRegionDestroy, "cairo_region_destroy")
	mustBind(libGTK, &gtkWidgetEvent, "gtk_widget_event")
	mustBind(libGTK, &gtkContainerGetFocusChild, "gtk_container_get_focus_child")
	// GTK moves the keyboard with Tab through focus signals, which
	// WebKit's Tab past the page's ends starts: the content takes the
	// keyboard back, and GTK moving it into a page by itself, as a window
	// shows, leaves it with the content.
	cbWebViewFocus = purego.NewCallback(func(widget ptr, direction int32, data ptr) bool {
		w := theBackend.window(data)
		if w == nil || w.host == nil {
			return false
		}
		const tabForward, tabBackward = 0, 1 // GtkDirectionType
		switch {
		case gtkWidgetHasFocus(w.web):
			if direction == tabForward || direction == tabBackward {
				w.tabOut(direction == tabBackward)
				return true
			}
			return false
		case gtkContainerGetFocusChild(w.web) != 0:
			return false // a dialog of WebKit's in the view has the keyboard
		}
		if s := w.host.surface; s != nil {
			gtkWidgetGrabFocus(s.area)
		}
		return true
	})
}

func (s *surface) NewWebView(o *platform.WindowOptions, h platform.WindowHandler) (platform.WebView, error) {
	if err := webKit(); err != nil {
		return nil, err
	}
	loadWebViews()
	host := s.w
	b := host.b
	if s.layers == 0 {
		s.makeLayers()
	}
	b.nextID++
	v := &window{b: b, id: b.nextID, h: h, opts: o, win: host.win, host: host}
	b.webViews[v.id] = v
	v.createWebView()
	connect(v.web, "focus", cbWebViewFocus, ptr(v.id))
	gtkWidgetSetNoShowAll(v.web, true) // hidden until placed
	gtkLayoutPut(s.web, v.web, 0, 0)
	b.byWebView[v.web] = v
	host.webViews = append(host.webViews, v)
	return v, nil
}

// makeLayers puts the surface's area over a layout holding the web views,
// in an overlay where the area was.
func (s *surface) makeLayers() {
	area := s.area
	parent := gtkWidgetGetParent(area)
	focused := gtkWidgetHasFocus(area)
	s.layers = newOverlay()
	s.web = gtkLayoutNew(0, 0)
	gtkContainerAdd(s.layers, s.web)
	gObjectRef(area)
	if parent != 0 {
		gtkContainerRemove(parent, area)
	}
	if s.gl {
		// Holes show the web views through the frames.
		gtkGLAreaSetHasAlpha(area, true)
	}
	gtkOverlayAddOverlay(s.layers, area)
	gObjectUnref(area)
	switch {
	case parent == 0:
	case parent == s.w.box:
		gtkBoxPackStart(parent, s.layers, true, true, 0)
		if s.w.menubar != 0 {
			gtkBoxReorderChild(parent, s.layers, 1) // under the menu bar
		}
	default: // the overlay of a hidden title bar's controls
		gtkContainerAdd(parent, s.layers)
	}
	gtkWidgetShowAll(s.layers)
	if focused {
		gtkWidgetGrabFocus(area)
	}
}

// surfacePlacement is the native-view extension of a surface: what the
// placement code of a surface, which every app links, asks of an
// implementation of native views without naming one (nativeview.go). A
// surface makes no extension until a native view is made in it, so a
// webview-only surface reaches none of this: the surface itself holds the
// state and the code of the feature nowhere (AGENTS.md: apps pay only for
// what they use). None of the methods names a native type; the extension
// holds the placements, the views and the GTK widgets of the app's
// controls.
type surfacePlacement interface {
	// placeViews shows the content's native views where it shows them,
	// with the web views, and hides the others.
	placeViews()
	// shapeInput lets the pointer through the area where the views under
	// it take it, web and native in the content's paint order.
	shapeInput()
	// orderViews puts the view windows in the content's paint order.
	orderViews()
	// accessibles returns the accessible of each native view the content
	// shows, for the overlay of the surface's area (surface_access.go).
	accessibles() []ptr
}

// PlaceWebViews shows the window's web views where the content shows
// them, in its paint order under the surface, and hides the others; the
// area's input shape lets the pointer through to them. A surface that made
// a native view hands the placement to its extension, which places the web
// views and the native views together, in the content's one paint order
// (nativeview.go). One that made none places its web views alone, with the
// code every app has always linked (AGENTS.md: apps pay only for what they
// use): no part of the feature, no helper it added, is reachable from here.
func (s *surface) PlaceWebViews(views []platform.WebViewPlacement) {
	host := s.w
	if host.closed || s.layers == 0 {
		return
	}
	s.placed = append(s.placed[:0], views...)
	if s.ext != nil {
		s.ext.placeViews()
		return
	}
	shown := make([]*window, 0, len(views))
	for _, p := range views {
		v, ok := p.WebView.(*window)
		if !ok || v.host != host || v.closed {
			continue
		}
		f := p.Frame
		x, y := int32(math.Round(f.X)), int32(math.Round(f.Y))
		gtkLayoutMove(s.web, v.web, x, y)
		gtkWidgetSetSizeRequest(v.web, int32(math.Round(f.X+f.W))-x, int32(math.Round(f.Y+f.H))-y)
		if !gtkWidgetGetVisible(v.web) {
			gtkWidgetShow(v.web)
		}
		shown = append(shown, v)
	}
	for _, v := range host.webViews {
		if !slices.Contains(shown, v) && gtkWidgetGetVisible(v.web) {
			if gtkWidgetHasFocus(v.web) {
				// A hidden view keeps no keyboard: it goes back to the
				// content.
				gtkWidgetGrabFocus(s.area)
			}
			gtkWidgetHide(v.web)
		}
	}
	s.shapeInput()
}

// placeWeb shows the window's web views where the content shows them and
// hides the others: the placement of a surface that made a native view,
// which merges it with the native views and orders and shapes the lot
// itself (nativeview.go).
func (s *surface) placeWeb() {
	host := s.w
	shown := make([]*window, 0, len(s.placed))
	for _, p := range s.placed {
		v, ok := p.WebView.(*window)
		if !ok || v.host != host || v.closed {
			continue
		}
		f := p.Frame
		x, y := int32(math.Round(f.X)), int32(math.Round(f.Y))
		gtkLayoutMove(s.web, v.web, x, y)
		gtkWidgetSetSizeRequest(v.web, int32(math.Round(f.X+f.W))-x, int32(math.Round(f.Y+f.H))-y)
		if !gtkWidgetGetVisible(v.web) {
			// Showing the page's window maps it over its siblings,
			// whatever order the content painted them in: the order the
			// surface remembers (orderViews) no longer describes the
			// windows, which a page that goes away and comes back shows,
			// so ordering forgets it and puts them back.
			s.ordered = s.ordered[:0]
			gtkWidgetShow(v.web)
		}
		shown = append(shown, v)
	}
	for _, v := range host.webViews {
		if !slices.Contains(shown, v) && gtkWidgetGetVisible(v.web) {
			if gtkWidgetHasFocus(v.web) {
				// A hidden view keeps no keyboard: it goes back to the
				// content.
				gtkWidgetGrabFocus(s.area)
			}
			gtkWidgetHide(v.web)
		}
	}
}

// windowOrder is one window of a view of the surface: the order the
// content painted the view in, and the view's window, zero until GTK
// realizes it.
type windowOrder struct {
	order int
	win   ptr
}

// stackWindows puts the windows of the views the content shows in the
// order it painted them: the first painted first, the last over the
// others. A container maps a window over its siblings as it shows it, and
// the surface's area is an overlay over them all (makeLayers), so the
// order of the view windows among themselves is the content's. Only the
// surface's extension orders them (nativeview.go): a surface that made no
// native view has nothing to order them under, and links none of this
// (AGENTS.md: apps pay only for what they use). Windows that are not
// realized yet are ordered by the frame that realizes them (orderDirty):
// shown is how many views the content shows, more than len(entries) when
// some have no window yet.
func (s *surface) stackWindows(entries []windowOrder, shown int) {
	s.orderDirty = len(entries) < shown
	if len(entries) < 2 {
		// Nothing to order: one window cannot be in the wrong place
		// among the others, and no window at all less so. What the
		// surface remembers is not what it shows any more, though (a
		// view went away, or has no window yet), and it is what the
		// next frame with two windows compares against: forgetting it
		// makes that frame put them in the content's order rather than
		// trust an order that describes windows that are not these.
		s.ordered = s.ordered[:0]
		return
	}
	// A window shows a handful of views, and the content numbers them in
	// the order it painted them: sorting them is a few moves, and an
	// insertion sort of them stays stable and keeps the generic
	// instantiations of a sort out of the path every app links.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].order > entries[j].order; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
	same := len(s.ordered) == len(entries)
	for i := 0; same && i < len(entries); i++ {
		same = s.ordered[i] == entries[i].win
	}
	if same {
		// The windows are in the order the surface remembers: a frame
		// that changed nothing is not a reason to call into GDK.
		return
	}
	for i := 1; i < len(entries); i++ {
		// Each after the one before it: the last ends on top.
		gdkWindowRestack(entries[i].win, entries[i-1].win, true)
	}
	s.ordered = s.ordered[:0]
	for _, e := range entries {
		s.ordered = append(s.ordered, e.win)
	}
}

// shapeInput lets the pointer through the area where the views under it
// take it: the web views alone when the surface made no native view, and
// the web views merged with the native views, in the content's paint
// order, through the surface's extension when it made one (nativeview.go).
func (s *surface) shapeInput() {
	if s.ext != nil {
		s.ext.shapeInput()
		return
	}
	win := gtkWidgetGetParentWindow(s.area) // the overlay's window of the area
	if win == 0 || win == gtkWidgetGetWindow(s.w.win) {
		return
	}
	var all gdkRectangle
	gtkWidgetGetAllocation(s.area, &all)
	region := cairoRegionCreateRectangle(&gdkRectangle{Width: all.Width, Height: all.Height})
	for _, p := range s.placed {
		cairoRegionSubtractRectangle(region, gdkRect(p.Clip))
		// Unless what the content painted over it takes the pointer:
		// put back by a second region, as cairo has no union of a
		// difference.
		for _, c := range p.Covers {
			r := gdkRect(c)
			cover := cairoRegionCreateRectangle(r)
			cairoRegionUnion(region, cover)
			cairoRegionDestroy(cover)
		}
	}
	gdkWindowInputShapeCombineRegion(win, region, 0, 0)
	cairoRegionDestroy(region)
}

// nativeAccessibles returns the accessibles of the native views the
// content shows, which the overlay of the surface holds under its area
// (surface_access.go): none when the surface made no native view.
func (s *surface) nativeAccessibles() []ptr {
	if s.ext == nil {
		return nil
	}
	return s.ext.accessibles()
}

// gdkRect returns the pixels of r, in DIPs, that it covers wholly.
func gdkRect(r platform.RectF) *gdkRectangle {
	x, y := int32(math.Ceil(r.X)), int32(math.Ceil(r.Y))
	return &gdkRectangle{X: x, Y: y, Width: max(int32(math.Floor(r.X+r.W))-x, 0), Height: max(int32(math.Floor(r.Y+r.H))-y, 0)}
}

// webViewAt returns the web view that takes the pointer at x, y in the
// surface.
func (s *surface) webViewAt(x, y float64) *window {
	for i := len(s.placed) - 1; i >= 0; i-- {
		if p := &s.placed[i]; p.At(x, y) {
			if v, ok := p.WebView.(*window); ok && !v.closed {
				return v
			}
		}
	}
	return nil
}

// top returns the window that shows w: its host for a web view.
func (w *window) top() *window {
	if w.host != nil {
		return w.host
	}
	return w
}

// webViewPressed tells the content of the window that a button went down
// on the web view w, at x, y in its window, which takes the press.
func (w *window) webViewPressed(x, y float64, button uint32, mods platform.Modifiers) {
	s := w.host.surface
	if s == nil || w.host.closed {
		return
	}
	var a gdkRectangle
	gtkWidgetGetAllocation(w.web, &a)
	b, ok := map[uint32]int{1: 0, 2: 2, 3: 1}[button]
	if !ok {
		return
	}
	s.send(platform.SurfaceEvent{Kind: platform.WebViewPress, X: float64(a.X) + x, Y: float64(a.Y) + y, Button: b, Mods: mods})
}

// focusedWebView returns the web view of w that has the keyboard, if one
// does.
func (w *window) focusedWebView() *window {
	for _, v := range w.webViews {
		if !v.closed && gtkWidgetHasFocus(v.web) {
			return v
		}
	}
	return nil
}

// TabInto gives the web view the keyboard. WebKitGTK focuses no element
// of the page as the view takes it: once the page let go of the one it
// had, the Tab moving the focus here, passed on, focuses its first, or
// its last going back.
func (w *window) TabInto(back bool) {
	s := w.host.surface
	if w.closed || s == nil {
		return
	}
	w.Eval("document.activeElement && document.activeElement.blur()")
	gtkWidgetGrabFocus(w.web)
	if ev := s.pressing; ev != 0 {
		switch field[uint32](ev, 28) {
		case 0xff09, 0xfe20, 0xff89: // GDK_KEY_Tab, ISO_Left_Tab, KP_Tab
			gtkWidgetEvent(w.web, ev)
		}
	}
}

// tabOut gives the content the keyboard back as Tab leaves the page of the
// web view w past its last element, or Shift+Tab past its first.
func (w *window) tabOut(back bool) {
	s := w.host.surface
	if s == nil || w.host.closed {
		return
	}
	gtkWidgetGrabFocus(s.area)
	ev := platform.SurfaceEvent{Kind: platform.WebViewTabOut, Key: platform.KeyTab, WebView: w}
	if back {
		ev.Mods = platform.ModShift
	}
	s.send(ev)
}

// closeWebView closes a web view: by Close, or as its window closes.
func (w *window) closeWebView() {
	if w.closed {
		return
	}
	w.closed = true
	b := w.b
	delete(b.webViews, w.id)
	delete(b.byWebView, w.web)
	if gtkWidgetHasFocus(w.web) && w.host.surface != nil {
		gtkWidgetGrabFocus(w.host.surface.area)
	}
	webkitUserContentManagerUnregisterHandler(w.ucm, cs("mygo"))
	webkitUserContentManagerRemoveAllScripts(w.ucm)
	gtkWidgetDestroy(w.web)
	gObjectUnref(w.ucm)
	if w.press.event != 0 {
		gdkEventFree(w.press.event)
		w.press.event = 0
	}
	h := w.host
	h.webViews = slices.DeleteFunc(h.webViews, func(v *window) bool { return v == w })
	if s := h.surface; s != nil {
		s.placed = slices.DeleteFunc(s.placed, func(p platform.WebViewPlacement) bool { return p.WebView == platform.WebView(w) })
		if !h.closed {
			s.shapeInput()
		}
	}
}

// closeWebViews closes the web views of a window that closes.
func (w *window) closeWebViews() {
	for len(w.webViews) > 0 {
		w.webViews[len(w.webViews)-1].closeWebView()
	}
}
